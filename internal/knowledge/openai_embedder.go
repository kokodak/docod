package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	openAIEmbedBatchSize = 64
	openAIEmbedDelay     = 400 * time.Millisecond
	openAIEmbedRetries   = 5
	openAIRetryDelay     = 3 * time.Second
)

type OpenAIEmbedder struct {
	client    *http.Client
	apiKey    string
	model     string
	dimension int
	endpoint  string
}

type openAIEmbeddingRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions *int     `json:"dimensions,omitempty"`
}

type openAIEmbeddingItem struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type openAIEmbeddingResponse struct {
	Object string                `json:"object"`
	Data   []openAIEmbeddingItem `json:"data"`
	Model  string                `json:"model"`
}

type openAIErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

func NewOpenAIEmbedder(apiKey, model string, dim int, baseURL string) *OpenAIEmbedder {
	endpoint := strings.TrimSpace(baseURL)
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1/embeddings"
	}
	return &OpenAIEmbedder{
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		apiKey:    apiKey,
		model:     model,
		dimension: dim,
		endpoint:  endpoint,
	}
}

func (o *OpenAIEmbedder) Dimension() int {
	return o.dimension
}

func (o *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if strings.TrimSpace(o.apiKey) == "" {
		return nil, fmt.Errorf("openai api key is required")
	}
	if strings.TrimSpace(o.model) == "" {
		return nil, fmt.Errorf("openai embedding model is required")
	}
	if len(texts) == 0 {
		return nil, nil
	}

	results := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += openAIEmbedBatchSize {
		if i > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(openAIEmbedDelay):
			}
		}
		end := i + openAIEmbedBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]
		vecs, err := o.embedBatch(ctx, batch)
		if err != nil {
			return nil, err
		}
		results = append(results, vecs...)
	}
	return results, nil
}

func (o *OpenAIEmbedder) embedBatch(ctx context.Context, batch []string) ([][]float32, error) {
	if len(batch) == 0 {
		return nil, nil
	}

	payload := openAIEmbeddingRequest{
		Model: o.model,
		Input: batch,
	}
	if o.dimension > 0 {
		payload.Dimensions = &o.dimension
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 0; attempt <= openAIEmbedRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := o.client.Do(req)
		if err != nil {
			lastErr = err
			if attempt == openAIEmbedRetries {
				break
			}
			if !waitOrCancel(ctx, openAIRetryDelay) {
				return nil, ctx.Err()
			}
			continue
		}

		data, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("openai embeddings request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
			if attempt == openAIEmbedRetries {
				break
			}
			if !waitOrCancel(ctx, openAIRetryDelay) {
				return nil, ctx.Err()
			}
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			msg := strings.TrimSpace(string(data))
			var errBody openAIErrorBody
			if json.Unmarshal(data, &errBody) == nil && strings.TrimSpace(errBody.Error.Message) != "" {
				msg = strings.TrimSpace(errBody.Error.Message)
			}
			return nil, fmt.Errorf("openai embeddings request failed (%d): %s", resp.StatusCode, msg)
		}

		var parsed openAIEmbeddingResponse
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, err
		}
		if len(parsed.Data) != len(batch) {
			return nil, fmt.Errorf("embedding count mismatch: got %d, expected %d", len(parsed.Data), len(batch))
		}

		out := make([][]float32, len(batch))
		for _, item := range parsed.Data {
			if item.Index < 0 || item.Index >= len(batch) {
				continue
			}
			out[item.Index] = item.Embedding
		}
		for i := range out {
			if len(out[i]) == 0 {
				return nil, fmt.Errorf("embedding missing at index %d", i)
			}
		}
		return out, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("openai embeddings request failed")
	}
	return nil, lastErr
}

func waitOrCancel(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
