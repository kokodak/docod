package generator

import (
	"context"
	"docod/internal/config"
	"docod/internal/knowledge"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type DocUpdater struct {
	engine     *knowledge.Engine
	summarizer knowledge.Summarizer
}

type updaterOptions struct {
	maxLLMSections      int
	enableSemanticMatch bool
	enableLLMRouter     bool
	maxLLMRoutes        int
}

func NewDocUpdater(e *knowledge.Engine, s knowledge.Summarizer) *DocUpdater {
	return &DocUpdater{
		engine:     e,
		summarizer: s,
	}
}

// UpdateDocs incrementally updates the JSON doc model and re-renders Markdown.
func (u *DocUpdater) UpdateDocs(ctx context.Context, docPath string, changedFilePaths []string) error {
	opts := resolveUpdaterOptions()
	modelPath := filepath.Join(filepath.Dir(docPath), "doc_model.json")

	// Ensure we can bootstrap from existing markdown if model doesn't exist yet.
	model, err := u.loadOrBootstrapModel(modelPath, docPath)
	if err != nil {
		return err
	}
	NormalizeDocModel(model)

	fileChunks := u.engine.PrepareChunksForFiles(changedFilePaths)
	if len(fileChunks) == 0 {
		fmt.Println("  -> No exported code chunks changed; skipping doc update.")
		return nil
	}

	affected := make(map[string][]knowledge.SearchChunk)
	var unmatched []knowledge.SearchChunk

	for _, chunk := range fileChunks {
		matched := false
		for i := range model.Sections {
			if sectionReferencesFile(model.Sections[i], chunk.ID) {
				affected[model.Sections[i].ID] = append(affected[model.Sections[i].ID], chunk)
				matched = true
			}
		}
		if !matched {
			unmatched = append(unmatched, chunk)
		}
	}

	// Fallback 1: heuristic routing to canonical sections (no embedding cost).
	var stillUnmatched []knowledge.SearchChunk
	for _, chunk := range unmatched {
		if secID := chooseSectionByHeuristic(model, chunk); secID != "" {
			affected[secID] = append(affected[secID], chunk)
			continue
		}
		stillUnmatched = append(stillUnmatched, chunk)
	}
	unmatched = stillUnmatched

	// Fallback 2 (optional): semantic matching only if explicitly enabled.
	if len(unmatched) > 0 {
		if opts.enableLLMRouter {
			routed, still := u.llmRouteSections(ctx, model, unmatched, opts.maxLLMRoutes)
			for secID, chunks := range routed {
				affected[secID] = append(affected[secID], chunks...)
			}
			unmatched = still
		}
	}

	// Fallback 3 (optional): semantic matching only if explicitly enabled.
	if len(unmatched) > 0 {
		if opts.enableSemanticMatch {
			semMatched, semUnmatched := u.semanticMatchSections(ctx, model, unmatched)
			for secID, chunks := range semMatched {
				affected[secID] = append(affected[secID], chunks...)
			}
			unmatched = semUnmatched
		}
	}

	if len(affected) == 0 && len(unmatched) == 0 {
		fmt.Println("  -> No relevant documentation changes needed.")
		return nil
	}

	fmt.Printf("  -> Updating %d sections, creating %d sections.\n", len(affected), len(unmatched))
	now := time.Now().UTC().Format(time.RFC3339)
	appliedUpdates := 0
	maxLLMUpdates := opts.maxLLMSections
	updateOrder := prioritizedSectionIDs(affected)

	// Update affected sections.
	for i, secID := range updateOrder {
		triggeringChunks := affected[secID]
		sec := model.SectionByID(secID)
		if sec == nil {
			continue
		}

		// Always keep traceability up to date.
		sec.Sources = MergeSources(sec.Sources, triggeringChunks)
		sec.LastUpdated = &UpdateInfo{
			CommitSHA: "HEAD",
			Timestamp: now,
		}

		// Cost control: only top N affected sections get LLM rewrite.
		if i >= maxLLMUpdates {
			sec.Hash = sectionHash(*sec)
			appliedUpdates++
			continue
		}

		updatedContent, err := u.summarizer.UpdateDocSection(ctx, sec.ContentMD, triggeringChunks)
		if err != nil {
			fmt.Printf("Failed to update section %s: %v\n", sec.Title, err)
			sec.Hash = sectionHash(*sec)
			appliedUpdates++
			continue
		}

		sec.ContentMD = strings.TrimSpace(updatedContent)
		sec.Summary = summarizeContent(sec.ContentMD)
		sec.Hash = sectionHash(*sec)
		appliedUpdates++
	}

	// Create at most one new section for all unmatched chunks to minimize LLM calls.
	if len(unmatched) > 0 {
		batch := unmatched
		if len(batch) > 8 {
			batch = batch[:8]
		}
		newContent, err := u.summarizer.GenerateNewSection(ctx, batch)
		if err != nil {
			fmt.Printf("Failed to generate new section for unmatched changes: %v\n", err)
			newContent = buildFallbackBatchSectionContent(batch)
		}

		nextOrder := len(model.Sections)
		newID := ensureUniqueSectionID(model, "incremental-changes")
		newSec := ModelSect{
			ID:        newID,
			Title:     "Incremental Changes",
			Level:     2,
			Order:     nextOrder,
			ParentID:  nil,
			ContentMD: strings.TrimSpace(newContent),
			Summary:   summarizeContent(newContent),
			Status:    "active",
			Sources:   MergeSources(nil, batch),
		}
		newSec.Hash = sectionHash(newSec)
		newSec.LastUpdated = &UpdateInfo{
			CommitSHA: "HEAD",
			Timestamp: now,
		}
		model.Sections = append(model.Sections, newSec)
		appliedUpdates++
	}

	if appliedUpdates == 0 {
		return fmt.Errorf("no documentation updates could be applied")
	}

	model.Meta.GeneratedAt = now
	NormalizeDocModel(model)
	if err := model.Validate(); err != nil {
		return fmt.Errorf("doc model validation failed: %w", err)
	}

	if err := SaveDocModel(modelPath, model); err != nil {
		return fmt.Errorf("failed to save doc model: %w", err)
	}

	rendered := RenderMarkdownFromModel(model)
	if err := os.WriteFile(docPath, []byte(rendered), 0644); err != nil {
		return err
	}

	return nil
}

func (u *DocUpdater) loadOrBootstrapModel(modelPath, docPath string) (*DocModel, error) {
	model, err := LoadDocModel(modelPath)
	if err == nil {
		return model, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load doc model: %w", err)
	}

	contentBytes, readErr := os.ReadFile(docPath)
	if os.IsNotExist(readErr) {
		return nil, fmt.Errorf("documentation file not found: %s", docPath)
	}
	if readErr != nil {
		return nil, readErr
	}

	model = BuildModelFromMarkdown(string(contentBytes))
	if err := SaveDocModel(modelPath, model); err != nil {
		return nil, fmt.Errorf("failed to bootstrap doc model: %w", err)
	}
	return model, nil
}

func (u *DocUpdater) semanticMatchSections(ctx context.Context, model *DocModel, chunks []knowledge.SearchChunk) (map[string][]knowledge.SearchChunk, []knowledge.SearchChunk) {
	affected := make(map[string][]knowledge.SearchChunk)
	var unmatched []knowledge.SearchChunk

	if err := u.indexModelSections(ctx, model.Sections); err != nil {
		return affected, chunks
	}

	for _, chunk := range chunks {
		q := chunk.Description + "\n" + chunk.Signature
		vecs, err := u.engine.Embedder().Embed(ctx, []string{q})
		if err != nil || len(vecs) == 0 {
			unmatched = append(unmatched, chunk)
			continue
		}
		results, err := u.engine.Indexer().Search(ctx, vecs[0], 3)
		if err != nil {
			unmatched = append(unmatched, chunk)
			continue
		}

		found := false
		for _, res := range results {
			if res.Chunk.UnitType != "doc_section" {
				continue
			}
			affected[res.Chunk.ID] = append(affected[res.Chunk.ID], chunk)
			found = true
			break
		}
		if !found {
			unmatched = append(unmatched, chunk)
		}
	}

	return affected, unmatched
}

func (u *DocUpdater) indexModelSections(ctx context.Context, sections []ModelSect) error {
	texts := make([]string, 0, len(sections))
	items := make([]knowledge.VectorItem, 0, len(sections))

	for _, sec := range sections {
		texts = append(texts, fmt.Sprintf("Documentation Section: %s\nContent: %s", sec.Title, sec.ContentMD))
	}

	vectors, err := u.engine.Embedder().Embed(ctx, texts)
	if err != nil {
		return err
	}

	for i, sec := range sections {
		items = append(items, knowledge.VectorItem{
			Chunk: knowledge.SearchChunk{
				ID:          sec.ID,
				Name:        sec.Title,
				UnitType:    "doc_section",
				Description: sec.Title,
				Content:     sec.ContentMD,
			},
			Embedding: vectors[i],
		})
	}

	return u.engine.Indexer().Add(ctx, items)
}

func sectionReferencesFile(sec ModelSect, filePath string) bool {
	for _, src := range sec.Sources {
		if src.FilePath == filePath {
			return true
		}
	}
	return false
}

func ensureUniqueSectionID(model *DocModel, base string) string {
	if model.SectionByID(base) == nil {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if model.SectionByID(candidate) == nil {
			return candidate
		}
	}
}

func resolveUpdaterOptions() updaterOptions {
	// Safe defaults for low-cost operation.
	opts := updaterOptions{
		maxLLMSections:      2,
		enableSemanticMatch: false,
		enableLLMRouter:     false,
		maxLLMRoutes:        2,
	}

	cfg, err := config.LoadConfig("config.yaml")
	if err != nil || cfg == nil {
		return opts
	}

	if cfg.Docs.MaxLLMSections >= 0 {
		opts.maxLLMSections = cfg.Docs.MaxLLMSections
	}
	opts.enableSemanticMatch = cfg.Docs.EnableSemanticMatch
	opts.enableLLMRouter = cfg.Docs.EnableLLMRouter
	if cfg.Docs.MaxLLMRoutes >= 0 {
		opts.maxLLMRoutes = cfg.Docs.MaxLLMRoutes
	}
	return opts
}

func prioritizedSectionIDs(affected map[string][]knowledge.SearchChunk) []string {
	ids := make([]string, 0, len(affected))
	for id := range affected {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		li := len(affected[ids[i]])
		lj := len(affected[ids[j]])
		if li == lj {
			return ids[i] < ids[j]
		}
		return li > lj
	})
	return ids
}

func (u *DocUpdater) llmRouteSections(ctx context.Context, model *DocModel, chunks []knowledge.SearchChunk, routeBudget int) (map[string][]knowledge.SearchChunk, []knowledge.SearchChunk) {
	routed := make(map[string][]knowledge.SearchChunk)
	var unmatched []knowledge.SearchChunk

	ordered := orderedSections(model)
	if len(ordered) == 0 {
		return routed, chunks
	}

	var toc []string
	for _, sec := range ordered {
		toc = append(toc, sec.Title)
	}

	for _, chunk := range chunks {
		if routeBudget <= 0 {
			unmatched = append(unmatched, chunk)
			continue
		}

		preview := buildRoutingPreview(chunk)
		idx, err := u.summarizer.FindInsertionPoint(ctx, toc, preview)
		if err != nil {
			unmatched = append(unmatched, chunk)
			continue
		}

		target, ok := sectionFromRoutingIndex(ordered, idx)
		if !ok {
			unmatched = append(unmatched, chunk)
			continue
		}

		routed[target.ID] = append(routed[target.ID], chunk)
		routeBudget--
	}

	return routed, unmatched
}

func orderedSections(model *DocModel) []ModelSect {
	sections := append([]ModelSect(nil), model.Sections...)
	sort.Slice(sections, func(i, j int) bool {
		if sections[i].Order == sections[j].Order {
			return sections[i].ID < sections[j].ID
		}
		return sections[i].Order < sections[j].Order
	})
	return sections
}

func buildRoutingPreview(chunk knowledge.SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("Change candidate:\n")
	sb.WriteString("Name: " + chunk.Name + "\n")
	sb.WriteString("Type: " + chunk.UnitType + "\n")
	if strings.TrimSpace(chunk.Description) != "" {
		sb.WriteString("Summary: " + chunk.Description + "\n")
	}
	if strings.TrimSpace(chunk.Signature) != "" {
		sb.WriteString("Signature: " + chunk.Signature + "\n")
	}
	if len(chunk.Dependencies) > 0 {
		sb.WriteString("Depends: " + strings.Join(chunk.Dependencies, ", ") + "\n")
	}
	return sb.String()
}

func sectionFromRoutingIndex(sections []ModelSect, idx int) (ModelSect, bool) {
	if len(sections) == 0 {
		return ModelSect{}, false
	}
	// FindInsertionPoint returns "insert after idx".
	// For assignment to an existing section:
	// -1 -> first section, N -> section N.
	if idx < 0 {
		return sections[0], true
	}
	if idx >= len(sections) {
		return sections[len(sections)-1], true
	}
	return sections[idx], true
}

func chooseSectionByHeuristic(model *DocModel, chunk knowledge.SearchChunk) string {
	file := strings.ToLower(chunk.ID)
	name := strings.ToLower(chunk.Name)
	desc := strings.ToLower(chunk.Description)
	hay := file + " " + name + " " + desc

	if strings.Contains(hay, "config") || strings.Contains(hay, "env") || strings.Contains(hay, "setup") {
		if model.SectionByID("development") != nil {
			return "development"
		}
	}
	if strings.Contains(hay, "graph") || strings.Contains(hay, "index") || strings.Contains(hay, "extract") || strings.Contains(hay, "crawler") || strings.Contains(hay, "parser") {
		if model.SectionByID("overview") != nil {
			return "overview"
		}
	}
	if model.SectionByID("key-features") != nil {
		return "key-features"
	}
	return ""
}

func buildFallbackBatchSectionContent(chunks []knowledge.SearchChunk) string {
	var sb strings.Builder
	sb.WriteString("## Incremental Changes\n\n")
	sb.WriteString("### What Changed\n")
	for _, chunk := range chunks {
		sb.WriteString("- `" + chunk.Name + "`")
		if strings.TrimSpace(chunk.Description) != "" {
			sb.WriteString(": " + strings.TrimSpace(chunk.Description))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n### Notes\n")
	sb.WriteString("This section was generated in low-cost fallback mode from incremental code deltas.\n")
	return sb.String()
}
