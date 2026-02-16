package knowledge

import (
	"context"
	"encoding/gob"
	"math"
	"os"
	"sort"
	"strings"

	"docod/internal/graph"
)

// MemoryIndex is a simple in-memory vector storage with hash-based caching and graph awareness.
type MemoryIndex struct {
	items         []VectorItem
	indexByID     map[string]int
	contentHashes map[string]string
	graph         *graph.Graph // Reference to the dependency graph for hybrid search
}

func NewMemoryIndex(g *graph.Graph) *MemoryIndex {
	return &MemoryIndex{
		items:         []VectorItem{},
		indexByID:     make(map[string]int),
		contentHashes: make(map[string]string),
		graph:         g,
	}
}

func (m *MemoryIndex) Add(ctx context.Context, items []VectorItem) error {
	for _, item := range items {
		id := strings.TrimSpace(item.Chunk.ID)
		if id == "" {
			continue
		}
		if idx, ok := m.indexByID[id]; ok {
			m.items[idx] = item
		} else {
			m.indexByID[id] = len(m.items)
			m.items = append(m.items, item)
		}
		m.contentHashes[id] = item.Chunk.ContentHash
	}
	return nil
}

// Delete removes items from the index.
func (m *MemoryIndex) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	idSet := make(map[string]bool)
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		idSet[id] = true
	}

	var newItems []VectorItem
	for _, item := range m.items {
		id := item.Chunk.ID
		filePath := strings.TrimSpace(item.Chunk.FilePath)
		if !idSet[id] && (filePath == "" || !idSet[filePath]) {
			newItems = append(newItems, item)
		} else {
			delete(m.contentHashes, id)
		}
	}
	m.items = newItems
	m.indexByID = make(map[string]int, len(m.items))
	for i, item := range m.items {
		m.indexByID[item.Chunk.ID] = i
	}
	return nil
}

// Save persists the index to a file.
func (m *MemoryIndex) Save(filepath string) error {
	f, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer f.Close()
	// Graph is reconstructed from source, so we only persist items
	return gob.NewEncoder(f).Encode(m.items)
}

// Load restores the index from a file.
func (m *MemoryIndex) Load(filepath string) error {
	f, err := os.Open(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	var loadedItems []VectorItem
	if err := gob.NewDecoder(f).Decode(&loadedItems); err != nil {
		return err
	}

	m.items = loadedItems
	m.indexByID = make(map[string]int)
	m.contentHashes = make(map[string]string)
	for i, item := range m.items {
		m.indexByID[item.Chunk.ID] = i
		m.contentHashes[item.Chunk.ID] = item.Chunk.ContentHash
	}
	return nil
}

func (m *MemoryIndex) GetContentHashes(ctx context.Context, ids []string) (map[string]string, error) {
	out := make(map[string]string)
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if h, ok := m.contentHashes[id]; ok {
			out[id] = h
		}
	}
	return out, nil
}

// Search implements Indexer and performs hybrid search (vector + graph proximity).
func (m *MemoryIndex) Search(ctx context.Context, queryVector []float32, topK int) ([]VectorItem, error) {
	return m.searchWithSource(ctx, queryVector, topK, "")
}

// searchWithSource performs hybrid search; sourceID boosts graph-neighbor scores.
func (m *MemoryIndex) searchWithSource(_ context.Context, queryVector []float32, topK int, sourceID string) ([]VectorItem, error) {
	if len(m.items) == 0 {
		return nil, nil
	}

	type scoreItem struct {
		item  VectorItem
		score float32
	}
	scores := make([]scoreItem, 0, len(m.items))

	// Pre-calculate graph distances if sourceID is valid
	distances := make(map[string]int)
	if sourceID != "" && m.graph != nil {
		distances = m.bfsDistances(sourceID, 2) // Limit depth to 2 hops
	}

	for _, item := range m.items {
		// 1. Vector Similarity (0.0 ~ 1.0)
		vecScore := cosineSimilarity(queryVector, item.Embedding)

		// 2. Graph Proximity Boost
		// Direct neighbor (dist=1): +0.2
		// 2-hop neighbor (dist=2): +0.1
		graphBoost := float32(0.0)
		if dist, ok := distances[item.Chunk.ID]; ok {
			switch dist {
			case 1:
				graphBoost = 0.2
			case 2:
				graphBoost = 0.1
			}
		}

		finalScore := vecScore + graphBoost
		scores = append(scores, scoreItem{item: item, score: finalScore})
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	limit := topK
	if limit > len(scores) {
		limit = len(scores)
	}

	results := make([]VectorItem, 0, limit)
	for i := 0; i < limit; i++ {
		results = append(results, scores[i].item)
	}

	return results, nil
}

// bfsDistances calculates shortest path distances from startNode up to maxDepth.
func (m *MemoryIndex) bfsDistances(startID string, maxDepth int) map[string]int {
	dists := make(map[string]int)
	if m.graph == nil {
		return dists
	}

	// BFS queue: [NodeID, Depth]
	type queueItem struct {
		id    string
		depth int
	}
	queue := []queueItem{{id: startID, depth: 0}}
	visited := map[string]bool{startID: true}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr.depth > 0 {
			dists[curr.id] = curr.depth
		}

		if curr.depth >= maxDepth {
			continue
		}

		// Check Dependencies (Outgoing edges)
		for _, dep := range m.graph.GetDependencies(curr.id) {
			if !visited[dep.Unit.ID] {
				visited[dep.Unit.ID] = true
				queue = append(queue, queueItem{id: dep.Unit.ID, depth: curr.depth + 1})
			}
		}

		// Check Dependents (Incoming edges) - context flows both ways
		for _, dep := range m.graph.GetDependents(curr.id) {
			if !visited[dep.Unit.ID] {
				visited[dep.Unit.ID] = true
				queue = append(queue, queueItem{id: dep.Unit.ID, depth: curr.depth + 1})
			}
		}
	}
	return dists
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += float64(a[i] * b[i])
		normA += float64(a[i] * a[i])
		normB += float64(b[i] * b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dotProduct / (math.Sqrt(normA) * math.Sqrt(normB)))
}
