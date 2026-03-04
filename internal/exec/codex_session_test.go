package exec

import (
	"bytes"
	"testing"
)

func TestCodexSessionCaptureObserveLine(t *testing.T) {
	c := &codexSessionCapture{}
	c.ObserveLine("To continue this session, run codex resume 019b2e3d-b524-7c22-91da-47de9068d09a")
	if got := c.ID(); got != "019b2e3d-b524-7c22-91da-47de9068d09a" {
		t.Fatalf("unexpected session id: %q", got)
	}
	if got := c.Command(); got != "codex resume 019b2e3d-b524-7c22-91da-47de9068d09a" {
		t.Fatalf("unexpected resume command: %q", got)
	}
}

func TestCodexSessionCaptureIgnoreNonMatches(t *testing.T) {
	c := &codexSessionCapture{}
	c.ObserveLine("nothing to see here")
	if got := c.ID(); got != "" {
		t.Fatalf("expected empty id, got %q", got)
	}
}

func TestLineObserverWriter_PartialWritesAndFlush(t *testing.T) {
	c := &codexSessionCapture{}
	buf := new(bytes.Buffer)
	w := newLineObserverWriter(buf, c.ObserveLine)

	part1 := []byte("To continue this session, run codex res")
	part2 := []byte("ume 019b2e3d-b524-7c22-91da-47de9068d09a")

	if _, err := w.Write(part1); err != nil {
		t.Fatalf("write part1: %v", err)
	}
	if _, err := w.Write(part2); err != nil {
		t.Fatalf("write part2: %v", err)
	}

	// No newline, so Flush should process the buffered partial line.
	w.Flush()

	if got := c.ID(); got != "019b2e3d-b524-7c22-91da-47de9068d09a" {
		t.Fatalf("unexpected session id: %q", got)
	}
}

func TestLineObserverWriter_MultipleLinesTakesLast(t *testing.T) {
	c := &codexSessionCapture{}
	buf := new(bytes.Buffer)
	w := newLineObserverWriter(buf, c.ObserveLine)

	_, _ = w.Write([]byte("codex resume 11111111-1111-1111-1111-111111111111\r\n"))
	_, _ = w.Write([]byte("To continue this session, run codex resume 22222222-2222-2222-2222-222222222222\n"))
	w.Flush()

	if got := c.ID(); got != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("expected last session id, got %q", got)
	}
}

func TestCodexSessionCapture_ClaudeLine(t *testing.T) {
	c := &codexSessionCapture{}
	c.ObserveLine("To continue this session, run claude resume session-abc123")
	if got := c.Provider(); got != "claude" {
		t.Fatalf("unexpected provider: %q", got)
	}
	if got := c.ID(); got != "session-abc123" {
		t.Fatalf("unexpected session id: %q", got)
	}
	if got := c.Command(); got != "claude resume session-abc123" {
		t.Fatalf("unexpected resume command: %q", got)
	}
}

func TestCodexSessionCapture_ProviderFilter(t *testing.T) {
	c := &codexSessionCapture{provider: "codex"}
	c.ObserveLine("run claude resume session-abc123")
	if got := c.ID(); got != "" {
		t.Fatalf("expected no session capture for mismatched provider, got %q", got)
	}
	c.ObserveLine("run codex resume 33333333-3333-3333-3333-333333333333")
	if got := c.ID(); got != "33333333-3333-3333-3333-333333333333" {
		t.Fatalf("unexpected session id: %q", got)
	}
}
