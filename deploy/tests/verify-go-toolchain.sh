#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
module_version="$(sed -n 's/^go //p' "$repo_root/backend/go.mod")"
pinned_version="$(sed -n 's/^GO_VERSION=//p' "$repo_root/.github/custom-tool-versions.env")"

if [[ ! "$module_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ||
      "$module_version" != "$pinned_version" ]]; then
  echo "ERROR: backend/go.mod must match one exact pinned Go patch version" >&2
  exit 1
fi
actual_version="$(go env GOVERSION)"
if [[ "$actual_version" != "go${module_version}" ]]; then
  echo "ERROR: Go toolchain is $actual_version, expected go${module_version}" >&2
  exit 1
fi
printf 'Go toolchain verified: %s\n' "$actual_version"
