package cmd

import "testing"

func TestSanitizeTerminalText(t *testing.T) {
	raw := "\x1b[31mwork\x1b[0m-\u200blaptop\r\n"
	if got := sanitizeTerminalText(raw); got != "work-laptop" {
		t.Fatalf("sanitizeTerminalText() = %q, want %q", got, "work-laptop")
	}
}

func TestSanitizeTerminalBlock(t *testing.T) {
	raw := "Recommended: work\x1b[31m-main\x1b[0m\n  + ready\r\n  - stale\u200b token\n"
	want := "Recommended: work-main\n+ ready\n- stale token\n"
	if got := sanitizeTerminalBlock(raw); got != want {
		t.Fatalf("sanitizeTerminalBlock() = %q, want %q", got, want)
	}
}
