#!/usr/bin/env bash
set -euo pipefail

SRC_DIR="${1:-artifacts/e2e}"
OUT_ROOT="${2:-artifacts/failure-packets}"
SCHEMA_PATH="${E2E_LOG_SCHEMA_PATH:-docs/testing/e2e_log_schema.json}"
VALIDATOR="${E2E_LOG_VALIDATOR:-scripts/validate_e2e_log_schema.sh}"
VALIDATE_SCHEMA="${FAILURE_PACKET_VALIDATE_SCHEMA:-1}"
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

for f in "${LOG_FILES[@]}"; do
  cp "${f}" "${RAW_DIR}/$(basename "${f}")"
done

EVENTS_FILE="${OUT_DIR}/events.ndjson"
cat "${LOG_FILES[@]}" > "${EVENTS_FILE}"

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

if [ -f "docs/testing/e2e_log_schema.json" ]; then
  cp "docs/testing/e2e_log_schema.json" "${OUT_DIR}/e2e_log_schema.json"
fi
if [ -f "docs/testing/e2e_log_schema_policy.md" ]; then
  cp "docs/testing/e2e_log_schema_policy.md" "${OUT_DIR}/e2e_log_schema_policy.md"
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
  echo
  echo "## Top Error Signatures"
  if [ -s "${TOP_ERRORS_TSV}" ]; then
    echo '```text'
    cat "${TOP_ERRORS_TSV}"
    echo '```'
  else
    echo "_No error events found._"
  fi
} > "${SUMMARY_MD}"

ARCHIVE_PATH="${OUT_ROOT}/failure-packet-${STAMP}.tar.gz"
tar -czf "${ARCHIVE_PATH}" -C "${OUT_ROOT}" "${STAMP}"

echo "Failure packet generated:"
echo "  summary: ${SUMMARY_MD}"
echo "  merged timeline: ${MERGED_TIMELINE}"
echo "  failure index: ${FAILURE_INDEX_JSON}"
echo "  narratives: ${NARRATIVES_MD}"
echo "  archive: ${ARCHIVE_PATH}"
