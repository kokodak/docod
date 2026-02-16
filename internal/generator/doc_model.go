package generator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"docod/internal/knowledge"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
)

const docModelSchemaVersion = "v0.1.0"

var canonicalSectionOrder = []string{"overview", "key-features", "development"}
var (
	schemaCacheMu sync.Mutex
	schemaCache   = make(map[string]*jsonschema.Schema)
)

type DocModel struct {
	SchemaVersion string      `json:"schema_version"`
	Document      ModelDoc    `json:"document"`
	Sections      []ModelSect `json:"sections"`
	Policies      ModelPolicy `json:"policies"`
	Meta          ModelMeta   `json:"meta"`
}

type ModelDoc struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	RootSectionIDs []string `json:"root_section_ids"`
}

type ModelSect struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Level       int          `json:"level"`
	Order       int          `json:"order"`
	ParentID    *string      `json:"parent_id"`
	ContentMD   string       `json:"content_md"`
	Summary     string       `json:"summary,omitempty"`
	Status      string       `json:"status"`
	Sources     []SourceRef  `json:"sources"`
	Evidence    *EvidenceRef `json:"evidence,omitempty"`
	Hash        string       `json:"hash"`
	LastUpdated *UpdateInfo  `json:"last_updated,omitempty"`
}

type EvidenceRef struct {
	Coverage    float64 `json:"coverage"`
	Confidence  float64 `json:"confidence"`
	ChunkCount  int     `json:"chunk_count"`
	SourceCount int     `json:"source_count"`
	QueryCount  int     `json:"query_count"`
	LowEvidence bool    `json:"low_evidence"`
}

type SourceRef struct {
	SymbolID   string  `json:"symbol_id"`
	FilePath   string  `json:"file_path"`
	StartLine  int     `json:"start_line"`
	EndLine    int     `json:"end_line"`
	Relation   string  `json:"relation"`
	CommitSHA  string  `json:"commit_sha,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

type UpdateInfo struct {
	CommitSHA string `json:"commit_sha"`
	PRNumber  *int   `json:"pr_number,omitempty"`
	Timestamp string `json:"timestamp"`
}

type ModelPolicy struct {
	RequiredSectionIDs []string    `json:"required_section_ids"`
	MaxSectionChars    int         `json:"max_section_chars"`
	Style              PolicyStyle `json:"style"`
}

type PolicyStyle struct {
	Tone                       string `json:"tone"`
	Audience                   string `json:"audience"`
	CodeBlockLanguage          string `json:"code_block_language"`
	FocusMode                  string `json:"focus_mode"`
	AvoidCallGraphNarration    bool   `json:"avoid_call_graph_narration"`
	PreferConceptualDiagrams   bool   `json:"prefer_conceptual_diagrams"`
	PreferTaskOrientedExamples bool   `json:"prefer_task_oriented_examples"`
}

type ModelMeta struct {
	Repo             string `json:"repo"`
	DefaultBranch    string `json:"default_branch"`
	GeneratedAt      string `json:"generated_at"`
	GeneratorVersion string `json:"generator_version,omitempty"`
}

func LoadDocModel(path string) (*DocModel, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m DocModel
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func SaveDocModel(path string, model *DocModel) error {
	if err := validateDocModelWithSchema(path, model); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(model, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0644)
}

func BuildModelFromMarkdown(content string) *DocModel {
	sections := SplitMarkdown("docs/documentation.md", content)
	now := time.Now().UTC().Format(time.RFC3339)

	modelSections := make([]ModelSect, 0, len(sections))
	rootIDs := make([]string, 0, len(sections))
	requiredIDs := make([]string, 0, len(sections))
	usedIDs := make(map[string]int)

	for i, s := range sections {
		baseID := normalizeSectionID(s.Title)
		id := baseID
		if n := usedIDs[baseID]; n > 0 {
			id = fmt.Sprintf("%s-%d", baseID, n+1)
		}
		usedIDs[baseID]++
		sec := ModelSect{
			ID:        id,
			Title:     s.Title,
			Level:     max(1, s.Level),
			Order:     i,
			ParentID:  nil,
			ContentMD: strings.TrimSpace(s.Content),
			Summary:   summarizeContent(s.Content),
			Status:    "active",
			Sources:   []SourceRef{},
		}
		sec.Hash = sectionHash(sec)
		sec.LastUpdated = &UpdateInfo{
			CommitSHA: "HEAD",
			Timestamp: now,
		}
		modelSections = append(modelSections, sec)
		rootIDs = append(rootIDs, id)
		requiredIDs = append(requiredIDs, id)
	}

	if len(rootIDs) == 0 {
		rootIDs = []string{"overview"}
		requiredIDs = []string{"overview"}
		modelSections = append(modelSections, ModelSect{
			ID:        "overview",
			Title:     "Overview",
			Level:     1,
			Order:     0,
			ContentMD: "# Overview\n",
			Status:    "active",
			Sources:   []SourceRef{},
			Hash:      "sha256:empty",
		})
	}

	model := &DocModel{
		SchemaVersion: docModelSchemaVersion,
		Document: ModelDoc{
			ID:             "docod-main-doc",
			Title:          "Project Documentation",
			RootSectionIDs: rootIDs,
		},
		Sections: modelSections,
		Policies: ModelPolicy{
			RequiredSectionIDs: uniqueStrings(requiredIDs),
			MaxSectionChars:    8000,
			Style: PolicyStyle{
				Tone:                       "technical, objective",
				Audience:                   "open-source maintainers",
				CodeBlockLanguage:          "go",
				FocusMode:                  "semantic",
				AvoidCallGraphNarration:    true,
				PreferConceptualDiagrams:   true,
				PreferTaskOrientedExamples: true,
			},
		},
		Meta: ModelMeta{
			Repo:             ".",
			DefaultBranch:    "main",
			GeneratedAt:      now,
			GeneratorVersion: "docod-dev",
		},
	}
	NormalizeDocModel(model)
	return model
}

func (m *DocModel) Validate() error {
	if m == nil {
		return fmt.Errorf("doc model is nil")
	}
	if m.SchemaVersion == "" {
		return fmt.Errorf("schema_version is required")
	}
	if len(m.Sections) == 0 {
		return fmt.Errorf("sections must not be empty")
	}
	sectionIDs := make(map[string]bool, len(m.Sections))
	for _, s := range m.Sections {
		if s.ID == "" {
			return fmt.Errorf("section id is required")
		}
		if sectionIDs[s.ID] {
			return fmt.Errorf("duplicate section id: %s", s.ID)
		}
		sectionIDs[s.ID] = true
	}
	for _, req := range m.Policies.RequiredSectionIDs {
		if !sectionIDs[req] {
			return fmt.Errorf("required section missing: %s", req)
		}
	}
	return nil
}

func validateDocModelWithSchema(modelPath string, model *DocModel) error {
	if model == nil {
		return fmt.Errorf("doc model is nil")
	}
	if err := model.Validate(); err != nil {
		return err
	}

	schemaPath := resolveDocModelSchemaPath(modelPath)
	if schemaPath == "" {
		return fmt.Errorf("doc model schema file not found")
	}

	schema, err := loadCompiledSchema(schemaPath)
	if err != nil {
		return fmt.Errorf("failed to compile doc model schema: %w", err)
	}

	var v any
	raw, err := json.Marshal(model)
	if err != nil {
		return fmt.Errorf("failed to marshal doc model for schema validation: %w", err)
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("failed to normalize doc model for schema validation: %w", err)
	}
	if err := schema.Validate(v); err != nil {
		return fmt.Errorf("doc model schema validation failed: %w", err)
	}
	return nil
}

func resolveDocModelSchemaPath(modelPath string) string {
	candidates := []string{
		filepath.Join(filepath.Dir(modelPath), "doc_model.schema.json"),
		filepath.Join("docs", "doc_model.schema.json"),
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func loadCompiledSchema(schemaPath string) (*jsonschema.Schema, error) {
	abs, err := filepath.Abs(schemaPath)
	if err != nil {
		return nil, err
	}

	schemaCacheMu.Lock()
	if cached, ok := schemaCache[abs]; ok {
		schemaCacheMu.Unlock()
		return cached, nil
	}
	schemaCacheMu.Unlock()

	compiler := jsonschema.NewCompiler()
	compiled, err := compiler.Compile("file://" + filepath.ToSlash(abs))
	if err != nil {
		return nil, err
	}

	schemaCacheMu.Lock()
	schemaCache[abs] = compiled
	schemaCacheMu.Unlock()
	return compiled, nil
}

func (m *DocModel) SectionByID(id string) *ModelSect {
	for i := range m.Sections {
		if m.Sections[i].ID == id {
			return &m.Sections[i]
		}
	}
	return nil
}

func RenderMarkdownFromModel(m *DocModel) string {
	NormalizeDocModel(m)

	var sb strings.Builder
	title := strings.TrimSpace(m.Document.Title)
	if title == "" {
		title = "Project Documentation"
	}
	sb.WriteString("# " + title + "\n\n")
	sb.WriteString("Auto-generated by `docod`.\n\n")

	sections := append([]ModelSect(nil), m.Sections...)
	sort.Slice(sections, func(i, j int) bool {
		if sections[i].Order == sections[j].Order {
			return sections[i].ID < sections[j].ID
		}
		return sections[i].Order < sections[j].Order
	})

	for i, s := range sections {
		content := strings.TrimSpace(s.ContentMD)
		if content == "" {
			continue
		}
		if !startsWithHeading(content) {
			level := s.Level
			if level < 1 || level > 6 {
				level = 2
			}
			sb.WriteString(strings.Repeat("#", level) + " " + s.Title + "\n\n")
		}
		sb.WriteString(content)
		if i < len(sections)-1 {
			sb.WriteString("\n\n")
		} else {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func BuildSourcesFromChunk(chunk knowledge.SearchChunk) []SourceRef {
	if len(chunk.Sources) > 0 {
		out := make([]SourceRef, 0, len(chunk.Sources))
		for _, src := range chunk.Sources {
			symbolID := strings.TrimSpace(src.SymbolID)
			filePath := strings.TrimSpace(src.FilePath)
			if symbolID == "" || filePath == "" {
				continue
			}
			relation := normalizeSourceRelation(src.Relation)
			confidence := src.Confidence
			if confidence < 0 {
				confidence = 0
			}
			if confidence > 1 {
				confidence = 1
			}
			out = append(out, SourceRef{
				SymbolID:   symbolID,
				FilePath:   filePath,
				StartLine:  clampPositive(src.StartLine),
				EndLine:    clampPositive(src.EndLine),
				Relation:   relation,
				CommitSHA:  "HEAD",
				Confidence: confidence,
			})
		}
		if len(out) > 0 {
			return out
		}
	}

	filePath := chunk.FilePath
	if strings.TrimSpace(filePath) == "" {
		filePath = chunk.ID
	}
	return []SourceRef{
		{
			SymbolID:   chunk.ID,
			FilePath:   filePath,
			StartLine:  1,
			EndLine:    1,
			Relation:   "primary",
			CommitSHA:  "HEAD",
			Confidence: 0.9,
		},
	}
}

func MergeSources(existing []SourceRef, chunks []knowledge.SearchChunk) []SourceRef {
	seen := make(map[string]bool, len(existing))
	out := make([]SourceRef, 0, len(existing)+len(chunks))
	for _, src := range existing {
		key := src.SymbolID + "|" + src.FilePath
		seen[key] = true
		out = append(out, src)
	}
	for _, chunk := range chunks {
		for _, src := range BuildSourcesFromChunk(chunk) {
			key := src.SymbolID + "|" + src.FilePath
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, src)
		}
	}
	return out
}

func clampPositive(n int) int {
	if n <= 0 {
		return 1
	}
	return n
}

func normalizeSourceRelation(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "primary", "dependency", "context":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "primary"
	}
}

func sectionHash(s ModelSect) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(s.Title))
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(s.ContentMD))
	b.WriteString("\n")
	for _, src := range s.Sources {
		b.WriteString(src.SymbolID)
		b.WriteString("|")
		b.WriteString(src.FilePath)
		b.WriteString("|")
		b.WriteString(src.Relation)
		b.WriteString("\n")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func summarizeContent(content string) string {
	text := strings.TrimSpace(content)
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) > 120 {
			return line[:120]
		}
		return line
	}
	return ""
}

func startsWithHeading(content string) bool {
	first := strings.TrimSpace(content)
	return strings.HasPrefix(first, "#")
}

func normalizeSectionID(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	if s == "" {
		return "section"
	}
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "section"
	}
	return out
}

// NormalizeDocModel repairs document shape to keep output deterministic and stable.
func NormalizeDocModel(m *DocModel) {
	if m == nil {
		return
	}
	ensurePolicyDefaults(m)
	ensureCanonicalRootSections(m)
	ensureRootSectionIDs(m)
	reindexSectionOrder(m)
	normalizeSectionHeadings(m)
}

func ensurePolicyDefaults(m *DocModel) {
	if m.Policies.MaxSectionChars == 0 {
		m.Policies.MaxSectionChars = 8000
	}
	if m.Policies.Style.Tone == "" {
		m.Policies.Style.Tone = "technical, objective"
	}
	if m.Policies.Style.Audience == "" {
		m.Policies.Style.Audience = "open-source maintainers"
	}
	if m.Policies.Style.CodeBlockLanguage == "" {
		m.Policies.Style.CodeBlockLanguage = "go"
	}
	if m.Policies.Style.FocusMode == "" {
		m.Policies.Style.FocusMode = "semantic"
	}
	// Default to official-doc oriented behavior unless explicitly disabled.
	if !m.Policies.Style.PreferConceptualDiagrams {
		m.Policies.Style.PreferConceptualDiagrams = true
	}
	if !m.Policies.Style.PreferTaskOrientedExamples {
		m.Policies.Style.PreferTaskOrientedExamples = true
	}
	m.Policies.Style.AvoidCallGraphNarration = true
}

func ensureCanonicalRootSections(m *DocModel) {
	existing := make(map[string]bool, len(m.Sections))
	for _, s := range m.Sections {
		existing[s.ID] = true
	}

	for _, id := range canonicalSectionOrder {
		if existing[id] {
			continue
		}
		title := sectionTitleFromID(id)
		newSec := ModelSect{
			ID:        id,
			Title:     title,
			Level:     1,
			Order:     len(m.Sections),
			ParentID:  nil,
			ContentMD: fmt.Sprintf("# %s\n\nTBD.", title),
			Summary:   "",
			Status:    "active",
			Sources:   []SourceRef{},
		}
		newSec.Hash = sectionHash(newSec)
		m.Sections = append(m.Sections, newSec)
	}
}

func ensureRootSectionIDs(m *DocModel) {
	seen := make(map[string]bool)
	var roots []string

	for _, id := range canonicalSectionOrder {
		if m.SectionByID(id) != nil {
			roots = append(roots, id)
			seen[id] = true
		}
	}

	for _, s := range m.Sections {
		if s.ParentID == nil && !seen[s.ID] {
			roots = append(roots, s.ID)
			seen[s.ID] = true
		}
	}

	m.Document.RootSectionIDs = roots
	if len(m.Policies.RequiredSectionIDs) == 0 {
		m.Policies.RequiredSectionIDs = append([]string(nil), canonicalSectionOrder...)
	}
}

func reindexSectionOrder(m *DocModel) {
	sort.Slice(m.Sections, func(i, j int) bool {
		ri := sectionRank(m.Sections[i].ID)
		rj := sectionRank(m.Sections[j].ID)
		if ri == rj {
			return m.Sections[i].Order < m.Sections[j].Order
		}
		return ri < rj
	})
	for i := range m.Sections {
		m.Sections[i].Order = i
	}
}

func normalizeSectionHeadings(m *DocModel) {
	for i := range m.Sections {
		sec := &m.Sections[i]
		if sec.Level < 1 || sec.Level > 6 {
			sec.Level = 2
		}
		if sec.Title == "" {
			sec.Title = sectionTitleFromID(sec.ID)
		}

		trimmed := strings.TrimSpace(sec.ContentMD)
		if trimmed == "" {
			sec.ContentMD = fmt.Sprintf("%s %s\n\nTBD.", strings.Repeat("#", sec.Level), sec.Title)
		} else if startsWithHeading(trimmed) {
			lines := strings.Split(trimmed, "\n")
			if len(lines) > 0 {
				lines[0] = fmt.Sprintf("%s %s", strings.Repeat("#", sec.Level), sec.Title)
				sec.ContentMD = strings.Join(lines, "\n")
			}
		} else {
			sec.ContentMD = fmt.Sprintf("%s %s\n\n%s", strings.Repeat("#", sec.Level), sec.Title, trimmed)
		}
		sec.Summary = summarizeContent(sec.ContentMD)
		sec.Hash = sectionHash(*sec)
	}
}

func sectionRank(id string) int {
	for i, v := range canonicalSectionOrder {
		if id == v {
			return i
		}
	}
	return len(canonicalSectionOrder) + 1
}

func sectionTitleFromID(id string) string {
	switch id {
	case "overview":
		return "Overview"
	case "key-features":
		return "Key Features"
	case "development":
		return "Development"
	default:
		parts := strings.Split(id, "-")
		for i := range parts {
			if parts[i] == "" {
				continue
			}
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
		return strings.Join(parts, " ")
	}
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
