# Documentation Index

This repository uses the same agent-facing documentation style used across the flywheel tooling repos:

- Front-load install and quick usage examples.
- Include an explicit **Agent Quickstart** with machine-output expectations.
- State output contract clearly: `stdout=data`, `stderr=diagnostics`, `exit 0=success`.
- Explain the problem first, then the solution, then implementation/reference details.
- Keep command examples copy/paste-ready.

## Quick Links

- Smart profile management: [`SMART_PROFILE_MANAGEMENT.md`](SMART_PROFILE_MANAGEMENT.md)
- Distributed auth recovery: [`DISTRIBUTED_AUTH_RECOVERY.md`](DISTRIBUTED_AUTH_RECOVERY.md)
- Unified interface plan: [`AIM_UNIFIED_INTERFACE_PLAN.md`](AIM_UNIFIED_INTERFACE_PLAN.md)
- Feature plan: [`FEATURE_PLAN_2025Q1.md`](FEATURE_PLAN_2025Q1.md)
- Claude auth inventory: [`CLAUDE_AUTH_INVENTORY.md`](CLAUDE_AUTH_INVENTORY.md), [`CLAUDE_AUTH_INVENTORY.json`](CLAUDE_AUTH_INVENTORY.json)
- Clean environment support matrix: [`testing/clean_environment_support_matrix.md`](testing/clean_environment_support_matrix.md)
- Bounded live validation protocols: [`testing/bounded_live_validation_protocols.md`](testing/bounded_live_validation_protocols.md)
- Agent install and Codex setup: [`AGENT_INSTALL.md`](AGENT_INSTALL.md)
- Testing docs: [`testing/`](testing/)

## Authoring Template

Use this section order for new top-level docs:

1. Title and one-line value proposition.
2. Install / setup or entry command.
3. `## 🤖 Agent Quickstart` with JSON/robot examples.
4. Output conventions (`stdout`, `stderr`, exit codes).
5. Problem statement.
6. Solution summary.
7. Deep technical details (architecture, workflows, command reference).
8. Troubleshooting/FAQ.

## Agent Output Contract

When adding commands intended for automated agents:

- Provide JSON mode examples.
- Avoid interactive-only instructions unless clearly marked.
- Document expected failure behavior and non-zero exit codes.
