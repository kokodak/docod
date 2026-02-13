package knowledge

import (
	"context"
	"docod/internal/extractor"
	"docod/internal/graph"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockEmbedder struct {
	dim int
}

func (m *mockEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i := range texts {
		results[i] = make([]float32, m.dim) // zeros
	}
	return results, nil
}

func (m *mockEmbedder) Dimension() int { return m.dim }

func TestEngine_IndexAll(t *testing.T) {
	g := graph.NewGraph()
	unit := &extractor.CodeUnit{
		ID:       "test",
		Name:     "TestFunc",
		UnitType: "function",
		Filepath: "pkg/test_file.go",
	}
	g.AddUnit(unit)
	g.LinkRelations()

	embedder := &mockEmbedder{dim: 768}
	index := NewMemoryIndex(g)
	engine := NewEngine(g, embedder, index)

	err := engine.IndexAll(context.Background())
	require.NoError(t, err)

	assert.Len(t, index.items, 1)
	// PrepareSearchChunks aggregates by file, so the chunk name is the filename
	assert.Equal(t, "test_file.go", index.items[0].Chunk.Name)
	assert.Contains(t, index.items[0].Chunk.Description, "TestFunc")
	assert.Len(t, index.items[0].Embedding, 768)
}

func TestEngine_CreateChunk(t *testing.T) {
	g := graph.NewGraph()

	// Setup a mini-graph
	unitA := &extractor.CodeUnit{
		ID:          "file1:ProcessOrder:1",
		Name:        "ProcessOrder",
		UnitType:    "function",
		Package:     "logic",
		Description: "ProcessOrder handles the incoming order requests.",
		Content:     "func ProcessOrder(o Order) error { return nil }",
		Relations: []extractor.Relation{
			{Target: "Order", Kind: "uses_type"},
		},
	}

	unitB := &extractor.CodeUnit{
		ID:       "file2:Order:1",
		Name:     "Order",
		UnitType: "struct",
		Package:  "models",
		Content:  "type Order struct { ID string }",
	}

	g.AddUnit(unitA)
	g.AddUnit(unitB)
	g.LinkRelations()

	engine := NewEngine(g, nil, nil)

	t.Run("Structured chunk for function", func(t *testing.T) {
		chunk := engine.CreateChunk(unitA.ID, g.Nodes[unitA.ID])

		assert.Equal(t, "logic", chunk.Package)
		assert.Equal(t, "ProcessOrder", chunk.Name)
		assert.Contains(t, chunk.Dependencies, "Order")

		text := chunk.ToEmbeddableText()
		assert.Contains(t, text, "Symbol: ProcessOrder (function)")
		assert.Contains(t, text, "Depends on: Order")
	})

	t.Run("Structured chunk for struct", func(t *testing.T) {
		chunk := engine.CreateChunk(unitB.ID, g.Nodes[unitB.ID])

		assert.Equal(t, "struct", chunk.UnitType)
		assert.Contains(t, chunk.UsedBy, "ProcessOrder")
	})
}

func TestEngine_IndexIncrementalWithOptions_BudgetLimit(t *testing.T) {
	g := graph.NewGraph()
	g.AddUnit(&extractor.CodeUnit{
		ID:       "id1",
		Name:     "Alpha",
		UnitType: "function",
		Filepath: "pkg/a.go",
	})
	g.AddUnit(&extractor.CodeUnit{
		ID:       "id2",
		Name:     "Beta",
		UnitType: "function",
		Filepath: "pkg/b.go",
	})
	g.LinkRelations()

	embedder := &mockEmbedder{dim: 8}
	index := NewMemoryIndex(g)
	engine := NewEngine(g, embedder, index)

	err := engine.IndexIncrementalWithOptions(
		context.Background(),
		[]string{"pkg/a.go", "pkg/b.go"},
		nil,
		IndexingOptions{MaxChunksPerRun: 1},
	)
	require.NoError(t, err)
	assert.Len(t, index.items, 1)
}
