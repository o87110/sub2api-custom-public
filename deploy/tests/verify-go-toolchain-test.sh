#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
mkdir -p "$tmp_dir/deploy/tests" "$tmp_dir/backend" "$tmp_dir/.github" "$tmp_dir/bin"
cp "$repo_root/deploy/tests/verify-go-toolchain.sh" "$tmp_dir/deploy/tests/"
cat > "$tmp_dir/bin/go" <<'EOF'
#!/bin/sh
[ "$*" = "env GOVERSION" ] || exit 1
printf '%s\n' "$TEST_GO_VERSION"
EOF
chmod 0755 "$tmp_dir/bin/go"
export PATH="$tmp_dir/bin:$PATH"
export TEST_GO_VERSION=go1.27.4
printf 'module fixture\n\ngo 1.27.4\n' > "$tmp_dir/backend/go.mod"
printf 'GO_VERSION=1.27.4\n' > "$tmp_dir/.github/custom-tool-versions.env"
verifier="$tmp_dir/deploy/tests/verify-go-toolchain.sh"
expect_failure() {
  if /bin/bash "$verifier" > "$tmp_dir/output.log" 2>&1; then
    echo "ERROR: toolchain verifier accepted $1" >&2
    exit 1
  fi
}
/bin/bash "$verifier" > "$tmp_dir/output.log"
TEST_GO_VERSION=go1.27.3 expect_failure "a different installed patch"
printf 'GO_VERSION=1.27.3\n' > "$tmp_dir/.github/custom-tool-versions.env"
expect_failure "module/pin disagreement"
printf 'GO_VERSION=1.27.4\nGO_VERSION=1.27.4\n' > "$tmp_dir/.github/custom-tool-versions.env"
expect_failure "duplicate pins"
printf 'GO_VERSION=1.27.4\n' > "$tmp_dir/.github/custom-tool-versions.env"
printf 'module fixture\n\ngo 1.27\n' > "$tmp_dir/backend/go.mod"
expect_failure "a version without an exact patch"
printf 'module fixture\n\ngo 1.27.4\ngo 1.27.4\n' > "$tmp_dir/backend/go.mod"
expect_failure "duplicate module versions"
printf 'Go toolchain verification fixtures passed (1 positive, 5 negative)\n'
