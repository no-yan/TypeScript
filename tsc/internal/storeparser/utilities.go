package storeparser

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
)

func getLanguageVariant(scriptKind core.ScriptKind) core.LanguageVariant {
	switch scriptKind {
	case core.ScriptKindTSX, core.ScriptKindJSX, core.ScriptKindJS, core.ScriptKindJSON:
		// .tsx and .jsx files are treated as jsx language variant.
		return core.LanguageVariantJSX
	}
	return core.LanguageVariantStandard
}

func tokenIsIdentifierOrKeyword(token ast.Kind) bool {
	return token >= ast.KindIdentifier
}

func tokenIsIdentifierOrKeywordOrGreaterThan(token ast.Kind) bool {
	return token == ast.KindGreaterThanToken || tokenIsIdentifierOrKeyword(token)
}

func isKeywordOrPunctuation(token ast.Kind) bool {
	return ast.IsKeywordKind(token) || ast.IsPunctuationKind(token)
}

// The View versions of what the Pointer parser reads off *ast.Node
// (store-parser-implementation-instructions-20260922.md 4.6). Each one reads
// the scratch through Builder.View and is valid only until the next append.

func (p *Parser) pos(id store.NodeRef) int { return int(p.b.View(id).Pos()) }
func (p *Parser) end(id store.NodeRef) int { return int(p.b.View(id).End()) }

func (p *Parser) loc(id store.NodeRef) core.TextRange {
	n := p.b.View(id)
	return core.NewTextRange(int(n.Pos()), int(n.End()))
}

func (p *Parser) listLoc(at store.ListRef) core.TextRange {
	l := p.b.ViewList(at)
	return core.NewTextRange(int(l.Pos()), int(l.End()))
}

// Determines if a node is missing (either nil or empty)
func nodeIsMissing(node store.Node) bool {
	return node.IsNil() || node.Pos() == node.End() && node.Pos() >= 0 && node.Kind() != ast.KindEndOfFile
}

// Determines if a node is present
func nodeIsPresent(node store.Node) bool {
	return !nodeIsMissing(node)
}

func tagNamesAreEquivalent(lhs store.Node, rhs store.Node) bool {
	if lhs.Kind() != rhs.Kind() {
		return false
	}
	switch lhs.Kind() {
	case ast.KindIdentifier:
		return lhs.Text() == rhs.Text()
	case ast.KindThisKeyword:
		return true
	case ast.KindJsxNamespacedName:
		return lhs.AsJsxNamespacedName().Namespace().Text() == rhs.AsJsxNamespacedName().Namespace().Text() &&
			lhs.AsJsxNamespacedName().Name().Text() == rhs.AsJsxNamespacedName().Name().Text()
	case ast.KindPropertyAccessExpression:
		return lhs.AsPropertyAccessExpression().Name().Text() == rhs.AsPropertyAccessExpression().Name().Text() &&
			tagNamesAreEquivalent(lhs.Expression(), rhs.Expression())
	}
	panic("Unhandled case in tagNamesAreEquivalent")
}

func isOptionalChain(node store.Node) bool {
	if node.Flags()&ast.NodeFlagsOptionalChain != 0 {
		switch node.Kind() {
		case ast.KindPropertyAccessExpression,
			ast.KindElementAccessExpression,
			ast.KindCallExpression,
			ast.KindNonNullExpression:
			return true
		}
	}
	return false
}

// isLeftHandSideExpression is ast.IsLeftHandSideExpression by kind: the
// parser never makes a PartiallyEmittedExpression, so there is nothing to skip.
func isLeftHandSideExpression(kind ast.Kind) bool {
	switch kind {
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression, ast.KindNewExpression, ast.KindCallExpression,
		ast.KindJsxElement, ast.KindJsxSelfClosingElement, ast.KindJsxFragment, ast.KindTaggedTemplateExpression, ast.KindArrayLiteralExpression,
		ast.KindParenthesizedExpression, ast.KindObjectLiteralExpression, ast.KindClassExpression, ast.KindFunctionExpression, ast.KindIdentifier,
		ast.KindPrivateIdentifier, ast.KindRegularExpressionLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindStringLiteral,
		ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateExpression, ast.KindFalseKeyword, ast.KindNullKeyword, ast.KindThisKeyword,
		ast.KindTrueKeyword, ast.KindSuperKeyword, ast.KindNonNullExpression, ast.KindExpressionWithTypeArguments, ast.KindMetaProperty,
		ast.KindImportKeyword, ast.KindMissingDeclaration:
		return true
	}
	return false
}

// getTextOfNodeFromSourceText is scanner.GetTextOfNodeFromSourceText without
// the JSDoc and reparse cases, which the parser never makes.
func getTextOfNodeFromSourceText(sourceText string, node store.Node, includeTrivia bool) string {
	if nodeIsMissing(node) {
		return ""
	}
	pos := int(node.Pos())
	if !includeTrivia {
		pos = scanner.SkipTrivia(sourceText, pos)
	}
	return sourceText[pos:node.End()]
}

// someModifier is core.Some over the elements of a modifier block.
func (p *Parser) someModifier(at store.ListRef, pred func(store.Node) bool) bool {
	return p.findModifierIndex(at, pred) >= 0
}

// findModifierIndex is core.FindIndex over the elements of a modifier block.
func (p *Parser) findModifierIndex(at store.ListRef, pred func(store.Node) bool) int {
	for i, m := range p.b.ViewList(at).Refs() {
		if pred(p.b.View(m)) {
			return i
		}
	}
	return -1
}

// modifierFlags is ast.ModifiersToFlags over a modifier block; the Pointer
// ModifierList carries this precomputed.
func (p *Parser) modifierFlags(at store.ListRef) ast.ModifierFlags {
	var flags ast.ModifierFlags
	for _, m := range p.b.ViewList(at).Refs() {
		flags |= ast.ModifierToFlag(p.b.View(m).Kind())
	}
	return flags
}

func isDecorator(node store.Node) bool {
	return node.Kind() == ast.KindDecorator
}

func isModifier(node store.Node) bool {
	return ast.IsModifierKind(node.Kind())
}

func isNotReparsed(node store.Node) bool {
	return node.Flags()&ast.NodeFlagsReparsed == 0
}

// canHaveIllegalDecorators and canHaveDecorators are ast.CanHaveIllegalDecorators
// and ast.CanHaveDecorators by kind.
func canHaveIllegalDecorators(kind ast.Kind) bool {
	switch kind {
	case ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment,
		ast.KindFunctionDeclaration, ast.KindConstructor,
		ast.KindIndexSignature, ast.KindClassStaticBlockDeclaration,
		ast.KindMissingDeclaration, ast.KindVariableStatement,
		ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration,
		ast.KindEnumDeclaration, ast.KindModuleDeclaration,
		ast.KindImportEqualsDeclaration, ast.KindImportDeclaration, ast.KindJSImportDeclaration,
		ast.KindNamespaceExportDeclaration, ast.KindExportDeclaration,
		ast.KindExportAssignment:
		return true
	}
	return false
}

func canHaveDecorators(kind ast.Kind) bool {
	switch kind {
	case ast.KindParameter,
		ast.KindPropertyDeclaration,
		ast.KindMethodDeclaration,
		ast.KindGetAccessor,
		ast.KindSetAccessor,
		ast.KindClassExpression,
		ast.KindClassDeclaration:
		return true
	}
	return false
}
