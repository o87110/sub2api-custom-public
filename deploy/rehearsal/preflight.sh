#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$script_dir"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

env_value() {
  local key="$1"
  awk -F= -v key="$key" '
    $1 == key {
      value = substr($0, index($0, "=") + 1)
    }
    END {
      sub(/\r$/, "", value)
      print value
    }
  ' .env
}

for command in docker ss awk grep; do
  command -v "$command" >/dev/null 2>&1 ||
    fail "required command is missing: $command"
done

docker compose version >/dev/null 2>&1 ||
  fail "Docker Compose plugin is unavailable"

case "$script_dir/" in
  *"/sub2api-deploy/"*)
    fail "refusing to run rehearsal files inside the production deployment directory"
    ;;
esac

test -f .env || fail "copy .env.example to .env and configure it first"

if grep -nE '=CHANGE_ME([_A-Z0-9]*)?$' .env; then
  fail "replace every CHANGE_ME value in .env"
fi

bind_host="$(env_value REHEARSAL_BIND_HOST)"
bind_host="${bind_host:-127.0.0.1}"
allow_public_bind="$(env_value REHEARSAL_ALLOW_PUBLIC_BIND)"
allow_public_bind="${allow_public_bind:-false}"
case "$allow_public_bind" in
  true | false) ;;
  *) fail "REHEARSAL_ALLOW_PUBLIC_BIND must be true or false" ;;
esac
case "$bind_host" in
  127.0.0.1) ;;
  0.0.0.0)
    test "$allow_public_bind" = "true" ||
      fail "public binding requires REHEARSAL_ALLOW_PUBLIC_BIND=true"
    echo "WARNING: rehearsal endpoint will be exposed on all IPv4 interfaces" >&2
    ;;
  *) fail "REHEARSAL_BIND_HOST must be 127.0.0.1 or 0.0.0.0" ;;
esac

port="$(env_value REHEARSAL_PORT)"
port="${port:-18081}"
[[ "$port" =~ ^[0-9]+$ ]] ||
  fail "REHEARSAL_PORT must be numeric"
((port >= 1025 && port <= 65535)) ||
  fail "REHEARSAL_PORT must be between 1025 and 65535"

image="$(env_value SUB2API_IMAGE)"
test -n "$image" || fail "SUB2API_IMAGE is required"
if [[ "$image" =~ ^ghcr\.io/o87110/sub2api-custom-public:v[0-9]+\.[0-9]+\.[0-9]+-custom\.[0-9]+$ ]]; then
  :
elif [[ "$image" =~ ^ghcr\.io/o87110/sub2api-custom-public@sha256:[0-9a-f]{64}$ ]]; then
  :
else
  fail "SUB2API_IMAGE must use an exact custom version tag or sha256 digest"
fi

own_stack_running=false
if docker compose ps --status running --services 2>/dev/null |
  grep -qx sub2api; then
  own_stack_running=true
fi

if ss -lntH "sport = :$port" | grep -q . &&
  [ "$own_stack_running" = false ]; then
  fail "127.0.0.1:$port is already in use; choose another REHEARSAL_PORT"
fi

if [ "$own_stack_running" = false ]; then
  available_kib="$(awk '/^MemAvailable:/ { print $2 }' /proc/meminfo)"
  if [ -n "$available_kib" ] && ((available_kib < 1572864)); then
    fail "less than 1.5 GiB memory is currently available"
  fi
fi

docker compose config --quiet

restart_policy_count="$(
  docker compose config |
    grep -c '^    restart: unless-stopped$' || true
)"
test "$restart_policy_count" = "3" ||
  fail "all rehearsal services must use restart: unless-stopped"

echo "rehearsal preflight checks passed"
echo "bind: $bind_host:$port"
if [ "$bind_host" = "127.0.0.1" ]; then
  echo "endpoint: http://127.0.0.1:$port"
else
  echo "endpoint: http://<server-ip>:$port"
fi
