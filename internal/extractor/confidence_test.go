package extractor

import "testing"

func TestCalibrateRelationConfidence_Bounds(t *testing.T) {
	ev := Evidence{Filepath: "a.go", StartLine: 1, EndLine: 1}
	c := CalibrateRelationConfidence("calls", "ast_heuristic", ev)
	if c <= 0 || c >= 1 {
		t.Fatalf("expected confidence in (0,1), got %f", c)
	}
}

func TestCalibrateRelationConfidence_TypesHigherThanHeuristic(t *testing.T) {
	ev := Evidence{Filepath: "a.go", StartLine: 10, EndLine: 12}
	h := CalibrateRelationConfidence("uses_type", "ast_heuristic", ev)
	tt := CalibrateRelationConfidence("uses_type", "types", ev)
	if tt <= h {
		t.Fatalf("expected types confidence > heuristic (%f <= %f)", tt, h)
	}
}

func TestCalibrateRelationConfidence_EvidencePenalty(t *testing.T) {
	withEv := CalibrateRelationConfidence("calls", "ast_heuristic", Evidence{Filepath: "a.go", StartLine: 3, EndLine: 3})
	noEv := CalibrateRelationConfidence("calls", "ast_heuristic", Evidence{})
	if noEv >= withEv {
		t.Fatalf("expected missing evidence to reduce confidence (%f >= %f)", noEv, withEv)
	}
}
