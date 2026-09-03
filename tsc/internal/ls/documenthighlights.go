package ls

import (
	"context"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsconv"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
)

func (l *LanguageService) ProvideDocumentHighlights(ctx context.Context, documentUri lsproto.DocumentUri, documentPosition lsproto.Position) (lsproto.DocumentHighlightResponse, error) {
	result, err := l.provideDocumentHighlightsWorker(ctx, documentUri, documentPosition, nil)
	if err != nil {
		return lsproto.DocumentHighlightsOrNull{}, err
	}
	var documentHighlights []*// Extract highlights for the current file only.
	// Cheap JSX check before resolving files to search.
	// Resolve the source files to search, deduplicating by file name.
	// Fall back to syntactic highlights for the current file only.
	// Group highlights by file
	// Determine write access for node references.
	// We'd like to highlight else/ifs together if they are only separated by whitespace
	// (i.e. the keywords are separated by no comments, no newlines).
	// this *should* always be an 'if' keyword.
	// Avoid recalculating getStart() by iterating backwards.
	// skip the next keyword
	// Ordinary case: just highlight the keyword.
	// We may be at an if statement like those in the range below:
	//
	//   ```
	//   if (...) {
	//   } else [|if (...) {}|]
	//   ````
	//
	// Traverse upwards through all parent if-statements linked by their else-branches.
	// See if the parent's `else` is actually the current `if` statement.
	// Traverse back down through the else branches, aggregating if/else keywords of if-statements.
	// Generally the 'else' keyword is second-to-last, so traverse backwards.
	// continue traversal
	// Get all throw statements not in a try block
	// Exceptions thrown within a try block lacking a catch clause are "owned" in the current context.
	// Do not cross function boundaries.
	// continue traversal
	// Aggregate all throw statements "owned" by this owner.
	// If the "owner" is a function, then we equate 'return' and 'throw' statements in their
	// ability to "jump out" of the function, and include occurrences for both
	// continue traversal
	// For lack of a better name, this function takes a throw statement and returns the
	// nearest ancestor that is a try-block (whose try statement has a catch clause),
	// function-block, or source file.
	// A throw-statement is only owned by a try-statement if the try-statement has
	// a catch clause, and if the throw-statement occurs within the try block.
	// If the statement is labeled, check if the node is labeled by the statement's label.
	// Don't cross function boundaries.
	// Whether or not a 'node' is preceded by a label of the given string.
	// Note: 'node' cannot be a SourceFile.
	// continue traversal
	// continue traversal
	// continue traversal
	// Types of node whose children might have modifiers.
	// Container is either a class declaration or the declaration is a classDeclaration
	// Parameters and, if inside a class, also class members
	// If we're an accessibility modifier, we're in an instance member and should search
	// the constructor's parameter list for instance members as well.
	// Syntactically invalid positions or unsupported containers
	lsproto.DocumentHighlight
	if result.MultiDocumentHighlights != nil {
		for _, mh := range *result.MultiDocumentHighlights {
			if mh.Uri == documentUri {
				documentHighlights = append(documentHighlights, mh.Highlights...)
			}
		}
	}
	return lsproto.DocumentHighlightsOrNull{DocumentHighlights: &documentHighlights}, nil
}
func (l *LanguageService) ProvideMultiDocumentHighlights(ctx context.Context, documentUri lsproto.DocumentUri, documentPosition lsproto.Position, filesToSearch []lsproto.DocumentUri) (lsproto.CustomMultiDocumentHighlightResponse, error) {
	return l.provideDocumentHighlightsWorker(ctx, documentUri, documentPosition, filesToSearch)
}
func (l *LanguageService) provideDocumentHighlightsWorker(ctx context.Context, documentUri lsproto.DocumentUri, documentPosition lsproto.Position, filesToSearch []lsproto.DocumentUri) (lsproto.MultiDocumentHighlightsOrNull, error) {
	program, sourceFile := l.getProgramAndFile(documentUri)
	positions := lsconv.FromLSPPositionForSourceFile(l.converters, sourceFile, documentPosition, spanmap.FeatureDocumentHighlights)
	results := make([]lsproto.MultiDocumentHighlightsOrNull, 0, len(positions))
	for _, mapped := range positions {
		if mapped.Fidelity.IsSingleSegment() {
			results = append(results, l.provideDocumentHighlightsAtPosition(ctx, documentUri, int(mapped.Position), program, mapped.Script, filesToSearch))
		}
	}
	return combineMultiDocumentHighlights(results), nil
}
func (l *LanguageService) provideDocumentHighlightsAtPosition(ctx context.Context, documentUri lsproto.DocumentUri, position int, program *compiler.Program, sourceFile *ast.SourceFile, filesToSearch []lsproto.DocumentUri) lsproto.MultiDocumentHighlightsOrNull {
	node := astnav.GetTouchingPropertyName(sourceFile, position)
	if !node.Parent().IsNil() && (node.Parent().Kind == ast.KindJsxClosingElement || (node.Parent().Kind == ast.KindJsxOpeningElement && node.Parent().TagName() == node)) {
		var openingElement, closingElement ast.Handle
		if ast.IsJsxElement(node.Parent().Parent()) {
			openingElement = node.Parent().Parent().JsxElementOpeningElement()
			closingElement = node.Parent().Parent().JsxElementClosingElement()
		}
		var highlights []*lsproto.DocumentHighlight
		kind := lsproto.DocumentHighlightKindRead
		if !openingElement.IsNil() {
			if lspRange, fidelity := l.createLspRangeFromNodeForFeature(openingElement, sourceFile, spanmap.FeatureDocumentHighlights); !fidelity.IsNone() {
				highlights = append(highlights, &lsproto.DocumentHighlight{Range: lspRange, Kind: &kind})
			}
		}
		if !closingElement.IsNil() {
			if lspRange, fidelity := l.createLspRangeFromNodeForFeature(closingElement, sourceFile, spanmap.FeatureDocumentHighlights); !fidelity.IsNone() {
				highlights = append(highlights, &lsproto.DocumentHighlight{Range: lspRange, Kind: &kind})
			}
		}
		multiHighlights := []*lsproto.MultiDocumentHighlight{{Uri: documentUri, Highlights: highlights}}
		return lsproto.MultiDocumentHighlightsOrNull{MultiDocumentHighlights: &multiHighlights}
	}
	var sourceFiles []*ast.SourceFile
	seenFiles := collections.NewSetWithSizeHint[string](len(filesToSearch))
	for _, uri := range filesToSearch {
		fileName := uri.FileName()
		if !seenFiles.AddIfAbsent(fileName) {
			continue
		}
		if sf := program.GetSourceFile(fileName); sf != nil {
			sourceFiles = append(sourceFiles, sf)
		}
	}
	if len(sourceFiles) == 0 {
		sourceFiles = []*ast.SourceFile{sourceFile}
	}
	multiHighlights := l.getSemanticDocumentHighlights(ctx, position, node, program, sourceFiles)
	if len(multiHighlights) == 0 {
		syntacticHighlights := l.getSyntacticDocumentHighlights(node, sourceFile)
		if len(syntacticHighlights) > 0 {
			multiHighlights = []*lsproto.MultiDocumentHighlight{{Uri: documentUri, Highlights: syntacticHighlights}}
		}
	}
	return lsproto.MultiDocumentHighlightsOrNull{MultiDocumentHighlights: &multiHighlights}
}
func combineMultiDocumentHighlights(results []lsproto.MultiDocumentHighlightsOrNull) lsproto.MultiDocumentHighlightsOrNull {
	byURI := make(map[lsproto.DocumentUri]*lsproto.MultiDocumentHighlight)
	seen := make(map[lsproto.DocumentUri]collections.Set[lsproto.Range])
	var combinedDocuments []*lsproto.MultiDocumentHighlight
	for _, result := range results {
		if result.MultiDocumentHighlights == nil {
			continue
		}
		for _, document := range *result.MultiDocumentHighlights {
			combinedDocument := byURI[document.Uri]
			if combinedDocument == nil {
				combinedDocument = &lsproto.MultiDocumentHighlight{Uri: document.Uri}
				byURI[document.Uri] = combinedDocument
				combinedDocuments = append(combinedDocuments, combinedDocument)
			}
			ranges := seen[document.Uri]
			for _, highlight := range document.Highlights {
				if ranges.AddIfAbsent(highlight.Range) {
					combinedDocument.Highlights = append(combinedDocument.Highlights, highlight)
				}
			}
			seen[document.Uri] = ranges
		}
	}
	return lsproto.MultiDocumentHighlightsOrNull{MultiDocumentHighlights: &combinedDocuments}
}
func (l *LanguageService) getSemanticDocumentHighlights(ctx context.Context, position int, node ast.Handle, program *compiler.Program, sourceFiles []*ast.SourceFile) []*lsproto.MultiDocumentHighlight {
	options := refOptions{use: referenceUseNone}
	referenceEntries := l.getReferencedSymbolsForNode(ctx, position, node, program, sourceFiles, options)
	if referenceEntries == nil {
		return nil
	}
	fileHighlights := make(map[string][]*lsproto.DocumentHighlight)
	for _, entry := range referenceEntries {
		for _, ref := range entry.references {
			fileName, highlight := l.toDocumentHighlight(ref)
			if highlight == nil {
				continue
			}
			fileHighlights[fileName] = append(fileHighlights[fileName], highlight)
		}
	}
	var result []*lsproto.MultiDocumentHighlight
	for _, sf := range sourceFiles {
		fileName := sf.OriginalFileName()
		if highlights, ok := fileHighlights[fileName]; ok {
			result = append(result, &lsproto.MultiDocumentHighlight{Uri: lsconv.FileNameToDocumentURI(fileName), Highlights: highlights})
		}
	}
	return result
}
func (l *LanguageService) toDocumentHighlight(entry *ReferenceEntry) (string, *lsproto.DocumentHighlight) {
	entry = l.resolveEntry(entry)
	fileName := entry.sourceFile.OriginalFileName()
	kind := lsproto.DocumentHighlightKindRead
	lspRange, ok := l.getRangeOfEntryForFeature(entry, spanmap.FeatureDocumentHighlights)
	if !ok {
		return fileName, nil
	}
	if entry.kind == entryKindRange {
		return fileName, &lsproto.DocumentHighlight{Range: lspRange, Kind: &kind}
	}
	if ast.IsWriteAccessForReference(entry.node) {
		kind = lsproto.DocumentHighlightKindWrite
	}
	dh := &lsproto.DocumentHighlight{Range: lspRange, Kind: &kind}
	return fileName, dh
}
func (l *LanguageService) getSyntacticDocumentHighlights(node ast.Handle, sourceFile *ast.SourceFile) []*lsproto.DocumentHighlight {
	switch node.Kind {
	case ast.KindIfKeyword, ast.KindElseKeyword:
		if ast.IsIfStatement(node.Parent()) {
			return l.getIfElseOccurrences(node.Parent(), sourceFile)
		}
		return nil
	case ast.KindReturnKeyword:
		return l.useParent(node.Parent(), ast.IsReturnStatement, getReturnOccurrences, sourceFile)
	case ast.KindThrowKeyword:
		return l.useParent(node.Parent(), ast.IsThrowStatement, getThrowOccurrences, sourceFile)
	case ast.KindTryKeyword, ast.KindCatchKeyword, ast.KindFinallyKeyword:
		var tryStatement ast.Handle
		if node.Kind == ast.KindCatchKeyword {
			tryStatement = node.Parent().Parent()
		} else {
			tryStatement = node.Parent()
		}
		return l.useParent(tryStatement, ast.IsTryStatement, getTryCatchFinallyOccurrences, sourceFile)
	case ast.KindSwitchKeyword:
		return l.useParent(node.Parent(), ast.IsSwitchStatement, getSwitchCaseDefaultOccurrences, sourceFile)
	case ast.KindCaseKeyword, ast.KindDefaultKeyword:
		if ast.IsDefaultClause(node.Parent()) || ast.IsCaseClause(node.Parent()) {
			return l.useParent(node.Parent().Parent().Parent(), ast.IsSwitchStatement, getSwitchCaseDefaultOccurrences, sourceFile)
		}
		return nil
	case ast.KindBreakKeyword, ast.KindContinueKeyword:
		return l.useParent(node.Parent(), ast.IsBreakOrContinueStatement, getBreakOrContinueStatementOccurrences, sourceFile)
	case ast.KindForKeyword, ast.KindWhileKeyword, ast.KindDoKeyword:
		return l.useParent(node.Parent(), func(n ast.Handle) bool {
			return ast.IsIterationStatement(n, true)
		}, getLoopBreakContinueOccurrences, sourceFile)
	case ast.KindConstructorKeyword:
		return l.getFromAllDeclarations(ast.IsConstructorDeclaration, []ast.Kind{ast.KindConstructorKeyword}, node, sourceFile)
	case ast.KindGetKeyword, ast.KindSetKeyword:
		return l.getFromAllDeclarations(ast.IsAccessor, []ast.Kind{ast.KindGetKeyword, ast.KindSetKeyword}, node, sourceFile)
	case ast.KindAwaitKeyword:
		return l.useParent(node.Parent(), ast.IsAwaitExpression, getAsyncAndAwaitOccurrences, sourceFile)
	case ast.KindAsyncKeyword:
		return l.highlightSpans(getAsyncAndAwaitOccurrences(node, sourceFile), sourceFile)
	case ast.KindYieldKeyword:
		return l.highlightSpans(getYieldOccurrences(node, sourceFile), sourceFile)
	case ast.KindInKeyword, ast.KindOutKeyword:
		return nil
	default:
		if ast.IsModifierKind(node.Kind) && (ast.IsDeclaration(node.Parent()) || ast.IsVariableStatement(node.Parent())) {
			return l.highlightSpans(getModifierOccurrences(node.Kind, node.Parent(), sourceFile), sourceFile)
		}
		return nil
	}
}
func (l *LanguageService) useParent(node ast.Handle, nodeTest func(ast.Handle) bool, getNodes func(ast.Handle, *ast.SourceFile) []ast.Handle, sourceFile *ast.SourceFile) []*lsproto.DocumentHighlight {
	if nodeTest(node) {
		return l.highlightSpans(getNodes(node, sourceFile), sourceFile)
	}
	return nil
}
func (l *LanguageService) highlightSpans(nodes []ast.Handle, sourceFile *ast.SourceFile) []*lsproto.DocumentHighlight {
	if len(nodes) == 0 {
		return nil
	}
	var highlights []*lsproto.DocumentHighlight
	kind := lsproto.DocumentHighlightKindRead
	for _, node := range nodes {
		if !node.IsNil() {
			if lspRange, fidelity := l.createLspRangeFromNodeForFeature(node, sourceFile, spanmap.FeatureDocumentHighlights); !fidelity.IsNone() {
				highlights = append(highlights, &lsproto.DocumentHighlight{Range: lspRange, Kind: &kind})
			}
		}
	}
	return highlights
}
func (l *LanguageService) getFromAllDeclarations(nodeTest func(ast.Handle) bool, keywords []ast.Kind, node ast.Handle, sourceFile *ast.SourceFile) []*lsproto.DocumentHighlight {
	return l.useParent(node.Parent(), nodeTest, func(decl ast.Handle, sf *ast.SourceFile) []ast.Handle {
		var symbolDecls []ast.Handle
		if ast.CanHaveSymbol(decl) {
			if symbol := decl.Symbol(); symbol != nil {
				for _, d := range ast.DeclarationNodes(symbol) {
					if nodeTest(d) {
					outer:
						for _, c := range getChildrenFromNonJSDocNode(d, sourceFile) {
							for _, k := range keywords {
								if c.Kind == k {
									symbolDecls = append(symbolDecls, c)
									break outer
								}
							}
						}
					}
				}
			}
		}
		return symbolDecls
	}, sourceFile)
}
func (l *LanguageService) getIfElseOccurrences(ifStatement ast.Handle, sourceFile *ast.SourceFile) []*lsproto.DocumentHighlight {
	keywords := getIfElseKeywords(ifStatement, sourceFile)
	kind := lsproto.DocumentHighlightKindRead
	var highlights []*lsproto.DocumentHighlight
	for i := 0; i < len(keywords); i++ {
		if keywords[i].Kind == ast.KindElseKeyword && i < len(keywords)-1 {
			elseKeyword := keywords[i]
			ifKeyword := keywords[i+1]
			shouldCombine := true
			ifTokenStart := scanner.GetTokenPosOfNode(ifKeyword, sourceFile, false)
			if ifTokenStart < 0 {
				ifTokenStart = ifKeyword.Pos()
			}
			for j := ifTokenStart - 1; j >= elseKeyword.End(); j-- {
				if !stringutil.IsWhiteSpaceSingleLine(rune(sourceFile.Text()[j])) {
					shouldCombine = false
					break
				}
			}
			if shouldCombine {
				lspRange, fidelity := l.createLspRangeFromBounds(scanner.SkipTrivia(sourceFile.Text(), elseKeyword.Pos()), ifKeyword.End(), sourceFile)
				if !fidelity.IsNone() {
					highlights = append(highlights, &lsproto.DocumentHighlight{Range: lspRange, Kind: &kind})
				}
				i++
				continue
			}
		}
		if lspRange, fidelity := l.createLspRangeFromNodeForFeature(keywords[i], sourceFile, spanmap.FeatureDocumentHighlights); !fidelity.IsNone() {
			highlights = append(highlights, &lsproto.DocumentHighlight{Range: lspRange, Kind: &kind})
		}
	}
	return highlights
}
func getIfElseKeywords(ifStatement ast.Handle, sourceFile *ast.SourceFile) []ast.Handle {
	for ast.IsIfStatement(ifStatement.Parent()) {
		parentingIf := ifStatement.Parent()
		elseStatement := parentingIf.ElseStatement()
		if elseStatement != ifStatement {
			break
		}
		ifStatement = parentingIf
	}
	var keywords []ast.Handle
	for {
		children := getChildrenFromNonJSDocNode(ifStatement, sourceFile)
		if len(children) > 0 && children[0].Kind == ast.KindIfKeyword {
			keywords = append(keywords, children[0])
		}
		for i := len(children) - 1; i >= 0; i-- {
			if children[i].Kind == ast.KindElseKeyword {
				keywords = append(keywords, children[i])
				break
			}
		}
		elseStatement := ifStatement.ElseStatement()
		if elseStatement.IsNil() || !ast.IsIfStatement(elseStatement) {
			break
		}
		ifStatement = elseStatement
	}
	return keywords
}
func getReturnOccurrences(node ast.Handle, sourceFile *ast.SourceFile) []ast.Handle {
	funcNode := ast.FindAncestor(node.Parent(), ast.IsFunctionLike)
	if funcNode.IsNil() {
		return nil
	}
	var keywords []ast.Handle
	body := funcNode.Body()
	if !body.IsNil() {
		ast.ForEachReturnStatement(body, func(ret ast.Handle) bool {
			keyword := astnav.FindChildOfKind(ret, ast.KindReturnKeyword, sourceFile)
			if !keyword.IsNil() {
				keywords = append(keywords, keyword)
			}
			return false
		})
		throwStatements := aggregateOwnedThrowStatements(body, sourceFile)
		for _, throw := range throwStatements {
			keyword := astnav.FindChildOfKind(throw, ast.KindThrowKeyword, sourceFile)
			if !keyword.IsNil() {
				keywords = append(keywords, keyword)
			}
		}
	}
	return keywords
}
func aggregateOwnedThrowStatements(node ast.Handle, sourceFile *ast.SourceFile) []ast.Handle {
	if ast.IsThrowStatement(node) {
		return []ast.Handle{node}
	}
	if ast.IsTryStatement(node) {
		statement := node
		tryBlock := statement.TryBlock()
		catchClause := statement.CatchClause()
		finallyBlock := statement.FinallyBlock()
		var result []ast.Handle
		if !catchClause.IsNil() {
			result = aggregateOwnedThrowStatements(catchClause, sourceFile)
		} else if !tryBlock.IsNil() {
			result = aggregateOwnedThrowStatements(tryBlock, sourceFile)
		}
		if !finallyBlock.IsNil() {
			result = append(result, aggregateOwnedThrowStatements(finallyBlock, sourceFile)...)
		}
		return result
	}
	if ast.IsFunctionLike(node) {
		return nil
	}
	return flatMapChildren(node, sourceFile, aggregateOwnedThrowStatements)
}
func flatMapChildren[T any](node ast.Handle, sourceFile *ast.SourceFile, cb func(child ast.Handle, sourceFile *ast.SourceFile) []T) []T {
	var result []T
	node.ForEachChild(func(child ast.Handle) bool {
		value := cb(child, sourceFile)
		if value != nil {
			result = append(result, value...)
		}
		return false
	})
	return result
}
func getThrowOccurrences(node ast.Handle, sourceFile *ast.SourceFile) []ast.Handle {
	owner := getThrowStatementOwner(node)
	if owner.IsNil() {
		return nil
	}
	var keywords []ast.Handle
	throwStatements := aggregateOwnedThrowStatements(owner, sourceFile)
	for _, throw := range throwStatements {
		keyword := astnav.FindChildOfKind(throw, ast.KindThrowKeyword, sourceFile)
		if !keyword.IsNil() {
			keywords = append(keywords, keyword)
		}
	}
	if ast.IsFunctionBlock(owner) {
		ast.ForEachReturnStatement(owner, func(ret ast.Handle) bool {
			keyword := astnav.FindChildOfKind(ret, ast.KindReturnKeyword, sourceFile)
			if !keyword.IsNil() {
				keywords = append(keywords, keyword)
			}
			return false
		})
	}
	return keywords
}

func getThrowStatementOwner(throwStatement ast.Handle) ast.Handle {
	child := throwStatement
	for !child.Parent().IsNil() {
		parent := child.Parent()
		if ast.IsFunctionBlock(parent) || parent.Kind == ast.KindSourceFile {
			return parent
		}
		if ast.IsTryStatement(parent) {
			tryStatement := parent
			if tryStatement.TryBlock() == child && !tryStatement.CatchClause().IsNil() {
				return child
			}
		}
		child = parent
	}
	return ast.Handle{}
}
func getTryCatchFinallyOccurrences(node ast.Handle, sourceFile *ast.SourceFile) []ast.Handle {
	tryStatement := node
	var keywords []ast.Handle
	token := lsutil.GetFirstToken(node, sourceFile)
	if !token.IsNil() && token.Kind == ast.KindTryKeyword {
		keywords = append(keywords, token)
	}
	if !tryStatement.CatchClause().IsNil() {
		if catchToken := astnav.FindChildOfKind(node, ast.KindCatchKeyword, sourceFile); !catchToken.IsNil() {
			keywords = append(keywords, catchToken)
		}
	}
	if !tryStatement.FinallyBlock().IsNil() {
		if finallyKeyword := astnav.FindChildOfKind(node, ast.KindFinallyKeyword, sourceFile); !finallyKeyword.IsNil() {
			keywords = append(keywords, finallyKeyword)
		}
	}
	return keywords
}
func getSwitchCaseDefaultOccurrences(node ast.Handle, sourceFile *ast.SourceFile) []ast.Handle {
	switchStatement := node
	var keywords []ast.Handle
	token := lsutil.GetFirstToken(node, sourceFile)
	if token.Kind == ast.KindSwitchKeyword {
		keywords = append(keywords, token)
	}
	clauses := switchStatement.CaseBlock().CaseBlockClauses()
	for _, clause := range node.Store().ListSlice(clauses) {
		clauseToken := lsutil.GetFirstToken(clause, sourceFile)
		if clauseToken.Kind == ast.KindCaseKeyword || clauseToken.Kind == ast.KindDefaultKeyword {
			keywords = append(keywords, clauseToken)
		}
		breakAndContinueStatements := aggregateAllBreakAndContinueStatements(clause, sourceFile)
		for _, statement := range breakAndContinueStatements {
			if statement.Kind == ast.KindBreakStatement && ownsBreakOrContinueStatement(switchStatement, statement) {
				keywords = append(keywords, lsutil.GetFirstToken(statement, sourceFile))
			}
		}
	}
	return keywords
}
func aggregateAllBreakAndContinueStatements(node ast.Handle, sourceFile *ast.SourceFile) []ast.Handle {
	if ast.IsBreakOrContinueStatement(node) {
		return []ast.Handle{node}
	}
	if ast.IsFunctionLike(node) {
		return nil
	}
	return flatMapChildren(node, sourceFile, aggregateAllBreakAndContinueStatements)
}
func ownsBreakOrContinueStatement(owner ast.Handle, statement ast.Handle) bool {
	actualOwner := getBreakOrContinueOwner(statement)
	if actualOwner.IsNil() {
		return false
	}
	return actualOwner == owner
}
func getBreakOrContinueOwner(statement ast.Handle) ast.Handle {
	return ast.FindAncestorOrQuit(statement, func(node ast.Handle) ast.FindAncestorResult {
		switch node.Kind {
		case ast.KindSwitchStatement:
			if statement.Kind == ast.KindContinueStatement {
				return ast.FindAncestorFalse
			}
			fallthrough
		case ast.KindForStatement, ast.KindForInStatement, ast.KindForOfStatement, ast.KindWhileStatement, ast.KindDoStatement:
			if statement.Label().IsNil() || isLabeledBy(node, statement.Label().Text()) {
				return ast.FindAncestorTrue
			}
			return ast.FindAncestorFalse
		default:
			if ast.IsFunctionLike(node) {
				return ast.FindAncestorQuit
			}
			return ast.FindAncestorFalse
		}
	})
}

func isLabeledBy(node ast.Handle, labelName string) bool {
	return !ast.FindAncestorOrQuit(node.Parent(), func(owner ast.Handle) ast.FindAncestorResult {
		if !ast.IsLabeledStatement(owner) {
			return ast.FindAncestorQuit
		}
		if owner.Label().Text() == labelName {
			return ast.FindAncestorTrue
		}
		return ast.FindAncestorFalse
	}).IsNil()
}
func getBreakOrContinueStatementOccurrences(node ast.Handle, sourceFile *ast.SourceFile) []ast.Handle {
	if owner := getBreakOrContinueOwner(node); !owner.IsNil() {
		switch owner.Kind {
		case ast.KindForStatement, ast.KindForInStatement, ast.KindForOfStatement, ast.KindDoStatement, ast.KindWhileStatement:
			return getLoopBreakContinueOccurrences(owner, sourceFile)
		case ast.KindSwitchStatement:
			return getSwitchCaseDefaultOccurrences(owner, sourceFile)
		}
	}
	return nil
}
func getLoopBreakContinueOccurrences(node ast.Handle, sourceFile *ast.SourceFile) []ast.Handle {
	var keywords []ast.Handle
	token := lsutil.GetFirstToken(node, sourceFile)
	if token.Kind == ast.KindForKeyword || token.Kind == ast.KindDoKeyword || token.Kind == ast.KindWhileKeyword {
		keywords = append(keywords, token)
		if node.Kind == ast.KindDoStatement {
			loopTokens := getChildrenFromNonJSDocNode(node, sourceFile)
			for i := len(loopTokens) - 1; i >= 0; i-- {
				if loopTokens[i].Kind == ast.KindWhileKeyword {
					keywords = append(keywords, loopTokens[i])
					break
				}
			}
		}
	}
	breakAndContinueStatements := aggregateAllBreakAndContinueStatements(node, sourceFile)
	for _, statement := range breakAndContinueStatements {
		token := lsutil.GetFirstToken(statement, sourceFile)
		if ownsBreakOrContinueStatement(node, statement) && (token.Kind == ast.KindBreakKeyword || token.Kind == ast.KindContinueKeyword) {
			keywords = append(keywords, token)
		}
	}
	return keywords
}
func getAsyncAndAwaitOccurrences(node ast.Handle, sourceFile *ast.SourceFile) []ast.Handle {
	fun := ast.GetContainingFunction(node)
	if fun.IsNil() {
		return nil
	}
	var keywords []ast.Handle
	for _, modifier := range fun.ModifierNodes() {
		if modifier.Kind == ast.KindAsyncKeyword {
			keywords = append(keywords, modifier)
		}
	}
	fun.ForEachChild(func(child ast.Handle) bool {
		traverseWithoutCrossingFunction(child, sourceFile, func(child ast.Handle) {
			if ast.IsAwaitExpression(child) {
				token := lsutil.GetFirstToken(child, sourceFile)
				if token.Kind == ast.KindAwaitKeyword {
					keywords = append(keywords, token)
				}
			}
		})
		return false
	})
	return keywords
}
func getYieldOccurrences(node ast.Handle, sourceFile *ast.SourceFile) []ast.Handle {
	parentFunc := ast.FindAncestor(node.Parent(), ast.IsFunctionLike)
	if parentFunc.IsNil() {
		return nil
	}
	var keywords []ast.Handle
	parentFunc.ForEachChild(func(child ast.Handle) bool {
		traverseWithoutCrossingFunction(child, sourceFile, func(child ast.Handle) {
			if ast.IsYieldExpression(child) {
				token := lsutil.GetFirstToken(child, sourceFile)
				if token.Kind == ast.KindYieldKeyword {
					keywords = append(keywords, token)
				}
			}
		})
		return false
	})
	return keywords
}
func traverseWithoutCrossingFunction(node ast.Handle, sourceFile *ast.SourceFile, cb func(ast.Handle)) {
	cb(node)
	if !ast.IsFunctionLike(node) && !ast.IsClassLike(node) && !ast.IsInterfaceDeclaration(node) && !ast.IsModuleDeclaration(node) && !ast.IsTypeAliasDeclaration(node) && !ast.IsTypeNode(node) {
		node.ForEachChild(func(child ast.Handle) bool {
			traverseWithoutCrossingFunction(child, sourceFile, cb)
			return false
		})
	}
}
func getModifierOccurrences(kind ast.Kind, node ast.Handle, sourceFile *ast.SourceFile) []ast.Handle {
	var result []ast.Handle
	nodesToSearch := getNodesToSearchForModifier(node, ast.ModifierToFlag(kind))
	for _, n := range nodesToSearch {
		modifier := findModifier(n, kind)
		if !modifier.IsNil() {
			result = append(result, modifier)
		}
	}
	return result
}
func getNodesToSearchForModifier(declaration ast.Handle, modifierFlag ast.ModifierFlags) []ast.Handle {
	var result []ast.Handle
	container := declaration.Parent()
	if container.IsNil() {
		return nil
	}
	switch container.Kind {
	case ast.KindModuleBlock, ast.KindSourceFile, ast.KindBlock, ast.KindCaseClause, ast.KindDefaultClause:
		if (modifierFlag&ast.ModifierFlagsAbstract) != 0 && ast.IsClassDeclaration(declaration) {
			return append(append(result, declaration.Members()...), declaration)
		} else {
			return append(result, container.Statements()...)
		}
	case ast.KindConstructor, ast.KindMethodDeclaration, ast.KindFunctionDeclaration:
		result = append(result, container.Parameters()...)
		if ast.IsClassLike(container.Parent()) {
			result = append(result, container.Parent().Members()...)
		}
		return result
	case ast.KindClassDeclaration, ast.KindClassExpression, ast.KindInterfaceDeclaration, ast.KindTypeLiteral:
		nodes := container.Members()
		result = append(result, nodes...)
		if (modifierFlag & (ast.ModifierFlagsAccessibilityModifier | ast.ModifierFlagsReadonly)) != 0 {
			var constructor ast.Handle
			for _, member := range nodes {
				if ast.IsConstructorDeclaration(member) {
					constructor = member
					break
				}
			}
			if !constructor.IsNil() {
				result = append(result, constructor.Parameters()...)
			}
		} else if (modifierFlag & ast.ModifierFlagsAbstract) != 0 {
			result = append(result, container)
		}
		return result
	default:
		return nil
	}
}
func findModifier(node ast.Handle, kind ast.Kind) ast.Handle {
	for _, modifier := range node.ModifierNodes() {
		if modifier.Kind == kind {
			return modifier
		}
	}
	return ast.Handle{}
}
