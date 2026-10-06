package storeparser

// internal/parser/jsdoc.go on the Store (TODO 7d-1: internal/ast/docs/
// store-checker-design-20260923.md 2.1 and 2.2, and
// store-checker-7d1-implementation-instructions-20260928.md 4). The port
// follows the rules of parser.go (doc.go); in addition:
//
//   - The text parts of JSDocText and JSDocLink are joined into one string
//     (instructions 2): strings.Join replaces stringSliceArena.Clone.
//   - A comment's parent is its host, which does not hold it in a slot:
//     Builder.SetParent writes it for the eager JSDoc, and
//     store.FinishLazyJSDoc (a foreign edge) for the lazy JSDoc.
//   - A list collected in a local slice is written by Builder.List when the
//     Pointer parser makes its NodeList; a raw list (JSDocTypeLiteral's
//     JSDocPropertyTags) has the range 0, 0.

import (
	"strings"
	"unicode"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
)

func init() {
	store.SetParseJSDocForNode(parseJSDocForNode)
}

// parseJSDocForNode lazily parses JSDoc for a node in a TS file.
// Called on first access to Node.JSDoc() for non-JS source files.
// The comments are built in the pool parser's Builder and published as one
// sealed Store by store.FinishLazyJSDoc; the file's counts and diagnostics
// are not touched.
func parseJSDocForNode(sourceFile *store.File, node store.Node) []store.Node {
	p := getParser()
	defer putParser(p)
	p.initializeState(nil, 0, ast.SourceFileParseOptions{FileName: sourceFile.FileName(), Path: sourceFile.Path()}, sourceFile.Text(), sourceFile.ScriptKind)
	ranges := GetJSDocCommentRanges(&p.pragmaFactory, nil, node, sourceFile.Text())
	if len(ranges) == 0 {
		return nil
	}
	jsdoc := make([]store.NodeRef, 0, len(ranges))
	pos := int(node.Pos())
	for _, comment := range ranges {
		if parsed := p.parseJSDocComment(store.NoNodeRef /*parent*/, comment.Pos(), comment.End(), pos); parsed != store.NoNodeRef {
			jsdoc = append(jsdoc, parsed)
			pos = p.end(parsed)
		}
	}
	return store.FinishLazyJSDoc(p.b, node, jsdoc)
}

type jsdocState int32

const (
	jsdocStateBeginningOfLine jsdocState = iota
	jsdocStateSawAsterisk
	jsdocStateSavingComments
	jsdocStateSavingBackticks
)

type propertyLikeParse int32

const (
	propertyLikeParseProperty propertyLikeParse = 1 << iota
	propertyLikeParseParameter
	propertyLikeParseCallbackParameter
)

func (p *Parser) withJSDoc(node store.NodeRef, info jsdocScannerInfo) []store.NodeRef {
	if info&jsdocScannerInfoHasJSDoc == 0 {
		return nil
	}

	// For TS/TSX files, defer JSDoc parsing to first access, unless the comment
	// contains @see/@link (needed for unused-identifier checks).
	// @deprecated is detected via cheap text scan to set PossiblyContainsDeprecatedTag;
	// callers must confirm via JSDoc lookup.
	if !p.isJavaScript() {
		p.b.AddFlags(node, ast.NodeFlagsHasJSDoc)
		if info&jsdocScannerInfoHasDeprecated != 0 {
			p.b.AddFlags(node, ast.NodeFlagsPossiblyContainsDeprecatedTag)
		}
		if info&jsdocScannerInfoHasSeeOrLink == 0 {
			return nil
		}
		// Fall through to eager parse for @see/@link
	}

	ranges := GetJSDocCommentRanges(&p.pragmaFactory, p.jsdocCommentRangesSpace, p.b.View(node), p.sourceText)
	p.jsdocCommentRangesSpace = ranges[:0]

	// Should only be called once per node
	p.hasDeprecatedTag = false
	jsdoc := make([]store.NodeRef, 0, len(ranges))
	pos := p.pos(node)
	for _, comment := range ranges {
		if parsed := p.parseJSDocComment(node, comment.Pos(), comment.End(), pos); parsed != store.NoNodeRef {
			p.b.SetParent(parsed, node)
			jsdoc = append(jsdoc, parsed)
			pos = p.end(parsed)
		}
	}
	if len(jsdoc) != 0 {
		if p.b.View(node).Flags()&ast.NodeFlagsHasJSDoc == 0 {
			p.b.AddFlags(node, ast.NodeFlagsHasJSDoc)
		}
		if p.hasDeprecatedTag {
			p.hasDeprecatedTag = false
			p.b.AddFlags(node, ast.NodeFlagsPossiblyContainsDeprecatedTag)
		}
		if p.isJavaScript() {
			p.reparseTags(node, jsdoc)
		}
		p.jsdocInfos = append(p.jsdocInfos, JSDocInfo{parent: node, jsDocs: jsdoc})
		return jsdoc
	}
	return nil
}

func (p *Parser) parseJSDocTypeExpression(mayOmitBraces bool) store.NodeRef {
	pos := p.nodePos()
	var hasBrace bool
	if mayOmitBraces {
		hasBrace = p.parseOptional(ast.KindOpenBraceToken)
	} else {
		hasBrace = p.parseExpected(ast.KindOpenBraceToken)
	}
	saveContextFlags := p.contextFlags
	p.setContextFlags(ast.NodeFlagsJSDoc, true)
	t := p.parseJSDocType()
	p.contextFlags = saveContextFlags
	if hasBrace {
		p.parseExpectedJSDoc(ast.KindCloseBraceToken)
	}

	return p.b.NewJSDocTypeExpression(p.flags(), int32(pos), int32(p.nodePos()), t)
}

func (p *Parser) parseJSDocNameReference() store.NodeRef {
	pos := p.nodePos()
	hasBrace := p.parseOptional(ast.KindOpenBraceToken)
	entityName := p.parseJSDocLinkName()
	if hasBrace {
		p.parseExpectedJSDoc(ast.KindCloseBraceToken)
	}
	p.scanner.ResetPos(p.scanner.TokenFullStart())
	p.nextTokenJSDoc()
	return p.b.NewJSDocNameReference(p.flags(), int32(pos), int32(p.nodePos()), entityName)
}

// Pass end=-1 to parse the text to the end
func (p *Parser) parseJSDocComment(parent store.NodeRef, start int, end int, fullStart int) store.NodeRef {
	if end == -1 {
		end = len(p.sourceText)
	}
	// Check for /** (JSDoc opening part)
	if !isJSDocLikeText(p.sourceText[start:]) {
		// TODO: This should be a panic, unless parseSingleJSDocComment is calling this (not ported yet)
		return store.NoNodeRef
	}

	saveSourceText := p.sourceText
	saveToken := p.token
	saveContextFlags := p.contextFlags
	saveParsingContexts := p.parsingContexts
	saveScannerState := p.scanner.Mark()
	saveDiagnosticsLength := len(p.diagnostics)
	saveHasParseError := p.hasParseError
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier

	// initial indent is start+4 to account for leading `/** `
	// + 1 because \n is one character before the first character in the line and,
	// if there is no \n before start, -1 is one index before the first character in the string
	initialIndent := start + 4 - (strings.LastIndex(p.sourceText[:start], "\n") + 1)
	// -2 for trailing `*/`
	p.sourceText = p.sourceText[:end-2]
	p.scanner.SetText(p.sourceText)
	// +3 for leading `/**`
	p.scanner.ResetPos(start + 3)
	p.setContextFlags(ast.NodeFlagsJSDoc, true)
	p.parsingContexts |= 1 << PCJSDocComment

	comment := p.parseJSDocCommentWorker(start, end, fullStart, initialIndent)
	// move jsdoc diagnostics to jsdocDiagnostics -- for JS files only
	if p.contextFlags&ast.NodeFlagsJavaScriptFile != 0 {
		p.jsdocDiagnostics = append(p.jsdocDiagnostics, p.diagnostics[saveDiagnosticsLength:]...)
	}
	p.diagnostics = p.diagnostics[0:saveDiagnosticsLength]

	p.sourceText = saveSourceText
	p.scanner.SetText(p.sourceText)
	p.parsingContexts = saveParsingContexts
	p.contextFlags = saveContextFlags
	p.scanner.Rewind(saveScannerState)
	p.token = saveToken
	p.hasParseError = saveHasParseError
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier

	return comment
}

/**
 * @param offset - the offset in the containing file
 * @param indent - the number of spaces to consider as the margin (applies to non-first lines only)
 */
func (p *Parser) parseJSDocCommentWorker(start int, end int, fullStart int, indent int) store.NodeRef {
	// Initially we can parse out a tag.  We also have seen a starting asterisk.
	// This is so that /** * @type */ doesn't parse.
	tags := make([]store.NodeRef, 0, 1)
	tagsPos := -1
	tagsEnd := -1
	state := jsdocStateSawAsterisk
	backtickCount := 0
	inFencedCodeBlock := false
	commentParts := make([]store.NodeRef, 0, 1)
	comments := p.jsdocCommentsSpace
	commentsPos := -1
	linkEnd := start
	margin := -1
	pushComment := func(text string) {
		if margin == -1 {
			margin = indent
		}
		comments = append(comments, text)
		indent += len(text)
	}

	p.nextTokenJSDoc()
	for p.parseOptionalJsdoc(ast.KindWhitespaceTrivia) {
	}
	if p.parseOptionalJsdoc(ast.KindNewLineTrivia) {
		state = jsdocStateBeginningOfLine
		indent = 0
	}
loop:
	for {
		// Detect fenced code blocks by counting consecutive backtick tokens.
		// Three or more consecutive backticks toggle the fenced code block state.
		if p.token != ast.KindBacktickToken && backtickCount > 0 {
			if backtickCount >= 3 {
				inFencedCodeBlock = !inFencedCodeBlock
			}
			backtickCount = 0
		}
		switch p.token {
		case ast.KindAtToken:
			if inFencedCodeBlock || !p.scanner.CanFollowJSDocAt() {
				if inFencedCodeBlock {
					state = jsdocStateSavingBackticks
				} else {
					state = jsdocStateSavingComments
				}
				pushComment(p.scanner.TokenText())
				break
			}
			comments = removeTrailingWhitespace(comments)
			if commentsPos == -1 {
				commentsPos = p.nodePos()
			}
			tag := p.parseTag(tags, indent)
			if tagsPos == -1 {
				tagsPos = p.pos(tag)
			}
			tags = append(tags, tag)
			tagsEnd = p.end(tag)
			// NOTE: According to usejsdoc.org, a tag goes to end of line, except the last tag.
			// Real-world comments may break this rule, so "BeginningOfLine" will not be a real line beginning
			// for malformed examples like `/** @param {string} x @returns {number} the length */`
			state = jsdocStateBeginningOfLine
			margin = -1
		case ast.KindNewLineTrivia:
			comments = append(comments, p.scanner.TokenText())
			state = jsdocStateBeginningOfLine
			indent = 0
		case ast.KindAsteriskToken:
			asterisk := p.scanner.TokenText()
			if state == jsdocStateSawAsterisk {
				// If we've already seen an asterisk, then we can no longer parse a tag on this line
				state = jsdocStateSavingComments
				pushComment(asterisk)
			} else {
				if state != jsdocStateBeginningOfLine {
					panic("state must be BeginningOfLine")
				}
				// Ignore the first asterisk on a line
				state = jsdocStateSawAsterisk
				indent += len(asterisk)
			}
		case ast.KindWhitespaceTrivia:
			if state == jsdocStateSavingComments || state == jsdocStateSavingBackticks {
				panic("whitespace shouldn't come from the scanner while saving top-level comment text")
			}
			// only collect whitespace if we're already saving comments or have just crossed the comment indent margin
			whitespace := p.scanner.TokenText()
			if margin > -1 && indent+len(whitespace) > margin {
				existingIndent := margin - indent
				if existingIndent < 0 {
					existingIndent += len(whitespace)
				}
				if existingIndent < 0 {
					existingIndent = 0
				}
				comments = append(comments, whitespace[existingIndent:])
			}
			indent += len(whitespace)
		case ast.KindEndOfFile:
			break loop
		case ast.KindJSDocCommentTextToken:
			if state != jsdocStateSavingBackticks {
				if inFencedCodeBlock {
					state = jsdocStateSavingBackticks
				} else {
					state = jsdocStateSavingComments
				}
			}
			pushComment(p.scanner.TokenValue())
		case ast.KindBacktickToken:
			backtickCount++
			if state == jsdocStateSavingBackticks {
				state = jsdocStateSavingComments
			} else {
				state = jsdocStateSavingBackticks
			}
			pushComment(p.scanner.TokenText())
		case ast.KindOpenBraceToken:
			if inFencedCodeBlock {
				state = jsdocStateSavingBackticks
				pushComment(p.scanner.TokenText())
				break
			}
			state = jsdocStateSavingComments
			commentEnd := p.scanner.TokenFullStart()
			linkStart := p.scanner.TokenEnd() - 1
			link := p.parseJSDocLink(linkStart)
			if link != store.NoNodeRef {
				if linkEnd == start {
					comments = removeLeadingNewlines(comments)
				}
				jsdocText := p.b.NewJSDocText(p.flags(), int32(linkEnd), int32(commentEnd), strings.Join(comments, ""))
				commentParts = append(commentParts, jsdocText, link)
				comments = comments[:0]
				linkEnd = p.scanner.TokenEnd()
				break
			}
			fallthrough
		default:
			// Anything else is doc comment text. We just save it. Because it
			// wasn't a tag, we can no longer parse a tag on this line until we hit the next
			// line break.
			if state != jsdocStateSavingBackticks {
				if inFencedCodeBlock {
					state = jsdocStateSavingBackticks
				} else {
					state = jsdocStateSavingComments
				}
			}
			pushComment(p.scanner.TokenText())
		}
		if state == jsdocStateSavingComments || state == jsdocStateSavingBackticks {
			p.nextJSDocCommentTextToken(state == jsdocStateSavingBackticks)
		} else {
			p.nextTokenJSDoc()
		}
	}

	p.jsdocCommentsSpace = comments[:0] // Reuse this slice for further parses
	if commentsPos == -1 {
		commentsPos = p.scanner.TokenFullStart()
	}

	if len(comments) > 0 {
		comments[len(comments)-1] = strings.TrimRightFunc(comments[len(comments)-1], unicode.IsSpace)
		jsdocText := p.b.NewJSDocText(p.flags(), int32(linkEnd), int32(commentsPos), strings.Join(comments, ""))
		commentParts = append(commentParts, jsdocText)
	}

	if len(commentParts) > 0 && len(tags) > 0 && commentsPos == -1 {
		panic("having parsed tags implies that the end of the comment span should be set")
	}

	var tagsNodeList store.ListRef
	if tagsPos != -1 {
		tagsNodeList = p.b.List(int32(tagsPos), int32(tagsEnd), tags)
	}

	commentList := p.b.List(int32(start), int32(commentsPos), commentParts)
	return p.b.NewJSDoc(p.flags(), int32(fullStart), int32(end), commentList, tagsNodeList)
}

func removeLeadingNewlines(comments []string) []string {
	i := 0
	for i < len(comments) && strings.TrimLeft(comments[i], "\r\n") == "" {
		i++
	}
	return comments[i:]
}

func trimEnd(s string) string {
	return strings.TrimRightFunc(s, stringutil.IsWhiteSpaceLike)
}

func removeTrailingWhitespace(comments []string) []string {
	end := len(comments)
	for i := len(comments) - 1; i >= 0; i-- {
		trimmed := trimEnd(comments[i])
		if trimmed == "" {
			end = i
		} else {
			comments[i] = trimmed
			break
		}
	}
	return comments[:end]
}

func (p *Parser) isNextNonwhitespaceTokenEndOfFile() bool {
	// We must use infinite lookahead, as there could be any number of newlines :(
	for {
		p.nextTokenJSDoc()
		if p.token == ast.KindEndOfFile {
			return true
		}
		if !(p.token == ast.KindWhitespaceTrivia || p.token == ast.KindNewLineTrivia) {
			return false
		}
	}
}

func (p *Parser) skipWhitespace() {
	if p.token == ast.KindWhitespaceTrivia || p.token == ast.KindNewLineTrivia {
		if p.lookAhead((*Parser).isNextNonwhitespaceTokenEndOfFile) {
			return
			// Don't skip whitespace prior to EoF (or end of comment) - that shouldn't be included in any node's range
		}
	}
	for p.token == ast.KindWhitespaceTrivia || p.token == ast.KindNewLineTrivia {
		p.nextTokenJSDoc()
	}
}

func (p *Parser) skipWhitespaceOrAsterisk() string {
	if p.token == ast.KindWhitespaceTrivia || p.token == ast.KindNewLineTrivia {
		if p.lookAhead((*Parser).isNextNonwhitespaceTokenEndOfFile) {
			return ""
			// Don't skip whitespace prior to EoF (or end of comment) - that shouldn't be included in any node's range
		}
	}

	precedingLineBreak := p.scanner.HasPrecedingLineBreak()
	seenLineBreak := false
	indents := make([]string, 0, 4)
	for (precedingLineBreak && p.token == ast.KindAsteriskToken) || p.token == ast.KindWhitespaceTrivia || p.token == ast.KindNewLineTrivia {
		indents = append(indents, p.scanner.TokenText())
		if p.token == ast.KindNewLineTrivia {
			precedingLineBreak = true
			seenLineBreak = true
			indents = indents[:0]
		} else if p.token == ast.KindAsteriskToken {
			precedingLineBreak = false
		}
		p.nextTokenJSDoc()
	}
	if seenLineBreak {
		return strings.Join(indents, "")
	} else {
		return ""
	}
}

func (p *Parser) parseTag(tags []store.NodeRef, margin int) store.NodeRef {
	if p.token != ast.KindAtToken {
		panic("should be called only at the start of a tag")
	}
	start := p.scanner.TokenStart()
	p.nextTokenJSDoc()

	tagName := p.parseJSDocIdentifierName(diagnostics.Identifier_expected)
	indentText := p.skipWhitespaceOrAsterisk()

	var tag store.NodeRef
	switch p.b.View(tagName).Text() {
	case "implements":
		tag = p.parseImplementsTag(start, tagName, margin, indentText)
	case "augments", "extends":
		tag = p.parseAugmentsTag(start, tagName, margin, indentText)
	case "public":
		tag = p.parseSimpleTag(start, p.b.NewJSDocPublicTag, tagName, margin, indentText)
	case "private":
		tag = p.parseSimpleTag(start, p.b.NewJSDocPrivateTag, tagName, margin, indentText)
	case "protected":
		tag = p.parseSimpleTag(start, p.b.NewJSDocProtectedTag, tagName, margin, indentText)
	case "readonly":
		tag = p.parseSimpleTag(start, p.b.NewJSDocReadonlyTag, tagName, margin, indentText)
	case "override":
		tag = p.parseSimpleTag(start, p.b.NewJSDocOverrideTag, tagName, margin, indentText)
	case "deprecated":
		p.hasDeprecatedTag = true
		tag = p.parseSimpleTag(start, p.b.NewJSDocDeprecatedTag, tagName, margin, indentText)
	case "this":
		tag = p.parseThisTag(start, tagName, margin, indentText)
	case "arg", "argument", "param":
		tag = p.parseParameterOrPropertyTag(start, tagName, propertyLikeParseParameter, margin)
	case "return", "returns":
		tag = p.parseReturnTag(tags, start, tagName, margin, indentText)
	case "template":
		tag = p.parseTemplateTag(start, tagName, margin, indentText)
	case "type":
		tag = p.parseTypeTag(tags, start, tagName, margin, indentText)
	case "typedef":
		tag = p.parseTypedefTag(start, tagName, margin, indentText)
	case "callback":
		tag = p.parseCallbackTag(start, tagName, margin, indentText)
	case "overload":
		tag = p.parseOverloadTag(start, tagName, margin, indentText)
	case "satisfies":
		tag = p.parseSatisfiesTag(start, tagName, margin, indentText)
	case "see":
		tag = p.parseSeeTag(start, tagName, margin, indentText)
	case "exception", "throws":
		tag = p.parseThrowsTag(start, tagName, margin, indentText)
	case "import":
		tag = p.parseImportTag(start, tagName, margin, indentText)
	default:
		tag = p.parseUnknownTag(start, tagName, margin, indentText)
	}
	if tag == store.NoNodeRef {
		panic("tag should not be nil")
	}
	return tag
}

func (p *Parser) parseTrailingTagComments(pos int, end int, margin int, indentText string) store.ListRef {
	// some tags, like typedef and callback, have already parsed their comments earlier
	if len(indentText) == 0 {
		margin += end - pos
	}
	var initialMargin string
	if margin < len(indentText) {
		initialMargin = indentText[margin:]
	}
	return p.parseTagComments(margin, &initialMargin)
}

func (p *Parser) parseTagComments(indent int, initialMargin *string) store.ListRef {
	commentsPos := p.nodePos()
	comments := p.jsdocTagCommentsSpace
	p.jsdocTagCommentsSpace = nil // !!! can parseTagComments call itself?
	parts := p.jsdocTagCommentsPartsSpace
	p.jsdocTagCommentsPartsSpace = nil
	linkEnd := -1
	state := jsdocStateBeginningOfLine
	backtickCount := 0
	inFencedCodeBlock := false
	if indent < 0 {
		panic("indent must be a natural number")
	}
	margin := -1
	pushComment := func(text string) {
		if margin == -1 {
			margin = indent
		}
		comments = append(comments, text)
		indent += len(text)
	}

	if initialMargin != nil {
		// jump straight to saving comments if there is some initial indentation
		if *initialMargin != "" {
			pushComment(*initialMargin)
		}
		state = jsdocStateSawAsterisk
	}
	tok := p.token
loop:
	for {
		// Detect fenced code blocks by counting consecutive backtick tokens.
		// Three or more consecutive backticks toggle the fenced code block state.
		if tok != ast.KindBacktickToken && backtickCount > 0 {
			if backtickCount >= 3 {
				inFencedCodeBlock = !inFencedCodeBlock
			}
			backtickCount = 0
		}
		switch tok {
		case ast.KindNewLineTrivia:
			state = jsdocStateBeginningOfLine
			// don't use pushComment here because we want to keep the margin unchanged
			comments = append(comments, p.scanner.TokenText())
			indent = 0
		case ast.KindAtToken:
			if !inFencedCodeBlock && p.scanner.CanFollowJSDocAt() {
				p.scanner.ResetPos(p.scanner.TokenEnd() - 1)
				break loop
			}
			if inFencedCodeBlock {
				state = jsdocStateSavingBackticks
			} else {
				state = jsdocStateSavingComments
			}
			pushComment(p.scanner.TokenText())
		case ast.KindEndOfFile:
			// Done
			break loop
		case ast.KindWhitespaceTrivia:
			if state == jsdocStateSavingComments || state == jsdocStateSavingBackticks {
				panic("whitespace shouldn't come from the scanner while saving comment text")
			}
			whitespace := p.scanner.TokenText()
			// if the whitespace crosses the margin, take only the whitespace that passes the margin
			if margin > -1 && indent+len(whitespace) > margin {
				comments = append(comments, whitespace[max(margin-indent, 0):])
				if inFencedCodeBlock {
					state = jsdocStateSavingBackticks
				} else {
					state = jsdocStateSavingComments
				}
			}
			indent += len(whitespace)
		case ast.KindOpenBraceToken:
			if inFencedCodeBlock {
				state = jsdocStateSavingBackticks
				pushComment(p.scanner.TokenText())
				break
			}
			state = jsdocStateSavingComments
			commentEnd := p.scanner.TokenFullStart()
			linkStart := p.scanner.TokenEnd() - 1
			link := p.parseJSDocLink(linkStart)
			if link != store.NoNodeRef {
				var commentStart int
				if linkEnd > -1 {
					commentStart = linkEnd
				} else {
					commentStart = commentsPos
				}
				text := p.b.NewJSDocText(p.flags(), int32(commentStart), int32(commentEnd), strings.Join(comments, ""))
				parts = append(parts, text)
				parts = append(parts, link)
				comments = comments[:0]
				linkEnd = p.scanner.TokenEnd()
			} else {
				pushComment(p.scanner.TokenText())
			}
		case ast.KindBacktickToken:
			backtickCount++
			if state == jsdocStateSavingBackticks {
				state = jsdocStateSavingComments
			} else {
				state = jsdocStateSavingBackticks
			}
			pushComment(p.scanner.TokenText())
		case ast.KindJSDocCommentTextToken:
			if state != jsdocStateSavingBackticks {
				if inFencedCodeBlock {
					state = jsdocStateSavingBackticks
				} else {
					state = jsdocStateSavingComments
				}
				// leading identifiers start recording as well
			}
			pushComment(p.scanner.TokenValue())
		case ast.KindAsteriskToken:
			if state == jsdocStateBeginningOfLine {
				// leading asterisks start recording on the *next* (non-whitespace) token
				state = jsdocStateSawAsterisk
				indent += 1
				break
			}
			// record the * as a comment
			fallthrough
		default:
			if state != jsdocStateSavingBackticks {
				if inFencedCodeBlock {
					state = jsdocStateSavingBackticks
				} else {
					state = jsdocStateSavingComments
				}
				// leading identifiers start recording as well
			}
			pushComment(p.scanner.TokenText())
		}
		if state == jsdocStateSavingComments || state == jsdocStateSavingBackticks {
			tok = p.nextJSDocCommentTextToken(state == jsdocStateSavingBackticks)
		} else {
			tok = p.nextTokenJSDoc()
		}
	}

	p.jsdocTagCommentsSpace = comments[:0]

	comments = removeLeadingNewlines(comments)
	comments = removeTrailingWhitespace(comments)
	if len(comments) > 0 {
		var commentStart int
		if linkEnd > -1 {
			commentStart = linkEnd
		} else {
			commentStart = commentsPos
		}
		text := p.b.NewJSDocText(p.flags(), int32(commentStart), int32(p.nodePos()), strings.Join(comments, ""))
		parts = append(parts, text)
	}

	p.jsdocTagCommentsPartsSpace = parts[:0]

	if len(parts) > 0 {
		return p.b.List(int32(commentsPos), int32(p.scanner.TokenEnd()), parts)
	}
	return store.NoListRef
}

func (p *Parser) parseJSDocLink(start int) store.NodeRef {
	state := p.mark()
	linkType, ok := p.parseJSDocLinkPrefix()
	if !ok {
		p.rewind(state)
		return store.NoNodeRef
	}
	p.nextTokenJSDoc()
	// start at token after link, then skip any whitespace
	p.skipWhitespace()
	name := p.parseJSDocLinkName()
	var text []string
	for p.token != ast.KindCloseBraceToken && p.token != ast.KindNewLineTrivia && p.token != ast.KindEndOfFile {
		text = append(text, p.scanner.TokenText())
		p.nextTokenJSDoc() // Couldn't this be nextTokenCommentJSDoc?
	}
	switch linkType {
	case "link":
		return p.b.NewJSDocLink(p.flags(), int32(start), int32(p.scanner.TokenEnd()), name, strings.Join(text, ""))
	case "linkcode":
		return p.b.NewJSDocLinkCode(p.flags(), int32(start), int32(p.scanner.TokenEnd()), name, strings.Join(text, ""))
	default:
		return p.b.NewJSDocLinkPlain(p.flags(), int32(start), int32(p.scanner.TokenEnd()), name, strings.Join(text, ""))
	}
}

func (p *Parser) parseJSDocLinkName() store.NodeRef {
	if tokenIsIdentifierOrKeyword(p.token) {
		pos := p.nodePos()
		name := p.parseIdentifierName()
		for p.parseOptional(ast.KindDotToken) {
			var right store.NodeRef
			if p.token == ast.KindPrivateIdentifier {
				right = p.createMissingIdentifier()
			} else {
				right = p.parseIdentifierName()
			}
			name = p.b.NewQualifiedName(p.flags(), int32(pos), int32(p.nodePos()), name, right)
		}
		for p.token == ast.KindPrivateIdentifier {
			p.scanner.ReScanHashToken()
			p.nextTokenJSDoc()
			right := p.parseIdentifier()
			name = p.b.NewQualifiedName(p.flags(), int32(pos), int32(p.nodePos()), name, right)
		}
		return name
	}
	return store.NoNodeRef
}

func (p *Parser) parseJSDocLinkPrefix() (string, bool) {
	p.skipWhitespaceOrAsterisk()
	if p.token == ast.KindOpenBraceToken && p.nextTokenJSDoc() == ast.KindAtToken && tokenIsIdentifierOrKeyword(p.nextTokenJSDoc()) {
		kind := p.scanner.TokenValue()
		if isJSDocLinkTag(kind) {
			return kind, true
		}
	}
	return "NONE", false
}

func isJSDocLinkTag(kind string) bool {
	return kind == "link" || kind == "linkcode" || kind == "linkplain"
}

func (p *Parser) parseUnknownTag(start int, tagName store.NodeRef, indent int, indentText string) store.NodeRef {
	comments := p.parseTrailingTagComments(start, p.nodePos(), indent, indentText)
	return p.b.NewJSDocUnknownTag(p.flags(), int32(start), int32(p.nodePos()), tagName, comments)
}

func (p *Parser) tryParseTypeExpression() store.NodeRef {
	p.skipWhitespaceOrAsterisk()
	if p.token == ast.KindOpenBraceToken {
		return p.parseJSDocTypeExpression(false /*mayOmitBraces*/)
	} else {
		return store.NoNodeRef
	}
}

func (p *Parser) parseBracketNameInPropertyAndParamTag(target propertyLikeParse) (name store.NodeRef, isBracketed bool) {
	// Looking for something like '[foo]', 'foo', '[foo.bar]' or 'foo.bar'
	isBracketed = p.parseOptionalJsdoc(ast.KindOpenBracketToken)
	if isBracketed {
		p.skipWhitespace()
	}
	// a markdown-quoted name: `arg` is not legal jsdoc, but occurs in the wild
	isBackquoted := p.parseOptionalJsdoc(ast.KindBacktickToken)
	name = p.parseJSDocEntityName(core.IfElse(target == propertyLikeParseParameter, nil, diagnostics.Identifier_expected))
	if isBackquoted {
		p.parseExpectedTokenJSDoc(ast.KindBacktickToken)
	}
	if isBracketed {
		p.skipWhitespace()
		// May have an optional default, e.g. '[foo = 42]'
		if p.parseOptionalToken(ast.KindEqualsToken) != store.NoNodeRef {
			p.parseExpression()
		}

		p.parseExpected(ast.KindCloseBracketToken)
	}

	return name, isBracketed
}

func isObjectOrObjectArrayTypeReference(node store.Node) bool {
	switch node.Kind() {
	case ast.KindObjectKeyword:
		return true
	case ast.KindArrayType:
		return isObjectOrObjectArrayTypeReference(node.AsArrayTypeNode().ElementType())
	default:
		if store.IsTypeReferenceNode(node) {
			ref := node.AsTypeReferenceNode()
			return store.IsIdentifier(ref.TypeName()) && ref.TypeName().Text() == "Object" && ref.TypeArguments().IsNil()
		}
		return false
	}
}

func (p *Parser) parseParameterOrPropertyTag(start int, tagName store.NodeRef, target propertyLikeParse, indent int) store.NodeRef {
	typeExpression := p.tryParseTypeExpression()
	isNameFirst := typeExpression == store.NoNodeRef
	p.skipWhitespaceOrAsterisk()

	name, isBracketed := p.parseBracketNameInPropertyAndParamTag(target)
	indentText := p.skipWhitespaceOrAsterisk()

	if isNameFirst && p.lookAhead(func(p *Parser) bool { _, ok := p.parseJSDocLinkPrefix(); return !ok }) {
		typeExpression = p.tryParseTypeExpression()
	}

	comment := p.parseTrailingTagComments(start, p.nodePos(), indent, indentText)

	nestedTypeLiteral := p.parseNestedTypeLiteral(typeExpression, name, target, indent)
	if nestedTypeLiteral != store.NoNodeRef {
		typeExpression = nestedTypeLiteral
		isNameFirst = true
	}
	kind := core.IfElse(target == propertyLikeParseProperty, ast.KindJSDocPropertyTag, ast.KindJSDocParameterTag)
	return p.b.NewJSDocParameterOrPropertyTag(kind, p.flags(), int32(start), int32(p.nodePos()), tagName, name, isBracketed, typeExpression, isNameFirst, comment)
}

func (p *Parser) parseNestedTypeLiteral(typeExpression store.NodeRef, name store.NodeRef, target propertyLikeParse, indent int) store.NodeRef {
	if typeExpression != store.NoNodeRef && isObjectOrObjectArrayTypeReference(p.b.View(typeExpression).Type()) {
		pos := p.nodePos()
		var children []store.NodeRef
		for {
			state := p.mark()
			child := p.parseChildParameterOrPropertyTag(target, indent, name)
			if child == store.NoNodeRef {
				p.rewind(state)
				break
			}
			switch p.b.View(child).Kind() {
			case ast.KindJSDocParameterTag, ast.KindJSDocPropertyTag:
				children = append(children, child)
			case ast.KindJSDocTemplateTag:
				p.parseErrorAtRange(p.b.View(child).TagName().Loc(), diagnostics.A_JSDoc_template_tag_may_not_follow_a_typedef_callback_or_overload_tag)
			}
		}
		if children != nil {
			literal := p.b.NewJSDocTypeLiteral(p.flags(), int32(pos), int32(p.nodePos()), p.rawList(children), p.b.View(typeExpression).Type().Kind() == ast.KindArrayType)
			return p.b.NewJSDocTypeExpression(p.flags(), int32(pos), int32(p.nodePos()), literal)
		}
	}
	return store.NoNodeRef
}

// rawList is a raw []*ast.Node of the Pointer AST (nil stays nil) as a block
// without a range.
func (p *Parser) rawList(nodes []store.NodeRef) store.ListRef {
	if nodes == nil {
		return store.NoListRef
	}
	return p.b.List(0, 0, nodes)
}

func (p *Parser) parseReturnTag(previousTags []store.NodeRef, start int, tagName store.NodeRef, indent int, indentText string) store.NodeRef {
	if core.Some(previousTags, func(tag store.NodeRef) bool { return p.b.View(tag).Kind() == ast.KindJSDocReturnTag }) {
		p.parseErrorAt(p.pos(tagName), p.scanner.TokenStart(), diagnostics.X_0_tag_already_specified, p.b.View(tagName).Text())
	}

	typeExpression := p.tryParseTypeExpression()
	comments := p.parseTrailingTagComments(start, p.nodePos(), indent, indentText)
	return p.b.NewJSDocReturnTag(p.flags(), int32(start), int32(p.nodePos()), tagName, typeExpression, comments)
}

// pass indent=-1 to skip parsing trailing comments (as when a type tag is nested in a typedef)
func (p *Parser) parseTypeTag(previousTags []store.NodeRef, start int, tagName store.NodeRef, indent int, indentText string) store.NodeRef {
	if core.Some(previousTags, func(tag store.NodeRef) bool { return p.b.View(tag).Kind() == ast.KindJSDocTypeTag }) {
		p.parseErrorAt(p.pos(tagName), p.scanner.TokenStart(), diagnostics.X_0_tag_already_specified, p.b.View(tagName).Text())
	}

	typeExpression := p.parseJSDocTypeExpression(true)
	var comments store.ListRef
	if indent != -1 {
		comments = p.parseTrailingTagComments(start, p.nodePos(), indent, indentText)
	}
	return p.b.NewJSDocTypeTag(p.flags(), int32(start), int32(p.nodePos()), tagName, typeExpression, comments)
}

func (p *Parser) parseSeeTag(start int, tagName store.NodeRef, indent int, indentText string) store.NodeRef {
	hasNameReference := p.isIdentifier() && !strings.HasPrefix(p.sourceText[p.scanner.TokenEnd():], "://") ||
		p.token == ast.KindOpenBraceToken && p.lookAhead((*Parser).nextTokenIsIdentifierOrKeyword)
	var nameExpression store.NodeRef
	if hasNameReference {
		nameExpression = p.parseJSDocNameReference()
	}
	comments := p.parseTrailingTagComments(start, p.nodePos(), indent, indentText)
	return p.b.NewJSDocSeeTag(p.flags(), int32(start), int32(p.nodePos()), tagName, nameExpression, comments)
}

func (p *Parser) parseImplementsTag(start int, tagName store.NodeRef, margin int, indentText string) store.NodeRef {
	className := p.parseExpressionWithTypeArgumentsForAugments()
	comments := p.parseTrailingTagComments(start, p.nodePos(), margin, indentText)
	return p.b.NewJSDocImplementsTag(p.flags(), int32(start), int32(p.nodePos()), tagName, className, comments)
}

func (p *Parser) parseAugmentsTag(start int, tagName store.NodeRef, margin int, indentText string) store.NodeRef {
	className := p.parseExpressionWithTypeArgumentsForAugments()
	comments := p.parseTrailingTagComments(start, p.nodePos(), margin, indentText)
	return p.b.NewJSDocAugmentsTag(p.flags(), int32(start), int32(p.nodePos()), tagName, className, comments)
}

func (p *Parser) parseSatisfiesTag(start int, tagName store.NodeRef, margin int, indentText string) store.NodeRef {
	typeExpression := p.parseJSDocTypeExpression(false)
	comments := p.parseTrailingTagComments(start, p.nodePos(), margin, indentText)
	return p.b.NewJSDocSatisfiesTag(p.flags(), int32(start), int32(p.nodePos()), tagName, typeExpression, comments)
}

func (p *Parser) parseThrowsTag(start int, tagName store.NodeRef, margin int, indentText string) store.NodeRef {
	typeExpression := p.tryParseTypeExpression()
	comment := p.parseTrailingTagComments(start, p.nodePos(), margin, indentText)
	return p.b.NewJSDocThrowsTag(p.flags(), int32(start), int32(p.nodePos()), tagName, typeExpression, comment)
}

func (p *Parser) parseImportTag(start int, tagName store.NodeRef, margin int, indentText string) store.NodeRef {
	afterImportTagPos := p.scanner.TokenFullStart()

	var identifier store.NodeRef
	if p.isIdentifier() {
		identifier = p.parseIdentifier()
	}

	importClause := p.tryParseImportClause(identifier, afterImportTagPos, ast.KindTypeKeyword, true /*skipJSDocLeadingAsterisks*/)
	moduleSpecifier := p.parseModuleSpecifier()
	attributes := p.tryParseImportAttributes()

	comments := p.parseTrailingTagComments(start, p.nodePos(), margin, indentText)
	return p.b.NewJSDocImportTag(p.flags(), int32(start), int32(p.nodePos()), tagName, importClause, moduleSpecifier, attributes, comments)
}

func (p *Parser) parseExpressionWithTypeArgumentsForAugments() store.NodeRef {
	usedBrace := p.parseOptional(ast.KindOpenBraceToken)
	pos := p.nodePos()
	expression := p.parsePropertyAccessEntityNameExpression()
	p.scanner.SetSkipJSDocLeadingAsterisks(true)
	typeArguments := p.parseTypeArguments()
	p.scanner.SetSkipJSDocLeadingAsterisks(false)
	node := p.b.NewExpressionWithTypeArguments(p.flags(), int32(pos), int32(p.nodePos()), expression, typeArguments)
	if usedBrace {
		p.skipWhitespace()
		p.parseExpected(ast.KindCloseBraceToken)
	}
	return node
}

func (p *Parser) parsePropertyAccessEntityNameExpression() store.NodeRef {
	pos := p.nodePos()
	node := p.parseJSDocIdentifierName(diagnostics.Identifier_expected)
	for p.parseOptional(ast.KindDotToken) {
		name := p.parseJSDocIdentifierName(diagnostics.Identifier_expected)
		node = p.b.NewPropertyAccessExpression(p.flags(), int32(pos), int32(p.nodePos()), node, store.NoNodeRef, name)
	}
	return node
}

func (p *Parser) parseSimpleTag(start int, createTag func(flags ast.NodeFlags, pos, end int32, tagName store.NodeRef, comment store.ListRef) store.NodeRef, tagName store.NodeRef, margin int, indentText string) store.NodeRef {
	comments := p.parseTrailingTagComments(start, p.nodePos(), margin, indentText)
	return createTag(p.flags(), int32(start), int32(p.nodePos()), tagName, comments)
}

func (p *Parser) parseThisTag(start int, tagName store.NodeRef, margin int, indentText string) store.NodeRef {
	typeExpression := p.parseJSDocTypeExpression(true)
	p.skipWhitespace()
	comments := p.parseTrailingTagComments(start, p.nodePos(), margin, indentText)
	return p.b.NewJSDocThisTag(p.flags(), int32(start), int32(p.nodePos()), tagName, typeExpression, comments)
}

func (p *Parser) parseJSDocTypeNameWithNamespace(nested bool) store.NodeRef {
	start := p.scanner.TokenStart()
	if !tokenIsIdentifierOrKeyword(p.token) {
		return store.NoNodeRef
	}
	typeNameOrNamespaceName := p.parseJSDocIdentifierName(nil)
	if p.parseOptionalJsdoc(ast.KindDotToken) {
		body := p.parseJSDocTypeNameWithNamespace(true /*nested*/)
		var flags ast.NodeFlags
		if nested {
			flags |= ast.NodeFlagsNestedNamespace
		}
		return p.b.NewModuleDeclaration(flags|p.flags(), int32(start), int32(p.nodePos()),
			store.NoListRef,          /*modifiers*/
			ast.KindNamespaceKeyword, /*keyword*/
			typeNameOrNamespaceName,
			store.NoNodeRef, /*attributes*/
			body,
		)
	}
	if nested {
		p.b.AddFlags(typeNameOrNamespaceName, ast.NodeFlagsIdentifierIsInJSDocNamespace)
	}
	return typeNameOrNamespaceName
}

func (p *Parser) parseTypedefTag(start int, tagName store.NodeRef, indent int, indentText string) store.NodeRef {
	typeExpression := p.tryParseTypeExpression()
	p.skipWhitespaceOrAsterisk()
	fullName := p.parseJSDocTypeNameWithNamespace(false /*nested*/)
	if fullName == store.NoNodeRef {
		fullName = p.parseJSDocIdentifierName(diagnostics.Identifier_expected)
	}
	p.skipWhitespace()
	comment := p.parseTagComments(indent, nil)

	end := -1
	hasChildren := false
	if typeExpression == store.NoNodeRef || isObjectOrObjectArrayTypeReference(p.b.View(typeExpression).Type()) {
		var child store.NodeRef
		var childTypeTag store.NodeRef
		var jsdocPropertyTags []store.NodeRef
		for {
			state := p.mark()
			child = p.parseChildPropertyTag(indent)
			if child == store.NoNodeRef {
				p.rewind(state)
				break
			}
			hasChildren = true
			switch p.b.View(child).Kind() {
			case ast.KindJSDocTemplateTag:
				p.parseErrorAtRange(p.b.View(child).TagName().Loc(), diagnostics.A_JSDoc_template_tag_may_not_follow_a_typedef_callback_or_overload_tag)
			case ast.KindJSDocTypeTag:
				if childTypeTag == store.NoNodeRef {
					childTypeTag = child
				} else {
					lastError := p.parseErrorAtCurrentToken(diagnostics.A_JSDoc_typedef_comment_may_not_contain_multiple_type_tags)
					if lastError != nil {
						related := ast.NewDiagnostic(nil, core.NewTextRange(0, 0), diagnostics.The_tag_was_first_specified_here)
						lastError.AddRelatedInfo(related)
					}
				}
			default:
				jsdocPropertyTags = append(jsdocPropertyTags, child)
			}
		}
		if hasChildren {
			isArrayType := typeExpression != store.NoNodeRef && p.b.View(typeExpression).Type().Kind() == ast.KindArrayType
			// The Pointer parser makes the JSDocTypeLiteral here and leaves it
			// unfinished when it takes the child's type instead; a constructor
			// writes the parent of the property tags, so it is made only when
			// it is used.
			if childTypeTag != store.NoNodeRef && !p.b.View(childTypeTag).AsJSDocTypeTag().TypeExpression().IsNil() && !isObjectOrObjectArrayTypeReference(p.b.View(childTypeTag).AsJSDocTypeTag().TypeExpression().Type()) {
				typeExpression = p.b.View(childTypeTag).AsJSDocTypeTag().TypeExpression().Ref()
				p.unkeptNodes++ // the Pointer's unfinished JSDocTypeLiteral
			} else {
				// !!! This differs from Strada but prevents a crash
				pos := start
				if len(jsdocPropertyTags) > 0 {
					pos = p.pos(jsdocPropertyTags[0])
				}
				typeExpression = p.b.NewJSDocTypeLiteral(p.flags(), int32(pos), int32(p.nodePos()), p.rawList(jsdocPropertyTags), isArrayType)
			}
			end = p.end(typeExpression)
		}
	}

	// Only include the characters between the name end and the next token if a comment was actually parsed out - otherwise it's just whitespace
	if end == -1 {
		if hasChildren && typeExpression != store.NoNodeRef {
			end = p.end(typeExpression)
		} else if comment != store.NoListRef {
			end = p.nodePos()
		} else if fullName != store.NoNodeRef {
			end = p.end(fullName)
		} else if typeExpression != store.NoNodeRef {
			end = p.end(typeExpression)
		} else {
			end = p.end(tagName)
		}
	}

	if comment == store.NoListRef {
		comment = p.parseTrailingTagComments(start, end, indent, indentText)
	}

	// The constructor (re)sets the parent of typeExpression, which an inner type
	// expression parse may have set.
	return p.b.NewJSDocTypedefTag(p.flags(), int32(start), int32(end), tagName, typeExpression, fullName, comment)
}

func (p *Parser) parseCallbackTagParameters(indent int) store.ListRef {
	var child store.NodeRef
	var parameters []store.NodeRef
	pos := p.nodePos()
	for {
		state := p.mark()
		child = p.parseChildParameterOrPropertyTag(propertyLikeParseCallbackParameter, indent, store.NoNodeRef)
		if child == store.NoNodeRef {
			p.rewind(state)
			break
		}
		if p.b.View(child).Kind() == ast.KindJSDocTemplateTag {
			p.parseErrorAtRange(p.b.View(child).TagName().Loc(), diagnostics.A_JSDoc_template_tag_may_not_follow_a_typedef_callback_or_overload_tag)
		} else {
			parameters = append(parameters, child)
		}
	}
	return p.b.List(int32(pos), int32(p.nodePos()), parameters)
}

func (p *Parser) parseJSDocSignature(start int, indent int) store.NodeRef {
	parameters := p.parseCallbackTagParameters(indent)
	var returnTag store.NodeRef
	state := p.mark()
	if p.parseOptionalJsdoc(ast.KindAtToken) {
		tag := p.parseTag(nil, indent)
		if p.b.View(tag).Kind() == ast.KindJSDocReturnTag {
			returnTag = tag
		}
	}
	if returnTag == store.NoNodeRef {
		p.rewind(state)
	}
	return p.b.NewJSDocSignature(p.flags(), int32(start), int32(p.nodePos()), store.NoListRef, parameters, returnTag)
}

func (p *Parser) parseCallbackTag(start int, tagName store.NodeRef, indent int, indentText string) store.NodeRef {
	fullName := p.parseJSDocTypeNameWithNamespace(false /*nested*/)
	if fullName == store.NoNodeRef {
		fullName = p.parseJSDocIdentifierName(diagnostics.Identifier_expected)
	}
	p.skipWhitespace()
	comment := p.parseTagComments(indent, nil)
	typeExpression := p.parseJSDocSignature(p.nodePos(), indent)
	if comment == store.NoListRef {
		comment = p.parseTrailingTagComments(start, p.nodePos(), indent, indentText)
	}
	var end int
	if comment != store.NoListRef {
		end = p.nodePos()
	} else {
		end = p.end(typeExpression)
	}
	return p.b.NewJSDocCallbackTag(p.flags(), int32(start), int32(end), tagName, typeExpression, fullName, comment)
}

func (p *Parser) parseOverloadTag(start int, tagName store.NodeRef, indent int, indentText string) store.NodeRef {
	p.skipWhitespace()
	comment := p.parseTagComments(indent, nil)
	typeExpression := p.parseJSDocSignature(start, indent)
	if comment == store.NoListRef {
		comment = p.parseTrailingTagComments(start, p.nodePos(), indent, indentText)
	}
	var end int
	if comment != store.NoListRef {
		end = p.nodePos()
	} else {
		end = p.end(typeExpression)
	}
	return p.b.NewJSDocOverloadTag(p.flags(), int32(start), int32(end), tagName, typeExpression, comment)
}

func textsEqual(a store.Node, b store.Node) bool {
	for !store.IsIdentifier(a) || !store.IsIdentifier(b) {
		if !store.IsIdentifier(a) && !store.IsIdentifier(b) && a.AsQualifiedName().Right().Text() == b.AsQualifiedName().Right().Text() {
			a = a.AsQualifiedName().Left()
			b = b.AsQualifiedName().Left()
		} else {
			return false
		}
	}
	return a.Text() == b.Text()
}

func (p *Parser) parseChildPropertyTag(indent int) store.NodeRef {
	return p.parseChildParameterOrPropertyTag(propertyLikeParseProperty, indent, store.NoNodeRef)
}

func (p *Parser) parseChildParameterOrPropertyTag(target propertyLikeParse, indent int, name store.NodeRef) store.NodeRef {
	canParseTag := true
	seenAsterisk := false
	for {
		switch p.nextTokenJSDoc() {
		case ast.KindAtToken:
			if canParseTag && p.scanner.CanFollowJSDocAt() {
				child := p.tryParseChildTag(target, indent)
				if child != store.NoNodeRef && name != store.NoNodeRef &&
					(p.b.View(child).Kind() == ast.KindJSDocParameterTag || p.b.View(child).Kind() == ast.KindJSDocPropertyTag) &&
					(store.IsIdentifier(p.b.View(child).Name()) || !textsEqual(p.b.View(name), p.b.View(child).Name().AsQualifiedName().Left())) {
					return store.NoNodeRef
				}
				return child
			}
			seenAsterisk = false
		case ast.KindNewLineTrivia:
			canParseTag = true
			seenAsterisk = false
		case ast.KindAsteriskToken:
			if seenAsterisk {
				canParseTag = false
			}
			seenAsterisk = true
		case ast.KindIdentifier:
			canParseTag = false
		case ast.KindEndOfFile:
			return store.NoNodeRef
		}
	}
}

func (p *Parser) tryParseChildTag(target propertyLikeParse, indent int) store.NodeRef {
	if p.token != ast.KindAtToken {
		panic("should only be called when at @")
	}
	start := p.scanner.TokenFullStart()
	p.nextTokenJSDoc()

	tagName := p.parseJSDocIdentifierName(diagnostics.Identifier_expected)
	indentText := p.skipWhitespaceOrAsterisk()
	var t propertyLikeParse
	switch p.b.View(tagName).Text() {
	case "type":
		if target == propertyLikeParseProperty {
			return p.parseTypeTag(nil, start, tagName, -1, "")
		}
	case "prop", "property":
		t = propertyLikeParseProperty
	case "arg", "argument", "param":
		t = propertyLikeParseParameter | propertyLikeParseCallbackParameter
	case "template":
		return p.parseTemplateTag(start, tagName, indent, indentText)
	case "this":
		return p.parseThisTag(start, tagName, indent, indentText)
	default:
		return store.NoNodeRef
	}
	if (target & t) == 0 {
		return store.NoNodeRef
	}
	return p.parseParameterOrPropertyTag(start, tagName, target, indent)
}

func (p *Parser) parseTemplateTagTypeParameter() store.NodeRef {
	typeParameterPos := p.nodePos()
	isBracketed := p.parseOptionalJsdoc(ast.KindOpenBracketToken)
	if isBracketed {
		p.skipWhitespace()
	}

	modifiers := p.parseModifiersEx(false, true /*permitConstAsModifier*/, false)
	name := p.parseJSDocIdentifierName(diagnostics.Unexpected_token_A_type_parameter_name_was_expected_without_curly_braces)
	var defaultType store.NodeRef
	if isBracketed {
		p.skipWhitespace()
		p.parseExpected(ast.KindEqualsToken)
		saveContextFlags := p.contextFlags
		p.setContextFlags(ast.NodeFlagsJSDoc, true)
		defaultType = p.parseJSDocType()
		p.contextFlags = saveContextFlags
		p.parseExpected(ast.KindCloseBracketToken)
	}

	if nodeIsMissing(p.b.View(name)) {
		return store.NoNodeRef
	}
	return p.b.NewTypeParameterDeclaration(p.flags(), int32(typeParameterPos), int32(p.nodePos()), modifiers, name, store.NoNodeRef /*constraint*/, store.NoNodeRef /*expression*/, defaultType)
}

// The Pointer list is a zero ast.TypeParameterList: its range is 0, 0.
func (p *Parser) parseTemplateTagTypeParameters() store.ListRef {
	var typeParameters []store.NodeRef
	for ok := true; ok; ok = p.parseOptionalJsdoc(ast.KindCommaToken) { // do-while loop
		p.skipWhitespace()
		node := p.parseTemplateTagTypeParameter()
		if node != store.NoNodeRef {
			typeParameters = append(typeParameters, node)
		}
		p.skipWhitespaceOrAsterisk()
	}
	return p.b.List(0, 0, typeParameters)
}

func (p *Parser) parseTemplateTag(start int, tagName store.NodeRef, indent int, indentText string) store.NodeRef {
	// The template tag looks like one of the following:
	//   @template T,U,V
	//   @template {Constraint} T
	//
	// According to the [closure docs](https://github.com/google/closure-compiler/wiki/Generic-Types#multiple-bounded-template-types):
	//   > Multiple bounded generics cannot be declared on the same line. For the sake of clarity, if multiple templates share the same
	//   > type bound they must be declared on separate lines.
	//
	// TODO: Determine whether we should enforce this in the checker.
	// TODO: Consider moving the `constraint` to the first type parameter as we could then remove `getEffectiveConstraintOfTypeParameter`.
	// TODO: Consider only parsing a single type parameter if there is a constraint.
	var constraint store.NodeRef
	if p.token == ast.KindOpenBraceToken {
		constraint = p.parseJSDocTypeExpression(false)
	}
	typeParameters := p.parseTemplateTagTypeParameters()
	comments := p.parseTrailingTagComments(start, p.nodePos(), indent, indentText)
	return p.b.NewJSDocTemplateTag(p.flags(), int32(start), int32(p.nodePos()), tagName, constraint, typeParameters, comments)
}

func (p *Parser) parseOptionalJsdoc(t ast.Kind) bool {
	if p.token == t {
		p.nextTokenJSDoc()
		return true
	}
	return false
}

func (p *Parser) parseJSDocEntityName(diagnosticMessage *diagnostics.Message) store.NodeRef {
	entity := p.parseJSDocIdentifierName(diagnosticMessage)
	if p.parseOptional(ast.KindOpenBracketToken) {
		p.parseExpected(ast.KindCloseBracketToken)
		// Note that y[] is accepted as an entity name, but the postfix brackets are not saved for checking.
		// Technically usejsdoc.org requires them for specifying a property of a type equivalent to Array<{ x: ...}>
		// but it's not worth it to enforce that restriction.
	}
	for p.parseOptional(ast.KindDotToken) {
		name := p.parseJSDocIdentifierName(diagnostics.Identifier_expected)
		if p.parseOptional(ast.KindOpenBracketToken) {
			p.parseExpected(ast.KindCloseBracketToken)
		}
		pos := p.pos(entity)
		entity = p.b.NewQualifiedName(p.flags(), int32(pos), int32(p.nodePos()), entity, name)
	}
	return entity
}

func (p *Parser) parseJSDocIdentifierName(diagnosticMessage *diagnostics.Message) store.NodeRef {
	if !tokenIsIdentifierOrKeyword(p.token) {
		if diagnosticMessage != nil {
			p.parseErrorAtCurrentToken(diagnosticMessage)
		} else if isReservedWord(p.token) {
			p.parseErrorAtCurrentToken(diagnostics.Identifier_expected_0_is_a_reserved_word_that_cannot_be_used_here, p.scanner.TokenText())
		}
		return p.newIdentifier("", p.nodePos(), p.nodePos())
	}
	pos := p.scanner.TokenStart()
	end := p.scanner.TokenEnd()
	text := p.scanner.TokenValue()
	p.nextTokenJSDoc()
	return p.newIdentifier(text, pos, end)
}
