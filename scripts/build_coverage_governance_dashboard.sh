#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

mkdir -p artifacts/coverage-governance

PACKAGES=(
  "./cmd/caam/cmd"
  "./internal/exec"
  "./internal/coordinator"
  "./internal/agent"
  "./internal/deploy"
  "./internal/sync"
  "./internal/setup"
  "./internal/provider/claude"
  "./internal/provider/codex"
  "./internal/provider/gemini"
  "./internal/tailscale"
)

get_tier() {
  case "$1" in
    "./cmd/caam/cmd"|"./internal/exec"|"./internal/coordinator"|"./internal/agent") echo "A" ;;
    "./internal/deploy"|"./internal/sync"|"./internal/setup"|"./internal/provider/claude"|"./internal/provider/codex"|"./internal/provider/gemini") echo "B" ;;
    *) echo "C" ;;
  esac
}

get_owner() {
  case "$1" in
    "./cmd/caam/cmd") echo "cli-team" ;;
    "./internal/exec") echo "runtime-team" ;;
    "./internal/coordinator") echo "coordinator-team" ;;
    "./internal/agent") echo "agent-team" ;;
    "./internal/deploy") echo "deploy-team" ;;
    "./internal/sync") echo "sync-team" ;;
    "./internal/setup") echo "setup-team" ;;
    "./internal/provider/claude"|"./internal/provider/codex"|"./internal/provider/gemini") echo "provider-team" ;;
    *) echo "infra-team" ;;
  esac
}

get_thresholds() {
  case "$1" in
    A) echo "80 90 95" ;;
    B) echo "70 80 90" ;;
    C) echo "60 70 80" ;;
  esac
}

json_tmp="artifacts/coverage-governance/dashboard_rows.jsonl"
: > "$json_tmp"

GOCACHE="$ROOT_DIR/.cache/go-build"
GOMODCACHE="$ROOT_DIR/.cache/go-mod"
GOPATH="$ROOT_DIR/.cache/go-path"
mkdir -p "$GOCACHE" "$GOMODCACHE" "$GOPATH"

for pkg in "${PACKAGES[@]}"; do
  tier="$(get_tier "$pkg")"
  owner="$(get_owner "$pkg")"
  read -r floor target stretch <<< "$(get_thresholds "$tier")"

  cov_profile="$(mktemp)"
  out="$(GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" GOPATH="$GOPATH" go test -coverprofile="$cov_profile" "$pkg" -count=1 2>&1 || true)"
  cov="$(GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" GOPATH="$GOPATH" go tool cover -func="$cov_profile" 2>/dev/null | awk '/^total:/{gsub(/%/,"",$3); print $3}' | tail -n 1)"
  rm -f "$cov_profile"
  if [[ -z "$cov" ]]; then
    cov="$(printf '%s\n' "$out" | sed -n 's/.*coverage: \([0-9.]*\)%.*/\1/p' | tail -n 1)"
  fi
  if [[ -z "$cov" ]]; then
    cov="0.0"
  fi

  status="on_track"
  if awk "BEGIN {exit !($cov < $floor)}"; then
    status="regression"
  elif awk "BEGIN {exit !($cov >= $target)}"; then
    status="above_target"
  fi

  jq -cn \
    --arg package "$pkg" \
    --arg tier "$tier" \
    --arg owner "$owner" \
    --arg status "$status" \
    --argjson coverage "$cov" \
    --argjson floor "$floor" \
    --argjson target "$target" \
    --argjson stretch "$stretch" \
    '{package:$package,tier:$tier,owner:$owner,coverage:$coverage,floor:$floor,target:$target,stretch:$stretch,status:$status}' \
    >> "$json_tmp"
done

jq -s '{generated_at:(now|todate),rows:.}' "$json_tmp" > artifacts/coverage-governance/dashboard.json

{
  echo "# Coverage Governance Dashboard"
  echo
  echo "Generated: $(date -u +'%Y-%m-%dT%H:%M:%SZ')"
  echo
  echo "| Package | Tier | Coverage | Floor | Target | Stretch | Status | Owner |"
  echo "|---|---|---:|---:|---:|---:|---|---|"
  jq -r '.rows[] | "| `\(.package)` | \(.tier) | \(.coverage)% | \(.floor)% | \(.target)% | \(.stretch)% | \(.status) | \(.owner) |"' artifacts/coverage-governance/dashboard.json
  echo
  echo "## Hotspots"
  jq -r '.rows[] | select(.status=="regression") | "- `\(.package)` below floor by \((.floor - .coverage)|tostring)% (owner: \(.owner))"' artifacts/coverage-governance/dashboard.json
} > docs/testing/coverage_governance_dashboard.md

echo "Wrote artifacts/coverage-governance/dashboard.json and docs/testing/coverage_governance_dashboard.md"
