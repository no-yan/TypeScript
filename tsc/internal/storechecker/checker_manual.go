package storechecker

import (
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/jsnum"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/storeevaluator"
	"github.com/microsoft/TypeScript/tsc/internal/storescanner"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// The P2 declarations of checker/checker.go:
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4. Each carries the hash of the Pointer declaration it replaces; a List
// version <F>List carries F's.

// checkSourceElementsList is checkSourceElements on a list (3.4, List
// version): the Pointer body with the List rules.
//
// storetranslate:pointer-hash f7f45356a90c2e5e checker.go:Checker.checkSourceElements
func (c *Checker) checkSourceElementsList(nodes store.List) {
	for i := range nodes.Len() {
		node := nodes.At(i)
		if c.isCanceled() {
			break
		}
		c.checkSourceElement(node)
	}
}

// checkTypeParametersList is checkTypeParameters on a list (3.4, List
// version): the Pointer body with the List rules.
//
// storetranslate:pointer-hash ab817d8640b147ef checker.go:Checker.checkTypeParameters
func (c *Checker) checkTypeParametersList(typeParameterDeclarations store.List) {
	seenDefault := false
	for i := range typeParameterDeclarations.Len() {
		node := typeParameterDeclarations.At(i)
		c.checkTypeParameter(node)
		defaultTypeNode := node.AsTypeParameterDeclaration().DefaultType()
		if !defaultTypeNode.IsNil() {
			seenDefault = true
			c.checkTypeParametersNotReferencedList(defaultTypeNode, typeParameterDeclarations, i)
		} else if seenDefault {
			c.error(node, diagnostics.Required_type_parameters_may_not_follow_optional_type_parameters)
		}
		for j := range i {
			if typeParameterDeclarations.At(j).Symbol() == node.Symbol() {
				c.error(node.Name(), diagnostics.Duplicate_identifier_0, storescanner.DeclarationNameToString(node.Name()))
			}
		}
	}
}

// checkTypeParametersNotReferencedList is checkTypeParametersNotReferenced on
// a list (3.4, List version): the Pointer body with the List rules.
//
// storetranslate:pointer-hash 2d98ca2989bee8ef checker.go:Checker.checkTypeParametersNotReferenced
func (c *Checker) checkTypeParametersNotReferencedList(root store.Node, typeParameters store.List, index int) {
	var visit func(store.Node) bool
	visit = func(node store.Node) bool {
		if store.IsTypeReferenceNode(node) {
			t := c.getTypeFromTypeReference(node)
			if t.flags&TypeFlagsTypeParameter != 0 {
				for i := index; i < typeParameters.Len(); i++ {
					if t.symbol == c.getSymbolOfDeclaration(typeParameters.At(i)) {
						c.error(node, diagnostics.Type_parameter_defaults_can_only_reference_previously_declared_type_parameters)
					}
				}
			}
		}
		return node.ForEachChild(visit)
	}
	visit(root)
}

// reportUnusedVariableDeclarationsList is reportUnusedVariableDeclarations on
// a list (3.4, List version): the Pointer body with the List rules.
//
// storetranslate:pointer-hash 0a993712199ab42f checker.go:Checker.reportUnusedVariableDeclarations
func (c *Checker) reportUnusedVariableDeclarationsList(declarations store.List) {
	for i := range declarations.Len() {
		declaration := declarations.At(i)
		name := declaration.Name()
		if !name.IsNil() && !store.IsParameterPropertyDeclaration(declaration, declaration.Parent()) && !store.IsThisParameter(declaration) {
			if store.IsBindingPattern(name) {
				c.reportUnusedBindingElements(name)
			} else if c.isUnreferencedVariableDeclaration(declaration) {
				c.reportUnusedVariable(declaration, NewDiagnosticForNode(name, diagnostics.X_0_is_declared_but_its_value_is_never_read, name.Text()))
			}
		}
	}
}

// getEntityNameForDecoratorMetadataFromTypeListList is
// getEntityNameForDecoratorMetadataFromTypeList on a list (3.4, List version):
// the Pointer body with the List rules.
//
// storetranslate:pointer-hash 3168d06927eaab68 checker.go:Checker.getEntityNameForDecoratorMetadataFromTypeList
func (c *Checker) getEntityNameForDecoratorMetadataFromTypeListList(typeNodes store.List) store.Node {
	var commonEntityName store.Node
	for i := range typeNodes.Len() {
		typeNode := typeNodes.At(i)
		if typeNode.Kind() == ast.KindNeverKeyword {
			continue // Always elide `never` from the union/intersection if possible
		}
		if !c.strictNullChecks && (typeNode.Kind() == ast.KindLiteralType && typeNode.AsLiteralTypeNode().Literal().Kind() == ast.KindNullKeyword || typeNode.Kind() == ast.KindUndefinedKeyword) {
			continue // Elide null and undefined from unions for metadata, just like what we did prior to the implementation of strict null checks
		}
		individualEntityName := c.getEntityNameForDecoratorMetadata(typeNode)
		if individualEntityName.IsNil() {
			// Individual is something like string number
			// So it would be serialized to either that type or object
			// Safe to return here
			return store.Node{}
		}

		if commonEntityName.IsNil() {
			commonEntityName = individualEntityName
		} else {
			// Note this is in sync with the transformation that happens for type node.
			// Keep this in sync with serializeUnionOrIntersectionType
			// Verify if they refer to same entity and is identifier
			// return undefined if they dont match because we would emit object
			if !store.IsIdentifier(commonEntityName) || !store.IsIdentifier(individualEntityName) || commonEntityName.AsIdentifier().Text() != individualEntityName.AsIdentifier().Text() {
				return store.Node{}
			}
		}
	}
	return commonEntityName
}

// getSpreadArgumentIndexList is getSpreadArgumentIndex on a list (3.4, List
// version): the Pointer body with the List rules.
//
// storetranslate:pointer-hash 26f5fc5b7e041051 checker.go:Checker.getSpreadArgumentIndex
func (c *Checker) getSpreadArgumentIndexList(args store.List) int {
	return store.FindIndexList(args, isSpreadArgument)
}

// appendTypeParametersList is appendTypeParameters on a list (3.4, List
// version): the Pointer body with the List rules.
//
// storetranslate:pointer-hash f8c24baf4381dbe9 checker.go:Checker.appendTypeParameters
func (c *Checker) appendTypeParametersList(typeParameters []*Type, declarations store.List) []*Type {
	for i := range declarations.Len() {
		declaration := declarations.At(i)
		typeParameters = core.AppendIfUnique(typeParameters, c.getDeclaredTypeOfTypeParameter(c.getSymbolOfDeclaration(declaration)))
	}
	return typeParameters
}

// getConstructorsForTypeArgumentsList is getConstructorsForTypeArguments on a
// list (3.4, List version): the Pointer body with the List rules.
//
// storetranslate:pointer-hash 7c235debcdfb5929 checker.go:Checker.getConstructorsForTypeArguments
func (c *Checker) getConstructorsForTypeArgumentsList(t *Type, typeArgumentNodes store.List, location store.Node) []*Signature {
	typeArgCount := typeArgumentNodes.Len()
	return core.Filter(c.getSignaturesOfType(t, SignatureKindConstruct), func(sig *Signature) bool {
		return typeArgCount >= c.getMinTypeArgumentCount(sig.typeParameters) && typeArgCount <= len(sig.typeParameters)
	})
}

// getInstantiatedConstructorsForTypeArgumentsList is
// getInstantiatedConstructorsForTypeArguments on a list (3.4, List version):
// the Pointer body with the List rules.
//
// storetranslate:pointer-hash 69e8493dd867e762 checker.go:Checker.getInstantiatedConstructorsForTypeArguments
func (c *Checker) getInstantiatedConstructorsForTypeArgumentsList(t *Type, typeArgumentNodes store.List, location store.Node) []*Signature {
	signatures := c.getConstructorsForTypeArgumentsList(t, typeArgumentNodes, location)
	typeArguments := store.MapList(typeArgumentNodes, c.getTypeFromTypeNode)
	return core.SameMap(signatures, func(sig *Signature) *Signature {
		if len(sig.typeParameters) != 0 {
			return c.getSignatureInstantiation(sig, typeArguments, store.IsInJSFile(location), nil)
		}
		return sig
	})
}

// checkTypeArgumentsList is checkTypeArguments on a list (3.4, List version):
// the Pointer body with the List rules.
//
// storetranslate:pointer-hash 5969694cab6d8003 checker.go:Checker.checkTypeArguments
func (c *Checker) checkTypeArgumentsList(signature *Signature, typeArgumentNodes store.List, reportErrors bool, headMessage *diagnostics.Message) []*Type {
	isJavaScript := store.IsInJSFile(signature.declaration)
	typeParameters := signature.typeParameters
	typeArgumentTypes := c.fillMissingTypeArguments(store.MapList(typeArgumentNodes, c.getTypeFromTypeNode), typeParameters, c.getMinTypeArgumentCount(typeParameters), isJavaScript)
	var mapper *TypeMapper
	for i := range typeArgumentNodes.Len() {
		debug.Assert(typeParameters[i] != nil, "Should not call checkTypeArguments with too many type arguments")
		constraint := c.getConstraintOfTypeParameter(typeParameters[i])
		if constraint != nil {
			typeArgumentHeadMessage := core.OrElse(headMessage, diagnostics.Type_0_does_not_satisfy_the_constraint_1)
			if mapper == nil {
				mapper = newTypeMapper(typeParameters, typeArgumentTypes)
			}
			typeArgument := typeArgumentTypes[i]
			var errorNode store.Node
			if reportErrors {
				errorNode = typeArgumentNodes.At(i)
			}
			var diags []*store.Diagnostic
			if !c.checkTypeAssignableToEx(typeArgument, c.getTypeWithThisArgument(c.instantiateType(constraint, mapper), typeArgument, false), errorNode, typeArgumentHeadMessage, &diags) {
				if len(diags) != 0 {
					diagnostic := diags[0]
					if headMessage != nil {
						diagnostic = store.NewDiagnosticChain(diagnostic, diagnostics.Type_0_does_not_satisfy_the_constraint_1)
					}
					c.addDiagnostic(diagnostic)
				}
				return nil
			}
		}
	}
	return typeArgumentTypes
}

// hasCorrectTypeArgumentArityList is hasCorrectTypeArgumentArity on a list
// (3.4, List version): the Pointer body with the List rules.
//
// storetranslate:pointer-hash c94335fc3f1613ea checker.go:Checker.hasCorrectTypeArgumentArity
func (c *Checker) hasCorrectTypeArgumentArityList(signature *Signature, typeArguments store.List) bool {
	// If the user supplied type arguments, but the number of type arguments does not match
	// the declared number of type parameters, the call has an incorrect arity.
	numTypeParameters := len(signature.typeParameters)
	minTypeArgumentCount := c.getMinTypeArgumentCount(signature.typeParameters)
	return typeArguments.Len() == 0 || typeArguments.Len() >= minTypeArgumentCount && typeArguments.Len() <= numTypeParameters
}

// initializeChecker resolves the NodeRefs of the file's ModuleAugmentations
// into a []Node for the list of lists it keeps (R3 resolves them only where a
// range or an index reads them).
//
// storetranslate:pointer-hash 3b37de1440a3ac4a checker.go:Checker.initializeChecker
func (c *Checker) initializeChecker() {
	// Initialize global symbol table
	var ambientModuleSymbols []*store.Symbol
	augmentations := make([][]store.Node, 0, len(c.files))
	for _, file := range c.files {
		if !store.IsExternalOrCommonJSModule(file) {
			// It is an error for a non-external-module (i.e. script) to declare its own `globalThis`.
			if fileGlobalThisSymbol := file.Locals()["globalThis"]; fileGlobalThisSymbol != nil {
				for _, d := range fileGlobalThisSymbol.Declarations {
					c.addDiagnostic(NewDiagnosticForNode(d, diagnostics.Declaration_name_conflicts_with_built_in_global_identifier_0, "globalThis"))
				}
			}
			for _, symbol := range file.Locals() {
				// We defer merging of global ambient module declarations since they may require other global symbols
				// and types to be resolved. See https://github.com/microsoft/TypeScript/tsc/issues/2953.
				if symbol.Flags&ast.SymbolFlagsModule != 0 && ast.IsAmbientModuleSymbolName(symbol.Name) {
					ambientModuleSymbols = append(ambientModuleSymbols, symbol)
				} else {
					c.mergeGlobalSymbol(symbol)
				}
			}
		}
		c.patternAmbientModules = append(c.patternAmbientModules, file.PatternAmbientModules()...)
		moduleAugmentations := make([]store.Node, len(file.ModuleAugmentations))
		for i, ref := range file.ModuleAugmentations {
			moduleAugmentations[i] = file.Store.Node(ref)
		}
		augmentations = append(augmentations, moduleAugmentations)
		if file.Symbol() != nil {
			// Merge in UMD exports with first-in-wins semantics (see #9771)
			for name, symbol := range file.GlobalExports() {
				if _, ok := c.globals[name]; !ok {
					c.globals[name] = symbol
				}
			}
		}
	}
	// We do global augmentations separately from module augmentations (and before creating global types) because they
	//  1. Affect global types. We won't have the correct global types until global augmentations are merged. Also,
	//  2. Module augmentation instantiation requires creating the type of a module, which, in turn, can require
	//       checking for an export or property on the module (if export=) which, in turn, can fall back to the
	//       apparent type of the module - either globalObjectType or globalFunctionType - which wouldn't exist if we
	//       did module augmentations prior to finalizing the global types.
	for _, list := range augmentations {
		for _, augmentation := range list {
			// Merge 'global' module augmentations. This needs to be done after global symbol table is initialized to
			// make sure that all ambient modules are indexed
			if store.IsGlobalScopeAugmentation(augmentation.Parent()) {
				c.mergeModuleAugmentation(augmentation)
			}
		}
	}
	c.addUndefinedToGlobalsOrErrorOnRedeclaration()
	c.valueSymbolLinks.Get(c.undefinedSymbol).resolvedType = c.undefinedWideningType
	c.valueSymbolLinks.Get(c.argumentsSymbol).resolvedType = c.getGlobalType("IArguments", 0 /*arity*/, true /*reportErrors*/)
	c.valueSymbolLinks.Get(c.unknownSymbol).resolvedType = c.errorType
	c.valueSymbolLinks.Get(c.globalThisSymbol).resolvedType = c.newObjectType(ObjectFlagsAnonymous, c.globalThisSymbol)
	// Initialize special types
	c.globalArrayType = c.getGlobalType("Array", 1 /*arity*/, true /*reportErrors*/)
	c.globalObjectType = c.getGlobalType("Object", 0 /*arity*/, true /*reportErrors*/)
	c.globalFunctionType = c.getGlobalType("Function", 0 /*arity*/, true /*reportErrors*/)
	c.globalCallableFunctionType = c.getGlobalStrictFunctionType("CallableFunction")
	c.globalNewableFunctionType = c.getGlobalStrictFunctionType("NewableFunction")
	c.globalStringType = c.getGlobalType("String", 0 /*arity*/, true /*reportErrors*/)
	c.globalNumberType = c.getGlobalType("Number", 0 /*arity*/, true /*reportErrors*/)
	c.globalBooleanType = c.getGlobalType("Boolean", 0 /*arity*/, true /*reportErrors*/)
	c.globalRegExpType = c.getGlobalType("RegExp", 0 /*arity*/, true /*reportErrors*/)
	c.anyArrayType = c.createArrayType(c.anyType)
	c.autoArrayType = c.createArrayType(c.autoType)
	if c.autoArrayType == c.emptyObjectType {
		// autoArrayType is used as a marker, so even if global Array type is not defined, it needs to be a unique type
		c.autoArrayType = c.newAnonymousType(nil, nil, nil, nil, nil)
	}
	c.globalReadonlyArrayType = c.getGlobalType("ReadonlyArray", 1 /*arity*/, false /*reportErrors*/)
	if c.globalReadonlyArrayType == c.emptyGenericType {
		c.globalReadonlyArrayType = c.globalArrayType
	}
	c.anyReadonlyArrayType = c.createTypeFromGenericGlobalType(c.globalReadonlyArrayType, []*Type{c.anyType})
	c.globalThisType = c.getGlobalType("ThisType", 1 /*arity*/, false /*reportErrors*/)
	// Now merge global ambient module declarations
	for _, symbol := range ambientModuleSymbols {
		c.mergeGlobalSymbol(symbol)
	}
	c.mergePatternAmbientModules()
	// merge _nonglobal_ module augmentations.
	// this needs to be done after global symbol table is initialized to make sure that all ambient modules are indexed
	for _, list := range augmentations {
		for _, augmentation := range list {
			if !store.IsGlobalScopeAugmentation(augmentation.Parent()) {
				c.mergeModuleAugmentation(augmentation)
			}
		}
	}
}

// checkAllCodePathsInNonVoidFunctionReturnOrThrow tests FunctionLikeData() !=
// nil with HasFunctionLikeData and reads its member on the node (4.1).
//
// storetranslate:pointer-hash ae669408b1363d59 checker.go:Checker.checkAllCodePathsInNonVoidFunctionReturnOrThrow
func (c *Checker) checkAllCodePathsInNonVoidFunctionReturnOrThrow(fn store.Node, returnType *Type) {
	functionFlags := store.GetFunctionFlags(fn)
	var t *Type
	if returnType != nil {
		t = c.unwrapReturnType(returnType, functionFlags)
	}
	// Functions with an explicitly specified return type that includes `void` or is exactly `any` or `undefined` don't
	// need any return statements.
	if t != nil && (c.maybeTypeOfKind(t, TypeFlagsVoid) || t.flags&(TypeFlagsAny|TypeFlagsUndefined) != 0) {
		return
	}
	// If all we have is a function signature, or an arrow function with an expression body, then there is nothing to check.
	// also if HasImplicitReturn flag is not set this means that all codepaths in function body end with return or throw
	if store.IsMethodSignatureDeclaration(fn) || store.NodeIsMissing(fn.Body()) || !store.IsBlock(fn.Body()) || !c.functionHasImplicitReturn(fn) {
		return
	}
	hasExplicitReturn := fn.Flags()&ast.NodeFlagsHasExplicitReturn != 0
	errorNode := fn.Type()
	if errorNode.IsNil() {
		if fn.HasFunctionLikeData() && !fn.FullSignature().IsNil() {
			errorNode = fn.FullSignature()
		}
	}
	if errorNode.IsNil() {
		errorNode = fn
	}
	switch {
	case t != nil && t.flags&TypeFlagsNever != 0:
		c.error(errorNode, diagnostics.A_function_returning_never_cannot_have_a_reachable_end_point)
	case t != nil && !hasExplicitReturn:
		// minimal check: function has syntactic return type annotation and no explicit return statements in the body
		// this function does not conform to the specification.
		c.error(errorNode, diagnostics.A_function_whose_declared_type_is_neither_undefined_void_nor_any_must_return_a_value)
	case t != nil && c.strictNullChecks && !c.isTypeAssignableTo(c.undefinedType, t):
		c.error(errorNode, diagnostics.Function_lacks_ending_return_statement_and_return_type_does_not_include_undefined)
	case c.compilerOptions.NoImplicitReturns == core.TSTrue:
		if t == nil {
			// If return type annotation is omitted check if function has any explicit return statements.
			// If it does not have any - its inferred return type is void - don't do any checks.
			// Otherwise get inferred return type from function body and report error only if it is not void / anytype
			if !hasExplicitReturn {
				return
			}
			inferredReturnType := c.getReturnTypeOfSignature(c.getSignatureFromDeclaration(fn))
			if c.isUnwrappedReturnTypeUndefinedVoidOrAny(fn, inferredReturnType) {
				return
			}
		}
		c.error(errorNode, diagnostics.Not_all_code_paths_return_a_value)
	}
}

// checkTypeParameterListsIdentical passes a function for the method value
// (*ast.Node).TypeParameters, whose Store version returns a List: the list
// copied with Nodes() (R4').
//
// storetranslate:pointer-hash 77c63216e3fd4859 checker.go:Checker.checkTypeParameterListsIdentical
func (c *Checker) checkTypeParameterListsIdentical(symbol *store.Symbol) {
	if len(symbol.Declarations) == 1 {
		return
	}
	links := c.declaredTypeLinks.Get(symbol)
	if !links.typeParametersChecked {
		links.typeParametersChecked = true
		declarations := c.getClassOrInterfaceDeclarationsOfSymbol(symbol)
		if len(declarations) <= 1 {
			return
		}
		t := c.getDeclaredTypeOfSymbol(symbol)
		if !c.areTypeParametersIdentical(declarations, t.AsInterfaceType().LocalTypeParameters(), func(node store.Node) []store.Node { return node.TypeParameters().Nodes() }) {
			// Report an error on every conflicting declaration.
			name := c.symbolToString(symbol)
			for _, declaration := range declarations {
				c.error(declaration.Name(), diagnostics.All_declarations_of_0_must_have_identical_type_parameters, name)
			}
		}
	}
}

// checkThisBeforeSuper tests FlowNodeData() != nil with HasFlowNodeData (4.1).
//
// storetranslate:pointer-hash 390aeec668f12127 checker.go:Checker.checkThisBeforeSuper
func (c *Checker) checkThisBeforeSuper(node store.Node, container store.Node, diagnosticMessage *diagnostics.Message) {
	containingClassDecl := container.Parent()
	baseTypeNode := store.GetClassExtendsHeritageElement(containingClassDecl)
	// If a containing class does not have extends clause or the class extends null
	// skip checking whether super statement is called before "this" accessing.
	if !baseTypeNode.IsNil() && !c.classDeclarationExtendsNull(containingClassDecl) {
		if node.HasFlowNodeData() && !c.isPostSuperFlowNode(node.FlowNode(), false /*noCacheCheck*/) {
			c.error(node, diagnosticMessage)
		}
	}
}

// getSymbolOfNode tests DeclarationData() != nil with HasDeclarationData and
// reads the symbol on the node (4.1).
//
// storetranslate:pointer-hash 57b2a2bd38f5e3c5 checker.go:Checker.getSymbolOfNode
func (c *Checker) getSymbolOfNode(node store.Node) *store.Symbol {
	if node.HasDeclarationData() && node.Symbol() != nil {
		return c.getMergedSymbol(c.getLateBoundSymbol(node.Symbol()))
	}
	return nil
}

// checkIfExpressionRefinesParameter makes no flow node: the Pointer makes a
// TrueCondition and a FalseCondition on the expression, and a Start when the
// expression has no flow, but the Store's flows are the rows of a source
// Bound, shared by the checkers and sealed. The synthetic flows are only ever
// the start of the walk, and getTypeAtFlowNode on a Condition calls
// getTypeAtFlowCondition, which reads Flags, Node and Antecedent; they are not
// Shared. So the condition is evaluated directly, true then false
// (getFlowTypeOfReferenceUnderCondition), with a nil antecedent for the
// Pointer's Start (internal/ast/docs/store-checker-7d2a-implementation-report-20260930.md
// 8.1, store-checker-7d2b1-implementation-instructions-20261001.md 3.1).
//
// storetranslate:pointer-hash 867f582282b847d0 checker.go:Checker.checkIfExpressionRefinesParameter
func (c *Checker) checkIfExpressionRefinesParameter(fn store.Node, expr store.Node, param store.Node, initType *Type) *Type {
	antecedent := getFlowNodeOfNode(expr)
	if antecedent.IsNil() && store.IsReturnStatement(expr.Parent()) {
		antecedent = getFlowNodeOfNode(expr.Parent())
	}
	trueType := c.getFlowTypeOfReferenceUnderCondition(param.Name(), initType, initType, fn, antecedent, expr, true /*assumeTrue*/)
	if trueType == initType {
		return nil
	}
	// "x is T" means that x is T if and only if it returns true. If it returns false then x is not T.
	// This means that if the function is called with an argument of type trueType, there can't be anything left in the `else` branch. It must reduce to `never`.
	falseSubtype := c.getReducedType(c.getFlowTypeOfReferenceUnderCondition(param.Name(), initType, trueType, fn, antecedent, expr, false /*assumeTrue*/))
	if falseSubtype.flags&TypeFlagsNever != 0 {
		return trueType
	}
	return nil
}

// checkKindsOfPropertyMemberOverrides reads back the keys of a map keyed by a
// node, which a NodeKey does not give: the nodes are kept in a second map of
// the same keys.
//
// storetranslate:pointer-hash 4f6ad5838dfa80ad checker.go:Checker.checkKindsOfPropertyMemberOverrides
func (c *Checker) checkKindsOfPropertyMemberOverrides(t *Type, baseType *Type) {
	// TypeScript 1.0 spec (April 2014): 8.2.3
	// A derived class inherits all members from its base class it doesn't override.
	// Inheritance means that a derived class implicitly contains all non - overridden members of the base class.
	// Both public and private property members are inherited, but only public property members can be overridden.
	// A property member in a derived class is said to override a property member in a base class
	// when the derived class property member has the same name and kind(instance or static)
	// as the base class property member.
	// The type of an overriding property member must be assignable(section 3.8.4)
	// to the type of the overridden property member, or otherwise a compile - time error occurs.
	// Base class instance member functions can be overridden by derived class instance member functions,
	// but not by other kinds of members.
	// Base class instance member variables and accessors can be overridden by
	// derived class instance member variables and accessors, but not by other kinds of members.
	// NOTE: assignability is checked in checkClassDeclaration
	type MemberInfo struct {
		missedProperties []string
		baseTypeName     string
		typeName         string
	}
	var notImplementedInfo map[store.NodeKey]MemberInfo
	var notImplementedNodes map[store.NodeKey]store.Node
basePropertyCheck:
	for _, baseProperty := range c.getPropertiesOfType(baseType) {
		base := c.getTargetSymbol(baseProperty)
		if base.Flags&ast.SymbolFlagsPrototype != 0 {
			continue
		}
		baseSymbol := c.getPropertyOfObjectType(t, base.Name)
		if baseSymbol == nil {
			continue
		}
		derived := c.getTargetSymbol(baseSymbol)
		baseDeclarationFlags := getDeclarationModifierFlagsFromSymbol(base)
		// In order to resolve whether the inherited method was overridden in the base class or not,
		// we compare the Symbols obtained. Since getTargetSymbol returns the symbol on the *uninstantiated*
		// type declaration, derived and base resolve to the same symbol even in the case of generic classes.
		if derived == base {
			// derived class inherits base without override/redeclaration.
			if baseDeclarationFlags&ast.ModifierFlagsAbstract != 0 {
				// It is an error to inherit an abstract member without implementing it or being declared abstract.
				// If there is no declaration for the derived class (as in the case of class expressions),
				// then the class cannot be declared abstract.
				derivedClassDecl := store.GetClassLikeDeclarationOfSymbol(t.symbol)
				if derivedClassDecl.IsNil() || !store.HasSyntacticModifier(derivedClassDecl, ast.ModifierFlagsAbstract) {
					// Searches other base types for a declaration that would satisfy the inherited abstract member.
					// (The class may have more than one base type via declaration merging with an interface with the
					// same name.)
					for _, otherBaseType := range c.getBaseTypes(t) {
						if otherBaseType == baseType {
							continue
						}
						if baseSymbol := c.getPropertyOfObjectType(otherBaseType, base.Name); baseSymbol != nil && base != c.getTargetSymbol(baseSymbol) {
							// Derived property exists elsewhere.
							continue basePropertyCheck
						}
					}
					baseTypeName := c.TypeToString(baseType)
					typeName := c.TypeToString(t)
					missedProperties := append(notImplementedInfo[derivedClassDecl.Key()].missedProperties, c.symbolToString(baseProperty))
					if notImplementedInfo == nil {
						notImplementedInfo = make(map[store.NodeKey]MemberInfo)
						notImplementedNodes = make(map[store.NodeKey]store.Node)
					}
					notImplementedNodes[derivedClassDecl.Key()] = derivedClassDecl
					notImplementedInfo[derivedClassDecl.Key()] = MemberInfo{
						baseTypeName:     baseTypeName,
						typeName:         typeName,
						missedProperties: missedProperties,
					}
				}
			}
		} else {
			// derived overrides base.
			derivedDeclarationFlags := getDeclarationModifierFlagsFromSymbol(derived)
			if baseDeclarationFlags&ast.ModifierFlagsPrivate != 0 || derivedDeclarationFlags&ast.ModifierFlagsPrivate != 0 {
				// either base or derived property is private - not override, skip it
				continue
			}
			var errorMessage *diagnostics.Message
			basePropertyFlags := base.Flags & ast.SymbolFlagsPropertyOrAccessor
			derivedPropertyFlags := derived.Flags & ast.SymbolFlagsPropertyOrAccessor
			if basePropertyFlags != 0 && derivedPropertyFlags != 0 {
				// property/accessor is overridden with property/accessor
				if base.CheckFlags&ast.CheckFlagsMapped != 0 ||
					!derived.ValueDeclaration.IsNil() && store.IsBinaryExpression(derived.ValueDeclaration) ||
					c.arePropertiesAbstractOrInterface(base, baseDeclarationFlags) {
					// when the base property is abstract or from an interface, base/derived flags don't need to match
					// for intersection properties, this must be true of *any* of the declarations, for others it must be true of *all*
					// same when the derived property is from an assignment
					continue
				}
				overriddenInstanceProperty := basePropertyFlags != ast.SymbolFlagsProperty && derivedPropertyFlags == ast.SymbolFlagsProperty
				overriddenInstanceAccessor := basePropertyFlags == ast.SymbolFlagsProperty && derivedPropertyFlags != ast.SymbolFlagsProperty
				if overriddenInstanceProperty || overriddenInstanceAccessor {
					errorMessage := core.IfElse(overriddenInstanceProperty,
						diagnostics.X_0_is_defined_as_an_accessor_in_class_1_but_is_overridden_here_in_2_as_an_instance_property,
						diagnostics.X_0_is_defined_as_a_property_in_class_1_but_is_overridden_here_in_2_as_an_accessor)
					c.error(store.OrElse(store.GetNameOfDeclaration(derived.ValueDeclaration), derived.ValueDeclaration), errorMessage, c.symbolToString(base), c.TypeToString(baseType), c.TypeToString(t))
				} else if c.compilerOptions.GetUseDefineForClassFields() {
					uninitialized := core.Find(derived.Declarations, func(d store.Node) bool {
						return store.IsPropertyDeclaration(d) && d.Initializer().IsNil()
					})
					if !uninitialized.IsNil() && derived.Flags&ast.SymbolFlagsTransient == 0 && baseDeclarationFlags&ast.ModifierFlagsAbstract == 0 && derivedDeclarationFlags&ast.ModifierFlagsAbstract == 0 && !core.Some(derived.Declarations, func(d store.Node) bool {
						return d.Flags()&ast.NodeFlagsAmbient != 0
					}) {
						constructor := store.FindConstructorDeclaration(store.GetClassLikeDeclarationOfSymbol(t.symbol))
						propName := uninitialized.Name()
						if isExclamationToken(uninitialized.PostfixToken()) || constructor.IsNil() || !store.IsIdentifier(propName) || !c.strictNullChecks || !c.isPropertyInitializedInConstructor(propName, t, constructor) {
							errorMessage := diagnostics.Property_0_will_overwrite_the_base_property_in_1_If_this_is_intentional_add_an_initializer_Otherwise_add_a_declare_modifier_or_remove_the_redundant_declaration
							c.error(store.OrElse(store.GetNameOfDeclaration(derived.ValueDeclaration), derived.ValueDeclaration), errorMessage, c.symbolToString(base), c.TypeToString(baseType))
						}
					}
				}
				// correct case
				continue
			} else if isPrototypeProperty(base) {
				if isPrototypeProperty(derived) || derived.Flags&ast.SymbolFlagsProperty != 0 {
					// method is overridden with method or property -- correct case
					continue
				} else {
					errorMessage = diagnostics.Class_0_defines_instance_member_function_1_but_extended_class_2_defines_it_as_instance_member_accessor
				}
			} else if base.Flags&ast.SymbolFlagsAccessor != 0 {
				errorMessage = diagnostics.Class_0_defines_instance_member_accessor_1_but_extended_class_2_defines_it_as_instance_member_function
			} else {
				errorMessage = diagnostics.Class_0_defines_instance_member_property_1_but_extended_class_2_defines_it_as_instance_member_function
			}
			c.error(store.OrElse(store.GetNameOfDeclaration(derived.ValueDeclaration), derived.ValueDeclaration), errorMessage, c.TypeToString(baseType), c.symbolToString(base), c.TypeToString(t))
		}
	}
	for errorNodeKey, memberInfo := range notImplementedInfo {
		errorNode := notImplementedNodes[errorNodeKey]
		switch {
		case len(memberInfo.missedProperties) == 1:
			missedProperty := memberInfo.missedProperties[0]
			if store.IsClassExpression(errorNode) {
				c.error(errorNode, diagnostics.Non_abstract_class_expression_does_not_implement_inherited_abstract_member_0_from_class_1, missedProperty, memberInfo.baseTypeName)
			} else {
				c.error(errorNode, diagnostics.Non_abstract_class_0_does_not_implement_inherited_abstract_member_1_from_class_2, memberInfo.typeName, missedProperty, memberInfo.baseTypeName)
			}
		case len(memberInfo.missedProperties) > 5:
			missedProperties := strings.Join(core.Map(memberInfo.missedProperties[:4], func(prop string) string { return "'" + prop + "'" }), ", ")
			remainingMissedProperties := len(memberInfo.missedProperties) - 4
			if store.IsClassExpression(errorNode) {
				c.error(errorNode, diagnostics.Non_abstract_class_expression_is_missing_implementations_for_the_following_members_of_0_Colon_1_and_2_more, memberInfo.baseTypeName, missedProperties, remainingMissedProperties)
			} else {
				c.error(errorNode, diagnostics.Non_abstract_class_0_is_missing_implementations_for_the_following_members_of_1_Colon_2_and_3_more, memberInfo.typeName, memberInfo.baseTypeName, missedProperties, remainingMissedProperties)
			}
		default:
			missedProperties := strings.Join(core.Map(memberInfo.missedProperties, func(prop string) string { return "'" + prop + "'" }), ", ")
			if store.IsClassExpression(errorNode) {
				c.error(errorNode, diagnostics.Non_abstract_class_expression_is_missing_implementations_for_the_following_members_of_0_Colon_1, memberInfo.baseTypeName, missedProperties)
			} else {
				c.error(errorNode, diagnostics.Non_abstract_class_0_is_missing_implementations_for_the_following_members_of_1_Colon_2, memberInfo.typeName, memberInfo.baseTypeName, missedProperties)
			}
		}
	}
}

// findResolutionCycleStartIndex compares the targets, TypeSystemEntity (any),
// with SameNode when both are a Node: pushTypeResolution puts a declaration
// in one (TypeSystemPropertyNameInitializerIsUndefined), and == of two
// interfaces holding a Node panics, a Node being uncomparable. Any other pair
// is compared with ==, false when the dynamic types differ
// (store-checker-7d2b2-implementation-instructions-20261001.md 3.3).
//
// storetranslate:pointer-hash 7215403e5af99e9c checker.go:Checker.findResolutionCycleStartIndex
func (c *Checker) findResolutionCycleStartIndex(target TypeSystemEntity, propertyName TypeSystemPropertyName) int {
	for i := len(c.typeResolutions) - 1; i >= c.resolutionStart; i-- {
		resolution := &c.typeResolutions[i]
		if c.typeResolutionHasProperty(resolution) {
			return -1
		}
		var sameTarget bool
		if node, ok := resolution.target.(store.Node); ok {
			targetNode, ok := target.(store.Node)
			sameTarget = ok && store.SameNode(node, targetNode)
		} else {
			sameTarget = resolution.target == target
		}
		if sameTarget && resolution.propertyName == propertyName {
			return i
		}
	}
	return -1
}

// checkUnusedLocalsAndParameters reads back the keys of a map keyed by a
// node: as checkKindsOfPropertyMemberOverrides.
//
// storetranslate:pointer-hash 6b7e447d707db873 checker.go:Checker.checkUnusedLocalsAndParameters
func (c *Checker) checkUnusedLocalsAndParameters(node store.Node) {
	var variableParents store.NodeSet
	var importClauses map[store.NodeKey][]store.Node
	var importClauseNodes map[store.NodeKey]store.Node
	for _, local := range node.Locals() {
		referenceKinds := c.symbolReferenceLinks.Get(local).referenceKinds
		if local.Flags&ast.SymbolFlagsTypeParameter != 0 && (local.Flags&ast.SymbolFlagsVariable == 0 || referenceKinds&ast.SymbolFlagsVariable != 0) ||
			local.Flags&ast.SymbolFlagsTypeParameter == 0 && (referenceKinds != 0 || local.ExportSymbol != nil ||
				local.Flags&ast.SymbolFlagsModuleExports != 0) {
			continue
		}
		for _, declaration := range local.Declarations {
			switch {
			case store.IsVariableDeclaration(declaration) || store.IsParameterDeclaration(declaration) || store.IsBindingElement(declaration):
				variableParents.Add(store.GetRootDeclaration(declaration).Parent())
			case store.IsImportClause(declaration) || store.IsImportSpecifier(declaration) || store.IsNamespaceImport(declaration):
				if !isIdentifierThatStartsWithUnderscore(declaration.Name()) {
					if importClauses == nil {
						importClauses = make(map[store.NodeKey][]store.Node)
						importClauseNodes = make(map[store.NodeKey]store.Node)
					}
					importClause := importClauseFromImported(declaration)
					importClauseNodes[importClause.Key()] = importClause
					importClauses[importClause.Key()] = append(importClauses[importClause.Key()], declaration)
				}
			default:
				if !store.IsTypeParameterDeclaration(declaration) && !store.IsAmbientModule(declaration) {
					c.reportUnusedLocal(declaration, store.SymbolName(local))
				}
			}
		}
	}
	for declaration := range variableParents.Keys() {
		if store.IsVariableDeclarationList(declaration) {
			c.reportUnusedVariables(declaration)
		} else {
			c.reportUnusedParameters(declaration)
		}
	}
	for declarationKey, unuseds := range importClauses {
		c.reportUnusedImports(importClauseNodes[declarationKey], unuseds)
	}
}

// Program is the Pointer interface with IDSpace (5.1): the adapter of 2b
// implements it. The resolved modules are the program's, as the Pointer's
// (their diagnostics are the resolver's *ast.Diagnostic, as the parse
// diagnostics of a store.File are, R11).
//
// storetranslate:pointer-hash 0cdd6935a2e99410 checker.go:Program
type Program interface {
	Host
	Options() *core.CompilerOptions
	SourceFiles() []*store.File
	BindSourceFiles()
	FileExists(fileName string) bool
	GetSourceFile(fileName string) *store.File
	GetSourceFileForResolvedModule(fileName string) *store.File
	GetEmitModuleFormatOfFile(sourceFile ast.HasFileName) core.ModuleKind
	GetEmitSyntaxForUsageLocation(sourceFile ast.HasFileName, usageLocation store.Node) core.ResolutionMode
	GetImpliedNodeFormatForEmit(sourceFile ast.HasFileName) core.ModuleKind
	GetResolvedModule(currentSourceFile ast.HasFileName, moduleReference string, mode core.ResolutionMode) *module.ResolvedModule
	GetResolvedModules() map[tspath.Path]module.ModeAwareCache[*module.ResolvedModule]
	GetPackagesMap() map[string]bool
	GetSourceFileMetaData(path tspath.Path) ast.SourceFileMetaData
	GetJSXRuntimeImportSpecifier(path tspath.Path) (moduleReference string, specifier store.Node)
	GetImportHelpersImportSpecifier(path tspath.Path) store.Node
	SourceFileMayBeEmitted(sourceFile *store.File, forceDtsEmit bool) bool
	IsSourceFileDefaultLibrary(path tspath.Path) bool
	GetProjectReferenceFromOutputDts(path tspath.Path) *tsoptions.SourceOutputAndProjectReference
	GetRedirectForResolution(file ast.HasFileName) *tsoptions.ParsedCommandLine
	CommonSourceDirectory() string
	// IDSpace is the program's StoreID namespace: the checker's region is made
	// in it (5.1).
	IDSpace() *store.IDSpace
}

// NewChecker makes the checker's region and its Factory (5.1): the Store's
// Factory has no zero value. The IDSpace is the program's, not the first
// file's: a program may have no file.
//
// storetranslate:pointer-hash 8fb66510eaaa18d2 checker.go:NewChecker
func NewChecker(program Program, tracer *Tracer) (*Checker, *sync.Mutex) {
	program.BindSourceFiles()

	c := &Checker{}
	c.id = nextCheckerID.Add(1)
	c.tracer = tracer
	c.program = program
	region := store.NewNodeRegion(program.IDSpace())
	c.factory = store.NewFactory(region, synthStoreCapacity, store.FactoryHooks{})
	c.compilerOptions = program.Options()
	c.files = program.SourceFiles()
	c.fileIndexMap = createFileIndexMap(c.files)
	c.compareSymbols = c.compareSymbolsWorker           // Closure optimization
	c.compareSymbolChains = c.compareSymbolChainsWorker // Closure optimization
	c.languageVersion = c.compilerOptions.GetEmitScriptTarget()
	c.moduleKind = c.compilerOptions.GetEmitModuleKind()
	c.moduleResolutionKind = c.compilerOptions.GetModuleResolutionKind()
	c.legacyDecorators = c.compilerOptions.ExperimentalDecorators == core.TSTrue
	c.emitStandardClassFields = c.compilerOptions.GetEmitStandardClassFields()
	c.strictNullChecks = c.compilerOptions.GetStrictOptionValue(c.compilerOptions.StrictNullChecks)
	c.strictFunctionTypes = c.compilerOptions.GetStrictOptionValue(c.compilerOptions.StrictFunctionTypes)
	c.strictBindCallApply = c.compilerOptions.GetStrictOptionValue(c.compilerOptions.StrictBindCallApply)
	c.strictPropertyInitialization = c.compilerOptions.GetStrictOptionValue(c.compilerOptions.StrictPropertyInitialization)
	c.strictBuiltinIteratorReturn = c.compilerOptions.GetStrictOptionValue(c.compilerOptions.StrictBuiltinIteratorReturn)
	c.noImplicitAny = c.compilerOptions.GetStrictOptionValue(c.compilerOptions.NoImplicitAny)
	c.noImplicitThis = c.compilerOptions.GetStrictOptionValue(c.compilerOptions.NoImplicitThis)
	c.useUnknownInCatchVariables = c.compilerOptions.GetStrictOptionValue(c.compilerOptions.UseUnknownInCatchVariables)
	c.exactOptionalPropertyTypes = c.compilerOptions.ExactOptionalPropertyTypes == core.TSTrue
	c.canCollectSymbolAliasAccessibilityData = c.compilerOptions.VerbatimModuleSyntax.IsFalseOrUnknown()
	c.arrayVariances = []VarianceFlags{VarianceFlagsCovariant}
	c.globals = make(store.SymbolTable, countGlobalSymbols(c.files))
	c.evaluate = storeevaluator.NewEvaluator(c.evaluateEntity, ast.OEKParentheses)
	c.stringLiteralTypes = make(map[string]*Type)
	c.numberLiteralTypes = make(map[jsnum.Number]*Type)
	c.bigintLiteralTypes = make(map[jsnum.PseudoBigInt]*Type)
	c.enumLiteralTypes = make(map[EnumLiteralKey]*Type)
	c.enumNaNLiteralTypes = make(map[*store.Symbol]*Type)
	c.indexedAccessTypes = make(map[CacheHashKey]*Type)
	c.templateLiteralTypes = make(map[CacheHashKey]*Type)
	c.stringMappingTypes = make(map[StringMappingKey]*Type)
	c.uniqueESSymbolTypes = make(map[*store.Symbol]*Type)
	c.thisExpandoKinds = make(map[*store.Symbol]thisAssignmentDeclarationKind)
	c.thisExpandoLocations = make(map[*store.Symbol]store.Node)
	c.subtypeReductionCache = make(map[CacheHashKey][]*Type)
	c.cachedTypes = make(map[CachedTypeKey]*Type)
	c.cachedSignatures = make(map[CachedSignatureKey]*Signature)
	c.undefinedProperties = make(map[string]*store.Symbol)
	c.narrowedTypes = make(map[NarrowedTypeKey]*Type)
	c.assignmentReducedTypes = make(map[AssignmentReducedKey]*Type)
	c.discriminatedContextualTypes = make(map[DiscriminatedContextualTypeKey]*Type)
	c.instantiationExpressionTypes = make(map[InstantiationExpressionKey]*Type)
	c.substitutionTypes = make(map[SubstitutionTypeKey]*Type)
	c.reverseMappedCache = make(map[ReverseMappedTypeKey]*Type)
	c.reverseHomomorphicMappedCache = make(map[ReverseMappedTypeKey]*Type)
	c.iterationTypesCache = make(map[IterationTypesKey]IterationTypes)
	c.undefinedSymbol = c.newSymbol(ast.SymbolFlagsProperty, "undefined")
	c.argumentsSymbol = c.newSymbol(ast.SymbolFlagsProperty, "arguments")
	c.requireSymbol = c.newSymbol(ast.SymbolFlagsProperty, "require")
	c.unknownSymbol = c.newSymbol(ast.SymbolFlagsProperty, "unknown")
	c.unresolvedSymbols = make(map[string]*store.Symbol)
	c.errorTypes = make(map[CacheHashKey]*Type)
	c.moduleSymbols = make(map[store.NodeKey]*store.Symbol)
	c.globalThisSymbol = c.newSymbolEx(ast.SymbolFlagsModule, "globalThis", ast.CheckFlagsReadonly)
	c.globalThisSymbol.Exports = c.globals
	c.globals[c.globalThisSymbol.Name] = c.globalThisSymbol
	c.resolveName = c.createNameResolver().Resolve
	c.resolveNameForSymbolSuggestion = c.createNameResolverForSuggestion().Resolve
	c.tupleTypes = make(map[CacheHashKey]*Type)
	c.unionTypes = make(map[CacheHashKey]*Type)
	c.unionOfUnionTypes = make(map[UnionOfUnionKey]*Type)
	c.intersectionTypes = make(map[CacheHashKey]*Type)
	c.propertiesTypes = make(map[PropertiesTypesKey]*Type)
	c.mergedSymbols = make(map[*store.Symbol]*store.Symbol)
	c.patternForType = make(map[*Type]store.Node)
	c.contextFreeTypes = make(map[store.NodeKey]*Type)
	c.anyType = c.newIntrinsicType(TypeFlagsAny, "any")
	c.autoType = c.newIntrinsicTypeEx(TypeFlagsAny, "any", ObjectFlagsNonInferrableType)
	c.wildcardType = c.newIntrinsicType(TypeFlagsAny, "any")
	c.blockedStringType = c.newIntrinsicType(TypeFlagsAny, "any")
	c.errorType = c.newIntrinsicType(TypeFlagsAny, "error")
	c.unresolvedType = c.newIntrinsicType(TypeFlagsAny, "unresolved")
	c.nonInferrableAnyType = c.newIntrinsicTypeEx(TypeFlagsAny, "any", ObjectFlagsContainsWideningType)
	c.intrinsicMarkerType = c.newIntrinsicType(TypeFlagsAny, "intrinsic")
	c.unknownType = c.newIntrinsicType(TypeFlagsUnknown, "unknown")
	c.undefinedType = c.newIntrinsicType(TypeFlagsUndefined, "undefined")
	c.undefinedWideningType = c.createWideningType(c.undefinedType)
	c.missingType = c.newIntrinsicType(TypeFlagsUndefined, "undefined")
	c.undefinedOrMissingType = core.IfElse(c.exactOptionalPropertyTypes, c.missingType, c.undefinedType)
	c.optionalType = c.newIntrinsicType(TypeFlagsUndefined, "undefined")
	c.nullType = c.newIntrinsicType(TypeFlagsNull, "null")
	c.nullWideningType = c.createWideningType(c.nullType)
	c.stringType = c.newIntrinsicType(TypeFlagsString, "string")
	c.numberType = c.newIntrinsicType(TypeFlagsNumber, "number")
	c.bigintType = c.newIntrinsicType(TypeFlagsBigInt, "bigint")
	c.regularFalseType = c.newLiteralType(TypeFlagsBooleanLiteral, false, nil)
	c.falseType = c.newLiteralType(TypeFlagsBooleanLiteral, false, c.regularFalseType)
	c.regularFalseType.AsLiteralType().freshType = c.falseType
	c.falseType.AsLiteralType().freshType = c.falseType
	c.regularTrueType = c.newLiteralType(TypeFlagsBooleanLiteral, true, nil)
	c.trueType = c.newLiteralType(TypeFlagsBooleanLiteral, true, c.regularTrueType)
	c.regularTrueType.AsLiteralType().freshType = c.trueType
	c.trueType.AsLiteralType().freshType = c.trueType
	c.booleanType = c.getUnionType([]*Type{c.regularFalseType, c.regularTrueType})
	c.esSymbolType = c.newIntrinsicType(TypeFlagsESSymbol, "symbol")
	c.voidType = c.newIntrinsicType(TypeFlagsVoid, "void")
	c.neverType = c.newIntrinsicType(TypeFlagsNever, "never")
	c.silentNeverType = c.newIntrinsicTypeEx(TypeFlagsNever, "never", ObjectFlagsNonInferrableType)
	c.implicitNeverType = c.newIntrinsicType(TypeFlagsNever, "never")
	c.unreachableNeverType = c.newIntrinsicType(TypeFlagsNever, "never")
	c.nonPrimitiveType = c.newIntrinsicType(TypeFlagsNonPrimitive, "object")
	c.stringOrNumberType = c.getUnionType([]*Type{c.stringType, c.numberType})
	c.stringNumberSymbolType = c.getUnionType([]*Type{c.stringType, c.numberType, c.esSymbolType})
	c.numberOrBigIntType = c.getUnionType([]*Type{c.numberType, c.bigintType})
	c.numericStringType = c.getTemplateLiteralType([]string{"", ""}, []*Type{c.numberType}) // The `${number}` type
	c.templateConstraintType = c.getUnionType([]*Type{c.stringType, c.numberType, c.booleanType, c.bigintType, c.nullType, c.undefinedType})
	c.uniqueLiteralType = c.newIntrinsicType(TypeFlagsNever, "never") // Special `never` flagged by union reduction to behave as a literal
	c.uniqueLiteralMapper = newFunctionTypeMapper(c.getUniqueLiteralTypeForTypeParameter)
	c.reportUnreliableMapper = newFunctionTypeMapper(c.reportUnreliableWorker)
	c.reportUnmeasurableMapper = newFunctionTypeMapper(c.reportUnmeasurableWorker)
	c.restrictiveMapper = newFunctionTypeMapper(c.restrictiveMapperWorker)
	c.permissiveMapper = newFunctionTypeMapper(c.permissiveMapperWorker)
	c.emptyObjectType = c.newAnonymousType(nil /*symbol*/, nil, nil, nil, nil)
	c.emptyJsxObjectType = c.newAnonymousType(nil /*symbol*/, nil, nil, nil, nil)
	c.emptyFreshJsxObjectType = c.newAnonymousType(nil /*symbol*/, nil, nil, nil, nil)
	c.emptyTypeLiteralType = c.newAnonymousType(c.newSymbol(ast.SymbolFlagsTypeLiteral, ast.InternalSymbolNameType), nil, nil, nil, nil)
	c.unknownEmptyObjectType = c.newAnonymousType(nil /*symbol*/, nil, nil, nil, nil)
	c.unknownUnionType = c.createUnknownUnionType()
	c.emptyGenericType = c.newAnonymousType(nil /*symbol*/, nil, nil, nil, nil)
	c.emptyGenericType.AsObjectType().instantiations = make(map[CacheHashKey]*Type)
	c.anyFunctionType = c.newAnonymousType(nil /*symbol*/, nil, nil, nil, nil)
	c.anyFunctionType.objectFlags |= ObjectFlagsNonInferrableType
	c.noConstraintType = c.newAnonymousType(nil /*symbol*/, nil, nil, nil, nil)
	c.circularConstraintType = c.newAnonymousType(nil /*symbol*/, nil, nil, nil, nil)
	c.resolvingDefaultType = c.newAnonymousType(nil /*symbol*/, nil, nil, nil, nil)
	c.markerSuperType = c.newTypeParameter(nil)
	c.markerSubType = c.newTypeParameter(nil)
	c.markerSubType.AsTypeParameter().constraint = c.markerSuperType
	c.markerOtherType = c.newTypeParameter(nil)
	c.markerSuperTypeForCheck = c.newTypeParameter(nil)
	c.markerSubTypeForCheck = c.newTypeParameter(nil)
	c.markerSubTypeForCheck.AsTypeParameter().constraint = c.markerSuperTypeForCheck
	c.noTypePredicate = &TypePredicate{kind: TypePredicateKindIdentifier, parameterIndex: 0, parameterName: "<<unresolved>>", t: c.anyType}
	c.anySignature = c.newSignature(SignatureFlagsNone, store.Node{}, nil, nil, nil, c.anyType, nil, 0)
	c.unknownSignature = c.newSignature(SignatureFlagsNone, store.Node{}, nil, nil, nil, c.errorType, nil, 0)
	c.resolvingSignature = c.newSignature(SignatureFlagsNone, store.Node{}, nil, nil, nil, c.anyType, nil, 0)
	c.silentNeverSignature = c.newSignature(SignatureFlagsNone, store.Node{}, nil, nil, nil, c.silentNeverType, nil, 0)
	c.cachedArgumentsReferenced = make(map[store.NodeKey]bool)
	c.enumNumberIndexInfo = &IndexInfo{keyType: c.numberType, valueType: c.stringType, isReadonly: true}
	c.anyBaseTypeIndexInfo = &IndexInfo{keyType: c.stringType, valueType: c.anyType, isReadonly: false}
	c.emptyStringType = c.getStringLiteralType("")
	c.zeroType = c.getNumberLiteralType(0)
	c.zeroBigIntType = c.getBigIntLiteralType(jsnum.PseudoBigInt{})
	c.typeofType = c.getUnionType(core.Map(slices.Sorted(maps.Keys(typeofNEFacts)), c.getStringLiteralType))
	c.flowLoopCache = make(map[FlowLoopKey]*Type)
	c.flowNodeReachable = make(map[store.Flow]bool)
	c.flowNodePostSuper = make(map[store.Flow]bool)
	c.subtypeRelation = &Relation{}
	c.strictSubtypeRelation = &Relation{}
	c.assignableRelation = &Relation{}
	c.comparableRelation = &Relation{}
	c.identityRelation = &Relation{}
	c.enumRelation = make(map[EnumRelationKey]RelationComparisonResult)
	c.moduleImportAttributesTypes = make(map[*store.Symbol]*Type)
	c.getGlobalESSymbolType = c.getGlobalTypeResolver("Symbol", 0 /*arity*/, false /*reportErrors*/)
	c.getGlobalBigIntType = c.getGlobalTypeResolver("BigInt", 0 /*arity*/, false /*reportErrors*/)
	c.getGlobalImportMetaType = c.getGlobalTypeResolver("ImportMeta", 0 /*arity*/, true /*reportErrors*/)
	c.getGlobalImportAttributesType = c.getGlobalTypeResolver("ImportAttributes", 0 /*arity*/, false /*reportErrors*/)
	c.getGlobalImportAttributesTypeChecked = c.getGlobalTypeResolver("ImportAttributes", 0 /*arity*/, true /*reportErrors*/)
	c.getGlobalNonNullableTypeAliasOrNil = c.getGlobalTypeAliasResolver("NonNullable", 1 /*arity*/, false /*reportErrors*/)
	c.getGlobalExtractSymbol = c.getGlobalTypeAliasResolver("Extract", 2 /*arity*/, true /*reportErrors*/)
	c.getGlobalDisposableType = c.getGlobalTypeResolver("Disposable", 0 /*arity*/, true /*reportErrors*/)
	c.getGlobalAsyncDisposableType = c.getGlobalTypeResolver("AsyncDisposable", 0 /*arity*/, true /*reportErrors*/)
	c.getGlobalAwaitedSymbol = c.getGlobalTypeAliasResolver("Awaited", 1 /*arity*/, true /*reportErrors*/)
	c.getGlobalAwaitedSymbolOrNil = c.getGlobalTypeAliasResolver("Awaited", 1 /*arity*/, false /*reportErrors*/)
	c.getGlobalNaNSymbolOrNil = c.getGlobalValueSymbolResolver("NaN", false /*reportErrors*/)
	c.getGlobalRecordSymbol = c.getGlobalTypeAliasResolver("Record", 2 /*arity*/, true /*reportErrors*/)
	c.getGlobalTemplateStringsArrayType = c.getGlobalTypeResolver("TemplateStringsArray", 0 /*arity*/, true /*reportErrors*/)
	c.getGlobalESSymbolConstructorSymbolOrNil = c.getGlobalValueSymbolResolver("Symbol", false /*reportErrors*/)
	c.getGlobalESSymbolConstructorTypeSymbolOrNil = c.getGlobalTypeSymbolResolver("SymbolConstructor", false /*reportErrors*/)
	c.getGlobalImportCallOptionsType = c.getGlobalTypeResolver("ImportCallOptions", 0 /*arity*/, false /*reportErrors*/)
	c.getGlobalImportCallOptionsTypeChecked = c.getGlobalTypeResolver("ImportCallOptions", 0 /*arity*/, true /*reportErrors*/)
	c.getGlobalPromiseType = c.getGlobalTypeResolver("Promise", 1 /*arity*/, false /*reportErrors*/)
	c.getGlobalPromiseTypeChecked = c.getGlobalTypeResolver("Promise", 1 /*arity*/, true /*reportErrors*/)
	c.getGlobalPromiseLikeType = c.getGlobalTypeResolver("PromiseLike", 1 /*arity*/, true /*reportErrors*/)
	c.getGlobalPromiseConstructorSymbol = c.getGlobalValueSymbolResolver("Promise", true /*reportErrors*/)
	c.getGlobalPromiseConstructorSymbolOrNil = c.getGlobalValueSymbolResolver("Promise", false /*reportErrors*/)
	c.getGlobalOmitSymbol = c.getGlobalTypeAliasResolver("Omit", 2 /*arity*/, true /*reportErrors*/)
	c.getGlobalNoInferSymbolOrNil = c.getGlobalTypeAliasResolver("NoInfer", 1 /*arity*/, false /*reportErrors*/)
	c.getGlobalIteratorType = c.getGlobalTypeResolver("Iterator", 3 /*arity*/, false /*reportErrors*/)
	c.getGlobalIterableType = c.getGlobalTypeResolver("Iterable", 3 /*arity*/, false /*reportErrors*/)
	c.getGlobalIterableTypeChecked = c.getGlobalTypeResolver("Iterable", 3 /*arity*/, true /*reportErrors*/)
	c.getGlobalIterableIteratorType = c.getGlobalTypeResolver("IterableIterator", 3 /*arity*/, false /*reportErrors*/)
	c.getGlobalIterableIteratorTypeChecked = c.getGlobalTypeResolver("IterableIterator", 3 /*arity*/, true /*reportErrors*/)
	c.getGlobalIteratorObjectType = c.getGlobalTypeResolver("IteratorObject", 3 /*arity*/, false /*reportErrors*/)
	c.getGlobalGeneratorType = c.getGlobalTypeResolver("Generator", 3 /*arity*/, false /*reportErrors*/)
	c.getGlobalAsyncIteratorType = c.getGlobalTypeResolver("AsyncIterator", 3 /*arity*/, false /*reportErrors*/)
	c.getGlobalAsyncIterableType = c.getGlobalTypeResolver("AsyncIterable", 3 /*arity*/, false /*reportErrors*/)
	c.getGlobalAsyncIterableTypeChecked = c.getGlobalTypeResolver("AsyncIterable", 3 /*arity*/, true /*reportErrors*/)
	c.getGlobalAsyncIterableIteratorType = c.getGlobalTypeResolver("AsyncIterableIterator", 3 /*arity*/, false /*reportErrors*/)
	c.getGlobalAsyncIterableIteratorTypeChecked = c.getGlobalTypeResolver("AsyncIterableIterator", 3 /*arity*/, true /*reportErrors*/)
	c.getGlobalAsyncIteratorObjectType = c.getGlobalTypeResolver("AsyncIteratorObject", 3 /*arity*/, false /*reportErrors*/)
	c.getGlobalAsyncGeneratorType = c.getGlobalTypeResolver("AsyncGenerator", 3 /*arity*/, false /*reportErrors*/)
	c.getGlobalIteratorYieldResultType = c.getGlobalTypeResolver("IteratorYieldResult", 1 /*arity*/, false /*reportErrors*/)
	c.getGlobalIteratorReturnResultType = c.getGlobalTypeResolver("IteratorReturnResult", 1 /*arity*/, false /*reportErrors*/)
	c.getGlobalTypedPropertyDescriptorType = c.getGlobalTypeResolver("TypedPropertyDescriptor", 1 /*arity*/, true /*reportErrors*/)
	c.getGlobalClassDecoratorContextType = c.getGlobalTypeResolver("ClassDecoratorContext", 1 /*arity*/, true /*reportErrors*/)
	c.getGlobalClassMethodDecoratorContextType = c.getGlobalTypeResolver("ClassMethodDecoratorContext", 2 /*arity*/, true /*reportErrors*/)
	c.getGlobalClassGetterDecoratorContextType = c.getGlobalTypeResolver("ClassGetterDecoratorContext", 2 /*arity*/, true /*reportErrors*/)
	c.getGlobalClassSetterDecoratorContextType = c.getGlobalTypeResolver("ClassSetterDecoratorContext", 2 /*arity*/, true /*reportErrors*/)
	c.getGlobalClassAccessorDecoratorContextType = c.getGlobalTypeResolver("ClassAccessorDecoratorContext", 2 /*arity*/, true /*reportErrors*/)
	c.getGlobalClassAccessorDecoratorTargetType = c.getGlobalTypeResolver("ClassAccessorDecoratorTarget", 2 /*arity*/, true /*reportErrors*/)
	c.getGlobalClassAccessorDecoratorResultType = c.getGlobalTypeResolver("ClassAccessorDecoratorResult", 2 /*arity*/, true /*reportErrors*/)
	c.getGlobalClassFieldDecoratorContextType = c.getGlobalTypeResolver("ClassFieldDecoratorContext", 2 /*arity*/, true /*reportErrors*/)
	c.initializeClosures()
	c.initializeIterationResolvers()
	c.initializeChecker()
	return c, &c.mu
}
