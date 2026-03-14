package ratelimit

import (
	"strings"
	"testing"
)

func TestNewDetector(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		patterns []string
		wantErr  bool
	}{
		{
			name:     "claude with defaults",
			provider: ProviderClaude,
			patterns: nil,
			wantErr:  false,
		},
		{
			name:     "codex with defaults",
			provider: ProviderCodex,
			patterns: nil,
			wantErr:  false,
		},
		{
			name:     "gemini with defaults",
			provider: ProviderGemini,
			patterns: nil,
			wantErr:  false,
		},
		{
			name:     "custom patterns",
			provider: ProviderClaude,
			patterns: []string{`test pattern`, `\d+`},
			wantErr:  false,
		},
		{
			name:     "invalid regex",
			provider: ProviderClaude,
			patterns: []string{`[invalid`},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := NewDetector(tt.provider, tt.patterns)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewDetector() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && d == nil {
				t.Error("NewDetector() returned nil detector without error")
			}
		})
	}
}

func TestDetector_Check(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		texts    []string
		want     bool
	}{
		{
			name:     "claude rate limit",
			provider: ProviderClaude,
			texts:    []string{"Error: rate limit exceeded"},
			want:     true,
		},
		{
			name:     "claude usage limit",
			provider: ProviderClaude,
			texts:    []string{"usage limit reached"},
			want:     true,
		},
		{
			name:     "claude 429",
			provider: ProviderClaude,
			texts:    []string{"HTTP 429 Too Many Requests"},
			want:     true,
		},
		{
			name:     "claude capacity",
			provider: ProviderClaude,
			texts:    []string{"Over capacity, please try again"},
			want:     true,
		},
		{
			name:     "claude exhausted signal",
			provider: ProviderClaude,
			texts:    []string{"weekly usage exhausted; switch account"},
			want:     true,
		},
		{
			name:     "claude retry window signal",
			provider: ProviderClaude,
			texts:    []string{"Please try again in 12m due to limits"},
			want:     true,
		},
		{
			name:     "codex rate limit",
			provider: ProviderCodex,
			texts:    []string{"rate-limit hit"},
			want:     true,
		},
		{
			name:     "codex quota exceeded",
			provider: ProviderCodex,
			texts:    []string{"quota exceeded for this model"},
			want:     true,
		},
		{
			name:     "codex out of credits",
			provider: ProviderCodex,
			texts:    []string{"You are out of credits for this billing period"},
			want:     true,
		},
		{
			name:     "codex insufficient credits",
			provider: ProviderCodex,
			texts:    []string{"insufficient credits to continue request"},
			want:     true,
		},
		{
			name:     "codex usage limit exact message",
			provider: ProviderCodex,
			texts: []string{
				"■ You've hit your usage limit. Visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at 3:05 PM.",
			},
			want: true,
		},
		{
			name:     "codex refresh token reused",
			provider: ProviderCodex,
			texts: []string{
				"Your access token could not be refreshed because your refresh token was already used. Please log out and sign in again.",
			},
			want: true,
		},
		{
			name:     "codex refresh token reused snake case",
			provider: ProviderCodex,
			texts:    []string{"oauth error: refresh_token_reused"},
			want:     true,
		},
		{
			name:     "gemini resource exhausted",
			provider: ProviderGemini,
			texts:    []string{"RESOURCE_EXHAUSTED: Too many requests"},
			want:     true,
		},
		{
			name:     "gemini quota",
			provider: ProviderGemini,
			texts:    []string{"Quota exceeded for project"},
			want:     true,
		},
		{
			name:     "normal output",
			provider: ProviderClaude,
			texts:    []string{"Here is the code you requested", "function foo() { return 42; }"},
			want:     false,
		},
		{
			name:     "multiple lines with rate limit at end",
			provider: ProviderClaude,
			texts:    []string{"Starting...", "Processing...", "Error: rate limit"},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := NewDetector(tt.provider, nil)
			if err != nil {
				t.Fatalf("NewDetector() error = %v", err)
			}

			var got bool
			for _, text := range tt.texts {
				got = d.Check(text)
			}

			if got != tt.want {
				t.Errorf("Check() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetector_StickyDetection(t *testing.T) {
	d, err := NewDetector(ProviderClaude, nil)
	if err != nil {
		t.Fatalf("NewDetector() error = %v", err)
	}

	// Initially not detected
	if d.Detected() {
		t.Error("Detected() = true before any check")
	}

	// Normal output
	d.Check("Hello, world!")
	if d.Detected() {
		t.Error("Detected() = true after normal output")
	}

	// Rate limit detected
	d.Check("rate limit exceeded")
	if !d.Detected() {
		t.Error("Detected() = false after rate limit")
	}

	// Should remain true after more checks
	d.Check("normal text again")
	if !d.Detected() {
		t.Error("Detected() = false, should be sticky")
	}

	// Reset should clear
	d.Reset()
	if d.Detected() {
		t.Error("Detected() = true after Reset()")
	}
}

func TestDetector_Reason(t *testing.T) {
	d, err := NewDetector(ProviderClaude, nil)
	if err != nil {
		t.Fatalf("NewDetector() error = %v", err)
	}

	// No reason before detection
	if d.Reason() != "" {
		t.Errorf("Reason() = %q before detection, want empty", d.Reason())
	}

	// Detect rate limit
	d.Check("Error: rate limit exceeded")

	reason := d.Reason()
	if reason == "" {
		t.Error("Reason() is empty after detection")
	}
	if !strings.Contains(strings.ToLower(reason), "rate") {
		t.Errorf("Reason() = %q, expected to contain 'rate'", reason)
	}
}

func TestObservingWriter(t *testing.T) {
	d, err := NewDetector(ProviderClaude, nil)
	if err != nil {
		t.Fatalf("NewDetector() error = %v", err)
	}

	var lines []string
	w := NewObservingWriter(d, func(line string) {
		lines = append(lines, line)
	})

	// Write some data with newlines
	input := "Line 1\nLine 2\nrate limit error\nLine 4\n"
	n, err := w.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(input) {
		t.Errorf("Write() = %d, want %d", n, len(input))
	}

	// Should have detected rate limit
	if !d.Detected() {
		t.Error("Detected() = false after rate limit in stream")
	}

	// Should have captured all lines
	if len(lines) != 4 {
		t.Errorf("callback called %d times, want 4", len(lines))
	}
}

func TestObservingWriter_PartialLines(t *testing.T) {
	d, err := NewDetector(ProviderClaude, nil)
	if err != nil {
		t.Fatalf("NewDetector() error = %v", err)
	}

	var lines []string
	w := NewObservingWriter(d, func(line string) {
		lines = append(lines, line)
	})

	// Write partial data
	if _, err := w.Write([]byte("Hello ")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := w.Write([]byte("rate ")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := w.Write([]byte("limit\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := w.Write([]byte("Done")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	w.Flush()

	// Should have detected rate limit
	if !d.Detected() {
		t.Error("Detected() = false after rate limit across writes")
	}

	// Should have two lines
	if len(lines) != 2 {
		t.Errorf("callback called %d times, want 2", len(lines))
	}
}

func TestProviderFromString(t *testing.T) {
	tests := []struct {
		input string
		want  Provider
	}{
		{"claude", ProviderClaude},
		{"Claude", ProviderClaude},
		{"CLAUDE", ProviderClaude},
		{"openclaw", ProviderCodex},
		{"open-claw", ProviderCodex},
		{"open_claw", ProviderCodex},
		{"codex", ProviderCodex},
		{"Codex", ProviderCodex},
		{"gemini", ProviderGemini},
		{"Gemini", ProviderGemini},
		{"unknown", ProviderClaude}, // default
		{"", ProviderClaude},        // default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ProviderFromString(tt.input)
			if got != tt.want {
				t.Errorf("ProviderFromString(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDefaultPatterns(t *testing.T) {
	patterns := DefaultPatterns()

	// Should have patterns for all providers
	if len(patterns[ProviderClaude]) == 0 {
		t.Error("No default patterns for Claude")
	}
	if len(patterns[ProviderCodex]) == 0 {
		t.Error("No default patterns for Codex")
	}
	if len(patterns[ProviderGemini]) == 0 {
		t.Error("No default patterns for Gemini")
	}
}
