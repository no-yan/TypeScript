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
	name    string // package name
	inStore bool   // package store: its names are unqualified
	// funcs are the ast functions that exist in the output's view of package
	// store: translated, P2 or hand-written. A call to any other ast function
	// is a problem.
	funcs map[string]bool
	// local are the functions of the source package that the output
	// package has under the same name (the binder trial: binder.go's own).
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
	vars     map[types.Object]types.Type // the Store type of a local whose type the mapping does not give
	results  [][]types.Type              // the Pointer result types, innermost function last
	names    map[string]bool             // identifiers in the function, for fresh names
	posConv  map[*ast.CallExpr]*ast.CallExpr
	problems []problem
}

func (t *translator) newFn(decl ast.Node) *fn {
	f := &fn{translator: t, vars: map[types.Object]types.Type{}, names: map[string]bool{}, posConv: map[*ast.CallExpr]*ast.CallExpr{}}
	ast.Inspect(decl, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			f.names[id.Name] = true
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

func (f *fn) qualify(pkg *types.Package, name string, pos token.Pos) ast.Expr {
	switch {
	case pkg == nil:
		return &ast.Ident{NamePos: pos, Name: name}
	case pkg == f.store && f.out.inStore:
		return &ast.Ident{NamePos: pos, Name: name}
	case pkg == f.src && f.out.local:
		return &ast.Ident{NamePos: pos, Name: name}
	}
	local := f.importName(pkg)
	return &ast.SelectorExpr{X: &ast.Ident{NamePos: pos, Name: local}, Sel: &ast.Ident{NamePos: pos, Name: name}}
}

func (f *fn) importName(pkg *types.Package) string {
	if name, ok := f.out.imports[pkg.Path()]; ok {
		return name
	}
	f.out.imports[pkg.Path()] = pkg.Name()
	return pkg.Name()
}

// storeFunc is the Store name of an ast function: unqualified in package
// store, store.X elsewhere.
func (f *fn) storeFunc(name string, pos token.Pos) ast.Expr {
	return f.qualify(f.store, name, pos)
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

// typ translates a type expression: the source syntax re-qualified when the
// type does not change, else the Store type rendered.
func (f *fn) typ(e ast.Expr) ast.Expr {
	if e == nil {
		return nil
	}
	t := f.info.TypeOf(e)
	if t == nil {
		f.problem(e, "no type for type expression")
		return e
	}
	if !f.containsNode(t) {
		return f.requalify(e)
	}
	m, ok := f.mapType(t)
	if !ok {
		f.problem(e, "type %s has no Store form", t)
	}
	switch e := e.(type) {
	case *ast.FuncType:
		// Keep the parameter names and the layout.
		return f.funcType(e)
	case *ast.StructType:
		// Keep the field names, comments and tags.
		return &ast.StructType{Struct: e.Struct, Fields: f.fields(e.Fields)}
	case *ast.Ellipsis:
		elt := f.typ(e.Elt)
		return &ast.Ellipsis{Ellipsis: e.Ellipsis, Elt: elt}
	}
	return f.typeExpr(m, e.Pos())
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
		if len(field.Names) > 0 {
			if v, ok := f.info.Defs[field.Names[0]].(*types.Var); ok && f.keyFields[v] {
				typ = f.typeExpr(f.keyType(v.Type()), field.Type.Pos())
			}
		}
		out.List = append(out.List, &ast.Field{Doc: field.Doc, Names: field.Names, Type: typ, Tag: field.Tag, Comment: field.Comment})
	}
	return out
}

// requalify copies a type expression whose type is the same on both sides,
// qualifying the names of package ast when the source is package ast.
func (f *fn) requalify(e ast.Expr) ast.Expr {
	switch e := e.(type) {
	case *ast.Ident:
		obj := f.info.Uses[e]
		if obj != nil && obj.Pkg() == f.src && obj.Parent() == f.src.Scope() {
			return f.qualify(obj.Pkg(), e.Name, e.Pos())
		}
		return e
	case *ast.SelectorExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			if pkgName, ok := f.info.Uses[id].(*types.PkgName); ok {
				f.importName(pkgName.Imported())
			}
		}
		return e
	case *ast.StarExpr:
		return &ast.StarExpr{Star: e.Star, X: f.requalify(e.X)}
	case *ast.ArrayType:
		return &ast.ArrayType{Lbrack: e.Lbrack, Len: e.Len, Elt: f.requalify(e.Elt)}
	case *ast.MapType:
		return &ast.MapType{Map: e.Map, Key: f.requalify(e.Key), Value: f.requalify(e.Value)}
	case *ast.ChanType:
		return &ast.ChanType{Begin: e.Begin, Arrow: e.Arrow, Dir: e.Dir, Value: f.requalify(e.Value)}
	case *ast.FuncType:
		return f.funcType(e)
	case *ast.Ellipsis:
		return &ast.Ellipsis{Ellipsis: e.Ellipsis, Elt: f.requalify(e.Elt)}
	case *ast.IndexExpr:
		return &ast.IndexExpr{X: f.requalify(e.X), Lbrack: e.Lbrack, Index: f.requalify(e.Index), Rbrack: e.Rbrack}
	case *ast.IndexListExpr:
		var idx []ast.Expr
		for _, i := range e.Indices {
			idx = append(idx, f.requalify(i))
		}
		return &ast.IndexListExpr{X: f.requalify(e.X), Lbrack: e.Lbrack, Indices: idx, Rbrack: e.Rbrack}
	case *ast.ParenExpr:
		return &ast.ParenExpr{Lparen: e.Lparen, X: f.requalify(e.X), Rparen: e.Rparen}
	case *ast.StructType:
		return &ast.StructType{Struct: e.Struct, Fields: f.fields(e.Fields)}
	case *ast.InterfaceType:
		return e
	}
	return e
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
			f.problem(e, "slice of a list")
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
	if obj.Pkg() == f.src && obj.Parent() == f.src.Scope() {
		// A package-level name of the source package.
		if fun, ok := obj.(*types.Func); ok && f.src == f.ast {
			if keptInAST(f.universe, fun) {
				return f.qualify(obj.Pkg(), e.Name, e.Pos()), obj.Type()
			}
			if !f.out.funcs[fun.Name()] {
				f.problem(e, "ast.%s is not translated", fun.Name())
			}
			t, _ := f.mapType(obj.Type())
			return f.storeFunc(e.Name, e.Pos()), t
		}
		t, _ := f.mapType(obj.Type())
		return f.qualify(obj.Pkg(), e.Name, e.Pos()), t
	}
	return e, f.storeTypeOf(e)
}

func (f *fn) compositeLit(e *ast.CompositeLit) (ast.Expr, types.Type) {
	t := f.info.TypeOf(e)
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
	obj := pkg.Scope().Lookup(e.Sel.Name)
	if pkg == f.ast {
		if fun, ok := obj.(*types.Func); ok && !keptInAST(f.universe, fun) {
			if !f.out.funcs[fun.Name()] {
				f.problem(e, "ast.%s is not translated", fun.Name())
			}
			t, _ := f.mapType(obj.Type())
			return f.storeFunc(e.Sel.Name, e.Pos()), t
		}
		if tn, ok := obj.(*types.TypeName); ok && f.containsNode(tn.Type()) {
			return f.typ(e), nil
		}
	}
	f.importName(pkg)
	if obj == nil {
		return e, nil
	}
	t, _ := f.mapType(obj.Type())
	return e, t
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
	switch {
	case f.isNode(pt):
		switch name {
		case "Kind", "Flags", "Parent", "Loc":
			m := storeMethod(f.storeType("Node"), name)
			return method(x, name, pos), m.Type().(*types.Signature).Results().At(0).Type()
		}
		f.problem(e, "field %s of a node", name)
	case f.isData(pt):
		accessor := capitalize(name)
		if m := storeMethod(st, accessor); m != nil && m.Type().(*types.Signature).Params().Len() == 0 {
			return method(x, accessor, pos), m.Type().(*types.Signature).Results().At(0).Type()
		}
		if m := storeMethod(f.storeType("Node"), accessor); m != nil && m.Type().(*types.Signature).Params().Len() == 0 {
			return method(f.asNode(x, st), accessor, pos), m.Type().(*types.Signature).Results().At(0).Type()
		}
		f.problem(e, "field %s of %s has no Store accessor", name, f.pointerNamed(pt))
	case f.isList(pt):
		if name == "Nodes" {
			return x, st
		}
		f.problem(e, "field %s of a list", name)
	case f.isFlow(pt), f.isFlowList(pt):
		if m := storeMethod(st, name); m != nil {
			return method(x, name, pos), m.Type().(*types.Signature).Results().At(0).Type()
		}
		f.problem(e, "field %s of a flow", name)
	case f.isSymbol(pt), f.isSourceFile(pt):
		if fv := storeField(st, name); fv != nil {
			if want, _ := f.mapType(sel.Obj().Type()); !types.Identical(want, fv.Type()) {
				f.problem(e, "field %s of %s is %s in the Store, not %s", name, f.pointerNamed(pt), fv.Type(), want)
			}
			return &ast.SelectorExpr{X: x, Sel: e.Sel}, fv.Type()
		}
		f.problem(e, "field %s of %s has no Store field", name, f.pointerNamed(pt))
	default:
		if f.containsNode(pt) && f.src != f.ast {
			// A struct of the source package with node fields keeps its
			// field names; its field types are translated with it.
			m, _ := f.mapType(sel.Obj().Type())
			return &ast.SelectorExpr{X: x, Sel: e.Sel}, m
		}
	}
	m, _ := f.mapType(sel.Obj().Type())
	return &ast.SelectorExpr{X: x, Sel: e.Sel}, m
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
	if f.isStoreList(st) {
		i, _ := f.expr(e.Index, nil)
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

// keptInAST: an exported ast function whose signature has no node type (a
// predicate on a Kind or a string) is the same function for the Store; it is
// called in package ast, not translated. An unexported one cannot be called
// from package store and is translated.
func keptInAST(u *universe, fun *types.Func) bool {
	return fun.Pkg() == u.ast && fun.Exported() && !u.containsNode(fun.Type())
}

// ── Calls ──

func (f *fn) args(c *ast.CallExpr, sig *types.Signature) []ast.Expr {
	var out []ast.Expr
	for i, a := range c.Args {
		var want types.Type
		if sig != nil {
			switch {
			case sig.Variadic() && i >= sig.Params().Len()-1:
				last := sig.Params().At(sig.Params().Len() - 1).Type()
				if c.Ellipsis.IsValid() {
					want = last
				} else if s, ok := last.(*types.Slice); ok {
					want = s.Elem()
				}
			case i < sig.Params().Len():
				want = sig.Params().At(i).Type()
			}
		}
		x, st := f.expr(a, want)
		if want != nil && f.isNodeSlice(want) && f.isStoreList(st) {
			f.problem(a, "a list passed where the Pointer takes []*Node")
		}
		if want != nil && f.isNode(want) {
			x = f.asNode(x, st)
		}
		out = append(out, x)
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
	args := f.args(c, sig)
	var result types.Type
	if s, ok := ft.(*types.Signature); ok && s.Results().Len() == 1 {
		result = s.Results().At(0).Type()
	} else if s, ok := ft.(*types.Signature); ok && s.Results().Len() > 1 {
		result = s.Results()
	} else {
		result = f.storeTypeOf(c)
	}
	return &ast.CallExpr{Fun: x, Lparen: c.Lparen, Args: args, Ellipsis: c.Ellipsis, Rparen: c.Rparen}, result
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
			if c.Ellipsis.IsValid() && f.isStoreList(xt) {
				f.problem(a, "a list appended with ...")
			}
			if want != nil && f.isNode(want) {
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
}

// specialCall is a call that a rule rewrites as a whole.
func (f *fn) specialCall(c *ast.CallExpr, callee *types.Func, id *ast.Ident, sig *types.Signature) (ast.Expr, types.Type, bool) {
	pkg := callee.Pkg().Path()
	name := callee.Name()
	switch {
	case pkg == astPath && name == "GetSourceFileOfNode":
		x, st := f.expr(c.Args[0], nil)
		return method(f.asNode(x, st), "File", c.Lparen), types.NewPointer(f.storeType("File")), true
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
	case pkg == "slices" && (name == "Contains" || name == "Index"):
		inst := f.info.Instances[id]
		if inst.TypeArgs == nil || inst.TypeArgs.Len() < 2 || !f.isNode(inst.TypeArgs.At(1)) {
			return nil, nil, false
		}
		args := f.args(c, sig)
		out := &ast.CallExpr{Fun: f.storeFunc(name, c.Fun.Pos()), Lparen: c.Lparen, Args: args, Rparen: c.Rparen}
		if name == "Contains" {
			return out, types.Typ[types.Bool], true
		}
		return out, types.Typ[types.Int], true
	}
	return f.genericCheck(c, callee, id, sig)
}

// genericCheck reports a comparable helper of core or slices instantiated
// with a Node type, which does not compile on the Store (Node is not
// comparable) and has no rule.
func (f *fn) genericCheck(c *ast.CallExpr, callee *types.Func, id *ast.Ident, sig *types.Signature) (ast.Expr, types.Type, bool) {
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
	// x.Loc.Pos() and x.Loc.End() of a node are x.Pos() and x.End(), which is
	// how (*ast.Node).Pos and End are defined; the Store reads the header
	// without making a TextRange.
	if loc, ok := fx.X.(*ast.SelectorExpr); ok && loc.Sel.Name == "Loc" && (name == "Pos" || name == "End") && f.isNode(f.info.TypeOf(loc.X)) {
		x, _ := f.expr(loc.X, nil)
		return f.intPos(method(x, name, pos)), types.Typ[types.Int]
	}
	switch {
	case f.isNode(pt) || f.isData(pt):
		x, st := f.expr(fx.X, nil)
		switch name {
		case "AsNode":
			return f.asNode(x, st), f.storeType("Node")
		case "AsSourceFile":
			// The Pointer SourceFile is the file; the Store's is *File.
			return method(f.asNode(x, st), "File", pos), types.NewPointer(f.storeType("File"))
		case "FlowNodeData":
			f.problem(c, "FlowNodeData without .FlowNode")
		case "Pos", "End":
			return f.intPos(method(x, name, pos)), types.Typ[types.Int]
		}
		if m := storeMethod(st, name); m != nil {
			args := f.args(c, sig)
			return &ast.CallExpr{Fun: &ast.SelectorExpr{X: x, Sel: fx.Sel}, Lparen: c.Lparen, Args: args, Ellipsis: c.Ellipsis, Rparen: c.Rparen}, resultOf(m)
		}
		if m := storeMethod(f.storeType("Node"), name); m != nil {
			args := f.args(c, sig)
			return &ast.CallExpr{Fun: &ast.SelectorExpr{X: f.asNode(x, st), Sel: fx.Sel}, Lparen: c.Lparen, Args: args, Ellipsis: c.Ellipsis, Rparen: c.Rparen}, resultOf(m)
		}
		f.problem(c, "method %s of a node has no Store version", name)
		return c, nil
	case f.isList(pt):
		x, st := f.expr(fx.X, nil)
		switch name {
		case "Pos", "End":
			return f.intPos(method(x, name, pos)), types.Typ[types.Int]
		}
		if m := storeMethod(st, name); m != nil {
			args := f.args(c, sig)
			return &ast.CallExpr{Fun: &ast.SelectorExpr{X: x, Sel: fx.Sel}, Lparen: c.Lparen, Args: args, Rparen: c.Rparen}, resultOf(m)
		}
		f.problem(c, "method %s of a list has no Store version", name)
		return c, nil
	case f.isSymbol(pt), f.isSourceFile(pt), f.isFlow(pt), f.isFlowList(pt):
		x, st := f.expr(fx.X, nil)
		if m := storeMethod(st, name); m != nil {
			args := f.args(c, sig)
			return &ast.CallExpr{Fun: &ast.SelectorExpr{X: x, Sel: fx.Sel}, Lparen: c.Lparen, Args: args, Ellipsis: c.Ellipsis, Rparen: c.Rparen}, resultOf(m)
		}
		f.problem(c, "method %s of %s has no Store version", name, f.pointerNamed(pt))
		return c, nil
	}
	x, _ := f.expr(fx.X, nil)
	args := f.args(c, sig)
	var result types.Type
	if sig != nil && sig.Results().Len() == 1 {
		result, _ = f.mapType(sig.Results().At(0).Type())
	}
	return &ast.CallExpr{Fun: &ast.SelectorExpr{X: x, Sel: fx.Sel}, Lparen: c.Lparen, Args: args, Ellipsis: c.Ellipsis, Rparen: c.Rparen}, result
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
				f.problem(r, "a list returned where the Pointer returns []*Node")
			}
			if want != nil && f.isNode(want) {
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
		f.problem(s, "type switch")
		return []ast.Stmt{s}
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
			if want != nil && f.isNode(want) {
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
			f.problem(l, "a list assigned to a %s", lt)
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
				if want != nil && f.isNode(want) {
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
