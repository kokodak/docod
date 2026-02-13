package knowledge

import (
	"context"
	"docod/internal/graph"
	"fmt"
	"path/filepath"
	"sort"
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
	Content      string   `json:"content"`      // Actual code body for LLM analysis
	ContentHash  string   `json:"content_hash"` // Hash for change detection
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

type IndexingOptions struct {
	MaxChunksPerRun int
}

// NewEngine creates a new knowledge engine with optional embedder and indexer.
func NewEngine(g *graph.Graph, em Embedder, idx Indexer) *Engine {
	return &Engine{
		graph:    g,
		embedder: em,
		index:    idx,
	}
}

func (e *Engine) Embedder() Embedder {
	return e.embedder
}

func (e *Engine) Indexer() Indexer {
	return e.index
}

// IndexAll processes all graph nodes, converts them to embeddings, and adds them to the index.
func (e *Engine) IndexAll(ctx context.Context) error {
	if e.embedder == nil || e.index == nil {
		return fmt.Errorf("embedder or indexer not initialized")
	}

	chunks := e.PrepareSearchChunks()
	return e.embedChunks(ctx, chunks)
}

// IndexIncremental updates embeddings only for the specified files and removes deleted ones.
func (e *Engine) IndexIncremental(ctx context.Context, updatedFiles []string, deletedFiles []string) error {
	return e.IndexIncrementalWithOptions(ctx, updatedFiles, deletedFiles, IndexingOptions{})
}

// IndexIncrementalWithOptions updates embeddings incrementally with runtime budget controls.
func (e *Engine) IndexIncrementalWithOptions(ctx context.Context, updatedFiles []string, deletedFiles []string, opts IndexingOptions) error {
	if e.embedder == nil || e.index == nil {
		return fmt.Errorf("embedder or indexer not initialized")
	}

	// 1. Remove chunks for deleted files
	// Note: Chunk ID currently equals the filepath for file-level chunks
	if len(deletedFiles) > 0 {
		if err := e.index.Delete(ctx, deletedFiles); err != nil {
			return fmt.Errorf("failed to delete stale chunks: %w", err)
		}
	}

	// 2. Process updated files
	if len(updatedFiles) > 0 {
		chunks := e.PrepareChunksForFiles(updatedFiles)
		chunks = limitChunksByBudget(chunks, opts.MaxChunksPerRun)
		if len(chunks) > 0 {
			if err := e.embedChunks(ctx, chunks); err != nil {
				return fmt.Errorf("failed to embed updated chunks: %w", err)
			}
		}
	}

	return nil
}

func limitChunksByBudget(chunks []SearchChunk, max int) []SearchChunk {
	if max <= 0 || len(chunks) <= max {
		return chunks
	}
	ordered := append([]SearchChunk(nil), chunks...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].ID == ordered[j].ID {
			return ordered[i].ContentHash < ordered[j].ContentHash
		}
		return ordered[i].ID < ordered[j].ID
	})
	return ordered[:max]
}

func (e *Engine) embedChunks(ctx context.Context, chunks []SearchChunk) error {
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
	// Collect all filepaths from the graph
	uniqueFiles := make(map[string]bool)
	for _, node := range e.graph.Nodes {
		uniqueFiles[node.Unit.Filepath] = true
	}

	var files []string
	for f := range uniqueFiles {
		files = append(files, f)
	}

	return e.PrepareChunksForFiles(files)
}

// PrepareChunksForFiles generates search chunks for specific files.
func (e *Engine) PrepareChunksForFiles(filepaths []string) []SearchChunk {
	var chunks []SearchChunk

	// Group nodes by file path to create "File Chunks" or "Logical Component Chunks"
	// Optimization: Only scan nodes that belong to requested filepaths
	// Since graph.Nodes is a map by ID, we need to iterate all or use an index.
	// For now, iterating all is acceptable but inefficient for large graphs.
	// TODO: Use FindNodesByFile from store or build an index in memory if repeated often.
	// Current Graph struct doesn't have a file index in memory, but we can build a temp map.

	targetFiles := make(map[string]bool)
	for _, f := range filepaths {
		targetFiles[f] = true
	}

	fileNodes := make(map[string][]*graph.Node)

	for id, node := range e.graph.Nodes {
		if targetFiles[node.Unit.Filepath] {
			if !e.isDocRelevantNode(id, node) {
				continue
			}
			fileNodes[node.Unit.Filepath] = append(fileNodes[node.Unit.Filepath], node)
		}
	}

	for path, nodes := range fileNodes {
		if len(nodes) == 0 {
			continue
		}

		pkgName := nodes[0].Unit.Package
		fileName := filepath.Base(path)

		// Combined ContentHash for the file chunk
		var combinedHashBuilder strings.Builder
		for _, node := range nodes {
			combinedHashBuilder.WriteString(node.Unit.ContentHash)
		}

		chunk := SearchChunk{
			ID:          path,
			Name:        fileName,
			UnitType:    "file_module",
			Package:     pkgName,
			ContentHash: combinedHashBuilder.String(),
		}

		var descBuilder, sigBuilder strings.Builder
		var contentBuilder strings.Builder // To aggregate full code content

		depsSet := make(map[string]bool)
		usedBySet := make(map[string]bool)

		fmt.Fprintf(&descBuilder, "Module `%s` in package `%s` containing:\n", fileName, pkgName)

		for _, node := range nodes {
			// Aggregate description
			fmt.Fprintf(&descBuilder, "- **%s** (%s): %s\n", node.Unit.Name, node.Unit.UnitType, node.Unit.Description)

			// Aggregate Content (Actual Code)
			// Only include actual code for Structs, Interfaces, and Functions
			if node.Unit.UnitType == "struct" || node.Unit.UnitType == "interface" || node.Unit.UnitType == "function" || node.Unit.UnitType == "method" {
				fmt.Fprintf(&contentBuilder, "// %s %s\n%s\n\n", node.Unit.UnitType, node.Unit.Name, node.Unit.Content)
			}

			// Aggregate Signature
			if node.Unit.UnitType == "struct" || node.Unit.UnitType == "interface" {
				fmt.Fprintf(&sigBuilder, "%s\n\n", e.getConciseSignature(node.Unit))
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

		// Truncate content to avoid excessive tokens (e.g., 3000 chars)
		rawContent := contentBuilder.String()
		if len(rawContent) > 3000 {
			chunk.Content = rawContent[:3000] + "\n... (truncated)"
		} else {
			chunk.Content = rawContent
		}

		for dep := range depsSet {
			chunk.Dependencies = append(chunk.Dependencies, dep)
		}
		for user := range usedBySet {
			chunk.UsedBy = append(chunk.UsedBy, user)
		}

		chunks = append(chunks, chunk)
	}

	fmt.Printf("📦 Prepared %d Chunks from %d files\n", len(chunks), len(filepaths))
	return chunks
}

func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}

// isDocRelevantNode keeps documentation scope focused while still capturing
// internal changes that are connected to public symbols.
func (e *Engine) isDocRelevantNode(id string, node *graph.Node) bool {
	if node == nil || node.Unit == nil {
		return false
	}
	if isExported(node.Unit.Name) {
		return true
	}
	return e.reachesExportedSymbol(id, 2)
}

func (e *Engine) reachesExportedSymbol(startID string, maxDepth int) bool {
	if maxDepth <= 0 || e.graph == nil {
		return false
	}
	type qItem struct {
		id    string
		depth int
	}
	queue := []qItem{{id: startID, depth: 0}}
	visited := map[string]bool{startID: true}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr.depth > 0 {
			if n, ok := e.graph.Nodes[curr.id]; ok && n != nil && n.Unit != nil && isExported(n.Unit.Name) {
				return true
			}
		}
		if curr.depth >= maxDepth {
			continue
		}

		for _, dep := range e.graph.GetDependencies(curr.id) {
			if dep == nil || dep.Unit == nil {
				continue
			}
			nextID := dep.Unit.ID
			if visited[nextID] {
				continue
			}
			visited[nextID] = true
			queue = append(queue, qItem{id: nextID, depth: curr.depth + 1})
		}
		for _, dep := range e.graph.GetDependents(curr.id) {
			if dep == nil || dep.Unit == nil {
				continue
			}
			nextID := dep.Unit.ID
			if visited[nextID] {
				continue
			}
			visited[nextID] = true
			queue = append(queue, qItem{id: nextID, depth: curr.depth + 1})
		}
	}
	return false
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
		Content:     u.Content,
		ContentHash: u.ContentHash,
	}

	for _, d := range e.graph.GetDependencies(id) {
		chunk.Dependencies = append(chunk.Dependencies, d.Unit.Name)
	}

	for _, d := range e.graph.GetDependents(id) {
		chunk.UsedBy = append(chunk.UsedBy, d.Unit.Name)
	}

	return chunk
}

func (e *Engine) getConciseSignature(u *graph.Symbol) string {
	if u != nil && strings.TrimSpace(u.Metadata.Signature) != "" {
		return strings.TrimSpace(u.Metadata.Signature)
	}
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
