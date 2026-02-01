package graph

import (
	"docod/internal/extractor"
	"strings"
)

// Node represents a vertex in the dependency graph.
type Node struct {
	Unit *extractor.CodeUnit
}

// Edge represents a directed relationship between two nodes.
type Edge struct {
	From string // Source CodeUnit ID
	To   string // Target CodeUnit ID
	Kind string // Relationship type
}

// Graph manages nodes and their relationships.
type Graph struct {
	Nodes map[string]*Node
	Edges []Edge
	
	// Index for faster lookup: Name -> []ID
	// Useful for resolving name-based relations to actual IDs.
	nameIndex map[string][]string
}

// NewGraph creates an empty graph.
func NewGraph() *Graph {
	return &Graph{
		Nodes:     make(map[string]*Node),
		Edges:     []Edge{},
		nameIndex: make(map[string][]string),
	}
}

// AddUnit adds a CodeUnit as a node and indexes it.
func (g *Graph) AddUnit(unit *extractor.CodeUnit) {
	if unit == nil {
		return
	}
	g.Nodes[unit.ID] = &Node{Unit: unit}
	
	// Simple index: Name -> ID
	g.nameIndex[unit.Name] = append(g.nameIndex[unit.Name], unit.ID)
	
	// Qualified index: Package.Name -> ID
	if unit.Package != "" {
		key := unit.Package + "." + unit.Name
		g.nameIndex[key] = append(g.nameIndex[key], unit.ID)
	}
}

// LinkRelations attempts to resolve all name-based relations to actual node IDs.
func (g *Graph) LinkRelations() {
	g.Edges = []Edge{} // Reset edges
	
	for sourceID, node := range g.Nodes {
		for _, rel := range node.Unit.Relations {
			targets := g.resolveTarget(rel.Target, node.Unit.Package)
			for _, targetID := range targets {
				g.Edges = append(g.Edges, Edge{
					From: sourceID,
					To:   targetID,
					Kind: rel.Kind,
				})
			}
		}
	}
}

// resolveTarget finds potential target IDs for a given name.
func (g *Graph) resolveTarget(targetName string, sourcePackage string) []string {
	// Normalize target name (e.g., "*Extractor" -> "Extractor", "[]Node" -> "Node")
	cleanName := strings.TrimPrefix(targetName, "*")
	cleanName = strings.TrimPrefix(cleanName, "[]")
	
	// 1. Try exact match with normalized name
	if ids, ok := g.nameIndex[cleanName]; ok {
		return ids
	}
	
	// 2. Try match with original name (for qualified names like pkg.Type)
	if ids, ok := g.nameIndex[targetName]; ok {
		return ids
	}
	
	// 3. Try package-local match with normalized name
	localKey := sourcePackage + "." + cleanName
	if ids, ok := g.nameIndex[localKey]; ok {
		return ids
	}
	
	return nil
}

// GetDependencies returns all nodes that the given node depends on.
func (g *Graph) GetDependencies(id string) []*Node {
	var deps []*Node
	for _, edge := range g.Edges {
		if edge.From == id {
			if node, ok := g.Nodes[edge.To]; ok {
				deps = append(deps, node)
			}
		}
	}
	return deps
}

// GetDependents returns all nodes that depend on the given node.
func (g *Graph) GetDependents(id string) []*Node {
	var deps []*Node
	for _, edge := range g.Edges {
		if edge.To == id {
			if node, ok := g.Nodes[edge.From]; ok {
				deps = append(deps, node)
			}
		}
	}
	return deps
}
