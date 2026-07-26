#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  cat >&2 <<'EOF'
usage: validate-upgrade-report-maintenance.sh \
  --base-ref <trusted-main-ref> \
  --target-ref <pull-request-head> \
  --vendor-ref-prefix <trusted-vendor-ref-prefix>
EOF
  exit 2
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
base_ref=""
target_ref=""
vendor_ref_prefix=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --base-ref) base_ref="${2:-}"; shift 2 ;;
    --target-ref) target_ref="${2:-}"; shift 2 ;;
    --vendor-ref-prefix) vendor_ref_prefix="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[[ -n "$base_ref" && -n "$target_ref" ]] || usage
[[ "$vendor_ref_prefix" =~ ^refs/[A-Za-z0-9._/-]+/$ ]] || usage
git -C "$repo_root" cat-file -e "${base_ref}^{commit}" ||
  fail "trusted main ref is unavailable"
git -C "$repo_root" cat-file -e "${target_ref}^{commit}" ||
  fail "pull request Head is unavailable"

blob_content() {
  local ref="$1"
  local path="$2"
  local blob
  blob="$(
    git -C "$repo_root" ls-tree "$ref" -- "$path" |
      awk 'NR == 1 { print $3 } END { if (NR != 1) exit 1 }'
  )" || return 1
  [[ "$blob" =~ ^[0-9a-f]{40,64}$ ]] || return 1
  git -C "$repo_root" cat-file blob "$blob"
}

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
changes_file="$tmp_dir/changes.tsv"
rejected_file="$tmp_dir/rejected.tsv"
maintained_file="$tmp_dir/maintained.txt"
: > "$rejected_file"
: > "$maintained_file"

git -C "$repo_root" diff \
  --name-status \
  --no-renames \
  "$base_ref" \
  "$target_ref" \
  -- '.github/upgrades/*.md' > "$changes_file"

identity_pattern='^- (Base|Target|Custom base commit|Official target commit|Upgrade branch): '
while IFS=$'\t' read -r status path; do
  [[ -n "$status" && -n "$path" ]] || continue
  reason=""
  version=""
  if [[ "$status" != "M" ]]; then
    reason="upgrade reports cannot be added or deleted on a non-upgrade branch"
  elif [[ "$path" =~ ^\.github/upgrades/([0-9]+\.[0-9]+\.[0-9]+)\.md$ ]]; then
    version="${BASH_REMATCH[1]}"
  else
    reason="upgrade report path does not use the X.Y.Z.md format"
  fi

  base_content=""
  target_content=""
  if [[ -z "$reason" ]]; then
    base_content="$(
      blob_content "$base_ref" "$path" | tr -d '\r'
    )"
    target_content="$(
      blob_content "$target_ref" "$path" | tr -d '\r'
    )"
    base_identity="$(
      printf '%s\n' "$base_content" |
        grep -E "$identity_pattern" || true
    )"
    target_identity="$(
      printf '%s\n' "$target_content" |
        grep -E "$identity_pattern" || true
    )"
    base_identity_count="$(
      printf '%s\n' "$base_identity" |
        grep -Ec "$identity_pattern" || true
    )"
    target_identity_count="$(
      printf '%s\n' "$target_identity" |
        grep -Ec "$identity_pattern" || true
    )"
    if [[ "$base_identity_count" -ne 5 ||
          "$target_identity_count" -ne 5 ||
          "$base_identity" != "$target_identity" ]]; then
      reason="immutable upgrade identity fields changed"
    fi
  fi

  if [[ -z "$reason" ]]; then
    expected_target="- Target: \`v${version}\`"
    if ! grep -Fqx -- "$expected_target" <<<"$target_content"; then
      reason="upgrade target does not match the report filename"
    fi
  fi

  if [[ -z "$reason" ]]; then
    vendor_ref="${vendor_ref_prefix}vendor-${version}"
    vendor_commit="$(
      git -C "$repo_root" rev-parse \
        --verify "${vendor_ref}^{commit}" 2>/dev/null || true
    )"
    reported_official_commit="$(
      sed -nE \
        's/^- Official target commit: `([0-9a-f]{40})`$/\1/p' \
        <<<"$target_content"
    )"
    if [[ ! "$vendor_commit" =~ ^[0-9a-f]{40}$ ]]; then
      reason="trusted Vendor tag is unavailable"
    elif [[ "$reported_official_commit" != "$vendor_commit" ]]; then
      reason="official target commit does not match the trusted Vendor tag"
    elif ! git -C "$repo_root" merge-base \
      --is-ancestor "${vendor_ref}^{commit}" "${base_ref}^{commit}"; then
      reason="trusted Vendor tag is not integrated into main"
    fi
  fi

  if [[ -n "$reason" ]]; then
    printf '%s\t%s\t%s\n' "$status" "$path" "$reason" >> "$rejected_file"
  else
    printf '%s\n' "$path" >> "$maintained_file"
  fi
done < "$changes_file"

if [[ -s "$rejected_file" ]]; then
  echo \
    "ERROR: non-upgrade branches may only maintain existing released upgrade reports without changing their identity:" \
    >&2
  while IFS=$'\t' read -r status path reason; do
    printf '  - %s %s: %s\n' "$status" "$path" "$reason" >&2
  done < "$rejected_file"
  exit 1
fi

if [[ -s "$maintained_file" ]]; then
  while IFS= read -r path; do
    echo "Validated released upgrade report maintenance: $path"
  done < "$maintained_file"
else
  echo "No upgrade reports changed by this non-upgrade pull request."
fi
