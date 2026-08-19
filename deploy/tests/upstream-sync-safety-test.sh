#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
sync_workflow="$repo_root/.github/workflows/upstream-sync.yml"
gate_workflow="$repo_root/.github/workflows/upstream-upgrade-gate.yml"
shadow_map="$repo_root/.github/upstream-shadowed-sources.tsv"
shadow_map_test="$repo_root/deploy/tests/upstream-shadow-map-test.sh"
delta_test="$repo_root/deploy/tests/custom-upstream-delta-test.sh"
database_test="$repo_root/deploy/tests/custom-database-boundary-test.sh"
candidate_tree_script="$repo_root/deploy/tests/custom-candidate-tree.sh"
report_maintenance_policy="$repo_root/deploy/upstream/validate-upgrade-report-maintenance.sh"
codeowners="$repo_root/.github/CODEOWNERS"
makefile="$repo_root/Makefile"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}
trap 'fail "unexpected command failure at line $LINENO"' ERR

# Historical official v0.1.161 commit used by fixed transition fixtures. The
# public bootstrap intentionally carries only the current vendor tag.
previous_vendor_commit="19149ca196eeae4a4482e5299dc6fa4ba0b06c8c"
git -C "$repo_root" rev-parse --verify "${previous_vendor_commit}^{commit}" >/dev/null ||
  fail "historical v0.1.161 fixture commit is unavailable"
git -C "$repo_root" merge-base --is-ancestor \
  "$previous_vendor_commit" "vendor-0.1.162^{commit}" ||
  fail "historical v0.1.161 fixture is not an ancestor of vendor-0.1.162"

fail_if_present() {
  local description="$1"
  local pattern="$2"
  shift 2
  if grep -nF -- "$pattern" "$@"; then
    echo "ERROR: $description" >&2
    exit 1
  fi
}

expect_map_failure() {
  local description="$1"
  local fixture="$2"
  if UPSTREAM_SHADOW_MAP="$fixture" \
    UPSTREAM_SHADOW_SKIP_BOUNDARY_TEST=true \
    /bin/bash "$shadow_map_test" >/dev/null 2>&1; then
    echo "ERROR: map validation unexpectedly accepted $description" >&2
    exit 1
  fi
}

expect_map_success() {
  local description="$1"
  shift
  if ! "$@" >/dev/null 2>&1; then
    echo "ERROR: map validation unexpectedly rejected $description" >&2
    exit 1
  fi
}

expect_boundary_failure() {
  local description="$1"
  local fixture_root="$2"
  if UPSTREAM_SHADOW_BOUNDARY_ROOT="$fixture_root" \
    /bin/bash "$shadow_map_test" >/dev/null 2>&1; then
    echo "ERROR: shadow runtime boundary unexpectedly accepted $description" >&2
    exit 1
  fi
}

fail_if_present \
  "scheduled synchronization must not reset a persistent AI handoff branch" \
  'git switch -C "$branch"' \
  "$sync_workflow"
fail_if_present \
  "scheduled synchronization must not force-push a persistent AI handoff branch" \
  'git push --force-with-lease origin "$branch"' \
  "$sync_workflow"
fail_if_present \
  "the detector must not publish a custom Release before the PR gate passes" \
  'gh workflow run release.yml' \
  "$sync_workflow"
fail_if_present \
  "the upgrade gate must run the repository unit and integration suites" \
  'run: go test ./...' \
  "$gate_workflow"
fail_if_present \
  "the upgrade gate must not dispatch the Release builder directly" \
  'gh workflow run release.yml' \
  "$gate_workflow"
fail_if_present \
  "generated Wire graph must not instantiate the upstream updater" \
  'service.ProvideUpdateService' \
  "$repo_root/backend/cmd/server/wire_gen.go"
fail_if_present \
  "generated Wire graph must not instantiate the upstream GitHub release client" \
  'repository.ProvideGitHubReleaseClient' \
  "$repo_root/backend/cmd/server/wire_gen.go"
grep -Fq \
  'src/views/user/__tests__/KeysView.spec.ts \' \
  "$makefile"
[[ -s "$report_maintenance_policy" ]] ||
  fail "released upgrade report maintenance policy is missing"
/bin/bash -n "$report_maintenance_policy"

tmp_dir="$(mktemp -d)"
temporary_baseline_ref=""
temporary_baseline_commit=""
cleanup() {
  if [[ -n "$temporary_baseline_ref" ]]; then
    git -C "$repo_root" update-ref \
      -d "$temporary_baseline_ref" "$temporary_baseline_commit"
  fi
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

base_fixture_commit="$(git -C "$repo_root" rev-parse 'origin/main^{commit}')"
base_report_path=".github/upgrades/0.1.165.md"
base_report_blob="$(
  git -C "$repo_root" ls-tree "$base_fixture_commit" -- "$base_report_path" |
    awk 'NR == 1 { print $3 } END { if (NR != 1) exit 1 }'
)"
git -C "$repo_root" cat-file blob "$base_report_blob" \
  > "$tmp_dir/base-upgrade-report.md"
sed \
  's/^- State: .*/- State: `released`/' \
  "$tmp_dir/base-upgrade-report.md" \
  > "$tmp_dir/maintained-upgrade-report.md"
sed \
  's/^- Official target commit: .*/- Official target commit: `0000000000000000000000000000000000000000`/' \
  "$tmp_dir/maintained-upgrade-report.md" \
  > "$tmp_dir/changed-identity-upgrade-report.md"

fixture_counter=0
make_report_fixture_commit() {
  local action="$1"
  local path="$2"
  local content_file="${3:-}"
  local fixture_index fixture_blob fixture_tree
  fixture_counter=$((fixture_counter + 1))
  fixture_index="$tmp_dir/report-index-${fixture_counter}"
  GIT_INDEX_FILE="$fixture_index" \
    git -C "$repo_root" read-tree "${base_fixture_commit}^{tree}"
  case "$action" in
    update)
      fixture_blob="$(git -C "$repo_root" hash-object -w -- "$content_file")"
      GIT_INDEX_FILE="$fixture_index" \
        git -C "$repo_root" update-index \
          --add \
          --cacheinfo 100644 "$fixture_blob" "$path"
      ;;
    delete)
      GIT_INDEX_FILE="$fixture_index" \
        git -C "$repo_root" update-index --force-remove -- "$path"
      ;;
    *)
      fail "unsupported report fixture action: $action"
      ;;
  esac
  fixture_tree="$(
    GIT_INDEX_FILE="$fixture_index" git -C "$repo_root" write-tree
  )"
  printf 'upgrade report maintenance fixture\n' |
    GIT_AUTHOR_NAME=fixture \
    GIT_AUTHOR_EMAIL=fixture@example.invalid \
    GIT_COMMITTER_NAME=fixture \
    GIT_COMMITTER_EMAIL=fixture@example.invalid \
      git -C "$repo_root" commit-tree \
        "$fixture_tree" \
        -p "$base_fixture_commit"
}

maintained_report_commit="$(
  make_report_fixture_commit \
    update \
    "$base_report_path" \
    "$tmp_dir/maintained-upgrade-report.md"
)"
changed_identity_commit="$(
  make_report_fixture_commit \
    update \
    "$base_report_path" \
    "$tmp_dir/changed-identity-upgrade-report.md"
)"
added_report_commit="$(
  make_report_fixture_commit \
    update \
    ".github/upgrades/9.9.9.md" \
    "$tmp_dir/maintained-upgrade-report.md"
)"
deleted_report_commit="$(
  make_report_fixture_commit delete "$base_report_path"
)"

/bin/bash "$report_maintenance_policy" \
  --base-ref "$base_fixture_commit" \
  --target-ref "$maintained_report_commit" \
  --vendor-ref-prefix refs/tags/ \
  >/dev/null ||
  fail "released upgrade report maintenance was rejected"
cp "$report_maintenance_policy" "$tmp_dir/copied-report-maintenance-policy.sh"
GITHUB_WORKSPACE="$repo_root" \
  /bin/bash "$tmp_dir/copied-report-maintenance-policy.sh" \
    --base-ref "$base_fixture_commit" \
    --target-ref "$maintained_report_commit" \
    --vendor-ref-prefix refs/tags/ \
    >/dev/null ||
  fail "trusted copied report maintenance policy lost the repository root"
for rejected_report_commit in \
  "$changed_identity_commit" \
  "$added_report_commit" \
  "$deleted_report_commit"; do
  if /bin/bash "$report_maintenance_policy" \
    --base-ref "$base_fixture_commit" \
    --target-ref "$rejected_report_commit" \
    --vendor-ref-prefix refs/tags/ \
    >/dev/null 2>&1; then
    fail "unsafe upgrade report maintenance was accepted"
  fi
done

extract_protected_pattern() {
  local workflow="$1"
  local protected_pattern appended_pattern group_model_access_protected_pattern
  protected_pattern="$(sed -n "s/^[[:space:]]*protected_pattern='\(.*\)'$/\1/p" "$workflow")"
  while IFS= read -r appended_pattern; do
    [[ -n "$appended_pattern" ]] || continue
    [[ "$appended_pattern" != *'${'* ]] || continue
    protected_pattern="${protected_pattern}|${appended_pattern}"
  done < <(sed -n 's/^[[:space:]]*protected_pattern="${protected_pattern}|\(.*\)"$/\1/p' "$workflow")
  group_model_access_protected_pattern="$(sed -n "s/^[[:space:]]*group_model_access_protected_pattern='\(.*\)'$/\1/p" "$workflow")"
  if [[ -n "$group_model_access_protected_pattern" ]]; then
    protected_pattern="${protected_pattern}|${group_model_access_protected_pattern}"
  fi
  printf '%s\n' "$protected_pattern"
}

sync_protected_pattern="$(extract_protected_pattern "$sync_workflow")"
gate_protected_pattern="$(extract_protected_pattern "$gate_workflow")"
[[ -n "$sync_protected_pattern" ]] || fail "sync protected path pattern is missing"
[[ -n "$gate_protected_pattern" ]] || fail "upgrade gate protected path pattern is missing"

checked_branch="${GITHUB_HEAD_REF:-${GITHUB_REF_NAME:-$(git -C "$repo_root" branch --show-current)}}"
if [[ "$checked_branch" == upgrade/* ]]; then
  git -C "$repo_root" show-ref --verify --quiet refs/remotes/origin/main ||
    fail "origin/main is unavailable while validating an upgrade branch"
  custom_ref=origin/main
  vendor_ref="$(git -C "$repo_root" tag --merged "$custom_ref" --list 'vendor-*' --sort=-version:refname | sed -n '1p')"
  [[ -n "$vendor_ref" ]] || fail "no merged vendor baseline is available for reverse test coverage"
  git -C "$repo_root" diff --name-only --no-renames "${vendor_ref}..${custom_ref}" -- \
    '*_test.go' \
    '*test*.ts' \
    '*test*.js' \
    '*.spec.ts' \
    '*.spec.js' \
    'deploy/tests/*.sh' > "$tmp_dir/custom-development-tests.txt"
else
  vendor_ref="$(git -C "$repo_root" tag --merged HEAD --list 'vendor-*' --sort=-version:refname | sed -n '1p')"
  [[ -n "$vendor_ref" ]] || fail "no merged vendor baseline is available for reverse test coverage"
  {
    git -C "$repo_root" diff --name-only --no-renames "$vendor_ref" -- \
      '*_test.go' \
      '*test*.ts' \
      '*test*.js' \
      '*.spec.ts' \
      '*.spec.js' \
      'deploy/tests/*.sh'
    git -C "$repo_root" ls-files --others --exclude-standard -- \
      '*_test.go' \
      '*test*.ts' \
      '*test*.js' \
      '*.spec.ts' \
      '*.spec.js' \
      'deploy/tests/*.sh'
  } | sort -u > "$tmp_dir/custom-development-tests.txt"
fi

grep -Ev '^(backend/internal/custom/|frontend/src/custom/)' \
  "$tmp_dir/custom-development-tests.txt" \
  > "$tmp_dir/non-custom-development-tests.txt" || true
[[ -s "$tmp_dir/non-custom-development-tests.txt" ]] ||
  fail "no non-custom-directory development tests were found for reverse coverage"

codeowners_covers() {
  local repository_path="/$1"
  local pattern owners prefix
  while read -r pattern owners _; do
    [[ "$owners" == *"@o87110"* ]] || continue
    if [[ "$pattern" == "$repository_path" ]]; then
      return 0
    fi
    if [[ "$pattern" == *'/**' ]]; then
      prefix="${pattern%/\*\*}"
      if [[ "$repository_path" == "$prefix"/* ]]; then
        return 0
      fi
    fi
  done < "$codeowners"
  return 1
}

while IFS= read -r test_path; do
  if ! grep -Eq "$sync_protected_pattern" <<<"$test_path"; then
    fail "sync protected path pattern misses custom-development test: $test_path"
  fi
  if ! grep -Eq "$gate_protected_pattern" <<<"$test_path"; then
    fail "upgrade gate protected path pattern misses custom-development test: $test_path"
  fi
  codeowners_covers "$test_path" ||
    fail "CODEOWNERS misses custom-development test: $test_path"
done < "$tmp_dir/non-custom-development-tests.txt"

while IFS= read -r protected_path; do
  if ! grep -Eq "$sync_protected_pattern" <<<"$protected_path"; then
    fail "sync protected path pattern misses custom runtime entry point: $protected_path"
  fi
  if ! grep -Eq "$gate_protected_pattern" <<<"$protected_path"; then
    fail "upgrade gate protected path pattern misses custom runtime entry point: $protected_path"
  fi
done <<'EOF'
backend/internal/handler/admin/channel_monitor_handler.go
backend/internal/handler/admin/payment_handler.go
backend/internal/handler/admin/setting_handler.go
backend/internal/handler/admin/setting_handler_update.go
backend/internal/handler/auth_wechat_oauth.go
backend/internal/handler/channel_monitor_user_handler.go
backend/internal/handler/dto/settings.go
backend/internal/handler/payment_handler.go
backend/internal/payment/load_balancer.go
backend/internal/payment/types.go
backend/internal/repository/channel_monitor_repo.go
backend/internal/service/channel_monitor_aggregator.go
backend/internal/service/channel_monitor_const.go
backend/internal/service/channel_monitor_service.go
backend/internal/service/channel_monitor_types.go
backend/internal/service/payment_config_limits.go
backend/internal/service/payment_config_service.go
backend/internal/service/payment_order.go
backend/internal/service/payment_resume_service.go
backend/internal/service/payment_resume_service_test.go
backend/internal/service/payment_service.go
frontend/src/api/channelMonitor.ts
frontend/src/api/admin/channelMonitor.ts
frontend/src/api/admin/payment.ts
frontend/src/api/admin/settings.ts
frontend/src/components/admin/monitor/MonitorFormDialog.vue
frontend/src/components/payment/paymentFlow.ts
frontend/src/components/payment/AmountInput.vue
frontend/src/components/payment/PaymentProviderDialog.vue
frontend/src/components/user/monitor/MonitorCard.vue
frontend/src/i18n/locales/en/admin/channels.ts
frontend/src/i18n/locales/en/admin/settings.ts
frontend/src/i18n/locales/zh/admin/channels.ts
frontend/src/i18n/locales/zh/admin/settings.ts
frontend/src/types/payment.ts
frontend/src/views/admin/SettingsView.vue
frontend/src/views/auth/__tests__/WechatPaymentCallbackView.spec.ts
frontend/src/views/user/KeysView.vue
frontend/src/views/user/PaymentView.vue
frontend/src/views/user/__tests__/paymentWechatResume.spec.ts
frontend/src/views/user/paymentWechatResume.ts
EOF

/bin/bash "$shadow_map_test"
grep -Fqx '/.github/upstream-shadowed-sources.tsv @o87110' "$codeowners"
grep -Fqx '/.github/workflows/** @o87110' "$codeowners"
grep -Fqx '/backend/internal/custom/** @o87110' "$codeowners"
grep -Fqx '/frontend/src/custom/** @o87110' "$codeowners"
grep -Fqx '/deploy/tests/upstream-shadow-map-test.sh @o87110' "$codeowners"
grep -Fqx '/deploy/tests/upstream-sync-safety-test.sh @o87110' "$codeowners"
grep -Fqx '/deploy/tests/custom-release-safety-test.sh @o87110' "$codeowners"
grep -Fqx '/deploy/tests/docker-entrypoint-runtime-test.sh @o87110' "$codeowners"
while IFS= read -r owned_path; do
  grep -Fqx "${owned_path} @o87110" "$codeowners" ||
    fail "CODEOWNERS misses protected custom runtime entry point: $owned_path"
done <<'EOF'
/Dockerfile
/Dockerfile.goreleaser
/.goreleaser.yaml
/.goreleaser.simple.yaml
/backend/cmd/server/wire.go
/backend/cmd/server/wire_gen.go
/backend/internal/config/config.go
/backend/internal/handler/wire.go
/backend/internal/handler/channel_monitor_user_handler.go
/backend/internal/handler/admin/channel_monitor_handler.go
/backend/internal/handler/admin/content_moderation_handler.go
/backend/internal/handler/admin/payment_handler.go
/backend/internal/handler/admin/setting_handler.go
/backend/internal/handler/admin/setting_handler_update.go
/backend/internal/handler/admin/system_handler.go
/backend/internal/handler/auth_wechat_oauth.go
/backend/internal/handler/dto/settings.go
/backend/internal/handler/openai_gateway_handler.go
/backend/internal/handler/payment_handler.go
/backend/internal/payment/load_balancer.go
/backend/internal/payment/types.go
/backend/internal/repository/channel_monitor_repo.go
/backend/internal/repository/content_moderation_repo.go
/backend/internal/service/channel_monitor_aggregator.go
/backend/internal/service/channel_monitor_const.go
/backend/internal/service/channel_monitor_service.go
/backend/internal/service/channel_monitor_types.go
/backend/internal/service/content_moderation.go
/backend/internal/service/custom_moderation_bridge.go
/backend/internal/service/payment_config_limits.go
/backend/internal/service/payment_config_service.go
/backend/internal/service/payment_order.go
/backend/internal/service/payment_resume_service.go
/backend/internal/service/payment_resume_service_test.go
/backend/internal/service/payment_service.go
/deploy/docker-compose.custom.yml
/deploy/docker-entrypoint.sh
/frontend/src/api/channelMonitor.ts
/frontend/src/api/admin/channelMonitor.ts
/frontend/src/api/admin/payment.ts
/frontend/src/api/admin/settings.ts
/frontend/src/components/admin/monitor/MonitorFormDialog.vue
/frontend/src/components/layout/AppSidebar.vue
/frontend/src/components/payment/AmountInput.vue
/frontend/src/components/payment/PaymentProviderDialog.vue
/frontend/src/components/payment/paymentFlow.ts
/frontend/src/components/user/monitor/MonitorCard.vue
/frontend/src/i18n/locales/en/admin/channels.ts
/frontend/src/i18n/locales/en/admin/settings.ts
/frontend/src/i18n/locales/zh/admin/channels.ts
/frontend/src/i18n/locales/zh/admin/settings.ts
/frontend/src/router/index.ts
/frontend/src/types/payment.ts
/frontend/src/views/admin/SettingsView.vue
/frontend/src/views/auth/__tests__/WechatPaymentCallbackView.spec.ts
/frontend/src/views/user/KeysView.vue
/frontend/src/views/user/PaymentView.vue
/frontend/src/views/user/__tests__/paymentWechatResume.spec.ts
/frontend/src/views/user/paymentWechatResume.ts
EOF

awk 'BEGIN { FS=OFS="\t" } NR == 2 { print $1, $2, "extra"; next } { print }' \
  "$shadow_map" > "$tmp_dir/invalid-fields.tsv"
expect_map_failure "a row with extra fields" "$tmp_dir/invalid-fields.tsv"

awk 'BEGIN { FS=OFS="\t" } NR == 2 { $2="backend/internal/custom/missing.go" } { print }' \
  "$shadow_map" > "$tmp_dir/missing-target.tsv"
expect_map_failure "a missing target" "$tmp_dir/missing-target.tsv"

awk 'BEGIN { FS=OFS="\t" } NR == 2 { $2="../../escape.go" } { print }' \
  "$shadow_map" > "$tmp_dir/unsafe-target.tsv"
expect_map_failure "an unsafe target" "$tmp_dir/unsafe-target.tsv"

first_source="$(awk -F '\t' '$1 !~ /^#/ { print $1; exit }' "$shadow_map")"
companion_source='backend/internal/service/content_moderation_companion.go'
negative_companion='backend/internal/service/not_content_moderation_companion.go'
printf '%s\n%s\n%s\n%s\n' \
  "$first_source" \
  "$companion_source" \
  "$negative_companion" \
  'unmapped/workflow-fixture.txt' > "$tmp_dir/official-diff.txt"
UPSTREAM_SHADOW_OFFICIAL_FILES="$tmp_dir/official-diff.txt" \
  UPSTREAM_SHADOW_DETECTED_OUTPUT="$tmp_dir/detected-shadow-paths.txt" \
  UPSTREAM_SHADOW_SKIP_BOUNDARY_TEST=true \
  /bin/bash "$shadow_map_test" >/dev/null
grep -Fqx "$first_source" "$tmp_dir/detected-shadow-paths.txt"
grep -Fqx "$companion_source" "$tmp_dir/detected-shadow-paths.txt"
if grep -Fqx "$negative_companion" "$tmp_dir/detected-shadow-paths.txt"; then
  echo "ERROR: shared shadow detector matched a non-family companion fixture" >&2
  exit 1
fi
if grep -Fqx 'unmapped/workflow-fixture.txt' "$tmp_dir/detected-shadow-paths.txt"; then
  echo "ERROR: shared shadow detector matched an unmapped workflow fixture" >&2
  exit 1
fi

mkdir -p "$tmp_dir/shadow-boundary/frontend/src/components/layout"
cat > "$tmp_dir/shadow-boundary/frontend/src/components/layout/AppHeader.vue" <<'EOF'
<script setup lang="ts">
import VersionBadge from '@/components/common/VersionBadge.vue'
</script>
EOF
expect_boundary_failure \
  "a new caller of the official VersionBadge" \
  "$tmp_dir/shadow-boundary"

mkdir -p "$tmp_dir/shadow-boundary-destructure/frontend/src/components/layout"
cat > "$tmp_dir/shadow-boundary-destructure/frontend/src/components/layout/AppHeader.vue" <<'EOF'
<script setup lang="ts">
import { useAppStore } from '@/stores/app'

const updaterStore = useAppStore()
const { fetchVersion } = updaterStore
void fetchVersion
</script>
EOF
expect_boundary_failure \
  "a destructured updater store call" \
  "$tmp_dir/shadow-boundary-destructure"

mkdir -p "$tmp_dir/shadow-boundary-alias/backend/cmd/server"
cat > "$tmp_dir/shadow-boundary-alias/backend/cmd/server/legacy_updater.go" <<'EOF'
package server

import updatesvc "github.com/Wei-Shaw/sub2api/internal/service"

var legacyUpdateFactory = updatesvc.NewUpdateService
EOF
expect_boundary_failure \
  "an aliased legacy updater factory call" \
  "$tmp_dir/shadow-boundary-alias"

mkdir -p "$tmp_dir/shadow-boundary-same-package/backend/internal/service"
cat > "$tmp_dir/shadow-boundary-same-package/backend/internal/service/new_caller.go" <<'EOF'
package service

var legacyUpdateFactory = NewUpdateService
EOF
expect_boundary_failure \
  "a same-package legacy updater factory call" \
  "$tmp_dir/shadow-boundary-same-package"
awk -v source="$first_source" 'BEGIN { FS=OFS="\t" } $1 !~ /^#/ { count++ } count == 22 { $1=source } { print }' \
  "$shadow_map" > "$tmp_dir/duplicate-source.tsv"
expect_map_failure "a duplicate source" "$tmp_dir/duplicate-source.tsv"

awk 'BEGIN { FS=OFS="\t" } NR == 2 { $1="backend/internal/service/not-present.go" } { print }' \
  "$shadow_map" > "$tmp_dir/missing-source.tsv"
expect_map_failure "a source absent from the vendor baseline" "$tmp_dir/missing-source.tsv"

sed '$d' "$shadow_map" > "$tmp_dir/shrunken-map.tsv"
expect_map_failure "an accidentally shrunken map" "$tmp_dir/shrunken-map.tsv"

transition_target="$(awk -F '\t' '$1 !~ /^#/ { print $2; exit }' "$shadow_map")"
printf '%s\n%s\t%s\n' \
  '# Official source alternatives	Active custom target' \
  'frontend/public/logo.png|frontend/public/logo.svg' \
  "$transition_target" > "$tmp_dir/rename-transition.tsv"
expect_map_success \
  "an old/new source transition against the old vendor baseline" \
  env \
    UPSTREAM_SHADOW_MAP="$tmp_dir/rename-transition.tsv" \
    UPSTREAM_SHADOW_EXPECTED_COUNT=1 \
    UPSTREAM_SHADOW_BASE_REF="$previous_vendor_commit" \
    UPSTREAM_SHADOW_NEXT_REF=vendor-0.1.162 \
    UPSTREAM_SHADOW_SKIP_DETECTOR_TEST=true \
    UPSTREAM_SHADOW_SKIP_BOUNDARY_TEST=true \
    /bin/bash "$shadow_map_test"
expect_map_success \
  "the same source transition after the new vendor baseline is established" \
  env \
    UPSTREAM_SHADOW_MAP="$tmp_dir/rename-transition.tsv" \
    UPSTREAM_SHADOW_EXPECTED_COUNT=1 \
    UPSTREAM_SHADOW_BASE_REF=vendor-0.1.162 \
    UPSTREAM_SHADOW_SKIP_DETECTOR_TEST=true \
    UPSTREAM_SHADOW_SKIP_BOUNDARY_TEST=true \
    /bin/bash "$shadow_map_test"

printf '%s\n%s\t%s\n' \
  '# Official source alternatives	Active custom target' \
  'frontend/public/logo.png|frontend/public/not-the-renamed-logo.svg' \
  "$transition_target" > "$tmp_dir/unresolved-transition.tsv"
if UPSTREAM_SHADOW_MAP="$tmp_dir/unresolved-transition.tsv" \
   UPSTREAM_SHADOW_EXPECTED_COUNT=1 \
   UPSTREAM_SHADOW_BASE_REF="$previous_vendor_commit" \
   UPSTREAM_SHADOW_NEXT_REF=vendor-0.1.162 \
   UPSTREAM_SHADOW_SKIP_BOUNDARY_TEST=true \
   /bin/bash "$shadow_map_test" >/dev/null 2>&1; then
  echo "ERROR: upgrade validation accepted an unresolved source transition" >&2
  exit 1
fi

printf '%s\n%s\t%s\n' \
  '# Official source deletion	Active custom target' \
  'frontend/public/logo.png|@removed' \
  "$transition_target" > "$tmp_dir/source-deletion.tsv"
expect_map_success \
  "a source deletion verified against the target vendor" \
  env \
    UPSTREAM_SHADOW_MAP="$tmp_dir/source-deletion.tsv" \
    UPSTREAM_SHADOW_EXPECTED_COUNT=1 \
    UPSTREAM_SHADOW_BASE_REF="$previous_vendor_commit" \
    UPSTREAM_SHADOW_NEXT_REF=vendor-0.1.162 \
    UPSTREAM_SHADOW_SKIP_DETECTOR_TEST=true \
    UPSTREAM_SHADOW_SKIP_BOUNDARY_TEST=true \
    /bin/bash "$shadow_map_test"
expect_map_success \
  "the retired source after the new vendor baseline is established" \
  env \
    UPSTREAM_SHADOW_MAP="$tmp_dir/source-deletion.tsv" \
    UPSTREAM_SHADOW_EXPECTED_COUNT=1 \
    UPSTREAM_SHADOW_BASE_REF=vendor-0.1.162 \
    UPSTREAM_SHADOW_SKIP_DETECTOR_TEST=true \
    UPSTREAM_SHADOW_SKIP_BOUNDARY_TEST=true \
    /bin/bash "$shadow_map_test"

printf '%s\n%s\t%s\n' \
  '# Unacknowledged source deletion	Active custom target' \
  'frontend/public/logo.png' \
  "$transition_target" > "$tmp_dir/unacknowledged-deletion.tsv"
if UPSTREAM_SHADOW_MAP="$tmp_dir/unacknowledged-deletion.tsv" \
   UPSTREAM_SHADOW_EXPECTED_COUNT=1 \
   UPSTREAM_SHADOW_BASE_REF="$previous_vendor_commit" \
   UPSTREAM_SHADOW_NEXT_REF=vendor-0.1.162 \
   UPSTREAM_SHADOW_SKIP_BOUNDARY_TEST=true \
   /bin/bash "$shadow_map_test" >/dev/null 2>&1; then
  echo "ERROR: upgrade validation accepted a source deletion without @removed" >&2
  exit 1
fi

printf '%s\n%s\t%s\n' \
  '# Persistent source tombstone	Active custom target' \
  'frontend/public/logo.svg|@removed' \
  "$transition_target" > "$tmp_dir/reintroduced-source.tsv"
expect_map_success \
  "a tombstoned path that reappears in a later official version" \
  env \
    UPSTREAM_SHADOW_MAP="$tmp_dir/reintroduced-source.tsv" \
    UPSTREAM_SHADOW_EXPECTED_COUNT=1 \
    UPSTREAM_SHADOW_BASE_REF="$previous_vendor_commit" \
    UPSTREAM_SHADOW_NEXT_REF=vendor-0.1.162 \
    UPSTREAM_SHADOW_SKIP_DETECTOR_TEST=true \
    UPSTREAM_SHADOW_SKIP_BOUNDARY_TEST=true \
    /bin/bash "$shadow_map_test"

grep -Fqx $'backend/internal/repository/content_moderation_repo.go\tbackend/internal/custom/moderation/violation_counter.go' "$shadow_map"
grep -Fqx $'backend/internal/handler/openai_gateway_cyber_test.go\tbackend/internal/handler/openai_gateway_custom_test.go' "$shadow_map"
grep -Fqx $'backend/internal/repository/content_moderation_repo_test.go\tbackend/internal/custom/moderation/violation_counter_test.go' "$shadow_map"
grep -Fqx $'backend/internal/service/content_moderation.go\tbackend/internal/custom/moderation/api_audit_ban.go|backend/internal/custom/moderation/api_audit_scope.go|backend/internal/custom/moderation/cyber_policy.go|backend/internal/custom/moderation/excerpt.go|backend/internal/custom/moderation/group_scope_reconcile.go|backend/internal/custom/moderation/user_ban_threshold.go' "$shadow_map"
grep -Fqx $'backend/internal/service/content_moderation_cyber_test.go\tbackend/internal/custom/moderation/cyber_policy_test.go|backend/internal/service/custom_moderation_bridge_test.go' "$shadow_map"
grep -Fqx $'backend/internal/service/content_moderation_email.go\tbackend/internal/custom/moderation/cyber_policy.go' "$shadow_map"
grep -Fqx $'backend/internal/service/content_moderation_test.go\tbackend/internal/custom/moderation/api_audit_ban_test.go|backend/internal/custom/moderation/api_audit_scope_test.go|backend/internal/custom/moderation/excerpt_test.go|backend/internal/custom/moderation/group_scope_reconcile_test.go|backend/internal/custom/moderation/user_ban_threshold_test.go|backend/internal/service/custom_moderation_bridge_test.go' "$shadow_map"
grep -Fqx $'backend/internal/service/notification_email_service.go\tbackend/internal/custom/moderation/cyber_policy.go' "$shadow_map"
grep -Fqx $'backend/internal/service/notification_email_service_test.go\tbackend/internal/custom/moderation/cyber_policy_test.go' "$shadow_map"
grep -Fqx $'frontend/src/api/admin/riskControl.ts\tfrontend/src/custom/moderation/api.ts|frontend/src/custom/moderation/userBanThresholds.ts' "$shadow_map"
grep -Fqx $'frontend/src/views/admin/RiskControlView.vue\tfrontend/src/custom/moderation/UserBanThresholdOverrides.vue|frontend/src/custom/moderation/userBanThresholds.ts|frontend/src/custom/moderation/views/RiskControlView.vue' "$shadow_map"
grep -Fqx $'frontend/src/views/admin/__tests__/RiskControlView.spec.ts\tfrontend/src/custom/moderation/UserBanThresholdOverrides.spec.ts|frontend/src/custom/moderation/userBanThresholds.spec.ts|frontend/src/custom/moderation/views/__tests__/RiskControlView.spec.ts' "$shadow_map"
grep -Fqx $'frontend/src/utils/releaseNotes.ts|@removed\tfrontend/src/custom/updater/releaseNotes.ts' "$shadow_map"
grep -Fqx $'frontend/src/utils/__tests__/releaseNotes.spec.ts|@removed\tfrontend/src/custom/updater/__tests__/releaseNotes.spec.ts' "$shadow_map"
grep -Fqx $'frontend/src/components/common/__tests__/VersionBadge.rollback.spec.ts|@removed\tfrontend/src/custom/updater/components/__tests__/VersionBadge.rollback.spec.ts' "$shadow_map"

fail_if_present \
  "the manual approval tag must not be interpolated into shell source" \
  'if [[ "${{ inputs.approved_official_tag }}"' \
  "$gate_workflow"
grep -Fq \
  'INPUT_APPROVED_OFFICIAL_TAG: ${{ inputs.approved_official_tag }}' \
  "$gate_workflow"
grep -Fq \
  'INPUT_APPROVED_DATABASE_BASE_COMMIT: ${{ inputs.approved_database_base_commit }}' \
  "$gate_workflow"
grep -Fq \
  'INPUT_APPROVED_DATABASE_FINGERPRINT: ${{ inputs.approved_database_fingerprint }}' \
  "$gate_workflow"
grep -Fq \
  'if [[ ! "$INPUT_APPROVED_OFFICIAL_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then' \
  "$gate_workflow"
grep -Fq \
  "format('Official upgrade PR #{0} @ {1}', inputs.pr_number, inputs.expected_head_sha)" \
  "$gate_workflow"
grep -Fq 'INPUT_EXPECTED_HEAD_SHA: ${{ inputs.expected_head_sha }}' "$gate_workflow"
grep -Fq 'expected_head_sha must be a full 40-character commit SHA.' "$gate_workflow"
grep -Fq '"$head_sha" != "$INPUT_EXPECTED_HEAD_SHA"' "$gate_workflow"

if [[ ! "v0.1.162" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "ERROR: a valid manual approval tag was rejected" >&2
  exit 1
fi
malicious_approval_tags=(
  'v0.1.162"; echo injected; #'
  $'v0.1.162\necho injected'
  'v0.1.162$(echo injected)'
)
for approval_tag in "${malicious_approval_tags[@]}"; do
  if [[ "$approval_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "ERROR: unsafe manual approval tag was accepted" >&2
    exit 1
  fi
done

sed -n '/^  required_validation:/,/^  publish_validation_status:/p' \
  "$gate_workflow" > "$tmp_dir/required-validation.yml"
grep -Fq 'name: Internal upgrade validation aggregate' "$tmp_dir/required-validation.yml"
grep -Fq 'if: ${{ always() }}' "$tmp_dir/required-validation.yml"
grep -Fq 'CONTEXT_RESULT: ${{ needs.context.result }}' "$tmp_dir/required-validation.yml"
grep -Fq 'IS_UPGRADE: ${{ needs.context.outputs.is_upgrade }}' "$tmp_dir/required-validation.yml"
grep -Fq 'case "$IS_UPGRADE" in' "$tmp_dir/required-validation.yml"
grep -Fq 'if [[ "$result" != "success" ]]; then' "$tmp_dir/required-validation.yml"
for result_name in \
  GATE_RESULT \
  BACKEND_RESULT \
  FRONTEND_RESULT \
  RELEASE_PREFLIGHT_RESULT; do
  grep -Fq "${result_name}:" "$tmp_dir/required-validation.yml"
done

awk '
  /^        run: \|$/ { capture=1; next }
  capture && /^  [A-Za-z0-9_-]+:/ { exit }
  capture {
    if ($0 ~ /^          /) {
      sub(/^          /, "")
      print
    } else if ($0 ~ /^[[:space:]]*$/) {
      print
    } else {
      exit
    }
  }
' "$tmp_dir/required-validation.yml" > "$tmp_dir/required-validation.sh"

if ! env \
  CONTEXT_RESULT=success \
  IS_UPGRADE=false \
  GATE_RESULT=skipped \
  BACKEND_RESULT=skipped \
  FRONTEND_RESULT=skipped \
  RELEASE_PREFLIGHT_RESULT=skipped \
  RELEASE_INPUTS_CHANGED=false \
  /bin/bash "$tmp_dir/required-validation.sh" >/dev/null; then
  echo "ERROR: required validation rejected an ordinary pull request" >&2
  exit 1
fi

successful_upgrade_results=(
  GATE_RESULT=success
  BACKEND_RESULT=success
  FRONTEND_RESULT=success
  RELEASE_PREFLIGHT_RESULT=success
  RELEASE_INPUTS_CHANGED=true
)
if ! env \
  CONTEXT_RESULT=success \
  IS_UPGRADE=true \
  "${successful_upgrade_results[@]}" \
  /bin/bash "$tmp_dir/required-validation.sh" >/dev/null; then
  echo "ERROR: required validation rejected successful upgrade jobs" >&2
  exit 1
fi

for rejected_result in failure cancelled skipped neutral; do
  if env \
    CONTEXT_RESULT=success \
    IS_UPGRADE=true \
    "${successful_upgrade_results[@]}" \
    GATE_RESULT="$rejected_result" \
    /bin/bash "$tmp_dir/required-validation.sh" >/dev/null 2>&1; then
    echo "ERROR: required validation accepted gate result ${rejected_result}" >&2
    exit 1
  fi
done
if env \
  CONTEXT_RESULT=skipped \
  IS_UPGRADE=false \
  /bin/bash "$tmp_dir/required-validation.sh" >/dev/null 2>&1; then
  echo "ERROR: required validation accepted a skipped context classifier" >&2
  exit 1
fi

if grep -Fxq '    name: Required upgrade validation' "$gate_workflow"; then
  fail "Actions Check must not share the Required upgrade validation Commit Status context"
fi

fail_if_present \
  "the trusted gate must not dispatch workflow definitions from an upgrade branch" \
  'independent_validation:' \
  "$gate_workflow"
fail_if_present \
  "the trusted gate must not load a worker workflow from the upgrade branch" \
  '--ref "$HEAD_REF"' \
  "$gate_workflow"

sed -n '/^  finalize:/,/^  report-finalize-failure:/p' \
  "$gate_workflow" > "$tmp_dir/finalize.yml"
sed -n '/^  publish_validation_status:/,/^  report-failure:/p' \
  "$gate_workflow" > "$tmp_dir/publish-validation-status.yml"
grep -Fq "github.event_name == 'workflow_dispatch'" "$tmp_dir/publish-validation-status.yml"
grep -Fq "github.ref == 'refs/heads/main'" "$tmp_dir/publish-validation-status.yml"
grep -Fq "github.event_name == 'pull_request_target'" "$tmp_dir/publish-validation-status.yml"
grep -Fq "needs.context.outputs.is_upgrade != 'true'" "$tmp_dir/publish-validation-status.yml"
fail_if_present \
  "pull request runs must not publish the trusted upgrade validation status" \
  "github.event_name == 'pull_request'" \
  "$tmp_dir/publish-validation-status.yml"
grep -Fq 'statuses: write' "$tmp_dir/publish-validation-status.yml"
grep -Fq 'HEAD_SHA: ${{ needs.context.outputs.head_sha || inputs.expected_head_sha || github.event.pull_request.head.sha }}' "$tmp_dir/publish-validation-status.yml"
grep -Fq 'CONTEXT_RESULT: ${{ needs.context.result }}' "$tmp_dir/publish-validation-status.yml"
grep -Fq 'VALIDATION_RESULT: ${{ needs.required_validation.result }}' "$tmp_dir/publish-validation-status.yml"
grep -Fq "if [[ \"\$CONTEXT_RESULT\" == \"success\" ]]; then" "$tmp_dir/publish-validation-status.yml"
grep -Fq "state=error" "$tmp_dir/publish-validation-status.yml"
grep -Fq 'if [[ "$VALIDATION_RESULT" == "success" ]]; then' "$tmp_dir/publish-validation-status.yml"
grep -Fq -- "-f context='Required upgrade validation'" "$tmp_dir/publish-validation-status.yml"
grep -Fq 'statuses/${HEAD_SHA}' "$tmp_dir/publish-validation-status.yml"

awk '
  /^        run: \|$/ { capture=1; next }
  capture && /^  [A-Za-z0-9_-]+:/ { exit }
  capture {
    if ($0 ~ /^          /) {
      sub(/^          /, "")
      print
    } else if ($0 ~ /^[[:space:]]*$/) {
      print
    } else {
      exit
    }
  }
' "$tmp_dir/publish-validation-status.yml" > "$tmp_dir/publish-validation-status.sh"
mkdir -p "$tmp_dir/mock-bin"
cat > "$tmp_dir/mock-bin/gh" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" > "$GH_CAPTURE"
EOF
chmod +x "$tmp_dir/mock-bin/gh"

run_publish_status_case() {
  local expected_state="$1"
  shift
  rm -f "$tmp_dir/gh-capture.txt"
  if ! env \
    PATH="$tmp_dir/mock-bin:$PATH" \
    GH_CAPTURE="$tmp_dir/gh-capture.txt" \
    HEAD_SHA=0123456789abcdef0123456789abcdef01234567 \
    GITHUB_REPOSITORY=o87110/sub2api-custom-public \
    GITHUB_SERVER_URL=https://github.com \
    GITHUB_RUN_ID=1 \
    "$@" \
    /bin/bash "$tmp_dir/publish-validation-status.sh" >/dev/null; then
    fail "validation status publisher rejected ${expected_state} test case"
  fi
  grep -Fq -- "-f state=${expected_state}" "$tmp_dir/gh-capture.txt" ||
    fail "validation status publisher did not emit ${expected_state}"
}

run_publish_status_case success \
  CONTEXT_RESULT=success \
  IS_UPGRADE=false \
  VALIDATION_RESULT=success
run_publish_status_case success \
  CONTEXT_RESULT=success \
  IS_UPGRADE=true \
  VALIDATION_RESULT=success
run_publish_status_case failure \
  CONTEXT_RESULT=success \
  IS_UPGRADE=true \
  VALIDATION_RESULT=failure
run_publish_status_case error \
  CONTEXT_RESULT=failure \
  IS_UPGRADE=true \
  VALIDATION_RESULT=failure

if env \
  PATH="$tmp_dir/mock-bin:$PATH" \
  GH_CAPTURE="$tmp_dir/gh-capture.txt" \
  HEAD_SHA=invalid \
  CONTEXT_RESULT=success \
  IS_UPGRADE=false \
  VALIDATION_RESULT=success \
  GITHUB_REPOSITORY=o87110/sub2api-custom-public \
  GITHUB_SERVER_URL=https://github.com \
  GITHUB_RUN_ID=1 \
  /bin/bash "$tmp_dir/publish-validation-status.sh" >/dev/null 2>&1; then
  fail "validation status publisher accepted an invalid Head SHA"
fi

grep -Fq "needs.publish_validation_status.result == 'success'" "$tmp_dir/finalize.yml"
grep -Fq "github.event_name == 'workflow_dispatch'" "$tmp_dir/finalize.yml"
grep -Fq "github.ref == 'refs/heads/main'" "$tmp_dir/finalize.yml"
grep -Fq "needs.context.outputs.is_upgrade == 'true'" "$tmp_dir/finalize.yml"
grep -Fq "needs.required_validation.result == 'success'" "$tmp_dir/finalize.yml"
grep -Fq -- '- context' "$tmp_dir/finalize.yml"
grep -Fq -- '- publish_validation_status' "$tmp_dir/finalize.yml"
grep -Fq -- '- required_validation' "$tmp_dir/finalize.yml"
grep -Fq 'GH_TOKEN: ${{ secrets.UPGRADE_FINALIZER_TOKEN || github.token }}' "$tmp_dir/finalize.yml"
grep -Fq "HAS_FINALIZER_TOKEN: \${{ secrets.UPGRADE_FINALIZER_TOKEN != '' }}" "$tmp_dir/finalize.yml"
test "$(grep -Fc 'secrets.UPGRADE_FINALIZER_TOKEN' "$gate_workflow")" -eq 2
grep -Fq \
  'Official Workflow files changed; configure UPGRADE_FINALIZER_TOKEN with workflow scope before merging.' \
  "$tmp_dir/finalize.yml"

grep -Fq 'statuses: write' "$sync_workflow"
grep -Fq 'dispatch_missing_upgrade_checks()' "$sync_workflow"
grep -Fq 'latest_upgrade_validation_status()' "$sync_workflow"
grep -Fq 'latest_upgrade_gate_run()' "$sync_workflow"
grep -Fq 'status_is_stale()' "$sync_workflow"
grep -Fq 'sort_by(.updated_at)' "$sync_workflow"
grep -Fq 'select(.display_title == $run_name)' "$sync_workflow"
grep -Fq 'success|failure)' "$sync_workflow"
grep -Fq 'status_is_stale "$validation_updated_at" 7200' "$sync_workflow"
grep -Fq 'status_is_stale "$validation_updated_at" 900' "$sync_workflow"
grep -Fq 'queued|in_progress|pending|requested|waiting)' "$sync_workflow"
fail_if_present \
  "any historical validation status must not permanently disable exact-head recovery" \
  'if [[ "$validation_status_count" -gt 0' \
  "$sync_workflow"
fail_if_present \
  "trusted dispatch runs cannot be associated through their main run Head SHA" \
  'workflow_run_count_for_sha upstream-upgrade-gate.yml "$pr_head_sha"' \
  "$sync_workflow"
grep -Fq 'git diff --quiet origin/main "$pr_head_sha" -- .github/workflows' "$sync_workflow"
fail_if_present \
  "the synchronizer must not dispatch worker definitions from an upgrade branch" \
  '--ref "$pr_head_ref"' \
  "$sync_workflow"
grep -Fq -- '--ref main' "$sync_workflow"
grep -Fq -- '-f pr_number="$pr_number"' "$sync_workflow"
grep -Fq -- '-f expected_head_sha="$pr_head_sha"' "$sync_workflow"
test "$(grep -Fc 'dispatch_missing_upgrade_checks "$pr_url" "$branch"' "$sync_workflow")" -eq 2
grep -Fq -- "-f context='Required upgrade validation'" "$sync_workflow"
grep -Fq 'Trusted official upgrade validation could not be dispatched.' "$sync_workflow"

grep -Fq \
  'remote_branch="refs/remotes/origin/${branch}"' \
  "$sync_workflow"
grep -Fq \
  'git diff --quiet origin/main "$remote_branch" -- .github/workflows' \
  "$sync_workflow"
grep -Fq \
  'echo "state=in_progress" >> "$GITHUB_OUTPUT"' \
  "$sync_workflow"
grep -Fq \
  'git push --set-upstream origin "$branch"' \
  "$sync_workflow"
grep -Fq \
  'git diff --quiet origin/main HEAD -- .github/workflows' \
  "$sync_workflow"
grep -Fq -- \
  '--source=origin/main' \
  "$sync_workflow"
grep -Fq -- \
  '-- .github/workflows' \
  "$sync_workflow"
grep -Fq \
  'later scheduled runs will not reset or force-push this branch' \
  "$sync_workflow"
grep -Fq \
  'shadow_map="/tmp/upstream-shadowed-sources.tsv"' \
  "$sync_workflow"
grep -Fq \
  'git show origin/main:.github/upstream-shadowed-sources.tsv > "$shadow_map"' \
  "$sync_workflow"
grep -Fq -- \
  'git diff --name-only --no-renames' \
  "$sync_workflow"
grep -Fq \
  '/tmp/shadowed-source-changes.txt' \
  "$sync_workflow"
grep -Fq \
  'UPSTREAM_SHADOW_NEXT_REF="$official_ref"' \
  "$sync_workflow"
grep -Fq \
  'UPSTREAM_SHADOW_OFFICIAL_FILES=/tmp/official-files.txt' \
  "$sync_workflow"
grep -Fq \
  'UPSTREAM_SHADOW_DETECTED_OUTPUT=/tmp/shadowed-source-changes.txt' \
  "$sync_workflow"
grep -Fq \
  'cat /tmp/protected-overlap.txt /tmp/shadowed-source-changes.txt' \
  "$sync_workflow"
grep -Fq \
  'same-prefix companion family' \
  "$sync_workflow"
grep -Fq \
  "'+refs/tags/v*:refs/upstream-tags/v*'" \
  "$sync_workflow"
grep -Fq \
  "'+refs/tags/*:refs/origin-tags/*'" \
  "$sync_workflow"
grep -Fq \
  'official_ref="refs/upstream-tags/${official_tag}"' \
  "$sync_workflow"
grep -Fq -- '--no-tags' "$sync_workflow"
fail_if_present \
  "official and private tags must not share refs/tags in synchronization" \
  'git fetch upstream --tags' \
  "$sync_workflow" \
  "$gate_workflow"

grep -Fq \
  'pull_request_target:' \
  "$gate_workflow"
fail_if_present \
  "the writable upgrade workflow must not use an untrusted pull_request definition" \
  '  pull_request:' \
  "$gate_workflow"
grep -Fq \
  'Trusted upgrade dispatch must run from refs/heads/main.' \
  "$gate_workflow"
grep -Fq \
  'resume_finalization:' \
  "$gate_workflow"
grep -Fq \
  'INPUT_RESUME_FINALIZATION: ${{ inputs.resume_finalization || false }}' \
  "$gate_workflow"
grep -Fq \
  'is_upgrade: ${{ steps.resolve.outputs.is_upgrade }}' \
  "$gate_workflow"
grep -Fq \
  'base_sha: ${{ steps.resolve.outputs.base_sha }}' \
  "$gate_workflow"
grep -Fq \
  'recovery_mode: ${{ steps.resolve.outputs.recovery_mode }}' \
  "$gate_workflow"
grep -Fq \
  'Finalization recovery requires an already-merged upgrade PR with an exact merge commit.' \
  "$gate_workflow"
grep -Fq \
  'Reject official upgrades on non-standard branches' \
  "$gate_workflow"
grep -Fq \
  "'+refs/tags/v*:refs/upstream-tags/v*'" \
  "$gate_workflow"
grep -Fq \
  "'+refs/tags/vendor-*:refs/origin-tags/vendor-*'" \
  "$gate_workflow"
grep -Fq \
  'origin/main:deploy/upstream/validate-upgrade-report-maintenance.sh' \
  "$gate_workflow"
grep -Fq -- \
  '--vendor-ref-prefix refs/origin-tags/' \
  "$gate_workflow"
grep -Fq \
  "'.github/upgrades/*.md'" \
  "$report_maintenance_policy"
grep -Fq \
  "if: needs.context.outputs.is_upgrade == 'true'" \
  "$gate_workflow"
test "$(grep -Fc "'+refs/tags/v*:refs/upstream-tags/v*'" "$gate_workflow")" -ge 3
test "$(grep -Fc "'+refs/tags/*:refs/origin-tags/*'" "$gate_workflow")" -ge 2
test "$(grep -Fc 'official_ref="refs/upstream-tags/${OFFICIAL_TAG}"' "$gate_workflow")" -eq 2
test "$(grep -Fc -- '--no-tags' "$gate_workflow")" -ge 7
grep -Fq \
  'git merge-base --is-ancestor "${official_ref}^{commit}" "$HEAD_SHA"' \
  "$gate_workflow"
grep -Fq \
  'git merge-base --is-ancestor "${base_ref}^{commit}" "${official_ref}^{commit}"' \
  "$gate_workflow"
grep -Fq \
  'git merge-base --is-ancestor "$trusted_main_ref" "$HEAD_SHA"' \
  "$gate_workflow"
grep -Fq \
  'upgrade:database-approved' \
  "$gate_workflow"
grep -Fq \
  'upgrade:protected-reviewed' \
  "$gate_workflow"
grep -Fq \
  'head=${approval_head}' \
  "$gate_workflow"
grep -Fq \
  'select(.user.login == "github-actions[bot]")' \
  "$gate_workflow"
grep -Fq \
  'database_marker="<!-- sub2api-upgrade-approval database tag=${OFFICIAL_TAG} head=${HEAD_SHA} base=${CUSTOM_UPSTREAM_BASE_COMMIT} fingerprint=${database_fingerprint} -->"' \
  "$gate_workflow"
grep -Fq \
  'protected_marker="<!-- sub2api-upgrade-approval protected tag=${OFFICIAL_TAG} head=${HEAD_SHA} -->"' \
  "$gate_workflow"
grep -Fq -- \
  '--match-head-commit "$HEAD_SHA"' \
  "$gate_workflow"
fail_if_present \
  "GitHub App finalization must not push official workflow objects through Git" \
  'git push --atomic origin' \
  "$gate_workflow"
grep -Fq \
  '"repos/${GITHUB_REPOSITORY}/git/refs/heads/upstream%2Fmain"' \
  "$gate_workflow"
grep -Fq \
  '"repos/${GITHUB_REPOSITORY}/git/tags"' \
  "$gate_workflow"
grep -Fq \
  '"repos/${GITHUB_REPOSITORY}/git/refs"' \
  "$gate_workflow"
grep -Fq \
  'release_workflow_api="repos/${GITHUB_REPOSITORY}/actions/workflows/release.yml"' \
  "$tmp_dir/finalize.yml"
grep -Fq \
  'trap restore_vendor_release_workflow EXIT' \
  "$tmp_dir/finalize.yml"
grep -Fq \
  'gh api --method PUT "${release_workflow_api}/disable"' \
  "$tmp_dir/finalize.yml"
grep -Fq \
  'gh api --method PUT "${release_workflow_api}/enable"' \
  "$tmp_dir/finalize.yml"
grep -Fq \
  'release_workflow_state" != "disabled_inactivity"' \
  "$tmp_dir/finalize.yml"
disable_release_line="$(grep -nF 'gh api --method PUT "${release_workflow_api}/disable"' "$tmp_dir/finalize.yml" | cut -d: -f1)"
create_vendor_ref_line="$(grep -nF '"repos/${GITHUB_REPOSITORY}/git/refs"' "$tmp_dir/finalize.yml" | cut -d: -f1)"
restore_release_line="$(grep -nF 'restore_vendor_release_workflow' "$tmp_dir/finalize.yml" | tail -n 1 | cut -d: -f1)"
if [[ -z "$disable_release_line" || -z "$create_vendor_ref_line" || -z "$restore_release_line" ||
      "$disable_release_line" -ge "$create_vendor_ref_line" ||
      "$create_vendor_ref_line" -ge "$restore_release_line" ]]; then
  fail "Vendor Tag publication is not enclosed by Release Workflow disable/restore protection"
fi
awk '
  /release_workflow_api="repos\/\$\{GITHUB_REPOSITORY\}\/actions\/workflows\/release.yml"/ { capture=1 }
  capture {
    sub(/^            /, "")
    print
  }
  capture && /trap - EXIT/ { exit }
' "$tmp_dir/finalize.yml" > "$tmp_dir/vendor-tag-publication.sh"
cat > "$tmp_dir/mock-bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$GH_CAPTURE"
if [[ "$*" == *'--jq .state'* ]]; then
  printf '%s\n' "${GH_WORKFLOW_STATE:-active}"
elif [[ "$*" == *'/git/tags'* ]]; then
  if [[ "${GH_FAIL_VENDOR_TAG:-false}" == "true" ]]; then
    exit 1
  fi
  printf '%s\n' 0123456789abcdef0123456789abcdef01234567
fi
EOF
chmod +x "$tmp_dir/mock-bin/gh"

run_vendor_tag_publication_case() {
  local failure_mode="$1"
  local capture="$tmp_dir/vendor-tag-${failure_mode}.txt"
  rm -f "$capture"
  if [[ "$failure_mode" == "success" ]]; then
    env \
      PATH="$tmp_dir/mock-bin:$PATH" \
      GH_CAPTURE="$capture" \
      GITHUB_REPOSITORY=o87110/sub2api-custom-public \
      VENDOR_TAG=vendor-9.9.9 \
      official_commit=0123456789abcdef0123456789abcdef01234567 \
      /bin/bash -euo pipefail "$tmp_dir/vendor-tag-publication.sh"
  elif [[ "$failure_mode" == "disabled" ]]; then
    env \
      PATH="$tmp_dir/mock-bin:$PATH" \
      GH_CAPTURE="$capture" \
      GH_WORKFLOW_STATE=disabled_manually \
      GITHUB_REPOSITORY=o87110/sub2api-custom-public \
      VENDOR_TAG=vendor-9.9.9 \
      official_commit=0123456789abcdef0123456789abcdef01234567 \
      /bin/bash -euo pipefail "$tmp_dir/vendor-tag-publication.sh"
  elif env \
      PATH="$tmp_dir/mock-bin:$PATH" \
      GH_CAPTURE="$capture" \
      GH_FAIL_VENDOR_TAG=true \
      GITHUB_REPOSITORY=o87110/sub2api-custom-public \
      VENDOR_TAG=vendor-9.9.9 \
      official_commit=0123456789abcdef0123456789abcdef01234567 \
      /bin/bash -euo pipefail "$tmp_dir/vendor-tag-publication.sh" >/dev/null 2>&1; then
    fail "Vendor Tag publication fixture did not exercise its failure path"
  fi
  if [[ "$failure_mode" == "disabled" ]]; then
    test "$(grep -Fc '/actions/workflows/release.yml/disable' "$capture")" -eq 0
    test "$(grep -Fc '/actions/workflows/release.yml/enable' "$capture")" -eq 0
  else
    test "$(grep -Fc '/actions/workflows/release.yml/disable' "$capture")" -eq 1
    test "$(grep -Fc '/actions/workflows/release.yml/enable' "$capture")" -eq 1
  fi
}

run_vendor_tag_publication_case success
run_vendor_tag_publication_case failure
run_vendor_tag_publication_case disabled
grep -Fq \
  '"$(git cat-file -t "$vendor_ref")" != "tag"' \
  "$gate_workflow"
grep -Fq \
  'git merge-base --is-ancestor origin/upstream/main "$official_commit"' \
  "$gate_workflow"
grep -Fq \
  'git merge-base --is-ancestor origin/main "origin/${HEAD_REF}"' \
  "$gate_workflow"
grep -Fq \
  'git merge-base --is-ancestor "$HEAD_SHA" "$MERGE_COMMIT_SHA"' \
  "$gate_workflow"
grep -Fq \
  'git merge-base --is-ancestor "$MERGE_COMMIT_SHA" origin/main' \
  "$gate_workflow"
grep -Fq \
  'upgrade blocked: finalize' \
  "$gate_workflow"
grep -Fq \
  'shadow_map="/tmp/upstream-shadowed-sources.tsv"' \
  "$gate_workflow"
grep -Fq \
  'git show "${trusted_main_ref}:.github/upstream-shadowed-sources.tsv" > "$shadow_map"' \
  "$gate_workflow"
grep -Fq -- \
  'git diff --name-only --no-renames "$trusted_main_ref" "$HEAD_SHA"' \
  "$gate_workflow"
grep -Fq \
  'git merge-tree --write-tree "$trusted_main_ref" "${official_ref}^{commit}"' \
  "$gate_workflow"
grep -Fq \
  '/tmp/unexpected-upgrade-tree-files.txt' \
  "$gate_workflow"
grep -Fq \
  'Matches deterministic official merge tree' \
  "$gate_workflow"
grep -Fq \
  'Upgrade PRs must not modify trusted upgrade control-plane files.' \
  "$gate_workflow"
grep -Fq \
  "grep -Ev '^\\.github/(custom-database-exceptions\\.tsv|custom-upstream-(baseline\\.env|delta\\.tsv))$'" \
  "$gate_workflow"
grep -Fq \
  'expected_candidate_baseline_ref="${VENDOR_TAG}^{commit}"' \
  "$gate_workflow"
grep -Fq \
  'git update-ref "refs/tags/${VENDOR_TAG}" "$official_commit"' \
  "$gate_workflow"
grep -Fq \
  '/bin/bash deploy/tests/custom-upstream-delta-test.sh \' \
  "$gate_workflow"
grep -Fq \
  '/bin/bash deploy/tests/custom-database-boundary-test.sh \' \
  "$gate_workflow"
grep -Fq -- \
  '--mode final \' \
  "$gate_workflow"
grep -Fq \
  'CUSTOM_UPSTREAM_BASELINE_FILE="$trusted_baseline_file" \' \
  "$gate_workflow"
test "$(
  grep -Fc 'CUSTOM_UPSTREAM_BASELINE_FILE="$trusted_baseline_file" \' \
    "$gate_workflow"
)" -eq 2
for baseline_test in "$delta_test" "$database_test"; do
  grep -Fq \
    'baseline_file="${CUSTOM_UPSTREAM_BASELINE_FILE:-$repo_root/.github/custom-upstream-baseline.env}"' \
    "$baseline_test"
  grep -Fq \
    '[[ "${#baseline_lines[@]}" -eq 2 ]]' \
    "$baseline_test"
  grep -Fq \
    'CUSTOM_UPSTREAM_BASE_REF="${baseline_ref_line#CUSTOM_UPSTREAM_BASE_REF=}"' \
    "$baseline_test"
done
grep -Fq \
  'while IFS= read -r baseline_line || [[ -n "$baseline_line" ]]; do' \
  "$delta_test"
if grep -nE '(^|[[:space:]])(mapfile|readarray|declare[[:space:]]+-A)([[:space:]]|$)' \
  "$delta_test"; then
  fail "custom upstream delta test must remain compatible with macOS Bash 3.2"
fi
baseline_ref="$(
  sed -n 's/^CUSTOM_UPSTREAM_BASE_REF=//p' \
    "$repo_root/.github/custom-upstream-baseline.env" | tr -d '\r'
)"
baseline_commit="$(
  sed -n 's/^CUSTOM_UPSTREAM_BASE_COMMIT=//p' \
    "$repo_root/.github/custom-upstream-baseline.env" | tr -d '\r'
)"
[[ "$baseline_ref" =~ ^(vendor-[0-9]+\.[0-9]+\.[0-9]+)\^\{commit\}$ ]] ||
  fail "candidate baseline ref is invalid"
baseline_tag="${BASH_REMATCH[1]}"
[[ "$baseline_commit" =~ ^[0-9a-f]{40}$ ]] ||
  fail "candidate baseline commit is invalid"
if ! git -C "$repo_root" rev-parse --verify "$baseline_ref" >/dev/null 2>&1; then
  git -C "$repo_root" rev-parse --verify "${baseline_commit}^{commit}" >/dev/null
  temporary_baseline_ref="refs/tags/$baseline_tag"
  temporary_baseline_commit="$baseline_commit"
  git -C "$repo_root" update-ref \
    "$temporary_baseline_ref" "$temporary_baseline_commit" ""
fi
candidate_tree="$(/bin/bash "$candidate_tree_script" --worktree)"
awk '{ sub(/\r$/, ""); printf "%s\r\n", $0 }' \
  "$repo_root/.github/custom-upstream-delta.tsv" \
  > "$tmp_dir/custom-upstream-delta-crlf.tsv"
CUSTOM_UPSTREAM_DELTA_LEDGER="$tmp_dir/custom-upstream-delta-crlf.tsv" \
  /bin/bash "$delta_test" --candidate-tree "$candidate_tree" >/dev/null
if [[ -n "$temporary_baseline_ref" ]]; then
  git -C "$repo_root" update-ref \
    -d "$temporary_baseline_ref" "$temporary_baseline_commit"
  temporary_baseline_ref=""
  temporary_baseline_commit=""
fi
grep -Fq \
  '\.github/workflows/' \
  "$gate_workflow"
grep -Fq \
  'deploy/tests/' \
  "$gate_workflow"
grep -Fq \
  '/tmp/shadowed-source-changes.txt' \
  "$gate_workflow"
grep -Fq \
  'UPSTREAM_SHADOW_NEXT_REF="$official_ref"' \
  "$gate_workflow"
grep -Fq \
  'UPSTREAM_SHADOW_OFFICIAL_FILES=/tmp/official-files.txt' \
  "$gate_workflow"
grep -Fq \
  'UPSTREAM_SHADOW_DETECTED_OUTPUT=/tmp/shadowed-source-changes.txt' \
  "$gate_workflow"
grep -Fq \
  '/tmp/upgrade-protected-files.txt \' \
  "$gate_workflow"
grep -Fq \
  'make -C backend test-unit' \
  "$gate_workflow"
grep -Fq \
  '/bin/bash deploy/tests/backend-integration-test.sh' \
  "$gate_workflow"
grep -Fq \
  'go generate ./cmd/server' \
  "$gate_workflow"
grep -Fq \
  'make -C .. test-frontend test-frontend-custom' \
  "$gate_workflow"
grep -Fq \
  'run: /bin/bash deploy/tests/install-custom-tools.sh golangci-lint' \
  "$gate_workflow"
grep -Fq \
  'CI, Security Scan, and Publish Custom Build were dispatched from trusted main for the exact merged SHA; Publish waits for exact-SHA CI and boundaries while Security Scan reports independently.' \
  "$gate_workflow"
grep -Fq \
  'for workflow in backend-ci.yml security-scan.yml; do' \
  "$gate_workflow"
grep -Fq \
  'gh workflow run publish-custom.yml \' \
  "$gate_workflow"
grep -Fq -- \
  '-f expected_sha="$merged_sha"' \
  "$gate_workflow"
grep -Fq -- \
  '--ref main' \
  "$gate_workflow"
grep -Fq -- \
  '-f expected_sha="$merged_sha"' \
  "$gate_workflow"

echo "upstream synchronization safety checks passed"
