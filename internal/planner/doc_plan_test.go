package planner

import (
	"testing"

	"docod/internal/generator"
	"docod/internal/retrieval"

	"github.com/stretchr/testify/assert"
)

func TestBuildDocUpdatePlan_ScoresAndSortsSections(t *testing.T) {
	model := &generator.DocModel{
		Sections: []generator.ModelSect{
			{
				ID: "overview",
				Sources: []generator.SourceRef{
					{SymbolID: "sym.A", FilePath: "a.go"},
				},
			},
			{
				ID: "dev",
				Sources: []generator.SourceRef{
					{SymbolID: "sym.A", FilePath: "a.go"},
					{SymbolID: "sym.B", FilePath: "b.go"},
				},
			},
		},
	}

	sg := &retrieval.Subgraph{
		NodeIDs:      []string{"sym.A", "sym.B", "sym.C"},
		UpdatedFiles: []string{"a.go"},
		NodeScores: map[string]float64{
			"sym.A": 0.9,
			"sym.B": 0.7,
			"sym.C": 0.4,
		},
	}

	plan := BuildDocUpdatePlan(model, sg)

	if assert.Len(t, plan.AffectedSections, 2) {
		assert.Equal(t, "dev", plan.AffectedSections[0].SectionID)
		assert.Equal(t, "overview", plan.AffectedSections[1].SectionID)
		assert.Greater(t, plan.AffectedSections[0].Score, plan.AffectedSections[1].Score)
		assert.GreaterOrEqual(t, plan.AffectedSections[0].Confidence, plan.AffectedSections[1].Confidence)
	}
	assert.Equal(t, []string{"sym.C"}, plan.UnmatchedSymbols)
}

func TestBuildDocUpdatePlan_HandlesNilModel(t *testing.T) {
	sg := &retrieval.Subgraph{NodeIDs: []string{"sym.A"}, UpdatedFiles: []string{"a.go"}}
	plan := BuildDocUpdatePlan(nil, sg)

	assert.Equal(t, []string{"sym.A"}, plan.TriggeredSymbolIDs)
	assert.Equal(t, []string{"a.go"}, plan.TriggeredFiles)
	assert.Equal(t, []string{"sym.A"}, plan.UnmatchedSymbols)
	assert.Empty(t, plan.AffectedSections)
}

func TestBuildDocUpdatePlan_ConfidenceFirstOrdering(t *testing.T) {
	model := &generator.DocModel{
		Sections: []generator.ModelSect{
			{
				ID: "high",
				Sources: []generator.SourceRef{
					{SymbolID: "sym.H", FilePath: "h.go", Confidence: 0.95},
				},
			},
			{
				ID: "low",
				Sources: []generator.SourceRef{
					{SymbolID: "sym.L", FilePath: "l.go", Confidence: 0.40},
				},
			},
		},
	}
	sg := &retrieval.Subgraph{
		NodeIDs:      []string{"sym.H", "sym.L"},
		UpdatedFiles: []string{"h.go", "l.go"},
		NodeScores: map[string]float64{
			"sym.H": 0.9,
			"sym.L": 0.4,
		},
	}

	plan := BuildDocUpdatePlan(model, sg)
	if assert.Len(t, plan.AffectedSections, 2) {
		assert.Equal(t, "high", plan.AffectedSections[0].SectionID)
		assert.Equal(t, "low", plan.AffectedSections[1].SectionID)
		assert.Greater(t, plan.AffectedSections[0].Confidence, plan.AffectedSections[1].Confidence)
	}
}
