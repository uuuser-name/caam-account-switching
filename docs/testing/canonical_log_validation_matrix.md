# Canonical log validation matrix

Last updated: 2026-03-14

This matrix enumerates the current test workflows that must validate canonical logs through `ValidateCanonicalLogs()` directly or through the shared SmartRunner helper that wraps the same validation path.

## Direct validation call sites

| File | Test |
| --- | --- |
| `internal/testutil/extended_harness_test.go` | `TestExtendedHarness_ValidateCanonicalLogsReadsOnDiskArtifact` |
| `internal/testutil/extended_harness_test.go` | `TestExtendedHarness_ValidateCanonicalLogsRejectsDenyPatternInOutput` |
| `internal/testutil/extended_harness_test.go` | `TestExtendedHarness_ValidateCanonicalLogsAllowsDiagnosticTokenPhrase` |
| `internal/testutil/extended_harness_test.go` | `TestExtendedHarness_ValidateCanonicalLogsRejectsRunIntegrityDrift` |
| `internal/e2e/workflows/ratelimit_recovery_test.go` | `TestE2E_RateLimitRecoveryWorkflow` |
| `internal/e2e/workflows/rotation_exhaustion_test.go` | `TestE2E_FiveHourCreditExhaustionSwitch` |
| `internal/e2e/workflows/rotation_exhaustion_test.go` | `TestE2E_WeeklyCreditExhaustionSwitch` |
| `internal/e2e/workflows/rotation_exhaustion_test.go` | `TestE2E_ActiveSessionHandoffContinuityUnderExhaustion` |
| `internal/e2e/workflows/rotation_exhaustion_test.go` | `TestE2E_CooldownClearNonexistentProfile` |
| `internal/e2e/workflows/backup_activate_test.go` | `TestE2E_CompleteBackupActivateSwitchWorkflow` |
| `internal/e2e/workflows/backup_activate_test.go` | `TestE2E_RapidProfileSwitching` |
| `internal/e2e/workflows/backup_activate_test.go` | `TestE2E_CrossProviderSwitching` |
| `internal/e2e/workflows/rotation_test.go` | `TestRotationWorkflow` |
| `internal/e2e/workflows/rotation_cooldown_test.go` | `TestE2E_CooldownEnforcesDuringAutoRotation` |
| `internal/e2e/workflows/rotation_cooldown_test.go` | `TestE2E_RotationAlgorithms` |
| `internal/e2e/workflows/rotation_cooldown_test.go` | `TestE2E_CooldownBypass` |
| `internal/e2e/workflows/rotation_cooldown_test.go` | `TestE2E_CooldownListAndClearAll` |
| `internal/e2e/workflows/rotation_cooldown_test.go` | `TestE2E_RotationWithRecencyPenalty` |
| `cmd/caam/cmd/e2e_cli_test.go` | `TestE2E_RestoreCommand` |
| `cmd/caam/cmd/e2e_cli_test.go` | `TestE2E_MultiProviderWorkflow` |

## Helper-wrapped validation call sites

These tests validate canonical logs through `validateCanonicalLogsWithFailureCheck(...)`, which first calls `h.ValidateCanonicalLogs()` and then verifies failure on an intentionally corrupted on-disk artifact.

| File | Test |
| --- | --- |
| `internal/exec/smart_runner_e2e_test.go` | `TestSmartRunner_E2E` |
| `internal/exec/smart_runner_e2e_test.go` | `TestSmartRunner_E2E_RateLimitExitPreservesSwitchedProfile` |
| `internal/exec/smart_runner_e2e_test.go` | `TestSmartRunner_E2E_CodexRefreshTokenReusedTriggersSwitch` |
| `internal/exec/smart_runner_e2e_test.go` | `TestSmartRunner_E2E_CodexRateLimitSwitchSkipsInteractiveLogin` |
| `internal/exec/smart_runner_e2e_test.go` | `TestSmartRunner_E2E_CodexRateLimitSwitchRepairsManagedConfig` |
| `internal/exec/smart_runner_e2e_test.go` | `TestSmartRunner_E2E_CodexFallsBackToSystemProfileWhenOnlyDistinctAlternative` |
| `internal/exec/smart_runner_e2e_test.go` | `TestSmartRunner_E2E_CodexRateLimitExitAutoResumesOnSwitchedProfile` |
| `internal/exec/smart_runner_e2e_test.go` | `TestSmartRunner_E2E_CodexRateLimitExitAutoResumesAndContinuesOnSwitchedProfile` |
| `internal/exec/smart_runner_e2e_test.go` | `TestSmartRunner_E2E_CodexRateLimitExitIgnoresStaleRedispatchBeforeSeamlessResume` |
| `internal/exec/smart_runner_e2e_test.go` | `TestSmartRunner_E2E_CodexRateLimitExitZeroStillSeamlesslyResumes` |
| `internal/exec/smart_runner_e2e_test.go` | `TestSmartRunner_E2E_MultiProfileChainUntilHealthy` |
| `internal/exec/smart_runner_e2e_test.go` | `TestSmartRunner_E2E_HandoffFailureStillErrorsWhenWrappedCommandExitsZero` |

## Current interpretation

1. Switching, handoff, resume, cooldown, and command E2E coverage now has an explicit validation list instead of an implicit grep-only contract.
2. SmartRunner E2E coverage relies on the shared helper and should stay in this matrix unless those tests are migrated to direct inline validation.
3. Bounded-live workflows are not yet represented because the bounded-live protocol lanes are still open.

## Next action

1. Use this matrix as the source artifact for `bd-3fy.4.1.1`.
2. Add any new switching, handoff, or bounded-live workflow to this matrix in the same change that introduces the test.
