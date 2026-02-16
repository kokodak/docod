package generator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ReportSignal struct {
	Code     string  `json:"code"`
	Stage    string  `json:"stage"`
	Severity string  `json:"severity"`
	Message  string  `json:"message"`
	Value    float64 `json:"value,omitempty"`
}

type StageMetric struct {
	Name       string             `json:"name"`
	Status     string             `json:"status"`
	StartedAt  string             `json:"started_at"`
	FinishedAt string             `json:"finished_at"`
	DurationMS int64              `json:"duration_ms"`
	Counters   map[string]float64 `json:"counters,omitempty"`
	Notes      []string           `json:"notes,omitempty"`
	Error      string             `json:"error,omitempty"`
}

type SectionMetric struct {
	SectionID           string   `json:"section_id"`
	Title               string   `json:"title"`
	QueryCount          int      `json:"query_count"`
	SearchHits          int      `json:"search_hits"`
	HeuristicHits       int      `json:"heuristic_hits"`
	ChunkCount          int      `json:"chunk_count"`
	SourceCount         int      `json:"source_count"`
	FileDiversity       int      `json:"file_diversity"`
	EvidenceConfidence  float64  `json:"evidence_confidence"`
	EvidenceCoverage    float64  `json:"evidence_coverage"`
	LowEvidence         bool     `json:"low_evidence"`
	WriterQualityScore  float64  `json:"writer_quality_score"`
	WriterQualityIssues []string `json:"writer_quality_issues,omitempty"`
	UsedDraft           bool     `json:"used_draft"`
	UsedLLM             bool     `json:"used_llm"`
	UsedFallback        bool     `json:"used_fallback"`
}

type ReportSummary struct {
	StageCount         int     `json:"stage_count"`
	SectionCount       int     `json:"section_count"`
	FailedStages       int     `json:"failed_stages"`
	LowEvidenceSections int    `json:"low_evidence_sections"`
	AvgWriterQuality   float64 `json:"avg_writer_quality"`
	SignalsBySeverity  map[string]int `json:"signals_by_severity"`
}

type PipelineReport struct {
	Version     string          `json:"version"`
	Mode        string          `json:"mode"`
	GeneratedAt string          `json:"generated_at"`
	OutputDir   string          `json:"output_dir"`
	Stages      []StageMetric   `json:"stages"`
	Sections    []SectionMetric `json:"sections,omitempty"`
	Signals     []ReportSignal  `json:"signals,omitempty"`
	Summary     ReportSummary   `json:"summary"`
}

type StageHandle struct {
	name    string
	started time.Time
}

func NewPipelineReport(mode, outputDir string) *PipelineReport {
	return &PipelineReport{
		Version:     "v1",
		Mode:        mode,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		OutputDir:   outputDir,
		Stages:      []StageMetric{},
		Sections:    []SectionMetric{},
		Signals:     []ReportSignal{},
	}
}

func (r *PipelineReport) BeginStage(name string) StageHandle {
	return StageHandle{name: strings.TrimSpace(name), started: time.Now().UTC()}
}

func (r *PipelineReport) EndStage(h StageHandle, status string, counters map[string]float64, notes []string, err error) {
	if r == nil || strings.TrimSpace(h.name) == "" {
		return
	}
	if strings.TrimSpace(status) == "" {
		status = "ok"
	}
	finished := time.Now().UTC()
	m := StageMetric{
		Name:       h.name,
		Status:     status,
		StartedAt:  h.started.Format(time.RFC3339Nano),
		FinishedAt: finished.Format(time.RFC3339Nano),
		DurationMS: finished.Sub(h.started).Milliseconds(),
		Counters:   cleanCounters(counters),
		Notes:      cleanNotes(notes),
	}
	if err != nil {
		m.Error = err.Error()
		if status == "ok" {
			m.Status = "error"
		}
	}
	r.Stages = append(r.Stages, m)
}

func (r *PipelineReport) AddSignal(code, stage, severity, message string, value float64) {
	if r == nil {
		return
	}
	s := ReportSignal{
		Code:     strings.TrimSpace(code),
		Stage:    strings.TrimSpace(stage),
		Severity: strings.ToLower(strings.TrimSpace(severity)),
		Message:  strings.TrimSpace(message),
		Value:    value,
	}
	if s.Code == "" || s.Stage == "" || s.Severity == "" || s.Message == "" {
		return
	}
	r.Signals = append(r.Signals, s)
}

func (r *PipelineReport) AddSectionMetric(m SectionMetric) {
	if r == nil || strings.TrimSpace(m.SectionID) == "" {
		return
	}
	r.Sections = append(r.Sections, m)
}

func (r *PipelineReport) Finalize() {
	if r == nil {
		return
	}
	r.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	severityCount := map[string]int{
		"critical": 0,
		"warning":  0,
		"info":     0,
	}
	sort.Slice(r.Signals, func(i, j int) bool {
		pi := signalPriority(r.Signals[i].Severity)
		pj := signalPriority(r.Signals[j].Severity)
		if pi == pj {
			if r.Signals[i].Stage == r.Signals[j].Stage {
				return r.Signals[i].Code < r.Signals[j].Code
			}
			return r.Signals[i].Stage < r.Signals[j].Stage
		}
		return pi > pj
	})
	for _, s := range r.Signals {
		if _, ok := severityCount[s.Severity]; ok {
			severityCount[s.Severity]++
		} else {
			severityCount[s.Severity] = 1
		}
	}

	failed := 0
	for _, st := range r.Stages {
		if st.Status != "ok" {
			failed++
		}
	}

	lowEvidence := 0
	totalQuality := 0.0
	for _, sec := range r.Sections {
		if sec.LowEvidence {
			lowEvidence++
		}
		totalQuality += sec.WriterQualityScore
	}
	avgQuality := 0.0
	if len(r.Sections) > 0 {
		avgQuality = totalQuality / float64(len(r.Sections))
	}

	r.Summary = ReportSummary{
		StageCount:         len(r.Stages),
		SectionCount:       len(r.Sections),
		FailedStages:       failed,
		LowEvidenceSections: lowEvidence,
		AvgWriterQuality:   avgQuality,
		SignalsBySeverity:  severityCount,
	}
}

func (r *PipelineReport) Save(path string) error {
	if r == nil {
		return nil
	}
	r.Finalize()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

func cleanCounters(raw map[string]float64) map[string]float64 {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]float64, len(raw))
	for k, v := range raw {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out[key] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cleanNotes(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, n := range raw {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func signalPriority(severity string) int {
	switch severity {
	case "critical":
		return 3
	case "warning":
		return 2
	default:
		return 1
	}
}
