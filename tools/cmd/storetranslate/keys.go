package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
)

// The rule of the composite keys (Z1, 7d-0 instructions 5.1): a struct that
// is the key of a map or the element of a collections.Set, and holds a node,
// holds the node's identity only. Such a field becomes a store.NodeKey, and
// every construction and assignment of it takes node.Key() (a nil literal is
// NodeKey{}). The rule starts from the actual key and element types, follows
// nested structs and arrays, and requires that the field is never read back
// as a node: a read, an exported field (used outside the analysed package)
// or a pointer to the field leaves the key unproven, and is reported instead
// of rewritten.

const collectionsPath = "github.com/microsoft/TypeScript/tsc/internal/collections"

// keyFields finds the key-only node fields of pkg. A field found here is a
// NodeKey in the output (universe.keyFields).
func keyFields(u *universe, fset *token.FileSet, pkg *types.Package, info *types.Info, files []*ast.File) (map[*types.Var]bool, []problem) {
	keyTypes := map[types.Type]bool{}
	note := func(t types.Type) {
		if t != nil {
			keyTypes[types.Unalias(t)] = true
		}
	}
	var visitType func(t types.Type, seen map[types.Type]bool)
	visitType = func(t types.Type, seen map[types.Type]bool) {
		if t == nil || seen[t] {
			return
		}
		seen[t] = true
		switch t := types.Unalias(t).(type) {
		case *types.Map:
			note(t.Key())
			visitType(t.Elem(), seen)
		case *types.Named:
			if obj := t.Obj(); obj.Pkg() != nil && obj.Pkg().Path() == collectionsPath && t.TypeArgs() != nil && t.TypeArgs().Len() > 0 {
				switch obj.Name() {
				case "Set", "OrderedSet", "SyncSet":
					note(t.TypeArgs().At(0))
				case "OrderedMap", "SyncMap":
					note(t.TypeArgs().At(0))
				}
			}
			if t.Obj().Pkg() == pkg {
				visitType(t.Underlying(), seen)
			}
		case *types.Struct:
			for i := range t.NumFields() {
				visitType(t.Field(i).Type(), seen)
			}
		case *types.Pointer:
			visitType(t.Elem(), seen)
		case *types.Slice:
			visitType(t.Elem(), seen)
		case *types.Array:
			visitType(t.Elem(), seen)
		case *types.Signature:
			for i := range t.Params().Len() {
				visitType(t.Params().At(i).Type(), seen)
			}
			for i := range t.Results().Len() {
				visitType(t.Results().At(i).Type(), seen)
			}
		}
	}
	seen := map[types.Type]bool{}
	for _, tv := range info.Types {
		visitType(tv.Type, seen)
	}
	for _, obj := range info.Defs {
		if obj != nil {
			visitType(obj.Type(), seen)
		}
	}

	// The node fields of each key type, through nested structs and arrays.
	fields := map[*types.Var]types.Type{} // field -> the key type it belongs to
	var collect func(key, t types.Type, depth int)
	collect = func(key, t types.Type, depth int) {
		if depth > 8 {
			return
		}
		switch u2 := types.Unalias(t).(type) {
		case *types.Named:
			if s, ok := u2.Underlying().(*types.Struct); ok {
				collectStruct(u, key, s, fields, collect, depth)
			}
		case *types.Struct:
			collectStruct(u, key, u2, fields, collect, depth)
		case *types.Array:
			collect(key, u2.Elem(), depth+1)
		}
	}
	for t := range keyTypes {
		collect(t, t, 0)
	}

	// The uses of each field: a construction or an assignment only.
	var problems []problem
	bad := map[*types.Var]string{}
	for field := range fields {
		if field.Exported() {
			bad[field] = "exported, so used outside the package"
		}
	}
	for _, file := range files {
		assigned := map[*ast.SelectorExpr]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			if a, ok := n.(*ast.AssignStmt); ok && a.Tok == token.ASSIGN {
				for _, l := range a.Lhs {
					if sel, ok := l.(*ast.SelectorExpr); ok {
						assigned[sel] = true
					}
				}
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			s := info.Selections[sel]
			if s == nil || s.Kind() != types.FieldVal {
				return true
			}
			field, _ := s.Obj().(*types.Var)
			if _, isKey := fields[field]; !isKey || assigned[sel] {
				return true
			}
			if _, done := bad[field]; !done {
				bad[field] = fmt.Sprintf("read as a node at %s", fset.Position(sel.Pos()))
			}
			return true
		})
	}
	out := map[*types.Var]bool{}
	var names []*types.Var
	for field := range fields {
		names = append(names, field)
	}
	sort.Slice(names, func(i, j int) bool { return names[i].Pos() < names[j].Pos() })
	for _, field := range names {
		if why, ok := bad[field]; ok {
			problems = append(problems, problem{fset.Position(field.Pos()), fmt.Sprintf("the node field %s of the key type %s is not a key only (%s): it needs another rule, such as a separate key and value", field.Name(), fields[field], why)})
			continue
		}
		out[field] = true
	}
	return out, problems
}

func collectStruct(u *universe, key types.Type, s *types.Struct, fields map[*types.Var]types.Type, collect func(key, t types.Type, depth int), depth int) {
	for i := range s.NumFields() {
		f := s.Field(i)
		if u.isNode(f.Type()) || u.isData(f.Type()) || isNodeArray(u, f.Type()) {
			fields[f] = key
			continue
		}
		collect(key, f.Type(), depth+1)
	}
}

func isNodeArray(u *universe, t types.Type) bool {
	a, ok := types.Unalias(t).(*types.Array)
	if !ok {
		return false
	}
	return u.isNode(a.Elem()) || isNodeArray(u, a.Elem())
}

// keyValue is the value stored in a key field: node.Key(), NodeKey{} for nil,
// and an array of keys for an array of nodes (element by element, in a
// composite literal only).
func (f *fn) keyValue(e ast.Expr, fieldType types.Type) ast.Expr {
	if isNil(e) {
		return &ast.CompositeLit{Type: f.qualify(f.store, "NodeKey", e.Pos()), Lbrace: e.Pos(), Rbrace: e.Pos()}
	}
	if isNodeArray(f.universe, fieldType) {
		f.problem(e, "an array of nodes in a key: no rule for a value that is not a literal")
		x, _ := f.expr(e, fieldType)
		return x
	}
	x, st := f.expr(e, fieldType)
	return method(f.asNode(x, st), "Key", e.Pos())
}
