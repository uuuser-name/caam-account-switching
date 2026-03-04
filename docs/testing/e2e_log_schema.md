# Canonical E2E JSONL Log Schema

This document defines the required per-event schema for e2e workflow logs.
Each line in a `.jsonl` e2e log must be one JSON object matching this contract.

Schema version: `1.1.0`
Compatibility policy: `docs/testing/e2e_log_schema_policy.md`
Redaction rules: `docs/testing/e2e_redaction_rules.json`

## Required Fields

- `run_id`: Stable ID for a full e2e run.
- `scenario_id`: Stable scenario key from the workflow matrix.
- `step_id`: Stable step key inside the scenario.
- `timestamp`: RFC3339 UTC timestamp.
- `actor`: Human or automation actor.
- `component`: Subsystem touched by the step.
- `input_redacted`: Input summary with secrets removed.
- `output`: Output summary (state changes, snippets, artifacts).
- `decision`: Step decision (`pass`, `retry`, `abort`, `continue`).
- `duration_ms`: Step latency in milliseconds.
- `error`: Structured envelope with:
  - `present` (boolean)
  - `code` (string)
  - `message` (string)
  - `details` (object)

## Validation Rules

- All required keys must be present on every line.
- `timestamp` must parse as RFC3339.
- `duration_ms` must be an integer `>= 0`.
- When `error.present=false`, `error.code` and `error.message` should be empty strings.
- `input_redacted` must not include raw tokens or credentials.

## Validation Command

Run the canonical validator against the sample fixture:

```bash
make validate-e2e-schema
```

This command validates both:
- positive fixture: `docs/testing/e2e_log_sample.jsonl` (must pass)
- negative fixture: `docs/testing/e2e_log_invalid_sample.jsonl` (must fail)

Or run directly with custom paths:

```bash
./scripts/validate_e2e_log_schema.sh docs/testing/e2e_log_schema.json path/to/log.jsonl
```

## Example Event

```json
{
  "run_id": "run-20260302-001",
  "scenario_id": "switch-rate-limit-recovery",
  "step_id": "auth-swap",
  "timestamp": "2026-03-02T13:30:00Z",
  "actor": "ci",
  "component": "authpool",
  "input_redacted": {
    "provider": "claude",
    "profile": "primary"
  },
  "output": {
    "result": "swapped",
    "artifact": "artifacts/e2e/run-20260302-001/swap.log"
  },
  "decision": "continue",
  "duration_ms": 182,
  "error": {
    "present": false,
    "code": "",
    "message": "",
    "details": {}
  }
}
```
