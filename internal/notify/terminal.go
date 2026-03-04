package notify

import (
	"fmt"
	"io"
	"os"
)

// TerminalNotifier delivers alerts to a writer (usually stderr).
type TerminalNotifier struct {
	writer io.Writer
	color  bool
}

// NewTerminalNotifier creates a new TerminalNotifier.
func NewTerminalNotifier(w io.Writer, color bool) *TerminalNotifier {
	if w == nil {
		w = os.Stderr
	}
	return &TerminalNotifier{
		writer: w,
		color:  color,
	}
}

func (n *TerminalNotifier) Name() string {
	return "terminal"
}

func (n *TerminalNotifier) Available() bool {
	return true
}

func (n *TerminalNotifier) Notify(alert *Alert) error {
	if n == nil || alert == nil {
		return nil
	}

	var prefix string
	var colorCode string
	var resetCode = "\033[0m"

	switch alert.Level {
	case Info:
		prefix = "[INFO]"
		colorCode = "\033[36m" // Cyan
	case Warning:
		prefix = "[WARN]"
		colorCode = "\033[33m" // Yellow
	case Critical:
		prefix = "[CRIT]"
		colorCode = "\033[31m" // Red
	default:
		prefix = "[ALERT]"
		colorCode = "\033[37m" // White
	}

	title := alert.Title
	message := alert.Message

	if !n.color {
		colorCode = ""
		resetCode = ""
	}

	// Format: [LEVEL] Title: Message (Profile)
	line := fmt.Sprintf("%s%s %s%s: %s", colorCode, prefix, title, resetCode, message)

	if alert.Profile != "" {
		line += fmt.Sprintf(" (%s)", alert.Profile)
	}

	writer := n.writer
	if writer == nil {
		writer = os.Stderr
	}
	if _, err := fmt.Fprintln(writer, line); err != nil {
		return err
	}

	if alert.Action != "" {
		if _, err := fmt.Fprintf(writer, "       Action: %s\n", alert.Action); err != nil {
			return err
		}
	}

	return nil
}
