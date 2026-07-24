#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
versions_file="$repo_root/.github/custom-tool-versions.env"
install_root="${CUSTOM_TOOL_INSTALL_ROOT:-${RUNNER_TEMP:-$(mktemp -d)}/sub2api-custom-tools}"
requested="${1:-}"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

[[ -s "$versions_file" ]] || fail "custom tool version manifest is missing"
# shellcheck disable=SC1090
source "$versions_file"
mkdir -p "$install_root/bin" "$install_root/downloads"

download_verified() {
  local url="$1"
  local checksum="$2"
  local destination="$3"
  [[ "$url" == https://* ]] || fail "tool URL must use HTTPS: $url"
  [[ "$checksum" =~ ^[0-9a-f]{64}$ ]] || fail "invalid tool checksum for $url"
  curl --fail --location --silent --show-error --proto '=https' \
    "$url" --output "$destination"
  printf '%s  %s\n' "$checksum" "$destination" | sha256sum --check --status ||
    fail "tool checksum mismatch: $url"
}

install_archive_binary() {
  local name="$1"
  local url="$2"
  local checksum="$3"
  local archive="$install_root/downloads/${name}.tar.gz"
  download_verified "$url" "$checksum" "$archive"
  tar -xzf "$archive" -C "$install_root/bin" "$name"
  chmod 0755 "$install_root/bin/$name"
}

IFS=',' read -r -a tools <<< "$requested"
for tool in "${tools[@]}"; do
  case "$tool" in
    actionlint)
      install_archive_binary actionlint \
        "$ACTIONLINT_LINUX_AMD64_URL" "$ACTIONLINT_LINUX_AMD64_SHA256"
      "$install_root/bin/actionlint" -version | grep -Fq "$ACTIONLINT_VERSION"
      ;;
    oras)
      install_archive_binary oras "$ORAS_LINUX_AMD64_URL" "$ORAS_LINUX_AMD64_SHA256"
      "$install_root/bin/oras" version | grep -Fq "$ORAS_VERSION"
      ;;
    goreleaser)
      install_archive_binary goreleaser \
        "$GORELEASER_LINUX_AMD64_URL" "$GORELEASER_LINUX_AMD64_SHA256"
      "$install_root/bin/goreleaser" --version | grep -Fq "$GORELEASER_VERSION"
      ;;
    buildx)
      destination="$install_root/bin/docker-buildx"
      download_verified "$BUILDX_LINUX_AMD64_URL" "$BUILDX_LINUX_AMD64_SHA256" "$destination"
      chmod 0755 "$destination"
      "$destination" version | grep -Fq "v${BUILDX_VERSION}"
      ;;
    golangci-lint)
      archive="$install_root/downloads/golangci-lint.tar.gz"
      download_verified \
        "$GOLANGCI_LINT_LINUX_AMD64_URL" \
        "$GOLANGCI_LINT_LINUX_AMD64_SHA256" \
        "$archive"
      tar -xzf "$archive" \
        --strip-components=1 \
        -C "$install_root/bin" \
        "golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64/golangci-lint"
      chmod 0755 "$install_root/bin/golangci-lint"
      "$install_root/bin/golangci-lint" version | grep -Fq "$GOLANGCI_LINT_VERSION"
      ;;
    node)
      archive="$install_root/downloads/node.tar.xz"
      download_verified "$NODE_LINUX_X64_URL" "$NODE_LINUX_X64_SHA256" "$archive"
      mkdir -p "$install_root/node"
      tar -xJf "$archive" --strip-components=1 -C "$install_root/node"
      ln -sf "$install_root/node/bin/node" "$install_root/bin/node"
      ln -sf "$install_root/node/bin/npm" "$install_root/bin/npm"
      "$install_root/bin/node" --version | grep -Fxq "v${NODE_VERSION}"
      ;;
    pnpm)
      archive="$install_root/downloads/pnpm.tgz"
      download_verified "$PNPM_TARBALL_URL" "$PNPM_TARBALL_SHA256" "$archive"
      mkdir -p "$install_root/pnpm"
      tar -xzf "$archive" --strip-components=1 -C "$install_root/pnpm"
      ln -sf "$install_root/pnpm/bin/pnpm.cjs" "$install_root/bin/pnpm"
      PATH="$install_root/bin:$PATH" "$install_root/bin/pnpm" --version |
        grep -Fxq "$PNPM_VERSION"
      ;;
    "")
      ;;
    *)
      fail "unsupported custom tool: $tool"
      ;;
  esac
done

if [[ -n "${GITHUB_PATH:-}" ]]; then
  printf '%s\n' "$install_root/bin" >> "$GITHUB_PATH"
else
  printf '%s\n' "$install_root/bin"
fi
