#!/usr/bin/env bash
set -euo pipefail

bin_path="${1:-${INSTALLED_CAAM_BIN:-}}"
provenance="${INSTALLED_VALIDATION_PROVENANCE:-unknown}"
artifact_path="${INSTALLED_VALIDATION_ARTIFACT_PATH:-}"
out_json="${INSTALLED_VALIDATION_OUT_JSON:-artifacts/test-audit/installed_validation_summary.json}"
out_md="${INSTALLED_VALIDATION_OUT_MD:-artifacts/test-audit/installed_validation_summary.md}"
tmp_root="${INSTALLED_VALIDATION_TMP_ROOT:-}"

if [[ -z "${bin_path}" ]]; then
  echo "usage: $0 /path/to/caam-binary" >&2
  echo "or set INSTALLED_CAAM_BIN" >&2
  exit 2
fi

if [[ ! -x "${bin_path}" ]]; then
  echo "installed binary is missing or not executable: ${bin_path}" >&2
  exit 2
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 2
fi

if [[ "${provenance}" == "release_artifact" ]]; then
  if [[ -z "${artifact_path}" ]]; then
    echo "INSTALLED_VALIDATION_ARTIFACT_PATH is required for release_artifact provenance" >&2
    exit 2
  fi
  if [[ ! -f "${artifact_path}" ]]; then
    echo "release artifact path does not exist: ${artifact_path}" >&2
    exit 2
  fi
fi

cleanup_tmp=0
if [[ -z "${tmp_root}" ]]; then
  tmp_root="$(mktemp -d)"
  cleanup_tmp=1
fi

cleanup() {
  if [[ "${cleanup_tmp}" == "1" ]]; then
    rm -rf "${tmp_root}"
  fi
}
trap cleanup EXIT

mkdir -p "$(dirname "${out_json}")" "$(dirname "${out_md}")"

bin_abs="$(cd "$(dirname "${bin_path}")" && pwd)/$(basename "${bin_path}")"
work_dir="${tmp_root}/work"
home_dir="${tmp_root}/home"
data_dir="${tmp_root}/xdg-data"
config_dir="${tmp_root}/xdg-config"
caam_home="${tmp_root}/caam-home"
commands_dir="${tmp_root}/commands"
mkdir -p "${work_dir}" "${home_dir}" "${data_dir}" "${config_dir}" "${caam_home}" "${commands_dir}"

run_installed_command() {
  local name="$1"
  shift

  local stdout_path="${commands_dir}/${name}.stdout.log"
  local stderr_path="${commands_dir}/${name}.stderr.log"
  local exit_code=0
  local json_valid=false

  set +e
  (
    cd "${work_dir}"
    HOME="${home_dir}" \
    XDG_DATA_HOME="${data_dir}" \
    XDG_CONFIG_HOME="${config_dir}" \
    CAAM_HOME="${caam_home}" \
    TERM="dumb" \
    NO_COLOR="1" \
    "$@"
  ) >"${stdout_path}" 2>"${stderr_path}"
  exit_code=$?
  set -e

  case "${name}" in
    ls_json|doctor_json)
      if jq empty "${stdout_path}" >/dev/null 2>&1; then
        json_valid=true
      fi
      ;;
  esac

  jq -n \
    --arg name "${name}" \
    --arg stdout_path "${stdout_path}" \
    --arg stderr_path "${stderr_path}" \
    --argjson exit_code "${exit_code}" \
    --argjson json_valid "$( [[ "${json_valid}" == "true" ]] && echo true || echo false )" \
    '{
      name: $name,
      exit_code: $exit_code,
      stdout_path: $stdout_path,
      stderr_path: $stderr_path,
      stdout_json_valid: $json_valid
    }'
}

version_result="$(run_installed_command version "${bin_abs}" version)"
ls_result="$(run_installed_command ls_json "${bin_abs}" ls --json)"
doctor_result="$(run_installed_command doctor_json "${bin_abs}" doctor --json)"

pass="$(jq -n \
  --argjson version "${version_result}" \
  --argjson ls "${ls_result}" \
  --argjson doctor "${doctor_result}" \
  '(
    ($version.exit_code == 0) and
    ($ls.exit_code == 0) and $ls.stdout_json_valid and
    ($doctor.exit_code == 0) and $doctor.stdout_json_valid
  )')"

eligible_for_attestation=false
case "${provenance}" in
  release_artifact|package_install)
    eligible_for_attestation=true
    ;;
esac

truth_label="failed"
if [[ "${pass}" == "true" ]]; then
  if [[ "${eligible_for_attestation}" == "true" ]]; then
    truth_label="installed_binary_green"
  else
    truth_label="not_run"
  fi
fi

generated_at_utc="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

jq -n \
  --arg generated_at_utc "${generated_at_utc}" \
  --arg binary_path "${bin_abs}" \
  --arg provenance "${provenance}" \
  --arg artifact_path "${artifact_path}" \
  --arg work_dir "${work_dir}" \
  --arg home_dir "${home_dir}" \
  --arg data_dir "${data_dir}" \
  --arg config_dir "${config_dir}" \
  --arg caam_home "${caam_home}" \
  --arg truth_label "${truth_label}" \
  --argjson eligible_for_attestation "$( [[ "${eligible_for_attestation}" == "true" ]] && echo true || echo false )" \
  --argjson pass "${pass}" \
  --argjson version "${version_result}" \
  --argjson ls "${ls_result}" \
  --argjson doctor "${doctor_result}" \
  '{
    generated_at_utc: $generated_at_utc,
    pass: $pass,
    truth_label: $truth_label,
    provenance: $provenance,
    eligible_for_attestation: $eligible_for_attestation,
    binary_path: $binary_path,
    artifact_path: (if $artifact_path == "" then null else $artifact_path end),
    isolated_env: {
      work_dir: $work_dir,
      home_dir: $home_dir,
      xdg_data_home: $data_dir,
      xdg_config_home: $config_dir,
      caam_home: $caam_home
    },
    commands: [
      $version,
      $ls,
      $doctor
    ]
  }' >"${out_json}"

{
  echo "# Installed Binary Validation Summary"
  echo
  echo "- Generated at: ${generated_at_utc}"
  echo "- Binary: \`${bin_abs}\`"
  echo "- Provenance: \`${provenance}\`"
  if [[ -n "${artifact_path}" ]]; then
    echo "- Artifact: \`${artifact_path}\`"
  fi
  echo "- Pass: \`${pass}\`"
  echo "- Truth label: \`${truth_label}\`"
  echo "- Eligible for attestation: \`${eligible_for_attestation}\`"
  echo
  echo "## Commands"
  jq -r '.commands[] | "- \(.name): exit_code=\(.exit_code) stdout_json_valid=\(.stdout_json_valid) stdout=\(.stdout_path) stderr=\(.stderr_path)"' "${out_json}"
} >"${out_md}"

echo "wrote ${out_json}"
echo "wrote ${out_md}"
