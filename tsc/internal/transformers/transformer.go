package transformers

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
)

type Transformer struct {
	emitContext *printer.EmitContext
	factory     *printer.NodeFactory
	visitor     *ast.HandleVisitor
}

func (tx *Transformer) NewTransformer(visit func(node ast.Handle) ast.Handle, emitContext *printer.EmitContext) *Transformer {
	if tx.emitContext != nil {
		panic("Transformer already initialized")
	}
	if emitContext == nil {
		emitContext = printer.NewEmitContext()
	}
	tx.emitContext = emitContext
	tx.factory = emitContext.Factory
	tx.visitor = emitContext.NewNodeVisitor(visit)
	return tx
}
func (tx *Transformer) EmitContext() *printer.EmitContext {
	return tx.emitContext
}
func (tx *Transformer) Visitor() *ast.HandleVisitor {
	return tx.visitor
}
func (tx *Transformer) Factory() *printer.NodeFactory {
	return tx.factory
}
func (tx *Transformer) TransformSourceFile(file *ast.SourceFile) *ast.SourceFile {
	tx.emitContext.BindFileStore(file)
	if tx.factory != nil {
		tx.visitor.Factory = tx.factory.Factory
	}
	return tx.visitor.VisitSourceFile(file)
}
