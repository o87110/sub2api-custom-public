#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  cat >&2 <<'EOF'
usage: wait-for-required-ci.sh \
  --repo <owner/repository> \
  --workflow <workflow-file> \
  --sha <40-character-commit> \
  [--wait]
EOF
  exit 2
}

repository=""
workflow=""
target_sha=""
wait_for_completion=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo) repository="${2:-}"; shift 2 ;;
    --workflow) workflow="${2:-}"; shift 2 ;;
    --sha) target_sha="${2:-}"; shift 2 ;;
    --wait) wait_for_completion=true; shift ;;
    *) usage ;;
  esac
done

[[ "$repository" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || usage
[[ "$workflow" =~ ^[A-Za-z0-9_.-]+\.ya?ml$ ]] || usage
[[ "$target_sha" =~ ^[0-9a-f]{40}$ ]] || usage
command -v gh >/dev/null 2>&1 || fail "GitHub CLI is unavailable"

max_attempts="${PUBLISH_CI_MAX_ATTEMPTS:-80}"
poll_seconds="${PUBLISH_CI_POLL_SECONDS:-15}"
[[ "$max_attempts" =~ ^[1-9][0-9]*$ && "$max_attempts" -le 240 ]] ||
  fail "PUBLISH_CI_MAX_ATTEMPTS must be between 1 and 240"
[[ "$poll_seconds" =~ ^[0-9]+$ && "$poll_seconds" -le 60 ]] ||
  fail "PUBLISH_CI_POLL_SECONDS must be between 0 and 60"

query_run() {
  gh run list \
    --repo "$repository" \
    --workflow "$workflow" \
    --commit "$target_sha" \
    --limit 20 \
    --json databaseId,status,conclusion,headSha,headBranch,event \
    --jq '
      map(select(
        (.event == "push" or .event == "workflow_dispatch") and
        .headBranch == "main" and
        .headSha == "'"$target_sha"'"
      )) |
      if length == 0 then
        ""
      else
        .[0] |
        [
          .databaseId,
          .status,
          (if ((.conclusion // "") == "") then "pending" else .conclusion end),
          .headSha,
          .headBranch,
          .event
        ] |
        @tsv
      end
    '
}

for ((attempt = 1; attempt <= max_attempts; attempt++)); do
  run_state="$(query_run)"
  if [[ -n "$run_state" ]]; then
    IFS=$'\t' read -r \
      run_id status conclusion head_sha head_branch event_type \
      <<<"$run_state"
    [[ "$run_id" =~ ^[0-9]+$ ]] ||
      fail "required workflow returned an invalid run ID"
    [[ "$head_sha" == "$target_sha" && "$head_branch" == "main" ]] ||
      fail "required workflow resolved an unexpected revision"

    if [[ "$status" == "completed" ]]; then
      [[ "$conclusion" == "success" ]] ||
        fail "required workflow ${workflow} (${event_type}) completed as ${conclusion}"
      boundary_state="$(
        gh run view "$run_id" \
          --repo "$repository" \
          --json jobs \
          --jq '
            .jobs |
            map(select(.name == "boundaries")) |
            if length == 1 then
              [
                .[0].status,
                (if ((.[0].conclusion // "") == "") then
                  "pending"
                else
                  .[0].conclusion
                end)
              ] |
              @tsv
            else
              ""
            end
          '
      )"
      [[ -n "$boundary_state" ]] ||
        fail "required boundaries job is missing in CI run ${run_id}"
      IFS=$'\t' read -r boundary_status boundary_conclusion \
        <<<"$boundary_state"
      [[ "$boundary_status" == "completed" &&
         "$boundary_conclusion" == "success" ]] ||
        fail "required boundaries job is ${boundary_status}/${boundary_conclusion} in CI run ${run_id}"
      full_validation_state="$(
        gh run view "$run_id" \
          --repo "$repository" \
          --json jobs \
          --jq '
            .jobs |
            map(select(.name == "Full validation")) |
            if length == 1 then [.[0].status, (.[0].conclusion // "pending")] | @tsv else "" end
          '
      )"
      [[ -n "$full_validation_state" ]] ||
        fail "required full-validation job is missing in CI run ${run_id}"
      IFS=$'\t' read -r full_validation_status full_validation_conclusion <<<"$full_validation_state"
      [[ "$full_validation_status" == "completed" &&
         "$full_validation_conclusion" == "success" ]] ||
        fail "required full-validation job is ${full_validation_status}/${full_validation_conclusion} in CI run ${run_id}"
      printf '%s\n' "$run_id"
      exit 0
    fi

    if [[ "$wait_for_completion" != "true" ]]; then
      fail "required workflow ${workflow} (${event_type}) is ${status}/${conclusion}"
    fi
    echo \
      "Waiting for ${workflow} run ${run_id} at ${target_sha} (${status}/${conclusion}), attempt ${attempt}/${max_attempts}." \
      >&2
  else
    if [[ "$wait_for_completion" != "true" ]]; then
      fail "required workflow ${workflow} has no accepted run for ${target_sha}"
    fi
    echo \
      "Waiting for ${workflow} at ${target_sha}, attempt ${attempt}/${max_attempts}." \
      >&2
  fi

  [[ "$attempt" -lt "$max_attempts" ]] ||
    fail "timed out waiting for ${workflow} success at ${target_sha}"
  sleep "$poll_seconds"
done

fail "unreachable CI wait state"
