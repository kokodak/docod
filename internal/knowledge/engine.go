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
	ID           string        `json:"id"`
	FilePath     string        `json:"file_path,omitempty"`
	Name         string        `json:"name"`
	UnitType     string        `json:"unit_type"`
	Package      string        `json:"package"`
	Description  string        `json:"description"`
	Signature    string        `json:"signature"`
	Content      string        `json:"content"`      // Actual code body for LLM analysis
	ContentHash  string        `json:"content_hash"` // Hash for change detection
	Dependencies []string      `json:"dependencies"`
	UsedBy       []string      `json:"used_by"`
	Sources      []ChunkSource `json:"sources,omitempty"`
}

type ChunkSource struct {
	SymbolID   string  `json:"symbol_id"`
	FilePath   string  `json:"file_path"`
	StartLine  int     `json:"start_line"`
	EndLine    int     `json:"end_line"`
	Relation   string  `json:"relation"` // primary, dependency, context
	Confidence float64 `json:"confidence,omitempty"`
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
	graph         *graph.Graph
	embedder      Embedder
	index         Indexer
	queryVecCache map[string][]float32
}

type IndexingOptions struct {
	MaxChunksPerRun int
}

// NewEngine creates a new knowledge engine with optional embedder and indexer.
func NewEngine(g *graph.Graph, em Embedder, idx Indexer) *Engine {
	return &Engine{
		graph:         g,
		embedder:      em,
		index:         idx,
		queryVecCache: make(map[string][]float32),
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
	return e.IndexAllWithOptions(ctx, IndexingOptions{})
}

// IndexAllWithOptions processes all graph nodes with runtime budget controls.
func (e *Engine) IndexAllWithOptions(ctx context.Context, opts IndexingOptions) error {
	if e.embedder == nil || e.index == nil {
		return fmt.Errorf("embedder or indexer not initialized")
	}

	chunks := e.PrepareSearchChunks()
	chunks = limitChunksByBudget(chunks, opts.MaxChunksPerRun)
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
		// Remove existing chunks for updated files first to avoid stale symbol IDs.
		if err := e.index.Delete(ctx, updatedFiles); err != nil {
			return fmt.Errorf("failed to delete stale chunks for updated files: %w", err)
		}

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

	var symbols []SearchChunk
	var files []SearchChunk
	for _, c := range chunks {
		if c.UnitType == "file_module" {
			files = append(files, c)
			continue
		}
		symbols = append(symbols, c)
	}

	sortChunksByPriority(symbols)
	sortChunksByPriority(files)

	targetFiles := 0
	switch {
	case max >= 8:
		targetFiles = max / 4
	case max >= 4:
		targetFiles = 1
	default:
		targetFiles = 0
	}
	targetSymbols := max - targetFiles

	out := make([]SearchChunk, 0, max)
	out = append(out, symbols[:minInt(len(symbols), targetSymbols)]...)
	out = append(out, files[:minInt(len(files), targetFiles)]...)

	// Backfill from whichever pool still has capacity.
	if len(out) < max && len(symbols) > len(out) {
		for _, c := range symbols {
			if len(out) >= max {
				break
			}
			if containsChunkID(out, c.ID) {
				continue
			}
			out = append(out, c)
		}
	}
	if len(out) < max {
		for _, c := range files {
			if len(out) >= max {
				break
			}
			if containsChunkID(out, c.ID) {
				continue
			}
			out = append(out, c)
		}
	}
	return out
}

func (e *Engine) embedChunks(ctx context.Context, chunks []SearchChunk) error {
	chunks = e.filterChunksForEmbedding(ctx, chunks)
	if len(chunks) == 0 {
		return nil
	}

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

	queryKey := strings.TrimSpace(query)
	var queryVec []float32
	if cached, ok := e.queryVecCache[queryKey]; ok && len(cached) > 0 {
		queryVec = cached
	} else {
		// 1. Get embedding for the query text
		vectors, err := e.embedder.Embed(ctx, []string{query})
		if err != nil || len(vectors) == 0 {
			return nil, err
		}
		queryVec = vectors[0]
		if queryKey != "" {
			e.queryVecCache[queryKey] = queryVec
		}
	}

	// 2. Search index
	items, err := e.index.Search(ctx, queryVec, topK)
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

func (e *Engine) filterChunksForEmbedding(ctx context.Context, chunks []SearchChunk) []SearchChunk {
	if len(chunks) == 0 {
		return nil
	}

	hashReader, ok := e.index.(IndexContentHashReader)
	if !ok {
		return chunks
	}

	ids := make([]string, 0, len(chunks))
	for _, c := range chunks {
		if strings.TrimSpace(c.ID) == "" {
			continue
		}
		ids = append(ids, c.ID)
	}
	if len(ids) == 0 {
		return chunks
	}

	existing, err := hashReader.GetContentHashes(ctx, ids)
	if err != nil {
		return chunks
	}
	if len(existing) == 0 {
		return chunks
	}

	out := make([]SearchChunk, 0, len(chunks))
	for _, c := range chunks {
		id := strings.TrimSpace(c.ID)
		if id == "" {
			continue
		}
		if oldHash, ok := existing[id]; ok && oldHash != "" && c.ContentHash != "" && oldHash == c.ContentHash {
			continue
		}
		out = append(out, c)
	}
	return out
}

// PrepareSearchChunks converts graph nodes into hybrid chunks.
// Symbol chunks are primary, file-level chunks are secondary context.
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

// PrepareChunksForFiles generates hybrid search chunks for specific files.
func (e *Engine) PrepareChunksForFiles(filepaths []string) []SearchChunk {
	var chunks []SearchChunk
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

	// 1) Symbol-first chunks
	for _, nodes := range fileNodes {
		for _, node := range nodes {
			if node == nil || node.Unit == nil {
				continue
			}
			symbolChunks := e.createSymbolChunksForNode(node)
			chunks = append(chunks, symbolChunks...)
		}
	}

	// 2) File-level context chunks (secondary)
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
			FilePath:    path,
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
			source := ChunkSource{
				SymbolID:   node.Unit.ID,
				FilePath:   node.Unit.Filepath,
				StartLine:  node.Unit.StartLine,
				EndLine:    node.Unit.EndLine,
				Relation:   "primary",
				Confidence: 0.9,
			}
			chunk.Sources = append(chunk.Sources, source)

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
	sortChunksByPriority(chunks)
	fmt.Printf("📦 Prepared %d Chunks (symbol-first) from %d files\n", len(chunks), len(filepaths))
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
		FilePath:    u.Filepath,
		Name:        u.Name,
		UnitType:    u.UnitType,
		Package:     u.Package,
		Description: u.Description,
		Signature:   e.getConciseSignature(u),
		Content:     u.Content,
		ContentHash: u.ContentHash,
		Sources: []ChunkSource{
			{
				SymbolID:   u.ID,
				FilePath:   u.Filepath,
				StartLine:  u.StartLine,
				EndLine:    u.EndLine,
				Relation:   "primary",
				Confidence: 0.9,
			},
		},
	}

	for _, d := range e.graph.GetDependencies(id) {
		chunk.Dependencies = append(chunk.Dependencies, d.Unit.Name)
	}

	for _, d := range e.graph.GetDependents(id) {
		chunk.UsedBy = append(chunk.UsedBy, d.Unit.Name)
	}

	return chunk
}

func (e *Engine) createSymbolChunksForNode(node *graph.Node) []SearchChunk {
	base := e.CreateChunk(node.Unit.ID, node)
	base.Content = truncateChunkContent(base.Content, 1200)
	if !shouldSegmentChunk(base) {
		return []SearchChunk{base}
	}

	const (
		segmentLines   = 40
		segmentOverlap = 8
		maxSegments    = 3
	)
	lines := strings.Split(base.Content, "\n")
	step := segmentLines - segmentOverlap
	if step <= 0 {
		step = segmentLines
	}

	segments := make([]SearchChunk, 0, maxSegments+1)
	segments = append(segments, base)

	for idx, start := 0, 0; start < len(lines) && idx < maxSegments; idx, start = idx+1, start+step {
		end := start + segmentLines
		if end > len(lines) {
			end = len(lines)
		}
		if end <= start {
			break
		}
		block := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
		if block == "" {
			continue
		}
		seg := base
		seg.ID = fmt.Sprintf("%s::seg:%d", base.ID, idx+1)
		seg.UnitType = "symbol_segment"
		seg.Description = fmt.Sprintf("%s [segment %d]", strings.TrimSpace(base.Description), idx+1)
		seg.Content = block
		seg.ContentHash = fmt.Sprintf("%s::seg:%d", base.ContentHash, idx+1)
		seg.Sources = segmentSources(base.Sources, start, end)
		segments = append(segments, seg)
	}
	return segments
}

func shouldSegmentChunk(c SearchChunk) bool {
	switch c.UnitType {
	case "function", "method":
		return lineCount(c.Content) > 45
	default:
		return false
	}
}

func lineCount(s string) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

func segmentSources(src []ChunkSource, segStartOffset int, segEndOffset int) []ChunkSource {
	if len(src) == 0 {
		return nil
	}
	out := make([]ChunkSource, 0, len(src))
	for _, s := range src {
		start := s.StartLine + segStartOffset
		end := s.StartLine + segEndOffset - 1
		if start <= 0 {
			start = s.StartLine
		}
		if end < start {
			end = start
		}
		copy := s
		copy.StartLine = start
		copy.EndLine = end
		copy.Relation = "context"
		out = append(out, copy)
	}
	return out
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

func truncateChunkContent(content string, max int) string {
	if max <= 0 || len(content) <= max {
		return content
	}
	return content[:max] + "\n... (truncated)"
}

func containsChunkID(chunks []SearchChunk, id string) bool {
	for _, c := range chunks {
		if c.ID == id {
			return true
		}
	}
	return false
}

func sortChunksByPriority(chunks []SearchChunk) {
	sort.Slice(chunks, func(i, j int) bool {
		pi := chunkPriority(chunks[i])
		pj := chunkPriority(chunks[j])
		if pi == pj {
			if chunks[i].ID == chunks[j].ID {
				return chunks[i].ContentHash < chunks[j].ContentHash
			}
			return chunks[i].ID < chunks[j].ID
		}
		return pi > pj
	})
}

func chunkPriority(c SearchChunk) int {
	score := 0
	if c.UnitType == "file_module" {
		score += 5
	} else {
		score += 40
	}
	if isExported(c.Name) {
		score += 20
	}
	switch c.UnitType {
	case "function", "method", "struct", "interface":
		score += 12
	case "constant", "variable":
		score += 4
	}
	score += minInt(len(c.Dependencies), 8)
	score += minInt(len(c.UsedBy), 8)
	return score
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
