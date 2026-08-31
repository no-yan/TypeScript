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
		return p.parseNativeLiteral(factory), true
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

func (p *Parser) parseNativePropertyName(factory *ast.Factory) (ast.Handle, bool) {
	if tokenIsIdentifierOrKeyword(p.token) {
		return p.parseNativeIdentifierName(factory)
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
