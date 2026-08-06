#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
shadow_map="${UPSTREAM_SHADOW_MAP:-$repo_root/.github/upstream-shadowed-sources.tsv}"
expected_count="${UPSTREAM_SHADOW_EXPECTED_COUNT:-96}"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

[[ -s "$shadow_map" ]] || fail "shadow source map is missing or empty: $shadow_map"

base_ref="${UPSTREAM_SHADOW_BASE_REF:-}"
if [[ -z "$base_ref" ]]; then
  base_ref="$(git -C "$repo_root" tag --merged HEAD --list 'vendor-*' --sort=-version:refname | sed -n '1p')"
fi
[[ -n "$base_ref" ]] || fail "no vendor-* baseline tag is available"
git -C "$repo_root" rev-parse --verify "${base_ref}^{commit}" >/dev/null
next_ref="${UPSTREAM_SHADOW_NEXT_REF:-}"
if [[ -n "$next_ref" ]]; then
  git -C "$repo_root" rev-parse --verify "${next_ref}^{commit}" >/dev/null
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
rows="$tmp_dir/rows.tsv"
sources="$tmp_dir/sources.txt"
: > "$sources"

awk -F '\t' '
  /^[[:space:]]*$/ || /^#/ { next }
  NF != 2 || $1 == "" || $2 == "" {
    printf "invalid mapping at line %d: expected exactly two non-empty TSV fields\n", NR > "/dev/stderr"
    failed = 1
    next
  }
  { print $1 "\t" $2 }
  END { exit failed }
' "$shadow_map" > "$rows"

row_count="$(wc -l < "$rows" | tr -d ' ')"
[[ "$row_count" -eq "$expected_count" ]] ||
  fail "expected $expected_count effective mappings, found $row_count"

assert_mapping() {
  grep -Fqx -- "$1" "$rows" || fail "required exact shadow mapping is missing: $1"
}

if [[ "$expected_count" -eq 96 ]]; then
  assert_mapping $'backend/internal/repository/content_moderation_repo.go\tbackend/internal/custom/moderation/violation_counter.go'
  assert_mapping $'backend/internal/handler/openai_gateway_cyber_test.go\tbackend/internal/handler/openai_gateway_custom_test.go'
  assert_mapping $'backend/internal/repository/content_moderation_repo_test.go\tbackend/internal/custom/moderation/violation_counter_test.go'
  assert_mapping $'backend/internal/service/content_moderation.go\tbackend/internal/custom/moderation/api_audit_scope.go|backend/internal/custom/moderation/cyber_policy.go|backend/internal/custom/moderation/excerpt.go|backend/internal/custom/moderation/user_ban_threshold.go'
  assert_mapping $'backend/internal/service/content_moderation_cyber_test.go\tbackend/internal/custom/moderation/cyber_policy_test.go|backend/internal/service/custom_moderation_bridge_test.go'
  assert_mapping $'backend/internal/service/content_moderation_test.go\tbackend/internal/custom/moderation/api_audit_scope_test.go|backend/internal/custom/moderation/excerpt_test.go|backend/internal/custom/moderation/user_ban_threshold_test.go|backend/internal/service/custom_moderation_bridge_test.go'
  assert_mapping $'backend/internal/service/content_moderation_email.go\tbackend/internal/custom/moderation/cyber_policy.go'
  assert_mapping $'backend/internal/service/notification_email_service.go\tbackend/internal/custom/moderation/cyber_policy.go'
  assert_mapping $'backend/internal/service/notification_email_service_test.go\tbackend/internal/custom/moderation/cyber_policy_test.go'
  assert_mapping $'frontend/src/api/admin/riskControl.ts\tfrontend/src/custom/moderation/api.ts|frontend/src/custom/moderation/userBanThresholds.ts'
  assert_mapping $'frontend/src/views/user/KeysView.vue\tfrontend/src/custom/api-keys/ApiKeyBulkActions.vue|frontend/src/custom/api-keys/bulkActions.ts|frontend/src/custom/group-access/GroupBalanceWarning.vue|frontend/src/custom/group-access/minimumBalance.ts'
  assert_mapping $'frontend/src/views/user/__tests__/KeysView.spec.ts\tfrontend/src/custom/api-keys/__tests__/ApiKeyBulkActions.spec.ts|frontend/src/custom/api-keys/__tests__/bulkActions.spec.ts|frontend/src/custom/group-access/__tests__/GroupBalanceWarning.spec.ts|frontend/src/custom/group-access/__tests__/minimumBalance.spec.ts'
  assert_mapping $'backend/internal/handler/api_key_handler.go\tbackend/internal/custom/groupaccess/minimum_balance.go'
  assert_mapping $'backend/internal/handler/gateway_handler.go\tbackend/internal/custom/groupaccess/minimum_balance.go'
  assert_mapping $'backend/internal/service/admin_group.go\tbackend/internal/custom/groupaccess/minimum_balance.go'
  assert_mapping $'backend/internal/service/api_key_service.go\tbackend/internal/custom/groupaccess/minimum_balance.go'
  assert_mapping $'backend/internal/service/batch_image_public.go\tbackend/internal/custom/groupaccess/minimum_balance.go'
  assert_mapping $'backend/internal/service/billing_cache_service.go\tbackend/internal/custom/groupaccess/minimum_balance.go|backend/internal/custom/groupaccess/eligibility.go'
  assert_mapping $'frontend/src/i18n/locales/en/dashboard.ts\tfrontend/src/custom/api-keys/i18n.ts'
  assert_mapping $'frontend/src/i18n/locales/zh/dashboard.ts\tfrontend/src/custom/api-keys/i18n.ts'
  assert_mapping $'backend/internal/service/payment_order.go\tbackend/internal/custom/paymentchannels/payment_channels.go|backend/internal/custom/paymentchannels/channel_settings.go|backend/internal/custom/paymentchannels/order_policy.go|backend/internal/custom/paymentchannels/order_coordinator.go|backend/internal/custom/subscriptioninventory/inventory.go'
  assert_mapping $'backend/internal/service/payment_order_result_test.go\tbackend/internal/custom/paymentchannels/payment_channels_test.go|backend/internal/custom/paymentchannels/channel_settings_test.go|backend/internal/custom/paymentchannels/order_policy_test.go|backend/internal/custom/paymentchannels/revalidation_test.go|backend/internal/custom/paymentchannels/order_coordinator_test.go'
  assert_mapping $'backend/internal/handler/payment_handler.go\tbackend/internal/custom/paymentchannels/payment_channels.go|backend/internal/custom/paymentchannels/channel_settings.go'
  assert_mapping $'backend/internal/handler/admin/setting_handler.go\tbackend/internal/custom/paymentchannels/channel_settings.go'
  assert_mapping $'backend/internal/handler/admin/payment_handler.go\tbackend/internal/custom/paymentchannels/channel_settings.go|backend/internal/custom/subscriptioninventory/inventory.go'
  assert_mapping $'backend/internal/service/payment_config_service.go\tbackend/internal/custom/paymentchannels/channel_settings.go|backend/internal/custom/subscriptioninventory/inventory.go'
  assert_mapping $'backend/internal/service/payment_config_service_test.go\tbackend/internal/custom/paymentchannels/channel_settings_test.go|backend/internal/custom/subscriptioninventory/inventory_test.go'
  assert_mapping $'backend/internal/service/payment_config_plans.go\tbackend/internal/custom/subscriptioninventory/inventory.go'
  assert_mapping $'backend/internal/service/payment_fulfillment.go\tbackend/internal/custom/subscriptioninventory/inventory.go'
  assert_mapping $'backend/internal/service/payment_fulfillment_test.go\tbackend/internal/custom/subscriptioninventory/inventory_test.go'
  assert_mapping $'backend/internal/service/payment_order_lifecycle.go\tbackend/internal/custom/subscriptioninventory/inventory.go'
  assert_mapping $'backend/internal/service/payment_order_lifecycle_test.go\tbackend/internal/custom/subscriptioninventory/inventory_test.go'
  assert_mapping $'backend/internal/service/payment_refund_test.go\tbackend/internal/custom/subscriptioninventory/inventory_test.go'
  assert_mapping $'frontend/src/views/admin/orders/AdminPaymentPlansView.vue\tfrontend/src/custom/subscription-plan-inventory/InventoryQuantityCell.vue|frontend/src/custom/subscription-plan-inventory/inventory.ts'
  assert_mapping $'frontend/src/views/admin/orders/PlanEditDialog.vue\tfrontend/src/custom/subscription-plan-inventory/InventoryQuantityInput.vue|frontend/src/custom/subscription-plan-inventory/inventory.ts'
  assert_mapping $'frontend/src/views/admin/orders/__tests__/AdminPaymentPlansView.spec.ts\tfrontend/src/custom/subscription-plan-inventory/__tests__/inventory.spec.ts'
  assert_mapping $'frontend/src/views/admin/orders/__tests__/PlanEditDialog.spec.ts\tfrontend/src/custom/subscription-plan-inventory/__tests__/inventory.spec.ts'
  assert_mapping $'backend/internal/payment/load_balancer.go\tbackend/internal/custom/paymentchannels/payment_channels.go|backend/internal/custom/paymentchannels/revalidation.go|backend/internal/custom/paymentchannels/instance_coordinator.go'
  assert_mapping $'backend/internal/payment/load_balancer_test.go\tbackend/internal/custom/paymentchannels/instance_coordinator_test.go'
  assert_mapping $'backend/internal/service/payment_resume_service_test.go\tbackend/internal/custom/paymentchannels/payment_channels_test.go|backend/internal/custom/paymentchannels/channel_settings_test.go|backend/internal/custom/paymentchannels/resume_policy_test.go'
  assert_mapping $'frontend/src/components/payment/AmountInput.vue\tfrontend/src/custom/payment-channels/paymentMoney.ts'
  assert_mapping $'frontend/src/views/user/PaymentView.vue\tfrontend/src/custom/payment-channels/PaymentChannelSelector.vue|frontend/src/custom/payment-channels/paymentChannels.ts|frontend/src/custom/payment-channels/paymentMoney.ts|frontend/src/custom/payment-channels/usePaymentChannelPricing.ts|frontend/src/custom/payment-channels/usePaymentChannelRecovery.ts|frontend/src/custom/payment-channels/paymentRecoveryRoute.ts'
  assert_mapping $'frontend/src/views/user/__tests__/PaymentView.spec.ts\tfrontend/src/custom/payment-channels/PaymentChannelSelector.spec.ts|frontend/src/custom/payment-channels/paymentChannels.spec.ts|frontend/src/custom/payment-channels/paymentMoney.spec.ts|frontend/src/custom/payment-channels/usePaymentChannelPricing.spec.ts|frontend/src/custom/payment-channels/usePaymentChannelRecovery.spec.ts'
  assert_mapping $'frontend/src/views/admin/SettingsView.vue\tfrontend/src/custom/payment-channels/PaymentChannelSelector.vue|frontend/src/custom/payment-channels/paymentChannels.ts|frontend/src/custom/payment-channels/PaymentChannelSettings.vue|frontend/src/custom/payment-channels/adminPaymentChannels.ts'
  assert_mapping $'frontend/src/views/admin/__tests__/SettingsView.spec.ts\tfrontend/src/custom/payment-channels/PaymentChannelSelector.spec.ts|frontend/src/custom/payment-channels/paymentChannels.spec.ts|frontend/src/custom/payment-channels/PaymentChannelSettings.spec.ts|frontend/src/custom/payment-channels/adminPaymentChannels.spec.ts'
  assert_mapping $'frontend/src/api/admin/payment.ts\tfrontend/src/custom/payment-channels/PaymentChannelSettings.vue'
  assert_mapping $'frontend/src/components/payment/PaymentProviderDialog.vue\tfrontend/src/custom/payment-channels/PaymentChannelSettings.vue'
  assert_mapping $'frontend/src/views/auth/__tests__/WechatPaymentCallbackView.spec.ts\tfrontend/src/custom/payment-channels/paymentChannels.spec.ts'
  assert_mapping $'frontend/src/views/user/__tests__/paymentWechatResume.spec.ts\tfrontend/src/custom/payment-channels/usePaymentChannelRecovery.spec.ts'
  assert_mapping $'frontend/src/views/user/paymentWechatResume.ts\tfrontend/src/custom/payment-channels/paymentRecoveryRoute.ts'
  assert_mapping $'backend/internal/handler/channel_monitor_user_handler.go\tbackend/internal/custom/channelmonitor/group_rate_resolver.go|backend/internal/custom/channelmonitor/group_rate_lookup.go|backend/internal/custom/channelmonitor/runtime_eligibility.go'
  assert_mapping $'backend/internal/service/channel_monitor_service.go\tbackend/internal/custom/channelmonitor/ratedisplay/config.go'
  assert_mapping $'frontend/src/components/user/monitor/MonitorCard.vue\tfrontend/src/custom/channel-monitor/GroupRateBadge.vue|frontend/src/custom/channel-monitor/groupRate.ts'
  assert_mapping $'frontend/src/components/admin/monitor/MonitorFormDialog.vue\tfrontend/src/custom/channel-monitor/groupRate.ts|frontend/src/custom/channel-monitor/MonitorGroupRateFields.vue'
  assert_mapping $'frontend/src/i18n/locales/en/admin/channels.ts\tfrontend/src/custom/moderation/i18n.ts|frontend/src/custom/channel-monitor/GroupRateBadge.vue|frontend/src/custom/channel-monitor/groupRate.ts'
  assert_mapping $'frontend/src/i18n/locales/zh/admin/channels.ts\tfrontend/src/custom/moderation/i18n.ts|frontend/src/custom/channel-monitor/GroupRateBadge.vue|frontend/src/custom/channel-monitor/groupRate.ts'
  assert_mapping $'backend/cmd/server/wire.go\tbackend/internal/custom/channelmonitor/wire.go|backend/internal/custom/moderation/wire.go|backend/internal/custom/updater/wire.go'
  assert_mapping $'backend/internal/handler/admin/system_handler.go\tbackend/internal/custom/updater/service.go'
  assert_mapping $'backend/internal/handler/admin/system_handler_test.go\tbackend/internal/custom/updater/service_test.go'
  assert_mapping $'backend/internal/handler/openai_gateway_handler.go\tbackend/internal/custom/moderation/cyber_policy.go'
  assert_mapping $'backend/internal/handler/wire.go\tbackend/internal/custom/updater/wire.go'
  assert_mapping $'frontend/src/components/layout/AppSidebar.vue\tfrontend/src/custom/updater/components/VersionBadge.vue'
  assert_mapping $'frontend/src/router/index.ts\tfrontend/src/custom/moderation/views/RiskControlView.vue'
  assert_mapping $'frontend/src/views/admin/GroupsView.vue\tfrontend/src/custom/group-access/GroupMinimumBalanceField.vue|frontend/src/custom/group-access/minimumBalance.ts'
fi

validate_relative_path() {
  local kind="$1"
  local path="$2"
  [[ -n "$path" ]] || fail "$kind path must not be empty"
  case "$path" in
    . | .. | /* | [A-Za-z]:* | *\\* | ./* | */./* | ../* | */../* | */.. | *//*)
      fail "$kind path must be a normalized repository-relative path: $path"
      ;;
  esac
}

while IFS=$'\t' read -r source_field target_field; do
  case "$source_field" in
    \|* | *\| | *\|\|*)
      fail "source alternatives must be non-empty repository paths: $source_field"
      ;;
  esac
  case "$target_field" in
    \|* | *\| | *\|\|*)
      fail "target alternatives must be non-empty repository paths: $target_field"
      ;;
  esac
  IFS='|' read -r -a target_paths <<< "$target_field"
  for target in "${target_paths[@]}"; do
    validate_relative_path target "$target"
    case "$target" in
      backend/internal/custom/* | frontend/src/custom/* | \
        backend/internal/handler/openai_gateway_custom_test.go | \
        backend/internal/service/custom_moderation_bridge_test.go) ;;
      *) fail "target is outside an allowed custom directory: $target" ;;
    esac
    [[ -f "$repo_root/$target" ]] || fail "target file does not exist: $target"
  done

  source_in_base=false
  source_in_next=false
  source_removed=false
  real_source_count=0
  IFS='|' read -r -a source_paths <<< "$source_field"
  for source in "${source_paths[@]}"; do
    if [[ "$source" == "@removed" ]]; then
      source_removed=true
      continue
    fi
    validate_relative_path source "$source"
    real_source_count=$((real_source_count + 1))
    printf '%s\n' "$source" >> "$sources"
    if git -C "$repo_root" cat-file -e "${base_ref}:${source}" 2>/dev/null; then
      source_in_base=true
    fi
    if [[ -n "$next_ref" ]]; then
      if git -C "$repo_root" cat-file -e "${next_ref}:${source}" 2>/dev/null; then
        source_in_next=true
      fi
    fi
  done
  [[ "$real_source_count" -gt 0 ]] || fail "mapping must contain at least one real source path: $source_field"
  if [[ -n "$next_ref" ]]; then
    if [[ "$source_removed" != "true" && "$source_in_next" != "true" ]]; then
      fail "no source alternative exists in target ref $next_ref; declare its replacement or append |@removed"
    fi
  elif [[ "$source_in_base" != "true" && "$source_removed" != "true" ]]; then
    fail "no source alternative exists in $base_ref: $source_field"
  fi
done < "$rows"

duplicate_sources="$(sort "$sources" | uniq -d)"
[[ -z "$duplicate_sources" ]] || fail "duplicate source paths detected: $duplicate_sources"

duplicate_pairs="$(sort "$rows" | uniq -d)"
[[ -z "$duplicate_pairs" ]] || fail "duplicate mapping rows detected: $duplicate_pairs"

if [[ "${UPSTREAM_SHADOW_SKIP_BOUNDARY_TEST:-false}" != "true" ]]; then
  boundary_root="${UPSTREAM_SHADOW_BOUNDARY_ROOT:-$repo_root}"
  boundary_violations="$tmp_dir/shadow-boundary-violations.txt"
  : > "$boundary_violations"

is_compatibility_test() {
  case "$1" in
    */__tests__/* | *.spec.ts | *.test.ts | *_test.go) return 0 ;;
    *) return 1 ;;
  esac
}

is_shadowed_source() {
  grep -Fqx -- "$1" "$sources"
}

if [[ -d "$boundary_root/frontend/src" ]]; then
  while IFS= read -r match; do
    file="${match%%:*}"
    relative="${file#"$boundary_root"/}"
    is_compatibility_test "$relative" && continue
    is_shadowed_source "$relative" && continue
    case "$relative" in
      frontend/src/custom/* | frontend/src/components/common/VersionBadge.vue | frontend/src/components/layout/AppSidebar.vue) ;;
      *) echo "$match" >> "$boundary_violations" ;;
    esac
  done < <(grep -RInE --include='*.ts' --include='*.tsx' --include='*.vue' 'VersionBadge' "$boundary_root/frontend/src" || true)

  while IFS= read -r match; do
    file="${match%%:*}"
    relative="${file#"$boundary_root"/}"
    is_compatibility_test "$relative" && continue
    case "$relative" in
      frontend/src/custom/* | frontend/src/views/admin/RiskControlView.vue | frontend/src/router/index.ts) ;;
      *) echo "$match" >> "$boundary_violations" ;;
    esac
  done < <(grep -RInE --include='*.ts' --include='*.tsx' --include='*.vue' 'RiskControlView' "$boundary_root/frontend/src" || true)

  while IFS= read -r match; do
    file="${match%%:*}"
    relative="${file#"$boundary_root"/}"
    is_compatibility_test "$relative" && continue
    case "$relative" in
      frontend/src/custom/* | frontend/src/stores/app.ts | frontend/src/components/common/VersionBadge.vue) ;;
      *) echo "$match" >> "$boundary_violations" ;;
    esac
  done < <(grep -RInE --include='*.ts' --include='*.tsx' --include='*.vue' 'api/admin/system' "$boundary_root/frontend/src" || true)

  while IFS= read -r match; do
    file="${match%%:*}"
    relative="${file#"$boundary_root"/}"
    is_compatibility_test "$relative" && continue
    is_shadowed_source "$relative" && continue
    case "$relative" in
      frontend/src/custom/* | frontend/src/components/layout/AppSidebar.vue | \
        frontend/src/router/index.ts | frontend/src/views/user/KeysView.vue) ;;
      *) echo "$match" >> "$boundary_violations" ;;
    esac
  done < <(
    grep -RInE \
      --include='*.ts' \
      --include='*.tsx' \
      --include='*.vue' \
      '@/custom/(updater|moderation|group-access)(/|[^[:alnum:]_]|$)|useCustomUpdaterStore' \
      "$boundary_root/frontend/src" || true
  )

  while IFS= read -r match; do
    file="${match%%:*}"
    relative="${file#"$boundary_root"/}"
    is_compatibility_test "$relative" && continue
    is_shadowed_source "$relative" && continue
    case "$relative" in
      frontend/src/custom/*) ;;
      *) echo "$match" >> "$boundary_violations" ;;
    esac
  done < <(
    grep -RInE \
      --include='*.ts' \
      --include='*.tsx' \
      --include='*.vue' \
      '(^|[^[:alnum:]_])(fetchVersion|clearVersionCache|versionLoading|officialLatestVersion|hasOfficialUpdate|officialReleaseInfo|officialReleaseWarning|updateRepository)([^[:alnum:]_]|$)' \
      "$boundary_root/frontend/src" || true
  )
fi

if [[ -d "$boundary_root/backend" ]]; then
  while IFS= read -r match; do
    file="${match%%:*}"
    relative="${file#"$boundary_root"/}"
    is_shadowed_source "$relative" && continue
    case "$relative" in
      backend/internal/custom/*) ;;
      *) echo "$match" >> "$boundary_violations" ;;
    esac
  done < <(
    grep -RInE \
      --include='*.go' \
      '(^|[^[:alnum:]_])(NewUpdateService|ProvideUpdateService|NewGitHubReleaseClient|ProvideGitHubReleaseClient)([^[:alnum:]_]|$)' \
      "$boundary_root/backend" || true
  )

  while IFS= read -r match; do
    file="${match%%:*}"
    relative="${file#"$boundary_root"/}"
    is_shadowed_source "$relative" && continue
    case "$relative" in
      backend/internal/custom/* | \
        backend/cmd/server/wire.go | backend/cmd/server/wire_gen.go | \
        backend/internal/handler/wire.go | backend/internal/handler/api_key_handler.go | \
        backend/internal/handler/gateway_handler.go | \
        backend/internal/handler/gateway_handler_billing_error_test.go | \
        backend/internal/handler/admin/system_handler.go | \
        backend/internal/handler/admin/system_handler_test.go | \
        backend/internal/service/api_key_service.go | \
        backend/internal/service/api_key_service_delete_test.go | \
        backend/internal/service/batch_image_public.go | \
        backend/internal/service/batch_image_public_test.go | \
        backend/internal/service/billing_cache_service.go | \
        backend/internal/service/billing_cache_service_balance_test.go | \
        backend/internal/service/custom_moderation_bridge.go | \
        backend/internal/service/custom_moderation_bridge_test.go) ;;
      *) echo "$match" >> "$boundary_violations" ;;
    esac
  done < <(
    grep -RInE \
      --include='*.go' \
      'github\.com/Wei-Shaw/sub2api/internal/custom/(updater|moderation|groupaccess)(/|[^[:alnum:]_]|$)' \
      "$boundary_root/backend" || true
  )
fi

  if [[ -s "$boundary_violations" ]]; then
    echo "Shadowed official runtime references are not allowed outside compatibility sources/tests:" >&2
    sed 's/^/  /' "$boundary_violations" >&2
    exit 1
  fi
fi

sort -u "$sources" > "$tmp_dir/shadowed-source-paths.txt"
if [[ "${UPSTREAM_SHADOW_SKIP_DETECTOR_TEST:-false}" != "true" ]]; then
  official_files="${UPSTREAM_SHADOW_OFFICIAL_FILES:-}"
  detector_output="${UPSTREAM_SHADOW_DETECTED_OUTPUT:-$tmp_dir/shadowed-source-changes.txt}"
  if [[ -n "$official_files" ]]; then
    [[ -f "$official_files" ]] || fail "official changed-files input does not exist: $official_files"
    sort -u "$official_files" > "$tmp_dir/official-files.txt"
  else
    cat > "$tmp_dir/official-files.txt" <<'EOF'
backend/internal/repository/github_release_service.go
backend/internal/repository/github_release_service_test.go
backend/internal/repository/content_moderation_repo.go
backend/internal/repository/content_moderation_repo_test.go
backend/internal/repository/wire.go
backend/cmd/server/wire.go
backend/internal/handler/wire.go
backend/internal/handler/admin/system_handler.go
backend/internal/handler/admin/system_handler_test.go
backend/internal/handler/admin/payment_handler.go
backend/internal/handler/admin/setting_handler.go
backend/internal/handler/admin/setting_handler_update.go
backend/internal/handler/auth_wechat_oauth.go
backend/internal/handler/auth_wechat_oauth_test.go
backend/internal/handler/api_key_handler.go
backend/internal/handler/channel_monitor_user_handler.go
backend/internal/handler/dto/settings.go
backend/internal/handler/gateway_handler.go
backend/internal/handler/payment_handler.go
backend/internal/handler/payment_handler_resume_test.go
backend/internal/handler/openai_gateway_handler.go
backend/internal/handler/openai_gateway_cyber_test.go
backend/internal/payment/load_balancer.go
backend/internal/payment/load_balancer_test.go
backend/internal/service/content_moderation.go
backend/internal/service/content_moderation_companion.go
backend/internal/service/content_moderation_cyber_test.go
backend/internal/service/content_moderation_email.go
backend/internal/service/content_moderation_test.go
backend/internal/service/channel_monitor_service.go
backend/internal/service/notification_email_service.go
backend/internal/service/notification_email_service_test.go
backend/internal/service/admin_group.go
backend/internal/service/api_key_service.go
backend/internal/service/batch_image_public.go
backend/internal/service/billing_cache_service.go
backend/internal/service/payment_config_limits.go
backend/internal/service/payment_config_limits_test.go
backend/internal/service/payment_config_service.go
backend/internal/service/payment_config_service_test.go
backend/internal/service/payment_config_plans.go
backend/internal/service/payment_fulfillment.go
backend/internal/service/payment_fulfillment_test.go
backend/internal/service/payment_order.go
backend/internal/service/payment_order_lifecycle.go
backend/internal/service/payment_order_lifecycle_test.go
backend/internal/service/payment_order_result_test.go
backend/internal/service/payment_refund_test.go
backend/internal/service/payment_resume_service.go
backend/internal/service/payment_resume_service_test.go
backend/internal/service/payment_service.go
backend/internal/service/update_service.go
backend/internal/service/update_service_test.go
backend/internal/service/wire.go
frontend/src/api/__tests__/admin.system.rollback.spec.ts
frontend/src/api/admin/payment.ts
frontend/src/api/admin/riskControl.ts
frontend/src/api/admin/settings.ts
frontend/src/api/admin/system.ts
frontend/src/components/common/VersionBadge.vue
frontend/src/components/common/__tests__/VersionBadge.rollback.spec.ts
frontend/src/components/layout/AppSidebar.vue
frontend/src/components/payment/AmountInput.vue
frontend/src/components/payment/PaymentProviderDialog.vue
frontend/src/components/payment/paymentFlow.ts
frontend/src/components/payment/__tests__/paymentFlow.spec.ts
frontend/src/components/admin/monitor/MonitorFormDialog.vue
frontend/src/components/user/monitor/MonitorCard.vue
frontend/src/i18n/locales/en/admin/settings.ts
frontend/src/i18n/locales/en/dashboard.ts
frontend/src/i18n/locales/en/admin/channels.ts
frontend/src/i18n/locales/en/misc.ts
frontend/src/i18n/locales/zh/admin/settings.ts
frontend/src/i18n/locales/zh/dashboard.ts
frontend/src/i18n/locales/zh/admin/channels.ts
frontend/src/i18n/locales/zh/misc.ts
frontend/src/stores/app.ts
frontend/src/types/payment.ts
frontend/src/utils/__tests__/releaseNotes.spec.ts
frontend/src/utils/releaseNotes.ts
frontend/src/views/admin/RiskControlView.vue
frontend/src/views/admin/__tests__/RiskControlView.spec.ts
frontend/src/views/admin/GroupsView.vue
frontend/src/views/admin/SettingsView.vue
frontend/src/views/admin/__tests__/SettingsView.spec.ts
frontend/src/views/admin/orders/AdminPaymentPlansView.vue
frontend/src/views/admin/orders/PlanEditDialog.vue
frontend/src/views/admin/orders/__tests__/AdminPaymentPlansView.spec.ts
frontend/src/views/admin/orders/__tests__/PlanEditDialog.spec.ts
frontend/src/views/auth/__tests__/WechatPaymentCallbackView.spec.ts
frontend/src/views/user/KeysView.vue
frontend/src/views/user/__tests__/KeysView.spec.ts
frontend/src/views/user/PaymentView.vue
frontend/src/views/user/__tests__/PaymentView.spec.ts
frontend/src/views/user/__tests__/paymentWechatResume.spec.ts
frontend/src/views/user/paymentWechatResume.ts
frontend/src/router/index.ts
backend/internal/service/not_content_moderation_companion.go
unmapped/fixture-must-not-match.txt
EOF
    sort -u -o "$tmp_dir/official-files.txt" "$tmp_dir/official-files.txt"
  fi

  : > "$detector_output"
  while IFS= read -r official_path; do
    if grep -Fqx -- "$official_path" "$tmp_dir/shadowed-source-paths.txt"; then
      printf '%s\n' "$official_path" >> "$detector_output"
      continue
    fi
    while IFS= read -r source_path; do
      source_dir="${source_path%/*}"
      source_name="${source_path##*/}"
      case "$source_name" in
        *.*)
          source_stem="${source_name%.*}"
          source_extension=".${source_name##*.}"
          ;;
        *) continue ;;
      esac
      case "$official_path" in
        "$source_dir/$source_stem"_*"$source_extension")
          printf '%s\n' "$official_path" >> "$detector_output"
          break
          ;;
      esac
    done < "$tmp_dir/shadowed-source-paths.txt"
  done < "$tmp_dir/official-files.txt"
  sort -u -o "$detector_output" "$detector_output"

  if [[ -z "$official_files" ]]; then
    source_count="$(wc -l < "$tmp_dir/shadowed-source-paths.txt" | tr -d ' ')"
    detected_count="$(wc -l < "$detector_output" | tr -d ' ')"
    [[ "$detected_count" -eq $((source_count + 1)) ]] ||
      fail "expected every exact source and one companion match; detected $detected_count paths for $source_count sources"
    grep -Fqx 'backend/internal/service/content_moderation_companion.go' "$detector_output" ||
      fail "the detector missed a same-family companion file"
    if grep -Fqx 'backend/internal/service/not_content_moderation_companion.go' "$detector_output"; then
      fail "the detector matched a path whose basename only contains the mapped stem"
    fi
    if grep -Fqx 'unmapped/fixture-must-not-match.txt' "$detector_output"; then
      fail "the detector matched an unmapped official path"
    fi
  fi
fi

source_count="$(wc -l < "$tmp_dir/shadowed-source-paths.txt" | tr -d ' ')"

echo "upstream shadow map and runtime-boundary checks passed ($expected_count mappings, $source_count source paths, baseline $base_ref)"
