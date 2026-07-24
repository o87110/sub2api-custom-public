#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  echo "usage: $0 --commit <full-sha> | --worktree" >&2
  exit 2
}

mode=""
commit=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --commit)
      [[ $# -ge 2 ]] || usage
      mode=commit
      commit="$2"
      shift 2
      ;;
    --worktree)
      mode=worktree
      shift
      ;;
    *)
      usage
      ;;
  esac
done

[[ -n "$mode" ]] || usage

if [[ "$mode" == "commit" ]]; then
  [[ "$commit" =~ ^[0-9a-f]{40}$ ]] || fail "commit must be a full 40-character SHA"
  resolved="$(git -C "$repo_root" rev-parse --verify "${commit}^{commit}")"
  [[ "$resolved" == "$commit" ]] || fail "commit resolved to a different object: $resolved"
  git -C "$repo_root" rev-parse "${commit}^{tree}"
  exit 0
fi

tmp_dir="$(mktemp -d)"
tmp_index="$tmp_dir/index"
trap 'rm -rf "$tmp_dir"' EXIT

real_index="$(git -C "$repo_root" rev-parse --git-path index)"
case "$real_index" in
  /* | [A-Za-z]:*) ;;
  *) real_index="$repo_root/$real_index" ;;
esac

index_before="@absent"
if [[ -f "$real_index" ]]; then
  index_before="$(git hash-object "$real_index")"
fi
cached_before="$(git -C "$repo_root" diff --cached --raw HEAD)"

export GIT_INDEX_FILE="$tmp_index"
git -C "$repo_root" read-tree HEAD
git -C "$repo_root" add -A -- \
  . \
  ':(exclude)tools/**/__pycache__/**' \
  ':(exclude)tools/**/*.pyc'
tree="$(git -C "$repo_root" write-tree)"
unset GIT_INDEX_FILE

index_after="@absent"
if [[ -f "$real_index" ]]; then
  index_after="$(git hash-object "$real_index")"
fi
cached_after="$(git -C "$repo_root" diff --cached --raw HEAD)"

[[ "$index_before" == "$index_after" ]] ||
  fail "real Git index changed while constructing the candidate tree"
[[ "$cached_before" == "$cached_after" ]] ||
  fail "real staged state changed while constructing the candidate tree"
git -C "$repo_root" cat-file -e "${tree}^{tree}"
printf '%s\n' "$tree"
