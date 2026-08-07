#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
frontend_tool="$repo_root/deploy/release/frontend-dist.sh"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

[[ -s "$frontend_tool" ]] || fail "frontend Artifact preparation tool is missing"
/bin/bash -n "$frontend_tool"
for tool in git sha256sum; do
  command -v "$tool" >/dev/null 2>&1 || fail "fixture tool is missing: $tool"
done
if ! command -v jq >/dev/null 2>&1; then
  echo "jq not found; frontend Artifact fixtures deferred to Linux CI"
  exit 0
fi
python_command=""
for candidate in python3 python; do
  if command -v "$candidate" >/dev/null 2>&1 &&
     "$candidate" -c 'import sys; raise SystemExit(sys.version_info < (3, 8))'; then
    python_command="$candidate"
    break
  fi
done
if [[ -z "$python_command" ]]; then
  echo "Python 3.8+ not found; frontend Artifact fixtures deferred to Linux CI"
  exit 0
fi

tmp_root="$(mktemp -d)"
cleanup() {
  [[ -n "$tmp_root" && -d "$tmp_root" && ! -L "$tmp_root" ]] ||
    fail "refusing to clean an invalid frontend fixture directory"
  rm -rf -- "$tmp_root"
}
trap cleanup EXIT

fixture_repo="$tmp_root/repo"
fixture_bin="$tmp_root/bin"
runtime_root="$tmp_root/runtime"
mkdir -p \
  "$fixture_repo/deploy/release" \
  "$fixture_repo/frontend" \
  "$fixture_repo/backend/internal/web" \
  "$fixture_bin" \
  "$runtime_root"
cp "$frontend_tool" "$fixture_repo/deploy/release/frontend-dist.sh"

cat > "$fixture_bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

[[ "${1:-}" == "api" ]] || exit 91
shift
if [[ "${1:-}" == "--paginate" ]]; then
  shift
fi
endpoint="${1:-}"
case "$endpoint" in
  repos/o87110/sub2api-custom-public/actions/runs/123)
    printf '%s\n' "$MOCK_RUN_JSON"
    ;;
  'repos/o87110/sub2api-custom-public/actions/runs/123/artifacts?per_page=100')
    printf '%s\n' "$MOCK_ARTIFACTS_JSON"
    ;;
  repos/o87110/sub2api-custom-public/actions/artifacts/*/zip)
    [[ -s "${MOCK_ARCHIVE:-}" ]] || exit 92
    command cat -- "$MOCK_ARCHIVE"
    ;;
  *)
    echo "unexpected mock gh endpoint: $endpoint" >&2
    exit 93
    ;;
esac
EOF
chmod 0700 "$fixture_bin/gh"

cat > "$fixture_bin/pnpm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == *"install --frozen-lockfile"* ]]
EOF
chmod 0700 "$fixture_bin/pnpm"

cat > "$fixture_bin/make" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" == "-C" && -n "${2:-}" && "${3:-}" == "build-frontend" ]]
mkdir -p "$2/backend/internal/web/dist"
printf '<!doctype html><title>fallback</title>\n' \
  > "$2/backend/internal/web/dist/index.html"
EOF
chmod 0700 "$fixture_bin/make"

commit="1111111111111111111111111111111111111111"
artifact_name="release-frontend-dist-${commit}"
run_json() {
  local run_id="${1:-123}"
  local source_commit="${2:-$commit}"
  local workflow="${3:-.github/workflows/backend-ci.yml}"
  jq -nc \
    --argjson id "$run_id" \
    --arg sha "$source_commit" \
    --arg path "$workflow" '
      {
        id: $id,
        status: "completed",
        conclusion: "success",
        head_sha: $sha,
        head_branch: "main",
        event: "push",
        path: $path
      }
    '
}

create_archive() {
  local kind="$1"
  local output="$2"
  "$python_command" - "$kind" "$output" <<'PY'
import stat
import sys
import zipfile

kind, output = sys.argv[1:]
with zipfile.ZipFile(output, "w", zipfile.ZIP_DEFLATED) as archive:
    if kind == "normal":
        archive.writestr("index.html", "<!doctype html><title>artifact</title>\n")
        archive.writestr("assets/app.js", "console.log('fixture')\n")
    elif kind == "traversal":
        archive.writestr("index.html", "fixture\n")
        archive.writestr("../escaped.txt", "escaped\n")
    elif kind == "symlink":
        archive.writestr("index.html", "fixture\n")
        link = zipfile.ZipInfo("assets/link")
        link.create_system = 3
        link.external_attr = (stat.S_IFLNK | 0o777) << 16
        archive.writestr(link, "../../outside")
    elif kind == "extra-root":
        archive.writestr("dist/index.html", "fixture\n")
    else:
        raise SystemExit(f"unknown archive kind: {kind}")
PY
}

artifact_page() {
  local archive="$1"
  local expired="${2:-false}"
  local workflow_run_id="${3:-123}"
  local digest_override="${4:-}"
  local digest
  digest="${digest_override:-sha256:$(sha256sum "$archive" | awk '{print $1}')}"
  jq -nc \
    --arg name "$artifact_name" \
    --arg digest "$digest" \
    --argjson expired "$expired" \
    --argjson workflow_run_id "$workflow_run_id" '
      {
        total_count: 1,
        artifacts: [{
          id: 456,
          name: $name,
          expired: $expired,
          digest: $digest,
          workflow_run: {id: $workflow_run_id}
        }]
      }
    '
}

run_prepare() {
  local run="$1"
  local artifacts="$2"
  local archive="$3"
  local directory="$4"
  local provenance="$5"
  local output="$6"
  PATH="$fixture_bin:$PATH" \
  GITHUB_REPOSITORY=o87110/sub2api-custom-public \
  RUNNER_TEMP="$runtime_root" \
  MOCK_RUN_JSON="$run" \
  MOCK_ARTIFACTS_JSON="$artifacts" \
  MOCK_ARCHIVE="$archive" \
    /bin/bash "$fixture_repo/deploy/release/frontend-dist.sh" prepare \
      --ci-run-id 123 \
      --commit "$commit" \
      --directory "$directory" \
      --provenance "$provenance" \
      --output "$output"
}

expect_prepare_failure() {
  local description="$1"
  local run="$2"
  local artifacts="$3"
  local archive="$4"
  local case_root="$tmp_root/failure-$RANDOM-$RANDOM"
  mkdir -p "$case_root/dist"
  if run_prepare \
    "$run" "$artifacts" "$archive" \
    "$case_root/dist" "$case_root/provenance.json" "$case_root/output" \
    >"$case_root/stdout" 2>"$case_root/stderr"; then
    fail "$description was accepted"
  fi
}

normal_archive="$tmp_root/normal.zip"
create_archive normal "$normal_archive"
valid_run="$(run_json)"
valid_artifacts="$(artifact_page "$normal_archive")"
success_root="$tmp_root/success"
mkdir -p "$success_root/dist"
run_prepare \
  "$valid_run" "$valid_artifacts" "$normal_archive" \
  "$success_root/dist" "$success_root/provenance.json" "$success_root/output"
jq -e \
  --arg commit "$commit" '
    .mode == "ci-artifact" and
    .source_commit == $commit and
    .ci_run_id == 123 and
    .artifact_id == 456 and
    (.artifact_digest | test("^sha256:[0-9a-f]{64}$")) and
    (.content_sha256 | test("^[0-9a-f]{64}$"))
  ' "$success_root/provenance.json" >/dev/null
[[ "$(jq -r '.content_sha256' "$success_root/provenance.json")" == "$(
  /bin/bash "$fixture_repo/deploy/release/frontend-dist.sh" hash \
    --directory "$success_root/dist"
)" ]] || fail "reused frontend Artifact content hash is unstable"

fallback_directory="$fixture_repo/backend/internal/web/dist"
missing_artifacts='{"total_count":0,"artifacts":[]}'
run_prepare \
  "$valid_run" "$missing_artifacts" "$normal_archive" \
  "$fallback_directory" "$tmp_root/missing-provenance.json" "$tmp_root/missing-output"
jq -e --arg commit "$commit" '
  .mode == "release-build" and
  .source_commit == $commit and
  (.content_sha256 | test("^[0-9a-f]{64}$")) and
  (has("artifact_id") | not)
' "$tmp_root/missing-provenance.json" >/dev/null

rm -rf -- "$fallback_directory"
expired_artifacts="$(artifact_page "$normal_archive" true)"
run_prepare \
  "$valid_run" "$expired_artifacts" "$normal_archive" \
  "$fallback_directory" "$tmp_root/expired-provenance.json" "$tmp_root/expired-output"
jq -e '.mode == "release-build"' "$tmp_root/expired-provenance.json" >/dev/null

duplicate_artifacts="$(
  jq -nc \
    --argjson first "$(jq '.artifacts[0]' <<<"$valid_artifacts")" \
    '{total_count: 2, artifacts: [$first, $first]}'
)"
expect_prepare_failure "duplicate exact-SHA frontend Artifacts" \
  "$valid_run" "$duplicate_artifacts" "$normal_archive"
expect_prepare_failure "frontend Artifact from the wrong CI run" \
  "$(run_json 124)" "$valid_artifacts" "$normal_archive"
expect_prepare_failure "frontend Artifact from the wrong commit" \
  "$(run_json 123 2222222222222222222222222222222222222222)" \
  "$valid_artifacts" "$normal_archive"
expect_prepare_failure "frontend Artifact from the wrong workflow" \
  "$(run_json 123 "$commit" .github/workflows/release.yml)" \
  "$valid_artifacts" "$normal_archive"
expect_prepare_failure "frontend Artifact with mismatched producer metadata" \
  "$valid_run" "$(artifact_page "$normal_archive" false 999)" "$normal_archive"
expect_prepare_failure "frontend Artifact with a mismatched archive digest" \
  "$valid_run" \
  "$(artifact_page "$normal_archive" false 123 sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa)" \
  "$normal_archive"

for kind in traversal symlink extra-root; do
  archive="$tmp_root/${kind}.zip"
  create_archive "$kind" "$archive"
  expect_prepare_failure "frontend Artifact archive with ${kind}" \
    "$valid_run" "$(artifact_page "$archive")" "$archive"
done

echo "frontend dist safety checks passed"
