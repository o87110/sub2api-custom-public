#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
baseline_file="${CUSTOM_UPSTREAM_BASELINE_FILE:-$repo_root/.github/custom-upstream-baseline.env}"
ledger="${CUSTOM_UPSTREAM_DELTA_LEDGER:-$repo_root/.github/custom-upstream-delta.tsv}"
shadow_map="$repo_root/.github/upstream-shadowed-sources.tsv"
thin_bridge_contract="$repo_root/.github/custom-thin-bridge-contract.tsv"
thin_bridge_validator="$repo_root/tools/validate_custom_thin_bridges.py"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  echo "usage: $0 --candidate-tree <tree>" >&2
  exit 2
}

candidate_tree=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --candidate-tree)
      candidate_tree="${2:-}"
      shift 2
      ;;
    *)
      usage
      ;;
  esac
done

[[ "$candidate_tree" =~ ^[0-9a-f]{40,64}$ ]] || usage
git -C "$repo_root" cat-file -e "${candidate_tree}^{tree}" ||
  fail "candidate tree does not exist: $candidate_tree"
[[ -s "$baseline_file" ]] || fail "explicit upstream baseline file is missing"
[[ -s "$ledger" ]] || fail "custom upstream delta ledger is missing"
[[ -s "$shadow_map" ]] || fail "upstream shadow map is missing"
[[ -s "$thin_bridge_contract" ]] || fail "custom thin bridge contract is missing"
[[ -s "$thin_bridge_validator" ]] || fail "custom thin bridge validator is missing"

baseline_lines=()
baseline_line=""
while IFS= read -r baseline_line || [[ -n "$baseline_line" ]]; do
  baseline_lines[${#baseline_lines[@]}]="$baseline_line"
done < "$baseline_file"
[[ "${#baseline_lines[@]}" -eq 2 ]] ||
  fail "baseline file must contain exactly two assignments"
baseline_ref_line="${baseline_lines[0]%$'\r'}"
baseline_commit_line="${baseline_lines[1]%$'\r'}"
[[ "$baseline_ref_line" == CUSTOM_UPSTREAM_BASE_REF=* ]] ||
  fail "baseline ref assignment is missing"
[[ "$baseline_commit_line" == CUSTOM_UPSTREAM_BASE_COMMIT=* ]] ||
  fail "baseline commit assignment is missing"
CUSTOM_UPSTREAM_BASE_REF="${baseline_ref_line#CUSTOM_UPSTREAM_BASE_REF=}"
CUSTOM_UPSTREAM_BASE_COMMIT="${baseline_commit_line#CUSTOM_UPSTREAM_BASE_COMMIT=}"
[[ "${CUSTOM_UPSTREAM_BASE_REF:-}" =~ ^vendor-[0-9]+\.[0-9]+\.[0-9]+\^\{commit\}$ ]] ||
  fail "baseline ref must be an explicit vendor-X.Y.Z commit"
[[ "${CUSTOM_UPSTREAM_BASE_COMMIT:-}" =~ ^[0-9a-f]{40}$ ]] ||
  fail "baseline commit is invalid"
resolved_base="$(git -C "$repo_root" rev-parse --verify "$CUSTOM_UPSTREAM_BASE_REF")"
[[ "$resolved_base" == "$CUSTOM_UPSTREAM_BASE_COMMIT" ]] ||
  fail "baseline ref resolves to $resolved_base, expected $CUSTOM_UPSTREAM_BASE_COMMIT"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
rows="$tmp_dir/rows.tsv"
actual="$tmp_dir/actual.tsv"
seen_actual="$tmp_dir/seen-actual.txt"
: > "$seen_actual"

expected_header=$'path\tinitial_status\tdecision\texpected_status\tcategory\tbase_blob\tfinal_blob\tshadow_source\tshadow_target\tverification\treason'
IFS= read -r actual_header < "$ledger"
actual_header="${actual_header%$'\r'}"
[[ "$actual_header" == "$expected_header" ]] || fail "delta ledger header is invalid"

awk -F '\t' '
  { sub(/\r$/, "") }
  NR == 1 { next }
  /^[[:space:]]*$/ { next }
  NF != 11 {
    printf "invalid delta ledger line %d: expected 11 TSV fields\n", NR > "/dev/stderr"
    failed = 1
    next
  }
  {
    for (i = 1; i <= 11; i++) {
      if ($i == "") {
        printf "invalid delta ledger line %d: field %d is empty\n", NR, i > "/dev/stderr"
        failed = 1
      }
    }
    print
  }
  END { exit failed }
' "$ledger" > "$rows"

cut -f1 "$rows" > "$tmp_dir/paths.txt"
LC_ALL=C sort "$tmp_dir/paths.txt" > "$tmp_dir/sorted-paths.txt"
cmp -s "$tmp_dir/paths.txt" "$tmp_dir/sorted-paths.txt" ||
  fail "delta ledger paths must be sorted"
duplicate_paths="$(uniq -d "$tmp_dir/sorted-paths.txt")"
[[ -z "$duplicate_paths" ]] || fail "duplicate delta ledger paths: $duplicate_paths"

git -C "$repo_root" diff \
  --name-status \
  --no-renames \
  "$CUSTOM_UPSTREAM_BASE_COMMIT" \
  "$candidate_tree" > "$actual"
if grep -Ev $'^[AMD]\t' "$actual" | grep -q .; then
  fail "candidate diff contains a status other than A/M/D"
fi

is_sha() {
  [[ "$1" =~ ^[0-9a-f]{40,64}$ ]]
}

validate_path() {
  case "$1" in
    "" | . | .. | /* | [A-Za-z]:* | *\\* | ./* | */./* | ../* | */../* | */.. | *//*)
      fail "ledger path is not normalized repository-relative: $1"
      ;;
  esac
}

actual_status() {
  awk -F '\t' -v path="$1" '$2 == path { print $1 }' "$actual"
}

base_blob_paths=()
base_blob_values=()
candidate_blob_paths=()
candidate_blob_values=()
while IFS= read -r -d '' record; do
  metadata="${record%%$'\t'*}"
  path="${record#*$'\t'}"
  blob_index="${#base_blob_paths[@]}"
  base_blob_paths[$blob_index]="$path"
  base_blob_values[$blob_index]="${metadata##* }"
done < <(git -C "$repo_root" ls-tree -rz "$CUSTOM_UPSTREAM_BASE_COMMIT")
while IFS= read -r -d '' record; do
  metadata="${record%%$'\t'*}"
  path="${record#*$'\t'}"
  blob_index="${#candidate_blob_paths[@]}"
  candidate_blob_paths[$blob_index]="$path"
  candidate_blob_values[$blob_index]="${metadata##* }"
done < <(git -C "$repo_root" ls-tree -rz "$candidate_tree")

blob_at() {
  local object="$1"
  local requested_path="$2"
  local blob_index

  case "$object" in
    "$CUSTOM_UPSTREAM_BASE_COMMIT")
      for ((blob_index = 0; blob_index < ${#base_blob_paths[@]}; blob_index++)); do
        if [[ "${base_blob_paths[$blob_index]}" == "$requested_path" ]]; then
          printf '%s' "${base_blob_values[$blob_index]}"
          return
        fi
      done
      ;;
    "$candidate_tree")
      for ((blob_index = 0; blob_index < ${#candidate_blob_paths[@]}; blob_index++)); do
        if [[ "${candidate_blob_paths[$blob_index]}" == "$requested_path" ]]; then
          printf '%s' "${candidate_blob_values[$blob_index]}"
          return
        fi
      done
      ;;
    *)
      fail "unexpected Blob lookup object: $object"
      ;;
  esac
}

thin_bridge_allowed() {
  case "$1" in
    backend/cmd/server/wire.go | \
      backend/internal/handler/admin/content_moderation_handler.go | \
      backend/internal/handler/admin/group_handler.go | \
      backend/internal/handler/admin/payment_handler.go | \
      backend/internal/handler/admin/payment_handler_test.go | \
      backend/internal/handler/admin/channel_monitor_handler.go | \
      backend/internal/handler/admin/setting_handler.go | \
      backend/internal/handler/admin/setting_handler_update.go | \
      backend/internal/handler/admin/subscription_handler.go | \
      backend/internal/handler/auth_wechat_oauth.go | \
      backend/internal/handler/auth_wechat_oauth_test.go | \
      backend/internal/handler/api_key_handler.go | \
      backend/internal/handler/channel_monitor_user_handler.go | \
      backend/internal/handler/admin/system_handler.go | \
      backend/internal/handler/admin/system_handler_test.go | \
      backend/internal/handler/dto/mappers.go | \
      backend/internal/handler/dto/settings.go | \
      backend/internal/handler/dto/types.go | \
      backend/internal/handler/gateway_handler.go | \
      backend/internal/handler/openai_gateway_handler.go | \
      backend/internal/handler/payment_handler.go | \
      backend/internal/handler/payment_handler_resume_test.go | \
      backend/internal/handler/wire.go | \
      backend/internal/payment/load_balancer.go | \
      backend/internal/payment/types.go | \
      backend/internal/repository/api_key_minimum_balance_repo.go | \
      backend/internal/repository/api_key_repo.go | \
      backend/internal/repository/channel_monitor_repo.go | \
	  backend/internal/repository/group_repo.go | \
	  backend/internal/repository/usage_billing_repo.go | \
      backend/internal/repository/user_subscription_repo.go | \
      backend/internal/repository/wire.go | \
      backend/internal/server/routes/admin.go | \
      backend/internal/service/admin_group.go | \
      backend/internal/service/admin_group_duplicate.go | \
      backend/internal/service/admin_service.go | \
      backend/internal/service/api_key_auth_cache.go | \
      backend/internal/service/api_key_auth_cache_impl.go | \
      backend/internal/service/api_key_service.go | \
      backend/internal/service/batch_image_public.go | \
      backend/internal/service/billing_cache_service.go | \
      backend/internal/service/channel_monitor_aggregator.go | \
      backend/internal/service/channel_monitor_const.go | \
      backend/internal/service/channel_monitor_service.go | \
      backend/internal/service/channel_monitor_types.go | \
      backend/internal/service/content_moderation.go | \
      backend/internal/service/content_moderation_email.go | \
      backend/internal/service/group.go | \
      backend/internal/service/idempotency.go | \
      backend/internal/service/idempotency_test.go | \
      backend/internal/service/notification_email_service.go | \
      backend/internal/service/notification_email_service_test.go | \
      backend/internal/service/payment_config_limits.go | \
      backend/internal/service/payment_config_limits_test.go | \
      backend/internal/service/payment_config_plans.go | \
      backend/internal/service/payment_config_service.go | \
      backend/internal/service/payment_config_service_test.go | \
      backend/internal/service/payment_fulfillment.go | \
      backend/internal/service/payment_fulfillment_test.go | \
      backend/internal/service/payment_order.go | \
      backend/internal/service/payment_order_lifecycle.go | \
      backend/internal/service/payment_order_lifecycle_test.go | \
      backend/internal/service/payment_order_result_test.go | \
      backend/internal/service/payment_refund.go | \
      backend/internal/service/payment_refund_test.go | \
      backend/internal/service/payment_resume_service.go | \
      backend/internal/service/payment_resume_service_test.go | \
      backend/internal/service/payment_service.go | \
      backend/internal/service/redeem_service.go | \
      backend/internal/service/subscription_service.go | \
      backend/internal/service/user_subscription.go | \
      backend/internal/service/user_subscription_port.go | \
      frontend/src/api/admin/subscriptions.ts | \
      frontend/src/api/admin/payment.ts | \
      frontend/src/api/admin/channelMonitor.ts | \
      frontend/src/api/admin/settings.ts | \
      frontend/src/api/channelMonitor.ts | \
      frontend/src/components/admin/monitor/MonitorFormDialog.vue | \
      frontend/src/components/common/SubscriptionProgressMini.vue | \
      frontend/src/components/payment/AmountInput.vue | \
      frontend/src/components/payment/PaymentProviderDialog.vue | \
      frontend/src/components/payment/SubscriptionPlanCard.vue | \
      frontend/src/components/payment/paymentFlow.ts | \
      frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts | \
      frontend/src/components/payment/__tests__/paymentFlow.spec.ts | \
      frontend/src/components/user/monitor/MonitorCard.vue | \
      frontend/src/components/layout/AppSidebar.vue | \
      frontend/src/features/channel-monitor-v2/RelayPulseMatrix.vue | \
      frontend/src/i18n/locales/en/admin/channels.ts | \
      frontend/src/i18n/locales/en/admin/overview.ts | \
      frontend/src/i18n/locales/en/admin/settings.ts | \
      frontend/src/i18n/locales/en/misc.ts | \
      frontend/src/i18n/locales/zh/admin/settings.ts | \
      frontend/src/i18n/locales/zh/admin/channels.ts | \
      frontend/src/i18n/locales/zh/admin/overview.ts | \
      frontend/src/i18n/locales/zh/misc.ts | \
      frontend/src/router/index.ts | \
      frontend/src/types/index.ts | \
      frontend/src/types/payment.ts | \
      frontend/src/views/admin/GroupsView.vue | \
      frontend/src/views/admin/SettingsView.vue | \
      frontend/src/views/admin/SubscriptionsView.vue | \
      frontend/src/views/admin/__tests__/SettingsView.spec.ts | \
      frontend/src/views/admin/orders/AdminPaymentPlansView.vue | \
      frontend/src/views/admin/orders/PlanEditDialog.vue | \
      frontend/src/views/admin/orders/__tests__/AdminPaymentPlansView.spec.ts | \
      frontend/src/views/admin/orders/__tests__/PlanEditDialog.spec.ts | \
      frontend/src/views/auth/__tests__/WechatPaymentCallbackView.spec.ts | \
      frontend/src/views/user/PaymentView.vue | \
      frontend/src/views/user/__tests__/PaymentView.spec.ts | \
      frontend/src/views/user/__tests__/paymentWechatResume.spec.ts | \
      frontend/src/views/user/paymentWechatResume.ts | \
      frontend/src/views/user/KeysView.vue | \
      frontend/src/views/user/SubscriptionsView.vue)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

while IFS=$'\t' read -r \
  path initial_status decision expected_status category base_blob final_blob \
  shadow_source shadow_target verification reason; do
  validate_path "$path"
  [[ "$initial_status" =~ ^[AMD]$ ]] ||
    fail "invalid initial status for $path: $initial_status"
  case "$category" in
    whole-file-restore | official-thin-bridge | generated | fixed-path | \
      custom-backend | custom-frontend | custom-test | custom-docs-ops | \
      explicit-exception)
      ;;
    *)
      fail "invalid category for $path: $category"
      ;;
  esac

  base_actual="$(blob_at "$CUSTOM_UPSTREAM_BASE_COMMIT" "$path")"
  [[ -n "$base_actual" ]] || base_actual="@absent"
  [[ "$base_blob" == "$base_actual" ]] ||
    fail "baseline Blob mismatch for $path"

  status="$(actual_status "$path")"
  if [[ "$decision" == "restore" ]]; then
    [[ "$expected_status" == "RESTORED" && "$category" == "whole-file-restore" ]] ||
      fail "restored path has invalid status/category: $path"
    [[ -z "$status" ]] || fail "restored path still differs from baseline: $path"
    is_sha "$base_blob" || fail "restored path must exist in the baseline: $path"
    [[ "$final_blob" == "$base_blob" ]] ||
      fail "restored final Blob must equal baseline Blob: $path"
    candidate_blob="$(blob_at "$candidate_tree" "$path")"
    [[ "$candidate_blob" == "$base_blob" ]] ||
      fail "restored candidate Blob differs from baseline: $path"
  elif [[ "$decision" == "keep" ]]; then
    [[ "$expected_status" =~ ^[AMD]$ ]] ||
      fail "kept path has invalid expected status: $path"
    [[ "$status" == "$expected_status" ]] ||
      fail "actual status for $path is ${status:-absent}, expected $expected_status"
    candidate_blob="$(blob_at "$candidate_tree" "$path")"
    [[ -n "$candidate_blob" ]] || candidate_blob="@absent"
    if [[ "$final_blob" == "@self" ]]; then
      [[ "$path" == ".github/custom-upstream-delta.tsv" ]] ||
        fail "@self is only allowed for the delta ledger"
      [[ "$candidate_blob" != "@absent" ]] ||
        fail "delta ledger is absent from candidate tree"
    else
      [[ "$final_blob" == "$candidate_blob" ]] ||
        fail "final Blob mismatch for $path"
      if [[ "$final_blob" != "@absent" ]]; then
        is_sha "$final_blob" || fail "invalid final Blob for $path"
      fi
    fi
    printf '%s\n' "$path" >> "$seen_actual"
  else
    fail "invalid decision for $path: $decision"
  fi

  if [[ "$category" == "official-thin-bridge" ]]; then
    thin_bridge_allowed "$path" ||
      fail "official thin bridge is outside the exact allowlist: $path"
  fi

  if [[ "$shadow_source" != "-" || "$shadow_target" != "-" ]]; then
    [[ "$shadow_source" != "-" && "$shadow_target" != "-" ]] ||
      fail "shadow source and target must be declared together: $path"
    awk -F '\t' -v source="$shadow_source" -v target="$shadow_target" '
      $1 !~ /^#/ && index("|" $1 "|", "|" source "|") &&
      index("|" $2 "|", "|" target "|") { found = 1 }
      END { exit !found }
    ' "$shadow_map" || fail "ledger shadow relation is not in the exact shadow map: $path"
  fi
done < "$rows"

LC_ALL=C sort -u "$seen_actual" -o "$seen_actual"
cut -f2 "$actual" | LC_ALL=C sort -u > "$tmp_dir/actual-paths.txt"
if ! cmp -s "$seen_actual" "$tmp_dir/actual-paths.txt"; then
  echo "ERROR: actual candidate diff and ledger differ:" >&2
  comm -3 "$seen_actual" "$tmp_dir/actual-paths.txt" >&2
  exit 1
fi

python_bin="${PYTHON:-python}"
if ! command -v "$python_bin" >/dev/null 2>&1; then
  python_bin=python3
fi
command -v "$python_bin" >/dev/null 2>&1 || fail "python is required for thin bridge validation"
"$python_bin" "$thin_bridge_validator" \
  --repo-root "$repo_root" \
  --baseline "$CUSTOM_UPSTREAM_BASE_COMMIT" \
  --candidate-tree "$candidate_tree" \
  --contract "$thin_bridge_contract" \
  --ledger "$ledger" \
  --shadow-map "$shadow_map"

echo "custom upstream delta ledger passed ($(wc -l < "$rows" | tr -d ' ') decisions, baseline $CUSTOM_UPSTREAM_BASE_COMMIT, tree $candidate_tree)"
