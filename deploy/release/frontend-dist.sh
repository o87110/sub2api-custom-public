#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  cat >&2 <<'EOF'
usage:
  frontend-dist.sh hash --directory <dist-directory>
  frontend-dist.sh prepare --ci-run-id <id> --commit <sha> \
    --directory <dist-directory> --provenance <new-json-file> --output <file>
EOF
  exit 2
}

validate_dist() {
  local directory="$1"
  [[ -d "$directory" && -s "$directory/index.html" ]] ||
    fail "frontend dist must contain a non-empty root index.html"
  if find "$directory" -mindepth 1 -type l -print -quit | grep -q .; then
    fail "frontend dist must not contain symbolic links"
  fi
  if find "$directory" -mindepth 1 ! -type f ! -type d -print -quit | grep -q .; then
    fail "frontend dist must contain only regular files and directories"
  fi
  file_count="$(find "$directory" -type f | wc -l | tr -d ' ')"
  [[ "$file_count" =~ ^[1-9][0-9]*$ ]] || fail "frontend dist is empty"
  size_kib="$(du -sk "$directory" | awk '{print $1}')"
  [[ "$size_kib" =~ ^[0-9]+$ && "$size_kib" -le 262144 ]] ||
    fail "frontend dist exceeds the 256 MiB extracted size limit"
}

hash_dist() {
  local directory="$1"
  validate_dist "$directory"
  tar \
    --sort=name \
    --mtime='UTC 1970-01-01' \
    --owner=0 \
    --group=0 \
    --numeric-owner \
    --format=ustar \
    -cf - \
    -C "$directory" . |
    sha256sum |
    awk '{print $1}'
}

command_name="${1:-}"
[[ -n "$command_name" ]] || usage
shift

if [[ "$command_name" == "hash" ]]; then
  directory=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --directory) directory="${2:-}"; shift 2 ;;
      *) usage ;;
    esac
  done
  [[ -n "$directory" ]] || usage
  hash_dist "$directory"
  exit 0
fi

[[ "$command_name" == "prepare" ]] || usage

ci_run_id=""
commit=""
directory=""
provenance=""
output=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --ci-run-id) ci_run_id="${2:-}"; shift 2 ;;
    --commit) commit="${2:-}"; shift 2 ;;
    --directory) directory="${2:-}"; shift 2 ;;
    --provenance) provenance="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[[ "$ci_run_id" =~ ^[1-9][0-9]*$ ]] || usage
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || usage
[[ -n "$directory" && -n "$provenance" && -n "$output" ]] || usage
[[ ! -e "$provenance" ]] || fail "frontend provenance output must be a new file"
[[ "${GITHUB_REPOSITORY:-}" == "o87110/sub2api-custom-public" ]] ||
  fail "frontend Artifact reuse only permits the trusted public custom repository"
for tool in gh jq sha256sum tar; do
  command -v "$tool" >/dev/null 2>&1 || fail "required frontend preparation tool is missing: $tool"
done
python_command=""
for candidate in python3 python; do
  if command -v "$candidate" >/dev/null 2>&1 &&
     "$candidate" -c 'import sys; raise SystemExit(sys.version_info < (3, 8))'; then
    python_command="$candidate"
    break
  fi
done
[[ -n "$python_command" ]] || fail "Python 3.8 or newer is required for safe Artifact extraction"

if [[ -e "$directory" ]]; then
  [[ -d "$directory" && -z "$(find "$directory" -mindepth 1 -print -quit)" ]] ||
    fail "frontend dist destination must not contain existing files"
else
  parent="${directory%/*}"
  [[ "$parent" != "$directory" && -d "$parent" ]] ||
    fail "frontend dist destination parent is unavailable"
fi

started_at=$SECONDS
run_json="$(gh api "repos/${GITHUB_REPOSITORY}/actions/runs/${ci_run_id}")"
jq -e \
  --argjson run_id "$ci_run_id" \
  --arg commit "$commit" '
    .id == $run_id and
    .status == "completed" and
    .conclusion == "success" and
    .head_sha == $commit and
    .head_branch == "main" and
    (.event == "push" or .event == "workflow_dispatch") and
    .path == ".github/workflows/backend-ci.yml"
  ' <<<"$run_json" >/dev/null || fail "frontend Artifact producer CI run is invalid"

artifact_name="release-frontend-dist-${commit}"
artifacts_json="$(
  gh api --paginate \
    "repos/${GITHUB_REPOSITORY}/actions/runs/${ci_run_id}/artifacts?per_page=100"
)"
matching_artifacts="$(
  jq -sce --arg name "$artifact_name" '
    if all(.[]; (.artifacts | type) == "array") then
      [.[].artifacts[] | select(.name == $name)]
    else
      error("frontend Artifact pagination response is invalid")
    end
  ' <<<"$artifacts_json"
)" || fail "frontend Artifact listing is invalid"
artifact_count="$(jq 'length' <<<"$matching_artifacts")"
[[ "$artifact_count" -le 1 ]] || fail "frontend CI run contains duplicate exact-SHA Artifacts"

mode=release-build
artifact_id=""
artifact_digest=""
tmp_base="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
[[ "$tmp_base" == /* && -d "$tmp_base" && ! -L "$tmp_base" ]] ||
  fail "frontend temporary directory root is invalid"
tmp_dir="$(mktemp -d "$tmp_base/frontend-dist.XXXXXX")"
cleanup() {
  [[ "$tmp_dir" == "$tmp_base"/frontend-dist.* && -d "$tmp_dir" && ! -L "$tmp_dir" ]] ||
    fail "refusing to clean an invalid frontend temporary directory"
  rm -rf -- "$tmp_dir"
}
trap cleanup EXIT

artifact_expired=false
if [[ "$artifact_count" -eq 1 ]]; then
  artifact_expired="$(
    jq -er '
      if (.[0].expired | type) == "boolean" then
        .[0].expired | tostring
      else
        error("invalid expiration metadata")
      end
    ' <<<"$matching_artifacts"
  )" || fail "frontend Artifact expiration metadata is invalid"
fi

if [[ "$artifact_count" -eq 1 && "$artifact_expired" == "false" ]]; then
  artifact="$(jq -c '.[0]' <<<"$matching_artifacts")"
  artifact_id="$(jq -er '.id | select(type == "number" and . > 0)' <<<"$artifact")"
  artifact_digest="$(jq -er '.digest | select(test("^sha256:[0-9a-f]{64}$"))' <<<"$artifact")"
  [[ "$(jq -r '.workflow_run.id' <<<"$artifact")" == "$ci_run_id" ]] ||
    fail "frontend Artifact producer run ID mismatch"

  archive="$tmp_dir/frontend-dist.zip"
  gh api "repos/${GITHUB_REPOSITORY}/actions/artifacts/${artifact_id}/zip" > "$archive"
  archive_size="$(wc -c < "$archive" | tr -d ' ')"
  [[ "$archive_size" =~ ^[1-9][0-9]*$ && "$archive_size" -le 104857600 ]] ||
    fail "frontend Artifact archive exceeds the 100 MiB download limit"
  [[ "sha256:$(sha256sum "$archive" | awk '{print $1}')" == "$artifact_digest" ]] ||
    fail "frontend Artifact archive digest mismatch"

  extracted="$tmp_dir/extracted"
  mkdir -p "$extracted"
  "$python_command" - "$archive" "$extracted" <<'PY'
import os
import shutil
import stat
import sys
import zipfile
from pathlib import Path

archive = Path(sys.argv[1])
destination = Path(sys.argv[2])
max_files = 10_000
max_size = 256 * 1024 * 1024

with zipfile.ZipFile(archive) as source:
    entries = source.infolist()
    if not entries:
        raise SystemExit("frontend Artifact archive is empty")
    if len(entries) > max_files:
        raise SystemExit("frontend Artifact contains too many entries")

    seen = set()
    declared_size = 0
    for entry in entries:
        name = entry.filename
        if not name or name.startswith("/") or "\\" in name or "\x00" in name:
            raise SystemExit("frontend Artifact contains an invalid path")
        parts = name.rstrip("/").split("/")
        if not parts or any(part in ("", ".", "..") for part in parts):
            raise SystemExit("frontend Artifact contains path traversal or an ambiguous path")
        normalized = "/".join(parts)
        if normalized in seen:
            raise SystemExit("frontend Artifact contains duplicate paths")
        seen.add(normalized)

        mode = entry.external_attr >> 16
        file_type = stat.S_IFMT(mode)
        if file_type not in (0, stat.S_IFREG, stat.S_IFDIR):
            raise SystemExit("frontend Artifact contains a symbolic link or special file")
        if entry.flag_bits & 0x1:
            raise SystemExit("frontend Artifact contains an encrypted entry")
        declared_size += entry.file_size
        if declared_size > max_size:
            raise SystemExit("frontend Artifact exceeds the 256 MiB extracted size limit")

    extracted_size = 0
    for entry in entries:
        parts = entry.filename.rstrip("/").split("/")
        target = destination.joinpath(*parts)
        if entry.is_dir() or entry.filename.endswith("/"):
            target.mkdir(parents=True, exist_ok=True)
            continue
        target.parent.mkdir(parents=True, exist_ok=True)
        with source.open(entry) as input_file, target.open("xb") as output_file:
            while True:
                chunk = input_file.read(1024 * 1024)
                if not chunk:
                    break
                extracted_size += len(chunk)
                if extracted_size > max_size:
                    raise SystemExit("frontend Artifact exceeds the 256 MiB extracted size limit")
                output_file.write(chunk)
PY
  validate_dist "$extracted"
  mkdir -p "$directory"
  cp -a "$extracted/." "$directory/"
  mode=ci-artifact
else
  expected_directory="$repo_root/backend/internal/web/dist"
  [[ "$directory" == "$expected_directory" ]] ||
    fail "local frontend fallback only permits the repository dist path"
  for tool in pnpm make; do
    command -v "$tool" >/dev/null 2>&1 ||
      fail "required local frontend build tool is missing: $tool"
  done
  pnpm --dir "$repo_root/frontend" install --frozen-lockfile
  make -C "$repo_root" build-frontend
fi

content_sha256="$(hash_dist "$directory")"
if [[ "$mode" == "ci-artifact" ]]; then
  jq -n \
    --arg mode "$mode" \
    --arg source_commit "$commit" \
    --arg content_sha256 "$content_sha256" \
    --argjson ci_run_id "$ci_run_id" \
    --argjson artifact_id "$artifact_id" \
    --arg artifact_digest "$artifact_digest" \
    '{
      mode: $mode,
      source_commit: $source_commit,
      content_sha256: $content_sha256,
      ci_run_id: $ci_run_id,
      artifact_id: $artifact_id,
      artifact_digest: $artifact_digest
    }' > "$provenance"
else
  jq -n \
    --arg mode "$mode" \
    --arg source_commit "$commit" \
    --arg content_sha256 "$content_sha256" \
    '{
      mode: $mode,
      source_commit: $source_commit,
      content_sha256: $content_sha256
    }' > "$provenance"
fi

elapsed_seconds=$((SECONDS - started_at))
{
  echo "frontend_mode=$mode"
  echo "frontend_content_sha256=$content_sha256"
  echo "frontend_seconds=$elapsed_seconds"
} >> "$output"

if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  {
    echo "### Frontend Release input"
    echo
    echo "- Mode: \`${mode}\`"
    echo "- Source commit: \`${commit}\`"
    echo "- Content SHA256: \`${content_sha256}\`"
    echo "- Preparation time: \`${elapsed_seconds}s\`"
  } >> "$GITHUB_STEP_SUMMARY"
fi
