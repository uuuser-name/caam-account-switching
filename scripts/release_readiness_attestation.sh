#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

audit_json="${1:-artifacts/test-audit/test_audit.json}"
gate_json="${2:-artifacts/test-audit/e2e_quality_gate.json}"
trend_json="${3:-artifacts/test-audit/quality_trend_diff.json}"
min_coverage="${RELEASE_MIN_AGG_COVERAGE:-60}"

out_json="artifacts/test-audit/release_attestation.json"
out_md="artifacts/test-audit/release_attestation.md"

for path in "$audit_json" "$gate_json" "$trend_json"; do
  if [[ ! -f "$path" ]]; then
    echo "missing required attestation input: $path" >&2
    exit 1
  fi
done

mock_violations="$(jq -r '.mock_fake_stub_violations // 999999' "$audit_json")"
aggregate_coverage="$(jq -r '.aggregate_total_coverage // 0' "$audit_json")"
gate_pass="$(jq -r '.pass // false' "$gate_json")"
required_scenarios="$(jq -r '.totals.required // 0' "$gate_json")"
covered_scenarios="$(jq -r '.totals.covered // 0' "$gate_json")"
uncovered_scenarios="$(jq -r '.totals.uncovered // 0' "$gate_json")"
trend_status="$(jq -r '.status // "unknown"' "$trend_json")"

check_no_mock="false"
if (( mock_violations == 0 )); then
  check_no_mock="true"
fi

check_gate_pass="false"
if [[ "$gate_pass" == "true" ]]; then
  check_gate_pass="true"
fi

check_trend_ok="false"
if [[ "$trend_status" != "regression" ]]; then
  check_trend_ok="true"
fi

check_traceability_totals="false"
if (( required_scenarios > 0 )) && (( covered_scenarios + uncovered_scenarios == required_scenarios )); then
  check_traceability_totals="true"
fi

check_coverage_floor="false"
if awk -v c="$aggregate_coverage" -v f="$min_coverage" 'BEGIN { exit !(c >= f) }'; then
  check_coverage_floor="true"
fi

pass="true"
for check in "$check_no_mock" "$check_gate_pass" "$check_trend_ok" "$check_traceability_totals" "$check_coverage_floor"; do
  if [[ "$check" != "true" ]]; then
    pass="false"
    break
  fi
done

mkdir -p "$(dirname "$out_json")"
jq -n \
  --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg audit_json "$audit_json" \
  --arg gate_json "$gate_json" \
  --arg trend_json "$trend_json" \
  --argjson mock_violations "$mock_violations" \
  --argjson aggregate_coverage "$aggregate_coverage" \
  --argjson min_coverage "$min_coverage" \
  --arg gate_pass "$gate_pass" \
  --arg trend_status "$trend_status" \
  --argjson required_scenarios "$required_scenarios" \
  --argjson covered_scenarios "$covered_scenarios" \
  --argjson uncovered_scenarios "$uncovered_scenarios" \
  --arg check_no_mock "$check_no_mock" \
  --arg check_gate_pass "$check_gate_pass" \
  --arg check_trend_ok "$check_trend_ok" \
  --arg check_traceability_totals "$check_traceability_totals" \
  --arg check_coverage_floor "$check_coverage_floor" \
  --arg pass "$pass" '
  {
    generated_at: $generated_at,
    inputs: {
      audit_json: $audit_json,
      gate_json: $gate_json,
      trend_json: $trend_json
    },
    metrics: {
      mock_fake_stub_violations: $mock_violations,
      aggregate_coverage: $aggregate_coverage,
      min_required_aggregate_coverage: $min_coverage,
      e2e_quality_gate_pass: ($gate_pass == "true"),
      trend_status: $trend_status,
      required_scenarios: $required_scenarios,
      covered_scenarios: $covered_scenarios,
      uncovered_scenarios: $uncovered_scenarios
    },
    checks: [
      {id: "no_mock_violations", pass: ($check_no_mock == "true")},
      {id: "e2e_quality_gate_pass", pass: ($check_gate_pass == "true")},
      {id: "trend_not_regression", pass: ($check_trend_ok == "true")},
      {id: "traceability_totals_consistent", pass: ($check_traceability_totals == "true")},
      {id: "aggregate_coverage_floor", pass: ($check_coverage_floor == "true")}
    ],
    pass: ($pass == "true")
  }
' > "$out_json"

{
  echo "# Release Readiness Attestation"
  echo
  echo "- Generated (UTC): $(jq -r '.generated_at' "$out_json")"
  echo "- Pass: $(jq -r '.pass' "$out_json")"
  echo
  echo "## Metrics"
  echo "- Mock/fake/stub violations: $(jq -r '.metrics.mock_fake_stub_violations' "$out_json")"
  echo "- Aggregate coverage: $(jq -r '.metrics.aggregate_coverage' "$out_json")%"
  echo "- Coverage floor: $(jq -r '.metrics.min_required_aggregate_coverage' "$out_json")%"
  echo "- E2E quality gate pass: $(jq -r '.metrics.e2e_quality_gate_pass' "$out_json")"
  echo "- Trend status: $(jq -r '.metrics.trend_status' "$out_json")"
  echo "- Required/Covered/Uncovered scenarios: $(jq -r '.metrics.required_scenarios' "$out_json") / $(jq -r '.metrics.covered_scenarios' "$out_json") / $(jq -r '.metrics.uncovered_scenarios' "$out_json")"
  echo
  echo "## Checks"
  jq -r '.checks[] | "- " + .id + ": " + (if .pass then "PASS" else "FAIL" end)' "$out_json"
} > "$out_md"

echo "wrote ${out_json}"
echo "wrote ${out_md}"

if [[ "$pass" != "true" ]]; then
  echo "release readiness attestation failed" >&2
  exit 1
fi
