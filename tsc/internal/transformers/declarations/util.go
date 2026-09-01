package declarations

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
)

func needsScopeMarker(result ast.Handle) bool {
	return !ast.IsAnyImportOrReExport(result) && !ast.IsExportAssignment(result) && !ast.HasSyntacticModifier(result, ast.ModifierFlagsExport) && !ast.IsAmbientModule(result)
}
func canHaveLiteralInitializer(host DeclarationEmitHost, node ast.Handle) bool {
	switch node.Kind() {
	case ast.KindPropertyDeclaration, ast.KindPropertySignature:
		return host.GetEffectiveDeclarationFlags(node, ast.ModifierFlagsPrivate) == 0
	case ast.KindParameter, ast.KindVariableDeclaration:
		return true
	}
	return false
}
func canProduceDiagnostics(node ast.Handle) bool {
	return ast.IsVariableDeclaration(node) || ast.IsPropertyDeclaration(node) || ast.IsPropertySignatureDeclaration(node) || ast.IsBindingElement(node) || ast.IsSetAccessorDeclaration(node) || ast.IsGetAccessorDeclaration(node) || ast.IsConstructSignatureDeclaration(node) || ast.IsCallSignatureDeclaration(node) || ast.IsMethodDeclaration(node) || ast.IsMethodSignatureDeclaration(node) || ast.IsFunctionDeclaration(node) || ast.IsParameterDeclaration(node) || ast.IsTypeParameterDeclaration(node) || ast.IsExpressionWithTypeArguments(node) || ast.IsImportEqualsDeclaration(node) || ast.IsTypeAliasDeclaration(node) || ast.IsJSTypeAliasDeclaration(node) || ast.IsConstructorDeclaration(node) || ast.IsIndexSignatureDeclaration(node) || ast.IsPropertyAccessExpression(node) || ast.IsElementAccessExpression(node) || ast.IsBinaryExpression(node) || ast.IsCallExpression(node)
}
func canReuseModifierNodes(nodes []ast.Handle) bool {
	for _, node := range nodes {
		if ast.IsModifier(node) && node.Flags()&ast.NodeFlagsReparsed != 0 {
			return false
		}
	}
	return true
}
func isDeclarationAndNotVisible(emitContext *printer.EmitContext, resolver printer.EmitResolver, node ast.Handle) bool {
	node = emitContext.ParseNode(node)
	switch node.Kind() {
	case ast.KindFunctionDeclaration, ast.KindModuleDeclaration, ast.KindInterfaceDeclaration, ast.KindClassDeclaration, ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration, ast.KindEnumDeclaration:
		return !resolver.IsDeclarationVisible(node)
	case ast.KindVariableDeclaration:
		return !getBindingNameVisible(resolver, node)
	case ast.KindImportEqualsDeclaration, ast.KindImportDeclaration, ast.KindJSImportDeclaration, ast.KindExportDeclaration, ast.KindExportAssignment:
		return false
	case ast.KindClassStaticBlockDeclaration:
		return true
	}
	return false
}
func getBindingNameVisible(resolver printer.EmitResolver, elem ast.Handle) bool {
	if ast.IsOmittedExpression(elem) {
		return false
	}
	if elem.Name().IsNil() {
		return false
	}
	if ast.IsBindingPattern(elem.Name()) {
		for _, elem := range elem.Name().Elements() {
			if getBindingNameVisible(resolver, elem) {
				return true
			}
		}
		return false
	} else {
		return resolver.IsDeclarationVisible(elem)
	}
}
func isEnclosingDeclaration(node ast.Handle) bool {
	return ast.IsSourceFile(node) || ast.IsTypeAliasDeclaration(node) || ast.IsJSTypeAliasDeclaration(node) || ast.IsModuleDeclaration(node) || ast.IsClassDeclaration(node) || ast.IsInterfaceDeclaration(node) || ast.IsFunctionLike(node) || ast.IsIndexSignatureDeclaration(node) || ast.IsMappedTypeNode(node) || ast.IsVariableDeclaration(node)
}
func isAlwaysType(node ast.Handle) bool {
	if node.Kind() == ast.KindInterfaceDeclaration {
		return true
	}
	return false
}
func maskModifierFlags(node ast.Handle, modifierMask ast.ModifierFlags, modifierAdditions ast.ModifierFlags) ast.ModifierFlags {
	flags := (ast.GetCombinedModifierFlags(node) & modifierMask) | modifierAdditions
	if flags&ast.ModifierFlagsDefault != 0 && (flags&ast.ModifierFlagsExport == 0) {
		flags ^= ast.ModifierFlagsExport
	}
	if flags&ast.ModifierFlagsDefault != 0 && flags&ast.ModifierFlagsAmbient != 0 {
		flags ^= ast.ModifierFlagsAmbient
	}
	return flags
}
func unwrapParenthesizedExpression(o ast.Handle) ast.Handle {
	for o.Kind() == ast.KindParenthesizedExpression {
		o = o.Expression()
	}
	return o
}
func isPrivateMethodTypeParameter(host DeclarationEmitHost, node ast.Handle) bool {
	return node.Parent().Kind() == ast.KindMethodDeclaration && host.GetEffectiveDeclarationFlags(node.Parent(), ast.ModifierFlagsPrivate) != 0
}

func shouldEmitFunctionProperties(input ast.Handle) bool {
	if !input.Body().IsNil() {
		return true
	}
	return !ast.EveryDeclaration(input.Symbol(), func(decl ast.Handle) bool {
		return !ast.IsFunctionDeclaration(decl) || decl.FunctionDeclarationBody().IsNil()
	})
}
func getEffectiveBaseTypeNode(node ast.Handle) ast.Handle {
	baseType := ast.GetClassExtendsHeritageElement(node)
	return baseType
}
func isScopeMarker(node ast.Handle) bool {
	return ast.IsExportAssignment(node) || ast.IsExportDeclaration(node)
}
func hasScopeMarker(store *ast.Store, statements ast.ListRef) bool {
	if store == nil || statements == 0 {
		return false
	}
	return core.Some(store.ListSlice(statements), isScopeMarker)
}
