#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
baseline_file="${CUSTOM_UPSTREAM_BASELINE_FILE:-$repo_root/.github/custom-upstream-baseline.env}"
exceptions="$repo_root/.github/custom-database-exceptions.tsv"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  cat >&2 <<'EOF'
usage:
  custom-database-boundary-test.sh --mode final --candidate-tree <tree>
  custom-database-boundary-test.sh --mode upgrade \
    --base-tree <tree> --official-tree <tree> --custom-tree <tree> \
    --candidate-tree <tree> --official-tag <vX.Y.Z> --head-sha <sha> \
    [--approved-base-commit <sha> --approved-official-tag <tag> \
     --approved-head-sha <sha> --approved-fingerprint <sha256>]
EOF
  exit 2
}

mode=""
candidate_tree=""
base_tree=""
official_tree=""
custom_tree=""
official_tag=""
head_sha=""
approved_base_commit=""
approved_official_tag=""
approved_head_sha=""
approved_fingerprint=""
report_only=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode) mode="${2:-}"; shift 2 ;;
    --candidate-tree) candidate_tree="${2:-}"; shift 2 ;;
    --base-tree) base_tree="${2:-}"; shift 2 ;;
    --official-tree) official_tree="${2:-}"; shift 2 ;;
    --custom-tree) custom_tree="${2:-}"; shift 2 ;;
    --official-tag) official_tag="${2:-}"; shift 2 ;;
    --head-sha) head_sha="${2:-}"; shift 2 ;;
    --approved-base-commit) approved_base_commit="${2:-}"; shift 2 ;;
    --approved-official-tag) approved_official_tag="${2:-}"; shift 2 ;;
    --approved-head-sha) approved_head_sha="${2:-}"; shift 2 ;;
    --approved-fingerprint) approved_fingerprint="${2:-}"; shift 2 ;;
    --report-only) report_only=true; shift ;;
    *) usage ;;
  esac
done

[[ -s "$baseline_file" ]] || fail "explicit custom baseline file is missing"
mapfile -t baseline_lines < "$baseline_file"
[[ "${#baseline_lines[@]}" -eq 2 ]] ||
  fail "baseline file must contain exactly two assignments"
baseline_ref_line="${baseline_lines[0]%$'\r'}"
baseline_commit_line="${baseline_lines[1]%$'\r'}"
[[ "$baseline_ref_line" == CUSTOM_UPSTREAM_BASE_REF=* ]] ||
  fail "baseline ref assignment is missing"
[[ "$baseline_commit_line" == CUSTOM_UPSTREAM_BASE_COMMIT=* ]] ||
  fail "baseline commit assignment is missing"
CUSTOM_UPSTREAM_BASE_REF="${baseline_ref_line#CUSTOM_UPSTREAM_BASE_REF=}"
CUSTOM_UPSTREAM_BASE_COMMIT="${baseline_commit_line#CUSTOM_UPSTREAM_BASE_COMMIT=}"
[[ "${CUSTOM_UPSTREAM_BASE_REF:-}" =~ ^vendor-[0-9]+\.[0-9]+\.[0-9]+\^\{commit\}$ ]] ||
  fail "custom baseline ref must be an explicit vendor-X.Y.Z commit"
[[ "${CUSTOM_UPSTREAM_BASE_COMMIT:-}" =~ ^[0-9a-f]{40}$ ]] ||
  fail "custom baseline commit is invalid"
resolved_base="$(git -C "$repo_root" rev-parse --verify "$CUSTOM_UPSTREAM_BASE_REF")"
[[ "$resolved_base" == "$CUSTOM_UPSTREAM_BASE_COMMIT" ]] ||
  fail "custom baseline ref resolves to $resolved_base, expected $CUSTOM_UPSTREAM_BASE_COMMIT"

validate_tree() {
  [[ "$1" =~ ^[0-9a-f]{40,64}$ ]] || fail "invalid tree/object ID: $1"
  git -C "$repo_root" cat-file -e "$1^{tree}" ||
    fail "Git tree/object does not exist: $1"
}

run_tool() {
  (
    cd "$repo_root/backend"
    go run ./cmd/custom-database-boundary "$@"
  )
}

case "$mode" in
  final)
    [[ -n "$candidate_tree" ]] || usage
    validate_tree "$candidate_tree"
    run_tool \
      --repo "$repo_root" \
      --mode final \
      --base "$CUSTOM_UPSTREAM_BASE_COMMIT" \
      --target "$candidate_tree" \
      --baseline-commit "$CUSTOM_UPSTREAM_BASE_COMMIT" \
      --exceptions "$exceptions"
    ;;
  upgrade)
    for object in "$base_tree" "$official_tree" "$custom_tree" "$candidate_tree"; do
      [[ -n "$object" ]] || usage
      validate_tree "$object"
    done
    [[ "$official_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || usage
    [[ "$head_sha" =~ ^[0-9a-f]{40}$ ]] || usage

    tmp_dir="$(mktemp -d)"
    trap 'rm -rf "$tmp_dir"' EXIT
    run_tool \
      --repo "$repo_root" \
      --mode report \
      --base "$base_tree" \
      --target "$official_tree" \
      --manifest "$tmp_dir/official.tsv" >/dev/null
    run_tool \
      --repo "$repo_root" \
      --mode report \
      --base "$custom_tree" \
      --target "$candidate_tree" \
      --manifest "$tmp_dir/custom.tsv" >/dev/null
    {
      sed 's/^/official\t/' "$tmp_dir/official.tsv"
      sed 's/^/custom\t/' "$tmp_dir/custom.tsv"
    } | sort > "$tmp_dir/combined.tsv"
    fingerprint="$(sha256sum "$tmp_dir/combined.tsv" | awk '{print $1}')"
    changed=false
    if [[ -s "$tmp_dir/combined.tsv" ]]; then
      changed=true
      if [[ "$report_only" != "true" ]]; then
        [[ "$approved_base_commit" == "$CUSTOM_UPSTREAM_BASE_COMMIT" ]] ||
          fail "database approval must bind the explicit baseline commit $CUSTOM_UPSTREAM_BASE_COMMIT"
        [[ "$approved_official_tag" == "$official_tag" ]] ||
          fail "database approval must bind official tag $official_tag"
        [[ "$approved_head_sha" == "$head_sha" ]] ||
          fail "database approval must bind upgrade Head $head_sha"
        [[ "$approved_fingerprint" == "$fingerprint" ]] ||
          fail "database approval must bind normalized fingerprint $fingerprint"
      fi
    fi
    printf 'database_changed=%s\n' "$changed"
    printf 'database_fingerprint=%s\n' "$fingerprint"
    ;;
  *)
    usage
    ;;
esac
