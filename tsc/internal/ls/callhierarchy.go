package ls

import (
	"context"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsconv"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
	"slices"
	"strings"
	"sync"
)

type CallHierarchyDeclaration = // Indictates whether a node is named function or class expression.
// Indicates whether a node is a function, arrow, or class expression assigned to a constant variable or class property.
// Indicates whether a node could possibly be a call hierarchy declaration.
//
// See `resolveCallHierarchyDeclaration` for the specific rules.
// Indicates whether a node is a valid a call hierarchy declaration.
//
// See `resolveCallHierarchyDeclaration` for the specific rules.
// Gets the node that can be used as a reference to a call hierarchy declaration.
// Gets the symbol for a call hierarchy declaration.
// Gets the text and range for the name of a call hierarchy declaration.
// "static".length
// "function".length
// "class".length
// Finds the implementation of a function-like declaration, if one exists.
// Find the implementation or the first declaration for a call hierarchy declaration.
// Resolves the call hierarchy declaration for a node.
// A call hierarchy item must refer to either a SourceFile, Module Declaration, Class Static Block, or something intrinsically callable that has a name:
// - Class Declarations
// - Class Expressions (with a name)
// - Function Declarations
// - Function Expressions (with a name or assigned to a const variable)
// - Arrow Functions (assigned to a const variable)
// - Constructors
// - Class `static {}` initializer blocks
// - Methods
// - Accessors
//
// If a call is contained in a non-named callable Node (function expression, arrow function, etc.), then
// its containing `CallHierarchyItem` is a containing function or SourceFile that matches the above list.
// #39453
// Creates a `CallHierarchyItem` for a call hierarchy declaration.
/*includeElementAccess*/ /*skipPastOuterExpressions*/ /*includeJsDoc*/ // Gets the call sites that call into the provided call hierarchy declaration.
// Source files and modules have no incoming calls.
/*includeJsDoc*/ /*defaultProjectData*/ // do not descend into ambient nodes.
// do not descend into other call site declarations, other than class member names
// do not descend into nodes that cannot contain callable nodes
// do not descend into the type side of an assertion
// do not descend into the type of a variable or parameter declaration
// do not descend into the type arguments of a call expression
// do not descend into the type arguments of a new expression
// do not descend into the type arguments of a tagged template expression
// do not descend into the type arguments of a JsxOpeningLikeElement
// do not descend into the type side of an assertion
// do not descend into types
// Gets the call sites that call out of the provided call hierarchy declaration.
ast.Handle

func isNamedExpression(node ast.Handle) bool {
	if node.IsNil() {
		return false
	}
	if !ast.IsFunctionExpression(node) && !ast.IsClassExpression(node) {
		return false
	}
	name := node.Name()
	return !name.IsNil() && ast.IsIdentifier(name)
}
func isVariableLike(node ast.Handle) bool {
	if node.IsNil() {
		return false
	}
	return ast.IsPropertyDeclaration(node) || ast.IsVariableDeclaration(node)
}

func isAssignedExpression(node ast.Handle) bool {
	if node.IsNil() {
		return false
	}
	if !(ast.IsFunctionExpression(node) || ast.IsArrowFunction(node) || ast.IsClassExpression(node)) {
		return false
	}
	if !node.Name().IsNil() {
		return false
	}
	parent := node.Parent()
	if !isVariableLike(parent) {
		return false
	}
	if parent.Initializer() != node {
		return false
	}
	name := parent.Name()
	if !ast.IsIdentifier(name) {
		return false
	}
	return (ast.GetCombinedNodeFlags(parent)&ast.NodeFlagsConst) != 0 || ast.IsPropertyDeclaration(parent)
}

func isPossibleCallHierarchyDeclaration(node ast.Handle) bool {
	if node.IsNil() {
		return false
	}
	return ast.IsSourceFile(node) || ast.IsModuleDeclaration(node) || ast.IsFunctionDeclaration(node) || ast.IsFunctionExpression(node) || ast.IsClassDeclaration(node) || ast.IsClassExpression(node) || ast.IsClassStaticBlockDeclaration(node) || ast.IsMethodDeclaration(node) || ast.IsMethodSignatureDeclaration(node) || ast.IsGetAccessorDeclaration(node) || ast.IsSetAccessorDeclaration(node)
}

func isValidCallHierarchyDeclaration(node ast.Handle) bool {
	if node.IsNil() {
		return false
	}
	if ast.IsSourceFile(node) {
		return true
	}
	if ast.IsModuleDeclaration(node) {
		return ast.IsIdentifier(node.Name())
	}
	return ast.IsFunctionDeclaration(node) || ast.IsClassDeclaration(node) || ast.IsClassStaticBlockDeclaration(node) || ast.IsMethodDeclaration(node) || ast.IsMethodSignatureDeclaration(node) || ast.IsGetAccessorDeclaration(node) || ast.IsSetAccessorDeclaration(node) || isNamedExpression(node) || isAssignedExpression(node)
}

func getCallHierarchyDeclarationReferenceNode(node ast.Handle) ast.Handle {
	if node.IsNil() {
		return ast.Handle{}
	}
	if ast.IsSourceFile(node) {
		return node
	}
	if name := node.Name(); !name.IsNil() {
		return name
	}
	if isAssignedExpression(node) {
		return node.Parent().Name()
	}
	if modifiers := node.Modifiers(); modifiers != 0 {
		for _, mod := range node.Store().ListSlice(modifiers) {
			if mod.Kind == ast.KindDefaultKeyword {
				return mod
			}
		}
	}
	return ast.Handle{}
}

func getSymbolOfCallHierarchyDeclaration(c *checker.Checker, node ast.Handle) *ast.Symbol {
	if ast.IsClassStaticBlockDeclaration(node) {
		return nil
	}
	location := getCallHierarchyDeclarationReferenceNode(node)
	if location.IsNil() {
		return nil
	}
	return c.GetSymbolAtLocation(location)
}

func getCallHierarchyItemName(program *compiler.Program, node ast.Handle) (text string, pos int, end int) {
	if ast.IsSourceFile(node) {
		sourceFile := node
		return ast.GetSourceFileOfNode(sourceFile).FileName(), 0, 0
	}
	if (ast.IsFunctionDeclaration(node) || ast.IsClassDeclaration(node)) && node.Name().IsNil() {
		if modifiers := node.Modifiers(); modifiers != 0 {
			for _, mod := range node.Store().ListSlice(modifiers) {
				if mod.Kind == ast.KindDefaultKeyword {
					sourceFile := ast.GetSourceFileOfNode(node)
					start := scanner.SkipTrivia(sourceFile.Text(), mod.Pos())
					return "default", start, mod.End()
				}
			}
		}
	}
	if ast.IsClassStaticBlockDeclaration(node) {
		sourceFile := ast.GetSourceFileOfNode(node)
		pos := scanner.SkipTrivia(sourceFile.Text(), moveRangePastModifiers(node).Pos())
		end := pos + 6
		c, done := program.GetTypeCheckerForFile(context.Background(), sourceFile)
		defer done()
		symbol := c.GetSymbolAtLocation(node.Parent())
		prefix := ""
		if symbol != nil {
			prefix = c.SymbolToString(symbol) + " "
		}
		return prefix + "static {}", pos, end
	}
	var declName ast.Handle
	if isAssignedExpression(node) {
		declName = node.Parent().Name()
	} else {
		declName = ast.GetNameOfDeclaration(node)
	}
	if declName.IsNil() || !ast.NodeIsPresent(declName) {
		sourceFile := ast.GetSourceFileOfNode(node)
		switch {
		case ast.IsFunctionDeclaration(node) || ast.IsFunctionExpression(node):
			kwPos := scanner.SkipTrivia(sourceFile.Text(), moveRangePastModifiers(node).Pos())
			return "(anonymous)", kwPos, kwPos + 8
		case ast.IsClassDeclaration(node) || ast.IsClassExpression(node):
			kwPos := scanner.SkipTrivia(sourceFile.Text(), moveRangePastModifiers(node).Pos())
			return "(anonymous)", kwPos, kwPos + 5
		}
		debug.Assert(!declName.IsNil(), "Expected call hierarchy item to have a name")
	}
	text = getTextOfCallHierarchyName(program, node, declName, node)
	sourceFile := ast.GetSourceFileOfNode(node)
	namePos := scanner.SkipTrivia(sourceFile.Text(), declName.Pos())
	return text, namePos, declName.End()
}
func getTextOfCallHierarchyName(program *compiler.Program, sourceNode ast.Handle, name ast.Handle, printNode ast.Handle) string {
	if ast.IsIdentifier(name) || ast.IsStringOrNumericLiteralLike(name) {
		return name.Text()
	}
	if ast.IsComputedPropertyName(name) {
		expr := name.Expression()
		if ast.IsStringOrNumericLiteralLike(expr) {
			return expr.Text()
		}
	}
	c, done := program.GetTypeCheckerForFile(context.Background(), ast.GetSourceFileOfNode(sourceNode))
	defer done()
	symbol := c.GetSymbolAtLocation(name)
	if symbol != nil {
		text := c.SymbolToString(symbol)
		if text != "" {
			return text
		}
	}
	sourceFile := ast.GetSourceFileOfNode(sourceNode)
	writer, putWriter := printer.GetSingleLineStringWriter()
	defer putWriter()
	p := printer.NewPrinter(printer.PrinterOptions{RemoveComments: true}, printer.PrintHandlers{}, nil)
	p.Write(printNode, sourceFile, writer, nil)
	return writer.String()
}
func getCallHierarchyItemContainerName(program *compiler.Program, node ast.Handle) string {
	if isAssignedExpression(node) {
		parent := node.Parent()
		if ast.IsPropertyDeclaration(parent) && ast.IsClassLike(parent.Parent()) {
			if ast.IsClassExpression(parent.Parent()) {
				if assignedName := ast.GetAssignedName(parent.Parent()); !assignedName.IsNil() {
					return getTextOfCallHierarchyName(program, node, assignedName, assignedName)
				}
			} else {
				if name := parent.Parent().Name(); !name.IsNil() {
					return getTextOfCallHierarchyName(program, node, name, name)
				}
			}
		}
		if !parent.Parent().Parent().IsNil() && !parent.Parent().Parent().Parent().IsNil() && ast.IsModuleBlock(parent.Parent().Parent().Parent()) {
			modParent := parent.Parent().Parent().Parent().Parent()
			if ast.IsModuleDeclaration(modParent) {
				if name := modParent.Name(); !name.IsNil() && ast.IsIdentifier(name) {
					return name.Text()
				}
			}
		}
		return ""
	}
	switch node.Kind {
	case ast.KindGetAccessor, ast.KindSetAccessor, ast.KindMethodDeclaration:
		if node.Parent().Kind == ast.KindObjectLiteralExpression {
			if assignedName := ast.GetAssignedName(node.Parent()); !assignedName.IsNil() {
				return getTextOfCallHierarchyName(program, node, assignedName, assignedName)
			}
		}
		if name := ast.GetNameOfDeclaration(node.Parent()); !name.IsNil() {
			return getTextOfCallHierarchyName(program, node, name, name)
		}
	case ast.KindFunctionDeclaration, ast.KindClassDeclaration, ast.KindModuleDeclaration:
		if ast.IsModuleBlock(node.Parent()) {
			if ast.IsModuleDeclaration(node.Parent().Parent()) {
				if name := node.Parent().Parent().Name(); !name.IsNil() && ast.IsIdentifier(name) {
					return name.Text()
				}
			}
		}
	}
	return ""
}
func moveRangePastModifiers(node ast.Handle) core.TextRange {
	if modifiers := node.Modifiers(); modifiers != 0 && node.Store().ListLen(modifiers) > 0 {
		lastMod := node.Store().ListAt(modifiers, node.Store().ListLen(modifiers)-1)
		return core.NewTextRange(lastMod.End(), node.End())
	}
	return core.NewTextRange(node.Pos(), node.End())
}

func findImplementation(c *checker.Checker, node ast.Handle) ast.Handle {
	if node.IsNil() {
		return ast.Handle{}
	}
	if !ast.IsFunctionLikeDeclaration(node) {
		return node
	}
	if !node.Body().IsNil() {
		return node
	}
	if ast.IsConstructorDeclaration(node) {
		return ast.GetFirstConstructorWithBody(node.Parent())
	}
	if ast.IsFunctionDeclaration(node) || ast.IsMethodDeclaration(node) {
		symbol := getSymbolOfCallHierarchyDeclaration(c, node)
		if symbol != nil && symbol.ValueDeclaration != 0 {
			if ast.IsFunctionLikeDeclaration(ast.NodeOf(symbol.ValueDeclaration)) && !ast.NodeOf(symbol.ValueDeclaration).Body().IsNil() {
				return ast.NodeOf(symbol.ValueDeclaration)
			}
		}
		return ast.Handle{}
	}
	return node
}
func findAllInitialDeclarations(c *checker.Checker, node ast.Handle) []ast.Handle {
	if ast.IsClassStaticBlockDeclaration(node) {
		return nil
	}
	symbol := getSymbolOfCallHierarchyDeclaration(c, node)
	if symbol == nil || symbol.Declarations == nil {
		return nil
	}
	type declKey struct {
		file string
		pos  int
	}
	indices := make([]int, len(symbol.Declarations))
	for i := range indices {
		indices[i] = i
	}
	keys := make([]declKey, len(symbol.Declarations))
	for i, decl := range ast.DeclarationNodes(symbol) {
		keys[i] = declKey{file: ast.GetSourceFileOfNode(decl).FileName(), pos: decl.Pos()}
	}
	slices.SortFunc(indices, func(a, b int) int {
		if keys[a].file != keys[b].file {
			return strings.Compare(keys[a].file, keys[b].file)
		}
		return keys[a].pos - keys[b].pos
	})
	var declarations []ast.Handle
	var lastDecl ast.Handle
	for _, i := range indices {
		decl := ast.DeclarationNodes(symbol)[i]
		if isValidCallHierarchyDeclaration(decl) {
			if lastDecl.IsNil() || lastDecl.Parent() != decl.Parent() || lastDecl.End() != decl.Pos() {
				declarations = append(declarations, decl)
			}
			lastDecl = decl
		}
	}
	return declarations
}

func findImplementationOrAllInitialDeclarations(c *checker.Checker, node ast.Handle) any {
	if ast.IsClassStaticBlockDeclaration(node) {
		return node
	}
	if ast.IsFunctionLikeDeclaration(node) {
		if impl := findImplementation(c, node); !impl.IsNil() {
			return impl
		}
		if decls := findAllInitialDeclarations(c, node); decls != nil {
			return decls
		}
		return node
	}
	if decls := findAllInitialDeclarations(c, node); decls != nil {
		return decls
	}
	return node
}

func resolveCallHierarchyDeclaration(program *compiler.Program, location ast.Handle) (result any) {
	c, done := program.GetTypeChecker(context.Background())
	defer done()
	followingSymbol := false
	for !location.IsNil() {
		if isValidCallHierarchyDeclaration(location) {
			return findImplementationOrAllInitialDeclarations(c, location)
		}
		if isPossibleCallHierarchyDeclaration(location) {
			ancestor := ast.FindAncestor(location, isValidCallHierarchyDeclaration)
			if !ancestor.IsNil() {
				return findImplementationOrAllInitialDeclarations(c, ancestor)
			}
		}
		if ast.IsDeclarationName(location) {
			if isValidCallHierarchyDeclaration(location.Parent()) {
				return findImplementationOrAllInitialDeclarations(c, location.Parent())
			}
			if isPossibleCallHierarchyDeclaration(location.Parent()) {
				ancestor := ast.FindAncestor(location.Parent(), isValidCallHierarchyDeclaration)
				if !ancestor.IsNil() {
					return findImplementationOrAllInitialDeclarations(c, ancestor)
				}
			}
			if isVariableLike(location.Parent()) {
				initializer := location.Parent().Initializer()
				if !initializer.IsNil() && isAssignedExpression(initializer) {
					return initializer
				}
			}
			return nil
		}
		if ast.IsConstructorDeclaration(location) {
			if isValidCallHierarchyDeclaration(location.Parent()) {
				return location.Parent()
			}
			return nil
		}
		if location.Kind == ast.KindStaticKeyword && ast.IsClassStaticBlockDeclaration(location.Parent()) {
			location = location.Parent()
			continue
		}
		if ast.IsVariableDeclaration(location) {
			if initializer := location.Initializer(); !initializer.IsNil() && isAssignedExpression(initializer) {
				return initializer
			}
		}
		if !followingSymbol {
			symbol := c.GetSymbolAtLocation(location)
			if symbol != nil {
				if (symbol.Flags & ast.SymbolFlagsAlias) != 0 {
					symbol = c.GetAliasedSymbol(symbol)
				}
				if symbol.ValueDeclaration != 0 {
					followingSymbol = true
					location = ast.NodeOf(symbol.ValueDeclaration)
					continue
				}
			}
		}
		return nil
	}
	return nil
}

func (l *LanguageService) createCallHierarchyItem(program *compiler.Program, node ast.Handle) *lsproto.CallHierarchyItem {
	sourceFile := ast.GetSourceFileOfNode(node)
	nameText, namePos, nameEnd := getCallHierarchyItemName(program, node)
	containerName := getCallHierarchyItemContainerName(program, node)
	kind := getSymbolKindFromNode(node)
	fullStart := scanner.SkipTriviaEx(sourceFile.Text(), node.Pos(), &scanner.SkipTriviaOptions{StopAtComments: true})
	span, spanFidelity := l.converters.ToLSPRangeForFeature(sourceFile, core.NewTextRange(fullStart, node.End()), spanmap.FeatureCallHierarchy)
	selectionSpan, selectionFidelity := l.converters.ToLSPRangeForFeature(sourceFile, core.NewTextRange(namePos, nameEnd), spanmap.FeatureCallHierarchy)
	if !selectionFidelity.IsSingleSegment() {
		return nil
	}
	if spanFidelity.IsNone() || sourceFile.ContentMapper() != "" && !lspRangeContains(span, selectionSpan) {
		span = selectionSpan
	}
	item := &lsproto.CallHierarchyItem{Name: nameText, Kind: kind, Uri: lsconv.FileNameToDocumentURI(sourceFile.OriginalFileName()), Range: span, SelectionRange: selectionSpan}
	if containerName != "" {
		item.Detail = &containerName
	}
	return item
}

type callSite struct {
	declaration ast.Handle
	textRange   core.TextRange
	sourceFile  *ast.SourceFile
}

func convertEntryToCallSite(entry *ReferenceEntry) *callSite {
	if entry.kind != entryKindNode {
		return nil
	}
	node := entry.node
	if !ast.IsCallOrNewExpressionTarget(node, true, true) && !ast.IsTaggedTemplateTag(node, true, true) && !ast.IsDecoratorTarget(node, true, true) && !ast.IsJsxOpeningLikeElementTagName(node, true, true) && !ast.IsRightSideOfPropertyAccess(node) && !ast.IsArgumentExpressionOfElementAccess(node) {
		return nil
	}
	sourceFile := ast.GetSourceFileOfNode(node)
	ancestor := ast.FindAncestor(node, isValidCallHierarchyDeclaration)
	if ancestor.IsNil() {
		ancestor = sourceFile.ParseRoot()
	}
	start := scanner.SkipTrivia(sourceFile.Text(), node.Pos())
	return &callSite{declaration: ancestor, textRange: core.NewTextRange(start, node.End()), sourceFile: sourceFile}
}
func getCallSiteGroupKey(site *callSite) ast.NodeId {
	return site.declaration.NodeId()
}
func (l *LanguageService) convertCallSiteGroupToIncomingCall(program *compiler.Program, entries []*callSite) *lsproto.CallHierarchyIncomingCall {
	fromRanges := make([]lsproto.Range, 0, len(entries))
	for _, entry := range entries {
		sourceFile := entry.sourceFile
		if lspRange, fidelity := l.converters.ToLSPRangeForFeature(sourceFile, entry.textRange, spanmap.FeatureCallHierarchy); !fidelity.IsNone() {
			fromRanges = append(fromRanges, lspRange)
		}
	}
	from := l.createCallHierarchyItem(program, entries[0].declaration)
	if from == nil || len(fromRanges) == 0 {
		return nil
	}
	slices.SortFunc(fromRanges, lsproto.CompareRanges)
	return &lsproto.CallHierarchyIncomingCall{From: from, FromRanges: fromRanges}
}

type incomingEntry struct {
	ls              *LanguageService
	node            ast.Handle
	sourceFileOnce  sync.Once
	sourceFile      *ast.SourceFile
	documentUriOnce sync.Once
	documentUri     lsproto.DocumentUri
	positionOnce    sync.Once
	position        lsproto.Position
}

var _ lsproto.HasTextDocumentPosition = (*incomingEntry)(nil)

func (d *incomingEntry) getSourceFile() *ast.SourceFile {
	d.sourceFileOnce.Do(func() {
		d.sourceFile = ast.GetSourceFileOfNode(d.node)
	})
	return d.sourceFile
}
func (d *incomingEntry) TextDocumentURI() lsproto.DocumentUri {
	d.documentUriOnce.Do(func() {
		d.documentUri = lsconv.FileNameToDocumentURI(d.getSourceFile().OriginalFileName())
	})
	return d.documentUri
}
func (d *incomingEntry) TextDocumentPosition() lsproto.Position {
	d.positionOnce.Do(func() {
		start := scanner.GetTokenPosOfNode(d.node, d.getSourceFile(), false)
		d.position, _ = d.ls.createLspPosition(start, d.getSourceFile())
	})
	return d.position
}

func (l *LanguageService) getIncomingCalls(ctx context.Context, program *compiler.Program, declaration ast.Handle, orchestrator CrossProjectOrchestrator) (lsproto.CallHierarchyIncomingCallsResponse, error) {
	if ast.IsSourceFile(declaration) || ast.IsModuleDeclaration(declaration) || ast.IsClassStaticBlockDeclaration(declaration) {
		return lsproto.CallHierarchyIncomingCallsOrNull{}, nil
	}
	location := getCallHierarchyDeclarationReferenceNode(declaration)
	if location.IsNil() {
		return lsproto.CallHierarchyIncomingCallsOrNull{}, nil
	}
	locationFile := ast.GetSourceFileOfNode(location)
	locationStart := scanner.GetTokenPosOfNode(location, locationFile, false)
	if _, fidelity := l.converters.ToLSPPositionForFeature(locationFile, core.TextPos(locationStart), spanmap.FeatureCallHierarchy); fidelity.IsNone() {
		return lsproto.CallHierarchyIncomingCallsOrNull{}, nil
	}
	incomingEntry := &incomingEntry{ls: l, node: location}
	result, err := handleCrossProject(l, ctx, incomingEntry, orchestrator, (*LanguageService).symbolAndEntriesToIncomingCalls, combineIncomingCalls, false, false, symbolEntryTransformOptions{}, nil)
	if result.CallHierarchyIncomingCalls != nil {
		slices.SortFunc(*result.CallHierarchyIncomingCalls, func(a, b *lsproto.CallHierarchyIncomingCall) int {
			if uriComp := strings.Compare(string(a.From.Uri), string(b.From.Uri)); uriComp != 0 {
				return uriComp
			}
			if len(a.FromRanges) == 0 || len(b.FromRanges) == 0 {
				return 0
			}
			return lsproto.CompareRanges(a.FromRanges[0], b.FromRanges[0])
		})
	}
	return result, err
}
func (l *LanguageService) symbolAndEntriesToIncomingCalls(ctx context.Context, params *incomingEntry, data SymbolAndEntriesData, options symbolEntryTransformOptions) (lsproto.CallHierarchyIncomingCallsResponse, error) {
	program := l.GetProgram()
	var refEntries []*ReferenceEntry
	for _, symbolAndEntry := range data.SymbolsAndEntries {
		refEntries = append(refEntries, symbolAndEntry.references...)
	}
	var callSites []*callSite
	for _, entry := range refEntries {
		if site := convertEntryToCallSite(entry); site != nil {
			callSites = append(callSites, site)
		}
	}
	if len(callSites) == 0 {
		return lsproto.CallHierarchyIncomingCallsOrNull{}, nil
	}
	grouped := make(map[ast.NodeId][]*callSite)
	for _, site := range callSites {
		key := getCallSiteGroupKey(site)
		grouped[key] = append(grouped[key], site)
	}
	var result []*lsproto.CallHierarchyIncomingCall
	for _, sites := range grouped {
		if incomingCall := l.convertCallSiteGroupToIncomingCall(program, sites); incomingCall != nil {
			result = append(result, incomingCall)
		}
	}
	return lsproto.CallHierarchyIncomingCallsOrNull{CallHierarchyIncomingCalls: &result}, nil
}

type callSiteCollector struct {
	program   *compiler.Program
	callSites []*callSite
}

func (c *callSiteCollector) recordCallSite(node ast.Handle) {
	var target ast.Handle
	switch {
	case ast.IsTaggedTemplateExpression(node):
		target = node.TaggedTemplateExpressionTag()
	case ast.IsJsxOpeningElement(node):
		target = node.TagName()
	case ast.IsJsxSelfClosingElement(node):
		target = node.TagName()
	case ast.IsPropertyAccessExpression(node) || ast.IsElementAccessExpression(node):
		target = node
	case ast.IsClassStaticBlockDeclaration(node):
		target = node
	case ast.IsCallExpression(node):
		target = node.Expression()
	case ast.IsNewExpression(node):
		target = node.Expression()
	case ast.IsDecorator(node):
		target = node.Expression()
	}
	if target.IsNil() {
		return
	}
	declaration := resolveCallHierarchyDeclaration(c.program, target)
	if declaration == nil {
		return
	}
	sourceFile := ast.GetSourceFileOfNode(target)
	start := scanner.SkipTrivia(sourceFile.Text(), target.Pos())
	textRange := core.NewTextRange(start, target.End())
	switch decl := declaration.(type) {
	case ast.Handle:
		c.callSites = append(c.callSites, &callSite{declaration: decl, textRange: textRange, sourceFile: sourceFile})
	case []ast.Handle:
		for _, d := range decl {
			c.callSites = append(c.callSites, &callSite{declaration: d, textRange: textRange, sourceFile: sourceFile})
		}
	}
}
func (c *callSiteCollector) collect(node ast.Handle) {
	if node.IsNil() {
		return
	}
	if (node.Flags() & ast.NodeFlagsAmbient) != 0 {
		return
	}
	if isValidCallHierarchyDeclaration(node) {
		if ast.IsClassLike(node) {
			for _, member := range node.Members() {
				if !member.Name().IsNil() && ast.IsComputedPropertyName(member.Name()) {
					c.collect(member.Name().Expression())
				}
			}
		}
		return
	}
	switch node.Kind {
	case ast.KindIdentifier, ast.KindImportEqualsDeclaration, ast.KindImportDeclaration, ast.KindExportDeclaration, ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration:
		return
	case ast.KindClassStaticBlockDeclaration:
		c.recordCallSite(node)
		return
	case ast.KindTypeAssertionExpression, ast.KindAsExpression:
		c.collect(node.Expression())
		return
	case ast.KindVariableDeclaration, ast.KindParameter:
		c.collect(node.Name())
		c.collect(node.Initializer())
		return
	case ast.KindCallExpression:
		c.recordCallSite(node)
		c.collect(node.Expression())
		for _, arg := range node.Arguments() {
			c.collect(arg)
		}
		return
	case ast.KindNewExpression:
		c.recordCallSite(node)
		c.collect(node.Expression())
		for _, arg := range node.Arguments() {
			c.collect(arg)
		}
		return
	case ast.KindTaggedTemplateExpression:
		c.recordCallSite(node)
		taggedTemplate := node
		c.collect(taggedTemplate.Tag())
		c.collect(taggedTemplate.Template())
		return
	case ast.KindJsxOpeningElement, ast.KindJsxSelfClosingElement:
		c.recordCallSite(node)
		c.collect(node.TagName())
		c.collect(node.Attributes())
		return
	case ast.KindDecorator:
		c.recordCallSite(node)
		c.collect(node.Expression())
		return
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		c.recordCallSite(node)
		node.ForEachChild(func(child ast.Handle) bool {
			c.collect(child)
			return false
		})
		return
	case ast.KindSatisfiesExpression:
		c.collect(node.Expression())
		return
	}
	if ast.IsPartOfTypeNode(node) {
		return
	}
	node.ForEachChild(func(child ast.Handle) bool {
		c.collect(child)
		return false
	})
}
func collectCallSites(program *compiler.Program, c *checker.Checker, node ast.Handle) []*callSite {
	collector := &callSiteCollector{program: program, callSites: make([]*callSite, 0)}
	switch node.Kind {
	case ast.KindSourceFile:
		for _, stmt := range node.Statements() {
			collector.collect(stmt)
		}
	case ast.KindModuleDeclaration:
		if body := node.Body(); !ast.HasSyntacticModifier(node, ast.ModifierFlagsAmbient) && !body.IsNil() && ast.IsModuleBlock(body) {
			for _, stmt := range body.Statements() {
				collector.collect(stmt)
			}
		}
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
		impl := findImplementation(c, node)
		if !impl.IsNil() {
			for _, param := range impl.Parameters() {
				collector.collect(param)
			}
			collector.collect(impl.Body())
		}
	case ast.KindClassDeclaration, ast.KindClassExpression:
		if modifiers := node.Modifiers(); modifiers != 0 {
			for _, mod := range node.Store().ListSlice(modifiers) {
				collector.collect(mod)
			}
		}
		heritage := ast.GetClassExtendsHeritageElement(node)
		if !heritage.IsNil() {
			collector.collect(heritage.Expression())
		}
		for _, member := range node.Members() {
			if ast.CanHaveModifiers(member) && member.Modifiers() != 0 {
				for _, mod := range member.Store().ListSlice(member.Modifiers()) {
					collector.collect(mod)
				}
			}
			if ast.IsPropertyDeclaration(member) {
				collector.collect(member.Initializer())
			} else if ast.IsConstructorDeclaration(member) {
				if body := member.Body(); !body.IsNil() {
					for _, param := range member.Parameters() {
						collector.collect(param)
					}
					collector.collect(body)
				}
			} else if ast.IsClassStaticBlockDeclaration(member) {
				collector.collect(member)
			}
		}
	case ast.KindClassStaticBlockDeclaration:
		staticBlock := node
		collector.collect(staticBlock.Body())
	default:
		debug.AssertNever(node)
	}
	return collector.callSites
}
func (l *LanguageService) convertCallSiteGroupToOutgoingCall(program *compiler.Program, entries []*callSite) *lsproto.CallHierarchyOutgoingCall {
	fromRanges := make([]lsproto.Range, 0, len(entries))
	for _, entry := range entries {
		sourceFile := entry.sourceFile
		if lspRange, fidelity := l.converters.ToLSPRangeForFeature(sourceFile, entry.textRange, spanmap.FeatureCallHierarchy); !fidelity.IsNone() {
			fromRanges = append(fromRanges, lspRange)
		}
	}
	to := l.createCallHierarchyItem(program, entries[0].declaration)
	if to == nil || len(fromRanges) == 0 {
		return nil
	}
	slices.SortFunc(fromRanges, lsproto.CompareRanges)
	return &lsproto.CallHierarchyOutgoingCall{To: to, FromRanges: fromRanges}
}

func (l *LanguageService) getOutgoingCalls(program *compiler.Program, declaration ast.Handle) []*lsproto.CallHierarchyOutgoingCall {
	if (declaration.Flags()&ast.NodeFlagsAmbient) != 0 || ast.IsMethodSignatureDeclaration(declaration) {
		return nil
	}
	c, done := program.GetTypeChecker(context.Background())
	defer done()
	callSites := collectCallSites(program, c, declaration)
	if len(callSites) == 0 {
		return nil
	}
	grouped := make(map[ast.NodeId][]*callSite)
	for _, site := range callSites {
		key := getCallSiteGroupKey(site)
		grouped[key] = append(grouped[key], site)
	}
	var result []*lsproto.CallHierarchyOutgoingCall
	for _, sites := range grouped {
		if outgoingCall := l.convertCallSiteGroupToOutgoingCall(program, sites); outgoingCall != nil {
			result = append(result, outgoingCall)
		}
	}
	slices.SortFunc(result, func(a, b *lsproto.CallHierarchyOutgoingCall) int {
		if uriComp := strings.Compare(string(a.To.Uri), string(b.To.Uri)); uriComp != 0 {
			return uriComp
		}
		if len(a.FromRanges) == 0 || len(b.FromRanges) == 0 {
			return 0
		}
		return lsproto.CompareRanges(a.FromRanges[0], b.FromRanges[0])
	})
	return result
}
func (l *LanguageService) ProvidePrepareCallHierarchy(ctx context.Context, documentURI lsproto.DocumentUri, position lsproto.Position) (lsproto.CallHierarchyPrepareResponse, error) {
	program, file := l.getProgramAndFile(documentURI)
	declarations := l.callHierarchyDeclarations(file, position, program, false)
	var items []*lsproto.CallHierarchyItem
	var seen collections.Set[lsproto.Location]
	for _, declaration := range declarations {
		if item := l.createCallHierarchyItem(program, declaration); item != nil {
			location := lsproto.Location{Uri: item.Uri, Range: item.SelectionRange}
			if seen.AddIfAbsent(location) {
				items = append(items, item)
			}
		}
	}
	if items == nil {
		return lsproto.CallHierarchyItemsOrNull{}, nil
	}
	return lsproto.CallHierarchyItemsOrNull{CallHierarchyItems: &items}, nil
}
func (l *LanguageService) ProvideCallHierarchyIncomingCalls(ctx context.Context, item *lsproto.CallHierarchyItem, orchestrator CrossProjectOrchestrator) (lsproto.CallHierarchyIncomingCallsResponse, error) {
	program := l.GetProgram()
	fileName := item.Uri.FileName()
	file := program.GetSourceFile(fileName)
	if file == nil {
		return lsproto.CallHierarchyIncomingCallsOrNull{}, nil
	}
	declarations := l.callHierarchyDeclarations(file, item.SelectionRange.Start, program, true)
	var calls []*lsproto.CallHierarchyIncomingCall
	seen := make(map[lsproto.Location]*lsproto.CallHierarchyIncomingCall)
	for _, declaration := range declarations {
		response, err := l.getIncomingCalls(ctx, program, declaration, orchestrator)
		if err != nil {
			return lsproto.CallHierarchyIncomingCallsOrNull{}, err
		}
		if response.CallHierarchyIncomingCalls != nil {
			for _, call := range *response.CallHierarchyIncomingCalls {
				location := lsproto.Location{Uri: call.From.Uri, Range: call.From.SelectionRange}
				if existing := seen[location]; existing != nil {
					for _, fromRange := range call.FromRanges {
						if !slices.Contains(existing.FromRanges, fromRange) {
							existing.FromRanges = append(existing.FromRanges, fromRange)
						}
					}
				} else {
					seen[location] = call
					calls = append(calls, call)
				}
			}
		}
	}
	if len(calls) == 0 {
		return lsproto.CallHierarchyIncomingCallsOrNull{}, nil
	}
	return lsproto.CallHierarchyIncomingCallsOrNull{CallHierarchyIncomingCalls: &calls}, nil
}
func (l *LanguageService) ProvideCallHierarchyOutgoingCalls(ctx context.Context, item *lsproto.CallHierarchyItem) (lsproto.CallHierarchyOutgoingCallsResponse, error) {
	program := l.GetProgram()
	fileName := item.Uri.FileName()
	file := program.GetSourceFile(fileName)
	if file == nil {
		return lsproto.CallHierarchyOutgoingCallsOrNull{}, nil
	}
	declarations := l.callHierarchyDeclarations(file, item.SelectionRange.Start, program, true)
	var calls []*lsproto.CallHierarchyOutgoingCall
	seen := make(map[lsproto.Location]*lsproto.CallHierarchyOutgoingCall)
	for _, declaration := range declarations {
		for _, call := range l.getOutgoingCalls(program, declaration) {
			location := lsproto.Location{Uri: call.To.Uri, Range: call.To.SelectionRange}
			if existing := seen[location]; existing != nil {
				for _, fromRange := range call.FromRanges {
					if !slices.Contains(existing.FromRanges, fromRange) {
						existing.FromRanges = append(existing.FromRanges, fromRange)
					}
				}
			} else {
				seen[location] = call
				calls = append(calls, call)
			}
		}
	}
	if len(calls) == 0 {
		return lsproto.CallHierarchyOutgoingCallsOrNull{}, nil
	}
	return lsproto.CallHierarchyOutgoingCallsOrNull{CallHierarchyOutgoingCalls: &calls}, nil
}
func (l *LanguageService) callHierarchyDeclarations(file *ast.SourceFile, position lsproto.Position, program *compiler.Program, allowSourceFile bool) []ast.Handle {
	positions := lsconv.FromLSPPositionForSourceFile(l.converters, file, position, spanmap.FeatureCallHierarchy)
	var declarations []ast.Handle
	var seen collections.Set[ast.Handle]
	for _, mapped := range positions {
		if !mapped.Fidelity.IsSingleSegment() {
			continue
		}
		file := mapped.Script
		pos := int(mapped.Position)
		node := file.ParseRoot()
		if pos != 0 {
			node = astnav.GetTouchingPropertyName(file, pos)
		}
		if node.IsNil() || !allowSourceFile && node.Kind == ast.KindSourceFile {
			continue
		}
		switch declaration := resolveCallHierarchyDeclaration(program, node).(type) {
		case ast.Handle:
			if seen.AddIfAbsent(declaration) {
				declarations = append(declarations, declaration)
			}
		case []ast.Handle:
			for _, declaration := range declaration {
				if seen.AddIfAbsent(declaration) {
					declarations = append(declarations, declaration)
				}
			}
		}
	}
	return declarations
}
