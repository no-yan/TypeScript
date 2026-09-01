package checker

import (
	"fmt"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/jsnum"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"strings"
)

func (c *Checker) grammarErrorOnFirstToken(node ast.Handle, message *diagnostics.Message, args ...any) bool {
	sourceFile := ast.GetSourceFileOfNode(node)
	if !c.hasParseDiagnostics(sourceFile) {
		span := scanner.GetRangeOfTokenAtPosition(sourceFile, node.Pos())
		c.addDiagnostic(ast.NewDiagnostic(sourceFile, span, message, args...))
		return true
	}
	return false
}
func (c *Checker) grammarErrorAtPos(nodeForSourceFile ast.Handle, start int, length int, message *diagnostics.Message, args ...any) bool {
	sourceFile := ast.GetSourceFileOfNode(nodeForSourceFile)
	if !c.hasParseDiagnostics(sourceFile) {
		c.addDiagnostic(ast.NewDiagnostic(sourceFile, core.NewTextRange(start, start+length), message, args...))
		return true
	}
	return false
}
func (c *Checker) grammarErrorOnNode(node ast.Handle, message *diagnostics.Message, args ...any) bool {
	sourceFile := ast.GetSourceFileOfNode(node)
	if !c.hasParseDiagnostics(sourceFile) {
		c.error(node, message, args...)
		return true
	}
	return false
}
func (c *Checker) grammarErrorOnNodeSkippedOnNoEmit(node ast.Handle, message *diagnostics.Message, args ...any) bool {
	sourceFile := ast.GetSourceFileOfNode(node)
	if !c.hasParseDiagnostics(sourceFile) {
		d := NewDiagnosticForNode(node, message, args...)
		d.SetSkippedOnNoEmit()
		c.addDiagnostic(d)
		return true
	}
	return false
}
func getIdentifierFromEntityNameExpression(node ast.Handle) ast.Handle {
	switch node.Kind() {
	case ast.KindIdentifier:
		return node
	case ast.KindPropertyAccessExpression:
		return node.PropertyAccessExpressionName()
	default:
		return ast.Handle{}
	}
}
func (c *Checker) checkGrammarRegularExpressionLiteral(node ast.Handle) bool {
	sourceFile := ast.GetSourceFileOfNode(node)
	if !c.hasParseDiagnostics(sourceFile) {
		var lastError *ast.Diagnostic
		if c.regExpScanner == nil {
			c.regExpScanner = scanner.NewScanner()
		}
		c.regExpScanner.SetScriptTarget(c.languageVersion)
		c.regExpScanner.SetLanguageVariant(sourceFile.LanguageVariant)
		c.regExpScanner.SetOnError(func(message *diagnostics.Message, start int, length int, args ...any) {
			if message.Category() == diagnostics.CategoryMessage && lastError != nil && start == lastError.Pos() && length == lastError.Len() {
				err := ast.NewDiagnostic(nil, core.NewTextRange(start, start+length), message, args...)
				lastError.AddRelatedInfo(err)
			} else if lastError == nil || start != lastError.Pos() {
				lastError = ast.NewDiagnostic(sourceFile, core.NewTextRange(start, start+length), message, args...)
				lastError = c.addDiagnostic(lastError)
			}
		})
		c.regExpScanner.SetText(sourceFile.Text())
		c.regExpScanner.ResetTokenState(node.Pos())
		c.regExpScanner.Scan()
		tokenIsRegularExpressionLiteral := c.regExpScanner.ReScanSlashToken(true) == ast.KindRegularExpressionLiteral
		c.regExpScanner.SetText("")
		c.regExpScanner.SetOnError(nil)
		debug.Assert(tokenIsRegularExpressionLiteral)
		return lastError != nil
	}
	return false
}
func (c *Checker) checkGrammarPrivateIdentifierExpression(privId ast.Handle) bool {
	privIdAsNode := privId
	if ast.GetContainingClass(privId).IsNil() {
		return c.grammarErrorOnNode(privId, diagnostics.Private_identifiers_are_not_allowed_outside_class_bodies)
	}
	if !ast.IsForInStatement(privId.Parent()) {
		if !ast.IsExpressionNode(privIdAsNode) {
			return c.grammarErrorOnNode(privIdAsNode, diagnostics.Private_identifiers_are_only_allowed_in_class_bodies_and_may_only_be_used_as_part_of_a_class_member_declaration_property_access_or_on_the_left_hand_side_of_an_in_expression)
		}
		isInOperation := ast.IsBinaryExpression(privId.Parent()) && privId.Parent().BinaryExpressionOperatorToken().Kind() == ast.KindInKeyword
		if c.getSymbolForPrivateIdentifierExpression(privIdAsNode) == nil && !isInOperation {
			return c.grammarErrorOnNode(privIdAsNode, diagnostics.Cannot_find_name_0, privId.Text)
		}
	}
	return false
}
func (c *Checker) checkGrammarMappedType(node ast.Handle) bool {
	if len(node.Members()) > 0 {
		return c.grammarErrorOnNode(node.Members()[0], diagnostics.A_mapped_type_may_not_declare_properties_or_methods)
	}
	return false
}
func (c *Checker) checkGrammarDecorator(decorator ast.Handle) bool {
	sourceFile := ast.GetSourceFileOfNode(decorator)
	if !c.hasParseDiagnostics(sourceFile) {
		node := decorator.Expression()
		if ast.IsParenthesizedExpression(node) {
			return false
		}
		canHaveCallExpression := true
		var errorNode ast.Handle
		for {
			if ast.IsExpressionWithTypeArguments(node) || ast.IsNonNullExpression(node) {
				node = node.Expression()
				continue
			}
			if ast.IsCallExpression(node) {
				callExpr := node
				if !canHaveCallExpression {
					errorNode = node
				}
				if !callExpr.QuestionDotToken().IsNil() {
					errorNode = callExpr.QuestionDotToken()
				}
				node = callExpr.Expression()
				canHaveCallExpression = false
				continue
			}
			if ast.IsPropertyAccessExpression(node) {
				propertyAccessExpr := node
				if !propertyAccessExpr.QuestionDotToken().IsNil() {
					errorNode = propertyAccessExpr.QuestionDotToken()
				}
				node = propertyAccessExpr.Expression()
				canHaveCallExpression = false
				continue
			}
			if !ast.IsIdentifier(node) {
				errorNode = node
			}
			break
		}
		if !errorNode.IsNil() {
			err := c.error(decorator.Expression(), diagnostics.Expression_must_be_enclosed_in_parentheses_to_be_used_as_a_decorator)
			err.AddRelatedInfo(createDiagnosticForNode(errorNode, diagnostics.Invalid_syntax_in_decorator))
			return true
		}
	}
	return false
}
func (c *Checker) checkGrammarExportDeclaration(node ast.Handle) bool {
	if node.IsTypeOnly() && !node.ExportClause().IsNil() && node.ExportClause().Kind() == ast.KindNamedExports {
		return c.checkGrammarTypeOnlyNamedImportsOrExports(node.ExportClause())
	}
	return false
}
func (c *Checker) checkGrammarModuleElementContext(node ast.Handle, errorMessage *diagnostics.Message) bool {
	isInAppropriateContext := node.Parent().Kind() == ast.KindSourceFile || node.Parent().Kind() == ast.KindModuleBlock || node.Parent().Kind() == ast.KindModuleDeclaration
	if !isInAppropriateContext {
		c.grammarErrorOnFirstToken(node, errorMessage)
	}
	return !isInAppropriateContext
}
func (c *Checker) checkGrammarModifiers(node ast.Handle) bool {
	if node.Modifiers() == 0 {
		return false
	}
	if c.reportObviousDecoratorErrors(node) || c.reportObviousModifierErrors(node) {
		return true
	}
	if ast.IsThisParameter(node) {
		return c.grammarErrorOnFirstToken(node, diagnostics.Neither_decorators_nor_modifiers_may_be_applied_to_this_parameters)
	}
	blockScopeKind := ast.NodeFlagsNone
	if ast.IsVariableStatement(node) {
		blockScopeKind = node.VariableStatementDeclarationList().Flags() & ast.NodeFlagsBlockScoped
	}
	var lastStatic ast.Handle
	var lastDeclare ast.Handle
	var lastAsync ast.Handle
	var lastOverride ast.Handle
	var firstDecorator ast.Handle
	flags := ast.ModifierFlagsNone
	sawExportBeforeDecorators := false
	hasLeadingDecorators := false
	modifiers := node.ModifierNodes()
	for _, modifier := range modifiers {
		if ast.IsDecorator(modifier) {
			if !ast.NodeCanBeDecorated(c.legacyDecorators, node, node.Parent(), node.Parent().Parent()) {
				if node.Kind() == ast.KindMethodDeclaration && !ast.NodeIsPresent(node.Body()) {
					return c.grammarErrorOnFirstToken(node, diagnostics.A_decorator_can_only_decorate_a_method_implementation_not_an_overload)
				} else {
					return c.grammarErrorOnFirstToken(node, diagnostics.Decorators_are_not_valid_here)
				}
			} else if c.legacyDecorators && (node.Kind() == ast.KindGetAccessor || node.Kind() == ast.KindSetAccessor) {
				accessors := ast.GetAllAccessorDeclarationsForDeclaration(node, ast.DeclarationNodes(c.getSymbolOfDeclaration(node)))
				if ast.HasDecorators(accessors.FirstAccessor) && node == accessors.SecondAccessor {
					return c.grammarErrorOnFirstToken(node, diagnostics.Decorators_cannot_be_applied_to_multiple_get_Slashset_accessors_of_the_same_name)
				}
			}
			if flags&^(ast.ModifierFlagsExportDefault|ast.ModifierFlagsDecorator) != 0 {
				return c.grammarErrorOnNode(modifier, diagnostics.Decorators_are_not_valid_here)
			}
			if hasLeadingDecorators && flags&ast.ModifierFlagsModifier != 0 {
				if firstDecorator.IsNil() {
					panic("Expected firstDecorator to be set")
				}
				sourceFile := ast.GetSourceFileOfNode(modifier)
				if !c.hasParseDiagnostics(sourceFile) {
					err := c.error(modifier, diagnostics.Decorators_may_not_appear_after_export_or_export_default_if_they_also_appear_before_export)
					err.AddRelatedInfo(createDiagnosticForNode(firstDecorator, diagnostics.Decorator_used_before_export_here))
					return true
				}
				return false
			}
			flags |= ast.ModifierFlagsDecorator
			if flags&ast.ModifierFlagsModifier == 0 {
				hasLeadingDecorators = true
			} else if flags&ast.ModifierFlagsExport != 0 {
				sawExportBeforeDecorators = true
			}
			if firstDecorator.IsNil() {
				firstDecorator = modifier
			}
		} else {
			if modifier.Kind() != ast.KindReadonlyKeyword {
				if node.Kind() == ast.KindPropertySignature || node.Kind() == ast.KindMethodSignature {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_a_type_member, scanner.TokenToString(modifier.Kind()))
				}
				if node.Kind() == ast.KindIndexSignature && (modifier.Kind() != ast.KindStaticKeyword || !ast.IsClassLike(node.Parent())) {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_an_index_signature, scanner.TokenToString(modifier.Kind()))
				}
			}
			if modifier.Kind() != ast.KindInKeyword && modifier.Kind() != ast.KindOutKeyword && modifier.Kind() != ast.KindConstKeyword {
				if node.Kind() == ast.KindTypeParameter {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_a_type_parameter, scanner.TokenToString(modifier.Kind()))
				}
			}
			switch modifier.Kind() {
			case ast.KindConstKeyword:
				if node.Kind() != ast.KindEnumDeclaration && node.Kind() != ast.KindTypeParameter {
					return c.grammarErrorOnNode(node, diagnostics.A_class_member_cannot_have_the_0_keyword, scanner.TokenToString(ast.KindConstKeyword))
				}
				parent := node.Parent()
				if node.Kind() == ast.KindTypeParameter {
					if !(ast.IsFunctionLikeDeclaration(parent) || ast.IsClassLike(parent) || ast.IsFunctionTypeNode(parent) || ast.IsConstructorTypeNode(parent) || ast.IsCallSignatureDeclaration(parent) || ast.IsConstructSignatureDeclaration(parent) || ast.IsMethodSignatureDeclaration(parent)) {
						return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_can_only_appear_on_a_type_parameter_of_a_function_method_or_class, scanner.TokenToString(modifier.Kind()))
					}
				}
			case ast.KindOverrideKeyword:
				if flags&ast.ModifierFlagsOverride != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_already_seen, "override")
				} else if flags&ast.ModifierFlagsAmbient != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_be_used_with_1_modifier, "override", "declare")
				} else if flags&ast.ModifierFlagsReadonly != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, "override", "readonly")
				} else if flags&ast.ModifierFlagsAccessor != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, "override", "accessor")
				} else if flags&ast.ModifierFlagsAsync != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, "override", "async")
				}
				flags |= ast.ModifierFlagsOverride
				lastOverride = modifier
			case ast.KindPublicKeyword, ast.KindProtectedKeyword, ast.KindPrivateKeyword:
				text := visibilityToString(ast.ModifierToFlag(modifier.Kind()))
				if flags&ast.ModifierFlagsAccessibilityModifier != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.Accessibility_modifier_already_seen)
				} else if flags&ast.ModifierFlagsOverride != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, text, "override")
				} else if flags&ast.ModifierFlagsStatic != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, text, "static")
				} else if flags&ast.ModifierFlagsAccessor != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, text, "accessor")
				} else if flags&ast.ModifierFlagsReadonly != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, text, "readonly")
				} else if flags&ast.ModifierFlagsAsync != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, text, "async")
				} else if node.Parent().Kind() == ast.KindModuleBlock || node.Parent().Kind() == ast.KindSourceFile {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_a_module_or_namespace_element, text)
				} else if flags&ast.ModifierFlagsAbstract != 0 {
					if modifier.Kind() == ast.KindPrivateKeyword {
						return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_be_used_with_1_modifier, text, "abstract")
					} else if modifier.Flags()&ast.NodeFlagsReparsed == 0 {
						return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, text, "abstract")
					}
				} else if ast.IsPrivateIdentifierClassElementDeclaration(node) {
					return c.grammarErrorOnNode(modifier, diagnostics.An_accessibility_modifier_cannot_be_used_with_a_private_identifier)
				}
				flags |= ast.ModifierToFlag(modifier.Kind())
			case ast.KindStaticKeyword:
				if flags&ast.ModifierFlagsStatic != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_already_seen, "static")
				} else if flags&ast.ModifierFlagsReadonly != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, "static", "readonly")
				} else if flags&ast.ModifierFlagsAsync != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, "static", "async")
				} else if flags&ast.ModifierFlagsAccessor != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, "static", "accessor")
				} else if node.Parent().Kind() == ast.KindModuleBlock || node.Parent().Kind() == ast.KindSourceFile {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_a_module_or_namespace_element, "static")
				} else if node.Kind() == ast.KindParameter {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_a_parameter, "static")
				} else if flags&ast.ModifierFlagsAbstract != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_be_used_with_1_modifier, "static", "abstract")
				} else if flags&ast.ModifierFlagsOverride != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, "static", "override")
				}
				flags |= ast.ModifierFlagsStatic
				lastStatic = modifier
			case ast.KindAccessorKeyword:
				if flags&ast.ModifierFlagsAccessor != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_already_seen, "accessor")
				} else if flags&ast.ModifierFlagsReadonly != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_be_used_with_1_modifier, "accessor", "readonly")
				} else if flags&ast.ModifierFlagsAmbient != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_be_used_with_1_modifier, "accessor", "declare")
				} else if node.Kind() != ast.KindPropertyDeclaration {
					return c.grammarErrorOnNode(modifier, diagnostics.X_accessor_modifier_can_only_appear_on_a_property_declaration)
				}
				flags |= ast.ModifierFlagsAccessor
			case ast.KindReadonlyKeyword:
				if flags&ast.ModifierFlagsReadonly != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_already_seen, "readonly")
				} else if node.Kind() != ast.KindPropertyDeclaration && node.Kind() != ast.KindPropertySignature && node.Kind() != ast.KindIndexSignature && node.Kind() != ast.KindParameter {
					return c.grammarErrorOnNode(modifier, diagnostics.X_readonly_modifier_can_only_appear_on_a_property_declaration_or_index_signature)
				} else if flags&ast.ModifierFlagsAccessor != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_be_used_with_1_modifier, "readonly", "accessor")
				}
				flags |= ast.ModifierFlagsReadonly
			case ast.KindExportKeyword:
				if c.compilerOptions.VerbatimModuleSyntax == core.TSTrue && node.Flags()&ast.NodeFlagsAmbient == 0 && node.Kind() != ast.KindTypeAliasDeclaration && node.Kind() != ast.KindInterfaceDeclaration && node.Kind() != ast.KindModuleDeclaration && node.Parent().Kind() == ast.KindSourceFile && c.program.GetEmitModuleFormatOfFile(ast.GetSourceFileOfNode(node)) == core.ModuleKindCommonJS {
					return c.grammarErrorOnNode(modifier, diagnostics.A_top_level_export_modifier_cannot_be_used_on_value_declarations_in_a_CommonJS_module_when_verbatimModuleSyntax_is_enabled)
				}
				if flags&ast.ModifierFlagsExport != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_already_seen, "export")
				} else if flags&ast.ModifierFlagsAmbient != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, "export", "declare")
				} else if flags&ast.ModifierFlagsAbstract != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, "export", "abstract")
				} else if flags&ast.ModifierFlagsAsync != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, "export", "async")
				} else if ast.IsClassLike(node.Parent()) && !ast.IsJSTypeAliasDeclaration(node) {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_class_elements_of_this_kind, "export")
				} else if node.Kind() == ast.KindParameter {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_a_parameter, "export")
				} else if blockScopeKind == ast.NodeFlagsUsing {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_a_using_declaration, "export")
				} else if blockScopeKind == ast.NodeFlagsAwaitUsing {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_an_await_using_declaration, "export")
				}
				flags |= ast.ModifierFlagsExport
			case ast.KindDefaultKeyword:
				var container ast.Handle
				if node.Parent().Kind() == ast.KindSourceFile {
					container = node.Parent()
				} else {
					container = node.Parent().Parent()
				}
				if container.Kind() == ast.KindModuleDeclaration && !ast.IsAmbientModule(container) {
					return c.grammarErrorOnNode(modifier, diagnostics.A_default_export_can_only_be_used_in_an_ECMAScript_style_module)
				} else if blockScopeKind == ast.NodeFlagsUsing {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_a_using_declaration, "default")
				} else if blockScopeKind == ast.NodeFlagsAwaitUsing {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_an_await_using_declaration, "default")
				} else if flags&ast.ModifierFlagsExport == 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, "export", "default")
				} else if sawExportBeforeDecorators {
					return c.grammarErrorOnNode(firstDecorator, diagnostics.Decorators_are_not_valid_here)
				}
				flags |= ast.ModifierFlagsDefault
			case ast.KindDeclareKeyword:
				if flags&ast.ModifierFlagsAmbient != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_already_seen, "declare")
				} else if flags&ast.ModifierFlagsAsync != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_be_used_in_an_ambient_context, "async")
				} else if flags&ast.ModifierFlagsOverride != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_be_used_in_an_ambient_context, "override")
				} else if ast.IsClassLike(node.Parent()) && !ast.IsPropertyDeclaration(node) {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_class_elements_of_this_kind, "declare")
				} else if node.Kind() == ast.KindParameter {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_a_parameter, "declare")
				} else if blockScopeKind == ast.NodeFlagsUsing {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_a_using_declaration, "declare")
				} else if blockScopeKind == ast.NodeFlagsAwaitUsing {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_an_await_using_declaration, "declare")
				} else if (node.Parent().Flags()&ast.NodeFlagsAmbient != 0) && node.Parent().Kind() == ast.KindModuleBlock {
					return c.grammarErrorOnNode(modifier, diagnostics.A_declare_modifier_cannot_be_used_in_an_already_ambient_context)
				} else if ast.IsPrivateIdentifierClassElementDeclaration(node) {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_be_used_with_a_private_identifier, "declare")
				} else if flags&ast.ModifierFlagsAccessor != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_be_used_with_1_modifier, "declare", "accessor")
				}
				flags |= ast.ModifierFlagsAmbient
				lastDeclare = modifier
			case ast.KindAbstractKeyword:
				if flags&ast.ModifierFlagsAbstract != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_already_seen, "abstract")
				}
				if node.Kind() != ast.KindClassDeclaration && node.Kind() != ast.KindConstructorType {
					if node.Kind() != ast.KindMethodDeclaration && node.Kind() != ast.KindPropertyDeclaration && node.Kind() != ast.KindGetAccessor && node.Kind() != ast.KindSetAccessor {
						return c.grammarErrorOnNode(modifier, diagnostics.X_abstract_modifier_can_only_appear_on_a_class_method_or_property_declaration)
					}
					if !(node.Parent().Kind() == ast.KindClassDeclaration && ast.HasSyntacticModifier(node.Parent(), ast.ModifierFlagsAbstract)) {
						var message *diagnostics.Message
						if node.Kind() == ast.KindPropertyDeclaration {
							message = diagnostics.Abstract_properties_can_only_appear_within_an_abstract_class
						} else {
							message = diagnostics.Abstract_methods_can_only_appear_within_an_abstract_class
						}
						return c.grammarErrorOnNode(modifier, message)
					}
					if flags&ast.ModifierFlagsStatic != 0 {
						return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_be_used_with_1_modifier, "static", "abstract")
					}
					if flags&ast.ModifierFlagsPrivate != 0 {
						return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_be_used_with_1_modifier, "private", "abstract")
					}
					if flags&ast.ModifierFlagsAsync != 0 && !lastAsync.IsNil() {
						return c.grammarErrorOnNode(lastAsync, diagnostics.X_0_modifier_cannot_be_used_with_1_modifier, "async", "abstract")
					}
					if flags&ast.ModifierFlagsOverride != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
						return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, "abstract", "override")
					}
					if flags&ast.ModifierFlagsAccessor != 0 && modifier.Flags()&ast.NodeFlagsReparsed == 0 {
						return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, "abstract", "accessor")
					}
				}
				if name := node.Name(); !name.IsNil() && name.Kind() == ast.KindPrivateIdentifier {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_be_used_with_a_private_identifier, "abstract")
				}
				flags |= ast.ModifierFlagsAbstract
			case ast.KindAsyncKeyword:
				if flags&ast.ModifierFlagsAsync != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_already_seen, "async")
				} else if flags&ast.ModifierFlagsAmbient != 0 || node.Parent().Flags()&ast.NodeFlagsAmbient != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_be_used_in_an_ambient_context, "async")
				} else if node.Kind() == ast.KindParameter {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_appear_on_a_parameter, "async")
				}
				if flags&ast.ModifierFlagsAbstract != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_cannot_be_used_with_1_modifier, "async", "abstract")
				}
				flags |= ast.ModifierFlagsAsync
				lastAsync = modifier
			case ast.KindInKeyword, ast.KindOutKeyword:
				var inOutFlag ast.ModifierFlags
				if modifier.Kind() == ast.KindInKeyword {
					inOutFlag = ast.ModifierFlagsIn
				} else {
					inOutFlag = ast.ModifierFlagsOut
				}
				var inOutText string
				if modifier.Kind() == ast.KindInKeyword {
					inOutText = "in"
				} else {
					inOutText = "out"
				}
				parent := node.Parent()
				if node.Kind() != ast.KindTypeParameter || !parent.IsNil() && !(ast.IsInterfaceDeclaration(parent) || ast.IsClassLike(parent) || ast.IsTypeOrJSTypeAliasDeclaration(parent)) {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_can_only_appear_on_a_type_parameter_of_a_class_interface_or_type_alias, inOutText)
				}
				if flags&inOutFlag != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_already_seen, inOutText)
				}
				if inOutFlag&ast.ModifierFlagsIn != 0 && flags&ast.ModifierFlagsOut != 0 {
					return c.grammarErrorOnNode(modifier, diagnostics.X_0_modifier_must_precede_1_modifier, "in", "out")
				}
				flags |= inOutFlag
			}
		}
	}
	if node.Kind() == ast.KindConstructor {
		if flags&ast.ModifierFlagsStatic != 0 {
			return c.grammarErrorOnNode(lastStatic, diagnostics.X_0_modifier_cannot_appear_on_a_constructor_declaration, "static")
		}
		if flags&ast.ModifierFlagsOverride != 0 {
			return c.grammarErrorOnNode(lastOverride, diagnostics.X_0_modifier_cannot_appear_on_a_constructor_declaration, "override")
		}
		if flags&ast.ModifierFlagsAsync != 0 {
			return c.grammarErrorOnNode(lastAsync, diagnostics.X_0_modifier_cannot_appear_on_a_constructor_declaration, "async")
		}
		return false
	} else if (node.Kind() == ast.KindImportDeclaration || node.Kind() == ast.KindJSImportDeclaration || node.Kind() == ast.KindImportEqualsDeclaration) && flags&ast.ModifierFlagsAmbient != 0 {
		return c.grammarErrorOnNode(lastDeclare, diagnostics.A_0_modifier_cannot_be_used_with_an_import_declaration, "declare")
	} else if node.Kind() == ast.KindParameter && (flags&ast.ModifierFlagsParameterPropertyModifier != 0) && ast.IsBindingPattern(node.Name()) {
		return c.grammarErrorOnNode(node, diagnostics.A_parameter_property_may_not_be_declared_using_a_binding_pattern)
	} else if node.Kind() == ast.KindParameter && (flags&ast.ModifierFlagsParameterPropertyModifier != 0) && !node.ParameterDeclarationDotDotDotToken().IsNil() {
		return c.grammarErrorOnNode(node, diagnostics.A_parameter_property_cannot_be_declared_using_a_rest_parameter)
	}
	if flags&ast.ModifierFlagsAsync != 0 {
		return c.checkGrammarAsyncModifier(node, lastAsync)
	}
	return false
}
func (c *Checker) reportObviousModifierErrors(node ast.Handle) bool {
	modifier := c.findFirstIllegalModifier(node)
	if modifier.IsNil() {
		return false
	}
	return c.grammarErrorOnFirstToken(modifier, diagnostics.Modifiers_cannot_appear_here)
}
func (c *Checker) findFirstModifierExcept(node ast.Handle, allowedModifier ast.Kind) ast.Handle {
	modifier := core.Find(node.ModifierNodes(), ast.IsModifier)
	if !modifier.IsNil() && modifier.Kind() != allowedModifier {
		return modifier
	}
	return ast.Handle{}
}
func (c *Checker) findFirstIllegalModifier(node ast.Handle) ast.Handle {
	switch node.Kind() {
	case ast.KindGetAccessor, ast.KindSetAccessor, ast.KindConstructor, ast.KindPropertyDeclaration, ast.KindPropertySignature, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindIndexSignature, ast.KindModuleDeclaration, ast.KindImportDeclaration, ast.KindJSImportDeclaration, ast.KindImportEqualsDeclaration, ast.KindExportDeclaration, ast.KindExportAssignment, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindParameter, ast.KindTypeParameter, ast.KindJSTypeAliasDeclaration:
		return ast.Handle{}
	case ast.KindClassStaticBlockDeclaration, ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment, ast.KindNamespaceExportDeclaration, ast.KindMissingDeclaration:
		return core.Find(node.ModifierNodes(), ast.IsModifier)
	default:
		if node.Parent().Kind() == ast.KindModuleBlock || node.Parent().Kind() == ast.KindSourceFile {
			return ast.Handle{}
		}
		switch node.Kind() {
		case ast.KindFunctionDeclaration:
			return c.findFirstModifierExcept(node, ast.KindAsyncKeyword)
		case ast.KindClassDeclaration, ast.KindConstructorType:
			return c.findFirstModifierExcept(node, ast.KindAbstractKeyword)
		case ast.KindClassExpression, ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration:
			return core.Find(node.ModifierNodes(), ast.IsModifier)
		case ast.KindVariableStatement:
			if node.VariableStatementDeclarationList().Flags()&ast.NodeFlagsUsing != 0 {
				return c.findFirstModifierExcept(node, ast.KindAwaitKeyword)
			}
			return core.Find(node.ModifierNodes(), ast.IsModifier)
		case ast.KindEnumDeclaration:
			return c.findFirstModifierExcept(node, ast.KindConstKeyword)
		default:
			panic("Unhandled case in findFirstIllegalModifier.")
		}
	}
}
func (c *Checker) reportObviousDecoratorErrors(node ast.Handle) bool {
	decorator := c.findFirstIllegalDecorator(node)
	if decorator.IsNil() {
		return false
	}
	return c.grammarErrorOnFirstToken(decorator, diagnostics.Decorators_are_not_valid_here)
}
func (c *Checker) findFirstIllegalDecorator(node ast.Handle) ast.Handle {
	if ast.CanHaveIllegalDecorators(node) {
		decorator := core.Find(node.ModifierNodes(), ast.IsDecorator)
		return decorator
	} else {
		return ast.Handle{}
	}
}
func (c *Checker) checkGrammarAsyncModifier(node ast.Handle, asyncModifier ast.Handle) bool {
	switch node.Kind() {
	case ast.KindMethodDeclaration, ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction:
		return false
	}
	return c.grammarErrorOnNode(asyncModifier, diagnostics.X_0_modifier_cannot_be_used_here, "async")
}
func (c *Checker) checkGrammarForDisallowedTrailingComma(store *ast.Store, list ast.ListRef, diag *diagnostics.Message) bool {
	if store == nil || list == 0 {
		return false
	}
	if store.ListHasTrailingComma(list) {
		return c.grammarErrorAtPos(store.ListAt(list, 0), store.ListLoc(list).End()-len(","), len(","), diag)
	}
	return false
}
func (c *Checker) checkGrammarTypeParameterList(typeParameters ast.ListRef, file *ast.SourceFile) bool {
	if typeParameters != 0 && file.ParseStore().ListLen(typeParameters) == 0 {
		start := file.ParseStore().ListLoc(typeParameters).Pos() - len("<")
		end := scanner.SkipTrivia(file.Text(), file.ParseStore().ListLoc(typeParameters).End()) + len(">")
		return c.grammarErrorAtPos(file.ParseRoot(), start, end-start, diagnostics.Type_parameter_list_cannot_be_empty)
	}
	return false
}
func (c *Checker) checkGrammarParameterList(store *ast.Store, parameters ast.ListRef) bool {
	if store == nil {
		return false
	}
	seenOptionalParameter := false
	parameterCount := store.ListLen(parameters)
	for i := range parameterCount {
		parameter := store.ListAt(parameters, i)
		if !parameter.DotDotDotToken().IsNil() {
			if i != parameterCount-1 {
				return c.grammarErrorOnNode(parameter.DotDotDotToken(), diagnostics.A_rest_parameter_must_be_last_in_a_parameter_list)
			}
			if parameter.Flags()&ast.NodeFlagsAmbient == 0 {
				c.checkGrammarForDisallowedTrailingComma(store, parameters, diagnostics.A_rest_parameter_or_binding_pattern_may_not_have_a_trailing_comma)
			}
			if !parameter.QuestionToken().IsNil() {
				return c.grammarErrorOnNode(parameter.QuestionToken(), diagnostics.A_rest_parameter_cannot_be_optional)
			}
			if !parameter.Initializer().IsNil() {
				return c.grammarErrorOnNode(parameter.Name(), diagnostics.A_rest_parameter_cannot_have_an_initializer)
			}
		} else if isOptionalDeclaration(parameter) {
			seenOptionalParameter = true
			if !parameter.QuestionToken().IsNil() && parameter.QuestionToken().Flags()&ast.NodeFlagsReparsed == 0 && !parameter.Initializer().IsNil() {
				return c.grammarErrorOnNode(parameter.Name(), diagnostics.Parameter_cannot_have_question_mark_and_initializer)
			}
		} else if seenOptionalParameter && parameter.Initializer().IsNil() {
			return c.grammarErrorOnNode(parameter.Name(), diagnostics.A_required_parameter_cannot_follow_an_optional_parameter)
		}
	}
	return false
}
func (c *Checker) checkGrammarForUseStrictSimpleParameterList(node ast.Handle) bool {
	if c.languageVersion >= core.ScriptTargetES2016 {
		body := node.Body()
		var useStrictDirective ast.Handle
		if !body.IsNil() && ast.IsBlock(body) {
			useStrictDirective = binder.FindUseStrictPrologue(ast.GetSourceFileOfNode(node), body.Statements())
		}
		if !useStrictDirective.IsNil() {
			nonSimpleParameters := core.Filter(node.Parameters(), func(n ast.Handle) bool {
				parameter := n
				return !parameter.Initializer().IsNil() || ast.IsBindingPattern(parameter.Name()) || isRestParameter(parameter)
			})
			if len(nonSimpleParameters) != 0 {
				for _, parameter := range nonSimpleParameters {
					err := c.error(parameter, diagnostics.This_parameter_is_not_allowed_with_use_strict_directive)
					err.AddRelatedInfo(createDiagnosticForNode(useStrictDirective, diagnostics.X_use_strict_directive_used_here))
				}
				err := c.error(useStrictDirective, diagnostics.X_use_strict_directive_cannot_be_used_with_non_simple_parameter_list)
				for index, parameter := range nonSimpleParameters {
					var relatedMessage *diagnostics.Message
					if index == 0 {
						relatedMessage = diagnostics.Non_simple_parameter_declared_here
					} else {
						relatedMessage = diagnostics.X_and_here
					}
					err.AddRelatedInfo(createDiagnosticForNode(parameter, relatedMessage))
				}
				return true
			}
		}
	}
	return false
}
func (c *Checker) checkGrammarFunctionLikeDeclaration(node ast.Handle) bool {
	file := ast.GetSourceFileOfNode(node)
	return c.checkGrammarModifiers(node) || c.checkGrammarTypeParameterList(node.TypeParameterList(), file) || c.checkGrammarParameterList(node.Store(), node.ParameterList()) || c.checkGrammarArrowFunction(node, file) || (ast.IsFunctionLikeDeclaration(node) && c.checkGrammarForUseStrictSimpleParameterList(node))
}
func (c *Checker) checkGrammarClassLikeDeclaration(node ast.Handle) bool {
	file := ast.GetSourceFileOfNode(node)
	return c.checkGrammarClassDeclarationHeritageClauses(node, file) || c.checkGrammarTypeParameterList(node.TypeParameterList(), file)
}
func (c *Checker) checkGrammarArrowFunction(node ast.Handle, file *ast.SourceFile) bool {
	if !ast.IsArrowFunction(node) {
		return false
	}
	arrowFunc := node
	typeParameters := arrowFunc.TypeParameterList()
	if typeParameters != 0 {
		typeParamNodes := node.Store().ListSlice(typeParameters)
		hasConstraint := len(typeParamNodes) > 0 && !typeParamNodes[0].TypeParameterDeclarationConstraint().IsNil()
		if !(len(typeParamNodes) > 1 || node.Store().ListHasTrailingComma(typeParameters) || hasConstraint) {
			if tspath.FileExtensionIsOneOf(file.FileName(), []string{tspath.ExtensionMts, tspath.ExtensionCts}) {
				c.grammarErrorOnNode(node.Store().ListAt(typeParameters, 0), diagnostics.This_syntax_is_reserved_in_files_with_the_mts_or_cts_extension_Add_a_trailing_comma_or_explicit_constraint)
			}
		}
	}
	equalsGreaterThanToken := arrowFunc.EqualsGreaterThanToken()
	arrowFullText := file.Text()[equalsGreaterThanToken.Pos():equalsGreaterThanToken.End()]
	return strings.ContainsFunc(arrowFullText, stringutil.IsLineBreak) && c.grammarErrorOnNode(equalsGreaterThanToken, diagnostics.Line_terminator_not_permitted_before_arrow)
}
func (c *Checker) checkGrammarIndexSignatureParameters(node ast.Handle) bool {
	paramNodes := node.Parameters()
	if len(paramNodes) == 0 {
		return c.grammarErrorOnNode(node, diagnostics.An_index_signature_must_have_exactly_one_parameter)
	}
	parameter := paramNodes[0]
	if len(paramNodes) != 1 {
		return c.grammarErrorOnNode(parameter.Name(), diagnostics.An_index_signature_must_have_exactly_one_parameter)
	}
	c.checkGrammarForDisallowedTrailingComma(node.Store(), node.ParameterList(), diagnostics.An_index_signature_cannot_have_a_trailing_comma)
	if !parameter.DotDotDotToken().IsNil() {
		return c.grammarErrorOnNode(parameter.DotDotDotToken(), diagnostics.An_index_signature_cannot_have_a_rest_parameter)
	}
	if parameter.Modifiers() != 0 {
		return c.grammarErrorOnNode(parameter.Name(), diagnostics.An_index_signature_parameter_cannot_have_an_accessibility_modifier)
	}
	if !parameter.QuestionToken().IsNil() {
		return c.grammarErrorOnNode(parameter.QuestionToken(), diagnostics.An_index_signature_parameter_cannot_have_a_question_mark)
	}
	if !parameter.Initializer().IsNil() {
		return c.grammarErrorOnNode(parameter.Name(), diagnostics.An_index_signature_parameter_cannot_have_an_initializer)
	}
	typeNode := parameter.Type
	if typeNode().IsNil() {
		return c.grammarErrorOnNode(parameter.Name(), diagnostics.An_index_signature_parameter_must_have_a_type_annotation)
	}
	t := c.getTypeFromTypeNode(typeNode())
	if someType(t, func(t *Type) bool {
		return t.flags&TypeFlagsStringOrNumberLiteralOrUnique != 0
	}) || c.isGenericType(t) {
		return c.grammarErrorOnNode(parameter.Name(), diagnostics.An_index_signature_parameter_type_cannot_be_a_literal_type_or_generic_type_Consider_using_a_mapped_object_type_instead)
	}
	if !everyType(t, c.isValidIndexKeyType) {
		return c.grammarErrorOnNode(parameter.Name(), diagnostics.An_index_signature_parameter_type_must_be_string_number_symbol_or_a_template_literal_type)
	}
	if node.Type().IsNil() {
		return c.grammarErrorOnNode(node, diagnostics.An_index_signature_must_have_a_type_annotation)
	}
	return false
}
func (c *Checker) checkGrammarIndexSignature(node ast.Handle) bool {
	return c.checkGrammarModifiers(node) || c.checkGrammarIndexSignatureParameters(node)
}
func (c *Checker) checkGrammarForAtLeastOneTypeArgument(node ast.Handle, typeArguments ast.ListRef) bool {
	if typeArguments != 0 && node.Store().ListLen(typeArguments) == 0 {
		sourceFile := ast.GetSourceFileOfNode(node)
		start := node.Store().ListLoc(typeArguments).Pos() - len("<")
		end := scanner.SkipTrivia(sourceFile.Text(), node.Store().ListLoc(typeArguments).End()) + len(">")
		return c.grammarErrorAtPos(sourceFile.ParseRoot(), start, end-start, diagnostics.Type_argument_list_cannot_be_empty)
	}
	return false
}
func (c *Checker) checkGrammarTypeArguments(node ast.Handle, typeArguments ast.ListRef) bool {
	return c.checkGrammarForDisallowedTrailingComma(node.Store(), typeArguments, diagnostics.Trailing_comma_not_allowed) || c.checkGrammarForAtLeastOneTypeArgument(node, typeArguments)
}
func (c *Checker) checkGrammarTaggedTemplateChain(node ast.Handle) bool {
	if !node.QuestionDotToken().IsNil() || node.Flags()&ast.NodeFlagsOptionalChain != 0 {
		return c.grammarErrorOnNode(node.Template(), diagnostics.Tagged_template_expressions_are_not_permitted_in_an_optional_chain)
	}
	return false
}
func (c *Checker) checkGrammarHeritageClause(node ast.Handle) bool {
	types := node.HeritageClauseTypes()
	s := node.Store()
	if c.checkGrammarForDisallowedTrailingComma(s, types, diagnostics.Trailing_comma_not_allowed) {
		return true
	}
	if types != 0 && s.ListLen(types) == 0 {
		listType := scanner.TokenToString(node.HeritageClauseToken())
		return c.grammarErrorAtPos(node, s.ListLoc(types).Pos(), 0, diagnostics.X_0_list_cannot_be_empty, listType)
	}
	for _, n := range s.ListSlice(types) {
		if c.checkGrammarExpressionWithTypeArguments(n) {
			return true
		}
	}
	return false
}
func (c *Checker) checkGrammarExpressionWithTypeArguments(node ast.Handle) bool {
	if ast.IsExpressionWithTypeArguments(node) && node.Expression().Kind() == ast.KindImportKeyword && node.TypeArgumentList() != 0 {
		return c.grammarErrorOnNode(node, diagnostics.This_use_of_import_is_invalid_import_calls_can_be_written_but_they_must_have_parentheses_and_cannot_have_type_arguments)
	}
	return c.checkGrammarTypeArguments(node, node.TypeArgumentList())
}
func (c *Checker) checkGrammarClassDeclarationHeritageClauses(node ast.Handle, file *ast.SourceFile) bool {
	seenExtendsClause := false
	seenImplementsClause := false
	if !c.checkGrammarModifiers(node) && node.HeritageClauses() != 0 {
		for _, heritageClauseNode := range node.Store().ListSlice(node.HeritageClauses()) {
			heritageClause := heritageClauseNode
			if heritageClause.HeritageClauseToken() == ast.KindExtendsKeyword {
				if seenExtendsClause {
					return c.grammarErrorOnFirstToken(heritageClauseNode, diagnostics.X_extends_clause_already_seen)
				}
				if seenImplementsClause {
					return c.grammarErrorOnFirstToken(heritageClauseNode, diagnostics.X_extends_clause_must_precede_implements_clause)
				}
				typeNodes := heritageClause.Types()
				if len(typeNodes) > 1 {
					return c.grammarErrorOnFirstToken(typeNodes[1], diagnostics.Classes_can_only_extend_a_single_class)
				}
				seenExtendsClause = true
			} else {
				if heritageClause.HeritageClauseToken() != ast.KindImplementsKeyword {
					panic(fmt.Sprintf("Unexpected token %q", heritageClause.HeritageClauseToken()))
				}
				if seenImplementsClause {
					return c.grammarErrorOnFirstToken(heritageClauseNode, diagnostics.X_implements_clause_already_seen)
				}
				seenImplementsClause = true
			}
			c.checkGrammarHeritageClause(heritageClause)
		}
	}
	return false
}
func (c *Checker) checkGrammarInterfaceDeclaration(node ast.Handle) bool {
	if node.HeritageClauses() != 0 {
		seenExtendsClause := false
		for _, heritageClauseNode := range node.Store().ListSlice(node.HeritageClauses()) {
			heritageClause := heritageClauseNode
			switch heritageClause.HeritageClauseToken() {
			case ast.KindExtendsKeyword:
				if seenExtendsClause {
					return c.grammarErrorOnFirstToken(heritageClauseNode, diagnostics.X_extends_clause_already_seen)
				}
				seenExtendsClause = true
			case ast.KindImplementsKeyword:
				return c.grammarErrorOnFirstToken(heritageClauseNode, diagnostics.Interface_declaration_cannot_have_implements_clause)
			default:
				panic(fmt.Sprintf("Unexpected token %q", heritageClause.HeritageClauseToken().String()))
			}
			c.checkGrammarHeritageClause(heritageClause)
		}
	}
	return false
}
func (c *Checker) checkGrammarComputedPropertyName(node ast.Handle) bool {
	if node.Kind() != ast.KindComputedPropertyName {
		return false
	}
	computedPropertyName := node
	if computedPropertyName.Expression().Kind() == ast.KindBinaryExpression && computedPropertyName.Expression().BinaryExpressionOperatorToken().Kind() == ast.KindCommaToken {
		return c.grammarErrorOnNode(computedPropertyName.Expression(), diagnostics.A_comma_expression_is_not_allowed_in_a_computed_property_name)
	}
	return false
}
func (c *Checker) checkGrammarForGenerator(node ast.Handle) bool {
	if asterisk := node.AsteriskToken(); !asterisk.IsNil() {
		if node.Kind() != ast.KindFunctionDeclaration && node.Kind() != ast.KindFunctionExpression && node.Kind() != ast.KindMethodDeclaration {
			panic(fmt.Sprintf("Unexpected node kind %q", node.Kind()))
		}
		if node.Flags()&ast.NodeFlagsAmbient != 0 {
			return c.grammarErrorOnNode(asterisk, diagnostics.Generators_are_not_allowed_in_an_ambient_context)
		}
		if node.Body().IsNil() {
			return c.grammarErrorOnNode(asterisk, diagnostics.An_overload_signature_cannot_be_declared_as_a_generator)
		}
	}
	return false
}
func (c *Checker) checkGrammarForInvalidQuestionMark(postfixToken ast.Handle, message *diagnostics.Message) bool {
	return !postfixToken.IsNil() && postfixToken.Kind() == ast.KindQuestionToken && c.grammarErrorOnNode(postfixToken, message)
}
func (c *Checker) checkGrammarForInvalidExclamationToken(postfixToken ast.Handle, message *diagnostics.Message) bool {
	return !postfixToken.IsNil() && postfixToken.Kind() == ast.KindExclamationToken && c.grammarErrorOnNode(postfixToken, message)
}
func (c *Checker) checkGrammarObjectLiteralExpression(node ast.Handle, inDestructuring bool) bool {
	seen := make(map[string]DeclarationMeaning)
	properties := node.Properties()
	for _, prop := range properties {
		if prop.Kind() == ast.KindSpreadAssignment {
			spreadAssignment := prop
			if inDestructuring {
				expression := ast.SkipParentheses(spreadAssignment.Expression())
				if ast.IsArrayLiteralExpression(expression) || ast.IsObjectLiteralExpression(expression) {
					return c.grammarErrorOnNode(spreadAssignment.Expression(), diagnostics.A_rest_element_cannot_contain_a_binding_pattern)
				}
			}
			continue
		}
		name := prop.Name
		if name().Kind() == ast.KindComputedPropertyName {
			c.checkGrammarComputedPropertyName(name())
		}
		if prop.Kind() == ast.KindShorthandPropertyAssignment && !inDestructuring {
			shorthandProp := prop
			if !shorthandProp.ObjectAssignmentInitializer().IsNil() {
				var lastNodeBeforeInitializer ast.Handle
				shorthandProp.ForEachChild(func(child ast.Handle) bool {
					if child != shorthandProp.ObjectAssignmentInitializer() {
						lastNodeBeforeInitializer = child
						return false
					}
					return true
				})
				c.grammarErrorOnFirstToken(lastNodeBeforeInitializer, diagnostics.Did_you_mean_to_use_a_Colon_An_can_only_follow_a_property_name_when_the_containing_object_literal_is_part_of_a_destructuring_pattern)
			}
		}
		if name().Kind() == ast.KindPrivateIdentifier {
			c.grammarErrorOnNode(name(), diagnostics.Private_identifiers_are_not_allowed_outside_class_bodies)
		}
		if modifiers := prop.ModifierNodes(); len(modifiers) != 0 {
			if ast.CanHaveModifiers(prop) {
				for _, mod := range modifiers {
					if ast.IsModifier(mod) && (mod.Kind() != ast.KindAsyncKeyword || prop.Kind() != ast.KindMethodDeclaration) {
						c.grammarErrorOnNode(mod, diagnostics.X_0_modifier_cannot_be_used_here, scanner.GetTextOfNode(mod))
					}
				}
			} else if ast.CanHaveIllegalModifiers(prop) {
				for _, mod := range modifiers {
					if ast.IsModifier(mod) {
						c.grammarErrorOnNode(mod, diagnostics.X_0_modifier_cannot_be_used_here, scanner.GetTextOfNode(mod))
					}
				}
			}
		}
		var currentKind DeclarationMeaning
		switch prop.Kind() {
		case ast.KindShorthandPropertyAssignment, ast.KindPropertyAssignment:
			c.checkGrammarForInvalidExclamationToken(prop.ExclamationToken(), diagnostics.A_definite_assignment_assertion_is_not_permitted_in_this_context)
			c.checkGrammarForInvalidQuestionMark(prop.QuestionToken(), diagnostics.An_object_member_cannot_be_declared_optional)
			if prop.Name().Kind() == ast.KindNumericLiteral {
				c.checkGrammarNumericLiteral(prop.Name())
			}
			if prop.Name().Kind() == ast.KindBigIntLiteral {
				c.addErrorOrSuggestion(true, createDiagnosticForNode(prop.Name(), diagnostics.A_bigint_literal_cannot_be_used_as_a_property_name))
			}
			currentKind = DeclarationMeaningPropertyAssignment
		case ast.KindMethodDeclaration:
			currentKind = DeclarationMeaningMethod
		case ast.KindGetAccessor:
			currentKind = DeclarationMeaningGetAccessor
		case ast.KindSetAccessor:
			currentKind = DeclarationMeaningSetAccessor
		default:
			panic(fmt.Sprintf("Unexpected node kind %q", prop.Kind()))
		}
		if !inDestructuring {
			effectiveName, ok := c.getEffectivePropertyNameForPropertyNameNode(name())
			if !ok {
				continue
			}
			existingKind := seen[effectiveName]
			if existingKind == 0 {
				seen[effectiveName] = currentKind
			} else {
				if (currentKind&DeclarationMeaningMethod != 0) && (existingKind&DeclarationMeaningMethod != 0) {
					c.grammarErrorOnNode(name(), diagnostics.Duplicate_identifier_0, scanner.GetTextOfNode(name()))
				} else if (currentKind&DeclarationMeaningPropertyAssignment != 0) && (existingKind&DeclarationMeaningPropertyAssignment != 0) {
					c.grammarErrorOnNode(name(), diagnostics.An_object_literal_cannot_have_multiple_properties_with_the_same_name, scanner.GetTextOfNode(name()))
				} else if (currentKind&DeclarationMeaningGetOrSetAccessor != 0) && (existingKind&DeclarationMeaningGetOrSetAccessor != 0) {
					if existingKind != DeclarationMeaningGetOrSetAccessor && currentKind != existingKind {
						seen[effectiveName] = currentKind | existingKind
					} else {
						return c.grammarErrorOnNode(name(), diagnostics.An_object_literal_cannot_have_multiple_get_Slashset_accessors_with_the_same_name)
					}
				} else {
					return c.grammarErrorOnNode(name(), diagnostics.An_object_literal_cannot_have_property_and_accessor_with_the_same_name)
				}
			}
		}
	}
	return false
}
func (c *Checker) checkGrammarJsxElement(node ast.Handle) bool {
	c.checkGrammarJsxName(node.TagName())
	c.checkGrammarTypeArguments(node, node.TypeArgumentList())
	var seen collections.Set[string]
	for _, attrNode := range node.Attributes().Properties() {
		if attrNode.Kind() == ast.KindJsxSpreadAttribute {
			continue
		}
		attr := attrNode
		name := attr.Name()
		initializer := attr.Initializer
		textOfName := name.Text()
		if !seen.Has(textOfName) {
			seen.Add(textOfName)
		} else {
			return c.grammarErrorOnNode(name, diagnostics.JSX_elements_cannot_have_multiple_attributes_with_the_same_name)
		}
		if !initializer().IsNil() && initializer().Kind() == ast.KindJsxExpression && initializer().Expression().IsNil() {
			return c.grammarErrorOnNode(initializer(), diagnostics.JSX_attributes_must_only_be_assigned_a_non_empty_expression)
		}
	}
	return false
}
func (c *Checker) checkGrammarJsxName(node ast.Handle) bool {
	if ast.IsPropertyAccessExpression(node) && ast.IsJsxNamespacedName(node.Expression()) {
		return c.grammarErrorOnNode(node.Expression(), diagnostics.JSX_property_access_expressions_cannot_include_JSX_namespace_names)
	}
	if ast.IsJsxNamespacedName(node) && c.compilerOptions.GetJSXTransformEnabled() && !scanner.IsIntrinsicJsxName(node.JsxNamespacedNameNamespace().Text()) {
		return c.grammarErrorOnNode(node, diagnostics.React_components_cannot_include_JSX_namespace_names)
	}
	return false
}
func (c *Checker) checkGrammarJsxExpression(node ast.Handle) bool {
	if !node.Expression().IsNil() && ast.IsCommaSequence(node.Expression()) {
		return c.grammarErrorOnNode(node.Expression(), diagnostics.JSX_expressions_may_not_use_the_comma_operator_Did_you_mean_to_write_an_array)
	}
	return false
}
func (c *Checker) checkGrammarForInOrForOfStatement(forInOrOfStatement ast.Handle) bool {
	asNode := forInOrOfStatement
	if c.checkGrammarStatementInAmbientContext(asNode) {
		return true
	}
	if forInOrOfStatement.Kind() == ast.KindForOfStatement && !forInOrOfStatement.AwaitModifier().IsNil() {
		if forInOrOfStatement.Flags()&ast.NodeFlagsAwaitContext == 0 {
			sourceFile := ast.GetSourceFileOfNode(asNode)
			if ast.IsInTopLevelContext(asNode) {
				if !c.hasParseDiagnostics(sourceFile) {
					if !ast.IsEffectiveExternalModule(sourceFile, c.compilerOptions) {
						c.addDiagnostic(createDiagnosticForNode(forInOrOfStatement.AwaitModifier(), diagnostics.X_for_await_loops_are_only_allowed_at_the_top_level_of_a_file_when_that_file_is_a_module_but_this_file_has_no_imports_or_exports_Consider_adding_an_empty_export_to_make_this_file_a_module))
					}
					switch c.moduleKind {
					case core.ModuleKindNode16, core.ModuleKindNode18, core.ModuleKindNode20, core.ModuleKindNodeNext:
						sourceFileMetaData := c.program.GetSourceFileMetaData(sourceFile.Path())
						if sourceFileMetaData.ImpliedNodeFormat == core.ModuleKindCommonJS {
							c.addDiagnostic(createDiagnosticForNode(forInOrOfStatement.AwaitModifier(), diagnostics.The_current_file_is_a_CommonJS_module_and_cannot_use_await_at_the_top_level))
							break
						}
						fallthrough
					case core.ModuleKindES2022, core.ModuleKindESNext, core.ModuleKindPreserve, core.ModuleKindSystem:
						if c.languageVersion >= core.ScriptTargetES2017 {
							break
						}
						fallthrough
					default:
						c.addDiagnostic(createDiagnosticForNode(forInOrOfStatement.AwaitModifier(), diagnostics.Top_level_for_await_loops_are_only_allowed_when_the_module_option_is_set_to_es2022_esnext_system_node16_node18_node20_nodenext_or_preserve_and_the_target_option_is_set_to_es2017_or_higher))
					}
				}
			} else {
				if !c.hasParseDiagnostics(sourceFile) {
					diagnostic := createDiagnosticForNode(forInOrOfStatement.AwaitModifier(), diagnostics.X_for_await_loops_are_only_allowed_within_async_functions_and_at_the_top_levels_of_modules)
					containingFunc := ast.GetContainingFunction(forInOrOfStatement)
					if !containingFunc.IsNil() && containingFunc.Kind() != ast.KindConstructor {
						debug.Assert((ast.GetFunctionFlags(containingFunc)&ast.FunctionFlagsAsync) == 0, "Enclosing function should never be an async function.")
						relatedInfo := createDiagnosticForNode(containingFunc, diagnostics.Did_you_mean_to_mark_this_function_as_async)
						diagnostic.AddRelatedInfo(relatedInfo)
					}
					c.addDiagnostic(diagnostic)
					return true
				}
			}
		}
	}
	if ast.IsForOfStatement(asNode) && forInOrOfStatement.Flags()&ast.NodeFlagsAwaitContext == 0 && ast.IsIdentifier(forInOrOfStatement.Initializer()) && forInOrOfStatement.Initializer().Text() == "async" {
		c.grammarErrorOnNode(forInOrOfStatement.Initializer(), diagnostics.The_left_hand_side_of_a_for_of_statement_may_not_be_async)
		return false
	}
	if forInOrOfStatement.Initializer().Kind() == ast.KindVariableDeclarationList {
		variableList := forInOrOfStatement.Initializer
		if !c.checkGrammarVariableDeclarationList(variableList()) {
			declarations := variableList().Declarations()
			if len(declarations) == 0 {
				return false
			}
			if len(declarations) > 1 {
				var diagnostic *diagnostics.Message
				if forInOrOfStatement.Kind() == ast.KindForInStatement {
					diagnostic = diagnostics.Only_a_single_variable_declaration_is_allowed_in_a_for_in_statement
				} else {
					diagnostic = diagnostics.Only_a_single_variable_declaration_is_allowed_in_a_for_of_statement
				}
				return c.grammarErrorOnFirstToken(declarations[1], diagnostic)
			}
			firstVariableDeclaration := declarations[0]
			if !firstVariableDeclaration.Initializer().IsNil() {
				var diagnostic *diagnostics.Message
				if forInOrOfStatement.Kind() == ast.KindForInStatement {
					diagnostic = diagnostics.The_variable_declaration_of_a_for_in_statement_cannot_have_an_initializer
				} else {
					diagnostic = diagnostics.The_variable_declaration_of_a_for_of_statement_cannot_have_an_initializer
				}
				return c.grammarErrorOnNode(firstVariableDeclaration.Name(), diagnostic)
			}
			if !firstVariableDeclaration.Type().IsNil() {
				var diagnostic *diagnostics.Message
				if forInOrOfStatement.Kind() == ast.KindForInStatement {
					diagnostic = diagnostics.The_left_hand_side_of_a_for_in_statement_cannot_use_a_type_annotation
				} else {
					diagnostic = diagnostics.The_left_hand_side_of_a_for_of_statement_cannot_use_a_type_annotation
				}
				return c.grammarErrorOnNode(firstVariableDeclaration, diagnostic)
			}
		}
	}
	return false
}
func (c *Checker) checkGrammarAccessor(accessor ast.Handle) bool {
	body := accessor.Body()
	if accessor.Flags()&ast.NodeFlagsAmbient == 0 && (accessor.Parent().Kind() != ast.KindTypeLiteral) && (accessor.Parent().Kind() != ast.KindInterfaceDeclaration) {
		if body.IsNil() && !ast.HasSyntacticModifier(accessor, ast.ModifierFlagsAbstract) {
			return c.grammarErrorAtPos(accessor, accessor.End()-1, len(";"), diagnostics.X_0_expected, "{")
		}
	}
	if !body.IsNil() {
		if ast.HasSyntacticModifier(accessor, ast.ModifierFlagsAbstract) {
			return c.grammarErrorOnNode(accessor, diagnostics.An_abstract_accessor_cannot_have_an_implementation)
		}
		if accessor.Parent().Kind() == ast.KindTypeLiteral || accessor.Parent().Kind() == ast.KindInterfaceDeclaration {
			return c.grammarErrorOnNode(body, diagnostics.An_implementation_cannot_be_declared_in_ambient_contexts)
		}
	}
	typeParameters := accessor.TypeParameterList()
	if typeParameters != 0 {
		return c.grammarErrorOnNode(accessor.Name(), diagnostics.An_accessor_cannot_have_type_parameters)
	}
	if !c.doesAccessorHaveCorrectParameterCount(accessor) {
		return c.grammarErrorOnNode(accessor.Name(), core.IfElse(accessor.Kind() == ast.KindGetAccessor, diagnostics.A_get_accessor_cannot_have_parameters, diagnostics.A_set_accessor_must_have_exactly_one_parameter))
	}
	if accessor.Kind() == ast.KindSetAccessor {
		if !accessor.Type().IsNil() {
			return c.grammarErrorOnNode(accessor.Name(), diagnostics.A_set_accessor_cannot_have_a_return_type_annotation)
		}
		parameterNode := GetSetAccessorValueParameter(accessor)
		if parameterNode.IsNil() {
			panic("Return value does not match parameter count assertion.")
		}
		parameter := parameterNode
		if !parameter.DotDotDotToken().IsNil() {
			return c.grammarErrorOnNode(parameter.DotDotDotToken(), diagnostics.A_set_accessor_cannot_have_rest_parameter)
		}
		if !parameter.QuestionToken().IsNil() {
			return c.grammarErrorOnNode(parameter.QuestionToken(), diagnostics.A_set_accessor_cannot_have_an_optional_parameter)
		}
		if !parameter.Initializer().IsNil() {
			return c.grammarErrorOnNode(accessor.Name(), diagnostics.A_set_accessor_parameter_cannot_have_an_initializer)
		}
	}
	return false
}

func (c *Checker) doesAccessorHaveCorrectParameterCount(accessor ast.Handle) bool {
	return !c.getAccessorThisParameter(accessor).IsNil() || len(accessor.Parameters()) == core.IfElse(accessor.Kind() == ast.KindGetAccessor, 0, 1)
}
func (c *Checker) checkGrammarTypeOperatorNode(node ast.Handle) bool {
	if node.TypeOperatorNodeOperator() == ast.KindUniqueKeyword {
		innerType := node.Type()
		if innerType.Kind() != ast.KindSymbolKeyword {
			return c.grammarErrorOnNode(innerType, diagnostics.X_0_expected, scanner.TokenToString(ast.KindSymbolKeyword))
		}
		parent := ast.WalkUpParenthesizedTypes(node.Parent())
		switch parent.Kind() {
		case ast.KindVariableDeclaration:
			decl := parent
			if decl.Name().Kind() != ast.KindIdentifier {
				return c.grammarErrorOnNode(node, diagnostics.X_unique_symbol_types_may_not_be_used_on_a_variable_declaration_with_a_binding_name)
			}
			if !isVariableDeclarationInVariableStatement(decl) {
				return c.grammarErrorOnNode(node, diagnostics.X_unique_symbol_types_are_only_allowed_on_variables_in_a_variable_statement)
			}
			if decl.Parent().Flags()&ast.NodeFlagsConst == 0 {
				return c.grammarErrorOnNode(parent.VariableDeclarationName(), diagnostics.A_variable_whose_type_is_a_unique_symbol_type_must_be_const)
			}
		case ast.KindPropertyDeclaration:
			if !ast.IsStatic(parent) || !hasReadonlyModifier(parent) {
				return c.grammarErrorOnNode(parent.PropertyDeclarationName(), diagnostics.A_property_of_a_class_whose_type_is_a_unique_symbol_type_must_be_both_static_and_readonly)
			}
		case ast.KindPropertySignature:
			if !ast.HasSyntacticModifier(parent, ast.ModifierFlagsReadonly) {
				return c.grammarErrorOnNode(parent.PropertySignatureDeclarationName(), diagnostics.A_property_of_an_interface_or_type_literal_whose_type_is_a_unique_symbol_type_must_be_readonly)
			}
		default:
			return c.grammarErrorOnNode(node, diagnostics.X_unique_symbol_types_are_not_allowed_here)
		}
	} else if node.TypeOperatorNodeOperator() == ast.KindReadonlyKeyword {
		innerType := node.Type
		if innerType().Kind() != ast.KindArrayType && innerType().Kind() != ast.KindTupleType {
			return c.grammarErrorOnFirstToken(node, diagnostics.X_readonly_type_modifier_is_only_permitted_on_array_and_tuple_literal_types, scanner.TokenToString(ast.KindSymbolKeyword))
		}
	}
	return false
}
func (c *Checker) checkGrammarForInvalidDynamicName(node ast.Handle, message *diagnostics.Message) bool {
	if !c.isNonBindableDynamicName(node) {
		return false
	}
	var expression ast.Handle
	if ast.IsElementAccessExpression(node) {
		expression = ast.SkipParentheses(node.ElementAccessExpressionArgumentExpression())
	} else {
		expression = node.Expression()
	}
	if !ast.IsEntityNameExpression(expression) {
		return c.grammarErrorOnNode(node, message)
	}
	return false
}

func (c *Checker) isNonBindableDynamicName(node ast.Handle) bool {
	return ast.IsDynamicName(node) && !c.isLateBindableName(node)
}
func (c *Checker) checkGrammarMethod(node ast.Handle) bool {
	if c.checkGrammarFunctionLikeDeclaration(node) {
		return true
	}
	if node.Kind() == ast.KindMethodDeclaration {
		if node.Parent().Kind() == ast.KindObjectLiteralExpression {
			if modifiers := node.Modifiers(); modifiers != 0 && !(node.Store().ListLen(modifiers) == 1 && node.Store().ListAt(modifiers, 0).Kind() == ast.KindAsyncKeyword) {
				return c.grammarErrorOnFirstToken(node, diagnostics.Modifiers_cannot_appear_here)
			}
			methodDecl := node
			if c.checkGrammarForInvalidQuestionMark(methodDecl.PostfixToken(), diagnostics.An_object_member_cannot_be_declared_optional) {
				return true
			}
			if c.checkGrammarForInvalidExclamationToken(methodDecl.PostfixToken(), diagnostics.A_definite_assignment_assertion_is_not_permitted_in_this_context) {
				return true
			}
			if node.Body().IsNil() {
				return c.grammarErrorAtPos(node, node.End()-1, len(";"), diagnostics.X_0_expected, "{")
			}
		}
		if c.checkGrammarForGenerator(node) {
			return true
		}
	}
	if ast.IsClassLike(node.Parent()) {
		if node.Flags()&ast.NodeFlagsAmbient != 0 {
			return c.checkGrammarForInvalidDynamicName(node.Name(), diagnostics.A_computed_property_name_in_an_ambient_context_must_refer_to_an_expression_whose_type_is_a_literal_type_or_a_unique_symbol_type)
		} else if node.Kind() == ast.KindMethodDeclaration && node.Body().IsNil() {
			return c.checkGrammarForInvalidDynamicName(node.Name(), diagnostics.A_computed_property_name_in_a_method_overload_must_refer_to_an_expression_whose_type_is_a_literal_type_or_a_unique_symbol_type)
		}
	} else if node.Parent().Kind() == ast.KindInterfaceDeclaration {
		return c.checkGrammarForInvalidDynamicName(node.Name(), diagnostics.A_computed_property_name_in_an_interface_must_refer_to_an_expression_whose_type_is_a_literal_type_or_a_unique_symbol_type)
	} else if node.Parent().Kind() == ast.KindTypeLiteral {
		return c.checkGrammarForInvalidDynamicName(node.Name(), diagnostics.A_computed_property_name_in_a_type_literal_must_refer_to_an_expression_whose_type_is_a_literal_type_or_a_unique_symbol_type)
	}
	return false
}
func (c *Checker) checkGrammarBreakOrContinueStatement(node ast.Handle) bool {
	targetLabel := node.Label()
	var current ast.Handle = node
	for !current.IsNil() {
		if ast.IsFunctionLikeOrClassStaticBlockDeclaration(current) {
			return c.grammarErrorOnNode(node, diagnostics.Jump_target_cannot_cross_function_boundary)
		}
		switch current.Kind() {
		case ast.KindLabeledStatement:
			if !targetLabel.IsNil() && current.Label().Text() == targetLabel.Text() {
				isMisplacedContinueLabel := node.Kind() == ast.KindContinueStatement && !ast.IsIterationStatement(current.Statement(), true)
				if isMisplacedContinueLabel {
					return c.grammarErrorOnNode(node, diagnostics.A_continue_statement_can_only_jump_to_a_label_of_an_enclosing_iteration_statement)
				}
				return false
			}
		case ast.KindSwitchStatement:
			if node.Kind() == ast.KindBreakStatement && targetLabel.IsNil() {
				return false
			}
		default:
			if ast.IsIterationStatement(current, false) && targetLabel.IsNil() {
				return false
			}
		}
		current = current.Parent()
	}
	if !targetLabel.IsNil() {
		var message *diagnostics.Message
		if node.Kind() == ast.KindBreakStatement {
			message = diagnostics.A_break_statement_can_only_jump_to_a_label_of_an_enclosing_statement
		} else {
			message = diagnostics.A_continue_statement_can_only_jump_to_a_label_of_an_enclosing_iteration_statement
		}
		return c.grammarErrorOnNode(node, message)
	} else {
		var message *diagnostics.Message
		if node.Kind() == ast.KindBreakStatement {
			message = diagnostics.A_break_statement_can_only_be_used_within_an_enclosing_iteration_or_switch_statement
		} else {
			message = diagnostics.A_continue_statement_can_only_be_used_within_an_enclosing_iteration_statement
		}
		return c.grammarErrorOnNode(node, message)
	}
}
func (c *Checker) checkGrammarBindingElement(node ast.Handle) bool {
	if !node.DotDotDotToken().IsNil() {
		elements := node.Parent().ElementList()
		if node != core.LastOrNil(node.Store().ListSlice(elements)) {
			return c.grammarErrorOnNode(node, diagnostics.A_rest_element_must_be_last_in_a_destructuring_pattern)
		}
		c.checkGrammarForDisallowedTrailingComma(node.Store(), elements, diagnostics.A_rest_parameter_or_binding_pattern_may_not_have_a_trailing_comma)
		if !node.PropertyName().IsNil() {
			return c.grammarErrorOnNode(node.Name(), diagnostics.A_rest_element_cannot_have_a_property_name)
		}
	}
	if !node.DotDotDotToken().IsNil() && !node.Initializer().IsNil() {
		return c.grammarErrorAtPos(node, node.Initializer().Pos()-1, 1, diagnostics.A_rest_element_cannot_have_an_initializer)
	}
	return false
}
func (c *Checker) checkGrammarVariableDeclaration(node ast.Handle) bool {
	nodeFlags := c.getCombinedNodeFlagsCached(node)
	blockScopeKind := nodeFlags & ast.NodeFlagsBlockScoped
	if ast.IsBindingPattern(node.Name()) {
		switch blockScopeKind {
		case ast.NodeFlagsAwaitUsing:
			return c.grammarErrorOnNode(node, diagnostics.X_0_declarations_may_not_have_binding_patterns, "await using")
		case ast.NodeFlagsUsing:
			return c.grammarErrorOnNode(node, diagnostics.X_0_declarations_may_not_have_binding_patterns, "using")
		}
	}
	if node.Parent().Parent().Kind() != ast.KindForInStatement && node.Parent().Parent().Kind() != ast.KindForOfStatement {
		if nodeFlags&ast.NodeFlagsAmbient != 0 {
			c.checkAmbientInitializer(node)
		} else if node.Initializer().IsNil() {
			if ast.IsBindingPattern(node.Name()) && !ast.IsBindingPattern(node.Parent()) {
				return c.grammarErrorOnNode(node, diagnostics.A_destructuring_declaration_must_have_an_initializer)
			}
			switch blockScopeKind {
			case ast.NodeFlagsAwaitUsing:
				return c.grammarErrorOnNode(node, diagnostics.X_0_declarations_must_be_initialized, "await using")
			case ast.NodeFlagsUsing:
				return c.grammarErrorOnNode(node, diagnostics.X_0_declarations_must_be_initialized, "using")
			case ast.NodeFlagsConst:
				return c.grammarErrorOnNode(node, diagnostics.X_0_declarations_must_be_initialized, "const")
			}
		}
	}
	if !node.ExclamationToken().IsNil() && (node.Parent().Parent().Kind() != ast.KindVariableStatement || node.Type().IsNil() || !node.Initializer().IsNil() || nodeFlags&ast.NodeFlagsAmbient != 0) {
		var message *diagnostics.Message
		switch {
		case !node.Initializer().IsNil():
			message = diagnostics.Declarations_with_initializers_cannot_also_have_definite_assignment_assertions
		case node.Type().IsNil():
			message = diagnostics.Declarations_with_definite_assignment_assertions_must_also_have_type_annotations
		default:
			message = diagnostics.A_definite_assignment_assertion_is_not_permitted_in_this_context
		}
		return c.grammarErrorOnNode(node.ExclamationToken(), message)
	}
	if c.program.GetEmitModuleFormatOfFile(ast.GetSourceFileOfNode(node)) < core.ModuleKindSystem && (node.Parent().Parent().Flags()&ast.NodeFlagsAmbient == 0) && ast.HasSyntacticModifier(node.Parent().Parent(), ast.ModifierFlagsExport) {
		c.checkGrammarForEsModuleMarkerInBindingName(node.Name())
	}
	return blockScopeKind != 0 && c.checkGrammarNameInLetOrConstDeclarations(node.Name())
}
func (c *Checker) checkGrammarForEsModuleMarkerInBindingName(name ast.Handle) bool {
	if ast.IsIdentifier(name) {
		if name.Text() == "__esModule" {
			return c.grammarErrorOnNodeSkippedOnNoEmit(name, diagnostics.Identifier_expected_esModule_is_reserved_as_an_exported_marker_when_transforming_ECMAScript_modules)
		}
	} else {
		for _, element := range name.Elements() {
			if !element.Name().IsNil() {
				return c.checkGrammarForEsModuleMarkerInBindingName(element.Name())
			}
		}
	}
	return false
}
func (c *Checker) checkGrammarNameInLetOrConstDeclarations(name ast.Handle) bool {
	if name.Kind() == ast.KindIdentifier {
		if name.Text() == "let" {
			return c.grammarErrorOnNode(name, diagnostics.X_let_is_not_allowed_to_be_used_as_a_name_in_let_or_const_declarations)
		}
	} else {
		elements := name.Elements()
		for _, element := range elements {
			bindingElement := element
			if !bindingElement.Name().IsNil() {
				c.checkGrammarNameInLetOrConstDeclarations(bindingElement.Name())
			}
		}
	}
	return false
}
func (c *Checker) checkGrammarVariableDeclarationList(declarationList ast.Handle) bool {
	declarations := declarationList.VariableDeclarationListDeclarations()
	s := declarationList.Store()
	if c.checkGrammarForDisallowedTrailingComma(s, declarations, diagnostics.Trailing_comma_not_allowed) {
		return true
	}
	if s.ListLen(declarations) == 0 {
		loc := s.ListLoc(declarations)
		return c.grammarErrorAtPos(declarationList, loc.Pos(), loc.End()-loc.Pos(), diagnostics.Variable_declaration_list_cannot_be_empty)
	}
	blockScopeFlags := declarationList.Flags() & ast.NodeFlagsBlockScoped
	if blockScopeFlags == ast.NodeFlagsUsing || blockScopeFlags == ast.NodeFlagsAwaitUsing {
		if ast.IsForInStatement(declarationList.Parent()) {
			return c.grammarErrorOnNode(declarationList, core.IfElse(blockScopeFlags == ast.NodeFlagsUsing, diagnostics.The_left_hand_side_of_a_for_in_statement_cannot_be_a_using_declaration, diagnostics.The_left_hand_side_of_a_for_in_statement_cannot_be_an_await_using_declaration))
		}
		if declarationList.Flags()&ast.NodeFlagsAmbient != 0 {
			return c.grammarErrorOnNode(declarationList, core.IfElse(blockScopeFlags == ast.NodeFlagsUsing, diagnostics.X_using_declarations_are_not_allowed_in_ambient_contexts, diagnostics.X_await_using_declarations_are_not_allowed_in_ambient_contexts))
		}
		if ast.IsVariableStatement(declarationList.Parent()) && (ast.IsCaseClause(declarationList.Parent().Parent()) || ast.IsDefaultClause(declarationList.Parent().Parent())) {
			return c.grammarErrorOnNode(declarationList, core.IfElse(blockScopeFlags == ast.NodeFlagsUsing, diagnostics.X_using_declarations_are_not_allowed_in_case_or_default_clauses_unless_contained_within_a_block, diagnostics.X_await_using_declarations_are_not_allowed_in_case_or_default_clauses_unless_contained_within_a_block))
		}
	}
	if blockScopeFlags == ast.NodeFlagsAwaitUsing {
		return c.checkGrammarAwaitOrAwaitUsing(declarationList)
	}
	return false
}
func (c *Checker) checkGrammarAwaitOrAwaitUsing(node ast.Handle) bool {
	hasError := false
	container := getContainingFunctionOrClassStaticBlock(node)
	if !container.IsNil() && ast.IsClassStaticBlockDeclaration(container) {
		var message *diagnostics.Message
		if ast.IsAwaitExpression(node) {
			message = diagnostics.X_await_expression_cannot_be_used_inside_a_class_static_block
		} else {
			message = diagnostics.X_await_using_statements_cannot_be_used_inside_a_class_static_block
		}
		c.error(node, message)
		hasError = true
	} else if node.Flags()&ast.NodeFlagsAwaitContext == 0 {
		if ast.IsInTopLevelContext(node) {
			sourceFile := ast.GetSourceFileOfNode(node)
			if !c.hasParseDiagnostics(sourceFile) {
				var span core.TextRange
				var spanCalculated bool
				if !ast.IsEffectiveExternalModule(sourceFile, c.compilerOptions) {
					span = scanner.GetRangeOfTokenAtPosition(sourceFile, node.Pos())
					spanCalculated = true
					var message *diagnostics.Message
					if ast.IsAwaitExpression(node) {
						message = diagnostics.X_await_expressions_are_only_allowed_at_the_top_level_of_a_file_when_that_file_is_a_module_but_this_file_has_no_imports_or_exports_Consider_adding_an_empty_export_to_make_this_file_a_module
					} else {
						message = diagnostics.X_await_using_statements_are_only_allowed_at_the_top_level_of_a_file_when_that_file_is_a_module_but_this_file_has_no_imports_or_exports_Consider_adding_an_empty_export_to_make_this_file_a_module
					}
					diagnostic := ast.NewDiagnostic(sourceFile, span, message)
					c.addDiagnostic(diagnostic)
					hasError = true
				}
				switch c.moduleKind {
				case core.ModuleKindNode16, core.ModuleKindNode18, core.ModuleKindNode20, core.ModuleKindNodeNext:
					sourceFileMetaData := c.program.GetSourceFileMetaData(sourceFile.Path())
					if sourceFileMetaData.ImpliedNodeFormat == core.ModuleKindCommonJS {
						if !spanCalculated {
							span = scanner.GetRangeOfTokenAtPosition(sourceFile, node.Pos())
						}
						c.addDiagnostic(ast.NewDiagnostic(sourceFile, span, diagnostics.The_current_file_is_a_CommonJS_module_and_cannot_use_await_at_the_top_level))
						hasError = true
						break
					}
					fallthrough
				case core.ModuleKindES2022, core.ModuleKindESNext, core.ModuleKindPreserve, core.ModuleKindSystem:
					if c.languageVersion >= core.ScriptTargetES2017 {
						break
					}
					fallthrough
				default:
					if !spanCalculated {
						span = scanner.GetRangeOfTokenAtPosition(sourceFile, node.Pos())
					}
					var message *diagnostics.Message
					if ast.IsAwaitExpression(node) {
						message = diagnostics.Top_level_await_expressions_are_only_allowed_when_the_module_option_is_set_to_es2022_esnext_system_node16_node18_node20_nodenext_or_preserve_and_the_target_option_is_set_to_es2017_or_higher
					} else {
						message = diagnostics.Top_level_await_using_statements_are_only_allowed_when_the_module_option_is_set_to_es2022_esnext_system_node16_node18_node20_nodenext_or_preserve_and_the_target_option_is_set_to_es2017_or_higher
					}
					c.addDiagnostic(ast.NewDiagnostic(sourceFile, span, message))
					hasError = true
				}
			}
		} else {
			sourceFile := ast.GetSourceFileOfNode(node)
			if !c.hasParseDiagnostics(sourceFile) {
				span := scanner.GetRangeOfTokenAtPosition(sourceFile, node.Pos())
				var message *diagnostics.Message
				if ast.IsAwaitExpression(node) {
					message = diagnostics.X_await_expressions_are_only_allowed_within_async_functions_and_at_the_top_levels_of_modules
				} else {
					message = diagnostics.X_await_using_statements_are_only_allowed_within_async_functions_and_at_the_top_levels_of_modules
				}
				diagnostic := ast.NewDiagnostic(sourceFile, span, message)
				if !container.IsNil() && container.Kind() != ast.KindConstructor && !hasAsyncModifier(container) {
					relatedInfo := NewDiagnosticForNode(container, diagnostics.Did_you_mean_to_mark_this_function_as_async)
					diagnostic.AddRelatedInfo(relatedInfo)
				}
				c.addDiagnostic(diagnostic)
				hasError = true
			}
		}
	}
	if ast.IsAwaitExpression(node) && c.isInParameterInitializerBeforeContainingFunction(node) {
		c.error(node, diagnostics.X_await_expressions_cannot_be_used_in_a_parameter_initializer)
		hasError = true
	}
	return hasError
}
func (c *Checker) checkGrammarYieldExpression(node ast.Handle) bool {
	hasError := false
	if node.Flags()&ast.NodeFlagsYieldContext == 0 {
		c.grammarErrorOnFirstToken(node, diagnostics.A_yield_expression_is_only_allowed_in_a_generator_body)
		hasError = true
	}
	if c.isInParameterInitializerBeforeContainingFunction(node) {
		c.error(node, diagnostics.X_yield_expressions_cannot_be_used_in_a_parameter_initializer)
		hasError = true
	}
	return hasError
}
func (c *Checker) checkGrammarForDisallowedBlockScopedVariableStatement(node ast.Handle) bool {
	if !c.containerAllowsBlockScopedVariable(node.Parent()) {
		blockScopeKind := c.getCombinedNodeFlagsCached(node.VariableStatementDeclarationList()) & ast.NodeFlagsBlockScoped
		if blockScopeKind != 0 {
			var keyword string
			switch {
			case blockScopeKind == ast.NodeFlagsLet:
				keyword = "let"
			case blockScopeKind == ast.NodeFlagsConst:
				keyword = "const"
			case blockScopeKind == ast.NodeFlagsUsing:
				keyword = "using"
			case blockScopeKind == ast.NodeFlagsAwaitUsing:
				keyword = "await using"
			default:
				panic("Unknown BlockScope flag")
			}
			c.error(node, diagnostics.X_0_declarations_can_only_be_declared_inside_a_block, keyword)
		}
	}
	return false
}
func (c *Checker) containerAllowsBlockScopedVariable(parent ast.Handle) bool {
	switch parent.Kind() {
	case ast.KindIfStatement, ast.KindDoStatement, ast.KindWhileStatement, ast.KindWithStatement, ast.KindForStatement, ast.KindForInStatement, ast.KindForOfStatement:
		return false
	case ast.KindLabeledStatement:
		return c.containerAllowsBlockScopedVariable(parent.Parent())
	}
	return true
}
func (c *Checker) checkGrammarMetaProperty(node ast.Handle) bool {
	nodeName := node.Name()
	nameText := nodeName.Text()
	switch node.KeywordToken() {
	case ast.KindNewKeyword:
		if nameText != "target" {
			return c.grammarErrorOnNode(nodeName, diagnostics.X_0_is_not_a_valid_meta_property_for_keyword_1_Did_you_mean_2, nameText, scanner.TokenToString(node.KeywordToken()), "target")
		}
	case ast.KindImportKeyword:
		if nameText != "meta" {
			isCallee := ast.IsCallExpression(node.Parent()) && node.Parent().Expression() == node
			if nameText == "defer" {
				if !isCallee {
					return c.grammarErrorAtPos(node, node.End(), 0, diagnostics.X_0_expected, "(")
				}
			} else {
				if isCallee {
					return c.grammarErrorOnNode(nodeName, diagnostics.X_0_is_not_a_valid_meta_property_for_keyword_import_Did_you_mean_meta_or_defer, nameText)
				}
				return c.grammarErrorOnNode(nodeName, diagnostics.X_0_is_not_a_valid_meta_property_for_keyword_1_Did_you_mean_2, nameText, scanner.TokenToString(node.KeywordToken()), "meta")
			}
		}
	}
	return false
}
func (c *Checker) checkGrammarConstructorTypeParameters(node ast.Handle) bool {
	range_ := node.TypeParameterList()
	if range_ != 0 {
		var pos int
		loc := node.Store().ListLoc(range_)
		if loc.Pos() == loc.End() {
			pos = loc.Pos()
		} else {
			pos = scanner.SkipTrivia(ast.GetSourceFileOfNode(node).Text(), loc.Pos())
		}
		return c.grammarErrorAtPos(node, pos, loc.End()-pos, diagnostics.Type_parameters_cannot_appear_on_a_constructor_declaration)
	}
	return false
}
func (c *Checker) checkGrammarConstructorTypeAnnotation(node ast.Handle) bool {
	t := node.Type()
	if !t.IsNil() {
		return c.grammarErrorOnNode(t, diagnostics.Type_annotation_cannot_appear_on_a_constructor_declaration)
	}
	return false
}
func (c *Checker) checkGrammarProperty(node ast.Handle) bool {
	propertyName := node.Name()
	if ast.IsComputedPropertyName(propertyName) && ast.IsBinaryExpression(propertyName.Expression()) && propertyName.Expression().BinaryExpressionOperatorToken().Kind() == ast.KindInKeyword {
		return c.grammarErrorOnNode(node.Parent().Members()[0], diagnostics.A_mapped_type_may_not_declare_properties_or_methods)
	}
	if ast.IsClassLike(node.Parent()) {
		if ast.IsStringLiteral(propertyName) && propertyName.Text() == "constructor" {
			return c.grammarErrorOnNode(propertyName, diagnostics.Classes_may_not_have_a_field_named_constructor)
		}
		if c.checkGrammarForInvalidDynamicName(propertyName, diagnostics.A_computed_property_name_in_a_class_property_declaration_must_have_a_simple_literal_type_or_a_unique_symbol_type) {
			return true
		}
		if ast.IsAutoAccessorPropertyDeclaration(node) && c.checkGrammarForInvalidQuestionMark(node.PostfixToken(), diagnostics.An_accessor_property_cannot_be_declared_optional) {
			return true
		}
	} else if ast.IsInterfaceDeclaration(node.Parent()) {
		if c.checkGrammarForInvalidDynamicName(propertyName, diagnostics.A_computed_property_name_in_an_interface_must_refer_to_an_expression_whose_type_is_a_literal_type_or_a_unique_symbol_type) {
			return true
		}
		if !ast.IsPropertySignatureDeclaration(node) {
			panic(fmt.Sprintf("Unexpected node kind %q", node.Kind()))
		}
		if initializer := node.Initializer(); !initializer.IsNil() {
			return c.grammarErrorOnNode(initializer, diagnostics.An_interface_property_cannot_have_an_initializer)
		}
	} else if ast.IsTypeLiteralNode(node.Parent()) {
		if c.checkGrammarForInvalidDynamicName(node.Name(), diagnostics.A_computed_property_name_in_a_type_literal_must_refer_to_an_expression_whose_type_is_a_literal_type_or_a_unique_symbol_type) {
			return true
		}
		if !ast.IsPropertySignatureDeclaration(node) {
			panic(fmt.Sprintf("Unexpected node kind %q", node.Kind()))
		}
		if initializer := node.Initializer(); !initializer.IsNil() {
			return c.grammarErrorOnNode(initializer, diagnostics.A_type_literal_property_cannot_have_an_initializer)
		}
	}
	if node.Flags()&ast.NodeFlagsAmbient != 0 {
		c.checkAmbientInitializer(node)
	}
	if ast.IsPropertyDeclaration(node) {
		propDecl := node
		postfixToken := propDecl.PostfixToken
		if !postfixToken().IsNil() && postfixToken().Kind() == ast.KindExclamationToken {
			switch {
			case !propDecl.Initializer().IsNil():
				return c.grammarErrorOnNode(postfixToken(), diagnostics.Declarations_with_initializers_cannot_also_have_definite_assignment_assertions)
			case propDecl.Type().IsNil():
				return c.grammarErrorOnNode(postfixToken(), diagnostics.Declarations_with_definite_assignment_assertions_must_also_have_type_annotations)
			case !ast.IsClassLike(node.Parent()) || node.Flags()&ast.NodeFlagsAmbient != 0 || ast.IsStatic(node) || ast.HasAbstractModifier(node):
				return c.grammarErrorOnNode(postfixToken(), diagnostics.A_definite_assignment_assertion_is_not_permitted_in_this_context)
			}
		}
	}
	return false
}
func (c *Checker) checkAmbientInitializer(node ast.Handle) bool {
	var initializer ast.Handle
	var typeNode ast.Handle
	switch node.Kind() {
	case ast.KindVariableDeclaration:
		varDecl := node
		initializer = varDecl.Initializer()
		typeNode = varDecl.Type()
	case ast.KindPropertyDeclaration:
		propDecl := node
		initializer = propDecl.Initializer()
		typeNode = propDecl.Type()
	case ast.KindPropertySignature:
		propSig := node
		initializer = propSig.Initializer()
		typeNode = propSig.Type()
	default:
		panic(fmt.Sprintf("Unexpected node kind %q", node.Kind()))
	}
	if !initializer.IsNil() {
		isInvalidInitializer := !(isInitializerStringOrNumberLiteralExpression(initializer) || c.isInitializerSimpleLiteralEnumReference(initializer) || initializer.Kind() == ast.KindTrueKeyword || initializer.Kind() == ast.KindFalseKeyword || isInitializerBigIntLiteralExpression(initializer))
		isConstOrReadonly := isDeclarationReadonly(node) || ast.IsVariableDeclaration(node) && c.isVarConstLike(node)
		if isConstOrReadonly && (typeNode.IsNil()) {
			if isInvalidInitializer {
				return c.grammarErrorOnNode(initializer, diagnostics.A_const_initializer_in_an_ambient_context_must_be_a_string_or_numeric_literal_or_literal_enum_reference)
			}
		} else {
			return c.grammarErrorOnNode(initializer, diagnostics.Initializers_are_not_allowed_in_ambient_contexts)
		}
	}
	return false
}
func isInitializerStringOrNumberLiteralExpression(expr ast.Handle) bool {
	return ast.IsStringOrNumericLiteralLike(expr) || expr.Kind() == ast.KindPrefixUnaryExpression && expr.PrefixUnaryExpressionOperator() == ast.KindMinusToken && expr.PrefixUnaryExpressionOperand().Kind() == ast.KindNumericLiteral
}
func isInitializerBigIntLiteralExpression(expr ast.Handle) bool {
	if expr.Kind() == ast.KindBigIntLiteral {
		return true
	}
	if expr.Kind() == ast.KindPrefixUnaryExpression {
		unaryExpr := expr
		return unaryExpr.PrefixUnaryExpressionOperator() == ast.KindMinusToken && unaryExpr.Operand().Kind() == ast.KindBigIntLiteral
	}
	return false
}
func (c *Checker) isInitializerSimpleLiteralEnumReference(expr ast.Handle) bool {
	if ast.IsPropertyAccessExpression(expr) {
		return c.checkExpressionCached(expr).flags&TypeFlagsEnumLike != 0
	}
	if ast.IsElementAccessExpression(expr) {
		elementAccess := expr
		return isInitializerStringOrNumberLiteralExpression(elementAccess.ArgumentExpression()) && ast.IsEntityNameExpression(elementAccess.Expression()) && c.checkExpressionCached(expr).flags&TypeFlagsEnumLike != 0
	}
	return false
}
func (c *Checker) checkGrammarTopLevelElementForRequiredDeclareModifier(node ast.Handle) bool {
	if node.Kind() == ast.KindInterfaceDeclaration || node.Kind() == ast.KindTypeAliasDeclaration || node.Kind() == ast.KindImportDeclaration || node.Kind() == ast.KindJSImportDeclaration || node.Kind() == ast.KindImportEqualsDeclaration || node.Kind() == ast.KindExportDeclaration || node.Kind() == ast.KindExportAssignment || node.Kind() == ast.KindNamespaceExportDeclaration || ast.HasSyntacticModifier(node, ast.ModifierFlagsAmbient|ast.ModifierFlagsExport|ast.ModifierFlagsDefault) {
		return false
	}
	return c.grammarErrorOnFirstToken(node, diagnostics.Top_level_declarations_in_d_ts_files_must_start_with_either_a_declare_or_export_modifier)
}
func (c *Checker) checkGrammarTopLevelElementsForRequiredDeclareModifier(file *ast.SourceFile) bool {
	for _, decl := range file.ParseRoot().Statements() {
		if ast.IsDeclarationNode(decl) || decl.Kind() == ast.KindVariableStatement {
			if c.checkGrammarTopLevelElementForRequiredDeclareModifier(decl) {
				return true
			}
		}
	}
	return false
}
func (c *Checker) checkGrammarSourceFile(node *ast.SourceFile) bool {
	return node.Flags&ast.NodeFlagsAmbient != 0 && c.checkGrammarTopLevelElementsForRequiredDeclareModifier(node)
}
func (c *Checker) checkGrammarStatementInAmbientContext(node ast.Handle) bool {
	if node.Flags()&ast.NodeFlagsAmbient != 0 {
		links := c.nodeLinks.Get(node)
		if !links.hasReportedStatementInAmbientContext && (ast.IsFunctionLike(node.Parent()) || ast.IsAccessor(node.Parent())) {
			links.hasReportedStatementInAmbientContext = c.grammarErrorOnFirstToken(node, diagnostics.An_implementation_cannot_be_declared_in_ambient_contexts)
			return links.hasReportedStatementInAmbientContext
		}
		if node.Parent().Kind() == ast.KindBlock || node.Parent().Kind() == ast.KindModuleBlock || node.Parent().Kind() == ast.KindSourceFile {
			links := c.nodeLinks.Get(node.Parent())
			if !links.hasReportedStatementInAmbientContext {
				links.hasReportedStatementInAmbientContext = c.grammarErrorOnFirstToken(node, diagnostics.Statements_are_not_allowed_in_ambient_contexts)
				return links.hasReportedStatementInAmbientContext
			}
		} else {
		}
	}
	return false
}
func (c *Checker) checkGrammarNumericLiteral(node ast.Handle) {
	nodeText := scanner.GetTextOfNode(node)
	isFractional := strings.ContainsRune(nodeText, '.')
	isScientific := node.TokenFlags()&ast.TokenFlagsScientific != 0
	if isFractional || isScientific {
		return
	}
	value := jsnum.FromString(node.Text())
	if value <= jsnum.MaxSafeInteger {
		return
	}
	c.addErrorOrSuggestion(false, createDiagnosticForNode(node, diagnostics.Numeric_literals_with_absolute_values_equal_to_2_53_or_greater_are_too_large_to_be_represented_accurately_as_integers))
}
func (c *Checker) checkGrammarBigIntLiteral(node ast.Handle) bool {
	literalType := ast.IsLiteralTypeNode(node.Parent()) || ast.IsPrefixUnaryExpression(node.Parent()) && ast.IsLiteralTypeNode(node.Parent().Parent())
	if !literalType {
		if node.Flags()&ast.NodeFlagsAmbient == 0 && c.languageVersion < core.ScriptTargetES2020 {
			if c.grammarErrorOnNode(node, diagnostics.BigInt_literals_are_not_available_when_targeting_lower_than_ES2020) {
				return true
			}
		}
	}
	return false
}
func (c *Checker) checkGrammarImportClause(node ast.Handle) bool {
	switch node.ImportClausePhaseModifier() {
	case ast.KindTypeKeyword:
		if node.Flags()&ast.NodeFlagsJSDoc == 0 && !node.Name().IsNil() && !node.NamedBindings().IsNil() {
			return c.grammarErrorOnNode(node, diagnostics.A_type_only_import_can_specify_a_default_import_or_named_bindings_but_not_both)
		}
		if !node.NamedBindings().IsNil() && node.NamedBindings().Kind() == ast.KindNamedImports {
			return c.checkGrammarTypeOnlyNamedImportsOrExports(node.NamedBindings())
		}
	case ast.KindDeferKeyword:
		if !node.Name().IsNil() {
			return c.grammarErrorOnNode(node, diagnostics.Default_imports_are_not_allowed_in_a_deferred_import)
		}
		if !node.NamedBindings().IsNil() && node.NamedBindings().Kind() == ast.KindNamedImports {
			return c.grammarErrorOnNode(node, diagnostics.Named_imports_are_not_allowed_in_a_deferred_import)
		}
		if c.moduleKind != core.ModuleKindESNext && c.moduleKind != core.ModuleKindPreserve {
			return c.grammarErrorOnNode(node, diagnostics.Deferred_imports_are_only_supported_when_the_module_flag_is_set_to_esnext_or_preserve)
		}
	}
	return false
}
func (c *Checker) checkGrammarTypeOnlyNamedImportsOrExports(namedBindings ast.Handle) bool {
	nodeList := namedBindings.ElementList()
	for _, specifier := range namedBindings.Store().ListSlice(nodeList) {
		var specifierIsTypeOnly bool
		var message *diagnostics.Message
		if specifier.Kind() == ast.KindImportSpecifier {
			specifierIsTypeOnly = specifier.IsTypeOnly()
			message = diagnostics.The_type_modifier_cannot_be_used_on_a_named_import_when_import_type_is_used_on_its_import_statement
		} else {
			specifierIsTypeOnly = specifier.IsTypeOnly()
			message = diagnostics.The_type_modifier_cannot_be_used_on_a_named_export_when_export_type_is_used_on_its_export_statement
		}
		if specifierIsTypeOnly {
			return c.grammarErrorOnFirstToken(specifier, message)
		}
	}
	return false
}
func (c *Checker) checkGrammarImportCallExpression(node ast.Handle) bool {
	if c.compilerOptions.VerbatimModuleSyntax == core.TSTrue && c.moduleKind == core.ModuleKindCommonJS {
		return c.grammarErrorOnNode(node, getVerbatimModuleSyntaxErrorMessage(node))
	}
	if node.Expression().Kind() == ast.KindMetaProperty {
		if c.moduleKind != core.ModuleKindESNext && c.moduleKind != core.ModuleKindPreserve {
			return c.grammarErrorOnNode(node, diagnostics.Deferred_imports_are_only_supported_when_the_module_flag_is_set_to_esnext_or_preserve)
		}
	} else if c.moduleKind == core.ModuleKindES2015 {
		return c.grammarErrorOnNode(node, diagnostics.Dynamic_imports_are_only_supported_when_the_module_flag_is_set_to_es2020_es2022_esnext_commonjs_amd_system_umd_node16_node18_node20_or_nodenext)
	}
	nodeAsCall := node
	if nodeAsCall.TypeArgumentList() != 0 {
		return c.grammarErrorOnNode(node, diagnostics.This_use_of_import_is_invalid_import_calls_can_be_written_but_they_must_have_parentheses_and_cannot_have_type_arguments)
	}
	nodeArguments := nodeAsCall.ArgumentList()
	argumentNodes := node.Store().ListSlice(nodeArguments)
	if !(core.ModuleKindNode16 <= c.moduleKind && c.moduleKind <= core.ModuleKindNodeNext) && c.moduleKind != core.ModuleKindESNext && c.moduleKind != core.ModuleKindPreserve {
		c.checkGrammarForDisallowedTrailingComma(node.Store(), nodeArguments, diagnostics.Trailing_comma_not_allowed)
		if len(argumentNodes) > 1 {
			importAttributesArgument := argumentNodes[1]
			return c.grammarErrorOnNode(importAttributesArgument, diagnostics.Dynamic_imports_only_support_a_second_argument_when_the_module_option_is_set_to_esnext_node16_node18_node20_nodenext_or_preserve)
		}
	}
	if len(argumentNodes) == 0 || len(argumentNodes) > 2 {
		return c.grammarErrorOnNode(node, diagnostics.Dynamic_imports_can_only_accept_a_module_specifier_and_an_optional_set_of_attributes_as_arguments)
	}
	spreadElement := core.Find(argumentNodes, ast.IsSpreadElement)
	if !spreadElement.IsNil() {
		return c.grammarErrorOnNode(spreadElement, diagnostics.Argument_of_dynamic_import_cannot_be_spread_element)
	}
	return false
}
