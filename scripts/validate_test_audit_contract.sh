#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

tmp_cover="$(mktemp)"
cleanup() {
  rm -f "$tmp_cover"
}
trap cleanup EXIT

./scripts/test_audit.sh >/dev/null

audit_cmd_cov="$(
  jq -r '.[] | select(.import_path=="github.com/Dicklesworthstone/coding_agent_account_manager/cmd/caam/cmd") | .coverage' \
    artifacts/test-audit/coverage_by_package_with_risk.json
)"

go test -coverprofile="$tmp_cover" ./cmd/caam/cmd >/dev/null
expected_cmd_cov="$(
  go tool cover -func="$tmp_cover" | awk '/^total:/{gsub("%","",$3); print $3}' | tail -n1
)"

python3 - "$audit_cmd_cov" "$expected_cmd_cov" <<'PY'
import sys

observed = float(sys.argv[1])
expected = float(sys.argv[2])
if abs(observed - expected) > 0.1:
    raise SystemExit(f"audit cmd coverage mismatch: observed={observed} expected={expected}")
PY

jq -e '
  .traceability_json_path != null and
  (.traceability_json_present | type) == "boolean" and
  .traceability_md_path != null and
  (.traceability_md_present | type) == "boolean" and
  .traceability_bindings_path != null and
  (.traceability_bindings_present | type) == "boolean" and
  .release_attestation_path != null and
  (.release_attestation_present | type) == "boolean" and
  .release_attestation_md_path != null and
  (.release_attestation_md_present | type) == "boolean"
' artifacts/test-audit/test_audit.json >/dev/null

echo "test_audit contract validation passed"
