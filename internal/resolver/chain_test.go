package resolver

import (
	"testing"

	"docod/internal/graph"
)

type fakeResolver struct {
	name string
	fn   func(g *graph.Graph) (ResolveStats, error)
}

func (f fakeResolver) Name() string { return f.name }
func (f fakeResolver) Resolve(g *graph.Graph) (ResolveStats, error) {
	return f.fn(g)
}

func TestResolverChain_Run(t *testing.T) {
	g := graph.NewGraph()
	g.Unresolved = []graph.UnresolvedRelation{
		{From: "a", Target: "x", Kind: graph.RelationCalls, Reason: graph.ReasonNoCandidate},
		{From: "b", Target: "y", Kind: graph.RelationCalls, Reason: graph.ReasonNoCandidate},
	}

	r1 := fakeResolver{
		name: "r1",
		fn: func(g *graph.Graph) (ResolveStats, error) {
			if len(g.Unresolved) > 0 {
				g.Unresolved = g.Unresolved[1:]
			}
			g.Edges = append(g.Edges, graph.Edge{From: "a", To: "c", Kind: graph.RelationCalls})
			return ResolveStats{Attempted: 2, Resolved: 1, Skipped: 1}, nil
		},
	}
	r2 := fakeResolver{
		name: "r2",
		fn: func(g *graph.Graph) (ResolveStats, error) {
			g.Unresolved = nil
			g.Edges = append(g.Edges, graph.Edge{From: "b", To: "d", Kind: graph.RelationCalls})
			return ResolveStats{Attempted: 1, Resolved: 1, Skipped: 0}, nil
		},
	}

	chain := NewResolverChain(r1, r2)
	results := chain.Run(g)

	if len(results) != 2 {
		t.Fatalf("expected 2 stage results, got %d", len(results))
	}
	if results[0].Resolver != "r1" || results[1].Resolver != "r2" {
		t.Fatalf("unexpected resolver order: %+v", results)
	}
	if results[0].UnresolvedBefore != 2 || results[0].UnresolvedAfter != 1 {
		t.Fatalf("unexpected unresolved transition for r1: %+v", results[0])
	}
	if results[1].UnresolvedBefore != 1 || results[1].UnresolvedAfter != 0 {
		t.Fatalf("unexpected unresolved transition for r2: %+v", results[1])
	}
}
