from __future__ import annotations

import csv
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import validate_custom_thin_bridges as validator


LEDGER_HEADER = [
    "path", "initial_status", "decision", "expected_status", "category",
    "base_blob", "final_blob", "shadow_source", "shadow_target",
    "verification", "reason",
]


class ThinBridgeFixture:
    def __init__(
        self,
        bridge_path: str = "frontend/src/api/example.ts",
        base_content: str = "export const value = 1\n",
        candidate_content: str = "export const value = 2\n",
        kind: str = "dto",
        shadow_required: bool = False,
    ) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.bridge_path = bridge_path
        self.kind = kind
        self.shadow_required = shadow_required
        self.target_path = "frontend/src/custom/example/implementation.ts"
        self._git("init", "-q")
        self._git("config", "user.email", "fixture@example.com")
        self._git("config", "user.name", "fixture")
        self._write(bridge_path, base_content)
        self._git("add", ".")
        self._git("commit", "-qm", "baseline")
        self.baseline = self._git("rev-parse", "HEAD").strip()

        self._write(bridge_path, candidate_content)
        self._write(self.target_path, "export const implementation = true\n")
        self._git("add", ".")
        self._git("commit", "-qm", "candidate")
        self.candidate_tree = self._git("rev-parse", "HEAD^{tree}").strip()
        additions, deletions, _ = self._git(
            "diff", "--numstat", self.baseline, self.candidate_tree, "--", bridge_path,
        ).strip().split("\t", 2)
        self.additions = int(additions)
        self.deletions = int(deletions)
        self.write_ledger(include_bridge=True)
        self.write_shadow(include_relation=shadow_required)
        self.write_contract()

    def close(self) -> None:
        self.temp.cleanup()

    def _git(self, *args: str) -> str:
        return subprocess.run(
            ["git", "-C", str(self.root), *args],
            check=True,
            capture_output=True,
            text=True,
            encoding="utf-8",
        ).stdout

    def _write(self, path: str, content: str) -> None:
        target = self.root / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content, encoding="utf-8")

    def _write_tsv(self, path: str, header: list[str], rows: list[list[object]]) -> None:
        target = self.root / path
        target.parent.mkdir(parents=True, exist_ok=True)
        with target.open("w", encoding="utf-8", newline="") as handle:
            writer = csv.writer(handle, delimiter="\t", lineterminator="\n")
            writer.writerow(header)
            writer.writerows(rows)

    def write_ledger(self, include_bridge: bool) -> None:
        rows = []
        if include_bridge:
            rows.append([
                self.bridge_path, "M", "keep", "M", "official-thin-bridge",
                "base", "final", "-", "-", "fixture", "fixture",
            ])
        self._write_tsv(".github/custom-upstream-delta.tsv", LEDGER_HEADER, rows)

    def write_shadow(self, include_relation: bool) -> None:
        content = "# source\ttarget\n"
        if include_relation:
            content += f"{self.bridge_path}\t{self.target_path}\n"
        self._write(".github/upstream-shadowed-sources.tsv", content)

    def write_contract(
        self,
        *,
        include_bridge: bool = True,
        additions: int | None = None,
        deletions: int | None = None,
    ) -> None:
        rows = []
        if include_bridge:
            rows.append([
                self.bridge_path,
                self.kind,
                str(self.shadow_required).lower(),
                self.additions if additions is None else additions,
                self.deletions if deletions is None else deletions,
            ])
        self._write_tsv(
            ".github/custom-thin-bridge-contract.tsv",
            validator.CONTRACT_HEADER,
            rows,
        )

    def args(self):
        return validator.parse_args([
            "--repo-root", str(self.root),
            "--baseline", self.baseline,
            "--candidate-tree", self.candidate_tree,
        ])


class ThinBridgeContractTests(unittest.TestCase):
    def fixture(self, **kwargs) -> ThinBridgeFixture:
        fixture = ThinBridgeFixture(**kwargs)
        self.addCleanup(fixture.close)
        return fixture

    def test_accepts_an_exact_dto_contract(self) -> None:
        fixture = self.fixture()
        validator.validate(fixture.args())

    def test_rejects_a_bridge_missing_from_the_contract(self) -> None:
        fixture = self.fixture()
        fixture.write_contract(include_bridge=False)
        with self.assertRaisesRegex(validator.ContractError, "contract/ledger mismatch"):
            validator.validate(fixture.args())

    def test_rejects_a_required_shadow_mapping(self) -> None:
        fixture = self.fixture(
            bridge_path="frontend/src/views/ExampleView.vue",
            base_content="<template />\n",
            candidate_content="<template><CustomView /></template>\n",
            kind="view",
            shadow_required=True,
        )
        fixture.write_shadow(include_relation=False)
        with self.assertRaisesRegex(validator.ContractError, "requires an exact shadow mapping"):
            validator.validate(fixture.args())

    def test_rejects_an_inexact_line_budget(self) -> None:
        fixture = self.fixture()
        fixture.write_contract(additions=fixture.additions + 1)
        with self.assertRaisesRegex(validator.ContractError, "line budget mismatch"):
            validator.validate(fixture.args())

    def test_rejects_control_flow_in_a_dto_bridge(self) -> None:
        fixture = self.fixture(candidate_content="if (enabled) { value = 2 }\n", kind="dto")
        with self.assertRaisesRegex(validator.ContractError, "introduces control flow"):
            validator.validate(fixture.args())

    def test_rejects_a_high_risk_business_function_returning_to_a_view(self) -> None:
        fixture = self.fixture(
            bridge_path="frontend/src/views/user/PaymentView.vue",
            base_content="<template />\n",
            candidate_content=(
                "<template />\n"
                "<script setup lang=\"ts\">\n"
                "function attemptMobileQrFallback() { return false }\n"
                "</script>\n"
            ),
            kind="view",
            shadow_required=True,
        )
        with self.assertRaisesRegex(validator.ContractError, "high-risk business symbol"):
            validator.validate(fixture.args())

    def test_rejects_a_renamed_view_orchestrator(self) -> None:
        fixture = self.fixture(
            bridge_path="frontend/src/views/user/PaymentView.vue",
            base_content="<template />\n",
            candidate_content=(
                "<template />\n"
                "<script setup lang=\"ts\">\n"
                "async function orchestrateCheckout() {\n"
                "  for (const channel of channels) {\n"
                "    await retry(channel)\n"
                "  }\n"
                "}\n"
                "</script>\n"
            ),
            kind="view",
            shadow_required=True,
        )
        with self.assertRaisesRegex(validator.ContractError, "view bridge introduces orchestration"):
            validator.validate(fixture.args())

    def test_rejects_a_renamed_delegate_orchestrator(self) -> None:
        fixture = self.fixture(
            bridge_path="backend/internal/service/payment_order.go",
            base_content="package service\n",
            candidate_content=(
                "package service\n"
                "func orchestrateCheckout() {\n"
                "  for _, channel := range channels {\n"
                "    retry(channel)\n"
                "  }\n"
                "}\n"
            ),
            kind="delegate",
            shadow_required=True,
        )
        with self.assertRaisesRegex(validator.ContractError, "delegate bridge introduces orchestration"):
            validator.validate(fixture.args())

    def test_rejects_a_renamed_view_conditional_orchestrator(self) -> None:
        fixture = self.fixture(
            bridge_path="frontend/src/views/user/PaymentView.vue",
            base_content="<template />\n",
            candidate_content=(
                "<template />\n"
                "<script setup lang=\"ts\">\n"
                "async function processCheckout() {\n"
                "  if (enabled) await submitOrder()\n"
                "  await confirmOrder()\n"
                "}\n"
                "</script>\n"
            ),
            kind="view",
            shadow_required=True,
        )
        with self.assertRaisesRegex(validator.ContractError, "view bridge introduces orchestration"):
            validator.validate(fixture.args())

    def test_rejects_a_new_multistep_view_helper_without_control_flow(self) -> None:
        fixture = self.fixture(
            bridge_path="frontend/src/views/user/PaymentView.vue",
            base_content="<template />\n",
            candidate_content=(
                "<template />\n"
                "<script setup lang=\"ts\">\n"
                "async function processCheckout() {\n"
                "  await submitOrder()\n"
                "  await confirmOrder()\n"
                "}\n"
                "</script>\n"
            ),
            kind="view",
            shadow_required=True,
        )
        with self.assertRaisesRegex(validator.ContractError, "unapproved new function"):
            validator.validate(fixture.args())

    def test_rejects_sequential_calls_added_to_an_existing_view_function(self) -> None:
        fixture = self.fixture(
            bridge_path="frontend/src/views/user/PaymentView.vue",
            base_content=(
                "<template />\n"
                "<script setup lang=\"ts\">\n"
                "async function processCheckout() {\n"
                "  await trackCheckout()\n"
                "}\n"
                "</script>\n"
            ),
            candidate_content=(
                "<template />\n"
                "<script setup lang=\"ts\">\n"
                "async function processCheckout() {\n"
                "  await trackCheckout()\n"
                "  await submitOrder()\n"
                "  await confirmOrder()\n"
                "}\n"
                "</script>\n"
            ),
            kind="view",
            shadow_required=True,
        )
        with self.assertRaisesRegex(validator.ContractError, "unapproved executable call"):
            validator.validate(fixture.args())

    def test_rejects_indirect_calls_added_to_an_existing_view_function(self) -> None:
        fixture = self.fixture(
            bridge_path="frontend/src/views/user/PaymentView.vue",
            base_content=(
                "<template />\n"
                "<script setup lang=\"ts\">\n"
                "async function processCheckout() {\n"
                "  await trackCheckout()\n"
                "}\n"
                "</script>\n"
            ),
            candidate_content=(
                "<template />\n"
                "<script setup lang=\"ts\">\n"
                "async function processCheckout() {\n"
                "  await trackCheckout()\n"
                "  await (submitOrder)()\n"
                "  await confirmOrder?.()\n"
                "}\n"
                "</script>\n"
            ),
            kind="view",
            shadow_required=True,
        )
        with self.assertRaisesRegex(validator.ContractError, "unapproved executable call"):
            validator.validate(fixture.args())

    def test_rejects_computed_calls_added_to_an_existing_view_function(self) -> None:
        fixture = self.fixture(
            bridge_path="frontend/src/views/user/PaymentView.vue",
            base_content=(
                "<template />\n"
                "<script setup lang=\"ts\">\n"
                "async function processCheckout() {\n"
                "  await trackCheckout()\n"
                "}\n"
                "</script>\n"
            ),
            candidate_content=(
                "<template />\n"
                "<script setup lang=\"ts\">\n"
                "async function processCheckout() {\n"
                "  await trackCheckout()\n"
                "  await actions[\"submitOrder\"]()\n"
                "  await actions[\"confirmOrder\"]()\n"
                "}\n"
                "</script>\n"
            ),
            kind="view",
            shadow_required=True,
        )
        with patch.dict(
            validator.APPROVED_DELEGATE_VIEW_CALL_DELTAS,
            {fixture.bridge_path: ()},
        ):
            with self.assertRaisesRegex(validator.ContractError, "unapproved executable call"):
                validator.validate(fixture.args())

    def test_computed_call_surface_covers_conservative_variants(self) -> None:
        surface = validator.delegate_view_call_surface(
            "async function processCheckout() {\n"
            "  await actions[\"submitOrder\"]()\n"
            "  await (actions[dynamicKey])?.()\n"
            "  await actions[keys[0]]()\n"
            "  await actions[\n"
            "    nextAction\n"
            "  ]()\n"
            "}\n"
        )
        self.assertEqual(surface[("processCheckout", 'actions["submitOrder"]')], 1)
        self.assertEqual(surface[("processCheckout", "actions[dynamicKey]")], 1)
        self.assertEqual(surface[("processCheckout", "actions[nextAction]")], 1)
        self.assertEqual(surface[("processCheckout", "[<computed>]")], 1)

    def test_rejects_bare_vue_template_event_handlers(self) -> None:
        fixture = self.fixture(
            bridge_path="frontend/src/views/user/PaymentView.vue",
            base_content=(
                '<template><button @click="legacyHandler">Pay</button></template>\n'
            ),
            candidate_content=(
                '<template><button @click="submitOrder" '
                'v-on:keyup="confirmOrder">Pay</button></template>\n'
            ),
            kind="view",
            shadow_required=True,
        )
        with patch.dict(
            validator.APPROVED_DELEGATE_VIEW_CALL_DELTAS,
            {fixture.bridge_path: ()},
        ):
            with self.assertRaisesRegex(validator.ContractError, "unapproved executable call"):
                validator.validate(fixture.args())

    def test_accepts_an_explicitly_approved_adapter_function(self) -> None:
        fixture = self.fixture(
            bridge_path="backend/internal/payment/load_balancer.go",
            base_content="package payment\n",
            candidate_content=(
                "package payment\n"
                "func instanceCoordinator() {\n"
                "  paymentchannels.NewInstanceCoordinator()\n"
                "}\n"
            ),
            kind="delegate",
            shadow_required=True,
        )
        approved = (("instanceCoordinator", "paymentchannels.NewInstanceCoordinator"),)
        with patch.dict(
            validator.APPROVED_DELEGATE_VIEW_CALL_DELTAS,
            {fixture.bridge_path: approved},
        ):
            validator.validate(fixture.args())

    def test_rejects_an_extra_approved_delegate_call(self) -> None:
        fixture = self.fixture(
            bridge_path="backend/internal/payment/load_balancer.go",
            base_content="package payment\n",
            candidate_content=(
                "package payment\n"
                "func instanceCoordinator() {\n"
                "  paymentchannels.NewInstanceCoordinator()\n"
                "  paymentchannels.NewInstanceCoordinator()\n"
                "}\n"
            ),
            kind="delegate",
            shadow_required=True,
        )
        approved = (("instanceCoordinator", "paymentchannels.NewInstanceCoordinator"),)
        with patch.dict(
            validator.APPROVED_DELEGATE_VIEW_CALL_DELTAS,
            {fixture.bridge_path: approved},
        ):
            with self.assertRaisesRegex(validator.ContractError, "unapproved executable call"):
                validator.validate(fixture.args())

    def test_rejects_a_missing_approved_delegate_call(self) -> None:
        fixture = self.fixture(
            bridge_path="backend/internal/payment/load_balancer.go",
            base_content="package payment\n",
            candidate_content=(
                "package payment\n"
                "func instanceCoordinator() {}\n"
            ),
            kind="delegate",
            shadow_required=True,
        )
        approved = (("instanceCoordinator", "paymentchannels.NewInstanceCoordinator"),)
        with patch.dict(
            validator.APPROVED_DELEGATE_VIEW_CALL_DELTAS,
            {fixture.bridge_path: approved},
        ):
            with self.assertRaisesRegex(validator.ContractError, "missing an approved executable call"):
                validator.validate(fixture.args())

    def test_accepts_an_allowlisted_dto_projection_loop(self) -> None:
        fixture = self.fixture(
            bridge_path="backend/internal/handler/api_key_handler.go",
            base_content=(
                "package handler\n"
                "func (h *APIKeyHandler) GetAvailableGroups() {}\n"
            ),
            candidate_content=(
                "package handler\n"
                "func (h *APIKeyHandler) GetAvailableGroups() {\n"
                "  for i := range options {\n"
                "    _ = i\n"
                "  }\n"
                "}\n"
            ),
            kind="delegate",
            shadow_required=True,
        )
        with patch.dict(
            validator.APPROVED_DELEGATE_VIEW_CALL_DELTAS,
            {fixture.bridge_path: ()},
        ):
            validator.validate(fixture.args())

    def test_admin_group_minimum_balance_bridge_is_explicitly_scoped(self) -> None:
        path = "backend/internal/service/admin_group.go"
        self.assertEqual(
            validator.APPROVED_NEW_BRIDGE_FUNCTIONS[path],
            frozenset({"checkGroupMinimumBalanceForUser"}),
        )

        approved_calls = validator.APPROVED_DELEGATE_VIEW_CALL_DELTAS[path]
        for call in (
            ("AdminUpdateAPIKeyGroupID", "s.checkGroupMinimumBalanceForUser"),
            ("ReplaceUserGroup", "s.checkGroupMinimumBalanceForUser"),
            ("checkGroupMinimumBalanceForUser", "s.userRepo.GetByID"),
            ("checkGroupMinimumBalanceForUser", "groupaccess.CheckMinimumBalance"),
        ):
            self.assertEqual(approved_calls.count(call), 1)

        approved_control = validator.APPROVED_DELEGATE_VIEW_CONTROL[path]
        self.assertIn(("ReplaceUserGroup", "if migrated > 0 {"), approved_control)
        self.assertIn(
            ("checkGroupMinimumBalanceForUser", "if group == nil || group.MinimumBalance <= 0 {"),
            approved_control,
        )

    def test_content_moderation_user_ban_threshold_bridge_is_explicitly_scoped(self) -> None:
        path = "backend/internal/service/content_moderation.go"
        approved_calls = validator.APPROVED_DELEGATE_VIEW_CALL_DELTAS[path]
        for call in (
            ("UpdateConfig", "cloneContentModerationUserBanThresholdOverrides"),
            ("persistContentModerationLog", "effectiveContentModerationConfigForUser"),
            ("sendCyberPolicyEmail", "contentModerationEmailVariables"),
            ("validateConfig", "validateContentModerationUserBanThresholdOverrides"),
        ):
            self.assertEqual(approved_calls.count(call), 1)

        approved_control = validator.APPROVED_DELEGATE_VIEW_CONTROL[path]
        self.assertIn(
            ("RecordCyberPolicyEvent", "if err := s.sendCyberPolicyEmail(ctx, cfg, log); err != nil {"),
            approved_control,
        )
        self.assertIn(
            ("UpdateConfig", "if input.UserBanThresholds != nil {"),
            approved_control,
        )
        self.assertIn(
            (
                "validateConfig",
                "if err := validateContentModerationUserBanThresholdOverrides(cfg.UserBanThresholds); err != nil {",
            ),
            approved_control,
        )

        email_path = "backend/internal/service/content_moderation_email.go"
        self.assertIn(
            ("buildCyberPolicyNoticeEmailBody", "if cfg != nil && cfg.BanThreshold > 0 {"),
            validator.APPROVED_DELEGATE_VIEW_CONTROL[email_path],
        )


if __name__ == "__main__":
    unittest.main()
