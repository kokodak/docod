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
	SummarizeFullDoc(ctx context.Context, archChunks, featChunks, confChunks []SearchChunk) (string, error)
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
