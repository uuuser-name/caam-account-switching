#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

schema="docs/testing/e2e_log_schema.json"
valid="docs/testing/e2e_log_sample.jsonl"
invalid_fixtures=(
  "docs/testing/e2e_log_invalid_sample.jsonl"
  "docs/testing/e2e_log_invalid_shape_sample.jsonl"
  "docs/testing/e2e_log_invalid_unbalanced_steps.jsonl"
)

./scripts/validate_e2e_log_schema.sh "$schema" "$valid"

for invalid in "${invalid_fixtures[@]}"; do
  if ./scripts/validate_e2e_log_schema.sh "$schema" "$invalid" >/dev/null 2>&1; then
    echo "invalid fixture unexpectedly passed: $invalid" >&2
    exit 1
  fi

  echo "invalid fixture correctly failed: $invalid"
done
