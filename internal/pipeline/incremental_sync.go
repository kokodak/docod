package pipeline

import (
	"context"
	"fmt"
	"log"
	"os"
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
	"docod/internal/resolver"
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

	if err := s.documentationStage(ctx, store, graphResult, plan.FullResync); err != nil {
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
		s.runTypeResolverStage(g)
		fmt.Printf("📊 Graph Update: full rebuild completed in %v. Nodes=%d\n", time.Since(start), len(g.Nodes))
		fmt.Printf("  -> Linked edges: %d, unresolved relations: %d\n", len(g.Edges), len(g.Unresolved))
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
	g.LinkRelations()
	s.runTypeResolverStage(g)
	fmt.Printf("  -> Linked edges: %d, unresolved relations: %d\n", len(g.Edges), len(g.Unresolved))
	updatedFiles, deletedFiles := splitUpdatedDeleted(plan.Changes)

	return &graphUpdateResult{
		Graph:        g,
		UpdatedFiles: updatedFiles,
		DeletedFiles: deletedFiles,
	}, nil
}

func (s *IncrementalSync) runTypeResolverStage(g *graph.Graph) {
	if g == nil || len(g.Unresolved) == 0 {
		return
	}

	r := resolver.NewGoTypesResolver()
	stats, err := r.ResolveGraphRelations(g)
	if err != nil {
		log.Printf("Warning: types resolver failed: %v", err)
		return
	}
	fmt.Printf("  -> Types resolver: attempted=%d resolved=%d skipped=%d\n", stats.Attempted, stats.Resolved, stats.Skipped)
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

func (s *IncrementalSync) documentationStage(ctx context.Context, store *storage.SQLiteStore, graphResult *graphUpdateResult, fullResync bool) error {
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
		if err := engine.IndexIncremental(ctx, graphResult.UpdatedFiles, graphResult.DeletedFiles); err != nil {
			log.Printf("Warning: Embedding update failed: %v", err)
		}
	}

	docUpdater := generator.NewDocUpdater(engine, summarizer)
	if _, err := os.Stat(s.DocPath); err == nil {
		fmt.Println("📝 Updating existing documentation sections...")
		if err := docUpdater.UpdateDocs(ctx, s.DocPath, graphResult.UpdatedFiles); err != nil {
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
