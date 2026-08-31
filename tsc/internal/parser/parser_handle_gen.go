package parser

// Code generated from parser.go grammar for the Handle recovery worker.
// JSX functions are omitted; hitting JSX aborts to the pointer worker.

import (
	"slices"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
)

func (p *handleParser) parseStatement() ast.Handle {
	switch p.token {
	case ast.KindSemicolonToken:
		return p.parseEmptyStatement()
	case ast.KindOpenBraceToken:
		return p.parseBlock(false /*ignoreMissingOpenBrace*/, nil)
	case ast.KindVarKeyword:
		return p.parseVariableStatement(p.nodePos(), p.jsdocScannerInfo(), 0)
	case ast.KindLetKeyword:
		if p.isLetDeclaration() {
			return p.parseVariableStatement(p.nodePos(), p.jsdocScannerInfo(), 0)
		}
	case ast.KindAwaitKeyword:
		if p.isAwaitUsingDeclaration() {
			return p.parseVariableStatement(p.nodePos(), p.jsdocScannerInfo(), 0)
		}
	case ast.KindUsingKeyword:
		if p.isUsingDeclaration() {
			return p.parseVariableStatement(p.nodePos(), p.jsdocScannerInfo(), 0)
		}
	case ast.KindFunctionKeyword:
		return p.parseFunctionDeclaration(p.nodePos(), p.jsdocScannerInfo(), 0)
	case ast.KindClassKeyword:
		return p.parseClassDeclaration(p.nodePos(), p.jsdocScannerInfo(), 0)
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

func (p *handleParser) parseDeclaration() ast.Handle {
	// `parseListElement` attempted to get the reused node at this position,
	// but the ambient context flag was not yet set, so the node appeared
	// not reusable in that context.
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	modifiers := p.parseModifiersEx( /*allowDecorators*/ true, false /*permitConstAsModifier*/, false /*stopOnStartOfClassStaticBlock*/)
	isAmbient := modifiers != 0 && p.listSome(modifiers, handleIsDeclare)
	if isAmbient {
		// !!! incremental parsing
		// node := p.tryReuseAmbientDeclaration(pos)
		// if node {
		// 	return node
		// }
		for _, m := range p.listHandles(modifiers) {
			m.SetFlags(m.Flags() | ast.NodeFlagsAmbient)
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

func (p *handleParser) parseDeclarationWorker(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
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
	if modifiers != 0 {
		// We reached this point because we encountered decorators and/or modifiers and assumed a declaration
		// would follow. For recovery and error reporting purposes, return an incomplete declaration.
		p.parseErrorAt(p.nodePos(), p.nodePos(), diagnostics.Declaration_expected)
		return p.finishHandle(p.f.NewMissingDeclaration(modifiers), pos)
	}
	panic("Unhandled case in parseDeclarationWorker")
}

func (p *handleParser) parseBlock(ignoreMissingOpenBrace bool, diagnosticMessage *diagnostics.Message) ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	openBracePosition := p.scanner.TokenStart()
	openBraceParsed := p.parseExpectedWithDiagnostic(ast.KindOpenBraceToken, diagnosticMessage, true /*shouldAdvance*/)
	multiline := false
	if openBraceParsed || ignoreMissingOpenBrace {
		multiline = p.hasPrecedingLineBreak()
		statements := p.parseList(PCBlockStatements, (*handleParser).parseStatement)
		p.parseExpectedMatchingBrackets(ast.KindOpenBraceToken, ast.KindCloseBraceToken, openBraceParsed, openBracePosition)
		result := p.finishHandle(p.f.NewBlock(statements, multiline), pos)
		p.withJSDocHandle(result, jsdoc)
		if p.token == ast.KindEqualsToken {
			p.parseErrorAtCurrentToken(diagnostics.Declaration_or_statement_expected_This_follows_a_block_of_statements_so_if_you_intended_to_write_a_destructuring_assignment_you_might_need_to_wrap_the_whole_assignment_in_parentheses)
			p.nextToken()
		}
		return result
	}
	result := p.finishHandle(p.f.NewBlock(p.createMissingListHandle(), multiline), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseEmptyStatement() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindSemicolonToken)
	result := p.finishHandle(p.f.NewEmptyStatement(), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseIfStatement() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindIfKeyword)
	openParenPosition := p.scanner.TokenStart()
	openParenParsed := p.parseExpected(ast.KindOpenParenToken)
	expression := p.parseExpressionAllowIn()
	p.parseExpectedMatchingBrackets(ast.KindOpenParenToken, ast.KindCloseParenToken, openParenParsed, openParenPosition)
	thenStatement := p.parseStatement()
	var elseStatement ast.Handle
	if p.parseOptional(ast.KindElseKeyword) {
		elseStatement = p.parseStatement()
	}
	result := p.finishHandle(p.f.NewIfStatement(expression, thenStatement, elseStatement), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseDoStatement() ast.Handle {
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
	result := p.finishHandle(p.f.NewDoStatement(statement, expression), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseWhileStatement() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindWhileKeyword)
	openParenPosition := p.scanner.TokenStart()
	openParenParsed := p.parseExpected(ast.KindOpenParenToken)
	expression := p.parseExpressionAllowIn()
	p.parseExpectedMatchingBrackets(ast.KindOpenParenToken, ast.KindCloseParenToken, openParenParsed, openParenPosition)
	statement := p.parseStatement()
	result := p.finishHandle(p.f.NewWhileStatement(expression, statement), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseForOrForInOrForOfStatement() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindForKeyword)
	awaitToken := p.parseOptionalTokenHandle(ast.KindAwaitKeyword)
	p.parseExpected(ast.KindOpenParenToken)
	var initializer ast.Handle
	if p.token != ast.KindSemicolonToken {
		if p.token == ast.KindVarKeyword || p.token == ast.KindLetKeyword || p.token == ast.KindConstKeyword ||
			p.token == ast.KindUsingKeyword && p.lookAhead((*Parser).nextTokenIsBindingIdentifierOrStartOfDestructuringOnSameLineDisallowOf) ||
			// this one is meant to allow of
			p.token == ast.KindAwaitKeyword && p.lookAhead((*Parser).nextIsUsingKeywordThenBindingIdentifierOrStartOfObjectDestructuringOnSameLine) {
			initializer = p.parseVariableDeclarationList(true /*inForStatementInitializer*/)
		} else {
			initializer = p.inContext(ast.NodeFlagsDisallowInContext, true, (*handleParser).parseExpression)
		}
	}
	var result ast.Handle
	switch {
	case !awaitToken.IsNil() && p.parseExpected(ast.KindOfKeyword) || awaitToken.IsNil() && p.parseOptional(ast.KindOfKeyword):
		expression := p.inContext(ast.NodeFlagsDisallowInContext, false, (*handleParser).parseAssignmentExpressionOrHigher)
		p.parseExpected(ast.KindCloseParenToken)
		result = p.f.NewForInOrOfStatement(ast.KindForOfStatement, awaitToken, initializer, expression, p.parseStatement())
	case p.parseOptional(ast.KindInKeyword):
		expression := p.parseExpressionAllowIn()
		p.parseExpected(ast.KindCloseParenToken)
		result = p.f.NewForInOrOfStatement(ast.KindForInStatement, ast.Handle{}, initializer, expression, p.parseStatement())
	default:
		p.parseExpected(ast.KindSemicolonToken)
		var condition ast.Handle
		if p.token != ast.KindSemicolonToken && p.token != ast.KindCloseParenToken {
			condition = p.parseExpressionAllowIn()
		}
		p.parseExpected(ast.KindSemicolonToken)
		var incrementor ast.Handle
		if p.token != ast.KindCloseParenToken {
			incrementor = p.parseExpressionAllowIn()
		}
		p.parseExpected(ast.KindCloseParenToken)
		result = p.f.NewForStatement(initializer, condition, incrementor, p.parseStatement())
	}
	p.finishHandle(result, pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseBreakStatement() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindBreakKeyword)
	label := p.parseIdentifierUnlessAtSemicolon()
	p.parseSemicolon()
	result := p.finishHandle(p.f.NewBreakStatement(label), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseContinueStatement() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindContinueKeyword)
	label := p.parseIdentifierUnlessAtSemicolon()
	p.parseSemicolon()
	result := p.finishHandle(p.f.NewContinueStatement(label), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseIdentifierUnlessAtSemicolon() ast.Handle {
	if !p.canParseSemicolon() {
		return p.parseIdentifier()
	}
	return ast.Handle{}
}

func (p *handleParser) parseReturnStatement() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindReturnKeyword)
	var expression ast.Handle
	if !p.canParseSemicolon() {
		expression = p.parseExpressionAllowIn()
	}
	p.parseSemicolon()
	result := p.finishHandle(p.f.NewReturnStatement(expression), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseWithStatement() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindWithKeyword)
	openParenPosition := p.scanner.TokenStart()
	openParenParsed := p.parseExpected(ast.KindOpenParenToken)
	expression := p.parseExpressionAllowIn()
	p.parseExpectedMatchingBrackets(ast.KindOpenParenToken, ast.KindCloseParenToken, openParenParsed, openParenPosition)
	statement := p.inContext(ast.NodeFlagsInWithStatement, true, (*handleParser).parseStatement)
	result := p.finishHandle(p.f.NewWithStatement(expression, statement), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseCaseClause() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindCaseKeyword)
	expression := p.parseExpressionAllowIn()
	p.parseExpected(ast.KindColonToken)
	statements := p.parseList(PCSwitchClauseStatements, (*handleParser).parseStatement)
	result := p.finishHandle(p.f.NewCaseOrDefaultClause(ast.KindCaseClause, expression, statements), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseDefaultClause() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindDefaultKeyword)
	p.parseExpected(ast.KindColonToken)
	statements := p.parseList(PCSwitchClauseStatements, (*handleParser).parseStatement)
	result := p.finishHandle(p.f.NewCaseOrDefaultClause(ast.KindDefaultClause, ast.Handle{}, statements), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseCaseOrDefaultClause() ast.Handle {
	if p.token == ast.KindCaseKeyword {
		return p.parseCaseClause()
	}
	return p.parseDefaultClause()
}

func (p *handleParser) parseCaseBlock() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindOpenBraceToken)
	clauses := p.parseList(PCSwitchClauses, (*handleParser).parseCaseOrDefaultClause)
	p.parseExpected(ast.KindCloseBraceToken)
	result := p.finishHandle(p.f.NewCaseBlock(clauses), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseSwitchStatement() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindSwitchKeyword)
	p.parseExpected(ast.KindOpenParenToken)
	expression := p.parseExpressionAllowIn()
	p.parseExpected(ast.KindCloseParenToken)
	caseBlock := p.parseCaseBlock()
	result := p.finishHandle(p.f.NewSwitchStatement(expression, caseBlock), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseThrowStatement() ast.Handle {
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
	var expression ast.Handle
	if !p.hasPrecedingLineBreak() {
		expression = p.parseExpressionAllowIn()
	} else {
		expression = p.createMissingIdentifierHandle()
	}
	if !p.tryParseSemicolon() {
		p.parseErrorForMissingSemicolonAfter(expression)
	}
	result := p.finishHandle(p.f.NewThrowStatement(expression), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseTryStatement() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindTryKeyword)
	tryBlock := p.parseBlock(false /*ignoreMissingOpenBrace*/, nil)
	var catchClause ast.Handle
	if p.token == ast.KindCatchKeyword {
		catchClause = p.parseCatchClause()
	}
	// If we don't have a catch clause, then we must have a finally clause.  Try to parse
	// one out no matter what.
	var finallyBlock ast.Handle
	if catchClause.IsNil() || p.token == ast.KindFinallyKeyword {
		p.parseExpectedWithDiagnostic(ast.KindFinallyKeyword, diagnostics.X_catch_or_finally_expected, true /*shouldAdvance*/)
		finallyBlock = p.parseBlock(false /*ignoreMissingOpenBrace*/, nil)
	}
	result := p.finishHandle(p.f.NewTryStatement(tryBlock, catchClause, finallyBlock), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseCatchClause() ast.Handle {
	pos := p.nodePos()
	p.parseExpected(ast.KindCatchKeyword)
	var variableDeclaration ast.Handle
	if p.parseOptional(ast.KindOpenParenToken) {
		variableDeclaration = p.parseVariableDeclaration()
		p.parseExpected(ast.KindCloseParenToken)
	}
	block := p.parseBlock(false /*ignoreMissingOpenBrace*/, nil)
	result := p.finishHandle(p.f.NewCatchClause(variableDeclaration, block), pos)
	return result
}

func (p *handleParser) parseDebuggerStatement() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindDebuggerKeyword)
	p.parseSemicolon()
	result := p.finishHandle(p.f.NewDebuggerStatement(), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseExpressionOrLabeledStatement() ast.Handle {
	// Avoiding having to do the lookahead for a labeled statement by just trying to parse
	// out an expression, seeing if it is identifier and then seeing if it is followed by
	// a colon.
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	hasParen := p.token == ast.KindOpenParenToken
	expression := p.parseExpression()

	if expression.Kind() == ast.KindIdentifier && p.parseOptional(ast.KindColonToken) {
		result := p.finishHandle(p.f.NewLabeledStatement(expression, p.parseStatement()), pos)
		p.withJSDocHandle(result, jsdoc)
		return result
	}

	if !p.tryParseSemicolon() {
		p.parseErrorForMissingSemicolonAfter(expression)
	}
	result := p.finishHandle(p.f.NewExpressionStatement(expression), pos)
	if hasParen {
		jsdoc &^= jsdocScannerInfoHasJSDoc
	}
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseVariableStatement(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
	declarationList := p.parseVariableDeclarationList(false /*inForStatementInitializer*/)
	p.parseSemicolon()
	result := p.finishHandle(p.f.NewVariableStatement(modifiers, declarationList), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseVariableDeclarationList(inForStatementInitializer bool) ast.Handle {
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
	var declarations ast.ListRef
	if p.token == ast.KindOfKeyword && p.lookAhead((*Parser).nextIsIdentifierAndCloseParen) {
		declarations = p.createMissingListHandle()
	} else {
		saveContextFlags := p.contextFlags
		p.setContextFlags(ast.NodeFlagsDisallowInContext, inForStatementInitializer)
		declarations = p.parseDelimitedList(PCVariableDeclarations, core.IfElse(inForStatementInitializer, (*handleParser).parseVariableDeclaration, (*handleParser).parseVariableDeclarationAllowExclamation))
		p.contextFlags = saveContextFlags
	}
	result := p.finishHandle(p.f.NewVariableDeclarationList(declarations, flags), pos)
	return result
}

func (p *handleParser) parseVariableDeclaration() ast.Handle {
	return p.parseVariableDeclarationWorker(false /*allowExclamation*/)
}

func (p *handleParser) parseVariableDeclarationAllowExclamation() ast.Handle {
	return p.parseVariableDeclarationWorker(true /*allowExclamation*/)
}

func (p *handleParser) parseVariableDeclarationWorker(allowExclamation bool) ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	name := p.parseIdentifierOrPatternWithDiagnostic(diagnostics.Private_identifiers_are_not_allowed_in_variable_declarations)
	var exclamationToken ast.Handle
	if allowExclamation && name.Kind() == ast.KindIdentifier && p.token == ast.KindExclamationToken && !p.hasPrecedingLineBreak() {
		exclamationToken = p.parseTokenHandle()
	}
	typeNode := p.parseTypeAnnotation()
	var initializer ast.Handle
	if p.token != ast.KindInKeyword && p.token != ast.KindOfKeyword {
		initializer = p.parseInitializer()
	}
	result := p.finishHandle(p.f.NewVariableDeclaration(name, exclamationToken, typeNode, initializer), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseIdentifierOrPattern() ast.Handle {
	return p.parseIdentifierOrPatternWithDiagnostic(nil)
}

func (p *handleParser) parseIdentifierOrPatternWithDiagnostic(privateIdentifierDiagnosticMessage *diagnostics.Message) ast.Handle {
	if p.token == ast.KindOpenBracketToken {
		return p.parseArrayBindingPattern()
	}
	if p.token == ast.KindOpenBraceToken {
		return p.parseObjectBindingPattern()
	}
	return p.parseBindingIdentifierWithDiagnostic(privateIdentifierDiagnosticMessage)
}

func (p *handleParser) parseArrayBindingPattern() ast.Handle {
	pos := p.nodePos()
	p.parseExpected(ast.KindOpenBracketToken)
	saveContextFlags := p.contextFlags
	p.setContextFlags(ast.NodeFlagsDisallowInContext, false)
	elements := p.parseDelimitedList(PCArrayBindingElements, (*handleParser).parseArrayBindingElement)
	p.contextFlags = saveContextFlags
	p.parseExpected(ast.KindCloseBracketToken)
	return p.finishHandle(p.f.NewBindingPattern(ast.KindArrayBindingPattern, elements), pos)
}

func (p *handleParser) parseArrayBindingElement() ast.Handle {
	pos := p.nodePos()
	var dotDotDotToken ast.Handle
	var name ast.Handle
	var initializer ast.Handle
	if p.token != ast.KindCommaToken {
		// These are all nil for a missing element
		dotDotDotToken = p.parseOptionalTokenHandle(ast.KindDotDotDotToken)
		name = p.parseIdentifierOrPattern()
		initializer = p.parseInitializer()
	}
	return p.finishHandle(p.f.NewBindingElement(dotDotDotToken, ast.Handle{}, name, initializer), pos)
}

func (p *handleParser) parseObjectBindingPattern() ast.Handle {
	pos := p.nodePos()
	p.parseExpected(ast.KindOpenBraceToken)
	saveContextFlags := p.contextFlags
	p.setContextFlags(ast.NodeFlagsDisallowInContext, false)
	elements := p.parseDelimitedList(PCObjectBindingElements, (*handleParser).parseObjectBindingElement)
	p.contextFlags = saveContextFlags
	p.parseExpected(ast.KindCloseBraceToken)
	return p.finishHandle(p.f.NewBindingPattern(ast.KindObjectBindingPattern, elements), pos)
}

func (p *handleParser) parseObjectBindingElement() ast.Handle {
	pos := p.nodePos()
	dotDotDotToken := p.parseOptionalTokenHandle(ast.KindDotDotDotToken)
	tokenIsIdentifier := p.isBindingIdentifier()
	propertyName := p.parsePropertyName()
	var name ast.Handle
	if tokenIsIdentifier && p.token != ast.KindColonToken {
		name = propertyName
		propertyName = ast.Handle{}
	} else {
		p.parseExpected(ast.KindColonToken)
		name = p.parseIdentifierOrPattern()
	}
	initializer := p.parseInitializer()
	return p.finishHandle(p.f.NewBindingElement(dotDotDotToken, propertyName, name, initializer), pos)
}

func (p *handleParser) parseInitializer() ast.Handle {
	if p.parseOptional(ast.KindEqualsToken) {
		return p.parseAssignmentExpressionOrHigher()
	}
	return ast.Handle{}
}

func (p *handleParser) parseTypeAnnotation() ast.Handle {
	if p.parseOptional(ast.KindColonToken) {
		return p.parseType()
	}
	return ast.Handle{}
}

func (p *handleParser) parseFunctionDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
	p.parseExpected(ast.KindFunctionKeyword)
	asteriskToken := p.parseOptionalTokenHandle(ast.KindAsteriskToken)
	// We don't parse the name here in await context, instead we will report a grammar error in the checker.
	var name ast.Handle
	if modifiers == 0 || p.modifiersToFlags(modifiers)&ast.ModifierFlagsDefault == 0 || p.isBindingIdentifier() {
		name = p.parseBindingIdentifier()
	}
	signatureFlags := core.IfElse(!asteriskToken.IsNil(), ParseFlagsYield, ParseFlagsNone) | core.IfElse(modifiers != 0 && p.modifiersToFlags(modifiers)&ast.ModifierFlagsAsync != 0, ParseFlagsAwait, ParseFlagsNone)
	typeParameters := p.parseTypeParameters()
	saveContextFlags := p.contextFlags
	if modifiers != 0 && p.modifiersToFlags(modifiers)&ast.ModifierFlagsExport != 0 {
		p.setContextFlags(ast.NodeFlagsAwaitContext, true)
	}
	parameters := p.parseParameters(signatureFlags)
	returnType := p.parseReturnType(ast.KindColonToken, false /*isType*/)
	body := p.parseFunctionBlockOrSemicolon(signatureFlags, diagnostics.X_or_expected)
	p.contextFlags = saveContextFlags
	result := p.finishHandle(p.f.NewFunctionDeclaration(modifiers, asteriskToken, name, typeParameters, parameters, returnType, ast.Handle{}, body), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseClassDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
	return p.parseClassDeclarationOrExpression(pos, jsdoc, modifiers, ast.KindClassDeclaration)
}

func (p *handleParser) parseClassExpression() ast.Handle {
	return p.parseClassDeclarationOrExpression(p.nodePos(), p.jsdocScannerInfo(), 0, ast.KindClassExpression)
}

func (p *handleParser) parseClassDeclarationOrExpression(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef, kind ast.Kind) ast.Handle {
	saveContextFlags := p.contextFlags
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	p.parseExpected(ast.KindClassKeyword)
	// We don't parse the name here in await context, instead we will report a grammar error in the checker.
	name := p.parseNameOfClassDeclarationOrExpression()
	typeParameters := p.parseTypeParameters()
	if modifiers != 0 &&
		p.parsingContexts&(1<<PCSourceElements) != 0 &&
		p.parsingContexts&((1<<PCBlockStatements)|(1<<PCSwitchClauseStatements)) == 0 &&
		p.listSome(modifiers, handleIsExport) {
		p.setContextFlags(ast.NodeFlagsAwaitContext, true /*value*/)
	}
	heritageClauses := p.parseHeritageClauses(false /*isInterface*/)
	var members ast.ListRef
	if p.parseExpected(ast.KindOpenBraceToken) {
		// ClassTail[Yield,Await] : (Modified) See 14.5
		//      ClassHeritage[?Yield,?Await]opt { ClassBody[?Yield,?Await]opt }
		members = p.parseList(PCClassMembers, (*handleParser).parseClassElement)
		p.parseExpected(ast.KindCloseBraceToken)
	} else {
		members = p.createMissingListHandle()
	}
	p.contextFlags = saveContextFlags
	var result ast.Handle
	if modifiers != 0 && p.modifiersToFlags(modifiers)&ast.ModifierFlagsAmbient != 0 {
		p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	}
	if kind == ast.KindClassDeclaration {
		result = p.f.NewClassDeclaration(modifiers, name, typeParameters, heritageClauses, members)
	} else {
		result = p.f.NewClassExpression(modifiers, name, typeParameters, heritageClauses, members)
	}
	p.finishHandle(result, pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseNameOfClassDeclarationOrExpression() ast.Handle {
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
	return ast.Handle{}
}

func (p *handleParser) parseHeritageClauses(isInterface bool) ast.ListRef {
	// ClassTail[Yield,Await] : (Modified) See 14.5
	//      ClassHeritage[?Yield,?Await]opt { ClassBody[?Yield,?Await]opt }
	if p.isHeritageClause() {
		return p.parseList(PCHeritageClauses, func(p *handleParser) ast.Handle {
			return p.parseHeritageClause(isInterface)
		})
	}
	return 0
}

func (p *handleParser) parseHeritageClause(isInterface bool) ast.Handle {
	pos := p.nodePos()
	kind := p.token
	p.nextToken()
	parseElement := (*handleParser).parseExpressionWithTypeArguments
	if isTypeHeritageClause(isInterface, kind) {
		parseElement = (*handleParser).parseTypeHeritageClauseElement
	}
	types := p.parseDelimitedList(PCHeritageClauseElement, parseElement)
	return (p.finishHandle(p.f.NewHeritageClause(kind, types), pos))
}

func (p *handleParser) parseTypeHeritageClauseElement() ast.Handle {
	pos := p.nodePos()
	expressionWithTypeArguments := p.parseExpressionWithTypeArguments()
	if !handleIsValidHeritageTypeReferenceExpression(expressionWithTypeArguments.ExpressionWithTypeArgumentsExpression()) {
		return expressionWithTypeArguments
	}
	typeName := p.convertEntityNameExpressionToEntityName(expressionWithTypeArguments.ExpressionWithTypeArgumentsExpression())
	return p.finishHandle(p.f.NewTypeReferenceNode(typeName, expressionWithTypeArguments.ExpressionWithTypeArgumentsTypeArguments()), pos)
}

func (p *handleParser) convertEntityNameExpressionToEntityName(node ast.Handle) ast.Handle {
	if node.Kind() == ast.KindIdentifier {
		return node
	}
	result := p.f.NewQualifiedName(
		p.convertEntityNameExpressionToEntityName(node.PropertyAccessExpressionExpression()),
		node.PropertyAccessExpressionName(),
	)
	return p.finishHandleWithEnd(result, node.Loc().Pos(), node.Loc().End())
}

func (p *handleParser) parseExpressionWithTypeArguments() ast.Handle {
	pos := p.nodePos()
	expression := p.parseLeftHandSideExpressionOrHigher()
	if expression.Kind() == ast.KindExpressionWithTypeArguments {
		return expression
	}
	typeArguments := p.parseTypeArguments()
	return p.finishHandle(p.f.NewExpressionWithTypeArguments(expression, typeArguments), pos)
}

func (p *handleParser) parseClassElement() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	if p.token == ast.KindSemicolonToken {
		p.nextToken()
		result := p.finishHandle(p.f.NewSemicolonClassElement(), pos)
		p.withJSDocHandle(result, jsdoc)
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
		if !constructorDeclaration.IsNil() {
			return constructorDeclaration
		}
	}
	if p.isIndexSignature() {
		return (p.parseIndexSignatureDeclaration(pos, jsdoc, modifiers))
	}
	// It is very important that we check this *after* checking indexers because
	// the [ token can start an index signature or a computed property name
	if tokenIsIdentifierOrKeyword(p.token) || p.token == ast.KindStringLiteral || p.token == ast.KindNumericLiteral || p.token == ast.KindBigIntLiteral || p.token == ast.KindAsteriskToken || p.token == ast.KindOpenBracketToken {
		isAmbient := modifiers != 0 && p.listSome(modifiers, handleIsDeclare)
		if isAmbient {
			for _, m := range p.listHandles(modifiers) {
				m.SetFlags(m.Flags() | ast.NodeFlagsAmbient)
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
	if modifiers != 0 {
		// treat this as a property declaration with a missing name.
		p.parseErrorAt(p.nodePos(), p.nodePos(), diagnostics.Declaration_expected)
		name := p.createMissingIdentifierHandle()
		return p.parsePropertyDeclaration(pos, jsdoc, modifiers, name, ast.Handle{})
	}
	// 'isClassMemberStart' should have hinted not to attempt parsing.
	panic("Should not have attempted to parse class member declaration.")
}

func (p *handleParser) parseClassStaticBlockDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
	p.parseExpectedTokenHandle(ast.KindStaticKeyword)
	body := p.parseClassStaticBlockBody()
	result := p.finishHandle(p.f.NewClassStaticBlockDeclaration(modifiers, body), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseClassStaticBlockBody() ast.Handle {
	saveContextFlags := p.contextFlags
	p.setContextFlags(ast.NodeFlagsYieldContext, false)
	p.setContextFlags(ast.NodeFlagsAwaitContext, true)
	body := p.parseBlock(false /*ignoreMissingOpenBrace*/, nil /*diagnosticMessage*/)
	p.contextFlags = saveContextFlags
	return body
}

func (p *handleParser) tryParseConstructorDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
	state := p.mark()
	if p.token == ast.KindConstructorKeyword || p.token == ast.KindStringLiteral && p.scanner.TokenValue() == "constructor" && p.lookAhead((*Parser).nextTokenIsOpenParen) {
		p.nextToken()
		typeParameters := p.parseTypeParameters()
		parameters := p.parseParameters(ParseFlagsNone)
		returnType := p.parseReturnType(ast.KindColonToken, false /*isType*/)
		body := p.parseFunctionBlockOrSemicolon(ParseFlagsNone, diagnostics.X_or_expected)
		result := p.finishHandle(p.f.NewConstructorDeclaration(modifiers, typeParameters, parameters, returnType, ast.Handle{}, body), pos)
		p.withJSDocHandle(result, jsdoc)
		return result
	}
	p.rewind(state)
	return ast.Handle{}
}

func (p *handleParser) parsePropertyOrMethodDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
	asteriskToken := p.parseOptionalTokenHandle(ast.KindAsteriskToken)
	name := p.parsePropertyName()
	// Note: this is not legal as per the grammar.  But we allow it in the parser and
	// report an error in the grammar checker.
	questionToken := p.parseOptionalTokenHandle(ast.KindQuestionToken)
	if !asteriskToken.IsNil() || p.token == ast.KindOpenParenToken || p.token == ast.KindLessThanToken {
		return p.parseMethodDeclaration(pos, jsdoc, modifiers, asteriskToken, name, questionToken, diagnostics.X_or_expected)
	}
	return p.parsePropertyDeclaration(pos, jsdoc, modifiers, name, questionToken)
}

func (p *handleParser) parseMethodDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef, asteriskToken ast.Handle, name ast.Handle, questionToken ast.Handle, diagnosticMessage *diagnostics.Message) ast.Handle {
	signatureFlags := core.IfElse(!asteriskToken.IsNil(), ParseFlagsYield, ParseFlagsNone) | core.IfElse(p.listSome(modifiers, handleIsAsync), ParseFlagsAwait, ParseFlagsNone)
	typeParameters := p.parseTypeParameters()
	parameters := p.parseParameters(signatureFlags)
	typeNode := p.parseReturnType(ast.KindColonToken, false /*isType*/)
	body := p.parseFunctionBlockOrSemicolon(signatureFlags, diagnosticMessage)
	result := p.finishHandle(p.f.NewMethodDeclaration(modifiers, asteriskToken, name, questionToken, typeParameters, parameters, typeNode, ast.Handle{}, body), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parsePropertyDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef, name ast.Handle, questionToken ast.Handle) ast.Handle {
	postfixToken := questionToken
	if postfixToken.IsNil() && !p.hasPrecedingLineBreak() {
		postfixToken = p.parseOptionalTokenHandle(ast.KindExclamationToken)
	}
	typeNode := p.parseTypeAnnotation()
	initializer := p.inContext(ast.NodeFlagsYieldContext|ast.NodeFlagsAwaitContext|ast.NodeFlagsDisallowInContext, false, (*handleParser).parseInitializer)
	p.parseSemicolonAfterPropertyName(name, typeNode, initializer)
	result := p.finishHandle(p.f.NewPropertyDeclaration(modifiers, name, postfixToken, typeNode, initializer), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseSemicolonAfterPropertyName(name ast.Handle, typeNode ast.Handle, initializer ast.Handle) {
	if p.token == ast.KindAtToken && !p.hasPrecedingLineBreak() {
		p.parseErrorAtCurrentToken(diagnostics.Decorators_must_precede_the_name_and_all_keywords_of_property_declarations)
		return
	}
	if p.token == ast.KindOpenParenToken {
		p.parseErrorAtCurrentToken(diagnostics.Cannot_start_a_function_call_in_a_type_annotation)
		p.nextToken()
		return
	}
	if !typeNode.IsNil() && !p.canParseSemicolon() {
		if !initializer.IsNil() {
			p.parseErrorAtCurrentToken(diagnostics.X_0_expected, scanner.TokenToString(ast.KindSemicolonToken))
		} else {
			p.parseErrorAtCurrentToken(diagnostics.Expected_for_property_initializer)
		}
		return
	}
	if p.tryParseSemicolon() {
		return
	}
	if !initializer.IsNil() {
		p.parseErrorAtCurrentToken(diagnostics.X_0_expected, scanner.TokenToString(ast.KindSemicolonToken))
		return
	}
	p.parseErrorForMissingSemicolonAfter(name)
}

func (p *handleParser) parseErrorForMissingSemicolonAfter(node ast.Handle) {
	// Tagged template literals are sometimes used in places where only simple strings are allowed, i.e.:
	//   module `M1` {
	//   ^^^^^^^^^^^ This block is parsed as a template literal like module`M1`.
	if node.Kind() == ast.KindTaggedTemplateExpression {
		p.parseErrorAtRange(p.skipRangeTrivia(node.TaggedTemplateExpressionTemplate().Loc()), diagnostics.Module_declaration_names_may_only_use_or_quoted_strings)
		return
	}
	// Otherwise, if this isn't a well-known keyword-like identifier, give the generic fallback message.
	var expressionText string
	if node.Kind() == ast.KindIdentifier {
		expressionText = handleText(node)
	}
	if expressionText == "" {
		p.parseErrorAtCurrentToken(diagnostics.X_0_expected, scanner.TokenToString(ast.KindSemicolonToken))
		return
	}
	pos := scanner.SkipTrivia(p.sourceText, node.Loc().Pos())
	// Some known keywords are likely signs of syntax being used improperly.
	switch expressionText {
	case "const", "let", "var":
		p.parseErrorAt(pos, node.Loc().End(), diagnostics.Variable_declaration_not_allowed_at_this_location)
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
		p.parseErrorAt(pos, node.Loc().End(), diagnostics.Unknown_keyword_or_identifier_Did_you_mean_0, suggestion)
		return
	}
	// Unknown tokens are handled with their own errors in the scanner
	if p.token == ast.KindUnknown {
		return
	}
	// Otherwise, we know this some kind of unknown word, not just a missing expected semicolon.
	p.parseErrorAt(pos, node.Loc().End(), diagnostics.Unexpected_keyword_or_identifier)
}

func (p *handleParser) parseInterfaceDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
	p.parseExpected(ast.KindInterfaceKeyword)
	name := p.parseIdentifier()
	typeParameters := p.parseTypeParameters()
	heritageClauses := p.parseHeritageClauses(true /*isInterface*/)
	members := p.parseObjectTypeMembers()
	result := p.finishHandle(p.f.NewInterfaceDeclaration(modifiers, name, typeParameters, heritageClauses, members), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseTypeAliasDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
	p.parseExpected(ast.KindTypeKeyword)
	if p.hasPrecedingLineBreak() {
		p.parseErrorAtCurrentToken(diagnostics.Line_break_not_permitted_here)
	}
	name := p.parseIdentifier()
	typeParameters := p.parseTypeParameters()
	p.parseExpected(ast.KindEqualsToken)
	var typeNode ast.Handle
	if p.token == ast.KindIntrinsicKeyword && p.lookAhead((*Parser).nextIsNotDot) {
		typeNode = p.parseKeywordTypeNode()
	} else {
		typeNode = p.parseType()
	}
	p.parseSemicolon()
	result := p.finishHandle(p.f.NewTypeAliasDeclaration(modifiers, name, typeParameters, typeNode), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseEnumMember() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	name := p.parsePropertyName()
	initializer := p.inContext(ast.NodeFlagsDisallowInContext, false, (*handleParser).parseInitializer)
	result := p.finishHandle(p.f.NewEnumMember(name, initializer), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseEnumDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	p.parseExpected(ast.KindEnumKeyword)
	name := p.parseIdentifier()
	var members ast.ListRef
	if p.parseExpected(ast.KindOpenBraceToken) {
		saveContextFlags := p.contextFlags
		p.setContextFlags(ast.NodeFlagsYieldContext|ast.NodeFlagsAwaitContext, false)
		members = p.parseDelimitedList(PCEnumMembers, (*handleParser).parseEnumMember)
		p.contextFlags = saveContextFlags
		p.parseExpected(ast.KindCloseBraceToken)
	} else {
		members = p.createMissingListHandle()
	}
	result := p.finishHandle(p.f.NewEnumDeclaration(modifiers, name, members), pos)
	p.withJSDocHandle(result, jsdoc)
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	return result
}

func (p *handleParser) parseModuleDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
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

func (p *handleParser) parseAmbientExternalModuleDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
	var name ast.Handle
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
	var body ast.Handle
	if p.token == ast.KindOpenBraceToken {
		body = p.parseModuleBlock()
	} else {
		p.parseSemicolon()
	}
	result := p.finishHandle(p.f.NewModuleDeclaration(modifiers, keyword, name, body), pos)
	p.withJSDocHandle(result, jsdoc)
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	return result
}

func (p *handleParser) parseModuleBlock() ast.Handle {
	pos := p.nodePos()
	var statements ast.ListRef
	if p.parseExpected(ast.KindOpenBraceToken) {
		statements = p.parseList(PCBlockStatements, (*handleParser).parseStatement)
		p.parseExpected(ast.KindCloseBraceToken)
	} else {
		statements = p.createMissingListHandle()
	}
	return p.finishHandle(p.f.NewModuleBlock(statements), pos)
}

func (p *handleParser) parseModuleOrNamespaceDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef, nested bool, keyword ast.Kind) ast.Handle {
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	var name ast.Handle
	if nested {
		name = p.parseIdentifierName()
	} else {
		name = p.parseIdentifier()
	}
	var body ast.Handle
	if p.parseOptional(ast.KindDotToken) {
		implicitExport := p.f.NewToken(ast.KindExportKeyword)
		implicitExport.SetLoc(core.NewTextRange(p.nodePos(), p.nodePos()))
		implicitExport.SetFlags(ast.NodeFlagsReparsed)
		implicitModifiers := p.newList(implicitExport.Loc(), []ast.Handle{implicitExport})
		body = p.parseModuleOrNamespaceDeclaration(p.nodePos(), 0 /*jsdoc*/, implicitModifiers, true /*nested*/, keyword)
	} else {
		body = p.parseModuleBlock()
	}
	result := p.finishHandle(p.f.NewModuleDeclaration(modifiers, keyword, name, body), pos)
	p.withJSDocHandle(result, jsdoc)
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	return result
}

func (p *handleParser) parseImportDeclarationOrImportEqualsDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
	p.parseExpected(ast.KindImportKeyword)
	afterImportPos := p.nodePos()
	// We don't parse the identifier here in await context, instead we will report a grammar error in the checker.
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	var identifier ast.Handle
	if p.isIdentifier() {
		identifier = p.parseIdentifier()
	}
	phaseModifier := ast.KindUnknown
	if !identifier.IsNil() && handleText(identifier) == "type" &&
		(p.token != ast.KindFromKeyword || p.isIdentifier() && p.lookAhead((*Parser).nextTokenIsFromKeywordOrEqualsToken)) &&
		(p.isIdentifier() || p.tokenAfterImportDefinitelyProducesImportDeclaration()) {
		phaseModifier = ast.KindTypeKeyword
		identifier = ast.Handle{}
		if p.isIdentifier() {
			identifier = p.parseIdentifier()
		}
	} else if !identifier.IsNil() && handleText(identifier) == "defer" {
		var shouldParseAsDeferModifier bool
		if p.token == ast.KindFromKeyword {
			shouldParseAsDeferModifier = !p.lookAhead((*Parser).nextTokenIsTokenStringLiteral)
		} else {
			shouldParseAsDeferModifier = p.token != ast.KindCommaToken && p.token != ast.KindEqualsToken
		}
		if shouldParseAsDeferModifier {
			phaseModifier = ast.KindDeferKeyword
			identifier = ast.Handle{}
			if p.isIdentifier() {
				identifier = p.parseIdentifier()
			}
		}
	}
	if !identifier.IsNil() && !p.tokenAfterImportedIdentifierDefinitelyProducesImportDeclaration() && phaseModifier != ast.KindDeferKeyword {
		importEquals := (p.parseImportEqualsDeclaration(pos, jsdoc, modifiers, identifier, phaseModifier == ast.KindTypeKeyword))
		p.statementHasAwaitIdentifier = saveHasAwaitIdentifier // Import= declaration is always parsed in an Await context, no need to reparse
		return importEquals
	}
	importClause := p.tryParseImportClause(identifier, afterImportPos, phaseModifier, false /*skipJSDocLeadingAsterisks*/)
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier // import clause is always parsed in an Await context
	moduleSpecifier := p.parseModuleSpecifier()
	attributes := p.tryParseImportAttributes()
	p.parseSemicolon()
	result := p.finishHandle(p.f.NewImportDeclaration(modifiers, importClause, moduleSpecifier, attributes), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseImportEqualsDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef, identifier ast.Handle, isTypeOnly bool) ast.Handle {
	p.parseExpected(ast.KindEqualsToken)
	moduleReference := p.parseModuleReference()
	p.parseSemicolon()
	result := p.finishHandle(p.f.NewImportEqualsDeclaration(modifiers, isTypeOnly, identifier, moduleReference), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseModuleReference() ast.Handle {
	if p.token == ast.KindRequireKeyword && p.lookAhead((*Parser).nextTokenIsOpenParen) {
		return p.parseExternalModuleReference()
	}
	return p.parseEntityName(false /*allowReservedWords*/, false /*allowPrivateName*/, nil /*diagnosticMessage*/)
}

func (p *handleParser) parseExternalModuleReference() ast.Handle {
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	pos := p.nodePos()
	p.parseExpected(ast.KindRequireKeyword)
	p.parseExpected(ast.KindOpenParenToken)
	expression := p.parseModuleSpecifier()
	p.parseExpected(ast.KindCloseParenToken)
	result := p.finishHandle(p.f.NewExternalModuleReference(expression), pos)
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	return result
}

func (p *handleParser) parseModuleSpecifier() ast.Handle {
	if p.token == ast.KindStringLiteral {
		return p.parseLiteralExpression()
	}
	// We allow arbitrary expressions here, even though the grammar only allows string
	// literals.  We check to ensure that it is only a string literal later in the grammar
	// check pass.
	return p.parseExpression()
}

func (p *handleParser) tryParseImportClause(identifier ast.Handle, pos int, phaseModifier ast.Kind, skipJSDocLeadingAsterisks bool) ast.Handle {
	// ImportDeclaration:
	//  import ImportClause from ModuleSpecifier ;
	//  import ModuleSpecifier;
	if !identifier.IsNil() || p.token == ast.KindAsteriskToken || p.token == ast.KindOpenBraceToken {
		importClause := p.parseImportClause(identifier, pos, phaseModifier, skipJSDocLeadingAsterisks)
		p.parseExpected(ast.KindFromKeyword)
		return importClause
	}
	return ast.Handle{}
}

func (p *handleParser) parseImportClause(identifier ast.Handle, pos int, phaseModifier ast.Kind, skipJSDocLeadingAsterisks bool) ast.Handle {
	// ImportClause:
	//  ImportedDefaultBinding
	//  NameSpaceImport
	//  NamedImports
	//  ImportedDefaultBinding, NameSpaceImport
	//  ImportedDefaultBinding, NamedImports
	// If there was no default import or if there is comma token after default import
	// parse namespace or named imports
	var namedBindings ast.Handle
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	if identifier.IsNil() || p.parseOptional(ast.KindCommaToken) {
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
	result := p.finishHandle(p.f.NewImportClause(phaseModifier, identifier, namedBindings), pos)
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	return result
}

func (p *handleParser) parseNamespaceImport() ast.Handle {
	// NameSpaceImport:
	//  * as ImportedBinding
	pos := p.nodePos()
	p.parseExpected(ast.KindAsteriskToken)
	p.parseExpected(ast.KindAsKeyword)
	name := p.parseIdentifier()
	return p.finishHandle(p.f.NewNamespaceImport(name), pos)
}

func (p *handleParser) parseNamedImports() ast.Handle {
	pos := p.nodePos()
	// NamedImports:
	//  { }
	//  { ImportsList }
	//  { ImportsList, }
	imports := p.parseBracketedList(PCImportOrExportSpecifiers, (*handleParser).parseImportSpecifier, ast.KindOpenBraceToken, ast.KindCloseBraceToken)
	return p.finishHandle(p.f.NewNamedImports(imports), pos)
}

func (p *handleParser) parseImportSpecifier() ast.Handle {
	pos := p.nodePos()
	isTypeOnly, propertyName, name := p.parseImportOrExportSpecifier(ast.KindImportSpecifier)
	var identifierName ast.Handle
	if name.Kind() == ast.KindIdentifier {
		identifierName = name
	} else {
		p.parseErrorAtRange(p.skipRangeTrivia(name.Loc()), diagnostics.Identifier_expected)
		identifierName = p.newIdentifierHandle("")
		p.finishHandle(identifierName, name.Loc().Pos())
	}
	result := (p.finishHandle(p.f.NewImportSpecifier(isTypeOnly, propertyName, identifierName), pos))
	return result
}

func (p *handleParser) parseImportOrExportSpecifier(kind ast.Kind) (isTypeOnly bool, propertyName ast.Handle, name ast.Handle) {
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
	if name.Kind() == ast.KindIdentifier && handleText(name) == "type" {
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
		p.parseErrorAtRange(p.skipRangeTrivia(name.Loc()), diagnostics.Identifier_expected)
	}

	return isTypeOnly, propertyName, name
}

func (p *handleParser) parseModuleExportName(disallowKeywords bool) (node ast.Handle, nameOk bool) {
	nameOk = true

	if p.token == ast.KindStringLiteral {
		return p.parseLiteralExpression(), nameOk
	}
	if disallowKeywords && ast.IsKeyword(p.token) && !p.isIdentifier() {
		nameOk = false
	}
	return p.parseIdentifierName(), nameOk
}

func (p *handleParser) tryParseImportAttributes() ast.Handle {
	if p.token == ast.KindWithKeyword || (p.token == ast.KindAssertKeyword && !p.hasPrecedingLineBreak()) {
		if p.token == ast.KindAssertKeyword {
			p.parseErrorAtCurrentToken(diagnostics.Import_assertions_have_been_replaced_by_import_attributes_Use_with_instead_of_assert)
		}
		return p.parseImportAttributes(p.token, false /*skipKeyword*/)
	}
	return ast.Handle{}
}

func (p *handleParser) parseExportAssignment(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
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
	result := p.finishHandle(p.f.NewExportAssignment(modifiers, isExportEquals, ast.Handle{}, expression), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseNamespaceExportDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
	p.parseExpected(ast.KindAsKeyword)
	p.parseExpected(ast.KindNamespaceKeyword)
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	name := p.parseIdentifier()
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	p.parseSemicolon()
	// NamespaceExportDeclaration nodes cannot have decorators or modifiers, we attach them here so we can report them in the grammar checker
	result := p.finishHandle(p.f.NewNamespaceExportDeclaration(modifiers, name), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseExportDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
	saveContextFlags := p.contextFlags
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	p.setContextFlags(ast.NodeFlagsAwaitContext, true)
	var exportClause ast.Handle
	var moduleSpecifier ast.Handle
	var attributes ast.Handle
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
	if !moduleSpecifier.IsNil() && (p.token == ast.KindWithKeyword || p.token == ast.KindAssertKeyword) && !p.hasPrecedingLineBreak() {
		if p.token == ast.KindAssertKeyword {
			p.parseErrorAtCurrentToken(diagnostics.Import_assertions_have_been_replaced_by_import_attributes_Use_with_instead_of_assert)
		}
		attributes = p.parseImportAttributes(p.token, false /*skipKeyword*/)
	}
	p.parseSemicolon()
	p.contextFlags = saveContextFlags
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	result := p.finishHandle(p.f.NewExportDeclaration(modifiers, isTypeOnly, exportClause, moduleSpecifier, attributes), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseNamespaceExport(pos int) ast.Handle {
	exportName, _ := p.parseModuleExportName(false /*disallowKeywords*/)
	return p.finishHandle(p.f.NewNamespaceExport(exportName), pos)
}

func (p *handleParser) parseNamedExports() ast.Handle {
	pos := p.nodePos()
	// NamedImports:
	//  { }
	//  { ImportsList }
	//  { ImportsList, }
	exports := p.parseBracketedList(PCImportOrExportSpecifiers, (*handleParser).parseExportSpecifier, ast.KindOpenBraceToken, ast.KindCloseBraceToken)
	return p.finishHandle(p.f.NewNamedExports(exports), pos)
}

func (p *handleParser) parseExportSpecifier() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	isTypeOnly, propertyName, name := p.parseImportOrExportSpecifier(ast.KindExportSpecifier)
	result := p.finishHandle(p.f.NewExportSpecifier(isTypeOnly, propertyName, name), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseType() ast.Handle {
	saveContextFlags := p.contextFlags
	p.setContextFlags(ast.NodeFlagsTypeExcludesFlags, false)
	var typeNode ast.Handle
	if p.isStartOfFunctionTypeOrConstructorType() {
		typeNode = p.parseFunctionOrConstructorType()
	} else {
		pos := p.nodePos()
		typeNode = p.parseUnionTypeOrHigher()
		if !p.inDisallowConditionalTypesContext() && !p.hasPrecedingLineBreak() && p.parseOptional(ast.KindExtendsKeyword) {
			// The type following 'extends' is not permitted to be another conditional type
			extendsType := p.inContext(ast.NodeFlagsDisallowConditionalTypesContext, true, (*handleParser).parseType)
			p.parseExpected(ast.KindQuestionToken)
			trueType := p.inContext(ast.NodeFlagsDisallowConditionalTypesContext, false, (*handleParser).parseType)
			p.parseExpected(ast.KindColonToken)
			falseType := p.inContext(ast.NodeFlagsDisallowConditionalTypesContext, false, (*handleParser).parseType)
			conditionalType := p.f.NewConditionalTypeNode(typeNode, extendsType, trueType, falseType)
			p.finishHandle(conditionalType, pos)
			typeNode = conditionalType
		}
	}
	p.contextFlags = saveContextFlags
	return typeNode
}

func (p *handleParser) parseUnionTypeOrHigher() ast.Handle {
	return p.parseUnionOrIntersectionType(ast.KindBarToken, (*handleParser).parseIntersectionTypeOrHigher)
}

func (p *handleParser) parseIntersectionTypeOrHigher() ast.Handle {
	return p.parseUnionOrIntersectionType(ast.KindAmpersandToken, (*handleParser).parseTypeOperatorOrHigher)
}

func (p *handleParser) parseUnionOrIntersectionType(operator ast.Kind, parseConstituentType func(p *handleParser) ast.Handle) ast.Handle {
	pos := p.nodePos()
	isUnionType := operator == ast.KindBarToken
	hasLeadingOperator := p.parseOptional(operator)
	var typeNode ast.Handle
	if hasLeadingOperator {
		typeNode = p.parseFunctionOrConstructorTypeToError(isUnionType, parseConstituentType)
	} else {
		typeNode = parseConstituentType(p)
	}
	if p.token == operator || hasLeadingOperator {
		types := make([]ast.Handle, 1, 8)
		types[0] = typeNode
		for p.parseOptional(operator) {
			types = append(types, p.parseFunctionOrConstructorTypeToError(isUnionType, parseConstituentType))
		}
		typeNode = p.createUnionOrIntersectionTypeNode(operator, p.newList(core.NewTextRange(pos, p.nodePos()), slices.Clone(types)))
		p.finishHandle(typeNode, pos)
	}
	return typeNode
}

func (p *handleParser) createUnionOrIntersectionTypeNode(operator ast.Kind, types ast.ListRef) ast.Handle {
	switch operator {
	case ast.KindBarToken:
		return p.f.NewUnionTypeNode(types)
	case ast.KindAmpersandToken:
		return p.f.NewIntersectionTypeNode(types)
	default:
		panic("Unhandled case in createUnionOrIntersectionType")
	}
}

func (p *handleParser) parseTypeOperatorOrHigher() ast.Handle {
	operator := p.token
	switch operator {
	case ast.KindKeyOfKeyword, ast.KindUniqueKeyword, ast.KindReadonlyKeyword:
		return p.parseTypeOperator(operator)
	case ast.KindInferKeyword:
		return p.parseInferType()
	}
	return p.inContext(ast.NodeFlagsDisallowConditionalTypesContext, false, (*handleParser).parsePostfixTypeOrHigher)
}

func (p *handleParser) parseTypeOperator(operator ast.Kind) ast.Handle {
	pos := p.nodePos()
	p.parseExpected(operator)
	return p.finishHandle(p.f.NewTypeOperatorNode(operator, p.parseTypeOperatorOrHigher()), pos)
}

func (p *handleParser) parseInferType() ast.Handle {
	pos := p.nodePos()
	p.parseExpected(ast.KindInferKeyword)
	return p.finishHandle(p.f.NewInferTypeNode(p.parseTypeParameterOfInferType()), pos)
}

func (p *handleParser) parseTypeParameterOfInferType() ast.Handle {
	pos := p.nodePos()
	name := p.parseIdentifier()
	constraint := p.tryParseConstraintOfInferType()
	return p.finishHandle(p.f.NewTypeParameterDeclaration(0, name, constraint, ast.Handle{}, ast.Handle{}), pos)
}

func (p *handleParser) tryParseConstraintOfInferType() ast.Handle {
	state := p.mark()
	if p.parseOptional(ast.KindExtendsKeyword) {
		constraint := p.inContext(ast.NodeFlagsDisallowConditionalTypesContext, true, (*handleParser).parseType)
		if p.inDisallowConditionalTypesContext() || p.token != ast.KindQuestionToken {
			return constraint
		}
	}
	p.rewind(state)
	return ast.Handle{}
}

func (p *handleParser) parsePostfixTypeOrHigher() ast.Handle {
	pos := p.nodePos()
	typeNode := p.parseNonArrayType()
	for !p.hasPrecedingLineBreak() {
		switch p.token {
		case ast.KindExclamationToken:
			p.nextToken()
			typeNode = p.finishHandle(p.f.NewJSDocNonNullableType(typeNode), pos)
		case ast.KindQuestionToken:
			// If next token is start of a type we have a conditional type
			if p.lookAhead((*Parser).nextIsStartOfType) {
				return typeNode
			}
			p.nextToken()
			typeNode = p.finishHandle(p.f.NewJSDocNullableType(typeNode), pos)
		case ast.KindOpenBracketToken:
			p.parseExpected(ast.KindOpenBracketToken)
			if p.isStartOfType(false /*isStartOfParameter*/) {
				indexType := p.parseType()
				p.parseExpected(ast.KindCloseBracketToken)
				typeNode = p.finishHandle(p.f.NewIndexedAccessTypeNode(typeNode, indexType), pos)
			} else {
				p.parseExpected(ast.KindCloseBracketToken)
				typeNode = p.finishHandle(p.f.NewArrayTypeNode(typeNode), pos)
			}
		default:
			return typeNode
		}
	}
	return typeNode
}

func (p *handleParser) parseNonArrayType() ast.Handle {
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

func (p *handleParser) parseKeywordTypeNode() ast.Handle {
	pos := p.nodePos()
	result := p.f.NewKeywordTypeNode(p.token)
	p.nextToken()
	return p.finishHandle(result, pos)
}

func (p *handleParser) parseThisTypeNode() ast.Handle {
	pos := p.nodePos()
	p.nextToken()
	return p.finishHandle(p.f.NewThisTypeNode(), pos)
}

func (p *handleParser) parseThisTypePredicate(lhs ast.Handle) ast.Handle {
	p.nextToken()
	return p.finishHandle(p.f.NewTypePredicateNode(ast.Handle{}, lhs, p.parseType()), lhs.Loc().Pos())
}

func (p *handleParser) parseLiteralTypeNode(negative bool) ast.Handle {
	pos := p.nodePos()
	if negative {
		p.nextToken()
	}
	var expression ast.Handle
	if p.token == ast.KindTrueKeyword || p.token == ast.KindFalseKeyword || p.token == ast.KindNullKeyword {
		expression = p.parseKeywordExpression()
	} else {
		expression = p.parseLiteralExpression()
	}
	if negative {
		expression = p.finishHandle(p.f.NewPrefixUnaryExpression(ast.KindMinusToken, expression), pos)
	}
	return p.finishHandle(p.f.NewLiteralTypeNode(expression), pos)
}

func (p *handleParser) parseTypeReference() ast.Handle {
	pos := p.nodePos()
	return p.finishHandle(p.f.NewTypeReferenceNode(p.parseEntityNameOfTypeReference(), p.parseTypeArgumentsOfTypeReference()), pos)
}

func (p *handleParser) parseEntityNameOfTypeReference() ast.Handle {
	return p.parseEntityName(true /*allowReservedWords*/, false /*allowPrivateName*/, diagnostics.Type_expected)
}

func (p *handleParser) parseEntityName(allowReservedWords bool, allowPrivateName bool, diagnosticMessage *diagnostics.Message) ast.Handle {
	pos := p.nodePos()
	var entity ast.Handle
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
		entity = p.finishHandle(p.f.NewQualifiedName(entity, p.parseRightSideOfDot(allowReservedWords, allowPrivateName, true /*allowUnicodeEscapeSequenceInIdentifierName*/)), pos)
	}
	return entity
}

func (p *handleParser) parseRightSideOfDot(allowIdentifierNames bool, allowPrivateIdentifiers bool, allowUnicodeEscapeSequenceInIdentifierName bool) ast.Handle {
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
		return p.createMissingIdentifierHandle()
	}
	if p.token == ast.KindPrivateIdentifier {
		node := p.parsePrivateIdentifier()
		if allowPrivateIdentifiers {
			return node
		}
		p.parseErrorAt(p.nodePos(), p.nodePos(), diagnostics.Identifier_expected)
		return p.createMissingIdentifierHandle()
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

func (p *handleParser) parsePrivateIdentifier() ast.Handle {
	pos := p.nodePos()
	text := p.scanner.TokenValue()
	p.nextToken()
	return p.finishHandle(p.f.NewPrivateIdentifier(text), pos)
}

func (p *handleParser) parseTypeArgumentsOfTypeReference() ast.ListRef {
	if !p.hasPrecedingLineBreak() && p.reScanLessThanToken() == ast.KindLessThanToken {
		return p.parseTypeArguments()
	}
	return 0
}

func (p *handleParser) parseTypeArguments() ast.ListRef {
	if p.token == ast.KindLessThanToken {
		return p.parseBracketedList(PCTypeArguments, (*handleParser).parseType, ast.KindLessThanToken, ast.KindGreaterThanToken)
	}
	return 0
}

func (p *handleParser) parseImportType() ast.Handle {
	p.sourceFlags |= ast.NodeFlagsPossiblyContainsDynamicImport
	pos := p.nodePos()
	isTypeOf := p.parseOptional(ast.KindTypeOfKeyword)
	p.parseExpected(ast.KindImportKeyword)
	p.parseExpected(ast.KindOpenParenToken)
	typeNode := p.parseType()
	var attributes ast.Handle
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
	var qualifier ast.Handle
	if p.parseOptional(ast.KindDotToken) {
		qualifier = p.parseEntityNameOfTypeReference()
	}
	typeArguments := p.parseTypeArgumentsOfTypeReference()
	return p.finishHandle(p.f.NewImportTypeNode(isTypeOf, typeNode, attributes, qualifier, typeArguments), pos)
}

func (p *handleParser) parseImportAttribute() ast.Handle {
	pos := p.nodePos()
	var name ast.Handle
	if tokenIsIdentifierOrKeyword(p.token) {
		name = p.parseIdentifierName()
	} else if p.token == ast.KindStringLiteral {
		name = p.parseLiteralExpression()
	}
	if !name.IsNil() {
		p.parseExpected(ast.KindColonToken)
	} else {
		p.parseErrorAtCurrentToken(diagnostics.Identifier_or_string_literal_expected)
	}
	value := p.parseAssignmentExpressionOrHigher()
	return p.finishHandle(p.f.NewImportAttribute(name, value), pos)
}

func (p *handleParser) parseImportAttributes(token ast.Kind, skipKeyword bool) ast.Handle {
	pos := p.nodePos()
	if !skipKeyword {
		p.parseExpected(token)
	}
	var elements ast.ListRef
	var multiLine bool
	openBracePosition := p.scanner.TokenStart()
	if p.parseExpected(ast.KindOpenBraceToken) {
		multiLine = p.hasPrecedingLineBreak()
		elements = p.parseDelimitedList(PCImportAttributes, (*handleParser).parseImportAttribute)
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
		elements = p.parseEmptyListHandle()
	}
	return p.finishHandle(p.f.NewImportAttributes(token, elements, multiLine), pos)
}

func (p *handleParser) parseTypeQuery() ast.Handle {
	pos := p.nodePos()
	p.parseExpected(ast.KindTypeOfKeyword)
	entityName := p.parseEntityName(true /*allowReservedWords*/, true /*allowPrivateName*/, nil)
	// Make sure we perform ASI to prevent parsing the next line's type arguments as part of an instantiation expression
	var typeArguments ast.ListRef
	if !p.hasPrecedingLineBreak() {
		typeArguments = p.parseTypeArguments()
	}
	return p.finishHandle(p.f.NewTypeQueryNode(entityName, typeArguments), pos)
}

func (p *handleParser) parseMappedType() ast.Handle {
	pos := p.nodePos()
	p.parseExpected(ast.KindOpenBraceToken)
	var readonlyToken ast.Handle // ReadonlyKeyword | PlusToken | MinusToken
	if p.token == ast.KindReadonlyKeyword || p.token == ast.KindPlusToken || p.token == ast.KindMinusToken {
		readonlyToken = p.parseTokenHandle()
		if readonlyToken.Kind() != ast.KindReadonlyKeyword {
			p.parseExpected(ast.KindReadonlyKeyword)
		}
	}
	p.parseExpected(ast.KindOpenBracketToken)
	typeParameter := p.parseMappedTypeParameter()
	var nameType ast.Handle
	if p.parseOptional(ast.KindAsKeyword) {
		nameType = p.parseType()
	}
	p.parseExpected(ast.KindCloseBracketToken)
	var questionToken ast.Handle // QuestionToken | PlusToken | MinusToken
	if p.token == ast.KindQuestionToken || p.token == ast.KindPlusToken || p.token == ast.KindMinusToken {
		questionToken = p.parseTokenHandle()
		if questionToken.Kind() != ast.KindQuestionToken {
			p.parseExpected(ast.KindQuestionToken)
		}
	}
	typeNode := p.parseTypeAnnotation()
	p.parseSemicolon()
	members := p.parseList(PCTypeMembers, (*handleParser).parseTypeMember)
	p.parseExpected(ast.KindCloseBraceToken)
	return p.finishHandle(p.f.NewMappedTypeNode(readonlyToken, typeParameter, nameType, questionToken, typeNode, members), pos)
}

func (p *handleParser) parseMappedTypeParameter() ast.Handle {
	pos := p.nodePos()
	name := p.parseIdentifierName()
	p.parseExpected(ast.KindInKeyword)
	typeNode := p.parseType()
	return p.finishHandle(p.f.NewTypeParameterDeclaration(0, name, typeNode, ast.Handle{}, ast.Handle{}), pos)
}

func (p *handleParser) parseTypeMember() ast.Handle {
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

func (p *handleParser) parseSignatureMember(kind ast.Kind) ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	if kind == ast.KindConstructSignature {
		p.parseExpected(ast.KindNewKeyword)
	}
	typeParameters := p.parseTypeParameters()
	parameters := p.parseParameters(ParseFlagsType)
	typeNode := p.parseReturnType(ast.KindColonToken /*isType*/, true)
	p.parseTypeMemberSemicolon()
	var result ast.Handle
	if kind == ast.KindCallSignature {
		result = p.f.NewCallSignatureDeclaration(typeParameters, parameters, typeNode)
	} else {
		result = p.f.NewConstructSignatureDeclaration(typeParameters, parameters, typeNode)
	}
	p.finishHandle(result, pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseTypeParameters() ast.ListRef {
	if p.token == ast.KindLessThanToken {
		return p.parseBracketedList(PCTypeParameters, (*handleParser).parseTypeParameter, ast.KindLessThanToken, ast.KindGreaterThanToken)
	}
	return 0
}

func (p *handleParser) parseTypeParameter() ast.Handle {
	pos := p.nodePos()
	modifiers := p.parseModifiersEx(false /*allowDecorators*/, true /*permitConstAsModifier*/, false /*stopOnStartOfClassStaticBlock*/)
	name := p.parseIdentifier()
	var constraint ast.Handle
	var expression ast.Handle
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
	var defaultType ast.Handle
	if p.parseOptional(ast.KindEqualsToken) {
		defaultType = p.parseType()
	}
	result := p.f.NewTypeParameterDeclaration(modifiers, name, constraint, expression, defaultType)
	return p.finishHandle(result, pos)
}

func (p *handleParser) parseParameters(flags ParseFlags) ast.ListRef {
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
	return p.createMissingListHandle()
}

func (p *handleParser) parseParametersWorker(flags ParseFlags, allowAmbiguity bool) ast.ListRef {
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
	parameters := p.parseDelimitedList(PCParameters, func(p *handleParser) ast.Handle {
		parameter := p.parseParameterEx(inAwaitContext, allowAmbiguity)
		if !parameter.IsNil() && flags&ParseFlagsType == 0 {
		}
		return parameter
	})
	p.contextFlags = saveContextFlags
	return parameters
}

func (p *handleParser) parseParameter() ast.Handle {
	return p.parseParameterEx(false /*inOuterAwaitContext*/, true /*allowAmbiguity*/)
}

func (p *handleParser) parseParameterEx(inOuterAwaitContext bool, allowAmbiguity bool) ast.Handle {
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
		result := p.f.NewParameterDeclaration(
			modifiers,
			ast.Handle{},
			p.createIdentifier(true /*isIdentifier*/),
			ast.Handle{},
			p.parseTypeAnnotation(),
			ast.Handle{},
		)
		if modifiers != 0 {
			p.parseErrorAtRange(p.listHandles(modifiers)[0].Loc(), diagnostics.Neither_decorators_nor_modifiers_may_be_applied_to_this_parameters)
		}
		p.withJSDocHandle(p.finishHandle(result, pos), jsdoc)
		return result
	}
	dotDotDotToken := p.parseOptionalTokenHandle(ast.KindDotDotDotToken)
	if !allowAmbiguity && !p.isParameterNameStart() {
		return ast.Handle{}
	}
	result := p.f.NewParameterDeclaration(
		modifiers,
		dotDotDotToken,
		p.parseNameOfParameter(modifiers),
		p.parseOptionalTokenHandle(ast.KindQuestionToken),
		p.parseTypeAnnotation(),
		p.parseInitializer(),
	)
	p.withJSDocHandle(p.finishHandle(result, pos), jsdoc)
	return result
}

func (p *handleParser) parseNameOfParameter(modifiers ast.ListRef) ast.Handle {
	// FormalParameter [Yield,Await]:
	//      BindingElement[?Yield,?Await]
	name := p.parseIdentifierOrPatternWithDiagnostic(diagnostics.Private_identifiers_cannot_be_used_as_parameters)
	if name.Loc().Len() == 0 && modifiers == 0 && ast.IsModifierKind(p.token) {
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

func (p *handleParser) parseReturnType(returnToken ast.Kind, isType bool) ast.Handle {
	if p.shouldParseReturnType(returnToken, isType) {
		return p.inContext(ast.NodeFlagsDisallowConditionalTypesContext, false, (*handleParser).parseTypeOrTypePredicate)
	}
	return ast.Handle{}
}

func (p *handleParser) parseTypeOrTypePredicate() ast.Handle {
	if p.isIdentifier() {
		state := p.mark()
		pos := p.nodePos()
		id := p.parseIdentifier()
		if p.token == ast.KindIsKeyword && !p.hasPrecedingLineBreak() {
			p.nextToken()
			return p.finishHandle(p.f.NewTypePredicateNode(ast.Handle{}, id, p.parseType()), pos)
		}
		p.rewind(state)
	}
	return p.parseType()
}

func (p *handleParser) parseAccessorDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef, kind ast.Kind, flags ParseFlags) ast.Handle {
	name := p.parsePropertyName()
	typeParameters := p.parseTypeParameters()
	parameters := p.parseParameters(ParseFlagsNone)
	returnType := p.parseReturnType(ast.KindColonToken, false /*isType*/)
	body := p.parseFunctionBlockOrSemicolon(flags, nil /*diagnosticMessage*/)
	var result ast.Handle
	// Keep track of `typeParameters` (for both) and `type` (for setters) if they were parsed those indicate grammar errors
	if kind == ast.KindGetAccessor {
		result = p.f.NewGetAccessorDeclaration(modifiers, name, typeParameters, parameters, returnType, ast.Handle{}, body)
	} else {
		result = p.f.NewSetAccessorDeclaration(modifiers, name, typeParameters, parameters, returnType, ast.Handle{}, body)
	}
	p.withJSDocHandle(p.finishHandle(result, pos), jsdoc)
	if flags&ParseFlagsType == 0 {
	}
	return result
}

func (p *handleParser) parsePropertyName() ast.Handle {
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	prop := p.parsePropertyNameWorker(true /*allowComputedPropertyNames*/)
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	return prop
}

func (p *handleParser) parsePropertyNameWorker(allowComputedPropertyNames bool) ast.Handle {
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

func (p *handleParser) parseComputedPropertyName() ast.Handle {
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
	return p.finishHandle(p.f.NewComputedPropertyName(expression), pos)
}

func (p *handleParser) parseFunctionBlockOrSemicolon(flags ParseFlags, diagnosticMessage *diagnostics.Message) ast.Handle {
	if p.token != ast.KindOpenBraceToken {
		if flags&ParseFlagsType != 0 {
			p.parseTypeMemberSemicolon()
			return ast.Handle{}
		}
		if p.canParseSemicolon() {
			p.parseSemicolon()
			return ast.Handle{}
		}
	}
	return p.parseFunctionBlock(flags, diagnosticMessage)
}

func (p *handleParser) parseFunctionBlock(flags ParseFlags, diagnosticMessage *diagnostics.Message) ast.Handle {
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

func (p *handleParser) parseIndexSignatureDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
	parameters := p.parseBracketedList(PCParameters, (*handleParser).parseParameter, ast.KindOpenBracketToken, ast.KindCloseBracketToken)
	typeNode := p.parseTypeAnnotation()
	p.parseTypeMemberSemicolon()
	result := p.finishHandle(p.f.NewIndexSignatureDeclaration(modifiers, parameters, typeNode), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parsePropertyOrMethodSignature(pos int, jsdoc jsdocScannerInfo, modifiers ast.ListRef) ast.Handle {
	name := p.parsePropertyName()
	questionToken := p.parseOptionalTokenHandle(ast.KindQuestionToken)
	var result ast.Handle
	if p.token == ast.KindOpenParenToken || p.token == ast.KindLessThanToken {
		// Method signatures don't exist in expression contexts.  So they have neither
		// [Yield] nor [Await]
		typeParameters := p.parseTypeParameters()
		parameters := p.parseParameters(ParseFlagsType)
		returnType := p.parseReturnType(ast.KindColonToken /*isType*/, true)
		result = p.f.NewMethodSignatureDeclaration(modifiers, name, questionToken, typeParameters, parameters, returnType)
	} else {
		typeNode := p.parseTypeAnnotation()
		// Although type literal properties cannot not have initializers, we attempt
		// to parse an initializer so we can report in the checker that an interface
		// property or type literal property cannot have an initializer.
		var initializer ast.Handle
		if p.token == ast.KindEqualsToken {
			initializer = p.parseInitializer()
		}
		result = p.f.NewPropertySignatureDeclaration(modifiers, name, questionToken, typeNode, initializer)
	}
	p.parseTypeMemberSemicolon()
	p.withJSDocHandle(p.finishHandle(result, pos), jsdoc)
	return result
}

func (p *handleParser) parseTypeLiteral() ast.Handle {
	pos := p.nodePos()
	result := p.finishHandle(p.f.NewTypeLiteralNode(p.parseObjectTypeMembers()), pos)
	return result
}

func (p *handleParser) parseObjectTypeMembers() ast.ListRef {
	if p.parseExpected(ast.KindOpenBraceToken) {
		members := p.parseList(PCTypeMembers, (*handleParser).parseTypeMember)
		p.parseExpected(ast.KindCloseBraceToken)
		return members
	}
	return p.createMissingListHandle()
}

func (p *handleParser) parseTupleType() ast.Handle {
	pos := p.nodePos()
	return p.finishHandle(p.f.NewTupleTypeNode(p.parseBracketedList(PCTupleElementTypes, (*handleParser).parseTupleElementNameOrTupleElementType, ast.KindOpenBracketToken, ast.KindCloseBracketToken)), pos)
}

func (p *handleParser) parseTupleElementNameOrTupleElementType() ast.Handle {
	if p.lookAhead((*Parser).scanStartOfNamedTupleElement) {
		pos := p.nodePos()
		jsdoc := p.jsdocScannerInfo()
		dotDotDotToken := p.parseOptionalTokenHandle(ast.KindDotDotDotToken)
		name := p.parseIdentifierName()
		questionToken := p.parseOptionalTokenHandle(ast.KindQuestionToken)
		p.parseExpected(ast.KindColonToken)
		typeNode := p.parseTupleElementType()
		result := p.finishHandle(p.f.NewNamedTupleMember(dotDotDotToken, name, questionToken, typeNode), pos)
		p.withJSDocHandle(result, jsdoc)
		return result
	}
	return p.parseTupleElementType()
}

func (p *handleParser) parseTupleElementType() ast.Handle {
	pos := p.nodePos()
	if p.parseOptional(ast.KindDotDotDotToken) {
		return p.finishHandle(p.f.NewRestTypeNode(p.parseType()), pos)
	}
	typeNode := p.parseType()
	if typeNode.Kind() == ast.KindJSDocNullableType && typeNode.Loc().Pos() == typeNode.JSDocNullableTypeType().Loc().Pos() {
		node := p.f.NewOptionalTypeNode(typeNode.JSDocNullableTypeType())
		node.SetFlags(typeNode.Flags())
		node.SetLoc(typeNode.Loc())
		typeNode.JSDocNullableTypeType().SetParent(node)
		return node
	}
	return typeNode
}

func (p *handleParser) parseParenthesizedType() ast.Handle {
	pos := p.nodePos()
	p.parseExpected(ast.KindOpenParenToken)
	typeNode := p.parseType()
	p.parseExpected(ast.KindCloseParenToken)
	return p.finishHandle(p.f.NewParenthesizedTypeNode(typeNode), pos)
}

func (p *handleParser) parseAssertsTypePredicate() ast.Handle {
	pos := p.nodePos()
	assertsModifier := p.parseExpectedTokenHandle(ast.KindAssertsKeyword)
	var parameterName ast.Handle
	if p.token == ast.KindThisKeyword {
		parameterName = p.parseThisTypeNode()
	} else {
		parameterName = p.parseIdentifier()
	}
	var typeNode ast.Handle
	if p.parseOptional(ast.KindIsKeyword) {
		typeNode = p.parseType()
	}
	return p.finishHandle(p.f.NewTypePredicateNode(assertsModifier, parameterName, typeNode), pos)
}

func (p *handleParser) parseTemplateType() ast.Handle {
	pos := p.nodePos()
	return p.finishHandle(p.f.NewTemplateLiteralTypeNode(p.parseTemplateHead(false /*isTaggedTemplate*/), p.parseTemplateTypeSpans()), pos)
}

func (p *handleParser) parseTemplateHead(isTaggedTemplate bool) ast.Handle {
	if !isTaggedTemplate && p.scanner.TokenFlags()&ast.TokenFlagsIsInvalid != 0 {
		p.reScanTemplateToken(false /*isTaggedTemplate*/)
	}
	pos := p.nodePos()
	result := p.f.NewTemplateHead(p.scanner.TokenValue(), p.getTemplateLiteralRawText(2 /*endLength*/), p.scanner.TokenFlags())
	p.nextToken()
	return p.finishHandle(result, pos)
}

func (p *handleParser) parseTemplateTypeSpans() ast.ListRef {
	pos := p.nodePos()
	var list []ast.Handle
	for {
		span := p.parseTemplateTypeSpan()
		list = append(list, span)
		if span.TemplateLiteralTypeSpanLiteral().Kind() != ast.KindTemplateMiddle {
			break
		}
	}
	return p.newList(core.NewTextRange(pos, p.nodePos()), list)
}

func (p *handleParser) parseTemplateTypeSpan() ast.Handle {
	pos := p.nodePos()
	return p.finishHandle(p.f.NewTemplateLiteralTypeSpan(p.parseType(), p.parseLiteralOfTemplateSpan(false /*isTaggedTemplate*/)), pos)
}

func (p *handleParser) parseLiteralOfTemplateSpan(isTaggedTemplate bool) ast.Handle {
	if p.token == ast.KindCloseBraceToken {
		p.reScanTemplateToken(isTaggedTemplate)
		return p.parseTemplateMiddleOrTail()
	}
	p.parseErrorAtCurrentToken(diagnostics.X_0_expected, scanner.TokenToString(ast.KindCloseBraceToken))
	return p.finishHandle(p.f.NewTemplateTail("", "", ast.TokenFlagsNone), p.nodePos())
}

func (p *handleParser) parseTemplateMiddleOrTail() ast.Handle {
	pos := p.nodePos()
	var result ast.Handle
	if p.token == ast.KindTemplateMiddle {
		result = p.f.NewTemplateMiddle(p.scanner.TokenValue(), p.getTemplateLiteralRawText(2 /*endLength*/), p.scanner.TokenFlags())
	} else {
		result = p.f.NewTemplateTail(p.scanner.TokenValue(), p.getTemplateLiteralRawText(1 /*endLength*/), p.scanner.TokenFlags())
	}
	p.nextToken()
	return p.finishHandle(result, pos)
}

func (p *handleParser) parseFunctionOrConstructorTypeToError(isInUnionType bool, parseConstituentType func(p *handleParser) ast.Handle) ast.Handle {
	// the function type and constructor type shorthand notation
	// are not allowed directly in unions and intersections, but we'll
	// try to parse them gracefully and issue a helpful message.
	if p.isStartOfFunctionTypeOrConstructorType() {
		typeNode := p.parseFunctionOrConstructorType()
		var diagnostic *diagnostics.Message
		if typeNode.Kind() == ast.KindFunctionType {
			diagnostic = core.IfElse(isInUnionType,
				diagnostics.Function_type_notation_must_be_parenthesized_when_used_in_a_union_type,
				diagnostics.Function_type_notation_must_be_parenthesized_when_used_in_an_intersection_type)
		} else {
			diagnostic = core.IfElse(isInUnionType,
				diagnostics.Constructor_type_notation_must_be_parenthesized_when_used_in_a_union_type,
				diagnostics.Constructor_type_notation_must_be_parenthesized_when_used_in_an_intersection_type)
		}
		p.parseErrorAtRange(typeNode.Loc(), diagnostic)
		return typeNode
	}
	return parseConstituentType(p)
}

func (p *handleParser) parseFunctionOrConstructorType() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	modifiers := p.parseModifiersForConstructorType()
	isConstructorType := p.parseOptional(ast.KindNewKeyword)
	debug.Assert(modifiers == 0 || isConstructorType, "Per isStartOfFunctionOrConstructorType, a function type cannot have modifiers.")
	typeParameters := p.parseTypeParameters()
	parameters := p.parseParameters(ParseFlagsType)
	returnType := p.parseReturnType(ast.KindEqualsGreaterThanToken, false /*isType*/)
	var result ast.Handle
	if isConstructorType {
		result = p.f.NewConstructorTypeNode(modifiers, typeParameters, parameters, returnType)
	} else {
		result = p.f.NewFunctionTypeNode(typeParameters, parameters, returnType)
	}
	p.finishHandle(result, pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseModifiersForConstructorType() ast.ListRef {
	if p.token == ast.KindAbstractKeyword {
		pos := p.nodePos()
		modifier := p.f.NewToken(p.token)
		p.nextToken()
		p.finishHandle(modifier, pos)
		return p.newList(modifier.Loc(), []ast.Handle{modifier})
	}
	return 0
}

func (p *handleParser) parseModifiers() ast.ListRef {
	return p.parseModifiersEx(false, false, false)
}

func (p *handleParser) parseModifiersEx(allowDecorators bool, permitConstAsModifier bool, stopOnStartOfClassStaticBlock bool) ast.ListRef {
	var hasLeadingModifier bool
	var hasTrailingDecorator bool
	var hasTrailingModifier bool
	var hasStaticModifier bool
	// Decorators should be contiguous in a list of modifiers but can potentially appear in two places (i.e., `[...leadingDecorators, ...leadingModifiers, ...trailingDecorators, ...trailingModifiers]`).
	// The leading modifiers *should* only contain `export` and `default` when trailingDecorators are present, but we'll handle errors for any other leading modifiers in the checker.
	// It is illegal to have both leadingDecorators and trailingDecorators, but we will report that as a grammar check in the checker.
	// parse leading decorators
	pos := p.nodePos()
	list := make([]ast.Handle, 0, 16)
	for {
		if allowDecorators && p.token == ast.KindAtToken && !hasTrailingModifier {
			decorator := p.parseDecorator()
			list = append(list, decorator)
			if hasLeadingModifier {
				hasTrailingDecorator = true
			}
		} else {
			modifier := p.tryParseModifier(hasStaticModifier, permitConstAsModifier, stopOnStartOfClassStaticBlock)
			if modifier.IsNil() {
				break
			}
			if modifier.Kind() == ast.KindStaticKeyword {
				hasStaticModifier = true
			}
			list = append(list, modifier)
			if hasTrailingDecorator {
				hasTrailingModifier = true
			} else {
				hasLeadingModifier = true
			}
		}
	}
	if len(list) != 0 {
		return p.newList(core.NewTextRange(pos, p.nodePos()), slices.Clone(list))
	}
	return 0
}

func (p *handleParser) parseDecorator() ast.Handle {
	pos := p.nodePos()
	p.parseExpected(ast.KindAtToken)
	expression := p.inContext(ast.NodeFlagsDecoratorContext, true, (*handleParser).parseDecoratorExpression)
	return p.finishHandle(p.f.NewDecorator(expression), pos)
}

func (p *handleParser) parseDecoratorExpression() ast.Handle {
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

func (p *handleParser) tryParseModifier(hasSeenStaticModifier bool, permitConstAsModifier bool, stopOnStartOfClassStaticBlock bool) ast.Handle {
	pos := p.nodePos()
	kind := p.token
	if p.token == ast.KindConstKeyword && permitConstAsModifier {
		// We need to ensure that any subsequent modifiers appear on the same line
		// so that when 'const' is a standalone declaration, we don't issue an error.
		if !p.lookAhead((*Parser).nextTokenIsOnSameLineAndCanFollowModifier) {
			return ast.Handle{}
		} else {
			p.nextToken()
		}
	} else if stopOnStartOfClassStaticBlock && p.token == ast.KindStaticKeyword && p.lookAhead((*Parser).nextTokenIsOpenBrace) {
		return ast.Handle{}
	} else if hasSeenStaticModifier && p.token == ast.KindStaticKeyword {
		return ast.Handle{}
	} else {
		if !p.parseAnyContextualModifier() {
			return ast.Handle{}
		}
	}
	return p.finishHandle(p.f.NewToken(kind), pos)
}

func (p *handleParser) parseExpression() ast.Handle {
	// Expression[in]:
	//      AssignmentExpression[in]
	//      Expression[in] , AssignmentExpression[in]

	// clear the decorator context when parsing Expression, as it should be unambiguous when parsing a decorator
	saveContextFlags := p.contextFlags
	p.contextFlags &^= ast.NodeFlagsDecoratorContext
	pos := p.nodePos()
	expr := p.parseAssignmentExpressionOrHigher()
	for {
		operatorToken := p.parseOptionalTokenHandle(ast.KindCommaToken)
		if operatorToken.IsNil() {
			break
		}
		expr = p.makeBinaryExpression(expr, operatorToken, p.parseAssignmentExpressionOrHigher(), pos)
	}
	p.contextFlags = saveContextFlags
	return expr
}

func (p *handleParser) parseExpressionAllowIn() ast.Handle {
	return p.inContext(ast.NodeFlagsDisallowInContext, false, (*handleParser).parseExpression)
}

func (p *handleParser) parseAssignmentExpressionOrHigher() ast.Handle {
	return p.parseAssignmentExpressionOrHigherWorker(true /*allowReturnTypeInArrowFunction*/)
}

func (p *handleParser) parseAssignmentExpressionOrHigherWorker(allowReturnTypeInArrowFunction bool) ast.Handle {
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
	if !arrowExpression.IsNil() {
		return arrowExpression
	}
	arrowExpression = p.tryParseAsyncSimpleArrowFunctionExpression(allowReturnTypeInArrowFunction)
	if !arrowExpression.IsNil() {
		return arrowExpression
	}
	// arrowExpression2 := p.tryParseAsyncSimpleArrowFunctionExpression(allowReturnTypeInArrowFunction)
	// if !arrowExpression2.IsNil() {
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
	if expr.Kind() == ast.KindIdentifier && p.token == ast.KindEqualsGreaterThanToken {
		return p.parseSimpleArrowFunctionExpression(pos, expr, allowReturnTypeInArrowFunction, jsdoc, 0)
	}
	// Now see if we might be in cases '2' or '3'.
	// If the expression was a LHS expression, and we have an assignment operator, then
	// we're in '2' or '3'. Consume the assignment and return.
	//
	// Note: we call reScanGreaterToken so that we get an appropriately merged token
	// for cases like `> > =` becoming `>>=`
	if handleIsLeftHandSide(expr) && ast.IsAssignmentOperator(p.reScanGreaterThanToken()) {
		return p.makeBinaryExpression(expr, p.parseTokenHandle(), p.parseAssignmentExpressionOrHigherWorker(allowReturnTypeInArrowFunction), pos)
	}
	// It wasn't an assignment or a lambda.  This is a conditional expression:
	return p.parseConditionalExpressionRest(expr, pos, allowReturnTypeInArrowFunction)
}

func (p *handleParser) parseYieldExpression() ast.Handle {
	pos := p.nodePos()
	// YieldExpression[In] :
	//      yield
	//      yield [no LineTerminator here] [Lexical goal InputElementRegExp]AssignmentExpression[?In, Yield]
	//      yield [no LineTerminator here] * [Lexical goal InputElementRegExp]AssignmentExpression[?In, Yield]
	p.nextToken()
	var result ast.Handle
	if !p.hasPrecedingLineBreak() && (p.token == ast.KindAsteriskToken || p.isStartOfExpression()) {
		result = p.f.NewYieldExpression(p.parseOptionalTokenHandle(ast.KindAsteriskToken), p.parseAssignmentExpressionOrHigher())
	} else {
		// if the next token is not on the same line as yield.  or we don't have an '*' or
		// the start of an expression, then this is just a simple "yield" expression.
		result = p.f.NewYieldExpression(ast.Handle{}, ast.Handle{})
	}
	return p.finishHandle(result, pos)
}

func (p *handleParser) tryParseParenthesizedArrowFunctionExpression(allowReturnTypeInArrowFunction bool) ast.Handle {
	tristate := p.isParenthesizedArrowFunctionExpression()
	if tristate == core.TSFalse {
		// It's definitely not a parenthesized arrow function expression.
		return ast.Handle{}
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
	if result.IsNil() {
		p.rewind(state)
	}
	return result
}

func (p *handleParser) parseParenthesizedArrowFunctionExpression(allowAmbiguity bool, allowReturnTypeInArrowFunction bool) ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	modifiers := p.parseModifiersForArrowFunction()
	isAsync := p.listSome(modifiers, handleIsAsync)
	signatureFlags := core.IfElse(isAsync, ParseFlagsAwait, ParseFlagsNone)
	// Arrow functions are never generators.
	//
	// If we're speculatively parsing a signature for a parenthesized arrow function, then
	// we have to have a complete parameter list.  Otherwise we might see something like
	// a => (b => c)
	// And think that "(b =>" was actually a parenthesized arrow function with a missing
	// close paren.
	typeParameters := p.parseTypeParameters()
	var parameters ast.ListRef
	if !p.parseExpected(ast.KindOpenParenToken) {
		if !allowAmbiguity {
			return ast.Handle{}
		}
		parameters = p.createMissingListHandle()
	} else {
		if !allowAmbiguity {
			maybeParameters := p.parseParametersWorker(signatureFlags, allowAmbiguity)
			if maybeParameters == 0 {
				return ast.Handle{}
			}
			parameters = maybeParameters
		} else {
			parameters = p.parseParametersWorker(signatureFlags, allowAmbiguity)
		}
		if !p.parseExpected(ast.KindCloseParenToken) && !allowAmbiguity {
			return ast.Handle{}
		}
	}
	hasReturnColon := p.token == ast.KindColonToken
	returnType := p.parseReturnType(ast.KindColonToken /*isType*/, false)
	if !returnType.IsNil() && !allowAmbiguity && typeHasArrowFunctionBlockingParseErrorHandle(returnType) {
		return ast.Handle{}
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
	for !unwrappedType.IsNil() && unwrappedType.Kind() == ast.KindParenthesizedType {
		unwrappedType = unwrappedType.ParenthesizedTypeNodeType() // Skip parens if need be
	}
	if !allowAmbiguity && p.token != ast.KindEqualsGreaterThanToken && p.token != ast.KindOpenBraceToken {
		// Returning undefined here will cause our caller to rewind to where we started from.
		return ast.Handle{}
	}
	// If we have an arrow, then try to parse the body. Even if not, try to parse if we
	// have an opening brace, just in case we're in an error state.
	lastToken := p.token
	equalsGreaterThanToken := p.parseExpectedTokenHandle(ast.KindEqualsGreaterThanToken)
	var body ast.Handle
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
			return ast.Handle{}
		}
	}
	result := p.finishHandle(p.f.NewArrowFunction(modifiers, typeParameters, parameters, returnType, ast.Handle{}, equalsGreaterThanToken, body), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseModifiersForArrowFunction() ast.ListRef {
	if p.token == ast.KindAsyncKeyword {
		pos := p.nodePos()
		p.nextToken()
		modifier := p.finishHandle(p.f.NewToken(ast.KindAsyncKeyword), pos)
		return p.newList(modifier.Loc(), []ast.Handle{modifier})
	}
	return 0
}

func (p *handleParser) parseArrowFunctionExpressionBody(isAsync bool, allowReturnTypeInArrowFunction bool) ast.Handle {
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

func (p *handleParser) parsePossibleParenthesizedArrowFunctionExpression(allowReturnTypeInArrowFunction bool) ast.Handle {
	tokenPos := p.scanner.TokenStart()
	if p.notParenthesizedArrow.Has(tokenPos) {
		return ast.Handle{}
	}
	result := p.parseParenthesizedArrowFunctionExpression(false /*allowAmbiguity*/, allowReturnTypeInArrowFunction)
	if result.IsNil() {
		p.notParenthesizedArrow.Add(tokenPos)
	}
	return result
}

func (p *handleParser) tryParseAsyncSimpleArrowFunctionExpression(allowReturnTypeInArrowFunction bool) ast.Handle {
	// We do a check here so that we won't be doing unnecessarily call to "lookAhead"
	if p.token == ast.KindAsyncKeyword && p.lookAhead((*Parser).nextIsUnParenthesizedAsyncArrowFunction) {
		pos := p.nodePos()
		jsdoc := p.jsdocScannerInfo()
		asyncModifier := p.parseModifiersForArrowFunction()
		expr := p.parseBinaryExpressionOrHigher(ast.OperatorPrecedenceLowest)
		return p.parseSimpleArrowFunctionExpression(pos, expr, allowReturnTypeInArrowFunction, jsdoc, asyncModifier)
	}
	return ast.Handle{}
}

func (p *handleParser) parseSimpleArrowFunctionExpression(pos int, identifier ast.Handle, allowReturnTypeInArrowFunction bool, jsdoc jsdocScannerInfo, asyncModifier ast.ListRef) ast.Handle {
	debug.Assert(p.token == ast.KindEqualsGreaterThanToken, "parseSimpleArrowFunctionExpression should only have been called if we had a =>")
	parameter := p.finishHandle(p.f.NewParameterDeclaration(0, ast.Handle{}, identifier, ast.Handle{}, ast.Handle{}, ast.Handle{}), identifier.Loc().Pos())
	parameters := p.newList(parameter.Loc(), []ast.Handle{parameter})
	equalsGreaterThanToken := p.parseExpectedTokenHandle(ast.KindEqualsGreaterThanToken)
	body := p.parseArrowFunctionExpressionBody(asyncModifier != 0 /*isAsync*/, allowReturnTypeInArrowFunction)
	result := p.finishHandle(p.f.NewArrowFunction(asyncModifier, 0, parameters, ast.Handle{}, ast.Handle{}, equalsGreaterThanToken, body), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseConditionalExpressionRest(leftOperand ast.Handle, pos int, allowReturnTypeInArrowFunction bool) ast.Handle {
	// Note: we are passed in an expression which was produced from parseBinaryExpressionOrHigher.
	questionToken := p.parseOptionalTokenHandle(ast.KindQuestionToken)
	if questionToken.IsNil() {
		return leftOperand
	}
	// Note: we explicitly 'allowIn' in the whenTrue part of the condition expression, and
	// we do not that for the 'whenFalse' part.
	saveContextFlags := p.contextFlags
	p.setContextFlags(ast.NodeFlagsDisallowInContext, false)
	trueExpression := p.parseAssignmentExpressionOrHigherWorker(false /*allowReturnTypeInArrowFunction*/)
	p.contextFlags = saveContextFlags
	colonToken := p.parseExpectedTokenHandle(ast.KindColonToken)
	var falseExpression ast.Handle
	if handleIsPresent(colonToken) {
		falseExpression = p.parseAssignmentExpressionOrHigherWorker(allowReturnTypeInArrowFunction)
	} else {
		falseExpression = p.createMissingIdentifierHandle()
	}
	return p.finishHandle(p.f.NewConditionalExpression(leftOperand, questionToken, trueExpression, colonToken, falseExpression), pos)
}

func (p *handleParser) parseBinaryExpressionOrHigher(precedence ast.OperatorPrecedence) ast.Handle {
	pos := p.nodePos()
	leftOperand := p.parseUnaryExpressionOrHigher()
	return p.parseBinaryExpressionRest(precedence, leftOperand, pos)
}

func (p *handleParser) parseBinaryExpressionRest(precedence ast.OperatorPrecedence, leftOperand ast.Handle, pos int) ast.Handle {
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
				if lastOperand.Kind() == ast.KindBinaryExpression {
					lastPrecedence = ast.GetBinaryOperatorPrecedence(lastOperand.BinaryExpressionOperatorToken().Kind())
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
			leftOperand = p.makeBinaryExpression(leftOperand, p.parseTokenHandle(), p.parseBinaryExpressionOrHigher(newPrecedence), pos)
			lastOperand = leftOperand
		}
	}
	return leftOperand
}

func (p *handleParser) makeSatisfiesExpression(expression ast.Handle, typeNode ast.Handle) ast.Handle {
	return (p.finishHandle(p.f.NewSatisfiesExpression(expression, typeNode), expression.Loc().Pos()))
}

func (p *handleParser) makeAsExpression(left ast.Handle, right ast.Handle) ast.Handle {
	return (p.finishHandle(p.f.NewAsExpression(left, right), left.Loc().Pos()))
}

func (p *handleParser) makeBinaryExpression(left ast.Handle, operatorToken ast.Handle, right ast.Handle, pos int) ast.Handle {
	return p.finishHandle(p.f.NewBinaryExpression(0, left, ast.Handle{}, operatorToken, right), pos)
}

func (p *handleParser) parseUnaryExpressionOrHigher() ast.Handle {
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
		pos := scanner.SkipTrivia(p.sourceText, simpleUnaryExpression.Loc().Pos())
		end := simpleUnaryExpression.Loc().End()
		if simpleUnaryExpression.Kind() == ast.KindTypeAssertionExpression {
			p.parseErrorAt(pos, end, diagnostics.A_type_assertion_expression_is_not_allowed_in_the_left_hand_side_of_an_exponentiation_expression_Consider_enclosing_the_expression_in_parentheses)
		} else {
			debug.Assert(isKeywordOrPunctuation(unaryOperator))
			p.parseErrorAt(pos, end, diagnostics.An_unary_expression_with_the_0_operator_is_not_allowed_in_the_left_hand_side_of_an_exponentiation_expression_Consider_enclosing_the_expression_in_parentheses, scanner.TokenToString(unaryOperator))
		}
	}
	return simpleUnaryExpression
}

func (p *handleParser) parseUpdateExpression() ast.Handle {
	pos := p.nodePos()
	if p.token == ast.KindPlusPlusToken || p.token == ast.KindMinusMinusToken {
		operator := p.token
		p.nextToken()
		return p.finishHandle(p.f.NewPrefixUnaryExpression(operator, p.parseLeftHandSideExpressionOrHigher()), pos)
	} else if p.languageVariant == core.LanguageVariantJSX && p.token == ast.KindLessThanToken && p.lookAhead((*Parser).nextTokenIsIdentifierOrKeywordOrGreaterThan) {
		// JSXElement is part of primaryExpression
		p.abortJSX()
		return ast.Handle{}
	}
	expression := p.parseLeftHandSideExpressionOrHigher()
	if (p.token == ast.KindPlusPlusToken || p.token == ast.KindMinusMinusToken) && !p.hasPrecedingLineBreak() {
		operator := p.token
		p.nextToken()
		return p.finishHandle(p.f.NewPostfixUnaryExpression(expression, operator), pos)
	}
	return expression
}

func (p *handleParser) parseSimpleUnaryExpression() ast.Handle {
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
			p.abortJSX()
			return ast.Handle{}
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

func (p *handleParser) parsePrefixUnaryExpression() ast.Handle {
	pos := p.nodePos()
	operator := p.token
	p.nextToken()
	return p.finishHandle(p.f.NewPrefixUnaryExpression(operator, p.parseSimpleUnaryExpression()), pos)
}

func (p *handleParser) parseDeleteExpression() ast.Handle {
	pos := p.nodePos()
	p.nextToken()
	return p.finishHandle(p.f.NewDeleteExpression(p.parseSimpleUnaryExpression()), pos)
}

func (p *handleParser) parseTypeOfExpression() ast.Handle {
	pos := p.nodePos()
	p.nextToken()
	return p.finishHandle(p.f.NewTypeOfExpression(p.parseSimpleUnaryExpression()), pos)
}

func (p *handleParser) parseVoidExpression() ast.Handle {
	pos := p.nodePos()
	p.nextToken()
	return p.finishHandle(p.f.NewVoidExpression(p.parseSimpleUnaryExpression()), pos)
}

func (p *handleParser) parseAwaitExpression() ast.Handle {
	pos := p.nodePos()
	p.nextToken()
	return p.finishHandle(p.f.NewAwaitExpression(p.parseSimpleUnaryExpression()), pos)
}

func (p *handleParser) parseTypeAssertion() ast.Handle {
	debug.Assert(p.languageVariant != core.LanguageVariantJSX, "Type assertions should never be parsed in JSX; they should be parsed as comparisons or JSX elements/fragments.")
	pos := p.nodePos()
	p.parseExpected(ast.KindLessThanToken)
	typeNode := p.parseType()
	p.parseExpected(ast.KindGreaterThanToken)
	expression := p.parseSimpleUnaryExpression()
	return p.finishHandle(p.f.NewTypeAssertion(typeNode, expression), pos)
}

func (p *handleParser) parseLeftHandSideExpressionOrHigher() ast.Handle {
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
	var expression ast.Handle
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
			expression = p.finishHandle(p.f.NewMetaProperty(ast.KindImportKeyword, p.parseIdentifierName()), pos)
			if handleText(expression) == "defer" {
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

func (p *handleParser) parseSuperExpression() ast.Handle {
	pos := p.nodePos()
	expression := p.parseKeywordExpression()
	if p.token == ast.KindLessThanToken {
		startPos := p.nodePos()
		typeArguments := p.tryParseTypeArgumentsInExpression()
		if typeArguments != 0 {
			p.parseErrorAt(startPos, p.nodePos(), diagnostics.X_super_may_not_use_type_arguments)
			if !p.isTemplateStartOfTaggedTemplate() {
				expression = p.finishHandle(p.f.NewExpressionWithTypeArguments(expression, typeArguments), pos)
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
	return p.finishHandle(p.f.NewPropertyAccessExpression(expression, ast.Handle{}, p.parseRightSideOfDot(true /*allowIdentifierNames*/, true /*allowPrivateIdentifiers*/, true /*allowUnicodeEscapeSequenceInIdentifierName*/), ast.NodeFlagsNone), pos)
}

func (p *handleParser) tryParseTypeArgumentsInExpression() ast.ListRef {
	// TypeArguments must not be parsed in JavaScript files to avoid ambiguity with binary operators.
	// Check the cheap preconditions before saving the parser state: unless the current token is `<`
	// (or `<<`, which reScanLessThanToken would split), there is nothing to speculatively parse and
	// the mark/rewind would be a no-op.
	if p.contextFlags&ast.NodeFlagsJavaScriptFile != 0 || (p.token != ast.KindLessThanToken && p.token != ast.KindLessThanLessThanToken) {
		return 0
	}
	state := p.mark()
	if p.reScanLessThanToken() == ast.KindLessThanToken {
		p.nextToken()
		typeArguments := p.parseDelimitedList(PCTypeArguments, (*handleParser).parseType)
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
	return 0
}

func (p *handleParser) parseMemberExpressionOrHigher() ast.Handle {
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

func (p *handleParser) parseMemberExpressionRest(pos int, expression ast.Handle, allowOptionalChain bool) ast.Handle {
	for {
		var questionDotToken ast.Handle
		isPropertyAccess := false
		if allowOptionalChain && p.isStartOfOptionalPropertyOrElementAccessChain() {
			questionDotToken = p.parseExpectedTokenHandle(ast.KindQuestionDotToken)
			isPropertyAccess = tokenIsIdentifierOrKeyword(p.token)
		} else {
			isPropertyAccess = p.parseOptional(ast.KindDotToken)
		}
		if isPropertyAccess {
			expression = p.parsePropertyAccessExpressionRest(pos, expression, questionDotToken)
			continue
		}
		// when in the [Decorator] context, we do not parse ElementAccess as it could be part of a ComputedPropertyName
		if (!questionDotToken.IsNil() || !p.inDecoratorContext()) && p.parseOptional(ast.KindOpenBracketToken) {
			expression = p.parseElementAccessExpressionRest(pos, expression, questionDotToken)
			continue
		}
		if p.isTemplateStartOfTaggedTemplate() {
			// Absorb type arguments into TemplateExpression when preceding expression is ExpressionWithTypeArguments
			if questionDotToken.IsNil() && expression.Kind() == ast.KindExpressionWithTypeArguments {
				inner := expression.ExpressionWithTypeArgumentsExpression()
				targs := expression.ExpressionWithTypeArgumentsTypeArguments()
				expression = p.parseTaggedTemplateRest(pos, inner, questionDotToken, targs)
				p.unparseExpressionWithTypeArguments(inner, targs, expression)
			} else {
				expression = p.parseTaggedTemplateRest(pos, expression, questionDotToken, 0)
			}
			continue
		}
		if questionDotToken.IsNil() {
			if p.token == ast.KindExclamationToken && !p.hasPrecedingLineBreak() {
				p.nextToken()
				expression = (p.finishHandle(p.f.NewNonNullExpression(expression, ast.NodeFlagsNone), pos))
				continue
			}
			typeArguments := p.tryParseTypeArgumentsInExpression()
			if typeArguments != 0 {
				expression = p.finishHandle(p.f.NewExpressionWithTypeArguments(expression, typeArguments), pos)
				continue
			}
		}
		return expression
	}
}

func (p *handleParser) parsePropertyAccessExpressionRest(pos int, expression ast.Handle, questionDotToken ast.Handle) ast.Handle {
	name := p.parseRightSideOfDot(true /*allowIdentifierNames*/, true /*allowPrivateIdentifiers*/, true /*allowUnicodeEscapeSequenceInIdentifierName*/)
	isOptionalChain := !questionDotToken.IsNil() || p.tryReparseOptionalChain(expression)
	propertyAccess := p.f.NewPropertyAccessExpression(expression, questionDotToken, name, core.IfElse(isOptionalChain, ast.NodeFlagsOptionalChain, ast.NodeFlagsNone))
	if isOptionalChain && name.Kind() == ast.KindPrivateIdentifier {
		p.parseErrorAtRange(p.skipRangeTrivia(name.Loc()), diagnostics.An_optional_chain_cannot_contain_private_identifiers)
	}
	if expression.Kind() == ast.KindExpressionWithTypeArguments {
		typeArguments := expression.ExpressionWithTypeArgumentsTypeArguments()
		if typeArguments != 0 {
			listLoc := p.f.Store().ListLoc(typeArguments)
			loc := core.NewTextRange(listLoc.Pos()-1, scanner.SkipTrivia(p.sourceText, listLoc.End())+1)
			p.parseErrorAtRange(loc, diagnostics.An_instantiation_expression_cannot_be_followed_by_a_property_access)
		}
	}
	return p.finishHandle(propertyAccess, pos)
}

func (p *handleParser) tryReparseOptionalChain(node ast.Handle) bool {
	if node.Flags()&ast.NodeFlagsOptionalChain != 0 {
		return true
	}
	// check for an optional chain in a non-null expression
	if node.Kind() == ast.KindNonNullExpression {
		expr := node.NonNullExpressionExpression()
		for expr.Kind() == ast.KindNonNullExpression && expr.Flags()&ast.NodeFlagsOptionalChain == 0 {
			expr = expr.NonNullExpressionExpression()
		}
		if expr.Flags()&ast.NodeFlagsOptionalChain != 0 {
			// this is part of an optional chain. Walk down from `node` to `expression` and set the flag.
			for node.Kind() == ast.KindNonNullExpression {
				node.SetFlags(node.Flags() | ast.NodeFlagsOptionalChain)
				node = node.NonNullExpressionExpression()
			}
			return true
		}
	}
	return false
}

func (p *handleParser) parseElementAccessExpressionRest(pos int, expression ast.Handle, questionDotToken ast.Handle) ast.Handle {
	argumentExpression := p.createMissingIdentifierHandle()
	if p.token == ast.KindCloseBracketToken {
		p.parseErrorAt(p.nodePos(), p.nodePos(), diagnostics.An_element_access_expression_should_take_an_argument)
	} else {
		argumentExpression = p.parseExpressionAllowIn()
	}
	p.parseExpected(ast.KindCloseBracketToken)
	isOptionalChain := !questionDotToken.IsNil() || p.tryReparseOptionalChain(expression)
	return p.finishHandle(p.f.NewElementAccessExpression(expression, questionDotToken, argumentExpression, core.IfElse(isOptionalChain, ast.NodeFlagsOptionalChain, ast.NodeFlagsNone)), pos)
}

func (p *handleParser) parseCallExpressionRest(pos int, expression ast.Handle) ast.Handle {
	for {
		expression = p.parseMemberExpressionRest(pos, expression /*allowOptionalChain*/, true)
		var typeArguments ast.ListRef
		questionDotToken := p.parseOptionalTokenHandle(ast.KindQuestionDotToken)
		if !questionDotToken.IsNil() {
			typeArguments = p.tryParseTypeArgumentsInExpression()
			if p.isTemplateStartOfTaggedTemplate() {
				expression = p.parseTaggedTemplateRest(pos, expression, questionDotToken, typeArguments)
				continue
			}
		}
		if typeArguments != 0 || p.token == ast.KindOpenParenToken {
			// Absorb type arguments into CallExpression when preceding expression is ExpressionWithTypeArguments
			if questionDotToken.IsNil() && expression.Kind() == ast.KindExpressionWithTypeArguments {
				typeArguments = expression.ExpressionWithTypeArgumentsTypeArguments()
				expression = expression.ExpressionWithTypeArgumentsExpression()
			}
			inner := expression
			argumentList := p.parseArgumentList()
			isOptionalChain := !questionDotToken.IsNil() || p.tryReparseOptionalChain(expression)
			expression = (p.finishHandle(p.f.NewCallExpression(expression, questionDotToken, typeArguments, argumentList, core.IfElse(isOptionalChain, ast.NodeFlagsOptionalChain, ast.NodeFlagsNone)), pos))
			p.unparseExpressionWithTypeArguments(inner, typeArguments, expression)
			continue
		}
		if !questionDotToken.IsNil() {
			// We parsed `?.` but then failed to parse anything, so report a missing identifier here.
			p.parseErrorAtCurrentToken(diagnostics.Identifier_expected)
			name := p.createMissingIdentifierHandle()
			expression = p.finishHandle(p.f.NewPropertyAccessExpression(expression, questionDotToken, name, ast.NodeFlagsOptionalChain), pos)
		}
		break
	}
	return expression
}

func (p *handleParser) parseArgumentList() ast.ListRef {
	p.parseExpected(ast.KindOpenParenToken)
	result := p.parseDelimitedList(PCArgumentExpressions, (*handleParser).parseArgumentExpression)
	p.parseExpected(ast.KindCloseParenToken)
	return result
}

func (p *handleParser) parseArgumentExpression() ast.Handle {
	return p.inContext(ast.NodeFlagsDisallowInContext|ast.NodeFlagsDecoratorContext, false, (*handleParser).parseArgumentOrArrayLiteralElement)
}

func (p *handleParser) parseArgumentOrArrayLiteralElement() ast.Handle {
	switch p.token {
	case ast.KindDotDotDotToken:
		return p.parseSpreadElement()
	case ast.KindCommaToken:
		return p.finishHandle(p.f.NewOmittedExpression(), p.nodePos())
	}
	return p.parseAssignmentExpressionOrHigher()
}

func (p *handleParser) parseSpreadElement() ast.Handle {
	pos := p.nodePos()
	p.parseExpected(ast.KindDotDotDotToken)
	expression := p.parseAssignmentExpressionOrHigher()
	return p.finishHandle(p.f.NewSpreadElement(expression), pos)
}

func (p *handleParser) parseTaggedTemplateRest(pos int, tag ast.Handle, questionDotToken ast.Handle, typeArguments ast.ListRef) ast.Handle {
	var template ast.Handle
	if p.token == ast.KindNoSubstitutionTemplateLiteral {
		p.reScanTemplateToken(true /*isTaggedTemplate*/)
		template = p.parseLiteralExpression()
	} else {
		template = p.parseTemplateExpression(true /*isTaggedTemplate*/)
	}
	isOptionalChain := !questionDotToken.IsNil() || tag.Flags()&ast.NodeFlagsOptionalChain != 0
	return (p.finishHandle(p.f.NewTaggedTemplateExpression(tag, questionDotToken, typeArguments, template, core.IfElse(isOptionalChain, ast.NodeFlagsOptionalChain, ast.NodeFlagsNone)), pos))
}

func (p *handleParser) parseTemplateExpression(isTaggedTemplate bool) ast.Handle {
	pos := p.nodePos()
	return p.finishHandle(p.f.NewTemplateExpression(p.parseTemplateHead(isTaggedTemplate), p.parseTemplateSpans(isTaggedTemplate)), pos)
}

func (p *handleParser) parseTemplateSpans(isTaggedTemplate bool) ast.ListRef {
	pos := p.nodePos()
	var list []ast.Handle
	for {
		span := p.parseTemplateSpan(isTaggedTemplate)
		list = append(list, span)
		if span.TemplateSpanLiteral().Kind() != ast.KindTemplateMiddle {
			break
		}
	}
	return p.newList(core.NewTextRange(pos, p.nodePos()), list)
}

func (p *handleParser) parseTemplateSpan(isTaggedTemplate bool) ast.Handle {
	pos := p.nodePos()
	expression := p.parseExpressionAllowIn()
	literal := p.parseLiteralOfTemplateSpan(isTaggedTemplate)
	return p.finishHandle(p.f.NewTemplateSpan(expression, literal), pos)
}

func (p *handleParser) parsePrimaryExpression() ast.Handle {
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

func (p *handleParser) parseParenthesizedExpression() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	p.parseExpected(ast.KindOpenParenToken)
	expression := p.parseExpressionAllowIn()
	p.parseExpected(ast.KindCloseParenToken)
	result := p.finishHandle(p.f.NewParenthesizedExpression(expression), pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseArrayLiteralExpression() ast.Handle {
	pos := p.nodePos()
	openBracketPosition := p.scanner.TokenStart()
	openBracketParsed := p.parseExpected(ast.KindOpenBracketToken)
	multiLine := p.hasPrecedingLineBreak()
	elements := p.parseDelimitedList(PCArrayLiteralMembers, (*handleParser).parseArgumentOrArrayLiteralElement)
	p.parseExpectedMatchingBrackets(ast.KindOpenBracketToken, ast.KindCloseBracketToken, openBracketParsed, openBracketPosition)
	return p.finishHandle(p.f.NewArrayLiteralExpression(elements, multiLine), pos)
}

func (p *handleParser) parseObjectLiteralExpression() ast.Handle {
	pos := p.nodePos()
	openBracePosition := p.scanner.TokenStart()
	openBraceParsed := p.parseExpected(ast.KindOpenBraceToken)
	multiLine := p.hasPrecedingLineBreak()
	properties := p.parseDelimitedList(PCObjectLiteralMembers, (*handleParser).parseObjectLiteralElement)
	p.parseExpectedMatchingBrackets(ast.KindOpenBraceToken, ast.KindCloseBraceToken, openBraceParsed, openBracePosition)
	return p.finishHandle(p.f.NewObjectLiteralExpression(properties, multiLine), pos)
}

func (p *handleParser) parseObjectLiteralElement() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	if p.parseOptional(ast.KindDotDotDotToken) {
		expression := p.parseAssignmentExpressionOrHigher()
		result := p.finishHandle(p.f.NewSpreadAssignment(expression), pos)
		p.withJSDocHandle(result, jsdoc)
		return result
	}
	modifiers := p.parseModifiersEx(true /*allowDecorators*/, false /*permitConstAsModifier*/, false /*stopOnStartOfClassStaticBlock*/)
	if p.parseContextualModifier(ast.KindGetKeyword) {
		return p.parseAccessorDeclaration(pos, jsdoc, modifiers, ast.KindGetAccessor, ParseFlagsNone)
	}
	if p.parseContextualModifier(ast.KindSetKeyword) {
		return p.parseAccessorDeclaration(pos, jsdoc, modifiers, ast.KindSetAccessor, ParseFlagsNone)
	}
	asteriskToken := p.parseOptionalTokenHandle(ast.KindAsteriskToken)
	tokenIsIdentifier := p.isIdentifier()
	name := p.parsePropertyName()
	// Disallowing of optional property assignments and definite assignment assertion happens in the grammar checker.
	postfixToken := p.parseOptionalTokenHandle(ast.KindQuestionToken)
	// Decorators, Modifiers, questionToken, and exclamationToken are not supported by property assignments and are reported in the grammar checker
	if postfixToken.IsNil() {
		postfixToken = p.parseOptionalTokenHandle(ast.KindExclamationToken)
	}
	if !asteriskToken.IsNil() || p.token == ast.KindOpenParenToken || p.token == ast.KindLessThanToken {
		return p.parseMethodDeclaration(pos, jsdoc, modifiers, asteriskToken, name, postfixToken, nil /*diagnosticMessage*/)
	}
	// check if it is short-hand property assignment or normal property assignment
	// NOTE: if token is EqualsToken it is interpreted as CoverInitializedName production
	// CoverInitializedName[Yield] :
	//     IdentifierReference[?Yield] Initializer[In, ?Yield]
	// this is necessary because ObjectLiteral productions are also used to cover grammar for ObjectAssignmentPattern
	var node ast.Handle
	isShorthandPropertyAssignment := tokenIsIdentifier && p.token != ast.KindColonToken
	if isShorthandPropertyAssignment {
		equalsToken := p.parseOptionalTokenHandle(ast.KindEqualsToken)
		var initializer ast.Handle
		if !equalsToken.IsNil() {
			initializer = p.inContext(ast.NodeFlagsDisallowInContext, false, (*handleParser).parseAssignmentExpressionOrHigher)
		}
		node = p.f.NewShorthandPropertyAssignment(modifiers, name, postfixToken, ast.Handle{}, equalsToken, initializer)
	} else {
		p.parseExpected(ast.KindColonToken)
		initializer := p.inContext(ast.NodeFlagsDisallowInContext, false, (*handleParser).parseAssignmentExpressionOrHigher)
		node = p.f.NewPropertyAssignment(modifiers, name, postfixToken, ast.Handle{}, initializer)
	}
	p.finishHandle(node, pos)
	p.withJSDocHandle(node, jsdoc)
	return node
}

func (p *handleParser) parseFunctionExpression() ast.Handle {
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
	asteriskToken := p.parseOptionalTokenHandle(ast.KindAsteriskToken)
	isGenerator := !asteriskToken.IsNil()
	isAsync := p.listSome(modifiers, handleIsAsync)
	signatureFlags := core.IfElse(isGenerator, ParseFlagsYield, ParseFlagsNone) | core.IfElse(isAsync, ParseFlagsAwait, ParseFlagsNone)
	var name ast.Handle
	switch {
	case isGenerator && isAsync:
		name = p.inContext(ast.NodeFlagsYieldContext|ast.NodeFlagsAwaitContext, true, (*handleParser).parseOptionalBindingIdentifier)
	case isGenerator:
		name = p.inContext(ast.NodeFlagsYieldContext, true, (*handleParser).parseOptionalBindingIdentifier)
	case isAsync:
		name = p.inContext(ast.NodeFlagsAwaitContext, true, (*handleParser).parseOptionalBindingIdentifier)
	default:
		name = p.parseOptionalBindingIdentifier()
	}
	typeParameters := p.parseTypeParameters()
	parameters := p.parseParameters(signatureFlags)
	returnType := p.parseReturnType(ast.KindColonToken, false /*isType*/)
	body := p.parseFunctionBlock(signatureFlags, nil /*diagnosticMessage*/)
	p.contextFlags = saveContexFlags
	result := p.f.NewFunctionExpression(modifiers, asteriskToken, name, typeParameters, parameters, returnType, ast.Handle{}, body)
	p.finishHandle(result, pos)
	p.withJSDocHandle(result, jsdoc)
	return result
}

func (p *handleParser) parseOptionalBindingIdentifier() ast.Handle {
	if p.isBindingIdentifier() {
		return p.parseBindingIdentifier()
	}
	return ast.Handle{}
}

func (p *handleParser) parseDecoratedExpression() ast.Handle {
	pos := p.nodePos()
	jsdoc := p.jsdocScannerInfo()
	modifiers := p.parseModifiersEx(true /*allowDecorators*/, false /*permitConstAsModifier*/, false /*stopOnStartOfClassStaticBlock*/)
	if p.token == ast.KindClassKeyword {
		return p.parseClassDeclarationOrExpression(pos, jsdoc, modifiers, ast.KindClassExpression)
	}
	p.parseErrorAt(p.nodePos(), p.nodePos(), diagnostics.Expression_expected)
	return p.finishHandle(p.f.NewMissingDeclaration(modifiers), pos)
}

func (p *handleParser) unparseExpressionWithTypeArguments(expression ast.Handle, typeArguments ast.ListRef, result ast.Handle) {
	// force overwrite the `.Parent` of the expression and type arguments to erase the fact that they may have originally been parsed as an ExpressionWithTypeArguments and be parented to such
	if !expression.IsNil() {
		expression.SetParent(result)
	}
	if typeArguments != 0 {
		for _, a := range p.listHandles(typeArguments) {
			a.SetParent(result)
		}
	}
}

func (p *handleParser) parseNewExpressionOrNewDotTarget() ast.Handle {
	pos := p.nodePos()
	p.parseExpected(ast.KindNewKeyword)
	if p.parseOptional(ast.KindDotToken) {
		name := p.parseIdentifierName()
		return p.finishHandle(p.f.NewMetaProperty(ast.KindNewKeyword, name), pos)
	}
	expressionPos := p.nodePos()
	expression := p.parseMemberExpressionRest(expressionPos, p.parsePrimaryExpression(), false /*allowOptionalChain*/)
	var typeArguments ast.ListRef
	// Absorb type arguments into NewExpression when preceding expression is ExpressionWithTypeArguments
	if expression.Kind() == ast.KindExpressionWithTypeArguments {
		typeArguments = expression.ExpressionWithTypeArgumentsTypeArguments()
		expression = expression.ExpressionWithTypeArgumentsExpression()
	}
	if p.token == ast.KindQuestionDotToken {
		p.parseErrorAtCurrentToken(diagnostics.Invalid_optional_chain_from_new_expression_Did_you_mean_to_call_0, handleGetText(p.sourceText, expression, false /*includeTrivia*/))
	}
	var argumentList ast.ListRef
	if p.token == ast.KindOpenParenToken {
		argumentList = p.parseArgumentList()
	}
	result := (p.finishHandle(p.f.NewNewExpression(expression, typeArguments, argumentList), pos))
	p.unparseExpressionWithTypeArguments(expression, typeArguments, result)
	return result
}

func (p *handleParser) parseKeywordExpression() ast.Handle {
	pos := p.nodePos()
	result := p.f.NewKeywordExpression(p.token)
	p.nextToken()
	return p.finishHandle(result, pos)
}

func (p *handleParser) parseLiteralExpression() ast.Handle {
	pos := p.nodePos()
	text := p.scanner.TokenValue()
	tokenFlags := p.scanner.TokenFlags()
	var result ast.Handle
	switch p.token {
	case ast.KindStringLiteral:
		result = p.f.NewStringLiteral(text, tokenFlags)
	case ast.KindNumericLiteral:
		result = p.f.NewNumericLiteral(text, tokenFlags)
	case ast.KindBigIntLiteral:
		result = p.f.NewBigIntLiteral(text, tokenFlags)
	case ast.KindRegularExpressionLiteral:
		result = p.f.NewRegularExpressionLiteral(text, tokenFlags)
	case ast.KindNoSubstitutionTemplateLiteral:
		result = p.f.NewNoSubstitutionTemplateLiteral(text, tokenFlags)
	default:
		panic("Unhandled case in parseLiteralExpression")
	}
	p.nextToken()
	return p.finishHandle(result, pos)
}

func (p *handleParser) parseIdentifierNameErrorOnUnicodeEscapeSequence() ast.Handle {
	if p.scanner.HasUnicodeEscape() || p.scanner.HasExtendedUnicodeEscape() {
		p.parseErrorAtCurrentToken(diagnostics.Unicode_escape_sequence_cannot_appear_here)
	}
	return p.createIdentifier(tokenIsIdentifierOrKeyword(p.token))
}

func (p *handleParser) parseBindingIdentifier() ast.Handle {
	return p.parseBindingIdentifierWithDiagnostic(nil)
}

func (p *handleParser) parseBindingIdentifierWithDiagnostic(privateIdentifierDiagnosticMessage *diagnostics.Message) ast.Handle {
	saveHasAwaitIdentifier := p.statementHasAwaitIdentifier
	id := p.createIdentifierWithDiagnostic(p.isBindingIdentifier(), nil /*diagnosticMessage*/, privateIdentifierDiagnosticMessage)
	p.statementHasAwaitIdentifier = saveHasAwaitIdentifier
	return id
}

func (p *handleParser) parseIdentifierName() ast.Handle {
	return p.parseIdentifierNameWithDiagnostic(nil)
}

func (p *handleParser) parseIdentifierNameWithDiagnostic(diagnosticMessage *diagnostics.Message) ast.Handle {
	return p.createIdentifierWithDiagnostic(tokenIsIdentifierOrKeyword(p.token), diagnosticMessage, nil)
}

func (p *handleParser) parseIdentifier() ast.Handle {
	return p.parseIdentifierWithDiagnostic(nil, nil)
}

func (p *handleParser) parseIdentifierWithDiagnostic(diagnosticMessage *diagnostics.Message, privateIdentifierDiagnosticMessage *diagnostics.Message) ast.Handle {
	return p.createIdentifierWithDiagnostic(p.isIdentifier(), diagnosticMessage, privateIdentifierDiagnosticMessage)
}

func (p *handleParser) createIdentifier(isIdentifier bool) ast.Handle {
	return p.createIdentifierWithDiagnostic(isIdentifier, nil, nil)
}

func (p *handleParser) createIdentifierWithDiagnostic(isIdentifier bool, diagnosticMessage *diagnostics.Message, privateIdentifierDiagnosticMessage *diagnostics.Message) ast.Handle {
	if isIdentifier {
		var pos int
		if p.scanner.HasPrecedingJSDocLeadingAsterisks() {
			pos = p.scanner.TokenStart()
		} else {
			pos = p.nodePos()
		}
		text := p.scanner.TokenValue()
		p.nextTokenWithoutCheck()
		return p.finishHandle(p.newIdentifierHandle(text), pos)
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
	return p.createMissingIdentifierHandle()
}
