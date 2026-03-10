#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MATRIX_JSON="${1:-$ROOT_DIR/artifacts/cli-matrix/cli_workflow_matrix.json}"
INVENTORY_JSON="${2:-$ROOT_DIR/artifacts/test-audit/e2e_inventory.json}"
OUT_JSON="${3:-$ROOT_DIR/artifacts/cli-matrix/scenario_traceability.json}"
OUT_MD="${4:-$ROOT_DIR/docs/testing/cli_scenario_traceability.md}"
BINDINGS_JSON="${5:-$ROOT_DIR/artifacts/cli-matrix/scenario_test_bindings.json}"
bindings_src="$BINDINGS_JSON"
bindings_mode="explicit"
tmp_bindings=""

cleanup() {
  if [[ -n "$tmp_bindings" && -f "$tmp_bindings" ]]; then
    rm -f "$tmp_bindings"
  fi
}
trap cleanup EXIT

mkdir -p "$(dirname "$OUT_JSON")" "$(dirname "$OUT_MD")"

if [[ ! -f "$MATRIX_JSON" ]]; then
  echo "missing CLI workflow matrix artifact: $MATRIX_JSON" >&2
  echo "provide MATRIX_JSON explicitly or generate artifacts/cli-matrix/cli_workflow_matrix.json first" >&2
  exit 2
fi

if [[ ! -f "$INVENTORY_JSON" ]]; then
  echo "missing e2e inventory artifact: $INVENTORY_JSON" >&2
  echo "run ./scripts/test_audit.sh before generating scenario traceability" >&2
  exit 2
fi

if [[ ! -f "$BINDINGS_JSON" ]]; then
  tmp_bindings="$(mktemp)"
  printf '{"bindings":[]}\n' >"$tmp_bindings"
  bindings_src="$tmp_bindings"
  bindings_mode="fallback_empty"
fi

# First pass: use explicit bindings if available, fall back to heuristic matching
jq -n --slurpfile m "$MATRIX_JSON" --slurpfile i "$INVENTORY_JSON" --slurpfile b "$bindings_src" '
  def norm: ascii_downcase | gsub("[^a-z0-9]+"; "");
  
  def binding_lookup:
    [($b[0].bindings // [])[] | {key: (.family + ":" + .scenario_type + ":" + .required_scenario), value: .}]
    | from_entries;
  
  def matches_explicit($fam; $stype; $sid; $lookup):
    ($lookup[$fam + ":" + $stype + ":" + $sid] // null) as $binding
    | if $binding then
        [{
          scenario_id: $binding.bound_test,
          file: $binding.test_file,
          owner: ($binding.owner // "unassigned"),
          workflow: ($binding.bound_test | split("_")[0] // "unknown"),
          binding_confidence: $binding.binding_confidence,
          binding_source: "explicit"
        }]
      else [] end;
  
  def matches_heuristic($sid):
    [ $i[0].scenario_catalog[] as $cat
      | select((($cat.scenario_id|norm)|contains($sid|norm)) or (($sid|norm)|contains($cat.scenario_id|norm)))
      | {scenario_id: $cat.scenario_id, file: $cat.file, owner: $cat.owner, workflow: $cat.workflow, binding_source: "heuristic"}
    ];

  (binding_lookup) as $lookup
  | [
    $m[0].command_families[] as $f
    | ["happy","failure","edge"][] as $stype
    | ($f.required_scenarios[$stype] // [])[] as $sid
    | (matches_explicit($f.family; $stype; $sid; $lookup)) as $explicit_matches
    | (if ($explicit_matches | length) > 0 then $explicit_matches else matches_heuristic($sid) end) as $matches
    | {
        family: $f.family,
        scenario_type: $stype,
        required_scenario: $sid,
        coverage_status: (if ($matches|length) > 0 then "covered" else "uncovered" end),
        matches: $matches,
        owner: (if ($matches|length) > 0 then ($matches[0].owner // "unassigned") else "unassigned" end),
        binding_source: (if ($matches|length) > 0 then ($matches[0].binding_source // "heuristic") else "none" end)
      }
  ] as $rows
  | {
      generated_at: (now | todateiso8601),
      source_bead: "bd-1r67.3.3.3",
      bindings_file: $BINDINGS_JSON,
      bindings_mode: $BINDINGS_MODE,
      totals: {
        required_scenarios: ($rows|length),
        covered: ([ $rows[] | select(.coverage_status=="covered") ] | length),
        uncovered: ([ $rows[] | select(.coverage_status=="uncovered") ] | length),
        explicit_bindings: ([ $rows[] | select(.binding_source=="explicit") ] | length),
        heuristic_matches: ([ $rows[] | select(.binding_source=="heuristic") ] | length)
      },
      rows: $rows
    }
' --arg BINDINGS_JSON "$BINDINGS_JSON" --arg BINDINGS_MODE "$bindings_mode" > "$OUT_JSON"

covered="$(jq -r '.totals.covered' "$OUT_JSON")"
uncovered="$(jq -r '.totals.uncovered' "$OUT_JSON")"
total="$(jq -r '.totals.required_scenarios' "$OUT_JSON")"
explicit="$(jq -r '.totals.explicit_bindings // 0' "$OUT_JSON")"
heuristic="$(jq -r '.totals.heuristic_matches // 0' "$OUT_JSON")"

{
  cat <<EOM
# CLI Scenario Traceability Map

Generated: $(date -u +"%Y-%m-%dT%H:%M:%SZ")
Source bead: bd-1r67.3.3.3

## Summary
- Required scenarios: ${total}
- Covered: ${covered} (${explicit} explicit bindings, ${heuristic} heuristic matches)
- Uncovered: ${uncovered}
- Machine-readable map: artifacts/cli-matrix/scenario_traceability.json
- Explicit bindings: artifacts/cli-matrix/scenario_test_bindings.json

## Uncovered Scenarios (Top 50)

| Family | Type | Required Scenario | Suggested Owner |
|---|---|---|---|
EOM
  jq -r '.rows | map(select(.coverage_status=="uncovered")) | .[:50] | .[] | "| `" + .family + "` | `" + .scenario_type + "` | `" + .required_scenario + "` | `" + .owner + "` |"' "$OUT_JSON"

  cat <<'EOM'

## Notes
- Explicit bindings are declared in `artifacts/cli-matrix/scenario_test_bindings.json`.
- Heuristic matching uses normalized string similarity between required scenario names and existing test scenario IDs.
- Exact scenario-to-test bindings should be refined as C3.2 fills matrix deficits.
EOM
} > "$OUT_MD"

echo "wrote $OUT_JSON"
echo "wrote $OUT_MD"
