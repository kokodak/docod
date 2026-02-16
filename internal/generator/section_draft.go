package generator

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"docod/internal/knowledge"
)

// SectionDraft is a structured intermediate model for section rendering.
type SectionDraft struct {
	SectionID string       `json:"section_id"`
	Title     string       `json:"title"`
	Summary   string       `json:"summary,omitempty"`
	Claims    []DraftClaim `json:"claims"`
	Mermaid   string       `json:"mermaid,omitempty"`
}

// DraftClaim binds each statement to source references.
type DraftClaim struct {
	ID         string      `json:"id"`
	Text       string      `json:"text"`
	Sources    []SourceRef `json:"sources"`
	Confidence float64     `json:"confidence"`
}

func BuildSectionDraft(sectionID, title string, chunks []knowledge.SearchChunk, capabilities []Capability) SectionDraft {
	draft := SectionDraft{
		SectionID: sectionID,
		Title:     title,
		Claims:    []DraftClaim{},
	}

	switch sectionID {
	case "overview":
		claims := topNChunks(filterDraftSemanticChunks(chunks), 6)
		for i, c := range claims {
			text := normalizeClaimText(c)
			if text == "" {
				text = fmt.Sprintf("%s contributes to the core architecture behavior.", c.Name)
			}
			draft.Claims = append(draft.Claims, DraftClaim{
				ID:         fmt.Sprintf("ov-%d", i+1),
				Text:       text,
				Sources:    BuildSourcesFromChunk(c),
				Confidence: computeClaimConfidence(BuildSourcesFromChunk(c)),
			})
		}
	case "key-features":
		if len(capabilities) == 0 {
			capabilities = ExtractCapabilities(chunks, 5)
		}
		for i, cap := range capabilities {
			src := MergeSources(nil, cap.Chunks)
			behavior := capabilityBehaviors(cap.Chunks)
			text := fmt.Sprintf("%s: %s %s", cap.Title, cap.Intent, strings.Join(behavior, " "))
			draft.Claims = append(draft.Claims, DraftClaim{
				ID:         fmt.Sprintf("kf-%d", i+1),
				Text:       strings.TrimSpace(text),
				Sources:    src,
				Confidence: maxFloat(cap.Confidence, computeClaimConfidence(src)),
			})
		}
	case "development":
		configs := make([]knowledge.SearchChunk, 0)
		for _, c := range chunks {
			if c.UnitType == "constant" || c.UnitType == "variable" {
				configs = append(configs, c)
			}
		}
		if len(configs) == 0 {
			configs = topNChunks(filterDraftSemanticChunks(chunks), 5)
		}
		for i, c := range configs {
			text := normalizeClaimText(c)
			if text == "" {
				text = fmt.Sprintf("%s affects runtime setup or operational behavior.", c.Name)
			}
			draft.Claims = append(draft.Claims, DraftClaim{
				ID:         fmt.Sprintf("dev-%d", i+1),
				Text:       text,
				Sources:    BuildSourcesFromChunk(c),
				Confidence: computeClaimConfidence(BuildSourcesFromChunk(c)),
			})
		}
	default:
		for i, c := range topNChunks(filterDraftSemanticChunks(chunks), 4) {
			text := normalizeClaimText(c)
			if text == "" {
				text = fmt.Sprintf("%s is relevant to this section.", c.Name)
			}
			src := BuildSourcesFromChunk(c)
			draft.Claims = append(draft.Claims, DraftClaim{
				ID:         fmt.Sprintf("cl-%d", i+1),
				Text:       text,
				Sources:    src,
				Confidence: computeClaimConfidence(src),
			})
		}
	}
	draft.Summary = summarizeDraft(draft.Claims)
	return draft
}

func ValidateSectionDraft(d SectionDraft) error {
	if strings.TrimSpace(d.SectionID) == "" {
		return fmt.Errorf("section_id is required")
	}
	if strings.TrimSpace(d.Title) == "" {
		return fmt.Errorf("title is required")
	}
	if len(d.Claims) == 0 {
		return fmt.Errorf("claims must not be empty")
	}
	for _, c := range d.Claims {
		if strings.TrimSpace(c.Text) == "" {
			return fmt.Errorf("claim text is required")
		}
		if len(c.Sources) == 0 {
			return fmt.Errorf("claim must include sources")
		}
	}
	return nil
}

func RenderSectionDraftMarkdown(d SectionDraft) string {
	var sb strings.Builder
	sb.WriteString("# " + d.Title + "\n\n")
	if strings.TrimSpace(d.Summary) != "" {
		sb.WriteString(d.Summary + "\n\n")
	}

	switch d.SectionID {
	case "overview":
		sb.WriteString("## Architecture Intent\n\n")
		for _, c := range topClaimsByConfidence(d.Claims, 2) {
			sb.WriteString(toParagraph(c.Text) + "\n\n")
		}
		sb.WriteString("## Core Concepts\n\n")
		for _, c := range topClaimsByConfidence(d.Claims, 4) {
			sb.WriteString("- " + toSentence(c.Text) + "\n")
		}
		sb.WriteString("\n")
	case "key-features":
		for _, c := range d.Claims {
			head := claimHeading(c.Text)
			sb.WriteString("## " + head + "\n\n")
			sb.WriteString(toParagraph(c.Text) + "\n\n")
		}
	case "development":
		sb.WriteString("## Developer Workflow\n\n")
		for _, c := range topClaimsByConfidence(d.Claims, 3) {
			sb.WriteString(toParagraph(c.Text) + "\n\n")
		}
		sb.WriteString("## Operational Notes\n\n")
		for _, c := range topClaimsByConfidence(d.Claims, 4) {
			sb.WriteString("- " + toSentence(c.Text) + "\n")
		}
		sb.WriteString("\n")
	default:
		sb.WriteString("## Highlights\n\n")
		for _, c := range topClaimsByConfidence(d.Claims, 5) {
			sb.WriteString(toParagraph(c.Text) + "\n\n")
		}
	}
	return sb.String()
}

func summarizeDraft(claims []DraftClaim) string {
	if len(claims) == 0 {
		return ""
	}
	sort.SliceStable(claims, func(i, j int) bool { return claims[i].Confidence > claims[j].Confidence })
	if len(claims) > 2 {
		claims = claims[:2]
	}
	parts := make([]string, 0, len(claims))
	for _, c := range claims {
		parts = append(parts, c.Text)
	}
	return strings.Join(parts, " ")
}

func computeClaimConfidence(src []SourceRef) float64 {
	if len(src) == 0 {
		return 0
	}
	sum := 0.0
	n := 0.0
	for _, s := range src {
		if s.Confidence <= 0 {
			continue
		}
		sum += s.Confidence
		n++
	}
	if n == 0 {
		return 0.6
	}
	conf := sum / n
	if conf > 1 {
		return 1
	}
	if conf < 0 {
		return 0
	}
	return conf
}

func claimHeading(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "Capability"
	}
	if len(text) > 68 {
		text = text[:68]
	}
	text = strings.TrimSpace(strings.Trim(text, "."))
	parts := strings.SplitN(text, ":", 2)
	head := strings.TrimSpace(parts[0])
	if head == "" {
		return "Capability"
	}
	return head
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func topClaimsByConfidence(claims []DraftClaim, n int) []DraftClaim {
	if n <= 0 || len(claims) == 0 {
		return nil
	}
	cp := append([]DraftClaim(nil), claims...)
	sort.SliceStable(cp, func(i, j int) bool {
		if cp[i].Confidence == cp[j].Confidence {
			return cp[i].ID < cp[j].ID
		}
		return cp[i].Confidence > cp[j].Confidence
	})
	if len(cp) > n {
		cp = cp[:n]
	}
	return cp
}

func toParagraph(text string) string {
	line := strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if line == "" {
		return "This behavior is grounded in source-linked evidence."
	}
	if !strings.HasSuffix(line, ".") {
		line += "."
	}
	return line
}

func toSentence(text string) string {
	return toParagraph(text)
}

func SerializeSectionDraft(d SectionDraft) string {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

func filterDraftSemanticChunks(chunks []knowledge.SearchChunk) []knowledge.SearchChunk {
	out := make([]knowledge.SearchChunk, 0, len(chunks))
	for _, c := range chunks {
		if c.UnitType == "file_module" || c.UnitType == "symbol_segment" {
			continue
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return chunks
	}
	return out
}

func normalizeClaimText(c knowledge.SearchChunk) string {
	text := strings.TrimSpace(c.Description)
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "module `") && strings.Contains(lower, "containing:") {
		return fmt.Sprintf("%s in package `%s` provides behavior relevant to this section.", c.Name, c.Package)
	}
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) > 280 {
		text = strings.TrimSpace(text[:280]) + "..."
	}
	return text
}
