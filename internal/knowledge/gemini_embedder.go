package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/genai"
)

// GeminiEmbedder implements Embedder using Google's Gemini API.
type GeminiEmbedder struct {
	client    *genai.Client
	model     string
	dimension int
}

func NewGeminiEmbedder(ctx context.Context, apiKey string, modelName string, dim int) (*GeminiEmbedder, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}
	return &GeminiEmbedder{
		client:    client,
		model:     modelName,
		dimension: dim,
	}, nil
}

const embedBatchSize = 50
const embedBatchDelay = 700 * time.Millisecond
const embedRetryDelay = 6 * time.Second
const embedMaxRetries = 5

func (g *GeminiEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	var results [][]float32

	var config *genai.EmbedContentConfig
	if g.dimension > 0 {
		dim := int32(g.dimension)
		config = &genai.EmbedContentConfig{OutputDimensionality: &dim}
	}

	for i := 0; i < len(texts); i += embedBatchSize {
		if i > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(embedBatchDelay):
			}
		}

		end := i + embedBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		contents := make([]*genai.Content, 0, len(batch))
		for _, text := range batch {
			contents = append(contents, genai.NewContentFromText(text, genai.RoleUser))
		}

		var res *genai.EmbedContentResponse
		var err error
		for attempt := 0; attempt <= embedMaxRetries; attempt++ {
			res, err = g.client.Models.EmbedContent(ctx, g.model, contents, config)
			if err == nil {
				break
			}
			if !isRateLimitError(err) || attempt == embedMaxRetries {
				return nil, fmt.Errorf("failed to embed text: %w", err)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(embedRetryDelay):
			}
		}

		if len(res.Embeddings) != len(batch) {
			return nil, fmt.Errorf("embedding count mismatch: got %d, expected %d", len(res.Embeddings), len(batch))
		}
		for _, emb := range res.Embeddings {
			results = append(results, emb.Values)
		}
	}
	return results, nil
}

func (g *GeminiEmbedder) Dimension() int {
	return g.dimension
}

func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *genai.APIError
	if errors.As(err, &apiErr) && apiErr.Code == 429 {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "429") || strings.Contains(s, "RESOURCE_EXHAUSTED") || strings.Contains(s, "quota")
}
