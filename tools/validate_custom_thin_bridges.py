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
    "frontend/src/views/user/KeysView.vue": frozenset({
        "refreshApiKeys",
        "handleTableSelectionChange",
        "handleBulkCompleted",
    }),
}

# Control-flow additions in delegate/view bridges use an exact structural
# allowlist. Keeping the owning function and complete trimmed statement makes
# renames, additional branches, and moved orchestration fail even when the TSV
# line budget and shadow mapping are updated.
APPROVED_DELEGATE_VIEW_CONTROL: dict[str, tuple[tuple[str, str], ...]] = {
    "backend/internal/handler/admin/setting_handler.go": (
        ("GetSettings", "if err != nil {"),
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
        ("applyFlaggedAccountSideEffects", "if n, err := s.countFlaggedByUserSince(ctx, *log.UserID, since, cfg.CyberPolicyExcludeFromBanCount); err == nil {"),
        ("RecordCyberPolicyEvent", "if s.tryRecordCustomCyberPolicyEvent(ctx, in) {"),
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
    "backend/internal/service/payment_config_service.go": (
        ("GetPaymentConfig", "if err != nil {"),
        ("UpdatePaymentConfig", "if req.ChannelSettings != nil {"),
        ("UpdatePaymentConfig", "if err != nil {"),
        ("setPaymentConfigValue", "if provided {"),
    ),
    "backend/internal/service/payment_order.go": (
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
    "frontend/src/components/user/monitor/MonitorCard.vue": (
        ("<top-level>", "v-if=\"typeof item.group_rate_multiplier === 'number'\""),
    ),
    "frontend/src/views/admin/GroupsView.vue": (
        ("handleCreateGroup", "if (minimumBalance === null) {"),
        ("handleUpdateGroup", "if (minimumBalance === null) {"),
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
        result.append(ContractRow(
            path=row["path"],
            kind=row["kind"],
            shadow_required=row["shadow_required"] == "true",
            additions=additions,
            deletions=deletions,
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


def validate_delegate_view_structure(
    row: ContractRow,
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
        if (additions, deletions) != (row.additions, row.deletions):
            raise ContractError(
                f"thin bridge line budget mismatch for {row.path}: "
                f"actual +{additions}/-{deletions}, approved +{row.additions}/-{row.deletions}"
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
