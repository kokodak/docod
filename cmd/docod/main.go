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
	"docod/internal/git"
	"docod/internal/index"
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
	rootCmd.AddCommand(diffCmd)
}

// initStore initializes the SQLite store.
func initStore() (storage.Store, error) {
	// Ensure config is loaded (even if defaults)
	_, _ = config.LoadConfig("config.yaml")
	
	return storage.NewSQLiteStore(dbPath)
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
		
		fmt.Printf("🎉 Scan complete! Database: %s\n", dbPath)
	},
}

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Analyze git changes and query the knowledge graph",
	Run: func(cmd *cobra.Command, args []string) {
		// 1. Get Local Git Changes
		changes, err := git.GetChangedFiles("HEAD")
		if err != nil {
			log.Fatalf("Failed to get git changes: %v", err)
		}

		if len(changes) == 0 {
			fmt.Println("No changes detected.")
			return
		}

		fmt.Printf("📝 Detected %d changed files.\n", len(changes))

		// 2. Initialize Store
		store, err := initStore()
		if err != nil {
			log.Fatalf("Failed to initialize database: %v", err)
		}
		defer store.Close()

		ctx := context.Background()

		// 3. Simple Impact Analysis (Demo)
		for _, change := range changes {
			fmt.Printf("\nAnalyzing impact for: %s\n", change.Path)
			
			// Find nodes in this file
			nodes, err := store.FindNodesByFile(ctx, change.Path)
			if err != nil {
				log.Printf("  Error querying nodes: %v", err)
				continue
			}
			
			if len(nodes) == 0 {
				fmt.Println("  No knowledge nodes found for this file.")
				continue
			}

			fmt.Printf("  Found %d affected symbols:\n", len(nodes))
			for _, node := range nodes {
				fmt.Printf("  - %s (%s)\n", node.Unit.Name, node.Unit.UnitType)
			}
		}
	},
}