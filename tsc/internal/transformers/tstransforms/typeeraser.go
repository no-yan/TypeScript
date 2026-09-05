package tstransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
	"slices"
)

type TypeEraserTransformer struct {
	transformers.Transformer
	compilerOptions *core.CompilerOptions
	parentNode      ast.Handle
	currentNode     ast.Handle
}

func NewTypeEraserTransformer(opt *transformers.TransformOptions) *transformers.Transformer {
	compilerOptions := opt.CompilerOptions
	emitContext := opt.Context
	tx := &TypeEraserTransformer{compilerOptions: compilerOptions}
	return tx.NewTransformer(tx.visit, emitContext)
}

func (tx *TypeEraserTransformer) pushNode(node ast.Handle) (grandparentNode ast.Handle) {
	grandparentNode = tx.parentNode
	tx.parentNode = tx.currentNode
	tx.currentNode = node
	return grandparentNode
}

func (tx *TypeEraserTransformer) popNode(grandparentNode ast.Handle) {
	tx.currentNode = tx.parentNode
	tx.parentNode = grandparentNode
}
func (tx *TypeEraserTransformer) elide(node ast.Handle) ast.Handle {
	return tx.EmitContext().NewNotEmittedStatement(node)
}
func (tx *TypeEraserTransformer) visit(node ast.Handle) ast.Handle {
	if node.SubtreeFacts()&ast.SubtreeContainsTypeScript == 0 {
		return node
	}
	if ast.IsStatement(node) && ast.HasSyntacticModifier(node, ast.ModifierFlagsAmbient) {
		return tx.elide(node)
	}
	grandparentNode := tx.pushNode(node)
	defer tx.popNode(grandparentNode)
	switch node.Kind {
	case ast.KindPublicKeyword, ast.KindPrivateKeyword, ast.KindProtectedKeyword, ast.KindAbstractKeyword, ast.KindOverrideKeyword, ast.KindConstKeyword, ast.KindDeclareKeyword, ast.KindReadonlyKeyword, ast.KindArrayType, ast.KindTupleType, ast.KindOptionalType, ast.KindRestType, ast.KindTypeLiteral, ast.KindTypePredicate, ast.KindTypeParameter, ast.KindAnyKeyword, ast.KindUnknownKeyword, ast.KindBooleanKeyword, ast.KindStringKeyword, ast.KindNumberKeyword, ast.KindNeverKeyword, ast.KindVoidKeyword, ast.KindSymbolKeyword, ast.KindConstructorType, ast.KindFunctionType, ast.KindTypeQuery, ast.KindTypeReference, ast.KindUnionType, ast.KindIntersectionType, ast.KindConditionalType, ast.KindParenthesizedType, ast.KindThisType, ast.KindTypeOperator, ast.KindIndexedAccessType, ast.KindMappedType, ast.KindLiteralType, ast.KindIndexSignature:
		return ast.Handle{}
	case ast.KindInKeyword, ast.KindOutKeyword:
		if tx.parentNode.IsNil() || !ast.IsBinaryExpression(tx.parentNode) {
			return ast.Handle{}
		}
		return tx.Visitor().VisitEachChild(node)
	case ast.KindJSImportDeclaration:
		return ast.Handle{}
	case ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration, ast.KindInterfaceDeclaration:
		return tx.elide(node)
	case ast.KindNamespaceExportDeclaration:
		return ast.Handle{}
	case ast.KindModuleDeclaration:
		if !ast.IsIdentifier(node.Name()) || !ast.IsInstantiatedModule(node, tx.compilerOptions.ShouldPreserveConstEnums()) || getInnermostModuleDeclarationFromDottedModule(node).Body().IsNil() {
			return tx.elide(node)
		}
		return tx.Visitor().VisitEachChild(node)
	case ast.KindExpressionWithTypeArguments:
		n := node
		return tx.Factory().UpdateExpressionWithTypeArguments(n, tx.Visitor().VisitNode(n.Expression()), 0)
	case ast.KindPropertyDeclaration:
		if tx.compilerOptions.ExperimentalDecorators.IsTrue() && ast.HasSyntacticModifier(node, ast.ModifierFlagsAmbient|ast.ModifierFlagsAbstract) && ast.HasDecorators(node) {
			n := node
			return tx.Factory().UpdatePropertyDeclaration(n, tx.Visitor().VisitModifiers(n.Modifiers()), tx.Visitor().VisitNode(n.Name()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(n.Initializer()))
		}
		if ast.HasSyntacticModifier(node, ast.ModifierFlagsAmbient|ast.ModifierFlagsAbstract) {
			return ast.Handle{}
		}
		n := node
		return tx.Factory().UpdatePropertyDeclaration(n, tx.Visitor().VisitModifiers(n.Modifiers()), tx.Visitor().VisitNode(n.Name()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(n.Initializer()))
	case ast.KindConstructor:
		n := node
		if ast.NodeIsMissing(n.Body()) {
			return ast.Handle{}
		}
		return tx.Factory().UpdateConstructorDeclaration(n, 0, 0, tx.Visitor().VisitNodes(n.ParameterList()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(n.Body()))
	case ast.KindMethodDeclaration:
		n := node
		if ast.NodeIsMissing(n.Body()) {
			return ast.Handle{}
		}
		return tx.Factory().UpdateMethodDeclaration(n, tx.Visitor().VisitModifiers(n.Modifiers()), n.AsteriskToken(), tx.Visitor().VisitNode(n.Name()), ast.Handle{}, 0, tx.Visitor().VisitNodes(n.ParameterList()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(n.Body()))
	case ast.KindGetAccessor:
		n := node
		if ast.NodeIsMissing(n.Body()) && ast.HasSyntacticModifier(node, ast.ModifierFlagsAbstract) {
			return ast.Handle{}
		}
		body := tx.Visitor().VisitNode(n.Body())
		if body.IsNil() {
			body = tx.Factory().NewBlock(tx.Factory().NewList(nil), false)
		}
		return tx.Factory().UpdateGetAccessorDeclaration(n, tx.Visitor().VisitModifiers(n.Modifiers()), tx.Visitor().VisitNode(n.Name()), 0, tx.Visitor().VisitNodes(n.ParameterList()), ast.Handle{}, ast.Handle{}, body)
	case ast.KindSetAccessor:
		n := node
		if ast.NodeIsMissing(n.Body()) && ast.HasSyntacticModifier(node, ast.ModifierFlagsAbstract) {
			return ast.Handle{}
		}
		body := tx.Visitor().VisitNode(n.Body())
		if body.IsNil() {
			body = tx.Factory().NewBlock(tx.Factory().NewList(nil), false)
		}
		return tx.Factory().UpdateSetAccessorDeclaration(n, tx.Visitor().VisitModifiers(n.Modifiers()), tx.Visitor().VisitNode(n.Name()), 0, tx.Visitor().VisitNodes(n.ParameterList()), ast.Handle{}, ast.Handle{}, body)
	case ast.KindVariableDeclaration:
		n := node
		updated := tx.Factory().UpdateVariableDeclaration(n, tx.Visitor().VisitNode(n.Name()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(n.Initializer()))
		if !n.Type().IsNil() {
			tx.EmitContext().SetTypeNode(updated.VariableDeclarationName(), n.Type())
		}
		return updated
	case ast.KindHeritageClause:
		n := node
		if n.HeritageClauseToken() == ast.KindImplementsKeyword {
			return ast.Handle{}
		}
		return tx.Factory().UpdateHeritageClause(n, n.HeritageClauseToken(), tx.Visitor().VisitNodes(n.TypeList()))
	case ast.KindClassDeclaration:
		n := node
		return tx.Factory().UpdateClassDeclaration(n, tx.Visitor().VisitModifiers(n.Modifiers()), tx.Visitor().VisitNode(n.Name()), 0, tx.Visitor().VisitNodes(n.HeritageClauses()), tx.Visitor().VisitNodes(n.MemberList()))
	case ast.KindClassExpression:
		n := node
		return tx.Factory().UpdateClassExpression(n, tx.Visitor().VisitModifiers(n.Modifiers()), tx.Visitor().VisitNode(n.Name()), 0, tx.Visitor().VisitNodes(n.HeritageClauses()), tx.Visitor().VisitNodes(n.MemberList()))
	case ast.KindFunctionDeclaration:
		n := node
		if ast.NodeIsMissing(n.Body()) {
			return tx.elide(node)
		}
		return tx.Factory().UpdateFunctionDeclaration(n, tx.Visitor().VisitModifiers(n.Modifiers()), n.AsteriskToken(), tx.Visitor().VisitNode(n.Name()), 0, tx.Visitor().VisitNodes(n.ParameterList()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(n.Body()))
	case ast.KindFunctionExpression:
		n := node
		return tx.Factory().UpdateFunctionExpression(n, tx.Visitor().VisitModifiers(n.Modifiers()), n.AsteriskToken(), tx.Visitor().VisitNode(n.Name()), 0, tx.Visitor().VisitNodes(n.ParameterList()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(n.Body()))
	case ast.KindArrowFunction:
		n := node
		return tx.Factory().UpdateArrowFunction(n, tx.Visitor().VisitModifiers(n.Modifiers()), 0, tx.Visitor().VisitNodes(n.ParameterList()), ast.Handle{}, ast.Handle{}, n.EqualsGreaterThanToken(), tx.Visitor().VisitNode(n.Body()))
	case ast.KindParameter:
		if ast.IsThisParameter(node) {
			return ast.Handle{}
		}
		n := node
		var modifiers ast.ListRef
		if ast.IsParameterPropertyDeclaration(node, tx.parentNode) {
			modifiers = transformers.ExtractModifiers(tx.EmitContext(), n.Modifiers(), ast.ModifierFlagsParameterPropertyModifier)
		}
		if ast.HasDecorators(node) {
			decorators := node.Decorators()
			visited := tx.Visitor().VisitSlice(decorators)
			if modifiers == 0 {
				modifiers = tx.Factory().NewModifierList(visited)
			} else {
				modifiers = tx.Factory().NewModifierList(slices.Concat(node.Store().ListSlice(modifiers).Slice(), visited))
			}
		}
		return tx.Factory().UpdateParameterDeclaration(n, modifiers, n.DotDotDotToken(), tx.Visitor().VisitNode(n.Name()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(n.Initializer()))
	case ast.KindCallExpression:
		n := node
		return tx.Factory().UpdateCallExpression(n, tx.Visitor().VisitNode(n.Expression()), n.QuestionDotToken(), 0, tx.Visitor().VisitNodes(n.ArgumentList()), n.Flags())
	case ast.KindNewExpression:
		n := node
		return tx.Factory().UpdateNewExpression(n, tx.Visitor().VisitNode(n.Expression()), 0, tx.Visitor().VisitNodes(n.ArgumentList()))
	case ast.KindTaggedTemplateExpression:
		n := node
		return tx.Factory().UpdateTaggedTemplateExpression(n, tx.Visitor().VisitNode(n.Tag()), n.QuestionDotToken(), 0, tx.Visitor().VisitNode(n.Template()), n.Flags())
	case ast.KindNonNullExpression, ast.KindTypeAssertionExpression, ast.KindAsExpression, ast.KindSatisfiesExpression:
		partial := tx.Factory().NewPartiallyEmittedExpression(tx.Visitor().VisitNode(node.Expression()))
		tx.EmitContext().SetOriginal(partial, node)
		partial.SetLoc(node.Loc())
		return partial
	case ast.KindParenthesizedExpression:
		if !ast.IsJSDocTypeAssertion(node) {
			n := node
			expression := ast.SkipOuterExpressions(n.Expression(), ast.OEKAllExceptAssertionsOrExpressionsWithTypeArguments)
			if ast.IsAssertionExpression(expression) || ast.IsSatisfiesExpression(expression) {
				partial := tx.Factory().NewPartiallyEmittedExpression(tx.Visitor().VisitNode(n.Expression()))
				tx.EmitContext().SetOriginal(partial, node)
				partial.SetLoc(node.Loc())
				return partial
			}
		}
		return tx.Visitor().VisitEachChild(node)
	case ast.KindJsxSelfClosingElement:
		n := node
		return tx.Factory().UpdateJsxSelfClosingElement(n, tx.Visitor().VisitNode(n.TagName()), 0, tx.Visitor().VisitNode(n.Attributes()))
	case ast.KindJsxOpeningElement:
		n := node
		return tx.Factory().UpdateJsxOpeningElement(n, tx.Visitor().VisitNode(n.TagName()), 0, tx.Visitor().VisitNode(n.Attributes()))
	case ast.KindImportEqualsDeclaration:
		n := node
		if n.IsTypeOnly() {
			return ast.Handle{}
		}
		return tx.Visitor().VisitEachChild(node)
	case ast.KindImportDeclaration:
		n := node
		if n.ImportClause().IsNil() {
			return node
		}
		importClause := tx.Visitor().VisitNode(n.ImportClause())
		if importClause.IsNil() {
			return ast.Handle{}
		}
		return tx.Factory().UpdateImportDeclaration(n, n.Modifiers(), importClause, n.ModuleSpecifier(), n.Attributes())
	case ast.KindImportClause:
		n := node
		if n.IsTypeOnly() {
			return ast.Handle{}
		}
		name := n.Name()
		namedBindings := tx.Visitor().VisitNode(n.ImportClauseNamedBindings())
		if name.IsNil() && namedBindings.IsNil() {
			return ast.Handle{}
		}
		return tx.Factory().UpdateImportClause(n, n.ImportClausePhaseModifier(), name, namedBindings)
	case ast.KindNamedImports:
		n := node
		if len(n.Elements()) == 0 {
			return node
		}
		elements := tx.Visitor().VisitNodes(n.ElementList())
		if !tx.compilerOptions.VerbatimModuleSyntax.IsTrue() && node.Store().ListLen(elements) == 0 {
			return ast.Handle{}
		}
		return tx.Factory().UpdateNamedImports(n, elements)
	case ast.KindImportSpecifier:
		n := node
		if n.IsTypeOnly() {
			return ast.Handle{}
		}
		return node
	case ast.KindExportDeclaration:
		n := node
		if n.IsTypeOnly() {
			return ast.Handle{}
		}
		var exportClause ast.Handle
		if !n.ExportClause().IsNil() {
			exportClause = tx.Visitor().VisitNode(n.ExportClause())
			if exportClause.IsNil() {
				return ast.Handle{}
			}
		}
		return tx.Factory().UpdateExportDeclaration(n, 0, false, exportClause, tx.Visitor().VisitNode(n.ModuleSpecifier()), tx.Visitor().VisitNode(n.Attributes()))
	case ast.KindNamedExports:
		n := node
		if len(n.Elements()) == 0 {
			return node
		}
		elements := tx.Visitor().VisitNodes(n.ElementList())
		if !tx.compilerOptions.VerbatimModuleSyntax.IsTrue() && node.Store().ListLen(elements) == 0 {
			return ast.Handle{}
		}
		return tx.Factory().UpdateNamedExports(n, elements)
	case ast.KindExportSpecifier:
		n := node
		if n.IsTypeOnly() {
			return ast.Handle{}
		}
		return node
	case ast.KindEnumDeclaration:
		if ast.IsEnumConst(node) {
			return node
		}
		return tx.Visitor().VisitEachChild(node)
	default:
		return tx.Visitor().VisitEachChild(node)
	}
}
