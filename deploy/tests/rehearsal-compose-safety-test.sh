#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose_file="$repo_root/deploy/rehearsal/docker-compose.yml"
env_example="$repo_root/deploy/rehearsal/.env.example"
preflight="$repo_root/deploy/rehearsal/preflight.sh"
readme="$repo_root/deploy/rehearsal/README_CN.md"
entrypoint="$repo_root/deploy/docker-entrypoint.sh"
gitignore="$repo_root/deploy/rehearsal/.gitignore"

for file in \
  "$compose_file" \
  "$env_example" \
  "$preflight" \
  "$readme" \
  "$entrypoint" \
  "$gitignore"; do
  test -s "$file" || {
    echo "ERROR: rehearsal file is missing or empty: $file" >&2
    exit 1
  }
done

/bin/bash -n "$preflight"

grep -Fq 'name: sub2api-rehearsal' "$compose_file"
grep -Fq \
  '${REHEARSAL_BIND_HOST:-127.0.0.1}:${REHEARSAL_PORT:-18081}:8080' \
  "$compose_file"
grep -Fq './runtime:/app/runtime' "$compose_file"
grep -Fq \
  'SUB2API_IMAGE=ghcr.io/o87110/sub2api-custom-public:vX.Y.Z-custom.N' \
  "$env_example"
grep -Fq \
  'fail "SUB2API_IMAGE must use an exact custom version tag or sha256 digest"' \
  "$preflight"
grep -Fq 'REHEARSAL_BIND_HOST=127.0.0.1' "$env_example"
grep -Fq 'REHEARSAL_ALLOW_PUBLIC_BIND=false' "$env_example"
grep -Fq 'REHEARSAL_PORT=18081' "$env_example"
grep -Fq '禁止执行 `docker compose down -v`' "$readme"
grep -Fq \
  'fail "public binding requires REHEARSAL_ALLOW_PUBLIC_BIND=true"' \
  "$preflight"
grep -Fq \
  'WARNING: rehearsal endpoint will be exposed on all IPv4 interfaces' \
  "$preflight"
grep -Fq \
  'fail "all rehearsal services must use restart: unless-stopped"' \
  "$preflight"
grep -Fq 'RUNTIME_UPDATE_TOKEN=$RUNTIME_SECRET_DIR/update-token' "$entrypoint"
grep -Fq 'chmod 0400 "$temp_token"' "$entrypoint"
grep -Fq 'export UPDATE_GITHUB_TOKEN_FILE=$RUNTIME_UPDATE_TOKEN' "$entrypoint"
grep -Fxq '.env' "$gitignore"
grep -Fxq 'data/' "$gitignore"
grep -Fxq 'runtime/' "$gitignore"
grep -Fxq 'secrets/' "$gitignore"

test "$(grep -c 'restart: unless-stopped' "$compose_file")" -eq 3
test "$(grep -c 'mem_limit:' "$compose_file")" -eq 3
test "$(grep -c 'cpus:' "$compose_file")" -eq 3
test "$(grep -c '^    ports:' "$compose_file")" -eq 1

for forbidden in \
  'container_name:' \
  '/var/run/docker.sock' \
  'network_mode: host' \
  '/www/wwwroot/sub2api-deploy' \
  'restart: "no"' \
  'UPDATE_GITHUB_TOKEN=' \
  'UPDATE_GITHUB_TOKEN_FILE:' \
  'sub2api_update_token' \
  'external: true'; do
  if grep -nF -- "$forbidden" "$compose_file" "$env_example"; then
    echo "ERROR: forbidden rehearsal configuration found: $forbidden" >&2
    exit 1
  fi
done

if grep -nE '(github_pat_|ghp_[[:alnum:]]+)' \
  "$compose_file" "$env_example" "$preflight" "$readme"; then
  echo "ERROR: a GitHub token-like value is present in rehearsal files" >&2
  exit 1
fi

echo "rehearsal compose safety checks passed"
