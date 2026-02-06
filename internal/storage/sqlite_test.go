package storage

import (
	"context"
	"path/filepath"
	"testing"

	"docod/internal/extractor"
	"docod/internal/graph"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteStore_SaveGraph_SnapshotSync(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewSQLiteStore(dbPath)
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Initial snapshot: A, B and edge A->B
	g1 := graph.NewGraph()
	a := testUnit("a:FuncA:1", "FuncA", "file_a.go", 1, 10)
	b := testUnit("b:FuncB:1", "FuncB", "file_b.go", 1, 10)
	g1.AddUnit(a)
	g1.AddUnit(b)
	g1.Edges = []graph.Edge{{From: a.ID, To: b.ID, Kind: "calls"}}
	require.NoError(t, store.SaveGraph(ctx, g1))

	// New snapshot: remove A, add C, and replace edge with C->B.
	g2 := graph.NewGraph()
	c := testUnit("c:FuncC:1", "FuncC", "file_c.go", 1, 10)
	g2.AddUnit(b)
	g2.AddUnit(c)
	g2.Edges = []graph.Edge{{From: c.ID, To: b.ID, Kind: "calls"}}
	require.NoError(t, store.SaveGraph(ctx, g2))

	loaded, err := store.LoadGraph(ctx)
	require.NoError(t, err)

	// Node snapshot should match exactly (A removed).
	assert.Len(t, loaded.Nodes, 2)
	_, hasA := loaded.Nodes[a.ID]
	assert.False(t, hasA)
	_, hasB := loaded.Nodes[b.ID]
	assert.True(t, hasB)
	_, hasC := loaded.Nodes[c.ID]
	assert.True(t, hasC)

	// Edge snapshot should match exactly (old edge removed).
	assert.Len(t, loaded.Edges, 1)
	assert.Equal(t, c.ID, loaded.Edges[0].From)
	assert.Equal(t, b.ID, loaded.Edges[0].To)
	assert.Equal(t, "calls", loaded.Edges[0].Kind)
}

func TestSQLiteStore_SaveGraph_EmptySnapshotClearsData(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewSQLiteStore(dbPath)
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	g := graph.NewGraph()
	u := testUnit("x:FuncX:1", "FuncX", "file_x.go", 1, 10)
	g.AddUnit(u)
	require.NoError(t, store.SaveGraph(ctx, g))

	empty := graph.NewGraph()
	require.NoError(t, store.SaveGraph(ctx, empty))

	loaded, err := store.LoadGraph(ctx)
	require.NoError(t, err)
	assert.Empty(t, loaded.Nodes)
	assert.Empty(t, loaded.Edges)
}

func testUnit(id, name, path string, startLine, endLine int) *extractor.CodeUnit {
	return &extractor.CodeUnit{
		ID:        id,
		Name:      name,
		Filepath:  path,
		StartLine: startLine,
		EndLine:   endLine,
		UnitType:  "function",
		Language:  "go",
	}
}
