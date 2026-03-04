# Core vs Boundary Test Realism Taxonomy

This taxonomy defines where test doubles are prohibited vs conditionally allowed.

## Policy Snapshot (2026-03-04)

- Core-path forbidden terms in tests: `mock`, `fake`, `stub`
- Detector command: `./scripts/lint_test_realism.sh --strict`
- Current strict result: 28 violations (all in `internal/agent` and `internal/setup`)
- Source artifact: `artifacts/test-audit/mock_fake_stub_by_file.json`

## Classification Rules

- Core logic:
1. In-process decision logic, orchestration/state machines, and command behavior that users directly depend on.
2. Must prefer real-behavior fixtures and integration-style unit tests.
3. Mock/fake/stub usage is treated as a policy violation unless explicitly escalated.

- Boundary adapters:
1. External provider/network calls, OS-dependent interfaces, and deploy/system adapters.
2. Doubles are allowed when they isolate unavailable or unsafe external side effects.
3. Every boundary double must include inline justification and ownership metadata.

## Edge-Case Guidance

- If a package wraps external calls but also contains branching business logic, split logic into a core-tested path and keep doubles only at the outer adapter seam.
- Command parsing/selection flows are core even when they eventually call boundaries.
- Time/race behavior should use deterministic test harnesses before falling back to doubles.

## Initial Package Mapping (from baseline coverage audit)

| Package | Scope | Rationale |
|---|---|---|
| `cmd/caam/cmd` | core | User-facing command logic and switching behavior. |
| `internal/exec` | core | Runtime process control + retry semantics. |
| `internal/coordinator` | core | Cross-profile orchestration and failure handling. |
| `internal/agent` | core | Agent/session state transitions. |
| `internal/setup` | core | Setup decisions and local environment policy. |
| `internal/deploy` | boundary | Deploy/system interaction surface. |
| `internal/tailscale` | boundary | External network control plane integration. |
| `internal/provider` | boundary | Provider API/auth adapter behavior. |

## Core-Path Package Index (No-Mock Required)

These package prefixes are treated as core paths for no-mock enforcement:

- `./cmd/caam/cmd/`
- `./internal/agent/`
- `./internal/coordinator/`
- `./internal/rotation/`
- `./internal/refresh/`
- `./internal/ratelimit/`
- `./internal/authfile/`
- `./internal/authpool/`
- `./internal/identity/`
- `./internal/profile/`

## Prohibited Constructs (Core Scope)

Core-scope tests must not contain unapproved occurrences of these tokens:

- `mock`
- `fake`
- `stub`

Boundary rules and approved exceptions are defined in:

- `docs/testing/test_realism_allowlist.json`

## Audit Coupling

- `scripts/test_audit.sh` reads `docs/testing/test_realism_allowlist.json` and emits:
1. `artifacts/test-audit/mock_fake_stub_by_file.json`
2. `artifacts/test-audit/mock_fake_stub_by_package.json`
- Core-scope matches are reported as `severity=violation`; boundary matches as `severity=allowed`.
