package resolver

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"docod/internal/graph"
)

type TypeResolutionStats struct {
	Attempted int
	Resolved  int
	Skipped   int
}

// GoTypesResolver resolves unresolved graph relations using go/types on local source files.
// It runs best-effort; failures in one package do not abort the entire resolution pass.
type GoTypesResolver struct{}

func NewGoTypesResolver() *GoTypesResolver {
	return &GoTypesResolver{}
}

func (r *GoTypesResolver) Name() string {
	return "types"
}

func (r *GoTypesResolver) Resolve(g *graph.Graph) (ResolveStats, error) {
	stats, err := r.ResolveGraphRelations(g)
	return ResolveStats{
		Attempted: stats.Attempted,
		Resolved:  stats.Resolved,
		Skipped:   stats.Skipped,
	}, err
}

func (r *GoTypesResolver) ResolveGraphRelations(g *graph.Graph) (TypeResolutionStats, error) {
	stats := TypeResolutionStats{}
	if g == nil || len(g.Unresolved) == 0 {
		return stats, nil
	}

	pkgs, err := r.loadTypedPackages(g)
	if err != nil {
		return stats, err
	}

	nodeIdx := buildNodeIndex(g)
	edgeSet := make(map[string]bool, len(g.Edges))
	for _, e := range g.Edges {
		edgeSet[edgeKey(e.From, e.To, e.Kind)] = true
	}

	var still []graph.UnresolvedRelation
	for _, ur := range g.Unresolved {
		stats.Attempted++
		sourceNode, ok := g.Nodes[ur.From]
		if !ok || sourceNode.Unit == nil {
			stats.Skipped++
			ur.Reason = graph.ReasonSourceMissing
			still = append(still, ur)
			continue
		}

		pkgKey := pkgGroupKey(sourceNode.Unit.Filepath, sourceNode.Unit.Package)
		pkgRes, ok := pkgs[pkgKey]
		if !ok {
			stats.Skipped++
			ur.Reason = graph.ReasonTypecheckFail
			still = append(still, ur)
			continue
		}

		targetIDs, reason := r.resolveUnresolvedWithTypes(pkgRes, nodeIdx, sourceNode.Unit, ur)
		if len(targetIDs) == 0 {
			ur.Reason = reason
			still = append(still, ur)
			continue
		}

		resolvedAny := false
		for _, toID := range targetIDs {
			key := edgeKey(ur.From, toID, ur.Kind)
			if edgeSet[key] {
				resolvedAny = true
				continue
			}
			edgeSet[key] = true
			g.Edges = append(g.Edges, graph.Edge{
				From:       ur.From,
				To:         toID,
				Kind:       ur.Kind,
				Resolver:   "types",
				Confidence: maxFloat(ur.Confidence, 0.9),
				Evidence:   ur.Evidence,
			})
			resolvedAny = true
		}

		if resolvedAny {
			stats.Resolved++
			continue
		}
		still = append(still, ur)
	}

	g.Unresolved = still
	return stats, nil
}

type typedPackage struct {
	fset      *token.FileSet
	files     []*ast.File
	info      *types.Info
	byFile    map[string][]ast.Node
	objToKeys map[types.Object][]string
}

func (r *GoTypesResolver) loadTypedPackages(g *graph.Graph) (map[string]*typedPackage, error) {
	byGroup := make(map[string][]string)
	for _, node := range g.Nodes {
		if node == nil || node.Unit == nil {
			continue
		}
		if !strings.HasSuffix(node.Unit.Filepath, ".go") || strings.HasSuffix(node.Unit.Filepath, "_test.go") {
			continue
		}
		key := pkgGroupKey(node.Unit.Filepath, node.Unit.Package)
		byGroup[key] = append(byGroup[key], node.Unit.Filepath)
	}

	result := make(map[string]*typedPackage)
	for key, files := range byGroup {
		uniq := dedupeStrings(files)
		sort.Strings(uniq)
		tp, err := loadOneTypedPackage(uniq)
		if err != nil {
			// Best effort: skip failing groups.
			continue
		}
		result[key] = tp
	}
	return result, nil
}

func loadOneTypedPackage(paths []string) (*typedPackage, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("empty package files")
	}

	fset := token.NewFileSet()
	parsed := make([]*ast.File, 0, len(paths))
	for _, p := range paths {
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, f)
	}

	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}

	conf := &types.Config{
		Importer: importer.Default(),
		Error:    func(error) {},
	}

	pkgName := parsed[0].Name.Name
	_, err := conf.Check(pkgName, fset, parsed, info)
	if err != nil {
		// Keep partial info if available.
	}

	byFile := make(map[string][]ast.Node)
	for _, f := range parsed {
		filePath := canonicalPath(fset.Position(f.Pos()).Filename)
		nodes := collectInterestingNodes(f)
		byFile[filePath] = nodes
	}

	objToKeys := make(map[types.Object][]string)
	addObjKey := func(obj types.Object, key string) {
		if obj == nil || key == "" {
			return
		}
		objToKeys[obj] = append(objToKeys[obj], key)
	}

	for ident, obj := range info.Defs {
		_ = ident
		if obj == nil {
			continue
		}
		keys := objectKeys(obj)
		for _, k := range keys {
			addObjKey(obj, k)
		}
	}

	for ident, obj := range info.Uses {
		_ = ident
		if obj == nil {
			continue
		}
		keys := objectKeys(obj)
		for _, k := range keys {
			addObjKey(obj, k)
		}
	}

	return &typedPackage{
		fset:      fset,
		files:     parsed,
		info:      info,
		byFile:    byFile,
		objToKeys: objToKeys,
	}, nil
}

func collectInterestingNodes(f *ast.File) []ast.Node {
	var out []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		switch n.(type) {
		case *ast.CallExpr, *ast.CompositeLit, *ast.SelectorExpr, *ast.Ident:
			out = append(out, n)
		}
		return true
	})
	return out
}

type nodeIndex struct {
	byName          map[string][]string
	byQualifiedName map[string][]string
	byMethod        map[string][]string // pkg.recv.method
}

func buildNodeIndex(g *graph.Graph) nodeIndex {
	idx := nodeIndex{
		byName:          make(map[string][]string),
		byQualifiedName: make(map[string][]string),
		byMethod:        make(map[string][]string),
	}

	for id, n := range g.Nodes {
		if n == nil || n.Unit == nil {
			continue
		}
		name := strings.TrimSpace(n.Unit.Name)
		if name != "" {
			idx.byName[name] = append(idx.byName[name], id)
		}
		if n.Unit.Package != "" && name != "" {
			k := n.Unit.Package + "." + name
			idx.byQualifiedName[k] = append(idx.byQualifiedName[k], id)
		}

		if n.Unit.UnitType == "method" {
			if recv := receiverFromUnit(n.Unit); recv != "" && n.Unit.Package != "" && name != "" {
				k := n.Unit.Package + "." + recv + "." + name
				idx.byMethod[k] = append(idx.byMethod[k], id)
			}
		}
	}
	return idx
}

func (r *GoTypesResolver) resolveUnresolvedWithTypes(tp *typedPackage, idx nodeIndex, source *graph.Symbol, ur graph.UnresolvedRelation) ([]string, graph.UnresolvedReason) {
	file := canonicalPath(source.Filepath)
	nodes := tp.byFile[file]
	if len(nodes) == 0 {
		return nil, graph.ReasonSourceMissing
	}

	// Try line-local resolution first.
	ids, reason := r.resolveByEvidence(tp, idx, ur, nodes)
	if len(ids) > 0 {
		return ids, ""
	}

	// Fallback: map by typed object keys derived from target hint.
	keys := targetKeys(ur.Target, source.Package)
	ids, ambiguous := resolveKeysToIDs(idx, keys)
	if len(ids) > 0 {
		return ids, ""
	}
	if ambiguous {
		return nil, graph.ReasonAmbiguous
	}
	if reason != "" {
		return nil, reason
	}
	return nil, graph.ReasonNoCandidate
}

func (r *GoTypesResolver) resolveByEvidence(tp *typedPackage, idx nodeIndex, ur graph.UnresolvedRelation, nodes []ast.Node) ([]string, graph.UnresolvedReason) {
	lineStart := ur.Evidence.StartLine
	lineEnd := ur.Evidence.EndLine
	if lineStart <= 0 {
		lineStart = 1
	}
	if lineEnd < lineStart {
		lineEnd = lineStart
	}

	var keys []string
	for _, n := range nodes {
		pos := tp.fset.Position(n.Pos())
		if pos.Line < lineStart || pos.Line > lineEnd {
			continue
		}
		switch node := n.(type) {
		case *ast.CallExpr:
			if ur.Kind != graph.RelationCalls {
				continue
			}
			if k := objectKeyFromCall(tp.info, node); k != "" {
				keys = append(keys, k)
			}
		case *ast.CompositeLit:
			if ur.Kind != graph.RelationInstantiates {
				continue
			}
			if k := objectKeyFromCompositeLit(tp.info, node); k != "" {
				keys = append(keys, k)
			}
		}
	}

	keys = dedupeStrings(keys)
	if len(keys) == 0 {
		return nil, graph.ReasonNoCandidate
	}
	ids, ambiguous := resolveKeysToIDs(idx, keys)
	if len(ids) > 0 {
		return ids, ""
	}
	if ambiguous {
		return nil, graph.ReasonAmbiguous
	}
	return nil, graph.ReasonNoCandidate
}

func objectKeyFromCall(info *types.Info, call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if obj := info.Uses[fun]; obj != nil {
			return bestObjectKey(obj)
		}
	case *ast.SelectorExpr:
		if sel := info.Selections[fun]; sel != nil && sel.Obj() != nil {
			return bestObjectKey(sel.Obj())
		}
		if obj := info.Uses[fun.Sel]; obj != nil {
			return bestObjectKey(obj)
		}
	}
	return ""
}

func objectKeyFromCompositeLit(info *types.Info, lit *ast.CompositeLit) string {
	tv, ok := info.Types[lit.Type]
	if !ok || tv.Type == nil {
		return ""
	}
	t := tv.Type
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj() == nil {
		return ""
	}
	return bestObjectKey(named.Obj())
}

func resolveKeysToIDs(idx nodeIndex, keys []string) ([]string, bool) {
	candidateSet := make(map[string]bool)
	for _, k := range keys {
		for _, id := range idx.byQualifiedName[k] {
			candidateSet[id] = true
		}
		for _, id := range idx.byMethod[k] {
			candidateSet[id] = true
		}
		if parts := strings.Split(k, "."); len(parts) > 0 {
			name := parts[len(parts)-1]
			for _, id := range idx.byName[name] {
				candidateSet[id] = true
			}
		}
	}
	var out []string
	for id := range candidateSet {
		out = append(out, id)
	}
	sort.Strings(out)
	if len(out) == 1 {
		return out, false
	}
	// Ambiguous mappings stay unresolved to avoid introducing low-quality edges.
	if len(out) > 1 {
		return nil, true
	}
	return nil, false
}

func objectKeys(obj types.Object) []string {
	var keys []string
	if obj == nil {
		return keys
	}
	if p := obj.Pkg(); p != nil {
		keys = append(keys, p.Name()+"."+obj.Name())
	}
	keys = append(keys, obj.Name())

	if fn, ok := obj.(*types.Func); ok {
		if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
			recv := typeName(sig.Recv().Type())
			if recv != "" && obj.Pkg() != nil {
				keys = append(keys, obj.Pkg().Name()+"."+recv+"."+obj.Name())
			}
		}
	}

	return dedupeStrings(keys)
}

func bestObjectKey(obj types.Object) string {
	keys := objectKeys(obj)
	if len(keys) == 0 {
		return ""
	}
	// Prefer the richest key first (method, then qualified, then bare).
	sort.Slice(keys, func(i, j int) bool {
		return strings.Count(keys[i], ".") > strings.Count(keys[j], ".")
	})
	return keys[0]
}

func targetKeys(target string, sourcePkg string) []string {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	clean := strings.TrimPrefix(target, "*")
	clean = strings.TrimPrefix(clean, "[]")
	keys := []string{clean}
	if sourcePkg != "" {
		keys = append(keys, sourcePkg+"."+clean)
	}
	if strings.Contains(clean, ".") {
		parts := strings.Split(clean, ".")
		keys = append(keys, parts[len(parts)-1])
	}
	return dedupeStrings(keys)
}

func typeName(t types.Type) string {
	switch tt := t.(type) {
	case *types.Pointer:
		return typeName(tt.Elem())
	case *types.Named:
		return tt.Obj().Name()
	default:
		return ""
	}
}

func receiverFromUnit(u *graph.Symbol) string {
	if u == nil {
		return ""
	}
	return cleanReceiver(u.Metadata.Receiver)
}

func cleanReceiver(recv string) string {
	recv = strings.TrimSpace(recv)
	if recv == "" {
		return ""
	}
	recv = strings.TrimPrefix(recv, "(")
	recv = strings.TrimSuffix(recv, ")")
	parts := strings.Fields(recv)
	if len(parts) == 0 {
		return ""
	}
	name := parts[len(parts)-1]
	name = strings.TrimPrefix(name, "*")
	return name
}

func pkgGroupKey(filePath, pkg string) string {
	dir := filepath.Dir(filePath)
	return canonicalPath(dir) + "|" + strings.TrimSpace(pkg)
}

func canonicalPath(p string) string {
	if p == "" {
		return p
	}
	cp := filepath.Clean(p)
	return filepath.ToSlash(cp)
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func edgeKey(from, to string, kind graph.RelationKind) string {
	return from + "|" + to + "|" + string(kind)
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
