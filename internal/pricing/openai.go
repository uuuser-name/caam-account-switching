package pricing

// OpenAIPricing contains per-1M token prices for OpenAI models.
var OpenAIPricing = map[string]TokenPrice{
	"gpt-4o":      {InputPer1M: 5.00, OutputPer1M: 15.00},
	"gpt-4o-mini": {InputPer1M: 0.15, OutputPer1M: 0.60},
}

var openAIAliases = map[string]string{
	"gpt4o":      "gpt-4o",
	"gpt4o-mini": "gpt-4o-mini",
}
