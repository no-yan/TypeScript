package parser

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// consumeNativeGreaterThan eats one `>` without combining adjacent `>`
// characters. Scan emits a single GreaterThanToken per `>`; calling
// ReScanGreaterThanToken here would merge the inner and outer closes of
// nested type arguments such as Map<string, Entry[]>.
func (p *Parser) consumeNativeGreaterThan() bool {
	if p.token != ast.KindGreaterThanToken {
		return false
	}
	p.nextToken()
	return true
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
	return p.consumeNativeGreaterThan()
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
	if !p.consumeNativeGreaterThan() {
		return 0, false
	}
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), types...)
	return list, true
}

func (p *Parser) parseNativeType(factory *ast.Factory) (ast.Handle, bool) {
	if predicate, ok := p.tryParseNativeTypePredicate(factory); ok {
		return predicate, true
	}
	if p.isNativeStartOfFunctionOrConstructorType() {
		return p.parseNativeFunctionOrConstructorType(factory)
	}
	pos := p.nodePos()
	typeNode, ok := p.parseNativeUnionType(factory)
	if !ok {
		return ast.Handle{}, false
	}
	if p.token == ast.KindExtendsKeyword && !p.hasPrecedingLineBreak() {
		p.nextToken()
		extendsType, ok := p.parseNativeType(factory)
		if !ok || p.token != ast.KindQuestionToken {
			return ast.Handle{}, false
		}
		p.nextToken()
		trueType, ok := p.parseNativeType(factory)
		if !ok || p.token != ast.KindColonToken {
			return ast.Handle{}, false
		}
		p.nextToken()
		falseType, ok := p.parseNativeType(factory)
		if !ok {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(factory, factory.NewConditionalTypeNode(typeNode, extendsType, trueType, falseType), pos), true
	}
	return typeNode, true
}

func (p *Parser) tryParseNativeTypePredicate(factory *ast.Factory) (ast.Handle, bool) {
	if !p.lookAhead((*Parser).isNativeTypePredicateStart) {
		return ast.Handle{}, false
	}
	pos := p.nodePos()
	var asserts ast.Handle
	if p.token == ast.KindAssertsKeyword {
		asserts = p.parseNativeToken(factory)
	}
	var name ast.Handle
	if p.token == ast.KindThisKeyword {
		thisPos := p.nodePos()
		p.nextToken()
		name = p.finishNativeHandle(factory, factory.NewThisTypeNode(), thisPos)
	} else {
		name = p.parseNativeIdentifier(factory)
	}
	if p.token != ast.KindIsKeyword {
		return p.finishNativeHandle(factory, factory.NewTypePredicateNode(asserts, name, ast.Handle{}), pos), true
	}
	p.nextToken()
	typeNode, ok := p.parseNativeType(factory)
	if !ok {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(factory, factory.NewTypePredicateNode(asserts, name, typeNode), pos), true
}

func (p *Parser) isNativeTypePredicateStart() bool {
	if p.token == ast.KindAssertsKeyword {
		p.nextToken()
		if !p.isIdentifier() && p.token != ast.KindThisKeyword {
			return false
		}
		p.nextTokenWithoutCheck()
		return true
	}
	if p.token == ast.KindThisKeyword {
		p.nextToken()
		return p.token == ast.KindIsKeyword && !p.hasPrecedingLineBreak()
	}
	if !p.isIdentifier() {
		return false
	}
	p.nextTokenWithoutCheck()
	return p.token == ast.KindIsKeyword && !p.hasPrecedingLineBreak()
}

func (p *Parser) scanNativeType() bool {
	if p.lookAhead((*Parser).isNativeTypePredicateStart) {
		if p.token == ast.KindAssertsKeyword {
			p.nextToken()
		}
		p.nextTokenWithoutCheck()
		if p.token == ast.KindIsKeyword {
			p.nextToken()
			return p.scanNativeType()
		}
		return true
	}
	if p.isNativeStartOfFunctionOrConstructorType() {
		if !p.scanNativeFunctionOrConstructorType() {
			return false
		}
	} else {
		if !p.scanNativeUnionType() {
			return false
		}
		if p.token == ast.KindExtendsKeyword && !p.hasPrecedingLineBreak() {
			p.nextToken()
			if !p.scanNativeType() || p.token != ast.KindQuestionToken {
				return false
			}
			p.nextToken()
			if !p.scanNativeType() || p.token != ast.KindColonToken {
				return false
			}
			p.nextToken()
			if !p.scanNativeType() {
				return false
			}
		}
	}
	return true
}

func (p *Parser) isNativeStartOfFunctionOrConstructorType() bool {
	switch p.token {
	case ast.KindLessThanToken, ast.KindLessThanLessThanToken, ast.KindNewKeyword:
		return true
	case ast.KindAbstractKeyword:
		return p.lookAhead(func(p *Parser) bool { return p.nextToken() == ast.KindNewKeyword })
	case ast.KindOpenParenToken:
		return p.lookAhead((*Parser).nextIsUnambiguouslyStartOfFunctionType)
	default:
		return false
	}
}

func (p *Parser) parseNativeFunctionOrConstructorType(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	var modifiers ast.ListRef
	if p.token == ast.KindAbstractKeyword {
		modifiers = factory.List(core.NewTextRange(p.nodePos(), p.nodePos()), p.parseNativeToken(factory))
	}
	isConstructor := false
	if p.token == ast.KindNewKeyword {
		isConstructor = true
		p.nextToken()
	}
	typeParameters, ok := p.parseNativeTypeParameters(factory)
	if !ok {
		return ast.Handle{}, false
	}
	parameters, ok := p.parseNativeParameterList(factory)
	if !ok || p.token != ast.KindEqualsGreaterThanToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	returnType, ok := p.parseNativeType(factory)
	if !ok {
		return ast.Handle{}, false
	}
	if isConstructor {
		return p.finishNativeHandle(factory, factory.NewConstructorTypeNode(modifiers, typeParameters, parameters, returnType), pos), true
	}
	return p.finishNativeHandle(factory, factory.NewFunctionTypeNode(typeParameters, parameters, returnType), pos), true
}

func (p *Parser) scanNativeFunctionOrConstructorType() bool {
	if p.token == ast.KindAbstractKeyword {
		p.nextToken()
	}
	if p.token == ast.KindNewKeyword {
		p.nextToken()
	}
	if p.token == ast.KindLessThanToken || p.token == ast.KindLessThanLessThanToken {
		if !p.scanNativeTypeParameterList() {
			return false
		}
	}
	if p.token != ast.KindOpenParenToken {
		return false
	}
	if !p.scanNativeParameterList() {
		return false
	}
	if p.token != ast.KindEqualsGreaterThanToken {
		return false
	}
	p.nextToken()
	return p.scanNativeType()
}

func (p *Parser) scanNativeTypeParameterList() bool {
	if p.reScanLessThanToken() != ast.KindLessThanToken {
		return false
	}
	p.nextToken()
	for p.token != ast.KindGreaterThanToken {
		if !p.scanNativeTypeParameter() {
			return false
		}
		if p.token != ast.KindCommaToken {
			break
		}
		p.nextToken()
	}
	return p.consumeNativeGreaterThan()
}

func (p *Parser) scanNativeTypeParameter() bool {
	if !p.isIdentifier() {
		return false
	}
	p.nextTokenWithoutCheck()
	if p.token == ast.KindExtendsKeyword {
		p.nextToken()
		if !p.scanNativeType() {
			return false
		}
	}
	if p.token == ast.KindEqualsToken {
		p.nextToken()
		if !p.scanNativeType() {
			return false
		}
	}
	return true
}

func (p *Parser) scanNativeParameterList() bool {
	if p.token != ast.KindOpenParenToken {
		return false
	}
	p.nextToken()
	for p.token != ast.KindCloseParenToken {
		if p.token == ast.KindDotDotDotToken {
			p.nextToken()
		}
		if p.token == ast.KindOpenBraceToken {
			if !p.scanNativeBindingPattern(ast.KindCloseBraceToken) {
				return false
			}
		} else if p.token == ast.KindOpenBracketToken {
			if !p.scanNativeBindingPattern(ast.KindCloseBracketToken) {
				return false
			}
		} else if p.isBindingIdentifier() || p.token == ast.KindThisKeyword {
			p.nextTokenWithoutCheck()
		} else {
			return false
		}
		if p.token == ast.KindQuestionToken {
			p.nextToken()
		}
		if p.token == ast.KindColonToken {
			p.nextToken()
			if !p.scanNativeType() {
				return false
			}
		}
		if p.token == ast.KindEqualsToken {
			p.nextToken()
			if !p.scanNativeExpressionish() {
				return false
			}
		}
		if p.token != ast.KindCommaToken {
			break
		}
		p.nextToken()
	}
	if p.token != ast.KindCloseParenToken {
		return false
	}
	p.nextToken()
	return true
}

func (p *Parser) scanNativeBindingPattern(close ast.Kind) bool {
	p.nextToken()
	for p.token != close && p.token != ast.KindEndOfFile {
		if p.token == ast.KindDotDotDotToken {
			p.nextToken()
		}
		if p.token == ast.KindOpenBraceToken {
			if !p.scanNativeBindingPattern(ast.KindCloseBraceToken) {
				return false
			}
		} else if p.token == ast.KindOpenBracketToken {
			if !p.scanNativeBindingPattern(ast.KindCloseBracketToken) {
				return false
			}
		} else if tokenIsIdentifierOrKeyword(p.token) || p.token == ast.KindStringLiteral || p.token == ast.KindNumericLiteral {
			p.nextTokenWithoutCheck()
			if p.token == ast.KindColonToken {
				p.nextToken()
				if p.token == ast.KindOpenBraceToken {
					if !p.scanNativeBindingPattern(ast.KindCloseBraceToken) {
						return false
					}
				} else if p.token == ast.KindOpenBracketToken {
					if !p.scanNativeBindingPattern(ast.KindCloseBracketToken) {
						return false
					}
				} else if p.isBindingIdentifier() {
					p.nextTokenWithoutCheck()
				} else {
					return false
				}
			}
		} else if p.token == ast.KindCommaToken {
			p.nextToken()
			continue
		} else {
			return false
		}
		if p.token == ast.KindEqualsToken {
			p.nextToken()
			if !p.scanNativeExpressionish() {
				return false
			}
		}
		if p.token == ast.KindCommaToken {
			p.nextToken()
		} else if p.token != close {
			return false
		}
	}
	if p.token != close {
		return false
	}
	p.nextToken()
	return true
}

func (p *Parser) scanNativeExpressionish() bool {
	// Speculative type-parameter defaults and binding initializers only need
	// to consume a primary-ish token sequence so the scanner stays in sync.
	if p.token == ast.KindEndOfFile {
		return false
	}
	depth := 0
	for {
		switch p.token {
		case ast.KindEndOfFile:
			return false
		case ast.KindOpenParenToken, ast.KindOpenBraceToken, ast.KindOpenBracketToken, ast.KindLessThanToken:
			depth++
		case ast.KindCloseParenToken, ast.KindCloseBraceToken, ast.KindCloseBracketToken, ast.KindGreaterThanToken:
			if depth == 0 {
				return true
			}
			depth--
		case ast.KindCommaToken, ast.KindEqualsGreaterThanToken:
			if depth == 0 {
				return true
			}
		}
		p.nextToken()
		if depth == 0 && (p.token == ast.KindCommaToken || p.token == ast.KindCloseParenToken ||
			p.token == ast.KindCloseBraceToken || p.token == ast.KindCloseBracketToken ||
			p.token == ast.KindGreaterThanToken || p.token == ast.KindEqualsGreaterThanToken) {
			return true
		}
	}
}

func (p *Parser) parseNativeUnionType(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	if p.token == ast.KindBarToken {
		p.nextToken()
	}
	first, ok := p.parseNativeIntersectionType(factory)
	if !ok {
		return ast.Handle{}, false
	}
	if p.token != ast.KindBarToken {
		return first, true
	}
	types := []ast.Handle{first}
	for p.token == ast.KindBarToken {
		p.nextToken()
		next, ok := p.parseNativeIntersectionType(factory)
		if !ok {
			return ast.Handle{}, false
		}
		types = append(types, next)
	}
	list := factory.List(core.NewTextRange(pos, p.nodePos()), types...)
	return p.finishNativeHandle(factory, factory.NewUnionTypeNode(list), pos), true
}

func (p *Parser) scanNativeUnionType() bool {
	if p.token == ast.KindBarToken {
		p.nextToken()
	}
	if !p.scanNativeIntersectionType() {
		return false
	}
	for p.token == ast.KindBarToken {
		p.nextToken()
		if !p.scanNativeIntersectionType() {
			return false
		}
	}
	return true
}

func (p *Parser) parseNativeIntersectionType(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	if p.token == ast.KindAmpersandToken {
		p.nextToken()
	}
	first, ok := p.parseNativeTypeOperator(factory)
	if !ok {
		return ast.Handle{}, false
	}
	if p.token != ast.KindAmpersandToken {
		return first, true
	}
	types := []ast.Handle{first}
	for p.token == ast.KindAmpersandToken {
		p.nextToken()
		next, ok := p.parseNativeTypeOperator(factory)
		if !ok {
			return ast.Handle{}, false
		}
		types = append(types, next)
	}
	list := factory.List(core.NewTextRange(pos, p.nodePos()), types...)
	return p.finishNativeHandle(factory, factory.NewIntersectionTypeNode(list), pos), true
}

func (p *Parser) scanNativeIntersectionType() bool {
	if p.token == ast.KindAmpersandToken {
		p.nextToken()
	}
	if !p.scanNativeTypeOperator() {
		return false
	}
	for p.token == ast.KindAmpersandToken {
		p.nextToken()
		if !p.scanNativeTypeOperator() {
			return false
		}
	}
	return true
}

func (p *Parser) parseNativeTypeOperator(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	switch p.token {
	case ast.KindKeyOfKeyword, ast.KindReadonlyKeyword, ast.KindUniqueKeyword:
		operator := p.token
		p.nextToken()
		operand, ok := p.parseNativeTypeOperator(factory)
		if !ok {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(factory, factory.NewTypeOperatorNode(operator, operand), pos), true
	case ast.KindInferKeyword:
		p.nextToken()
		param, ok := p.parseNativeTypeParameter(factory)
		if !ok {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(factory, factory.NewInferTypeNode(param), pos), true
	}
	return p.parseNativeArrayType(factory)
}

func (p *Parser) scanNativeTypeOperator() bool {
	switch p.token {
	case ast.KindKeyOfKeyword, ast.KindReadonlyKeyword, ast.KindUniqueKeyword:
		p.nextToken()
		return p.scanNativeTypeOperator()
	case ast.KindInferKeyword:
		p.nextToken()
		return p.scanNativeTypeParameter()
	}
	return p.scanNativeArrayType()
}

func (p *Parser) parseNativeArrayType(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	result, ok := p.parseNativeNonArrayType(factory)
	if !ok {
		return ast.Handle{}, false
	}
	for p.token == ast.KindOpenBracketToken {
		p.nextToken()
		if p.token == ast.KindCloseBracketToken {
			p.nextToken()
			result = p.finishNativeHandle(factory, factory.NewArrayTypeNode(result), pos)
			continue
		}
		index, ok := p.parseNativeType(factory)
		if !ok || p.token != ast.KindCloseBracketToken {
			return ast.Handle{}, false
		}
		p.nextToken()
		result = p.finishNativeHandle(factory, factory.NewIndexedAccessTypeNode(result, index), pos)
	}
	return result, true
}

func (p *Parser) scanNativeArrayType() bool {
	if !p.scanNativeNonArrayType() {
		return false
	}
	for p.token == ast.KindOpenBracketToken {
		p.nextToken()
		if p.token == ast.KindCloseBracketToken {
			p.nextToken()
			continue
		}
		if !p.scanNativeType() || p.token != ast.KindCloseBracketToken {
			return false
		}
		p.nextToken()
	}
	return true
}

func (p *Parser) parseNativeNonArrayType(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	if isNativeKeywordType(p.token) {
		kind := p.token
		p.nextToken()
		return p.finishNativeHandle(factory, factory.NewKeywordTypeNode(kind), pos), true
	}
	switch p.token {
	case ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral,
		ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword:
		literal := p.parseNativeTypeLiteral(factory)
		return p.finishNativeHandle(factory, factory.NewLiteralTypeNode(literal), pos), true
	case ast.KindTypeOfKeyword:
		p.nextToken()
		if p.token == ast.KindImportKeyword {
			return p.parseNativeImportType(factory, pos, true)
		}
		name, ok := p.parseNativeEntityName(factory)
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
		return p.finishNativeHandle(factory, factory.NewTypeQueryNode(name, arguments), pos), true
	case ast.KindImportKeyword:
		return p.parseNativeImportType(factory, pos, false)
	case ast.KindThisKeyword:
		p.nextToken()
		return p.finishNativeHandle(factory, factory.NewThisTypeNode(), pos), true
	case ast.KindOpenParenToken:
		p.nextToken()
		inner, ok := p.parseNativeType(factory)
		if !ok || p.token != ast.KindCloseParenToken {
			return ast.Handle{}, false
		}
		p.nextToken()
		return p.finishNativeHandle(factory, factory.NewParenthesizedTypeNode(inner), pos), true
	case ast.KindOpenBraceToken:
		if p.lookAhead((*Parser).isNativeMappedTypeStart) {
			return p.parseNativeMappedType(factory)
		}
		return p.parseNativeTypeLiteralMembers(factory)
	case ast.KindOpenBracketToken:
		return p.parseNativeTupleType(factory)
	case ast.KindTemplateHead:
		return p.parseNativeTemplateLiteralType(factory)
	case ast.KindNoSubstitutionTemplateLiteral:
		literal := p.parseNativeLiteral(factory)
		return p.finishNativeHandle(factory, factory.NewLiteralTypeNode(literal), pos), true
	}
	if p.isIdentifier() {
		name, ok := p.parseNativeEntityName(factory)
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
		return p.finishNativeHandle(factory, factory.NewTypeReferenceNode(name, arguments), pos), true
	}
	return ast.Handle{}, false
}

func (p *Parser) scanNativeNonArrayType() bool {
	if isNativeKeywordType(p.token) {
		p.nextToken()
		return true
	}
	switch p.token {
	case ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral,
		ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword,
		ast.KindNoSubstitutionTemplateLiteral:
		p.nextToken()
		return true
	case ast.KindTypeOfKeyword:
		p.nextToken()
		if p.token == ast.KindImportKeyword {
			return p.scanNativeImportType()
		}
		return p.scanNativeEntityNameAndArgs()
	case ast.KindImportKeyword:
		return p.scanNativeImportType()
	case ast.KindThisKeyword:
		p.nextToken()
		return true
	case ast.KindOpenParenToken:
		p.nextToken()
		if !p.scanNativeType() || p.token != ast.KindCloseParenToken {
			return false
		}
		p.nextToken()
		return true
	case ast.KindOpenBraceToken:
		if p.lookAhead((*Parser).isNativeMappedTypeStart) {
			return p.scanNativeMappedType()
		}
		return p.scanNativeTypeLiteralMembers()
	case ast.KindOpenBracketToken:
		return p.scanNativeTupleType()
	case ast.KindTemplateHead:
		return p.scanNativeTemplateLiteralType()
	}
	if p.isIdentifier() {
		return p.scanNativeEntityNameAndArgs()
	}
	return false
}

func (p *Parser) scanNativeEntityNameAndArgs() bool {
	if !p.isIdentifier() {
		return false
	}
	p.nextTokenWithoutCheck()
	for p.token == ast.KindDotToken {
		p.nextToken()
		if !tokenIsIdentifierOrKeyword(p.token) {
			return false
		}
		p.nextTokenWithoutCheck()
	}
	if p.token == ast.KindLessThanToken || p.token == ast.KindLessThanLessThanToken {
		return p.scanNativeTypeArguments()
	}
	return true
}

func (p *Parser) parseNativeEntityName(factory *ast.Factory) (ast.Handle, bool) {
	if !p.isIdentifier() {
		return ast.Handle{}, false
	}
	pos := p.nodePos()
	name := p.parseNativeIdentifier(factory)
	for p.token == ast.KindDotToken {
		p.nextToken()
		right, ok := p.parseNativeIdentifierName(factory)
		if !ok {
			return ast.Handle{}, false
		}
		name = p.finishNativeHandle(factory, factory.NewQualifiedName(name, right), pos)
	}
	return name, true
}

func (p *Parser) parseNativeTypeLiteral(factory *ast.Factory) ast.Handle {
	switch p.token {
	case ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword:
		pos := p.nodePos()
		kind := p.token
		p.nextToken()
		return p.finishNativeHandle(factory, factory.NewKeywordExpression(kind), pos)
	default:
		return p.parseNativeLiteral(factory)
	}
}

func (p *Parser) isNativeMappedTypeStart() bool {
	if p.token != ast.KindOpenBraceToken {
		return false
	}
	p.nextToken()
	if p.token == ast.KindReadonlyKeyword || p.token == ast.KindPlusToken || p.token == ast.KindMinusToken {
		p.nextToken()
		if p.token == ast.KindReadonlyKeyword {
			p.nextToken()
		}
	}
	if p.token != ast.KindOpenBracketToken {
		return false
	}
	p.nextToken()
	if !tokenIsIdentifierOrKeyword(p.token) {
		return false
	}
	p.nextTokenWithoutCheck()
	return p.token == ast.KindInKeyword
}

func (p *Parser) parseNativeMappedType(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	var readonly ast.Handle
	if p.token == ast.KindReadonlyKeyword || p.token == ast.KindPlusToken || p.token == ast.KindMinusToken {
		readonly = p.parseNativeToken(factory)
		if readonly.Kind() != ast.KindReadonlyKeyword {
			if p.token != ast.KindReadonlyKeyword {
				return ast.Handle{}, false
			}
			p.nextToken()
		}
	}
	if p.token != ast.KindOpenBracketToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	param, ok := p.parseNativeMappedTypeParameter(factory)
	if !ok {
		return ast.Handle{}, false
	}
	var nameType ast.Handle
	if p.token == ast.KindAsKeyword {
		p.nextToken()
		nameType, ok = p.parseNativeType(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	if p.token != ast.KindCloseBracketToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	var question ast.Handle
	if p.token == ast.KindQuestionToken || p.token == ast.KindPlusToken || p.token == ast.KindMinusToken {
		question = p.parseNativeToken(factory)
		if question.Kind() != ast.KindQuestionToken {
			if p.token != ast.KindQuestionToken {
				return ast.Handle{}, false
			}
			p.nextToken()
		}
	}
	var typeNode ast.Handle
	if p.token == ast.KindColonToken {
		p.nextToken()
		typeNode, ok = p.parseNativeType(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	if p.token == ast.KindSemicolonToken || p.token == ast.KindCommaToken {
		p.nextToken()
	}
	members := make([]ast.Handle, 0)
	listPos := p.nodePos()
	for p.token != ast.KindCloseBraceToken && p.token != ast.KindEndOfFile {
		member, ok := p.parseNativeTypeElement(factory)
		if !ok {
			return ast.Handle{}, false
		}
		members = append(members, member)
		if p.token == ast.KindSemicolonToken || p.token == ast.KindCommaToken {
			p.nextToken()
		} else if p.token != ast.KindCloseBraceToken {
			return ast.Handle{}, false
		}
	}
	if p.token != ast.KindCloseBraceToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), members...)
	return p.finishNativeHandle(factory, factory.NewMappedTypeNode(readonly, param, nameType, question, typeNode, list), pos), true
}

func (p *Parser) parseNativeMappedTypeParameter(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	if !tokenIsIdentifierOrKeyword(p.token) {
		return ast.Handle{}, false
	}
	name := p.parseNativeIdentifier(factory)
	if p.token != ast.KindInKeyword {
		return ast.Handle{}, false
	}
	p.nextToken()
	constraint, ok := p.parseNativeType(factory)
	if !ok {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(factory, factory.NewTypeParameterDeclaration(0, name, constraint, ast.Handle{}, ast.Handle{}), pos), true
}

func (p *Parser) scanNativeMappedType() bool {
	p.nextToken()
	if p.token == ast.KindReadonlyKeyword || p.token == ast.KindPlusToken || p.token == ast.KindMinusToken {
		p.nextToken()
		if p.token == ast.KindReadonlyKeyword {
			p.nextToken()
		}
	}
	if p.token != ast.KindOpenBracketToken {
		return false
	}
	p.nextToken()
	if !tokenIsIdentifierOrKeyword(p.token) {
		return false
	}
	p.nextTokenWithoutCheck()
	if p.token != ast.KindInKeyword {
		return false
	}
	p.nextToken()
	if !p.scanNativeType() {
		return false
	}
	if p.token == ast.KindAsKeyword {
		p.nextToken()
		if !p.scanNativeType() {
			return false
		}
	}
	if p.token != ast.KindCloseBracketToken {
		return false
	}
	p.nextToken()
	if p.token == ast.KindQuestionToken || p.token == ast.KindPlusToken || p.token == ast.KindMinusToken {
		p.nextToken()
		if p.token == ast.KindQuestionToken {
			p.nextToken()
		}
	}
	if p.token == ast.KindColonToken {
		p.nextToken()
		if !p.scanNativeType() {
			return false
		}
	}
	if p.token == ast.KindSemicolonToken || p.token == ast.KindCommaToken {
		p.nextToken()
	}
	return p.scanNativeTypeLiteralMembersFromInside()
}

func (p *Parser) parseNativeTypeLiteralMembers(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	listPos := p.nodePos()
	members := make([]ast.Handle, 0, 4)
	for p.token != ast.KindCloseBraceToken && p.token != ast.KindEndOfFile {
		member, ok := p.parseNativeTypeElement(factory)
		if !ok {
			return ast.Handle{}, false
		}
		members = append(members, member)
		if p.token == ast.KindSemicolonToken || p.token == ast.KindCommaToken {
			p.nextToken()
		} else if p.token != ast.KindCloseBraceToken {
			return ast.Handle{}, false
		}
	}
	if p.token != ast.KindCloseBraceToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), members...)
	return p.finishNativeHandle(factory, factory.NewTypeLiteralNode(list), pos), true
}

func (p *Parser) scanNativeTypeLiteralMembers() bool {
	if p.token != ast.KindOpenBraceToken {
		return false
	}
	p.nextToken()
	return p.scanNativeTypeLiteralMembersFromInside()
}

func (p *Parser) scanNativeTypeLiteralMembersFromInside() bool {
	for p.token != ast.KindCloseBraceToken && p.token != ast.KindEndOfFile {
		if !p.scanNativeTypeElement() {
			return false
		}
		if p.token == ast.KindSemicolonToken || p.token == ast.KindCommaToken {
			p.nextToken()
		} else if p.token != ast.KindCloseBraceToken {
			return false
		}
	}
	if p.token != ast.KindCloseBraceToken {
		return false
	}
	p.nextToken()
	return true
}

func (p *Parser) parseNativeTypeElement(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
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
		return p.finishNativeHandle(factory, factory.NewCallSignatureDeclaration(typeParameters, parameters, typeNode), pos), true
	}
	if p.token == ast.KindNewKeyword && p.lookAhead((*Parser).nextTokenIsOpenParenOrLessThan) {
		p.nextToken()
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
		return p.finishNativeHandle(factory, factory.NewConstructSignatureDeclaration(typeParameters, parameters, typeNode), pos), true
	}
	modifiers, ok := p.parseNativeModifiers(factory)
	if !ok {
		return ast.Handle{}, false
	}
	if p.token == ast.KindGetKeyword || p.token == ast.KindSetKeyword {
		return p.parseNativeAccessorInType(factory, pos, modifiers)
	}
	if p.token == ast.KindOpenBracketToken {
		if p.lookAhead((*Parser).isNativeIndexSignatureStart) {
			return p.parseNativeIndexSignature(factory, pos, modifiers)
		}
	}
	name, ok := p.parseNativePropertyName(factory)
	if !ok {
		return ast.Handle{}, false
	}
	var question ast.Handle
	if p.token == ast.KindQuestionToken {
		question = p.parseNativeToken(factory)
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
		return p.finishNativeHandle(
			factory,
			factory.NewMethodSignatureDeclaration(modifiers, name, question, typeParameters, parameters, typeNode),
			pos,
		), true
	}
	if p.token != ast.KindColonToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	typeNode, ok := p.parseNativeType(factory)
	if !ok {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(
		factory,
		factory.NewPropertySignatureDeclaration(modifiers, name, question, typeNode, ast.Handle{}),
		pos,
	), true
}

func (p *Parser) scanNativeTypeElement() bool {
	if p.token == ast.KindOpenParenToken || p.token == ast.KindLessThanToken {
		if p.token == ast.KindLessThanToken && !p.scanNativeTypeParameterList() {
			return false
		}
		return p.scanNativeParameterList() && p.scanNativeOptionalTypeAnnotation()
	}
	if p.token == ast.KindNewKeyword && p.lookAhead((*Parser).nextTokenIsOpenParenOrLessThan) {
		p.nextToken()
		if p.token == ast.KindLessThanToken && !p.scanNativeTypeParameterList() {
			return false
		}
		return p.scanNativeParameterList() && p.scanNativeOptionalTypeAnnotation()
	}
	for isNativeModifierStart(p.token) {
		p.nextToken()
	}
	if p.token == ast.KindGetKeyword || p.token == ast.KindSetKeyword {
		p.nextToken()
		if !p.scanNativePropertyName() {
			return false
		}
		if p.token == ast.KindLessThanToken && !p.scanNativeTypeParameterList() {
			return false
		}
		return p.scanNativeParameterList() && p.scanNativeOptionalTypeAnnotation()
	}
	if p.token == ast.KindOpenBracketToken && p.lookAhead((*Parser).isNativeIndexSignatureStart) {
		p.nextToken()
		if !p.isBindingIdentifier() {
			return false
		}
		p.nextTokenWithoutCheck()
		if p.token != ast.KindColonToken {
			return false
		}
		p.nextToken()
		if !p.scanNativeType() || p.token != ast.KindCloseBracketToken {
			return false
		}
		p.nextToken()
		return p.scanNativeOptionalTypeAnnotation()
	}
	if !p.scanNativePropertyName() {
		return false
	}
	if p.token == ast.KindQuestionToken {
		p.nextToken()
	}
	if p.token == ast.KindOpenParenToken || p.token == ast.KindLessThanToken {
		if p.token == ast.KindLessThanToken && !p.scanNativeTypeParameterList() {
			return false
		}
		return p.scanNativeParameterList() && p.scanNativeOptionalTypeAnnotation()
	}
	if p.token != ast.KindColonToken {
		return false
	}
	p.nextToken()
	return p.scanNativeType()
}

func (p *Parser) scanNativeOptionalTypeAnnotation() bool {
	if p.token == ast.KindColonToken {
		p.nextToken()
		return p.scanNativeType()
	}
	return true
}

func (p *Parser) scanNativePropertyName() bool {
	if tokenIsIdentifierOrKeyword(p.token) {
		p.nextTokenWithoutCheck()
		return true
	}
	if p.token == ast.KindStringLiteral || p.token == ast.KindNumericLiteral || p.token == ast.KindBigIntLiteral {
		p.nextToken()
		return true
	}
	if p.token != ast.KindOpenBracketToken {
		return false
	}
	p.nextToken()
	if !p.scanNativeType() && !p.scanNativeExpressionish() {
		return false
	}
	if p.token != ast.KindCloseBracketToken {
		return false
	}
	p.nextToken()
	return true
}

func (p *Parser) isNativeIndexSignatureStart() bool {
	if p.token != ast.KindOpenBracketToken {
		return false
	}
	p.nextToken()
	if !p.isBindingIdentifier() {
		return false
	}
	p.nextTokenWithoutCheck()
	return p.token == ast.KindColonToken
}

func (p *Parser) parseNativeIndexSignature(factory *ast.Factory, pos int, modifiers ast.ListRef) (ast.Handle, bool) {
	parameters, ok := p.parseNativeParameterListFromBrackets(factory)
	if !ok {
		return ast.Handle{}, false
	}
	if p.token != ast.KindColonToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	typeNode, ok := p.parseNativeType(factory)
	if !ok {
		return ast.Handle{}, false
	}
	return p.finishNativeHandle(factory, factory.NewIndexSignatureDeclaration(modifiers, parameters, typeNode), pos), true
}

func (p *Parser) parseNativeParameterListFromBrackets(factory *ast.Factory) (ast.ListRef, bool) {
	if p.token != ast.KindOpenBracketToken {
		return 0, false
	}
	p.nextToken()
	pos := p.nodePos()
	param, ok := p.parseNativeParameter(factory)
	if !ok || p.token != ast.KindCloseBracketToken {
		return 0, false
	}
	p.nextToken()
	return factory.List(core.NewTextRange(pos, p.nodePos()), param), true
}

func (p *Parser) parseNativeAccessorInType(factory *ast.Factory, pos int, modifiers ast.ListRef) (ast.Handle, bool) {
	isGet := p.token == ast.KindGetKeyword
	p.nextToken()
	name, ok := p.parseNativePropertyName(factory)
	if !ok {
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
	var typeNode ast.Handle
	if p.token == ast.KindColonToken {
		p.nextToken()
		typeNode, ok = p.parseNativeType(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	if isGet {
		return p.finishNativeHandle(factory, factory.NewGetAccessorDeclaration(modifiers, name, typeParameters, parameters, typeNode, ast.Handle{}, ast.Handle{}), pos), true
	}
	return p.finishNativeHandle(factory, factory.NewSetAccessorDeclaration(modifiers, name, typeParameters, parameters, typeNode, ast.Handle{}, ast.Handle{}), pos), true
}

func (p *Parser) parseNativeTupleType(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	listPos := p.nodePos()
	elements := make([]ast.Handle, 0, 4)
	for p.token != ast.KindCloseBracketToken && p.token != ast.KindEndOfFile {
		elem, ok := p.parseNativeTupleElement(factory)
		if !ok {
			return ast.Handle{}, false
		}
		elements = append(elements, elem)
		if p.token == ast.KindCommaToken {
			p.nextToken()
			continue
		}
		break
	}
	if p.token != ast.KindCloseBracketToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), elements...)
	return p.finishNativeHandle(factory, factory.NewTupleTypeNode(list), pos), true
}

func (p *Parser) parseNativeTupleElement(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	if p.lookAhead((*Parser).scanStartOfNamedTupleElement) {
		var rest ast.Handle
		if p.token == ast.KindDotDotDotToken {
			rest = p.parseNativeToken(factory)
		}
		name, ok := p.parseNativeIdentifierName(factory)
		if !ok {
			return ast.Handle{}, false
		}
		var question ast.Handle
		if p.token == ast.KindQuestionToken {
			question = p.parseNativeToken(factory)
		}
		if p.token != ast.KindColonToken {
			return ast.Handle{}, false
		}
		p.nextToken()
		typeNode, ok := p.parseNativeTupleElementType(factory)
		if !ok {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(factory, factory.NewNamedTupleMember(rest, name, question, typeNode), pos), true
	}
	return p.parseNativeTupleElementType(factory)
}

func (p *Parser) parseNativeTupleElementType(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	if p.token == ast.KindDotDotDotToken {
		p.nextToken()
		typeNode, ok := p.parseNativeType(factory)
		if !ok {
			return ast.Handle{}, false
		}
		return p.finishNativeHandle(factory, factory.NewRestTypeNode(typeNode), pos), true
	}
	typeNode, ok := p.parseNativeType(factory)
	if !ok {
		return ast.Handle{}, false
	}
	if p.token == ast.KindQuestionToken {
		p.nextToken()
		return p.finishNativeHandle(factory, factory.NewOptionalTypeNode(typeNode), pos), true
	}
	return typeNode, true
}

func (p *Parser) scanNativeTupleType() bool {
	p.nextToken()
	for p.token != ast.KindCloseBracketToken && p.token != ast.KindEndOfFile {
		if p.token == ast.KindDotDotDotToken {
			p.nextToken()
		}
		if !p.scanNativeType() {
			return false
		}
		if p.token == ast.KindQuestionToken {
			p.nextToken()
		}
		if p.token == ast.KindColonToken {
			p.nextToken()
			if p.token == ast.KindDotDotDotToken {
				p.nextToken()
			}
			if !p.scanNativeType() {
				return false
			}
			if p.token == ast.KindQuestionToken {
				p.nextToken()
			}
		}
		if p.token == ast.KindCommaToken {
			p.nextToken()
			continue
		}
		break
	}
	if p.token != ast.KindCloseBracketToken {
		return false
	}
	p.nextToken()
	return true
}

func (p *Parser) parseNativeImportType(factory *ast.Factory, pos int, isTypeOf bool) (ast.Handle, bool) {
	if p.token != ast.KindImportKeyword {
		return ast.Handle{}, false
	}
	p.nextToken()
	if p.token != ast.KindOpenParenToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	argument, ok := p.parseNativeType(factory)
	if !ok || p.token != ast.KindCloseParenToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	var qualifier ast.Handle
	if p.token == ast.KindDotToken {
		p.nextToken()
		qualifier, ok = p.parseNativeEntityName(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	var arguments ast.ListRef
	if p.token == ast.KindLessThanToken || p.token == ast.KindLessThanLessThanToken {
		arguments, ok = p.parseNativeTypeArguments(factory)
		if !ok {
			return ast.Handle{}, false
		}
	}
	return p.finishNativeHandle(factory, factory.NewImportTypeNode(isTypeOf, argument, ast.Handle{}, qualifier, arguments), pos), true
}

func (p *Parser) scanNativeImportType() bool {
	if p.token != ast.KindImportKeyword {
		return false
	}
	p.nextToken()
	if p.token != ast.KindOpenParenToken {
		return false
	}
	p.nextToken()
	if !p.scanNativeType() || p.token != ast.KindCloseParenToken {
		return false
	}
	p.nextToken()
	if p.token == ast.KindDotToken {
		p.nextToken()
		if !p.scanNativeEntityNameAndArgs() {
			return false
		}
		return true
	}
	if p.token == ast.KindLessThanToken || p.token == ast.KindLessThanLessThanToken {
		return p.scanNativeTypeArguments()
	}
	return true
}

func (p *Parser) parseNativeTemplateLiteralType(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	head := p.parseNativeTemplateHead(factory, false)
	spans := make([]ast.Handle, 0, 2)
	listPos := p.nodePos()
	for {
		spanPos := p.nodePos()
		typeNode, ok := p.parseNativeType(factory)
		if !ok || p.token != ast.KindCloseBraceToken {
			return ast.Handle{}, false
		}
		p.reScanTemplateToken(false)
		if p.token != ast.KindTemplateMiddle && p.token != ast.KindTemplateTail {
			return ast.Handle{}, false
		}
		literalKind := p.token
		literal := p.parseNativeTemplateMiddleOrTail(factory)
		spans = append(spans, p.finishNativeHandle(factory, factory.NewTemplateLiteralTypeSpan(typeNode, literal), spanPos))
		if literalKind == ast.KindTemplateTail {
			break
		}
	}
	list := factory.List(core.NewTextRange(listPos, p.nodePos()), spans...)
	return p.finishNativeHandle(factory, factory.NewTemplateLiteralTypeNode(head, list), pos), true
}

func (p *Parser) scanNativeTemplateLiteralType() bool {
	p.nextToken()
	for {
		if !p.scanNativeType() {
			return false
		}
		if p.token != ast.KindTemplateMiddle && p.token != ast.KindTemplateTail {
			p.reScanTemplateToken(false)
		}
		if p.token == ast.KindTemplateTail {
			p.nextToken()
			return true
		}
		if p.token != ast.KindTemplateMiddle {
			return false
		}
		p.nextToken()
	}
}

func isNativeKeywordType(kind ast.Kind) bool {
	switch kind {
	case ast.KindAnyKeyword, ast.KindBigIntKeyword, ast.KindBooleanKeyword,
		ast.KindIntrinsicKeyword, ast.KindNeverKeyword, ast.KindNumberKeyword,
		ast.KindObjectKeyword, ast.KindStringKeyword, ast.KindSymbolKeyword,
		ast.KindUndefinedKeyword, ast.KindUnknownKeyword, ast.KindVoidKeyword,
		ast.KindConstKeyword:
		return true
	default:
		return false
	}
}
