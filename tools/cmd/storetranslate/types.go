package main

import (
	"go/types"
	"strings"
)

// The two universes: the Pointer types of package ast that name a node, a
// list, a symbol, a flow or a file, and their Store forms in package store
// (the rule table of the 7d-0 instructions 5.1). Every other type of package
// ast (Kind, NodeFlags, ...) is the same type on both sides.

const (
	astPath   = "github.com/microsoft/TypeScript/tsc/internal/ast"
	storePath = "github.com/microsoft/TypeScript/tsc/internal/ast/store"
	corePath  = "github.com/microsoft/TypeScript/tsc/internal/core"
)

type universe struct {
	ast   *types.Package
	store *types.Package
	// keyFields are the key-only node fields of the package being translated
	// (keys.go): a NodeKey in the Store.
	keyFields map[*types.Var]bool
}

// keyType is the Store type of a key-only field: NodeKey for a node, an
// array of them for an array of nodes.
func (u *universe) keyType(t types.Type) types.Type {
	if a, ok := types.Unalias(t).(*types.Array); ok {
		return types.NewArray(u.keyType(a.Elem()), a.Len())
	}
	return u.storeType("NodeKey")
}

func (u *universe) storeType(name string) types.Type {
	obj := u.store.Scope().Lookup(name)
	if obj == nil {
		return nil
	}
	return obj.Type()
}

// pointerNamed is the named ast type that t points to, or "".
func (u *universe) pointerNamed(t types.Type) string {
	p, ok := types.Unalias(t).(*types.Pointer)
	if !ok {
		return ""
	}
	n, ok := types.Unalias(p.Elem()).(*types.Named)
	if !ok || n.Obj().Pkg() != u.ast {
		return ""
	}
	return n.Obj().Name()
}

func (u *universe) isNode(t types.Type) bool { return u.pointerNamed(t) == "Node" }

func (u *universe) isList(t types.Type) bool {
	name := u.pointerNamed(t)
	return name == "NodeList" || name == "ModifierList"
}

func (u *universe) isSourceFile(t types.Type) bool { return u.pointerNamed(t) == "SourceFile" }
func (u *universe) isSymbol(t types.Type) bool     { return u.pointerNamed(t) == "Symbol" }
func (u *universe) isFlow(t types.Type) bool       { return u.pointerNamed(t) == "FlowNode" }
func (u *universe) isFlowList(t types.Type) bool   { return u.pointerNamed(t) == "FlowList" }

// isData is a pointer to the data struct of a node definition that has a
// typed view in the Store (*ast.BinaryExpression -> store.BinaryExpression).
// SourceFile is the file, not a view.
func (u *universe) isData(t types.Type) bool {
	name := u.pointerNamed(t)
	if name == "" || name == "Node" || name == "SourceFile" {
		return false
	}
	return u.isView(u.storeType(name))
}

// isNodeLike is a node or the data of one: both are a Store Node.
func (u *universe) isNodeLike(t types.Type) bool { return u.isNode(t) || u.isData(t) }

func (u *universe) isNodeSlice(t types.Type) bool {
	s, ok := types.Unalias(t).(*types.Slice)
	return ok && u.isNode(s.Elem())
}

// isView is store.Node or a typed view: a named type of package store whose
// underlying type is Node's.
func (u *universe) isView(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() != u.store {
		return false
	}
	node := u.storeType("Node")
	return types.Identical(n.Underlying(), node.Underlying())
}

func (u *universe) isStoreNode(t types.Type) bool {
	return t != nil && types.Identical(t, u.storeType("Node"))
}

func (u *universe) isStoreList(t types.Type) bool {
	return t != nil && types.Identical(t, u.storeType("List"))
}

// containsNode reports whether a Pointer type mentions a type that the
// mapping changes, so that its syntax cannot be kept.
func (u *universe) containsNode(t types.Type) bool {
	seen := map[types.Type]bool{}
	var visit func(t types.Type) bool
	visit = func(t types.Type) bool {
		if seen[t] {
			return false
		}
		seen[t] = true
		switch t := t.(type) {
		case *types.Alias:
			return visit(types.Unalias(t))
		case *types.Pointer:
			if name := u.pointerNamed(t); name != "" && u.mapsPointer(name) {
				return true
			}
			return visit(t.Elem())
		case *types.Named:
			if t.Obj().Pkg() == u.ast && (t.Obj().Name() == "SymbolTable" || t.Obj().Name() == "NodeId") {
				return true
			}
			if args := t.TypeArgs(); args != nil {
				for i := range args.Len() {
					if visit(args.At(i)) {
						return true
					}
				}
			}
			return false
		case *types.Slice:
			return visit(t.Elem())
		case *types.Array:
			return visit(t.Elem())
		case *types.Map:
			return visit(t.Key()) || visit(t.Elem())
		case *types.Chan:
			return visit(t.Elem())
		case *types.Signature:
			for i := range t.Params().Len() {
				if visit(t.Params().At(i).Type()) {
					return true
				}
			}
			for i := range t.Results().Len() {
				if visit(t.Results().At(i).Type()) {
					return true
				}
			}
			return false
		case *types.Struct:
			for i := range t.NumFields() {
				if visit(t.Field(i).Type()) {
					return true
				}
			}
			return false
		case *types.Tuple:
			for i := range t.Len() {
				if visit(t.At(i).Type()) {
					return true
				}
			}
		}
		return false
	}
	return visit(t)
}

func (u *universe) mapsPointer(name string) bool {
	switch name {
	case "Node", "NodeList", "ModifierList", "Symbol", "FlowNode", "FlowList", "SourceFile":
		return true
	}
	return u.isView(u.storeType(name))
}

// mapType is the Store form of a Pointer type. ok is false when the type has
// no Store form (a node struct by value, an unmapped data struct).
func (u *universe) mapType(t types.Type) (types.Type, bool) {
	ok := true
	var m func(t types.Type) types.Type
	m = func(t types.Type) types.Type {
		switch t := t.(type) {
		case *types.Alias:
			return m(types.Unalias(t))
		case *types.Pointer:
			if name := u.pointerNamed(t); name != "" {
				switch name {
				case "Node":
					return u.storeType("Node")
				case "NodeList", "ModifierList":
					return u.storeType("List")
				case "Symbol":
					return types.NewPointer(u.storeType("Symbol"))
				case "FlowNode":
					return u.storeType("Flow")
				case "FlowList":
					return u.storeType("FlowList")
				case "SourceFile":
					return types.NewPointer(u.storeType("File"))
				}
				if v := u.storeType(name); u.isView(v) {
					return v
				}
			}
			return types.NewPointer(m(t.Elem()))
		case *types.Named:
			if t.Obj().Pkg() == u.ast {
				switch t.Obj().Name() {
				case "SymbolTable":
					return u.storeType("SymbolTable")
				case "NodeId":
					return u.storeType("NodeKey")
				case "Node", "NodeList", "ModifierList", "FlowNode":
					ok = false
				}
				return t
			}
			args := t.TypeArgs()
			if args == nil || args.Len() == 0 {
				return t
			}
			var targs []types.Type
			changed := false
			for i := range args.Len() {
				a := m(args.At(i))
				changed = changed || a != args.At(i)
				targs = append(targs, a)
			}
			if !changed {
				return t
			}
			inst, err := types.Instantiate(nil, t.Origin(), targs, true)
			if err != nil {
				ok = false
				return t
			}
			return inst
		case *types.Slice:
			return types.NewSlice(m(t.Elem()))
		case *types.Array:
			return types.NewArray(m(t.Elem()), t.Len())
		case *types.Map:
			key := m(t.Key())
			if u.isNode(t.Key()) {
				key = u.storeType("NodeKey")
			}
			return types.NewMap(key, m(t.Elem()))
		case *types.Chan:
			return types.NewChan(t.Dir(), m(t.Elem()))
		case *types.Signature:
			return types.NewSignatureType(nil, nil, nil, m(t.Params()).(*types.Tuple), m(t.Results()).(*types.Tuple), t.Variadic())
		case *types.Tuple:
			if t == nil {
				return t
			}
			vars := make([]*types.Var, t.Len())
			for i := range t.Len() {
				v := t.At(i)
				vars[i] = types.NewVar(v.Pos(), v.Pkg(), v.Name(), m(v.Type()))
			}
			return types.NewTuple(vars...)
		case *types.Struct:
			fields := make([]*types.Var, t.NumFields())
			tags := make([]string, t.NumFields())
			for i := range t.NumFields() {
				f := t.Field(i)
				ft := m(f.Type())
				if u.keyFields[f] {
					ft = u.keyType(f.Type())
				}
				fields[i] = types.NewField(f.Pos(), f.Pkg(), f.Name(), ft, f.Embedded())
				tags[i] = t.Tag(i)
			}
			return types.NewStruct(fields, tags)
		}
		return t
	}
	r := m(t)
	return r, ok
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func uncapitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// storeMethod is the method name of a Store type, or nil: value or pointer
// receiver.
func storeMethod(t types.Type, name string) *types.Func {
	if t == nil {
		return nil
	}
	ms := types.NewMethodSet(t)
	if sel := ms.Lookup(nil, name); sel != nil {
		if f, ok := sel.Obj().(*types.Func); ok {
			return f
		}
	}
	if _, isPtr := t.(*types.Pointer); !isPtr {
		ms = types.NewMethodSet(types.NewPointer(t))
		for i := range ms.Len() {
			if ms.At(i).Obj().Name() == name {
				if f, ok := ms.At(i).Obj().(*types.Func); ok {
					return f
				}
			}
		}
	}
	return nil
}

// storeField is the field name of a Store struct type (through a pointer).
func storeField(t types.Type, name string) *types.Var {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	s, ok := t.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	for i := range s.NumFields() {
		if f := s.Field(i); f.Name() == name && f.Exported() {
			return f
		}
	}
	return nil
}
