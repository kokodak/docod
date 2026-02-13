package ir

// Evidence describes where a node/edge claim originated in source code.
type Evidence struct {
	Filepath    string `json:"filepath"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	StartColumn int    `json:"start_column,omitempty"`
	EndColumn   int    `json:"end_column,omitempty"`
}

// RawSymbolIR is parser-level symbol data before semantic resolution.
type RawSymbolIR struct {
	ID          string    `json:"id"`
	Language    string    `json:"language"`
	Package     string    `json:"package"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Signature   string    `json:"signature,omitempty"`
	ContentHash string    `json:"content_hash,omitempty"`
	Doc         string    `json:"doc,omitempty"`
	Evidence    Evidence  `json:"evidence"`
	Relations   []RawEdge `json:"relations,omitempty"`
}

// RawEdge is a parser-level relationship candidate that may need resolution.
type RawEdge struct {
	Kind       string   `json:"kind"`
	TargetHint string   `json:"target_hint"`
	Evidence   Evidence `json:"evidence"`
}

// SemanticSymbolIR is a resolved symbol identity suitable for graph construction.
type SemanticSymbolIR struct {
	ID          string   `json:"id"`
	Language    string   `json:"language"`
	ModulePath  string   `json:"module_path,omitempty"`
	PackagePath string   `json:"package_path"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Receiver    string   `json:"receiver,omitempty"`
	Signature   string   `json:"signature,omitempty"`
	Evidence    Evidence `json:"evidence"`
}

// SemanticEdgeIR is a resolved relationship between semantic symbols.
type SemanticEdgeIR struct {
	FromID     string   `json:"from_id"`
	ToID       string   `json:"to_id"`
	Kind       string   `json:"kind"`
	Confidence float64  `json:"confidence"`
	Resolver   string   `json:"resolver"` // e.g. "types", "heuristic"
	Evidence   Evidence `json:"evidence"`
}

// GraphSnapshot is the persisted graph-oriented view derived from Semantic IR.
type GraphSnapshot struct {
	Version string             `json:"version"`
	Nodes   []SemanticSymbolIR `json:"nodes"`
	Edges   []SemanticEdgeIR   `json:"edges"`
}
