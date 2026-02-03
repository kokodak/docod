package knowledge

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"docod/internal/graph"

	"google.golang.org/genai"
)

// GeminiEmbedder implements Embedder using Google's Gemini API (google.golang.org/genai).
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

// embedBatchSize is the number of texts to send in a single API call to reduce rate limit hits.
const embedBatchSize = 50

// embedBatchDelay is the delay between batches to stay under 100 RPM.
const embedBatchDelay = 700 * time.Millisecond

// embedRetryDelay is how long to wait before retrying on 429.
const embedRetryDelay = 6 * time.Second

// embedMaxRetries is the max number of retries per batch on rate limit.
const embedMaxRetries = 5

func (g *GeminiEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	var results [][]float32

	var config *genai.EmbedContentConfig
	if g.dimension > 0 {
		dim := int32(g.dimension)
		config = &genai.EmbedContentConfig{OutputDimensionality: &dim}
	}

	// Process in batches to reduce API calls (e.g. 136 chunks → 3 requests instead of 136)
	for i := 0; i < len(texts); i += embedBatchSize {
		// Delay between batches to avoid hitting 100 RPM
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
			// Retry on 429 / RESOURCE_EXHAUSTED
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

func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *genai.APIError
	if errors.As(err, &apiErr) && apiErr.Code == 429 {
		return true
	}
	// Fallback: check error string for RESOURCE_EXHAUSTED / quota
	s := err.Error()
	return strings.Contains(s, "429") || strings.Contains(s, "RESOURCE_EXHAUSTED") || strings.Contains(s, "quota")
}

func (g *GeminiEmbedder) Dimension() int {
	return g.dimension
}

// GeminiSummarizer implements Summarizer using Google's Gemini Pro.
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
	return text, nil
}

// MemoryIndex is a simple in-memory vector storage with hash-based caching and graph awareness.
type MemoryIndex struct {
	items  []VectorItem
	hashes map[string]bool
	graph  *graph.Graph // Reference to the dependency graph for hybrid search
}

func NewMemoryIndex(g *graph.Graph) *MemoryIndex {
	return &MemoryIndex{
		items:  []VectorItem{},
		hashes: make(map[string]bool),
		graph:  g,
	}
}

func (m *MemoryIndex) Add(ctx context.Context, items []VectorItem) error {
	for _, item := range items {
		if !m.hashes[item.Chunk.ID] {
			m.items = append(m.items, item)
			m.hashes[item.Chunk.ID] = true
		}
	}
	return nil
}

// Save persists the index to a file.
func (m *MemoryIndex) Save(filepath string) error {
	f, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer f.Close()
	// Graph is reconstructed from source, so we only persist items
	return gob.NewEncoder(f).Encode(m.items)
}

// Load restores the index from a file.
func (m *MemoryIndex) Load(filepath string) error {
	f, err := os.Open(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	var loadedItems []VectorItem
	if err := gob.NewDecoder(f).Decode(&loadedItems); err != nil {
		return err
	}

	m.items = loadedItems
	m.hashes = make(map[string]bool)
	for _, item := range m.items {
		m.hashes[item.Chunk.ID] = true
	}
	return nil
}

// Search implements Indexer and performs hybrid search (vector + graph proximity).
func (m *MemoryIndex) Search(ctx context.Context, queryVector []float32, topK int) ([]VectorItem, error) {
	return m.searchWithSource(ctx, queryVector, topK, "")
}

// searchWithSource performs hybrid search; sourceID boosts graph-neighbor scores.
func (m *MemoryIndex) searchWithSource(_ context.Context, queryVector []float32, topK int, sourceID string) ([]VectorItem, error) {
	if len(m.items) == 0 {
		return nil, nil
	}

	type scoreItem struct {
		item  VectorItem
		score float32
	}
	scores := make([]scoreItem, 0, len(m.items))

	// Pre-calculate graph distances if sourceID is valid
	distances := make(map[string]int)
	if sourceID != "" && m.graph != nil {
		distances = m.bfsDistances(sourceID, 2) // Limit depth to 2 hops
	}

	for _, item := range m.items {
		// 1. Vector Similarity (0.0 ~ 1.0)
		vecScore := cosineSimilarity(queryVector, item.Embedding)

		// 2. Graph Proximity Boost
		// Direct neighbor (dist=1): +0.2
		// 2-hop neighbor (dist=2): +0.1
		graphBoost := float32(0.0)
		if dist, ok := distances[item.Chunk.ID]; ok {
			switch dist {
			case 1:
				graphBoost = 0.2
			case 2:
				graphBoost = 0.1
			}
		}

		finalScore := vecScore + graphBoost
		scores = append(scores, scoreItem{item: item, score: finalScore})
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	limit := topK
	if limit > len(scores) {
		limit = len(scores)
	}

	results := make([]VectorItem, 0, limit)
	for i := 0; i < limit; i++ {
		results = append(results, scores[i].item)
	}

	return results, nil
}

// bfsDistances calculates shortest path distances from startNode up to maxDepth.
func (m *MemoryIndex) bfsDistances(startID string, maxDepth int) map[string]int {
	dists := make(map[string]int)
	if m.graph == nil {
		return dists
	}

	// BFS queue: [NodeID, Depth]
	type queueItem struct {
		id    string
		depth int
	}
	queue := []queueItem{{id: startID, depth: 0}}
	visited := map[string]bool{startID: true}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr.depth > 0 {
			dists[curr.id] = curr.depth
		}

		if curr.depth >= maxDepth {
			continue
		}

		// Check Dependencies (Outgoing edges)
		for _, dep := range m.graph.GetDependencies(curr.id) {
			if !visited[dep.Unit.ID] {
				visited[dep.Unit.ID] = true
				queue = append(queue, queueItem{id: dep.Unit.ID, depth: curr.depth + 1})
			}
		}

		// Check Dependents (Incoming edges) - context flows both ways
		for _, dep := range m.graph.GetDependents(curr.id) {
			if !visited[dep.Unit.ID] {
				visited[dep.Unit.ID] = true
				queue = append(queue, queueItem{id: dep.Unit.ID, depth: curr.depth + 1})
			}
		}
	}
	return dists
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += float64(a[i] * b[i])
		normA += float64(a[i] * a[i])
		normB += float64(b[i] * b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dotProduct / (math.Sqrt(normA) * math.Sqrt(normB)))
}
