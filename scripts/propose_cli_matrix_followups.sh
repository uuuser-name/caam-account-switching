#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TRACE_JSON="${TRACE_JSON:-$ROOT_DIR/artifacts/cli-matrix/scenario_traceability.json}"
PARENT_BEAD="${PARENT_BEAD:-bd-1r67.3.3.4}"
LIMIT="${LIMIT:-25}"
APPLY="${APPLY:-0}"
OUT_JSON="${OUT_JSON:-$ROOT_DIR/artifacts/cli-matrix/followup_proposals.json}"

if [[ ! -f "$TRACE_JSON" ]]; then
  echo "traceability file not found: $TRACE_JSON" >&2
  exit 1
fi

mkdir -p "$(dirname "$OUT_JSON")"

jq -n --slurpfile t "$TRACE_JSON" --arg parent "$PARENT_BEAD" --argjson limit "$LIMIT" '
  [ $t[0].rows[]
    | select(.coverage_status == "uncovered")
    | {
        parent: $parent,
        title: ("C3 follow-up: " + .family + " / " + .scenario_type + " / " + .required_scenario),
        description: (
          "Auto-generated from scenario traceability gap scan.\n"
          + "Family: " + .family + "\n"
          + "Scenario type: " + .scenario_type + "\n"
          + "Required scenario: " + .required_scenario + "\n"
          + "Source bead: bd-1r67.3.3.4\n"
          + "Expected outcome: implement/attach deterministic test coverage and link artifact evidence."
        ),
        priority: 2,
        issue_type: "task",
        family: .family,
        scenario_type: .scenario_type,
        required_scenario: .required_scenario
      }
  ][0:$limit]
' > "$OUT_JSON"

count="$(jq 'length' "$OUT_JSON")"
echo "Generated $count follow-up proposals: $OUT_JSON"

if [[ "$APPLY" == "1" ]]; then
  while IFS= read -r row; do
    title="$(jq -r '.title' <<<"$row")"
    desc="$(jq -r '.description' <<<"$row")"
    br create --title "$title" --type task --priority 2 --description "$desc" --parent "$PARENT_BEAD" >/dev/null
    echo "created: $title"
  done < <(jq -c '.[]' "$OUT_JSON")
else
  echo "Dry-run only. Set APPLY=1 to create beads."
  jq -r '.[] | "- " + .title' "$OUT_JSON"
fi
