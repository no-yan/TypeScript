package storechecker

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/storebinder"
	"github.com/microsoft/TypeScript/tsc/internal/storescanner"
)

// The P2 declarations of checker/grammarchecks.go:
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4.

// checkGrammarForUseStrictSimpleParameterList passes the NodeRefs of the
// statements to storebinder.FindUseStrictPrologue, which takes them (R15). The
// body is a node of a source Store, whose list Refs gives.
//
// storetranslate:pointer-hash 20e5b70624236d99 grammarchecks.go:Checker.checkGrammarForUseStrictSimpleParameterList
func (c *Checker) checkGrammarForUseStrictSimpleParameterList(node store.Node) bool {
	if c.languageVersion >= core.ScriptTargetES2016 {
		body := node.Body()
		var useStrictDirective store.Node
		if !body.IsNil() && store.IsBlock(body) {
			useStrictDirective = storebinder.FindUseStrictPrologue(node.File(), body.Statements().Refs())
		}
		if !useStrictDirective.IsNil() {
			nonSimpleParameters := store.FilterList(node.Parameters(), func(n store.Node) bool {
				parameter := n.AsParameterDeclaration()
				return !parameter.Initializer().IsNil() || store.IsBindingPattern(parameter.Name()) || isRestParameter(store.Node(parameter))
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

// checkGrammarFunctionLikeDeclaration reads the members of FunctionLikeData on
// the node (4.1).
//
// storetranslate:pointer-hash 9805c1944f8d032e grammarchecks.go:Checker.checkGrammarFunctionLikeDeclaration
func (c *Checker) checkGrammarFunctionLikeDeclaration(node store.Node) bool {
	// Prevent cascading error by short-circuit
	file := node.File()
	return c.checkGrammarModifiers(node) || c.checkGrammarTypeParameterList(node.TypeParameters(), file) ||
		c.checkGrammarParameterList(node.Parameters()) || c.checkGrammarArrowFunction(node, file) ||
		(store.IsFunctionLikeDeclaration(node) && c.checkGrammarForUseStrictSimpleParameterList(node))
}

// checkGrammarClassDeclarationHeritageClauses reads the member of ClassLikeData
// on the node (4.1).
//
// storetranslate:pointer-hash 397b54844bc4f6a9 grammarchecks.go:Checker.checkGrammarClassDeclarationHeritageClauses
func (c *Checker) checkGrammarClassDeclarationHeritageClauses(node store.Node, file *store.File) bool {
	seenExtendsClause := false
	seenImplementsClause := false

	if !c.checkGrammarModifiers(node) && !node.HeritageClauses().IsNil() {
		heritageClauses := node.HeritageClauses()
		for i := range heritageClauses.Len() {
			heritageClauseNode := heritageClauses.At(i)
			heritageClause := heritageClauseNode.AsHeritageClause()
			if heritageClause.Token() == ast.KindExtendsKeyword {
				if seenExtendsClause {
					return c.grammarErrorOnFirstToken(heritageClauseNode, diagnostics.X_extends_clause_already_seen)
				}

				if seenImplementsClause {
					return c.grammarErrorOnFirstToken(heritageClauseNode, diagnostics.X_extends_clause_must_precede_implements_clause)
				}

				typeNodes := heritageClause.Types()
				if typeNodes.Len() > 1 {
					return c.grammarErrorOnFirstToken(typeNodes.At(1), diagnostics.Classes_can_only_extend_a_single_class)
				}

				seenExtendsClause = true
			} else {
				if heritageClause.Token() != ast.KindImplementsKeyword {
					panic(fmt.Sprintf("Unexpected token %q", heritageClause.Token()))
				}
				if seenImplementsClause {
					return c.grammarErrorOnFirstToken(heritageClauseNode, diagnostics.X_implements_clause_already_seen)
				}

				seenImplementsClause = true
			}

			// Grammar checking heritageClause inside class declaration
			c.checkGrammarHeritageClause(heritageClause)
		}
	}

	return false
}

// checkGrammarForGenerator tests BodyData() != nil with HasBodyData and reads
// its members on the node (4.1).
//
// storetranslate:pointer-hash 372d4ef04d452ede grammarchecks.go:Checker.checkGrammarForGenerator
func (c *Checker) checkGrammarForGenerator(node store.Node) bool {
	if node.HasBodyData() && !node.AsteriskToken().IsNil() {
		if node.Kind() != ast.KindFunctionDeclaration && node.Kind() != ast.KindFunctionExpression && node.Kind() != ast.KindMethodDeclaration {
			panic(fmt.Sprintf("Unexpected node kind %q", node.Kind()))
		}
		if node.Flags()&ast.NodeFlagsAmbient != 0 {
			return c.grammarErrorOnNode(node.AsteriskToken(), diagnostics.Generators_are_not_allowed_in_an_ambient_context)
		}
		if node.Body().IsNil() {
			return c.grammarErrorOnNode(node.AsteriskToken(), diagnostics.An_overload_signature_cannot_be_declared_as_a_generator)
		}
	}

	return false
}

// checkGrammarObjectLiteralExpression evaluates the lone ClassLikeData() of the
// Pointer (a call without effect) as HasClassLikeData (4.1).
//
// storetranslate:pointer-hash 522134c50f2ff0f8 grammarchecks.go:Checker.checkGrammarObjectLiteralExpression
func (c *Checker) checkGrammarObjectLiteralExpression(node store.ObjectLiteralExpression, inDestructuring bool) bool {
	seen := make(map[string]DeclarationMeaning)

	var properties []store.Node
	if !node.Properties().IsNil() {
		properties = node.Properties().Nodes()
	}
	for _, prop := range properties {
		if prop.Kind() == ast.KindSpreadAssignment {
			spreadAssignment := prop.AsSpreadAssignment()
			if inDestructuring {
				// a rest property cannot be destructured any further
				expression := store.SkipParentheses(spreadAssignment.Expression())
				if store.IsArrayLiteralExpression(expression) || store.IsObjectLiteralExpression(expression) {
					return c.grammarErrorOnNode(spreadAssignment.Expression(), diagnostics.A_rest_element_cannot_contain_a_binding_pattern)
				}
			}
			continue
		}
		name := prop.Name()
		if name.Kind() == ast.KindComputedPropertyName {
			// If the name is not a ComputedPropertyName, the grammar checking will skip it
			c.checkGrammarComputedPropertyName(name)
		}

		if prop.Kind() == ast.KindShorthandPropertyAssignment && !inDestructuring {
			shorthandProp := prop.AsShorthandPropertyAssignment()
			if !shorthandProp.ObjectAssignmentInitializer().IsNil() {
				// having objectAssignmentInitializer is only valid in an ObjectAssignmentPattern.
				// Outside of destructuring, it is a syntax error.

				// Try to grab the last node prior to the initializer,
				// then error on the first token following (which should be the `=` token).
				var lastNodeBeforeInitializer store.Node
				store.Node(shorthandProp).ForEachChild(func(child store.Node) bool {
					if !store.SameNode(child, shorthandProp.ObjectAssignmentInitializer()) {
						lastNodeBeforeInitializer = child
						return false
					}
					return true
				})

				c.grammarErrorOnFirstToken(lastNodeBeforeInitializer, diagnostics.Did_you_mean_to_use_a_Colon_An_can_only_follow_a_property_name_when_the_containing_object_literal_is_part_of_a_destructuring_pattern)
			}
		}

		if name.Kind() == ast.KindPrivateIdentifier {
			c.grammarErrorOnNode(name, diagnostics.Private_identifiers_are_not_allowed_outside_class_bodies)
		}

		// Modifiers are never allowed on properties except for 'async' on a method declaration
		if modifiers := prop.ModifierNodes(); modifiers.Len() != 0 {
			if store.CanHaveModifiers(prop) {
				for i := range modifiers.Len() {
					mod := modifiers.At(i)
					if store.IsModifier(mod) && (mod.Kind() != ast.KindAsyncKeyword || prop.Kind() != ast.KindMethodDeclaration) {
						c.grammarErrorOnNode(mod, diagnostics.X_0_modifier_cannot_be_used_here, storescanner.GetTextOfNode(mod))
					}
				}
			} else if store.CanHaveIllegalModifiers(prop) {
				for i2 := range modifiers.Len() {
					mod := modifiers.At(i2)
					if store.IsModifier(mod) {
						c.grammarErrorOnNode(mod, diagnostics.X_0_modifier_cannot_be_used_here, storescanner.GetTextOfNode(mod))
					}
				}
			}
		}

		// ECMA-262 11.1.5 Object Initializer
		// If previous is not undefined then throw a SyntaxError exception if any of the following conditions are true
		// a.This production is contained in strict code and IsDataDescriptor(previous) is true and
		// IsDataDescriptor(propId.descriptor) is true.
		//    b.IsDataDescriptor(previous) is true and IsAccessorDescriptor(propId.descriptor) is true.
		//    c.IsAccessorDescriptor(previous) is true and IsDataDescriptor(propId.descriptor) is true.
		//    d.IsAccessorDescriptor(previous) is true and IsAccessorDescriptor(propId.descriptor) is true
		// and either both previous and propId.descriptor have[[Get]] fields or both previous and propId.descriptor have[[Set]] fields
		var currentKind DeclarationMeaning
		switch prop.Kind() {
		case ast.KindShorthandPropertyAssignment,
			ast.KindPropertyAssignment:
			var commonProp store.Node
			if prop.Kind() == ast.KindShorthandPropertyAssignment {
				prop.HasClassLikeData()
				commonProp = store.Node(prop.AsShorthandPropertyAssignment())
			} else {
				commonProp = store.Node(prop.AsPropertyAssignment())
			}

			// Grammar checking for computedPropertyName and shorthandPropertyAssignment
			c.checkGrammarForInvalidExclamationToken(commonProp.PostfixToken(), diagnostics.A_definite_assignment_assertion_is_not_permitted_in_this_context)
			c.checkGrammarForInvalidQuestionMark(commonProp.PostfixToken(), diagnostics.An_object_member_cannot_be_declared_optional)

			if name.Kind() == ast.KindNumericLiteral {
				c.checkGrammarNumericLiteral(name.AsNumericLiteral())
			}

			if name.Kind() == ast.KindBigIntLiteral {
				c.addErrorOrSuggestion(true, createDiagnosticForNode(name, diagnostics.A_bigint_literal_cannot_be_used_as_a_property_name))
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
			effectiveName, ok := c.getEffectivePropertyNameForPropertyNameNode(name)
			if !ok {
				continue
			}

			existingKind := seen[effectiveName]
			if existingKind == 0 {
				seen[effectiveName] = currentKind
			} else {
				if (currentKind&DeclarationMeaningMethod != 0) && (existingKind&DeclarationMeaningMethod != 0) {
					c.grammarErrorOnNode(name, diagnostics.Duplicate_identifier_0, storescanner.GetTextOfNode(name))
				} else if (currentKind&DeclarationMeaningPropertyAssignment != 0) && (existingKind&DeclarationMeaningPropertyAssignment != 0) {
					c.grammarErrorOnNode(name, diagnostics.An_object_literal_cannot_have_multiple_properties_with_the_same_name, storescanner.GetTextOfNode(name))
				} else if (currentKind&DeclarationMeaningGetOrSetAccessor != 0) && (existingKind&DeclarationMeaningGetOrSetAccessor != 0) {
					if existingKind != DeclarationMeaningGetOrSetAccessor && currentKind != existingKind {
						seen[effectiveName] = currentKind | existingKind
					} else {
						return c.grammarErrorOnNode(name, diagnostics.An_object_literal_cannot_have_multiple_get_Slashset_accessors_with_the_same_name)
					}
				} else {
					return c.grammarErrorOnNode(name, diagnostics.An_object_literal_cannot_have_property_and_accessor_with_the_same_name)
				}
			}
		}
	}

	return false
}

// checkGrammarAccessor tests FunctionLikeData() != nil with
// HasFunctionLikeData and reads its member on the node (4.1).
//
// storetranslate:pointer-hash 81bddfde1ad7a021 grammarchecks.go:Checker.checkGrammarAccessor
func (c *Checker) checkGrammarAccessor(accessor store.Node) bool {
	body := accessor.Body()
	if accessor.Flags()&ast.NodeFlagsAmbient == 0 && (accessor.Parent().Kind() != ast.KindTypeLiteral) && (accessor.Parent().Kind() != ast.KindInterfaceDeclaration) {
		if body.IsNil() && !store.HasSyntacticModifier(accessor, ast.ModifierFlagsAbstract) {
			return c.grammarErrorAtPos(accessor, int(accessor.End())-1, len(";"), diagnostics.X_0_expected, "{")
		}
	}
	if !body.IsNil() {
		if store.HasSyntacticModifier(accessor, ast.ModifierFlagsAbstract) {
			return c.grammarErrorOnNode(accessor, diagnostics.An_abstract_accessor_cannot_have_an_implementation)
		}
		if accessor.Parent().Kind() == ast.KindTypeLiteral || accessor.Parent().Kind() == ast.KindInterfaceDeclaration {
			return c.grammarErrorOnNode(body, diagnostics.An_implementation_cannot_be_declared_in_ambient_contexts)
		}
	}

	var typeParameters store.List
	if accessor.HasFunctionLikeData() {
		typeParameters = accessor.TypeParameters()
	}

	if !typeParameters.IsNil() {
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
		parameter := parameterNode.AsParameterDeclaration()
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
