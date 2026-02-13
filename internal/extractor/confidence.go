package extractor

func CalibrateRelationConfidence(kind string, resolver string, evidence Evidence) float64 {
	base := baseConfidence(kind)

	switch resolver {
	case "types":
		base += 0.18
	case "ast_heuristic":
		// no-op
	default:
		base -= 0.03
	}

	if evidence.Filepath == "" {
		base -= 0.05
	}
	if evidence.StartLine <= 0 || evidence.EndLine < evidence.StartLine {
		base -= 0.05
	}

	return clamp(base, 0.1, 0.99)
}

func baseConfidence(kind string) float64 {
	switch kind {
	case "belongs_to":
		return 0.8
	case "instantiates":
		return 0.72
	case "calls":
		return 0.7
	case "uses_type":
		return 0.65
	case "embeds":
		return 0.6
	default:
		return 0.55
	}
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
