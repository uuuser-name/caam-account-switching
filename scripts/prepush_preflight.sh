#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

if [[ ! -x "scripts/test_audit.sh" ]]; then
  echo "scripts/test_audit.sh not found or not executable" >&2
  exit 1
fi
if [[ ! -x "scripts/lint_test_realism.sh" ]]; then
  echo "scripts/lint_test_realism.sh not found or not executable" >&2
  exit 1
fi
if [[ ! -x "scripts/ci_e2e_quality_gate.sh" ]]; then
  echo "scripts/ci_e2e_quality_gate.sh not found or not executable" >&2
  exit 1
fi

echo "== Local Pre-Push Preflight =="
echo "1) test audit"
./scripts/test_audit.sh

echo "2) strict realism lint"
./scripts/lint_test_realism.sh --strict

echo "3) e2e quality gate"
./scripts/ci_e2e_quality_gate.sh

if [[ -f "artifacts/test-audit/test_audit.json" ]]; then
  echo
  echo "== Preflight Summary =="
  jq -r '"aggregate_coverage=\(.aggregate_total_coverage)%"' artifacts/test-audit/test_audit.json
  jq -r '"mock_fake_stub_violations=\(.mock_fake_stub_violations)"' artifacts/test-audit/test_audit.json
  jq -r '"below_threshold_packages=\(.below_threshold_packages | length)"' artifacts/test-audit/test_audit.json
  jq -r '"e2e_inventory_path=\(.e2e_inventory_path)"' artifacts/test-audit/test_audit.json
fi
if [[ -f "artifacts/test-audit/e2e_quality_gate.json" ]]; then
  jq -r '"covered_scenarios=\(.totals.covered)"' artifacts/test-audit/e2e_quality_gate.json
  jq -r '"uncovered_scenarios=\(.totals.uncovered)"' artifacts/test-audit/e2e_quality_gate.json
fi

echo
echo "Pre-push preflight passed."
