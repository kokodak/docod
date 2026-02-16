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
	UpdateDocSection(ctx context.Context, currentContent string, relevantCode []SearchChunk) (string, error)
	RenderSectionFromDraft(ctx context.Context, draftJSON string, relevantCode []SearchChunk) (string, error)
	GenerateNewSection(ctx context.Context, relevantCode []SearchChunk) (string, error)
	FindInsertionPoint(ctx context.Context, toc []string, newContent string) (int, error)
}

// VectorItem represents a chunk paired with its embedding.
type VectorItem struct {
	Chunk     SearchChunk
	Embedding []float32
}

// Indexer manages the storage and retrieval of VectorItems.
type Indexer interface {
	Add(ctx context.Context, items []VectorItem) error
	Delete(ctx context.Context, ids []string) error
	Search(ctx context.Context, queryVector []float32, topK int) ([]VectorItem, error)
}

// IndexContentHashReader is an optional capability for index implementations.
// It allows callers to skip embedding work when chunk content has not changed.
type IndexContentHashReader interface {
	GetContentHashes(ctx context.Context, ids []string) (map[string]string, error)
}
