package change

import (
	"context"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/format"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsconv"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"slices"
)

type NodeOptions struct {
	Prefix      string
	Suffix      string
	indentation *int
	delta       *int
	LeadingTriviaOption
	TrailingTriviaOption
	joiner string
}
type LeadingTriviaOption int

const (
	LeadingTriviaOptionNone       LeadingTriviaOption = 0
	LeadingTriviaOptionExclude    LeadingTriviaOption = 1
	LeadingTriviaOptionIncludeAll LeadingTriviaOption = 2
	LeadingTriviaOptionJSDoc      LeadingTriviaOption = 3
	LeadingTriviaOptionStartLine  LeadingTriviaOption = 4
)

type TrailingTriviaOption int

const (
	TrailingTriviaOptionNone              TrailingTriviaOption = 0
	TrailingTriviaOptionExclude           TrailingTriviaOption = 1
	TrailingTriviaOptionExcludeWhitespace TrailingTriviaOption = 2
	TrailingTriviaOptionInclude           TrailingTriviaOption = 3
)

type trackerEditKind int

const (
	trackerEditKindText                     trackerEditKind = 1
	trackerEditKindRemove                   trackerEditKind = 2
	trackerEditKindReplaceWithSingleNode    trackerEditKind = 3
	trackerEditKindReplaceWithMultipleNodes trackerEditKind = 4
)

type trackerEdit struct {
	kind trackerEditKind
	lsproto.Range
	Node    ast.Handle
	NewText string // Text to be inserted before the new node
	// Text to be inserted after the new node
	// Text of inserted node will be formatted with this indentation, otherwise indentation will be inferred from the old node
	// Text of inserted node will be formatted with this delta, otherwise delta will be inferred from the new node kind
	// kind == text
	// single
	// multiple
	// initialized with
	// unmappableFiles collects the files for which an edit could not be represented within a single
	// verbatim span of the original text. GetChanges drops their edits so a partial, corrupting change is
	// never emitted for a content-mapped file.
	// created during call to getChanges
	// printer
	// !!! formatSettings in context?
	// GetChanges returns the accumulated text edits grouped by file name. Any file whose edits could not be
	// faithfully mapped back onto content-mapped original text is omitted from the returned map, and its name
	// is included in the returned slice. Dropping the whole file (rather than the individual edit) keeps a
	// logical change atomic, and returning the result inline means a caller cannot forget to check it or
	// accidentally emit a partial, corrupting change.
	// Note: after calling this, the Tracker object must be discarded!
	// !!! changes for new files
	// toLSPEditRange converts a transformed-text range to an LSP range for an edit, mapping through the
	// content mapper's span map when the file is content-mapped. If the range does not fall entirely within a
	// single verbatim span the edit cannot be represented safely in the original text: the file is recorded so
	// GetChanges drops its edits, and a best-effort range is returned so the accumulated edits stay well-formed.
	// The range does not map into a single verbatim span, so the edit cannot be represented safely in
	// the original text. Record the file so GetChanges drops its edits, keeping the best-effort range so
	// the accumulated edits stay well-formed.
	// toLSPEditPos converts a transformed-text offset to an LSP position for a zero-length edit (an insertion),
	// applying the same content-mapping safety check as toLSPEditRange.
	// defaults to `useNonAdjustedPositions`
	// ReplaceTextRangeWithText replaces textRange (in transformed-text coordinates) with text, mapping the
	// range through the content-mapping guard so an edit that cannot be represented in the original text marks
	// the file unmappable and is dropped by GetChanges.
	// TryInsertTypeAnnotation inserts a type annotation after the appropriate position on a node
	// (after the close paren for function-like, after the name/exclamation/question for variable-like).
	// Returns true if successful.
	// If no `)`, is an arrow function `x => x`, so use the end of the first parameter
	// ParenthesizeArrowParameters wraps the parameters of a paren-less arrow function in `(` and `)`.
	// This is a no-op if the arrow function already has parens.
	// InsertModifierBefore inserts a modifier token (like 'type') before a node with a trailing space.
	// Delete queues a node for deletion with smart handling of list items, imports, etc.
	// The actual deletion happens in finishDeleteDeclarations during GetChanges.
	// DeleteRange deletes a text range from the source file.
	// DeleteNode deletes a node immediately with specified trivia options.
	// Stop! Consider using Delete instead, which has logic for deleting nodes from delimited lists.
	// DeleteNodeRange deletes a range of nodes with specified trivia options.
	// finishDeleteDeclarations processes all queued deletions with smart handling for lists and trailing commas.
	// Skip if this node is contained within another deleted node
	// Handle trailing commas for last elements in lists
	// check if previous statement ends with semicolon
	// if not - insert semicolon to preserve the code from changing the meaning due to ASI
	/**
	* This function should be used to insert nodes in lists when nodes don't carry separators as the part of the node range,
	* i.e. arguments in arguments lists, parameters in parameter lists etc.
	* Note that separators are part of the node in statements and class elements.
	 */ // Debug.fail("node is not a list element")
	// any element except the last one
	// use next sibling as an anchor
	// for list
	// a, b, c
	// create change for adding 'e' after 'a' as
	// - find start of next element after a (it is b)
	// - use next element start as start and end position in final change
	// - build text of change by formatting the text of node + whitespace trivia of b
	// in multiline case it will work as
	//   a,
	//   b,
	//   c,
	// result - '*' denotes leading trivia that will be inserted after new text (displayed as '#')
	//   a,
	//   insertedtext<separator>#
	// ###b,
	//   c,
	// write separator and leading trivia of the next element as suffix
	// insert element after the last element in the list that has more than one item
	// pick the element preceding the after element to:
	// - pick the separator
	// - determine if list is a multiline
	// if list has only one element then we'll format is as multiline if node has comment in trailing trivia, or as singleline otherwise
	// i.e. var x = 1 // this is x
	//     | new element will be inserted at this position
	// SyntaxKind.CommaToken | SyntaxKind.SemicolonToken
	// otherwise, if list has more than one element, pick separator from the list
	// determine if list is multiline by checking lines of after element and element that precedes it.
	// in this case we'll always treat containing list as multiline
	// insert separator immediately following the 'after' node to preserve comments in trailing trivia
	// use the same indentation as 'after' item
	// insert element before the line break on the line that contains 'after' element
	// find position before "\n" or "\r\n"
	// InsertImportSpecifierAtIndex inserts a new import specifier at the specified index in a NamedImports list
	// A content mapper may synthesize a header. Advance to the first writable segment so the insertion
	// maps exactly to the original file, and use its original position when deciding leading trivia.
	// default opts
	// Else we haven't handled this kind of node yet -- add it
	// insert `x = 1, ` into `const x = 1, y = 2;
	// We haven't handled this kind of node yet -- add it
	ast.Handle
	nodes   []ast.Handle
	options NodeOptions
}
type nodesInsertedAtStartState struct {
	node       ast.Handle
	sourceFile *ast.SourceFile
}
type Tracker struct {
	formatSettings lsutil.FormatCodeSettings
	newLine        string
	converters     *lsconv.Converters
	ctx            context.Context
	*printer.EmitContext
	ast.HandleFactory
	changes                    *collections.MultiMap[*ast.SourceFile, *trackerEdit]
	deletedNodes               []deletedNode
	nodesWithInsertionsAtStart map[ast.Handle]*nodesInsertedAtStartState
	unmappableFiles            collections.Set[string]
	writer                     *printer.ChangeTrackerWriter
}
type deletedNode struct {
	sourceFile *ast.SourceFile
	node       ast.Handle
}

func NewTracker(ctx context.Context, compilerOptions *core.CompilerOptions, formatOptions lsutil.FormatCodeSettings, converters *lsconv.Converters) *Tracker {
	emitContext := printer.NewEmitContext()
	newLine := compilerOptions.NewLine.GetNewLineCharacter()
	ctx = format.WithFormatCodeSettings(ctx, formatOptions, newLine)
	return &Tracker{EmitContext: emitContext, HandleFactory: emitContext.StoreFactory(), changes: &collections.MultiMap[*ast.SourceFile, *trackerEdit]{}, ctx: ctx, converters: converters, formatSettings: formatOptions, newLine: newLine, nodesWithInsertionsAtStart: make(map[ast.Handle]*nodesInsertedAtStartState)}
}

func (t *Tracker) GetChanges() (map[string][]*lsproto.TextEdit, []string) {
	t.finishDeleteDeclarations()
	t.finishNodesWithInsertionsAtStart()
	changes := t.getTextChangesFromChanges()
	if t.unmappableFiles.Len() == 0 {
		return changes, nil
	}
	unmappable := make([]string, 0, t.unmappableFiles.Len())
	for fileName := range t.unmappableFiles.Keys() {
		delete(changes, fileName)
		unmappable = append(unmappable, fileName)
	}
	slices.Sort(unmappable)
	return changes, unmappable
}

func (t *Tracker) toLSPEditRange(sourceFile *ast.SourceFile, textRange core.TextRange) lsproto.Range {
	r, fidelity := t.converters.ToLSPRange(sourceFile, textRange)
	if !fidelity.IsExact() {
		t.unmappableFiles.Add(sourceFile.OriginalFileName())
	}
	return r
}

func (t *Tracker) toLSPEditPos(sourceFile *ast.SourceFile, pos core.TextPos) lsproto.Position {
	return t.toLSPEditRange(sourceFile, core.NewTextRange(int(pos), int(pos))).Start
}
func (t *Tracker) ReplaceNode(sourceFile *ast.SourceFile, oldNode ast.Handle, newNode ast.Handle, options *NodeOptions) {
	if options == nil {
		options = &NodeOptions{LeadingTriviaOption: LeadingTriviaOptionExclude, TrailingTriviaOption: TrailingTriviaOptionExclude}
	}
	t.ReplaceRange(sourceFile, t.GetAdjustedRange(sourceFile, oldNode, oldNode, options.LeadingTriviaOption, options.TrailingTriviaOption), newNode, *options)
}
func (t *Tracker) ReplaceNodeWithNodes(sourceFile *ast.SourceFile, oldNode ast.Handle, newNodes []ast.Handle, options *NodeOptions) {
	if options == nil {
		options = &NodeOptions{LeadingTriviaOption: LeadingTriviaOptionExclude, TrailingTriviaOption: TrailingTriviaOptionExclude}
	}
	t.ReplaceRangeWithNodes(sourceFile, t.GetAdjustedRange(sourceFile, oldNode, oldNode, options.LeadingTriviaOption, options.TrailingTriviaOption), newNodes, *options)
}
func (t *Tracker) ReplaceRange(sourceFile *ast.SourceFile, lsprotoRange lsproto.Range, newNode ast.Handle, options NodeOptions) {
	t.changes.Add(sourceFile, &trackerEdit{kind: trackerEditKindReplaceWithSingleNode, Range: lsprotoRange, options: options, Node: newNode})
}
func (t *Tracker) ReplaceRangeWithText(sourceFile *ast.SourceFile, lsprotoRange lsproto.Range, text string) {
	t.changes.Add(sourceFile, &trackerEdit{kind: trackerEditKindText, Range: lsprotoRange, NewText: text})
}

func (t *Tracker) ReplaceTextRangeWithText(sourceFile *ast.SourceFile, textRange core.TextRange, text string) {
	t.ReplaceRangeWithText(sourceFile, t.toLSPEditRange(sourceFile, textRange), text)
}
func (t *Tracker) ReplaceRangeWithNodes(sourceFile *ast.SourceFile, lsprotoRange lsproto.Range, newNodes []ast.Handle, options NodeOptions) {
	if len(newNodes) == 1 {
		t.ReplaceRange(sourceFile, lsprotoRange, newNodes[0], options)
		return
	}
	t.changes.Add(sourceFile, &trackerEdit{kind: trackerEditKindReplaceWithMultipleNodes, Range: lsprotoRange, nodes: newNodes, options: options})
}
func (t *Tracker) InsertText(sourceFile *ast.SourceFile, pos lsproto.Position, text string) {
	t.ReplaceRangeWithText(sourceFile, lsproto.Range{Start: pos, End: pos}, text)
}
func (t *Tracker) InsertNodeAt(sourceFile *ast.SourceFile, pos core.TextPos, newNode ast.Handle, options NodeOptions) {
	lsPos := t.toLSPEditPos(sourceFile, pos)
	t.ReplaceRange(sourceFile, lsproto.Range{Start: lsPos, End: lsPos}, newNode, options)
}
func (t *Tracker) InsertNodesAt(sourceFile *ast.SourceFile, pos core.TextPos, newNodes []ast.Handle, options NodeOptions) {
	lsPos := t.toLSPEditPos(sourceFile, pos)
	t.ReplaceRangeWithNodes(sourceFile, lsproto.Range{Start: lsPos, End: lsPos}, newNodes, options)
}
func (t *Tracker) InsertNodeAfter(sourceFile *ast.SourceFile, after ast.Handle, newNode ast.Handle) {
	endPosition := t.endPosForInsertNodeAfter(sourceFile, after, newNode)
	t.InsertNodeAt(sourceFile, endPosition, newNode, t.getInsertNodeAfterOptions(sourceFile, after))
}
func (t *Tracker) InsertNodesAfter(sourceFile *ast.SourceFile, after ast.Handle, newNodes []ast.Handle) {
	endPosition := t.endPosForInsertNodeAfter(sourceFile, after, newNodes[0])
	t.InsertNodesAt(sourceFile, endPosition, newNodes, t.getInsertNodeAfterOptions(sourceFile, after))
}
func (t *Tracker) InsertNodeBefore(sourceFile *ast.SourceFile, before ast.Handle, newNode ast.Handle, blankLineBetween bool, leadingTriviaOption LeadingTriviaOption) {
	t.InsertNodeAt(sourceFile, core.TextPos(t.getAdjustedStartPosition(sourceFile, before, leadingTriviaOption, false)), newNode, t.getOptionsForInsertNodeBefore(before, newNode, blankLineBetween))
}

func (t *Tracker) TryInsertTypeAnnotation(sourceFile *ast.SourceFile, node ast.Handle, typeNode ast.Handle) bool {
	var endNode ast.Handle
	if ast.IsFunctionLike(node) {
		endNode = astnav.FindChildOfKind(node, ast.KindCloseParenToken, sourceFile)
		if endNode.IsNil() {
			if !ast.IsArrowFunction(node) {
				return false
			}
			params := node.Parameters()
			if len(params) == 0 {
				return false
			}
			endNode = params[0]
		}
	} else {
		switch node.Kind {
		case ast.KindVariableDeclaration:
			endNode = node.VariableDeclarationExclamationToken()
		case ast.KindPropertySignature:
			endNode = node.PropertySignatureDeclarationPostfixToken()
		case ast.KindPropertyDeclaration:
			endNode = node.PropertyDeclarationPostfixToken()
		case ast.KindParameter:
			endNode = node.ParameterDeclarationQuestionToken()
		}
		if endNode.IsNil() {
			endNode = node.Name()
		}
	}
	if endNode.IsNil() {
		return false
	}
	t.InsertNodeAt(sourceFile, core.TextPos(endNode.End()), typeNode, NodeOptions{Prefix: ": "})
	return true
}

func (t *Tracker) ParenthesizeArrowParameters(sourceFile *ast.SourceFile, arrowFunc ast.Handle) {
	if !astnav.FindChildOfKind(arrowFunc, ast.KindCloseParenToken, sourceFile).IsNil() {
		return
	}
	params := arrowFunc.Parameters()
	if len(params) == 0 {
		return
	}
	firstParam := params[0]
	lastParam := params[len(params)-1]
	startPos := astnav.GetStartOfNode(firstParam, sourceFile, false)
	t.InsertText(sourceFile, t.toLSPEditPos(sourceFile, core.TextPos(startPos)), "(")
	t.InsertText(sourceFile, t.toLSPEditPos(sourceFile, core.TextPos(lastParam.End())), ")")
}

func (t *Tracker) InsertModifierBefore(sourceFile *ast.SourceFile, modifier ast.Kind, before ast.Handle) {
	pos := astnav.GetStartOfNode(before, sourceFile, false)
	token := t.NewToken(modifier)
	token.SetLoc(core.NewTextRange(pos, pos))
	token.SetParent(before.Parent())
	t.InsertNodeAt(sourceFile, core.TextPos(pos), token, NodeOptions{Suffix: " "})
}

func (t *Tracker) Delete(sourceFile *ast.SourceFile, node ast.Handle) {
	t.deletedNodes = append(t.deletedNodes, deletedNode{sourceFile: sourceFile, node: node})
}

func (t *Tracker) DeleteRange(sourceFile *ast.SourceFile, textRange core.TextRange) {
	lspRange := t.toLSPEditRange(sourceFile, textRange)
	t.ReplaceRangeWithText(sourceFile, lspRange, "")
}

func (t *Tracker) DeleteNode(sourceFile *ast.SourceFile, node ast.Handle, leadingTrivia LeadingTriviaOption, trailingTrivia TrailingTriviaOption) {
	rng := t.GetAdjustedRange(sourceFile, node, node, leadingTrivia, trailingTrivia)
	t.ReplaceRangeWithText(sourceFile, rng, "")
}

func (t *Tracker) DeleteNodeRange(sourceFile *ast.SourceFile, startNode ast.Handle, endNode ast.Handle, leadingTrivia LeadingTriviaOption, trailingTrivia TrailingTriviaOption) {
	startPosition := t.getAdjustedStartPosition(sourceFile, startNode, leadingTrivia, false)
	endPosition := t.getAdjustedEndPosition(sourceFile, endNode, trailingTrivia)
	t.ReplaceRangeWithText(sourceFile, t.toLSPEditRange(sourceFile, core.NewTextRange(startPosition, endPosition)), "")
}

func (t *Tracker) finishDeleteDeclarations() {
	deletedNodesInLists := make(map[ast.Handle]bool)
	for _, deleted := range t.deletedNodes {
		isContained := false
		for _, other := range t.deletedNodes {
			if other.sourceFile == deleted.sourceFile && other.node != deleted.node && rangeContainsRangeExclusive(other.node, deleted.node) {
				isContained = true
				break
			}
		}
		if isContained {
			continue
		}
		deleteDeclaration(t, deletedNodesInLists, deleted.sourceFile, deleted.node)
	}
	for node := range deletedNodesInLists {
		sourceFile := ast.GetSourceFileOfNode(node)
		list := format.GetContainingList(node, sourceFile)
		s := sourceFile.ParseStore()
		n := s.ListLen(list)
		if list == 0 || n == 0 || node != s.ListAt(list, n-1) {
			continue
		}
		lastNonDeletedIndex := -1
		for i := n - 2; i >= 0; i-- {
			if !deletedNodesInLists[s.ListAt(list, i)] {
				lastNonDeletedIndex = i
				break
			}
		}
		if lastNonDeletedIndex != -1 {
			start := s.ListAt(list, lastNonDeletedIndex).End()
			end := t.startPositionToDeleteNodeInList(sourceFile, s.ListAt(list, lastNonDeletedIndex+1))
			t.ReplaceRangeWithText(sourceFile, t.toLSPEditRange(sourceFile, core.NewTextRange(start, end)), "")
		}
	}
}
func (t *Tracker) endPosForInsertNodeAfter(sourceFile *ast.SourceFile, after ast.Handle, newNode ast.Handle) core.TextPos {
	if needSemicolonBetween(after, newNode) && (rune(sourceFile.Text()[after.End()-1]) != ';') {
		endPos := t.toLSPEditPos(sourceFile, core.TextPos(after.End()))
		semicolon := t.NewToken(ast.KindSemicolonToken)
		semicolon.SetLoc(core.NewTextRange(after.End(), after.End()))
		semicolon.SetParent(after.Parent())
		t.ReplaceRange(sourceFile, lsproto.Range{Start: endPos, End: endPos}, semicolon, NodeOptions{})
	}
	return core.TextPos(t.getAdjustedEndPosition(sourceFile, after, TrailingTriviaOptionNone))
}

func (t *Tracker) InsertNodeInListAfter(sourceFile *ast.SourceFile, after ast.Handle, newNode ast.Handle, containingList ast.ListRef) {
	if containingList == 0 {
		containingList = format.GetContainingList(after, sourceFile)
	}
	if containingList == 0 {
		return
	}
	index := after.Store().ListIndexOf(containingList, after)
	if index < 0 {
		return
	}
	end := after.End()
	if index != after.Store().ListLen(containingList)-1 {
		if nextToken := astnav.GetTokenAtPosition(sourceFile, after.End()); !nextToken.IsNil() && isSeparator(after, nextToken) {
			nextNode := after.Store().ListAt(containingList, index+1)
			startPos := scanner.SkipTriviaEx(sourceFile.Text(), nextNode.Pos(), &scanner.SkipTriviaOptions{StopAfterLineBreak: false, StopAtComments: true})
			suffix := scanner.TokenToString(nextToken.Kind) + sourceFile.Text()[nextToken.End():startPos]
			t.InsertNodesAt(sourceFile, core.TextPos(startPos), []ast.Handle{newNode}, NodeOptions{Suffix: suffix})
		}
		return
	}
	afterStart := astnav.GetStartOfNode(after, sourceFile, false)
	afterStartLinePosition := format.GetLineStartPositionForPosition(afterStart, sourceFile)
	multilineList := false
	separator := ast.KindCommaToken
	if after.Store().ListLen(containingList) != 1 {
		tokenBeforeInsertPosition := astnav.FindPrecedingToken(sourceFile, after.Pos())
		separator = core.IfElse(isSeparator(after, tokenBeforeInsertPosition), tokenBeforeInsertPosition.Kind, ast.KindCommaToken)
		afterMinusOneStartLinePosition := format.GetLineStartPositionForPosition(astnav.GetStartOfNode(after.Store().ListAt(containingList, index-1), sourceFile, false), sourceFile)
		multilineList = afterMinusOneStartLinePosition != afterStartLinePosition
	}
	if hasCommentsBeforeLineBreak(sourceFile.Text(), after.End()) || !positionsAreOnSameLine(after.Store().ListLoc(containingList).Pos(), after.Store().ListLoc(containingList).End(), sourceFile) {
		multilineList = true
	}
	if multilineList {
		separatorToken := t.NewToken(separator)
		separatorString := scanner.TokenToString(separator)
		separatorToken.SetLoc(core.NewTextRange(end, end+len(separatorString)))
		separatorToken.SetParent(after.Parent())
		endPos := t.toLSPEditPos(sourceFile, core.TextPos(end))
		t.ReplaceRange(sourceFile, lsproto.Range{Start: endPos, End: endPos}, separatorToken, NodeOptions{})
		indentation := format.FindFirstNonWhitespaceColumn(afterStartLinePosition, afterStart, sourceFile, t.formatSettings)
		insertPos := scanner.SkipTriviaEx(sourceFile.Text(), end, &scanner.SkipTriviaOptions{StopAfterLineBreak: true, StopAtComments: false})
		for insertPos != end && stringutil.IsLineBreak(rune(sourceFile.Text()[insertPos-1])) {
			insertPos--
		}
		insertLSPos := t.toLSPEditPos(sourceFile, core.TextPos(insertPos))
		t.ReplaceRange(sourceFile, lsproto.Range{Start: insertLSPos, End: insertLSPos}, newNode, NodeOptions{indentation: &indentation, Prefix: t.newLine})
	} else {
		separatorString := scanner.TokenToString(separator)
		endPos := t.toLSPEditPos(sourceFile, core.TextPos(end))
		t.ReplaceRange(sourceFile, lsproto.Range{Start: endPos, End: endPos}, newNode, NodeOptions{Prefix: separatorString + " "})
	}
}

func (t *Tracker) InsertImportSpecifierAtIndex(sourceFile *ast.SourceFile, newSpecifier ast.Handle, namedImports ast.Handle, index int) {
	namedImportsNode := namedImports
	elements := namedImportsNode.Elements()
	var prevSpecifier ast.Handle
	if index > 0 && index-1 < len(elements) {
		prevSpecifier = elements[index-1]
	}
	if !prevSpecifier.IsNil() {
		t.InsertNodeInListAfter(sourceFile, prevSpecifier, newSpecifier, 0)
	} else {
		t.InsertNodeBefore(sourceFile, elements[0], newSpecifier, !positionsAreOnSameLine(astnav.GetStartOfNode(elements[0], sourceFile, false), astnav.GetStartOfNode(namedImports.Parent().Parent(), sourceFile, false), sourceFile), LeadingTriviaOptionNone)
	}
}
func (t *Tracker) InsertAtTopOfFile(sourceFile *ast.SourceFile, insert []ast.Handle, blankLineBetween bool) {
	if len(insert) == 0 {
		return
	}
	pos := t.getInsertionPositionAtSourceFileTop(sourceFile)
	originalPos := pos
	if spanMap := sourceFile.SpanMap(); spanMap != nil {
		for _, segment := range spanMap.Segments() {
			if segment.Kind != spanmap.KindVerbatim || segment.VirtualEnd <= core.TextPos(pos) {
				continue
			}
			if segment.VirtualStart > core.TextPos(pos) {
				pos = int(segment.VirtualStart)
			}
			originalPos = int(segment.OriginalStart + core.TextPos(pos) - segment.VirtualStart)
			break
		}
	}
	options := NodeOptions{}
	if originalPos != 0 {
		options.Prefix = t.newLine
	}
	if len(sourceFile.Text()) == 0 || !stringutil.IsLineBreak(rune(sourceFile.Text()[pos])) {
		options.Suffix = t.newLine
	}
	if blankLineBetween {
		options.Suffix += t.newLine
	}
	if len(insert) == 1 {
		t.InsertNodeAt(sourceFile, core.TextPos(pos), insert[0], options)
	} else {
		t.InsertNodesAt(sourceFile, core.TextPos(pos), insert, options)
	}
}
func (t *Tracker) InsertMemberAtStart(sourceFile *ast.SourceFile, node ast.Handle, newElement ast.Handle) {
	t.insertNodeAtStartWorker(sourceFile, node, newElement)
}
func (t *Tracker) insertNodeAtStartWorker(sourceFile *ast.SourceFile, node ast.Handle, newElement ast.Handle) {
	indentation := t.tryComputeIndentationFromExistingMembers(sourceFile, node)
	if indentation < 0 {
		indentation = t.tryComputeIndentationForNewMember(sourceFile, node)
	}
	members := getMembersOrProperties(node)
	if members == 0 {
		return
	}
	t.InsertNodeAt(sourceFile, core.TextPos(node.Store().ListLoc(members).Pos()), newElement, t.getInsertNodeAtStartInsertOptions(sourceFile, node, indentation))
}
func (t *Tracker) tryComputeIndentationForNewMember(sourceFile *ast.SourceFile, node ast.Handle) int {
	nodeStart := astnav.GetStartOfNode(node, sourceFile, false)
	lineStart := format.GetLineStartPositionForPosition(nodeStart, sourceFile)
	tabSize := t.formatSettings.TabSize
	if tabSize <= 0 {
		tabSize = 4
	}
	indentSize := t.formatSettings.IndentSize
	if indentSize <= 0 {
		indentSize = 4
	}
	return max(findIndentationColumn(sourceFile.Text(), lineStart, nodeStart, tabSize), 0) + indentSize
}
func (t *Tracker) tryComputeIndentationFromExistingMembers(sourceFile *ast.SourceFile, node ast.Handle) int {
	members := getMembersOrProperties(node)
	if members == 0 {
		return -1
	}
	indentation := -1
	text := sourceFile.Text()
	tabSize := t.formatSettings.TabSize
	last := node
	if tabSize <= 0 {
		tabSize = 4
	}
	for _, member := range node.Store().ListSlice(members) {
		if member.IsNil() {
			continue
		}
		if printer.RangeStartPositionsAreOnSameLine(last.Loc(), member.Loc(), sourceFile) {
			return -1
		}
		memberStart := astnav.GetStartOfNode(member, sourceFile, false)
		lineStart := format.GetLineStartPositionForPosition(memberStart, sourceFile)
		column := findIndentationColumn(text, lineStart, memberStart, tabSize)
		if column < 0 {
			return -1
		}
		if indentation >= 0 {
			if indentation != column {
				return -1
			}
			last = member
			continue
		}
		indentation = column
		last = member
	}
	return indentation
}
func (t *Tracker) getInsertNodeAfterOptions(sourceFile *ast.SourceFile, node ast.Handle) NodeOptions {
	newLineChar := t.newLine
	var options NodeOptions
	switch node.Kind {
	case ast.KindParameter:
		options = NodeOptions{}
	case ast.KindClassDeclaration, ast.KindModuleDeclaration:
		options = NodeOptions{Prefix: newLineChar, Suffix: newLineChar}
	case ast.KindVariableDeclaration, ast.KindStringLiteral, ast.KindIdentifier:
		options = NodeOptions{Prefix: ", "}
	case ast.KindPropertyAssignment:
		options = NodeOptions{Suffix: "," + newLineChar}
	case ast.KindExportKeyword:
		options = NodeOptions{Prefix: " "}
	default:
		if !(ast.IsStatement(node) || ast.IsClassOrTypeElement(node)) {
			panic("unimplemented node type " + node.Kind.String() + " in changeTracker.getInsertNodeAfterOptions")
		}
		options = NodeOptions{Suffix: newLineChar}
	}
	if node.End() == sourceFile.End() && ast.IsStatement(node) {
		options.Prefix = t.newLine + options.Prefix
	}
	return options
}
func (t *Tracker) getOptionsForInsertNodeBefore(before ast.Handle, inserted ast.Handle, blankLineBetween bool) NodeOptions {
	if ast.IsStatement(before) || ast.IsClassOrTypeElement(before) {
		if blankLineBetween {
			return NodeOptions{Suffix: t.newLine + t.newLine}
		}
		return NodeOptions{Suffix: t.newLine}
	} else if before.Kind == ast.KindVariableDeclaration {
		return NodeOptions{Suffix: ", "}
	} else if before.Kind == ast.KindParameter {
		if inserted.Kind == ast.KindParameter {
			return NodeOptions{Suffix: ", "}
		}
		return NodeOptions{}
	} else if (before.Kind == ast.KindStringLiteral && !before.Parent().IsNil() && before.Parent().Kind == ast.KindImportDeclaration) || before.Kind == ast.KindNamedImports {
		return NodeOptions{Suffix: ", "}
	} else if before.Kind == ast.KindImportSpecifier {
		suffix := ","
		if blankLineBetween {
			suffix += t.newLine
		} else {
			suffix += " "
		}
		return NodeOptions{Suffix: suffix}
	}
	panic("unimplemented node type " + before.Kind.String() + " in changeTracker.getOptionsForInsertNodeBefore")
}
func (t *Tracker) getInsertNodeAtStartInsertOptions(sourceFile *ast.SourceFile, node ast.Handle, indentation int) NodeOptions {
	state := t.nodesWithInsertionsAtStart[node]
	hasPreviousInsertion := state != nil
	if state == nil {
		state = &nodesInsertedAtStartState{node: node, sourceFile: sourceFile}
		t.nodesWithInsertionsAtStart[node] = state
	}
	members := getMembersOrProperties(node)
	isObjectLiteral := ast.IsObjectLiteralExpression(node)
	isJSON := ast.IsJsonSourceFile(sourceFile)
	hasMembers := members != 0 && node.Store().ListLen(members) > 0
	insertTrailingComma := isObjectLiteral && (hasMembers || !isJSON)
	insertLeadingComma := isObjectLiteral && isJSON && !hasMembers && hasPreviousInsertion
	suffix := ""
	if insertTrailingComma {
		suffix = ","
	} else if ast.IsInterfaceDeclaration(node) && !hasMembers {
		suffix = ";"
	}
	prefix := t.newLine
	if insertLeadingComma {
		prefix = "," + prefix
	}
	return NodeOptions{indentation: &indentation, Prefix: prefix, Suffix: suffix}
}
func (t *Tracker) finishNodesWithInsertionsAtStart() {
	for _, state := range t.nodesWithInsertionsAtStart {
		if state == nil {
			continue
		}
		openBrace := astnav.FindChildOfKind(state.node, ast.KindOpenBraceToken, state.sourceFile)
		if openBrace.IsNil() {
			continue
		}
		closeBrace := astnav.FindChildOfKind(state.node, ast.KindCloseBraceToken, state.sourceFile)
		if closeBrace.IsNil() {
			continue
		}
		members := getMembersOrProperties(state.node)
		isEmpty := members == 0 || state.sourceFile.ParseStore().ListLen(members) == 0
		isSingleLine := positionsAreOnSameLine(openBrace.End(), closeBrace.End(), state.sourceFile)
		if isEmpty && isSingleLine && openBrace.End() != closeBrace.End()-1 {
			t.DeleteRange(state.sourceFile, core.NewTextRange(openBrace.End(), closeBrace.End()-1))
		}
		if isSingleLine {
			t.InsertText(state.sourceFile, t.toLSPEditPos(state.sourceFile, core.TextPos(closeBrace.End()-1)), t.newLine)
		}
	}
}
func getMembersOrProperties(node ast.Handle) ast.ListRef {
	if ast.IsObjectLiteralExpression(node) {
		return node.PropertyList()
	}
	return node.MemberList()
}
func rangeContainsRangeExclusive(outer ast.Handle, inner ast.Handle) bool {
	return outer.Pos() < inner.Pos() && inner.End() < outer.End()
}
func isSeparator(node ast.Handle, candidate ast.Handle) bool {
	return !candidate.IsNil() && !node.Parent().IsNil() && (candidate.Kind == ast.KindCommaToken || (candidate.Kind == ast.KindSemicolonToken && node.Parent().Kind == ast.KindObjectLiteralExpression))
}
func findIndentationColumn(text string, lineStart, memberStart, tabSize int) int {
	column := 0
	for i := lineStart; i < memberStart && i < len(text); i++ {
		ch := rune(text[i])
		if stringutil.IsLineBreak(ch) {
			return -1
		}
		if stringutil.IsWhiteSpaceSingleLine(ch) {
			column = advanceIndentationColumn(column, ch, tabSize)
			continue
		}
		return column
	}
	return column
}
func advanceIndentationColumn(column int, ch rune, tabSize int) int {
	if ch == '\t' {
		return column + tabSize - (column % tabSize)
	}
	return column + 1
}
