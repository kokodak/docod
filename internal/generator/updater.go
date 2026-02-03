package generator

import (
	"context"
	"docod/internal/knowledge"
	"fmt"
	"os"
	"strings"
)

type DocUpdater struct {
	engine     *knowledge.Engine
	summarizer knowledge.Summarizer
}

func NewDocUpdater(e *knowledge.Engine, s knowledge.Summarizer) *DocUpdater {
	return &DocUpdater{
		engine:     e,
		summarizer: s,
	}
}

// UpdateDocs handles the incremental update of documentation.
func (u *DocUpdater) UpdateDocs(ctx context.Context, docPath string, changedFilePaths []string) error {
	// 1. Read existing documentation
	contentBytes, err := os.ReadFile(docPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("documentation file not found: %s", docPath)
	}
	if err != nil {
		return err
	}
	content := string(contentBytes)

	// 2. Parse and Chunk Doc
	sections := SplitMarkdown(docPath, content)

	// 3. Index Doc Chunks
	if err := u.indexDocSections(ctx, sections); err != nil {
		return fmt.Errorf("failed to index doc sections: %w", err)
	}

	// 4. Identify Relevant Doc Sections for Changes & Track Unmatched Files
	affectedSections := make(map[string][]knowledge.SearchChunk) // ID -> List of triggering chunks
	var unmatchedFiles []string
	chunkVectors := make(map[string][]float32) // ID (filepath) -> Vector

	// Retrieve file chunks using PrepareChunksForFiles
	fileChunks := u.engine.PrepareChunksForFiles(changedFilePaths)

	// Batch Embed Queries to minimize LLM calls
	var searchQueries []string
	var queryToChunk []knowledge.SearchChunk // Map index back to chunk

	for _, chunk := range fileChunks {
		// We query using the file's description + signature
		q := chunk.Description + "\n" + chunk.Signature
		searchQueries = append(searchQueries, q)
		queryToChunk = append(queryToChunk, chunk)
	}

	if len(searchQueries) > 0 {
		vectors, err := u.engine.Embedder().Embed(ctx, searchQueries)
		if err != nil {
			return fmt.Errorf("failed to embed search queries: %w", err)
		}

		for i, vec := range vectors {
			chunkVectors[queryToChunk[i].ID] = vec // Store vector for later use

			results, err := u.engine.Indexer().Search(ctx, vec, 3)
			if err != nil {
				unmatchedFiles = append(unmatchedFiles, queryToChunk[i].ID) // ID is filepath
				continue
			}

			matched := false
			for _, res := range results {
				// Search result is VectorItem, which contains Chunk
				if res.Chunk.UnitType == "doc_section" {
					// Found a match
					matched = true
					secID := res.Chunk.ID
					affectedSections[secID] = append(affectedSections[secID], queryToChunk[i])
				}
			}

			if !matched {
				unmatchedFiles = append(unmatchedFiles, queryToChunk[i].ID)
			}
		}
	}

	if len(affectedSections) == 0 && len(unmatchedFiles) == 0 {
		fmt.Println("  -> No relevant documentation changes needed.")
		return nil
	}

	fmt.Printf("  -> Found %d existing sections to update and %d new files to document.\n", len(affectedSections), len(unmatchedFiles))

	// 5. Generate Updates (Patches)
	patches := make(map[string]string)

	for secID, triggeringChunks := range affectedSections {
		// Find the section object
		var sec DocSection
		for _, s := range sections {
			if s.ID == secID {
				sec = s
				break
			}
		}

		// Optimization: Use the triggering chunks directly as context
		// This saves a search call and ensures the LLM sees the recent changes.
		updatedContent, err := u.summarizer.UpdateDocSection(ctx, sec.Content, triggeringChunks)
		if err != nil {
			fmt.Printf("Failed to update section %s: %v\n", sec.Title, err)
			patches[sec.ID] = sec.Content // Keep original
		} else {
			patches[sec.ID] = updatedContent
		}
	}

	// 6. Generate New Sections & Find Insertion Point
	var newSectionsContent strings.Builder
	insertionTargetIndex := -1

	if len(unmatchedFiles) > 0 {
		fmt.Println("  -> Generating new documentation for unmatched files...")

		// Group chunks for new files
		var newChunks []knowledge.SearchChunk
		chunkMap := make(map[string]knowledge.SearchChunk)
		for _, c := range fileChunks {
			chunkMap[c.ID] = c
		}

		for _, path := range unmatchedFiles {
			if chunk, ok := chunkMap[path]; ok {
				newChunks = append(newChunks, chunk)
			}
		}

		if len(newChunks) > 0 {
			// Ask LLM to generate a new section for these features
			newContent, err := u.summarizer.GenerateNewSection(ctx, newChunks)
			if err == nil {
				newSectionsContent.WriteString("\n\n")
				newSectionsContent.WriteString(newContent)
				
				// Determine Insertion Point using LLM (Context-Aware)
				var toc []string
				for _, s := range sections {
					toc = append(toc, s.Title)
				}
				
				fmt.Println("  -> Consulting LLM for best insertion point...")
				idx, err := u.summarizer.FindInsertionPoint(ctx, toc, newContent)
				if err == nil {
					insertionTargetIndex = idx
					if idx >= 0 && idx < len(toc) {
						fmt.Printf("  -> Smart Insertion: Placing after '%s'\n", toc[idx])
					} else {
						fmt.Printf("  -> Smart Insertion: Placing at index %d\n", idx)
					}
				} else {
					fmt.Printf("  -> Insertion point detection failed: %v. Appending to end.\n", err)
					insertionTargetIndex = len(sections) - 1
				}

			} else {
				fmt.Printf("Failed to generate new section: %v\n", err)
			}
		}
	}

	// 7. Reassemble Document
	var sb strings.Builder
	
	// Handle special case: Insert at beginning
	if insertionTargetIndex == -1 && newSectionsContent.Len() > 0 {
		sb.WriteString(strings.TrimSpace(newSectionsContent.String()))
		sb.WriteString("\n\n")
	}

	for i, sec := range sections {
		var contentToWrite string
		if newContent, ok := patches[sec.ID]; ok {
			contentToWrite = newContent
		} else {
			contentToWrite = sec.Content
		}

		sb.WriteString(strings.TrimSpace(contentToWrite))
		sb.WriteString("\n\n")

		// Insert AFTER the target index
		if i == insertionTargetIndex && newSectionsContent.Len() > 0 {
			sb.WriteString(strings.TrimSpace(newSectionsContent.String()))
			sb.WriteString("\n\n") 
		}
	}

	// Fallback logic handled by initialization of insertionTargetIndex. 
	// If it was defaulted to len-1 (failed case), it inserts after last section.
	
	// 8. Write back
	return os.WriteFile(docPath, []byte(sb.String()), 0644)
}

func (u *DocUpdater) indexDocSections(ctx context.Context, sections []DocSection) error {
	var items []knowledge.VectorItem
	var texts []string

	for _, sec := range sections {
		// Embed
		text := fmt.Sprintf("Documentation Section: %s\nContent: %s", sec.Title, sec.Content)
		texts = append(texts, text)
		
		// We can't batch efficiently here without refactoring Embedder to take generic items.
		// So we collect all texts first.
	}

	// Batch embed
	vectors, err := u.engine.Embedder().Embed(ctx, texts)
	if err != nil {
		return err
	}

	for i, sec := range sections {
		chunk := knowledge.SearchChunk{
			ID:          sec.ID,
			Name:        sec.Title,
			UnitType:    "doc_section",
			Description: sec.Title,
			Content:     sec.Content,
		}
		items = append(items, knowledge.VectorItem{
			Chunk:     chunk,
			Embedding: vectors[i],
		})
	}

	return u.engine.Indexer().Add(ctx, items)
}
