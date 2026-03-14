# Agent Install and Codex Setup

This fork is designed for agent-driven use. The binary is still named `caam`, but the primary workflow is Codex/GPT Pro account switching with reliable handoff and resume behavior.

## Install a release build

Use a published release asset when possible.

```bash
# macOS Apple Silicon
curl -L -o caam.tar.gz https://github.com/uuuser-name/caam-account-switching/releases/download/v0.1.6/caam_0.1.6_darwin_arm64.tar.gz
tar -xzf caam.tar.gz
sudo mv caam /usr/local/bin/caam

# Linux x86_64
curl -L -o caam.tar.gz https://github.com/uuuser-name/caam-account-switching/releases/download/v0.1.6/caam_0.1.6_linux_amd64.tar.gz
tar -xzf caam.tar.gz
sudo mv caam /usr/local/bin/caam
```

Verify the binary:

```bash
caam version
caam ls --json
```

`caam ls --json` should return machine-readable output even on a fresh machine.

## Codex configuration: required

If you use Codex with this fork, keep the lower-model / cheaper-model rate-limit nudge disabled.

Reason:

- that interactive prompt interrupts wrapper-driven handoff
- the correct recovery path is account switching, not silent model downgrades

Required Codex config:

```toml
cli_auth_credentials_store = "file"

[features]
multi_agent = true

[notice]
hide_rate_limit_model_nudge = true
```

File:

```text
~/.codex/config.toml
```

The easiest way to enforce this is:

```bash
caam doctor --fix
```

## First-run sequence

1. Install `caam`.
2. Run `caam version`.
3. Run `caam doctor --fix`.
4. Confirm `~/.codex/config.toml` contains the settings above.
5. Add or import your profiles.
6. Check state with `caam status --json`.

## Expectations for agents

Agents should:

- prefer JSON output where available
- avoid re-enabling the Codex model-downgrade prompt
- use `caam doctor --fix` if managed Codex settings drift
- file a GitHub issue with repro steps if switching, install, or resume continuity fails

## Troubleshooting

If switching fails after a Codex update:

```bash
caam doctor --fix
caam status --json
```

If the lower-model prompt appears again, your Codex config likely drifted or was overwritten.
