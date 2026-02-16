package generator

import (
	"context"
	"docod/internal/knowledge"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// MarkdownGenerator produces documentation in Markdown format.
type MarkdownGenerator struct {
	engine     *knowledge.Engine
	summarizer knowledge.Summarizer
	mermaid    *MermaidGenerator
}

type sectionEvidencePack struct {
	Queries []string
	Chunks  []knowledge.SearchChunk
	Stats   *EvidenceRef
	SearchHits    int
	HeuristicHits int
}

type sectionGenerationTrace struct {
	UsedDraft    bool
	UsedLLM      bool
	UsedFallback bool
}

func NewMarkdownGenerator(e *knowledge.Engine, s knowledge.Summarizer) *MarkdownGenerator {
	return &MarkdownGenerator{
		engine:     e,
		summarizer: s,
		mermaid:    &MermaidGenerator{},
	}
}

// GenerateDocs builds docs from KG/index retrieval and writes model + markdown.
func (g *MarkdownGenerator) GenerateDocs(ctx context.Context, outputDir string) error {
	report := NewPipelineReport("full_generate", outputDir)
	return g.GenerateDocsWithReport(ctx, outputDir, report)
}

// GenerateDocsWithReport builds docs and writes stage metrics to pipeline_report.json.
func (g *MarkdownGenerator) GenerateDocsWithReport(ctx context.Context, outputDir string, report *PipelineReport) (retErr error) {
	if report == nil {
		report = NewPipelineReport("full_generate", outputDir)
	}
	reportPath := filepath.Join(outputDir, "pipeline_report.json")
	defer func() {
		if retErr != nil {
			report.AddSignal("full_generate_failed", "generator", "critical", "Full documentation generation failed.", 1)
		}
		if err := report.Save(reportPath); err != nil {
			fmt.Printf("⚠️  Failed to write pipeline report: %v\n", err)
		}
	}()

	stage := report.BeginStage("init_output_dir")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		report.EndStage(stage, "error", nil, nil, err)
		return err
	}
	report.EndStage(stage, "ok", nil, nil, nil)

	now := time.Now().UTC().Format(time.RFC3339)
	fmt.Println("🔍 Preparing KG chunks for full generate...")
	stage = report.BeginStage("prepare_chunks")
	allChunks := g.engine.PrepareSearchChunks()
	report.EndStage(stage, "ok", map[string]float64{
		"prepared_chunks_total": float64(len(allChunks)),
	}, nil, nil)
	if len(allChunks) == 0 {
		report.AddSignal("no_chunks_prepared", "prepare_chunks", "critical", "No searchable chunks were prepared from the graph.", 0)
	}
	if len(allChunks) == 0 {
		fmt.Println("⚠️  No searchable chunks found. Generating skeletal documentation.")
	}

	model := g.buildSchemaScaffoldModel(now)
	fullPlan := BuildDefaultFullDocPlan()
	llmBudget := 1
	keyFeaturePlan, _ := fullPlan.SectionByID("key-features")
	if strings.TrimSpace(keyFeaturePlan.SectionID) == "" {
		keyFeaturePlan = SectionDocPlan{
			SectionID:         "key-features",
			QueryHints:        []string{"core capabilities", "business behavior", "constraints"},
			RetrievalKeywords: []string{"feature", "service", "workflow", "domain"},
			TopK:              20,
			MinEvidence:       8,
			AllowLLM:          true,
		}
	}
	keyFeatureSeed := g.selectSectionEvidence(ctx, keyFeaturePlan, allChunks, nil)
	globalCapabilities := ExtractCapabilities(keyFeatureSeed.Chunks, 6)
	for i := range model.Sections {
		sec := &model.Sections[i]
		sectionStage := report.BeginStage("section_" + sec.ID)
		secPlan, ok := fullPlan.SectionByID(sec.ID)
		if !ok {
			secPlan = fallbackSectionPlan(*sec)
		}
		secCaps := []Capability(nil)
		if sec.ID == "key-features" {
			secCaps = globalCapabilities
		}
		pack := g.selectSectionEvidence(ctx, secPlan, allChunks, secCaps)
		sectionChunks := pack.Chunks
		if sec.ID == "key-features" && len(secCaps) == 0 {
			secCaps = ExtractCapabilities(sectionChunks, 6)
		}
		content, trace := g.generateSectionContent(ctx, *sec, secPlan, sectionChunks, secCaps, &llmBudget)
		if pack.Stats != nil && pack.Stats.LowEvidence {
			content = applyLowEvidencePolicy(content)
			report.AddSignal("low_evidence_section", "section_"+sec.ID, "warning", "Section evidence is below required threshold.", pack.Stats.Confidence)
		}
		if pack.SearchHits == 0 {
			report.AddSignal("semantic_hits_zero", "section_"+sec.ID, "warning", "Semantic retrieval returned zero hits; section relied on heuristic evidence.", 0)
		}
		if len(sectionChunks) > 0 {
			heuristicShare := float64(pack.HeuristicHits) / float64(len(sectionChunks))
			if heuristicShare >= 0.8 {
				report.AddSignal("heuristic_dominant", "section_"+sec.ID, "warning", "Heuristic retrieval dominates section evidence selection.", heuristicShare)
			}
		}
		wq := assessWriterQuality(sec.ID, content)
		if wq.Score < 0.55 {
			report.AddSignal("writer_quality_low", "section_"+sec.ID, "warning", "Writer quality score is below target threshold.", wq.Score)
		}
		sec.ContentMD = strings.TrimSpace(content)
		sec.Sources = MergeSources(nil, sectionChunks)
		sec.Evidence = pack.Stats
		sec.Summary = summarizeContent(sec.ContentMD)
		sec.LastUpdated = &UpdateInfo{CommitSHA: "HEAD", Timestamp: now}
		sec.Hash = sectionHash(*sec)
		sourceCount := len(sec.Sources)
		chunkCount := len(sectionChunks)
		confidence := 0.0
		coverage := 0.0
		lowEvidence := false
		if pack.Stats != nil {
			confidence = pack.Stats.Confidence
			coverage = pack.Stats.Coverage
			lowEvidence = pack.Stats.LowEvidence
		}
		report.AddSectionMetric(SectionMetric{
			SectionID:           sec.ID,
			Title:               sec.Title,
			QueryCount:          len(pack.Queries),
			SearchHits:          pack.SearchHits,
			HeuristicHits:       pack.HeuristicHits,
			ChunkCount:          chunkCount,
			SourceCount:         sourceCount,
			FileDiversity:       uniqueFileCount(sectionChunks),
			EvidenceConfidence:  confidence,
			EvidenceCoverage:    coverage,
			LowEvidence:         lowEvidence,
			WriterQualityScore:  wq.Score,
			WriterQualityIssues: wq.Issues,
			UsedDraft:           trace.UsedDraft,
			UsedLLM:             trace.UsedLLM,
			UsedFallback:        trace.UsedFallback,
		})
		report.EndStage(sectionStage, "ok", map[string]float64{
			"queries":        float64(len(pack.Queries)),
			"search_hits":    float64(pack.SearchHits),
			"heuristic_hits": float64(pack.HeuristicHits),
			"selected_chunks": float64(chunkCount),
			"source_count":   float64(sourceCount),
			"file_diversity": float64(uniqueFileCount(sectionChunks)),
			"evidence_confidence": confidence,
			"writer_quality": wq.Score,
		}, nil, nil)
	}

	model.Meta.GeneratedAt = now
	NormalizeDocModel(model)

	modelPath := filepath.Join(outputDir, "doc_model.json")
	stage = report.BeginStage("save_doc_model")
	if err := SaveDocModel(modelPath, model); err != nil {
		report.EndStage(stage, "error", nil, nil, err)
		return fmt.Errorf("failed to save doc model: %w", err)
	}
	report.EndStage(stage, "ok", map[string]float64{
		"sections_total": float64(len(model.Sections)),
	}, nil, nil)

	path := filepath.Join(outputDir, "documentation.md")
	stage = report.BeginStage("render_markdown")
	rendered := RenderMarkdownFromModel(model)
	if err := os.WriteFile(path, []byte(rendered), 0644); err != nil {
		report.EndStage(stage, "error", nil, nil, err)
		return err
	}
	report.EndStage(stage, "ok", map[string]float64{
		"rendered_bytes": float64(len(rendered)),
	}, nil, nil)
	report.AddSignal("full_generate_complete", "generator", "info", "Full generation completed successfully.", 1)
	return nil
}

func (g *MarkdownGenerator) buildSchemaScaffoldModel(now string) *DocModel {
	sections := make([]ModelSect, 0, len(canonicalSectionOrder))
	for i, id := range canonicalSectionOrder {
		title := sectionTitleFromID(id)
		sec := ModelSect{
			ID:        id,
			Title:     title,
			Level:     1,
			Order:     i,
			ParentID:  nil,
			ContentMD: sectionScaffold(id, title),
			Summary:   "",
			Status:    "active",
			Sources:   []SourceRef{},
			LastUpdated: &UpdateInfo{
				CommitSHA: "HEAD",
				Timestamp: now,
			},
		}
		sec.Hash = sectionHash(sec)
		sections = append(sections, sec)
	}

	model := &DocModel{
		SchemaVersion: docModelSchemaVersion,
		Document: ModelDoc{
			ID:             "docod-main-doc",
			Title:          "Project Documentation",
			RootSectionIDs: append([]string(nil), canonicalSectionOrder...),
		},
		Sections: sections,
		Policies: ModelPolicy{
			RequiredSectionIDs: append([]string(nil), canonicalSectionOrder...),
			MaxSectionChars:    8000,
			Style: PolicyStyle{
				Tone:                       "technical, objective",
				Audience:                   "open-source maintainers",
				CodeBlockLanguage:          "go",
				FocusMode:                  "semantic",
				AvoidCallGraphNarration:    true,
				PreferConceptualDiagrams:   true,
				PreferTaskOrientedExamples: true,
			},
		},
		Meta: ModelMeta{
			Repo:             ".",
			DefaultBranch:    "main",
			GeneratedAt:      now,
			GeneratorVersion: "docod-dev",
		},
	}
	NormalizeDocModel(model)
	return model
}

func (g *MarkdownGenerator) selectSectionEvidence(ctx context.Context, secPlan SectionDocPlan, allChunks []knowledge.SearchChunk, capabilities []Capability) sectionEvidencePack {
	topK := secPlan.TopK
	if topK <= 0 {
		topK = 12
	}
	queries := BuildSectionQueries(secPlan, capabilities)
	if len(queries) == 0 {
		queries = []string{secPlan.SectionID}
	}

	perQueryTopK := topK
	if len(queries) > 1 {
		perQueryTopK = topK / len(queries)
		if perQueryTopK < 4 {
			perQueryTopK = 4
		}
		if perQueryTopK > topK {
			perQueryTopK = topK
		}
	}
	selected := make([]knowledge.SearchChunk, 0, topK*2)
	searchHits := 0
	for _, q := range queries {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		hits, err := g.engine.SearchByText(ctx, q, perQueryTopK, "")
		if err != nil {
			continue
		}
		searchHits += len(hits)
		selected = append(selected, hits...)
	}
	selected = mergeChunkLists(nil, selected, topK*2)
	selected = filterChunksForSection(secPlan.SectionID, selected)

	heuristicHits := 0
	if len(selected) < topK/2 {
		heuristic := heuristicSelectChunks(allChunks, secPlan.RetrievalKeywords, topK)
		heuristic = filterChunksForSection(secPlan.SectionID, heuristic)
		heuristicHits = len(heuristic)
		selected = mergeChunkLists(selected, heuristic, topK)
	}

	if len(selected) == 0 {
		selected = topNChunks(filterChunksForSection(secPlan.SectionID, allChunks), topK)
	}
	selected = DiversityRerank(selected, topK, 2)
	stats := buildEvidenceStats(secPlan, queries, selected)
	return sectionEvidencePack{
		Queries:       queries,
		Chunks:        selected,
		Stats:         stats,
		SearchHits:    searchHits,
		HeuristicHits: heuristicHits,
	}
}

func fallbackSectionPlan(sec ModelSect) SectionDocPlan {
	return SectionDocPlan{
		SectionID:         sec.ID,
		Title:             sec.Title,
		QueryHints:        []string{sec.Title, sec.ID},
		RetrievalKeywords: strings.Fields(strings.ToLower(sec.Title + " " + sec.ID)),
		TopK:              12,
		MinEvidence:       4,
	}
}

func heuristicSelectChunks(chunks []knowledge.SearchChunk, keywords []string, limit int) []knowledge.SearchChunk {
	if limit <= 0 || len(chunks) == 0 {
		return nil
	}
	kw := make([]string, 0, len(keywords))
	for _, k := range keywords {
		k = strings.TrimSpace(strings.ToLower(k))
		if k != "" {
			kw = append(kw, k)
		}
	}
	type scored struct {
		chunk knowledge.SearchChunk
		score int
	}
	ranked := make([]scored, 0, len(chunks))
	for _, c := range chunks {
		text := strings.ToLower(c.Name + "\n" + c.Description + "\n" + c.Signature + "\n" + c.Content)
		score := 0
		for _, token := range kw {
			if strings.Contains(text, token) {
				score += 3
			}
		}
		switch c.UnitType {
		case "function", "method", "struct", "interface", "file_module":
			score += 1
		}
		ranked = append(ranked, scored{chunk: c, score: score})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].chunk.ID < ranked[j].chunk.ID
		}
		return ranked[i].score > ranked[j].score
	})

	out := make([]knowledge.SearchChunk, 0, limit)
	for _, item := range ranked {
		if len(out) >= limit {
			break
		}
		if item.score <= 0 {
			continue
		}
		out = append(out, item.chunk)
	}
	if len(out) == 0 {
		for _, item := range ranked {
			if len(out) >= limit {
				break
			}
			out = append(out, item.chunk)
		}
	}
	return out
}

func mergeChunkLists(primary, secondary []knowledge.SearchChunk, limit int) []knowledge.SearchChunk {
	seen := make(map[string]bool, len(primary)+len(secondary))
	out := make([]knowledge.SearchChunk, 0, limit)
	for _, group := range [][]knowledge.SearchChunk{primary, secondary} {
		for _, c := range group {
			if c.ID == "" || seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			out = append(out, c)
			if limit > 0 && len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func (g *MarkdownGenerator) generateSectionContent(ctx context.Context, sec ModelSect, secPlan SectionDocPlan, chunks []knowledge.SearchChunk, capabilities []Capability, llmBudget *int) (string, sectionGenerationTrace) {
	trace := sectionGenerationTrace{}
	draft := BuildSectionDraft(sec.ID, sec.Title, chunks, capabilities)
	if err := ValidateSectionDraft(draft); err == nil {
		trace.UsedDraft = true
		content := RenderSectionDraftMarkdown(draft)
		if g.summarizer != nil {
			if refined, ok := g.tryRenderDraftWithLLM(ctx, draft, chunks); ok {
				content = refined
				trace.UsedLLM = true
			}
		}
		content = g.enrichSectionWithDiagrams(sec.ID, content, chunks)
		q := assessWriterQuality(sec.ID, content)
		if !isLowQualitySection(sec.ID, content) && q.Score >= 0.55 {
			return content, trace
		}
		if g.summarizer != nil && secPlan.AllowLLM && llmBudget != nil && *llmBudget > 0 {
			if refined, ok := g.tryLLMSectionRewrite(ctx, sec.ID, sec.Title, content, chunks); ok {
				*llmBudget--
				refined = g.enrichSectionWithDiagrams(sec.ID, refined, chunks)
				rq := assessWriterQuality(sec.ID, refined)
				if !isLowQualitySection(sec.ID, refined) && rq.Score >= 0.55 {
					trace.UsedLLM = true
					return refined, trace
				}
			}
		}
	}

	var content string
	switch sec.ID {
	case "overview":
		content = g.buildOverviewSection(chunks)
	case "key-features":
		if len(capabilities) == 0 {
			capabilities = ExtractCapabilities(chunks, 5)
		}
		content = BuildKeyFeaturesSection(capabilities)
		avgConf := AverageCapabilityConfidence(capabilities)
		needsSemanticLift := len(capabilities) < 3 || avgConf < 0.5
		if needsSemanticLift && secPlan.AllowLLM && llmBudget != nil && *llmBudget > 0 {
			if refined, ok := g.tryLLMSectionRewrite(ctx, sec.ID, sec.Title, content, chunks); ok {
				*llmBudget--
				content = refined
				trace.UsedLLM = true
			}
		}
	case "development":
		content = g.buildDevelopmentSection(chunks)
	default:
		content = g.buildFallbackSection(sec.ID, chunks)
	}
	content = g.enrichSectionWithDiagrams(sec.ID, content, chunks)
	q := assessWriterQuality(sec.ID, content)
	if isLowQualitySection(sec.ID, content) || q.Score < 0.45 {
		trace.UsedFallback = true
		return g.enrichSectionWithDiagrams(sec.ID, g.buildFallbackSection(sec.ID, chunks), chunks), trace
	}
	return content, trace
}

func (g *MarkdownGenerator) tryLLMSectionRewrite(ctx context.Context, sectionID, sectionTitle, seed string, chunks []knowledge.SearchChunk) (string, bool) {
	if g.summarizer == nil {
		return "", false
	}
	promptSeed := strings.TrimSpace(seed)
	if promptSeed == "" {
		promptSeed = sectionScaffold(sectionID, sectionTitle)
	}
	generated, err := g.summarizer.UpdateDocSection(ctx, promptSeed, topNChunks(chunks, 10))
	if err != nil {
		return "", false
	}
	generated = sanitizeGeneratedSection(generated)
	if generated == "" {
		return "", false
	}
	if isLowQualitySection(sectionID, generated) {
		return "", false
	}
	return generated, true
}

func (g *MarkdownGenerator) tryRenderDraftWithLLM(ctx context.Context, draft SectionDraft, chunks []knowledge.SearchChunk) (string, bool) {
	if g.summarizer == nil {
		return "", false
	}
	draftJSON := SerializeSectionDraft(draft)
	contextChunks := BuildDraftLLMContext(draft, chunks)
	if len(contextChunks) == 0 {
		contextChunks = topNChunks(chunks, 10)
	}
	generated, err := g.summarizer.RenderSectionFromDraft(ctx, draftJSON, contextChunks)
	if err != nil {
		return "", false
	}
	generated = sanitizeGeneratedSection(generated)
	generated = stripPromptArtifacts(generated)
	if strings.TrimSpace(generated) == "" {
		return "", false
	}
	if isLowQualitySection(draft.SectionID, generated) {
		return "", false
	}
	return generated, true
}

func sectionScaffold(sectionID, title string) string {
	switch sectionID {
	case "overview":
		return "# " + title + "\n\n" +
			"## Purpose\n\n" +
			"## End-to-End Flow\n\n" +
			"## Core Concepts\n\n"
	case "key-features":
		return "# " + title + "\n\n" +
			"## Capability 1\n\n" +
			"## Capability 2\n\n" +
			"## Capability 3\n\n"
	case "development":
		return "# " + title + "\n\n" +
			"## Quick Start\n\n" +
			"## Configuration\n\n" +
			"## Architecture Snapshot\n\n"
	default:
		return "# " + title + "\n\nWrite a technical section grounded in provided code context."
	}
}

func (g *MarkdownGenerator) enrichSectionWithDiagrams(sectionID, content string, chunks []knowledge.SearchChunk) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return trimmed
	}
	switch sectionID {
	case "overview":
		return upsertSectionMermaid(trimmed, "## End-to-End Flow", g.mermaid.GenerateArchitectureFlow(topNChunks(chunks, 14)))
	case "development":
		return upsertSectionMermaid(trimmed, "## Architecture Snapshot", g.mermaid.GenerateArchitectureSnapshot(topNChunks(chunks, 24)))
	default:
		return trimmed
	}
}

func (g *MarkdownGenerator) buildFallbackSection(sectionID string, chunks []knowledge.SearchChunk) string {
	switch sectionID {
	case "overview":
		return g.buildOverviewSection(chunks)
	case "key-features":
		return g.buildFeatureSection(chunks)
	case "development":
		return g.buildDevelopmentSection(chunks)
	default:
		return "# " + sectionTitleFromID(sectionID) + "\n\nNo content available yet."
	}
}

func (g *MarkdownGenerator) buildOverviewSection(chunks []knowledge.SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("# Overview\n\n")
	sb.WriteString("This project is documented from the code knowledge graph and section-scoped retrieval.\n\n")
	sb.WriteString("## End-to-End Flow\n\n")
	diagram := g.mermaid.GenerateArchitectureFlow(topNChunks(chunks, 14))
	sb.WriteString(diagram + "\n")
	sb.WriteString("## Core Components\n")
	for _, c := range topNChunks(chunks, 8) {
		line := strings.TrimSpace(c.Description)
		if line == "" {
			line = "Symbol extracted from the knowledge graph."
		}
		sb.WriteString(fmt.Sprintf("- `%s` (%s): %s\n", c.Name, c.UnitType, line))
	}
	sb.WriteString("\n")
	return sb.String()
}

func (g *MarkdownGenerator) buildFeatureSection(chunks []knowledge.SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("# Key Features\n\n")
	if len(chunks) == 0 {
		sb.WriteString("No feature-level symbols were found in the indexed scope.\n")
		return sb.String()
	}
	for _, c := range topNChunks(chunks, 6) {
		sb.WriteString(fmt.Sprintf("## %s\n\n", c.Name))
		desc := strings.TrimSpace(c.Description)
		if desc == "" {
			desc = "Feature inferred from graph-indexed source code."
		}
		sb.WriteString(desc + "\n\n")
		sb.WriteString("```go\n")
		snippet := strings.TrimSpace(c.Content)
		if snippet == "" {
			snippet = c.Signature
		}
		sb.WriteString(truncate(snippet, 800))
		sb.WriteString("\n```\n\n")
	}
	return sb.String()
}

func (g *MarkdownGenerator) buildDevelopmentSection(chunks []knowledge.SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("# Development\n\n")
	sb.WriteString("## Quick Start\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString("go test ./...\n")
	sb.WriteString("# run your app/tool entrypoint\n")
	sb.WriteString("```\n\n")
	sb.WriteString("## Configuration Reference\n\n")
	sb.WriteString(g.configTableMarkdown(chunks))
	sb.WriteString("\n## Architecture Snapshot\n\n")
	sb.WriteString(g.mermaid.GenerateArchitectureSnapshot(topNChunks(chunks, 24)))
	sb.WriteString("\n")
	return sb.String()
}

func inferProjectLabel(chunks []knowledge.SearchChunk) string {
	if len(chunks) == 0 {
		return "project"
	}
	counts := make(map[string]int)
	for _, c := range chunks {
		pkg := strings.TrimSpace(c.Package)
		if pkg == "" {
			continue
		}
		counts[pkg]++
	}
	best := "project"
	bestN := 0
	for pkg, n := range counts {
		if n > bestN || (n == bestN && pkg < best) {
			best = pkg
			bestN = n
		}
	}
	return best
}

func (g *MarkdownGenerator) configTableMarkdown(units []knowledge.SearchChunk) string {
	var configs []knowledge.SearchChunk
	for _, u := range units {
		if u.UnitType == "constant" || u.UnitType == "variable" {
			configs = append(configs, u)
		}
	}

	if len(configs) == 0 {
		return "No configuration constants were detected in the indexed scope.\n"
	}

	var sb strings.Builder
	sb.WriteString("| Name | Value | Description |\n")
	sb.WriteString("| :--- | :--- | :--- |\n")

	for _, c := range configs {
		value := "-"
		parts := strings.SplitN(c.Signature, "=", 2)
		if len(parts) == 2 {
			value = strings.TrimSpace(parts[1])
		}
		desc := strings.ReplaceAll(c.Description, "\n", " ")
		fmt.Fprintf(&sb, "| `%s` | `%s` | %s |\n", c.Name, value, desc)
	}
	return sb.String()
}

func topNChunks(chunks []knowledge.SearchChunk, n int) []knowledge.SearchChunk {
	if n <= 0 || len(chunks) <= n {
		return chunks
	}
	cp := append([]knowledge.SearchChunk(nil), chunks...)
	sort.Slice(cp, func(i, j int) bool {
		if cp[i].ID == cp[j].ID {
			return cp[i].Name < cp[j].Name
		}
		return cp[i].ID < cp[j].ID
	})
	return cp[:n]
}

func uniqueFileCount(chunks []knowledge.SearchChunk) int {
	if len(chunks) == 0 {
		return 0
	}
	seen := make(map[string]bool)
	for _, c := range chunks {
		path := strings.TrimSpace(c.FilePath)
		if path == "" {
			path = strings.TrimSpace(c.ID)
		}
		if path == "" {
			continue
		}
		seen[path] = true
	}
	return len(seen)
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "\n// ... truncated ..."
}

func sanitizeGeneratedSection(content string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	instructionLine := regexp.MustCompile(`(?i)^(explain|describe|write|must include|provide|document|do not|for each capability include)`)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if instructionLine.MatchString(trimmed) {
			continue
		}
		if strings.EqualFold(trimmed, "- **Intent**: why this behavior exists") ||
			strings.EqualFold(trimmed, "- **Behavior**: contracts, edge cases, constraints") ||
			strings.EqualFold(trimmed, "- **Usage**: short Go snippet when useful") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func isLowQualitySection(sectionID, content string) bool {
	text := strings.ToLower(strings.TrimSpace(content))
	if text == "" {
		return true
	}
	if strings.Contains(text, "## capability 1") || strings.Contains(text, "## capability 2") {
		return true
	}
	if strings.Contains(text, "m u s t include") || strings.Contains(text, "write 3-5 feature") {
		return true
	}
	if sectionID == "overview" && !strings.Contains(text, "```mermaid") {
		return true
	}
	if sectionID == "key-features" {
		if strings.Count(text, "\n## ") < 2 {
			return true
		}
		if strings.Contains(text, ".go") {
			return true
		}
	}
	return false
}

func injectDiagram(content, heading, diagram string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return heading + "\n\n" + strings.TrimSpace(diagram)
	}
	pos := strings.Index(trimmed, heading)
	if pos == -1 {
		return trimmed + "\n\n" + heading + "\n\n" + strings.TrimSpace(diagram)
	}
	headEnd := pos + len(heading)
	prefix := strings.TrimRight(trimmed[:headEnd], "\n")
	suffix := strings.TrimLeft(trimmed[headEnd:], "\n")
	if suffix == "" {
		return prefix + "\n\n" + strings.TrimSpace(diagram)
	}
	return prefix + "\n\n" + strings.TrimSpace(diagram) + "\n\n" + suffix
}

func upsertSectionMermaid(content, heading, diagram string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return heading + "\n\n" + strings.TrimSpace(diagram)
	}
	pos := strings.Index(trimmed, heading)
	if pos == -1 {
		return injectDiagram(trimmed, heading, diagram)
	}
	headEnd := pos + len(heading)
	afterHeading := strings.TrimLeft(trimmed[headEnd:], "\n")
	if strings.HasPrefix(afterHeading, "```mermaid") {
		end := strings.Index(afterHeading[len("```mermaid"):], "```")
		if end >= 0 {
			// Skip existing mermaid block right under the heading and replace with deterministic one.
			blockEnd := len("```mermaid") + end + len("```")
			rest := strings.TrimLeft(afterHeading[blockEnd:], "\n")
			prefix := strings.TrimRight(trimmed[:headEnd], "\n")
			if rest == "" {
				return prefix + "\n\n" + strings.TrimSpace(diagram)
			}
			return prefix + "\n\n" + strings.TrimSpace(diagram) + "\n\n" + rest
		}
	}
	return injectDiagram(trimmed, heading, diagram)
}

func filterChunksForSection(sectionID string, chunks []knowledge.SearchChunk) []knowledge.SearchChunk {
	if len(chunks) == 0 {
		return chunks
	}
	out := make([]knowledge.SearchChunk, 0, len(chunks))
	for _, c := range chunks {
		name := strings.ToLower(strings.TrimSpace(c.Name))
		switch sectionID {
		case "key-features":
			// Prefer semantic behavior units over physical module wrappers.
			if c.UnitType == "file_module" || c.UnitType == "constant" || c.UnitType == "variable" {
				continue
			}
			if strings.Contains(name, "_test") || strings.HasSuffix(name, "test") {
				continue
			}
		case "overview":
			if c.UnitType == "constant" || c.UnitType == "variable" {
				continue
			}
		case "development":
			// Keep config/runtime facing units; exclude noisy test symbols.
			if strings.Contains(name, "_test") || strings.HasSuffix(name, "test") {
				continue
			}
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return chunks
	}
	return out
}

func applyLowEvidencePolicy(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return trimmed
	}
	if strings.Contains(strings.ToLower(trimmed), "## evidence limitations") {
		return trimmed
	}
	note := "## Evidence Limitations\n\nThe current section is based on limited evidence from indexed chunks. Validate details against source references before relying on this as normative behavior."
	return trimmed + "\n\n" + note
}

func stripPromptArtifacts(content string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trim := strings.TrimSpace(strings.ToLower(line))
		if strings.HasPrefix(trim, "===") {
			continue
		}
		if strings.Contains(trim, "section draft") || strings.Contains(trim, "code evidence") {
			continue
		}
		if strings.Contains(trim, "**instruction**") || strings.Contains(trim, "must include one mermaid") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
