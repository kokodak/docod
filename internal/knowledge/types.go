package knowledge

import (
	"context"
)

// Embedder defines the interface for converting text to vectors.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
}

// Summarizer defines the interface for generating hierarchical documentation.
type Summarizer interface {
	// SummarizeProject generates a high-level overview of the entire project.
	SummarizeProject(ctx context.Context, allChunks []SearchChunk) (string, error)
	// SummarizePackage generates an architectural overview of a specific package.
	SummarizePackage(ctx context.Context, pkgName string, pkgChunks []SearchChunk) (string, error)
	// SummarizeUnit provides a deep dive into a specific component.
	SummarizeUnit(ctx context.Context, unit SearchChunk, codeBody string, contextUnits []SearchChunk) (string, error)
	// SummarizeFeatures identifies key features and groups components.
	SummarizeFeatures(ctx context.Context, allChunks []SearchChunk) (string, error)
	// SummarizeGettingStarted generates a setup and usage guide.
	SummarizeGettingStarted(ctx context.Context, allChunks []SearchChunk) (string, error)
}

// VectorItem represents a chunk paired with its embedding.
type VectorItem struct {
	Chunk     SearchChunk
	Embedding []float32
}

// Indexer manages the storage and retrieval of VectorItems.
type Indexer interface {
	Add(ctx context.Context, items []VectorItem) error
	Search(ctx context.Context, queryVector []float32, topK int) ([]VectorItem, error)
}
