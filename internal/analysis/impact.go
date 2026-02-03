package analysis

import (
	"docod/internal/git"
	"docod/internal/graph"
)

// ImpactReport summarizes the code units affected by changes.
type ImpactReport struct {
	DirectlyAffected   []*graph.Node
	IndirectlyAffected []*graph.Node
}

// Analyzer performs impact analysis on the dependency graph.
type Analyzer struct {
	g *graph.Graph
}

// NewAnalyzer creates a new analyzer.
func NewAnalyzer(g *graph.Graph) *Analyzer {
	return &Analyzer{g: g}
}

// AnalyzeImpact identifies which nodes are affected by the given changes.
func (a *Analyzer) AnalyzeImpact(changes []git.ChangedFile) (*ImpactReport, error) {
	report := &ImpactReport{
		DirectlyAffected:   []*graph.Node{},
		IndirectlyAffected: []*graph.Node{},
	}

	seenDirect := make(map[string]bool)
	seenIndirect := make(map[string]bool)

	// 1. Find Direct Impacts
	// Optimization: Index nodes by filepath on the fly if this becomes slow.
	for _, change := range changes {
		for _, node := range a.g.Nodes {
			if node.Unit.Filepath == change.Path {
				if isAffected(node, change.ChangedLines) {
					if !seenDirect[node.Unit.ID] {
						report.DirectlyAffected = append(report.DirectlyAffected, node)
						seenDirect[node.Unit.ID] = true
					}
				}
			}
		}
	}

	// 2. Find Indirect Impacts (Callers)
	for _, node := range report.DirectlyAffected {
		dependents := a.g.GetDependents(node.Unit.ID)
		for _, dep := range dependents {
			if !seenDirect[dep.Unit.ID] && !seenIndirect[dep.Unit.ID] {
				report.IndirectlyAffected = append(report.IndirectlyAffected, dep)
				seenIndirect[dep.Unit.ID] = true
			}
		}
	}

	return report, nil
}

func isAffected(node *graph.Node, lines []int) bool {
	// Simple overlap check
	for _, line := range lines {
		if line >= node.Unit.StartLine && line <= node.Unit.EndLine {
			return true
		}
	}
	return false
}
