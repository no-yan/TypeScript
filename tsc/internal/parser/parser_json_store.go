package parser

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// ParseJSONStore parses strict JSON directly into Store. Inputs requiring the
// compiler's JSON error recovery return false so callers can use ParseSourceFile.
func ParseJSONStore(opts ast.SourceFileParseOptions, sourceText string) (ast.Handle, bool) {
	p := getParser()
	defer putParser(p)
	p.initializeState(opts, sourceText, core.ScriptKindJSON)
	p.nextToken()
	factory := ast.NewFactoryHint(ast.FactoryHooks{}, max(64, len(sourceText)/10))
	root, ok := p.tryParseJSONTextHandle(factory)
	factory.Seal()
	return root, ok
}

func (p *Parser) tryParseJSONTextHandle(factory *ast.Factory) (ast.Handle, bool) {
	state := p.mark()
	diagnosticsLen := len(p.diagnostics)
	root, ok := p.parseJSONTextHandle(factory)
	if !ok || len(p.diagnostics) != diagnosticsLen {
		p.rewind(state)
		return ast.Handle{}, false
	}
	return root, true
}

func (p *Parser) parseJSONTextHandle(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	var statements ast.ListRef
	var eof ast.Handle

	if p.token == ast.KindEndOfFile {
		statements = factory.List(core.NewTextRange(pos, p.nodePos()))
		eof = p.parseJSONTokenHandle(factory)
	} else {
		expression, ok := p.parseJSONValueHandle(factory)
		if !ok || p.token != ast.KindEndOfFile {
			return ast.Handle{}, false
		}
		statement := p.finishJSONHandle(factory, factory.NewExpressionStatement(expression), pos)
		statements = factory.List(core.NewTextRange(pos, p.nodePos()), statement)
		eof = p.parseJSONTokenHandle(factory)
	}

	root := factory.NewSourceFile(statements, eof)
	return p.finishJSONHandle(factory, root, pos), true
}

func (p *Parser) parseJSONValueHandle(factory *ast.Factory) (ast.Handle, bool) {
	switch p.token {
	case ast.KindOpenBraceToken:
		return p.parseJSONObjectHandle(factory)
	case ast.KindOpenBracketToken:
		return p.parseJSONArrayHandle(factory)
	case ast.KindStringLiteral, ast.KindNumericLiteral:
		return p.parseJSONLiteralHandle(factory), true
	case ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword:
		return p.parseJSONTokenHandle(factory), true
	case ast.KindMinusToken:
		pos := p.nodePos()
		p.nextToken()
		if p.token != ast.KindNumericLiteral {
			return ast.Handle{}, false
		}
		operand := p.parseJSONLiteralHandle(factory)
		return p.finishJSONHandle(
			factory,
			factory.NewPrefixUnaryExpression(ast.KindMinusToken, operand),
			pos,
		), true
	default:
		return ast.Handle{}, false
	}
}

func (p *Parser) parseJSONArrayHandle(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	multiLine := p.hasPrecedingLineBreak()
	listPos := p.nodePos()
	elements := make([]ast.Handle, 0, 8)

	for p.token != ast.KindCloseBracketToken {
		element, ok := p.parseJSONValueHandle(factory)
		if !ok {
			return ast.Handle{}, false
		}
		elements = append(elements, element)
		if p.token != ast.KindCommaToken {
			if p.token != ast.KindCloseBracketToken {
				return ast.Handle{}, false
			}
			break
		}
		p.nextToken()
		if p.token == ast.KindCloseBracketToken {
			break
		}
	}

	list := factory.List(core.NewTextRange(listPos, p.nodePos()), elements...)
	if p.token != ast.KindCloseBracketToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	return p.finishJSONHandle(factory, factory.NewArrayLiteralExpression(list, multiLine), pos), true
}

func (p *Parser) parseJSONObjectHandle(factory *ast.Factory) (ast.Handle, bool) {
	pos := p.nodePos()
	p.nextToken()
	multiLine := p.hasPrecedingLineBreak()
	listPos := p.nodePos()
	properties := make([]ast.Handle, 0, 8)

	for p.token != ast.KindCloseBraceToken {
		propertyPos := p.nodePos()
		if p.token != ast.KindStringLiteral {
			return ast.Handle{}, false
		}
		name := p.parseJSONLiteralHandle(factory)
		if p.token != ast.KindColonToken {
			return ast.Handle{}, false
		}
		p.nextToken()
		initializer, ok := p.parseJSONValueHandle(factory)
		if !ok {
			return ast.Handle{}, false
		}
		property := p.finishJSONHandle(
			factory,
			factory.NewPropertyAssignment(0, name, ast.Handle{}, ast.Handle{}, initializer),
			propertyPos,
		)
		properties = append(properties, property)
		if p.token != ast.KindCommaToken {
			if p.token != ast.KindCloseBraceToken {
				return ast.Handle{}, false
			}
			break
		}
		p.nextToken()
		if p.token == ast.KindCloseBraceToken {
			break
		}
	}

	list := factory.List(core.NewTextRange(listPos, p.nodePos()), properties...)
	if p.token != ast.KindCloseBraceToken {
		return ast.Handle{}, false
	}
	p.nextToken()
	return p.finishJSONHandle(factory, factory.NewObjectLiteralExpression(list, multiLine), pos), true
}

func (p *Parser) parseJSONLiteralHandle(factory *ast.Factory) ast.Handle {
	pos := p.nodePos()
	text := p.scanner.TokenValue()
	tokenFlags := p.scanner.TokenFlags()
	var result ast.Handle
	switch p.token {
	case ast.KindStringLiteral:
		result = factory.NewStringLiteral(text, tokenFlags)
	case ast.KindNumericLiteral:
		result = factory.NewNumericLiteral(text, tokenFlags)
	default:
		panic("parseJSONLiteralHandle called for non-literal")
	}
	p.nextToken()
	return p.finishJSONHandle(factory, result, pos)
}

func (p *Parser) parseJSONTokenHandle(factory *ast.Factory) ast.Handle {
	pos := p.nodePos()
	kind := p.token
	p.nextToken()
	return p.finishJSONHandle(factory, factory.NewToken(kind), pos)
}

func (p *Parser) finishJSONHandle(factory *ast.Factory, h ast.Handle, pos int) ast.Handle {
	h.SetFlags(h.Flags() | p.contextFlags)
	return factory.Finish(h, core.NewTextRange(pos, p.nodePos()))
}
