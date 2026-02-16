package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"docod/internal/knowledge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDraftLLMContext_HydratesLowConfidenceFlowClaims(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "flow.go")
	content := strings.Join([]string{
		"package sample",
		"",
		"func StepA() {",
		"    StepB()",
		"}",
		"",
		"func StepB() {}",
	}, "\n")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	draft := SectionDraft{
		SectionID: "overview",
		Title:     "Overview",
		Claims: []DraftClaim{
			{
				ID:         "ov-1",
				Text:       "The flow routes StepA then StepB.",
				Confidence: 0.4,
				Sources: []SourceRef{{
					SymbolID:   "sym.stepA",
					FilePath:   path,
					StartLine:  3,
					EndLine:    5,
					Relation:   "primary",
					Confidence: 0.9,
				}},
			},
		},
	}

	chunks := []knowledge.SearchChunk{{
		ID:          "base-1",
		FilePath:    path,
		Name:        "StepA",
		UnitType:    "function",
		Description: "entry step",
		Signature:   "func StepA()",
		Content:     "func StepA(){ StepB() }",
	}}

	ctx := BuildDraftLLMContext(draft, chunks)
	require.NotEmpty(t, ctx)

	foundHydrated := false
	for _, c := range ctx {
		if c.UnitType == "evidence_block" {
			foundHydrated = true
			assert.Contains(t, c.Content, "StepB")
		}
	}
	assert.True(t, foundHydrated)
}

func TestBuildLayerBContext_PrioritizesMinimumFlowBlocks(t *testing.T) {
	tmp := t.TempDir()
	pathA := filepath.Join(tmp, "a.go")
	pathB := filepath.Join(tmp, "b.go")
	pathC := filepath.Join(tmp, "c.go")
	require.NoError(t, os.WriteFile(pathA, []byte("package x\nfunc A(){B()}"), 0644))
	require.NoError(t, os.WriteFile(pathB, []byte("package x\nfunc B(){C()}"), 0644))
	require.NoError(t, os.WriteFile(pathC, []byte("package x\nfunc C(){}"), 0644))

	draft := SectionDraft{
		SectionID: "overview",
		Title:     "Overview",
		Claims: []DraftClaim{
			{
				ID:         "flow-1",
				Text:       "Request flow starts at A then routes to B.",
				Confidence: 0.8,
				Sources: []SourceRef{{
					SymbolID: "sym.a", FilePath: pathA, StartLine: 2, EndLine: 2, Relation: "primary", Confidence: 0.9,
				}},
			},
			{
				ID:         "flow-2",
				Text:       "Pipeline flow then reaches C.",
				Confidence: 0.8,
				Sources: []SourceRef{{
					SymbolID: "sym.b", FilePath: pathB, StartLine: 2, EndLine: 2, Relation: "primary", Confidence: 0.9,
				}},
			},
			{
				ID:         "non-flow",
				Text:       "Validation logic applies constraints.",
				Confidence: 0.4,
				Sources: []SourceRef{{
					SymbolID: "sym.c", FilePath: pathC, StartLine: 2, EndLine: 2, Relation: "primary", Confidence: 0.9,
				}},
			},
		},
	}

	layerB := buildLayerBContext(draft, 3, 20, 2)
	require.NotEmpty(t, layerB)

	flowHits := 0
	for _, b := range layerB {
		if strings.Contains(b.ID, "sym.a") || strings.Contains(b.ID, "sym.b") {
			flowHits++
		}
	}
	assert.GreaterOrEqual(t, flowHits, 2)
}
