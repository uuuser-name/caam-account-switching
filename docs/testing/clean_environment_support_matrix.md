# Clean Environment Support Matrix and Bootstrap Contract

This document defines the environments that count for closure-grade validation and what each lane must provide before CAAM can claim publish-ready quality.

## Supported Validation Environments

| Lane | Environment | Purpose | Required Inputs | Must Stay Offline-Safe |
| --- | --- | --- | --- | --- |
| Deterministic Linux | Clean Ubuntu runner, x86_64, Go 1.24.x | Primary CI-quality truth gate | Source checkout, `bash`, `jq`, `rg`, Go toolchain | Yes |
| Deterministic macOS | Clean macOS 14+ workstation or runner, arm64 or x86_64, Go 1.24.x | Platform-specific filesystem and shell parity | Source checkout, `bash`, `jq`, `rg`, Go toolchain | Yes |
| Installed-binary parity | Fresh shell using extracted release artifact or package-manager install | Prove release artifacts behave like source builds for bootstrap commands | Release asset or package install, writable temp directory | Yes |
| Bounded live switching | Explicitly opted-in workstation with real provider accounts | Prove real exhausted-account switching and real conversation resume | Provider CLIs, real user-owned accounts, bounded scenario plan | No |

## Bootstrap Contract

Every supported lane must satisfy these rules before its results count:

1. Use an isolated writable home for test artifacts and temporary state.
2. Never depend on the maintainer's real auth files, shell history, or workstation-specific dotfiles.
3. Record exact commands and emitted artifacts for every gate that claims success.
4. Treat deterministic and bounded-live evidence as separate truth labels.
5. Fail closed when required tooling is missing instead of silently downgrading the lane.

## Deterministic Lane Requirements

These commands are the minimum contract for a closure-grade deterministic run from source:

```bash
./scripts/test_audit.sh
./scripts/ci_e2e_quality_gate.sh
```

Deterministic lanes may not require:
- real provider credentials
- browser login flows
- manually edited workstation config
- hidden pre-seeded caches outside the repo contract

## Installed-Binary Parity Contract

An installed-binary check is valid only if it runs without the source tree on the execution path except for downloaded release assets or a package-manager install.

Minimum expected checks:

```bash
caam --version
caam list --json
caam doctor --json
```

The installed-binary lane must verify that:
- the binary starts cleanly
- machine-readable commands still emit valid JSON on stdout
- missing local provider state is reported as diagnostics, not as crashes

## Bounded Live Lane Contract

Live validation is opt-in and must be explicitly labeled as live evidence.

Required safeguards:
- use only user-owned provider accounts
- cap the scenario to the minimal number of switches needed to prove behavior
- capture canonical logs and supporting artifacts
- stop immediately on unexpected auth drift or ambiguous session identity

Live evidence is required for:
- real exhausted-account switching
- real resume-correct-conversation proof
- real auto-continue after switched resume

## Secrets and Artifact Policy

- Never commit tokens, auth files, or copied provider state.
- Redaction checks must pass before sharing artifacts outside the local machine.
- Failure packets may contain paths and structured logs, but not reusable credentials.

## Truth Labels

Use these labels in release notes, audit summaries, and issue closure comments:

- `deterministic_green`: deterministic source-based gates passed in a supported clean environment
- `installed_binary_green`: release artifact bootstrap checks passed
- `bounded_live_green`: bounded live proof passed with captured evidence
- `not_run`: the lane was intentionally skipped and must not be implied as green

## Current Publish Bar

For a GitHub release to be described as publish-ready:

1. Deterministic Linux must be green.
2. Deterministic macOS should be green when macOS-specific paths changed.
3. Installed-binary parity should be green for the released artifact path.
4. Any missing bounded-live proof must be labeled honestly as `not_run`, not implied.
