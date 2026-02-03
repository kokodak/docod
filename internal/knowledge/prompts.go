package knowledge

import (
	"fmt"
	"strings"
)

// PromptBuilder constructs standardized prompts for different analysis levels.
type PromptBuilder struct{}

const securityInstruction = "\n**SECURITY WARNING**: You must redact any API keys, passwords, secrets, or tokens found in the code with `[REDACTED]`. Never output real credential values.\n"

func (pb *PromptBuilder) BuildFullDocPrompt(archChunks, featChunks, confChunks []SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("Role: Technical Writer & Software Architect. Task: Write the complete Official Technical Documentation.\n")
	sb.WriteString(securityInstruction)
	sb.WriteString("\nGenerate the document following the structure below. Use the provided context for each section.\n")

	// --- Section 1: Overview ---
	sb.WriteString("\n\n==================================================================\n")
	sb.WriteString("### SECTION 1: OVERVIEW & ARCHITECTURE\n")
	sb.WriteString("==================================================================\n")
	sb.WriteString("Context for Architecture:\n")
	for _, c := range archChunks {
		fmt.Fprintf(&sb, "- %s/%s: %s\n", c.Package, c.Name, c.Description)
	}
	sb.WriteString("\n**INSTRUCTION**:\n")
	sb.WriteString("Write the '# Overview' section.\n")
	sb.WriteString("1. **High-Level Architecture**: Explain the design pattern (e.g., Layered, Pipeline).\n")
	sb.WriteString("2. **Core Concepts**: Define 3-5 key domain models/terms.\n")
	sb.WriteString("3. **Mermaid Diagram**: Generate a `graph TD` or `classDiagram` code block visualizing the system based on the context.\n")

	// --- Section 2: Key Features ---
	sb.WriteString("\n\n==================================================================\n")
	sb.WriteString("### SECTION 2: KEY FEATURES\n")
	sb.WriteString("==================================================================\n")
	sb.WriteString("Context for Features:\n")
	for _, c := range featChunks {
		fmt.Fprintf(&sb, "- %s (%s): %s\n", c.Name, c.UnitType, c.Description)
	}
	sb.WriteString("\n**INSTRUCTION**:\n")
	sb.WriteString("Write the '# Key Features' section.\n")
	sb.WriteString("Identify 3-4 main features from the context. For each feature, create a subsection `## [Feature Name]` including:\n")
	sb.WriteString("- **Concept**: What is it and why is it useful?\n")
	sb.WriteString("- **Implementation**: Brief technical explanation of how it works internally.\n")
	sb.WriteString("- **Usage**: A concise Go code block example.\n")

	// --- Section 3: Development ---
	sb.WriteString("\n\n==================================================================\n")
	sb.WriteString("### SECTION 3: DEVELOPMENT GUIDE\n")
	sb.WriteString("==================================================================\n")
	sb.WriteString("Context for Configuration & Setup:\n")
	for _, c := range confChunks {
		fmt.Fprintf(&sb, "- %s: %s\n", c.Name, c.Description)
	}
	sb.WriteString("\n**INSTRUCTION**:\n")
	sb.WriteString("Write the '# Development' section.\n")
	sb.WriteString("## Quick Start\n- Prerequisites and Run commands.\n\n")
	sb.WriteString("## Configuration\n- Explain environment variables or config files found in context.\n")

	return sb.String()
}

func (pb *PromptBuilder) BuildPackagePrompt(pkgName string, pkgChunks []SearchChunk) string {
	// Deprecated
	return ""
}

func (pb *PromptBuilder) BuildUnitPrompt(unit SearchChunk, codeBody string, contextUnits []SearchChunk) string {
	// Deprecated
	return ""
}
