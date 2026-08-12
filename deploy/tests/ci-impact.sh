#!/usr/bin/env bash
set -euo pipefail

repo_root="${CI_IMPACT_REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
config_file="${CI_IMPACT_CONFIG:-$repo_root/.github/ci-impact.yml}"

fail() { echo "ERROR: $*" >&2; exit 1; }
usage() { echo "usage: $0 classify --base <sha> --head <sha> [--config <file>] [--repo-root <dir>]" >&2; exit 2; }

[[ "${1:-}" == "classify" ]] || usage
shift
base=""
head=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --base) base="${2:-}"; shift 2 ;;
    --head) head="${2:-}"; shift 2 ;;
    --config) config_file="${2:-}"; shift 2 ;;
    --repo-root) repo_root="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[[ "$base" =~ ^[0-9a-f]{40}$ ]] || usage
[[ "$head" =~ ^[0-9a-f]{40}$ ]] || usage
[[ -s "$config_file" ]] || fail "impact configuration is missing: $config_file"
git -C "$repo_root" cat-file -e "${base}^{commit}" || fail "base commit is unavailable: $base"
git -C "$repo_root" cat-file -e "${head}^{commit}" || fail "head commit is unavailable: $head"

patterns_for() {
  local section="$1"
  awk -v section="$section" '
    $0 == section ":" { active=1; next }
    active && /^[[:alnum:]_]+:$/ { exit }
    active && /^[[:space:]]*-[[:space:]]*/ {
      sub(/^[[:space:]]*-[[:space:]]*/, "")
      sub(/^\x27|\x27$/, "")
      print
    }
  ' "$config_file"
}

mapfile -t frontend_patterns < <(patterns_for frontend)
mapfile -t backend_patterns < <(patterns_for backend)
mapfile -t high_risk_patterns < <(patterns_for high_risk)
mapfile -t release_input_patterns < <(patterns_for release_inputs)
mapfile -t fast_patterns < <(patterns_for fast)

matches_section() {
  local path="$1" section="$2" pattern
  local -n patterns_ref="${section}_patterns"
  for pattern in "${patterns_ref[@]}"; do
    [[ -n "$pattern" ]] || continue
    [[ "$path" =~ $pattern ]] && return 0
  done
  return 1
}

matches_fast() {
  local path="$1" pattern
  for pattern in "${fast_patterns[@]}"; do
    [[ -n "$pattern" ]] || continue
    [[ "$path" =~ $pattern ]] && return 0
  done
  return 1
}

mapfile -t changed_files < <(git -C "$repo_root" diff --name-only --no-renames "$base" "$head")
[[ "${#changed_files[@]}" -gt 0 ]] || changed_files=("<empty>")

mode="fast"
frontend_changed=false
backend_changed=false
shell_changed=false
release_inputs_changed=false
declare -a backend_packages=()

for path in "${changed_files[@]}"; do
  [[ "$path" != "<empty>" ]] || continue
  matches_section "$path" frontend && frontend_changed=true
  matches_section "$path" backend && backend_changed=true
  matches_section "$path" release_inputs && release_inputs_changed=true
  if [[ "$path" == deploy/* || "$path" == Dockerfile* || "$path" == Makefile ]]; then
    shell_changed=true
  fi
  if matches_section "$path" high_risk; then
    mode="full"
  fi
  if ! matches_section "$path" frontend &&
     ! matches_section "$path" backend &&
     ! matches_fast "$path" &&
     ! matches_section "$path" high_risk; then
    # Unknown paths must never silently enter the fast lane.
    mode="full"
  fi
  if [[ "$path" == backend/internal/* ]]; then
    package_dir="${path#backend/}"
    package_dir="${package_dir%/*}"
    [[ "$package_dir" == internal/* ]] && backend_packages+=("./$package_dir")
  fi
done

if [[ "$backend_changed" == "true" && "${#backend_packages[@]}" -eq 0 ]]; then
  mode="full"
fi

if [[ "$backend_changed" == "true" && "${#backend_packages[@]}" -gt 0 ]]; then
  # Package-level tests are safe only for clearly isolated custom packages.
  # Official handler/service/repository/server changes are deliberately full.
  for path in "${changed_files[@]}"; do
    if [[ "$path" == backend/internal/handler/* ||
          "$path" == backend/internal/service/* ||
          "$path" == backend/internal/repository/* ||
          "$path" == backend/internal/server/* ||
          "$path" == backend/migrations/* ||
          "$path" == backend/ent/* ]]; then
      mode="full"
      break
    fi
  done
fi

printf 'mode=%s\n' "$mode"
printf 'frontend_changed=%s\n' "$frontend_changed"
printf 'backend_changed=%s\n' "$backend_changed"
printf 'shell_changed=%s\n' "$shell_changed"
printf 'release_inputs_changed=%s\n' "$release_inputs_changed"
printf 'changed_count=%s\n' "${#changed_files[@]}"
if [[ "${#backend_packages[@]}" -gt 0 ]]; then
  printf 'backend_packages=%s\n' "$(printf '%s\n' "${backend_packages[@]}" | sort -u | paste -sd' ' -)"
else
  printf 'backend_packages=\n'
fi
