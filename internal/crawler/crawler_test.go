package crawler

import (
	"docod/internal/extractor"
	"docod/internal/graph"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCrawler_ScanSelf(t *testing.T) {
	// Initialize components
	ext, err := extractor.NewExtractor("go")
	require.NoError(t, err)

	c := NewCrawler(ext)
	g := graph.NewGraph()

	// Find project root (assumed to be 2 levels up from internal/crawler)
	root, _ := filepath.Abs("../../")

	// Scan the project and build the graph
	err = c.ScanProject(root, func(unit *extractor.CodeUnit) {
		g.AddUnit(unit)
	})
	require.NoError(t, err)

	// Link all relations
	g.LinkRelations()

	// Verify if we captured core components
	t.Run("Extract core units", func(t *testing.T) {
		assert.Greater(t, len(g.Nodes), 10, "Should find at least 10 nodes in this project")
	})

	t.Run("Graph integrity", func(t *testing.T) {
		// Example: Crawler should depend on Extractor
		var foundCrawlerNode *graph.Node
		for _, node := range g.Nodes {
			if node.Unit.Name == "Crawler" {
				foundCrawlerNode = node
				break
			}
		}
		require.NotNil(t, foundCrawlerNode)
		
		deps := g.GetDependencies(foundCrawlerNode.Unit.ID)
		assert.NotEmpty(t, deps)
		
		var foundExtractorDep bool
		for _, d := range deps {
			if d.Unit.Name == "Extractor" {
				foundExtractorDep = true
			}
		}
		assert.True(t, foundExtractorDep, "Crawler should depend on Extractor")
	})
}
