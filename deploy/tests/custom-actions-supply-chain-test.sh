#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
versions_file="$repo_root/.github/custom-tool-versions.env"
installer="$repo_root/deploy/tests/install-custom-tools.sh"
integration_runner="$repo_root/deploy/tests/backend-integration-test.sh"
workflows=(
  "$repo_root/.github/workflows/backend-ci.yml"
  "$repo_root/.github/workflows/publish-custom.yml"
  "$repo_root/.github/workflows/release.yml"
  "$repo_root/.github/workflows/security-scan.yml"
  "$repo_root/.github/workflows/upstream-sync.yml"
  "$repo_root/.github/workflows/upstream-upgrade-gate.yml"
)

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

trigger_branches() {
  local workflow="$1"
  local trigger="$2"

  awk -v trigger="$trigger" '
    $0 == "  " trigger ":" { in_trigger = 1; next }
    in_trigger && /^  [[:alnum:]_-]+:$/ { exit }
    in_trigger && /^    branches:$/ { in_branches = 1; next }
    in_branches && /^    [[:alnum:]_-]+:$/ { exit }
    in_branches && /^      - / {
      sub(/^      - /, "")
      print
    }
  ' "$workflow"
}

job_block() {
  local workflow="$1"
  local job="$2"
  awk -v job="$job" '
    $0 == "  " job ":" { in_job = 1 }
    in_job && $0 ~ /^  [[:alnum:]_-]+:$/ && $0 != "  " job ":" { exit }
    in_job { print }
  ' "$workflow"
}

[[ -s "$versions_file" ]] || fail "custom tool version manifest is missing"
[[ -s "$installer" ]] || fail "verified custom tool installer is missing"
[[ -s "$integration_runner" ]] || fail "scoped backend Integration runner is missing"
/bin/bash -n "$integration_runner"
# shellcheck disable=SC1090
source "$versions_file"

required_variables=(
  ACTIONLINT_VERSION ACTIONLINT_LINUX_AMD64_URL ACTIONLINT_LINUX_AMD64_SHA256
  ORAS_VERSION ORAS_LINUX_AMD64_URL ORAS_LINUX_AMD64_SHA256
  GORELEASER_VERSION GORELEASER_LINUX_AMD64_URL GORELEASER_LINUX_AMD64_SHA256
  BUILDX_VERSION BUILDX_LINUX_AMD64_URL BUILDX_LINUX_AMD64_SHA256
  QEMU_BINFMT_VERSION QEMU_BINFMT_IMAGE
  NODE_VERSION NODE_LINUX_X64_URL NODE_LINUX_X64_SHA256
  PNPM_VERSION PNPM_TARBALL_URL PNPM_TARBALL_SHA256
  GO_VERSION GO_LINUX_AMD64_URL GO_LINUX_AMD64_SHA256
  GOVULNCHECK_VERSION GOVULNCHECK_MODULE_SUM
  GOLANGCI_LINT_VERSION GOLANGCI_LINT_LINUX_AMD64_URL
  GOLANGCI_LINT_LINUX_AMD64_SHA256
)
for variable in "${required_variables[@]}"; do
  grep -Eq "^${variable}=.+" "$versions_file" ||
    fail "tool manifest is missing $variable"
done

if grep -nEi '=(latest|[0-9]+|[~^><=].*)$|:latest($|@)' "$versions_file"; then
  fail "custom tool versions must be exact and immutable"
fi
grep -Fq 'sha256sum --check --status' "$installer" ||
  fail "custom tool downloads are not checksum-verified"
grep -Fq "curl --fail --location --silent --show-error --proto '=https'" "$installer" ||
  fail "custom tool downloads are not HTTPS-only"
grep -Eq '^QEMU_BINFMT_IMAGE=[^[:space:]]+@sha256:[0-9a-f]{64}$' "$versions_file" ||
  fail "QEMU/binfmt image is not pinned by digest"
grep -Eq "^go ${GO_VERSION}$" "$repo_root/backend/go.mod" ||
  fail "backend/go.mod does not match the declared Go version"
grep -Fq 'source ../.github/custom-upstream-baseline.env' \
  "$repo_root/.github/workflows/backend-ci.yml" ||
  fail "golangci-lint does not load the explicit custom baseline"
awk '
  /^  golangci-lint:$/ { in_job = 1; next }
  /^  [[:alnum:]_-]+:$/ && in_job { in_job = 0 }
  in_job && /fetch-depth: 0/ { found = 1 }
  END { exit(found ? 0 : 1) }
' "$repo_root/.github/workflows/backend-ci.yml" ||
  fail "golangci-lint checkout does not fetch the explicit baseline history"
grep -Fq 'git merge-base --is-ancestor "$CUSTOM_UPSTREAM_BASE_COMMIT" HEAD' \
  "$repo_root/.github/workflows/backend-ci.yml" ||
  fail "golangci-lint does not verify that the explicit baseline is an ancestor"
grep -Fq -- '--new-from-rev "$CUSTOM_UPSTREAM_BASE_COMMIT"' \
  "$repo_root/.github/workflows/backend-ci.yml" ||
  fail "golangci-lint is not scoped to changes from the explicit custom baseline"

backend_ci="$repo_root/.github/workflows/backend-ci.yml"
security_scan="$repo_root/.github/workflows/security-scan.yml"
publish_workflow="$repo_root/.github/workflows/publish-custom.yml"
upgrade_gate="$repo_root/.github/workflows/upstream-upgrade-gate.yml"

for workflow in "$backend_ci" "$security_scan"; do
  [[ "$(trigger_branches "$workflow" push)" == "main" ]] ||
    fail "feature branches must not duplicate PR checks through push: $workflow"
  grep -Fqx '  pull_request:' "$workflow" ||
    fail "pull request validation trigger is missing: $workflow"
done
[[ "$(trigger_branches "$publish_workflow" workflow_run)" == "main" ]] ||
  fail "automatic publication must only react to completed main CI runs"

grep -Fq \
  'group: ci-${{ github.event_name == '\''pull_request'\'' && github.event.pull_request.number || github.run_id }}' \
  "$backend_ci" || fail "CI does not isolate concurrency to each pull request"
grep -Fq \
  'group: security-scan-${{ github.event_name == '\''pull_request'\'' && github.event.pull_request.number || github.run_id }}' \
  "$security_scan" || fail "Security Scan does not isolate concurrency to each pull request"
for workflow in "$backend_ci" "$security_scan"; do
  grep -Fq 'cancel-in-progress: ${{ github.event_name == '\''pull_request'\'' }}' "$workflow" ||
    fail "stale pull request runs are not cancelled: $workflow"
done

for job in backend_unit backend_integration backend_build test; do
  grep -Fq "  ${job}:" "$backend_ci" ||
    fail "parallel backend validation job is missing: $job"
done
[[ "$(grep -Fc 'run: make test-unit' "$backend_ci")" -eq 1 ]] ||
  fail "backend unit tests must run exactly once"
[[ "$(grep -Fc 'run: /bin/bash deploy/tests/backend-integration-test.sh' "$backend_ci")" -eq 1 ]] ||
  fail "backend integration tests must run exactly once"
[[ "$(grep -Fc 'run: make build-backend' "$backend_ci")" -eq 1 ]] ||
  fail "backend production build must run exactly once"
grep -Fq 'needs: [backend_unit, backend_integration, backend_build]' "$backend_ci" ||
  fail "required test check does not aggregate all backend validation paths"
grep -Fq '    if: ${{ always() }}' "$backend_ci" ||
  fail "required test check does not run after a failed or skipped backend path"
grep -Fq 'BACKEND_UNIT_RESULT: ${{ needs.backend_unit.result }}' "$backend_ci" ||
  fail "required test check does not inspect the unit result"
grep -Fq 'BACKEND_INTEGRATION_RESULT: ${{ needs.backend_integration.result }}' "$backend_ci" ||
  fail "required test check does not inspect the integration result"
grep -Fq 'BACKEND_BUILD_RESULT: ${{ needs.backend_build.result }}' "$backend_ci" ||
  fail "required test check does not inspect the build result"
grep -Fq '[[ "$BACKEND_UNIT_RESULT" == "success" ]]' "$backend_ci" ||
  fail "required test check does not fail closed on unit failure"
grep -Fq '[[ "$BACKEND_INTEGRATION_RESULT" == "success" ]]' "$backend_ci" ||
  fail "required test check does not fail closed on integration failure"
grep -Fq '[[ "$BACKEND_BUILD_RESULT" == "success" ]]' "$backend_ci" ||
  fail "required test check does not fail closed on build failure"

for job in boundaries shell backend_unit backend_integration backend_build frontend golangci-lint; do
  grep -Fq '    needs: verify-target' <<<"$(job_block "$backend_ci" "$job")" ||
    fail "CI job must start after exact-target resolution without waiting for boundaries: $job"
done
grep -Fq "if: github.event_name == 'push' || github.event_name == 'workflow_dispatch'" \
  "$backend_ci" || fail "frontend Release Artifact is not limited to trusted main CI events"
grep -Fq 'name: release-frontend-dist-${{ needs.verify-target.outputs.checkout_sha }}' \
  "$backend_ci" || fail "frontend Release Artifact is not bound to the exact CI SHA"
grep -Fq 'path: backend/internal/web/dist/' "$backend_ci" ||
  fail "frontend Release Artifact does not contain the production dist"
grep -Fq 'retention-days: 30' "$backend_ci" ||
  fail "frontend Release Artifact retention is not fixed"

integration_fixture="$(mktemp -d)"
for package in \
  internal/custom/subscriptioninventory \
  internal/custom/subscriptionrepository \
  internal/middleware \
  internal/pkg/tlsfingerprint \
  internal/repository \
  internal/server/routes; do
  mkdir -p "$integration_fixture/$package"
  printf '%s\n' '//go:build integration' '' 'package fixture' \
    > "$integration_fixture/$package/integration_test.go"
done
integration_log="$integration_fixture/check.log"
/bin/bash "$integration_runner" check \
  --backend-root "$integration_fixture" > "$integration_log"
for package in \
  ./internal/custom/subscriptioninventory \
  ./internal/custom/subscriptionrepository \
  ./internal/middleware \
  ./internal/pkg/tlsfingerprint \
  ./internal/repository \
  ./internal/server/routes; do
  grep -Fq "  $package" "$integration_log" ||
    fail "Integration contract omitted registered package: $package"
done
if grep -Fq './internal/service' "$integration_log"; then
  fail "package without Integration-tagged tests leaked into the Integration contract"
fi
mkdir -p "$integration_fixture/internal/newintegration"
printf '%s\n' '//go:build integration' '' 'package newintegration' \
  > "$integration_fixture/internal/newintegration/new_integration_test.go"
if /bin/bash "$integration_runner" check \
  --backend-root "$integration_fixture" >/dev/null 2>&1; then
  fail "an unregistered Integration-tagged package was accepted"
fi
rm -rf "$integration_fixture/internal/newintegration"
printf '%s\n' '// +build integration' '' 'package middleware' \
  > "$integration_fixture/internal/middleware/legacy_integration_test.go"
if /bin/bash "$integration_runner" check \
  --backend-root "$integration_fixture" >/dev/null 2>&1; then
  fail "a legacy Integration constraint without the exact go:build contract was accepted"
fi
rm -rf "$integration_fixture"

upgrade_backend_job="$(
  awk '
    /^  backend:$/ { in_job = 1 }
    /^  [[:alnum:]_-]+:$/ && in_job && $0 !~ /^  backend:$/ { exit }
    in_job { print }
  ' "$upgrade_gate"
)"
grep -Fq 'fetch-depth: 0' <<<"$upgrade_backend_job" ||
  fail "upgrade golangci-lint checkout does not fetch the explicit baseline history"
grep -Fq 'source ../.github/custom-upstream-baseline.env' <<<"$upgrade_backend_job" ||
  fail "upgrade golangci-lint does not load the explicit custom baseline"
grep -Fq 'git merge-base --is-ancestor "$CUSTOM_UPSTREAM_BASE_COMMIT" HEAD' \
  <<<"$upgrade_backend_job" ||
  fail "upgrade golangci-lint does not verify that the explicit baseline is an ancestor"
grep -Fq -- '--new-from-rev "$CUSTOM_UPSTREAM_BASE_COMMIT"' <<<"$upgrade_backend_job" ||
  fail "upgrade golangci-lint is not scoped to changes from the explicit custom baseline"
grep -Fq 'is_official_upgrade: ${{ steps.resolve.outputs.is_official_upgrade }}' \
  "$backend_ci" ||
  fail "CI does not expose exact official-upgrade classification"
grep -Fq 'CANDIDATE_REF: ${{ github.head_ref || github.ref_name }}' \
  "$backend_ci" ||
  fail "CI does not classify the exact pull request or pushed branch"
grep -Fq 'if [[ "$CANDIDATE_REF" =~ ^upgrade/v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then' \
  "$backend_ci" ||
  fail "CI official-upgrade branch classification is not exact"
[[ "$(grep -Fc "if: needs.verify-target.outputs.is_official_upgrade != 'true'" "$backend_ci")" -eq 2 ]] ||
  fail "CI must defer exactly the final delta and database checks for official upgrades"
grep -Fq "if: needs.verify-target.outputs.is_official_upgrade == 'true'" \
  "$backend_ci" ||
  fail "CI does not record delegation to the trusted official-upgrade gate"
grep -Fq "context='Required upgrade validation'" "$upgrade_gate" ||
  fail "trusted official-upgrade validation does not publish the required exact-head status"

for workflow in \
  "$repo_root/.github/workflows/backend-ci.yml" \
  "$repo_root/.github/workflows/security-scan.yml" \
  "$repo_root/.github/workflows/upstream-upgrade-gate.yml"; do
  grep -Fq "version: ${PNPM_VERSION}" "$workflow" ||
    fail "pnpm Action input does not match the tool manifest: $workflow"
  grep -Fq "node-version: '${NODE_VERSION}'" "$workflow" ||
    fail "Node Action input does not match the tool manifest: $workflow"
done
grep -Fq 'install-custom-tools.sh \' "$repo_root/.github/workflows/release.yml"
grep -Fq 'node,pnpm,goreleaser,oras,buildx' "$repo_root/.github/workflows/release.yml"

for workflow in \
  "$repo_root/.github/workflows/backend-ci.yml" \
  "$repo_root/.github/workflows/publish-custom.yml" \
  "$repo_root/.github/workflows/release.yml" \
  "$repo_root/.github/workflows/upstream-sync.yml" \
  "$repo_root/.github/workflows/upstream-upgrade-gate.yml"; do
  [[ "$(grep -Fc 'fetch-depth: 0' "$workflow")" -eq \
     "$(grep -Fc 'filter: blob:none' "$workflow")" ]] ||
    fail "full-history checkout does not consistently use blob filtering: $workflow"
done
for workflow in \
  "$repo_root/.github/workflows/publish-custom.yml" \
  "$repo_root/.github/workflows/upstream-sync.yml" \
  "$repo_root/.github/workflows/upstream-upgrade-gate.yml"; do
  [[ "$(grep -Ec '^[[:space:]]+git fetch (origin|upstream) \\' "$workflow")" -eq \
     "$(grep -Fc -- '--filter=blob:none \' "$workflow")" ]] ||
    fail "manual full-history fetch does not consistently use blob filtering: $workflow"
done

if grep -RInE \
  'github\.com/.+/releases/download/|nodejs\.org/dist/|registry\.npmjs\.org/.+\.tgz' \
  "$repo_root/.github/workflows" \
  "$repo_root/deploy/release"; then
  fail "dynamic tool downloads must be declared only in the checksum-pinned tool manifest"
fi

for workflow in "${workflows[@]}"; do
  [[ -s "$workflow" ]] || fail "custom workflow is missing: $workflow"
  while IFS= read -r action_ref; do
    action_ref="${action_ref#*uses:}"
    action_ref="${action_ref%%#*}"
    action_ref="${action_ref//[[:space:]]/}"
    [[ "$action_ref" =~ ^[^@]+@[0-9a-f]{40}$ ]] ||
      fail "third-party Action is not pinned to a full commit: $workflow: $action_ref"
  done < <(grep -E '^[[:space:]]+-?[[:space:]]*uses:' "$workflow")
done

if command -v actionlint >/dev/null 2>&1; then
  actionlint "${workflows[@]}"

  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' EXIT
  expect_failure() {
    local description="$1"
    local fixture="$2"
    if actionlint "$fixture" >/dev/null 2>&1; then
      fail "actionlint accepted $description"
    fi
  }

  cat > "$tmp_dir/invalid-field.yml" <<'EOF'
name: invalid-field
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    invalid-field: true
    steps:
      - run: 'true'
EOF
  expect_failure "an invalid job field" "$tmp_dir/invalid-field.yml"

  cat > "$tmp_dir/invalid-expression.yml" <<'EOF'
name: invalid-expression
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo '${{ github.does_not_exist }}'
EOF
  expect_failure "an invalid expression property" "$tmp_dir/invalid-expression.yml"

  cat > "$tmp_dir/missing-needs.yml" <<'EOF'
name: missing-needs
on: push
jobs:
  test:
    needs: missing
    runs-on: ubuntu-latest
    steps:
      - run: 'true'
EOF
  expect_failure "a missing needs target" "$tmp_dir/missing-needs.yml"

  cat > "$tmp_dir/cyclic-needs.yml" <<'EOF'
name: cyclic-needs
on: push
jobs:
  first:
    needs: second
    runs-on: ubuntu-latest
    steps:
      - run: 'true'
  second:
    needs: first
    runs-on: ubuntu-latest
    steps:
      - run: 'true'
EOF
  expect_failure "a cyclic needs graph" "$tmp_dir/cyclic-needs.yml"
elif [[ "${CUSTOM_ACTIONS_REQUIRE_ACTIONLINT:-false}" == "true" ]]; then
  fail "actionlint is required but unavailable"
else
  echo "actionlint not found; static supply-chain checks passed, semantic check deferred"
fi

echo "custom Actions supply-chain checks passed"
