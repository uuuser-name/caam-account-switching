#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

artifact_dir="$repo_root/artifacts/test-audit"
mkdir -p "$artifact_dir"
snapshot_root="$artifact_dir/snapshots"
mkdir -p "$snapshot_root"

coverage_json="$artifact_dir/coverage_by_package.json"
coverage_risk_json="$artifact_dir/coverage_by_package_with_risk.json"
coverage_md="$artifact_dir/test_audit.md"
summary_json="$artifact_dir/test_audit.json"
e2e_json="$artifact_dir/e2e_inventory.json"
double_json="$artifact_dir/mock_fake_stub_by_package.json"
double_file_json="$artifact_dir/mock_fake_stub_by_file.json"
realism_allowlist_invalid_json="$artifact_dir/realism_allowlist_invalid_rules.json"
baseline_snapshot_json="$artifact_dir/baseline_snapshot.json"
baseline_ref_path="${TEST_AUDIT_BASELINE_FILE:-$repo_root/docs/testing/coverage_baseline.json}"
realism_allowlist_path="${TEST_AUDIT_REALISM_ALLOWLIST:-$repo_root/docs/testing/test_realism_allowlist.json}"
cli_matrix_json="${TEST_AUDIT_CLI_MATRIX_FILE:-$repo_root/docs/testing/cli_workflow_matrix.json}"
traceability_json_path="${TEST_AUDIT_TRACEABILITY_FILE:-$repo_root/artifacts/cli-matrix/scenario_traceability.json}"
traceability_md_path="${TEST_AUDIT_TRACEABILITY_MD_FILE:-$repo_root/artifacts/cli-matrix/cli_scenario_traceability.md}"
traceability_bindings_path="${TEST_AUDIT_TRACEABILITY_BINDINGS_FILE:-$repo_root/docs/testing/scenario_test_bindings.json}"
release_attestation_json_path="${TEST_AUDIT_RELEASE_ATTESTATION_FILE:-$repo_root/artifacts/test-audit/release_attestation.json}"
release_attestation_md_path="${TEST_AUDIT_RELEASE_ATTESTATION_MD_FILE:-$repo_root/artifacts/test-audit/release_attestation.md}"
timestamp_utc="$(date -u +"%Y%m%dT%H%M%SZ")"
snapshot_dir="$snapshot_root/$timestamp_utc"

threshold="${TEST_AUDIT_COVERAGE_THRESHOLD:-70.0}"
tmp_cov="$(mktemp)"
tmp_pkgs="$(mktemp)"
tmp_pkg_cov="$(mktemp)"
tmp_double_events="$(mktemp)"
tmp_realism_invalid="$(mktemp)"
tmp_scenarios="$(mktemp)"
tmp_pkg_cov_risk="$(mktemp)"
tmp_pkg_cov_json="$(mktemp)"
tmp_pkg_cover_dir="$(mktemp -d)"
go_test_failure_log="$artifact_dir/go_test_failures.log"
suite_failed="false"
audit_failed="false"
first_failure_stage=""
first_failure_package=""
first_failure_message=""
cleanup() {
  rm -f \
    "$tmp_cov" \
    "$tmp_pkgs" \
    "$tmp_pkg_cov" \
    "$tmp_double_events" \
    "$tmp_realism_invalid" \
    "$tmp_scenarios" \
    "$tmp_pkg_cov_risk" \
    "$tmp_pkg_cov_json"
  rm -rf "$tmp_pkg_cover_dir"
}
trap cleanup EXIT

: >"$go_test_failure_log"

export GOFLAGS="${GOFLAGS:--buildvcs=false}"
record_failure() {
  local stage="$1"
  local package="${2:-}"
  local message="${3:-}"

  audit_failed="true"
  if [[ -z "$first_failure_stage" ]]; then
    first_failure_stage="$stage"
    first_failure_package="$package"
    first_failure_message="$message"
  fi
}

append_failure_log() {
  local heading="$1"
  local body="${2:-}"

  {
    printf '== %s ==\n' "$heading"
    if [[ -n "$body" ]]; then
      printf '%s\n' "$body"
    fi
  } >>"$go_test_failure_log"
}

if ! aggregate_test_output="$(go test ./... -coverprofile="$tmp_cov" 2>&1)"; then
  suite_failed="true"
  record_failure "aggregate" "" "go test ./... failed while generating coverage profile"
  append_failure_log "aggregate go test ./... failure" "$aggregate_test_output"
  echo "test audit: go test ./... failed while generating coverage profile; continuing to emit partial artifacts" >&2
fi

module_path="$(go list -m -f '{{.Path}}' 2>/dev/null || true)"

normalize_pkg_path() {
  local pkg="$1"
  if [[ -n "$module_path" && "$pkg" == "$module_path" ]]; then
    echo "."
    return
  fi
  if [[ -n "$module_path" && "$pkg" == "$module_path/"* ]]; then
    echo "./${pkg#"$module_path/"}"
    return
  fi
  echo "$pkg"
}

go list ./... >"$tmp_pkgs"
while IFS= read -r pkg; do
  pkg_test_target="$(normalize_pkg_path "$pkg")"
  pkg_cov_profile="$tmp_pkg_cover_dir/$(tr '/.' '__' <<<"$pkg").cover"
  # Use coverprofiles instead of parsing `go test -cover` output so helper
  # subprocess coverage is included for command-heavy packages.
  if ! pkg_test_output="$(go test -coverprofile="$pkg_cov_profile" "$pkg_test_target" 2>&1)"; then
    suite_failed="true"
    record_failure "per_package" "$pkg" "go test -coverprofile failed for package: $pkg"
    append_failure_log "package $pkg failure" "$pkg_test_output"
    echo "test audit: go test -coverprofile failed for package: $pkg; recording 0.0% and continuing" >&2
    printf '%s\t%s\t%s\n' "$pkg" "0.0" "true" >>"$tmp_pkg_cov"
    continue
  fi
  has_statements="true"
  if grep -Fq 'coverage: [no statements]' <<<"$pkg_test_output"; then
    has_statements="false"
  fi
  pct=""
  if [[ -s "$pkg_cov_profile" ]]; then
    pct="$(go tool cover -func="$pkg_cov_profile" 2>/dev/null | awk '/^total:/{gsub("%","",$3); print $3}' | tail -n1)"
  fi
  if [[ -z "$pct" ]]; then
    line="$(printf '%s\n' "$pkg_test_output" | awk '/coverage:/{print $0}' | tail -n1 || true)"
    pct="$(awk '{for(i=1;i<=NF;i++) if ($i ~ /[0-9]+\.[0-9]+%|[0-9]+%/) {gsub("%","",$i); print $i; exit}}' <<<"$line")"
  fi
  if [[ -z "$pct" ]]; then
    pct="0.0"
  fi
  printf '%s\t%s\t%s\n' "$pkg" "$pct" "$has_statements" >>"$tmp_pkg_cov"
done <"$tmp_pkgs"

while IFS=$'\t' read -r pkg pct has_statements; do
  jq -cn \
    --arg package "$pkg" \
    --argjson coverage "$pct" \
    --arg has_statements "$has_statements" \
    '{
      package:$package,
      coverage:$coverage,
      has_statements:($has_statements=="true")
    }' >>"$tmp_pkg_cov_json"
done <"$tmp_pkg_cov"

if [[ -s "$tmp_pkg_cov_json" ]]; then
  jq -s '.' "$tmp_pkg_cov_json" >"$coverage_json"
else
  echo "[]" >"$coverage_json"
fi

get_risk_tier() {
  case "$1" in
    "./cmd/caam/cmd"|"./internal/exec"|"./internal/coordinator"|"./internal/agent")
      echo "A"
      ;;
    "./internal/deploy"|"./internal/sync"|"./internal/setup"|"./internal/provider/claude"|"./internal/provider/codex"|"./internal/provider/gemini")
      echo "B"
      ;;
    *)
      echo "C"
      ;;
  esac
}

get_risk_floor() {
  case "$1" in
    A) echo "80" ;;
    B) echo "70" ;;
    C) echo "60" ;;
    *) echo "60" ;;
  esac
}

get_owner_hint() {
  case "$1" in
    "./cmd/caam/cmd") echo "cli-team" ;;
    "./internal/exec") echo "runtime-team" ;;
    "./internal/coordinator") echo "coordinator-team" ;;
    "./internal/agent") echo "agent-team" ;;
    "./internal/deploy") echo "deploy-team" ;;
    "./internal/sync") echo "sync-team" ;;
    "./internal/setup") echo "setup-team" ;;
    "./internal/provider/claude"|"./internal/provider/codex"|"./internal/provider/gemini") echo "provider-team" ;;
    "./internal/rotation"|"./internal/refresh"|"./internal/ratelimit") echo "switching-core-team" ;;
    "./internal/authfile"|"./internal/authpool"|"./internal/identity"|"./internal/profile") echo "identity-core-team" ;;
    *) echo "unassigned" ;;
  esac
}

is_critical_path_package() {
  case "$1" in
    "./cmd/caam/cmd"|\
    "./internal/agent"|\
    "./internal/coordinator"|\
    "./internal/rotation"|\
    "./internal/refresh"|\
    "./internal/ratelimit"|\
    "./internal/exec"|\
    "./internal/authfile"|\
    "./internal/authpool"|\
    "./internal/identity"|\
    "./internal/profile"|\
    "./internal/monitor"|\
    "./internal/health"|\
    "./internal/prediction")
      echo "true"
      ;;
    *)
      echo "false"
      ;;
  esac
}

while IFS=$'\t' read -r pkg pct has_statements; do
  normalized_pkg="$(normalize_pkg_path "$pkg")"
  tier="$(get_risk_tier "$normalized_pkg")"
  floor="$(get_risk_floor "$tier")"
  owner="$(get_owner_hint "$normalized_pkg")"
  critical_path="$(is_critical_path_package "$normalized_pkg")"
  status="on_track"
  if [[ "$has_statements" != "true" ]]; then
    status="not_applicable"
  elif [[ "$pct" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
    if awk -v pct="$pct" -v floor="$floor" 'BEGIN { exit !(pct < floor) }'; then
      status="below_floor"
    elif awk -v pct="$pct" -v floor="$floor" 'BEGIN { exit !(pct >= (floor + 10)) }'; then
      status="above_floor"
    fi
  else
    pct="0"
    status="below_floor"
  fi
  jq -cn \
    --arg package "$normalized_pkg" \
    --arg import_path "$pkg" \
    --arg tier "$tier" \
    --arg owner "$owner" \
    --arg status "$status" \
    --arg critical_path "$critical_path" \
    --arg has_statements "$has_statements" \
    --argjson coverage "$pct" \
    --argjson floor "$floor" \
    '{
      package:$package,
      import_path:$import_path,
      coverage:$coverage,
      has_statements:($has_statements=="true"),
      risk_tier:$tier,
      floor:$floor,
      owner:$owner,
      critical_path:($critical_path=="true"),
      status:$status
    }' >>"$tmp_pkg_cov_risk"
done <"$tmp_pkg_cov"

if [[ -s "$tmp_pkg_cov_risk" ]]; then
  jq -s 'sort_by(.risk_tier, .package)' "$tmp_pkg_cov_risk" >"$coverage_risk_json"
else
  echo "[]" >"$coverage_risk_json"
fi

total_cov="0.0"
if [[ -s "$tmp_cov" && "$suite_failed" != "true" ]]; then
  total_cov="$(go tool cover -func="$tmp_cov" | awk '/^total:/{gsub("%","",$3); print $3}')"
  if [[ -z "$total_cov" ]]; then
    total_cov="0.0"
  fi
fi

baseline_cov="$(
  if [[ -f "$baseline_ref_path" ]]; then
    jq -r '.baseline_total_coverage // empty' "$baseline_ref_path" 2>/dev/null || true
  fi
)"
if [[ -z "$baseline_cov" ]]; then
  baseline_cov="$total_cov"
fi
coverage_delta="$(awk -v cur="$total_cov" -v base="$baseline_cov" 'BEGIN{printf "%.1f", cur-base}')"

below_threshold="$(awk -F '\t' -v t="$threshold" '$3=="true" && $2+0 < t {print $1}' "$tmp_pkg_cov" | jq -R -s -c 'split("\n") | map(select(length>0))')"
critical_path_below_floor="$(jq -c '[.[] | select(.critical_path == true and .status == "below_floor")]' "$coverage_risk_json")"
critical_path_below_floor_count="$(jq 'length' <<<"$critical_path_below_floor")"

double_hits="$(rg -n --glob '*_test.go' '\b(mock|fake|stub)\b' . || true)"
mock_count="$(printf "%s\n" "$double_hits" | sed '/^$/d' | wc -l | tr -d ' ')"
rule_rows=""
invalid_rule_rows=""
if [[ ! -f "$realism_allowlist_path" ]]; then
  record_failure "realism_allowlist" "" "realism allowlist not found: $realism_allowlist_path"
  append_failure_log "realism allowlist failure" "realism allowlist not found: $realism_allowlist_path"
  echo "test audit: realism allowlist not found: $realism_allowlist_path; continuing with empty rules" >&2
elif ! rule_rows="$(jq -r '.rules[]? | [.prefix, .scope, .owner, (.expiry // ""), (.remediation_issue // ""), (.notes // "")] | @tsv' "$realism_allowlist_path" 2>/dev/null)"; then
  record_failure "realism_allowlist" "" "realism allowlist could not be parsed: $realism_allowlist_path"
  append_failure_log "realism allowlist parse failure" "realism allowlist could not be parsed: $realism_allowlist_path"
  echo "test audit: realism allowlist could not be parsed: $realism_allowlist_path; continuing with empty rules" >&2
elif ! invalid_rule_rows="$(jq -c '.rules[]? | . as $rule | [if ((.prefix // "") | endswith("/")) then "broad_prefix" else empty end, if ((.owner // "") | length == 0) then "missing_owner" else empty end, if ((.expiry // "") | length == 0) then "missing_expiry" else empty end, if ((.remediation_issue // "") | length == 0) then "missing_remediation_issue" else empty end] | map(select(length > 0)) as $issues | select(($issues | length) > 0) | {prefix: ($rule.prefix // null), owner: ($rule.owner // null), expiry: ($rule.expiry // null), remediation_issue: ($rule.remediation_issue // null), issues: $issues}' "$realism_allowlist_path" 2>/dev/null)"; then
  record_failure "realism_allowlist" "" "realism allowlist metadata could not be validated: $realism_allowlist_path"
  append_failure_log "realism allowlist metadata failure" "realism allowlist metadata could not be validated: $realism_allowlist_path"
  echo "test audit: realism allowlist metadata could not be validated: $realism_allowlist_path; continuing with empty invalid-rule set" >&2
fi

if [[ -n "$invalid_rule_rows" ]]; then
  printf '%s\n' "$invalid_rule_rows" | jq -s '.' >"$realism_allowlist_invalid_json"
  invalid_rule_count="$(jq 'length' "$realism_allowlist_invalid_json")"
  record_failure "realism_allowlist" "" "realism allowlist has $invalid_rule_count invalid exception rule(s)"
  append_failure_log "realism allowlist invalid rules" "$(jq -r '.[] | "- \(.prefix // "<missing-prefix>"): " + (.issues | join(","))' "$realism_allowlist_invalid_json")"
else
  echo "[]" >"$realism_allowlist_invalid_json"
fi

while IFS= read -r hit; do
  [[ -z "$hit" ]] && continue
  path="${hit%%:*}"
  rest="${hit#*:}"
  line="${rest%%:*}"
  text="${rest#*:}"
  term="$(printf "%s\n" "$text" | rg -oi '\b(mock|fake|stub)\b' | head -n1 | tr '[:upper:]' '[:lower:]' || true)"
  if [[ -z "$term" ]]; then
    term="unknown"
  fi

  scope="core"
  owner_hint="unassigned"
  matched_prefix=""
  rule_notes=""
  rule_expiry=""
  rule_remediation_issue=""
  rule_metadata_valid="false"
  while IFS=$'\t' read -r prefix scope_rule owner_rule expiry_rule remediation_rule notes_rule; do
    [[ -z "$prefix" ]] && continue
    if [[ "$path" == "$prefix"* ]]; then
      scope="$scope_rule"
      owner_hint="$owner_rule"
      matched_prefix="$prefix"
      rule_expiry="$expiry_rule"
      rule_remediation_issue="$remediation_rule"
      rule_notes="$notes_rule"
      if [[ "$prefix" != */ && -n "$owner_rule" && -n "$expiry_rule" && -n "$remediation_rule" ]]; then
        rule_metadata_valid="true"
      else
        rule_notes="${notes_rule:+$notes_rule; }missing required allowlist metadata"
      fi
      break
    fi
  done <<<"$rule_rows"

  severity="violation"
  if [[ "$scope" == "boundary" && "$rule_metadata_valid" == "true" ]]; then
    severity="allowed"
  fi

  jq -nc \
    --arg file "$path" \
    --argjson line "$line" \
    --arg term "$term" \
    --arg scope "$scope" \
    --arg severity "$severity" \
    --arg owner_hint "$owner_hint" \
    --arg matched_prefix "$matched_prefix" \
    --arg rule_expiry "$rule_expiry" \
    --arg rule_remediation_issue "$rule_remediation_issue" \
    --argjson rule_metadata_valid "$([[ "$rule_metadata_valid" == "true" ]] && echo "true" || echo "false")" \
    --arg rule_notes "$rule_notes" \
    '{file:$file,line:$line,term:$term,scope:$scope,severity:$severity,owner_hint:$owner_hint,matched_prefix:$matched_prefix,rule_expiry:$rule_expiry,rule_remediation_issue:$rule_remediation_issue,rule_metadata_valid:$rule_metadata_valid,rule_notes:$rule_notes}' \
    >>"$tmp_double_events"
done <<<"$(printf "%s\n" "$double_hits" | sed '/^$/d')"

if [[ -s "$tmp_double_events" ]]; then
  double_by_file_json="$(jq -s '.' "$tmp_double_events")"
else
  double_by_file_json="[]"
fi
printf "%s\n" "$double_by_file_json" >"$double_file_json"
double_violation_count="$(jq '[.[] | select(.severity=="violation")] | length' <<<"$double_by_file_json")"

double_by_package_json="$(
  printf "%s\n" "$double_hits" \
    | awk -F: '
      NF {
        path=$1
        gsub(/^\.\//, "", path)
        n=split(path, seg, "/")
        if (n <= 1) {
          pkg="."
        } else {
          pkg=seg[1]
          for (i=2; i<n; i++) {
            pkg=pkg "/" seg[i]
          }
        }
        c[pkg]++
      }
      END {
        for (k in c) {
          printf "%s\t%s\n", k, c[k]
        }
      }
    ' \
    | sort -k2,2nr -k1,1 \
    | jq -R -s -c '
        split("\n")
        | map(select(length>0))
        | map(split("\t"))
        | map({
            package: .[0],
            matches: (.[1] | tonumber),
            owner: "unassigned"
          })
      '
)"
if [[ -z "$double_by_package_json" || "$double_by_package_json" == "null" ]]; then
  double_by_package_json="[]"
fi
printf "%s\n" "$double_by_package_json" >"$double_json"

e2e_files_json="$(find . -type f \
  \( \
    -path './internal/e2e/workflows/*_test.go' \
    -o -path './cmd/caam/cmd/*e2e*_test.go' \
    -o -name '*e2e*_test.go' \
  \) \
  | sort \
  | sed 's#^\./##' \
  | jq -R -s -c 'split("\n") | map(select(length>0))')"
e2e_count="$(jq 'length' <<<"$e2e_files_json")"

while IFS= read -r rel_file; do
  [[ -z "$rel_file" ]] && continue
  owner="unassigned"
  if [[ "$rel_file" == internal/e2e/workflows/* ]]; then
    owner="e2e-team"
  elif [[ "$rel_file" == cmd/caam/cmd/* ]]; then
    owner="cli-team"
  fi
  workflow="${rel_file##*/}"
  workflow="${workflow%_test.go}"

  tests_found=0
  while IFS= read -r test_name; do
    [[ -z "$test_name" ]] && continue
    tests_found=1
    jq -nc \
      --arg file "$rel_file" \
      --arg workflow "$workflow" \
      --arg test_name "$test_name" \
      --arg owner "$owner" \
      '{file:$file,workflow:$workflow,scenario_id:$test_name,owner:$owner}' \
      >>"$tmp_scenarios"
  done < <(rg -n '^func Test[A-Za-z0-9_]+' "$rel_file" | awk '{print $2}' | cut -d'(' -f1)

  if [[ "$tests_found" -eq 0 ]]; then
    jq -nc \
      --arg file "$rel_file" \
      --arg workflow "$workflow" \
      --arg owner "$owner" \
      '{file:$file,workflow:$workflow,scenario_id:"UNDETECTED_TEST_NAME",owner:$owner}' \
      >>"$tmp_scenarios"
  fi
done < <(jq -r '.[]' <<<"$e2e_files_json")

if [[ -s "$tmp_scenarios" ]]; then
  e2e_scenarios_json="$(jq -s '.' "$tmp_scenarios")"
else
  e2e_scenarios_json="[]"
fi
scenario_entry_count="$(jq 'length' <<<"$e2e_scenarios_json")"

workflow_files_count="$(jq '[.[] | select(startswith("internal/e2e/workflows/"))] | length' <<<"$e2e_files_json")"
workflow_covered_count="$(jq '[.[] | select(.file | startswith("internal/e2e/workflows/")) | .file] | unique | length' <<<"$e2e_scenarios_json")"
cmd_e2e_files_count="$(jq '[.[] | select(startswith("cmd/caam/cmd/"))] | length' <<<"$e2e_files_json")"
cmd_e2e_covered_count="$(jq '[.[] | select(.file | startswith("cmd/caam/cmd/")) | .file] | unique | length' <<<"$e2e_scenarios_json")"
all_workflows_covered="false"
all_cmd_e2e_covered="false"
if [[ "$workflow_files_count" -gt 0 && "$workflow_files_count" -eq "$workflow_covered_count" ]]; then
  all_workflows_covered="true"
fi
if [[ "$cmd_e2e_files_count" -gt 0 && "$cmd_e2e_files_count" -eq "$cmd_e2e_covered_count" ]]; then
  all_cmd_e2e_covered="true"
fi

traceability_matrix_present="false"
traceability_artifact_present="false"
traceability_bindings_present="false"
traceability_markdown_present="false"
traceability_status="blocked_missing_cli_matrix"
traceability_summary_json="null"
if [[ -f "$cli_matrix_json" ]]; then
  traceability_matrix_present="true"
  traceability_status="matrix_present_traceability_not_generated"
fi
if [[ -f "$traceability_bindings_path" ]]; then
  traceability_bindings_present="true"
fi
if [[ -f "$traceability_md_path" ]]; then
  traceability_markdown_present="true"
fi
if [[ -f "$traceability_json_path" ]]; then
  traceability_artifact_present="true"
  traceability_status="traceability_artifact_present"
  traceability_summary_json="$(jq -c '{
    generated_at,
    totals,
    bindings_mode
  }' "$traceability_json_path" 2>/dev/null || printf 'null')"
fi

release_attestation_json_present="false"
release_attestation_md_present="false"
if [[ -f "$release_attestation_json_path" ]]; then
  release_attestation_json_present="true"
fi
if [[ -f "$release_attestation_md_path" ]]; then
  release_attestation_md_present="true"
fi

logging_baseline_json="$(
  while IFS= read -r rel_file; do
    [[ -z "$rel_file" ]] && continue
    has_logging_signal="false"
    if rg -n -q 'daemon\.log|logs|stderr|stdout|tail|ReadFile\(|Output:' "$rel_file"; then
      has_logging_signal="true"
    fi
    jq -nc \
      --arg file "$rel_file" \
      --argjson has_logging_signal "$has_logging_signal" \
      '{file:$file,has_logging_signal:$has_logging_signal}'
  done < <(jq -r '.[]' <<<"$e2e_files_json") | jq -s '.'
)"
if [[ -z "$logging_baseline_json" || "$logging_baseline_json" == "null" ]]; then
  logging_baseline_json="[]"
fi
logging_files_with_signals="$(jq '[.[] | select(.has_logging_signal)] | length' <<<"$logging_baseline_json")"
logging_files_without_signals_json="$(jq '[.[] | select(.has_logging_signal | not) | .file]' <<<"$logging_baseline_json")"
logging_status="partial_heuristic_baseline"
if [[ "$e2e_count" -gt 0 && "$logging_files_with_signals" -eq "$e2e_count" ]]; then
  logging_status="all_e2e_files_show_obvious_logging_signals"
fi

cat >"$e2e_json" <<EOF
{
  "scenario_files": $e2e_files_json,
  "scenario_count": $e2e_count,
  "scenario_file_count": $e2e_count,
  "scenario_entry_count": $scenario_entry_count,
  "scenario_catalog": $e2e_scenarios_json,
  "completeness": {
    "internal_workflow_files": $workflow_files_count,
    "internal_workflow_files_with_scenarios": $workflow_covered_count,
    "all_internal_workflow_files_covered": $all_workflows_covered,
    "cmd_e2e_files": $cmd_e2e_files_count,
    "cmd_e2e_files_with_scenarios": $cmd_e2e_covered_count,
    "all_cmd_e2e_files_covered": $all_cmd_e2e_covered
  },
  "traceability_matrix": {
    "status": "$traceability_status",
    "cli_matrix_path": "${cli_matrix_json#"$repo_root"/}",
    "cli_matrix_present": $traceability_matrix_present,
    "bindings_path": "${traceability_bindings_path#"$repo_root"/}",
    "bindings_present": $traceability_bindings_present,
    "traceability_json_path": "${traceability_json_path#"$repo_root"/}",
    "traceability_json_present": $traceability_artifact_present,
    "traceability_md_path": "${traceability_md_path#"$repo_root"/}",
    "traceability_md_present": $traceability_markdown_present,
    "summary": $traceability_summary_json
  },
  "logging_validation": {
    "status": "$logging_status",
    "method": "heuristic_static_scan",
    "signal_patterns": ["daemon.log","logs","stderr","stdout","tail","ReadFile(","Output:"],
    "files_with_obvious_logging_signals": $logging_files_with_signals,
    "files_without_obvious_logging_signals": $logging_files_without_signals_json,
    "file_results": $logging_baseline_json
  }
}
EOF

cat >"$summary_json" <<EOF
{
  "generated_at_utc": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "aggregate_total_coverage": $total_cov,
  "baseline_total_coverage": $baseline_cov,
  "coverage_delta_from_baseline": $coverage_delta,
  "coverage_threshold": $threshold,
  "below_threshold_packages": $below_threshold,
  "mock_fake_stub_matches": $mock_count,
  "mock_fake_stub_violations": $double_violation_count,
  "realism_allowlist_path": "${realism_allowlist_path#"$repo_root"/}",
  "realism_allowlist_invalid_rules_path": "artifacts/test-audit/realism_allowlist_invalid_rules.json",
  "realism_allowlist_invalid_rule_count": $(jq 'length' "$realism_allowlist_invalid_json"),
  "baseline_reference_path": "${baseline_ref_path#"$repo_root"/}",
  "baseline_snapshot_path": "artifacts/test-audit/baseline_snapshot.json",
  "mock_fake_stub_by_package_path": "artifacts/test-audit/mock_fake_stub_by_package.json",
  "mock_fake_stub_by_file_path": "artifacts/test-audit/mock_fake_stub_by_file.json",
  "coverage_by_package_path": "artifacts/test-audit/coverage_by_package.json",
  "coverage_by_package_with_risk_path": "artifacts/test-audit/coverage_by_package_with_risk.json",
  "critical_path_below_floor_count": $critical_path_below_floor_count,
  "critical_path_below_floor": $critical_path_below_floor,
  "e2e_inventory_path": "artifacts/test-audit/e2e_inventory.json",
  "traceability_json_path": "${traceability_json_path#"$repo_root"/}",
  "traceability_json_present": $traceability_artifact_present,
  "traceability_md_path": "${traceability_md_path#"$repo_root"/}",
  "traceability_md_present": $traceability_markdown_present,
  "traceability_bindings_path": "${traceability_bindings_path#"$repo_root"/}",
  "traceability_bindings_present": $traceability_bindings_present,
  "traceability_status": "$traceability_status",
  "release_attestation_path": "${release_attestation_json_path#"$repo_root"/}",
  "release_attestation_present": $release_attestation_json_present,
  "release_attestation_md_path": "${release_attestation_md_path#"$repo_root"/}",
  "release_attestation_md_present": $release_attestation_md_present,
  "audit_pass": $( [[ "$audit_failed" == "true" ]] && echo "false" || echo "true" ),
  "go_test_pass": $( [[ "$suite_failed" == "true" ]] && echo "false" || echo "true" ),
  "first_failure_stage": $( if [[ -n "$first_failure_stage" ]]; then printf '"%s"' "$first_failure_stage"; else printf 'null'; fi ),
  "first_failure_package": $( if [[ -n "$first_failure_package" ]]; then printf '"%s"' "$first_failure_package"; else printf 'null'; fi ),
  "first_failure_message": $( if [[ -n "$first_failure_message" ]]; then printf '%s' "$first_failure_message" | jq -Rs '.'; else printf 'null'; fi ),
  "go_test_failure_log_path": "artifacts/test-audit/go_test_failures.log",
  "snapshot_dir": "artifacts/test-audit/snapshots/$timestamp_utc"
}
EOF

cat >"$baseline_snapshot_json" <<EOF
{
  "generated_at_utc": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "baseline_reference_path": "${baseline_ref_path#"$repo_root"/}",
  "baseline_total_coverage": $baseline_cov,
  "current_total_coverage": $total_cov,
  "coverage_delta_from_baseline": $coverage_delta
}
EOF

cat >"$coverage_md" <<EOF
# Test Audit Report

- Generated at: $(date -u +"%Y-%m-%dT%H:%M:%SZ")
- Aggregate total coverage: ${total_cov}%
- Baseline total coverage: ${baseline_cov}%
- Coverage delta vs baseline: ${coverage_delta}%
- Coverage threshold: ${threshold}%
- Mock/fake/stub matches in test files: ${mock_count}
- Mock/fake/stub violations in core scope: ${double_violation_count}
- Invalid realism allowlist rules: $(jq 'length' "$realism_allowlist_invalid_json")
- E2E scenario file count: ${e2e_count}
- E2E scenario entry count: ${scenario_entry_count}
- Traceability status: ${traceability_status}
- CLI matrix present: ${traceability_matrix_present}
- Logging baseline status: ${logging_status}
- E2E files with obvious logging signals: ${logging_files_with_signals}/${e2e_count}
- Audit pass: $([[ "$audit_failed" == "true" ]] && echo "false" || echo "true")
- Go test pass: $([[ "$suite_failed" == "true" ]] && echo "false" || echo "true")
- First failure stage: ${first_failure_stage:-none}
- First failure package: ${first_failure_package:-none}
- First failure condition: ${first_failure_message:-none}

## Artifact Paths

- JSON summary: \`artifacts/test-audit/test_audit.json\`
- Baseline snapshot: \`artifacts/test-audit/baseline_snapshot.json\`
- Baseline reference: \`${baseline_ref_path#"$repo_root"/}\`
- Realism allowlist: \`${realism_allowlist_path#"$repo_root"/}\`
- Package coverage: \`artifacts/test-audit/coverage_by_package.json\`
- Package coverage with risk tiers: \`artifacts/test-audit/coverage_by_package_with_risk.json\`
- Mock/fake/stub by package: \`artifacts/test-audit/mock_fake_stub_by_package.json\`
- Mock/fake/stub by file: \`artifacts/test-audit/mock_fake_stub_by_file.json\`
- Invalid realism allowlist rules: \`artifacts/test-audit/realism_allowlist_invalid_rules.json\`
- E2E inventory: \`artifacts/test-audit/e2e_inventory.json\`
- CLI traceability JSON: \`${traceability_json_path#"$repo_root"/}\`
- CLI traceability markdown: \`${traceability_md_path#"$repo_root"/}\`
- Release attestation JSON: \`${release_attestation_json_path#"$repo_root"/}\`
- Release attestation markdown: \`${release_attestation_md_path#"$repo_root"/}\`

## Packages Below Threshold (${threshold}%)

EOF

if [[ "$below_threshold" != "[]" ]]; then
  jq -r '.[] | "- `\(.)`"' <<<"$below_threshold" >>"$coverage_md"
else
  echo "- None" >>"$coverage_md"
fi

cat >>"$coverage_md" <<EOF

## Critical Path Packages Below Floor

EOF
if [[ "$critical_path_below_floor" != "[]" ]]; then
  jq -r '.[] | "- `\(.package)` tier=`\(.risk_tier)` coverage=\(.coverage)% floor=\(.floor)% owner=`\(.owner)`"' <<<"$critical_path_below_floor" >>"$coverage_md"
else
  echo "- None" >>"$coverage_md"
fi

cat >>"$coverage_md" <<EOF

## Mock/Fake/Stub Matches By Package

EOF
if [[ "$double_by_package_json" != "[]" ]]; then
  jq -r '.[] | "- `\(.package)` (\(.matches)) owner=`\(.owner)`"' <<<"$double_by_package_json" >>"$coverage_md"
else
  echo "- None" >>"$coverage_md"
fi

cat >>"$coverage_md" <<EOF

## Mock/Fake/Stub File-Level Classification

EOF
if [[ "$double_by_file_json" != "[]" ]]; then
  jq -r '.[] | "- `\(.file):\(.line)` term=`\(.term)` scope=`\(.scope)` severity=`\(.severity)` owner=`\(.owner_hint)`" + (if (.matched_prefix // "") != "" then " rule=`\(.matched_prefix)`" else "" end)' <<<"$double_by_file_json" >>"$coverage_md"
else
  echo "- None" >>"$coverage_md"
fi

mkdir -p "$snapshot_dir"
cp "$summary_json" "$snapshot_dir/test_audit.json"
cp "$coverage_md" "$snapshot_dir/test_audit.md"
cp "$coverage_json" "$snapshot_dir/coverage_by_package.json"
cp "$coverage_risk_json" "$snapshot_dir/coverage_by_package_with_risk.json"
cp "$double_json" "$snapshot_dir/mock_fake_stub_by_package.json"
cp "$double_file_json" "$snapshot_dir/mock_fake_stub_by_file.json"
cp "$realism_allowlist_invalid_json" "$snapshot_dir/realism_allowlist_invalid_rules.json"
cp "$e2e_json" "$snapshot_dir/e2e_inventory.json"
cp "$baseline_snapshot_json" "$snapshot_dir/baseline_snapshot.json"
cp "$go_test_failure_log" "$snapshot_dir/go_test_failures.log"

if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  {
    echo "## Test Audit Snapshot"
    echo
    echo "- Timestamp: \`$timestamp_utc\`"
    echo "- Summary: \`artifacts/test-audit/test_audit.json\`"
    echo "- Markdown: \`artifacts/test-audit/test_audit.md\`"
    echo "- Snapshot dir: \`artifacts/test-audit/snapshots/$timestamp_utc\`"
  } >>"$GITHUB_STEP_SUMMARY"
fi

echo "wrote $summary_json"
echo "wrote $baseline_snapshot_json"
echo "wrote $coverage_md"

if [[ "$audit_failed" == "true" ]]; then
  echo "test audit: emitted partial artifacts with failing status (first failure stage=${first_failure_stage:-unknown}${first_failure_package:+ package=$first_failure_package})" >&2
  exit 1
fi
echo "wrote $coverage_json"
echo "wrote $coverage_risk_json"
echo "wrote $double_json"
echo "wrote $double_file_json"
echo "wrote $e2e_json"
echo "wrote $snapshot_dir"

if [[ "$suite_failed" == "true" ]]; then
  echo "test audit detected failing go test runs; partial artifacts emitted" >&2
  exit 1
fi
