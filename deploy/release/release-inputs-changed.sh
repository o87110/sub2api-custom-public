#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  echo "usage: $0 --base <git-ref> --target <git-ref> --output <file>" >&2
  exit 2
}

base_ref=""
target_ref=""
output=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --base) base_ref="${2:-}"; shift 2 ;;
    --target) target_ref="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[[ -n "$base_ref" && -n "$target_ref" && -n "$output" ]] || usage

base_commit="$(git -C "$repo_root" rev-parse --verify "${base_ref}^{commit}")" ||
  fail "release input base ref is unavailable: $base_ref"
target_commit="$(git -C "$repo_root" rev-parse --verify "${target_ref}^{commit}")" ||
  fail "release input target ref is unavailable: $target_ref"
[[ "$base_commit" =~ ^[0-9a-f]{40}$ && "$target_commit" =~ ^[0-9a-f]{40}$ ]] ||
  fail "release input refs must resolve to full commit IDs"
git -C "$repo_root" merge-base --is-ancestor "$base_commit" "$target_commit" ||
  fail "release input base $base_ref is not an ancestor of $target_ref"

release_inputs=(
  backend
  frontend
  docs/legal
  docs/custom/OPERATIONS_CN.md
  deploy/docker-entrypoint.sh
  deploy/release/Dockerfile
  deploy/release/build-release-payload.sh
  deploy/release/frontend-dist.sh
  deploy/tests/install-custom-tools.sh
  .goreleaser.yaml
  .github/custom-tool-versions.env
  .github/custom-upstream-baseline.env
  Makefile
  LICENSE
)

changed_file="$(mktemp)"
cleanup() {
  rm -f "$changed_file"
}
trap cleanup EXIT

git -C "$repo_root" diff \
  --name-only \
  --diff-filter=ACDMRTUXB \
  "$base_commit" "$target_commit" \
  -- "${release_inputs[@]}" > "$changed_file"

release_required=false
release_reason=no-release-input-changes
if [[ -s "$changed_file" ]]; then
  release_required=true
  release_reason=release-inputs-changed
fi

{
  echo "release_required=$release_required"
  echo "release_reason=$release_reason"
  echo "release_base_commit=$base_commit"
  echo "release_target_commit=$target_commit"
} >> "$output"

if [[ -s "$changed_file" ]]; then
  echo "Release-relevant paths changed between ${base_ref} and ${target_ref}:"
  sed 's/^/  - /' "$changed_file"
else
  echo "No Release-relevant paths changed between ${base_ref} and ${target_ref}."
fi
