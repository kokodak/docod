package knowledge

import (
	"context"
	"docod/internal/extractor"
	"docod/internal/graph"
	"fmt"
	"path/filepath"
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

// SearchRelated finds semantically similar code units for a given chunk to provide better context.
func (e *Engine) SearchRelated(ctx context.Context, chunk SearchChunk, topK int) ([]SearchChunk, error) {
	return e.SearchByText(ctx, chunk.ToEmbeddableText(), topK+1, chunk.ID)
}

// SearchByText finds code units semantically similar to the provided query text.
func (e *Engine) SearchByText(ctx context.Context, query string, topK int, excludeID string) ([]SearchChunk, error) {
	if e.embedder == nil || e.index == nil {
		return nil, nil
	}

	// 1. Get embedding for the query text
	vectors, err := e.embedder.Embed(ctx, []string{query})
	if err != nil || len(vectors) == 0 {
		return nil, err
	}

	// 2. Search index
	items, err := e.index.Search(ctx, vectors[0], topK)
	if err != nil {
		return nil, err
	}

	var results []SearchChunk
	for _, item := range items {
		if item.Chunk.ID == excludeID {
			continue // Skip exclusion target (usually itself)
		}
		results = append(results, item.Chunk)
	}
	return results, nil
}

// PrepareSearchChunks converts nodes into optimized chunks, aggregating by file context.
func (e *Engine) PrepareSearchChunks() []SearchChunk {
	var chunks []SearchChunk
	
	// Group nodes by file path to create "File Chunks" or "Logical Component Chunks"
	// This reduces the number of chunks and provides better local context for embedding.
	files := make(map[string][]*graph.Node)

	for _, node := range e.graph.Nodes {
		// Filter unexported symbols
		if !isExported(node.Unit.Name) {
			continue
		}
		// Skip trivial types if needed
		if node.Unit.UnitType == "variable" || node.Unit.UnitType == "constant" {
			// Constants are better summarized at package level, but let's keep them if they are exported
		}
		
		files[node.Unit.Filepath] = append(files[node.Unit.Filepath], node)
	}

	for path, nodes := range files {
		if len(nodes) == 0 {
			continue
		}
		
		// Create a consolidated chunk for the file
		// Use the first node's package as the chunk's package
		pkgName := nodes[0].Unit.Package
		fileName := filepath.Base(path)
		
		chunk := SearchChunk{
			ID:       path, // Use filepath as ID for the aggregated chunk
			Name:     fileName,
			UnitType: "file_module",
			Package:  pkgName,
		}

		var descBuilder, sigBuilder strings.Builder
		depsSet := make(map[string]bool)
		usedBySet := make(map[string]bool)

		fmt.Fprintf(&descBuilder, "Module `%s` in package `%s` containing:\n", fileName, pkgName)

		for _, node := range nodes {
			// Aggregate description and signature
			fmt.Fprintf(&descBuilder, "- **%s** (%s): %s\n", node.Unit.Name, node.Unit.UnitType, node.Unit.Description)
			
			// For signature, we might want to be selective to avoid token overflow
			// Only add signatures for Structs and Interfaces
			if node.Unit.UnitType == "struct" || node.Unit.UnitType == "interface" {
				fmt.Fprintf(&sigBuilder, "%s\n\n", getFileName(node.Unit.Filepath))
			}

			// Aggregate dependencies
			for _, d := range e.graph.GetDependencies(node.Unit.ID) {
				depsSet[d.Unit.Name] = true
			}
			for _, d := range e.graph.GetDependents(node.Unit.ID) {
				usedBySet[d.Unit.Name] = true
			}
		}

		chunk.Description = descBuilder.String()
		chunk.Signature = sigBuilder.String()
		
		for dep := range depsSet {
			chunk.Dependencies = append(chunk.Dependencies, dep)
		}
		for user := range usedBySet {
			chunk.UsedBy = append(chunk.UsedBy, user)
		}

		chunks = append(chunks, chunk)
	}

	fmt.Printf("📦 Optimized Chunks: Reduced to %d File-based Contexts\n", len(chunks))
	return chunks
}

func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}

// GetNodeByID retrieves a single graph node for a given ID.
func (e *Engine) GetNodeByID(id string) (*graph.Node, bool) {
	node, ok := e.graph.Nodes[id]
	return node, ok
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

func getFileName(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return path
}
