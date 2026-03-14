#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

manifest_json="${RUN_ARTIFACT_INDEX_PATH:-artifacts/test-audit/run_artifact_index.json}"
if (( $# == 0 )) && [[ "${RUN_ARTIFACT_INDEX_AUTO_REFRESH:-1}" == "1" ]]; then
  RUN_ARTIFACT_INDEX_OUT_JSON="${manifest_json}" ./scripts/build_run_artifact_index.sh >/dev/null
fi

installed_validation_json="${RELEASE_INSTALLED_VALIDATION_JSON:-}"
bounded_live_json="${RELEASE_BOUNDED_LIVE_VALIDATION_JSON:-}"

if (( $# == 0 )) && [[ -f "${manifest_json}" ]]; then
  audit_json="$(jq -r '.consumer_inputs.release_readiness_attestation.audit_json' "${manifest_json}")"
  gate_json="$(jq -r '.consumer_inputs.release_readiness_attestation.gate_json' "${manifest_json}")"
  trend_json="$(jq -r '.consumer_inputs.release_readiness_attestation.trend_json' "${manifest_json}")"
  manifest_installed_validation_json="$(jq -r '.consumer_inputs.release_readiness_attestation.installed_validation_json // empty' "${manifest_json}")"
  manifest_bounded_live_json="$(jq -r '.consumer_inputs.release_readiness_attestation.bounded_live_json // empty' "${manifest_json}")"
  if [[ -n "${manifest_installed_validation_json}" ]]; then
    installed_validation_json="${manifest_installed_validation_json}"
  fi
  if [[ -n "${manifest_bounded_live_json}" ]]; then
    bounded_live_json="${manifest_bounded_live_json}"
  fi
else
  audit_json="${1:-artifacts/test-audit/test_audit.json}"
  gate_json="${2:-artifacts/test-audit/e2e_quality_gate.json}"
  trend_json="${3:-artifacts/test-audit/quality_trend_diff.json}"
fi
min_coverage="${RELEASE_MIN_AGG_COVERAGE:-60}"

out_json="${RELEASE_ATTESTATION_OUT_JSON:-artifacts/test-audit/release_attestation.json}"
out_md="${RELEASE_ATTESTATION_OUT_MD:-artifacts/test-audit/release_attestation.md}"

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

installed_binary_label="not_run"
if [[ -f "$installed_validation_json" ]]; then
  installed_binary_label="$(jq -r '.truth_label // (if (.pass // false) then "installed_binary_green" else "failed" end)' "$installed_validation_json")"
fi
case "$installed_binary_label" in
  installed_binary_green|failed|not_run) ;;
  *)
    echo "invalid installed binary truth label: $installed_binary_label" >&2
    exit 1
    ;;
esac

bounded_live_label="not_run"
if [[ -f "$bounded_live_json" ]]; then
  bounded_live_label="$(jq -r '.truth_label // (if (.pass // false) then "bounded_live_green" else "failed" end)' "$bounded_live_json")"
fi
case "$bounded_live_label" in
  bounded_live_green|failed|not_run) ;;
  *)
    echo "invalid bounded live truth label: $bounded_live_label" >&2
    exit 1
    ;;
esac

pass="true"
for check in "$check_no_mock" "$check_gate_pass" "$check_trend_ok" "$check_traceability_totals" "$check_coverage_floor"; do
  if [[ "$check" != "true" ]]; then
    pass="false"
    break
  fi
done

full_closure_pass="false"
if [[ "$pass" == "true" ]] && [[ "$installed_binary_label" == "installed_binary_green" ]] && [[ "$bounded_live_label" == "bounded_live_green" ]]; then
  full_closure_pass="true"
fi

deterministic_truth_label="failed"
if [[ "$pass" == "true" ]]; then
  deterministic_truth_label="deterministic_green"
fi

mkdir -p "$(dirname "$out_json")"
jq -n \
  --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg audit_json "$audit_json" \
  --arg gate_json "$gate_json" \
  --arg trend_json "$trend_json" \
  --arg installed_validation_json "$installed_validation_json" \
  --arg bounded_live_json "$bounded_live_json" \
  --arg installed_binary_label "$installed_binary_label" \
  --arg bounded_live_label "$bounded_live_label" \
  --arg deterministic_truth_label "$deterministic_truth_label" \
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
  --arg pass "$pass" \
  --arg full_closure_pass "$full_closure_pass" '
  {
    generated_at: $generated_at,
    inputs: {
      audit_json: $audit_json,
      gate_json: $gate_json,
      trend_json: $trend_json,
      installed_validation_json: (if $installed_validation_json == "" then null else $installed_validation_json end),
      bounded_live_json: (if $bounded_live_json == "" then null else $bounded_live_json end)
    },
    truth_labels: {
      deterministic: $deterministic_truth_label,
      installed_binary: $installed_binary_label,
      bounded_live: $bounded_live_label
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
    pass: ($pass == "true"),
    full_closure_pass: ($full_closure_pass == "true")
  }
' > "$out_json"

{
  echo "# Release Readiness Attestation"
  echo
  echo "- Generated (UTC): $(jq -r '.generated_at' "$out_json")"
  echo "- Deterministic pass: $(jq -r '.pass' "$out_json")"
  echo "- Full closure pass: $(jq -r '.full_closure_pass' "$out_json")"
  echo "- Deterministic truth label: $(jq -r '.truth_labels.deterministic' "$out_json")"
  echo "- Installed-binary truth label: $(jq -r '.truth_labels.installed_binary' "$out_json")"
  echo "- Bounded-live truth label: $(jq -r '.truth_labels.bounded_live' "$out_json")"
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
