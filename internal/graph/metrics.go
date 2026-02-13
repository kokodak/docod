package graph

func (g *Graph) UnresolvedReasonCounts() map[UnresolvedReason]int {
	counts := make(map[UnresolvedReason]int)
	if g == nil {
		return counts
	}
	for _, u := range g.Unresolved {
		reason := u.Reason
		if reason == "" {
			reason = ReasonNoCandidate
		}
		counts[reason]++
	}
	return counts
}
