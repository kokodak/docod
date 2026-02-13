package resolver

import "docod/internal/graph"

type ResolveStats struct {
	Attempted int
	Resolved  int
	Skipped   int
}

type GraphResolver interface {
	Name() string
	Resolve(g *graph.Graph) (ResolveStats, error)
}

type StageResult struct {
	Resolver         string
	Stats            ResolveStats
	UnresolvedBefore int
	UnresolvedAfter  int
	EdgeCount        int
	Err              error
}

type ResolverChain struct {
	resolvers []GraphResolver
}

func NewResolverChain(resolvers ...GraphResolver) *ResolverChain {
	return &ResolverChain{resolvers: resolvers}
}

func NewDefaultChain() *ResolverChain {
	return NewResolverChain(NewHeuristicResolver(), NewGoTypesResolver())
}

func (c *ResolverChain) Run(g *graph.Graph) []StageResult {
	if g == nil {
		return nil
	}

	var out []StageResult
	for _, r := range c.resolvers {
		before := len(g.Unresolved)
		stats, err := r.Resolve(g)
		after := len(g.Unresolved)
		out = append(out, StageResult{
			Resolver:         r.Name(),
			Stats:            stats,
			UnresolvedBefore: before,
			UnresolvedAfter:  after,
			EdgeCount:        len(g.Edges),
			Err:              err,
		})
		if err != nil {
			break
		}
	}
	return out
}

type HeuristicResolver struct{}

func NewHeuristicResolver() *HeuristicResolver {
	return &HeuristicResolver{}
}

func (r *HeuristicResolver) Name() string {
	return "heuristic"
}

func (r *HeuristicResolver) Resolve(g *graph.Graph) (ResolveStats, error) {
	if g == nil {
		return ResolveStats{}, nil
	}
	g.LinkRelations()
	return ResolveStats{
		Attempted: len(g.Edges) + len(g.Unresolved),
		Resolved:  len(g.Edges),
		Skipped:   len(g.Unresolved),
	}, nil
}
