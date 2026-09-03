package estransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
)

type exponentiationTransformer struct{ transformers.Transformer }

func (ch *exponentiationTransformer) visit(node ast.Handle) ast.Handle {
	if node.SubtreeFacts()&ast.SubtreeContainsExponentiationOperator == 0 {
		return node
	}
	switch node.Kind {
	case ast.KindBinaryExpression:
		return ch.visitBinaryExpression(node)
	default:
		return ch.Visitor().VisitEachChild(node)
	}
}
func (ch *exponentiationTransformer) visitBinaryExpression(node ast.Handle) ast.Handle {
	switch node.OperatorToken().Kind {
	case ast.KindAsteriskAsteriskEqualsToken:
		return ch.visitExponentiationAssignmentExpression(node)
	case ast.KindAsteriskAsteriskToken:
		return ch.visitExponentiationExpression(node)
	}
	return ch.Visitor().VisitEachChild(node)
}
func (ch *exponentiationTransformer) visitExponentiationAssignmentExpression(node ast.Handle) ast.Handle {
	var target ast.Handle
	var value ast.Handle
	left := ch.Visitor().VisitNode(node.Left())
	right := ch.Visitor().VisitNode(node.Right())
	if ast.IsElementAccessExpression(left) {
		expressionTemp := ch.Factory().NewTempVariable()
		ch.EmitContext().AddVariableDeclaration(expressionTemp)
		argumentExpressionTemp := ch.Factory().NewTempVariable()
		ch.EmitContext().AddVariableDeclaration(argumentExpressionTemp)
		objExpr := ch.Factory().NewAssignmentExpression(expressionTemp, left.Expression())
		objExpr.SetLoc(left.Expression().Loc())
		accessExpr := ch.Factory().NewAssignmentExpression(argumentExpressionTemp, left.ElementAccessExpressionArgumentExpression())
		accessExpr.SetLoc(left.ElementAccessExpressionArgumentExpression().Loc())
		target = ch.Factory().NewElementAccessExpression(objExpr, ast.Handle{}, accessExpr, ast.NodeFlagsNone)
		value = ch.Factory().NewElementAccessExpression(expressionTemp, ast.Handle{}, argumentExpressionTemp, ast.NodeFlagsNone)
		value.SetLoc(left.Loc())
	} else if ast.IsPropertyAccessExpression(left) {
		expressionTemp := ch.Factory().NewTempVariable()
		ch.EmitContext().AddVariableDeclaration(expressionTemp)
		assignment := ch.Factory().NewAssignmentExpression(expressionTemp, left.Expression())
		assignment.SetLoc(left.Expression().Loc())
		target = ch.Factory().NewPropertyAccessExpression(assignment, ast.Handle{}, left.Name(), ast.NodeFlagsNone)
		target.SetLoc(left.Loc())
		value = ch.Factory().NewPropertyAccessExpression(expressionTemp, ast.Handle{}, left.Name(), ast.NodeFlagsNone)
		value.SetLoc(left.Loc())
	} else {
		target = left
		value = left
	}
	rhs := ch.Factory().NewGlobalMethodCall("Math", "pow", []ast.Handle{value, right})
	rhs.SetLoc(node.Loc())
	result := ch.Factory().NewAssignmentExpression(target, rhs)
	result.SetLoc(node.Loc())
	return result
}
func (ch *exponentiationTransformer) visitExponentiationExpression(node ast.Handle) ast.Handle {
	left := ch.Visitor().VisitNode(node.Left())
	right := ch.Visitor().VisitNode(node.Right())
	result := ch.Factory().NewGlobalMethodCall("Math", "pow", []ast.Handle{left, right})
	result.SetLoc(node.Loc())
	return result
}
func newExponentiationTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	tx := &exponentiationTransformer{}
	return tx.NewTransformer(tx.visit, opts.Context)
}
