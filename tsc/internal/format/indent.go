package format

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"iter"
	"slices"
	"unicode/utf8"
)

func GetIndentationForNode(n /*indentationDelta*/ /*isNextChild*/ ast.Handle, ignoreActualIndentationRange *core.TextRange, sourceFile *ast.SourceFile, options lsutil.FormatCodeSettings) int {
	startline, startpos := scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, scanner.GetTokenPosOfNode(n, sourceFile, false))
	return getIndentationForNodeWorker(n, startline, startpos, ignoreActualIndentationRange, 0, sourceFile, false, options)
}

func GetIndentation(position int, sourceFile *ast.SourceFile, options lsutil.FormatCodeSettings, assumeNewLineBeforeCloseBrace bool) int {
	if position > len(sourceFile.Text()) {
		return options.BaseIndentSize
	}
	if options.IndentStyle == lsutil.IndentStyleNone {
		return 0
	}
	precedingToken := astnav.FindPrecedingTokenEx(sourceFile, position, ast.Handle{}, true)
	enclosingCommentRange := getRangeOfEnclosingComment(sourceFile, position, precedingToken)
	if enclosingCommentRange != nil && enclosingCommentRange.Kind == ast.KindMultiLineCommentTrivia {
		return getCommentIndent(sourceFile, position, options, enclosingCommentRange)
	}
	if precedingToken.IsNil() {
		return options.BaseIndentSize
	}
	if isStringOrRegularExpressionOrTemplateLiteral(precedingToken.Kind) {
		tokenStart := scanner.GetTokenPosOfNode(precedingToken, sourceFile, false)
		if tokenStart <= position && position < precedingToken.End() {
			return 0
		}
	}
	lineAtPosition := scanner.GetECMALineOfPosition(sourceFile, position)
	currentToken := astnav.GetTokenAtPosition(sourceFile, position)
	isObjectLiteral := currentToken.Kind == ast.KindOpenBraceToken && !currentToken.Parent().IsNil() && currentToken.Parent().Kind == ast.KindObjectLiteralExpression
	if options.IndentStyle == lsutil.IndentStyleBlock || isObjectLiteral {
		return getBlockIndent(sourceFile, position, options)
	}
	if precedingToken.Kind == ast.KindCommaToken && !precedingToken.Parent().IsNil() && precedingToken.Parent().Kind != ast.KindBinaryExpression {
		actualIndentation := getActualIndentationForListItemBeforeComma(precedingToken, sourceFile, options)
		if actualIndentation != -1 {
			return actualIndentation
		}
	}
	containerList := getListByPosition(position, precedingToken.Parent(), sourceFile)
	if containerList != 0 && !precedingToken.Loc().ContainedBy(sourceFile.ParseStore().ListLoc(containerList)) {
		useTheSameBaseIndentation := !currentToken.Parent().IsNil() && (currentToken.Parent().Kind == ast.KindFunctionExpression || currentToken.Parent().Kind == ast.KindArrowFunction)
		indentSize := 0
		if !useTheSameBaseIndentation {
			indentSize = options.IndentSize
		}
		res := getActualIndentationForListStartLine(containerList, sourceFile, options)
		if res == -1 {
			return indentSize
		}
		return res + indentSize
	}
	return getSmartIndent(sourceFile, position, precedingToken, lineAtPosition, assumeNewLineBeforeCloseBrace, options)
}
func getCommentIndent(sourceFile *ast.SourceFile, position int, options lsutil.FormatCodeSettings, enclosingCommentRange *ast.CommentRange) int {
	previousLine := scanner.GetECMALineOfPosition(sourceFile, position) - 1
	commentStartLine := scanner.GetECMALineOfPosition(sourceFile, enclosingCommentRange.Pos())
	debug.Assert(commentStartLine >= 0, "commentStartLine >= 0")
	if previousLine <= commentStartLine {
		lineStarts := scanner.GetECMALineStarts(sourceFile)
		return FindFirstNonWhitespaceColumn(int(lineStarts[commentStartLine]), position, sourceFile, options)
	}
	lineStarts := scanner.GetECMALineStarts(sourceFile)
	startPositionOfLine := int(lineStarts[previousLine])
	character, column := findFirstNonWhitespaceCharacterAndColumn(startPositionOfLine, position, sourceFile, options)
	if column == 0 {
		return column
	}
	firstNonWhitespaceCharacterCode := sourceFile.Text()[startPositionOfLine+character]
	if firstNonWhitespaceCharacterCode == '*' {
		return column - 1
	}
	return column
}
func getLeadingCommentRangesOfNode(node ast.Handle, file *ast.SourceFile) iter.Seq[ast.CommentRange] {
	if node.Kind == ast.KindJsxText {
		return nil
	}
	return scanner.GetLeadingCommentRanges(file.Text(), node.Pos())
}
func getRangeOfEnclosingComment(sourceFile *ast.SourceFile, position int, precedingToken ast.Handle) *ast.CommentRange {
	tokenAtPosition := astnav.GetTokenAtPosition(sourceFile, position)
	jsdoc := ast.FindAncestor(tokenAtPosition, ast.IsJSDoc)
	if !jsdoc.IsNil() {
		tokenAtPosition = jsdoc.Parent()
	}
	tokenStart := astnav.GetStartOfNode(tokenAtPosition, sourceFile, false)
	if tokenStart <= position && position < tokenAtPosition.End() {
		return nil
	}
	var trailingRangesOfPreviousToken iter.Seq[ast.CommentRange]
	if !precedingToken.IsNil() {
		trailingRangesOfPreviousToken = scanner.GetTrailingCommentRanges(sourceFile.Text(), precedingToken.End())
	}
	leadingRangesOfNextToken := getLeadingCommentRangesOfNode(tokenAtPosition, sourceFile)
	commentRanges := core.ConcatenateSeq(trailingRangesOfPreviousToken, leadingRangesOfNextToken)
	for commentRange := range commentRanges {
		if commentRange.ContainsExclusive(position) || position == commentRange.End() && (commentRange.Kind == ast.KindSingleLineCommentTrivia || position == len(sourceFile.Text())) {
			return &commentRange
		}
	}
	return nil
}
func getBlockIndent(sourceFile *ast.SourceFile, position int, options lsutil.FormatCodeSettings) int {
	current := position
	for current > 0 {
		ch, size := utf8.DecodeRuneInString(sourceFile.Text()[current:])
		if !stringutil.IsWhiteSpaceLike(ch) {
			break
		}
		current -= size
	}
	lineStart := GetLineStartPositionForPosition(current, sourceFile)
	return FindFirstNonWhitespaceColumn(lineStart, current, sourceFile, options)
}
func getActualIndentationForListItemBeforeComma(commaToken ast.Handle, sourceFile *ast.SourceFile, options lsutil.FormatCodeSettings) int {
	if commaToken.Parent().IsNil() {
		return -1
	}
	containingList := GetContainingList(commaToken, sourceFile)
	if containingList == 0 {
		return -1
	}
	commaIndex := core.FindIndex(commaToken.Store().ListSlice(containingList), func(n ast.Handle) bool {
		return n == commaToken
	})
	if commaIndex > 0 {
		return deriveActualIndentationFromList(containingList, commaIndex-1, sourceFile, options)
	}
	return -1
}

type nextTokenKind int

const (
	nextTokenKindUnknown    nextTokenKind = 0
	nextTokenKindOpenBrace  nextTokenKind = 1
	nextTokenKindCloseBrace nextTokenKind = 2
)

func nextTokenIsCurlyBraceOnSameLineAsCursor(precedingToken ast.Handle, current ast.Handle, lineAtPosition int, sourceFile *ast.SourceFile) nextTokenKind {
	nextToken := astnav.FindNextToken(precedingToken, current, sourceFile)
	if nextToken.IsNil() {
		return nextTokenKindUnknown
	}
	if nextToken.Kind == ast.KindOpenBraceToken {
		return nextTokenKindOpenBrace
	} else if nextToken.Kind == ast.KindCloseBraceToken {
		nextTokenStartLine := getStartLineForNode(nextToken, sourceFile)
		if lineAtPosition == nextTokenStartLine {
			return nextTokenKindCloseBrace
		}
		return nextTokenKindUnknown
	}
	return nextTokenKindUnknown
}
func getSmartIndent(sourceFile *ast.SourceFile, position int, precedingToken ast.Handle, lineAtPosition int, assumeNewLineBeforeCloseBrace bool, options lsutil.FormatCodeSettings) int {
	var previous ast.Handle
	current := precedingToken
	for !current.IsNil() {
		if lsutil.PositionBelongsToNode(current, position, sourceFile) && ShouldIndentChildNode(options, current, previous, sourceFile, true) {
			currentStartLine, currentStartChar := getStartLineAndCharacterForNode(current, sourceFile)
			ntk := nextTokenIsCurlyBraceOnSameLineAsCursor(precedingToken, current, lineAtPosition, sourceFile)
			var indentationDelta int
			if ntk != nextTokenKindUnknown {
				if assumeNewLineBeforeCloseBrace && ntk == nextTokenKindCloseBrace {
					indentationDelta = options.IndentSize
				}
			} else {
				if lineAtPosition != currentStartLine {
					indentationDelta = options.IndentSize
				}
			}
			return getIndentationForNodeWorker(current, currentStartLine, currentStartChar, nil, indentationDelta, sourceFile, true, options)
		}
		actualIndentation := getActualIndentationForListItem(current, sourceFile, options, true)
		if actualIndentation != -1 {
			return actualIndentation
		}
		previous = current
		current = current.Parent()
	}
	return options.BaseIndentSize
}
func getIndentationForNodeWorker(current ast.Handle, currentStartLine int, currentStartCharacter int, ignoreActualIndentationRange *core.TextRange, indentationDelta int, sourceFile *ast.SourceFile, isNextChild bool, options lsutil.FormatCodeSettings) int {
	parent := current.Parent()
	for !parent.IsNil() {
		useActualIndentation := true
		if ignoreActualIndentationRange != nil {
			start := scanner.GetTokenPosOfNode(current, sourceFile, false)
			useActualIndentation = start < ignoreActualIndentationRange.Pos() || start > ignoreActualIndentationRange.End()
		}
		containingListOrParentStartLine, containingListOrParentStartCharacter := getContainingListOrParentStart(parent, current, sourceFile)
		parentAndChildShareLine := containingListOrParentStartLine == currentStartLine || childStartsOnTheSameLineWithElseInIfStatement(parent, current, currentStartLine, sourceFile)
		if useActualIndentation {
			var firstListChild ast.Handle
			containerList := GetContainingList(current, sourceFile)
			if containerList != 0 {
				firstListChild = core.FirstOrNil(current.Store().ListSlice(containerList))
			}
			var listIndentsChild bool
			if !firstListChild.IsNil() {
				listLine := getStartLineForNode(firstListChild, sourceFile)
				listIndentsChild = listLine > containingListOrParentStartLine
			}
			actualIndentation := getActualIndentationForListItem(current, sourceFile, options, listIndentsChild)
			if actualIndentation != -1 {
				return actualIndentation + indentationDelta
			}
			actualIndentation = getActualIndentationForNode(current, parent, currentStartLine, currentStartCharacter, parentAndChildShareLine, sourceFile, options)
			if actualIndentation != -1 {
				return actualIndentation + indentationDelta
			}
		}
		if ShouldIndentChildNode(options, parent, current, sourceFile, isNextChild) && !parentAndChildShareLine {
			indentationDelta += options.IndentSize
		}
		useTrueStart := isArgumentAndStartLineOverlapsExpressionBeingCalled(parent, current, currentStartLine, sourceFile)
		current = parent
		parent = current.Parent()
		if useTrueStart {
			currentStartLine, currentStartCharacter = scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, scanner.GetTokenPosOfNode(current, sourceFile, false))
		} else {
			currentStartLine = containingListOrParentStartLine
			currentStartCharacter = containingListOrParentStartCharacter
		}
	}
	return indentationDelta + options.BaseIndentSize
}

func getActualIndentationForNode(current ast.Handle, parent ast.Handle, cuurentLine int, currentChar int, parentAndChildShareLine bool, sourceFile *ast.SourceFile, options lsutil.FormatCodeSettings) int {
	useActualIndentation := (ast.IsDeclaration(current) || ast.IsStatementButNotDeclaration(current)) && (parent.Kind == ast.KindSourceFile || !parentAndChildShareLine)
	if !useActualIndentation {
		return -1
	}
	return findColumnForFirstNonWhitespaceCharacterInLine(cuurentLine, currentChar, sourceFile, options)
}
func isArgumentAndStartLineOverlapsExpressionBeingCalled(parent ast.Handle, child ast.Handle, childStartLine int, sourceFile *ast.SourceFile) bool {
	if !(ast.IsCallExpression(parent) && slices.Contains(parent.Arguments(), child)) {
		return false
	}
	expressionOfCallExpressionEnd := parent.Expression().End()
	expressionOfCallExpressionEndLine := scanner.GetECMALineOfPosition(sourceFile, expressionOfCallExpressionEnd)
	return expressionOfCallExpressionEndLine == childStartLine
}
func getActualIndentationForListItem(node ast.Handle, sourceFile *ast.SourceFile, options lsutil.FormatCodeSettings, listIndentsChild bool) int {
	if !node.Parent().IsNil() && node.Parent().Kind == ast.KindVariableDeclarationList {
		return -1
	}
	containingList := GetContainingList(node, sourceFile)
	if containingList != 0 {
		index := core.FindIndex(sourceFile.ParseStore().ListSlice(containingList), func(e ast.Handle) bool {
			return e == node
		})
		if index != -1 {
			result := deriveActualIndentationFromList(containingList, index, sourceFile, options)
			if result != -1 {
				return result
			}
		}
		delta := 0
		if listIndentsChild {
			delta = options.IndentSize
		}
		res := getActualIndentationForListStartLine(containingList, sourceFile, options)
		if res == -1 {
			return delta
		}
		return res + delta
	}
	return -1
}
func getActualIndentationForListStartLine(list ast.ListRef, sourceFile *ast.SourceFile, options lsutil.FormatCodeSettings) int {
	if list == 0 {
		return -1
	}
	line, char := scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, sourceFile.ParseStore().ListLoc(list).Pos())
	return findColumnForFirstNonWhitespaceCharacterInLine(line, char, sourceFile, options)
}
func deriveActualIndentationFromList(list ast.ListRef, index int, sourceFile *ast.SourceFile, options lsutil.FormatCodeSettings) int {
	debug.Assert(list != 0 && index >= 0 && index < sourceFile.ParseStore().ListLen(list))
	node := sourceFile.ParseStore().ListAt(list, index)
	line, char := getStartLineAndCharacterForNode(node, sourceFile)
	for i := index; i >= 0; i-- {
		if sourceFile.ParseStore().ListAt(list, i).Kind == ast.KindCommaToken {
			continue
		}
		prevEndLine := scanner.GetECMALineOfPosition(sourceFile, sourceFile.ParseStore().ListAt(list, i).End())
		if prevEndLine != line {
			return findColumnForFirstNonWhitespaceCharacterInLine(line, char, sourceFile, options)
		}
		line, char = getStartLineAndCharacterForNode(sourceFile.ParseStore().ListAt(list, i), sourceFile)
	}
	return -1
}
func findColumnForFirstNonWhitespaceCharacterInLine(line int, char int, sourceFile *ast.SourceFile, options lsutil.FormatCodeSettings) int {
	lineStart := scanner.GetECMAPositionOfLineAndByteOffset(sourceFile, line, 0)
	return FindFirstNonWhitespaceColumn(lineStart, lineStart+char, sourceFile, options)
}
func FindFirstNonWhitespaceColumn(startPos int, endPos int, sourceFile *ast.SourceFile, options lsutil.FormatCodeSettings) int {
	_, col := findFirstNonWhitespaceCharacterAndColumn(startPos, endPos, sourceFile, options)
	return col
}

func findFirstNonWhitespaceCharacterAndColumn(startPos int, endPos int, sourceFile *ast.SourceFile, options lsutil.FormatCodeSettings) (character int, column int) {
	column = 0
	text := sourceFile.Text()
	pos := startPos
	for pos < endPos {
		ch, size := utf8.DecodeRuneInString(text[pos:])
		if !stringutil.IsWhiteSpaceSingleLine(ch) {
			break
		}
		if ch == '\t' {
			if options.TabSize > 0 {
				column += options.TabSize + (column % options.TabSize)
			}
		} else {
			column++
		}
		pos += size
	}
	return pos - startPos, column
}
func childStartsOnTheSameLineWithElseInIfStatement(parent ast.Handle, child ast.Handle, childStartLine int, sourceFile *ast.SourceFile) bool {
	if parent.Kind == ast.KindIfStatement && parent.IfStatementElseStatement() == child {
		elseKeyword := astnav.FindPrecedingToken(sourceFile, child.Pos())
		debug.Assert(!elseKeyword.IsNil())
		elseKeywordStartLine := getStartLineForNode(elseKeyword, sourceFile)
		return elseKeywordStartLine == childStartLine
	}
	return false
}
func getStartLineAndCharacterForNode(n ast.Handle, sourceFile *ast.SourceFile) (line int, character int) {
	return scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, scanner.GetTokenPosOfNode(n, sourceFile, false))
}
func getStartLineForNode(n ast.Handle, sourceFile *ast.SourceFile) int {
	return scanner.GetECMALineOfPosition(sourceFile, scanner.GetTokenPosOfNode(n, sourceFile, false))
}
func GetContainingList(node ast.Handle, sourceFile *ast.SourceFile) ast.ListRef {
	if node.Parent().IsNil() {
		return 0
	}
	return getListByRange(scanner.GetTokenPosOfNode(node, sourceFile, false), node.End(), node.Parent(), sourceFile)
}
func getListByPosition(pos int, node ast.Handle, sourceFile *ast.SourceFile) ast.ListRef {
	if node.IsNil() {
		return 0
	}
	return getListByRange(pos, pos, node, sourceFile)
}
func getListByRange(start int, end int, node ast.Handle, sourceFile *ast.SourceFile) ast.ListRef {
	r := core.NewTextRange(start, end)
	switch node.Kind {
	case ast.KindTypeReference:
		return getList(node.TypeArgumentList(), r, node, sourceFile)
	case ast.KindObjectLiteralExpression:
		return getList(node.PropertyList(), r, node, sourceFile)
	case ast.KindArrayLiteralExpression:
		return getList(node.ElementList(), r, node, sourceFile)
	case ast.KindTypeLiteral:
		return getList(node.MemberList(), r, node, sourceFile)
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindCallSignature, ast.KindConstructor, ast.KindConstructorType, ast.KindConstructSignature:
		tpl := getList(node.TypeParameterList(), r, node, sourceFile)
		if tpl != 0 {
			return tpl
		}
		return getList(node.ParameterList(), r, node, sourceFile)
	case ast.KindGetAccessor:
		return getList(node.ParameterList(), r, node, sourceFile)
	case ast.KindClassDeclaration, ast.KindClassExpression, ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration, ast.KindJSDocTemplateTag:
		return getList(node.TypeParameterList(), r, node, sourceFile)
	case ast.KindNewExpression, ast.KindCallExpression:
		l := getList(node.TypeArgumentList(), r, node, sourceFile)
		if l != 0 {
			return l
		}
		return getList(node.ArgumentList(), r, node, sourceFile)
	case ast.KindVariableDeclarationList:
		return getList(node.VariableDeclarationListDeclarations(), r, node, sourceFile)
	case ast.KindObjectBindingPattern, ast.KindArrayBindingPattern, ast.KindNamedImports, ast.KindNamedExports:
		return getList(node.ElementList(), r, node, sourceFile)
	}
	return 0
}
func getList(list ast.ListRef, r core.TextRange, node ast.Handle, sourceFile *ast.SourceFile) ast.ListRef {
	if list == 0 {
		return 0
	}
	if r.ContainedBy(getVisualListRange(node, node.Store().ListLoc(list), sourceFile)) {
		return list
	}
	return 0
}
func getVisualListRange(node ast.Handle, list core.TextRange, sourceFile *ast.SourceFile) core.TextRange {
	prior := astnav.FindPrecedingToken(sourceFile, list.Pos())
	var priorEnd int
	if prior.IsNil() {
		priorEnd = list.Pos()
	} else {
		priorEnd = prior.End()
	}
	scan := scanner.GetScannerForSourceFile(sourceFile, list.End())
	var nextStart int
	if scan.Token() == ast.KindEndOfFile {
		nextStart = list.End()
	} else {
		nextStart = scan.TokenStart()
	}
	return core.NewTextRange(priorEnd, nextStart)
}
func getContainingListOrParentStart(parent ast.Handle, child ast.Handle, sourceFile *ast.SourceFile) (line int, character int) {
	containingList := GetContainingList(child, sourceFile)
	var startPos int
	if containingList != 0 {
		startPos = sourceFile.ParseStore().ListLoc(containingList).Pos()
	} else {
		startPos = scanner.GetTokenPosOfNode(parent, sourceFile, false)
	}
	return scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, startPos)
}
func isControlFlowEndingStatement(kind ast.Kind, parentKind ast.Kind) bool {
	switch kind {
	case ast.KindReturnStatement, ast.KindThrowStatement, ast.KindContinueStatement, ast.KindBreakStatement:
		return parentKind != ast.KindBlock
	default:
		return false
	}
}

func ShouldIndentChildNode(settings lsutil.FormatCodeSettings, parent ast.Handle, child ast.Handle, sourceFile *ast.SourceFile, isNextChildArg ...bool) bool {
	isNextChild := false
	if len(isNextChildArg) > 0 {
		isNextChild = isNextChildArg[0]
	}
	return NodeWillIndentChild(settings, parent, child, sourceFile, false) && !(isNextChild && !child.IsNil() && isControlFlowEndingStatement(child.Kind, parent.Kind))
}
func NodeWillIndentChild(settings lsutil.FormatCodeSettings, parent ast.Handle, child ast.Handle, sourceFile *ast.SourceFile, indentByDefault bool) bool {
	childKind := ast.KindUnknown
	if !child.IsNil() {
		childKind = child.Kind
	}
	switch parent.Kind {
	case ast.KindExpressionStatement, ast.KindClassDeclaration, ast.KindClassExpression, ast.KindInterfaceDeclaration, ast.KindEnumDeclaration, ast.KindTypeAliasDeclaration, ast.KindArrayLiteralExpression, ast.KindBlock, ast.KindModuleBlock, ast.KindObjectLiteralExpression, ast.KindTypeLiteral, ast.KindMappedType, ast.KindTupleType, ast.KindParenthesizedExpression, ast.KindPropertyAccessExpression, ast.KindCallExpression, ast.KindNewExpression, ast.KindVariableStatement, ast.KindExportAssignment, ast.KindReturnStatement, ast.KindConditionalExpression, ast.KindArrayBindingPattern, ast.KindObjectBindingPattern, ast.KindJsxOpeningElement, ast.KindJsxOpeningFragment, ast.KindJsxSelfClosingElement, ast.KindJsxExpression, ast.KindMethodSignature, ast.KindCallSignature, ast.KindConstructSignature, ast.KindParameter, ast.KindFunctionType, ast.KindConstructorType, ast.KindParenthesizedType, ast.KindTaggedTemplateExpression, ast.KindAwaitExpression, ast.KindNamedExports, ast.KindNamedImports, ast.KindExportSpecifier, ast.KindImportSpecifier, ast.KindPropertyDeclaration, ast.KindCaseClause, ast.KindDefaultClause:
		return true
	case ast.KindCaseBlock:
		return settings.IndentSwitchCase.IsTrueOrUnknown()
	case ast.KindVariableDeclaration, ast.KindPropertyAssignment, ast.KindBinaryExpression:
		if settings.IndentMultiLineObjectLiteralBeginningOnBlankLine.IsFalseOrUnknown() && sourceFile != nil && childKind == ast.KindObjectLiteralExpression {
			return rangeIsOnOneLine(child.Loc(), sourceFile)
		}
		if parent.Kind == ast.KindBinaryExpression && sourceFile != nil && childKind == ast.KindJsxElement {
			parentStartLine := scanner.GetECMALineOfPosition(sourceFile, scanner.SkipTrivia(sourceFile.Text(), parent.Pos()))
			childStartLine := scanner.GetECMALineOfPosition(sourceFile, scanner.SkipTrivia(sourceFile.Text(), child.Pos()))
			return parentStartLine != childStartLine
		}
		if parent.Kind != ast.KindBinaryExpression {
			return true
		}
		return indentByDefault
	case ast.KindDoStatement, ast.KindWhileStatement, ast.KindForInStatement, ast.KindForOfStatement, ast.KindForStatement, ast.KindIfStatement, ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindMethodDeclaration, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor:
		return childKind != ast.KindBlock
	case ast.KindArrowFunction:
		if sourceFile != nil && childKind == ast.KindParenthesizedExpression {
			return rangeIsOnOneLine(child.Loc(), sourceFile)
		}
		return childKind != ast.KindBlock
	case ast.KindExportDeclaration:
		return childKind != ast.KindNamedExports
	case ast.KindImportDeclaration:
		return childKind != ast.KindImportClause || (!child.ImportClauseNamedBindings().IsNil() && child.ImportClauseNamedBindings().Kind != ast.KindNamedImports)
	case ast.KindJsxElement:
		return childKind != ast.KindJsxClosingElement
	case ast.KindJsxFragment:
		return childKind != ast.KindJsxClosingFragment
	case ast.KindIntersectionType, ast.KindUnionType, ast.KindSatisfiesExpression:
		if childKind == ast.KindTypeLiteral || childKind == ast.KindTupleType || childKind == ast.KindMappedType {
			return false
		}
		return indentByDefault
	case ast.KindTryStatement:
		if childKind == ast.KindBlock {
			return false
		}
		return indentByDefault
	}
	return indentByDefault
}

func childIsUnindentedBranchOfConditionalExpression(parent ast.Handle, child ast.Handle, childStartLine int, sourceFile *ast.SourceFile) bool {
	if parent.Kind == ast.KindConditionalExpression && (child == parent.ConditionalExpressionWhenTrue() || child == parent.ConditionalExpressionWhenFalse()) {
		conditionEndLine := scanner.GetECMALineOfPosition(sourceFile, parent.ConditionalExpressionCondition().End())
		if child == parent.ConditionalExpressionWhenTrue() {
			return childStartLine == conditionEndLine
		} else {
			trueStartLine := getStartLineForNode(parent.ConditionalExpressionWhenTrue(), sourceFile)
			trueEndLine := scanner.GetECMALineOfPosition(sourceFile, parent.ConditionalExpressionWhenTrue().End())
			return conditionEndLine == trueStartLine && trueEndLine == childStartLine
		}
	}
	return false
}
func argumentStartsOnSameLineAsPreviousArgument(parent ast.Handle, child ast.Handle, childStartLine int, sourceFile *ast.SourceFile) bool {
	if ast.IsCallExpression(parent) || ast.IsNewExpression(parent) {
		if len(parent.Arguments()) == 0 {
			return false
		}
		currentIndex := core.FindIndex(parent.Arguments(), func(n ast.Handle) bool {
			return n == child
		})
		if currentIndex == -1 {
			return false
		}
		if currentIndex == 0 {
			return false
		}
		previousNode := parent.Arguments()[currentIndex-1]
		lineOfPreviousNode := scanner.GetECMALineOfPosition(sourceFile, previousNode.End())
		if childStartLine == lineOfPreviousNode {
			return true
		}
	}
	return false
}
