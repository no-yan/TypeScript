package format

import (
	"context"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"math"
	"slices"
	"strings"
	"unicode/utf8"
)

func findEnclosingNode(r core.TextRange, sourceFile *ast.SourceFile) ast.Handle {
	var find func(ast.Handle) ast.Handle
	find = func(n ast.Handle) ast.Handle {
		var candidate ast.Handle
		n.ForEachChild(func(c ast.Handle) bool {
			if c.Flags()&ast.NodeFlagsReparsed != 0 {
				return false
			}
			if r.ContainedBy(withTokenStart(c, sourceFile)) {
				candidate = c
				return true
			}
			return false
		})
		if !candidate.IsNil() {
			result := find(candidate)
			if !result.IsNil() {
				return result
			}
		}
		return n
	}
	return find(sourceFile.ParseRoot())
}

func getScanStartPosition(enclosingNode ast.Handle, originalRange core.TextRange, sourceFile *ast.SourceFile) int {
	adjusted := withTokenStart(enclosingNode, sourceFile)
	start := adjusted.Pos()
	if start == originalRange.Pos() && enclosingNode.End() == originalRange.End() {
		return start
	}
	precedingToken := astnav.FindPrecedingTokenEx(sourceFile, originalRange.Pos(), ast.Handle{}, true)
	if precedingToken.IsNil() {
		return enclosingNode.Pos()
	}
	if precedingToken.End() >= originalRange.Pos() {
		return enclosingNode.Pos()
	}
	return precedingToken.End()
}

func getOwnOrInheritedDelta(n ast.Handle, options lsutil.FormatCodeSettings, sourceFile *ast.SourceFile) int {
	previousLine := -1
	var child ast.Handle
	for !n.IsNil() {
		line := scanner.GetECMALineOfPosition(sourceFile, withTokenStart(n, sourceFile).Pos())
		if previousLine != -1 && line != previousLine {
			break
		}
		if ShouldIndentChildNode(options, n, child, sourceFile) {
			return options.IndentSize
		}
		previousLine = line
		child = n
		n = n.Parent()
	}
	return 0
}
func rangeHasNoErrors(_ core.TextRange) bool {
	return false
}
func prepareRangeContainsErrorFunction(errors []*ast.Diagnostic, originalRange core.TextRange) func(r core.TextRange) bool {
	if len(errors) == 0 {
		return rangeHasNoErrors
	}
	sorted := core.Filter(errors, func(d *ast.Diagnostic) bool {
		return originalRange.Overlaps(d.Loc())
	})
	if len(sorted) == 0 {
		return rangeHasNoErrors
	}
	slices.SortStableFunc(sorted, func(a *ast.Diagnostic, b *ast.Diagnostic) int {
		return a.Pos() - b.Pos()
	})
	index := 0
	return func(r core.TextRange) bool {
		for true {
			if index >= len(sorted) {
				return false
			}
			err := sorted[index]
			if r.End() <= err.Pos() {
				return false
			}
			if r.Overlaps(err.Loc()) {
				return true
			}
			index++
		}
		return false
	}
}

type formatSpanWorker struct {
	originalRange                    core.TextRange
	enclosingNode                    ast.Handle
	initialIndentation               int
	delta                            int
	requestKind                      FormatRequestKind
	rangeContainsError               func(r core.TextRange) bool
	sourceFile                       *ast.SourceFile
	ctx                              context.Context
	formattingScanner                *formattingScanner
	formattingContext                *FormattingContext
	edits                            []core.TextChange
	previousRange                    TextRangeWithKind
	previousRangeTriviaEnd           int
	previousParent                   ast.Handle
	previousRangeStartLine           int
	childContextNode                 ast.Handle
	lastIndentedLine                 int
	indentationOnLastIndentedLine    int
	visitor                          *ast.HandleVisitor
	visitingNode                     ast.Handle
	visitingIndenter                 *dynamicIndenter
	visitingNodeStartLine            int
	visitingUndecoratedNodeStartLine int
	currentRules                     []*ruleImpl
}

func newFormatSpanWorker(ctx context.Context, originalRange core.TextRange, enclosingNode ast.Handle, initialIndentation int, delta int, requestKind FormatRequestKind, rangeContainsError func(r core.TextRange) bool, sourceFile *ast.SourceFile) *formatSpanWorker {
	return &formatSpanWorker{ctx: ctx, originalRange: originalRange, enclosingNode: enclosingNode, initialIndentation: initialIndentation, delta: delta, requestKind: requestKind, rangeContainsError: rangeContainsError, sourceFile: sourceFile, currentRules: make([]*ruleImpl, 0, 32)}
}
func getNonDecoratorTokenPosOfNode(node ast.Handle, file *ast.SourceFile) int {
	var lastDecorator ast.Handle
	if ast.HasDecorators(node) {
		lastDecorator = core.FindLast(node.ModifierNodes(), ast.IsDecorator)
	}
	if file == nil {
		file = ast.GetSourceFileOfNode(node)
	}
	if lastDecorator.IsNil() {
		return withTokenStart(node, file).Pos()
	}
	return scanner.SkipTrivia(file.Text(), lastDecorator.End())
}
func (w *formatSpanWorker) execute(s *formattingScanner) []core.TextChange {
	w.formattingScanner = s
	w.indentationOnLastIndentedLine = -1
	w.lastIndentedLine = -1
	opt := GetFormatCodeSettingsFromContext(w.ctx)
	w.formattingContext = NewFormattingContext(w.sourceFile, w.requestKind, opt)
	w.visitor = ast.NewHandleVisitor(func(child ast.Handle) ast.Handle {
		if child.IsNil() {
			return child
		}
		w.processChildNode(w.visitingNode, w.visitingIndenter, w.visitingNodeStartLine, w.visitingUndecoratedNodeStartLine, child, -1, w.visitingNode, w.visitingIndenter, w.visitingNodeStartLine, w.visitingUndecoratedNodeStartLine, false, false)
		return child
	}, ast.NewFactory(ast.FactoryHooks{}), ast.HandleVisitorHooks{VisitNodes: func(nodes ast.ListRef, v *ast.HandleVisitor) ast.ListRef {
		if nodes == 0 {
			return nodes
		}
		w.processChildNodes(w.visitingNode, w.visitingIndenter, w.visitingNodeStartLine, w.visitingUndecoratedNodeStartLine, nodes, w.visitingNode, w.visitingNodeStartLine, w.visitingIndenter)
		return nodes
	}})
	w.formattingScanner.advance()
	if w.formattingScanner.isOnToken() {
		startLine := scanner.GetECMALineOfPosition(w.sourceFile, withTokenStart(w.enclosingNode, w.sourceFile).Pos())
		undecoratedStartLine := startLine
		if ast.HasDecorators(w.enclosingNode) {
			undecoratedStartLine = scanner.GetECMALineOfPosition(w.sourceFile, getNonDecoratorTokenPosOfNode(w.enclosingNode, w.sourceFile))
		}
		w.processNode(w.enclosingNode, w.enclosingNode, startLine, undecoratedStartLine, w.initialIndentation, w.delta)
	}
	remainingTrivia := w.formattingScanner.getCurrentLeadingTrivia()
	if len(remainingTrivia) > 0 {
		indentation := w.initialIndentation
		if NodeWillIndentChild(w.formattingContext.Options, w.enclosingNode, ast.Handle{}, w.sourceFile, false) {
			indentation += opt.IndentSize
		}
		w.indentTriviaItems(remainingTrivia, indentation, true, func(item TextRangeWithKind) {
			startLine, startChar := scanner.GetECMALineAndByteOffsetOfPosition(w.sourceFile, item.Loc.Pos())
			w.processRange(item, startLine, startChar, w.enclosingNode, w.enclosingNode, nil)
			w.insertIndentation(item.Loc.Pos(), indentation, false)
		})
		if opt.TrimTrailingWhitespace.IsTrue() {
			w.trimTrailingWhitespacesForRemainingRange(remainingTrivia)
		}
	}
	if w.previousRange != NewTextRangeWithKind(0, 0, 0) && w.formattingScanner.getTokenFullStart() >= w.originalRange.End() {
		var tokenInfo TextRangeWithKind
		if w.formattingScanner.isOnEOF() {
			tokenInfo = w.formattingScanner.readEOFTokenRange()
		} else if w.formattingScanner.isOnToken() {
			tokenInfo = w.formattingScanner.readTokenInfo(w.enclosingNode).token
		}
		if tokenInfo.Loc.Pos() == w.previousRangeTriviaEnd {
			parent := astnav.FindPrecedingToken(w.sourceFile, tokenInfo.Loc.End())
			if !parent.IsNil() {
				parent = parent.Parent()
			}
			if parent.IsNil() {
				parent = w.previousParent
			}
			line := scanner.GetECMALineOfPosition(w.sourceFile, tokenInfo.Loc.Pos())
			w.processPair(tokenInfo, line, parent, w.previousRange, w.previousRangeStartLine, w.previousParent, parent, nil)
		}
	}
	return w.edits
}
func (w *formatSpanWorker) processChildNode(node ast.Handle, indenter *dynamicIndenter, nodeStartLine int, undecoratedNodeStartLine int, child ast.Handle, inheritedIndentation int, parent ast.Handle, parentDynamicIndentation *dynamicIndenter, parentStartLine int, undecoratedParentStartLine int, isListItem bool, isFirstListItem bool) int {
	debug.Assert(!ast.NodeIsSynthesized(child))
	if ast.NodeIsMissing(child) || child.Flags()&ast.NodeFlagsReparsed != 0 {
		return inheritedIndentation
	}
	childStartPos := scanner.GetTokenPosOfNode(child, w.sourceFile, false)
	childStartLine := scanner.GetECMALineOfPosition(w.sourceFile, childStartPos)
	undecoratedChildStartLine := childStartLine
	if ast.HasDecorators(child) {
		undecoratedChildStartLine = scanner.GetECMALineOfPosition(w.sourceFile, getNonDecoratorTokenPosOfNode(child, w.sourceFile))
	}
	isErrorMemberListElement := child.Flags()&ast.NodeFlagsThisNodeHasError != 0 && isMemberListElement(parent, child)
	childIndentationAmount := -1
	if !isErrorMemberListElement && isListItem && parent.Loc().ContainedBy(w.originalRange) {
		childIndentationAmount = w.tryComputeIndentationForListItem(childStartPos, child.End(), parentStartLine, w.originalRange, inheritedIndentation)
		if childIndentationAmount != -1 {
			inheritedIndentation = childIndentationAmount
		}
	}
	if !w.originalRange.Overlaps(child.Loc()) {
		if child.End() < w.originalRange.Pos() {
			childLoc := child.Loc()
			w.formattingScanner.skipToEndOf(&childLoc)
		}
		return inheritedIndentation
	}
	if child.Loc().Len() == 0 {
		return inheritedIndentation
	}
	for w.formattingScanner.isOnToken() && w.formattingScanner.getTokenFullStart() < w.originalRange.End() {
		tokenInfo := w.formattingScanner.readTokenInfo(node)
		if tokenInfo.token.Loc.End() > w.originalRange.End() {
			return inheritedIndentation
		}
		if tokenInfo.token.Loc.End() > childStartPos {
			if tokenInfo.token.Loc.Pos() > childStartPos {
				childLoc := child.Loc()
				w.formattingScanner.skipToStartOf(&childLoc)
			}
			break
		}
		w.consumeTokenAndAdvanceScanner(tokenInfo, node, parentDynamicIndentation, node, false)
	}
	if !w.formattingScanner.isOnToken() || w.formattingScanner.getTokenFullStart() >= w.originalRange.End() {
		return inheritedIndentation
	}
	if ast.IsTokenKind(child.Kind()) {
		tokenInfo := w.formattingScanner.readTokenInfo(child)
		if child.Kind() != ast.KindJsxText {
			debug.Assert(tokenInfo.token.Loc.End() == child.Loc().End(), "Token end is child end")
			w.consumeTokenAndAdvanceScanner(tokenInfo, node, parentDynamicIndentation, child, false)
			return inheritedIndentation
		}
	}
	effectiveParentStartLine := undecoratedParentStartLine
	if child.Kind() == ast.KindDecorator {
		effectiveParentStartLine = childStartLine
	}
	childIndentation := 0
	delta := 0
	if isErrorMemberListElement {
		childIndentation = w.getCurrentIndentationAtPosition(childStartPos)
	} else {
		childIndentation, delta = w.computeIndentation(child, childStartLine, childIndentationAmount, node, parentDynamicIndentation, effectiveParentStartLine)
	}
	w.processNode(child, w.childContextNode, childStartLine, undecoratedChildStartLine, childIndentation, delta)
	w.childContextNode = node
	if isFirstListItem && parent.Kind() == ast.KindArrayLiteralExpression && inheritedIndentation == -1 {
		inheritedIndentation = childIndentation
	}
	return inheritedIndentation
}
func (w *formatSpanWorker) processChildNodes(node ast.Handle, indenter *dynamicIndenter, nodeStartLine int, undecoratedNodeStartLine int, nodes ast.ListRef, parent ast.Handle, parentStartLine int, parentDynamicIndentation *dynamicIndenter) {
	debug.Assert(nodes != 0)
	debug.Assert(!ast.PositionIsSynthesized(node.Store().ListLoc(nodes).Pos()))
	debug.Assert(!ast.PositionIsSynthesized(node.Store().ListLoc(nodes).End()))
	listStartToken := getOpenTokenForList(parent, nodes)
	listDynamicIndentation := parentDynamicIndentation
	startLine := parentStartLine
	if !w.originalRange.Overlaps(node.Store().ListLoc(nodes)) {
		if node.Store().ListLoc(nodes).End() < w.originalRange.Pos() && (node.Store().ListLen(nodes) == 0 || node.Store().ListAt(nodes, 0).Flags()&ast.NodeFlagsReparsed == 0) {
			listLoc := node.Store().ListLoc(nodes)
			w.formattingScanner.skipToEndOf(&listLoc)
		}
		return
	}
	if listStartToken != ast.KindUnknown {
		for w.formattingScanner.isOnToken() && w.formattingScanner.getTokenFullStart() < w.originalRange.End() {
			tokenInfo := w.formattingScanner.readTokenInfo(parent)
			if tokenInfo.token.Loc.End() > node.Store().ListLoc(nodes).Pos() {
				break
			} else if tokenInfo.token.Kind == listStartToken {
				startLine = scanner.GetECMALineOfPosition(w.sourceFile, tokenInfo.token.Loc.Pos())
				w.consumeTokenAndAdvanceScanner(tokenInfo, parent, parentDynamicIndentation, parent, false)
				indentationOnListStartToken := 0
				if w.indentationOnLastIndentedLine != -1 {
					indentationOnListStartToken = w.indentationOnLastIndentedLine
				} else {
					indentationOnListStartToken = w.getCurrentIndentationAtPosition(tokenInfo.token.Loc.Pos())
				}
				listDynamicIndentation = w.getDynamicIndentation(parent, parentStartLine, indentationOnListStartToken, w.formattingContext.Options.IndentSize)
			} else {
				w.consumeTokenAndAdvanceScanner(tokenInfo, parent, parentDynamicIndentation, parent, false)
			}
		}
	}
	inheritedIndentation := -1
	for i := range node.Store().ListLen(nodes) {
		child := node.Store().ListAt(nodes, i)
		inheritedIndentation = w.processChildNode(node, indenter, nodeStartLine, undecoratedNodeStartLine, child, inheritedIndentation, node, listDynamicIndentation, startLine, startLine, true, i == 0)
	}
	listEndToken := getCloseTokenForOpenToken(listStartToken)
	if listEndToken != ast.KindUnknown && w.formattingScanner.isOnToken() && w.formattingScanner.getTokenFullStart() < w.originalRange.End() {
		tokenInfo := w.formattingScanner.readTokenInfo(parent)
		if tokenInfo.token.Kind == ast.KindCommaToken {
			w.consumeTokenAndAdvanceScanner(tokenInfo, parent, listDynamicIndentation, parent, false)
			if w.formattingScanner.isOnToken() {
				tokenInfo = w.formattingScanner.readTokenInfo(parent)
			} else {
				return
			}
		}
		if tokenInfo.token.Kind == listEndToken && tokenInfo.token.Loc.ContainedBy(parent.Loc()) {
			w.consumeTokenAndAdvanceScanner(tokenInfo, parent, listDynamicIndentation, parent, true)
		}
	}
}
func (w *formatSpanWorker) executeProcessNodeVisitor(node ast.Handle, indenter *dynamicIndenter, nodeStartLine int, undecoratedNodeStartLine int) {
	oldNode := w.visitingNode
	oldIndenter := w.visitingIndenter
	oldStart := w.visitingNodeStartLine
	oldUndecoratedStart := w.visitingUndecoratedNodeStartLine
	w.visitingNode = node
	w.visitingIndenter = indenter
	w.visitingNodeStartLine = nodeStartLine
	w.visitingUndecoratedNodeStartLine = undecoratedNodeStartLine
	node.VisitEachChild(w.visitor)
	w.visitingNode = oldNode
	w.visitingIndenter = oldIndenter
	w.visitingNodeStartLine = oldStart
	w.visitingUndecoratedNodeStartLine = oldUndecoratedStart
}
func (w *formatSpanWorker) getCurrentIndentationAtPosition(pos int) int {
	startLinePosition := GetLineStartPositionForPosition(pos, w.sourceFile)
	return FindFirstNonWhitespaceColumn(startLinePosition, pos, w.sourceFile, w.formattingContext.Options)
}
func (w *formatSpanWorker) computeIndentation(node ast.Handle, startLine int, inheritedIndentation int, parent ast.Handle, parentDynamicIndentation *dynamicIndenter, effectiveParentStartLine int) (indentation int, delta int) {
	delta = 0
	if ShouldIndentChildNode(w.formattingContext.Options, node, ast.Handle{}, nil) {
		delta = w.formattingContext.Options.IndentSize
	}
	if effectiveParentStartLine == startLine {
		indentation = w.indentationOnLastIndentedLine
		if startLine != w.lastIndentedLine {
			indentation = parentDynamicIndentation.getIndentation()
		}
		delta = min(w.formattingContext.Options.IndentSize, parentDynamicIndentation.getDelta(node)+delta)
		return indentation, delta
	} else if inheritedIndentation == -1 {
		if node.Kind() == ast.KindOpenParenToken && startLine == w.lastIndentedLine {
			return w.indentationOnLastIndentedLine, parentDynamicIndentation.getDelta(node)
		} else if childStartsOnTheSameLineWithElseInIfStatement(parent, node, startLine, w.sourceFile) || childIsUnindentedBranchOfConditionalExpression(parent, node, startLine, w.sourceFile) || argumentStartsOnSameLineAsPreviousArgument(parent, node, startLine, w.sourceFile) {
			return parentDynamicIndentation.getIndentation(), delta
		} else {
			i := parentDynamicIndentation.getIndentation()
			if i == -1 {
				return parentDynamicIndentation.getIndentation(), delta
			}
			return i + parentDynamicIndentation.getDelta(node), delta
		}
	}
	return inheritedIndentation, delta
}

func (w *formatSpanWorker) tryComputeIndentationForListItem(startPos int, endPos int, parentStartLine int, r core.TextRange, inheritedIndentation int) int {
	r2 := core.NewTextRange(startPos, endPos)
	if r.Overlaps(r2) || r2.ContainedBy(r) {
		if inheritedIndentation != -1 {
			return inheritedIndentation
		}
	} else {
		startLine := scanner.GetECMALineOfPosition(w.sourceFile, startPos)
		column := w.getCurrentIndentationAtPosition(startPos)
		if startLine != parentStartLine || startPos == column {
			baseIndentSize := w.formattingContext.Options.BaseIndentSize
			if baseIndentSize > column {
				return baseIndentSize
			}
			return column
		}
	}
	return -1
}
func (w *formatSpanWorker) processNode(node ast.Handle, contextNode ast.Handle, nodeStartLine int, undecoratedNodeStartLine int, indentation int, delta int) {
	if !w.originalRange.Overlaps(withTokenStart(node, w.sourceFile)) {
		return
	}
	nodeDynamicIndentation := w.getDynamicIndentation(node, nodeStartLine, indentation, delta)
	w.childContextNode = contextNode
	w.executeProcessNodeVisitor(node, nodeDynamicIndentation, nodeStartLine, undecoratedNodeStartLine)
	for w.formattingScanner.isOnToken() && w.formattingScanner.getTokenFullStart() < w.originalRange.End() {
		tokenInfo := w.formattingScanner.readTokenInfo(node)
		if tokenInfo.token.Loc.End() > min(node.End(), w.originalRange.End()) {
			break
		}
		w.consumeTokenAndAdvanceScanner(tokenInfo, node, nodeDynamicIndentation, node, false)
	}
}
func (w *formatSpanWorker) processPair(currentItem TextRangeWithKind, currentStartLine int, currentParent ast.Handle, previousItem TextRangeWithKind, previousStartLine int, previousParent ast.Handle, contextNode ast.Handle, dynamicIndentation *dynamicIndenter) LineAction {
	w.formattingContext.UpdateContext(previousItem, previousParent, currentItem, currentParent, contextNode)
	w.currentRules = w.currentRules[:0]
	w.currentRules = getRules(w.formattingContext, w.currentRules)
	trimTrailingWhitespaces := !w.formattingContext.Options.TrimTrailingWhitespace.IsFalse()
	lineAction := LineActionNone
	if len(w.currentRules) > 0 {
		for i := len(w.currentRules) - 1; i >= 0; i-- {
			rule := w.currentRules[i]
			lineAction = w.applyRuleEdits(rule, previousItem, previousStartLine, currentItem, currentStartLine)
			if dynamicIndentation != nil {
				switch lineAction {
				case LineActionLineRemoved:
					if scanner.GetTokenPosOfNode(currentParent, w.sourceFile, false) == currentItem.Loc.Pos() {
						dynamicIndentation.recomputeIndentation(false, contextNode)
					}
				case LineActionLineAdded:
					if scanner.GetTokenPosOfNode(currentParent, w.sourceFile, false) == currentItem.Loc.Pos() {
						dynamicIndentation.recomputeIndentation(true, contextNode)
					}
				default:
					debug.Assert(lineAction == LineActionNone)
				}
			}
			trimTrailingWhitespaces = trimTrailingWhitespaces && (rule.Action()&ruleActionDeleteSpace == 0) && rule.Flags() != ruleFlagsCanDeleteNewLines
		}
	} else {
		trimTrailingWhitespaces = trimTrailingWhitespaces && currentItem.Kind != ast.KindEndOfFile
	}
	if currentStartLine != previousStartLine && trimTrailingWhitespaces {
		w.trimTrailingWhitespacesForLines(previousStartLine, currentStartLine, previousItem)
	}
	return lineAction
}
func (w *formatSpanWorker) applyRuleEdits(rule *ruleImpl, previousRange TextRangeWithKind, previousStartLine int, currentRange TextRangeWithKind, currentStartLine int) LineAction {
	onLaterLine := currentStartLine != previousStartLine
	switch rule.Action() {
	case ruleActionStopProcessingSpaceActions:
		return LineActionNone
	case ruleActionDeleteSpace:
		if previousRange.Loc.End() != currentRange.Loc.Pos() {
			w.recordDelete(previousRange.Loc.End(), currentRange.Loc.Pos()-previousRange.Loc.End())
			if onLaterLine {
				return LineActionLineRemoved
			}
			return LineActionNone
		}
	case ruleActionDeleteToken:
		w.recordDelete(previousRange.Loc.Pos(), previousRange.Loc.Len())
	case ruleActionInsertNewLine:
		if rule.Flags() != ruleFlagsCanDeleteNewLines && previousStartLine != currentStartLine {
			return LineActionNone
		}
		lineDelta := currentStartLine - previousStartLine
		if lineDelta != 1 {
			w.recordReplace(previousRange.Loc.End(), currentRange.Loc.Pos()-previousRange.Loc.End(), GetNewLineOrDefaultFromContext(w.ctx))
			if onLaterLine {
				return LineActionNone
			}
			return LineActionLineAdded
		}
	case ruleActionInsertSpace:
		if rule.Flags() != ruleFlagsCanDeleteNewLines && previousStartLine != currentStartLine {
			return LineActionNone
		}
		posDelta := currentRange.Loc.Pos() - previousRange.Loc.End()
		if posDelta != 1 || !strings.HasPrefix(w.sourceFile.Text()[previousRange.Loc.End():], " ") {
			w.recordReplace(previousRange.Loc.End(), posDelta, " ")
			if onLaterLine {
				return LineActionLineRemoved
			}
			return LineActionNone
		}
	case ruleActionInsertTrailingSemicolon:
		w.recordInsert(previousRange.Loc.End(), ";")
	}
	return LineActionNone
}

type LineAction int

const (
	LineActionNone LineAction = iota
	LineActionLineAdded
	LineActionLineRemoved
)

func (w *formatSpanWorker) processRange(r TextRangeWithKind, rangeStartLine int, rangeStartCharacter int, parent ast.Handle, contextNode ast.Handle, dynamicIndentation *dynamicIndenter) LineAction {
	rangeHasError := w.rangeContainsError(r.Loc)
	lineAction := LineActionNone
	if !rangeHasError {
		if w.previousRange == NewTextRangeWithKind(0, 0, 0) {
			originalStartLine := scanner.GetECMALineOfPosition(w.sourceFile, w.originalRange.Pos())
			w.trimTrailingWhitespacesForLines(originalStartLine, rangeStartLine, NewTextRangeWithKind(0, 0, 0))
		} else {
			lineAction = w.processPair(r, rangeStartLine, parent, w.previousRange, w.previousRangeStartLine, w.previousParent, contextNode, dynamicIndentation)
		}
	}
	w.previousRange = r
	w.previousRangeTriviaEnd = r.Loc.End()
	w.previousParent = parent
	w.previousRangeStartLine = rangeStartLine
	return lineAction
}
func (w *formatSpanWorker) processTrivia(trivia []TextRangeWithKind, parent ast.Handle, contextNode ast.Handle, dynamicIndentation *dynamicIndenter) {
	for _, triviaItem := range trivia {
		if isComment(triviaItem.Kind) && triviaItem.Loc.ContainedBy(w.originalRange) {
			triviaItemStartLine, triviaItemStartCharacter := scanner.GetECMALineAndByteOffsetOfPosition(w.sourceFile, triviaItem.Loc.Pos())
			w.processRange(triviaItem, triviaItemStartLine, triviaItemStartCharacter, parent, contextNode, dynamicIndentation)
		}
	}
}

func (w *formatSpanWorker) trimTrailingWhitespacesForRemainingRange(trivias []TextRangeWithKind) {
	startPos := w.originalRange.Pos()
	if w.previousRange != NewTextRangeWithKind(0, 0, 0) {
		startPos = w.previousRange.Loc.End()
	}
	for _, trivia := range trivias {
		if isComment(trivia.Kind) {
			if startPos < trivia.Loc.Pos() {
				w.trimTrailingWitespacesForPositions(startPos, trivia.Loc.Pos()-1, w.previousRange)
			}
			startPos = trivia.Loc.End() + 1
		}
	}
	if startPos < w.originalRange.End() {
		w.trimTrailingWitespacesForPositions(startPos, w.originalRange.End(), w.previousRange)
	}
}
func (w *formatSpanWorker) trimTrailingWitespacesForPositions(startPos int, endPos int, previousRange TextRangeWithKind) {
	startLine := scanner.GetECMALineOfPosition(w.sourceFile, startPos)
	endLine := scanner.GetECMALineOfPosition(w.sourceFile, endPos)
	w.trimTrailingWhitespacesForLines(startLine, endLine+1, previousRange)
}
func (w *formatSpanWorker) trimTrailingWhitespacesForLines(line1 int, line2 int, r TextRangeWithKind) {
	lineStarts := scanner.GetECMALineStarts(w.sourceFile)
	for line := line1; line < line2; line++ {
		lineStartPosition := int(lineStarts[line])
		lineEndPosition := scanner.GetECMAEndLinePosition(w.sourceFile, line)
		if r != NewTextRangeWithKind(0, 0, 0) && (isComment(r.Kind) || isStringOrRegularExpressionOrTemplateLiteral(r.Kind)) && r.Loc.Pos() <= lineEndPosition && r.Loc.End() > lineEndPosition {
			continue
		}
		whitespaceStart := w.getTrailingWhitespaceStartPosition(lineStartPosition, lineEndPosition)
		if whitespaceStart != -1 {
			if whitespaceStart != lineStartPosition {
				r, _ := utf8.DecodeRuneInString(w.sourceFile.Text()[whitespaceStart-1:])
				debug.Assert(!stringutil.IsWhiteSpaceSingleLine(r))
			}
			w.recordDelete(whitespaceStart, lineEndPosition+1-whitespaceStart)
		}
	}
}

func (w *formatSpanWorker) getTrailingWhitespaceStartPosition(start int, end int) int {
	pos := end
	text := w.sourceFile.Text()
	for pos >= start {
		ch, size := utf8.DecodeRuneInString(text[pos:])
		if size == 0 {
			pos--
			continue
		}
		if !stringutil.IsWhiteSpaceSingleLine(ch) {
			break
		}
		pos--
	}
	if pos != end {
		return pos + 1
	}
	return -1
}
func isStringOrRegularExpressionOrTemplateLiteral(kind ast.Kind) bool {
	return kind == ast.KindStringLiteral || kind == ast.KindRegularExpressionLiteral || ast.IsTemplateLiteralKind(kind)
}
func isComment(kind ast.Kind) bool {
	return kind == ast.KindSingleLineCommentTrivia || kind == ast.KindMultiLineCommentTrivia
}
func (w *formatSpanWorker) insertIndentation(pos int, indentation int, lineAdded bool) {
	indentationString := getIndentationString(indentation, w.formattingContext.Options)
	if lineAdded {
		w.recordReplace(pos, 0, indentationString)
	} else {
		tokenStartLine, tokenStartCharacter := scanner.GetECMALineAndByteOffsetOfPosition(w.sourceFile, pos)
		startLinePosition := int(scanner.GetECMALineStarts(w.sourceFile)[tokenStartLine])
		if indentation != w.characterToColumn(startLinePosition, tokenStartCharacter) || w.indentationIsDifferent(indentationString, startLinePosition) {
			w.recordReplace(startLinePosition, tokenStartCharacter, indentationString)
		}
	}
}
func (w *formatSpanWorker) characterToColumn(startLinePosition int, characterInLine int) int {
	column := 0
	for i := range characterInLine {
		if w.sourceFile.Text()[startLinePosition+i] == '\t' {
			if w.formattingContext.Options.TabSize > 0 {
				column += w.formattingContext.Options.TabSize - (column % w.formattingContext.Options.TabSize)
			}
		} else {
			column++
		}
	}
	return column
}
func (w *formatSpanWorker) indentationIsDifferent(indentationString string, startLinePosition int) bool {
	text := w.sourceFile.Text()
	end := startLinePosition + len(indentationString)
	if end > len(text) {
		return true
	}
	return indentationString != text[startLinePosition:end]
}
func (w *formatSpanWorker) indentTriviaItems(trivia []TextRangeWithKind, commentIndentation int, indentNextTokenOrTrivia bool, indentSingleLine func(item TextRangeWithKind)) bool {
	for _, triviaItem := range trivia {
		triviaInRange := triviaItem.Loc.ContainedBy(w.originalRange)
		switch triviaItem.Kind {
		case ast.KindMultiLineCommentTrivia:
			if triviaInRange {
				w.indentMultilineComment(triviaItem.Loc, commentIndentation, !indentNextTokenOrTrivia, true)
			}
			indentNextTokenOrTrivia = false
		case ast.KindSingleLineCommentTrivia:
			if indentNextTokenOrTrivia && triviaInRange {
				indentSingleLine(triviaItem)
			}
			indentNextTokenOrTrivia = false
		case ast.KindNewLineTrivia:
			indentNextTokenOrTrivia = true
		}
	}
	return indentNextTokenOrTrivia
}
func (w *formatSpanWorker) indentMultilineComment(commentRange core.TextRange, indentation int, firstLineIsIndented bool, indentFinalLine bool) {
	startLine := scanner.GetECMALineOfPosition(w.sourceFile, commentRange.Pos())
	endLine := scanner.GetECMALineOfPosition(w.sourceFile, commentRange.End())
	if startLine == endLine {
		if !firstLineIsIndented {
			w.insertIndentation(commentRange.Pos(), indentation, false)
		}
		return
	}
	parts := make([]core.TextRange, 0, strings.Count(w.sourceFile.Text()[commentRange.Pos():commentRange.End()], "\n"))
	startPos := commentRange.Pos()
	for line := startLine; line < endLine; line++ {
		endOfLine := scanner.GetECMAEndLinePosition(w.sourceFile, line)
		parts = append(parts, core.NewTextRange(startPos, endOfLine))
		startPos = int(scanner.GetECMALineStarts(w.sourceFile)[line+1])
	}
	if indentFinalLine {
		parts = append(parts, core.NewTextRange(startPos, commentRange.End()))
	}
	if len(parts) == 0 {
		return
	}
	startLinePos := int(scanner.GetECMALineStarts(w.sourceFile)[startLine])
	nonWhitespaceInFirstPartCharacter, nonWhitespaceInFirstPartColumn := findFirstNonWhitespaceCharacterAndColumn(startLinePos, parts[0].Pos(), w.sourceFile, w.formattingContext.Options)
	startIndex := 0
	if firstLineIsIndented {
		startIndex = 1
		startLine++
	}
	delta := indentation - nonWhitespaceInFirstPartColumn
	for i := startIndex; i < len(parts); i++ {
		startLinePos := int(scanner.GetECMALineStarts(w.sourceFile)[startLine])
		nonWhitespaceCharacter := nonWhitespaceInFirstPartCharacter
		nonWhitespaceColumn := nonWhitespaceInFirstPartColumn
		if i != 0 {
			nonWhitespaceCharacter, nonWhitespaceColumn = findFirstNonWhitespaceCharacterAndColumn(parts[i].Pos(), parts[i].End(), w.sourceFile, w.formattingContext.Options)
		}
		newIndentation := nonWhitespaceColumn + delta
		if newIndentation > 0 {
			indentationString := getIndentationString(newIndentation, w.formattingContext.Options)
			w.recordReplace(startLinePos, nonWhitespaceCharacter, indentationString)
		} else {
			w.recordDelete(startLinePos, nonWhitespaceCharacter)
		}
		startLine++
	}
}
func getIndentationString(indentation int, options lsutil.FormatCodeSettings) string {
	if !options.ConvertTabsToSpaces.IsTrue() {
		if options.TabSize == 0 {
			return ""
		}
		tabs := int(math.Floor(float64(indentation) / float64(options.TabSize)))
		spaces := indentation - (tabs * options.TabSize)
		res := strings.Repeat("\t", tabs)
		if spaces > 0 {
			res = res + strings.Repeat(" ", spaces)
		}
		return res
	} else {
		return strings.Repeat(" ", indentation)
	}
}
func createTextChangeFromStartLength(start int, length int, newText string) core.TextChange {
	return core.TextChange{NewText: newText, TextRange: core.NewTextRange(start, start+length)}
}
func (w *formatSpanWorker) recordDelete(start int, length int) {
	if length != 0 {
		w.edits = append(w.edits, createTextChangeFromStartLength(start, length, ""))
	}
}
func (w *formatSpanWorker) recordReplace(start int, length int, newText string) {
	if length != 0 || newText != "" {
		w.edits = append(w.edits, createTextChangeFromStartLength(start, length, newText))
	}
}
func (w *formatSpanWorker) recordInsert(start int, text string) {
	if text != "" {
		w.edits = append(w.edits, createTextChangeFromStartLength(start, 0, text))
	}
}
func (w *formatSpanWorker) consumeTokenAndAdvanceScanner(currentTokenInfo tokenInfo, parent ast.Handle, dynamicIndenation *dynamicIndenter, container ast.Handle, isListEndToken bool) {
	lastTriviaWasNewLine := w.formattingScanner.lastTrailingTriviaWasNewLine()
	indentToken := false
	if len(currentTokenInfo.leadingTrivia) > 0 {
		w.processTrivia(currentTokenInfo.leadingTrivia, parent, w.childContextNode, dynamicIndenation)
	}
	lineAction := LineActionNone
	isTokenInRange := currentTokenInfo.token.Loc.ContainedBy(w.originalRange)
	tokenStartLine, tokenStartChar := scanner.GetECMALineAndByteOffsetOfPosition(w.sourceFile, currentTokenInfo.token.Loc.Pos())
	if isTokenInRange {
		rangeHasError := w.rangeContainsError(currentTokenInfo.token.Loc)
		savePreviousRange := w.previousRange
		lineAction = w.processRange(currentTokenInfo.token, tokenStartLine, tokenStartChar, parent, w.childContextNode, dynamicIndenation)
		if !rangeHasError {
			if lineAction == LineActionNone {
				if savePreviousRange != NewTextRangeWithKind(0, 0, 0) {
					prevEndLine := scanner.GetECMALineOfPosition(w.sourceFile, savePreviousRange.Loc.End())
					indentToken = lastTriviaWasNewLine && tokenStartLine != prevEndLine
				} else {
					indentToken = lastTriviaWasNewLine
				}
			} else {
				indentToken = lineAction == LineActionLineAdded
			}
		}
	}
	if len(currentTokenInfo.trailingTrivia) > 0 {
		w.previousRangeTriviaEnd = core.LastOrNil(currentTokenInfo.trailingTrivia).Loc.End()
		for _, trivia := range currentTokenInfo.trailingTrivia {
			if isComment(trivia.Kind) && !trivia.Loc.ContainedBy(w.originalRange) {
				w.previousRangeTriviaEnd = trivia.Loc.Pos()
				break
			}
		}
		w.processTrivia(currentTokenInfo.trailingTrivia, parent, w.childContextNode, dynamicIndenation)
	}
	if indentToken {
		tokenIndentation := -1
		if isTokenInRange && !w.rangeContainsError(currentTokenInfo.token.Loc) {
			tokenIndentation = dynamicIndenation.getIndentationForToken(tokenStartLine, currentTokenInfo.token.Kind, container, !!isListEndToken)
		}
		indentNextTokenOrTrivia := true
		if len(currentTokenInfo.leadingTrivia) > 0 {
			commentIndentation := dynamicIndenation.getIndentationForComment(currentTokenInfo.token.Kind, tokenIndentation, container)
			indentNextTokenOrTrivia = w.indentTriviaItems(currentTokenInfo.leadingTrivia, commentIndentation, indentNextTokenOrTrivia, func(item TextRangeWithKind) {
				w.insertIndentation(item.Loc.Pos(), commentIndentation, false)
			})
		}
		if tokenIndentation != -1 && indentNextTokenOrTrivia {
			w.insertIndentation(currentTokenInfo.token.Loc.Pos(), tokenIndentation, lineAction == LineActionLineAdded)
			w.lastIndentedLine = tokenStartLine
			w.indentationOnLastIndentedLine = tokenIndentation
		}
	}
	w.formattingScanner.advance()
	w.childContextNode = parent
}

type dynamicIndenter struct {
	node          ast.Handle
	nodeStartLine int
	indentation   int
	delta         int
	options       lsutil.FormatCodeSettings
	sourceFile    *ast.SourceFile
}

func (i *dynamicIndenter) getIndentationForComment(kind ast.Kind, tokenIndentation int, container ast.Handle) int {
	switch kind {
	case ast.KindCloseBraceToken, ast.KindCloseBracketToken, ast.KindCloseParenToken:
		return i.indentation + i.getDelta(container)
	}
	if tokenIndentation != -1 {
		return tokenIndentation
	}
	return i.indentation
}

func (i *dynamicIndenter) getIndentationForToken(line int, kind ast.Kind, container ast.Handle, suppressDelta bool) int {
	if !suppressDelta && i.shouldAddDelta(line, kind, container) {
		return i.indentation + i.getDelta(container)
	}
	return i.indentation
}
func (i *dynamicIndenter) getIndentation() int {
	return i.indentation
}
func (i *dynamicIndenter) getDelta(child ast.Handle) int {
	if NodeWillIndentChild(i.options, i.node, child, i.sourceFile, true) {
		return i.delta
	}
	return 0
}
func (i *dynamicIndenter) recomputeIndentation(lineAdded bool, parent ast.Handle) {
	if ShouldIndentChildNode(i.options, parent, i.node, i.sourceFile) {
		if lineAdded {
			i.indentation += i.options.IndentSize
		} else {
			i.indentation -= i.options.IndentSize
		}
		if ShouldIndentChildNode(i.options, i.node, ast.Handle{}, nil) {
			i.delta = i.options.IndentSize
		} else {
			i.delta = 0
		}
	}
}
func (i *dynamicIndenter) shouldAddDelta(line int, kind ast.Kind, container ast.Handle) bool {
	switch kind {
	case ast.KindOpenBraceToken, ast.KindCloseBraceToken, ast.KindCloseParenToken, ast.KindElseKeyword, ast.KindWhileKeyword, ast.KindAtToken:
		return false
	case ast.KindSlashToken, ast.KindGreaterThanToken:
		switch container.Kind() {
		case ast.KindJsxOpeningElement, ast.KindJsxClosingElement, ast.KindJsxSelfClosingElement:
			return false
		}
		break
	case ast.KindOpenBracketToken, ast.KindCloseBracketToken:
		if container.Kind() != ast.KindMappedType {
			return false
		}
		break
	}
	return i.nodeStartLine != line && !(ast.HasDecorators(i.node) && kind == getFirstNonDecoratorTokenOfNode(i.node))
}
func getFirstNonDecoratorTokenOfNode(node ast.Handle) ast.Kind {
	if ast.CanHaveModifiers(node) {
		modifier := core.Find(node.ModifierNodes()[core.FindIndex(node.ModifierNodes(), ast.IsDecorator):], ast.IsModifier)
		if !modifier.IsNil() {
			return modifier.Kind()
		}
	}
	switch node.Kind() {
	case ast.KindClassDeclaration:
		return ast.KindClassKeyword
	case ast.KindInterfaceDeclaration:
		return ast.KindInterfaceKeyword
	case ast.KindFunctionDeclaration:
		return ast.KindFunctionKeyword
	case ast.KindEnumDeclaration:
		return ast.KindEnumDeclaration
	case ast.KindGetAccessor:
		return ast.KindGetKeyword
	case ast.KindSetAccessor:
		return ast.KindSetKeyword
	case ast.KindMethodDeclaration:
		if !node.MethodDeclarationAsteriskToken().IsNil() {
			return ast.KindAsteriskToken
		}
		fallthrough
	case ast.KindPropertyDeclaration, ast.KindParameter:
		name := ast.GetNameOfDeclaration(node)
		if !name.IsNil() {
			return name.Kind()
		}
	}
	return ast.KindUnknown
}
func (w *formatSpanWorker) getDynamicIndentation(node ast.Handle, nodeStartLine int, indentation int, delta int) *dynamicIndenter {
	return &dynamicIndenter{node: node, nodeStartLine: nodeStartLine, indentation: indentation, delta: delta, options: w.formattingContext.Options, sourceFile: w.sourceFile}
}
