package pricing

// ClaudePricing contains per-1M token prices for Claude models.
var ClaudePricing = map[string]TokenPrice{
	"claude-3-opus":     {InputPer1M: 15.00, OutputPer1M: 75.00, CacheReadPer1M: 1.50, CacheCreatePer1M: 18.75},
	"claude-3.5-sonnet": {InputPer1M: 3.00, OutputPer1M: 15.00, CacheReadPer1M: 0.30, CacheCreatePer1M: 3.75},
	"claude-3-haiku":    {InputPer1M: 0.25, OutputPer1M: 1.25, CacheReadPer1M: 0.025, CacheCreatePer1M: 0.3125},
}

var claudeAliases = map[string]string{
	"claude-3-5-sonnet": "claude-3.5-sonnet",
	"claude-3-sonnet":   "claude-3.5-sonnet",
}
