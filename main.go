package main

import (
	"context"
	"fmt"
	"log"

	"docod/internal/config"
	"docod/internal/crawler"
	"docod/internal/extractor"
	"docod/internal/generator"
	"docod/internal/graph"
	"docod/internal/knowledge"
)

func main() {
	// 1. Load Configuration
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ctx := context.Background()

	// 2. Initialize Components
	ext, err := extractor.NewExtractor("go")
	if err != nil {
		log.Fatalf("Failed to create extractor: %v", err)
	}

	cr := crawler.NewCrawler(ext)
	g := graph.NewGraph()

	// 3. Scan Project
	fmt.Printf("🚀 Scanning project at %s...\n", cfg.Project.Root)
	err = cr.ScanProject(cfg.Project.Root, func(unit *extractor.CodeUnit) {
		g.AddUnit(unit)
	})
	if err != nil {
		log.Fatalf("Failed to scan project: %v", err)
	}

	// 4. Build Graph
	fmt.Println("🔗 Linking relations...")
	g.LinkRelations()
	fmt.Printf("✅ Found %d code units\n", len(g.Nodes))

	// 5. Initialize AI Knowledge Engine
	var embedder knowledge.Embedder
	switch cfg.AI.Provider {
	case "gemini":
		if cfg.AI.APIKey == "" {
			log.Fatal("Gemini API Key is required (set DOCOD_API_KEY env or in config.yaml)")
		}
		embedder, err = knowledge.NewGeminiEmbedder(ctx, cfg.AI.APIKey, cfg.AI.Model, cfg.AI.Dimension)
		if err != nil {
			log.Fatalf("Failed to create Gemini embedder: %v", err)
		}
	default:
		log.Fatalf("Unsupported AI provider: %s", cfg.AI.Provider)
	}

	index := knowledge.NewMemoryIndex(g)
	engine := knowledge.NewEngine(g, embedder, index)

	// 6. Indexing
	fmt.Printf("🧠 Indexing knowledge using %s (%s)...\n", cfg.AI.Provider, cfg.AI.Model)
	err = engine.IndexAll(ctx)
	if err != nil {
		log.Fatalf("Failed to index knowledge: %v", err)
	}

	// 7. Documentation Generation
	fmt.Println("📝 Generating documentation...")
	summaryModel := cfg.AI.SummaryModel
	if summaryModel == "" {
		summaryModel = "gemini-2.5-flash-lite"
	}
	summarizer, err := knowledge.NewGeminiSummarizer(ctx, cfg.AI.APIKey, summaryModel)
	if err != nil {
		log.Fatalf("Failed to create summarizer: %v", err)
	}

	gen := generator.NewMarkdownGenerator(engine, summarizer)
	err = gen.GenerateDocs(ctx, "docs")
	if err != nil {
		log.Fatalf("Failed to generate documentation: %v", err)
	}

	fmt.Println("✨ Process complete! Check the 'docs' directory for generated files.")
}
