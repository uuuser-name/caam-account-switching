#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

make_stub() {
  local path="$1"
  local body="$2"
  cat >"${path}" <<EOF
#!/usr/bin/env bash
set -euo pipefail
${body}
EOF
  chmod +x "${path}"
}

run_gate() {
  local workdir="$1"
  shift
  (
    cd "${repo_root}"
    env "$@" ./scripts/ci_e2e_quality_gate.sh
  )
}

mkdir -p "${tmp_root}/stubs" "${tmp_root}/artifacts/test-audit" "${tmp_root}/artifacts/cli-matrix"

cat >"${tmp_root}/artifacts/cli-matrix/cli_workflow_matrix.json" <<'JSON'
{
  "command_families": [
    {
      "family": "switch",
      "required_scenarios": {
        "happy": ["switch-rate-limit-recovery"],
        "failure": [],
        "edge": []
      }
    }
  ]
}
JSON

cat >"${tmp_root}/artifacts/test-audit/e2e_inventory.json" <<'JSON'
{
  "scenario_catalog": [
    {
      "scenario_id": "switch-rate-limit-recovery",
      "file": "internal/e2e/workflows/ratelimit_recovery_test.go",
      "owner": "runtime-team",
      "workflow": "switch"
    }
  ]
}
JSON

make_stub "${tmp_root}/stubs/pass.sh" 'echo "ok"'
make_stub "${tmp_root}/stubs/fail_schema.sh" 'echo "schema fixture failed" >&2; exit 7'
make_stub "${tmp_root}/stubs/write_valid_traceability.sh" '
out="${3:?missing traceability output path}"
mkdir -p "$(dirname "${out}")"
cat >"${out}" <<'"'"'JSON'"'"'
{
  "totals": {
    "required_scenarios": 1,
    "covered": 1,
    "uncovered": 0
  }
}
JSON
'
make_stub "${tmp_root}/stubs/write_invalid_traceability.sh" '
out="${3:?missing traceability output path}"
mkdir -p "$(dirname "${out}")"
cat >"${out}" <<'"'"'JSON'"'"'
{
  "totals": {
    "required_scenarios": 4,
    "covered": 1,
    "uncovered": 1
  }
}
JSON
'

failure_summary="${tmp_root}/artifacts/test-audit/e2e_quality_gate.failure.json"
if run_gate "${tmp_root}" \
  E2E_QUALITY_GATE_MATRIX_JSON="${tmp_root}/artifacts/cli-matrix/cli_workflow_matrix.json" \
  E2E_QUALITY_GATE_INVENTORY_JSON="${tmp_root}/artifacts/test-audit/e2e_inventory.json" \
  E2E_QUALITY_GATE_TRACEABILITY_JSON="${tmp_root}/artifacts/cli-matrix/scenario_traceability.failure.json" \
  E2E_QUALITY_GATE_SUMMARY_JSON="${failure_summary}" \
  E2E_QUALITY_GATE_SCHEMA_FIXTURE_CMD="${tmp_root}/stubs/fail_schema.sh" \
  E2E_QUALITY_GATE_FAILURE_PACKET_CMD="${tmp_root}/stubs/pass.sh" \
  E2E_QUALITY_GATE_PARITY_CMD="${tmp_root}/stubs/pass.sh" \
  E2E_QUALITY_GATE_TRACEABILITY_CMD="${tmp_root}/stubs/write_valid_traceability.sh"; then
  echo "quality gate unexpectedly passed when schema fixture stage failed" >&2
  exit 1
fi

if [[ ! -f "${failure_summary}" ]]; then
  echo "quality gate did not emit failure summary artifact" >&2
  exit 1
fi
if [[ "$(jq -r '.failure_reason' "${failure_summary}")" != "schema_fixture_gate_failed" ]]; then
  echo "quality gate did not preserve first failure reason for schema stage" >&2
  exit 1
fi
if [[ "$(jq -r '.failed_stage' "${failure_summary}")" != "Schema fixture gate" ]]; then
  echo "quality gate did not record failed stage name" >&2
  exit 1
fi
if [[ "$(jq -r '.stage_results[0].status' "${failure_summary}")" != "failed" ]]; then
  echo "quality gate did not record failed stage status" >&2
  exit 1
fi
if [[ "$(jq -r '.stage_results[0].exit_code' "${failure_summary}")" != "7" ]]; then
  echo "quality gate did not record failed stage exit code" >&2
  exit 1
fi

invalid_summary="${tmp_root}/artifacts/test-audit/e2e_quality_gate.invalid-traceability.json"
if run_gate "${tmp_root}" \
  E2E_QUALITY_GATE_MATRIX_JSON="${tmp_root}/artifacts/cli-matrix/cli_workflow_matrix.json" \
  E2E_QUALITY_GATE_INVENTORY_JSON="${tmp_root}/artifacts/test-audit/e2e_inventory.json" \
  E2E_QUALITY_GATE_TRACEABILITY_JSON="${tmp_root}/artifacts/cli-matrix/scenario_traceability.invalid.json" \
  E2E_QUALITY_GATE_SUMMARY_JSON="${invalid_summary}" \
  E2E_QUALITY_GATE_SCHEMA_FIXTURE_CMD="${tmp_root}/stubs/pass.sh" \
  E2E_QUALITY_GATE_FAILURE_PACKET_CMD="${tmp_root}/stubs/pass.sh" \
  E2E_QUALITY_GATE_PARITY_CMD="${tmp_root}/stubs/pass.sh" \
  E2E_QUALITY_GATE_TRACEABILITY_CMD="${tmp_root}/stubs/write_invalid_traceability.sh"; then
  echo "quality gate unexpectedly passed with invalid traceability totals" >&2
  exit 1
fi

if [[ ! -f "${invalid_summary}" ]]; then
  echo "quality gate did not emit invalid-traceability summary artifact" >&2
  exit 1
fi
if [[ "$(jq -r '.failure_reason' "${invalid_summary}")" != "invalid_traceability_artifact" ]]; then
  echo "quality gate did not fail closed on invalid traceability totals" >&2
  exit 1
fi

pass_summary="${tmp_root}/artifacts/test-audit/e2e_quality_gate.pass.json"
run_gate "${tmp_root}" \
  E2E_QUALITY_GATE_MATRIX_JSON="${tmp_root}/artifacts/cli-matrix/cli_workflow_matrix.json" \
  E2E_QUALITY_GATE_INVENTORY_JSON="${tmp_root}/artifacts/test-audit/e2e_inventory.json" \
  E2E_QUALITY_GATE_TRACEABILITY_JSON="${tmp_root}/artifacts/cli-matrix/scenario_traceability.pass.json" \
  E2E_QUALITY_GATE_SUMMARY_JSON="${pass_summary}" \
  E2E_QUALITY_GATE_SCHEMA_FIXTURE_CMD="${tmp_root}/stubs/pass.sh" \
  E2E_QUALITY_GATE_FAILURE_PACKET_CMD="${tmp_root}/stubs/pass.sh" \
  E2E_QUALITY_GATE_PARITY_CMD="${tmp_root}/stubs/pass.sh" \
  E2E_QUALITY_GATE_TRACEABILITY_CMD="${tmp_root}/stubs/write_valid_traceability.sh" >/dev/null

if [[ "$(jq -r '.pass' "${pass_summary}")" != "true" ]]; then
  echo "quality gate did not pass on valid staged inputs" >&2
  exit 1
fi
if [[ "$(jq -r '.stage_results | length' "${pass_summary}")" != "4" ]]; then
  echo "quality gate did not record all stage results on pass path" >&2
  exit 1
fi

echo "ci e2e quality gate contract passed"
