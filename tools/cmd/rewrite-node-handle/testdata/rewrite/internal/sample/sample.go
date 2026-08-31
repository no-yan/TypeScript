package sample

import "testdata.rewrite/internal/ast"

type TypeMapper struct{}

func (m *TypeMapper) Kind() int { return 0 }

func walk(n *ast.Node, e *ast.Expression, id *ast.IdentifierNode, list *ast.NodeList, file *ast.SourceFile) *ast.Node {
	if n == nil || e != nil {
		_ = n.Kind
		_ = n.Flags
		_ = n.Parent
		_ = n.Loc
		_ = n.AsParameterDeclaration()
	}
	n.Flags |= 1
	n.Parent = e
	var mapper TypeMapper
	_ = mapper.Kind()
	var s ast.Symbol
	_ = s.Flags
	_ = s.Parent
	var nodes []*ast.Node
	_ = nodes
	_ = list
	_ = file
	_ = id
	if n == nil {
		return nil
	}
	return n
}
