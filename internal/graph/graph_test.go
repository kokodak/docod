package graph

import (
	"docod/internal/extractor"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGraph_LinkRelations(t *testing.T) {
	g := NewGraph()

	// 1. Define sample units
	unitA := &extractor.CodeUnit{
		ID:      "file1:FuncA:1",
		Name:    "FuncA",
		Package: "pkg1",
		Relations: []extractor.Relation{
			{Target: "FuncB", Kind: "calls"},
			{Target: "pkg2.TypeC", Kind: "uses_type"},
		},
	}

	unitB := &extractor.CodeUnit{
		ID:      "file1:FuncB:10",
		Name:    "FuncB",
		Package: "pkg1",
	}

	unitC := &extractor.CodeUnit{
		ID:      "file2:TypeC:1",
		Name:    "TypeC",
		Package: "pkg2",
	}

	g.AddUnit(unitA)
	g.AddUnit(unitB)
	g.AddUnit(unitC)

	// 2. Link them
	g.LinkRelations()

	// 3. Verify
	t.Run("Internal package resolution", func(t *testing.T) {
		deps := g.GetDependencies(unitA.ID)
		assert.NotEmpty(t, deps)

		var foundB bool
		for _, d := range deps {
			if d.Unit.Name == "FuncB" {
				foundB = true
			}
		}
		assert.True(t, foundB, "FuncA should link to FuncB in the same package")
	})

	t.Run("Cross package resolution", func(t *testing.T) {
		deps := g.GetDependencies(unitA.ID)
		var foundC bool
		for _, d := range deps {
			if d.Unit.Name == "TypeC" && d.Unit.Package == "pkg2" {
				foundC = true
			}
		}
		assert.True(t, foundC, "FuncA should link to pkg2.TypeC")
	})

	t.Run("Dependent lookup", func(t *testing.T) {
		dependents := g.GetDependents(unitC.ID)
		assert.Len(t, dependents, 1)
		assert.Equal(t, "FuncA", dependents[0].Unit.Name)
	})
}
