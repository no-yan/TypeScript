package ls

import (
	"context"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/ls/change"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"slices"
	"strings"
)

func (l *LanguageService) OrganizeImports(ctx context.Context, sourceFile *ast.SourceFile, program *compiler.Program, kind lsproto.CodeActionKind) map[ // OrganizeImports organizes imports by:
//  1. Removing unused imports
//  2. Coalescing imports from the same module
//  3. Sorting imports
// Unmappable files are dropped by GetChanges, so a content-mapped file whose imports cannot be
// faithfully rewritten yields no edits rather than a corrupting one.
// Header comment preservation is handled via LeadingTriviaOptionExclude in the change tracker below
// Preserve header comment
// Preserve header comment
// no import clause
// getImportAttributesKey returns a key for grouping imports by their attributes.
// groupByNewlineContiguous groups declarations by blank lines between them.
// Must not skip trivia to detect newlines
string][]*lsproto.TextEdit {
	changeTracker := change.NewTracker(ctx, program.Options(), l.FormatOptions(), l.converters)
	shouldSort := kind == lsproto.CodeActionKindSourceSortImports || kind == lsproto.CodeActionKindSourceOrganizeImports
	shouldCombine := shouldSort
	shouldRemove := kind == lsproto.CodeActionKindSourceRemoveUnusedImports || kind == lsproto.CodeActionKindSourceOrganizeImports
	topLevelImportDecls := lsutil.FilterImportDeclarations(sourceFile.ParseRoot().Statements())
	topLevelImportGroupDecls := groupByNewlineContiguous(sourceFile, topLevelImportDecls)
	preferences := l.UserPreferences()
	comparersToTest, typeOrdersToTest := lsutil.GetDetectionLists(preferences)
	defaultComparer := comparersToTest[0]
	sort := lsutil.ResolveOrganizeImportsSort(preferences)
	var moduleSpecifierComparer func(a, b string) int
	var namedImportComparer func(a, b string) int
	if sort != lsutil.OrganizeImportsSortAuto {
		moduleSpecifierComparer = defaultComparer
		namedImportComparer = defaultComparer
	}
	typeOrder := preferences.OrganizeImportsTypeOrder
	if sort == lsutil.OrganizeImportsSortAuto {
		result, _ := lsutil.DetectModuleSpecifierCaseBySort(topLevelImportGroupDecls, comparersToTest)
		moduleSpecifierComparer = result
	}
	if typeOrder == lsutil.OrganizeImportsTypeOrderAuto || sort == lsutil.OrganizeImportsSortAuto {
		namedImportComparer2, typeOrder2, found := lsutil.DetectNamedImportOrganizationBySort(topLevelImportDecls, comparersToTest, typeOrdersToTest)
		if found {
			if namedImportComparer == nil || sort == lsutil.OrganizeImportsSortAuto {
				namedImportComparer = namedImportComparer2
			}
			if typeOrder == lsutil.OrganizeImportsTypeOrderAuto {
				typeOrder = typeOrder2
			}
		}
	}
	comparer := organizeImportsComparerSettings{moduleSpecifierComparer: moduleSpecifierComparer, namedImportComparer: namedImportComparer, typeOrder: typeOrder}
	for _, importGroupDecl := range topLevelImportGroupDecls {
		organizeImportsWorker(importGroupDecl, comparer, shouldSort, shouldCombine, shouldRemove, sourceFile, program, changeTracker, ctx)
	}
	if kind != lsproto.CodeActionKindSourceRemoveUnusedImports {
		topLevelExportGroupDecls := getTopLevelExportGroups(sourceFile)
		for _, exportGroupDecl := range topLevelExportGroupDecls {
			organizeExportsWorker(exportGroupDecl, comparer, sourceFile, changeTracker)
		}
	}
	for _, stmt := range sourceFile.ParseRoot().Statements() {
		if !ast.IsAmbientModule(stmt) {
			continue
		}
		ambientModule := stmt
		if ambientModule.Body().IsNil() {
			continue
		}
		moduleBody := ambientModule.Body()
		ambientModuleImportDecls := lsutil.FilterImportDeclarations(moduleBody.Statements())
		ambientModuleImportGroupDecls := groupByNewlineContiguous(sourceFile, ambientModuleImportDecls)
		for _, importGroupDecl := range ambientModuleImportGroupDecls {
			organizeImportsWorker(importGroupDecl, comparer, shouldSort, shouldCombine, shouldRemove, sourceFile, program, changeTracker, ctx)
		}
		if kind != lsproto.CodeActionKindSourceRemoveUnusedImports {
			var ambientModuleExportDecls []ast.Handle
			for _, s := range moduleBody.Statements() {
				if s.Kind() == ast.KindExportDeclaration {
					ambientModuleExportDecls = append(ambientModuleExportDecls, s)
				}
			}
			organizeExportsWorker(ambientModuleExportDecls, comparer, sourceFile, changeTracker)
		}
	}
	changes, _ := changeTracker.GetChanges()
	return changes
}

type organizeImportsComparerSettings struct {
	moduleSpecifierComparer func(a, b string) int
	namedImportComparer     func(a, b string) int
	typeOrder               lsutil.OrganizeImportsTypeOrder
}

func organizeImportsWorker(oldImportDecls []ast.Handle, comparer organizeImportsComparerSettings, shouldSort bool, shouldCombine bool, shouldRemove bool, sourceFile *ast.SourceFile, program *compiler.Program, changeTracker *change.Tracker, ctx context.Context) {
	if len(oldImportDecls) == 0 {
		return
	}
	processedImports := slices.Clone(oldImportDecls)
	if shouldRemove {
		typeChecker, done := program.GetTypeCheckerForFile(ctx, sourceFile)
		defer done()
		processedImports = removeUnusedImports(processedImports, sourceFile, typeChecker, program, changeTracker)
	}
	var newImportDecls []ast.Handle
	if shouldCombine {
		grouped := groupByModuleSpecifier(processedImports)
		if shouldSort {
			slices.SortFunc(grouped, func(a, b []ast.Handle) int {
				if len(a) == 0 || len(b) == 0 {
					return 0
				}
				return lsutil.CompareModuleSpecifiers(a[0].ModuleSpecifier(), b[0].ModuleSpecifier(), comparer.moduleSpecifierComparer)
			})
		}
		specifierComparer := lsutil.GetNamedImportSpecifierComparer(lsutil.UserPreferences{OrganizeImportsTypeOrder: comparer.typeOrder}, comparer.namedImportComparer)
		for _, importGroup := range grouped {
			coalesced := coalesceImportsWorker(importGroup, comparer.moduleSpecifierComparer, specifierComparer, sourceFile, changeTracker)
			if shouldSort {
				slices.SortFunc(coalesced, func(a, b ast.Handle) int {
					return lsutil.CompareImportsOrRequireStatements(a, b, comparer.moduleSpecifierComparer)
				})
			}
			newImportDecls = append(newImportDecls, coalesced...)
		}
	} else {
		newImportDecls = processedImports
	}
	if shouldSort && !shouldCombine {
		slices.SortFunc(newImportDecls, func(a, b ast.Handle) int {
			return lsutil.CompareImportsOrRequireStatements(a, b, comparer.moduleSpecifierComparer)
		})
	}
	if len(newImportDecls) == 0 {
		changeTracker.DeleteNodeRange(sourceFile, oldImportDecls[0], oldImportDecls[len(oldImportDecls)-1], change.LeadingTriviaOptionExclude, change.TrailingTriviaOptionInclude)
	} else {
		for _, imp := range newImportDecls {
			changeTracker.SetEmitFlags(imp, printer.EFNoLeadingComments)
		}
		options := change.NodeOptions{LeadingTriviaOption: change.LeadingTriviaOptionExclude, TrailingTriviaOption: change.TrailingTriviaOptionInclude, Suffix: "\n"}
		newNodes := core.Map(newImportDecls, func(s ast.Handle) ast.Handle {
			return s
		})
		changeTracker.ReplaceNodeWithNodes(sourceFile, oldImportDecls[0], newNodes, &options)
		if len(oldImportDecls) > 1 {
			for i := 1; i < len(oldImportDecls); i++ {
				changeTracker.Delete(sourceFile, oldImportDecls[i])
			}
		}
	}
}
func groupByModuleSpecifier(imports []ast.Handle) [][]ast.Handle {
	groups := make(map[string][]ast.Handle)
	var order []string
	for _, imp := range imports {
		specifier := lsutil.GetExternalModuleName(imp.ModuleSpecifier())
		if _, exists := groups[specifier]; !exists {
			order = append(order, specifier)
		}
		groups[specifier] = append(groups[specifier], imp)
	}
	result := make([][]ast.Handle, 0, len(order))
	for _, key := range order {
		result = append(result, groups[key])
	}
	return result
}
func removeUnusedImports(oldImports []ast.Handle, sourceFile *ast.SourceFile, typeChecker *checker.Checker, program *compiler.Program, changeTracker *change.Tracker) []ast.Handle {
	compilerOptions := program.Options()
	jsxElementsPresent := (sourceFile.ParseRoot().SubtreeFacts() & ast.SubtreeContainsJsx) != 0
	jsxModeNeedsExplicitImport := compilerOptions.Jsx == core.JsxEmitReact || compilerOptions.Jsx == core.JsxEmitReactNative
	factory := ast.NewFactory(ast.FactoryHooks{})
	usedImports := make([]ast.Handle, 0, len(oldImports))
	for _, importDecl := range oldImports {
		importClause := importDecl.ImportDeclarationImportClause()
		if importClause.IsNil() {
			usedImports = append(usedImports, importDecl)
			continue
		}
		clause := importClause
		name := clause.Name()
		namedBindings := clause.NamedBindings()
		if !name.IsNil() && !typeChecker.IsDeclarationUsed(sourceFile, name, jsxElementsPresent, jsxModeNeedsExplicitImport) {
			name = ast.Handle{}
		}
		if !namedBindings.IsNil() {
			switch namedBindings.Kind() {
			case ast.KindNamespaceImport:
				nsImport := namedBindings
				if !typeChecker.IsDeclarationUsed(sourceFile, nsImport.Name(), jsxElementsPresent, jsxModeNeedsExplicitImport) {
					namedBindings = ast.Handle{}
				}
			case ast.KindNamedImports:
				namedImports := namedBindings
				originalBindings := namedBindings
				newElements := filterUsedImportSpecifiers(namedImports.Elements(), typeChecker, sourceFile, jsxElementsPresent, jsxModeNeedsExplicitImport)
				if len(newElements) == 0 {
					namedBindings = ast.Handle{}
				} else if len(newElements) < len(namedImports.Elements()) {
					newList := factory.NewList(newElements)
					updatedNamedImports := factory.UpdateNamedImports(namedImports, newList)
					namedBindings = updatedNamedImports
				}
				if !namedBindings.IsNil() && !ast.NodeIsSynthesized(originalBindings) && !printer.RangeIsOnSingleLine(originalBindings.Loc(), sourceFile) {
					changeTracker.SetEmitFlags(namedBindings, printer.EFMultiLine)
				}
			}
		}
		if !name.IsNil() || !namedBindings.IsNil() {
			importDeclNode := importDecl
			newClause := factory.UpdateImportClause(clause, clause.ImportClausePhaseModifier(), name, namedBindings)
			newImportDecl := factory.UpdateImportDeclaration(importDeclNode, importDeclNode.Modifiers(), newClause, importDeclNode.ModuleSpecifier(), importDeclNode.Attributes())
			usedImports = append(usedImports, newImportDecl)
		} else {
			moduleSpecifier := importDecl.ModuleSpecifier()
			if hasModuleDeclarationMatchingSpecifier(sourceFile, moduleSpecifier) {
				if sourceFile.IsDeclarationFile {
					importDeclNode := importDecl
					newImportDecl := factory.UpdateImportDeclaration(importDeclNode, importDeclNode.Modifiers(), ast.Handle{}, importDeclNode.ModuleSpecifier(), importDeclNode.Attributes())
					usedImports = append(usedImports, newImportDecl)
				} else {
					usedImports = append(usedImports, importDecl)
				}
			}
		}
	}
	return usedImports
}
func filterUsedImportSpecifiers(elements []ast.Handle, typeChecker *checker.Checker, sourceFile *ast.SourceFile, jsxElementsPresent bool, jsxModeNeedsExplicitImport bool) []ast.Handle {
	var result []ast.Handle
	for _, elem := range elements {
		spec := elem
		if typeChecker.IsDeclarationUsed(sourceFile, spec.Name(), jsxElementsPresent, jsxModeNeedsExplicitImport) {
			result = append(result, elem)
		}
	}
	return result
}
func hasModuleDeclarationMatchingSpecifier(sourceFile *ast.SourceFile, moduleSpecifier ast.Handle) bool {
	if moduleSpecifier.IsNil() || !ast.IsStringLiteral(moduleSpecifier) {
		return false
	}
	moduleSpecifierText := moduleSpecifier.Text()
	for _, moduleName := range sourceFile.ModuleAugmentations {
		if ast.IsStringLiteral(moduleName) && moduleName.Text() == moduleSpecifierText {
			return true
		}
	}
	return false
}

func getImportAttributesKey(attributes ast.Handle) string {
	if attributes.IsNil() {
		return ""
	}
	importAttrs := attributes
	var key strings.Builder
	key.WriteString(importAttrs.ImportAttributesToken().String())
	key.WriteString(" ")
	attrList := importAttrs.ImportAttributesAttributes()
	attrNodes := make([]ast.Handle, len(importAttrs.Store().ListSlice(attrList)))
	copy(attrNodes, importAttrs.Store().ListSlice(attrList))
	slices.SortFunc(attrNodes, func(a, b ast.Handle) int {
		aName := a.ImportAttributeName().Text()
		bName := b.ImportAttributeName().Text()
		return stringutil.CompareStringsCaseSensitive(aName, bName)
	})
	for _, attrNode := range attrNodes {
		attr := attrNode
		key.WriteString(attr.Name().Text())
		key.WriteString(":")
		if ast.IsStringLiteralLike(attr.ImportAttributeValue()) {
			key.WriteString(`"`)
			key.WriteString(attr.ImportAttributeValue().Text())
			key.WriteString(`"`)
		} else {
			key.WriteString(attr.ImportAttributeValue().Text())
		}
		key.WriteString(" ")
	}
	return key.String()
}

func groupByNewlineContiguous(sourceFile *ast.SourceFile, decls []ast.Handle) [][]ast.Handle {
	s := scanner.NewScanner()
	s.SetSkipTrivia(false)
	var groups [][]ast.Handle
	var currentGroup []ast.Handle
	for _, decl := range decls {
		if len(currentGroup) > 0 && isNewGroup(sourceFile, decl, s) {
			groups = append(groups, currentGroup)
			currentGroup = nil
		}
		currentGroup = append(currentGroup, decl)
	}
	if len(currentGroup) > 0 {
		groups = append(groups, currentGroup)
	}
	return groups
}
func isNewGroup(sourceFile *ast.SourceFile, decl ast.Handle, s *scanner.Scanner) bool {
	fullStart := decl.Pos()
	if fullStart < 0 {
		return false
	}
	text := sourceFile.Text()
	textLen := len(text)
	if fullStart >= textLen {
		return false
	}
	startPos := scanner.SkipTrivia(text, fullStart)
	if startPos <= fullStart {
		return false
	}
	triviaLen := startPos - fullStart
	s.SetText(text[fullStart:startPos])
	numberOfNewLines := 0
	for s.TokenStart() < triviaLen {
		tokenKind := s.Scan()
		if tokenKind == ast.KindNewLineTrivia {
			numberOfNewLines++
			if numberOfNewLines >= 2 {
				return true
			}
		}
	}
	return false
}
func coalesceImportsWorker(importDecls []ast.Handle, comparer func(a, b string) int, specifierComparer func(s1, s2 ast.Handle) int, sourceFile *ast.SourceFile, changeTracker *change.Tracker) []ast.Handle {
	if len(importDecls) == 0 {
		return importDecls
	}
	importGroupsByAttributes := make(map[string][]ast.Handle)
	var attributeKeys []string
	for _, importDecl := range importDecls {
		key := getImportAttributesKey(importDecl.ImportDeclarationAttributes())
		if _, exists := importGroupsByAttributes[key]; !exists {
			attributeKeys = append(attributeKeys, key)
		}
		importGroupsByAttributes[key] = append(importGroupsByAttributes[key], importDecl)
	}
	coalescedImports := make([]ast.Handle, 0)
	for _, attributeKey := range attributeKeys {
		importGroupSameAttrs := importGroupsByAttributes[attributeKey]
		categorized := getCategorizedImports(importGroupSameAttrs)
		if !categorized.importWithoutClause.IsNil() {
			coalescedImports = append(coalescedImports, categorized.importWithoutClause)
		}
		factory := ast.NewFactory(ast.FactoryHooks{})
		for i, group := range []importGroup{categorized.regularImports, categorized.typeOnlyImports} {
			if group.isEmpty() {
				continue
			}
			isTypeOnly := i == 1
			if !isTypeOnly && len(group.defaultImports) == 1 && len(group.namespaceImports) == 1 && len(group.namedImports) == 0 {
				defaultImport := group.defaultImports[0]
				namespaceImport := group.namespaceImports[0]
				defaultClause := defaultImport.ImportDeclarationImportClause()
				namespaceBindings := namespaceImport.ImportDeclarationImportClause().ImportClauseNamedBindings()
				newClause := factory.UpdateImportClause(defaultClause, defaultClause.ImportClausePhaseModifier(), defaultClause.Name(), namespaceBindings)
				defaultDeclNode := defaultImport
				newImportDecl := factory.UpdateImportDeclaration(defaultDeclNode, defaultDeclNode.Modifiers(), newClause, defaultDeclNode.ModuleSpecifier(), defaultDeclNode.Attributes())
				coalescedImports = append(coalescedImports, newImportDecl)
				continue
			}
			slices.SortFunc(group.namespaceImports, func(a, b ast.Handle) int {
				n1 := a.ImportDeclarationImportClause().ImportClauseNamedBindings().NamespaceImportName()
				n2 := b.ImportDeclarationImportClause().ImportClauseNamedBindings().NamespaceImportName()
				return comparer(n1.Text(), n2.Text())
			})
			for _, nsImport := range group.namespaceImports {
				nsImportDecl := nsImport
				clause := nsImportDecl.ImportClause()
				newClause := factory.UpdateImportClause(clause, clause.ImportClausePhaseModifier(), ast.Handle{}, clause.NamedBindings())
				newImportDecl := factory.UpdateImportDeclaration(nsImportDecl, nsImportDecl.Modifiers(), newClause, nsImportDecl.ModuleSpecifier(), nsImportDecl.Attributes())
				coalescedImports = append(coalescedImports, newImportDecl)
			}
			var firstDefaultImport ast.Handle
			var firstNamedImport ast.Handle
			if len(group.defaultImports) > 0 {
				firstDefaultImport = group.defaultImports[0]
			}
			if len(group.namedImports) > 0 {
				firstNamedImport = group.namedImports[0]
			}
			importDecl := firstDefaultImport
			if importDecl.IsNil() {
				importDecl = firstNamedImport
			}
			if importDecl.IsNil() {
				continue
			}
			var newDefaultImport ast.Handle
			var newImportSpecifiers []ast.Handle
			if len(group.defaultImports) == 1 {
				newDefaultImport = group.defaultImports[0].ImportDeclarationImportClause().ImportClauseName()
			} else {
				for _, defaultImport := range group.defaultImports {
					defaultClause := defaultImport.ImportDeclarationImportClause()
					defaultName := defaultClause.Name()
					propertyName := factory.NewIdentifier("default")
					importSpec := factory.NewImportSpecifier(false, propertyName, defaultName)
					newImportSpecifiers = append(newImportSpecifiers, importSpec)
				}
			}
			newImportSpecifiers = append(newImportSpecifiers, getNewImportSpecifiers(group.namedImports, factory)...)
			slices.SortStableFunc(newImportSpecifiers, specifierComparer)
			var newNamedImports ast.Handle
			if len(newImportSpecifiers) == 0 {
				if !newDefaultImport.IsNil() {
					newNamedImports = ast.Handle{}
				} else {
					newNamedImports = factory.NewNamedImports(factory.NewList(nil))
				}
			} else {
				sortedList := factory.NewList(newImportSpecifiers)
				if !firstNamedImport.IsNil() {
					firstNamedBindings := firstNamedImport.ImportDeclarationImportClause().ImportClauseNamedBindings()
					origList := firstNamedBindings.ElementList()
					if firstNamedBindings.Store().ListHasTrailingComma(origList) {
						sortedList = factory.RelocateList(sortedList, firstNamedBindings.Store().ListLoc(origList))
					}
					newNamedImports = factory.UpdateNamedImports(firstNamedBindings, sortedList)
				} else {
					newNamedImports = factory.NewNamedImports(sortedList)
				}
			}
			if sourceFile != nil && !newNamedImports.IsNil() && !firstNamedImport.IsNil() {
				firstNamedBindings := firstNamedImport.ImportDeclarationImportClause().ImportClauseNamedBindings()
				if !ast.NodeIsSynthesized(firstNamedBindings) && !printer.RangeIsOnSingleLine(firstNamedBindings.Loc(), sourceFile) {
					changeTracker.SetEmitFlags(newNamedImports, printer.EFMultiLine)
				}
			}
			if isTypeOnly && !newDefaultImport.IsNil() && !newNamedImports.IsNil() {
				importDeclNode := importDecl
				defaultClause := factory.NewImportClause(importDeclNode.ImportClause().ImportClausePhaseModifier(), newDefaultImport, ast.Handle{})
				defaultImportDecl := factory.UpdateImportDeclaration(importDeclNode, importDeclNode.Modifiers(), defaultClause, importDeclNode.ModuleSpecifier(), importDeclNode.Attributes())
				coalescedImports = append(coalescedImports, defaultImportDecl)
				namedDeclNode := firstNamedImport
				if namedDeclNode.IsNil() {
					namedDeclNode = importDecl
				}
				namedImportDeclNode := namedDeclNode
				namedClause := factory.NewImportClause(namedImportDeclNode.ImportClause().ImportClausePhaseModifier(), ast.Handle{}, newNamedImports)
				namedImportDecl := factory.UpdateImportDeclaration(namedImportDeclNode, namedImportDeclNode.Modifiers(), namedClause, namedImportDeclNode.ModuleSpecifier(), namedImportDeclNode.Attributes())
				coalescedImports = append(coalescedImports, namedImportDecl)
			} else {
				importDeclNode := importDecl
				clauseNode := importDeclNode.ImportClause()
				newClause := factory.UpdateImportClause(clauseNode, clauseNode.ImportClausePhaseModifier(), newDefaultImport, newNamedImports)
				newImportDecl := factory.UpdateImportDeclaration(importDeclNode, importDeclNode.Modifiers(), newClause, importDeclNode.ModuleSpecifier(), importDeclNode.Attributes())
				coalescedImports = append(coalescedImports, newImportDecl)
			}
		}
	}
	return coalescedImports
}

type categorizedImports struct {
	importWithoutClause ast.Handle
	typeOnlyImports     importGroup
	regularImports      importGroup
}
type importGroup struct {
	defaultImports   []ast.Handle
	namespaceImports []ast.Handle
	namedImports     []ast.Handle
}

func (g importGroup) isEmpty() bool {
	return len(g.defaultImports) == 0 && len(g.namespaceImports) == 0 && len(g.namedImports) == 0
}
func getCategorizedImports(importDecls []ast.Handle) categorizedImports {
	var importWithoutClause ast.Handle
	var typeOnlyImports, regularImports importGroup
	for _, importDecl := range importDecls {
		if importDecl.ImportDeclarationImportClause().IsNil() {
			if importWithoutClause.IsNil() {
				importWithoutClause = importDecl
			}
			continue
		}
		clause := importDecl.ImportDeclarationImportClause()
		group := &regularImports
		if clause.IsTypeOnly() {
			group = &typeOnlyImports
		}
		name := clause.Name()
		namedBindings := clause.NamedBindings()
		if !name.IsNil() {
			group.defaultImports = append(group.defaultImports, importDecl)
		}
		if !namedBindings.IsNil() {
			switch namedBindings.Kind() {
			case ast.KindNamespaceImport:
				group.namespaceImports = append(group.namespaceImports, importDecl)
			case ast.KindNamedImports:
				group.namedImports = append(group.namedImports, importDecl)
			}
		}
	}
	return categorizedImports{importWithoutClause: importWithoutClause, typeOnlyImports: typeOnlyImports, regularImports: regularImports}
}
func getNewImportSpecifiers(namedImports []ast.Handle, factory ast.HandleFactory) []ast.Handle {
	var result []ast.Handle
	for _, namedImport := range namedImports {
		elements := tryGetNamedBindingElements(namedImport)
		if elements == nil {
			continue
		}
		for _, elem := range elements {
			spec := elem
			if !spec.PropertyName().IsNil() && !spec.Name().IsNil() {
				propertyText := spec.PropertyName().Text()
				nameText := spec.Name().Text()
				if propertyText == nameText {
					normalized := factory.UpdateImportSpecifier(spec, spec.IsTypeOnly(), ast.Handle{}, spec.Name())
					result = append(result, normalized)
					continue
				}
			}
			result = append(result, elem)
		}
	}
	return result
}
func tryGetNamedBindingElements(namedImport ast.Handle) []ast.Handle {
	if namedImport.Kind() != ast.KindImportDeclaration {
		return nil
	}
	importDecl := namedImport
	if importDecl.ImportClause().IsNil() {
		return nil
	}
	clause := importDecl.ImportClause()
	namedBindings := clause.NamedBindings()
	if !namedBindings.IsNil() && namedBindings.Kind() == ast.KindNamedImports {
		namedImportsNode := namedBindings
		return namedImportsNode.Elements()
	}
	return nil
}
func getTopLevelExportGroups(sourceFile *ast.SourceFile) [][]ast.Handle {
	var topLevelExportGroups [][]ast.Handle
	statements := sourceFile.ParseRoot().Statements()
	statementsLen := len(statements)
	i := 0
	groupIndex := 0
	for i < statementsLen {
		if statements[i].Kind() == ast.KindExportDeclaration {
			if groupIndex >= len(topLevelExportGroups) {
				topLevelExportGroups = append(topLevelExportGroups, []ast.Handle{})
			}
			exportDecl := statements[i]
			if !exportDecl.ModuleSpecifier().IsNil() {
				topLevelExportGroups[groupIndex] = append(topLevelExportGroups[groupIndex], statements[i])
				i++
			} else {
				for i < statementsLen && statements[i].Kind() == ast.KindExportDeclaration {
					topLevelExportGroups[groupIndex] = append(topLevelExportGroups[groupIndex], statements[i])
					i++
				}
				groupIndex++
			}
		} else {
			i++
			if groupIndex < len(topLevelExportGroups) && len(topLevelExportGroups[groupIndex]) > 0 {
				groupIndex++
			}
		}
	}
	var result [][]ast.Handle
	for _, exportGroup := range topLevelExportGroups {
		subGroups := groupByNewlineContiguous(sourceFile, exportGroup)
		result = append(result, subGroups...)
	}
	return result
}
func organizeExportsWorker(oldExportDecls []ast.Handle, comparer organizeImportsComparerSettings, sourceFile *ast.SourceFile, changeTracker *change.Tracker) {
	if len(oldExportDecls) == 0 {
		return
	}
	specifierComparerFunc := lsutil.GetNamedImportSpecifierComparer(lsutil.UserPreferences{OrganizeImportsTypeOrder: comparer.typeOrder}, comparer.namedImportComparer)
	newExportDecls := coalesceExportsWorker(oldExportDecls, specifierComparerFunc, comparer.moduleSpecifierComparer, sourceFile, changeTracker)
	if len(oldExportDecls) > 0 {
		if len(newExportDecls) == 0 {
			changeTracker.DeleteNodeRange(sourceFile, oldExportDecls[0], oldExportDecls[len(oldExportDecls)-1], change.LeadingTriviaOptionExclude, change.TrailingTriviaOptionInclude)
		} else {
			for _, exp := range newExportDecls {
				changeTracker.AddEmitFlags(exp, printer.EFNoLeadingComments)
			}
			options := change.NodeOptions{LeadingTriviaOption: change.LeadingTriviaOptionExclude, TrailingTriviaOption: change.TrailingTriviaOptionInclude, Suffix: "\n"}
			newNodes := core.Map(newExportDecls, func(s ast.Handle) ast.Handle {
				return s
			})
			changeTracker.ReplaceNodeWithNodes(sourceFile, oldExportDecls[0], newNodes, &options)
			if len(oldExportDecls) > 1 {
				for i := 1; i < len(oldExportDecls); i++ {
					changeTracker.Delete(sourceFile, oldExportDecls[i])
				}
			}
		}
	}
}
func coalesceExportsWorker(exportGroup []ast.Handle, specifierComparer func(s1, s2 ast.Handle) int, moduleSpecifierComparer func(a, b string) int, sourceFile *ast.SourceFile, changeTracker *change.Tracker) []ast.Handle {
	if len(exportGroup) == 0 {
		return exportGroup
	}
	exportsByModuleSpecifier := make(map[string][]ast.Handle)
	var moduleSpecifierOrder []string
	for _, exportDecl := range exportGroup {
		export := exportDecl
		var moduleSpecifier string
		if !export.ModuleSpecifier().IsNil() {
			moduleSpecifier = export.ModuleSpecifier().Text()
		}
		if _, exists := exportsByModuleSpecifier[moduleSpecifier]; !exists {
			moduleSpecifierOrder = append(moduleSpecifierOrder, moduleSpecifier)
		}
		exportsByModuleSpecifier[moduleSpecifier] = append(exportsByModuleSpecifier[moduleSpecifier], exportDecl)
	}
	slices.SortStableFunc(moduleSpecifierOrder, func(a, b string) int {
		if a == "" && b != "" {
			return 1
		}
		if a != "" && b == "" {
			return -1
		}
		return moduleSpecifierComparer(a, b)
	})
	var coalescedExports []ast.Handle
	factory := ast.NewFactory(ast.FactoryHooks{})
	for _, moduleSpecifier := range moduleSpecifierOrder {
		group := exportsByModuleSpecifier[moduleSpecifier]
		categorized := getCategorizedExports(group)
		if !categorized.exportWithoutClause.IsNil() {
			coalescedExports = append(coalescedExports, categorized.exportWithoutClause)
		}
		for _, subGroup := range [][]ast.Handle{categorized.namedExports, categorized.typeOnlyExports} {
			if len(subGroup) == 0 {
				continue
			}
			var newExportSpecifiers []ast.Handle
			for _, exportDecl := range subGroup {
				exportClause := exportDecl.ExportDeclarationExportClause()
				if !exportClause.IsNil() && exportClause.Kind() == ast.KindNamedExports {
					namedExports := exportClause
					newExportSpecifiers = append(newExportSpecifiers, namedExports.Elements()...)
				}
			}
			slices.SortStableFunc(newExportSpecifiers, specifierComparer)
			exportDecl := subGroup[0]
			var updatedExportClause ast.Handle
			if !exportDecl.ExportClause().IsNil() {
				if exportDecl.ExportClause().Kind() == ast.KindNamedExports {
					namedExports := exportDecl.ExportClause()
					sortedList := factory.NewList(newExportSpecifiers)
					updatedExportClause = factory.UpdateNamedExports(namedExports, sortedList)
					if sourceFile != nil && !ast.NodeIsSynthesized(namedExports) && !printer.RangeIsOnSingleLine(namedExports.Loc(), sourceFile) {
						changeTracker.SetEmitFlags(updatedExportClause, printer.EFMultiLine)
					}
				} else {
					updatedExportClause = exportDecl.ExportClause()
				}
			}
			newExportDecl := factory.UpdateExportDeclaration(exportDecl, exportDecl.Modifiers(), exportDecl.IsTypeOnly(), updatedExportClause, exportDecl.ModuleSpecifier(), exportDecl.Attributes())
			coalescedExports = append(coalescedExports, newExportDecl)
		}
	}
	return coalescedExports
}

type categorizedExports struct {
	exportWithoutClause ast.Handle
	namedExports        []ast.Handle
	typeOnlyExports     []ast.Handle
}

func getCategorizedExports(exportGroup []ast.Handle) categorizedExports {
	var exportWithoutClause ast.Handle
	var namedExports, typeOnlyExports []ast.Handle
	for _, exportDecl := range exportGroup {
		export := exportDecl
		if export.ExportClause().IsNil() {
			if exportWithoutClause.IsNil() {
				exportWithoutClause = exportDecl
			}
		} else if export.IsTypeOnly() {
			typeOnlyExports = append(typeOnlyExports, exportDecl)
		} else {
			namedExports = append(namedExports, exportDecl)
		}
	}
	return categorizedExports{exportWithoutClause: exportWithoutClause, namedExports: namedExports, typeOnlyExports: typeOnlyExports}
}
