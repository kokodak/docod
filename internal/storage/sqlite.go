package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"

	"docod/internal/extractor"
	"docod/internal/graph"
	"docod/internal/knowledge"

	_ "github.com/mattn/go-sqlite3"
)

type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates or opens a SQLite database.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	s := &SQLiteStore{db: db}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to init schema: %w", err)
	}

	return s, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) initSchema() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS nodes (
			id TEXT PRIMARY KEY,
			name TEXT,
			package TEXT,
			unit_type TEXT,
			filepath TEXT,
			start_line INTEGER,
			end_line INTEGER,
			content TEXT,
			content_hash TEXT,
			description TEXT,
			details JSON
		);`,
		`CREATE TABLE IF NOT EXISTS edges (
			from_id TEXT,
			to_id TEXT,
			kind TEXT,
			PRIMARY KEY (from_id, to_id, kind)
		);`,
		`CREATE TABLE IF NOT EXISTS chunks (
			id TEXT PRIMARY KEY,
			content JSON,
			embedding BLOB
		);`,
		`CREATE INDEX IF NOT EXISTS idx_nodes_file ON nodes(filepath);`,
	}

	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// --- CodeGraphStore Implementation ---

func (s *SQLiteStore) SaveNode(ctx context.Context, node *graph.Node) error {
	u := node.Unit
	details, _ := json.Marshal(u.Details)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO nodes (id, name, package, unit_type, filepath, start_line, end_line, content, content_hash, description, details)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			package=excluded.package,
			unit_type=excluded.unit_type,
			filepath=excluded.filepath,
			start_line=excluded.start_line,
			end_line=excluded.end_line,
			content=excluded.content,
			content_hash=excluded.content_hash,
			description=excluded.description,
			details=excluded.details
	`, u.ID, u.Name, u.Package, u.UnitType, u.Filepath, u.StartLine, u.EndLine, u.Content, u.ContentHash, u.Description, details)

	return err
}

func (s *SQLiteStore) SaveGraph(ctx context.Context, g *graph.Graph) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Save Nodes
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO nodes (id, name, package, unit_type, filepath, start_line, end_line, content, content_hash, description, details)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			package=excluded.package,
			unit_type=excluded.unit_type,
			filepath=excluded.filepath,
			start_line=excluded.start_line,
			end_line=excluded.end_line,
			content=excluded.content,
			content_hash=excluded.content_hash,
			description=excluded.description,
			details=excluded.details
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, node := range g.Nodes {
		u := node.Unit
		details, _ := json.Marshal(u.Details)
		if _, err := stmt.Exec(u.ID, u.Name, u.Package, u.UnitType, u.Filepath, u.StartLine, u.EndLine, u.Content, u.ContentHash, u.Description, details); err != nil {
			return err
		}
	}

	// 2. Save Edges
	// Insert edges, ignoring duplicates to support incremental updates.
	edgeStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO edges (from_id, to_id, kind) VALUES (?, ?, ?)
		ON CONFLICT(from_id, to_id, kind) DO NOTHING
	`)
	if err != nil {
		return err
	}
	defer edgeStmt.Close()

	for _, edge := range g.Edges {
		if _, err := edgeStmt.Exec(edge.From, edge.To, edge.Kind); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) LoadGraph(ctx context.Context) (*graph.Graph, error) {
	g := graph.NewGraph()

	// 1. Load Nodes
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, package, unit_type, filepath, start_line, end_line, content, content_hash, description, details FROM nodes")
	if err != nil {
		return nil, fmt.Errorf("failed to query nodes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var u extractor.CodeUnit
		var details []byte
		if err := rows.Scan(&u.ID, &u.Name, &u.Package, &u.UnitType, &u.Filepath, &u.StartLine, &u.EndLine, &u.Content, &u.ContentHash, &u.Description, &details); err != nil {
			return nil, fmt.Errorf("failed to scan node: %w", err)
		}
		if len(details) > 0 {
			_ = json.Unmarshal(details, &u.Details)
		}
		g.Nodes[u.ID] = &graph.Node{Unit: &u}
	}

	// Rebuild name index for lookups
	g.RebuildIndices()

	// 2. Load Edges
	edgeRows, err := s.db.QueryContext(ctx, "SELECT from_id, to_id, kind FROM edges")
	if err != nil {
		return nil, fmt.Errorf("failed to query edges: %w", err)
	}
	defer edgeRows.Close()

	for edgeRows.Next() {
		var edge graph.Edge
		if err := edgeRows.Scan(&edge.From, &edge.To, &edge.Kind); err != nil {
			return nil, fmt.Errorf("failed to scan edge: %w", err)
		}
		g.Edges = append(g.Edges, edge)
	}

	return g, nil
}

func (s *SQLiteStore) GetNode(ctx context.Context, id string) (*graph.Node, error) {
	row := s.db.QueryRowContext(ctx, "SELECT id, name, package, unit_type, filepath, start_line, end_line, content, content_hash, description, details FROM nodes WHERE id = ?", id)

	var u extractor.CodeUnit
	var details []byte
	if err := row.Scan(&u.ID, &u.Name, &u.Package, &u.UnitType, &u.Filepath, &u.StartLine, &u.EndLine, &u.Content, &u.ContentHash, &u.Description, &details); err != nil {
		return nil, err
	}
	if len(details) > 0 {
		_ = json.Unmarshal(details, &u.Details)
	}

	return &graph.Node{Unit: &u}, nil
}

func (s *SQLiteStore) FindNodesByFile(ctx context.Context, filepath string) ([]*graph.Node, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, package, unit_type, filepath, start_line, end_line, content, content_hash, description, details FROM nodes WHERE filepath = ?", filepath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*graph.Node
	for rows.Next() {
		var u extractor.CodeUnit
		var details []byte
		if err := rows.Scan(&u.ID, &u.Name, &u.Package, &u.UnitType, &u.Filepath, &u.StartLine, &u.EndLine, &u.Content, &u.ContentHash, &u.Description, &details); err != nil {
			return nil, err
		}
		if len(details) > 0 {
			_ = json.Unmarshal(details, &u.Details)
		}
		nodes = append(nodes, &graph.Node{Unit: &u})
	}
	return nodes, nil
}

// --- VectorStore Implementation ---

func (s *SQLiteStore) SaveEmbeddings(ctx context.Context, items []knowledge.VectorItem) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO chunks (id, content, embedding) VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET content=excluded.content, embedding=excluded.embedding
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, item := range items {
		contentJSON, err := json.Marshal(item.Chunk)
		if err != nil {
			continue
		}

		// Convert []float32 to []byte
		buf := new(bytes.Buffer)
		if err := binary.Write(buf, binary.LittleEndian, item.Embedding); err != nil {
			return err
		}

		if _, err := stmt.Exec(item.Chunk.ID, contentJSON, buf.Bytes()); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) SearchSimilar(ctx context.Context, queryVector []float32, topK int) ([]knowledge.SearchChunk, error) {
	// Naive In-Memory Cosine Similarity
	// For small to medium codebases (up to 10k chunks), this is fast enough (ms range).

	rows, err := s.db.QueryContext(ctx, "SELECT content, embedding FROM chunks")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type candidate struct {
		chunk knowledge.SearchChunk
		score float32
	}
	var candidates []candidate

	for rows.Next() {
		var contentJSON []byte
		var embeddingBlob []byte
		if err := rows.Scan(&contentJSON, &embeddingBlob); err != nil {
			return nil, err
		}

		// Decode Chunk
		var chunk knowledge.SearchChunk
		if err := json.Unmarshal(contentJSON, &chunk); err != nil {
			continue
		}

		// Decode Embedding
		embedding := make([]float32, len(embeddingBlob)/4)
		if err := binary.Read(bytes.NewReader(embeddingBlob), binary.LittleEndian, &embedding); err != nil {
			continue
		}

		score := cosineSimilarity(queryVector, embedding)
		candidates = append(candidates, candidate{chunk: chunk, score: score})
	}

	// Sort by score descending
	// Simple insertion sort for TopK or full sort
	// Using generic slice sort for simplicity
	// Note: In a real prod environment, use a heap for TopK
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[i].score < candidates[j].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	if len(candidates) > topK {
		candidates = candidates[:topK]
	}

	result := make([]knowledge.SearchChunk, len(candidates))
	for i, c := range candidates {
		result[i] = c.chunk
	}

	return result, nil
}

// Add implements knowledge.Indexer interface
func (s *SQLiteStore) Add(ctx context.Context, items []knowledge.VectorItem) error {
	return s.SaveEmbeddings(ctx, items)
}

// Delete implements knowledge.Indexer interface
func (s *SQLiteStore) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := "DELETE FROM chunks WHERE id = ?"
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, id := range ids {
		if _, err := stmt.Exec(id); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Search implements knowledge.Indexer interface
func (s *SQLiteStore) Search(ctx context.Context, queryVector []float32, topK int) ([]knowledge.VectorItem, error) {
	chunks, err := s.SearchSimilar(ctx, queryVector, topK)
	if err != nil {
		return nil, err
	}
	
	// Convert SearchChunk to VectorItem.
	var items []knowledge.VectorItem
	for _, c := range chunks {
		items = append(items, knowledge.VectorItem{Chunk: c})
	}
	return items, nil
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, magA, magB float32
	for i := 0; i < len(a); i++ {
		dot += a[i] * b[i]
		magA += a[i] * a[i]
		magB += b[i] * b[i]
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (float32(math.Sqrt(float64(magA))) * float32(math.Sqrt(float64(magB))))
}
