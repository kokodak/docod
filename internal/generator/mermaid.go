package generator

import (
	"docod/internal/knowledge"
	"fmt"
	"regexp"
	"sort"
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

// GenerateArchitectureFlow builds a high-level architecture flow from semantically relevant symbols.
func (m *MermaidGenerator) GenerateArchitectureFlow(chunks []knowledge.SearchChunk) string {
	stageKeywords := []struct {
		Key   string
		Label string
		Match []string
	}{
		{Key: "entry", Label: "Entry/API", Match: []string{"main", "cmd", "api", "handler", "controller", "router", "endpoint", "serve"}},
		{Key: "app", Label: "Orchestration", Match: []string{"service", "orchestr", "pipeline", "runner", "sync", "workflow", "manager"}},
		{Key: "domain", Label: "Domain Logic", Match: []string{"domain", "core", "resolver", "analy", "planner", "extract", "generator"}},
		{Key: "data", Label: "Storage/Index", Match: []string{"store", "repo", "db", "sqlite", "index", "cache", "vector"}},
		{Key: "output", Label: "Output", Match: []string{"doc", "render", "markdown", "writer", "export"}},
	}

	stageHits := map[string]int{}
	stageExamples := map[string]map[string]int{}
	type edgeKey struct {
		from string
		to   string
	}
	edgeWeights := map[edgeKey]int{}

	nameStages := make(map[string]string)
	for _, c := range chunks {
		stage := bestStageForChunk(c, stageKeywords)
		if stage == "" {
			continue
		}
		if strings.TrimSpace(c.Name) != "" {
			nameStages[c.Name] = stage
		}
		if stageExamples[stage] == nil {
			stageExamples[stage] = map[string]int{}
		}
		if pkg := strings.TrimSpace(c.Package); pkg != "" {
			stageExamples[stage][pkg]++
		}
	}

	for _, c := range chunks {
		stage := bestStageForChunk(c, stageKeywords)
		if stage == "" {
			continue
		}
		stageHits[stage]++
		for _, dep := range c.Dependencies {
			ds := strings.TrimSpace(dep)
			if ds == "" {
				continue
			}
			depStage := nameStages[ds]
			if depStage == "" || depStage == stage {
				continue
			}
			edgeWeights[edgeKey{from: stage, to: depStage}]++
		}
		for _, caller := range c.UsedBy {
			cs := strings.TrimSpace(caller)
			if cs == "" {
				continue
			}
			callerStage := nameStages[cs]
			if callerStage == "" || callerStage == stage {
				continue
			}
			// caller -> callee
			edgeWeights[edgeKey{from: callerStage, to: stage}]++
		}
	}

	ordered := make([]struct {
		Key   string
		Label string
	}, 0, len(stageKeywords))
	for _, stage := range stageKeywords {
		if stageHits[stage.Key] > 0 {
			ordered = append(ordered, struct {
				Key   string
				Label string
			}{Key: stage.Key, Label: stage.Label})
		}
	}
	if len(ordered) < 3 {
		// Fallback to package-level flow if stage extraction is too weak.
		return m.generatePackageFlow(chunks)
	}
	stageOrder := map[string]int{}
	for i, s := range stageKeywords {
		stageOrder[s.Key] = i
	}

	var sb strings.Builder
	sb.WriteString("```mermaid\n")
	sb.WriteString("graph LR\n")
	for _, node := range ordered {
		id := sanitizeMermaidID(node.Key)
		label := node.Label
		if ex := topStageExamples(stageExamples[node.Key], 2); len(ex) > 0 {
			label = label + "\\n" + strings.Join(ex, ", ")
		}
		sb.WriteString(fmt.Sprintf("    %s[%q]\n", id, label))
	}
	drawn := 0
	for _, from := range ordered {
		bestTo := ""
		bestW := 0
		for _, to := range ordered {
			if from.Key == to.Key {
				continue
			}
			if stageOrder[to.Key] <= stageOrder[from.Key] {
				continue
			}
			w := edgeWeights[edgeKey{from: from.Key, to: to.Key}]
			if w > bestW {
				bestW = w
				bestTo = to.Key
			}
		}
		if bestTo != "" && bestW > 0 {
			sb.WriteString(fmt.Sprintf("    %s --> %s\n", sanitizeMermaidID(from.Key), sanitizeMermaidID(bestTo)))
			drawn++
		}
	}
	if drawn < 2 {
		// Deterministic fallback chain when relation signal is weak.
		for i := 1; i < len(ordered); i++ {
			sb.WriteString(fmt.Sprintf("    %s --> %s\n", sanitizeMermaidID(ordered[i-1].Key), sanitizeMermaidID(ordered[i].Key)))
		}
	}
	sb.WriteString("```\n")
	return sb.String()
}

func topStageExamples(m map[string]int, limit int) []string {
	if len(m) == 0 || limit <= 0 {
		return nil
	}
	type pair struct {
		v string
		n int
	}
	items := make([]pair, 0, len(m))
	for v, n := range m {
		items = append(items, pair{v: v, n: n})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].n == items[j].n {
			return items[i].v < items[j].v
		}
		return items[i].n > items[j].n
	})
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.v)
	}
	return out
}

func (m *MermaidGenerator) generatePackageFlow(chunks []knowledge.SearchChunk) string {
	pkgCount := make(map[string]int)
	for _, c := range chunks {
		pkg := strings.TrimSpace(c.Package)
		if pkg == "" {
			continue
		}
		pkgCount[pkg]++
	}
	if len(pkgCount) == 0 {
		return "```mermaid\ngraph LR\n    A[\"Source\"] --> B[\"Core Logic\"] --> C[\"Output\"]\n```\n"
	}

	type pkgNode struct {
		Pkg string
		Cnt int
	}
	nodes := make([]pkgNode, 0, len(pkgCount))
	for pkg, n := range pkgCount {
		nodes = append(nodes, pkgNode{Pkg: pkg, Cnt: n})
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Cnt == nodes[j].Cnt {
			return nodes[i].Pkg < nodes[j].Pkg
		}
		return nodes[i].Cnt > nodes[j].Cnt
	})
	if len(nodes) > 6 {
		nodes = nodes[:6]
	}

	var sb strings.Builder
	sb.WriteString("```mermaid\n")
	sb.WriteString("graph LR\n")
	for i, n := range nodes {
		id := sanitizeMermaidID(n.Pkg)
		sb.WriteString(fmt.Sprintf("    %s[%q]\n", id, n.Pkg))
		if i > 0 {
			prev := sanitizeMermaidID(nodes[i-1].Pkg)
			sb.WriteString(fmt.Sprintf("    %s --> %s\n", prev, id))
		}
	}
	sb.WriteString("```\n")
	return sb.String()
}

// GenerateArchitectureSnapshot emits a compact component graph to avoid noisy symbol-level dumps.
func (m *MermaidGenerator) GenerateArchitectureSnapshot(chunks []knowledge.SearchChunk) string {
	type edge struct {
		from string
		to   string
	}
	pkgWeight := map[string]int{}
	edgeWeight := map[edge]int{}
	seenNames := map[string]string{} // symbol -> pkg

	for _, c := range chunks {
		if c.Package == "" {
			continue
		}
		pkgWeight[c.Package]++
		if c.UnitType == "file_module" || c.UnitType == "symbol_segment" {
			continue
		}
		seenNames[c.Name] = c.Package
	}
	for _, c := range chunks {
		from := c.Package
		if from == "" {
			continue
		}
		for _, dep := range c.Dependencies {
			to := seenNames[dep]
			if to == "" || to == from {
				continue
			}
			edgeWeight[edge{from: from, to: to}]++
		}
	}

	type pkgNode struct {
		name string
		w    int
	}
	nodes := make([]pkgNode, 0, len(pkgWeight))
	for p, w := range pkgWeight {
		nodes = append(nodes, pkgNode{name: p, w: w})
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].w == nodes[j].w {
			return nodes[i].name < nodes[j].name
		}
		return nodes[i].w > nodes[j].w
	})
	if len(nodes) > 8 {
		nodes = nodes[:8]
	}
	selected := map[string]bool{}
	for _, n := range nodes {
		selected[n.name] = true
	}

	type eNode struct {
		e edge
		w int
	}
	edges := make([]eNode, 0, len(edgeWeight))
	for e, w := range edgeWeight {
		if !selected[e.from] || !selected[e.to] {
			continue
		}
		edges = append(edges, eNode{e: e, w: w})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].w == edges[j].w {
			if edges[i].e.from == edges[j].e.from {
				return edges[i].e.to < edges[j].e.to
			}
			return edges[i].e.from < edges[j].e.from
		}
		return edges[i].w > edges[j].w
	})
	if len(edges) > 10 {
		edges = edges[:10]
	}

	var sb strings.Builder
	sb.WriteString("```mermaid\n")
	sb.WriteString("graph LR\n")
	for _, n := range nodes {
		sb.WriteString(fmt.Sprintf("    %s[%q]\n", sanitizeMermaidID(n.name), n.name))
	}
	if len(edges) == 0 {
		for i := 1; i < len(nodes); i++ {
			sb.WriteString(fmt.Sprintf("    %s --> %s\n", sanitizeMermaidID(nodes[i-1].name), sanitizeMermaidID(nodes[i].name)))
		}
	} else {
		for _, e := range edges {
			sb.WriteString(fmt.Sprintf("    %s --> %s\n", sanitizeMermaidID(e.e.from), sanitizeMermaidID(e.e.to)))
		}
	}
	sb.WriteString("```\n")
	return sb.String()
}

func bestStageForChunk(c knowledge.SearchChunk, defs []struct {
	Key   string
	Label string
	Match []string
}) string {
	text := strings.ToLower(c.Name + " " + c.Package + " " + c.Description)
	bestKey := ""
	bestScore := 0
	for _, stage := range defs {
		score := 0
		for _, token := range stage.Match {
			if strings.Contains(text, token) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			bestKey = stage.Key
		}
	}
	return bestKey
}

func sanitizeMermaidID(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return "node"
	}
	re := regexp.MustCompile(`[^a-z0-9_]`)
	v = re.ReplaceAllString(strings.ReplaceAll(v, "-", "_"), "_")
	if v == "" {
		return "node"
	}
	if v[0] >= '0' && v[0] <= '9' {
		v = "n_" + v
	}
	return v
}
