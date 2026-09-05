package estransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
)

type logicalAssignmentTransformer struct{ transformers.Transformer }

func (ch *logicalAssignmentTransformer) visit(node ast.Handle) ast.Handle {
	if node.SubtreeFacts()&ast.SubtreeContainsLogicalAssignments == 0 {
		return node
	}
	switch node.Kind {
	case ast.KindBinaryExpression:
		return ch.visitBinaryExpression(node)
	default:
		return ch.Visitor().VisitEachChild(node)
	}
}
func (ch *logicalAssignmentTransformer) visitBinaryExpression(node ast.Handle) ast.Handle {
	var nonAssignmentOperator ast.Kind
	switch node.OperatorToken().Kind {
	case ast.KindBarBarEqualsToken:
		nonAssignmentOperator = ast.KindBarBarToken
	case ast.KindAmpersandAmpersandEqualsToken:
		nonAssignmentOperator = ast.KindAmpersandAmpersandToken
	case ast.KindQuestionQuestionEqualsToken:
		nonAssignmentOperator = ast.KindQuestionQuestionToken
	default:
		return ch.Visitor().VisitEachChild(node)
	}
	left := ast.SkipParentheses(ch.Visitor().VisitNode(node.Left()))
	assignmentTarget := left
	right := ast.SkipParentheses(ch.Visitor().VisitNode(node.Right()))
	if ast.IsAccessExpression(left) {
		propertyAccessTargetSimpleCopiable := transformers.IsSimpleCopiableExpression(left.Expression())
		propertyAccessTarget := left.Expression()
		propertyAccessTargetAssignment := left.Expression()
		if !propertyAccessTargetSimpleCopiable {
			propertyAccessTarget = ch.Factory().NewTempVariable()
			ch.EmitContext().AddVariableDeclaration(propertyAccessTarget)
			propertyAccessTargetAssignment = ch.Factory().NewAssignmentExpression(propertyAccessTarget, left.Expression())
		}
		if ast.IsPropertyAccessExpression(left) {
			assignmentTarget = ch.Factory().NewPropertyAccessExpression(propertyAccessTarget, ast.Handle{}, left.Name(), ast.NodeFlagsNone)
			left = ch.Factory().NewPropertyAccessExpression(propertyAccessTargetAssignment, ast.Handle{}, left.Name(), ast.NodeFlagsNone)
		} else {
			elementAccessArgumentSimpleCopiable := transformers.IsSimpleCopiableExpression(left.ElementAccessExpressionArgumentExpression())
			elementAccessArgument := left.ElementAccessExpressionArgumentExpression()
			argumentExpr := elementAccessArgument
			if !elementAccessArgumentSimpleCopiable {
				elementAccessArgument = ch.Factory().NewTempVariable()
				ch.EmitContext().AddVariableDeclaration(elementAccessArgument)
				argumentExpr = ch.Factory().NewAssignmentExpression(elementAccessArgument, left.ElementAccessExpressionArgumentExpression())
			}
			assignmentTarget = ch.Factory().NewElementAccessExpression(propertyAccessTarget, ast.Handle{}, elementAccessArgument, ast.NodeFlagsNone)
			left = ch.Factory().NewElementAccessExpression(propertyAccessTargetAssignment, ast.Handle{}, argumentExpr, ast.NodeFlagsNone)
		}
	}
	return ch.Factory().NewBinaryExpression(0, left, ast.Handle{}, ch.Factory().NewToken(nonAssignmentOperator), ch.Factory().NewParenthesizedExpression(ch.Factory().NewAssignmentExpression(assignmentTarget, right)))
}
func newLogicalAssignmentTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	tx := &logicalAssignmentTransformer{}
	return tx.NewTransformer(tx.visit, opts.Context)
}
