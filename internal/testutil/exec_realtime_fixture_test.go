package testutil

import (
	"context"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	ptyctrl "github.com/Dicklesworthstone/coding_agent_account_manager/internal/pty"
)

func TestRealtimePTYSessionCapturesTranscript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pty controller not supported on windows")
	}

	session, err := StartRealtimePTYSessionFromArgs(
		"sh",
		[]string{"-lc", "printf 'ready\\n'; IFS= read line; printf 'echo:%s\\n' \"$line\""},
		&ptyctrl.Options{Rows: 24, Cols: 80},
	)
	if err != nil {
		t.Fatalf("StartRealtimePTYSessionFromArgs(): %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	line, err := session.ReadLine(ctx)
	if err != nil {
		t.Fatalf("ReadLine(): %v", err)
	}
	if !strings.Contains(line, "ready") {
		t.Fatalf("expected ready line, got %q", line)
	}

	if err := session.InjectCommand("hello"); err != nil {
		t.Fatalf("InjectCommand(): %v", err)
	}

	output, err := session.WaitForPattern(ctx, regexp.MustCompile(`echo:hello`), 2*time.Second)
	if err != nil {
		t.Fatalf("WaitForPattern(): %v", err)
	}
	if !strings.Contains(output, "echo:hello") {
		t.Fatalf("expected echoed output, got %q", output)
	}

	transcript := session.Transcript()
	if !strings.Contains(transcript, "ready") {
		t.Fatalf("expected transcript to include ready, got %q", transcript)
	}
	if !strings.Contains(transcript, "echo:hello") {
		t.Fatalf("expected transcript to include echoed line, got %q", transcript)
	}
}
