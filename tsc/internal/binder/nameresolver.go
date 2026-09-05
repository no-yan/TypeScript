package binder

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
)

type NameResolver struct {
	CompilerOptions                  *core.CompilerOptions
	GetSymbolOfDeclaration           func(node ast.Handle) *ast.Symbol
	Error                            func(location ast.Handle, message *diagnostics.Message, args ...any) *ast.Diagnostic
	Globals                          ast.SymbolTable
	ArgumentsSymbol                  *ast.Symbol
	RequireSymbol                    *ast.Symbol
	Lookup                           func(symbols ast.SymbolTable, name string, meaning ast.SymbolFlags) *ast.Symbol
	SymbolReferenced                 func(symbol *ast.Symbol, meaning ast.SymbolFlags)
	SetRequiresScopeChangeCache      func(node ast.Handle, value core.Tristate)
	GetRequiresScopeChangeCache      func(node ast.Handle) core.Tristate
	OnPropertyWithInvalidInitializer func(location ast.Handle, name string, declaration ast.Handle, result *ast.Symbol) bool
	OnFailedToResolveSymbol          func(location ast.Handle, name string, meaning ast.SymbolFlags, nameNotFoundMessage *diagnostics.Message)
	OnSuccessfullyResolvedSymbol     func(location ast.Handle, result *ast.Symbol, meaning ast.SymbolFlags, lastLocation ast.Handle, associatedDeclarationForContainingInitializerOrBindingName ast.Handle, withinDeferredContext bool)
}

func (r *NameResolver) Resolve(location ast.Handle, name string, meaning ast.SymbolFlags, nameNotFoundMessage *diagnostics.Message, isUse bool, excludeGlobals bool) *ast.Symbol {
	var result *ast.Symbol
	var lastLocation ast.Handle
	var lastSelfReferenceLocation ast.Handle
	var propertyWithInvalidInitializer ast.Handle
	var associatedDeclarationForContainingInitializerOrBindingName ast.Handle
	var withinDeferredContext bool
	var grandparent ast.Handle
	originalLocation := location
	nameIsConst := name == "const"
loop:
	for !location.IsNil() {
		if nameIsConst && ast.IsConstAssertion(location) {
			return nil
		}
		if ast.IsModuleOrEnumDeclaration(location) && !lastLocation.IsNil() && location.Name() == lastLocation {
			lastLocation = location
			location = location.Parent()
		}
		locals := location.Locals()
		if locals != nil && !ast.IsGlobalSourceFile(location) {
			result = r.lookup(locals, name, meaning)
			if result != nil {
				useResult := true
				if ast.IsFunctionLike(location) && !lastLocation.IsNil() && lastLocation != location.Body() {
					if meaning&result.Flags&ast.SymbolFlagsType != 0 && lastLocation.Kind != ast.KindJSDoc {
						useResult = result.Flags&ast.SymbolFlagsTypeParameter != 0 && (lastLocation.Flags()&ast.NodeFlagsSynthesized != 0 || lastLocation == location.Type() || lastLocation.Kind == ast.KindParameter || lastLocation.Kind == ast.KindJSDocParameterTag || lastLocation.Kind == ast.KindJSDocReturnTag || lastLocation.Kind == ast.KindTypeParameter)
					}
					if meaning&result.Flags&ast.SymbolFlagsVariable != 0 {
						if r.useOuterVariableScopeInParameter(result, location, lastLocation) {
							useResult = false
						} else if result.Flags&ast.SymbolFlagsFunctionScopedVariable != 0 {
							useResult = lastLocation.Kind == ast.KindParameter || lastLocation.Flags()&ast.NodeFlagsSynthesized != 0 || lastLocation == location.Type() && !ast.FindAncestor(ast.NodeOf(result.ValueDeclaration), ast.IsParameterDeclaration).IsNil()
						}
					}
				} else if location.Kind == ast.KindConditionalType {
					useResult = lastLocation == location.ConditionalTypeNodeTrueType()
				}
				if useResult {
					break loop
				}
				result = nil
			}
		}
		withinDeferredContext = withinDeferredContext || getIsDeferredContext(location, lastLocation)
		switch location.Kind {
		case ast.KindSourceFile:
			if !ast.IsExternalOrCommonJSModule(ast.GetSourceFileOfNode(location)) {
				break
			}
			fallthrough
		case ast.KindModuleDeclaration:
			moduleSymbol := r.getSymbolOfDeclaration(location)
			if moduleSymbol == nil {
				break
			}
			moduleExports := moduleSymbol.Exports
			if ast.IsSourceFile(location) || (ast.IsModuleDeclaration(location) && location.Flags()&ast.NodeFlagsAmbient != 0 && !ast.IsGlobalScopeAugmentation(location)) {
				result = moduleExports[ast.InternalSymbolNameDefault]
				if result != nil {
					localSymbol := GetLocalSymbolForExportDefault(result)
					if localSymbol != nil && result.Flags&meaning != 0 && localSymbol.Name == name {
						break loop
					}
					result = nil
				}
				moduleExport := moduleExports[name]
				if moduleExport != nil && moduleExport.Flags == ast.SymbolFlagsAlias && (!ast.GetDeclarationOfKind(moduleExport, ast.KindExportSpecifier).IsNil() || !ast.GetDeclarationOfKind(moduleExport, ast.KindNamespaceExport).IsNil()) {
					break
				}
			}
			if name != ast.InternalSymbolNameDefault {
				if result = r.lookup(moduleExports, name, meaning&ast.SymbolFlagsModuleMember); result != nil {
					if ast.IsSourceFile(location) && !ast.GetSourceFileOfNode(location).CommonJSModuleIndicator.IsNil() && result.Flags&ast.SymbolFlagsType == 0 {
						result = nil
					} else {
						break loop
					}
				}
			}
		case ast.KindEnumDeclaration:
			enumSymbol := r.getSymbolOfDeclaration(location)
			if enumSymbol == nil {
				break
			}
			result = r.lookup(enumSymbol.Exports, name, meaning&ast.SymbolFlagsEnumMember)
			if result != nil {
				if nameNotFoundMessage != nil && r.CompilerOptions.GetIsolatedModules() && location.Flags()&ast.NodeFlagsAmbient == 0 && ast.GetSourceFileOfNode(location) != ast.GetSourceFileOfNode(ast.NodeOf(result.ValueDeclaration)) {
					isolatedModulesLikeFlagName := core.IfElse(r.CompilerOptions.VerbatimModuleSyntax == core.TSTrue, "verbatimModuleSyntax", "isolatedModules")
					r.error(originalLocation, diagnostics.Cannot_access_0_from_another_file_without_qualification_when_1_is_enabled_Use_2_instead, name, isolatedModulesLikeFlagName, enumSymbol.Name+"."+name)
				}
				break loop
			}
		case ast.KindPropertyDeclaration:
			if !ast.IsStatic(location) {
				ctor := ast.FindConstructorDeclaration(location.Parent())
				if !ctor.IsNil() && ctor.Locals() != nil {
					if r.lookup(ctor.Locals(), name, meaning&ast.SymbolFlagsValue) != nil {
						propertyWithInvalidInitializer = location
					}
				}
			}
		case ast.KindClassDeclaration, ast.KindClassExpression, ast.KindInterfaceDeclaration:
			result = r.lookup(r.getSymbolOfDeclaration(location).Members, name, meaning&ast.SymbolFlagsType)
			if result != nil {
				if !isTypeParameterSymbolDeclaredInContainer(result, location) {
					result = nil
					break
				}
				if !lastLocation.IsNil() && ast.IsStatic(lastLocation) {
					if nameNotFoundMessage != nil {
						r.error(originalLocation, diagnostics.Static_members_cannot_reference_class_type_parameters)
					}
					return nil
				}
				break loop
			}
			if ast.IsClassExpression(location) && meaning&ast.SymbolFlagsClass != 0 {
				className := location.Name()
				if !className.IsNil() && name == className.Text() {
					result = location.Symbol()
					break loop
				}
			}
		case ast.KindExpressionWithTypeArguments:
			if lastLocation == location.Expression() && ast.IsHeritageClause(location.Parent()) && location.Parent().HeritageClauseToken() == ast.KindExtendsKeyword {
				container := location.Parent().Parent()
				if ast.IsClassLike(container) {
					result = r.lookup(r.getSymbolOfDeclaration(container).Members, name, meaning&ast.SymbolFlagsType)
					if result != nil {
						if nameNotFoundMessage != nil {
							r.error(originalLocation, diagnostics.Base_class_expressions_cannot_reference_class_type_parameters)
						}
						return nil
					}
				}
			}
		case ast.KindComputedPropertyName:
			grandparent = location.Parent().Parent()
			if ast.IsClassLike(grandparent) || ast.IsInterfaceDeclaration(grandparent) {
				result = r.lookup(r.getSymbolOfDeclaration(grandparent).Members, name, meaning&ast.SymbolFlagsType)
				if result != nil {
					if nameNotFoundMessage != nil {
						r.error(originalLocation, diagnostics.A_computed_property_name_cannot_reference_a_type_parameter_from_its_containing_type)
					}
					return nil
				}
			}
		case ast.KindMethodDeclaration, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindFunctionDeclaration:
			if meaning&ast.SymbolFlagsVariable != 0 && name == "arguments" {
				result = r.argumentsSymbol()
				break loop
			}
		case ast.KindFunctionExpression:
			if meaning&ast.SymbolFlagsVariable != 0 && name == "arguments" {
				result = r.argumentsSymbol()
				break loop
			}
			if meaning&ast.SymbolFlagsFunction != 0 {
				functionName := location.FunctionExpressionName()
				if !functionName.IsNil() && name == functionName.Text() {
					result = location.Symbol()
					break loop
				}
			}
		case ast.KindDecorator:
			if !location.Parent().IsNil() && location.Parent().Kind == ast.KindParameter {
				location = location.Parent()
			}
			if !location.Parent().IsNil() && (ast.IsClassElement(location.Parent()) || location.Parent().Kind == ast.KindClassDeclaration) {
				location = location.Parent()
			}
		case ast.KindParameter:
			parameterDeclaration := location
			if !lastLocation.IsNil() && (lastLocation == parameterDeclaration.Initializer() || lastLocation == parameterDeclaration.Name() && ast.IsBindingPattern(lastLocation)) {
				if associatedDeclarationForContainingInitializerOrBindingName.IsNil() {
					associatedDeclarationForContainingInitializerOrBindingName = location
				}
			}
		case ast.KindBindingElement:
			bindingElement := location
			if !lastLocation.IsNil() && (lastLocation == bindingElement.Initializer() || lastLocation == bindingElement.Name() && ast.IsBindingPattern(lastLocation)) {
				if ast.IsPartOfParameterDeclaration(location) && associatedDeclarationForContainingInitializerOrBindingName.IsNil() {
					associatedDeclarationForContainingInitializerOrBindingName = location
				}
			}
		case ast.KindInferType:
			if meaning&ast.SymbolFlagsTypeParameter != 0 {
				parameterName := location.InferTypeNodeTypeParameter().TypeParameterDeclarationName()
				if !parameterName.IsNil() && name == parameterName.Text() {
					result = location.InferTypeNodeTypeParameter().Symbol()
					break loop
				}
			}
		case ast.KindExportSpecifier:
			exportSpecifier := location
			if !lastLocation.IsNil() && lastLocation == exportSpecifier.PropertyName() && !location.Parent().Parent().ModuleSpecifier().IsNil() {
				location = location.Parent().Parent().Parent()
			}
		}
		if isSelfReferenceLocation(location, lastLocation) {
			lastSelfReferenceLocation = location
		}
		lastLocation = location
		location = location.Parent()
	}
	if isUse && result != nil && (lastSelfReferenceLocation.IsNil() || result != lastSelfReferenceLocation.Symbol()) {
		if r.SymbolReferenced != nil {
			r.SymbolReferenced(result, meaning)
		}
	}
	if result == nil && !excludeGlobals {
		result = r.lookup(r.Globals, name, meaning|ast.SymbolFlagsGlobalLookup)
	}
	if result == nil {
		if !originalLocation.IsNil() && ast.IsInJSFile(originalLocation) && !originalLocation.Parent().IsNil() {
			if ast.IsRequireCall(originalLocation.Parent(), false) {
				return r.RequireSymbol
			}
		}
	}
	if nameNotFoundMessage != nil {
		if !propertyWithInvalidInitializer.IsNil() && r.OnPropertyWithInvalidInitializer != nil && r.OnPropertyWithInvalidInitializer(originalLocation, name, propertyWithInvalidInitializer, result) {
			return nil
		}
		if result == nil {
			if r.OnFailedToResolveSymbol != nil {
				r.OnFailedToResolveSymbol(originalLocation, name, meaning, nameNotFoundMessage)
			}
		} else {
			if r.OnSuccessfullyResolvedSymbol != nil {
				r.OnSuccessfullyResolvedSymbol(originalLocation, result, meaning, lastLocation, associatedDeclarationForContainingInitializerOrBindingName, withinDeferredContext)
			}
		}
	}
	return result
}
func (r *NameResolver) useOuterVariableScopeInParameter(result *ast.Symbol, location ast.Handle, lastLocation ast.Handle) bool {
	if ast.IsParameterDeclaration(lastLocation) {
		body := location.Body()
		if !body.IsNil() {
			declaration := ast.NodeOf(result.ValueDeclaration)
			if !declaration.IsNil() && declaration.Pos() >= body.Pos() && declaration.End() <= body.End() {
				functionLocation := location
				declarationRequiresScopeChange := core.TSUnknown
				if r.GetRequiresScopeChangeCache != nil {
					declarationRequiresScopeChange = r.GetRequiresScopeChangeCache(functionLocation)
				}
				if declarationRequiresScopeChange == core.TSUnknown {
					declarationRequiresScopeChange = core.IfElse(core.Some(functionLocation.Parameters(), r.requiresScopeChange), core.TSTrue, core.TSFalse)
					if r.SetRequiresScopeChangeCache != nil {
						r.SetRequiresScopeChangeCache(functionLocation, declarationRequiresScopeChange)
					}
				}
				return declarationRequiresScopeChange != core.TSTrue
			}
		}
	}
	return false
}
func (r *NameResolver) requiresScopeChange(node ast.Handle) bool {
	d := node
	return r.requiresScopeChangeWorker(d.Name()) || !d.Initializer().IsNil() && r.requiresScopeChangeWorker(d.Initializer())
}
func (r *NameResolver) requiresScopeChangeWorker(node ast.Handle) bool {
	switch node.Kind {
	case ast.KindArrowFunction, ast.KindFunctionExpression, ast.KindFunctionDeclaration, ast.KindConstructor:
		return false
	case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindPropertyAssignment:
		return r.requiresScopeChangeWorker(node.Name())
	case ast.KindPropertyDeclaration:
		if ast.HasStaticModifier(node) {
			return !r.CompilerOptions.GetEmitStandardClassFields()
		}
		return r.requiresScopeChangeWorker(node.PropertyDeclarationName())
	default:
		if ast.IsNullishCoalesce(node) || ast.IsOptionalChain(node) {
			return r.CompilerOptions.GetEmitScriptTarget() < core.ScriptTargetES2020
		}
		if ast.IsBindingElement(node) && !node.BindingElementDotDotDotToken().IsNil() && ast.IsObjectBindingPattern(node.Parent()) {
			return r.CompilerOptions.GetEmitScriptTarget() < core.ScriptTargetES2017
		}
		if ast.IsTypeNode(node) {
			return false
		}
		return node.ForEachChild(r.requiresScopeChangeWorker)
	}
}
func (r *NameResolver) error(location ast.Handle, message *diagnostics.Message, args ...any) {
	if r.Error != nil {
		r.Error(location, message, args...)
	}
}
func (r *NameResolver) getSymbolOfDeclaration(node ast.Handle) *ast.Symbol {
	if r.GetSymbolOfDeclaration != nil {
		return r.GetSymbolOfDeclaration(node)
	}
	return node.Symbol()
}
func (r *NameResolver) lookup(symbols ast.SymbolTable, name string, meaning ast.SymbolFlags) *ast.Symbol {
	if r.Lookup != nil {
		return r.Lookup(symbols, name, meaning)
	}
	if meaning != 0 {
		symbol := symbols[name]
		if symbol != nil {
			if symbol.Flags&meaning != 0 {
				return symbol
			}
		}
	}
	return nil
}
func (r *NameResolver) argumentsSymbol() *ast.Symbol {
	if r.ArgumentsSymbol == nil {
		r.ArgumentsSymbol = &ast.Symbol{Name: "arguments", Flags: ast.SymbolFlagsProperty | ast.SymbolFlagsTransient}
	}
	return r.ArgumentsSymbol
}
func GetLocalSymbolForExportDefault(symbol *ast.Symbol) *ast.Symbol {
	if !isExportDefaultSymbol(symbol) || len(symbol.Declarations) == 0 {
		return nil
	}
	for _, declRef := range symbol.Declarations {
		decl := ast.NodeOf(declRef)
		if decl.IsNil() {
			continue
		}
		localSymbol := decl.LocalSymbol()
		if localSymbol != nil {
			return localSymbol
		}
	}
	return nil
}
func isExportDefaultSymbol(symbol *ast.Symbol) bool {
	return symbol != nil && len(symbol.Declarations) > 0 && ast.HasSyntacticModifier(ast.NodeOf(symbol.Declarations[0]), ast.ModifierFlagsDefault)
}
func getIsDeferredContext(location ast.Handle, lastLocation ast.Handle) bool {
	if location.Kind != ast.KindArrowFunction && location.Kind != ast.KindFunctionExpression {
		return ast.IsTypeQueryNode(location) || (ast.IsFunctionLikeDeclaration(location) || location.Kind == ast.KindPropertyDeclaration && !ast.IsStatic(location)) && (lastLocation.IsNil() || lastLocation != location.Name())
	}
	if !lastLocation.IsNil() && lastLocation == location.Name() {
		return false
	}
	if !location.AsteriskToken().IsNil() || ast.HasSyntacticModifier(location, ast.ModifierFlagsAsync) {
		return true
	}
	return ast.GetImmediatelyInvokedFunctionExpression(location).IsNil()
}
func isTypeParameterSymbolDeclaredInContainer(symbol *ast.Symbol, container ast.Handle) bool {
	for _, declRef := range symbol.Declarations {
		decl := ast.NodeOf(declRef)
		if !decl.IsNil() && decl.Kind == ast.KindTypeParameter {
			parent := decl.Parent()
			if parent == container {
				return true
			}
		}
	}
	return false
}
func isSelfReferenceLocation(node ast.Handle, lastLocation ast.Handle) bool {
	switch node.Kind {
	case ast.KindParameter:
		return !lastLocation.IsNil() && lastLocation == node.Name()
	case ast.KindFunctionDeclaration, ast.KindClassDeclaration, ast.KindInterfaceDeclaration, ast.KindEnumDeclaration, ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration, ast.KindModuleDeclaration:
		return true
	}
	return false
}
