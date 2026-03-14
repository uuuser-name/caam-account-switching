#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

tmp_root="$(mktemp -d)"
trap 'rm -rf "$tmp_root"' EXIT

mkdir -p \
  "${tmp_root}/artifacts/test-audit" \
  "${tmp_root}/artifacts/cli-matrix" \
  "${tmp_root}/docs/testing"

audit_json="${tmp_root}/artifacts/test-audit/test_audit.json"
gate_json="${tmp_root}/artifacts/test-audit/e2e_quality_gate.json"
trend_json="${tmp_root}/artifacts/test-audit/quality_trend_diff.json"
traceability_json="${tmp_root}/artifacts/cli-matrix/scenario_traceability.json"
coverage_baseline_json="${tmp_root}/docs/testing/coverage_baseline.json"
traceability_baseline_json="${tmp_root}/docs/testing/e2e_traceability_baseline.json"
manifest_json="${tmp_root}/artifacts/test-audit/run_artifact_index.json"
attestation_json="${tmp_root}/artifacts/test-audit/release_attestation.json"
attestation_md="${tmp_root}/artifacts/test-audit/release_attestation.md"
installed_validation_json="${tmp_root}/artifacts/test-audit/installed_validation_summary.json"
bounded_live_json="${tmp_root}/artifacts/test-audit/bounded_live_validation_summary.json"
trend_md="${tmp_root}/artifacts/test-audit/quality_trend_diff.md"

cat > "${audit_json}" <<'JSON'
{
  "aggregate_total_coverage": 75,
  "mock_fake_stub_violations": 0
}
JSON

cat > "${gate_json}" <<'JSON'
{
  "pass": true,
  "totals": {
    "required": 100,
    "covered": 80,
    "uncovered": 20
  }
}
JSON

cat > "${traceability_json}" <<'JSON'
{
  "totals": {
    "required_scenarios": 100,
    "covered": 80,
    "uncovered": 20
  }
}
JSON

cat > "${installed_validation_json}" <<'JSON'
{
  "truth_label": "not_run"
}
JSON

cat > "${bounded_live_json}" <<'JSON'
{
  "truth_label": "not_run"
}
JSON

cat > "${coverage_baseline_json}" <<'JSON'
{
  "baseline_total_coverage": 70
}
JSON

cat > "${traceability_baseline_json}" <<'JSON'
{
  "required_scenarios": 100,
  "covered_scenarios": 75,
  "uncovered_scenarios": 25
}
JSON

env \
  RUN_ARTIFACT_INDEX_PATH="${manifest_json}" \
  RUN_ARTIFACT_INDEX_OUT_JSON="${manifest_json}" \
  RUN_ARTIFACT_INDEX_AUDIT_JSON="${audit_json}" \
  RUN_ARTIFACT_INDEX_GATE_JSON="${gate_json}" \
  RUN_ARTIFACT_INDEX_TREND_JSON="${trend_json}" \
  RUN_ARTIFACT_INDEX_TRACEABILITY_JSON="${traceability_json}" \
  RUN_ARTIFACT_INDEX_COVERAGE_BASELINE_JSON="${coverage_baseline_json}" \
  RUN_ARTIFACT_INDEX_TRACEABILITY_BASELINE_JSON="${traceability_baseline_json}" \
  RUN_ARTIFACT_INDEX_RELEASE_ATTESTATION_JSON="${attestation_json}" \
  RUN_ARTIFACT_INDEX_INSTALLED_VALIDATION_JSON="${installed_validation_json}" \
  RUN_ARTIFACT_INDEX_BOUNDED_LIVE_VALIDATION_JSON="${bounded_live_json}" \
  QUALITY_TREND_OUT_JSON="${trend_json}" \
  QUALITY_TREND_OUT_MD="${trend_md}" \
  ./scripts/build_quality_trend_diff.sh >/dev/null

env \
  RUN_ARTIFACT_INDEX_PATH="${manifest_json}" \
  RUN_ARTIFACT_INDEX_OUT_JSON="${manifest_json}" \
  RUN_ARTIFACT_INDEX_AUDIT_JSON="${audit_json}" \
  RUN_ARTIFACT_INDEX_GATE_JSON="${gate_json}" \
  RUN_ARTIFACT_INDEX_TREND_JSON="${trend_json}" \
  RUN_ARTIFACT_INDEX_TRACEABILITY_JSON="${traceability_json}" \
  RUN_ARTIFACT_INDEX_COVERAGE_BASELINE_JSON="${coverage_baseline_json}" \
  RUN_ARTIFACT_INDEX_TRACEABILITY_BASELINE_JSON="${traceability_baseline_json}" \
  RUN_ARTIFACT_INDEX_RELEASE_ATTESTATION_JSON="${attestation_json}" \
  RUN_ARTIFACT_INDEX_INSTALLED_VALIDATION_JSON="${installed_validation_json}" \
  RUN_ARTIFACT_INDEX_BOUNDED_LIVE_VALIDATION_JSON="${bounded_live_json}" \
  RELEASE_INSTALLED_VALIDATION_JSON="${installed_validation_json}" \
  RELEASE_BOUNDED_LIVE_VALIDATION_JSON="${bounded_live_json}" \
  RELEASE_ATTESTATION_OUT_JSON="${attestation_json}" \
  RELEASE_ATTESTATION_OUT_MD="${attestation_md}" \
  ./scripts/release_readiness_attestation.sh >/dev/null

if [[ "$(jq -r '.inputs.audit_json' "${attestation_json}")" != "${audit_json}" ]]; then
  echo "attestation did not consume audit_json from run artifact index" >&2
  exit 1
fi
if [[ "$(jq -r '.inputs.gate_json' "${attestation_json}")" != "${gate_json}" ]]; then
  echo "attestation did not consume gate_json from run artifact index" >&2
  exit 1
fi
if [[ "$(jq -r '.inputs.trend_json' "${attestation_json}")" != "${trend_json}" ]]; then
  echo "attestation did not consume trend_json from run artifact index" >&2
  exit 1
fi
if [[ "$(jq -r '.inputs.installed_validation_json' "${attestation_json}")" != "${installed_validation_json}" ]]; then
  echo "attestation did not record installed_validation_json input" >&2
  exit 1
fi
if [[ "$(jq -r '.inputs.bounded_live_json' "${attestation_json}")" != "${bounded_live_json}" ]]; then
  echo "attestation did not record bounded_live_json input" >&2
  exit 1
fi
if [[ "$(jq -r '.consumer_inputs.quality_trend_diff.traceability_json' "${manifest_json}")" != "${traceability_json}" ]]; then
  echo "run artifact index did not record dashboard traceability input" >&2
  exit 1
fi
if [[ "$(jq -r '.consumer_inputs.release_readiness_attestation.installed_validation_json' "${manifest_json}")" != "${installed_validation_json}" ]]; then
  echo "run artifact index did not record installed_validation_json input" >&2
  exit 1
fi
if [[ "$(jq -r '.consumer_inputs.release_readiness_attestation.bounded_live_json' "${manifest_json}")" != "${bounded_live_json}" ]]; then
  echo "run artifact index did not record bounded_live_json input" >&2
  exit 1
fi
if [[ "$(jq -r '.artifacts.quality_trend_diff_json.path' "${manifest_json}")" != "${trend_json}" ]]; then
  echo "run artifact index did not record quality trend output path" >&2
  exit 1
fi
if [[ "$(jq -r '.truth_labels.installed_binary' "${attestation_json}")" != "not_run" ]]; then
  echo "attestation did not preserve installed-binary not_run truth label" >&2
  exit 1
fi
if [[ "$(jq -r '.truth_labels.bounded_live' "${attestation_json}")" != "not_run" ]]; then
  echo "attestation did not preserve bounded-live not_run truth label" >&2
  exit 1
fi
if [[ "$(jq -r '.full_closure_pass' "${attestation_json}")" != "false" ]]; then
  echo "attestation incorrectly marked full closure pass with not_run live surfaces" >&2
  exit 1
fi

echo "run artifact index consumers passed"
