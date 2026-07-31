#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
release_workflow="$repo_root/.github/workflows/release.yml"
publish_workflow="$repo_root/.github/workflows/publish-custom.yml"
backend_ci_workflow="$repo_root/.github/workflows/backend-ci.yml"
security_workflow="$repo_root/.github/workflows/security-scan.yml"
upgrade_gate_workflow="$repo_root/.github/workflows/upstream-upgrade-gate.yml"
goreleaser="$repo_root/.goreleaser.yaml"
simple_goreleaser="$repo_root/.goreleaser.simple.yaml"
preflight="$repo_root/deploy/release/custom-release-preflight.sh"
payload_builder="$repo_root/deploy/release/build-release-payload.sh"
manifest_builder="$repo_root/deploy/release/create-release-manifest.sh"
publisher="$repo_root/deploy/release/publish-release.sh"
release_policy="$repo_root/deploy/release/release-state-policy.sh"
release_notes_tool="$repo_root/deploy/release/release-notes.sh"
ci_waiter="$repo_root/deploy/release/wait-for-required-ci.sh"
tool_versions="$repo_root/.github/custom-tool-versions.env"
release_dockerfile="$repo_root/deploy/release/Dockerfile"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

fail_if_present() {
  local description="$1"
  local pattern="$2"
  shift 2
  if grep -nF -- "$pattern" "$@"; then
    fail "$description"
  fi
}

for file in \
  "$release_workflow" \
  "$publish_workflow" \
  "$backend_ci_workflow" \
  "$security_workflow" \
  "$upgrade_gate_workflow" \
  "$goreleaser" \
  "$simple_goreleaser" \
  "$preflight" \
  "$payload_builder" \
  "$manifest_builder" \
  "$publisher" \
  "$release_policy" \
  "$release_notes_tool" \
  "$ci_waiter" \
  "$release_dockerfile" \
  "$tool_versions"; do
  [[ -s "$file" ]] || fail "required custom release control is missing: $file"
done

for script in \
  "$preflight" \
  "$payload_builder" \
  "$manifest_builder" \
  "$publisher" \
  "$release_policy" \
  "$release_notes_tool" \
  "$ci_waiter"; do
  /bin/bash -n "$script"
done
# shellcheck disable=SC1090
source "$release_policy"

fail_if_present \
  "formal publication must not retain the simple_release bypass" \
  'simple_release' \
  "$release_workflow" \
  "$publish_workflow"
fail_if_present \
  "formal publication must not reference the official simple GoReleaser config" \
  '.goreleaser.simple.yaml' \
  "$release_workflow" \
  "$publish_workflow" \
  "$preflight" \
  "$payload_builder" \
  "$publisher"
fail_if_present \
  "GoReleaser validation must not be skipped" \
  '--skip=validate' \
  "$release_workflow" \
  "$upgrade_gate_workflow" \
  "$payload_builder"
fail_if_present \
  "verified Release assets must never be replaced" \
  '--clobber' \
  "$release_workflow" \
  "$publisher"
fail_if_present \
  "the custom publisher must never delete a Release or asset" \
  'gh release delete' \
  "$publisher" \
  "$release_workflow"
fail_if_present \
  "an existing immutable Tag must not be passed as target_commitish" \
  '--target "$commit"' \
  "$publisher"
fail_if_present \
  "Release target_commitish must not replace Tag ref verification" \
  'target_commitish' \
  "$preflight" \
  "$publisher"
fail_if_present \
  "custom Release code must not invoke an official install script" \
  'deploy/install.sh' \
  "$release_workflow" \
  "$goreleaser" \
  "$publisher"
fail_if_present \
  "custom Release code must not invoke an official Docker deployment script" \
  'deploy/docker-deploy.sh' \
  "$release_workflow" \
  "$goreleaser" \
  "$publisher"
fail_if_present \
  "custom archives must not bundle official deployment scripts" \
  '      - deploy/*' \
  "$goreleaser"
fail_if_present \
  "custom archives must not bundle the official README install entry" \
  '      - README*' \
  "$goreleaser"
grep -Fq '      - docs/custom/OPERATIONS_CN.md' "$goreleaser"
fail_if_present \
  "custom Release code must not advertise the official repository as an install source" \
  'github.com/Wei-Shaw/sub2api' \
  "$release_workflow" \
  "$goreleaser" \
  "$publisher"
fail_if_present \
  "custom publication must not manage a latest convenience image Tag" \
  'sub2api-custom:latest' \
  "$release_workflow" \
  "$goreleaser" \
  "$publisher"
fail_if_present \
  "custom publication must not use GoReleaser major/minor convenience templates" \
  '.Major' \
  "$goreleaser"
fail_if_present \
  "the formal workflow must not use a GoReleaser download Action" \
  'goreleaser/goreleaser-action@' \
  "$release_workflow" \
  "$upgrade_gate_workflow"

grep -Fq 'group: release-exact-${{ inputs.tag || github.ref_name }}' "$release_workflow"
grep -Fq 'cancel-in-progress: false' "$release_workflow"
grep -Fq 'permissions: {}' "$release_workflow"
grep -Fq 'actions: read' "$release_workflow"
grep -Fq 'contents: write' "$release_workflow"
grep -Fq 'packages: write' "$release_workflow"
grep -Fq 'environment: custom-release-publish' "$release_workflow"
grep -Fq 'issues: write' "$release_workflow"
grep -Fq 'if: needs.context.outputs.release_state == '\''none'\''' "$release_workflow"
grep -Fq 'CURRENT_PAYLOAD_ARTIFACT_ID: ${{ needs.build.outputs.payload_artifact_id }}' "$release_workflow"
grep -Fq 'CURRENT_MANIFEST_ARTIFACT_ID: ${{ needs.build.outputs.manifest_artifact_id }}' "$release_workflow"
grep -Fq 'release-payload-${{ needs.context.outputs.tag }}-${{ github.run_id }}-${{ github.run_attempt }}' "$release_workflow"
grep -Fq 'release-manifest-${{ needs.context.outputs.tag }}-${{ github.run_id }}-${{ github.run_attempt }}' "$release_workflow"
grep -Fq 'expected_sha:' "$publish_workflow"
grep -Fq 'INPUT_EXPECTED_SHA: ${{ inputs.expected_sha }}' "$publish_workflow"
grep -Fq 'deploy/release/wait-for-required-ci.sh \' "$publish_workflow"
grep -Fq 'ci_wait_args+=(--wait)' "$publish_workflow"
grep -Fq 'deploy/release/release-notes.sh render \' "$publish_workflow"
grep -Fq 'deploy/release/release-notes.sh \' "$publish_workflow"
grep -Fq 'validate "$tag_message_file"' "$publish_workflow"
grep -Fq 'git tag -a --cleanup=verbatim \' "$publish_workflow"
grep -Fq 'gh workflow run publish-custom.yml \' "$upgrade_gate_workflow"
grep -Fq -- '-f expected_sha="$merged_sha"' "$upgrade_gate_workflow"
grep -Fq 'validate_release_notes "$release"' "$publisher"
grep -Fq '/bin/bash "$release_notes_tool" validate "$notes_file"' "$publisher"

payload_upload_line="$(grep -nE '^[[:space:]]+id: payload$' "$release_workflow" | cut -d: -f1)"
manifest_create_line="$(grep -nF 'Create immutable release manifest' "$release_workflow" | cut -d: -f1)"
manifest_upload_line="$(grep -nE '^[[:space:]]+id: manifest$' "$release_workflow" | cut -d: -f1)"
[[ "$payload_upload_line" -lt "$manifest_create_line" &&
   "$manifest_create_line" -lt "$manifest_upload_line" ]] ||
  fail "payload artifact must precede manifest creation and upload"

grep -Fq 'goreleaser check' "$payload_builder"
grep -Fq 'goreleaser release --clean --skip=publish' "$payload_builder"
grep -Fq 'type=oci,dest=$oci_tar' "$payload_builder"
grep -Fq 'cp -R "$repo_root/backend/resources" "$output/image-context/backend/resources"' "$payload_builder"
grep -Fq 'QEMU_BINFMT_IMAGE' "$payload_builder"
grep -Fq '(.manifests | length) == 2' "$payload_builder"
grep -Fq 'application/vnd.oci.image.index.v1+json' "$payload_builder"
grep -Fq 'application/vnd.oci.image.manifest.v1+json' "$payload_builder"
grep -Fq 'index_media_type' "$manifest_builder"
grep -Fq 'media_type: .mediaType' "$publisher"
grep -Fq 'payload-files.sha256' "$payload_builder"
grep -Eq '^ARG POSTGRES_IMAGE=postgres:18-alpine3\.21@sha256:[0-9a-f]{64}$' "$release_dockerfile"
grep -Eq '^ARG ALPINE_IMAGE=alpine:3\.21@sha256:[0-9a-f]{64}$' "$release_dockerfile"
grep -Fq 'COPY --chown=sub2api:sub2api backend/resources /app/resources' "$release_dockerfile"
grep -Fq 'payload_artifact' "$manifest_builder"
grep -Fq 'producer.run_attempt' "$publisher"
grep -Fq 'producer.workflow_commit' "$publisher"
grep -Fq 'producer.workflow_ref == "refs/heads/main"' "$publisher"
grep -Fq 'workflow dispatch came from an untrusted branch' "$publisher"
grep -Fq 'source "$policy_script"' "$publisher"
grep -Fq 'actions/artifacts/${artifact_id}/zip' "$publisher"
grep -Fq 'all(.[]; (.artifacts | type) == "array")' "$publisher"
grep -Fq '[.[].artifacts[]]' "$publisher"
grep -Fq 'oras repo tags --format json' "$publisher"
grep -Fq 'is_missing_ghcr_repository_error()' "$publisher"
grep -Fq '[[ "$(<"$error_file")" == \' "$publisher"
grep -Fq '"Error response from registry: name unknown: repository name not known to registry" ]]' "$publisher"
grep -Fq 'if is_missing_ghcr_repository_error "$error_file"; then' "$publisher"
grep -Fq "tags_json='{\"tags\":[]}'" "$publisher"
fail_if_present \
  "GHCR query failures must not be treated as missing tags" \
  'oras resolve "$oci_repository:$1" 2>/dev/null || true' \
  "$publisher"
grep -Fq 'conflicting manifest artifacts prevent unique recovery' "$release_policy"
grep -Fq 'gh release create "$tag"' "$publisher"
grep -Fq 'gh release upload "$tag" "$manifest_upload"' "$publisher"
grep -Fq '[[ "$attempt" -lt 5 ]] || fail "Draft creation did not become visible"' "$publisher"
grep -Fq 'fail "Draft creation produced multiple Releases"' "$publisher"
grep -Fq 'verify_release_assets "$release" "$authoritative_manifest"' "$publisher"
grep -Fq 'oras cp' "$publisher"
grep -Fq -- '--from-oci-layout' "$publisher"
grep -Fq 'oras tag "$oci_repository@$amd64_digest" "$amd64_tag"' "$publisher"
grep -Fq 'oras tag "$oci_repository@$arm64_digest" "$arm64_tag"' "$publisher"
grep -Fq 'oras resolve' "$publisher"
grep -Fq 'oras manifest fetch --descriptor' "$publisher"
grep -Fq 'gh release edit "$tag"' "$publisher"
grep -Fq 'exact_tag_needs_write()' "$publisher"
grep -Fq 'verify_tag_unchanged' "$publisher"
grep -Fq 'local_tag_ref="refs/tags/${tag}"' "$publisher"
fail_if_present \
  "Release publisher must not depend on persisted checkout credentials for Tag verification" \
  'git fetch origin' \
  "$publisher"
grep -Fq 'if exact_tag_needs_write "$multi_tag" "$index_digest"; then' "$publisher"
grep -Fq 'if exact_tag_needs_write "$amd64_tag" "$amd64_digest"; then' "$publisher"
grep -Fq 'if exact_tag_needs_write "$arm64_tag" "$arm64_digest"; then' "$publisher"
grep -Fq 'Release assets do not exactly match the authoritative manifest' "$publisher"
grep -Fq 'Draft contains duplicate or undeclared assets' "$publisher"
grep -Fq 'Draft contains an asset created before its authoritative manifest' "$publisher"
grep -Fq 'Draft contains assets before its authoritative manifest' "$publisher"

ci_wait_tmp="$(mktemp -d)"
mkdir -p "$ci_wait_tmp/bin"
cat > "$ci_wait_tmp/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-} ${2:-}" == "run list" ]]; then
  case "${FAKE_CI_SCENARIO:-success}" in
    success)
      printf '101\tcompleted\tsuccess\t%s\tmain\tworkflow_dispatch\n' \
        "$FAKE_TARGET_SHA"
      ;;
    delayed)
      count=0
      if [[ -s "$FAKE_STATE_FILE" ]]; then
        count="$(<"$FAKE_STATE_FILE")"
      fi
      count=$((count + 1))
      printf '%s\n' "$count" > "$FAKE_STATE_FILE"
      if [[ "$count" -gt 1 ]]; then
        printf '102\tcompleted\tsuccess\t%s\tmain\tworkflow_dispatch\n' \
          "$FAKE_TARGET_SHA"
      fi
      ;;
    failed)
      printf '103\tcompleted\tfailure\t%s\tmain\tworkflow_dispatch\n' \
        "$FAKE_TARGET_SHA"
      ;;
    boundary-failed)
      printf '104\tcompleted\tsuccess\t%s\tmain\tworkflow_dispatch\n' \
        "$FAKE_TARGET_SHA"
      ;;
    *)
      exit 91
      ;;
  esac
  exit 0
fi

if [[ "${1:-} ${2:-}" == "run view" ]]; then
  if [[ "${FAKE_CI_SCENARIO:-success}" == "boundary-failed" ]]; then
    printf 'completed\tfailure\n'
  else
    printf 'completed\tsuccess\n'
  fi
  exit 0
fi

exit 92
EOF
chmod +x "$ci_wait_tmp/bin/gh"
fixture_sha="0bec4e52db6919325cb7efa4847483da9efb870a"

wait_result="$(
  PATH="$ci_wait_tmp/bin:$PATH" \
  FAKE_CI_SCENARIO=success \
  FAKE_TARGET_SHA="$fixture_sha" \
    /bin/bash "$ci_waiter" \
      --repo o87110/sub2api-custom-public \
      --workflow backend-ci.yml \
      --sha "$fixture_sha"
)"
[[ "$wait_result" == "101" ]] ||
  fail "exact-SHA CI waiter rejected a successful required run"

wait_result="$(
  PATH="$ci_wait_tmp/bin:$PATH" \
  FAKE_CI_SCENARIO=delayed \
  FAKE_TARGET_SHA="$fixture_sha" \
  FAKE_STATE_FILE="$ci_wait_tmp/state" \
  PUBLISH_CI_MAX_ATTEMPTS=2 \
  PUBLISH_CI_POLL_SECONDS=0 \
    /bin/bash "$ci_waiter" \
      --repo o87110/sub2api-custom-public \
      --workflow backend-ci.yml \
      --sha "$fixture_sha" \
      --wait
)"
[[ "$wait_result" == "102" ]] ||
  fail "exact-SHA CI waiter did not wait for the trusted dispatch"

if PATH="$ci_wait_tmp/bin:$PATH" \
  FAKE_CI_SCENARIO=failed \
  FAKE_TARGET_SHA="$fixture_sha" \
    /bin/bash "$ci_waiter" \
      --repo o87110/sub2api-custom-public \
      --workflow backend-ci.yml \
      --sha "$fixture_sha" >/dev/null 2>&1; then
  fail "exact-SHA CI waiter accepted a failed required run"
fi
if PATH="$ci_wait_tmp/bin:$PATH" \
  FAKE_CI_SCENARIO=boundary-failed \
  FAKE_TARGET_SHA="$fixture_sha" \
    /bin/bash "$ci_waiter" \
      --repo o87110/sub2api-custom-public \
      --workflow backend-ci.yml \
      --sha "$fixture_sha" >/dev/null 2>&1; then
  fail "exact-SHA CI waiter accepted a failed boundaries job"
fi
rm -rf "$ci_wait_tmp"

notes_tmp="$(mktemp -d)"
release_fixture_commit="$(
  git -C "$repo_root" rev-parse 'v0.1.165-custom.1^{commit}'
)"
CUSTOM_RELEASE_EXTRA_NOTES=$'User-visible custom behavior remains available.\nRelease controls were hardened.' \
  /bin/bash "$release_notes_tool" render \
    --tag v0.1.165-custom.1 \
    --official-tag v0.1.165 \
    --commit "$release_fixture_commit" \
    --previous-ref v0.1.164-custom.6 \
    --repository o87110/sub2api-custom-public \
    --ci-run-id 30199742684 \
    > "$notes_tmp/upgrade-notes.md"
/bin/bash "$release_notes_tool" validate "$notes_tmp/upgrade-notes.md"
for heading in \
  '## Custom changes' \
  '## Database' \
  '## Validation'; do
  [[ "$(grep -Fxc -- "$heading" "$notes_tmp/upgrade-notes.md")" -eq 1 ]] ||
    fail "generated Release Notes contain an invalid ${heading} section"
done
grep -Fq 'backend/migrations/187_add_usage_log_session_id.sql' \
  "$notes_tmp/upgrade-notes.md"
grep -Fq 'backend/migrations/190_add_users_email_alias_dedup_index_notx.sql' \
  "$notes_tmp/upgrade-notes.md"
grep -Fq 'Includes `CREATE INDEX CONCURRENTLY`' \
  "$notes_tmp/upgrade-notes.md"
grep -Fq -- '- User-visible custom behavior remains available.' \
  "$notes_tmp/upgrade-notes.md"

tag_fixture_repo="$notes_tmp/tag-fixture"
git init -q "$tag_fixture_repo"
git -C "$tag_fixture_repo" config user.name "Custom Release Safety"
git -C "$tag_fixture_repo" config user.email "release-safety@example.invalid"
printf 'fixture\n' > "$tag_fixture_repo/fixture.txt"
git -C "$tag_fixture_repo" add fixture.txt
git -C "$tag_fixture_repo" commit -q -m "fixture"
{
  echo 'Sub2API v0.1.165-custom.1'
  echo
  cat "$notes_tmp/upgrade-notes.md"
} > "$notes_tmp/tag-message.md"
git -C "$tag_fixture_repo" tag -a --cleanup=verbatim \
  -F "$notes_tmp/tag-message.md" \
  v0.1.165-custom.1 HEAD
git -C "$tag_fixture_repo" tag -l \
  --format='%(contents:body)' \
  v0.1.165-custom.1 \
  > "$notes_tmp/tag-body.md"
/bin/bash "$release_notes_tool" validate "$notes_tmp/tag-body.md"

/bin/bash "$release_notes_tool" render \
  --tag v0.1.165-custom.2 \
  --official-tag v0.1.165 \
  --commit "$release_fixture_commit" \
  --previous-ref v0.1.165-custom.1 \
  --repository o87110/sub2api-custom-public \
  --ci-run-id 30199742684 \
  > "$notes_tmp/no-migration-notes.md"
grep -Fq 'No Migration files changed' "$notes_tmp/no-migration-notes.md"
printf '%s\n' \
  '## Custom changes' \
  '## Database' \
  > "$notes_tmp/incomplete-notes.md"
if /bin/bash "$release_notes_tool" \
  validate "$notes_tmp/incomplete-notes.md" >/dev/null 2>&1; then
  fail "Release Notes without Validation were accepted"
fi
rm -rf "$notes_tmp"

[[ "$(release_manifest_action none false true)" == "current-manifest" ]]
[[ "$(release_manifest_action draft true false)" == "remote-manifest" ]]
[[ "$(release_manifest_action draft false false)" == "recover-manifest" ]]
[[ "$(release_manifest_action remote false true)" == "recover-manifest" ]]
if release_manifest_action published false false >/dev/null 2>&1; then
  fail "published Release without a manifest was accepted"
fi

if command -v jq >/dev/null 2>&1; then
  ordered_draft='{"assets":[
    {"name":"release-manifest.json","created_at":"2026-07-24T01:00:00Z"},
    {"name":"checksums.txt","created_at":"2026-07-24T01:00:01Z"}
  ]}'
  draft_assets_follow_manifest "$ordered_draft"
  early_asset_draft='{"assets":[
    {"name":"checksums.txt","created_at":"2026-07-24T00:59:59Z"},
    {"name":"release-manifest.json","created_at":"2026-07-24T01:00:00Z"}
  ]}'
  if draft_assets_follow_manifest "$early_asset_draft"; then
    fail "Draft asset older than its manifest was accepted"
  fi

  artifact_pages=$'{"total_count":2,"artifacts":[{"id":101,"name":"release-manifest-v0.1.162-custom.99-1-1","expired":false}]}\n{"total_count":2,"artifacts":[{"id":202,"name":"release-manifest-v0.1.162-custom.99-2-1","expired":true}]}'
  selected_artifacts="$(
    jq -s --arg prefix "release-manifest-v0.1.162-custom.99-" '
      if all(.[]; (.artifacts | type) == "array") then
        [.[].artifacts[]] |
        map(select((.name | startswith($prefix)) and (.expired == false))) |
        sort_by(.id)
      else
        error("Actions artifact pagination response is invalid")
      end
    ' <<<"$artifact_pages"
  )"
  jq -e '
    length == 1 and
    .[0].id == 101 and
    .[0].name == "release-manifest-v0.1.162-custom.99-1-1"
  ' <<<"$selected_artifacts" >/dev/null

  ci_fixture_tmp="$(mktemp -d)"
  mkdir -p "$ci_fixture_tmp/bin" "$ci_fixture_tmp/repo"
  cat > "$ci_fixture_tmp/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "api" ]]; then
  shift
  if [[ "${1:-}" == "--paginate" ]]; then
    printf '[]\n'
    exit 0
  fi
  case "${1:-}" in
    repos/o87110/sub2api-custom-public/git/ref/tags/*)
      printf '{"object":{"sha":"%s","type":"commit"}}\n' "$MOCK_TAG_OID"
      exit 0
      ;;
  esac
elif [[ "${1:-}" == "run" && "${2:-}" == "list" ]]; then
  shift 2
  jq_filter=""
  while [[ $# -gt 0 ]]; do
    if [[ "$1" == "--jq" ]]; then
      jq_filter="${2:-}"
      break
    fi
    shift
  done
  [[ -n "$jq_filter" ]]
  jq -r "$jq_filter" <<<"$MOCK_CI_RUNS_JSON"
  exit 0
elif [[ "${1:-}" == "run" && "${2:-}" == "view" ]]; then
  printf 'completed\tsuccess\n'
  exit 0
fi

echo "unexpected mock gh invocation: $*" >&2
exit 1
EOF
  chmod 0700 "$ci_fixture_tmp/bin/gh"
  git -C "$ci_fixture_tmp/repo" init --quiet
  git -C "$ci_fixture_tmp/repo" config user.name "Release Safety Test"
  git -C "$ci_fixture_tmp/repo" config user.email "release-safety@example.invalid"
  git -C "$ci_fixture_tmp/repo" config commit.gpgsign false
  printf 'fixture\n' > "$ci_fixture_tmp/repo/fixture.txt"
  git -C "$ci_fixture_tmp/repo" add fixture.txt
  git -C "$ci_fixture_tmp/repo" commit --quiet -m fixture
  fixture_commit="$(git -C "$ci_fixture_tmp/repo" rev-parse HEAD)"
  fixture_tag="v0.1.162-custom.99"
  git -C "$ci_fixture_tmp/repo" tag "$fixture_tag"

  older_success_newer_failure="$(
    jq -nc --arg sha "$fixture_commit" '[
      {
        databaseId: 101,
        status: "completed",
        conclusion: "success",
        headSha: $sha,
        headBranch: "main",
        event: "push",
        createdAt: "2026-07-24T01:00:00Z"
      },
      {
        databaseId: 202,
        status: "completed",
        conclusion: "failure",
        headSha: $sha,
        headBranch: "main",
        event: "workflow_dispatch",
        createdAt: "2026-07-24T02:00:00Z"
      }
    ]'
  )"
  if (
    cd "$ci_fixture_tmp/repo"
    PATH="$ci_fixture_tmp/bin:$PATH" \
    GITHUB_REPOSITORY="o87110/sub2api-custom-public" \
    MOCK_TAG_OID="$fixture_commit" \
    MOCK_CI_RUNS_JSON="$older_success_newer_failure" \
      /bin/bash "$preflight" \
        --tag "$fixture_tag" \
        --control-commit "$fixture_commit" \
        --output "$ci_fixture_tmp/blocked-output"
  ) >"$ci_fixture_tmp/blocked-stdout" 2>"$ci_fixture_tmp/blocked-stderr"; then
    fail "an older successful CI run masked a newer failed run"
  fi
  grep -Fq \
    'latest accepted CI run 202 is completed/failure' \
    "$ci_fixture_tmp/blocked-stderr"

  older_failure_newer_success="$(
    jq -nc --arg sha "$fixture_commit" '[
      {
        databaseId: 101,
        status: "completed",
        conclusion: "failure",
        headSha: $sha,
        headBranch: "main",
        event: "push",
        createdAt: "2026-07-24T01:00:00Z"
      },
      {
        databaseId: 202,
        status: "completed",
        conclusion: "success",
        headSha: $sha,
        headBranch: "main",
        event: "workflow_dispatch",
        createdAt: "2026-07-24T02:00:00Z"
      }
    ]'
  )"
  (
    cd "$ci_fixture_tmp/repo"
    PATH="$ci_fixture_tmp/bin:$PATH" \
    GITHUB_REPOSITORY="o87110/sub2api-custom-public" \
    MOCK_TAG_OID="$fixture_commit" \
    MOCK_CI_RUNS_JSON="$older_failure_newer_success" \
      /bin/bash "$preflight" \
        --tag "$fixture_tag" \
        --control-commit "$fixture_commit" \
        --output "$ci_fixture_tmp/accepted-output"
  )
  grep -Fq 'ci_run_id=202' "$ci_fixture_tmp/accepted-output"
  rm -rf "$ci_fixture_tmp"
fi

policy_tmp="$(mktemp -d)"
printf '%s\t%s\t%s\t%s\n' \
  manifest-sha payload-ref 101 "$policy_tmp/manifest-a.json" \
  manifest-sha payload-ref 202 "$policy_tmp/manifest-b.json" \
  > "$policy_tmp/consistent.tsv"
[[ "$(
  select_consistent_manifest_candidate "$policy_tmp/consistent.tsv"
)" == "$policy_tmp/manifest-a.json" ]]
printf '%s\t%s\t%s\t%s\n' \
  manifest-sha-a payload-ref-a 101 "$policy_tmp/manifest-a.json" \
  manifest-sha-b payload-ref-b 202 "$policy_tmp/manifest-b.json" \
  > "$policy_tmp/conflicting.tsv"
if select_consistent_manifest_candidate "$policy_tmp/conflicting.tsv" >/dev/null 2>&1; then
  fail "conflicting manifest candidates were accepted"
fi
rm -rf "$policy_tmp"

draft_create_line="$(grep -nF 'gh release create "$tag"' "$publisher" | cut -d: -f1)"
draft_manifest_line="$(grep -nF 'gh release upload "$tag" "$manifest_upload"' "$publisher" | cut -d: -f1)"
archive_loop_line="$(grep -nF 'read -r name expected_sha; do' "$publisher" | tail -n 1 | cut -d: -f1)"
[[ "$draft_create_line" -lt "$draft_manifest_line" &&
   "$draft_manifest_line" -lt "$archive_loop_line" ]] ||
  fail "Draft must be created before its manifest, and the manifest before archive assets"

grep -Fq 'remote Tag ref OID changed' "$preflight"
grep -Fq 'local_tag_ref="refs/tags/${tag}"' "$preflight"
grep -Fq 'git rev-parse "${local_tag_ref}^{commit}"' "$preflight"
fail_if_present \
  "Release preflight must not retain checkout credentials for extra fetches" \
  'git fetch origin' \
  "$preflight"
grep -Fq -- '--workflow backend-ci.yml' "$preflight"
grep -Fq '.headBranch == "main"' "$preflight"
grep -Fq '.event == "push" or .event == "workflow_dispatch"' "$preflight"
grep -Fq -- '--json databaseId,status,conclusion,headSha,headBranch,event,createdAt' "$preflight"
grep -Fq 'sort_by(.createdAt, .databaseId)' "$preflight"
grep -Fq 'last |' "$preflight"
fail_if_present \
  "Release preflight must not discard newer failed CI runs before selecting the latest run" \
  '.conclusion == "success"' \
  "$preflight"
grep -Fq '.name == "boundaries"' "$preflight"
grep -Fq 'require_successful_ci "$control_commit" "control commit $control_commit"' "$preflight"
grep -Fq 'control_ci_run_id=$control_ci_state' "$preflight"
grep -Fq 'published Release has no authoritative release manifest' "$preflight"
grep -Fq 'Tag ref OID changed during Release preflight' "$preflight"
grep -Fq 'if (.draft | type) == "boolean" then' "$preflight"
fail_if_present \
  "published Release metadata must not make jq reject the false draft value" \
  "jq -er '.draft'" \
  "$preflight"

grep -Fq 'workflow=backend-ci.yml' "$publish_workflow"
grep -Fq '.headBranch == "main"' "$ci_waiter"
grep -Fq '.name == "boundaries"' "$ci_waiter"
grep -Fq 'source .github/custom-upstream-baseline.env' "$publish_workflow"
grep -Fq 'vendor_tag" != "$declared_vendor_tag' "$publish_workflow"
grep -Fq 'vendor_commit" != "$CUSTOM_UPSTREAM_BASE_COMMIT' "$publish_workflow"
grep -Fq 'main changed from ${target_sha} to ${latest_target_sha} before Tag creation' "$publish_workflow"
grep -Fq '"$(git rev-parse "$current_vendor_ref")" != "$vendor_ref_oid"' "$publish_workflow"
grep -Fq '"$(git rev-parse "$official_ref")" != "$official_ref_oid"' "$publish_workflow"
grep -Fq 'queue_release "$existing"' "$publish_workflow"
grep -Fq 'Latest immutable Tag ${latest_custom} has no published Release; retrying it before creating a new Tag.' \
  "$publish_workflow"
grep -Fq 'git merge-base --is-ancestor "${latest_tag_ref}^{commit}" origin/main' \
  "$publish_workflow"
grep -Fq 'queue_release "$latest_custom"' "$publish_workflow"
grep -Fq 'git update-index --assume-unchanged -- "${compatibility_files[@]}"' "$release_workflow"
grep -Fq 'git update-index --no-assume-unchanged -- "${compatibility_files[@]}"' "$release_workflow"
grep -Fq 'git restore --source=HEAD --worktree -- "${compatibility_files[@]}"' "$release_workflow"
grep -Fq 'git hash-object --path "$version_file" "$version_file"' "$release_workflow"
grep -Fq 'git hash-object --path "$go_sum_file" "$go_sum_file"' "$release_workflow"
grep -Fq 'CONTROL_COMMIT: ${{ github.sha }}' "$release_workflow"
grep -Fq '[[ "$GITHUB_REF" == "refs/heads/main" ]]' "$release_workflow"
grep -Fq 'persist-credentials: false' "$release_workflow"
grep -Fq 'control_ref="refs/remotes/origin/main"' "$release_workflow"
grep -Fq 'git merge-base --is-ancestor "$CONTROL_COMMIT" "$control_ref"' "$release_workflow"
context_job="$(
  sed -n '/^  context:/,/^  build:/p' "$release_workflow"
)"
build_job="$(
  sed -n '/^  build:/,/^  publish:/p' "$release_workflow"
)"
publish_job="$(
  sed -n '/^  publish:/,/^  report-release-failure:/p' "$release_workflow"
)"
grep -Fq 'contents: write' <<<"$context_job"
grep -Fq 'persist-credentials: false' <<<"$context_job"
grep -Fq 'persist-credentials: false' <<<"$build_job"
grep -Fq 'persist-credentials: false' <<<"$publish_job"
fail_if_present \
  "Release Tag checkouts must not persist job credentials" \
  'persist-credentials: true' \
  "$release_workflow"
grep -Fq 'git show "${CONTROL_COMMIT}:deploy/release/custom-release-preflight.sh"' "$release_workflow"
grep -Fq '/bin/bash "$RUNNER_TEMP/release-control/custom-release-preflight.sh"' "$release_workflow"
grep -Fq -- '--control-commit "$CONTROL_COMMIT"' "$release_workflow"
grep -Fq 'git show "${CONTROL_COMMIT}:deploy/release/create-release-manifest.sh"' "$release_workflow"
grep -Fq 'GH_TOKEN: ${{ github.token }}' <<<"$build_job"
grep -Fq '"repos/${GITHUB_REPOSITORY}/git/ref/heads/main"' <<<"$build_job"
if grep -Fq 'git fetch origin' <<<"$build_job"; then
  fail "Release build must not depend on persisted checkout credentials for control ref refresh"
fi
grep -Fq '/bin/bash "$control_dir/publish-release.sh"' "$release_workflow"
grep -Fq \
  'deploy/release/release-notes.sh \' \
  "$release_workflow"
grep -Fq \
  '"$control_dir/release-notes.sh" \' \
  "$release_workflow"
grep -Fq '"repos/${GITHUB_REPOSITORY}/git/ref/heads/main"' <<<"$publish_job"
if grep -Fq 'git fetch origin' <<<"$publish_job"; then
  fail "Release publish must not depend on persisted checkout credentials for control ref refresh"
fi
fail_if_present \
  "current payload builder must not mutate the embedded VERSION file" \
  'printf '\''%s\n'\'' "$version" > "$repo_root/backend/cmd/server/VERSION"' \
  "$payload_builder"
fail_if_present \
  "GoReleaser must not rewrite module metadata during a release build" \
  'go mod tidy -C backend' \
  "$goreleaser"
fail_if_present \
  "Security Scan must remain independent of automatic publication" \
  'security-scan.yml' \
  "$publish_workflow"
fail_if_present \
  "required publication checks must not have a bypass input" \
  'bypass_checks' \
  "$publish_workflow"

grep -Fq 'make build-backend' "$backend_ci_workflow"
grep -Fq 'make build-frontend' "$backend_ci_workflow"
grep -Fq 'custom-upstream-delta-test.sh' "$backend_ci_workflow"
grep -Fq 'custom-database-boundary-test.sh' "$backend_ci_workflow"
grep -Fq 'custom-actions-supply-chain-test.sh' "$backend_ci_workflow"
grep -Fq 'CUSTOM_ACTIONS_REQUIRE_ACTIONLINT: '\''true'\''' "$backend_ci_workflow"

fail_if_present \
  "pnpm audit execution status must not be discarded" \
  'pnpm audit --prod --audit-level=high --json > audit.json || true' \
  "$security_workflow"
grep -Fq 'audit_exit_code=$?' "$security_workflow"
grep -Fq 'validate_pnpm_audit.py' "$security_workflow"
grep -Fq "if: steps.pnpm-audit.outputs.audit_has_vulnerabilities == 'true'" "$security_workflow"

grep -Fq 'CUSTOM_UPSTREAM_BASE_COMMIT' "$upgrade_gate_workflow"
grep -Fq 'database_fingerprint' "$upgrade_gate_workflow"
grep -Fq 'custom-database-boundary-test.sh' "$upgrade_gate_workflow"
grep -Fq 'goreleaser check' "$upgrade_gate_workflow"
fail_if_present \
  "custom upgrade preflight must not use a GoReleaser version range" \
  "version: '~> v2'" \
  "$upgrade_gate_workflow"

grep -Fq 'ORAS_VERSION=' "$tool_versions"
grep -Fq 'GORELEASER_VERSION=' "$tool_versions"
grep -Fq 'BUILDX_VERSION=' "$tool_versions"
grep -Eq '^QEMU_BINFMT_IMAGE=.+@sha256:[0-9a-f]{64}$' "$tool_versions"
grep -Fq 'NODE_VERSION=20.20.2' "$tool_versions"
grep -Fq 'PNPM_VERSION=9.15.9' "$tool_versions"

if command -v jq >/dev/null 2>&1; then
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' EXIT
  mkdir -p "$tmp_dir/payload"
  cat > "$tmp_dir/payload/build-metadata.json" <<'EOF'
{
  "schema": "sub2api-custom-payload/v1",
  "tag": "v0.1.162-custom.99",
  "target_commit": "1111111111111111111111111111111111111111",
  "payload_content_sha256": "2222222222222222222222222222222222222222222222222222222222222222",
  "assets": [
    {"name": "sub2api_0.1.162-custom.99_linux_amd64.tar.gz", "sha256": "3333333333333333333333333333333333333333333333333333333333333333"},
    {"name": "sub2api_0.1.162-custom.99_linux_arm64.tar.gz", "sha256": "4444444444444444444444444444444444444444444444444444444444444444"},
    {"name": "checksums.txt", "sha256": "5555555555555555555555555555555555555555555555555555555555555555"}
  ],
  "oci": {
    "index_digest": "sha256:6666666666666666666666666666666666666666666666666666666666666666",
    "index_media_type": "application/vnd.oci.image.index.v1+json",
    "manifests": [
      {
        "os": "linux",
        "architecture": "amd64",
        "variant": "",
        "media_type": "application/vnd.oci.image.manifest.v1+json",
        "digest": "sha256:7777777777777777777777777777777777777777777777777777777777777777"
      },
      {
        "os": "linux",
        "architecture": "arm64",
        "variant": "v8",
        "media_type": "application/vnd.oci.image.manifest.v1+json",
        "digest": "sha256:8888888888888888888888888888888888888888888888888888888888888888"
      }
    ]
  }
}
EOF
  GITHUB_RUN_ID=123 \
  GITHUB_RUN_ATTEMPT=4 \
  GITHUB_EVENT_NAME=workflow_dispatch \
  GITHUB_SHA=9999999999999999999999999999999999999999 \
  GITHUB_REF=refs/heads/main \
  GITHUB_REPOSITORY=o87110/sub2api-custom-public \
    /bin/bash "$manifest_builder" \
      --payload "$tmp_dir/payload" \
      --output "$tmp_dir/release-manifest.json" \
      --tag-ref-oid aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
      --artifact-id 456 \
      --artifact-digest sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb \
      --artifact-name release-payload-v0.1.162-custom.99-123-4
  jq -e '
    .producer.run_id == 123 and
    .producer.run_attempt == 4 and
    .producer.workflow_ref == "refs/heads/main" and
    .producer.workflow_commit == "9999999999999999999999999999999999999999" and
    .payload_artifact.id == 456 and
    .payload_artifact.digest == "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" and
    .oci.index_media_type == "application/vnd.oci.image.index.v1+json" and
    ([.oci.manifests[].media_type] | unique) ==
      ["application/vnd.oci.image.manifest.v1+json"] and
    (.producer | has("artifact_id") | not) and
    (.producer | has("artifact_digest") | not)
  ' "$tmp_dir/release-manifest.json" >/dev/null
else
  echo "jq not found; manifest fixture deferred to Linux CI"
fi

echo "custom release safety checks passed"
