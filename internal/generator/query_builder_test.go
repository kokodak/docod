package generator

import (
	"testing"

	"docod/internal/knowledge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSectionQueries_UsesPlanAndCapabilities(t *testing.T) {
	plan := SectionDocPlan{
		SectionID:      "key-features",
		Goal:           "Describe capabilities and constraints.",
		RequiredBlocks: []string{"Capability", "Usage"},
		QueryHints:     []string{"core capabilities", "workflow behavior"},
	}
	caps := []Capability{{Title: "Semantic Retrieval", Intent: "Retrieve relevant evidence."}}

	queries := BuildSectionQueries(plan, caps)
	require.NotEmpty(t, queries)
	assert.Contains(t, queries[0], "core capabilities")
	assert.GreaterOrEqual(t, len(queries), 4)
}

func TestDiversityRerank_ReducesSingleFileDominance(t *testing.T) {
	chunks := []knowledge.SearchChunk{
		{ID: "a1", FilePath: "a.go", Name: "A1", UnitType: "function", Description: "x"},
		{ID: "a2", FilePath: "a.go", Name: "A2", UnitType: "function", Description: "x"},
		{ID: "a3", FilePath: "a.go", Name: "A3", UnitType: "function", Description: "x"},
		{ID: "b1", FilePath: "b.go", Name: "B1", UnitType: "function", Description: "x"},
		{ID: "c1", FilePath: "c.go", Name: "C1", UnitType: "function", Description: "x"},
	}

	out := DiversityRerank(chunks, 4, 1)
	require.Len(t, out, 4)

	counts := map[string]int{}
	for _, c := range out {
		counts[c.FilePath]++
	}
	assert.LessOrEqual(t, counts["a.go"], 2)
}

func TestBuildEvidenceStats_ComputesCoverageAndConfidence(t *testing.T) {
	plan := SectionDocPlan{SectionID: "overview", MinEvidence: 4}
	queries := []string{"q1", "q2"}
	chunks := []knowledge.SearchChunk{
		{
			ID:       "a",
			FilePath: "a.go",
			Sources: []knowledge.ChunkSource{
				{SymbolID: "a1", FilePath: "a.go", Confidence: 0.9},
			},
		},
		{
			ID:       "b",
			FilePath: "b.go",
			Sources: []knowledge.ChunkSource{
				{SymbolID: "b1", FilePath: "b.go", Confidence: 0.8},
			},
		},
	}

	stats := buildEvidenceStats(plan, queries, chunks)
	require.NotNil(t, stats)
	assert.InDelta(t, 0.5, stats.Coverage, 0.001)
	assert.Greater(t, stats.Confidence, 0.6)
	assert.Equal(t, 2, stats.QueryCount)
	assert.Equal(t, 2, stats.ChunkCount)
	assert.True(t, stats.LowEvidence)
}
