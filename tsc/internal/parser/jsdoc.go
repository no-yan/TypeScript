package parser

import (
	"slices"
	"strings"
	"unicode"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
)

func init() {
	ast.SetParseJSDocForNode(parseJSDocForNode)
}

// parseJSDocForNode lazily parses JSDoc for a node in a TS file. The JSDoc
// nodes are allocated into store (a checker's synth Store or the file's shared
// side Store), never into the parse Store, which is frozen by the time this
// runs. The host node's parent edge is written on store as an externalParent.
func parseJSDocForNode(sourceFile *ast.SourceFile, node ast.Handle, store *ast.Store) []ast.Handle {
	if store == nil || sourceFile.ParseStore() == nil {
		return nil
	}
	p := getParser()
	defer putParser(p)
	p.initializeState(sourceFile.ParseOptions(), sourceFile.Text(), sourceFile.ScriptKind)
	p.factory = ast.NewFactoryOn(store, ast.FactoryHooks{})
	ranges := GetJSDocCommentRanges(nil, node.Kind, node.Pos(), node.End(), sourceFile.Text())
	if len(ranges) == 0 {
		return nil
	}
	jsdoc := make([]ast.Handle, 0, len(ranges))
	pos := node.Pos()
	for _, comment := range ranges {
		if parsed := p.parseJSDocComment(0, comment.Pos(), comment.End(), pos); parsed != 0 {
			h := p.factory.Store().At(parsed)
			h.SetParent(node)
			jsdoc = append(jsdoc, h)
			pos = h.End()
		}
	}
	return jsdoc
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

func (p *Parser) withJSDoc(node ast.NodeRef, info jsdocScannerInfo) []ast.NodeRef {
	if info&jsdocScannerInfoHasJSDoc == 0 {
		return nil
	}

	// For TS/TSX files, defer JSDoc parsing to first access, unless the comment
	// contains @see/@link (needed for unused-identifier checks).
	// @deprecated is detected via cheap text scan to set PossiblyContainsDeprecatedTag;
	// callers must confirm via JSDoc lookup.
	h := p.at(node)
	if !p.isJavaScript() {
		h.SetFlags(h.Flags() | ast.NodeFlagsHasJSDoc)
		if info&jsdocScannerInfoHasDeprecated != 0 {
			h.SetFlags(h.Flags() | ast.NodeFlagsPossiblyContainsDeprecatedTag)
		}
		if info&jsdocScannerInfoHasSeeOrLink == 0 {
			p.lazyJSDoc = append(p.lazyJSDoc, node)
			return nil
		}
		// Fall through to eager parse for @see/@link
	}

	ranges := GetJSDocCommentRanges(p.jsdocCommentRangesSpace, h.Kind, h.Pos(), h.End(), p.sourceText)
	p.jsdocCommentRangesSpace = ranges[:0]

	// Should only be called once per node
	p.hasDeprecatedTag = false
	jsdoc := make([]ast.NodeRef, len(ranges))[:0]
	pos := h.Pos()
	for _, comment := range ranges {
		if parsed := p.parseJSDocComment(node, comment.Pos(), comment.End(), pos); parsed != 0 {
			ph := p.at(parsed)
			ph.SetParent(h)
			jsdoc = append(jsdoc, parsed)
			pos = ph.End()
		}
	}
	if len(jsdoc) != 0 {
		if h.Flags()&ast.NodeFlagsHasJSDoc == 0 {
			h.SetFlags(h.Flags() | ast.NodeFlagsHasJSDoc)
		}
		if p.hasDeprecatedTag {
			p.hasDeprecatedTag = false
			h.SetFlags(h.Flags() | ast.NodeFlagsPossiblyContainsDeprecatedTag)
		}
		if p.isJavaScript() {
			p.reparseTags(node, jsdoc)
		}
		p.jsdocInfos = append(p.jsdocInfos, JSDocInfo{parent: node, jsDocs: jsdoc})
		return jsdoc
	}
	return nil
}

func (p *Parser) parseJSDocTypeExpression(mayOmitBraces bool) ast.NodeRef {
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

	return p.finishParse(p.factory.ParseJSDocTypeExpression(t), pos)
}

func (p *Parser) parseJSDocNameReference() ast.NodeRef {
	pos := p.nodePos()
	hasBrace := p.parseOptional(ast.KindOpenBraceToken)
	entityName := p.parseJSDocLinkName()
	if hasBrace {
		p.parseExpectedJSDoc(ast.KindCloseBraceToken)
	}
	p.scanner.ResetPos(p.scanner.TokenFullStart())
	p.nextTokenJSDoc()
	return p.finishParse(p.factory.ParseJSDocNameReference(entityName), pos)
}

// Pass end=-1 to parse the text to the end
func (p *Parser) parseJSDocComment(parent ast.NodeRef, start int, end int, fullStart int) ast.NodeRef {
	if end == -1 {
		end = len(p.sourceText)
	}
	// Check for /** (JSDoc opening part)
	if !isJSDocLikeText(p.sourceText[start:]) {
		// TODO: This should be a panic, unless parseSingleJSDocComment is calling this (not ported yet)
		return 0
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
func (p *Parser) parseJSDocCommentWorker(start int, end int, fullStart int, indent int) ast.NodeRef {
	// Initially we can parse out a tag.  We also have seen a starting asterisk.
	// This is so that /** * @type */ doesn't parse.
	tags := make([]ast.NodeRef, 1)[:0]
	tagsPos := -1
	tagsEnd := -1
	state := jsdocStateSawAsterisk
	backtickCount := 0
	inFencedCodeBlock := false
	commentParts := make([]ast.NodeRef, 1)[:0]
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
				tagsPos = p.at(tag).Pos()
			}
			tags = append(tags, tag)
			tagsEnd = p.at(tag).End()
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
			if link != 0 {
				if linkEnd == start {
					comments = removeLeadingNewlines(comments)
				}
				jsdocText := p.finishParseWithEnd(p.factory.ParseJSDocText(p.stringSliceArena.Clone(comments)), linkEnd, commentEnd)
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
		jsdocText := p.finishParseWithEnd(p.factory.ParseJSDocText(p.stringSliceArena.Clone(comments)), linkEnd, commentsPos)
		commentParts = append(commentParts, jsdocText)
	}

	if len(commentParts) > 0 && len(tags) > 0 && commentsPos == -1 {
		panic("having parsed tags implies that the end of the comment span should be set")
	}

	var tagsNodeList ast.ListRef
	if tagsPos != -1 {
		tagsNodeList = p.newListRefs(core.NewTextRange(tagsPos, tagsEnd), tags)
	}

	jsdocComment := p.factory.ParseJSDoc(
		p.newListRefs(core.NewTextRange(start, commentsPos), commentParts),
		tagsNodeList,
	)
	return p.finishParseWithEnd(jsdocComment, fullStart, end)
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

func (p *Parser) parseTag(tags []ast.NodeRef, margin int) ast.NodeRef {
	if p.token != ast.KindAtToken {
		panic("should be called only at the start of a tag")
	}
	start := p.scanner.TokenStart()
	p.nextTokenJSDoc()

	tagName := p.parseJSDocIdentifierName(diagnostics.Identifier_expected)
	indentText := p.skipWhitespaceOrAsterisk()

	var tag ast.NodeRef
	switch p.at(tagName).Text() {
	case "implements":
		tag = p.parseImplementsTag(start, tagName, margin, indentText)
	case "augments", "extends":
		tag = p.parseAugmentsTag(start, tagName, margin, indentText)
	case "public":
		tag = p.parseSimpleTag(start, func(tagName ast.NodeRef, comments ast.ListRef) ast.NodeRef {
			return p.factory.ParseJSDocPublicTag(tagName, comments)
		}, tagName, margin, indentText)
	case "private":
		tag = p.parseSimpleTag(start, func(tagName ast.NodeRef, comments ast.ListRef) ast.NodeRef {
			return p.factory.ParseJSDocPrivateTag(tagName, comments)
		}, tagName, margin, indentText)
	case "protected":
		tag = p.parseSimpleTag(start, func(tagName ast.NodeRef, comments ast.ListRef) ast.NodeRef {
			return p.factory.ParseJSDocProtectedTag(tagName, comments)
		}, tagName, margin, indentText)
	case "readonly":
		tag = p.parseSimpleTag(start, func(tagName ast.NodeRef, comments ast.ListRef) ast.NodeRef {
			return p.factory.ParseJSDocReadonlyTag(tagName, comments)
		}, tagName, margin, indentText)
	case "override":
		tag = p.parseSimpleTag(start, func(tagName ast.NodeRef, comments ast.ListRef) ast.NodeRef {
			return p.factory.ParseJSDocOverrideTag(tagName, comments)
		}, tagName, margin, indentText)
	case "deprecated":
		p.hasDeprecatedTag = true
		tag = p.parseSimpleTag(start, func(tagName ast.NodeRef, comments ast.ListRef) ast.NodeRef {
			return p.factory.ParseJSDocDeprecatedTag(tagName, comments)
		}, tagName, margin, indentText)
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
	if tag == 0 {
		panic("tag should not be nil")
	}
	return tag
}

func (p *Parser) parseTrailingTagComments(pos int, end int, margin int, indentText string) ast.ListRef {
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

func (p *Parser) parseTagComments(indent int, initialMargin *string) ast.ListRef {
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
			if link != 0 {
				var commentStart int
				if linkEnd > -1 {
					commentStart = linkEnd
				} else {
					commentStart = commentsPos
				}
				text := p.finishParseWithEnd(p.factory.ParseJSDocText(p.stringSliceArena.Clone(comments)), commentStart, commentEnd)
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
		text := p.finishParse(p.factory.ParseJSDocText(p.stringSliceArena.Clone(comments)), commentStart)
		parts = append(parts, text)
	}

	p.jsdocTagCommentsPartsSpace = parts[:0]

	if len(parts) > 0 {
		return p.newListRefs(core.NewTextRange(commentsPos, p.scanner.TokenEnd()), slices.Clone(parts))
	}
	return 0
}

func (p *Parser) parseJSDocLink(start int) ast.NodeRef {
	state := p.mark()
	linkType, ok := p.parseJSDocLinkPrefix()
	if !ok {
		p.rewind(state)
		return 0
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
	var create ast.NodeRef
	switch linkType {
	case "link":
		create = p.factory.ParseJSDocLink(name, text)
	case "linkcode":
		create = p.factory.ParseJSDocLinkCode(name, text)
	default:
		create = p.factory.ParseJSDocLinkPlain(name, text)
	}
	return p.finishParseWithEnd(create, start, p.scanner.TokenEnd())
}

func (p *Parser) parseJSDocLinkName() ast.NodeRef {
	if tokenIsIdentifierOrKeyword(p.token) {
		pos := p.nodePos()
		name := p.parseIdentifierName()
		for p.parseOptional(ast.KindDotToken) {
			var right ast.NodeRef
			if p.token == ast.KindPrivateIdentifier {
				right = p.createMissingIdentifier()
			} else {
				right = p.parseIdentifierName()
			}
			name = p.finishParse(p.factory.ParseQualifiedName(name, right), pos)
		}
		for p.token == ast.KindPrivateIdentifier {
			p.scanner.ReScanHashToken()
			p.nextTokenJSDoc()
			name = p.finishParse(p.factory.ParseQualifiedName(name, p.parseIdentifier()), pos)
		}
		return name
	}
	return 0
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

func (p *Parser) parseUnknownTag(start int, tagName ast.NodeRef, indent int, indentText string) ast.NodeRef {
	return p.finishParse(p.factory.ParseJSDocUnknownTag(tagName, p.parseTrailingTagComments(start, p.nodePos(), indent, indentText)), start)
}

func (p *Parser) tryParseTypeExpression() ast.NodeRef {
	p.skipWhitespaceOrAsterisk()
	if p.token == ast.KindOpenBraceToken {
		return p.parseJSDocTypeExpression(false /*mayOmitBraces*/)
	} else {
		return 0
	}
}

func (p *Parser) parseBracketNameInPropertyAndParamTag(target propertyLikeParse) (name ast.NodeRef, isBracketed bool) {
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
		if p.parseOptionalToken(ast.KindEqualsToken) != 0 {
			p.parseExpression()
		}

		p.parseExpected(ast.KindCloseBracketToken)
	}

	return name, isBracketed
}

func isObjectOrObjectArrayTypeReference(node ast.Handle) bool {
	switch node.Kind {
	case ast.KindObjectKeyword:
		return true
	case ast.KindArrayType:
		return isObjectOrObjectArrayTypeReference(node.ArrayTypeNodeElementType())
	default:
		if node.Kind == ast.KindTypeReference {
			return node.TypeReferenceNodeTypeName().Kind == ast.KindIdentifier && node.TypeReferenceNodeTypeName().Text() == "Object" && node.TypeReferenceNodeTypeArguments() == 0
		}
		return false
	}
}

func (p *Parser) parseParameterOrPropertyTag(start int, tagName ast.NodeRef, target propertyLikeParse, indent int) ast.NodeRef {
	typeExpression := p.tryParseTypeExpression()
	isNameFirst := typeExpression == 0
	p.skipWhitespaceOrAsterisk()

	name, isBracketed := p.parseBracketNameInPropertyAndParamTag(target)
	indentText := p.skipWhitespaceOrAsterisk()

	if isNameFirst && p.lookAhead(func(p *Parser) bool { _, ok := p.parseJSDocLinkPrefix(); return !ok }) {
		typeExpression = p.tryParseTypeExpression()
	}

	comment := p.parseTrailingTagComments(start, p.nodePos(), indent, indentText)

	nestedTypeLiteral := p.parseNestedTypeLiteral(typeExpression, name, target, indent)
	if nestedTypeLiteral != 0 {
		typeExpression = nestedTypeLiteral
		isNameFirst = true
	}
	var result ast.NodeRef /* JSDocPropertyTag | JSDocParameterTag */
	kind := core.IfElse(target == propertyLikeParseProperty, ast.KindJSDocPropertyTag, ast.KindJSDocParameterTag)
	result = p.factory.ParseJSDocParameterOrPropertyTag(kind, tagName, name, isBracketed, typeExpression, isNameFirst, comment)
	return p.finishParse(result, start)
}

func (p *Parser) parseNestedTypeLiteral(typeExpression ast.NodeRef, name ast.NodeRef, target propertyLikeParse, indent int) ast.NodeRef {
	if typeExpression != 0 && isObjectOrObjectArrayTypeReference(p.at(typeExpression).Type()) {
		pos := p.nodePos()
		var children []ast.NodeRef
		for {
			state := p.mark()
			child := p.parseChildParameterOrPropertyTag(target, indent, name)
			if child == 0 {
				p.rewind(state)
				break
			}
			switch p.factory.Store().KindAt(child) {
			case ast.KindJSDocParameterTag, ast.KindJSDocPropertyTag:
				children = append(children, child)
			case ast.KindJSDocTemplateTag:
				p.parseErrorAtRange(p.at(child).TagName().Loc(), diagnostics.A_JSDoc_template_tag_may_not_follow_a_typedef_callback_or_overload_tag)
			}
		}
		if len(children) != 0 {
			literal := p.finishParse(p.factory.ParseJSDocTypeLiteral(p.newListRefs(core.UndefinedTextRange(), children), p.at(typeExpression).Type().Kind == ast.KindArrayType), pos)
			return p.finishParse(p.factory.ParseJSDocTypeExpression(literal), pos)
		}
	}
	return 0
}

func (p *Parser) parseReturnTag(previousTags []ast.NodeRef, start int, tagName ast.NodeRef, indent int, indentText string) ast.NodeRef {
	if core.Some(previousTags, func(h ast.NodeRef) bool { return p.factory.Store().KindAt(h) == ast.KindJSDocReturnTag }) {
		p.parseErrorAt(p.at(tagName).Pos(), p.scanner.TokenStart(), diagnostics.X_0_tag_already_specified, p.at(tagName).Text())
	}

	typeExpression := p.tryParseTypeExpression()
	return p.finishParse(p.factory.ParseJSDocReturnTag(tagName, typeExpression, p.parseTrailingTagComments(start, p.nodePos(), indent, indentText)), start)
}

// pass indent=-1 to skip parsing trailing comments (as when a type tag is nested in a typedef)
func (p *Parser) parseTypeTag(previousTags []ast.NodeRef, start int, tagName ast.NodeRef, indent int, indentText string) ast.NodeRef {
	if core.Some(previousTags, func(h ast.NodeRef) bool { return p.factory.Store().KindAt(h) == ast.KindJSDocTypeTag }) {
		p.parseErrorAt(p.at(tagName).Pos(), p.scanner.TokenStart(), diagnostics.X_0_tag_already_specified, p.at(tagName).Text())
	}

	typeExpression := p.parseJSDocTypeExpression(true)
	var comments ast.ListRef
	if indent != -1 {
		comments = p.parseTrailingTagComments(start, p.nodePos(), indent, indentText)
	}
	return p.finishParse(p.factory.ParseJSDocTypeTag(tagName, typeExpression, comments), start)
}

func (p *Parser) parseSeeTag(start int, tagName ast.NodeRef, indent int, indentText string) ast.NodeRef {
	hasNameReference := p.isIdentifier() && !strings.HasPrefix(p.sourceText[p.scanner.TokenEnd():], "://") ||
		p.token == ast.KindOpenBraceToken && p.lookAhead((*Parser).nextTokenIsIdentifierOrKeyword)
	var nameExpression ast.NodeRef
	if hasNameReference {
		nameExpression = p.parseJSDocNameReference()
	}
	comments := p.parseTrailingTagComments(start, p.nodePos(), indent, indentText)
	return p.finishParse(p.factory.ParseJSDocSeeTag(tagName, nameExpression, comments), start)
}

func (p *Parser) parseImplementsTag(start int, tagName ast.NodeRef, margin int, indentText string) ast.NodeRef {
	className := p.parseExpressionWithTypeArgumentsForAugments()
	return p.finishParse(p.factory.ParseJSDocImplementsTag(tagName, className, p.parseTrailingTagComments(start, p.nodePos(), margin, indentText)), start)
}

func (p *Parser) parseAugmentsTag(start int, tagName ast.NodeRef, margin int, indentText string) ast.NodeRef {
	className := p.parseExpressionWithTypeArgumentsForAugments()
	return p.finishParse(p.factory.ParseJSDocAugmentsTag(tagName, className, p.parseTrailingTagComments(start, p.nodePos(), margin, indentText)), start)
}

func (p *Parser) parseSatisfiesTag(start int, tagName ast.NodeRef, margin int, indentText string) ast.NodeRef {
	typeExpression := p.parseJSDocTypeExpression(false)
	comments := p.parseTrailingTagComments(start, p.nodePos(), margin, indentText)
	return p.finishParse(p.factory.ParseJSDocSatisfiesTag(tagName, typeExpression, comments), start)
}

func (p *Parser) parseThrowsTag(start int, tagName ast.NodeRef, margin int, indentText string) ast.NodeRef {
	typeExpression := p.tryParseTypeExpression()
	comment := p.parseTrailingTagComments(start, p.nodePos(), margin, indentText)
	return p.finishParse(p.factory.ParseJSDocThrowsTag(tagName, typeExpression, comment), start)
}

func (p *Parser) parseImportTag(start int, tagName ast.NodeRef, margin int, indentText string) ast.NodeRef {
	afterImportTagPos := p.scanner.TokenFullStart()

	var identifier ast.NodeRef
	if p.isIdentifier() {
		identifier = p.parseIdentifier()
	}

	importClause := p.tryParseImportClause(identifier, afterImportTagPos, ast.KindTypeKeyword, true /*skipJSDocLeadingAsterisks*/)
	moduleSpecifier := p.parseModuleSpecifier()
	attributes := p.tryParseImportAttributes()

	comments := p.parseTrailingTagComments(start, p.nodePos(), margin, indentText)
	return p.finishParse(p.factory.ParseJSDocImportTag(tagName, importClause, moduleSpecifier, attributes, comments), start)
}

func (p *Parser) parseExpressionWithTypeArgumentsForAugments() ast.NodeRef {
	usedBrace := p.parseOptional(ast.KindOpenBraceToken)
	pos := p.nodePos()
	expression := p.parsePropertyAccessEntityNameExpression()
	p.scanner.SetSkipJSDocLeadingAsterisks(true)
	typeArguments := p.parseTypeArguments()
	p.scanner.SetSkipJSDocLeadingAsterisks(false)
	node := p.finishParse(p.factory.ParseExpressionWithTypeArguments(expression, typeArguments), pos)
	if usedBrace {
		p.skipWhitespace()
		p.parseExpected(ast.KindCloseBraceToken)
	}
	return node
}

func (p *Parser) parsePropertyAccessEntityNameExpression() ast.NodeRef {
	pos := p.nodePos()
	node := p.parseJSDocIdentifierName(diagnostics.Identifier_expected)
	for p.parseOptional(ast.KindDotToken) {
		name := p.parseJSDocIdentifierName(diagnostics.Identifier_expected)
		node = p.finishParse(p.factory.ParsePropertyAccessExpression(node, ast.NoNodeRef, name, ast.NodeFlagsNone), pos)
	}
	return node
}

func (p *Parser) parseSimpleTag(start int, createTag func(tagName ast.NodeRef, comment ast.ListRef) ast.NodeRef, tagName ast.NodeRef, margin int, indentText string) ast.NodeRef {
	return p.finishParse(createTag(tagName, p.parseTrailingTagComments(start, p.nodePos(), margin, indentText)), start)
}

func (p *Parser) parseThisTag(start int, tagName ast.NodeRef, margin int, indentText string) ast.NodeRef {
	typeExpression := p.parseJSDocTypeExpression(true)
	p.skipWhitespace()
	result := p.factory.ParseJSDocThisTag(tagName, typeExpression, p.parseTrailingTagComments(start, p.nodePos(), margin, indentText))
	return p.finishParse(result, start)
}

func (p *Parser) parseJSDocTypeNameWithNamespace(nested bool) ast.NodeRef {
	start := p.scanner.TokenStart()
	if !tokenIsIdentifierOrKeyword(p.token) {
		return 0
	}
	typeNameOrNamespaceName := p.parseJSDocIdentifierName(nil)
	if p.parseOptionalJsdoc(ast.KindDotToken) {
		body := p.parseJSDocTypeNameWithNamespace(true /*nested*/)
		jsDocNamespaceNode := p.factory.ParseModuleDeclaration(
			0,
			ast.KindNamespaceKeyword, /*keyword*/
			typeNameOrNamespaceName,
			body,
		)
		if nested {
			p.at(jsDocNamespaceNode).SetFlags(p.at(jsDocNamespaceNode).Flags() | ast.NodeFlagsNestedNamespace)
		}
		return p.finishParse(jsDocNamespaceNode, start)
	}
	if nested {
		p.at(typeNameOrNamespaceName).SetFlags(p.at(typeNameOrNamespaceName).Flags() | ast.NodeFlagsIdentifierIsInJSDocNamespace)
	}
	return typeNameOrNamespaceName
}

func (p *Parser) parseTypedefTag(start int, tagName ast.NodeRef, indent int, indentText string) ast.NodeRef {
	typeExpression := p.tryParseTypeExpression()
	p.skipWhitespaceOrAsterisk()
	fullName := p.parseJSDocTypeNameWithNamespace(false /*nested*/)
	if fullName == 0 {
		fullName = p.parseJSDocIdentifierName(diagnostics.Identifier_expected)
	}
	p.skipWhitespace()
	comment := p.parseTagComments(indent, nil)

	end := -1
	hasChildren := false
	if typeExpression == 0 || isObjectOrObjectArrayTypeReference(p.at(typeExpression).Type()) {
		var child ast.NodeRef
		var childTypeTag ast.NodeRef
		var jsdocPropertyTags []ast.NodeRef
		for {
			state := p.mark()
			child = p.parseChildPropertyTag(indent)
			if child == 0 {
				p.rewind(state)
				break
			}
			hasChildren = true
			switch p.factory.Store().KindAt(child) {
			case ast.KindJSDocTemplateTag:
				p.parseErrorAtRange(p.at(child).TagName().Loc(), diagnostics.A_JSDoc_template_tag_may_not_follow_a_typedef_callback_or_overload_tag)
			case ast.KindJSDocTypeTag:
				if childTypeTag == 0 {
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
			isArrayType := typeExpression != 0 && p.at(typeExpression).Type().Kind == ast.KindArrayType
			jsdocTypeLiteral := p.factory.ParseJSDocTypeLiteral(p.newListRefs(core.UndefinedTextRange(), jsdocPropertyTags), isArrayType)
			if childTypeTag != 0 && !p.at(childTypeTag).JSDocTypeTagTypeExpression().IsNil() && !isObjectOrObjectArrayTypeReference(p.at(childTypeTag).JSDocTypeTagTypeExpression().Type()) {
				typeExpression = p.at(childTypeTag).JSDocTypeTagTypeExpression().Ref()
			} else {
				// !!! This differs from Strada but prevents a crash
				pos := start
				if len(jsdocPropertyTags) > 0 {
					pos = p.at(jsdocPropertyTags[0]).Pos()
				}
				typeExpression = p.finishParse(jsdocTypeLiteral, pos)
			}
			end = p.at(typeExpression).End()
		}
	}

	// Only include the characters between the name end and the next token if a comment was actually parsed out - otherwise it's just whitespace
	if end == -1 {
		if hasChildren && typeExpression != 0 {
			end = p.at(typeExpression).End()
		} else if comment != 0 {
			end = p.nodePos()
		} else if fullName != 0 {
			end = p.at(fullName).End()
		} else if typeExpression != 0 {
			end = p.at(typeExpression).End()
		} else {
			end = p.at(tagName).End()
		}
	}

	if comment == 0 {
		comment = p.parseTrailingTagComments(start, end, indent, indentText)
	}

	typedefTag := p.finishParseWithEnd(p.factory.ParseJSDocTypedefTag(tagName, typeExpression, fullName, comment), start, end)
	if typeExpression != 0 {
		p.at(typeExpression).SetParent(p.at(typedefTag))
	}
	return typedefTag
}

func (p *Parser) parseCallbackTagParameters(indent int) ast.ListRef {
	var child ast.NodeRef
	var parameters []ast.NodeRef
	pos := p.nodePos()
	for {
		state := p.mark()
		child = p.parseChildParameterOrPropertyTag(propertyLikeParseCallbackParameter, indent, 0)
		if child == 0 {
			p.rewind(state)
			break
		}
		if p.factory.Store().KindAt(child) == ast.KindJSDocTemplateTag {
			p.parseErrorAtRange(p.at(child).TagName().Loc(), diagnostics.A_JSDoc_template_tag_may_not_follow_a_typedef_callback_or_overload_tag)
		} else {
			parameters = append(parameters, child)
		}
	}
	return p.newListRefs(core.NewTextRange(pos, p.nodePos()), parameters)
}

func (p *Parser) parseJSDocSignature(start int, indent int) ast.NodeRef {
	parameters := p.parseCallbackTagParameters(indent)
	var returnTag ast.NodeRef
	state := p.mark()
	if p.parseOptionalJsdoc(ast.KindAtToken) {
		tag := p.parseTag(nil, indent)
		if p.factory.Store().KindAt(tag) == ast.KindJSDocReturnTag {
			returnTag = tag
		}
	}
	if returnTag == 0 {
		p.rewind(state)
	}
	return p.finishParse(p.factory.ParseJSDocSignature(ast.NoListRef, parameters, returnTag), start)
}

func (p *Parser) parseCallbackTag(start int, tagName ast.NodeRef, indent int, indentText string) ast.NodeRef {
	fullName := p.parseJSDocTypeNameWithNamespace(false /*nested*/)
	if fullName == 0 {
		fullName = p.parseJSDocIdentifierName(diagnostics.Identifier_expected)
	}
	p.skipWhitespace()
	comment := p.parseTagComments(indent, nil)
	typeExpression := p.parseJSDocSignature(p.nodePos(), indent)
	if comment == 0 {
		comment = p.parseTrailingTagComments(start, p.nodePos(), indent, indentText)
	}
	var end int
	if comment != 0 {
		end = p.nodePos()
	} else {
		end = p.at(typeExpression).End()
	}
	return p.finishParseWithEnd(p.factory.ParseJSDocCallbackTag(tagName, typeExpression, fullName, comment), start, end)
}

func (p *Parser) parseOverloadTag(start int, tagName ast.NodeRef, indent int, indentText string) ast.NodeRef {
	p.skipWhitespace()
	comment := p.parseTagComments(indent, nil)
	typeExpression := p.parseJSDocSignature(start, indent)
	if comment == 0 {
		comment = p.parseTrailingTagComments(start, p.nodePos(), indent, indentText)
	}
	var end int
	if comment != 0 {
		end = p.nodePos()
	} else {
		end = p.at(typeExpression).End()
	}
	return p.finishParseWithEnd(p.factory.ParseJSDocOverloadTag(tagName, typeExpression, comment), start, end)
}

func textsEqual(a ast.Handle, b ast.Handle) bool {
	for a.Kind != ast.KindIdentifier || b.Kind != ast.KindIdentifier {
		if a.Kind != ast.KindIdentifier && b.Kind != ast.KindIdentifier && a.QualifiedNameRight().Text() == b.QualifiedNameRight().Text() {
			a = a.QualifiedNameLeft()
			b = b.QualifiedNameLeft()
		} else {
			return false
		}
	}
	return a.Text() == b.Text()
}

func (p *Parser) parseChildPropertyTag(indent int) ast.NodeRef {
	return p.parseChildParameterOrPropertyTag(propertyLikeParseProperty, indent, 0)
}

func (p *Parser) parseChildParameterOrPropertyTag(target propertyLikeParse, indent int, name ast.NodeRef) ast.NodeRef {
	canParseTag := true
	seenAsterisk := false
	for {
		switch p.nextTokenJSDoc() {
		case ast.KindAtToken:
			if canParseTag && p.scanner.CanFollowJSDocAt() {
				child := p.tryParseChildTag(target, indent)
				if child != 0 && name != 0 &&
					(p.factory.Store().KindAt(child) == ast.KindJSDocParameterTag || p.factory.Store().KindAt(child) == ast.KindJSDocPropertyTag) &&
					(p.at(child).Name().Kind == ast.KindIdentifier || !textsEqual(p.at(name), p.at(child).Name().QualifiedNameLeft())) {
					return 0
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
			return 0
		}
	}
}

func (p *Parser) tryParseChildTag(target propertyLikeParse, indent int) ast.NodeRef {
	if p.token != ast.KindAtToken {
		panic("should only be called when at @")
	}
	start := p.scanner.TokenFullStart()
	p.nextTokenJSDoc()

	tagName := p.parseJSDocIdentifierName(diagnostics.Identifier_expected)
	indentText := p.skipWhitespaceOrAsterisk()
	var t propertyLikeParse
	switch p.at(tagName).Text() {
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
		return 0
	}
	if (target & t) == 0 {
		return 0
	}
	return p.parseParameterOrPropertyTag(start, tagName, target, indent)
}

func (p *Parser) parseTemplateTagTypeParameter() ast.NodeRef {
	typeParameterPos := p.nodePos()
	isBracketed := p.parseOptionalJsdoc(ast.KindOpenBracketToken)
	if isBracketed {
		p.skipWhitespace()
	}

	modifiers := p.parseModifiersEx(false, true /*permitConstAsModifier*/, false)
	name := p.parseJSDocIdentifierName(diagnostics.Unexpected_token_A_type_parameter_name_was_expected_without_curly_braces)
	var defaultType ast.NodeRef
	if isBracketed {
		p.skipWhitespace()
		p.parseExpected(ast.KindEqualsToken)
		saveContextFlags := p.contextFlags
		p.setContextFlags(ast.NodeFlagsJSDoc, true)
		defaultType = p.parseJSDocType()
		p.contextFlags = saveContextFlags
		p.parseExpected(ast.KindCloseBracketToken)
	}

	if ast.NodeIsMissing(p.at(name)) {
		return 0
	}
	return p.finishParse(p.factory.ParseTypeParameterDeclaration(modifiers, name, ast.NoNodeRef, ast.NoNodeRef, defaultType), typeParameterPos)
}

func (p *Parser) parseTemplateTagTypeParameters() ast.ListRef {
	pos := p.nodePos()
	var typeParameters []ast.NodeRef
	for ok := true; ok; ok = p.parseOptionalJsdoc(ast.KindCommaToken) {
		p.skipWhitespace()
		node := p.parseTemplateTagTypeParameter()
		if node != 0 {
			typeParameters = append(typeParameters, node)
		}
		p.skipWhitespaceOrAsterisk()
	}
	return p.newListRefs(core.NewTextRange(pos, p.nodePos()), typeParameters)
}

func (p *Parser) parseTemplateTag(start int, tagName ast.NodeRef, indent int, indentText string) ast.NodeRef {
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
	var constraint ast.NodeRef
	if p.token == ast.KindOpenBraceToken {
		constraint = p.parseJSDocTypeExpression(false)
	}
	typeParameters := p.parseTemplateTagTypeParameters()
	result := p.factory.ParseJSDocTemplateTag(tagName, constraint, typeParameters, p.parseTrailingTagComments(start, p.nodePos(), indent, indentText))
	return p.finishParse(result, start)
}

func (p *Parser) parseOptionalJsdoc(t ast.Kind) bool {
	if p.token == t {
		p.nextTokenJSDoc()
		return true
	}
	return false
}

func (p *Parser) parseJSDocEntityName(diagnosticMessage *diagnostics.Message) ast.NodeRef {
	var entity ast.NodeRef = p.parseJSDocIdentifierName(diagnosticMessage)
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
		pos := p.at(entity).Pos()
		entity = p.finishParse(p.factory.ParseQualifiedName(entity, name), pos)
	}
	return entity
}

func (p *Parser) parseJSDocIdentifierName(diagnosticMessage *diagnostics.Message) ast.NodeRef {
	if !tokenIsIdentifierOrKeyword(p.token) {
		if diagnosticMessage != nil {
			p.parseErrorAtCurrentToken(diagnosticMessage)
		} else if isReservedWord(p.token) {
			p.parseErrorAtCurrentToken(diagnostics.Identifier_expected_0_is_a_reserved_word_that_cannot_be_used_here, p.scanner.TokenText())
		}
		return p.finishParse(p.newIdentifier(""), p.nodePos())
	}
	pos := p.scanner.TokenStart()
	end := p.scanner.TokenEnd()
	text := p.scanner.TokenValue()
	p.nextTokenJSDoc()
	return p.finishParseWithEnd(p.newIdentifier(text), pos, end)
}
