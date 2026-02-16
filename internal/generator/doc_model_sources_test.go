package generator

import (
	"testing"

	"docod/internal/knowledge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSourcesFromChunk_UsesSymbolSources(t *testing.T) {
	chunk := knowledge.SearchChunk{
		ID:       "pkg/a.go",
		FilePath: "pkg/a.go",
		Sources: []knowledge.ChunkSource{
			{
				SymbolID:   "pkg/a.go#FuncA",
				FilePath:   "pkg/a.go",
				StartLine:  10,
				EndLine:    42,
				Relation:   "dependency",
				Confidence: 0.77,
			},
		},
	}

	sources := BuildSourcesFromChunk(chunk)
	require.Len(t, sources, 1)
	assert.Equal(t, "pkg/a.go#FuncA", sources[0].SymbolID)
	assert.Equal(t, "pkg/a.go", sources[0].FilePath)
	assert.Equal(t, 10, sources[0].StartLine)
	assert.Equal(t, 42, sources[0].EndLine)
	assert.Equal(t, "dependency", sources[0].Relation)
	assert.InDelta(t, 0.77, sources[0].Confidence, 0.001)
}

func TestBuildSourcesFromChunk_FallbackWhenMissingSources(t *testing.T) {
	chunk := knowledge.SearchChunk{ID: "pkg/a.go", FilePath: "pkg/a.go"}
	sources := BuildSourcesFromChunk(chunk)
	require.Len(t, sources, 1)
	assert.Equal(t, "pkg/a.go", sources[0].SymbolID)
	assert.Equal(t, "pkg/a.go", sources[0].FilePath)
	assert.Equal(t, 1, sources[0].StartLine)
	assert.Equal(t, 1, sources[0].EndLine)
	assert.Equal(t, "primary", sources[0].Relation)
}
