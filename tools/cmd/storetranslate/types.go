package main

import (
	"go/token"
	"go/types"
	"strings"
)

// The two universes: the Pointer types of package ast that name a node, a
// list, a symbol, a flow or a file, and their Store forms in package store
// (the rule table of the 7d-0 instructions 5.1); and the types of the other
// translated packages, whose Store form is the type of the same name in the
// output package (7d-2a instructions 3.1). Every other type (Kind, NodeFlags,
// the types of an untranslated package) is the same type on both sides.
//
// In phase 1 the output packages do not exist yet, and a type of one is a
// placeholder of the right name and package, enough to print it. In phase 2
// the output packages are loaded with stubs, and a type of one is the loaded
// type, with its fields and methods.

const (
	astPath   = "github.com/microsoft/TypeScript/tsc/internal/ast"
	storePath = "github.com/microsoft/TypeScript/tsc/internal/ast/store"
	corePath  = "github.com/microsoft/TypeScript/tsc/internal/core"
)

type universe struct {
	ast   *types.Package
	store *types.Package
	w     *world
	res   *reachResult
	// keyFields are the key-only node fields of the package being translated
	// (keys.go): a NodeKey in the Store.
	keyFields map[*types.Var]bool
	// changedType reports a named type of a translated package whose Store
	// form is the output package's (set by the reach).
	changedType   func(*types.Named) bool
	placeholders  map[string]*types.TypeName
	nodeStructSet map[*types.TypeName]bool
	// r4Sites are the R4' copies the translation inserted (Nodes()).
	r4Sites []string
	r8Sites []string
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

// isNodeLike is a node, the data of one, or a base struct of one: all are a
// Store Node.
func (u *universe) isNodeLike(t types.Type) bool {
	return u.isNode(t) || u.isData(t) || u.isBasePtr(t)
}

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

func (u *universe) isStoreNodeSlice(t types.Type) bool {
	s, ok := types.Unalias(t).(*types.Slice)
	return ok && u.isStoreNode(s.Elem())
}

func (u *universe) isNodeKey(t types.Type) bool {
	return t != nil && types.Identical(t, u.storeType("NodeKey"))
}

func (u *universe) isFactoryPtr(t types.Type) bool {
	p, ok := types.Unalias(t).(*types.Pointer)
	return ok && types.Identical(p.Elem(), u.storeType("Factory"))
}

// isNodeFactory is ast.NodeFactory or *ast.NodeFactory (R7).
func (u *universe) isNodeFactory(t types.Type) bool {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() == u.ast && n.Obj().Name() == "NodeFactory"
}

// containsNode reports whether a Pointer type mentions a node, list, symbol,
// flow or file type of package ast.
func (u *universe) containsNode(t types.Type) bool {
	return u.mentions(t, func(*types.Named) bool { return false })
}

// changes reports whether a Pointer type's Store form differs from it: it
// mentions a node type of ast, or a named type that an output package
// translates.
func (u *universe) changes(t types.Type) bool {
	return u.mentions(t, func(n *types.Named) bool {
		return u.changedType != nil && u.changedType(n.Origin())
	})
}

func (u *universe) mentions(t types.Type, named func(*types.Named) bool) bool {
	seen := map[types.Type]bool{}
	var visit func(t types.Type) bool
	visit = func(t types.Type) bool {
		if t == nil || seen[t] {
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
			if t.Obj().Pkg() == u.ast {
				switch t.Obj().Name() {
				case "SymbolTable", "NodeId", "NodeFactory", "Symbol":
					return true
				}
			}
			if named(t) {
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
		case *types.Interface:
			for i := range t.NumMethods() {
				if visit(t.Method(i).Type()) {
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
	case "Node", "NodeList", "ModifierList", "Symbol", "FlowNode", "FlowList", "SourceFile", "NodeFactory":
		return true
	}
	if u.isView(u.storeType(name)) {
		return true
	}
	tn, ok := u.ast.Scope().Lookup(name).(*types.TypeName)
	return ok && u.nodeStructs()[tn]
}

// outOf is the output package of a Pointer object's package, or nil.
func (u *universe) outOf(obj types.Object) *outPkg {
	if u.w == nil || obj == nil || obj.Pkg() == nil {
		return nil
	}
	return u.w.outs[obj.Pkg().Path()]
}

// provides reports whether the output package o has a package-level name:
// output by the translation, P2, or hand-written.
func (u *universe) provides(o *outPkg, name string) bool {
	if o.manual[name] != nil || o.hand[name] {
		return true
	}
	return u.res != nil && u.res.output[o.srcPath+"."+name]
}

// targetType is the Store form of a named type of a translated package: the
// output package's type of the same name.
func (u *universe) targetType(n *types.Named) (types.Type, bool) {
	obj := n.Obj()
	o := u.outOf(obj)
	if o == nil || !u.provides(o, obj.Name()) {
		return nil, false
	}
	if o.mode == modeAST {
		// The types of ast that the output has: the files output whole, and
		// the types that hold nodes (astTypeTranslated).
		output := u.res != nil && u.res.output[o.srcPath+"."+obj.Name()]
		if !output && !(o.hand[obj.Name()] && u.sameShape(n)) {
			return nil, false
		}
	}
	var tn *types.TypeName
	if u.w.stubbed {
		pkg := u.w.byPath[o.path]
		if pkg == nil || pkg.Types == nil {
			return nil, false
		}
		tn, _ = pkg.Types.Scope().Lookup(obj.Name()).(*types.TypeName)
		if tn == nil {
			return nil, false
		}
	} else {
		tn = u.placeholder(o, obj.Name())
	}
	t := tn.Type()
	if args := n.TypeArgs(); args != nil && args.Len() > 0 {
		var targs []types.Type
		for i := range args.Len() {
			m, _ := u.mapType(args.At(i))
			targs = append(targs, m)
		}
		inst, err := types.Instantiate(nil, t, targs, false)
		if err != nil {
			return nil, false
		}
		return inst, true
	}
	return t, true
}

// placeholder is a named type of an output package in phase 1: a name to
// print, with no fields or methods.
func (u *universe) placeholder(o *outPkg, name string) *types.TypeName {
	if u.placeholders == nil {
		u.placeholders = map[string]*types.TypeName{}
	}
	key := o.path + "." + name
	if tn, ok := u.placeholders[key]; ok {
		return tn
	}
	pkg := types.NewPackage(o.path, o.name)
	tn := types.NewTypeName(token.NoPos, pkg, name, nil)
	types.NewNamed(tn, types.NewStruct(nil, nil), nil)
	u.placeholders[key] = tn
	return tn
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
				case "NodeFactory":
					return types.NewPointer(u.storeType("Factory"))
				}
				if v := u.storeType(name); u.isView(v) {
					return v
				}
				if st := u.storeStruct(name); st != nil {
					return types.NewPointer(st)
				}
				if u.isBasePtr(t) {
					return u.storeType("Node")
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
				case "NodeFactory":
					// R7: a NodeFactory held by value is the Store's *Factory.
					return types.NewPointer(u.storeType("Factory"))
				case "Symbol":
					// A Symbol by value: a composite literal of one.
					return u.storeType("Symbol")
				case "Node", "NodeList", "ModifierList", "FlowNode":
					ok = false
				}
			}
			if target, found := u.targetType(t); found {
				return target
			}
			args := t.TypeArgs()
			if args == nil || args.Len() == 0 {
				return t
			}
			if set := u.nodeSetType(t); set != nil {
				return set
			}
			var targs []types.Type
			changed := false
			tparams := t.Origin().TypeParams()
			for i := range args.Len() {
				a := m(args.At(i))
				// L1: a comparable type parameter takes the node's key.
				if u.isStoreNode(a) && tparams != nil && i < tparams.Len() && isComparableConstraint(tparams.At(i)) {
					a = u.storeType("NodeKey")
				}
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

func isComparableConstraint(tp *types.TypeParam) bool {
	iface, ok := tp.Constraint().Underlying().(*types.Interface)
	return ok && iface.IsComparable()
}

// nodeSetType is R5: a generic collection whose element is a node and whose
// Store version is package store's type of the same name with Node before
// Set (collections.Set -> store.NodeSet, OrderedSet -> OrderedNodeSet). The
// Pointer set reads its nodes back (Keys, Values), which a set of NodeKey
// cannot.
func (u *universe) nodeSetType(t *types.Named) types.Type {
	args := t.TypeArgs()
	if args.Len() != 1 || !u.isNode(args.At(0)) {
		return nil
	}
	name := t.Obj().Name()
	if !strings.HasSuffix(name, "Set") {
		return nil
	}
	return u.storeType(strings.TrimSuffix(name, "Set") + "NodeSet")
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
	for i := range ms.Len() {
		if ms.At(i).Obj().Name() == name {
			if f, ok := ms.At(i).Obj().(*types.Func); ok {
				return f
			}
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

// storeField is the field name of a Store struct type (through a pointer),
// promoted fields included.
func storeField(t types.Type, name string) *types.Var {
	if t == nil {
		return nil
	}
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, name)
	if v, ok := obj.(*types.Var); ok && v.IsField() {
		return v
	}
	// An unexported field of another package: look it up by name.
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if s, ok := t.Underlying().(*types.Struct); ok {
		for i := range s.NumFields() {
			if f := s.Field(i); f.Name() == name {
				return f
			}
		}
	}
	return nil
}

// nodeStructs are the struct types of package ast whose value is a node in
// the Store: Node, the data of each kind (the types that implement
// ast.nodeData), and the base structs that the data embed (NodeBase,
// FunctionLikeBase, NamedMemberBase, ...). A pointer to one of them, and the
// address of an embedded one (&data.Node, &data.NamedMemberBase), is the
// node; a field of one is read with the node's accessor.
func (u *universe) nodeStructs() map[*types.TypeName]bool {
	if u.nodeStructSet != nil {
		return u.nodeStructSet
	}
	set := map[*types.TypeName]bool{}
	u.nodeStructSet = set
	iface, _ := u.ast.Scope().Lookup("nodeData").Type().Underlying().(*types.Interface)
	var embed func(t types.Type)
	embed = func(t types.Type) {
		named, ok := types.Unalias(t).(*types.Named)
		if !ok || named.Obj().Pkg() != u.ast {
			return
		}
		s, ok := named.Underlying().(*types.Struct)
		if !ok || set[named.Obj()] {
			return
		}
		set[named.Obj()] = true
		for i := range s.NumFields() {
			if s.Field(i).Embedded() {
				embed(s.Field(i).Type())
			}
		}
	}
	for _, name := range u.ast.Scope().Names() {
		tn, ok := u.ast.Scope().Lookup(name).(*types.TypeName)
		if !ok || tn.IsAlias() {
			continue
		}
		if _, ok := tn.Type().Underlying().(*types.Struct); !ok {
			continue
		}
		if iface != nil && types.Implements(types.NewPointer(tn.Type()), iface) {
			embed(tn.Type())
		}
	}
	embed(u.ast.Scope().Lookup("Node").Type())
	return set
}

// isNodeStruct is a named struct of nodeStructs (not a pointer).
func (u *universe) isNodeStruct(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	return ok && u.nodeStructs()[named.Obj()]
}

// isBasePtr is a pointer to a node struct that has no view: a base struct,
// or the data of a kind without members. Its Store form is Node.
func (u *universe) isBasePtr(t types.Type) bool {
	p, ok := types.Unalias(t).(*types.Pointer)
	if !ok || !u.isNodeStruct(p.Elem()) {
		return false
	}
	name := u.pointerNamed(t)
	return name != "Node" && name != "SourceFile" && !u.isView(u.storeType(name)) && u.storeStruct(name) == nil
}

// storeStruct is a struct type of package store with the name of an ast
// type, which is not a view: the data of a synthetic flow node, read from
// the flow (FlowSwitchClauseData).
func (u *universe) storeStruct(name string) types.Type {
	t := u.storeType(name)
	if t == nil || u.isView(t) {
		return nil
	}
	if _, ok := t.Underlying().(*types.Struct); !ok {
		return nil
	}
	return t
}

// sameShape: package store's hand-written type of the name of an ast type is
// its Store form only when it has the ast type's shape, mapped (Visitor,
// PatternAmbientModule); a type that only shares the name (store.SymbolId,
// the Bound's dense index, against ast.SymbolId) is not.
func (u *universe) sameShape(n *types.Named) bool {
	st := u.storeType(n.Obj().Name())
	if st == nil {
		return false
	}
	m, ok := u.mapType(n.Underlying())
	return ok && types.Identical(m, st.Underlying())
}

// astTypeInStore: a named type of package ast whose Store form is package
// store's type of the same name: one the output has (the files output whole,
// the types that hold nodes), or a hand-written one of the same shape.
func (u *universe) astTypeInStore(n *types.Named) bool {
	o := u.outOf(n.Obj())
	if o == nil || o.mode != modeAST {
		return false
	}
	name := n.Obj().Name()
	if d := u.w.decl(o.srcPath, name); d != nil && o.isFileLevel(d.fileName) {
		return true
	}
	if u.res != nil && u.res.output[o.srcPath+"."+name] {
		return true
	}
	return o.hand[name] && u.sameShape(n)
}
