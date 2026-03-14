# Security Policy

## Supported versions

Only the latest release and any security-fix branches are supported for vulnerability response.

## Reporting a vulnerability

If you suspect a security issue:

1. Do not file it in a public issue.
2. Contact the maintainer via GitHub Security Advisory:
   https://github.com/Dicklesworthstone/coding_agent_account_manager/security/advisories/new
3. Include:
   - Affected version or commit
   - Exact provider/tool context (`claude`, `codex`, or `gemini`)
   - Repro steps
   - Any logs or output showing credential/auth handling behavior

## What to include

Please provide:

- `caam --version` or the release tag used
- OS and architecture
- Whether profiles/auth files were local, isolated, or installed paths
- Steps to reproduce, including commands run
- Risk level estimate and blast radius

## Response expectations

- We aim to acknowledge reports within 72 hours.
- We will prioritize findings that could expose tokens, credentials, account state, or profile metadata.
- Confirmed issues receive a public advisory and patch notice as soon as possible.

## Sensitive data handling

Do not paste tokens, OAuth files, session cookies, or raw profile directories in reports.
If needed for triage, provide redacted snippets and exact file paths separately in private report channels.

