#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
policy_script="$script_dir/release-state-policy.sh"
[[ -s "$policy_script" ]] || {
  echo "ERROR: release state policy is missing" >&2
  exit 1
}
# shellcheck disable=SC1090
source "$policy_script"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  cat >&2 <<'EOF'
usage: publish-release.sh --tag <tag> --tag-ref-oid <oid> --commit <sha>
  [--current-manifest-artifact-id <id>
   --current-manifest-artifact-digest <sha256:...>
   --current-payload-artifact-id <id>
   --current-payload-artifact-digest <sha256:...>]
EOF
  exit 2
}

tag=""
tag_ref_oid=""
commit=""
current_manifest_artifact_id=""
current_manifest_artifact_digest=""
current_payload_artifact_id=""
current_payload_artifact_digest=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag) tag="${2:-}"; shift 2 ;;
    --tag-ref-oid) tag_ref_oid="${2:-}"; shift 2 ;;
    --commit) commit="${2:-}"; shift 2 ;;
    --current-manifest-artifact-id) current_manifest_artifact_id="${2:-}"; shift 2 ;;
    --current-manifest-artifact-digest) current_manifest_artifact_digest="${2:-}"; shift 2 ;;
    --current-payload-artifact-id) current_payload_artifact_id="${2:-}"; shift 2 ;;
    --current-payload-artifact-digest) current_payload_artifact_digest="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-custom\.[0-9]+$ ]] || usage
[[ "$tag_ref_oid" =~ ^[0-9a-f]{40,64}$ ]] || usage
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || usage
[[ "${GITHUB_REPOSITORY:-}" == "o87110/sub2api-custom-public" ]] ||
  fail "publishing is restricted to the trusted public custom repository"
for tool in gh jq oras sha256sum unzip; do
  command -v "$tool" >/dev/null 2>&1 || fail "required publishing tool is missing: $tool"
done

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
oci_repository="ghcr.io/o87110/sub2api-custom-public"

query_release() {
  gh api --paginate "repos/${GITHUB_REPOSITORY}/releases?per_page=100" |
    jq -s --arg tag "$tag" 'add | map(select(.tag_name == $tag))'
}

verify_tag_unchanged() {
  local local_tag_ref remote_oid remote_commit
  remote_oid="$(
    gh api "repos/${GITHUB_REPOSITORY}/git/ref/tags/${tag}" \
      --jq '.object.sha'
  )"
  [[ "$remote_oid" == "$tag_ref_oid" ]] ||
    fail "remote Tag ref OID changed before a Release operation"
  local_tag_ref="refs/tags/${tag}"
  git show-ref --verify --quiet "$local_tag_ref" ||
    fail "checked-out repository lacks the requested Tag"
  [[ "$(git rev-parse "$local_tag_ref")" == "$tag_ref_oid" ]] ||
    fail "local Tag ref OID differs from preflight"
  remote_commit="$(git rev-parse "${local_tag_ref}^{commit}")"
  [[ "$remote_commit" == "$commit" ]] ||
    fail "Tag peeled commit changed before a Release operation"
}

download_release_asset() {
  local asset_id="$1"
  local destination="$2"
  local expected_digest="$3"
  gh api \
    -H "Accept: application/octet-stream" \
    "repos/${GITHUB_REPOSITORY}/releases/assets/${asset_id}" > "$destination"
  [[ "$expected_digest" =~ ^sha256:[0-9a-f]{64}$ ]] ||
    fail "Release asset digest metadata is missing"
  actual="sha256:$(sha256sum "$destination" | awk '{print $1}')"
  [[ "$actual" == "$expected_digest" ]] ||
    fail "Release asset ${asset_id} bytes do not match GitHub digest metadata"
}

validate_manifest() {
  local manifest="$1"
  jq -e \
    --arg repository "$GITHUB_REPOSITORY" \
    --arg tag "$tag" \
    --arg tag_ref_oid "$tag_ref_oid" \
    --arg commit "$commit" \
    '
      .schema == "sub2api-custom-release/v1" and
      .repository == $repository and
      .tag == $tag and
      .tag_ref_oid == $tag_ref_oid and
      .target_commit == $commit and
      .producer.workflow == ".github/workflows/release.yml" and
      (.producer.event == "push" or .producer.event == "workflow_dispatch") and
      (.producer.workflow_commit | test("^[0-9a-f]{40}$")) and
      (
        (
          .producer.event == "workflow_dispatch" and
          .producer.workflow_ref == "refs/heads/main"
        ) or
        (
          .producer.event == "push" and
          .producer.workflow_ref == ("refs/tags/" + $tag) and
          .producer.workflow_commit == $commit
        )
      ) and
      (.producer.run_id | type == "number" and . > 0) and
      (.producer.run_attempt | type == "number" and . > 0) and
      (.payload_artifact.id | type == "number" and . > 0) and
      (.payload_artifact.digest | test("^sha256:[0-9a-f]{64}$")) and
      (.payload_artifact.name |
        test("^release-payload-v[0-9]+\\.[0-9]+\\.[0-9]+-custom\\.[0-9]+-[0-9]+-[0-9]+$")) and
      (.payload_content_sha256 | test("^[0-9a-f]{64}$")) and
      (.assets | length) == 3 and
      ([.assets[].name] | unique | length) == 3 and
      ([.assets[].name] | index("checksums.txt")) != null and
      ([.assets[].sha256 | test("^[0-9a-f]{64}$")] | all) and
      .oci.repository == "ghcr.io/o87110/sub2api-custom-public" and
      (.oci.index_digest | test("^sha256:[0-9a-f]{64}$")) and
      .oci.index_media_type == "application/vnd.oci.image.index.v1+json" and
      (.oci.manifests | length) == 2 and
      ([.oci.manifests[] | .os + "/" + .architecture] | sort) ==
        ["linux/amd64", "linux/arm64"] and
      ([.oci.manifests[].variant | type == "string"] | all) and
      ([.oci.manifests[].media_type] | unique) ==
        ["application/vnd.oci.image.manifest.v1+json"] and
      ([.oci.manifests[].digest | test("^sha256:[0-9a-f]{64}$")] | all) and
      .oci.tags.multiarch == $tag and
      .oci.tags.amd64 == ($tag + "-amd64") and
      .oci.tags.arm64 == ($tag + "-arm64")
    ' "$manifest" >/dev/null || fail "release manifest is invalid"
}

validate_artifact_producer() {
  local artifact_json="$1"
  local manifest="$2"
  local artifact_id artifact_name run_id expected_name run_json
  artifact_id="$(jq -er '.id' <<<"$artifact_json")"
  artifact_name="$(jq -er '.name' <<<"$artifact_json")"
  run_id="$(jq -er '.workflow_run.id' <<<"$artifact_json")"
  [[ "$(jq -r '.expired' <<<"$artifact_json")" == "false" ]] ||
    fail "workflow artifact $artifact_id is expired"
  expected_name="release-manifest-${tag}-$(jq -r '.producer.run_id' "$manifest")-$(jq -r '.producer.run_attempt' "$manifest")"
  [[ "$artifact_name" == "$expected_name" ]] ||
    fail "manifest workflow artifact has a non-deterministic name"
  [[ "$run_id" == "$(jq -r '.producer.run_id' "$manifest")" ]] ||
    fail "manifest artifact producer run ID mismatch"
  run_json="$(gh api "repos/${GITHUB_REPOSITORY}/actions/runs/${run_id}")"
  [[ "$(jq -r '.run_attempt' <<<"$run_json")" == "$(jq -r '.producer.run_attempt' "$manifest")" ]] ||
    fail "manifest artifact producer run attempt mismatch"
  [[ "$(jq -r '.head_sha' <<<"$run_json")" == "$(jq -r '.producer.workflow_commit' "$manifest")" ]] ||
    fail "manifest artifact producer workflow commit mismatch"
  [[ "$(jq -r '.event' <<<"$run_json")" == "$(jq -r '.producer.event' "$manifest")" ]] ||
    fail "manifest artifact producer event mismatch"
  if [[ "$(jq -r '.producer.event' "$manifest")" == "workflow_dispatch" ]]; then
    [[ "$(jq -r '.head_branch' <<<"$run_json")" == "main" ]] ||
      fail "manifest artifact workflow dispatch came from an untrusted branch"
  fi
  [[ "$(jq -r '.path' <<<"$run_json")" == ".github/workflows/release.yml" ]] ||
    fail "manifest artifact came from a different workflow"
}

download_current_manifest_artifact() {
  local artifact_id="$1"
  local expected_digest="$2"
  local destination="$3"
  local artifact_json artifact_digest archive artifact_dir

  artifact_json="$(gh api "repos/${GITHUB_REPOSITORY}/actions/artifacts/${artifact_id}")"
  [[ "$(jq -r '.id' <<<"$artifact_json")" == "$artifact_id" ]] ||
    fail "current manifest artifact ID mismatch"
  [[ "$(jq -r '.expired' <<<"$artifact_json")" == "false" ]] ||
    fail "current manifest artifact is expired"
  artifact_digest="$(jq -r '.digest // ""' <<<"$artifact_json")"
  [[ "$artifact_digest" == "$expected_digest" ]] ||
    fail "current manifest artifact digest metadata mismatch"

  archive="$tmp_dir/current-manifest-artifact.zip"
  gh api "repos/${GITHUB_REPOSITORY}/actions/artifacts/${artifact_id}/zip" > "$archive"
  [[ "sha256:$(sha256sum "$archive" | awk '{print $1}')" == "$expected_digest" ]] ||
    fail "downloaded current manifest artifact digest mismatch"
  artifact_dir="$tmp_dir/current-manifest-artifact"
  mkdir -p "$artifact_dir"
  unzip -q "$archive" -d "$artifact_dir"
  mapfile -t manifests < <(
    find "$artifact_dir" -type f -name release-manifest.json -print
  )
  [[ "${#manifests[@]}" -eq 1 ]] ||
    fail "current manifest artifact must contain exactly one release-manifest.json"
  cp "${manifests[0]}" "$destination"
  validate_manifest "$destination"
  validate_artifact_producer "$artifact_json" "$destination"
}

recover_manifest_artifact() {
  local artifacts candidate_dir artifact_id artifact_json archive digest
  artifacts="$(
    gh api --paginate "repos/${GITHUB_REPOSITORY}/actions/artifacts?per_page=100" |
      jq -s --arg prefix "release-manifest-${tag}-" '
        if all(.[]; (.artifacts | type) == "array") then
          [.[].artifacts[]] |
          map(select((.name | startswith($prefix)) and (.expired == false))) |
          sort_by(.id)
        else
          error("Actions artifact pagination response is invalid")
        end
      '
  )"
  [[ "$(jq 'length' <<<"$artifacts")" -gt 0 ]] ||
    fail "remote publishing state exists but no recoverable manifest artifact was found"
  : > "$tmp_dir/valid-manifests.tsv"
  while IFS= read -r artifact_id; do
    candidate_dir="$tmp_dir/manifest-artifact-$artifact_id"
    mkdir -p "$candidate_dir"
    artifact_json="$(jq --argjson id "$artifact_id" '.[] | select(.id == $id)' <<<"$artifacts")"
    archive="$candidate_dir/artifact.zip"
    if ! gh api "repos/${GITHUB_REPOSITORY}/actions/artifacts/${artifact_id}/zip" > "$archive"; then
      continue
    fi
    digest="$(jq -r '.digest // ""' <<<"$artifact_json")"
    [[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] || continue
    [[ "sha256:$(sha256sum "$archive" | awk '{print $1}')" == "$digest" ]] || continue
    unzip -q "$archive" -d "$candidate_dir/content"
    mapfile -t manifests < <(find "$candidate_dir/content" -type f -name release-manifest.json -print)
    [[ "${#manifests[@]}" -eq 1 ]] || continue
    if ! (validate_manifest "${manifests[0]}") 2>/dev/null; then
      continue
    fi
    if ! (validate_artifact_producer "$artifact_json" "${manifests[0]}") 2>/dev/null; then
      continue
    fi
    manifest_sha="$(sha256sum "${manifests[0]}" | awk '{print $1}')"
    payload_ref="$(
      jq -cS '.payload_artifact | {id,name,digest}' "${manifests[0]}"
    )"
    printf '%s\t%s\t%s\t%s\n' \
      "$manifest_sha" "$payload_ref" "$artifact_id" "${manifests[0]}" \
      >> "$tmp_dir/valid-manifests.tsv"
  done < <(jq -r '.[].id' <<<"$artifacts")

  authoritative_manifest="$(
    select_consistent_manifest_candidate "$tmp_dir/valid-manifests.tsv"
  )" || fail "no unique manifest artifact can recover the existing remote publishing state"
}

download_payload_from_manifest() {
  local manifest="$1"
  local destination="$2"
  local artifact_id artifact_json artifact_digest archive run_id run_json
  artifact_id="$(jq -er '.payload_artifact.id' "$manifest")"
  artifact_json="$(gh api "repos/${GITHUB_REPOSITORY}/actions/artifacts/${artifact_id}")"
  [[ "$(jq -r '.expired' <<<"$artifact_json")" == "false" ]] ||
    fail "payload workflow artifact is expired"
  [[ "$(jq -r '.name' <<<"$artifact_json")" == "$(jq -r '.payload_artifact.name' "$manifest")" ]] ||
    fail "payload artifact name mismatch"
  artifact_digest="$(jq -r '.digest // ""' <<<"$artifact_json")"
  [[ "$artifact_digest" == "$(jq -r '.payload_artifact.digest' "$manifest")" ]] ||
    fail "payload artifact digest metadata mismatch"
  run_id="$(jq -er '.workflow_run.id' <<<"$artifact_json")"
  [[ "$run_id" == "$(jq -r '.producer.run_id' "$manifest")" ]] ||
    fail "payload producer run mismatch"
  run_json="$(gh api "repos/${GITHUB_REPOSITORY}/actions/runs/${run_id}")"
  [[ "$(jq -r '.run_attempt' <<<"$run_json")" == "$(jq -r '.producer.run_attempt' "$manifest")" ]] ||
    fail "payload producer attempt mismatch"
  [[ "$(jq -r '.head_sha' <<<"$run_json")" == "$(jq -r '.producer.workflow_commit' "$manifest")" ]] ||
    fail "payload producer workflow commit mismatch"
  [[ "$(jq -r '.event' <<<"$run_json")" == "$(jq -r '.producer.event' "$manifest")" ]] ||
    fail "payload producer event mismatch"
  if [[ "$(jq -r '.producer.event' "$manifest")" == "workflow_dispatch" ]]; then
    [[ "$(jq -r '.head_branch' <<<"$run_json")" == "main" ]] ||
      fail "payload workflow dispatch came from an untrusted branch"
  fi
  [[ "$(jq -r '.path' <<<"$run_json")" == ".github/workflows/release.yml" ]] ||
    fail "payload came from a different workflow"

  archive="$tmp_dir/payload-artifact.zip"
  gh api "repos/${GITHUB_REPOSITORY}/actions/artifacts/${artifact_id}/zip" > "$archive"
  [[ "sha256:$(sha256sum "$archive" | awk '{print $1}')" == "$artifact_digest" ]] ||
    fail "downloaded payload artifact digest mismatch"
  mkdir -p "$destination"
  unzip -q "$archive" -d "$destination"
}

validate_payload() {
  local manifest="$1"
  local payload="$2"
  [[ -s "$payload/build-metadata.json" && -s "$payload/payload-files.sha256" ]] ||
    fail "payload metadata is missing"
  (
    cd "$payload"
    sha256sum --check --strict payload-files.sha256 >/dev/null
  )
  [[ "$(sha256sum "$payload/payload-files.sha256" | awk '{print $1}')" == "$(jq -r '.payload_content_sha256' "$manifest")" ]] ||
    fail "payload internal content digest mismatch"
  jq -e --slurpfile manifest "$manifest" '
    .tag == $manifest[0].tag and
    .target_commit == $manifest[0].target_commit and
    .payload_content_sha256 == $manifest[0].payload_content_sha256 and
    .assets == $manifest[0].assets and
    .oci.index_digest == $manifest[0].oci.index_digest and
    .oci.index_media_type == $manifest[0].oci.index_media_type and
    .oci.manifests == $manifest[0].oci.manifests
  ' "$payload/build-metadata.json" >/dev/null || fail "payload metadata does not match release manifest"

  while IFS=$'\t' read -r name expected_sha; do
    [[ -f "$payload/assets/$name" ]] || fail "payload asset is missing: $name"
    [[ "$(sha256sum "$payload/assets/$name" | awk '{print $1}')" == "$expected_sha" ]] ||
      fail "payload asset digest mismatch: $name"
  done < <(jq -r '.assets[] | [.name, .sha256] | @tsv' "$manifest")

  version="${tag#v}"
  for name in "sub2api_${version}_linux_amd64.tar.gz" "sub2api_${version}_linux_arm64.tar.gz"; do
    count="$(awk -v name="$name" '$2 == name && $1 ~ /^[0-9a-fA-F]{64}$/ { count++ } END { print count + 0 }' "$payload/assets/checksums.txt")"
    [[ "$count" -eq 1 ]] || fail "checksum entry is not unique for $name"
    checksum="$(awk -v name="$name" '$2 == name { print tolower($1) }' "$payload/assets/checksums.txt")"
    expected="$(jq -r --arg name "$name" '.assets[] | select(.name == $name) | .sha256' "$manifest")"
    [[ "$checksum" == "$expected" ]] || fail "checksums.txt disagrees with release manifest for $name"
  done

  index_digest="$(jq -r '.oci.index_digest' "$manifest")"
  local_index="$tmp_dir/local-index.json"
  oras manifest fetch --oci-layout "$payload/oci-layout@$index_digest" > "$local_index"
  [[ "sha256:$(sha256sum "$local_index" | awk '{print $1}')" == "$index_digest" ]] ||
    fail "local OCI index bytes do not match its digest"
  jq -e --slurpfile manifest "$manifest" '
    (.manifests | length) == 2 and
    ([.manifests[] | {
      os: .platform.os,
      architecture: .platform.architecture,
      variant: (.platform.variant // ""),
      media_type: .mediaType,
      digest
    }] | sort_by(.architecture)) ==
    ([$manifest[0].oci.manifests[] | {
      os,
      architecture,
      variant,
      media_type,
      digest
    }] | sort_by(.architecture))
  ' "$local_index" >/dev/null || fail "OCI index does not contain the two declared manifests"
}

remote_digest_value=""
is_missing_ghcr_repository_error() {
  local error_file="$1"
  [[ "$(<"$error_file")" == \
    "Error response from registry: name unknown: repository name not known to registry" ]]
}

resolve_remote_digest() {
  local remote_tag="$1"
  local tags_json digest error_file
  error_file="$tmp_dir/oras-query-error"

  if ! tags_json="$(
    oras repo tags --format json "$oci_repository" 2>"$error_file"
  )"; then
    if is_missing_ghcr_repository_error "$error_file"; then
      tags_json='{"tags":[]}'
    else
      fail "cannot query GHCR tags: $(<"$error_file")"
    fi
  fi
  jq -e '.tags | type == "array" and all(.[]; type == "string")' \
    <<<"$tags_json" >/dev/null ||
    fail "GHCR tag listing is invalid"
  if ! jq -e --arg tag "$remote_tag" '.tags | index($tag) != null' \
    <<<"$tags_json" >/dev/null; then
    remote_digest_value=""
    return 0
  fi

  if ! digest="$(oras resolve "$oci_repository:$remote_tag" 2>"$error_file")"; then
    fail "cannot resolve existing GHCR tag $remote_tag: $(<"$error_file")"
  fi
  [[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] ||
    fail "GHCR tag $remote_tag returned an invalid digest"
  remote_digest_value="$digest"
}

exact_tag_needs_write() {
  local remote_tag="$1"
  local expected_digest="$2"
  local existing_digest

  verify_tag_unchanged
  resolve_remote_digest "$remote_tag"
  existing_digest="$remote_digest_value"
  if [[ -z "$existing_digest" ]]; then
    return 0
  fi
  [[ "$existing_digest" == "$expected_digest" ]] ||
    fail "exact GHCR Tag $remote_tag changed before publication"
  return 1
}

verify_remote_oci() {
  local manifest="$1"
  local multi_tag amd64_tag arm64_tag index_digest amd64_digest arm64_digest
  multi_tag="$(jq -r '.oci.tags.multiarch' "$manifest")"
  amd64_tag="$(jq -r '.oci.tags.amd64' "$manifest")"
  arm64_tag="$(jq -r '.oci.tags.arm64' "$manifest")"
  index_digest="$(jq -r '.oci.index_digest' "$manifest")"
  amd64_digest="$(jq -r '.oci.manifests[] | select(.architecture == "amd64") | .digest' "$manifest")"
  arm64_digest="$(jq -r '.oci.manifests[] | select(.architecture == "arm64") | .digest' "$manifest")"
  resolve_remote_digest "$multi_tag"
  [[ "$remote_digest_value" == "$index_digest" ]] ||
    fail "remote multi-architecture Tag digest mismatch"
  resolve_remote_digest "$amd64_tag"
  [[ "$remote_digest_value" == "$amd64_digest" ]] ||
    fail "remote amd64 Tag digest mismatch"
  resolve_remote_digest "$arm64_tag"
  [[ "$remote_digest_value" == "$arm64_digest" ]] ||
    fail "remote arm64 Tag digest mismatch"

  oras manifest fetch --descriptor "$oci_repository@$index_digest" > "$tmp_dir/remote-index-descriptor.json"
  jq -e \
    --arg digest "$index_digest" \
    --arg media_type "$(jq -r '.oci.index_media_type' "$manifest")" \
    '.digest == $digest and .mediaType == $media_type' \
    "$tmp_dir/remote-index-descriptor.json" >/dev/null ||
    fail "remote OCI index descriptor mismatch"
  oras manifest fetch "$oci_repository@$index_digest" > "$tmp_dir/remote-index.json"
  [[ "sha256:$(sha256sum "$tmp_dir/remote-index.json" | awk '{print $1}')" == "$index_digest" ]] ||
    fail "remote OCI index bytes do not match its digest"
  jq -e --slurpfile manifest "$manifest" '
    (.manifests | length) == 2 and
    ([.manifests[] | {
      os: .platform.os,
      architecture: .platform.architecture,
      variant: (.platform.variant // ""),
      media_type: .mediaType,
      digest
    }] | sort_by(.architecture)) ==
    ([$manifest[0].oci.manifests[] | {
      os,
      architecture,
      variant,
      media_type,
      digest
    }] | sort_by(.architecture))
  ' "$tmp_dir/remote-index.json" >/dev/null || fail "remote OCI index members differ from release manifest"
}

query_release_asset() {
  local release="$1"
  local name="$2"
  jq -c --arg name "$name" '[.assets[] | select(.name == $name)]' <<<"$release"
}

validate_draft_asset_names() {
  local release="$1"
  local manifest="$2"
  jq -e --slurpfile manifest "$manifest" '
    ($manifest[0].assets | map(.name) + ["release-manifest.json"]) as $allowed |
    [.assets[].name] as $actual |
    ($actual | length) == ($actual | unique | length) and
    all($actual[]; . as $name | ($allowed | index($name)) != null)
  ' <<<"$release" >/dev/null ||
    fail "Draft contains duplicate or undeclared assets"
  draft_assets_follow_manifest "$release" ||
    fail "Draft contains an asset created before its authoritative manifest"
}

verify_release_assets() {
  local release="$1"
  local manifest="$2"
  actual_names="$(jq -c '[.assets[].name] | sort' <<<"$release")"
  expected_names="$(jq -c '[.assets[].name, "release-manifest.json"] | sort' "$manifest")"
  [[ "$actual_names" == "$expected_names" ]] ||
    fail "Release assets do not exactly match the authoritative manifest"

  while IFS=$'\t' read -r name expected_sha; do
    matches="$(query_release_asset "$release" "$name")"
    [[ "$(jq 'length' <<<"$matches")" -eq 1 ]] ||
      fail "Release asset must exist exactly once: $name"
    asset_id="$(jq -r '.[0].id' <<<"$matches")"
    asset_digest="$(jq -r '.[0].digest // ""' <<<"$matches")"
    download_release_asset "$asset_id" "$tmp_dir/remote-$name" "$asset_digest"
    [[ "$(sha256sum "$tmp_dir/remote-$name" | awk '{print $1}')" == "$expected_sha" ]] ||
      fail "Release asset bytes differ from manifest: $name"
  done < <(jq -r '.assets[] | [.name, .sha256] | @tsv' "$manifest")
}

printf '%s\n' "${GHCR_TOKEN:-${GH_TOKEN:-}}" |
  oras login ghcr.io \
    --username "${GITHUB_ACTOR:-github-actions[bot]}" \
    --password-stdin >/dev/null
verify_tag_unchanged

releases="$(query_release)"
[[ "$(jq 'length' <<<"$releases")" -le 1 ]] || fail "multiple Releases exist for $tag"
release=""
release_state=none
manifest_asset=""
if [[ "$(jq 'length' <<<"$releases")" -eq 1 ]]; then
  release="$(jq '.[0]' <<<"$releases")"
  [[ "$(jq -r '.tag_name' <<<"$release")" == "$tag" ]] ||
    fail "Release tag metadata is inconsistent"
  if [[ "$(jq -r '.draft' <<<"$release")" == "false" ||
        "$(jq -r '.immutable // false' <<<"$release")" == "true" ]]; then
    release_state=published
  else
    release_state=draft
  fi
  manifests="$(query_release_asset "$release" release-manifest.json)"
  [[ "$(jq 'length' <<<"$manifests")" -le 1 ]] ||
    fail "Release contains duplicate manifest assets"
  if [[ "$(jq 'length' <<<"$manifests")" -eq 1 ]]; then
    manifest_asset="$(jq '.[0]' <<<"$manifests")"
  fi
else
  resolve_remote_digest "$tag"
  multiarch_remote_digest="$remote_digest_value"
  resolve_remote_digest "${tag}-amd64"
  amd64_remote_digest="$remote_digest_value"
  resolve_remote_digest "${tag}-arm64"
  arm64_remote_digest="$remote_digest_value"
fi
if [[ -z "$release" &&
      ( -n "${multiarch_remote_digest:-}" ||
        -n "${amd64_remote_digest:-}" ||
        -n "${arm64_remote_digest:-}" ) ]]; then
  release_state=remote
fi

authoritative_manifest=""
if [[ -n "$manifest_asset" ]]; then
  authoritative_manifest="$tmp_dir/release-manifest.json"
  download_release_asset \
    "$(jq -r '.id' <<<"$manifest_asset")" \
    "$authoritative_manifest" \
    "$(jq -r '.digest // ""' <<<"$manifest_asset")"
  validate_manifest "$authoritative_manifest"
fi

manifest_present=false
[[ -n "$manifest_asset" ]] && manifest_present=true
current_artifacts_present=false
if [[ "$current_manifest_artifact_id" =~ ^[1-9][0-9]*$ &&
      "$current_manifest_artifact_digest" =~ ^sha256:[0-9a-f]{64}$ &&
      "$current_payload_artifact_id" =~ ^[1-9][0-9]*$ &&
      "$current_payload_artifact_digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
  current_artifacts_present=true
fi
manifest_action="$(
  release_manifest_action \
    "$release_state" \
    "$manifest_present" \
    "$current_artifacts_present"
)" || fail "Release state cannot select an authoritative manifest"

case "$manifest_action" in
  remote-manifest)
    [[ -n "$authoritative_manifest" ]] ||
      fail "remote manifest selection produced no manifest"
    ;;
  recover-manifest)
    recover_manifest_artifact
    ;;
  current-manifest)
    authoritative_manifest="$tmp_dir/current-release-manifest.json"
    download_current_manifest_artifact \
      "$current_manifest_artifact_id" \
      "$current_manifest_artifact_digest" \
      "$authoritative_manifest"
    [[ "$current_payload_artifact_id" == "$(jq -r '.payload_artifact.id' "$authoritative_manifest")" ]] ||
      fail "current payload artifact ID does not match manifest"
    [[ "$current_payload_artifact_digest" == "$(jq -r '.payload_artifact.digest' "$authoritative_manifest")" ]] ||
      fail "current payload artifact digest does not match manifest"
    ;;
  *)
    fail "unsupported manifest selection action: $manifest_action"
    ;;
esac

if [[ "$release_state" == "published" ]]; then
  verify_release_assets "$release" "$authoritative_manifest"
  verify_remote_oci "$authoritative_manifest"
  echo "published Release is read-only and matches the authoritative manifest"
  exit 0
fi

if [[ -n "$release" ]]; then
  validate_draft_asset_names "$release" "$authoritative_manifest"
  if [[ -z "$manifest_asset" && "$(jq '.assets | length' <<<"$release")" -ne 0 ]]; then
    fail "Draft contains assets before its authoritative manifest"
  fi
fi

payload_dir="$tmp_dir/payload"
download_payload_from_manifest "$authoritative_manifest" "$payload_dir"
validate_payload "$authoritative_manifest" "$payload_dir"

if [[ "$release_state" == "none" || "$release_state" == "remote" ]]; then
  [[ -z "$release" ]] || fail "unexpected Release state"
  verify_tag_unchanged
  {
    echo "Sub2API ${tag}"
    echo
    echo "Source commit: ${commit}"
    echo
    git tag -l --format='%(contents:body)' "$tag"
    echo
    echo "Installation guide:"
    echo "https://github.com/${GITHUB_REPOSITORY}/blob/${tag}/docs/custom/OPERATIONS_CN.md"
  } | gh release create "$tag" \
    --repo "$GITHUB_REPOSITORY" \
    --draft \
    --title "Sub2API ${tag#v}" \
    --notes-file -
  verify_tag_unchanged
  for attempt in 1 2 3 4 5; do
    releases="$(query_release)"
    release_count="$(jq 'length' <<<"$releases")"
    [[ "$release_count" -le 1 ]] ||
      fail "Draft creation produced multiple Releases"
    if [[ "$release_count" -eq 1 ]]; then
      break
    fi
    [[ "$attempt" -lt 5 ]] || fail "Draft creation did not become visible"
    sleep 2
  done
  release="$(jq '.[0]' <<<"$releases")"
  [[ "$(jq -r '.draft' <<<"$release")" == "true" ]] || fail "new Release is not Draft"
  release_state=draft
  validate_draft_asset_names "$release" "$authoritative_manifest"
  [[ "$(jq '.assets | length' <<<"$release")" -eq 0 ]] ||
    fail "new Draft unexpectedly contains assets before its manifest"
fi

manifests="$(query_release_asset "$release" release-manifest.json)"
if [[ "$(jq 'length' <<<"$manifests")" -eq 0 ]]; then
  verify_tag_unchanged
  manifest_upload="$tmp_dir/release-manifest.json"
  cp "$authoritative_manifest" "$manifest_upload"
  gh release upload "$tag" "$manifest_upload" --repo "$GITHUB_REPOSITORY"
elif [[ "$(jq 'length' <<<"$manifests")" -ne 1 ]]; then
  fail "Draft contains duplicate manifest assets"
fi

releases="$(query_release)"
release="$(jq '.[0]' <<<"$releases")"
manifests="$(query_release_asset "$release" release-manifest.json)"
[[ "$(jq 'length' <<<"$manifests")" -eq 1 ]] || fail "Draft manifest upload is not authoritative"
download_release_asset \
  "$(jq -r '.[0].id' <<<"$manifests")" \
  "$tmp_dir/authoritative-roundtrip.json" \
  "$(jq -r '.[0].digest // ""' <<<"$manifests")"
cmp -s "$authoritative_manifest" "$tmp_dir/authoritative-roundtrip.json" ||
  fail "Draft manifest bytes changed during upload"

while IFS=$'\t' read -r name expected_sha; do
  matches="$(query_release_asset "$release" "$name")"
  count="$(jq 'length' <<<"$matches")"
  if [[ "$count" -eq 0 ]]; then
    verify_tag_unchanged
    gh release upload "$tag" "$payload_dir/assets/$name" --repo "$GITHUB_REPOSITORY"
  elif [[ "$count" -eq 1 ]]; then
    asset_id="$(jq -r '.[0].id' <<<"$matches")"
    asset_digest="$(jq -r '.[0].digest // ""' <<<"$matches")"
    download_release_asset "$asset_id" "$tmp_dir/existing-$name" "$asset_digest"
    [[ "$(sha256sum "$tmp_dir/existing-$name" | awk '{print $1}')" == "$expected_sha" ]] ||
      fail "existing Draft asset differs from authoritative payload: $name"
  else
    fail "Draft contains duplicate asset: $name"
  fi
done < <(jq -r '.assets[] | [.name, .sha256] | @tsv' "$authoritative_manifest")

releases="$(query_release)"
release="$(jq '.[0]' <<<"$releases")"
verify_release_assets "$release" "$authoritative_manifest"

multi_tag="$(jq -r '.oci.tags.multiarch' "$authoritative_manifest")"
amd64_tag="$(jq -r '.oci.tags.amd64' "$authoritative_manifest")"
arm64_tag="$(jq -r '.oci.tags.arm64' "$authoritative_manifest")"
index_digest="$(jq -r '.oci.index_digest' "$authoritative_manifest")"
amd64_digest="$(jq -r '.oci.manifests[] | select(.architecture == "amd64") | .digest' "$authoritative_manifest")"
arm64_digest="$(jq -r '.oci.manifests[] | select(.architecture == "arm64") | .digest' "$authoritative_manifest")"

for pair in "$multi_tag=$index_digest" "$amd64_tag=$amd64_digest" "$arm64_tag=$arm64_digest"; do
  remote_tag="${pair%%=*}"
  expected_digest="${pair#*=}"
  resolve_remote_digest "$remote_tag"
  existing_digest="$remote_digest_value"
  [[ -z "$existing_digest" || "$existing_digest" == "$expected_digest" ]] ||
    fail "exact GHCR Tag $remote_tag already points to a different digest"
done

if exact_tag_needs_write "$multi_tag" "$index_digest"; then
  oras cp \
    --from-oci-layout \
    "$payload_dir/oci-layout@$index_digest" \
    "$oci_repository:$multi_tag"
fi
if exact_tag_needs_write "$amd64_tag" "$amd64_digest"; then
  oras tag "$oci_repository@$amd64_digest" "$amd64_tag"
fi
if exact_tag_needs_write "$arm64_tag" "$arm64_digest"; then
  oras tag "$oci_repository@$arm64_digest" "$arm64_tag"
fi
verify_remote_oci "$authoritative_manifest"

verify_tag_unchanged
releases="$(query_release)"
release="$(jq '.[0]' <<<"$releases")"
[[ "$(jq -r '.draft' <<<"$release")" == "true" ]] || fail "Release left Draft state unexpectedly"
verify_release_assets "$release" "$authoritative_manifest"
gh release edit "$tag" \
  --repo "$GITHUB_REPOSITORY" \
  --draft=false \
  --verify-tag

echo "Release $tag published from one verified payload and OCI layout"
