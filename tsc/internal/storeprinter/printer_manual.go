package storeprinter

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// The P2 declarations of printer/printer.go and printer/namegenerator.go:
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4.

// emitConciseBody sets the range of the parenthesized expression it makes
// through the context's Factory (R8: the Printer holds no NodeFactory field).
//
// storetranslate:pointer-hash 0dd156f0456bcc99 printer.go:Printer.emitConciseBody
func (p *Printer) emitConciseBody(node store.Node) {
	switch {
	case store.IsBlock(node):
		p.emitFunctionBody(node.AsBlock())
	case store.IsObjectLiteralExpression(store.GetLeftmostExpression(node, false /*stopAtCallExpressions*/)):
		// Wrap in ParenthesizedExpression to ensure parens are emitted after any leading
		// PartiallyEmittedExpression comments, matching TypeScript's factory-time wrapping
		// via parenthesizeConciseBodyOfArrowFunction.
		paren := p.emitContext.Factory.NewParenthesizedExpression(node)
		p.emitContext.Factory.SetRange(paren, node.Pos(), node.End())
		p.emitExpression(paren, ast.OperatorPrecedenceLowest)
	case store.IsExpression(node):
		p.emitExpression(node, ast.OperatorPrecedenceYield)
	default:
		panic(fmt.Sprintf("unexpected ConciseBody: %v", node.Kind()))
	}
}

// emitPropertyAccessExpression sets the range of the dot token it makes
// through the context's Factory (R8).
//
// storetranslate:pointer-hash 384b421c7fabf106 printer.go:Printer.emitPropertyAccessExpression
func (p *Printer) emitPropertyAccessExpression(node store.PropertyAccessExpression) {
	state := p.enterNode(store.Node(node))
	p.emitExpression(node.Expression(), core.IfElse(store.IsOptionalChain(store.Node(node)), ast.OperatorPrecedenceOptionalChain, ast.OperatorPrecedenceMember))
	token := node.QuestionDotToken()
	if token.IsNil() {
		token = p.emitContext.Factory.NewToken(ast.KindDotToken)
		p.emitContext.Factory.SetRange(token, node.Expression().End(), node.Name().Pos())
		p.emitContext.AddEmitFlags(token, EFNoSourceMap)
	}
	linesBeforeDot := p.getLinesBetweenNodes(store.Node(node), node.Expression(), token)
	p.writeLineRepeat(linesBeforeDot)
	p.increaseIndentIf(linesBeforeDot > 0)
	shouldEmitDotDot := token.Kind() != ast.KindQuestionDotToken &&
		p.mayNeedDotDotForPropertyAccess(node.Expression()) &&
		!p.writer.HasTrailingComment() &&
		!p.writer.HasTrailingWhitespace()
	if shouldEmitDotDot {
		p.writePunctuation(".")
	}
	if !node.QuestionDotToken().IsNil() {
		p.emitTokenNode(token)
	} else {
		p.emitToken(ast.KindDotToken, int(node.Expression().End()), WriteKindPunctuation, store.Node(node))
	}
	linesAfterDot := p.getLinesBetweenNodes(store.Node(node), token, node.Name())
	p.writeLineRepeat(linesAfterDot)
	p.increaseIndentIf(linesAfterDot > 0)
	p.emitMemberName(node.Name())
	p.decreaseIndentIf(linesAfterDot > 0)
	p.decreaseIndentIf(linesBeforeDot > 0)
	p.exitNode(store.Node(node), state)
}

// parenthesizeExpressionForNoAsi sets the range of the parentheses it makes
// through the context's Factory (R8).
//
// storetranslate:pointer-hash 771dffca40b6d932 printer.go:Printer.parenthesizeExpressionForNoAsi
func (p *Printer) parenthesizeExpressionForNoAsi(node store.Node) store.Node {
	if !p.commentsDisabled {
		switch node.Kind() {
		case ast.KindPartiallyEmittedExpression:
			if p.willEmitLeadingNewLine(node) {
				pee := node.AsPartiallyEmittedExpression()
				parseNode := p.emitContext.ParseNode(node)
				if !parseNode.IsNil() && store.IsParenthesizedExpression(parseNode) {
					// If the original node was a parenthesized expression, restore it to preserve comment and source map emit
					parens := p.emitContext.Factory.NewParenthesizedExpression(pee.Expression())
					p.emitContext.SetOriginal(parens, node)
					p.emitContext.Factory.SetRange(parens, parseNode.Pos(), parseNode.End())
					return parens
				}
				return p.emitContext.Factory.NewParenthesizedExpression(node)
			}
			pee := node.AsPartiallyEmittedExpression()
			return p.emitContext.Factory.UpdatePartiallyEmittedExpression(
				pee,
				p.parenthesizeExpressionForNoAsi(pee.Expression()),
			)
		case ast.KindPropertyAccessExpression:
			pae := node.AsPropertyAccessExpression()
			return p.emitContext.Factory.UpdatePropertyAccessExpression(
				pae,
				p.parenthesizeExpressionForNoAsi(pae.Expression()),
				pae.QuestionDotToken(),
				pae.Name(),
				store.Node(pae).Flags(),
			)
		case ast.KindElementAccessExpression:
			eae := node.AsElementAccessExpression()
			return p.emitContext.Factory.UpdateElementAccessExpression(
				eae,
				p.parenthesizeExpressionForNoAsi(eae.Expression()),
				eae.QuestionDotToken(),
				eae.ArgumentExpression(),
				store.Node(eae).Flags(),
			)
		case ast.KindCallExpression:
			ce := node.AsCallExpression()
			return p.emitContext.Factory.UpdateCallExpression(
				ce,
				p.parenthesizeExpressionForNoAsi(ce.Expression()),
				ce.QuestionDotToken(),
				ce.TypeArguments(),
				ce.Arguments(),
				store.Node(ce).Flags(),
			)
		case ast.KindTaggedTemplateExpression:
			tte := node.AsTaggedTemplateExpression()
			return p.emitContext.Factory.UpdateTaggedTemplateExpression(
				tte,
				p.parenthesizeExpressionForNoAsi(tte.Tag()),
				tte.QuestionDotToken(),
				tte.TypeArguments(),
				tte.Template(),
				store.Node(tte).Flags(),
			)
		case ast.KindPostfixUnaryExpression:
			pue := node.AsPostfixUnaryExpression()
			return p.emitContext.Factory.UpdatePostfixUnaryExpression(
				pue,
				p.parenthesizeExpressionForNoAsi(pue.Operand()),
				pue.Operator(),
			)
		case ast.KindBinaryExpression:
			be := node.AsBinaryExpression()
			return p.emitContext.Factory.UpdateBinaryExpression(
				be,
				be.Modifiers(),
				p.parenthesizeExpressionForNoAsi(be.Left()),
				be.Type(),
				be.OperatorToken(),
				be.Right(),
			)
		case ast.KindConditionalExpression:
			ce := node.AsConditionalExpression()
			return p.emitContext.Factory.UpdateConditionalExpression(
				ce,
				p.parenthesizeExpressionForNoAsi(ce.Condition()),
				ce.QuestionToken(),
				ce.WhenTrue(),
				ce.ColonToken(),
				ce.WhenFalse(),
			)
		case ast.KindAsExpression:
			ae := node.AsAsExpression()
			return p.emitContext.Factory.UpdateAsExpression(
				ae,
				p.parenthesizeExpressionForNoAsi(ae.Expression()),
				ae.Type(),
			)
		case ast.KindSatisfiesExpression:
			se := node.AsSatisfiesExpression()
			return p.emitContext.Factory.UpdateSatisfiesExpression(
				se,
				p.parenthesizeExpressionForNoAsi(se.Expression()),
				se.Type(),
			)
		case ast.KindNonNullExpression:
			nne := node.AsNonNullExpression()
			return p.emitContext.Factory.UpdateNonNullExpression(
				nne,
				p.parenthesizeExpressionForNoAsi(nne.Expression()),
				store.Node(nne).Flags(),
			)
		}
	}
	return node
}

// emitSignature reads the members of FunctionLikeData on the node: a lone
// FunctionLikeData() has no Store form (4.1).
//
// storetranslate:pointer-hash 85c60e01e0b3fd33 printer.go:Printer.emitSignature
func (p *Printer) emitSignature(node store.Node) {
	n := node

	// !!! In old emitter, quickinfo used type arguments in place of type parameters on instantiated signatures
	////if n.TypeArguments != nil {
	////	p.emitTypeArguments(node, n.TypeArguments)
	////} else {
	p.emitTypeParameters(node, n.TypeParameters())
	////}

	p.emitParameters(node, n.Parameters())
	p.emitTypeAnnotation(n.Type())
}

// nextContainer reads the binder's chain through Node.NextContainer: a lone
// LocalsContainerData() has no Store form (4.1).
//
// storetranslate:pointer-hash bcde09b66a3f4855 namegenerator.go:nextContainer
func nextContainer(node store.Node) store.Node {
	if node.HasLocalsContainerData() {
		return node.NextContainer()
	}
	return store.Node{}
}

// isUniqueLocalName tests LocalsContainerData() != nil with
// HasLocalsContainerData (4.1).
//
// storetranslate:pointer-hash 8a84fc079eba14b2 namegenerator.go:isUniqueLocalName
func isUniqueLocalName(name string, container store.Node) bool {
	node := container
	for !node.IsNil() && store.IsNodeDescendantOf(node, container) && node.HasLocalsContainerData() {
		locals := node.Locals()
		if locals != nil {
			// We conservatively include alias symbols to cover cases where they're emitted as locals
			if local, ok := locals[name]; ok && local.Flags&(ast.SymbolFlagsValue|ast.SymbolFlagsExportValue|ast.SymbolFlagsAlias) != 0 {
				return false
			}
		}
		node = nextContainer(node)
	}
	return true
}
