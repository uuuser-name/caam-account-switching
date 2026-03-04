#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

dry_path="${1:-}"
live_path="${2:-}"
schema_path="${E2E_LOG_SCHEMA_PATH:-docs/testing/e2e_log_schema.json}"
validator="${E2E_LOG_VALIDATOR:-scripts/validate_e2e_log_schema.sh}"
validate_schema="${E2E_PARITY_VALIDATE_SCHEMA:-1}"

if [[ -z "$dry_path" || -z "$live_path" ]]; then
  echo "usage: scripts/verify_e2e_dry_live_parity.sh <dry_log.jsonl> <live_log.jsonl>" >&2
  exit 1
fi
if [[ ! -f "$dry_path" ]]; then
  echo "dry-run log file not found: $dry_path" >&2
  exit 1
fi
if [[ ! -f "$live_path" ]]; then
  echo "live-run log file not found: $live_path" >&2
  exit 1
fi

if [[ "$validate_schema" != "0" ]]; then
  if [[ ! -x "$validator" ]]; then
    echo "schema validator not found or not executable: $validator" >&2
    exit 1
  fi
  "$validator" "$schema_path" "$dry_path" >/dev/null
  "$validator" "$schema_path" "$live_path" >/dev/null
fi

dry_sig="$(mktemp)"
live_sig="$(mktemp)"
trap 'rm -f "$dry_sig" "$live_sig"' EXIT

normalize_program='
  def strip_volatile:
    if type == "object" then
      del(
        .run_id,
        .timestamp,
        .duration_ms,
        .elapsed_ms,
        .elapsed,
        .request_id,
        .trace_id,
        .nonce,
        .auth_code,
        .resume_id,
        .session_token
      )
    else . end;
  def norm_payload:
    ((. // {}) | if type == "object" then del(.mode) else . end | strip_volatile);
  map({
    scenario_id: .scenario_id,
    step_id: .step_id,
    step_base: (.step_id | sub("-(start|end)$"; "")),
    phase: (if (.step_id | endswith("-start")) then "start" elif (.step_id | endswith("-end")) then "end" else "event" end),
    actor: (.actor // ""),
    component: (.component // ""),
    decision: (.decision // ""),
    error_present: (.error.present // false),
    error_code: (if (.error.present // false) then (.error.code // "") else "" end),
    input_norm: (.input_redacted | norm_payload),
    output_norm: (.output | norm_payload)
  })
'

jq -s "$normalize_program" "$dry_path" > "$dry_sig"
jq -s "$normalize_program" "$live_path" > "$live_sig"

check_phase_balance() {
  local signature_path="$1"
  local source_path="$2"
  local imbalance_json
  imbalance_json="$(jq -c '
    reduce .[] as $e ({};
      .[$e.step_base] = (.[$e.step_base] // {start: 0, end: 0, event: 0}) |
      if $e.phase == "start" then
        .[$e.step_base].start += 1
      elif $e.phase == "end" then
        .[$e.step_base].end += 1
      else
        .[$e.step_base].event += 1
      end
    )
    | to_entries
    | map(select(.value.start != .value.end))
  ' "$signature_path")"

  if [[ "$imbalance_json" != "[]" ]]; then
    jq -n \
      --arg path "$source_path" \
      --argjson mismatches "$imbalance_json" '
      {
        parity: "failed",
        reason: "phase_balance_mismatch",
        log: $path,
        mismatches: $mismatches
      }
    ' >&2
    return 1
  fi
}

check_phase_balance "$dry_sig" "$dry_path"
check_phase_balance "$live_sig" "$live_path"

dry_count="$(jq 'length' "$dry_sig")"
live_count="$(jq 'length' "$live_sig")"
if [[ "$dry_count" -ne "$live_count" ]]; then
  jq -n --arg dry "$dry_path" --arg live "$live_path" --argjson dry_count "$dry_count" --argjson live_count "$live_count" '
    {
      parity: "failed",
      reason: "event_count_mismatch",
      dry_log: $dry,
      live_log: $live,
      dry_events: $dry_count,
      live_events: $live_count
    }
  ' >&2
  exit 1
fi

for ((i=0; i<dry_count; i++)); do
  dry_event="$(jq -c ".[$i]" "$dry_sig")"
  live_event="$(jq -c ".[$i]" "$live_sig")"
  if [[ "$dry_event" != "$live_event" ]]; then
    jq -n \
      --arg dry "$dry_path" \
      --arg live "$live_path" \
      --argjson index "$i" \
      --argjson dry_event "$dry_event" \
      --argjson live_event "$live_event" '
      {
        parity: "failed",
        reason: "event_mismatch",
        dry_log: $dry,
        live_log: $live,
        index: $index,
        dry_event: $dry_event,
        live_event: $live_event
      }
    ' >&2
    exit 1
  fi
done

echo "dry/live parity check passed: ${dry_path} vs ${live_path} (${dry_count} events)"
