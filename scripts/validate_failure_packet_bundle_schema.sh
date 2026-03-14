#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

schema_path="${1:-docs/testing/failure_packet_bundle_schema.json}"
packet_dir="${2:-}"

if [[ ! -f "${schema_path}" ]]; then
  echo "failure-packet bundle schema not found: ${schema_path}" >&2
  exit 1
fi
if [[ -z "${packet_dir}" ]]; then
  echo "usage: scripts/validate_failure_packet_bundle_schema.sh [schema_path] <packet_dir>" >&2
  exit 1
fi
if [[ ! -d "${packet_dir}" ]]; then
  echo "packet directory not found: ${packet_dir}" >&2
  exit 1
fi

manifest_path="${packet_dir}/failure_packet_manifest.json"
if [[ ! -f "${manifest_path}" ]]; then
  echo "failure-packet manifest not found: ${manifest_path}" >&2
  exit 1
fi

schema_version="$(jq -r '.schema_version // empty' "${schema_path}")"
if [[ -z "${schema_version}" ]]; then
  echo "schema_version missing in ${schema_path}" >&2
  exit 1
fi

if ! jq -e --arg schema_version "${schema_version}" '
  .schema_version == $schema_version
  and (.packet_id | type == "string" and length > 0)
  and (.generated_utc | fromdateiso8601? != null)
  and (.source_dir | type == "string" and length > 0)
  and (
    .first_failure == null
    or (
      (.first_failure | type == "object")
      and (.first_failure.run_id | type == "string" and length > 0)
      and (.first_failure.scenario_id | type == "string" and length > 0)
      and (.first_failure.step_id | type == "string" and length > 0)
      and (.first_failure.timestamp | fromdateiso8601? != null)
      and (.first_failure.component | type == "string" and length > 0)
      and (.first_failure.decision | type == "string" and length > 0)
      and (.first_failure.error | type == "object")
      and (.first_failure.error.code | type == "string")
      and (.first_failure.error.message | type == "string")
      and (.first_failure.error.details | type == "object")
      and (.first_failure.likely_owner | type == "object")
      and (.first_failure.likely_owner.lane | type == "string" and length > 0)
      and (.first_failure.likely_owner.source | type == "string" and length > 0)
      and (.first_failure.artifact_paths | type == "object")
      and (.first_failure.artifact_paths.packet_summary | type == "string" and length > 0)
      and (.first_failure.artifact_paths.packet_merged_timeline | type == "string" and length > 0)
      and ((.first_failure.artifact_paths.packet_raw_log == null) or (.first_failure.artifact_paths.packet_raw_log | type == "string"))
      and (.first_failure.replay_context | type == "object")
      and (.first_failure.replay_context.source | type == "string" and length > 0)
      and (.first_failure.replay_context.command | type == "string" and length > 0)
      and (.first_failure.replay_context.commands | type == "array" and length >= 1 and all(.[]; type == "string" and length > 0))
    )
  )
  and (.schema_validation | type == "object")
  and (.schema_validation.enabled | type == "boolean")
  and (.schema_validation.log_schema | type == "string" and length > 0)
  and (.schema_validation.log_validator | type == "string" and length > 0)
  and (.totals | type == "object")
  and (.totals.log_files | type == "number" and . >= 1 and floor == .)
  and (.totals.events | type == "number" and . >= 1 and floor == .)
  and (.totals.errors | type == "number" and . >= 0 and floor == .)
  and (.totals.runs | type == "number" and . >= 1 and floor == .)
  and (.totals.scenarios | type == "number" and . >= 1 and floor == .)
  and (.schema_artifacts | type == "object")
  and (.schema_artifacts.bundle_schema | type == "string" and length > 0)
  and (.schema_artifacts.log_schema | type == "string" and length > 0)
  and (.schema_artifacts.log_schema_policy | type == "string" and length > 0)
  and (.artifacts | type == "object")
  and (.artifacts.summary | type == "string" and length > 0)
  and (.artifacts.events | type == "string" and length > 0)
  and (.artifacts.merged_timeline | type == "string" and length > 0)
  and (.artifacts.top_errors | type == "string" and length > 0)
  and (.artifacts.failure_index | type == "string" and length > 0)
  and (.artifacts.failure_narratives | type == "string" and length > 0)
  and (.artifacts.first_failure | type == "string" and length > 0)
  and (.artifacts.raw_dir | type == "string" and length > 0)
  and (.artifacts.raw_logs | type == "array" and length >= 1 and all(.[]; type == "string" and length > 0))
  and (.artifacts.archive_name | type == "string" and length > 0)
' "${manifest_path}" >/dev/null; then
  echo "invalid failure-packet manifest: ${manifest_path}" >&2
  cat "${manifest_path}" >&2
  exit 1
fi

validate_rel_path() {
  local rel_path="$1"
  if [[ -z "${rel_path}" || "${rel_path}" == /* || "${rel_path}" == *".."* ]]; then
    echo "invalid relative artifact path in manifest: ${rel_path}" >&2
    exit 1
  fi
}

while IFS= read -r rel_path; do
  validate_rel_path "${rel_path}"
  if [[ ! -e "${packet_dir}/${rel_path}" ]]; then
    echo "manifest references missing packet artifact: ${packet_dir}/${rel_path}" >&2
    exit 1
  fi
done < <(
  jq -r '
    [
      .schema_artifacts.bundle_schema,
      .schema_artifacts.log_schema,
      .schema_artifacts.log_schema_policy,
      .artifacts.summary,
      .artifacts.events,
      .artifacts.merged_timeline,
      .artifacts.top_errors,
      .artifacts.failure_index,
      .artifacts.failure_narratives,
      .artifacts.first_failure,
      .artifacts.raw_dir
    ] + .artifacts.raw_logs
    | .[]
  ' "${manifest_path}"
)

raw_count="$(jq -r '.artifacts.raw_logs | length' "${manifest_path}")"
declared_log_count="$(jq -r '.totals.log_files' "${manifest_path}")"
if [[ "${raw_count}" != "${declared_log_count}" ]]; then
  echo "raw log count mismatch in manifest: raw_logs=${raw_count}, totals.log_files=${declared_log_count}" >&2
  exit 1
fi

archive_name="$(jq -r '.artifacts.archive_name' "${manifest_path}")"
if [[ "${archive_name}" == */* || "${archive_name}" == *".."* ]]; then
  echo "invalid archive_name in manifest: ${archive_name}" >&2
  exit 1
fi
if [[ ! -f "$(dirname "${packet_dir}")/${archive_name}" ]]; then
  echo "manifest references missing archive: $(dirname "${packet_dir}")/${archive_name}" >&2
  exit 1
fi

echo "validated failure-packet bundle manifest schema v${schema_version}: ${manifest_path}"
