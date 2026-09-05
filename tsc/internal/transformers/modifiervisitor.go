package transformers

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
)

type modifierVisitor struct {
	Transformer
	AllowedModifiers ast.ModifierFlags
}

func (v *modifierVisitor) visit(node ast.Handle) ast.Handle {
	flags := ast.ModifierToFlag(node.Kind)
	if flags != ast.ModifierFlagsNone && flags&v.AllowedModifiers == 0 {
		return ast.Handle{}
	}
	return node
}
func ExtractModifiers(emitContext *printer.EmitContext, modifiers ast.ListRef, allowed ast.ModifierFlags) ast.ListRef {
	if modifiers == 0 {
		return 0
	}
	tx := modifierVisitor{AllowedModifiers: allowed}
	tx.NewTransformer(tx.visit, emitContext)
	return tx.visitor.VisitModifiers(modifiers)
}
