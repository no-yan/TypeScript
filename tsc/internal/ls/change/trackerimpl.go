package change

import (
	"fmt"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/format"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsconv"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"slices"
	"strings"
	"unicode"
)

func (t *Tracker) getTextChangesFromChanges( // order changes by start position
// If the start position is the same, put the shorter range first, since an empty range (x, x) may precede (x, y) but not vice-versa.
// verify that change intervals do not overlap, except possibly at end points.
// assert change[i].End <= change[i + 1].Start
// !!! targetSourceFile
// span := createTextSpanFromRange(c.Range)
// !!!
// Filter out redundant changes.
// if (span.length == newText.length && stringContainsAt(targetSourceFile.text, newText, span.start)) { return nil }
// The original range may have multiple verbatim copies; it is safe to lose their identity only when
// formatting at every exact projection produces the same edit.
// Strip initial indentation if text will be inserted in the middle of the line.
/** Note: this may mutate `nodeIn`. */ // !!! if (validate) validate(node, text);
// method on the changeTracker because use of converters
// GetAdjustedRange computes the adjusted range for a node in a source file, accounting for trivia.
// method on the changeTracker because use of converters
// full start and start of the node are on the same line
//   a,     b;
//    ^     ^
//    |   start
// fullstart
// when b is replaced - we usually want to keep the leading trvia
// when b is deleted - we delete it
// if node has a trailing comments, use comment end position as the text has already been included.
// Check first for leading comments as if the node is the first import, we want to exclude the trivia;
// otherwise we get the trailing comments.
// get start position of the line following the line that contains fullstart position
// (but only if the fullstart isn't the very beginning of the file)
// skip whitespaces/newlines
// method on the changeTracker because of converters
// Return the end position of a multiline comment of it is on another line; otherwise returns `undefined`;
// If the trailing comment is a multiline comment that extends to the next lines,
// return the end of the comment and track it for the next nodes to adjust.
// Single line can break the loop as trivia will only be this line.
// Comments on subsequent lines are also ignored.
// Get the end line of the comment and compare against the end line of the node.
// If the comment end line position and the multiline comment extends to multiple lines,
// then is safe to return the end position.
// method on the changeTracker because of converters
// ============= utilities =============
// TODO: only if b would start with a `(` or `[`
// Find the first attached comment to the first node and add before it
// Always insert after pinned or triple slash comments
// There was a blank line between the last comment and this comment.
// This comment is not part of the copyright comments
) map[string][]*lsproto.TextEdit {
	changes := map[string][]*lsproto.TextEdit{}
	for sourceFile, changesInFile := range t.changes.M {
		if t.unmappableFiles.Has(sourceFile.OriginalFileName()) {
			continue
		}
		slices.SortStableFunc(changesInFile, func(a, b *trackerEdit) int {
			return lsproto.CompareRanges(a.Range, b.Range)
		})
		for i := range len(changesInFile) - 1 {
			if lsproto.ComparePositions(changesInFile[i].Range.End, changesInFile[i+1].Range.Start) > 0 {
				panic(fmt.Sprintf("changes overlap: %v and %v", changesInFile[i].Range, changesInFile[i+1].Range))
			}
		}
		textChanges := core.MapNonNil(changesInFile, func(change *trackerEdit) *lsproto.TextEdit {
			newText := t.computeNewText(change, sourceFile, sourceFile)
			return &lsproto.TextEdit{NewText: newText, Range: change.Range}
		})
		if len(textChanges) > 0 {
			fileName := sourceFile.OriginalFileName()
			if t.unmappableFiles.Has(fileName) {
				continue
			}
			changes[fileName] = append(changes[fileName], textChanges...)
		}
	}
	return changes
}
func (t *Tracker) computeNewText(change *trackerEdit, targetSourceFile *ast.SourceFile, sourceFile *ast.SourceFile) string {
	switch change.kind {
	case trackerEditKindRemove:
		return ""
	case trackerEditKindText:
		return change.NewText
	}
	positions := lsconv.FromLSPPositionForSourceFile(t.converters, sourceFile, change.Range.Start, spanmap.FeatureAll)
	var result string
	found := false
	for _, mapped := range positions {
		if !mapped.Fidelity.IsExact() {
			continue
		}
		projection := mapped.Script
		pos := int(mapped.Position)
		formatNode := func(n ast.Handle) string {
			return t.getFormattedTextOfNode(n, targetSourceFile, projection, pos, change.options)
		}
		var text string
		switch change.kind {
		case trackerEditKindReplaceWithMultipleNodes:
			joiner := change.options.joiner
			if joiner == "" {
				joiner = t.newLine
			}
			text = strings.Join(core.Map(change.nodes, func(n ast.Handle) string {
				return strings.TrimSuffix(formatNode(n), t.newLine)
			}), joiner)
		case trackerEditKindReplaceWithSingleNode:
			text = formatNode(change.Node)
		default:
			panic(fmt.Sprintf("change kind %d should have been handled earlier", change.kind))
		}
		noIndent := text
		if !(change.options.indentation != nil || format.GetLineStartPositionForPosition(pos, projection) == pos) {
			noIndent = strings.TrimLeftFunc(text, unicode.IsSpace)
		}
		candidate := change.options.Prefix + noIndent + core.IfElse(strings.HasSuffix(noIndent, change.options.Suffix), "", change.options.Suffix)
		if found && candidate != result {
			t.unmappableFiles.Add(sourceFile.OriginalFileName())
			return ""
		}
		result = candidate
		found = true
	}
	if !found {
		t.unmappableFiles.Add(sourceFile.OriginalFileName())
	}
	return result
}

func (t *Tracker) getFormattedTextOfNode(nodeIn ast.Handle, targetSourceFile *ast.SourceFile, sourceFile *ast.SourceFile, pos int, options NodeOptions) string {
	text, sourceFileLike := t.getNonformattedText(nodeIn, targetSourceFile)
	formatOptions := GetFormatCodeSettingsForWriting(t.formatSettings, targetSourceFile)
	var initialIndentation, delta int
	if options.indentation == nil {
		initialIndentation = format.GetIndentation(pos, sourceFile, formatOptions, options.Prefix == t.newLine || format.GetLineStartPositionForPosition(pos, sourceFile) == pos)
	} else {
		initialIndentation = *options.indentation
	}
	if options.delta != nil {
		delta = *options.delta
	} else if formatOptions.IndentSize != 0 && format.ShouldIndentChildNode(formatOptions, nodeIn, ast.Handle{}, nil) {
		delta = formatOptions.IndentSize
	}
	changes := format.FormatNodeGivenIndentation(t.ctx, nodeIn, sourceFileLike, targetSourceFile.LanguageVariant, initialIndentation, delta)
	return core.ApplyBulkEdits(text, changes)
}
func GetFormatCodeSettingsForWriting(options lsutil.FormatCodeSettings, sourceFile *ast.SourceFile) lsutil.FormatCodeSettings {
	shouldAutoDetectSemicolonPreference := options.Semicolons == lsutil.SemicolonPreferenceIgnore
	shouldRemoveSemicolons := options.Semicolons == lsutil.SemicolonPreferenceRemove || shouldAutoDetectSemicolonPreference && !lsutil.ProbablyUsesSemicolons(sourceFile)
	if shouldRemoveSemicolons {
		options.Semicolons = lsutil.SemicolonPreferenceRemove
	}
	return options
}
func (t *Tracker) getNonformattedText(node ast.Handle, sourceFile *ast.SourceFile) (string, *ast.SourceFile) {
	text, nodeOut := printer.PrintAndPositionNode(t.HandleFactory, node, sourceFile, t.newLine, t.formatSettings.IndentSize, t.EmitContext)
	return text, printer.CreateSyntheticSourceFile(t.HandleFactory, nodeOut, text, ast.SourceFileParseOptions{FileName: sourceFile.FileName(), Path: sourceFile.Path()})
}

func (t *Tracker) GetAdjustedRange(sourceFile *ast.SourceFile, startNode ast.Handle, endNode ast.Handle, leadingOption LeadingTriviaOption, trailingOption TrailingTriviaOption) lsproto.Range {
	return t.toLSPEditRange(sourceFile, core.NewTextRange(t.getAdjustedStartPosition(sourceFile, startNode, leadingOption, false), t.getAdjustedEndPosition(sourceFile, endNode, trailingOption)))
}

func (t *Tracker) getAdjustedStartPosition(sourceFile *ast.SourceFile, node ast.Handle, leadingOption LeadingTriviaOption, hasTrailingComment bool) int {
	if leadingOption == LeadingTriviaOptionJSDoc {
		if JSDocComments := parser.GetJSDocCommentRanges(nil, node.Kind, node.Pos(), node.End(), sourceFile.Text()); len(JSDocComments) > 0 {
			return format.GetLineStartPositionForPosition(JSDocComments[0].Pos(), sourceFile)
		}
	}
	start := astnav.GetStartOfNode(node, sourceFile, false)
	startOfLinePos := format.GetLineStartPositionForPosition(start, sourceFile)
	switch leadingOption {
	case LeadingTriviaOptionExclude:
		return start
	case LeadingTriviaOptionStartLine:
		if node.Loc().ContainsInclusive(startOfLinePos) {
			return startOfLinePos
		}
		return start
	}
	fullStart := node.Pos()
	if fullStart == start {
		return start
	}
	lineStarts := sourceFile.ECMALineMap()
	fullStartLineIndex := scanner.ComputeLineOfPosition(lineStarts, fullStart)
	fullStartLinePos := int(lineStarts[fullStartLineIndex])
	if startOfLinePos == fullStartLinePos {
		if leadingOption == LeadingTriviaOptionIncludeAll {
			return fullStart
		}
		return start
	}
	if hasTrailingComment {
		comments := slices.Collect(scanner.GetLeadingCommentRanges(sourceFile.Text(), fullStart))
		if len(comments) == 0 {
			comments = slices.Collect(scanner.GetTrailingCommentRanges(sourceFile.Text(), fullStart))
		}
		if len(comments) > 0 {
			return scanner.SkipTriviaEx(sourceFile.Text(), comments[0].End(), &scanner.SkipTriviaOptions{StopAfterLineBreak: true, StopAtComments: true})
		}
	}
	nextLineStart := core.IfElse(fullStart > 0, 1, 0)
	adjustedStartPosition := int(lineStarts[fullStartLineIndex+nextLineStart])
	adjustedStartPosition = scanner.SkipTriviaEx(sourceFile.Text(), adjustedStartPosition, &scanner.SkipTriviaOptions{StopAtComments: true})
	return int(lineStarts[scanner.ComputeLineOfPosition(lineStarts, adjustedStartPosition)])
}

func (t *Tracker) getEndPositionOfMultilineTrailingComment(sourceFile *ast.SourceFile, node ast.Handle, trailingOpt TrailingTriviaOption) int {
	if trailingOpt == TrailingTriviaOptionInclude {
		lineStarts := sourceFile.ECMALineMap()
		nodeEndLine := scanner.ComputeLineOfPosition(lineStarts, node.End())
		for comment := range scanner.GetTrailingCommentRanges(sourceFile.Text(), node.End()) {
			if comment.Kind == ast.KindSingleLineCommentTrivia || scanner.ComputeLineOfPosition(lineStarts, comment.Pos()) > nodeEndLine {
				break
			}
			if commentEndLine := scanner.ComputeLineOfPosition(lineStarts, comment.End()); commentEndLine > nodeEndLine {
				return scanner.SkipTriviaEx(sourceFile.Text(), comment.End(), &scanner.SkipTriviaOptions{StopAfterLineBreak: true, StopAtComments: true})
			}
		}
	}
	return 0
}

func (t *Tracker) getAdjustedEndPosition(sourceFile *ast.SourceFile, node ast.Handle, TrailingTriviaOption TrailingTriviaOption) int {
	if TrailingTriviaOption == TrailingTriviaOptionExclude {
		return node.End()
	}
	if TrailingTriviaOption == TrailingTriviaOptionExcludeWhitespace {
		if comments := slices.AppendSeq(slices.Collect(scanner.GetTrailingCommentRanges(sourceFile.Text(), node.End())), scanner.GetLeadingCommentRanges(sourceFile.Text(), node.End())); len(comments) > 0 {
			if realEnd := comments[len(comments)-1].End(); realEnd != 0 {
				return realEnd
			}
		}
		return node.End()
	}
	if multilineEndPosition := t.getEndPositionOfMultilineTrailingComment(sourceFile, node, TrailingTriviaOption); multilineEndPosition != 0 {
		return multilineEndPosition
	}
	newEnd := scanner.SkipTriviaEx(sourceFile.Text(), node.End(), &scanner.SkipTriviaOptions{StopAfterLineBreak: true})
	if newEnd != node.End() && (TrailingTriviaOption == TrailingTriviaOptionInclude || stringutil.IsLineBreak(rune(sourceFile.Text()[newEnd-1]))) {
		return newEnd
	}
	return node.End()
}
func hasCommentsBeforeLineBreak(text string, start int) bool {
	for _, ch := range []rune(text[start:]) {
		if !stringutil.IsWhiteSpaceSingleLine(ch) {
			return ch == '/'
		}
	}
	return false
}
func needSemicolonBetween(a, b ast.Handle) bool {
	return (ast.IsPropertySignatureDeclaration(a) || ast.IsPropertyDeclaration(a)) && ast.IsClassOrTypeElement(b) && b.Name().Kind == ast.KindComputedPropertyName || ast.IsStatementButNotDeclaration(a) && ast.IsStatementButNotDeclaration(b)
}
func (t *Tracker) getInsertionPositionAtSourceFileTop(sourceFile *ast.SourceFile) int {
	var lastPrologue ast.Handle
	for _, node := range sourceFile.ParseRoot().Statements() {
		if ast.IsPrologueDirective(node) {
			lastPrologue = node
		} else {
			break
		}
	}
	position := 0
	text := sourceFile.Text()
	advancePastLineBreak := func() {
		if position >= len(text) {
			return
		}
		if char := rune(text[position]); stringutil.IsLineBreak(char) {
			position++
			if position < len(text) && char == '\r' && rune(text[position]) == '\n' {
				position++
			}
		}
	}
	if !lastPrologue.IsNil() {
		position = lastPrologue.End()
		advancePastLineBreak()
		return position
	}
	shebang := scanner.GetShebang(text)
	if shebang != "" {
		position = len(shebang)
		advancePastLineBreak()
	}
	ranges := slices.Collect(scanner.GetLeadingCommentRanges(text, position))
	if len(ranges) == 0 {
		return position
	}
	var lastComment *ast.CommentRange
	pinnedOrTripleSlash := false
	firstNodeLine := -1
	lenStatements := len(sourceFile.ParseRoot().Statements())
	lineMap := sourceFile.ECMALineMap()
	for _, r := range ranges {
		if r.Kind == ast.KindMultiLineCommentTrivia {
			if printer.IsPinnedComment(text, r) {
				lastComment = &r
				pinnedOrTripleSlash = true
				continue
			}
		} else if printer.IsRecognizedTripleSlashComment(text, r) {
			lastComment = &r
			pinnedOrTripleSlash = true
			continue
		}
		if lastComment != nil {
			if pinnedOrTripleSlash {
				break
			}
			commentLine := scanner.ComputeLineOfPosition(lineMap, r.Pos())
			lastCommentEndLine := scanner.ComputeLineOfPosition(lineMap, lastComment.End())
			if commentLine >= lastCommentEndLine+2 {
				break
			}
		}
		if lenStatements > 0 {
			if firstNodeLine == -1 {
				firstNodeLine = scanner.ComputeLineOfPosition(lineMap, astnav.GetStartOfNode(sourceFile.ParseRoot().Statements()[0], sourceFile, false))
			}
			commentEndLine := scanner.ComputeLineOfPosition(lineMap, r.End())
			if firstNodeLine < commentEndLine+2 {
				break
			}
		}
		lastComment = &r
		pinnedOrTripleSlash = false
	}
	if lastComment != nil {
		position = lastComment.End()
		advancePastLineBreak()
	}
	return position
}
