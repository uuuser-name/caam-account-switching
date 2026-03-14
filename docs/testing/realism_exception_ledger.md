# Realism exception ledger

Last updated: 2026-03-14

This ledger is the decision surface for broad or file-scoped entries in [test_realism_allowlist.json](/Users/hope/Documents/Projects/caam-account-switching/docs/testing/test_realism_allowlist.json). The goal is to keep the current test-audit claim truthful: broad exceptions are allowed only where the tests are genuinely boundary-facing or still on an explicit retirement path.

## Decision summary

| Prefix | Owner | Decision | Review by | Why it exists now | Retirement trigger |
| --- | --- | --- | --- | --- | --- |
| `./internal/provider/` | `provider-team` | Keep | 2026-06-01 | External provider CLIs and auth surfaces are boundary-facing. | Replace any remaining name-only doubles with fixture-backed provider state where feasible. |
| `./internal/tailscale/` | `network-team` | Keep | 2026-06-01 | Network and system integration boundary. | Keep unless a narrower file-level split becomes practical. |
| `./internal/deploy/` | `ops-team` | Keep | 2026-06-01 | Deployment flows are external-system boundary tests. | Keep unless deployment simulation becomes first-class and truthful. |
| `./internal/exec/` | `runtime-team` | Narrow over time | 2026-04-15 | Runtime subprocess and PTY control still needs isolation in some tests, but direct fixture coverage is now materially better. | Continue retiring double-heavy paths via `bd-3fy.2.1.2` and `bd-3fy.2.1.3` until only irreducible subprocess boundaries remain. |
| `./cmd/caam/cmd/add_test.go` | `cli-team` | Narrow retained | 2026-04-15 | Legacy command setup still uses fake-backed fixtures. | Remove after the legacy add-path fixture is replaced with temp-home real state. |
| `./cmd/caam/cmd/add_extended_test.go` | `cli-team` | Narrow retained | 2026-04-15 | Residual extended command fixture debt only. | Remove after add command coverage converges on real state fixtures. |
| `./cmd/caam/cmd/activate_extended_test.go` | `cli-team` | Narrow retained | 2026-04-15 | Legacy activation slices still use doubles; newer real coverage exists elsewhere. | Remove after extended activation slices are rewritten onto the real activation fixture path. |
| `./cmd/caam/cmd/auth_extended_test.go` | `cli-team` | Narrow retained | 2026-04-15 | Auth command coverage still has mock-backed helper seams. | Remove after auth command tests use real temp-home vault state only. |
| `./cmd/caam/cmd/bundle_export_import_sync_test.go` | `cli-team` | Narrow retained | 2026-04-15 | Bundle import/export sync still uses fake-backed setup. | Remove after bundle sync scenarios move to real filesystem fixtures. |
| `./cmd/caam/cmd/e2e_cli_test.go` | `cli-team` | Keep for now | 2026-05-15 | Command E2E still needs controlled doubles for external dependencies. | Narrow if individual external seams become fixture-backed and deterministic. |
| `./cmd/caam/cmd/run_extended_test.go` | `cli-team` | Narrow retained | 2026-04-15 | Legacy run/startup helper debt remains; newer real startup coverage exists. | Remove after extended run slices migrate to the real startup fixture path. |
| `./cmd/caam/cmd/vault_test.go` | `cli-team` | Narrow retained | 2026-04-15 | Some vault slices still use fake-backed fixtures. | Remove after vault command tests rely only on temp-dir vault state. |
| `./internal/testutil/` | `test-infra-team` | Keep | 2026-06-01 | Test infrastructure is boundary code by definition and often models external conditions. | Keep, but continue separating checker-source strings from actual runtime leakage. |
| `./internal/e2e/workflows/` | `e2e-team` | Keep | 2026-06-01 | E2E workflows intentionally exercise external seams with controlled doubles where needed. | Keep unless bounded-live coverage can fully replace a specific synthetic seam. |
| `./internal/pty/` | `platform-team` | Keep | 2026-06-01 | OS-specific PTY behavior requires platform-specific seams. | Keep unless platform coverage can be narrowed per-file. |
| `./internal/discovery/` | `discovery-team` | Keep | 2026-06-01 | Filesystem watching is OS-boundary behavior. | Keep unless watcher tests split cleanly into pure logic and OS integration files. |
| `./internal/notify/` | `ux-team` | Keep | 2026-06-01 | Desktop notification side effects are external-system boundary. | Keep unless notifier adapters are split more narrowly. |
| `./internal/sync/` | `sync-team` | Narrow over time | 2026-04-15 | Sync logic still touches external-service style boundaries, but the env/global-state debt is now reduced. | Narrow once network or external-state seams are isolated to smaller file groups. |
| `./internal/logs/` | `observability-team` | Narrow over time | 2026-04-15 | Log scanners and capture fixtures still use synthetic sources. | Narrow once scanners are consistently exercised against real on-disk fixtures only. |
| `./internal/wrap/` | `runtime-team` | Keep | 2026-06-01 | External command wrapping is a real subprocess boundary. | Keep unless the package is decomposed into pure logic plus boundary adapter files. |
| `./internal/warnings/` | `warnings-team` | Keep for now | 2026-05-15 | Warning collector verification still relies on fake collectors. | Narrow when collector behavior is exercised through concrete sink fixtures. |
| `./internal/bundle/` | `bundle-team` | Keep for now | 2026-05-15 | Bundle operations remain filesystem boundary heavy. | Narrow when archive operations are covered through real temp-dir fixtures end-to-end. |
| `./internal/passthrough/` | `passthrough-team` | Keep | 2026-06-01 | Passthrough mode wraps external execution. | Keep unless adapter logic is split from pure routing logic. |
| `./internal/coordinator/` | `coordination-team` | Narrow over time | 2026-04-15 | Cross-profile orchestration still uses mock external dependencies. | Narrow as real profile/vault/runtime fixtures replace coordinator doubles. |
| `./internal/authpool/monitor_test.go` | `authpool-team` | Narrow retained | 2026-04-15 | File-scoped scripted refresher seam only. | Remove by replacing `ScriptedRefresher` with direct state-fixture monitor coverage before 2026-04-15. |
| `./internal/prediction/` | `ml-team` | Keep | 2026-06-01 | Synthetic training data is intrinsic to this package's test surface. | Keep unless a better canonical fixture corpus replaces synthetic samples. |

## Immediate conclusions

1. The package-wide `cmd/caam/cmd`, `internal/authfile`, `internal/authpool`, `internal/identity`, and `internal/profile` waivers are already retired and should stay retired.
2. The next broad rules to challenge are `./internal/exec/`, `./internal/sync/`, `./internal/logs/`, and `./internal/coordinator/`, because those are closest to the core reliability claim and already have active retirement work.
3. File-scoped `cmd/caam/cmd/*` exceptions are acceptable as temporary truth-preserving debt, but they should not expand back into package-wide waivers.
4. `internal/testutil/safety_check_test.go` still contains `os.Setenv("HOME"...` string literals, but those are checker fixtures, not live env mutation. They should not be counted as runtime leakage debt.

## Next actions

1. Use this ledger as the source artifact for `bd-3fy.1.2`.
2. Convert the "Narrow retained" entries into explicit expiry and retirement metadata in the allowlist policy.
3. Continue the direct-fixture retirement lanes for `internal/exec`, `internal/sync`, `internal/logs`, and `internal/coordinator`.
