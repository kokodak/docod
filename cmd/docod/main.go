package main

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
	"docod/internal/storage"

	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:   "docod",
		Short: "AI-powered Documentation Agent",
	}
	dbPath string
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

	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(generateCmd)
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

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Incrementally update the knowledge graph and documentation based on git changes",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()

		// 1. Get Local Git Changes
		changes, err := git.GetChangedFiles("HEAD")
		if err != nil {
			log.Fatalf("Failed to get git changes: %v", err)
		}

		if len(changes) == 0 {
			fmt.Println("✅ No changes detected.")
			return
		}

		fmt.Printf("📝 Detected %d changed files.\n", len(changes))

		// 2. Initialize Store & Load Graph
		store, err := initStore()
		if err != nil {
			log.Fatalf("Failed to initialize database: %v", err)
		}
		defer store.Close()

		fmt.Println("🔄 Loading existing knowledge graph...")
		g, err := store.LoadGraph(ctx)
		if err != nil {
			log.Fatalf("Failed to load graph: %v", err)
		}

		// 3. Process Changes
		ext, err := extractor.NewExtractor("go")
		if err != nil {
			log.Fatalf("Failed to create extractor: %v", err)
		}

		nodesUpdated := 0
		nodesRemoved := 0

		for _, change := range changes {
			// Skip non-Go files for now (since we only use Go extractor)
			// TODO: Use a better file filter based on configured languages
			if !strings.HasSuffix(change.Path, ".go") {
				continue
			}

			// Remove old nodes for this file
			// Naive approach: Iterate all nodes. Optimize later with index.
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

			// If file exists (not deleted), parse and add new nodes
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

		// 4. Re-link Graph (Update Edges)
		g.RebuildIndices() // Important after manual map modifications
		g.LinkRelations()

		// 5. Save Updated Graph
		if err := store.SaveGraph(ctx, g); err != nil {
			log.Fatalf("Failed to save updated graph: %v", err)
		}

		// 6. Impact Analysis
		fmt.Println("🔍 Analyzing impact...")
		analyzer := analysis.NewAnalyzer(g)
		report, err := analyzer.AnalyzeImpact(changes)
		if err != nil {
			log.Printf("Analysis warning: %v", err)
		} else {
			fmt.Printf("  -> %d symbols directly affected\n", len(report.DirectlyAffected))
			fmt.Printf("  -> %d symbols indirectly affected (callers)\n", len(report.IndirectlyAffected))
		}

		// 7. Regenerate Documentation (if configured)
		fmt.Println("✍️  Regenerating documentation...")
		engine, summarizer, err := initEngine(ctx, g, store)
		if err != nil {
			fmt.Printf("⚠️  Skipping documentation generation: %v\n", err)
			return
		}

		fmt.Println("🧠 Updating embeddings incrementally...")
		
		var updatedFiles, deletedFiles []string
		for _, change := range changes {
			if _, err := os.Stat(change.Path); os.IsNotExist(err) {
				deletedFiles = append(deletedFiles, change.Path)
			} else {
				updatedFiles = append(updatedFiles, change.Path)
			}
		}

		if err := engine.IndexIncremental(ctx, updatedFiles, deletedFiles); err != nil {
			log.Printf("Warning: Embedding update failed: %v", err)
		}

		// Use DocUpdater for incremental doc update
		docUpdater := generator.NewDocUpdater(engine, summarizer)
		docPath := "docs/documentation.md"
		if _, err := os.Stat(docPath); err == nil {
			fmt.Println("📝 Updating existing documentation sections...")
			if err := docUpdater.UpdateDocs(ctx, docPath, updatedFiles); err != nil {
				log.Printf("Warning: Failed to update docs incrementally, falling back to full gen: %v", err)
				// Fallback to full generation?
				// gen := generator.NewMarkdownGenerator(engine, summarizer)
				// gen.GenerateDocs(ctx, "docs")
			} else {
				fmt.Println("✅ Documentation updated incrementally in 'docs/'.")
				return
			}
		} else {
			fmt.Println("📄 Documentation not found, generating from scratch...")
			gen := generator.NewMarkdownGenerator(engine, summarizer)
			if err := gen.GenerateDocs(ctx, "docs"); err != nil {
				log.Fatalf("Failed to generate docs: %v", err)
			}
			fmt.Println("✅ Documentation generated in 'docs/'.")
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