package storage

import (
	"context"
	"docod/internal/graph"
	"docod/internal/knowledge"
)

// Store combines graph and vector storage capabilities.
type Store interface {
	CodeGraphStore
	VectorStore
	Close() error
}

// CodeGraphStore defines operations for persisting the dependency graph.
type CodeGraphStore interface {
	// SaveNode upserts a node into the database.
	SaveNode(ctx context.Context, node *graph.Node) error
	
	// SaveGraph persists the entire graph structure (nodes and edges).
	SaveGraph(ctx context.Context, g *graph.Graph) error
	
	// GetNode retrieves a node by its ID.
	GetNode(ctx context.Context, id string) (*graph.Node, error)
	
	// FindNodesByFile retrieves all nodes belonging to a specific file.
	FindNodesByFile(ctx context.Context, filepath string) ([]*graph.Node, error)
}

// VectorStore defines operations for semantic search.
type VectorStore interface {
	// SaveEmbeddings stores code chunks with their vector representations.
	SaveEmbeddings(ctx context.Context, items []knowledge.VectorItem) error
	
	// SearchSimilar finds chunks semantically similar to the query vector.
	SearchSimilar(ctx context.Context, vector []float32, topK int) ([]knowledge.SearchChunk, error)
}
