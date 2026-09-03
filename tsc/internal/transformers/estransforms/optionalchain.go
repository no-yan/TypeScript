package estransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
)

type optionalChainTransformer struct{ transformers.Transformer }

func (ch *optionalChainTransformer) visit(node ast.Handle) ast.Handle {
	if node.SubtreeFacts()&ast.SubtreeContainsOptionalChaining == 0 {
		return node
	}
	switch node.Kind {
	case ast.KindCallExpression:
		return ch.visitCallExpression(node, false)
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		if node.Flags()&ast.NodeFlagsOptionalChain != 0 {
			return ch.visitOptionalExpression(node, false, false)
		}
		return ch.Visitor().VisitEachChild(node)
	case ast.KindDeleteExpression:
		return ch.visitDeleteExpression(node)
	default:
		return ch.Visitor().VisitEachChild(node)
	}
}
func (ch *optionalChainTransformer) visitCallExpression(node ast.Handle, captureThisArg bool) ast.Handle {
	if node.Flags()&ast.NodeFlagsOptionalChain != 0 {
		return ch.visitOptionalExpression(node, captureThisArg, false)
	}
	if ast.IsParenthesizedExpression(node.Expression()) {
		unwrapped := ast.SkipParentheses(node.Expression())
		if unwrapped.Flags()&ast.NodeFlagsOptionalChain != 0 {
			expression := ch.visitParenthesizedExpression(node.Expression(), true, false)
			args := ch.Visitor().VisitSlice(node.Arguments())
			if ast.IsSyntheticReferenceExpression(expression) {
				res := ch.Factory().NewFunctionCallCall(expression.SyntheticReferenceExpressionExpression(), expression.SyntheticReferenceExpressionThisArg(), args)
				res.SetLoc(node.Loc())
				ch.EmitContext().SetOriginal(res, node)
				return res
			}
			return ch.Factory().UpdateCallExpression(node, expression, ast.Handle{}, 0, ch.Factory().NewList(args), node.Flags())
		}
	}
	return ch.Visitor().VisitEachChild(node)
}
func (ch *optionalChainTransformer) visitParenthesizedExpression(node ast.Handle, captureThisArg bool, isDelete bool) ast.Handle {
	expr := ch.visitNonOptionalExpression(node.Expression(), captureThisArg, isDelete)
	if ast.IsSyntheticReferenceExpression(expr) {
		synth := expr
		res := ch.Factory().NewSyntheticReferenceExpression(ch.Factory().UpdateParenthesizedExpression(node, synth.Expression()), synth.ThisArg())
		ch.EmitContext().SetOriginal(res, node)
		return res
	}
	return ch.Factory().UpdateParenthesizedExpression(node, expr)
}
func (ch *optionalChainTransformer) visitPropertyOrElementAccessExpression(node ast.Handle, captureThisArg bool, isDelete bool) ast.Handle {
	if node.Flags()&ast.NodeFlagsOptionalChain != 0 {
		return ch.visitOptionalExpression(node, captureThisArg, isDelete)
	}
	expression := ch.Visitor().VisitNode(node.Expression())
	debug.Assert(expression.IsNil() || !ast.IsSyntheticReferenceExpression(expression))
	var thisArg ast.Handle
	if captureThisArg {
		if !transformers.IsSimpleCopiableExpression(expression) {
			thisArg = ch.Factory().NewTempVariable()
			ch.EmitContext().AddVariableDeclaration(thisArg)
			expression = ch.Factory().NewAssignmentExpression(thisArg, expression)
		} else {
			thisArg = expression
		}
	}
	if node.Kind == ast.KindPropertyAccessExpression {
		p := node
		expression = ch.Factory().UpdatePropertyAccessExpression(p, expression, ast.Handle{}, ch.Visitor().VisitNode(p.Name()), p.Flags())
	} else {
		p := node
		expression = ch.Factory().UpdateElementAccessExpression(p, expression, ast.Handle{}, ch.Visitor().VisitNode(p.ElementAccessExpressionArgumentExpression()), p.Flags())
	}
	if !thisArg.IsNil() {
		res := ch.Factory().NewSyntheticReferenceExpression(expression, thisArg)
		ch.EmitContext().SetOriginal(res, node)
		return res
	}
	return expression
}
func (ch *optionalChainTransformer) visitDeleteExpression(node ast.Handle) ast.Handle {
	unwrapped := ast.SkipParentheses(node.Expression())
	if unwrapped.Flags()&ast.NodeFlagsOptionalChain != 0 {
		return ch.visitNonOptionalExpression(node.Expression(), false, true)
	}
	return ch.Visitor().VisitEachChild(node)
}
func (ch *optionalChainTransformer) visitNonOptionalExpression(node ast.Handle, captureThisArg bool, isDelete bool) ast.Handle {
	switch node.Kind {
	case ast.KindParenthesizedExpression:
		return ch.visitParenthesizedExpression(node, captureThisArg, isDelete)
	case ast.KindElementAccessExpression, ast.KindPropertyAccessExpression:
		return ch.visitPropertyOrElementAccessExpression(node, captureThisArg, isDelete)
	case ast.KindCallExpression:
		return ch.visitCallExpression(node, captureThisArg)
	default:
		return ch.Visitor().VisitNode(node)
	}
}

type flattenResult struct {
	expression ast.Handle
	chain      []ast.Handle
}

func isNonNullChain(node ast.Handle) bool {
	return ast.IsNonNullExpression(node) && node.Flags()&ast.NodeFlagsOptionalChain != 0
}
func flattenChain(chain ast.Handle) flattenResult {
	debug.Assert(!isNonNullChain(chain))
	links := []ast.Handle{chain}
	for !ast.IsTaggedTemplateExpression(chain) && chain.QuestionDotToken().IsNil() {
		chain = ast.SkipPartiallyEmittedExpressions(chain.Expression())
		debug.Assert(!isNonNullChain(chain))
		links = append([]ast.Handle{chain}, links...)
	}
	return flattenResult{chain.Expression(), links}
}
func isCallChain(node ast.Handle) bool {
	return ast.IsCallExpression(node) && node.Flags()&ast.NodeFlagsOptionalChain != 0
}
func (ch *optionalChainTransformer) visitOptionalExpression(node ast.Handle, captureThisArg bool, isDelete bool) ast.Handle {
	r := flattenChain(node)
	expression := r.expression
	chain := r.chain
	left := ch.visitNonOptionalExpression(ast.SkipPartiallyEmittedExpressions(expression), isCallChain(chain[0]), false)
	var leftThisArg ast.Handle
	capturedLeft := left
	if ast.IsSyntheticReferenceExpression(left) {
		leftThisArg = left.SyntheticReferenceExpressionThisArg()
		capturedLeft = left.SyntheticReferenceExpressionExpression()
	}
	leftExpression := ch.Factory().RestoreOuterExpressions(expression, capturedLeft, ast.OEKPartiallyEmittedExpressions)
	if !transformers.IsSimpleCopiableExpression(capturedLeft) {
		capturedLeft = ch.Factory().NewTempVariable()
		ch.EmitContext().AddVariableDeclaration(capturedLeft)
		leftExpression = ch.Factory().NewAssignmentExpression(capturedLeft, leftExpression)
	}
	rightExpression := capturedLeft
	var thisArg ast.Handle
	for i, segment := range chain {
		switch segment.Kind {
		case ast.KindElementAccessExpression, ast.KindPropertyAccessExpression:
			if i == len(chain)-1 && captureThisArg {
				if !transformers.IsSimpleCopiableExpression(rightExpression) {
					thisArg = ch.Factory().NewTempVariable()
					ch.EmitContext().AddVariableDeclaration(thisArg)
					rightExpression = ch.Factory().NewAssignmentExpression(thisArg, rightExpression)
				} else {
					thisArg = rightExpression
				}
			}
			if segment.Kind == ast.KindElementAccessExpression {
				rightExpression = ch.Factory().NewElementAccessExpression(rightExpression, ast.Handle{}, ch.Visitor().VisitNode(segment.ElementAccessExpressionArgumentExpression()), ast.NodeFlagsNone)
			} else {
				rightExpression = ch.Factory().NewPropertyAccessExpression(rightExpression, ast.Handle{}, ch.Visitor().VisitNode(segment.PropertyAccessExpressionName()), ast.NodeFlagsNone)
			}
		case ast.KindCallExpression:
			if i == 0 && !leftThisArg.IsNil() {
				if !ch.EmitContext().HasAutoGenerateInfo(leftThisArg) {
					leftThisArg = ch.Factory().DeepCloneNode(leftThisArg)
					ch.EmitContext().AddEmitFlags(leftThisArg, printer.EFNoComments)
				}
				callThisArg := leftThisArg
				if leftThisArg.Kind == ast.KindSuperKeyword {
					callThisArg = ch.Factory().NewThisExpression()
				}
				rightExpression = ch.Factory().NewFunctionCallCall(rightExpression, callThisArg, ch.Visitor().VisitSlice(segment.Arguments()))
			} else {
				rightExpression = ch.Factory().NewCallExpression(rightExpression, ast.Handle{}, 0, ch.Visitor().VisitNodes(segment.ArgumentList()), ast.NodeFlagsNone)
			}
		}
		ch.EmitContext().SetOriginal(rightExpression, segment)
	}
	var target ast.Handle
	if isDelete {
		target = ch.Factory().NewConditionalExpression(createNotNullCondition(ch.EmitContext(), leftExpression, capturedLeft, true), ch.Factory().NewToken(ast.KindQuestionToken), ch.Factory().NewTrueExpression(), ch.Factory().NewToken(ast.KindColonToken), ch.Factory().NewDeleteExpression(rightExpression))
	} else {
		target = ch.Factory().NewConditionalExpression(createNotNullCondition(ch.EmitContext(), leftExpression, capturedLeft, true), ch.Factory().NewToken(ast.KindQuestionToken), ch.Factory().NewVoidZeroExpression(), ch.Factory().NewToken(ast.KindColonToken), rightExpression)
	}
	target.SetLoc(node.Loc())
	if !thisArg.IsNil() {
		target = ch.Factory().NewSyntheticReferenceExpression(target, thisArg)
	}
	ch.EmitContext().SetOriginal(target, node)
	return target
}
func newOptionalChainTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	tx := &optionalChainTransformer{}
	return tx.NewTransformer(tx.visit, opts.Context)
}
