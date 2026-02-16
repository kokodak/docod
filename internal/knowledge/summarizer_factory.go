package knowledge

import (
	"context"
	"fmt"
	"strings"
)

type SummarizerOptions struct {
	Provider string
	APIKey   string
	Model    string
	BaseURL  string
}

func NewSummarizer(ctx context.Context, opts SummarizerOptions) (Summarizer, error) {
	provider := strings.ToLower(strings.TrimSpace(opts.Provider))
	if provider == "" {
		provider = "gemini"
	}

	switch provider {
	case "gemini":
		return NewGeminiSummarizer(ctx, opts.APIKey, opts.Model)
	case "openai":
		return NewOpenAISummarizer(opts.APIKey, opts.Model, opts.BaseURL), nil
	default:
		return nil, fmt.Errorf("unsupported summarizer provider: %s", opts.Provider)
	}
}
