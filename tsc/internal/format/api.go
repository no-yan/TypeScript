package format

import (
	"context"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"unicode/utf8"
)

type FormatRequestKind int

const (
	FormatRequestKindFormatDocument FormatRequestKind = iota
	FormatRequestKindFormatSelection
	FormatRequestKindFormatOnEnter
	FormatRequestKindFormatOnSemicolon
	FormatRequestKindFormatOnOpeningCurlyBrace
	FormatRequestKindFormatOnClosingCurlyBrace
)

type formatContextKey int

const (
	formatOptionsKey formatContextKey = iota
	formatNewlineKey
)

func WithFormatCodeSettings(ctx context.Context, options lsutil.FormatCodeSettings, newLine string) context.Context {
	ctx = context.WithValue(ctx, formatOptionsKey, options)
	ctx = context.WithValue(ctx, formatNewlineKey, newLine)
	return ctx
}
func GetFormatCodeSettingsFromContext(ctx context.Context) lsutil.FormatCodeSettings {
	if opt := ctx.Value(formatOptionsKey); opt != nil {
		return opt.(lsutil.FormatCodeSettings)
	}
	return lsutil.GetDefaultFormatCodeSettings()
}
func GetNewLineOrDefaultFromContext(ctx context.Context) string {
	opt := GetFormatCodeSettingsFromContext(ctx)
	if len(opt.NewLineCharacter) > 0 {
		return opt.NewLineCharacter
	}
	host := ctx.Value(formatNewlineKey).(string)
	if len(host) > 0 {
		return host
	}
	return "\n"
}
func FormatSpan(ctx context.Context, span core.TextRange, file *ast.SourceFile, kind FormatRequestKind) []core.TextChange {
	enclosingNode := findEnclosingNode(span, file)
	opts := GetFormatCodeSettingsFromContext(ctx)
	return newFormattingScanner(file.Text(), file.LanguageVariant, getScanStartPosition(enclosingNode, span, file), span.End(), newFormatSpanWorker(ctx, span, enclosingNode, GetIndentationForNode(enclosingNode, &span, file, opts), getOwnOrInheritedDelta(enclosingNode, opts, file), kind, prepareRangeContainsErrorFunction(file.Diagnostics(), span), file))
}
func FormatNodeGivenIndentation(ctx context.Context, node ast.Handle, file *ast.SourceFile, languageVariant core.LanguageVariant, initialIndentation int, delta int) []core.TextChange {
	textRange := core.NewTextRange(node.Pos(), node.End())
	return newFormattingScanner(file.Text(), languageVariant, textRange.Pos(), textRange.End(), newFormatSpanWorker(ctx, textRange, node, initialIndentation, delta, FormatRequestKindFormatSelection, func(core.TextRange) bool {
		return false
	}, file))
}
func formatNodeLines(ctx context.Context, sourceFile *ast.SourceFile, node ast.Handle, requestKind FormatRequestKind) []core.TextChange {
	if node.IsNil() {
		return nil
	}
	tokenStart := scanner.GetTokenPosOfNode(node, sourceFile, false)
	lineStart := GetLineStartPositionForPosition(tokenStart, sourceFile)
	span := core.NewTextRange(lineStart, node.End())
	return FormatSpan(ctx, span, sourceFile, requestKind)
}
func FormatDocument(ctx context.Context, sourceFile *ast.SourceFile) []core.TextChange {
	return FormatSpan(ctx, core.NewTextRange(0, sourceFile.End()), sourceFile, FormatRequestKindFormatDocument)
}
func FormatSelection(ctx context.Context, sourceFile *ast.SourceFile, start int, end int) []core.TextChange {
	return FormatSpan(ctx, core.NewTextRange(GetLineStartPositionForPosition(start, sourceFile), end), sourceFile, FormatRequestKindFormatSelection)
}
func FormatOnOpeningCurly(ctx context.Context, sourceFile *ast.SourceFile, position int) []core.TextChange {
	openingCurly := findImmediatelyPrecedingTokenOfKind(position, ast.KindOpenBraceToken, sourceFile)
	if openingCurly.IsNil() {
		return nil
	}
	curlyBraceRange := openingCurly.Parent()
	outermostNode := findOutermostNodeWithinListLevel(curlyBraceRange)
	textRange := core.NewTextRange(GetLineStartPositionForPosition(scanner.GetTokenPosOfNode(outermostNode, sourceFile, false), sourceFile), position)
	return FormatSpan(ctx, textRange, sourceFile, FormatRequestKindFormatOnOpeningCurlyBrace)
}
func FormatOnClosingCurly(ctx context.Context, sourceFile *ast.SourceFile, position int) []core.TextChange {
	precedingToken := findImmediatelyPrecedingTokenOfKind(position, ast.KindCloseBraceToken, sourceFile)
	return formatNodeLines(ctx, sourceFile, findOutermostNodeWithinListLevel(precedingToken), FormatRequestKindFormatOnClosingCurlyBrace)
}
func FormatOnSemicolon(ctx context.Context, sourceFile *ast.SourceFile, position int) []core.TextChange {
	semicolon := findImmediatelyPrecedingTokenOfKind(position, ast.KindSemicolonToken, sourceFile)
	return formatNodeLines(ctx, sourceFile, findOutermostNodeWithinListLevel(semicolon), FormatRequestKindFormatOnSemicolon)
}
func FormatOnEnter(ctx context.Context, sourceFile *ast.SourceFile, position int) []core.TextChange {
	line := scanner.GetECMALineOfPosition(sourceFile, position)
	if line == 0 {
		return nil
	}
	startPos := int(scanner.GetECMALineStarts(sourceFile)[line-1])
	endOfFormatSpan := scanner.GetECMAEndLinePosition(sourceFile, line)
	for endOfFormatSpan > startPos {
		ch, s := utf8.DecodeRuneInString(sourceFile.Text()[endOfFormatSpan:])
		if s == 0 || stringutil.IsWhiteSpaceSingleLine(ch) {
			endOfFormatSpan--
			continue
		}
		break
	}
	ch, _ := utf8.DecodeRuneInString(sourceFile.Text()[endOfFormatSpan:])
	if stringutil.IsLineBreak(ch) {
		endOfFormatSpan--
	}
	span := core.NewTextRange(startPos, endOfFormatSpan+1)
	return FormatSpan(ctx, span, sourceFile, FormatRequestKindFormatOnEnter)
}
