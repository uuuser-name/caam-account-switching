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

cat > "${src_dir}/a.jsonl" <<'JSONL'
{"run_id":"run-failure-packet-001","scenario_id":"rotation-failover","step_id":"setup-start","timestamp":"2026-03-04T10:00:00Z","actor":"ci","component":"harness","input_redacted":{"case":"fixture"},"output":{"state":"begin"},"decision":"continue","duration_ms":0,"error":{"present":false,"code":"","message":"","details":{}}}
{"run_id":"run-failure-packet-001","scenario_id":"rotation-failover","step_id":"setup-end","timestamp":"2026-03-04T10:00:01Z","actor":"ci","component":"harness","input_redacted":{"case":"fixture"},"output":{"state":"ready"},"decision":"pass","duration_ms":25,"error":{"present":false,"code":"","message":"","details":{}}}
JSONL

cat > "${src_dir}/b.jsonl" <<'JSONL'
{"run_id":"run-failure-packet-002","scenario_id":"rotation-failover","step_id":"switch-start","timestamp":"2026-03-04T10:01:00Z","actor":"ci","component":"switch","input_redacted":{"provider":"codex"},"output":{"target":"profile-b"},"decision":"continue","duration_ms":0,"error":{"present":false,"code":"","message":"","details":{}}}
{"run_id":"run-failure-packet-002","scenario_id":"rotation-failover","step_id":"switch-end","timestamp":"2026-03-04T10:01:02Z","actor":"ci","component":"switch","input_redacted":{"provider":"codex"},"output":{"result":"failed"},"decision":"retry","duration_ms":211,"error":{"present":true,"code":"RATE_LIMIT","message":"provider credits exhausted","details":{"retry_after_seconds":300}}}
JSONL

FAILURE_PACKET_VALIDATE_SCHEMA=1 ./scripts/generate_failure_packet.sh "$src_dir" "$out_root" >/dev/null

packet_dir="$(find "$out_root" -mindepth 1 -maxdepth 1 -type d | sort | tail -1)"
if [[ -z "$packet_dir" || ! -d "$packet_dir" ]]; then
  echo "failed to locate generated packet directory" >&2
  exit 1
fi

required_files=(
  "$packet_dir/FAILURE_SUMMARY.md"
  "$packet_dir/failure_narratives.md"
  "$packet_dir/failure_index.json"
  "$packet_dir/merged_timeline.jsonl"
  "$packet_dir/top_errors.tsv"
  "$packet_dir/raw/a.jsonl"
  "$packet_dir/raw/b.jsonl"
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
