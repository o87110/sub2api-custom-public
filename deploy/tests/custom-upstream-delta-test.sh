#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
baseline_file="$repo_root/.github/custom-upstream-baseline.env"
ledger="$repo_root/.github/custom-upstream-delta.tsv"
shadow_map="$repo_root/.github/upstream-shadowed-sources.tsv"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  echo "usage: $0 --candidate-tree <tree>" >&2
  exit 2
}

candidate_tree=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --candidate-tree)
      candidate_tree="${2:-}"
      shift 2
      ;;
    *)
      usage
      ;;
  esac
done

[[ "$candidate_tree" =~ ^[0-9a-f]{40,64}$ ]] || usage
git -C "$repo_root" cat-file -e "${candidate_tree}^{tree}" ||
  fail "candidate tree does not exist: $candidate_tree"
[[ -s "$baseline_file" ]] || fail "explicit upstream baseline file is missing"
[[ -s "$ledger" ]] || fail "custom upstream delta ledger is missing"
[[ -s "$shadow_map" ]] || fail "upstream shadow map is missing"

# shellcheck disable=SC1090
source "$baseline_file"
[[ "${CUSTOM_UPSTREAM_BASE_REF:-}" == "vendor-0.1.162^{commit}" ]] ||
  fail "baseline ref must remain explicit"
[[ "${CUSTOM_UPSTREAM_BASE_COMMIT:-}" =~ ^[0-9a-f]{40}$ ]] ||
  fail "baseline commit is invalid"
resolved_base="$(git -C "$repo_root" rev-parse --verify "$CUSTOM_UPSTREAM_BASE_REF")"
[[ "$resolved_base" == "$CUSTOM_UPSTREAM_BASE_COMMIT" ]] ||
  fail "baseline ref resolves to $resolved_base, expected $CUSTOM_UPSTREAM_BASE_COMMIT"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
rows="$tmp_dir/rows.tsv"
actual="$tmp_dir/actual.tsv"
seen_actual="$tmp_dir/seen-actual.txt"
: > "$seen_actual"

expected_header=$'path\tinitial_status\tdecision\texpected_status\tcategory\tbase_blob\tfinal_blob\tshadow_source\tshadow_target\tverification\treason'
IFS= read -r actual_header < "$ledger"
[[ "$actual_header" == "$expected_header" ]] || fail "delta ledger header is invalid"

awk -F '\t' '
  NR == 1 { next }
  /^[[:space:]]*$/ { next }
  NF != 11 {
    printf "invalid delta ledger line %d: expected 11 TSV fields\n", NR > "/dev/stderr"
    failed = 1
    next
  }
  {
    for (i = 1; i <= 11; i++) {
      if ($i == "") {
        printf "invalid delta ledger line %d: field %d is empty\n", NR, i > "/dev/stderr"
        failed = 1
      }
    }
    print
  }
  END { exit failed }
' "$ledger" > "$rows"

cut -f1 "$rows" > "$tmp_dir/paths.txt"
LC_ALL=C sort "$tmp_dir/paths.txt" > "$tmp_dir/sorted-paths.txt"
cmp -s "$tmp_dir/paths.txt" "$tmp_dir/sorted-paths.txt" ||
  fail "delta ledger paths must be sorted"
duplicate_paths="$(uniq -d "$tmp_dir/sorted-paths.txt")"
[[ -z "$duplicate_paths" ]] || fail "duplicate delta ledger paths: $duplicate_paths"

git -C "$repo_root" diff \
  --name-status \
  --no-renames \
  "$CUSTOM_UPSTREAM_BASE_COMMIT" \
  "$candidate_tree" > "$actual"
if grep -Ev $'^[AMD]\t' "$actual" | grep -q .; then
  fail "candidate diff contains a status other than A/M/D"
fi

is_sha() {
  [[ "$1" =~ ^[0-9a-f]{40,64}$ ]]
}

validate_path() {
  case "$1" in
    "" | . | .. | /* | [A-Za-z]:* | *\\* | ./* | */./* | ../* | */../* | */.. | *//*)
      fail "ledger path is not normalized repository-relative: $1"
      ;;
  esac
}

actual_status() {
  awk -F '\t' -v path="$1" '$2 == path { print $1 }' "$actual"
}

declare -A base_blobs=()
declare -A candidate_blobs=()
while IFS= read -r -d '' record; do
  metadata="${record%%$'\t'*}"
  path="${record#*$'\t'}"
  base_blobs["$path"]="${metadata##* }"
done < <(git -C "$repo_root" ls-tree -rz "$CUSTOM_UPSTREAM_BASE_COMMIT")
while IFS= read -r -d '' record; do
  metadata="${record%%$'\t'*}"
  path="${record#*$'\t'}"
  candidate_blobs["$path"]="${metadata##* }"
done < <(git -C "$repo_root" ls-tree -rz "$candidate_tree")

blob_at() {
  case "$1" in
    "$CUSTOM_UPSTREAM_BASE_COMMIT")
      printf '%s' "${base_blobs[$2]:-}"
      ;;
    "$candidate_tree")
      printf '%s' "${candidate_blobs[$2]:-}"
      ;;
    *)
      fail "unexpected Blob lookup object: $1"
      ;;
  esac
}

thin_bridge_allowed() {
  case "$1" in
    backend/cmd/server/wire.go | \
      backend/internal/handler/admin/system_handler.go | \
      backend/internal/handler/admin/system_handler_test.go | \
      backend/internal/handler/openai_gateway_handler.go | \
      backend/internal/handler/wire.go | \
      backend/internal/service/content_moderation.go | \
      frontend/src/components/layout/AppSidebar.vue | \
      frontend/src/router/index.ts)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

while IFS=$'\t' read -r \
  path initial_status decision expected_status category base_blob final_blob \
  shadow_source shadow_target verification reason; do
  validate_path "$path"
  [[ "$initial_status" =~ ^[AMD]$ ]] ||
    fail "invalid initial status for $path: $initial_status"
  case "$category" in
    whole-file-restore | official-thin-bridge | generated | fixed-path | \
      custom-backend | custom-frontend | custom-test | custom-docs-ops | \
      explicit-exception)
      ;;
    *)
      fail "invalid category for $path: $category"
      ;;
  esac

  base_actual="$(blob_at "$CUSTOM_UPSTREAM_BASE_COMMIT" "$path")"
  [[ -n "$base_actual" ]] || base_actual="@absent"
  [[ "$base_blob" == "$base_actual" ]] ||
    fail "baseline Blob mismatch for $path"

  status="$(actual_status "$path")"
  if [[ "$decision" == "restore" ]]; then
    [[ "$expected_status" == "RESTORED" && "$category" == "whole-file-restore" ]] ||
      fail "restored path has invalid status/category: $path"
    [[ -z "$status" ]] || fail "restored path still differs from baseline: $path"
    is_sha "$base_blob" || fail "restored path must exist in the baseline: $path"
    [[ "$final_blob" == "$base_blob" ]] ||
      fail "restored final Blob must equal baseline Blob: $path"
    candidate_blob="$(blob_at "$candidate_tree" "$path")"
    [[ "$candidate_blob" == "$base_blob" ]] ||
      fail "restored candidate Blob differs from baseline: $path"
  elif [[ "$decision" == "keep" ]]; then
    [[ "$expected_status" =~ ^[AMD]$ ]] ||
      fail "kept path has invalid expected status: $path"
    [[ "$status" == "$expected_status" ]] ||
      fail "actual status for $path is ${status:-absent}, expected $expected_status"
    candidate_blob="$(blob_at "$candidate_tree" "$path")"
    [[ -n "$candidate_blob" ]] || candidate_blob="@absent"
    if [[ "$final_blob" == "@self" ]]; then
      [[ "$path" == ".github/custom-upstream-delta.tsv" ]] ||
        fail "@self is only allowed for the delta ledger"
      [[ "$candidate_blob" != "@absent" ]] ||
        fail "delta ledger is absent from candidate tree"
    else
      [[ "$final_blob" == "$candidate_blob" ]] ||
        fail "final Blob mismatch for $path"
      if [[ "$final_blob" != "@absent" ]]; then
        is_sha "$final_blob" || fail "invalid final Blob for $path"
      fi
    fi
    printf '%s\n' "$path" >> "$seen_actual"
  else
    fail "invalid decision for $path: $decision"
  fi

  if [[ "$category" == "official-thin-bridge" ]]; then
    thin_bridge_allowed "$path" ||
      fail "official thin bridge is outside the exact allowlist: $path"
  fi

  if [[ "$shadow_source" != "-" || "$shadow_target" != "-" ]]; then
    [[ "$shadow_source" != "-" && "$shadow_target" != "-" ]] ||
      fail "shadow source and target must be declared together: $path"
    awk -F '\t' -v source="$shadow_source" -v target="$shadow_target" '
      $1 !~ /^#/ && index("|" $1 "|", "|" source "|") &&
      index("|" $2 "|", "|" target "|") { found = 1 }
      END { exit !found }
    ' "$shadow_map" || fail "ledger shadow relation is not in the exact shadow map: $path"
  fi
done < "$rows"

LC_ALL=C sort -u "$seen_actual" -o "$seen_actual"
cut -f2 "$actual" | LC_ALL=C sort -u > "$tmp_dir/actual-paths.txt"
if ! cmp -s "$seen_actual" "$tmp_dir/actual-paths.txt"; then
  echo "ERROR: actual candidate diff and ledger differ:" >&2
  comm -3 "$seen_actual" "$tmp_dir/actual-paths.txt" >&2
  exit 1
fi

echo "custom upstream delta ledger passed ($(wc -l < "$rows" | tr -d ' ') decisions, baseline $CUSTOM_UPSTREAM_BASE_COMMIT, tree $candidate_tree)"
