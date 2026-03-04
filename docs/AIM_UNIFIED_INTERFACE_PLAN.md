# AIM Plan: Unified Multi-Provider Account Interface

Owner bead: `bd-1ztl`
Date: 2026-02-28
Codebase: `coding_agent_account_manager@v0.1.10`

## 1) Project Bid (Build Proposal)

### Objective
Deliver a production-ready AIM layer that gives agents one interface to inspect usage headroom and switch profiles across Codex, Claude, and Gemini with hot reload and background health checks.

### Bid Scope
- Build on top of existing CAAM provider/profile switching logic.
- Add unified usage schema and adapters.
- Add local API service and machine-first CLI commands.
- Add background monitor + telemetry endpoint.
- Add integration and E2E tests.

### Estimated Effort (engineering days)
1. Stage 0 research + contracts: 1 day
2. Stage 1 schema + adapters (status/usage): 2 days
3. Stage 2 switch orchestration + verification: 2 days
4. Stage 3 background service + hot reload: 2 days
5. Stage 4 E2E + APR reliability harness: 2 days

Total: 9 engineering days for MVP; 12-14 days with hardening + docs + migration tooling.

### Non-goals (MVP)
- Cloud-hosted control plane
- Auto-buy/replenish subscriptions
- Browser automation for OAuth login completion

## 2) Problem Statement

Agent workflows break when one account hits provider limits. Existing tools already solve profile switching per provider, but there is no single operational interface for:
- standardized usage view,
- ranked account selection,
- verified switch transactions,
- zero-restart config updates,
- background self-healing telemetry.

## 3) 10x Strategy (Different, Not Incremental)

Instead of adding one more switch command, AIM becomes an account control bus:
- One local API for all providers and all agents.
- One state model for usage + health + cooldowns.
- One switch transaction primitive with pre/post proofs.
- One daemon loop that keeps the system ready without user interruption.

## 4) Architecture

### Components
1. `internal/aim/schema`
- Canonical JSON schema for provider/profile/usage/switch events.

2. `internal/aim/adapters`
- `caam_adapter`: profile/status/switch calls.
- `usage_adapter`: weekly/rolling usage ingest (CAAM local telemetry + optional CAUT bridge).

3. `internal/aim/orchestrator`
- Decides and executes switch transactions.
- Produces signed result envelope with verification evidence.

4. `internal/aim/service`
- Local API server (`127.0.0.1`), read/write endpoints.
- Hot-reload on config file change.

5. `internal/aim/healthloop`
- Periodic checks, bounded retry, cooldown, incident log.

### Data model (minimum)
- `ProviderStatus`: provider, active_profile, last_switch_at, health.
- `ProfileUsage`: provider, profile, window_type (`weekly|rolling`), used, limit, reset_at.
- `SwitchRequest`: provider, target_profile, reason, dry_run.
- `SwitchResult`: success, previous_profile, new_profile, verification, warnings.

## 5) API/CLI Contract

### API endpoints
- `GET /v1/status`
- `GET /v1/usage`
- `POST /v1/switch`
- `POST /v1/reload`
- `GET /v1/health`
- `GET /v1/events?limit=N`

### CLI commands
- `caam aim status --json`
- `caam aim usage --json`
- `caam aim switch <provider> <profile> [--dry-run] --json`
- `caam aim reload --json`
- `caam aim health --json`

## 6) Stage Gates

### Stage 0: Contracts and fixtures
Exit gate:
- JSON schema checked in.
- Golden fixtures for 3 providers.
- Contract tests pass.

### Stage 1: Read path
Exit gate:
- `aim status` and `aim usage` return unified schema from adapters.
- Unknown/missing usage represented explicitly (`source=unavailable`, `confidence=low`).

### Stage 2: Write path (switch)
Exit gate:
- Transactional switch with pre/post verification works on at least one real provider and mocked all providers.
- Failures leave previous state intact and logged.

### Stage 3: Background daemon + hot reload
Exit gate:
- Service runs in background with configurable interval.
- Config changes applied without restart.
- Cooldown + max-attempt guard proven in tests.

### Stage 4: Reliability and APR integration
Exit gate:
- E2E scenario: limit reached -> auto-select alternate profile -> verify resumed operation.
- APR workflow (`aim-unified-interface`) runs 3 rounds and stores outputs under `.apr/rounds/aim-unified-interface/`.

## 7) Telemetry and Logs

Required artifacts:
- `~/.caam/aim/events.ndjson`
- `~/.caam/aim/state.json`
- `~/.caam/aim/health.json`
- `~/.caam/aim/incidents/YYYY-MM-DD.md`

Each event includes:
- timestamp UTC
- operation id
- provider/profile
- outcome
- latency ms
- verification evidence summary

## 8) Test Plan

1. Unit tests
- Schema serialization and validation.
- Adapter normalization edge cases.

2. Integration tests
- Switch success and rollback.
- Usage aggregation with partial provider outages.

3. E2E tests
- Simulated limit hit and automatic failover.
- Hot reload while daemon active.

4. Static checks
- `go test ./...`
- `ubs <changed-files>`

## 9) Risks and Mitigations

1. Provider usage data inconsistency
- Mitigation: explicit confidence/source metadata and fallback policies.

2. Mid-session auth mutation race
- Mitigation: provider lock + transactional file swap + post-switch probe.

3. Daemon runaway retries
- Mitigation: capped retries, exponential backoff, cooldown windows.

## 10) Immediate Next Steps

1. Implement `bd-1ztl.1`: schema and fixtures.
2. Scaffold `internal/aim/*` packages and CLI wiring.
3. Add `aim` APR workflow skeleton and run first round.
