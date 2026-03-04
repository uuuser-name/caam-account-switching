# Test Realism Migration Playbook

This playbook provides step-by-step guidance for converting mock-heavy tests to realistic fixtures, aligned with the test realism contract defined in `test_realism_taxonomy.md`.

## Background

The test audit identifies mock/fake/stub violations in core-scope test files (see current count in `artifacts/test-audit/test_audit.json`). This playbook enables systematic migration from test doubles to realistic fixtures, improving test reliability and catching integration issues earlier.

## Prerequisites

- Read `docs/testing/test_realism_taxonomy.md` for scope definitions
- Review `artifacts/test-audit/mock_fake_stub_by_file.json` for violation inventory
- Check `docs/testing/test_realism_allowlist.json` for boundary exceptions

## Migration Steps

### Step 1: Identify and Classify Violations

```bash
# List all violations for your package
cat artifacts/test-audit/mock_fake_stub_by_file.json | jq '.[] | select(.file | contains("internal/exec"))'

# Check severity and scope
cat artifacts/test-audit/mock_fake_stub_by_file.json | jq '.[] | select(.severity == "violation")'
```

For each violation, determine:
1. **Core vs Boundary**: Is the package classified as core in the taxonomy?
2. **Double Type**: mock, fake, or stub?
3. **Usage Pattern**: What behavior is being simulated?

### Step 2: Analyze the Test Double's Purpose

Document what the double provides:

```go
// Example analysis for a mock:
// Mock purpose: Simulates ExecRunner interface returning success/error
// Real behavior needed: Actual process execution with controlled inputs
// Fixture required: Real binary that produces predictable outputs
```

Questions to answer:
- What inputs does the double accept?
- What outputs does it produce?
- What state transitions does it simulate?
- What error conditions does it handle?

### Step 3: Design the Realistic Fixture

Choose an appropriate fixture strategy:

| Double Type | Recommended Fixture |
|-------------|---------------------|
| Mock (behavior verification) | Real implementation with controlled environment |
| Fake (simplified implementation) | Real implementation with test-specific configuration |
| Stub (canned responses) | Real implementation with deterministic test data |

Fixture patterns for this codebase:

1. **Process execution** (`internal/exec`):
   - Use real binaries with controlled arguments
   - Create helper scripts that produce deterministic output
   - Use temporary directories for file-based tests

2. **OAuth/Authentication** (`internal/provider/*`):
   - Use recorded HTTP interactions (VCR-style) if boundary-allowed
   - Otherwise, use real token validation with test credentials

3. **Profile management** (`internal/profile`):
   - Use real profile files in temp directories
   - Exercise actual file I/O paths

4. **Command execution** (`cmd/caam/cmd`):
   - Use real CLI invocation with captured output
   - Test against real config files in isolated directories

### Step 4: Create the Fixture

```go
// Example: Converting a mock ExecRunner to real execution

// BEFORE (mock):
type MockRunner struct {
    mock.Mock
}

func (m *MockRunner) Run(ctx context.Context, name string, args ...string) error {
    return m.Called(ctx, name, args).Error(0)
}

// Test using mock:
mockRunner := new(MockRunner)
mockRunner.On("Run", ctx, "git", []string{"status"}).Return(nil)

// AFTER (realistic fixture):
func newTestRunner(t *testing.T, workDir string) *exec.Runner {
    t.Helper()
    return &exec.Runner{
        Dir:    workDir,
        Env:    []string{"PATH=/usr/bin:/bin"},
        Stdout: &bytes.Buffer{},
        Stderr: &bytes.Buffer{},
    }
}

// Test using real execution:
runner := newTestRunner(t, tmpDir)
err := runner.Run(ctx, "git", "init")
require.NoError(t, err)
```

### Step 5: Update Test Assertions

Replace mock verification with behavior verification:

```go
// BEFORE:
mockRunner.AssertExpectations(t)

// AFTER:
// Verify actual behavior
assert.FileExists(t, filepath.Join(tmpDir, ".git", "config"))
assert.Contains(t, runner.Stdout.String(), "Initialized empty Git repository")
```

### Step 6: Handle Edge Cases

For tests that rely on specific error conditions:

```go
// Create deterministic error scenarios with real behavior
func createFailingFixture(t *testing.T) string {
    t.Helper()
    dir := t.TempDir()
    // Create a file where a directory is expected
    os.WriteFile(filepath.Join(dir, "blocking-file"), []byte{}, 0644)
    return dir
}
```

### Step 7: Validate Migration

Run the test audit to verify the violation is resolved:

```bash
# Run test audit
./scripts/test_audit.sh

# Verify violation count decreased
cat artifacts/test-audit/mock_fake_stub_by_file.json | jq 'length'

# Check your specific file no longer has violations
cat artifacts/test-audit/mock_fake_stub_by_file.json | jq '.[] | select(.file | contains("your_file_test.go"))'
```

## Example Migrations

### Example 1: internal/exec/exec_test.go

**Before** (43 violations):
```go
func TestRunner_Success(t *testing.T) {
    mockRunner := new(MockRunner)
    mockRunner.On("Run", mock.Anything, "echo", []string{"hello"}).Return(nil)
    
    err := mockRunner.Run(context.Background(), "echo", "hello")
    assert.NoError(t, err)
    mockRunner.AssertExpectations(t)
}
```

**After**:
```go
func TestRunner_Success(t *testing.T) {
    tmpDir := t.TempDir()
    runner := exec.NewRunner(tmpDir)
    
    var stdout, stderr bytes.Buffer
    runner.Stdout = &stdout
    runner.Stderr = &stderr
    
    err := runner.Run(context.Background(), "echo", "hello")
    assert.NoError(t, err)
    assert.Equal(t, "hello\n", stdout.String())
}
```

### Example 2: internal/warnings/warnings_test.go

**Before** (7 fake violations):
```go
func TestWarningChecker_FakeResponse(t *testing.T) {
    fakeChecker := &FakeWarningChecker{
        Response: []Warning{{Message: "test warning"}},
    }
    warnings := fakeChecker.Check(context.Background())
    assert.Len(t, warnings, 1)
}
```

**After**:
```go
func TestWarningChecker_RealResponse(t *testing.T) {
    if testing.Short() {
        t.Skip("Integration test requires real environment")
    }
    
    // Use real warning checker with controlled environment
    checker := warnings.NewChecker()
    
    // Create test scenario that triggers warning
    tmpDir := t.TempDir()
    os.WriteFile(filepath.Join(tmpDir, "test-file"), []byte{}, 0644)
    
    warnings := checker.Check(context.Background(), tmpDir)
    // Verify real behavior
}
```

## Exception Process

If a test cannot be migrated to realistic fixtures:

1. Document the technical blocker
2. Submit exception request via `br comment bd-1r67.1.2.3 --text "Exception request: <reason>"`
3. Update `test_realism_allowlist.json` with the exception after approval

See `docs/testing/exception_approval_process.md` for full process.

## Common Pitfalls

1. **Over-mocking**: When multiple mocks are used in a single test, consider whether the test is testing integration behavior
2. **Leaky abstractions**: If the mock interface doesn't match real behavior, the test provides false confidence
3. **Fixture complexity**: If the fixture is more complex than the mock, document why and consider simplification

## Metrics

Track migration progress:

```bash
# Count violations over time
cat artifacts/test-audit/mock_fake_stub_by_file.json | jq '[.[] | select(.severity == "violation")] | length'

# Violations by package
cat artifacts/test-audit/mock_fake_stub_by_package.json | jq '.[] | select(.count > 0)'
```

Target: Reduce core-scope violations from the current baseline to 0, with documented exceptions in the allowlist.
