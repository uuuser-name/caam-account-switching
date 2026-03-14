#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

out_json="${RUN_ARTIFACT_INDEX_OUT_JSON:-artifacts/test-audit/run_artifact_index.json}"

audit_json="${RUN_ARTIFACT_INDEX_AUDIT_JSON:-artifacts/test-audit/test_audit.json}"
gate_json="${RUN_ARTIFACT_INDEX_GATE_JSON:-artifacts/test-audit/e2e_quality_gate.json}"
trend_json="${RUN_ARTIFACT_INDEX_TREND_JSON:-artifacts/test-audit/quality_trend_diff.json}"
inventory_json="${RUN_ARTIFACT_INDEX_INVENTORY_JSON:-artifacts/test-audit/e2e_inventory.json}"
traceability_json="${RUN_ARTIFACT_INDEX_TRACEABILITY_JSON:-artifacts/cli-matrix/scenario_traceability.json}"
traceability_md="${RUN_ARTIFACT_INDEX_TRACEABILITY_MD:-artifacts/cli-matrix/cli_scenario_traceability.md}"
coverage_baseline_json="${RUN_ARTIFACT_INDEX_COVERAGE_BASELINE_JSON:-docs/testing/coverage_baseline.json}"
traceability_baseline_json="${RUN_ARTIFACT_INDEX_TRACEABILITY_BASELINE_JSON:-docs/testing/e2e_traceability_baseline.json}"
release_attestation_json="${RUN_ARTIFACT_INDEX_RELEASE_ATTESTATION_JSON:-artifacts/test-audit/release_attestation.json}"
installed_validation_json="${RUN_ARTIFACT_INDEX_INSTALLED_VALIDATION_JSON:-}"
bounded_live_json="${RUN_ARTIFACT_INDEX_BOUNDED_LIVE_VALIDATION_JSON:-}"
failure_packet_registry_index="${RUN_ARTIFACT_INDEX_FAILURE_PACKET_REGISTRY:-artifacts/failure-packets-registry/index.json}"

sha256_or_null() {
  local path="$1"
  if [[ -f "$path" ]]; then
    shasum -a 256 "$path" | awk '{print $1}'
  else
    echo ""
  fi
}

size_or_zero() {
  local path="$1"
  if [[ -f "$path" ]]; then
    wc -c < "$path" | tr -d ' '
  else
    echo "0"
  fi
}

artifact_entry() {
  local path="$1"
  local kind="$2"
  local producer="$3"
  local exists="false"
  if [[ -f "$path" ]]; then
    exists="true"
  fi

  jq -n \
    --arg path "$path" \
    --arg kind "$kind" \
    --arg producer "$producer" \
    --arg exists "$exists" \
    --arg sha256 "$(sha256_or_null "$path")" \
    --argjson size_bytes "$(size_or_zero "$path")" '
    {
      path: $path,
      kind: $kind,
      producer: $producer,
      exists: ($exists == "true"),
      sha256: (if $sha256 == "" then null else $sha256 end),
      size_bytes: $size_bytes
    }
  '
}

producer_entry() {
  local path="$1"
  jq -n \
    --arg path "$path" \
    --arg sha256 "$(sha256_or_null "$path")" '
    {
      path: $path,
      sha256: (if $sha256 == "" then null else $sha256 end)
    }
  '
}

mkdir -p "$(dirname "$out_json")"

jq -n \
  --arg schema_version "1.0.0" \
  --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg repo_root "$repo_root" \
  --arg out_json "$out_json" \
  --arg audit_json "$audit_json" \
  --arg gate_json "$gate_json" \
  --arg trend_json "$trend_json" \
  --arg inventory_json "$inventory_json" \
  --arg traceability_json "$traceability_json" \
  --arg traceability_md "$traceability_md" \
  --arg coverage_baseline_json "$coverage_baseline_json" \
  --arg traceability_baseline_json "$traceability_baseline_json" \
  --arg release_attestation_json "$release_attestation_json" \
  --arg installed_validation_json "$installed_validation_json" \
  --arg bounded_live_json "$bounded_live_json" \
  --arg failure_packet_registry_index "$failure_packet_registry_index" \
  --argjson builder "$(producer_entry "scripts/build_run_artifact_index.sh")" \
  --argjson trend_builder "$(producer_entry "scripts/build_quality_trend_diff.sh")" \
  --argjson attestation_builder "$(producer_entry "scripts/release_readiness_attestation.sh")" \
  --argjson audit_entry "$(artifact_entry "$audit_json" "json" "scripts/test_audit.sh")" \
  --argjson gate_entry "$(artifact_entry "$gate_json" "json" "scripts/ci_e2e_quality_gate.sh")" \
  --argjson trend_entry "$(artifact_entry "$trend_json" "json" "scripts/build_quality_trend_diff.sh")" \
  --argjson inventory_entry "$(artifact_entry "$inventory_json" "json" "scripts/test_audit.sh")" \
  --argjson traceability_entry "$(artifact_entry "$traceability_json" "json" "scripts/build_cli_scenario_traceability.sh")" \
  --argjson traceability_md_entry "$(artifact_entry "$traceability_md" "markdown" "scripts/build_cli_scenario_traceability.sh")" \
  --argjson coverage_baseline_entry "$(artifact_entry "$coverage_baseline_json" "json" "docs/testing/coverage_baseline.json")" \
  --argjson traceability_baseline_entry "$(artifact_entry "$traceability_baseline_json" "json" "docs/testing/e2e_traceability_baseline.json")" \
  --argjson release_attestation_entry "$(artifact_entry "$release_attestation_json" "json" "scripts/release_readiness_attestation.sh")" \
  --argjson failure_packet_registry_entry "$(artifact_entry "$failure_packet_registry_index" "json" "scripts/failure_packet_ctl.sh")" '
  {
    schema_version: $schema_version,
    generated_at: $generated_at,
    repo_root: $repo_root,
    manifest_path: $out_json,
    producers: {
      artifact_index: $builder,
      quality_trend_diff: $trend_builder,
      release_readiness_attestation: $attestation_builder
    },
    consumer_inputs: {
      quality_trend_diff: {
        audit_json: $audit_json,
        gate_json: $gate_json,
        traceability_json: $traceability_json,
        coverage_baseline_json: $coverage_baseline_json,
        traceability_baseline_json: $traceability_baseline_json
      },
      release_readiness_attestation: {
        audit_json: $audit_json,
        gate_json: $gate_json,
        trend_json: $trend_json,
        installed_validation_json: (if $installed_validation_json == "" then null else $installed_validation_json end),
        bounded_live_json: (if $bounded_live_json == "" then null else $bounded_live_json end)
      }
    },
    artifacts: {
      test_audit_json: $audit_entry,
      e2e_quality_gate_json: $gate_entry,
      quality_trend_diff_json: $trend_entry,
      e2e_inventory_json: $inventory_entry,
      scenario_traceability_json: $traceability_entry,
      scenario_traceability_md: $traceability_md_entry,
      coverage_baseline_json: $coverage_baseline_entry,
      traceability_baseline_json: $traceability_baseline_entry,
      release_attestation_json: $release_attestation_entry,
      failure_packet_registry_index: $failure_packet_registry_entry
    }
  }
' > "$out_json"

echo "wrote ${out_json}"
