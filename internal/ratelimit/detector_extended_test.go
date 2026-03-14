package ratelimit

import (
	"strings"
	"testing"
)

func TestDetectorProviderAccessorAndUnknownDefaultPatterns(t *testing.T) {
	provider := Provider("unknown-provider")

	d, err := NewDetector(provider, nil)
	if err != nil {
		t.Fatalf("NewDetector() error = %v", err)
	}

	if got := d.Provider(); got != provider {
		t.Fatalf("Provider() = %q, want %q", got, provider)
	}

	if d.Detected() {
		t.Fatal("Detected() = true before any checks")
	}
	if d.Check("normal output without rate limiting") {
		t.Fatal("Check() = true for unknown provider without custom patterns")
	}
}

func TestObservingWriter_OversizedBufferAndCompaction(t *testing.T) {
	d, err := NewDetector(ProviderClaude, []string{`oversized-limit`, `compaction-limit`})
	if err != nil {
		t.Fatalf("NewDetector() error = %v", err)
	}

	var lines []string
	w := NewObservingWriter(d, func(line string) {
		lines = append(lines, line)
	})

	oversized := strings.Repeat("a", maxBufferSize) + " oversized-limit"
	n, err := w.Write([]byte(oversized))
	if err != nil {
		t.Fatalf("Write(oversized) error = %v", err)
	}
	if n != len(oversized) {
		t.Fatalf("Write(oversized) = %d, want %d", n, len(oversized))
	}
	if !d.Detected() {
		t.Fatal("Detected() = false after oversized buffer flush")
	}
	if len(lines) != 1 || lines[0] != oversized {
		t.Fatalf("oversized callback lines = %#v, want single oversized line", lines)
	}
	if len(w.buffer) != 0 {
		t.Fatalf("buffer length after oversized flush = %d, want 0", len(w.buffer))
	}

	d.Reset()
	lines = nil

	payload := strings.Repeat("b", 5000) + " compaction-limit\nok"
	n, err = w.Write([]byte(payload))
	if err != nil {
		t.Fatalf("Write(compaction) error = %v", err)
	}
	if n != len(payload) {
		t.Fatalf("Write(compaction) = %d, want %d", n, len(payload))
	}
	if !d.Detected() {
		t.Fatal("Detected() = false after newline-delimited match")
	}
	if len(lines) != 1 {
		t.Fatalf("callback lines after compaction write = %d, want 1", len(lines))
	}
	if got := lines[0]; !strings.Contains(got, "compaction-limit") {
		t.Fatalf("callback line = %q, want compaction marker", got)
	}
	if got := string(w.buffer); got != "ok" {
		t.Fatalf("buffer contents after compaction write = %q, want %q", got, "ok")
	}
	if cap(w.buffer) > 4096 {
		t.Fatalf("buffer capacity after compaction = %d, want <= 4096", cap(w.buffer))
	}
}
