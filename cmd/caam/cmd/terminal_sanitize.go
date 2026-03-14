package cmd

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	terminalANSIEscapeRe  = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	terminalOSCSequenceRe = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
)

// sanitizeTerminalText removes terminal control sequences and collapses
// remaining control characters to plain spaces before rendering to users.
func sanitizeTerminalText(value string) string {
	return sanitizeTerminalLine(stripTerminalSequences(value))
}

// sanitizeTerminalBlock preserves line structure for formatted status output
// while still stripping control sequences from each rendered line.
func sanitizeTerminalBlock(value string) string {
	cleaned := stripTerminalSequences(value)
	hasTrailingNewline := strings.HasSuffix(cleaned, "\n")
	lines := strings.Split(cleaned, "\n")
	for i, line := range lines {
		lines[i] = sanitizeTerminalLine(line)
	}
	block := strings.Join(lines, "\n")
	if hasTrailingNewline && !strings.HasSuffix(block, "\n") {
		block += "\n"
	}
	return block
}

func stripTerminalSequences(value string) string {
	cleaned := terminalOSCSequenceRe.ReplaceAllString(value, "")
	return terminalANSIEscapeRe.ReplaceAllString(cleaned, "")
}

func sanitizeTerminalLine(value string) string {
	value = strings.Map(func(r rune) rune {
		switch {
		case unicode.In(r, unicode.Cf):
			return -1
		case unicode.IsControl(r):
			return ' '
		default:
			return r
		}
	}, value)
	return strings.Join(strings.Fields(value), " ")
}
