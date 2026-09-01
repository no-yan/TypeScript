package printer

import (
	"fmt"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/sourcemap"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"slices"
	"strings"
	"unicode/utf8"
)

type PrinterOptions struct {
	RemoveComments                bool
	NewLine                       core.NewLineKind
	OmitTrailingSemicolon         bool
	NoEmitHelpers                 bool
	Target                        core.ScriptTarget
	SourceMap                     bool
	InlineSourceMap               bool
	InlineSources                 bool
	OmitBraceSourceMapPositions   bool
	OnlyPrintJSDocStyle           bool
	NeverAsciiEscape              bool
	PreserveSourceNewlines        bool
	TerminateUnterminatedLiterals bool
}
type PrintHandlers struct {
	HasGlobalName        func(name string) bool
	MapSourcePosition    func(source sourcemap.Source, pos int) (mappedSource sourcemap.Source, mappedPos int, ok bool)
	OnBeforeEmitNode     func(nodeOpt ast.Handle)
	OnAfterEmitNode      func(nodeOpt ast.Handle)
	OnBeforeEmitNodeList func(nodesOpt ast.ListRef)
	OnAfterEmitNodeList  func(nodesOpt ast.ListRef)
	OnBeforeEmitToken    func(nodeOpt ast.Handle)
	OnAfterEmitToken     func(nodeOpt ast.Handle)
}
type Printer struct {
	PrintHandlers
	Options                           PrinterOptions
	emitContext                       *EmitContext
	currentSourceFile                 *ast.SourceFile
	uniqueHelperNames                 map[string]ast.Handle
	externalHelpersModuleName         ast.Handle
	nextListElementPos                int
	writer                            EmitTextWriter
	ownWriter                         EmitTextWriter
	writeKind                         WriteKind
	sourceMapsDisabled                bool
	sourceMapGenerator                *sourcemap.Generator
	sourceMapSource                   sourcemap.Source
	sourceMapSourceIndex              sourcemap.SourceIndex
	sourceMapSourceIsJson             bool
	sourceMapLineCharCache            *lineCharacterCache
	mostRecentSourceMapSource         sourcemap.Source
	mostRecentSourceMapSourceIndex    sourcemap.SourceIndex
	containerPos                      int
	containerEnd                      int
	declarationListContainerEnd       int
	detachedCommentsInfo              core.Stack[detachedCommentsInfo]
	commentsDisabled                  bool
	inExtends                         bool
	nameGenerator                     NameGenerator
	makeFileLevelOptimisticUniqueName func(string) string
	commentStateArena                 core.Arena[commentState]
	sourceMapStateArena               core.Arena[sourceMapState]
	IdToSymbol                        map[ast.Handle]*ast.Symbol
}
type detachedCommentsInfo struct {
	nodePos               int
	detachedCommentEndPos int
}
type commentState struct {
	emitFlags                   EmitFlags
	commentRange                core.TextRange
	containerPos                int
	containerEnd                int
	declarationListContainerEnd int
}
type sourceMapState struct {
	emitFlags              EmitFlags
	sourceMapRange         core.TextRange
	hasTokenSourceMapRange bool
}
type printerState struct {
	commentState   *commentState
	sourceMapState *sourceMapState
}

func NewPrinter(options PrinterOptions, handlers PrintHandlers, emitContext *EmitContext) *Printer {
	printer := &Printer{PrintHandlers: handlers, Options: options, emitContext: emitContext}
	if printer.emitContext == nil {
		printer.emitContext = NewEmitContext()
	}
	printer.nameGenerator.Context = printer.emitContext
	printer.nameGenerator.GetTextOfNode = func(node ast.Handle) string {
		return printer.getTextOfNode(node, false)
	}
	printer.nameGenerator.IsFileLevelUniqueNameInCurrentFile = printer.isFileLevelUniqueNameInCurrentFile
	printer.makeFileLevelOptimisticUniqueName = func(name string) string {
		return printer.nameGenerator.MakeFileLevelOptimisticUniqueName(name)
	}
	printer.containerPos = -1
	printer.containerEnd = -1
	printer.declarationListContainerEnd = -1
	printer.commentsDisabled = options.RemoveComments
	return printer
}
func (p *Printer) getLiteralTextOfNode(node ast.Handle, sourceFile *ast.SourceFile, flags getLiteralTextFlags) string {
	if ast.IsStringLiteral(node) {
		if textSourceNode := p.emitContext.TextSource(node); !textSourceNode.IsNil() {
			var text string
			switch textSourceNode.Kind() {
			default:
				return p.getLiteralTextOfNode(textSourceNode, ast.GetSourceFileOfNode(textSourceNode), flags)
			case ast.KindNumericLiteral:
				text = textSourceNode.Text()
			case ast.KindIdentifier, ast.KindPrivateIdentifier, ast.KindJsxNamespacedName:
				text = p.getTextOfNode(textSourceNode, false)
			}
			switch {
			case flags&getLiteralTextFlagsJsxAttributeEscape != 0:
				return "\"" + escapeJsxAttributeString(text, QuoteCharDoubleQuote) + "\""
			case flags&getLiteralTextFlagsNeverAsciiEscape != 0 || p.emitContext.EmitFlags(node)&EFNoAsciiEscaping != 0:
				return "\"" + EscapeString(text, QuoteCharDoubleQuote) + "\""
			default:
				return "\"" + escapeNonAsciiString(text, QuoteCharDoubleQuote) + "\""
			}
		}
	}
	if p.emitContext.EmitFlags(node)&EFNoAsciiEscaping != 0 {
		flags |= getLiteralTextFlagsNeverAsciiEscape
	}
	if p.Options.Target >= core.ScriptTargetES2021 {
		flags |= getLiteralTextFlagsAllowNumericSeparator
	}
	return getLiteralText(node, core.Coalesce(sourceFile, p.currentSourceFile), flags)
}

func (p *Printer) getTextOfNode(node ast.Handle, includeTrivia bool) string {
	if ast.IsMemberName(node) && p.emitContext.GetAutoGenerateInfo(node) != nil {
		return p.nameGenerator.GenerateName(node)
	}
	if ast.IsStringLiteral(node) {
		if textSourceNode := p.emitContext.TextSource(node); !textSourceNode.IsNil() {
			return p.getTextOfNode(textSourceNode, includeTrivia)
		}
	}
	canUseSourceFile := p.currentSourceFile != nil && !node.Parent().IsNil() && !ast.NodeIsSynthesized(node)
	switch node.Kind() {
	case ast.KindIdentifier, ast.KindPrivateIdentifier, ast.KindJsxNamespacedName:
		if !canUseSourceFile || ast.GetSourceFileOfNode(node) != ast.GetSourceFileOfNode(p.emitContext.MostOriginal(p.currentSourceFile.ParseRoot())) {
			return node.Text()
		}
	case ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateHead, ast.KindTemplateMiddle, ast.KindTemplateTail:
		return p.getLiteralTextOfNode(node, nil, getLiteralTextFlagsNone)
	default:
		panic(fmt.Sprintf("unexpected node: %v", node.Kind()))
	}
	return scanner.GetSourceTextOfNodeFromSourceFile(p.currentSourceFile, node, includeTrivia)
}

type WriteKind int

const (
	WriteKindNone WriteKind = iota
	WriteKindKeyword
	WriteKindOperator
	WriteKindPunctuation
	WriteKindStringLiteral
	WriteKindParameter
	WriteKindProperty
	WriteKindComment
	WriteKindLiteral
)

func (p *Printer) writeAs(text string, writeKind WriteKind) {
	switch writeKind {
	case WriteKindNone:
		p.writer.Write(text)
	case WriteKindParameter:
		p.writeParameter(text)
	case WriteKindKeyword:
		p.writeKeyword(text)
	case WriteKindOperator:
		p.writeOperator(text)
	case WriteKindProperty:
		p.writeProperty(text)
	case WriteKindPunctuation:
		p.writePunctuation(text)
	case WriteKindStringLiteral:
		p.writer.WriteStringLiteral(text)
	case WriteKindComment:
		p.writeComment(text)
	case WriteKindLiteral:
		p.writeLiteral(text)
	default:
		panic(fmt.Sprintf("unexpected printer.WriteKind: %v", writeKind))
	}
}
func (p *Printer) write(text string) {
	p.writeAs(text, p.writeKind)
}
func (p *Printer) setWriteKind(kind WriteKind) WriteKind {
	previous := p.writeKind
	p.writeKind = kind
	return previous
}
func (p *Printer) writeSymbol(text string, optSymbol *ast.Symbol) {
	if optSymbol == nil {
		p.write(text)
	} else {
		p.writer.WriteSymbol(text, optSymbol)
	}
}
func (p *Printer) writeLiteral(text string) {
	p.writer.WriteLiteral(text)
}
func (p *Printer) writePunctuation(text string) {
	p.writer.WritePunctuation(text)
}
func (p *Printer) writeOperator(text string) {
	p.writer.WriteOperator(text)
}
func (p *Printer) writeKeyword(text string) {
	p.writer.WriteKeyword(text)
}
func (p *Printer) writeProperty(text string) {
	p.writer.WriteProperty(text)
}
func (p *Printer) writeParameter(text string) {
	p.writer.WriteParameter(text)
}
func (p *Printer) writeComment(text string) {
	p.writer.WriteComment(text)
}
func (p *Printer) writeSpace() {
	p.writer.WriteSpace(" ")
}
func (p *Printer) writeLine() {
	p.writer.WriteLine()
}
func (p *Printer) writeLineRepeat(count int) {
	for range count {
		p.writeLine()
	}
}
func (p *Printer) writeLines(text string) {
	lines := stringutil.SplitLines(text)
	indentation := stringutil.GuessIndentation(lines)
	for _, line := range lines {
		if indentation > 0 {
			line = line[indentation:]
		}
		if len(line) > 0 {
			p.writeLine()
			p.write(line)
		}
	}
}
func (p *Printer) writeTrailingSemicolon() {
	p.writer.WriteTrailingSemicolon(";")
}
func (p *Printer) increaseIndent() {
	p.writer.IncreaseIndent()
}
func (p *Printer) decreaseIndent() {
	p.writer.DecreaseIndent()
}
func (p *Printer) increaseIndentIf(indentRequested bool) {
	if indentRequested {
		p.increaseIndent()
	}
}
func (p *Printer) decreaseIndentIf(indentRequested bool) {
	if indentRequested {
		p.decreaseIndent()
	}
}
func (p *Printer) writeLineOrSpace(parentNode ast.Handle, prevChildNode ast.Handle, nextChildNode ast.Handle) {
	if p.shouldEmitOnSingleLine(parentNode) {
		p.writeSpace()
	} else if p.Options.PreserveSourceNewlines {
		lines := p.getLinesBetweenNodes(parentNode, prevChildNode, nextChildNode)
		if lines > 0 {
			p.writeLineRepeat(lines)
		} else {
			p.writeSpace()
		}
	} else {
		p.writeLine()
	}
}
func (p *Printer) writeLinesAndIndent(lineCount int, writeSpaceIfNotIndenting bool) {
	if lineCount > 0 {
		p.increaseIndent()
		p.writeLineRepeat(lineCount)
	} else if writeSpaceIfNotIndenting {
		p.writeSpace()
	}
}
func (p *Printer) writeLineSeparatorsAndIndentBefore(node ast.Handle, parent ast.Handle) bool {
	if p.Options.PreserveSourceNewlines {
		leadingNewlines := p.getLeadingLineTerminatorCount(parent, node, LFNone)
		if leadingNewlines > 0 {
			p.writeLinesAndIndent(leadingNewlines, false)
			return true
		}
	}
	return false
}
func (p *Printer) writeLineSeparatorsAfter(node ast.Handle, parent ast.Handle) {
	if p.Options.PreserveSourceNewlines {
		trailingNewlines := p.getClosingLineTerminatorCount(parent, node, LFNone, core.NewTextRange(-1, -1))
		if trailingNewlines > 0 {
			p.writeLineRepeat(trailingNewlines)
		}
	}
}
func (p *Printer) getLinesBetweenNodes(parent ast.Handle, node1 ast.Handle, node2 ast.Handle) int {
	if p.shouldElideIndentation(parent) {
		return 0
	}
	parent = skipSynthesizedParentheses(parent)
	node1 = skipSynthesizedParentheses(node1)
	node2 = skipSynthesizedParentheses(node2)
	if p.shouldEmitOnNewLine(node2, LFNone) {
		return 1
	}
	if p.currentSourceFile != nil && !ast.NodeIsSynthesized(parent) && !ast.NodeIsSynthesized(node1) && !ast.NodeIsSynthesized(node2) {
		if p.Options.PreserveSourceNewlines {
			return p.getEffectiveLines(func(includeComments bool) int {
				return getLinesBetweenRangeEndAndRangeStart(node1.Loc(), node2.Loc(), p.currentSourceFile, includeComments)
			})
		}
		return core.IfElse(rangeEndIsOnSameLineAsRangeStart(node1.Loc(), node2.Loc(), p.currentSourceFile), 0, 1)
	}
	return 0
}
func (p *Printer) getEffectiveLines(getLineDifference func(includeComments bool) int) int {
	if !p.Options.PreserveSourceNewlines {
		panic("Should not be called when preserveSourceNewlines is false")
	}
	lines := getLineDifference(true)
	if lines == 0 {
		return getLineDifference(false)
	}
	return lines
}
func (p *Printer) getLeadingLineTerminatorCount(parentNode ast.Handle, firstChild ast.Handle, format ListFormat) int {
	if format&LFPreserveLines != 0 || p.Options.PreserveSourceNewlines {
		if format&LFPreferNewLine != 0 {
			return 1
		}
		if firstChild.IsNil() {
			return core.IfElse(parentNode.IsNil() || p.currentSourceFile != nil && RangeIsOnSingleLine(parentNode.Loc(), p.currentSourceFile), 0, 1)
		}
		if p.nextListElementPos > 0 && firstChild.Pos() == p.nextListElementPos {
			return 0
		}
		if firstChild.Kind() == ast.KindJsxText {
			return 0
		}
		if p.currentSourceFile != nil && !parentNode.IsNil() && !ast.PositionIsSynthesized(parentNode.Pos()) && !ast.NodeIsSynthesized(firstChild) && (firstChild.Parent().IsNil()) {
			if p.Options.PreserveSourceNewlines {
				return p.getEffectiveLines(func(includeComments bool) int {
					return getLinesBetweenPositionAndPrecedingNonWhitespaceCharacter(firstChild.Pos(), parentNode.Pos(), p.currentSourceFile, includeComments)
				})
			}
			return core.IfElse(RangeStartPositionsAreOnSameLine(parentNode.Loc(), firstChild.Loc(), p.currentSourceFile), 0, 1)
		}
		if p.shouldEmitOnNewLine(firstChild, format) {
			return 1
		}
	}
	return core.IfElse(format&LFMultiLine != 0, 1, 0)
}
func (p *Printer) getSeparatingLineTerminatorCount(previousNode ast.Handle, nextNode ast.Handle, format ListFormat) int {
	if format&LFPreserveLines != 0 || p.Options.PreserveSourceNewlines {
		if previousNode.IsNil() || nextNode.IsNil() {
			return 0
		}
		if nextNode.Kind() == ast.KindJsxText {
			return 0
		} else if p.currentSourceFile != nil && !ast.NodeIsSynthesized(previousNode) && !ast.NodeIsSynthesized(nextNode) {
			if p.Options.PreserveSourceNewlines && siblingNodePositionsAreComparable(p.emitContext, previousNode, nextNode) {
				return p.getEffectiveLines(func(includeComments bool) int {
					return getLinesBetweenRangeEndAndRangeStart(previousNode.Loc(), nextNode.Loc(), p.currentSourceFile, includeComments)
				})
			} else if !p.Options.PreserveSourceNewlines && originalNodesHaveSameParent(p.emitContext, previousNode, nextNode) {
				return core.IfElse(rangeEndIsOnSameLineAsRangeStart(previousNode.Loc(), nextNode.Loc(), p.currentSourceFile), 0, 1)
			}
			return core.IfElse(format&LFPreferNewLine != 0, 1, 0)
		} else if p.shouldEmitOnNewLine(previousNode, format) || p.shouldEmitOnNewLine(nextNode, format) {
			return 1
		}
	} else if p.shouldEmitOnNewLine(nextNode, LFNone) {
		return 1
	}
	return core.IfElse(format&LFMultiLine != 0, 1, 0)
}
func (p *Printer) getClosingLineTerminatorCount(parentNode ast.Handle, lastChild ast.Handle, format ListFormat, childrenTextRange core.TextRange) int {
	if format&LFPreserveLines != 0 || p.Options.PreserveSourceNewlines {
		if format&LFPreferNewLine != 0 {
			return 1
		}
		if lastChild.IsNil() {
			return core.IfElse(parentNode.IsNil() || p.currentSourceFile != nil && RangeIsOnSingleLine(parentNode.Loc(), p.currentSourceFile), 0, 1)
		}
		if p.currentSourceFile != nil && !parentNode.IsNil() && !ast.PositionIsSynthesized(parentNode.Pos()) && !ast.NodeIsSynthesized(lastChild) && (lastChild.Parent().IsNil() || lastChild.Parent() == parentNode) {
			if p.Options.PreserveSourceNewlines {
				end := greatestEnd(lastChild.End(), childrenTextRange)
				return p.getEffectiveLines(func(includeComments bool) int {
					return getLinesBetweenPositionAndNextNonWhitespaceCharacter(end, parentNode.End(), p.currentSourceFile, includeComments)
				})
			}
			return core.IfElse(rangeEndPositionsAreOnSameLine(parentNode.Loc(), lastChild.Loc(), p.currentSourceFile), 0, 1)
		}
		if p.shouldEmitOnNewLine(lastChild, format) {
			return 1
		}
	}
	if format&LFMultiLine != 0 && format&LFNoTrailingNewLine == 0 {
		return 1
	}
	return 0
}
func (p *Printer) writeCommentRange(comment ast.CommentRange) {
	if p.currentSourceFile == nil {
		return
	}
	text := p.currentSourceFile.Text()
	lineMap := p.currentSourceFile.ECMALineMap()
	p.writeCommentRangeWorker(text, lineMap, comment.Kind, comment.TextRange)
}
func (p *Printer) writeCommentRangeWorker(text string, lineMap []core.TextPos, kind ast.Kind, loc core.TextRange) {
	if kind == ast.KindMultiLineCommentTrivia {
		indentSize := GetDefaultIndentSize()
		firstLine := scanner.ComputeLineOfPosition(lineMap, loc.Pos())
		lineCount := len(lineMap)
		firstCommentLineIndent := -1
		pos := loc.Pos()
		currentLine := firstLine
		for ; pos < loc.End(); currentLine++ {
			var nextLineStart int
			if currentLine+1 == lineCount {
				nextLineStart = len(text) + 1
			} else {
				nextLineStart = int(lineMap[currentLine+1])
			}
			if pos != loc.Pos() {
				if firstCommentLineIndent == -1 {
					firstCommentLineIndent = calculateIndent(text, int(lineMap[firstLine]), loc.Pos())
				}
				currentWriterIndentSpacing := p.writer.GetIndent() * indentSize
				spacesToEmit := currentWriterIndentSpacing - firstCommentLineIndent + calculateIndent(text, pos, nextLineStart)
				if spacesToEmit > 0 {
					numberOfSingleSpacesToEmit := spacesToEmit % indentSize
					indentSizeSpaceString := getIndentString((spacesToEmit-numberOfSingleSpacesToEmit)/indentSize, indentSize)
					p.writer.RawWrite(indentSizeSpaceString)
					for numberOfSingleSpacesToEmit > 0 {
						p.writer.RawWrite(" ")
						numberOfSingleSpacesToEmit--
					}
				} else {
					p.writer.RawWrite("")
				}
			}
			end := min(loc.End(), nextLineStart)
			for scan := pos; scan < end; {
				ch, size := utf8.DecodeRuneInString(text[scan:end])
				if size == 0 {
					break
				}
				if stringutil.IsLineBreak(ch) {
					end = scan
					break
				}
				scan += size
			}
			currentLineText := strings.TrimSpace(text[pos:end])
			if len(currentLineText) > 0 {
				p.writeComment(currentLineText)
				if end != loc.End() {
					p.writeLine()
				}
			} else {
				p.writer.WriteLineForce(true)
			}
			pos = nextLineStart
		}
	} else {
		p.writeComment(text[loc.Pos():loc.End()])
	}
}
func (p *Printer) shouldEmitComments(node ast.Handle) bool {
	return !p.commentsDisabled && p.currentSourceFile != nil && !ast.IsSourceFile(node)
}
func (p *Printer) shouldWriteComment(comment ast.CommentRange) bool {
	return !p.Options.OnlyPrintJSDocStyle || p.currentSourceFile != nil && isJSDocLikeText(p.currentSourceFile.Text(), comment) || p.currentSourceFile != nil && IsPinnedComment(p.currentSourceFile.Text(), comment)
}
func (p *Printer) shouldEmitIndented(node ast.Handle) bool {
	return p.emitContext.EmitFlags(node)&EFIndented != 0
}
func (p *Printer) shouldElideIndentation(node ast.Handle) bool {
	return p.emitContext.EmitFlags(node)&EFNoIndentation != 0
}
func (p *Printer) shouldEmitOnSingleLine(node ast.Handle) bool {
	return p.emitContext.EmitFlags(node)&EFSingleLine != 0
}
func (p *Printer) shouldEmitOnMultipleLines(node ast.Handle) bool {
	return p.emitContext.EmitFlags(node)&EFMultiLine != 0
}
func (p *Printer) shouldEmitBlockFunctionBodyOnSingleLine(body ast.Handle) bool {
	if p.shouldEmitOnSingleLine(body) {
		return true
	}
	if body.BlockMultiLine() {
		return false
	}
	if !ast.NodeIsSynthesized(body) && p.currentSourceFile != nil && !RangeIsOnSingleLine(body.Loc(), p.currentSourceFile) {
		return false
	}
	if p.getLeadingLineTerminatorCount(body, core.FirstOrNil(body.Statements()), LFPreserveLines) > 0 || p.getClosingLineTerminatorCount(body, core.LastOrNil(body.Statements()), LFPreserveLines, body.Store().ListLoc(body.StatementList())) > 0 {
		return false
	}
	var previousStatement ast.Handle
	for _, statement := range body.Statements() {
		if p.getSeparatingLineTerminatorCount(previousStatement, statement, LFPreserveLines) > 0 {
			return false
		}
		previousStatement = statement
	}
	return true
}
func (p *Printer) shouldEmitOnNewLine(node ast.Handle, format ListFormat) bool {
	if p.emitContext.EmitFlags(node)&EFStartOnNewLine != 0 {
		return true
	}
	return format&LFPreferNewLine != 0
}
func (p *Printer) shouldEmitSourceMaps(node ast.Handle) bool {
	return !p.sourceMapsDisabled && p.sourceMapSource != nil && !ast.IsSourceFile(node) && !ast.IsInJsonFile(node)
}
func (p *Printer) shouldEmitTokenSourceMaps(token ast.Kind, pos int, contextNode ast.Handle, flags tokenEmitFlags) bool {
	return flags&tefNoSourceMaps == 0 && p.shouldEmitSourceMaps(contextNode) && !p.Options.OmitBraceSourceMapPositions && (token == ast.KindOpenBraceToken || token == ast.KindCloseBraceToken)
}
func (p *Printer) shouldEmitLeadingComments(node ast.Handle) bool {
	return p.emitContext.EmitFlags(node)&EFNoLeadingComments == 0
}
func (p *Printer) shouldEmitTrailingComments(node ast.Handle) bool {
	return p.emitContext.EmitFlags(node)&EFNoTrailingComments == 0
}
func (p *Printer) shouldEmitNestedComments(node ast.Handle) bool {
	return p.emitContext.EmitFlags(node)&EFNoNestedComments == 0
}
func (p *Printer) shouldEmitDetachedComments(node ast.Handle) bool {
	if !ast.IsSourceFile(node) {
		return true
	}
	return len(node.Statements()) == 0 || !ast.IsPrologueDirective(node.Statements()[0]) || ast.NodeIsSynthesized(node.Statements()[0])
}
func (p *Printer) hasCommentsAtPosition(pos int) bool {
	if p.currentSourceFile == nil {
		return false
	}
	for range scanner.GetTrailingCommentRanges(p.currentSourceFile.Text(), pos+1) {
		return true
	}
	for range scanner.GetLeadingCommentRanges(p.currentSourceFile.Text(), pos+1) {
		return true
	}
	return false
}
func (p *Printer) shouldEmitIndirectCall(node ast.Handle) bool {
	return p.emitContext.EmitFlags(node)&EFIndirectCall != 0
}
func (p *Printer) shouldAllowTrailingComma(node ast.Handle, list ast.ListRef) bool {
	if p.currentSourceFile == nil || p.currentSourceFile.ScriptKind == core.ScriptKindJSON {
		return false
	}
	switch node.Kind() {
	case ast.KindObjectLiteralExpression:
		return true
	case ast.KindArrayLiteralExpression, ast.KindArrowFunction, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration, ast.KindFunctionType, ast.KindConstructorType, ast.KindCallSignature, ast.KindConstructSignature, ast.KindTaggedTemplateExpression, ast.KindObjectBindingPattern, ast.KindArrayBindingPattern, ast.KindNamedImports, ast.KindNamedExports, ast.KindImportAttributes:
		return true
	case ast.KindClassExpression, ast.KindClassDeclaration, ast.KindInterfaceDeclaration:
		return list == node.TypeParameterList()
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindMethodDeclaration:
		return true
	case ast.KindCallExpression:
		return true
	case ast.KindNewExpression:
		return true
	}
	return false
}
func (p *Printer) writeTokenText(token ast.Kind, writeKind WriteKind, pos int) int {
	tokenString := scanner.TokenToString(token)
	p.writeAs(tokenString, writeKind)
	if ast.PositionIsSynthesized(pos) {
		return pos
	} else {
		return pos + len(tokenString)
	}
}
func (p *Printer) emitToken(token ast.Kind, pos int, writeKind WriteKind, contextNode ast.Handle) int {
	return p.emitTokenEx(token, pos, writeKind, contextNode, tefNone)
}
func (p *Printer) emitTokenEx(token ast.Kind, pos int, writeKind WriteKind, contextNode ast.Handle, flags tokenEmitFlags) int {
	state, pos := p.enterToken(token, pos, contextNode, flags)
	pos = p.writeTokenText(token, writeKind, pos)
	p.exitToken(token, pos, contextNode, state)
	return pos
}
func (p *Printer) emitKeywordNode(node ast.Handle) {
	p.emitKeywordNodeEx(node, tefNone)
}
func (p *Printer) emitKeywordNodeEx(node ast.Handle, flags tokenEmitFlags) {
	if node.IsNil() {
		return
	}
	state := p.enterTokenNode(node, flags)
	p.writeTokenText(node.Kind(), WriteKindKeyword, node.Pos())
	p.exitTokenNode(node, state)
}
func (p *Printer) emitPunctuationNode(node ast.Handle) {
	p.emitPunctuationNodeEx(node, tefNone)
}
func (p *Printer) emitPunctuationNodeEx(node ast.Handle, flags tokenEmitFlags) {
	if node.IsNil() {
		return
	}
	state := p.enterTokenNode(node, flags)
	p.writeTokenText(node.Kind(), WriteKindPunctuation, node.Pos())
	p.exitTokenNode(node, state)
}
func (p *Printer) emitTokenNode(node ast.Handle) {
	p.emitTokenNodeEx(node, tefNone)
}
func (p *Printer) emitTokenNodeEx(node ast.Handle, flags tokenEmitFlags) {
	if node.IsNil() {
		return
	}
	switch {
	case ast.IsKeywordKind(node.Kind()):
		p.emitKeywordNodeEx(node, flags)
	case ast.IsPunctuationKind(node.Kind()):
		p.emitPunctuationNodeEx(node, flags)
	default:
		panic(fmt.Sprintf("unexpected TokenNode: %v", node.Kind()))
	}
}

func (p *Printer) emitLiteral(node ast.Handle, flags getLiteralTextFlags) {
	if p.Options.NeverAsciiEscape {
		flags |= getLiteralTextFlagsNeverAsciiEscape
	}
	if p.Options.TerminateUnterminatedLiterals {
		flags |= getLiteralTextFlagsTerminateUnterminatedLiterals
	}
	text := p.getLiteralTextOfNode(node, nil, flags)
	p.writer.WriteStringLiteral(text)
}
func (p *Printer) emitNumericLiteral(node ast.Handle) {
	state := p.enterNode(node)
	p.emitLiteral(node, getLiteralTextFlagsNone)
	p.exitNode(node, state)
}
func (p *Printer) emitBigIntLiteral(node ast.Handle) {
	state := p.enterNode(node)
	p.emitLiteral(node, getLiteralTextFlagsNone)
	p.exitNode(node, state)
}
func (p *Printer) emitStringLiteral(node ast.Handle) {
	state := p.enterNode(node)
	p.emitLiteral(node, getLiteralTextFlagsNone)
	p.exitNode(node, state)
}
func (p *Printer) emitNoSubstitutionTemplateLiteral(node ast.Handle) {
	state := p.enterNode(node)
	p.emitLiteral(node, getLiteralTextFlagsNone)
	p.exitNode(node, state)
}
func (p *Printer) emitRegularExpressionLiteral(node ast.Handle) {
	state := p.enterNode(node)
	p.emitLiteral(node, getLiteralTextFlagsNone)
	p.exitNode(node, state)
}
func (p *Printer) emitTemplateHead(node ast.Handle) {
	state := p.enterNode(node)
	p.emitLiteral(node, getLiteralTextFlagsNone)
	p.exitNode(node, state)
}
func (p *Printer) emitTemplateMiddle(node ast.Handle) {
	state := p.enterNode(node)
	p.emitLiteral(node, getLiteralTextFlagsNone)
	p.exitNode(node, state)
}
func (p *Printer) emitTemplateTail(node ast.Handle) {
	state := p.enterNode(node)
	p.emitLiteral(node, getLiteralTextFlagsNone)
	p.exitNode(node, state)
}
func (p *Printer) emitTemplateMiddleTail(node ast.Handle) {
	switch node.Kind() {
	case ast.KindTemplateMiddle:
		p.emitTemplateMiddle(node)
	case ast.KindTemplateTail:
		p.emitTemplateTail(node)
	}
}
func (p *Printer) emitSnippetNode(node ast.Handle, snippetElement *SnippetElement) {
	switch snippetElement.Kind {
	case SnippetKindTabStop:
		p.emitTabStop(node, snippetElement)
	default:
		panic(fmt.Sprintf("Unhandled snippet element kind: %v", snippetElement.Kind))
	}
}
func (p *Printer) emitTabStop(node ast.Handle, snippetElement *SnippetElement) {
	debug.Assert(node.Kind() == ast.KindEmptyStatement, "Snippet tab stops can only be emitted on empty statements")
	p.writer.RawWrite(fmt.Sprintf("$%d", snippetElement.Order))
}
func (p *Printer) emitIdentifierText(node ast.Handle) {
	f := ast.GetSourceFileOfNode(node)
	debug.Assert(f == nil || p.currentSourceFile == nil || f.FileName() == p.currentSourceFile.FileName())
	text := p.getTextOfNode(node, false)
	if p.IdToSymbol != nil {
		if symbol, ok := p.IdToSymbol[node]; ok {
			p.writeSymbol(text, symbol)
			return
		}
	}
	p.write(text)
}
func (p *Printer) emitIdentifierName(node ast.Handle) {
	state := p.enterNode(node)
	p.emitIdentifierText(node)
	p.exitNode(node, state)
}
func (p *Printer) emitIdentifierNameNode(node ast.Handle) {
	if node.IsNil() {
		return
	}
	p.emitIdentifierName(node)
}
func (p *Printer) getUniqueHelperName(name string) ast.Handle {
	helperName := p.uniqueHelperNames[name]
	if helperName.IsNil() {
		helperName := p.emitContext.Factory.NewUniqueNameEx(name, AutoGenerateOptions{Flags: GeneratedIdentifierFlagsFileLevel | GeneratedIdentifierFlagsOptimistic})
		p.generateName(helperName)
		p.uniqueHelperNames[name] = helperName
		return helperName
	}
	return p.emitContext.Factory.DeepCloneNode(helperName)
}
func (p *Printer) emitIdentifierReference(node ast.Handle) {
	if (!p.externalHelpersModuleName.IsNil() || p.uniqueHelperNames != nil) && p.emitContext.EmitFlags(node)&EFHelperName != 0 {
		if !p.externalHelpersModuleName.IsNil() {
			helper := p.emitContext.Factory.NewPropertyAccessExpression(p.emitContext.Factory.DeepCloneNode(p.externalHelpersModuleName), ast.Handle{}, p.emitContext.Factory.DeepCloneNode(node), ast.NodeFlagsNone)
			p.emitContext.AssignCommentAndSourceMapRanges(helper, node)
			p.emitPropertyAccessExpression(helper)
			return
		}
		if p.uniqueHelperNames != nil {
			helperName := p.getUniqueHelperName(node.Text())
			p.emitContext.AssignCommentAndSourceMapRanges(helperName, node)
			node = helperName
		}
	}
	state := p.enterNode(node)
	p.emitIdentifierText(node)
	p.exitNode(node, state)
}
func (p *Printer) emitBindingIdentifier(node ast.Handle) {
	if p.uniqueHelperNames != nil && p.emitContext.EmitFlags(node)&EFHelperName != 0 {
		helperName := p.getUniqueHelperName(node.Text())
		p.emitContext.AssignCommentAndSourceMapRanges(helperName, node)
		node = helperName
	}
	state := p.enterNode(node)
	p.emitIdentifierText(node)
	p.exitNode(node, state)
}
func (p *Printer) emitLabelIdentifier(node ast.Handle) {
	state := p.enterNode(node)
	p.emitIdentifierText(node)
	p.exitNode(node, state)
}
func (p *Printer) emitPrivateIdentifier(node ast.Handle) {
	state := p.enterNode(node)
	p.write(p.getTextOfNode(node, false))
	p.exitNode(node, state)
}
func (p *Printer) emitQualifiedName(node ast.Handle) {
	state := p.enterNode(node)
	p.emitEntityName(node.QualifiedNameLeft())
	p.writePunctuation(".")
	p.emitMemberName(node.QualifiedNameRight())
	p.exitNode(node, state)
}
func (p *Printer) emitComputedPropertyName(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("[")
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceDisallowComma)
	p.writePunctuation("]")
	p.exitNode(node, state)
}
func (p *Printer) emitEntityName(node ast.Handle) {
	switch node.Kind() {
	case ast.KindIdentifier:
		p.emitIdentifierReference(node)
	case ast.KindQualifiedName:
		p.emitQualifiedName(node)
	case ast.KindPropertyAccessExpression:
		p.emitExpression(node, ast.OperatorPrecedenceDisallowComma)
	default:
		panic(fmt.Sprintf("unexpected EntityName: %v", node.Kind()))
	}
}
func (p *Printer) emitBindingName(node ast.Handle) {
	if node.IsNil() {
		return
	}
	switch node.Kind() {
	case ast.KindIdentifier:
		p.emitBindingIdentifier(node)
	case ast.KindObjectBindingPattern:
		p.emitObjectBindingPattern(node)
	case ast.KindArrayBindingPattern:
		p.emitArrayBindingPattern(node)
	default:
		panic(fmt.Sprintf("unexpected BindingName: %v", node.Kind()))
	}
}
func (p *Printer) emitPropertyName(node ast.Handle) {
	if node.IsNil() {
		return
	}
	savedWriteKind := p.writeKind
	p.writeKind = WriteKindProperty
	switch node.Kind() {
	case ast.KindIdentifier:
		p.emitIdentifierName(node)
	case ast.KindPrivateIdentifier:
		p.emitPrivateIdentifier(node)
	case ast.KindStringLiteral:
		p.emitStringLiteral(node)
	case ast.KindNoSubstitutionTemplateLiteral:
		p.emitNoSubstitutionTemplateLiteral(node)
	case ast.KindNumericLiteral:
		p.emitNumericLiteral(node)
	case ast.KindBigIntLiteral:
		p.emitBigIntLiteral(node)
	case ast.KindComputedPropertyName:
		p.emitComputedPropertyName(node)
	default:
		panic(fmt.Sprintf("unexpected PropertyName: %v", node.Kind()))
	}
	p.writeKind = savedWriteKind
}
func (p *Printer) emitMemberName(node ast.Handle) {
	if node.IsNil() {
		return
	}
	switch node.Kind() {
	case ast.KindIdentifier:
		p.emitIdentifierName(node)
	case ast.KindPrivateIdentifier:
		p.emitPrivateIdentifier(node)
	default:
		panic(fmt.Sprintf("unexpected MemberName: %v", node.Kind()))
	}
}
func (p *Printer) emitModuleName(node ast.Handle) {
	if node.IsNil() {
		return
	}
	switch node.Kind() {
	case ast.KindIdentifier:
		p.emitBindingIdentifier(node)
	case ast.KindStringLiteral:
		p.emitStringLiteral(node)
	default:
		panic(fmt.Sprintf("unexpected ModuleName: %v", node.Kind()))
	}
}
func (p *Printer) emitModuleExportName(node ast.Handle) {
	if node.IsNil() {
		return
	}
	switch node.Kind() {
	case ast.KindIdentifier:
		p.emitIdentifierName(node)
	case ast.KindStringLiteral:
		p.emitStringLiteral(node)
	default:
		panic(fmt.Sprintf("unexpected ModuleExportName: %v", node.Kind()))
	}
}
func (p *Printer) emitImportAttributeName(node ast.Handle) {
	switch node.Kind() {
	case ast.KindIdentifier:
		p.emitIdentifierName(node)
	case ast.KindStringLiteral:
		p.emitStringLiteral(node)
	default:
		panic(fmt.Sprintf("unexpected ImportAttributeName: %v", node.Kind()))
	}
}
func (p *Printer) emitNestedModuleName(node ast.Handle) {
	if node.IsNil() {
		return
	}
	switch node.Kind() {
	case ast.KindIdentifier:
		p.emitIdentifierName(node)
	case ast.KindStringLiteral:
		p.emitStringLiteral(node)
	default:
		panic(fmt.Sprintf("unexpected ModuleName: %v", node.Kind()))
	}
}
func (p *Printer) emitModifierList(parentNode ast.Handle, modifiers ast.ListRef, allowDecorators bool) int {
	if modifiers == 0 || parentNode.Store().ListLen(modifiers) == 0 {
		return parentNode.Pos()
	}
	if core.Every(parentNode.Store().ListSlice(modifiers), ast.IsModifier) {
		p.emitList((*Printer).emitKeywordNode, parentNode, modifiers, LFModifiers)
	} else if core.Every(parentNode.Store().ListSlice(modifiers), ast.IsDecorator) {
		if !allowDecorators {
			return parentNode.Pos()
		}
		p.emitList((*Printer).emitModifierLike, parentNode, modifiers, LFDecorators)
	} else {
		if p.OnBeforeEmitNodeList != nil {
			p.OnBeforeEmitNodeList(modifiers)
		}
		type Mode int
		const (
			ModeNone Mode = iota
			ModeModifiers
			ModeDecorators
		)
		lastMode := ModeNone
		mode := ModeNone
		start := 0
		pos := 0
		var lastModifier ast.Handle
		for start < parentNode.Store().ListLen(modifiers) {
			for pos < parentNode.Store().ListLen(modifiers) {
				lastModifier = parentNode.Store().ListAt(modifiers, pos)
				if ast.IsDecorator(lastModifier) {
					mode = ModeDecorators
				} else {
					mode = ModeModifiers
				}
				if lastMode == ModeNone {
					lastMode = mode
				} else if mode != lastMode {
					break
				}
				pos++
			}
			textRange := core.NewTextRange(-1, -1)
			if start == 0 {
				textRange = core.NewTextRange(parentNode.Store().ListLoc(modifiers).Pos(), textRange.End())
			}
			if pos == parentNode.Store().ListLen(modifiers)-1 {
				textRange = core.NewTextRange(textRange.Pos(), parentNode.Store().ListLoc(modifiers).End())
			}
			if allowDecorators || lastMode == ModeModifiers {
				p.emitListItems((*Printer).emitModifierLike, parentNode, parentNode.Store().ListSlice(modifiers)[start:pos], core.IfElse(lastMode == ModeModifiers, LFModifiers, LFDecorators), false, textRange)
			}
			start = pos
			lastMode = mode
			pos++
		}
		if p.OnAfterEmitNodeList != nil {
			p.OnAfterEmitNodeList(modifiers)
		}
	}
	return greatestEnd(parentNode.Pos(), core.LastOrNil(parentNode.Store().ListSlice(modifiers)))
}
func (p *Printer) emitTypeParameter(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), false)
	p.emitBindingIdentifier(node.Name())
	if !node.TypeParameterDeclarationConstraint().IsNil() {
		p.writeSpace()
		p.writeKeyword("extends")
		p.writeSpace()
		p.emitTypeNodeOutsideExtends(node.TypeParameterDeclarationConstraint())
	}
	if !node.TypeParameterDeclarationDefaultType().IsNil() {
		p.writeSpace()
		p.writeOperator("=")
		p.writeSpace()
		p.emitTypeNodeOutsideExtends(node.TypeParameterDeclarationDefaultType())
	}
	p.exitNode(node, state)
}
func (p *Printer) emitTypeParameterDeclarationNode(node ast.Handle) {
	if ast.IsTypeParameterDeclaration(node) {
		p.emitTypeParameter(node)
	} else {
		p.emitTypeArgument(node)
	}
}
func (p *Printer) emitParameterName(node ast.Handle) {
	savedWriteKind := p.writeKind
	p.writeKind = WriteKindParameter
	p.emitBindingName(node)
	p.writeKind = savedWriteKind
}
func (p *Printer) emitParameter(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), true)
	p.emitTokenNode(node.DotDotDotToken())
	p.emitParameterName(node.Name())
	p.emitTokenNode(node.QuestionToken())
	p.emitTypeAnnotation(node.Type())
	p.emitInitializer(node.Initializer(), greatestEnd(node.Pos(), node.Type(), node.QuestionToken(), node.Name(), node.Store().ListLoc(node.Modifiers())), node)
	p.exitNode(node, state)
}
func (p *Printer) emitParameterDeclarationNode(node ast.Handle) {
	p.emitParameter(node)
}
func (p *Printer) emitDecorator(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("@")
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceLeftHandSide)
	p.exitNode(node, state)
}
func (p *Printer) emitModifierLike(node ast.Handle) {
	switch {
	case ast.IsDecorator(node):
		p.emitDecorator(node)
	case ast.IsModifier(node):
		p.emitKeywordNode(node)
	default:
		panic(fmt.Sprintf("unhandled ModifierLike: %v", node.Kind()))
	}
}
func (p *Printer) emitTypeParameters(parentNode ast.Handle, nodes ast.ListRef) {
	if nodes == 0 {
		return
	}
	p.emitList((*Printer).emitTypeParameterDeclarationNode, parentNode, nodes, LFTypeParameters|core.IfElse(ast.IsArrowFunction(parentNode), LFAllowTrailingComma, LFNone))
}
func (p *Printer) emitTypeAnnotation(node ast.Handle) {
	if node.IsNil() {
		return
	}
	p.writePunctuation(":")
	p.writeSpace()
	p.emitTypeNodeOutsideExtends(node)
}
func (p *Printer) emitInitializer(node ast.Handle, equalTokenPos int, contextNode ast.Handle) {
	if node.IsNil() {
		return
	}
	p.writeSpace()
	p.emitToken(ast.KindEqualsToken, equalTokenPos, WriteKindOperator, contextNode)
	p.writeSpace()
	p.emitExpression(node, ast.OperatorPrecedenceDisallowComma)
}
func (p *Printer) emitParameters(parentNode ast.Handle, parameters ast.ListRef) {
	p.generateAllNames(parentNode, parameters)
	p.emitList((*Printer).emitParameterDeclarationNode, parentNode, parameters, LFParameters)
}
func canEmitSimpleArrowHead(parentNode ast.Handle, parameters ast.ListRef) bool {
	if !ast.IsArrowFunction(parentNode) || parentNode.Store().ListLen(parameters) != 1 {
		return false
	}
	parent := parentNode
	parameter := parentNode.Store().ListAt(parameters, 0)
	return parameter.Pos() == parent.Pos() && parent.TypeParameterList() == 0 && parent.Type().IsNil() && (parent.Modifiers() == 0 || len(parent.Store().ListSlice(parent.Modifiers())) == 0) && !parentNode.Store().ListHasTrailingComma(parameters) && parameter.Modifiers() == 0 && parameter.DotDotDotToken().IsNil() && parameter.QuestionToken().IsNil() && parameter.Type().IsNil() && parameter.Initializer().IsNil() && ast.IsIdentifier(parameter.Name())
}
func (p *Printer) emitParametersForArrow(parentNode ast.Handle, parameters ast.ListRef) {
	if canEmitSimpleArrowHead(parentNode, parameters) {
		p.generateAllNames(parentNode, parameters)
		p.emitList((*Printer).emitParameterDeclarationNode, parentNode, parameters, LFSingleArrowParameter)
	} else {
		p.emitParameters(parentNode, parameters)
	}
}
func (p *Printer) emitParametersForIndexSignature(parentNode ast.Handle, parameters ast.ListRef) {
	p.generateAllNames(parentNode, parameters)
	p.emitList((*Printer).emitParameterDeclarationNode, parentNode, parameters, LFIndexSignatureParameters)
}
func (p *Printer) emitSignature(node ast.Handle) {
	p.emitTypeParameters(node, node.TypeParameterList())
	p.emitParameters(node, node.ParameterList())
	p.emitTypeAnnotation(node.Type())
}
func (p *Printer) emitFunctionBody(body ast.Handle) {
	p.emitContext.AddEmitFlags(body, EFNoSourceMap)
	if p.OnBeforeEmitNode != nil {
		p.OnBeforeEmitNode(body)
	}
	p.generateNames(body)
	p.writePunctuation("{")
	p.increaseIndent()
	detachedState := p.emitDetachedCommentsBeforeStatementList(body, body.Store().ListLoc(body.StatementList()))
	statementOffset := p.emitPrologueDirectives(body.StatementList())
	pos := p.writer.GetTextPos()
	p.emitHelpers(body)
	if p.shouldEmitBlockFunctionBodyOnSingleLine(body) && statementOffset == 0 && pos == p.writer.GetTextPos() {
		p.decreaseIndent()
		p.emitListRange((*Printer).emitStatement, body, body.StatementList(), LFSingleLineFunctionBodyStatements, statementOffset, -1)
		p.increaseIndent()
	} else {
		p.emitListRange((*Printer).emitStatement, body, body.StatementList(), LFMultiLineFunctionBodyStatements, statementOffset, -1)
	}
	p.emitDetachedCommentsAfterStatementList(body, body.Store().ListLoc(body.StatementList()), detachedState)
	p.decreaseIndent()
	p.emitTokenEx(ast.KindCloseBraceToken, body.Store().ListLoc(body.StatementList()).End(), WriteKindPunctuation, body, tefNoComments)
	if p.OnAfterEmitNode != nil {
		p.OnAfterEmitNode(body)
	}
}
func (p *Printer) emitFunctionBodyNode(node ast.Handle) {
	if node.IsNil() {
		p.writeTrailingSemicolon()
		return
	}
	p.writeSpace()
	p.emitFunctionBody(node)
}
func (p *Printer) emitPropertySignature(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), false)
	p.emitPropertyName(node.Name())
	p.emitTokenNode(node.PropertySignatureDeclarationPostfixToken())
	p.emitTypeAnnotation(node.Type())
	p.writeTrailingSemicolon()
	p.exitNode(node, state)
}
func (p *Printer) emitPropertyDeclaration(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), true)
	p.emitPropertyName(node.Name())
	p.emitTokenNode(node.PropertyDeclarationPostfixToken())
	p.emitTypeAnnotation(node.Type())
	p.emitInitializer(node.Initializer(), greatestEnd(node.Name().End(), node.Type(), node.PropertyDeclarationPostfixToken()), node)
	p.writeTrailingSemicolon()
	p.exitNode(node, state)
}
func (p *Printer) emitMethodSignature(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), false)
	p.emitPropertyName(node.Name())
	p.emitTokenNode(node.MethodSignatureDeclarationPostfixToken())
	indented := p.shouldEmitIndented(node)
	p.increaseIndentIf(indented)
	p.pushNameGenerationScope(node)
	p.emitSignature(node)
	p.writeTrailingSemicolon()
	p.popNameGenerationScope(node)
	p.decreaseIndentIf(indented)
	p.exitNode(node, state)
}
func (p *Printer) emitMethodDeclaration(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), true)
	p.emitTokenNode(node.AsteriskToken())
	p.emitPropertyName(node.Name())
	p.emitTokenNode(node.MethodDeclarationPostfixToken())
	indented := p.shouldEmitIndented(node)
	p.increaseIndentIf(indented)
	p.pushNameGenerationScope(node)
	p.emitSignature(node)
	p.emitFunctionBodyNode(node.Body())
	p.popNameGenerationScope(node)
	p.decreaseIndentIf(indented)
	p.exitNode(node, state)
}
func (p *Printer) emitClassStaticBlockDeclaration(node ast.Handle) {
	state := p.enterNode(node)
	p.writeKeyword("static")
	p.pushNameGenerationScope(node)
	p.emitFunctionBodyNode(node.Body())
	p.popNameGenerationScope(node)
	p.exitNode(node, state)
}
func (p *Printer) emitConstructor(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), false)
	p.writeKeyword("constructor")
	indented := p.shouldEmitIndented(node)
	p.increaseIndentIf(indented)
	p.pushNameGenerationScope(node)
	p.emitSignature(node)
	p.emitFunctionBodyNode(node.Body())
	p.popNameGenerationScope(node)
	p.decreaseIndentIf(indented)
	p.exitNode(node, state)
}
func (p *Printer) emitAccessorDeclaration(token ast.Kind, node ast.Handle) {
	state := p.enterNode(node)
	pos := p.emitModifierList(node, node.Modifiers(), true)
	p.emitToken(token, pos, WriteKindKeyword, node)
	p.writeSpace()
	p.emitPropertyName(node.Name())
	indented := p.shouldEmitIndented(node)
	p.increaseIndentIf(indented)
	p.pushNameGenerationScope(node)
	p.emitSignature(node)
	p.emitFunctionBodyNode(node.Body())
	p.popNameGenerationScope(node)
	p.decreaseIndentIf(indented)
	p.exitNode(node, state)
}
func (p *Printer) emitGetAccessorDeclaration(node ast.Handle) {
	p.emitAccessorDeclaration(ast.KindGetKeyword, node)
}
func (p *Printer) emitSetAccessorDeclaration(node ast.Handle) {
	p.emitAccessorDeclaration(ast.KindSetKeyword, node)
}
func (p *Printer) emitCallSignature(node ast.Handle) {
	state := p.enterNode(node)
	indented := p.shouldEmitIndented(node)
	p.increaseIndentIf(indented)
	p.pushNameGenerationScope(node)
	p.emitSignature(node)
	p.writeTrailingSemicolon()
	p.popNameGenerationScope(node)
	p.decreaseIndentIf(indented)
	p.exitNode(node, state)
}
func (p *Printer) emitConstructSignature(node ast.Handle) {
	state := p.enterNode(node)
	p.writeKeyword("new")
	p.writeSpace()
	indented := p.shouldEmitIndented(node)
	p.increaseIndentIf(indented)
	p.pushNameGenerationScope(node)
	p.emitSignature(node)
	p.writeTrailingSemicolon()
	p.popNameGenerationScope(node)
	p.decreaseIndentIf(indented)
	p.exitNode(node, state)
}
func (p *Printer) emitIndexSignature(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), false)
	indented := p.shouldEmitIndented(node)
	p.increaseIndentIf(indented)
	p.pushNameGenerationScope(node)
	p.emitParametersForIndexSignature(node, node.ParameterList())
	p.emitTypeAnnotation(node.Type())
	p.writeTrailingSemicolon()
	p.popNameGenerationScope(node)
	p.decreaseIndentIf(indented)
	p.exitNode(node, state)
}
func (p *Printer) emitClassElement(node ast.Handle) {
	switch node.Kind() {
	case ast.KindPropertyDeclaration:
		p.emitPropertyDeclaration(node)
	case ast.KindMethodDeclaration:
		p.emitMethodDeclaration(node)
	case ast.KindClassStaticBlockDeclaration:
		p.emitClassStaticBlockDeclaration(node)
	case ast.KindConstructor:
		p.emitConstructor(node)
	case ast.KindGetAccessor:
		p.emitGetAccessorDeclaration(node)
	case ast.KindSetAccessor:
		p.emitSetAccessorDeclaration(node)
	case ast.KindIndexSignature:
		p.emitIndexSignature(node)
	case ast.KindSemicolonClassElement:
		p.emitSemicolonClassElement(node)
	case ast.KindNotEmittedStatement:
		p.emitNotEmittedStatement(node)
	case ast.KindJSTypeAliasDeclaration:
		p.emitTypeAliasDeclaration(node)
	default:
		panic(fmt.Sprintf("unexpected ClassElement: %v", node.Kind()))
	}
}
func (p *Printer) emitTypeElement(node ast.Handle) {
	switch node.Kind() {
	case ast.KindPropertySignature:
		p.emitPropertySignature(node)
	case ast.KindMethodSignature:
		p.emitMethodSignature(node)
	case ast.KindCallSignature:
		p.emitCallSignature(node)
	case ast.KindConstructSignature:
		p.emitConstructSignature(node)
	case ast.KindGetAccessor:
		p.emitGetAccessorDeclaration(node)
	case ast.KindSetAccessor:
		p.emitSetAccessorDeclaration(node)
	case ast.KindIndexSignature:
		p.emitIndexSignature(node)
	case ast.KindNotEmittedTypeElement:
		p.emitNotEmittedTypeElement(node)
	default:
		panic(fmt.Sprintf("unexpected TypeElement: %v", node.Kind()))
	}
}
func (p *Printer) emitObjectLiteralElement(node ast.Handle) {
	switch node.Kind() {
	case ast.KindPropertyAssignment:
		p.emitPropertyAssignment(node)
	case ast.KindShorthandPropertyAssignment:
		p.emitShorthandPropertyAssignment(node)
	case ast.KindSpreadAssignment:
		p.emitSpreadAssignment(node)
	case ast.KindMethodDeclaration:
		p.emitMethodDeclaration(node)
	case ast.KindGetAccessor:
		p.emitGetAccessorDeclaration(node)
	case ast.KindSetAccessor:
		p.emitSetAccessorDeclaration(node)
	default:
		panic(fmt.Sprintf("unhandled ObjectLiteralElement: %v", node.Kind()))
	}
}
func (p *Printer) emitKeywordTypeNode(node ast.Handle) {
	p.emitKeywordNode(node)
}
func (p *Printer) emitTypePredicateParameterName(node ast.Handle) {
	switch node.Kind() {
	case ast.KindIdentifier:
		p.emitIdentifierReference(node)
	case ast.KindThisType:
		p.emitThisType(node)
	default:
		panic(fmt.Sprintf("unexpected TypePredicateParameterName: %v", node.Kind()))
	}
}
func (p *Printer) emitTypePredicate(node ast.Handle) {
	state := p.enterNode(node)
	if !node.TypePredicateNodeAssertsModifier().IsNil() {
		p.emitTokenNode(node.TypePredicateNodeAssertsModifier())
		p.writeSpace()
	}
	p.emitTypePredicateParameterName(node.TypePredicateNodeParameterName())
	if !node.Type().IsNil() {
		p.writeSpace()
		p.writeKeyword("is")
		p.writeSpace()
		p.emitTypeNodeOutsideExtends(node.Type())
	}
	p.exitNode(node, state)
}
func (p *Printer) emitTypeArgument(node ast.Handle) {
	p.emitTypeNodeOutsideExtends(node)
}
func (p *Printer) emitTypeArguments(parentNode ast.Handle, nodes ast.ListRef) {
	if nodes == 0 {
		return
	}
	p.emitList((*Printer).emitTypeParameterDeclarationNode, parentNode, nodes, LFTypeArguments)
}
func (p *Printer) emitTypeReference(node ast.Handle) {
	state := p.enterNode(node)
	p.emitEntityName(node.TypeReferenceNodeTypeName())
	p.emitTypeArguments(node, node.TypeArgumentList())
	p.exitNode(node, state)
}

func (p *Printer) emitReturnType(node ast.Handle) {
	if node.IsNil() {
		return
	}
	p.writePunctuation("=>")
	p.writeSpace()
	if p.inExtends && node.Kind() == ast.KindInferType && !node.InferTypeNodeTypeParameter().TypeParameterDeclarationConstraint().IsNil() {
		p.emitTypeNodePreservingExtends(node, ast.TypePrecedenceHighest)
	} else {
		p.emitTypeNodePreservingExtends(node, ast.TypePrecedenceLowest)
	}
}
func (p *Printer) emitFunctionType(node ast.Handle) {
	state := p.enterNode(node)
	indented := p.shouldEmitIndented(node)
	p.increaseIndentIf(indented)
	p.pushNameGenerationScope(node)
	p.emitTypeParameters(node, node.TypeParameterList())
	p.emitParameters(node, node.ParameterList())
	p.writeSpace()
	p.emitReturnType(node.Type())
	p.popNameGenerationScope(node)
	p.decreaseIndentIf(indented)
	p.exitNode(node, state)
}
func (p *Printer) emitConstructorType(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), false)
	p.writeKeyword("new")
	p.writeSpace()
	indented := p.shouldEmitIndented(node)
	p.increaseIndentIf(indented)
	p.pushNameGenerationScope(node)
	p.emitTypeParameters(node, node.TypeParameterList())
	p.emitParameters(node, node.ParameterList())
	p.writeSpace()
	p.emitReturnType(node.Type())
	p.popNameGenerationScope(node)
	p.decreaseIndentIf(indented)
	p.exitNode(node, state)
}
func (p *Printer) emitTypeQuery(node ast.Handle) {
	state := p.enterNode(node)
	p.writeKeyword("typeof")
	p.writeSpace()
	p.emitEntityName(node.TypeQueryNodeExprName())
	p.emitTypeArguments(node, node.TypeArgumentList())
	p.exitNode(node, state)
}
func (p *Printer) emitTypeLiteral(node ast.Handle) {
	state := p.enterNode(node)
	p.pushNameGenerationScope(node)
	p.generateAllMemberNames(node, node.MemberList())
	p.writePunctuation("{")
	flags := core.IfElse(p.shouldEmitOnSingleLine(node), LFSingleLineTypeLiteralMembers, LFMultiLineTypeLiteralMembers)
	p.emitList((*Printer).emitTypeElement, node, node.MemberList(), flags|LFNoSpaceIfEmpty)
	p.writePunctuation("}")
	p.popNameGenerationScope(node)
	p.exitNode(node, state)
}
func (p *Printer) emitArrayType(node ast.Handle) {
	state := p.enterNode(node)
	p.emitPostfixTypeOperand(node.ArrayTypeNodeElementType(), node)
	p.writePunctuation("[")
	p.writePunctuation("]")
	p.exitNode(node, state)
}

func (p *Printer) emitPostfixTypeOperand(operand ast.Handle, parent ast.Handle) {
	if ast.IsParseTreeNode(parent) && operand.Kind() == ast.KindTypeQuery {
		p.emitTypeNode(operand, ast.TypePrecedenceTypeOperator)
		return
	}
	p.emitTypeNode(operand, ast.TypePrecedencePostfix)
}
func (p *Printer) emitTupleElementType(node ast.Handle) {
	p.emitTypeNodeOutsideExtends(node)
}
func (p *Printer) emitTupleType(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindOpenBracketToken, node.Pos(), WriteKindPunctuation, node)
	flags := core.IfElse(p.shouldEmitOnSingleLine(node), LFSingleLineTupleTypeElements, LFMultiLineTupleTypeElements)
	p.emitList((*Printer).emitTupleElementType, node, node.ElementList(), flags|LFNoSpaceIfEmpty)
	p.emitToken(ast.KindCloseBracketToken, node.Store().ListLoc(node.ElementList()).End(), WriteKindPunctuation, node)
	p.exitNode(node, state)
}
func (p *Printer) emitRestType(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("...")
	p.emitTypeNodeOutsideExtends(node.Type())
	p.exitNode(node, state)
}
func (p *Printer) emitOptionalType(node ast.Handle) {
	state := p.enterNode(node)
	p.emitPostfixTypeOperand(node.Type(), node)
	p.writePunctuation("?")
	p.exitNode(node, state)
}
func (p *Printer) emitNamedTupleMember(node ast.Handle) {
	state := p.enterNode(node)
	p.emitPunctuationNode(node.DotDotDotToken())
	p.emitIdentifierName(node.Name())
	p.emitPunctuationNode(node.QuestionToken())
	p.emitToken(ast.KindColonToken, greatestEnd(node.Name().End(), node.QuestionToken()), WriteKindPunctuation, node)
	p.writeSpace()
	p.emitTypeNodeOutsideExtends(node.Type())
	p.exitNode(node, state)
}
func (p *Printer) emitUnionTypeConstituent(node ast.Handle) {
	p.emitTypeNode(node, ast.TypePrecedenceTypeOperator)
}
func (p *Printer) emitUnionType(node ast.Handle) {
	state := p.enterNode(node)
	p.emitList((*Printer).emitUnionTypeConstituent, node, node.TypeList(), LFUnionTypeConstituents)
	p.exitNode(node, state)
}
func (p *Printer) emitIntersectionTypeConstituent(node ast.Handle) {
	p.emitTypeNode(node, ast.TypePrecedenceTypeOperator)
}
func (p *Printer) emitIntersectionType(node ast.Handle) {
	state := p.enterNode(node)
	p.emitList((*Printer).emitIntersectionTypeConstituent, node, node.TypeList(), LFIntersectionTypeConstituents)
	p.exitNode(node, state)
}
func (p *Printer) emitConditionalType(node ast.Handle) {
	state := p.enterNode(node)
	p.emitTypeNode(node.ConditionalTypeNodeCheckType(), ast.TypePrecedenceUnion)
	p.writeSpace()
	p.writeKeyword("extends")
	p.writeSpace()
	p.emitTypeNodeInExtends(node.ConditionalTypeNodeExtendsType())
	p.writeSpace()
	p.writePunctuation("?")
	p.writeSpace()
	p.emitTypeNodeOutsideExtends(node.ConditionalTypeNodeTrueType())
	p.writeSpace()
	p.writePunctuation(":")
	p.writeSpace()
	p.emitTypeNodeOutsideExtends(node.ConditionalTypeNodeFalseType())
	p.exitNode(node, state)
}
func (p *Printer) emitInferTypeParameter(node ast.Handle) {
	state := p.enterNode(node)
	p.emitBindingIdentifier(node.Name())
	if !node.TypeParameterDeclarationConstraint().IsNil() {
		p.writeSpace()
		p.writeKeyword("extends")
		p.writeSpace()
		p.emitTypeNodeInExtends(node.TypeParameterDeclarationConstraint())
	}
	p.exitNode(node, state)
}
func (p *Printer) emitInferType(node ast.Handle) {
	state := p.enterNode(node)
	p.writeKeyword("infer")
	p.writeSpace()
	p.emitInferTypeParameter(node.InferTypeNodeTypeParameter())
	p.exitNode(node, state)
}
func (p *Printer) emitParenthesizedType(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("(")
	p.emitTypeNodeOutsideExtends(node.Type())
	p.writePunctuation(")")
	p.exitNode(node, state)
}
func (p *Printer) emitThisType(node ast.Handle) {
	state := p.enterNode(node)
	p.writeKeyword("this")
	p.exitNode(node, state)
}
func (p *Printer) emitTypeOperator(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(node.TypeOperatorNodeOperator(), node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	p.emitTypeNode(node.Type(), core.IfElse(node.TypeOperatorNodeOperator() == ast.KindReadonlyKeyword, ast.TypePrecedencePostfix, ast.TypePrecedenceTypeOperator))
	p.exitNode(node, state)
}
func (p *Printer) emitIndexedAccessType(node ast.Handle) {
	state := p.enterNode(node)
	p.emitPostfixTypeOperand(node.IndexedAccessTypeNodeObjectType(), node)
	p.writePunctuation("[")
	p.emitTypeNodeOutsideExtends(node.IndexedAccessTypeNodeIndexType())
	p.writePunctuation("]")
	p.exitNode(node, state)
}
func (p *Printer) emitMappedTypeParameter(node ast.Handle) {
	state := p.enterNode(node)
	p.emitBindingIdentifier(node.Name())
	p.writeSpace()
	p.writeKeyword("in")
	p.writeSpace()
	p.emitTypeNodeOutsideExtends(node.TypeParameterDeclarationConstraint())
	p.exitNode(node, state)
}
func (p *Printer) emitMappedType(node ast.Handle) {
	state := p.enterNode(node)
	singleLine := p.shouldEmitOnSingleLine(node)
	p.writePunctuation("{")
	if singleLine {
		p.writeSpace()
	} else {
		p.writeLine()
		p.increaseIndent()
	}
	if !node.MappedTypeNodeReadonlyToken().IsNil() {
		p.emitTokenNode(node.MappedTypeNodeReadonlyToken())
		if node.MappedTypeNodeReadonlyToken().Kind() != ast.KindReadonlyKeyword {
			p.writeKeyword("readonly")
		}
		p.writeSpace()
	}
	p.writePunctuation("[")
	p.emitMappedTypeParameter(node.MappedTypeNodeTypeParameter())
	if !node.MappedTypeNodeNameType().IsNil() {
		p.writeSpace()
		p.writeKeyword("as")
		p.writeSpace()
		p.emitTypeNodeOutsideExtends(node.MappedTypeNodeNameType())
	}
	p.writePunctuation("]")
	if !node.QuestionToken().IsNil() {
		p.emitPunctuationNode(node.QuestionToken())
		if node.QuestionToken().Kind() != ast.KindQuestionToken {
			p.writePunctuation("?")
		}
	}
	p.writePunctuation(":")
	p.writeSpace()
	p.emitTypeNodeOutsideExtends(node.Type())
	p.writeTrailingSemicolon()
	if node.MemberList() != 0 {
		if node.Store().ListLen(node.MemberList()) > 0 {
			if singleLine {
				p.writeSpace()
			} else {
				p.writeLine()
			}
			p.emitList((*Printer).emitTypeElement, node, node.MemberList(), LFPreserveLines)
		}
	}
	if singleLine {
		p.writeSpace()
	} else {
		p.writeLine()
		p.decreaseIndent()
	}
	p.writePunctuation("}")
	p.exitNode(node, state)
}
func (p *Printer) emitLiteralType(node ast.Handle) {
	state := p.enterNode(node)
	p.emitExpression(node.LiteralTypeNodeLiteral(), ast.OperatorPrecedenceComma)
	p.exitNode(node, state)
}
func (p *Printer) emitTemplateTypeSpan(node ast.Handle) {
	state := p.enterNode(node)
	p.emitTypeNodeOutsideExtends(node.Type())
	p.emitTemplateMiddleTail(node.TemplateLiteralTypeSpanLiteral())
	p.exitNode(node, state)
}
func (p *Printer) emitTemplateTypeSpanNode(node ast.Handle) {
	p.emitTemplateTypeSpan(node)
}
func (p *Printer) emitTemplateType(node ast.Handle) {
	state := p.enterNode(node)
	p.emitTemplateHead(node.TemplateLiteralTypeNodeHead())
	p.emitList((*Printer).emitTemplateTypeSpanNode, node, node.TemplateSpanList(), LFTemplateExpressionSpans)
	p.exitNode(node, state)
}
func (p *Printer) emitImportTypeNodeAttributes(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("{")
	p.writeSpace()
	p.writeKeyword(core.IfElse(node.ImportAttributesToken() == ast.KindAssertKeyword, "assert", "with"))
	p.writePunctuation(":")
	p.writeSpace()
	p.emitList((*Printer).emitImportAttributeNode, node, node.ImportAttributesAttributes(), LFImportAttributes)
	p.writeSpace()
	p.writePunctuation("}")
	p.exitNode(node, state)
}
func (p *Printer) emitImportTypeNode(node ast.Handle) {
	state := p.enterNode(node)
	if node.ImportTypeNodeIsTypeOf() {
		p.writeKeyword("typeof")
		p.writeSpace()
	}
	p.writeKeyword("import")
	p.writePunctuation("(")
	p.emitTypeNodeOutsideExtends(node.ImportTypeNodeArgument())
	if !node.ImportTypeNodeAttributes().IsNil() {
		p.writePunctuation(",")
		p.writeSpace()
		p.emitImportTypeNodeAttributes(node.ImportTypeNodeAttributes())
	}
	p.writePunctuation(")")
	if !node.ImportTypeNodeQualifier().IsNil() {
		p.writePunctuation(".")
		p.emitEntityName(node.ImportTypeNodeQualifier())
	}
	p.emitTypeArguments(node, node.TypeArgumentList())
	p.exitNode(node, state)
}

func (p *Printer) emitTypeNodeInExtends(node ast.Handle) {
	savedInExtends := p.inExtends
	p.inExtends = true
	p.emitTypeNodePreservingExtends(node, ast.TypePrecedenceLowest)
	p.inExtends = savedInExtends
}

func (p *Printer) emitTypeNodeOutsideExtends(node ast.Handle) {
	savedInExtends := p.inExtends
	p.inExtends = false
	p.emitTypeNodePreservingExtends(node, ast.TypePrecedenceLowest)
	p.inExtends = savedInExtends
}

func (p *Printer) emitTypeNodePreservingExtends(node ast.Handle, precedence ast.TypePrecedence) {
	p.emitTypeNode(node, precedence)
}
func (p *Printer) emitTypeNode(node ast.Handle, precedence ast.TypePrecedence) {
	if p.inExtends && precedence <= ast.TypePrecedenceConditional {
		precedence = ast.TypePrecedenceFunction
	}
	savedInExtends := p.inExtends
	parens := ast.GetTypeNodePrecedence(node) < precedence
	if parens {
		p.inExtends = false
		p.writePunctuation("(")
	}
	switch node.Kind() {
	case ast.KindAnyKeyword, ast.KindUnknownKeyword, ast.KindNumberKeyword, ast.KindBigIntKeyword, ast.KindObjectKeyword, ast.KindBooleanKeyword, ast.KindStringKeyword, ast.KindSymbolKeyword, ast.KindVoidKeyword, ast.KindUndefinedKeyword, ast.KindNeverKeyword, ast.KindIntrinsicKeyword:
		p.emitKeywordTypeNode(node)
	case ast.KindTypePredicate:
		p.emitTypePredicate(node)
	case ast.KindTypeReference:
		p.emitTypeReference(node)
	case ast.KindFunctionType:
		p.emitFunctionType(node)
	case ast.KindConstructorType:
		p.emitConstructorType(node)
	case ast.KindTypeQuery:
		p.emitTypeQuery(node)
	case ast.KindTypeLiteral:
		p.emitTypeLiteral(node)
	case ast.KindArrayType:
		p.emitArrayType(node)
	case ast.KindTupleType:
		p.emitTupleType(node)
	case ast.KindOptionalType:
		p.emitOptionalType(node)
	case ast.KindRestType:
		p.emitRestType(node)
	case ast.KindUnionType:
		p.emitUnionType(node)
	case ast.KindIntersectionType:
		p.emitIntersectionType(node)
	case ast.KindConditionalType:
		p.emitConditionalType(node)
	case ast.KindInferType:
		p.emitInferType(node)
	case ast.KindParenthesizedType:
		p.emitParenthesizedType(node)
	case ast.KindThisType:
		p.emitThisType(node)
	case ast.KindTypeOperator:
		p.emitTypeOperator(node)
	case ast.KindIndexedAccessType:
		p.emitIndexedAccessType(node)
	case ast.KindMappedType:
		p.emitMappedType(node)
	case ast.KindLiteralType:
		p.emitLiteralType(node)
	case ast.KindNamedTupleMember:
		p.emitNamedTupleMember(node)
	case ast.KindTemplateLiteralType:
		p.emitTemplateType(node)
	case ast.KindTemplateLiteralTypeSpan:
		p.emitTemplateTypeSpan(node)
	case ast.KindImportType:
		p.emitImportTypeNode(node)
	case ast.KindPropertyAccessExpression:
		p.emitPropertyAccessExpression(node)
	case ast.KindExpressionWithTypeArguments:
		p.emitExpressionWithTypeArguments(node)
	case ast.KindJSDocAllType:
		p.emitJSDocAllType(node)
	case ast.KindJSDocNonNullableType:
		p.emitJSDocNonNullableType(node)
	case ast.KindJSDocNullableType:
		p.emitJSDocNullableType(node)
	case ast.KindJSDocOptionalType:
		p.emitJSDocOptionalType(node)
	case ast.KindJSDocVariadicType:
		p.emitJSDocVariadicType(node)
	default:
		panic(fmt.Sprintf("unhandled TypeNode: %v", node.Kind()))
	}
	if parens {
		p.writePunctuation(")")
	}
	p.inExtends = savedInExtends
}
func (p *Printer) emitObjectBindingPattern(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("{")
	p.emitList((*Printer).emitBindingElementNode, node, node.ElementList(), LFObjectBindingPatternElements)
	p.writePunctuation("}")
	p.exitNode(node, state)
}
func (p *Printer) emitArrayBindingPattern(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("[")
	p.emitList((*Printer).emitBindingElementNode, node, node.ElementList(), LFArrayBindingPatternElements)
	p.writePunctuation("]")
	p.exitNode(node, state)
}
func (p *Printer) emitBindingElement(node ast.Handle) {
	state := p.enterNode(node)
	p.emitTokenNode(node.DotDotDotToken())
	if !node.PropertyName().IsNil() {
		p.emitPropertyName(node.PropertyName())
		p.writePunctuation(":")
		p.writeSpace()
	}
	if name := node.Name(); !name.IsNil() {
		p.emitBindingName(name)
		p.emitInitializer(node.Initializer(), node.Name().End(), node)
	}
	p.exitNode(node, state)
}
func (p *Printer) emitBindingElementNode(node ast.Handle) {
	p.emitBindingElement(node)
}
func (p *Printer) emitJSDocAllType(node ast.Handle) {
	p.emitKeywordNode(node)
}
func (p *Printer) emitJSDocNonNullableType(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("!")
	p.emitTypeNode(node.Type(), ast.TypePrecedenceNonArray)
	p.exitNode(node, state)
}
func (p *Printer) emitJSDocNullableType(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("?")
	p.emitTypeNode(node.Type(), ast.TypePrecedenceNonArray)
	p.exitNode(node, state)
}
func (p *Printer) emitJSDocOptionalType(node ast.Handle) {
	state := p.enterNode(node)
	p.emitTypeNode(node.Type(), ast.TypePrecedenceJSDoc)
	p.writePunctuation("=")
	p.exitNode(node, state)
}
func (p *Printer) emitJSDocVariadicType(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("...")
	p.emitTypeNode(node.Type(), ast.TypePrecedenceJSDoc)
	p.exitNode(node, state)
}
func (p *Printer) emitKeywordExpression(node ast.Handle) {
	p.emitKeywordNode(node)
}
func (p *Printer) emitArrayLiteralExpressionElement(node ast.Handle) {
	p.emitExpression(node, ast.OperatorPrecedenceSpread)
}
func (p *Printer) emitArrayLiteralExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.emitList((*Printer).emitArrayLiteralExpressionElement, node, node.ElementList(), LFArrayLiteralExpressionElements|core.IfElse(node.ArrayLiteralExpressionMultiLine(), LFPreferNewLine, LFNone))
	p.exitNode(node, state)
}
func (p *Printer) emitObjectLiteralExpression(node ast.Handle) {
	state := p.enterNode(node)
	indented := p.shouldEmitIndented(node)
	p.increaseIndentIf(indented)
	p.pushNameGenerationScope(node)
	p.generateAllMemberNames(node, node.PropertyList())
	p.emitList((*Printer).emitObjectLiteralElement, node, node.PropertyList(), LFObjectLiteralExpressionProperties|core.IfElse(node.ObjectLiteralExpressionMultiLine(), LFPreferNewLine, LFNone)|core.IfElse(p.shouldAllowTrailingComma(node, node.PropertyList()), LFAllowTrailingComma, LFNone))
	p.popNameGenerationScope(node)
	p.decreaseIndentIf(indented)
	p.exitNode(node, state)
}

func (p *Printer) mayNeedDotDotForPropertyAccess(expression ast.Handle) bool {
	expression = ast.SkipPartiallyEmittedExpressions(expression)
	if ast.IsNumericLiteral(expression) {
		text := p.getLiteralTextOfNode(expression, nil, getLiteralTextFlagsNeverAsciiEscape)
		return expression.NumericLiteralTokenFlags()&ast.TokenFlagsWithSpecifier == 0 && !strings.Contains(text, scanner.TokenToString(ast.KindDotToken)) && !strings.Contains(text, "E") && !strings.Contains(text, "e")
	}
	return false
}
func (p *Printer) emitPropertyAccessExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.emitExpression(node.Expression(), core.IfElse(ast.IsOptionalChain(node), ast.OperatorPrecedenceOptionalChain, ast.OperatorPrecedenceMember))
	token := node.QuestionDotToken()
	if token.IsNil() {
		token = p.emitContext.Factory.NewToken(ast.KindDotToken)
		token.SetLoc(core.NewTextRange(node.Expression().End(), node.Name().Pos()))
		p.emitContext.AddEmitFlags(token, EFNoSourceMap)
	}
	linesBeforeDot := p.getLinesBetweenNodes(node, node.Expression(), token)
	p.writeLineRepeat(linesBeforeDot)
	p.increaseIndentIf(linesBeforeDot > 0)
	shouldEmitDotDot := token.Kind() != ast.KindQuestionDotToken && p.mayNeedDotDotForPropertyAccess(node.Expression()) && !p.writer.HasTrailingComment() && !p.writer.HasTrailingWhitespace()
	if shouldEmitDotDot {
		p.writePunctuation(".")
	}
	if !node.QuestionDotToken().IsNil() {
		p.emitTokenNode(token)
	} else {
		p.emitToken(ast.KindDotToken, node.Expression().End(), WriteKindPunctuation, node)
	}
	linesAfterDot := p.getLinesBetweenNodes(node, token, node.Name())
	p.writeLineRepeat(linesAfterDot)
	p.increaseIndentIf(linesAfterDot > 0)
	p.emitMemberName(node.Name())
	p.decreaseIndentIf(linesAfterDot > 0)
	p.decreaseIndentIf(linesBeforeDot > 0)
	p.exitNode(node, state)
}
func (p *Printer) emitElementAccessExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.emitExpression(node.Expression(), core.IfElse(ast.IsOptionalChain(node), ast.OperatorPrecedenceOptionalChain, ast.OperatorPrecedenceMember))
	p.emitTokenNode(node.QuestionDotToken())
	p.emitToken(ast.KindOpenBracketToken, greatestEnd(-1, node.Expression(), node.QuestionDotToken()), WriteKindPunctuation, node)
	p.emitExpression(node.ElementAccessExpressionArgumentExpression(), ast.OperatorPrecedenceComma)
	p.emitToken(ast.KindCloseBracketToken, node.ElementAccessExpressionArgumentExpression().End(), WriteKindPunctuation, node)
	p.exitNode(node, state)
}
func (p *Printer) emitArgument(node ast.Handle) {
	p.emitExpression(node, ast.OperatorPrecedenceSpread)
}
func (p *Printer) emitCallee(callee ast.Handle, parentNode ast.Handle) {
	if p.shouldEmitIndirectCall(parentNode) {
		p.writePunctuation("(")
		p.writeLiteral("0")
		p.writePunctuation(",")
		p.writeSpace()
		p.emitExpression(callee, ast.OperatorPrecedenceComma)
		p.writePunctuation(")")
	} else if parentNode.Kind() == ast.KindCallExpression && isNewExpressionWithoutArguments(ast.SkipPartiallyEmittedExpressions(callee)) {
		p.emitExpression(callee, ast.OperatorPrecedenceParentheses)
	} else {
		p.emitExpression(callee, core.IfElse(ast.IsOptionalChain(parentNode), ast.OperatorPrecedenceOptionalChain, ast.OperatorPrecedenceMember))
	}
}
func (p *Printer) emitCallExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.emitCallee(node.Expression(), node)
	p.emitTokenNode(node.QuestionDotToken())
	p.emitTypeArguments(node, node.TypeArgumentList())
	p.emitList((*Printer).emitArgument, node, node.ArgumentList(), LFCallExpressionArguments)
	p.exitNode(node, state)
}
func (p *Printer) emitNewExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindNewKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	if ast.SkipPartiallyEmittedExpressions(node.Expression()).Kind() == ast.KindCallExpression {
		p.emitExpression(node.Expression(), ast.OperatorPrecedenceParentheses)
	} else {
		p.emitExpression(node.Expression(), ast.OperatorPrecedenceMember)
	}
	p.emitTypeArguments(node, node.TypeArgumentList())
	p.emitList((*Printer).emitArgument, node, node.ArgumentList(), LFNewExpressionArguments)
	p.exitNode(node, state)
}
func (p *Printer) emitTemplateLiteral(node ast.Handle) {
	switch node.Kind() {
	case ast.KindNoSubstitutionTemplateLiteral:
		p.emitNoSubstitutionTemplateLiteral(node)
	case ast.KindTemplateExpression:
		p.emitTemplateExpression(node)
	default:
		panic(fmt.Sprintf("unhandled TemplateLiteral: %v", node.Kind()))
	}
}
func (p *Printer) emitTaggedTemplateExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.emitCallee(node.TaggedTemplateExpressionTag(), node)
	p.emitTypeArguments(node, node.TypeArgumentList())
	p.writeSpace()
	p.emitTemplateLiteral(node.TaggedTemplateExpressionTemplate())
	p.exitNode(node, state)
}
func (p *Printer) emitTypeAssertionExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("<")
	p.emitTypeNodeOutsideExtends(node.Type())
	p.writePunctuation(">")
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceUpdate)
	p.exitNode(node, state)
}
func (p *Printer) emitParenthesizedExpression(node ast.Handle) {
	state := p.enterNode(node)
	openParenPos := p.emitToken(ast.KindOpenParenToken, node.Pos(), WriteKindPunctuation, node)
	indented := p.writeLineSeparatorsAndIndentBefore(node.Expression(), node)
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceComma)
	p.writeLineSeparatorsAfter(node.Expression(), node)
	p.decreaseIndentIf(indented)
	closeParenPos := openParenPos
	if !node.Expression().IsNil() {
		closeParenPos = node.Expression().End()
	}
	p.emitToken(ast.KindCloseParenToken, closeParenPos, WriteKindPunctuation, node)
	p.exitNode(node, state)
}
func (p *Printer) emitFunctionExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.generateNameIfNeeded(node.Name())
	p.emitModifierList(node, node.Modifiers(), false)
	p.writeKeyword("function")
	p.emitTokenNode(node.AsteriskToken())
	p.writeSpace()
	p.emitIdentifierNameNode(node.Name())
	indented := p.shouldEmitIndented(node)
	p.increaseIndentIf(indented)
	p.pushNameGenerationScope(node)
	p.emitSignature(node)
	p.emitFunctionBodyNode(node.Body())
	p.popNameGenerationScope(node)
	p.decreaseIndentIf(indented)
	p.exitNode(node, state)
}
func (p *Printer) emitConciseBody(node ast.Handle) {
	switch {
	case ast.IsBlock(node):
		p.emitFunctionBody(node)
	case ast.IsObjectLiteralExpression(ast.GetLeftmostExpression(node, false)):
		paren := p.emitContext.Factory.NewParenthesizedExpression(node)
		paren.SetLoc(node.Loc())
		p.emitExpression(paren, ast.OperatorPrecedenceLowest)
	case ast.IsExpression(node):
		p.emitExpression(node, ast.OperatorPrecedenceYield)
	default:
		panic(fmt.Sprintf("unexpected ConciseBody: %v", node.Kind()))
	}
}
func (p *Printer) emitArrowFunction(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), false)
	indented := p.shouldEmitIndented(node)
	p.increaseIndentIf(indented)
	p.pushNameGenerationScope(node)
	p.emitTypeParameters(node, node.TypeParameterList())
	p.emitParametersForArrow(node, node.ParameterList())
	p.emitTypeAnnotation(node.Type())
	p.writeSpace()
	p.emitTokenNode(node.ArrowFunctionEqualsGreaterThanToken())
	p.writeSpace()
	p.emitConciseBody(node.Body())
	p.popNameGenerationScope(node)
	p.decreaseIndentIf(indented)
	p.exitNode(node, state)
}
func (p *Printer) emitDeleteExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindDeleteKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceUnary)
	p.exitNode(node, state)
}
func (p *Printer) emitTypeOfExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindTypeOfKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceUnary)
	p.exitNode(node, state)
}
func (p *Printer) emitVoidExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindVoidKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceUnary)
	p.exitNode(node, state)
}
func (p *Printer) emitAwaitExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindAwaitKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceUnary)
	p.exitNode(node, state)
}
func (p *Printer) emitPrefixUnaryExpression(node ast.Handle) {
	state := p.enterNode(node)
	operator := node.PrefixUnaryExpressionOperator()
	operand := node.PrefixUnaryExpressionOperand()
	p.emitToken(operator, node.Pos(), WriteKindOperator, node)
	if operand.Kind() == ast.KindPrefixUnaryExpression {
		inner := operand.PrefixUnaryExpressionOperator()
		if (operator == ast.KindPlusToken && (inner == ast.KindPlusToken || inner == ast.KindPlusPlusToken)) || (operator == ast.KindMinusToken && (inner == ast.KindMinusToken || inner == ast.KindMinusMinusToken)) {
			p.writeSpace()
		}
	}
	p.emitExpression(node.PrefixUnaryExpressionOperand(), ast.OperatorPrecedenceUnary)
	p.exitNode(node, state)
}
func (p *Printer) emitPostfixUnaryExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.emitExpression(node.PostfixUnaryExpressionOperand(), ast.OperatorPrecedenceLeftHandSide)
	p.emitToken(node.PostfixUnaryExpressionOperator(), node.PostfixUnaryExpressionOperand().End(), WriteKindOperator, node)
	p.exitNode(node, state)
}

func (p *Printer) getLiteralKindOfBinaryPlusOperand(node ast.Handle) ast.Kind {
	node = ast.SkipPartiallyEmittedExpressions(node)
	if ast.IsLiteralKind(node.Kind()) {
		return node.Kind()
	}
	if node.Kind() == ast.KindBinaryExpression {
		if n := node; n.Operator().Kind() == ast.KindPlusToken {
			leftKind := p.getLiteralKindOfBinaryPlusOperand(n.Left())
			literalKind := ast.KindUnknown
			if ast.IsLiteralKind(leftKind) && leftKind == p.getLiteralKindOfBinaryPlusOperand(n.Right()) {
				literalKind = leftKind
			}
			return literalKind
		}
	}
	return ast.KindUnknown
}
func (p *Printer) getBinaryExpressionPrecedence(node ast.Handle) (leftPrec ast.OperatorPrecedence, rightPrec ast.OperatorPrecedence) {
	precedence := ast.GetExpressionPrecedence(node)
	leftPrec = precedence
	rightPrec = precedence
	switch precedence {
	case ast.OperatorPrecedenceComma:
		break
	case ast.OperatorPrecedenceAssignment:
		leftPrec = ast.OperatorPrecedenceConditional
		rightPrec = ast.OperatorPrecedenceYield
	case ast.OperatorPrecedenceLogicalOR:
		rightPrec = ast.OperatorPrecedenceLogicalAND
	case ast.OperatorPrecedenceLogicalAND:
		rightPrec = ast.OperatorPrecedenceBitwiseOR
	case ast.OperatorPrecedenceBitwiseOR:
		break
	case ast.OperatorPrecedenceBitwiseXOR:
		break
	case ast.OperatorPrecedenceBitwiseAND:
		break
	case ast.OperatorPrecedenceEquality:
		rightPrec = ast.OperatorPrecedenceRelational
	case ast.OperatorPrecedenceRelational:
		rightPrec = ast.OperatorPrecedenceShift
	case ast.OperatorPrecedenceShift:
		rightPrec = ast.OperatorPrecedenceAdditive
	case ast.OperatorPrecedenceAdditive:
		if node.Operator().Kind() == ast.KindPlusToken && isBinaryOperation(node.Right(), ast.KindPlusToken) {
			leftKind := p.getLiteralKindOfBinaryPlusOperand(node.Left())
			if ast.IsLiteralKind(leftKind) && leftKind == p.getLiteralKindOfBinaryPlusOperand(node.Right()) {
				break
			}
		}
		rightPrec = ast.OperatorPrecedenceMultiplicative
	case ast.OperatorPrecedenceMultiplicative:
		if node.Operator().Kind() == ast.KindAsteriskToken && isBinaryOperation(node.Right(), ast.KindAsteriskToken) {
			break
		}
		rightPrec = ast.OperatorPrecedenceExponentiation
	case ast.OperatorPrecedenceExponentiation:
		leftPrec = ast.OperatorPrecedenceUpdate
	default:
		panic(fmt.Sprintf("unhandled precedence: %v", precedence))
	}
	return leftPrec, rightPrec
}
func (p *Printer) emitBinaryExpression(node ast.Handle) {
	leftPrec, rightPrec := p.getBinaryExpressionPrecedence(node)
	if emittedLeft := ast.SkipPartiallyEmittedExpressions(node.BinaryExpressionLeft()); ast.NodeIsSynthesized(emittedLeft) && emittedLeft.Kind() == ast.KindBinaryExpression && mixingBinaryOperatorsRequiresParentheses(node.BinaryExpressionOperatorToken().Kind(), emittedLeft.BinaryExpressionOperatorToken().Kind()) {
		leftPrec = ast.OperatorPrecedenceHighest
	}
	if emittedRight := ast.SkipPartiallyEmittedExpressions(node.BinaryExpressionRight()); ast.NodeIsSynthesized(emittedRight) && emittedRight.Kind() == ast.KindBinaryExpression && mixingBinaryOperatorsRequiresParentheses(node.BinaryExpressionOperatorToken().Kind(), emittedRight.BinaryExpressionOperatorToken().Kind()) {
		rightPrec = ast.OperatorPrecedenceHighest
	}
	state := p.enterNode(node)
	p.emitExpression(node.BinaryExpressionLeft(), leftPrec)
	linesBeforeOperator := p.getLinesBetweenNodes(node, node.BinaryExpressionLeft(), node.BinaryExpressionOperatorToken())
	linesAfterOperator := p.getLinesBetweenNodes(node, node.BinaryExpressionOperatorToken(), node.BinaryExpressionRight())
	p.writeLinesAndIndent(linesBeforeOperator, node.BinaryExpressionOperatorToken().Kind() != ast.KindCommaToken)
	p.emitTokenNodeEx(node.BinaryExpressionOperatorToken(), tefNoSourceMaps)
	p.writeLinesAndIndent(linesAfterOperator, true)
	p.emitExpression(node.BinaryExpressionRight(), rightPrec)
	p.decreaseIndentIf(linesAfterOperator > 0)
	p.decreaseIndentIf(linesBeforeOperator > 0)
	p.exitNode(node, state)
}
func (p *Printer) emitShortCircuitExpression(node ast.Handle) {
	if isBinaryOperation(ast.SkipPartiallyEmittedExpressions(node), ast.KindQuestionQuestionToken) {
		p.emitExpression(node, ast.OperatorPrecedenceCoalesce)
	} else {
		p.emitExpression(node, ast.OperatorPrecedenceLogicalOR)
	}
}
func (p *Printer) emitConditionalExpression(node ast.Handle) {
	state := p.enterNode(node)
	linesBeforeQuestion := p.getLinesBetweenNodes(node, node.ConditionalExpressionCondition(), node.QuestionToken())
	linesAfterQuestion := p.getLinesBetweenNodes(node, node.QuestionToken(), node.ConditionalExpressionWhenTrue())
	linesBeforeColon := p.getLinesBetweenNodes(node, node.ConditionalExpressionWhenTrue(), node.ConditionalExpressionColonToken())
	linesAfterColon := p.getLinesBetweenNodes(node, node.ConditionalExpressionColonToken(), node.ConditionalExpressionWhenFalse())
	p.emitShortCircuitExpression(node.ConditionalExpressionCondition())
	p.writeLinesAndIndent(linesBeforeQuestion, true)
	p.emitPunctuationNode(node.QuestionToken())
	p.writeLinesAndIndent(linesAfterQuestion, true)
	p.emitExpression(node.ConditionalExpressionWhenTrue(), ast.OperatorPrecedenceYield)
	p.decreaseIndentIf(linesAfterQuestion > 0)
	p.decreaseIndentIf(linesBeforeQuestion > 0)
	p.writeLinesAndIndent(linesBeforeColon, true)
	p.emitPunctuationNode(node.ConditionalExpressionColonToken())
	p.writeLinesAndIndent(linesAfterColon, true)
	p.emitExpression(node.ConditionalExpressionWhenFalse(), ast.OperatorPrecedenceYield)
	p.decreaseIndentIf(linesAfterColon > 0)
	p.decreaseIndentIf(linesBeforeColon > 0)
	p.exitNode(node, state)
}
func (p *Printer) emitTemplateExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.emitTemplateHead(node.TemplateExpressionHead())
	p.emitList((*Printer).emitTemplateSpanNode, node, node.TemplateSpanList(), LFTemplateExpressionSpans)
	p.exitNode(node, state)
}
func (p *Printer) emitYieldExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindYieldKeyword, node.Pos(), WriteKindKeyword, node)
	p.emitPunctuationNode(node.AsteriskToken())
	if !node.Expression().IsNil() {
		p.writeSpace()
		p.emitExpressionNoASI(node.Expression(), ast.OperatorPrecedenceDisallowComma)
	}
	p.exitNode(node, state)
}
func (p *Printer) emitSpreadElement(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindDotDotDotToken, node.Pos(), WriteKindPunctuation, node)
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceDisallowComma)
	p.exitNode(node, state)
}
func (p *Printer) emitClassExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.generateNameIfNeeded(node.Name())
	pos := p.emitModifierList(node, node.Modifiers(), true)
	p.emitToken(ast.KindClassKeyword, pos, WriteKindKeyword, node)
	if !node.Name().IsNil() {
		p.writeSpace()
		p.emitIdentifierName(node.Name())
	}
	indented := p.shouldEmitIndented(node)
	p.increaseIndentIf(indented)
	p.emitTypeParameters(node, node.TypeParameterList())
	p.emitList((*Printer).emitHeritageClauseNode, node, node.HeritageClauses(), LFClassHeritageClauses)
	p.writeSpace()
	p.writePunctuation("{")
	p.pushNameGenerationScope(node)
	p.generateAllMemberNames(node, node.MemberList())
	p.emitList((*Printer).emitClassElement, node, node.MemberList(), LFClassMembers)
	p.popNameGenerationScope(node)
	p.writePunctuation("}")
	p.decreaseIndentIf(indented)
	p.exitNode(node, state)
}
func (p *Printer) emitOmittedExpression(node ast.Handle) {
	p.exitNode(node, p.enterNode(node))
}
func (p *Printer) emitExpressionWithTypeArguments(node ast.Handle) {
	state := p.enterNode(node)
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceMember)
	p.emitTypeArguments(node, node.TypeArgumentList())
	p.exitNode(node, state)
}
func (p *Printer) emitAsExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceRelational)
	p.writeSpace()
	p.writeKeyword("as")
	p.writeSpace()
	p.emitTypeNodeOutsideExtends(node.Type())
	p.exitNode(node, state)
}
func (p *Printer) emitSatisfiesExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceRelational)
	p.writeSpace()
	p.writeKeyword("satisfies")
	p.writeSpace()
	p.emitTypeNodeOutsideExtends(node.Type())
	p.exitNode(node, state)
}
func (p *Printer) emitNonNullExpression(node ast.Handle) {
	state := p.enterNode(node)
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceMember)
	p.writeOperator("!")
	p.exitNode(node, state)
}
func (p *Printer) emitMetaProperty(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(node.MetaPropertyKeywordToken(), node.Pos(), WriteKindPunctuation, node)
	p.writePunctuation(".")
	p.emitIdentifierName(node.Name())
	p.exitNode(node, state)
}
func (p *Printer) emitPartiallyEmittedExpression(node ast.Handle) {
	type entry struct {
		node  ast.Handle
		state printerState
	}
	var stack core.Stack[entry]
	for {
		state := p.enterNode(node)
		emitFlags := p.emitContext.EmitFlags(node)
		if emitFlags&EFNoLeadingComments == 0 && node.Pos() != node.Expression().Pos() {
			p.emitTrailingCommentsOfPosition(node.Expression().Pos(), false, false)
		}
		stack.Push(entry{node, state})
		if !ast.IsPartiallyEmittedExpression(node.Expression()) {
			break
		}
		node = node.Expression()
	}
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceLowest)
	for stack.Len() > 0 {
		entry := stack.Pop()
		emitFlags := p.emitContext.EmitFlags(node)
		if emitFlags&EFNoTrailingComments == 0 && node.End() != node.Expression().End() {
			p.emitLeadingCommentsOfPosition(node.Expression().End())
		}
		p.exitNode(node, entry.state)
		node = entry.node
	}
}
func (p *Printer) commentWillEmitNewLine(comment ast.CommentRange) bool {
	return comment.Kind == ast.KindSingleLineCommentTrivia || comment.HasTrailingNewLine
}
func (p *Printer) syntheticCommentWillEmitNewLine(comment SynthesizedComment) bool {
	return comment.Kind == ast.KindSingleLineCommentTrivia || comment.HasTrailingNewLine
}
func (p *Printer) willEmitLeadingNewLine(node ast.Handle) bool {
	if p.currentSourceFile == nil {
		return false
	}
	hasLeadingCommentRanges := false
	hasNewLineComment := false
	for comment := range scanner.GetLeadingCommentRanges(p.currentSourceFile.Text(), node.Pos()) {
		hasLeadingCommentRanges = true
		if p.commentWillEmitNewLine(comment) {
			hasNewLineComment = true
		}
	}
	if hasLeadingCommentRanges {
		parseNode := p.emitContext.ParseNode(node)
		if !parseNode.IsNil() && ast.IsParenthesizedExpression(parseNode.Parent()) {
			return true
		}
	}
	if hasNewLineComment {
		return true
	}
	if slices.ContainsFunc(p.emitContext.GetSyntheticLeadingComments(node), p.syntheticCommentWillEmitNewLine) {
		return true
	}
	if ast.IsPartiallyEmittedExpression(node) {
		pee := node
		if node.Pos() != pee.Expression().Pos() {
			for comment := range scanner.GetTrailingCommentRanges(p.currentSourceFile.Text(), pee.Expression().Pos()) {
				if p.commentWillEmitNewLine(comment) {
					return true
				}
			}
		}
		return p.willEmitLeadingNewLine(pee.Expression())
	}
	return false
}

func (p *Printer) parenthesizeExpressionForNoAsi(node ast.Handle) ast.Handle {
	if !p.commentsDisabled {
		switch node.Kind() {
		case ast.KindPartiallyEmittedExpression:
			if p.willEmitLeadingNewLine(node) {
				pee := node
				parseNode := p.emitContext.ParseNode(node)
				if !parseNode.IsNil() && ast.IsParenthesizedExpression(parseNode) {
					parens := p.emitContext.Factory.NewParenthesizedExpression(pee.Expression())
					p.emitContext.SetOriginal(parens, node)
					parens.SetLoc(parseNode.Loc())
					return parens
				}
				return p.emitContext.Factory.NewParenthesizedExpression(node)
			}
			pee := node
			return p.emitContext.Factory.UpdatePartiallyEmittedExpression(pee, p.parenthesizeExpressionForNoAsi(pee.Expression()))
		case ast.KindPropertyAccessExpression:
			pae := node
			return p.emitContext.Factory.UpdatePropertyAccessExpression(pae, p.parenthesizeExpressionForNoAsi(pae.Expression()), pae.QuestionDotToken(), pae.Name(), pae.Flags())
		case ast.KindElementAccessExpression:
			eae := node
			return p.emitContext.Factory.UpdateElementAccessExpression(eae, p.parenthesizeExpressionForNoAsi(eae.Expression()), eae.QuestionDotToken(), eae.ArgumentExpression(), eae.Flags())
		case ast.KindCallExpression:
			ce := node
			return p.emitContext.Factory.UpdateCallExpression(ce, p.parenthesizeExpressionForNoAsi(ce.Expression()), ce.QuestionDotToken(), ce.TypeArgumentList(), ce.ArgumentList(), ce.Flags())
		case ast.KindTaggedTemplateExpression:
			tte := node
			return p.emitContext.Factory.UpdateTaggedTemplateExpression(tte, p.parenthesizeExpressionForNoAsi(tte.TaggedTemplateExpressionTag()), tte.QuestionDotToken(), tte.TypeArgumentList(), tte.TaggedTemplateExpressionTemplate(), tte.Flags())
		case ast.KindPostfixUnaryExpression:
			pue := node
			return p.emitContext.Factory.UpdatePostfixUnaryExpression(pue, p.parenthesizeExpressionForNoAsi(pue.PostfixUnaryExpressionOperand()), pue.PostfixUnaryExpressionOperator())
		case ast.KindBinaryExpression:
			be := node
			return p.emitContext.Factory.UpdateBinaryExpression(be, be.Modifiers(), p.parenthesizeExpressionForNoAsi(be.Left()), be.Type(), be.Operator(), be.Right())
		case ast.KindConditionalExpression:
			ce := node
			return p.emitContext.Factory.UpdateConditionalExpression(ce, p.parenthesizeExpressionForNoAsi(ce.ConditionalExpressionCondition()), ce.QuestionToken(), ce.ConditionalExpressionWhenTrue(), ce.ConditionalExpressionColonToken(), ce.ConditionalExpressionWhenFalse())
		case ast.KindAsExpression:
			ae := node
			return p.emitContext.Factory.UpdateAsExpression(ae, p.parenthesizeExpressionForNoAsi(ae.Expression()), ae.Type())
		case ast.KindSatisfiesExpression:
			se := node
			return p.emitContext.Factory.UpdateSatisfiesExpression(se, p.parenthesizeExpressionForNoAsi(se.Expression()), se.Type())
		case ast.KindNonNullExpression:
			nne := node
			return p.emitContext.Factory.UpdateNonNullExpression(nne, p.parenthesizeExpressionForNoAsi(nne.Expression()), nne.Flags())
		}
	}
	return node
}
func (p *Printer) emitExpressionNoASI(node ast.Handle, precedence ast.OperatorPrecedence) {
	node = p.parenthesizeExpressionForNoAsi(node)
	p.emitExpression(node, precedence)
}
func (p *Printer) emitExpression(node ast.Handle, precedence ast.OperatorPrecedence) {
	parens := ast.GetExpressionPrecedence(ast.SkipPartiallyEmittedExpressions(node)) < precedence
	if parens {
		p.writePunctuation("(")
	}
	switch node.Kind() {
	case ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword:
		p.emitTokenNode(node)
	case ast.KindThisKeyword, ast.KindSuperKeyword, ast.KindImportKeyword:
		p.emitKeywordExpression(node)
	case ast.KindNumericLiteral:
		p.emitNumericLiteral(node)
	case ast.KindBigIntLiteral:
		p.emitBigIntLiteral(node)
	case ast.KindStringLiteral:
		p.emitStringLiteral(node)
	case ast.KindRegularExpressionLiteral:
		p.emitRegularExpressionLiteral(node)
	case ast.KindNoSubstitutionTemplateLiteral:
		p.emitNoSubstitutionTemplateLiteral(node)
	case ast.KindIdentifier:
		p.emitIdentifierReference(node)
	case ast.KindPrivateIdentifier:
		p.emitPrivateIdentifier(node)
	case ast.KindArrayLiteralExpression:
		p.emitArrayLiteralExpression(node)
	case ast.KindObjectLiteralExpression:
		p.emitObjectLiteralExpression(node)
	case ast.KindPropertyAccessExpression:
		p.emitPropertyAccessExpression(node)
	case ast.KindElementAccessExpression:
		p.emitElementAccessExpression(node)
	case ast.KindCallExpression:
		p.emitCallExpression(node)
	case ast.KindNewExpression:
		p.emitNewExpression(node)
	case ast.KindTaggedTemplateExpression:
		p.emitTaggedTemplateExpression(node)
	case ast.KindTypeAssertionExpression:
		p.emitTypeAssertionExpression(node)
	case ast.KindParenthesizedExpression:
		p.emitParenthesizedExpression(node)
	case ast.KindFunctionExpression:
		p.emitFunctionExpression(node)
	case ast.KindArrowFunction:
		p.emitArrowFunction(node)
	case ast.KindDeleteExpression:
		p.emitDeleteExpression(node)
	case ast.KindTypeOfExpression:
		p.emitTypeOfExpression(node)
	case ast.KindVoidExpression:
		p.emitVoidExpression(node)
	case ast.KindAwaitExpression:
		p.emitAwaitExpression(node)
	case ast.KindPrefixUnaryExpression:
		p.emitPrefixUnaryExpression(node)
	case ast.KindPostfixUnaryExpression:
		p.emitPostfixUnaryExpression(node)
	case ast.KindBinaryExpression:
		p.emitBinaryExpression(node)
	case ast.KindConditionalExpression:
		p.emitConditionalExpression(node)
	case ast.KindTemplateExpression:
		p.emitTemplateExpression(node)
	case ast.KindYieldExpression:
		p.emitYieldExpression(node)
	case ast.KindSpreadElement:
		p.emitSpreadElement(node)
	case ast.KindClassExpression:
		p.emitClassExpression(node)
	case ast.KindOmittedExpression:
		p.emitOmittedExpression(node)
	case ast.KindAsExpression:
		p.emitAsExpression(node)
	case ast.KindNonNullExpression:
		p.emitNonNullExpression(node)
	case ast.KindExpressionWithTypeArguments:
		p.emitExpressionWithTypeArguments(node)
	case ast.KindSatisfiesExpression:
		p.emitSatisfiesExpression(node)
	case ast.KindMetaProperty:
		p.emitMetaProperty(node)
	case ast.KindSyntheticExpression:
		panic("SyntheticExpression should never be printed.")
	case ast.KindMissingDeclaration:
		break
	case ast.KindJsxElement:
		p.emitJsxElement(node)
	case ast.KindJsxSelfClosingElement:
		p.emitJsxSelfClosingElement(node)
	case ast.KindJsxFragment:
		p.emitJsxFragment(node)
	case ast.KindSyntaxList:
		panic("SyntaxList should not be printed")
	case ast.KindNotEmittedStatement:
		return
	case ast.KindPartiallyEmittedExpression:
		p.emitPartiallyEmittedExpression(node)
	case ast.KindSyntheticReferenceExpression:
		panic("SyntheticReferenceExpression should not be printed")
	default:
		panic(fmt.Sprintf("unexpected Expression: %v", node.Kind()))
	}
	if parens {
		p.writePunctuation(")")
	}
}
func (p *Printer) emitTemplateSpan(node ast.Handle) {
	state := p.enterNode(node)
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceComma)
	p.emitTemplateMiddleTail(node.TemplateSpanLiteral())
	p.exitNode(node, state)
}
func (p *Printer) emitTemplateSpanNode(node ast.Handle) {
	p.emitTemplateSpan(node)
}
func (p *Printer) emitSemicolonClassElement(node ast.Handle) {
	state := p.enterNode(node)
	p.writeTrailingSemicolon()
	p.exitNode(node, state)
}
func (p *Printer) isEmptyBlock(block ast.Handle, statements ast.ListRef) bool {
	return block.Store().ListLen(statements) == 0 && (p.currentSourceFile == nil || rangeEndIsOnSameLineAsRangeStart(block.Loc(), block.Loc(), p.currentSourceFile))
}
func (p *Printer) emitBlock(node ast.Handle) {
	state := p.enterNode(node)
	p.generateNames(node)
	p.emitToken(ast.KindOpenBraceToken, node.Pos(), WriteKindPunctuation, node)
	format := core.IfElse(!node.BlockMultiLine() && p.isEmptyBlock(node, node.StatementList()) || p.shouldEmitOnSingleLine(node), LFSingleLineBlockStatements, LFMultiLineBlockStatements)
	p.emitList((*Printer).emitStatement, node, node.StatementList(), format)
	p.emitTokenEx(ast.KindCloseBraceToken, node.Store().ListLoc(node.StatementList()).End(), WriteKindPunctuation, node, core.IfElse(format&LFMultiLine != 0, tefIndentLeadingComments, tefNone))
	p.exitNode(node, state)
}
func (p *Printer) emitVariableStatement(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), false)
	p.emitVariableDeclarationList(node.VariableStatementDeclarationList())
	p.writeTrailingSemicolon()
	p.exitNode(node, state)
}
func (p *Printer) emitEmptyStatement(node ast.Handle, isEmbeddedStatement bool) {
	state := p.enterNode(node)
	if isEmbeddedStatement {
		p.writePunctuation(";")
	} else {
		p.writeTrailingSemicolon()
	}
	p.exitNode(node, state)
}
func (p *Printer) emitExpressionStatement(node ast.Handle) {
	state := p.enterNode(node)
	if p.currentSourceFile != nil && p.currentSourceFile.ScriptKind == core.ScriptKindJSON {
		p.emitExpression(node.Expression(), ast.OperatorPrecedenceComma)
	} else if isImmediatelyInvokedFunctionExpressionOrArrowFunction(node.Expression()) {
		p.emitIIFEWithParenthesizedCallee(node.Expression())
	} else {
		switch ast.GetLeftmostExpression(node.Expression(), false).Kind() {
		case ast.KindFunctionExpression, ast.KindObjectLiteralExpression:
			p.emitExpression(node.Expression(), ast.OperatorPrecedenceParentheses)
		default:
			p.emitExpression(node.Expression(), ast.OperatorPrecedenceComma)
		}
	}
	if p.currentSourceFile == nil || p.currentSourceFile.ScriptKind != core.ScriptKindJSON || ast.NodeIsSynthesized(node.Expression()) {
		p.writeTrailingSemicolon()
	}
	p.exitNode(node, state)
}

func (p *Printer) emitIIFEWithParenthesizedCallee(node ast.Handle) {
	call := ast.SkipPartiallyEmittedExpressions(node)
	state := p.enterNode(call)
	p.writePunctuation("(")
	p.emitExpression(call.Expression(), ast.OperatorPrecedenceLowest)
	p.writePunctuation(")")
	p.emitTokenNode(call.QuestionDotToken())
	p.emitTypeArguments(call, call.TypeArgumentList())
	p.emitList((*Printer).emitArgument, call, call.ArgumentList(), LFCallExpressionArguments)
	p.exitNode(call, state)
}
func (p *Printer) emitIfStatement(node ast.Handle) {
	state := p.enterNode(node)
	pos := p.emitToken(ast.KindIfKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	p.emitToken(ast.KindOpenParenToken, pos, WriteKindPunctuation, node)
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceLowest)
	p.emitToken(ast.KindCloseParenToken, node.Expression().End(), WriteKindPunctuation, node)
	p.emitEmbeddedStatement(node, node.IfStatementThenStatement())
	if !node.IfStatementElseStatement().IsNil() {
		p.writeLineOrSpace(node, node.IfStatementThenStatement(), node.IfStatementElseStatement())
		p.emitToken(ast.KindElseKeyword, node.IfStatementThenStatement().End(), WriteKindKeyword, node)
		if node.IfStatementElseStatement().Kind() == ast.KindIfStatement {
			p.writeSpace()
			p.emitIfStatement(node.IfStatementElseStatement())
		} else {
			p.emitEmbeddedStatement(node, node.IfStatementElseStatement())
		}
	}
	p.exitNode(node, state)
}
func (p *Printer) emitWhileClause(node ast.Handle, expression ast.Handle, startPos int) {
	pos := p.emitToken(ast.KindWhileKeyword, startPos, WriteKindKeyword, node)
	p.writeSpace()
	p.emitToken(ast.KindOpenParenToken, pos, WriteKindPunctuation, node)
	p.emitExpression(expression, ast.OperatorPrecedenceLowest)
	p.emitToken(ast.KindCloseParenToken, expression.End(), WriteKindPunctuation, node)
}
func (p *Printer) emitDoStatement(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindDoKeyword, node.Pos(), WriteKindKeyword, node)
	p.emitEmbeddedStatement(node, node.Statement())
	if ast.IsBlock(node.Statement()) && !p.Options.PreserveSourceNewlines {
		p.writeSpace()
	} else {
		p.writeLineOrSpace(node, node.Statement(), node.Expression())
	}
	p.emitWhileClause(node, node.Expression(), node.Statement().End())
	p.writeTrailingSemicolon()
	p.exitNode(node, state)
}
func (p *Printer) emitWhileStatement(node ast.Handle) {
	state := p.enterNode(node)
	p.emitWhileClause(node, node.Expression(), node.Pos())
	p.emitEmbeddedStatement(node, node.Statement())
	p.exitNode(node, state)
}
func (p *Printer) emitForInitializer(node ast.Handle) {
	if node.Kind() == ast.KindVariableDeclarationList {
		p.emitVariableDeclarationList(node)
	} else {
		p.emitExpression(node, ast.OperatorPrecedenceLowest)
	}
}
func (p *Printer) emitForStatement(node ast.Handle) {
	state := p.enterNode(node)
	pos := p.emitToken(ast.KindForKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	pos = p.emitToken(ast.KindOpenParenToken, pos, WriteKindPunctuation, node)
	if !node.Initializer().IsNil() {
		p.emitForInitializer(node.Initializer())
		pos = node.Initializer().End()
	}
	pos = p.emitToken(ast.KindSemicolonToken, pos, WriteKindPunctuation, node)
	if !node.ForStatementCondition().IsNil() {
		p.writeSpace()
		p.emitExpression(node.ForStatementCondition(), ast.OperatorPrecedenceLowest)
		pos = node.ForStatementCondition().End()
	}
	pos = p.emitToken(ast.KindSemicolonToken, pos, WriteKindPunctuation, node)
	if !node.ForStatementIncrementor().IsNil() {
		p.writeSpace()
		p.emitExpression(node.ForStatementIncrementor(), ast.OperatorPrecedenceLowest)
		pos = node.ForStatementIncrementor().End()
	}
	p.emitToken(ast.KindCloseParenToken, pos, WriteKindPunctuation, node)
	p.emitEmbeddedStatement(node, node.Statement())
	p.exitNode(node, state)
}
func (p *Printer) emitForInStatement(node ast.Handle) {
	state := p.enterNode(node)
	pos := p.emitToken(ast.KindForKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	p.emitToken(ast.KindOpenParenToken, pos, WriteKindPunctuation, node)
	p.emitForInitializer(node.Initializer())
	p.writeSpace()
	p.emitToken(ast.KindInKeyword, node.Initializer().End(), WriteKindKeyword, node)
	p.writeSpace()
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceLowest)
	p.emitToken(ast.KindCloseParenToken, node.Expression().End(), WriteKindPunctuation, node)
	p.emitEmbeddedStatement(node, node.Statement())
	p.exitNode(node, state)
}
func (p *Printer) emitForOfStatement(node ast.Handle) {
	state := p.enterNode(node)
	openParenPos := p.emitToken(ast.KindForKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	if !node.ForInOrOfStatementAwaitModifier().IsNil() {
		p.emitKeywordNode(node.ForInOrOfStatementAwaitModifier())
		p.writeSpace()
	}
	p.emitToken(ast.KindOpenParenToken, openParenPos, WriteKindPunctuation, node)
	p.emitForInitializer(node.Initializer())
	p.writeSpace()
	p.emitToken(ast.KindOfKeyword, node.Initializer().End(), WriteKindKeyword, node)
	p.writeSpace()
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceLowest)
	p.emitToken(ast.KindCloseParenToken, node.Expression().End(), WriteKindPunctuation, node)
	p.emitEmbeddedStatement(node, node.Statement())
	p.exitNode(node, state)
}
func (p *Printer) emitContinueStatement(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindContinueKeyword, node.Pos(), WriteKindKeyword, node)
	if !node.Label().IsNil() {
		p.writeSpace()
		p.emitLabelIdentifier(node.Label())
	}
	p.writeTrailingSemicolon()
	p.exitNode(node, state)
}
func (p *Printer) emitBreakStatement(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindBreakKeyword, node.Pos(), WriteKindKeyword, node)
	if !node.Label().IsNil() {
		p.writeSpace()
		p.emitLabelIdentifier(node.Label())
	}
	p.writeTrailingSemicolon()
	p.exitNode(node, state)
}
func (p *Printer) emitReturnStatement(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindReturnKeyword, node.Pos(), WriteKindKeyword, node)
	if !node.Expression().IsNil() {
		p.writeSpace()
		p.emitExpressionNoASI(node.Expression(), ast.OperatorPrecedenceLowest)
	}
	p.writeTrailingSemicolon()
	p.exitNode(node, state)
}
func (p *Printer) emitWithStatement(node ast.Handle) {
	state := p.enterNode(node)
	pos := p.emitToken(ast.KindWithKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	p.emitToken(ast.KindOpenParenToken, pos, WriteKindPunctuation, node)
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceLowest)
	p.emitToken(ast.KindCloseParenToken, node.Expression().End(), WriteKindPunctuation, node)
	p.emitEmbeddedStatement(node, node.Statement())
	p.exitNode(node, state)
}
func (p *Printer) emitSwitchStatement(node ast.Handle) {
	state := p.enterNode(node)
	pos := p.emitToken(ast.KindSwitchKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	p.emitToken(ast.KindOpenParenToken, pos, WriteKindPunctuation, node)
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceLowest)
	p.emitToken(ast.KindCloseParenToken, node.Expression().End(), WriteKindPunctuation, node)
	p.writeSpace()
	p.emitCaseBlock(node.SwitchStatementCaseBlock())
	p.exitNode(node, state)
}
func (p *Printer) emitLabeledStatement(node ast.Handle) {
	state := p.enterNode(node)
	p.emitLabelIdentifier(node.Label())
	p.emitToken(ast.KindColonToken, node.Label().End(), WriteKindPunctuation, node)
	p.writeSpace()
	p.emitStatement(node.Statement())
	p.exitNode(node, state)
}
func (p *Printer) emitThrowStatement(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindThrowKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	p.emitExpressionNoASI(node.Expression(), ast.OperatorPrecedenceLowest)
	p.writeTrailingSemicolon()
	p.exitNode(node, state)
}
func (p *Printer) emitTryStatement(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindTryKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	p.emitBlock(node.TryStatementTryBlock())
	if !node.TryStatementCatchClause().IsNil() {
		p.writeLineOrSpace(node, node.TryStatementTryBlock(), node.TryStatementCatchClause())
		p.emitCatchClause(node.TryStatementCatchClause())
	}
	if !node.TryStatementFinallyBlock().IsNil() {
		prev := node.TryStatementCatchClause()
		if prev.IsNil() {
			prev = node.TryStatementTryBlock()
		}
		p.writeLineOrSpace(node, prev, node.TryStatementFinallyBlock())
		p.emitToken(ast.KindFinallyKeyword, prev.End(), WriteKindKeyword, node)
		p.writeSpace()
		p.emitBlock(node.TryStatementFinallyBlock())
	}
	p.exitNode(node, state)
}
func (p *Printer) emitDebuggerStatement(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindDebuggerKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeTrailingSemicolon()
	p.exitNode(node, state)
}
func (p *Printer) emitNotEmittedStatement(node ast.Handle) {
	p.exitNode(node, p.enterNode(node))
}
func (p *Printer) emitNotEmittedTypeElement(node ast.Handle) {
	p.exitNode(node, p.enterNode(node))
}
func (p *Printer) emitVariableDeclaration(node ast.Handle) {
	state := p.enterNode(node)
	p.emitBindingName(node.Name())
	p.emitPunctuationNode(node.VariableDeclarationExclamationToken())
	p.emitTypeAnnotation(node.Type())
	p.emitInitializer(node.Initializer(), greatestEnd(node.Name().End(), node.Type(), p.emitContext.GetTypeNode(node.Name())), node)
	p.exitNode(node, state)
}
func (p *Printer) emitVariableDeclarationNode(node ast.Handle) {
	p.emitVariableDeclaration(node)
}
func (p *Printer) emitVariableDeclarationList(node ast.Handle) {
	state := p.enterNode(node)
	switch {
	case ast.IsVarLet(node):
		p.writeKeyword("let")
	case ast.IsVarConst(node):
		p.writeKeyword("const")
	case ast.IsVarUsing(node):
		p.writeKeyword("using")
	case ast.IsVarAwaitUsing(node):
		p.writeKeyword("await")
		p.writeSpace()
		p.writeKeyword("using")
	default:
		p.writeKeyword("var")
	}
	p.writeSpace()
	p.emitList((*Printer).emitVariableDeclarationNode, node, node.DeclarationList(), LFVariableDeclarationList)
	p.exitNode(node, state)
}
func (p *Printer) emitFunctionDeclaration(node ast.Handle) {
	state := p.enterNode(node)
	p.generateNameIfNeeded(node.Name())
	p.emitModifierList(node, node.Modifiers(), false)
	p.writeKeyword("function")
	p.emitTokenNode(node.AsteriskToken())
	p.writeSpace()
	if name := node.Name(); !name.IsNil() {
		p.emitIdentifierName(name)
	}
	indented := p.shouldEmitIndented(node)
	p.increaseIndentIf(indented)
	p.pushNameGenerationScope(node)
	p.emitSignature(node)
	p.emitFunctionBodyNode(node.Body())
	p.popNameGenerationScope(node)
	p.decreaseIndentIf(indented)
	p.exitNode(node, state)
}
func (p *Printer) emitClassDeclaration(node ast.Handle) {
	state := p.enterNode(node)
	p.generateNameIfNeeded(node.Name())
	pos := p.emitModifierList(node, node.Modifiers(), true)
	p.emitToken(ast.KindClassKeyword, pos, WriteKindKeyword, node)
	if !node.Name().IsNil() {
		p.writeSpace()
		p.emitIdentifierName(node.Name())
	}
	indented := p.shouldEmitIndented(node)
	p.increaseIndentIf(indented)
	p.emitTypeParameters(node, node.TypeParameterList())
	p.emitList((*Printer).emitHeritageClauseNode, node, node.HeritageClauses(), LFClassHeritageClauses)
	p.writeSpace()
	p.writePunctuation("{")
	p.pushNameGenerationScope(node)
	p.generateAllMemberNames(node, node.MemberList())
	p.emitList((*Printer).emitClassElement, node, node.MemberList(), LFClassMembers)
	p.popNameGenerationScope(node)
	p.writePunctuation("}")
	p.decreaseIndentIf(indented)
	p.exitNode(node, state)
}
func (p *Printer) emitInterfaceDeclaration(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), false)
	p.writeKeyword("interface")
	p.writeSpace()
	p.emitBindingIdentifier(node.Name())
	p.emitTypeParameters(node, node.TypeParameterList())
	p.emitList((*Printer).emitHeritageClauseNode, node, node.HeritageClauses(), LFHeritageClauses)
	p.writeSpace()
	p.writePunctuation("{")
	p.pushNameGenerationScope(node)
	p.generateAllMemberNames(node, node.MemberList())
	p.emitList((*Printer).emitTypeElement, node, node.MemberList(), LFInterfaceMembers)
	p.popNameGenerationScope(node)
	p.writePunctuation("}")
	p.exitNode(node, state)
}
func (p *Printer) emitTypeAliasDeclaration(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), false)
	p.writeKeyword("type")
	p.writeSpace()
	p.emitBindingIdentifier(node.Name())
	p.emitTypeParameters(node, node.TypeParameterList())
	p.writeSpace()
	p.writePunctuation("=")
	p.writeSpace()
	p.emitTypeNodeOutsideExtends(node.Type())
	p.writeTrailingSemicolon()
	p.exitNode(node, state)
}
func (p *Printer) emitEnumDeclaration(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), false)
	p.writeKeyword("enum")
	p.writeSpace()
	p.emitBindingIdentifier(node.Name())
	p.writeSpace()
	p.writePunctuation("{")
	p.emitList((*Printer).emitEnumMemberNode, node, node.MemberList(), LFEnumMembers)
	p.writePunctuation("}")
	p.exitNode(node, state)
}
func (p *Printer) emitModuleDeclaration(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), false)
	if node.ModuleDeclarationKeyword() != ast.KindGlobalKeyword {
		p.writeKeyword(core.IfElse(node.ModuleDeclarationKeyword() == ast.KindNamespaceKeyword, "namespace", "module"))
		p.writeSpace()
	}
	p.emitModuleName(node.Name())
	body := node.Body()
	for !body.IsNil() && ast.IsModuleDeclaration(body) {
		module := body
		p.writePunctuation(".")
		p.emitNestedModuleName(module.Name())
		body = module.Body()
	}
	if body.IsNil() {
		p.writeTrailingSemicolon()
	} else {
		p.writeSpace()
		p.emitModuleBlock(body)
	}
	p.exitNode(node, state)
}
func (p *Printer) emitModuleBlock(node ast.Handle) {
	state := p.enterNode(node)
	p.generateNames(node)
	p.emitToken(ast.KindOpenBraceToken, node.Pos(), WriteKindPunctuation, node)
	format := core.IfElse(p.isEmptyBlock(node, node.StatementList()) || p.shouldEmitOnSingleLine(node), LFSingleLineBlockStatements, LFMultiLineBlockStatements)
	p.emitList((*Printer).emitStatement, node, node.StatementList(), format)
	p.emitTokenEx(ast.KindCloseBraceToken, node.Store().ListLoc(node.StatementList()).End(), WriteKindPunctuation, node, core.IfElse(format&LFMultiLine != 0, tefIndentLeadingComments, tefNone))
	p.exitNode(node, state)
}
func (p *Printer) emitCaseBlock(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindOpenBraceToken, node.Pos(), WriteKindPunctuation, node)
	p.emitList((*Printer).emitCaseOrDefaultClauseNode, node, node.ClauseList(), LFCaseBlockClauses)
	p.emitTokenEx(ast.KindCloseBraceToken, node.Store().ListLoc(node.ClauseList()).End(), WriteKindPunctuation, node, tefIndentLeadingComments)
	p.exitNode(node, state)
}
func (p *Printer) emitImportEqualsDeclaration(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), false)
	pos := p.emitToken(ast.KindImportKeyword, greatestEnd(node.Pos(), node.Store().ListLoc(node.Modifiers())), WriteKindKeyword, node)
	p.writeSpace()
	if node.IsTypeOnly() {
		p.emitToken(ast.KindTypeKeyword, pos, WriteKindKeyword, node)
		p.writeSpace()
	}
	p.emitBindingIdentifier(node.Name())
	p.writeSpace()
	p.emitToken(ast.KindEqualsToken, node.Name().End(), WriteKindPunctuation, node)
	p.writeSpace()
	p.emitModuleReference(node.ImportEqualsDeclarationModuleReference())
	p.writeTrailingSemicolon()
	p.exitNode(node, state)
}
func (p *Printer) emitModuleReference(node ast.Handle) {
	switch node.Kind() {
	case ast.KindIdentifier:
		p.emitIdentifierReference(node)
	case ast.KindQualifiedName:
		p.emitQualifiedName(node)
	case ast.KindExternalModuleReference:
		p.emitExternalModuleReference(node)
	default:
		panic(fmt.Sprintf("unhandled ModuleReference: %v", node.Kind()))
	}
}
func (p *Printer) emitImportDeclaration(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), false)
	p.emitToken(ast.KindImportKeyword, greatestEnd(node.Pos(), node.Store().ListLoc(node.Modifiers())), WriteKindKeyword, node)
	p.writeSpace()
	if !node.ImportClause().IsNil() {
		p.emitImportClause(node.ImportClause())
		p.writeSpace()
		p.emitToken(ast.KindFromKeyword, node.ImportClause().End(), WriteKindKeyword, node)
		p.writeSpace()
	}
	p.emitExpression(node.ModuleSpecifier(), ast.OperatorPrecedenceLowest)
	if !node.ImportDeclarationAttributes().IsNil() {
		p.writeSpace()
		p.emitImportAttributes(node.ImportDeclarationAttributes())
	}
	p.writeTrailingSemicolon()
	p.exitNode(node, state)
}
func (p *Printer) emitImportClause(node ast.Handle) {
	state := p.enterNode(node)
	if node.ImportClausePhaseModifier() != ast.KindUnknown {
		p.emitToken(node.ImportClausePhaseModifier(), node.Pos(), WriteKindKeyword, node)
		p.writeSpace()
	}
	if name := node.Name(); !name.IsNil() {
		p.emitBindingIdentifier(node.Name())
		if !node.ImportClauseNamedBindings().IsNil() {
			p.emitToken(ast.KindCommaToken, name.End(), WriteKindPunctuation, node)
			p.writeSpace()
		}
	}
	p.emitNamedImportBindings(node.ImportClauseNamedBindings())
	p.exitNode(node, state)
}
func (p *Printer) emitNamespaceImport(node ast.Handle) {
	state := p.enterNode(node)
	pos := p.emitToken(ast.KindAsteriskToken, node.Pos(), WriteKindPunctuation, node)
	p.writeSpace()
	p.emitToken(ast.KindAsKeyword, pos, WriteKindKeyword, node)
	p.writeSpace()
	p.emitBindingIdentifier(node.Name())
	p.exitNode(node, state)
}
func (p *Printer) emitNamedImports(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("{")
	p.emitList((*Printer).emitImportSpecifierNode, node, node.ElementList(), LFNamedImportsOrExportsElements)
	p.writePunctuation("}")
	p.exitNode(node, state)
}
func (p *Printer) emitNamedImportBindings(node ast.Handle) {
	if node.IsNil() {
		return
	}
	switch node.Kind() {
	case ast.KindNamespaceImport:
		p.emitNamespaceImport(node)
	case ast.KindNamedImports:
		p.emitNamedImports(node)
	default:
		panic(fmt.Sprintf("unhandled NamedImportBindings: %v", node.Kind()))
	}
}
func (p *Printer) emitImportSpecifier(node ast.Handle) {
	state := p.enterNode(node)
	if node.IsTypeOnly() {
		p.writeKeyword("type")
		p.writeSpace()
	}
	if !node.PropertyName().IsNil() {
		p.emitModuleExportName(node.PropertyName())
		p.writeSpace()
		p.emitToken(ast.KindAsKeyword, node.PropertyName().End(), WriteKindKeyword, node)
		p.writeSpace()
	}
	p.emitBindingIdentifier(node.Name())
	p.exitNode(node, state)
}
func (p *Printer) emitImportSpecifierNode(node ast.Handle) {
	p.emitImportSpecifier(node)
}
func (p *Printer) emitExportAssignment(node ast.Handle) {
	state := p.enterNode(node)
	nextPos := p.emitToken(ast.KindExportKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	if node.ExportAssignmentIsExportEquals() {
		p.emitToken(ast.KindEqualsToken, nextPos, WriteKindOperator, node)
	} else {
		p.emitToken(ast.KindDefaultKeyword, nextPos, WriteKindKeyword, node)
	}
	p.writeSpace()
	if node.ExportAssignmentIsExportEquals() {
		p.emitExpression(node.Expression(), ast.OperatorPrecedenceAssignment)
	} else {
		expr := ast.GetLeftmostExpression(node.Expression(), false)
		if ast.IsClassExpression(expr) || ast.IsFunctionExpression(expr) {
			p.emitExpression(node.Expression(), ast.OperatorPrecedenceParentheses)
		} else {
			p.emitExpression(node.Expression(), ast.OperatorPrecedenceAssignment)
		}
	}
	p.writeTrailingSemicolon()
	p.exitNode(node, state)
}
func (p *Printer) emitExportDeclaration(node ast.Handle) {
	state := p.enterNode(node)
	p.emitModifierList(node, node.Modifiers(), false)
	pos := p.emitToken(ast.KindExportKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	if node.IsTypeOnly() {
		pos = p.emitToken(ast.KindTypeKeyword, pos, WriteKindKeyword, node)
		p.writeSpace()
	}
	if !node.ExportDeclarationExportClause().IsNil() {
		p.emitNamedExportBindings(node.ExportDeclarationExportClause())
	} else {
		pos = p.emitToken(ast.KindAsteriskToken, pos, WriteKindPunctuation, node)
	}
	if !node.ModuleSpecifier().IsNil() {
		p.writeSpace()
		p.emitToken(ast.KindFromKeyword, greatestEnd(pos, node.ExportDeclarationExportClause()), WriteKindKeyword, node)
		p.writeSpace()
		p.emitExpression(node.ModuleSpecifier(), ast.OperatorPrecedenceLowest)
	}
	if !node.ExportDeclarationAttributes().IsNil() {
		p.writeSpace()
		p.emitImportAttributes(node.ExportDeclarationAttributes())
	}
	p.writeTrailingSemicolon()
	p.exitNode(node, state)
}
func (p *Printer) emitImportAttributes(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(node.ImportAttributesToken(), node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	p.emitList((*Printer).emitImportAttributeNode, node, node.ImportAttributesAttributes(), LFImportAttributes)
	p.exitNode(node, state)
}
func (p *Printer) emitImportAttribute(node ast.Handle) {
	state := p.enterNode(node)
	p.emitImportAttributeName(node.Name())
	p.writePunctuation(":")
	p.writeSpace()
	value := node.ImportAttributeValue()
	if p.emitContext.EmitFlags(node.ImportAttributeValue())&EFNoLeadingComments == 0 {
		commentRange := p.emitContext.CommentRange(value)
		p.emitTrailingComments(commentRange.Pos(), commentSeparatorAfter)
	}
	p.emitExpression(value, ast.OperatorPrecedenceDisallowComma)
	p.exitNode(node, state)
}
func (p *Printer) emitImportAttributeNode(node ast.Handle) {
	p.emitImportAttribute(node)
}
func (p *Printer) emitNamespaceExportDeclaration(node ast.Handle) {
	state := p.enterNode(node)
	pos := p.emitToken(ast.KindExportKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	pos = p.emitToken(ast.KindAsKeyword, pos, WriteKindKeyword, node)
	p.writeSpace()
	p.emitToken(ast.KindNamespaceKeyword, pos, WriteKindKeyword, node)
	p.writeSpace()
	p.emitBindingIdentifier(node.Name())
	p.writeTrailingSemicolon()
	p.exitNode(node, state)
}
func (p *Printer) emitNamespaceExport(node ast.Handle) {
	state := p.enterNode(node)
	pos := p.emitToken(ast.KindAsteriskToken, node.Pos(), WriteKindPunctuation, node)
	p.writeSpace()
	p.emitToken(ast.KindAsKeyword, pos, WriteKindKeyword, node)
	p.writeSpace()
	p.emitModuleExportName(node.Name())
	p.exitNode(node, state)
}
func (p *Printer) emitNamedExports(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("{")
	p.emitList((*Printer).emitExportSpecifierNode, node, node.ElementList(), LFNamedImportsOrExportsElements)
	p.writePunctuation("}")
	p.exitNode(node, state)
}
func (p *Printer) emitNamedExportBindings(node ast.Handle) {
	switch node.Kind() {
	case ast.KindNamespaceExport:
		p.emitNamespaceExport(node)
	case ast.KindNamedExports:
		p.emitNamedExports(node)
	default:
		panic(fmt.Sprintf("unhandled NamedExportBindings: %v", node.Kind()))
	}
}
func (p *Printer) emitExportSpecifier(node ast.Handle) {
	state := p.enterNode(node)
	if node.IsTypeOnly() {
		p.writeKeyword("type")
		p.writeSpace()
	}
	if !node.PropertyName().IsNil() {
		p.emitModuleExportName(node.PropertyName())
		p.writeSpace()
		p.emitToken(ast.KindAsKeyword, node.PropertyName().End(), WriteKindKeyword, node)
		p.writeSpace()
	}
	p.emitModuleExportName(node.Name())
	p.exitNode(node, state)
}
func (p *Printer) emitExportSpecifierNode(node ast.Handle) {
	p.emitExportSpecifier(node)
}
func (p *Printer) emitEmbeddedStatement(parentNode ast.Handle, node ast.Handle) {
	if ast.IsBlock(node) || p.shouldEmitOnSingleLine(parentNode) || p.Options.PreserveSourceNewlines && p.getLeadingLineTerminatorCount(parentNode, node, LFNone) == 0 {
		p.writeSpace()
		p.emitStatement(node)
	} else {
		p.writeLine()
		p.increaseIndent()
		if node.Kind() == ast.KindEmptyStatement {
			p.emitEmptyStatement(node, true)
		} else {
			p.emitStatement(node)
		}
		p.decreaseIndent()
	}
}
func (p *Printer) emitStatement(node ast.Handle) {
	if snippetElement := p.emitContext.SnippetElement(node); snippetElement != nil {
		p.emitSnippetNode(node, snippetElement)
		return
	}
	switch node.Kind() {
	case ast.KindBlock:
		p.emitBlock(node)
	case ast.KindEmptyStatement:
		p.emitEmptyStatement(node, false)
	case ast.KindVariableStatement:
		p.emitVariableStatement(node)
	case ast.KindExpressionStatement:
		p.emitExpressionStatement(node)
	case ast.KindIfStatement:
		p.emitIfStatement(node)
	case ast.KindDoStatement:
		p.emitDoStatement(node)
	case ast.KindWhileStatement:
		p.emitWhileStatement(node)
	case ast.KindForStatement:
		p.emitForStatement(node)
	case ast.KindForInStatement:
		p.emitForInStatement(node)
	case ast.KindForOfStatement:
		p.emitForOfStatement(node)
	case ast.KindContinueStatement:
		p.emitContinueStatement(node)
	case ast.KindBreakStatement:
		p.emitBreakStatement(node)
	case ast.KindReturnStatement:
		p.emitReturnStatement(node)
	case ast.KindWithStatement:
		p.emitWithStatement(node)
	case ast.KindSwitchStatement:
		p.emitSwitchStatement(node)
	case ast.KindLabeledStatement:
		p.emitLabeledStatement(node)
	case ast.KindThrowStatement:
		p.emitThrowStatement(node)
	case ast.KindTryStatement:
		p.emitTryStatement(node)
	case ast.KindDebuggerStatement:
		p.emitDebuggerStatement(node)
	case ast.KindNotEmittedStatement:
		p.emitNotEmittedStatement(node)
	case ast.KindFunctionDeclaration:
		p.emitFunctionDeclaration(node)
	case ast.KindClassDeclaration:
		p.emitClassDeclaration(node)
	case ast.KindInterfaceDeclaration:
		p.emitInterfaceDeclaration(node)
	case ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration:
		p.emitTypeAliasDeclaration(node)
	case ast.KindEnumDeclaration:
		p.emitEnumDeclaration(node)
	case ast.KindModuleDeclaration:
		p.emitModuleDeclaration(node)
	case ast.KindMissingDeclaration:
		break
	case ast.KindNamespaceExportDeclaration:
		p.emitNamespaceExportDeclaration(node)
	case ast.KindImportEqualsDeclaration:
		p.emitImportEqualsDeclaration(node)
	case ast.KindImportDeclaration:
		p.emitImportDeclaration(node)
	case ast.KindExportAssignment:
		p.emitExportAssignment(node)
	case ast.KindExportDeclaration:
		p.emitExportDeclaration(node)
	default:
		panic(fmt.Sprintf("unhandled statement: %v", node.Kind()))
	}
}
func (p *Printer) emitExternalModuleReference(node ast.Handle) {
	state := p.enterNode(node)
	p.writeKeyword("require")
	p.writePunctuation("(")
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceDisallowComma)
	p.writePunctuation(")")
	p.exitNode(node, state)
}
func (p *Printer) emitJsxElement(node ast.Handle) {
	state := p.enterNode(node)
	p.emitJsxOpeningElement(node.JsxElementOpeningElement())
	p.emitList((*Printer).emitJsxChild, node, node.ChildList(), LFJsxElementOrFragmentChildren)
	p.emitJsxClosingElement(node.JsxElementClosingElement())
	p.exitNode(node, state)
}
func (p *Printer) emitJsxSelfClosingElement(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("<")
	p.emitJsxTagName(node.TagName())
	p.emitTypeArguments(node, node.TypeArgumentList())
	p.writeSpace()
	p.emitJsxAttributes(node.JsxSelfClosingElementAttributes())
	p.writePunctuation("/>")
	p.exitNode(node, state)
}
func (p *Printer) emitJsxFragment(node ast.Handle) {
	state := p.enterNode(node)
	p.emitJsxOpeningFragment(node.JsxFragmentOpeningFragment())
	p.emitList((*Printer).emitJsxChild, node, node.ChildList(), LFJsxElementOrFragmentChildren)
	p.emitJsxClosingFragment(node.JsxFragmentClosingFragment())
	p.exitNode(node, state)
}
func (p *Printer) emitJsxOpeningElement(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("<")
	indented := p.writeLineSeparatorsAndIndentBefore(node.TagName(), node)
	p.emitJsxTagName(node.TagName())
	p.emitTypeArguments(node, node.TypeArgumentList())
	if len(node.JsxOpeningElementAttributes().Properties()) > 0 {
		p.writeSpace()
	}
	p.emitJsxAttributes(node.JsxOpeningElementAttributes())
	p.writeLineSeparatorsAfter(node.JsxOpeningElementAttributes(), node)
	p.decreaseIndentIf(indented)
	p.writePunctuation(">")
	p.exitNode(node, state)
}
func (p *Printer) emitJsxClosingElement(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("</")
	p.emitJsxTagName(node.TagName())
	p.writePunctuation(">")
	p.exitNode(node, state)
}
func (p *Printer) emitJsxOpeningFragment(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("<")
	p.writePunctuation(">")
	p.exitNode(node, state)
}
func (p *Printer) emitJsxClosingFragment(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("</")
	p.writePunctuation(">")
	p.exitNode(node, state)
}
func (p *Printer) emitJsxText(node ast.Handle) {
	state := p.enterNode(node)
	p.writeLiteral(node.Text())
	p.exitNode(node, state)
}
func (p *Printer) emitJsxAttributes(node ast.Handle) {
	state := p.enterNode(node)
	p.emitList((*Printer).emitJsxAttributeLike, node, node.PropertyList(), LFJsxElementAttributes)
	p.exitNode(node, state)
}
func (p *Printer) emitJsxAttribute(node ast.Handle) {
	state := p.enterNode(node)
	p.emitJsxAttributeName(node.Name())
	if !node.Initializer().IsNil() {
		p.writePunctuation("=")
		p.emitJsxAttributeValue(node.Initializer())
	}
	p.exitNode(node, state)
}
func (p *Printer) emitJsxSpreadAttribute(node ast.Handle) {
	state := p.enterNode(node)
	p.writePunctuation("{...")
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceLowest)
	p.writePunctuation("}")
	p.exitNode(node, state)
}
func (p *Printer) emitJsxAttributeLike(node ast.Handle) {
	switch node.Kind() {
	case ast.KindJsxAttribute:
		p.emitJsxAttribute(node)
	case ast.KindJsxSpreadAttribute:
		p.emitJsxSpreadAttribute(node)
	default:
		panic(fmt.Sprintf("unhandled JsxAttributeLike: %v", node.Kind()))
	}
}
func (p *Printer) emitJsxExpression(node ast.Handle) {
	state := p.enterNode(node)
	if !node.Expression().IsNil() || !p.commentsDisabled && !ast.NodeIsSynthesized(node) && p.hasCommentsAtPosition(node.Pos()) {
		indented := p.currentSourceFile != nil && !ast.NodeIsSynthesized(node) && GetLinesBetweenPositions(p.currentSourceFile, node.Pos(), node.End()) != 0
		p.increaseIndentIf(indented)
		end := p.emitToken(ast.KindOpenBraceToken, node.Pos(), WriteKindPunctuation, node)
		p.emitTokenNode(node.DotDotDotToken())
		if !node.Expression().IsNil() {
			p.emitExpression(node.Expression(), ast.OperatorPrecedenceDisallowComma)
		}
		p.emitToken(ast.KindCloseBraceToken, greatestEnd(end, node.Expression(), node.DotDotDotToken()), WriteKindPunctuation, node)
		p.decreaseIndentIf(indented)
	}
	p.exitNode(node, state)
}
func (p *Printer) emitJsxNamespacedName(node ast.Handle) {
	state := p.enterNode(node)
	p.emitIdentifierName(node.JsxNamespacedNameNamespace())
	p.writePunctuation(":")
	p.emitIdentifierName(node.Name())
	p.exitNode(node, state)
}
func (p *Printer) emitJsxChild(node ast.Handle) {
	switch node.Kind() {
	case ast.KindJsxText:
		p.emitJsxText(node)
	case ast.KindJsxExpression:
		p.emitJsxExpression(node)
	case ast.KindJsxElement:
		p.emitJsxElement(node)
	case ast.KindJsxSelfClosingElement:
		p.emitJsxSelfClosingElement(node)
	case ast.KindJsxFragment:
		p.emitJsxFragment(node)
	default:
		panic(fmt.Sprintf("unhandled JsxChild: %v", node.Kind()))
	}
}
func (p *Printer) emitJsxTagName(node ast.Handle) {
	switch node.Kind() {
	case ast.KindIdentifier:
		p.emitIdentifierReference(node)
	case ast.KindThisKeyword:
		p.emitKeywordExpression(node)
	case ast.KindJsxNamespacedName:
		p.emitJsxNamespacedName(node)
	case ast.KindPropertyAccessExpression:
		p.emitPropertyAccessExpression(node)
	default:
		panic(fmt.Sprintf("unhandled JsxTagName: %v", node.Kind()))
	}
}
func (p *Printer) emitJsxAttributeName(node ast.Handle) {
	switch node.Kind() {
	case ast.KindIdentifier:
		p.emitIdentifierName(node)
	case ast.KindJsxNamespacedName:
		p.emitJsxNamespacedName(node)
	default:
		panic(fmt.Sprintf("unhandled JsxAttributeName: %v", node.Kind()))
	}
}
func (p *Printer) emitJsxAttributeValue(node ast.Handle) {
	switch node.Kind() {
	case ast.KindStringLiteral:
		p.emitStringLiteral(node)
	case ast.KindJsxExpression:
		p.emitJsxExpression(node)
	case ast.KindJsxElement:
		p.emitJsxElement(node)
	case ast.KindJsxSelfClosingElement:
		p.emitJsxSelfClosingElement(node)
	case ast.KindJsxFragment:
		p.emitJsxFragment(node)
	default:
		p.emitExpression(node, ast.OperatorPrecedenceLowest)
	}
}
func (p *Printer) emitCaseOrDefaultClauseStatements(node ast.Handle, colonPos int) {
	emitAsSingleStatement := node.Store().ListLen(node.StatementList()) == 1 && (p.currentSourceFile == nil || ast.NodeIsSynthesized(node) || ast.NodeIsSynthesized(node.Store().ListAt(node.StatementList(), 0)) || RangeStartPositionsAreOnSameLine(node.Loc(), node.Store().ListAt(node.StatementList(), 0).Loc(), p.currentSourceFile))
	format := LFCaseOrDefaultClauseStatements
	if emitAsSingleStatement {
		p.writeTokenText(ast.KindColonToken, WriteKindPunctuation, colonPos)
		p.writeSpace()
		format &^= LFMultiLine | LFIndented
	} else {
		p.emitToken(ast.KindColonToken, colonPos, WriteKindPunctuation, node)
	}
	p.emitList((*Printer).emitStatement, node, node.StatementList(), format)
}
func (p *Printer) emitCaseClause(node ast.Handle) {
	state := p.enterNode(node)
	p.emitToken(ast.KindCaseKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	p.emitExpression(node.Expression(), ast.OperatorPrecedenceLowest)
	p.emitCaseOrDefaultClauseStatements(node, node.Expression().End())
	p.exitNode(node, state)
}
func (p *Printer) emitDefaultClause(node ast.Handle) {
	state := p.enterNode(node)
	pos := p.emitToken(ast.KindDefaultKeyword, node.Pos(), WriteKindKeyword, node)
	p.emitCaseOrDefaultClauseStatements(node, pos)
	p.exitNode(node, state)
}
func (p *Printer) emitCaseOrDefaultClauseNode(node ast.Handle) {
	switch node.Kind() {
	case ast.KindCaseClause:
		p.emitCaseClause(node)
	case ast.KindDefaultClause:
		p.emitDefaultClause(node)
	default:
		panic(fmt.Sprintf("unhandled CaseOrDefaultClause: %v", node.Kind()))
	}
}
func (p *Printer) emitHeritageClause(node ast.Handle) {
	state := p.enterNode(node)
	p.writeSpace()
	p.emitToken(node.HeritageClauseToken(), node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	p.emitList((*Printer).emitHeritageClauseElement, node, node.TypeList(), LFHeritageClauseTypes)
	p.exitNode(node, state)
}
func (p *Printer) emitHeritageClauseElement(node ast.Handle) {
	switch node.Kind() {
	case ast.KindExpressionWithTypeArguments:
		p.emitExpressionWithTypeArguments(node)
	case ast.KindTypeReference:
		p.emitTypeReference(node)
	default:
		panic(fmt.Sprintf("unhandled HeritageClauseElement: %v", node.Kind()))
	}
}
func (p *Printer) emitHeritageClauseNode(node ast.Handle) {
	p.emitHeritageClause(node)
}
func (p *Printer) emitCatchClause(node ast.Handle) {
	state := p.enterNode(node)
	openParenPos := p.emitToken(ast.KindCatchKeyword, node.Pos(), WriteKindKeyword, node)
	p.writeSpace()
	if !node.CatchClauseVariableDeclaration().IsNil() {
		p.emitToken(ast.KindOpenParenToken, openParenPos, WriteKindPunctuation, node)
		p.emitVariableDeclaration(node.CatchClauseVariableDeclaration())
		p.emitToken(ast.KindCloseParenToken, node.CatchClauseVariableDeclaration().End(), WriteKindPunctuation, node)
		p.writeSpace()
	}
	p.emitBlock(node.CatchClauseBlock())
	p.exitNode(node, state)
}
func (p *Printer) emitPropertyAssignment(node ast.Handle) {
	state := p.enterNode(node)
	p.emitPropertyName(node.Name())
	p.writePunctuation(":")
	p.writeSpace()
	initializer := node.Initializer()
	if p.emitContext.EmitFlags(initializer)&EFNoLeadingComments == 0 {
		commentRange := p.emitContext.CommentRange(initializer)
		p.emitTrailingComments(commentRange.Pos(), commentSeparatorAfter)
	}
	p.emitExpression(initializer, ast.OperatorPrecedenceDisallowComma)
	p.exitNode(node, state)
}
func (p *Printer) emitShorthandPropertyAssignment(node ast.Handle) {
	state := p.enterNode(node)
	p.emitPropertyName(node.Name())
	if !node.ShorthandPropertyAssignmentObjectAssignmentInitializer().IsNil() {
		p.writeSpace()
		p.writePunctuation("=")
		p.writeSpace()
		p.emitExpression(node.ShorthandPropertyAssignmentObjectAssignmentInitializer(), ast.OperatorPrecedenceDisallowComma)
	}
	p.exitNode(node, state)
}
func (p *Printer) emitSpreadAssignment(node ast.Handle) {
	state := p.enterNode(node)
	if !node.Expression().IsNil() {
		p.emitToken(ast.KindDotDotDotToken, node.Pos(), WriteKindPunctuation, node)
		p.emitExpression(node.Expression(), ast.OperatorPrecedenceDisallowComma)
	}
	p.exitNode(node, state)
}
func (p *Printer) emitEnumMember(node ast.Handle) {
	state := p.enterNode(node)
	p.emitPropertyName(node.Name())
	p.emitInitializer(node.Initializer(), node.Name().End(), node)
	p.exitNode(node, state)
}
func (p *Printer) emitEnumMemberNode(node ast.Handle) {
	p.emitEnumMember(node)
}
func (p *Printer) emitJSDocNode(node ast.Handle) {
	panic("not implemented")
}
func (p *Printer) emitShebangIfNeeded(node *ast.SourceFile) {
	if ast.NodeIsSynthesized(node.ParseRoot()) {
		return
	}
	shebang := scanner.GetShebang(node.Text())
	if shebang != "" {
		p.writeComment(shebang)
		p.writeLine()
	}
}
func (p *Printer) emitPrologueDirectives(statements ast.ListRef) int {
	for i, statement := range p.currentSourceFile.ParseStore().ListSlice(statements) {
		if ast.IsPrologueDirective(statement) {
			p.writeLine()
			p.emitStatement(statement)
		} else {
			return i
		}
	}
	return p.currentSourceFile.ParseStore().ListLen(statements)
}
func (p *Printer) emitHelpers(node ast.Handle) bool {
	helpersEmitted := false
	sourceFile := p.currentSourceFile
	shouldSkip := p.Options.NoEmitHelpers || (sourceFile != nil && p.emitContext.HasRecordedExternalHelpers(sourceFile))
	helpers := slices.Clone(p.emitContext.GetEmitHelpers(node))
	if len(helpers) > 0 {
		slices.SortStableFunc(helpers, compareEmitHelpers)
		for _, helper := range helpers {
			if !helper.Scoped {
				if shouldSkip {
					continue
				}
			}
			if helper.TextCallback != nil {
				p.writeLines(helper.TextCallback(p.makeFileLevelOptimisticUniqueName))
			} else {
				p.writeLines(helper.Text)
			}
			helpersEmitted = true
		}
	}
	return helpersEmitted
}
func (p *Printer) emitSourceFile(node *ast.SourceFile) {
	savedCurrentSourceFile := p.currentSourceFile
	savedCommentsDisabled := p.commentsDisabled
	p.currentSourceFile = node
	p.writeLine()
	p.pushNameGenerationScope(node.ParseRoot())
	p.generateAllNames(node.ParseRoot(), node.ParseRoot().StatementList())
	index := 0
	var state *commentState
	if node.ScriptKind != core.ScriptKindJSON {
		p.emitShebangIfNeeded(node)
		index = p.emitPrologueDirectives(node.ParseRoot().StatementList())
		if !p.writer.IsAtStartOfLine() {
			p.writeLine()
		}
		state = p.emitDetachedCommentsBeforeStatementList(node.ParseRoot(), node.ParseStore().ListLoc(node.ParseRoot().StatementList()))
		p.emitHelpers(node.ParseRoot())
		if node.IsDeclarationFile {
			p.emitTripleSlashDirectives(node)
		}
	} else {
		state = p.emitDetachedCommentsBeforeStatementList(node.ParseRoot(), node.ParseStore().ListLoc(node.ParseRoot().StatementList()))
	}
	p.emitListRange((*Printer).emitStatement, node.ParseRoot(), node.ParseRoot().StatementList(), LFMultiLine, index, -1)
	p.popNameGenerationScope(node.ParseRoot())
	p.emitDetachedCommentsAfterStatementList(node.ParseRoot(), node.ParseStore().ListLoc(node.ParseRoot().StatementList()), state)
	p.currentSourceFile = savedCurrentSourceFile
	p.commentsDisabled = savedCommentsDisabled
}
func (p *Printer) emitTripleSlashDirectives(node *ast.SourceFile) {
	p.emitDirective("path", node.ReferencedFiles)
	p.emitDirective("types", node.TypeReferenceDirectives)
	p.emitDirective("lib", node.LibReferenceDirectives)
}
func (p *Printer) emitDirective(kind string, refs []*ast.FileReference) {
	for _, ref := range refs {
		var resolutionMode string
		if ref.ResolutionMode != core.ResolutionModeNone {
			resolutionMode = fmt.Sprintf(`resolution-mode="%s" `, core.IfElse(ref.ResolutionMode == core.ResolutionModeESM, "import", "require"))
		}
		p.writeComment(fmt.Sprintf("/// <reference %s=\"%s\" %s%s/>", kind, ref.FileName, resolutionMode, core.IfElse(ref.Preserve, `preserve="true" `, "")))
		p.writeLine()
	}
}
func (p *Printer) emitList(emit func(p *Printer, node ast.Handle), parentNode ast.Handle, children ast.ListRef, format ListFormat) {
	if p.shouldEmitOnMultipleLines(parentNode) {
		format |= LFPreferNewLine | LFIndented
	}
	p.emitListRange(emit, parentNode, children, format, -1, -1)
}
func (p *Printer) emitListRange(emit func(p *Printer, node ast.Handle), parentNode ast.Handle, children ast.ListRef, format ListFormat, start int, count int) {
	isNil := children == 0
	length := 0
	if !isNil {
		length = parentNode.Store().ListLen(children)
	}
	if start < 0 {
		start = 0
	}
	if count < 0 {
		count = length - start
	}
	if isNil && format&LFOptionalIfNil != 0 {
		return
	}
	isEmpty := isNil || start >= length || count <= 0
	if isEmpty && format&LFOptionalIfEmpty != 0 {
		if p.OnBeforeEmitNodeList != nil {
			p.OnBeforeEmitNodeList(children)
		}
		if p.OnAfterEmitNodeList != nil {
			p.OnAfterEmitNodeList(children)
		}
		return
	}
	if format&LFBracketsMask != 0 {
		p.writePunctuation(getOpeningBracket(format))
		if isEmpty && !isNil {
			p.emitTrailingComments(parentNode.Store().ListLoc(children).Pos(), commentSeparatorBefore)
		}
	}
	if p.OnBeforeEmitNodeList != nil {
		p.OnBeforeEmitNodeList(children)
	}
	if isEmpty {
		if format&LFMultiLine != 0 && !(p.Options.PreserveSourceNewlines && (parentNode.IsNil() || p.currentSourceFile != nil && RangeIsOnSingleLine(parentNode.Loc(), p.currentSourceFile))) {
			p.writeLine()
		} else if format&LFSpaceBetweenBraces != 0 && format&LFNoSpaceIfEmpty == 0 {
			p.writeSpace()
		}
	} else {
		end := min(start+count, length)
		p.emitListItems(emit, parentNode, parentNode.Store().ListSlice(children)[start:end], format, p.hasTrailingComma(parentNode, children), parentNode.Store().ListLoc(children))
	}
	if p.OnAfterEmitNodeList != nil {
		p.OnAfterEmitNodeList(children)
	}
	if format&LFBracketsMask != 0 {
		if isEmpty && !isNil {
			p.emitLeadingComments(parentNode.Store().ListLoc(children).End(), false)
		}
		p.writePunctuation(getClosingBracket(format))
	}
}
func (p *Printer) hasTrailingComma(parentNode ast.Handle, children ast.ListRef) bool {
	if !parentNode.Store().ListHasTrailingComma(children) {
		return false
	}
	originalParent := p.emitContext.MostOriginal(parentNode)
	if originalParent == parentNode {
		return true
	}
	if originalParent.Kind() != parentNode.Kind() {
		return false
	}
	originalList := children
	switch originalParent.Kind() {
	case ast.KindObjectLiteralExpression:
		originalList = originalParent.PropertyList()
	case ast.KindArrayLiteralExpression:
		originalList = originalParent.ElementList()
	case ast.KindCallExpression, ast.KindNewExpression:
		switch children {
		case parentNode.TypeArgumentList():
			originalList = originalParent.TypeArgumentList()
		case parentNode.ArgumentList():
			originalList = originalParent.ArgumentList()
		}
	case ast.KindConstructor, ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindFunctionType, ast.KindConstructorType, ast.KindCallSignature, ast.KindConstructSignature:
		switch children {
		case parentNode.TypeParameterList():
			originalList = originalParent.TypeParameterList()
		case parentNode.ParameterList():
			originalList = originalParent.ParameterList()
		}
	case ast.KindClassDeclaration, ast.KindClassExpression, ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration:
		switch children {
		case parentNode.TypeParameterList():
			originalList = originalParent.TypeParameterList()
		}
	case ast.KindObjectBindingPattern, ast.KindArrayBindingPattern:
		switch children {
		case parentNode.ElementList():
			originalList = originalParent.ElementList()
		}
	case ast.KindNamedImports, ast.KindNamedExports:
		originalList = originalParent.ElementList()
	case ast.KindImportAttributes:
		originalList = originalParent.ImportAttributesAttributes()
	}
	if originalList != 0 {
		return parentNode.Store().ListHasTrailingComma(originalList)
	}
	return false
}
func (p *Printer) writeDelimiter(format ListFormat) {
	switch format & LFDelimitersMask {
	case LFNone:
		break
	case LFCommaDelimited:
		p.writePunctuation(",")
	case LFBarDelimited:
		p.writeSpace()
		p.writePunctuation("|")
	case LFAsteriskDelimited:
		p.writeSpace()
		p.writePunctuation("*")
		p.writeSpace()
	case LFAmpersandDelimited:
		p.writeSpace()
		p.writePunctuation("&")
	}
}

func (p *Printer) emitListItems(emit func(p *Printer, node ast.Handle), parentNode ast.Handle, children []ast.Handle, format ListFormat, hasTrailingComma bool, childrenTextRange core.TextRange) {
	mayEmitInterveningComments := format&LFNoInterveningComments == 0
	shouldEmitInterveningComments := mayEmitInterveningComments
	leadingLineTerminatorCount := 0
	if len(children) > 0 {
		leadingLineTerminatorCount = p.getLeadingLineTerminatorCount(parentNode, children[0], format)
	}
	if leadingLineTerminatorCount > 0 {
		for range leadingLineTerminatorCount {
			p.writeLine()
		}
		shouldEmitInterveningComments = false
	} else if format&LFSpaceBetweenBraces != 0 {
		p.writeSpace()
	}
	if format&LFIndented != 0 {
		p.increaseIndent()
	}
	parentEnd := greatestEnd(-1, parentNode)
	var previousSibling ast.Handle
	shouldDecreaseIndentAfterEmit := false
	for _, child := range children {
		if format&LFAsteriskDelimited != 0 {
			p.writeLine()
			p.writeDelimiter(format)
		} else if !previousSibling.IsNil() {
			if format&LFDelimitersMask != 0 && previousSibling.End() != parentEnd {
				if !p.commentsDisabled && p.shouldEmitTrailingComments(previousSibling) {
					p.emitLeadingComments(previousSibling.End(), false)
				}
			}
			p.writeDelimiter(format)
			separatingLineTerminatorCount := p.getSeparatingLineTerminatorCount(previousSibling, child, format)
			if separatingLineTerminatorCount > 0 {
				if format&(LFLinesMask|LFIndented) == LFSingleLine {
					p.increaseIndent()
					shouldDecreaseIndentAfterEmit = true
				}
				if shouldEmitInterveningComments && format&LFDelimitersMask != 0 && !ast.PositionIsSynthesized(child.Pos()) && p.shouldEmitLeadingComments(child) {
					commentRange := p.emitContext.CommentRange(child)
					p.emitTrailingCommentsOfPosition(commentRange.Pos(), format&LFSpaceBetweenSiblings != 0, true)
				}
				for range separatingLineTerminatorCount {
					p.writeLine()
				}
				shouldEmitInterveningComments = false
			} else if format&LFSpaceBetweenSiblings != 0 {
				p.writeSpace()
			}
		}
		if shouldEmitInterveningComments && p.shouldEmitLeadingComments(child) {
			commentRange := p.emitContext.CommentRange(child)
			p.emitTrailingCommentsOfPosition(commentRange.Pos(), false, false)
		} else {
			shouldEmitInterveningComments = mayEmitInterveningComments
		}
		p.nextListElementPos = child.Pos()
		emit(p, child)
		if shouldDecreaseIndentAfterEmit {
			p.decreaseIndent()
			shouldDecreaseIndentAfterEmit = false
		}
		previousSibling = child
	}
	skipTrailingComments := p.commentsDisabled || !p.shouldEmitTrailingComments(previousSibling)
	emitTrailingComma := hasTrailingComma && format&LFAllowTrailingComma != 0 && format&LFCommaDelimited != 0
	if emitTrailingComma {
		if !previousSibling.IsNil() && !skipTrailingComments {
			p.emitToken(ast.KindCommaToken, previousSibling.End(), WriteKindPunctuation, previousSibling)
		} else {
			p.writePunctuation(",")
		}
	}
	if !previousSibling.IsNil() && parentEnd != previousSibling.End() && format&LFDelimitersMask != 0 && !skipTrailingComments {
		var commentsPos int
		if emitTrailingComma && childrenTextRange.End() > 0 {
			commentsPos = childrenTextRange.End()
		} else {
			commentsPos = previousSibling.End()
		}
		p.emitLeadingComments(commentsPos, false)
	}
	if format&LFIndented != 0 {
		p.decreaseIndent()
	}
	closingLineTerminatorCount := p.getClosingLineTerminatorCount(parentNode, core.LastOrNil(children), format, childrenTextRange)
	if closingLineTerminatorCount > 0 {
		for range closingLineTerminatorCount {
			p.writeLine()
		}
	} else if format&(LFSpaceAfterList|LFSpaceBetweenBraces) != 0 {
		p.writeSpace()
	}
}
func (p *Printer) Emit(node ast.Handle, sourceFile *ast.SourceFile) string {
	if p.ownWriter == nil {
		p.ownWriter = NewTextWriter(p.Options.NewLine.GetNewLineCharacter(), 0)
	}
	p.Write(node, sourceFile, p.ownWriter, nil)
	text := p.ownWriter.String()
	p.ownWriter.Clear()
	return text
}
func (p *Printer) EmitSourceFile(sourceFile *ast.SourceFile) string {
	return p.Emit(sourceFile.ParseRoot(), sourceFile)
}
func (p *Printer) setSourceFile(sourceFile *ast.SourceFile) {
	p.currentSourceFile = sourceFile
	p.uniqueHelperNames = nil
	p.externalHelpersModuleName = ast.Handle{}
	if sourceFile != nil {
		if p.emitContext.EmitFlags(p.emitContext.MostOriginal(sourceFile.ParseRoot()))&EFExternalHelpers != 0 {
			p.uniqueHelperNames = make(map[string]ast.Handle)
		}
		p.externalHelpersModuleName = p.emitContext.GetExternalHelpersModuleName(sourceFile)
		p.setSourceMapSource(sourceFile)
	}
}
func (p *Printer) Write(node ast.Handle, sourceFile *ast.SourceFile, writer EmitTextWriter, sourceMapGenerator *sourcemap.Generator) {
	savedCurrentSourceFile := p.currentSourceFile
	savedWriter := p.writer
	savedUniqueHelperNames := p.uniqueHelperNames
	savedSourceMapsDisabled := p.sourceMapsDisabled
	savedSourceMapGenerator := p.sourceMapGenerator
	savedSourceMapSource := p.sourceMapSource
	savedSourceMapSourceIndex := p.sourceMapSourceIndex
	savedSourceMapLineCharCache := p.sourceMapLineCharCache
	p.sourceMapsDisabled = sourceMapGenerator == nil
	p.sourceMapGenerator = sourceMapGenerator
	p.sourceMapSource = nil
	p.sourceMapSourceIndex = -1
	p.sourceMapLineCharCache = nil
	p.setSourceFile(sourceFile)
	if p.Options.OmitTrailingSemicolon {
		writer = getTrailingSemicolonDeferringWriter(writer)
	}
	p.writer = writer
	p.writer.Clear()
	if sourceFile != nil {
		if grower, ok := p.writer.(interface{ Grow(n int) }); ok {
			grower.Grow(len(sourceFile.Text()))
		}
	}
	switch node.Kind() {
	case ast.KindTemplateHead:
		p.emitTemplateHead(node)
	case ast.KindTemplateMiddle:
		p.emitTemplateMiddle(node)
	case ast.KindTemplateTail:
		p.emitTemplateTail(node)
	case ast.KindIdentifier:
		p.emitIdentifierName(node)
	case ast.KindPrivateIdentifier:
		p.emitPrivateIdentifier(node)
	case ast.KindQualifiedName:
		p.emitQualifiedName(node)
	case ast.KindComputedPropertyName:
		p.emitComputedPropertyName(node)
	case ast.KindTypeParameter:
		p.emitTypeParameter(node)
	case ast.KindParameter:
		p.emitParameter(node)
	case ast.KindDecorator:
		p.emitDecorator(node)
	case ast.KindPropertySignature:
		p.emitPropertySignature(node)
	case ast.KindPropertyDeclaration:
		p.emitPropertyDeclaration(node)
	case ast.KindMethodSignature:
		p.emitMethodSignature(node)
	case ast.KindMethodDeclaration:
		p.emitMethodDeclaration(node)
	case ast.KindClassStaticBlockDeclaration:
		p.emitClassStaticBlockDeclaration(node)
	case ast.KindConstructor:
		p.emitConstructor(node)
	case ast.KindGetAccessor:
		p.emitGetAccessorDeclaration(node)
	case ast.KindSetAccessor:
		p.emitSetAccessorDeclaration(node)
	case ast.KindCallSignature:
		p.emitCallSignature(node)
	case ast.KindConstructSignature:
		p.emitConstructSignature(node)
	case ast.KindIndexSignature:
		p.emitIndexSignature(node)
	case ast.KindObjectBindingPattern:
		p.emitObjectBindingPattern(node)
	case ast.KindArrayBindingPattern:
		p.emitArrayBindingPattern(node)
	case ast.KindBindingElement:
		p.emitBindingElement(node)
	case ast.KindTemplateSpan:
		p.emitTemplateSpan(node)
	case ast.KindSemicolonClassElement:
		p.emitSemicolonClassElement(node)
	case ast.KindVariableDeclaration:
		p.emitVariableDeclaration(node)
	case ast.KindVariableDeclarationList:
		p.emitVariableDeclarationList(node)
	case ast.KindModuleBlock:
		p.emitModuleBlock(node)
	case ast.KindCaseBlock:
		p.emitCaseBlock(node)
	case ast.KindImportClause:
		p.emitImportClause(node)
	case ast.KindNamespaceImport:
		p.emitNamespaceImport(node)
	case ast.KindNamespaceExport:
		p.emitNamespaceExport(node)
	case ast.KindNamedImports:
		p.emitNamedImports(node)
	case ast.KindImportSpecifier:
		p.emitImportSpecifier(node)
	case ast.KindNamedExports:
		p.emitNamedExports(node)
	case ast.KindExportSpecifier:
		p.emitExportSpecifier(node)
	case ast.KindImportAttributes:
		p.emitImportAttributes(node)
	case ast.KindImportAttribute:
		p.emitImportAttribute(node)
	case ast.KindExternalModuleReference:
		p.emitExternalModuleReference(node)
	case ast.KindJsxText:
		p.emitJsxText(node)
	case ast.KindJsxOpeningElement:
		p.emitJsxOpeningElement(node)
	case ast.KindJsxOpeningFragment:
		p.emitJsxOpeningFragment(node)
	case ast.KindJsxClosingElement:
		p.emitJsxClosingElement(node)
	case ast.KindJsxClosingFragment:
		p.emitJsxClosingFragment(node)
	case ast.KindJsxAttribute:
		p.emitJsxAttribute(node)
	case ast.KindJsxAttributes:
		p.emitJsxAttributes(node)
	case ast.KindJsxSpreadAttribute:
		p.emitJsxSpreadAttribute(node)
	case ast.KindJsxExpression:
		p.emitJsxExpression(node)
	case ast.KindJsxNamespacedName:
		p.emitJsxNamespacedName(node)
	case ast.KindCaseClause:
		p.emitCaseClause(node)
	case ast.KindDefaultClause:
		p.emitDefaultClause(node)
	case ast.KindHeritageClause:
		p.emitHeritageClause(node)
	case ast.KindCatchClause:
		p.emitCatchClause(node)
	case ast.KindPropertyAssignment:
		p.emitPropertyAssignment(node)
	case ast.KindShorthandPropertyAssignment:
		p.emitShorthandPropertyAssignment(node)
	case ast.KindSpreadAssignment:
		p.emitSpreadAssignment(node)
	case ast.KindEnumMember:
		p.emitEnumMember(node)
	case ast.KindSourceFile:
		p.emitSourceFile(ast.GetSourceFileOfNode(node))
	case ast.KindNotEmittedTypeElement:
		p.emitNotEmittedTypeElement(node)
	default:
		switch {
		case ast.IsTypeNode(node):
			p.emitTypeNodeOutsideExtends(node)
		case ast.IsStatement(node):
			p.emitStatement(node)
		case ast.IsExpression(node):
			p.emitExpression(node, ast.OperatorPrecedenceLowest)
		case ast.IsKeywordKind(node.Kind()):
			p.emitKeywordNode(node)
		case ast.IsPunctuationKind(node.Kind()):
			p.emitPunctuationNode(node)
		case ast.IsJSDocKind(node.Kind()):
			p.emitJSDocNode(node)
		default:
			panic(fmt.Sprintf("unhandled Node: %v", node.Kind()))
		}
	}
	p.currentSourceFile = savedCurrentSourceFile
	p.writer = savedWriter
	p.uniqueHelperNames = savedUniqueHelperNames
	p.sourceMapsDisabled = savedSourceMapsDisabled
	p.sourceMapGenerator = savedSourceMapGenerator
	p.sourceMapSource = savedSourceMapSource
	p.sourceMapSourceIndex = savedSourceMapSourceIndex
	p.sourceMapLineCharCache = savedSourceMapLineCharCache
}
func (p *Printer) emitCommentsBeforeNode(node ast.Handle) *commentState {
	if !p.shouldEmitComments(node) {
		return nil
	}
	emitFlags := p.emitContext.EmitFlags(node)
	commentRange := p.emitContext.CommentRange(node)
	containerPos := p.containerPos
	containerEnd := p.containerEnd
	declarationListContainerEnd := p.declarationListContainerEnd
	p.emitLeadingCommentsOfNode(node, emitFlags, commentRange)
	p.emitLeadingSyntheticCommentsOfNode(node, emitFlags)
	if emitFlags&EFNoNestedComments != 0 {
		p.commentsDisabled = true
	}
	c := p.commentStateArena.New()
	*c = commentState{emitFlags, commentRange, containerPos, containerEnd, declarationListContainerEnd}
	return c
}
func (p *Printer) emitCommentsAfterNode(node ast.Handle, state *commentState) {
	if state == nil {
		return
	}
	emitFlags := state.emitFlags
	commentRange := state.commentRange
	containerPos := state.containerPos
	containerEnd := state.containerEnd
	declarationListContainerEnd := state.declarationListContainerEnd
	if emitFlags&EFNoNestedComments != 0 {
		p.commentsDisabled = false
	}
	p.emitTrailingSyntheticCommentsOfNode(node, emitFlags)
	p.emitTrailingCommentsOfNode(node, emitFlags, commentRange, containerPos, containerEnd, declarationListContainerEnd)
	if typeNode := p.emitContext.GetTypeNode(node); !typeNode.IsNil() {
		p.emitTrailingCommentsOfNode(node, emitFlags, typeNode.Loc(), containerPos, containerEnd, declarationListContainerEnd)
	}
}
func (p *Printer) emitCommentsBeforeToken(token ast.Kind, pos int, contextNode ast.Handle, flags tokenEmitFlags) (*commentState, int) {
	if flags&tefNoComments != 0 || p.commentsDisabled {
		if p.currentSourceFile != nil && !ast.PositionIsSynthesized(pos) {
			pos = scanner.SkipTrivia(p.currentSourceFile.Text(), pos)
		}
		return nil, pos
	}
	startPos := pos
	if p.currentSourceFile != nil {
		pos = scanner.SkipTrivia(p.currentSourceFile.Text(), startPos)
	}
	node := p.emitContext.ParseNode(contextNode)
	isSimilarNode := !node.IsNil() && node.Kind() == contextNode.Kind()
	if !isSimilarNode {
		return nil, pos
	}
	if contextNode.Pos() != startPos {
		indentLeading := flags&tefIndentLeadingComments != 0
		needsIndent := indentLeading && p.currentSourceFile != nil && !PositionsAreOnSameLine(startPos, pos, p.currentSourceFile)
		p.increaseIndentIf(needsIndent)
		p.emitLeadingComments(startPos, false)
		p.decreaseIndentIf(needsIndent)
	}
	return p.commentStateArena.New(), pos
}
func (p *Printer) emitCommentsAfterToken(token ast.Kind, pos int, contextNode ast.Handle, state *commentState) {
	if state == nil {
		return
	}
	if contextNode.End() != pos {
		isJsxExprContext := contextNode.Kind() == ast.KindJsxExpression
		p.emitTrailingComments(pos, core.IfElse(isJsxExprContext, commentSeparatorNone, commentSeparatorBefore))
	}
}
func (p *Printer) emitDetachedCommentsBeforeStatementList(node ast.Handle, detachedRange core.TextRange) *commentState {
	if !p.shouldEmitDetachedComments(node) {
		return nil
	}
	emitFlags := p.emitContext.EmitFlags(node)
	containerPos := p.containerPos
	containerEnd := p.containerEnd
	declarationListContainerEnd := p.declarationListContainerEnd
	skipLeadingComments := ast.PositionIsSynthesized(detachedRange.Pos()) || emitFlags&EFNoLeadingComments != 0
	if !skipLeadingComments {
		p.emitDetachedCommentsAndUpdateCommentsInfo(detachedRange)
	}
	if emitFlags&EFNoNestedComments != 0 {
		p.commentsDisabled = true
	}
	return &commentState{emitFlags, detachedRange, containerPos, containerEnd, declarationListContainerEnd}
}
func (p *Printer) emitDetachedCommentsAfterStatementList(node ast.Handle, detachedRange core.TextRange, state *commentState) {
	if state == nil {
		return
	}
	emitFlags := state.emitFlags
	skipTrailingComments := p.commentsDisabled || ast.PositionIsSynthesized(detachedRange.End()) || emitFlags&EFNoTrailingComments != 0
	if !skipTrailingComments {
		hasWrittenComment := p.emitLeadingComments(detachedRange.End(), false)
		if hasWrittenComment && !p.writer.IsAtStartOfLine() {
			p.writeLine()
		}
	}
}
func (p *Printer) emitLeadingCommentsOfNode(node ast.Handle, emitFlags EmitFlags, commentRange core.TextRange) {
	pos := commentRange.Pos()
	end := commentRange.End()
	if (!ast.PositionIsSynthesized(pos) || !ast.PositionIsSynthesized(end)) && pos != end {
		skipLeadingComments := ast.PositionIsSynthesized(pos) || emitFlags&EFNoLeadingComments != 0 || node.Kind() == ast.KindJsxText
		skipTrailingComments := ast.PositionIsSynthesized(end) || emitFlags&EFNoTrailingComments != 0 || node.Kind() == ast.KindJsxText
		if !skipLeadingComments {
			p.emitLeadingComments(pos, node.Kind() == ast.KindNotEmittedStatement)
		}
		if !skipLeadingComments || (pos >= 0 && (emitFlags&EFNoLeadingComments) != 0) {
			p.containerPos = pos
		}
		if !skipTrailingComments || (end >= 0 && (emitFlags&EFNoTrailingComments) != 0) {
			p.containerEnd = end
			if node.Kind() == ast.KindVariableDeclarationList {
				p.declarationListContainerEnd = end
			}
		}
	}
}
func (p *Printer) emitTrailingCommentsOfNode(node ast.Handle, emitFlags EmitFlags, commentRange core.TextRange, containerPos int, containerEnd int, declarationListContainerEnd int) {
	pos := commentRange.Pos()
	end := commentRange.End()
	skipTrailingComments := end < 0 || (emitFlags&EFNoTrailingComments) != 0 || node.Kind() == ast.KindJsxText
	if (!ast.PositionIsSynthesized(pos) || !ast.PositionIsSynthesized(end)) && pos != end {
		p.containerPos = containerPos
		p.containerEnd = containerEnd
		p.declarationListContainerEnd = declarationListContainerEnd
		if !skipTrailingComments && node.Kind() != ast.KindNotEmittedStatement {
			p.emitTrailingComments(end, commentSeparatorBefore)
		}
	}
}
func (p *Printer) emitLeadingSyntheticCommentsOfNode(node ast.Handle, emitFlags EmitFlags) {
	if emitFlags&EFNoLeadingComments != 0 {
		return
	}
	synth := p.emitContext.GetSyntheticLeadingComments(node)
	for _, c := range synth {
		p.emitLeadingSynthesizedComment(c)
	}
}
func (p *Printer) emitLeadingSynthesizedComment(comment SynthesizedComment) {
	if comment.HasLeadingNewLine || comment.Kind == ast.KindSingleLineCommentTrivia {
		p.writer.WriteLine()
	}
	p.writeSynthesizedComment(comment)
	if comment.HasTrailingNewLine || comment.Kind == ast.KindSingleLineCommentTrivia {
		p.writer.WriteLine()
	} else {
		p.writer.WriteSpace(" ")
	}
}
func (p *Printer) emitTrailingSyntheticCommentsOfNode(node ast.Handle, emitFlags EmitFlags) {
	if emitFlags&EFNoTrailingComments != 0 {
		return
	}
	synth := p.emitContext.GetSyntheticTrailingComments(node)
	for _, c := range synth {
		p.emitTrailingSynthesizedComment(c)
	}
}
func (p *Printer) emitTrailingSynthesizedComment(comment SynthesizedComment) {
	if !p.writer.IsAtStartOfLine() {
		p.writer.WriteSpace(" ")
	}
	p.writeSynthesizedComment(comment)
	if comment.HasTrailingNewLine {
		p.writer.WriteLine()
	}
}
func formatSynthesizedComment(comment SynthesizedComment) string {
	if comment.Kind == ast.KindMultiLineCommentTrivia {
		return "/*" + comment.Text + "*/"
	}
	return "//" + comment.Text
}
func (p *Printer) writeSynthesizedComment(comment SynthesizedComment) {
	text := formatSynthesizedComment(comment)
	var lineMap []core.TextPos
	if comment.Kind == ast.KindMultiLineCommentTrivia {
		lineMap = core.ComputeECMALineStarts(text)
	}
	p.writeCommentRangeWorker(text, lineMap, comment.Kind, core.NewTextRange(0, len(text)))
}
func (p *Printer) emitLeadingComments(pos int, elided bool) bool {
	if p.commentsDisabled || p.currentSourceFile == nil || ast.PositionIsSynthesized(pos) || pos == p.containerPos {
		return false
	}
	tripleSlash := core.TSUnknown
	if !elided {
		if pos == 0 && p.currentSourceFile != nil && p.currentSourceFile.IsDeclarationFile {
			tripleSlash = core.TSFalse
		}
	} else if pos == 0 {
		tripleSlash = core.TSTrue
	} else {
		return false
	}
	if p.detachedCommentsInfo.Len() > 0 {
		if info := p.detachedCommentsInfo.Peek(); info.nodePos == pos {
			pos = p.detachedCommentsInfo.Pop().detachedCommentEndPos
		}
	}
	var comments []ast.CommentRange
	for comment := range scanner.GetLeadingCommentRanges(p.currentSourceFile.Text(), pos) {
		if p.shouldWriteComment(comment) && p.shouldEmitCommentIfTripleSlash(comment, tripleSlash) {
			comments = append(comments, comment)
		}
	}
	if len(comments) > 0 && p.shouldEmitNewLineBeforeLeadingCommentOfPosition(pos, comments[0].Pos()) {
		p.writeLine()
	}
	return p.emitComments(comments, commentSeparatorAfter)
}
func (p *Printer) shouldEmitCommentIfTripleSlash(comment ast.CommentRange, tripleSlash core.Tristate) bool {
	switch tripleSlash {
	case core.TSTrue:
		return p.isTripleSlashComment(comment)
	case core.TSFalse:
		return !p.isTripleSlashComment(comment)
	default:
		return true
	}
}
func (p *Printer) shouldEmitNewLineBeforeLeadingCommentOfPosition(pos int, commentPos int) bool {
	return p.currentSourceFile != nil && pos != commentPos && scanner.ComputeLineOfPosition(p.currentSourceFile.ECMALineMap(), pos) != scanner.ComputeLineOfPosition(p.currentSourceFile.ECMALineMap(), commentPos)
}
func (p *Printer) emitLeadingCommentsOfPosition(pos int) {
	if p.commentsDisabled || pos == -1 {
		return
	}
	p.emitLeadingComments(pos, false)
}
func (p *Printer) emitTrailingComments(pos int, commentSeparator commentSeparator) {
	if p.commentsDisabled {
		return
	}
	if p.commentsDisabled || p.currentSourceFile == nil || p.containerEnd != -1 && (pos == p.containerEnd || pos == p.declarationListContainerEnd) {
		return
	}
	var comments []ast.CommentRange
	for comment := range scanner.GetTrailingCommentRanges(p.currentSourceFile.Text(), pos) {
		if p.shouldWriteComment(comment) {
			comments = append(comments, comment)
		}
	}
	p.emitComments(comments, commentSeparator)
}
func (p *Printer) emitTrailingCommentsOfPosition(pos int, prefixSpace bool, forceNoNewline bool) {
	if p.commentsDisabled || p.currentSourceFile == nil {
		return
	}
	if p.containerEnd != -1 && (pos == p.containerEnd || pos == p.declarationListContainerEnd) {
		return
	}
	var comments []ast.CommentRange
	for comment := range scanner.GetTrailingCommentRanges(p.currentSourceFile.Text(), pos) {
		comments = append(comments, comment)
	}
	if len(comments) == 0 {
		return
	}
	for _, comment := range comments {
		if prefixSpace {
			if !p.shouldWriteComment(comment) {
				continue
			}
			if !p.writer.IsAtStartOfLine() {
				p.writeSpace()
			}
			p.emitComment(comment)
			if comment.HasTrailingNewLine {
				p.writeLine()
			}
			continue
		}
		p.emitComment(comment)
		switch {
		case forceNoNewline:
			if comment.Kind == ast.KindSingleLineCommentTrivia {
				p.writeLine()
			}
		case comment.HasTrailingNewLine:
			p.writeLine()
		default:
			p.writeSpace()
		}
	}
}
func (p *Printer) emitDetachedCommentsAndUpdateCommentsInfo(textRange core.TextRange) {
	if p.currentSourceFile == nil {
		return
	}
	if currentDetachedCommentInfo, ok := p.emitDetachedComments(textRange); ok {
		p.detachedCommentsInfo.Push(currentDetachedCommentInfo)
	}
}
func (p *Printer) emitDetachedComments(textRange core.TextRange) (result detachedCommentsInfo, hasResult bool) {
	if p.currentSourceFile == nil {
		return result, hasResult
	}
	text := p.currentSourceFile.Text()
	lineMap := p.currentSourceFile.ECMALineMap()
	var leadingComments []ast.CommentRange
	if p.commentsDisabled {
		if textRange.Pos() == 0 {
			for comment := range scanner.GetLeadingCommentRanges(text, textRange.Pos()) {
				if IsPinnedComment(text, comment) {
					leadingComments = append(leadingComments, comment)
				}
			}
		}
	} else {
		leadingComments = slices.Collect(scanner.GetLeadingCommentRanges(text, textRange.Pos()))
	}
	if len(leadingComments) > 0 {
		var detachedComments []ast.CommentRange
		var lastComment ast.CommentRange
		for i, comment := range leadingComments {
			if i > 0 {
				lastCommentLine := scanner.ComputeLineOfPosition(lineMap, lastComment.End())
				commentLine := scanner.ComputeLineOfPosition(lineMap, comment.Pos())
				if commentLine >= lastCommentLine+2 {
					break
				}
			}
			detachedComments = append(detachedComments, comment)
			lastComment = comment
		}
		if len(detachedComments) > 0 {
			lastCommentLine := scanner.ComputeLineOfPosition(lineMap, core.LastOrNil(detachedComments).End())
			nodeLine := scanner.ComputeLineOfPosition(lineMap, scanner.SkipTrivia(text, textRange.Pos()))
			if nodeLine >= lastCommentLine+2 {
				var commentsToEmit []ast.CommentRange
				for _, comment := range detachedComments {
					if p.shouldWriteComment(comment) {
						commentsToEmit = append(commentsToEmit, comment)
					}
				}
				if len(commentsToEmit) > 0 {
					if p.shouldEmitNewLineBeforeLeadingCommentOfPosition(textRange.Pos(), commentsToEmit[0].Pos()) {
						p.writeLine()
					}
					p.emitComments(commentsToEmit, commentSeparatorAfter)
				}
				result = detachedCommentsInfo{nodePos: textRange.Pos(), detachedCommentEndPos: core.LastOrNil(detachedComments).End()}
				hasResult = true
			}
		}
	}
	return result, hasResult
}

type commentSeparator uint32

const (
	commentSeparatorNone commentSeparator = iota
	commentSeparatorBefore
	commentSeparatorAfter
)

func (p *Printer) emitComments(comments []ast.CommentRange, commentSeparator commentSeparator) bool {
	interveningSeparator := false
	if len(comments) == 0 {
		return false
	}
	if commentSeparator == commentSeparatorBefore {
		p.writeSpace()
	}
	for _, comment := range comments {
		if interveningSeparator {
			p.writeSpace()
			interveningSeparator = false
		}
		p.emitComment(comment)
		if comment.Kind == ast.KindSingleLineCommentTrivia || comment.HasTrailingNewLine && commentSeparator != commentSeparatorNone {
			p.writeLine()
		} else {
			interveningSeparator = commentSeparator != commentSeparatorNone
		}
	}
	if interveningSeparator && commentSeparator == commentSeparatorAfter {
		p.writeSpace()
	}
	return true
}
func (p *Printer) emitComment(comment ast.CommentRange) {
	p.emitPos(comment.Pos())
	p.writeCommentRange(comment)
	p.emitPos(comment.End())
}
func (p *Printer) isTripleSlashComment(comment ast.CommentRange) bool {
	return p.currentSourceFile != nil && IsRecognizedTripleSlashComment(p.currentSourceFile.Text(), comment)
}
func (p *Printer) setSourceMapSource(source sourcemap.Source) {
	if p.sourceMapsDisabled {
		return
	}
	p.sourceMapSource = source
	p.sourceMapLineCharCache = newLineCharacterCache(source)
	if p.mostRecentSourceMapSource == source {
		p.sourceMapSourceIndex = p.mostRecentSourceMapSourceIndex
		return
	}
	p.sourceMapSourceIsJson = tspath.FileExtensionIs(source.FileName(), tspath.ExtensionJson)
	if p.sourceMapSourceIsJson {
		return
	}
	p.sourceMapSourceIndex = p.sourceMapGenerator.AddSource(source.FileName())
	if p.Options.InlineSources {
		if err := p.sourceMapGenerator.SetSourceContent(p.sourceMapSourceIndex, source.Text()); err != nil {
			panic(err)
		}
	}
	p.mostRecentSourceMapSource = source
	p.mostRecentSourceMapSourceIndex = p.sourceMapSourceIndex
}
func (p *Printer) emitPos(pos int) {
	if p.sourceMapsDisabled || p.sourceMapSource == nil || p.sourceMapGenerator == nil || p.sourceMapSourceIsJson || ast.PositionIsSynthesized(pos) {
		return
	}
	source := p.sourceMapSource
	sourceIndex := p.sourceMapSourceIndex
	lineCharCache := p.sourceMapLineCharCache
	if p.MapSourcePosition != nil {
		mappedSource, mappedPos, ok := p.MapSourcePosition(source, pos)
		if !ok {
			if err := p.sourceMapGenerator.AddGeneratedMapping(p.writer.GetLine(), p.writer.GetColumn()); err != nil {
				panic(err)
			}
			return
		}
		pos = mappedPos
		if mappedSource != source {
			savedSource := p.sourceMapSource
			savedSourceIndex := p.sourceMapSourceIndex
			savedSourceIsJson := p.sourceMapSourceIsJson
			savedLineCharCache := p.sourceMapLineCharCache
			p.setSourceMapSource(mappedSource)
			sourceIndex = p.sourceMapSourceIndex
			lineCharCache = p.sourceMapLineCharCache
			p.sourceMapSource = savedSource
			p.sourceMapSourceIndex = savedSourceIndex
			p.sourceMapSourceIsJson = savedSourceIsJson
			p.sourceMapLineCharCache = savedLineCharCache
		}
	}
	sourceLine, sourceCharacter := lineCharCache.getLineAndCharacter(pos)
	if err := p.sourceMapGenerator.AddSourceMapping(p.writer.GetLine(), p.writer.GetColumn(), sourceIndex, sourceLine, sourceCharacter); err != nil {
		panic(err)
	}
}
func (p *Printer) emitSourcePos(source sourcemap.Source, pos int) {
	if source != p.sourceMapSource {
		savedSourceMapSource := p.sourceMapSource
		savedSourceMapSourceIndex := p.sourceMapSourceIndex
		savedSourceMapLineCharCache := p.sourceMapLineCharCache
		p.setSourceMapSource(source)
		p.emitPos(pos)
		p.sourceMapSource = savedSourceMapSource
		p.sourceMapSourceIndex = savedSourceMapSourceIndex
		p.sourceMapLineCharCache = savedSourceMapLineCharCache
	} else {
		p.emitPos(pos)
	}
}
func (p *Printer) emitSourceMapsBeforeNode(node ast.Handle) *sourceMapState {
	if !p.shouldEmitSourceMaps(node) {
		return nil
	}
	emitFlags := p.emitContext.EmitFlags(node)
	loc := p.emitContext.SourceMapRange(node)
	if !ast.IsNotEmittedStatement(node) && emitFlags&EFNoLeadingSourceMap == 0 && p.currentSourceFile != nil && !ast.PositionIsSynthesized(loc.Pos()) {
		p.emitSourcePos(p.sourceMapSource, scanner.SkipTrivia(p.currentSourceFile.Text(), loc.Pos()))
	}
	if emitFlags&EFNoNestedSourceMaps != 0 {
		p.sourceMapsDisabled = true
	}
	state := p.sourceMapStateArena.New()
	*state = sourceMapState{emitFlags, loc, false}
	return state
}
func (p *Printer) emitSourceMapsAfterNode(node ast.Handle, previousState *sourceMapState) {
	if previousState == nil {
		return
	}
	emitFlags := previousState.emitFlags
	loc := previousState.sourceMapRange
	if emitFlags&EFNoNestedSourceMaps != 0 {
		p.sourceMapsDisabled = false
	}
	if !ast.IsNotEmittedStatement(node) && emitFlags&EFNoTrailingSourceMap == 0 && !ast.PositionIsSynthesized(loc.End()) {
		p.emitSourcePos(p.sourceMapSource, loc.End())
	}
}
func (p *Printer) emitSourceMapsBeforeToken(token ast.Kind, pos int, contextNode ast.Handle, flags tokenEmitFlags) *sourceMapState {
	if !p.shouldEmitTokenSourceMaps(token, pos, contextNode, flags) {
		return nil
	}
	emitFlags := p.emitContext.EmitFlags(contextNode)
	loc, hasLoc := p.emitContext.TokenSourceMapRange(contextNode, token)
	if hasLoc {
		pos = loc.Pos()
	}
	if pos >= 0 && p.currentSourceFile != nil {
		pos = scanner.SkipTrivia(p.currentSourceFile.Text(), pos)
	}
	if emitFlags&EFNoTokenLeadingSourceMaps == 0 && pos >= 0 {
		p.emitSourcePos(p.sourceMapSource, pos)
	}
	state := p.sourceMapStateArena.New()
	*state = sourceMapState{emitFlags, loc, hasLoc}
	return state
}
func (p *Printer) emitSourceMapsAfterToken(token ast.Kind, pos int, contextNode ast.Handle, previousState *sourceMapState) {
	if previousState == nil {
		return
	}
	emitFlags := previousState.emitFlags
	loc := previousState.sourceMapRange
	hasLoc := previousState.hasTokenSourceMapRange
	if emitFlags&EFNoTokenTrailingSourceMaps == 0 {
		if hasLoc {
			pos = loc.End()
		}
		if pos >= 0 {
			p.emitSourcePos(p.sourceMapSource, pos)
		}
	}
}
func (p *Printer) shouldReuseTempVariableScope(node ast.Handle) bool {
	return !node.IsNil() && p.emitContext.EmitFlags(node)&EFReuseTempVariableScope != 0
}
func (p *Printer) pushNameGenerationScope(node ast.Handle) {
	p.nameGenerator.PushScope(p.shouldReuseTempVariableScope(node))
}
func (p *Printer) popNameGenerationScope(node ast.Handle) {
	p.nameGenerator.PopScope(p.shouldReuseTempVariableScope(node))
}
func (p *Printer) generateAllNames(owner ast.Handle, nodes ast.ListRef) {
	if nodes == 0 {
		return
	}
	for _, node := range owner.ListSlice(nodes) {
		p.generateNames(node)
	}
}
func (p *Printer) generateNames(node ast.Handle) {
	if node.IsNil() {
		return
	}
	switch node.Kind() {
	case ast.KindBlock, ast.KindCaseClause, ast.KindDefaultClause:
		p.generateAllNames(node, node.StatementList())
	case ast.KindLabeledStatement, ast.KindWithStatement, ast.KindDoStatement, ast.KindWhileStatement:
		p.generateNames(node.Statement())
	case ast.KindIfStatement:
		p.generateNames(node.IfStatementThenStatement())
		p.generateNames(node.IfStatementElseStatement())
	case ast.KindForStatement, ast.KindForOfStatement, ast.KindForInStatement:
		p.generateNames(node.Initializer())
		p.generateNames(node.Statement())
	case ast.KindSwitchStatement:
		p.generateNames(node.SwitchStatementCaseBlock())
	case ast.KindCaseBlock:
		p.generateAllNames(node, node.CaseBlockClauses())
	case ast.KindTryStatement:
		p.generateNames(node.TryStatementTryBlock())
		p.generateNames(node.TryStatementCatchClause())
		p.generateNames(node.TryStatementFinallyBlock())
	case ast.KindCatchClause:
		p.generateNames(node.CatchClauseVariableDeclaration())
		p.generateNames(node.CatchClauseBlock())
	case ast.KindVariableStatement:
		p.generateNames(node.VariableStatementDeclarationList())
	case ast.KindVariableDeclarationList:
		p.generateAllNames(node, node.VariableDeclarationListDeclarations())
	case ast.KindVariableDeclaration, ast.KindParameter, ast.KindBindingElement, ast.KindClassDeclaration:
		p.generateNameIfNeeded(node.Name())
	case ast.KindFunctionDeclaration:
		p.generateNameIfNeeded(node.Name())
		if p.shouldReuseTempVariableScope(node) {
			p.generateAllNames(node, node.FunctionDeclarationParameters())
			p.generateNames(node.FunctionDeclarationBody())
		}
	case ast.KindObjectBindingPattern, ast.KindArrayBindingPattern:
		p.generateAllNames(node, node.ElementList())
	case ast.KindImportDeclaration, ast.KindJSImportDeclaration:
		p.generateNames(node.ImportDeclarationImportClause())
	case ast.KindImportClause:
		p.generateNameIfNeeded(node.ImportClauseName())
		p.generateNames(node.ImportClauseNamedBindings())
	case ast.KindNamespaceImport, ast.KindNamespaceExport:
		p.generateNameIfNeeded(node.Name())
	case ast.KindNamedImports:
		p.generateAllNames(node, node.ElementList())
	case ast.KindImportSpecifier:
		n := node
		if !n.PropertyName().IsNil() {
			p.generateNameIfNeeded(n.PropertyName())
		} else {
			p.generateNameIfNeeded(n.Name())
		}
	}
}
func (p *Printer) generateAllMemberNames(owner ast.Handle, nodes ast.ListRef) {
	if nodes == 0 {
		return
	}
	for _, node := range owner.ListSlice(nodes) {
		p.generateMemberNames(node)
	}
}
func (p *Printer) generateMemberNames(node ast.Handle) {
	if node.IsNil() {
		return
	}
	switch node.Kind() {
	case ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment, ast.KindPropertyDeclaration, ast.KindPropertySignature, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindGetAccessor, ast.KindSetAccessor:
		p.generateNameIfNeeded(node.Name())
	}
}
func (p *Printer) generateNameIfNeeded(name ast.Handle) {
	if !name.IsNil() {
		if ast.IsMemberName(name) {
			p.generateName(name)
		} else if ast.IsBindingPattern(name) {
			p.generateNames(name)
		}
	}
}

func (p *Printer) generateName(name ast.Handle) {
	_ = p.nameGenerator.GenerateName(name)
}

func (p *Printer) isFileLevelUniqueNameInCurrentFile(name string, _ bool) bool {
	if p.currentSourceFile != nil {
		return p.emitContext.IsFileLevelUniqueName(p.currentSourceFile, name, p.HasGlobalName)
	} else {
		return true
	}
}
func (p *Printer) enterNode(node ast.Handle) printerState {
	state := printerState{}
	if p.OnBeforeEmitNode != nil {
		p.OnBeforeEmitNode(node)
	}
	state.commentState = p.emitCommentsBeforeNode(node)
	state.sourceMapState = p.emitSourceMapsBeforeNode(node)
	return state
}
func (p *Printer) exitNode(node ast.Handle, previousState printerState) {
	p.emitSourceMapsAfterNode(node, previousState.sourceMapState)
	p.emitCommentsAfterNode(node, previousState.commentState)
	if p.OnAfterEmitNode != nil {
		p.OnAfterEmitNode(node)
	}
}
func (p *Printer) enterTokenNode(node ast.Handle, flags tokenEmitFlags) printerState {
	state := printerState{}
	if p.OnBeforeEmitToken != nil {
		p.OnBeforeEmitToken(node)
	}
	if flags&tefNoComments == 0 {
		state.commentState = p.emitCommentsBeforeNode(node)
	}
	if flags&tefNoSourceMaps == 0 {
		state.sourceMapState = p.emitSourceMapsBeforeNode(node)
	}
	return state
}
func (p *Printer) exitTokenNode(node ast.Handle, previousState printerState) {
	p.emitSourceMapsAfterNode(node, previousState.sourceMapState)
	p.emitCommentsAfterNode(node, previousState.commentState)
	if p.OnAfterEmitToken != nil {
		p.OnAfterEmitToken(node)
	}
}

type tokenEmitFlags uint32

const (
	tefNoComments tokenEmitFlags = 1 << iota
	tefIndentLeadingComments
	tefNoSourceMaps
	tefNone tokenEmitFlags = 0
)

func (p *Printer) enterToken(token ast.Kind, pos int, contextNode ast.Handle, flags tokenEmitFlags) (printerState, int) {
	state := printerState{}
	state.commentState, pos = p.emitCommentsBeforeToken(token, pos, contextNode, flags)
	state.sourceMapState = p.emitSourceMapsBeforeToken(token, pos, contextNode, flags)
	return state, pos
}
func (p *Printer) exitToken(token ast.Kind, pos int, contextNode ast.Handle, previousState printerState) {
	p.emitSourceMapsAfterToken(token, pos, contextNode, previousState.sourceMapState)
	p.emitCommentsAfterToken(token, pos, contextNode, previousState.commentState)
}

type ListFormat int

const (
	LFNone                              ListFormat = 0
	LFSingleLine                        ListFormat = 0
	LFMultiLine                         ListFormat = 1 << 0
	LFPreserveLines                     ListFormat = 1 << 1
	LFLinesMask                         ListFormat = LFSingleLine | LFMultiLine | LFPreserveLines
	LFNotDelimited                      ListFormat = 0
	LFBarDelimited                      ListFormat = 1 << 2
	LFAmpersandDelimited                ListFormat = 1 << 3
	LFCommaDelimited                    ListFormat = 1 << 4
	LFAsteriskDelimited                 ListFormat = 1 << 5
	LFDelimitersMask                    ListFormat = LFBarDelimited | LFAmpersandDelimited | LFCommaDelimited | LFAsteriskDelimited
	LFAllowTrailingComma                ListFormat = 1 << 6
	LFIndented                          ListFormat = 1 << 7
	LFSpaceBetweenBraces                ListFormat = 1 << 8
	LFSpaceBetweenSiblings              ListFormat = 1 << 9
	LFBraces                            ListFormat = 1 << 10
	LFParenthesis                       ListFormat = 1 << 11
	LFAngleBrackets                     ListFormat = 1 << 12
	LFSquareBrackets                    ListFormat = 1 << 13
	LFBracketsMask                      ListFormat = LFBraces | LFParenthesis | LFAngleBrackets | LFSquareBrackets
	LFOptionalIfNil                     ListFormat = 1 << 14
	LFOptionalIfEmpty                   ListFormat = 1 << 15
	LFOptional                          ListFormat = LFOptionalIfNil | LFOptionalIfEmpty
	LFPreferNewLine                     ListFormat = 1 << 16
	LFNoTrailingNewLine                 ListFormat = 1 << 17
	LFNoInterveningComments             ListFormat = 1 << 18
	LFNoSpaceIfEmpty                    ListFormat = 1 << 19
	LFSingleElement                     ListFormat = 1 << 20
	LFSpaceAfterList                    ListFormat = 1 << 21
	LFModifiers                         ListFormat = LFSingleLine | LFSpaceBetweenSiblings | LFNoInterveningComments | LFSpaceAfterList
	LFHeritageClauses                   ListFormat = LFSingleLine | LFSpaceBetweenSiblings
	LFSingleLineTypeLiteralMembers      ListFormat = LFSingleLine | LFSpaceBetweenBraces | LFSpaceBetweenSiblings
	LFMultiLineTypeLiteralMembers       ListFormat = LFMultiLine | LFIndented | LFOptionalIfEmpty
	LFSingleLineTupleTypeElements       ListFormat = LFCommaDelimited | LFSpaceBetweenSiblings | LFSingleLine
	LFMultiLineTupleTypeElements        ListFormat = LFCommaDelimited | LFIndented | LFSpaceBetweenSiblings | LFMultiLine
	LFUnionTypeConstituents             ListFormat = LFBarDelimited | LFSpaceBetweenSiblings | LFSingleLine
	LFIntersectionTypeConstituents      ListFormat = LFAmpersandDelimited | LFSpaceBetweenSiblings | LFSingleLine
	LFObjectBindingPatternElements      ListFormat = LFSingleLine | LFAllowTrailingComma | LFSpaceBetweenBraces | LFCommaDelimited | LFSpaceBetweenSiblings | LFNoSpaceIfEmpty
	LFArrayBindingPatternElements       ListFormat = LFSingleLine | LFAllowTrailingComma | LFCommaDelimited | LFSpaceBetweenSiblings | LFNoSpaceIfEmpty
	LFObjectLiteralExpressionProperties ListFormat = LFPreserveLines | LFCommaDelimited | LFSpaceBetweenSiblings | LFSpaceBetweenBraces | LFIndented | LFBraces | LFNoSpaceIfEmpty
	LFImportAttributes                  ListFormat = LFPreserveLines | LFCommaDelimited | LFSpaceBetweenSiblings | LFSpaceBetweenBraces | LFIndented | LFBraces | LFNoSpaceIfEmpty
	LFArrayLiteralExpressionElements    ListFormat = LFPreserveLines | LFCommaDelimited | LFSpaceBetweenSiblings | LFAllowTrailingComma | LFIndented | LFSquareBrackets
	LFCommaListElements                 ListFormat = LFCommaDelimited | LFSpaceBetweenSiblings | LFSingleLine
	LFCallExpressionArguments           ListFormat = LFCommaDelimited | LFSpaceBetweenSiblings | LFSingleLine | LFParenthesis
	LFNewExpressionArguments            ListFormat = LFCommaDelimited | LFSpaceBetweenSiblings | LFSingleLine | LFParenthesis | LFOptionalIfNil
	LFTemplateExpressionSpans           ListFormat = LFSingleLine | LFNoInterveningComments
	LFSingleLineBlockStatements         ListFormat = LFSpaceBetweenBraces | LFSpaceBetweenSiblings | LFSingleLine
	LFMultiLineBlockStatements          ListFormat = LFIndented | LFMultiLine
	LFVariableDeclarationList           ListFormat = LFCommaDelimited | LFSpaceBetweenSiblings | LFSingleLine
	LFSingleLineFunctionBodyStatements  ListFormat = LFSingleLine | LFSpaceBetweenSiblings | LFSpaceBetweenBraces
	LFMultiLineFunctionBodyStatements   ListFormat = LFMultiLine
	LFClassHeritageClauses              ListFormat = LFSingleLine
	LFClassMembers                      ListFormat = LFIndented | LFMultiLine
	LFInterfaceMembers                  ListFormat = LFIndented | LFMultiLine
	LFEnumMembers                       ListFormat = LFCommaDelimited | LFIndented | LFMultiLine
	LFCaseBlockClauses                  ListFormat = LFIndented | LFMultiLine
	LFNamedImportsOrExportsElements     ListFormat = LFCommaDelimited | LFSpaceBetweenSiblings | LFAllowTrailingComma | LFSingleLine | LFSpaceBetweenBraces | LFNoSpaceIfEmpty
	LFJsxElementOrFragmentChildren      ListFormat = LFSingleLine | LFNoInterveningComments
	LFJsxElementAttributes              ListFormat = LFSingleLine | LFSpaceBetweenSiblings | LFNoInterveningComments
	LFCaseOrDefaultClauseStatements     ListFormat = LFIndented | LFMultiLine | LFNoTrailingNewLine | LFOptionalIfEmpty
	LFHeritageClauseTypes               ListFormat = LFCommaDelimited | LFSpaceBetweenSiblings | LFSingleLine
	LFSourceFileStatements              ListFormat = LFMultiLine | LFNoTrailingNewLine
	LFDecorators                        ListFormat = LFMultiLine | LFOptional | LFSpaceAfterList
	LFTypeArguments                     ListFormat = LFCommaDelimited | LFSpaceBetweenSiblings | LFSingleLine | LFAngleBrackets | LFOptional
	LFTypeParameters                    ListFormat = LFCommaDelimited | LFSpaceBetweenSiblings | LFSingleLine | LFAngleBrackets | LFOptional
	LFParameters                        ListFormat = LFCommaDelimited | LFSpaceBetweenSiblings | LFSingleLine | LFParenthesis
	LFSingleArrowParameter              ListFormat = LFCommaDelimited | LFSpaceBetweenSiblings | LFSingleLine
	LFIndexSignatureParameters          ListFormat = LFCommaDelimited | LFSpaceBetweenSiblings | LFSingleLine | LFIndented | LFSquareBrackets
	LFJSDocComment                      ListFormat = LFMultiLine | LFAsteriskDelimited
	LFImportClauseEntries               ListFormat = LFImportAttributes
)

func getOpeningBracket(format ListFormat) string {
	switch format & LFBracketsMask {
	case LFBraces:
		return "{"
	case LFParenthesis:
		return "("
	case LFAngleBrackets:
		return "<"
	case LFSquareBrackets:
		return "["
	default:
		panic(fmt.Sprintf("Unexpected bracket: %v", format&LFBracketsMask))
	}
}
func getClosingBracket(format ListFormat) string {
	switch format & LFBracketsMask {
	case LFBraces:
		return "}"
	case LFParenthesis:
		return ")"
	case LFAngleBrackets:
		return ">"
	case LFSquareBrackets:
		return "]"
	default:
		panic(fmt.Sprintf("Unexpected bracket: %v", format&LFBracketsMask))
	}
}
