package extractor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

// GoExtractor implements LanguageExtractor for Go.
type GoExtractor struct{}

func (g *GoExtractor) GetLanguage() *sitter.Language {
	return golang.GetLanguage()
}

func (g *GoExtractor) GetQuery() string {
	return `
		(function_declaration) @func
		(method_declaration) @func
		(type_spec) @type
		(const_spec) @const
		(var_spec) @var
	`
}

func (g *GoExtractor) ExtractUnit(captureName string, node *sitter.Node, sourceCode []byte, filepath string, packageName string) *CodeUnit {
	var unit *CodeUnit
	switch captureName {
	case "func":
		unit = g.extractFunctionUnit(node, sourceCode, filepath)
	case "type":
		unit = g.extractTypeUnit(node, sourceCode, filepath)
	case "const":
		unit = g.extractConstUnit(node, sourceCode, filepath)
	case "var":
		unit = g.extractVarUnit(node, sourceCode, filepath)
	}

	if unit != nil {
		unit.Package = packageName
		unit.Language = "go"
		unit.Role = g.inferRole(unit)
		unit.ContentHash = g.calculateHash(unit.Content) // Calculate hash
		if unit.Relations == nil {
			unit.Relations = []Relation{}
		}
	}
	return unit
}

func (g *GoExtractor) calculateHash(content string) string {
	hasher := sha256.New()
	hasher.Write([]byte(content))
	return hex.EncodeToString(hasher.Sum(nil))
}

func (g *GoExtractor) sanitizeValue(name, value string) string {
	lowerName := strings.ToLower(name)
	sensitiveKeywords := []string{"key", "secret", "token", "password", "credential", "auth"}

	for _, kw := range sensitiveKeywords {
		if strings.Contains(lowerName, kw) {
			return "\"[REDACTED]\""
		}
	}
	return value
}

func (g *GoExtractor) inferRole(unit *CodeUnit) string {
	name := strings.ToLower(unit.Name)

	switch unit.UnitType {
	case "interface":
		return "Interface"
	case "struct":
		if strings.HasSuffix(name, "service") {
			return "Service"
		}
		if strings.HasSuffix(name, "repository") || strings.HasSuffix(name, "repo") || strings.HasSuffix(name, "store") {
			return "Data Access"
		}
		if strings.HasSuffix(name, "handler") || strings.HasSuffix(name, "controller") {
			return "API Handler"
		}
		if strings.HasSuffix(name, "config") || strings.HasSuffix(name, "options") {
			return "Configuration"
		}
		if strings.HasSuffix(name, "request") || strings.HasSuffix(name, "response") {
			return "DTO"
		}
		return "Data Model"
	case "function", "method":
		if strings.HasPrefix(name, "new") {
			return "Constructor"
		}
		if strings.HasPrefix(name, "get") || strings.HasPrefix(name, "set") {
			return "Accessor"
		}
		if strings.Contains(name, "test") {
			return "Test"
		}
		return "Logic"
	case "constant":
		return "Constant"
	case "variable":
		return "Variable"
	}
	return "Component"
}

// Go-specific Detail Schemas

type GoFunctionDetails struct {
	Receiver   string     `json:"receiver,omitempty"`
	Parameters []GoParam  `json:"parameters"`
	Returns    []GoReturn `json:"returns"`
	Signature  string     `json:"signature"`
}

type GoTypeDetails struct {
	Fields []GoField `json:"fields"`
}

type GoInterfaceDetails struct {
	Methods []GoFunctionDetails `json:"methods"`
}

type GoConstDetails struct {
	Value string `json:"value"`
	Type  string `json:"type"`
}

type GoVarDetails struct {
	Value string `json:"value,omitempty"`
	Type  string `json:"type"`
}

type GoParam struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type GoReturn struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type"`
}

type GoField struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Tag  string `json:"tag,omitempty"`
}

// Extraction Logic

func (g *GoExtractor) extractTypeUnit(node *sitter.Node, sourceCode []byte, filepath string) *CodeUnit {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nameNode.Content(sourceCode)

	parentNode := node.Parent()
	if parentNode == nil || parentNode.Type() != "type_declaration" {
		parentNode = node
	}
	content := parentNode.Content(sourceCode)
	docComment := g.extractDocComment(parentNode, sourceCode)

	id := fmt.Sprintf("%s:%s:%d", filepath, name, node.StartPoint().Row+1)

	var details interface{}
	var unitType string
	relations := []Relation{}

	typeNode := node.ChildByFieldName("type")
	if typeNode != nil {
		switch typeNode.Type() {
		case "struct_type":
			unitType = "struct"
			structDetails := g.extractStructDetails(typeNode, sourceCode)
			details = structDetails
			for _, field := range structDetails.Fields {
				kind := "uses_type"
				if field.Name == field.Type || strings.HasSuffix(field.Type, "."+field.Name) {
					kind = "embeds"
				}
				if isUserDefinedType(field.Type) {
					relations = append(relations, Relation{Target: field.Type, Kind: kind})
				}
			}
		case "interface_type":
			unitType = "interface"
			interfaceDetails := g.extractInterfaceDetails(typeNode, sourceCode)
			details = interfaceDetails
			for _, method := range interfaceDetails.Methods {
				if !strings.Contains(method.Signature, "(") && isUserDefinedType(method.Signature) {
					relations = append(relations, Relation{Target: method.Signature, Kind: "embeds"})
				}
			}
		default:
			unitType = "type"
		}
	}

	return &CodeUnit{
		ID:          id,
		Filepath:    filepath,
		StartLine:   int(parentNode.StartPoint().Row + 1),
		EndLine:     int(parentNode.EndPoint().Row + 1),
		Content:     content,
		UnitType:    unitType,
		Name:        name,
		Description: docComment,
		Details:     details,
		Relations:   relations,
	}
}

func (g *GoExtractor) extractStructDetails(structNode *sitter.Node, sourceCode []byte) GoTypeDetails {
	fields := []GoField{}
	var fieldList *sitter.Node
	for i := 0; i < int(structNode.ChildCount()); i++ {
		child := structNode.Child(i)
		if child.Type() == "field_declaration_list" {
			fieldList = child
			break
		}
	}
	if fieldList == nil {
		return GoTypeDetails{Fields: fields}
	}

	for i := 0; i < int(fieldList.NamedChildCount()); i++ {
		fieldDecl := fieldList.NamedChild(i)
		if fieldDecl.Type() != "field_declaration" {
			continue
		}

		typeNode := fieldDecl.ChildByFieldName("type")
		var fieldType string
		if typeNode != nil {
			fieldType = typeNode.Content(sourceCode)
		}

		tagNode := fieldDecl.ChildByFieldName("tag")
		var fieldTag string
		if tagNode != nil {
			fieldTag = tagNode.Content(sourceCode)
		}

		foundNames := false
		for j := 0; j < int(fieldDecl.NamedChildCount()); j++ {
			child := fieldDecl.NamedChild(j)
			if child.Type() == "field_identifier" {
				fields = append(fields, GoField{
					Name: child.Content(sourceCode),
					Type: fieldType,
					Tag:  fieldTag,
				})
				foundNames = true
			}
		}

		if !foundNames && fieldType != "" {
			name := fieldType
			if lastDot := strings.LastIndex(name, "."); lastDot != -1 {
				name = name[lastDot+1:]
			}
			name = strings.TrimPrefix(name, "*")
			fields = append(fields, GoField{Name: name, Type: fieldType, Tag: fieldTag})
		}
	}
	return GoTypeDetails{Fields: fields}
}

func (g *GoExtractor) extractInterfaceDetails(interfaceNode *sitter.Node, sourceCode []byte) GoInterfaceDetails {
	methods := []GoFunctionDetails{}
	cursor := sitter.NewTreeCursor(interfaceNode)
	defer cursor.Close()

	var visit func(*sitter.TreeCursor)
	visit = func(c *sitter.TreeCursor) {
		n := c.CurrentNode()
		if n.Type() == "method_elem" || n.Type() == "method_spec" {
			details := GoFunctionDetails{
				Signature:  n.Content(sourceCode),
				Parameters: []GoParam{},
				Returns:    []GoReturn{},
			}
			if paramsNode := n.ChildByFieldName("parameters"); paramsNode != nil {
				details.Parameters = g.extractParams(paramsNode, sourceCode)
			}
			if resultNode := n.ChildByFieldName("result"); resultNode != nil {
				details.Returns = g.extractReturns(resultNode, sourceCode)
			}
			methods = append(methods, details)
			return
		} else if n.Type() == "type_elem" || n.Type() == "type_identifier" || n.Type() == "qualified_type" || n.Type() == "selector_expression" {
			parentType := ""
			if n.Parent() != nil {
				parentType = n.Parent().Type()
			}
			if parentType == "interface_type" || parentType == "method_spec_list" {
				methods = append(methods, GoFunctionDetails{
					Signature:  n.Content(sourceCode),
					Parameters: []GoParam{},
					Returns:    []GoReturn{},
				})
				return
			}
		}
		if c.GoToFirstChild() {
			visit(c)
			for c.GoToNextSibling() {
				visit(c)
			}
			c.GoToParent()
		}
	}
	visit(cursor)
	return GoInterfaceDetails{Methods: methods}
}

func (g *GoExtractor) extractFunctionUnit(node *sitter.Node, sourceCode []byte, filepath string) *CodeUnit {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nameNode.Content(sourceCode)
	content := node.Content(sourceCode)
	id := fmt.Sprintf("%s:%s:%d", filepath, name, node.StartPoint().Row+1)

	unitType := "function"
	details := GoFunctionDetails{
		Parameters: []GoParam{},
		Returns:    []GoReturn{},
	}
	relations := []Relation{}

	if node.Type() == "method_declaration" {
		unitType = "method"
		if receiverNode := node.ChildByFieldName("receiver"); receiverNode != nil {
			details.Receiver = receiverNode.Content(sourceCode)
			recvType := extractBaseType(details.Receiver)
			if recvType != "" {
				relations = append(relations, Relation{Target: recvType, Kind: "belongs_to"})
			}
		}
	}

	docComment := g.extractDocComment(node, sourceCode)
	if paramsNode := node.ChildByFieldName("parameters"); paramsNode != nil {
		details.Parameters = g.extractParams(paramsNode, sourceCode)
		for _, p := range details.Parameters {
			if isUserDefinedType(p.Type) {
				relations = append(relations, Relation{Target: p.Type, Kind: "uses_type"})
			}
		}
	}
	if resultNode := node.ChildByFieldName("result"); resultNode != nil {
		details.Returns = g.extractReturns(resultNode, sourceCode)
		for _, r := range details.Returns {
			if isUserDefinedType(r.Type) {
				relations = append(relations, Relation{Target: r.Type, Kind: "uses_type"})
			}
		}
	}

	bodyNode := node.ChildByFieldName("body")
	if bodyNode != nil {
		details.Signature = strings.TrimSpace(string(sourceCode[node.StartByte():bodyNode.StartByte()]))
		bodyRelations := g.extractBodyRelations(bodyNode, sourceCode)
		relations = append(relations, bodyRelations...)
	} else {
		details.Signature = content
	}

	return &CodeUnit{
		ID:          id,
		Filepath:    filepath,
		StartLine:   int(node.StartPoint().Row + 1),
		EndLine:     int(node.EndPoint().Row + 1),
		Content:     content,
		UnitType:    unitType,
		Name:        name,
		Description: docComment,
		Details:     details,
		Relations:   relations,
	}
}

func (g *GoExtractor) extractBodyRelations(bodyNode *sitter.Node, sourceCode []byte) []Relation {
	relations := []Relation{}
	seen := make(map[string]bool)
	var visit func(*sitter.Node)
	visit = func(n *sitter.Node) {
		var target string
		var kind string
		switch n.Type() {
		case "call_expression":
			fnNode := n.ChildByFieldName("function")
			if fnNode != nil {
				target = fnNode.Content(sourceCode)
				kind = "calls"
			}
		case "composite_literal":
			typeNode := n.ChildByFieldName("type")
			if typeNode != nil {
				target = typeNode.Content(sourceCode)
				kind = "instantiates"
			}
		}
		if target != "" && !seen[target] {
			if !isNoise(target) {
				relations = append(relations, Relation{Target: target, Kind: kind})
				seen[target] = true
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			visit(n.Child(i))
		}
	}
	visit(bodyNode)
	return relations
}

func isNoise(target string) bool {
	builtins := map[string]bool{
		"append": true, "cap": true, "close": true, "complex": true, "copy": true,
		"delete": true, "imag": true, "len": true, "make": true, "new": true,
		"panic": true, "print": true, "println": true, "real": true, "recover": true,
	}
	if builtins[target] {
		return true
	}
	stdlibs := []string{
		"fmt.", "os.", "context.", "errors.", "time.", "strings.", "sync.",
		"json.", "http.", "io.", "log.", "bytes.", "reflect.",
	}
	for _, std := range stdlibs {
		if strings.HasPrefix(target, std) {
			return true
		}
	}
	return false
}

func isUserDefinedType(t string) bool {
	primitives := map[string]bool{
		"bool": true, "string": true, "int": true, "int8": true, "int16": true, "int32": true, "int64": true,
		"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true, "uintptr": true,
		"byte": true, "rune": true, "float32": true, "float64": true, "complex64": true, "complex128": true,
		"error": true, "interface{}": true, "any": true,
	}
	base := strings.TrimPrefix(t, "*")
	base = strings.TrimPrefix(base, "[]")
	return !primitives[base]
}

func extractBaseType(receiver string) string {
	content := strings.Trim(receiver, "()")
	parts := strings.Fields(content)
	t := content
	if len(parts) > 1 {
		t = parts[1]
	}
	t = strings.TrimPrefix(t, "*")
	return t
}

func (g *GoExtractor) extractConstUnit(node *sitter.Node, sourceCode []byte, filepath string) *CodeUnit {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nameNode.Content(sourceCode)
	parentNode := node.Parent()
	if parentNode == nil {
		parentNode = node
	}
	content := node.Content(sourceCode)
	docComment := g.extractDocComment(parentNode, sourceCode)
	if docComment == "" && parentNode.Type() == "const_declaration" {
		docComment = g.extractDocComment(node, sourceCode)
	}
	details := GoConstDetails{}
	if typeNode := node.ChildByFieldName("type"); typeNode != nil {
		details.Type = typeNode.Content(sourceCode)
	}
	if valueNode := node.ChildByFieldName("value"); valueNode != nil {
		rawVal := valueNode.Content(sourceCode)
		details.Value = g.sanitizeValue(name, rawVal)
	}
	return &CodeUnit{
		ID:          fmt.Sprintf("%s:%s:%d", filepath, name, node.StartPoint().Row+1),
		Filepath:    filepath,
		StartLine:   int(node.StartPoint().Row + 1),
		EndLine:     int(node.EndPoint().Row + 1),
		Content:     content,
		UnitType:    "constant",
		Name:        name,
		Description: docComment,
		Details:     details,
	}
}

func (g *GoExtractor) extractVarUnit(node *sitter.Node, sourceCode []byte, filepath string) *CodeUnit {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := nameNode.Content(sourceCode)
	parentNode := node.Parent()
	if parentNode == nil {
		parentNode = node
	}
	content := node.Content(sourceCode)
	docComment := g.extractDocComment(parentNode, sourceCode)
	details := GoVarDetails{}
	if typeNode := node.ChildByFieldName("type"); typeNode != nil {
		details.Type = typeNode.Content(sourceCode)
	}
	if valueNode := node.ChildByFieldName("value"); valueNode != nil {
		rawVal := valueNode.Content(sourceCode)
		details.Value = g.sanitizeValue(name, rawVal)
	}
	return &CodeUnit{
		ID:          fmt.Sprintf("%s:%s:%d", filepath, name, node.StartPoint().Row+1),
		Filepath:    filepath,
		StartLine:   int(node.StartPoint().Row + 1),
		EndLine:     int(node.EndPoint().Row + 1),
		Content:     content,
		UnitType:    "variable",
		Name:        name,
		Description: docComment,
		Details:     details,
	}
}

func (g *GoExtractor) extractDocComment(node *sitter.Node, sourceCode []byte) string {
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

func (g *GoExtractor) extractParams(paramsNode *sitter.Node, sourceCode []byte) []GoParam {
	params := []GoParam{}
	query, _ := sitter.NewQuery([]byte("(parameter_declaration) @param"), golang.GetLanguage())
	qc := sitter.NewQueryCursor()
	qc.Exec(query, paramsNode)
	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}
		for _, c := range m.Captures {
			pNode := c.Node
			pType := ""
			if tn := pNode.ChildByFieldName("type"); tn != nil {
				pType = tn.Content(sourceCode)
			}
			var names []string
			cursor := sitter.NewTreeCursor(pNode)
			if cursor.GoToFirstChild() {
				for {
					if cursor.CurrentNode().Type() == "identifier" {
						names = append(names, cursor.CurrentNode().Content(sourceCode))
					}
					if !cursor.GoToNextSibling() {
						break
					}
				}
			}
			cursor.Close()
			if len(names) > 0 {
				for _, n := range names {
					params = append(params, GoParam{Name: n, Type: pType})
				}
			} else {
				params = append(params, GoParam{Type: pType})
			}
		}
	}
	return params
}

func (g *GoExtractor) extractReturns(resultNode *sitter.Node, sourceCode []byte) []GoReturn {
	returns := []GoReturn{}
	if resultNode.Type() == "parameter_list" {
		temp := g.extractParams(resultNode, sourceCode)
		for _, p := range temp {
			returns = append(returns, GoReturn{Name: p.Name, Type: p.Type})
		}
	} else if resultNode.Type() == "type_list" {
		cursor := sitter.NewTreeCursor(resultNode)
		if cursor.GoToFirstChild() {
			for {
				if t := cursor.CurrentNode().Type(); t != "(" && t != ")" && t != "," {
					returns = append(returns, GoReturn{Type: cursor.CurrentNode().Content(sourceCode)})
				}
				if !cursor.GoToNextSibling() {
					break
				}
			}
		}
		cursor.Close()
	} else {
		returns = append(returns, GoReturn{Type: resultNode.Content(sourceCode)})
	}
	return returns
}

func cleanDocComment(rawComment string) string {
	if rawComment == "" {
		return ""
	}
	lines := strings.Split(rawComment, "\n")
	var cleaned []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		l = strings.TrimPrefix(l, "//")
		l = strings.TrimPrefix(l, "/*")
		l = strings.TrimSuffix(l, "*/")
		cleaned = append(cleaned, strings.TrimSpace(l))
	}
	return strings.Join(cleaned, "\n")
}
