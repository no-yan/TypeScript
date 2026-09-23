package storeparser

import (
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

type ParsingContext int

const (
	PCSourceElements           ParsingContext = iota // Elements in source file
	PCBlockStatements                                // Statements in block
	PCSwitchClauses                                  // Clauses in switch statement
	PCSwitchClauseStatements                         // Statements in switch clause
	PCTypeMembers                                    // Members in interface or type literal
	PCClassMembers                                   // Members in class declaration
	PCEnumMembers                                    // Members in enum declaration
	PCHeritageClauseElement                          // Elements in a heritage clause
	PCVariableDeclarations                           // Variable declarations in variable statement
	PCObjectBindingElements                          // Binding elements in object binding list
	PCArrayBindingElements                           // Binding elements in array binding list
	PCArgumentExpressions                            // Expressions in argument list
	PCObjectLiteralMembers                           // Members in object literal
	PCJsxAttributes                                  // Attributes in jsx element
	PCJsxChildren                                    // Things between opening and closing JSX tags
	PCArrayLiteralMembers                            // Members in array literal
	PCParameters                                     // Parameters in parameter list
	PCJSDocParameters                                // JSDoc parameters in parameter list of JSDoc function type
	PCRestProperties                                 // Property names in a rest type list
	PCTypeParameters                                 // Type parameters in type parameter list
	PCTypeArguments                                  // Type arguments in type argument list
	PCTupleElementTypes                              // Element types in tuple element type list
	PCHeritageClauses                                // Heritage clauses for a class or interface declaration.
	PCImportOrExportSpecifiers                       // Named import clause's import specifier list
	PCImportAttributes                               // Import attributes
	PCJSDocComment                                   // Parsing via JSDocParser
	PCCount                                          // Number of parsing contexts
)

type ParsingContexts int

type jsdocScannerInfo uint8

const (
	jsdocScannerInfoHasJSDoc jsdocScannerInfo = 1 << iota
	jsdocScannerInfoHasDeprecated
	jsdocScannerInfoHasSeeOrLink
)

type Parser struct {
	scanner *scanner.Scanner
	b       *store.Builder
	// pragmaFactory only makes the CommentRange values that
	// scanner.GetLeadingCommentRanges hands to getCommentPragmas (see doc.go).
	pragmaFactory ast.NodeFactory

	opts       ast.SourceFileParseOptions
	sourceText string

	scriptKind      core.ScriptKind
	languageVariant core.LanguageVariant
	diagnostics     []*ast.Diagnostic
	jsDiagnostics   []*ast.Diagnostic

	token                       ast.Kind
	sourceFlags                 ast.NodeFlags
	contextFlags                ast.NodeFlags
	parsingContexts             ParsingContexts
	statementHasAwaitIdentifier bool
	hasParseError               bool

	identifierCount       int
	notParenthesizedArrow collections.Set[int]
	possibleAwaitSpans    []int
	// elems is the LIFO scratch of the lists under construction: a list pushes
	// its elements and pops them once Builder.List has copied them.
	elems []store.NodeRef
	// missingLists are the blocks made by createMissingList, in increasing
	// order; rewind pops the ones made after its mark.
	missingLists []store.ListRef
}

func newParser() *Parser {
	return &Parser{}
}

var viableKeywordSuggestions = scanner.GetViableKeywordSuggestions()

// isMissingNodeList distinguishes a "missing" node list (where the expected
// opening token was not found) from an ordinary empty node list. The Pointer
// parser marks it with a sentinel backing array; here the block is recorded
// in missingLists.
func (p *Parser) isMissingNodeList(list store.ListRef) bool {
	return list != store.NoListRef && slices.Contains(p.missingLists, list)
}

var parserPool = sync.Pool{
	New: func() any {
		return newParser()
	},
}

func getParser() *Parser {
	return parserPool.Get().(*Parser)
}

func putParser(p *Parser) {
	*p = Parser{scanner: p.scanner, b: p.b, elems: p.elems[:0], missingLists: p.missingLists[:0]}
	parserPool.Put(p)
}

func ParseSourceFile(opts ast.SourceFileParseOptions, sourceText string, scriptKind core.ScriptKind) *store.File {
	p := getParser()
	defer putParser(p)
	p.initializeState(opts, sourceText, scriptKind)
	p.nextToken()
	if p.scriptKind == core.ScriptKindJSON {
		return p.parseJSONText()
	}
	return p.parseSourceFileWorker()
}

func (p *Parser) isJavaScript() bool {
	return p.scriptKind == core.ScriptKindJS || p.scriptKind == core.ScriptKindJSX
}

func (p *Parser) parseJSONText() *store.File {
	pos := p.nodePos()
	var statements store.ListRef
	var eof store.NodeRef

	if p.token == ast.KindEndOfFile {
		statements = p.b.List(int32(pos), int32(p.nodePos()), nil)
		eof = p.parseTokenNode()
	} else {
		var expressions any // []store.NodeRef | store.NodeRef

		for p.token != ast.KindEndOfFile {
			var expression store.NodeRef
			switch p.token {
			case ast.KindOpenBracketToken:
				expression = p.parseArrayLiteralExpression()
			case ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword:
				expression = p.parseTokenNode()
			case ast.KindMinusToken:
				if p.lookAhead(func(p *Parser) bool {
					return p.nextToken() == ast.KindNumericLiteral && p.nextToken() != ast.KindColonToken
				}) {
					expression = p.parsePrefixUnaryExpression()
				} else {
					expression = p.parseObjectLiteralExpression()
				}
			case ast.KindNumericLiteral, ast.KindStringLiteral:
				if p.lookAhead(func(p *Parser) bool { return p.nextToken() != ast.KindColonToken }) {
					expression = p.parseLiteralExpression()
					break
				}
				fallthrough
			default:
				expression = p.parseObjectLiteralExpression()
			}

			// Error recovery: collect multiple top-level expressions
			if expressions != nil {
				if es, ok := expressions.([]store.NodeRef); ok {
					expressions = append(es, expression)
				} else {
					expressions = []store.NodeRef{expressions.(store.NodeRef), expression}
				}
			} else {
				expressions = expression
				if p.token != ast.KindEndOfFile {
					p.parseErrorAtCurrentToken(diagnostics.Unexpected_token)
				}
			}
		}

		var expression store.NodeRef
		if es, ok := expressions.([]store.NodeRef); ok {
			expression = p.b.NewArrayLiteralExpression(p.flags(), int32(pos), int32(p.nodePos()), p.b.List(int32(pos), int32(p.nodePos()), es), false)
		} else {
			expression = expressions.(store.NodeRef)
		}
		statement := p.b.NewExpressionStatement(p.flags(), int32(pos), int32(p.nodePos()), expression)
		statements = p.b.List(int32(pos), int32(p.nodePos()), []store.NodeRef{statement})
		eof = p.parseExpectedToken(ast.KindEndOfFile)
	}
	node := p.b.NewSourceFile(p.flags(), int32(pos), int32(p.nodePos()), statements, eof)
	if p.b.ViewList(statements).Len() > 0 {
		p.validateJsonValue(p.b.ViewList(statements).At(0).Expression())
	}
	result := p.finishSourceFile(node, false)
	result.Store = p.b.Finish()
	return result
}

func getErrorSpanForNode(sourceText string, node store.Node) core.TextRange {
	pos := int(node.Pos())
	if !nodeIsMissing(node) {
		pos = scanner.SkipTrivia(sourceText, pos)
	}
	return core.NewTextRange(pos, int(node.End()))
}

// The JSON validation reads a finished tree and only appends diagnostics, so
// the views stay valid throughout.
func (p *Parser) validateJsonValue(valueExpression store.Node) {
	if valueExpression.IsNil() {
		return
	}
	switch valueExpression.Kind() {
	case ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword, ast.KindNumericLiteral:
		return
	case ast.KindStringLiteral:
		if !isDoubleQuotedString(valueExpression) {
			p.diagnostics = append(p.diagnostics, ast.NewDiagnostic(nil, getErrorSpanForNode(p.sourceText, valueExpression), diagnostics.String_literal_with_double_quotes_expected))
		}
		return
	case ast.KindPrefixUnaryExpression:
		if valueExpression.AsPrefixUnaryExpression().Operator() != ast.KindMinusToken || valueExpression.AsPrefixUnaryExpression().Operand().Kind() != ast.KindNumericLiteral {
			break // not valid JSON syntax
		}
		return
	case ast.KindObjectLiteralExpression:
		p.validateJsonObjectLiteral(valueExpression.AsObjectLiteralExpression())
		return
	case ast.KindArrayLiteralExpression:
		elements := valueExpression.Elements()
		for i := range elements.Len() {
			p.validateJsonValue(elements.At(i))
		}
		return
	}
	p.diagnostics = append(p.diagnostics, ast.NewDiagnostic(nil, getErrorSpanForNode(p.sourceText, valueExpression), diagnostics.Property_value_can_only_be_string_literal_numeric_literal_true_false_null_object_literal_or_array_literal))
}

func isDoubleQuotedString(node store.Node) bool {
	return node.Kind() == ast.KindStringLiteral && node.AsStringLiteral().TokenFlags()&ast.TokenFlagsSingleQuote == 0
}

// validateJsonObjectLiteral validates properties of a JSON object literal.
func (p *Parser) validateJsonObjectLiteral(node store.ObjectLiteralExpression) {
	properties := node.Properties()
	for i := range properties.Len() {
		element := properties.At(i)
		if element.Kind() != ast.KindPropertyAssignment {
			p.diagnostics = append(p.diagnostics, ast.NewDiagnostic(nil, getErrorSpanForNode(p.sourceText, element), diagnostics.Property_assignment_expected))
			continue
		}
		if !element.Name().IsNil() && !isDoubleQuotedString(element.Name()) {
			p.diagnostics = append(p.diagnostics, ast.NewDiagnostic(nil, getErrorSpanForNode(p.sourceText, element.Name()), diagnostics.String_literal_with_double_quotes_expected))
		}
		p.validateJsonValue(element.AsPropertyAssignment().Initializer())
	}
}

// ParseIsolatedEntityName is not ported: a NodeRef is meaningless without its
// Store, and nothing in 7b needs the entry point.

func (p *Parser) initializeState(opts ast.SourceFileParseOptions, sourceText string, scriptKind core.ScriptKind) {
	if scriptKind == core.ScriptKindUnknown {
		panic("ScriptKind must be specified when parsing source file: " + opts.FileName)
	}

	if p.scanner == nil {
		p.scanner = scanner.NewScanner()
	} else {
		p.scanner.Reset()
	}
	if p.b == nil {
		p.b = store.NewBuilder(sourceText, 0)
	} else {
		p.b.Reset(sourceText, 0)
	}
	p.opts = opts
	p.sourceText = sourceText
	p.scriptKind = scriptKind
	p.languageVariant = getLanguageVariant(p.scriptKind)
	switch p.scriptKind {
	case core.ScriptKindJS, core.ScriptKindJSX:
		p.contextFlags = ast.NodeFlagsJavaScriptFile
	case core.ScriptKindJSON:
		p.contextFlags = ast.NodeFlagsJavaScriptFile | ast.NodeFlagsJsonFile
	default:
		p.contextFlags = ast.NodeFlagsNone
	}
	p.scanner.SetText(p.sourceText)
	p.scanner.SetOnError(p.scanError)
	p.scanner.SetLanguageVariant(p.languageVariant)
}

func (p *Parser) scanError(message *diagnostics.Message, pos int, length int, args ...any) {
	p.parseErrorAtRange(core.NewTextRange(pos, pos+length), message, args...)
}

func (p *Parser) parseErrorAt(pos int, end int, message *diagnostics.Message, args ...any) *ast.Diagnostic {
	return p.parseErrorAtRange(core.NewTextRange(pos, end), message, args...)
}

func (p *Parser) parseErrorAtCurrentToken(message *diagnostics.Message, args ...any) *ast.Diagnostic {
	return p.parseErrorAtRange(p.scanner.TokenRange(), message, args...)
}

func (p *Parser) parseErrorAtRange(loc core.TextRange, message *diagnostics.Message, args ...any) *ast.Diagnostic {
	// Don't report another error if it would just be at the same location as the last error
	var result *ast.Diagnostic
	if len(p.diagnostics) == 0 || p.diagnostics[len(p.diagnostics)-1].Pos() != loc.Pos() {
		result = ast.NewDiagnostic(nil, loc, message, args...)
		p.diagnostics = append(p.diagnostics, result)
	}
	p.hasParseError = true
	return result
}

type ParserState struct {
	scannerState                scanner.ScannerState
	contextFlags                ast.NodeFlags
	diagnosticsLen              int
	jsDiagnosticsLen            int
	nodes, extra                int // Builder.Mark
	statementHasAwaitIdentifier bool
	hasParseError               bool
}

func (p *Parser) mark() ParserState {
	nodes, extra := p.b.Mark()
	return ParserState{
		scannerState:                p.scanner.Mark(),
		contextFlags:                p.contextFlags,
		diagnosticsLen:              len(p.diagnostics),
		jsDiagnosticsLen:            len(p.jsDiagnostics),
		nodes:                       nodes,
		extra:                       extra,
		statementHasAwaitIdentifier: p.statementHasAwaitIdentifier,
		hasParseError:               p.hasParseError,
	}
}

func (p *Parser) rewind(state ParserState) {
	p.scanner.Rewind(state.scannerState)
	p.token = p.scanner.Token()
	p.contextFlags = state.contextFlags
	p.diagnostics = p.diagnostics[0:state.diagnosticsLen]
	p.jsDiagnostics = p.jsDiagnostics[0:state.jsDiagnosticsLen]
	p.b.Truncate(state.nodes, state.extra)
	for len(p.missingLists) != 0 && int(p.missingLists[len(p.missingLists)-1]) >= state.extra {
		p.missingLists = p.missingLists[:len(p.missingLists)-1]
	}
	p.statementHasAwaitIdentifier = state.statementHasAwaitIdentifier
	p.hasParseError = state.hasParseError
}

func (p *Parser) lookAhead(callback func(p *Parser) bool) bool {
	state := p.mark()
	result := callback(p)
	p.rewind(state)
	return result
}

func (p *Parser) nextToken() ast.Kind {
	// if the keyword had an escape
	if ast.IsKeyword(p.token) && (p.scanner.HasUnicodeEscape() || p.scanner.HasExtendedUnicodeEscape()) {
		// issue a parse error for the escape
		p.parseErrorAtCurrentToken(diagnostics.Keywords_cannot_contain_escape_characters)
	}
	p.token = p.scanner.Scan()
	return p.token
}

func (p *Parser) nextTokenWithoutCheck() ast.Kind {
	p.token = p.scanner.Scan()
	return p.token
}

func (p *Parser) nextTokenJSDoc() ast.Kind {
	p.token = p.scanner.ScanJSDocToken()
	return p.token
}

func (p *Parser) nextJSDocCommentTextToken(inBackticks bool) ast.Kind {
	p.token = p.scanner.ScanJSDocCommentTextToken(inBackticks)
	return p.token
}

func (p *Parser) nodePos() int {
	return p.scanner.TokenFullStart()
}

func (p *Parser) hasPrecedingLineBreak() bool {
	return p.scanner.HasPrecedingLineBreak()
}

func (p *Parser) jsdocScannerInfo() jsdocScannerInfo {
	if !p.scanner.HasPrecedingJSDocComment() {
		return 0
	}
	info := jsdocScannerInfoHasJSDoc
	if p.scanner.HasPrecedingJSDocWithDeprecatedTag() {
		info |= jsdocScannerInfoHasDeprecated
	}
	if p.scanner.HasPrecedingJSDocWithSeeOrLink() {
		info |= jsdocScannerInfoHasSeeOrLink
	}
	return info
}

// jsdocFlags is what withJSDoc sets on a node in a TS file: the flags from
// the scanner's cheap scan. No JSDoc node is parsed in 7b, in any file.
func (p *Parser) jsdocFlags(info jsdocScannerInfo) ast.NodeFlags {
	if info&jsdocScannerInfoHasJSDoc == 0 {
		return ast.NodeFlagsNone
	}
	flags := ast.NodeFlagsHasJSDoc
	if info&jsdocScannerInfoHasDeprecated != 0 {
		flags |= ast.NodeFlagsPossiblyContainsDeprecatedTag
	}
	return flags
}

// flags is the flags part of finishNodeWithEnd: the context flags, plus
// ThisNodeHasError when a parse error is pending, which the call consumes.
func (p *Parser) flags() ast.NodeFlags {
	flags := p.contextFlags
	if p.hasParseError {
		flags |= ast.NodeFlagsThisNodeHasError
		p.hasParseError = false
	}
	return flags
}

func (p *Parser) parseSourceFileWorker() *store.File {
	isDeclarationFile := tspath.IsDeclarationFileName(p.opts.FileName)
	if isDeclarationFile {
		p.contextFlags |= ast.NodeFlagsAmbient
	}
	pos := p.nodePos()
	mark := p.parseListIndex(PCSourceElements, (*Parser).parseToplevelStatement)
	end := p.nodePos()
	endJSDoc := p.jsdocScannerInfo()
	eof := p.parseTokenNode()
	p.b.AddFlags(eof, p.jsdocFlags(endJSDoc))
	if p.b.View(eof).Kind() != ast.KindEndOfFile {
		panic("Expected end of file token from scanner.")
	}
	statements := p.b.List(int32(pos), int32(end), p.elems[mark:])
	p.elems = p.elems[:mark]
	node := p.b.NewSourceFile(p.flags(), int32(pos), int32(p.nodePos()), statements, eof)
	result := p.finishSourceFile(node, isDeclarationFile)
	if !result.IsDeclarationFile && result.ExternalModuleIndicator != store.NoNodeRef && len(p.possibleAwaitSpans) > 0 {
		reparse := p.reparseTopLevelAwait(node)
		if node != reparse {
			result = p.finishSourceFile(reparse, isDeclarationFile)
		}
	}
	result.Store = p.b.Finish()
	collectExternalModuleReferences(result)
	if result.Flags&ast.NodeFlagsJavaScriptFile != 0 {
		result.SetJSDiagnostics(p.jsDiagnostics)
	}
	return result
}

// finishSourceFile fills the fields of store.File from the scratch, the
// external module indicator included; the caller compacts the scratch into
// result.Store afterwards (Finish), because the top-level await reparse may
// still rewrite the tree. The JSDoc cache and the reparsed clones are not
// ported (store-ast-design-20260922.md 5).
func (p *Parser) finishSourceFile(root store.NodeRef, isDeclarationFile bool) *store.File {
	result := &store.File{FileName: p.opts.FileName, Path: p.opts.Path, Text: p.sourceText}
	result.CommentDirectives = p.scanner.CommentDirectives()
	result.Pragmas = getCommentPragmas(&p.pragmaFactory, p.sourceText)
	p.processPragmasIntoFields(result)
	result.SetDiagnostics(p.diagnostics)
	result.IsDeclarationFile = isDeclarationFile
	result.LanguageVariant = p.languageVariant
	result.ScriptKind = p.scriptKind
	p.b.AddFlags(root, p.sourceFlags)
	result.Flags = p.b.View(root).Flags()
	result.NodeCount = p.b.Len() - 1
	result.IdentifierCount = p.identifierCount
	p.setExternalModuleIndicator(result, root, p.opts.ExternalModuleIndicatorOptions)
	return result
}

func (p *Parser) parseToplevelStatement(i int) store.NodeRef {
	p.statementHasAwaitIdentifier = false
	statement := p.parseStatement()
	if p.statementHasAwaitIdentifier && p.b.View(statement).Flags()&ast.NodeFlagsAwaitContext == 0 {
		if len(p.possibleAwaitSpans) == 0 || p.possibleAwaitSpans[len(p.possibleAwaitSpans)-1] != i {
			p.possibleAwaitSpans = append(p.possibleAwaitSpans, i, i+1)
		} else {
			p.possibleAwaitSpans[len(p.possibleAwaitSpans)-1] = i + 1
		}
	}
	return statement
}

// reparseTopLevelAwait rebuilds the source file with the statements of the
// await spans reparsed in an await context; the old statements and the old
// root stay as dead nodes. The old statements are copied out of the list
// block first: the reparse appends nodes, so a view of the block must not be
// held across it. The rewinds keep the reparsed nodes, like they keep the
// diagnostics, by moving the mark forward before each rewind.
func (p *Parser) reparseTopLevelAwait(sourceFile store.NodeRef) store.NodeRef {
	if len(p.possibleAwaitSpans)%2 == 1 {
		panic("possibleAwaitSpans malformed: odd number of indices, not paired into spans.")
	}
	file := p.b.View(sourceFile).AsSourceFile()
	oldStatements := slices.Clone(file.Statements().Refs())
	statementsLoc := core.NewTextRange(int(file.Statements().Pos()), int(file.Statements().End()))
	endOfFileToken := file.EndOfFileToken().Ref()
	mark := len(p.elems)
	savedParseDiagnostics := p.diagnostics
	p.diagnostics = []*ast.Diagnostic{}

	afterAwaitStatement := 0
	for i := 0; i < len(p.possibleAwaitSpans); i += 2 {
		nextAwaitStatement := p.possibleAwaitSpans[i]
		// append all non-await statements between afterAwaitStatement and nextAwaitStatement
		prevStatement := oldStatements[afterAwaitStatement]
		nextStatement := oldStatements[nextAwaitStatement]
		p.elems = append(p.elems, oldStatements[afterAwaitStatement:nextAwaitStatement]...)

		// append all diagnostics associated with the copied range
		diagnosticStart := core.FindIndex(savedParseDiagnostics, func(diagnostic *ast.Diagnostic) bool {
			return diagnostic.Pos() >= p.pos(prevStatement)
		})
		var diagnosticEnd int
		if diagnosticStart >= 0 {
			diagnosticEnd = core.FindIndex(savedParseDiagnostics[diagnosticStart:], func(diagnostic *ast.Diagnostic) bool {
				return diagnostic.Pos() >= p.pos(nextStatement)
			})
		} else {
			diagnosticEnd = -1
		}
		if diagnosticStart >= 0 {
			var slice []*ast.Diagnostic
			if diagnosticEnd >= 0 {
				slice = savedParseDiagnostics[diagnosticStart : diagnosticStart+diagnosticEnd]
			} else {
				slice = savedParseDiagnostics[diagnosticStart:]
			}
			p.diagnostics = append(p.diagnostics, slice...)
		}

		state := p.mark()
		// reparse all statements between start and pos. We skip existing diagnostics for the same range and allow the parser to generate new ones.
		p.contextFlags |= ast.NodeFlagsAwaitContext
		p.scanner.ResetPos(p.pos(nextStatement))
		p.nextToken()

		afterAwaitStatement = p.possibleAwaitSpans[i+1]
		for p.token != ast.KindEndOfFile {
			startPos := p.scanner.TokenFullStart()
			statement := p.parseStatement()
			p.elems = append(p.elems, statement)
			if startPos == p.scanner.TokenFullStart() {
				p.nextToken()
			}
			if afterAwaitStatement < len(oldStatements) {
				lastAwaitStatement := oldStatements[afterAwaitStatement-1]
				if p.end(statement) == p.end(lastAwaitStatement) {
					// done reparsing this section
					break
				}
				if p.end(statement) > p.end(lastAwaitStatement) {
					// we ate into the next statement, so we must continue reparsing the next span
					i += 2
					if i < len(p.possibleAwaitSpans) {
						afterAwaitStatement = p.possibleAwaitSpans[i+1]
					} else {
						afterAwaitStatement = len(oldStatements)
					}
				}
			}
		}

		// Keep diagnostics and nodes from the reparse
		state.diagnosticsLen = len(p.diagnostics)
		state.nodes, state.extra = p.b.Mark()
		p.rewind(state)
	}

	// append all statements between pos and the end of the list
	if afterAwaitStatement < len(oldStatements) {
		prevStatement := oldStatements[afterAwaitStatement]
		p.elems = append(p.elems, oldStatements[afterAwaitStatement:]...)

		// append all diagnostics associated with the copied range
		diagnosticStart := core.FindIndex(savedParseDiagnostics, func(diagnostic *ast.Diagnostic) bool {
			return diagnostic.Pos() >= p.pos(prevStatement)
		})
		if diagnosticStart >= 0 {
			p.diagnostics = append(p.diagnostics, savedParseDiagnostics[diagnosticStart:]...)
		}
	}

	statements := p.b.List(int32(statementsLoc.Pos()), int32(statementsLoc.End()), p.elems[mark:])
	p.elems = p.elems[:mark]
	// The constructor (re)sets the parent of every statement to the reparsed source file.
	return p.b.NewSourceFile(p.b.View(sourceFile).Flags(), int32(p.pos(sourceFile)), int32(p.end(sourceFile)), statements, endOfFileToken)
}

// parseListIndex pushes the elements on p.elems and returns the mark where
// they start; the caller makes the block and pops them.
func (p *Parser) parseListIndex(kind ParsingContext, parseElement func(p *Parser, index int) store.NodeRef) (mark int) {
	saveParsingContexts := p.parsingContexts
	p.parsingContexts |= 1 << kind
	mark = len(p.elems)
	for i := 0; !p.isListTerminator(kind); i++ {
		if p.isListElement(kind, false /*inErrorRecovery*/) {
			elt := parseElement(p, len(p.elems)-mark)
			p.elems = append(p.elems, elt)
			continue
		}
		if p.abortParsingListOrMoveToNextToken(kind) {
			break
		}
	}
	p.parsingContexts = saveParsingContexts
	return mark
}

func (p *Parser) parseList(kind ParsingContext, parseElement func(p *Parser) store.NodeRef) store.ListRef {
	pos := p.nodePos()
	mark := p.parseListIndex(kind, func(p *Parser, _ int) store.NodeRef { return parseElement(p) })
	at := p.b.List(int32(pos), int32(p.nodePos()), p.elems[mark:])
	p.elems = p.elems[:mark]
	return at
}

// Return a non-nil (but possibly empty) list if parsing was successful, or nil if parseElement returned nil
func (p *Parser) parseDelimitedList(kind ParsingContext, parseElement func(p *Parser) store.NodeRef) store.ListRef {
	pos := p.nodePos()
	saveParsingContexts := p.parsingContexts
	p.parsingContexts |= 1 << kind
	mark := len(p.elems)
	for {
		if p.isListElement(kind, false /*inErrorRecovery*/) {
			startPos := p.nodePos()
			element := parseElement(p)
			if element == store.NoNodeRef {
				p.parsingContexts = saveParsingContexts
				p.elems = p.elems[:mark]
				// Return nil to indicate parseElement failed
				return store.NoListRef
			}
			p.elems = append(p.elems, element)
			if p.parseOptional(ast.KindCommaToken) {
				// No need to check for a zero length node since we know we parsed a comma
				continue
			}
			if p.isListTerminator(kind) {
				break
			}
			// We didn't get a comma, and the list wasn't terminated, explicitly parse
			// out a comma so we give a good error message.
			if p.token != ast.KindCommaToken && kind == PCEnumMembers {
				p.parseErrorAtCurrentToken(diagnostics.An_enum_member_name_must_be_followed_by_a_or)
			} else {
				p.parseExpected(ast.KindCommaToken)
			}
			// If the token was a semicolon, and the caller allows that, then skip it and
			// continue.  This ensures we get back on track and don't result in tons of
			// parse errors.  For example, this can happen when people do things like use
			// a semicolon to delimit object literal members.   Note: we'll have already
			// reported an error when we called parseExpected above.
			if (kind == PCObjectLiteralMembers || kind == PCImportAttributes) && p.token == ast.KindSemicolonToken && !p.hasPrecedingLineBreak() {
				p.nextToken()
			}
			if startPos == p.nodePos() {
				// What we're parsing isn't actually remotely recognizable as a element and we've consumed no tokens whatsoever
				// Consume a token to advance the parser in some way and avoid an infinite loop
				// This can happen when we're speculatively parsing parenthesized expressions which we think may be arrow functions,
				// or when a modifier keyword which is disallowed as a parameter name (ie, `static` in strict mode) is supplied
				p.nextToken()
			}
			continue
		}
		if p.isListTerminator(kind) {
			break
		}
		if p.abortParsingListOrMoveToNextToken(kind) {
			break
		}
	}
	p.parsingContexts = saveParsingContexts
	at := p.b.List(int32(pos), int32(p.nodePos()), p.elems[mark:])
	p.elems = p.elems[:mark]
	return at
}

// Return a non-nil (but possibly empty) list if parsing was successful, a missing list if the opening
// token wasn't found, or nil if parseElement returned nil.
func (p *Parser) parseBracketedList(kind ParsingContext, parseElement func(p *Parser) store.NodeRef, opening ast.Kind, closing ast.Kind) store.ListRef {
	if p.parseExpected(opening) {
		result := p.parseDelimitedList(kind, parseElement)
		p.parseExpected(closing)
		return result
	}
	return p.createMissingList()
}

func (p *Parser) parseEmptyNodeList() store.ListRef {
	return p.b.List(int32(p.nodePos()), int32(p.nodePos()), nil)
}

func (p *Parser) createMissingList() store.ListRef {
	result := p.parseEmptyNodeList()
	p.missingLists = append(p.missingLists, result)
	return result
}

// Returns true if we should abort parsing.
func (p *Parser) abortParsingListOrMoveToNextToken(kind ParsingContext) bool {
	p.parsingContextErrors(kind)
	if p.isInSomeParsingContext() {
		return true
	}
	p.nextToken()
	return false
}

// True if positioned at element or terminator of the current list or any enclosing list
func (p *Parser) isInSomeParsingContext() bool {
	// We should be in at least one parsing context, be it SourceElements while parsing
	// a SourceFile, or JSDocComment when lazily parsing JSDoc.
	debug.Assert(p.parsingContexts != 0, "Missing parsing context")
	for kind := range PCCount {
		if p.parsingContexts&(1<<kind) != 0 {
			if p.isListElement(kind, true /*inErrorRecovery*/) || p.isListTerminator(kind) {
				return true
			}
		}
	}
	return false
}

func (p *Parser) parsingContextErrors(context ParsingContext) {
	switch context {
	case PCSourceElements:
		if p.token == ast.KindDefaultKeyword {
			p.parseErrorAtCurrentToken(diagnostics.X_0_expected, "export")
		} else {
			p.parseErrorAtCurrentToken(diagnostics.Declaration_or_statement_expected)
		}
	case PCBlockStatements:
		p.parseErrorAtCurrentToken(diagnostics.Declaration_or_statement_expected)
	case PCSwitchClauses:
		p.parseErrorAtCurrentToken(diagnostics.X_case_or_default_expected)
	case PCSwitchClauseStatements:
		p.parseErrorAtCurrentToken(diagnostics.Statement_expected)
	case PCRestProperties, PCTypeMembers:
		p.parseErrorAtCurrentToken(diagnostics.Property_or_signature_expected)
	case PCClassMembers:
		p.parseErrorAtCurrentToken(diagnostics.Unexpected_token_A_constructor_method_accessor_or_property_was_expected)
	case PCEnumMembers:
		p.parseErrorAtCurrentToken(diagnostics.Enum_member_expected)
	case PCHeritageClauseElement:
		p.parseErrorAtCurrentToken(diagnostics.Expression_expected)
	case PCVariableDeclarations:
		if ast.IsKeyword(p.token) {
			p.parseErrorAtCurrentToken(diagnostics.X_0_is_not_allowed_as_a_variable_declaration_name, scanner.TokenToString(p.token))
		} else {
			p.parseErrorAtCurrentToken(diagnostics.Variable_declaration_expected)
		}
	case PCObjectBindingElements:
		p.parseErrorAtCurrentToken(diagnostics.Property_destructuring_pattern_expected)
	case PCArrayBindingElements:
		p.parseErrorAtCurrentToken(diagnostics.Array_element_destructuring_pattern_expected)
	case PCArgumentExpressions:
		p.parseErrorAtCurrentToken(diagnostics.Argument_expression_expected)
	case PCObjectLiteralMembers:
		p.parseErrorAtCurrentToken(diagnostics.Property_assignment_expected)
	case PCArrayLiteralMembers:
		p.parseErrorAtCurrentToken(diagnostics.Expression_or_comma_expected)
	case PCJSDocParameters:
		p.parseErrorAtCurrentToken(diagnostics.Parameter_declaration_expected)
	case PCParameters:
		if ast.IsKeyword(p.token) {
			p.parseErrorAtCurrentToken(diagnostics.X_0_is_not_allowed_as_a_parameter_name, scanner.TokenToString(p.token))
		} else {
			p.parseErrorAtCurrentToken(diagnostics.Parameter_declaration_expected)
		}
	case PCTypeParameters:
		p.parseErrorAtCurrentToken(diagnostics.Type_parameter_declaration_expected)
	case PCTypeArguments:
		p.parseErrorAtCurrentToken(diagnostics.Type_argument_expected)
	case PCTupleElementTypes:
		p.parseErrorAtCurrentToken(diagnostics.Type_expected)
	case PCHeritageClauses:
		p.parseErrorAtCurrentToken(diagnostics.Unexpected_token_expected)
	case PCImportOrExportSpecifiers:
		if p.token == ast.KindFromKeyword {
			p.parseErrorAtCurrentToken(diagnostics.X_0_expected, "}")
		} else {
			p.parseErrorAtCurrentToken(diagnostics.Identifier_expected)
		}
	case PCJsxAttributes, PCJsxChildren, PCJSDocComment:
		p.parseErrorAtCurrentToken(diagnostics.Identifier_expected)
	case PCImportAttributes:
		p.parseErrorAtCurrentToken(diagnostics.Identifier_or_string_literal_expected)
	default:
		panic("Unhandled case in parsingContextErrors")
	}
}

func (p *Parser) isListElement(parsingContext ParsingContext, inErrorRecovery bool) bool {
	switch parsingContext {
	case PCSourceElements, PCBlockStatements, PCSwitchClauseStatements:
		// If we're in error recovery, then we don't want to treat ';' as an empty statement.
		// The problem is that ';' can show up in far too many contexts, and if we see one
		// and assume it's a statement, then we may bail out inappropriately from whatever
		// we're parsing.  For example, if we have a semicolon in the middle of a class, then
		// we really don't want to assume the class is over and we're on a statement in the
		// outer module.  We just want to consume and move on.
		return !(p.token == ast.KindSemicolonToken && inErrorRecovery) && p.isStartOfStatement()
	case PCSwitchClauses:
		return p.token == ast.KindCaseKeyword || p.token == ast.KindDefaultKeyword
	case PCTypeMembers:
		return p.lookAhead((*Parser).scanTypeMemberStart)
	case PCClassMembers:
		// We allow semicolons as class elements (as specified by ES6) as long as we're
		// not in error recovery.  If we're in error recovery, we don't want an errant
		// semicolon to be treated as a class member (since they're almost always used
		// for statements.
		return p.lookAhead((*Parser).scanClassMemberStart) || p.token == ast.KindSemicolonToken && !inErrorRecovery
	case PCEnumMembers:
		// Include open bracket computed properties. This technically also lets in indexers,
		// which would be a candidate for improved error reporting.
		return p.token == ast.KindOpenBracketToken || p.isLiteralPropertyName()
	case PCObjectLiteralMembers:
		switch p.token {
		case ast.KindOpenBracketToken, ast.KindAsteriskToken, ast.KindDotDotDotToken, ast.KindDotToken: // Not an object literal member, but don't want to close the object (see `tests/cases/fourslash/completionsDotInObjectLiteral.ts`)
			return true
		default:
			return p.isLiteralPropertyName()
		}
	case PCRestProperties:
		return p.isLiteralPropertyName()
	case PCObjectBindingElements:
		return p.token == ast.KindOpenBracketToken || p.token == ast.KindDotDotDotToken || p.isLiteralPropertyName()
	case PCImportAttributes:
		return p.isImportAttributeName()
	case PCHeritageClauseElement:
		// If we see `{ ... }` then only consume it as an expression if it is followed by `,` or `{`
		// That way we won't consume the body of a class in its heritage clause.
		if p.token == ast.KindOpenBraceToken {
			return p.isValidHeritageClauseObjectLiteral()
		}
		if !inErrorRecovery {
			return p.isStartOfLeftHandSideExpression() && !p.isHeritageClauseExtendsOrImplementsKeyword()
		}
		// If we're in error recovery we tighten up what we're willing to match.
		// That way we don't treat something like "this" as a valid heritage clause
		// element during recovery.
		return p.isIdentifier() && !p.isHeritageClauseExtendsOrImplementsKeyword()
	case PCVariableDeclarations:
		return p.isBindingIdentifierOrPrivateIdentifierOrPattern()
	case PCArrayBindingElements:
		return p.token == ast.KindCommaToken || p.token == ast.KindDotDotDotToken || p.isBindingIdentifierOrPrivateIdentifierOrPattern()
	case PCTypeParameters:
		return p.token == ast.KindInKeyword || p.token == ast.KindConstKeyword || p.isIdentifier()
	case PCArrayLiteralMembers:
		// Not an array literal member, but don't want to close the array (see `tests/cases/fourslash/completionsDotInArrayLiteralInObjectLiteral.ts`)
		if p.token == ast.KindCommaToken || p.token == ast.KindDotToken {
			return true
		}
		fallthrough
	case PCArgumentExpressions:
		return p.token == ast.KindDotDotDotToken || p.isStartOfExpression()
	case PCParameters:
		return p.isStartOfParameter(false /*isJSDocParameter*/)
	case PCJSDocParameters:
		return p.isStartOfParameter(true /*isJSDocParameter*/)
	case PCTypeArguments, PCTupleElementTypes:
		return p.token == ast.KindCommaToken || p.isStartOfType(false /*inStartOfParameter*/)
	case PCHeritageClauses:
		return p.isHeritageClause()
	case PCImportOrExportSpecifiers:
		// bail out if the next token is [FromKeyword StringLiteral].
		// That means we're in something like `import { from "mod"`. Stop here can give better error message.
		if p.token == ast.KindFromKeyword && p.lookAhead((*Parser).nextTokenIsTokenStringLiteral) {
			return false
		}
		if p.token == ast.KindStringLiteral {
			return true // For "arbitrary module namespace identifiers"
		}
		return tokenIsIdentifierOrKeyword(p.token)
	case PCJsxAttributes:
		return tokenIsIdentifierOrKeyword(p.token) || p.token == ast.KindOpenBraceToken
	case PCJsxChildren:
		return true
	case PCJSDocComment:
		return true
	}
	panic("Unhandled case in isListElement")
}

func (p *Parser) isListTerminator(kind ParsingContext) bool {
	if p.token == ast.KindEndOfFile {
		return true
	}
	switch kind {
	case PCBlockStatements, PCSwitchClauses, PCTypeMembers, PCClassMembers, PCEnumMembers, PCObjectLiteralMembers,
		PCObjectBindingElements, PCImportOrExportSpecifiers, PCImportAttributes:
		return p.token == ast.KindCloseBraceToken
	case PCSwitchClauseStatements:
		return p.token == ast.KindCloseBraceToken || p.token == ast.KindCaseKeyword || p.token == ast.KindDefaultKeyword
	case PCHeritageClauseElement:
		return p.token == ast.KindOpenBraceToken || p.token == ast.KindExtendsKeyword || p.token == ast.KindImplementsKeyword
	case PCVariableDeclarations:
		// If we can consume a semicolon (either explicitly, or with ASI), then consider us done
		// with parsing the list of variable declarators.
		// In the case where we're parsing the variable declarator of a 'for-in' statement, we
		// are done if we see an 'in' keyword in front of us. Same with for-of
		// ERROR RECOVERY TWEAK:
		// For better error recovery, if we see an '=>' then we just stop immediately.  We've got an
		// arrow function here and it's going to be very unlikely that we'll resynchronize and get
		// another variable declaration.
		return p.canParseSemicolon() || p.token == ast.KindInKeyword || p.token == ast.KindOfKeyword || p.token == ast.KindEqualsGreaterThanToken
	case PCTypeParameters:
		// Tokens other than '>' are here for better error recovery
		return p.token == ast.KindGreaterThanToken || p.token == ast.KindOpenParenToken || p.token == ast.KindOpenBraceToken || p.token == ast.KindExtendsKeyword || p.token == ast.KindImplementsKeyword
	case PCArgumentExpressions:
		// Tokens other than ')' are here for better error recovery
		return p.token == ast.KindCloseParenToken || p.token == ast.KindSemicolonToken
	case PCArrayLiteralMembers, PCTupleElementTypes, PCArrayBindingElements:
		return p.token == ast.KindCloseBracketToken
	case PCJSDocParameters, PCParameters, PCRestProperties:
		// Tokens other than ')' and ']' (the latter for index signatures) are here for better error recovery
		return p.token == ast.KindCloseParenToken || p.token == ast.KindCloseBracketToken /*|| token == ast.KindOpenBraceToken*/
	case PCTypeArguments:
		// All other tokens should cause the type-argument to terminate except comma token
		return p.token != ast.KindCommaToken
	case PCHeritageClauses:
		return p.token == ast.KindOpenBraceToken || p.token == ast.KindCloseBraceToken
	case PCJsxAttributes:
		return p.token == ast.KindGreaterThanToken || p.token == ast.KindSlashToken
	case PCJsxChildren:
		return p.token == ast.KindLessThanToken && p.lookAhead((*Parser).nextTokenIsSlash)
	}
	return false
}

func (p *Parser) parseExpectedJSDoc(kind ast.Kind) bool {
	if p.token == kind {
		p.nextTokenJSDoc()
		return true
	}
	if !isKeywordOrPunctuation(kind) {
		panic("Invalid JSDoc kind: expected keyword or punctuation")
	}
	p.parseErrorAtCurrentToken(diagnostics.X_0_expected, scanner.TokenToString(kind))
	return false
}

func (p *Parser) parseExpectedMatchingBrackets(openKind ast.Kind, closeKind ast.Kind, openParsed bool, openPosition int) {
	if p.token == closeKind {
		p.nextToken()
		return
	}
	lastError := p.parseErrorAtCurrentToken(diagnostics.X_0_expected, scanner.TokenToString(closeKind))
	if !openParsed {
		return
	}
	if lastError != nil {
		related := ast.NewDiagnostic(nil, core.NewTextRange(openPosition, openPosition), diagnostics.The_parser_expected_to_find_a_1_to_match_the_0_token_here, scanner.TokenToString(openKind), scanner.TokenToString(closeKind))
		lastError.AddRelatedInfo(related)
	}
}

func (p *Parser) parseOptional(token ast.Kind) bool {
	if p.token == token {
		p.nextToken()
		return true
	}
	return false
}

func (p *Parser) parseExpected(kind ast.Kind) bool {
	return p.parseExpectedWithDiagnostic(kind, nil, true)
}

func (p *Parser) parseExpectedWithoutAdvancing(kind ast.Kind) bool {
	return p.parseExpectedWithDiagnostic(kind, nil, false)
}

func (p *Parser) parseExpectedWithDiagnostic(kind ast.Kind, message *diagnostics.Message, shouldAdvance bool) bool {
	if p.token == kind {
		if shouldAdvance {
			p.nextToken()
		}
		return true
	}
	// Report specific message if provided with one.  Otherwise, report generic fallback message.
	if message != nil {
		p.parseErrorAtCurrentToken(message)
	} else {
		p.parseErrorAtCurrentToken(diagnostics.X_0_expected, scanner.TokenToString(kind))
	}
	return false
}

func (p *Parser) parseTokenNode() store.NodeRef {
	pos := p.nodePos()
	kind := p.token
	p.nextToken()
	return p.b.NewToken(kind, p.flags(), int32(pos), int32(p.nodePos()))
}

func (p *Parser) parseExpectedToken(kind ast.Kind) store.NodeRef {
	token := p.parseOptionalToken(kind)
	if token == store.NoNodeRef {
		p.parseErrorAtCurrentToken(diagnostics.X_0_expected, scanner.TokenToString(kind))
		token = p.b.NewToken(kind, p.flags(), int32(p.nodePos()), int32(p.nodePos()))
	}
	return token
}

func (p *Parser) parseOptionalToken(kind ast.Kind) store.NodeRef {
	if p.token == kind {
		return p.parseTokenNode()
	}
	return store.NoNodeRef
}

func (p *Parser) parseExpectedTokenJSDoc(kind ast.Kind) store.NodeRef {
	optional := p.parseOptionalTokenJSDoc(kind)
	if optional == store.NoNodeRef {
		if !isKeywordOrPunctuation(kind) {
			panic("expected keyword or punctuation")
		}
		p.parseErrorAtCurrentToken(diagnostics.X_0_expected, scanner.TokenToString(kind))
		optional = p.b.NewToken(kind, p.flags(), int32(p.nodePos()), int32(p.nodePos()))
	}
	return optional
}

func (p *Parser) parseOptionalTokenJSDoc(kind ast.Kind) store.NodeRef {
	if p.token == kind {
		return p.parseTokenNode()
	}
	return store.NoNodeRef
}

func (p *Parser) parseStatement() store.NodeRef {
	switch p.token {
	case ast.KindSemicolonToken:
		return p.parseEmptyStatement()
	case ast.KindOpenBraceToken:
		return p.parseBlock(false /*ignoreMissingOpenBrace*/, nil)
	case ast.KindVarKeyword:
		return p.parseVariableStatement(p.nodePos(), p.jsdocScannerInfo(), store.NoListRef /*modifiers*/)
	case ast.KindLetKeyword:
		if p.isLetDeclaration() {
			return p.parseVariableStatement(p.nodePos(), p.jsdocScannerInfo(), store.NoListRef /*modifiers*/)
		}
	case ast.KindAwaitKeyword:
		if p.isAwaitUsingDeclaration() {
			return p.parseVariableStatement(p.nodePos(), p.jsdocScannerInfo(), store.NoListRef /*modifiers*/)
		}
	case ast.KindUsingKeyword:
		if p.isUsingDeclaration() {
			return p.parseVariableStatement(p.nodePos(), p.jsdocScannerInfo(), store.NoListRef /*modifiers*/)
		}
	case ast.KindFunctionKeyword:
		return p.parseFunctionDeclaration(p.nodePos(), p.jsdocScannerInfo(), store.NoListRef /*modifiers*/)
	case ast.KindClassKeyword:
		return p.parseClassDeclaration(p.nodePos(), p.jsdocScannerInfo(), store.NoListRef /*modifiers*/)
	case ast.KindIfKeyword:
		return p.parseIfStatement()
	case ast.KindDoKeyword:
		return p.parseDoStatement()
	case ast.KindWhileKeyword:
		return p.parseWhileStatement()
	case ast.KindForKeyword:
		return p.parseForOrForInOrForOfStatement()
	case ast.KindContinueKeyword:
		return p.parseContinueStatement()
	case ast.KindBreakKeyword:
		return p.parseBreakStatement()
	case ast.KindReturnKeyword:
		return p.parseReturnStatement()
	case ast.KindWithKeyword:
		return p.parseWithStatement()
	case ast.KindSwitchKeyword:
		return p.parseSwitchStatement()
	case ast.KindThrowKeyword:
		return p.parseThrowStatement()
	case ast.KindTryKeyword, ast.KindCatchKeyword, ast.KindFinallyKeyword:
		return p.parseTryStatement()
	case ast.KindDebuggerKeyword:
		return p.parseDebuggerStatement()
	case ast.KindAtToken:
		return p.parseDeclaration()
	case ast.KindAsyncKeyword, ast.KindInterfaceKeyword, ast.KindTypeKeyword, ast.KindModuleKeyword, ast.KindNamespaceKeyword,
		ast.KindDeclareKeyword, ast.KindConstKeyword, ast.KindEnumKeyword, ast.KindExportKeyword, ast.KindImportKeyword,
		ast.KindPrivateKeyword, ast.KindProtectedKeyword, ast.KindPublicKeyword, ast.KindAbstractKeyword, ast.KindAccessorKeyword,
		ast.KindStaticKeyword, ast.KindReadonlyKeyword, ast.KindGlobalKeyword:
		if p.isStartOfDeclaration() {
			return p.parseDeclaration()
		}
	}
	return p.parseExpressionOrLabeledStatement()
}

func (p *Parser) parseDeclaration() store.NodeRef {
	// `parseListElement` attempted to get the reused node at this position,
	// but the ambient context flag was not yet set, so the node appeared
	// not reusable in that context.
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	modifiers := p.parseModifiersEx( /*allowDecorators*/ true, false /*permitConstAsModifier*/, false /*stopOnStartOfClassStaticBlock*/)
	isAmbient := modifiers != store.NoListRef && p.someModifier(modifiers, isDeclareModifier)
	if isAmbient {
		// !!! incremental parsing
		// node := p.tryReuseAmbientDeclaration(pos)
		// if node {
		// 	return node
		// }
		for _, m := range p.b.ViewList(modifiers).Refs() {
			p.b.AddFlags(m, ast.NodeFlagsAmbient)
		}
		saveContextFlags := p.contextFlags
		p.setContextFlags(ast.NodeFlagsAmbient, true)
		result := p.parseDeclarationWorker(pos, jsdoc, modifiers)
		p.contextFlags = saveContextFlags
		return result
	} else {
		return p.parseDeclarationWorker(pos, jsdoc, modifiers)
	}
}

func (p *Parser) parseDeclarationWorker(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	switch p.token {
	case ast.KindVarKeyword, ast.KindLetKeyword, ast.KindConstKeyword, ast.KindUsingKeyword:
		return p.parseVariableStatement(pos, jsdoc, modifiers)
	case ast.KindAwaitKeyword:
		if p.isAwaitUsingDeclaration() {
			return p.parseVariableStatement(pos, jsdoc, modifiers)
		}
	case ast.KindFunctionKeyword:
		return p.parseFunctionDeclaration(pos, jsdoc, modifiers)
	case ast.KindClassKeyword:
		return p.parseClassDeclaration(pos, jsdoc, modifiers)
	case ast.KindInterfaceKeyword:
		return p.parseInterfaceDeclaration(pos, jsdoc, modifiers)
	case ast.KindTypeKeyword:
		return p.parseTypeAliasDeclaration(pos, jsdoc, modifiers)
	case ast.KindEnumKeyword:
		return p.parseEnumDeclaration(pos, jsdoc, modifiers)
	case ast.KindGlobalKeyword, ast.KindModuleKeyword, ast.KindNamespaceKeyword:
		return p.parseModuleDeclaration(pos, jsdoc, modifiers)
	case ast.KindImportKeyword:
		return p.parseImportDeclarationOrImportEqualsDeclaration(pos, jsdoc, modifiers)
	case ast.KindExportKeyword:
		p.nextToken()
		switch p.token {
		case ast.KindDefaultKeyword, ast.KindEqualsToken:
			return p.parseExportAssignment(pos, jsdoc, modifiers)
		case ast.KindAsKeyword:
			return p.parseNamespaceExportDeclaration(pos, jsdoc, modifiers)
		default:
			return p.parseExportDeclaration(pos, jsdoc, modifiers)
		}
	}
	if modifiers != store.NoListRef {
		// We reached this point because we encountered decorators and/or modifiers and assumed a declaration
		// would follow. For recovery and error reporting purposes, return an incomplete declaration.
		p.parseErrorAt(p.nodePos(), p.nodePos(), diagnostics.Declaration_expected)
		return p.b.NewMissingDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers)
	}
	panic("Unhandled case in parseDeclarationWorker")
}

func isDeclareModifier(modifier store.Node) bool {
	return modifier.Kind() == ast.KindDeclareKeyword
}

func (p *Parser) isLetDeclaration() bool {
	// In ES6 'let' always starts a lexical declaration if followed by an identifier or {
	// or [.
	return p.lookAhead((*Parser).nextTokenIsBindingIdentifierOrStartOfDestructuring)
}

func (p *Parser) nextTokenIsBindingIdentifierOrStartOfDestructuring() bool {
	p.nextToken()
	return p.isBindingIdentifier() || p.token == ast.KindOpenBraceToken || p.token == ast.KindOpenBracketToken
}

func (p *Parser) parseBlock(ignoreMissingOpenBrace bool, diagnosticMessage *diagnostics.Message) store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	openBracePosition := p.scanner.TokenStart()
	openBraceParsed := p.parseExpectedWithDiagnostic(ast.KindOpenBraceToken, diagnosticMessage, true /*shouldAdvance*/)
	multiline := false
	if openBraceParsed || ignoreMissingOpenBrace {
		multiline = p.hasPrecedingLineBreak()
		statements := p.parseList(PCBlockStatements, (*Parser).parseStatement)
		p.parseExpectedMatchingBrackets(ast.KindOpenBraceToken, ast.KindCloseBraceToken, openBraceParsed, openBracePosition)
		result := p.b.NewBlock(p.flags(), int32(pos), int32(p.nodePos()), statements, multiline)
		p.b.AddFlags(result, p.jsdocFlags(jsdoc))
		if p.token == ast.KindEqualsToken {
			p.parseErrorAtCurrentToken(diagnostics.Declaration_or_statement_expected_This_follows_a_block_of_statements_so_if_you_intended_to_write_a_destructuring_assignment_you_might_need_to_wrap_the_whole_assignment_in_parentheses)
			p.nextToken()
		}
		return result
	}
	statements := p.createMissingList()
	result := p.b.NewBlock(p.flags(), int32(pos), int32(p.nodePos()), statements, multiline)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseEmptyStatement() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindSemicolonToken)
	result := p.b.NewToken(ast.KindEmptyStatement, p.flags(), int32(pos), int32(p.nodePos()))
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseIfStatement() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindIfKeyword)
	openParenPosition := p.scanner.TokenStart()
	openParenParsed := p.parseExpected(ast.KindOpenParenToken)
	expression := p.parseExpressionAllowIn()
	p.parseExpectedMatchingBrackets(ast.KindOpenParenToken, ast.KindCloseParenToken, openParenParsed, openParenPosition)
	thenStatement := p.parseStatement()
	var elseStatement store.NodeRef
	if p.parseOptional(ast.KindElseKeyword) {
		elseStatement = p.parseStatement()
	}
	result := p.b.NewIfStatement(p.flags(), int32(pos), int32(p.nodePos()), expression, thenStatement, elseStatement)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseDoStatement() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindDoKeyword)
	statement := p.parseStatement()
	p.parseExpected(ast.KindWhileKeyword)
	openParenPosition := p.scanner.TokenStart()
	openParenParsed := p.parseExpected(ast.KindOpenParenToken)
	expression := p.parseExpressionAllowIn()
	p.parseExpectedMatchingBrackets(ast.KindOpenParenToken, ast.KindCloseParenToken, openParenParsed, openParenPosition)
	// From: https://mail.mozilla.org/pipermail/es-discuss/2011-August/016188.html
	// 157 min --- All allen at wirfs-brock.com CONF --- "do{;}while(false)false" prohibited in
	// spec but allowed in consensus reality. Approved -- this is the de-facto standard whereby
	//  do;while(0)x will have a semicolon inserted before x.
	p.parseOptional(ast.KindSemicolonToken)
	result := p.b.NewDoStatement(p.flags(), int32(pos), int32(p.nodePos()), statement, expression)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseWhileStatement() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindWhileKeyword)
	openParenPosition := p.scanner.TokenStart()
	openParenParsed := p.parseExpected(ast.KindOpenParenToken)
	expression := p.parseExpressionAllowIn()
	p.parseExpectedMatchingBrackets(ast.KindOpenParenToken, ast.KindCloseParenToken, openParenParsed, openParenPosition)
	statement := p.parseStatement()
	result := p.b.NewWhileStatement(p.flags(), int32(pos), int32(p.nodePos()), expression, statement)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseForOrForInOrForOfStatement() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindForKeyword)
	awaitToken := p.parseOptionalToken(ast.KindAwaitKeyword)
	p.parseExpected(ast.KindOpenParenToken)
	var initializer store.NodeRef
	if p.token != ast.KindSemicolonToken {
		if p.token == ast.KindVarKeyword || p.token == ast.KindLetKeyword || p.token == ast.KindConstKeyword ||
			p.token == ast.KindUsingKeyword && p.lookAhead((*Parser).nextTokenIsBindingIdentifierOrStartOfDestructuringOnSameLineDisallowOf) ||
			// this one is meant to allow of
			p.token == ast.KindAwaitKeyword && p.lookAhead((*Parser).nextIsUsingKeywordThenBindingIdentifierOrStartOfObjectDestructuringOnSameLine) {
			initializer = p.parseVariableDeclarationList(true /*inForStatementInitializer*/)
		} else {
			initializer = p.doInContext(ast.NodeFlagsDisallowInContext, true, (*Parser).parseExpression)
		}
	}
	var result store.NodeRef
	switch {
	case awaitToken != store.NoNodeRef && p.parseExpected(ast.KindOfKeyword) || awaitToken == store.NoNodeRef && p.parseOptional(ast.KindOfKeyword):
		expression := p.doInContext(ast.NodeFlagsDisallowInContext, false, (*Parser).parseAssignmentExpressionOrHigher)
		p.parseExpected(ast.KindCloseParenToken)
		statement := p.parseStatement()
		result = p.b.NewForInOrOfStatement(ast.KindForOfStatement, p.flags(), int32(pos), int32(p.nodePos()), awaitToken, initializer, expression, statement)
	case p.parseOptional(ast.KindInKeyword):
		expression := p.parseExpressionAllowIn()
		p.parseExpected(ast.KindCloseParenToken)
		statement := p.parseStatement()
		result = p.b.NewForInOrOfStatement(ast.KindForInStatement, p.flags(), int32(pos), int32(p.nodePos()), store.NoNodeRef /*awaitToken*/, initializer, expression, statement)
	default:
		p.parseExpected(ast.KindSemicolonToken)
		var condition store.NodeRef
		if p.token != ast.KindSemicolonToken && p.token != ast.KindCloseParenToken {
			condition = p.parseExpressionAllowIn()
		}
		p.parseExpected(ast.KindSemicolonToken)
		var incrementor store.NodeRef
		if p.token != ast.KindCloseParenToken {
			incrementor = p.parseExpressionAllowIn()
		}
		p.parseExpected(ast.KindCloseParenToken)
		statement := p.parseStatement()
		result = p.b.NewForStatement(p.flags(), int32(pos), int32(p.nodePos()), initializer, condition, incrementor, statement)
	}
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseBreakStatement() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindBreakKeyword)
	label := p.parseIdentifierUnlessAtSemicolon()
	p.parseSemicolon()
	result := p.b.NewBreakStatement(p.flags(), int32(pos), int32(p.nodePos()), label)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseContinueStatement() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindContinueKeyword)
	label := p.parseIdentifierUnlessAtSemicolon()
	p.parseSemicolon()
	result := p.b.NewContinueStatement(p.flags(), int32(pos), int32(p.nodePos()), label)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseIdentifierUnlessAtSemicolon() store.NodeRef {
	if !p.canParseSemicolon() {
		return p.parseIdentifier()
	}
	return store.NoNodeRef
}

func (p *Parser) parseReturnStatement() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindReturnKeyword)
	var expression store.NodeRef
	if !p.canParseSemicolon() {
		expression = p.parseExpressionAllowIn()
	}
	p.parseSemicolon()
	result := p.b.NewReturnStatement(p.flags(), int32(pos), int32(p.nodePos()), expression)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseWithStatement() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindWithKeyword)
	openParenPosition := p.scanner.TokenStart()
	openParenParsed := p.parseExpected(ast.KindOpenParenToken)
	expression := p.parseExpressionAllowIn()
	p.parseExpectedMatchingBrackets(ast.KindOpenParenToken, ast.KindCloseParenToken, openParenParsed, openParenPosition)
	statement := p.doInContext(ast.NodeFlagsInWithStatement, true, (*Parser).parseStatement)
	result := p.b.NewWithStatement(p.flags(), int32(pos), int32(p.nodePos()), expression, statement)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseCaseClause() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindCaseKeyword)
	expression := p.parseExpressionAllowIn()
	p.parseExpected(ast.KindColonToken)
	statements := p.parseList(PCSwitchClauseStatements, (*Parser).parseStatement)
	result := p.b.NewCaseOrDefaultClause(ast.KindCaseClause, p.flags(), int32(pos), int32(p.nodePos()), expression, statements)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseDefaultClause() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindDefaultKeyword)
	p.parseExpected(ast.KindColonToken)
	statements := p.parseList(PCSwitchClauseStatements, (*Parser).parseStatement)
	result := p.b.NewCaseOrDefaultClause(ast.KindDefaultClause, p.flags(), int32(pos), int32(p.nodePos()), store.NoNodeRef /*expression*/, statements)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseCaseOrDefaultClause() store.NodeRef {
	if p.token == ast.KindCaseKeyword {
		return p.parseCaseClause()
	}
	return p.parseDefaultClause()
}

func (p *Parser) parseCaseBlock() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindOpenBraceToken)
	clauses := p.parseList(PCSwitchClauses, (*Parser).parseCaseOrDefaultClause)
	p.parseExpected(ast.KindCloseBraceToken)
	result := p.b.NewCaseBlock(p.flags(), int32(pos), int32(p.nodePos()), clauses)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseSwitchStatement() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindSwitchKeyword)
	p.parseExpected(ast.KindOpenParenToken)
	expression := p.parseExpressionAllowIn()
	p.parseExpected(ast.KindCloseParenToken)
	caseBlock := p.parseCaseBlock()
	result := p.b.NewSwitchStatement(p.flags(), int32(pos), int32(p.nodePos()), expression, caseBlock)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseThrowStatement() store.NodeRef {
	// ThrowStatement[Yield] :
	//      throw [no LineTerminator here]Expression[In, ?Yield];
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindThrowKeyword)
	// Because of automatic semicolon insertion, we need to report error if this
	// throw could be terminated with a semicolon.  Note: we can't call 'parseExpression'
	// directly as that might consume an expression on the following line.
	// Instead, we create a "missing" identifier, but don't report an error. The actual error
	// will be reported in the grammar walker.
	var expression store.NodeRef
	if !p.hasPrecedingLineBreak() {
		expression = p.parseExpressionAllowIn()
	} else {
		expression = p.createMissingIdentifier()
	}
	if !p.tryParseSemicolon() {
		p.parseErrorForMissingSemicolonAfter(expression)
	}
	result := p.b.NewThrowStatement(p.flags(), int32(pos), int32(p.nodePos()), expression)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

// TODO: Review for error recovery
func (p *Parser) parseTryStatement() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindTryKeyword)
	tryBlock := p.parseBlock(false /*ignoreMissingOpenBrace*/, nil)
	var catchClause store.NodeRef
	if p.token == ast.KindCatchKeyword {
		catchClause = p.parseCatchClause()
	}
	// If we don't have a catch clause, then we must have a finally clause.  Try to parse
	// one out no matter what.
	var finallyBlock store.NodeRef
	if catchClause == store.NoNodeRef || p.token == ast.KindFinallyKeyword {
		p.parseExpectedWithDiagnostic(ast.KindFinallyKeyword, diagnostics.X_catch_or_finally_expected, true /*shouldAdvance*/)
		finallyBlock = p.parseBlock(false /*ignoreMissingOpenBrace*/, nil)
	}
	result := p.b.NewTryStatement(p.flags(), int32(pos), int32(p.nodePos()), tryBlock, catchClause, finallyBlock)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseCatchClause() store.NodeRef {
	pos := p.nodePos()
	p.parseExpected(ast.KindCatchKeyword)
	var variableDeclaration store.NodeRef
	if p.parseOptional(ast.KindOpenParenToken) {
		variableDeclaration = p.parseVariableDeclaration()
		p.parseExpected(ast.KindCloseParenToken)
	}
	block := p.parseBlock(false /*ignoreMissingOpenBrace*/, nil)
	result := p.b.NewCatchClause(p.flags(), int32(pos), int32(p.nodePos()), variableDeclaration, block)
	return result
}

func (p *Parser) parseDebuggerStatement() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindDebuggerKeyword)
	p.parseSemicolon()
	result := p.b.NewToken(ast.KindDebuggerStatement, p.flags(), int32(pos), int32(p.nodePos()))
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseExpressionOrLabeledStatement() store.NodeRef {
	// Avoiding having to do the lookahead for a labeled statement by just trying to parse
	// out an expression, seeing if it is identifier and then seeing if it is followed by
	// a colon.
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	hasParen := p.token == ast.KindOpenParenToken
	expression := p.parseExpression()

	if p.b.View(expression).Kind() == ast.KindIdentifier && p.parseOptional(ast.KindColonToken) {
		statement := p.parseStatement()
		result := p.b.NewLabeledStatement(p.flags(), int32(pos), int32(p.nodePos()), expression, statement)
		p.b.AddFlags(result, p.jsdocFlags(jsdoc))
		return result
	}

	if !p.tryParseSemicolon() {
		p.parseErrorForMissingSemicolonAfter(expression)
	}
	result := p.b.NewExpressionStatement(p.flags(), int32(pos), int32(p.nodePos()), expression)
	if hasParen {
		jsdoc &^= jsdocScannerInfoHasJSDoc
	}
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseVariableStatement(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	declarationList := p.parseVariableDeclarationList(false /*inForStatementInitializer*/)
	p.parseSemicolon()
	result := p.b.NewVariableStatement(p.flags(), int32(pos), int32(p.nodePos()), modifiers, declarationList)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	p.checkJSSyntax(result)
	return result
}

func (p *Parser) parseVariableDeclarationList(inForStatementInitializer bool) store.NodeRef {
	pos := p.nodePos()
	var flags ast.NodeFlags
	switch p.token {
	case ast.KindVarKeyword:
		flags = ast.NodeFlagsNone
	case ast.KindLetKeyword:
		flags = ast.NodeFlagsLet
	case ast.KindConstKeyword:
		flags = ast.NodeFlagsConst
	case ast.KindUsingKeyword:
		flags = ast.NodeFlagsUsing
	case ast.KindAwaitKeyword:
		if !p.isAwaitUsingDeclaration() {
			break
		}
		flags = ast.NodeFlagsAwaitUsing
		p.nextToken()
	default:
		panic("Unhandled case in parseVariableDeclarationList")
	}
	p.nextToken()
	// The user may have written the following:
	//
	//    for (let of X) { }
	//
	// In this case, we want to parse an empty declaration list, and then parse 'of'
	// as a keyword. The reason this is not automatic is that 'of' is a valid identifier.
	// So we need to look ahead to determine if 'of' should be treated as a keyword in
	// this context.
	// The checker will then give an error that there is an empty declaration list.
	var declarations store.ListRef
	if p.token == ast.KindOfKeyword && p.lookAhead((*Parser).nextIsIdentifierAndCloseParen) {
		declarations = p.createMissingList()
	} else {
		saveContextFlags := p.contextFlags
		p.setContextFlags(ast.NodeFlagsDisallowInContext, inForStatementInitializer)
		declarations = p.parseDelimitedList(PCVariableDeclarations, core.IfElse(inForStatementInitializer, (*Parser).parseVariableDeclaration, (*Parser).parseVariableDeclarationAllowExclamation))
		p.contextFlags = saveContextFlags
	}
	result := p.b.NewVariableDeclarationList(p.flags()|flags, int32(pos), int32(p.nodePos()), declarations)
	return result
}

func (p *Parser) nextIsIdentifierAndCloseParen() bool {
	return p.nextTokenIsIdentifier() && p.nextToken() == ast.KindCloseParenToken
}

func (p *Parser) nextTokenIsIdentifier() bool {
	p.nextToken()
	return p.isIdentifier()
}

func (p *Parser) parseVariableDeclaration() store.NodeRef {
	return p.parseVariableDeclarationWorker(false /*allowExclamation*/)
}

func (p *Parser) parseVariableDeclarationAllowExclamation() store.NodeRef {
	return p.parseVariableDeclarationWorker(true /*allowExclamation*/)
}

func (p *Parser) parseVariableDeclarationWorker(allowExclamation bool) store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	name := p.parseIdentifierOrPatternWithDiagnostic(diagnostics.Private_identifiers_are_not_allowed_in_variable_declarations)
	var exclamationToken store.NodeRef
	if allowExclamation && p.b.View(name).Kind() == ast.KindIdentifier && p.token == ast.KindExclamationToken && !p.hasPrecedingLineBreak() {
		exclamationToken = p.parseTokenNode()
	}
	typeNode := p.parseTypeAnnotation()
	var initializer store.NodeRef
	if p.token != ast.KindInKeyword && p.token != ast.KindOfKeyword {
		initializer = p.parseInitializer()
	}
	result := p.b.NewVariableDeclaration(p.flags(), int32(pos), int32(p.nodePos()), name, exclamationToken, typeNode, initializer)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	p.checkJSSyntax(result)
	return result
}

func (p *Parser) parseIdentifierOrPattern() store.NodeRef {
	return p.parseIdentifierOrPatternWithDiagnostic(nil)
}

func (p *Parser) parseIdentifierOrPatternWithDiagnostic(privateIdentifierDiagnosticMessage *diagnostics.Message) store.NodeRef {
	if p.token == ast.KindOpenBracketToken {
		return p.parseArrayBindingPattern()
	}
	if p.token == ast.KindOpenBraceToken {
		return p.parseObjectBindingPattern()
	}
	return p.parseBindingIdentifierWithDiagnostic(privateIdentifierDiagnosticMessage)
}

func (p *Parser) parseArrayBindingPattern() store.NodeRef {
	pos := p.nodePos()
	p.parseExpected(ast.KindOpenBracketToken)
	saveContextFlags := p.contextFlags
	p.setContextFlags(ast.NodeFlagsDisallowInContext, false)
	elements := p.parseDelimitedList(PCArrayBindingElements, (*Parser).parseArrayBindingElement)
	p.contextFlags = saveContextFlags
	p.parseExpected(ast.KindCloseBracketToken)
	return p.b.NewBindingPattern(ast.KindArrayBindingPattern, p.flags(), int32(pos), int32(p.nodePos()), elements)
}

func (p *Parser) parseArrayBindingElement() store.NodeRef {
	pos := p.nodePos()
	var dotDotDotToken store.NodeRef
	var name store.NodeRef
	var initializer store.NodeRef
	if p.token != ast.KindCommaToken {
		// These are all nil for a missing element
		dotDotDotToken = p.parseOptionalToken(ast.KindDotDotDotToken)
		name = p.parseIdentifierOrPattern()
		initializer = p.parseInitializer()
	}
	return p.b.NewBindingElement(p.flags(), int32(pos), int32(p.nodePos()), dotDotDotToken, store.NoNodeRef /*propertyName*/, name, initializer)
}

func (p *Parser) parseObjectBindingPattern() store.NodeRef {
	pos := p.nodePos()
	p.parseExpected(ast.KindOpenBraceToken)
	saveContextFlags := p.contextFlags
	p.setContextFlags(ast.NodeFlagsDisallowInContext, false)
	elements := p.parseDelimitedList(PCObjectBindingElements, (*Parser).parseObjectBindingElement)
	p.contextFlags = saveContextFlags
	p.parseExpected(ast.KindCloseBraceToken)
	return p.b.NewBindingPattern(ast.KindObjectBindingPattern, p.flags(), int32(pos), int32(p.nodePos()), elements)
}

func (p *Parser) parseObjectBindingElement() store.NodeRef {
	pos := p.nodePos()
	dotDotDotToken := p.parseOptionalToken(ast.KindDotDotDotToken)
	tokenIsIdentifier := p.isBindingIdentifier()
	propertyName := p.parsePropertyName()
	var name store.NodeRef
	if tokenIsIdentifier && p.token != ast.KindColonToken {
		name = propertyName
		propertyName = store.NoNodeRef
	} else {
		p.parseExpected(ast.KindColonToken)
		name = p.parseIdentifierOrPattern()
	}
	initializer := p.parseInitializer()
	return p.b.NewBindingElement(p.flags(), int32(pos), int32(p.nodePos()), dotDotDotToken, propertyName, name, initializer)
}

func (p *Parser) parseInitializer() store.NodeRef {
	if p.parseOptional(ast.KindEqualsToken) {
		return p.parseAssignmentExpressionOrHigher()
	}
	return store.NoNodeRef
}

func (p *Parser) parseTypeAnnotation() store.NodeRef {
	if p.parseOptional(ast.KindColonToken) {
		return p.parseType()
	}
	return store.NoNodeRef
}

func (p *Parser) parseFunctionDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	p.parseExpected(ast.KindFunctionKeyword)
	asteriskToken := p.parseOptionalToken(ast.KindAsteriskToken)
	// We don't parse the name here in await context, instead we will report a grammar error in the checker.
	var name store.NodeRef
	if modifiers == store.NoListRef || p.modifierFlags(modifiers)&ast.ModifierFlagsDefault == 0 || p.isBindingIdentifier() {
		name = p.parseBindingIdentifier()
	}
	signatureFlags := core.IfElse(asteriskToken != store.NoNodeRef, ParseFlagsYield, ParseFlagsNone) | core.IfElse(modifiers != store.NoListRef && p.modifierFlags(modifiers)&ast.ModifierFlagsAsync != 0, ParseFlagsAwait, ParseFlagsNone)
	typeParameters := p.parseTypeParameters()
	saveContextFlags := p.contextFlags
	if modifiers != store.NoListRef && p.modifierFlags(modifiers)&ast.ModifierFlagsExport != 0 {
		p.setContextFlags(ast.NodeFlagsAwaitContext, true)
	}
	parameters := p.parseParameters(signatureFlags)
	returnType := p.parseReturnType(ast.KindColonToken, false /*isType*/)
	body := p.parseFunctionBlockOrSemicolon(signatureFlags, diagnostics.X_or_expected)
	p.contextFlags = saveContextFlags
	result := p.b.NewFunctionDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, asteriskToken, name, typeParameters, parameters, returnType, store.NoNodeRef /*fullSignature*/, body)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	p.checkJSSyntax(result)
	return result
}

func (p *Parser) parseClassDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	return p.parseClassDeclarationOrExpression(pos, jsdoc, modifiers, ast.KindClassDeclaration)
}

func (p *Parser) parseClassExpression() store.NodeRef {
	return p.parseClassDeclarationOrExpression(p.nodePos(), p.jsdocScannerInfo(), store.NoListRef /*modifiers*/, ast.KindClassExpression)
}

func (p *Parser) parseClassDeclarationOrExpression(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef, kind ast.Kind) store.NodeRef {
	saveContextFlags := p.contextFlags
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	p.parseExpected(ast.KindClassKeyword)
	// We don't parse the name here in await context, instead we will report a grammar error in the checker.
	name := p.parseNameOfClassDeclarationOrExpression()
	typeParameters := p.parseTypeParameters()
	if modifiers != store.NoListRef &&
		p.parsingContexts&(1<<PCSourceElements) != 0 &&
		p.parsingContexts&((1<<PCBlockStatements)|(1<<PCSwitchClauseStatements)) == 0 &&
		p.someModifier(modifiers, isExportModifier) {
		p.setContextFlags(ast.NodeFlagsAwaitContext, true /*value*/)
	}
	heritageClauses := p.parseHeritageClauses(false /*isInterface*/)
	var members store.ListRef
	if p.parseExpected(ast.KindOpenBraceToken) {
		// ClassTail[Yield,Await] : (Modified) See 14.5
		//      ClassHeritage[?Yield,?Await]opt { ClassBody[?Yield,?Await]opt }
		members = p.parseList(PCClassMembers, (*Parser).parseClassElement)
		p.parseExpected(ast.KindCloseBraceToken)
	} else {
		members = p.createMissingList()
	}
	p.contextFlags = saveContextFlags
	var result store.NodeRef
	if modifiers != store.NoListRef && p.modifierFlags(modifiers)&ast.ModifierFlagsAmbient != 0 {
		p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	}
	if kind == ast.KindClassDeclaration {
		result = p.b.NewClassDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, name, typeParameters, heritageClauses, members)
	} else {
		result = p.b.NewClassExpression(p.flags(), int32(pos), int32(p.nodePos()), modifiers, name, typeParameters, heritageClauses, members)
	}
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	if p.b.View(result).Flags()&ast.NodeFlagsJavaScriptFile != 0 {
		p.checkJSSyntax(result)
		if heritageClauses != store.NoListRef {
			for _, clause := range p.b.ViewList(heritageClauses).Refs() {
				if p.b.View(clause).AsHeritageClause().Token() == ast.KindExtendsKeyword {
					for _, expr := range p.b.View(clause).AsHeritageClause().Types().Refs() {
						p.checkJSSyntax(expr)
					}
				}
			}
		}
	}
	return result
}

func (p *Parser) parseNameOfClassDeclarationOrExpression() store.NodeRef {
	// implements is a future reserved word so
	// 'class implements' might mean either
	// - class expression with omitted name, 'implements' starts heritage clause
	// - class with name 'implements'
	// 'isImplementsClause' helps to disambiguate between these two cases
	if p.isBindingIdentifier() && !p.isImplementsClause() {
		saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
		id := p.createIdentifier(p.isBindingIdentifier())
		p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
		return id
	}
	return store.NoNodeRef
}

func (p *Parser) isImplementsClause() bool {
	return p.token == ast.KindImplementsKeyword && p.lookAhead((*Parser).nextTokenIsIdentifierOrKeyword)
}

func isExportModifier(modifier store.Node) bool {
	return modifier.Kind() == ast.KindExportKeyword
}

func isAsyncModifier(modifier store.Node) bool {
	return modifier.Kind() == ast.KindAsyncKeyword
}

func (p *Parser) parseHeritageClauses(isInterface bool) store.ListRef {
	// ClassTail[Yield,Await] : (Modified) See 14.5
	//      ClassHeritage[?Yield,?Await]opt { ClassBody[?Yield,?Await]opt }
	if p.isHeritageClause() {
		return p.parseList(PCHeritageClauses, func(p *Parser) store.NodeRef {
			return p.parseHeritageClause(isInterface)
		})
	}
	return store.NoListRef
}

func (p *Parser) parseHeritageClause(isInterface bool) store.NodeRef {
	pos := p.nodePos()
	kind := p.token
	p.nextToken()
	parseElement := (*Parser).parseExpressionWithTypeArguments
	if isTypeHeritageClause(isInterface, kind) {
		parseElement = (*Parser).parseTypeHeritageClauseElement
	}
	types := p.parseDelimitedList(PCHeritageClauseElement, parseElement)
	return p.checkJSSyntax(p.b.NewHeritageClause(p.flags(), int32(pos), int32(p.nodePos()), kind, types))
}

func isTypeHeritageClause(isInterface bool, token ast.Kind) bool {
	return isInterface && token == ast.KindExtendsKeyword ||
		!isInterface && token == ast.KindImplementsKeyword
}

func (p *Parser) parseTypeHeritageClauseElement() store.NodeRef {
	pos := p.nodePos()
	expressionWithTypeArguments := p.parseExpressionWithTypeArguments()
	v := p.b.View(expressionWithTypeArguments).AsExpressionWithTypeArguments()
	if !isValidHeritageTypeReferenceExpression(v.Expression()) {
		return expressionWithTypeArguments
	}
	// The ExpressionWithTypeArguments node stays as a dead node.
	expression, typeArguments := v.Expression().Ref(), v.TypeArguments().Ref()
	typeName := p.convertEntityNameExpressionToEntityName(expression)
	return p.b.NewTypeReferenceNode(p.flags(), int32(pos), int32(p.nodePos()), typeName, typeArguments)
}

func isValidHeritageTypeReferenceExpression(node store.Node) bool {
	if node.Kind() == ast.KindIdentifier {
		return nodeIsPresent(node)
	}
	return node.Kind() == ast.KindPropertyAccessExpression &&
		!isOptionalChain(node) &&
		nodeIsPresent(node.Name()) &&
		isValidHeritageTypeReferenceExpression(node.Expression())
}

// convertEntityNameExpressionToEntityName reads the PropertyAccessExpression
// before the recursion appends; the PropertyAccessExpression nodes stay as
// dead nodes.
func (p *Parser) convertEntityNameExpressionToEntityName(node store.NodeRef) store.NodeRef {
	v := p.b.View(node)
	if v.Kind() == ast.KindIdentifier {
		return node
	}
	propertyAccess := v.AsPropertyAccessExpression()
	expression, name, pos, end := propertyAccess.Expression().Ref(), propertyAccess.Name().Ref(), v.Pos(), v.End()
	left := p.convertEntityNameExpressionToEntityName(expression)
	return p.b.NewQualifiedName(p.flags(), pos, end, left, name)
}

func (p *Parser) parseExpressionWithTypeArguments() store.NodeRef {
	pos := p.nodePos()
	expression := p.parseLeftHandSideExpressionOrHigher()
	if p.b.View(expression).Kind() == ast.KindExpressionWithTypeArguments {
		return expression
	}
	typeArguments := p.parseTypeArguments()
	return p.b.NewExpressionWithTypeArguments(p.flags(), int32(pos), int32(p.nodePos()), expression, typeArguments)
}

func (p *Parser) parseClassElement() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	if p.token == ast.KindSemicolonToken {
		p.nextToken()
		result := p.b.NewToken(ast.KindSemicolonClassElement, p.flags(), int32(pos), int32(p.nodePos()))
		p.b.AddFlags(result, p.jsdocFlags(jsdoc))
		return result
	}
	modifiers := p.parseModifiersEx(true /*allowDecorators*/, true /*permitConstAsModifier*/, true /*stopOnStartOfClassStaticBlock*/)
	if p.token == ast.KindStaticKeyword && p.lookAhead((*Parser).nextTokenIsOpenBrace) {
		return p.parseClassStaticBlockDeclaration(pos, jsdoc, modifiers)
	}
	if p.parseContextualModifier(ast.KindGetKeyword) {
		return p.parseAccessorDeclaration(pos, jsdoc, modifiers, ast.KindGetAccessor, ParseFlagsNone)
	}
	if p.parseContextualModifier(ast.KindSetKeyword) {
		return p.parseAccessorDeclaration(pos, jsdoc, modifiers, ast.KindSetAccessor, ParseFlagsNone)
	}
	if p.token == ast.KindConstructorKeyword || p.token == ast.KindStringLiteral {
		constructorDeclaration := p.tryParseConstructorDeclaration(pos, jsdoc, modifiers)
		if constructorDeclaration != store.NoNodeRef {
			return constructorDeclaration
		}
	}
	if p.isIndexSignature() {
		return p.checkJSSyntax(p.parseIndexSignatureDeclaration(pos, jsdoc, modifiers))
	}
	// It is very important that we check this *after* checking indexers because
	// the [ token can start an index signature or a computed property name
	if tokenIsIdentifierOrKeyword(p.token) || p.token == ast.KindStringLiteral || p.token == ast.KindNumericLiteral || p.token == ast.KindBigIntLiteral || p.token == ast.KindAsteriskToken || p.token == ast.KindOpenBracketToken {
		isAmbient := modifiers != store.NoListRef && p.someModifier(modifiers, isDeclareModifier)
		if isAmbient {
			for _, m := range p.b.ViewList(modifiers).Refs() {
				p.b.AddFlags(m, ast.NodeFlagsAmbient)
			}
			saveContextFlags := p.contextFlags
			p.setContextFlags(ast.NodeFlagsAmbient, true)
			result := p.parsePropertyOrMethodDeclaration(pos, jsdoc, modifiers)
			p.contextFlags = saveContextFlags
			return result
		} else {
			return p.parsePropertyOrMethodDeclaration(pos, jsdoc, modifiers)
		}
	}
	if modifiers != store.NoListRef {
		// treat this as a property declaration with a missing name.
		p.parseErrorAt(p.nodePos(), p.nodePos(), diagnostics.Declaration_expected)
		name := p.createMissingIdentifier()
		return p.parsePropertyDeclaration(pos, jsdoc, modifiers, name, store.NoNodeRef /*questionToken*/)
	}
	// 'isClassMemberStart' should have hinted not to attempt parsing.
	panic("Should not have attempted to parse class member declaration.")
}

func (p *Parser) parseClassStaticBlockDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	p.parseExpectedToken(ast.KindStaticKeyword)
	body := p.parseClassStaticBlockBody()
	result := p.b.NewClassStaticBlockDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, body)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseClassStaticBlockBody() store.NodeRef {
	saveContextFlags := p.contextFlags
	p.setContextFlags(ast.NodeFlagsYieldContext, false)
	p.setContextFlags(ast.NodeFlagsAwaitContext, true)
	body := p.parseBlock(false /*ignoreMissingOpenBrace*/, nil /*diagnosticMessage*/)
	p.contextFlags = saveContextFlags
	return body
}

func (p *Parser) tryParseConstructorDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	state := p.mark()
	if p.token == ast.KindConstructorKeyword || p.token == ast.KindStringLiteral && p.scanner.TokenValue() == "constructor" && p.lookAhead((*Parser).nextTokenIsOpenParen) {
		p.nextToken()
		typeParameters := p.parseTypeParameters()
		parameters := p.parseParameters(ParseFlagsNone)
		returnType := p.parseReturnType(ast.KindColonToken, false /*isType*/)
		body := p.parseFunctionBlockOrSemicolon(ParseFlagsNone, diagnostics.X_or_expected)
		result := p.b.NewConstructorDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, typeParameters, parameters, returnType, store.NoNodeRef /*fullSignature*/, body)
		p.b.AddFlags(result, p.jsdocFlags(jsdoc))
		p.checkJSSyntax(result)
		return result
	}
	p.rewind(state)
	return store.NoNodeRef
}

func (p *Parser) nextTokenIsOpenParen() bool {
	return p.nextToken() == ast.KindOpenParenToken
}

func (p *Parser) parsePropertyOrMethodDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	asteriskToken := p.parseOptionalToken(ast.KindAsteriskToken)
	name := p.parsePropertyName()
	// Note: this is not legal as per the grammar.  But we allow it in the parser and
	// report an error in the grammar checker.
	questionToken := p.parseOptionalToken(ast.KindQuestionToken)
	if asteriskToken != store.NoNodeRef || p.token == ast.KindOpenParenToken || p.token == ast.KindLessThanToken {
		return p.parseMethodDeclaration(pos, jsdoc, modifiers, asteriskToken, name, questionToken, diagnostics.X_or_expected)
	}
	return p.parsePropertyDeclaration(pos, jsdoc, modifiers, name, questionToken)
}

func (p *Parser) parseMethodDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef, asteriskToken store.NodeRef, name store.NodeRef, questionToken store.NodeRef, diagnosticMessage *diagnostics.Message) store.NodeRef {
	signatureFlags := core.IfElse(asteriskToken != store.NoNodeRef, ParseFlagsYield, ParseFlagsNone) | core.IfElse(p.modifierListHasAsync(modifiers), ParseFlagsAwait, ParseFlagsNone)
	typeParameters := p.parseTypeParameters()
	parameters := p.parseParameters(signatureFlags)
	typeNode := p.parseReturnType(ast.KindColonToken, false /*isType*/)
	body := p.parseFunctionBlockOrSemicolon(signatureFlags, diagnosticMessage)
	result := p.b.NewMethodDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, asteriskToken, name, questionToken, typeParameters, parameters, typeNode, store.NoNodeRef /*fullSignature*/, body)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	p.checkJSSyntax(result)
	return result
}

func (p *Parser) modifierListHasAsync(modifiers store.ListRef) bool {
	return modifiers != store.NoListRef && p.someModifier(modifiers, isAsyncModifier)
}

func (p *Parser) parsePropertyDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef, name store.NodeRef, questionToken store.NodeRef) store.NodeRef {
	postfixToken := questionToken
	if postfixToken == store.NoNodeRef && !p.hasPrecedingLineBreak() {
		postfixToken = p.parseOptionalToken(ast.KindExclamationToken)
	}
	typeNode := p.parseTypeAnnotation()
	initializer := p.doInContext(ast.NodeFlagsYieldContext|ast.NodeFlagsAwaitContext|ast.NodeFlagsDisallowInContext, false, (*Parser).parseInitializer)
	p.parseSemicolonAfterPropertyName(name, typeNode, initializer)
	result := p.b.NewPropertyDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, name, postfixToken, typeNode, initializer)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	p.checkJSSyntax(result)
	return result
}

func (p *Parser) parseSemicolonAfterPropertyName(name store.NodeRef, typeNode store.NodeRef, initializer store.NodeRef) {
	if p.token == ast.KindAtToken && !p.hasPrecedingLineBreak() {
		p.parseErrorAtCurrentToken(diagnostics.Decorators_must_precede_the_name_and_all_keywords_of_property_declarations)
		return
	}
	if p.token == ast.KindOpenParenToken {
		p.parseErrorAtCurrentToken(diagnostics.Cannot_start_a_function_call_in_a_type_annotation)
		p.nextToken()
		return
	}
	if typeNode != store.NoNodeRef && !p.canParseSemicolon() {
		if initializer != store.NoNodeRef {
			p.parseErrorAtCurrentToken(diagnostics.X_0_expected, scanner.TokenToString(ast.KindSemicolonToken))
		} else {
			p.parseErrorAtCurrentToken(diagnostics.Expected_for_property_initializer)
		}
		return
	}
	if p.tryParseSemicolon() {
		return
	}
	if initializer != store.NoNodeRef {
		p.parseErrorAtCurrentToken(diagnostics.X_0_expected, scanner.TokenToString(ast.KindSemicolonToken))
		return
	}
	p.parseErrorForMissingSemicolonAfter(name)
}

func (p *Parser) parseErrorForMissingSemicolonAfter(node store.NodeRef) {
	// Tagged template literals are sometimes used in places where only simple strings are allowed, i.e.:
	//   module `M1` {
	//   ^^^^^^^^^^^ This block is parsed as a template literal like module`M1`.
	if p.b.View(node).Kind() == ast.KindTaggedTemplateExpression {
		p.parseErrorAtRange(p.skipRangeTrivia(p.loc(p.b.View(node).AsTaggedTemplateExpression().Template().Ref())), diagnostics.Module_declaration_names_may_only_use_or_quoted_strings)
		return
	}
	// Otherwise, if this isn't a well-known keyword-like identifier, give the generic fallback message.
	var expressionText string
	if p.b.View(node).Kind() == ast.KindIdentifier {
		expressionText = p.b.View(node).Text()
	}
	if expressionText == "" {
		p.parseErrorAtCurrentToken(diagnostics.X_0_expected, scanner.TokenToString(ast.KindSemicolonToken))
		return
	}
	pos := scanner.SkipTrivia(p.sourceText, p.pos(node))
	// Some known keywords are likely signs of syntax being used improperly.
	switch expressionText {
	case "const", "let", "var":
		p.parseErrorAt(pos, p.end(node), diagnostics.Variable_declaration_not_allowed_at_this_location)
		return
	case "declare":
		// If a declared node failed to parse, it would have emitted a diagnostic already.
		return
	case "interface":
		p.parseErrorForInvalidName(diagnostics.Interface_name_cannot_be_0, diagnostics.Interface_must_be_given_a_name, ast.KindOpenBraceToken)
		return
	case "is":
		p.parseErrorAt(pos, p.scanner.TokenStart(), diagnostics.A_type_predicate_is_only_allowed_in_return_type_position_for_functions_and_methods)
		return
	case "module", "namespace":
		p.parseErrorForInvalidName(diagnostics.Namespace_name_cannot_be_0, diagnostics.Namespace_must_be_given_a_name, ast.KindOpenBraceToken)
		return
	case "type":
		p.parseErrorForInvalidName(diagnostics.Type_alias_name_cannot_be_0, diagnostics.Type_alias_must_be_given_a_name, ast.KindEqualsToken)
		return
	}
	// The user alternatively might have misspelled or forgotten to add a space after a common keyword.
	suggestion := core.GetSpellingSuggestionForStrings(expressionText, slices.Values(viableKeywordSuggestions))
	if suggestion == "" {
		suggestion = getSpaceSuggestion(expressionText)
	}
	if suggestion != "" {
		p.parseErrorAt(pos, p.end(node), diagnostics.Unknown_keyword_or_identifier_Did_you_mean_0, suggestion)
		return
	}
	// Unknown tokens are handled with their own errors in the scanner
	if p.token == ast.KindUnknown {
		return
	}
	// Otherwise, we know this some kind of unknown word, not just a missing expected semicolon.
	p.parseErrorAt(pos, p.end(node), diagnostics.Unexpected_keyword_or_identifier)
}

func getSpaceSuggestion(expressionText string) string {
	for _, keyword := range viableKeywordSuggestions {
		if len(expressionText) > len(keyword)+2 && strings.HasPrefix(expressionText, keyword) {
			return keyword + " " + expressionText[len(keyword):]
		}
	}
	return ""
}

func (p *Parser) parseErrorForInvalidName(nameDiagnostic *diagnostics.Message, blankDiagnostic *diagnostics.Message, tokenIfBlankName ast.Kind) {
	if p.token == tokenIfBlankName {
		p.parseErrorAtCurrentToken(blankDiagnostic)
	} else {
		p.parseErrorAtCurrentToken(nameDiagnostic, p.scanner.TokenValue())
	}
}

func (p *Parser) parseInterfaceDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	p.parseExpected(ast.KindInterfaceKeyword)
	name := p.parseIdentifier()
	typeParameters := p.parseTypeParameters()
	heritageClauses := p.parseHeritageClauses(true /*isInterface*/)
	members := p.parseObjectTypeMembers()
	result := p.b.NewInterfaceDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, name, typeParameters, heritageClauses, members)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	p.checkJSSyntax(result)
	return result
}

func (p *Parser) parseTypeAliasDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	p.parseExpected(ast.KindTypeKeyword)
	if p.hasPrecedingLineBreak() {
		p.parseErrorAtCurrentToken(diagnostics.Line_break_not_permitted_here)
	}
	name := p.parseIdentifier()
	typeParameters := p.parseTypeParameters()
	p.parseExpected(ast.KindEqualsToken)
	var typeNode store.NodeRef
	if p.token == ast.KindIntrinsicKeyword && p.lookAhead((*Parser).nextIsNotDot) {
		typeNode = p.parseKeywordTypeNode()
	} else {
		typeNode = p.parseType()
	}
	p.parseSemicolon()
	result := p.b.NewTypeAliasDeclaration(ast.KindTypeAliasDeclaration, p.flags(), int32(pos), int32(p.nodePos()), modifiers, name, typeParameters, typeNode)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	p.checkJSSyntax(result)
	return result
}

func (p *Parser) nextIsNotDot() bool {
	return p.nextToken() != ast.KindDotToken
}

// In an ambient declaration, the grammar only allows integer literals as initializers.
// In a non-ambient declaration, the grammar allows uninitialized members only in a
// ConstantEnumMemberSection, which starts at the beginning of an enum declaration
// or any time an integer literal initializer is encountered.
func (p *Parser) parseEnumMember() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	name := p.parsePropertyName()
	initializer := p.doInContext(ast.NodeFlagsDisallowInContext, false, (*Parser).parseInitializer)
	result := p.b.NewEnumMember(p.flags(), int32(pos), int32(p.nodePos()), name, initializer)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseEnumDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	p.parseExpected(ast.KindEnumKeyword)
	name := p.parseIdentifier()
	var members store.ListRef
	if p.parseExpected(ast.KindOpenBraceToken) {
		saveContextFlags := p.contextFlags
		p.setContextFlags(ast.NodeFlagsYieldContext|ast.NodeFlagsAwaitContext, false)
		members = p.parseDelimitedList(PCEnumMembers, (*Parser).parseEnumMember)
		p.contextFlags = saveContextFlags
		p.parseExpected(ast.KindCloseBraceToken)
	} else {
		members = p.createMissingList()
	}
	result := p.b.NewEnumDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, name, members)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	p.checkJSSyntax(result)
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	return result
}

func (p *Parser) parseModuleDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	keyword := ast.KindModuleKeyword
	if p.token == ast.KindGlobalKeyword {
		// global augmentation
		return p.parseAmbientExternalModuleDeclaration(pos, jsdoc, modifiers)
	} else if p.parseOptional(ast.KindNamespaceKeyword) {
		keyword = ast.KindNamespaceKeyword
	} else {
		p.parseExpected(ast.KindModuleKeyword)
		if p.token == ast.KindStringLiteral {
			return p.parseAmbientExternalModuleDeclaration(pos, jsdoc, modifiers)
		}
	}
	return p.parseModuleOrNamespaceDeclaration(pos, jsdoc, modifiers, false /*nested*/, keyword)
}

func (p *Parser) parseAmbientExternalModuleDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	var name store.NodeRef
	keyword := ast.KindModuleKeyword
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	if p.token == ast.KindGlobalKeyword {
		// parse 'global' as name of global scope augmentation
		name = p.parseIdentifier()
		keyword = ast.KindGlobalKeyword
	} else {
		// parse string literal
		name = p.parseLiteralExpression()
	}
	var attributes store.NodeRef
	if keyword == ast.KindModuleKeyword && p.parseOptional(ast.KindWithKeyword) {
		attributes = p.parseTypeLiteral()
	}
	var body store.NodeRef
	if p.token == ast.KindOpenBraceToken {
		body = p.parseModuleBlock()
	} else {
		p.parseSemicolon()
	}
	result := p.b.NewModuleDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, keyword, name, attributes, body)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	return result
}

func (p *Parser) parseModuleBlock() store.NodeRef {
	pos := p.nodePos()
	var statements store.ListRef
	if p.parseExpected(ast.KindOpenBraceToken) {
		statements = p.parseList(PCBlockStatements, (*Parser).parseStatement)
		p.parseExpected(ast.KindCloseBraceToken)
	} else {
		statements = p.createMissingList()
	}
	return p.b.NewModuleBlock(p.flags(), int32(pos), int32(p.nodePos()), statements)
}

func (p *Parser) parseModuleOrNamespaceDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef, nested bool, keyword ast.Kind) store.NodeRef {
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	var name store.NodeRef
	if nested {
		name = p.parseIdentifierName()
	} else {
		name = p.parseIdentifier()
	}
	var body store.NodeRef
	if p.parseOptional(ast.KindDotToken) {
		implicitExport := p.b.NewToken(ast.KindExportKeyword, ast.NodeFlagsReparsed, int32(p.nodePos()), int32(p.nodePos()))
		implicitModifiers := p.b.List(int32(p.nodePos()), int32(p.nodePos()), []store.NodeRef{implicitExport})
		body = p.parseModuleOrNamespaceDeclaration(p.nodePos(), 0 /*jsdoc*/, implicitModifiers, true /*nested*/, keyword)
	} else {
		body = p.parseModuleBlock()
	}
	result := p.b.NewModuleDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, keyword, name, store.NoNodeRef, body)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	p.checkJSSyntax(result)
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	return result
}

func (p *Parser) parseImportDeclarationOrImportEqualsDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	p.parseExpected(ast.KindImportKeyword)
	afterImportPos := p.nodePos()
	// We don't parse the identifier here in await context, instead we will report a grammar error in the checker.
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	var identifier store.NodeRef
	if p.isIdentifier() {
		identifier = p.parseIdentifier()
	}
	phaseModifier := ast.KindUnknown
	if identifier != store.NoNodeRef && p.b.View(identifier).Text() == "type" &&
		(p.token != ast.KindFromKeyword || p.isIdentifier() && p.lookAhead((*Parser).nextTokenIsFromKeywordOrEqualsToken)) &&
		(p.isIdentifier() || p.tokenAfterImportDefinitelyProducesImportDeclaration()) {
		phaseModifier = ast.KindTypeKeyword
		identifier = store.NoNodeRef
		if p.isIdentifier() {
			identifier = p.parseIdentifier()
		}
	} else if identifier != store.NoNodeRef && p.b.View(identifier).Text() == "defer" {
		var shouldParseAsDeferModifier bool
		if p.token == ast.KindFromKeyword {
			shouldParseAsDeferModifier = !p.lookAhead((*Parser).nextTokenIsTokenStringLiteral)
		} else {
			shouldParseAsDeferModifier = p.token != ast.KindCommaToken && p.token != ast.KindEqualsToken
		}
		if shouldParseAsDeferModifier {
			phaseModifier = ast.KindDeferKeyword
			identifier = store.NoNodeRef
			if p.isIdentifier() {
				identifier = p.parseIdentifier()
			}
		}
	}
	if identifier != store.NoNodeRef && !p.tokenAfterImportedIdentifierDefinitelyProducesImportDeclaration() && phaseModifier != ast.KindDeferKeyword {
		importEquals := p.checkJSSyntax(p.parseImportEqualsDeclaration(pos, jsdoc, modifiers, identifier, phaseModifier == ast.KindTypeKeyword))
		p.statementHasAwaitIdentifier = saveHasAwaitIdentifier // Import= declaration is always parsed in an Await context, no need to reparse
		return importEquals
	}
	importClause := p.tryParseImportClause(identifier, afterImportPos, phaseModifier, false /*skipJSDocLeadingAsterisks*/)
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier // import clause is always parsed in an Await context
	moduleSpecifier := p.parseModuleSpecifier()
	attributes := p.tryParseImportAttributes()
	p.parseSemicolon()
	result := p.b.NewImportDeclaration(ast.KindImportDeclaration, p.flags(), int32(pos), int32(p.nodePos()), modifiers, importClause, moduleSpecifier, attributes)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	p.checkJSSyntax(result)
	return result
}

func (p *Parser) nextTokenIsFromKeywordOrEqualsToken() bool {
	p.nextToken()
	return p.token == ast.KindFromKeyword || p.token == ast.KindEqualsToken
}

func (p *Parser) tokenAfterImportDefinitelyProducesImportDeclaration() bool {
	return p.token == ast.KindAsteriskToken || p.token == ast.KindOpenBraceToken
}

func (p *Parser) tokenAfterImportedIdentifierDefinitelyProducesImportDeclaration() bool {
	// In `import id ___`, the current token decides whether to produce
	// an ImportDeclaration or ImportEqualsDeclaration.
	return p.token == ast.KindCommaToken || p.token == ast.KindFromKeyword
}

func (p *Parser) parseImportEqualsDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef, identifier store.NodeRef, isTypeOnly bool) store.NodeRef {
	p.parseExpected(ast.KindEqualsToken)
	moduleReference := p.parseModuleReference()
	p.parseSemicolon()
	result := p.b.NewImportEqualsDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, isTypeOnly, identifier, moduleReference)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseModuleReference() store.NodeRef {
	if p.token == ast.KindRequireKeyword && p.lookAhead((*Parser).nextTokenIsOpenParen) {
		return p.parseExternalModuleReference()
	}
	return p.parseEntityName(false /*allowReservedWords*/, false /*allowPrivateName*/, nil /*diagnosticMessage*/)
}

func (p *Parser) parseExternalModuleReference() store.NodeRef {
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	pos := p.nodePos()
	p.parseExpected(ast.KindRequireKeyword)
	p.parseExpected(ast.KindOpenParenToken)
	expression := p.parseModuleSpecifier()
	p.parseExpected(ast.KindCloseParenToken)
	result := p.b.NewExternalModuleReference(p.flags(), int32(pos), int32(p.nodePos()), expression)
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	return result
}

func (p *Parser) parseModuleSpecifier() store.NodeRef {
	if p.token == ast.KindStringLiteral {
		return p.parseLiteralExpression()
	}
	// We allow arbitrary expressions here, even though the grammar only allows string
	// literals.  We check to ensure that it is only a string literal later in the grammar
	// check pass.
	return p.parseExpression()
}

func (p *Parser) tryParseImportClause(identifier store.NodeRef, pos int, phaseModifier ast.Kind, skipJSDocLeadingAsterisks bool) store.NodeRef {
	// ImportDeclaration:
	//  import ImportClause from ModuleSpecifier ;
	//  import ModuleSpecifier;
	if identifier != store.NoNodeRef || p.token == ast.KindAsteriskToken || p.token == ast.KindOpenBraceToken {
		importClause := p.parseImportClause(identifier, pos, phaseModifier, skipJSDocLeadingAsterisks)
		p.parseExpected(ast.KindFromKeyword)
		return importClause
	}
	return store.NoNodeRef
}

func (p *Parser) parseImportClause(identifier store.NodeRef, pos int, phaseModifier ast.Kind, skipJSDocLeadingAsterisks bool) store.NodeRef {
	// ImportClause:
	//  ImportedDefaultBinding
	//  NameSpaceImport
	//  NamedImports
	//  ImportedDefaultBinding, NameSpaceImport
	//  ImportedDefaultBinding, NamedImports
	// If there was no default import or if there is comma token after default import
	// parse namespace or named imports
	var namedBindings store.NodeRef
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	if identifier == store.NoNodeRef || p.parseOptional(ast.KindCommaToken) {
		if skipJSDocLeadingAsterisks {
			p.scanner.SetSkipJSDocLeadingAsterisks(true)
		}
		if p.token == ast.KindAsteriskToken {
			namedBindings = p.parseNamespaceImport()
		} else {
			namedBindings = p.parseNamedImports()
		}
		if skipJSDocLeadingAsterisks {
			p.scanner.SetSkipJSDocLeadingAsterisks(false)
		}
	}
	result := p.b.NewImportClause(p.flags(), int32(pos), int32(p.nodePos()), phaseModifier, identifier, namedBindings)
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	return result
}

func (p *Parser) parseNamespaceImport() store.NodeRef {
	// NameSpaceImport:
	//  * as ImportedBinding
	pos := p.nodePos()
	p.parseExpected(ast.KindAsteriskToken)
	p.parseExpected(ast.KindAsKeyword)
	name := p.parseIdentifier()
	return p.b.NewNamespaceImport(p.flags(), int32(pos), int32(p.nodePos()), name)
}

func (p *Parser) parseNamedImports() store.NodeRef {
	pos := p.nodePos()
	// NamedImports:
	//  { }
	//  { ImportsList }
	//  { ImportsList, }
	imports := p.parseBracketedList(PCImportOrExportSpecifiers, (*Parser).parseImportSpecifier, ast.KindOpenBraceToken, ast.KindCloseBraceToken)
	return p.b.NewNamedImports(p.flags(), int32(pos), int32(p.nodePos()), imports)
}

func (p *Parser) parseImportSpecifier() store.NodeRef {
	pos := p.nodePos()
	isTypeOnly, propertyName, name := p.parseImportOrExportSpecifier(ast.KindImportSpecifier)
	var identifierName store.NodeRef
	if p.b.View(name).Kind() == ast.KindIdentifier {
		identifierName = name
	} else {
		p.parseErrorAtRange(p.skipRangeTrivia(p.loc(name)), diagnostics.Identifier_expected)
		identifierName = p.newIdentifier("", p.pos(name), p.nodePos())
	}
	result := p.checkJSSyntax(p.b.NewImportSpecifier(p.flags(), int32(pos), int32(p.nodePos()), isTypeOnly, propertyName, identifierName))
	return result
}

func (p *Parser) parseImportOrExportSpecifier(kind ast.Kind) (isTypeOnly bool, propertyName store.NodeRef, name store.NodeRef) {
	// ImportSpecifier:
	//   BindingIdentifier
	//   ModuleExportName as BindingIdentifier
	// ExportSpecifier:
	//   ModuleExportName
	//   ModuleExportName as ModuleExportName
	// let checkIdentifierIsKeyword = isKeyword(token()) && !isIdentifier();
	// let checkIdentifierStart = scanner.getTokenStart();
	// let checkIdentifierEnd = scanner.getTokenEnd();
	canParseAsKeyword := true
	disallowKeywords := kind == ast.KindImportSpecifier
	var nameOk bool
	name, nameOk = p.parseModuleExportName(disallowKeywords)
	if p.b.View(name).Kind() == ast.KindIdentifier && p.b.View(name).Text() == "type" {
		// If the first token of an import specifier is 'type', there are a lot of possibilities,
		// especially if we see 'as' afterwards:
		//
		// import { type } from "mod";          - isTypeOnly: false,   name: type
		// import { type as } from "mod";       - isTypeOnly: true,    name: as
		// import { type as as } from "mod";    - isTypeOnly: false,   name: as,    propertyName: type
		// import { type as as as } from "mod"; - isTypeOnly: true,    name: as,    propertyName: as
		if p.token == ast.KindAsKeyword {
			// { type as ...? }
			firstAs := p.parseIdentifierName()
			if p.token == ast.KindAsKeyword {
				// { type as as ...? }
				secondAs := p.parseIdentifierName()
				if p.canParseModuleExportName() {
					// { type as as something }
					// { type as as "something" }
					isTypeOnly = true
					propertyName = firstAs
					name, nameOk = p.parseModuleExportName(disallowKeywords)
					canParseAsKeyword = false
				} else {
					// { type as as }
					propertyName = name
					name = secondAs
					canParseAsKeyword = false
				}
			} else if p.canParseModuleExportName() {
				// { type as something }
				// { type as "something" }
				propertyName = name
				canParseAsKeyword = false
				name, nameOk = p.parseModuleExportName(disallowKeywords)
			} else {
				// { type as }
				isTypeOnly = true
				name = firstAs
			}
		} else if p.canParseModuleExportName() {
			// { type something ...? }
			// { type "something" ...? }
			isTypeOnly = true
			name, nameOk = p.parseModuleExportName(disallowKeywords)
		}
	}
	if canParseAsKeyword && p.token == ast.KindAsKeyword {
		propertyName = name
		p.parseExpected(ast.KindAsKeyword)
		name, nameOk = p.parseModuleExportName(disallowKeywords)
	}

	if !nameOk {
		p.parseErrorAtRange(p.skipRangeTrivia(p.loc(name)), diagnostics.Identifier_expected)
	}

	return isTypeOnly, propertyName, name
}

func (p *Parser) canParseModuleExportName() bool {
	return tokenIsIdentifierOrKeyword(p.token) || p.token == ast.KindStringLiteral
}

func (p *Parser) parseModuleExportName(disallowKeywords bool) (node store.NodeRef, nameOk bool) {
	nameOk = true

	if p.token == ast.KindStringLiteral {
		return p.parseLiteralExpression(), nameOk
	}
	if disallowKeywords && ast.IsKeyword(p.token) && !p.isIdentifier() {
		nameOk = false
	}
	return p.parseIdentifierName(), nameOk
}

func (p *Parser) tryParseImportAttributes() store.NodeRef {
	if p.token == ast.KindWithKeyword || (p.token == ast.KindAssertKeyword && !p.hasPrecedingLineBreak()) {
		if p.token == ast.KindAssertKeyword {
			p.parseErrorAtCurrentToken(diagnostics.Import_assertions_have_been_replaced_by_import_attributes_Use_with_instead_of_assert)
		}
		return p.parseImportAttributes(p.token, false /*skipKeyword*/)
	}
	return store.NoNodeRef
}

func (p *Parser) parseExportAssignment(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	saveContextFlags := p.contextFlags
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	p.setContextFlags(ast.NodeFlagsAwaitContext, true)
	isExportEquals := false
	if p.parseOptional(ast.KindEqualsToken) {
		isExportEquals = true
	} else {
		p.parseExpected(ast.KindDefaultKeyword)
	}
	expression := p.parseAssignmentExpressionOrHigher()
	p.parseSemicolon()
	p.contextFlags = saveContextFlags
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	result := p.b.NewExportAssignment(p.flags(), int32(pos), int32(p.nodePos()), modifiers, isExportEquals, store.NoNodeRef /*typeNode*/, expression)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	p.checkJSSyntax(result)
	return result
}

func (p *Parser) parseNamespaceExportDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	p.parseExpected(ast.KindAsKeyword)
	p.parseExpected(ast.KindNamespaceKeyword)
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	name := p.parseIdentifier()
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	p.parseSemicolon()
	// NamespaceExportDeclaration nodes cannot have decorators or modifiers, we attach them here so we can report them in the grammar checker
	result := p.b.NewNamespaceExportDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, name)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseExportDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	saveContextFlags := p.contextFlags
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	p.setContextFlags(ast.NodeFlagsAwaitContext, true)
	var exportClause store.NodeRef
	var moduleSpecifier store.NodeRef
	var attributes store.NodeRef
	isTypeOnly := p.parseOptional(ast.KindTypeKeyword)
	namespaceExportPos := p.nodePos()
	if p.parseOptional(ast.KindAsteriskToken) {
		if p.parseOptional(ast.KindAsKeyword) {
			exportClause = p.parseNamespaceExport(namespaceExportPos)
		}
		p.parseExpected(ast.KindFromKeyword)
		moduleSpecifier = p.parseModuleSpecifier()
	} else {
		exportClause = p.parseNamedExports()
		// It is not uncommon to accidentally omit the 'from' keyword. Additionally, in editing scenarios,
		// the 'from' keyword can be parsed as a named export when the export clause is unterminated (i.e. `export { from "moduleName";`)
		// If we don't have a 'from' keyword, see if we have a string literal such that ASI won't take effect.
		if p.token == ast.KindFromKeyword || (p.token == ast.KindStringLiteral && !p.hasPrecedingLineBreak()) {
			p.parseExpected(ast.KindFromKeyword)
			moduleSpecifier = p.parseModuleSpecifier()
		}
	}
	if moduleSpecifier != store.NoNodeRef && (p.token == ast.KindWithKeyword || p.token == ast.KindAssertKeyword) && !p.hasPrecedingLineBreak() {
		if p.token == ast.KindAssertKeyword {
			p.parseErrorAtCurrentToken(diagnostics.Import_assertions_have_been_replaced_by_import_attributes_Use_with_instead_of_assert)
		}
		attributes = p.parseImportAttributes(p.token, false /*skipKeyword*/)
	}
	p.parseSemicolon()
	p.contextFlags = saveContextFlags
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	result := p.b.NewExportDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, isTypeOnly, exportClause, moduleSpecifier, attributes)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	p.checkJSSyntax(result)
	return result
}

func (p *Parser) parseNamespaceExport(pos int) store.NodeRef {
	exportName, _ := p.parseModuleExportName(false /*disallowKeywords*/)
	return p.b.NewNamespaceExport(p.flags(), int32(pos), int32(p.nodePos()), exportName)
}

func (p *Parser) parseNamedExports() store.NodeRef {
	pos := p.nodePos()
	// NamedImports:
	//  { }
	//  { ImportsList }
	//  { ImportsList, }
	exports := p.parseBracketedList(PCImportOrExportSpecifiers, (*Parser).parseExportSpecifier, ast.KindOpenBraceToken, ast.KindCloseBraceToken)
	return p.b.NewNamedExports(p.flags(), int32(pos), int32(p.nodePos()), exports)
}

func (p *Parser) parseExportSpecifier() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	isTypeOnly, propertyName, name := p.parseImportOrExportSpecifier(ast.KindExportSpecifier)
	result := p.b.NewExportSpecifier(p.flags(), int32(pos), int32(p.nodePos()), isTypeOnly, propertyName, name)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	p.checkJSSyntax(result)
	return result
}

// TYPES

func (p *Parser) parseType() store.NodeRef {
	saveContextFlags := p.contextFlags
	p.setContextFlags(ast.NodeFlagsTypeExcludesFlags, false)
	var typeNode store.NodeRef
	if p.isStartOfFunctionTypeOrConstructorType() {
		typeNode = p.parseFunctionOrConstructorType()
	} else {
		pos := p.nodePos()
		typeNode = p.parseUnionTypeOrHigher()
		if !p.inDisallowConditionalTypesContext() && !p.hasPrecedingLineBreak() && p.parseOptional(ast.KindExtendsKeyword) {
			// The type following 'extends' is not permitted to be another conditional type
			extendsType := p.doInContext(ast.NodeFlagsDisallowConditionalTypesContext, true, (*Parser).parseType)
			p.parseExpected(ast.KindQuestionToken)
			trueType := p.doInContext(ast.NodeFlagsDisallowConditionalTypesContext, false, (*Parser).parseType)
			p.parseExpected(ast.KindColonToken)
			falseType := p.doInContext(ast.NodeFlagsDisallowConditionalTypesContext, false, (*Parser).parseType)
			conditionalType := p.b.NewConditionalTypeNode(p.flags(), int32(pos), int32(p.nodePos()), typeNode, extendsType, trueType, falseType)
			typeNode = conditionalType
		}
	}
	p.contextFlags = saveContextFlags
	return typeNode
}

func (p *Parser) parseUnionTypeOrHigher() store.NodeRef {
	return p.parseUnionOrIntersectionType(ast.KindBarToken, (*Parser).parseIntersectionTypeOrHigher)
}

func (p *Parser) parseIntersectionTypeOrHigher() store.NodeRef {
	return p.parseUnionOrIntersectionType(ast.KindAmpersandToken, (*Parser).parseTypeOperatorOrHigher)
}

func (p *Parser) parseUnionOrIntersectionType(operator ast.Kind, parseConstituentType func(p *Parser) store.NodeRef) store.NodeRef {
	pos := p.nodePos()
	isUnionType := operator == ast.KindBarToken
	hasLeadingOperator := p.parseOptional(operator)
	var typeNode store.NodeRef
	if hasLeadingOperator {
		typeNode = p.parseFunctionOrConstructorTypeToError(isUnionType, parseConstituentType)
	} else {
		typeNode = parseConstituentType(p)
	}
	if p.token == operator || hasLeadingOperator {
		mark := len(p.elems)
		p.elems = append(p.elems, typeNode)
		for p.parseOptional(operator) {
			constituent := p.parseFunctionOrConstructorTypeToError(isUnionType, parseConstituentType)
			p.elems = append(p.elems, constituent)
		}
		types := p.b.List(int32(pos), int32(p.nodePos()), p.elems[mark:])
		p.elems = p.elems[:mark]
		typeNode = p.createUnionOrIntersectionTypeNode(operator, pos, types)
	}
	return typeNode
}

func (p *Parser) createUnionOrIntersectionTypeNode(operator ast.Kind, pos int, types store.ListRef) store.NodeRef {
	switch operator {
	case ast.KindBarToken:
		return p.b.NewUnionTypeNode(p.flags(), int32(pos), int32(p.nodePos()), types)
	case ast.KindAmpersandToken:
		return p.b.NewIntersectionTypeNode(p.flags(), int32(pos), int32(p.nodePos()), types)
	default:
		panic("Unhandled case in createUnionOrIntersectionType")
	}
}

func (p *Parser) parseTypeOperatorOrHigher() store.NodeRef {
	operator := p.token
	switch operator {
	case ast.KindKeyOfKeyword, ast.KindUniqueKeyword, ast.KindReadonlyKeyword:
		return p.parseTypeOperator(operator)
	case ast.KindInferKeyword:
		return p.parseInferType()
	}
	return p.doInContext(ast.NodeFlagsDisallowConditionalTypesContext, false, (*Parser).parsePostfixTypeOrHigher)
}

func (p *Parser) parseTypeOperator(operator ast.Kind) store.NodeRef {
	pos := p.nodePos()
	p.parseExpected(operator)
	typeNode := p.parseTypeOperatorOrHigher()
	return p.b.NewTypeOperatorNode(p.flags(), int32(pos), int32(p.nodePos()), operator, typeNode)
}

func (p *Parser) parseInferType() store.NodeRef {
	pos := p.nodePos()
	p.parseExpected(ast.KindInferKeyword)
	typeParameter := p.parseTypeParameterOfInferType()
	return p.b.NewInferTypeNode(p.flags(), int32(pos), int32(p.nodePos()), typeParameter)
}

func (p *Parser) parseTypeParameterOfInferType() store.NodeRef {
	pos := p.nodePos()
	name := p.parseIdentifier()
	constraint := p.tryParseConstraintOfInferType()
	return p.b.NewTypeParameterDeclaration(p.flags(), int32(pos), int32(p.nodePos()), store.NoListRef /*modifiers*/, name, constraint, store.NoNodeRef /*expression*/, store.NoNodeRef /*defaultType*/)
}

func (p *Parser) tryParseConstraintOfInferType() store.NodeRef {
	state := p.mark()
	if p.parseOptional(ast.KindExtendsKeyword) {
		constraint := p.doInContext(ast.NodeFlagsDisallowConditionalTypesContext, true, (*Parser).parseType)
		if p.inDisallowConditionalTypesContext() || p.token != ast.KindQuestionToken {
			return constraint
		}
	}
	p.rewind(state)
	return store.NoNodeRef
}

func (p *Parser) parsePostfixTypeOrHigher() store.NodeRef {
	pos := p.nodePos()
	typeNode := p.parseNonArrayType()
	for !p.hasPrecedingLineBreak() {
		switch p.token {
		case ast.KindExclamationToken:
			p.nextToken()
			typeNode = p.b.NewJSDocNonNullableType(p.flags(), int32(pos), int32(p.nodePos()), typeNode)
		case ast.KindQuestionToken:
			// If next token is start of a type we have a conditional type
			if p.lookAhead((*Parser).nextIsStartOfType) {
				return typeNode
			}
			p.nextToken()
			typeNode = p.b.NewJSDocNullableType(p.flags(), int32(pos), int32(p.nodePos()), typeNode)
		case ast.KindOpenBracketToken:
			p.parseExpected(ast.KindOpenBracketToken)
			if p.isStartOfType(false /*isStartOfParameter*/) {
				indexType := p.parseType()
				p.parseExpected(ast.KindCloseBracketToken)
				typeNode = p.b.NewIndexedAccessTypeNode(p.flags(), int32(pos), int32(p.nodePos()), typeNode, indexType)
			} else {
				p.parseExpected(ast.KindCloseBracketToken)
				typeNode = p.b.NewArrayTypeNode(p.flags(), int32(pos), int32(p.nodePos()), typeNode)
			}
		default:
			return typeNode
		}
	}
	return typeNode
}

func (p *Parser) nextIsStartOfType() bool {
	p.nextToken()
	return p.isStartOfType(false /*inStartOfParameter*/)
}

func (p *Parser) parseNonArrayType() store.NodeRef {
	switch p.token {
	case ast.KindAnyKeyword, ast.KindUnknownKeyword, ast.KindStringKeyword, ast.KindNumberKeyword, ast.KindBigIntKeyword,
		ast.KindSymbolKeyword, ast.KindBooleanKeyword, ast.KindUndefinedKeyword, ast.KindNeverKeyword, ast.KindObjectKeyword:
		state := p.mark()
		keywordTypeNode := p.parseKeywordTypeNode()
		// If these are followed by a dot then parse these out as a dotted type reference instead
		if p.token != ast.KindDotToken {
			return keywordTypeNode
		}
		p.rewind(state)
		return p.parseTypeReference()
	case ast.KindAsteriskEqualsToken:
		// If there is '*=', treat it as * followed by postfix =
		p.scanner.ReScanAsteriskEqualsToken()
		fallthrough
	case ast.KindAsteriskToken:
		return p.parseJSDocAllType()
	case ast.KindQuestionQuestionToken:
		// If there is '??', treat it as prefix-'?' in JSDoc type.
		p.scanner.ReScanQuestionToken()
		fallthrough
	case ast.KindQuestionToken:
		return p.parseJSDocNullableType()
	case ast.KindExclamationToken:
		return p.parseJSDocNonNullableType()
	case ast.KindNoSubstitutionTemplateLiteral, ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindTrueKeyword,
		ast.KindFalseKeyword, ast.KindNullKeyword:
		return p.parseLiteralTypeNode(false /*negative*/)
	case ast.KindMinusToken:
		if p.lookAhead((*Parser).nextTokenIsNumericOrBigIntLiteral) {
			return p.parseLiteralTypeNode(true /*negative*/)
		}
		return p.parseTypeReference()
	case ast.KindVoidKeyword:
		return p.parseKeywordTypeNode()
	case ast.KindThisKeyword:
		thisKeyword := p.parseThisTypeNode()
		if p.token == ast.KindIsKeyword && !p.hasPrecedingLineBreak() {
			return p.parseThisTypePredicate(thisKeyword)
		}
		return thisKeyword
	case ast.KindTypeOfKeyword:
		if p.lookAhead((*Parser).nextIsStartOfTypeOfImportType) {
			return p.parseImportType()
		}
		return p.parseTypeQuery()
	case ast.KindOpenBraceToken:
		if p.lookAhead((*Parser).nextIsStartOfMappedType) {
			return p.parseMappedType()
		}
		return p.parseTypeLiteral()
	case ast.KindOpenBracketToken:
		return p.parseTupleType()
	case ast.KindOpenParenToken:
		return p.parseParenthesizedType()
	case ast.KindImportKeyword:
		return p.parseImportType()
	case ast.KindAssertsKeyword:
		if p.lookAhead((*Parser).nextTokenIsIdentifierOrKeywordOnSameLine) {
			return p.parseAssertsTypePredicate()
		}
		return p.parseTypeReference()
	case ast.KindTemplateHead:
		return p.parseTemplateType()
	default:
		return p.parseTypeReference()
	}
}

func (p *Parser) parseKeywordTypeNode() store.NodeRef {
	pos := p.nodePos()
	kind := p.token
	p.nextToken()
	return p.b.NewToken(kind, p.flags(), int32(pos), int32(p.nodePos()))
}

func (p *Parser) parseThisTypeNode() store.NodeRef {
	pos := p.nodePos()
	p.nextToken()
	return p.b.NewToken(ast.KindThisType, p.flags(), int32(pos), int32(p.nodePos()))
}

func (p *Parser) parseThisTypePredicate(lhs store.NodeRef) store.NodeRef {
	p.nextToken()
	typeNode := p.parseType()
	return p.b.NewTypePredicateNode(p.flags(), int32(p.pos(lhs)), int32(p.nodePos()), store.NoNodeRef /*assertsModifier*/, lhs, typeNode)
}

func (p *Parser) parseJSDocAllType() store.NodeRef {
	pos := p.nodePos()
	p.nextToken()
	return p.b.NewToken(ast.KindJSDocAllType, p.flags(), int32(pos), int32(p.nodePos()))
}

func (p *Parser) parseJSDocNonNullableType() store.NodeRef {
	pos := p.nodePos()
	p.nextToken()
	typeNode := p.parseTypeOperatorOrHigher()
	return p.b.NewJSDocNonNullableType(p.flags(), int32(pos), int32(p.nodePos()), typeNode)
}

func (p *Parser) parseJSDocNullableType() store.NodeRef {
	pos := p.nodePos()
	// skip the ?
	p.nextToken()
	typeNode := p.parseTypeOperatorOrHigher()
	return p.b.NewJSDocNullableType(p.flags(), int32(pos), int32(p.nodePos()), typeNode)
}

func (p *Parser) parseJSDocType() store.NodeRef {
	p.scanner.SetSkipJSDocLeadingAsterisks(true)
	pos := p.nodePos()

	hasDotDotDot := p.parseOptional(ast.KindDotDotDotToken)
	t := p.parseTypeOrTypePredicate()
	p.scanner.SetSkipJSDocLeadingAsterisks(false)
	if hasDotDotDot {
		t = p.b.NewJSDocVariadicType(p.flags(), int32(pos), int32(p.nodePos()), t)
	}
	if p.token == ast.KindEqualsToken {
		p.nextToken()
		return p.b.NewJSDocOptionalType(p.flags(), int32(pos), int32(p.nodePos()), t)
	}
	return t
}

func (p *Parser) parseLiteralTypeNode(negative bool) store.NodeRef {
	pos := p.nodePos()
	if negative {
		p.nextToken()
	}
	var expression store.NodeRef
	if p.token == ast.KindTrueKeyword || p.token == ast.KindFalseKeyword || p.token == ast.KindNullKeyword {
		expression = p.parseKeywordExpression()
	} else {
		expression = p.parseLiteralExpression()
	}
	if negative {
		expression = p.b.NewPrefixUnaryExpression(p.flags(), int32(pos), int32(p.nodePos()), ast.KindMinusToken, expression)
	}
	return p.b.NewLiteralTypeNode(p.flags(), int32(pos), int32(p.nodePos()), expression)
}

func (p *Parser) parseTypeReference() store.NodeRef {
	pos := p.nodePos()
	typeName := p.parseEntityNameOfTypeReference()
	typeArguments := p.parseTypeArgumentsOfTypeReference()
	return p.b.NewTypeReferenceNode(p.flags(), int32(pos), int32(p.nodePos()), typeName, typeArguments)
}

func (p *Parser) parseEntityNameOfTypeReference() store.NodeRef {
	return p.parseEntityName(true /*allowReservedWords*/, false /*allowPrivateName*/, diagnostics.Type_expected)
}

func (p *Parser) parseEntityName(allowReservedWords bool, allowPrivateName bool, diagnosticMessage *diagnostics.Message) store.NodeRef {
	pos := p.nodePos()
	var entity store.NodeRef
	if allowReservedWords {
		entity = p.parseIdentifierNameWithDiagnostic(diagnosticMessage)
	} else {
		entity = p.parseIdentifierWithDiagnostic(diagnosticMessage, nil)
	}
	for p.parseOptional(ast.KindDotToken) {
		if p.token == ast.KindLessThanToken {
			// The entity is part of a JSDoc-style generic. We will use the gap between `typeName` and
			// `typeArguments` to report it as a grammar error in the checker.
			break
		}
		right := p.parseRightSideOfDot(allowReservedWords, allowPrivateName, true /*allowUnicodeEscapeSequenceInIdentifierName*/)
		entity = p.b.NewQualifiedName(p.flags(), int32(pos), int32(p.nodePos()), entity, right)
	}
	return entity
}

func (p *Parser) parseRightSideOfDot(allowIdentifierNames bool, allowPrivateIdentifiers bool, allowUnicodeEscapeSequenceInIdentifierName bool) store.NodeRef {
	// Technically a keyword is valid here as all identifiers and keywords are identifier names.
	// However, often we'll encounter this in error situations when the identifier or keyword
	// is actually starting another valid construct.
	//
	// So, we check for the following specific case:
	//
	//      name.
	//      identifierOrKeyword identifierNameOrKeyword
	//
	// Note: the newlines are important here.  For example, if that above code
	// were rewritten into:
	//
	//      name.identifierOrKeyword
	//      identifierNameOrKeyword
	//
	// Then we would consider it valid.  That's because ASI would take effect and
	// the code would be implicitly: "name.identifierOrKeyword; identifierNameOrKeyword".
	// In the first case though, ASI will not take effect because there is not a
	// line terminator after the identifier or keyword.
	if p.hasPrecedingLineBreak() && tokenIsIdentifierOrKeyword(p.token) && p.lookAhead((*Parser).nextTokenIsIdentifierOrKeywordOnSameLine) {
		// Report that we need an identifier.  However, report it right after the dot,
		// and not on the next token.  This is because the next token might actually
		// be an identifier and the error would be quite confusing.
		p.parseErrorAt(p.nodePos(), p.nodePos(), diagnostics.Identifier_expected)
		return p.createMissingIdentifier()
	}
	if p.token == ast.KindPrivateIdentifier {
		node := p.parsePrivateIdentifier()
		if allowPrivateIdentifiers {
			return node
		}
		p.parseErrorAt(p.nodePos(), p.nodePos(), diagnostics.Identifier_expected)
		return p.createMissingIdentifier()
	}
	if allowIdentifierNames {
		if allowUnicodeEscapeSequenceInIdentifierName {
			return p.parseIdentifierName()
		}
		return p.parseIdentifierNameErrorOnUnicodeEscapeSequence()
	}
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	id := p.parseIdentifier()
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	return id
}

// newIdentifier takes the position too: a Store node is made finished.
func (p *Parser) newIdentifier(text string, pos int, end int) store.NodeRef {
	p.identifierCount++
	id := p.b.NewIdentifier(p.flags(), int32(pos), int32(end), text)
	if text == "await" {
		p.statementHasAwaitIdentifier = true
	}
	return id
}

func (p *Parser) createMissingIdentifier() store.NodeRef {
	return p.newIdentifier("", p.nodePos(), p.nodePos())
}

func (p *Parser) parsePrivateIdentifier() store.NodeRef {
	pos := p.nodePos()
	text := p.scanner.TokenValue()
	p.nextToken()
	return p.b.NewPrivateIdentifier(p.flags(), int32(pos), int32(p.nodePos()), text)
}

func (p *Parser) reScanLessThanToken() ast.Kind {
	p.token = p.scanner.ReScanLessThanToken()
	return p.token
}

func (p *Parser) reScanGreaterThanToken() ast.Kind {
	p.token = p.scanner.ReScanGreaterThanToken()
	return p.token
}

func (p *Parser) reScanSlashToken() ast.Kind {
	p.token = p.scanner.ReScanSlashToken()
	return p.token
}

func (p *Parser) reScanTemplateToken(isTaggedTemplate bool) ast.Kind {
	p.token = p.scanner.ReScanTemplateToken(isTaggedTemplate)
	return p.token
}

func (p *Parser) parseTypeArgumentsOfTypeReference() store.ListRef {
	if !p.hasPrecedingLineBreak() && p.reScanLessThanToken() == ast.KindLessThanToken {
		return p.parseTypeArguments()
	}
	return store.NoListRef
}

func (p *Parser) parseTypeArguments() store.ListRef {
	if p.token == ast.KindLessThanToken {
		return p.parseBracketedList(PCTypeArguments, (*Parser).parseType, ast.KindLessThanToken, ast.KindGreaterThanToken)
	}
	return store.NoListRef
}

func (p *Parser) nextIsStartOfTypeOfImportType() bool {
	p.nextToken()
	return p.token == ast.KindImportKeyword
}

func (p *Parser) parseImportType() store.NodeRef {
	p.sourceFlags |= ast.NodeFlagsPossiblyContainsDynamicImport
	pos := p.nodePos()
	isTypeOf := p.parseOptional(ast.KindTypeOfKeyword)
	p.parseExpected(ast.KindImportKeyword)
	p.parseExpected(ast.KindOpenParenToken)
	typeNode := p.parseType()
	var attributes store.NodeRef
	if p.parseOptional(ast.KindCommaToken) {
		openBracePosition := p.scanner.TokenStart()
		p.parseExpected(ast.KindOpenBraceToken)
		currentToken := p.token
		if currentToken == ast.KindWithKeyword || currentToken == ast.KindAssertKeyword {
			if currentToken == ast.KindAssertKeyword {
				p.parseErrorAtCurrentToken(diagnostics.Import_assertions_have_been_replaced_by_import_attributes_Use_with_instead_of_assert)
			}
			p.nextToken()
		} else {
			p.parseErrorAtCurrentToken(diagnostics.X_0_expected, scanner.TokenToString(ast.KindWithKeyword))
		}
		p.parseExpected(ast.KindColonToken)
		attributes = p.parseImportAttributes(currentToken, true /*skipKeyword*/)
		p.parseOptional(ast.KindCommaToken)
		if !p.parseExpected(ast.KindCloseBraceToken) {
			if len(p.diagnostics) != 0 {
				lastDiagnostic := p.diagnostics[len(p.diagnostics)-1]
				if lastDiagnostic.Code() == diagnostics.X_0_expected.Code() {
					related := ast.NewDiagnostic(nil, core.NewTextRange(openBracePosition, openBracePosition), diagnostics.The_parser_expected_to_find_a_1_to_match_the_0_token_here, "{", "}")
					lastDiagnostic.AddRelatedInfo(related)
				}
			}
		}
	}
	p.parseExpected(ast.KindCloseParenToken)
	var qualifier store.NodeRef
	if p.parseOptional(ast.KindDotToken) {
		qualifier = p.parseEntityNameOfTypeReference()
	}
	typeArguments := p.parseTypeArgumentsOfTypeReference()
	return p.b.NewImportTypeNode(p.flags(), int32(pos), int32(p.nodePos()), isTypeOf, typeNode, attributes, qualifier, typeArguments)
}

func (p *Parser) parseImportAttribute() store.NodeRef {
	pos := p.nodePos()
	var name store.NodeRef
	if tokenIsIdentifierOrKeyword(p.token) {
		name = p.parseIdentifierName()
	} else if p.token == ast.KindStringLiteral {
		name = p.parseLiteralExpression()
	}
	if name != store.NoNodeRef {
		p.parseExpected(ast.KindColonToken)
	} else {
		p.parseErrorAtCurrentToken(diagnostics.Identifier_or_string_literal_expected)
	}
	value := p.parseAssignmentExpressionOrHigher()
	return p.b.NewImportAttribute(p.flags(), int32(pos), int32(p.nodePos()), name, value)
}

func (p *Parser) parseImportAttributes(token ast.Kind, skipKeyword bool) store.NodeRef {
	pos := p.nodePos()
	if !skipKeyword {
		p.parseExpected(token)
	}
	var elements store.ListRef
	var multiLine bool
	openBracePosition := p.scanner.TokenStart()
	if p.parseExpected(ast.KindOpenBraceToken) {
		multiLine = p.hasPrecedingLineBreak()
		elements = p.parseDelimitedList(PCImportAttributes, (*Parser).parseImportAttribute)
		if !p.parseExpected(ast.KindCloseBraceToken) {
			if len(p.diagnostics) != 0 {
				lastDiagnostic := p.diagnostics[len(p.diagnostics)-1]
				if lastDiagnostic.Code() == diagnostics.X_0_expected.Code() {
					related := ast.NewDiagnostic(nil, core.NewTextRange(openBracePosition, openBracePosition), diagnostics.The_parser_expected_to_find_a_1_to_match_the_0_token_here, "{", "}")
					lastDiagnostic.AddRelatedInfo(related)
				}
			}
		}
	} else {
		elements = p.parseEmptyNodeList()
	}
	return p.b.NewImportAttributes(p.flags(), int32(pos), int32(p.nodePos()), token, elements, multiLine)
}

func (p *Parser) parseTypeQuery() store.NodeRef {
	pos := p.nodePos()
	p.parseExpected(ast.KindTypeOfKeyword)
	entityName := p.parseEntityName(true /*allowReservedWords*/, true /*allowPrivateName*/, nil)
	// Make sure we perform ASI to prevent parsing the next line's type arguments as part of an instantiation expression
	var typeArguments store.ListRef
	if !p.hasPrecedingLineBreak() {
		typeArguments = p.parseTypeArguments()
	}
	return p.b.NewTypeQueryNode(p.flags(), int32(pos), int32(p.nodePos()), entityName, typeArguments)
}

func (p *Parser) nextIsStartOfMappedType() bool {
	p.nextToken()
	if p.token == ast.KindPlusToken || p.token == ast.KindMinusToken {
		return p.nextToken() == ast.KindReadonlyKeyword
	}
	if p.token == ast.KindReadonlyKeyword {
		p.nextToken()
	}
	return p.token == ast.KindOpenBracketToken && p.nextTokenIsIdentifier() && p.nextToken() == ast.KindInKeyword
}

func (p *Parser) parseMappedType() store.NodeRef {
	pos := p.nodePos()
	p.parseExpected(ast.KindOpenBraceToken)
	var readonlyToken store.NodeRef // ReadonlyKeyword | PlusToken | MinusToken
	if p.token == ast.KindReadonlyKeyword || p.token == ast.KindPlusToken || p.token == ast.KindMinusToken {
		readonlyToken = p.parseTokenNode()
		if p.b.View(readonlyToken).Kind() != ast.KindReadonlyKeyword {
			p.parseExpected(ast.KindReadonlyKeyword)
		}
	}
	p.parseExpected(ast.KindOpenBracketToken)
	typeParameter := p.parseMappedTypeParameter()
	var nameType store.NodeRef
	if p.parseOptional(ast.KindAsKeyword) {
		nameType = p.parseType()
	}
	p.parseExpected(ast.KindCloseBracketToken)
	var questionToken store.NodeRef // QuestionToken | PlusToken | MinusToken
	if p.token == ast.KindQuestionToken || p.token == ast.KindPlusToken || p.token == ast.KindMinusToken {
		questionToken = p.parseTokenNode()
		if p.b.View(questionToken).Kind() != ast.KindQuestionToken {
			p.parseExpected(ast.KindQuestionToken)
		}
	}
	typeNode := p.parseTypeAnnotation()
	p.parseSemicolon()
	members := p.parseList(PCTypeMembers, (*Parser).parseTypeMember)
	p.parseExpected(ast.KindCloseBraceToken)
	return p.b.NewMappedTypeNode(p.flags(), int32(pos), int32(p.nodePos()), readonlyToken, typeParameter, nameType, questionToken, typeNode, members)
}

func (p *Parser) parseMappedTypeParameter() store.NodeRef {
	pos := p.nodePos()
	name := p.parseIdentifierName()
	p.parseExpected(ast.KindInKeyword)
	typeNode := p.parseType()
	return p.b.NewTypeParameterDeclaration(p.flags(), int32(pos), int32(p.nodePos()), store.NoListRef /*modifiers*/, name, typeNode, store.NoNodeRef /*expression*/, store.NoNodeRef /*defaultType*/)
}

func (p *Parser) parseTypeMember() store.NodeRef {
	if p.token == ast.KindOpenParenToken || p.token == ast.KindLessThanToken {
		return p.parseSignatureMember(ast.KindCallSignature)
	}
	if p.token == ast.KindNewKeyword && p.lookAhead((*Parser).nextTokenIsOpenParenOrLessThan) {
		return p.parseSignatureMember(ast.KindConstructSignature)
	}
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	modifiers := p.parseModifiers()
	if p.parseContextualModifier(ast.KindGetKeyword) {
		return p.parseAccessorDeclaration(pos, jsdoc, modifiers, ast.KindGetAccessor, ParseFlagsType)
	}
	if p.parseContextualModifier(ast.KindSetKeyword) {
		return p.parseAccessorDeclaration(pos, jsdoc, modifiers, ast.KindSetAccessor, ParseFlagsType)
	}
	if p.isIndexSignature() {
		return p.parseIndexSignatureDeclaration(pos, jsdoc, modifiers)
	}
	return p.parsePropertyOrMethodSignature(pos, jsdoc, modifiers)
}

func (p *Parser) nextTokenIsOpenParenOrLessThan() bool {
	p.nextToken()
	return p.token == ast.KindOpenParenToken || p.token == ast.KindLessThanToken
}

func (p *Parser) parseSignatureMember(kind ast.Kind) store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	if kind == ast.KindConstructSignature {
		p.parseExpected(ast.KindNewKeyword)
	}
	typeParameters := p.parseTypeParameters()
	parameters := p.parseParameters(ParseFlagsType)
	typeNode := p.parseReturnType(ast.KindColonToken /*isType*/, true)
	p.parseTypeMemberSemicolon()
	var result store.NodeRef
	if kind == ast.KindCallSignature {
		result = p.b.NewCallSignatureDeclaration(p.flags(), int32(pos), int32(p.nodePos()), typeParameters, parameters, typeNode)
	} else {
		result = p.b.NewConstructSignatureDeclaration(p.flags(), int32(pos), int32(p.nodePos()), typeParameters, parameters, typeNode)
	}
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseTypeParameters() store.ListRef {
	if p.token == ast.KindLessThanToken {
		return p.parseBracketedList(PCTypeParameters, (*Parser).parseTypeParameter, ast.KindLessThanToken, ast.KindGreaterThanToken)
	}
	return store.NoListRef
}

func (p *Parser) parseTypeParameter() store.NodeRef {
	pos := p.nodePos()
	modifiers := p.parseModifiersEx(false /*allowDecorators*/, true /*permitConstAsModifier*/, false /*stopOnStartOfClassStaticBlock*/)
	name := p.parseIdentifier()
	var constraint store.NodeRef
	var expression store.NodeRef
	if p.parseOptional(ast.KindExtendsKeyword) {
		// It's not uncommon for people to write improper constraints to a generic.  If the
		// user writes a constraint that is an expression and not an actual type, then parse
		// it out as an expression (so we can recover well), but report that a type is needed
		// instead.
		if p.isStartOfType(false /*inStartOfParameter*/) || !p.isStartOfExpression() {
			constraint = p.parseType()
		} else {
			// It was not a type, and it looked like an expression.  Parse out an expression
			// here so we recover well.  Note: it is important that we call parseUnaryExpression
			// and not parseExpression here.  If the user has:
			//
			//      <T extends "">
			//
			// We do *not* want to consume the `>` as we're consuming the expression for "".
			expression = p.parseUnaryExpressionOrHigher()
		}
	}
	var defaultType store.NodeRef
	if p.parseOptional(ast.KindEqualsToken) {
		defaultType = p.parseType()
	}
	result := p.b.NewTypeParameterDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, name, constraint, expression, defaultType)
	return result
}

func (p *Parser) parseParameters(flags ParseFlags) store.ListRef {
	// FormalParameters [Yield,Await]: (modified)
	//      [empty]
	//      FormalParameterList[?Yield,Await]
	//
	// FormalParameter[Yield,Await]: (modified)
	//      BindingElement[?Yield,Await]
	//
	// BindingElement [Yield,Await]: (modified)
	//      SingleNameBinding[?Yield,?Await]
	//      BindingPattern[?Yield,?Await]Initializer [In, ?Yield,?Await] opt
	//
	// SingleNameBinding [Yield,Await]:
	//      BindingIdentifier[?Yield,?Await]Initializer [In, ?Yield,?Await] opt
	if p.parseExpected(ast.KindOpenParenToken) {
		parameters := p.parseParametersWorker(flags, true /*allowAmbiguity*/)
		p.parseExpected(ast.KindCloseParenToken)
		return parameters
	}
	return p.createMissingList()
}

func (p *Parser) parseParametersWorker(flags ParseFlags, allowAmbiguity bool) store.ListRef {
	// FormalParameters [Yield,Await]: (modified)
	//      [empty]
	//      FormalParameterList[?Yield,Await]
	//
	// FormalParameter[Yield,Await]: (modified)
	//      BindingElement[?Yield,Await]
	//
	// BindingElement [Yield,Await]: (modified)
	//      SingleNameBinding[?Yield,?Await]
	//      BindingPattern[?Yield,?Await]Initializer [In, ?Yield,?Await] opt
	//
	// SingleNameBinding [Yield,Await]:
	//      BindingIdentifier[?Yield,?Await]Initializer [In, ?Yield,?Await] opt
	inAwaitContext := p.contextFlags&ast.NodeFlagsAwaitContext != 0
	saveContextFlags := p.contextFlags
	p.setContextFlags(ast.NodeFlagsYieldContext, flags&ParseFlagsYield != 0)
	p.setContextFlags(ast.NodeFlagsAwaitContext, flags&ParseFlagsAwait != 0)
	parameters := p.parseDelimitedList(PCParameters, func(p *Parser) store.NodeRef {
		parameter := p.parseParameterEx(inAwaitContext, allowAmbiguity)
		if parameter != store.NoNodeRef && flags&ParseFlagsType == 0 {
			p.checkJSSyntax(parameter)
		}
		return parameter
	})
	p.contextFlags = saveContextFlags
	return parameters
}

func (p *Parser) parseParameter() store.NodeRef {
	return p.parseParameterEx(false /*inOuterAwaitContext*/, true /*allowAmbiguity*/)
}

func (p *Parser) parseParameterEx(inOuterAwaitContext bool, allowAmbiguity bool) store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	// FormalParameter [Yield,Await]:
	//      BindingElement[?Yield,?Await]
	// Decorators are parsed in the outer [Await] context, the rest of the parameter is parsed in the function's [Await] context.
	saveContextFlags := p.contextFlags
	p.setContextFlags(ast.NodeFlagsAwaitContext, inOuterAwaitContext)
	modifiers := p.parseModifiersEx(true /*allowDecorators*/, false /*permitConstAsModifier*/, false /*stopOnStartOfClassStaticBlock*/)
	p.contextFlags = saveContextFlags
	if p.token == ast.KindThisKeyword {
		name := p.createIdentifier(true /*isIdentifier*/)
		typeNode := p.parseTypeAnnotation()
		if modifiers != store.NoListRef {
			p.parseErrorAtRange(p.loc(p.b.ViewList(modifiers).Refs()[0]), diagnostics.Neither_decorators_nor_modifiers_may_be_applied_to_this_parameters)
		}
		// The node is made after the error above, which finishNode saw as a pending parse error.
		result := p.b.NewParameterDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, store.NoNodeRef /*dotDotDotToken*/, name, store.NoNodeRef /*questionToken*/, typeNode, store.NoNodeRef /*initializer*/)
		p.b.AddFlags(result, p.jsdocFlags(jsdoc))
		return result
	}
	dotDotDotToken := p.parseOptionalToken(ast.KindDotDotDotToken)
	if !allowAmbiguity && !p.isParameterNameStart() {
		return store.NoNodeRef
	}
	name := p.parseNameOfParameter(modifiers)
	questionToken := p.parseOptionalToken(ast.KindQuestionToken)
	typeNode := p.parseTypeAnnotation()
	initializer := p.parseInitializer()
	result := p.b.NewParameterDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, dotDotDotToken, name, questionToken, typeNode, initializer)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) isParameterNameStart() bool {
	// Be permissive about await and yield by calling isBindingIdentifier instead of isIdentifier; disallowing
	// them during a speculative parse leads to many more follow-on errors than allowing the function to parse then later
	// complaining about the use of the keywords.
	return p.isBindingIdentifier() || p.token == ast.KindOpenBracketToken || p.token == ast.KindOpenBraceToken
}

func (p *Parser) parseNameOfParameter(modifiers store.ListRef) store.NodeRef {
	// FormalParameter [Yield,Await]:
	//      BindingElement[?Yield,?Await]
	name := p.parseIdentifierOrPatternWithDiagnostic(diagnostics.Private_identifiers_cannot_be_used_as_parameters)
	if p.loc(name).Len() == 0 && modifiers == store.NoListRef && ast.IsModifierKind(p.token) {
		// in cases like
		// 'use strict'
		// function foo(static)
		// isParameter('static') == true, because of isModifier('static')
		// however 'static' is not a legal identifier in a strict mode.
		// so result of this function will be Parameter (flags = 0, name = missing, type = undefined, initializer = undefined)
		// and current token will not change => parsing of the enclosing parameter list will last till the end of time (or OOM)
		// to avoid this we'll advance cursor to the next token.
		p.nextToken()
	}
	return name
}

func (p *Parser) parseReturnType(returnToken ast.Kind, isType bool) store.NodeRef {
	if p.shouldParseReturnType(returnToken, isType) {
		return p.doInContext(ast.NodeFlagsDisallowConditionalTypesContext, false, (*Parser).parseTypeOrTypePredicate)
	}
	return store.NoNodeRef
}

func (p *Parser) shouldParseReturnType(returnToken ast.Kind, isType bool) bool {
	if returnToken == ast.KindEqualsGreaterThanToken {
		p.parseExpected(returnToken)
		return true
	} else if p.parseOptional(ast.KindColonToken) {
		return true
	} else if isType && p.token == ast.KindEqualsGreaterThanToken {
		// This is easy to get backward, especially in type contexts, so parse the type anyway
		p.parseErrorAtCurrentToken(diagnostics.X_0_expected, scanner.TokenToString(ast.KindColonToken))
		p.nextToken()
		return true
	}
	return false
}

func (p *Parser) parseTypeOrTypePredicate() store.NodeRef {
	if p.isIdentifier() {
		state := p.mark()
		pos := p.nodePos()
		id := p.parseIdentifier()
		if p.token == ast.KindIsKeyword && !p.hasPrecedingLineBreak() {
			p.nextToken()
			typeNode := p.parseType()
			return p.b.NewTypePredicateNode(p.flags(), int32(pos), int32(p.nodePos()), store.NoNodeRef /*assertsModifier*/, id, typeNode)
		}
		p.rewind(state)
	}
	return p.parseType()
}

func (p *Parser) parseTypeMemberSemicolon() {
	// We allow type members to be separated by commas or (possibly ASI) semicolons.
	// First check if it was a comma.  If so, we're done with the member.
	if p.parseOptional(ast.KindCommaToken) {
		return
	}
	// Didn't have a comma.  We must have a (possible ASI) semicolon.
	p.parseSemicolon()
}

func (p *Parser) parseAccessorDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef, kind ast.Kind, flags ParseFlags) store.NodeRef {
	name := p.parsePropertyName()
	typeParameters := p.parseTypeParameters()
	parameters := p.parseParameters(ParseFlagsNone)
	returnType := p.parseReturnType(ast.KindColonToken, false /*isType*/)
	body := p.parseFunctionBlockOrSemicolon(flags, nil /*diagnosticMessage*/)
	var result store.NodeRef
	// Keep track of `typeParameters` (for both) and `type` (for setters) if they were parsed those indicate grammar errors
	if kind == ast.KindGetAccessor {
		result = p.b.NewGetAccessorDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, name, typeParameters, parameters, returnType, store.NoNodeRef /*fullSignature*/, body)
	} else {
		result = p.b.NewSetAccessorDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, name, typeParameters, parameters, returnType, store.NoNodeRef /*fullSignature*/, body)
	}
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	if flags&ParseFlagsType == 0 {
		p.checkJSSyntax(result)
	}
	return result
}

func (p *Parser) parsePropertyName() store.NodeRef {
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	prop := p.parsePropertyNameWorker(true /*allowComputedPropertyNames*/)
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	return prop
}

func (p *Parser) parsePropertyNameWorker(allowComputedPropertyNames bool) store.NodeRef {
	if p.token == ast.KindStringLiteral || p.token == ast.KindNumericLiteral || p.token == ast.KindBigIntLiteral {
		return p.parseLiteralExpression()
	}
	if allowComputedPropertyNames && p.token == ast.KindOpenBracketToken {
		return p.parseComputedPropertyName()
	}
	if p.token == ast.KindPrivateIdentifier {
		return p.parsePrivateIdentifier()
	}
	return p.parseIdentifierName()
}

func (p *Parser) parseComputedPropertyName() store.NodeRef {
	// PropertyName [Yield]:
	//      LiteralPropertyName
	//      ComputedPropertyName[?Yield]
	pos := p.nodePos()
	p.parseExpected(ast.KindOpenBracketToken)
	// We parse any expression (including a comma expression). But the grammar
	// says that only an assignment expression is allowed, so the grammar checker
	// will error if it sees a comma expression.
	expression := p.parseExpressionAllowIn()
	p.parseExpected(ast.KindCloseBracketToken)
	return p.b.NewComputedPropertyName(p.flags(), int32(pos), int32(p.nodePos()), expression)
}

func (p *Parser) parseFunctionBlockOrSemicolon(flags ParseFlags, diagnosticMessage *diagnostics.Message) store.NodeRef {
	if p.token != ast.KindOpenBraceToken {
		if flags&ParseFlagsType != 0 {
			p.parseTypeMemberSemicolon()
			return store.NoNodeRef
		}
		if p.canParseSemicolon() {
			p.parseSemicolon()
			return store.NoNodeRef
		}
	}
	return p.parseFunctionBlock(flags, diagnosticMessage)
}

func (p *Parser) parseFunctionBlock(flags ParseFlags, diagnosticMessage *diagnostics.Message) store.NodeRef {
	saveContextFlags := p.contextFlags
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	p.setContextFlags(ast.NodeFlagsYieldContext, flags&ParseFlagsYield != 0)
	p.setContextFlags(ast.NodeFlagsAwaitContext, flags&ParseFlagsAwait != 0)
	// We may be in a [Decorator] context when parsing a function expression or
	// arrow function. The body of the function is not in [Decorator] context.
	p.setContextFlags(ast.NodeFlagsDecoratorContext, false)
	block := p.parseBlock(flags&ParseFlagsIgnoreMissingOpenBrace != 0, diagnosticMessage)
	p.contextFlags = saveContextFlags
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	return block
}

func (p *Parser) isIndexSignature() bool {
	return p.token == ast.KindOpenBracketToken && p.lookAhead((*Parser).nextIsUnambiguouslyIndexSignature)
}

func (p *Parser) nextIsUnambiguouslyIndexSignature() bool {
	// The only allowed sequence is:
	//
	//   [id:
	//
	// However, for error recovery, we also check the following cases:
	//
	//   [...
	//   [id,
	//   [id?,
	//   [id?:
	//   [id?]
	//   [public id
	//   [private id
	//   [protected id
	//   []
	//
	p.nextToken()
	if p.token == ast.KindDotDotDotToken || p.token == ast.KindCloseBracketToken {
		return true
	}
	if ast.IsModifierKind(p.token) {
		p.nextToken()
		if p.isIdentifier() {
			return true
		}
	} else if !p.isIdentifier() {
		return false
	} else {
		// Skip the identifier
		p.nextToken()
	}
	// A colon signifies a well formed indexer
	// A comma should be a badly formed indexer because comma expressions are not allowed
	// in computed properties.
	if p.token == ast.KindColonToken || p.token == ast.KindCommaToken {
		return true
	}
	// Question mark could be an indexer with an optional property,
	// or it could be a conditional expression in a computed property.
	if p.token != ast.KindQuestionToken {
		return false
	}
	// If any of the following tokens are after the question mark, it cannot
	// be a conditional expression, so treat it as an indexer.
	p.nextToken()
	return p.token == ast.KindColonToken || p.token == ast.KindCommaToken || p.token == ast.KindCloseBracketToken
}

func (p *Parser) parseIndexSignatureDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	parameters := p.parseBracketedList(PCParameters, (*Parser).parseParameter, ast.KindOpenBracketToken, ast.KindCloseBracketToken)
	typeNode := p.parseTypeAnnotation()
	p.parseTypeMemberSemicolon()
	result := p.b.NewIndexSignatureDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, parameters, typeNode)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parsePropertyOrMethodSignature(pos int, jsdoc jsdocScannerInfo, modifiers store.ListRef) store.NodeRef {
	name := p.parsePropertyName()
	questionToken := p.parseOptionalToken(ast.KindQuestionToken)
	var result store.NodeRef
	if p.token == ast.KindOpenParenToken || p.token == ast.KindLessThanToken {
		// Method signatures don't exist in expression contexts.  So they have neither
		// [Yield] nor [Await]
		typeParameters := p.parseTypeParameters()
		parameters := p.parseParameters(ParseFlagsType)
		returnType := p.parseReturnType(ast.KindColonToken /*isType*/, true)
		// The Pointer parser finishes the node after the semicolon, so it is made after it here.
		p.parseTypeMemberSemicolon()
		result = p.b.NewMethodSignatureDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, name, questionToken, typeParameters, parameters, returnType)
	} else {
		typeNode := p.parseTypeAnnotation()
		// Although type literal properties cannot not have initializers, we attempt
		// to parse an initializer so we can report in the checker that an interface
		// property or type literal property cannot have an initializer.
		var initializer store.NodeRef
		if p.token == ast.KindEqualsToken {
			initializer = p.parseInitializer()
		}
		p.parseTypeMemberSemicolon()
		result = p.b.NewPropertySignatureDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers, name, questionToken, typeNode, initializer)
	}
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseTypeLiteral() store.NodeRef {
	pos := p.nodePos()
	members := p.parseObjectTypeMembers()
	result := p.b.NewTypeLiteralNode(p.flags(), int32(pos), int32(p.nodePos()), members)
	return result
}

func (p *Parser) parseObjectTypeMembers() store.ListRef {
	if p.parseExpected(ast.KindOpenBraceToken) {
		members := p.parseList(PCTypeMembers, (*Parser).parseTypeMember)
		p.parseExpected(ast.KindCloseBraceToken)
		return members
	}
	return p.createMissingList()
}

func (p *Parser) parseTupleType() store.NodeRef {
	pos := p.nodePos()
	elements := p.parseBracketedList(PCTupleElementTypes, (*Parser).parseTupleElementNameOrTupleElementType, ast.KindOpenBracketToken, ast.KindCloseBracketToken)
	return p.b.NewTupleTypeNode(p.flags(), int32(pos), int32(p.nodePos()), elements)
}

func (p *Parser) parseTupleElementNameOrTupleElementType() store.NodeRef {
	if p.lookAhead((*Parser).scanStartOfNamedTupleElement) {
		pos := p.nodePos()
		jsdoc := p.jsdocScannerInfo()
		dotDotDotToken := p.parseOptionalToken(ast.KindDotDotDotToken)
		name := p.parseIdentifierName()
		questionToken := p.parseOptionalToken(ast.KindQuestionToken)
		p.parseExpected(ast.KindColonToken)
		typeNode := p.parseTupleElementType()
		result := p.b.NewNamedTupleMember(p.flags(), int32(pos), int32(p.nodePos()), dotDotDotToken, name, questionToken, typeNode)
		p.b.AddFlags(result, p.jsdocFlags(jsdoc))
		return result
	}
	return p.parseTupleElementType()
}

func (p *Parser) scanStartOfNamedTupleElement() bool {
	if p.token == ast.KindDotDotDotToken {
		return tokenIsIdentifierOrKeyword(p.nextToken()) && p.nextTokenIsColonOrQuestionColon()
	}
	return tokenIsIdentifierOrKeyword(p.token) && p.nextTokenIsColonOrQuestionColon()
}

func (p *Parser) nextTokenIsColonOrQuestionColon() bool {
	return p.nextToken() == ast.KindColonToken || p.token == ast.KindQuestionToken && p.nextToken() == ast.KindColonToken
}

func (p *Parser) parseTupleElementType() store.NodeRef {
	pos := p.nodePos()
	if p.parseOptional(ast.KindDotDotDotToken) {
		typeNode := p.parseType()
		return p.b.NewRestTypeNode(p.flags(), int32(pos), int32(p.nodePos()), typeNode)
	}
	typeNode := p.parseType()
	if v := p.b.View(typeNode); v.Kind() == ast.KindJSDocNullableType && v.Pos() == v.AsJSDocNullableType().Type().Pos() {
		// The OptionalTypeNode takes the flags and position of the JSDocNullableType and
		// adopts its type; the JSDocNullableType stays as a dead node.
		return p.b.NewOptionalTypeNode(v.Flags(), v.Pos(), v.End(), v.AsJSDocNullableType().Type().Ref())
	}
	return typeNode
}

func (p *Parser) parseParenthesizedType() store.NodeRef {
	pos := p.nodePos()
	p.parseExpected(ast.KindOpenParenToken)
	typeNode := p.parseType()
	p.parseExpected(ast.KindCloseParenToken)
	return p.b.NewParenthesizedTypeNode(p.flags(), int32(pos), int32(p.nodePos()), typeNode)
}

func (p *Parser) parseAssertsTypePredicate() store.NodeRef {
	pos := p.nodePos()
	assertsModifier := p.parseExpectedToken(ast.KindAssertsKeyword)
	var parameterName store.NodeRef
	if p.token == ast.KindThisKeyword {
		parameterName = p.parseThisTypeNode()
	} else {
		parameterName = p.parseIdentifier()
	}
	var typeNode store.NodeRef
	if p.parseOptional(ast.KindIsKeyword) {
		typeNode = p.parseType()
	}
	return p.b.NewTypePredicateNode(p.flags(), int32(pos), int32(p.nodePos()), assertsModifier, parameterName, typeNode)
}

func (p *Parser) parseTemplateType() store.NodeRef {
	pos := p.nodePos()
	head := p.parseTemplateHead(false /*isTaggedTemplate*/)
	templateSpans := p.parseTemplateTypeSpans()
	return p.b.NewTemplateLiteralTypeNode(p.flags(), int32(pos), int32(p.nodePos()), head, templateSpans)
}

func (p *Parser) parseTemplateHead(isTaggedTemplate bool) store.NodeRef {
	if !isTaggedTemplate && p.scanner.TokenFlags()&ast.TokenFlagsIsInvalid != 0 {
		p.reScanTemplateToken(false /*isTaggedTemplate*/)
	}
	pos := p.nodePos()
	text, rawText, templateFlags := p.scanner.TokenValue(), p.getTemplateLiteralRawText(2 /*endLength*/), p.scanner.TokenFlags()&ast.TokenFlagsTemplateLiteralLikeFlags
	p.nextToken()
	return p.b.NewTemplateHead(p.flags(), int32(pos), int32(p.nodePos()), text, rawText, templateFlags)
}

func (p *Parser) getTemplateLiteralRawText(endLength int) string {
	tokenText := p.scanner.TokenText()
	if p.scanner.TokenFlags()&ast.TokenFlagsUnterminated != 0 {
		endLength = 0
	}
	return tokenText[1 : len(tokenText)-endLength]
}

func (p *Parser) parseTemplateTypeSpans() store.ListRef {
	pos := p.nodePos()
	mark := len(p.elems)
	for {
		span := p.parseTemplateTypeSpan()
		p.elems = append(p.elems, span)
		if p.b.View(span).AsTemplateLiteralTypeSpan().Literal().Kind() != ast.KindTemplateMiddle {
			break
		}
	}
	at := p.b.List(int32(pos), int32(p.nodePos()), p.elems[mark:])
	p.elems = p.elems[:mark]
	return at
}

func (p *Parser) parseTemplateTypeSpan() store.NodeRef {
	pos := p.nodePos()
	typeNode := p.parseType()
	literal := p.parseLiteralOfTemplateSpan(false /*isTaggedTemplate*/)
	return p.b.NewTemplateLiteralTypeSpan(p.flags(), int32(pos), int32(p.nodePos()), typeNode, literal)
}

func (p *Parser) parseLiteralOfTemplateSpan(isTaggedTemplate bool) store.NodeRef {
	if p.token == ast.KindCloseBraceToken {
		p.reScanTemplateToken(isTaggedTemplate)
		return p.parseTemplateMiddleOrTail()
	}
	p.parseErrorAtCurrentToken(diagnostics.X_0_expected, scanner.TokenToString(ast.KindCloseBraceToken))
	return p.b.NewTemplateTail(p.flags(), int32(p.nodePos()), int32(p.nodePos()), "", "", ast.TokenFlagsNone)
}

func (p *Parser) parseTemplateMiddleOrTail() store.NodeRef {
	pos := p.nodePos()
	isMiddle := p.token == ast.KindTemplateMiddle
	text, templateFlags := p.scanner.TokenValue(), p.scanner.TokenFlags()&ast.TokenFlagsTemplateLiteralLikeFlags
	rawText := p.getTemplateLiteralRawText(core.IfElse(isMiddle, 2, 1) /*endLength*/)
	p.nextToken()
	if isMiddle {
		return p.b.NewTemplateMiddle(p.flags(), int32(pos), int32(p.nodePos()), text, rawText, templateFlags)
	}
	return p.b.NewTemplateTail(p.flags(), int32(pos), int32(p.nodePos()), text, rawText, templateFlags)
}

func (p *Parser) parseFunctionOrConstructorTypeToError(isInUnionType bool, parseConstituentType func(p *Parser) store.NodeRef) store.NodeRef {
	// the function type and constructor type shorthand notation
	// are not allowed directly in unions and intersections, but we'll
	// try to parse them gracefully and issue a helpful message.
	if p.isStartOfFunctionTypeOrConstructorType() {
		typeNode := p.parseFunctionOrConstructorType()
		var diagnostic *diagnostics.Message
		if p.b.View(typeNode).Kind() == ast.KindFunctionType {
			diagnostic = core.IfElse(isInUnionType,
				diagnostics.Function_type_notation_must_be_parenthesized_when_used_in_a_union_type,
				diagnostics.Function_type_notation_must_be_parenthesized_when_used_in_an_intersection_type)
		} else {
			diagnostic = core.IfElse(isInUnionType,
				diagnostics.Constructor_type_notation_must_be_parenthesized_when_used_in_a_union_type,
				diagnostics.Constructor_type_notation_must_be_parenthesized_when_used_in_an_intersection_type)
		}
		p.parseErrorAtRange(p.loc(typeNode), diagnostic)
		return typeNode
	}
	return parseConstituentType(p)
}

func (p *Parser) isStartOfFunctionTypeOrConstructorType() bool {
	return p.token == ast.KindLessThanToken ||
		p.token == ast.KindOpenParenToken && p.lookAhead((*Parser).nextIsUnambiguouslyStartOfFunctionType) ||
		p.token == ast.KindNewKeyword ||
		p.token == ast.KindAbstractKeyword && p.lookAhead((*Parser).nextTokenIsNewKeyword)
}

func (p *Parser) parseFunctionOrConstructorType() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	modifiers := p.parseModifiersForConstructorType()
	isConstructorType := p.parseOptional(ast.KindNewKeyword)
	debug.Assert(modifiers == store.NoListRef || isConstructorType, "Per isStartOfFunctionOrConstructorType, a function type cannot have modifiers.")
	typeParameters := p.parseTypeParameters()
	parameters := p.parseParameters(ParseFlagsType)
	returnType := p.parseReturnType(ast.KindEqualsGreaterThanToken, false /*isType*/)
	var result store.NodeRef
	if isConstructorType {
		result = p.b.NewConstructorTypeNode(p.flags(), int32(pos), int32(p.nodePos()), modifiers, typeParameters, parameters, returnType)
	} else {
		result = p.b.NewFunctionTypeNode(p.flags(), int32(pos), int32(p.nodePos()), typeParameters, parameters, returnType)
	}
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseModifiersForConstructorType() store.ListRef {
	if p.token == ast.KindAbstractKeyword {
		pos := p.nodePos()
		kind := p.token
		p.nextToken()
		modifier := p.b.NewToken(kind, p.flags(), int32(pos), int32(p.nodePos()))
		return p.b.List(int32(pos), int32(p.nodePos()), []store.NodeRef{modifier})
	}
	return store.NoListRef
}

func (p *Parser) nextTokenIsNewKeyword() bool {
	return p.nextToken() == ast.KindNewKeyword
}

func (p *Parser) nextIsUnambiguouslyStartOfFunctionType() bool {
	p.nextToken()
	if p.token == ast.KindCloseParenToken || p.token == ast.KindDotDotDotToken {
		// ( )
		// ( ...
		return true
	}
	if p.skipParameterStart() {
		// We successfully skipped modifiers (if any) and an identifier or binding pattern,
		// now see if we have something that indicates a parameter declaration
		if p.token == ast.KindColonToken || p.token == ast.KindCommaToken || p.token == ast.KindQuestionToken || p.token == ast.KindEqualsToken {
			// ( xxx :
			// ( xxx ,
			// ( xxx ?
			// ( xxx =
			return true
		}
		if p.token == ast.KindCloseParenToken && p.nextToken() == ast.KindEqualsGreaterThanToken {
			// ( xxx ) =>
			return true
		}
	}
	return false
}

func (p *Parser) skipParameterStart() bool {
	if ast.IsModifierKind(p.token) {
		// Skip modifiers
		p.parseModifiers()
	}
	p.parseOptional(ast.KindDotDotDotToken)
	if p.isIdentifier() || p.token == ast.KindThisKeyword {
		p.nextToken()
		return true
	}
	if p.token == ast.KindOpenBracketToken || p.token == ast.KindOpenBraceToken {
		// Return true if we can parse an array or object binding pattern with no errors
		previousErrorCount := len(p.diagnostics)
		p.parseIdentifierOrPattern()
		return previousErrorCount == len(p.diagnostics)
	}
	return false
}

func (p *Parser) parseModifiers() store.ListRef {
	return p.parseModifiersEx(false, false, false)
}

func (p *Parser) parseModifiersEx(allowDecorators bool, permitConstAsModifier bool, stopOnStartOfClassStaticBlock bool) store.ListRef {
	var hasLeadingModifier bool
	var hasTrailingDecorator bool
	var hasTrailingModifier bool
	var hasStaticModifier bool
	// Decorators should be contiguous in a list of modifiers but can potentially appear in two places (i.e., `[...leadingDecorators, ...leadingModifiers, ...trailingDecorators, ...trailingModifiers]`).
	// The leading modifiers *should* only contain `export` and `default` when trailingDecorators are present, but we'll handle errors for any other leading modifiers in the checker.
	// It is illegal to have both leadingDecorators and trailingDecorators, but we will report that as a grammar check in the checker.
	// parse leading decorators
	pos := p.nodePos()
	mark := len(p.elems)
	for {
		if allowDecorators && p.token == ast.KindAtToken && !hasTrailingModifier {
			decorator := p.parseDecorator()
			p.elems = append(p.elems, decorator)
			if hasLeadingModifier {
				hasTrailingDecorator = true
			}
		} else {
			modifier := p.tryParseModifier(hasStaticModifier, permitConstAsModifier, stopOnStartOfClassStaticBlock)
			if modifier == store.NoNodeRef {
				break
			}
			if p.b.View(modifier).Kind() == ast.KindStaticKeyword {
				hasStaticModifier = true
			}
			p.elems = append(p.elems, modifier)
			if hasTrailingDecorator {
				hasTrailingModifier = true
			} else {
				hasLeadingModifier = true
			}
		}
	}
	if len(p.elems) != mark {
		at := p.b.List(int32(pos), int32(p.nodePos()), p.elems[mark:])
		p.elems = p.elems[:mark]
		return at
	}
	return store.NoListRef
}

func (p *Parser) parseDecorator() store.NodeRef {
	pos := p.nodePos()
	p.parseExpected(ast.KindAtToken)
	expression := p.doInContext(ast.NodeFlagsDecoratorContext, true, (*Parser).parseDecoratorExpression)
	return p.b.NewDecorator(p.flags(), int32(pos), int32(p.nodePos()), expression)
}

func (p *Parser) parseDecoratorExpression() store.NodeRef {
	if p.inAwaitContext() && p.token == ast.KindAwaitKeyword {
		// `@await` is disallowed in an [Await] context, but can cause parsing to go off the rails
		// This simply parses the missing identifier and moves on.
		pos := p.nodePos()
		awaitExpression := p.parseIdentifierWithDiagnostic(diagnostics.Expression_expected, nil)
		p.nextToken()
		memberExpression := p.parseMemberExpressionRest(pos, awaitExpression /*allowOptionalChain*/, true)
		return p.parseCallExpressionRest(pos, memberExpression)
	}
	return p.parseLeftHandSideExpressionOrHigher()
}

func (p *Parser) tryParseModifier(hasSeenStaticModifier bool, permitConstAsModifier bool, stopOnStartOfClassStaticBlock bool) store.NodeRef {
	pos := p.nodePos()
	kind := p.token
	if p.token == ast.KindConstKeyword && permitConstAsModifier {
		// We need to ensure that any subsequent modifiers appear on the same line
		// so that when 'const' is a standalone declaration, we don't issue an error.
		if !p.lookAhead((*Parser).nextTokenIsOnSameLineAndCanFollowModifier) {
			return store.NoNodeRef
		} else {
			p.nextToken()
		}
	} else if stopOnStartOfClassStaticBlock && p.token == ast.KindStaticKeyword && p.lookAhead((*Parser).nextTokenIsOpenBrace) {
		return store.NoNodeRef
	} else if hasSeenStaticModifier && p.token == ast.KindStaticKeyword {
		return store.NoNodeRef
	} else {
		if !p.parseAnyContextualModifier() {
			return store.NoNodeRef
		}
	}
	return p.b.NewToken(kind, p.flags(), int32(pos), int32(p.nodePos()))
}

func (p *Parser) parseContextualModifier(t ast.Kind) bool {
	state := p.mark()
	if p.token == t && p.nextTokenCanFollowModifier() {
		return true
	}
	p.rewind(state)
	return false
}

func (p *Parser) parseAnyContextualModifier() bool {
	state := p.mark()
	if ast.IsModifierKind(p.token) && p.nextTokenCanFollowModifier() {
		return true
	}
	p.rewind(state)
	return false
}

func (p *Parser) nextTokenCanFollowModifier() bool {
	switch p.token {
	case ast.KindConstKeyword:
		// 'const' is only a modifier if followed by 'enum'.
		return p.nextToken() == ast.KindEnumKeyword
	case ast.KindExportKeyword:
		p.nextToken()
		if p.token == ast.KindDefaultKeyword {
			return p.lookAhead((*Parser).nextTokenCanFollowDefaultKeyword)
		}
		if p.token == ast.KindTypeKeyword {
			return p.lookAhead((*Parser).nextTokenCanFollowExportModifier)
		}
		return p.canFollowExportModifier()
	case ast.KindDefaultKeyword:
		return p.nextTokenCanFollowDefaultKeyword()
	case ast.KindStaticKeyword:
		p.nextToken()
		return p.canFollowModifier()
	case ast.KindGetKeyword, ast.KindSetKeyword:
		p.nextToken()
		return p.canFollowGetOrSetKeyword()
	default:
		return p.nextTokenIsOnSameLineAndCanFollowModifier()
	}
}

func (p *Parser) nextTokenCanFollowDefaultKeyword() bool {
	switch p.nextToken() {
	case ast.KindClassKeyword, ast.KindFunctionKeyword, ast.KindInterfaceKeyword, ast.KindAtToken:
		return true
	case ast.KindAbstractKeyword:
		return p.lookAhead((*Parser).nextTokenIsClassKeywordOnSameLine)
	case ast.KindAsyncKeyword:
		return p.lookAhead((*Parser).nextTokenIsFunctionKeywordOnSameLine)
	}
	return false
}

func (p *Parser) nextTokenIsIdentifierOrKeyword() bool {
	return tokenIsIdentifierOrKeyword(p.nextToken())
}

func (p *Parser) nextTokenIsIdentifierOrKeywordOrGreaterThan() bool {
	return tokenIsIdentifierOrKeywordOrGreaterThan(p.nextToken())
}

func (p *Parser) nextTokenIsIdentifierOrKeywordOnSameLine() bool {
	return p.nextTokenIsIdentifierOrKeyword() && !p.hasPrecedingLineBreak()
}

func (p *Parser) nextTokenIsIdentifierOrKeywordOrLiteralOnSameLine() bool {
	return (p.nextTokenIsIdentifierOrKeyword() || p.token == ast.KindNumericLiteral || p.token == ast.KindBigIntLiteral || p.token == ast.KindStringLiteral) && !p.hasPrecedingLineBreak()
}

func (p *Parser) nextTokenIsClassKeywordOnSameLine() bool {
	return p.nextToken() == ast.KindClassKeyword && !p.hasPrecedingLineBreak()
}

func (p *Parser) nextTokenIsFunctionKeywordOnSameLine() bool {
	return p.nextToken() == ast.KindFunctionKeyword && !p.hasPrecedingLineBreak()
}

func (p *Parser) nextTokenCanFollowExportModifier() bool {
	p.nextToken()
	return p.canFollowExportModifier()
}

func (p *Parser) canFollowExportModifier() bool {
	return p.token == ast.KindAtToken || p.token != ast.KindAsteriskToken && p.token != ast.KindAsKeyword && p.token != ast.KindOpenBraceToken && p.canFollowModifier()
}

func (p *Parser) canFollowModifier() bool {
	return p.token == ast.KindOpenBracketToken || p.token == ast.KindOpenBraceToken || p.token == ast.KindAsteriskToken || p.token == ast.KindDotDotDotToken || p.isLiteralPropertyName()
}

func (p *Parser) canFollowGetOrSetKeyword() bool {
	return p.token == ast.KindOpenBracketToken || p.isLiteralPropertyName()
}

func (p *Parser) nextTokenIsOnSameLineAndCanFollowModifier() bool {
	p.nextToken()
	if p.hasPrecedingLineBreak() {
		return false
	}
	return p.canFollowModifier()
}

func (p *Parser) nextTokenIsOpenBrace() bool {
	return p.nextToken() == ast.KindOpenBraceToken
}

func (p *Parser) parseExpression() store.NodeRef {
	// Expression[in]:
	//      AssignmentExpression[in]
	//      Expression[in] , AssignmentExpression[in]

	// clear the decorator context when parsing Expression, as it should be unambiguous when parsing a decorator
	saveContextFlags := p.contextFlags
	p.contextFlags &^= ast.NodeFlagsDecoratorContext
	pos := p.nodePos()
	expr := p.parseAssignmentExpressionOrHigher()
	for {
		operatorToken := p.parseOptionalToken(ast.KindCommaToken)
		if operatorToken == store.NoNodeRef {
			break
		}
		expr = p.makeBinaryExpression(expr, operatorToken, p.parseAssignmentExpressionOrHigher(), pos)
	}
	p.contextFlags = saveContextFlags
	return expr
}

func (p *Parser) parseExpressionAllowIn() store.NodeRef {
	return p.doInContext(ast.NodeFlagsDisallowInContext, false, (*Parser).parseExpression)
}

func (p *Parser) parseAssignmentExpressionOrHigher() store.NodeRef {
	return p.parseAssignmentExpressionOrHigherWorker(true /*allowReturnTypeInArrowFunction*/)
}

func (p *Parser) parseAssignmentExpressionOrHigherWorker(allowReturnTypeInArrowFunction bool) store.NodeRef {
	//  AssignmentExpression[in,yield]:
	//      1) ConditionalExpression[?in,?yield]
	//      2) LeftHandSideExpression = AssignmentExpression[?in,?yield]
	//      3) LeftHandSideExpression AssignmentOperator AssignmentExpression[?in,?yield]
	//      4) ArrowFunctionExpression[?in,?yield]
	//      5) AsyncArrowFunctionExpression[in,yield,await]
	//      6) [+Yield] YieldExpression[?In]
	//
	// Note: for ease of implementation we treat productions '2' and '3' as the same thing.
	// (i.e. they're both BinaryExpressions with an assignment operator in it).
	// First, do the simple check if we have a YieldExpression (production '6').
	if p.isYieldExpression() {
		return p.parseYieldExpression()
	}
	// Then, check if we have an arrow function (production '4' and '5') that starts with a parenthesized
	// parameter list or is an async arrow function.
	// AsyncArrowFunctionExpression:
	//      1) async[no LineTerminator here]AsyncArrowBindingIdentifier[?Yield][no LineTerminator here]=>AsyncConciseBody[?In]
	//      2) CoverCallExpressionAndAsyncArrowHead[?Yield, ?Await][no LineTerminator here]=>AsyncConciseBody[?In]
	// Production (1) of AsyncArrowFunctionExpression is parsed in "tryParseAsyncSimpleArrowFunctionExpression".
	// And production (2) is parsed in "tryParseParenthesizedArrowFunctionExpression".
	//
	// If we do successfully parse arrow-function, we must *not* recurse for productions 1, 2 or 3. An ArrowFunction is
	// not a LeftHandSideExpression, nor does it start a ConditionalExpression.  So we are done
	// with AssignmentExpression if we see one.
	arrowExpression := p.tryParseParenthesizedArrowFunctionExpression(allowReturnTypeInArrowFunction)
	if arrowExpression != store.NoNodeRef {
		return arrowExpression
	}
	arrowExpression = p.tryParseAsyncSimpleArrowFunctionExpression(allowReturnTypeInArrowFunction)
	if arrowExpression != store.NoNodeRef {
		return arrowExpression
	}
	// arrowExpression2 := p.tryParseAsyncSimpleArrowFunctionExpression(allowReturnTypeInArrowFunction)
	// if arrowExpression2 != store.NoNodeRef {
	// 	return arrowExpression2
	// }
	// Now try to see if we're in production '1', '2' or '3'.  A conditional expression can
	// start with a LogicalOrExpression, while the assignment productions can only start with
	// LeftHandSideExpressions.
	//
	// So, first, we try to just parse out a BinaryExpression.  If we get something that is a
	// LeftHandSide or higher, then we can try to parse out the assignment expression part.
	// Otherwise, we try to parse out the conditional expression bit.  We want to allow any
	// binary expression here, so we pass in the 'lowest' precedence here so that it matches
	// and consumes anything.
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	expr := p.parseBinaryExpressionOrHigher(ast.OperatorPrecedenceLowest)
	// To avoid a look-ahead, we did not handle the case of an arrow function with a single un-parenthesized
	// parameter ('x => ...') above. We handle it here by checking if the parsed expression was a single
	// identifier and the current token is an arrow.
	if p.b.View(expr).Kind() == ast.KindIdentifier && p.token == ast.KindEqualsGreaterThanToken {
		return p.parseSimpleArrowFunctionExpression(pos, expr, allowReturnTypeInArrowFunction, jsdoc, store.NoListRef /*asyncModifier*/)
	}
	// Now see if we might be in cases '2' or '3'.
	// If the expression was a LHS expression, and we have an assignment operator, then
	// we're in '2' or '3'. Consume the assignment and return.
	//
	// Note: we call reScanGreaterToken so that we get an appropriately merged token
	// for cases like `> > =` becoming `>>=`
	if isLeftHandSideExpression(p.b.View(expr).Kind()) && ast.IsAssignmentOperator(p.reScanGreaterThanToken()) {
		return p.makeBinaryExpression(expr, p.parseTokenNode(), p.parseAssignmentExpressionOrHigherWorker(allowReturnTypeInArrowFunction), pos)
	}
	// It wasn't an assignment or a lambda.  This is a conditional expression:
	return p.parseConditionalExpressionRest(expr, pos, allowReturnTypeInArrowFunction)
}

func (p *Parser) isYieldExpression() bool {
	if p.token == ast.KindYieldKeyword {
		// If we have a 'yield' keyword, and this is a context where yield expressions are
		// allowed, then definitely parse out a yield expression.
		if p.inYieldContext() {
			return true
		}

		// We're in a context where 'yield expr' is not allowed.  However, if we can
		// definitely tell that the user was trying to parse a 'yield expr' and not
		// just a normal expr that start with a 'yield' identifier, then parse out
		// a 'yield expr'.  We can then report an error later that they are only
		// allowed in generator expressions.
		//
		// for example, if we see 'yield(foo)', then we'll have to treat that as an
		// invocation expression of something called 'yield'.  However, if we have
		// 'yield foo' then that is not legal as a normal expression, so we can
		// definitely recognize this as a yield expression.
		//
		// for now we just check if the next token is an identifier.  More heuristics
		// can be added here later as necessary.  We just need to make sure that we
		// don't accidentally consume something legal.
		return p.lookAhead((*Parser).nextTokenIsIdentifierOrKeywordOrLiteralOnSameLine)
	}
	return false
}

func (p *Parser) parseYieldExpression() store.NodeRef {
	pos := p.nodePos()
	// YieldExpression[In] :
	//      yield
	//      yield [no LineTerminator here] [Lexical goal InputElementRegExp]AssignmentExpression[?In, Yield]
	//      yield [no LineTerminator here] * [Lexical goal InputElementRegExp]AssignmentExpression[?In, Yield]
	p.nextToken()
	var result store.NodeRef
	if !p.hasPrecedingLineBreak() && (p.token == ast.KindAsteriskToken || p.isStartOfExpression()) {
		asteriskToken := p.parseOptionalToken(ast.KindAsteriskToken)
		expression := p.parseAssignmentExpressionOrHigher()
		result = p.b.NewYieldExpression(p.flags(), int32(pos), int32(p.nodePos()), asteriskToken, expression)
	} else {
		// if the next token is not on the same line as yield.  or we don't have an '*' or
		// the start of an expression, then this is just a simple "yield" expression.
		result = p.b.NewYieldExpression(p.flags(), int32(pos), int32(p.nodePos()), store.NoNodeRef /*asteriskToken*/, store.NoNodeRef /*expression*/)
	}
	return result
}

func (p *Parser) isParenthesizedArrowFunctionExpression() core.Tristate {
	if p.token == ast.KindOpenParenToken || p.token == ast.KindLessThanToken || p.token == ast.KindAsyncKeyword {
		state := p.mark()
		result := p.nextIsParenthesizedArrowFunctionExpression()
		p.rewind(state)
		return result
	}
	if p.token == ast.KindEqualsGreaterThanToken {
		// ERROR RECOVERY TWEAK:
		// If we see a standalone => try to parse it as an arrow function expression as that's
		// likely what the user intended to write.
		return core.TSTrue
	}
	// Definitely not a parenthesized arrow function.
	return core.TSFalse
}

func (p *Parser) nextIsParenthesizedArrowFunctionExpression() core.Tristate {
	if p.token == ast.KindAsyncKeyword {
		p.nextToken()
		if p.hasPrecedingLineBreak() {
			return core.TSFalse
		}
		if p.token != ast.KindOpenParenToken && p.token != ast.KindLessThanToken {
			return core.TSFalse
		}
	}
	first := p.token
	second := p.nextToken()
	if first == ast.KindOpenParenToken {
		if second == ast.KindCloseParenToken {
			// Simple cases: "() =>", "(): ", and "() {".
			// This is an arrow function with no parameters.
			// The last one is not actually an arrow function,
			// but this is probably what the user intended.
			third := p.nextToken()
			switch third {
			case ast.KindEqualsGreaterThanToken, ast.KindColonToken, ast.KindOpenBraceToken:
				return core.TSTrue
			}
			return core.TSFalse
		}
		// If encounter "([" or "({", this could be the start of a binding pattern.
		// Examples:
		//      ([ x ]) => { }
		//      ({ x }) => { }
		//      ([ x ])
		//      ({ x })
		if second == ast.KindOpenBracketToken || second == ast.KindOpenBraceToken {
			return core.TSUnknown
		}
		// Simple case: "(..."
		// This is an arrow function with a rest parameter.
		if second == ast.KindDotDotDotToken {
			return core.TSTrue
		}
		// Check for "(xxx yyy", where xxx is a modifier and yyy is an identifier. This
		// isn't actually allowed, but we want to treat it as a lambda so we can provide
		// a good error message.
		if ast.IsModifierKind(second) && second != ast.KindAsyncKeyword && p.lookAhead((*Parser).nextTokenIsIdentifier) {
			if p.nextToken() == ast.KindAsKeyword {
				// https://github.com/microsoft/TypeScript/issues/44466
				return core.TSFalse
			}
			return core.TSTrue
		}
		// If we had "(" followed by something that's not an identifier,
		// then this definitely doesn't look like a lambda.  "this" is not
		// valid, but we want to parse it and then give a semantic error.
		if !p.isIdentifier() && second != ast.KindThisKeyword {
			return core.TSFalse
		}
		switch p.nextToken() {
		case ast.KindColonToken:
			// If we have something like "(a:", then we must have a
			// type-annotated parameter in an arrow function expression.
			return core.TSTrue
		case ast.KindQuestionToken:
			p.nextToken()
			// If we have "(a?:" or "(a?," or "(a?=" or "(a?)" then it is definitely a lambda.
			if p.token == ast.KindColonToken || p.token == ast.KindCommaToken || p.token == ast.KindEqualsToken || p.token == ast.KindCloseParenToken {
				return core.TSTrue
			}
			// Otherwise it is definitely not a lambda.
			return core.TSFalse
		case ast.KindCommaToken, ast.KindEqualsToken, ast.KindCloseParenToken:
			// If we have "(a," or "(a=" or "(a)" this *could* be an arrow function
			return core.TSUnknown
		}
		// It is definitely not an arrow function
		return core.TSFalse
	} else {
		debug.Assert(first == ast.KindLessThanToken)
		// If we have "<" not followed by an identifier,
		// then this definitely is not an arrow function.
		if !p.isIdentifier() && p.token != ast.KindConstKeyword {
			return core.TSFalse
		}
		// JSX overrides
		if p.languageVariant == core.LanguageVariantJSX {
			isArrowFunctionInJsx := p.lookAhead(func(p *Parser) bool {
				p.parseOptional(ast.KindConstKeyword)
				third := p.nextToken()
				if third == ast.KindExtendsKeyword {
					fourth := p.nextToken()
					switch fourth {
					case ast.KindEqualsToken, ast.KindGreaterThanToken, ast.KindSlashToken:
						return false
					}
					return true
				} else if third == ast.KindCommaToken || third == ast.KindEqualsToken {
					return true
				}
				return false
			})
			if isArrowFunctionInJsx {
				return core.TSTrue
			}
			return core.TSFalse
		}
		// This *could* be a parenthesized arrow function.
		return core.TSUnknown
	}
}

func (p *Parser) tryParseParenthesizedArrowFunctionExpression(allowReturnTypeInArrowFunction bool) store.NodeRef {
	tristate := p.isParenthesizedArrowFunctionExpression()
	if tristate == core.TSFalse {
		// It's definitely not a parenthesized arrow function expression.
		return store.NoNodeRef
	}
	// If we definitely have an arrow function, then we can just parse one, not requiring a
	// following => or { token. Otherwise, we *might* have an arrow function.  Try to parse
	// it out, but don't allow any ambiguity, and return 'undefined' if this could be an
	// expression instead.
	if tristate == core.TSTrue {
		return p.parseParenthesizedArrowFunctionExpression(true /*allowAmbiguity*/, true /*allowReturnTypeInArrowFunction*/)
	}
	state := p.mark()
	result := p.parsePossibleParenthesizedArrowFunctionExpression(allowReturnTypeInArrowFunction)
	if result == store.NoNodeRef {
		p.rewind(state)
	}
	return result
}

func (p *Parser) parseParenthesizedArrowFunctionExpression(allowAmbiguity bool, allowReturnTypeInArrowFunction bool) store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	modifiers := p.parseModifiersForArrowFunction()
	isAsync := p.modifierListHasAsync(modifiers)
	signatureFlags := core.IfElse(isAsync, ParseFlagsAwait, ParseFlagsNone)
	// Arrow functions are never generators.
	//
	// If we're speculatively parsing a signature for a parenthesized arrow function, then
	// we have to have a complete parameter list.  Otherwise we might see something like
	// a => (b => c)
	// And think that "(b =>" was actually a parenthesized arrow function with a missing
	// close paren.
	typeParameters := p.parseTypeParameters()
	var parameters store.ListRef
	if !p.parseExpected(ast.KindOpenParenToken) {
		if !allowAmbiguity {
			return store.NoNodeRef
		}
		parameters = p.createMissingList()
	} else {
		if !allowAmbiguity {
			maybeParameters := p.parseParametersWorker(signatureFlags, allowAmbiguity)
			if maybeParameters == store.NoListRef {
				return store.NoNodeRef
			}
			parameters = maybeParameters
		} else {
			parameters = p.parseParametersWorker(signatureFlags, allowAmbiguity)
		}
		if !p.parseExpected(ast.KindCloseParenToken) && !allowAmbiguity {
			return store.NoNodeRef
		}
	}
	hasReturnColon := p.token == ast.KindColonToken
	returnType := p.parseReturnType(ast.KindColonToken /*isType*/, false)
	if returnType != store.NoNodeRef && !allowAmbiguity && p.typeHasArrowFunctionBlockingParseError(p.b.View(returnType)) {
		return store.NoNodeRef
	}
	// Parsing a signature isn't enough.
	// Parenthesized arrow signatures often look like other valid expressions.
	// For instance:
	//  - "(x = 10)" is an assignment expression parsed as a signature with a default parameter value.
	//  - "(x,y)" is a comma expression parsed as a signature with two parameters.
	//  - "a ? (b): c" will have "(b):" parsed as a signature with a return type annotation.
	//  - "a ? (b): function() {}" will too, since function() is a valid JSDoc function type.
	//  - "a ? (b): (function() {})" as well, but inside of a parenthesized type with an arbitrary amount of nesting.
	//
	// So we need just a bit of lookahead to ensure that it can only be a signature.
	unwrappedType := returnType
	for unwrappedType != store.NoNodeRef && p.b.View(unwrappedType).Kind() == ast.KindParenthesizedType {
		unwrappedType = p.b.View(unwrappedType).Type().Ref() // Skip parens if need be
	}
	if !allowAmbiguity && p.token != ast.KindEqualsGreaterThanToken && p.token != ast.KindOpenBraceToken {
		// Returning undefined here will cause our caller to rewind to where we started from.
		return store.NoNodeRef
	}
	// If we have an arrow, then try to parse the body. Even if not, try to parse if we
	// have an opening brace, just in case we're in an error state.
	lastToken := p.token
	equalsGreaterThanToken := p.parseExpectedToken(ast.KindEqualsGreaterThanToken)
	var body store.NodeRef
	if lastToken == ast.KindEqualsGreaterThanToken || lastToken == ast.KindOpenBraceToken {
		body = p.parseArrowFunctionExpressionBody(isAsync, allowReturnTypeInArrowFunction)
	} else {
		body = p.parseIdentifier()
	}
	// Given:
	//     x ? y => ({ y }) : z => ({ z })
	// We try to parse the body of the first arrow function by looking at:
	//     ({ y }) : z => ({ z })
	// This is a valid arrow function with "z" as the return type.
	//
	// But, if we're in the true side of a conditional expression, this colon
	// terminates the expression, so we cannot allow a return type if we aren't
	// certain whether or not the preceding text was parsed as a parameter list.
	//
	// For example,
	//     a() ? (b: number, c?: string): void => d() : e
	// is determined by isParenthesizedArrowFunctionExpression to unambiguously
	// be an arrow expression, so we allow a return type.
	if !allowReturnTypeInArrowFunction && hasReturnColon {
		// However, if the arrow function we were able to parse is followed by another colon
		// as in:
		//     a ? (x): string => x : null
		// Then allow the arrow function, and treat the second colon as terminating
		// the conditional expression. It's okay to do this because this code would
		// be a syntax error in JavaScript (as the second colon shouldn't be there).
		if p.token != ast.KindColonToken {
			return store.NoNodeRef
		}
	}
	result := p.b.NewArrowFunction(p.flags(), int32(pos), int32(p.nodePos()), modifiers, typeParameters, parameters, returnType, store.NoNodeRef /*fullSignature*/, equalsGreaterThanToken, body)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	p.checkJSSyntax(result)
	return result
}

func (p *Parser) parseModifiersForArrowFunction() store.ListRef {
	if p.token == ast.KindAsyncKeyword {
		pos := p.nodePos()
		p.nextToken()
		modifier := p.b.NewToken(ast.KindAsyncKeyword, p.flags(), int32(pos), int32(p.nodePos()))
		return p.b.List(int32(pos), int32(p.nodePos()), []store.NodeRef{modifier})
	}
	return store.NoListRef
}

// If true, we should abort parsing an error function.
func (p *Parser) typeHasArrowFunctionBlockingParseError(node store.Node) bool {
	switch node.Kind() {
	case ast.KindTypeReference:
		return nodeIsMissing(node.AsTypeReferenceNode().TypeName())
	case ast.KindFunctionType, ast.KindConstructorType:
		return p.isMissingNodeList(node.Parameters().Ref()) || p.typeHasArrowFunctionBlockingParseError(node.Type())
	case ast.KindParenthesizedType:
		return p.typeHasArrowFunctionBlockingParseError(node.Type())
	}
	return false
}

func (p *Parser) parseArrowFunctionExpressionBody(isAsync bool, allowReturnTypeInArrowFunction bool) store.NodeRef {
	if p.token == ast.KindOpenBraceToken {
		return p.parseFunctionBlock(core.IfElse(isAsync, ParseFlagsAwait, ParseFlagsNone), nil /*diagnosticMessage*/)
	}
	if p.token != ast.KindSemicolonToken && p.token != ast.KindFunctionKeyword && p.token != ast.KindClassKeyword && p.isStartOfStatement() && !p.isStartOfExpressionStatement() {
		// Check if we got a plain statement (i.e. no expression-statements, no function/class expressions/declarations)
		//
		// Here we try to recover from a potential error situation in the case where the
		// user meant to supply a block. For example, if the user wrote:
		//
		//  a =>
		//      let v = 0;
		//  }
		//
		// they may be missing an open brace.  Check to see if that's the case so we can
		// try to recover better.  If we don't do this, then the next close curly we see may end
		// up preemptively closing the containing construct.
		//
		// Note: even when 'IgnoreMissingOpenBrace' is passed, parseBody will still error.
		return p.parseFunctionBlock(ParseFlagsIgnoreMissingOpenBrace|core.IfElse(isAsync, ParseFlagsAwait, ParseFlagsNone), nil /*diagnosticMessage*/)
	}
	saveContextFlags := p.contextFlags
	p.setContextFlags(ast.NodeFlagsAwaitContext, isAsync)
	p.setContextFlags(ast.NodeFlagsYieldContext, false)
	node := p.parseAssignmentExpressionOrHigherWorker(allowReturnTypeInArrowFunction)
	p.contextFlags = saveContextFlags
	return node
}

func (p *Parser) isStartOfExpressionStatement() bool {
	// As per the grammar, none of '{' or 'function' or 'class' can start an expression statement.
	return p.token != ast.KindOpenBraceToken && p.token != ast.KindFunctionKeyword && p.token != ast.KindClassKeyword && p.token != ast.KindAtToken && p.isStartOfExpression()
}

func (p *Parser) parsePossibleParenthesizedArrowFunctionExpression(allowReturnTypeInArrowFunction bool) store.NodeRef {
	tokenPos := p.scanner.TokenStart()
	if p.notParenthesizedArrow.Has(tokenPos) {
		return store.NoNodeRef
	}
	result := p.parseParenthesizedArrowFunctionExpression(false /*allowAmbiguity*/, allowReturnTypeInArrowFunction)
	if result == store.NoNodeRef {
		p.notParenthesizedArrow.Add(tokenPos)
	}
	return result
}

func (p *Parser) tryParseAsyncSimpleArrowFunctionExpression(allowReturnTypeInArrowFunction bool) store.NodeRef {
	// We do a check here so that we won't be doing unnecessarily call to "lookAhead"
	if p.token == ast.KindAsyncKeyword && p.lookAhead((*Parser).nextIsUnParenthesizedAsyncArrowFunction) {
		pos := p.nodePos()
		jsdoc := p.jsdocScannerInfo()
		asyncModifier := p.parseModifiersForArrowFunction()
		expr := p.parseBinaryExpressionOrHigher(ast.OperatorPrecedenceLowest)
		return p.parseSimpleArrowFunctionExpression(pos, expr, allowReturnTypeInArrowFunction, jsdoc, asyncModifier)
	}
	return store.NoNodeRef
}

func (p *Parser) nextIsUnParenthesizedAsyncArrowFunction() bool {
	// AsyncArrowFunctionExpression:
	//      1) async[no LineTerminator here]AsyncArrowBindingIdentifier[?Yield][no LineTerminator here]=>AsyncConciseBody[?In]
	//      2) CoverCallExpressionAndAsyncArrowHead[?Yield, ?Await][no LineTerminator here]=>AsyncConciseBody[?In]
	if p.token == ast.KindAsyncKeyword {
		p.nextToken()
		// If the "async" is followed by "=>" token then it is not a beginning of an async arrow-function
		// but instead a simple arrow-function which will be parsed inside "parseAssignmentExpressionOrHigher"
		if p.hasPrecedingLineBreak() || p.token == ast.KindEqualsGreaterThanToken {
			return false
		}
		// Check for un-parenthesized AsyncArrowFunction
		if !p.isIdentifier() {
			return false
		}
		p.nextTokenWithoutCheck()
		return !p.hasPrecedingLineBreak() && p.token == ast.KindEqualsGreaterThanToken
	}
	return false
}

func (p *Parser) parseSimpleArrowFunctionExpression(pos int, identifier store.NodeRef, allowReturnTypeInArrowFunction bool, jsdoc jsdocScannerInfo, asyncModifier store.ListRef) store.NodeRef {
	debug.Assert(p.token == ast.KindEqualsGreaterThanToken, "parseSimpleArrowFunctionExpression should only have been called if we had a =>")
	parameter := p.b.NewParameterDeclaration(p.flags(), int32(p.pos(identifier)), int32(p.nodePos()), store.NoListRef /*modifiers*/, store.NoNodeRef /*dotDotDotToken*/, identifier, store.NoNodeRef /*questionToken*/, store.NoNodeRef /*typeNode*/, store.NoNodeRef /*initializer*/)
	parameters := p.b.List(int32(p.pos(parameter)), int32(p.end(parameter)), []store.NodeRef{parameter})
	equalsGreaterThanToken := p.parseExpectedToken(ast.KindEqualsGreaterThanToken)
	body := p.parseArrowFunctionExpressionBody(asyncModifier != store.NoListRef /*isAsync*/, allowReturnTypeInArrowFunction)
	result := p.b.NewArrowFunction(p.flags(), int32(pos), int32(p.nodePos()), asyncModifier, store.NoListRef /*typeParameters*/, parameters, store.NoNodeRef /*returnType*/, store.NoNodeRef /*fullSignature*/, equalsGreaterThanToken, body)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseConditionalExpressionRest(leftOperand store.NodeRef, pos int, allowReturnTypeInArrowFunction bool) store.NodeRef {
	// Note: we are passed in an expression which was produced from parseBinaryExpressionOrHigher.
	questionToken := p.parseOptionalToken(ast.KindQuestionToken)
	if questionToken == store.NoNodeRef {
		return leftOperand
	}
	// Note: we explicitly 'allowIn' in the whenTrue part of the condition expression, and
	// we do not that for the 'whenFalse' part.
	saveContextFlags := p.contextFlags
	p.setContextFlags(ast.NodeFlagsDisallowInContext, false)
	trueExpression := p.parseAssignmentExpressionOrHigherWorker(false /*allowReturnTypeInArrowFunction*/)
	p.contextFlags = saveContextFlags
	colonToken := p.parseExpectedToken(ast.KindColonToken)
	var falseExpression store.NodeRef
	if nodeIsPresent(p.b.View(colonToken)) {
		falseExpression = p.parseAssignmentExpressionOrHigherWorker(allowReturnTypeInArrowFunction)
	} else {
		falseExpression = p.createMissingIdentifier()
	}
	return p.b.NewConditionalExpression(p.flags(), int32(pos), int32(p.nodePos()), leftOperand, questionToken, trueExpression, colonToken, falseExpression)
}

func (p *Parser) parseBinaryExpressionOrHigher(precedence ast.OperatorPrecedence) store.NodeRef {
	pos := p.nodePos()
	leftOperand := p.parseUnaryExpressionOrHigher()
	return p.parseBinaryExpressionRest(precedence, leftOperand, pos)
}

// shouldConsumeBinaryOperator reports whether an operator binds before the operator represented by currentPrecedence.
// At equal precedence, only the right-associative exponentiation operator binds first.
func shouldConsumeBinaryOperator(operator ast.Kind, operatorPrecedence ast.OperatorPrecedence, currentPrecedence ast.OperatorPrecedence) bool {
	if operatorPrecedence > currentPrecedence {
		return true
	}
	return operatorPrecedence == currentPrecedence && operator == ast.KindAsteriskAsteriskToken
}

func (p *Parser) parseBinaryExpressionRest(precedence ast.OperatorPrecedence, leftOperand store.NodeRef, pos int) store.NodeRef {
	lastOperand := leftOperand
	for {
		// We either have a binary operator here, or we're finished.  We call
		// reScanGreaterToken so that we merge token sequences like > and = into >=
		operator := p.reScanGreaterThanToken()
		newPrecedence := ast.GetBinaryOperatorPrecedence(operator)
		// Check the precedence to see if we should "take" this operator
		// - For left associative operator (all operator but **), consume the operator,
		//   recursively call the function below, and parse binaryExpression as a rightOperand
		//   of the caller if the new precedence of the operator is greater then or equal to the current precedence.
		//   For example:
		//      a - b - c;
		//            ^token; leftOperand = b. Return b to the caller as a rightOperand
		//      a * b - c
		//            ^token; leftOperand = b. Return b to the caller as a rightOperand
		//      a - b * c;
		//            ^token; leftOperand = b. Return b * c to the caller as a rightOperand
		// - For right associative operator (**), consume the operator, recursively call the function
		//   and parse binaryExpression as a rightOperand of the caller if the new precedence of
		//   the operator is strictly grater than the current precedence
		//   For example:
		//      a ** b ** c;
		//             ^^token; leftOperand = b. Return b ** c to the caller as a rightOperand
		//      a - b ** c;
		//            ^^token; leftOperand = b. Return b ** c to the caller as a rightOperand
		//      a ** b - c
		//             ^token; leftOperand = b. Return b to the caller as a rightOperand
		if !shouldConsumeBinaryOperator(operator, newPrecedence, precedence) {
			break
		}
		if operator == ast.KindInKeyword && p.inDisallowInContext() {
			break
		}
		if operator == ast.KindAsKeyword || operator == ast.KindSatisfiesKeyword {
			// Make sure we *do* perform ASI for constructs like this:
			//    var x = foo
			//    as (Bar)
			// This should be parsed as an initialized variable, followed
			// by a function call to 'as' with the argument 'Bar'
			if p.hasPrecedingLineBreak() {
				break
			} else {
				p.nextToken()
				// When we have 'a ## b as SomeType $$ c' or 'a ## b satisfies SomeType $$ c', where ## and $$
				// are binary operators, we want to stop parsing when $$ would bind before ## after erasing the
				// assertion. See https://github.com/microsoft/TypeScript/issues/63527.
				lastPrecedence := ast.OperatorPrecedenceHighest
				if p.b.View(lastOperand).Kind() == ast.KindBinaryExpression {
					lastPrecedence = ast.GetBinaryOperatorPrecedence(p.b.View(lastOperand).AsBinaryExpression().OperatorToken().Kind())
				}
				if operator == ast.KindSatisfiesKeyword {
					leftOperand = p.makeSatisfiesExpression(leftOperand, p.parseType())
				} else {
					leftOperand = p.makeAsExpression(leftOperand, p.parseType())
				}
				// Stop if the next operator would bind before the last operator when the assertion is erased.
				nextOperator := p.reScanGreaterThanToken()
				nextPrecedence := ast.GetBinaryOperatorPrecedence(nextOperator)
				if shouldConsumeBinaryOperator(nextOperator, nextPrecedence, lastPrecedence) {
					break
				}
			}
		} else {
			leftOperand = p.makeBinaryExpression(leftOperand, p.parseTokenNode(), p.parseBinaryExpressionOrHigher(newPrecedence), pos)
			lastOperand = leftOperand
		}
	}
	return leftOperand
}

func (p *Parser) makeSatisfiesExpression(expression store.NodeRef, typeNode store.NodeRef) store.NodeRef {
	return p.checkJSSyntax(p.b.NewSatisfiesExpression(p.flags(), int32(p.pos(expression)), int32(p.nodePos()), expression, typeNode))
}

func (p *Parser) makeAsExpression(left store.NodeRef, right store.NodeRef) store.NodeRef {
	return p.checkJSSyntax(p.b.NewAsExpression(p.flags(), int32(p.pos(left)), int32(p.nodePos()), left, right))
}

func (p *Parser) makeBinaryExpression(left store.NodeRef, operatorToken store.NodeRef, right store.NodeRef, pos int) store.NodeRef {
	return p.b.NewBinaryExpression(p.flags(), int32(pos), int32(p.nodePos()), store.NoListRef /*modifiers*/, left, store.NoNodeRef /*typeNode*/, operatorToken, right)
}

func (p *Parser) parseUnaryExpressionOrHigher() store.NodeRef {
	// ES7 UpdateExpression:
	//      1) LeftHandSideExpression[?Yield]
	//      2) LeftHandSideExpression[?Yield][no LineTerminator here]++
	//      3) LeftHandSideExpression[?Yield][no LineTerminator here]--
	//      4) ++UnaryExpression[?Yield]
	//      5) --UnaryExpression[?Yield]
	if p.isUpdateExpression() {
		pos := p.nodePos()
		updateExpression := p.parseUpdateExpression()
		if p.token == ast.KindAsteriskAsteriskToken {
			return p.parseBinaryExpressionRest(ast.GetBinaryOperatorPrecedence(p.token), updateExpression, pos)
		}
		return updateExpression
	}
	// ES7 UnaryExpression:
	//      1) UpdateExpression[?yield]
	//      2) delete UpdateExpression[?yield]
	//      3) void UpdateExpression[?yield]
	//      4) typeof UpdateExpression[?yield]
	//      5) + UpdateExpression[?yield]
	//      6) - UpdateExpression[?yield]
	//      7) ~ UpdateExpression[?yield]
	//      8) ! UpdateExpression[?yield]
	unaryOperator := p.token
	simpleUnaryExpression := p.parseSimpleUnaryExpression()
	if p.token == ast.KindAsteriskAsteriskToken {
		pos := scanner.SkipTrivia(p.sourceText, p.pos(simpleUnaryExpression))
		end := p.end(simpleUnaryExpression)
		if p.b.View(simpleUnaryExpression).Kind() == ast.KindTypeAssertionExpression {
			p.parseErrorAt(pos, end, diagnostics.A_type_assertion_expression_is_not_allowed_in_the_left_hand_side_of_an_exponentiation_expression_Consider_enclosing_the_expression_in_parentheses)
		} else {
			debug.Assert(isKeywordOrPunctuation(unaryOperator))
			p.parseErrorAt(pos, end, diagnostics.An_unary_expression_with_the_0_operator_is_not_allowed_in_the_left_hand_side_of_an_exponentiation_expression_Consider_enclosing_the_expression_in_parentheses, scanner.TokenToString(unaryOperator))
		}
	}
	return simpleUnaryExpression
}

func (p *Parser) isUpdateExpression() bool {
	switch p.token {
	case ast.KindPlusToken, ast.KindMinusToken, ast.KindTildeToken, ast.KindExclamationToken, ast.KindDeleteKeyword, ast.KindTypeOfKeyword, ast.KindVoidKeyword, ast.KindAwaitKeyword:
		return false
	case ast.KindLessThanToken:
		return p.languageVariant == core.LanguageVariantJSX
	}
	return true
}

func (p *Parser) parseUpdateExpression() store.NodeRef {
	pos := p.nodePos()
	if p.token == ast.KindPlusPlusToken || p.token == ast.KindMinusMinusToken {
		operator := p.token
		p.nextToken()
		operand := p.parseLeftHandSideExpressionOrHigher()
		return p.b.NewPrefixUnaryExpression(p.flags(), int32(pos), int32(p.nodePos()), operator, operand)
	} else if p.languageVariant == core.LanguageVariantJSX && p.token == ast.KindLessThanToken && p.lookAhead((*Parser).nextTokenIsIdentifierOrKeywordOrGreaterThan) {
		// JSXElement is part of primaryExpression
		return p.parseJsxElementOrSelfClosingElementOrFragment(true /*inExpressionContext*/, -1 /*topInvalidNodePosition*/, store.NoNodeRef /*openingTag*/, false /*mustBeUnary*/)
	}
	expression := p.parseLeftHandSideExpressionOrHigher()
	if (p.token == ast.KindPlusPlusToken || p.token == ast.KindMinusMinusToken) && !p.hasPrecedingLineBreak() {
		operator := p.token
		p.nextToken()
		return p.b.NewPostfixUnaryExpression(p.flags(), int32(pos), int32(p.nodePos()), expression, operator)
	}
	return expression
}

func (p *Parser) parseJsxElementOrSelfClosingElementOrFragment(inExpressionContext bool, topInvalidNodePosition int, openingTag store.NodeRef, mustBeUnary bool) store.NodeRef {
	pos := p.nodePos()
	opening := p.parseJsxOpeningOrSelfClosingElementOrOpeningFragment(inExpressionContext)
	var result store.NodeRef
	switch p.b.View(opening).Kind() {
	case ast.KindJsxOpeningElement:
		children := p.parseJsxChildren(opening)
		var closingElement store.NodeRef
		lastChild := core.LastOrNil(p.b.ViewList(children).Refs())
		if lastChild != store.NoNodeRef && p.b.View(lastChild).Kind() == ast.KindJsxElement &&
			!tagNamesAreEquivalent(p.b.View(lastChild).AsJsxElement().OpeningElement().TagName(), p.b.View(lastChild).AsJsxElement().ClosingElement().TagName()) &&
			tagNamesAreEquivalent(p.b.View(opening).TagName(), p.b.View(lastChild).AsJsxElement().ClosingElement().TagName()) {
			// when an unclosed JsxOpeningElement incorrectly parses its parent's JsxClosingElement,
			// restructure (<div>(...<span>...</div>)) --> (<div>(...<span>...</>)</div>)
			// (no need to error; the parent will error)
			last := p.b.View(lastChild).AsJsxElement()
			lastOpening, lastChildren, lastClosing := last.OpeningElement().Ref(), last.Children().Ref(), last.ClosingElement().Ref()
			lastOpeningPos, childrenPos := last.OpeningElement().Pos(), p.b.ViewList(children).Pos()
			end := last.Children().End()
			missingIdentifier := p.newIdentifier("", int(end), int(end))
			newClosingElement := p.b.NewJsxClosingElement(p.flags(), end, end, missingIdentifier)
			// The constructor reparents the opening element and the children of the discarded
			// parse result; the discarded JsxElement stays as a dead node.
			newLast := p.b.NewJsxElement(p.flags(), lastOpeningPos, end, lastOpening, lastChildren, newClosingElement)
			mark := len(p.elems)
			p.elems = append(p.elems, p.b.ViewList(children).Refs()[0:p.b.ViewList(children).Len()-1]...)
			p.elems = append(p.elems, newLast)
			children = p.b.List(childrenPos, p.b.View(newLast).End(), p.elems[mark:])
			p.elems = p.elems[:mark]
			closingElement = lastClosing
		} else {
			closingElement = p.parseJsxClosingElement(opening, inExpressionContext)
			if !tagNamesAreEquivalent(p.b.View(opening).TagName(), p.b.View(closingElement).TagName()) {
				if openingTag != store.NoNodeRef && p.b.View(openingTag).Kind() == ast.KindJsxOpeningElement && tagNamesAreEquivalent(p.b.View(closingElement).TagName(), p.b.View(openingTag).TagName()) {
					// opening incorrectly matched with its parent's closing -- put error on opening
					p.parseErrorAtRange(p.loc(p.b.View(opening).TagName().Ref()), diagnostics.JSX_element_0_has_no_corresponding_closing_tag, getTextOfNodeFromSourceText(p.sourceText, p.b.View(opening).TagName(), false /*includeTrivia*/))
				} else {
					// other opening/closing mismatches -- put error on closing
					p.parseErrorAtRange(p.loc(p.b.View(closingElement).TagName().Ref()), diagnostics.Expected_corresponding_JSX_closing_tag_for_0, getTextOfNodeFromSourceText(p.sourceText, p.b.View(opening).TagName(), false /*includeTrivia*/))
				}
			}
		}
		// The constructor (re)sets the parent of the closing element, which may come from a discarded parse result.
		result = p.b.NewJsxElement(p.flags(), int32(pos), int32(p.nodePos()), opening, children, closingElement)
	case ast.KindJsxOpeningFragment:
		children := p.parseJsxChildren(opening)
		closingFragment := p.parseJsxClosingFragment(inExpressionContext)
		result = p.b.NewJsxFragment(p.flags(), int32(pos), int32(p.nodePos()), opening, children, closingFragment)
	case ast.KindJsxSelfClosingElement:
		// Nothing else to do for self-closing elements
		result = opening
	default:
		panic("Unhandled case in parseJsxElementOrSelfClosingElementOrFragment")
	}
	// If the user writes the invalid code '<div></div><div></div>' in an expression context (i.e. not wrapped in
	// an enclosing tag), we'll naively try to parse   ^ this as a 'less than' operator and the remainder of the tag
	// as garbage, which will cause the formatter to badly mangle the JSX. Perform a speculative parse of a JSX
	// element if we see a < token so that we can wrap it in a synthetic binary expression so the formatter
	// does less damage and we can report a better error.
	// Since JSX elements are invalid < operands anyway, this lookahead parse will only occur in error scenarios
	// of one sort or another.
	// If we are in a unary context, we can't do this recovery; the binary expression we return here is not
	// a valid UnaryExpression and will cause problems later.
	if !mustBeUnary && inExpressionContext && p.token == ast.KindLessThanToken {
		topBadPos := topInvalidNodePosition
		if topBadPos < 0 {
			topBadPos = p.pos(result)
		}
		invalidElement := p.parseJsxElementOrSelfClosingElementOrFragment( /*inExpressionContext*/ true, topBadPos, store.NoNodeRef, false)
		// The Pointer token never goes through finishNode, so its flags are none.
		operatorToken := p.b.NewToken(ast.KindCommaToken, ast.NodeFlagsNone, int32(p.pos(invalidElement)), int32(p.pos(invalidElement)))
		p.parseErrorAt(scanner.SkipTrivia(p.sourceText, topBadPos), p.end(invalidElement), diagnostics.JSX_expressions_must_have_one_parent_element)
		result = p.b.NewBinaryExpression(p.flags(), int32(pos), int32(p.nodePos()), store.NoListRef /*modifiers*/, result, store.NoNodeRef /*typeNode*/, operatorToken, invalidElement)
	}
	return result
}

func (p *Parser) parseJsxChildren(openingTag store.NodeRef) store.ListRef {
	pos := p.nodePos()
	saveParsingContexts := p.parsingContexts
	p.parsingContexts |= 1 << PCJsxChildren
	mark := len(p.elems)
	for {
		currentToken := p.scanner.ReScanJsxToken(true /*allowMultilineJsxText*/)
		child := p.parseJsxChild(openingTag, currentToken)
		if child == store.NoNodeRef {
			break
		}
		p.elems = append(p.elems, child)
		if p.b.View(openingTag).Kind() == ast.KindJsxOpeningElement && p.b.View(child).Kind() == ast.KindJsxElement &&
			!tagNamesAreEquivalent(p.b.View(child).AsJsxElement().OpeningElement().TagName(), p.b.View(child).AsJsxElement().ClosingElement().TagName()) &&
			tagNamesAreEquivalent(p.b.View(openingTag).TagName(), p.b.View(child).AsJsxElement().ClosingElement().TagName()) {
			// stop after parsing a mismatched child like <div>...(<span></div>) in order to reattach the </div> higher
			break
		}
	}
	p.parsingContexts = saveParsingContexts
	at := p.b.List(int32(pos), int32(p.nodePos()), p.elems[mark:])
	p.elems = p.elems[:mark]
	return at
}

func (p *Parser) parseJsxChild(openingTag store.NodeRef, token ast.Kind) store.NodeRef {
	switch token {
	case ast.KindEndOfFile:
		// If we hit EOF, issue the error at the tag that lacks the closing element
		// rather than at the end of the file (which is useless)
		if p.b.View(openingTag).Kind() == ast.KindJsxOpeningFragment {
			p.parseErrorAtRange(p.loc(openingTag), diagnostics.JSX_fragment_has_no_corresponding_closing_tag)
		} else {
			// We want the error span to cover only 'Foo.Bar' in < Foo.Bar >
			// or to cover only 'Foo' in < Foo >
			tag := p.b.View(openingTag).TagName()
			start := min(scanner.SkipTrivia(p.sourceText, int(tag.Pos())), int(tag.End()))
			p.parseErrorAt(start, int(tag.End()), diagnostics.JSX_element_0_has_no_corresponding_closing_tag,
				getTextOfNodeFromSourceText(p.sourceText, tag, false /*includeTrivia*/))
		}
		return store.NoNodeRef
	case ast.KindLessThanSlashToken, ast.KindConflictMarkerTrivia:
		return store.NoNodeRef
	case ast.KindJsxText, ast.KindJsxTextAllWhiteSpaces:
		return p.parseJsxText()
	case ast.KindOpenBraceToken:
		return p.parseJsxExpression(false /*inExpressionContext*/)
	case ast.KindLessThanToken:
		return p.parseJsxElementOrSelfClosingElementOrFragment(false /*inExpressionContext*/, -1 /*topInvalidNodePosition*/, openingTag, false)
	}
	panic("Unhandled case in parseJsxChild")
}

func (p *Parser) parseJsxText() store.NodeRef {
	pos := p.nodePos()
	text, containsOnlyTriviaWhiteSpaces := p.scanner.TokenValue(), p.token == ast.KindJsxTextAllWhiteSpaces
	p.scanJsxText()
	return p.b.NewJsxText(p.flags(), int32(pos), int32(p.nodePos()), text, containsOnlyTriviaWhiteSpaces)
}

func (p *Parser) parseJsxExpression(inExpressionContext bool) store.NodeRef {
	pos := p.nodePos()
	if !p.parseExpected(ast.KindOpenBraceToken) {
		return store.NoNodeRef
	}
	var dotDotDotToken store.NodeRef
	var expression store.NodeRef
	if p.token != ast.KindCloseBraceToken {
		if !inExpressionContext {
			dotDotDotToken = p.parseOptionalToken(ast.KindDotDotDotToken)
		}
		// Only an AssignmentExpression is valid here per the JSX spec,
		// but we can unambiguously parse a comma sequence and provide
		// a better error message in grammar checking.
		expression = p.parseExpression()
	}
	if inExpressionContext {
		p.parseExpected(ast.KindCloseBraceToken)
	} else if p.parseExpectedWithoutAdvancing(ast.KindCloseBraceToken) {
		p.scanJsxText()
	}
	return p.b.NewJsxExpression(p.flags(), int32(pos), int32(p.nodePos()), dotDotDotToken, expression)
}

func (p *Parser) scanJsxText() ast.Kind {
	p.token = p.scanner.ScanJsxToken()
	return p.token
}

func (p *Parser) scanJsxIdentifier() ast.Kind {
	p.token = p.scanner.ScanJsxIdentifier()
	return p.token
}

func (p *Parser) scanJsxAttributeValue() ast.Kind {
	p.token = p.scanner.ScanJsxAttributeValue()
	return p.token
}

func (p *Parser) parseJsxClosingElement(open store.NodeRef, inExpressionContext bool) store.NodeRef {
	pos := p.nodePos()
	p.parseExpected(ast.KindLessThanSlashToken)
	tagName := p.parseJsxElementName()
	if p.parseExpectedWithDiagnostic(ast.KindGreaterThanToken, nil /*diagnosticMessage*/, false /*shouldAdvance*/) {
		// manually advance the scanner in order to look for jsx text inside jsx
		if inExpressionContext || !tagNamesAreEquivalent(p.b.View(open).TagName(), p.b.View(tagName)) {
			p.nextToken()
		} else {
			p.scanJsxText()
		}
	}
	return p.b.NewJsxClosingElement(p.flags(), int32(pos), int32(p.nodePos()), tagName)
}

func (p *Parser) parseJsxOpeningOrSelfClosingElementOrOpeningFragment(inExpressionContext bool) store.NodeRef {
	pos := p.nodePos()
	p.parseExpected(ast.KindLessThanToken)
	if p.token == ast.KindGreaterThanToken {
		// See below for explanation of scanJsxText
		p.scanJsxText()
		return p.b.NewToken(ast.KindJsxOpeningFragment, p.flags(), int32(pos), int32(p.nodePos()))
	}
	tagName := p.parseJsxElementName()
	var typeArguments store.ListRef
	if p.contextFlags&ast.NodeFlagsJavaScriptFile == 0 {
		typeArguments = p.parseTypeArguments()
	}
	attributes := p.parseJsxAttributes()
	var result store.NodeRef
	if p.token == ast.KindGreaterThanToken {
		// Closing tag, so scan the immediately-following text with the JSX scanning instead
		// of regular scanning to avoid treating illegal characters (e.g. '#') as immediate
		// scanning errors
		p.scanJsxText()
		result = p.b.NewJsxOpeningElement(p.flags(), int32(pos), int32(p.nodePos()), tagName, typeArguments, attributes)
	} else {
		p.parseExpected(ast.KindSlashToken)
		if p.parseExpectedWithoutAdvancing(ast.KindGreaterThanToken) {
			if inExpressionContext {
				p.nextToken()
			} else {
				p.scanJsxText()
			}
		}
		result = p.b.NewJsxSelfClosingElement(p.flags(), int32(pos), int32(p.nodePos()), tagName, typeArguments, attributes)
	}
	return result
}

func (p *Parser) parseJsxElementName() store.NodeRef {
	pos := p.nodePos()
	// JsxElement can have name in the form of
	//      propertyAccessExpression
	//      primaryExpression in the form of an identifier and "this" keyword
	// We can't just simply use parseLeftHandSideExpressionOrHigher because then we will start consider class,function etc as a keyword
	// We only want to consider "this" as a primaryExpression
	initialExpression := p.parseJsxTagName()
	if p.b.View(initialExpression).Kind() == ast.KindJsxNamespacedName {
		return initialExpression // `a:b.c` is invalid syntax, don't even look for the `.` if we parse `a:b`, and let `parseAttribute` report "unexpected :" instead.
	}
	expression := initialExpression
	for p.parseOptional(ast.KindDotToken) {
		name := p.parseRightSideOfDot(true /*allowIdentifierNames*/, false /*allowPrivateIdentifiers*/, false /*allowUnicodeEscapeSequenceInIdentifierName*/)
		expression = p.b.NewPropertyAccessExpression(p.flags(), int32(pos), int32(p.nodePos()), expression, store.NoNodeRef, name)
	}
	return expression
}

func (p *Parser) parseJsxTagName() store.NodeRef {
	pos := p.nodePos()
	p.scanJsxIdentifier()
	isThis := p.token == ast.KindThisKeyword
	tagName := p.parseIdentifierNameErrorOnUnicodeEscapeSequence()
	if p.parseOptional(ast.KindColonToken) {
		p.scanJsxIdentifier()
		name := p.parseIdentifierNameErrorOnUnicodeEscapeSequence()
		return p.b.NewJsxNamespacedName(p.flags(), int32(pos), int32(p.nodePos()), tagName, name)
	}
	if isThis {
		return p.b.NewToken(ast.KindThisKeyword, p.flags(), int32(pos), int32(p.nodePos()))
	}
	return tagName
}

func (p *Parser) parseJsxAttributes() store.NodeRef {
	pos := p.nodePos()
	properties := p.parseList(PCJsxAttributes, (*Parser).parseJsxAttribute)
	return p.b.NewJsxAttributes(p.flags(), int32(pos), int32(p.nodePos()), properties)
}

func (p *Parser) parseJsxAttribute() store.NodeRef {
	if p.token == ast.KindOpenBraceToken {
		return p.parseJsxSpreadAttribute()
	}
	pos := p.nodePos()
	name := p.parseJsxAttributeName()
	initializer := p.parseJsxAttributeValue()
	return p.b.NewJsxAttribute(p.flags(), int32(pos), int32(p.nodePos()), name, initializer)
}

func (p *Parser) parseJsxSpreadAttribute() store.NodeRef {
	pos := p.nodePos()
	p.parseExpected(ast.KindOpenBraceToken)
	p.parseExpected(ast.KindDotDotDotToken)
	expression := p.parseExpression()
	p.parseExpected(ast.KindCloseBraceToken)
	return p.b.NewJsxSpreadAttribute(p.flags(), int32(pos), int32(p.nodePos()), expression)
}

func (p *Parser) parseJsxAttributeName() store.NodeRef {
	pos := p.nodePos()
	p.scanJsxIdentifier()
	attrName := p.parseIdentifierNameErrorOnUnicodeEscapeSequence()
	if p.parseOptional(ast.KindColonToken) {
		p.scanJsxIdentifier()
		name := p.parseIdentifierNameErrorOnUnicodeEscapeSequence()
		return p.b.NewJsxNamespacedName(p.flags(), int32(pos), int32(p.nodePos()), attrName, name)
	}
	return attrName
}

func (p *Parser) parseJsxAttributeValue() store.NodeRef {
	if p.token == ast.KindEqualsToken {
		if p.scanJsxAttributeValue() == ast.KindStringLiteral {
			return p.parseLiteralExpression()
		}
		if p.token == ast.KindOpenBraceToken {
			return p.parseJsxExpression( /*inExpressionContext*/ true)
		}
		if p.token == ast.KindLessThanToken {
			return p.parseJsxElementOrSelfClosingElementOrFragment(true /*inExpressionContext*/, -1, store.NoNodeRef, false)
		}
		p.parseErrorAtCurrentToken(diagnostics.X_or_JSX_element_expected)
	}
	return store.NoNodeRef
}

func (p *Parser) parseJsxClosingFragment(inExpressionContext bool) store.NodeRef {
	pos := p.nodePos()
	p.parseExpected(ast.KindLessThanSlashToken)
	if p.parseExpectedWithDiagnostic(ast.KindGreaterThanToken, diagnostics.Expected_corresponding_closing_tag_for_JSX_fragment, false /*shouldAdvance*/) {
		// manually advance the scanner in order to look for jsx text inside jsx
		if inExpressionContext {
			p.nextToken()
		} else {
			p.scanJsxText()
		}
	}
	return p.b.NewToken(ast.KindJsxClosingFragment, p.flags(), int32(pos), int32(p.nodePos()))
}

func (p *Parser) parseSimpleUnaryExpression() store.NodeRef {
	switch p.token {
	case ast.KindPlusToken, ast.KindMinusToken, ast.KindTildeToken, ast.KindExclamationToken:
		return p.parsePrefixUnaryExpression()
	case ast.KindDeleteKeyword:
		return p.parseDeleteExpression()
	case ast.KindTypeOfKeyword:
		return p.parseTypeOfExpression()
	case ast.KindVoidKeyword:
		return p.parseVoidExpression()
	case ast.KindLessThanToken:
		// Just like in parseUpdateExpression, we need to avoid parsing type assertions when
		// in JSX and we see an expression like "+ <foo> bar".
		if p.languageVariant == core.LanguageVariantJSX {
			return p.parseJsxElementOrSelfClosingElementOrFragment(true /*inExpressionContext*/, -1 /*topInvalidNodePosition*/, store.NoNodeRef /*openingTag*/, true /*mustBeUnary*/)
		}
		// // This is modified UnaryExpression grammar in TypeScript
		// //  UnaryExpression (modified):
		// //      < type > UnaryExpression
		return p.parseTypeAssertion()
	case ast.KindAwaitKeyword:
		if p.isAwaitExpression() {
			return p.parseAwaitExpression()
		}
		fallthrough
	default:
		return p.parseUpdateExpression()
	}
}

func (p *Parser) parsePrefixUnaryExpression() store.NodeRef {
	pos := p.nodePos()
	operator := p.token
	p.nextToken()
	operand := p.parseSimpleUnaryExpression()
	return p.b.NewPrefixUnaryExpression(p.flags(), int32(pos), int32(p.nodePos()), operator, operand)
}

func (p *Parser) parseDeleteExpression() store.NodeRef {
	pos := p.nodePos()
	p.nextToken()
	expression := p.parseSimpleUnaryExpression()
	return p.b.NewDeleteExpression(p.flags(), int32(pos), int32(p.nodePos()), expression)
}

func (p *Parser) parseTypeOfExpression() store.NodeRef {
	pos := p.nodePos()
	p.nextToken()
	expression := p.parseSimpleUnaryExpression()
	return p.b.NewTypeOfExpression(p.flags(), int32(pos), int32(p.nodePos()), expression)
}

func (p *Parser) parseVoidExpression() store.NodeRef {
	pos := p.nodePos()
	p.nextToken()
	expression := p.parseSimpleUnaryExpression()
	return p.b.NewVoidExpression(p.flags(), int32(pos), int32(p.nodePos()), expression)
}

func (p *Parser) isAwaitExpression() bool {
	if p.token == ast.KindAwaitKeyword {
		if p.inAwaitContext() {
			return true
		}
		// here we are using similar heuristics as 'isYieldExpression'
		return p.lookAhead((*Parser).nextTokenIsIdentifierOrKeywordOrLiteralOnSameLine)
	}
	return false
}

func (p *Parser) parseAwaitExpression() store.NodeRef {
	pos := p.nodePos()
	p.nextToken()
	expression := p.parseSimpleUnaryExpression()
	return p.b.NewAwaitExpression(p.flags(), int32(pos), int32(p.nodePos()), expression)
}

func (p *Parser) parseTypeAssertion() store.NodeRef {
	debug.Assert(p.languageVariant != core.LanguageVariantJSX, "Type assertions should never be parsed in JSX; they should be parsed as comparisons or JSX elements/fragments.")
	pos := p.nodePos()
	p.parseExpected(ast.KindLessThanToken)
	typeNode := p.parseType()
	p.parseExpected(ast.KindGreaterThanToken)
	expression := p.parseSimpleUnaryExpression()
	return p.b.NewTypeAssertion(p.flags(), int32(pos), int32(p.nodePos()), typeNode, expression)
}

func (p *Parser) parseLeftHandSideExpressionOrHigher() store.NodeRef {
	// Original Ecma:
	// LeftHandSideExpression: See 11.2
	//      NewExpression
	//      CallExpression
	//
	// Our simplification:
	//
	// LeftHandSideExpression: See 11.2
	//      MemberExpression
	//      CallExpression
	//
	// See comment in parseMemberExpressionOrHigher on how we replaced NewExpression with
	// MemberExpression to make our lives easier.
	//
	// to best understand the below code, it's important to see how CallExpression expands
	// out into its own productions:
	//
	// CallExpression:
	//      MemberExpression Arguments
	//      CallExpression Arguments
	//      CallExpression[Expression]
	//      CallExpression.IdentifierName
	//      import (AssignmentExpression)
	//      super Arguments
	//      super.IdentifierName
	//
	// Because of the recursion in these calls, we need to bottom out first. There are three
	// bottom out states we can run into: 1) We see 'super' which must start either of
	// the last two CallExpression productions. 2) We see 'import' which must start import call.
	// 3)we have a MemberExpression which either completes the LeftHandSideExpression,
	// or starts the beginning of the first four CallExpression productions.
	pos := p.nodePos()
	var expression store.NodeRef
	if p.token == ast.KindImportKeyword {
		if p.lookAhead((*Parser).nextTokenIsOpenParenOrLessThan) {
			// We don't want to eagerly consume all import keyword as import call expression so we look ahead to find "("
			// For example:
			//      var foo3 = require("subfolder
			//      import * as foo1 from "module-from-node
			// We want this import to be a statement rather than import call expression
			p.sourceFlags |= ast.NodeFlagsPossiblyContainsDynamicImport
			expression = p.parseKeywordExpression()
		} else if p.lookAhead((*Parser).nextTokenIsDot) {
			// This is an 'import.*' metaproperty (i.e. 'import.meta')
			p.nextToken() // advance past the 'import'
			p.nextToken() // advance past the dot
			name := p.parseIdentifierName()
			expression = p.b.NewMetaProperty(p.flags(), int32(pos), int32(p.nodePos()), ast.KindImportKeyword, name)
			if p.b.View(expression).AsMetaProperty().Name().Text() == "defer" {
				if p.token == ast.KindOpenParenToken || p.token == ast.KindLessThanToken {
					p.sourceFlags |= ast.NodeFlagsPossiblyContainsDynamicImport
				}
			} else {
				p.sourceFlags |= ast.NodeFlagsPossiblyContainsImportMeta
			}
		} else {
			expression = p.parseMemberExpressionOrHigher()
		}
	} else if p.token == ast.KindSuperKeyword {
		expression = p.parseSuperExpression()
	} else {
		expression = p.parseMemberExpressionOrHigher()
	}
	// Now, we *may* be complete.  However, we might have consumed the start of a
	// CallExpression or OptionalExpression.  As such, we need to consume the rest
	// of it here to be complete.
	return p.parseCallExpressionRest(pos, expression)
}

func (p *Parser) nextTokenIsDot() bool {
	return p.nextToken() == ast.KindDotToken
}

func (p *Parser) parseSuperExpression() store.NodeRef {
	pos := p.nodePos()
	expression := p.parseKeywordExpression()
	if p.token == ast.KindLessThanToken {
		startPos := p.nodePos()
		typeArguments := p.tryParseTypeArgumentsInExpression()
		if typeArguments != store.NoListRef {
			p.parseErrorAt(startPos, p.nodePos(), diagnostics.X_super_may_not_use_type_arguments)
			if !p.isTemplateStartOfTaggedTemplate() {
				expression = p.b.NewExpressionWithTypeArguments(p.flags(), int32(pos), int32(p.nodePos()), expression, typeArguments)
			}
		}
	}
	if p.token == ast.KindOpenParenToken || p.token == ast.KindDotToken || p.token == ast.KindOpenBracketToken {
		return expression
	}
	// If we have seen "super" it must be followed by '(' or '.'.
	// If it wasn't then just try to parse out a '.' and report an error.
	p.parseErrorAtCurrentToken(diagnostics.X_super_must_be_followed_by_an_argument_list_or_member_access)
	// private names will never work with `super` (`super.#foo`), but that's a semantic error, not syntactic
	name := p.parseRightSideOfDot(true /*allowIdentifierNames*/, true /*allowPrivateIdentifiers*/, true /*allowUnicodeEscapeSequenceInIdentifierName*/)
	return p.b.NewPropertyAccessExpression(p.flags(), int32(pos), int32(p.nodePos()), expression, store.NoNodeRef /*questionDotToken*/, name)
}

func (p *Parser) isTemplateStartOfTaggedTemplate() bool {
	return p.token == ast.KindNoSubstitutionTemplateLiteral || p.token == ast.KindTemplateHead
}

func (p *Parser) tryParseTypeArgumentsInExpression() store.ListRef {
	// TypeArguments must not be parsed in JavaScript files to avoid ambiguity with binary operators.
	// Check the cheap preconditions before saving the parser state: unless the current token is `<`
	// (or `<<`, which reScanLessThanToken would split), there is nothing to speculatively parse and
	// the mark/rewind would be a no-op.
	if p.contextFlags&ast.NodeFlagsJavaScriptFile != 0 || (p.token != ast.KindLessThanToken && p.token != ast.KindLessThanLessThanToken) {
		return store.NoListRef
	}
	state := p.mark()
	if p.reScanLessThanToken() == ast.KindLessThanToken {
		p.nextToken()
		typeArguments := p.parseDelimitedList(PCTypeArguments, (*Parser).parseType)
		// If it doesn't have the closing `>` then it's definitely not an type argument list.
		if p.reScanGreaterThanToken() == ast.KindGreaterThanToken {
			p.nextToken()
			// We successfully parsed a type argument list. The next token determines whether we want to
			// treat it as such. If the type argument list is followed by `(` or a template literal, as in
			// `f<number>(42)`, we favor the type argument interpretation even though JavaScript would view
			// it as a relational expression.
			if p.canFollowTypeArgumentsInExpression() {
				return typeArguments
			}
		}
	}
	p.rewind(state)
	return store.NoListRef
}

func (p *Parser) canFollowTypeArgumentsInExpression() bool {
	switch p.token {
	// These tokens can follow a type argument list in a call expression:
	// foo<x>(
	// foo<T> `...`
	// foo<T> `...${100}...`
	case ast.KindOpenParenToken, ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateHead:
		return true
	// A type argument list followed by `<` never makes sense, and a type argument list followed
	// by `>` is ambiguous with a (re-scanned) `>>` operator, so we disqualify both. Also, in
	// this context, `+` and `-` are unary operators, not binary operators.
	case ast.KindLessThanToken, ast.KindGreaterThanToken, ast.KindPlusToken, ast.KindMinusToken:
		return false
	}
	// We favor the type argument list interpretation when it is immediately followed by
	// a line break, a binary operator, or something that can't start an expression.
	return p.hasPrecedingLineBreak() || p.isBinaryOperator() || !p.isStartOfExpression()
}

func (p *Parser) parseMemberExpressionOrHigher() store.NodeRef {
	// Note: to make our lives simpler, we decompose the NewExpression productions and
	// place ObjectCreationExpression and FunctionExpression into PrimaryExpression.
	// like so:
	//
	//   PrimaryExpression : See 11.1
	//      this
	//      Identifier
	//      Literal
	//      ArrayLiteral
	//      ObjectLiteral
	//      (Expression)
	//      FunctionExpression
	//      new MemberExpression Arguments?
	//
	//   MemberExpression : See 11.2
	//      PrimaryExpression
	//      MemberExpression[Expression]
	//      MemberExpression.IdentifierName
	//
	//   CallExpression : See 11.2
	//      MemberExpression
	//      CallExpression Arguments
	//      CallExpression[Expression]
	//      CallExpression.IdentifierName
	//
	// Technically this is ambiguous.  i.e. CallExpression defines:
	//
	//   CallExpression:
	//      CallExpression Arguments
	//
	// If you see: "new Foo()"
	//
	// Then that could be treated as a single ObjectCreationExpression, or it could be
	// treated as the invocation of "new Foo".  We disambiguate that in code (to match
	// the original grammar) by making sure that if we see an ObjectCreationExpression
	// we always consume arguments if they are there. So we treat "new Foo()" as an
	// object creation only, and not at all as an invocation.  Another way to think
	// about this is that for every "new" that we see, we will consume an argument list if
	// it is there as part of the *associated* object creation node.  Any additional
	// argument lists we see, will become invocation expressions.
	//
	// Because there are no other places in the grammar now that refer to FunctionExpression
	// or ObjectCreationExpression, it is safe to push down into the PrimaryExpression
	// production.
	//
	// Because CallExpression and MemberExpression are left recursive, we need to bottom out
	// of the recursion immediately.  So we parse out a primary expression to start with.
	pos := p.nodePos()
	expression := p.parsePrimaryExpression()
	return p.parseMemberExpressionRest(pos, expression, true /*allowOptionalChain*/)
}

func (p *Parser) parseMemberExpressionRest(pos int, expression store.NodeRef, allowOptionalChain bool) store.NodeRef {
	for {
		var questionDotToken store.NodeRef
		isPropertyAccess := false
		if allowOptionalChain && p.isStartOfOptionalPropertyOrElementAccessChain() {
			questionDotToken = p.parseExpectedToken(ast.KindQuestionDotToken)
			isPropertyAccess = tokenIsIdentifierOrKeyword(p.token)
		} else {
			isPropertyAccess = p.parseOptional(ast.KindDotToken)
		}
		if isPropertyAccess {
			expression = p.parsePropertyAccessExpressionRest(pos, expression, questionDotToken)
			continue
		}
		// when in the [Decorator] context, we do not parse ElementAccess as it could be part of a ComputedPropertyName
		if (questionDotToken != store.NoNodeRef || !p.inDecoratorContext()) && p.parseOptional(ast.KindOpenBracketToken) {
			expression = p.parseElementAccessExpressionRest(pos, expression, questionDotToken)
			continue
		}
		if p.isTemplateStartOfTaggedTemplate() {
			// Absorb type arguments into TemplateExpression when preceding expression is ExpressionWithTypeArguments
			if questionDotToken == store.NoNodeRef && p.b.View(expression).Kind() == ast.KindExpressionWithTypeArguments {
				original := p.b.View(expression).AsExpressionWithTypeArguments()
				originalExpression, originalTypeArguments := original.Expression().Ref(), original.TypeArguments().Ref()
				// The constructor reparents the expression and the type arguments; the
				// ExpressionWithTypeArguments node stays as a dead node.
				expression = p.parseTaggedTemplateRest(pos, originalExpression, questionDotToken, originalTypeArguments)
			} else {
				expression = p.parseTaggedTemplateRest(pos, expression, questionDotToken, store.NoListRef /*typeArguments*/)
			}
			continue
		}
		if questionDotToken == store.NoNodeRef {
			if p.token == ast.KindExclamationToken && !p.hasPrecedingLineBreak() {
				p.nextToken()
				expression = p.checkJSSyntax(p.b.NewNonNullExpression(p.flags(), int32(pos), int32(p.nodePos()), expression))
				continue
			}
			typeArguments := p.tryParseTypeArgumentsInExpression()
			if typeArguments != store.NoListRef {
				expression = p.b.NewExpressionWithTypeArguments(p.flags(), int32(pos), int32(p.nodePos()), expression, typeArguments)
				continue
			}
		}
		return expression
	}
}

func (p *Parser) isStartOfOptionalPropertyOrElementAccessChain() bool {
	return p.token == ast.KindQuestionDotToken && p.lookAhead((*Parser).nextTokenIsIdentifierOrKeywordOrOpenBracketOrTemplate)
}

func (p *Parser) nextTokenIsIdentifierOrKeywordOrOpenBracketOrTemplate() bool {
	p.nextToken()
	return tokenIsIdentifierOrKeyword(p.token) || p.token == ast.KindOpenBracketToken || p.isTemplateStartOfTaggedTemplate()
}

func (p *Parser) parsePropertyAccessExpressionRest(pos int, expression store.NodeRef, questionDotToken store.NodeRef) store.NodeRef {
	name := p.parseRightSideOfDot(true /*allowIdentifierNames*/, true /*allowPrivateIdentifiers*/, true /*allowUnicodeEscapeSequenceInIdentifierName*/)
	isOptionalChain := questionDotToken != store.NoNodeRef || p.tryReparseOptionalChain(expression)
	if isOptionalChain && p.b.View(name).Kind() == ast.KindPrivateIdentifier {
		p.parseErrorAtRange(p.skipRangeTrivia(p.loc(name)), diagnostics.An_optional_chain_cannot_contain_private_identifiers)
	}
	if p.b.View(expression).Kind() == ast.KindExpressionWithTypeArguments {
		typeArguments := p.b.View(expression).TypeArguments()
		if !typeArguments.IsNil() {
			loc := core.NewTextRange(int(typeArguments.Pos())-1, scanner.SkipTrivia(p.sourceText, int(typeArguments.End()))+1)
			p.parseErrorAtRange(loc, diagnostics.An_instantiation_expression_cannot_be_followed_by_a_property_access)
		}
	}
	// The Pointer parser finishes the node after the errors above, so it is made after them here.
	return p.b.NewPropertyAccessExpression(p.flags()|core.IfElse(isOptionalChain, ast.NodeFlagsOptionalChain, ast.NodeFlagsNone), int32(pos), int32(p.nodePos()), expression, questionDotToken, name)
}

func (p *Parser) tryReparseOptionalChain(node store.NodeRef) bool {
	if p.b.View(node).Flags()&ast.NodeFlagsOptionalChain != 0 {
		return true
	}
	// check for an optional chain in a non-null expression
	if p.b.View(node).Kind() == ast.KindNonNullExpression {
		expr := p.b.View(node).Expression()
		for expr.Kind() == ast.KindNonNullExpression && expr.Flags()&ast.NodeFlagsOptionalChain == 0 {
			expr = expr.Expression()
		}
		if expr.Flags()&ast.NodeFlagsOptionalChain != 0 {
			// this is part of an optional chain. Walk down from `node` to `expression` and set the flag.
			for p.b.View(node).Kind() == ast.KindNonNullExpression {
				p.b.AddFlags(node, ast.NodeFlagsOptionalChain)
				node = p.b.View(node).Expression().Ref()
			}
			return true
		}
	}
	return false
}

func (p *Parser) parseElementAccessExpressionRest(pos int, expression store.NodeRef, questionDotToken store.NodeRef) store.NodeRef {
	argumentExpression := p.createMissingIdentifier()
	if p.token == ast.KindCloseBracketToken {
		p.parseErrorAt(p.nodePos(), p.nodePos(), diagnostics.An_element_access_expression_should_take_an_argument)
	} else {
		argumentExpression = p.parseExpressionAllowIn()
	}
	p.parseExpected(ast.KindCloseBracketToken)
	isOptionalChain := questionDotToken != store.NoNodeRef || p.tryReparseOptionalChain(expression)
	return p.b.NewElementAccessExpression(p.flags()|core.IfElse(isOptionalChain, ast.NodeFlagsOptionalChain, ast.NodeFlagsNone), int32(pos), int32(p.nodePos()), expression, questionDotToken, argumentExpression)
}

func (p *Parser) parseCallExpressionRest(pos int, expression store.NodeRef) store.NodeRef {
	for {
		expression = p.parseMemberExpressionRest(pos, expression /*allowOptionalChain*/, true)
		var typeArguments store.ListRef
		questionDotToken := p.parseOptionalToken(ast.KindQuestionDotToken)
		if questionDotToken != store.NoNodeRef {
			typeArguments = p.tryParseTypeArgumentsInExpression()
			if p.isTemplateStartOfTaggedTemplate() {
				expression = p.parseTaggedTemplateRest(pos, expression, questionDotToken, typeArguments)
				continue
			}
		}
		if typeArguments != store.NoListRef || p.token == ast.KindOpenParenToken {
			// Absorb type arguments into CallExpression when preceding expression is ExpressionWithTypeArguments
			if questionDotToken == store.NoNodeRef && p.b.View(expression).Kind() == ast.KindExpressionWithTypeArguments {
				// The constructor reparents the expression and the type arguments; the
				// ExpressionWithTypeArguments node stays as a dead node.
				typeArguments = p.b.View(expression).TypeArguments().Ref()
				expression = p.b.View(expression).AsExpressionWithTypeArguments().Expression().Ref()
			}
			argumentList := p.parseArgumentList()
			isOptionalChain := questionDotToken != store.NoNodeRef || p.tryReparseOptionalChain(expression)
			expression = p.checkJSSyntax(p.b.NewCallExpression(p.flags()|core.IfElse(isOptionalChain, ast.NodeFlagsOptionalChain, ast.NodeFlagsNone), int32(pos), int32(p.nodePos()), expression, questionDotToken, typeArguments, argumentList))
			continue
		}
		if questionDotToken != store.NoNodeRef {
			// We parsed `?.` but then failed to parse anything, so report a missing identifier here.
			p.parseErrorAtCurrentToken(diagnostics.Identifier_expected)
			name := p.createMissingIdentifier()
			expression = p.b.NewPropertyAccessExpression(p.flags()|ast.NodeFlagsOptionalChain, int32(pos), int32(p.nodePos()), expression, questionDotToken, name)
		}
		break
	}
	return expression
}

func (p *Parser) parseArgumentList() store.ListRef {
	p.parseExpected(ast.KindOpenParenToken)
	result := p.parseDelimitedList(PCArgumentExpressions, (*Parser).parseArgumentExpression)
	p.parseExpected(ast.KindCloseParenToken)
	return result
}

func (p *Parser) parseArgumentExpression() store.NodeRef {
	return p.doInContext(ast.NodeFlagsDisallowInContext|ast.NodeFlagsDecoratorContext, false, (*Parser).parseArgumentOrArrayLiteralElement)
}

func (p *Parser) parseArgumentOrArrayLiteralElement() store.NodeRef {
	switch p.token {
	case ast.KindDotDotDotToken:
		return p.parseSpreadElement()
	case ast.KindCommaToken:
		return p.b.NewToken(ast.KindOmittedExpression, p.flags(), int32(p.nodePos()), int32(p.nodePos()))
	}
	return p.parseAssignmentExpressionOrHigher()
}

func (p *Parser) parseSpreadElement() store.NodeRef {
	pos := p.nodePos()
	p.parseExpected(ast.KindDotDotDotToken)
	expression := p.parseAssignmentExpressionOrHigher()
	return p.b.NewSpreadElement(p.flags(), int32(pos), int32(p.nodePos()), expression)
}

func (p *Parser) parseTaggedTemplateRest(pos int, tag store.NodeRef, questionDotToken store.NodeRef, typeArguments store.ListRef) store.NodeRef {
	var template store.NodeRef
	if p.token == ast.KindNoSubstitutionTemplateLiteral {
		p.reScanTemplateToken(true /*isTaggedTemplate*/)
		template = p.parseLiteralExpression()
	} else {
		template = p.parseTemplateExpression(true /*isTaggedTemplate*/)
	}
	isOptionalChain := questionDotToken != store.NoNodeRef || p.b.View(tag).Flags()&ast.NodeFlagsOptionalChain != 0
	return p.checkJSSyntax(p.b.NewTaggedTemplateExpression(p.flags()|core.IfElse(isOptionalChain, ast.NodeFlagsOptionalChain, ast.NodeFlagsNone), int32(pos), int32(p.nodePos()), tag, questionDotToken, typeArguments, template))
}

func (p *Parser) parseTemplateExpression(isTaggedTemplate bool) store.NodeRef {
	pos := p.nodePos()
	head := p.parseTemplateHead(isTaggedTemplate)
	templateSpans := p.parseTemplateSpans(isTaggedTemplate)
	return p.b.NewTemplateExpression(p.flags(), int32(pos), int32(p.nodePos()), head, templateSpans)
}

func (p *Parser) parseTemplateSpans(isTaggedTemplate bool) store.ListRef {
	pos := p.nodePos()
	mark := len(p.elems)
	for {
		span := p.parseTemplateSpan(isTaggedTemplate)
		p.elems = append(p.elems, span)
		if p.b.View(span).AsTemplateSpan().Literal().Kind() != ast.KindTemplateMiddle {
			break
		}
	}
	at := p.b.List(int32(pos), int32(p.nodePos()), p.elems[mark:])
	p.elems = p.elems[:mark]
	return at
}

func (p *Parser) parseTemplateSpan(isTaggedTemplate bool) store.NodeRef {
	pos := p.nodePos()
	expression := p.parseExpressionAllowIn()
	literal := p.parseLiteralOfTemplateSpan(isTaggedTemplate)
	return p.b.NewTemplateSpan(p.flags(), int32(pos), int32(p.nodePos()), expression, literal)
}

func (p *Parser) parsePrimaryExpression() store.NodeRef {
	switch p.token {
	case ast.KindNoSubstitutionTemplateLiteral:
		if p.scanner.TokenFlags()&ast.TokenFlagsIsInvalid != 0 {
			p.reScanTemplateToken(false /*isTaggedTemplate*/)
		}
		fallthrough
	case ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindStringLiteral:
		return p.parseLiteralExpression()
	case ast.KindThisKeyword, ast.KindSuperKeyword, ast.KindNullKeyword, ast.KindTrueKeyword, ast.KindFalseKeyword:
		return p.parseKeywordExpression()
	case ast.KindOpenParenToken:
		return p.parseParenthesizedExpression()
	case ast.KindOpenBracketToken:
		return p.parseArrayLiteralExpression()
	case ast.KindOpenBraceToken:
		return p.parseObjectLiteralExpression()
	case ast.KindAsyncKeyword:
		// Async arrow functions are parsed earlier in parseAssignmentExpressionOrHigher.
		// If we encounter `async [no LineTerminator here] function` then this is an async
		// function; otherwise, its an identifier.
		if !p.lookAhead((*Parser).nextTokenIsFunctionKeywordOnSameLine) {
			break
		}
		return p.parseFunctionExpression()
	case ast.KindAtToken:
		return p.parseDecoratedExpression()
	case ast.KindClassKeyword:
		return p.parseClassExpression()
	case ast.KindFunctionKeyword:
		return p.parseFunctionExpression()
	case ast.KindNewKeyword:
		return p.parseNewExpressionOrNewDotTarget()
	case ast.KindSlashToken, ast.KindSlashEqualsToken:
		if p.reScanSlashToken() == ast.KindRegularExpressionLiteral {
			return p.parseLiteralExpression()
		}
	case ast.KindTemplateHead:
		return p.parseTemplateExpression(false /*isTaggedTemplate*/)
	case ast.KindPrivateIdentifier:
		return p.parsePrivateIdentifier()
	}
	return p.parseIdentifierWithDiagnostic(diagnostics.Expression_expected, nil)
}

func (p *Parser) parseParenthesizedExpression() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindOpenParenToken)
	expression := p.parseExpressionAllowIn()
	p.parseExpected(ast.KindCloseParenToken)
	result := p.b.NewParenthesizedExpression(p.flags(), int32(pos), int32(p.nodePos()), expression)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	return result
}

func (p *Parser) parseArrayLiteralExpression() store.NodeRef {
	pos := p.nodePos()
	openBracketPosition := p.scanner.TokenStart()
	openBracketParsed := p.parseExpected(ast.KindOpenBracketToken)
	multiLine := p.hasPrecedingLineBreak()
	elements := p.parseDelimitedList(PCArrayLiteralMembers, (*Parser).parseArgumentOrArrayLiteralElement)
	p.parseExpectedMatchingBrackets(ast.KindOpenBracketToken, ast.KindCloseBracketToken, openBracketParsed, openBracketPosition)
	return p.b.NewArrayLiteralExpression(p.flags(), int32(pos), int32(p.nodePos()), elements, multiLine)
}

func (p *Parser) parseObjectLiteralExpression() store.NodeRef {
	pos := p.nodePos()
	openBracePosition := p.scanner.TokenStart()
	openBraceParsed := p.parseExpected(ast.KindOpenBraceToken)
	multiLine := p.hasPrecedingLineBreak()
	properties := p.parseDelimitedList(PCObjectLiteralMembers, (*Parser).parseObjectLiteralElement)
	p.parseExpectedMatchingBrackets(ast.KindOpenBraceToken, ast.KindCloseBraceToken, openBraceParsed, openBracePosition)
	return p.b.NewObjectLiteralExpression(p.flags(), int32(pos), int32(p.nodePos()), properties, multiLine)
}

func (p *Parser) parseObjectLiteralElement() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	if p.parseOptional(ast.KindDotDotDotToken) {
		expression := p.parseAssignmentExpressionOrHigher()
		result := p.b.NewSpreadAssignment(p.flags(), int32(pos), int32(p.nodePos()), expression)
		p.b.AddFlags(result, p.jsdocFlags(jsdoc))
		return result
	}
	modifiers := p.parseModifiersEx(true /*allowDecorators*/, false /*permitConstAsModifier*/, false /*stopOnStartOfClassStaticBlock*/)
	if p.parseContextualModifier(ast.KindGetKeyword) {
		return p.parseAccessorDeclaration(pos, jsdoc, modifiers, ast.KindGetAccessor, ParseFlagsNone)
	}
	if p.parseContextualModifier(ast.KindSetKeyword) {
		return p.parseAccessorDeclaration(pos, jsdoc, modifiers, ast.KindSetAccessor, ParseFlagsNone)
	}
	asteriskToken := p.parseOptionalToken(ast.KindAsteriskToken)
	tokenIsIdentifier := p.isIdentifier()
	name := p.parsePropertyName()
	// Disallowing of optional property assignments and definite assignment assertion happens in the grammar checker.
	postfixToken := p.parseOptionalToken(ast.KindQuestionToken)
	// Decorators, Modifiers, questionToken, and exclamationToken are not supported by property assignments and are reported in the grammar checker
	if postfixToken == store.NoNodeRef {
		postfixToken = p.parseOptionalToken(ast.KindExclamationToken)
	}
	if asteriskToken != store.NoNodeRef || p.token == ast.KindOpenParenToken || p.token == ast.KindLessThanToken {
		return p.parseMethodDeclaration(pos, jsdoc, modifiers, asteriskToken, name, postfixToken, nil /*diagnosticMessage*/)
	}
	// check if it is short-hand property assignment or normal property assignment
	// NOTE: if token is EqualsToken it is interpreted as CoverInitializedName production
	// CoverInitializedName[Yield] :
	//     IdentifierReference[?Yield] Initializer[In, ?Yield]
	// this is necessary because ObjectLiteral productions are also used to cover grammar for ObjectAssignmentPattern
	var node store.NodeRef
	isShorthandPropertyAssignment := tokenIsIdentifier && p.token != ast.KindColonToken
	if isShorthandPropertyAssignment {
		equalsToken := p.parseOptionalToken(ast.KindEqualsToken)
		var initializer store.NodeRef
		if equalsToken != store.NoNodeRef {
			initializer = p.doInContext(ast.NodeFlagsDisallowInContext, false, (*Parser).parseAssignmentExpressionOrHigher)
		}
		node = p.b.NewShorthandPropertyAssignment(p.flags(), int32(pos), int32(p.nodePos()), modifiers, name, postfixToken, store.NoNodeRef /*typeNode*/, equalsToken, initializer)
	} else {
		p.parseExpected(ast.KindColonToken)
		initializer := p.doInContext(ast.NodeFlagsDisallowInContext, false, (*Parser).parseAssignmentExpressionOrHigher)
		node = p.b.NewPropertyAssignment(p.flags(), int32(pos), int32(p.nodePos()), modifiers, name, postfixToken, store.NoNodeRef /*typeNode*/, initializer)
	}
	p.b.AddFlags(node, p.jsdocFlags(jsdoc))
	return node
}

func (p *Parser) parseFunctionExpression() store.NodeRef {
	// GeneratorExpression:
	//      function* BindingIdentifier [Yield][opt](FormalParameters[Yield]){ GeneratorBody }
	//
	// FunctionExpression:
	//      function BindingIdentifier[opt](FormalParameters){ FunctionBody }
	saveContexFlags := p.contextFlags
	p.setContextFlags(ast.NodeFlagsDecoratorContext, false)
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	modifiers := p.parseModifiers()
	p.parseExpected(ast.KindFunctionKeyword)
	asteriskToken := p.parseOptionalToken(ast.KindAsteriskToken)
	isGenerator := asteriskToken != store.NoNodeRef
	isAsync := p.modifierListHasAsync(modifiers)
	signatureFlags := core.IfElse(isGenerator, ParseFlagsYield, ParseFlagsNone) | core.IfElse(isAsync, ParseFlagsAwait, ParseFlagsNone)
	var name store.NodeRef
	switch {
	case isGenerator && isAsync:
		name = p.doInContext(ast.NodeFlagsYieldContext|ast.NodeFlagsAwaitContext, true, (*Parser).parseOptionalBindingIdentifier)
	case isGenerator:
		name = p.doInContext(ast.NodeFlagsYieldContext, true, (*Parser).parseOptionalBindingIdentifier)
	case isAsync:
		name = p.doInContext(ast.NodeFlagsAwaitContext, true, (*Parser).parseOptionalBindingIdentifier)
	default:
		name = p.parseOptionalBindingIdentifier()
	}
	typeParameters := p.parseTypeParameters()
	parameters := p.parseParameters(signatureFlags)
	returnType := p.parseReturnType(ast.KindColonToken, false /*isType*/)
	body := p.parseFunctionBlock(signatureFlags, nil /*diagnosticMessage*/)
	p.contextFlags = saveContexFlags
	result := p.b.NewFunctionExpression(p.flags(), int32(pos), int32(p.nodePos()), modifiers, asteriskToken, name, typeParameters, parameters, returnType, store.NoNodeRef /*fullSignature*/, body)
	p.b.AddFlags(result, p.jsdocFlags(jsdoc))
	p.checkJSSyntax(result)
	return result
}

func (p *Parser) parseOptionalBindingIdentifier() store.NodeRef {
	if p.isBindingIdentifier() {
		return p.parseBindingIdentifier()
	}
	return store.NoNodeRef
}

func (p *Parser) parseDecoratedExpression() store.NodeRef {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	modifiers := p.parseModifiersEx(true /*allowDecorators*/, false /*permitConstAsModifier*/, false /*stopOnStartOfClassStaticBlock*/)
	if p.token == ast.KindClassKeyword {
		return p.parseClassDeclarationOrExpression(pos, jsdoc, modifiers, ast.KindClassExpression)
	}
	p.parseErrorAt(p.nodePos(), p.nodePos(), diagnostics.Expression_expected)
	return p.b.NewMissingDeclaration(p.flags(), int32(pos), int32(p.nodePos()), modifiers)
}

// unparseExpressionWithTypeArguments is not needed: the constructor of the
// node that absorbs the expression and the type arguments writes their parent.

func (p *Parser) parseNewExpressionOrNewDotTarget() store.NodeRef {
	pos := p.nodePos()
	p.parseExpected(ast.KindNewKeyword)
	if p.parseOptional(ast.KindDotToken) {
		name := p.parseIdentifierName()
		return p.b.NewMetaProperty(p.flags(), int32(pos), int32(p.nodePos()), ast.KindNewKeyword, name)
	}
	expressionPos := p.nodePos()
	expression := p.parseMemberExpressionRest(expressionPos, p.parsePrimaryExpression(), false /*allowOptionalChain*/)
	var typeArguments store.ListRef
	// Absorb type arguments into NewExpression when preceding expression is ExpressionWithTypeArguments
	if p.b.View(expression).Kind() == ast.KindExpressionWithTypeArguments {
		// The constructor reparents the expression and the type arguments; the
		// ExpressionWithTypeArguments node stays as a dead node.
		typeArguments = p.b.View(expression).TypeArguments().Ref()
		expression = p.b.View(expression).AsExpressionWithTypeArguments().Expression().Ref()
	}
	if p.token == ast.KindQuestionDotToken {
		p.parseErrorAtCurrentToken(diagnostics.Invalid_optional_chain_from_new_expression_Did_you_mean_to_call_0, getTextOfNodeFromSourceText(p.sourceText, p.b.View(expression), false /*includeTrivia*/))
	}
	var argumentList store.ListRef
	if p.token == ast.KindOpenParenToken {
		argumentList = p.parseArgumentList()
	}
	result := p.checkJSSyntax(p.b.NewNewExpression(p.flags(), int32(pos), int32(p.nodePos()), expression, typeArguments, argumentList))
	return result
}

func (p *Parser) parseKeywordExpression() store.NodeRef {
	pos := p.nodePos()
	kind := p.token
	p.nextToken()
	return p.b.NewToken(kind, p.flags(), int32(pos), int32(p.nodePos()))
}

// parseLiteralExpression reads the token, advances, and only then makes the
// node, because a Store node is made finished.
func (p *Parser) parseLiteralExpression() store.NodeRef {
	pos := p.nodePos()
	text := p.scanner.TokenValue()
	tokenFlags := p.scanner.TokenFlags()
	kind := p.token
	p.nextToken()
	// The masks are the ones the Pointer constructors apply.
	var result store.NodeRef
	switch kind {
	case ast.KindStringLiteral:
		result = p.b.NewStringLiteral(p.flags(), int32(pos), int32(p.nodePos()), text, tokenFlags&ast.TokenFlagsStringLiteralFlags)
	case ast.KindNumericLiteral:
		result = p.b.NewNumericLiteral(p.flags(), int32(pos), int32(p.nodePos()), text, tokenFlags&ast.TokenFlagsNumericLiteralFlags)
	case ast.KindBigIntLiteral:
		result = p.b.NewBigIntLiteral(p.flags(), int32(pos), int32(p.nodePos()), text, tokenFlags&ast.TokenFlagsNumericLiteralFlags)
	case ast.KindRegularExpressionLiteral:
		result = p.b.NewRegularExpressionLiteral(p.flags(), int32(pos), int32(p.nodePos()), text, tokenFlags&ast.TokenFlagsRegularExpressionLiteralFlags)
	case ast.KindNoSubstitutionTemplateLiteral:
		result = p.b.NewNoSubstitutionTemplateLiteral(p.flags(), int32(pos), int32(p.nodePos()), text, tokenFlags&ast.TokenFlagsTemplateLiteralLikeFlags)
	default:
		panic("Unhandled case in parseLiteralExpression")
	}
	return result
}

func (p *Parser) parseIdentifierNameErrorOnUnicodeEscapeSequence() store.NodeRef {
	if p.scanner.HasUnicodeEscape() || p.scanner.HasExtendedUnicodeEscape() {
		p.parseErrorAtCurrentToken(diagnostics.Unicode_escape_sequence_cannot_appear_here)
	}
	return p.createIdentifier(tokenIsIdentifierOrKeyword(p.token))
}

func (p *Parser) parseBindingIdentifier() store.NodeRef {
	return p.parseBindingIdentifierWithDiagnostic(nil)
}

func (p *Parser) parseBindingIdentifierWithDiagnostic(privateIdentifierDiagnosticMessage *diagnostics.Message) store.NodeRef {
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	id := p.createIdentifierWithDiagnostic(p.isBindingIdentifier(), nil /*diagnosticMessage*/, privateIdentifierDiagnosticMessage)
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	return id
}

func (p *Parser) parseIdentifierName() store.NodeRef {
	return p.parseIdentifierNameWithDiagnostic(nil)
}

func (p *Parser) parseIdentifierNameWithDiagnostic(diagnosticMessage *diagnostics.Message) store.NodeRef {
	return p.createIdentifierWithDiagnostic(tokenIsIdentifierOrKeyword(p.token), diagnosticMessage, nil)
}

func (p *Parser) parseIdentifier() store.NodeRef {
	return p.parseIdentifierWithDiagnostic(nil, nil)
}

func (p *Parser) parseIdentifierWithDiagnostic(diagnosticMessage *diagnostics.Message, privateIdentifierDiagnosticMessage *diagnostics.Message) store.NodeRef {
	return p.createIdentifierWithDiagnostic(p.isIdentifier(), diagnosticMessage, privateIdentifierDiagnosticMessage)
}

func (p *Parser) createIdentifier(isIdentifier bool) store.NodeRef {
	return p.createIdentifierWithDiagnostic(isIdentifier, nil, nil)
}

func (p *Parser) createIdentifierWithDiagnostic(isIdentifier bool, diagnosticMessage *diagnostics.Message, privateIdentifierDiagnosticMessage *diagnostics.Message) store.NodeRef {
	if isIdentifier {
		var pos int
		if p.scanner.HasPrecedingJSDocLeadingAsterisks() {
			pos = p.scanner.TokenStart()
		} else {
			pos = p.nodePos()
		}
		text := p.scanner.TokenValue()
		p.nextTokenWithoutCheck()
		return p.newIdentifier(text, pos, p.nodePos())
	}
	if p.token == ast.KindPrivateIdentifier {
		if privateIdentifierDiagnosticMessage != nil {
			p.parseErrorAtCurrentToken(privateIdentifierDiagnosticMessage)
		} else {
			p.parseErrorAtCurrentToken(diagnostics.Private_identifiers_are_not_allowed_outside_class_bodies)
		}
		return p.createIdentifier(true /*isIdentifier*/)
	}
	// Only for end of file because the error gets reported incorrectly on embedded script tags.
	reportAtCurrentPosition := p.token == ast.KindEndOfFile
	if diagnosticMessage != nil {
		if reportAtCurrentPosition {
			pos := p.scanner.TokenFullStart()
			p.parseErrorAt(pos, pos, diagnosticMessage)
		} else {
			p.parseErrorAtCurrentToken(diagnosticMessage)
		}
	} else if isReservedWord(p.token) {
		if reportAtCurrentPosition {
			pos := p.scanner.TokenFullStart()
			p.parseErrorAt(pos, pos, diagnostics.Identifier_expected_0_is_a_reserved_word_that_cannot_be_used_here, p.scanner.TokenText())
		} else {
			p.parseErrorAtCurrentToken(diagnostics.Identifier_expected_0_is_a_reserved_word_that_cannot_be_used_here, p.scanner.TokenText())
		}
	} else {
		if reportAtCurrentPosition {
			pos := p.scanner.TokenFullStart()
			p.parseErrorAt(pos, pos, diagnostics.Identifier_expected)
		} else {
			p.parseErrorAtCurrentToken(diagnostics.Identifier_expected)
		}
	}
	return p.createMissingIdentifier()
}

// newNodeList, newModifierList, finishNode, finishNodeWithEnd and
// overrideParentInImmediateChildren have no counterpart: a list is made by
// Builder.List, a node is made finished by its constructor, and the
// constructor writes the parent of its children.

func (p *Parser) nextTokenIsSlash() bool {
	return p.nextToken() == ast.KindSlashToken
}

func (p *Parser) scanTypeMemberStart() bool {
	// Return true if we have the start of a signature member
	if p.token == ast.KindOpenParenToken || p.token == ast.KindLessThanToken || p.token == ast.KindGetKeyword || p.token == ast.KindSetKeyword {
		return true
	}
	idToken := false
	// Eat up all modifiers, but hold on to the last one in case it is actually an identifier
	for ast.IsModifierKind(p.token) {
		idToken = true
		p.nextToken()
	}
	// Index signatures and computed property names are type members
	if p.token == ast.KindOpenBracketToken {
		return true
	}
	// Try to get the first property-like token following all modifiers
	if p.isLiteralPropertyName() {
		idToken = true
		p.nextToken()
	}
	// If we were able to get any potential identifier, check that it is
	// the start of a member declaration
	if idToken {
		return p.token == ast.KindOpenParenToken || p.token == ast.KindLessThanToken || p.token == ast.KindQuestionToken || p.token == ast.KindColonToken || p.token == ast.KindCommaToken || p.canParseSemicolon()
	}
	return false
}

func (p *Parser) scanClassMemberStart() bool {
	idToken := ast.KindUnknown
	if p.token == ast.KindAtToken {
		return true
	}
	// Eat up all modifiers, but hold on to the last one in case it is actually an identifier.
	for ast.IsModifierKind(p.token) {
		idToken = p.token
		// If the idToken is a class modifier (protected, private, public, and static), it is
		// certain that we are starting to parse class member. This allows better error recovery
		// Example:
		//      public foo() ...     // true
		//      public @dec blah ... // true; we will then report an error later
		//      export public ...    // true; we will then report an error later
		if ast.IsClassMemberModifier(idToken) {
			return true
		}
		p.nextToken()
	}
	if p.token == ast.KindAsteriskToken {
		return true
	}
	// Try to get the first property-like token following all modifiers.
	// This can either be an identifier or the 'get' or 'set' keywords.
	if p.isLiteralPropertyName() {
		idToken = p.token
		p.nextToken()
	}
	// Index signatures and computed properties are class members; we can parse.
	if p.token == ast.KindOpenBracketToken {
		return true
	}
	// If we were able to get any potential identifier...
	if idToken != ast.KindUnknown {
		// If we have a non-keyword identifier, or if we have an accessor, then it's safe to parse.
		if !ast.IsKeyword(idToken) || idToken == ast.KindSetKeyword || idToken == ast.KindGetKeyword {
			return true
		}
		// If it *is* a keyword, but not an accessor, check a little farther along
		// to see if it should actually be parsed as a class member.
		switch p.token {
		case ast.KindOpenParenToken, // Method declaration
			ast.KindLessThanToken,    // Generic Method declaration
			ast.KindExclamationToken, // Non-null assertion on property name
			ast.KindColonToken,       // Type Annotation for declaration
			ast.KindEqualsToken,      // Initializer for declaration
			ast.KindQuestionToken:    // Not valid, but permitted so that it gets caught later on.
			return true
		}
		// Covers
		//  - Semicolons     (declaration termination)
		//  - Closing braces (end-of-class, must be declaration)
		//  - End-of-files   (not valid, but permitted so that it gets caught later on)
		//  - Line-breaks    (enabling *automatic semicolon insertion*)
		return p.canParseSemicolon()
	}
	return false
}

func (p *Parser) canParseSemicolon() bool {
	// If there's a real semicolon, then we can always parse it out.
	// We can parse out an optional semicolon in ASI cases in the following cases.
	return p.token == ast.KindSemicolonToken || p.token == ast.KindCloseBraceToken || p.token == ast.KindEndOfFile || p.hasPrecedingLineBreak()
}

func (p *Parser) tryParseSemicolon() bool {
	if !p.canParseSemicolon() {
		return false
	}
	if p.token == ast.KindSemicolonToken {
		// consume the semicolon if it was explicitly provided.
		p.nextToken()
	}
	return true
}

func (p *Parser) parseSemicolon() bool {
	return p.tryParseSemicolon() || p.parseExpected(ast.KindSemicolonToken)
}

func (p *Parser) isLiteralPropertyName() bool {
	return tokenIsIdentifierOrKeyword(p.token) || p.token == ast.KindStringLiteral || p.token == ast.KindNumericLiteral || p.token == ast.KindBigIntLiteral
}

func (p *Parser) isStartOfStatement() bool {
	switch p.token {
	// 'catch' and 'finally' do not actually indicate that the code is part of a statement,
	// however, we say they are here so that we may gracefully parse them and error later.
	case ast.KindAtToken, ast.KindSemicolonToken, ast.KindOpenBraceToken, ast.KindVarKeyword, ast.KindLetKeyword,
		ast.KindUsingKeyword, ast.KindFunctionKeyword, ast.KindClassKeyword, ast.KindEnumKeyword, ast.KindIfKeyword,
		ast.KindDoKeyword, ast.KindWhileKeyword, ast.KindForKeyword, ast.KindContinueKeyword, ast.KindBreakKeyword,
		ast.KindReturnKeyword, ast.KindWithKeyword, ast.KindSwitchKeyword, ast.KindThrowKeyword, ast.KindTryKeyword,
		ast.KindDebuggerKeyword, ast.KindCatchKeyword, ast.KindFinallyKeyword:
		return true
	case ast.KindImportKeyword:
		return p.isStartOfDeclaration() || p.isNextTokenOpenParenOrLessThanOrDot()
	case ast.KindConstKeyword, ast.KindExportKeyword:
		return p.isStartOfDeclaration()
	case ast.KindAsyncKeyword, ast.KindDeclareKeyword, ast.KindInterfaceKeyword, ast.KindModuleKeyword, ast.KindNamespaceKeyword,
		ast.KindTypeKeyword, ast.KindGlobalKeyword, ast.KindDeferKeyword:
		// When these don't start a declaration, they're an identifier in an expression statement
		return true
	case ast.KindAccessorKeyword, ast.KindPublicKeyword, ast.KindPrivateKeyword, ast.KindProtectedKeyword, ast.KindStaticKeyword,
		ast.KindReadonlyKeyword:
		// When these don't start a declaration, they may be the start of a class member if an identifier
		// immediately follows. Otherwise they're an identifier in an expression statement.
		return p.isStartOfDeclaration() || !p.lookAhead((*Parser).nextTokenIsIdentifierOrKeywordOnSameLine)

	default:
		return p.isStartOfExpression()
	}
}

func (p *Parser) isStartOfDeclaration() bool {
	return p.lookAhead((*Parser).scanStartOfDeclaration)
}

func (p *Parser) scanStartOfDeclaration() bool {
	for {
		switch p.token {
		case ast.KindVarKeyword, ast.KindLetKeyword, ast.KindConstKeyword, ast.KindFunctionKeyword, ast.KindClassKeyword,
			ast.KindEnumKeyword:
			return true
		case ast.KindUsingKeyword:
			return p.isUsingDeclaration()
		case ast.KindAwaitKeyword:
			return p.isAwaitUsingDeclaration()
		// 'declare', 'module', 'namespace', 'interface'* and 'type' are all legal JavaScript identifiers;
		// however, an identifier cannot be followed by another identifier on the same line. This is what we
		// count on to parse out the respective declarations. For instance, we exploit this to say that
		//
		//    namespace n
		//
		// can be none other than the beginning of a namespace declaration, but need to respect that JavaScript sees
		//
		//    namespace
		//    n
		//
		// as the identifier 'namespace' on one line followed by the identifier 'n' on another.
		// We need to look one token ahead to see if it permissible to try parsing a declaration.
		//
		// *Note*: 'interface' is actually a strict mode reserved word. So while
		//
		//   "use strict"
		//   interface
		//   I {}
		//
		// could be legal, it would add complexity for very little gain.
		case ast.KindInterfaceKeyword, ast.KindTypeKeyword, ast.KindDeferKeyword:
			return p.nextTokenIsIdentifierOnSameLine()
		case ast.KindModuleKeyword, ast.KindNamespaceKeyword:
			return p.nextTokenIsIdentifierOrStringLiteralOnSameLine()
		case ast.KindAbstractKeyword, ast.KindAccessorKeyword, ast.KindAsyncKeyword, ast.KindDeclareKeyword, ast.KindPrivateKeyword,
			ast.KindProtectedKeyword, ast.KindPublicKeyword, ast.KindReadonlyKeyword:
			previousToken := p.token
			p.nextToken()
			// ASI takes effect for this modifier.
			if p.hasPrecedingLineBreak() {
				return false
			}
			if previousToken == ast.KindDeclareKeyword && p.token == ast.KindTypeKeyword {
				// If we see 'declare type', then commit to parsing a type alias. parseTypeAliasDeclaration will
				// report Line_break_not_permitted_here if needed.
				return true
			}
			continue
		case ast.KindGlobalKeyword:
			p.nextToken()
			return p.token == ast.KindOpenBraceToken || p.token == ast.KindIdentifier || p.token == ast.KindExportKeyword
		case ast.KindImportKeyword:
			p.nextToken()
			return p.token == ast.KindDeferKeyword || p.token == ast.KindStringLiteral || p.token == ast.KindAsteriskToken || p.token == ast.KindOpenBraceToken || tokenIsIdentifierOrKeyword(p.token)
		case ast.KindExportKeyword:
			p.nextToken()
			if p.token == ast.KindEqualsToken || p.token == ast.KindAsteriskToken || p.token == ast.KindOpenBraceToken ||
				p.token == ast.KindDefaultKeyword || p.token == ast.KindAsKeyword || p.token == ast.KindAtToken {
				return true
			}
			if p.token == ast.KindTypeKeyword {
				p.nextToken()
				return p.token == ast.KindAsteriskToken || p.token == ast.KindOpenBraceToken || p.isIdentifier() && !p.hasPrecedingLineBreak()
			}
			continue
		case ast.KindStaticKeyword:
			p.nextToken()
			continue
		}
		return false
	}
}

func (p *Parser) isStartOfExpression() bool {
	if p.isStartOfLeftHandSideExpression() {
		return true
	}
	switch p.token {
	case ast.KindPlusToken, ast.KindMinusToken, ast.KindTildeToken, ast.KindExclamationToken, ast.KindDeleteKeyword,
		ast.KindTypeOfKeyword, ast.KindVoidKeyword, ast.KindPlusPlusToken, ast.KindMinusMinusToken, ast.KindLessThanToken,
		ast.KindAwaitKeyword, ast.KindYieldKeyword, ast.KindPrivateIdentifier, ast.KindAtToken:
		// Yield/await always starts an expression.  Either it is an identifier (in which case
		// it is definitely an expression).  Or it's a keyword (either because we're in
		// a generator or async function, or in strict mode (or both)) and it started a yield or await expression.
		return true
	}
	// Error tolerance.  If we see the start of some binary operator, we consider
	// that the start of an expression.  That way we'll parse out a missing identifier,
	// give a good message about an identifier being missing, and then consume the
	// rest of the binary expression.
	if p.isBinaryOperator() {
		return true
	}
	return p.isIdentifier()
}

func (p *Parser) isStartOfLeftHandSideExpression() bool {
	switch p.token {
	case ast.KindThisKeyword, ast.KindSuperKeyword, ast.KindNullKeyword, ast.KindTrueKeyword, ast.KindFalseKeyword,
		ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateHead,
		ast.KindOpenParenToken, ast.KindOpenBracketToken, ast.KindOpenBraceToken, ast.KindFunctionKeyword, ast.KindClassKeyword,
		ast.KindNewKeyword, ast.KindSlashToken, ast.KindSlashEqualsToken, ast.KindIdentifier:
		return true
	case ast.KindImportKeyword:
		return p.isNextTokenOpenParenOrLessThanOrDot()
	}
	return p.isIdentifier()
}

func (p *Parser) isStartOfType(inStartOfParameter bool) bool {
	switch p.token {
	case ast.KindAnyKeyword, ast.KindUnknownKeyword, ast.KindStringKeyword, ast.KindNumberKeyword, ast.KindBigIntKeyword,
		ast.KindBooleanKeyword, ast.KindReadonlyKeyword, ast.KindSymbolKeyword, ast.KindUniqueKeyword, ast.KindVoidKeyword,
		ast.KindUndefinedKeyword, ast.KindNullKeyword, ast.KindThisKeyword, ast.KindTypeOfKeyword, ast.KindNeverKeyword,
		ast.KindOpenBraceToken, ast.KindOpenBracketToken, ast.KindLessThanToken, ast.KindBarToken, ast.KindAmpersandToken,
		ast.KindNewKeyword, ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindTrueKeyword,
		ast.KindFalseKeyword, ast.KindObjectKeyword, ast.KindAsteriskToken, ast.KindQuestionToken, ast.KindExclamationToken,
		ast.KindDotDotDotToken, ast.KindInferKeyword, ast.KindImportKeyword, ast.KindAssertsKeyword, ast.KindNoSubstitutionTemplateLiteral,
		ast.KindTemplateHead:
		return true
	case ast.KindFunctionKeyword:
		return !inStartOfParameter
	case ast.KindMinusToken:
		return !inStartOfParameter && p.lookAhead((*Parser).nextTokenIsNumericOrBigIntLiteral)
	case ast.KindOpenParenToken:
		// Only consider '(' the start of a type if followed by ')', '...', an identifier, a modifier,
		// or something that starts a type. We don't want to consider things like '(1)' a type.
		return !inStartOfParameter && p.lookAhead((*Parser).nextIsParenthesizedOrFunctionType)
	}
	return p.isIdentifier()
}

func (p *Parser) nextTokenIsNumericOrBigIntLiteral() bool {
	p.nextToken()
	return p.token == ast.KindNumericLiteral || p.token == ast.KindBigIntLiteral
}

func (p *Parser) nextIsParenthesizedOrFunctionType() bool {
	p.nextToken()
	return p.token == ast.KindCloseParenToken || p.isStartOfParameter(false /*isJSDocParameter*/) || p.isStartOfType(false /*inStartOfParameter*/)
}

func (p *Parser) isStartOfParameter(isJSDocParameter bool) bool {
	return p.token == ast.KindDotDotDotToken ||
		p.isBindingIdentifierOrPrivateIdentifierOrPattern() ||
		ast.IsModifierKind(p.token) ||
		p.token == ast.KindAtToken ||
		p.isStartOfType(!isJSDocParameter /*inStartOfParameter*/)
}

func (p *Parser) isBindingIdentifierOrPrivateIdentifierOrPattern() bool {
	return p.token == ast.KindOpenBraceToken || p.token == ast.KindOpenBracketToken || p.token == ast.KindPrivateIdentifier || p.isBindingIdentifier()
}

func (p *Parser) isNextTokenOpenParenOrLessThanOrDot() bool {
	return p.lookAhead((*Parser).nextTokenIsOpenParenOrLessThanOrDot)
}

func (p *Parser) nextTokenIsOpenParenOrLessThanOrDot() bool {
	switch p.nextToken() {
	case ast.KindOpenParenToken, ast.KindLessThanToken, ast.KindDotToken:
		return true
	}
	return false
}

func (p *Parser) nextTokenIsIdentifierOnSameLine() bool {
	p.nextToken()
	return p.isIdentifier() && !p.hasPrecedingLineBreak()
}

func (p *Parser) nextTokenIsIdentifierOrStringLiteralOnSameLine() bool {
	p.nextToken()
	return (p.isIdentifier() || p.token == ast.KindStringLiteral) && !p.hasPrecedingLineBreak()
}

// Ignore strict mode flag because we will report an error in type checker instead.
func (p *Parser) isIdentifier() bool {
	if p.token == ast.KindIdentifier {
		return true
	}
	// If we have a 'yield' keyword, and we're in the [yield] context, then 'yield' is
	// considered a keyword and is not an identifier.
	// If we have a 'await' keyword, and we're in the [Await] context, then 'await' is
	// considered a keyword and is not an identifier.
	if p.token == ast.KindYieldKeyword && p.inYieldContext() || p.token == ast.KindAwaitKeyword && p.inAwaitContext() {
		return false
	}
	return p.token > ast.KindLastReservedWord
}

func (p *Parser) isBindingIdentifier() bool {
	// `let await`/`let yield` in [Yield] or [Await] are allowed here and disallowed in the binder.
	return p.token == ast.KindIdentifier || p.token > ast.KindLastReservedWord
}

func (p *Parser) isImportAttributeName() bool {
	return tokenIsIdentifierOrKeyword(p.token) || p.token == ast.KindStringLiteral
}

func (p *Parser) isBinaryOperator() bool {
	if p.inDisallowInContext() && p.token == ast.KindInKeyword {
		return false
	}
	return ast.GetBinaryOperatorPrecedence(p.token) != ast.OperatorPrecedenceInvalid
}

func (p *Parser) isValidHeritageClauseObjectLiteral() bool {
	return p.lookAhead((*Parser).nextIsValidHeritageClauseObjectLiteral)
}

func (p *Parser) nextIsValidHeritageClauseObjectLiteral() bool {
	if p.nextToken() == ast.KindCloseBraceToken {
		// if we see "extends {}" then only treat the {} as what we're extending (and not
		// the class body) if we have:
		//
		//      extends {} {
		//      extends {},
		//      extends {} extends
		//      extends {} implements
		next := p.nextToken()
		return next == ast.KindCommaToken || next == ast.KindOpenBraceToken || next == ast.KindExtendsKeyword || next == ast.KindImplementsKeyword
	}
	return true
}

func (p *Parser) isHeritageClause() bool {
	return p.token == ast.KindExtendsKeyword || p.token == ast.KindImplementsKeyword
}

func (p *Parser) isHeritageClauseExtendsOrImplementsKeyword() bool {
	return p.isHeritageClause() && p.lookAhead((*Parser).nextIsStartOfExpression)
}

func (p *Parser) nextIsStartOfExpression() bool {
	p.nextToken()
	return p.isStartOfExpression()
}

func (p *Parser) isUsingDeclaration() bool {
	// 'using' always starts a lexical declaration if followed by an identifier. We also eagerly parse
	// |ObjectBindingPattern| so that we can report a grammar error during check. We don't parse out
	// |ArrayBindingPattern| since it potentially conflicts with element access (i.e., `using[x]`).
	return p.lookAhead(func(p *Parser) bool {
		return p.nextTokenIsBindingIdentifierOrStartOfDestructuringOnSameLine( /*disallowOf*/ false)
	})
}

func (p *Parser) nextTokenIsEqualsOrSemicolonOrColonToken() bool {
	p.nextToken()
	return p.token == ast.KindEqualsToken || p.token == ast.KindSemicolonToken || p.token == ast.KindColonToken
}

func (p *Parser) nextTokenIsBindingIdentifierOrStartOfDestructuringOnSameLine(disallowOf bool) bool {
	p.nextToken()
	if disallowOf && p.token == ast.KindOfKeyword {
		return p.lookAhead((*Parser).nextTokenIsEqualsOrSemicolonOrColonToken)
	}
	return (p.isBindingIdentifier() || p.token == ast.KindOpenBraceToken) && !p.hasPrecedingLineBreak()
}

func (p *Parser) nextTokenIsBindingIdentifierOrStartOfDestructuringOnSameLineDisallowOf() bool {
	return p.nextTokenIsBindingIdentifierOrStartOfDestructuringOnSameLine( /*disallowOf*/ true)
}

func (p *Parser) isAwaitUsingDeclaration() bool {
	return p.lookAhead((*Parser).nextIsUsingKeywordThenBindingIdentifierOrStartOfObjectDestructuringOnSameLine)
}

func (p *Parser) nextIsUsingKeywordThenBindingIdentifierOrStartOfObjectDestructuringOnSameLine() bool {
	return p.nextToken() == ast.KindUsingKeyword && p.nextTokenIsBindingIdentifierOrStartOfDestructuringOnSameLine( /*disallowOf*/ false)
}

func (p *Parser) nextTokenIsTokenStringLiteral() bool {
	return p.nextToken() == ast.KindStringLiteral
}

func (p *Parser) setContextFlags(flags ast.NodeFlags, value bool) {
	if value {
		p.contextFlags |= flags
	} else {
		p.contextFlags &^= flags
	}
}

func (p *Parser) doInContext[T any](flags ast.NodeFlags, value bool, f func(p *Parser) T) T {
	saveContextFlags := p.contextFlags
	p.setContextFlags(flags, value)
	result := f(p)
	p.contextFlags = saveContextFlags
	return result
}

func (p *Parser) inYieldContext() bool {
	return p.contextFlags&ast.NodeFlagsYieldContext != 0
}

func (p *Parser) inDisallowInContext() bool {
	return p.contextFlags&ast.NodeFlagsDisallowInContext != 0
}

func (p *Parser) inDisallowConditionalTypesContext() bool {
	return p.contextFlags&ast.NodeFlagsDisallowConditionalTypesContext != 0
}

func (p *Parser) inDecoratorContext() bool {
	return p.contextFlags&ast.NodeFlagsDecoratorContext != 0
}

func (p *Parser) inAwaitContext() bool {
	return p.contextFlags&ast.NodeFlagsAwaitContext != 0
}

func (p *Parser) skipRangeTrivia(textRange core.TextRange) core.TextRange {
	return core.NewTextRange(scanner.SkipTrivia(p.sourceText, textRange.Pos()), textRange.End())
}

func isReservedWord(token ast.Kind) bool {
	return ast.KindFirstReservedWord <= token && token <= ast.KindLastReservedWord
}

// attachFileToDiagnostics is not ported: the diagnostics keep a nil file
// until the program attaches one (7c).

func getCommentPragmas(f *ast.NodeFactory, sourceText string) (pragmas []ast.Pragma) {
	for commentRange := range scanner.GetLeadingCommentRanges(f, sourceText, 0) {
		comment := sourceText[commentRange.Pos():commentRange.End()]
		pragmas = append(pragmas, extractPragmas(commentRange, comment)...)
	}
	return pragmas
}

func extractPragmas(commentRange ast.CommentRange, text string) []ast.Pragma {
	if commentRange.Kind == ast.KindSingleLineCommentTrivia {
		pos := 2
		tripleSlash := match(text, pos, "/")
		if tripleSlash {
			pos++
		}
		pos = skipBlanks(text, pos)
		if tripleSlash && match(text, pos, "<") {
			tagName := extractName(text, pos+1)
			if tagName != "reference" {
				return nil
			}
			pos += 10
			args := make(map[string]ast.PragmaArgument)
			for {
				pos = skipBlanks(text, pos)
				if match(text, pos, "/>") {
					break
				}
				argName := extractName(text, pos)
				if argName == "" {
					break
				}
				pos = skipBlanks(text, pos+len(argName))
				if !match(text, pos, "=") {
					break
				}
				pos = skipBlanks(text, pos+1)
				value, ok := extractQuotedString(text, pos)
				if !ok {
					break
				}
				args[argName] = ast.PragmaArgument{
					Name:      argName,
					Value:     value,
					TextRange: core.NewTextRange(commentRange.Pos()+pos+1, commentRange.Pos()+pos+1+len(value)),
				}
				pos += len(value) + 2
			}
			return []ast.Pragma{{
				CommentRange: commentRange,
				Name:         "reference",
				Args:         args,
			}}
		}
		if match(text, pos, "@") {
			pos++
			pragmaName := extractName(text, pos)
			if !(pragmaName == "ts-check" || pragmaName == "ts-nocheck") {
				return nil
			}
			return []ast.Pragma{{
				CommentRange: commentRange,
				Name:         pragmaName,
			}}
		}
	}
	if commentRange.Kind == ast.KindMultiLineCommentTrivia {
		text = strings.TrimSuffix(text, "*/")
		pos := 2
		var pragmas []ast.Pragma
		for {
			if pos = skipTo(text, pos, "@"); pos < 0 {
				break
			}
			// Mirrors the /@(\S+)(\s+(?:\S.*)?)?$/gm pragma regex used by TypeScript: the '@'
			// must be immediately followed by a non-whitespace pragma name, and the remainder
			// of the line is consumed as that pragma's arguments. As a consequence, only the
			// first '@'-token on a line is considered, so an unrelated '@token' earlier on the
			// line (e.g. an email address) prevents a later '@jsx' on the same line from being
			// treated as a pragma.
			namePos := pos + 1
			nameEnd := skipNonBlanks(text, namePos)
			if nameEnd == namePos {
				pos++
				continue
			}
			lineEnd := lineEndPos(text, pos)
			pragmaName := strings.ToLower(text[namePos:nameEnd])
			if pragmaName == "jsx" || pragmaName == "jsxfrag" || pragmaName == "jsximportsource" || pragmaName == "jsxruntime" {
				start := skipBlanks(text, nameEnd)
				argEnd := skipNonBlanks(text, start)
				if argEnd != start {
					args := make(map[string]ast.PragmaArgument, 1)
					args["factory"] = ast.PragmaArgument{
						Name:      "factory",
						Value:     text[start:argEnd],
						TextRange: core.NewTextRange(commentRange.Pos()+start, commentRange.Pos()+argEnd),
					}
					pragmas = append(pragmas, ast.Pragma{
						CommentRange: commentRange,
						Name:         pragmaName,
						Args:         args,
					})
				}
			}
			pos = lineEnd
		}
		return pragmas
	}
	return nil
}

func match(text string, pos int, s string) bool {
	return strings.HasPrefix(text[pos:], s)
}

func skipBlanks(text string, pos int) int {
	for pos < len(text) && (text[pos] == ' ' || text[pos] == '\t') {
		pos++
	}
	return pos
}

func skipNonBlanks(text string, pos int) int {
	for pos < len(text) && (text[pos] != ' ' && text[pos] != '\t' && text[pos] != '\r' && text[pos] != '\n') {
		pos++
	}
	return pos
}

func skipTo(text string, pos int, s string) int {
	if pos >= len(text) {
		return -1
	}
	i := strings.Index(text[pos:], s)
	if i < 0 {
		return -1
	}
	return pos + i
}

func lineEndPos(text string, pos int) int {
	for pos < len(text) {
		ch, size := utf8.DecodeRuneInString(text[pos:])
		if stringutil.IsLineBreak(ch) {
			return pos
		}
		pos += size
	}
	return len(text)
}

func extractName(text string, pos int) string {
	start := pos
	for pos < len(text) && (text[pos] >= 'A' && text[pos] <= 'Z' || text[pos] >= 'a' && text[pos] <= 'z' || text[pos] == '-') {
		pos++
	}
	return strings.ToLower(text[start:pos])
}

func extractQuotedString(text string, pos int) (string, bool) {
	if pos == len(text) {
		return "", false
	}
	quote := text[pos]
	if quote != '\'' && quote != '"' {
		return "", false
	}
	pos++
	start := pos
	for pos < len(text) && text[pos] != quote {
		pos++
	}
	if pos == len(text) {
		return "", false
	}
	return text[start:pos], true
}

func (p *Parser) processPragmasIntoFields(context *store.File) {
	context.CheckJsDirective = nil
	context.ReferencedFiles = nil
	context.TypeReferenceDirectives = nil
	context.LibReferenceDirectives = nil
	// context.AmdDependencies = nil
	for _, pragma := range context.Pragmas {
		switch pragma.Name {
		case "reference":
			types, typesOk := pragma.Args["types"]
			lib, libOk := pragma.Args["lib"]
			path, pathOk := pragma.Args["path"]
			resolutionMode, resolutionModeOk := pragma.Args["resolution-mode"]
			preserve, preserveOk := pragma.Args["preserve"]
			noDefaultLib, noDefaultLibOk := pragma.Args["no-default-lib"]
			switch {
			case noDefaultLibOk && noDefaultLib.Value == "true":
				// Ignored.
			case typesOk:
				var parsed core.ResolutionMode
				if resolutionModeOk {
					parsed = p.parseResolutionMode(resolutionMode.Value, resolutionMode.Pos(), resolutionMode.End())
				}
				context.TypeReferenceDirectives = append(context.TypeReferenceDirectives, &ast.FileReference{
					TextRange:      types.TextRange,
					FileName:       types.Value,
					ResolutionMode: parsed,
					Preserve:       preserveOk && preserve.Value == "true",
				})
			case libOk:
				context.LibReferenceDirectives = append(context.LibReferenceDirectives, &ast.FileReference{
					TextRange: lib.TextRange,
					FileName:  lib.Value,
					Preserve:  preserveOk && preserve.Value == "true",
				})
			case pathOk:
				context.ReferencedFiles = append(context.ReferencedFiles, &ast.FileReference{
					TextRange: path.TextRange,
					FileName:  path.Value,
					Preserve:  preserveOk && preserve.Value == "true",
				})
			default:
				p.parseErrorAtRange(pragma.TextRange, diagnostics.Invalid_reference_directive_syntax)
			}
		case "ts-check", "ts-nocheck":
			// _last_ of either nocheck or check in a file is the "winner"
			if context.CheckJsDirective == nil || pragma.TextRange.Pos() > context.CheckJsDirective.Range.Pos() {
				context.CheckJsDirective = &ast.CheckJsDirective{
					Enabled: pragma.Name == "ts-check",
					Range:   pragma.CommentRange,
				}
			}
		case "jsx", "jsxfrag", "jsximportsource", "jsxruntime":
			// Nothing to do here
		default:
			panic("Unhandled pragma kind: " + pragma.Name)
		}
	}
}

func (p *Parser) parseResolutionMode(mode string, pos int, end int) (resolutionKind core.ResolutionMode) {
	if mode == "import" {
		resolutionKind = core.ModuleKindESNext
		return resolutionKind
	}
	if mode == "require" {
		resolutionKind = core.ModuleKindCommonJS
		return resolutionKind
	}
	p.parseErrorAt(pos, end, diagnostics.X_resolution_mode_should_be_either_require_or_import)
	return resolutionKind
}

func (p *Parser) jsErrorAtRange(loc core.TextRange, message *diagnostics.Message, args ...any) {
	p.jsDiagnostics = append(p.jsDiagnostics, ast.NewDiagnostic(nil, core.NewTextRange(scanner.SkipTrivia(p.sourceText, loc.Pos()), loc.End()), message, args...))
}

func (p *Parser) checkJSDecoratorSyntax(node store.NodeRef) {
	modifiers := p.b.View(node).Modifiers()
	if modifiers.Len() == 0 {
		return
	}

	kind := p.b.View(node).Kind()
	if canHaveIllegalDecorators(kind) {
		for _, modifier := range modifiers.Refs() {
			if p.b.View(modifier).Kind() == ast.KindDecorator {
				p.jsErrorAtRange(p.loc(modifier), diagnostics.Decorators_are_not_valid_here)
				break
			}
		}
	} else if canHaveDecorators(kind) {
		decoratorIndex := p.findModifierIndex(modifiers.Ref(), isDecorator)
		if decoratorIndex >= 0 {
			if kind == ast.KindClassDeclaration {
				exportIndex := p.findModifierIndex(modifiers.Ref(), isExportModifier)
				if exportIndex >= 0 {
					defaultIndex := p.findModifierIndex(modifiers.Ref(), func(m store.Node) bool {
						return m.Kind() == ast.KindDefaultKeyword
					})
					if decoratorIndex > exportIndex && defaultIndex >= 0 && decoratorIndex < defaultIndex {
						// Decorator between `export` and `default`
						p.jsErrorAtRange(p.loc(modifiers.Refs()[decoratorIndex]), diagnostics.Decorators_are_not_valid_here)
					} else if decoratorIndex < exportIndex {
						// Find a trailing decorator after the export keyword
						trailingDecoratorIndex := -1
						for i := exportIndex; i < modifiers.Len(); i++ {
							if modifiers.At(i).Kind() == ast.KindDecorator {
								trailingDecoratorIndex = i
								break
							}
						}
						if trailingDecoratorIndex >= 0 {
							trailing, first := p.loc(modifiers.Refs()[trailingDecoratorIndex]), p.loc(modifiers.Refs()[decoratorIndex])
							diag := ast.NewDiagnostic(
								nil,
								core.NewTextRange(scanner.SkipTrivia(p.sourceText, trailing.Pos()), trailing.End()),
								diagnostics.Decorators_may_not_appear_after_export_or_export_default_if_they_also_appear_before_export,
							)
							diag.AddRelatedInfo(ast.NewDiagnostic(
								nil,
								core.NewTextRange(scanner.SkipTrivia(p.sourceText, first.Pos()), first.End()),
								diagnostics.Decorator_used_before_export_here,
							))
							p.jsDiagnostics = append(p.jsDiagnostics, diag)
						}
					}
				}
			}
		}
	}
}

// checkJSSyntax only reads the node and appends diagnostics, so the view v
// stays valid throughout.
func (p *Parser) checkJSSyntax(node store.NodeRef) store.NodeRef {
	v := p.b.View(node)
	if v.Flags()&ast.NodeFlagsJavaScriptFile == 0 || v.Flags()&(ast.NodeFlagsJSDoc|ast.NodeFlagsReparsed) != 0 {
		return node
	}
	switch v.Kind() {
	case ast.KindParameter, ast.KindPropertyDeclaration, ast.KindMethodDeclaration:
		// (*ast.Node).QuestionToken falls back to the postfix token.
		token := v.QuestionToken()
		if token.IsNil() {
			token = v.PostfixToken()
		}
		if !token.IsNil() && token.Flags()&ast.NodeFlagsReparsed == 0 && token.Kind() == ast.KindQuestionToken {
			p.jsErrorAtRange(p.loc(token.Ref()), diagnostics.The_0_modifier_can_only_be_used_in_TypeScript_files, "?")
		}
		fallthrough
	case ast.KindMethodSignature, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindFunctionExpression,
		ast.KindFunctionDeclaration, ast.KindArrowFunction, ast.KindVariableDeclaration, ast.KindIndexSignature:
		if ast.IsFunctionLikeKind(v.Kind()) && v.Body().IsNil() {
			p.jsErrorAtRange(p.loc(node), diagnostics.Signature_declarations_can_only_be_used_in_TypeScript_files)
		} else if t := v.Type(); !t.IsNil() && t.Flags()&ast.NodeFlagsReparsed == 0 {
			p.jsErrorAtRange(p.loc(t.Ref()), diagnostics.Type_annotations_can_only_be_used_in_TypeScript_files)
		}
	case ast.KindImportDeclaration:
		if clause := v.ImportClause(); !clause.IsNil() && clause.AsImportClause().PhaseModifier() == ast.KindTypeKeyword {
			p.jsErrorAtRange(p.loc(node), diagnostics.X_0_declarations_can_only_be_used_in_TypeScript_files, "import type")
		}
	case ast.KindExportDeclaration:
		if v.AsExportDeclaration().IsTypeOnly() {
			p.jsErrorAtRange(p.loc(node), diagnostics.X_0_declarations_can_only_be_used_in_TypeScript_files, "export type")
		}
	case ast.KindImportSpecifier:
		if v.AsImportSpecifier().IsTypeOnly() {
			p.jsErrorAtRange(p.loc(node), diagnostics.X_0_declarations_can_only_be_used_in_TypeScript_files, "import...type")
		}
	case ast.KindExportSpecifier:
		if v.AsExportSpecifier().IsTypeOnly() {
			p.jsErrorAtRange(p.loc(node), diagnostics.X_0_declarations_can_only_be_used_in_TypeScript_files, "export...type")
		}
	case ast.KindImportEqualsDeclaration:
		p.jsErrorAtRange(p.loc(node), diagnostics.X_import_can_only_be_used_in_TypeScript_files)
	case ast.KindExportAssignment:
		if v.AsExportAssignment().IsExportEquals() {
			p.jsErrorAtRange(p.loc(node), diagnostics.X_export_can_only_be_used_in_TypeScript_files)
		}
	case ast.KindHeritageClause:
		if v.AsHeritageClause().Token() == ast.KindImplementsKeyword {
			p.jsErrorAtRange(p.loc(node), diagnostics.X_implements_clauses_can_only_be_used_in_TypeScript_files)
		}
	case ast.KindInterfaceDeclaration:
		p.jsErrorAtRange(p.loc(v.Name().Ref()), diagnostics.X_0_declarations_can_only_be_used_in_TypeScript_files, "interface")
	case ast.KindModuleDeclaration:
		p.jsErrorAtRange(p.loc(v.Name().Ref()), diagnostics.X_0_declarations_can_only_be_used_in_TypeScript_files, scanner.TokenToString(v.AsModuleDeclaration().Keyword()))
	case ast.KindTypeAliasDeclaration:
		p.jsErrorAtRange(p.loc(v.Name().Ref()), diagnostics.Type_aliases_can_only_be_used_in_TypeScript_files)
	case ast.KindEnumDeclaration:
		p.jsErrorAtRange(p.loc(v.Name().Ref()), diagnostics.X_0_declarations_can_only_be_used_in_TypeScript_files, "enum")
	case ast.KindNonNullExpression:
		p.jsErrorAtRange(p.loc(node), diagnostics.Non_null_assertions_can_only_be_used_in_TypeScript_files)
	case ast.KindAsExpression:
		p.jsErrorAtRange(p.loc(v.Type().Ref()), diagnostics.Type_assertion_expressions_can_only_be_used_in_TypeScript_files)
	case ast.KindSatisfiesExpression:
		p.jsErrorAtRange(p.loc(v.Type().Ref()), diagnostics.Type_satisfaction_expressions_can_only_be_used_in_TypeScript_files)
	}
	// Check decorator placement in JS files
	p.checkJSDecoratorSyntax(node)
	// Check absence of type parameters, type arguments and non-JavaScript modifiers
	switch v.Kind() {
	case ast.KindClassDeclaration, ast.KindClassExpression, ast.KindMethodDeclaration, ast.KindConstructor, ast.KindGetAccessor,
		ast.KindSetAccessor, ast.KindFunctionExpression, ast.KindFunctionDeclaration, ast.KindArrowFunction:
		if list := v.TypeParameters(); !list.IsNil() && p.someModifier(list.Ref(), isNotReparsed) {
			p.jsErrorAtRange(p.listLoc(list.Ref()), diagnostics.Type_parameter_declarations_can_only_be_used_in_TypeScript_files)
		}
		fallthrough
	case ast.KindVariableStatement, ast.KindPropertyDeclaration:
		for _, modifier := range v.Modifiers().Refs() {
			m := p.b.View(modifier)
			if m.Flags()&ast.NodeFlagsReparsed == 0 && m.Kind() != ast.KindDecorator && ast.ModifierToFlag(m.Kind())&ast.ModifierFlagsJavaScript == 0 {
				p.jsErrorAtRange(p.loc(modifier), diagnostics.The_0_modifier_can_only_be_used_in_TypeScript_files, scanner.TokenToString(m.Kind()))
			}
		}
	case ast.KindParameter:
		if p.someModifier(v.Modifiers().Ref(), isModifier) {
			p.jsErrorAtRange(p.listLoc(v.Modifiers().Ref()), diagnostics.Parameter_modifiers_can_only_be_used_in_TypeScript_files)
		}
	case ast.KindCallExpression, ast.KindNewExpression, ast.KindExpressionWithTypeArguments, ast.KindJsxSelfClosingElement,
		ast.KindJsxOpeningElement, ast.KindTaggedTemplateExpression:
		if list := v.TypeArguments(); !list.IsNil() && p.someModifier(list.Ref(), isNotReparsed) {
			p.jsErrorAtRange(p.listLoc(list.Ref()), diagnostics.Type_arguments_can_only_be_used_in_TypeScript_files)
		}
	}
	return node
}
