package retrieval

import (
	"sort"

	"docod/internal/git"
	"docod/internal/graph"
)

// Config controls how impact subgraphs are extracted.
type Config struct {
	MaxHops       int
	MinConfidence float64
	AllowedKinds  map[graph.RelationKind]bool
}

func DefaultConfig() Config {
	return Config{
		MaxHops:       2,
		MinConfidence: 0.0,
		AllowedKinds:  nil,
	}
}

// Subgraph is the retrieval result used by downstream planning/generation.
type Subgraph struct {
	MaxHops      int
	SeedIDs      []string
	UpdatedFiles []string
	NodeIDs      []string
	NodeScores   map[string]float64
	Edges        []graph.Edge
}

func ExtractFromChanges(g *graph.Graph, changes []git.ChangedFile, cfg Config) *Subgraph {
	if g == nil {
		return &Subgraph{}
	}
	if cfg.MaxHops < 0 {
		cfg.MaxHops = 0
	}

	seedSet := findSeedNodeIDs(g, changes)
	seedIDs := sortedKeys(seedSet)
	updatedFiles := changedFilePaths(changes)

	if len(seedIDs) == 0 {
		return &Subgraph{
			MaxHops:      cfg.MaxHops,
			SeedIDs:      seedIDs,
			UpdatedFiles: updatedFiles,
			NodeIDs:      nil,
			NodeScores:   map[string]float64{},
			Edges:        nil,
		}
	}

	adj := make(map[string][]edgeHop)
	for _, e := range g.Edges {
		if !edgeAllowed(e, cfg) {
			continue
		}
		adj[e.From] = append(adj[e.From], edgeHop{to: e.To, edge: e})
		adj[e.To] = append(adj[e.To], edgeHop{to: e.From, edge: e})
	}

	visitedDepth := make(map[string]int, len(seedIDs))
	nodeScores := make(map[string]float64, len(seedIDs))
	queue := make([]queueItem, 0, len(seedIDs))
	for _, id := range seedIDs {
		visitedDepth[id] = 0
		nodeScores[id] = 1.0
		queue = append(queue, queueItem{id: id, depth: 0})
	}

	edgeSeen := make(map[string]bool)
	edges := make([]graph.Edge, 0)

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if cur.depth >= cfg.MaxHops {
			continue
		}

		for _, next := range adj[cur.id] {
			edgeKey := edgeSignature(next.edge)
			if !edgeSeen[edgeKey] {
				edgeSeen[edgeKey] = true
				edges = append(edges, next.edge)
			}

			nextDepth := cur.depth + 1
			candidateScore := nodeScores[cur.id] * normalizedEdgeConfidence(next.edge.Confidence)
			if candidateScore > nodeScores[next.to] {
				nodeScores[next.to] = candidateScore
			}
			prevDepth, seen := visitedDepth[next.to]
			if !seen || nextDepth < prevDepth {
				visitedDepth[next.to] = nextDepth
				queue = append(queue, queueItem{id: next.to, depth: nextDepth})
			}
		}
	}

	nodeIDs := sortedKeys(visitedDepth)
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From == edges[j].From {
			if edges[i].To == edges[j].To {
				return string(edges[i].Kind) < string(edges[j].Kind)
			}
			return edges[i].To < edges[j].To
		}
		return edges[i].From < edges[j].From
	})

	return &Subgraph{
		MaxHops:      cfg.MaxHops,
		SeedIDs:      seedIDs,
		UpdatedFiles: updatedFiles,
		NodeIDs:      nodeIDs,
		NodeScores:   nodeScores,
		Edges:        edges,
	}
}

type queueItem struct {
	id    string
	depth int
}

type edgeHop struct {
	to   string
	edge graph.Edge
}

func findSeedNodeIDs(g *graph.Graph, changes []git.ChangedFile) map[string]int {
	out := make(map[string]int)
	for _, ch := range changes {
		for id, node := range g.Nodes {
			if node == nil || node.Unit == nil {
				continue
			}
			if node.Unit.Filepath != ch.Path {
				continue
			}
			if !lineRangeOverlaps(node.Unit.StartLine, node.Unit.EndLine, ch.ChangedLines) {
				continue
			}
			out[id] = 0
		}
	}
	return out
}

func lineRangeOverlaps(start, end int, changed []int) bool {
	if len(changed) == 0 {
		return true
	}
	for _, line := range changed {
		if line >= start && line <= end {
			return true
		}
	}
	return false
}

func edgeAllowed(e graph.Edge, cfg Config) bool {
	if cfg.MinConfidence > 0 && e.Confidence < cfg.MinConfidence {
		return false
	}
	if len(cfg.AllowedKinds) == 0 {
		return true
	}
	return cfg.AllowedKinds[e.Kind]
}

func edgeSignature(e graph.Edge) string {
	return e.From + "->" + e.To + ":" + string(e.Kind)
}

func normalizedEdgeConfidence(c float64) float64 {
	if c <= 0 {
		return 0.5
	}
	if c > 1 {
		return 1
	}
	return c
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func changedFilePaths(changes []git.ChangedFile) []string {
	seen := make(map[string]bool, len(changes))
	paths := make([]string, 0, len(changes))
	for _, ch := range changes {
		if ch.Path == "" || seen[ch.Path] {
			continue
		}
		seen[ch.Path] = true
		paths = append(paths, ch.Path)
	}
	sort.Strings(paths)
	return paths
}
