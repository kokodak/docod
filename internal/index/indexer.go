package index

import (
	"docod/internal/crawler"
	"docod/internal/extractor"
	"docod/internal/graph"
	"encoding/json"
	"fmt"
	"os"
)

// Indexer orchestrates codebase indexing and graph management.
type Indexer struct {
	crawler *crawler.Crawler
}

// NewIndexer creates a new indexer.
func NewIndexer(c *crawler.Crawler) *Indexer {
	return &Indexer{
		crawler: c,
	}
}

// BuildGraph scans the project root and constructs a dependency graph.
func (i *Indexer) BuildGraph(root string) (*graph.Graph, error) {
	g := graph.NewGraph()

	err := i.crawler.ScanProject(root, func(unit *extractor.CodeUnit) {
		g.AddUnit(unit)
	})
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	// Resolve relationships after all units are loaded
	g.LinkRelations()

	return g, nil
}

// SaveGraph persists the graph to a JSON file.
func (i *Indexer) SaveGraph(g *graph.Graph, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create graph file: %w", err)
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(g); err != nil {
		return fmt.Errorf("failed to encode graph: %w", err)
	}
	return nil
}

// LoadGraph loads a graph from a JSON file.
func (i *Indexer) LoadGraph(path string) (*graph.Graph, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open graph file: %w", err)
	}
	defer f.Close()

	g := graph.NewGraph()
	decoder := json.NewDecoder(f)
	if err := decoder.Decode(g); err != nil {
		return nil, fmt.Errorf("failed to decode graph: %w", err)
	}

	// Important: Rebuild internal indices that aren't serialized
	g.RebuildIndices()

	return g, nil
}
