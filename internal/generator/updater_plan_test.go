package generator

import (
	"testing"

	"docod/internal/knowledge"

	"github.com/stretchr/testify/assert"
)

func TestMergePreferredSectionOrder(t *testing.T) {
	base := []string{"overview", "key-features", "development"}
	preferred := []string{"development", "overview"}

	merged := mergePreferredSectionOrder(base, preferred)
	assert.Equal(t, []string{"development", "overview", "key-features"}, merged)
}

func TestRouteUnmatchedToPreferred(t *testing.T) {
	unmatched := []knowledge.SearchChunk{
		{ID: "a.go", Name: "A"},
		{ID: "b.go", Name: "B"},
		{ID: "c.go", Name: "C"},
	}
	preferred := []string{"overview", "development"}

	routed, still := routeUnmatchedToPreferred(unmatched, preferred)
	assert.Empty(t, still)
	assert.Len(t, routed["overview"], 2)
	assert.Len(t, routed["development"], 1)
	assert.Equal(t, "A", routed["overview"][0].Name)
	assert.Equal(t, "B", routed["development"][0].Name)
	assert.Equal(t, "C", routed["overview"][1].Name)
}

func TestRouteUnmatchedToPreferred_NoPreferred(t *testing.T) {
	unmatched := []knowledge.SearchChunk{{ID: "a.go", Name: "A"}}
	routed, still := routeUnmatchedToPreferred(unmatched, nil)
	assert.Empty(t, routed)
	assert.Len(t, still, 1)
}

func TestResolveSectionConfidence(t *testing.T) {
	plan := &UpdatePlan{
		SectionConfidence: map[string]float64{
			"overview":    0.85,
			"development": 1.5,
			"invalid":     -1.0,
		},
	}

	assert.InDelta(t, 0.85, resolveSectionConfidence(plan, "overview"), 0.001)
	assert.InDelta(t, 1.0, resolveSectionConfidence(plan, "development"), 0.001)
	assert.InDelta(t, 0.0, resolveSectionConfidence(plan, "invalid"), 0.001)
	assert.InDelta(t, 0.0, resolveSectionConfidence(plan, "missing"), 0.001)
}
