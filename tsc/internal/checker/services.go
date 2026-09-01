package checker

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"maps"
	"slices"
	"strings"
)

func (c *Checker) GetSymbolsInScope(location ast.Handle, meaning ast.SymbolFlags) []*ast.Symbol {
	return c.getSymbolsInScope(location, meaning)
}
func (c *Checker) getSymbolsInScope(location ast.Handle, meaning ast.SymbolFlags) []*ast.Symbol {
	if location.Flags()&ast.NodeFlagsInWithStatement != 0 {
		return nil
	}
	symbols := make(ast.SymbolTable)
	isStaticSymbol := false
	copySymbol := func(symbol *ast.Symbol, meaning ast.SymbolFlags) {
		if symbol.CombinedLocalAndExportSymbolFlags()&meaning != 0 {
			id := symbol.Name
			if _, ok := symbols[id]; !ok {
				symbols[id] = symbol
			}
		}
	}
	copySymbols := func(source ast.SymbolTable, meaning ast.SymbolFlags) {
		if meaning != 0 {
			for _, symbol := range source {
				copySymbol(symbol, meaning)
			}
		}
	}
	copyLocallyVisibleExportSymbols := func(source ast.SymbolTable, meaning ast.SymbolFlags) {
		if meaning != 0 {
			for _, symbol := range source {
				if ast.GetDeclarationOfKind(symbol, ast.KindExportSpecifier).IsNil() && ast.GetDeclarationOfKind(symbol, ast.KindNamespaceExport).IsNil() && symbol.Name != ast.InternalSymbolNameDefault {
					copySymbol(symbol, meaning)
				}
			}
		}
	}
	populateSymbols := func() {
		for !location.IsNil() {
			if canHaveLocals(location) && location.Locals() != nil && !ast.IsGlobalSourceFile(location) {
				copySymbols(location.Locals(), meaning)
			}
			switch location.Kind() {
			case ast.KindSourceFile:
				if !ast.IsExternalModule(ast.GetSourceFileOfNode(location)) {
					break
				}
				fallthrough
			case ast.KindModuleDeclaration:
				copyLocallyVisibleExportSymbols(c.getSymbolOfDeclaration(location).Exports, meaning&ast.SymbolFlagsModuleMember)
			case ast.KindEnumDeclaration:
				copySymbols(c.getSymbolOfDeclaration(location).Exports, meaning&ast.SymbolFlagsEnumMember)
			case ast.KindClassExpression:
				className := location.ClassExpressionName()
				if !className.IsNil() {
					copySymbol(location.Symbol(), meaning)
				}
				fallthrough
			case ast.KindClassDeclaration, ast.KindInterfaceDeclaration:
				if !isStaticSymbol {
					copySymbols(c.getMembersOfSymbol(c.getSymbolOfDeclaration(location)), meaning&ast.SymbolFlagsType)
				}
			case ast.KindFunctionExpression:
				funcName := location.Name()
				if !funcName.IsNil() {
					copySymbol(location.Symbol(), meaning)
				}
			}
			if introducesArgumentsExoticObject(location) {
				copySymbol(c.argumentsSymbol, meaning)
			}
			isStaticSymbol = ast.IsStatic(location)
			location = location.Parent()
		}
		copySymbols(c.globals, meaning)
	}
	populateSymbols()
	delete(symbols, ast.InternalSymbolNameThis)
	return symbolsToArray(symbols)
}
func (c *Checker) GetExportsOfModule(symbol *ast.Symbol) []*ast.Symbol {
	return symbolsToArray(c.getExportsOfModule(symbol))
}
func (c *Checker) ForEachExportAndPropertyOfModule(moduleSymbol *ast.Symbol, cb func(*ast.Symbol, string)) {
	for key, exportedSymbol := range c.getExportsOfModule(moduleSymbol) {
		if !isReservedMemberName(key) {
			cb(exportedSymbol, key)
		}
	}
	exportEquals := c.resolveExternalModuleSymbol(moduleSymbol, false)
	if exportEquals == moduleSymbol {
		return
	}
	typeOfSymbol := c.getTypeOfSymbol(exportEquals)
	if !c.shouldTreatPropertiesOfExternalModuleAsExports(typeOfSymbol) {
		return
	}
	reducedType := c.getReducedApparentType(typeOfSymbol)
	if reducedType.flags&TypeFlagsStructuredType == 0 {
		return
	}
	for name, symbol := range c.resolveStructuredTypeMembers(reducedType).members {
		if c.isNamedMember(symbol, name) {
			cb(symbol, name)
		}
	}
}
func (c *Checker) IsValidPropertyAccess(node ast.Handle, propertyName string) bool {
	return c.isValidPropertyAccess(node, propertyName)
}
func (c *Checker) isValidPropertyAccess(node ast.Handle, propertyName string) bool {
	switch node.Kind() {
	case ast.KindPropertyAccessExpression:
		return c.isValidPropertyAccessWithType(node, node.Expression().Kind() == ast.KindSuperKeyword, propertyName, c.getWidenedType(c.checkExpression(node.Expression())))
	case ast.KindQualifiedName:
		return c.isValidPropertyAccessWithType(node, false, propertyName, c.getWidenedType(c.checkExpression(node.QualifiedNameLeft())))
	case ast.KindImportType:
		return c.isValidPropertyAccessWithType(node, false, propertyName, c.getTypeFromTypeNode(node))
	}
	panic("Unexpected node kind in isValidPropertyAccess: " + node.Kind().String())
}
func (c *Checker) isValidPropertyAccessWithType(node ast.Handle, isSuper bool, propertyName string, t *Type) bool {
	if IsTypeAny(t) {
		return true
	}
	prop := c.getPropertyOfType(t, propertyName)
	return prop != nil && c.isPropertyAccessible(node, isSuper, false, t, prop)
}

func (c *Checker) IsValidPropertyAccessForCompletions(node ast.Handle, t *Type, property *ast.Symbol) bool {
	return c.isPropertyAccessible(node, node.Kind() == ast.KindPropertyAccessExpression && node.Expression().Kind() == ast.KindSuperKeyword, false, t, property)
}
func (c *Checker) GetAllPossiblePropertiesOfTypes(types []*Type) []*ast.Symbol {
	unionType := c.getUnionType(types)
	if unionType.flags&TypeFlagsUnion == 0 {
		return c.getAugmentedPropertiesOfType(unionType)
	}
	props := make(ast.SymbolTable)
	for _, memberType := range types {
		augmentedProps := c.getAugmentedPropertiesOfType(memberType)
		for _, p := range augmentedProps {
			if _, ok := props[p.Name]; !ok {
				prop := c.createUnionOrIntersectionProperty(unionType, p.Name, false)
				if prop != nil {
					props[p.Name] = prop
				}
			}
		}
	}
	return slices.Collect(maps.Values(props))
}
func (c *Checker) IsUnknownSymbol(symbol *ast.Symbol) bool {
	return symbol == c.unknownSymbol
}
func (c *Checker) IsUndefinedSymbol(symbol *ast.Symbol) bool {
	return symbol == c.undefinedSymbol
}
func (c *Checker) IsArgumentsSymbol(symbol *ast.Symbol) bool {
	return symbol == c.argumentsSymbol
}

func (c *Checker) GetNonOptionalType(t *Type) *Type {
	return c.removeOptionalTypeMarker(t)
}
func (c *Checker) GetStringIndexType(t *Type) *Type {
	return c.getIndexTypeOfType(t, c.stringType)
}
func (c *Checker) GetNumberIndexType(t *Type) *Type {
	return c.getIndexTypeOfType(t, c.numberType)
}
func (c *Checker) GetElementTypeOfArrayType(t *Type) *Type {
	return c.getElementTypeOfArrayType(t)
}
func (c *Checker) GetCallSignatures(t *Type) []*Signature {
	return c.getSignaturesOfType(t, SignatureKindCall)
}
func (c *Checker) GetConstructSignatures(t *Type) []*Signature {
	return c.getSignaturesOfType(t, SignatureKindConstruct)
}
func (c *Checker) GetApparentProperties(t *Type) []*ast.Symbol {
	return c.getAugmentedPropertiesOfType(t)
}
func (c *Checker) getAugmentedPropertiesOfType(t *Type) []*ast.Symbol {
	t = c.getApparentType(t)
	propsByName := createSymbolTable(c.getPropertiesOfType(t))
	var functionType *Type
	if len(c.getSignaturesOfType(t, SignatureKindCall)) > 0 {
		functionType = c.globalCallableFunctionType
	} else if len(c.getSignaturesOfType(t, SignatureKindConstruct)) > 0 {
		functionType = c.globalNewableFunctionType
	}
	if propsByName == nil {
		propsByName = make(ast.SymbolTable)
	}
	if functionType != nil {
		for _, p := range c.getPropertiesOfType(functionType) {
			if _, ok := propsByName[p.Name]; !ok {
				propsByName[p.Name] = p
			}
		}
	}
	return c.getNamedMembers(propsByName, nil)
}
func (c *Checker) TryGetMemberInModuleExportsAndProperties(memberName string, moduleSymbol *ast.Symbol) *ast.Symbol {
	symbol := c.TryGetMemberInModuleExports(memberName, moduleSymbol)
	if symbol != nil {
		return symbol
	}
	exportEquals := c.resolveExternalModuleSymbol(moduleSymbol, false)
	if exportEquals == moduleSymbol {
		return nil
	}
	t := c.getTypeOfSymbol(exportEquals)
	if c.shouldTreatPropertiesOfExternalModuleAsExports(t) {
		return c.getPropertyOfType(t, memberName)
	}
	return nil
}
func (c *Checker) TryGetMemberInModuleExports(memberName string, moduleSymbol *ast.Symbol) *ast.Symbol {
	symbolTable := c.getExportsOfModule(moduleSymbol)
	return symbolTable[memberName]
}
func (c *Checker) shouldTreatPropertiesOfExternalModuleAsExports(resolvedExternalModuleType *Type) bool {
	return resolvedExternalModuleType.flags&TypeFlagsPrimitive == 0 || resolvedExternalModuleType.objectFlags&ObjectFlagsClass != 0 || c.isArrayType(resolvedExternalModuleType) || isTupleType(resolvedExternalModuleType)
}
func (c *Checker) GetContextualType(node ast.Handle, contextFlags ContextFlags) *Type {
	if contextFlags&ContextFlagsIgnoreNodeInferences != 0 {
		return runWithInferenceBlockedFromSourceNode(c, node, func() *Type {
			return c.getContextualType(node, contextFlags)
		})
	}
	return c.getContextualType(node, contextFlags)
}
func runWithInferenceBlockedFromSourceNode[T any](c *Checker, node ast.Handle, fn func() T) T {
	containingCall := ast.FindAncestor(node, ast.IsCallLikeExpression)
	if !containingCall.IsNil() {
		toMarkSkip := node
		for {
			c.skipDirectInferenceNodes.Add(toMarkSkip)
			toMarkSkip = toMarkSkip.Parent()
			if toMarkSkip.IsNil() || toMarkSkip == containingCall {
				break
			}
		}
	}
	c.isInferencePartiallyBlocked = true
	result := runWithoutResolvedSignatureCaching(c, node, fn)
	c.isInferencePartiallyBlocked = false
	c.skipDirectInferenceNodes.Clear()
	return result
}
func GetResolvedSignatureForSignatureHelp(node ast.Handle, argumentCount int, c *Checker) (*Signature, []*Signature) {
	type result struct {
		signature  *Signature
		candidates []*Signature
	}
	res := runWithoutResolvedSignatureCaching(c, node, func() result {
		signature, candidates := c.getResolvedSignatureWorker(node, CheckModeIsForSignatureHelp, argumentCount)
		return result{signature, candidates}
	})
	return res.signature, res.candidates
}
func runWithoutResolvedSignatureCaching[T any](c *Checker, node ast.Handle, fn func() T) T {
	ancestorNode := ast.FindAncestor(node, ast.IsCallLikeOrFunctionLikeExpression)
	if !ancestorNode.IsNil() {
		cachedResolvedSignatures := make(map[*SignatureLinks]*Signature)
		cachedTypes := make(map[*ValueSymbolLinks]*Type)
		for !ancestorNode.IsNil() {
			signatureLinks := c.signatureLinks.Get(ancestorNode)
			cachedResolvedSignatures[signatureLinks] = signatureLinks.resolvedSignature
			signatureLinks.resolvedSignature = nil
			if ast.IsFunctionExpressionOrArrowFunction(ancestorNode) {
				symbolLinks := c.valueSymbolLinks.Get(c.getSymbolOfDeclaration(ancestorNode))
				resolvedType := symbolLinks.resolvedType
				cachedTypes[symbolLinks] = resolvedType
				symbolLinks.resolvedType = nil
			}
			ancestorNode = ast.FindAncestor(ancestorNode.Parent(), ast.IsCallLikeOrFunctionLikeExpression)
		}
		result := fn()
		for signatureLinks, resolvedSignature := range cachedResolvedSignatures {
			signatureLinks.resolvedSignature = resolvedSignature
		}
		for symbolLinks, resolvedType := range cachedTypes {
			symbolLinks.resolvedType = resolvedType
		}
		return result
	}
	return fn()
}
func (c *Checker) SkipAlias(symbol *ast.Symbol) *ast.Symbol {
	if symbol.Flags&ast.SymbolFlagsAlias != 0 {
		return c.GetAliasedSymbol(symbol)
	}
	return symbol
}
func (c *Checker) GetRootSymbols(symbol *ast.Symbol) []*ast.Symbol {
	roots := c.getImmediateRootSymbols(symbol)
	if len(roots) == 0 {
		return []*ast.Symbol{symbol}
	}
	var result []*ast.Symbol
	for _, root := range roots {
		result = append(result, c.GetRootSymbols(root)...)
	}
	return result
}
func (c *Checker) GetMappedTypeSymbolOfProperty(symbol *ast.Symbol) *ast.Symbol {
	if valueLinks := c.valueSymbolLinks.TryGet(symbol); valueLinks != nil {
		return valueLinks.containingType.symbol
	}
	return nil
}
func (c *Checker) getImmediateRootSymbols(symbol *ast.Symbol) []*ast.Symbol {
	if symbol.CheckFlags&ast.CheckFlagsSynthetic != 0 {
		return core.MapNonNil(c.valueSymbolLinks.Get(symbol).containingType.Types(), func(t *Type) *ast.Symbol {
			return c.getPropertyOfType(t, symbol.Name)
		})
	}
	if symbol.Flags&ast.SymbolFlagsTransient != 0 {
		if c.spreadLinks.Has(symbol) {
			leftSpread := c.spreadLinks.Get(symbol).leftSpread
			rightSpread := c.spreadLinks.Get(symbol).rightSpread
			if leftSpread != nil {
				return []*ast.Symbol{leftSpread, rightSpread}
			}
		}
		if c.mappedSymbolLinks.Has(symbol) {
			syntheticOrigin := c.mappedSymbolLinks.Get(symbol).syntheticOrigin
			if syntheticOrigin != nil {
				return []*ast.Symbol{syntheticOrigin}
			}
		}
		target := c.tryGetTarget(symbol)
		if target != nil {
			return []*ast.Symbol{target}
		}
	}
	return nil
}
func (c *Checker) tryGetTarget(symbol *ast.Symbol) *ast.Symbol {
	var target *ast.Symbol
	next := symbol
	for {
		if c.valueSymbolLinks.Has(next) {
			next = c.valueSymbolLinks.Get(next).target
		} else if c.exportTypeLinks.Has(next) {
			next = c.exportTypeLinks.Get(next).target
		} else {
			next = nil
		}
		if next == nil {
			break
		}
		target = next
	}
	return target
}
func (c *Checker) GetExportSymbolOfSymbol(symbol *ast.Symbol) *ast.Symbol {
	return c.getMergedSymbol(core.IfElse(symbol.ExportSymbol != nil, symbol.ExportSymbol, symbol))
}
func (c *Checker) GetExportSpecifierLocalTargetSymbol(node ast.Handle) *ast.Symbol {
	switch node.Kind() {
	case ast.KindExportSpecifier:
		if !node.Parent().Parent().ModuleSpecifier().IsNil() {
			return c.getExternalModuleMember(node.Parent().Parent(), node, false)
		}
		name := node.PropertyNameOrName()
		if name.Kind() == ast.KindStringLiteral {
			return nil
		}
		return c.resolveEntityName(name, ast.SymbolFlagsValue|ast.SymbolFlagsType|ast.SymbolFlagsNamespace|ast.SymbolFlagsAlias, true, false, ast.Handle{})
	case ast.KindIdentifier:
		return c.resolveEntityName(node, ast.SymbolFlagsValue|ast.SymbolFlagsType|ast.SymbolFlagsNamespace|ast.SymbolFlagsAlias, true, false, ast.Handle{})
	}
	panic("Unhandled case in getExportSpecifierLocalTargetSymbol, node should be ExportSpecifier | Identifier")
}
func (c *Checker) GetShorthandAssignmentValueSymbol(location ast.Handle) *ast.Symbol {
	if !location.IsNil() && location.Kind() == ast.KindShorthandPropertyAssignment {
		return c.resolveEntityName(location.Name(), ast.SymbolFlagsValue|ast.SymbolFlagsAlias, true, false, ast.Handle{})
	}
	return nil
}

func (c *Checker) GetSymbolsOfParameterPropertyDeclaration(parameter ast.Handle, parameterName string) (*ast.Symbol, *ast.Symbol) {
	constructorDeclaration := parameter.Parent()
	classDeclaration := parameter.Parent().Parent()
	parameterSymbol := c.getSymbol(constructorDeclaration.Locals(), parameterName, ast.SymbolFlagsValue)
	propertySymbol := c.getSymbol(c.getMembersOfSymbol(classDeclaration.Symbol()), parameterName, ast.SymbolFlagsValue)
	if parameterSymbol != nil && propertySymbol != nil {
		return parameterSymbol, propertySymbol
	}
	panic("There should exist two symbols, one as property declaration and one as parameter declaration")
}

func (c *Checker) IsDeclarationUsed(sourceFile *ast.SourceFile, identifier ast.Handle, jsxElementsPresent bool, jsxModeNeedsExplicitImport bool) bool {
	if jsxElementsPresent && jsxModeNeedsExplicitImport {
		jsxNamespace := c.getJsxNamespace(sourceFile.ParseRoot())
		jsxFragmentFactory := c.GetJsxFragmentFactory(sourceFile.ParseRoot())
		identifierText := identifier.Text()
		if identifierText == jsxNamespace {
			return true
		}
		if jsxFragmentFactory != "" && identifierText == jsxFragmentFactory {
			return true
		}
	}
	symbol := c.GetSymbolAtLocation(identifier)
	if symbol == nil {
		return true
	}
	return c.IsSymbolReferencedInFile(sourceFile, identifier, symbol)
}

func (c *Checker) IsSymbolReferencedInFile(sourceFile *ast.SourceFile, definition ast.Handle, symbol *ast.Symbol) bool {
	identifierText := definition.Text()
	for _, token := range getPossibleSymbolReferenceNodes(sourceFile, identifierText, sourceFile.ParseRoot()) {
		if !ast.IsIdentifier(token) {
			continue
		}
		id := token
		if id == definition || id.Text() != identifierText {
			continue
		}
		refSymbol := c.GetSymbolAtLocation(token)
		if refSymbol == symbol {
			return true
		}
		if !token.Parent().IsNil() && token.Parent().Kind() == ast.KindShorthandPropertyAssignment {
			shorthandSymbol := c.GetShorthandAssignmentValueSymbol(token.Parent())
			if shorthandSymbol == symbol {
				return true
			}
		}
		if !token.Parent().IsNil() && ast.IsExportSpecifier(token.Parent()) {
			localSymbol := c.getLocalSymbolForExportSpecifier(token, refSymbol, token.Parent())
			if localSymbol == symbol {
				return true
			}
		}
	}
	return false
}

func (c *Checker) GetReferencesToSymbolInFile(sourceFile *ast.SourceFile, symbol *ast.Symbol) []ast.Handle {
	identifierText := symbol.Name
	var result []ast.Handle
	for _, token := range getPossibleSymbolReferenceNodes(sourceFile, identifierText, sourceFile.ParseRoot()) {
		if !ast.IsIdentifier(token) {
			continue
		}
		id := token
		if id.Text() != identifierText {
			continue
		}
		refSymbol := c.GetSymbolAtLocation(token)
		if refSymbol == symbol {
			result = append(result, token)
			continue
		}
		if !token.Parent().IsNil() && token.Parent().Kind() == ast.KindShorthandPropertyAssignment {
			shorthandSymbol := c.GetShorthandAssignmentValueSymbol(token.Parent())
			if shorthandSymbol == symbol {
				result = append(result, token)
				continue
			}
		}
		if !token.Parent().IsNil() && ast.IsExportSpecifier(token.Parent()) {
			localSymbol := c.getLocalSymbolForExportSpecifier(token, refSymbol, token.Parent())
			if localSymbol == symbol {
				result = append(result, token)
				continue
			}
		}
	}
	return result
}
func (c *Checker) getLocalSymbolForExportSpecifier(referenceLocation ast.Handle, referenceSymbol *ast.Symbol, exportSpecifier ast.Handle) *ast.Symbol {
	if isExportSpecifierAlias(referenceLocation, exportSpecifier) {
		if symbol := c.GetExportSpecifierLocalTargetSymbol(exportSpecifier); symbol != nil {
			return symbol
		}
	}
	return referenceSymbol
}
func isExportSpecifierAlias(referenceLocation ast.Handle, exportSpecifier ast.Handle) bool {
	debug.Assert(exportSpecifier.PropertyName() == referenceLocation || exportSpecifier.Name() == referenceLocation, "referenceLocation is not export specifier name or property name")
	propertyName := exportSpecifier.PropertyName()
	if !propertyName.IsNil() {
		return propertyName == referenceLocation
	} else {
		return exportSpecifier.Parent().Parent().ModuleSpecifier().IsNil()
	}
}
func getPossibleSymbolReferenceNodes(sourceFile *ast.SourceFile, symbolName string, container ast.Handle) []ast.Handle {
	return core.MapNonNil(getPossibleSymbolReferencePositions(sourceFile, symbolName, container), func(pos int) ast.Handle {
		if referenceLocation := astnav.GetTouchingPropertyName(sourceFile, pos); referenceLocation != sourceFile.ParseRoot() {
			return referenceLocation
		}
		return ast.Handle{}
	})
}
func getPossibleSymbolReferencePositions(sourceFile *ast.SourceFile, symbolName string, container ast.Handle) []int {
	positions := []int{}
	if symbolName == "" {
		return positions
	}
	text := sourceFile.Text()
	sourceLength := len(text)
	symbolNameLength := len(symbolName)
	if container.IsNil() {
		container = sourceFile.ParseRoot()
	}
	position := strings.Index(text[container.Pos():], symbolName)
	endPos := container.End()
	for position >= 0 && position < endPos {
		endPosition := position + symbolNameLength
		if (position == 0 || !scanner.IsIdentifierPart(rune(text[position-1]))) && (endPosition == sourceLength || !scanner.IsIdentifierPart(rune(text[endPosition]))) {
			positions = append(positions, position)
		}
		startIndex := position + symbolNameLength + 1
		if startIndex > len(text) {
			break
		}
		if foundIndex := strings.Index(text[startIndex:], symbolName); foundIndex != -1 {
			position = startIndex + foundIndex
		} else {
			break
		}
	}
	return positions
}
func (c *Checker) GetTypeArgumentConstraint(node ast.Handle) *Type {
	if !ast.IsTypeNode(node) {
		return nil
	}
	return c.getTypeArgumentConstraint(node)
}

func (c *Checker) getUninstantiatedSignatures(node ast.Handle) []*Signature {
	switch node.Kind() {
	case ast.KindCallExpression, ast.KindDecorator:
		return c.getSignaturesOfType(c.getTypeOfExpression(node.Expression()), SignatureKindCall)
	case ast.KindNewExpression:
		return c.getSignaturesOfType(c.getTypeOfExpression(node.Expression()), SignatureKindConstruct)
	case ast.KindJsxSelfClosingElement, ast.KindJsxOpeningElement:
		if isJsxIntrinsicTagName(node.TagName()) {
			return nil
		}
		return c.getSignaturesOfType(c.getTypeOfExpression(node.TagName()), SignatureKindCall)
	case ast.KindTaggedTemplateExpression:
		return c.getSignaturesOfType(c.getTypeOfExpression(node.TaggedTemplateExpressionTag()), SignatureKindCall)
	case ast.KindBinaryExpression, ast.KindJsxOpeningFragment:
		return nil
	}
	return nil
}
func (c *Checker) getTypeParameterConstraintForPositionAcrossSignatures(signatures []*Signature, position int) *Type {
	var relevantConstraints []*Type
	for _, signature := range signatures {
		if position >= len(signature.typeParameters) {
			continue
		}
		relevantTypeParameter := signature.typeParameters[position]
		relevantConstraint := c.getConstraintOfTypeParameter(relevantTypeParameter)
		if relevantConstraint != nil {
			relevantConstraints = append(relevantConstraints, relevantConstraint)
		}
	}
	return c.getUnionType(relevantConstraints)
}
func (c *Checker) getTypeArgumentConstraint(node ast.Handle) *Type {
	var typeArgumentPosition int = -1
	if ast.HasTypeArguments(node.Parent()) {
		typeArgs := node.Parent().TypeArguments()
		for i, arg := range typeArgs {
			if arg == node {
				typeArgumentPosition = i
				break
			}
		}
	}
	if typeArgumentPosition >= 0 {
		if ast.IsCallLikeExpression(node.Parent()) {
			return c.getTypeParameterConstraintForPositionAcrossSignatures(c.getUninstantiatedSignatures(node.Parent()), typeArgumentPosition)
		}
		if ast.IsDecorator(node.Parent().Parent()) {
			return c.getTypeParameterConstraintForPositionAcrossSignatures(c.getUninstantiatedSignatures(node.Parent().Parent()), typeArgumentPosition)
		}
		if ast.IsExpressionWithTypeArguments(node.Parent()) && ast.IsExpressionStatement(node.Parent().Parent()) {
			uninstantiatedType := c.checkExpression(node.Parent().Expression())
			callConstraint := c.getTypeParameterConstraintForPositionAcrossSignatures(c.getSignaturesOfType(uninstantiatedType, SignatureKindCall), typeArgumentPosition)
			constructConstraint := c.getTypeParameterConstraintForPositionAcrossSignatures(c.getSignaturesOfType(uninstantiatedType, SignatureKindConstruct), typeArgumentPosition)
			if constructConstraint.flags&TypeFlagsNever != 0 {
				return callConstraint
			}
			if callConstraint.flags&TypeFlagsNever != 0 {
				return constructConstraint
			}
			return c.getIntersectionType([]*Type{callConstraint, constructConstraint})
		}
		if ast.IsTypeReferenceType(node.Parent()) {
			typeParameters := c.getTypeParametersForTypeReferenceOrImport(node.Parent())
			if len(typeParameters) == 0 {
				return nil
			}
			if typeArgumentPosition >= len(typeParameters) {
				return nil
			}
			relevantTypeParameter := typeParameters[typeArgumentPosition]
			constraint := c.getConstraintOfTypeParameter(relevantTypeParameter)
			if constraint != nil {
				return c.instantiateType(constraint, newTypeMapper(typeParameters, c.getEffectiveTypeArguments(node.Parent(), typeParameters)))
			}
		}
	}
	return nil
}
func (c *Checker) IsTypeInvalidDueToUnionDiscriminant(contextualType *Type, obj ast.Handle) bool {
	properties := obj.Properties()
	return core.Some(properties, func(property ast.Handle) bool {
		var nameType *Type
		propertyName := property.Name
		if !propertyName().IsNil() {
			if ast.IsJsxNamespacedName(propertyName()) {
				nameType = c.getStringLiteralType(propertyName().Text())
			} else {
				nameType = c.getLiteralTypeFromPropertyName(propertyName())
			}
		}
		var name string
		if nameType != nil && isTypeUsableAsPropertyName(nameType) {
			name = getPropertyNameFromType(nameType)
		}
		var expected *Type
		if name != "" {
			expected = c.getTypeOfPropertyOfType(contextualType, name)
		}
		return expected != nil && isLiteralType(expected) && !c.isTypeAssignableTo(c.getTypeOfNode(property), expected)
	})
}

func (c *Checker) GetExportsAndPropertiesOfModule(moduleSymbol *ast.Symbol) []*ast.Symbol {
	exports := c.getExportsOfModuleAsArray(moduleSymbol)
	exportEquals := c.resolveExternalModuleSymbol(moduleSymbol, false)
	if exportEquals != moduleSymbol {
		t := c.getTypeOfSymbol(exportEquals)
		if c.shouldTreatPropertiesOfExternalModuleAsExports(t) {
			exports = append(exports, c.getPropertiesOfType(t)...)
		}
	}
	return exports
}
func (c *Checker) getExportsOfModuleAsArray(moduleSymbol *ast.Symbol) []*ast.Symbol {
	return symbolsToArray(c.getExportsOfModule(moduleSymbol))
}

func (c *Checker) GetJsxIntrinsicTagNamesAt(location ast.Handle) []*ast.Symbol {
	intrinsics := c.getJsxType(JsxNames.IntrinsicElements, location)
	if intrinsics == nil {
		return nil
	}
	return c.GetPropertiesOfType(intrinsics)
}
func (c *Checker) GetContextualTypeForJsxAttribute(attribute ast.Handle) *Type {
	return c.getContextualTypeForJsxAttribute(attribute, ContextFlagsNone)
}
func (c *Checker) GetConstantValue(node ast.Handle) any {
	if node.Kind() == ast.KindEnumMember {
		return c.getEnumMemberValue(node).Value
	}
	if c.symbolNodeLinks.Get(node).resolvedSymbol == nil {
		c.checkExpressionCached(node)
	}
	symbol := c.symbolNodeLinks.Get(node).resolvedSymbol
	if symbol == nil && ast.IsEntityNameExpression(node) {
		symbol = c.resolveEntityName(node, ast.SymbolFlagsValue, true, false, ast.Handle{})
	}
	if symbol != nil && symbol.Flags&ast.SymbolFlagsEnumMember != 0 {
		member := ast.NodeOf(symbol.ValueDeclaration)
		if ast.IsEnumConst(member.Parent()) {
			return c.getEnumMemberValue(member).Value
		}
	}
	return nil
}
func (c *Checker) getResolvedSignatureWorker(node ast.Handle, checkMode CheckMode, argumentCount int) (*Signature, []*Signature) {
	parsedNode := printer.NewEmitContext().ParseNode(node)
	c.apparentArgumentCount = &argumentCount
	candidatesOutArray := &[]*Signature{}
	var res *Signature
	if !parsedNode.IsNil() {
		res = c.getResolvedSignature(parsedNode, candidatesOutArray, checkMode)
	}
	c.apparentArgumentCount = nil
	return res, *candidatesOutArray
}
func (c *Checker) GetCandidateSignaturesForStringLiteralCompletions(call ast.Handle, editingArgument ast.Handle) []*Signature {
	candidates := runWithInferenceBlockedFromSourceNode(c, editingArgument, func() []*Signature {
		_, blockedInferenceCandidates := c.getResolvedSignatureWorker(call, CheckModeNormal, 0)
		return blockedInferenceCandidates
	})
	candidatesSet := collections.NewSetFromItems(candidates...)
	otherCandidates := runWithoutResolvedSignatureCaching(c, editingArgument, func() []*Signature {
		_, inferenceCandidates := c.getResolvedSignatureWorker(call, CheckModeNormal, 0)
		return inferenceCandidates
	})
	for _, candidate := range otherCandidates {
		if candidatesSet.Has(candidate) {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func (c *Checker) GetTypeAtPosition(s *Signature, pos int) *Type {
	return c.getTypeAtPosition(s, pos)
}
func (c *Checker) GetTypeParameterAtPosition(s *Signature, pos int) *Type {
	t := c.getTypeAtPosition(s, pos)
	if t.IsIndex() && isThisTypeParameter(t.AsIndexType().target) {
		constraint := c.getBaseConstraintOfType(t.AsIndexType().target)
		if constraint != nil {
			return c.getIndexType(constraint)
		}
	}
	return t
}

func (c *Checker) GetContextualTypeForArrayLiteralAtPosition(contextualArrayType *Type, arrayLiteral ast.Handle, position int) *Type {
	if contextualArrayType == nil {
		return nil
	}
	firstSpreadIndex, lastSpreadIndex := -1, -1
	elementIndex := 0
	elements := arrayLiteral.Elements()
	for i, elem := range elements {
		if elem.Pos() < position {
			elementIndex++
		}
		if ast.IsSpreadElement(elem) {
			if firstSpreadIndex == -1 {
				firstSpreadIndex = i
			}
			lastSpreadIndex = i
		}
	}
	return c.getContextualTypeForElementExpression(contextualArrayType, elementIndex, -1, firstSpreadIndex, lastSpreadIndex)
}

var knownGenericTypeNames = map[string]struct{}{"Array": {}, "ArrayLike": {}, "ReadonlyArray": {}, "Promise": {}, "PromiseLike": {}, "Iterable": {}, "IterableIterator": {}, "AsyncIterable": {}, "Set": {}, "WeakSet": {}, "ReadonlySet": {}, "Map": {}, "WeakMap": {}, "ReadonlyMap": {}, "Partial": {}, "Required": {}, "Readonly": {}, "Pick": {}, "Omit": {}, "NonNullable": {}}

func isKnownGenericTypeName(name string) bool {
	_, exists := knownGenericTypeNames[name]
	return exists
}
func (c *Checker) GetFirstTypeArgumentFromKnownType(t *Type) *Type {
	if t.objectFlags&ObjectFlagsReference != 0 && t.symbol != nil && isKnownGenericTypeName(t.symbol.Name) {
		symbol := c.getGlobalSymbol(t.symbol.Name, ast.SymbolFlagsType, nil)
		if symbol != nil && symbol == t.Target().symbol {
			return core.FirstOrNil(c.getTypeArguments(t))
		}
	}
	if t.alias != nil && isKnownGenericTypeName(t.alias.symbol.Name) {
		symbol := c.getGlobalSymbol(t.alias.symbol.Name, ast.SymbolFlagsType, nil)
		if symbol != nil && symbol == t.alias.symbol {
			return core.FirstOrNil(t.alias.typeArguments)
		}
	}
	return nil
}

func (c *Checker) GetPropertySymbolsFromContextualType(node ast.Handle, contextualType *Type, unionSymbolOk bool) []*ast.Symbol {
	name := ast.GetTextOfPropertyName(node.Name())
	if name == "" {
		return nil
	}
	if contextualType.flags&TypeFlagsUnion == 0 {
		if symbol := c.getPropertyOfType(contextualType, name); symbol != nil {
			return []*ast.Symbol{symbol}
		}
		return nil
	}
	filteredTypes := contextualType.Types()
	if ast.IsObjectLiteralExpression(node.Parent()) || ast.IsJsxAttributes(node.Parent()) {
		filteredTypes = core.Filter(filteredTypes, func(t *Type) bool {
			return !c.IsTypeInvalidDueToUnionDiscriminant(t, node.Parent())
		})
	}
	discriminatedPropertySymbols := core.MapNonNil(filteredTypes, func(t *Type) *ast.Symbol {
		return c.getPropertyOfType(t, name)
	})
	if unionSymbolOk && (len(discriminatedPropertySymbols) == 0 || len(discriminatedPropertySymbols) == len(contextualType.Types())) {
		if symbol := c.getPropertyOfType(contextualType, name); symbol != nil {
			return []*ast.Symbol{symbol}
		}
	}
	if len(filteredTypes) == 0 && len(discriminatedPropertySymbols) == 0 {
		return core.MapNonNil(contextualType.Types(), func(t *Type) *ast.Symbol {
			return c.getPropertyOfType(t, name)
		})
	}
	return core.Deduplicate(discriminatedPropertySymbols)
}

func (c *Checker) GetPropertySymbolOfDestructuringAssignment(location ast.Handle) *ast.Symbol {
	if ast.IsArrayLiteralOrObjectLiteralDestructuringPattern(location.Parent().Parent()) {
		if typeOfObjectLiteral := c.getTypeOfAssignmentPattern(location.Parent().Parent()); typeOfObjectLiteral != nil {
			return c.getPropertyOfType(typeOfObjectLiteral, location.Text())
		}
	}
	return nil
}

func (c *Checker) getTypeOfAssignmentPattern(expr ast.Handle) *Type {
	if ast.IsForOfStatement(expr.Parent()) {
		iteratedType := c.checkRightHandSideOfForOf(expr.Parent())
		return c.checkDestructuringAssignment(expr, core.OrElse(iteratedType, c.errorType), CheckModeNormal, false)
	}
	if ast.IsBinaryExpression(expr.Parent()) {
		iteratedType := c.getTypeOfExpression(expr.Parent().BinaryExpressionRight())
		return c.checkDestructuringAssignment(expr, core.OrElse(iteratedType, c.errorType), CheckModeNormal, false)
	}
	if ast.IsPropertyAssignment(expr.Parent()) {
		node := expr.Parent().Parent()
		typeOfParentObjectLiteral := core.OrElse(c.getTypeOfAssignmentPattern(node), c.errorType)
		propertyIndex := slices.Index(node.Properties(), expr.Parent())
		return c.checkObjectLiteralDestructuringPropertyAssignment(node, typeOfParentObjectLiteral, propertyIndex, 0, false)
	}
	node := expr.Parent()
	typeOfArrayLiteral := core.OrElse(c.getTypeOfAssignmentPattern(node), c.errorType)
	elementType := core.OrElse(c.checkIteratedTypeOrElementType(IterationUseDestructuring, typeOfArrayLiteral, c.undefinedType, expr.Parent()), c.errorType)
	return c.checkArrayLiteralDestructuringElementAssignment(node, typeOfArrayLiteral, slices.Index(node.Elements(), expr), elementType, CheckModeNormal)
}
func (c *Checker) GetSignatureFromDeclaration(node ast.Handle) *Signature {
	return c.getSignatureFromDeclaration(node)
}

func (c *Checker) IsLibSymbolForHoverVerbosity(symbol *ast.Symbol) bool {
	if symbol == nil {
		return false
	}
	for _, decl := range ast.DeclarationNodes(symbol) {
		sf := ast.GetSourceFileOfNode(decl)
		if sf != nil && c.program.IsSourceFileDefaultLibrary(sf.Path()) {
			return true
		}
	}
	return false
}

func (c *Checker) IsLibTypeForHoverVerbosity(t *Type) bool {
	var symbol *ast.Symbol
	if t.objectFlags&ObjectFlagsReference != 0 {
		symbol = t.Target().Symbol()
	} else {
		symbol = t.Symbol()
	}
	if c.IsLibSymbolForHoverVerbosity(symbol) {
		return true
	}
	return isTupleType(t)
}
