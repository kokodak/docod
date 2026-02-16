package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"docod/internal/config"
	"docod/internal/crawler"
	"docod/internal/extractor"
	"docod/internal/generator"
	"docod/internal/graph"
	"docod/internal/index"
	"docod/internal/knowledge"
	"docod/internal/pipeline"
	"docod/internal/storage"

	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:   "docod",
		Short: "AI-powered Documentation Agent",
	}
	dbPath      string
	syncForce   bool
	updateForce bool
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	// Default DB path is local to the project
	rootCmd.PersistentFlags().StringVarP(&dbPath, "db", "d", "docod.db", "Path to the local knowledge graph database (SQLite)")

	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(generateCmd)

	// Prefer `sync` as the primary command; keep generate for compatibility.
	generateCmd.Hidden = true

	syncCmd.Flags().BoolVarP(&syncForce, "force", "f", false, "Sync current codebase even when git reports no changes")
	updateCmd.Flags().BoolVarP(&updateForce, "force", "f", false, "Update docs from current codebase even when git reports no changes")
}

// initStore initializes the SQLite store.
func initStore() (*storage.SQLiteStore, error) {
	// Ensure config is loaded (even if defaults)
	_, _ = config.LoadConfig("config.yaml")

	return storage.NewSQLiteStore(dbPath)
}

// initEngine initializes the Knowledge Engine with configured Embedder and Summarizer.
func initEngine(ctx context.Context, g *graph.Graph, store *storage.SQLiteStore) (*knowledge.Engine, knowledge.Summarizer, error) {
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	embeddingProvider := strings.ToLower(strings.TrimSpace(cfg.AI.EmbeddingProvider))
	embedKey := strings.TrimSpace(cfg.AI.EmbeddingAPIKey)
	baseURL := ""
	switch embeddingProvider {
	case "openai":
		baseURL = cfg.AI.OpenAIBaseURL
	case "ollama":
		embedKey = ""
		baseURL = cfg.AI.OllamaBaseURL
	}
	if embeddingProvider != "ollama" && strings.TrimSpace(embedKey) == "" {
		return nil, nil, fmt.Errorf("embedding API key not configured for provider=%s", cfg.AI.EmbeddingProvider)
	}

	// 1. Setup Embedder
	embedder, err := knowledge.NewEmbedder(ctx, knowledge.EmbedderOptions{
		Provider:  cfg.AI.EmbeddingProvider,
		APIKey:    embedKey,
		Model:     cfg.AI.EmbeddingModel,
		Dimension: cfg.AI.EmbeddingDim,
		BaseURL:   baseURL,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	// 2. Setup Summarizer
	llmProvider := strings.ToLower(strings.TrimSpace(cfg.AI.LLMProvider))
	llmKey := strings.TrimSpace(cfg.AI.LLMAPIKey)
	llmBaseURL := strings.TrimSpace(cfg.AI.LLMBaseURL)
	if (llmProvider == "gemini" || llmProvider == "openai") && llmKey == "" {
		return nil, nil, fmt.Errorf("LLM API key not configured for provider=%s", cfg.AI.LLMProvider)
	}
	summarizer, err := knowledge.NewSummarizer(ctx, knowledge.SummarizerOptions{
		Provider: cfg.AI.LLMProvider,
		APIKey:   llmKey,
		Model:    cfg.AI.LLMModel,
		BaseURL:  llmBaseURL,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create llm summarizer: %w", err)
	}

	// 3. Create Engine
	// Store implements Indexer via our adapter methods
	engine := knowledge.NewEngine(g, embedder, store)

	return engine, summarizer, nil
}

var scanCmd = &cobra.Command{
	Use:   "scan [path]",
	Short: "Scan the project and update the knowledge graph locally",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		path := "."
		if len(args) > 0 {
			path = args[0]
		}

		absPath, err := os.Getwd()
		if err != nil {
			log.Fatalf("Failed to get current directory: %v", err)
		}
		if path != "." {
			// Basic path handling, in real app use filepath.Abs
			absPath = path
		}

		fmt.Printf("📂 Scanning directory: %s\n", absPath)

		// 1. Initialize Store
		store, err := initStore()
		if err != nil {
			log.Fatalf("Failed to initialize database: %v", err)
		}
		defer store.Close()

		// 2. Setup Extractor & Indexer
		// Currently defaulting to 'go', but could be auto-detected
		ext, err := extractor.NewExtractor("go")
		if err != nil {
			log.Fatalf("Failed to create extractor: %v", err)
		}

		cr := crawler.NewCrawler(ext)
		idx := index.NewIndexer(cr)

		// 3. Build Graph
		fmt.Println("🚀 Building dependency graph...")
		start := time.Now()
		g, err := idx.BuildGraph(absPath)
		if err != nil {
			log.Fatalf("Build failed: %v", err)
		}
		fmt.Printf("✅ Graph built in %v. Found %d nodes.\n", time.Since(start), len(g.Nodes))

		// 4. Save to DB
		ctx := context.Background()
		fmt.Println("💾 Saving to local database...")
		if err := store.SaveGraph(ctx, g); err != nil {
			log.Fatalf("Failed to save graph: %v", err)
		}

		// 5. Index Embeddings (Optional/Future: could be done here if API key exists)
		// For now, we leave it to explicit 'generate' or 'update' to avoid cost on every scan.

		fmt.Printf("🎉 Scan complete! Database: %s\n", dbPath)
	},
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Run docod in automatic mode (bootstrap or incremental)",
	Run: func(cmd *cobra.Command, args []string) {
		// Bootstrap if db does not exist.
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			fmt.Println("🆕 No local graph database found. Running initial bootstrap...")
			scanCmd.Run(scanCmd, []string{"."})
			generateCmd.Run(generateCmd, []string{})
			return
		}

		// Otherwise, run incremental update flow.
		runner := pipeline.NewIncrementalSync(dbPath)
		if err := runner.Run(context.Background(), syncForce); err != nil {
			log.Fatalf("Sync failed: %v", err)
		}
	},
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Incrementally update the knowledge graph and documentation based on git changes",
	Run: func(cmd *cobra.Command, args []string) {
		runner := pipeline.NewIncrementalSync(dbPath)
		if err := runner.Run(context.Background(), updateForce); err != nil {
			log.Fatalf("Update failed: %v", err)
		}
	},
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate documentation from the knowledge graph",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		report := generator.NewPipelineReport("full_generate", "docs")
		reportPath := "docs/pipeline_report.json"

		// 1. Initialize Store
		stage := report.BeginStage("init_store")
		store, err := initStore()
		if err != nil {
			report.EndStage(stage, "error", nil, nil, err)
			_ = report.Save(reportPath)
			log.Fatalf("Failed to initialize database: %v", err)
		}
		report.EndStage(stage, "ok", nil, nil, nil)
		defer store.Close()

		fmt.Println("🔄 Loading knowledge graph...")
		stage = report.BeginStage("load_graph")
		g, err := store.LoadGraph(ctx)
		if err != nil {
			report.EndStage(stage, "error", nil, nil, err)
			_ = report.Save(reportPath)
			log.Fatalf("Failed to load graph: %v", err)
		}
		report.EndStage(stage, "ok", map[string]float64{
			"nodes_total": float64(len(g.Nodes)),
			"edges_total": float64(len(g.Edges)),
		}, nil, nil)

		// 2. Initialize Engine & Summarizer
		stage = report.BeginStage("init_engine")
		engine, summarizer, err := initEngine(ctx, g, store)
		if err != nil {
			report.EndStage(stage, "error", nil, nil, err)
			report.AddSignal("engine_init_failed", "init_engine", "critical", "Failed to initialize embedder/summarizer.", 1)
			_ = report.Save(reportPath)
			log.Fatalf("Setup failed: %v\nCheck your config.yaml and API keys.", err)
		}
		report.EndStage(stage, "ok", nil, nil, nil)

		stage = report.BeginStage("index_health")
		indexMode := "reuse"
		indexRebuildError := ""
		expectedIDs, healthBefore, err := assessIndexHealth(ctx, engine, store)
		if err != nil {
			report.EndStage(stage, "error", nil, nil, err)
			report.AddSignal("index_health_assess_failed", "index_health", "warning", "Failed to assess vector index health.", 1)
		} else {
			if healthBefore.IndexedChunks == 0 {
				report.AddSignal("index_empty_before_generate", "index_health", "warning", "Vector index is empty before generation.", 0)
			}

			if shouldRebuildIndex(healthBefore) {
				indexMode = "rebuild_full"
				fmt.Println("🧠 Rebuilding vector index for full generation...")
				if err := engine.IndexAllWithOptions(ctx, knowledge.IndexingOptions{
					// Full generation prioritizes retrieval quality over runtime cap.
					MaxChunksPerRun: 0,
				}); err != nil {
					indexRebuildError = err.Error()
					report.AddSignal("index_rebuild_failed", "index_health", "critical", fmt.Sprintf("Vector index rebuild failed: %v", err), 1)
				}
			}

			healthAfter, staleAfter, err := reassessIndexHealth(ctx, store, expectedIDs)
			if err != nil {
				report.EndStage(stage, "error", nil, []string{"mode=" + indexMode}, err)
				report.AddSignal("index_health_reassess_failed", "index_health", "warning", "Failed to reassess index health after maintenance.", 1)
			} else {
				if len(staleAfter) > 0 {
					if err := store.Delete(ctx, staleAfter); err != nil {
						report.AddSignal("stale_chunk_cleanup_failed", "index_health", "warning", "Failed to clean stale chunks after health check.", float64(len(staleAfter)))
					} else {
						healthAfter.StaleChunks = 0
						healthAfter.StaleRatio = 0
					}
				}

				if healthAfter.IndexedChunks == 0 {
					report.AddSignal("index_empty_after_health", "index_health", "critical", "Vector index remains empty after health maintenance.", 0)
				}
				if healthAfter.Coverage < 0.70 {
					report.AddSignal("index_low_coverage", "index_health", "warning", "Indexed chunk coverage is below threshold.", healthAfter.Coverage)
				}
				if healthAfter.Freshness < 0.85 {
					report.AddSignal("index_low_freshness", "index_health", "warning", "Index freshness is below threshold.", healthAfter.Freshness)
				}
				if healthAfter.StaleChunks > 0 {
					report.AddSignal("index_stale_chunks_remaining", "index_health", "warning", "Stale chunks remain after cleanup.", float64(healthAfter.StaleChunks))
				}

				notes := []string{"mode=" + indexMode}
				if strings.TrimSpace(indexRebuildError) != "" {
					notes = append(notes, "index_rebuild_error="+strings.TrimSpace(indexRebuildError))
				}
				report.EndStage(stage, "ok", map[string]float64{
					"expected_chunks":       float64(healthBefore.ExpectedChunks),
					"indexed_chunks_before": float64(healthBefore.IndexedChunks),
					"indexed_chunks_after":  float64(healthAfter.IndexedChunks),
					"missing_chunks_before": float64(healthBefore.MissingChunks),
					"stale_chunks_before":   float64(healthBefore.StaleChunks),
					"missing_chunks_after":  float64(healthAfter.MissingChunks),
					"stale_chunks_after":    float64(healthAfter.StaleChunks),
					"coverage_before":       healthBefore.Coverage,
					"coverage_after":        healthAfter.Coverage,
					"freshness_before":      healthBefore.Freshness,
					"freshness_after":       healthAfter.Freshness,
					"stale_ratio_before":    healthBefore.StaleRatio,
					"stale_ratio_after":     healthAfter.StaleRatio,
					"chunk_files_before":    float64(healthBefore.ChunkFiles),
					"chunk_files_after":     float64(healthAfter.ChunkFiles),
				}, notes, nil)
			}
		}

		// 3. Generate
		fmt.Println("🚀 Generating documentation...")
		gen := generator.NewMarkdownGenerator(engine, summarizer)
		if err := gen.GenerateDocsWithReport(ctx, "docs", report); err != nil {
			report.AddSignal("generate_docs_failed", "generate_docs", "critical", "Failed while generating docs.", 1)
			_ = report.Save(reportPath)
			log.Fatalf("Failed to generate docs: %v", err)
		}

		fmt.Println("✅ Documentation generated in 'docs/'.")
	},
}

type indexHealthMetrics struct {
	ExpectedChunks int
	IndexedChunks  int
	MissingChunks  int
	StaleChunks    int
	Coverage       float64
	Freshness      float64
	StaleRatio     float64
	ChunkFiles     int
}

func assessIndexHealth(ctx context.Context, engine *knowledge.Engine, store *storage.SQLiteStore) (map[string]bool, indexHealthMetrics, error) {
	expectedSet := make(map[string]bool)
	for _, c := range engine.PrepareSearchChunks() {
		id := strings.TrimSpace(c.ID)
		if id == "" {
			continue
		}
		expectedSet[id] = true
	}
	metrics, _, err := reassessIndexHealth(ctx, store, expectedSet)
	return expectedSet, metrics, err
}

func reassessIndexHealth(ctx context.Context, store *storage.SQLiteStore, expectedSet map[string]bool) (indexHealthMetrics, []string, error) {
	indexedIDs, err := store.ListChunkIDs(ctx)
	if err != nil {
		return indexHealthMetrics{}, nil, err
	}
	indexedSet := make(map[string]bool, len(indexedIDs))
	for _, id := range indexedIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		indexedSet[id] = true
	}

	missing := 0
	intersection := 0
	for id := range expectedSet {
		if indexedSet[id] {
			intersection++
			continue
		}
		missing++
	}

	staleIDs := make([]string, 0)
	for id := range indexedSet {
		if expectedSet[id] {
			continue
		}
		staleIDs = append(staleIDs, id)
	}

	files, err := store.CountChunkFiles(ctx)
	if err != nil {
		return indexHealthMetrics{}, nil, err
	}

	expectedTotal := len(expectedSet)
	indexedTotal := len(indexedSet)
	coverage := 1.0
	if expectedTotal > 0 {
		coverage = float64(intersection) / float64(expectedTotal)
	}
	denom := maxInt(expectedTotal, indexedTotal)
	freshness := 1.0
	if denom > 0 {
		freshness = 1.0 - (float64(missing+len(staleIDs)) / float64(denom))
	}
	staleRatio := 0.0
	if indexedTotal > 0 {
		staleRatio = float64(len(staleIDs)) / float64(indexedTotal)
	}

	return indexHealthMetrics{
		ExpectedChunks: expectedTotal,
		IndexedChunks:  indexedTotal,
		MissingChunks:  missing,
		StaleChunks:    len(staleIDs),
		Coverage:       clamp01(coverage),
		Freshness:      clamp01(freshness),
		StaleRatio:     clamp01(staleRatio),
		ChunkFiles:     files,
	}, staleIDs, nil
}

func shouldRebuildIndex(m indexHealthMetrics) bool {
	if m.IndexedChunks == 0 {
		return true
	}
	if m.ExpectedChunks == 0 {
		return false
	}
	return m.Freshness < 0.85 || m.Coverage < 0.70 || m.StaleRatio > 0.15
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
