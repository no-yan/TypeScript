package ls

import (
	"context"
	"fmt"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/locale"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
)

func (l *LanguageService) ProvideCodeLenses(ctx context.Context, documentURI lsproto.DocumentUri) (lsproto.CodeLensResponse, error) {
	_, file := l.getProgramAndFile(documentURI)
	userPrefs := l.UserPreferences().CodeLens
	if !userPrefs.ReferencesCodeLensEnabled.IsTrue() && !userPrefs.ImplementationsCodeLensEnabled.IsTrue() {
		return lsproto.CodeLensResponse{}, nil
	}
	var result []*// Keeps track of the last symbol to avoid duplicating code lenses across overloads.
	// Don't include the declaration in the references count.
	// "Force" link support to be false so that we only get `Locations` back,
	// and don't include the "current" node in the results.
	// Always show on interfaces
	// TODO: ast.KindTypeAliasDeclaration?
	// If configured, show on interface methods
	// If configured, show on all class methods - but not private ones.
	// Always show on abstract classes/properties/methods
	// Don't show if child and parent have same start
	// For https://github.com/microsoft/vscode/issues/90396
	// !!!
	lsproto.CodeLens
	seen := collections.Set[codeLensKey]{}
	projections := append([]*ast.SourceFile{file}, file.SupplementalSourceFiles()...)
	for _, projection := range projections {
		var lastSymbol *ast.Symbol
		var visit func(node ast.Handle) bool
		visit = func(node ast.Handle) bool {
			if ctx.Err() != nil {
				return true
			}
			if currentSymbol := node.Symbol(); lastSymbol != currentSymbol {
				lastSymbol = currentSymbol
				if userPrefs.ReferencesCodeLensEnabled.IsTrue() && isValidReferenceLensNode(node, userPrefs) {
					if codeLens := l.newCodeLensForNode(documentURI, projection, node, lsproto.CodeLensKindReferences); codeLens != nil && seen.AddIfAbsent(keyForCodeLens(codeLens)) {
						result = append(result, codeLens)
					}
				}
				if userPrefs.ImplementationsCodeLensEnabled.IsTrue() && isValidImplementationsCodeLensNode(node, userPrefs) {
					if codeLens := l.newCodeLensForNode(documentURI, projection, node, lsproto.CodeLensKindImplementations); codeLens != nil && seen.AddIfAbsent(keyForCodeLens(codeLens)) {
						result = append(result, codeLens)
					}
				}
			}
			savedLastSymbol := lastSymbol
			node.ForEachChild(visit)
			lastSymbol = savedLastSymbol
			return false
		}
		visit(projection.ParseRoot())
	}
	return lsproto.CodeLensResponse{CodeLenses: &result}, nil
}

type codeLensKey struct {
	kind                                             lsproto.CodeLensKind
	startLine, startCharacter, endLine, endCharacter uint32
}

func keyForCodeLens(codeLens *lsproto.CodeLens) codeLensKey {
	return codeLensKey{kind: codeLens.Data.Kind, startLine: codeLens.Range.Start.Line, startCharacter: codeLens.Range.Start.Character, endLine: codeLens.Range.End.Line, endCharacter: codeLens.Range.End.Character}
}
func (l *LanguageService) ResolveCodeLens(ctx context.Context, codeLens *lsproto.CodeLens, showLocationsCommandName *string, orchestrator CrossProjectOrchestrator) (*lsproto.CodeLens, error) {
	uri := codeLens.Data.Uri
	textDoc := lsproto.TextDocumentIdentifier{Uri: uri}
	program, file := l.getProgramAndFile(uri)
	file = sourceFileForSupplementalFileIndex(file, codeLens.Data.SupplementalFileIndex)
	if file == nil {
		return nil, fmt.Errorf("supplemental source file index not found: %d", *codeLens.Data.SupplementalFileIndex)
	}
	locale := locale.FromContext(ctx)
	var locs []lsproto.Location
	var lensTitle string
	switch codeLens.Data.Kind {
	case lsproto.CodeLensKindReferences:
		data, _ := l.provideSymbolsAndEntriesAtPosition(ctx, program, file, int(codeLens.Data.Position), false, false)
		referencesResp, err := l.provideReferencesFromData(ctx, &lsproto.ReferenceParams{TextDocument: textDoc, Position: codeLens.Range.Start, Context: &lsproto.ReferenceContext{IncludeDeclaration: false}}, orchestrator, data)
		if err != nil {
			return nil, err
		}
		if referencesResp.Locations != nil {
			locs = *referencesResp.Locations
		}
		if len(locs) == 1 {
			lensTitle = diagnostics.X_1_reference.Localize(locale)
		} else {
			lensTitle = diagnostics.X_0_references.Localize(locale, len(locs))
		}
	case lsproto.CodeLensKindImplementations:
		data, _ := l.provideSymbolsAndEntriesAtPosition(ctx, program, file, int(codeLens.Data.Position), false, true)
		implementations, err := l.provideImplementationsFromData(ctx, &lsproto.ImplementationParams{TextDocument: textDoc, Position: codeLens.Range.Start}, symbolEntryTransformOptions{requireLocationsResult: true, dropOriginNodes: true}, orchestrator, data)
		if err != nil {
			return nil, err
		}
		if implementations.Locations != nil {
			locs = *implementations.Locations
		}
		if len(locs) == 1 {
			lensTitle = diagnostics.X_1_implementation.Localize(locale)
		} else {
			lensTitle = diagnostics.X_0_implementations.Localize(locale, len(locs))
		}
	}
	cmd := &lsproto.Command{Title: lensTitle}
	if len(locs) > 0 && showLocationsCommandName != nil {
		cmd.Command = *showLocationsCommandName
		cmd.Arguments = &[]any{uri, codeLens.Range.Start, locs}
	}
	codeLens.Command = cmd
	return codeLens, nil
}
func (l *LanguageService) newCodeLensForNode(fileUri lsproto.DocumentUri, file *ast.SourceFile, node ast.Handle, kind lsproto.CodeLensKind) *lsproto.CodeLens {
	nodeForRange := node
	nodeName := node.Name()
	if !nodeName.IsNil() {
		nodeForRange = nodeName
	}
	pos := scanner.SkipTrivia(file.Text(), nodeForRange.Pos())
	lspRange, fidelity := l.converters.ToLSPRangeForFeature(file, core.NewTextRange(pos, node.End()), spanmap.FeatureCodeLens)
	if fidelity.IsNone() {
		return nil
	}
	return &lsproto.CodeLens{Range: lspRange, Data: &lsproto.CodeLensData{Kind: kind, Uri: fileUri, Position: int32(pos), SupplementalFileIndex: supplementalFileIndex(file)}}
}
func isValidImplementationsCodeLensNode(node ast.Handle, userPrefs lsutil.CodeLensUserPreferences) bool {
	switch node.Kind() {
	case ast.KindInterfaceDeclaration:
		return true
	case ast.KindMethodSignature:
		return userPrefs.ImplementationsCodeLensShowOnInterfaceMethods.IsTrue() && node.Parent().Kind() == ast.KindInterfaceDeclaration
	case ast.KindMethodDeclaration:
		if userPrefs.ImplementationsCodeLensShowOnAllClassMethods.IsTrue() && node.Parent().Kind() == ast.KindClassDeclaration {
			return !ast.HasModifier(node, ast.ModifierFlagsPrivate) && node.Name().Kind() != ast.KindPrivateIdentifier
		}
		fallthrough
	case ast.KindClassDeclaration, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindPropertyDeclaration:
		return ast.HasModifier(node, ast.ModifierFlagsAbstract)
	}
	return false
}
func isValidReferenceLensNode(node ast.Handle, userPrefs lsutil.CodeLensUserPreferences) bool {
	switch node.Kind() {
	case ast.KindFunctionDeclaration:
		if userPrefs.ReferencesCodeLensShowOnAllFunctions.IsTrue() {
			return true
		}
		fallthrough
	case ast.KindVariableDeclaration:
		return ast.GetCombinedModifierFlags(node)&ast.ModifierFlagsExport != 0
	case ast.KindClassDeclaration, ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration, ast.KindEnumDeclaration, ast.KindEnumMember:
		return true
	case ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindPropertyDeclaration, ast.KindPropertySignature:
		switch node.Parent().Kind() {
		case ast.KindClassDeclaration, ast.KindInterfaceDeclaration, ast.KindTypeLiteral:
			return true
		}
	}
	return false
}
