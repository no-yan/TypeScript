package storebinder

import (
	"unicode/utf8"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
)

// The scanner helpers that the binder calls with a *ast.SourceFile, on a
// *store.File. They read only the text and the node; the JSDoc branches of
// the Pointer versions (a reparsed constructor or function, a JSDoc type
// expression, a satisfies tag, a reparser-transformed literal) are left out
// because the Store has no JSDoc nodes.

func getScannerForSourceFile(file *store.File, pos int) *scanner.Scanner {
	s := scanner.NewScanner()
	s.SetText(file.Text)
	s.SetLanguageVariant(file.LanguageVariant)
	s.ResetPos(pos)
	s.Scan()
	return s
}

func getRangeOfTokenAtPosition(file *store.File, pos int) core.TextRange {
	s := getScannerForSourceFile(file, pos)
	return core.NewTextRange(s.TokenStart(), s.TokenEnd())
}

// getErrorRangeForArrowFunction computes the line starts of the file on each
// call; the Pointer version reads the file's cached line map. It runs only
// for a diagnostic on an arrow function.
func getErrorRangeForArrowFunction(file *store.File, node store.Node) core.TextRange {
	pos := scanner.SkipTrivia(file.Text, int(node.Pos()))
	body := node.Body()
	if !body.IsNil() && body.Kind() == ast.KindBlock {
		lineStarts := core.ComputeECMALineStarts(file.Text)
		startLine := scanner.ComputeLineOfPosition(lineStarts, int(body.Pos()))
		endLine := scanner.ComputeLineOfPosition(lineStarts, int(body.End()))
		if startLine < endLine {
			// The arrow function spans multiple lines, make the error span be the first line, inclusive.
			return core.NewTextRange(pos, getECMAEndLinePosition(file.Text, lineStarts, startLine)+1)
		}
	}
	return core.NewTextRange(pos, int(node.End()))
}

// getECMAEndLinePosition is scanner.GetECMAEndLinePosition on a text and its line starts.
func getECMAEndLinePosition(text string, lineStarts []core.TextPos, line int) int {
	pos := int(lineStarts[line])
	for {
		ch, size := utf8.DecodeRuneInString(text[pos:])
		if size == 0 || isLineBreak(ch) {
			return pos - 1
		}
		pos += size
	}
}

// isLineBreak is stringutil.IsLineBreak, which this package does not import.
func isLineBreak(ch rune) bool {
	return ch == '\n' || ch == '\r' || ch == 0x2028 || ch == 0x2029
}

func getErrorRangeForNode(file *store.File, node store.Node) core.TextRange {
	errorNode := node
	switch node.Kind() {
	case ast.KindSourceFile:
		pos := scanner.SkipTrivia(file.Text, 0)
		if pos == len(file.Text) {
			return core.NewTextRange(0, 0)
		}
		return getRangeOfTokenAtPosition(file, pos)
	// This list is a work in progress. Add missing node kinds to improve their error spans
	case ast.KindFunctionDeclaration, ast.KindMethodDeclaration:
		if node.Flags()&ast.NodeFlagsReparsed != 0 {
			errorNode = node
			break
		}
		fallthrough
	case ast.KindVariableDeclaration, ast.KindBindingElement, ast.KindClassDeclaration, ast.KindInterfaceDeclaration,
		ast.KindModuleDeclaration, ast.KindEnumDeclaration, ast.KindEnumMember, ast.KindFunctionExpression,
		ast.KindGetAccessor, ast.KindSetAccessor, ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration, ast.KindPropertyDeclaration,
		ast.KindPropertySignature, ast.KindNamespaceImport:
		errorNode = store.GetNameOfDeclaration(node)
	case ast.KindClassExpression:
		errorNode = node.Name()

	case ast.KindArrowFunction:
		return getErrorRangeForArrowFunction(file, node)
	case ast.KindCaseClause, ast.KindDefaultClause:
		start := scanner.SkipTrivia(file.Text, int(node.Pos()))
		end := int(node.End())
		statements := node.Statements()
		if statements.Len() != 0 {
			end = int(statements.At(0).Pos())
		}
		return core.NewTextRange(start, end)
	case ast.KindReturnStatement, ast.KindYieldExpression:
		pos := scanner.SkipTrivia(file.Text, int(node.Pos()))
		return getRangeOfTokenAtPosition(file, pos)
	case ast.KindSatisfiesExpression:
		pos := scanner.SkipTrivia(file.Text, int(node.AsSatisfiesExpression().Expression().End()))
		return getRangeOfTokenAtPosition(file, pos)
	case ast.KindConstructor:
		if node.Flags()&ast.NodeFlagsReparsed != 0 {
			errorNode = node
			break
		}
		scanner := getScannerForSourceFile(file, int(node.Pos()))
		start := scanner.TokenStart()
		for scanner.Token() != ast.KindConstructorKeyword && scanner.Token() != ast.KindStringLiteral && scanner.Token() != ast.KindEndOfFile {
			scanner.Scan()
		}
		return core.NewTextRange(start, scanner.TokenEnd())
	}
	if errorNode.IsNil() {
		// If we don't have a better node, then just set the error on the first token of
		// construct.
		return getRangeOfTokenAtPosition(file, int(node.Pos()))
	}
	pos := int(errorNode.Pos())
	if !store.NodeIsMissing(errorNode) && errorNode.Kind() != ast.KindJsxText {
		pos = scanner.SkipTrivia(file.Text, pos)
	}
	return core.NewTextRange(pos, int(errorNode.End()))
}

func getSourceTextOfNodeFromSourceFile(file *store.File, node store.Node, includeTrivia bool) string {
	return getTextOfNodeFromSourceText(file.Text, node, includeTrivia)
}

func getTextOfNodeFromSourceText(sourceText string, node store.Node, includeTrivia bool) string {
	if store.NodeIsMissing(node) {
		return ""
	}
	pos := int(node.Pos())
	if !includeTrivia {
		pos = scanner.SkipTrivia(sourceText, pos)
	}
	return sourceText[pos:node.End()]
}

// declarationNameToString takes the file: the Pointer version walks the
// parents to the source file for its text.
func declarationNameToString(file *store.File, name store.Node) string {
	if name.IsNil() || name.Pos() == name.End() {
		return "(Missing)"
	}
	return getSourceTextOfNodeFromSourceFile(file, name, false /*includeTrivia*/)
}

// jsxNamespacedNameText is the JsxNamespacedName case of (*ast.Node).Text,
// which the Store's Text role does not have.
func jsxNamespacedNameText(name store.Node) string {
	n := name.AsJsxNamespacedName()
	return n.Namespace().Text() + ":" + n.Name().Text()
}
