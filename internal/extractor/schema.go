
package extractor

// CodeUnit represents a single piece of extracted code information,
// such as a function, type, or constant. It's a generic container.
type CodeUnit struct {
	ID          string      `json:"id"`           // Unique identifier (e.g., file_path:function_name:start_line)
	Filepath    string      `json:"filepath"`     // Path to the source file
	Language    string      `json:"language"`     // Programming language
	StartLine   int         `json:"start_line"`   // Starting line number in the source file
	EndLine     int         `json:"end_line"`     // Ending line number in the source file
	Content     string      `json:"content"`      // Raw source code of the unit
	UnitType    string      `json:"unit_type"`    // e.g., "function", "type", "interface", "constant"
	Name        string      `json:"name"`         // Name of the code unit (e.g., function name)
	Description string      `json:"description"`  // Associated comments or docstrings
	Details     interface{} `json:"details"`      // Type-specific details (e.g., FunctionDetails)
}

// FunctionDetails contains specific information about a function or method.
type FunctionDetails struct {
	Receiver   string   `json:"receiver,omitempty"` // Receiver type for methods (e.g., "(c *MyClass)")
	Parameters []Param  `json:"parameters"`         // List of parameters
	Returns    []Return `json:"returns"`            // List of return values
	Signature  string   `json:"signature"`          // Full function signature
}

// TypeDetails contains specific information about a struct, class, or type definition.
type TypeDetails struct {
	Fields []Field `json:"fields"` // List of fields/properties
}

// InterfaceDetails contains specific information about an interface.
type InterfaceDetails struct {
	Methods []FunctionDetails `json:"methods"` // List of method signatures required by the interface
}

// Param represents a single function or method parameter.
type Param struct {
	Name string `json:"name"` // Parameter name
	Type string `json:"type"` // Parameter type
}

// Return represents a single return value from a function or method.
type Return struct {
	Name string `json:"name,omitempty"` // Optional name for the return value
	Type string `json:"type"`           // Return type
}

// Field represents a field in a struct or a property in a class.
type Field struct {
	Name string `json:"name"` // Field name
	Type string `json:"type"` // Field type
	Tag  string `json:"tag,omitempty"`  // Struct tags (e.g., `json:"my_field"`)
}
