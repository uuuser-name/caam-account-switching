#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

tmp_root="$(mktemp -d)"
trap 'rm -rf "$tmp_root"' EXIT

on_track_json="${tmp_root}/on_track.json"
regression_json="${tmp_root}/regression.json"

cat > "$on_track_json" <<'JSON'
{
  "generated_at": "2026-03-04T00:00:00Z",
  "status": "on_track",
  "current": {
    "aggregate_coverage": 64.2,
    "uncovered_scenarios": 153
  },
  "regression_signals": []
}
JSON

cat > "$regression_json" <<'JSON'
{
  "generated_at": "2026-03-04T00:01:00Z",
  "status": "regression",
  "current": {
    "aggregate_coverage": 63.0,
    "uncovered_scenarios": 160
  },
  "regression_signals": [
    "coverage_below_baseline",
    "uncovered_scenarios_above_baseline"
  ]
}
JSON

on_track_out="$(./scripts/auto_create_regression_bead.sh "$on_track_json")"
if [[ "$(jq -r '.action' <<<"$on_track_out")" != "no_regression" ]]; then
  echo "expected no_regression action for on-track input" >&2
  exit 1
fi

dry_run_out="$(REGRESSION_AUTOCREATE_DRY_RUN=1 ./scripts/auto_create_regression_bead.sh "$regression_json")"
if [[ "$(jq -r '.action' <<<"$dry_run_out")" != "dry_run_created" ]]; then
  echo "expected dry_run_created action for regression dry-run input" >&2
  exit 1
fi

echo "regression bead auto-create validator passed"
