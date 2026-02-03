package generator

// DocSection represents a parsed section of a Markdown document.
type DocSection struct {
	ID       string // Unique ID (e.g., hash of title or path)
	Title    string
	Level    int    // Header level (1 for #, 2 for ##, etc.)
	Content  string // The text content under this header
	Children []*DocSection
}

// ToMarkdown reconstructs the section into Markdown format.
type DocPatch struct {
	SectionID string
	NewContent string
}
