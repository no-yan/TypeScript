package ls

import (
	"context"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsconv"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (l *LanguageService) ProvideDocumentSymbols(ctx context.Context, documentURI lsproto.DocumentUri) (lsproto.DocumentSymbolResponse, error) {
	_, file := l.getProgramAndFile(documentURI)
	projections := append([]*// Client doesn't support hierarchical document symbols, return flat SymbolInformation array
	// getDocumentSymbolInformations converts hierarchical DocumentSymbols to a flat SymbolInformation array
	// First get hierarchical symbols
	// Flatten the hierarchy
	// Recursively flatten children with this symbol as container
	/*name*/ /*children*/ /*name*/ /*name*/ /*name*/ /*name*/ /*children*/ /*name*/ /*name*/ /*children*/ /*name*/ /*children*/ // Handle default import case e.g.:
	//    import d from "mod";
	/*children*/ // Handle named bindings in imports e.g.:
	//    import * as NS from "mod";
	//    import {a, b as B} from "mod";
	/*name*/ /*children*/ /*name*/ /*children*/ // `module.exports = ...`` should be reparsed into a JSExportAssignment,
	// and `exports.a = ...`` into a CommonJSExport.
	// `A.b = ... ` or `A.prototype.b = ...`
	// `A.b` or `A.prototype.b`
	// `A["b"]` or `A.prototype["b"]`
	// `Object.defineProperty(A, "b", {...})`
	// If we see a prototype assignment, start tracking the target as an expando target.
	/*name*/ // Target is `f.prototype`.
	// Merges expando symbols into their target symbols, and namespaces of same name.
	// Modifies the input slice.
	// Collect symbols that can be an expando target.
	// Collect namespaces.
	// Anonymous symbols never merge.
	// Merge expandos.
	// Mark this symbol as merged.
	// Merge namespaces.
	// Mark this symbol as merged.
	// See `getUnnamedNodeLabel`.
	// Obtain set of non-declaration source files from all active programs.
	// Create DeclarationInfos for all declarations in the source files.
	// Sort the DeclarationInfos and return the top 256 matches.
	// Use the name node's span so that VS selects just the symbol name (matching
	// the TS5 navto behaviour). GetNameOfDeclaration is always non-nil here because
	// computeDeclarationMap only adds declarations whose GetDeclarationName (string
	// form) is non-empty, which implies a name node exists.
	/*includeJsDoc*/ // The name has no counterpart in the original text, so there is nothing to navigate to.
	// Return a score for matching `s` against `pattern`. In order to match, `s` must contain each of the characters in
	// `pattern` in the same order. Upper case characters in `pattern` must match exactly, whereas lower case characters
	// in `pattern` match either case in `s`. If `s` doesn't match, -1 is returned. Otherwise, the returned score is the
	// number of characters in `s` that weren't matched. Thus, zero represents an exact match, and higher values represent
	// increasingly less specific partial matches.
	// Sort DeclarationInfos by ascending match score, then ascending case insensitive name, then
	// ascending case sensitive name, and finally by source file name and position.
	// getSymbolKindFromNode converts an AST node to an LSP SymbolKind.
	// Combines getNodeKind with VS Code's fromProtocolScriptElementKind.
	// String literals used as property names (e.g., in Object.defineProperty)
	ast.SourceFile{file}, file.SupplementalSourceFiles()...)
	var symbols []*lsproto.DocumentSymbol
	var seen collections.Set[struct {
		name string
		kind lsproto.SymbolKind
		rng  lsproto.Range
	}]
	for _, projection := range projections {
		for _, symbol := range l.getDocumentSymbolsForChildren(ctx, projection.ParseRoot(), projection) {
			key := struct {
				name string
				kind lsproto.SymbolKind
				rng  lsproto.Range
			}{symbol.Name, symbol.Kind, symbol.Range}
			if seen.AddIfAbsent(key) {
				symbols = append(symbols, symbol)
			}
		}
	}
	if lsproto.GetClientCapabilities(ctx).TextDocument.DocumentSymbol.HierarchicalDocumentSymbolSupport {
		return lsproto.SymbolInformationsOrDocumentSymbolsOrNull{DocumentSymbols: &symbols}, nil
	}
	symbolInfos := flattenDocumentSymbols(symbols, documentURI)
	symbolInfoPtrs := make([]*lsproto.SymbolInformation, len(symbolInfos))
	for i := range symbolInfos {
		symbolInfoPtrs[i] = &symbolInfos[i]
	}
	return lsproto.SymbolInformationsOrDocumentSymbolsOrNull{SymbolInformations: &symbolInfoPtrs}, nil
}

func (l *LanguageService) getDocumentSymbolInformations(ctx context.Context, file *ast.SourceFile, documentURI lsproto.DocumentUri) []lsproto.SymbolInformation {
	docSymbols := l.getDocumentSymbolsForChildren(ctx, file.ParseRoot(), file)
	return flattenDocumentSymbols(docSymbols, documentURI)
}
func flattenDocumentSymbols(docSymbols []*lsproto.DocumentSymbol, documentURI lsproto.DocumentUri) []lsproto.SymbolInformation {
	var result []lsproto.SymbolInformation
	var flatten func(symbols []*lsproto.DocumentSymbol, containerName *string)
	flatten = func(symbols []*lsproto.DocumentSymbol, containerName *string) {
		for _, symbol := range symbols {
			info := lsproto.SymbolInformation{Name: symbol.Name, Kind: symbol.Kind, Location: lsproto.Location{Uri: documentURI, Range: symbol.Range}, ContainerName: containerName, Tags: symbol.Tags, Deprecated: symbol.Deprecated}
			result = append(result, info)
			if symbol.Children != nil && len(*symbol.Children) > 0 {
				flatten(*symbol.Children, &symbol.Name)
			}
		}
	}
	flatten(docSymbols, nil)
	return result
}
func (l *LanguageService) getDocumentSymbolsForChildren(ctx context.Context, node ast.Handle, file *ast.SourceFile) []*lsproto.DocumentSymbol {
	var symbols []*lsproto.DocumentSymbol
	expandoTargets := collections.Set[string]{}
	addSymbolForNode := func(node ast.Handle, name ast.Handle, children []*lsproto.DocumentSymbol) {
		if node.Flags()&ast.NodeFlagsReparsed == 0 {
			symbol := l.newDocumentSymbol(node, name, children)
			if symbol != nil {
				symbols = append(symbols, symbol)
			}
		}
	}
	var visit func(ast.Handle) bool
	getSymbolsForChildren := func(node ast.Handle) []*lsproto.DocumentSymbol {
		var result []*lsproto.DocumentSymbol
		if !node.IsNil() {
			saveExpandoTargets := expandoTargets
			expandoTargets = collections.Set[string]{}
			saveSymbols := symbols
			symbols = nil
			node.ForEachChild(visit)
			result = symbols
			symbols = saveSymbols
			expandoTargets = saveExpandoTargets
		}
		return result
	}
	startNode := func(node ast.Handle, name ast.Handle) func() {
		if node.IsNil() {
			return func() {
			}
		}
		saveExpandoTargets := expandoTargets
		expandoTargets = collections.Set[string]{}
		saveSymbols := symbols
		symbols = nil
		return func() {
			result := symbols
			symbols = saveSymbols
			expandoTargets = saveExpandoTargets
			addSymbolForNode(node, name, result)
		}
	}
	getSymbolsForNode := func(node ast.Handle) []*lsproto.DocumentSymbol {
		var result []*lsproto.DocumentSymbol
		if !node.IsNil() {
			saveSymbols := symbols
			symbols = nil
			visit(node)
			result = symbols
			symbols = saveSymbols
		}
		return result
	}
	visit = func(node ast.Handle) bool {
		if ctx.Err() != nil {
			return true
		}
		if node.Flags()&ast.NodeFlagsReparsed == 0 {
			if jsdocs := node.JSDoc(file); len(jsdocs) > 0 {
				for _, jsdoc := range jsdocs {
					if tagList := jsdoc.JSDocTags(); tagList != 0 {
						for _, tag := range node.Store().ListSlice(tagList) {
							if ast.IsJSDocTypedefTag(tag) || ast.IsJSDocCallbackTag(tag) {
								addSymbolForNode(tag, ast.Handle{}, nil)
							}
						}
					}
				}
			}
		}
		switch node.Kind {
		case ast.KindClassDeclaration, ast.KindClassExpression, ast.KindInterfaceDeclaration, ast.KindEnumDeclaration:
			if ast.IsClassLike(node) && ast.GetDeclarationName(node) != "" {
				expandoTargets.Add(ast.GetDeclarationName(node))
			}
			addSymbolForNode(node, ast.Handle{}, getSymbolsForChildren(node))
		case ast.KindModuleDeclaration:
			addSymbolForNode(node, ast.Handle{}, getSymbolsForChildren(getInteriorModule(node)))
		case ast.KindConstructor:
			addSymbolForNode(node, ast.Handle{}, getSymbolsForChildren(node.Body()))
			for _, param := range node.Parameters() {
				if ast.IsParameterPropertyDeclaration(param, node) {
					addSymbolForNode(param, ast.Handle{}, nil)
				}
			}
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
			declName := ast.GetDeclarationName(node)
			if declName != "" {
				expandoTargets.Add(declName)
			}
			addSymbolForNode(node, ast.Handle{}, getSymbolsForChildren(node.Body()))
		case ast.KindVariableDeclaration, ast.KindBindingElement, ast.KindPropertyAssignment, ast.KindPropertyDeclaration:
			nodeName := node.Name()
			if !nodeName.IsNil() {
				if ast.IsBindingPattern(nodeName) {
					visit(nodeName)
				} else {
					addSymbolForNode(node, ast.Handle{}, getSymbolsForChildren(node.Initializer()))
				}
			}
		case ast.KindSpreadAssignment:
			addSymbolForNode(node, node.Expression(), nil)
		case ast.KindMethodSignature, ast.KindPropertySignature, ast.KindCallSignature, ast.KindConstructSignature, ast.KindIndexSignature, ast.KindEnumMember, ast.KindShorthandPropertyAssignment, ast.KindTypeAliasDeclaration, ast.KindImportEqualsDeclaration, ast.KindExportSpecifier:
			addSymbolForNode(node, ast.Handle{}, nil)
		case ast.KindImportClause:
			if !node.Name().IsNil() {
				addSymbolForNode(node.Name(), node.Name(), nil)
			}
			if namedBindings := node.ImportClauseNamedBindings(); !namedBindings.IsNil() {
				if namedBindings.Kind == ast.KindNamespaceImport {
					addSymbolForNode(namedBindings, ast.Handle{}, nil)
				} else {
					for _, element := range namedBindings.Elements() {
						addSymbolForNode(element, ast.Handle{}, nil)
					}
				}
			}
		case ast.KindBinaryExpression, ast.KindCallExpression:
			assignmentKind := ast.GetAssignmentDeclarationKind(node)
			switch assignmentKind {
			case ast.JSDeclarationKindNone, ast.JSDeclarationKindThisProperty, ast.JSDeclarationKindModuleExports, ast.JSDeclarationKindExportsProperty, ast.JSDeclarationKindObjectDefinePropertyExports:
				node.ForEachChild(visit)
			case ast.JSDeclarationKindProperty, ast.JSDeclarationKindObjectDefinePropertyValue:
				var target ast.Handle
				var targetFunction ast.Handle
				var definition ast.Handle
				var propertyName ast.Handle
				if ast.IsBinaryExpression(node) {
					binaryExpr := node
					target = binaryExpr.Left()
					targetFunction = target.Expression()
					definition = binaryExpr.Right()
					if ast.IsPropertyAccessExpression(target) {
						propertyName = target.PropertyAccessExpressionName()
					} else {
						propertyName = target.ElementAccessExpressionArgumentExpression()
					}
				} else {
					args := node.Arguments()
					targetFunction = args[0]
					target = args[1]
					propertyName = target
					definition = args[2]
				}
				if isPrototypeExpando(targetFunction) {
					targetFunction = targetFunction.Expression()
					if ast.IsIdentifier(targetFunction) {
						expandoTargets.Add(targetFunction.Text())
					}
				}
				if ast.IsIdentifier(targetFunction) && expandoTargets.Has(targetFunction.Text()) {
					endNode := startNode(node, targetFunction)
					addSymbolForNode(target, propertyName, getSymbolsForNode(definition))
					endNode()
				} else {
					node.ForEachChild(visit)
				}
			}
		case ast.KindExportAssignment:
			if node.ExportAssignmentIsExportEquals() {
				addSymbolForNode(node, ast.Handle{}, getSymbolsForNode(node.Expression()))
			} else {
				node.ForEachChild(visit)
			}
		default:
			node.ForEachChild(visit)
		}
		return false
	}
	node.ForEachChild(visit)
	return mergeExpandos(symbols)
}

func isPrototypeExpando(target ast.Handle) bool {
	if ast.IsAccessExpression(target) {
		accessName := ast.GetElementOrPropertyAccessName(target)
		return !accessName.IsNil() && accessName.Text() == "prototype"
	}
	return false
}

const maxLength = 150

func (l *LanguageService) newDocumentSymbol(node ast.Handle, name ast.Handle, children []*lsproto.DocumentSymbol) *lsproto.DocumentSymbol {
	result := new(lsproto.DocumentSymbol)
	file := ast.GetSourceFileOfNode(node)
	nodeStartPos := scanner.SkipTrivia(file.Text(), node.Pos())
	if name.IsNil() {
		name = ast.GetNameOfDeclaration(node)
	}
	var text string
	var nameStartPos, nameEndPos int
	if ast.IsModuleDeclaration(node) && !ast.IsAmbientModule(node) {
		text = getModuleName(node)
		nameStartPos = scanner.SkipTrivia(file.Text(), name.Pos())
		nameEndPos = getInteriorModule(node).Name().End()
	} else if ast.IsAnyExportAssignment(node) && node.ExportAssignmentIsExportEquals() {
		text = "export="
		if !ast.NodeIsMissing(name) {
			nameStartPos = scanner.SkipTrivia(file.Text(), name.Pos())
			nameEndPos = name.End()
		} else {
			nameStartPos = nodeStartPos
			nameEndPos = node.End()
		}
	} else if !name.IsNil() {
		text = getTextOfName(name)
		nameStartPos = max(scanner.SkipTrivia(file.Text(), name.Pos()), nodeStartPos)
		nameEndPos = max(name.End(), nodeStartPos)
	} else {
		text = getUnnamedNodeLabel(node)
		nameStartPos = nodeStartPos
		nameEndPos = nodeStartPos
	}
	if text == "" {
		return nil
	}
	truncatedText := stringutil.TruncateByRunes(text, maxLength)
	if len(truncatedText) < len(text) {
		text = truncatedText + "..."
	}
	result.Name = text
	result.Kind = getSymbolKindFromNode(node)
	selectionRange, selectionFidelity := l.converters.ToLSPRangeForFeature(file, core.NewTextRange(nameStartPos, nameEndPos), spanmap.FeatureDocumentSymbols)
	if !selectionFidelity.IsSingleSegment() {
		return nil
	}
	symbolRange, rangeFidelity := l.converters.ToLSPRangeForFeature(file, core.NewTextRange(nodeStartPos, node.End()), spanmap.FeatureDocumentSymbols)
	if rangeFidelity.IsNone() {
		symbolRange = selectionRange
	}
	result.Range = symbolRange
	result.SelectionRange = selectionRange
	if children == nil {
		children = []*lsproto.DocumentSymbol{}
	}
	result.Children = &children
	return result
}

func mergeExpandos(symbols []*lsproto.DocumentSymbol) []*lsproto.DocumentSymbol {
	mergedSymbols := make([]*lsproto.DocumentSymbol, 0, len(symbols))
	nameToExpandoTargetIndex := collections.MultiMap[string, int]{}
	nameToNamespaceIndex := map[string]int{}
	for i, symbol := range symbols {
		if isAnonymousName(symbol.Name) {
			continue
		}
		if symbol.Kind == lsproto.SymbolKindClass || symbol.Kind == lsproto.SymbolKindFunction || symbol.Kind == lsproto.SymbolKindVariable {
			nameToExpandoTargetIndex.Add(symbol.Name, i)
		}
		if symbol.Kind == lsproto.SymbolKindNamespace {
			if _, ok := nameToNamespaceIndex[symbol.Name]; !ok {
				nameToNamespaceIndex[symbol.Name] = i
			}
		}
	}
	for i, symbol := range symbols {
		if symbol.Children != nil {
			children := mergeExpandos(*symbol.Children)
			symbol.Children = &children
		}
		if isAnonymousName(symbol.Name) {
			continue
		}
		if symbol.Kind == lsproto.SymbolKindProperty {
			symbolsWithSameName := nameToExpandoTargetIndex.Get(symbol.Name)
			for j := len(symbolsWithSameName) - 1; j >= 0; j-- {
				targetIndex := symbolsWithSameName[j]
				targetSymbol := symbols[targetIndex]
				mergeChildren(targetSymbol, symbol)
				symbols[i] = nil
			}
		}
		if symbol.Kind == lsproto.SymbolKindNamespace {
			if targetIndex, ok := nameToNamespaceIndex[symbol.Name]; ok && targetIndex != i {
				targetSymbol := symbols[targetIndex]
				mergeChildren(targetSymbol, symbol)
				symbols[i] = nil
			}
		}
	}
	for _, symbol := range symbols {
		if symbol != nil {
			mergedSymbols = append(mergedSymbols, symbol)
		}
	}
	return mergedSymbols
}
func mergeChildren(target *lsproto.DocumentSymbol, source *lsproto.DocumentSymbol) {
	if source.Children != nil {
		if target.Children == nil {
			target.Children = source.Children
		} else {
			*target.Children = mergeExpandos(append(*target.Children, *source.Children...))
			slices.SortFunc(*target.Children, func(a, b *lsproto.DocumentSymbol) int {
				return lsproto.CompareRanges(a.Range, b.Range)
			})
		}
	}
}

func isAnonymousName(name string) bool {
	return name == "<function>" || name == "<class>" || name == "export=" || name == "default" || name == "constructor" || name == "()" || name == "new()" || name == "[]" || strings.HasSuffix(name, ") callback")
}
func getTextOfName(node ast.Handle) string {
	switch node.Kind {
	case ast.KindIdentifier, ast.KindPrivateIdentifier, ast.KindNumericLiteral:
		return node.Text()
	case ast.KindStringLiteral:
		return "\"" + printer.EscapeString(node.Text(), '"') + "\""
	case ast.KindNoSubstitutionTemplateLiteral:
		return "`" + printer.EscapeString(node.Text(), '`') + "`"
	case ast.KindComputedPropertyName:
		if ast.IsStringOrNumericLiteralLike(node.Expression()) {
			return getTextOfName(node.Expression())
		}
	}
	return scanner.GetTextOfNode(node)
}
func getUnnamedNodeLabel(node ast.Handle) string {
	if parent := ast.WalkUpParenthesizedExpressions(node.Parent()); !parent.IsNil() && ast.IsExportAssignment(parent) {
		if parent.ExportAssignmentIsExportEquals() {
			return "export="
		}
		return "default"
	}
	switch node.Kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction:
		if node.ModifierFlags()&ast.ModifierFlagsDefault != 0 {
			return "default"
		}
		if ast.IsCallExpression(node.Parent()) {
			name := getCallExpressionName(node.Parent().Expression())
			if name != "" {
				name = cleanCallbackText(name)
				if len(name) > maxLength {
					return name + " callback"
				}
				args := cleanCallbackText(getCallExpressionLiteralArgs(node.Parent()))
				return name + "(" + args + ") callback"
			}
		}
		return "<function>"
	case ast.KindClassDeclaration, ast.KindClassExpression:
		if node.ModifierFlags()&ast.ModifierFlagsDefault != 0 {
			return "default"
		}
		return "<class>"
	case ast.KindConstructor:
		return "constructor"
	case ast.KindCallSignature:
		return "()"
	case ast.KindConstructSignature:
		return "new()"
	case ast.KindIndexSignature:
		return "[]"
	}
	return ""
}
func getCallExpressionName(node ast.Handle) string {
	switch node.Kind {
	case ast.KindIdentifier, ast.KindPrivateIdentifier:
		return node.Text()
	case ast.KindPropertyAccessExpression:
		left := getCallExpressionName(node.Expression())
		right := getCallExpressionName(node.Name())
		if left != "" {
			return left + "." + right
		}
		return right
	}
	return ""
}
func getCallExpressionLiteralArgs(callExpr ast.Handle) string {
	var parts []string
	for _, arg := range callExpr.Arguments() {
		if ast.IsStringLiteralLike(arg) || ast.IsTemplateExpression(arg) {
			parts = append(parts, scanner.GetTextOfNode(arg))
		}
	}
	return strings.Join(parts, ", ")
}
func cleanCallbackText(text string) string {
	truncated := stringutil.TruncateByRunes(text, maxLength)
	if len(truncated) < len(text) {
		text = truncated + "..."
	}
	return strings.Map(func(r rune) rune {
		if stringutil.IsLineBreak(r) {
			return -1
		}
		return r
	}, text)
}
func getInteriorModule(node ast.Handle) ast.Handle {
	for !node.Body().IsNil() && ast.IsModuleDeclaration(node.Body()) {
		node = node.Body()
	}
	return node
}
func getModuleName(node ast.Handle) string {
	result := node.Name().Text()
	for !node.Body().IsNil() && ast.IsModuleDeclaration(node.Body()) {
		node = node.Body()
		result = result + "." + node.Name().Text()
	}
	return result
}

func collectNamedDeclarations(file *ast.SourceFile) map[string][]ast.Handle {
	result := make(map[string][]ast.Handle)
	var visit ast.StoreVisitor
	visit = func(node ast.Handle) bool {
		if name := ast.GetNameOfDeclaration(node); !name.IsNil() {
			if text := ast.GetTextOfPropertyName(name); text != "" {
				result[text] = append(result[text], node)
			}
		}
		return node.ForEachChild(visit)
	}
	file.ParseRoot().ForEachChild(visit)
	return result
}

type DeclarationInfo struct {
	name        string
	declaration ast.Handle
	matchScore  int
}

func ProvideWorkspaceSymbols(ctx context.Context, programs []*compiler.Program, converters *lsconv.Converters, preferences lsutil.UserPreferences, query string) (lsproto.WorkspaceSymbolResponse, error) {
	excludeLibrarySymbols := preferences.ExcludeLibrarySymbolsInNavTo.IsTrue()
	sourceFiles := map[tspath.Path]*ast.SourceFile{}
	for _, program := range programs {
		for _, sourceFile := range program.SourceFiles() {
			if (program.HasTSFile() || !sourceFile.IsDeclarationFile) && !shouldExcludeFile(sourceFile, program, excludeLibrarySymbols) {
				sourceFiles[sourceFile.Path()] = sourceFile
			}
		}
	}
	var infos []DeclarationInfo
	for _, sourceFile := range sourceFiles {
		if ctx.Err() != nil {
			return lsproto.SymbolInformationsOrWorkspaceSymbolsOrNull{}, nil
		}
		declarationMap := collectNamedDeclarations(sourceFile)
		for name, declarations := range declarationMap {
			score := getMatchScore(name, query)
			if score >= 0 {
				for _, declaration := range declarations {
					infos = append(infos, DeclarationInfo{name, declaration, score})
				}
			}
		}
	}
	slices.SortFunc(infos, compareDeclarationInfos)
	count := min(len(infos), 256)
	symbols := make([]*lsproto.SymbolInformation, 0, count)
	for _, info := range infos[0:count] {
		node := info.declaration
		sourceFile := ast.GetSourceFileOfNode(node)
		container := getContainerNode(info.declaration)
		var containerName *string
		if !container.IsNil() {
			containerName = strPtrTo(ast.GetDeclarationName(container))
		}
		nameNode := ast.GetNameOfDeclaration(node)
		nameStart := astnav.GetStartOfNode(nameNode, sourceFile, false)
		nameRange := core.NewTextRange(nameStart, nameNode.End())
		location, fidelity := converters.ToLSPLocationForFeature(sourceFile, nameRange, spanmap.FeatureDocumentSymbols)
		if !fidelity.IsSingleSegment() {
			continue
		}
		var symbol lsproto.SymbolInformation
		symbol.Name = info.name
		symbol.Kind = getSymbolKindFromNode(info.declaration)
		symbol.Location = location
		symbol.ContainerName = containerName
		symbols = append(symbols, &symbol)
	}
	return lsproto.SymbolInformationsOrWorkspaceSymbolsOrNull{SymbolInformations: &symbols}, nil
}
func shouldExcludeFile(file *ast.SourceFile, program *compiler.Program, excludeLibrarySymbols bool) bool {
	return excludeLibrarySymbols && (isInsideNodeModules(file.FileName()) || program.IsLibFile(file))
}
func isInsideNodeModules(fileName string) bool {
	return strings.Contains(fileName, "/node_modules/")
}

func getMatchScore(s string, pattern string) int {
	score := 0
	for _, p := range pattern {
		exact := unicode.IsUpper(p)
		for {
			c, size := utf8.DecodeRuneInString(s)
			if size == 0 {
				return -1
			}
			s = s[size:]
			if exact && c == p || !exact && unicode.ToLower(c) == unicode.ToLower(p) {
				break
			}
			score++
		}
	}
	return score
}

func compareDeclarationInfos(d1, d2 DeclarationInfo) int {
	if d1.matchScore != d2.matchScore {
		return d1.matchScore - d2.matchScore
	}
	if c := stringutil.CompareStringsCaseInsensitive(d1.name, d2.name); c != 0 {
		return c
	}
	if c := strings.Compare(d1.name, d2.name); c != 0 {
		return c
	}
	s1 := ast.GetSourceFileOfNode(d1.declaration)
	s2 := ast.GetSourceFileOfNode(d2.declaration)
	if s1 != s2 {
		return strings.Compare(string(s1.Path()), string(s2.Path()))
	}
	return d1.declaration.Pos() - d2.declaration.Pos()
}

func getSymbolKindFromNode(node ast.Handle) lsproto.SymbolKind {
	switch node.Kind {
	case ast.KindSourceFile:
		if ast.IsExternalModule(ast.GetSourceFileOfNode(node)) {
			return lsproto.SymbolKindModule
		}
		return lsproto.SymbolKindFile
	case ast.KindModuleDeclaration:
		return lsproto.SymbolKindNamespace
	case ast.KindClassDeclaration, ast.KindClassExpression:
		return lsproto.SymbolKindClass
	case ast.KindInterfaceDeclaration:
		return lsproto.SymbolKindInterface
	case ast.KindTypeAliasDeclaration, ast.KindJSDocTypedefTag, ast.KindJSDocCallbackTag:
		return lsproto.SymbolKindClass
	case ast.KindEnumDeclaration:
		return lsproto.SymbolKindEnum
	case ast.KindVariableDeclaration:
		return lsproto.SymbolKindVariable
	case ast.KindArrowFunction, ast.KindFunctionDeclaration, ast.KindFunctionExpression:
		return lsproto.SymbolKindFunction
	case ast.KindGetAccessor, ast.KindSetAccessor:
		return lsproto.SymbolKindProperty
	case ast.KindMethodDeclaration, ast.KindMethodSignature:
		return lsproto.SymbolKindMethod
	case ast.KindPropertyDeclaration, ast.KindPropertySignature, ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment, ast.KindSpreadAssignment, ast.KindIndexSignature:
		return lsproto.SymbolKindProperty
	case ast.KindCallSignature:
		return lsproto.SymbolKindMethod
	case ast.KindConstructSignature:
		return lsproto.SymbolKindConstructor
	case ast.KindConstructor, ast.KindClassStaticBlockDeclaration:
		return lsproto.SymbolKindConstructor
	case ast.KindTypeParameter:
		return lsproto.SymbolKindTypeParameter
	case ast.KindEnumMember:
		return lsproto.SymbolKindEnumMember
	case ast.KindParameter:
		if ast.HasSyntacticModifier(node, ast.ModifierFlagsParameterPropertyModifier) {
			return lsproto.SymbolKindProperty
		}
		return lsproto.SymbolKindVariable
	case ast.KindBinaryExpression, ast.KindCallExpression:
		kind := ast.GetAssignmentDeclarationKind(node)
		switch kind {
		case ast.JSDeclarationKindThisProperty, ast.JSDeclarationKindProperty, ast.JSDeclarationKindObjectDefinePropertyValue:
			return lsproto.SymbolKindProperty
		}
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral, ast.KindNumericLiteral:
		return lsproto.SymbolKindProperty
	}
	return lsproto.SymbolKindVariable
}
