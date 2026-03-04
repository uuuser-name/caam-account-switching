#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

max_uncovered="${MAX_UNCOVERED_SCENARIOS:-153}"
min_covered="${MIN_COVERED_SCENARIOS:-74}"
traceability_json="artifacts/cli-matrix/scenario_traceability.json"
summary_json="artifacts/test-audit/e2e_quality_gate.json"

run_stage() {
  local stage="$1"
  shift
  echo "== ${stage} =="
  "$@"
}

run_stage "Schema fixture gate" ./scripts/validate_e2e_log_fixtures.sh
run_stage "Failure-packet pipeline gate" ./scripts/validate_failure_packet_pipeline.sh
run_stage "Dry/live parity fixture gate" ./scripts/validate_e2e_dry_live_parity_fixtures.sh
run_stage "Scenario traceability generation" ./scripts/build_cli_scenario_traceability.sh

if [[ ! -f "${traceability_json}" ]]; then
  echo "missing scenario traceability artifact: ${traceability_json}" >&2
  exit 1
fi

required="$(jq -r '.totals.required_scenarios // .totals.required // 0' "${traceability_json}")"
covered="$(jq -r '.totals.covered // 0' "${traceability_json}")"
uncovered="$(jq -r '.totals.uncovered // 0' "${traceability_json}")"
coverage_pct="$(awk -v c="${covered}" -v r="${required}" 'BEGIN { if (r <= 0) { print "0.00" } else { printf("%.2f", (c/r)*100) } }')"

pass=true
if (( covered < min_covered )); then
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
  --arg pass "${pass}" '
  {
    generated_at: $generated_at,
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
