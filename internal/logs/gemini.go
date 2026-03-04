package logs

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// GeminiScanner parses Gemini CLI JSONL logs.
// Gemini CLI stores logs at ~/.gemini/logs/ or $GEMINI_HOME/logs/.
type GeminiScanner struct {
	logDir string
}

// NewGeminiScanner creates a scanner with the default log directory.
func NewGeminiScanner() *GeminiScanner {
	homeDir, _ := os.UserHomeDir()
	geminiHome := os.Getenv("GEMINI_HOME")
	if geminiHome == "" {
		geminiHome = filepath.Join(homeDir, ".gemini")
	}
	return &GeminiScanner{logDir: filepath.Join(geminiHome, "logs")}
}

// NewGeminiScannerWithDir creates a scanner with a custom log directory.
// Useful for testing.
func NewGeminiScannerWithDir(logDir string) *GeminiScanner {
	return &GeminiScanner{logDir: logDir}
}

// LogDir returns the default log directory.
func (s *GeminiScanner) LogDir() string {
	return s.logDir
}

// Scan parses logs since the given time.
// If logDir is empty, uses the default log directory.
func (s *GeminiScanner) Scan(ctx context.Context, logDir string, since time.Time) (*ScanResult, error) {
	if logDir == "" {
		logDir = s.logDir
	}

	result := &ScanResult{
		Provider: "gemini",
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

		info, err := entry.Info()
		if err != nil {
			result.ParseErrors++
			continue
		}

		if !since.IsZero() && info.ModTime().Before(since) {
			continue
		}

		path := filepath.Join(logDir, entry.Name())
		if err := s.scanFile(ctx, path, since, result); err != nil {
			result.ParseErrors++
		}
	}

	return result, nil
}

func (s *GeminiScanner) scanFile(ctx context.Context, filePath string, since time.Time, result *ScanResult) error {
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

func (s *GeminiScanner) parseLine(line []byte) (*LogEntry, error) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, err
	}

	entry := &LogEntry{Raw: raw}
	entry.Timestamp = extractTimestamp(raw)
	entry.Type = extractString(raw,
		"type",
		"event",
		"event_type",
		"name",
	)
	entry.Model = extractString(raw,
		"model",
		"model_name",
		"modelId",
		"model_id",
	)
	if entry.Model == "" {
		if attrs, ok := asMap(raw["attributes"]); ok {
			entry.Model = extractString(attrs, "model", "model_name", "modelId", "model_id")
		}
	}

	entry.ConversationID = extractString(raw,
		"conversation_id",
		"conversationId",
		"session_id",
		"sessionId",
		"chat_id",
		"chatId",
		"thread_id",
	)
	if entry.ConversationID == "" {
		if attrs, ok := asMap(raw["attributes"]); ok {
			entry.ConversationID = extractString(attrs, "conversation_id", "conversationId", "session_id", "sessionId")
		}
	}

	entry.MessageID = extractString(raw,
		"message_id",
		"messageId",
		"request_id",
		"requestId",
		"prompt_id",
		"promptId",
	)
	if entry.MessageID == "" {
		if attrs, ok := asMap(raw["attributes"]); ok {
			entry.MessageID = extractString(attrs, "message_id", "messageId", "request_id", "requestId")
		}
	}

	if usage, ok := asMap(raw["usage"]); ok {
		applyTokenFields(entry, usage)
	}
	if tokens, ok := asMap(raw["tokens"]); ok {
		applyTokenFields(entry, tokens)
	}
	applyTokenFields(entry, raw)

	if entry.TotalTokens == 0 {
		entry.TotalTokens = entry.CalculateTotalTokens()
	}

	return entry, nil
}

func extractTimestamp(raw map[string]any) time.Time {
	for _, key := range []string{"timestamp", "time", "ts", "created_at", "createdAt"} {
		if ts, ok := raw[key]; ok {
			if t, ok := parseTimeAny(ts); ok {
				return t
			}
		}
	}
	if attrs, ok := asMap(raw["attributes"]); ok {
		for _, key := range []string{"timestamp", "time", "ts"} {
			if ts, ok := attrs[key]; ok {
				if t, ok := parseTimeAny(ts); ok {
					return t
				}
			}
		}
	}
	return time.Time{}
}

func parseTimeAny(value any) (time.Time, bool) {
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
			return parseUnixTime(num), true
		}
	case float64:
		return parseUnixTime(v), true
	case int64:
		return time.Unix(v, 0), true
	case int:
		return time.Unix(int64(v), 0), true
	case json.Number:
		if num, err := v.Float64(); err == nil {
			return parseUnixTime(num), true
		}
	}
	return time.Time{}, false
}

func parseUnixTime(value float64) time.Time {
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

func extractString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if val, ok := raw[key]; ok {
			if s, ok := val.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func applyTokenFields(entry *LogEntry, raw map[string]any) {
	if entry.InputTokens == 0 {
		entry.InputTokens = extractInt64(raw, "input_tokens", "inputTokens", "prompt_tokens", "promptTokens")
	}
	if entry.OutputTokens == 0 {
		entry.OutputTokens = extractInt64(raw, "output_tokens", "outputTokens", "completion_tokens", "completionTokens")
	}
	if entry.CacheReadTokens == 0 {
		entry.CacheReadTokens = extractInt64(raw, "cache_read_tokens", "cacheReadTokens", "cache_read_input_tokens")
	}
	if entry.CacheCreateTokens == 0 {
		entry.CacheCreateTokens = extractInt64(raw, "cache_create_tokens", "cacheCreateTokens", "cache_creation_tokens", "cacheCreationTokens")
	}
	if entry.TotalTokens == 0 {
		entry.TotalTokens = extractInt64(raw, "total_tokens", "totalTokens")
	}
}

func extractInt64(raw map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if val, ok := raw[key]; ok {
			if n, ok := asInt64(val); ok {
				return n
			}
		}
	}
	return 0
}

func asInt64(value any) (int64, bool) {
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

func asMap(value any) (map[string]any, bool) {
	m, ok := value.(map[string]any)
	return m, ok
}

// Ensure GeminiScanner implements Scanner interface.
var _ Scanner = (*GeminiScanner)(nil)
