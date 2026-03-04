#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

strict=0
json_mode=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --strict)
      strict=1
      ;;
    --json)
      json_mode=1
      ;;
    *)
      echo "unknown argument: $1" >&2
      echo "usage: $0 [--strict] [--json]" >&2
      exit 2
      ;;
  esac
  shift
done

./scripts/test_audit.sh >/dev/null

violations_path="artifacts/test-audit/mock_fake_stub_by_file.json"
if [[ ! -f "$violations_path" ]]; then
  echo "missing artifact: $violations_path" >&2
  exit 1
fi

violations="$(jq '[.[] | select(.severity=="violation")] | length' "$violations_path")"
allowed="$(jq '[.[] | select(.severity=="allowed")] | length' "$violations_path")"
total="$(jq 'length' "$violations_path")"

if [[ "$json_mode" -eq 1 ]]; then
  jq -n \
    --arg generated_at "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
    --argjson strict "$strict" \
    --argjson total "$total" \
    --argjson allowed "$allowed" \
    --argjson violations "$violations" \
    --arg violations_path "$violations_path" \
    '{
      generated_at_utc: $generated_at,
      strict_mode: ($strict == 1),
      total_matches: $total,
      allowed_matches: $allowed,
      violation_matches: $violations,
      violations_path: $violations_path,
      status: (if $violations == 0 then "pass" elif $strict == 1 then "fail" else "warn" end)
    }'
fi

if [[ "$violations" -eq 0 ]]; then
  if [[ "$json_mode" -eq 0 ]]; then
    echo "test realism lint passed: no core-scope undocumented doubles detected"
    echo "artifact: $violations_path"
  fi
  exit 0
fi

if [[ "$json_mode" -eq 0 ]]; then
  echo "test realism lint found $violations core-scope undocumented doubles"
  jq -r '.[] | select(.severity=="violation") | "- \(.file):\(.line) term=\(.term) owner=\(.owner_hint)"' "$violations_path"
  echo
  echo "artifact: $violations_path"
  echo "To resolve: either remove doubles in core paths or add a justified boundary rule to docs/testing/test_realism_allowlist.json."
fi

if [[ "$strict" -eq 1 ]]; then
  exit 1
fi
