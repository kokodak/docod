package knowledge

import (
	"context"
	"fmt"
	"strings"
)

type EmbedderOptions struct {
	Provider  string
	APIKey    string
	Model     string
	Dimension int
	BaseURL   string
}

func NewEmbedder(ctx context.Context, opts EmbedderOptions) (Embedder, error) {
	provider := strings.ToLower(strings.TrimSpace(opts.Provider))
	if provider == "" {
		provider = "gemini"
	}

	switch provider {
	case "gemini":
		return NewGeminiEmbedder(ctx, opts.APIKey, opts.Model, opts.Dimension)
	case "openai":
		return NewOpenAIEmbedder(opts.APIKey, opts.Model, opts.Dimension, opts.BaseURL), nil
	case "ollama":
		return NewOllamaEmbedder(opts.Model, opts.Dimension, opts.BaseURL), nil
	default:
		return nil, fmt.Errorf("unsupported embedder provider: %s", opts.Provider)
	}
}
