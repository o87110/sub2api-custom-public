#!/usr/bin/env python3
"""Validate the exact structural contract for official thin bridges."""

from __future__ import annotations

import argparse
import csv
import re
import subprocess
import sys
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

# These loops only project official repository/service values into response or
# adapter DTOs. Every other newly added loop/watcher/orchestration construct in
# a delegate or view must live in Custom instead of being approved by budget.
DELEGATE_CONTROL_FLOW_ALLOWLIST: dict[str, frozenset[str]] = {
    "backend/internal/handler/api_key_handler.go": frozenset({"GetAvailableGroups"}),
    "backend/internal/service/api_key_service.go": frozenset({
        "GetAvailableGroupOptions",
        "availableGroupsForUser",
    }),
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
    content: str,
    changed_lines: set[int],
) -> None:
    if row.kind not in {"delegate", "view"}:
        return
    lines = content.splitlines()
    blocks = function_blocks(content)
    allowed = DELEGATE_CONTROL_FLOW_ALLOWLIST.get(row.path, frozenset())
    for line_number in sorted(changed_lines):
        if line_number < 1 or line_number > len(lines):
            continue
        line = lines[line_number - 1]
        if not CONTROL_FLOW_RE.search(line) and not ORCHESTRATION_RE.search(line):
            continue
        block = containing_function(blocks, line_number)
        function_name = block.name if block else "<top-level>"
        if row.kind == "delegate" and function_name in allowed:
            continue
        raise ContractError(
            f"{row.kind} bridge introduces orchestration/control flow in "
            f"{function_name}: {row.path}:{line_number}"
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
        validate_delegate_view_structure(
            row,
            content,
            added_line_numbers(repo, args.baseline, args.candidate_tree, row.path),
        )
        if row.kind in {"dto", "wire", "persistence"} and CONTROL_FLOW_RE.search(code):
            raise ContractError(f"{row.kind} bridge introduces a loop or watcher: {row.path}")
        if row.kind in {"dto", "wire"} and DTO_WIRE_CONTROL_FLOW_RE.search(code):
            raise ContractError(f"{row.kind} bridge introduces control flow: {row.path}")
        if row.kind in {"dto", "wire", "persistence"} and BUSINESS_HELPER_RE.search(code):
            raise ContractError(f"{row.kind} bridge introduces a business helper: {row.path}")

        for forbidden in HIGH_RISK_DEFINITIONS.get(row.path, ()):
            if forbidden.search(content):
                raise ContractError(f"high-risk business symbol returned to official bridge: {row.path}")


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
