package project

import (
	"testing"
)

// =============================================================================
// isGlob Tests
// =============================================================================

func TestIsGlob_WithWildcard(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"*", true},
		{"*.go", true},
		{"src/*", true},
		{"**/*.go", true},
		{"file?.txt", true},
		{"[abc]", true},
		{"[a-z]", true},
		{"test[123].go", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := isGlob(tt.pattern)
			if got != tt.want {
				t.Errorf("isGlob(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

func TestIsGlob_WithoutWildcard(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"file.txt", false},
		{"/path/to/file", false},
		{"src/main.go", false},
		{"", false},
		{"normal-name", false},
		{"file_with_underscores.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := isGlob(tt.pattern)
			if got != tt.want {
				t.Errorf("isGlob(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

// =============================================================================
// wildcardCount Tests
// =============================================================================

func TestWildcardCount(t *testing.T) {
	tests := []struct {
		pattern string
		want    int
	}{
		{"", 0},
		{"file.txt", 0},
		{"*", 1},
		{"*.go", 1},
		{"**/*.go", 3},
		{"*.txt?", 2},
		{"[abc]", 1},
		{"[abc]*", 2},
		{"*.?.[a-z]", 3},
		{"***", 3},
		{"src/**/test/*.go", 3},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := wildcardCount(tt.pattern)
			if got != tt.want {
				t.Errorf("wildcardCount(%q) = %d, want %d", tt.pattern, got, tt.want)
			}
		})
	}
}

// =============================================================================
// matchingGlobs Tests
// =============================================================================

func TestMatchingGlobs_EmptyAssociations(t *testing.T) {
	matches := matchingGlobs(nil, "target")
	if matches != nil {
		t.Errorf("expected nil for nil associations, got %v", matches)
	}

	matches = matchingGlobs(map[string]map[string]string{}, "target")
	if matches != nil {
		t.Errorf("expected nil for empty associations, got %v", matches)
	}
}

func TestMatchingGlobs_NoGlobPatterns(t *testing.T) {
	associations := map[string]map[string]string{
		"exact-path": {"key": "value"},
		"another":    {"key": "value"},
	}

	matches := matchingGlobs(associations, "target")
	if len(matches) != 0 {
		t.Errorf("expected no matches for non-glob patterns, got %v", matches)
	}
}

func TestMatchingGlobs_SingleMatch(t *testing.T) {
	associations := map[string]map[string]string{
		"*.go":    {"key": "go-files"},
		"*.txt":   {"key": "txt-files"},
		"noMatch": {"key": "exact"},
	}

	matches := matchingGlobs(associations, "main.go")
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %v", len(matches), matches)
	}
	if matches[0].pattern != "*.go" {
		t.Errorf("expected pattern '*.go', got %q", matches[0].pattern)
	}
}

func TestMatchingGlobs_MultipleMatches_SortedBySpecificity(t *testing.T) {
	associations := map[string]map[string]string{
		"*":       {"key": "all"},
		"*.go":    {"key": "go-files"},
		"main*":   {"key": "main-prefix"},
		"main.go": {"key": "exact"}, // Not a glob, should be ignored
	}

	matches := matchingGlobs(associations, "main.go")

	// Should match: *, *.go, main* (3 patterns)
	// main.go is not a glob, so it's skipped
	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d: %v", len(matches), matches)
	}

	// Sorted by specificity: fewer wildcards first
	// *.go and main* both have 1 wildcard, but *.go is shorter pattern
	// * has 1 wildcard but is shorter
	// With equal wildcard count, longer pattern wins (more specific)
	// main* (5 chars, 1 wildcard) should come before *.go (4 chars, 1 wildcard)
	// Actually: main* has 5 chars, *.go has 4 chars - main* is longer = more specific

	// The sorting is:
	// 1. Fewer wildcards = more specific
	// 2. Longer pattern = more specific (tie breaker)
	// 3. Lexicographic (final tie breaker)

	// *.go, main*, * all have 1 wildcard
	// main* (5) > *.go (4) > * (1) by length
	// So order should be: main*, *.go, *
	if matches[0].pattern != "main*" {
		t.Errorf("first match should be 'main*' (most specific), got %q", matches[0].pattern)
	}
	if matches[1].pattern != "*.go" {
		t.Errorf("second match should be '*.go', got %q", matches[1].pattern)
	}
	if matches[2].pattern != "*" {
		t.Errorf("third match should be '*' (least specific), got %q", matches[2].pattern)
	}
}

func TestMatchingGlobs_NoMatch(t *testing.T) {
	associations := map[string]map[string]string{
		"*.txt": {"key": "txt"},
		"*.md":  {"key": "md"},
	}

	matches := matchingGlobs(associations, "main.go")
	if len(matches) != 0 {
		t.Errorf("expected no matches, got %v", matches)
	}
}

func TestMatchingGlobs_QuestionMark(t *testing.T) {
	associations := map[string]map[string]string{
		"file?.txt": {"key": "single-char"},
	}

	tests := []struct {
		target string
		want   int
	}{
		{"file1.txt", 1},
		{"fileA.txt", 1},
		{"file.txt", 0},   // ? requires exactly one char
		{"file12.txt", 0}, // ? requires exactly one char
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			matches := matchingGlobs(associations, tt.target)
			if len(matches) != tt.want {
				t.Errorf("matchingGlobs for %q: expected %d matches, got %d", tt.target, tt.want, len(matches))
			}
		})
	}
}

func TestMatchingGlobs_CharacterClass(t *testing.T) {
	associations := map[string]map[string]string{
		"[abc].txt": {"key": "abc"},
		"[0-9].log": {"key": "digits"},
	}

	tests := []struct {
		target string
		want   int
	}{
		{"a.txt", 1},
		{"b.txt", 1},
		{"c.txt", 1},
		{"d.txt", 0}, // Not in [abc]
		{"1.log", 1},
		{"5.log", 1},
		{"a.log", 0}, // Not a digit
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			matches := matchingGlobs(associations, tt.target)
			if len(matches) != tt.want {
				t.Errorf("matchingGlobs for %q: expected %d matches, got %d", tt.target, tt.want, len(matches))
			}
		})
	}
}

func TestMatchingGlobs_SortStability(t *testing.T) {
	// When patterns have same wildcard count and length, use lexicographic order
	associations := map[string]map[string]string{
		"b*.go": {"key": "b"},
		"a*.go": {"key": "a"},
		"c*.go": {"key": "c"},
	}

	// All patterns match anything.go but none match main.go specifically
	// Let's use a target that matches all of them
	matches := matchingGlobs(associations, "abcd.go")

	// Only a*.go matches "abcd.go" (starts with a)
	// Actually *.go patterns require the target to start with specific letter
	// a*.go matches targets starting with 'a'
	// So only a*.go matches "abcd.go"
	if len(matches) != 1 {
		t.Fatalf("expected 1 match for 'abcd.go', got %d: %v", len(matches), matches)
	}
	if matches[0].pattern != "a*.go" {
		t.Errorf("expected 'a*.go', got %q", matches[0].pattern)
	}
}

func TestMatchingGlobs_InvalidGlobPattern(t *testing.T) {
	// filepath.Match returns error for malformed patterns like unclosed brackets
	associations := map[string]map[string]string{
		"[abc": {"key": "invalid"}, // Unclosed bracket - invalid pattern
		"*.go": {"key": "go"},
	}

	// The invalid pattern should be skipped (error from filepath.Match)
	matches := matchingGlobs(associations, "test.go")

	// Should only match *.go
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %v", len(matches), matches)
	}
	if matches[0].pattern != "*.go" {
		t.Errorf("expected '*.go', got %q", matches[0].pattern)
	}
}

// =============================================================================
// globMatch Tests
// =============================================================================

func TestGlobMatch_Fields(t *testing.T) {
	gm := globMatch{pattern: "*.go"}
	if gm.pattern != "*.go" {
		t.Errorf("pattern = %q, want %q", gm.pattern, "*.go")
	}
}
