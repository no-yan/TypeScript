package sample

import "testdata.rewrite/internal/ast"

type TypeMapper struct{}

func (m *TypeMapper) Kind() int { return 0 }

func walk(n ast.Handle, e ast.Handle, id ast.Handle, list *ast.NodeList, file *ast.SourceFile) ast.Handle {
	if n == nil || e != nil {
		_ = n.Kind
		_ = n.Flags
		_ = n.Parent
		_ = n.Loc
		_ = e.Kind
		_ = n.AsParameterDeclaration()
	}
	_ = (ast.Handle).IsJSDoc
	_ = (ast.Handle).IsJSDoc
	n.Flags |= 1
	n.Parent = n
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
	if n == nil {
		return nil
	}
	return n
}

func walkExpr(e ast.Handle) ast.Handle {
	if e == nil {
		return nil
	}
	_ = e.Kind
	_ = e.Flags
	_ = e.Parent
	_ = e.Loc
	return e
}
