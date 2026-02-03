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

func (pb *PromptBuilder) BuildUpdateDocPrompt(currentContent string, relevantCode []SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("Role: Technical Writer. Task: Update an existing documentation section based on code changes.\n")
	sb.WriteString(securityInstruction)
	
	sb.WriteString("\n\n=== EXISTING DOCUMENTATION SECTION ===\n")
	sb.WriteString(currentContent)
	sb.WriteString("\n\n=== RELEVANT CODE CHANGES (CONTEXT) ===\n")
	
	for _, c := range relevantCode {
		fmt.Fprintf(&sb, "File: %s\nSymbol: %s\nDescription: %s\nCode:\n```go\n%s\n```\n\n", c.Name, c.Name, c.Description, c.Content)
	}

	sb.WriteString("\n**INSTRUCTION**:\n")
	sb.WriteString("1. Analyze the 'RELEVANT CODE CHANGES' and identify discrepancies with the 'EXISTING DOCUMENTATION SECTION'.\n")
	sb.WriteString("2. **CLEANUP**: If a symbol (function, type, etc.) mentioned in the documentation is NOT present in the provided code context, assume it has been deleted and REMOVE it from the documentation.\n")
	sb.WriteString("3. **UPDATE**: Rewrite the documentation section to reflect the code changes accurately.\n")
	sb.WriteString("4. **KEY FEATURE DETECTION**: If the code changes introduce a significant NEW feature that is NOT covered by the existing text, DO NOT just rewrite. Instead, **append a new subsection** (e.g., `### New Feature Name`) at the end of the existing content.\n")
	sb.WriteString("5. Preserve the original structure, tone, and formatting unless they are incorrect.\n")
	sb.WriteString("6. **OUTPUT ONLY the updated Markdown content** for this section. Do not include introductory text.\n")

	return sb.String()
}

func (pb *PromptBuilder) BuildNewSectionPrompt(relevantCode []SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("Role: Technical Writer. Task: Write a new documentation section for a new feature.\n")
	sb.WriteString(securityInstruction)

	sb.WriteString("\n\n=== NEW FEATURE CODE CONTEXT ===\n")
	for _, c := range relevantCode {
		fmt.Fprintf(&sb, "File: %s\nDescription: %s\nCode:\n```go\n%s\n```\n\n", c.Name, c.Description, c.Content)
	}

	sb.WriteString("\n**INSTRUCTION**:\n")
	sb.WriteString("1. Identify the core functionality of this new code.\n")
	sb.WriteString("2. Write a concise but comprehensive documentation section.\n")
	sb.WriteString("3. Use a clear header format (e.g. `## Feature Name`).\n")
	sb.WriteString("4. Include: **Concept** (What is it?), **Usage** (How to use it?), and **Example** (Code snippet).\n")
	sb.WriteString("5. **Tone**: Maintain a professional, objective, and technical tone consistent with standard software documentation. Avoid conversational filler.\n")
	sb.WriteString("6. **OUTPUT ONLY the Markdown content**.\n")

	return sb.String()
}

func (pb *PromptBuilder) BuildInsertionPointPrompt(toc []string, newContent string) string {
	var sb strings.Builder
	sb.WriteString("Role: Technical Editor. Task: Determine the best insertion point for a new section in existing documentation.\n")
	
	sb.WriteString("\n=== EXISTING TABLE OF CONTENTS ===\n")
	for i, title := range toc {
		fmt.Fprintf(&sb, "%d. %s\n", i, title)
	}

	sb.WriteString("\n=== NEW SECTION CONTENT (PREVIEW) ===\n")
	// Only show first few lines to save tokens
	lines := strings.Split(newContent, "\n")
	if len(lines) > 10 {
		sb.WriteString(strings.Join(lines[:10], "\n"))
		sb.WriteString("\n...")
	} else {
		sb.WriteString(newContent)
	}

	sb.WriteString("\n\n**INSTRUCTION**:\n")
	sb.WriteString("1. Analyze the context of the 'EXISTING TABLE OF CONTENTS'.\n")
	sb.WriteString("2. Determine where the 'NEW SECTION' logically belongs.\n")
	sb.WriteString("3. It should be placed after a section that is semantically related (e.g., if it's a new feature, put it after other features).\n")
	sb.WriteString("4. **OUTPUT ONLY the index number** of the section that the new content should follow (insert IMMEDIATELY AFTER this index).\n")
	sb.WriteString("5. If it should go at the very end, output the last index.\n")
	sb.WriteString("6. If it should go at the very beginning, output -1.\n")

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
