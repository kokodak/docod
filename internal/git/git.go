package git

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type ChangedFile struct {
	Path         string
	ChangedLines []int
}

// GetChangedFiles runs git diff and returns a list of changed files with line numbers.
func GetChangedFiles(baseRef string) ([]ChangedFile, error) {
	cmd := exec.Command("git", "diff", "-U0", baseRef)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %w", err)
	}

	return parseDiff(output)
}

func parseDiff(output []byte) ([]ChangedFile, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	var changes []ChangedFile
	var currentFile *ChangedFile

	// Regex for chunk header: @@ -oldStart,oldLen +newStart,newLen @@
	// We only care about newStart and newLen (the + part)
	chunkHeader := regexp.MustCompile(`^@@ \-\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "diff --git") {
			// Start of a new file
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				// a/path/to/file b/path/to/file
				// We want the b/ path (new version)
				bPath := parts[3]
				path := strings.TrimPrefix(bPath, "b/")
				
				// Save previous file if exists
				if currentFile != nil {
					changes = append(changes, *currentFile)
				}
				currentFile = &ChangedFile{Path: path, ChangedLines: []int{}}
			}
			continue
		}

		if currentFile == nil {
			continue
		}

		if strings.HasPrefix(line, "@@") {
			matches := chunkHeader.FindStringSubmatch(line)
			if len(matches) > 1 {
				startLine, _ := strconv.Atoi(matches[1])
				count := 1 // Default length is 1 if omitted
				if len(matches) > 2 && matches[2] != "" {
					count, _ = strconv.Atoi(matches[2])
				}

				// If count is 0, it's a deletion, we might skip or mark adjacent lines.
				// For now, let's treat it as "something changed around here".
				// But strictly, if count is 0, no lines exist in the new file at this pos.
				// However, usually we care about added/modified lines.
				
				for i := 0; i < count; i++ {
					currentFile.ChangedLines = append(currentFile.ChangedLines, startLine+i)
				}
			}
		}
	}

	if currentFile != nil {
		changes = append(changes, *currentFile)
	}

	return changes, nil
}
