#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
backend_root="$repo_root/backend"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

integration_packages=(
  ./internal/custom/subscriptioninventory
  ./internal/middleware
  ./internal/pkg/tlsfingerprint
  ./internal/repository
  ./internal/server/routes
)

usage() {
  echo "usage: $0 [run | check [--backend-root <directory>]]" >&2
  exit 2
}

command_name="${1:-run}"
shift $(( $# > 0 ? 1 : 0 ))
case "$command_name" in
  run)
    [[ $# -eq 0 ]] || usage
    ;;
  check)
    if [[ "${1:-}" == "--backend-root" ]]; then
      backend_root="${2:-}"
      shift 2
    fi
    [[ $# -eq 0 && -d "$backend_root" ]] || usage
    backend_root="$(cd "$backend_root" && pwd)"
    ;;
  *)
    usage
    ;;
esac

declared_file="$(mktemp)"
discovered_file="$(mktemp)"
cleanup() {
  rm -f "$declared_file" "$discovered_file"
}
trap cleanup EXIT

printf '%s\n' "${integration_packages[@]#./}" | sort -u > "$declared_file"

list_test_files() {
  if [[ "$backend_root" == "$repo_root/backend" ]]; then
    git -C "$repo_root" grep \
      --untracked \
      --exclude-standard \
      -Ilz \
      -E '^//(go:build[[:space:]]|[[:space:]]+\+build).*integration' \
      -- 'backend/**/*_test.go' |
      while IFS= read -r -d '' relative_file; do
        printf '%s\0' "$repo_root/$relative_file"
      done
  else
    find "$backend_root" -type f -name '*_test.go' -print0
  fi
}

while IFS= read -r -d '' test_file; do
  build_constraint=""
  while IFS= read -r source_line; do
    source_line="${source_line%$'\r'}"
    [[ "$source_line" != package[[:space:]]* ]] || break
    if [[ "$source_line" == '//go:build '* ]]; then
      build_constraint="$source_line"
      break
    fi
  done < "$test_file"
  [[ -n "$build_constraint" ]] ||
    fail "integration test must use the exact '//go:build integration' constraint: ${test_file#$repo_root/}"
  if [[ "$build_constraint" =~ (^|[^[:alnum:]_])integration([^[:alnum:]_]|$) ]]; then
    [[ "$build_constraint" == '//go:build integration' ]] ||
      fail "integration test must use the exact '//go:build integration' constraint: ${test_file#$repo_root/}"
    package_dir="${test_file%/*}"
    printf '%s\n' "${package_dir#$backend_root/}" >> "$discovered_file"
  fi
done < <(list_test_files)

sort -u -o "$discovered_file" "$discovered_file"
if ! diff -u "$declared_file" "$discovered_file"; then
  fail "declared integration packages do not match integration-tagged test files"
fi

echo "Verified Integration packages:"
sed 's#^#  ./#' "$declared_file"

if [[ "$command_name" == "check" ]]; then
  exit 0
fi

(
  cd "$backend_root"
  go test -tags=integration "${integration_packages[@]}"
)
