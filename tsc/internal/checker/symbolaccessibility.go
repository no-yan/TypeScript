package checker

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"slices"
)

func (c *Checker) IsTypeSymbolAccessible(typeSymbol *ast.Symbol, enclosingDeclaration ast.Handle) bool {
	access := c.isSymbolAccessibleWorker(typeSymbol, enclosingDeclaration, ast.SymbolFlagsType, false, true)
	return access.Accessibility == printer.SymbolAccessibilityAccessible
}
func (c *Checker) IsValueSymbolAccessible(symbol *ast.Symbol, enclosingDeclaration ast.Handle) bool {
	access := c.isSymbolAccessibleWorker(symbol, enclosingDeclaration, ast.SymbolFlagsValue, false, true)
	return access.Accessibility == printer.SymbolAccessibilityAccessible
}
func (c *Checker) IsSymbolAccessibleByFlags(symbol *ast.Symbol, enclosingDeclaration ast.Handle, flags ast.SymbolFlags) bool {
	access := c.isSymbolAccessibleWorker(symbol, enclosingDeclaration, flags, false, false)
	return access.Accessibility == printer.SymbolAccessibilityAccessible
}
func (c *Checker) IsAnySymbolAccessible(symbols []*ast.Symbol, enclosingDeclaration ast.Handle, initialSymbol *ast.Symbol, meaning ast.SymbolFlags, shouldComputeAliasesToMakeVisible bool, allowModules bool) *printer.SymbolAccessibilityResult {
	if len(symbols) == 0 {
		return nil
	}
	var hadAccessibleChain *ast.Symbol
	earlyModuleBail := false
	for _, symbol := range symbols {
		accessibleSymbolChain := c.getAccessibleSymbolChain(symbol, enclosingDeclaration, meaning, false)
		if len(accessibleSymbolChain) > 0 {
			hadAccessibleChain = symbol
			hasAccessibleDeclarations := c.GetEmitResolver().hasVisibleDeclarations(accessibleSymbolChain[0], shouldComputeAliasesToMakeVisible)
			if hasAccessibleDeclarations != nil {
				return hasAccessibleDeclarations
			}
		}
		if allowModules {
			if ast.SomeDeclaration(symbol, hasNonGlobalAugmentationExternalModuleSymbol) {
				if shouldComputeAliasesToMakeVisible {
					earlyModuleBail = true
					continue
				}
				return &printer.SymbolAccessibilityResult{Accessibility: printer.SymbolAccessibilityAccessible}
			}
		}
		containers := c.getContainersOfSymbol(symbol, enclosingDeclaration, meaning)
		nextMeaning := meaning
		if initialSymbol == symbol {
			nextMeaning = getQualifiedLeftMeaning(meaning)
		}
		parentResult := c.IsAnySymbolAccessible(containers, enclosingDeclaration, initialSymbol, nextMeaning, shouldComputeAliasesToMakeVisible, allowModules)
		if parentResult != nil {
			return parentResult
		}
	}
	if earlyModuleBail {
		return &printer.SymbolAccessibilityResult{Accessibility: printer.SymbolAccessibilityAccessible}
	}
	if hadAccessibleChain != nil {
		var moduleName string
		if hadAccessibleChain != initialSymbol {
			moduleName = c.symbolToStringEx(hadAccessibleChain, enclosingDeclaration, ast.SymbolFlagsNamespace, SymbolFormatFlagsAllowAnyNodeKind)
		}
		return &printer.SymbolAccessibilityResult{Accessibility: printer.SymbolAccessibilityNotAccessible, ErrorSymbolName: c.symbolToStringEx(initialSymbol, enclosingDeclaration, meaning, SymbolFormatFlagsAllowAnyNodeKind), ErrorModuleName: moduleName}
	}
	return nil
}
func hasNonGlobalAugmentationExternalModuleSymbol(declaration ast.Handle) bool {
	return ast.IsModuleWithStringLiteralName(declaration) || (declaration.Kind == ast.KindSourceFile && ast.IsExternalOrCommonJSModule(ast.GetSourceFileOfNode(declaration)))
}
func getQualifiedLeftMeaning(rightMeaning ast.SymbolFlags) ast.SymbolFlags {
	if rightMeaning == ast.SymbolFlagsValue {
		return ast.SymbolFlagsValue
	}
	return ast.SymbolFlagsNamespace
}
func (c *Checker) getWithAlternativeContainers(container *ast.Symbol, symbol *ast.Symbol, enclosingDeclaration ast.Handle, meaning ast.SymbolFlags) []*ast.Symbol {
	var additionalContainers []*ast.Symbol
	for _, d := range ast.DeclarationNodes(container).All() {
		if s := c.getFileSymbolIfFileSymbolExportEqualsContainer(d, container); s != nil {
			additionalContainers = append(additionalContainers, s)
		}
	}
	var reexportContainers []*ast.Symbol
	if !enclosingDeclaration.IsNil() {
		reexportContainers = c.getAlternativeContainingModules(symbol, enclosingDeclaration)
	}
	objectLiteralContainer := c.getVariableDeclarationOfObjectLiteral(container, meaning)
	leftMeaning := getQualifiedLeftMeaning(meaning)
	if !enclosingDeclaration.IsNil() && container.Flags&leftMeaning != 0 && len(c.getAccessibleSymbolChain(container, enclosingDeclaration, ast.SymbolFlagsNamespace, false)) > 0 {
		res := append(append([]*ast.Symbol{container}, additionalContainers...), reexportContainers...)
		if objectLiteralContainer != nil {
			res = append(res, objectLiteralContainer)
		}
		return res
	}
	var variableMatches []*ast.Symbol
	if (meaning == ast.SymbolFlagsValue && container.Flags&leftMeaning == 0) && container.Flags&ast.SymbolFlagsType != 0 && c.getDeclaredTypeOfSymbol(container).flags&TypeFlagsObject != 0 {
		c.someSymbolTableInScope(enclosingDeclaration, func(t ast.SymbolTable, _ symbolTableID, _ bool, _ bool, _ ast.Handle) bool {
			found := false
			for _, s := range t {
				if s.Flags&leftMeaning != 0 && c.getTypeOfSymbol(s) == c.getDeclaredTypeOfSymbol(container) {
					variableMatches = append(variableMatches, s)
					found = true
				}
			}
			return found
		})
		c.sortSymbols(variableMatches)
	}
	var res []*ast.Symbol
	res = append(res, variableMatches...)
	res = append(res, additionalContainers...)
	res = append(res, container)
	if objectLiteralContainer != nil {
		res = append(res, objectLiteralContainer)
	}
	res = append(res, reexportContainers...)
	return res
}
func (c *Checker) getAlternativeContainingModules(symbol *ast.Symbol, enclosingDeclaration ast.Handle) []*ast.Symbol {
	if enclosingDeclaration.IsNil() {
		return nil
	}
	containingFile := ast.GetSourceFileOfNode(enclosingDeclaration)
	id := ast.GetNodeId(containingFile.AsNode())
	links := c.symbolContainerLinks.Get(symbol)
	if links.extendedContainersByFile == nil {
		links.extendedContainersByFile = make(map[ast.NodeId][]*ast.Symbol)
	}
	existing, ok := links.extendedContainersByFile[id]
	if ok && existing != nil {
		return existing
	}
	var results []*ast.Symbol
	if len(containingFile.Imports()) > 0 {
		for _, importRef := range containingFile.Imports() {
			if ast.NodeIsSynthesized(importRef) {
				continue
			}
			resolvedModule := c.resolveExternalModuleName(enclosingDeclaration, importRef, true)
			if resolvedModule == nil {
				continue
			}
			ref := c.getAliasForSymbolInContainer(resolvedModule, symbol)
			if ref == nil {
				continue
			}
			results = append(results, resolvedModule)
		}
		if len(results) > 0 {
			links.extendedContainersByFile[id] = results
			return results
		}
	}
	if links.extendedContainers != nil {
		return *links.extendedContainers
	}
	otherFiles := c.program.SourceFiles()
	for _, file := range otherFiles {
		if !ast.IsExternalModule(file) {
			continue
		}
		sym := c.getSymbolOfDeclaration(file.ParseRoot())
		ref := c.getAliasForSymbolInContainer(sym, symbol)
		if ref == nil {
			continue
		}
		results = append(results, sym)
	}
	links.extendedContainers = &results
	return results
}
func (c *Checker) getVariableDeclarationOfObjectLiteral(symbol *ast.Symbol, meaning ast.SymbolFlags) *ast.Symbol {
	if meaning&ast.SymbolFlagsValue == 0 {
		return nil
	}
	if len(symbol.Declarations) == 0 {
		return nil
	}
	firstDecl := ast.DeclarationNodes(symbol).First()
	if firstDecl.Parent().IsNil() {
		return nil
	}
	if !ast.IsVariableDeclaration(firstDecl.Parent()) {
		return nil
	}
	if ast.IsObjectLiteralExpression(firstDecl) && firstDecl == firstDecl.Parent().Initializer() || ast.IsTypeLiteralNode(firstDecl) && firstDecl == firstDecl.Parent().Type() {
		return c.getSymbolOfDeclaration(firstDecl.Parent())
	}
	return nil
}
func hasExternalModuleSymbol(declaration ast.Handle) bool {
	return ast.IsAmbientModule(declaration) || (declaration.Kind == ast.KindSourceFile && ast.IsExternalOrCommonJSModule(ast.GetSourceFileOfNode(declaration)))
}
func (c *Checker) getExternalModuleContainer(declaration ast.Handle) *ast.Symbol {
	node := ast.FindAncestor(declaration, hasExternalModuleSymbol)
	if node.IsNil() {
		return nil
	}
	return c.getSymbolOfDeclaration(node)
}
func (c *Checker) getFileSymbolIfFileSymbolExportEqualsContainer(d ast.Handle, container *ast.Symbol) *ast.Symbol {
	fileSymbol := c.getExternalModuleContainer(d)
	if fileSymbol == nil || fileSymbol.Exports == nil {
		return nil
	}
	exported, ok := fileSymbol.Exports[ast.InternalSymbolNameExportEquals]
	if !ok || exported == nil {
		return nil
	}
	if c.getSymbolIfSameReference(exported, container) != nil {
		return fileSymbol
	}
	return nil
}

func (c *Checker) getContainersOfSymbol(symbol *ast.Symbol, enclosingDeclaration ast.Handle, meaning ast.SymbolFlags) []*ast.Symbol {
	container := c.getParentOfSymbol(symbol)
	if container != nil && (symbol.Flags&ast.SymbolFlagsTypeParameter == 0) {
		return c.getWithAlternativeContainers(container, symbol, enclosingDeclaration, meaning)
	}
	var candidates []*ast.Symbol
	for _, d := range ast.DeclarationNodes(symbol).All() {
		if !ast.IsAmbientModule(d) && !d.Parent().IsNil() {
			if hasNonGlobalAugmentationExternalModuleSymbol(d.Parent()) {
				sym := c.getSymbolOfDeclaration(d.Parent())
				if sym != nil && !slices.Contains(candidates, sym) {
					candidates = append(candidates, sym)
				}
				continue
			}
			if ast.IsModuleBlock(d.Parent()) && !d.Parent().Parent().IsNil() && c.resolveExternalModuleSymbol(c.getSymbolOfDeclaration(d.Parent().Parent()), false) == symbol {
				sym := c.getSymbolOfDeclaration(d.Parent().Parent())
				if sym != nil && !slices.Contains(candidates, sym) {
					candidates = append(candidates, sym)
				}
				continue
			}
		}
		if ast.IsClassExpression(d) && ast.IsBinaryExpression(d.Parent()) && d.Parent().BinaryExpressionOperatorToken().Kind == ast.KindEqualsToken && ast.IsAccessExpression(d.Parent().BinaryExpressionLeft()) && ast.IsEntityNameExpression(d.Parent().BinaryExpressionLeft().Expression()) {
			if ast.IsModuleExportsAccessExpression(d.Parent().BinaryExpressionLeft()) || ast.IsExportsIdentifier(d.Parent().BinaryExpressionLeft().Expression()) {
				sym := c.getSymbolOfDeclaration(ast.GetSourceFileOfNode(d).ParseRoot())
				if sym != nil && !slices.Contains(candidates, sym) {
					candidates = append(candidates, sym)
				}
				continue
			}
			c.checkExpressionCached(d.Parent().BinaryExpressionLeft().Expression())
			sym := c.symbolNodeLinks.Get(d.Parent().BinaryExpressionLeft().Expression()).resolvedSymbol
			if sym != nil && !slices.Contains(candidates, sym) {
				candidates = append(candidates, sym)
			}
			continue
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	var bestContainers []*ast.Symbol
	var alternativeContainers []*ast.Symbol
	for _, container := range candidates {
		if c.getAliasForSymbolInContainer(container, symbol) == nil {
			continue
		}
		allAlts := c.getWithAlternativeContainers(container, symbol, enclosingDeclaration, meaning)
		if len(allAlts) == 0 {
			continue
		}
		bestContainers = append(bestContainers, allAlts[0])
		alternativeContainers = append(alternativeContainers, allAlts[1:]...)
	}
	return append(bestContainers, alternativeContainers...)
}
func (c *Checker) getAliasForSymbolInContainer(container *ast.Symbol, symbol *ast.Symbol) *ast.Symbol {
	if container == nil || symbol == nil {
		return nil
	}
	if container == c.getParentOfSymbol(symbol) {
		return symbol
	}
	if container.Exports != nil {
		exportEquals, ok := container.Exports[ast.InternalSymbolNameExportEquals]
		if ok && exportEquals != nil && c.getSymbolIfSameReference(exportEquals, symbol) != nil {
			return container
		}
	}
	exports := c.getExportsOfSymbol(container)
	quick, ok := exports[symbol.Name]
	if ok && quick != nil && c.getSymbolIfSameReference(quick, symbol) != nil {
		return quick
	}
	var candidates []*ast.Symbol
	for _, exported := range exports {
		if c.getSymbolIfSameReference(exported, symbol) != nil {
			candidates = append(candidates, exported)
		}
	}
	if len(candidates) > 0 {
		c.sortSymbols(candidates)
		return candidates[0]
	}
	return nil
}
func (c *Checker) getAccessibleSymbolChain(symbol *ast.Symbol, enclosingDeclaration ast.Handle, meaning ast.SymbolFlags, useOnlyExternalAliasing bool) []*ast.Symbol {
	return c.getAccessibleSymbolChainEx(accessibleSymbolChainContext{symbol, enclosingDeclaration, meaning, useOnlyExternalAliasing, make(map[ast.SymbolId]map[symbolTableID]struct{})})
}
func (c *Checker) GetAccessibleSymbolChain(symbol *ast.Symbol, enclosingDeclaration ast.Handle, meaning ast.SymbolFlags, useOnlyExternalAliasing bool) []*ast.Symbol {
	return c.getAccessibleSymbolChain(symbol, enclosingDeclaration, meaning, useOnlyExternalAliasing)
}

type accessibleSymbolChainContext struct {
	symbol                  *ast.Symbol
	enclosingDeclaration    ast.Handle
	meaning                 ast.SymbolFlags
	useOnlyExternalAliasing bool
	visitedSymbolTablesMap  map[ast.SymbolId]map[symbolTableID]struct{}
}

type symbolTableID uint64

const stKindShift = 61
const (
	stKindLocals symbolTableID = iota << stKindShift
	stKindExports
	stKindMembers
	stKindGlobals
	stKindResolvedExports
	stKindMask symbolTableID = (iota - 1) << stKindShift
)

func symbolTableIDFromLocals(node ast.Handle) symbolTableID {
	return stKindLocals | symbolTableID(node.NodeId())
}
func symbolTableIDFromExports(sym *ast.Symbol) symbolTableID {
	return stKindExports | symbolTableID(ast.GetSymbolId(sym))
}

func symbolTableIDFromResolvedExports(sym *ast.Symbol) symbolTableID {
	return stKindResolvedExports | symbolTableID(ast.GetSymbolId(sym))
}
func symbolTableIDFromMembers(sym *ast.Symbol) symbolTableID {
	return stKindMembers | symbolTableID(ast.GetSymbolId(sym))
}
func symbolTableIDFromGlobals() symbolTableID {
	return stKindGlobals
}
func (c *Checker) getAccessibleSymbolChainEx(ctx accessibleSymbolChainContext) []*ast.Symbol {
	if ctx.symbol == nil {
		return nil
	}
	if isPropertyOrMethodDeclarationSymbol(ctx.symbol) {
		return nil
	}
	var firstRelevantLocation ast.Handle
	c.someSymbolTableInScope(ctx.enclosingDeclaration, func(_ ast.SymbolTable, _ symbolTableID, _ bool, _ bool, node ast.Handle) bool {
		firstRelevantLocation = node
		return true
	})
	links := c.symbolContainerLinks.Get(ctx.symbol)
	linkKey := accessibleChainCacheKey{ctx.useOnlyExternalAliasing, firstRelevantLocation, ctx.meaning}
	if links.accessibleChainCache == nil {
		links.accessibleChainCache = make(map[accessibleChainCacheKey][]*ast.Symbol)
	}
	existing, ok := links.accessibleChainCache[linkKey]
	if ok {
		return existing
	}
	var result []*ast.Symbol
	c.someSymbolTableInScope(ctx.enclosingDeclaration, func(t ast.SymbolTable, tableId symbolTableID, ignoreQualification bool, isLocalNameLookup bool, _ ast.Handle) bool {
		res := c.getAccessibleSymbolChainFromSymbolTable(ctx, t, tableId, ignoreQualification, isLocalNameLookup)
		if len(res) > 0 {
			result = res
			return true
		}
		return false
	})
	links.accessibleChainCache[linkKey] = result
	return result
}

func (c *Checker) getAccessibleSymbolChainFromSymbolTable(ctx accessibleSymbolChainContext, t ast.SymbolTable, tableId symbolTableID, ignoreQualification bool, isLocalNameLookup bool) []*ast.Symbol {
	symId := ast.GetSymbolId(ctx.symbol)
	visitedSymbolTables, ok := ctx.visitedSymbolTablesMap[symId]
	if !ok {
		visitedSymbolTables = make(map[symbolTableID]struct{})
		ctx.visitedSymbolTablesMap[symId] = visitedSymbolTables
	}
	_, present := visitedSymbolTables[tableId]
	if present {
		return nil
	}
	visitedSymbolTables[tableId] = struct{}{}
	res := c.trySymbolTable(ctx, t, tableId, ignoreQualification, isLocalNameLookup)
	delete(visitedSymbolTables, tableId)
	return res
}

func (c *Checker) getSymbolTableAliases(symbols ast.SymbolTable, tableId symbolTableID) []*ast.Symbol {
	kind := tableId & stKindMask
	if kind == stKindMembers {
		return nil
	}
	if kind == stKindGlobals || kind == stKindExports || kind == stKindResolvedExports {
		if c.symbolTableAliasCache != nil {
			if aliases, ok := c.symbolTableAliasCache[tableId]; ok {
				return aliases
			}
		}
	}
	var aliases []*ast.Symbol
	for _, sym := range symbols {
		if sym.Flags&ast.SymbolFlagsAlias != 0 {
			aliases = append(aliases, sym)
		}
	}
	if kind == stKindGlobals || kind == stKindExports || kind == stKindResolvedExports {
		if c.symbolTableAliasCache == nil {
			c.symbolTableAliasCache = make(map[symbolTableID][]*ast.Symbol)
		}
		c.symbolTableAliasCache[tableId] = aliases
	}
	return aliases
}
func (c *Checker) trySymbolTable(ctx accessibleSymbolChainContext, symbols ast.SymbolTable, tableId symbolTableID, ignoreQualification bool, isLocalNameLookup bool) []*ast.Symbol {
	isGlobals := tableId == stKindGlobals
	res, ok := symbols[ctx.symbol.Name]
	if ok && res != nil && c.isAccessible(ctx, res, nil, ignoreQualification) {
		return []*ast.Symbol{ctx.symbol}
	}
	var candidateChains [][]*ast.Symbol
	if ok && res != nil && res.ExportSymbol != nil {
		if c.isAccessible(ctx, c.getMergedSymbol(res.ExportSymbol), nil, ignoreQualification) {
			candidateChains = append(candidateChains, []*ast.Symbol{ctx.symbol})
		}
	}
	for _, symbolFromSymbolTable := range c.getSymbolTableAliases(symbols, tableId) {
		if symbolFromSymbolTable.Name != ast.InternalSymbolNameExportEquals && symbolFromSymbolTable.Name != ast.InternalSymbolNameDefault && !(isUMDExportSymbol(symbolFromSymbolTable) && !ctx.enclosingDeclaration.IsNil() && ast.IsExternalModule(ast.GetSourceFileOfNode(ctx.enclosingDeclaration))) && (!ctx.useOnlyExternalAliasing || ast.SomeDeclaration(symbolFromSymbolTable, ast.IsExternalModuleImportEqualsDeclaration)) && (isLocalNameLookup && !ast.SomeDeclaration(symbolFromSymbolTable, isNamespaceReexportDeclaration) || !isLocalNameLookup) && (ignoreQualification || len(getDeclarationsOfKind(symbolFromSymbolTable, ast.KindExportSpecifier)) == 0) {
			resolvedImportedSymbol := c.resolveAlias(symbolFromSymbolTable)
			candidate := c.getCandidateListForSymbol(ctx, symbolFromSymbolTable, resolvedImportedSymbol, ignoreQualification)
			if len(candidate) > 0 {
				candidateChains = append(candidateChains, candidate)
			}
		}
	}
	if len(candidateChains) > 0 {
		slices.SortStableFunc(candidateChains, c.compareSymbolChains)
		return candidateChains[0]
	}
	if isGlobals {
		return c.getCandidateListForSymbol(ctx, c.globalThisSymbol, c.globalThisSymbol, ignoreQualification)
	}
	return nil
}
func (c *Checker) compareSymbolChainsWorker(a []*ast.Symbol, b []*ast.Symbol) int {
	chainLen := len(a) - len(b)
	if chainLen != 0 {
		return chainLen
	}
	idx := 0
	for idx < len(a) {
		comparison := c.compareSymbols(a[idx], b[idx])
		if comparison != 0 {
			return comparison
		}
		idx++
	}
	return 0
}
func isUMDExportSymbol(symbol *ast.Symbol) bool {
	return symbol != nil && len(symbol.Declarations) > 0 && !ast.NodeOf(symbol.Declarations[0]).IsNil() && ast.IsNamespaceExportDeclaration(ast.NodeOf(symbol.Declarations[0]))
}
func isNamespaceReexportDeclaration(node ast.Handle) bool {
	return ast.IsNamespaceExport(node) && !node.Parent().ModuleSpecifier().IsNil()
}
func (c *Checker) getCandidateListForSymbol(ctx accessibleSymbolChainContext, symbolFromSymbolTable *ast.Symbol, resolvedImportedSymbol *ast.Symbol, ignoreQualification bool) []*ast.Symbol {
	if c.isAccessible(ctx, symbolFromSymbolTable, resolvedImportedSymbol, ignoreQualification) {
		return []*ast.Symbol{symbolFromSymbolTable}
	}
	candidateTable := c.getExportsOfSymbol(resolvedImportedSymbol)
	if candidateTable == nil {
		return nil
	}
	candidateTableId := symbolTableIDFromResolvedExports(resolvedImportedSymbol)
	accessibleSymbolsFromExports := c.getAccessibleSymbolChainFromSymbolTable(ctx, candidateTable, candidateTableId, true, false)
	if len(accessibleSymbolsFromExports) == 0 {
		return nil
	}
	if !c.canQualifySymbol(ctx, symbolFromSymbolTable, getQualifiedLeftMeaning(ctx.meaning)) {
		return nil
	}
	return append([]*ast.Symbol{symbolFromSymbolTable}, accessibleSymbolsFromExports...)
}
func (c *Checker) isAccessible(ctx accessibleSymbolChainContext, symbolFromSymbolTable *ast.Symbol, resolvedAliasSymbol *ast.Symbol, ignoreQualification bool) bool {
	likeSymbols := false
	if ctx.symbol == resolvedAliasSymbol {
		likeSymbols = true
	}
	if ctx.symbol == symbolFromSymbolTable {
		likeSymbols = true
	}
	symbol := c.getMergedSymbol(ctx.symbol)
	if symbol == c.getMergedSymbol(resolvedAliasSymbol) {
		likeSymbols = true
	}
	if symbol == c.getMergedSymbol(symbolFromSymbolTable) {
		likeSymbols = true
	}
	if !likeSymbols {
		return false
	}
	return !ast.SomeDeclaration(symbolFromSymbolTable, hasNonGlobalAugmentationExternalModuleSymbol) && (ignoreQualification || c.canQualifySymbol(ctx, c.getMergedSymbol(symbolFromSymbolTable), ctx.meaning))
}
func (c *Checker) canQualifySymbol(ctx accessibleSymbolChainContext, symbolFromSymbolTable *ast.Symbol, meaning ast.SymbolFlags) bool {
	return !c.needsQualification(symbolFromSymbolTable, ctx.enclosingDeclaration, meaning) || len(c.getAccessibleSymbolChainEx(accessibleSymbolChainContext{symbolFromSymbolTable.Parent, ctx.enclosingDeclaration, getQualifiedLeftMeaning(meaning), ctx.useOnlyExternalAliasing, ctx.visitedSymbolTablesMap})) > 0
}
func (c *Checker) needsQualification(symbol *ast.Symbol, enclosingDeclaration ast.Handle, meaning ast.SymbolFlags) bool {
	qualify := false
	c.someSymbolTableInScope(enclosingDeclaration, func(symbolTable ast.SymbolTable, _ symbolTableID, _ bool, _ bool, _ ast.Handle) bool {
		res, ok := symbolTable[symbol.Name]
		if !ok || res == nil {
			return false
		}
		symbolFromSymbolTable := c.getMergedSymbol(res)
		if symbolFromSymbolTable == nil {
			return false
		}
		if symbolFromSymbolTable == symbol {
			return true
		}
		shouldResolveAlias := symbolFromSymbolTable.Flags&ast.SymbolFlagsAlias != 0 && ast.GetDeclarationOfKind(symbolFromSymbolTable, ast.KindExportSpecifier).IsNil()
		if shouldResolveAlias {
			symbolFromSymbolTable = c.resolveAlias(symbolFromSymbolTable)
		}
		flags := symbolFromSymbolTable.Flags
		if shouldResolveAlias {
			flags = c.getSymbolFlags(symbolFromSymbolTable)
		}
		if flags&meaning != 0 {
			qualify = true
			return true
		}
		return false
	})
	return qualify
}
func isPropertyOrMethodDeclarationSymbol(symbol *ast.Symbol) bool {
	if len(symbol.Declarations) > 0 {
		for _, declaration := range ast.DeclarationNodes(symbol).All() {
			switch declaration.Kind {
			case ast.KindPropertyDeclaration, ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
				continue
			default:
				return false
			}
		}
		return true
	}
	return false
}
func (c *Checker) someSymbolTableInScope(enclosingDeclaration ast.Handle, callback func(symbolTable ast.SymbolTable, tableId symbolTableID, ignoreQualification bool, isLocalNameLookup bool, scopeNode ast.Handle) bool) bool {
	for location := enclosingDeclaration; !location.IsNil(); location = location.Parent() {
		if canHaveLocals(location) && location.Locals() != nil && !ast.IsGlobalSourceFile(location) {
			if callback(location.Locals(), symbolTableIDFromLocals(location), false, true, location) {
				return true
			}
		}
		switch location.Kind {
		case ast.KindSourceFile, ast.KindModuleDeclaration:
			if ast.IsSourceFile(location) && !ast.IsExternalOrCommonJSModule(ast.GetSourceFileOfNode(location)) {
				break
			}
			sym := c.getSymbolOfDeclaration(ast.GetReparsedHandle(location))
			if callback(sym.Exports, symbolTableIDFromExports(sym), false, true, location) {
				return true
			}
		case ast.KindClassDeclaration, ast.KindClassExpression, ast.KindInterfaceDeclaration:
			var table ast.SymbolTable
			sym := c.getSymbolOfDeclaration(location)
			for key, memberSymbol := range sym.Members {
				if memberSymbol.Flags&(ast.SymbolFlagsType & ^ast.SymbolFlagsAssignment) != 0 {
					if table == nil {
						table = make(ast.SymbolTable)
					}
					table[key] = memberSymbol
				}
			}
			if table != nil && callback(table, symbolTableIDFromMembers(sym), false, false, location) {
				return true
			}
			if ast.IsClassExpression(location) && !location.ClassExpressionName().IsNil() {
				nameTable := c.getClassExpressionNameTable(location)
				if nameTable != nil && callback(nameTable, symbolTableIDFromLocals(location), false, true, location) {
					return true
				}
			}
		}
	}
	return callback(c.globals, symbolTableIDFromGlobals(), false, true, ast.Handle{})
}

func (c *Checker) getClassExpressionNameTable(location ast.Handle) ast.SymbolTable {
	nodeId := location.NodeId()
	if c.classExpressionNameTables != nil {
		if table, ok := c.classExpressionNameTables[nodeId]; ok {
			return table
		}
	}
	classSymbol := c.getSymbolOfDeclaration(location)
	nameText := location.ClassExpressionName().Text()
	if len(nameText) == 0 || classSymbol == nil {
		return nil
	}
	table := ast.SymbolTable{nameText: classSymbol}
	if c.classExpressionNameTables == nil {
		c.classExpressionNameTables = make(map[ast.NodeId]ast.SymbolTable)
	}
	c.classExpressionNameTables[nodeId] = table
	return table
}
func (c *Checker) IsSymbolAccessible(symbol *ast.Symbol, enclosingDeclaration ast.Handle, meaning ast.SymbolFlags, shouldComputeAliasesToMakeVisible bool) printer.SymbolAccessibilityResult {
	return c.isSymbolAccessibleWorker(symbol, enclosingDeclaration, meaning, shouldComputeAliasesToMakeVisible, true)
}
func (c *Checker) isSymbolAccessibleWorker(symbol *ast.Symbol, enclosingDeclaration ast.Handle, meaning ast.SymbolFlags, shouldComputeAliasesToMakeVisible bool, allowModules bool) printer.SymbolAccessibilityResult {
	if symbol != nil && !enclosingDeclaration.IsNil() {
		result := c.IsAnySymbolAccessible([]*ast.Symbol{symbol}, enclosingDeclaration, symbol, meaning, shouldComputeAliasesToMakeVisible, allowModules)
		if result != nil {
			return *result
		}
		var symbolExternalModule *ast.Symbol
		for _, d := range ast.DeclarationNodes(symbol).All() {
			if s := c.getExternalModuleContainer(d); s != nil {
				symbolExternalModule = s
				break
			}
		}
		if symbolExternalModule != nil {
			enclosingExternalModule := c.getExternalModuleContainer(enclosingDeclaration)
			if symbolExternalModule != enclosingExternalModule {
				return printer.SymbolAccessibilityResult{Accessibility: printer.SymbolAccessibilityCannotBeNamed, ErrorSymbolName: c.symbolToStringEx(symbol, enclosingDeclaration, meaning, SymbolFormatFlagsAllowAnyNodeKind), ErrorModuleName: c.symbolToString(symbolExternalModule), ErrorNode: core.IfElse(ast.IsInJSFile(enclosingDeclaration), enclosingDeclaration, ast.Handle{})}
			}
		}
		return printer.SymbolAccessibilityResult{Accessibility: printer.SymbolAccessibilityNotAccessible, ErrorSymbolName: c.symbolToStringEx(symbol, enclosingDeclaration, meaning, SymbolFormatFlagsAllowAnyNodeKind)}
	}
	return printer.SymbolAccessibilityResult{Accessibility: printer.SymbolAccessibilityAccessible}
}
