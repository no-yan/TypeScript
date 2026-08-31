package parser

import (
	"slices"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// handleParser is the TS/JS recovery worker. It allocates exclusively through
// ast.Factory. Pointer Parser methods remain for JSX/TSX, JSON, and JSDoc.
type handleParser struct {
	*Parser
	f        *ast.Factory
	fallback storeFallbackKind
}

type storeFallbackKind int

const (
	storeFallbackNone storeFallbackKind = iota
	storeFallbackJSX
	storeFallbackJSDoc
)

type storeFallback struct {
	kind storeFallbackKind
}

func (p *handleParser) abortJSX() {
	panic(storeFallback{kind: storeFallbackJSX})
}

func (p *handleParser) abortJSDoc() {
	panic(storeFallback{kind: storeFallbackJSDoc})
}

func handleIsMissing(h ast.Handle) bool {
	if h.IsNil() {
		return true
	}
	loc := h.Loc()
	return loc.Pos() == loc.End() && loc.Pos() >= 0 && h.Kind() != ast.KindEndOfFile
}

func handleIsPresent(h ast.Handle) bool {
	return !handleIsMissing(h)
}

func handleIsDeclare(h ast.Handle) bool {
	return !h.IsNil() && h.Kind() == ast.KindDeclareKeyword
}

func handleIsExport(h ast.Handle) bool {
	return !h.IsNil() && h.Kind() == ast.KindExportKeyword
}

func handleIsAsync(h ast.Handle) bool {
	return !h.IsNil() && h.Kind() == ast.KindAsyncKeyword
}

func (p *handleParser) listHandles(list ast.ListRef) []ast.Handle {
	if list == 0 {
		return nil
	}
	n := p.f.Store().ListLen(list)
	out := make([]ast.Handle, n)
	for i := 0; i < n; i++ {
		out[i] = p.f.Store().ListAt(list, i)
	}
	return out
}

func (p *handleParser) listSome(list ast.ListRef, pred func(ast.Handle) bool) bool {
	for _, h := range p.listHandles(list) {
		if pred(h) {
			return true
		}
	}
	return false
}

func (p *handleParser) modifiersToFlags(list ast.ListRef) ast.ModifierFlags {
	var flags ast.ModifierFlags
	for _, h := range p.listHandles(list) {
		flags |= ast.ModifierToFlag(h.Kind())
	}
	return flags
}

func (p *handleParser) newList(loc core.TextRange, nodes []ast.Handle) ast.ListRef {
	if len(nodes) == 0 {
		return p.f.List(loc)
	}
	return p.f.List(loc, nodes...)
}

func (p *handleParser) parseEmptyListHandle() ast.ListRef {
	return p.newList(core.NewTextRange(p.nodePos(), p.nodePos()), nil)
}

func (p *handleParser) createMissingListHandle() ast.ListRef {
	return p.parseEmptyListHandle()
}

func (p *handleParser) finishHandle(node ast.Handle, pos int) ast.Handle {
	return p.finishHandleWithEnd(node, pos, p.nodePos())
}

func (p *handleParser) finishHandleWithEnd(node ast.Handle, pos int, end int) ast.Handle {
	flags := node.Flags() | p.contextFlags
	if p.hasParseError {
		flags |= ast.NodeFlagsThisNodeHasError
		p.hasParseError = false
	}
	node.SetFlags(flags)
	return p.f.Finish(node, core.NewTextRange(pos, end))
}

func (p *handleParser) parseTokenHandle() ast.Handle {
	pos := p.nodePos()
	kind := p.token
	p.nextToken()
	return p.finishHandle(p.f.NewToken(kind), pos)
}

func (p *handleParser) parseExpectedTokenHandle(kind ast.Kind) ast.Handle {
	token := p.parseOptionalTokenHandle(kind)
	if token.IsNil() {
		p.parseErrorAtCurrentToken(diagnostics.X_0_expected, scanner.TokenToString(kind))
		token = p.finishHandle(p.f.NewToken(kind), p.nodePos())
	}
	return token
}

func (p *handleParser) parseOptionalTokenHandle(kind ast.Kind) ast.Handle {
	if p.token == kind {
		return p.parseTokenHandle()
	}
	return ast.Handle{}
}

func (p *handleParser) newIdentifierHandle(text string) ast.Handle {
	p.identifierCount++
	id := p.f.NewIdentifier(text)
	if text == "await" {
		p.statementHasAwaitIdentifier = true
	}
	return id
}

func (p *handleParser) createMissingIdentifierHandle() ast.Handle {
	return p.finishHandle(p.newIdentifierHandle(""), p.nodePos())
}

func (p *handleParser) withJSDocHandle(node ast.Handle, info jsdocScannerInfo) {
	if info&jsdocScannerInfoHasJSDoc == 0 {
		return
	}
	if p.isJavaScript() {
		p.abortJSDoc()
	}
	flags := node.Flags() | ast.NodeFlagsHasJSDoc
	if info&jsdocScannerInfoHasDeprecated != 0 {
		flags |= ast.NodeFlagsPossiblyContainsDeprecatedTag
	}
	node.SetFlags(flags)
}

func (p *handleParser) inContext(flags ast.NodeFlags, value bool, f func(*handleParser) ast.Handle) ast.Handle {
	saveContextFlags := p.contextFlags
	p.setContextFlags(flags, value)
	result := f(p)
	p.contextFlags = saveContextFlags
	return result
}

func (p *handleParser) parseListIndex(kind ParsingContext, parseElement func(p *handleParser, index int) ast.Handle) []ast.Handle {
	saveParsingContexts := p.parsingContexts
	p.parsingContexts |= 1 << kind
	list := make([]ast.Handle, 0, 16)
	for i := 0; !p.isListTerminator(kind); i++ {
		if p.isListElement(kind, false /*inErrorRecovery*/) {
			elt := parseElement(p, len(list))
			list = append(list, elt)
			continue
		}
		if p.abortParsingListOrMoveToNextToken(kind) {
			break
		}
	}
	p.parsingContexts = saveParsingContexts
	return slices.Clone(list)
}

func (p *handleParser) parseList(kind ParsingContext, parseElement func(p *handleParser) ast.Handle) ast.ListRef {
	pos := p.nodePos()
	nodes := p.parseListIndex(kind, func(p *handleParser, _ int) ast.Handle { return parseElement(p) })
	return p.newList(core.NewTextRange(pos, p.nodePos()), nodes)
}

func (p *handleParser) parseDelimitedList(kind ParsingContext, parseElement func(p *handleParser) ast.Handle) ast.ListRef {
	pos := p.nodePos()
	saveParsingContexts := p.parsingContexts
	p.parsingContexts |= 1 << kind
	list := make([]ast.Handle, 0, 16)
	for {
		if p.isListElement(kind, false /*inErrorRecovery*/) {
			startPos := p.nodePos()
			element := parseElement(p)
			if element.IsNil() {
				p.parsingContexts = saveParsingContexts
				return 0
			}
			list = append(list, element)
			if p.parseOptional(ast.KindCommaToken) {
				continue
			}
			if p.isListTerminator(kind) {
				break
			}
			if p.token != ast.KindCommaToken && kind == PCEnumMembers {
				p.parseErrorAtCurrentToken(diagnostics.An_enum_member_name_must_be_followed_by_a_or)
			} else {
				p.parseExpected(ast.KindCommaToken)
			}
			if (kind == PCObjectLiteralMembers || kind == PCImportAttributes) && p.token == ast.KindSemicolonToken && !p.hasPrecedingLineBreak() {
				p.nextToken()
			}
			if startPos == p.nodePos() {
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
	return p.newList(core.NewTextRange(pos, p.nodePos()), list)
}

func (p *handleParser) parseBracketedList(kind ParsingContext, parseElement func(p *handleParser) ast.Handle, opening ast.Kind, closing ast.Kind) ast.ListRef {
	if p.parseExpected(opening) {
		result := p.parseDelimitedList(kind, parseElement)
		p.parseExpected(closing)
		return result
	}
	return p.createMissingListHandle()
}

func (p *handleParser) parseToplevelStatement(i int) ast.Handle {
	p.statementHasAwaitIdentifier = false
	statement := p.parseStatement()
	i += len(p.reparseList)
	if p.statementHasAwaitIdentifier && statement.Flags()&ast.NodeFlagsAwaitContext == 0 {
		if len(p.possibleAwaitSpans) == 0 || p.possibleAwaitSpans[len(p.possibleAwaitSpans)-1] != i {
			p.possibleAwaitSpans = append(p.possibleAwaitSpans, i, i+1)
		} else {
			p.possibleAwaitSpans[len(p.possibleAwaitSpans)-1] = i + 1
		}
	}
	return statement
}

func (p *handleParser) parseSourceFileWorkerHandle() (root ast.Handle) {
	defer func() {
		if r := recover(); r != nil {
			if fb, ok := r.(storeFallback); ok {
				root = ast.Handle{}
				p.fallback = fb.kind
				return
			}
			panic(r)
		}
	}()
	isDeclarationFile := tspath.IsDeclarationFileName(p.opts.FileName)
	if isDeclarationFile {
		p.contextFlags |= ast.NodeFlagsAmbient
	}
	pos := p.nodePos()
	stmts := p.parseListIndex(PCSourceElements, (*handleParser).parseToplevelStatement)
	end := p.nodePos()
	endJSDoc := p.jsdocScannerInfo()
	eof := p.parseTokenHandle()
	p.withJSDocHandle(eof, endJSDoc)
	if eof.Kind() != ast.KindEndOfFile {
		panic("Expected end of file token from scanner.")
	}
	list := p.newList(core.NewTextRange(pos, end), stmts)
	root = p.finishHandle(p.f.NewSourceFile(list, eof), pos)
	if !isDeclarationFile && p.handleLooksLikeExternalModule(root) && len(p.possibleAwaitSpans) > 0 {
		root = p.reparseTopLevelAwaitHandle(root, pos)
	}
	return root
}

func (p *handleParser) handleLooksLikeExternalModule(root ast.Handle) bool {
	if p.opts.ExternalModuleIndicatorOptions.Force {
		return true
	}
	list := root.SourceFileStatements()
	if list == 0 {
		return false
	}
	s := p.f.Store()
	for i := 0; i < s.ListLen(list); i++ {
		stmt := s.ListAt(list, i)
		if p.modifiersToFlags(stmtModifiers(stmt))&ast.ModifierFlagsExport != 0 {
			return true
		}
		switch stmt.Kind() {
		case ast.KindImportDeclaration, ast.KindExportDeclaration, ast.KindExportAssignment, ast.KindImportEqualsDeclaration:
			return true
		}
	}
	return p.sourceFlags&ast.NodeFlagsPossiblyContainsImportMeta != 0
}

func stmtModifiers(h ast.Handle) ast.ListRef {
	if h.IsNil() {
		return 0
	}
	switch h.Kind() {
	case ast.KindVariableStatement:
		return h.VariableStatementModifiers()
	case ast.KindFunctionDeclaration:
		return h.FunctionDeclarationModifiers()
	case ast.KindClassDeclaration:
		return h.ClassDeclarationModifiers()
	case ast.KindInterfaceDeclaration:
		return h.InterfaceDeclarationModifiers()
	case ast.KindTypeAliasDeclaration:
		return h.TypeAliasDeclarationModifiers()
	case ast.KindEnumDeclaration:
		return h.EnumDeclarationModifiers()
	case ast.KindModuleDeclaration:
		return h.ModuleDeclarationModifiers()
	case ast.KindImportDeclaration:
		return h.ImportDeclarationModifiers()
	case ast.KindExportDeclaration:
		return h.ExportDeclarationModifiers()
	case ast.KindExportAssignment:
		return h.ExportAssignmentModifiers()
	case ast.KindImportEqualsDeclaration:
		return h.ImportEqualsDeclarationModifiers()
	case ast.KindNamespaceExportDeclaration:
		return h.NamespaceExportDeclarationModifiers()
	}
	return 0
}

func handleText(h ast.Handle) string {
	if h.IsNil() {
		return ""
	}
	switch h.Kind() {
	case ast.KindIdentifier:
		return h.IdentifierText()
	case ast.KindPrivateIdentifier:
		return h.PrivateIdentifierText()
	case ast.KindStringLiteral:
		return h.StringLiteralText()
	default:
		return ""
	}
}

func handleIsLeftHandSide(h ast.Handle) bool {
	if h.IsNil() {
		return false
	}
	switch h.Kind() {
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression, ast.KindNewExpression, ast.KindCallExpression,
		ast.KindJsxElement, ast.KindJsxSelfClosingElement, ast.KindJsxFragment, ast.KindTaggedTemplateExpression, ast.KindArrayLiteralExpression,
		ast.KindParenthesizedExpression, ast.KindObjectLiteralExpression, ast.KindClassExpression, ast.KindFunctionExpression, ast.KindIdentifier,
		ast.KindPrivateIdentifier, ast.KindRegularExpressionLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindStringLiteral,
		ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateExpression, ast.KindFalseKeyword, ast.KindNullKeyword, ast.KindThisKeyword,
		ast.KindTrueKeyword, ast.KindSuperKeyword, ast.KindNonNullExpression, ast.KindExpressionWithTypeArguments, ast.KindMetaProperty,
		ast.KindImportKeyword, ast.KindMissingDeclaration:
		return true
	}
	return false
}

func handleCanHaveDecorators(h ast.Handle) bool {
	if h.IsNil() {
		return false
	}
	switch h.Kind() {
	case ast.KindParameter, ast.KindPropertyDeclaration, ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindClassExpression, ast.KindClassDeclaration:
		return true
	}
	return false
}

func handleCanHaveIllegalDecorators(h ast.Handle) bool {
	if h.IsNil() {
		return false
	}
	switch h.Kind() {
	case ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment,
		ast.KindFunctionDeclaration, ast.KindConstructor,
		ast.KindIndexSignature, ast.KindClassStaticBlockDeclaration,
		ast.KindMissingDeclaration, ast.KindVariableStatement,
		ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration,
		ast.KindEnumDeclaration, ast.KindModuleDeclaration,
		ast.KindImportEqualsDeclaration, ast.KindImportDeclaration, ast.KindJSImportDeclaration,
		ast.KindNamespaceExportDeclaration, ast.KindExportDeclaration,
		ast.KindExportAssignment:
		return true
	}
	return false
}

func handleIsValidHeritageTypeReferenceExpression(node ast.Handle) bool {
	if node.Kind() == ast.KindIdentifier {
		return handleIsPresent(node)
	}
	return node.Kind() == ast.KindPropertyAccessExpression &&
		node.Flags()&ast.NodeFlagsOptionalChain == 0 &&
		handleIsPresent(node.PropertyAccessExpressionName()) &&
		handleIsValidHeritageTypeReferenceExpression(node.PropertyAccessExpressionExpression())
}

func typeHasArrowFunctionBlockingParseErrorHandle(node ast.Handle) bool {
	if node.IsNil() {
		return false
	}
	switch node.Kind() {
	case ast.KindTypeReference:
		return handleIsMissing(node.TypeReferenceNodeTypeName())
	case ast.KindFunctionType:
		return node.FunctionTypeNodeParameters() == 0 || typeHasArrowFunctionBlockingParseErrorHandle(node.FunctionTypeNodeType())
	case ast.KindConstructorType:
		return node.ConstructorTypeNodeParameters() == 0 || typeHasArrowFunctionBlockingParseErrorHandle(node.ConstructorTypeNodeType())
	case ast.KindParenthesizedType:
		return typeHasArrowFunctionBlockingParseErrorHandle(node.ParenthesizedTypeNodeType())
	}
	return false
}

func (p *handleParser) parseJSDocAllType() ast.Handle {
	pos := p.nodePos()
	p.nextToken()
	return p.finishHandle(p.f.NewJSDocAllType(), pos)
}

func (p *handleParser) parseJSDocNonNullableType() ast.Handle {
	pos := p.nodePos()
	p.nextToken()
	return p.finishHandle(p.f.NewJSDocNonNullableType(p.parseTypeOperatorOrHigher()), pos)
}

func (p *handleParser) parseJSDocNullableType() ast.Handle {
	pos := p.nodePos()
	p.nextToken()
	return p.finishHandle(p.f.NewJSDocNullableType(p.parseTypeOperatorOrHigher()), pos)
}

func handleGetText(sourceText string, h ast.Handle, includeTrivia bool) string {
	if h.IsNil() {
		return ""
	}
	loc := h.Loc()
	start := loc.Pos()
	if !includeTrivia {
		start = scanner.SkipTrivia(sourceText, start)
	}
	end := loc.End()
	if start < 0 || end > len(sourceText) || start > end {
		return ""
	}
	return sourceText[start:end]
}

func (p *handleParser) reparseTopLevelAwaitHandle(root ast.Handle, pos int) ast.Handle {
	if len(p.possibleAwaitSpans)%2 == 1 {
		panic("possibleAwaitSpans malformed: odd number of indices, not paired into spans.")
	}
	s := p.f.Store()
	oldList := root.SourceFileStatements()
	oldLen := s.ListLen(oldList)
	statements := make([]ast.Handle, 0, oldLen)
	savedParseDiagnostics := p.diagnostics
	p.diagnostics = []*ast.Diagnostic{}
	afterAwaitStatement := 0
	for i := 0; i < len(p.possibleAwaitSpans); i += 2 {
		nextAwaitStatement := p.possibleAwaitSpans[i]
		prevStatement := s.ListAt(oldList, afterAwaitStatement)
		nextStatement := s.ListAt(oldList, nextAwaitStatement)
		for j := afterAwaitStatement; j < nextAwaitStatement; j++ {
			statements = append(statements, s.ListAt(oldList, j))
		}
		diagnosticStart := core.FindIndex(savedParseDiagnostics, func(diagnostic *ast.Diagnostic) bool {
			return diagnostic.Pos() >= prevStatement.Loc().Pos()
		})
		var diagnosticEnd int
		if diagnosticStart >= 0 {
			diagnosticEnd = core.FindIndex(savedParseDiagnostics[diagnosticStart:], func(diagnostic *ast.Diagnostic) bool {
				return diagnostic.Pos() >= nextStatement.Loc().Pos()
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
		p.contextFlags |= ast.NodeFlagsAwaitContext
		p.scanner.ResetPos(nextStatement.Loc().Pos())
		p.nextToken()
		afterAwaitStatement = p.possibleAwaitSpans[i+1]
		for p.token != ast.KindEndOfFile {
			startPos := p.scanner.TokenFullStart()
			statement := p.parseStatement()
			statements = append(statements, statement)
			if startPos == p.scanner.TokenFullStart() {
				p.nextToken()
			}
			if afterAwaitStatement < oldLen {
				lastAwaitStatement := s.ListAt(oldList, afterAwaitStatement-1)
				if statement.Loc().End() == lastAwaitStatement.Loc().End() {
					break
				}
				if statement.Loc().End() > lastAwaitStatement.Loc().End() {
					i += 2
					if i < len(p.possibleAwaitSpans) {
						afterAwaitStatement = p.possibleAwaitSpans[i+1]
					} else {
						afterAwaitStatement = oldLen
					}
				}
			}
		}
		state.diagnosticsLen = len(p.diagnostics)
		p.rewind(state)
	}
	if afterAwaitStatement < oldLen {
		prevStatement := s.ListAt(oldList, afterAwaitStatement)
		for j := afterAwaitStatement; j < oldLen; j++ {
			statements = append(statements, s.ListAt(oldList, j))
		}
		diagnosticStart := core.FindIndex(savedParseDiagnostics, func(diagnostic *ast.Diagnostic) bool {
			return diagnostic.Pos() >= prevStatement.Loc().Pos()
		})
		if diagnosticStart >= 0 {
			p.diagnostics = append(p.diagnostics, savedParseDiagnostics[diagnosticStart:]...)
		}
	}
	list := p.newList(s.ListLoc(oldList), statements)
	eof := root.SourceFileEndOfFileToken()
	return p.finishHandle(p.f.NewSourceFile(list, eof), pos)
}
