package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// The rules (7d-0 instructions 5.1), applied to one function at a time:
//
//	*ast.Node (and its aliases)          store.Node
//	x == nil, x != nil (x a node)        x.IsNil(), !x.IsNil()
//	a == b, a != b (both nodes)          store.SameNode(a, b), !store.SameNode(a, b)
//	x.Kind, x.Flags, x.Parent, x.Loc     x.Kind(), x.Flags(), x.Parent(), x.Loc()
//	x.AsXxx().Field                      x.AsXxx().Field()
//	*ast.NodeList, list.Nodes            store.List; len, index and range as Len, At and an index loop
//	ast.GetSourceFileOfNode(x)           x.File()
//	ast.GetNodeId(x), map[*ast.Node]T    x.Key(), map[store.NodeKey]T
//	slices.Contains / Index on nodes     store.Contains / Index (SameNode)
//	core.FirstNonNil / MapNonNil / OrElse, SameMap / SameMapIndex /
//	  Deduplicate / AppendIfUnique       store's Node versions, by the actual type arguments
//	*ast.Symbol, ast.SymbolTable,
//	  *ast.FlowNode                      *store.Symbol, store.SymbolTable, store.Flow
//	x.FlowNodeData().FlowNode            x.FlowNode()
//	ast.Xxx(...)                         store.Xxx(...)
//
// A construct no rule covers is a problem: the function is left to the P2
// replacement (store/utilities_manual.go) and the problem is reported.

type problem struct {
	pos token.Position
	msg string
}

// target is the package the output belongs to.
type target struct {
	o    *outPkg // nil for a fixture of the tests
	name string  // package name
	path string  // import path
	// local: the names of the source package are the output's (a fixture of
	// the tests, whose source is not in the table).
	local   bool
	imports map[string]string // path -> name
}

type translator struct {
	*universe
	fset *token.FileSet
	src  *types.Package
	info *types.Info
	out  *target
}

// fn is the state of the function being translated.
type fn struct {
	*translator
	vars    map[types.Object]types.Type // the Store type of a local whose type the mapping does not give
	results [][]types.Type              // the Pointer result types, innermost function last
	names   map[string]bool             // identifiers in the function, for fresh names
	posConv map[*ast.CallExpr]*ast.CallExpr
	// refSlices are the []NodeRef expressions of a file (R3) and the Store
	// that resolves their elements.
	refSlices map[ast.Expr]ast.Expr
	// selBases are the calls that are the X of a selector: x.XxxData() is
	// read only as x.XxxData().F.
	selBases map[*ast.CallExpr]bool
	r4Sites  []string
	r8Sites  []string      // the writes of R8, for the report
	decl     *ast.FuncDecl // the function being translated, for R8
	// listParams: the []*ast.Node parameters are store.List (listDraft).
	listParams bool
	problems   []problem
}

func (t *translator) newFn(decl ast.Node) *fn {
	f := &fn{translator: t, vars: map[types.Object]types.Type{}, names: map[string]bool{}, posConv: map[*ast.CallExpr]*ast.CallExpr{}, refSlices: map[ast.Expr]ast.Expr{}, selBases: map[*ast.CallExpr]bool{}}
	ast.Inspect(decl, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			f.names[n.Name] = true
		case *ast.SelectorExpr:
			if c, ok := n.X.(*ast.CallExpr); ok {
				f.selBases[c] = true
			}
		}
		return true
	})
	return f
}

func (f *fn) problem(n ast.Node, format string, args ...any) {
	f.problems = append(f.problems, problem{f.fset.Position(n.Pos()), fmt.Sprintf(format, args...)})
}

func (f *fn) fresh(base string) string {
	name := base
	for i := 2; f.names[name]; i++ {
		name = fmt.Sprintf("%s%d", base, i)
	}
	f.names[name] = true
	return name
}

// ── Names and types ──

// qualify names a package-level object of a Store package (store, an output
// package, or a package the Store shares with the Pointer).
func (f *fn) qualify(pkg *types.Package, name string, pos token.Pos) ast.Expr {
	if pkg == nil {
		return &ast.Ident{NamePos: pos, Name: name}
	}
	return f.qualifyPath(pkg.Path(), pkg.Name(), name, pos)
}

func (f *fn) qualifyPath(path, pkgName, name string, pos token.Pos) ast.Expr {
	if path == f.out.path {
		return &ast.Ident{NamePos: pos, Name: name}
	}
	local := f.importPath(path, pkgName)
	return &ast.SelectorExpr{X: &ast.Ident{NamePos: pos, Name: local}, Sel: &ast.Ident{NamePos: pos, Name: name}}
}

func (f *fn) importName(pkg *types.Package) string {
	return f.importPath(pkg.Path(), pkg.Name())
}

func (f *fn) importPath(path, name string) string {
	if local, ok := f.out.imports[path]; ok {
		return local
	}
	f.out.imports[path] = name
	return name
}

// storeFunc is the Store name of an ast function: unqualified in package
// store, store.X elsewhere.
func (f *fn) storeFunc(name string, pos token.Pos) ast.Expr {
	return f.qualify(f.store, name, pos)
}

// ref names a package-level object of the Pointer universe in the output:
// the output package's version when it has one (translated, P2 or
// hand-written), else the Pointer package's, which the Store shares when the
// object's type does not change.
func (f *fn) ref(obj types.Object, pos token.Pos) ast.Expr {
	pkg := obj.Pkg()
	name := obj.Name()
	if pkg == f.src && f.out.local || obj.Parent() != pkg.Scope() {
		// The fixture's own names, and a name local to a function.
		return &ast.Ident{NamePos: pos, Name: name}
	}
	if o := f.outOf(obj); o != nil {
		if o.mode == modeAST {
			if fun, ok := obj.(*types.Func); ok {
				if keptInAST(f.universe, fun) {
					return f.qualify(pkg, name, pos)
				}
				if !f.provides(o, name) {
					f.problem(&ast.Ident{NamePos: pos}, "ast.%s is not translated", name)
				}
				return f.qualifyPath(o.path, o.name, name, pos)
			}
			if f.res != nil && f.res.output[o.srcPath+"."+name] {
				return f.qualifyPath(o.path, o.name, name, pos)
			}
			if tn, ok := obj.(*types.TypeName); ok {
				if named, ok := types.Unalias(tn.Type()).(*types.Named); ok && !tn.IsAlias() && f.astTypeInStore(named) {
					return f.qualifyPath(o.path, o.name, name, pos)
				}
			}
			return f.qualify(pkg, name, pos)
		}
		if f.provides(o, name) {
			return f.qualifyPath(o.path, o.name, name, pos)
		}
		if f.changes(obj.Type()) || f.holdsNodes(obj) {
			f.problem(&ast.Ident{NamePos: pos}, "%s.%s has no Store version", pkg.Name(), name)
		}
	}
	if !obj.Exported() && pkg.Path() != f.out.path {
		f.problem(&ast.Ident{NamePos: pos}, "the unexported %s.%s is not in %s", pkg.Name(), name, f.out.name)
	}
	return f.qualify(pkg, name, pos)
}

// typeExpr renders a Store type.
func (f *fn) typeExpr(t types.Type, pos token.Pos) ast.Expr {
	switch t := t.(type) {
	case *types.Alias:
		return f.qualify(t.Obj().Pkg(), t.Obj().Name(), pos)
	case *types.Named:
		name := f.qualify(t.Obj().Pkg(), t.Obj().Name(), pos)
		args := t.TypeArgs()
		if args == nil || args.Len() == 0 {
			return name
		}
		var indices []ast.Expr
		for i := range args.Len() {
			indices = append(indices, f.typeExpr(args.At(i), pos))
		}
		if len(indices) == 1 {
			return &ast.IndexExpr{X: name, Lbrack: pos, Index: indices[0], Rbrack: pos}
		}
		return &ast.IndexListExpr{X: name, Lbrack: pos, Indices: indices, Rbrack: pos}
	case *types.Basic:
		if t.Kind() == types.UnsafePointer {
			return f.qualifyPath("unsafe", "unsafe", "Pointer", pos)
		}
		return &ast.Ident{NamePos: pos, Name: t.Name()}
	case *types.Pointer:
		return &ast.StarExpr{Star: pos, X: f.typeExpr(t.Elem(), pos)}
	case *types.Slice:
		return &ast.ArrayType{Lbrack: pos, Elt: f.typeExpr(t.Elem(), pos)}
	case *types.Array:
		return &ast.ArrayType{Lbrack: pos, Len: &ast.BasicLit{ValuePos: pos, Kind: token.INT, Value: fmt.Sprint(t.Len())}, Elt: f.typeExpr(t.Elem(), pos)}
	case *types.Map:
		return &ast.MapType{Map: pos, Key: f.typeExpr(t.Key(), pos), Value: f.typeExpr(t.Elem(), pos)}
	case *types.Chan:
		dir := ast.SEND | ast.RECV
		switch t.Dir() {
		case types.SendOnly:
			dir = ast.SEND
		case types.RecvOnly:
			dir = ast.RECV
		}
		return &ast.ChanType{Begin: pos, Dir: dir, Value: f.typeExpr(t.Elem(), pos)}
	case *types.Signature:
		return &ast.FuncType{Func: pos, Params: f.fieldList(t.Params(), t.Variadic(), pos), Results: f.fieldList(t.Results(), false, pos)}
	case *types.Interface:
		if t.Empty() {
			return &ast.Ident{NamePos: pos, Name: "any"}
		}
		methods := &ast.FieldList{Opening: pos, Closing: pos}
		for i := range t.NumEmbeddeds() {
			methods.List = append(methods.List, &ast.Field{Type: f.typeExpr(t.EmbeddedType(i), pos)})
		}
		for i := range t.NumExplicitMethods() {
			m := t.ExplicitMethod(i)
			sig := m.Type().(*types.Signature)
			methods.List = append(methods.List, &ast.Field{Names: []*ast.Ident{{NamePos: pos, Name: m.Name()}}, Type: &ast.FuncType{Func: token.NoPos, Params: f.fieldList(sig.Params(), sig.Variadic(), pos), Results: f.fieldList(sig.Results(), false, pos)}})
		}
		return &ast.InterfaceType{Interface: pos, Methods: methods}
	case *types.Struct:
		fields := &ast.FieldList{Opening: pos, Closing: pos}
		for i := range t.NumFields() {
			fv := t.Field(i)
			fl := &ast.Field{Type: f.typeExpr(fv.Type(), pos)}
			if !fv.Embedded() {
				fl.Names = []*ast.Ident{{NamePos: pos, Name: fv.Name()}}
			}
			fields.List = append(fields.List, fl)
		}
		return &ast.StructType{Struct: pos, Fields: fields}
	case *types.TypeParam:
		return &ast.Ident{NamePos: pos, Name: t.Obj().Name()}
	}
	return &ast.Ident{NamePos: pos, Name: fmt.Sprintf("/* %s */", t)}
}

func (f *fn) fieldList(tuple *types.Tuple, variadic bool, pos token.Pos) *ast.FieldList {
	if tuple == nil || tuple.Len() == 0 {
		return nil
	}
	list := &ast.FieldList{Opening: pos, Closing: pos}
	for i := range tuple.Len() {
		v := tuple.At(i)
		var typ ast.Expr
		if variadic && i == tuple.Len()-1 {
			typ = &ast.Ellipsis{Ellipsis: pos, Elt: f.typeExpr(v.Type().(*types.Slice).Elem(), pos)}
		} else {
			typ = f.typeExpr(v.Type(), pos)
		}
		field := &ast.Field{Type: typ}
		if v.Name() != "" {
			field.Names = []*ast.Ident{{NamePos: pos, Name: v.Name()}}
		}
		list.List = append(list.List, field)
	}
	return list
}

// typ translates a type expression, following its syntax: a type name is
// the Store's (the mapping of a node type of ast, or ref), and the type
// constructors keep their shape, parameter names and field names.
func (f *fn) typ(e ast.Expr) ast.Expr {
	if e == nil {
		return nil
	}
	t := f.info.TypeOf(e)
	if t == nil {
		f.problem(e, "no type for type expression")
		return e
	}
	switch x := e.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		var id *ast.Ident
		if sel, ok := x.(*ast.SelectorExpr); ok {
			id = sel.Sel
		} else {
			id = x.(*ast.Ident)
		}
		tn, ok := f.info.Uses[id].(*types.TypeName)
		if !ok || tn.Pkg() == nil {
			// A predeclared type, or a type parameter.
			return &ast.Ident{NamePos: id.Pos(), Name: id.Name}
		}
		if tn.Pkg() == f.ast && f.changes(t) {
			m, ok := f.mapType(t)
			if !ok {
				f.problem(e, "type %s has no Store form", t)
			}
			return f.typeExpr(m, e.Pos())
		}
		return f.ref(tn, e.Pos())
	case *ast.StarExpr:
		if f.changes(t) {
			if name := f.pointerNamed(t); name != "" && f.mapsPointer(name) {
				m, ok := f.mapType(t)
				if !ok {
					f.problem(e, "type %s has no Store form", t)
				}
				return f.typeExpr(m, e.Pos())
			}
		}
		return &ast.StarExpr{Star: x.Star, X: f.typ(x.X)}
	case *ast.ParenExpr:
		return &ast.ParenExpr{Lparen: x.Lparen, X: f.typ(x.X), Rparen: x.Rparen}
	case *ast.ArrayType:
		return &ast.ArrayType{Lbrack: x.Lbrack, Len: x.Len, Elt: f.typ(x.Elt)}
	case *ast.MapType:
		key := f.typ(x.Key)
		if f.isNode(f.info.TypeOf(x.Key)) {
			key = f.qualify(f.store, "NodeKey", x.Key.Pos())
		}
		return &ast.MapType{Map: x.Map, Key: key, Value: f.typ(x.Value)}
	case *ast.ChanType:
		return &ast.ChanType{Begin: x.Begin, Arrow: x.Arrow, Dir: x.Dir, Value: f.typ(x.Value)}
	case *ast.FuncType:
		return f.funcType(x)
	case *ast.StructType:
		return &ast.StructType{Struct: x.Struct, Fields: f.fields(x.Fields)}
	case *ast.InterfaceType:
		return &ast.InterfaceType{Interface: x.Interface, Methods: f.fields(x.Methods), Incomplete: x.Incomplete}
	case *ast.Ellipsis:
		return &ast.Ellipsis{Ellipsis: x.Ellipsis, Elt: f.typ(x.Elt)}
	case *ast.IndexExpr, *ast.IndexListExpr:
		return f.instanceType(e, t)
	case *ast.UnaryExpr, *ast.BinaryExpr:
		// A constraint's term (~T, A | B).
		return e
	}
	f.problem(e, "type expression %T", e)
	return e
}

// instanceType is a generic type's instantiation G[T...]: R5 (a node set),
// L1 (a comparable type parameter takes a node's key), else each argument
// translated.
func (f *fn) instanceType(e ast.Expr, t types.Type) ast.Expr {
	var gen ast.Expr
	var args []ast.Expr
	var lbrack, rbrack token.Pos
	switch x := e.(type) {
	case *ast.IndexExpr:
		gen, args, lbrack, rbrack = x.X, []ast.Expr{x.Index}, x.Lbrack, x.Rbrack
	case *ast.IndexListExpr:
		gen, args, lbrack, rbrack = x.X, x.Indices, x.Lbrack, x.Rbrack
	}
	var tparams *types.TypeParamList
	if named, ok := types.Unalias(t).(*types.Named); ok {
		if set := f.nodeSetType(named); set != nil {
			return f.typeExpr(set, e.Pos())
		}
		tparams = named.Origin().TypeParams()
	}
	var out []ast.Expr
	for i, a := range args {
		ta := f.typ(a)
		if at := f.info.TypeOf(a); at != nil && f.isNode(at) && tparams != nil && i < tparams.Len() && isComparableConstraint(tparams.At(i)) {
			ta = f.qualify(f.store, "NodeKey", a.Pos())
		}
		out = append(out, ta)
	}
	x := f.typ(gen)
	if len(out) == 1 {
		return &ast.IndexExpr{X: x, Lbrack: lbrack, Index: out[0], Rbrack: rbrack}
	}
	return &ast.IndexListExpr{X: x, Lbrack: lbrack, Indices: out, Rbrack: rbrack}
}

func (f *fn) funcType(e *ast.FuncType) *ast.FuncType {
	return &ast.FuncType{Func: e.Func, TypeParams: f.fields(e.TypeParams), Params: f.fields(e.Params), Results: f.fields(e.Results)}
}

func (f *fn) fields(l *ast.FieldList) *ast.FieldList {
	if l == nil {
		return nil
	}
	out := &ast.FieldList{Opening: l.Opening, Closing: l.Closing}
	for _, field := range l.List {
		typ := f.typ(field.Type)
		if f.listParams && len(field.Names) > 0 {
			if obj := f.info.Defs[field.Names[0]]; obj != nil && f.vars[obj] != nil && f.isStoreList(f.vars[obj]) {
				typ = f.qualify(f.store, "List", field.Type.Pos())
			}
		}
		if len(field.Names) > 0 {
			if v, ok := f.info.Defs[field.Names[0]].(*types.Var); ok && f.keyFields[v] {
				typ = f.typeExpr(f.keyType(v.Type()), field.Type.Pos())
			}
		}
		out.List = append(out.List, &ast.Field{Doc: field.Doc, Names: field.Names, Type: typ, Tag: field.Tag, Comment: field.Comment})
	}
	return out
}

// storeObj is the Store counterpart of a package-level Pointer object: the
// output package's (phase 2), package store's for an ast function, or the
// object itself when the Store shares it. nil in phase 1.
func (f *fn) storeObj(obj types.Object) types.Object {
	o := f.outOf(obj)
	if o == nil {
		return obj
	}
	if f.w == nil || !f.w.stubbed {
		return nil
	}
	if o.mode == modeAST {
		if fun, ok := obj.(*types.Func); ok && keptInAST(f.universe, fun) {
			return obj
		}
		if _, ok := obj.(*types.Func); !ok && (f.res == nil || !f.res.output[o.srcPath+"."+obj.Name()]) {
			return obj
		}
	} else if !f.provides(o, obj.Name()) {
		return obj
	}
	pkg := f.w.byPath[o.path]
	if pkg == nil || pkg.Types == nil {
		return nil
	}
	return pkg.Types.Scope().Lookup(obj.Name())
}

// storeTypeOf is the Store type of a Pointer expression that the rules do not
// change: its mapped type, or the recorded type of a local.
func (f *fn) storeTypeOf(e ast.Expr) types.Type {
	if id, ok := e.(*ast.Ident); ok {
		if obj := f.info.ObjectOf(id); obj != nil {
			if t, ok := f.vars[obj]; ok {
				return t
			}
		}
	}
	t := f.info.TypeOf(e)
	if t == nil {
		return nil
	}
	m, _ := f.mapType(t)
	return m
}

// ── Expressions ──

func ident(name string, pos token.Pos) *ast.Ident { return &ast.Ident{NamePos: pos, Name: name} }

func call(fun ast.Expr, pos token.Pos, args ...ast.Expr) *ast.CallExpr {
	return &ast.CallExpr{Fun: fun, Lparen: pos, Args: args, Rparen: pos}
}

func method(x ast.Expr, name string, pos token.Pos, args ...ast.Expr) *ast.CallExpr {
	return call(&ast.SelectorExpr{X: x, Sel: ident(name, pos)}, pos, args...)
}

// asNode makes a typed view a Node for an API that takes Node.
func (f *fn) asNode(e ast.Expr, st types.Type) ast.Expr {
	if st != nil && !f.isStoreNode(st) && f.isView(st) {
		return call(f.qualify(f.store, "Node", e.Pos()), e.Pos(), e)
	}
	return e
}

// zero is the Store form of an untyped nil that stands for a value of the
// Pointer type want.
func (f *fn) zero(want types.Type, pos token.Pos) (ast.Expr, types.Type) {
	if want != nil {
		m, _ := f.mapType(want)
		switch {
		case f.isNodeLike(want) || f.isList(want) || f.isFlow(want) || f.isFlowList(want):
			return &ast.CompositeLit{Type: f.typeExpr(m, pos), Lbrace: pos, Rbrace: pos}, m
		}
		return ident("nil", pos), m
	}
	return ident("nil", pos), nil
}

func isNil(e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && id.Name == "nil"
}

// expr translates e, where the context expects a value of the Pointer type
// want (nil when unknown). It returns the Store expression and its Store
// type.
func (f *fn) expr(e ast.Expr, want types.Type) (ast.Expr, types.Type) {
	switch e := e.(type) {
	case *ast.Ident:
		return f.identExpr(e, want)
	case *ast.BasicLit:
		if want != nil && f.isNodeIdType(want) && e.Value == "0" {
			// The zero ast.NodeId is the nil node's key.
			return &ast.CompositeLit{Type: f.qualify(f.store, "NodeKey", e.Pos()), Lbrace: e.Pos(), Rbrace: e.Pos()}, f.storeType("NodeKey")
		}
		return e, f.storeTypeOf(e)
	case *ast.ParenExpr:
		x, t := f.expr(e.X, want)
		return &ast.ParenExpr{Lparen: e.Lparen, X: x, Rparen: e.Rparen}, t
	case *ast.CompositeLit:
		return f.compositeLit(e)
	case *ast.FuncLit:
		return f.funcLit(e)
	case *ast.SelectorExpr:
		return f.selector(e)
	case *ast.CallExpr:
		return f.callExpr(e)
	case *ast.IndexExpr:
		return f.indexExpr(e)
	case *ast.IndexListExpr:
		x, t := f.expr(e.X, nil)
		var idx []ast.Expr
		for _, i := range e.Indices {
			idx = append(idx, f.typ(i))
		}
		return &ast.IndexListExpr{X: x, Lbrack: e.Lbrack, Indices: idx, Rbrack: e.Rbrack}, t
	case *ast.SliceExpr:
		x, t := f.expr(e.X, nil)
		if f.isStoreList(t) {
			x, t = f.nodesCopy(x, e.Pos(), "slice"), types.NewSlice(f.storeType("Node"))
		}
		lo, _ := f.exprOpt(e.Low, nil)
		hi, _ := f.exprOpt(e.High, nil)
		mx, _ := f.exprOpt(e.Max, nil)
		return &ast.SliceExpr{X: x, Lbrack: e.Lbrack, Low: lo, High: hi, Max: mx, Slice3: e.Slice3, Rbrack: e.Rbrack}, t
	case *ast.StarExpr:
		if f.info.Types[e].IsType() {
			return f.typ(e), nil
		}
		x, _ := f.expr(e.X, nil)
		if pt := f.info.TypeOf(e.X); f.isNodeLike(pt) || f.isList(pt) || f.isFlow(pt) || f.isFlowList(pt) || f.isSourceFile(pt) {
			f.problem(e, "dereference of a node pointer")
		}
		return &ast.StarExpr{Star: e.Star, X: x}, f.storeTypeOf(e)
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			// The address of a node struct (&data.Node, &data.Base), of a list
			// value (&modifiers.NodeList) or of a NodeFactory held by value
			// (&c.factory) is the Store value itself: a node, a list, a
			// *Factory (R7).
			if xt := f.info.TypeOf(e.X); f.isNodeStruct(xt) || f.isNodeFactory(xt) || f.isList(types.NewPointer(xt)) {
				return f.expr(e.X, nil)
			}
		}
		x, t := f.expr(e.X, nil)
		if e.Op == token.AND {
			if t != nil {
				t = types.NewPointer(t)
			}
		}
		return &ast.UnaryExpr{OpPos: e.OpPos, Op: e.Op, X: x}, f.unaryType(e, t)
	case *ast.BinaryExpr:
		return f.binaryExpr(e)
	case *ast.KeyValueExpr:
		k, _ := f.expr(e.Key, nil)
		v, _ := f.expr(e.Value, nil)
		return &ast.KeyValueExpr{Key: k, Colon: e.Colon, Value: v}, nil
	case *ast.TypeAssertExpr:
		x, _ := f.expr(e.X, nil)
		if f.containsNode(f.info.TypeOf(e.X)) {
			f.problem(e, "type assertion on a node")
		}
		return &ast.TypeAssertExpr{X: x, Lparen: e.Lparen, Type: f.typ(e.Type), Rparen: e.Rparen}, f.storeTypeOf(e)
	case *ast.ArrayType, *ast.MapType, *ast.FuncType, *ast.ChanType, *ast.InterfaceType, *ast.StructType, *ast.Ellipsis:
		return f.typ(e), nil
	}
	f.problem(e, "expression %T", e)
	return e, nil
}

func (f *fn) unaryType(e *ast.UnaryExpr, t types.Type) types.Type {
	if e.Op == token.AND {
		return t
	}
	return f.storeTypeOf(e)
}

func (f *fn) exprOpt(e ast.Expr, want types.Type) (ast.Expr, types.Type) {
	if e == nil {
		return nil, nil
	}
	return f.expr(e, want)
}

func (f *fn) identExpr(e *ast.Ident, want types.Type) (ast.Expr, types.Type) {
	obj := f.info.Uses[e]
	if obj == nil {
		obj = f.info.Defs[e]
	}
	switch obj.(type) {
	case *types.Nil:
		return f.zero(want, e.Pos())
	case nil:
		return e, nil
	case *types.Builtin:
		return e, nil
	case *types.TypeName:
		if f.info.Types[e].IsType() {
			return f.typ(e), nil
		}
	}
	if obj.Pkg() != nil && obj.Parent() == obj.Pkg().Scope() {
		return f.pkgLevel(obj, e.Pos())
	}
	return e, f.storeTypeOf(e)
}

// pkgLevel is a reference to a package-level object and its Store type.
func (f *fn) pkgLevel(obj types.Object, pos token.Pos) (ast.Expr, types.Type) {
	x := f.ref(obj, pos)
	if so := f.storeObj(obj); so != nil {
		return x, so.Type()
	}
	t, _ := f.mapType(obj.Type())
	return x, t
}

func (f *fn) compositeLit(e *ast.CompositeLit) (ast.Expr, types.Type) {
	t := f.info.TypeOf(e)
	if f.isNodeFactory(t) {
		// R7: a Store Factory needs a region; there is no zero one.
		f.problem(e, "a NodeFactory literal: the Store Factory has no zero value (P2)")
		return e, nil
	}
	out := &ast.CompositeLit{Lbrace: e.Lbrace, Rbrace: e.Rbrace, Incomplete: e.Incomplete}
	if e.Type != nil {
		out.Type = f.typ(e.Type)
	}
	under := types.Unalias(t)
	if p, ok := under.(*types.Pointer); ok {
		under = p.Elem()
	}
	var st *types.Struct
	var elem, key types.Type
	switch u := under.Underlying().(type) {
	case *types.Struct:
		st = u
	case *types.Slice:
		elem = u.Elem()
	case *types.Array:
		elem = u.Elem()
	case *types.Map:
		key, elem = u.Key(), u.Elem()
	}
	for i, el := range e.Elts {
		switch {
		case st != nil:
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				var field *types.Var
				if name, ok := kv.Key.(*ast.Ident); ok {
					for j := range st.NumFields() {
						if st.Field(j).Name() == name.Name {
							field = st.Field(j)
						}
					}
				}
				out.Elts = append(out.Elts, &ast.KeyValueExpr{Key: kv.Key, Colon: kv.Colon, Value: f.fieldValue(kv.Value, field)})
			} else {
				var field *types.Var
				if i < st.NumFields() {
					field = st.Field(i)
				}
				out.Elts = append(out.Elts, f.fieldValue(el, field))
			}
		default:
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				k, _ := f.expr(kv.Key, key)
				if key != nil && f.isNode(key) {
					k = method(k, "Key", kv.Key.Pos())
				}
				v, _ := f.expr(kv.Value, elem)
				out.Elts = append(out.Elts, &ast.KeyValueExpr{Key: k, Colon: kv.Colon, Value: v})
			} else {
				v, _ := f.expr(el, elem)
				out.Elts = append(out.Elts, v)
			}
		}
	}
	m, _ := f.mapType(t)
	return out, m
}

// fieldValue is the value of a struct field in a composite literal: a key
// for a key-only field.
func (f *fn) fieldValue(e ast.Expr, field *types.Var) ast.Expr {
	if field == nil {
		v, _ := f.expr(e, nil)
		return v
	}
	if f.keyFields[field] {
		return f.keyValue(e, field.Type())
	}
	v, st := f.expr(e, field.Type())
	if f.isNode(field.Type()) {
		v = f.asNode(v, st)
	}
	return v
}

func (f *fn) funcLit(e *ast.FuncLit) (ast.Expr, types.Type) {
	sig, _ := f.info.TypeOf(e).(*types.Signature)
	f.pushResults(sig)
	body := f.block(e.Body)
	f.results = f.results[:len(f.results)-1]
	m, _ := f.mapType(f.info.TypeOf(e))
	return &ast.FuncLit{Type: f.funcType(e.Type), Body: body}, m
}

func (f *fn) pushResults(sig *types.Signature) {
	var rs []types.Type
	if sig != nil {
		for i := range sig.Results().Len() {
			rs = append(rs, sig.Results().At(i).Type())
		}
	}
	f.results = append(f.results, rs)
}

// selector is a selector that is not the function of a call: a qualified
// name, a field, or a method value.
func (f *fn) selector(e *ast.SelectorExpr) (ast.Expr, types.Type) {
	if id, ok := e.X.(*ast.Ident); ok {
		if pkgName, ok := f.info.Uses[id].(*types.PkgName); ok {
			return f.qualified(e, pkgName.Imported())
		}
	}
	sel := f.info.Selections[e]
	if sel == nil {
		f.problem(e, "selector without a selection")
		return e, nil
	}
	if sel.Kind() != types.FieldVal {
		// A method value: a node's has no rule (its Store method may differ in
		// type); any other receiver's is kept.
		if pt := f.info.TypeOf(e.X); f.isNodeLike(pt) || f.isList(pt) || f.isSourceFile(pt) || f.isFlow(pt) {
			f.problem(e, "method value %s of a node", e.Sel.Name)
		}
		x, _ := f.expr(e.X, nil)
		m, _ := f.mapType(sel.Type())
		return &ast.SelectorExpr{X: x, Sel: e.Sel}, m
	}
	return f.field(e, sel)
}

// qualified is pkg.Name from the source.
func (f *fn) qualified(e *ast.SelectorExpr, pkg *types.Package) (ast.Expr, types.Type) {
	obj := f.info.Uses[e.Sel]
	if obj == nil {
		f.problem(e, "no object for %s", e.Sel.Name)
		return e, nil
	}
	if tn, ok := obj.(*types.TypeName); ok && f.info.Types[e].IsType() {
		_ = tn
		return f.typ(e), nil
	}
	return f.pkgLevel(obj, e.Pos())
}

// field is x.F: an accessor call in the Store for a node, a list or a flow.
func (f *fn) field(e *ast.SelectorExpr, sel *types.Selection) (ast.Expr, types.Type) {
	pt := f.info.TypeOf(e.X)
	name := e.Sel.Name
	pos := e.Sel.Pos()
	// x.FlowNodeData().FlowNode is x.FlowNode().
	if name == "FlowNode" {
		if c, ok := e.X.(*ast.CallExpr); ok {
			if s, ok := c.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "FlowNodeData" && len(c.Args) == 0 && f.isNodeLike(f.info.TypeOf(s.X)) {
				x, st := f.expr(s.X, nil)
				return method(f.asNode(x, st), "FlowNode", pos), f.storeType("Flow")
			}
		}
	}
	x, st := f.expr(e.X, nil)
	fv, _ := sel.Obj().(*types.Var)
	fieldType := sel.Obj().Type()
	nodeValue := f.isNodeStruct(pt) && (f.isStoreNode(st) || f.isView(st))
	switch {
	case f.isNode(pt) || nodeValue && f.pointerNamed(types.NewPointer(pt)) == "Node":
		switch name {
		case "Kind", "Flags", "Parent", "Loc":
			m := storeMethod(f.storeType("Node"), name)
			return method(f.asNode(x, st), name, pos), m.Type().(*types.Signature).Results().At(0).Type()
		}
		f.problem(e, "field %s of a node", name)
	case f.isData(pt) || f.isBasePtr(pt) || nodeValue:
		// An embedded node struct (&data.Node, data.NamedMemberBase) is the
		// node itself.
		if fv != nil && fv.Embedded() && f.isNodeStruct(fieldType) {
			return x, st
		}
		// A field of the embedded ast.Node: its header accessor.
		if fv != nil && isFieldOf(fv, f.ast.Scope().Lookup("Node").Type()) {
			m := storeMethod(f.storeType("Node"), name)
			if m != nil {
				return method(f.asNode(x, st), name, pos), m.Type().(*types.Signature).Results().At(0).Type()
			}
		}
		accessor := capitalize(name)
		if m := storeMethod(st, accessor); m != nil && m.Type().(*types.Signature).Params().Len() == 0 {
			return method(x, accessor, pos), resultOf(m)
		}
		if m := storeMethod(f.storeType("Node"), accessor); m != nil && m.Type().(*types.Signature).Params().Len() == 0 {
			return method(f.asNode(x, st), accessor, pos), resultOf(m)
		}
		f.problem(e, "field %s of %s has no Store accessor", name, types.TypeString(pt, nil))
	case f.isList(pt):
		switch {
		case name == "Nodes":
			return x, st
		case name == "Loc":
			return method(x, "Loc", pos), f.mustType(storeMethod(st, "Loc"))
		case fv != nil && fv.Embedded():
			// ModifierList.NodeList is the list.
			return x, st
		}
		f.problem(e, "field %s of a list", name)
	case f.isFlow(pt), f.isFlowList(pt):
		if m := storeMethod(st, name); m != nil {
			return method(x, name, pos), m.Type().(*types.Signature).Results().At(0).Type()
		}
		f.problem(e, "field %s of a flow", name)
	case f.isSymbol(pt):
		if sf := storeField(st, name); sf != nil {
			if want, _ := f.mapType(fieldType); !types.Identical(want, sf.Type()) {
				f.problem(e, "field %s of Symbol is %s in the Store, not %s", name, sf.Type(), want)
			}
			return &ast.SelectorExpr{X: x, Sel: e.Sel}, sf.Type()
		}
		f.problem(e, "field %s of Symbol has no Store field (Store type %v)", name, st)
	case f.isSourceFile(pt):
		return f.fileField(e, x, st, fieldType)
	case fv != nil && fv.Embedded() && f.isNodeFactory(fieldType):
		// R7: an embedded ast.NodeFactory is an embedded *store.Factory, whose
		// field name is Factory.
		return &ast.SelectorExpr{X: x, Sel: ident("Factory", pos)}, types.NewPointer(f.storeType("Factory"))
	}
	if sf := storeField(st, name); sf != nil && sf.Pkg() != f.src {
		// The field of the Store type (an output package's struct). A struct
		// of the source package itself (a fixture) is the Pointer's.
		return &ast.SelectorExpr{X: x, Sel: e.Sel}, sf.Type()
	}
	m, _ := f.mapType(fieldType)
	return &ast.SelectorExpr{X: x, Sel: e.Sel}, m
}

func isFieldOf(v *types.Var, t types.Type) bool {
	s, ok := t.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := range s.NumFields() {
		if s.Field(i) == v {
			return true
		}
	}
	return false
}

func (f *fn) mustType(m *types.Func) types.Type {
	if m == nil {
		return nil
	}
	return resultOf(m)
}

// fileField is a field of *ast.SourceFile read on a *store.File (R3): the
// Store field of the same name and type; a NodeRef field, resolved in the
// file's Store; a []NodeRef field, whose elements are resolved where they are
// read (refSlices); or the Store method of that name.
func (f *fn) fileField(e *ast.SelectorExpr, x ast.Expr, st types.Type, fieldType types.Type) (ast.Expr, types.Type) {
	name := e.Sel.Name
	want, _ := f.mapType(fieldType)
	if sf := storeField(st, name); sf != nil && sf.Exported() {
		out := &ast.SelectorExpr{X: x, Sel: e.Sel}
		switch {
		case types.Identical(want, sf.Type()):
			return out, sf.Type()
		case f.isNode(fieldType) && types.Identical(sf.Type(), f.storeType("NodeRef")):
			if !isPure(x) {
				f.problem(e, "the file of %s is not a name", name)
			}
			return method(&ast.SelectorExpr{X: x, Sel: ident("Store", e.Pos())}, "Node", e.Pos(), out), f.storeType("Node")
		case f.isNodeSlice(fieldType) && types.Identical(sf.Type(), types.NewSlice(f.storeType("NodeRef"))):
			if !isPure(x) {
				f.problem(e, "the file of %s is not a name", name)
			}
			f.refSlices[out] = &ast.SelectorExpr{X: x, Sel: ident("Store", e.Pos())}
			return out, sf.Type()
		}
		f.problem(e, "field %s of SourceFile is %s in the Store, not %s", name, sf.Type(), want)
		return out, sf.Type()
	}
	if m := storeMethod(st, name); m != nil && m.Type().(*types.Signature).Params().Len() == 0 {
		return method(x, name, e.Sel.Pos()), resultOf(m)
	}
	f.problem(e, "field %s of SourceFile has no Store field or method", name)
	return e, nil
}

// isPure is a name or a chain of field selections, which may be evaluated
// twice.
func isPure(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return isPure(e.X)
	case *ast.ParenExpr:
		return isPure(e.X)
	case *ast.CallExpr:
		return pureCalls[e]
	}
	return false
}

// pureCalls are the calls that a rule makes and knows to read without a side
// effect: n.File() of a pure n (GetSourceFileOfNode, AsSourceFile).
var pureCalls = map[*ast.CallExpr]bool{}

func (f *fn) fileOf(x ast.Expr, st types.Type, pos token.Pos) ast.Expr {
	c := method(f.asNode(x, st), "File", pos)
	if isPure(x) {
		pureCalls[c] = true
	}
	return c
}

func (f *fn) indexExpr(e *ast.IndexExpr) (ast.Expr, types.Type) {
	if tv, ok := f.info.Types[e]; ok && tv.IsType() {
		return f.typ(e), nil
	}
	// An instantiation of a generic function: f[T].
	if _, ok := f.info.TypeOf(e.X).(*types.Signature); ok {
		x, t := f.expr(e.X, nil)
		return &ast.IndexExpr{X: x, Lbrack: e.Lbrack, Index: f.typ(e.Index), Rbrack: e.Rbrack}, t
	}
	x, st := f.expr(e.X, nil)
	pt := f.info.TypeOf(e.X)
	if owner, ok := f.refSlices[x]; ok {
		i, _ := f.expr(e.Index, nil)
		return method(owner, "Node", e.Lbrack, &ast.IndexExpr{X: x, Lbrack: e.Lbrack, Index: i, Rbrack: e.Rbrack}), f.storeType("Node")
	}
	if f.isStoreList(st) {
		i, it := f.expr(e.Index, nil)
		if b, ok := types.Unalias(it).(*types.Basic); it != nil && (!ok || b.Kind() != types.Int && b.Kind() != types.UntypedInt) {
			// A slice takes any integer index; At takes an int.
			i = call(ident("int", e.Lbrack), e.Lbrack, i)
		}
		return method(x, "At", e.Lbrack, i), f.storeType("Node")
	}
	if m, ok := types.Unalias(pt).Underlying().(*types.Map); ok {
		k, _ := f.expr(e.Index, m.Key())
		if f.isNode(m.Key()) {
			k = method(k, "Key", e.Index.Pos())
		}
		t, _ := f.mapType(m.Elem())
		return &ast.IndexExpr{X: x, Lbrack: e.Lbrack, Index: k, Rbrack: e.Rbrack}, t
	}
	i, _ := f.expr(e.Index, nil)
	var t types.Type
	switch u := types.Unalias(st).(type) {
	case *types.Slice:
		t = u.Elem()
	default:
		t = f.storeTypeOf(e)
	}
	return &ast.IndexExpr{X: x, Lbrack: e.Lbrack, Index: i, Rbrack: e.Rbrack}, t
}

// intPos is int(x.Pos()) or int(x.End()): the Store reads int32 where the
// Pointer reads int. The conversion is remembered (posConv) so that a
// comparison of two of them, or of one with a constant, drops it.
func (f *fn) intPos(inner *ast.CallExpr) ast.Expr {
	conv := call(ident("int", inner.Pos()), inner.Pos(), inner)
	f.posConv[conv] = inner
	return conv
}

func (f *fn) binaryExpr(e *ast.BinaryExpr) (ast.Expr, types.Type) {
	lt, rt := f.info.TypeOf(e.X), f.info.TypeOf(e.Y)
	if e.Op == token.EQL || e.Op == token.NEQ {
		// x == nil
		for _, side := range [][2]ast.Expr{{e.X, e.Y}, {e.Y, e.X}} {
			value, other := side[0], side[1]
			if !isNil(other) {
				continue
			}
			vt := f.info.TypeOf(value)
			x, st := f.expr(value, nil)
			if f.isNodeLike(vt) || f.isList(vt) || f.isFlow(vt) || f.isFlowList(vt) || f.isStoreList(st) {
				test := method(f.asNode(x, st), "IsNil", e.OpPos)
				if f.isList(vt) || f.isStoreList(st) || f.isFlow(vt) || f.isFlowList(vt) {
					test = method(x, "IsNil", e.OpPos)
				}
				if e.Op == token.NEQ {
					return &ast.UnaryExpr{OpPos: e.X.Pos(), Op: token.NOT, X: test}, types.Typ[types.Bool]
				}
				return test, types.Typ[types.Bool]
			}
			n, _ := f.zero(vt, other.Pos())
			if value == e.X {
				return &ast.BinaryExpr{X: x, OpPos: e.OpPos, Op: e.Op, Y: n}, types.Typ[types.Bool]
			}
			return &ast.BinaryExpr{X: n, OpPos: e.OpPos, Op: e.Op, Y: x}, types.Typ[types.Bool]
		}
		if f.isNodeLike(lt) && f.isNodeLike(rt) {
			x, xt := f.expr(e.X, nil)
			y, yt := f.expr(e.Y, nil)
			same := call(f.storeFunc("SameNode", e.X.Pos()), e.OpPos, f.asNode(x, xt), f.asNode(y, yt))
			if e.Op == token.NEQ {
				return &ast.UnaryExpr{OpPos: e.X.Pos(), Op: token.NOT, X: same}, types.Typ[types.Bool]
			}
			return same, types.Typ[types.Bool]
		}
		if f.isList(lt) && f.isList(rt) {
			x, _ := f.expr(e.X, nil)
			y, _ := f.expr(e.Y, nil)
			same := call(f.storeFunc("SameList", e.X.Pos()), e.OpPos, x, y)
			if e.Op == token.NEQ {
				return &ast.UnaryExpr{OpPos: e.X.Pos(), Op: token.NOT, X: same}, types.Typ[types.Bool]
			}
			return same, types.Typ[types.Bool]
		}
	}
	x, _ := f.expr(e.X, lt)
	y, _ := f.expr(e.Y, rt)
	if isComparison(e.Op) {
		x, y = f.dropIntPos(x, y)
	}
	return &ast.BinaryExpr{X: x, OpPos: e.OpPos, Op: e.Op, Y: y}, f.storeTypeOf(e)
}

func isComparison(op token.Token) bool {
	switch op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return true
	}
	return false
}

// dropIntPos: int(a.Pos()) < int(b.End()) is a.Pos() < b.End(), and so is
// int(a.Pos()) == 0.
func (f *fn) dropIntPos(x, y ast.Expr) (ast.Expr, ast.Expr) {
	xc, _ := x.(*ast.CallExpr)
	yc, _ := y.(*ast.CallExpr)
	xi, xok := f.posConv[xc]
	yi, yok := f.posConv[yc]
	_, ylit := y.(*ast.BasicLit)
	_, xlit := x.(*ast.BasicLit)
	switch {
	case xok && yok:
		return xi, yi
	case xok && ylit:
		return xi, y
	case yok && xlit:
		return x, yi
	}
	return x, y
}

func (f *fn) isKeyField(sel *ast.SelectorExpr) bool {
	if s := f.info.Selections[sel]; s != nil {
		if field, ok := s.Obj().(*types.Var); ok {
			return f.keyFields[field]
		}
	}
	return false
}

// keptInAST: an exported ast function whose signature has no type that the
// translation changes (a predicate on a Kind or a string) is the same
// function for the Store; it is called in package ast, not translated. An
// unexported one cannot be called from package store and is translated, and
// so is every function of a file that goes to store whole.
func keptInAST(u *universe, fun *types.Func) bool {
	if fun.Pkg() != u.ast || !fun.Exported() || u.changes(fun.Type()) {
		return false
	}
	if u.w != nil {
		if o := u.w.outs[astPath]; o != nil {
			if d := u.w.decl(astPath, fun.Name()); d != nil && o.isFileLevel(d.fileName) {
				return false
			}
		}
	}
	return true
}

// ── Calls ──

// args translates the arguments of a call that has no List version.
func (f *fn) args(c *ast.CallExpr, sig *types.Signature) []ast.Expr {
	out, lists := f.argsL(c, sig, nil, nil)
	for _, i := range lists {
		f.problem(c.Args[i], "a list passed where the Pointer takes []*Node")
	}
	return out
}

func (f *fn) callExpr(c *ast.CallExpr) (ast.Expr, types.Type) {
	funType := f.info.Types[c.Fun]
	// A conversion.
	if funType.IsType() {
		t := f.info.TypeOf(c.Fun)
		var args []ast.Expr
		for _, a := range c.Args {
			x, _ := f.expr(a, t)
			// An integer conversion of an ast.NodeId: the NodeKey packed.
			if b, ok := t.Underlying().(*types.Basic); ok && b.Info()&types.IsInteger != 0 && f.isNodeIdType(f.info.TypeOf(a)) {
				x = method(x, "Uint64", a.Pos())
			}
			args = append(args, x)
		}
		m, _ := f.mapType(t)
		return &ast.CallExpr{Fun: f.typ(c.Fun), Lparen: c.Lparen, Args: args, Ellipsis: c.Ellipsis, Rparen: c.Rparen}, m
	}
	fun := ast.Unparen(c.Fun)
	// Builtins.
	if id, ok := fun.(*ast.Ident); ok {
		if b, ok := f.info.Uses[id].(*types.Builtin); ok {
			return f.builtin(c, b.Name())
		}
	}
	// A generic function's instantiation f[T](...) or a plain call.
	var callee *types.Func
	var calleeIdent *ast.Ident
	switch fx := fun.(type) {
	case *ast.Ident:
		callee, _ = f.info.Uses[fx].(*types.Func)
		calleeIdent = fx
	case *ast.SelectorExpr:
		if sel := f.info.Selections[fx]; sel != nil {
			if sel.Kind() == types.MethodVal {
				return f.methodCall(c, fx, sel)
			}
		} else {
			callee, _ = f.info.Uses[fx.Sel].(*types.Func)
			calleeIdent = fx.Sel
		}
	case *ast.IndexExpr:
		if id, ok := fx.X.(*ast.Ident); ok {
			callee, _ = f.info.Uses[id].(*types.Func)
			calleeIdent = id
		} else if s, ok := fx.X.(*ast.SelectorExpr); ok {
			callee, _ = f.info.Uses[s.Sel].(*types.Func)
			calleeIdent = s.Sel
		}
	case *ast.IndexListExpr:
		if id, ok := fx.X.(*ast.Ident); ok {
			callee, _ = f.info.Uses[id].(*types.Func)
			calleeIdent = id
		} else if s, ok := fx.X.(*ast.SelectorExpr); ok {
			callee, _ = f.info.Uses[s.Sel].(*types.Func)
			calleeIdent = s.Sel
		}
	}
	sig, _ := funType.Type.(*types.Signature)
	if callee != nil && callee.Pkg() != nil {
		if out, t, ok := f.specialCall(c, callee, calleeIdent, sig); ok {
			return out, t
		}
	}
	x, ft := f.expr(c.Fun, nil)
	storeSig, _ := ft.(*types.Signature)
	args, lists := f.argsL(c, sig, storeSig, calleeIdent)
	if len(lists) > 0 {
		if lx, lt := f.listVersion(callee, c.Fun.Pos()); lx != nil {
			x, ft = lx, lt
			if ix, ok := ast.Unparen(c.Fun).(*ast.IndexExpr); ok {
				// An explicit instantiation keeps its type arguments.
				x = &ast.IndexExpr{X: lx, Lbrack: ix.Lbrack, Index: f.typ(ix.Index), Rbrack: ix.Rbrack}
			}
		} else {
			for _, i := range lists {
				f.problem(c.Args[i], "a list passed where the Pointer takes []*Node (no List version of %s)", calleeName(callee))
			}
		}
	}
	return f.sameBasic(&ast.CallExpr{Fun: x, Lparen: c.Lparen, Args: args, Ellipsis: c.Ellipsis, Rparen: c.Rparen}, f.callResult(c, ft), c)
}

func calleeName(fn *types.Func) string {
	if fn == nil {
		return "a function value"
	}
	return fn.Name()
}

func (f *fn) builtin(c *ast.CallExpr, name string) (ast.Expr, types.Type) {
	switch name {
	case "len":
		x, st := f.expr(c.Args[0], nil)
		if f.isStoreList(st) {
			return method(x, "Len", c.Lparen), types.Typ[types.Int]
		}
		return call(c.Fun, c.Lparen, x), types.Typ[types.Int]
	case "cap":
		x, st := f.expr(c.Args[0], nil)
		if f.isStoreList(st) {
			f.problem(c, "cap of a list")
		}
		return call(c.Fun, c.Lparen, x), types.Typ[types.Int]
	case "make", "new":
		args := []ast.Expr{f.typ(c.Args[0])}
		for _, a := range c.Args[1:] {
			x, _ := f.expr(a, nil)
			args = append(args, x)
		}
		m, _ := f.mapType(f.info.TypeOf(c))
		return &ast.CallExpr{Fun: c.Fun, Lparen: c.Lparen, Args: args, Rparen: c.Rparen}, m
	case "append":
		sliceT := f.info.TypeOf(c.Args[0])
		s, st := f.expr(c.Args[0], sliceT)
		if f.isStoreList(st) {
			f.problem(c, "append to a list")
		}
		args := []ast.Expr{s}
		var elem types.Type
		if sl, ok := types.Unalias(sliceT).Underlying().(*types.Slice); ok {
			elem = sl.Elem()
		}
		for _, a := range c.Args[1:] {
			want := elem
			if c.Ellipsis.IsValid() {
				want = sliceT
			}
			x, xt := f.expr(a, want)
			if _, ok := f.refSlices[x]; ok {
				f.problem(a, "the node refs of a file appended (R3 resolves them in range and index only)")
			}
			if c.Ellipsis.IsValid() && f.isStoreList(xt) {
				x = f.nodesCopy(x, a.Pos(), "append")
			}
			if want != nil && (f.isNode(want) || f.isBasePtr(want)) {
				x = f.asNode(x, xt)
			}
			args = append(args, x)
		}
		return &ast.CallExpr{Fun: c.Fun, Lparen: c.Lparen, Args: args, Ellipsis: c.Ellipsis, Rparen: c.Rparen}, st
	case "delete":
		m, _ := f.expr(c.Args[0], nil)
		mt, _ := types.Unalias(f.info.TypeOf(c.Args[0])).Underlying().(*types.Map)
		var key types.Type
		if mt != nil {
			key = mt.Key()
		}
		k, _ := f.expr(c.Args[1], key)
		if key != nil && f.isNode(key) {
			k = method(k, "Key", c.Args[1].Pos())
		}
		return call(c.Fun, c.Lparen, m, k), nil
	}
	var args []ast.Expr
	for _, a := range c.Args {
		x, _ := f.expr(a, nil)
		args = append(args, x)
	}
	return &ast.CallExpr{Fun: c.Fun, Lparen: c.Lparen, Args: args, Ellipsis: c.Ellipsis, Rparen: c.Rparen}, f.storeTypeOf(c)
}

// nodeHelpers are the generic helpers of core whose comparison of a Node
// argument must be IsNil or SameNode; the value is the type parameter that
// decides (by index).
var nodeHelpers = map[string]int{
	"FirstNonNil":    1, // U: the result, compared with its zero value
	"MapNonNil":      1,
	"OrElse":         0,
	"SameMap":        0,
	"SameMapIndex":   0,
	"Deduplicate":    0,
	"AppendIfUnique": 0,
	"Coalesce":       0,
}

// specialCall is a call that a rule rewrites as a whole.
func (f *fn) specialCall(c *ast.CallExpr, callee *types.Func, id *ast.Ident, sig *types.Signature) (ast.Expr, types.Type, bool) {
	pkg := callee.Pkg().Path()
	name := callee.Name()
	switch {
	case pkg == astPath && name == "GetSourceFileOfNode":
		x, st := f.expr(c.Args[0], nil)
		return f.fileOf(x, st, c.Lparen), types.NewPointer(f.storeType("File")), true
	case pkg == astPath && name == "GetNodeId":
		x, st := f.expr(c.Args[0], nil)
		return method(f.asNode(x, st), "Key", c.Lparen), f.storeType("NodeKey"), true
	case pkg == corePath:
		i, ok := nodeHelpers[name]
		if !ok {
			return f.genericCheck(c, callee, id, sig)
		}
		inst := f.info.Instances[id]
		if inst.TypeArgs == nil || inst.TypeArgs.Len() <= i || !f.isNode(inst.TypeArgs.At(i)) {
			return f.genericCheck(c, callee, id, sig)
		}
		args := f.args(c, sig)
		out := &ast.CallExpr{Fun: f.storeFunc(name, c.Fun.Pos()), Lparen: c.Lparen, Args: args, Ellipsis: c.Ellipsis, Rparen: c.Rparen}
		m, _ := f.mapType(sig.Results().At(0).Type())
		return out, m, true
	case pkg == "slices" && (name == "Contains" || name == "Index" || name == "Compact"):
		inst := f.info.Instances[id]
		if inst.TypeArgs == nil || inst.TypeArgs.Len() < 2 || !f.isNode(inst.TypeArgs.At(1)) {
			return nil, nil, false
		}
		args, lists := f.argsL(c, sig, nil, nil)
		if len(lists) > 0 {
			name += "List"
		}
		out := &ast.CallExpr{Fun: f.storeFunc(name, c.Fun.Pos()), Lparen: c.Lparen, Args: args, Rparen: c.Rparen}
		m, _ := f.mapType(sig.Results().At(0).Type())
		return out, m, true
	}
	return f.genericCheck(c, callee, id, sig)
}

// genericCheck reports a comparable helper of core or slices instantiated
// with a Node type, which does not compile on the Store (Node is not
// comparable) and has no rule.
func (f *fn) genericCheck(c *ast.CallExpr, callee *types.Func, id *ast.Ident, sig *types.Signature) (ast.Expr, types.Type, bool) {
	if o := f.outOf(callee); o != nil && o.manual[callee.Name()] != nil {
		// A P2 version decides its own constraint (asRecursionId).
		return nil, nil, false
	}
	inst, ok := f.info.Instances[id]
	if !ok || inst.TypeArgs == nil {
		return nil, nil, false
	}
	generic, _ := callee.Origin().Type().(*types.Signature)
	if generic == nil || generic.TypeParams() == nil {
		return nil, nil, false
	}
	for i := range inst.TypeArgs.Len() {
		arg := inst.TypeArgs.At(i)
		if !f.containsNode(arg) || i >= generic.TypeParams().Len() {
			continue
		}
		constraint := generic.TypeParams().At(i).Constraint()
		mapped, _ := f.mapType(arg)
		if iface, ok := constraint.Underlying().(*types.Interface); ok && iface.IsComparable() && !types.Comparable(mapped) {
			f.problem(c, "%s.%s with a node type argument needs a comparable Node: no rule", callee.Pkg().Name(), callee.Name())
		}
	}
	return nil, nil, false
}

// methodCall is x.M(args).
func (f *fn) methodCall(c *ast.CallExpr, fx *ast.SelectorExpr, sel *types.Selection) (ast.Expr, types.Type) {
	pt := f.info.TypeOf(fx.X)
	name := fx.Sel.Name
	pos := fx.Sel.Pos()
	sig, _ := sel.Type().(*types.Signature)
	// x.Loc.Pos() and x.Loc.End() of a node or a list are x.Pos() and x.End(),
	// which is how (*ast.Node).Pos and End are defined; the Store reads the
	// header without making a TextRange.
	if loc, ok := fx.X.(*ast.SelectorExpr); ok && loc.Sel.Name == "Loc" && (name == "Pos" || name == "End") {
		if lt := f.info.TypeOf(loc.X); f.isNodeLike(lt) || f.isList(lt) {
			x, st := f.expr(loc.X, nil)
			return f.intPos(method(f.asNode(x, st), name, pos)), types.Typ[types.Int]
		}
	}
	var result types.Type
	if sig != nil && sig.Results().Len() == 1 {
		result = sig.Results().At(0).Type()
	}
	// flow.Node.M() of the data of a synthetic flow node is flow.M(): the
	// Store reads the data from the flow.
	if fn, ok := fx.X.(*ast.SelectorExpr); ok && fn.Sel.Name == "Node" && f.isFlow(f.info.TypeOf(fn.X)) && storeMethod(f.storeType("Node"), name) == nil {
		if m := storeMethod(f.storeType("Flow"), name); m != nil {
			x, _ := f.expr(fn.X, nil)
			return method(x, name, pos), resultOf(m)
		}
	}
	switch {
	case f.isNodeLike(pt):
		x, st := f.expr(fx.X, nil)
		switch name {
		case "AsNode":
			return f.asNode(x, st), f.storeType("Node")
		case "AsSourceFile":
			// The Pointer SourceFile is the file; the Store's is *File.
			return f.fileOf(x, st, pos), types.NewPointer(f.storeType("File"))
		case "Pos", "End":
			return f.intPos(method(f.asNode(x, st), name, pos)), types.Typ[types.Int]
		case "Clone":
			// R7: x.Clone(f) is f.Clone(x).
			if len(c.Args) == 1 {
				factory := f.factoryArg(c.Args[0])
				return method(factory, "Clone", pos, f.asNode(x, st)), f.storeType("Node")
			}
		}
		if m := storeMethod(st, name); m != nil {
			args := f.args(c, sig)
			return &ast.CallExpr{Fun: &ast.SelectorExpr{X: x, Sel: fx.Sel}, Lparen: c.Lparen, Args: args, Ellipsis: c.Ellipsis, Rparen: c.Rparen}, resultOf(m)
		}
		if m := storeMethod(f.storeType("Node"), name); m != nil {
			args := f.args(c, sig)
			return &ast.CallExpr{Fun: &ast.SelectorExpr{X: f.asNode(x, st), Sel: fx.Sel}, Lparen: c.Lparen, Args: args, Ellipsis: c.Ellipsis, Rparen: c.Rparen}, resultOf(m)
		}
		if len(c.Args) == 0 && result != nil && f.isBasePtr(result) {
			// x.AsXxx() of a kind without a view is the node; x.XxxData() is
			// the node where a field is read on it (x.XxxData().F, R2), and
			// has no Store form alone.
			if strings.HasPrefix(name, "As") || f.selBases[c] {
				return f.asNode(x, st), f.storeType("Node")
			}
			f.problem(c, "%s of a node is read alone, not as %s().F", name, name)
			return c, nil
		}
		f.problem(c, "method %s of a node has no Store version", name)
		return c, nil
	case f.isList(pt):
		x, st := f.expr(fx.X, nil)
		switch name {
		case "Pos", "End":
			return f.intPos(method(x, name, pos)), types.Typ[types.Int]
		case "Clone":
			// R7: l.Clone(f) is store.CloneNodeList(f, l).
			if len(c.Args) == 1 {
				factory := f.factoryArg(c.Args[0])
				return call(f.storeFunc("CloneNodeList", pos), pos, factory, x), f.storeType("List")
			}
		}
		if m := storeMethod(st, name); m != nil {
			args := f.args(c, sig)
			return &ast.CallExpr{Fun: &ast.SelectorExpr{X: x, Sel: fx.Sel}, Lparen: c.Lparen, Args: args, Rparen: c.Rparen}, resultOf(m)
		}
		f.problem(c, "method %s of a list has no Store version", name)
		return c, nil
	case f.isSourceFile(pt):
		x, st := f.expr(fx.X, nil)
		if name == "AsNode" {
			return method(x, "Root", pos), f.storeType("Node")
		}
		if m := storeMethod(st, name); m != nil {
			args := f.args(c, sig)
			out := &ast.CallExpr{Fun: &ast.SelectorExpr{X: x, Sel: fx.Sel}, Lparen: c.Lparen, Args: args, Ellipsis: c.Ellipsis, Rparen: c.Rparen}
			r := resultOf(m)
			if r != nil && types.Identical(r, types.NewSlice(f.storeType("NodeRef"))) {
				if !isPure(x) {
					f.problem(c, "the file of %s is not a name", name)
				}
				f.refSlices[out] = &ast.SelectorExpr{X: x, Sel: ident("Store", pos)}
			}
			return out, r
		}
		f.problem(c, "method %s of SourceFile has no Store version", name)
		return c, nil
	case f.isSymbol(pt), f.isFlow(pt), f.isFlowList(pt):
		x, st := f.expr(fx.X, nil)
		if m := storeMethod(st, name); m != nil {
			args := f.args(c, sig)
			return &ast.CallExpr{Fun: &ast.SelectorExpr{X: x, Sel: fx.Sel}, Lparen: c.Lparen, Args: args, Ellipsis: c.Ellipsis, Rparen: c.Rparen}, resultOf(m)
		}
		f.problem(c, "method %s of %s has no Store version", name, f.pointerNamed(pt))
		return c, nil
	case name == "AsNodeFactory" && isNodeFactoryMethod(f.universe, sel):
		// The NodeFactory itself, or the embedded one (printer.NodeFactory):
		// the Store's *Factory, or the embedded *Factory.
		x, _ := f.expr(fx.X, nil)
		if len(sel.Index()) > 1 {
			x = &ast.SelectorExpr{X: x, Sel: ident("Factory", pos)}
		}
		return x, types.NewPointer(f.storeType("Factory"))
	}
	x, st := f.expr(fx.X, nil)
	var storeSig *types.Signature
	m := storeMethod(st, name)
	if m != nil {
		storeSig, _ = m.Type().(*types.Signature)
	}
	args, lists := f.argsL(c, sig, storeSig, nil)
	fun := &ast.SelectorExpr{X: x, Sel: fx.Sel}
	if len(lists) > 0 {
		// R4: the method's List version.
		if lm := storeMethod(st, name+"List"); lm != nil {
			m = lm
			fun.Sel = ident(name+"List", fx.Sel.Pos())
		} else {
			for _, i := range lists {
				f.problem(c.Args[i], "a list passed where the Pointer takes []*Node (no List version of %s)", name)
			}
		}
	}
	var out types.Type
	if m != nil {
		out = f.callResult(c, m.Type())
	} else if result != nil {
		out, _ = f.mapType(result)
	} else if sig != nil && sig.Results().Len() > 1 {
		out, _ = f.mapType(sig.Results())
	}
	return &ast.CallExpr{Fun: fun, Lparen: c.Lparen, Args: args, Ellipsis: c.Ellipsis, Rparen: c.Rparen}, out
}

// factoryArg is the factory argument of Clone (ast.NodeFactoryCoercible): the
// Store's *Factory. &c.factory, a NodeFactory held by value, is c.factory (R7).
func (f *fn) factoryArg(a ast.Expr) ast.Expr {
	if u, ok := ast.Unparen(a).(*ast.UnaryExpr); ok && u.Op == token.AND && f.isNodeFactory(f.info.TypeOf(u.X)) {
		x, _ := f.expr(u.X, nil)
		return x
	}
	x, st := f.expr(a, nil)
	if f.isFactoryPtr(st) {
		return x
	}
	// A NodeFactoryCoercible that embeds the NodeFactory (printer.NodeFactory):
	// its AsNodeFactory is the embedded one, the Store's embedded *Factory.
	if fv := storeField(st, "Factory"); fv != nil && fv.Embedded() && f.isFactoryPtr(fv.Type()) {
		return &ast.SelectorExpr{X: x, Sel: ident("Factory", a.Pos())}
	}
	f.problem(a, "the factory argument is not a *store.Factory (%v)", st)
	return x
}

func resultOf(m *types.Func) types.Type {
	sig := m.Type().(*types.Signature)
	switch sig.Results().Len() {
	case 0:
		return nil
	case 1:
		return sig.Results().At(0).Type()
	}
	return sig.Results()
}

// nodesCopy is R4': a List that flows where the Pointer has a []*ast.Node
// variable, field, result or slice is copied with Nodes() (case A of the 7d-2
// design remainder 3.5). Each site is recorded for the report.
func (f *fn) nodesCopy(x ast.Expr, pos token.Pos, kind string) ast.Expr {
	f.r4Sites = append(f.r4Sites, fmt.Sprintf("%s %s", relPos(f.w.root, f.fset.Position(pos).String()), kind))
	return method(x, "Nodes", pos)
}

// ── Statements ──

func (f *fn) block(b *ast.BlockStmt) *ast.BlockStmt {
	if b == nil {
		return nil
	}
	return &ast.BlockStmt{Lbrace: b.Lbrace, List: f.stmts(b.List), Rbrace: b.Rbrace}
}

func (f *fn) stmts(list []ast.Stmt) []ast.Stmt {
	var out []ast.Stmt
	for _, s := range list {
		out = append(out, f.stmt(s)...)
	}
	return out
}

func (f *fn) stmt1(s ast.Stmt) ast.Stmt {
	if s == nil {
		return nil
	}
	out := f.stmt(s)
	if len(out) == 1 {
		return out[0]
	}
	return &ast.BlockStmt{Lbrace: s.Pos(), List: out, Rbrace: s.End()}
}

func (f *fn) stmt(s ast.Stmt) []ast.Stmt {
	switch s := s.(type) {
	case *ast.BlockStmt:
		return []ast.Stmt{f.block(s)}
	case *ast.ExprStmt:
		x, _ := f.expr(s.X, nil)
		return []ast.Stmt{&ast.ExprStmt{X: x}}
	case *ast.AssignStmt:
		if out, ok := f.write(s); ok {
			return out
		}
		return []ast.Stmt{f.assign(s)}
	case *ast.DeclStmt:
		return []ast.Stmt{&ast.DeclStmt{Decl: f.genDecl(s.Decl.(*ast.GenDecl))}}
	case *ast.ReturnStmt:
		out := &ast.ReturnStmt{Return: s.Return}
		var rs []types.Type
		if len(f.results) > 0 {
			rs = f.results[len(f.results)-1]
		}
		for i, r := range s.Results {
			var want types.Type
			if len(s.Results) == len(rs) {
				want = rs[i]
			}
			x, st := f.expr(r, want)
			if want != nil && f.isNodeSlice(want) && f.isStoreList(st) {
				x = f.nodesCopy(x, r.Pos(), "return")
			}
			if want != nil && (f.isNode(want) || f.isBasePtr(want)) {
				x = f.asNode(x, st)
			}
			out.Results = append(out.Results, x)
		}
		return []ast.Stmt{out}
	case *ast.IfStmt:
		init := f.stmt1(s.Init)
		cond, _ := f.expr(s.Cond, nil)
		return []ast.Stmt{&ast.IfStmt{If: s.If, Init: init, Cond: cond, Body: f.block(s.Body), Else: f.stmt1(s.Else)}}
	case *ast.ForStmt:
		init := f.stmt1(s.Init)
		cond, _ := f.exprOpt(s.Cond, nil)
		post := f.stmt1(s.Post)
		return []ast.Stmt{&ast.ForStmt{For: s.For, Init: init, Cond: (cond), Post: post, Body: f.block(s.Body)}}
	case *ast.RangeStmt:
		return f.rangeStmt(s)
	case *ast.SwitchStmt:
		if s.Tag != nil {
			if tt := f.info.TypeOf(s.Tag); f.isNodeLike(tt) || f.isList(tt) {
				return f.identitySwitch(s)
			}
		}
		init := f.stmt1(s.Init)
		tag, _ := f.exprOpt(s.Tag, nil)
		var tagT types.Type
		if s.Tag != nil {
			tagT = f.info.TypeOf(s.Tag)
			if f.containsNode(tagT) {
				f.problem(s.Tag, "switch on a node")
			}
		}
		body := &ast.BlockStmt{Lbrace: s.Body.Lbrace, Rbrace: s.Body.Rbrace}
		for _, cl := range s.Body.List {
			cc := cl.(*ast.CaseClause)
			out := &ast.CaseClause{Case: cc.Case, Colon: cc.Colon}
			for _, e := range cc.List {
				x, _ := f.expr(e, tagT)
				out.List = append(out.List, x)
			}
			out.Body = f.stmts(cc.Body)
			body.List = append(body.List, out)
		}
		return []ast.Stmt{&ast.SwitchStmt{Switch: s.Switch, Init: init, Tag: (tag), Body: body}}
	case *ast.TypeSwitchStmt:
		return []ast.Stmt{f.typeSwitch(s)}
	case *ast.IncDecStmt:
		x, _ := f.expr(s.X, nil)
		return []ast.Stmt{&ast.IncDecStmt{X: x, TokPos: s.TokPos, Tok: s.Tok}}
	case *ast.BranchStmt, *ast.EmptyStmt:
		return []ast.Stmt{s}
	case *ast.LabeledStmt:
		inner := f.stmt(s.Stmt)
		last := inner[len(inner)-1]
		return append(inner[:len(inner)-1], &ast.LabeledStmt{Label: s.Label, Colon: s.Colon, Stmt: last})
	case *ast.DeferStmt:
		x, _ := f.expr(s.Call, nil)
		c, ok := x.(*ast.CallExpr)
		if !ok {
			f.problem(s, "defer of a rewritten call")
			return []ast.Stmt{s}
		}
		return []ast.Stmt{&ast.DeferStmt{Defer: s.Defer, Call: c}}
	case *ast.GoStmt:
		x, _ := f.expr(s.Call, nil)
		c, ok := x.(*ast.CallExpr)
		if !ok {
			f.problem(s, "go of a rewritten call")
			return []ast.Stmt{s}
		}
		return []ast.Stmt{&ast.GoStmt{Go: s.Go, Call: c}}
	case *ast.SendStmt:
		ch, _ := f.expr(s.Chan, nil)
		v, _ := f.expr(s.Value, nil)
		return []ast.Stmt{&ast.SendStmt{Chan: ch, Arrow: s.Arrow, Value: v}}
	}
	f.problem(s, "statement %T", s)
	return []ast.Stmt{s}
}

// typeSwitch is a type switch on a value that is not a node (any, an
// interface of the translated packages): the case types translated, and the
// variable of each clause typed as its Store type.
func (f *fn) typeSwitch(s *ast.TypeSwitchStmt) ast.Stmt {
	init := f.stmt1(s.Init)
	var ta *ast.TypeAssertExpr
	var assign ast.Stmt
	switch a := s.Assign.(type) {
	case *ast.AssignStmt:
		ta = a.Rhs[0].(*ast.TypeAssertExpr)
		x, _ := f.expr(ta.X, nil)
		assign = &ast.AssignStmt{Lhs: a.Lhs, TokPos: a.TokPos, Tok: a.Tok, Rhs: []ast.Expr{&ast.TypeAssertExpr{X: x, Lparen: ta.Lparen, Rparen: ta.Rparen}}}
	case *ast.ExprStmt:
		ta = a.X.(*ast.TypeAssertExpr)
		x, _ := f.expr(ta.X, nil)
		assign = &ast.ExprStmt{X: &ast.TypeAssertExpr{X: x, Lparen: ta.Lparen, Rparen: ta.Rparen}}
	}
	if f.containsNode(f.info.TypeOf(ta.X)) {
		f.problem(s, "type switch on a node")
	}
	body := &ast.BlockStmt{Lbrace: s.Body.Lbrace, Rbrace: s.Body.Rbrace}
	for _, cl := range s.Body.List {
		cc := cl.(*ast.CaseClause)
		out := &ast.CaseClause{Case: cc.Case, Colon: cc.Colon}
		for _, e := range cc.List {
			if isNil(e) {
				out.List = append(out.List, e)
				continue
			}
			out.List = append(out.List, f.typ(e))
		}
		if obj := f.info.Implicits[cc]; obj != nil {
			if m, _ := f.mapType(obj.Type()); !types.Identical(m, obj.Type()) {
				f.vars[obj] = m
			}
		}
		out.Body = f.stmts(cc.Body)
		body.List = append(body.List, out)
	}
	return &ast.TypeSwitchStmt{Switch: s.Switch, Init: init, Assign: assign, Body: body}
}

// identitySwitch is a switch on a node or a list, which compares identities:
// a switch without a tag whose cases are SameNode or SameList. The tag is
// evaluated once, before the cases, as in the Pointer.
func (f *fn) identitySwitch(s *ast.SwitchStmt) []ast.Stmt {
	tt := f.info.TypeOf(s.Tag)
	same := "SameNode"
	if f.isList(tt) {
		same = "SameList"
	}
	init := f.stmt1(s.Init)
	tag, st := f.expr(s.Tag, nil)
	var pre []ast.Stmt
	if !isPure(tag) {
		if init != nil {
			f.problem(s, "a switch on a node with an init statement and a tag that is not a name")
		}
		name := f.fresh("tag")
		init = &ast.AssignStmt{Lhs: []ast.Expr{ident(name, s.Tag.Pos())}, TokPos: s.Tag.Pos(), Tok: token.DEFINE, Rhs: []ast.Expr{tag}}
		tag = ident(name, s.Tag.Pos())
	}
	if same == "SameNode" {
		tag = f.asNode(tag, st)
	}
	body := &ast.BlockStmt{Lbrace: s.Body.Lbrace, Rbrace: s.Body.Rbrace}
	for _, cl := range s.Body.List {
		cc := cl.(*ast.CaseClause)
		out := &ast.CaseClause{Case: cc.Case, Colon: cc.Colon}
		for _, e := range cc.List {
			x, xt := f.expr(e, tt)
			if same == "SameNode" {
				x = f.asNode(x, xt)
			}
			t := tag
			if id, ok := tag.(*ast.Ident); ok {
				// At the case, so that the call prints on one line.
				t = ident(id.Name, e.Pos())
			}
			out.List = append(out.List, call(f.storeFunc(same, e.Pos()), e.Pos(), t, x))
		}
		out.Body = f.stmts(cc.Body)
		body.List = append(body.List, out)
	}
	return append(pre, &ast.SwitchStmt{Switch: s.Switch, Init: init, Body: body})
}

func (f *fn) assign(s *ast.AssignStmt) ast.Stmt {
	out := &ast.AssignStmt{TokPos: s.TokPos, Tok: s.Tok}
	var rhsTypes []types.Type
	if len(s.Rhs) == 1 && len(s.Lhs) > 1 {
		x, t := f.expr(s.Rhs[0], nil)
		out.Rhs = []ast.Expr{x}
		if tuple, ok := t.(*types.Tuple); ok {
			for i := range tuple.Len() {
				rhsTypes = append(rhsTypes, tuple.At(i).Type())
			}
		}
	} else {
		for i, r := range s.Rhs {
			var want types.Type
			if s.Tok != token.DEFINE && i < len(s.Lhs) {
				want = f.info.TypeOf(s.Lhs[i])
				if sel, ok := s.Lhs[i].(*ast.SelectorExpr); ok && s.Tok == token.ASSIGN {
					if sv := f.info.Selections[sel]; sv != nil {
						if field, ok := sv.Obj().(*types.Var); ok && f.keyFields[field] {
							out.Rhs = append(out.Rhs, f.keyValue(r, field.Type()))
							rhsTypes = append(rhsTypes, f.keyType(field.Type()))
							continue
						}
					}
				}
			}
			x, t := f.expr(r, want)
			if want != nil && (f.isNode(want) || f.isBasePtr(want)) {
				x = f.asNode(x, t)
				t = f.storeType("Node")
			}
			out.Rhs = append(out.Rhs, x)
			rhsTypes = append(rhsTypes, t)
		}
	}
	for i, l := range s.Lhs {
		if s.Tok == token.DEFINE {
			id := l.(*ast.Ident)
			if obj := f.info.Defs[id]; obj != nil && i < len(rhsTypes) && rhsTypes[i] != nil {
				mapped, _ := f.mapType(obj.Type())
				if !types.Identical(mapped, rhsTypes[i]) {
					f.vars[obj] = rhsTypes[i]
				}
			}
			out.Lhs = append(out.Lhs, id)
			continue
		}
		x, lt := f.expr(l, nil)
		if sel, ok := l.(*ast.SelectorExpr); ok && f.isNodeLike(f.info.TypeOf(sel.X)) && !f.isKeyField(sel) {
			f.problem(l, "write to a field of a node")
		}
		if ix, ok := l.(*ast.IndexExpr); ok {
			if _, isList := x.(*ast.CallExpr); isList && f.isStoreList(f.storeTypeOf(ix.X)) {
				f.problem(l, "write to a list element")
			}
		}
		if i < len(rhsTypes) && f.isStoreList(rhsTypes[i]) && lt != nil && !f.isStoreList(lt) {
			if f.isStoreNodeSlice(lt) && len(out.Rhs) == len(s.Lhs) {
				out.Rhs[i] = f.nodesCopy(out.Rhs[i], s.Rhs[i].Pos(), "assign")
			} else {
				f.problem(l, "a list assigned to a %s", lt)
			}
		}
		out.Lhs = append(out.Lhs, x)
	}
	return out
}

func (f *fn) genDecl(d *ast.GenDecl) *ast.GenDecl {
	out := &ast.GenDecl{Doc: d.Doc, TokPos: d.TokPos, Tok: d.Tok, Lparen: d.Lparen, Rparen: d.Rparen}
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.ValueSpec:
			vs := &ast.ValueSpec{Doc: s.Doc, Names: s.Names, Type: f.typ(s.Type), Comment: s.Comment}
			for i, v := range s.Values {
				var want types.Type
				if i < len(s.Names) {
					if obj := f.info.Defs[s.Names[i]]; obj != nil {
						want = obj.Type()
					}
				}
				x, t := f.expr(v, want)
				if s.Type == nil && i < len(s.Names) {
					if obj := f.info.Defs[s.Names[i]]; obj != nil && t != nil {
						mapped, _ := f.mapType(obj.Type())
						if !types.Identical(mapped, t) {
							f.vars[obj] = t
						}
					}
				}
				if want != nil && (f.isNode(want) || f.isBasePtr(want)) {
					x = f.asNode(x, t)
				}
				vs.Values = append(vs.Values, x)
			}
			out.Specs = append(out.Specs, vs)
		case *ast.TypeSpec:
			out.Specs = append(out.Specs, &ast.TypeSpec{Doc: s.Doc, Name: s.Name, TypeParams: f.fields(s.TypeParams), Assign: s.Assign, Type: f.typ(s.Type), Comment: s.Comment})
		default:
			out.Specs = append(out.Specs, spec)
		}
	}
	return out
}

// rangeStmt: a range over a list becomes an index loop (the list of the
// rules): for i := range l.Len() { e := l.At(i); ... }. A list that is not a
// name is evaluated once before the loop.
func (f *fn) rangeStmt(s *ast.RangeStmt) []ast.Stmt {
	x, st := f.expr(s.X, nil)
	if m, ok := types.Unalias(f.info.TypeOf(s.X)).Underlying().(*types.Map); ok && f.isNode(m.Key()) && s.Key != nil && !isBlank(s.Key) {
		if sm, ok := types.Unalias(st).(*types.Map); ok && f.isNodeKey(sm.Key()) {
			// A map keyed by a node is keyed by its NodeKey, which does not
			// give the node back.
			f.problem(s, "the keys of a map keyed by a node are read back")
		}
	}
	if owner, ok := f.refSlices[x]; ok && s.Value != nil && !isBlank(s.Value) {
		// R3: a []NodeRef of a file; each element resolved in its Store.
		value, ok := s.Value.(*ast.Ident)
		if !ok || s.Tok != token.DEFINE {
			f.problem(s, "range over the refs of a file into existing variables")
			return []ast.Stmt{s}
		}
		if obj := f.info.Defs[value]; obj != nil {
			f.vars[obj] = f.storeType("Node")
		}
		ref := ident(f.fresh(value.Name+"Ref"), value.Pos())
		body := f.block(s.Body)
		elem := &ast.AssignStmt{Lhs: []ast.Expr{value}, TokPos: s.Body.Lbrace, Tok: token.DEFINE, Rhs: []ast.Expr{method(owner, "Node", s.Body.Lbrace, ref)}}
		body.List = append([]ast.Stmt{elem}, body.List...)
		return []ast.Stmt{&ast.RangeStmt{For: s.For, Key: s.Key, Value: ref, TokPos: s.TokPos, Tok: s.Tok, Range: s.Range, X: x, Body: body}}
	}
	if !f.isStoreList(st) {
		if s.Value != nil {
			if id, ok := s.Value.(*ast.Ident); ok && s.Tok == token.DEFINE {
				if obj := f.info.Defs[id]; obj != nil {
					if sl, ok := types.Unalias(st).(*types.Slice); ok {
						f.vars[obj] = sl.Elem()
					}
				}
			}
		}
		return []ast.Stmt{&ast.RangeStmt{For: s.For, Key: s.Key, Value: s.Value, TokPos: s.TokPos, Tok: s.Tok, Range: s.Range, X: x, Body: f.block(s.Body)}}
	}
	if s.Tok == token.ASSIGN {
		f.problem(s, "range over a list into existing variables")
	}
	var pre []ast.Stmt
	list := x
	if _, ok := ast.Unparen(x).(*ast.Ident); !ok {
		name := f.fresh(listName(s.X))
		pre = append(pre, &ast.AssignStmt{Lhs: []ast.Expr{ident(name, s.For)}, TokPos: s.For, Tok: token.DEFINE, Rhs: []ast.Expr{x}})
		list = ident(name, s.For)
	}
	key := s.Key
	if key == nil || isBlank(key) {
		if s.Value == nil || isBlank(s.Value) {
			key = nil
		} else {
			key = ident(f.fresh("i"), s.For)
		}
	}
	loop := &ast.RangeStmt{For: s.For, Key: key, TokPos: s.TokPos, Tok: token.DEFINE, Range: s.Range, X: method(list, "Len", s.Range)}
	if key == nil {
		loop.Tok = token.ILLEGAL
	}
	body := f.block(s.Body)
	if s.Value != nil && !isBlank(s.Value) {
		value := s.Value.(*ast.Ident)
		if obj := f.info.Defs[value]; obj != nil {
			f.vars[obj] = f.storeType("Node")
		}
		elem := &ast.AssignStmt{Lhs: []ast.Expr{value}, TokPos: s.Body.Lbrace, Tok: token.DEFINE, Rhs: []ast.Expr{method(list, "At", s.Body.Lbrace, key)}}
		body.List = append([]ast.Stmt{elem}, body.List...)
	}
	loop.Body = body
	return append(pre, loop)
}

func isBlank(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "_"
}

// listName names a hoisted list after its accessor: node.Arguments() is
// arguments, decl.ExportClause.Elements() is elements.
func listName(e ast.Expr) string {
	switch e := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		return listName(e.Fun)
	case *ast.SelectorExpr:
		if e.Sel.Name == "Nodes" {
			return listName(e.X)
		}
		return uncapitalize(e.Sel.Name)
	case *ast.Ident:
		return e.Name
	}
	return "list"
}

// ── Declarations ──

// funcDecl translates a function declaration. Its doc comment is kept.
func (f *fn) funcDecl(d *ast.FuncDecl) *ast.FuncDecl {
	f.decl = d
	if f.listParams {
		for _, field := range d.Type.Params.List {
			for _, name := range field.Names {
				if obj := f.info.Defs[name]; obj != nil && f.isNodeSlice(obj.Type()) {
					f.vars[obj] = f.storeType("List")
				}
			}
		}
	}
	sig, _ := f.info.Defs[d.Name].Type().(*types.Signature)
	f.pushResults(sig)
	out := &ast.FuncDecl{Doc: d.Doc, Recv: f.fields(d.Recv), Name: d.Name, Type: f.funcType(d.Type), Body: f.block(d.Body)}
	f.results = f.results[:len(f.results)-1]
	return out
}

// problemsText is the report of a function's problems.
func problemsText(name string, ps []problem) string {
	var b strings.Builder
	for _, p := range ps {
		fmt.Fprintf(&b, "  %s: %s: %s\n", p.pos, name, p.msg)
	}
	return b.String()
}

// isNodeFactoryMethod: the selected method is ast.NodeFactory's, maybe
// promoted through an embedded NodeFactory.
func isNodeFactoryMethod(u *universe, sel *types.Selection) bool {
	fn, ok := sel.Obj().(*types.Func)
	if !ok {
		return false
	}
	recv := fn.Type().(*types.Signature).Recv()
	return recv != nil && u.isNodeFactory(recv.Type())
}

// holdsNodes: a type whose declaration, looked into, mentions a node type of
// ast (binder.ReferenceResolver's methods take nodes). The Store cannot share
// it with the Pointer. Package ast's own types are the rules'.
func (f *fn) holdsNodes(obj types.Object) bool {
	tn, ok := obj.(*types.TypeName)
	if !ok || tn.Pkg() == f.ast || tn.Pkg() == f.store {
		return false
	}
	seen := map[*types.Named]bool{}
	var deep func(n *types.Named) bool
	deep = func(n *types.Named) bool {
		if seen[n] {
			return false
		}
		seen[n] = true
		return f.mentions(n.Underlying(), func(m *types.Named) bool {
			if m.Obj().Pkg() == f.ast || m.Obj().Pkg() == nil {
				return false
			}
			return deep(m.Origin())
		})
	}
	named, ok := types.Unalias(tn.Type()).(*types.Named)
	return ok && deep(named.Origin())
}

func (f *fn) isNodeIdType(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() == f.ast && n.Obj().Name() == "NodeId"
}
