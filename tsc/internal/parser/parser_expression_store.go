package parser

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// tryParseExpressionSourceHandle parses a complete source file whose top-level
// elements are expression statements. The producer is deliberately
// Handle-native: unsupported or malformed syntax returns false and the caller
// rewinds into the full recovery parser.
func (p *Parser) tryParseExpressionSourceHandle(factory *ast.Factory) (ast.Handle, bool) {
	state := p.mark()
	identifierCount := p.identifierCount
	root, ok := p.parseExpressionSourceHandle(factory)
	if !ok || state.diagnosticsLen != 0 || len(p.diagnostics) != 0 {
		p.rewind(state)
		p.identifierCount = identifierCount
		return ast.Handle{}, false
	}
	return root, true
}

func (p *Parser) parseExpressionSourceHandle(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	statements := make([]ast.Handle, 0, 8)

	for p.token != ast.KindEndOfFile {
		// A leading object literal is a block in statement position. JSDoc has
		// pointer-only attachment state and therefore remains on the legacy path.
		if p.token == ast.KindOpenBraceToken || p.jsdocScannerInfo() != 0 {
			return ast.Handle{}, false
		}
		statementPos := p.nodePos()
		expression, ok := p.parseNativeExpression(factory)
		if !ok {
			return ast.Handle{}, false
		}
		if p.token == ast.KindColonToken {
			// Labeled statements are outside this migrated producer.
			return ast.Handle{}, false
		}
		if p.token == ast.KindSemicolonToken {
			p.nextToken()
		} else if p.token != ast.KindEndOfFile && !p.hasPrecedingLineBreak() {
			return ast.Handle{}, false
		}
		statement := factory.NewExpressionStatement(expression)
		statements = append(statements, p.finishNativeHandle(factory, statement, statementPos))
	}
	// JSDoc can attach to the EOF token in comment-only files.
	if p.jsdocScannerInfo() != 0 {
		return ast.Handle{}, false
	}

	list := factory.List(core.NewTextRange(pos, p.nodePos()), statements...)
	eof := p.parseNativeToken(factory)
	root := factory.NewSourceFile(list, eof)
	return p.finishNativeHandle(factory, root, pos), true
}

func (p *Parser) parseNativeExpression(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	expression, ok := p.parseNativeAssignmentExpression(factory)
	if !ok {
		return ast.Handle{}, false
	}
	for p.token == ast.KindCommaToken {
		operator := p.parseNativeToken(factory)
		right, ok := p.parseNativeAssignmentExpression(factory)
		if !ok {
			return ast.Handle{}, false
		}
		expression = p.finishNativeHandle(
			factory,
			factory.NewBinaryExpression(0, expression, ast.Handle{}, operator, right),
			pos,
		)
	}
	return expression, true
}

func (p *Parser) parseNativeAssignmentExpression(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	expression, ok := p.parseNativeBinaryExpression(factory, ast.OperatorPrecedenceLowest)
	if !ok {
		return ast.Handle{}, false
	}
	if p.token != ast.KindQuestionToken {
		// Assignment and arrow expressions intentionally fall back until their
		// left-hand-side and parameter diagnostics can migrate with them.
		if ast.IsAssignmentOperator(p.reScanGreaterThanToken()) || p.token == ast.KindEqualsGreaterThanToken {
			return ast.Handle{}, false
		}
		return expression, true
	}

	question := p.parseNativeToken(factory)
	whenTrue, ok := p.parseNativeAssignmentExpression(factory)
	if !ok || p.token != ast.KindColonToken {
		return ast.Handle{}, false
	}
	colon := p.parseNativeToken(factory)
	whenFalse, ok := p.parseNativeAssignmentExpression(factory)
	if !ok {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(
		factory,
		factory.NewConditionalExpression(expression, question, whenTrue, colon, whenFalse),
		pos,
	), true
}

func (p *Parser) parseNativeBinaryExpression(factory *ast.Factory, precedence ast.OperatorPrecedence) (ast.Handle, bool) {
	pos := p.nodePos()
	left, ok := p.parseNativeUnaryExpression(factory)
	if !ok {
		return ast.Handle{}, false
	}
	for {
		operator := p.reScanGreaterThanToken()
		newPrecedence := ast.GetBinaryOperatorPrecedence(operator)
		if !shouldConsumeBinaryOperator(operator, newPrecedence, precedence) {
			break
		}
		// These operators require the TypeScript type grammar, which is not part
		// of the expression slice.
		if operator == ast.KindAsKeyword || operator == ast.KindSatisfiesKeyword {
			break
		}
		// In TypeScript, relational angle brackets are ambiguous with generic
		// calls and tagged templates. Conservatively retain the full parser for
		// those two operators until type arguments are Handle-native.
		if operator == ast.KindLessThanToken || operator == ast.KindGreaterThanToken {
			return ast.Handle{}, false
		}
		operatorToken := p.parseNativeToken(factory)
		right, ok := p.parseNativeBinaryExpression(factory, newPrecedence)
		if !ok {
			return ast.Handle{}, false
		}
		left = p.finishNativeHandle(
			factory,
			factory.NewBinaryExpression(0, left, ast.Handle{}, operatorToken, right),
			pos,
		)
	}
	return left, true
}

func (p *Parser) parseNativeUnaryExpression(factory *ast.Factory) (ast.Handle, bool) {
	switch p.token {
	case ast.KindPlusPlusToken, ast.KindMinusMinusToken:
		pos := p.nodePos()
		operator := p.token
		p.nextToken()
		operand, ok := p.parseNativeLeftHandSideExpression(factory)
		if !ok {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(factory, factory.NewPrefixUnaryExpression(operator, operand), pos), true
	case ast.KindPlusToken, ast.KindMinusToken, ast.KindTildeToken, ast.KindExclamationToken:
		pos := p.nodePos()
		operator := p.token
		p.nextToken()
		operand, ok := p.parseNativeUnaryExpression(factory)
		if !ok || p.token == ast.KindAsteriskAsteriskToken {
			// The legacy parser emits a targeted diagnostic for unary **.
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(factory, factory.NewPrefixUnaryExpression(operator, operand), pos), true
	}
	return p.parseNativeLeftHandSideExpression(factory)
}

func (p *Parser) parseNativeLeftHandSideExpression(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	expression, ok := p.parseNativePrimaryExpression(factory)
	if !ok {
		return ast.Handle{}, false
	}

	for {
		if p.isNativeTemplateStart() {
			if expression.Flags()&ast.NodeFlagsOptionalChain != 0 {
				return ast.Handle{}, false
			}
			template, ok := p.parseNativeTaggedTemplate(factory)
			if !ok {
				return ast.Handle{}, false
			}
			expression = p.finishNativeHandle(
				factory,
				factory.NewTaggedTemplateExpression(expression, ast.Handle{}, 0, template, ast.NodeFlagsNone),
				pos,
			)
			continue
		}
		if p.token == ast.KindLessThanToken || p.token == ast.KindLessThanLessThanToken {
			typeArguments, ok := p.tryParseNativeTypeArguments(factory)
			if !ok {
				return ast.Handle{}, false
			}
			switch {
			case p.token == ast.KindOpenParenToken:
				arguments, ok := p.parseNativeArgumentList(factory)
				if !ok {
					return ast.Handle{}, false
				}
				expression = p.finishNativeHandle(
					factory,
					factory.NewCallExpression(expression, ast.Handle{}, typeArguments, arguments, ast.NodeFlagsNone),
					pos,
				)
			case p.isNativeTemplateStart():
				template, ok := p.parseNativeTaggedTemplate(factory)
				if !ok {
					return ast.Handle{}, false
				}
				expression = p.finishNativeHandle(
					factory,
					factory.NewTaggedTemplateExpression(expression, ast.Handle{}, typeArguments, template, ast.NodeFlagsNone),
					pos,
				)
			default:
				return ast.Handle{}, false
			}
			continue
		}

		var questionDot ast.Handle
		optional := false
		if p.token == ast.KindQuestionDotToken {
			questionDot = p.parseNativeToken(factory)
			optional = true
		}
		flags := ast.NodeFlagsNone
		if optional || expression.Flags()&ast.NodeFlagsOptionalChain != 0 {
			flags = ast.NodeFlagsOptionalChain
		}

		switch {
		case !optional && p.token == ast.KindDotToken:
			p.nextToken()
			name, ok := p.parseNativeIdentifierName(factory)
			if !ok {
				return ast.Handle{}, false
			}
			expression = p.finishNativeHandle(
				factory,
				factory.NewPropertyAccessExpression(expression, ast.Handle{}, name, flags),
				pos,
			)
		case optional && tokenIsIdentifierOrKeyword(p.token):
			name, _ := p.parseNativeIdentifierName(factory)
			expression = p.finishNativeHandle(
				factory,
				factory.NewPropertyAccessExpression(expression, questionDot, name, flags),
				pos,
			)
		case p.token == ast.KindOpenBracketToken:
			p.nextToken()
			if p.token == ast.KindCloseBracketToken {
				return ast.Handle{}, false
			}
			argument, ok := p.parseNativeExpression(factory)
			if !ok || p.token != ast.KindCloseBracketToken {
				return ast.Handle{}, false
			}
			p.nextToken()
			expression = p.finishNativeHandle(
				factory,
				factory.NewElementAccessExpression(expression, questionDot, argument, flags),
				pos,
			)
		case p.token == ast.KindOpenParenToken:
			arguments, ok := p.parseNativeArgumentList(factory)
			if !ok {
				return ast.Handle{}, false
			}
			expression = p.finishNativeHandle(
				factory,
				factory.NewCallExpression(expression, questionDot, 0, arguments, flags),
				pos,
			)
		default:
			// A consumed ?. must introduce a property, element, or call.
			if optional {
				return ast.Handle{}, false
			}
			if (p.token == ast.KindPlusPlusToken || p.token == ast.KindMinusMinusToken) && !p.hasPrecedingLineBreak() {
				operator := p.token
				p.nextToken()
				return p.finishNativeHandle(factory, factory.NewPostfixUnaryExpression(expression, operator), pos), true
			}
			return expression, true
		}
	}
}

func (p *Parser) parseNativeArgumentList(factory *ast.Factory) (ast.ListRef, bool) {
	p.nextToken() // (
	listPos := p.nodePos()
	arguments := make([]ast.Handle, 0, 4)
	for p.token != ast.KindCloseParenToken {
		if p.token == ast.KindCommaToken {
			// Elisions are valid in arrays, not argument lists.
			return 0, false
		}
		var argument ast.Handle
		var ok bool
		if p.token == ast.KindDotDotDotToken {
			pos := p.nodePos()
			p.nextToken()
			var expression ast.Handle
			expression, ok = p.parseNativeAssignmentExpression(factory)
			if ok {
				argument = p.finishNativeHandle(factory, factory.NewSpreadElement(expression), pos)
			}
		} else {
			argument, ok = p.parseNativeAssignmentExpression(factory)
		}
		if !ok {
			return 0, false
		}
		arguments = append(arguments, argument)
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
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), arguments...)
	p.nextToken()
	return list, true
}

func (p *Parser) parseNativePrimaryExpression(factory *ast.Factory) (ast.Handle, bool) {
	switch p.token {
	case ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
		if p.token == ast.KindNoSubstitutionTemplateLiteral && p.scanner.TokenFlags()&ast.TokenFlagsIsInvalid != 0 {
			p.reScanTemplateToken(false)
		}
		return p.parseNativeLiteral(factory), true
	case ast.KindSlashToken, ast.KindSlashEqualsToken:
		if p.reScanSlashToken() != ast.KindRegularExpressionLiteral {
			return ast.Handle{}, false
		}
		return p.parseNativeLiteral(factory), true
	case ast.KindTemplateHead:
		return p.parseNativeTemplateExpression(factory, false)
	case ast.KindNewKeyword:
		return p.parseNativeNewExpression(factory)
	case ast.KindThisKeyword, ast.KindNullKeyword, ast.KindTrueKeyword, ast.KindFalseKeyword:
		pos := p.nodePos()
		kind := p.token
		p.nextToken()
		return p.finishNativeHandle(factory, factory.NewKeywordExpression(kind), pos), true
	case ast.KindOpenParenToken:
		pos := p.nodePos()
		p.nextToken()
		expression, ok := p.parseNativeExpression(factory)
		if !ok || p.token != ast.KindCloseParenToken {
			return ast.Handle{}, false
		}
		p.nextToken()
		return p.finishNativeHandle(factory, factory.NewParenthesizedExpression(expression), pos), true
	case ast.KindOpenBracketToken:
		return p.parseNativeArrayLiteral(factory)
	case ast.KindOpenBraceToken:
		return p.parseNativeObjectLiteral(factory)
	}
	if p.isIdentifier() && p.token != ast.KindAwaitKeyword && p.token != ast.KindYieldKeyword {
		return p.parseNativeIdentifier(factory), true
	}
	return ast.Handle{}, false
}

func (p *Parser) parseNativeArrayLiteral(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	multiLine := p.hasPrecedingLineBreak()
	listPos := p.nodePos()
	elements := make([]ast.Handle, 0, 8)
	for p.token != ast.KindCloseBracketToken {
		if p.token == ast.KindCommaToken {
			elements = append(elements, p.finishNativeHandle(factory, factory.NewOmittedExpression(), p.nodePos()))
			p.nextToken()
			continue
		}
		var element ast.Handle
		var ok bool
		if p.token == ast.KindDotDotDotToken {
			elementPos := p.nodePos()
			p.nextToken()
			var expression ast.Handle
			expression, ok = p.parseNativeAssignmentExpression(factory)
			if ok {
				element = p.finishNativeHandle(factory, factory.NewSpreadElement(expression), elementPos)
			}
		} else {
			element, ok = p.parseNativeAssignmentExpression(factory)
		}
		if !ok {
			return ast.Handle{}, false
		}
		elements = append(elements, element)
		if p.token != ast.KindCommaToken {
			break
		}
		p.nextToken()
	}
	if p.token != ast.KindCloseBracketToken {
		return ast.Handle{}, false
	}
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), elements...)
	p.nextToken()
	return p.finishNativeHandle(factory, factory.NewArrayLiteralExpression(list, multiLine), pos), true
}

func (p *Parser) parseNativeObjectLiteral(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	multiLine := p.hasPrecedingLineBreak()
	listPos := p.nodePos()
	properties := make([]ast.Handle, 0, 8)
	for p.token != ast.KindCloseBraceToken {
		propertyPos := p.nodePos()
		var property ast.Handle
		if p.token == ast.KindDotDotDotToken {
			p.nextToken()
			expression, ok := p.parseNativeAssignmentExpression(factory)
			if !ok {
				return ast.Handle{}, false
			}
			property = p.finishNativeHandle(factory, factory.NewSpreadAssignment(expression), propertyPos)
		} else {
			wasIdentifier := p.isIdentifier()
			name, ok := p.parseNativePropertyName(factory)
			if !ok {
				return ast.Handle{}, false
			}
			if p.token == ast.KindColonToken {
				p.nextToken()
				initializer, ok := p.parseNativeAssignmentExpression(factory)
				if !ok {
					return ast.Handle{}, false
				}
				property = p.finishNativeHandle(
					factory,
					factory.NewPropertyAssignment(0, name, ast.Handle{}, ast.Handle{}, initializer),
					propertyPos,
				)
			} else if wasIdentifier && p.token != ast.KindEqualsToken {
				property = p.finishNativeHandle(
					factory,
					factory.NewShorthandPropertyAssignment(0, name, ast.Handle{}, ast.Handle{}, ast.Handle{}, ast.Handle{}),
					propertyPos,
				)
			} else {
				return ast.Handle{}, false
			}
		}
		properties = append(properties, property)
		if p.token != ast.KindCommaToken {
			break
		}
		p.nextToken()
		if p.token == ast.KindCloseBraceToken {
			break
		}
	}
	if p.token != ast.KindCloseBraceToken {
		return ast.Handle{}, false
	}
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), properties...)
	p.nextToken()
	return p.finishNativeHandle(factory, factory.NewObjectLiteralExpression(list, multiLine), pos), true
}

func (p *Parser) isNativeTemplateStart() bool {
	return p.token == ast.KindNoSubstitutionTemplateLiteral || p.token == ast.KindTemplateHead
}

func (p *Parser) parseNativeTaggedTemplate(factory *ast.Factory) (ast.Handle, bool) {
	if p.token == ast.KindNoSubstitutionTemplateLiteral {
		p.reScanTemplateToken(true)
		return p.parseNativeLiteral(factory), true
	}
	return p.parseNativeTemplateExpression(factory, true)
}

func (p *Parser) parseNativeTemplateExpression(factory *ast.Factory, tagged bool) (ast.Handle, bool) {
	pos := p.nodePos()
	head := p.parseNativeTemplateHead(factory, tagged)
	listPos := p.nodePos()
	spans := make([]ast.Handle, 0, 2)
	for {
		spanPos := p.nodePos()
		expression, ok := p.parseNativeExpression(factory)
		if !ok || p.token != ast.KindCloseBraceToken {
			return ast.Handle{}, false
		}
		p.reScanTemplateToken(tagged)
		if p.token != ast.KindTemplateMiddle && p.token != ast.KindTemplateTail {
			return ast.Handle{}, false
		}
		literalKind := p.token
		literal := p.parseNativeTemplateMiddleOrTail(factory)
		spans = append(spans, p.finishNativeHandle(factory, factory.NewTemplateSpan(expression, literal), spanPos))
		if literalKind == ast.KindTemplateTail {
			break
		}
	}
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), spans...)
	return p.finishNativeHandle(factory, factory.NewTemplateExpression(head, list), pos), true
}

func (p *Parser) parseNativeTemplateHead(factory *ast.Factory, tagged bool) ast.Handle {
	if !tagged && p.scanner.TokenFlags()&ast.TokenFlagsIsInvalid != 0 {
		p.reScanTemplateToken(false)
	}
	pos := p.nodePos()
	result := factory.NewTemplateHead(
		p.scanner.TokenValue(),
		p.getTemplateLiteralRawText(2),
		p.scanner.TokenFlags(),
	)
	p.nextToken()
	return p.finishNativeHandle(factory, result, pos)
}

func (p *Parser) parseNativeTemplateMiddleOrTail(factory *ast.Factory) ast.Handle {
	pos := p.nodePos()
	text := p.scanner.TokenValue()
	rawText := p.getTemplateLiteralRawText(1)
	flags := p.scanner.TokenFlags()
	var result ast.Handle
	if p.token == ast.KindTemplateMiddle {
		rawText = p.getTemplateLiteralRawText(2)
		result = factory.NewTemplateMiddle(text, rawText, flags)
	} else {
		result = factory.NewTemplateTail(text, rawText, flags)
	}
	p.nextToken()
	return p.finishNativeHandle(factory, result, pos)
}

func (p *Parser) parseNativeNewExpression(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	if p.token == ast.KindDotToken {
		// new.target requires function-context diagnostics.
		return ast.Handle{}, false
	}
	expressionPos := p.nodePos()
	expression, ok := p.parseNativePrimaryExpression(factory)
	if !ok {
		return ast.Handle{}, false
	}
	for {
		switch p.token {
		case ast.KindDotToken:
			p.nextToken()
			name, ok := p.parseNativeIdentifierName(factory)
			if !ok {
				return ast.Handle{}, false
			}
			expression = p.finishNativeHandle(
				factory,
				factory.NewPropertyAccessExpression(expression, ast.Handle{}, name, ast.NodeFlagsNone),
				expressionPos,
			)
		case ast.KindOpenBracketToken:
			p.nextToken()
			argument, ok := p.parseNativeExpression(factory)
			if !ok || p.token != ast.KindCloseBracketToken {
				return ast.Handle{}, false
			}
			p.nextToken()
			expression = p.finishNativeHandle(
				factory,
				factory.NewElementAccessExpression(expression, ast.Handle{}, argument, ast.NodeFlagsNone),
				expressionPos,
			)
		default:
			goto memberDone
		}
	}

memberDone:
	var typeArguments ast.ListRef
	if p.token == ast.KindLessThanToken || p.token == ast.KindLessThanLessThanToken {
		typeArguments, ok = p.tryParseNativeTypeArguments(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	if p.token == ast.KindQuestionDotToken {
		return ast.Handle{}, false
	}
	var arguments ast.ListRef
	if p.token == ast.KindOpenParenToken {
		arguments, ok = p.parseNativeArgumentList(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	return p.finishNativeHandle(factory, factory.NewNewExpression(expression, typeArguments, arguments), pos), true
}

// tryParseNativeTypeArguments first recognizes the complete pointer-free type
// subset so a failed speculation cannot leave orphan Store rows.
func (p *Parser) tryParseNativeTypeArguments(factory *ast.Factory) (ast.ListRef, bool) {
	state := p.mark()
	if !p.scanNativeTypeArguments() ||
		(p.token != ast.KindOpenParenToken && !p.isNativeTemplateStart()) {
		p.rewind(state)
		return 0, false
	}
	p.rewind(state)
	return p.parseNativeTypeArguments(factory)
}

func (p *Parser) scanNativeTypeArguments() bool {
	if p.reScanLessThanToken() != ast.KindLessThanToken {
		return false
	}
	p.nextToken()
	if !p.scanNativeType() {
		return false
	}
	for p.token == ast.KindCommaToken {
		p.nextToken()
		if !p.scanNativeType() {
			return false
		}
	}
	if p.reScanGreaterThanToken() != ast.KindGreaterThanToken {
		return false
	}
	p.nextToken()
	return true
}

func (p *Parser) scanNativeType() bool {
	if isNativeKeywordType(p.token) {
		p.nextToken()
	} else if p.isIdentifier() {
		p.nextTokenWithoutCheck()
		for p.token == ast.KindDotToken {
			p.nextToken()
			if !tokenIsIdentifierOrKeyword(p.token) {
				return false
			}
			p.nextTokenWithoutCheck()
		}
		if p.token == ast.KindLessThanToken || p.token == ast.KindLessThanLessThanToken {
			if !p.scanNativeTypeArguments() {
				return false
			}
		}
	} else {
		return false
	}
	for p.token == ast.KindOpenBracketToken {
		p.nextToken()
		if p.token != ast.KindCloseBracketToken {
			return false
		}
		p.nextToken()
	}
	return true
}

func (p *Parser) parseNativeTypeArguments(factory *ast.Factory) (ast.ListRef, bool) {
	if p.reScanLessThanToken() != ast.KindLessThanToken {
		return 0, false
	}
	p.nextToken()
	listPos := p.nodePos()
	types := make([]ast.Handle, 0, 2)
	for {
		typeNode, ok := p.parseNativeType(factory)
		if !ok {
			return 0, false
		}
		types = append(types, typeNode)
		if p.token != ast.KindCommaToken {
			break
		}
		p.nextToken()
	}
	if p.reScanGreaterThanToken() != ast.KindGreaterThanToken {
		return 0, false
	}
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), types...)
	p.nextToken()
	return list, true
}

func (p *Parser) parseNativeType(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	var result ast.Handle
	if isNativeKeywordType(p.token) {
		kind := p.token
		p.nextToken()
		result = p.finishNativeHandle(factory, factory.NewKeywordTypeNode(kind), pos)
	} else if p.isIdentifier() {
		name := p.parseNativeIdentifier(factory)
		for p.token == ast.KindDotToken {
			p.nextToken()
			right, ok := p.parseNativeIdentifierName(factory)
			if !ok {
				return ast.Handle{}, false
			}
			name = p.finishNativeHandle(factory, factory.NewQualifiedName(name, right), pos)
		}
		var arguments ast.ListRef
		var ok bool
		if p.token == ast.KindLessThanToken || p.token == ast.KindLessThanLessThanToken {
			arguments, ok = p.parseNativeTypeArguments(factory)
			if !ok {
				return ast.Handle{}, false
			}
		}
		result = p.finishNativeHandle(factory, factory.NewTypeReferenceNode(name, arguments), pos)
	} else {
		return ast.Handle{}, false
	}
	for p.token == ast.KindOpenBracketToken {
		p.nextToken()
		if p.token != ast.KindCloseBracketToken {
			return ast.Handle{}, false
		}
		p.nextToken()
		result = p.finishNativeHandle(factory, factory.NewArrayTypeNode(result), pos)
	}
	return result, true
}

func isNativeKeywordType(kind ast.Kind) bool {
	switch kind {
	case ast.KindAnyKeyword, ast.KindBigIntKeyword, ast.KindBooleanKeyword,
		ast.KindIntrinsicKeyword, ast.KindNeverKeyword, ast.KindNumberKeyword,
		ast.KindObjectKeyword, ast.KindStringKeyword, ast.KindSymbolKeyword,
		ast.KindUndefinedKeyword, ast.KindUnknownKeyword, ast.KindVoidKeyword:
		return true
	default:
		return false
	}
}

func (p *Parser) parseNativePropertyName(factory *ast.Factory) (ast.Handle, bool) {
	if tokenIsIdentifierOrKeyword(p.token) {
		return p.parseNativeIdentifierName(factory)
	}
	if p.token == ast.KindOpenBracketToken {
		pos := p.nodePos()
		p.nextToken()
		expression, ok := p.parseNativeExpression(factory)
		if !ok || p.token != ast.KindCloseBracketToken {
			return ast.Handle{}, false
		}
		p.nextToken()
		return p.finishNativeHandle(factory, factory.NewComputedPropertyName(expression), pos), true
	}
	switch p.token {
	case ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral:
		return p.parseNativeLiteral(factory), true
	default:
		return ast.Handle{}, false
	}
}

func (p *Parser) parseNativeIdentifier(factory *ast.Factory) ast.Handle {
	pos := p.nodePos()
	text := p.scanner.TokenValue()
	p.identifierCount++
	p.nextTokenWithoutCheck()
	return p.finishNativeHandle(factory, factory.NewIdentifier(text), pos)
}

func (p *Parser) parseNativeIdentifierName(factory *ast.Factory) (ast.Handle, bool) {
	if !tokenIsIdentifierOrKeyword(p.token) {
		return ast.Handle{}, false
	}
	return p.parseNativeIdentifier(factory), true
}

func (p *Parser) parseNativeLiteral(factory *ast.Factory) ast.Handle {
	pos := p.nodePos()
	text := p.scanner.TokenValue()
	tokenFlags := p.scanner.TokenFlags()
	var result ast.Handle
	switch p.token {
	case ast.KindStringLiteral:
		result = factory.NewStringLiteral(text, tokenFlags)
	case ast.KindNumericLiteral:
		result = factory.NewNumericLiteral(text, tokenFlags)
	case ast.KindBigIntLiteral:
		result = factory.NewBigIntLiteral(text, tokenFlags)
	case ast.KindNoSubstitutionTemplateLiteral:
		result = factory.NewNoSubstitutionTemplateLiteral(text, tokenFlags)
	case ast.KindRegularExpressionLiteral:
		result = factory.NewRegularExpressionLiteral(text, tokenFlags)
	default:
		panic("parseNativeLiteral called for non-literal")
	}
	p.nextToken()
	return p.finishNativeHandle(factory, result, pos)
}

func (p *Parser) parseNativeToken(factory *ast.Factory) ast.Handle {
	pos := p.nodePos()
	kind := p.token
	p.nextToken()
	return p.finishNativeHandle(factory, factory.NewToken(kind), pos)
}

func (p *Parser) finishNativeHandle(factory *ast.Factory, handle ast.Handle, pos int) ast.Handle {
	handle.SetFlags(handle.Flags() | p.contextFlags)
	return factory.Finish(handle, core.NewTextRange(pos, p.nodePos()))
}
