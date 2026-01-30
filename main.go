package main

import (
	"docod/internal/extractor"
	"encoding/json"
	"fmt"
	"log"
)

func main() {
	// Create a new extractor for Go.
	ext, err := extractor.NewExtractor("go")
	if err != nil {
		log.Fatalf("Failed to create extractor: %v", err)
	}

	// Extract code units from the sample.go file.
	codeUnits, err := ext.ExtractFromFile("sample.go")
	if err != nil {
		log.Fatalf("Failed to extract from file: %v", err)
	}

	// Print the extracted units as JSON for clarity.
	if len(codeUnits) == 0 {
		fmt.Println("No code units found.")
		return
	}

	fmt.Printf("Found %d code unit(s):\n", len(codeUnits))
	for _, unit := range codeUnits {
		unitJSON, err := json.MarshalIndent(unit, "", "  ")
		if err != nil {
			log.Printf("Failed to marshal code unit to JSON: %v", err)
			continue
		}
		fmt.Println(string(unitJSON))
	}
}
