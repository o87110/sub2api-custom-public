#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  cat >&2 <<'EOF'
usage:
  release-notes.sh validate <notes-file>
  release-notes.sh render \
    --tag <vX.Y.Z-custom.N> \
    --official-tag <vX.Y.Z> \
    --commit <40-character-commit> \
    --previous-ref <vX.Y.Z-custom.N|vendor-X.Y.Z> \
    --repository <owner/repository> \
    --ci-run-id <id>

CUSTOM_RELEASE_EXTRA_NOTES may contain optional plain-text lines. Each
non-empty line is normalized into a bullet under "Custom changes".
EOF
  exit 2
}

validate_notes() {
  local notes_file="$1"
  local heading count
  [[ -s "$notes_file" ]] || fail "Release Notes are empty"
  for heading in \
    '## Custom changes' \
    '## Database' \
    '## Validation'; do
    count="$(grep -Fxc -- "$heading" "$notes_file" || true)"
    [[ "$count" -eq 1 ]] ||
      fail "Release Notes must contain exactly one ${heading} section"
  done
}

subcommand="${1:-}"
case "$subcommand" in
  validate)
    [[ $# -eq 2 ]] || usage
    validate_notes "$2"
    exit 0
    ;;
  render)
    shift
    ;;
  *)
    usage
    ;;
esac

tag=""
official_tag=""
commit=""
previous_ref=""
repository=""
ci_run_id=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag) tag="${2:-}"; shift 2 ;;
    --official-tag) official_tag="${2:-}"; shift 2 ;;
    --commit) commit="${2:-}"; shift 2 ;;
    --previous-ref) previous_ref="${2:-}"; shift 2 ;;
    --repository) repository="${2:-}"; shift 2 ;;
    --ci-run-id) ci_run_id="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[[ "$tag" =~ ^v([0-9]+\.[0-9]+\.[0-9]+)-custom\.[0-9]+$ ]] || usage
version="${BASH_REMATCH[1]}"
[[ "$official_tag" == "v${version}" ]] || usage
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || usage
[[ "$previous_ref" =~ ^(v[0-9]+\.[0-9]+\.[0-9]+-custom\.[0-9]+|vendor-[0-9]+\.[0-9]+\.[0-9]+)$ ]] ||
  usage
[[ "$repository" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || usage
[[ "$ci_run_id" =~ ^[1-9][0-9]*$ ]] || usage
git cat-file -e "${commit}^{commit}" ||
  fail "Release Notes commit is unavailable"
git cat-file -e "${previous_ref}^{commit}" ||
  fail "Release Notes comparison ref is unavailable"
git merge-base --is-ancestor "${previous_ref}^{commit}" "${commit}^{commit}" ||
  fail "Release Notes comparison ref is not an ancestor of the target commit"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
notes_file="$tmp_dir/release-notes.md"
migration_files="$tmp_dir/migrations.txt"
schema_files="$tmp_dir/schema.txt"

git diff --name-only --diff-filter=AM \
  "${previous_ref}^{commit}" "${commit}^{commit}" -- \
  'backend/migrations/*.sql' > "$migration_files"
git diff --name-only --diff-filter=AM \
  "${previous_ref}^{commit}" "${commit}^{commit}" -- \
  'backend/ent/schema/*.go' > "$schema_files"

database_exceptions_changed=false
set +e
git diff --quiet \
  "${previous_ref}^{commit}" "${commit}^{commit}" -- \
  .github/custom-database-exceptions.tsv
database_exception_status=$?
set -e
case "$database_exception_status" in
  0) ;;
  1) database_exceptions_changed=true ;;
  *) fail "unable to compare custom database exceptions" ;;
esac

concurrent_index_changed=false
while IFS= read -r migration_file; do
  [[ -n "$migration_file" ]] || continue
  if git show "${commit}:${migration_file}" |
    grep -Eiq 'CREATE[[:space:]]+([^;[:space:]]+[[:space:]]+)*INDEX[[:space:]]+CONCURRENTLY'; then
    concurrent_index_changed=true
  fi
done < "$migration_files"

previous_version=""
if [[ "$previous_ref" =~ ^v([0-9]+\.[0-9]+\.[0-9]+)-custom\.[0-9]+$ ]]; then
  previous_version="${BASH_REMATCH[1]}"
fi

{
  echo '## Custom changes'
  echo
  if [[ -n "$previous_version" && "$previous_version" != "$version" ]]; then
    echo "- Official baseline upgraded from \`v${previous_version}\` to \`${official_tag}\`."
  else
    echo "- Custom maintenance changes since \`${previous_ref}\`, based on \`${official_tag}\`."
  fi
  echo "- Source commit: \`${commit}\`."
  while IFS= read -r extra_line || [[ -n "$extra_line" ]]; do
    extra_line="${extra_line//$'\r'/}"
    extra_line="${extra_line#- }"
    [[ -n "$extra_line" ]] || continue
    printf -- '- %s\n' "$extra_line"
  done <<<"${CUSTOM_RELEASE_EXTRA_NOTES:-}"
  echo "- Full comparison: https://github.com/${repository}/compare/${previous_ref}...${tag}"
  echo
  echo '## Database'
  echo
  if [[ -s "$migration_files" ]]; then
    echo '- Migration files changed:'
    while IFS= read -r migration_file; do
      [[ -n "$migration_file" ]] || continue
      echo "  - \`${migration_file}\`"
    done < "$migration_files"
  else
    echo "- No Migration files changed between \`${previous_ref}\` and \`${tag}\`."
  fi
  if [[ -s "$schema_files" ]]; then
    echo '- Ent Schema sources changed:'
    while IFS= read -r schema_file; do
      [[ -n "$schema_file" ]] || continue
      echo "  - \`${schema_file}\`"
    done < "$schema_files"
  else
    echo "- No Ent Schema source files changed between \`${previous_ref}\` and \`${tag}\`."
  fi
  if [[ "$database_exceptions_changed" == "true" ]]; then
    echo '- The registered custom database-semantic exceptions changed and passed the exact-tree database boundary gate.'
  fi
  if [[ "$concurrent_index_changed" == "true" ]]; then
    echo '- Includes `CREATE INDEX CONCURRENTLY`: execute outside a transaction, monitor invalid-index cleanup, and review `DROP INDEX CONCURRENTLY` rollback before deployment.'
  fi
  echo '- Back up the production database before applying migrations and verify rollback compatibility before replacing the runtime.'
  upgrade_record=".github/upgrades/${version}.md"
  if git cat-file -e "${commit}:${upgrade_record}" 2>/dev/null; then
    echo "- Upgrade review and rollback conditions: https://github.com/${repository}/blob/${tag}/${upgrade_record}"
  fi
  echo
  echo '## Validation'
  echo
  echo "- Exact-SHA CI run: https://github.com/${repository}/actions/runs/${ci_run_id}"
  echo '- The Release workflow must verify archives, checksums, the immutable Manifest, and GHCR multi-architecture Digests before publication.'
  echo '- Security Scan reports independently and is not treated as a publication bypass or a false success.'
} > "$notes_file"

validate_notes "$notes_file"
cat "$notes_file"
