package evaluator

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/jsnum"
)

type Result struct {
	Value                 any
	IsSyntacticallyString bool
	ResolvedOtherFiles    bool
	HasExternalReferences bool
}

func NewResult(value any, isSyntacticallyString bool, resolvedOtherFiles bool, hasExternalReferences bool) Result {
	return Result{value, isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
}

type Evaluator func(expr ast.Handle, location ast.Handle) Result

func NewEvaluator(evaluateEntity Evaluator, outerExpressionsToSkip ast.OuterExpressionKinds) Evaluator {
	var evaluate Evaluator
	evaluate = func(expr ast.Handle, location ast.Handle) Result {
		isSyntacticallyString := false
		resolvedOtherFiles := false
		hasExternalReferences := false
		expr = ast.SkipOuterExpressions(expr, outerExpressionsToSkip|ast.OEKParentheses)
		switch expr.Kind {
		case ast.KindPrefixUnaryExpression:
			result := evaluate(expr.PrefixUnaryExpressionOperand(), location)
			resolvedOtherFiles = result.ResolvedOtherFiles
			hasExternalReferences = result.HasExternalReferences
			if value, ok := result.Value.(jsnum.Number); ok {
				switch expr.PrefixUnaryExpressionOperator() {
				case ast.KindPlusToken:
					return Result{value, isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
				case ast.KindMinusToken:
					return Result{-value, isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
				case ast.KindTildeToken:
					return Result{value.BitwiseNOT(), isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
				}
			}
		case ast.KindBinaryExpression:
			left := evaluate(expr.Left(), location)
			right := evaluate(expr.Right(), location)
			operator := expr.Operator().Kind
			isSyntacticallyString = (left.IsSyntacticallyString || right.IsSyntacticallyString) && operator == ast.KindPlusToken
			resolvedOtherFiles = left.ResolvedOtherFiles || right.ResolvedOtherFiles
			hasExternalReferences = left.HasExternalReferences || right.HasExternalReferences
			leftNum, leftIsNum := left.Value.(jsnum.Number)
			rightNum, rightIsNum := right.Value.(jsnum.Number)
			if leftIsNum && rightIsNum {
				switch operator {
				case ast.KindBarToken:
					return Result{leftNum.BitwiseOR(rightNum), isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
				case ast.KindAmpersandToken:
					return Result{leftNum.BitwiseAND(rightNum), isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
				case ast.KindGreaterThanGreaterThanToken:
					return Result{leftNum.SignedRightShift(rightNum), isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
				case ast.KindGreaterThanGreaterThanGreaterThanToken:
					return Result{leftNum.UnsignedRightShift(rightNum), isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
				case ast.KindLessThanLessThanToken:
					return Result{leftNum.LeftShift(rightNum), isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
				case ast.KindCaretToken:
					return Result{leftNum.BitwiseXOR(rightNum), isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
				case ast.KindAsteriskToken:
					return Result{leftNum * rightNum, isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
				case ast.KindSlashToken:
					return Result{leftNum / rightNum, isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
				case ast.KindPlusToken:
					return Result{leftNum + rightNum, isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
				case ast.KindMinusToken:
					return Result{leftNum - rightNum, isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
				case ast.KindPercentToken:
					return Result{leftNum.Remainder(rightNum), isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
				case ast.KindAsteriskAsteriskToken:
					return Result{leftNum.Exponentiate(rightNum), isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
				}
			}
			leftStr, leftIsStr := left.Value.(string)
			rightStr, rightIsStr := right.Value.(string)
			if (leftIsStr || leftIsNum) && (rightIsStr || rightIsNum) && operator == ast.KindPlusToken {
				if leftIsNum {
					leftStr = leftNum.String()
				}
				if rightIsNum {
					rightStr = rightNum.String()
				}
				return Result{leftStr + rightStr, isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
			}
		case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
			return Result{expr.Text(), true /*isSyntacticallyString*/, false, false}
		case ast.KindTemplateExpression:
			return evaluateTemplateExpression(expr, location, evaluate)
		case ast.KindNumericLiteral:
			return Result{jsnum.FromString(expr.Text()), false, false, false}
		case ast.KindIdentifier:
			return evaluateEntity(expr, location)
		case ast.KindElementAccessExpression, ast.KindPropertyAccessExpression:
			if ast.IsEntityNameExpression(expr.Expression()) {
				return evaluateEntity(expr, location)
			}
		}
		return Result{nil, isSyntacticallyString, resolvedOtherFiles, hasExternalReferences}
	}
	return evaluate
}

func evaluateTemplateExpression(expr ast.Handle, location ast.Handle, evaluate Evaluator) Result {
	var sb strings.Builder
	sb.WriteString(expr.TemplateExpressionHead().Text())
	resolvedOtherFiles := false
	hasExternalReferences := false
	for _, span := range expr.TemplateSpans() {
		spanResult := evaluate(span.Expression(), location)
		if spanResult.Value == nil {
			return Result{nil, true /*isSyntacticallyString*/, false, false}
		}
		sb.WriteString(AnyToString(spanResult.Value))
		sb.WriteString(span.TemplateSpanLiteral().Text())
		resolvedOtherFiles = resolvedOtherFiles || spanResult.ResolvedOtherFiles
		hasExternalReferences = hasExternalReferences || spanResult.HasExternalReferences
	}
	return Result{sb.String(), true, resolvedOtherFiles, hasExternalReferences}
}

func AnyToString(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case jsnum.Number:
		return v.String()
	case bool:
		return core.IfElse(v, "true", "false")
	case jsnum.PseudoBigInt:
		return v.String()
	}
	panic("Unhandled case in AnyToString")
}

func IsTruthy(v any) bool {
	switch v := v.(type) {
	case string:
		return len(v) != 0
	case jsnum.Number:
		return v != 0 && !v.IsNaN()
	case bool:
		return v
	case jsnum.PseudoBigInt:
		return v != jsnum.PseudoBigInt{}
	}
	panic("Unhandled case in IsTruthy")
}
