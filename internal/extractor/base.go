package extractor

import sitter "github.com/smacker/go-tree-sitter"

// CodeUnit is the universal container for any extracted code symbol.
type CodeUnit struct {
	ID          string      `json:"id"`
	Filepath    string      `json:"filepath"`
	Package     string      `json:"package"`
	Language    string      `json:"language"`
	StartLine   int         `json:"start_line"`
	EndLine     int         `json:"end_line"`
	Content     string      `json:"content"`
	UnitType    string      `json:"unit_type"` // e.g., "function", "class", "interface", "variable"
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Details     interface{} `json:"details"` // Language-specific details
}

// LanguageExtractor defines the interface that each language parser must implement.
type LanguageExtractor interface {
	GetLanguage() *sitter.Language
	GetQuery() string
	ExtractUnit(captureName string, node *sitter.Node, sourceCode []byte, filepath string, packageName string) *CodeUnit
}
