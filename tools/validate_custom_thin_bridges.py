#!/usr/bin/env python3
"""Validate the exact structural contract for official thin bridges."""

from __future__ import annotations

import argparse
import csv
import re
import subprocess
import sys
from collections import Counter
from dataclasses import dataclass
from pathlib import Path


CONTRACT_HEADER = [
    "path",
    "kind",
    "shadow_required",
    "approved_additions",
    "approved_deletions",
    "baseline_budget_overrides",
]
ALLOWED_KINDS = {"delegate", "view", "dto", "wire", "persistence", "compat-test"}
CUSTOM_IMPORT_RE = re.compile(r"(?:internal/custom/|@/custom/)")
CONTROL_FLOW_RE = re.compile(
    r"^\s*(?:for\s*(?:\(|\w+\s*:?=|\w+\s+(?:in|of)\b)|while\s*\(|"
    r"watch(?:Effect)?\s*\()|\.forEach\s*\(",
    re.MULTILINE,
)
ORCHESTRATION_RE = re.compile(
    r"\b(?:orchestrat|coordinat|workflow|pipeline|fallback|retry)\w*\s*\(",
    re.IGNORECASE,
)
CALL_SURFACE_PATTERNS = (
    re.compile(
        r"(?<![\w$])(?P<callee>[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*)"
        r"\s*(?:\?\.)?\s*\("
    ),
    re.compile(
        r"\(\s*(?P<callee>[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*)"
        r"\s*\)\s*(?:\?\.)?\s*\("
    ),
)
COMPUTED_CALL_SURFACE_RE = re.compile(
    r"(?:(?P<object>[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*)\s*)?"
    r"(?:\?\.)?\[\s*(?P<key>[^\]\r\n]+?)\s*\]\s*\)*\s*(?:\?\.)?\s*\("
)
COMPUTED_CALL_FALLBACK_RE = re.compile(r"\]\s*\)*\s*(?:\?\.)?\s*\(")
VUE_EVENT_BINDING_RE = re.compile(
    r"(?P<binding>"
    r"@(?:\[[^\]\r\n]+\]|[A-Za-z_][\w:-]*)(?:\.[A-Za-z_][\w-]*)*"
    r"|v-on:(?:\[[^\]\r\n]+\]|[A-Za-z_][\w:-]*)(?:\.[A-Za-z_][\w-]*)*"
    r")\s*=\s*(?P<quote>[\"'])(?P<expression>.*?)(?P=quote)",
    re.DOTALL,
)
CALL_SURFACE_IGNORED = frozenset({
    "async", "await", "catch", "delete", "for", "func", "function", "if", "len",
    "make", "new", "return", "switch", "typeof", "while",
})
DELEGATE_VIEW_CONTROL_FLOW_RE = re.compile(
    r"^\s*(?:}\s*)?(?:else\s+)?(?:if\b|switch\b|select\b|for\b|while\b|"
    r"do\b|try\b|catch\b)|\bv-(?:if|else-if|for)\s*=",
    re.IGNORECASE,
)
DTO_WIRE_CONTROL_FLOW_RE = re.compile(r"^\s*(?:if\s*[\s(]|switch\s*[\s(])", re.MULTILINE)
BUSINESS_HELPER_RE = re.compile(
    r"(?:^|\s)(?:func(?:\s*\([^)]*\))?\s+|function\s+|const\s+)"
    r"(?:select|resolve|validate|calculate|fallback|retry|fee|balance|channel|payment|eligib|rate)\w*",
    re.IGNORECASE,
)

HIGH_RISK_DEFINITIONS: dict[str, tuple[re.Pattern[str], ...]] = {
    "backend/internal/service/payment_config_limits.go": tuple(
        re.compile(rf"\bfunc\s+(?:\([^)]*\)\s*)?{name}\s*\(")
        for name in (
            "pcPaymentChannelInstances",
            "pcInstancePaymentChannelLimits",
            "pcInstancePaymentTypes",
            "pcInstanceSupportsPaymentType",
        )
    ),
    "backend/internal/service/billing_cache_service.go": tuple(
        re.compile(rf"\bfunc\s+(?:\([^)]*\)\s*)?{name}\s*\(")
        for name in ("minimumBalanceGroupsForRequest", "checkGroupMinimumBalanceEligibility")
    ),
    "frontend/src/views/user/PaymentView.vue": tuple(
        re.compile(rf"\bfunction\s+{name}\s*\(")
        for name in (
            "amountFitsChannel",
            "balancePrincipalAmountForGatewayLimit",
            "attemptMobileQrFallback",
            "appendBackupChannelHint",
        )
    ),
    "frontend/src/components/admin/monitor/MonitorFormDialog.vue": (
        re.compile(r"\bfunction\s+normalizedGroupRateOverride\s*\("),
        re.compile(r"\bfunction\s+validateGroupRateDisplayTemplate\s*\("),
    ),
    "frontend/src/views/admin/GroupsView.vue": (
        re.compile(r"v-model\.number=[\"'](?:createForm|editForm)\.minimum_balance[\"']"),
        re.compile(r"Number\((?:createForm|editForm)\.minimum_balance\)"),
    ),
}

# New functions in official delegate/view bridges are denied by default. The
# names below are the reviewed persistence, DTO, or one-call adapter functions
# required by the current Custom boundary.
APPROVED_NEW_BRIDGE_FUNCTIONS: dict[str, frozenset[str]] = {
    "backend/internal/handler/admin/subscription_handler.go": frozenset({
        "BulkResetQuota",
        "ListBulkResetQuotaCandidates",
    }),
    "backend/internal/handler/channel_monitor_user_handler.go": frozenset({"SetGroupRateResolver"}),
    "backend/internal/payment/load_balancer.go": frozenset({
        "RevalidateSelection",
        "instanceCoordinator",
        "paymentSelectionFromCustom",
        "customSelectionFromPayment",
        "LoadEnabledInstances",
        "LoadInstance",
        "LoadDailyUsage",
        "paymentInstanceRecord",
    }),
    "backend/internal/service/admin_group.go": frozenset({"checkGroupMinimumBalanceForUser"}),
    "backend/internal/service/api_key_service.go": frozenset({
        "GetAvailableGroupOptions",
        "availableGroupsForUser",
    }),
    "backend/internal/service/billing_cache_service.go": frozenset({
        "LoadMinimumBalanceGroup",
        "LoadCurrentBalance",
        "checkCustomMinimumBalanceEligibility",
        "minimumBalanceGroupSnapshot",
    }),
    "backend/internal/service/channel_monitor_service.go": frozenset({
        "cloneFloat64Pointer",
        "validateGroupRateOverride",
        "validateGroupRateDisplayTemplate",
        "normalizeGroupRateDisplayTemplate",
    }),
    "backend/internal/service/payment_config_limits.go": frozenset({
        "GetAvailableMethodOptions",
        "ValidateMethodProviderCurrencyConsistency",
        "HasConfiguredProviderPaymentType",
        "pcPaymentProviderRecords",
    }),
    "backend/internal/service/payment_config_service.go": frozenset({"setPaymentConfigValue"}),
    "backend/internal/service/payment_order.go": frozenset({
        "customOrderPreparationRequest",
        "HasConfiguredSelection",
        "LoadMethodCurrency",
        "CalculatePayAmount",
        "SelectOrderInstance",
        "RevalidateOrderInstance",
        "ValidatePayAmountCurrency",
        "UsesOfficialWeChatVisibleMethod",
        "LoadWeChatOAuthAppID",
        "BuildWeChatOAuth",
        "customOrderSelection",
        "paymentSelectionFromOrder",
        "buildOrderOAuthResponse",
    }),
    "backend/internal/service/payment_resume_service.go": frozenset({
        "RevalidateSelection",
        "CreateWeChatPaymentOAuthToken",
        "ParseWeChatPaymentOAuthToken",
    }),
    "backend/internal/service/subscription_service.go": frozenset({
        "AdjustSubscriptionForRefund",
        "AdminResetQuotaIdempotent",
        "FinalizeSubscriptionRefundDeduction",
        "RestoreSubscriptionTermAfterRefund",
        "invalidateAdjustedSubscriptionCache",
    }),
    "frontend/src/components/payment/SubscriptionPlanCard.vue": frozenset({"handleSelect"}),
    "frontend/src/views/admin/orders/PlanEditDialog.vue": frozenset({
        "handleRemainingQuantityInput",
        "remainingQuantityError",
        "togglePlanForSale",
    }),
    "frontend/src/views/admin/SubscriptionsView.vue": frozenset({"cancelResetQuota"}),
    "frontend/src/views/user/KeysView.vue": frozenset({
        "refreshApiKeys",
        "handleTableSelectionChange",
        "handleBulkCompleted",
    }),
}


def _approved_call_deltas(
    *groups: tuple[str, dict[str, int]],
) -> tuple[tuple[str, str], ...]:
    result: list[tuple[str, str]] = []
    for function_name, calls in groups:
        for callee, count in calls.items():
            result.extend((function_name, callee) for _ in range(count))
    return tuple(result)


# Positive call-surface deltas are exact per path, owning function, callee and
# count. Updating a ledger Blob or line budget cannot authorize an additional
# delegate/view operation without an explicit structural review here.
APPROVED_DELEGATE_VIEW_CALL_DELTAS: dict[str, tuple[tuple[str, str], ...]] = {
    'backend/internal/handler/admin/subscription_handler.go': _approved_call_deltas(
        ('<top-level>', {
            'AdminResetQuota': 1,
            'AdminResetQuotaIdempotent': 1,
            'const': 1,
        }),
        ('BulkResetQuota', {
            'adminActorScope': 1,
            'c.GetHeader': 1,
            'c.ShouldBindJSON': 1,
            'claimedAt.Add': 1,
            'err.Error': 1,
            'executeAdminIdempotentJSON': 1,
            'h.bulkResetService.ResetSelected': 1,
            'idempotencyexecution.FromContext': 1,
            'idempotencyexecution.New': 1,
            'response.BadRequest': 4,
            'response.ErrorFrom': 2,
            'service.DefaultWriteIdempotencyTTL': 1,
            'service.HashIdempotencyKey': 1,
            'service.NormalizeIdempotencyKey': 1,
            'strconv.Itoa': 1,
            'time.Now': 1,
        }),
        ('ListBulkResetQuotaCandidates', {
            'c.Request.Context': 1,
            'h.bulkResetService.ListCandidates': 1,
            'response.ErrorFrom': 1,
            'response.Success': 1,
        }),
        ('ResetQuota', {
            'adminActorScope': 1,
            'c.GetHeader': 1,
            'claimedAt.Add': 1,
            'dto.UserSubscriptionFromServiceAdmin': 1,
            'executeAdminIdempotentJSON': 1,
            'idempotencyexecution.FromContext': 1,
            'idempotencyexecution.New': 1,
            'resetter.AdminResetQuota': 1,
            'resetter.AdminResetQuotaIdempotent': 1,
            'response.ErrorFrom': 2,
            'service.DefaultWriteIdempotencyTTL': 1,
            'service.HashIdempotencyKey': 1,
            'service.NormalizeIdempotencyKey': 1,
            'time.Now': 1,
        }),
    ),
    'backend/internal/handler/admin/setting_handler.go': _approved_call_deltas(
        ('GetSettings', {'response.ErrorFrom': 1}),
    ),
    'backend/internal/service/idempotency.go': _approved_call_deltas(
        ('Execute', {
            'idempotencyexecution.New': 1,
            'idempotencyexecution.WithContext': 1,
        }),
    ),
    'backend/internal/handler/admin/setting_handler_update.go': _approved_call_deltas(
        ('UpdateSettings', {'response.ErrorFrom': 1}),
    ),
    'backend/internal/handler/admin/system_handler.go': _approved_call_deltas(
        ('Rollback', {'response.Error': 1}),
    ),
    'backend/internal/handler/api_key_handler.go': _approved_call_deltas(
        ('GetAvailableGroups', {'h.apiKeyService.GetAvailableGroupOptions': 1}),
    ),
    'backend/internal/handler/auth_wechat_oauth.go': _approved_call_deltas(
        ('WeChatPaymentOAuthCallback', {'redirectOAuthError': 1, 'strings.ToLower': 1, 'strings.TrimSpace': 1}),
        ('WeChatPaymentOAuthStart', {'ParseWeChatPaymentOAuthToken': 1, 'c.Query': 2, 'h.wechatPaymentResumeService': 1, 'response.BadRequest': 1, 'response.ErrorFrom': 1, 'strings.ToLower': 1, 'strings.TrimSpace': 2}),
    ),
    'backend/internal/handler/channel_monitor_user_handler.go': _approved_call_deltas(
        ('<top-level>', {'Resolve': 1}),
        ('List', {'c.Request.Context': 1, 'h.groupRateResolver.Resolve': 1}),
    ),
    'backend/internal/handler/gateway_handler.go': _approved_call_deltas(
        ('billingErrorDetails', {'pkgerrors.Message': 1, 'pkgerrors.Reason': 1}),
    ),
    'backend/internal/handler/openai_gateway_handler.go': _approved_call_deltas(
        ('recordCyberPolicyIfMarked', {'c.Request.Context': 1, 'cmSvc.CyberPolicyGroupInScope': 1}),
        ('rejectIfCyberSessionBlocked', {'c.Request.Context': 1, 'h.contentModerationService.CyberPolicyGroupInScope': 1}),
    ),
    'backend/internal/server/routes/admin.go': _approved_call_deltas(
        ('registerSubscriptionRoutes', {
            'subscriptions.GET': 1,
            'subscriptions.POST': 1,
        }),
    ),
    'backend/internal/handler/payment_handler.go': _approved_call_deltas(
        ('GetCheckoutInfo', {'h.configService.GetAvailableMethodOptions': 1, 'response.ErrorFrom': 1, 'subscriptioninventory.IsSoldOut': 1}),
        ('GetPlans', {'subscriptioninventory.IsSoldOut': 1}),
        ('applyWeChatPaymentResumeClaims', {'infraerrors.BadRequest': 3, 'math.IsInf': 1, 'math.IsNaN': 1, 'paymentchannels.IsValidSelection': 1, 'strings.EqualFold': 1, 'strings.ToLower': 1, 'strings.TrimSpace': 2}),
    ),
    'backend/internal/payment/load_balancer.go': _approved_call_deltas(
        ('<top-level>', {'RevalidateSelection': 1}),
        ('LoadDailyUsage', {'Aggregate': 1, 'GroupBy': 1, 'Scan': 1, 'Where': 1, 'dbent.Sum': 1, 'lb.db.PaymentOrder.Query': 1, 'paymentorder.CreatedAtGTE': 1, 'paymentorder.ProviderInstanceIDIn': 1, 'paymentorder.StatusIn': 1, 'startOfDay': 1, 'time.Now': 1}),
        ('LoadEnabledInstances', {'All': 1, 'Where': 1, 'append': 1, 'dbent.Asc': 1, 'fmt.Errorf': 1, 'lb.db.PaymentProviderInstance.Query': 1, 'lb.paymentInstanceRecord': 1, 'paymentproviderinstance.Enabled': 1, 'paymentproviderinstance.ProviderKey': 1, 'query.Order': 1, 'query.Where': 1}),
        ('LoadInstance', {'dbent.IsNotFound': 1, 'fmt.Errorf': 1, 'lb.db.PaymentProviderInstance.Get': 1, 'lb.paymentInstanceRecord': 1}),
        ('NewDefaultLoadBalancer', {'paymentchannels.NewInstanceCoordinator': 1}),
        ('RevalidateSelection', {'Revalidate': 1, 'customSelectionFromPayment': 1, 'lb.instanceCoordinator': 1}),
        ('SelectInstance', {'Select': 1, 'lb.instanceCoordinator': 1, 'paymentSelectionFromCustom': 1, 'slog.Info': 1, 'string': 1, 'wxpayJSAPIAppIDFromContext': 1}),
        ('instanceCoordinator', {'lb.coordinatorOnce.Do': 1, 'paymentchannels.NewInstanceCoordinator': 1}),
        ('paymentInstanceRecord', {'fmt.Errorf': 1, 'lb.decryptConfig': 1}),
    ),
    'backend/internal/service/admin_group.go': _approved_call_deltas(
        ('AdminUpdateAPIKeyGroupID', {'fmt.Errorf': 1, 's.apiKeyRepo.Update': 1, 's.authCacheInvalidator.InvalidateAuthCacheByKey': 1, 's.checkGroupMinimumBalanceForUser': 1}),
        ('CreateGroup', {'infraerrors.BadRequest': 1, 'math.IsInf': 1, 'math.IsNaN': 1}),
        ('ReplaceUserGroup', {'s.checkGroupMinimumBalanceForUser': 1}),
        ('UpdateGroup', {'infraerrors.BadRequest': 1, 'math.IsInf': 1, 'math.IsNaN': 1}),
        ('checkGroupMinimumBalanceForUser', {'groupaccess.CheckMinimumBalance': 1, 'infraerrors.InternalServer': 1, 's.userRepo.GetByID': 1}),
    ),
    'backend/internal/service/api_key_service.go': _approved_call_deltas(
        ('Create', {'groupaccess.CheckMinimumBalance': 1}),
        ('GetAvailableGroupOptions', {'append': 1, 'groupaccess.EvaluateMinimumBalance': 1, 's.availableGroupsForUser': 1}),
        ('GetAvailableGroups', {'s.availableGroupsForUser': 1}),
        ('Update', {'groupaccess.CheckMinimumBalance': 1}),
        ('availableGroupsForUser', {'append': 1, 'fmt.Errorf': 3, 's.canUserBindGroupInternal': 1, 's.groupRepo.ListActive': 1, 's.userRepo.GetByID': 1, 's.userSubRepo.ListActiveByUserID': 1}),
    ),
    'backend/internal/service/batch_image_public.go': _approved_call_deltas(
        ('Submit', {'ErrBillingServiceUnavailable.WithCause': 1, 'groupaccess.CheckMinimumBalance': 1, 's.UserRepo.GetByID': 1}),
    ),
    'backend/internal/service/billing_cache_service.go': _approved_call_deltas(
        ('<top-level>', {'GetGroupByIDForMinimumBalance': 1}),
        ('CheckBillingEligibility', {'s.checkCustomMinimumBalanceEligibility': 1}),
        ('LoadCurrentBalance', {'a.service.getUserBalanceFromDB': 1}),
        ('LoadMinimumBalanceGroup', {'a.loader.GetGroupByIDForMinimumBalance': 1, 'minimumBalanceGroupSnapshot': 1}),
        ('checkCustomMinimumBalanceEligibility', {'ErrBillingServiceUnavailable.WithCause': 1, 'IsClaudeCodeClient': 1, 'checker.Check': 1, 'ctx.Value': 1, 'errors.As': 1, 'errors.Is': 1, 'groupaccess.NewEligibilityChecker': 1, 'logger.LegacyPrintf': 1, 'minimumBalanceGroupSnapshot': 1}),
    ),
    'backend/internal/service/channel_monitor_service.go': _approved_call_deltas(
        ('Create', {'cloneFloat64Pointer': 1, 'normalizeGroupRateDisplayTemplate': 1}),
        ('Duplicate', {'cloneFloat64Pointer': 1}),
        ('applyMonitorUpdate', {'cloneFloat64Pointer': 1, 'normalizeGroupRateDisplayTemplate': 1, 'validateGroupRateDisplayTemplate': 1, 'validateGroupRateOverride': 1}),
        ('normalizeGroupRateDisplayTemplate', {'channelmonitorratedisplay.NormalizeTemplate': 1}),
        ('validateCreateParams', {'validateGroupRateDisplayTemplate': 1, 'validateGroupRateOverride': 1}),
        ('validateGroupRateDisplayTemplate', {'channelmonitorratedisplay.NormalizeTemplate': 1}),
        ('validateGroupRateOverride', {'channelmonitorratedisplay.ValidOverride': 1}),
    ),
    'backend/internal/service/content_moderation.go': _approved_call_deltas(
        ('Check', {
            'applyCustomContentModerationKeywordExcerpt': 1,
            'cfg.includesAPIAuditGroup': 1,
            'content.ExcerptText': 1,
            'contentModerationLogGroupID': 1,
            's.recordPreBlockSyncMetric': 1,
            'slog.Info': 1,
        }),
        ('RecordCyberPolicyEvent', {'s.tryRecordCustomCyberPolicyEvent': 1}),
        ('UpdateConfig', {
            'cloneContentModerationUserBanThresholdOverrides': 1,
            'normalizeContentModerationAPIAuditScope': 1,
        }),
        ('applyFlaggedAccountSideEffects', {'s.countFlaggedByUserSince': 1}),
        ('cloneContentModerationConfig', {
            'cloneContentModerationUserBanThresholdOverrides': 1,
            'normalizeContentModerationAPIAuditScope': 1,
        }),
        ('configView', {
            'cloneContentModerationUserBanThresholdOverrides': 1,
            'normalizeContentModerationAPIAuditScope': 1,
        }),
        ('defaultContentModerationConfig', {'defaultContentModerationAPIAuditScope': 1}),
        ('normalize', {
            'cloneContentModerationUserBanThresholdOverrides': 1,
            'normalizeContentModerationAPIAuditScope': 1,
        }),
        ('persistContentModerationLog', {'effectiveContentModerationConfigForUser': 1}),
        ('sendCyberPolicyEmail', {'contentModerationEmailVariables': 1}),
        ('validateConfig', {
            'err.Error': 2,
            'fmt.Sprintf': 1,
            'infraerrors.BadRequest': 3,
            's.groupRepo.GetByIDLite': 1,
            'validateContentModerationAPIAuditScope': 1,
            'validateContentModerationUserBanThresholdOverrides': 1,
        }),
        ('worker', {
            'cfg.includesAPIAuditGroup': 1,
            's.loadRuntimeSnapshot': 1,
        }),
    ),
    'backend/internal/service/payment_config_limits.go': _approved_call_deltas(
        ('GetAvailableMethodOptions', {'All': 1, 'BuildOptions': 1, 'Where': 1, 'fmt.Errorf': 1, 'paymentproviderinstance.EnabledEQ': 1, 's.entClient.PaymentProviderInstance.Query': 1, 's.pcPaymentProviderRecords': 1}),
        ('HasConfiguredProviderPaymentType', {'All': 1, 'HasConfiguredSelection': 1, 'NormalizeVisibleMethod': 1, 'fmt.Errorf': 1, 's.entClient.PaymentProviderInstance.Query': 1, 's.pcPaymentProviderRecords': 1, 'strings.ToLower': 1, 'strings.TrimSpace': 1}),
        ('ValidateMethodProviderCurrencyConsistency', {'All': 1, 'NormalizeVisibleMethod': 1, 'ValidateCurrency': 1, 'Where': 1, 'WithMetadata': 1, 'fmt.Errorf': 1, 'infraerrors.ServiceUnavailable': 1, 'paymentproviderinstance.EnabledEQ': 1, 's.ValidateMethodCurrencyConsistency': 1, 's.entClient.PaymentProviderInstance.Query': 1, 's.pcPaymentProviderRecords': 1, 'strings.ToLower': 1, 'strings.TrimSpace': 1}),
        ('pcPaymentProviderRecords', {'append': 1, 'int64': 1, 'paymentProviderConfigCurrency': 1, 's.decryptConfig': 1}),
    ),
    'backend/internal/service/payment_config_plans.go': _approved_call_deltas(
        ('CreatePlan', {'SetAllowBulkQuotaReset': 1, 'SetNillableRemainingQuantity': 1, 'SetSoldOutAction': 1, 'subscriptioninventory.NormalizeSoldOutAction': 1, 'subscriptioninventory.ValidateConfiguredQuantity': 1}),
        ('ListPlansForSale', {'subscriptioninventory.ListPlansForSale': 1}),
        ('UpdatePlan', {'subscriptioninventory.UpdateAdminPlan': 1}),
        ('validatePlanPatch', {'subscriptioninventory.ValidateSoldOutActionPatch': 1}),
    ),
    'backend/internal/service/payment_config_service.go': _approved_call_deltas(
        ('GetPaymentConfig', {'fmt.Errorf': 1, 'paymentchannels.ParseChannelSettings': 1}),
        ('UpdatePaymentConfig', {'err.Error': 1, 'infraerrors.BadRequest': 1, 'paymentchannels.SerializeChannelSettings': 1, 'setPaymentConfigValue': 26}),
    ),
    'backend/internal/service/payment_fulfillment.go': _approved_call_deltas(
        ('ensurePaymentSubscriptionAssigned', {
            'fmt.Errorf': 1,
            'strconv.FormatInt': 1,
            'subscriptioninventory.ConsumeForFulfillment': 1,
        }),
    ),
    'backend/internal/service/payment_order.go': _approved_call_deltas(
        ('BuildWeChatOAuth', {'loader.service.buildWeChatOAuthRequiredResponse': 1}),
        ('CalculatePayAmount', {'calculateCreateOrderPayAmountForOrderType': 1}),
        ('CreateOrder', {'Prepare': 1, 'buildOrderOAuthResponse': 1, 'customOrderPreparationRequest': 1, 'paymentSelectionFromOrder': 1, 'paymentchannels.NewOrderCoordinator': 1, 'slog.Error': 1, 'subscriptioninventory.TransitionPendingOrderAndRelease': 1}),
        ('HasConfiguredSelection', {'loader.service.configService.HasConfiguredProviderPaymentType': 1}),
        ('LoadMethodCurrency', {'loader.service.configService.ValidateMethodProviderCurrencyConsistency': 1}),
        ('LoadWeChatOAuthAppID', {'loader.service.getWeChatPaymentOAuthCredential': 1}),
        ('RevalidateOrderInstance', {'loader.service.loadBalancer.RevalidateSelection': 1, 'paymentSelectionFromOrder': 1}),
        ('SelectOrderInstance', {'customOrderSelection': 1, 'loader.service.loadBalancer.SelectInstance': 1, 'payment.Strategy': 1, 'payment.WithWxpayJSAPIAppID': 1}),
        ('UsesOfficialWeChatVisibleMethod', {'loader.service.usesOfficialWxpayVisibleMethod': 1}),
        ('ValidatePayAmountCurrency', {'paymentSelectionFromOrder': 1, 'validateSelectedCreateOrderAmountCurrency': 1}),
        ('buildWeChatOAuthRequiredResponse', {'CreateWeChatPaymentOAuthToken': 1, 'fmt.Errorf': 1, 's.paymentResume': 1, 'strconv.FormatFloat': 1}),
        ('buildWeChatPaymentOAuthStartURL', {'q.Set': 2, 'strings.TrimSpace': 2}),
        ('createOrderInTx', {'SetPlanInventoryState': 1, 'subscriptioninventory.ReserveForOrder': 1, 'tx.Client': 1}),
        ('invokeProvider', {'RevalidateBeforeProvider': 1, 'customOrderSelection': 1, 'paymentchannels.NewOrderCoordinator': 1}),
    ),
    'backend/internal/service/payment_order_lifecycle.go': _approved_call_deltas(
        ('cancelCore', {'subscriptioninventory.TransitionPendingOrderAndRelease': 1}),
    ),
    'backend/internal/service/payment_resume_service.go': _approved_call_deltas(
        ('CreateWeChatPaymentOAuthToken', {'paymentchannels.PrepareWeChatOAuthClaims': 1, 's.createSignedToken': 1, 's.ensureSigningKey': 1, 'time.Now': 1}),
        ('CreateWeChatPaymentResumeToken', {'paymentchannels.PrepareWeChatResumeClaims': 1}),
        ('ParseWeChatPaymentOAuthToken', {'err.Error': 1, 'infraerrors.BadRequest': 3, 'paymentchannels.ValidateWeChatOAuthClaims': 1, 's.ensureSigningKey': 1, 's.parseSignedToken': 1, 'validatePaymentResumeExpiry': 1}),
        ('ParseWeChatPaymentResumeToken', {'err.Error': 1, 'paymentchannels.ValidateWeChatResumeClaims': 1}),
        ('RevalidateSelection', {'lb.inner.RevalidateSelection': 1}),
    ),
    'backend/internal/service/payment_refund.go': _approved_call_deltas(
        ('ExecuteRefund', {'s.subscriptionSvc.AdjustSubscriptionForRefund': 1}),
        ('RollbackRefund', {'s.subscriptionSvc.RestoreSubscriptionTermAfterRefund': 1}),
        ('applyRefundFinalDeduction', {'s.subscriptionSvc.FinalizeSubscriptionRefundDeduction': 1}),
    ),
    'backend/internal/service/subscription_service.go': _approved_call_deltas(
        ('AdjustSubscriptionForRefund', {
            'infraerrors.InternalServer': 1,
            'repo.AdjustTerm': 1,
            's.invalidateAdjustedSubscriptionCache': 1,
            's.now': 1,
        }),
        ('AdminResetQuota', {
            'repo.ResetQuota': 1,
            's.invalidateSubscriptionCaches': 1,
            's.now': 1,
        }),
        ('AdminResetQuotaIdempotent', {
            'infraerrors.InternalServer': 1,
            'repo.ResetQuota': 1,
            's.invalidateSubscriptionCaches': 1,
            's.now': 1,
        }),
        ('EnsureWindowMaintenance', {'repo.EnsureWindowMaintenance': 1, 's.now': 1}),
        ('ExtendSubscription', {
            'repo.AdjustTerm': 1,
            's.invalidateAdjustedSubscriptionCache': 1,
            's.now': 1,
        }),
        ('FinalizeSubscriptionRefundDeduction', {
            'errors.Is': 1,
            'infraerrors.InternalServer': 1,
            'repo.AdjustTerm': 1,
            's.invalidateAdjustedSubscriptionCache': 1,
            's.now': 1,
        }),
        ('RestoreSubscriptionTermAfterRefund', {
            'infraerrors.InternalServer': 1,
            'repo.RestoreTermSnapshotExact': 1,
            's.invalidateAdjustedSubscriptionCache': 1,
        }),
        ('ValidateAndCheckLimits', {'subscriptionquota.NeedsAdvance': 1}),
        ('invalidateAdjustedSubscriptionCache', {
            'cancel': 1,
            'context.Background': 1,
            'context.WithTimeout': 1,
            's.InvalidateSubCache': 1,
            's.billingCacheService.InvalidateSubscription': 1,
        }),
        ('normalizeExpiredWindowsAt', {'subscriptionquota.NeedsAdvance': 1}),
        ('updateExistingSubscriptionTerm', {
            'repo.RenewExistingTerm': 1,
            's.now': 1,
            'time.Now': 1,
        }),
    ),
    'frontend/src/components/admin/monitor/MonitorFormDialog.vue': _approved_call_deltas(
        ('<top-level>', {'createEmptyMonitorGroupRateFormState': 1, 'ref': 1, 't': 1}),
        ('buildPayload', {'buildMonitorGroupRateCreateFields': 1}),
        ('handleSubmit', {'appStore.showError': 1, 'buildMonitorGroupRateUpdateFields': 1, 't': 1, 'validateMonitorGroupRateForm': 1}),
        ('loadFromMonitor', {'monitorGroupRateFormStateFromSource': 1}),
        ('resetForm', {'createEmptyMonitorGroupRateFormState': 1}),
    ),
    'frontend/src/components/payment/AmountInput.vue': _approved_call_deltas(
        ('<top-level>', {'RegExp': 1, 'computed': 3, 'currencySymbol': 1, 'normalizePaymentCurrency': 1, 'paymentCurrencyFractionDigits': 1}),
        ('handleInput', {'amountPattern.value.test': 1}),
    ),
    'frontend/src/components/payment/PaymentProviderDialog.vue': _approved_call_deltas(
        ('<top-level>', {'t': 1}),
    ),
    'frontend/src/components/payment/SubscriptionPlanCard.vue': _approved_call_deltas(
        ('<template:@click>', {'handleSelect': 1}),
        ('<top-level>', {'computed': 1, 'isPlanSoldOut': 1, 't': 2}),
        ('handleSelect', {'emit': 1}),
    ),
    'frontend/src/components/payment/paymentFlow.ts': _approved_call_deltas(
        ('buildCreateOrderPayload', {'input.providerKey.trim': 1, 'toLowerCase': 1, 'trim': 1}),
        ('decidePaymentLaunch', {'toLowerCase': 1, 'trim': 1}),
    ),
    'frontend/src/components/user/monitor/MonitorCard.vue': _approved_call_deltas(
        ('<top-level>', {'statusLabel': 1}),
    ),
    'frontend/src/views/admin/GroupsView.vue': _approved_call_deltas(
        ('<top-level>', {'minimumBalanceFormValue': 2, 'ref': 2}),
        ('closeCreateModal', {'minimumBalanceFormValue': 1}),
        ('closeEditModal', {'minimumBalanceFormValue': 1}),
        ('handleCreateGroup', {'appStore.showError': 1, 'normalizeMinimumBalanceFormValue': 1, 't': 1}),
        ('handleEdit', {'minimumBalanceFormValue': 1}),
        ('handleUpdateGroup', {'appStore.showError': 1, 'normalizeMinimumBalanceFormValue': 1, 't': 1}),
    ),
    'frontend/src/views/admin/orders/AdminPaymentPlansView.vue': _approved_call_deltas(
        ('<top-level>', {'canListSoldOutPlan': 3, 'isPlanSoldOut': 3, 't': 3}),
        ('toggleForSale', {'appStore.showError': 1, 'canListSoldOutPlan': 1, 'isPlanSoldOut': 1, 't': 1}),
    ),
    'frontend/src/views/admin/orders/PlanEditDialog.vue': _approved_call_deltas(
        ('<template:@click>', {'togglePlanForSale': 1}),
        ('<template:@update:model-value>', {'handleRemainingQuantityInput': 1}),
        ('<top-level>', {'String': 1, 'computed': 1, 'ref': 3, 'remainingQuantityInput.value.trim': 1}),
        ('buildPlanPayload', {'inventoryQuantityValue': 2}),
        ('handleSavePlan', {'appStore.showError': 1}),
        ('remainingQuantityError', {'computed': 1, 'isInventoryQuantity': 1, 'remainingQuantityInput.value.trim': 1, 't': 1}),
        ('togglePlanForSale', {'appStore.showError': 1, 'remainingQuantityInput.value.trim': 1, 't': 1}),
    ),
    'frontend/src/views/admin/SettingsView.vue': _approved_call_deltas(
        ('<top-level>', {'Number': 1}),
        ('saveSettings', {'appStore.showError': 1, 'localText': 1, 'paymentChannelSettingsRef.value.validate': 1}),
    ),
    'frontend/src/views/admin/SubscriptionsView.vue': _approved_call_deltas(
        ('<template:@cancel>', {'cancelResetQuota': 1}),
        ('<template:@click>', {'showBulkResetDialog = true': 1}),
        ('<template:@close>', {'showBulkResetDialog = false': 1}),
        ('<template:@completed>', {'loadSubscriptions': 1}),
        ('<top-level>', {'ref': 1, 't': 1}),
        ('handleResetQuota', {'Date.now': 1, 'Math.random': 1, 'randomUUID': 1}),
    ),
    'frontend/src/views/user/KeysView.vue': _approved_call_deltas(
        ('<top-level>', {'balanceRequirementForGroup': 2, 'balanceRequirementsByGroupID.value.get': 1, 'computed': 1, 'customApiKeyBulkText': 1, 'groupBalanceRequirement': 1, 'groupBalanceRequirementsByID': 1, 'ref': 1}),
        ('<template:@busy-change>', {'bulkActionBusy = $event': 1}),
        ('<template:@click>', {'refreshApiKeys': 1}),
        ('<template:@completed>', {'handleBulkCompleted': 1}),
        ('<template:@update:selected-ids>', {'selectedKeyIds = $event': 1}),
        ('<template:@update:selected-keys>', {'handleTableSelectionChange': 1}),
        ('changeGroup', {'balanceRequirementForGroup': 1, 'minimumBalanceErrorToast': 1}),
        ('handleBulkCompleted', {'loadApiKeys': 1}),
        ('handleSubmit', {'minimumBalanceErrorToast': 1}),
        ('handleTableSelectionChange', {'Set': 1, 'apiKeys.value.map': 1, 'keys.filter': 1, 'visibleKeyIds.has': 1}),
        ('loadApiKeys', {'Set': 1, 'response.items.map': 1, 'selectedKeyIds.value.filter': 1, 'visibleKeyIds.has': 1}),
        ('refreshApiKeys', {'loadApiKeys': 1}),
    ),
    'frontend/src/views/user/PaymentView.vue': _approved_call_deltas(
        ('<top-level>', {
            'appStore.showWarning': 2,
            'appendBackupChannelHint': 1,
            'checkout.value.plans.find': 1,
            'createOrder': 1,
            'extractI18nErrorMessage': 1,
            'findPaymentChannel': 3,
            'groupPlans.filter': 1,
            'isPlanSoldOut': 3,
            'paymentAPI.getCheckoutInfo': 1,
            'paymentChannelSupports': 2,
            'paymentStore.createOrder': 1,
            'planAvailabilityError': 1,
            'router.replace': 1,
            'router.resolve': 1,
            'synchronizePlanAvailability': 1,
            't': 2,
            'usePaymentChannelPricing': 1,
            'usePaymentChannelRecovery': 1,
        }),
        ('<template:@select>', {'selectedChannelId = $event': 2}),
        ('applyScenarioError', {'appendBackupChannelHint': 1}),
        ('confirmSubscribe', {'appStore.showWarning': 1, 'isPlanSoldOut': 1, 't': 1}),
        ('selectPlan', {'appStore.showWarning': 1, 'isPlanSoldOut': 1, 't': 1}),
        ('selectPlanFromModal', {'appStore.showWarning': 1, 'isPlanSoldOut': 1, 't': 1}),
    ),
}

# Official upgrades can absorb a previously approved bridge helper into the
# Vendor implementation. Keep that structural change bound to the exact
# reviewed Vendor Commit instead of weakening the default call surface.
BASELINE_DELEGATE_VIEW_CALL_DELTAS: dict[
    tuple[str, str], tuple[tuple[str, str], ...]
] = {
    (
        "c043c24774228ba891ddf90d783aa6dc7d0855b5",
        "backend/internal/service/payment_config_service.go",
    ): _approved_call_deltas(
        ("GetPaymentConfig", {
            "fmt.Errorf": 1,
            "paymentchannels.ParseChannelSettings": 1,
        }),
        ("UpdatePaymentConfig", {
            "err.Error": 1,
            "infraerrors.BadRequest": 1,
            "paymentchannels.SerializeChannelSettings": 1,
        }),
    ),
    (
        "f0e7a9c7a23a7d02fb159b62fa809621eb0475a6",
        "backend/internal/service/payment_config_service.go",
    ): _approved_call_deltas(
        ("GetPaymentConfig", {
            "fmt.Errorf": 1,
            "paymentchannels.ParseChannelSettings": 1,
        }),
        ("UpdatePaymentConfig", {
            "err.Error": 1,
            "infraerrors.BadRequest": 1,
            "paymentchannels.SerializeChannelSettings": 1,
        }),
    ),
    (
        "155c494964c3ea6ecc31f52679525c1034bf0f16",
        "backend/internal/service/payment_config_service.go",
    ): _approved_call_deltas(
        ("GetPaymentConfig", {
            "fmt.Errorf": 1,
            "paymentchannels.ParseChannelSettings": 1,
        }),
        ("UpdatePaymentConfig", {
            "err.Error": 1,
            "infraerrors.BadRequest": 1,
            "paymentchannels.SerializeChannelSettings": 1,
        }),
    ),
}

# Control-flow additions in delegate/view bridges use an exact structural
# allowlist. Keeping the owning function and complete trimmed statement makes
# renames, additional branches, and moved orchestration fail even when the TSV
# line budget and shadow mapping are updated.
APPROVED_DELEGATE_VIEW_CONTROL: dict[str, tuple[tuple[str, str], ...]] = {
    "backend/internal/handler/admin/subscription_handler.go": (
        ("BulkResetQuota", "for _, subscriptionID := range req.SubscriptionIDs {"),
        ("BulkResetQuota", "if err := c.ShouldBindJSON(&req); err != nil {"),
        ("BulkResetQuota", "if len(req.SubscriptionIDs) > subscriptionbulkreset.MaxBatchSize {"),
        ("BulkResetQuota", "if !ok {"),
        ("BulkResetQuota", "if err != nil {"),
        ("BulkResetQuota", "if err != nil {"),
        ("BulkResetQuota", 'if idempotencyKey == "" {'),
        ("BulkResetQuota", "if subscriptionID <= 0 {"),
        ("ResetQuota", 'if idempotencyKey == "" {'),
        ("ResetQuota", "if !ok {"),
        ("ResetQuota", "if err != nil {"),
        ("ResetQuota", "if err != nil {"),
        ("ResetQuota", "if err != nil {"),
        ("ResetQuota", "if execErr != nil {"),
        ("ResetQuota", "if resetter == nil {"),
        ("ListBulkResetQuotaCandidates", "if err != nil {"),
    ),
    "backend/internal/handler/admin/setting_handler.go": (
        ("GetSettings", "if err != nil {"),
    ),
    "backend/internal/service/idempotency.go": (
        ("Execute", "if err != nil {"),
    ),
    "backend/internal/handler/admin/setting_handler_update.go": (
        ("UpdateSettings", "if err != nil {"),
    ),
    "backend/internal/handler/admin/system_handler.go": (
        ("PerformUpdate", "if errors.Is(err, customupdater.ErrNoUpdateAvailable) {"),
        ("Rollback", "if err := c.ShouldBindJSON(&req); err != nil {"),
        ("Rollback", 'if targetVersion == "" {'),
    ),
    "backend/internal/handler/api_key_handler.go": (
        ("GetAvailableGroups", "for i := range options {"),
        ("GetAvailableGroups", "if requirement := options[i].BalanceRequirement; requirement != nil {"),
    ),
    "backend/internal/handler/auth_wechat_oauth.go": (
        ("WeChatPaymentOAuthStart", 'if contextToken := strings.TrimSpace(c.Query("payment_context_token")); contextToken != "" {'),
        ("WeChatPaymentOAuthStart", "if parseErr != nil {"),
        ("WeChatPaymentOAuthStart", 'if paymentContext.PaymentType == "" {'),
        ("WeChatPaymentOAuthStart", 'if paymentContext.ProviderKey != "" && paymentContext.ProviderKey != payment.TypeWxpay {'),
        ("WeChatPaymentOAuthCallback", 'if paymentContext.ProviderKey != "" && paymentContext.ProviderKey != payment.TypeWxpay {'),
    ),
    "backend/internal/handler/channel_monitor_user_handler.go": (
        ("SetGroupRateResolver", "if h == nil {"),
        ("List", "if h.groupRateResolver != nil {"),
        ("List", "if rate, ok := groupRates[v.ID]; ok {"),
    ),
    "backend/internal/handler/gateway_handler.go": (
        ("billingErrorDetails", "if pkgerrors.Reason(err) == groupaccess.MinimumBalanceNotMetReason {"),
    ),
    "backend/internal/handler/openai_gateway_handler.go": (
        ("rejectIfCyberSessionBlocked", "if h.contentModerationService == nil ||"),
        ("recordCyberPolicyIfMarked", 'if cyberPolicyInScope && gwSvc != nil && cyberBlockKey != "" {'),
    ),
    "backend/internal/handler/payment_handler.go": (
        ("GetCheckoutInfo", "if err != nil {"),
        ("applyWeChatPaymentResumeClaims", 'if providerKey != "" {'),
        ("applyWeChatPaymentResumeClaims", 'if req.ProviderKey != "" && !strings.EqualFold(strings.TrimSpace(req.ProviderKey), providerKey) {'),
        ("applyWeChatPaymentResumeClaims", "if !paymentchannels.IsValidSelection(paymentType, providerKey) {"),
        ("applyWeChatPaymentResumeClaims", "if claims.FeeRate != nil {"),
        ("applyWeChatPaymentResumeClaims", "if math.IsNaN(*claims.FeeRate) || math.IsInf(*claims.FeeRate, 0) || *claims.FeeRate < 0 || *claims.FeeRate > 100 {"),
    ),
    "backend/internal/payment/load_balancer.go": (
        ("SelectInstance", "if result.UsageLoadError != nil {"),
        ("SelectInstance", "for _, rejection := range result.LimitRejections {"),
        ("instanceCoordinator", "if lb.coordinator == nil {"),
        ("paymentSelectionFromCustom", "if selection == nil {"),
        ("customSelectionFromPayment", "if selection == nil {"),
        ("LoadEnabledInstances", 'if providerKey != "" {'),
        ("LoadEnabledInstances", "if err != nil {"),
        ("LoadEnabledInstances", "for _, instance := range instances {"),
        ("LoadEnabledInstances", "if err != nil {"),
        ("LoadInstance", "if err != nil {"),
        ("LoadInstance", "if dbent.IsNotFound(err) {"),
        ("LoadDailyUsage", "if err := lb.db.PaymentOrder.Query()."),
        ("LoadDailyUsage", "for _, item := range rows {"),
        ("paymentInstanceRecord", "if instance == nil {"),
        ("paymentInstanceRecord", "if err != nil {"),
    ),
    "backend/internal/service/admin_group.go": (
        ("AdminUpdateAPIKeyGroupID", "if (*groupID == 0 && apiKey.GroupID == nil) ||"),
        ("AdminUpdateAPIKeyGroupID", "if err := s.apiKeyRepo.Update(ctx, apiKey, APIKeyUpdateFields{GroupID: true}); err != nil {"),
        ("AdminUpdateAPIKeyGroupID", "if s.authCacheInvalidator != nil {"),
        ("AdminUpdateAPIKeyGroupID", "if err := s.checkGroupMinimumBalanceForUser(ctx, apiKey.UserID, group); err != nil {"),
        ("CreateGroup", "if math.IsNaN(input.MinimumBalance) || math.IsInf(input.MinimumBalance, 0) || input.MinimumBalance < 0 {"),
        ("ReplaceUserGroup", "if migrated > 0 {"),
        ("ReplaceUserGroup", "if err := s.checkGroupMinimumBalanceForUser(opCtx, userID, newGroup); err != nil {"),
        ("ReplaceUserGroup", "if err := s.userRepo.AddGroupToAllowedGroups(opCtx, userID, newGroupID); err != nil {"),
        ("UpdateGroup", "if input.MinimumBalance != nil {"),
        ("UpdateGroup", "if math.IsNaN(*input.MinimumBalance) || math.IsInf(*input.MinimumBalance, 0) || *input.MinimumBalance < 0 {"),
        ("checkGroupMinimumBalanceForUser", "if group == nil || group.MinimumBalance <= 0 {"),
        ("checkGroupMinimumBalanceForUser", "if s.userRepo == nil {"),
        ("checkGroupMinimumBalanceForUser", "if err != nil {"),
    ),
    "backend/internal/service/api_key_service.go": (
        ("Create", "if err := groupaccess.CheckMinimumBalance(group.ID, group.Name, user.Balance, group.MinimumBalance); err != nil {"),
        ("Update", "if groupChanged {"),
        ("Update", "if err != nil {"),
        ("Update", "if err != nil {"),
        ("Update", "if !s.canUserBindGroup(ctx, user, group) {"),
        ("Update", "if err := groupaccess.CheckMinimumBalance(group.ID, group.Name, user.Balance, group.MinimumBalance); err != nil {"),
        ("GetAvailableGroupOptions", "if err != nil {"),
        ("GetAvailableGroupOptions", "for i := range groups {"),
        ("GetAvailableGroupOptions", "if groups[i].MinimumBalance > 0 {"),
    ),
    "backend/internal/service/batch_image_public.go": (
        ("Submit", "if err != nil {"),
        ("Submit", "if group != nil && group.MinimumBalance > 0 {"),
        ("Submit", "if s.UserRepo == nil {"),
        ("Submit", "if err != nil {"),
        ("Submit", "if err := groupaccess.CheckMinimumBalance(group.ID, group.Name, user.Balance, group.MinimumBalance); err != nil {"),
        ("ListModels", "if _, err := s.ensureGroupAllowsBatchImage(ctx, owner.GroupID); err != nil {"),
    ),
    "backend/internal/service/billing_cache_service.go": (
        ("LoadMinimumBalanceGroup", "if err != nil || group == nil {"),
        ("CheckBillingEligibility", "if err != nil {"),
        ("CheckBillingEligibility", "if !groupMinimumBalanceEnabled && s.circuitBreaker != nil && !s.circuitBreaker.Allow() {"),
        ("checkCustomMinimumBalanceEligibility", "if loader, ok := s.apiKeyRateLimitLoader.(groupMinimumBalanceLoader); ok {"),
        ("checkCustomMinimumBalanceEligibility", "if s.circuitBreaker != nil {"),
        ("checkCustomMinimumBalanceEligibility", "if user != nil {"),
        ("checkCustomMinimumBalanceEligibility", 'if forcePlatform, ok := ctx.Value(ctxkey.ForcePlatform).(string); ok && forcePlatform != "" {'),
        ("checkCustomMinimumBalanceEligibility", "if err == nil {"),
        ("checkCustomMinimumBalanceEligibility", "if errors.Is(err, groupaccess.ErrCircuitOpen) {"),
        ("checkCustomMinimumBalanceEligibility", "if errors.As(err, &dependencyErr) {"),
        ("checkCustomMinimumBalanceEligibility", "if dependencyErr.Kind == groupaccess.DependencyBalanceLoad && user != nil {"),
        ("minimumBalanceGroupSnapshot", "if group == nil {"),
    ),
    "backend/internal/service/channel_monitor_service.go": (
        ("cloneFloat64Pointer", "if value == nil {"),
        ("validateGroupRateOverride", "if !channelmonitorratedisplay.ValidOverride(value) {"),
        ("validateGroupRateDisplayTemplate", "if _, ok := channelmonitorratedisplay.NormalizeTemplate(value); !ok {"),
        ("validateCreateParams", "if err := validateGroupRateOverride(p.GroupRateOverride); err != nil {"),
        ("validateCreateParams", "if err := validateGroupRateDisplayTemplate(p.GroupRateDisplayTemplate); err != nil {"),
        ("applyMonitorUpdate", "if p.ClearGroupRateOverride {"),
        ("applyMonitorUpdate", "} else if p.GroupRateOverride != nil {"),
        ("applyMonitorUpdate", "if err := validateGroupRateOverride(p.GroupRateOverride); err != nil {"),
        ("applyMonitorUpdate", "if p.GroupRateDisplayTemplate != nil {"),
        ("applyMonitorUpdate", "if err := validateGroupRateDisplayTemplate(*p.GroupRateDisplayTemplate); err != nil {"),
    ),
    "backend/internal/service/content_moderation.go": (
        ("Check", "if !inAPIAuditScope {"),
        ("Check", "if cfg.Mode == ContentModerationModePreBlock {"),
        ("RecordCyberPolicyEvent", "if err := s.sendCyberPolicyEmail(ctx, cfg, log); err != nil {"),
        ("UpdateConfig", "if input.APIAuditScope != nil {"),
        ("UpdateConfig", "if input.UserBanThresholds != nil {"),
        ("applyFlaggedAccountSideEffects", "if n, err := s.countFlaggedByUserSince(ctx, *log.UserID, since, cfg.CyberPolicyExcludeFromBanCount); err == nil {"),
        ("RecordCyberPolicyEvent", "if s.tryRecordCustomCyberPolicyEvent(ctx, in) {"),
        ("validateConfig", "for _, groupID := range cfg.APIAuditScope.GroupIDs {"),
        ("validateConfig", "if !cfg.APIAuditScope.AllInScope && s.groupRepo != nil {"),
        ("validateConfig", "if _, err := s.groupRepo.GetByIDLite(ctx, groupID); err != nil {"),
        ("validateConfig", "if err := validateContentModerationAPIAuditScope(cfg, requireAPIAuditScope); err != nil {"),
        ("validateConfig", "if err := validateContentModerationUserBanThresholdOverrides(cfg.UserBanThresholds); err != nil {"),
        ("worker", "if latestSnapshot, latestErr := s.loadRuntimeSnapshot(ctx); latestErr == nil && latestSnapshot != nil && latestSnapshot.config != nil {"),
        ("worker", "if !cfg.includesAPIAuditGroup(task.input.GroupID) {"),
    ),
    "backend/internal/service/content_moderation_email.go": (
        ("buildCyberPolicyNoticeEmailBody", "if cfg != nil && cfg.BanThreshold > 0 {"),
    ),
    "backend/internal/service/payment_config_limits.go": (
        ("GetAvailableMethodOptions", "if err != nil {"),
        ("ValidateMethodProviderCurrencyConsistency", 'if providerKey == "" {'),
        ("ValidateMethodProviderCurrencyConsistency", 'if method == "" || s == nil || s.entClient == nil {'),
        ("ValidateMethodProviderCurrencyConsistency", "if err != nil {"),
        ("ValidateMethodProviderCurrencyConsistency", "if conflict, ok := err.(*paymentchannels.CurrencyConflictError); ok {"),
        ("HasConfiguredProviderPaymentType", 'if method == "" || providerKey == "" || s == nil || s.entClient == nil {'),
        ("HasConfiguredProviderPaymentType", "if err != nil {"),
        ("pcPaymentProviderRecords", "for _, instance := range instances {"),
        ("pcPaymentProviderRecords", "if instance == nil {"),
        ("pcPaymentProviderRecords", "if decrypted, err := s.decryptConfig(instance.Config); err == nil && decrypted != nil {"),
    ),
    "backend/internal/service/payment_config_plans.go": (
        ("CreatePlan", "if err != nil {"),
        ("CreatePlan", "if err := subscriptioninventory.ValidateConfiguredQuantity(req.RemainingQuantity, soldOutAction); err != nil {"),
        ("validatePlanPatch", "if req.RemainingQuantity.Present {"),
        ("validatePlanPatch", "if err := subscriptioninventory.ValidateSoldOutActionPatch(req.SoldOutAction); err != nil {"),
    ),
    "backend/internal/service/payment_fulfillment.go": (
        ("ensurePaymentSubscriptionAssigned", "if err := subscriptioninventory.ConsumeForFulfillment(txCtx, txClient, o.ID); err != nil {"),
    ),
    "backend/internal/service/payment_refund.go": (
        ("applyRefundFinalDeduction", "if err := s.subscriptionSvc.FinalizeSubscriptionRefundDeduction(ctx, p.SubscriptionID, -p.SubDaysToDeduct); err != nil {"),
        ("RollbackRefund", "if err != nil {"),
        ("RollbackRefund", "if p.SubscriptionTermSnapshot != nil {"),
    ),
    "backend/internal/service/subscription_service.go": (
        ("AdjustSubscriptionForRefund", "if !ok {"),
        ("AdminResetQuota", "if repo, ok := s.userSubRepo.(UserSubscriptionCustomRepository); ok {"),
        ("AdminResetQuota", "if err != nil {"),
        ("AdminResetQuotaIdempotent", "if !ok {"),
        ("AdminResetQuotaIdempotent", "if err != nil {"),
        ("EnsureWindowMaintenance", "if repo, ok := s.userSubRepo.(UserSubscriptionCustomRepository); ok {"),
        ("EnsureWindowMaintenance", "if err != nil {"),
        ("ExtendSubscription", "if repo, ok := s.userSubRepo.(UserSubscriptionCustomRepository); ok {"),
        ("ExtendSubscription", "if err != nil {"),
        ("FinalizeSubscriptionRefundDeduction", "if !ok {"),
        ("FinalizeSubscriptionRefundDeduction", "if errors.Is(err, ErrAdjustWouldExpire) {"),
        ("RestoreSubscriptionTermAfterRefund", "if !ok {"),
        ("RestoreSubscriptionTermAfterRefund", "if err != nil {"),
        ("assignOrExtendSubscription", "if err := s.updateExistingSubscriptionTerm(ctx, existingSub.ID, validityDays, input.Notes, input.CycleSourceType, input.CycleSourceRef, false); err != nil {"),
        ("assignSubscriptionWithReuse", "if err := s.updateExistingSubscriptionTerm(ctx, sub.ID, validityDays, input.Notes, input.CycleSourceType, input.CycleSourceRef, true); err != nil {"),
        ("invalidateAdjustedSubscriptionCache", "if sub == nil {"),
        ("invalidateAdjustedSubscriptionCache", "if s.billingCacheService != nil {"),
        ("normalizeExpiredWindowsAt", "if subscriptionquota.NeedsAdvance(sub.CurrentCycleEndsAt, sub.ExpiresAt, now) {"),
        ("updateExistingSubscriptionTerm", "if repo, ok := s.userSubRepo.(UserSubscriptionCustomRepository); ok {"),
        ("updateExistingSubscriptionTerm", "if s.now != nil {"),
        ("ValidateAndCheckLimits", "if subscriptionquota.NeedsAdvance(sub.CurrentCycleEndsAt, sub.ExpiresAt, now) {"),
    ),
    "backend/internal/service/payment_config_service.go": (
        ("GetPaymentConfig", "if err != nil {"),
        ("UpdatePaymentConfig", "if req.ChannelSettings != nil {"),
        ("UpdatePaymentConfig", "if err != nil {"),
        ("setPaymentConfigValue", "if provided {"),
    ),
    "backend/internal/service/payment_order.go": (
        ("CreateOrder", "if _, cleanupErr := subscriptioninventory.TransitionPendingOrderAndRelease(ctx, s.entClient, order.ID, OrderStatusFailed); cleanupErr != nil {"),
        ("CreateOrder", "if preparation.OAuth != nil {"),
        ("HasConfiguredSelection", "if loader.service == nil || loader.service.configService == nil {"),
        ("LoadMethodCurrency", "if loader.service == nil || loader.service.configService == nil {"),
        ("SelectOrderInstance", 'if request.WeChatJSAPIAppID != "" {'),
        ("customOrderSelection", "if selection == nil {"),
        ("paymentSelectionFromOrder", "if selection == nil {"),
        ("invokeProvider", "if err := paymentchannels.NewOrderCoordinator(paymentOrderLoader{service: s}).RevalidateBeforeProvider("),
        ("buildWeChatOAuthRequiredResponse", "if err != nil {"),
        ("buildWeChatPaymentOAuthStartURL", 'if paymentContextToken = strings.TrimSpace(paymentContextToken); paymentContextToken != "" {'),
        ("buildWeChatPaymentOAuthStartURL", 'if providerKey := strings.TrimSpace(req.ProviderKey); providerKey != "" {'),
        ("createOrderInTx", "if err != nil {"),
        ("createOrderInTx", "if plan != nil {"),
    ),
    "backend/internal/service/payment_resume_service.go": (
        ("CreateWeChatPaymentOAuthToken", "if err != nil {"),
        ("ParseWeChatPaymentOAuthToken", "if err := s.ensureSigningKey(); err != nil {"),
        ("ParseWeChatPaymentOAuthToken", "if err := s.parseSignedToken(token, &claims); err != nil {"),
        ("ParseWeChatPaymentOAuthToken", "if claims.TokenType != paymentchannels.WeChatPaymentOAuthTokenType {"),
        ("ParseWeChatPaymentOAuthToken", 'if err := validatePaymentResumeExpiry(claims.ExpiresAt, "INVALID_WECHAT_PAYMENT_OAUTH_TOKEN", "wechat payment oauth token has expired"); err != nil {'),
        ("ParseWeChatPaymentOAuthToken", "if err != nil {"),
        ("CreateWeChatPaymentResumeToken", "if err := s.ensureSigningKey(); err != nil {"),
        ("CreateWeChatPaymentResumeToken", "if err != nil {"),
        ("ParseWeChatPaymentResumeToken", "if claims.TokenType != paymentchannels.WeChatPaymentResumeTokenType {"),
        ("ParseWeChatPaymentResumeToken", "if err != nil {"),
    ),
    "frontend/src/components/admin/monitor/MonitorFormDialog.vue": (
        ("handleSubmit", "if (groupRateError) {"),
    ),
    "frontend/src/components/payment/AmountInput.vue": (
        ("handleInput", "if (!amountPattern.value.test(val)) return"),
    ),
    "frontend/src/components/payment/paymentFlow.ts": (
        ("buildCreateOrderPayload", "if (input.providerKey?.trim()) {"),
    ),
    "frontend/src/components/payment/SubscriptionPlanCard.vue": (
        ("<top-level>", '<span v-if="soldOut" class="mt-1 inline-flex rounded-full bg-red-100 px-2 py-0.5 text-[11px] font-semibold text-red-700 dark:bg-red-900/30 dark:text-red-300">'),
        ("handleSelect", "if (soldOut.value) return"),
    ),
    "frontend/src/components/user/monitor/MonitorCard.vue": (
        ("<top-level>", "v-if=\"typeof item.group_rate_multiplier === 'number'\""),
    ),
    "frontend/src/views/admin/GroupsView.vue": (
        ("handleCreateGroup", "if (minimumBalance === null) {"),
        ("handleUpdateGroup", "if (minimumBalance === null) {"),
    ),
    "frontend/src/views/admin/orders/AdminPaymentPlansView.vue": (
        ("toggleForSale", "if (!plan.for_sale && isPlanSoldOut(plan) && !canListSoldOutPlan(plan)) {"),
    ),
    "frontend/src/views/admin/orders/PlanEditDialog.vue": (
        ("buildPlanPayload", "if (!props.plan) {"),
        ("buildPlanPayload", "if (forSaleDirty.value) payload.for_sale = planForm.for_sale"),
        ("buildPlanPayload", "if (remainingQuantityDirty.value) payload.remaining_quantity = inventoryQuantityValue(remainingQuantityInput.value)"),
        ("buildPlanPayload", "if (planForm.sold_out_action !== initialSoldOutAction.value) payload.sold_out_action = planForm.sold_out_action"),
        ("handleSavePlan", "if (remainingQuantityError.value) {"),
        ("remainingQuantityError", "if (value === '' || isInventoryQuantity(value, allowZero) || isUnchangedSoldOutQuantity.value) return ''"),
        ("togglePlanForSale", "if (!planForm.for_sale"),
    ),
    "frontend/src/views/admin/SettingsView.vue": (
        ("<top-level>", 'v-if="form.payment_enabled"'),
        ("saveSettings", "if ("),
    ),
    "frontend/src/views/user/KeysView.vue": (
        ("<top-level>", 'v-if="row.group && balanceRequirementForGroup(row.group.id)"'),
        ("<top-level>", '<span v-if="option" class="flex min-w-0 items-center">'),
        ("<top-level>", 'v-if="(option as unknown as GroupOption).balanceRequirement"'),
        ("<top-level>", 'v-if="option.balanceRequirement"'),
        ("onFilterChange", "if (bulkActionBusy.value) return"),
        ("loadApiKeys", "if (!options.preserveSelection) {"),
        ("refreshApiKeys", "if (bulkActionBusy.value) return"),
        ("handleTableSelectionChange", "if (bulkActionBusy.value) return"),
        ("handleBulkCompleted", "if ("),
        ("handlePageChange", "if (bulkActionBusy.value) return"),
        ("handlePageSizeChange", "if (bulkActionBusy.value) return"),
        ("handleSort", "if (bulkActionBusy.value) return"),
        ("changeGroup", "if (newGroupId !== null && balanceRequirementForGroup(newGroupId)) return"),
        ("changeGroup", "} catch (error: unknown) {"),
    ),
    "frontend/src/views/user/PaymentView.vue": (
        ("<top-level>", '<div v-if="enabledChannelIds.length === 0" class="card py-16 text-center">'),
        ("<top-level>", '<div v-if="enabledChannelIds.length >= 1" class="card p-6">'),
        ("<top-level>", '<div v-if="enabledChannelIds.length >= 1" class="card p-6">'),
        ("<top-level>", "if (enabledChannelIds.value.length) {"),
        ("<top-level>", "if (restoredChannel) {"),
        ("<top-level>", "if (!plan || isPlanSoldOut(plan)) {"),
        ("<top-level>", "if (orderType === 'subscription' && !options.isResume) {"),
        ("<top-level>", "if (purchasablePlans.length === 1) {"),
        ("<top-level>", "} else if (groupPlans.length > 0) {"),
        ("<top-level>", "if (availabilityError) {"),
        ("<top-level>", "} else if (apiErr.reason === 'TOO_MANY_PENDING') {"),
        ("<top-level>", "if (selectedPlan.value?.id === planId) selectedPlan.value = null"),
        ("confirmSubscribe", "if (isPlanSoldOut(selectedPlan.value)) {"),
        ("selectPlan", "if (isPlanSoldOut(plan)) {"),
        ("selectPlanFromModal", "if (isPlanSoldOut(plan)) {"),
    ),
}

FUNCTION_START_PATTERNS = (
    re.compile(
        r"^\s*func\s+(?:\([^)]*\)\s*)?(?P<name>[A-Za-z_]\w*)\s*\(",
        re.MULTILINE,
    ),
    re.compile(
        r"^\s*(?:export\s+)?(?:async\s+)?function\s+"
        r"(?P<name>[A-Za-z_$][\w$]*)\s*\(",
        re.MULTILINE,
    ),
    re.compile(
        r"^\s*(?:export\s+)?(?:const|let)\s+(?P<name>[A-Za-z_$][\w$]*)"
        r"(?:\s*:[^=\n]+)?\s*=\s*(?:async\s+)?(?:\([^\n]*?\)|[A-Za-z_$][\w$]*)"
        r"\s*=>\s*\{",
        re.MULTILINE,
    ),
    re.compile(
        r"^\s*(?:export\s+)?(?:const|let)\s+(?P<name>[A-Za-z_$][\w$]*)"
        r"(?:\s*:[^=\n]+)?\s*=\s*\w+\s*\(\s*(?:async\s+)?"
        r"(?:\([^\n]*?\)|[A-Za-z_$][\w$]*)\s*=>\s*\{",
        re.MULTILINE,
    ),
)


class ContractError(RuntimeError):
    pass


@dataclass(frozen=True)
class ContractRow:
    path: str
    kind: str
    shadow_required: bool
    additions: int
    deletions: int
    budget_overrides: tuple[tuple[str, int, int], ...]


@dataclass(frozen=True)
class FunctionBlock:
    name: str
    start_line: int
    end_line: int


def run_git(repo: Path, *args: str, text: bool = True) -> str | bytes:
    run_options: dict[str, object] = {
        "check": False,
        "capture_output": True,
        "text": text,
    }
    if text:
        run_options.update(encoding="utf-8", errors="replace")
    result = subprocess.run(["git", "-C", str(repo), *args], **run_options)
    if result.returncode != 0:
        stderr = result.stderr if text else result.stderr.decode("utf-8", "replace")
        raise ContractError(f"git {' '.join(args)} failed: {stderr.strip()}")
    return result.stdout


def load_tsv(path: Path, expected_header: list[str]) -> list[dict[str, str]]:
    try:
        with path.open("r", encoding="utf-8-sig", newline="") as handle:
            reader = csv.DictReader(handle, delimiter="\t")
            if reader.fieldnames != expected_header:
                raise ContractError(f"invalid TSV header: {path}")
            rows = []
            for line_number, row in enumerate(reader, 2):
                normalized = {key: (value or "").rstrip("\r") for key, value in row.items()}
                if any(not normalized[key] for key in expected_header):
                    raise ContractError(f"empty field in {path}:{line_number}")
                rows.append(normalized)
            return rows
    except OSError as error:
        raise ContractError(f"cannot read {path}: {error}") from error


def load_contract(path: Path) -> list[ContractRow]:
    rows = load_tsv(path, CONTRACT_HEADER)
    result: list[ContractRow] = []
    for row in rows:
        if row["kind"] not in ALLOWED_KINDS:
            raise ContractError(f"invalid thin bridge kind for {row['path']}: {row['kind']}")
        if row["shadow_required"] not in {"true", "false"}:
            raise ContractError(f"invalid shadow_required for {row['path']}")
        try:
            additions = int(row["approved_additions"])
            deletions = int(row["approved_deletions"])
        except ValueError as error:
            raise ContractError(f"invalid line budget for {row['path']}") from error
        if additions < 0 or deletions < 0:
            raise ContractError(f"negative line budget for {row['path']}")
        budget_overrides: list[tuple[str, int, int]] = []
        seen_override_commits: set[str] = set()
        override_value = row["baseline_budget_overrides"]
        if override_value != "-":
            for item in override_value.split(","):
                fields = item.split(":")
                if len(fields) != 3 or not re.fullmatch(r"[0-9a-f]{40}", fields[0]):
                    raise ContractError(f"invalid baseline budget override for {row['path']}")
                try:
                    override_additions = int(fields[1])
                    override_deletions = int(fields[2])
                except ValueError as error:
                    raise ContractError(f"invalid baseline budget override for {row['path']}") from error
                if override_additions < 0 or override_deletions < 0:
                    raise ContractError(f"negative baseline budget override for {row['path']}")
                if fields[0] in seen_override_commits:
                    raise ContractError(f"duplicate baseline budget override for {row['path']}: {fields[0]}")
                seen_override_commits.add(fields[0])
                budget_overrides.append((fields[0], override_additions, override_deletions))
        result.append(ContractRow(
            path=row["path"],
            kind=row["kind"],
            shadow_required=row["shadow_required"] == "true",
            additions=additions,
            deletions=deletions,
            budget_overrides=tuple(budget_overrides),
        ))
    paths = [row.path for row in result]
    if paths != sorted(paths):
        raise ContractError("thin bridge contract paths must be sorted")
    if len(paths) != len(set(paths)):
        raise ContractError("thin bridge contract contains duplicate paths")
    return result


def load_thin_bridge_paths(ledger: Path) -> set[str]:
    rows = load_tsv(
        ledger,
        [
            "path", "initial_status", "decision", "expected_status", "category",
            "base_blob", "final_blob", "shadow_source", "shadow_target",
            "verification", "reason",
        ],
    )
    return {row["path"] for row in rows if row["category"] == "official-thin-bridge"}


def load_shadow_map(path: Path) -> dict[str, set[str]]:
    relations: dict[str, set[str]] = {}
    try:
        lines = path.read_text(encoding="utf-8-sig").splitlines()
    except OSError as error:
        raise ContractError(f"cannot read {path}: {error}") from error
    for line_number, raw in enumerate(lines, 1):
        line = raw.rstrip("\r")
        if not line or line.startswith("#"):
            continue
        fields = line.split("\t")
        if len(fields) != 2 or not fields[0] or not fields[1]:
            raise ContractError(f"invalid shadow map row at {path}:{line_number}")
        sources = {item for item in fields[0].split("|") if item and item != "@removed"}
        targets = {item for item in fields[1].split("|") if item and item != "@removed"}
        for source in sources:
            relations.setdefault(source, set()).update(targets)
    return relations


def candidate_file(repo: Path, candidate_tree: str, path: str) -> str:
    content = run_git(repo, "show", f"{candidate_tree}:{path}", text=False)
    assert isinstance(content, bytes)
    return content.decode("utf-8", "replace")


def target_exists(repo: Path, candidate_tree: str, path: str) -> bool:
    result = subprocess.run(
        ["git", "-C", str(repo), "cat-file", "-e", f"{candidate_tree}:{path}"],
        check=False,
        capture_output=True,
    )
    return result.returncode == 0


def line_counts(repo: Path, baseline: str, candidate_tree: str, path: str) -> tuple[int, int]:
    output = run_git(repo, "diff", "--numstat", "--no-renames", baseline, candidate_tree, "--", path)
    assert isinstance(output, str)
    lines = [line for line in output.splitlines() if line]
    if len(lines) != 1:
        raise ContractError(f"thin bridge must have exactly one numstat row: {path}")
    additions, deletions, actual_path = lines[0].split("\t", 2)
    if actual_path != path or additions == "-" or deletions == "-":
        raise ContractError(f"thin bridge has unsupported binary or renamed diff: {path}")
    return int(additions), int(deletions)


def added_lines(repo: Path, baseline: str, candidate_tree: str, path: str) -> list[str]:
    output = run_git(
        repo,
        "diff",
        "--unified=0",
        "--no-ext-diff",
        "--no-renames",
        baseline,
        candidate_tree,
        "--",
        path,
    )
    assert isinstance(output, str)
    return [line[1:] for line in output.splitlines() if line.startswith("+") and not line.startswith("+++")]


def added_line_numbers(repo: Path, baseline: str, candidate_tree: str, path: str) -> set[int]:
    output = run_git(
        repo,
        "diff",
        "--unified=0",
        "--no-ext-diff",
        "--no-renames",
        baseline,
        candidate_tree,
        "--",
        path,
    )
    assert isinstance(output, str)
    result: set[int] = set()
    candidate_line: int | None = None
    for line in output.splitlines():
        if line.startswith("@@"):
            match = re.search(r"\+(\d+)(?:,\d+)?", line)
            candidate_line = int(match.group(1)) if match else None
            continue
        if candidate_line is None or line.startswith(("diff ", "index ", "---", "+++")):
            continue
        if line.startswith("+"):
            result.add(candidate_line)
            candidate_line += 1
        elif line.startswith("-") or line.startswith("\\"):
            continue
        else:
            candidate_line += 1
    return result


def matching_brace(content: str, opening: int) -> int | None:
    depth = 0
    index = opening
    quote = ""
    escaped = False
    line_comment = False
    block_comment = False
    while index < len(content):
        char = content[index]
        next_char = content[index + 1] if index + 1 < len(content) else ""
        if line_comment:
            if char == "\n":
                line_comment = False
            index += 1
            continue
        if block_comment:
            if char == "*" and next_char == "/":
                block_comment = False
                index += 2
            else:
                index += 1
            continue
        if quote:
            if escaped:
                escaped = False
            elif char == "\\" and quote != "`":
                escaped = True
            elif char == quote:
                quote = ""
            index += 1
            continue
        if char == "/" and next_char == "/":
            line_comment = True
            index += 2
            continue
        if char == "/" and next_char == "*":
            block_comment = True
            index += 2
            continue
        if char in {'"', "'", "`"}:
            quote = char
            index += 1
            continue
        if char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return index
        index += 1
    return None


def function_blocks(content: str) -> list[FunctionBlock]:
    blocks: list[FunctionBlock] = []
    seen: set[tuple[int, str]] = set()
    for pattern in FUNCTION_START_PATTERNS:
        for match in pattern.finditer(content):
            identity = (match.start(), match.group("name"))
            if identity in seen:
                continue
            seen.add(identity)
            if content[match.start():match.end()].rstrip().endswith("{"):
                opening = content.rfind("{", match.start(), match.end())
            else:
                opening = content.find("{", match.end(), min(len(content), match.end() + 4096))
            if opening < 0:
                continue
            closing = matching_brace(content, opening)
            if closing is None:
                continue
            blocks.append(FunctionBlock(
                name=match.group("name"),
                start_line=content.count("\n", 0, match.start()) + 1,
                end_line=content.count("\n", 0, closing) + 1,
            ))
    return blocks


def containing_function(blocks: list[FunctionBlock], line_number: int) -> FunctionBlock | None:
    matches = [block for block in blocks if block.start_line <= line_number <= block.end_line]
    if not matches:
        return None
    return min(matches, key=lambda block: block.end_line - block.start_line)


def delegate_view_call_surface(content: str) -> Counter[tuple[str, str]]:
    """Return executable call-like references grouped by their owning function.

    This intentionally uses a small language-neutral lexer instead of external
    Go/Vue parsers. The reviewed allowlist is a positive delta from the exact
    Vendor baseline, so conservative matches in templates or interface method
    declarations remain stable and cannot create an unreviewed runtime call.
    """
    def executable_callees(fragment: str) -> list[str]:
        callees: list[str] = []
        for pattern in CALL_SURFACE_PATTERNS:
            for match in pattern.finditer(fragment):
                callee = match.group("callee")
                if callee in CALL_SURFACE_IGNORED or callee.rsplit(".", 1)[-1] in CALL_SURFACE_IGNORED:
                    continue
                callees.append(callee)
        return callees

    lines = content.splitlines()
    blocks = function_blocks(content)
    surface: Counter[tuple[str, str]] = Counter()
    for line_number, line in enumerate(lines, 1):
        if line.lstrip().startswith(("//", "#", "*")):
            continue
        block = containing_function(blocks, line_number)
        function_name = block.name if block else "<top-level>"
        declared_names = {
            match.group("name")
            for pattern in FUNCTION_START_PATTERNS
            for match in pattern.finditer(line)
        }
        for callee in executable_callees(line):
            if callee in declared_names:
                continue
            surface[(function_name, callee)] += 1

    detailed_computed_closings: set[int] = set()
    for match in COMPUTED_CALL_SURFACE_RE.finditer(content):
        line_number = content.count("\n", 0, match.start()) + 1
        line = lines[line_number - 1]
        if line.lstrip().startswith(("//", "#", "*")):
            continue
        block = containing_function(blocks, line_number)
        function_name = block.name if block else "<top-level>"
        owner = match.group("object") or ""
        key = " ".join(match.group("key").split())
        callee = f"{owner}[{key}]" if owner else f"[{key}]"
        surface[(function_name, callee)] += 1
        detailed_computed_closings.add(content.rfind("]", match.start(), match.end()))

    for match in COMPUTED_CALL_FALLBACK_RE.finditer(content):
        if match.start() in detailed_computed_closings:
            continue
        line_number = content.count("\n", 0, match.start()) + 1
        line = lines[line_number - 1]
        if line.lstrip().startswith(("//", "#", "*")):
            continue
        block = containing_function(blocks, line_number)
        function_name = block.name if block else "<top-level>"
        surface[(function_name, "[<computed>]")] += 1

    for match in VUE_EVENT_BINDING_RE.finditer(content):
        expression = " ".join(match.group("expression").split())
        if (
            not expression
            or executable_callees(expression)
            or COMPUTED_CALL_FALLBACK_RE.search(expression)
        ):
            continue
        binding = match.group("binding")
        if binding.startswith("v-on:"):
            binding = f"@{binding.removeprefix('v-on:')}"
        surface[(f"<template:{binding}>", expression)] += 1
    return surface


def validate_delegate_view_structure(
    row: ContractRow,
    baseline_commit: str,
    baseline_content: str,
    content: str,
    changed_lines: set[int],
) -> None:
    if row.kind not in {"delegate", "view"}:
        return
    lines = content.splitlines()
    blocks = function_blocks(content)
    baseline_names = {block.name for block in function_blocks(baseline_content)}
    approved_new = APPROVED_NEW_BRIDGE_FUNCTIONS.get(row.path, frozenset())
    unexpected_functions = sorted({
        block.name
        for block in blocks
        if block.name not in baseline_names and block.name not in approved_new
    })
    if unexpected_functions:
        raise ContractError(
            f"{row.kind} bridge introduces orchestration through an unapproved "
            f"new function in {row.path}: {unexpected_functions}"
        )

    approved_calls = Counter(BASELINE_DELEGATE_VIEW_CALL_DELTAS.get(
        (baseline_commit, row.path),
        APPROVED_DELEGATE_VIEW_CALL_DELTAS.get(row.path, ()),
    ))
    added_calls = delegate_view_call_surface(content) - delegate_view_call_surface(baseline_content)
    unexpected_calls = added_calls - approved_calls
    if unexpected_calls:
        raise ContractError(
            f"{row.kind} bridge introduces an unapproved executable call in {row.path}: "
            f"{sorted(unexpected_calls.elements())}"
        )
    missing_calls = approved_calls - added_calls
    if missing_calls:
        raise ContractError(
            f"{row.kind} bridge is missing an approved executable call in {row.path}: "
            f"{sorted(missing_calls.elements())}"
        )

    approved_control = Counter(APPROVED_DELEGATE_VIEW_CONTROL.get(row.path, ()))
    actual_control: Counter[tuple[str, str]] = Counter()
    for line_number in sorted(changed_lines):
        if line_number < 1 or line_number > len(lines):
            continue
        line = lines[line_number - 1]
        block = containing_function(blocks, line_number)
        function_name = block.name if block else "<top-level>"
        if DELEGATE_VIEW_CONTROL_FLOW_RE.search(line):
            actual_control[(function_name, line.strip())] += 1
        if not ORCHESTRATION_RE.search(line):
            continue
        raise ContractError(
            f"{row.kind} bridge introduces orchestration in "
            f"{function_name}: {row.path}:{line_number}"
        )

    unexpected_control = actual_control - approved_control
    if unexpected_control:
        raise ContractError(
            f"{row.kind} bridge introduces unapproved control flow in {row.path}: "
            f"{sorted(unexpected_control.elements())}"
        )


def validate(args: argparse.Namespace) -> None:
    repo = args.repo_root.resolve()
    baseline_commit = run_git(repo, "rev-parse", "--verify", f"{args.baseline}^{{commit}}")
    assert isinstance(baseline_commit, str)
    baseline_commit = baseline_commit.strip()
    contract_rows = load_contract(args.contract)
    contract_paths = {row.path for row in contract_rows}
    ledger_paths = load_thin_bridge_paths(args.ledger)
    if contract_paths != ledger_paths:
        missing = sorted(ledger_paths - contract_paths)
        extra = sorted(contract_paths - ledger_paths)
        raise ContractError(f"thin bridge contract/ledger mismatch; missing={missing}, extra={extra}")

    shadows = load_shadow_map(args.shadow_map)
    for row in contract_rows:
        content = candidate_file(repo, args.candidate_tree, row.path)
        targets = shadows.get(row.path, set())
        direct_custom_import = bool(CUSTOM_IMPORT_RE.search(content))
        if (row.shadow_required or direct_custom_import or row.kind in {"delegate", "view"}) and not targets:
            raise ContractError(f"thin bridge requires an exact shadow mapping: {row.path}")
        if row.kind in {"delegate", "view"} and not row.shadow_required:
            raise ContractError(f"delegate/view must set shadow_required=true: {row.path}")
        for target in targets:
            if not target.startswith(("backend/internal/custom/", "frontend/src/custom/")):
                raise ContractError(f"shadow target is outside Custom roots for {row.path}: {target}")
            if not target_exists(repo, args.candidate_tree, target):
                raise ContractError(f"shadow target does not exist for {row.path}: {target}")

        additions, deletions = line_counts(repo, args.baseline, args.candidate_tree, row.path)
        approved_budget = (row.additions, row.deletions)
        for override_commit, override_additions, override_deletions in row.budget_overrides:
            if override_commit == baseline_commit:
                approved_budget = (override_additions, override_deletions)
                break
        if (additions, deletions) != approved_budget:
            raise ContractError(
                f"thin bridge line budget mismatch for {row.path}: "
                f"actual +{additions}/-{deletions}, approved +{approved_budget[0]}/-{approved_budget[1]}"
            )

        additions_only = added_lines(repo, args.baseline, args.candidate_tree, row.path)
        code = "\n".join(line for line in additions_only if not line.lstrip().startswith(("//", "#", "*")))
        for forbidden in HIGH_RISK_DEFINITIONS.get(row.path, ()):
            if forbidden.search(content):
                raise ContractError(f"high-risk business symbol returned to official bridge: {row.path}")
        if row.kind in {"delegate", "view"}:
            baseline_content = candidate_file(repo, args.baseline, row.path)
            validate_delegate_view_structure(
                row,
                baseline_commit,
                baseline_content,
                content,
                added_line_numbers(repo, args.baseline, args.candidate_tree, row.path),
            )
        if row.kind in {"dto", "wire", "persistence"} and CONTROL_FLOW_RE.search(code):
            raise ContractError(f"{row.kind} bridge introduces a loop or watcher: {row.path}")
        if row.kind in {"dto", "wire"} and DTO_WIRE_CONTROL_FLOW_RE.search(code):
            raise ContractError(f"{row.kind} bridge introduces control flow: {row.path}")
        if row.kind in {"dto", "wire", "persistence"} and BUSINESS_HELPER_RE.search(code):
            raise ContractError(f"{row.kind} bridge introduces a business helper: {row.path}")

def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo-root", type=Path, required=True)
    parser.add_argument("--baseline", required=True)
    parser.add_argument("--candidate-tree", required=True)
    parser.add_argument("--contract", type=Path)
    parser.add_argument("--ledger", type=Path)
    parser.add_argument("--shadow-map", type=Path)
    args = parser.parse_args(argv)
    args.contract = args.contract or args.repo_root / ".github/custom-thin-bridge-contract.tsv"
    args.ledger = args.ledger or args.repo_root / ".github/custom-upstream-delta.tsv"
    args.shadow_map = args.shadow_map or args.repo_root / ".github/upstream-shadowed-sources.tsv"
    return args


def main(argv: list[str] | None = None) -> int:
    try:
        validate(parse_args(argv or sys.argv[1:]))
    except ContractError as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print("custom thin bridge contract passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
