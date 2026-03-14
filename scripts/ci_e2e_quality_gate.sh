#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

max_uncovered="${MAX_UNCOVERED_SCENARIOS:-153}"
min_covered="${MIN_COVERED_SCENARIOS:-74}"
matrix_json="${E2E_QUALITY_GATE_MATRIX_JSON:-docs/testing/cli_workflow_matrix.json}"
inventory_json="${E2E_QUALITY_GATE_INVENTORY_JSON:-artifacts/test-audit/e2e_inventory.json}"
traceability_json="${E2E_QUALITY_GATE_TRACEABILITY_JSON:-artifacts/cli-matrix/scenario_traceability.json}"
summary_json="${E2E_QUALITY_GATE_SUMMARY_JSON:-artifacts/test-audit/e2e_quality_gate.json}"
schema_fixture_cmd="${E2E_QUALITY_GATE_SCHEMA_FIXTURE_CMD:-./scripts/validate_e2e_log_fixtures.sh}"
failure_packet_cmd="${E2E_QUALITY_GATE_FAILURE_PACKET_CMD:-./scripts/validate_failure_packet_pipeline.sh}"
parity_cmd="${E2E_QUALITY_GATE_PARITY_CMD:-./scripts/validate_e2e_dry_live_parity_fixtures.sh}"
traceability_cmd="${E2E_QUALITY_GATE_TRACEABILITY_CMD:-./scripts/build_cli_scenario_traceability.sh}"
matrix_mode="explicit_traceability"
failure_reason=""
failed_stage=""
tmp_stage_results="$(mktemp)"
trap 'rm -f "$tmp_stage_results"' EXIT
: >"${tmp_stage_results}"

append_stage_result() {
  local stage="$1"
  local status="$2"
  local exit_code="$3"
  local summary="$4"

  jq -cn \
    --arg stage "${stage}" \
    --arg status "${status}" \
    --argjson exit_code "${exit_code}" \
    --arg summary "${summary}" \
    '{
      stage: $stage,
      status: $status,
      exit_code: $exit_code,
      summary: (if $summary == "" then null else $summary end)
    }' >>"${tmp_stage_results}"
}

record_failure() {
  local reason="$1"
  local stage="$2"

  if [[ -z "${failure_reason}" ]]; then
    failure_reason="${reason}"
    failed_stage="${stage}"
  fi
}

run_stage() {
  local stage="$1"
  local failure_code="$2"
  shift 2

  echo "== ${stage} =="

  local output=""
  local exit_code=0
  set +e
  output="$("$@" 2>&1)"
  exit_code=$?
  set -e

  if [[ "${exit_code}" -eq 0 ]]; then
    if [[ -n "${output}" ]]; then
      printf '%s\n' "${output}"
    fi
    append_stage_result "${stage}" "passed" 0 "$(printf '%s\n' "${output}" | head -n1)"
    return 0
  fi

  if [[ -n "${output}" ]]; then
    printf '%s\n' "${output}" >&2
  fi
  append_stage_result "${stage}" "failed" "${exit_code}" "$(printf '%s\n' "${output}" | head -n1)"
  record_failure "${failure_code}" "${stage}"
  return 1
}

run_stage "Schema fixture gate" "schema_fixture_gate_failed" "${schema_fixture_cmd}" || true
run_stage "Failure-packet pipeline gate" "failure_packet_pipeline_failed" "${failure_packet_cmd}" || true
run_stage "Dry/live parity fixture gate" "dry_live_parity_fixture_gate_failed" "${parity_cmd}" || true

if [[ -f "${matrix_json}" ]]; then
  if [[ -f "${inventory_json}" ]]; then
    run_stage "Scenario traceability generation" "scenario_traceability_generation_failed" "${traceability_cmd}" "${matrix_json}" "${inventory_json}" "${traceability_json}" || true
  else
    matrix_mode="missing_e2e_inventory"
    append_stage_result "Scenario traceability generation" "skipped" 2 "missing e2e inventory artifact"
    record_failure "missing_e2e_inventory" "Scenario traceability generation"
    echo "missing e2e inventory artifact: ${inventory_json}" >&2
    echo "run ./scripts/test_audit.sh before generating scenario traceability" >&2
  fi
else
  matrix_mode="missing_cli_matrix"
  append_stage_result "Scenario traceability generation" "skipped" 2 "missing CLI workflow matrix artifact"
  record_failure "missing_cli_matrix" "Scenario traceability generation"
  echo "missing CLI workflow matrix artifact: ${matrix_json}" >&2
  echo "restore docs/testing/cli_workflow_matrix.json or pass E2E_QUALITY_GATE_MATRIX_JSON explicitly" >&2
fi

if [[ -z "${failure_reason}" && ! -f "${traceability_json}" ]]; then
  record_failure "missing_traceability_artifact" "Scenario traceability generation"
  echo "missing scenario traceability artifact: ${traceability_json}" >&2
fi

required=0
covered=0
uncovered=0
coverage_pct="0.00"
traceability_totals=""
if [[ -z "${failure_reason}" ]]; then
  if ! traceability_totals="$(jq -ce '
    .totals as $totals
    | {
        required: ($totals.required_scenarios // $totals.required // null),
        covered: ($totals.covered // null),
        uncovered: ($totals.uncovered // null)
      }
    | select(
        (.required | type == "number" and . >= 0 and floor == .)
        and (.covered | type == "number" and . >= 0 and floor == .)
        and (.uncovered | type == "number" and . >= 0 and floor == .)
        and (.covered + .uncovered == .required)
      )
  ' "${traceability_json}" 2>/dev/null)"; then
    record_failure "invalid_traceability_artifact" "Scenario traceability generation"
    echo "invalid scenario traceability totals: ${traceability_json}" >&2
  else
    required="$(jq -r '.required' <<<"${traceability_totals}")"
    covered="$(jq -r '.covered' <<<"${traceability_totals}")"
    uncovered="$(jq -r '.uncovered' <<<"${traceability_totals}")"
    coverage_pct="$(awk -v c="${covered}" -v r="${required}" 'BEGIN { if (r <= 0) { print "0.00" } else { printf("%.2f", (c/r)*100) } }')"
  fi
fi

# Avoid impossible floors when the required-scenario universe is smaller
# than the default threshold (common in local-first/fallback mode).
if [[ -z "${failure_reason}" ]] && (( required < min_covered )); then
  min_covered="${required}"
fi

pass=true
if [[ -n "${failure_reason}" ]]; then
  pass=false
elif (( covered < min_covered )); then
  echo "covered scenarios below floor: covered=${covered}, floor=${min_covered}" >&2
  record_failure "covered_below_floor" "Coverage thresholds"
  pass=false
fi
if (( uncovered > max_uncovered )); then
  echo "uncovered scenarios exceeded cap: uncovered=${uncovered}, cap=${max_uncovered}" >&2
  if [[ -z "${failure_reason}" ]]; then
    record_failure "uncovered_above_cap" "Coverage thresholds"
  fi
  pass=false
fi

mkdir -p "$(dirname "${summary_json}")"
jq -n \
  --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --slurpfile stage_results "${tmp_stage_results}" \
  --argjson required "${required}" \
  --argjson covered "${covered}" \
  --argjson uncovered "${uncovered}" \
  --arg coverage_pct "${coverage_pct}" \
  --argjson min_covered "${min_covered}" \
  --argjson max_uncovered "${max_uncovered}" \
  --arg matrix_mode "${matrix_mode}" \
  --arg failure_reason "${failure_reason}" \
  --arg failed_stage "${failed_stage}" \
  --arg pass "${pass}" '
  {
    generated_at: $generated_at,
    matrix_mode: $matrix_mode,
    failure_reason: (if $failure_reason == "" then null else $failure_reason end),
    failed_stage: (if $failed_stage == "" then null else $failed_stage end),
    stage_results: $stage_results,
    thresholds: {
      min_covered: $min_covered,
      max_uncovered: $max_uncovered
    },
    totals: {
      required: $required,
      covered: $covered,
      uncovered: $uncovered,
      coverage_pct: ($coverage_pct | tonumber)
    },
    pass: ($pass == "true")
  }
' > "${summary_json}"

if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  {
    echo "### E2E Quality Gate"
    echo
    if [[ -n "${failure_reason}" ]]; then
      echo "- Failure reason: ${failure_reason}"
      echo "- Failed stage: ${failed_stage:-n/a}"
    fi
    echo "- Required scenarios: ${required}"
    echo "- Covered scenarios: ${covered}"
    echo "- Uncovered scenarios: ${uncovered}"
    echo "- Coverage ratio: ${coverage_pct}%"
    echo "- Thresholds: covered >= ${min_covered}, uncovered <= ${max_uncovered}"
    echo "- Result: $([[ "${pass}" == "true" ]] && echo "PASS" || echo "FAIL")"
  } >> "${GITHUB_STEP_SUMMARY}"
fi

if [[ "${pass}" != "true" ]]; then
  exit 1
fi

echo "E2E quality gate passed (covered=${covered}, uncovered=${uncovered})"
