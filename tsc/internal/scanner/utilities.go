package scanner

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"strings"
	"unicode"
	"unicode/utf8"
)

func tokenIsIdentifierOrKeyword(token ast.Kind) bool {
	return token >= ast.KindIdentifier
}
func IdentifierToKeywordKind(node ast.Handle) ast.Kind {
	return textToKeyword[node.Text()]
}
func GetSourceTextOfNodeFromSourceFile(sourceFile *ast.SourceFile, node ast.Handle, includeTrivia bool) string {
	return GetTextOfNodeFromSourceText(sourceFile.Text(), node, includeTrivia)
}
func isJSDocTypeExpressionOrChild(node ast.Handle) bool {
	if ast.IsJSDocTypeExpression(node) {
		return true
	}
	if node.Flags()&(ast.NodeFlagsJSDoc|ast.NodeFlagsReparsed) == 0 {
		return false
	}
	for current := node; !current.IsNil(); current = current.Parent() {
		if ast.IsTypeNode(current) {
			return true
		}
	}
	return false
}
func normalizeJSDocTypeSourceText(text string) string {
	lineStarts := core.ComputeECMALineStarts(text)
	if len(lineStarts) == 1 {
		return stripLeadingJSDocComment(text)
	}
	var result strings.Builder
	result.Grow(len(text))
	newLine := core.NewLineKindLF.GetNewLineCharacter()
	for i, lineStart := range lineStarts {
		if i > 0 {
			result.WriteString(newLine)
		}
		lineEnd := len(text)
		if i+1 < len(lineStarts) {
			lineEnd = int(lineStarts[i+1])
		}
		line := strings.TrimRightFunc(text[lineStart:lineEnd], stringutil.IsLineBreak)
		result.WriteString(stripLeadingJSDocComment(line))
	}
	return result.String()
}
func stripLeadingJSDocComment(line string) string {
	line = strings.TrimLeftFunc(line, stringutil.IsWhiteSpaceLike)
	if len(line) > 0 && line[0] == '*' {
		line = line[1:]
	}
	return strings.TrimLeftFunc(line, stringutil.IsWhiteSpaceLike)
}
func GetTextOfNodeFromSourceText(sourceText string, node ast.Handle, includeTrivia bool) string {
	if ast.NodeIsMissing(node) {
		return ""
	}
	pos := node.Pos()
	if !includeTrivia {
		pos = SkipTrivia(sourceText, pos)
	}
	text := sourceText[pos:node.End()]
	if isJSDocTypeExpressionOrChild(node) {
		text = normalizeJSDocTypeSourceText(text)
	}
	if node.Flags()&ast.NodeFlagsReparserTransformedLiteral != 0 {
		if ast.IsStringLiteral(node) {
			if node.StringLiteralTokenFlags()&ast.TokenFlagsSingleQuote != 0 {
				return "'" + text + "'"
			}
			return "\"" + text + "\""
		} else if ast.IsIdentifier(node) {
			return node.Text()
		}
		debug.FailBadSyntaxKind(node, "Unexpected reparser-transformed node kind")
	}
	return text
}
func GetTextOfNode(node ast.Handle) string {
	return GetSourceTextOfNodeFromSourceFile(ast.GetSourceFileOfNode(node), node, false)
}
func GetTextOfJSDocComment(store *ast.Store, comment ast.ListRef) string {
	if store == nil || comment == 0 {
		return ""
	}
	var b strings.Builder
	for _, n := range store.ListSlice(comment).All() {
		switch n.Kind {
		case ast.KindJSDocText:
			b.WriteString(n.Text())
		case ast.KindJSDocLink, ast.KindJSDocLinkCode, ast.KindJSDocLinkPlain:
			b.WriteString(GetTextOfNode(n))
		}
	}
	return strings.TrimRightFunc(b.String(), unicode.IsSpace)
}
func DeclarationNameToString(name ast.Handle) string {
	if name.IsNil() || name.Pos() == name.End() {
		return "(Missing)"
	}
	return GetTextOfNode(name)
}
func IsIdentifierText(name string, languageVariant core.LanguageVariant) bool {
	ch, size := utf8.DecodeRuneInString(name)
	if !IsIdentifierStart(ch) {
		return false
	}
	for i := size; i < len(name); {
		ch, size = utf8.DecodeRuneInString(name[i:])
		if !IsIdentifierPartEx(ch, languageVariant) {
			return false
		}
		i += size
	}
	return true
}
func IsIntrinsicJsxName(name string) bool {
	return len(name) != 0 && (name[0] >= 'a' && name[0] <= 'z' || strings.ContainsRune(name, '-'))
}
