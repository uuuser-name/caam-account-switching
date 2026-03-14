# Contributing to CAAM

Thanks for contributing.

CAAM is a Go CLI for switching authenticated sessions for AI coding agents.
This guide is intentionally practical: small, clear patches with reproducible checks.

## Ways to contribute

- File bugs with clear reproduction steps
- Add or fix tests around account switching, profile handling, or watcher behavior
- Improve docs for installation, provider setup, and workflows
- Reduce flakiness and improve logging in tests

## Before you start

- Fork the repository.
- Create a topic branch.
- Keep PRs focused and incremental.
- Do not commit credentials or token files.

## Local setup

```bash
git clone <your-fork>
cd caam-account-switching
go test ./...
```

If tests require local auth or provider binaries, run only offline-safe checks first and document what needs to be mocked or gated.

## Code style

- Use `gofmt` formatting.
- Favor explicit error handling and deterministic behavior.
- Keep tests readable and fast where possible.
- Place new provider-specific behavior behind clear interfaces where possible.

## Required checks before opening a PR

Run at minimum:

- `go test ./...`
- `go test -run TestName ./...` for any tests modified
- Relevant lint or static checks if present in repo tooling
- Any project scripts that gate CI for changed area (document if unable to run locally)

## Testing guidance

- Avoid embedding secrets in tests.
- Prefer fixtures and temp directories over real user paths.
- If adding command tests, assert command output and exit codes explicitly.

## Issue-first workflow

For behavior changes, open an issue first when:

- A behavior change affects UX or switching semantics
- You need to adjust protocol boundaries or error behavior
- You change authentication/profile management behavior

## PR checklist

- [ ] Scope is narrow and includes no unrelated files
- [ ] Commands run and outputs are in PR description
- [ ] Tests added/updated for changed behavior
- [ ] Docs updated if user-facing behavior changes
- [ ] No secrets, credentials, or token paths added to logs

## Communication style

Keep PRs practical and outcome-oriented:

- What changed
- Why this change was needed
- How to validate it
- What risks remain

