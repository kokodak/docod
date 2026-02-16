package generator

import (
	"docod/internal/knowledge"
	"fmt"
	"sort"
	"strings"
)

type Capability struct {
	Key        string
	Title      string
	Intent     string
	Chunks     []knowledge.SearchChunk
	Confidence float64
}

type capabilityBucket struct {
	keywords []string
	title    string
	intent   string
}

var capabilityBuckets = map[string]capabilityBucket{
	"ingestion": {
		keywords: []string{"scan", "crawl", "extract", "parse", "discover"},
		title:    "Source Ingestion",
		intent:   "Collect and normalize source code units into analysis-ready artifacts.",
	},
	"resolution": {
		keywords: []string{"resolve", "link", "relation", "dependency", "graph"},
		title:    "Symbol Resolution",
		intent:   "Link unresolved relations into stable symbol-level dependencies.",
	},
	"retrieval": {
		keywords: []string{"search", "retrieve", "query", "index", "embed", "vector"},
		title:    "Semantic Retrieval",
		intent:   "Retrieve the most relevant code evidence for documentation sections.",
	},
	"planning": {
		keywords: []string{"plan", "impact", "route", "section", "scope"},
		title:    "Section Planning",
		intent:   "Prioritize which documentation sections should be updated first.",
	},
	"generation": {
		keywords: []string{"generate", "render", "markdown", "document", "summarize", "update"},
		title:    "Documentation Generation",
		intent:   "Generate and maintain the document model and markdown outputs.",
	},
	"runtime": {
		keywords: []string{"config", "setup", "init", "load", "store", "db", "sqlite", "cli"},
		title:    "Runtime Configuration",
		intent:   "Configure execution environment, storage, and command workflows.",
	},
	"quality": {
		keywords: []string{"validate", "schema", "test", "assert", "normalize"},
		title:    "Quality and Validation",
		intent:   "Guarantee structural consistency and quality constraints of outputs.",
	},
}

func ExtractCapabilities(chunks []knowledge.SearchChunk, maxCaps int) []Capability {
	if len(chunks) == 0 || maxCaps == 0 {
		return nil
	}
	if maxCaps < 0 {
		maxCaps = 0
	}

	cluster := make(map[string][]knowledge.SearchChunk)
	for _, c := range chunks {
		if !isCapabilityCandidate(c) {
			continue
		}
		key := classifyCapability(c)
		cluster[key] = append(cluster[key], c)
	}

	out := make([]Capability, 0, len(cluster))
	for key, grouped := range cluster {
		if len(grouped) == 0 {
			continue
		}
		sort.Slice(grouped, func(i, j int) bool {
			if grouped[i].Package == grouped[j].Package {
				return grouped[i].Name < grouped[j].Name
			}
			return grouped[i].Package < grouped[j].Package
		})
		if len(grouped) > 6 {
			grouped = grouped[:6]
		}
		title, intent := capabilityTitleIntent(key)
		out = append(out, Capability{
			Key:        key,
			Title:      title,
			Intent:     intent,
			Chunks:     grouped,
			Confidence: capabilityConfidence(grouped),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence == out[j].Confidence {
			return out[i].Title < out[j].Title
		}
		return out[i].Confidence > out[j].Confidence
	})

	if maxCaps > 0 && len(out) > maxCaps {
		out = out[:maxCaps]
	}
	return out
}

func classifyCapability(c knowledge.SearchChunk) string {
	text := strings.ToLower(strings.Join([]string{
		c.Name, c.UnitType, c.Package, c.Description, c.Signature,
	}, " "))

	bestKey := "core"
	bestScore := 0
	for key, bucket := range capabilityBuckets {
		score := 0
		for _, token := range bucket.keywords {
			if strings.Contains(text, token) {
				score += 2
			}
		}
		if score > bestScore {
			bestScore = score
			bestKey = key
		}
	}
	return bestKey
}

func capabilityTitleIntent(key string) (string, string) {
	if bucket, ok := capabilityBuckets[key]; ok {
		return bucket.title, bucket.intent
	}
	return "Core Processing", "Implement the project's core behavior and domain logic."
}

func capabilityConfidence(chunks []knowledge.SearchChunk) float64 {
	if len(chunks) == 0 {
		return 0
	}
	pkgs := map[string]bool{}
	types := map[string]bool{}
	for _, c := range chunks {
		if strings.TrimSpace(c.Package) != "" {
			pkgs[c.Package] = true
		}
		types[c.UnitType] = true
	}
	score := 0.18*float64(len(chunks)) + 0.14*float64(len(pkgs)) + 0.1*float64(len(types))
	if score > 1 {
		return 1
	}
	return score
}

func isCapabilityCandidate(c knowledge.SearchChunk) bool {
	name := strings.ToLower(strings.TrimSpace(c.Name))
	if name == "" {
		return false
	}
	if strings.Contains(name, "_test") || strings.HasSuffix(name, "test") {
		return false
	}
	switch c.UnitType {
	case "file_module", "constant", "variable":
		return false
	}
	return true
}

func BuildKeyFeaturesSection(capabilities []Capability) string {
	var sb strings.Builder
	sb.WriteString("# Key Features\n\n")
	if len(capabilities) == 0 {
		sb.WriteString("No capability-level evidence was found in the current index scope.\n")
		return sb.String()
	}

	for _, cap := range capabilities {
		sb.WriteString(fmt.Sprintf("## %s\n\n", cap.Title))
		sb.WriteString(fmt.Sprintf("- **Intent**: %s\n", cap.Intent))
		sb.WriteString("- **Behavior**:\n")
		for _, line := range capabilityBehaviors(cap.Chunks) {
			sb.WriteString(fmt.Sprintf("  - %s\n", line))
		}
		sb.WriteString("- **Usage**:\n\n")
		sb.WriteString("```go\n")
		sb.WriteString(truncate(capabilitySnippet(cap.Chunks), 600))
		sb.WriteString("\n```\n\n")
	}
	return sb.String()
}

func capabilityBehaviors(chunks []knowledge.SearchChunk) []string {
	out := make([]string, 0, 3)
	for _, c := range chunks {
		if len(out) >= 3 {
			break
		}
		desc := strings.TrimSpace(c.Description)
		if desc == "" {
			continue
		}
		desc = strings.ReplaceAll(desc, "\n", " ")
		out = append(out, desc)
	}
	if len(out) == 0 {
		out = append(out, "Implements behavior derived from graph-linked source evidence.")
	}
	return out
}

func capabilitySnippet(chunks []knowledge.SearchChunk) string {
	for _, c := range chunks {
		if strings.TrimSpace(c.Content) != "" {
			return strings.TrimSpace(c.Content)
		}
		if strings.TrimSpace(c.Signature) != "" {
			return strings.TrimSpace(c.Signature)
		}
	}
	return "// No code snippet available from current evidence."
}

func AverageCapabilityConfidence(caps []Capability) float64 {
	if len(caps) == 0 {
		return 0
	}
	sum := 0.0
	for _, c := range caps {
		sum += c.Confidence
	}
	return sum / float64(len(caps))
}
