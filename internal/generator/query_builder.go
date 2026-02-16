package generator

import (
	"docod/internal/knowledge"
	"sort"
	"strings"
)

// BuildSectionQueries derives retrieval queries from plan intent and capability labels.
func BuildSectionQueries(plan SectionDocPlan, capabilities []Capability) []string {
	queries := make([]string, 0, 8)
	base := strings.TrimSpace(plan.QueryText())
	if base != "" {
		queries = append(queries, base)
	}
	if goal := strings.TrimSpace(plan.Goal); goal != "" {
		queries = append(queries, goal)
	}
	for _, block := range plan.RequiredBlocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		queries = append(queries, plan.SectionID+" "+block)
	}
	for i, cap := range capabilities {
		if i >= 4 {
			break
		}
		if strings.TrimSpace(cap.Title) != "" {
			queries = append(queries, plan.SectionID+" capability "+cap.Title)
		}
		if strings.TrimSpace(cap.Intent) != "" {
			queries = append(queries, cap.Intent)
		}
	}
	return uniqueNonEmptyQueries(queries)
}

func uniqueNonEmptyQueries(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, q := range in {
		n := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(q)), " "))
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, q)
	}
	return out
}

// DiversityRerank keeps retrieval results representative across files.
func DiversityRerank(chunks []knowledge.SearchChunk, limit int, perFileLimit int) []knowledge.SearchChunk {
	if limit <= 0 || len(chunks) <= limit {
		return chunks
	}
	if perFileLimit <= 0 {
		perFileLimit = 2
	}

	bucketCount := map[string]int{}
	selected := make([]knowledge.SearchChunk, 0, limit)
	deferred := make([]knowledge.SearchChunk, 0, len(chunks))
	for _, c := range chunks {
		fileKey := chunkFileKey(c)
		if bucketCount[fileKey] < perFileLimit {
			selected = append(selected, c)
			bucketCount[fileKey]++
			if len(selected) == limit {
				return selected
			}
			continue
		}
		deferred = append(deferred, c)
	}

	// Fill remaining slots deterministically by semantic richness.
	sort.SliceStable(deferred, func(i, j int) bool {
		si := chunkRichnessScore(deferred[i])
		sj := chunkRichnessScore(deferred[j])
		if si == sj {
			return deferred[i].ID < deferred[j].ID
		}
		return si > sj
	})
	for _, c := range deferred {
		if len(selected) >= limit {
			break
		}
		selected = append(selected, c)
	}
	return selected
}

func chunkFileKey(c knowledge.SearchChunk) string {
	if strings.TrimSpace(c.FilePath) != "" {
		return c.FilePath
	}
	if strings.Contains(c.ID, ":") {
		parts := strings.Split(c.ID, ":")
		if len(parts) > 1 {
			return parts[0]
		}
	}
	return c.ID
}

func chunkRichnessScore(c knowledge.SearchChunk) int {
	score := 0
	if strings.TrimSpace(c.Description) != "" {
		score += 2
	}
	if strings.TrimSpace(c.Signature) != "" {
		score += 2
	}
	if strings.TrimSpace(c.Content) != "" {
		score += 2
	}
	score += len(c.Dependencies)
	score += len(c.UsedBy)
	switch c.UnitType {
	case "function", "method", "struct", "interface":
		score += 2
	case "file_module":
		score -= 1
	}
	return score
}

func buildEvidenceStats(plan SectionDocPlan, queries []string, chunks []knowledge.SearchChunk) *EvidenceRef {
	chunkCount := len(chunks)
	sourceCount := 0
	confidenceSum := 0.0
	confidenceN := 0.0
	fileSet := map[string]bool{}

	for _, c := range chunks {
		fileKey := chunkFileKey(c)
		if strings.TrimSpace(fileKey) != "" {
			fileSet[fileKey] = true
		}
		if len(c.Sources) == 0 {
			continue
		}
		for _, src := range c.Sources {
			sourceCount++
			if src.Confidence > 0 {
				confidenceSum += src.Confidence
				confidenceN++
			}
		}
	}
	if sourceCount == 0 {
		sourceCount = chunkCount
	}

	minEvidence := plan.MinEvidence
	if minEvidence <= 0 {
		minEvidence = 1
	}
	coverage := float64(chunkCount) / float64(minEvidence)
	if coverage > 1 {
		coverage = 1
	}
	if coverage < 0 {
		coverage = 0
	}

	baseConfidence := 0.55
	if confidenceN > 0 {
		baseConfidence = confidenceSum / confidenceN
	}
	diversityBonus := 0.0
	if chunkCount > 0 {
		diversityBonus = 0.2 * (float64(len(fileSet)) / float64(chunkCount))
	}
	confidence := baseConfidence + diversityBonus
	if confidence > 1 {
		confidence = 1
	}
	if confidence < 0 {
		confidence = 0
	}

	return &EvidenceRef{
		Coverage:    coverage,
		Confidence:  confidence,
		ChunkCount:  chunkCount,
		SourceCount: sourceCount,
		QueryCount:  len(queries),
		LowEvidence: coverage < 0.7 || confidence < 0.6,
	}
}
