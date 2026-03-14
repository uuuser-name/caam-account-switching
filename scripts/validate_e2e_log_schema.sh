#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

schema_path="${1:-docs/testing/e2e_log_schema.json}"
jsonl_path="${2:-docs/testing/e2e_log_sample.jsonl}"
redaction_rules_path="${TEST_AUDIT_REDACTION_RULES:-docs/testing/e2e_redaction_rules.json}"

if [[ ! -f "$schema_path" ]]; then
  echo "schema not found: $schema_path" >&2
  exit 1
fi
if [[ ! -f "$jsonl_path" ]]; then
  echo "jsonl file not found: $jsonl_path" >&2
  exit 1
fi
if [[ ! -f "$redaction_rules_path" ]]; then
  echo "redaction rules file not found: $redaction_rules_path" >&2
  exit 1
fi

schema_version="$(jq -r '.schema_version // empty' "$schema_path")"
if [[ -z "$schema_version" ]]; then
  echo "schema_version missing in $schema_path" >&2
  exit 1
fi

required_fields="$(jq -c '.required // empty' "$schema_path")"
if [[ -z "$required_fields" || "$required_fields" == "null" ]]; then
  echo "required field list missing in $schema_path" >&2
  exit 1
fi
deny_patterns="$(jq -c '.deny_patterns // []' "$redaction_rules_path")"
if [[ "$deny_patterns" == "null" ]]; then
  deny_patterns="[]"
fi

line_no=0
validated=0
tmp_events="$(mktemp)"
trap 'rm -f "$tmp_events"' EXIT

while IFS= read -r line || [[ -n "$line" ]]; do
  line_no=$((line_no + 1))
  [[ -z "$line" ]] && continue
  [[ "$line" =~ ^[[:space:]]*# ]] && continue

  if ! jq -e --argjson req "$required_fields" --argjson denies "$deny_patterns" '
    (. | type) == "object"
    and (. as $obj | reduce $req[] as $k (true; . and ($obj | has($k))))
    and (.run_id | type == "string" and length > 0)
    and (.scenario_id | type == "string" and length > 0)
    and (.step_id | type == "string" and length > 0)
    and (.timestamp | type == "string" and fromdateiso8601? != null)
    and (.actor | type == "string" and length > 0)
    and (.component | type == "string" and length > 0)
    and (.input_redacted | type == "object")
    and (.output | type == "object")
    and (.decision | type == "string" and (. == "pass" or . == "continue" or . == "retry" or . == "abort"))
    and (.duration_ms | type == "number" and . >= 0 and (floor == .))
    and (.error | type == "object" and has("present") and has("code") and has("message") and has("details"))
    and ((.error.present | type) == "boolean")
    and ((.error.code | type) == "string")
    and ((.error.message | type) == "string")
    and ((.error.details | type) == "object")
    and (if .error.present == false then (.error.code == "" and .error.message == "") else true end)
    and (([.input_redacted, .output, .error.details] | tostring) as $blob
      | ($denies | all(. as $re | ($blob | test($re; "i") | not))))
  ' <<<"$line" >/dev/null; then
    echo "invalid log at line $line_no" >&2
    echo "$line" >&2
    exit 1
  fi
  printf '%s\n' "$line" >>"$tmp_events"
  validated=$((validated + 1))
done <"$jsonl_path"

if [[ "$validated" -eq 0 ]]; then
  echo "no JSONL events validated from $jsonl_path" >&2
  exit 1
fi

# Correlation/timeline integrity checks across the entire run
if ! jq -s -e '
  def ts: (.timestamp | fromdateiso8601);
  def valid_decision: (. == "pass" or . == "continue" or . == "retry" or . == "abort");
  def step_base($suffix): sub(($suffix + "$"); "");
  def balanced_steps:
    reduce .[] as $e ({ok:true, open:{}};
      if .ok | not then .
      elif ($e.step_id | endswith("-start")) then
        ($e.step_id | step_base("-start")) as $base
        | .open[$base] = ((.open[$base] // 0) + 1)
      elif ($e.step_id | endswith("-end")) then
        ($e.step_id | step_base("-end")) as $base
        | if ((.open[$base] // 0) > 0)
          then .open[$base] = (.open[$base] - 1)
          else .ok = false
          end
      else .
      end
    ) as $state
    | $state.ok and ([($state.open // {})[]?] | all(. == 0));

  # Decisions must be in allowlist
  ([.[].decision] | all(valid_decision))
  and

  # Single-run/single-scenario consistency for each JSONL file
  (([.[].run_id] | unique | length) == 1)
  and
  (([.[].scenario_id] | unique | length) == 1)
  and

  # Timestamp monotonicity (non-decreasing)
  (
    reduce .[] as $e ({ok:true,last:null};
      if .ok | not then .
      else
        ($e | ts) as $cur
        | if .last == null or $cur >= .last
          then .last = $cur
          else .ok = false
          end
      end
    ) | .ok
  )
  and

  # Step accounting must stay balanced across the run.
  balanced_steps
' "$tmp_events" >/dev/null; then
  echo "correlation/timeline integrity validation failed: $jsonl_path" >&2
  jq -s '
    def step_base($suffix): sub(($suffix + "$"); "");
    {
      run_ids: ([.[].run_id] | unique),
      scenario_ids: ([.[].scenario_id] | unique),
      decisions: ([.[].decision] | unique),
      start_events: ([.[] | select(.step_id|endswith("-start")) | .step_id] | length),
      end_events: ([.[] | select(.step_id|endswith("-end")) | .step_id] | length),
      step_balance: (
        reduce .[] as $e ({};
          if ($e.step_id | endswith("-start")) then
            ($e.step_id | step_base("-start")) as $base
            | .[$base] = ((.[$base] // 0) + 1)
          elif ($e.step_id | endswith("-end")) then
            ($e.step_id | step_base("-end")) as $base
            | .[$base] = ((.[$base] // 0) - 1)
          else .
          end
        )
      )
    }
  ' "$tmp_events" >&2 || true
  exit 1
fi

echo "validated $validated events against schema contract v${schema_version}: $jsonl_path"
