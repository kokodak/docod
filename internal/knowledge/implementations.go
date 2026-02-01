package knowledge

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// GeminiEmbedder implements Embedder using Google's Gemini API.
type GeminiEmbedder struct {
	client    *genai.Client
	model     string
	dimension int
}

func NewGeminiEmbedder(ctx context.Context, apiKey string, modelName string, dim int) (*GeminiEmbedder, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}
	return &GeminiEmbedder{
		client:    client,
		model:     modelName,
		dimension: dim,
	}, nil
}

func (g *GeminiEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	em := g.client.EmbeddingModel(g.model)
	var results [][]float32

	for _, text := range texts {
		res, err := em.EmbedContent(ctx, genai.Text(text))
		if err != nil {
			return nil, fmt.Errorf("failed to embed text: %w", err)
		}
		results = append(results, res.Embedding.Values)
	}
	return results, nil
}

func (g *GeminiEmbedder) Dimension() int {
	return g.dimension
}

// GeminiSummarizer implements Summarizer using Google's Gemini Pro.
type GeminiSummarizer struct {
	client *genai.Client
	model  string
}

func NewGeminiSummarizer(ctx context.Context, apiKey string, modelName string) (*GeminiSummarizer, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}
	return &GeminiSummarizer{
		client: client,
		model:  modelName,
	}, nil
}

func (s *GeminiSummarizer) Summarize(ctx context.Context, chunk SearchChunk, contextUnits []SearchChunk) (string, error) {
	model := s.client.GenerativeModel(s.model)
	
	var sb strings.Builder
	sb.WriteString("You are an expert software architect and technical writer. Your task is to provide a deep, insightful interpretation of the following code unit.\n\n")
	
	sb.WriteString("### TARGET UNIT TO ANALYZE ###\n")
	sb.WriteString(chunk.ToEmbeddableText())
	
	if len(contextUnits) > 0 {
		sb.WriteString("\n### ARCHITECTURAL CONTEXT (Dependencies) ###\n")
		sb.WriteString("The target unit interacts with or depends on these components:\n")
		for _, ctx := range contextUnits {
			sb.WriteString("- " + ctx.ToEmbeddableText())
		}
	}
	
	sb.WriteString("\n### INSTRUCTIONS ###\n")
	sb.WriteString("1. **Deep Interpretation**: Don't just repeat the signature. Explain *why* this component exists and its architectural significance.\n")
	sb.WriteString("2. **Logical Flow**: Briefly describe how it processes data or interacts with its dependencies.\n")
	sb.WriteString("3. **Insight**: If applicable, mention any design patterns used or potential impact on the system.\n")
	sb.WriteString("4. **Format**: Write in 2-3 clear, professional paragraphs. Use precise technical language.")

	resp, err := model.GenerateContent(ctx, genai.Text(sb.String()))
	if err != nil {
		return "", fmt.Errorf("failed to generate summary: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "No analysis generated.", nil
	}

	return fmt.Sprintf("%v", resp.Candidates[0].Content.Parts[0]), nil
}

// MemoryIndex is a simple in-memory vector storage.
type MemoryIndex struct {
	items []VectorItem
}

func NewMemoryIndex() *MemoryIndex {
	return &MemoryIndex{items: []VectorItem{}}
}

func (m *MemoryIndex) Add(ctx context.Context, items []VectorItem) error {
	m.items = append(m.items, items...)
	return nil
}

func (m *MemoryIndex) Search(ctx context.Context, queryVector []float32, topK int) ([]VectorItem, error) {
	// Simple cosine similarity search could be implemented here.
	// For now, it's a placeholder for future RAG logic.
	return nil, nil
}
