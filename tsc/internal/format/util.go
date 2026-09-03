package format

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
)

func rangeIsOnOneLine(node core.TextRange, file *ast.SourceFile) bool {
	startLine := scanner.GetECMALineOfPosition(file, node.Pos())
	endLine := scanner.GetECMALineOfPosition(file, node.End())
	return startLine == endLine
}
func getOpenTokenForList(node ast.Handle, list ast.ListRef) ast.Kind {
	switch node.Kind {
	case ast.KindConstructor, ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindArrowFunction, ast.KindCallSignature, ast.KindConstructSignature, ast.KindFunctionType, ast.KindConstructorType, ast.KindGetAccessor, ast.KindSetAccessor:
		if node.TypeParameterList() == list {
			return ast.KindLessThanToken
		} else if node.ParameterList() == list {
			return ast.KindOpenParenToken
		}
	case ast.KindCallExpression, ast.KindNewExpression:
		if node.TypeArgumentList() == list {
			return ast.KindLessThanToken
		} else if node.ArgumentList() == list {
			return ast.KindOpenParenToken
		}
	case ast.KindClassDeclaration, ast.KindClassExpression, ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration:
		if node.TypeParameterList() == list {
			return ast.KindLessThanToken
		}
	case ast.KindTypeReference, ast.KindTaggedTemplateExpression, ast.KindTypeQuery, ast.KindExpressionWithTypeArguments, ast.KindImportType:
		if node.TypeArgumentList() == list {
			return ast.KindLessThanToken
		}
	case ast.KindTypeLiteral:
		return ast.KindOpenBraceToken
	}
	return ast.KindUnknown
}
func getCloseTokenForOpenToken(kind ast.Kind) ast.Kind {
	switch kind {
	case ast.KindOpenParenToken:
		return ast.KindCloseParenToken
	case ast.KindLessThanToken:
		return ast.KindGreaterThanToken
	case ast.KindOpenBraceToken:
		return ast.KindCloseBraceToken
	}
	return ast.KindUnknown
}
func GetLineStartPositionForPosition(position int, sourceFile *ast.SourceFile) int {
	lineStarts := scanner.GetECMALineStarts(sourceFile)
	line := scanner.GetECMALineOfPosition(sourceFile, position)
	return int(lineStarts[line])
}

func findImmediatelyPrecedingTokenOfKind(end int, expectedTokenKind ast.Kind, sourceFile *ast.SourceFile) ast.Handle {
	precedingToken := astnav.FindPrecedingToken(sourceFile, end)
	if precedingToken.IsNil() || precedingToken.Kind != expectedTokenKind || precedingToken.End() != end {
		return ast.Handle{}
	}
	return precedingToken
}

func findOutermostNodeWithinListLevel(node ast.Handle) ast.Handle {
	current := node
	for !current.IsNil() && !current.Parent().IsNil() && current.Parent().End() == node.End() && !isListElement(current.Parent(), current) {
		current = current.Parent()
	}
	return current
}

func isListElement(parent ast.Handle, node ast.Handle) bool {
	switch parent.Kind {
	case ast.KindClassDeclaration, ast.KindInterfaceDeclaration:
		return node.Loc().ContainedBy(parent.Store().ListLoc(parent.MemberList()))
	case ast.KindModuleDeclaration:
		body := parent.Body()
		return !body.IsNil() && body.Kind == ast.KindModuleBlock && node.Loc().ContainedBy(body.Store().ListLoc(body.StatementList()))
	case ast.KindSourceFile, ast.KindBlock, ast.KindModuleBlock:
		return node.Loc().ContainedBy(parent.Store().ListLoc(parent.StatementList()))
	case ast.KindCatchClause:
		block := parent.CatchClauseBlock()
		return node.Loc().ContainedBy(block.Store().ListLoc(block.StatementList()))
	}
	return false
}
func isMemberListElement(parent ast.Handle, node ast.Handle) bool {
	switch parent.Kind {
	case ast.KindClassDeclaration, ast.KindClassExpression, ast.KindInterfaceDeclaration, ast.KindEnumDeclaration, ast.KindTypeLiteral, ast.KindMappedType:
		return node.Loc().ContainedBy(parent.Store().ListLoc(parent.MemberList()))
	}
	return false
}
