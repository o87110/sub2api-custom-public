#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
versions_file="$repo_root/.github/custom-tool-versions.env"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  echo "usage: $0 --tag <vX.Y.Z-custom.N> --commit <sha> --output <new-directory>" >&2
  exit 2
}

tag=""
commit=""
output=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag) tag="${2:-}"; shift 2 ;;
    --commit) commit="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-custom\.[0-9]+$ ]] || usage
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || usage
[[ -n "$output" && ! -e "$output" ]] || fail "payload output must be a new path"
[[ "$(git -C "$repo_root" rev-parse HEAD)" == "$commit" ]] ||
  fail "checked out commit does not match payload target"
git -C "$repo_root" rev-parse --verify "refs/tags/${tag}^{commit}" >/dev/null ||
  fail "payload target custom Tag is unavailable"
[[ "$(git -C "$repo_root" rev-parse "refs/tags/${tag}^{commit}")" == "$commit" ]] ||
  fail "payload target custom Tag does not peel to the requested commit"
[[ -s "$versions_file" ]] || fail "custom tool version manifest is missing"
# shellcheck disable=SC1090
source "$versions_file"

for tool in goreleaser oras docker-buildx docker jq sha256sum; do
  command -v "$tool" >/dev/null 2>&1 || fail "required payload tool is missing: $tool"
done

version="${tag#v}"
amd64_asset="sub2api_${version}_linux_amd64.tar.gz"
arm64_asset="sub2api_${version}_linux_arm64.tar.gz"
mkdir -p \
  "$output/assets" \
  "$output/oci-layout" \
  "$output/image-context/binaries/amd64" \
  "$output/image-context/binaries/arm64" \
  "$output/image-context/backend"

builder=""
cleanup() {
  if [[ -n "$builder" ]]; then
    docker buildx rm "$builder" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

(
  cd "$repo_root"
  goreleaser check
  goreleaser release --clean --skip=publish
)
git -C "$repo_root" diff --exit-code -- \
  backend/go.mod \
  backend/go.sum

copy_unique() {
  local name="$1"
  mapfile -t matches < <(find "$repo_root/dist" -type f -name "$name" -print)
  [[ "${#matches[@]}" -eq 1 ]] || fail "expected exactly one GoReleaser output named $name"
  cp "${matches[0]}" "$output/assets/$name"
}
copy_unique "$amd64_asset"
copy_unique "$arm64_asset"
copy_unique checksums.txt

for asset in "$amd64_asset" "$arm64_asset"; do
  entry_count="$(awk -v name="$asset" '$2 == name && $1 ~ /^[0-9a-fA-F]{64}$/ { count++ } END { print count + 0 }' "$output/assets/checksums.txt")"
  [[ "$entry_count" -eq 1 ]] || fail "checksum entry must be unique for $asset"
  expected="$(awk -v name="$asset" '$2 == name { print tolower($1) }' "$output/assets/checksums.txt")"
  actual="$(sha256sum "$output/assets/$asset" | awk '{print $1}')"
  [[ "$actual" == "$expected" ]] || fail "GoReleaser checksum mismatch for $asset"
done

extract_binary() {
  local archive="$1"
  local architecture="$2"
  mapfile -t members < <(tar -tzf "$archive" | grep -E '(^|/)sub2api$' || true)
  [[ "${#members[@]}" -eq 1 ]] || fail "archive must contain exactly one sub2api binary: $archive"
  tar -xOzf "$archive" "${members[0]}" > "$output/image-context/binaries/$architecture/sub2api"
  chmod 0755 "$output/image-context/binaries/$architecture/sub2api"
}
extract_binary "$output/assets/$amd64_asset" amd64
extract_binary "$output/assets/$arm64_asset" arm64
cp "$repo_root/deploy/docker-entrypoint.sh" "$output/image-context/docker-entrypoint.sh"
cp -R "$repo_root/backend/resources" "$output/image-context/backend/resources"

docker_config="${DOCKER_CONFIG:-${RUNNER_TEMP:-$output}/docker-config}"
mkdir -p "$docker_config/cli-plugins"
cp "$(command -v docker-buildx)" "$docker_config/cli-plugins/docker-buildx"
chmod 0755 "$docker_config/cli-plugins/docker-buildx"
export DOCKER_CONFIG="$docker_config"
docker buildx version | grep -Fq "v${BUILDX_VERSION}"

docker run --privileged --rm "$QEMU_BINFMT_IMAGE" --install arm64 >/dev/null
builder="sub2api-release-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}"
docker buildx create --name "$builder" --driver docker-container --use >/dev/null
docker buildx inspect --bootstrap >/dev/null

oci_tar="$output/oci-layout.tar"
docker buildx build \
  --file "$repo_root/deploy/release/Dockerfile" \
  --platform linux/amd64,linux/arm64 \
  --build-arg "RELEASE_TAG=$tag" \
  --build-arg "SOURCE_COMMIT=$commit" \
  --provenance=false \
  --sbom=false \
  --output "type=oci,dest=$oci_tar" \
  "$output/image-context"
tar -xf "$oci_tar" -C "$output/oci-layout"
rm -f "$oci_tar"
rm -rf "$output/image-context"

index_descriptor="$(
  jq -ec '
  if (.manifests | length) != 1 then error("OCI layout must contain one root descriptor") end |
  .manifests[0] |
  select(
    .mediaType == "application/vnd.oci.image.index.v1+json" and
    (.digest | test("^sha256:[0-9a-f]{64}$"))
  )
' "$output/oci-layout/index.json"
)"
index_digest="$(jq -r '.digest' <<<"$index_descriptor")"
index_media_type="$(jq -r '.mediaType' <<<"$index_descriptor")"
oras manifest fetch --oci-layout "$output/oci-layout@$index_digest" > "$output/oci-index.json"
jq -e '
  .schemaVersion == 2 and
  (.manifests | length) == 2 and
  ([.manifests[].platform.architecture] | sort) == ["amd64", "arm64"] and
  ([.manifests[].platform.os] | unique) == ["linux"] and
  all(
    .manifests[];
    .mediaType == "application/vnd.oci.image.manifest.v1+json" and
    ((.platform.variant // "") | type == "string") and
    (.digest | test("^sha256:[0-9a-f]{64}$"))
  )
' "$output/oci-index.json" >/dev/null
amd64_descriptor="$(
  jq -ec '.manifests[] | select(.platform.os == "linux" and .platform.architecture == "amd64")' \
    "$output/oci-index.json"
)"
arm64_descriptor="$(
  jq -ec '.manifests[] | select(.platform.os == "linux" and .platform.architecture == "arm64")' \
    "$output/oci-index.json"
)"

amd64_sha="$(sha256sum "$output/assets/$amd64_asset" | awk '{print $1}')"
arm64_sha="$(sha256sum "$output/assets/$arm64_asset" | awk '{print $1}')"
checksums_sha="$(sha256sum "$output/assets/checksums.txt" | awk '{print $1}')"

(
  cd "$output"
  find assets oci-layout -type f -print0 |
    sort -z |
    xargs -0 sha256sum > payload-files.sha256
)
payload_content_sha="$(sha256sum "$output/payload-files.sha256" | awk '{print $1}')"

jq -n \
  --arg tag "$tag" \
  --arg commit "$commit" \
  --arg amd64_asset "$amd64_asset" \
  --arg amd64_sha "$amd64_sha" \
  --arg arm64_asset "$arm64_asset" \
  --arg arm64_sha "$arm64_sha" \
  --arg checksums_sha "$checksums_sha" \
  --arg index_digest "$index_digest" \
  --arg index_media_type "$index_media_type" \
  --argjson amd64_descriptor "$amd64_descriptor" \
  --argjson arm64_descriptor "$arm64_descriptor" \
  --arg payload_content_sha "$payload_content_sha" \
  '{
    schema: "sub2api-custom-payload/v1",
    tag: $tag,
    target_commit: $commit,
    payload_content_sha256: $payload_content_sha,
    assets: [
      {name: $amd64_asset, sha256: $amd64_sha},
      {name: $arm64_asset, sha256: $arm64_sha},
      {name: "checksums.txt", sha256: $checksums_sha}
    ],
    oci: {
      index_digest: $index_digest,
      index_media_type: $index_media_type,
      manifests: [
        {
          os: $amd64_descriptor.platform.os,
          architecture: $amd64_descriptor.platform.architecture,
          variant: ($amd64_descriptor.platform.variant // ""),
          media_type: $amd64_descriptor.mediaType,
          digest: $amd64_descriptor.digest
        },
        {
          os: $arm64_descriptor.platform.os,
          architecture: $arm64_descriptor.platform.architecture,
          variant: ($arm64_descriptor.platform.variant // ""),
          media_type: $arm64_descriptor.mediaType,
          digest: $arm64_descriptor.digest
        }
      ]
    }
  }' > "$output/build-metadata.json"

echo "release payload built at $output"
