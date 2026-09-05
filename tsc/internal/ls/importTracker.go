package ls

import (
	"context"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
)

type ImpExpKind int32

const (
	ImpExpKindUnknown ImpExpKind = iota
	ImpExpKindImport
	ImpExpKindExport
)

type ImportExportSymbol struct {
	kind       ImpExpKind
	symbol     *ast.Symbol
	exportInfo *ExportInfo
}
type ExportKind int

const (
	ExportKindNamed        ExportKind = 0
	ExportKindDefault      ExportKind = 1
	ExportKindExportEquals ExportKind = 2
	ExportKindUMD          ExportKind = 3
	ExportKindModule       ExportKind = 4
)

type ExportInfo struct {
	exportingModuleSymbol *ast.Symbol
	exportKind            ExportKind
}
type LocationAndSymbol struct {
	importLocation ast.Handle
	importSymbol   *ast.Symbol
}
type ImportsResult struct {
	importSearches   []LocationAndSymbol
	singleReferences []ast.Handle
	indirectUsers    []*ast.SourceFile
}
type ImportTracker func(exportSymbol *ast.Symbol, exportInfo *ExportInfo, isForRename bool) *ImportsResult
type ModuleReferenceKind int32

const (
	ModuleReferenceKindImport ModuleReferenceKind = iota
	ModuleReferenceKindReference
	ModuleReferenceKindImplicit
)

type ModuleReference struct {
	kind            ModuleReferenceKind
	literal         ast.Handle
	referencingFile *ast.SourceFile
	ref             *ast.FileReference
}

func createImportTracker(ctx context.Context, program *compiler.Program, sourceFiles []*ast.SourceFile, sourceFilesSet *collections.Set[string], checker *checker.Checker) ImportTracker {
	allDirectImports := getDirectImportsMap(ctx, program, sourceFiles, checker)
	return func(exportSymbol *ast.Symbol, exportInfo *ExportInfo, isForRename bool) *ImportsResult {
		directImports, indirectUsers := getImportersForExport(sourceFiles, sourceFilesSet, allDirectImports, exportInfo, checker)
		importSearches, singleReferences := getSearchesFromDirectImports(directImports, exportSymbol, exportInfo.exportKind, checker, isForRename)
		return &ImportsResult{importSearches, singleReferences, indirectUsers}
	}
}

func getDirectImportsMap(ctx context.Context, program *compiler.Program, sourceFiles []*ast.SourceFile, checker *checker.Checker) map[*ast.Symbol][]ast.Handle {
	result := make(map[*ast.Symbol][]ast.Handle)
	for _, sourceFile := range sourceFiles {
		if ctx.Err() != nil {
			return result
		}
		forEachImport(program, sourceFile, func(importDecl ast.Handle, moduleSpecifier ast.Handle) {
			if moduleSymbol := checker.GetSymbolAtLocation(moduleSpecifier); moduleSymbol != nil {
				result[moduleSymbol] = append(result[moduleSymbol], importDecl)
			}
		})
	}
	return result
}

func forEachImport(program *compiler.Program, sourceFile *ast.SourceFile, action func(importStatement ast.Handle, imported ast.Handle)) {
	var implicitImports []ast.Handle
	_, jsxSpecifier := program.GetJSXRuntimeImportSpecifier(sourceFile.Path())
	if !jsxSpecifier.IsNil() {
		implicitImports = append(implicitImports, jsxSpecifier)
	}
	importHelpersSpecifier := program.GetImportHelpersImportSpecifier(sourceFile.Path())
	if !importHelpersSpecifier.IsNil() {
		implicitImports = append(implicitImports, importHelpersSpecifier)
	}
	if !sourceFile.ExternalModuleIndicator.IsNil() || len(sourceFile.Imports())+len(implicitImports) != 0 {
		for _, i := range sourceFile.Imports() {
			action(ast.ImportFromModuleSpecifier(i), i)
		}
		for _, i := range implicitImports {
			action(ast.ImportFromModuleSpecifier(i), i)
		}
	} else {
		forEachPossibleImportOrExportStatement(sourceFile.ParseRoot(), func(node ast.Handle) bool {
			switch node.Kind {
			case ast.KindExportDeclaration, ast.KindImportDeclaration, ast.KindJSImportDeclaration:
				if specifier := node.ModuleSpecifier(); !specifier.IsNil() && ast.IsStringLiteral(specifier) {
					action(node, specifier)
				}
			case ast.KindImportEqualsDeclaration:
				if isExternalModuleImportEquals(node) {
					action(node, node.ImportEqualsDeclarationModuleReference().Expression())
				}
			}
			return false
		})
	}
}
func forEachPossibleImportOrExportStatement(sourceFileLike ast.Handle, action func(statement ast.Handle) bool) bool {
	for _, statement := range getStatementsOfSourceFileLike(sourceFileLike) {
		if action(statement) || isAmbientModuleDeclaration(statement) && forEachPossibleImportOrExportStatement(statement, action) {
			return true
		}
	}
	return false
}
func getSourceFileLikeForImportDeclaration(node ast.Handle) ast.Handle {
	if ast.IsCallExpression(node) || ast.IsJSDocImportTag(node) {
		return ast.GetSourceFileOfNode(node).ParseRoot()
	}
	parent := node.Parent()
	if ast.IsSourceFile(parent) {
		return parent
	}
	debug.Assert(ast.IsModuleBlock(parent) && isAmbientModuleDeclaration(parent.Parent()))
	return parent.Parent()
}
func isAmbientModuleDeclaration(node ast.Handle) bool {
	return ast.IsModuleDeclaration(node) && ast.IsStringLiteral(node.Name())
}
func getStatementsOfSourceFileLike(node ast.Handle) []ast.Handle {
	if ast.IsSourceFile(node) {
		return node.Statements()
	}
	if body := node.Body(); !body.IsNil() {
		return body.Statements()
	}
	return nil
}
func getImportersForExport(sourceFiles []*ast.SourceFile, sourceFilesSet *collections.Set[string], allDirectImports map[*ast.Symbol][]ast.Handle, exportInfo *ExportInfo, checker *checker.Checker) ([]ast.Handle, []*ast.SourceFile) {
	var directImports []ast.Handle
	var indirectUserDeclarations []ast.Handle
	markSeenDirectImport := nodeSeenTracker()
	markSeenIndirectUser := nodeSeenTracker()
	isAvailableThroughGlobal := isSourceFileWithGlobalExports(ast.NodeOf(exportInfo.exportingModuleSymbol.ValueDeclaration))
	getDirectImports := func(moduleSymbol *ast.Symbol) []ast.Handle {
		return allDirectImports[moduleSymbol]
	}
	var addIndirectUser func(ast.Handle, bool)
	addIndirectUser = func(sourceFileLike ast.Handle, addTransitiveDependencies bool) {
		if isAvailableThroughGlobal {
			return
		}
		if !markSeenIndirectUser(sourceFileLike) {
			return
		}
		indirectUserDeclarations = append(indirectUserDeclarations, sourceFileLike)
		if !addTransitiveDependencies {
			return
		}
		moduleSymbol := checker.GetMergedSymbol(sourceFileLike.Symbol())
		if moduleSymbol == nil {
			return
		}
		debug.Assert(moduleSymbol.Flags&ast.SymbolFlagsModule != 0)
		for _, directImport := range getDirectImports(moduleSymbol) {
			if !ast.IsImportTypeNode(directImport) {
				addIndirectUser(getSourceFileLikeForImportDeclaration(directImport), true)
			}
		}
	}
	isExported := func(node ast.Handle, stopAtAmbientModule bool) bool {
		for !node.IsNil() && !(stopAtAmbientModule && isAmbientModuleDeclaration(node)) {
			if ast.HasSyntacticModifier(node, ast.ModifierFlagsExport) {
				return true
			}
			node = node.Parent()
		}
		return false
	}
	handleImportCall := func(importCall ast.Handle) {
		top := ast.FindAncestor(importCall, isAmbientModuleDeclaration)
		if top.IsNil() {
			top = ast.GetSourceFileOfNode(importCall).ParseRoot()
		}
		addIndirectUser(top, isExported(importCall, true))
	}
	handleNamespaceImport := func(importDeclaration ast.Handle, name ast.Handle, isReExport bool, alreadyAddedDirect bool) {
		if exportInfo.exportKind == ExportKindExportEquals {
			if !alreadyAddedDirect {
				directImports = append(directImports, importDeclaration)
			}
		} else if !isAvailableThroughGlobal {
			sourceFileLike := getSourceFileLikeForImportDeclaration(importDeclaration)
			debug.Assert(ast.IsSourceFile(sourceFileLike) || ast.IsModuleDeclaration(sourceFileLike))
			addIndirectUser(sourceFileLike, isReExport || findNamespaceReExports(sourceFileLike, name, checker))
		}
	}
	var handleDirectImports func(*ast.Symbol)
	handleDirectImports = func(exportingModuleSymbol *ast.Symbol) {
		theseDirectImports := getDirectImports(exportingModuleSymbol)
		for _, direct := range theseDirectImports {
			if !markSeenDirectImport(direct) {
				continue
			}
			switch direct.Kind {
			case ast.KindCallExpression:
				if ast.IsImportCall(direct) {
					handleImportCall(direct)
				} else if !isAvailableThroughGlobal {
					parent := direct.Parent()
					if exportInfo.exportKind == ExportKindExportEquals && ast.IsVariableDeclaration(parent) {
						name := parent.Name()
						if ast.IsIdentifier(name) {
							directImports = append(directImports, name)
						}
					}
				}
			case ast.KindIdentifier:
			case ast.KindImportEqualsDeclaration:
				handleNamespaceImport(direct, direct.Name(), ast.HasSyntacticModifier(direct, ast.ModifierFlagsExport), false)
			case ast.KindImportDeclaration, ast.KindJSImportDeclaration, ast.KindJSDocImportTag:
				directImports = append(directImports, direct)
				if importClause := direct.ImportClause(); !importClause.IsNil() {
					if namedBindings := importClause.ImportClauseNamedBindings(); !namedBindings.IsNil() && ast.IsNamespaceImport(namedBindings) {
						handleNamespaceImport(direct, namedBindings.Name(), false, true)
						break
					}
				}
				if !isAvailableThroughGlobal && ast.IsDefaultImport(direct) {
					addIndirectUser(getSourceFileLikeForImportDeclaration(direct), false)
				}
			case ast.KindExportDeclaration:
				exportClause := direct.ExportDeclarationExportClause()
				if exportClause.IsNil() {
					handleDirectImports(getContainingModuleSymbol(direct, checker))
				} else if ast.IsNamespaceExport(exportClause) {
					addIndirectUser(getSourceFileLikeForImportDeclaration(direct), true)
				} else {
					directImports = append(directImports, direct)
				}
			case ast.KindImportType:
				if !isAvailableThroughGlobal && direct.ImportTypeNodeIsTypeOf() && direct.ImportTypeNodeQualifier().IsNil() && isExported(direct, false) {
					addIndirectUser(ast.GetSourceFileOfNode(direct).ParseRoot(), true)
				}
				directImports = append(directImports, direct)
			default:
				debug.FailBadSyntaxKind(direct, "Unexpected import kind.")
			}
		}
	}
	getIndirectUsers := func() []*ast.SourceFile {
		if isAvailableThroughGlobal {
			return sourceFiles
		}
		for _, decl := range ast.DeclarationNodes(exportInfo.exportingModuleSymbol).All() {
			if ast.IsExternalModuleAugmentation(decl) && sourceFilesSet.Has(ast.GetSourceFileOfNode(decl).FileName()) {
				addIndirectUser(decl, false)
			}
		}
		return core.Map(indirectUserDeclarations, ast.GetSourceFileOfNode)
	}
	handleDirectImports(exportInfo.exportingModuleSymbol)
	return directImports, getIndirectUsers()
}
func getContainingModuleSymbol(importer ast.Handle, checker *checker.Checker) *ast.Symbol {
	return checker.GetMergedSymbol(getSourceFileLikeForImportDeclaration(importer).Symbol())
}

func findNamespaceReExports(sourceFileLike ast.Handle, name ast.Handle, checker *checker.Checker) bool {
	namespaceImportSymbol := checker.GetSymbolAtLocation(name)
	return forEachPossibleImportOrExportStatement(sourceFileLike, func(statement ast.Handle) bool {
		if !ast.IsExportDeclaration(statement) {
			return false
		}
		exportClause := statement.ExportDeclarationExportClause()
		moduleSpecifier := statement.ModuleSpecifier()
		return moduleSpecifier.IsNil() && !exportClause.IsNil() && ast.IsNamedExports(exportClause) && core.Some(exportClause.Elements(), func(element ast.Handle) bool {
			return checker.GetExportSpecifierLocalTargetSymbol(element) == namespaceImportSymbol
		})
	})
}
func getSearchesFromDirectImports(directImports []ast.Handle, exportSymbol *ast.Symbol, exportKind ExportKind, checker *checker.Checker, isForRename bool) ([]LocationAndSymbol, []ast.Handle) {
	var importSearches []LocationAndSymbol
	var singleReferences []ast.Handle
	addSearch := func(location ast.Handle, symbol *ast.Symbol) {
		importSearches = append(importSearches, LocationAndSymbol{location, symbol})
	}
	isNameMatch := func(name string) bool {
		return name == exportSymbol.Name || exportKind != ExportKindNamed && name == ast.InternalSymbolNameDefault
	}
	handleNamespaceImportLike := func(importName ast.Handle) {
		if exportKind == ExportKindExportEquals && (!isForRename || isNameMatch(importName.Text())) {
			addSearch(importName, checker.GetSymbolAtLocation(importName))
		}
	}
	searchForNamedImport := func(namedBindings ast.Handle) {
		if namedBindings.IsNil() {
			return
		}
		for _, element := range namedBindings.Elements() {
			name := element.Name()
			propertyName := element.PropertyName()
			if !isNameMatch(core.OrElse(propertyName, name).Text()) {
				continue
			}
			if !propertyName.IsNil() {
				singleReferences = append(singleReferences, propertyName)
				if !isForRename || name.Text() == exportSymbol.Name {
					addSearch(name, checker.GetSymbolAtLocation(name))
				}
			} else {
				var localSymbol *ast.Symbol
				if ast.IsExportSpecifier(element) && !element.PropertyName().IsNil() {
					localSymbol = checker.GetExportSpecifierLocalTargetSymbol(element)
				} else {
					localSymbol = checker.GetSymbolAtLocation(name)
				}
				addSearch(name, localSymbol)
			}
		}
	}
	handleImport := func(decl ast.Handle) {
		if ast.IsImportEqualsDeclaration(decl) {
			if isExternalModuleImportEquals(decl) {
				handleNamespaceImportLike(decl.Name())
			}
			return
		}
		if ast.IsIdentifier(decl) {
			handleNamespaceImportLike(decl)
			return
		}
		if ast.IsImportTypeNode(decl) {
			if qualifier := decl.ImportTypeNodeQualifier(); !qualifier.IsNil() {
				firstIdentifier := ast.GetFirstIdentifier(qualifier)
				if firstIdentifier.Text() == ast.SymbolName(exportSymbol) {
					singleReferences = append(singleReferences, firstIdentifier)
				}
			} else if exportKind == ExportKindExportEquals {
				singleReferences = append(singleReferences, decl.ImportTypeNodeArgument().LiteralTypeNodeLiteral())
			}
			return
		}
		if !ast.IsStringLiteral(decl.ModuleSpecifier()) {
			return
		}
		if ast.IsExportDeclaration(decl) {
			if exportClause := decl.ExportDeclarationExportClause(); !exportClause.IsNil() && ast.IsNamedExports(exportClause) {
				searchForNamedImport(exportClause)
			}
			return
		}
		if importClause := decl.ImportClause(); !importClause.IsNil() {
			if namedBindings := importClause.ImportClauseNamedBindings(); !namedBindings.IsNil() {
				switch namedBindings.Kind {
				case ast.KindNamespaceImport:
					handleNamespaceImportLike(namedBindings.Name())
				case ast.KindNamedImports:
					if exportKind == ExportKindNamed || exportKind == ExportKindDefault {
						searchForNamedImport(namedBindings)
					}
				}
			}
			if name := importClause.Name(); !name.IsNil() && (exportKind == ExportKindDefault || exportKind == ExportKindExportEquals) && (!isForRename || name.Text() == symbolNameNoDefault(exportSymbol)) {
				defaultImportAlias := checker.GetSymbolAtLocation(name)
				addSearch(name, defaultImportAlias)
			}
		}
	}
	for _, decl := range directImports {
		handleImport(decl)
	}
	return importSearches, singleReferences
}
func getImportOrExportSymbol(node ast.Handle, symbol *ast.Symbol, checker *checker.Checker, comingFromExport bool) *ImportExportSymbol {
	exportInfo := func(symbol *ast.Symbol, kind ExportKind) *ImportExportSymbol {
		if exportInfo := getExportInfo(symbol, kind, checker); exportInfo != nil {
			return &ImportExportSymbol{kind: ImpExpKindExport, symbol: symbol, exportInfo: exportInfo}
		}
		return nil
	}
	getExport := func() *ImportExportSymbol {
		getExportAssignmentExport := func(ex ast.Handle) *ImportExportSymbol {
			if ex.Symbol().Parent == nil {
				return nil
			}
			exportKind := core.IfElse(ex.ExportAssignmentIsExportEquals(), ExportKindExportEquals, ExportKindDefault)
			return &ImportExportSymbol{kind: ImpExpKindExport, symbol: symbol, exportInfo: &ExportInfo{exportingModuleSymbol: ex.Symbol().Parent, exportKind: exportKind}}
		}
		getExportKindForDeclaration := func(node ast.Handle) ExportKind {
			if ast.HasSyntacticModifier(node, ast.ModifierFlagsDefault) {
				return ExportKindDefault
			}
			return ExportKindNamed
		}
		getSpecialPropertyExport := func(node ast.Handle, useLhsSymbol bool) *ImportExportSymbol {
			var kind ExportKind
			switch ast.GetAssignmentDeclarationKind(node) {
			case ast.JSDeclarationKindExportsProperty:
				kind = ExportKindNamed
			case ast.JSDeclarationKindModuleExports:
				kind = ExportKindExportEquals
			default:
				return nil
			}
			sym := symbol
			if useLhsSymbol {
				sym = node.Symbol()
			}
			if sym == nil {
				return nil
			}
			return exportInfo(sym, kind)
		}
		parent := node.Parent()
		grandparent := parent.Parent()
		if symbol.ExportSymbol != nil {
			if ast.IsPropertyAccessExpression(parent) {
				if ast.IsBinaryExpression(grandparent) && ast.DeclarationNodes(symbol).Some(func(d ast.Handle) bool { return d == parent }) {
					return getSpecialPropertyExport(grandparent, false)
				}
				return nil
			}
			return exportInfo(symbol.ExportSymbol, getExportKindForDeclaration(parent))
		} else {
			exportNode := getExportNode(parent, node)
			switch {
			case !exportNode.IsNil() && (ast.HasSyntacticModifier(exportNode, ast.ModifierFlagsExport) || ast.IsImplicitlyExportedJSDocDeclaration(exportNode)):
				if ast.IsImportEqualsDeclaration(exportNode) && exportNode.ImportEqualsDeclarationModuleReference() == node {
					if comingFromExport {
						return nil
					}
					lhsSymbol := checker.GetSymbolAtLocation(exportNode.Name())
					return &ImportExportSymbol{kind: ImpExpKindImport, symbol: lhsSymbol}
				}
				return exportInfo(symbol, getExportKindForDeclaration(exportNode))
			case ast.IsNamespaceExport(parent):
				return exportInfo(symbol, ExportKindNamed)
			case ast.IsExportAssignment(parent):
				return getExportAssignmentExport(parent)
			case ast.IsExportAssignment(grandparent):
				return getExportAssignmentExport(grandparent)
			case ast.IsBinaryExpression(parent):
				return getSpecialPropertyExport(parent, true)
			case ast.IsBinaryExpression(grandparent):
				return getSpecialPropertyExport(grandparent, true)
			case ast.IsJSDocTypedefTag(parent) || ast.IsJSDocCallbackTag(parent):
				return exportInfo(symbol, ExportKindNamed)
			}
		}
		return nil
	}
	getImport := func() *ImportExportSymbol {
		if !isNodeImport(node) {
			return nil
		}
		var importedSymbol *ast.Symbol
		if symbol.Flags&ast.SymbolFlagsAlias != 0 {
			importedSymbol = checker.GetImmediateAliasedSymbol(symbol)
		} else {
			importedSymbol = getPropertySymbolOfObjectBindingPatternWithoutPropertyName(symbol, checker)
		}
		if importedSymbol == nil {
			return nil
		}
		importedSymbol = skipExportSpecifierSymbol(importedSymbol, checker)
		if importedSymbol == nil {
			return nil
		}
		if importedSymbol.Name == "export=" {
			importedSymbol = getExportEqualsLocalSymbol(importedSymbol, checker)
			if importedSymbol == nil {
				return nil
			}
		}
		importedName := symbolNameNoDefault(importedSymbol)
		if importedName == "" || importedName == ast.InternalSymbolNameDefault || importedName == symbol.Name {
			return &ImportExportSymbol{kind: ImpExpKindImport, symbol: importedSymbol}
		}
		return nil
	}
	result := getExport()
	if result == nil && !comingFromExport {
		result = getImport()
	}
	return result
}
func getExportInfo(exportSymbol *ast.Symbol, exportKind ExportKind, c *checker.Checker) *ExportInfo {
	if exportSymbol.Parent != nil {
		exportingModuleSymbol := c.GetMergedSymbol(exportSymbol.Parent)
		if checker.IsExternalModuleSymbol(exportingModuleSymbol) {
			return &ExportInfo{exportingModuleSymbol: exportingModuleSymbol, exportKind: exportKind}
		}
	}
	return nil
}

func getExportNode(parent ast.Handle, node ast.Handle) ast.Handle {
	var declaration ast.Handle
	switch {
	case ast.IsVariableDeclaration(parent):
		declaration = parent
	case ast.IsBindingElement(parent):
		declaration = ast.WalkUpBindingElementsAndPatterns(parent)
	}
	if !declaration.IsNil() {
		if parent.Name() == node && !ast.IsCatchClause(declaration.Parent()) && ast.IsVariableStatement(declaration.Parent().Parent()) {
			return declaration.Parent().Parent()
		}
		return ast.Handle{}
	}
	return parent
}
func isNodeImport(node ast.Handle) bool {
	parent := node.Parent()
	switch parent.Kind {
	case ast.KindImportEqualsDeclaration:
		return parent.Name() == node && isExternalModuleImportEquals(parent)
	case ast.KindImportSpecifier:
		return parent.PropertyName().IsNil()
	case ast.KindImportClause, ast.KindNamespaceImport:
		debug.Assert(parent.Name() == node)
		return true
	case ast.KindBindingElement:
		return ast.IsInJSFile(node) && ast.IsVariableDeclarationInitializedToBareOrAccessedRequire(parent.Parent().Parent())
	}
	return false
}
func isExternalModuleImportEquals(node ast.Handle) bool {
	moduleReference := node.ImportEqualsDeclarationModuleReference()
	return ast.IsExternalModuleReference(moduleReference) && moduleReference.Expression().Kind == ast.KindStringLiteral
}

func skipExportSpecifierSymbol(symbol *ast.Symbol, checker *checker.Checker) *ast.Symbol {
	for _, declaration := range ast.DeclarationNodes(symbol).All() {
		switch {
		case ast.IsExportSpecifier(declaration) && declaration.PropertyName().IsNil() && declaration.Parent().Parent().ModuleSpecifier().IsNil():
			return core.OrElse(checker.GetExportSpecifierLocalTargetSymbol(declaration), symbol)
		case ast.IsPropertyAccessExpression(declaration) && ast.IsModuleExportsAccessExpression(declaration.Expression()) && !ast.IsPrivateIdentifier(declaration.Name()):
			return checker.GetSymbolAtLocation(declaration)
		case ast.IsShorthandPropertyAssignment(declaration) && ast.IsBinaryExpression(declaration.Parent().Parent()) && ast.GetAssignmentDeclarationKind(declaration.Parent().Parent()) == ast.JSDeclarationKindModuleExports:
			return checker.GetExportSpecifierLocalTargetSymbol(declaration.Name())
		}
	}
	return symbol
}
func getExportEqualsLocalSymbol(importedSymbol *ast.Symbol, checker *checker.Checker) *ast.Symbol {
	if importedSymbol.Flags&ast.SymbolFlagsAlias != 0 {
		return checker.GetImmediateAliasedSymbol(importedSymbol)
	}
	decl := ast.NodeOf(importedSymbol.ValueDeclaration)
	debug.Assert(!decl.IsNil())
	switch {
	case ast.IsExportAssignment(decl):
		return decl.Expression().Symbol()
	case ast.IsBinaryExpression(decl):
		return decl.BinaryExpressionRight().Symbol()
	case ast.IsSourceFile(decl):
		return decl.Symbol()
	}
	return nil
}
func symbolNameNoDefault(symbol *ast.Symbol) string {
	if symbol.Name != ast.InternalSymbolNameDefault {
		return symbol.Name
	}
	for _, decl := range ast.DeclarationNodes(symbol).All() {
		name := ast.GetNameOfDeclaration(decl)
		if !name.IsNil() && ast.IsIdentifier(name) {
			return name.Text()
		}
	}
	return ""
}

func findModuleReferences(program *compiler.Program, sourceFiles []*ast.SourceFile, searchModuleSymbol *ast.Symbol, checker *checker.Checker) []ModuleReference {
	refs := []ModuleReference{}
	for _, referencingFile := range sourceFiles {
		searchSourceFile := ast.NodeOf(searchModuleSymbol.ValueDeclaration)
		if !searchSourceFile.IsNil() && searchSourceFile.Kind == ast.KindSourceFile {
			for _, ref := range referencingFile.ReferencedFiles {
				if program.GetSourceFileFromReference(referencingFile, ref) == ast.GetSourceFileOfNode(searchSourceFile) {
					refs = append(refs, ModuleReference{kind: ModuleReferenceKindReference, referencingFile: referencingFile, ref: ref})
				}
			}
			for _, ref := range referencingFile.TypeReferenceDirectives {
				referenced := program.GetResolvedTypeReferenceDirectiveFromTypeReferenceDirective(ref, referencingFile)
				if referenced != nil && referenced.ResolvedFileName == ast.GetSourceFileOfNode(searchSourceFile).FileName() {
					refs = append(refs, ModuleReference{kind: ModuleReferenceKindReference, referencingFile: referencingFile, ref: ref})
				}
			}
		}
		forEachImport(program, referencingFile, func(importDecl ast.Handle, moduleSpecifier ast.Handle) {
			moduleSymbol := checker.GetSymbolAtLocation(moduleSpecifier)
			if moduleSymbol == searchModuleSymbol {
				if ast.NodeIsSynthesized(importDecl) {
					refs = append(refs, ModuleReference{kind: ModuleReferenceKindImplicit, literal: moduleSpecifier, referencingFile: referencingFile})
				} else {
					refs = append(refs, ModuleReference{kind: ModuleReferenceKindImport, literal: moduleSpecifier})
				}
			}
		})
	}
	return refs
}
