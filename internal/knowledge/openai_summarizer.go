package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type OpenAISummarizer struct {
	client        *http.Client
	apiKey        string
	model         string
	endpoint      string
	promptBuilder *PromptBuilder
}

type openAIChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openAIChatMessage `json:"messages"`
	Temperature float64             `json:"temperature,omitempty"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIChatMessage `json:"message"`
	} `json:"choices"`
}

func NewOpenAISummarizer(apiKey, model, baseURL string) *OpenAISummarizer {
	endpoint := strings.TrimSpace(baseURL)
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1/chat/completions"
	} else {
		endpoint = strings.TrimRight(endpoint, "/")
		if !strings.HasSuffix(endpoint, "/chat/completions") {
			if strings.HasSuffix(endpoint, "/v1") {
				endpoint += "/chat/completions"
			} else {
				endpoint += "/v1/chat/completions"
			}
		}
	}
	return &OpenAISummarizer{
		client: &http.Client{
			Timeout: 90 * time.Second,
		},
		apiKey:        apiKey,
		model:         model,
		endpoint:      endpoint,
		promptBuilder: &PromptBuilder{},
	}
}

func (s *OpenAISummarizer) SummarizeFullDoc(ctx context.Context, archChunks, featChunks, confChunks []SearchChunk) (string, error) {
	prompt := s.promptBuilder.BuildFullDocPrompt(archChunks, featChunks, confChunks)
	return s.generate(ctx, prompt)
}

func (s *OpenAISummarizer) UpdateDocSection(ctx context.Context, currentContent string, relevantCode []SearchChunk) (string, error) {
	prompt := s.promptBuilder.BuildUpdateDocPrompt(currentContent, relevantCode)
	return s.generate(ctx, prompt)
}

func (s *OpenAISummarizer) RenderSectionFromDraft(ctx context.Context, draftJSON string, relevantCode []SearchChunk) (string, error) {
	prompt := s.promptBuilder.BuildRenderFromDraftPrompt(draftJSON, relevantCode)
	return s.generate(ctx, prompt)
}

func (s *OpenAISummarizer) GenerateNewSection(ctx context.Context, relevantCode []SearchChunk) (string, error) {
	prompt := s.promptBuilder.BuildNewSectionPrompt(relevantCode)
	return s.generate(ctx, prompt)
}

func (s *OpenAISummarizer) FindInsertionPoint(ctx context.Context, toc []string, newContent string) (int, error) {
	prompt := s.promptBuilder.BuildInsertionPointPrompt(toc, newContent)
	resp, err := s.generate(ctx, prompt)
	if err != nil {
		return -1, err
	}
	val := strings.TrimSpace(resp)
	n, err := strconv.Atoi(val)
	if err == nil {
		return n, nil
	}
	for _, token := range strings.Fields(val) {
		if n, err := strconv.Atoi(token); err == nil {
			return n, nil
		}
	}
	return -1, fmt.Errorf("failed to parse index from LLM response: %s", resp)
}

func (s *OpenAISummarizer) generate(ctx context.Context, prompt string) (string, error) {
	if strings.TrimSpace(s.apiKey) == "" {
		return "", fmt.Errorf("openai api key is required")
	}
	if strings.TrimSpace(s.model) == "" {
		return "", fmt.Errorf("openai model is required")
	}

	reqBody := openAIChatRequest{
		Model: s.model,
		Messages: []openAIChatMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: 0.1,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai chat request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed openAIChatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "No analysis available.", nil
	}
	text := parsed.Choices[0].Message.Content
	if strings.TrimSpace(text) == "" {
		return "No analysis available.", nil
	}
	return cleanMarkdownOutput(text), nil
}
