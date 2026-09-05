package estransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
)

func isClassThisAssignmentBlock(emitContext *printer.EmitContext, node ast.Handle) bool {
	if ast.IsClassStaticBlockDeclaration(node) {
		n := node
		body := n.Body()
		if len(body.Statements()) == 1 {
			statement := body.Statements()[0]
			if ast.IsExpressionStatement(statement) {
				expression := statement.Expression()
				if ast.IsAssignmentExpression(expression, true) {
					binary := expression
					return ast.IsIdentifier(binary.Left()) && emitContext.ClassThis(node) == binary.Left() && binary.Right().Kind == ast.KindThisKeyword
				}
			}
		}
	}
	return false
}
