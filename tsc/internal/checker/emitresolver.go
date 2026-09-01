package checker

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/evaluator"
	"github.com/microsoft/TypeScript/tsc/internal/jsnum"
	"github.com/microsoft/TypeScript/tsc/internal/nodebuilder"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"maps"
	"slices"
	"sync"
)

var _ printer.EmitResolver = (*EmitResolver)(nil)

type JSXLinks struct {
	importRef ast.Handle
}
type DeclarationLinks struct {
	isVisible core.Tristate
}
type DeclarationFileLinks struct {
	aliasesMarked bool
}
type EmitResolver struct {
	checker                 *Checker
	checkerMu               *sync.Mutex
	isValueAliasDeclaration func(node ast.Handle) bool
	aliasMarkingVisitor     func(node ast.Handle) bool
	referenceResolver       binder.ReferenceResolver
	jsxLinks                core.LinkStore[ast.Handle, JSXLinks]
	declarationLinks        core.LinkStore[ast.Handle, DeclarationLinks]
	declarationFileLinks    core.LinkStore[ast.Handle, DeclarationFileLinks]
}

func newEmitResolver(checker *Checker) *EmitResolver {
	e := &EmitResolver{checker: checker}
	e.isValueAliasDeclaration = e.isValueAliasDeclarationWorker
	e.aliasMarkingVisitor = e.aliasMarkingVisitorWorker
	e.checkerMu = &checker.mu
	return e
}
func (r *EmitResolver) GetJsxFactoryEntity(location ast.Handle) ast.Handle {
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.checker.getJsxFactoryEntity(location)
}
func (r *EmitResolver) GetJsxFragmentFactoryEntity(location ast.Handle) ast.Handle {
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.checker.getJsxFragmentFactoryEntity(location)
}
func (r *EmitResolver) IsOptionalParameter(node ast.Handle) bool {
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.isOptionalParameter(node)
}
func (r *EmitResolver) IsLateBound(node ast.Handle) bool {
	if node.IsNil() {
		return false
	}
	if !ast.IsParseTreeNode(node) {
		return false
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	symbol := r.checker.getSymbolOfDeclaration(node)
	if symbol == nil {
		return false
	}
	return symbol.CheckFlags&ast.CheckFlagsLate != 0
}
func (r *EmitResolver) GetEnumMemberValue(node ast.Handle) evaluator.Result {
	if !ast.IsParseTreeNode(node) {
		return evaluator.NewResult(nil, false, false, false)
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	r.checker.computeEnumMemberValues(node.Parent())
	if !r.checker.enumMemberLinks.Has(node) {
		return evaluator.NewResult(nil, false, false, false)
	}
	return r.checker.enumMemberLinks.Get(node).value
}
func (r *EmitResolver) IsDeclarationVisible(node ast.Handle) bool {
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.isDeclarationVisible(node)
}
func (r *EmitResolver) isDeclarationVisible(node ast.Handle) bool {
	if !ast.IsParseTreeNode(node) {
		return false
	}
	if node.IsNil() {
		return false
	}
	links := r.declarationLinks.Get(node)
	if links.isVisible == core.TSUnknown {
		if r.determineIfDeclarationIsVisible(node) {
			links.isVisible = core.TSTrue
		} else {
			links.isVisible = core.TSFalse
		}
	}
	return links.isVisible == core.TSTrue
}
func (r *EmitResolver) determineIfDeclarationIsVisible(node ast.Handle) bool {
	switch node.Kind() {
	case ast.KindJSDocCallbackTag, ast.KindJSDocTypedefTag:
		return !node.Parent().IsNil() && !node.Parent().Parent().IsNil() && !node.Parent().Parent().Parent().IsNil() && ast.IsSourceFile(node.Parent().Parent().Parent())
	case ast.KindBindingElement:
		return r.isDeclarationVisible(node.Parent().Parent())
	case ast.KindVariableDeclaration, ast.KindModuleDeclaration, ast.KindClassDeclaration, ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration, ast.KindFunctionDeclaration, ast.KindEnumDeclaration, ast.KindImportEqualsDeclaration:
		if ast.IsVariableDeclaration(node) {
			if ast.IsBindingPattern(node.Name()) && len(node.Name().Elements()) == 0 {
				return false
			}
		}
		if ast.IsExternalModuleAugmentation(node) || ast.IsImplicitlyExportedJSDocDeclaration(node) {
			return true
		}
		parent := ast.GetDeclarationContainer(node)
		if r.checker.getCombinedModifierFlagsCached(node)&ast.ModifierFlagsExport == 0 && !(node.Kind() != ast.KindImportEqualsDeclaration && parent.Kind() != ast.KindSourceFile && parent.Flags()&ast.NodeFlagsAmbient != 0) {
			return ast.IsGlobalSourceFile(parent)
		}
		return r.isDeclarationVisible(parent)
	case ast.KindPropertyDeclaration, ast.KindPropertySignature, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindMethodDeclaration, ast.KindMethodSignature:
		if r.checker.GetEffectiveDeclarationFlags(node, ast.ModifierFlagsPrivate|ast.ModifierFlagsProtected) != 0 {
			return false
		}
		return r.isDeclarationVisible(node.Parent())
	case ast.KindConstructor, ast.KindConstructSignature, ast.KindCallSignature, ast.KindIndexSignature, ast.KindParameter, ast.KindModuleBlock, ast.KindFunctionType, ast.KindConstructorType, ast.KindTypeLiteral, ast.KindTypeReference, ast.KindArrayType, ast.KindTupleType, ast.KindUnionType, ast.KindIntersectionType, ast.KindParenthesizedType, ast.KindNamedTupleMember:
		return r.isDeclarationVisible(node.Parent())
	case ast.KindImportClause, ast.KindNamespaceImport, ast.KindImportSpecifier:
		return false
	case ast.KindTypeParameter:
		return true
	case ast.KindSourceFile, ast.KindNamespaceExportDeclaration:
		return true
	case ast.KindExportAssignment:
		return false
	case ast.KindExportSpecifier:
		exportDecl := node.Parent().Parent()
		if ast.IsExportDeclaration(exportDecl) && exportDecl.ExportDeclarationModuleSpecifier().IsNil() {
			return r.isDeclarationVisible(exportDecl.Parent())
		}
		return false
	default:
		return false
	}
}
func (r *EmitResolver) PrecalculateDeclarationEmitVisibility(file *ast.SourceFile) {
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	if r.declarationFileLinks.Get(file.ParseRoot()).aliasesMarked {
		return
	}
	r.declarationFileLinks.Get(file.ParseRoot()).aliasesMarked = true
	file.ParseRoot().ForEachChild(r.aliasMarkingVisitor)
}
func isCommonJSModuleExports(node ast.Handle) bool {
	if ast.IsBinaryExpression(node) && ast.IsExpressionStatement(node.Parent()) && ast.IsSourceFile(node.Parent().Parent()) && ast.GetSourceFileOfNode(node.Parent().Parent()) != nil && !ast.GetSourceFileOfNode(node.Parent().Parent()).CommonJSModuleIndicator.IsNil() {
		switch ast.GetAssignmentDeclarationKind(node) {
		case ast.JSDeclarationKindModuleExports, ast.JSDeclarationKindExportsProperty:
			return true
		}
	}
	return false
}
func (r *EmitResolver) aliasMarkingVisitorWorker(node ast.Handle) bool {
	switch node.Kind() {
	case ast.KindBinaryExpression:
		if isCommonJSModuleExports(node) && ast.IsIdentifier(node.BinaryExpressionRight()) {
			r.markLinkedAliases(node.BinaryExpressionRight())
		}
	case ast.KindExportAssignment:
		if node.Expression().Kind() == ast.KindIdentifier {
			r.markLinkedAliases(node.Expression())
		}
	case ast.KindExportSpecifier:
		r.markLinkedAliases(node.PropertyNameOrName())
	}
	return node.ForEachChild(r.aliasMarkingVisitor)
}

func (r *EmitResolver) markLinkedAliases(node ast.Handle) {
	var exportSymbol *ast.Symbol
	if node.Kind() != ast.KindStringLiteral && !node.Parent().IsNil() && (ast.IsExportAssignment(node.Parent()) || isCommonJSModuleExports(node.Parent())) {
		exportSymbol = r.checker.resolveName(node, node.Text(), ast.SymbolFlagsValue|ast.SymbolFlagsType|ast.SymbolFlagsNamespace|ast.SymbolFlagsAlias, nil, false, false)
	} else if node.Parent().Kind() == ast.KindExportSpecifier {
		exportSymbol = r.checker.getTargetOfExportSpecifier(node.Parent(), ast.SymbolFlagsValue|ast.SymbolFlagsType|ast.SymbolFlagsNamespace|ast.SymbolFlagsAlias, false)
	}
	visited := make(map[ast.SymbolId]struct{}, 2)
	for exportSymbol != nil {
		_, seen := visited[ast.GetSymbolId(exportSymbol)]
		if seen {
			break
		}
		visited[ast.GetSymbolId(exportSymbol)] = struct{}{}
		var nextSymbol *ast.Symbol
		for _, declaration := range ast.DeclarationNodes(exportSymbol) {
			r.declarationLinks.Get(declaration).isVisible = core.TSTrue
			if ast.IsInternalModuleImportEqualsDeclaration(declaration) {
				internalModuleReference := declaration.ImportEqualsDeclarationModuleReference()
				firstIdentifier := ast.GetFirstIdentifier(internalModuleReference)
				importSymbol := r.checker.resolveName(declaration, firstIdentifier.Text(), ast.SymbolFlagsValue|ast.SymbolFlagsType|ast.SymbolFlagsNamespace|ast.SymbolFlagsAlias, nil, false, false)
				nextSymbol = importSymbol
			}
		}
		exportSymbol = nextSymbol
	}
}
func getMeaningOfEntityNameReference(entityName ast.Handle) ast.SymbolFlags {
	if entityName.Parent().Kind() == ast.KindTypeQuery || entityName.Parent().Kind() == ast.KindExpressionWithTypeArguments && !ast.IsPartOfTypeNode(entityName.Parent()) || entityName.Parent().Kind() == ast.KindComputedPropertyName || entityName.Parent().Kind() == ast.KindTypePredicate && entityName.Parent().TypePredicateNodeParameterName() == entityName || entityName.Parent().Kind() == ast.KindBinaryExpression {
		return ast.SymbolFlagsValue | ast.SymbolFlagsExportValue
	}
	if entityName.Kind() == ast.KindQualifiedName || entityName.Kind() == ast.KindPropertyAccessExpression || entityName.Parent().Kind() == ast.KindImportEqualsDeclaration || (entityName.Parent().Kind() == ast.KindQualifiedName && entityName.Parent().QualifiedNameLeft() == entityName) || (entityName.Parent().Kind() == ast.KindPropertyAccessExpression && entityName.Parent().Expression() == entityName) || (entityName.Parent().Kind() == ast.KindElementAccessExpression && entityName.Parent().Expression() == entityName) {
		return ast.SymbolFlagsNamespace
	}
	return ast.SymbolFlagsType
}
func (r *EmitResolver) IsEntityNameVisible(entityName ast.Handle, enclosingDeclaration ast.Handle) printer.SymbolAccessibilityResult {
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.isEntityNameVisible(entityName, enclosingDeclaration, true)
}
func (r *EmitResolver) isEntityNameVisible(entityName ast.Handle, enclosingDeclaration ast.Handle, shouldComputeAliasToMakeVisible bool) printer.SymbolAccessibilityResult {
	if !ast.IsParseTreeNode(entityName) {
		return printer.SymbolAccessibilityResult{Accessibility: printer.SymbolAccessibilityNotAccessible}
	}
	meaning := getMeaningOfEntityNameReference(entityName)
	firstIdentifier := ast.GetFirstIdentifier(entityName)
	symbol := r.checker.resolveName(enclosingDeclaration, firstIdentifier.Text(), meaning, nil, false, false)
	if symbol != nil && symbol.Flags&ast.SymbolFlagsTypeParameter != 0 && meaning&ast.SymbolFlagsType != 0 {
		return printer.SymbolAccessibilityResult{Accessibility: printer.SymbolAccessibilityAccessible}
	}
	if symbol == nil && ast.IsThisIdentifier(firstIdentifier) {
		sym := r.checker.getSymbolOfDeclaration(r.checker.getThisContainer(firstIdentifier, false, false))
		if r.isSymbolAccessible(sym, enclosingDeclaration, meaning, false).Accessibility == printer.SymbolAccessibilityAccessible {
			return printer.SymbolAccessibilityResult{Accessibility: printer.SymbolAccessibilityAccessible}
		}
	}
	if symbol == nil {
		return printer.SymbolAccessibilityResult{Accessibility: printer.SymbolAccessibilityNotResolved, ErrorSymbolName: firstIdentifier.Text(), ErrorNode: firstIdentifier}
	}
	visible := r.hasVisibleDeclarations(symbol, shouldComputeAliasToMakeVisible)
	if visible != nil {
		return *visible
	}
	return printer.SymbolAccessibilityResult{Accessibility: printer.SymbolAccessibilityNotAccessible, ErrorSymbolName: firstIdentifier.Text(), ErrorNode: firstIdentifier}
}
func noopAddVisibleAlias(declaration ast.Handle, aliasingStatement ast.Handle) {
}
func (r *EmitResolver) hasVisibleDeclarations(symbol *ast.Symbol, shouldComputeAliasToMakeVisible bool) *printer.SymbolAccessibilityResult {
	var aliasesToMakeVisibleSet map[ast.NodeId]ast.Handle
	var addVisibleAlias func(declaration ast.Handle, aliasingStatement ast.Handle)
	if shouldComputeAliasToMakeVisible {
		addVisibleAlias = func(declaration ast.Handle, aliasingStatement ast.Handle) {
			r.declarationLinks.Get(declaration).isVisible = core.TSTrue
			if aliasesToMakeVisibleSet == nil {
				aliasesToMakeVisibleSet = make(map[ast.NodeId]ast.Handle)
			}
			aliasesToMakeVisibleSet[declaration.NodeId()] = aliasingStatement
		}
	} else {
		addVisibleAlias = noopAddVisibleAlias
	}
	for _, declaration := range ast.DeclarationNodes(symbol) {
		if ast.IsIdentifier(declaration) {
			continue
		}
		if !r.isDeclarationVisible(declaration) {
			anyImportSyntax := getAnyImportSyntax(declaration)
			if !anyImportSyntax.IsNil() && !ast.HasSyntacticModifier(anyImportSyntax, ast.ModifierFlagsExport) && r.isDeclarationVisible(anyImportSyntax.Parent()) {
				addVisibleAlias(declaration, anyImportSyntax)
				continue
			}
			if ast.IsVariableDeclaration(declaration) && ast.IsVariableStatement(declaration.Parent().Parent()) && !ast.HasSyntacticModifier(declaration.Parent().Parent(), ast.ModifierFlagsExport) && r.isDeclarationVisible(declaration.Parent().Parent().Parent()) {
				addVisibleAlias(declaration, declaration.Parent().Parent())
				continue
			}
			if ast.IsLateVisibilityPaintedStatement(declaration) && !ast.HasSyntacticModifier(declaration, ast.ModifierFlagsExport) && r.isDeclarationVisible(declaration.Parent()) {
				addVisibleAlias(declaration, declaration)
				continue
			}
			if ast.IsBindingElement(declaration) {
				if symbol.Flags&ast.SymbolFlagsAlias != 0 && ast.IsInJSFile(declaration) && !declaration.Parent().IsNil() && !declaration.Parent().Parent().IsNil() && ast.IsVariableDeclaration(declaration.Parent().Parent()) && !declaration.Parent().Parent().Parent().Parent().IsNil() && ast.IsVariableStatement(declaration.Parent().Parent().Parent().Parent()) && !ast.HasSyntacticModifier(declaration.Parent().Parent().Parent().Parent(), ast.ModifierFlagsExport) && !declaration.Parent().Parent().Parent().Parent().Parent().IsNil() && r.isDeclarationVisible(declaration.Parent().Parent().Parent().Parent().Parent()) {
					addVisibleAlias(declaration, declaration.Parent().Parent().Parent().Parent())
					continue
				}
				if symbol.Flags&ast.SymbolFlagsBlockScopedVariable != 0 {
					rootDeclaration := ast.WalkUpBindingElementsAndPatterns(declaration)
					if ast.IsParameterDeclaration(rootDeclaration) {
						return nil
					}
					variableStatement := rootDeclaration.Parent().Parent()
					if !ast.IsVariableStatement(variableStatement) {
						return nil
					}
					if ast.HasSyntacticModifier(variableStatement, ast.ModifierFlagsExport) {
						continue
					}
					if !r.isDeclarationVisible(variableStatement.Parent()) {
						return nil
					}
					addVisibleAlias(declaration, variableStatement)
					continue
				}
			}
			return nil
		}
	}
	return &printer.SymbolAccessibilityResult{Accessibility: printer.SymbolAccessibilityAccessible, AliasesToMakeVisible: slices.Collect(maps.Values(aliasesToMakeVisibleSet))}
}
func (r *EmitResolver) IsImplementationOfOverload(node ast.Handle) bool {
	if !ast.IsParseTreeNode(node) {
		return false
	}
	if ast.NodeIsPresent(node.Body()) {
		if ast.IsGetAccessorDeclaration(node) || ast.IsSetAccessorDeclaration(node) {
			return false
		}
		r.checkerMu.Lock()
		defer r.checkerMu.Unlock()
		symbol := r.checker.getSymbolOfDeclaration(node)
		signaturesOfSymbol := r.checker.getSignaturesOfSymbol(symbol)
		if len(signaturesOfSymbol) > 1 {
			return true
		}
		if len(signaturesOfSymbol) == 1 {
			signature := signaturesOfSymbol[0]
			if signature == r.checker.getSignatureOfFullSignatureType(node) {
				return false
			}
			declaration := signature.declaration
			if declaration != node && declaration.Flags()&ast.NodeFlagsJSDoc == 0 {
				return true
			}
		}
	}
	return false
}
func (r *EmitResolver) IsImportRequiredByAugmentation(decl ast.Handle) bool {
	if !ast.IsParseTreeNode(decl) {
		return false
	}
	file := ast.GetSourceFileOfNode(decl)
	if file == nil || file.Symbol == nil {
		return false
	}
	importTarget := r.GetExternalModuleFileFromDeclaration(decl)
	if importTarget == nil {
		return false
	}
	if importTarget == file {
		return false
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	exports := r.checker.getExportsOfModule(file.Symbol)
	for s := range maps.Values(exports) {
		merged := r.checker.getMergedSymbol(s)
		if merged != s {
			if len(merged.Declarations) > 0 {
				for _, d := range ast.DeclarationNodes(merged) {
					declFile := ast.GetSourceFileOfNode(d)
					if declFile == importTarget {
						return true
					}
				}
			}
		}
	}
	return false
}
func (r *EmitResolver) IsDefinitelyReferenceToGlobalSymbolObject(node ast.Handle) bool {
	if !ast.IsPropertyAccessExpression(node) || !ast.IsIdentifier(node.Name()) || !ast.IsPropertyAccessExpression(node.Expression()) && !ast.IsIdentifier(node.Expression()) {
		return false
	}
	if node.Expression().Kind() == ast.KindIdentifier {
		if node.Expression().Text() != "Symbol" {
			return false
		}
		r.checkerMu.Lock()
		defer r.checkerMu.Unlock()
		return r.checker.getResolvedSymbol(node.Expression()) == r.checker.getGlobalSymbol("Symbol", ast.SymbolFlagsValue|ast.SymbolFlagsExportValue, nil)
	}
	if node.Expression().Expression().Kind() != ast.KindIdentifier || node.Expression().Expression().Text() != "globalThis" || node.Expression().Name().Text() != "Symbol" {
		return false
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.checker.getResolvedSymbol(node.Expression().Expression()) == r.checker.globalThisSymbol
}
func (r *EmitResolver) RequiresAddingImplicitUndefined(declaration ast.Handle, symbol *ast.Symbol, enclosingDeclaration ast.Handle) bool {
	if !ast.IsParseTreeNode(declaration) {
		return false
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.requiresAddingImplicitUndefined(declaration, symbol, enclosingDeclaration)
}
func (r *EmitResolver) RequiresAddingImplicitUndefinedUnsafe(declaration ast.Handle, symbol *ast.Symbol, enclosingDeclaration ast.Handle) bool {
	if !ast.IsParseTreeNode(declaration) {
		return false
	}
	return r.requiresAddingImplicitUndefined(declaration, symbol, enclosingDeclaration)
}
func (r *EmitResolver) requiresAddingImplicitUndefined(declaration ast.Handle, symbol *ast.Symbol, enclosingDeclaration ast.Handle) bool {
	if !ast.IsParseTreeNode(declaration) {
		return false
	}
	switch declaration.Kind() {
	case ast.KindPropertyDeclaration, ast.KindPropertySignature, ast.KindJSDocPropertyTag:
		if symbol == nil {
			symbol = r.checker.getSymbolOfDeclaration(declaration)
		}
		t := r.checker.getTypeOfSymbol(symbol)
		r.checker.mappedSymbolLinks.Has(symbol)
		return (symbol.Flags&ast.SymbolFlagsProperty != 0) && (symbol.Flags&ast.SymbolFlagsOptional != 0) && isOptionalDeclaration(declaration) && r.checker.ReverseMappedSymbolLinks.Has(symbol) && r.checker.ReverseMappedSymbolLinks.Get(symbol).mappedType != nil && containsNonMissingUndefinedType(r.checker, t)
	case ast.KindParameter, ast.KindJSDocParameterTag:
		return r.requiresAddingImplicitUndefinedWorker(declaration, enclosingDeclaration)
	default:
		panic("Node cannot possibly require adding undefined")
	}
}
func (r *EmitResolver) requiresAddingImplicitUndefinedWorker(parameter ast.Handle, enclosingDeclaration ast.Handle) bool {
	return (r.isRequiredInitializedParameter(parameter, enclosingDeclaration) || r.isOptionalUninitializedParameterProperty(parameter)) && !r.declaredParameterTypeContainsUndefined(parameter)
}
func (r *EmitResolver) declaredParameterTypeContainsUndefined(parameter ast.Handle) bool {
	typeNode := parameter.Type()
	if typeNode.IsNil() {
		return false
	}
	t := r.checker.getTypeFromTypeNode(typeNode)
	return r.checker.isErrorType(t) || r.checker.containsUndefinedType(t)
}
func (r *EmitResolver) isOptionalUninitializedParameterProperty(parameter ast.Handle) bool {
	return r.checker.strictNullChecks && r.isOptionalParameter(parameter) && (parameter.Initializer().IsNil()) && ast.HasSyntacticModifier(parameter, ast.ModifierFlagsParameterPropertyModifier)
}
func (r *EmitResolver) isRequiredInitializedParameter(parameter ast.Handle, enclosingDeclaration ast.Handle) bool {
	if !r.checker.strictNullChecks || r.isOptionalParameter(parameter) || parameter.Initializer().IsNil() {
		return false
	}
	if ast.HasSyntacticModifier(parameter, ast.ModifierFlagsParameterPropertyModifier) {
		return !enclosingDeclaration.IsNil() && ast.IsFunctionLikeDeclaration(enclosingDeclaration)
	}
	return true
}
func (r *EmitResolver) isOptionalParameter(node ast.Handle) bool {
	return r.checker.isOptionalParameter(node)
}
func (r *EmitResolver) IsLiteralConstDeclaration(node ast.Handle) bool {
	if !ast.IsParseTreeNode(node) {
		return false
	}
	if isDeclarationReadonly(node) || ast.IsVariableDeclaration(node) && ast.IsVarConst(node) {
		r.checkerMu.Lock()
		defer r.checkerMu.Unlock()
		s := r.checker.getSymbolOfDeclaration(node)
		if s == nil {
			return false
		}
		return isFreshLiteralType(r.checker.getTypeOfSymbol(s))
	}
	return false
}
func (r *EmitResolver) IsExpandoFunctionDeclarationUnsafe(node ast.Handle) bool {
	if !ast.IsParseTreeNode(node) {
		return false
	}
	props := r.GetPropertiesOfContainerFunction(node)
	for _, p := range props {
		if ast.IsExpandoPropertyDeclaration(ast.NodeOf(p.ValueDeclaration)) {
			return true
		}
	}
	return false
}
func (r *EmitResolver) IsExpandoFunctionDeclaration(node ast.Handle) bool {
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.IsExpandoFunctionDeclarationUnsafe(node)
}
func (r *EmitResolver) isSymbolAccessible(symbol *ast.Symbol, enclosingDeclaration ast.Handle, meaning ast.SymbolFlags, shouldComputeAliasToMarkVisible bool) printer.SymbolAccessibilityResult {
	return r.checker.IsSymbolAccessible(symbol, enclosingDeclaration, meaning, shouldComputeAliasToMarkVisible)
}
func (r *EmitResolver) IsSymbolAccessible(symbol *ast.Symbol, enclosingDeclaration ast.Handle, meaning ast.SymbolFlags, shouldComputeAliasToMarkVisible bool) printer.SymbolAccessibilityResult {
	return r.isSymbolAccessible(symbol, enclosingDeclaration, meaning, shouldComputeAliasToMarkVisible)
}
func isConstEnumOrConstEnumOnlyModule(s *ast.Symbol) bool {
	return isConstEnumSymbol(s) || s.Flags&ast.SymbolFlagsConstEnumOnlyModule != 0
}
func (r *EmitResolver) IsReferencedAliasDeclaration(node ast.Handle) bool {
	c := r.checker
	if !c.canCollectSymbolAliasAccessibilityData || !ast.IsParseTreeNode(node) {
		return true
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	if ast.IsAliasSymbolDeclaration(node) {
		if symbol := c.getSymbolOfDeclaration(node); symbol != nil {
			aliasLinks := c.aliasSymbolLinks.Get(symbol)
			if aliasLinks.referenced {
				return true
			}
			target := aliasLinks.aliasTarget
			if target != nil && node.ModifierFlags()&ast.ModifierFlagsExport != 0 && c.getSymbolFlags(target)&ast.SymbolFlagsValue != 0 && (c.compilerOptions.ShouldPreserveConstEnums() || !isConstEnumOrConstEnumOnlyModule(target)) {
				return true
			}
		}
	}
	return false
}
func (r *EmitResolver) IsValueAliasDeclaration(node ast.Handle) bool {
	c := r.checker
	if !c.canCollectSymbolAliasAccessibilityData || !ast.IsParseTreeNode(node) {
		return true
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.isValueAliasDeclarationWorker(node)
}
func (r *EmitResolver) isValueAliasDeclarationWorker(node ast.Handle) bool {
	c := r.checker
	switch node.Kind() {
	case ast.KindImportEqualsDeclaration:
		return r.isAliasResolvedToValue(c.getSymbolOfDeclaration(node), false)
	case ast.KindImportClause, ast.KindNamespaceImport, ast.KindImportSpecifier, ast.KindExportSpecifier:
		symbol := c.getSymbolOfDeclaration(node)
		return symbol != nil && r.isAliasResolvedToValue(symbol, true)
	case ast.KindExportDeclaration:
		exportClause := node.ExportDeclarationExportClause()
		return !exportClause.IsNil() && (ast.IsNamespaceExport(exportClause) || core.Some(exportClause.Elements(), r.isValueAliasDeclaration))
	case ast.KindExportAssignment:
		if !node.Expression().IsNil() && node.Expression().Kind() == ast.KindIdentifier {
			return r.isAliasResolvedToValue(c.getSymbolOfDeclaration(node), true)
		}
		return true
	case ast.KindBinaryExpression:
		if isCommonJSModuleExports(node) && ast.IsIdentifier(node.BinaryExpressionRight()) {
			return r.isAliasResolvedToValue(c.getSymbolOfDeclaration(node), true)
		}
	}
	return false
}
func (r *EmitResolver) isAliasResolvedToValue(symbol *ast.Symbol, excludeTypeOnlyValues bool) bool {
	c := r.checker
	if symbol == nil {
		return false
	}
	if symbol.ValueDeclaration != 0 {
		if container := ast.GetSourceFileOfNode(ast.NodeOf(symbol.ValueDeclaration)); container != nil {
			fileSymbol := c.getSymbolOfDeclaration(container.ParseRoot())
			c.resolveExternalModuleSymbol(fileSymbol, false)
		}
	}
	target := c.getExportSymbolOfValueSymbolIfExported(c.resolveAlias(symbol))
	if target == c.unknownSymbol {
		return !excludeTypeOnlyValues || c.getTypeOnlyAliasDeclaration(symbol).IsNil()
	}
	return c.getSymbolFlagsEx(symbol, excludeTypeOnlyValues, true)&ast.SymbolFlagsValue != 0 && (c.compilerOptions.ShouldPreserveConstEnums() || !isConstEnumOrConstEnumOnlyModule(target))
}
func (r *EmitResolver) IsTopLevelValueImportEqualsWithEntityName(node ast.Handle) bool {
	c := r.checker
	if !c.canCollectSymbolAliasAccessibilityData {
		return true
	}
	if !ast.IsParseTreeNode(node) || node.Kind() != ast.KindImportEqualsDeclaration || node.Parent().Kind() != ast.KindSourceFile {
		return false
	}
	if ast.IsImportEqualsDeclaration(node) && (ast.NodeIsMissing(node.ImportEqualsDeclarationModuleReference()) || node.ImportEqualsDeclarationModuleReference().Kind() == ast.KindExternalModuleReference) {
		return false
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.isAliasResolvedToValue(c.getSymbolOfDeclaration(node), false)
}
func (r *EmitResolver) MarkLinkedReferencesRecursively(file *ast.SourceFile) {
	if file == nil || !ast.IsParseTreeNode(file.ParseRoot()) {
		return
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	if file != nil {
		var visit ast.StoreVisitor
		visit = func(n ast.Handle) bool {
			if ast.IsImportEqualsDeclaration(n) && n.ModifierFlags()&ast.ModifierFlagsExport == 0 {
				return false
			}
			if ast.IsImportDeclaration(n) {
				return false
			}
			r.checker.markLinkedReferences(n, ReferenceHintUnspecified, nil, nil)
			n.ForEachChild(visit)
			return false
		}
		file.ParseRoot().ForEachChild(visit)
	}
}
func (r *EmitResolver) GetExternalModuleFileFromDeclaration(declaration ast.Handle) *ast.SourceFile {
	if !ast.IsParseTreeNode(declaration) {
		return nil
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.checker.getExternalModuleFileFromDeclaration(declaration)
}
func (r *EmitResolver) getReferenceResolver() binder.ReferenceResolver {
	if r.referenceResolver == nil {
		r.referenceResolver = binder.NewReferenceResolver(r.checker.compilerOptions, binder.ReferenceResolverHooks{ResolveName: r.checker.resolveName, GetResolvedSymbol: r.checker.getResolvedSymbolOrNil, GetMergedSymbol: r.checker.getMergedSymbol, GetParentOfSymbol: r.checker.getParentOfSymbol, GetSymbolOfDeclaration: r.checker.getSymbolOfDeclaration, GetTypeOnlyAliasDeclaration: r.checker.getTypeOnlyAliasDeclarationEx, GetExportSymbolOfValueSymbolIfExported: r.checker.getExportSymbolOfValueSymbolIfExported, GetElementAccessExpressionName: r.checker.tryGetElementAccessExpressionName})
	}
	return r.referenceResolver
}
func (r *EmitResolver) GetReferencedExportContainer(node ast.Handle, prefixLocals bool) ast.Handle {
	if !ast.IsParseTreeNode(node) {
		return ast.Handle{}
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.getReferenceResolver().GetReferencedExportContainer(node, prefixLocals)
}
func (r *EmitResolver) SetReferencedImportDeclaration(node ast.Handle, ref ast.Handle) {
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	r.jsxLinks.Get(node).importRef = ref
}
func (r *EmitResolver) GetReferencedImportDeclaration(node ast.Handle) ast.Handle {
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	if !ast.IsParseTreeNode(node) {
		return r.jsxLinks.Get(node).importRef
	}
	symbol := r.checker.getReferencedValueOrAliasSymbol(node)
	if ast.IsNonLocalAlias(symbol, ast.SymbolFlagsValue) && r.checker.getTypeOnlyAliasDeclarationEx(symbol, ast.SymbolFlagsValue).IsNil() {
		return r.checker.getDeclarationOfAliasSymbol(symbol)
	}
	return ast.Handle{}
}
func (r *EmitResolver) GetReferencedValueDeclaration(node ast.Handle) ast.Handle {
	if !ast.IsParseTreeNode(node) {
		return ast.Handle{}
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.getReferenceResolver().GetReferencedValueDeclaration(node)
}
func (r *EmitResolver) GetReferencedValueDeclarationUnsafe(node ast.Handle) ast.Handle {
	return r.getReferenceResolver().GetReferencedValueDeclaration(node)
}
func (r *EmitResolver) GetReferencedValueDeclarations(node ast.Handle) []ast.Handle {
	if !ast.IsParseTreeNode(node) {
		return nil
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.getReferenceResolver().GetReferencedValueDeclarations(node)
}

func (r *EmitResolver) IsNameResolvable(location ast.Handle, name string) bool {
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	symbol := r.checker.resolveName(location, name, ast.SymbolFlagsValue|ast.SymbolFlagsType|ast.SymbolFlagsNamespace, nil, false, false)
	return symbol != nil
}
func (r *EmitResolver) GetElementAccessExpressionName(expression ast.Handle) string {
	if !ast.IsParseTreeNode(expression) {
		return ""
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.getReferenceResolver().GetElementAccessExpressionName(expression)
}
func (r *EmitResolver) GetReferencedMemberValueDeclaration(node ast.Handle) ast.Handle {
	if !ast.IsParseTreeNode(node) {
		return ast.Handle{}
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.getReferenceResolver().GetReferencedMemberValueDeclaration(node)
}
func (r *EmitResolver) CreateReturnTypeOfSignatureDeclaration(emitContext *printer.EmitContext, signatureDeclaration ast.Handle, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	original := emitContext.ParseNode(signatureDeclaration)
	if original.IsNil() {
		return emitContext.Factory.NewKeywordTypeNode(ast.KindAnyKeyword)
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	requestNodeBuilder := NewNodeBuilder(r.checker, emitContext)
	return requestNodeBuilder.SerializeReturnTypeForSignature(original, enclosingDeclaration, flags, internalFlags, tracker)
}
func (r *EmitResolver) CreateTypeParametersOfSignatureDeclaration(emitContext *printer.EmitContext, signatureDeclaration ast.Handle, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) []ast.Handle {
	original := emitContext.ParseNode(signatureDeclaration)
	if original.IsNil() {
		return nil
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	requestNodeBuilder := NewNodeBuilder(r.checker, emitContext)
	return requestNodeBuilder.SerializeTypeParametersForSignature(original, enclosingDeclaration, flags, internalFlags, tracker)
}
func (r *EmitResolver) CreateTypeOfDeclaration(emitContext *printer.EmitContext, declaration ast.Handle, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	original := emitContext.ParseNode(declaration)
	if original.IsNil() {
		return emitContext.Factory.NewKeywordTypeNode(ast.KindAnyKeyword)
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	requestNodeBuilder := NewNodeBuilder(r.checker, emitContext)
	symbol := r.checker.getSymbolOfDeclaration(declaration)
	return requestNodeBuilder.SerializeTypeForDeclaration(declaration, symbol, enclosingDeclaration, flags|nodebuilder.FlagsMultilineObjectLiterals, internalFlags, tracker)
}
func (r *EmitResolver) CreateLiteralConstValue(emitContext *printer.EmitContext, node ast.Handle, tracker nodebuilder.SymbolTracker) ast.Handle {
	node = emitContext.ParseNode(node)
	r.checkerMu.Lock()
	t := r.checker.getTypeOfSymbol(r.checker.getSymbolOfDeclaration(node))
	r.checkerMu.Unlock()
	if t == nil {
		return ast.Handle{}
	}
	var enumResult ast.Handle
	if t.flags&TypeFlagsEnumLike != 0 {
		r.checkerMu.Lock()
		defer r.checkerMu.Unlock()
		requestNodeBuilder := NewNodeBuilder(r.checker, emitContext)
		enumResult = requestNodeBuilder.SymbolToExpression(t.symbol, ast.SymbolFlagsValue, node, nodebuilder.FlagsNone, nodebuilder.InternalFlagsNone, tracker)
	} else if t == r.checker.trueType {
		enumResult = emitContext.Factory.NewKeywordExpression(ast.KindTrueKeyword)
	} else if t == r.checker.falseType {
		enumResult = emitContext.Factory.NewKeywordExpression(ast.KindFalseKeyword)
	}
	if !enumResult.IsNil() {
		return enumResult
	}
	if t.flags&TypeFlagsLiteral == 0 {
		return ast.Handle{}
	}
	switch value := t.AsLiteralType().value.(type) {
	case string:
		return emitContext.Factory.NewStringLiteral(value, ast.TokenFlagsNone)
	case jsnum.Number:
		if value.IsInf() {
			if value > 0 {
				return emitContext.Factory.NewIdentifier("Infinity")
			}
			return emitContext.Factory.NewPrefixUnaryExpression(ast.KindMinusToken, emitContext.Factory.NewIdentifier("Infinity"))
		}
		if value.IsNaN() {
			return emitContext.Factory.NewIdentifier("NaN")
		}
		if value.Abs() != value {
			return emitContext.Factory.NewPrefixUnaryExpression(ast.KindMinusToken, emitContext.Factory.NewNumericLiteral(value.String()[1:], ast.TokenFlagsNone))
		}
		return emitContext.Factory.NewNumericLiteral(value.String(), ast.TokenFlagsNone)
	case jsnum.PseudoBigInt:
		return emitContext.Factory.NewBigIntLiteral(pseudoBigIntToString(value)+"n", ast.TokenFlagsNone)
	case bool:
		kind := ast.KindFalseKeyword
		if value {
			kind = ast.KindTrueKeyword
		}
		return emitContext.Factory.NewKeywordExpression(kind)
	}
	panic("unhandled literal const value kind")
}
func (r *EmitResolver) CreateTypeOfExpression(emitContext *printer.EmitContext, expression ast.Handle, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	expression = emitContext.ParseNode(expression)
	if expression.IsNil() {
		return emitContext.Factory.NewKeywordTypeNode(ast.KindAnyKeyword)
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	requestNodeBuilder := NewNodeBuilder(r.checker, emitContext)
	return requestNodeBuilder.SerializeTypeForExpression(expression, enclosingDeclaration, flags|nodebuilder.FlagsMultilineObjectLiterals, internalFlags, tracker)
}
func (r *EmitResolver) CreateLateBoundIndexSignatures(emitContext *printer.EmitContext, container ast.Handle, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) []ast.Handle {
	container = emitContext.ParseNode(container)
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	sym := container.Symbol()
	staticInfos := r.checker.getIndexInfosOfType(r.checker.getTypeOfSymbol(sym))
	instanceIndexSymbol := r.checker.getIndexSymbol(sym)
	var instanceInfos []*IndexInfo
	if instanceIndexSymbol != nil {
		siblingSymbols := slices.Collect(maps.Values(r.checker.getMembersOfSymbol(sym)))
		instanceInfos = r.checker.getIndexInfosOfIndexSymbol(instanceIndexSymbol, siblingSymbols)
	}
	requestNodeBuilder := NewNodeBuilder(r.checker, emitContext)
	var result []ast.Handle
	for i, infoList := range [][]*IndexInfo{staticInfos, instanceInfos} {
		isStatic := true
		if i > 0 {
			isStatic = false
		}
		if len(infoList) == 0 {
			continue
		}
		for _, info := range infoList {
			if !info.declaration.IsNil() {
				continue
			}
			if info == r.checker.anyBaseTypeIndexInfo {
				continue
			}
			if len(info.components) != 0 {
				allComponentComputedNamesSerializable := !enclosingDeclaration.IsNil() && core.Every(info.components, func(c ast.Handle) bool {
					return !c.Name().IsNil() && ast.IsComputedPropertyName(c.Name()) && ast.IsEntityNameExpression(c.Name().Expression()) && r.isEntityNameVisible(c.Name().Expression(), enclosingDeclaration, false).Accessibility == printer.SymbolAccessibilityAccessible
				})
				if allComponentComputedNamesSerializable {
					for _, c := range info.components {
						if r.checker.hasLateBindableName(c) {
							continue
						}
						firstIdentifier := ast.GetFirstIdentifier(c.Name().Expression())
						name := r.checker.resolveName(firstIdentifier, firstIdentifier.Text(), ast.SymbolFlagsValue|ast.SymbolFlagsExportValue, nil, true, false)
						if name != nil {
							tracker.TrackSymbol(name, enclosingDeclaration, ast.SymbolFlagsValue)
						}
						mods := core.IfElse(isStatic, []ast.Handle{emitContext.Factory.NewModifier(ast.KindStaticKeyword)}, nil)
						if info.isReadonly {
							mods = append(mods, emitContext.Factory.NewModifier(ast.KindReadonlyKeyword))
						}
						decl := emitContext.Factory.NewPropertyDeclaration(core.IfElse(len(mods) != 0, emitContext.Factory.NewModifierList(mods), 0), c.Name(), c.QuestionToken(), requestNodeBuilder.TypeToTypeNode(r.checker.getTypeOfSymbol(c.Symbol()), enclosingDeclaration, flags, internalFlags, tracker), ast.Handle{})
						result = append(result, decl)
					}
					continue
				}
			}
			node := requestNodeBuilder.IndexInfoToIndexSignatureDeclaration(info, enclosingDeclaration, flags, internalFlags, tracker)
			if !node.IsNil() && isStatic {
				modNodes := []ast.Handle{emitContext.Factory.NewModifier(ast.KindStaticKeyword)}
				modNodes = append(modNodes, node.ModifierNodes()...)
				mods := emitContext.Factory.NewModifierList(modNodes)
				node = emitContext.Factory.UpdateIndexSignatureDeclaration(node, mods, node.ParameterList(), node.Type())
			}
			if !node.IsNil() {
				result = append(result, node)
			}
		}
	}
	return result
}
func (r *EmitResolver) GetEffectiveDeclarationFlags(node ast.Handle, flags ast.ModifierFlags) ast.ModifierFlags {
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.checker.GetEffectiveDeclarationFlags(node, flags)
}
func (r *EmitResolver) GetResolutionModeOverride(node ast.Handle) core.ResolutionMode {
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.checker.GetResolutionModeOverride(node, false)
}
func (r *EmitResolver) GetConstantValue(node ast.Handle) any {
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	return r.checker.GetConstantValue(node)
}
func (r *EmitResolver) GetTypeReferenceSerializationKind(typeName ast.Handle, location ast.Handle) printer.TypeReferenceSerializationKind {
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	if typeName.IsNil() || location.IsNil() {
		return printer.TypeReferenceSerializationKindUnknown
	}
	isTypeOnly := false
	if ast.IsQualifiedName(typeName) {
		rootValueSymbol := r.checker.resolveEntityName(ast.GetFirstIdentifier(typeName), ast.SymbolFlagsValue, true, true, location)
		if rootValueSymbol != nil && len(rootValueSymbol.Declarations) > 0 {
			isTypeOnly = ast.EveryDeclaration(rootValueSymbol, ast.IsTypeOnlyImportOrExportDeclaration)
		}
	}
	valueSymbol := r.checker.resolveEntityName(typeName, ast.SymbolFlagsValue, true, true, location)
	resolvedValueSymbol := valueSymbol
	if valueSymbol != nil && valueSymbol.Flags&ast.SymbolFlagsAlias != 0 {
		resolvedValueSymbol = r.checker.resolveAlias(valueSymbol)
	}
	isTypeOnly = isTypeOnly || (valueSymbol != nil && !r.checker.getTypeOnlyAliasDeclarationEx(valueSymbol, ast.SymbolFlagsValue).IsNil())
	typeSymbol := r.checker.resolveEntityName(typeName, ast.SymbolFlagsType, true, true, location)
	resolvedTypeSymbol := typeSymbol
	if typeSymbol != nil && typeSymbol.Flags&ast.SymbolFlagsAlias != 0 {
		resolvedTypeSymbol = r.checker.resolveAlias(typeSymbol)
	}
	isTypeOnly = isTypeOnly || (typeSymbol != nil && !r.checker.getTypeOnlyAliasDeclarationEx(typeSymbol, ast.SymbolFlagsType).IsNil())
	if resolvedValueSymbol != nil && resolvedValueSymbol == resolvedTypeSymbol {
		globalPromiseSymbol := r.checker.getGlobalPromiseConstructorSymbol()
		if globalPromiseSymbol != nil && resolvedValueSymbol == globalPromiseSymbol {
			return printer.TypeReferenceSerializationKindPromise
		}
		constructorType := r.checker.getTypeOfSymbol(resolvedValueSymbol)
		if constructorType != nil && r.checker.isConstructorType(constructorType) {
			if isTypeOnly {
				return printer.TypeReferenceSerializationKindTypeWithCallSignature
			}
			return printer.TypeReferenceSerializationKindTypeWithConstructSignatureAndValue
		}
	}
	if resolvedTypeSymbol == nil {
		if isTypeOnly {
			return printer.TypeReferenceSerializationKindObjectType
		}
		return printer.TypeReferenceSerializationKindUnknown
	}
	type_ := r.checker.getDeclaredTypeOfSymbol(resolvedTypeSymbol)
	if r.checker.isErrorType(type_) {
		if isTypeOnly {
			return printer.TypeReferenceSerializationKindObjectType
		}
		return printer.TypeReferenceSerializationKindUnknown
	}
	if type_.flags&TypeFlagsAnyOrUnknown != 0 {
		return printer.TypeReferenceSerializationKindObjectType
	} else if r.checker.isTypeAssignableToKind(type_, TypeFlagsVoid|TypeFlagsNullable|TypeFlagsNever) {
		return printer.TypeReferenceSerializationKindVoidNullableOrNeverType
	} else if r.checker.isTypeAssignableToKind(type_, TypeFlagsBooleanLike) {
		return printer.TypeReferenceSerializationKindBooleanType
	} else if r.checker.isTypeAssignableToKind(type_, TypeFlagsNumberLike) {
		return printer.TypeReferenceSerializationKindNumberLikeType
	} else if r.checker.isTypeAssignableToKind(type_, TypeFlagsBigIntLike) {
		return printer.TypeReferenceSerializationKindBigIntLikeType
	} else if r.checker.isTypeAssignableToKind(type_, TypeFlagsStringLike) {
		return printer.TypeReferenceSerializationKindStringLikeType
	} else if isTupleType(type_) {
		return printer.TypeReferenceSerializationKindArrayLikeType
	} else if r.checker.isTypeAssignableToKind(type_, TypeFlagsESSymbolLike) {
		return printer.TypeReferenceSerializationKindESSymbolType
	} else if r.checker.isFunctionType(type_) {
		return printer.TypeReferenceSerializationKindTypeWithCallSignature
	} else if r.checker.isArrayType(type_) {
		return printer.TypeReferenceSerializationKindArrayLikeType
	} else {
		return printer.TypeReferenceSerializationKindObjectType
	}
}
func (r *EmitResolver) GetPropertiesOfContainerFunction(node ast.Handle) []*ast.Symbol {
	if node.IsNil() {
		return []*ast.Symbol{}
	}
	s := r.checker.getSymbolOfDeclaration(node)
	if s == nil {
		return []*ast.Symbol{}
	}
	return r.checker.getPropertiesOfType(r.checker.getTypeOfSymbol(s))
}
func (r *EmitResolver) TryJSTypeNodeToTypeNode(emitContext *printer.EmitContext, typeNode ast.Handle, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle {
	typeNode = emitContext.ParseNode(typeNode)
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	requestNodeBuilder := NewNodeBuilder(r.checker, emitContext)
	return requestNodeBuilder.TryJSTypeNodeToTypeNode(typeNode, enclosingDeclaration, flags, internalFlags, tracker)
}

func (r *EmitResolver) IsThisPropertyAssignmentDeclarationRedundant(node ast.Handle) bool {
	if node.IsNil() {
		return false
	}
	r.checkerMu.Lock()
	defer r.checkerMu.Unlock()
	s := r.checker.getSymbolOfDeclaration(node)
	if s == nil || s.Parent == nil {
		return false
	}
	parentType := r.checker.getDeclaredTypeOfSymbol(s.Parent)
	if parentType == nil {
		return false
	}
	for _, base := range r.checker.getBaseTypes(parentType) {
		baseProp := r.checker.getPropertyOfType(base, s.Name)
		if baseProp == nil {
			continue
		}
		if baseProp.Flags&(ast.SymbolFlagsAccessor|ast.SymbolFlagsMethod|ast.SymbolFlagsFunction) != 0 {
			return true
		}
		if r.checker.isReadonlySymbol(baseProp) == r.checker.isReadonlySymbol(s) && (s.Flags&ast.SymbolFlagsOptional) == (baseProp.Flags&ast.SymbolFlagsOptional) && r.checker.isTypeIdenticalTo(r.checker.getTypeOfSymbol(s), r.checker.getTypeOfSymbol(baseProp)) {
			return true
		}
	}
	return false
}
