#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  echo "usage: $0 --payload <dir> --output <file> --tag-ref-oid <oid> --artifact-id <id> --artifact-digest <sha256:...> --artifact-name <name>" >&2
  exit 2
}

payload=""
output=""
tag_ref_oid=""
artifact_id=""
artifact_digest=""
artifact_name=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --payload) payload="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    --tag-ref-oid) tag_ref_oid="${2:-}"; shift 2 ;;
    --artifact-id) artifact_id="${2:-}"; shift 2 ;;
    --artifact-digest) artifact_digest="${2:-}"; shift 2 ;;
    --artifact-name) artifact_name="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[[ -d "$payload" && -s "$payload/build-metadata.json" ]] || usage
[[ -n "$output" && ! -e "$output" ]] || fail "manifest output must be a new file"
[[ "$tag_ref_oid" =~ ^[0-9a-f]{40,64}$ ]] || usage
[[ "$artifact_id" =~ ^[1-9][0-9]*$ ]] || usage
[[ "$artifact_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || usage
[[ "$artifact_name" =~ ^release-payload-v[0-9]+\.[0-9]+\.[0-9]+-custom\.[0-9]+-[0-9]+-[0-9]+$ ]] || usage
[[ "${GITHUB_RUN_ID:-}" =~ ^[1-9][0-9]*$ ]] || fail "GITHUB_RUN_ID is required"
[[ "${GITHUB_RUN_ATTEMPT:-}" =~ ^[1-9][0-9]*$ ]] || fail "GITHUB_RUN_ATTEMPT is required"
[[ "${GITHUB_SHA:-}" =~ ^[0-9a-f]{40}$ ]] || fail "GITHUB_SHA is required"
[[ -n "${GITHUB_REF:-}" ]] || fail "GITHUB_REF is required"
[[ "${GITHUB_REPOSITORY:-}" == "o87110/sub2api-custom-public" ]] ||
  fail "release manifest repository is not the private custom repository"

jq \
  --arg repository "$GITHUB_REPOSITORY" \
  --arg tag_ref_oid "$tag_ref_oid" \
  --arg workflow ".github/workflows/release.yml" \
  --arg workflow_ref "$GITHUB_REF" \
  --arg workflow_commit "$GITHUB_SHA" \
  --arg event "${GITHUB_EVENT_NAME:-}" \
  --argjson run_id "$GITHUB_RUN_ID" \
  --argjson run_attempt "$GITHUB_RUN_ATTEMPT" \
  --argjson artifact_id "$artifact_id" \
  --arg artifact_digest "$artifact_digest" \
  --arg artifact_name "$artifact_name" \
  '
    {
      schema: "sub2api-custom-release/v1",
      repository: $repository,
      tag: .tag,
      tag_ref_oid: $tag_ref_oid,
      target_commit: .target_commit,
      producer: {
        workflow: $workflow,
        workflow_ref: $workflow_ref,
        workflow_commit: $workflow_commit,
        event: $event,
        run_id: $run_id,
        run_attempt: $run_attempt
      },
      payload_artifact: {
        id: $artifact_id,
        name: $artifact_name,
        digest: $artifact_digest
      },
      payload_content_sha256: .payload_content_sha256,
      assets: .assets,
      oci: {
        repository: "ghcr.io/o87110/sub2api-custom-public",
        index_digest: .oci.index_digest,
        index_media_type: .oci.index_media_type,
        manifests: .oci.manifests,
        tags: {
          multiarch: .tag,
          amd64: (.tag + "-amd64"),
          arm64: (.tag + "-arm64")
        }
      }
    }
  ' "$payload/build-metadata.json" > "$output"

jq -e '
  .schema == "sub2api-custom-release/v1" and
  (.assets | length) == 3 and
  (.oci.manifests | length) == 2
' "$output" >/dev/null
