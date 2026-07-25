#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
entrypoint="$repo_root/deploy/docker-entrypoint.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

cat > "$tmp_dir/version-stub.go" <<'EOF'
package main

import (
	"flag"
	"fmt"
	"time"
)

var version = "invalid"
var mode = "version"

func main() {
	showVersion := flag.Bool("version", false, "show version")
	flag.Parse()
	if *showVersion {
		switch mode {
		case "hang":
			for {
				time.Sleep(time.Hour)
			}
		case "flood":
			for {
				fmt.Print("0123456789abcdef")
			}
		}
		fmt.Printf("Sub2API %s (commit: test, built: test)\n", version)
	}
}
EOF

build_stub() {
  local output=$1
  local version=$2
  local mode=${3:-version}
  go build -ldflags "-X main.version=$version -X main.mode=$mode" -o "$output" "$tmp_dir/version-stub.go"
}

assert_same() {
  cmp -s "$1" "$2" || fail "$1 does not match $2"
}

build_stub "$tmp_dir/v6" "0.1.162-custom.6"
build_stub "$tmp_dir/v7" "v0.1.162-custom.7"
build_stub "$tmp_dir/v8" "0.1.162-custom.8"
build_stub "$tmp_dir/official-v163" "0.1.163"
build_stub "$tmp_dir/invalid-version" "development"
build_stub "$tmp_dir/hanging-version" "0.1.162-custom.7" "hang"
build_stub "$tmp_dir/flooding-version" "0.1.162-custom.7" "flood"

export ENTRYPOINT_SOURCE_ONLY=true
export BINARY_VERSION_TIMEOUT_SECONDS=1
export BINARY_VERSION_MAX_OUTPUT_BYTES=1024
export IMAGE_BINARY="$tmp_dir/v7"
export LEGACY_IMAGE_BINARY="$tmp_dir/legacy"
export RUNTIME_BINARY="$tmp_dir/runtime/sub2api"
export RUNTIME_DIR="$tmp_dir/runtime"
mkdir -p "$RUNTIME_DIR"
# shellcheck source=../docker-entrypoint.sh
source "$entrypoint"

reconcile_runtime_binary >/dev/null
assert_same "$RUNTIME_BINARY" "$tmp_dir/v7"

cp "$tmp_dir/v6" "$RUNTIME_BINARY"
reconcile_runtime_binary >/dev/null
assert_same "$RUNTIME_BINARY" "$tmp_dir/v7"
assert_same "$RUNTIME_BINARY.backup" "$tmp_dir/v6"

cp "$tmp_dir/v8" "$RUNTIME_BINARY"
rm -f "$RUNTIME_BINARY.backup"
reconcile_runtime_binary >/dev/null
assert_same "$RUNTIME_BINARY" "$tmp_dir/v8"
[[ ! -e "$RUNTIME_BINARY.backup" ]] || fail "newer runtime must not be replaced or backed up"

cp "$tmp_dir/official-v163" "$RUNTIME_BINARY"
cp "$tmp_dir/v6" "$RUNTIME_BINARY.backup"
reconcile_runtime_binary >/dev/null
assert_same "$RUNTIME_BINARY" "$tmp_dir/v7"
[[ ! -e "$RUNTIME_BINARY.backup" ]] || fail "non-custom runtime must not become a rollback backup"

cp "$tmp_dir/v7" "$RUNTIME_BINARY"
cp "$tmp_dir/v6" "$tmp_dir/existing-backup"
cp "$tmp_dir/existing-backup" "$RUNTIME_BINARY.backup"
reconcile_runtime_binary >/dev/null
assert_same "$RUNTIME_BINARY" "$tmp_dir/v7"
assert_same "$RUNTIME_BINARY.backup" "$tmp_dir/existing-backup"

cp "$tmp_dir/invalid-version" "$RUNTIME_BINARY"
cp "$RUNTIME_BINARY" "$tmp_dir/runtime-before-invalid"
if reconcile_runtime_binary >/dev/null 2>&1; then
  fail "runtime with an unsupported version was accepted"
fi
assert_same "$RUNTIME_BINARY" "$tmp_dir/runtime-before-invalid"

cp "$tmp_dir/v6" "$RUNTIME_BINARY"
IMAGE_BINARY="$tmp_dir/invalid-version"
if reconcile_runtime_binary >/dev/null 2>&1; then
  fail "image with an unsupported version was accepted"
fi
assert_same "$RUNTIME_BINARY" "$tmp_dir/v6"

cp "$tmp_dir/official-v163" "$RUNTIME_BINARY"
cp "$tmp_dir/v6" "$RUNTIME_BINARY.backup"
cp "$RUNTIME_BINARY.backup" "$tmp_dir/backup-before-failed-repair"
IMAGE_BINARY="$tmp_dir/invalid-version"
if reconcile_runtime_binary >/dev/null 2>&1; then
  fail "invalid image unexpectedly repaired a non-custom runtime"
fi
assert_same "$RUNTIME_BINARY" "$tmp_dir/official-v163"
assert_same "$RUNTIME_BINARY.backup" "$tmp_dir/backup-before-failed-repair"

IMAGE_BINARY="$tmp_dir/hanging-version"
if binary_version "$IMAGE_BINARY" "hanging image" >/dev/null 2>&1; then
  fail "hanging version probe was accepted"
fi
if pgrep -f "$tmp_dir/hanging-version" >/dev/null 2>&1; then
  fail "hanging version probe left a child process"
fi

IMAGE_BINARY="$tmp_dir/flooding-version"
if binary_version "$IMAGE_BINARY" "flooding image" >/dev/null 2>&1; then
  fail "unbounded version output was accepted"
fi
if pgrep -f "$tmp_dir/flooding-version" >/dev/null 2>&1; then
  fail "flooding version probe left a child process"
fi

printf '#!/bin/sh\necho not-elf\n' > "$tmp_dir/not-elf"
chmod +x "$tmp_dir/not-elf"
IMAGE_BINARY="$tmp_dir/not-elf"
if reconcile_runtime_binary >/dev/null 2>&1; then
  fail "non-ELF image was accepted"
fi
assert_same "$RUNTIME_BINARY" "$tmp_dir/official-v163"
assert_same "$RUNTIME_BINARY.backup" "$tmp_dir/backup-before-failed-repair"

rm -f "$RUNTIME_BINARY" "$RUNTIME_BINARY.backup"
IMAGE_BINARY="$tmp_dir/official-v163"
reconcile_runtime_binary >/dev/null
assert_same "$RUNTIME_BINARY" "$tmp_dir/official-v163"

cp "$tmp_dir/v6" "$tmp_dir/runtime-symlink-target"
cp "$tmp_dir/runtime-symlink-target" "$tmp_dir/runtime-symlink-target.before"
rm -f "$RUNTIME_BINARY" "$RUNTIME_BINARY.backup"
ln -s "$tmp_dir/runtime-symlink-target" "$RUNTIME_BINARY"
IMAGE_BINARY="$tmp_dir/v7"
LEGACY_IMAGE_BINARY="$tmp_dir/legacy"
if reconcile_runtime_binary >/dev/null 2>&1; then
  fail "runtime symbolic link was accepted"
fi
[[ -L "$RUNTIME_BINARY" ]] || fail "runtime symbolic link was replaced"
assert_same "$tmp_dir/runtime-symlink-target" "$tmp_dir/runtime-symlink-target.before"

rm -f "$RUNTIME_BINARY"
ln -s "$tmp_dir/missing-runtime-target" "$RUNTIME_BINARY"
if reconcile_runtime_binary >/dev/null 2>&1; then
  fail "dangling runtime symbolic link was accepted"
fi
[[ -L "$RUNTIME_BINARY" ]] || fail "dangling runtime symbolic link was replaced"

rm -f "$RUNTIME_BINARY"
cp "$tmp_dir/v6" "$RUNTIME_BINARY"
cp "$tmp_dir/v7" "$tmp_dir/image-symlink-target"
cp "$tmp_dir/image-symlink-target" "$tmp_dir/image-symlink-target.before"
ln -s "$tmp_dir/image-symlink-target" "$tmp_dir/image-link"
IMAGE_BINARY="$tmp_dir/image-link"
if reconcile_runtime_binary >/dev/null 2>&1; then
  fail "image symbolic link was accepted"
fi
[[ -L "$IMAGE_BINARY" ]] || fail "image symbolic link was replaced"
assert_same "$tmp_dir/image-symlink-target" "$tmp_dir/image-symlink-target.before"
assert_same "$RUNTIME_BINARY" "$tmp_dir/v6"

rm -f "$tmp_dir/image-link"
ln -s "$tmp_dir/missing-image-target" "$tmp_dir/image-link"
if reconcile_runtime_binary >/dev/null 2>&1; then
  fail "dangling image symbolic link was accepted"
fi
[[ -L "$IMAGE_BINARY" ]] || fail "dangling image symbolic link was replaced"
assert_same "$RUNTIME_BINARY" "$tmp_dir/v6"

cp "$tmp_dir/v8" "$tmp_dir/backup-symlink-target"
cp "$tmp_dir/backup-symlink-target" "$tmp_dir/backup-symlink-target.before"
rm -f "$RUNTIME_BINARY.backup"
ln -s "$tmp_dir/backup-symlink-target" "$RUNTIME_BINARY.backup"
IMAGE_BINARY="$tmp_dir/v7"
if reconcile_runtime_binary >/dev/null 2>&1; then
  fail "runtime backup symbolic link was accepted"
fi
[[ -L "$RUNTIME_BINARY.backup" ]] || fail "runtime backup symbolic link was replaced"
assert_same "$tmp_dir/backup-symlink-target" "$tmp_dir/backup-symlink-target.before"
assert_same "$RUNTIME_BINARY" "$tmp_dir/v6"

rm -f "$RUNTIME_BINARY.backup"
ln -s "$tmp_dir/missing-backup-target" "$RUNTIME_BINARY.backup"
if reconcile_runtime_binary >/dev/null 2>&1; then
  fail "dangling runtime backup symbolic link was accepted"
fi
[[ -L "$RUNTIME_BINARY.backup" ]] || fail "dangling runtime backup symbolic link was replaced"
assert_same "$RUNTIME_BINARY" "$tmp_dir/v6"

rm -f "$RUNTIME_BINARY.backup"
cp "$tmp_dir/v7" "$tmp_dir/legacy-symlink-target"
cp "$tmp_dir/legacy-symlink-target" "$tmp_dir/legacy-symlink-target.before"
ln -s "$tmp_dir/legacy-symlink-target" "$LEGACY_IMAGE_BINARY"
IMAGE_BINARY=/app/image/sub2api
if resolve_image_binary >/dev/null 2>&1; then
  fail "legacy image symbolic link was accepted"
fi
[[ -L "$LEGACY_IMAGE_BINARY" ]] || fail "legacy image symbolic link was replaced"
assert_same "$tmp_dir/legacy-symlink-target" "$tmp_dir/legacy-symlink-target.before"

rm -f "$LEGACY_IMAGE_BINARY"
ln -s "$tmp_dir/missing-legacy-target" "$LEGACY_IMAGE_BINARY"
if resolve_image_binary >/dev/null 2>&1; then
  fail "dangling legacy image symbolic link was accepted"
fi
[[ -L "$LEGACY_IMAGE_BINARY" ]] || fail "dangling legacy image symbolic link was replaced"

rm -f "$LEGACY_IMAGE_BINARY"
cp "$tmp_dir/v7" "$LEGACY_IMAGE_BINARY"
IMAGE_BINARY=/app/image/sub2api
resolve_image_binary
[[ "$IMAGE_BINARY" == "$LEGACY_IMAGE_BINARY" ]] || fail "legacy /app/sub2api layout was not selected"

echo "docker entrypoint runtime version-selection and symbolic-link tests passed"
