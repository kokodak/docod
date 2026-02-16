package knowledge

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

// GeminiSummarizer implements Summarizer using Gemini text generation.
type GeminiSummarizer struct {
	client        *genai.Client
	model         string
	promptBuilder *PromptBuilder
}

func NewGeminiSummarizer(ctx context.Context, apiKey string, modelName string) (*GeminiSummarizer, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}
	return &GeminiSummarizer{
		client:        client,
		model:         modelName,
		promptBuilder: &PromptBuilder{},
	}, nil
}

func (s *GeminiSummarizer) SummarizeFullDoc(ctx context.Context, archChunks, featChunks, confChunks []SearchChunk) (string, error) {
	prompt := s.promptBuilder.BuildFullDocPrompt(archChunks, featChunks, confChunks)
	return s.generate(ctx, prompt)
}

func (s *GeminiSummarizer) UpdateDocSection(ctx context.Context, currentContent string, relevantCode []SearchChunk) (string, error) {
	prompt := s.promptBuilder.BuildUpdateDocPrompt(currentContent, relevantCode)
	return s.generate(ctx, prompt)
}

func (s *GeminiSummarizer) RenderSectionFromDraft(ctx context.Context, draftJSON string, relevantCode []SearchChunk) (string, error) {
	prompt := s.promptBuilder.BuildRenderFromDraftPrompt(draftJSON, relevantCode)
	return s.generate(ctx, prompt)
}

func (s *GeminiSummarizer) GenerateNewSection(ctx context.Context, relevantCode []SearchChunk) (string, error) {
	prompt := s.promptBuilder.BuildNewSectionPrompt(relevantCode)
	return s.generate(ctx, prompt)
}

func (s *GeminiSummarizer) FindInsertionPoint(ctx context.Context, toc []string, newContent string) (int, error) {
	prompt := s.promptBuilder.BuildInsertionPointPrompt(toc, newContent)
	resp, err := s.generate(ctx, prompt)
	if err != nil {
		return -1, err
	}

	var index int
	_, err = fmt.Sscanf(strings.TrimSpace(resp), "%d", &index)
	if err != nil {
		words := strings.Fields(resp)
		for _, w := range words {
			if n, err := fmt.Sscanf(w, "%d", &index); err == nil && n == 1 {
				return index, nil
			}
		}
		return -1, fmt.Errorf("failed to parse index from LLM response: %s", resp)
	}
	return index, nil
}

func (s *GeminiSummarizer) generate(ctx context.Context, prompt string) (string, error) {
	contents := genai.Text(prompt)
	resp, err := s.client.Models.GenerateContent(ctx, s.model, contents, nil)
	if err != nil {
		return "", err
	}
	text := resp.Text()
	if text == "" {
		return "No analysis available.", nil
	}
	return cleanMarkdownOutput(text), nil
}
