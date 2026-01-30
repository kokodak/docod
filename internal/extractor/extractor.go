package extractor

import (
	"context"
	"fmt"
	"io/ioutil"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

// Extractor is responsible for parsing source code and extracting CodeUnits.
type Extractor struct {
	language *sitter.Language
	langName string
}

// NewExtractor creates a new extractor for a given language.
// It initializes the tree-sitter parser for the specified language.
func NewExtractor(lang string) (*Extractor, error) {
	var language *sitter.Language
	switch lang {
	case "go":
		language = golang.GetLanguage()
	default:
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}
	return &Extractor{language: language, langName: lang}, nil
}

// ExtractFromFile parses a single source file and extracts all relevant code units.
func (e *Extractor) ExtractFromFile(filepath string) ([]*CodeUnit, error) {
	sourceCode, err := ioutil.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filepath, err)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(e.language)
	tree, err := parser.ParseCtx(context.Background(), nil, sourceCode)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file %s: %w", filepath, err)
	}

	var codeUnits []*CodeUnit

	// Query to find functions, methods, and type definitions.
	query, err := sitter.NewQuery([]byte(`
		(function_declaration) @func
		(method_declaration) @func
		(type_spec) @type
	`), e.language)
	if err != nil {
		return nil, fmt.Errorf("failed to create query: %w", err)
	}

	qc := sitter.NewQueryCursor()
	qc.Exec(query, tree.RootNode())

	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}
		for _, c := range m.Captures {
			captureName := query.CaptureNameForId(c.Index)
			var unit *CodeUnit
			switch captureName {
			case "func":
				unit = e.extractFunctionUnit(c.Node, sourceCode, filepath)
			case "type":
				unit = e.extractTypeUnit(c.Node, sourceCode, filepath)
			}

			if unit != nil {
				codeUnits = append(codeUnits, unit)
			}
		}
	}

	return codeUnits, nil
}

// extractTypeUnit processes a single type_spec node.
func (e *Extractor) extractTypeUnit(node *sitter.Node, sourceCode []byte, filepath string) *CodeUnit {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nameNode.Content(sourceCode)

	// The `type_spec` is inside a `type_declaration`. We want the parent `type_declaration` for content and comments.
	parentNode := node.Parent()
	if parentNode == nil || parentNode.Type() != "type_declaration" {
		parentNode = node
	}
	content := parentNode.Content(sourceCode)
	docComment := e.extractDocComment(parentNode, sourceCode)

	id := fmt.Sprintf("%s:%s:%d", filepath, name, node.StartPoint().Row+1)

	var details interface{}
	var unitType string

	typeNode := node.ChildByFieldName("type")
	if typeNode != nil {
		switch typeNode.Type() {
		case "struct_type":
			unitType = "struct"
			details = e.extractStructDetails(typeNode, sourceCode)
		case "interface_type":
			unitType = "interface"
			details = e.extractInterfaceDetails(typeNode, sourceCode)
		default:
			unitType = "type" // Could be an alias or other definition.
		}
	}

	return &CodeUnit{
		ID:          id,
		Filepath:    filepath,
		Language:    e.langName,
		StartLine:   int(parentNode.StartPoint().Row + 1),
		EndLine:     int(parentNode.EndPoint().Row + 1),
		Content:     content,
		UnitType:    unitType,
		Name:        name,
		Description: docComment,
		Details:     details,
	}
}

// extractStructDetails extracts fields from a struct_type node.
func (e *Extractor) extractStructDetails(structNode *sitter.Node, sourceCode []byte) TypeDetails {
	var fields []Field
	fieldList := structNode.ChildByFieldName("fields")
	if fieldList == nil {
		return TypeDetails{Fields: fields}
	}

	for i := 0; i < int(fieldList.ChildCount()); i++ {
		fieldDecl := fieldList.Child(i)
		if fieldDecl.Type() != "field_declaration" {
			continue
		}

		typeNode := fieldDecl.ChildByFieldName("type")
		if typeNode == nil {
			continue
		}
		fieldType := typeNode.Content(sourceCode)

		tagNode := fieldDecl.ChildByFieldName("tag")
		var fieldTag string
		if tagNode != nil {
			fieldTag = tagNode.Content(sourceCode)
		}

		// Extract one or more field names
		for j := 0; j < int(fieldDecl.NamedChildCount()); j++ {
			child := fieldDecl.NamedChild(j)
			if child.Type() == "field_identifier" {
				fields = append(fields, Field{
					Name: child.Content(sourceCode),
					Type: fieldType,
					Tag:  fieldTag,
				})
			}
		}
	}
	return TypeDetails{Fields: fields}
}

// extractInterfaceDetails extracts methods from an interface_type node.
func (e *Extractor) extractInterfaceDetails(interfaceNode *sitter.Node, sourceCode []byte) InterfaceDetails {
	var methods []FunctionDetails
	methodList := interfaceNode.ChildByFieldName("methods")
	if methodList == nil {
		return InterfaceDetails{Methods: methods}
	}

	for i := 0; i < int(methodList.ChildCount()); i++ {
		child := methodList.Child(i)
		if child.Type() == "method_spec" {
			methods = append(methods, FunctionDetails{
				Signature: child.Content(sourceCode),
			})
		}
	}
	return InterfaceDetails{Methods: methods}
}

// extractFunctionUnit processes a single function or method declaration node.
func (e *Extractor) extractFunctionUnit(node *sitter.Node, sourceCode []byte, filepath string) *CodeUnit {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nameNode.Content(sourceCode)
	content := node.Content(sourceCode)
	id := fmt.Sprintf("%s:%s:%d", filepath, name, node.StartPoint().Row+1)

	unitType := "function"
	if node.Type() == "method_declaration" {
		unitType = "method"
	}

	docComment := e.extractDocComment(node, sourceCode)

	details := FunctionDetails{}
	paramsNode := node.ChildByFieldName("parameters")
	if paramsNode != nil {
		details.Parameters = e.extractParams(paramsNode, sourceCode)
	}

	resultNode := node.ChildByFieldName("result")
	if resultNode != nil {
		details.Returns = e.extractReturns(resultNode, sourceCode)
	}

	signatureNode := node.ChildByFieldName("name").Parent()
	if signatureNode != nil {
		bodyNode := node.ChildByFieldName("body")
		if bodyNode != nil {
			details.Signature = strings.TrimSpace(string(sourceCode[signatureNode.StartByte():bodyNode.StartByte()]))
		}
	}

	return &CodeUnit{
		ID:          id,
		Filepath:    filepath,
		Language:    e.langName,
		StartLine:   int(node.StartPoint().Row + 1),
		EndLine:     int(node.EndPoint().Row + 1),
		Content:     content,
		UnitType:    unitType,
		Name:        name,
		Description: docComment,
		Details:     details,
	}
}

// extractDocComment walks backwards from a node to find its associated doc comment block.
func (e *Extractor) extractDocComment(node *sitter.Node, sourceCode []byte) string {
	var commentLines []string
	currentNode := node
	for {
		prevSibling := currentNode.PrevSibling()
		if prevSibling == nil || (currentNode.StartPoint().Row-prevSibling.EndPoint().Row > 1) {
			break
		}
		if prevSibling.Type() != "comment" {
			break
		}
		commentLines = append([]string{prevSibling.Content(sourceCode)}, commentLines...)
		currentNode = prevSibling
	}
	return cleanDocComment(strings.Join(commentLines, "\n"))
}


func (e *Extractor) extractParams(paramsNode *sitter.Node, sourceCode []byte) []Param {
	var params []Param
	query, _ := sitter.NewQuery([]byte(`(parameter_declaration) @param`), e.language)
	qc := sitter.NewQueryCursor()
	qc.Exec(query, paramsNode)

	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}
		for _, c := range m.Captures {
			paramNode := c.Node
			paramTypeNode := paramNode.ChildByFieldName("type")
			if paramTypeNode == nil {
				continue
			}
			paramType := paramTypeNode.Content(sourceCode)
			var names []string
			nameCursor := sitter.NewTreeCursor(paramNode)
			if nameCursor.GoToFirstChild() {
				for {
					if nameCursor.CurrentNode().Type() == "identifier" {
						names = append(names, nameCursor.CurrentNode().Content(sourceCode))
					}
					if !nameCursor.GoToNextSibling() {
						break
					}
				}
			}
			nameCursor.Close()

			if len(names) > 0 {
				for _, name := range names {
					params = append(params, Param{Name: name, Type: paramType})
				}
			} else {
				params = append(params, Param{Type: paramType})
			}
		}
	}
	return params
}

func (e *Extractor) extractReturns(resultNode *sitter.Node, sourceCode []byte) []Return {
	var returns []Return
	if resultNode.Type() == "parameter_list" {
		tempParams := e.extractParams(resultNode, sourceCode)
		for _, p := range tempParams {
			returns = append(returns, Return{Name: p.Name, Type: p.Type})
		}
	} else if resultNode.Type() == "type_list" {
		cursor := sitter.NewTreeCursor(resultNode)
		if cursor.GoToFirstChild() {
			for {
				nodeType := cursor.CurrentNode().Type()
				if nodeType != "(" && nodeType != ")" && nodeType != "," {
					returns = append(returns, Return{Type: cursor.CurrentNode().Content(sourceCode)})
				}
				if !cursor.GoToNextSibling() {
					break
				}
			}
		}
		cursor.Close()
	} else {
		returns = append(returns, Return{Type: resultNode.Content(sourceCode)})
	}
	return returns
}

// cleanDocComment removes comment markers and leading/trailing whitespace.
func cleanDocComment(rawComment string) string {
	if rawComment == "" {
		return ""
	}
	lines := strings.Split(rawComment, "\n")
	var cleanedLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "//")
		line = strings.TrimPrefix(line, "/*")
		line = strings.TrimSuffix(line, "*/")
		cleanedLines = append(cleanedLines, strings.TrimSpace(line))
	}
	return strings.Join(cleanedLines, "\n")
}