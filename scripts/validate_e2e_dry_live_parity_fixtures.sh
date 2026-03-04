#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

dry="docs/testing/e2e_dry_run_sample.jsonl"
live="docs/testing/e2e_live_run_sample.jsonl"
mismatch="docs/testing/e2e_live_run_mismatch_sample.jsonl"
profile_mismatch="docs/testing/e2e_live_run_profile_target_mismatch_sample.jsonl"

./scripts/verify_e2e_dry_live_parity.sh "$dry" "$live"

if ./scripts/verify_e2e_dry_live_parity.sh "$dry" "$mismatch" >/dev/null 2>&1; then
  echo "mismatch fixture unexpectedly passed parity check: $mismatch" >&2
  exit 1
fi

echo "mismatch fixture correctly failed parity check: $mismatch"

if ./scripts/verify_e2e_dry_live_parity.sh "$dry" "$profile_mismatch" >/dev/null 2>&1; then
  echo "profile target mismatch fixture unexpectedly passed parity check: $profile_mismatch" >&2
  exit 1
fi

echo "profile target mismatch fixture correctly failed parity check: $profile_mismatch"
