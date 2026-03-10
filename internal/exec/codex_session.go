package exec

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"sync"
)

var providerResumeCommandRe = regexp.MustCompile(`(?i)\b(codex|claude)\s+resume\b(?:\s+.*)?`)
var sessionTokenRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// maxBufferSize is the maximum buffer size before forcing a flush (64KB).
// This prevents unbounded memory growth when output contains no newlines.
const maxBufferSize = 64 * 1024

type codexSessionCapture struct {
	mu       sync.Mutex
	provider string
	id       string
	command  string
}

func (c *codexSessionCapture) ObserveLine(line string) {
	match := providerResumeCommandRe.FindString(strings.TrimSpace(line))
	if strings.TrimSpace(match) == "" {
		return
	}
	args, err := splitCommandLine(match)
	if err != nil || len(args) < 2 {
		return
	}
	provider := strings.ToLower(strings.TrimSpace(args[0]))
	if c.provider != "" && c.provider != provider {
		return
	}
	c.mu.Lock()
	c.provider = provider
	c.id = extractResumeSessionID(args)
	c.command = strings.TrimSpace(match)
	c.mu.Unlock()
}

func (c *codexSessionCapture) ID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.id
}

func (c *codexSessionCapture) Command() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.command
}

func (c *codexSessionCapture) Provider() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.provider
}

func splitCommandLine(s string) ([]string, error) {
	var args []string
	var current strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		args = append(args, current.String())
		current.Reset()
	}

	for _, r := range s {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		default:
			current.WriteRune(r)
		}
	}

	if escaped {
		current.WriteRune('\\')
	}
	if quote != 0 {
		return nil, io.ErrUnexpectedEOF
	}
	flush()
	return args, nil
}

func extractResumeSessionID(args []string) string {
	for _, arg := range args {
		normalized := strings.ToLower(strings.TrimSpace(arg))
		if sessionTokenRe.MatchString(normalized) {
			return arg
		}
	}
	for i := 2; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			continue
		}
		return args[i]
	}
	return ""
}

type lineObserverWriter struct {
	dst    io.Writer
	onLine func(line string)

	mu  sync.Mutex
	buf []byte
}

func newLineObserverWriter(dst io.Writer, onLine func(line string)) *lineObserverWriter {
	return &lineObserverWriter{dst: dst, onLine: onLine}
}

func (w *lineObserverWriter) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	if n <= 0 {
		return n, err
	}

	w.mu.Lock()
	w.buf = append(w.buf, p[:n]...)

	for {
		nl := bytes.IndexByte(w.buf, '\n')
		if nl < 0 {
			break
		}

		lineBytes := w.buf[:nl]
		if len(lineBytes) > 0 && lineBytes[len(lineBytes)-1] == '\r' {
			lineBytes = lineBytes[:len(lineBytes)-1]
		}
		w.onLine(string(lineBytes))

		w.buf = w.buf[nl+1:]
	}

	// Enforce buffer limit to prevent OOM on long lines without newlines
	if len(w.buf) > maxBufferSize {
		// Process oversized buffer as a partial line
		lineBytes := w.buf
		if len(lineBytes) > 0 && lineBytes[len(lineBytes)-1] == '\r' {
			lineBytes = lineBytes[:len(lineBytes)-1]
		}
		w.onLine(string(lineBytes))
		w.buf = nil
	}

	// Compact buffer if it has grown large but contains little data
	// (capacity > 4KB and usage < 25%)
	if cap(w.buf) > 4096 && len(w.buf) < cap(w.buf)/4 {
		newBuf := make([]byte, len(w.buf))
		copy(newBuf, w.buf)
		w.buf = newBuf
	}

	w.mu.Unlock()

	return n, err
}

func (w *lineObserverWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) == 0 {
		return
	}

	lineBytes := w.buf
	if len(lineBytes) > 0 && lineBytes[len(lineBytes)-1] == '\r' {
		lineBytes = lineBytes[:len(lineBytes)-1]
	}
	w.onLine(string(lineBytes))
	w.buf = nil
}
