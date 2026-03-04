# Coverage Governance Policy

## Purpose
Coverage targets are risk-weighted so we optimize for user-impacting reliability, not raw percentages.

## Scope
This policy governs Workstream B coverage signoff and CI ratchet behavior for package-level coverage floors.

## Risk Model (B5.1)
Risk score uses three factors, each scored 1-5:
- User impact: how directly user-facing failures are.
- Failure cost: severity and blast radius of defects.
- Change velocity: how often code changes and regression risk.

Risk score formula:
- `risk_score = user_impact + failure_cost + change_velocity`

Tier mapping:
- Tier A (Critical): score >= 12
- Tier B (High): score 9-11
- Tier C (Medium): score <= 8

## Package Criticality Map (B5.1)
| Package | User Impact | Failure Cost | Change Velocity | Risk Score | Tier | Owner |
|---|---:|---:|---:|---:|---|---|
| `./cmd/caam/cmd` | 5 | 5 | 5 | 15 | A | cli-team |
| `./internal/exec` | 5 | 5 | 4 | 14 | A | runtime-team |
| `./internal/coordinator` | 5 | 4 | 4 | 13 | A | coordinator-team |
| `./internal/agent` | 4 | 4 | 4 | 12 | A | agent-team |
| `./internal/deploy` | 4 | 4 | 3 | 11 | B | deploy-team |
| `./internal/sync` | 4 | 4 | 3 | 11 | B | sync-team |
| `./internal/setup` | 4 | 3 | 3 | 10 | B | setup-team |
| `./internal/provider/claude` | 4 | 3 | 3 | 10 | B | provider-team |
| `./internal/provider/codex` | 4 | 3 | 3 | 10 | B | provider-team |
| `./internal/provider/gemini` | 4 | 3 | 3 | 10 | B | provider-team |
| `./internal/tailscale` | 3 | 3 | 2 | 8 | C | infra-team |

## Tiered Thresholds (B5.2)
| Tier | Floor | Target | Stretch |
|---|---:|---:|---:|
| A | 80% | 90% | 95% |
| B | 70% | 80% | 90% |
| C | 60% | 70% | 80% |

CI gate requirements:
- PR gate: package must remain >= floor.
- Nightly gate: package trend should move toward target.
- Release gate: Tier A packages should be at or above target unless approved exception exists.

## Ratchet Algorithm (B5.4)
Ratchet is monotonic and package-local:
1. `effective_floor[pkg]` starts at policy floor for tier.
2. If current coverage < `effective_floor[pkg]`, gate fails.
3. If current coverage >= (`effective_floor[pkg] + 5`) for 2 consecutive runs, increase `effective_floor[pkg]` by 2.
4. `effective_floor[pkg]` never decreases unless approved exception exists.
5. `effective_floor[pkg]` cannot exceed tier target.

## Exception Policy (B5.4)
Exception process is defined in:
- `docs/testing/exception_approval_process.md`

Rules:
- Exception must include owner, reason, expiry, and follow-up bead.
- Exception is time-bounded and auditable.
- Expired exceptions fail CI until renewed or removed.

## Anti-Gaming Controls (B5.5)
Checklist and review controls are defined in:
- `docs/testing/pr_checklist.md`

Additional mandatory spot-audits for Tier A packages:
- assert behavioral checks for high-risk branches,
- verify tests fail when core logic is intentionally broken,
- reject coverage-only tests that assert implementation details without user-observable outcomes.

## Dashboard and Trend Readout (B5.6)
Dashboard generator:
- `scripts/build_coverage_governance_dashboard.sh`

Outputs:
- `docs/testing/coverage_governance_dashboard.md`
- `artifacts/coverage-governance/dashboard.json`

The dashboard highlights:
- current coverage vs floor/target/stretch,
- ratchet status (`regression`, `on_track`, `above_target`),
- open hotspots requiring owner action.
