package lsutil

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
)

func PositionBelongsToNode(candidate ast.Handle, position int, file *ast.SourceFile) bool {
	if candidate.Pos() > position {
		panic("Expected candidate.pos <= position")
	}
	return position < candidate.End() || !IsCompletedNode(candidate, file)
}
func IsCompletedNode(n ast.Handle, sourceFile *ast.SourceFile) bool {
	if n.IsNil() || ast.NodeIsMissing(n) {
		return false
	}
	switch n.Kind {
	case ast.KindClassDeclaration, ast.KindInterfaceDeclaration, ast.KindEnumDeclaration, ast.KindObjectLiteralExpression, ast.KindObjectBindingPattern, ast.KindTypeLiteral, ast.KindBlock, ast.KindModuleBlock, ast.KindCaseBlock, ast.KindNamedImports, ast.KindNamedExports:
		return nodeEndsWith(n, ast.KindCloseBraceToken, sourceFile)
	case ast.KindCatchClause:
		return IsCompletedNode(n.CatchClauseBlock(), sourceFile)
	case ast.KindNewExpression:
		if n.ArgumentList() == 0 {
			return true
		}
		fallthrough
	case ast.KindCallExpression, ast.KindParenthesizedExpression, ast.KindParenthesizedType:
		return nodeEndsWith(n, ast.KindCloseParenToken, sourceFile)
	case ast.KindFunctionType, ast.KindConstructorType:
		return IsCompletedNode(n.Type(), sourceFile)
	case ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindConstructSignature, ast.KindCallSignature, ast.KindArrowFunction:
		if !n.Body().IsNil() {
			return IsCompletedNode(n.Body(), sourceFile)
		}
		if !n.Type().IsNil() {
			return IsCompletedNode(n.Type(), sourceFile)
		}
		return hasChildOfKind(n, ast.KindCloseParenToken, sourceFile)
	case ast.KindModuleDeclaration:
		return !n.Body().IsNil() && IsCompletedNode(n.Body(), sourceFile)
	case ast.KindIfStatement:
		if !n.IfStatementElseStatement().IsNil() {
			return IsCompletedNode(n.IfStatementElseStatement(), sourceFile)
		}
		return IsCompletedNode(n.IfStatementThenStatement(), sourceFile)
	case ast.KindExpressionStatement:
		return IsCompletedNode(n.Expression(), sourceFile) || hasChildOfKind(n, ast.KindSemicolonToken, sourceFile)
	case ast.KindArrayLiteralExpression, ast.KindArrayBindingPattern, ast.KindElementAccessExpression, ast.KindComputedPropertyName, ast.KindTupleType:
		return nodeEndsWith(n, ast.KindCloseBracketToken, sourceFile)
	case ast.KindIndexSignature:
		if !n.IndexSignatureDeclarationType().IsNil() {
			return IsCompletedNode(n.IndexSignatureDeclarationType(), sourceFile)
		}
		return hasChildOfKind(n, ast.KindCloseBracketToken, sourceFile)
	case ast.KindCaseClause, ast.KindDefaultClause:
		return false
	case ast.KindForStatement, ast.KindForInStatement, ast.KindForOfStatement, ast.KindWhileStatement:
		return IsCompletedNode(n.Statement(), sourceFile)
	case ast.KindDoStatement:
		if hasChildOfKind(n, ast.KindWhileKeyword, sourceFile) {
			return nodeEndsWith(n, ast.KindCloseParenToken, sourceFile)
		}
		return IsCompletedNode(n.Statement(), sourceFile)
	case ast.KindTypeQuery:
		return IsCompletedNode(n.TypeQueryNodeExprName(), sourceFile)
	case ast.KindTypeOfExpression, ast.KindDeleteExpression, ast.KindVoidExpression, ast.KindYieldExpression, ast.KindSpreadElement:
		return IsCompletedNode(n.Expression(), sourceFile)
	case ast.KindTaggedTemplateExpression:
		return IsCompletedNode(n.TaggedTemplateExpressionTemplate(), sourceFile)
	case ast.KindTemplateExpression:
		if n.TemplateExpressionTemplateSpans() == 0 {
			return false
		}
		lastSpan := core.LastOrNil(n.Store().ListSlice(n.TemplateExpressionTemplateSpans()))
		return IsCompletedNode(lastSpan, sourceFile)
	case ast.KindTemplateSpan:
		return ast.NodeIsPresent(n.TemplateSpanLiteral())
	case ast.KindExportDeclaration, ast.KindImportDeclaration:
		return ast.NodeIsPresent(n.ModuleSpecifier())
	case ast.KindPrefixUnaryExpression:
		return IsCompletedNode(n.PrefixUnaryExpressionOperand(), sourceFile)
	case ast.KindBinaryExpression:
		return IsCompletedNode(n.BinaryExpressionRight(), sourceFile)
	case ast.KindConditionalExpression:
		return IsCompletedNode(n.ConditionalExpressionWhenFalse(), sourceFile)
	default:
		return true
	}
}

func nodeEndsWith(n ast.Handle, expectedLastToken ast.Kind, sourceFile *ast.SourceFile) bool {
	lastChildNode := GetLastVisitedChild(n, sourceFile)
	var lastNodeAndTokens []ast.Handle
	var tokenStartPos int
	if !lastChildNode.IsNil() {
		lastNodeAndTokens = []ast.Handle{lastChildNode}
		tokenStartPos = lastChildNode.End()
	} else {
		tokenStartPos = n.Pos()
	}
	scanner := scanner.GetScannerForSourceFile(sourceFile, tokenStartPos)
	for startPos := tokenStartPos; startPos < n.End(); {
		tokenKind := scanner.Token()
		tokenFullStart := scanner.TokenFullStart()
		tokenEnd := scanner.TokenEnd()
		token := sourceFile.GetOrCreateToken(tokenKind, tokenFullStart, tokenEnd, n, scanner.TokenFlags())
		lastNodeAndTokens = append(lastNodeAndTokens, token)
		startPos = tokenEnd
		scanner.Scan()
	}
	if len(lastNodeAndTokens) == 0 {
		return false
	}
	lastChild := lastNodeAndTokens[len(lastNodeAndTokens)-1]
	if lastChild.Kind == expectedLastToken {
		return true
	} else if lastChild.Kind == ast.KindSemicolonToken && len(lastNodeAndTokens) > 1 {
		return lastNodeAndTokens[len(lastNodeAndTokens)-2].Kind == expectedLastToken
	}
	return false
}
func hasChildOfKind(containingNode ast.Handle, kind ast.Kind, sourceFile *ast.SourceFile) bool {
	return !astnav.FindChildOfKind(containingNode, kind, sourceFile).IsNil()
}
