#!/usr/bin/env bash
set -euo pipefail

SRC_DIR="${1:-artifacts/e2e}"
OUT_ROOT="${2:-artifacts/failure-packets}"
SCHEMA_PATH="${E2E_LOG_SCHEMA_PATH:-docs/testing/e2e_log_schema.json}"
VALIDATOR="${E2E_LOG_VALIDATOR:-scripts/validate_e2e_log_schema.sh}"
VALIDATE_SCHEMA="${FAILURE_PACKET_VALIDATE_SCHEMA:-1}"
BUNDLE_SCHEMA_PATH="${FAILURE_PACKET_BUNDLE_SCHEMA_PATH:-docs/testing/failure_packet_bundle_schema.json}"
SCENARIO_CATALOG_PATH="${FAILURE_PACKET_SCENARIO_CATALOG_PATH:-artifacts/test-audit/e2e_inventory.json}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_DIR="${OUT_ROOT}/${STAMP}"
RAW_DIR="${OUT_DIR}/raw"

mkdir -p "${RAW_DIR}"

mapfile -t LOG_FILES < <(find "${SRC_DIR}" -type f -name '*.jsonl' 2>/dev/null | sort)
if [ "${#LOG_FILES[@]}" -eq 0 ]; then
  echo "No .jsonl log files found under ${SRC_DIR}" >&2
  exit 1
fi

if [[ "${VALIDATE_SCHEMA}" != "0" ]]; then
  if [[ ! -x "${VALIDATOR}" ]]; then
    echo "Schema validator not found or not executable: ${VALIDATOR}" >&2
    exit 1
  fi
  if [[ ! -f "${SCHEMA_PATH}" ]]; then
    echo "Schema file not found: ${SCHEMA_PATH}" >&2
    exit 1
  fi
  for f in "${LOG_FILES[@]}"; do
    "${VALIDATOR}" "${SCHEMA_PATH}" "${f}" >/dev/null
  done
fi

EVENTS_FILE="${OUT_DIR}/events.ndjson"
: > "${EVENTS_FILE}"
for f in "${LOG_FILES[@]}"; do
  rel_log_path="${f#${SRC_DIR}/}"
  if [[ "${rel_log_path}" == "${f}" ]]; then
    rel_log_path="$(basename "${f}")"
  fi
  raw_rel_path="raw/${rel_log_path}"
  mkdir -p "${OUT_DIR}/$(dirname "${raw_rel_path}")"
  cp "${f}" "${OUT_DIR}/${raw_rel_path}"

  bundle_dir="$(dirname "${f}")"
  replay_hints_path="${bundle_dir}/replay_hints.json"
  report_path="${bundle_dir}/report.json"
  if [[ ! -f "${replay_hints_path}" ]]; then
    replay_hints_path=""
  fi
  if [[ ! -f "${report_path}" ]]; then
    report_path=""
  fi

  jq -c \
    --arg source_log "${f}" \
    --arg packet_raw_log "${raw_rel_path}" \
    --arg source_bundle_dir "${bundle_dir}" \
    --arg source_replay_hints_path "${replay_hints_path}" \
    --arg source_report_path "${report_path}" \
    '
    . + {
      source_log: $source_log,
      packet_raw_log: $packet_raw_log,
      source_bundle_dir: $source_bundle_dir,
      source_replay_hints_path: (if $source_replay_hints_path == "" then null else $source_replay_hints_path end),
      source_report_path: (if $source_report_path == "" then null else $source_report_path end)
    }
    ' "${f}" >> "${EVENTS_FILE}"
done

MERGED_TIMELINE="${OUT_DIR}/merged_timeline.jsonl"
jq -c -s 'sort_by(.timestamp, .run_id, .scenario_id, .step_id)[]' "${EVENTS_FILE}" > "${MERGED_TIMELINE}"

TOTAL_EVENTS="$(wc -l < "${EVENTS_FILE}" | tr -d ' ')"
ERROR_EVENTS="$(jq -c 'select(.error.present == true)' "${EVENTS_FILE}" | wc -l | tr -d ' ')"
RUN_COUNT="$(jq -r '.run_id // empty' "${EVENTS_FILE}" | sort -u | wc -l | tr -d ' ')"
SCENARIO_COUNT="$(jq -r '.scenario_id // empty' "${EVENTS_FILE}" | sort -u | wc -l | tr -d ' ')"

TOP_ERRORS_TSV="${OUT_DIR}/top_errors.tsv"
jq -r 'select(.error.present == true) | [(.error.code // "UNKNOWN"), (.component // "unknown"), (.step_id // "unknown"), (.error.message // "")] | @tsv' "${EVENTS_FILE}" \
  | sort \
  | uniq -c \
  | sort -nr \
  | head -n 10 > "${TOP_ERRORS_TSV}" || true

FAILURE_INDEX_JSON="${OUT_DIR}/failure_index.json"
jq -s \
  --arg generated_utc "${STAMP}" \
  --arg source_dir "${SRC_DIR}" \
  '
  . as $events
  | [$events[] | select(.error.present == true)] as $errors
  | {
      generated_utc: $generated_utc,
      source_dir: $source_dir,
      totals: {
        events: ($events | length),
        errors: ($errors | length),
        runs: ($events | map(.run_id) | unique | length),
        scenarios: ($events | map(.scenario_id) | unique | length)
      },
      failures: (
        $errors
        | group_by([(.error.code // "UNKNOWN"), (.component // "unknown"), (.step_id // "unknown"), (.error.message // "")])
        | map({
            code: (.[0].error.code // "UNKNOWN"),
            component: (.[0].component // "unknown"),
            step_id: (.[0].step_id // "unknown"),
            message: (.[0].error.message // ""),
            occurrences: length,
            first_seen: (map(.timestamp) | sort | .[0]),
            last_seen: (map(.timestamp) | sort | .[-1]),
            run_ids: (map(.run_id) | unique | sort),
            scenario_ids: (map(.scenario_id) | unique | sort)
          })
      )
    }
  ' "${EVENTS_FILE}" > "${FAILURE_INDEX_JSON}"

NARRATIVES_MD="${OUT_DIR}/failure_narratives.md"
{
  echo "# Failure Narratives"
  echo
  jq -s -r '
    . as $events
    | [$events[] | select(.error.present == true)] as $errors
    | if ($errors | length) == 0 then
        "_No failure events found in this packet._"
      else
        (
          $errors
          | group_by(.run_id + "|" + .scenario_id)
          | map(sort_by(.timestamp))
          | .[]
          | . as $group
          | $group[0] as $first
          | (
              $events
              | map(select(.run_id == $first.run_id and .scenario_id == $first.scenario_id and (.timestamp <= $first.timestamp)))
              | sort_by(.timestamp)
              | .[-1]
            ) as $context
          | "## \($first.run_id) / \($first.scenario_id)\n- first_error_ts: \($first.timestamp)\n- step: \($first.step_id)\n- component: \($first.component)\n- code: \($first.error.code)\n- message: \($first.error.message)\n- occurrences: \($group | length)\n- last_decision_before_error: \($context.decision // "unknown")\n"
        )
      end
  ' "${EVENTS_FILE}"
} > "${NARRATIVES_MD}"

FIRST_FAILURE_JSON="${OUT_DIR}/first_failure.json"
first_failure_event="$(jq -c -s '
  [.[] | select(.error.present == true)]
  | sort_by(.timestamp, .run_id, .scenario_id, .step_id)
  | .[0] // null
' "${EVENTS_FILE}")"

if [[ "${first_failure_event}" == "null" ]]; then
  printf 'null\n' > "${FIRST_FAILURE_JSON}"
else
  first_replay_hints_path="$(jq -r '.source_replay_hints_path // empty' <<<"${first_failure_event}")"
  first_report_path="$(jq -r '.source_report_path // empty' <<<"${first_failure_event}")"

  if [[ -n "${first_replay_hints_path}" && -f "${first_replay_hints_path}" ]]; then
    replay_hints_json="$(cat "${first_replay_hints_path}")"
  else
    replay_hints_json="null"
  fi

  if [[ -n "${first_report_path}" && -f "${first_report_path}" ]]; then
    report_json="$(cat "${first_report_path}")"
  else
    report_json="null"
  fi

  first_test_name="$(jq -r '(.test_name // empty)' <<<"${replay_hints_json}")"
  if [[ -z "${first_test_name}" ]]; then
    first_test_name="$(jq -r '(.test_name // empty)' <<<"${report_json}")"
  fi

  first_working_dir="$(jq -r '(.working_directory // empty)' <<<"${replay_hints_json}")"

  owner_lane=""
  owner_source=""
  if [[ -n "${first_test_name}" && -f "${SCENARIO_CATALOG_PATH}" ]]; then
    owner_lane="$(jq -r --arg test_name "${first_test_name}" '
      .scenario_catalog[]? | select(.scenario_id == $test_name) | .owner // empty
    ' "${SCENARIO_CATALOG_PATH}" | head -n 1)"
    if [[ -n "${owner_lane}" ]]; then
      owner_source="scenario_catalog"
    fi
  fi
  if [[ -z "${owner_lane}" && -n "${first_working_dir}" ]]; then
    if [[ "${first_working_dir}" == "${REPO_ROOT}" ]]; then
      owner_lane="repo-root"
    elif [[ "${first_working_dir}" == "${REPO_ROOT}/"* ]]; then
      owner_lane="${first_working_dir#${REPO_ROOT}/}"
    else
      owner_lane="${first_working_dir}"
    fi
    owner_source="working_directory"
  fi
  if [[ -z "${owner_lane}" ]]; then
    owner_lane="$(jq -r '(.component // "unknown") + "-lane"' <<<"${first_failure_event}")"
    owner_source="component"
  fi

  owner_package=""
  if [[ -n "${first_working_dir}" && "${first_working_dir}" == "${REPO_ROOT}/"* ]]; then
    owner_package="${first_working_dir#${REPO_ROOT}/}"
  fi

  replay_commands_json="$(jq -c '(.commands // [])' <<<"${replay_hints_json}")"
  replay_command="$(jq -r '(.commands[0] // empty)' <<<"${replay_hints_json}")"
  replay_source="replay_hints"
  if [[ -z "${replay_command}" ]]; then
    replay_target="${first_test_name:-$(jq -r '.scenario_id // "unknown-scenario"' <<<"${first_failure_event}")}"
    replay_command="go test ./... -run '^${replay_target}$' -count=1"
    replay_commands_json="$(jq -nc --arg cmd "${replay_command}" '[$cmd]')"
    replay_source="fallback"
  fi

  jq -n \
    --argjson event "${first_failure_event}" \
    --argjson replay_hints "${replay_hints_json}" \
    --argjson report "${report_json}" \
    --arg owner_lane "${owner_lane}" \
    --arg owner_source "${owner_source}" \
    --arg owner_package "${owner_package}" \
    --arg first_test_name "${first_test_name}" \
    --arg first_working_dir "${first_working_dir}" \
    --arg replay_source "${replay_source}" \
    --arg replay_command "${replay_command}" \
    --argjson replay_commands "${replay_commands_json}" \
    '
    {
      run_id: $event.run_id,
      scenario_id: $event.scenario_id,
      step_id: $event.step_id,
      timestamp: $event.timestamp,
      component: $event.component,
      decision: $event.decision,
      error: {
        code: ($event.error.code // "UNKNOWN"),
        message: ($event.error.message // ""),
        details: ($event.error.details // {})
      },
      likely_owner: {
        lane: $owner_lane,
        source: $owner_source,
        test_name: (if $first_test_name == "" then null else $first_test_name end),
        package: (if $owner_package == "" then null else $owner_package end),
        working_directory: (if $first_working_dir == "" then null else $first_working_dir end)
      },
      artifact_paths: {
        packet_summary: "FAILURE_SUMMARY.md",
        packet_merged_timeline: "merged_timeline.jsonl",
        packet_raw_log: ($event.packet_raw_log // null),
        source_log: ($event.source_log // null),
        source_bundle_dir: ($event.source_bundle_dir // null),
        source_replay_hints_path: ($event.source_replay_hints_path // null),
        source_report_path: ($event.source_report_path // null),
        bundle_canonical_path: ($replay_hints.artifact_paths.canonical_path // $event.source_log // null),
        bundle_transcript_path: ($replay_hints.artifact_paths.transcript_path // null),
        bundle_summary_path: ($replay_hints.artifact_paths.summary_path // null),
        bundle_report_path: ($replay_hints.artifact_paths.report_path // $event.source_report_path // null),
        bundle_replay_hints_path: ($replay_hints.artifact_paths.replay_hints_path // $event.source_replay_hints_path // null)
      },
      replay_context: {
        source: $replay_source,
        command: $replay_command,
        commands: $replay_commands,
        test_name: (if $first_test_name == "" then ($report.test_name // null) else $first_test_name end),
        working_directory: (if $first_working_dir == "" then null else $first_working_dir end)
      }
    }
    ' > "${FIRST_FAILURE_JSON}"
fi

if [ -f "docs/testing/e2e_log_schema.json" ]; then
  cp "docs/testing/e2e_log_schema.json" "${OUT_DIR}/e2e_log_schema.json"
fi
if [ -f "docs/testing/e2e_log_schema_policy.md" ]; then
  cp "docs/testing/e2e_log_schema_policy.md" "${OUT_DIR}/e2e_log_schema_policy.md"
fi
if [ -f "${BUNDLE_SCHEMA_PATH}" ]; then
  cp "${BUNDLE_SCHEMA_PATH}" "${OUT_DIR}/failure_packet_bundle_schema.json"
fi

SUMMARY_MD="${OUT_DIR}/FAILURE_SUMMARY.md"
{
  echo "# Failure Packet Summary"
  echo
  echo "- Generated (UTC): ${STAMP}"
  echo "- Source directory: \`${SRC_DIR}\`"
  echo "- Log files: ${#LOG_FILES[@]}"
  echo "- Total events: ${TOTAL_EVENTS}"
  echo "- Error events: ${ERROR_EVENTS}"
  echo "- Unique run IDs: ${RUN_COUNT}"
  echo "- Unique scenario IDs: ${SCENARIO_COUNT}"
  echo "- Schema validation: $([[ \"${VALIDATE_SCHEMA}\" == \"0\" ]] && echo \"disabled\" || echo \"enabled\")"
  echo
  echo "## Inputs"
  for f in "${LOG_FILES[@]}"; do
    echo "- \`${f}\`"
  done
  echo
  echo "## Outputs"
  echo "- \`raw/\` copied source logs"
  echo "- \`events.ndjson\` concatenated events"
  echo "- \`merged_timeline.jsonl\` deterministic chronological merge"
  echo "- \`top_errors.tsv\` top error signatures"
  echo "- \`failure_index.json\` grouped machine-readable failure signatures"
  echo "- \`failure_narratives.md\` concise per-scenario first-failure narratives"
  echo "- \`first_failure.json\` actionable first-failure packet with owner and replay context"
  echo "- \`failure_packet_manifest.json\` machine-checkable bundle manifest"
  echo
  echo "## Top Error Signatures"
  if [ -s "${TOP_ERRORS_TSV}" ]; then
    echo '```text'
    cat "${TOP_ERRORS_TSV}"
    echo '```'
  else
    echo "_No error events found._"
  fi
  echo
  echo "## First Failure"
  if jq -e 'type == "object"' "${FIRST_FAILURE_JSON}" >/dev/null; then
    jq -r '
      "- Scenario: \(.scenario_id)\n- Run ID: \(.run_id)\n- Step: \(.step_id)\n- Timestamp: \(.timestamp)\n- Error: \(.error.code) - \(.error.message)\n- Likely owner: \(.likely_owner.lane)\n- Replay command: \(.replay_context.command)\n- Raw log: \(.artifact_paths.packet_raw_log // "unknown")\n"
    ' "${FIRST_FAILURE_JSON}"
  else
    echo "_No failure events found._"
  fi
} > "${SUMMARY_MD}"

ARCHIVE_PATH="${OUT_ROOT}/failure-packet-${STAMP}.tar.gz"
tar -czf "${ARCHIVE_PATH}" -C "${OUT_ROOT}" "${STAMP}"

RAW_LOGS_JSON="$(
  for f in "${LOG_FILES[@]}"; do
    rel_log_path="${f#${SRC_DIR}/}"
    if [[ "${rel_log_path}" == "${f}" ]]; then
      rel_log_path="$(basename "${f}")"
    fi
    printf '%s\n' "raw/${rel_log_path}"
  done | jq -R 'select(length > 0)' | jq -s '.'
)"

PACKET_MANIFEST_JSON="${OUT_DIR}/failure_packet_manifest.json"
jq -n \
  --arg schema_version "1.0.0" \
  --arg packet_id "${STAMP}" \
  --arg generated_utc "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg source_dir "${SRC_DIR}" \
  --arg log_schema "${SCHEMA_PATH}" \
  --arg log_validator "${VALIDATOR}" \
  --arg bundle_schema "failure_packet_bundle_schema.json" \
  --arg copied_log_schema "e2e_log_schema.json" \
  --arg copied_log_schema_policy "e2e_log_schema_policy.md" \
  --arg summary "FAILURE_SUMMARY.md" \
  --arg events "events.ndjson" \
  --arg merged_timeline "merged_timeline.jsonl" \
  --arg top_errors "top_errors.tsv" \
  --arg failure_index "failure_index.json" \
  --arg failure_narratives "failure_narratives.md" \
  --arg first_failure_artifact "first_failure.json" \
  --arg raw_dir "raw" \
  --arg archive_name "$(basename "${ARCHIVE_PATH}")" \
  --argjson schema_validation_enabled "$([[ "${VALIDATE_SCHEMA}" == "0" ]] && echo "false" || echo "true")" \
  --argjson log_file_count "${#LOG_FILES[@]}" \
  --argjson total_events "${TOTAL_EVENTS}" \
  --argjson error_events "${ERROR_EVENTS}" \
  --argjson run_count "${RUN_COUNT}" \
  --argjson scenario_count "${SCENARIO_COUNT}" \
  --argjson raw_logs "${RAW_LOGS_JSON}" \
  --slurpfile first_failure "${FIRST_FAILURE_JSON}" '
  {
    schema_version: $schema_version,
    packet_id: $packet_id,
    generated_utc: $generated_utc,
    source_dir: $source_dir,
    first_failure: ($first_failure[0] // null),
    schema_validation: {
      enabled: $schema_validation_enabled,
      log_schema: $log_schema,
      log_validator: $log_validator
    },
    totals: {
      log_files: $log_file_count,
      events: $total_events,
      errors: $error_events,
      runs: $run_count,
      scenarios: $scenario_count
    },
    schema_artifacts: {
      bundle_schema: $bundle_schema,
      log_schema: $copied_log_schema,
      log_schema_policy: $copied_log_schema_policy
    },
    artifacts: {
      summary: $summary,
      events: $events,
      merged_timeline: $merged_timeline,
      top_errors: $top_errors,
      failure_index: $failure_index,
      failure_narratives: $failure_narratives,
      first_failure: $first_failure_artifact,
      raw_dir: $raw_dir,
      raw_logs: $raw_logs,
      archive_name: $archive_name
    }
  }
' > "${PACKET_MANIFEST_JSON}"

echo "Failure packet generated:"
echo "  summary: ${SUMMARY_MD}"
echo "  merged timeline: ${MERGED_TIMELINE}"
echo "  failure index: ${FAILURE_INDEX_JSON}"
echo "  narratives: ${NARRATIVES_MD}"
echo "  manifest: ${PACKET_MANIFEST_JSON}"
echo "  archive: ${ARCHIVE_PATH}"
