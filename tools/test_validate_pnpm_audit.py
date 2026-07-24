import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from validate_pnpm_audit import AuditValidationError, validate_report


class ValidatePnpmAuditTest(unittest.TestCase):
    def test_valid_zero_vulnerability_schema_passes(self) -> None:
        report = {
            "auditReportVersion": 2,
            "vulnerabilities": {},
            "metadata": {"vulnerabilities": {"high": 0, "total": 0}},
        }
        self.assertFalse(validate_report(report, 0))

    def test_valid_advisory_schema_with_findings_passes_nonzero(self) -> None:
        report = {
            "advisories": {"1": {"severity": "high"}},
            "metadata": {"totalDependencies": 42},
        }
        self.assertTrue(validate_report(report, 1))

    def test_exit_zero_does_not_require_exception_check(self) -> None:
        report = {
            "vulnerabilities": {"low-only": {"severity": "low"}},
            "metadata": {"vulnerabilities": {"low": 1, "high": 0}},
        }
        self.assertFalse(validate_report(report, 0))

    def test_empty_document_fails(self) -> None:
        with self.assertRaises(AuditValidationError):
            validate_report({}, 0)

    def test_top_level_error_fails(self) -> None:
        with self.assertRaises(AuditValidationError):
            validate_report(
                {
                    "error": {"summary": "registry unavailable"},
                    "vulnerabilities": {},
                    "metadata": {"totalDependencies": 42},
                },
                1,
            )

    def test_empty_top_level_error_still_fails(self) -> None:
        with self.assertRaises(AuditValidationError):
            validate_report(
                {
                    "error": {},
                    "vulnerabilities": {},
                    "metadata": {"totalDependencies": 42},
                },
                0,
            )

    def test_nonzero_without_findings_fails(self) -> None:
        with self.assertRaises(AuditValidationError):
            validate_report(
                {
                    "vulnerabilities": {},
                    "metadata": {"vulnerabilities": {"high": 0}},
                },
                1,
            )

    def test_unknown_schema_fails(self) -> None:
        with self.assertRaises(AuditValidationError):
            validate_report({"metadata": {"totalDependencies": 42}}, 0)

    def test_invalid_json_fails_cli(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            audit = Path(temp_dir) / "audit.json"
            audit.write_text("{not-json", encoding="utf-8")
            result = subprocess.run(
                [
                    sys.executable,
                    str(Path(__file__).with_name("validate_pnpm_audit.py")),
                    "--audit",
                    str(audit),
                    "--exit-code",
                    "1",
                ],
                check=False,
                capture_output=True,
                text=True,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("invalid pnpm audit report", result.stderr)

    def test_valid_zero_vulnerability_cli_writes_state(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            audit = Path(temp_dir) / "audit.json"
            state = Path(temp_dir) / "state.env"
            audit.write_text(
                json.dumps(
                    {
                        "auditReportVersion": 2,
                        "vulnerabilities": {},
                        "metadata": {"vulnerabilities": {"high": 0}},
                    }
                ),
                encoding="utf-8",
            )
            result = subprocess.run(
                [
                    sys.executable,
                    str(Path(__file__).with_name("validate_pnpm_audit.py")),
                    "--audit",
                    str(audit),
                    "--exit-code",
                    "0",
                    "--state-output",
                    str(state),
                ],
                check=False,
                capture_output=True,
                text=True,
            )
            state_text = state.read_text(encoding="utf-8")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("audit_has_vulnerabilities=false", state_text)


if __name__ == "__main__":
    unittest.main()
