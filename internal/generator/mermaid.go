package generator

import (
	"docod/internal/knowledge"
	"fmt"
	"strings"
)

// MermaidGenerator creates diagrams from knowledge chunks.
type MermaidGenerator struct{}

func (m *MermaidGenerator) GeneratePackageDiagram(pkgName string, chunks []knowledge.SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("```mermaid\n")
	sb.WriteString("classDiagram\n")
	
	// Define classes/interfaces
	for _, c := range chunks {
		// Only visualize structs and interfaces
		if c.UnitType != "struct" && c.UnitType != "interface" {
			continue
		}
		sb.WriteString(fmt.Sprintf("    class %s {\n", c.Name))
		if c.UnitType == "interface" {
			sb.WriteString("        <<interface>>\n")
		}
		// Method/Field annotations are omitted for clarity.
		sb.WriteString("    }\n")
	}

	// Define relationships
	for _, c := range chunks {
		for _, dep := range c.Dependencies {
			// Basic dependency arrow
			// Filter to only show internal dependencies to avoid clutter with stdlib
			if !strings.Contains(dep, ".") { 
				sb.WriteString(fmt.Sprintf("    %s ..> %s : uses\n", c.Name, dep))
			}
		}
	}
	
sb.WriteString("```\n")
	return sb.String()
}

func (m *MermaidGenerator) GenerateFlowChart(chunks []knowledge.SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("```mermaid\ngraph TD\n")
	
	// Create a high-level flow chart focusing on function calls between packages
	// This is a simplified heuristic
	for _, c := range chunks {
		if c.UnitType != "function" && c.UnitType != "method" {
			continue
		}
		
		for _, usedBy := range c.UsedBy {
			// usedBy -> c.Name
			sb.WriteString(fmt.Sprintf("    %s --> %s\n", usedBy, c.Name))
		}
	}
	
sb.WriteString("```\n")
	return sb.String()
}

