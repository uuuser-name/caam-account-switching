# Bounded Live Validation Protocols

This runbook defines the live-account procedures that must exist before CAAM can claim real exhausted-account switching and real resume-correct-conversation proof.

Use this document together with [`clean_environment_support_matrix.md`](clean_environment_support_matrix.md). These procedures define how to run the live lane safely; they do not imply that the live lane has already passed.

## Automation Entry Point

Use the repo-owned runner for Codex bounded-live proofs so the run emits transcript, canonical JSONL, and summary artifacts in one place:

```bash
./scripts/run_bounded_live_codex_validation.sh --start-profile leothehumanbeing
```

The script will:

- require a concrete exhausted start profile
- derive the expected fallback from `caam next codex --dry-run` if one is not supplied
- force-activate the exhausted profile
- run a bounded `caam run codex` prompt
- capture raw transcript plus canonical log events
- emit `bounded_live_validation_summary.json` and markdown in `artifacts/bounded-live/...`

## Shared Rules for All Live Runs

Every bounded-live run must:

1. Use only user-owned accounts with explicit consent to spend quota.
2. Limit the scenario to the smallest number of live switches needed to prove behavior.
3. Capture canonical logs and a compact evidence bundle.
4. Record exact profile names, expected fallback order, and the success condition before starting.
5. Stop immediately on ambiguous session identity, unexpected auth drift, or unbounded repeated switching.

Required evidence for any live run:

- wall-clock start time and end time
- active provider and starting profile
- fallback profile chosen by CAAM
- canonical log path
- final truth label: `bounded_live_green`, `failed`, or `not_run`

## Protocol A: Exhausted-Account Auto-Switch Validation

Purpose:
- Prove that CAAM detects a genuinely exhausted Codex account and switches to a healthy fallback account without manual re-login.

### Preconditions

- At least two Codex profiles exist in CAAM.
- One profile is known to be exhausted or intentionally placed in a bounded state that will trigger the real rate-limit path.
- One fallback profile is healthy and ready to run.
- The operator has confirmed the expected fallback target before the run.

### Procedure

1. Record the exhausted starting profile and the exact fallback profile expected to win.
2. Start a bounded Codex task through CAAM using the exhausted profile path.
3. Wait only for the first authentic rate-limit or exhausted-account signal.
4. Observe CAAM selecting the fallback profile.
5. Stop after the first successful resumed response on the fallback profile.

### Required Success Evidence

- the triggering exhausted-account or rate-limit event
- the selected fallback profile identifier
- proof that no interactive re-login was required
- proof that the post-switch command continued on the fallback profile

### Fail-Fast Conditions

Stop and label the run `failed` if:

- CAAM picks an unexpected profile without a documented reason
- the flow requires manual browser or device re-login
- multiple fallback hops occur beyond the declared bound
- logs are incomplete or fail canonical validation

## Protocol B: Resume-Correct-Conversation and Auto-Continue Validation

Purpose:
- Prove that after a live Codex account switch, CAAM resumes the correct conversation and continues without waiting for a manual `go on` or `proceed`.

### Preconditions

- The starting Codex session has a known live conversation id or equivalent resume handle.
- The trigger path is expected to force a switch while the conversation is still resumable.
- The continuation prompt CAAM is expected to inject is known ahead of time.

### Procedure

1. Record the initial conversation id or resume handle before the switch.
2. Trigger the bounded rate-limit handoff using the live account that will exhaust.
3. Capture the switched profile id and resumed session id immediately after handoff.
4. Wait only long enough to confirm that the continuation prompt was injected automatically.
5. Stop after the first successful assistant response on the resumed switched session.

### Required Success Evidence

- starting conversation id or resume handle
- resumed conversation id after switch
- proof that the ids match or map to the same resumed conversation
- proof that the continuation prompt was injected automatically
- proof that the first post-resume response arrived without manual user input

### Fail-Fast Conditions

Stop and label the run `failed` if:

- the resumed session id is missing or ambiguous
- CAAM resumes the wrong conversation
- the operator has to type a manual continuation prompt
- logs fail schema, redaction, or timeline validation

## Evidence Bundle Checklist

Every bounded-live packet should include:

- a short scenario summary
- the exact command used
- canonical logs
- captured session ids or resume handles
- the selected fallback profile
- a one-line outcome and truth label

## Reporting Rule

Until these procedures are actually run, release notes and README language must say the live lane is `not_run`, not imply it is green.
