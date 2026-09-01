package ls

import (
	"context"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/locale"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsconv"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"slices"
	"strings"
)

type RenameInfo struct {
	CanRename             bool
	LocalizedErrorMessage string
	DisplayName           string
	TriggerSpan           lsproto.Range
	FileToRename          string
	NewFileName           string
}
type mappedRenameEdit struct {
	uri  lsproto.DocumentUri
	edit *lsproto.TextEdit
}
type renameEditKey struct {
	uri       lsproto.DocumentUri
	textRange lsproto.Range
}

func deduplicateRenameEdits(mappedEdits []mappedRenameEdit) (// RenameInfo represents the result of a rename validation check.
// It is used by the `textDocument/prepareRename` LSP handler.
/*isRename*/ /*implementations*/ /*defaultProjectData*/ /*forRename*/ // Defense-in-depth: validate rename eligibility even if the client skipped prepareRename.
// Use getRenameInfoForNode directly with the already-resolved node to avoid
// re-resolving the position and polluting state baselines.
// The occurrence lies outside a verbatim span of a content-mapped file, so it cannot be
// written back to the original text. Skip it and keep renaming the remaining occurrences.
// renameEditRange returns the LSP range at which a rename occurrence should be edited. For occurrences in
// content-mapped files it maps the transformed range strictly, returning ok=false when the occurrence is
// not fully within a single verbatim span, so the caller can skip an edit that cannot be applied to the
// original text.
// getRenameInfoForNode performs detailed validation for a rename operation on a specific node.
// Allow renaming of string literal types with contextual string literal types
// Only allow a symbol to be renamed if it actually has at least one declaration.
// renameBlockedReason returns a non-nil diagnostic message if the rename should be blocked
// because the symbol is a library definition, a default keyword, or would cross node_modules boundaries.
// Cannot rename `default` as in `import { default as foo } from "./someModule"`
// isDefinedInLibraryFile checks if a declaration is from a default library file (e.g., lib.d.ts).
// wouldRenameInOtherNodeModules checks if renaming the symbol would affect node_modules.
/*isFolder*/ // Original source file is not in node_modules.
// Original source file is in node_modules.
/*isFolder*/ // getRenameInfoForModule handles rename validation for module specifiers.
// Span should only be the last component of the path. + 1 to account for the quote character.
/*includeJSDoc*/ // Adjust the new name based on the old path that an import specifier resolves to.
// For example, if specifier "a.js" resolves to file a.ts, renaming "a.js" -> "b.js" should mean file rename a.ts -> b.ts.
/*extensions*/ /*extensions*/ /*extensions*/ /*extensions*/ // In `const o = { x }; o.x`, symbolAtLocation at `x` in `{ x }` is the property symbol.
// For a binding element `const { x } = o;`, symbolAtLocation at `x` is the property symbol.
// If the original symbol was using this alias, just rename the alias.
// If the symbol for the node is same as declared node symbol use prefix text
// If the node is a numerical indexing literal, then add quotes around the property access.
/*includeJSDoc*/ // Exclude the quotes
map[lsproto.DocumentUri][]*lsproto.TextEdit, bool) {
	editTexts := make(map[renameEditKey]string)
	uniqueEdits := make([]mappedRenameEdit, 0, len(mappedEdits))
	for _, mappedEdit := range mappedEdits {
		key := renameEditKey{uri: mappedEdit.uri, textRange: mappedEdit.edit.Range}
		if existingText, ok := editTexts[key]; ok {
			if existingText != mappedEdit.edit.NewText {
				return nil, false
			}
			continue
		}
		editTexts[key] = mappedEdit.edit.NewText
		uniqueEdits = append(uniqueEdits, mappedEdit)
	}
	changes := make(map[lsproto.DocumentUri][]*lsproto.TextEdit)
	for _, mappedEdit := range uniqueEdits {
		changes[mappedEdit.uri] = append(changes[mappedEdit.uri], mappedEdit.edit)
	}
	return changes, true
}
func (l *LanguageService) ProvideRename(ctx context.Context, params *lsproto.RenameParams, orchestrator CrossProjectOrchestrator) (lsproto.WorkspaceEditOrNull, error) {
	return handleCrossProject(l, ctx, params, orchestrator, (*LanguageService).symbolAndEntriesToRename, combineRenameResponse, true, false, symbolEntryTransformOptions{}, nil)
}
func (l *LanguageService) GetRenameInfo(ctx context.Context, newName string, documentURI lsproto.DocumentUri, position lsproto.Position) RenameInfo {
	program, sourceFile := l.getProgramAndFile(documentURI)
	positions := lsconv.FromLSPPositionForSourceFile(l.converters, sourceFile, position, spanmap.FeatureRename)
	for _, mapped := range positions {
		if !mapped.Fidelity.IsExact() {
			continue
		}
		sourceFile := mapped.Script
		node := astnav.GetTouchingPropertyName(sourceFile, int(mapped.Position))
		node = getAdjustedLocation(node, true, sourceFile)
		if nodeIsEligibleForRename(node) {
			if renameInfo, ok := l.getRenameInfoForNode(ctx, newName, node, sourceFile, program); ok {
				return renameInfo
			}
		}
	}
	return getRenameInfoError(ctx, diagnostics.You_cannot_rename_this_element)
}
func (l *LanguageService) symbolAndEntriesToRename(ctx context.Context, params *lsproto.RenameParams, data SymbolAndEntriesData, options symbolEntryTransformOptions) (lsproto.WorkspaceEditOrNull, error) {
	if !nodeIsEligibleForRename(data.OriginalNode) {
		return lsproto.WorkspaceEditOrNull{}, nil
	}
	program := l.GetProgram()
	sourceFile := ast.GetSourceFileOfNode(data.OriginalNode)
	if info, ok := l.getRenameInfoForNode(ctx, params.NewName, data.OriginalNode, sourceFile, program); !ok || !info.CanRename {
		return lsproto.WorkspaceEditOrNull{}, nil
	}
	entries := core.FlatMap(data.SymbolsAndEntries, func(s *SymbolAndEntries) []*ReferenceEntry {
		return s.references
	})
	var mappedEdits []mappedRenameEdit
	ch, done := program.GetTypeChecker(ctx)
	defer done()
	quotePreference := lsutil.GetQuotePreference(sourceFile, l.UserPreferences())
	useAliasesForRename := l.UserPreferences().UseAliasesForRename.IsTrueOrUnknown()
	for _, entry := range entries {
		uri := l.getFileNameOfEntry(entry)
		if l.UserPreferences().AllowRenameOfImportPath != core.TSTrue && !entry.node.IsNil() && ast.IsStringLiteralLike(entry.node) && !ast.TryGetImportFromModuleSpecifier(entry.node).IsNil() {
			continue
		}
		rng, ok := l.renameEditRange(entry)
		if !ok {
			continue
		}
		textEdit := &lsproto.TextEdit{Range: rng, NewText: l.getTextForRename(data.OriginalNode, entry, params.NewName, ch, quotePreference, useAliasesForRename)}
		mappedEdits = append(mappedEdits, mappedRenameEdit{uri: uri, edit: textEdit})
	}
	changes, ok := deduplicateRenameEdits(mappedEdits)
	if !ok {
		return lsproto.WorkspaceEditOrNull{}, nil
	}
	return lsproto.WorkspaceEditOrNull{WorkspaceEdit: &lsproto.WorkspaceEdit{Changes: &changes}}, nil
}

func (l *LanguageService) renameEditRange(entry *ReferenceEntry) (lsproto.Range, bool) {
	l.resolveEntry(entry)
	if entry.node.IsNil() {
		location, fidelity := l.sourceFileRangeToLSPLocation(entry.sourceFile, *entry.textRange)
		return location.Range, fidelity.IsExact()
	}
	sourceFile := ast.GetSourceFileOfNode(entry.node)
	if sourceFile == nil || sourceFile.SpanMap() == nil {
		return l.getRangeOfEntry(entry), true
	}
	lspRange, fidelity := l.converters.ToLSPRange(sourceFile, *entry.textRange)
	return lspRange, fidelity.IsExact()
}

func (l *LanguageService) getRenameInfoForNode(ctx context.Context, newName string, node ast.Handle, sourceFile *ast.SourceFile, program *compiler.Program) (RenameInfo, bool) {
	ch, done := program.GetTypeChecker(ctx)
	defer done()
	symbol := ch.GetSymbolAtLocation(node)
	if symbol == nil {
		if ast.IsStringLiteralLike(node) {
			typ := getContextualTypeFromParentOrAncestorTypeNode(node, ch)
			if typ != nil && (typ.IsStringLiteral() || (typ.IsUnion() && core.Every(typ.Types(), func(t *checker.Type) bool {
				return t.IsStringLiteral()
			}))) {
				return getRenameInfoSuccess(node, sourceFile, node.Text(), l.converters), true
			}
		} else if ast.IsLabelName(node) {
			name := node.Text()
			return getRenameInfoSuccess(node, sourceFile, name, l.converters), true
		}
		return RenameInfo{}, false
	}
	if len(symbol.Declarations) == 0 {
		return RenameInfo{}, false
	}
	if msg := l.renameBlockedReason(sourceFile, node, symbol, ch, program); msg != nil {
		return getRenameInfoError(ctx, msg), true
	}
	if ast.IsStringLiteralLike(node) && !ast.TryGetImportFromModuleSpecifier(node).IsNil() {
		if l.UserPreferences().AllowRenameOfImportPath.IsTrue() {
			return l.getRenameInfoForModule(ctx, newName, node, sourceFile, symbol)
		}
		return RenameInfo{}, false
	}
	return getRenameInfoSuccess(node, sourceFile, ch.SymbolToString(symbol), l.converters), true
}
func nodeIsEligibleForRename(node ast.Handle) bool {
	if node.IsNil() {
		return false
	}
	switch node.Kind() {
	case ast.KindIdentifier, ast.KindPrivateIdentifier, ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral, ast.KindThisKeyword:
		return true
	case ast.KindNumericLiteral:
		return isLiteralNameOfPropertyDeclarationOrIndexAccess(node)
	default:
		return false
	}
}

func (l *LanguageService) renameBlockedReason(sourceFile *ast.SourceFile, node ast.Handle, symbol *ast.Symbol, ch *checker.Checker, program *compiler.Program) *diagnostics.Message {
	for _, declaration := range ast.DeclarationNodes(symbol) {
		if isDefinedInLibraryFile(program, declaration) {
			return diagnostics.You_cannot_rename_elements_that_are_defined_in_the_standard_TypeScript_library
		}
	}
	if ast.IsIdentifier(node) && node.Text() == "default" && symbol.Parent != nil && symbol.Parent.Flags&ast.SymbolFlagsModule != 0 {
		return diagnostics.You_cannot_rename_this_element
	}
	if msg := wouldRenameInOtherNodeModules(sourceFile, symbol, ch, l.UserPreferences()); msg != nil {
		return msg
	}
	return nil
}

func isDefinedInLibraryFile(program *compiler.Program, declaration ast.Handle) bool {
	declSourceFile := ast.GetSourceFileOfNode(declaration)
	return program.IsSourceFileDefaultLibrary(declSourceFile.Path()) && tspath.IsDeclarationFileName(declSourceFile.FileName())
}

func wouldRenameInOtherNodeModules(originalFile *ast.SourceFile, symbol *ast.Symbol, ch *checker.Checker, preferences lsutil.UserPreferences) *diagnostics.Message {
	sym := symbol
	if !preferences.UseAliasesForRename.IsTrueOrUnknown() && sym.Flags&ast.SymbolFlagsAlias != 0 {
		importSpecifier := ast.FindSymbolDeclaration(sym, ast.IsImportSpecifier)
		if !importSpecifier.IsNil() && importSpecifier.ImportSpecifierPropertyName().IsNil() {
			sym = ch.GetAliasedSymbol(sym)
		}
	}
	declarations := ast.DeclarationNodes(sym)
	if len(declarations) == 0 {
		return nil
	}
	originalPackage := module.ParseNodeModuleFromPath(originalFile.FileName(), false)
	if originalPackage == "" {
		for _, declaration := range declarations {
			if isInsideNodeModules(ast.GetSourceFileOfNode(declaration).FileName()) {
				return diagnostics.You_cannot_rename_elements_that_are_defined_in_a_node_modules_folder
			}
		}
		return nil
	}
	for _, declaration := range declarations {
		declPackage := module.ParseNodeModuleFromPath(ast.GetSourceFileOfNode(declaration).FileName(), false)
		if declPackage != "" && declPackage != originalPackage {
			return diagnostics.You_cannot_rename_elements_that_are_defined_in_another_node_modules_folder
		}
	}
	return nil
}
func ClientSupportsWillRenameFiles(ctx context.Context) bool {
	return lsproto.GetClientCapabilities(ctx).Workspace.FileOperations.WillRename
}
func ClientSupportsDocumentChanges(ctx context.Context) bool {
	return lsproto.GetClientCapabilities(ctx).Workspace.WorkspaceEdit.DocumentChanges
}
func ClientSupportsRenameResourceOperations(ctx context.Context) bool {
	return slices.Contains(lsproto.GetClientCapabilities(ctx).Workspace.WorkspaceEdit.ResourceOperations, lsproto.ResourceOperationKindRename)
}

func (l *LanguageService) getRenameInfoForModule(ctx context.Context, newName string, specifier ast.Handle, sourceFile *ast.SourceFile, moduleSymbol *ast.Symbol) (RenameInfo, bool) {
	if !tspath.IsExternalModuleNameRelative(specifier.Text()) {
		return getRenameInfoError(ctx, diagnostics.You_cannot_rename_a_module_via_a_global_import), true
	}
	if !ClientSupportsDocumentChanges(ctx) || !ClientSupportsRenameResourceOperations(ctx) {
		return getRenameInfoError(ctx, diagnostics.File_rename_is_not_supported_by_the_editor), true
	}
	moduleSourceFile := ast.FindSymbolDeclaration(moduleSymbol, ast.IsSourceFile)
	if moduleSourceFile.IsNil() {
		return RenameInfo{}, false
	}
	fileName := ast.GetSourceFileOfNode(moduleSourceFile).FileName()
	withoutIndex := ""
	if !strings.HasSuffix(specifier.Text(), "/index") && !strings.HasSuffix(specifier.Text(), "/index.js") {
		candidate := tspath.RemoveFileExtension(fileName)
		if trimmed, ok := strings.CutSuffix(candidate, "/index"); ok {
			withoutIndex = trimmed
		}
	}
	displayName := fileName
	if withoutIndex != "" {
		displayName = withoutIndex
	}
	newFileName := l.getNewFileNameForModuleRename(displayName, specifier.Text(), newName)
	indexAfterLastSlash := strings.LastIndex(specifier.Text(), "/") + 1
	start := astnav.GetStartOfNode(specifier, sourceFile, false) + 1 + indexAfterLastSlash
	length := len(specifier.Text()) - indexAfterLastSlash
	triggerSpan, fidelity := l.converters.ToLSPRange(sourceFile, core.NewTextRange(start, start+length))
	if !fidelity.IsExact() {
		return RenameInfo{}, false
	}
	return RenameInfo{CanRename: true, DisplayName: specifier.Text()[indexAfterLastSlash:], TriggerSpan: triggerSpan, FileToRename: displayName, NewFileName: newFileName}, true
}

func (l *LanguageService) getNewFileNameForModuleRename(oldPath, specifierText, newName string) string {
	newPath := tspath.CombinePaths(tspath.GetDirectoryPath(oldPath), newName)
	ignoreCase := !l.host.UseCaseSensitiveFileNames()
	var oldExt string
	if tspath.IsDeclarationFileName(oldPath) {
		oldExt = tspath.GetDeclarationFileExtension(oldPath)
	} else {
		oldExt = tspath.GetAnyExtensionFromPath(oldPath, nil, ignoreCase)
	}
	if !tspath.HasExtension(newPath) {
		newPath = newPath + oldExt
	} else if tspath.GetAnyExtensionFromPath(newPath, nil, ignoreCase) == tspath.GetAnyExtensionFromPath(specifierText, nil, ignoreCase) {
		newPath = tspath.ChangeAnyExtension(newPath, oldExt, nil, ignoreCase)
	}
	return newPath
}
func (l *LanguageService) getTextForRename(originalNode ast.Handle, entry *ReferenceEntry, newText string, ch *checker.Checker, quotePreference lsutil.QuotePreference, useAliasesForRename bool) string {
	if useAliasesForRename && entry.kind != entryKindRange && (ast.IsIdentifier(originalNode) || ast.IsStringLiteralLike(originalNode)) {
		node := ast.GetReparsedHandle(entry.node)
		kind := entry.kind
		parent := node.Parent()
		name := originalNode.Text()
		isShorthandAssignment := ast.IsShorthandPropertyAssignment(parent)
		switch {
		case isShorthandAssignment || (isObjectBindingElementWithoutPropertyName(parent) && parent.Name() == node && parent.BindingElementDotDotDotToken().IsNil()):
			if kind == entryKindSearchedLocalFoundProperty {
				return name + ": " + newText
			}
			if kind == entryKindSearchedPropertyFoundLocal {
				return newText + ": " + name
			}
			if isShorthandAssignment {
				grandParent := parent.Parent()
				if ast.IsObjectLiteralExpression(grandParent) && ast.IsBinaryExpression(grandParent.Parent()) && ast.IsModuleExportsAccessExpression(grandParent.Parent().BinaryExpressionLeft()) {
					return name + ": " + newText
				}
				return newText + ": " + name
			}
			return name + ": " + newText
		case ast.IsImportSpecifier(parent) && parent.PropertyName().IsNil():
			var originalSymbol *ast.Symbol
			if ast.IsExportSpecifier(originalNode.Parent()) {
				originalSymbol = ch.GetExportSpecifierLocalTargetSymbol(originalNode.Parent())
			} else {
				originalSymbol = ch.GetSymbolAtLocation(originalNode)
			}
			if originalSymbol != nil && slices.Contains(ast.DeclarationNodes(originalSymbol), parent) {
				return name + " as " + newText
			}
			return newText
		case ast.IsExportSpecifier(parent) && parent.PropertyName().IsNil():
			if originalNode == entry.node || ch.GetSymbolAtLocation(originalNode) == ch.GetSymbolAtLocation(entry.node) {
				return name + " as " + newText
			}
			return newText + " as " + name
		}
	}
	if entry.kind != entryKindRange && ast.IsNumericLiteral(entry.node) && ast.IsAccessExpression(entry.node.Parent()) {
		quote := getQuoteFromPreference(quotePreference)
		return quote + newText + quote
	}
	return newText
}
func getQuoteFromPreference(quotePreference lsutil.QuotePreference) string {
	if quotePreference == lsutil.QuotePreferenceSingle {
		return "'"
	}
	return `"`
}
func getRenameInfoError(ctx context.Context, message *diagnostics.Message) RenameInfo {
	return RenameInfo{CanRename: false, LocalizedErrorMessage: message.Localize(locale.FromContext(ctx))}
}
func getRenameInfoSuccess(node ast.Handle, sourceFile *ast.SourceFile, displayName string, converters *lsconv.Converters) RenameInfo {
	start := astnav.GetStartOfNode(node, sourceFile, false)
	end := node.End()
	if ast.IsStringLiteralLike(node) {
		start++
		end--
	}
	triggerSpan, fidelity := converters.ToLSPRange(sourceFile, core.NewTextRange(start, end))
	if !fidelity.IsExact() {
		return RenameInfo{CanRename: false}
	}
	return RenameInfo{CanRename: true, DisplayName: displayName, TriggerSpan: triggerSpan}
}
