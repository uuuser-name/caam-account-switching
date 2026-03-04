# Test Realism PR Checklist

Additions to the standard PR template for enforcing test realism policy.

## Standard PR Template Additions

Include these items in every PR that modifies test files:

### Test Realism Checklist

- [ ] **Scope Classification**: New/modified tests use appropriate fixture types:
  - Core-scope packages (see `docs/testing/test_realism_taxonomy.md`) use real implementations
  - Boundary-scope packages may use mocks/fakes with documented justification

- [ ] **No Undocumented Doubles**: No new mock/fake/stub usage in core-scope tests without:
  - Exception request filed (if needed)
  - Allowlist entry in `docs/testing/test_realism_allowlist.json` (if exception approved)

- [ ] **Audit Compliance**: After changes, run:
  ```bash
  ./scripts/test_audit.sh
  ```
  Verify no new violations introduced in `artifacts/test-audit/mock_fake_stub_by_file.json`

- [ ] **Migration Plan**: If modifying a file with existing violations, include a brief migration plan or justify deferral

### For Files With Existing Violations

When modifying test files that already have mock/fake/stub violations:

1. **Check current status**:
   ```bash
   cat artifacts/test-audit/mock_fake_stub_by_file.json | jq '.[] | select(.file | contains("YOUR_FILE"))'
   ```

2. **Include in PR description**:
   - Current violation count in file
   - Whether PR adds/removes/neutral violations
   - If adding violations, document why (exception needed or fixture unavailable)

### PR Description Template Addition

```markdown
## Test Realism Impact

- [ ] No test file modifications in this PR
- [ ] Test files modified - checklist completed above

**Violation Impact**: 
- Before: N violations (link to audit artifact)
- After: N violations
- Net change: +/-N

**New Violations (if any)**:
| File | Line | Type | Justification |
|------|------|------|---------------|
| path/to/test.go | 123 | mock | Reason for exception |

**Migration Notes**:
- Brief notes on any fixtures created or migrations started
```

### Reviewer Checklist Addition

Reviewers should verify:

1. **New tests in core packages** use real implementations, not mocks
2. **Exception requests** are documented and tracked
3. **Audit compliance** - no new violations without exception

### CI Integration

The test audit runs in CI and will:
- Fail the build if new violations are introduced without allowlist entries
- Report violation count delta from baseline

To fix CI failures:
1. Migrate tests to realistic fixtures per `docs/testing/migration_playbook.md`
2. Or request exception and update allowlist

## Quick Reference

| Package Scope | Mock/Fake/Stub | Status |
|--------------|----------------|--------|
| Core | New usage | Requires exception |
| Core | Existing usage | Plan migration |
| Boundary | Any usage | Documented in test |

See also:
- `docs/testing/test_realism_taxonomy.md` - Scope definitions
- `docs/testing/migration_playbook.md` - Conversion guide
- `docs/testing/exception_approval_process.md` - Exception workflow
- `artifacts/test-audit/test_audit.md` - Current audit results