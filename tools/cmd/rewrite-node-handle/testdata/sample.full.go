package sample

import "testdata.rewrite/internal/ast"

type TypeMapper struct{}

func (m *TypeMapper) Kind() int { return 0 }

func walk(n ast.Handle, e ast.Handle, id ast.Handle, list *ast.NodeList, file *ast.SourceFile) ast.Handle {
	if n.IsNil() || !e.IsNil() {
		_ = n.Kind()
		_ = n.Flags()
		_ = n.Parent()
		_ = n.Loc()
		_ = e.Kind()
		_ = n.AsParameterDeclaration()
	}
	_ = (ast.Handle).IsJSDoc
	_ = (ast.Handle).IsJSDoc
	n.SetFlags(n.Flags() | 1)
	n.SetParent(n)
	var mapper TypeMapper
	_ = mapper.Kind()
	var s ast.Symbol
	_ = s.Flags
	_ = s.Parent
	var nodes []ast.Handle
	_ = nodes
	_ = list
	_ = file
	_ = id
	if n.IsNil() {
		return ast.Handle{}
	}
	return n
}

func walkExpr(e ast.Handle) ast.Handle {
	if e.IsNil() {
		return ast.Handle{}
	}
	_ = e.Kind()
	_ = e.Flags()
	_ = e.Parent()
	_ = e.Loc()
	return e
}
