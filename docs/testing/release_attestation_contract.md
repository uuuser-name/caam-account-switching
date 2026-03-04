# Release Attestation Contract (J4.4)

Release promotion must fail closed unless `scripts/release_readiness_attestation.sh` reports `pass=true`.

## Required Inputs

- `artifacts/test-audit/test_audit.json`
- `artifacts/test-audit/e2e_quality_gate.json`
- `artifacts/test-audit/quality_trend_diff.json`

## Mandatory Checks

1. `no_mock_violations`
2. `e2e_quality_gate_pass`
3. `trend_not_regression`
4. `traceability_totals_consistent`
5. `aggregate_coverage_floor` (default floor: `60%`, override with `RELEASE_MIN_AGG_COVERAGE`)

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

If any check fails, the script exits non-zero and release promotion must halt.
