#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

tmp_root="$(mktemp -d)"
trap 'rm -rf "$tmp_root"' EXIT

src_dir="${tmp_root}/e2e"
out_root="${tmp_root}/failure-packets"
registry_root="${tmp_root}/failure-packets-registry"
fetch_root="${tmp_root}/failure-packets-fetch"

mkdir -p "$src_dir" "$out_root" "$registry_root" "$fetch_root"
catalog_path="${tmp_root}/e2e_inventory.json"
mkdir -p "${src_dir}/run-failure-packet-001" "${src_dir}/run-failure-packet-002"

cat > "${src_dir}/run-failure-packet-001/canonical.jsonl" <<'JSONL'
{"run_id":"run-failure-packet-001","scenario_id":"rotation-failover","step_id":"setup-start","timestamp":"2026-03-04T10:00:00Z","actor":"ci","component":"harness","input_redacted":{"case":"fixture"},"output":{"state":"begin"},"decision":"continue","duration_ms":0,"error":{"present":false,"code":"","message":"","details":{}}}
{"run_id":"run-failure-packet-001","scenario_id":"rotation-failover","step_id":"setup-end","timestamp":"2026-03-04T10:00:01Z","actor":"ci","component":"harness","input_redacted":{"case":"fixture"},"output":{"state":"ready"},"decision":"pass","duration_ms":25,"error":{"present":false,"code":"","message":"","details":{}}}
JSONL

cat > "${src_dir}/run-failure-packet-002/canonical.jsonl" <<'JSONL'
{"run_id":"run-failure-packet-002","scenario_id":"rotation-failover","step_id":"switch-start","timestamp":"2026-03-04T10:01:00Z","actor":"ci","component":"switch","input_redacted":{"provider":"codex"},"output":{"target":"profile-b"},"decision":"continue","duration_ms":0,"error":{"present":false,"code":"","message":"","details":{}}}
{"run_id":"run-failure-packet-002","scenario_id":"rotation-failover","step_id":"switch-end","timestamp":"2026-03-04T10:01:02Z","actor":"ci","component":"switch","input_redacted":{"provider":"codex"},"output":{"result":"failed"},"decision":"retry","duration_ms":211,"error":{"present":true,"code":"RATE_LIMIT","message":"provider credits exhausted","details":{"retry_after_seconds":300}}}
JSONL

cat > "${src_dir}/run-failure-packet-002/replay_hints.json" <<'JSON'
{
  "run_id": "run-failure-packet-002",
  "scenario_id": "rotation-failover",
  "test_name": "TestE2E_RotationFailover",
  "working_directory": "/Users/hope/Documents/Projects/caam-account-switching/internal/e2e/workflows",
  "commands": [
    "go test ./internal/e2e/workflows -run '^TestE2E_RotationFailover$' -count=1",
    "go test ./internal/e2e/workflows -run '^TestE2E_RotationFailover$' -count=1 -v"
  ],
  "environment": {
    "HOME": "/Users/hope"
  },
  "artifact_paths": {
    "bundle_dir": "/Users/hope/Documents/Projects/caam-account-switching/artifacts/e2e/run-failure-packet-002",
    "canonical_path": "/Users/hope/Documents/Projects/caam-account-switching/artifacts/e2e/run-failure-packet-002/canonical.jsonl",
    "report_path": "/Users/hope/Documents/Projects/caam-account-switching/artifacts/e2e/run-failure-packet-002/report.json",
    "replay_hints_path": "/Users/hope/Documents/Projects/caam-account-switching/artifacts/e2e/run-failure-packet-002/replay_hints.json"
  }
}
JSON

cat > "${src_dir}/run-failure-packet-002/report.json" <<'JSON'
{
  "cleanup_count": 0,
  "passed": false,
  "run_id": "run-failure-packet-002",
  "scenario_id": "rotation-failover",
  "temp_dir": "/tmp/run-failure-packet-002",
  "test_name": "TestE2E_RotationFailover"
}
JSON

cat > "${catalog_path}" <<'JSON'
{
  "scenario_catalog": [
    {
      "file": "internal/e2e/workflows/rotation_test.go",
      "workflow": "rotation",
      "scenario_id": "TestE2E_RotationFailover",
      "owner": "switching-team"
    }
  ]
}
JSON

FAILURE_PACKET_SCENARIO_CATALOG_PATH="${catalog_path}" FAILURE_PACKET_VALIDATE_SCHEMA=1 ./scripts/generate_failure_packet.sh "$src_dir" "$out_root" >/dev/null

packet_dir="$(find "$out_root" -mindepth 1 -maxdepth 1 -type d | sort | tail -1)"
if [[ -z "$packet_dir" || ! -d "$packet_dir" ]]; then
  echo "failed to locate generated packet directory" >&2
  exit 1
fi

./scripts/validate_failure_packet_bundle_schema.sh docs/testing/failure_packet_bundle_schema.json "$packet_dir" >/dev/null

required_files=(
  "$packet_dir/FAILURE_SUMMARY.md"
  "$packet_dir/failure_packet_manifest.json"
  "$packet_dir/failure_narratives.md"
  "$packet_dir/first_failure.json"
  "$packet_dir/failure_index.json"
  "$packet_dir/merged_timeline.jsonl"
  "$packet_dir/top_errors.tsv"
  "$packet_dir/raw/run-failure-packet-001/canonical.jsonl"
  "$packet_dir/raw/run-failure-packet-002/canonical.jsonl"
)
for path in "${required_files[@]}"; do
  if [[ ! -f "$path" ]]; then
    echo "missing expected failure-packet artifact: $path" >&2
    exit 1
  fi
done

if ! grep -q "RATE_LIMIT" "$packet_dir/top_errors.tsv"; then
  echo "expected RATE_LIMIT signature in top_errors.tsv" >&2
  exit 1
fi
if ! grep -q "RATE_LIMIT" "$packet_dir/failure_narratives.md"; then
  echo "expected RATE_LIMIT narrative in failure_narratives.md" >&2
  exit 1
fi
if [[ "$(jq -r '.first_failure.scenario_id // empty' "$packet_dir/failure_packet_manifest.json")" != "rotation-failover" ]]; then
  echo "manifest did not capture first_failure scenario_id" >&2
  exit 1
fi
if [[ "$(jq -r '.first_failure.likely_owner.lane // empty' "$packet_dir/failure_packet_manifest.json")" != "switching-team" ]]; then
  echo "manifest did not capture first_failure owner lane" >&2
  exit 1
fi
if ! jq -e '.first_failure.replay_context.command | contains("TestE2E_RotationFailover")' "$packet_dir/failure_packet_manifest.json" >/dev/null; then
  echo "manifest did not capture first_failure replay command" >&2
  exit 1
fi
if [[ "$(jq -r '.artifacts.first_failure // empty' "$packet_dir/failure_packet_manifest.json")" != "first_failure.json" ]]; then
  echo "manifest did not declare first_failure artifact" >&2
  exit 1
fi

packet_id="$(basename "$packet_dir")"
FAILURE_PACKET_REGISTRY="$registry_root" ./scripts/failure_packet_ctl.sh publish "$packet_dir" >/dev/null
latest_id="$(FAILURE_PACKET_REGISTRY="$registry_root" ./scripts/failure_packet_ctl.sh latest | jq -r '.id // ""')"
if [[ "$latest_id" != "$packet_id" ]]; then
  echo "registry latest id mismatch: got '$latest_id', expected '$packet_id'" >&2
  exit 1
fi

FAILURE_PACKET_REGISTRY="$registry_root" ./scripts/failure_packet_ctl.sh fetch "$packet_id" "$fetch_root" >/dev/null
if [[ ! -f "${fetch_root}/${packet_id}/FAILURE_SUMMARY.md" ]]; then
  echo "fetch did not materialize expected summary file" >&2
  exit 1
fi

echo "failure packet pipeline fixtures passed: ${packet_id}"
