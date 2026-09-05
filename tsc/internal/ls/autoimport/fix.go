package autoimport

import (
	"cmp"
	"context"
	"fmt"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/locale"
	"github.com/microsoft/TypeScript/tsc/internal/ls/change"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsconv"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/modulespecifiers"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"slices"
	"strings"
	"unicode"
)

type newImportBinding struct {
	kind          lsproto.ImportKind
	propertyName  string
	name          string
	addAsTypeOnly lsproto.AddAsTypeOnly
}
type Fix struct {
	*lsproto.AutoImportFix
	ModuleSpecifierKind      modulespecifiers.ResultKind
	IsReExport               bool
	ModuleFileName           string
	TypeOnlyAliasDeclaration ast.Handle
}
type addToExistingImportFix struct {
	importClauseOrBindingPattern ast.Handle
	defaultImport                *newImportBinding
	namedImport                  *newImportBinding
}

func (f *Fix) Edits(ctx context.Context, file *ast.SourceFile, compilerOptions *core.CompilerOptions, formatOptions lsutil.FormatCodeSettings, converters *lsconv.Converters, preferences lsutil.UserPreferences) ([]*lsproto.TextEdit, string, bool) {
	locale := locale.FromContext(ctx)
	tracker := change.NewTracker(ctx, compilerOptions, formatOptions, converters)
	switch f.Kind {
	case lsproto.AutoImportFixKindUseNamespace:
		description := addNamespaceQualifier(f, tracker, file, locale)
		edits, safe := fileEdits(tracker, file)
		return edits, description, safe
	case lsproto.AutoImportFixKindAddToExisting:
		if len(file.Imports()) <= int(f.ImportIndex) {
			panic("import index out of range")
		}
		existingFix := getAddToExistingImportFix(file, f)
		addToExistingImport(tracker, file, existingFix.importClauseOrBindingPattern, existingFix.defaultImport, core.SingleElementSlice(existingFix.namedImport), preferences)
		edits, safe := fileEdits(tracker, file)
		return edits, diagnostics.Update_import_from_0.Localize(locale, f.ModuleSpecifier), safe
	case lsproto.AutoImportFixKindAddNew:
		var declarations []ast.Handle
		defaultImport := core.IfElse(f.ImportKind == lsproto.ImportKindDefault, &newImportBinding{name: f.Name, addAsTypeOnly: f.AddAsTypeOnly}, nil)
		namedImports := core.IfElse(f.ImportKind == lsproto.ImportKindNamed, []*newImportBinding{{name: f.Name, addAsTypeOnly: f.AddAsTypeOnly}}, nil)
		var namespaceLikeImport *newImportBinding
		if f.ImportKind == lsproto.ImportKindNamespace || f.ImportKind == lsproto.ImportKindCommonJS {
			namespaceLikeImport = &newImportBinding{kind: f.ImportKind, name: f.Name}
		}
		quotePreference := lsutil.GetQuotePreference(file, preferences)
		if f.UseRequire {
			declarations = getNewRequires(tracker, f.ModuleSpecifier, quotePreference, defaultImport, namedImports, namespaceLikeImport, compilerOptions)
		} else {
			declarations = getNewImports(tracker, f.ModuleSpecifier, quotePreference, defaultImport, namedImports, namespaceLikeImport, compilerOptions, preferences)
		}
		insertImports(tracker, file, declarations, true, preferences)
		edits, safe := fileEdits(tracker, file)
		return edits, diagnostics.Add_import_from_0.Localize(locale, f.ModuleSpecifier), safe
	case lsproto.AutoImportFixKindPromoteTypeOnly:
		promotedDeclaration := promoteFromTypeOnly(tracker, f.TypeOnlyAliasDeclaration, compilerOptions, file, preferences)
		if promotedDeclaration.Kind == ast.KindImportSpecifier {
			moduleSpec := getModuleSpecifierText(promotedDeclaration.Parent().Parent())
			edits, safe := fileEdits(tracker, file)
			return edits, diagnostics.Remove_type_from_import_of_0_from_1.Localize(locale, f.Name, moduleSpec), safe
		}
		moduleSpec := getModuleSpecifierText(promotedDeclaration)
		edits, safe := fileEdits(tracker, file)
		return edits, diagnostics.Remove_type_from_import_declaration_from_0.Localize(locale, moduleSpec), safe
	case lsproto.AutoImportFixKindJsdocTypeImport:
		description := addImportType(f, file, preferences, tracker, locale)
		edits, safe := fileEdits(tracker, file)
		return edits, description, safe
	default:
		panic("unimplemented fix edit")
	}
}

func fileEdits(tracker *change.Tracker, file *ast.SourceFile) (edits []*lsproto.TextEdit, safe bool) {
	changes, unmappable := tracker.GetChanges()
	return changes[file.OriginalFileName()], len(unmappable) == 0
}
func addImportType(f *Fix, file *ast.SourceFile, preferences lsutil.UserPreferences, tracker *change.Tracker, locale locale.Locale) string {
	if f.UsagePosition == nil {
		panic("UsagePosition must be set for JSDoc type import fix")
	}
	quotePreference := lsutil.GetQuotePreference(file, preferences)
	quoteChar := "\""
	if quotePreference == lsutil.QuotePreferenceSingle {
		quoteChar = "'"
	}
	importTypePrefix := fmt.Sprintf("import(%s%s%s).", quoteChar, f.ModuleSpecifier, quoteChar)
	tracker.InsertText(file, *f.UsagePosition, importTypePrefix)
	return diagnostics.Change_0_to_1.Localize(locale, f.Name, importTypePrefix+f.Name)
}
func addNamespaceQualifier(f *Fix, tracker *change.Tracker, file *ast.SourceFile, locale locale.Locale) string {
	if f.UsagePosition == nil || f.NamespacePrefix == "" {
		panic("namespace fix requires usage position and prefix")
	}
	qualified := fmt.Sprintf("%s.%s", f.NamespacePrefix, f.Name)
	tracker.InsertText(file, *f.UsagePosition, f.NamespacePrefix+".")
	return diagnostics.Change_0_to_1.Localize(locale, f.Name, qualified)
}
func getAddToExistingImportFix(file *ast.SourceFile, fix *Fix) *addToExistingImportFix {
	if fix.Kind != lsproto.AutoImportFixKindAddToExisting {
		panic("expected add to existing import fix")
	}
	moduleSpecifier := file.Imports()[fix.ImportIndex]
	importNode := ast.TryGetImportFromModuleSpecifier(moduleSpecifier)
	if importNode.IsNil() {
		panic("expected import declaration")
	}
	var importClauseOrBindingPattern ast.Handle
	switch importNode.Kind {
	case ast.KindImportDeclaration:
		importClauseOrBindingPattern = importNode.ImportClause()
		if importClauseOrBindingPattern.IsNil() {
			panic("expected import clause")
		}
	case ast.KindCallExpression:
		if !ast.IsVariableDeclarationInitializedToRequire(importNode.Parent()) {
			panic("expected require call expression to be in variable declaration")
		}
		importClauseOrBindingPattern = importNode.Parent().Name()
		if importClauseOrBindingPattern.IsNil() || !ast.IsObjectBindingPattern(importClauseOrBindingPattern) {
			panic("expected object binding pattern in variable declaration")
		}
	default:
		panic("expected import declaration or require call expression")
	}
	defaultImport := core.IfElse(fix.ImportKind == lsproto.ImportKindDefault, &newImportBinding{kind: lsproto.ImportKindDefault, name: fix.Name, addAsTypeOnly: fix.AddAsTypeOnly}, nil)
	namedImports := core.IfElse(fix.ImportKind == lsproto.ImportKindNamed, &newImportBinding{kind: lsproto.ImportKindNamed, name: fix.Name, addAsTypeOnly: fix.AddAsTypeOnly}, nil)
	return &addToExistingImportFix{importClauseOrBindingPattern: importClauseOrBindingPattern, defaultImport: defaultImport, namedImport: namedImports}
}
func addToExistingImport(ct *change.Tracker, file *ast.SourceFile, importClauseOrBindingPattern ast.Handle, defaultImport *newImportBinding, namedImports []*newImportBinding, preferences lsutil.UserPreferences) {
	switch importClauseOrBindingPattern.Kind {
	case ast.KindObjectBindingPattern:
		bindingPattern := importClauseOrBindingPattern
		if defaultImport != nil {
			addElementToBindingPattern(ct, file, bindingPattern, defaultImport.name, "default")
		}
		for _, namedImport := range namedImports {
			addElementToBindingPattern(ct, file, bindingPattern, namedImport.name, "")
		}
		return
	case ast.KindImportClause:
		importClause := importClauseOrBindingPattern
		promoteFromTypeOnly := importClause.IsTypeOnly() && core.Some(append(namedImports, defaultImport), func(i *newImportBinding) bool {
			if i == nil {
				return false
			}
			return i.addAsTypeOnly == lsproto.AddAsTypeOnlyNotAllowed
		})
		var existingSpecifiers []ast.Handle
		if !importClause.NamedBindings().IsNil() && importClause.NamedBindings().Kind == ast.KindNamedImports {
			existingSpecifiers = importClause.NamedBindings().Elements()
		}
		if defaultImport != nil {
			debug.Assert(importClause.Name().IsNil(), "Cannot add a default import to an import clause that already has one")
			ct.InsertNodeAt(file, core.TextPos(astnav.GetStartOfNode(importClause, file, false)), ct.HandleFactory.NewIdentifier(defaultImport.name), change.NodeOptions{Suffix: ", "})
		}
		if len(namedImports) > 0 {
			specifierComparer, isSorted := lsutil.GetNamedImportSpecifierComparerWithDetection(importClause.Parent(), file, preferences)
			newSpecifiers := core.Map(namedImports, func(namedImport *newImportBinding) ast.Handle {
				var identifier ast.Handle
				if namedImport.propertyName != "" {
					identifier = ct.HandleFactory.NewIdentifier(namedImport.propertyName)				}
				return ct.HandleFactory.NewImportSpecifier((!importClause.IsTypeOnly() || promoteFromTypeOnly) && shouldUseTypeOnly(namedImport.addAsTypeOnly, preferences), identifier, ct.HandleFactory.NewIdentifier(namedImport.name))
			})
			slices.SortFunc(newSpecifiers, specifierComparer)
			if len(existingSpecifiers) > 0 && isSorted != core.TSFalse {
				specsToCompareAgainst := existingSpecifiers
				if promoteFromTypeOnly && len(existingSpecifiers) > 0 {
					specsToCompareAgainst = core.Map(existingSpecifiers, func(e ast.Handle) ast.Handle {
						spec := e
						var propertyName ast.Handle
						if !spec.PropertyName().IsNil() {
							propertyName = spec.PropertyName()
						}
						syntheticSpec := ct.HandleFactory.NewImportSpecifier(true, propertyName, spec.Name())
						return syntheticSpec
					})
				}
				for _, spec := range newSpecifiers {
					insertionIndex := lsutil.GetImportSpecifierInsertionIndex(specsToCompareAgainst, spec, specifierComparer)
					ct.InsertImportSpecifierAtIndex(file, spec, importClause.NamedBindings(), insertionIndex)
				}
			} else if len(existingSpecifiers) > 0 {
				for _, spec := range newSpecifiers {
					ct.InsertNodeInListAfter(file, existingSpecifiers[len(existingSpecifiers)-1], spec, 0)
				}
			} else {
				if len(newSpecifiers) > 0 {
					namedImports := ct.HandleFactory.NewNamedImports(ct.HandleFactory.NewList(newSpecifiers))
					if !importClause.NamedBindings().IsNil() {
						ct.ReplaceNode(file, importClause.NamedBindings(), namedImports, nil)
					} else {
						if importClause.Name().IsNil() {
							panic("Import clause must have either named imports or a default import")
						}
						ct.InsertNodeAfter(file, importClause.Name(), namedImports)
					}
				}
			}
		}
		if promoteFromTypeOnly {
			typeKeyword := getTypeKeywordOfTypeOnlyImport(importClause, file)
			ct.Delete(file, typeKeyword)
			if len(existingSpecifiers) > 0 {
				for _, specifier := range existingSpecifiers {
					if !specifier.ImportSpecifierIsTypeOnly() {
						ct.InsertModifierBefore(file, ast.KindTypeKeyword, specifier)
					}
				}
			}
		}
	default:
		panic("Unsupported clause kind: " + importClauseOrBindingPattern.KindString() + " for addToExistingImport")
	}
}
func getTypeKeywordOfTypeOnlyImport(importClause ast.Handle, sourceFile *ast.SourceFile) ast.Handle {
	debug.Assert(importClause.IsTypeOnly(), "import clause must be type-only")
	typeKeyword := astnav.FindChildOfKind(importClause, ast.KindTypeKeyword, sourceFile)
	debug.Assert(!typeKeyword.IsNil(), "type-only import clause should have a type keyword")
	return typeKeyword
}
func addElementToBindingPattern(ct *change.Tracker, file *ast.SourceFile, bindingPattern ast.Handle, name string, propertyName string) {
	element := ct.HandleFactory.NewBindingElement(ast.Handle{}, ast.Handle{}, ct.HandleFactory.NewIdentifier(name), core.IfElse(propertyName == "", ast.Handle{}, ct.HandleFactory.NewIdentifier(propertyName)))
	if len(bindingPattern.Elements()) > 0 {
		ct.InsertNodeInListAfter(file, bindingPattern.Elements()[len(bindingPattern.Elements())-1], element, bindingPattern.ElementList())
	} else {
		ct.ReplaceNode(file, bindingPattern, ct.HandleFactory.NewBindingPattern(ast.KindObjectBindingPattern, ct.HandleFactory.NewList([]ast.Handle{element})), nil)
	}
}
func getNewImports(ct *change.Tracker, moduleSpecifier string, quotePreference lsutil.QuotePreference, defaultImport *newImportBinding, namedImports []*newImportBinding, namespaceLikeImport *newImportBinding, compilerOptions *core.CompilerOptions, preferences lsutil.UserPreferences) []ast.Handle {
	tokenFlags := core.IfElse(quotePreference == lsutil.QuotePreferenceSingle, ast.TokenFlagsSingleQuote, ast.TokenFlagsNone)
	moduleSpecifierStringLiteral := ct.HandleFactory.NewStringLiteral(moduleSpecifier, tokenFlags)
	var statements []ast.Handle
	if defaultImport != nil || len(namedImports) > 0 {
		topLevelTypeOnly := (defaultImport == nil || needsTypeOnly(defaultImport.addAsTypeOnly)) && core.Every(namedImports, func(i *newImportBinding) bool {
			return needsTypeOnly(i.addAsTypeOnly)
		}) || (compilerOptions.VerbatimModuleSyntax.IsTrue() || preferences.PreferTypeOnlyAutoImports.IsTrue()) && (defaultImport == nil || defaultImport.addAsTypeOnly != lsproto.AddAsTypeOnlyNotAllowed) && !core.Some(namedImports, func(i *newImportBinding) bool {
			return i.addAsTypeOnly == lsproto.AddAsTypeOnlyNotAllowed
		})
		var defaultImportNode ast.Handle
		if defaultImport != nil {
			defaultImportNode = ct.HandleFactory.NewIdentifier(defaultImport.name)
		}
		statements = append(statements, makeImport(ct, defaultImportNode, core.Map(namedImports, func(namedImport *newImportBinding) ast.Handle {
			var namedImportPropertyName ast.Handle
			if namedImport.propertyName != "" {
				namedImportPropertyName = ct.HandleFactory.NewIdentifier(namedImport.propertyName)
			}
			return ct.HandleFactory.NewImportSpecifier(!topLevelTypeOnly && shouldUseTypeOnly(namedImport.addAsTypeOnly, preferences), namedImportPropertyName, ct.HandleFactory.NewIdentifier(namedImport.name))
		}), moduleSpecifierStringLiteral, topLevelTypeOnly))
	}
	if namespaceLikeImport != nil {
		var declaration ast.Handle
		if namespaceLikeImport.kind == lsproto.ImportKindCommonJS {
			declaration = ct.HandleFactory.NewImportEqualsDeclaration(0, shouldUseTypeOnly(namespaceLikeImport.addAsTypeOnly, preferences), ct.HandleFactory.NewIdentifier(namespaceLikeImport.name), ct.HandleFactory.NewExternalModuleReference(moduleSpecifierStringLiteral))
		} else {
			declaration = ct.HandleFactory.NewImportDeclaration(0, ct.HandleFactory.NewImportClause(core.IfElse(shouldUseTypeOnly(namespaceLikeImport.addAsTypeOnly, preferences), ast.KindTypeKeyword, ast.KindUnknown), ast.Handle{}, ct.HandleFactory.NewNamespaceImport(ct.HandleFactory.NewIdentifier(namespaceLikeImport.name))), moduleSpecifierStringLiteral, ast.Handle{})
		}
		statements = append(statements, declaration)
	}
	if len(statements) == 0 {
		panic("No statements to insert for new imports")
	}
	return statements
}
func getNewRequires(changeTracker *change.Tracker, moduleSpecifier string, quotePreference lsutil.QuotePreference, defaultImport *newImportBinding, namedImports []*newImportBinding, namespaceLikeImport *newImportBinding, compilerOptions *core.CompilerOptions) []ast.Handle {
	quotedModuleSpecifier := changeTracker.HandleFactory.NewStringLiteral(moduleSpecifier, core.IfElse(quotePreference == lsutil.QuotePreferenceSingle, ast.TokenFlagsSingleQuote, ast.TokenFlagsNone))
	var statements []ast.Handle
	if defaultImport != nil || len(namedImports) > 0 {
		bindingElements := []ast.Handle{}
		for _, namedImport := range namedImports {
			var propertyName ast.Handle
			if namedImport.propertyName != "" {
				propertyName = changeTracker.HandleFactory.NewIdentifier(namedImport.propertyName)
			}
			bindingElements = append(bindingElements, changeTracker.HandleFactory.NewBindingElement(ast.Handle{}, propertyName, changeTracker.HandleFactory.NewIdentifier(namedImport.name), ast.Handle{}))
		}
		if defaultImport != nil {
			bindingElements = append([]ast.Handle{changeTracker.HandleFactory.NewBindingElement(ast.Handle{}, changeTracker.HandleFactory.NewIdentifier("default"), changeTracker.HandleFactory.NewIdentifier(defaultImport.name), ast.Handle{})}, bindingElements...)
		}
		declaration := createConstEqualsRequireDeclaration(changeTracker, changeTracker.HandleFactory.NewBindingPattern(ast.KindObjectBindingPattern, changeTracker.HandleFactory.NewList(bindingElements)), quotedModuleSpecifier)
		statements = append(statements, declaration)
	}
	if namespaceLikeImport != nil {
		declaration := createConstEqualsRequireDeclaration(changeTracker, changeTracker.HandleFactory.NewIdentifier(namespaceLikeImport.name), quotedModuleSpecifier)
		statements = append(statements, declaration)
	}
	debug.Assert(statements != nil)
	return statements
}
func createConstEqualsRequireDeclaration(changeTracker *change.Tracker, name ast.Handle, quotedModuleSpecifier ast.Handle) ast.Handle {
	return changeTracker.HandleFactory.NewVariableStatement(0, changeTracker.HandleFactory.NewVariableDeclarationList(changeTracker.HandleFactory.NewList([]ast.Handle{changeTracker.HandleFactory.NewVariableDeclaration(name, ast.Handle{}, ast.Handle{}, changeTracker.HandleFactory.NewCallExpression(changeTracker.HandleFactory.NewIdentifier("require"), ast.Handle{}, 0, changeTracker.HandleFactory.NewList([]ast.Handle{quotedModuleSpecifier}), ast.NodeFlagsNone))}), ast.NodeFlagsConst))
}
func insertImports(ct *change.Tracker, sourceFile *ast.SourceFile, imports []ast.Handle, blankLineBetween bool, preferences lsutil.UserPreferences) {
	var existingImportStatements []ast.Handle
	if imports[0].Kind == ast.KindVariableStatement {
		existingImportStatements = core.Filter(sourceFile.ParseRoot().Statements(), ast.IsRequireVariableStatement)
	} else {
		existingImportStatements = core.Filter(sourceFile.ParseRoot().Statements(), ast.IsAnyImportSyntax)
	}
	comparer, isSorted := lsutil.GetOrganizeImportsStringComparerWithDetection(existingImportStatements, preferences)
	sortedNewImports := slices.Clone(imports)
	slices.SortFunc(sortedNewImports, func(a, b ast.Handle) int {
		return lsutil.CompareImportsOrRequireStatements(a, b, comparer)
	})
	if len(existingImportStatements) > 0 && isSorted {
		for _, newImport := range sortedNewImports {
			insertionIndex := lsutil.GetImportDeclarationInsertIndex(existingImportStatements, newImport, func(a, b ast.Handle) stringutil.Comparison {
				return lsutil.CompareImportsOrRequireStatements(a, b, comparer)
			})
			if insertionIndex == 0 {
				leadingTriviaOption := change.LeadingTriviaOptionNone
				if existingImportStatements[0] == sourceFile.ParseRoot().Statements()[0] {
					leadingTriviaOption = change.LeadingTriviaOptionExclude
				}
				ct.InsertNodeBefore(sourceFile, existingImportStatements[0], newImport, false, leadingTriviaOption)
			} else {
				prevImport := existingImportStatements[insertionIndex-1]
				ct.InsertNodeAfter(sourceFile, prevImport, newImport)
			}
		}
	} else if len(existingImportStatements) > 0 {
		ct.InsertNodesAfter(sourceFile, existingImportStatements[len(existingImportStatements)-1], sortedNewImports)
	} else {
		ct.InsertAtTopOfFile(sourceFile, sortedNewImports, blankLineBetween)
	}
}
func makeImport(ct *change.Tracker, defaultImport ast.Handle, namedImports []ast.Handle, moduleSpecifier ast.Handle, isTypeOnly bool) ast.Handle {
	var newNamedImports ast.Handle
	if len(namedImports) > 0 {
		newNamedImports = ct.HandleFactory.NewNamedImports(ct.HandleFactory.NewList(namedImports))
	}
	var importClause ast.Handle
	if !defaultImport.IsNil() || !newNamedImports.IsNil() {
		importClause = ct.HandleFactory.NewImportClause(core.IfElse(isTypeOnly, ast.KindTypeKeyword, ast.KindUnknown), defaultImport, newNamedImports)
	}
	return ct.HandleFactory.NewImportDeclaration(0, importClause, moduleSpecifier, ast.Handle{})
}
func (v *View) GetFixes(ctx context.Context, export *Export, forJSX bool, isValidTypeOnlyUseSite bool, usagePosition *lsproto.Position) []*Fix {
	var fixes []*Fix
	if namespaceFix := v.tryUseExistingNamespaceImport(ctx, export, usagePosition); namespaceFix != nil {
		fixes = append(fixes, namespaceFix)
	}
	if fix := v.tryAddToExistingImport(ctx, export, isValidTypeOnlyUseSite); fix != nil {
		return append(fixes, fix)
	}
	moduleSpecifier, moduleSpecifierKind := v.GetModuleSpecifier(export, v.preferences)
	if moduleSpecifier == "" {
		if len(fixes) > 0 {
			return fixes
		}
		return nil
	}
	isJs := tspath.HasJSFileExtension(v.importingFile.FileName())
	importedSymbolHasValueMeaning := export.Flags&ast.SymbolFlagsValue != 0 || export.IsUnresolvedAlias()
	if !importedSymbolHasValueMeaning && isJs && usagePosition != nil {
		return []*Fix{{AutoImportFix: &lsproto.AutoImportFix{Kind: lsproto.AutoImportFixKindJsdocTypeImport, ModuleSpecifier: moduleSpecifier, Name: export.Name(), UsagePosition: usagePosition}, ModuleSpecifierKind: moduleSpecifierKind, IsReExport: export.Target.ModuleID != export.ModuleID, ModuleFileName: export.ModuleFileName}}
	}
	importKind := getImportKind(v.importingFile, export, v.program, false)
	addAsTypeOnly := getAddAsTypeOnly(isValidTypeOnlyUseSite, export, v.program.Options())
	name := export.Name()
	startsWithUpper := unicode.IsUpper(rune(name[0]))
	if forJSX && !startsWithUpper {
		if export.IsRenameable() {
			name = fmt.Sprintf("%c%s", unicode.ToUpper(rune(name[0])), name[1:])
		} else {
			return nil
		}
	}
	return append(fixes, &Fix{AutoImportFix: &lsproto.AutoImportFix{Kind: lsproto.AutoImportFixKindAddNew, ImportKind: importKind, ModuleSpecifier: moduleSpecifier, Name: name, UseRequire: v.shouldUseRequire(), AddAsTypeOnly: addAsTypeOnly}, ModuleSpecifierKind: moduleSpecifierKind, IsReExport: export.Target.ModuleID != export.ModuleID, ModuleFileName: export.ModuleFileName})
}

func getAddAsTypeOnly(isValidTypeOnlyUseSite bool, export *Export, compilerOptions *core.CompilerOptions) lsproto.AddAsTypeOnly {
	if !isValidTypeOnlyUseSite {
		return lsproto.AddAsTypeOnlyNotAllowed
	}
	if compilerOptions.VerbatimModuleSyntax.IsTrue() && (export.IsTypeOnly || export.Flags&ast.SymbolFlagsValue == 0) || export.IsTypeOnly && export.Flags&ast.SymbolFlagsValue != 0 {
		return lsproto.AddAsTypeOnlyRequired
	}
	return lsproto.AddAsTypeOnlyAllowed
}
func (v *View) tryUseExistingNamespaceImport(ctx context.Context, export *Export, usagePosition *lsproto.Position) *Fix {
	if usagePosition == nil {
		return nil
	}
	if getImportKind(v.importingFile, export, v.program, false) != lsproto.ImportKindNamed {
		return nil
	}
	existingImports := v.getExistingImports(ctx)
	matchingDeclarations := existingImports.Get(export.ModuleID)
	for _, existingImport := range matchingDeclarations {
		namespacePrefix := getNamespaceLikeImportText(existingImport.node)
		if namespacePrefix == "" || existingImport.moduleSpecifier == "" {
			continue
		}
		return &Fix{AutoImportFix: &lsproto.AutoImportFix{Kind: lsproto.AutoImportFixKindUseNamespace, Name: export.Name(), ModuleSpecifier: existingImport.moduleSpecifier, ImportKind: lsproto.ImportKindNamespace, AddAsTypeOnly: lsproto.AddAsTypeOnlyAllowed, ImportIndex: int32(existingImport.index), UsagePosition: usagePosition, NamespacePrefix: namespacePrefix}}
	}
	return nil
}
func getNamespaceLikeImportText(declaration ast.Handle) string {
	switch declaration.Kind {
	case ast.KindVariableDeclaration:
		name := declaration.Name()
		if !name.IsNil() && name.Kind == ast.KindIdentifier {
			return name.Text()
		}
		return ""
	case ast.KindImportEqualsDeclaration:
		return declaration.Name().Text()
	case ast.KindJSDocImportTag, ast.KindImportDeclaration:
		importClause := declaration.ImportClause()
		if !importClause.IsNil() && !importClause.ImportClauseNamedBindings().IsNil() && importClause.ImportClauseNamedBindings().Kind == ast.KindNamespaceImport {
			return importClause.ImportClauseNamedBindings().Name().Text()
		}
		return ""
	default:
		return ""
	}
}
func (v *View) tryAddToExistingImport(ctx context.Context, export *Export, isValidTypeOnlyUseSite bool) *Fix {
	existingImports := v.getExistingImports(ctx)
	matchingDeclarations := existingImports.Get(export.ModuleID)
	if len(matchingDeclarations) == 0 {
		return nil
	}
	if ast.IsSourceFileJS(v.importingFile) && export.Flags&ast.SymbolFlagsValue == 0 && !core.Every(matchingDeclarations, func(i existingImport) bool {
		return ast.IsJSDocImportTag(i.node)
	}) {
		return nil
	}
	importKind := getImportKind(v.importingFile, export, v.program, false)
	if importKind == lsproto.ImportKindCommonJS || importKind == lsproto.ImportKindNamespace {
		return nil
	}
	addAsTypeOnly := getAddAsTypeOnly(isValidTypeOnlyUseSite, export, v.program.Options())
	var best *Fix
	for _, existingImport := range matchingDeclarations {
		if existingImport.node.Kind == ast.KindImportEqualsDeclaration {
			continue
		}
		if existingImport.node.Kind == ast.KindVariableDeclaration {
			if (importKind == lsproto.ImportKindNamed || importKind == lsproto.ImportKindDefault) && existingImport.node.Name().Kind == ast.KindObjectBindingPattern {
				fix := &Fix{AutoImportFix: &lsproto.AutoImportFix{Kind: lsproto.AutoImportFixKindAddToExisting, Name: export.Name(), ImportKind: importKind, ImportIndex: int32(existingImport.index), ModuleSpecifier: existingImport.moduleSpecifier, AddAsTypeOnly: addAsTypeOnly}}
				if addAsTypeOnly == lsproto.AddAsTypeOnlyNotAllowed {
					return fix
				}
				if best == nil {
					best = fix
				}
			}
			continue
		}
		importClauseNode := existingImport.node.ImportClause()
		if importClauseNode.IsNil() || !ast.IsStringLiteralLike(existingImport.node.ModuleSpecifier()) {
			continue
		}
		importClause := importClauseNode
		namedBindings := importClause.NamedBindings()
		if importClause.IsTypeOnly() && !(importKind == lsproto.ImportKindNamed && !namedBindings.IsNil()) {
			continue
		}
		if importKind == lsproto.ImportKindDefault && (!importClause.Name().IsNil() || addAsTypeOnly == lsproto.AddAsTypeOnlyRequired && !namedBindings.IsNil()) {
			continue
		}
		if importKind == lsproto.ImportKindNamed && !namedBindings.IsNil() && namedBindings.Kind == ast.KindNamespaceImport {
			continue
		}
		fix := &Fix{AutoImportFix: &lsproto.AutoImportFix{Kind: lsproto.AutoImportFixKindAddToExisting, Name: export.Name(), ImportKind: importKind, ImportIndex: int32(existingImport.index), ModuleSpecifier: existingImport.moduleSpecifier, AddAsTypeOnly: addAsTypeOnly}}
		isTypeOnly := importClause.IsTypeOnly()
		if (addAsTypeOnly != lsproto.AddAsTypeOnlyNotAllowed && isTypeOnly) || (addAsTypeOnly == lsproto.AddAsTypeOnlyNotAllowed && !isTypeOnly) {
			return fix
		}
		if best == nil {
			best = fix
		}
	}
	return best
}
func GetImportKindForImportStatement(importingFile *ast.SourceFile, export *Export, program *compiler.Program) lsproto.ImportKind {
	return getImportKind(importingFile, export, program, true)
}
func getImportKind(importingFile *ast.SourceFile, export *Export, program *compiler.Program, forceImportKeyword bool) lsproto.ImportKind {
	if program.Options().VerbatimModuleSyntax.IsTrue() && program.GetEmitModuleFormatOfFile(importingFile) == core.ModuleKindCommonJS {
		return lsproto.ImportKindCommonJS
	}
	switch export.Syntax {
	case ExportSyntaxDefaultModifier, ExportSyntaxDefaultDeclaration:
		return lsproto.ImportKindDefault
	case ExportSyntaxNamed:
		if export.ExportName == ast.InternalSymbolNameDefault {
			return lsproto.ImportKindDefault
		}
		fallthrough
	case ExportSyntaxModifier, ExportSyntaxStar, ExportSyntaxCommonJSExportsProperty:
		return lsproto.ImportKindNamed
	case ExportSyntaxEquals, ExportSyntaxCommonJSModuleExports, ExportSyntaxUMD:
		if export.ExportName != ast.InternalSymbolNameExportEquals {
			return lsproto.ImportKindNamed
		}
		for _, statement := range importingFile.ParseRoot().Statements() {
			if ast.IsImportEqualsDeclaration(statement) && !ast.NodeIsMissing(statement.ImportEqualsDeclarationModuleReference()) {
				return lsproto.ImportKindCommonJS
			}
		}
		if !importingFile.ExternalModuleIndicator.IsNil() || forceImportKeyword || !ast.IsSourceFileJS(importingFile) {
			return lsproto.ImportKindDefault
		}
		return lsproto.ImportKindCommonJS
	default:
		panic("unhandled export syntax kind: " + export.Syntax.String())
	}
}

type existingImport struct {
	node            ast.Handle
	moduleSpecifier string
	index           int
}

func (v *View) getExistingImports(ctx context.Context) *collections.MultiMap[ModuleID, existingImport] {
	if v.existingImports != nil {
		return v.existingImports
	}
	result := collections.NewMultiMapWithSizeHint[ModuleID, existingImport](len(v.importingFile.Imports()))
	ch, done := v.program.GetTypeChecker(ctx)
	defer done()
	for i, moduleSpecifier := range v.importingFile.Imports() {
		node := ast.TryGetImportFromModuleSpecifier(moduleSpecifier)
		if node.IsNil() {
			panic("error: did not expect node kind " + moduleSpecifier.Kind.String())
		} else if ast.IsVariableDeclarationInitializedToRequire(node.Parent()) {
			if moduleSymbol := ch.ResolveExternalModuleName(moduleSpecifier); moduleSymbol != nil {
				if moduleID, _, ok := tryGetModuleIDAndFileNameOfModuleSymbol(moduleSymbol); ok {
					result.Add(moduleID, existingImport{node: node.Parent(), moduleSpecifier: moduleSpecifier.Text(), index: i})
				}
			}
		} else if node.Kind == ast.KindImportDeclaration || node.Kind == ast.KindImportEqualsDeclaration || node.Kind == ast.KindJSDocImportTag {
			if moduleSymbol := ch.GetSymbolAtLocation(moduleSpecifier); moduleSymbol != nil {
				if moduleID, _, ok := tryGetModuleIDAndFileNameOfModuleSymbol(moduleSymbol); ok {
					result.Add(moduleID, existingImport{node: node, moduleSpecifier: moduleSpecifier.Text(), index: i})
				}
			}
		}
	}
	v.existingImports = result
	return result
}
func (v *View) shouldUseRequire() bool {
	if v.shouldUseRequireForFixes != nil {
		return *v.shouldUseRequireForFixes
	}
	shouldUseRequire := v.computeShouldUseRequire()
	v.shouldUseRequireForFixes = &shouldUseRequire
	return shouldUseRequire
}

type fileSyntaxKind int

const (
	fileSyntaxKindAmbiguous fileSyntaxKind = iota
	fileSyntaxKindESM
	fileSyntaxKindCJS
)

func detectSyntax(file *ast.SourceFile, options *core.CompilerOptions) fileSyntaxKind {
	hasESM, hasCJS := detectSyntaxIndicators(file, options)
	switch {
	case hasCJS && !hasESM:
		return fileSyntaxKindCJS
	case hasESM && !hasCJS:
		return fileSyntaxKindESM
	default:
		return fileSyntaxKindAmbiguous
	}
}

func detectSyntaxIndicators(file *ast.SourceFile, options *core.CompilerOptions) (hasESM bool, hasCJS bool) {
	hasCJS = !file.CommonJSModuleIndicator.IsNil()
	if options.GetEmitModuleDetectionKind() != core.ModuleDetectionKindForce {
		hasESM = !file.ExternalModuleIndicator.IsNil()
		return hasESM, hasCJS
	}
	if !file.ExternalModuleIndicator.IsNil() && file.ExternalModuleIndicator != file.ParseRoot() {
		return true, hasCJS
	}
	for _, imp := range file.Imports() {
		if imp.Flags()&ast.NodeFlagsSynthesized != 0 {
			continue
		}
		parent := imp.Parent()
		if parent.IsNil() {
			continue
		}
		switch parent.Kind {
		case ast.KindImportDeclaration, ast.KindJSImportDeclaration, ast.KindExportDeclaration:
			return true, hasCJS
		case ast.KindExternalModuleReference:
			return true, hasCJS
		}
	}
	return hasESM, hasCJS
}
func (v *View) computeShouldUseRequire() bool {
	if !tspath.HasJSFileExtension(v.importingFile.FileName()) {
		return false
	}
	switch detectSyntax(v.importingFile, v.program.Options()) {
	case fileSyntaxKindCJS:
		return true
	case fileSyntaxKindESM:
		return false
	}
	switch v.program.GetImpliedNodeFormatForEmit(v.importingFile) {
	case core.ModuleKindCommonJS:
		return true
	case core.ModuleKindESNext:
		return false
	}
	if v.program.Options().ConfigFilePath != "" {
		return v.program.Options().GetEmitModuleKind() < core.ModuleKindES2015
	}
	for _, otherFile := range v.program.GetSourceFiles() {
		switch {
		case otherFile == v.importingFile, !ast.IsSourceFileJS(otherFile), v.program.IsSourceFileFromExternalLibrary(otherFile):
			continue
		}
		switch detectSyntax(otherFile, v.program.Options()) {
		case fileSyntaxKindCJS:
			return true
		case fileSyntaxKindESM:
			return false
		}
	}
	return true
}
func needsTypeOnly(addAsTypeOnly lsproto.AddAsTypeOnly) bool {
	return addAsTypeOnly == lsproto.AddAsTypeOnlyRequired
}
func shouldUseTypeOnly(addAsTypeOnly lsproto.AddAsTypeOnly, preferences lsutil.UserPreferences) bool {
	return needsTypeOnly(addAsTypeOnly) || addAsTypeOnly != lsproto.AddAsTypeOnlyNotAllowed && preferences.PreferTypeOnlyAutoImports.IsTrue()
}

func (v *View) CompareFixesForSorting(a, b *Fix) int {
	if res := v.CompareFixesForRanking(a, b); res != 0 {
		return res
	}
	return v.compareModuleSpecifiersForSorting(a, b)
}

func (v *View) CompareFixesForRanking(a, b *Fix) int {
	if res := compareFixKinds(a.Kind, b.Kind); res != 0 {
		return res
	}
	return v.compareModuleSpecifiersForRanking(a, b)
}
func compareFixKinds(a, b lsproto.AutoImportFixKind) int {
	return int(a) - int(b)
}
func (v *View) compareModuleSpecifiersForRanking(a, b *Fix) int {
	if comparison := compareModuleSpecifierRelativity(a, b, v.preferences); comparison != 0 {
		return comparison
	}
	if a.ModuleSpecifierKind == modulespecifiers.ResultKindAmbient && b.ModuleSpecifierKind == modulespecifiers.ResultKindAmbient {
		if comparison := v.compareNodeCoreModuleSpecifiers(a.ModuleSpecifier, b.ModuleSpecifier, v.importingFile, v.program); comparison != 0 {
			return comparison
		}
	}
	if a.ModuleSpecifierKind == modulespecifiers.ResultKindRelative && b.ModuleSpecifierKind == modulespecifiers.ResultKindRelative {
		if comparison := core.CompareBooleans(isFixPossiblyReExportingImportingFile(a, v.importingFile.FileName()), isFixPossiblyReExportingImportingFile(b, v.importingFile.FileName())); comparison != 0 {
			return comparison
		}
	}
	if comparison := tspath.CompareNumberOfDirectorySeparators(a.ModuleSpecifier, b.ModuleSpecifier); comparison != 0 {
		return comparison
	}
	return 0
}
func (v *View) compareModuleSpecifiersForSorting(a, b *Fix) int {
	if res := v.compareModuleSpecifiersForRanking(a, b); res != 0 {
		return res
	}
	if strings.HasPrefix(a.ModuleSpecifier, "./") && !strings.HasPrefix(b.ModuleSpecifier, "./") {
		return -1
	}
	if strings.HasPrefix(b.ModuleSpecifier, "./") && !strings.HasPrefix(a.ModuleSpecifier, "./") {
		return 1
	}
	if comparison := strings.Compare(a.ModuleSpecifier, b.ModuleSpecifier); comparison != 0 {
		return comparison
	}
	if comparison := cmp.Compare(a.ImportKind, b.ImportKind); comparison != 0 {
		return comparison
	}
	return 0
}
func (v *View) compareNodeCoreModuleSpecifiers(a, b string, importingFile *ast.SourceFile, program *compiler.Program) int {
	if strings.HasPrefix(a, "node:") && !strings.HasPrefix(b, "node:") {
		if v.shouldUseUriStyleNodeCoreModules.IsTrue() {
			return -1
		} else if v.shouldUseUriStyleNodeCoreModules.IsFalse() {
			return 1
		}
		return 0
	}
	if strings.HasPrefix(b, "node:") && !strings.HasPrefix(a, "node:") {
		if v.shouldUseUriStyleNodeCoreModules.IsTrue() {
			return 1
		} else if v.shouldUseUriStyleNodeCoreModules.IsFalse() {
			return -1
		}
	}
	return 0
}

func isFixPossiblyReExportingImportingFile(fix *Fix, importingFileName string) bool {
	if fix.IsReExport && isIndexFileName(fix.ModuleFileName) {
		reExportDir := tspath.GetDirectoryPath(fix.ModuleFileName)
		return strings.HasPrefix(importingFileName, reExportDir)
	}
	return false
}
func isIndexFileName(fileName string) bool {
	lastSlash := strings.LastIndexByte(fileName, '/')
	if lastSlash < 0 || len(fileName) <= lastSlash+1 {
		return false
	}
	fileName = fileName[lastSlash+1:]
	switch fileName {
	case "index.js", "index.jsx", "index.d.ts", "index.ts", "index.tsx":
		return true
	}
	return false
}
func promoteFromTypeOnly(changes *change.Tracker, aliasDeclaration ast.Handle, compilerOptions *core.CompilerOptions, sourceFile *ast.SourceFile, preferences lsutil.UserPreferences) ast.Handle {
	convertExistingToTypeOnly := compilerOptions.VerbatimModuleSyntax
	switch aliasDeclaration.Kind {
	case ast.KindImportSpecifier:
		spec := aliasDeclaration
		if spec.IsTypeOnly() {
			if !spec.Parent().IsNil() && spec.Parent().Kind == ast.KindNamedImports {
				namedImportsNode := spec.Parent()
				elements := namedImportsNode.Elements()
				if len(elements) > 1 {
					var propertyName ast.Handle
					if !spec.PropertyName().IsNil() {
						propertyName = changes.HandleFactory.NewIdentifier(spec.PropertyName().Text())					}
					newSpecifier := changes.HandleFactory.NewImportSpecifier(false, propertyName, changes.HandleFactory.NewIdentifier(spec.Name().Text()))
					specifierComparer, _ := lsutil.GetNamedImportSpecifierComparerWithDetection(spec.Parent().Parent().Parent(), sourceFile, preferences)
					insertionIndex := lsutil.GetImportSpecifierInsertionIndex(elements, newSpecifier, specifierComparer)
					currentIndex := slices.Index(elements, aliasDeclaration)
					if insertionIndex != currentIndex {
						changes.Delete(sourceFile, aliasDeclaration)
						changes.InsertImportSpecifierAtIndex(sourceFile, newSpecifier, spec.Parent(), insertionIndex)
						return aliasDeclaration
					}
				}
				firstToken := lsutil.GetFirstToken(aliasDeclaration, sourceFile)
				typeKeywordPos := scanner.GetTokenPosOfNode(firstToken, sourceFile, false)
				var targetNode ast.Handle
				if !spec.PropertyName().IsNil() {
					targetNode = spec.PropertyName()
				} else {
					targetNode = spec.Name()
				}
				targetPos := scanner.GetTokenPosOfNode(targetNode, sourceFile, false)
				changes.DeleteRange(sourceFile, core.NewTextRange(typeKeywordPos, targetPos))
			}
			return aliasDeclaration
		} else {
			if spec.Parent().IsNil() || spec.Parent().Kind != ast.KindNamedImports {
				panic("ImportSpecifier parent must be NamedImports")
			}
			if spec.Parent().Parent().IsNil() || spec.Parent().Parent().Kind != ast.KindImportClause {
				panic("NamedImports parent must be ImportClause")
			}
			promoteImportClause(changes, spec.Parent().Parent(), compilerOptions, sourceFile, preferences, convertExistingToTypeOnly, aliasDeclaration)
			return spec.Parent().Parent()
		}
	case ast.KindImportClause:
		promoteImportClause(changes, aliasDeclaration, compilerOptions, sourceFile, preferences, convertExistingToTypeOnly, aliasDeclaration)
		return aliasDeclaration
	case ast.KindNamespaceImport:
		if aliasDeclaration.Parent().IsNil() || aliasDeclaration.Parent().Kind != ast.KindImportClause {
			panic("NamespaceImport parent must be ImportClause")
		}
		promoteImportClause(changes, aliasDeclaration.Parent(), compilerOptions, sourceFile, preferences, convertExistingToTypeOnly, aliasDeclaration)
		return aliasDeclaration.Parent()
	case ast.KindImportEqualsDeclaration:
		importEqDecl := aliasDeclaration
		scan := scanner.GetScannerForSourceFile(sourceFile, importEqDecl.Pos())
		scan.Scan()
		deleteTypeKeyword(changes, sourceFile, scan.TokenStart())
		return aliasDeclaration
	default:
		panic(fmt.Sprintf("Unexpected alias declaration kind: %v", aliasDeclaration.Kind))
	}
}

func promoteImportClause(changes *change.Tracker, importClause ast.Handle, compilerOptions *core.CompilerOptions, sourceFile *ast.SourceFile, preferences lsutil.UserPreferences, convertExistingToTypeOnly core.Tristate, aliasDeclaration ast.Handle) {
	if importClause.ImportClausePhaseModifier() == ast.KindTypeKeyword {
		deleteTypeKeyword(changes, sourceFile, importClause.Pos())
	}
	if compilerOptions.AllowImportingTsExtensions.IsFalse() {
		moduleSpecifier := checker.TryGetModuleSpecifierFromDeclaration(importClause.Parent())
		if !moduleSpecifier.IsNil() {
		}
	}
	if convertExistingToTypeOnly.IsTrue() {
		namedImports := importClause.NamedBindings()
		if !namedImports.IsNil() && namedImports.Kind == ast.KindNamedImports {
			namedImportsData := namedImports
			if len(namedImportsData.Elements()) > 1 {
				_, isSorted := lsutil.GetNamedImportSpecifierComparerWithDetection(importClause.Parent(), sourceFile, preferences)
				if isSorted.IsFalse() == false && !aliasDeclaration.IsNil() && aliasDeclaration.Kind == ast.KindImportSpecifier {
					aliasIndex := -1
					for i, element := range namedImportsData.Elements() {
						if element == aliasDeclaration {
							aliasIndex = i
							break
						}
					}
					if aliasIndex > 0 {
						changes.Delete(sourceFile, aliasDeclaration)
						changes.InsertImportSpecifierAtIndex(sourceFile, aliasDeclaration, namedImports, 0)
					}
				}
				for _, element := range namedImportsData.Elements() {
					spec := element
					if !aliasDeclaration.IsNil() && aliasDeclaration.Kind == ast.KindImportSpecifier {
						if element == aliasDeclaration {
							continue
						}
					}
					if !spec.IsTypeOnly() {
						changes.InsertModifierBefore(sourceFile, ast.KindTypeKeyword, element)
					}
				}
			}
		}
	}
}

func deleteTypeKeyword(changes *change.Tracker, sourceFile *ast.SourceFile, startPos int) {
	scan := scanner.GetScannerForSourceFile(sourceFile, startPos)
	if scan.Token() != ast.KindTypeKeyword {
		return
	}
	typeStart := scan.TokenStart()
	typeEnd := scan.TokenEnd()
	text := sourceFile.Text()
	for typeEnd < len(text) && (text[typeEnd] == ' ' || text[typeEnd] == '\t') {
		typeEnd++
	}
	changes.DeleteRange(sourceFile, core.NewTextRange(typeStart, typeEnd))
}
func getModuleSpecifierText(promotedDeclaration ast.Handle) string {
	if promotedDeclaration.Kind == ast.KindImportEqualsDeclaration {
		importEqualsDeclaration := promotedDeclaration
		if ast.IsExternalModuleReference(importEqualsDeclaration.ModuleReference()) {
			expr := importEqualsDeclaration.ModuleReference().Expression()
			if !expr.IsNil() {
				if ast.IsStringLiteralLike(expr) {
					return expr.Text()
				}
				return scanner.GetTextOfNode(expr)
			}
		}
		return scanner.GetTextOfNode(importEqualsDeclaration.ModuleReference())
	}
	moduleSpecifier := promotedDeclaration.Parent().ModuleSpecifier()
	if ast.IsStringLiteralLike(moduleSpecifier) {
		return moduleSpecifier.Text()
	}
	return scanner.GetTextOfNode(moduleSpecifier)
}

func compareModuleSpecifierRelativity(a *Fix, b *Fix, preferences modulespecifiers.UserPreferences) int {
	switch preferences.ImportModuleSpecifierPreference {
	case modulespecifiers.ImportModuleSpecifierPreferenceNonRelative, modulespecifiers.ImportModuleSpecifierPreferenceProjectRelative:
		return core.CompareBooleans(a.ModuleSpecifierKind == modulespecifiers.ResultKindRelative, b.ModuleSpecifierKind == modulespecifiers.ResultKindRelative)
	}
	return 0
}
