#!/usr/bin/env python3
"""Validate pnpm audit JSON before the custom exception checker consumes it."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import sys


class AuditValidationError(ValueError):
    """Raised when pnpm did not produce a trustworthy audit report."""


def validate_report(document: object, exit_code: int) -> bool:
    if not isinstance(document, dict):
        raise AuditValidationError("audit report must be a JSON object")
    if "error" in document:
        raise AuditValidationError("audit report contains a top-level error")

    metadata = document.get("metadata")
    if not isinstance(metadata, dict) or not metadata:
        raise AuditValidationError("audit report metadata is missing or invalid")

    known_schema = False
    has_vulnerabilities = False
    if "vulnerabilities" in document:
        vulnerabilities = document["vulnerabilities"]
        if not isinstance(vulnerabilities, dict):
            raise AuditValidationError("vulnerabilities must be an object")
        known_schema = True
        has_vulnerabilities = has_vulnerabilities or bool(vulnerabilities)

    if "advisories" in document:
        advisories = document["advisories"]
        if not isinstance(advisories, (dict, list)):
            raise AuditValidationError("advisories must be an object or array")
        known_schema = True
        has_vulnerabilities = has_vulnerabilities or bool(advisories)

    if not known_schema:
        raise AuditValidationError("audit report has no supported vulnerability schema")

    metadata_vulnerabilities = metadata.get("vulnerabilities")
    if metadata_vulnerabilities is not None:
        if not isinstance(metadata_vulnerabilities, dict):
            raise AuditValidationError("metadata.vulnerabilities must be an object")
        for severity, count in metadata_vulnerabilities.items():
            if not isinstance(severity, str) or not isinstance(count, int) or count < 0:
                raise AuditValidationError("metadata.vulnerabilities contains invalid counts")

    if exit_code < 0 or exit_code > 255:
        raise AuditValidationError("audit exit code is invalid")
    if exit_code != 0 and not has_vulnerabilities:
        raise AuditValidationError(
            "pnpm audit failed without reporting a supported vulnerability"
        )
    return exit_code != 0 and has_vulnerabilities


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--audit", required=True, type=Path)
    parser.add_argument("--exit-code", required=True, type=int)
    parser.add_argument("--state-output", type=Path)
    args = parser.parse_args()

    try:
        with args.audit.open("r", encoding="utf-8") as handle:
            document = json.load(handle)
        has_vulnerabilities = validate_report(document, args.exit_code)
    except (OSError, json.JSONDecodeError, AuditValidationError) as error:
        print(f"invalid pnpm audit report: {error}", file=sys.stderr)
        return 1

    state = (
        f"audit_exit_code={args.exit_code}\n"
        f"audit_has_vulnerabilities={'true' if has_vulnerabilities else 'false'}\n"
    )
    if args.state_output:
        args.state_output.write_text(state, encoding="utf-8")
    else:
        sys.stdout.write(state)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
