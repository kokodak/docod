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
	ollamaEmbedBatchSize = 64
	ollamaEmbedDelay     = 200 * time.Millisecond
)

type OllamaEmbedder struct {
	client    *http.Client
	model     string
	dimension int
	endpoint  string
}

type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func NewOllamaEmbedder(model string, dim int, baseURL string) *OllamaEmbedder {
	url := strings.TrimSpace(baseURL)
	if url == "" {
		url = "http://127.0.0.1:11434"
	}
	url = strings.TrimRight(url, "/")
	if !strings.HasSuffix(url, "/api/embed") {
		url += "/api/embed"
	}

	return &OllamaEmbedder{
		client: &http.Client{
			Timeout: 90 * time.Second,
		},
		model:     model,
		dimension: dim,
		endpoint:  url,
	}
}

func (o *OllamaEmbedder) Dimension() int {
	return o.dimension
}

func (o *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if strings.TrimSpace(o.model) == "" {
		return nil, fmt.Errorf("ollama embedding model is required")
	}
	if len(texts) == 0 {
		return nil, nil
	}

	out := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += ollamaEmbedBatchSize {
		if i > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(ollamaEmbedDelay):
			}
		}
		end := i + ollamaEmbedBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]
		vecs, err := o.embedBatch(ctx, batch)
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}

	if o.dimension <= 0 && len(out) > 0 {
		o.dimension = len(out[0])
	}
	return out, nil
}

func (o *OllamaEmbedder) embedBatch(ctx context.Context, batch []string) ([][]float32, error) {
	reqBody := ollamaEmbedRequest{
		Model: o.model,
		Input: batch,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama embed request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed ollamaEmbedResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Embeddings) != len(batch) {
		return nil, fmt.Errorf("ollama embedding count mismatch: got %d, expected %d", len(parsed.Embeddings), len(batch))
	}
	return parsed.Embeddings, nil
}
