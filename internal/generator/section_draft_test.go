package generator

import (
	"testing"

	"docod/internal/knowledge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSectionDraft_KeyFeaturesHasBoundSources(t *testing.T) {
	chunks := []knowledge.SearchChunk{
		{
			ID:          "a",
			FilePath:    "pkg/a.go",
			Name:        "ResolveRelations",
			UnitType:    "function",
			Description: "Resolves unresolved graph relations.",
			Sources: []knowledge.ChunkSource{{
				SymbolID:   "sym.a",
				FilePath:   "pkg/a.go",
				StartLine:  10,
				EndLine:    30,
				Relation:   "primary",
				Confidence: 0.9,
			}},
		},
	}
	caps := []Capability{{
		Key:        "resolution",
		Title:      "Symbol Resolution",
		Intent:     "Links unresolved symbols.",
		Chunks:     chunks,
		Confidence: 0.88,
	}}

	draft := BuildSectionDraft("key-features", "Key Features", chunks, caps)
	require.NotEmpty(t, draft.Claims)
	for _, c := range draft.Claims {
		require.NotEmpty(t, c.Sources)
	}
	require.NoError(t, ValidateSectionDraft(draft))
}

func TestRenderSectionDraftMarkdown_IsNarrative(t *testing.T) {
	d := SectionDraft{
		SectionID: "overview",
		Title:     "Overview",
		Claims: []DraftClaim{{
			ID:   "c1",
			Text: "Core pipeline links extraction to documentation updates.",
			Sources: []SourceRef{{
				SymbolID:  "sym.1",
				FilePath:  "main.go",
				Relation:  "primary",
				StartLine: 1,
				EndLine:   10,
			}},
			Confidence: 0.8,
		}},
	}

	md := RenderSectionDraftMarkdown(d)
	assert.Contains(t, md, "# Overview")
	assert.Contains(t, md, "## Architecture Intent")
	assert.NotContains(t, md, "_Evidence:")
}
