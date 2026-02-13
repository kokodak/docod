package graph

type RelationKind string

const (
	RelationCalls        RelationKind = "calls"
	RelationUsesType     RelationKind = "uses_type"
	RelationBelongsTo    RelationKind = "belongs_to"
	RelationInstantiates RelationKind = "instantiates"
	RelationEmbeds       RelationKind = "embeds"
)

type UnresolvedReason string

const (
	ReasonNoCandidate   UnresolvedReason = "no_candidate"
	ReasonAmbiguous     UnresolvedReason = "ambiguous"
	ReasonTypecheckFail UnresolvedReason = "typecheck_failed"
	ReasonSourceMissing UnresolvedReason = "source_missing"
)

type SymbolMetadata struct {
	Signature string `json:"signature,omitempty"`
	Receiver  string `json:"receiver,omitempty"`
}

// Symbol is the graph-domain node payload.
// It is intentionally decoupled from extractor.CodeUnit.
type Symbol struct {
	ID          string         `json:"id"`
	Filepath    string         `json:"filepath"`
	Package     string         `json:"package"`
	Language    string         `json:"language"`
	StartLine   int            `json:"start_line"`
	EndLine     int            `json:"end_line"`
	Content     string         `json:"content"`
	ContentHash string         `json:"content_hash"`
	UnitType    string         `json:"unit_type"`
	Role        string         `json:"role"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Metadata    SymbolMetadata `json:"metadata,omitempty"`
	Relations   []Relation     `json:"relations,omitempty"`
}

type Evidence struct {
	Filepath  string `json:"filepath,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

type Relation struct {
	Target     string       `json:"target"`
	Kind       RelationKind `json:"kind"`
	Resolver   string       `json:"resolver,omitempty"`
	Confidence float64      `json:"confidence,omitempty"`
	Evidence   Evidence     `json:"evidence,omitempty"`
}
