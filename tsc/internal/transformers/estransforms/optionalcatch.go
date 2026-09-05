package estransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
)

type optionalCatchTransformer struct{ transformers.Transformer }

func (ch *optionalCatchTransformer) visit(node ast.Handle) ast.Handle {
	if node.SubtreeFacts()&ast.SubtreeContainsMissingCatchClauseVariable == 0 {
		return node
	}
	switch node.Kind() {
	case ast.KindCatchClause:
		return ch.visitCatchClause(node)
	default:
		return ch.Visitor().VisitEachChild(node)
	}
}
func (ch *optionalCatchTransformer) visitCatchClause(node ast.Handle) ast.Handle {
	if node.VariableDeclaration().IsNil() {
		return ch.Factory().NewCatchClause(ch.Factory().NewVariableDeclaration(ch.Factory().NewTempVariable(), ast.Handle{}, ast.Handle{}, ast.Handle{}), ch.Visitor().Visit(node.Block()))
	}
	return ch.Visitor().VisitEachChild(node)
}
func newOptionalCatchTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	tx := &optionalCatchTransformer{}
	return tx.NewTransformer(tx.visit, opts.Context)
}
