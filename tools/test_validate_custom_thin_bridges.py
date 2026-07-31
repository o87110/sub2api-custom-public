from __future__ import annotations

import csv
import subprocess
import tempfile
import unittest
from pathlib import Path

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

    def test_accepts_an_allowlisted_dto_projection_loop(self) -> None:
        fixture = self.fixture(
            bridge_path="backend/internal/handler/api_key_handler.go",
            base_content="package handler\n",
            candidate_content=(
                "package handler\n"
                "func (h *APIKeyHandler) GetAvailableGroups() {\n"
                "  for index := range options {\n"
                "    _ = index\n"
                "  }\n"
                "}\n"
            ),
            kind="delegate",
            shadow_required=True,
        )
        validator.validate(fixture.args())


if __name__ == "__main__":
    unittest.main()
