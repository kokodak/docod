package knowledge

import (
	"context"
	"docod/internal/extractor"
	"docod/internal/graph"
	"fmt"
	"strings"
)

// SearchChunk represents a structured piece of code knowledge, ready for indexing or embedding.
type SearchChunk struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	UnitType     string   `json:"unit_type"`
	Package      string   `json:"package"`
	Description  string   `json:"description"`
	Signature    string   `json:"signature"`
	Dependencies []string `json:"dependencies"`
	UsedBy       []string `json:"used_by"`
}

// ToEmbeddableText converts the structured chunk into a single string optimized for embedding models.
func (c SearchChunk) ToEmbeddableText() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Symbol: %s (%s) in package %s\n", c.Name, c.UnitType, c.Package)
	if c.Description != "" {
		fmt.Fprintf(&sb, "Context: %s\n", c.Description)
	}
	fmt.Fprintf(&sb, "Definition: %s\n", c.Signature)
	if len(c.Dependencies) > 0 {
		fmt.Fprintf(&sb, "Depends on: %s\n", strings.Join(c.Dependencies, ", "))
	}
	if len(c.UsedBy) > 0 {
		fmt.Fprintf(&sb, "Used by: %s\n", strings.Join(c.UsedBy, ", "))
	}
	return sb.String()
}

// Engine handles data refinement and preparation for LLM/Embedding.
type Engine struct {
	graph    *graph.Graph
	embedder Embedder
	index    Indexer
}

// NewEngine creates a new knowledge engine with optional embedder and indexer.
func NewEngine(g *graph.Graph, em Embedder, idx Indexer) *Engine {
	return &Engine{
		graph:    g,
		embedder: em,
		index:    idx,
	}
}

// IndexAll processes all graph nodes, converts them to embeddings, and adds them to the index.
func (e *Engine) IndexAll(ctx context.Context) error {
	if e.embedder == nil || e.index == nil {
		return fmt.Errorf("embedder or indexer not initialized")
	}

	chunks := e.PrepareSearchChunks()
	var texts []string
	for _, c := range chunks {
		texts = append(texts, c.ToEmbeddableText())
	}

	vectors, err := e.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("failed to generate embeddings: %w", err)
	}

	var items []VectorItem
	for i, chunk := range chunks {
		items = append(items, VectorItem{
			Chunk:     chunk,
			Embedding: vectors[i],
		})
	}

	return e.index.Add(ctx, items)
}

// PrepareSearchChunks converts all nodes in the graph into structured search chunks.
func (e *Engine) PrepareSearchChunks() []SearchChunk {
	var chunks []SearchChunk
	for id, node := range e.graph.Nodes {
		chunks = append(chunks, e.CreateChunk(id, node))
	}
	return chunks
}

// GetChunkByID retrieves a single structured chunk for a given ID.
func (e *Engine) GetChunkByID(id string) (SearchChunk, bool) {
	node, ok := e.graph.Nodes[id]
	if !ok {
		return SearchChunk{}, false
	}
	return e.CreateChunk(id, node), true
}

// CreateChunk builds a structured SearchChunk from a graph node.
func (e *Engine) CreateChunk(id string, node *graph.Node) SearchChunk {
	u := node.Unit
	chunk := SearchChunk{
		ID:          id,
		Name:        u.Name,
		UnitType:    u.UnitType,
		Package:     u.Package,
		Description: u.Description,
		Signature:   e.getConciseSignature(u),
	}

	for _, d := range e.graph.GetDependencies(id) {
		chunk.Dependencies = append(chunk.Dependencies, d.Unit.Name)
	}

	for _, d := range e.graph.GetDependents(id) {
		chunk.UsedBy = append(chunk.UsedBy, d.Unit.Name)
	}

	return chunk
}

func (e *Engine) getConciseSignature(u *extractor.CodeUnit) string {
	lines := strings.Split(u.Content, "\n")
	if len(lines) > 0 {
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "/*") {
				return trimmed
			}
		}
	}
	return u.Name
}
