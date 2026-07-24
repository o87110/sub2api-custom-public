#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  echo "usage: $0 --tag <vX.Y.Z-custom.N> --control-commit <sha> --output <github-output-file>" >&2
  exit 2
}

tag=""
control_commit=""
output=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag) tag="${2:-}"; shift 2 ;;
    --control-commit) control_commit="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-custom\.[0-9]+$ ]] || usage
[[ "$control_commit" =~ ^[0-9a-f]{40}$ ]] || usage
[[ -n "$output" ]] || usage
[[ "${GITHUB_REPOSITORY:-}" == "o87110/sub2api-custom-public" ]] ||
  fail "Release preflight only permits the private custom repository"
command -v gh >/dev/null 2>&1 || fail "gh is required"
command -v jq >/dev/null 2>&1 || fail "jq is required"

ref_json="$(gh api "repos/${GITHUB_REPOSITORY}/git/ref/tags/${tag}")"
tag_ref_oid="$(jq -er '.object.sha | select(test("^[0-9a-f]{40,64}$"))' <<<"$ref_json")"
tag_object_type="$(jq -er '.object.type | select(. == "tag" or . == "commit")' <<<"$ref_json")"

local_tag_ref="refs/tags/${tag}"
git show-ref --verify --quiet "$local_tag_ref" ||
  fail "checked-out repository lacks the requested Tag"
local_tag_oid="$(git rev-parse "$local_tag_ref")"
[[ "$local_tag_oid" == "$tag_ref_oid" ]] ||
  fail "remote Tag ref OID changed while fetching $tag"
target_commit="$(git rev-parse "${local_tag_ref}^{commit}")"
[[ "$target_commit" =~ ^[0-9a-f]{40}$ ]] || fail "Tag does not peel to a commit"
[[ "$(git rev-parse HEAD)" == "$target_commit" ]] ||
  fail "checkout HEAD does not equal the Tag commit"

find_latest_ci() {
  local commit="$1"
  gh run list \
    --repo "$GITHUB_REPOSITORY" \
    --workflow backend-ci.yml \
    --commit "$commit" \
    --limit 50 \
    --json databaseId,status,conclusion,headSha,headBranch,event,createdAt \
    --jq '
      map(select(
        (.event == "push" or .event == "workflow_dispatch") and
        .headBranch == "main" and
        .headSha == "'"$commit"'"
      )) |
      sort_by(.createdAt, .databaseId) |
      if length == 0 then
        ""
      else
        last |
        [.databaseId, .status, .conclusion, .headSha, .headBranch, .event] |
        @tsv
      end
    '
}

verify_ci_boundaries() {
  local run_id="$1"
  local boundary_state boundary_status boundary_conclusion
  boundary_state="$(
    gh run view "$run_id" \
      --repo "$GITHUB_REPOSITORY" \
      --json jobs \
      --jq '.jobs | map(select(.name == "boundaries")) | if length == 1 then [.[0].status, .[0].conclusion] | @tsv else "" end'
  )"
  IFS=$'\t' read -r boundary_status boundary_conclusion <<<"$boundary_state"
  [[ "$boundary_status" == "completed" && "$boundary_conclusion" == "success" ]] ||
    fail "CI run $run_id lacks the successful boundaries job"
}

require_successful_ci() {
  local commit="$1"
  local label="$2"
  local run_state run_id status conclusion head_sha head_branch event_type
  run_state="$(find_latest_ci "$commit")"
  [[ -n "$run_state" ]] ||
    fail "no accepted main CI exists for $label"
  IFS=$'\t' read -r \
    run_id status conclusion head_sha head_branch event_type <<<"$run_state"
  [[ "$run_id" =~ ^[1-9][0-9]*$ &&
     "$status" == "completed" &&
     "$conclusion" == "success" &&
     "$head_sha" == "$commit" &&
     "$head_branch" == "main" &&
     ( "$event_type" == "push" || "$event_type" == "workflow_dispatch" ) ]] ||
    fail "latest accepted CI run ${run_id:-missing} is ${status:-missing}/${conclusion:-missing} for ${head_branch:-missing}@${head_sha:-missing}; expected success for $label"
  verify_ci_boundaries "$run_id"
  printf '%s\n' "$run_id"
}

ci_state="$(require_successful_ci "$target_commit" "target commit $target_commit")"

control_ci_state="$ci_state"
if [[ "$control_commit" != "$target_commit" ]]; then
  control_ci_state="$(
    require_successful_ci "$control_commit" "control commit $control_commit"
  )"
fi

releases="$(
  gh api --paginate "repos/${GITHUB_REPOSITORY}/releases?per_page=100" |
    jq -s --arg tag "$tag" 'add | map(select(.tag_name == $tag))'
)"
release_count="$(jq 'length' <<<"$releases")"
[[ "$release_count" -le 1 ]] || fail "multiple Releases exist for $tag"

release_state=none
release_id=""
manifest_asset_id=""
if [[ "$release_count" -eq 1 ]]; then
  release="$(jq '.[0]' <<<"$releases")"
  release_id="$(jq -er '.id | select(. > 0)' <<<"$release")"
  release_tag="$(jq -er '.tag_name' <<<"$release")"
  [[ "$release_tag" == "$tag" ]] || fail "Release tag metadata is inconsistent"
  is_draft="$(
    jq -r '
      if (.draft | type) == "boolean" then
        .draft
      else
        error("Release draft metadata is not boolean")
      end
    ' <<<"$release"
  )"
  immutable="$(jq -r '.immutable // false' <<<"$release")"
  if [[ "$is_draft" == "false" || "$immutable" == "true" ]]; then
    [[ "$is_draft" == "false" ]] || fail "immutable Draft Release is invalid"
    release_state=published
  else
    release_state=draft
  fi

  mapfile -t manifest_assets < <(
    jq -r '.assets[] | select(.name == "release-manifest.json") | .id' <<<"$release"
  )
  [[ "${#manifest_assets[@]}" -le 1 ]] ||
    fail "Release contains duplicate release-manifest.json assets"
  if [[ "${#manifest_assets[@]}" -eq 1 ]]; then
    manifest_asset_id="${manifest_assets[0]}"
  elif [[ "$release_state" == "published" ]]; then
    fail "published Release has no authoritative release manifest"
  fi
fi

ref_json_after="$(gh api "repos/${GITHUB_REPOSITORY}/git/ref/tags/${tag}")"
tag_ref_oid_after="$(jq -er '.object.sha' <<<"$ref_json_after")"
[[ "$tag_ref_oid_after" == "$tag_ref_oid" ]] ||
  fail "Tag ref OID changed during Release preflight"
[[ "$(git rev-parse "$local_tag_ref")" == "$tag_ref_oid" ]] ||
  fail "local Tag ref OID changed during Release preflight"
[[ "$(git rev-parse "${local_tag_ref}^{commit}")" == "$target_commit" ]] ||
  fail "Tag peeled commit changed during Release preflight"

{
  echo "tag=$tag"
  echo "tag_ref_oid=$tag_ref_oid"
  echo "tag_object_type=$tag_object_type"
  echo "target_commit=$target_commit"
  echo "ci_run_id=$ci_state"
  echo "control_ci_run_id=$control_ci_state"
  echo "release_state=$release_state"
  echo "release_id=$release_id"
  echo "manifest_asset_id=$manifest_asset_id"
} >> "$output"
