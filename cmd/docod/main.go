package main

import (
	"context"
	"fmt"
	"log"
	"os"
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
func initEngine(ctx context.Context, g *graph.Graph, store *storage.SQLiteStore) (*knowledge.Engine, *knowledge.GeminiSummarizer, error) {
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.AI.APIKey == "" {
		return nil, nil, fmt.Errorf("AI API key not configured")
	}

	// 1. Setup Embedder
	embedder, err := knowledge.NewGeminiEmbedder(ctx, cfg.AI.APIKey, cfg.AI.Model, cfg.AI.Dimension)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	// 2. Setup Summarizer
	summarizer, err := knowledge.NewGeminiSummarizer(ctx, cfg.AI.APIKey, cfg.AI.SummaryModel)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create summarizer: %w", err)
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

		// 1. Initialize Store
		store, err := initStore()
		if err != nil {
			log.Fatalf("Failed to initialize database: %v", err)
		}
		defer store.Close()

		fmt.Println("🔄 Loading knowledge graph...")
		g, err := store.LoadGraph(ctx)
		if err != nil {
			log.Fatalf("Failed to load graph: %v", err)
		}

		// 2. Initialize Engine & Summarizer
		engine, summarizer, err := initEngine(ctx, g, store)
		if err != nil {
			log.Fatalf("Setup failed: %v\nCheck your config.yaml and API keys.", err)
		}

		// 3. Generate
		fmt.Println("🚀 Generating documentation...")
		gen := generator.NewMarkdownGenerator(engine, summarizer)
		if err := gen.GenerateDocs(ctx, "docs"); err != nil {
			log.Fatalf("Failed to generate docs: %v", err)
		}

		fmt.Println("✅ Documentation generated in 'docs/'.")
	},
}
