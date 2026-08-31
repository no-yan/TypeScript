package parser

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// tryParseSourceHandle parses a complete TypeScript or JavaScript source file
// into Store. Unsupported or malformed syntax returns false so ParseSourceFile
// can rewind into the pointer recovery parser.
func (p *Parser) tryParseSourceHandle(factory *ast.Factory) (ast.Handle, bool) {
	state := p.mark()
	identifierCount := p.identifierCount
	root, ok := p.parseSourceHandle(factory)
	if !ok || state.diagnosticsLen != 0 || len(p.diagnostics) != 0 {
		p.rewind(state)
		p.identifierCount = identifierCount
		return ast.Handle{}, false
	}
	return root, true
}

func (p *Parser) parseSourceHandle(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	statements := make([]ast.Handle, 0, 16)
	for p.token != ast.KindEndOfFile {
		if p.nativeJSDocBlocksParse() {
			return ast.Handle{}, false
		}
		statement, ok := p.parseNativeStatement(factory)
		if !ok {
			return ast.Handle{}, false
		}
		statements = append(statements, statement)
	}
	if p.nativeJSDocBlocksParse() {
		return ast.Handle{}, false
	}
	list := factory.List(core.NewTextRange(pos, p.nodePos()), statements...)
	eof := p.parseNativeToken(factory)
	root := factory.NewSourceFile(list, eof)
	return p.finishNativeHandle(factory, root, pos), true
}

func (p *Parser) nativeJSDocBlocksParse() bool {
	// TypeScript JSDoc is lazy after parse. Rejecting the whole file here
	// would keep checker.ts on dual-write. JavaScript JSDoc still falls back.
	if p.scriptKind == core.ScriptKindTS {
		return false
	}
	return p.jsdocScannerInfo() != 0
}

func (p *Parser) parseNativeStatement(factory *ast.Factory) (ast.Handle, bool) {
	if p.nativeJSDocBlocksParse() || p.token == ast.KindAtToken {
		return ast.Handle{}, false
	}
	switch p.token {
	case ast.KindSemicolonToken:
		return p.parseNativeEmptyStatement(factory)
	case ast.KindOpenBraceToken:
		return p.parseNativeBlock(factory)
	case ast.KindVarKeyword:
		return p.parseNativeVariableStatement(factory, 0)
	case ast.KindConstKeyword:
		if p.lookAhead(func(p *Parser) bool { return p.nextToken() == ast.KindEnumKeyword }) {
			return p.parseNativeEnumDeclaration(factory, p.takeNativeConstModifier(factory))
		}
		return p.parseNativeVariableStatement(factory, 0)
	case ast.KindLetKeyword:
		if !p.isLetDeclaration() {
			return p.parseNativeExpressionStatement(factory)
		}
		return p.parseNativeVariableStatement(factory, 0)
	case ast.KindFunctionKeyword:
		return p.parseNativeFunctionDeclaration(factory, 0)
	case ast.KindClassKeyword:
		return p.parseNativeClassDeclaration(factory, 0)
	case ast.KindTypeKeyword:
		if p.lookAhead((*Parser).isNativeTypeAliasStart) {
			return p.parseNativeTypeAliasDeclaration(factory, 0)
		}
		return p.parseNativeExpressionOrLabeledStatement(factory)
	case ast.KindEnumKeyword:
		if p.lookAhead((*Parser).isNativeNamedDeclarationStart) {
			return p.parseNativeEnumDeclaration(factory, 0)
		}
		return p.parseNativeExpressionOrLabeledStatement(factory)
	case ast.KindInterfaceKeyword:
		if p.lookAhead((*Parser).isNativeNamedDeclarationStart) {
			return p.parseNativeInterfaceDeclaration(factory, 0)
		}
		return p.parseNativeExpressionOrLabeledStatement(factory)
	case ast.KindModuleKeyword, ast.KindNamespaceKeyword:
		if p.lookAhead((*Parser).isNativeModuleDeclarationStart) {
			return p.parseNativeModuleDeclaration(factory, 0)
		}
		return p.parseNativeExpressionOrLabeledStatement(factory)
	case ast.KindIfKeyword:
		return p.parseNativeIfStatement(factory)
	case ast.KindDoKeyword:
		return p.parseNativeDoStatement(factory)
	case ast.KindWhileKeyword:
		return p.parseNativeWhileStatement(factory)
	case ast.KindForKeyword:
		return p.parseNativeForStatement(factory)
	case ast.KindContinueKeyword:
		return p.parseNativeJumpStatement(factory, ast.KindContinueStatement)
	case ast.KindBreakKeyword:
		return p.parseNativeJumpStatement(factory, ast.KindBreakStatement)
	case ast.KindReturnKeyword:
		return p.parseNativeReturnStatement(factory)
	case ast.KindSwitchKeyword:
		return p.parseNativeSwitchStatement(factory)
	case ast.KindThrowKeyword:
		return p.parseNativeThrowStatement(factory)
	case ast.KindTryKeyword:
		return p.parseNativeTryStatement(factory)
	case ast.KindDebuggerKeyword:
		return p.parseNativeDebuggerStatement(factory)
	case ast.KindImportKeyword:
		return p.parseNativeImportDeclaration(factory, 0)
	case ast.KindExportKeyword, ast.KindDeclareKeyword, ast.KindAsyncKeyword,
		ast.KindPublicKeyword, ast.KindPrivateKeyword, ast.KindProtectedKeyword,
		ast.KindReadonlyKeyword, ast.KindStaticKeyword, ast.KindAbstractKeyword,
		ast.KindOverrideKeyword, ast.KindAccessorKeyword, ast.KindDefaultKeyword:
		if p.lookAhead((*Parser).isStartOfDeclaration) {
			return p.parseNativeDeclarationStatement(factory)
		}
	}
	return p.parseNativeExpressionOrLabeledStatement(factory)
}

func (p *Parser) parseNativeDeclarationStatement(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	modifiers, ok := p.parseNativeModifiers(factory)
	if !ok {
		return ast.Handle{}, false
	}
	switch p.token {
	case ast.KindVarKeyword, ast.KindLetKeyword:
		return p.parseNativeVariableStatementAt(factory, pos, modifiers)
	case ast.KindConstKeyword:
		if p.lookAhead(func(p *Parser) bool { return p.nextToken() == ast.KindEnumKeyword }) {
			modifiers = nativeAppendModifier(factory, modifiers, p.parseNativeToken(factory))
			return p.parseNativeEnumDeclarationAt(factory, pos, modifiers)
		}
		return p.parseNativeVariableStatementAt(factory, pos, modifiers)
	case ast.KindFunctionKeyword:
		return p.parseNativeFunctionDeclarationAt(factory, pos, modifiers)
	case ast.KindClassKeyword:
		return p.parseNativeClassDeclarationAt(factory, pos, modifiers)
	case ast.KindInterfaceKeyword:
		return p.parseNativeInterfaceDeclarationAt(factory, pos, modifiers)
	case ast.KindTypeKeyword:
		return p.parseNativeTypeAliasDeclarationAt(factory, pos, modifiers)
	case ast.KindEnumKeyword:
		return p.parseNativeEnumDeclarationAt(factory, pos, modifiers)
	case ast.KindModuleKeyword, ast.KindNamespaceKeyword:
		return p.parseNativeModuleDeclarationAt(factory, pos, modifiers)
	case ast.KindImportKeyword:
		return p.parseNativeImportDeclarationAt(factory, pos, modifiers)
	case ast.KindExportKeyword:
		p.nextToken()
		return p.parseNativeExportAfterModifiers(factory, pos, modifiers)
	default:
		if modifiers != 0 {
			return p.parseNativeExportAfterModifiers(factory, pos, modifiers)
		}
		return ast.Handle{}, false
	}
}

func (p *Parser) parseNativeModifiers(factory *ast.Factory) (ast.ListRef, bool) {
	pos := p.nodePos()
	mods := make([]ast.Handle, 0, 4)
	for {
		if p.token == ast.KindAtToken {
			return 0, false
		}
		if !p.lookAhead((*Parser).nativeTokenIsModifier) {
			break
		}
		mods = append(mods, p.parseNativeToken(factory))
	}
	if len(mods) == 0 {
		return 0, true
	}
	return factory.List(core.NewTextRange(pos, p.nodePos()), mods...), true
}

func (p *Parser) nativeTokenIsModifier() bool {
	return ast.IsModifierKind(p.token) && p.nextTokenCanFollowModifier()
}

func isNativeModifierStart(kind ast.Kind) bool {
	switch kind {
	case ast.KindExportKeyword, ast.KindDefaultKeyword, ast.KindDeclareKeyword,
		ast.KindAsyncKeyword, ast.KindPublicKeyword, ast.KindPrivateKeyword,
		ast.KindProtectedKeyword, ast.KindReadonlyKeyword, ast.KindStaticKeyword,
		ast.KindAbstractKeyword, ast.KindOverrideKeyword, ast.KindAccessorKeyword:
		return true
	default:
		return false
	}
}

func nativeAppendModifier(factory *ast.Factory, modifiers ast.ListRef, extra ast.Handle) ast.ListRef {
	store := factory.Store()
	n := 0
	if modifiers != 0 {
		n = store.ListLen(modifiers)
	}
	elems := make([]ast.Handle, 0, n+1)
	pos := extra.Loc().Pos()
	if modifiers != 0 {
		pos = store.ListLoc(modifiers).Pos()
		for i := 0; i < n; i++ {
			elems = append(elems, store.ListAt(modifiers, i))
		}
	}
	elems = append(elems, extra)
	return factory.List(core.NewTextRange(pos, extra.Loc().End()), elems...)
}

func (p *Parser) takeNativeConstModifier(factory *ast.Factory) ast.ListRef {
	pos := p.nodePos()
	mod := p.parseNativeToken(factory)
	return factory.List(core.NewTextRange(pos, p.nodePos()), mod)
}

func (p *Parser) isNativeModuleDeclarationStart() bool {
	p.nextToken()
	return p.isIdentifier() || p.token == ast.KindStringLiteral
}

func (p *Parser) isNativeTypeAliasStart() bool {
	p.nextToken()
	return p.isIdentifier()
}

func (p *Parser) isNativeNamedDeclarationStart() bool {
	p.nextToken()
	return p.isIdentifier()
}

func (p *Parser) parseNativeEmptyStatement(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	return p.finishNativeHandle(factory, factory.NewEmptyStatement(), pos), true
}

func (p *Parser) parseNativeBlock(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	if p.token != ast.KindOpenBraceToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	multiLine := p.hasPrecedingLineBreak()
	listPos := p.nodePos()
	statements := make([]ast.Handle, 0, 8)
	for p.token != ast.KindCloseBraceToken && p.token != ast.KindEndOfFile {
		if p.nativeJSDocBlocksParse() {
			return ast.Handle{}, false
		}
		statement, ok := p.parseNativeStatement(factory)
		if !ok {
			return ast.Handle{}, false
		}
		statements = append(statements, statement)
	}
	if p.token != ast.KindCloseBraceToken {
		return ast.Handle{}, false
	}
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), statements...)
	p.nextToken()
	return p.finishNativeHandle(factory, factory.NewBlock(list, multiLine), pos), true
}

func (p *Parser) parseNativeVariableStatement(factory *ast.Factory, modifiers ast.ListRef) (ast.Handle, bool) {
	return p.parseNativeVariableStatementAt(factory, p.nodePos(), modifiers)
}

func (p *Parser) parseNativeVariableStatementAt(factory *ast.Factory, pos int, modifiers ast.ListRef) (ast.Handle, bool) {
	list, ok := p.parseNativeVariableDeclarationList(factory, false)
	if !ok || !p.tryParseNativeSemicolon() {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(factory, factory.NewVariableStatement(modifiers, list), pos), true
}

func (p *Parser) parseNativeVariableDeclarationList(factory *ast.Factory, inFor bool) (ast.Handle, bool) {
	pos := p.nodePos()
	var flags ast.NodeFlags
	switch p.token {
	case ast.KindVarKeyword:
		flags = ast.NodeFlagsNone
	case ast.KindLetKeyword:
		flags = ast.NodeFlagsLet
	case ast.KindConstKeyword:
		flags = ast.NodeFlagsConst
	default:
		return ast.Handle{}, false
	}
	p.nextToken()
	decls := make([]ast.Handle, 0, 2)
	listPos := p.nodePos()
	for {
		decl, ok := p.parseNativeVariableDeclaration(factory, inFor)
		if !ok {
			return ast.Handle{}, false
		}
		decls = append(decls, decl)
		if p.token != ast.KindCommaToken {
			break
		}
		p.nextToken()
	}
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), decls...)
	return p.finishNativeHandle(factory, factory.NewVariableDeclarationList(list, flags), pos), true
}

func (p *Parser) parseNativeVariableDeclaration(factory *ast.Factory, inFor bool) (ast.Handle, bool) {
	pos := p.nodePos()
	var name ast.Handle
	var ok bool
	if p.token == ast.KindOpenBraceToken || p.token == ast.KindOpenBracketToken {
		name, ok = p.parseNativeBindingPattern(factory)
		if !ok {
			return ast.Handle{}, false
		}
	} else {
		if !p.isBindingIdentifier() {
			return ast.Handle{}, false
		}
		name = p.parseNativeIdentifier(factory)
	}
	var bang ast.Handle
	if !inFor && p.token == ast.KindExclamationToken {
		bang = p.parseNativeToken(factory)
	}
	var typeNode ast.Handle
	if p.token == ast.KindColonToken {
		p.nextToken()
		typeNode, ok = p.parseNativeType(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	var initializer ast.Handle
	if p.token == ast.KindEqualsToken {
		p.nextToken()
		initializer, ok = p.parseNativeAssignmentExpression(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	return p.finishNativeHandle(factory, factory.NewVariableDeclaration(name, bang, typeNode, initializer), pos), true
}

func (p *Parser) parseNativeBindingPattern(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	var kind ast.Kind
	var close ast.Kind
	switch p.token {
	case ast.KindOpenBraceToken:
		kind = ast.KindObjectBindingPattern
		close = ast.KindCloseBraceToken
	case ast.KindOpenBracketToken:
		kind = ast.KindArrayBindingPattern
		close = ast.KindCloseBracketToken
	default:
		return ast.Handle{}, false
	}
	p.nextToken()
	listPos := p.nodePos()
	elems := make([]ast.Handle, 0, 4)
	for p.token != close && p.token != ast.KindEndOfFile {
		if kind == ast.KindArrayBindingPattern && p.token == ast.KindCommaToken {
			elems = append(elems, p.finishNativeHandle(factory, factory.NewBindingElement(ast.Handle{}, ast.Handle{}, ast.Handle{}, ast.Handle{}), p.nodePos()))
			p.nextToken()
			continue
		}
		elem, ok := p.parseNativeBindingElement(factory, kind)
		if !ok {
			return ast.Handle{}, false
		}
		elems = append(elems, elem)
		if p.token == ast.KindCommaToken {
			p.nextToken()
			continue
		}
		break
	}
	if p.token != close {
		return ast.Handle{}, false
	}
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), elems...)
	p.nextToken()
	return p.finishNativeHandle(factory, factory.NewBindingPattern(kind, list), pos), true
}

func (p *Parser) parseNativeBindingElement(factory *ast.Factory, patternKind ast.Kind) (ast.Handle, bool) {
	pos := p.nodePos()
	var rest ast.Handle
	if p.token == ast.KindDotDotDotToken {
		rest = p.parseNativeToken(factory)
	}
	var property ast.Handle
	var name ast.Handle
	var ok bool
	if patternKind == ast.KindObjectBindingPattern {
		if tokenIsIdentifierOrKeyword(p.token) {
			ident := p.parseNativeIdentifier(factory)
			if p.token == ast.KindColonToken {
				property = ident
				p.nextToken()
				name, ok = p.parseNativeBindingName(factory)
				if !ok {
					return ast.Handle{}, false
				}
			} else {
				name = ident
			}
		} else if p.token == ast.KindStringLiteral || p.token == ast.KindNumericLiteral {
			property = p.parseNativeLiteral(factory)
			if p.token != ast.KindColonToken {
				return ast.Handle{}, false
			}
			p.nextToken()
			name, ok = p.parseNativeBindingName(factory)
			if !ok {
				return ast.Handle{}, false
			}
		} else {
			return ast.Handle{}, false
		}
	} else {
		name, ok = p.parseNativeBindingName(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	var initializer ast.Handle
	if rest.Ref() == 0 && p.token == ast.KindEqualsToken {
		p.nextToken()
		initializer, ok = p.parseNativeAssignmentExpression(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	return p.finishNativeHandle(factory, factory.NewBindingElement(rest, property, name, initializer), pos), true
}

func (p *Parser) parseNativeBindingName(factory *ast.Factory) (ast.Handle, bool) {
	if p.token == ast.KindOpenBraceToken || p.token == ast.KindOpenBracketToken {
		return p.parseNativeBindingPattern(factory)
	}
	if !p.isBindingIdentifier() {
		return ast.Handle{}, false
	}
	return p.parseNativeIdentifier(factory), true
}

func (p *Parser) parseNativeFunctionDeclaration(factory *ast.Factory, modifiers ast.ListRef) (ast.Handle, bool) {
	return p.parseNativeFunctionDeclarationAt(factory, p.nodePos(), modifiers)
}

func (p *Parser) parseNativeFunctionDeclarationAt(factory *ast.Factory, pos int, modifiers ast.ListRef) (ast.Handle, bool) {
	if p.token != ast.KindFunctionKeyword {
		return ast.Handle{}, false
	}
	p.nextToken()
	var asterisk ast.Handle
	if p.token == ast.KindAsteriskToken {
		asterisk = p.parseNativeToken(factory)
	}
	var name ast.Handle
	if p.isBindingIdentifier() {
		name = p.parseNativeIdentifier(factory)
	} else if !nativeModifiersHasDefault(factory, modifiers) {
		return ast.Handle{}, false
	}
	typeParameters, ok := p.parseNativeTypeParameters(factory)
	if !ok {
		return ast.Handle{}, false
	}
	parameters, ok := p.parseNativeParameterList(factory)
	if !ok {
		return ast.Handle{}, false
	}
	var returnType ast.Handle
	if p.token == ast.KindColonToken {
		p.nextToken()
		returnType, ok = p.parseNativeType(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	var body ast.Handle
	if p.token == ast.KindOpenBraceToken {
		body, ok = p.parseNativeBlock(factory)
		if !ok {
			return ast.Handle{}, false
		}
	} else if !p.tryParseNativeSemicolon() {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(
		factory,
		factory.NewFunctionDeclaration(modifiers, asterisk, name, typeParameters, parameters, returnType, ast.Handle{}, body),
		pos,
	), true
}

func nativeModifiersHasDefault(factory *ast.Factory, modifiers ast.ListRef) bool {
	if modifiers == 0 {
		return false
	}
	store := factory.Store()
	for i := 0; i < store.ListLen(modifiers); i++ {
		if store.ListAt(modifiers, i).Kind() == ast.KindDefaultKeyword {
			return true
		}
	}
	return false
}

func (p *Parser) parseNativeTypeParameters(factory *ast.Factory) (ast.ListRef, bool) {
	if p.token != ast.KindLessThanToken && p.token != ast.KindLessThanLessThanToken {
		return 0, true
	}
	if p.reScanLessThanToken() != ast.KindLessThanToken {
		return 0, true
	}
	p.nextToken()
	pos := p.nodePos()
	params := make([]ast.Handle, 0, 2)
	for p.token != ast.KindGreaterThanToken {
		param, ok := p.parseNativeTypeParameter(factory)
		if !ok {
			return 0, false
		}
		params = append(params, param)
		if p.token != ast.KindCommaToken {
			break
		}
		p.nextToken()
	}
	if p.reScanGreaterThanToken() != ast.KindGreaterThanToken {
		return 0, false
	}
	list := factory.List(core.NewTextRange(pos, p.nodePos()), params...)
	p.nextToken()
	return list, true
}

func (p *Parser) parseNativeTypeParameter(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	if !p.isIdentifier() {
		return ast.Handle{}, false
	}
	name := p.parseNativeIdentifier(factory)
	var constraint ast.Handle
	var ok bool
	if p.token == ast.KindExtendsKeyword {
		p.nextToken()
		constraint, ok = p.parseNativeType(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	var def ast.Handle
	if p.token == ast.KindEqualsToken {
		p.nextToken()
		def, ok = p.parseNativeType(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	return p.finishNativeHandle(factory, factory.NewTypeParameterDeclaration(0, name, constraint, ast.Handle{}, def), pos), true
}

func (p *Parser) parseNativeParameterList(factory *ast.Factory) (ast.ListRef, bool) {
	if p.token != ast.KindOpenParenToken {
		return 0, false
	}
	p.nextToken()
	pos := p.nodePos()
	params := make([]ast.Handle, 0, 4)
	for p.token != ast.KindCloseParenToken {
		param, ok := p.parseNativeParameter(factory)
		if !ok {
			return 0, false
		}
		params = append(params, param)
		if p.token != ast.KindCommaToken {
			break
		}
		p.nextToken()
		if p.token == ast.KindCloseParenToken {
			break
		}
	}
	if p.token != ast.KindCloseParenToken {
		return 0, false
	}
	list := factory.List(core.NewTextRange(pos, p.nodePos()), params...)
	p.nextToken()
	return list, true
}

func (p *Parser) parseNativeParameter(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	if p.token == ast.KindThisKeyword {
		name := p.parseNativeIdentifier(factory)
		var typeNode ast.Handle
		var ok bool
		if p.token == ast.KindColonToken {
			p.nextToken()
			typeNode, ok = p.parseNativeType(factory)
			if !ok {
				return ast.Handle{}, false
			}
		}
		return p.finishNativeHandle(
			factory,
			factory.NewParameterDeclaration(0, ast.Handle{}, name, ast.Handle{}, typeNode, ast.Handle{}),
			pos,
		), true
	}
	var rest ast.Handle
	if p.token == ast.KindDotDotDotToken {
		rest = p.parseNativeToken(factory)
	}
	var name ast.Handle
	var ok bool
	if p.token == ast.KindOpenBraceToken || p.token == ast.KindOpenBracketToken {
		name, ok = p.parseNativeBindingPattern(factory)
		if !ok {
			return ast.Handle{}, false
		}
	} else {
		if !p.isBindingIdentifier() {
			return ast.Handle{}, false
		}
		name = p.parseNativeIdentifier(factory)
	}
	var question ast.Handle
	if p.token == ast.KindQuestionToken {
		question = p.parseNativeToken(factory)
	}
	var typeNode ast.Handle
	if p.token == ast.KindColonToken {
		p.nextToken()
		typeNode, ok = p.parseNativeType(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	var initializer ast.Handle
	if p.token == ast.KindEqualsToken {
		p.nextToken()
		initializer, ok = p.parseNativeAssignmentExpression(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	return p.finishNativeHandle(
		factory,
		factory.NewParameterDeclaration(0, rest, name, question, typeNode, initializer),
		pos,
	), true
}

func (p *Parser) parseNativeIfStatement(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	if p.token != ast.KindOpenParenToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	expression, ok := p.parseNativeExpression(factory)
	if !ok || p.token != ast.KindCloseParenToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	thenStatement, ok := p.parseNativeStatement(factory)
	if !ok {
		return ast.Handle{}, false
	}
	var elseStatement ast.Handle
	if p.token == ast.KindElseKeyword {
		p.nextToken()
		elseStatement, ok = p.parseNativeStatement(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	return p.finishNativeHandle(factory, factory.NewIfStatement(expression, thenStatement, elseStatement), pos), true
}

func (p *Parser) parseNativeDoStatement(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	statement, ok := p.parseNativeStatement(factory)
	if !ok || p.token != ast.KindWhileKeyword {
		return ast.Handle{}, false
	}
	p.nextToken()
	if p.token != ast.KindOpenParenToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	expression, ok := p.parseNativeExpression(factory)
	if !ok || p.token != ast.KindCloseParenToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	p.tryParseNativeSemicolon()
	return p.finishNativeHandle(factory, factory.NewDoStatement(statement, expression), pos), true
}

func (p *Parser) parseNativeWhileStatement(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	if p.token != ast.KindOpenParenToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	expression, ok := p.parseNativeExpression(factory)
	if !ok || p.token != ast.KindCloseParenToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	statement, ok := p.parseNativeStatement(factory)
	if !ok {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(factory, factory.NewWhileStatement(expression, statement), pos), true
}

func (p *Parser) parseNativeForStatement(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	if p.token == ast.KindAwaitKeyword {
		return ast.Handle{}, false
	}
	if p.token != ast.KindOpenParenToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	var initializer ast.Handle
	var ok bool
	if p.token != ast.KindSemicolonToken {
		switch p.token {
		case ast.KindVarKeyword, ast.KindLetKeyword, ast.KindConstKeyword:
			initializer, ok = p.parseNativeVariableDeclarationList(factory, true)
		default:
			initializer, ok = p.parseNativeExpression(factory)
		}
		if !ok {
			return ast.Handle{}, false
		}
	}
	if p.token == ast.KindInKeyword || p.token == ast.KindOfKeyword {
		kind := ast.KindForOfStatement
		if p.token == ast.KindInKeyword {
			kind = ast.KindForInStatement
		}
		p.nextToken()
		expression, ok := p.parseNativeExpression(factory)
		if !ok || p.token != ast.KindCloseParenToken {
			return ast.Handle{}, false
		}
		p.nextToken()
		statement, ok := p.parseNativeStatement(factory)
		if !ok {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(
			factory,
			factory.NewForInOrOfStatement(kind, ast.Handle{}, initializer, expression, statement),
			pos,
		), true
	}
	if p.token != ast.KindSemicolonToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	var condition ast.Handle
	if p.token != ast.KindSemicolonToken {
		condition, ok = p.parseNativeExpression(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	if p.token != ast.KindSemicolonToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	var incrementor ast.Handle
	if p.token != ast.KindCloseParenToken {
		incrementor, ok = p.parseNativeExpression(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	if p.token != ast.KindCloseParenToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	statement, ok := p.parseNativeStatement(factory)
	if !ok {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(factory, factory.NewForStatement(initializer, condition, incrementor, statement), pos), true
}

func (p *Parser) parseNativeJumpStatement(factory *ast.Factory, kind ast.Kind) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	var label ast.Handle
	if !p.canParseSemicolon() && p.isIdentifier() {
		label = p.parseNativeIdentifier(factory)
	}
	if !p.tryParseNativeSemicolon() {
		return ast.Handle{}, false
	}
	if kind == ast.KindBreakStatement {
		return p.finishNativeHandle(factory, factory.NewBreakStatement(label), pos), true
	}
	return p.finishNativeHandle(factory, factory.NewContinueStatement(label), pos), true
}

func (p *Parser) parseNativeReturnStatement(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	var expression ast.Handle
	var ok bool
	if !p.canParseSemicolon() {
		expression, ok = p.parseNativeExpression(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	if !p.tryParseNativeSemicolon() {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(factory, factory.NewReturnStatement(expression), pos), true
}

func (p *Parser) parseNativeThrowStatement(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	if p.canParseSemicolon() {
		return ast.Handle{}, false
	}
	expression, ok := p.parseNativeExpression(factory)
	if !ok || !p.tryParseNativeSemicolon() {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(factory, factory.NewThrowStatement(expression), pos), true
}

func (p *Parser) parseNativeDebuggerStatement(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	if !p.tryParseNativeSemicolon() {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(factory, factory.NewDebuggerStatement(), pos), true
}

func (p *Parser) parseNativeTryStatement(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	tryBlock, ok := p.parseNativeBlock(factory)
	if !ok {
		return ast.Handle{}, false
	}
	var catchClause ast.Handle
	if p.token == ast.KindCatchKeyword {
		catchClause, ok = p.parseNativeCatchClause(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	var finallyBlock ast.Handle
	if p.token == ast.KindFinallyKeyword {
		p.nextToken()
		finallyBlock, ok = p.parseNativeBlock(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	if catchClause.Ref() == 0 && finallyBlock.Ref() == 0 {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(factory, factory.NewTryStatement(tryBlock, catchClause, finallyBlock), pos), true
}

func (p *Parser) parseNativeCatchClause(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	var variable ast.Handle
	if p.token == ast.KindOpenParenToken {
		p.nextToken()
		if !p.isBindingIdentifier() {
			return ast.Handle{}, false
		}
		declPos := p.nodePos()
		name := p.parseNativeIdentifier(factory)
		var typeNode ast.Handle
		var ok bool
		if p.token == ast.KindColonToken {
			p.nextToken()
			typeNode, ok = p.parseNativeType(factory)
			if !ok {
				return ast.Handle{}, false
			}
		}
		variable = p.finishNativeHandle(factory, factory.NewVariableDeclaration(name, ast.Handle{}, typeNode, ast.Handle{}), declPos)
		if p.token != ast.KindCloseParenToken {
			return ast.Handle{}, false
		}
		p.nextToken()
	}
	block, ok := p.parseNativeBlock(factory)
	if !ok {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(factory, factory.NewCatchClause(variable, block), pos), true
}

func (p *Parser) parseNativeSwitchStatement(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	if p.token != ast.KindOpenParenToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	expression, ok := p.parseNativeExpression(factory)
	if !ok || p.token != ast.KindCloseParenToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	block, ok := p.parseNativeCaseBlock(factory)
	if !ok {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(factory, factory.NewSwitchStatement(expression, block), pos), true
}

func (p *Parser) parseNativeCaseBlock(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	if p.token != ast.KindOpenBraceToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	listPos := p.nodePos()
	clauses := make([]ast.Handle, 0, 4)
	for p.token != ast.KindCloseBraceToken && p.token != ast.KindEndOfFile {
		clause, ok := p.parseNativeCaseOrDefaultClause(factory)
		if !ok {
			return ast.Handle{}, false
		}
		clauses = append(clauses, clause)
	}
	if p.token != ast.KindCloseBraceToken {
		return ast.Handle{}, false
	}
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), clauses...)
	p.nextToken()
	return p.finishNativeHandle(factory, factory.NewCaseBlock(list), pos), true
}

func (p *Parser) parseNativeCaseOrDefaultClause(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	var kind ast.Kind
	var expression ast.Handle
	var ok bool
	switch p.token {
	case ast.KindCaseKeyword:
		kind = ast.KindCaseClause
		p.nextToken()
		expression, ok = p.parseNativeExpression(factory)
		if !ok {
			return ast.Handle{}, false
		}
	case ast.KindDefaultKeyword:
		kind = ast.KindDefaultClause
		p.nextToken()
	default:
		return ast.Handle{}, false
	}
	if p.token != ast.KindColonToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	listPos := p.nodePos()
	statements := make([]ast.Handle, 0, 4)
	for p.token != ast.KindCaseKeyword && p.token != ast.KindDefaultKeyword &&
		p.token != ast.KindCloseBraceToken && p.token != ast.KindEndOfFile {
		if p.nativeJSDocBlocksParse() {
			return ast.Handle{}, false
		}
		statement, ok := p.parseNativeStatement(factory)
		if !ok {
			return ast.Handle{}, false
		}
		statements = append(statements, statement)
	}
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), statements...)
	return p.finishNativeHandle(factory, factory.NewCaseOrDefaultClause(kind, expression, list), pos), true
}

func (p *Parser) parseNativeTypeAliasDeclaration(factory *ast.Factory, modifiers ast.ListRef) (ast.Handle, bool) {
	return p.parseNativeTypeAliasDeclarationAt(factory, p.nodePos(), modifiers)
}

func (p *Parser) parseNativeTypeAliasDeclarationAt(factory *ast.Factory, pos int, modifiers ast.ListRef) (ast.Handle, bool) {
	if p.token != ast.KindTypeKeyword {
		return ast.Handle{}, false
	}
	p.nextToken()
	if !p.isIdentifier() {
		return ast.Handle{}, false
	}
	name := p.parseNativeIdentifier(factory)
	typeParameters, ok := p.parseNativeTypeParameters(factory)
	if !ok || p.token != ast.KindEqualsToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	typeNode, ok := p.parseNativeType(factory)
	if !ok || !p.tryParseNativeSemicolon() {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(factory, factory.NewTypeAliasDeclaration(modifiers, name, typeParameters, typeNode), pos), true
}

func (p *Parser) parseNativeInterfaceDeclaration(factory *ast.Factory, modifiers ast.ListRef) (ast.Handle, bool) {
	return p.parseNativeInterfaceDeclarationAt(factory, p.nodePos(), modifiers)
}

func (p *Parser) parseNativeInterfaceDeclarationAt(factory *ast.Factory, pos int, modifiers ast.ListRef) (ast.Handle, bool) {
	if p.token != ast.KindInterfaceKeyword {
		return ast.Handle{}, false
	}
	p.nextToken()
	if !p.isIdentifier() {
		return ast.Handle{}, false
	}
	name := p.parseNativeIdentifier(factory)
	typeParameters, ok := p.parseNativeTypeParameters(factory)
	if !ok {
		return ast.Handle{}, false
	}
	heritage, ok := p.parseNativeHeritageClauses(factory, true)
	if !ok {
		return ast.Handle{}, false
	}
	members, ok := p.parseNativeTypeMemberList(factory)
	if !ok {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(
		factory,
		factory.NewInterfaceDeclaration(modifiers, name, typeParameters, heritage, members),
		pos,
	), true
}

func (p *Parser) parseNativeTypeMemberList(factory *ast.Factory) (ast.ListRef, bool) {
	if p.token != ast.KindOpenBraceToken {
		return 0, false
	}
	p.nextToken()
	pos := p.nodePos()
	members := make([]ast.Handle, 0, 8)
	for p.token != ast.KindCloseBraceToken && p.token != ast.KindEndOfFile {
		member, ok := p.parseNativeTypeElement(factory)
		if !ok {
			return 0, false
		}
		members = append(members, member)
		if p.token == ast.KindSemicolonToken || p.token == ast.KindCommaToken {
			p.nextToken()
		} else if p.token != ast.KindCloseBraceToken {
			return 0, false
		}
	}
	if p.token != ast.KindCloseBraceToken {
		return 0, false
	}
	list := factory.List(core.NewTextRange(pos, p.nodePos()), members...)
	p.nextToken()
	return list, true
}

func (p *Parser) parseNativeHeritageClauses(factory *ast.Factory, isInterface bool) (ast.ListRef, bool) {
	if p.token != ast.KindExtendsKeyword && (!isInterface || p.token != ast.KindImplementsKeyword) &&
		(isInterface || p.token != ast.KindImplementsKeyword) {
		if p.token != ast.KindExtendsKeyword && p.token != ast.KindImplementsKeyword {
			return 0, true
		}
	}
	pos := p.nodePos()
	clauses := make([]ast.Handle, 0, 2)
	for p.token == ast.KindExtendsKeyword || p.token == ast.KindImplementsKeyword {
		clause, ok := p.parseNativeHeritageClause(factory)
		if !ok {
			return 0, false
		}
		clauses = append(clauses, clause)
	}
	if len(clauses) == 0 {
		return 0, true
	}
	return factory.List(core.NewTextRange(pos, p.nodePos()), clauses...), true
}

func (p *Parser) parseNativeHeritageClause(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	token := p.token
	p.nextToken()
	listPos := p.nodePos()
	types := make([]ast.Handle, 0, 2)
	for {
		expr, ok := p.parseNativeLeftHandSideExpression(factory)
		if !ok {
			return ast.Handle{}, false
		}
		var arguments ast.ListRef
		if p.token == ast.KindLessThanToken || p.token == ast.KindLessThanLessThanToken {
			arguments, ok = p.parseNativeTypeArguments(factory)
			if !ok {
				return ast.Handle{}, false
			}
		}
		types = append(types, p.finishNativeHandle(factory, factory.NewExpressionWithTypeArguments(expr, arguments), expr.Loc().Pos()))
		if p.token != ast.KindCommaToken {
			break
		}
		p.nextToken()
	}
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), types...)
	return p.finishNativeHandle(factory, factory.NewHeritageClause(token, list), pos), true
}

func (p *Parser) parseNativeClassDeclaration(factory *ast.Factory, modifiers ast.ListRef) (ast.Handle, bool) {
	return p.parseNativeClassDeclarationAt(factory, p.nodePos(), modifiers)
}

func (p *Parser) parseNativeClassDeclarationAt(factory *ast.Factory, pos int, modifiers ast.ListRef) (ast.Handle, bool) {
	if p.token != ast.KindClassKeyword {
		return ast.Handle{}, false
	}
	p.nextToken()
	var name ast.Handle
	if p.isBindingIdentifier() && p.token != ast.KindImplementsKeyword {
		name = p.parseNativeIdentifier(factory)
	}
	typeParameters, ok := p.parseNativeTypeParameters(factory)
	if !ok {
		return ast.Handle{}, false
	}
	heritage, ok := p.parseNativeHeritageClauses(factory, false)
	if !ok {
		return ast.Handle{}, false
	}
	members, ok := p.parseNativeClassMembers(factory)
	if !ok {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(
		factory,
		factory.NewClassDeclaration(modifiers, name, typeParameters, heritage, members),
		pos,
	), true
}

func (p *Parser) parseNativeClassMembers(factory *ast.Factory) (ast.ListRef, bool) {
	if p.token != ast.KindOpenBraceToken {
		return 0, false
	}
	p.nextToken()
	pos := p.nodePos()
	members := make([]ast.Handle, 0, 8)
	for p.token != ast.KindCloseBraceToken && p.token != ast.KindEndOfFile {
		if p.token == ast.KindSemicolonToken {
			semiPos := p.nodePos()
			p.nextToken()
			members = append(members, p.finishNativeHandle(factory, factory.NewSemicolonClassElement(), semiPos))
			continue
		}
		member, ok := p.parseNativeClassElement(factory)
		if !ok {
			return 0, false
		}
		members = append(members, member)
	}
	if p.token != ast.KindCloseBraceToken {
		return 0, false
	}
	list := factory.List(core.NewTextRange(pos, p.nodePos()), members...)
	p.nextToken()
	return list, true
}

func (p *Parser) parseNativeClassElement(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	if p.nativeJSDocBlocksParse() || p.token == ast.KindAtToken {
		return ast.Handle{}, false
	}
	modifiers, ok := p.parseNativeModifiers(factory)
	if !ok {
		return ast.Handle{}, false
	}
	if p.token == ast.KindConstructorKeyword {
		return p.parseNativeConstructor(factory, pos, modifiers)
	}
	var asterisk ast.Handle
	if p.token == ast.KindAsteriskToken {
		asterisk = p.parseNativeToken(factory)
	}
	name, ok := p.parseNativePropertyName(factory)
	if !ok {
		return ast.Handle{}, false
	}
	var postfix ast.Handle
	if p.token == ast.KindQuestionToken || p.token == ast.KindExclamationToken {
		postfix = p.parseNativeToken(factory)
	}
	if p.token == ast.KindOpenParenToken || p.token == ast.KindLessThanToken {
		typeParameters, ok := p.parseNativeTypeParameters(factory)
		if !ok {
			return ast.Handle{}, false
		}
		parameters, ok := p.parseNativeParameterList(factory)
		if !ok {
			return ast.Handle{}, false
		}
		var typeNode ast.Handle
		if p.token == ast.KindColonToken {
			p.nextToken()
			typeNode, ok = p.parseNativeType(factory)
			if !ok {
				return ast.Handle{}, false
			}
		}
		var body ast.Handle
		if p.token == ast.KindOpenBraceToken {
			body, ok = p.parseNativeBlock(factory)
			if !ok {
				return ast.Handle{}, false
			}
		} else if !p.tryParseNativeSemicolon() {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(
			factory,
			factory.NewMethodDeclaration(modifiers, asterisk, name, postfix, typeParameters, parameters, typeNode, ast.Handle{}, body),
			pos,
		), true
	}
	var typeNode ast.Handle
	if p.token == ast.KindColonToken {
		p.nextToken()
		typeNode, ok = p.parseNativeType(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	var initializer ast.Handle
	if p.token == ast.KindEqualsToken {
		p.nextToken()
		initializer, ok = p.parseNativeAssignmentExpression(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	if !p.tryParseNativeSemicolon() {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(
		factory,
		factory.NewPropertyDeclaration(modifiers, name, postfix, typeNode, initializer),
		pos,
	), true
}

func (p *Parser) parseNativeConstructor(factory *ast.Factory, pos int, modifiers ast.ListRef) (ast.Handle, bool) {
	p.nextToken()
	parameters, ok := p.parseNativeParameterList(factory)
	if !ok {
		return ast.Handle{}, false
	}
	var body ast.Handle
	if p.token == ast.KindOpenBraceToken {
		body, ok = p.parseNativeBlock(factory)
		if !ok {
			return ast.Handle{}, false
		}
	} else if !p.tryParseNativeSemicolon() {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(
		factory,
		factory.NewConstructorDeclaration(modifiers, 0, parameters, ast.Handle{}, ast.Handle{}, body),
		pos,
	), true
}

func (p *Parser) parseNativeEnumDeclaration(factory *ast.Factory, modifiers ast.ListRef) (ast.Handle, bool) {
	return p.parseNativeEnumDeclarationAt(factory, p.nodePos(), modifiers)
}

func (p *Parser) parseNativeEnumDeclarationAt(factory *ast.Factory, pos int, modifiers ast.ListRef) (ast.Handle, bool) {
	if p.token != ast.KindEnumKeyword {
		return ast.Handle{}, false
	}
	p.nextToken()
	if !p.isIdentifier() {
		return ast.Handle{}, false
	}
	name := p.parseNativeIdentifier(factory)
	if p.token != ast.KindOpenBraceToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	listPos := p.nodePos()
	members := make([]ast.Handle, 0, 8)
	for p.token != ast.KindCloseBraceToken && p.token != ast.KindEndOfFile {
		member, ok := p.parseNativeEnumMember(factory)
		if !ok {
			return ast.Handle{}, false
		}
		members = append(members, member)
		if p.token == ast.KindCommaToken {
			p.nextToken()
		} else if p.token != ast.KindCloseBraceToken {
			return ast.Handle{}, false
		}
	}
	if p.token != ast.KindCloseBraceToken {
		return ast.Handle{}, false
	}
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), members...)
	p.nextToken()
	return p.finishNativeHandle(factory, factory.NewEnumDeclaration(modifiers, name, list), pos), true
}

func (p *Parser) parseNativeEnumMember(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	name, ok := p.parseNativePropertyName(factory)
	if !ok {
		return ast.Handle{}, false
	}
	var initializer ast.Handle
	if p.token == ast.KindEqualsToken {
		p.nextToken()
		initializer, ok = p.parseNativeAssignmentExpression(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	return p.finishNativeHandle(factory, factory.NewEnumMember(name, initializer), pos), true
}

func (p *Parser) parseNativeModuleDeclaration(factory *ast.Factory, modifiers ast.ListRef) (ast.Handle, bool) {
	return p.parseNativeModuleDeclarationAt(factory, p.nodePos(), modifiers)
}

func (p *Parser) parseNativeModuleDeclarationAt(factory *ast.Factory, pos int, modifiers ast.ListRef) (ast.Handle, bool) {
	if p.token != ast.KindModuleKeyword && p.token != ast.KindNamespaceKeyword {
		return ast.Handle{}, false
	}
	keyword := p.token
	p.nextToken()
	if p.token == ast.KindStringLiteral {
		name := p.parseNativeLiteral(factory)
		body, ok := p.parseNativeModuleBlock(factory)
		if !ok {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(factory, factory.NewModuleDeclaration(modifiers, keyword, name, body), pos), true
	}
	return p.parseNativeModuleOrNamespaceDeclarationAt(factory, pos, modifiers, false, keyword)
}

func (p *Parser) parseNativeModuleOrNamespaceDeclarationAt(factory *ast.Factory, pos int, modifiers ast.ListRef, nested bool, keyword ast.Kind) (ast.Handle, bool) {
	var name ast.Handle
	if nested {
		var ok bool
		name, ok = p.parseNativeIdentifierName(factory)
		if !ok {
			return ast.Handle{}, false
		}
	} else {
		if !p.isIdentifier() {
			return ast.Handle{}, false
		}
		name = p.parseNativeIdentifier(factory)
	}
	var body ast.Handle
	var ok bool
	if p.token == ast.KindDotToken {
		p.nextToken()
		exportPos := p.nodePos()
		exportTok := p.finishNativeHandle(factory, factory.NewToken(ast.KindExportKeyword), exportPos)
		implicitMods := factory.List(core.NewTextRange(exportPos, exportPos), exportTok)
		body, ok = p.parseNativeModuleOrNamespaceDeclarationAt(factory, p.nodePos(), implicitMods, true, keyword)
		if !ok {
			return ast.Handle{}, false
		}
	} else {
		body, ok = p.parseNativeModuleBlock(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	return p.finishNativeHandle(factory, factory.NewModuleDeclaration(modifiers, keyword, name, body), pos), true
}

func (p *Parser) parseNativeModuleBlock(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	if p.token != ast.KindOpenBraceToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	listPos := p.nodePos()
	statements := make([]ast.Handle, 0, 8)
	for p.token != ast.KindCloseBraceToken && p.token != ast.KindEndOfFile {
		if p.nativeJSDocBlocksParse() {
			return ast.Handle{}, false
		}
		statement, ok := p.parseNativeStatement(factory)
		if !ok {
			return ast.Handle{}, false
		}
		statements = append(statements, statement)
	}
	if p.token != ast.KindCloseBraceToken {
		return ast.Handle{}, false
	}
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), statements...)
	p.nextToken()
	return p.finishNativeHandle(factory, factory.NewModuleBlock(list), pos), true
}

func (p *Parser) parseNativeImportDeclaration(factory *ast.Factory, modifiers ast.ListRef) (ast.Handle, bool) {
	return p.parseNativeImportDeclarationAt(factory, p.nodePos(), modifiers)
}

func (p *Parser) parseNativeImportDeclarationAt(factory *ast.Factory, pos int, modifiers ast.ListRef) (ast.Handle, bool) {
	if p.token != ast.KindImportKeyword {
		return ast.Handle{}, false
	}
	p.nextToken()
	if p.token == ast.KindStringLiteral {
		spec := p.parseNativeLiteral(factory)
		if !p.tryParseNativeSemicolon() {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(factory, factory.NewImportDeclaration(modifiers, ast.Handle{}, spec, ast.Handle{}), pos), true
	}
	clause, ok := p.parseNativeImportClause(factory)
	if !ok || p.token != ast.KindFromKeyword {
		return ast.Handle{}, false
	}
	p.nextToken()
	if p.token != ast.KindStringLiteral {
		return ast.Handle{}, false
	}
	spec := p.parseNativeLiteral(factory)
	if !p.tryParseNativeSemicolon() {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(factory, factory.NewImportDeclaration(modifiers, clause, spec, ast.Handle{}), pos), true
}

func (p *Parser) parseNativeImportClause(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	phase := ast.KindUnknown
	if p.token == ast.KindTypeKeyword {
		phase = ast.KindTypeKeyword
		p.nextToken()
	}
	var name ast.Handle
	var named ast.Handle
	if p.isIdentifier() && p.token != ast.KindFromKeyword {
		name = p.parseNativeIdentifier(factory)
		if p.token == ast.KindCommaToken {
			p.nextToken()
			var ok bool
			named, ok = p.parseNativeNamedOrNamespaceImport(factory)
			if !ok {
				return ast.Handle{}, false
			}
		}
	} else {
		var ok bool
		named, ok = p.parseNativeNamedOrNamespaceImport(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	return p.finishNativeHandle(
		factory,
		factory.NewImportClause(ast.ImportPhaseModifierSyntaxKind(phase), name, named),
		pos,
	), true
}

func (p *Parser) parseNativeNamedOrNamespaceImport(factory *ast.Factory) (ast.Handle, bool) {
	if p.token == ast.KindAsteriskToken {
		pos := p.nodePos()
		p.nextToken()
		if p.token != ast.KindAsKeyword {
			return ast.Handle{}, false
		}
		p.nextToken()
		if !p.isIdentifier() {
			return ast.Handle{}, false
		}
		name := p.parseNativeIdentifier(factory)
		return p.finishNativeHandle(factory, factory.NewNamespaceImport(name), pos), true
	}
	if p.token != ast.KindOpenBraceToken {
		return ast.Handle{}, false
	}
	pos := p.nodePos()
	p.nextToken()
	listPos := p.nodePos()
	elems := make([]ast.Handle, 0, 4)
	for p.token != ast.KindCloseBraceToken && p.token != ast.KindEndOfFile {
		elem, ok := p.parseNativeImportSpecifier(factory)
		if !ok {
			return ast.Handle{}, false
		}
		elems = append(elems, elem)
		if p.token != ast.KindCommaToken {
			break
		}
		p.nextToken()
	}
	if p.token != ast.KindCloseBraceToken {
		return ast.Handle{}, false
	}
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), elems...)
	p.nextToken()
	return p.finishNativeHandle(factory, factory.NewNamedImports(list), pos), true
}

func (p *Parser) parseNativeImportSpecifier(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	isTypeOnly := false
	if p.token == ast.KindTypeKeyword {
		isTypeOnly = true
		p.nextToken()
	}
	if !tokenIsIdentifierOrKeyword(p.token) {
		return ast.Handle{}, false
	}
	name := p.parseNativeIdentifier(factory)
	var property ast.Handle
	if p.token == ast.KindAsKeyword {
		property = name
		p.nextToken()
		if !tokenIsIdentifierOrKeyword(p.token) {
			return ast.Handle{}, false
		}
		name = p.parseNativeIdentifier(factory)
	}
	return p.finishNativeHandle(factory, factory.NewImportSpecifier(isTypeOnly, property, name), pos), true
}

func (p *Parser) parseNativeExportAfterModifiers(factory *ast.Factory, pos int, modifiers ast.ListRef) (ast.Handle, bool) {
	if nativeModifiersHasDefault(factory, modifiers) {
		expr, ok := p.parseNativeAssignmentExpression(factory)
		if !ok || !p.tryParseNativeSemicolon() {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(factory, factory.NewExportAssignment(modifiers, false, ast.Handle{}, expr), pos), true
	}
	if p.token == ast.KindDefaultKeyword {
		p.nextToken()
		expr, ok := p.parseNativeAssignmentExpression(factory)
		if !ok || !p.tryParseNativeSemicolon() {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(factory, factory.NewExportAssignment(modifiers, false, ast.Handle{}, expr), pos), true
	}
	if p.token == ast.KindEqualsToken {
		p.nextToken()
		expr, ok := p.parseNativeAssignmentExpression(factory)
		if !ok || !p.tryParseNativeSemicolon() {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(factory, factory.NewExportAssignment(modifiers, true, ast.Handle{}, expr), pos), true
	}
	if p.token == ast.KindAsKeyword {
		p.nextToken()
		if p.token != ast.KindNamespaceKeyword {
			return ast.Handle{}, false
		}
		p.nextToken()
		if !p.isIdentifier() {
			return ast.Handle{}, false
		}
		name := p.parseNativeIdentifier(factory)
		if !p.tryParseNativeSemicolon() {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(factory, factory.NewNamespaceExportDeclaration(modifiers, name), pos), true
	}
	isTypeOnly := false
	if p.token == ast.KindTypeKeyword {
		isTypeOnly = true
		p.nextToken()
	}
	if p.token == ast.KindAsteriskToken {
		p.nextToken()
		var clause ast.Handle
		if p.token == ast.KindAsKeyword {
			p.nextToken()
			if !p.isIdentifier() {
				return ast.Handle{}, false
			}
			clause = p.finishNativeHandle(factory, factory.NewNamespaceExport(p.parseNativeIdentifier(factory)), p.nodePos())
		}
		if p.token != ast.KindFromKeyword {
			return ast.Handle{}, false
		}
		p.nextToken()
		if p.token != ast.KindStringLiteral {
			return ast.Handle{}, false
		}
		spec := p.parseNativeLiteral(factory)
		if !p.tryParseNativeSemicolon() {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(factory, factory.NewExportDeclaration(modifiers, isTypeOnly, clause, spec, ast.Handle{}), pos), true
	}
	if p.token == ast.KindOpenBraceToken {
		clause, ok := p.parseNativeNamedExports(factory)
		if !ok {
			return ast.Handle{}, false
		}
		var spec ast.Handle
		if p.token == ast.KindFromKeyword {
			p.nextToken()
			if p.token != ast.KindStringLiteral {
				return ast.Handle{}, false
			}
			spec = p.parseNativeLiteral(factory)
		}
		if !p.tryParseNativeSemicolon() {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(factory, factory.NewExportDeclaration(modifiers, isTypeOnly, clause, spec, ast.Handle{}), pos), true
	}
	return ast.Handle{}, false
}

func (p *Parser) parseNativeNamedExports(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	listPos := p.nodePos()
	elems := make([]ast.Handle, 0, 4)
	for p.token != ast.KindCloseBraceToken && p.token != ast.KindEndOfFile {
		elem, ok := p.parseNativeExportSpecifier(factory)
		if !ok {
			return ast.Handle{}, false
		}
		elems = append(elems, elem)
		if p.token != ast.KindCommaToken {
			break
		}
		p.nextToken()
	}
	if p.token != ast.KindCloseBraceToken {
		return ast.Handle{}, false
	}
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), elems...)
	p.nextToken()
	return p.finishNativeHandle(factory, factory.NewNamedExports(list), pos), true
}

func (p *Parser) parseNativeExportSpecifier(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	isTypeOnly := false
	if p.token == ast.KindTypeKeyword {
		isTypeOnly = true
		p.nextToken()
	}
	if !tokenIsIdentifierOrKeyword(p.token) {
		return ast.Handle{}, false
	}
	name := p.parseNativeIdentifier(factory)
	var property ast.Handle
	if p.token == ast.KindAsKeyword {
		property = name
		p.nextToken()
		if !tokenIsIdentifierOrKeyword(p.token) {
			return ast.Handle{}, false
		}
		name = p.parseNativeIdentifier(factory)
	}
	return p.finishNativeHandle(factory, factory.NewExportSpecifier(isTypeOnly, property, name), pos), true
}

func (p *Parser) parseNativeExpressionOrLabeledStatement(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	expression, ok := p.parseNativeExpression(factory)
	if !ok {
		return ast.Handle{}, false
	}
	if p.token == ast.KindColonToken && expression.Kind() == ast.KindIdentifier {
		p.nextToken()
		statement, ok := p.parseNativeStatement(factory)
		if !ok {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(factory, factory.NewLabeledStatement(expression, statement), pos), true
	}
	if !p.tryParseNativeSemicolon() {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(factory, factory.NewExpressionStatement(expression), pos), true
}

func (p *Parser) parseNativeExpressionStatement(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	expression, ok := p.parseNativeExpression(factory)
	if !ok || !p.tryParseNativeSemicolon() {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(factory, factory.NewExpressionStatement(expression), pos), true
}

func (p *Parser) tryParseNativeSemicolon() bool {
	if p.token == ast.KindSemicolonToken {
		p.nextToken()
		return true
	}
	return p.canParseSemicolon()
}
