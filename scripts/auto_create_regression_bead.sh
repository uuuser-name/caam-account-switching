#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

trend_json="${1:-artifacts/test-audit/quality_trend_diff.json}"
parent_issue="${REGRESSION_PARENT_BEAD:-bd-1r67.13.4}"
blocking_dep="${REGRESSION_BLOCKS_DEP:-bd-1r67.13.4.2}"
owner="${REGRESSION_OWNER:-qa-team@local}"
assignee="${REGRESSION_ASSIGNEE:-}"
sla_due="${REGRESSION_SLA_DUE:-+72h}"
actor="${REGRESSION_AUTOCREATE_ACTOR:-regression-bot}"
dry_run="${REGRESSION_AUTOCREATE_DRY_RUN:-0}"

if [[ ! -f "$trend_json" ]]; then
  echo "trend diff input not found: $trend_json" >&2
  exit 1
fi

status="$(jq -r '.status // "unknown"' "$trend_json")"
signals_csv="$(jq -r '.regression_signals // [] | join(",")' "$trend_json")"
generated_at="$(jq -r '.generated_at // ""' "$trend_json")"
current_cov="$(jq -r '.current.aggregate_coverage // 0' "$trend_json")"
current_uncovered="$(jq -r '.current.uncovered_scenarios // 0' "$trend_json")"

if [[ "$status" != "regression" ]]; then
  jq -n \
    --arg action "no_regression" \
    --arg status "$status" \
    --arg trend_file "$trend_json" \
    '{action:$action,status:$status,trend_file:$trend_file}'
  exit 0
fi

fingerprint_src="${generated_at}|${signals_csv}|${current_cov}|${current_uncovered}"
fingerprint="$(printf '%s' "$fingerprint_src" | shasum -a 256 | awk '{print $1}')"
external_ref="auto-regression:${fingerprint:0:16}"

existing_id="$(br list --json | jq -r --arg ref "$external_ref" '.[] | select((.external_ref // "") == $ref and .status != "closed") | .id' | head -n 1)"
if [[ -n "$existing_id" ]]; then
  jq -n \
    --arg action "already_open" \
    --arg issue_id "$existing_id" \
    --arg external_ref "$external_ref" \
    --arg trend_file "$trend_json" \
    '{action:$action,issue_id:$issue_id,external_ref:$external_ref,trend_file:$trend_file}'
  exit 0
fi

title="[AUTO][J4.3] Regression remediation $(date -u +%Y-%m-%d)"
description="$(cat <<EOF
Auto-created from nightly quality trend regression.

- Source trend file: \`${trend_json}\`
- Generated at: \`${generated_at}\`
- Regression signals: \`${signals_csv}\`
- Current aggregate coverage: \`${current_cov}%\`
- Current uncovered scenarios: \`${current_uncovered}\`

Required actions:
1. Reproduce regression with current CI/nightly commands.
2. Identify first failing condition and root cause.
3. Submit deterministic fix with verification evidence.
4. Update baselines only if change is intentional and approved.
EOF
)"

create_args=(
  --title "$title"
  --type task
  --priority 1
  --description "$description"
  --owner "$owner"
  --parent "$parent_issue"
  --deps "blocks:${blocking_dep}"
  --labels "regression,auto-created,j4_3"
  --due "$sla_due"
  --external-ref "$external_ref"
  --actor "$actor"
  --json
)
if [[ -n "$assignee" ]]; then
  create_args+=(--assignee "$assignee")
fi
if [[ "$dry_run" == "1" ]]; then
  create_args+=(--dry-run)
fi

create_json="$(br create "${create_args[@]}")"
if [[ "$dry_run" == "1" ]]; then
  jq -n \
    --arg action "dry_run_created" \
    --arg external_ref "$external_ref" \
    --argjson preview "$create_json" \
    '{action:$action,external_ref:$external_ref,preview:$preview}'
  exit 0
fi

issue_id="$(jq -r '.[0].id // .id // ""' <<<"$create_json")"
jq -n \
  --arg action "created" \
  --arg issue_id "$issue_id" \
  --arg external_ref "$external_ref" \
  --arg owner "$owner" \
  --arg due "$sla_due" \
  --arg parent "$parent_issue" \
  '{action:$action,issue_id:$issue_id,external_ref:$external_ref,owner:$owner,due:$due,parent:$parent}'
