package generator

import "strings"

// FullDocPlan defines section-level contracts for full documentation generation.
type FullDocPlan struct {
	Sections []SectionDocPlan
}

// SectionDocPlan controls retrieval and writing constraints per section.
type SectionDocPlan struct {
	SectionID         string
	Title             string
	Goal              string
	RequiredBlocks    []string
	QueryHints        []string
	RetrievalKeywords []string
	TopK              int
	MinEvidence       int
	RequireMermaid    bool
	AllowLLM          bool
}

func BuildDefaultFullDocPlan() *FullDocPlan {
	return &FullDocPlan{Sections: []SectionDocPlan{
		{
			SectionID:         "overview",
			Title:             "Overview",
			Goal:              "Explain purpose, boundaries, and system-level flow in semantic terms.",
			RequiredBlocks:    []string{"Purpose", "End-to-End Flow", "Core Concepts"},
			QueryHints:        []string{"system architecture", "runtime flow", "core components", "boundaries"},
			RetrievalKeywords: []string{"architecture", "system", "component", "module", "entry", "flow", "interface"},
			TopK:              16,
			MinEvidence:       6,
			RequireMermaid:    true,
			AllowLLM:          false,
		},
		{
			SectionID:         "key-features",
			Title:             "Key Features",
			Goal:              "Describe capability-level behaviors, constraints, and usage without file walkthroughs.",
			RequiredBlocks:    []string{"Capability"},
			QueryHints:        []string{"core capabilities", "business behavior", "constraints", "workflows"},
			RetrievalKeywords: []string{"feature", "service", "workflow", "domain", "policy", "validation", "resolver"},
			TopK:              20,
			MinEvidence:       8,
			RequireMermaid:    false,
			AllowLLM:          true,
		},
		{
			SectionID:         "development",
			Title:             "Development",
			Goal:              "Provide setup, configuration, and operational guidance for maintainers.",
			RequiredBlocks:    []string{"Quick Start", "Configuration", "Architecture Snapshot"},
			QueryHints:        []string{"development setup", "configuration", "cli", "testing", "runtime"},
			RetrievalKeywords: []string{"config", "env", "cli", "command", "test", "build", "deploy"},
			TopK:              14,
			MinEvidence:       5,
			RequireMermaid:    true,
			AllowLLM:          false,
		},
	}}
}

func (p *FullDocPlan) SectionByID(id string) (SectionDocPlan, bool) {
	if p == nil {
		return SectionDocPlan{}, false
	}
	for _, s := range p.Sections {
		if s.SectionID == id {
			return s, true
		}
	}
	return SectionDocPlan{}, false
}

func (s SectionDocPlan) QueryText() string {
	if len(s.QueryHints) == 0 {
		return strings.TrimSpace(s.SectionID)
	}
	return strings.Join(s.QueryHints, " ")
}
