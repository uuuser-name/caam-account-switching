# Release Attestation Contract (J4.4)

Deterministic release gating must fail closed unless `scripts/release_readiness_attestation.sh` reports `pass=true`.

The same attestation must also report separate truth labels for deterministic local proof, installed-binary proof, and bounded-live proof. `full_closure_pass=true` is required before the project can claim full closure across all three proof surfaces.

## Required Inputs

- `artifacts/test-audit/test_audit.json`
- `artifacts/test-audit/e2e_quality_gate.json`
  - This summary artifact must be emitted on both pass and fail paths so downstream attestation can fail closed with a concrete `failure_reason` and `stage_results` record.
- `artifacts/test-audit/quality_trend_diff.json`

## Optional Explicit Inputs

- installed-binary proof is counted only when `RELEASE_INSTALLED_VALIDATION_JSON` or the matching run-artifact-index input is explicitly set
- bounded-live proof is counted only when `RELEASE_BOUNDED_LIVE_VALIDATION_JSON` or the matching run-artifact-index input is explicitly set
- otherwise those truth labels must remain `not_run` rather than being inferred from any stale default file on disk

## Mandatory Deterministic Checks

1. `no_mock_violations`
2. `e2e_quality_gate_pass`
3. `trend_not_regression`
4. `traceability_totals_consistent`
5. `aggregate_coverage_floor` (default floor: `60%`, override with `RELEASE_MIN_AGG_COVERAGE`)

## Required Truth Labels

- `truth_labels.deterministic`: `deterministic_green` or `failed`
- `truth_labels.installed_binary`: `installed_binary_green`, `failed`, or `not_run`
- `truth_labels.bounded_live`: `bounded_live_green`, `failed`, or `not_run`

## Pass Semantics

- `pass=true`: deterministic local release gate is green
- `full_closure_pass=true`: deterministic local gate is green and both installed-binary and bounded-live proof surfaces are green
- If installed-binary or bounded-live proof has not run, the attestation must label that surface `not_run` rather than implying success

## Required Outputs

- `artifacts/test-audit/release_attestation.json`
- `artifacts/test-audit/release_attestation.md`

## Usage

```bash
./scripts/release_readiness_attestation.sh
```

Make target:

```bash
make release-attestation
```

If any deterministic check fails, the script exits non-zero and release promotion must halt. If deterministic checks pass but installed-binary or bounded-live proof is `not_run`, the script may still succeed, but release notes and README claims must not imply full closure.
