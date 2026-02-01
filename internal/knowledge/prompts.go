package knowledge

import (
	"fmt"
	"strings"
)

// PromptBuilder constructs standardized prompts for different analysis levels.
type PromptBuilder struct{}

func (pb *PromptBuilder) BuildProjectPrompt(allChunks []SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("Role: Technical Writer. Task: Summarize project architecture.\n\n")
	sb.WriteString("### COMPONENTS ###\n")
	for _, c := range allChunks {
		fmt.Fprintf(&sb, "- %s/%s: %s\n", c.Package, c.Name, c.Description)
	}
	sb.WriteString("\n### OUTPUT FORMAT ###\n")
	sb.WriteString("- **Architecture**: [One sentence design pattern]\n")
	sb.WriteString("- **Core Concepts**: [3-5 bullet points of key domain models]\n")
	sb.WriteString("- **Data Flow**: [Brief step-by-step of main data path]\n")
	sb.WriteString("\nKeep it brief and factual.")
	return sb.String()
}

func (pb *PromptBuilder) BuildPackagePrompt(pkgName string, pkgChunks []SearchChunk) string {
	// Deprecated in favor of Feature/Unit prompts, but kept for interface compatibility
	return ""
}

func (pb *PromptBuilder) BuildUnitPrompt(unit SearchChunk, codeBody string, contextUnits []SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("Role: Senior Engineer. Task: Document this module.\n\n")
	
	sb.WriteString("### MODULE: " + unit.Name + " ###\n")
	sb.WriteString(unit.ToEmbeddableText()) 
	
	if len(contextUnits) > 0 {
		sb.WriteString("\n### CONTEXT ###\n")
		for _, ctx := range contextUnits {
			fmt.Fprintf(&sb, "- %s: %s\n", ctx.Name, ctx.Description)
		}
	}
	
	sb.WriteString("\n### OUTPUT FORMAT ###\n")
	sb.WriteString("1. **Responsibility**: Single sentence summary.\n")
	sb.WriteString("2. **Key Components**: List main structs/interfaces and their specific purpose.\n")
	sb.WriteString("3. **Usage Example**: \n```go\n// Pseudo-code or inferred usage\n```\n")
	sb.WriteString("\nStyle: Concise Markdown. No intro/outro.")
	return sb.String()
}

func (pb *PromptBuilder) BuildFeatureListPrompt(allChunks []SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("Identify 3-5 **Key Features** from the component list.\n\n")
	sb.WriteString("### COMPONENTS ###\n")
	for _, c := range allChunks {
		fmt.Fprintf(&sb, "- %s (%s): %s\n", c.Name, c.Package, c.Description)
	}
	sb.WriteString("\n### OUTPUT FORMAT ###\n")
	sb.WriteString("List features as bullet points:\n")
	sb.WriteString("* **[Feature Name]**: [One sentence description] (Implemented by: `ComponentA`, `ComponentB`)\n")
	return sb.String()
}

func (pb *PromptBuilder) BuildGettingStartedPrompt(allChunks []SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("Write a **Getting Started** guide.\n\n")
	sb.WriteString("### HINTS ###\n")
	for _, c := range allChunks {
		if c.Package == "main" || strings.Contains(c.Name, "Config") {
			fmt.Fprintf(&sb, "- %s: %s\n", c.Name, c.Description)
		}
	}
	sb.WriteString("\n### OUTPUT FORMAT ###\n")
	sb.WriteString("1. **Prerequisites**: List required tools.\n")
	sb.WriteString("2. **Configuration**: Env vars or config files needed.\n")
	sb.WriteString("3. **Run**: Command line example.\n")
	sb.WriteString("\nStyle: Command-line focused. Minimal text.")
	return sb.String()
}


