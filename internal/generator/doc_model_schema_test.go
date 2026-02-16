package generator

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSaveDocModel_ValidatesAgainstJSONSchema(t *testing.T) {
	model := BuildModelFromMarkdown("# Overview\n\nhello\n")
	require.NotEmpty(t, model.Sections)
	model.Sections[0].Status = "not-a-valid-status"

	tmp := t.TempDir()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	schemaSrc := filepath.Join(filepath.Dir(currentFile), "..", "..", "docs", "doc_model.schema.json")
	schemaBytes, err := os.ReadFile(schemaSrc)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "doc_model.schema.json"), schemaBytes, 0644))

	err = SaveDocModel(filepath.Join(tmp, "doc_model.json"), model)
	require.Error(t, err)
	require.Contains(t, err.Error(), "schema validation")
}
