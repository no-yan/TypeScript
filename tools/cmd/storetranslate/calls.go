package main

import (
	"go/ast"
	"go/token"
	"go/types"
)

// The arguments of a call, against the Store callee (7d-2a instructions 3.3):
//
//   - R4: a list passed where the Pointer callee takes []*ast.Node calls the
//     callee's List version, <F>List, found by name in the Store: package
//     store's for a helper of core and slices, the output package's for a
//     function or method of a translated package (P2, 3.4). A callee without
//     one is a problem.
//   - L1: a node passed where the Store callee takes a NodeKey is its Key().
//   - R11: a function literal passed to the callee takes its parameter types
//     from the Store callee's signature, instantiated from the Store types of
//     the other arguments, not from the Pointer literal: a callback on the
//     parse diagnostics of a store.File takes *ast.Diagnostic.

// argsL translates the arguments. sig is the Pointer signature; storeSig the
// Store callee's, when known (generic or instantiated). lists are the
// indices of the lists passed where the Pointer takes []*ast.Node.
func (f *fn) argsL(c *ast.CallExpr, sig *types.Signature, storeSig *types.Signature, id *ast.Ident) (out []ast.Expr, lists []int) {
	out = make([]ast.Expr, len(c.Args))
	sts := make([]types.Type, len(c.Args))
	want := func(sg *types.Signature, i int) types.Type {
		if sg == nil {
			return nil
		}
		switch {
		case sg.Variadic() && i >= sg.Params().Len()-1:
			last := sg.Params().At(sg.Params().Len() - 1).Type()
			if c.Ellipsis.IsValid() {
				return last
			} else if s, ok := last.(*types.Slice); ok {
				return s.Elem()
			}
		case i < sg.Params().Len():
			return sg.Params().At(i).Type()
		}
		return nil
	}
	var lits []int
	for i, a := range c.Args {
		if _, ok := ast.Unparen(a).(*ast.FuncLit); ok && storeSig != nil {
			lits = append(lits, i)
			continue
		}
		w := want(sig, i)
		x, st := f.expr(a, w)
		if _, ok := f.refSlices[x]; ok {
			f.problem(a, "the node refs of a file passed to a function (R3 resolves them in range and index only)")
		}
		if w != nil && f.isNodeSlice(w) && f.isStoreList(st) {
			lists = append(lists, i)
		}
		if w != nil && (f.isNode(w) || f.isBasePtr(w)) {
			x = f.asNode(x, st)
			st = f.storeType("Node")
		} else if sw := want(storeSig, i); sw != nil && f.isStoreNode(sw) && f.isView(st) {
			x = f.asNode(x, st)
			st = sw
		}
		if sw := want(storeSig, i); sw != nil && f.isNodeKey(sw) && (f.isStoreNode(st) || f.isView(st)) {
			x = method(f.asNode(x, st), "Key", a.Pos())
			st = sw
		}
		out[i], sts[i] = x, st
	}
	if len(lits) > 0 {
		inst := f.instantiate(storeSig, c, sts, id)
		for _, i := range lits {
			lit := ast.Unparen(c.Args[i]).(*ast.FuncLit)
			var ws *types.Signature
			if w := want(inst, i); w != nil {
				ws, _ = types.Unalias(w).Underlying().(*types.Signature)
			}
			x, _ := f.funcLitAs(lit, ws)
			out[i] = x
		}
	}
	return out, lists
}

// instantiate is the Store signature of a generic callee, its type parameters
// bound by the Store types of the arguments, and those that no argument binds
// by the Pointer instance's type arguments mapped. Nil if a parameter stays
// unbound.
func (f *fn) instantiate(sig *types.Signature, c *ast.CallExpr, sts []types.Type, id *ast.Ident) *types.Signature {
	if sig == nil || sig.TypeParams().Len() == 0 {
		return sig
	}
	tparams := sig.TypeParams()
	bound := map[*types.TypeParam]types.Type{}
	for i := range sts {
		if sts[i] == nil {
			continue
		}
		var p types.Type
		switch {
		case sig.Variadic() && i >= sig.Params().Len()-1:
			p = sig.Params().At(sig.Params().Len() - 1).Type()
			if !c.Ellipsis.IsValid() {
				if s, ok := p.(*types.Slice); ok {
					p = s.Elem()
				}
			}
		case i < sig.Params().Len():
			p = sig.Params().At(i).Type()
		}
		unify(p, sts[i], bound)
	}
	var pointerArgs *types.TypeList
	if id != nil {
		pointerArgs = f.info.Instances[id].TypeArgs
	}
	targs := make([]types.Type, tparams.Len())
	for i := range tparams.Len() {
		if t, ok := bound[tparams.At(i)]; ok {
			targs[i] = t
			continue
		}
		if pointerArgs == nil || pointerArgs.Len() != tparams.Len() {
			return nil
		}
		targs[i], _ = f.mapType(pointerArgs.At(i))
	}
	inst, err := types.Instantiate(nil, sig, targs, false)
	if err != nil {
		return nil
	}
	s, _ := inst.(*types.Signature)
	return s
}

// unify binds the type parameters of p to the parts of a.
func unify(p, a types.Type, bound map[*types.TypeParam]types.Type) {
	if p == nil || a == nil {
		return
	}
	switch p := p.(type) {
	case *types.TypeParam:
		if _, ok := bound[p]; !ok {
			bound[p] = a
		}
	case *types.Pointer:
		if x, ok := types.Unalias(a).Underlying().(*types.Pointer); ok {
			unify(p.Elem(), x.Elem(), bound)
		}
	case *types.Slice:
		if x, ok := types.Unalias(a).Underlying().(*types.Slice); ok {
			unify(p.Elem(), x.Elem(), bound)
		}
	case *types.Map:
		if x, ok := types.Unalias(a).Underlying().(*types.Map); ok {
			unify(p.Key(), x.Key(), bound)
			unify(p.Elem(), x.Elem(), bound)
		}
	case *types.Signature:
		if x, ok := types.Unalias(a).Underlying().(*types.Signature); ok && x.Params().Len() == p.Params().Len() {
			for i := range p.Params().Len() {
				unify(p.Params().At(i).Type(), x.Params().At(i).Type(), bound)
			}
			if x.Results().Len() == p.Results().Len() {
				for i := range p.Results().Len() {
					unify(p.Results().At(i).Type(), x.Results().At(i).Type(), bound)
				}
			}
		}
	case *types.Named:
		if x, ok := types.Unalias(a).(*types.Named); ok && x.Origin() == p.Origin() && p.TypeArgs() != nil && x.TypeArgs() != nil {
			for i := range p.TypeArgs().Len() {
				unify(p.TypeArgs().At(i), x.TypeArgs().At(i), bound)
			}
		}
	}
}

// funcLitAs translates a function literal whose parameters take the types of
// the Store signature want where they differ from the mapped Pointer types
// (R11).
func (f *fn) funcLitAs(e *ast.FuncLit, want *types.Signature) (ast.Expr, types.Type) {
	sig, _ := f.info.TypeOf(e).(*types.Signature)
	if want != nil && sig != nil && want.Params().Len() == sig.Params().Len() && e.Type.Params != nil {
		ft := f.funcType(e.Type)
		i := 0
		for fi, field := range e.Type.Params.List {
			n := len(field.Names)
			if n == 0 {
				n = 1
			}
			pt, _ := f.mapType(sig.Params().At(i).Type())
			wt := want.Params().At(i).Type()
			if !types.Identical(pt, wt) && !hasTypeParam(wt) {
				ft.Params.List[fi].Type = f.typeExpr(wt, field.Type.Pos())
				for _, name := range field.Names {
					if obj := f.info.Defs[name]; obj != nil {
						f.vars[obj] = wt
					}
				}
			}
			i += n
		}
		f.pushResults(sig)
		body := f.block(e.Body)
		f.results = f.results[:len(f.results)-1]
		return &ast.FuncLit{Type: ft, Body: body}, want
	}
	return f.funcLit(e)
}

func hasTypeParam(t types.Type) bool {
	found := false
	var visit func(t types.Type)
	visit = func(t types.Type) {
		if found || t == nil {
			return
		}
		switch t := t.(type) {
		case *types.TypeParam:
			found = true
		case *types.Pointer:
			visit(t.Elem())
		case *types.Slice:
			visit(t.Elem())
		case *types.Map:
			visit(t.Key())
			visit(t.Elem())
		case *types.Signature:
			for i := range t.Params().Len() {
				visit(t.Params().At(i).Type())
			}
			for i := range t.Results().Len() {
				visit(t.Results().At(i).Type())
			}
		case *types.Named:
			if t.TypeArgs() != nil {
				for i := range t.TypeArgs().Len() {
					visit(t.TypeArgs().At(i))
				}
			}
		}
	}
	visit(t)
	return found
}

// listVersion is the <F>List of a function (R4): package store's for a helper
// of core or slices, the output package's for a function of a translated
// package. nil when there is none.
func (f *fn) listVersion(callee *types.Func, pos token.Pos) (ast.Expr, types.Type) {
	if callee == nil || callee.Pkg() == nil {
		return nil, nil
	}
	name := callee.Name() + "List"
	switch path := callee.Pkg().Path(); {
	case path == corePath || path == "slices":
		if obj := f.store.Scope().Lookup(name); obj != nil {
			return f.qualify(f.store, name, pos), obj.Type()
		}
	default:
		o := f.outOf(callee)
		if o == nil || !f.provides(o, name) {
			return nil, nil
		}
		x := f.qualifyPath(o.path, o.name, name, pos)
		if f.w != nil && f.w.stubbed {
			if pkg := f.w.byPath[o.path]; pkg != nil && pkg.Types != nil {
				if obj := pkg.Types.Scope().Lookup(name); obj != nil {
					return x, obj.Type()
				}
			}
			return nil, nil
		}
		return x, nil
	}
	return nil, nil
}

// callResult is the Store type of a call's result: the Store callee's, or,
// when it is generic, the Pointer result mapped.
func (f *fn) callResult(c *ast.CallExpr, ft types.Type) types.Type {
	s, ok := ft.(*types.Signature)
	if !ok {
		return f.storeTypeOf(c)
	}
	switch s.Results().Len() {
	case 0:
		return nil
	case 1:
		r := s.Results().At(0).Type()
		if hasTypeParam(r) {
			return f.storeTypeOf(c)
		}
		return r
	}
	if hasTypeParam(s.Results()) {
		return f.storeTypeOf(c)
	}
	return s.Results()
}

// calleeIdent is the identifier that names the function of a call.
func calleeIdent(fun ast.Expr) *ast.Ident {
	switch fx := ast.Unparen(fun).(type) {
	case *ast.Ident:
		return fx
	case *ast.SelectorExpr:
		return fx.Sel
	case *ast.IndexExpr:
		return calleeIdent(fx.X)
	case *ast.IndexListExpr:
		return calleeIdent(fx.X)
	}
	return nil
}

// sameBasic converts the result of a Store callee to the Pointer's type when
// the two differ only in their name over the same basic type:
// store.GetSymbolId returns uint64 where ast.GetSymbolId returns
// ast.SymbolId.
func (f *fn) sameBasic(x ast.Expr, storeT types.Type, c *ast.CallExpr) (ast.Expr, types.Type) {
	pt := f.info.TypeOf(c)
	if storeT == nil || pt == nil {
		return x, storeT
	}
	want, _ := f.mapType(pt)
	if types.Identical(want, storeT) {
		return x, storeT
	}
	wb, ok1 := want.Underlying().(*types.Basic)
	sb, ok2 := storeT.Underlying().(*types.Basic)
	if !ok1 || !ok2 || wb.Kind() != sb.Kind() {
		return x, storeT
	}
	return &ast.CallExpr{Fun: f.typeExpr(want, c.Pos()), Lparen: c.Pos(), Args: []ast.Expr{x}, Rparen: c.End()}, want
}
