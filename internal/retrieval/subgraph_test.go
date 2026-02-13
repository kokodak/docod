package retrieval

import (
	"testing"

	"docod/internal/git"
	"docod/internal/graph"

	"github.com/stretchr/testify/assert"
)

func TestExtractFromChanges_BasicHopTraversal(t *testing.T) {
	g := graph.NewGraph()
	g.AddSymbol(&graph.Symbol{ID: "A", Filepath: "a.go", StartLine: 10, EndLine: 40, Name: "A"})
	g.AddSymbol(&graph.Symbol{ID: "B", Filepath: "b.go", StartLine: 1, EndLine: 20, Name: "B"})
	g.AddSymbol(&graph.Symbol{ID: "C", Filepath: "c.go", StartLine: 1, EndLine: 20, Name: "C"})
	g.Edges = []graph.Edge{
		{From: "A", To: "B", Kind: graph.RelationCalls, Confidence: 0.9},
		{From: "B", To: "C", Kind: graph.RelationCalls, Confidence: 0.9},
	}

	changes := []git.ChangedFile{{Path: "a.go", ChangedLines: []int{20}}}
	sg := ExtractFromChanges(g, changes, Config{MaxHops: 1})

	assert.Equal(t, []string{"A"}, sg.SeedIDs)
	assert.Equal(t, []string{"A", "B"}, sg.NodeIDs)
	assert.Len(t, sg.Edges, 1)
	assert.Equal(t, "A", sg.Edges[0].From)
	assert.Equal(t, "B", sg.Edges[0].To)
	assert.InDelta(t, 1.0, sg.NodeScores["A"], 0.001)
	assert.InDelta(t, 0.9, sg.NodeScores["B"], 0.001)
}

func TestExtractFromChanges_FiltersByConfidence(t *testing.T) {
	g := graph.NewGraph()
	g.AddSymbol(&graph.Symbol{ID: "A", Filepath: "a.go", StartLine: 1, EndLine: 10, Name: "A"})
	g.AddSymbol(&graph.Symbol{ID: "B", Filepath: "b.go", StartLine: 1, EndLine: 10, Name: "B"})
	g.Edges = []graph.Edge{
		{From: "A", To: "B", Kind: graph.RelationCalls, Confidence: 0.3},
	}

	changes := []git.ChangedFile{{Path: "a.go", ChangedLines: []int{2}}}
	sg := ExtractFromChanges(g, changes, Config{MaxHops: 2, MinConfidence: 0.7})

	assert.Equal(t, []string{"A"}, sg.SeedIDs)
	assert.Equal(t, []string{"A"}, sg.NodeIDs)
	assert.Len(t, sg.Edges, 0)
	assert.InDelta(t, 1.0, sg.NodeScores["A"], 0.001)
}

func TestExtractFromChanges_FiltersByRelationKind(t *testing.T) {
	g := graph.NewGraph()
	g.AddSymbol(&graph.Symbol{ID: "A", Filepath: "a.go", StartLine: 1, EndLine: 10, Name: "A"})
	g.AddSymbol(&graph.Symbol{ID: "B", Filepath: "b.go", StartLine: 1, EndLine: 10, Name: "B"})
	g.AddSymbol(&graph.Symbol{ID: "C", Filepath: "c.go", StartLine: 1, EndLine: 10, Name: "C"})
	g.Edges = []graph.Edge{
		{From: "A", To: "B", Kind: graph.RelationCalls, Confidence: 0.9},
		{From: "A", To: "C", Kind: graph.RelationUsesType, Confidence: 0.9},
	}

	changes := []git.ChangedFile{{Path: "a.go", ChangedLines: []int{2}}}
	sg := ExtractFromChanges(g, changes, Config{
		MaxHops: 2,
		AllowedKinds: map[graph.RelationKind]bool{
			graph.RelationUsesType: true,
		},
	})

	assert.Equal(t, []string{"A", "C"}, sg.NodeIDs)
	assert.Len(t, sg.Edges, 1)
	assert.Equal(t, graph.RelationUsesType, sg.Edges[0].Kind)
}
