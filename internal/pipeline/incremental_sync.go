package pipeline

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"docod/internal/analysis"
	"docod/internal/config"
	"docod/internal/crawler"
	"docod/internal/extractor"
	"docod/internal/generator"
	"docod/internal/git"
	"docod/internal/graph"
	"docod/internal/index"
	"docod/internal/knowledge"
	"docod/internal/planner"
	"docod/internal/resolver"
	"docod/internal/retrieval"
	"docod/internal/storage"
)

type IncrementalSync struct {
	DBPath      string
	ProjectRoot string
	DocPath     string
}

type updatePlan struct {
	Changes    []git.ChangedFile
	FullResync bool
}

type graphUpdateResult struct {
	Graph        *graph.Graph
	UpdatedFiles []string
	DeletedFiles []string
}

func NewIncrementalSync(dbPath string) *IncrementalSync {
	return &IncrementalSync{
		DBPath:      dbPath,
		ProjectRoot: ".",
		DocPath:     "docs/documentation.md",
	}
}

func (s *IncrementalSync) Run(ctx context.Context, force bool) error {
	plan, err := s.detectChangesStage(force)
	if err != nil {
		return err
	}
	if len(plan.Changes) == 0 && !plan.FullResync {
		fmt.Println("✅ No changes detected.")
		return nil
	}

	store, err := s.initStoreStage()
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer store.Close()

	graphResult, err := s.graphUpdateStage(ctx, store, plan)
	if err != nil {
		return err
	}

	if err := store.SaveGraph(ctx, graphResult.Graph); err != nil {
		return fmt.Errorf("failed to save updated graph: %w", err)
	}

	if len(plan.Changes) > 0 {
		s.impactAnalysisStage(graphResult.Graph, plan.Changes)
	}

	var docPlan *planner.DocUpdatePlan
	if len(plan.Changes) > 0 {
		docPlan = s.retrievalPlanningStage(graphResult.Graph, plan.Changes)
	}

	if err := s.documentationStage(ctx, store, graphResult, plan.FullResync, docPlan); err != nil {
		return err
	}

	return nil
}

func (s *IncrementalSync) detectChangesStage(force bool) (*updatePlan, error) {
	changes, err := git.GetChangedFiles("HEAD")
	if err != nil {
		return nil, fmt.Errorf("failed to get git changes: %w", err)
	}

	fullResync := force && len(changes) == 0
	if fullResync {
		fmt.Println("🧭 No git changes detected. Running full sync from current codebase (--force).")
	} else if len(changes) > 0 {
		fmt.Printf("📝 Detected %d changed files.\n", len(changes))
	}

	return &updatePlan{
		Changes:    changes,
		FullResync: fullResync,
	}, nil
}

func (s *IncrementalSync) initStoreStage() (*storage.SQLiteStore, error) {
	_, _ = config.LoadConfig("config.yaml")
	return storage.NewSQLiteStore(s.DBPath)
}

func (s *IncrementalSync) graphUpdateStage(ctx context.Context, store *storage.SQLiteStore, plan *updatePlan) (*graphUpdateResult, error) {
	if plan.FullResync {
		start := time.Now()
		g, err := s.buildFullGraph()
		if err != nil {
			return nil, fmt.Errorf("full sync graph build failed: %w", err)
		}
		s.runResolverChainStage(g)
		fmt.Printf("📊 Graph Update: full rebuild completed in %v. Nodes=%d\n", time.Since(start), len(g.Nodes))
		fmt.Printf("  -> Linked edges: %d, unresolved relations: %d\n", len(g.Edges), len(g.Unresolved))
		s.printUnresolvedReasonMetrics(g)
		return &graphUpdateResult{
			Graph:        g,
			UpdatedFiles: collectGraphFiles(g),
			DeletedFiles: nil,
		}, nil
	}

	fmt.Println("🔄 Loading existing knowledge graph...")
	g, err := store.LoadGraph(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load graph: %w", err)
	}

	ext, err := extractor.NewExtractor("go")
	if err != nil {
		return nil, fmt.Errorf("failed to create extractor: %w", err)
	}

	nodesUpdated := 0
	nodesRemoved := 0
	for _, change := range plan.Changes {
		if !strings.HasSuffix(change.Path, ".go") {
			continue
		}

		var toRemove []string
		for id, node := range g.Nodes {
			if node.Unit.Filepath == change.Path {
				toRemove = append(toRemove, id)
			}
		}
		for _, id := range toRemove {
			delete(g.Nodes, id)
			nodesRemoved++
		}

		if _, err := os.Stat(change.Path); err == nil {
			units, err := ext.ExtractFromFile(change.Path)
			if err != nil {
				log.Printf("⚠️ Failed to parse file %s: %v", change.Path, err)
				continue
			}
			for _, u := range units {
				g.AddUnit(u)
				nodesUpdated++
			}
		}
	}

	fmt.Printf("📊 Graph Update: %d nodes removed, %d nodes added/updated.\n", nodesRemoved, nodesUpdated)
	g.RebuildIndices()
	s.runResolverChainStage(g)
	fmt.Printf("  -> Linked edges: %d, unresolved relations: %d\n", len(g.Edges), len(g.Unresolved))
	s.printUnresolvedReasonMetrics(g)
	updatedFiles, deletedFiles := splitUpdatedDeleted(plan.Changes)

	return &graphUpdateResult{
		Graph:        g,
		UpdatedFiles: updatedFiles,
		DeletedFiles: deletedFiles,
	}, nil
}

func (s *IncrementalSync) runResolverChainStage(g *graph.Graph) {
	if g == nil {
		return
	}

	chain := resolver.NewDefaultChain()
	results := chain.Run(g)
	for _, r := range results {
		if r.Err != nil {
			log.Printf("Warning: %s resolver failed: %v", r.Resolver, r.Err)
			break
		}
		fmt.Printf("  -> Resolver[%s]: attempted=%d resolved=%d skipped=%d unresolved=%d->%d edges=%d\n",
			r.Resolver,
			r.Stats.Attempted,
			r.Stats.Resolved,
			r.Stats.Skipped,
			r.UnresolvedBefore,
			r.UnresolvedAfter,
			r.EdgeCount,
		)
	}
}

func (s *IncrementalSync) printUnresolvedReasonMetrics(g *graph.Graph) {
	if g == nil || len(g.Unresolved) == 0 {
		return
	}
	counts := g.UnresolvedReasonCounts()
	for reason, n := range counts {
		fmt.Printf("     - unresolved[%s]=%d\n", reason, n)
	}
}

func (s *IncrementalSync) impactAnalysisStage(g *graph.Graph, changes []git.ChangedFile) {
	fmt.Println("🔍 Analyzing impact...")
	analyzer := analysis.NewAnalyzer(g)
	report, err := analyzer.AnalyzeImpact(changes)
	if err != nil {
		log.Printf("Analysis warning: %v", err)
		return
	}

	fmt.Printf("  -> %d symbols directly affected\n", len(report.DirectlyAffected))
	fmt.Printf("  -> %d symbols indirectly affected (callers)\n", len(report.IndirectlyAffected))
}

func (s *IncrementalSync) retrievalPlanningStage(g *graph.Graph, changes []git.ChangedFile) *planner.DocUpdatePlan {
	fmt.Println("🧩 Extracting retrieval subgraph...")
	sg := retrieval.ExtractFromChanges(g, changes, retrieval.DefaultConfig())
	fmt.Printf("  -> Retrieval seeds=%d nodes=%d edges=%d files=%d\n", len(sg.SeedIDs), len(sg.NodeIDs), len(sg.Edges), len(sg.UpdatedFiles))

	model, err := s.loadDocModelForPlanning()
	if err != nil {
		fmt.Printf("  -> Doc planning skipped: %v\n", err)
		return planner.BuildDocUpdatePlan(nil, sg)
	}

	plan := planner.BuildDocUpdatePlan(model, sg)
	if len(plan.AffectedSections) == 0 {
		fmt.Printf("  -> No section-source match. unmatched_symbols=%d\n", len(plan.UnmatchedSymbols))
		return plan
	}

	top := plan.AffectedSections
	if len(top) > 3 {
		top = top[:3]
	}
	for _, sec := range top {
		fmt.Printf("  -> Section[%s] score=%.2f conf=%.2f reasons=%s\n", sec.SectionID, sec.Score, sec.Confidence, strings.Join(sec.Reasons, ","))
	}
	fmt.Printf("  -> Planned sections=%d unmatched_symbols=%d\n", len(plan.AffectedSections), len(plan.UnmatchedSymbols))
	return plan
}

func (s *IncrementalSync) loadDocModelForPlanning() (*generator.DocModel, error) {
	modelPath := filepath.Join(filepath.Dir(s.DocPath), "doc_model.json")
	model, err := generator.LoadDocModel(modelPath)
	if err == nil {
		return model, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	docBytes, readErr := os.ReadFile(s.DocPath)
	if readErr != nil {
		return nil, readErr
	}
	return generator.BuildModelFromMarkdown(string(docBytes)), nil
}

func (s *IncrementalSync) documentationStage(ctx context.Context, store *storage.SQLiteStore, graphResult *graphUpdateResult, fullResync bool, docPlan *planner.DocUpdatePlan) error {
	fmt.Println("✍️  Regenerating documentation...")
	engine, summarizer, err := initEngine(ctx, graphResult.Graph, store)
	if err != nil {
		fmt.Printf("⚠️  Skipping documentation generation: %v\n", err)
		return nil
	}

	if fullResync {
		fmt.Println("🧠 Reindexing embeddings (full)...")
		if err := engine.IndexAll(ctx); err != nil {
			log.Printf("Warning: Full embedding index failed: %v", err)
		}
	} else {
		fmt.Println("🧠 Updating embeddings incrementally...")
		if err := engine.IndexIncrementalWithOptions(ctx, graphResult.UpdatedFiles, graphResult.DeletedFiles, knowledge.IndexingOptions{
			MaxChunksPerRun: s.maxEmbedChunksPerRun(),
		}); err != nil {
			log.Printf("Warning: Embedding update failed: %v", err)
		}
	}

	targetFiles := graphResult.UpdatedFiles
	if docPlan != nil && len(docPlan.TriggeredFiles) > 0 {
		targetFiles = dedupeSorted(targetFiles, docPlan.TriggeredFiles)
		fmt.Printf("  -> Doc update file scope: %d files (graph+retrieval merged)\n", len(targetFiles))
	}

	docUpdater := generator.NewDocUpdater(engine, summarizer)
	if _, err := os.Stat(s.DocPath); err == nil {
		fmt.Println("📝 Updating existing documentation sections...")
		var updatePlan *generator.UpdatePlan
		if docPlan != nil && len(docPlan.AffectedSections) > 0 {
			updatePlan = &generator.UpdatePlan{
				PreferredSectionIDs: sectionIDsByImpact(docPlan),
				StrictSectionScope:  false,
				SectionConfidence:   sectionConfidenceByImpact(docPlan),
				MinConfidenceForLLM: s.minConfidenceForLLM(),
			}
		}
		if err := docUpdater.UpdateDocsWithPlan(ctx, s.DocPath, targetFiles, updatePlan); err != nil {
			log.Printf("Warning: Failed to update docs incrementally, falling back to full gen: %v", err)
		} else {
			fmt.Println("✅ Documentation updated incrementally in 'docs/'.")
			return nil
		}
	}

	fmt.Println("📄 Documentation not found or incremental update failed, generating from scratch...")
	gen := generator.NewMarkdownGenerator(engine, summarizer)
	if err := gen.GenerateDocs(ctx, "docs"); err != nil {
		return fmt.Errorf("failed to generate docs: %w", err)
	}
	fmt.Println("✅ Documentation generated in 'docs/'.")
	return nil
}

func initEngine(ctx context.Context, g *graph.Graph, store *storage.SQLiteStore) (*knowledge.Engine, *knowledge.GeminiSummarizer, error) {
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.AI.APIKey == "" {
		return nil, nil, fmt.Errorf("AI API key not configured")
	}

	embedder, err := knowledge.NewGeminiEmbedder(ctx, cfg.AI.APIKey, cfg.AI.Model, cfg.AI.Dimension)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	summarizer, err := knowledge.NewGeminiSummarizer(ctx, cfg.AI.APIKey, cfg.AI.SummaryModel)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create summarizer: %w", err)
	}

	engine := knowledge.NewEngine(g, embedder, store)
	return engine, summarizer, nil
}

func (s *IncrementalSync) buildFullGraph() (*graph.Graph, error) {
	ext, err := extractor.NewExtractor("go")
	if err != nil {
		return nil, err
	}
	cr := crawler.NewCrawler(ext)
	idx := index.NewIndexer(cr)
	return idx.BuildGraph(s.ProjectRoot)
}

func splitUpdatedDeleted(changes []git.ChangedFile) ([]string, []string) {
	var updatedFiles, deletedFiles []string
	for _, change := range changes {
		if _, err := os.Stat(change.Path); os.IsNotExist(err) {
			deletedFiles = append(deletedFiles, change.Path)
		} else {
			updatedFiles = append(updatedFiles, change.Path)
		}
	}
	return updatedFiles, deletedFiles
}

func collectGraphFiles(g *graph.Graph) []string {
	seen := make(map[string]bool)
	var files []string
	for _, node := range g.Nodes {
		p := node.Unit.Filepath
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		files = append(files, p)
	}
	return files
}

func dedupeSorted(groups ...[]string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0)
	for _, group := range groups {
		for _, item := range group {
			if item == "" || seen[item] {
				continue
			}
			seen[item] = true
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
}

func sectionIDsByImpact(plan *planner.DocUpdatePlan) []string {
	if plan == nil || len(plan.AffectedSections) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	ids := make([]string, 0, len(plan.AffectedSections))
	for _, impact := range plan.AffectedSections {
		if impact.SectionID == "" || seen[impact.SectionID] {
			continue
		}
		seen[impact.SectionID] = true
		ids = append(ids, impact.SectionID)
	}
	return ids
}

func sectionConfidenceByImpact(plan *planner.DocUpdatePlan) map[string]float64 {
	if plan == nil || len(plan.AffectedSections) == 0 {
		return nil
	}
	out := make(map[string]float64, len(plan.AffectedSections))
	for _, impact := range plan.AffectedSections {
		if impact.SectionID == "" {
			continue
		}
		out[impact.SectionID] = impact.Confidence
	}
	return out
}

func (s *IncrementalSync) minConfidenceForLLM() float64 {
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil || cfg == nil {
		return 0.60
	}
	value := cfg.Docs.MinConfidenceForLLM
	if value <= 0 {
		return 0.60
	}
	if value > 1 {
		return 1
	}
	return value
}

func (s *IncrementalSync) maxEmbedChunksPerRun() int {
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil || cfg == nil {
		return 80
	}
	value := cfg.Docs.MaxEmbedChunksPerRun
	if value < 0 {
		return 0
	}
	return value
}
