package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDefaultFullDocPlan(t *testing.T) {
	plan := BuildDefaultFullDocPlan()
	require.NotNil(t, plan)
	require.GreaterOrEqual(t, len(plan.Sections), 3)

	overview, ok := plan.SectionByID("overview")
	require.True(t, ok)
	assert.True(t, overview.RequireMermaid)
	assert.Greater(t, overview.TopK, 0)
	assert.NotEmpty(t, overview.QueryText())
}
