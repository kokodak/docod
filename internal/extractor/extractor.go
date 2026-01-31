package extractor

import (
	"context"
	"fmt"
	"os"

	sitter "github.com/smacker/go-tree-sitter"
)

// Extractor orchestrates the extraction process using language-specific extractors.
type Extractor struct {
	langExtractor LanguageExtractor
	langName      string
}

// NewExtractor creates a new extractor for a given language.
func NewExtractor(lang string) (*Extractor, error) {
	var langExt LanguageExtractor
	switch lang {
	case "go":
		langExt = &GoExtractor{}
	default:
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}
	return &Extractor{langExtractor: langExt, langName: lang}, nil
}

// ExtractFromFile parses a single source file and extracts all relevant code units.
func (e *Extractor) ExtractFromFile(filepath string) ([]*CodeUnit, error) {
	sourceCode, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filepath, err)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(e.langExtractor.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, sourceCode)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file %s: %w", filepath, err)
	}

	// Step 1: Detect Package/Module name if possible (generic enough for now)
	packageName := e.detectPackageName(tree.RootNode(), sourceCode)

	var codeUnits []*CodeUnit

	// Step 2: Run language-specific query
	query, err := sitter.NewQuery([]byte(e.langExtractor.GetQuery()), e.langExtractor.GetLanguage())
	if err != nil {
		return nil, fmt.Errorf("failed to create query: %w", err)
	}

	qc := sitter.NewQueryCursor()
	qc.Exec(query, tree.RootNode())

	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}
		for _, c := range m.Captures {
			captureName := query.CaptureNameForId(c.Index)
			unit := e.langExtractor.ExtractUnit(captureName, c.Node, sourceCode, filepath, packageName)
			if unit != nil {
				codeUnits = append(codeUnits, unit)
			}
		}
	}

	return codeUnits, nil
}

func (e *Extractor) detectPackageName(root *sitter.Node, sourceCode []byte) string {
	// Simple package detection for Go. Can be moved to LanguageExtractor if needed.
	if e.langName == "go" {
		pkgQuery, _ := sitter.NewQuery([]byte(`(package_clause (package_identifier) @pkg)`), e.langExtractor.GetLanguage())
		pqc := sitter.NewQueryCursor()
		pqc.Exec(pkgQuery, root)
		if m, ok := pqc.NextMatch(); ok {
			return m.Captures[0].Node.Content(sourceCode)
		}
	}
	return ""
}