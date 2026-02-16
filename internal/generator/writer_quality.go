package generator

import "strings"

type writerQuality struct {
	Score  float64
	Issues []string
}

func assessWriterQuality(sectionID, content string) writerQuality {
	text := strings.TrimSpace(content)
	if text == "" {
		return writerQuality{Score: 0, Issues: []string{"empty_content"}}
	}

	score := 1.0
	issues := make([]string, 0, 6)
	lines := strings.Split(text, "\n")
	total := 0
	bullets := 0
	paragraphs := 0
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		total++
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			bullets++
		}
		if !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "* ") {
			paragraphs++
		}
	}
	if total > 0 && float64(bullets)/float64(total) > 0.45 {
		score -= 0.25
		issues = append(issues, "list_heavy")
	}
	if paragraphs < 2 {
		score -= 0.2
		issues = append(issues, "insufficient_paragraphs")
	}

	lower := strings.ToLower(text)
	fileWalkthroughSignals := 0
	for _, token := range []string{"module `", ".go`", ".go ", "package `", "containing:"} {
		if strings.Contains(lower, token) {
			fileWalkthroughSignals++
		}
	}
	if fileWalkthroughSignals >= 2 {
		score -= 0.35
		issues = append(issues, "file_walkthrough_style")
	}

	placeholders := []string{
		"explain the", "describe the", "write ", "must include", "tbd", "placeholder",
	}
	for _, token := range placeholders {
		if strings.Contains(lower, token) {
			score -= 0.2
			issues = append(issues, "instructional_or_placeholder_text")
			break
		}
	}
	if sectionID == "overview" && !strings.Contains(lower, "```mermaid") {
		score -= 0.2
		issues = append(issues, "missing_overview_diagram")
	}
	if sectionID == "key-features" && strings.Count(lower, "\n## ") < 2 {
		score -= 0.2
		issues = append(issues, "insufficient_feature_sections")
	}
	if sectionID == "key-features" && !strings.Contains(lower, "`") {
		score -= 0.15
		issues = append(issues, "missing_technical_anchors")
	}
	if score < 0 {
		score = 0
	}
	return writerQuality{Score: score, Issues: issues}
}
