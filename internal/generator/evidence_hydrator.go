package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"docod/internal/knowledge"
)

type draftLLMBudget struct {
	MaxLayerAChunks int
	MaxLayerBBlocks int
	MaxBlockLines   int
	MinFlowBlocks   int
}

func defaultDraftLLMBudget() draftLLMBudget {
	return draftLLMBudget{
		MaxLayerAChunks: 8,
		MaxLayerBBlocks: 4,
		MaxBlockLines:   60,
		MinFlowBlocks:   2,
	}
}

// BuildDraftLLMContext builds two-level evidence context:
// - Layer A: concise semantic chunks across claims
// - Layer B: hydrated code blocks for low-confidence or flow-sensitive claims
func BuildDraftLLMContext(draft SectionDraft, chunks []knowledge.SearchChunk) []knowledge.SearchChunk {
	budget := defaultDraftLLMBudget()
	layerA := buildLayerAContext(draft, chunks, budget.MaxLayerAChunks)
	layerB := buildLayerBContext(draft, budget.MaxLayerBBlocks, budget.MaxBlockLines, budget.MinFlowBlocks)
	return mergeChunkLists(layerA, layerB, budget.MaxLayerAChunks+budget.MaxLayerBBlocks)
}

func buildLayerAContext(draft SectionDraft, chunks []knowledge.SearchChunk, limit int) []knowledge.SearchChunk {
	if len(chunks) == 0 || limit <= 0 {
		return nil
	}
	ranked := append([]knowledge.SearchChunk(nil), chunks...)
	sort.SliceStable(ranked, func(i, j int) bool {
		si := chunkRichnessScore(ranked[i])
		sj := chunkRichnessScore(ranked[j])
		if si == sj {
			return ranked[i].ID < ranked[j].ID
		}
		return si > sj
	})
	ranked = topNChunks(ranked, limit)

	out := make([]knowledge.SearchChunk, 0, len(ranked))
	for _, c := range ranked {
		summary := strings.TrimSpace(c.Description)
		if summary == "" {
			summary = strings.TrimSpace(c.Signature)
		}
		layer := c
		layer.Content = summarizeEvidenceChunk(c)
		if summary != "" {
			layer.Description = summary
		}
		out = append(out, layer)
	}
	return out
}

func buildLayerBContext(draft SectionDraft, maxBlocks int, maxLines int, minFlowBlocks int) []knowledge.SearchChunk {
	if len(draft.Claims) == 0 || maxBlocks <= 0 {
		return nil
	}

	claims := append([]DraftClaim(nil), draft.Claims...)
	sort.SliceStable(claims, func(i, j int) bool {
		wi := claimHydrationWeight(claims[i])
		wj := claimHydrationWeight(claims[j])
		if wi == wj {
			return claims[i].ID < claims[j].ID
		}
		return wi > wj
	})

	out := make([]knowledge.SearchChunk, 0, maxBlocks)
	seen := make(map[string]bool)
	flowAdded := 0

	// Pass 1: guarantee a minimum number of flow-oriented evidence blocks.
	for _, claim := range claims {
		if len(out) >= maxBlocks || flowAdded >= minFlowBlocks {
			break
		}
		if !isFlowClaim(claim) {
			continue
		}
		added := collectHydratedBlocks(claim, maxLines, maxBlocks, seen, &out)
		flowAdded += added
	}

	// Pass 2: fill remaining budget by hydration priority.
	for _, claim := range claims {
		if len(out) >= maxBlocks {
			break
		}
		if claimHydrationWeight(claim) <= 0 {
			continue
		}
		_ = collectHydratedBlocks(claim, maxLines, maxBlocks, seen, &out)
	}
	return out
}

func collectHydratedBlocks(claim DraftClaim, maxLines int, maxBlocks int, seen map[string]bool, out *[]knowledge.SearchChunk) int {
	added := 0
	for _, src := range claim.Sources {
		if len(*out) >= maxBlocks {
			break
		}
		key := src.FilePath + ":" + fmt.Sprintf("%d-%d", src.StartLine, src.EndLine)
		if src.FilePath == "" || seen[key] {
			continue
		}
		block, ok := hydrateSourceBlock(claim, src, maxLines)
		if !ok {
			continue
		}
		seen[key] = true
		*out = append(*out, block)
		added++
	}
	return added
}

func claimHydrationWeight(c DraftClaim) int {
	text := strings.ToLower(c.Text)
	weight := 0
	if c.Confidence < 0.75 {
		weight += 3
	}
	for _, token := range []string{"flow", "pipeline", "sequence", "before", "after", "when", "then", "route"} {
		if strings.Contains(text, token) {
			weight += 2
		}
	}
	return weight
}

func isFlowClaim(c DraftClaim) bool {
	text := strings.ToLower(c.Text)
	for _, token := range []string{"flow", "pipeline", "sequence", "before", "after", "when", "then", "route"} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func hydrateSourceBlock(claim DraftClaim, src SourceRef, maxLines int) (knowledge.SearchChunk, bool) {
	path := strings.TrimSpace(src.FilePath)
	if path == "" {
		return knowledge.SearchChunk{}, false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return knowledge.SearchChunk{}, false
	}
	lines := strings.Split(string(body), "\n")
	if len(lines) == 0 {
		return knowledge.SearchChunk{}, false
	}

	start := src.StartLine
	end := src.EndLine
	if start <= 0 {
		start = 1
	}
	if end < start {
		end = start
	}
	// Add a small context window.
	start -= 4
	end += 4
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	if maxLines > 0 && end-start+1 > maxLines {
		end = start + maxLines - 1
		if end > len(lines) {
			end = len(lines)
		}
	}

	snippet := strings.Join(lines[start-1:end], "\n")
	if strings.TrimSpace(snippet) == "" {
		return knowledge.SearchChunk{}, false
	}

	return knowledge.SearchChunk{
		ID:          fmt.Sprintf("evidence:%s:%d-%d", src.SymbolID, start, end),
		FilePath:    path,
		Name:        src.SymbolID,
		UnitType:    "evidence_block",
		Package:     filepath.Base(filepath.Dir(path)),
		Description: fmt.Sprintf("Hydrated evidence block for claim `%s`.", claim.ID),
		Signature:   fmt.Sprintf("%s:%d-%d", path, start, end),
		Content:     snippet,
		ContentHash: "",
		Sources: []knowledge.ChunkSource{
			{
				SymbolID:   src.SymbolID,
				FilePath:   src.FilePath,
				StartLine:  start,
				EndLine:    end,
				Relation:   src.Relation,
				Confidence: src.Confidence,
			},
		},
	}, true
}

func summarizeEvidenceChunk(c knowledge.SearchChunk) string {
	var sb strings.Builder
	if strings.TrimSpace(c.Signature) != "" {
		sb.WriteString(c.Signature)
		sb.WriteString("\n")
	}
	if strings.TrimSpace(c.Description) != "" {
		sb.WriteString(c.Description)
		sb.WriteString("\n")
	}
	if len(c.Dependencies) > 0 {
		sb.WriteString("Depends on: ")
		sb.WriteString(strings.Join(c.Dependencies, ", "))
		sb.WriteString("\n")
	}
	text := strings.TrimSpace(sb.String())
	if text == "" {
		return strings.TrimSpace(c.Content)
	}
	return text
}
