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
	sb.WriteString("Role: Senior Technical Writer. Task: Write official product-grade technical documentation.\n")
	sb.WriteString(securityInstruction)
	sb.WriteString("\nGenerate an official document for users and maintainers.\n")
	sb.WriteString("Focus on intent, behavior, contracts, constraints, and usage patterns.\n")
	sb.WriteString("Do NOT include low-level call graph narration like 'used by', 'called from', or exhaustive symbol dependency dumps.\n")
	sb.WriteString("Use diagrams/examples only when they improve understanding.\n")

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
	sb.WriteString("1. Explain system purpose and architectural boundaries.\n")
	sb.WriteString("2. Define 3-5 core concepts and why they matter.\n")
	sb.WriteString("3. Include one concise Mermaid diagram (`graph TD` or `classDiagram`) for conceptual understanding.\n")

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
	sb.WriteString("Identify 3-4 major capabilities. For each, create `## [Feature Name]` with:\n")
	sb.WriteString("- **Concept**: user-visible value and intent\n")
	sb.WriteString("- **Behavior**: important semantics, constraints, edge cases\n")
	sb.WriteString("- **Usage**: concise Go example when helpful\n")
	sb.WriteString("Avoid mechanical symbol-by-symbol explanations.\n")

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
	sb.WriteString("Include `## Quick Start` (prerequisites, run/test commands) and `## Configuration` (env vars/config files and effects).\n")
	sb.WriteString("Prefer actionable guidance over implementation trivia.\n")

	return sb.String()
}

func (pb *PromptBuilder) BuildUpdateDocPrompt(currentContent string, relevantCode []SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("Role: Technical Writer. Task: Update exactly one existing documentation section based on code changes.\n")
	sb.WriteString(securityInstruction)

	sb.WriteString("\n\n=== EXISTING DOCUMENTATION SECTION ===\n")
	sb.WriteString(currentContent)
	sb.WriteString("\n\n=== RELEVANT CODE CHANGES (CONTEXT) ===\n")

	for _, c := range relevantCode {
		path := c.FilePath
		if strings.TrimSpace(path) == "" {
			path = c.ID
		}
		fmt.Fprintf(&sb, "Source: %s\nSymbol: %s (%s)\nPackage: %s\nSignature: %s\nDescription: %s\nCode:\n```go\n%s\n```\n\n",
			path,
			c.Name,
			c.UnitType,
			c.Package,
			c.Signature,
			c.Description,
			c.Content,
		)
	}

	sb.WriteString("\n**INSTRUCTION**:\n")
	sb.WriteString("1. Keep scope strictly within this section. Do NOT rewrite the whole document.\n")
	sb.WriteString("2. Preserve the section heading level and title. Keep heading hierarchy valid.\n")
	sb.WriteString("3. Remove stale statements that are contradicted by provided code changes.\n")
	sb.WriteString("4. Prioritize semantic explanation (intent, behavior, constraints) over raw code structure.\n")
	sb.WriteString("5. If new behavior exists, append a `###` subsection inside this section.\n")
	sb.WriteString("6. Do NOT include call-chain chatter such as 'this method is used by X'.\n")
	sb.WriteString("7. Do NOT describe section content as a file-by-file walkthrough.\n")
	sb.WriteString("8. Do NOT use source file paths or package paths as subsection titles.\n")
	sb.WriteString("9. Replace placeholders completely; never output instructional text (e.g., 'Explain...', 'Describe...', 'Write...').\n")
	sb.WriteString("10. In `# Key Features`, produce 3-5 semantic capabilities, not symbol/file listings.\n")
	sb.WriteString("11. In `# Overview`, include exactly one Mermaid `graph LR` with meaningful stage labels.\n")
	sb.WriteString("12. Avoid speculation and duplicated headings.\n")
	sb.WriteString("13. OUTPUT ONLY markdown for this single section.\n")

	return sb.String()
}

func (pb *PromptBuilder) BuildNewSectionPrompt(relevantCode []SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("Role: Technical Writer. Task: Write one concise documentation section for incremental code changes.\n")
	sb.WriteString(securityInstruction)

	sb.WriteString("\n\n=== NEW FEATURE CODE CONTEXT ===\n")
	for _, c := range relevantCode {
		fmt.Fprintf(&sb, "File: %s\nDescription: %s\nCode:\n```go\n%s\n```\n\n", c.Name, c.Description, c.Content)
	}

	sb.WriteString("\n**INSTRUCTION**:\n")
	sb.WriteString("1. Write exactly one `##` section.\n")
	sb.WriteString("2. Include only facts supported by the provided code context.\n")
	sb.WriteString("3. Keep it compact: concept, behavior, usage/example.\n")
	sb.WriteString("4. Avoid call graph narration and exhaustive symbol lists.\n")
	sb.WriteString("5. Tone: objective and technical.\n")
	sb.WriteString("6. OUTPUT ONLY markdown for this new section.\n")

	return sb.String()
}

func (pb *PromptBuilder) BuildInsertionPointPrompt(toc []string, newContent string) string {
	var sb strings.Builder
	sb.WriteString("Role: Technical Editor. Task: Determine the best target section index for incremental documentation placement.\n")

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
	sb.WriteString("2. Decide which existing section is most semantically related to the preview content.\n")
	sb.WriteString("3. Return the index of that section as an insertion-after index.\n")
	sb.WriteString("4. **OUTPUT ONLY one integer**.\n")
	sb.WriteString("5. If it belongs before the first section, output -1.\n")
	sb.WriteString("6. Do not output prose, markdown, or multiple numbers.\n")

	return sb.String()
}

func (pb *PromptBuilder) BuildRenderFromDraftPrompt(draftJSON string, relevantCode []SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("Role: Technical Documentation Renderer. Task: Render a polished markdown section from a structured draft.\n")
	sb.WriteString(securityInstruction)
	sb.WriteString("You MUST treat the draft as source-of-truth for claims.\n")
	sb.WriteString("Do NOT add claims not grounded in draft claims and code evidence.\n")
	sb.WriteString("Preserve section scope and heading intent.\n")

	sb.WriteString("\n\n=== SECTION DRAFT (JSON) ===\n")
	sb.WriteString(draftJSON)
	sb.WriteString("\n\n=== CODE EVIDENCE (SUPPORTING CONTEXT) ===\n")
	for _, c := range relevantCode {
		path := c.FilePath
		if strings.TrimSpace(path) == "" {
			path = c.ID
		}
		fmt.Fprintf(&sb, "Source: %s\nSymbol: %s (%s)\nPackage: %s\nDescription: %s\nSignature: %s\nCode:\n```go\n%s\n```\n\n",
			path, c.Name, c.UnitType, c.Package, c.Description, c.Signature, c.Content)
	}

	sb.WriteString("\n**INSTRUCTION**:\n")
	sb.WriteString("1. Output exactly one section in markdown.\n")
	sb.WriteString("2. Keep all draft claims, but rewrite for clarity and technical precision.\n")
	sb.WriteString("3. Do NOT invent new claims, APIs, or behavior.\n")
	sb.WriteString("4. Keep wording semantic and capability-oriented, not file-by-file narration.\n")
	sb.WriteString("5. Never use headings or bullets like 'module X.go', 'package Y', or symbol dump lists.\n")
	sb.WriteString("6. Explain behavior as capability + execution flow + constraints using concise prose paragraphs.\n")
	sb.WriteString("7. Include concrete technical anchors (function/type names in backticks) where relevant.\n")
	sb.WriteString("8. If a mermaid block exists in draft context, preserve one meaningful diagram.\n")
	sb.WriteString("9. Avoid placeholders, duplicated headings, and speculative language.\n")
	sb.WriteString("10. OUTPUT ONLY markdown.\n")

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
