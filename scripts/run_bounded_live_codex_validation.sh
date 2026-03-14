#!/usr/bin/env bash
set -euo pipefail

start_profile=""
expected_fallback=""
prompt='Reply with the single word READY and nothing else.'
algorithm="smart"
max_retries="1"
cooldown_duration="168h"
expect_timeout_seconds="${EXPECT_TIMEOUT_SECONDS:-45}"
scenario_id="bounded-live-codex-rate-limit-handoff"
out_dir=""
summary_json=""
summary_md=""
skip_cooldown_precheck=0
caam_bin="${CAAM_BIN:-caam}"

usage() {
  cat <<'EOF'
Usage: ./scripts/run_bounded_live_codex_validation.sh --start-profile <profile> [options]

Options:
  --start-profile <profile>      Codex profile expected to hit the real rate-limit path
  --expected-fallback <profile>  Expected fallback profile; if omitted, derive from `caam next codex --dry-run`
  --prompt <text>                Prompt to run through `caam run codex`
  --algorithm <name>             Rotation algorithm for the live run (default: smart)
  --max-retries <n>              Retry count for `caam run codex` (default: 1)
  --cooldown <duration>          Cooldown duration passed to `caam run` (default: 168h)
  --scenario-id <id>             Canonical scenario id for emitted log events
  --out-dir <path>               Output directory (default: artifacts/bounded-live/<timestamp>)
  --summary-json <path>          Summary JSON path (default: <out-dir>/bounded_live_validation_summary.json)
  --summary-md <path>            Summary markdown path (default: <out-dir>/bounded_live_validation_summary.md)
  --skip-cooldown-precheck       Allow the run even if the start profile is not currently in CAAM cooldowns
  -h, --help                     Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --start-profile)
      start_profile="${2:-}"
      shift 2
      ;;
    --expected-fallback)
      expected_fallback="${2:-}"
      shift 2
      ;;
    --prompt)
      prompt="${2:-}"
      shift 2
      ;;
    --algorithm)
      algorithm="${2:-}"
      shift 2
      ;;
    --max-retries)
      max_retries="${2:-}"
      shift 2
      ;;
    --cooldown)
      cooldown_duration="${2:-}"
      shift 2
      ;;
    --scenario-id)
      scenario_id="${2:-}"
      shift 2
      ;;
    --out-dir)
      out_dir="${2:-}"
      shift 2
      ;;
    --summary-json)
      summary_json="${2:-}"
      shift 2
      ;;
    --summary-md)
      summary_md="${2:-}"
      shift 2
      ;;
    --skip-cooldown-precheck)
      skip_cooldown_precheck=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "${start_profile}" ]]; then
  echo "--start-profile is required" >&2
  usage >&2
  exit 2
fi

for tool in jq expect perl; do
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "${tool} is required" >&2
    exit 2
  fi
done

if [[ "${caam_bin}" == */* ]]; then
  if [[ ! -x "${caam_bin}" ]]; then
    echo "caam binary is not executable: ${caam_bin}" >&2
    exit 2
  fi
  caam_bin="$(cd "$(dirname "${caam_bin}")" && pwd)/$(basename "${caam_bin}")"
else
  if ! command -v "${caam_bin}" >/dev/null 2>&1; then
    echo "caam binary not found in PATH: ${caam_bin}" >&2
    exit 2
  fi
  caam_bin="$(command -v "${caam_bin}")"
fi

run_id="bounded-live-$(date -u +%Y%m%dT%H%M%SZ)"
if [[ -z "${out_dir}" ]]; then
  out_dir="artifacts/bounded-live/${run_id}"
fi
if [[ -z "${summary_json}" ]]; then
  summary_json="${out_dir}/bounded_live_validation_summary.json"
fi
if [[ -z "${summary_md}" ]]; then
  summary_md="${out_dir}/bounded_live_validation_summary.md"
fi

commands_dir="${out_dir}/commands"
mkdir -p "${commands_dir}"
live_work_dir="${LIVE_VALIDATION_WORK_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/caam-bounded-live-workdir.XXXXXX")}"
mkdir -p "${live_work_dir}"

canonical_log_path="${out_dir}/canonical_log.jsonl"
transcript_path="${out_dir}/transcript.log"
transcript_text_path="${out_dir}/transcript.text.log"
pre_status_json="${out_dir}/pre_status.json"
post_status_json="${out_dir}/post_status.json"
cooldowns_json="${out_dir}/cooldowns.json"
schema_stdout="${commands_dir}/canonical_schema.stdout.log"
schema_stderr="${commands_dir}/canonical_schema.stderr.log"

touch "${canonical_log_path}"

started_at_utc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

bool_json() {
  if [[ "$1" == "true" ]]; then
    printf 'true'
  else
    printf 'false'
  fi
}

emit_event() {
  local step_id="$1"
  local component="$2"
  local decision="$3"
  local duration_ms="$4"
  local input_json="$5"
  local output_json="$6"
  local error_present="$7"
  local error_code="$8"
  local error_message="$9"
  local error_details_json="${10:-}"
  if [[ -z "${error_details_json}" ]]; then
    error_details_json='{}'
  fi

  jq -cn \
    --arg run_id "${run_id}" \
    --arg scenario_id "${scenario_id}" \
    --arg step_id "${step_id}" \
    --arg timestamp "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg actor "live-runner" \
    --arg component "${component}" \
    --argjson input_redacted "${input_json}" \
    --argjson output "${output_json}" \
    --arg decision "${decision}" \
    --argjson duration_ms "${duration_ms}" \
    --argjson error_present "$(bool_json "${error_present}")" \
    --arg error_code "${error_code}" \
    --arg error_message "${error_message}" \
    --argjson error_details "${error_details_json}" \
    '{
      run_id: $run_id,
      scenario_id: $scenario_id,
      step_id: $step_id,
      timestamp: $timestamp,
      actor: $actor,
      component: $component,
      input_redacted: $input_redacted,
      output: $output,
      decision: $decision,
      duration_ms: $duration_ms,
      error: {
        present: $error_present,
        code: $error_code,
        message: $error_message,
        details: $error_details
      }
    }' >>"${canonical_log_path}"
}

run_capture() {
  local name="$1"
  shift

  local stdout_path="${commands_dir}/${name}.stdout.log"
  local stderr_path="${commands_dir}/${name}.stderr.log"
  local exit_code=0

  set +e
  "$@" >"${stdout_path}" 2>"${stderr_path}"
  exit_code=$?
  set -e

  printf '%s\t%s\t%s\n' "${exit_code}" "${stdout_path}" "${stderr_path}"
}

step_started_epoch=0
step_name=""
step_component=""
step_input_json="{}"

step_start() {
  step_name="$1"
  step_component="$2"
  step_input_json="$3"
  step_started_epoch="$(date +%s)"
  emit_event "${step_name}-start" "${step_component}" "continue" 0 "${step_input_json}" '{}' false "" "" '{}'
}

step_end() {
  local decision="$1"
  local output_json="$2"
  local error_present="$3"
  local error_code="$4"
  local error_message="$5"
  local error_details_json="${6:-}"
  if [[ -z "${error_details_json}" ]]; then
    error_details_json='{}'
  fi
  local finished_epoch duration_ms

  finished_epoch="$(date +%s)"
  duration_ms=$(( (finished_epoch - step_started_epoch) * 1000 ))
  emit_event "${step_name}-end" "${step_component}" "${decision}" "${duration_ms}" "${step_input_json}" "${output_json}" "${error_present}" "${error_code}" "${error_message}" "${error_details_json}"
}

step_start "preflight" "caam" "$(jq -cn --arg start_profile "${start_profile}" --arg scenario_id "${scenario_id}" '{provider:"codex", start_profile:$start_profile, scenario_id:$scenario_id}')"

cp /dev/null "${pre_status_json}"
cp /dev/null "${post_status_json}"
cp /dev/null "${cooldowns_json}"

"${caam_bin}" status --json >"${pre_status_json}"
"${caam_bin}" cooldown list --json >"${cooldowns_json}"

original_profile="$(jq -r '.tools[] | select(.tool=="codex") | .active_profile // empty' "${pre_status_json}")"
cooldown_hit_count="$(jq --arg profile "${start_profile}" '[.cooldowns[]? | select(.provider=="codex" and .profile==$profile)] | length' "${cooldowns_json}")"

if [[ "${skip_cooldown_precheck}" != "1" && "${cooldown_hit_count}" == "0" ]]; then
  step_end "abort" "$(jq -cn --arg original_profile "${original_profile}" '{original_profile:$original_profile, cooldown_precheck:"missing"}')" true "missing_cooldown_precheck" "start profile is not currently in CAAM cooldowns" "$(jq -cn --arg profile "${start_profile}" '{profile:$profile}')"
  echo "start profile ${start_profile} is not currently in cooldown; use --skip-cooldown-precheck to override" >&2
  exit 1
fi

step_end "pass" "$(jq -cn --arg original_profile "${original_profile}" --argjson cooldown_hit_count "${cooldown_hit_count}" '{original_profile:$original_profile, cooldown_hit_count:$cooldown_hit_count}')" false "" "" '{}'

step_start "activate" "caam" "$(jq -cn --arg start_profile "${start_profile}" '{provider:"codex", start_profile:$start_profile}')"
IFS=$'\t' read -r activate_exit activate_stdout activate_stderr < <(run_capture activate "${caam_bin}" activate codex "${start_profile}" --force)
if [[ "${activate_exit}" != "0" ]]; then
  step_end "abort" "$(jq -cn --arg stdout_path "${activate_stdout}" --arg stderr_path "${activate_stderr}" --argjson exit_code "${activate_exit}" '{stdout_path:$stdout_path, stderr_path:$stderr_path, exit_code:$exit_code}')" true "activate_failed" "failed to activate start profile" '{}'
  exit 1
fi
step_end "pass" "$(jq -cn --arg stdout_path "${activate_stdout}" --arg stderr_path "${activate_stderr}" '{stdout_path:$stdout_path, stderr_path:$stderr_path}')" false "" "" '{}'

if [[ -z "${expected_fallback}" ]]; then
  IFS=$'\t' read -r expected_exit _ _ < <(run_capture expected_fallback "${caam_bin}" next codex --dry-run --algorithm "${algorithm}")
  if [[ "${expected_exit}" != "0" ]]; then
    echo "failed to derive expected fallback via caam next codex --dry-run" >&2
    exit 1
  fi
  expected_fallback="$(sed -n 's/^Recommended:[[:space:]]*//p' "${commands_dir}/expected_fallback.stdout.log" | head -n 1 | tr -d '\r')"
  if [[ -z "${expected_fallback}" ]]; then
    expected_fallback="$(sed -n 's/^Next:[[:space:]]*codex\///p' "${commands_dir}/expected_fallback.stdout.log" | head -n 1 | tr -d '\r')"
  fi
fi

if [[ -z "${expected_fallback}" ]]; then
  echo "expected fallback is empty after derivation" >&2
  exit 1
fi

step_start "run" "codex" "$(jq -cn --arg start_profile "${start_profile}" --arg expected_fallback "${expected_fallback}" --arg prompt "${prompt}" --arg live_work_dir "${live_work_dir}" '{provider:"codex", start_profile:$start_profile, expected_fallback:$expected_fallback, prompt:$prompt, live_work_dir:$live_work_dir}')"
run_stdout="${commands_dir}/live_run.stdout.log"
run_stderr="${commands_dir}/live_run.stderr.log"
expect_script="${commands_dir}/live_run.expect"
run_exit=0
cat >"${expect_script}" <<'EOF'
log_user 1
set timeout $env(EXPECT_TIMEOUT)
log_file -noappend $env(EXPECT_LOG_FILE)
spawn {*}$argv
expect {
  -re {Do you trust the contents of this directory\?} {
    send -- "1\r"
    exp_continue
  }
  -re {Press enter to continue} {
    send -- "\r"
    exp_continue
  }
  timeout {
    catch {send_intr}
    exit 124
  }
  eof
}
set wait_status [wait]
set exit_code [lindex $wait_status 3]
exit $exit_code
EOF
set +e
EXPECT_TIMEOUT="${expect_timeout_seconds}" EXPECT_LOG_FILE="${transcript_path}" \
  expect "${expect_script}" /bin/sh -lc 'cd "$1" && shift && exec "$@"' sh "${live_work_dir}" "${caam_bin}" run codex --algorithm "${algorithm}" --max-retries "${max_retries}" --cooldown "${cooldown_duration}" -- "${prompt}" >"${run_stdout}" 2>"${run_stderr}"
run_exit=$?
set -e

perl -pe 's/\e\[[0-9;?]*[ -\/]*[@-~]//g; s/\r/\n/g; s/[\x00-\x08\x0B-\x1F\x7F]//g' "${transcript_path}" >"${transcript_text_path}"

"${caam_bin}" status --json >"${post_status_json}"
selected_fallback="$(jq -r '.tools[] | select(.tool=="codex") | .active_profile // empty' "${post_status_json}")"

rate_limit_line="$(grep -aEm1 "You've hit your usage limit|out of credits|rate limit exceeded|Error: rate limit exceeded" "${transcript_text_path}" || true)"
resume_hint_line="$(grep -aEm1 'codex resume ' "${transcript_text_path}" || true)"
auto_resume_line="$(grep -aEm1 'auto-resuming on profile' "${transcript_text_path}" || true)"
restart_original_line="$(grep -aEm1 'restarting original command on profile' "${transcript_text_path}" || true)"
continuation_line="$(grep -aEm1 'Injected continuation prompt after seamless resume' "${transcript_text_path}" || true)"
manual_relogin_line="$(grep -aEm1 'sign in again|codex login|device-auth|browser' "${transcript_text_path}" || true)"
ready_output_line="$(grep -aE 'READY' "${transcript_text_path}" | grep -av 'Reply with the single word READY and nothing else' | head -n 1 || true)"
resume_handle="$(printf '%s\n' "${resume_hint_line}" | grep -aoE '[0-9a-f]{8}-[0-9a-f-]{27}' | head -n 1 || true)"
expected_fallback_match=false
if [[ -n "${expected_fallback}" && "${selected_fallback}" == "${expected_fallback}" ]]; then
  expected_fallback_match=true
fi

if [[ "${run_exit}" == "124" ]]; then
  step_end "abort" "$(jq -cn \
    --arg transcript_path "${transcript_path}" \
    --arg transcript_text_path "${transcript_text_path}" \
    --arg stdout_path "${run_stdout}" \
    --arg stderr_path "${run_stderr}" \
    --argjson exit_code "${run_exit}" \
    --argjson timeout_seconds "${expect_timeout_seconds}" \
    '{transcript_path:$transcript_path, transcript_text_path:$transcript_text_path, stdout_path:$stdout_path, stderr_path:$stderr_path, exit_code:$exit_code, timeout_seconds:$timeout_seconds}')" true "live_run_timeout" "bounded live run timed out before producing a completion signal" '{}'
elif [[ "${run_exit}" != "0" ]]; then
  step_end "abort" "$(jq -cn \
    --arg transcript_path "${transcript_path}" \
    --arg transcript_text_path "${transcript_text_path}" \
    --arg stdout_path "${run_stdout}" \
    --arg stderr_path "${run_stderr}" \
    --arg selected_fallback "${selected_fallback}" \
    --arg rate_limit_line "${rate_limit_line}" \
    --arg resume_hint_line "${resume_hint_line}" \
    --arg auto_resume_line "${auto_resume_line}" \
    --arg continuation_line "${continuation_line}" \
    --argjson exit_code "${run_exit}" \
    '{transcript_path:$transcript_path, transcript_text_path:$transcript_text_path, stdout_path:$stdout_path, stderr_path:$stderr_path, selected_fallback:$selected_fallback, rate_limit_line:$rate_limit_line, resume_hint_line:$resume_hint_line, auto_resume_line:$auto_resume_line, continuation_line:$continuation_line, exit_code:$exit_code}')" true "live_run_failed" "caam run codex exited non-zero" '{}'
else
  step_end "pass" "$(jq -cn \
    --arg transcript_path "${transcript_path}" \
    --arg transcript_text_path "${transcript_text_path}" \
    --arg stdout_path "${run_stdout}" \
    --arg stderr_path "${run_stderr}" \
    --arg selected_fallback "${selected_fallback}" \
    --arg rate_limit_line "${rate_limit_line}" \
    --arg resume_hint_line "${resume_hint_line}" \
    --arg auto_resume_line "${auto_resume_line}" \
    --arg continuation_line "${continuation_line}" \
    --arg resume_handle "${resume_handle}" \
    '{transcript_path:$transcript_path, transcript_text_path:$transcript_text_path, stdout_path:$stdout_path, stderr_path:$stderr_path, selected_fallback:$selected_fallback, rate_limit_line:$rate_limit_line, resume_hint_line:$resume_hint_line, auto_resume_line:$auto_resume_line, continuation_line:$continuation_line, resume_handle:$resume_handle}')" false "" "" '{}'
fi

step_start "verify" "live-runner" "$(jq -cn --arg start_profile "${start_profile}" --arg expected_fallback "${expected_fallback}" --arg selected_fallback "${selected_fallback}" '{start_profile:$start_profile, expected_fallback:$expected_fallback, selected_fallback:$selected_fallback}')"

cooldown_precheck_pass=true
if [[ "${skip_cooldown_precheck}" != "1" && "${cooldown_hit_count}" == "0" ]]; then
  cooldown_precheck_pass=false
fi

protocol_a_pass=false
if [[ -n "${rate_limit_line}" && -n "${selected_fallback}" && "${selected_fallback}" != "${start_profile}" && -z "${manual_relogin_line}" && ( "${run_exit}" == "0" || -n "${restart_original_line}" || -n "${ready_output_line}" ) ]]; then
  protocol_a_pass=true
fi

protocol_b_pass=false
if [[ -n "${resume_hint_line}" && -n "${auto_resume_line}" && -n "${continuation_line}" && -n "${resume_handle}" && -z "${manual_relogin_line}" && "${run_exit}" == "0" ]]; then
  protocol_b_pass=true
fi

verify_decision="abort"
if [[ "${cooldown_precheck_pass}" == "true" && "${protocol_a_pass}" == "true" ]]; then
  verify_decision="pass"
fi
step_end "${verify_decision}" "$(jq -cn \
  --arg selected_fallback "${selected_fallback}" \
  --arg rate_limit_line "${rate_limit_line}" \
  --arg resume_hint_line "${resume_hint_line}" \
  --arg continuation_line "${continuation_line}" \
  --argjson cooldown_precheck_pass "$(bool_json "${cooldown_precheck_pass}")" \
  --argjson protocol_a_pass "$(bool_json "${protocol_a_pass}")" \
  --argjson protocol_b_pass "$(bool_json "${protocol_b_pass}")" \
  '{selected_fallback:$selected_fallback, rate_limit_line:$rate_limit_line, resume_hint_line:$resume_hint_line, continuation_line:$continuation_line, cooldown_precheck_pass:$cooldown_precheck_pass, protocol_a_pass:$protocol_a_pass, protocol_b_pass:$protocol_b_pass}')" false "" "" '{}'

set +e
./scripts/validate_e2e_log_schema.sh docs/testing/e2e_log_schema.json "${canonical_log_path}" >"${schema_stdout}" 2>"${schema_stderr}"
schema_exit=$?
set -e

pass=false
truth_label="failed"
if [[ "${schema_exit}" == "0" && "${cooldown_precheck_pass}" == "true" && "${protocol_a_pass}" == "true" ]]; then
  pass=true
  truth_label="bounded_live_green"
fi

finished_at_utc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

jq -n \
  --arg generated_at_utc "${finished_at_utc}" \
  --arg started_at_utc "${started_at_utc}" \
  --arg finished_at_utc "${finished_at_utc}" \
  --arg run_id "${run_id}" \
  --arg scenario_id "${scenario_id}" \
  --arg provider "codex" \
  --arg start_profile "${start_profile}" \
  --arg expected_fallback "${expected_fallback}" \
  --arg selected_fallback "${selected_fallback}" \
  --arg original_profile "${original_profile}" \
  --arg transcript_path "${transcript_path}" \
  --arg transcript_text_path "${transcript_text_path}" \
  --arg canonical_log_path "${canonical_log_path}" \
  --arg pre_status_json "${pre_status_json}" \
  --arg post_status_json "${post_status_json}" \
  --arg cooldowns_json "${cooldowns_json}" \
  --arg live_work_dir "${live_work_dir}" \
  --arg activate_stdout "${activate_stdout}" \
  --arg activate_stderr "${activate_stderr}" \
  --arg run_stdout "${run_stdout}" \
  --arg run_stderr "${run_stderr}" \
  --arg schema_stdout "${schema_stdout}" \
  --arg schema_stderr "${schema_stderr}" \
  --arg rate_limit_line "${rate_limit_line}" \
  --arg resume_hint_line "${resume_hint_line}" \
  --arg auto_resume_line "${auto_resume_line}" \
  --arg restart_original_line "${restart_original_line}" \
  --arg continuation_line "${continuation_line}" \
  --arg manual_relogin_line "${manual_relogin_line}" \
  --arg ready_output_line "${ready_output_line}" \
  --arg resume_handle "${resume_handle}" \
  --arg truth_label "${truth_label}" \
  --arg prompt "${prompt}" \
  --argjson cooldown_hit_count "${cooldown_hit_count}" \
  --argjson activate_exit "${activate_exit}" \
  --argjson run_exit "${run_exit}" \
  --argjson schema_exit "${schema_exit}" \
  --argjson cooldown_precheck_pass "$(bool_json "${cooldown_precheck_pass}")" \
  --argjson protocol_a_pass "$(bool_json "${protocol_a_pass}")" \
  --argjson protocol_b_pass "$(bool_json "${protocol_b_pass}")" \
  --argjson expected_fallback_match "$(bool_json "${expected_fallback_match}")" \
  --argjson pass "$(bool_json "${pass}")" \
  '{
    generated_at_utc: $generated_at_utc,
    started_at_utc: $started_at_utc,
    finished_at_utc: $finished_at_utc,
    run_id: $run_id,
    scenario_id: $scenario_id,
    provider: $provider,
    pass: $pass,
    truth_label: $truth_label,
    profiles: {
      start: $start_profile,
      expected_fallback: $expected_fallback,
      selected_fallback: $selected_fallback,
      original_active: $original_profile
    },
    prompt: $prompt,
    artifacts: {
      transcript_path: $transcript_path,
      transcript_text_path: $transcript_text_path,
      canonical_log_path: $canonical_log_path,
      pre_status_json: $pre_status_json,
      post_status_json: $post_status_json,
      cooldowns_json: $cooldowns_json,
      live_work_dir: $live_work_dir
    },
    commands: {
      activate: {
        exit_code: $activate_exit,
        stdout_path: $activate_stdout,
        stderr_path: $activate_stderr
      },
      live_run: {
        exit_code: $run_exit,
        stdout_path: $run_stdout,
        stderr_path: $run_stderr
      },
      canonical_schema: {
        exit_code: $schema_exit,
        stdout_path: $schema_stdout,
        stderr_path: $schema_stderr
      }
    },
    evidence: {
      cooldown_hit_count: $cooldown_hit_count,
      rate_limit_line: (if $rate_limit_line == "" then null else $rate_limit_line end),
      resume_hint_line: (if $resume_hint_line == "" then null else $resume_hint_line end),
      auto_resume_line: (if $auto_resume_line == "" then null else $auto_resume_line end),
      restart_original_line: (if $restart_original_line == "" then null else $restart_original_line end),
      continuation_line: (if $continuation_line == "" then null else $continuation_line end),
      manual_relogin_line: (if $manual_relogin_line == "" then null else $manual_relogin_line end),
      ready_output_line: (if $ready_output_line == "" then null else $ready_output_line end),
      resume_handle: (if $resume_handle == "" then null else $resume_handle end)
    },
    criteria: {
      cooldown_precheck_pass: $cooldown_precheck_pass,
      expected_fallback_match: $expected_fallback_match,
      exhausted_account_auto_switch_pass: $protocol_a_pass,
      resume_auto_continue_pass: $protocol_b_pass
    }
  }' >"${summary_json}"

{
  echo "# Bounded Live Codex Validation Summary"
  echo
  echo "- Generated at: ${finished_at_utc}"
  echo "- Run ID: \`${run_id}\`"
  echo "- Scenario: \`${scenario_id}\`"
  echo "- Truth label: \`${truth_label}\`"
  echo "- Pass: \`${pass}\`"
  echo "- Start profile: \`${start_profile}\`"
  echo "- Expected fallback: \`${expected_fallback}\`"
  echo "- Selected fallback: \`${selected_fallback}\`"
  echo "- Resume handle: \`${resume_handle:-n/a}\`"
  echo
  echo "## Criteria"
  echo "- Cooldown precheck: \`${cooldown_precheck_pass}\`"
  echo "- Exhausted-account auto-switch: \`${protocol_a_pass}\`"
  echo "- Resume + auto-continue: \`${protocol_b_pass}\`"
  echo
  echo "## Artifacts"
  echo "- Transcript: \`${transcript_path}\`"
  echo "- Canonical log: \`${canonical_log_path}\`"
  echo "- Summary JSON: \`${summary_json}\`"
} >"${summary_md}"

echo "wrote ${summary_json}"
echo "wrote ${summary_md}"
