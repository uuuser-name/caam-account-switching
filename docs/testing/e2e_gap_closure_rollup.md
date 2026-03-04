# E2E Gap Closure Rollup (C2.4)

Generated: 2026-03-04
Source bead: `bd-1r67.3.2.4`

## Objective
Track closure state of externally-linked E2E gap tasks required before C3 matrix deficit closure can be certified.

## External Dependency Status

| Bead ID | Title | Status |
|---|---|---|
| `caam-pqkj.1` | Task: Watcher E2E tests + auth-change fixtures | open |
| `caam-6sao.2` | Task: WezTerm recovery E2E script + fixtures | open |
| `caam-3ezz.4` | Task: Distributed auth E2E smoke scripts + verbose logs | open |
| `caam-ldxo.6` | Task: Installer + update E2E tests | open |
| `caam-l19o.1.13` | Task: TUI interaction E2E tests (teatest) | open |

## Certification Checklist

- [ ] Watcher/auth-change fixtures complete (`caam-pqkj.1`)
- [ ] WezTerm recovery fixtures complete (`caam-6sao.2`)
- [ ] Distributed auth smoke complete (`caam-3ezz.4`)
- [ ] Installer/update E2E complete (`caam-ldxo.6`)
- [ ] TUI interaction E2E polish complete (`caam-l19o.1.13`)
- [ ] Linked evidence comments posted to `bd-1r67.3.2.4`

## Current Outcome
Not yet certifiable: all five linked external gap tasks remain open as of this snapshot.

## J1.3 Account-Switch Exhaustion Parity Audit (2026-03-04)

Focused ask: automatic profile switching when one account is exhausted (five-hour quota or weekly credits), with deterministic and diagnosable behavior.

| Required scenario | Current evidence | Status | Follow-up bead |
|---|---|---|---|
| Auto-select alternate profile when one profile is rate-limited | `internal/e2e/workflows/rotation_cooldown_test.go` (`TestE2E_CooldownEnforcesDuringAutoRotation`) | covered | `bd-1r67.13.3.1` |
| Recover previously rate-limited profile after cooldown expiry/clear | `rotation_cooldown_test.go` (`clear_cooldown` step), `ratelimit_recovery_test.go` | covered | `bd-1r67.13.3.1` |
| Deterministic failure when all profiles are exhausted | `rotation_cooldown_test.go` (`all_cooldown` step expects failure) | covered | `bd-1r67.13.3.1` |
| Weekly credits exhaustion semantics (provider-level quota exhaustion) | `internal/e2e/workflows/rotation_exhaustion_test.go` (`TestE2E_WeeklyCreditExhaustionSwitch`) | covered | `bd-1r67.13.3.2` |
| Five-hour quota exhaustion semantics tied to Codex credit windows | `internal/e2e/workflows/rotation_exhaustion_test.go` (`TestE2E_FiveHourCreditExhaustionSwitch`) | covered | `bd-1r67.13.3.2` |
| Frictionless in-flight handoff for active Codex sessions during exhaustion | `internal/e2e/workflows/rotation_exhaustion_test.go` (`TestE2E_ActiveSessionHandoffContinuityUnderExhaustion`) | covered | `bd-1r67.13.3.2` |
| Canonical correlation/timeline diagnosability gate for every switching E2E run | Logging exists (`StartStep`/`EndStep`/summary) but gate enforcement remains open | partial | `bd-1r67.13.3.3` |

### Parity Metrics Snapshot

- Scenario traceability matrix requirements: 227
- Covered by explicit bindings: 74
- Uncovered requirements: 153
- Source artifact: `artifacts/cli-matrix/scenario_traceability.json`
