#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose_file="$repo_root/deploy/rehearsal/docker-compose.yml"
env_example="$repo_root/deploy/rehearsal/.env.example"
preflight="$repo_root/deploy/rehearsal/preflight.sh"
readme="$repo_root/deploy/rehearsal/README_CN.md"
entrypoint="$repo_root/deploy/docker-entrypoint.sh"
gitignore="$repo_root/deploy/rehearsal/.gitignore"
local_compose="$repo_root/deploy/docker-compose.local.yml"
standalone_compose="$repo_root/deploy/docker-compose.standalone.yml"
deploy_script="$repo_root/deploy/docker-deploy.sh"
root_readme="$repo_root/README.md"
root_readme_cn="$repo_root/README_CN.md"
root_readme_ja="$repo_root/README_JA.md"
deploy_readme="$repo_root/deploy/README.md"

assert_in_order() {
  local file="$1"
  shift
  local previous_line=0
  local needle
  local matched_line

  for needle in "$@"; do
    matched_line="$(
      awk -v previous_line="$previous_line" -v needle="$needle" \
        'NR > previous_line && index($0, needle) { print NR; exit }' \
        "$file"
    )"
    if [ -z "$matched_line" ]; then
      echo "ERROR: missing or out-of-order deployment instruction in $file: $needle" >&2
      exit 1
    fi
    previous_line="$matched_line"
  done
}

for file in \
  "$compose_file" \
  "$env_example" \
  "$preflight" \
  "$readme" \
  "$entrypoint" \
  "$gitignore" \
  "$local_compose" \
  "$standalone_compose" \
  "$deploy_script" \
  "$root_readme" \
  "$root_readme_cn" \
  "$root_readme_ja" \
  "$deploy_readme"; do
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
grep -Fq './runtime:/app/runtime:Z' "$local_compose"
grep -Fq 'sub2api_runtime:/app/runtime' "$standalone_compose"
grep -Fq '  sub2api_runtime:' "$standalone_compose"
grep -Fq 'mkdir -p data runtime postgres_data redis_data' "$deploy_script"
grep -Fxq '.env' "$gitignore"
grep -Fxq 'data/' "$gitignore"
grep -Fxq 'runtime/' "$gitignore"
grep -Fxq 'secrets/' "$gitignore"

for deployment_doc in \
  "$root_readme" \
  "$root_readme_cn" \
  "$root_readme_ja" \
  "$deploy_readme"; do
  grep -Fq \
    'docker inspect "$container_id" --format '\''{{range .Mounts}}{{println .Destination}}{{end}}'\'' | grep -qx '\''/app/runtime'\''' \
    "$deployment_doc"
  grep -Fq 'test ! -L ./runtime' "$deployment_doc"
  grep -Fq 'test ! -L ./runtime/sub2api.backup' "$deployment_doc"
  grep -Fq \
    'docker exec --user sub2api "$container_id" /app/runtime/sub2api --version' \
    "$deployment_doc"
  grep -Fq \
    'docker exec --user sub2api "$container_id" /app/runtime/sub2api.backup --version' \
    "$deployment_doc"
  if grep -nF './runtime/sub2api' "$deployment_doc" |
    grep -Fq -- '--version'; then
    echo "ERROR: exported runtime must not execute on the host: $deployment_doc" >&2
    exit 1
  fi
  assert_in_order \
    "$deployment_doc" \
    'container_id="$(docker compose -f docker-compose.local.yml ps -q sub2api)"' \
    'docker exec --user sub2api "$container_id" /app/runtime/sub2api --version' \
    'docker exec --user sub2api "$container_id" /app/runtime/sub2api.backup --version' \
    'docker compose -f docker-compose.local.yml stop sub2api' \
    'mkdir -p runtime' \
    'docker cp "$container_id:/app/runtime/sub2api" ./runtime/sub2api' \
    'docker cp "$container_id:/app/runtime/sub2api.backup" ./runtime/sub2api.backup' \
    'test ! -L ./runtime/sub2api' \
    'sha256sum ./runtime/sub2api' \
    'test ! -L ./runtime/sub2api.backup' \
    'sha256sum ./runtime/sub2api.backup' \
    'docker compose -f docker-compose.local.yml pull' \
    'docker compose -f docker-compose.local.yml up -d' \
    'docker compose -f docker-compose.local.yml stop sub2api' \
    'docker compose -f docker-compose.local.yml down'
done

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
