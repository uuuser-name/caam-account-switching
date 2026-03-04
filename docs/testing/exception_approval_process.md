# Exception Approval Process

This document defines the workflow for requesting, reviewing, and approving exceptions to the test realism policy.

## Background

The test realism contract requires core-scope packages to use real implementations rather than test doubles (mocks, fakes, stubs). However, legitimate exceptions exist where realistic fixtures are impractical or impossible.

## When to Request an Exception

Exceptions are appropriate when:

1. **External dependencies unavailable**: Third-party services that cannot be reliably mocked with recordings
2. **Flaky or non-deterministic behavior**: Tests that require timing or environment conditions that cannot be made deterministic
3. **Security constraints**: Tests involving credentials, secrets, or sensitive data that cannot be safely exercised
4. **Platform limitations**: Features that require specific OS/hardware not available in CI
5. **Legacy migration in progress**: Existing violations with documented migration timeline

Exceptions are **NOT appropriate** for:

- Convenience or speed (use `testing.Short()` to skip in unit tests)
- Unwillingness to write realistic fixtures
- Workarounds for bugs (fix the underlying issue instead)

## Exception Request Workflow

### Step 1: Document the Request

Create a comment on the relevant task/bead with the following format:

```
br comment <issue-id> --text "Exception request: <file-path>

**Type**: mock|fake|stub
**Line(s)**: <line numbers>
**Reason**: <detailed technical justification>
**Attempted alternatives**: <what was tried before requesting exception>
**Proposed duration**: permanent|temporary with migration date
**Migration plan** (if temporary): <steps to eventually resolve>
"
```

### Step 2: Technical Review

The exception request is reviewed by:

1. **Package owner**: Listed in `docs/testing/test_realism_allowlist.json`
2. **Realism advocate**: Maintainer responsible for test quality

Review criteria:
- Technical validity of the justification
- Adequacy of attempted alternatives
- Appropriateness of proposed duration
- Completeness of migration plan (if temporary)

### Step 3: Approval Decision

**Approved**: Update the allowlist and proceed
**Rejected**: Provide guidance on alternative approaches
**Conditional**: Approval with specific requirements

Approval response format:

```
br comment <issue-id> --text "Exception approved for <file-path>

**Approved by**: <reviewer>
**Conditions**: <any conditions or requirements>
**Allowlist entry**: <JSON snippet to add>
**Review date**: <date for follow-up if temporary>
"
```

### Step 4: Update Allowlist

After approval, add an entry to `docs/testing/test_realism_allowlist.json`:

```json
{
  "prefix": "./internal/provider/github_test.go",
  "scope": "boundary",
  "owner": "provider-team",
  "exception": {
    "reason": "GitHub API rate limits in CI",
    "approved_by": "realism-advocate",
    "approved_date": "2026-03-02",
    "type": "mock",
    "review_date": "2026-06-02"
  }
}
```

### Step 5: Verify Compliance

Run the test audit to confirm the exception is recognized:

```bash
./scripts/test_audit.sh
./scripts/lint_test_realism.sh
```

The violation should now appear as "allowed" rather than a violation.

## Exception Categories

### Permanent Exceptions

For cases where realistic fixtures are fundamentally impossible:

- External APIs without sandbox environments
- Hardware-specific functionality
- Security-sensitive operations

Permanent exceptions require:
- Annual review
- Clear documentation of why no alternative exists
- Commitment to revisit if circumstances change

### Temporary Exceptions

For cases where migration is planned but not yet complete:

- Legacy test migration in progress
- Dependency refactoring underway
- CI infrastructure improvements pending

Temporary exceptions require:
- Specific migration timeline
- Assigned owner for the migration
- Follow-up review date

### Emergency Exceptions

For urgent situations blocking critical work:

- Must be followed by proper exception request within 5 business days
- Time-limited to 30 days
- Requires retrospective review

## Exception Review Cadence

| Exception Type | Review Frequency | Reviewer |
|---------------|------------------|----------|
| Permanent | Annual | Realism advocate + package owner |
| Temporary | Per milestone or quarterly | Package owner |
| Emergency | 30 days max | Realism advocate |

## Metrics and Reporting

Track exception health:

```bash
# Count exceptions
jq '[.rules[] | select(.exception)] | length' docs/testing/test_realism_allowlist.json

# Exceptions by type
jq '[.rules[].exception.type] | group_by(.) | map({type: .[0], count: length})' docs/testing/test_realism_allowlist.json

# Overdue reviews
jq '[.rules[] | select(.exception.review_date < "2026-03-02")]' docs/testing/test_realism_allowlist.json
```

## Example: Full Exception Request

```bash
# Step 1: Submit request
br comment bd-1r67.1.2.3 --text "Exception request: ./internal/provider/github_test.go

**Type**: mock
**Line(s)**: 45-67, 89-102
**Reason**: GitHub API has strict rate limits (5000/hour for authenticated, 60/hour for unauthenticated). CI runs would exhaust quota quickly, making real API calls impractical for automated testing. VCR-style recordings become stale and require manual refresh.

**Attempted alternatives**:
1. Used go-vcr for recording - recordings became stale after 2 weeks
2. Mocked HTTP transport - still a mock, not addressing root concern
3. Used GitHub's test mode - does not exist

**Proposed duration**: temporary with migration to GitHub App integration
**Migration plan**: 
- Q2 2026: Implement GitHub App authentication with higher rate limits
- Q3 2026: Create dedicated test organization with CI-controlled tokens
- Q4 2026: Migrate tests to use real API calls with test org
"

# Step 2: Wait for review (typically 1-2 business days)

# Step 3: After approval, update allowlist
# (add JSON entry as shown above)

# Step 4: Verify
./scripts/test_audit.sh
```

## Related Documents

- `docs/testing/test_realism_taxonomy.md` - Scope definitions
- `docs/testing/migration_playbook.md` - Conversion guide
- `docs/testing/pr_checklist.md` - PR requirements
- `artifacts/test-audit/test_audit.md` - Current audit results