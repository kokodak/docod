package crawler

import (
	"docod/internal/extractor"
	"io/fs"
	"path/filepath"
	"strings"
)

// Crawler scans a directory for source files.
type Crawler struct {
	extractor *extractor.Extractor
	ignored   []string
}

// NewCrawler creates a new crawler instance.
func NewCrawler(ext *extractor.Extractor) *Crawler {
	return &Crawler{
		extractor: ext,
		ignored:   []string{".git", "vendor", "node_modules", "testdata"},
	}
}

// ScanProject walks the root directory and processes all relevant files.
// It uses a callback to stream CodeUnits, preventing large memory buildup.
func (c *Crawler) ScanProject(root string, onUnit func(*extractor.CodeUnit)) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip ignored directories
		if d.IsDir() {
			for _, ign := range c.ignored {
				if d.Name() == ign {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Only process Go files
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		// Extract units from file
		units, err := c.extractor.ExtractFromFile(path)
		if err != nil {
			// Log and continue instead of failing the whole scan
			return nil
		}

		// Stream results back
		for _, unit := range units {
			onUnit(unit)
		}

		return nil
	})
}
