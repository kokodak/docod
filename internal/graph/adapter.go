package graph

import "docod/internal/extractor"

// FromCodeUnit converts extractor output into graph-domain Symbol.
func FromCodeUnit(unit *extractor.CodeUnit) *Symbol {
	if unit == nil {
		return nil
	}

	s := &Symbol{
		ID:          unit.ID,
		Filepath:    unit.Filepath,
		Package:     unit.Package,
		Language:    unit.Language,
		StartLine:   unit.StartLine,
		EndLine:     unit.EndLine,
		Content:     unit.Content,
		ContentHash: unit.ContentHash,
		UnitType:    unit.UnitType,
		Role:        unit.Role,
		Name:        unit.Name,
		Description: unit.Description,
		Metadata: SymbolMetadata{
			Signature: extractSignature(unit),
			Receiver:  extractReceiver(unit),
		},
	}

	if len(unit.Relations) > 0 {
		s.Relations = make([]Relation, 0, len(unit.Relations))
		for _, rel := range unit.Relations {
			s.Relations = append(s.Relations, Relation{
				Target:     rel.Target,
				Kind:       RelationKind(rel.Kind),
				Resolver:   rel.Resolver,
				Confidence: rel.Confidence,
				Evidence: Evidence{
					Filepath:  rel.Evidence.Filepath,
					StartLine: rel.Evidence.StartLine,
					EndLine:   rel.Evidence.EndLine,
				},
			})
		}
	}

	return s
}

func extractSignature(unit *extractor.CodeUnit) string {
	if unit == nil || unit.Details == nil {
		return ""
	}
	switch d := unit.Details.(type) {
	case extractor.GoFunctionDetails:
		return d.Signature
	case *extractor.GoFunctionDetails:
		if d != nil {
			return d.Signature
		}
	}
	return ""
}

func extractReceiver(unit *extractor.CodeUnit) string {
	if unit == nil || unit.Details == nil {
		return ""
	}
	switch d := unit.Details.(type) {
	case extractor.GoFunctionDetails:
		return d.Receiver
	case *extractor.GoFunctionDetails:
		if d != nil {
			return d.Receiver
		}
	}
	return ""
}

// AddUnit is a compatibility adapter to keep existing callers stable.
func (g *Graph) AddUnit(unit *extractor.CodeUnit) {
	g.AddSymbol(FromCodeUnit(unit))
}
