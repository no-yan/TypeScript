package main

import (
	"go/ast"
	"go/token"
	"go/types"
)

// R8 (7d-2a instructions 3.3; 7d-2 design remainder 3.1): a write to a field
// of a node, or to the range or an element of a list, is a call of a writer
// of the Factory that owns the written Store: f.SetParent(x, y) for
// x.Parent = y, and so on (the writers of 7d-0, and SetIsTypeOf,
// SetTokenFlags and SetListRange). f is found by type: among the receiver and
// the parameters of the function that holds the write, the one variable whose
// struct has a field of type ast.NodeFactory or *ast.NodeFactory, embedded or
// not; the write goes through that field (c.factory, b.f). None, or more
// than one, is a problem.

// nodeWriters are the writers of R8, by the field of the Pointer node that
// the write assigns. The table is the rule table of the instructions (R8),
// and each writer is a method of store.Factory.
var nodeWriters = map[string]string{
	"Parent":     "SetParent",
	"Loc":        "SetRange",
	"Symbol":     "SetNodeSymbol",
	"FlowNode":   "SetNodeFlow",
	"Locals":     "SetLocals",
	"IsTypeOf":   "SetIsTypeOf",
	"TokenFlags": "SetTokenFlags",
}

// write translates an assignment whose single left side is a field of a node
// or a list, or an element of a list. ok is false for any other assignment.
func (f *fn) write(s *ast.AssignStmt) ([]ast.Stmt, bool) {
	out, ok := f.write1(s)
	if ok && len(f.problems) == 0 && f.w != nil {
		f.r8Sites = append(f.r8Sites, relPos(f.w.root, f.fset.Position(s.Pos()).String())+" "+types.ExprString(s.Lhs[0]))
	}
	return out, ok
}

func (f *fn) write1(s *ast.AssignStmt) ([]ast.Stmt, bool) {
	if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		return nil, false
	}
	pos := s.Pos()
	switch l := ast.Unparen(s.Lhs[0]).(type) {
	case *ast.SelectorExpr:
		xt := f.info.TypeOf(l.X)
		sel := f.info.Selections[l]
		if sel == nil || sel.Kind() != types.FieldVal {
			return nil, false
		}
		if f.isKeyField(l) {
			return nil, false
		}
		name := l.Sel.Name
		switch {
		case f.isNodeLike(xt) || f.isNodeStruct(xt):
		case f.isList(xt) && name == "Loc":
		default:
			return nil, false
		}
		factory := f.ownerFactory(s)
		if factory == nil {
			return []ast.Stmt{s}, true
		}
		// The node: for x.XxxData().F and x.AsXxx().F, x.
		node, nt := f.expr(l.X, nil)
		if f.isList(xt) {
			return f.rangeWrite(s, factory, "SetListRange", node, pos), true
		}
		node = f.asNode(node, nt)
		if name == "Flags" && s.Tok == token.OR_ASSIGN {
			v, _ := f.expr(s.Rhs[0], nil)
			return []ast.Stmt{&ast.ExprStmt{X: method(factory, "AddFlags", pos, node, v)}}, true
		}
		writer, ok := nodeWriters[name]
		if !ok {
			f.problem(s, "write to the field %s of a node: no writer", name)
			return []ast.Stmt{s}, true
		}
		if name == "Loc" {
			if s.Tok != token.ASSIGN {
				f.problem(s, "write to Loc with %s", s.Tok)
			}
			return f.rangeWrite(s, factory, writer, node, pos), true
		}
		var value ast.Expr
		switch s.Tok {
		case token.ASSIGN:
			v, vt := f.expr(s.Rhs[0], sel.Obj().Type())
			if f.isNode(sel.Obj().Type()) {
				v = f.asNode(v, vt)
			}
			value = v
		case token.XOR_ASSIGN, token.OR_ASSIGN, token.AND_ASSIGN, token.AND_NOT_ASSIGN:
			// x.F op= v is x.F = x.F op v, the read by the field's accessor.
			cur, _ := f.expr(l, nil)
			v, _ := f.expr(s.Rhs[0], nil)
			value = &ast.BinaryExpr{X: cur, OpPos: s.TokPos, Op: compoundOp(s.Tok), Y: v}
		default:
			f.problem(s, "write to the field %s of a node with %s", name, s.Tok)
			return []ast.Stmt{s}, true
		}
		return []ast.Stmt{&ast.ExprStmt{X: method(factory, writer, pos, node, value)}}, true
	case *ast.IndexExpr:
		// l.Nodes[i] = n
		sx, ok := ast.Unparen(l.X).(*ast.SelectorExpr)
		if !ok || sx.Sel.Name != "Nodes" || !f.isList(f.info.TypeOf(sx.X)) {
			return nil, false
		}
		if s.Tok != token.ASSIGN {
			f.problem(s, "write to a list element with %s", s.Tok)
			return []ast.Stmt{s}, true
		}
		factory := f.ownerFactory(s)
		if factory == nil {
			return []ast.Stmt{s}, true
		}
		list, _ := f.expr(sx.X, nil)
		i, _ := f.expr(l.Index, nil)
		v, vt := f.expr(s.Rhs[0], nil)
		return []ast.Stmt{&ast.ExprStmt{X: method(factory, "SetListElement", pos, list, i, f.asNode(v, vt))}}, true
	}
	return nil, false
}

func compoundOp(tok token.Token) token.Token {
	switch tok {
	case token.XOR_ASSIGN:
		return token.XOR
	case token.OR_ASSIGN:
		return token.OR
	case token.AND_ASSIGN:
		return token.AND
	case token.AND_NOT_ASSIGN:
		return token.AND_NOT
	}
	return token.ILLEGAL
}

// rangeWrite is x.Loc = r: writer(x, int32(r.Pos()), int32(r.End())), with r
// held in a variable unless it is a name; core.NewTextRange(a, b) gives a and
// b directly.
func (f *fn) rangeWrite(s *ast.AssignStmt, factory ast.Expr, writer string, node ast.Expr, pos token.Pos) []ast.Stmt {
	r := s.Rhs[0]
	i32 := func(e ast.Expr) ast.Expr { return call(ident("int32", pos), pos, e) }
	if c, ok := ast.Unparen(r).(*ast.CallExpr); ok && len(c.Args) == 2 {
		if fn, ok := f.info.Uses[calleeIdent(c.Fun)].(*types.Func); ok && fn.Pkg() != nil && fn.Pkg().Path() == corePath && fn.Name() == "NewTextRange" {
			a, _ := f.expr(c.Args[0], nil)
			b, _ := f.expr(c.Args[1], nil)
			return []ast.Stmt{&ast.ExprStmt{X: method(factory, writer, pos, node, i32(a), i32(b))}}
		}
	}
	rx, _ := f.expr(r, nil)
	var pre []ast.Stmt
	if !isPure(rx) {
		name := f.fresh("loc")
		pre = append(pre, &ast.AssignStmt{Lhs: []ast.Expr{ident(name, pos)}, TokPos: pos, Tok: token.DEFINE, Rhs: []ast.Expr{rx}})
		rx = ident(name, pos)
	}
	stmt := &ast.ExprStmt{X: method(factory, writer, pos, node, i32(method(rx, "Pos", pos)), i32(method(rx, "End", pos)))}
	if pre == nil {
		return []ast.Stmt{stmt}
	}
	return []ast.Stmt{&ast.BlockStmt{Lbrace: pos, List: append(pre, stmt), Rbrace: pos}}
}

// ownerFactory is f of R8: the NodeFactory field of the one receiver or
// parameter of the enclosing function whose struct has one, as a Store
// expression (x.field). nil, with a problem, when there is none or more than
// one.
func (f *fn) ownerFactory(at ast.Node) ast.Expr {
	if f.decl == nil {
		f.problem(at, "a write outside a function")
		return nil
	}
	var params []*ast.Field
	if f.decl.Recv != nil {
		params = append(params, f.decl.Recv.List...)
	}
	params = append(params, f.decl.Type.Params.List...)
	type cand struct {
		v     *ast.Ident
		field *types.Var
	}
	var cands []cand
	for _, p := range params {
		for _, name := range p.Names {
			v, ok := f.info.Defs[name].(*types.Var)
			if !ok {
				continue
			}
			for _, fv := range factoryFields(f.universe, v.Type()) {
				cands = append(cands, cand{name, fv})
			}
		}
	}
	switch len(cands) {
	case 0:
		f.problem(at, "a write to a node, and no receiver or parameter holds a NodeFactory")
		return nil
	case 1:
		name := cands[0].field.Name()
		if cands[0].field.Embedded() {
			name = "Factory" // the embedded *store.Factory
		}
		return &ast.SelectorExpr{X: ident(cands[0].v.Name, at.Pos()), Sel: ident(name, at.Pos())}
	}
	f.problem(at, "a write to a node, and %d receivers or parameters hold a NodeFactory", len(cands))
	return nil
}

// factoryFields are the fields of type ast.NodeFactory or *ast.NodeFactory of
// the struct that t is or points to.
func factoryFields(u *universe, t types.Type) []*types.Var {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		t = p.Elem()
	}
	s, ok := types.Unalias(t).Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	var out []*types.Var
	for i := range s.NumFields() {
		if u.isNodeFactory(s.Field(i).Type()) {
			out = append(out, s.Field(i))
		}
	}
	return out
}
