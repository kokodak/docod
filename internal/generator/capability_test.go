package generator

import (
	"testing"

	"docod/internal/knowledge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractCapabilities_GroupsSemanticBuckets(t *testing.T) {
	chunks := []knowledge.SearchChunk{
		{ID: "1", Name: "ResolveRelations", UnitType: "function", Package: "resolver", Description: "resolve unresolved relations"},
		{ID: "2", Name: "BuildDocUpdatePlan", UnitType: "function", Package: "planner", Description: "plan section updates"},
		{ID: "3", Name: "SearchByText", UnitType: "function", Package: "knowledge", Description: "semantic vector search"},
		{ID: "4", Name: "GenerateDocs", UnitType: "function", Package: "generator", Description: "generate markdown documentation"},
	}

	caps := ExtractCapabilities(chunks, 5)
	require.NotEmpty(t, caps)

	foundRetrieval := false
	for _, c := range caps {
		if c.Key == "retrieval" {
			foundRetrieval = true
		}
	}
	assert.True(t, foundRetrieval)
}

func TestBuildKeyFeaturesSection_RendersCapabilities(t *testing.T) {
	caps := []Capability{
		{
			Key:    "retrieval",
			Title:  "Semantic Retrieval",
			Intent: "Retrieve relevant code evidence.",
			Chunks: []knowledge.SearchChunk{{Name: "SearchByText", Description: "retrieves similar chunks", Signature: "func SearchByText(...)"}},
		},
	}

	md := BuildKeyFeaturesSection(caps)
	assert.Contains(t, md, "# Key Features")
	assert.Contains(t, md, "## Semantic Retrieval")
	assert.Contains(t, md, "**Intent**")
	assert.Contains(t, md, "**Behavior**")
	assert.Contains(t, md, "```go")
}
