package pricing

// GeminiPricing contains per-1M token prices for Gemini models.
// Pricing values are TBD and should be updated when public rates are available.
var GeminiPricing = map[string]TokenPrice{
	"gemini-ultra": {},
	"gemini-pro":   {},
}

var geminiAliases = map[string]string{
	"gemini-1.5-pro":   "gemini-pro",
	"gemini-1.5-ultra": "gemini-ultra",
}
