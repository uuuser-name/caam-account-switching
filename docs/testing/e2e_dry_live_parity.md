# E2E Dry-Run vs Live-Run Parity

Purpose:
- Ensure dry-run and live-run switching workflows preserve the same step-level transition semantics.

Scope checked by `scripts/verify_e2e_dry_live_parity.sh`:
- identical event count,
- identical step order (`step_id`),
- balanced `*-start`/`*-end` phase pairs per `step_base`,
- identical transition semantics per step:
  - `component`,
  - `decision`,
  - `error.present`,
  - `error.code` (when `error.present=true`),
  - normalized `input_redacted` and `output` payloads after removing volatile fields.

What is intentionally ignored:
- top-level volatile metadata (`run_id`, timestamps, duration deltas).
- payload `mode` fields (`dry-run` vs `live-run`).
- volatile payload IDs (`request_id`, `trace_id`, `auth_code`, `resume_id`, etc.).

## Usage

```bash
./scripts/verify_e2e_dry_live_parity.sh <dry_log.jsonl> <live_log.jsonl>
```

Fixture validator:

```bash
./scripts/validate_e2e_dry_live_parity_fixtures.sh
```

Make target:

```bash
make validate-e2e-parity
```

Schema validation:
- Enabled by default through `scripts/validate_e2e_log_schema.sh`.
- Disable only for targeted debugging:

```bash
E2E_PARITY_VALIDATE_SCHEMA=0 ./scripts/verify_e2e_dry_live_parity.sh <dry> <live>
```
