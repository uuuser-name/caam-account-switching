#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

manifest_json="${RUN_ARTIFACT_INDEX_PATH:-artifacts/test-audit/run_artifact_index.json}"
if (( $# == 0 )) && [[ "${RUN_ARTIFACT_INDEX_AUTO_REFRESH:-1}" == "1" ]]; then
  RUN_ARTIFACT_INDEX_OUT_JSON="${manifest_json}" ./scripts/build_run_artifact_index.sh >/dev/null
fi

if (( $# == 0 )) && [[ -f "${manifest_json}" ]]; then
  audit_json="$(jq -r '.consumer_inputs.quality_trend_diff.audit_json' "${manifest_json}")"
  gate_json="$(jq -r '.consumer_inputs.quality_trend_diff.gate_json' "${manifest_json}")"
  traceability_json="$(jq -r '.consumer_inputs.quality_trend_diff.traceability_json' "${manifest_json}")"
  coverage_baseline_json="$(jq -r '.consumer_inputs.quality_trend_diff.coverage_baseline_json' "${manifest_json}")"
  traceability_baseline_json="$(jq -r '.consumer_inputs.quality_trend_diff.traceability_baseline_json' "${manifest_json}")"
else
  audit_json="${1:-artifacts/test-audit/test_audit.json}"
  gate_json="${2:-artifacts/test-audit/e2e_quality_gate.json}"
  traceability_json="${3:-artifacts/cli-matrix/scenario_traceability.json}"
  coverage_baseline_json="${4:-docs/testing/coverage_baseline.json}"
  traceability_baseline_json="${5:-docs/testing/e2e_traceability_baseline.json}"
fi
fail_on_regression="${FAIL_ON_QUALITY_REGRESSION:-0}"

out_json="${QUALITY_TREND_OUT_JSON:-artifacts/test-audit/quality_trend_diff.json}"
out_md="${QUALITY_TREND_OUT_MD:-artifacts/test-audit/quality_trend_diff.md}"

for path in "$audit_json" "$gate_json" "$traceability_json" "$coverage_baseline_json" "$traceability_baseline_json"; do
  if [[ ! -f "$path" ]]; then
    echo "missing required input: $path" >&2
    exit 1
  fi
done

current_cov="$(jq -r '.aggregate_total_coverage // 0' "$audit_json")"
current_violations="$(jq -r '.mock_fake_stub_violations // 0' "$audit_json")"
current_covered="$(jq -r '.totals.covered // 0' "$traceability_json")"
current_uncovered="$(jq -r '.totals.uncovered // 0' "$traceability_json")"
current_required="$(jq -r '.totals.required_scenarios // .totals.required // 0' "$traceability_json")"
current_gate_pass="$(jq -r '.pass // false' "$gate_json")"

baseline_cov="$(jq -r '.baseline_total_coverage // 0' "$coverage_baseline_json")"
baseline_covered="$(jq -r '.covered_scenarios // 0' "$traceability_baseline_json")"
baseline_uncovered="$(jq -r '.uncovered_scenarios // 0' "$traceability_baseline_json")"
baseline_required="$(jq -r '.required_scenarios // 0' "$traceability_baseline_json")"

cov_delta="$(awk -v c="$current_cov" -v b="$baseline_cov" 'BEGIN { printf "%.2f", c-b }')"
covered_delta="$(awk -v c="$current_covered" -v b="$baseline_covered" 'BEGIN { printf "%.0f", c-b }')"
uncovered_delta="$(awk -v c="$current_uncovered" -v b="$baseline_uncovered" 'BEGIN { printf "%.0f", c-b }')"
coverage_ratio="$(awk -v c="$current_covered" -v r="$current_required" 'BEGIN { if (r <= 0) { print "0.00" } else { printf "%.2f", (c/r)*100 } }')"

status="on_track"
regressions=()
if [[ "$current_gate_pass" != "true" ]]; then
  status="regression"
  regressions+=("quality_gate_failed")
fi
if awk -v d="$cov_delta" 'BEGIN { exit !(d < 0) }'; then
  status="regression"
  regressions+=("coverage_below_baseline")
fi
if (( covered_delta < 0 )); then
  status="regression"
  regressions+=("covered_scenarios_below_baseline")
fi
if (( uncovered_delta > 0 )); then
  status="regression"
  regressions+=("uncovered_scenarios_above_baseline")
fi

snapshot_prev=""
prev_cov=""
mapfile -t snapshot_audits < <(find artifacts/test-audit/snapshots -mindepth 2 -maxdepth 2 -type f -name test_audit.json 2>/dev/null | sort)
if (( ${#snapshot_audits[@]} >= 2 )); then
  snapshot_prev="${snapshot_audits[-2]}"
  prev_cov="$(jq -r '.aggregate_total_coverage // 0' "$snapshot_prev")"
fi

mkdir -p "$(dirname "$out_json")"
jq -n \
  --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg status "$status" \
  --argjson current_cov "$current_cov" \
  --argjson baseline_cov "$baseline_cov" \
  --argjson current_violations "$current_violations" \
  --argjson current_covered "$current_covered" \
  --argjson current_uncovered "$current_uncovered" \
  --argjson current_required "$current_required" \
  --argjson baseline_covered "$baseline_covered" \
  --argjson baseline_uncovered "$baseline_uncovered" \
  --argjson baseline_required "$baseline_required" \
  --arg cov_delta "$cov_delta" \
  --argjson covered_delta "$covered_delta" \
  --argjson uncovered_delta "$uncovered_delta" \
  --arg coverage_ratio "$coverage_ratio" \
  --arg current_gate_pass "$current_gate_pass" \
  --arg snapshot_prev "$snapshot_prev" \
  --arg prev_cov "$prev_cov" \
  --argjson regressions "$(printf '%s\n' "${regressions[@]-}" | jq -R -s 'split("\n") | map(select(length>0))')" '
  {
    generated_at: $generated_at,
    status: $status,
    current: {
      aggregate_coverage: $current_cov,
      mock_fake_stub_violations: $current_violations,
      covered_scenarios: $current_covered,
      uncovered_scenarios: $current_uncovered,
      required_scenarios: $current_required,
      scenario_coverage_pct: ($coverage_ratio | tonumber),
      quality_gate_pass: ($current_gate_pass == "true")
    },
    baseline: {
      aggregate_coverage: $baseline_cov,
      covered_scenarios: $baseline_covered,
      uncovered_scenarios: $baseline_uncovered,
      required_scenarios: $baseline_required
    },
    delta_from_baseline: {
      aggregate_coverage: ($cov_delta | tonumber),
      covered_scenarios: $covered_delta,
      uncovered_scenarios: $uncovered_delta
    },
    previous_snapshot: (
      if ($snapshot_prev | length) == 0 then null
      else {
        path: $snapshot_prev,
        aggregate_coverage: ($prev_cov | tonumber)
      }
      end
    ),
    regression_signals: $regressions
  }
' > "$out_json"

{
  echo "# Quality Trend Diff"
  echo
  echo "- Generated (UTC): $(jq -r '.generated_at' "$out_json")"
  echo "- Status: $(jq -r '.status' "$out_json")"
  echo
  echo "## Coverage"
  echo "- Current aggregate: $(jq -r '.current.aggregate_coverage' "$out_json")%"
  echo "- Baseline aggregate: $(jq -r '.baseline.aggregate_coverage' "$out_json")%"
  echo "- Delta vs baseline: $(jq -r '.delta_from_baseline.aggregate_coverage' "$out_json")%"
  echo
  echo "## Scenario Traceability"
  echo "- Covered: $(jq -r '.current.covered_scenarios' "$out_json") (baseline $(jq -r '.baseline.covered_scenarios' "$out_json"))"
  echo "- Uncovered: $(jq -r '.current.uncovered_scenarios' "$out_json") (baseline $(jq -r '.baseline.uncovered_scenarios' "$out_json"))"
  echo "- Coverage ratio: $(jq -r '.current.scenario_coverage_pct' "$out_json")%"
  echo
  echo "## Gate Health"
  echo "- E2E quality gate pass: $(jq -r '.current.quality_gate_pass' "$out_json")"
  echo "- Mock/fake/stub violations: $(jq -r '.current.mock_fake_stub_violations' "$out_json")"
  echo
  if [[ "$(jq -r '.regression_signals | length' "$out_json")" -gt 0 ]]; then
    echo "## Regression Signals"
    jq -r '.regression_signals[] | "- " + .' "$out_json"
  else
    echo "## Regression Signals"
    echo "- none"
  fi
} > "$out_md"

echo "wrote ${out_json}"
echo "wrote ${out_md}"

if [[ "$status" == "regression" && "$fail_on_regression" == "1" ]]; then
  echo "quality trend regression detected (FAIL_ON_QUALITY_REGRESSION=1)" >&2
  exit 1
fi
