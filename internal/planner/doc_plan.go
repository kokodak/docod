package planner

import (
	"sort"

	"docod/internal/generator"
	"docod/internal/retrieval"
)

// DocUpdatePlan decides which sections to update first.
type DocUpdatePlan struct {
	TriggeredSymbolIDs []string
	TriggeredFiles     []string
	AffectedSections   []SectionImpact
	UnmatchedSymbols   []string
}

// SectionImpact captures why a section should be updated.
type SectionImpact struct {
	SectionID      string
	Score          float64
	Confidence     float64
	Reasons        []string
	TriggerSymbols []string
	TriggerFiles   []string
}

func BuildDocUpdatePlan(model *generator.DocModel, sg *retrieval.Subgraph) *DocUpdatePlan {
	plan := &DocUpdatePlan{}
	if sg == nil {
		return plan
	}

	plan.TriggeredSymbolIDs = append(plan.TriggeredSymbolIDs, sg.NodeIDs...)
	plan.TriggeredFiles = append(plan.TriggeredFiles, sg.UpdatedFiles...)

	if model == nil || len(model.Sections) == 0 || len(sg.NodeIDs) == 0 {
		plan.UnmatchedSymbols = append(plan.UnmatchedSymbols, sg.NodeIDs...)
		return plan
	}

	symbolSet := toSet(sg.NodeIDs)
	fileSet := toSet(sg.UpdatedFiles)
	matchedSymbolSet := make(map[string]bool)

	impacts := make([]SectionImpact, 0)

	for _, section := range model.Sections {
		impact := SectionImpact{SectionID: section.ID}
		reasonSet := make(map[string]bool)
		symbolHit := make(map[string]bool)
		fileHit := make(map[string]bool)
		confSum := 0.0
		confCount := 0.0

		for _, src := range section.Sources {
			if src.SymbolID != "" && symbolSet[src.SymbolID] {
				symConf := normalizeConfidence(sg.NodeScores[src.SymbolID], 0.45)
				srcConf := normalizeConfidence(src.Confidence, symConf)
				combined := (symConf + srcConf) / 2.0
				impact.Score += 1.0 + (0.2 * combined)
				reasonSet["symbol_source_match"] = true
				symbolHit[src.SymbolID] = true
				matchedSymbolSet[src.SymbolID] = true
				confSum += combined
				confCount++
			}
			if src.FilePath != "" && fileSet[src.FilePath] {
				impact.Score += 0.35
				reasonSet["file_source_match"] = true
				fileHit[src.FilePath] = true
				confSum += 0.3
				confCount++
			}
		}

		if impact.Score <= 0 {
			continue
		}

		impact.Reasons = sortedSetKeys(reasonSet)
		impact.TriggerSymbols = sortedSetKeys(symbolHit)
		impact.TriggerFiles = sortedSetKeys(fileHit)
		if confCount > 0 {
			impact.Confidence = confSum / confCount
		}
		impacts = append(impacts, impact)
	}

	sort.Slice(impacts, func(i, j int) bool {
		if impacts[i].Confidence == impacts[j].Confidence {
			if impacts[i].Score == impacts[j].Score {
				return impacts[i].SectionID < impacts[j].SectionID
			}
			return impacts[i].Score > impacts[j].Score
		}
		return impacts[i].Confidence > impacts[j].Confidence
	})

	unmatched := make([]string, 0)
	for _, id := range sg.NodeIDs {
		if !matchedSymbolSet[id] {
			unmatched = append(unmatched, id)
		}
	}
	sort.Strings(unmatched)

	plan.AffectedSections = impacts
	plan.UnmatchedSymbols = unmatched
	return plan
}

func normalizeConfidence(value float64, fallback float64) float64 {
	if value <= 0 {
		return fallback
	}
	if value > 1 {
		return 1
	}
	return value
}

func toSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		out[v] = true
	}
	return out
}

func sortedSetKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
