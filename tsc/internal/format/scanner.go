package format

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"slices"
)

type TextRangeWithKind struct {
	Loc  core.TextRange
	Kind ast.Kind
}

func NewTextRangeWithKind(pos int, end int, kind ast.Kind) TextRangeWithKind {
	return TextRangeWithKind{Loc: core.NewTextRange(pos, end), Kind: kind}
}

type tokenInfo struct {
	leadingTrivia []TextRangeWithKind// Read leading trivia and token
	// consume leading trivia
	// May parse an identifier like `module-layout`; that will be scanned as a keyword at first, but we should parse the whole thing to get an identifier.
	// The leftmost name of a dotted JSX tag name (e.g. `a-b` in `<a-b.c>`) may contain hyphens, so rescan it as a JSX identifier.
	// normally scanner returns the smallest available token
	// check the kind of context node to determine if scanner should have more greedy behavior and consume more text.
	// readTokenInfo was called before with the same expected scan action.
	// No need to re-scan text, return existing 'lastTokenInfo'
	// it is ok to call fixTokenKind here since it does not affect
	// what portion of text is consumed. In contrast rescanning can change it,
	// i.e. for '>=' when originally scanner eats just one character
	// and rescanning forces it to consume more.
	// readTokenInfo was called before but scan action differs - rescan text
	// consume trailing trivia
	// move past new line
	/*isTaggedTemplate*/ /*allowMultilineJsxText*/ // TODO: redundant?

	token          TextRangeWithKind
	trailingTrivia []TextRangeWithKind
}
type formattingScanner struct {
	s                *scanner.Scanner
	startPos         int
	endPos           int
	savedPos         int
	hasLastTokenInfo bool
	lastTokenInfo    tokenInfo
	lastScanAction   scanAction
	leadingTrivia    []TextRangeWithKind
	trailingTrivia   []TextRangeWithKind
	wasNewLine       bool
}

func newFormattingScanner(text string, languageVariant core.LanguageVariant, startPos int, endPos int, worker *formatSpanWorker) []core.TextChange {
	scan := scanner.NewScanner()
	scan.SetSkipTrivia(false)
	scan.SetLanguageVariant(languageVariant)
	scan.SetText(text)
	scan.ResetTokenState(startPos)
	fmtScn := &formattingScanner{s: scan, startPos: startPos, endPos: endPos, wasNewLine: true}
	res := worker.execute(fmtScn)
	fmtScn.hasLastTokenInfo = false
	scan.Reset()
	return res
}
func (s *formattingScanner) advance() {
	s.hasLastTokenInfo = false
	isStarted := s.s.TokenFullStart() != s.startPos
	if isStarted {
		s.wasNewLine = len(s.trailingTrivia) > 0 && core.LastOrNil(s.trailingTrivia).Kind == ast.KindNewLineTrivia
	} else {
		s.s.Scan()
	}
	s.leadingTrivia = nil
	s.trailingTrivia = nil
	pos := s.s.TokenFullStart()
	for pos < s.endPos {
		t := s.s.Token()
		if !ast.IsTrivia(t) {
			break
		}
		s.s.Scan()
		item := NewTextRangeWithKind(pos, s.s.TokenFullStart(), t)
		pos = s.s.TokenFullStart()
		s.leadingTrivia = append(s.leadingTrivia, item)
	}
	s.savedPos = s.s.TokenFullStart()
}
func shouldRescanGreaterThanToken(node ast.Handle) bool {
	switch node.Kind() {
	case ast.KindGreaterThanEqualsToken, ast.KindGreaterThanGreaterThanEqualsToken, ast.KindGreaterThanGreaterThanGreaterThanEqualsToken, ast.KindGreaterThanGreaterThanGreaterThanToken, ast.KindGreaterThanGreaterThanToken:
		return true
	}
	return false
}
func shouldRescanJsxIdentifier(node ast.Handle) bool {
	if !node.Parent().IsNil() {
		switch node.Parent().Kind() {
		case ast.KindJsxAttribute, ast.KindJsxOpeningElement, ast.KindJsxClosingElement, ast.KindJsxSelfClosingElement, ast.KindJsxNamespacedName:
			return ast.IsKeywordKind(node.Kind()) || node.Kind() == ast.KindIdentifier
		case ast.KindPropertyAccessExpression:
			return (ast.IsKeywordKind(node.Kind()) || node.Kind() == ast.KindIdentifier) && isLeftmostJsxTagName(node)
		}
	}
	return false
}
func isLeftmostJsxTagName(node ast.Handle) bool {
	return !ast.FindAncestorOrQuit(node, func(n ast.Handle) ast.FindAncestorResult {
		switch {
		case n.Parent().IsNil():
			return ast.FindAncestorQuit
		case ast.IsJsxTagName(n):
			return ast.FindAncestorTrue
		case ast.IsPropertyAccessExpression(n.Parent()) && n.Parent().Expression() == n:
			return ast.FindAncestorFalse
		default:
			return ast.FindAncestorQuit
		}
	}).IsNil()
}
func (s *formattingScanner) shouldRescanJsxText(node ast.Handle) bool {
	if ast.IsJsxText(node) {
		return true
	}
	if !ast.IsJsxElement(node) || s.hasLastTokenInfo == false {
		return false
	}
	return s.lastTokenInfo.token.Kind == ast.KindJsxText
}
func shouldRescanSlashToken(container ast.Handle) bool {
	return container.Kind() == ast.KindRegularExpressionLiteral
}
func shouldRescanTemplateToken(container ast.Handle) bool {
	return container.Kind() == ast.KindTemplateMiddle || container.Kind() == ast.KindTemplateTail
}
func shouldRescanJsxAttributeValue(node ast.Handle) bool {
	return !node.Parent().IsNil() && ast.IsJsxAttribute(node.Parent()) && node.Parent().Initializer() == node
}
func startsWithSlashToken(t ast.Kind) bool {
	return t == ast.KindSlashToken || t == ast.KindSlashEqualsToken
}

type scanAction int

const (
	actionScan scanAction = iota
	actionRescanGreaterThanToken
	actionRescanSlashToken
	actionRescanTemplateToken
	actionRescanJsxIdentifier
	actionRescanJsxText
	actionRescanJsxAttributeValue
)

func fixTokenKind(tokenInfo tokenInfo, container ast.Handle) tokenInfo {
	if ast.IsTokenKind(container.Kind()) && tokenInfo.token.Kind != container.Kind() {
		tokenInfo.token.Kind = container.Kind()
	}
	return tokenInfo
}
func (s *formattingScanner) readTokenInfo(n ast.Handle) tokenInfo {
	debug.Assert(s.isOnToken())
	var expectedScanAction scanAction
	if shouldRescanGreaterThanToken(n) {
		expectedScanAction = actionRescanGreaterThanToken
	} else if shouldRescanSlashToken(n) {
		expectedScanAction = actionRescanSlashToken
	} else if shouldRescanTemplateToken(n) {
		expectedScanAction = actionRescanTemplateToken
	} else if shouldRescanJsxIdentifier(n) {
		expectedScanAction = actionRescanJsxIdentifier
	} else if s.shouldRescanJsxText(n) {
		expectedScanAction = actionRescanJsxText
	} else if shouldRescanJsxAttributeValue(n) {
		expectedScanAction = actionRescanJsxAttributeValue
	} else {
		expectedScanAction = actionScan
	}
	if s.hasLastTokenInfo && expectedScanAction == s.lastScanAction {
		s.lastTokenInfo = fixTokenKind(s.lastTokenInfo, n)
		return s.lastTokenInfo
	}
	if s.s.TokenFullStart() != s.savedPos {
		s.s.ResetTokenState(s.savedPos)
		s.s.Scan()
	}
	currentToken := s.getNextToken(n, expectedScanAction)
	token := NewTextRangeWithKind(s.s.TokenFullStart(), s.s.TokenEnd(), currentToken)
	s.trailingTrivia = nil
	for s.s.TokenFullStart() < s.endPos {
		currentToken = s.s.Scan()
		if !ast.IsTrivia(currentToken) {
			break
		}
		trivia := NewTextRangeWithKind(s.s.TokenFullStart(), s.s.TokenEnd(), currentToken)
		s.trailingTrivia = append(s.trailingTrivia, trivia)
		if currentToken == ast.KindNewLineTrivia {
			s.s.Scan()
			break
		}
	}
	s.hasLastTokenInfo = true
	s.lastTokenInfo = tokenInfo{leadingTrivia: slices.Clone(s.leadingTrivia), token: token, trailingTrivia: slices.Clone(s.trailingTrivia)}
	s.lastTokenInfo = fixTokenKind(s.lastTokenInfo, n)
	return s.lastTokenInfo
}
func (s *formattingScanner) getNextToken(n ast.Handle, expectedScanAction scanAction) ast.Kind {
	token := s.s.Token()
	s.lastScanAction = actionScan
	switch expectedScanAction {
	case actionRescanGreaterThanToken:
		if token == ast.KindGreaterThanToken {
			s.lastScanAction = actionRescanGreaterThanToken
			newToken := s.s.ReScanGreaterThanToken()
			debug.Assert(n.Kind() == newToken)
			return newToken
		}
	case actionRescanSlashToken:
		if startsWithSlashToken(token) {
			s.lastScanAction = actionRescanSlashToken
			newToken := s.s.ReScanSlashToken()
			debug.Assert(n.Kind() == newToken)
			return newToken
		}
	case actionRescanTemplateToken:
		if token == ast.KindCloseBraceToken {
			s.lastScanAction = actionRescanTemplateToken
			return s.s.ReScanTemplateToken(false)
		}
	case actionRescanJsxIdentifier:
		s.lastScanAction = actionRescanJsxIdentifier
		return s.s.ScanJsxIdentifier()
	case actionRescanJsxText:
		s.lastScanAction = actionRescanJsxText
		return s.s.ReScanJsxToken(false)
	case actionRescanJsxAttributeValue:
		s.lastScanAction = actionRescanJsxAttributeValue
		return s.s.ReScanJsxAttributeValue()
	case actionScan:
		break
	default:
		debug.AssertNever(expectedScanAction, "unhandled scan action kind")
	}
	return token
}
func (s *formattingScanner) readEOFTokenRange() TextRangeWithKind {
	debug.Assert(s.isOnEOF())
	return NewTextRangeWithKind(s.s.TokenFullStart(), s.s.TokenEnd(), ast.KindEndOfFile)
}
func (s *formattingScanner) isOnToken() bool {
	current := s.s.Token()
	if s.hasLastTokenInfo {
		current = s.lastTokenInfo.token.Kind
	}
	return current != ast.KindEndOfFile && !ast.IsTrivia(current)
}
func (s *formattingScanner) isOnEOF() bool {
	current := s.s.Token()
	if s.hasLastTokenInfo {
		current = s.lastTokenInfo.token.Kind
	}
	return current == ast.KindEndOfFile
}
func (s *formattingScanner) skipToEndOf(r *core.TextRange) {
	s.s.ResetTokenState(r.End())
	s.savedPos = s.s.TokenFullStart()
	s.lastScanAction = actionScan
	s.hasLastTokenInfo = false
	s.wasNewLine = false
	s.leadingTrivia = nil
	s.trailingTrivia = nil
}
func (s *formattingScanner) skipToStartOf(r *core.TextRange) {
	s.s.ResetTokenState(r.Pos())
	s.savedPos = s.s.TokenFullStart()
	s.lastScanAction = actionScan
	s.hasLastTokenInfo = false
	s.wasNewLine = false
	s.leadingTrivia = nil
	s.trailingTrivia = nil
}
func (s *formattingScanner) getCurrentLeadingTrivia() []TextRangeWithKind {
	return s.leadingTrivia
}
func (s *formattingScanner) lastTrailingTriviaWasNewLine() bool {
	return s.wasNewLine
}
func (s *formattingScanner) getTokenFullStart() int {
	if s.hasLastTokenInfo {
		return s.lastTokenInfo.token.Loc.Pos()
	}
	return s.s.TokenFullStart()
}
func (s *formattingScanner) getStartPos() int {
	return s.getTokenFullStart()
}
