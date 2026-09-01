package change

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/format"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"slices"
)

func deleteDeclaration(t *Tracker, deletedNodesInLists map[ // deleteDeclaration deletes a node with smart handling for different node types.
// This handles special cases like import specifiers in lists, parameters, etc.
// Lambdas with exactly one parameter are special because, after removal, there
// must be an empty parameter list (i.e. `()`) and this won't necessarily be the
// case if the parameter is simply removed (e.g. in `x => 1`).
// For first import, leave header comment in place, otherwise only delete JSDoc comments
// For type keyword in import clauses, we need to delete the keyword and any trailing space
// The trailing space is part of the next token's leading trivia, so we include it
// a misbehaving client can reach here with the SourceFile node
// Delete the whole import
// import |d,| * as ns from './file'
// shift first non-whitespace position after comma to the start position of the node
// Delete named imports while preserving the default import
// import d|, * as ns| from './file'
// import d|, { a }| from './file'
// Delete the entire import declaration
// |import * as ns from './file'|
// |import { a } from './file'|
// TODO: There's currently no unused diagnostic for this, could be a suggestion
// deleteNode deletes a node with the specified trivia options.
// Warning: This deletes comments too.
// Note: We will only delete a comma *after* a node. This will leave a trailing comma if we delete the last node.
// That's handled in the end by finishTrailingCommaAfterDeletingNodesInList.
// startPositionToDeleteNodeInList finds the first non-whitespace position in the leading trivia of the node
// hasJSDocNodes checks if a node has JSDoc comments
// nil is ok for JSDoc - it will return empty slice if not available
ast.Handle]bool, sourceFile *ast.SourceFile, node ast.Handle) {
	switch node.Kind() {
	case ast.KindParameter:
		oldFunction := node.Parent()
		if oldFunction.Kind() == ast.KindArrowFunction && len(oldFunction.Store().ListSlice(oldFunction.ArrowFunctionParameters())) == 1 && astnav.FindChildOfKind(oldFunction, ast.KindOpenParenToken, sourceFile).IsNil() {
			t.ReplaceRangeWithText(sourceFile, t.GetAdjustedRange(sourceFile, node, node, LeadingTriviaOptionIncludeAll, TrailingTriviaOptionInclude), "()")
		} else {
			deleteNodeInList(t, deletedNodesInLists, sourceFile, node)
		}
	case ast.KindImportDeclaration, ast.KindImportEqualsDeclaration:
		imports := sourceFile.Imports()
		isFirstImport := len(imports) > 0 && node == imports[0].Parent() || node == core.Find(sourceFile.ParseRoot().Statements(), func(s ast.Handle) bool {
			return ast.IsAnyImportSyntax(s)
		})
		leadingTrivia := LeadingTriviaOptionStartLine
		if isFirstImport {
			leadingTrivia = LeadingTriviaOptionExclude
		} else if hasJSDocNodes(node) {
			leadingTrivia = LeadingTriviaOptionJSDoc
		}
		deleteNode(t, sourceFile, node, leadingTrivia, TrailingTriviaOptionInclude)
	case ast.KindBindingElement:
		pattern := node.Parent()
		preserveComma := pattern.Kind() == ast.KindArrayBindingPattern && node != pattern.Store().ListSlice(pattern.BindingPatternElements())[len(pattern.Store().ListSlice(pattern.BindingPatternElements()))-1]
		if preserveComma {
			deleteNode(t, sourceFile, node, LeadingTriviaOptionIncludeAll, TrailingTriviaOptionExclude)
		} else {
			deleteNodeInList(t, deletedNodesInLists, sourceFile, node)
		}
	case ast.KindVariableDeclaration:
		deleteVariableDeclaration(t, deletedNodesInLists, sourceFile, node)
	case ast.KindTypeParameter:
		deleteNodeInList(t, deletedNodesInLists, sourceFile, node)
	case ast.KindImportSpecifier:
		namedImports := node.Parent()
		if len(namedImports.Store().ListSlice(namedImports.NamedImportsElements())) == 1 {
			deleteImportBinding(t, sourceFile, namedImports)
		} else {
			deleteNodeInList(t, deletedNodesInLists, sourceFile, node)
		}
	case ast.KindNamespaceImport:
		deleteImportBinding(t, sourceFile, node)
	case ast.KindSemicolonToken:
		deleteNode(t, sourceFile, node, LeadingTriviaOptionIncludeAll, TrailingTriviaOptionExclude)
	case ast.KindTypeKeyword:
		deleteNode(t, sourceFile, node, LeadingTriviaOptionExclude, TrailingTriviaOptionInclude)
	case ast.KindFunctionKeyword:
		deleteNode(t, sourceFile, node, LeadingTriviaOptionExclude, TrailingTriviaOptionInclude)
	case ast.KindClassDeclaration, ast.KindFunctionDeclaration:
		leadingTrivia := LeadingTriviaOptionStartLine
		if hasJSDocNodes(node) {
			leadingTrivia = LeadingTriviaOptionJSDoc
		}
		deleteNode(t, sourceFile, node, leadingTrivia, TrailingTriviaOptionInclude)
	default:
		if node.Parent().IsNil() {
			deleteNode(t, sourceFile, node, LeadingTriviaOptionIncludeAll, TrailingTriviaOptionInclude)
		} else if node.Parent().Kind() == ast.KindImportClause && node.Parent().ImportClauseName() == node {
			deleteDefaultImport(t, sourceFile, node.Parent())
		} else if node.Parent().Kind() == ast.KindCallExpression && slices.Contains(node.Store().ListSlice(node.Parent().CallExpressionArguments()), node) {
			deleteNodeInList(t, deletedNodesInLists, sourceFile, node)
		} else {
			deleteNode(t, sourceFile, node, LeadingTriviaOptionIncludeAll, TrailingTriviaOptionInclude)
		}
	}
}
func deleteDefaultImport(t *Tracker, sourceFile *ast.SourceFile, importClause ast.Handle) {
	clause := importClause
	if clause.NamedBindings().IsNil() {
		deleteNode(t, sourceFile, importClause.Parent(), LeadingTriviaOptionIncludeAll, TrailingTriviaOptionInclude)
	} else {
		name := clause.Name()
		start := astnav.GetStartOfNode(name, sourceFile, false)
		nextToken := astnav.GetTokenAtPosition(sourceFile, name.End())
		if !nextToken.IsNil() && nextToken.Kind() == ast.KindCommaToken {
			end := scanner.SkipTriviaEx(sourceFile.Text(), nextToken.End(), &scanner.SkipTriviaOptions{StopAfterLineBreak: false, StopAtComments: true})
			t.ReplaceRangeWithText(sourceFile, t.toLSPEditRange(sourceFile, core.NewTextRange(start, end)), "")
		} else {
			deleteNode(t, sourceFile, name, LeadingTriviaOptionIncludeAll, TrailingTriviaOptionInclude)
		}
	}
}
func deleteImportBinding(t *Tracker, sourceFile *ast.SourceFile, node ast.Handle) {
	importClause := node.Parent()
	if !importClause.Name().IsNil() {
		previousToken := astnav.GetTokenAtPosition(sourceFile, node.Pos()-1)
		debug.Assert(!previousToken.IsNil(), "previousToken should not be nil")
		start := astnav.GetStartOfNode(previousToken, sourceFile, false)
		t.ReplaceRangeWithText(sourceFile, t.toLSPEditRange(sourceFile, core.NewTextRange(start, node.End())), "")
	} else {
		importDecl := ast.FindAncestorKind(node, ast.KindImportDeclaration)
		debug.Assert(!importDecl.IsNil(), "importDecl should not be nil")
		deleteNode(t, sourceFile, importDecl, LeadingTriviaOptionIncludeAll, TrailingTriviaOptionInclude)
	}
}
func deleteVariableDeclaration(t *Tracker, deletedNodesInLists map[ast.Handle]bool, sourceFile *ast.SourceFile, node ast.Handle) {
	parent := node.Parent()
	if parent.Kind() == ast.KindCatchClause {
		openParen := astnav.FindChildOfKind(parent, ast.KindOpenParenToken, sourceFile)
		closeParen := astnav.FindChildOfKind(parent, ast.KindCloseParenToken, sourceFile)
		debug.Assert(!openParen.IsNil() && !closeParen.IsNil(), "catch clause should have parens")
		t.DeleteNodeRange(sourceFile, openParen, closeParen, LeadingTriviaOptionIncludeAll, TrailingTriviaOptionInclude)
		return
	}
	if len(parent.Store().ListSlice(parent.VariableDeclarationListDeclarations())) != 1 {
		deleteNodeInList(t, deletedNodesInLists, sourceFile, node)
		return
	}
	gp := parent.Parent()
	switch gp.Kind() {
	case ast.KindForOfStatement, ast.KindForInStatement:
		t.ReplaceNode(sourceFile, node, t.NewObjectLiteralExpression(t.NewList([]ast.Handle{}), false), nil)
	case ast.KindForStatement:
		deleteNode(t, sourceFile, parent, LeadingTriviaOptionIncludeAll, TrailingTriviaOptionInclude)
	case ast.KindVariableStatement:
		leadingTrivia := LeadingTriviaOptionStartLine
		if hasJSDocNodes(gp) {
			leadingTrivia = LeadingTriviaOptionJSDoc
		}
		deleteNode(t, sourceFile, gp, leadingTrivia, TrailingTriviaOptionInclude)
	default:
		debug.Fail("Unexpected grandparent kind: " + gp.Kind().String())
	}
}

func deleteNode(t *Tracker, sourceFile *ast.SourceFile, node ast.Handle, leadingTrivia LeadingTriviaOption, trailingTrivia TrailingTriviaOption) {
	startPosition := t.getAdjustedStartPosition(sourceFile, node, leadingTrivia, false)
	endPosition := t.getAdjustedEndPosition(sourceFile, node, trailingTrivia)
	t.ReplaceRangeWithText(sourceFile, t.toLSPEditRange(sourceFile, core.NewTextRange(startPosition, endPosition)), "")
}
func deleteNodeInList(t *Tracker, deletedNodesInLists map[ast.Handle]bool, sourceFile *ast.SourceFile, node ast.Handle) {
	containingList := format.GetContainingList(node, sourceFile)
	debug.Assert(containingList != 0, "containingList should not be nil")
	index := slices.Index(node.Store().ListSlice(containingList), node)
	debug.Assert(index != -1, "node should be in containing list")
	if node.Store().ListLen(containingList) == 1 {
		deleteNode(t, sourceFile, node, LeadingTriviaOptionIncludeAll, TrailingTriviaOptionInclude)
		return
	}
	debug.Assert(!deletedNodesInLists[node], "Deleting a node twice")
	deletedNodesInLists[node] = true
	startPos := t.startPositionToDeleteNodeInList(sourceFile, node)
	var endPos int
	if index == node.Store().ListLen(containingList)-1 {
		endPos = t.getAdjustedEndPosition(sourceFile, node, TrailingTriviaOptionNone)
	} else {
		prevNode := ast.Handle{}
		if index > 0 {
			prevNode = node.Store().ListAt(containingList, index-1)
		}
		endPos = t.endPositionToDeleteNodeInList(sourceFile, node, prevNode, node.Store().ListAt(containingList, index+1))
	}
	t.ReplaceRangeWithText(sourceFile, t.toLSPEditRange(sourceFile, core.NewTextRange(startPos, endPos)), "")
}

func (t *Tracker) startPositionToDeleteNodeInList(sourceFile *ast.SourceFile, node ast.Handle) int {
	start := t.getAdjustedStartPosition(sourceFile, node, LeadingTriviaOptionIncludeAll, false)
	return scanner.SkipTriviaEx(sourceFile.Text(), start, &scanner.SkipTriviaOptions{StopAfterLineBreak: false, StopAtComments: true})
}
func (t *Tracker) endPositionToDeleteNodeInList(sourceFile *ast.SourceFile, node ast.Handle, prevNode ast.Handle, nextNode ast.Handle) int {
	end := t.startPositionToDeleteNodeInList(sourceFile, nextNode)
	if prevNode.IsNil() || positionsAreOnSameLine(t.getAdjustedEndPosition(sourceFile, node, TrailingTriviaOptionInclude), end, sourceFile) {
		return end
	}
	token := astnav.FindPrecedingToken(sourceFile, astnav.GetStartOfNode(nextNode, sourceFile, false))
	if isSeparator(node, token) {
		prevToken := astnav.FindPrecedingToken(sourceFile, astnav.GetStartOfNode(node, sourceFile, false))
		if isSeparator(prevNode, prevToken) {
			pos := scanner.SkipTriviaEx(sourceFile.Text(), token.End(), &scanner.SkipTriviaOptions{StopAfterLineBreak: true, StopAtComments: true})
			if positionsAreOnSameLine(astnav.GetStartOfNode(prevToken, sourceFile, false), astnav.GetStartOfNode(token, sourceFile, false), sourceFile) {
				if pos > 0 && stringutil.IsLineBreak(rune(sourceFile.Text()[pos-1])) {
					return pos - 1
				}
				return pos
			}
			if stringutil.IsLineBreak(rune(sourceFile.Text()[pos])) {
				return pos
			}
		}
	}
	return end
}
func positionsAreOnSameLine(pos1, pos2 int, sourceFile *ast.SourceFile) bool {
	return format.GetLineStartPositionForPosition(pos1, sourceFile) == format.GetLineStartPositionForPosition(pos2, sourceFile)
}

func hasJSDocNodes(node ast.Handle) bool {
	if node.IsNil() {
		return false
	}
	jsdocs := node.JSDoc(nil)
	return len(jsdocs) > 0
}
