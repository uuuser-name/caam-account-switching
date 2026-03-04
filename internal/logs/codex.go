package logs

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// CodexScanner parses Codex CLI JSONL logs.
// Codex stores logs at $CODEX_HOME/logs/session-*.jsonl (default: ~/.codex/logs/).
type CodexScanner struct {
	logDir string
}

// NewCodexScanner creates a scanner with the default log directory.
func NewCodexScanner() *CodexScanner {
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		homeDir, _ := os.UserHomeDir()
		codexHome = filepath.Join(homeDir, ".codex")
	}
	return &CodexScanner{logDir: filepath.Join(codexHome, "logs")}
}

// NewCodexScannerWithDir creates a scanner with a custom log directory.
// Useful for testing.
func NewCodexScannerWithDir(logDir string) *CodexScanner {
	return &CodexScanner{logDir: logDir}
}

// LogDir returns the default log directory.
func (s *CodexScanner) LogDir() string {
	return s.logDir
}

// Scan parses logs since the given time.
// If logDir is empty, uses the default log directory.
func (s *CodexScanner) Scan(ctx context.Context, logDir string, since time.Time) (*ScanResult, error) {
	if logDir == "" {
		logDir = s.logDir
	}

	result := &ScanResult{
		Provider: "codex",
		Since:    since,
		Until:    time.Now(),
		Entries:  make([]*LogEntry, 0),
	}

	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		return result, nil
	}

	entries, err := os.ReadDir(logDir)
	if err != nil {
		return result, err
	}

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			result.ParseErrors++
			continue
		}

		if !since.IsZero() && info.ModTime().Before(since) {
			continue
		}

		path := filepath.Join(logDir, name)
		if err := s.scanFile(ctx, path, since, result); err != nil {
			result.ParseErrors++
		}
	}

	return result, nil
}

func (s *CodexScanner) scanFile(ctx context.Context, filePath string, since time.Time, result *ScanResult) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		result.TotalEntries++
		line := scanner.Bytes()

		entry, err := s.parseLine(line)
		if err != nil {
			result.ParseErrors++
			continue
		}

		if !since.IsZero() && entry.Timestamp.Before(since) {
			continue
		}

		result.Entries = append(result.Entries, entry)
		result.ParsedEntries++
	}

	return scanner.Err()
}

func (s *CodexScanner) parseLine(line []byte) (*LogEntry, error) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, err
	}

	entry := &LogEntry{Raw: raw}
	entry.Timestamp = extractCodexTimestamp(raw)
	entry.Type = extractCodexString(raw, "type", "event", "event_type", "kind")
	entry.Model = extractCodexString(raw, "model", "model_name", "modelId", "model_id")
	if entry.Model == "" {
		if req, ok := asCodexMap(raw["request"]); ok {
			entry.Model = extractCodexString(req, "model", "model_name", "modelId", "model_id")
		}
	}
	if entry.Model == "" {
		if resp, ok := asCodexMap(raw["response"]); ok {
			entry.Model = extractCodexString(resp, "model", "model_name", "modelId", "model_id")
		}
	}

	entry.ConversationID = extractCodexString(raw,
		"session_id",
		"sessionId",
		"conversation_id",
		"conversationId",
		"thread_id",
	)

	entry.MessageID = extractCodexString(raw,
		"message_id",
		"messageId",
		"request_id",
		"requestId",
		"response_id",
	)

	if usage, ok := asCodexMap(raw["usage"]); ok {
		applyCodexTokenFields(entry, usage)
	}
	if req, ok := asCodexMap(raw["request"]); ok {
		if usage, ok := asCodexMap(req["usage"]); ok {
			applyCodexTokenFields(entry, usage)
		}
	}
	if resp, ok := asCodexMap(raw["response"]); ok {
		if usage, ok := asCodexMap(resp["usage"]); ok {
			applyCodexTokenFields(entry, usage)
		}
	}
	applyCodexTokenFields(entry, raw)

	if entry.TotalTokens == 0 {
		entry.TotalTokens = entry.CalculateTotalTokens()
	}

	return entry, nil
}

func extractCodexTimestamp(raw map[string]any) time.Time {
	for _, key := range []string{"timestamp", "time", "ts", "created_at", "createdAt", "created"} {
		if ts, ok := raw[key]; ok {
			if t, ok := parseCodexTimeAny(ts); ok {
				return t
			}
		}
	}
	if req, ok := asCodexMap(raw["request"]); ok {
		for _, key := range []string{"timestamp", "time", "ts", "created_at", "created"} {
			if ts, ok := req[key]; ok {
				if t, ok := parseCodexTimeAny(ts); ok {
					return t
				}
			}
		}
	}
	if resp, ok := asCodexMap(raw["response"]); ok {
		for _, key := range []string{"timestamp", "time", "ts", "created_at", "created"} {
			if ts, ok := resp[key]; ok {
				if t, ok := parseCodexTimeAny(ts); ok {
					return t
				}
			}
		}
	}
	return time.Time{}
}

func parseCodexTimeAny(value any) (time.Time, bool) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return time.Time{}, false
		}
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t, true
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t, true
		}
		if num, err := strconv.ParseFloat(v, 64); err == nil {
			return parseCodexUnixTime(num), true
		}
	case float64:
		return parseCodexUnixTime(v), true
	case int64:
		return time.Unix(v, 0), true
	case int:
		return time.Unix(int64(v), 0), true
	case json.Number:
		if num, err := v.Float64(); err == nil {
			return parseCodexUnixTime(num), true
		}
	}
	return time.Time{}, false
}

func parseCodexUnixTime(value float64) time.Time {
	switch {
	case value > 1e18:
		return time.Unix(0, int64(value))
	case value > 1e15:
		return time.Unix(0, int64(value*1e3))
	case value > 1e12:
		return time.Unix(0, int64(value*1e6))
	default:
		sec := int64(value)
		ns := int64((value - float64(sec)) * 1e9)
		return time.Unix(sec, ns)
	}
}

func extractCodexString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if val, ok := raw[key]; ok {
			if s, ok := val.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func applyCodexTokenFields(entry *LogEntry, raw map[string]any) {
	if entry.InputTokens == 0 {
		entry.InputTokens = extractCodexInt64(raw, "input_tokens", "inputTokens", "prompt_tokens", "promptTokens")
	}
	if entry.OutputTokens == 0 {
		entry.OutputTokens = extractCodexInt64(raw, "output_tokens", "outputTokens", "completion_tokens", "completionTokens")
	}
	if entry.CacheReadTokens == 0 {
		entry.CacheReadTokens = extractCodexInt64(raw, "cache_read_tokens", "cacheReadTokens", "cache_read_input_tokens")
	}
	if entry.CacheCreateTokens == 0 {
		entry.CacheCreateTokens = extractCodexInt64(raw, "cache_create_tokens", "cacheCreateTokens", "cache_creation_tokens", "cacheCreationTokens")
	}
	if entry.TotalTokens == 0 {
		entry.TotalTokens = extractCodexInt64(raw, "total_tokens", "totalTokens")
	}
}

func extractCodexInt64(raw map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if val, ok := raw[key]; ok {
			if n, ok := asCodexInt64(val); ok {
				return n
			}
		}
	}
	return 0
}

func asCodexInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n, true
		}
		if n, err := v.Float64(); err == nil {
			return int64(n), true
		}
	case string:
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

func asCodexMap(value any) (map[string]any, bool) {
	m, ok := value.(map[string]any)
	return m, ok
}

// Ensure CodexScanner implements Scanner interface.
var _ Scanner = (*CodexScanner)(nil)
