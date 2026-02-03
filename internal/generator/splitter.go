package generator

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// SplitMarkdown parses raw markdown text into a flat list of sections for easier indexing.
// While a tree structure is good for representation, a flat list is better for vector search.
func SplitMarkdown(filename, content string) []DocSection {
	var sections []DocSection
	scanner := bufio.NewScanner(strings.NewReader(content))

	var currentTitle string
	var currentLevel int
	var currentBuffer strings.Builder
	
	// Default root section for content before the first header
	currentTitle = "Introduction" 
	currentLevel = 0

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "#") {
			// Check if it's a header
			level := 0
			for _, char := range trimmed {
				if char == '#' {
					level++
				} else {
					break
				}
			}

			// If valid header found
			if level > 0 && level < 7 && len(trimmed) > level && trimmed[level] == ' ' {
				// Save previous section
				if currentBuffer.Len() > 0 {
					sections = append(sections, createSection(filename, currentTitle, currentLevel, currentBuffer.String()))
				}

				// Start new section
				currentTitle = strings.TrimSpace(trimmed[level:])
				currentLevel = level
				currentBuffer.Reset()
				// We don't include the header line in the content to avoid redundancy, 
				// or we can include it. Let's include it for context.
				currentBuffer.WriteString(line + "\n")
				continue
			}
		}

		currentBuffer.WriteString(line + "\n")
	}

	// Save last section
	if currentBuffer.Len() > 0 {
		sections = append(sections, createSection(filename, currentTitle, currentLevel, currentBuffer.String()))
	}

	return sections
}

func createSection(filename, title string, level int, content string) DocSection {
	// Generate a stable ID
	idRaw := fmt.Sprintf("%s:%s", filename, title)
	hash := sha256.Sum256([]byte(idRaw))
	id := hex.EncodeToString(hash[:])

	return DocSection{
		ID:      id,
		Title:   title,
		Level:   level,
		Content: content,
	}
}
