#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
classifier="$repo_root/deploy/tests/ci-impact.sh"
config="$repo_root/.github/ci-impact.yml"
tmp_root="$(mktemp -d)"
trap 'rm -rf "$tmp_root"' EXIT

git -C "$tmp_root" init -q
git -C "$tmp_root" config user.email test@example.invalid
git -C "$tmp_root" config user.name ci-impact-test
mkdir -p "$tmp_root/.github"
cp "$config" "$tmp_root/.github/ci-impact.yml"
git -C "$tmp_root" add .github/ci-impact.yml
git -C "$tmp_root" commit -qm base
base_sha="$(git -C "$tmp_root" rev-parse HEAD)"
initial_base_sha="$base_sha"

assert_case() {
  local name="$1" path="$2" expected_mode="$3" expected_frontend="$4" expected_backend="$5" expected_package="$6"
  git -C "$tmp_root" reset --hard -q "$base_sha"
  git -C "$tmp_root" clean -fdq
  [[ "$path" != .github/* ]] || { echo "test path cannot overwrite config" >&2; exit 1; }
  mkdir -p "$(dirname "$tmp_root/$path")"
  printf '%s\n' "$name" > "$tmp_root/$path"
  git -C "$tmp_root" add "$path"
  git -C "$tmp_root" commit -qm "$name"
  head_sha="$(git -C "$tmp_root" rev-parse HEAD)"
  output="$(
    CI_IMPACT_REPO_ROOT="$tmp_root" \
      "$classifier" classify \
      --base "$base_sha" \
      --head "$head_sha" \
      --config "$tmp_root/.github/ci-impact.yml" \
      --repo-root "$tmp_root"
  )"
  mode="$(sed -n 's/^mode=//p' <<<"$output")"
  frontend="$(sed -n 's/^frontend_changed=//p' <<<"$output")"
  backend="$(sed -n 's/^backend_changed=//p' <<<"$output")"
  packages="$(sed -n 's/^backend_packages=//p' <<<"$output")"
  [[ "$mode" == "$expected_mode" ]] || { echo "$name: expected mode=$expected_mode, got $mode" >&2; exit 1; }
  [[ "$frontend" == "$expected_frontend" ]] || { echo "$name: expected frontend_changed=$expected_frontend, got $frontend" >&2; exit 1; }
  [[ "$backend" == "$expected_backend" ]] || { echo "$name: expected backend_changed=$expected_backend, got $backend" >&2; exit 1; }
  [[ "$packages" == "$expected_package" ]] || { echo "$name: expected backend_packages='$expected_package', got '$packages'" >&2; exit 1; }
  base_sha="$head_sha"
}

assert_mixed() {
  local mixed_base="$initial_base_sha"
  git -C "$tmp_root" reset --hard -q "$mixed_base"
  git -C "$tmp_root" clean -fdq
  mkdir -p "$tmp_root/frontend/src/custom" "$tmp_root/backend/internal/custom/example"
  printf 'frontend\n' > "$tmp_root/frontend/src/custom/example.vue"
  printf 'backend\n' > "$tmp_root/backend/internal/custom/example/example.go"
  git -C "$tmp_root" add frontend/src/custom/example.vue backend/internal/custom/example/example.go
  git -C "$tmp_root" commit -qm mixed
  head_sha="$(git -C "$tmp_root" rev-parse HEAD)"
  output="$(CI_IMPACT_REPO_ROOT="$tmp_root" "$classifier" classify --base "$mixed_base" --head "$head_sha" --config "$tmp_root/.github/ci-impact.yml" --repo-root "$tmp_root")"
  [[ "$(sed -n 's/^frontend_changed=//p' <<<"$output")" == true ]]
  [[ "$(sed -n 's/^backend_changed=//p' <<<"$output")" == true ]]
  [[ "$(sed -n 's/^mode=//p' <<<"$output")" == fast ]]
  base_sha="$head_sha"
}

assert_case docs docs/guide.md fast false false ''
assert_case frontend frontend/src/custom/example.ts fast true false ''
assert_case backend backend/internal/custom/example/example.go fast false true './internal/custom/example'
assert_case high-risk backend/internal/service/example.go full false true './internal/service'
assert_case migration backend/migrations/999_test.sql full false true ''
assert_mixed
assert_case unknown scripts/new-tool.sh full false false ''

echo "CI impact classification tests passed"
