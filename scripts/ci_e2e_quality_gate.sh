#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

max_uncovered="${MAX_UNCOVERED_SCENARIOS:-153}"
min_covered="${MIN_COVERED_SCENARIOS:-74}"
matrix_json="artifacts/cli-matrix/cli_workflow_matrix.json"
inventory_json="artifacts/test-audit/e2e_inventory.json"
traceability_json="artifacts/cli-matrix/scenario_traceability.json"
summary_json="artifacts/test-audit/e2e_quality_gate.json"
matrix_mode="explicit_traceability"
failure_reason=""

run_stage() {
  local stage="$1"
  shift
  echo "== ${stage} =="
  "$@"
}

run_stage "Schema fixture gate" ./scripts/validate_e2e_log_fixtures.sh
run_stage "Failure-packet pipeline gate" ./scripts/validate_failure_packet_pipeline.sh
run_stage "Dry/live parity fixture gate" ./scripts/validate_e2e_dry_live_parity_fixtures.sh

if [[ -f "${matrix_json}" ]]; then
  run_stage "Scenario traceability generation" ./scripts/build_cli_scenario_traceability.sh
else
  matrix_mode="missing_cli_matrix"
  failure_reason="missing_cli_matrix"
  echo "missing CLI workflow matrix artifact: ${matrix_json}" >&2
  echo "run ./scripts/build_cli_scenario_traceability.sh after generating the matrix artifact" >&2
fi

if [[ -z "${failure_reason}" && ! -f "${traceability_json}" ]]; then
  failure_reason="missing_traceability_artifact"
  echo "missing scenario traceability artifact: ${traceability_json}" >&2
fi

required=0
covered=0
uncovered=0
coverage_pct="0.00"
if [[ -z "${failure_reason}" ]]; then
  required="$(jq -r '.totals.required_scenarios // .totals.required // 0' "${traceability_json}")"
  covered="$(jq -r '.totals.covered // 0' "${traceability_json}")"
  uncovered="$(jq -r '.totals.uncovered // 0' "${traceability_json}")"
  coverage_pct="$(awk -v c="${covered}" -v r="${required}" 'BEGIN { if (r <= 0) { print "0.00" } else { printf("%.2f", (c/r)*100) } }')"
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
  pass=false
fi
if (( uncovered > max_uncovered )); then
  echo "uncovered scenarios exceeded cap: uncovered=${uncovered}, cap=${max_uncovered}" >&2
  pass=false
fi

mkdir -p "$(dirname "${summary_json}")"
jq -n \
  --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --argjson required "${required}" \
  --argjson covered "${covered}" \
  --argjson uncovered "${uncovered}" \
  --arg coverage_pct "${coverage_pct}" \
  --argjson min_covered "${min_covered}" \
  --argjson max_uncovered "${max_uncovered}" \
  --arg matrix_mode "${matrix_mode}" \
  --arg failure_reason "${failure_reason}" \
  --arg pass "${pass}" '
  {
    generated_at: $generated_at,
    matrix_mode: $matrix_mode,
    failure_reason: (if $failure_reason == "" then null else $failure_reason end),
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
