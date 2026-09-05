package ast_test

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"gotest.tools/v3/assert"
)

func TestFactoryIdentifierTokenBinary(t *testing.T) {
	t.Parallel()
	f := ast.NewFactory(ast.FactoryHooks{})
	left := f.Identifier("a")
	right := f.Identifier("b")
	op := f.Token(ast.KindPlusToken)
	bin := f.BinaryExpression(ast.BinaryParts{
		Left: left, Operator: op, Right: right,
		Loc: core.NewTextRange(0, 5),
	})
	bin.SetParentsInChildren()

	assert.Equal(t, ast.KindBinaryExpression, bin.Kind)
	assert.Equal(t, "a", bin.Left().Text())
	assert.Equal(t, "b", bin.Right().Text())
	assert.Equal(t, ast.KindPlusToken, bin.Operator().Kind)
	assert.Equal(t, bin.Ref(), left.Parent().Ref())

	var kinds []ast.Kind
	ast.Walk(bin, func(h ast.Handle) bool {
		kinds = append(kinds, h.Kind)
		return false
	})
	assert.DeepEqual(t, []ast.Kind{
		ast.KindBinaryExpression,
		ast.KindIdentifier,
		ast.KindPlusToken,
		ast.KindIdentifier,
	}, kinds)

	f.Seal()
}

func TestFactoryList(t *testing.T) {
	t.Parallel()
	f := ast.NewFactory(ast.FactoryHooks{})
	a := f.Identifier("a")
	b := f.Identifier("b")
	list := f.List(core.NewTextRange(1, 4), a, b)
	assert.Equal(t, 2, f.Store().ListLen(list))
	assert.Equal(t, a.Ref(), f.Store().ListAt(list, 0).Ref())
	assert.Equal(t, b.Ref(), f.Store().ListAt(list, 1).Ref())
}

func TestFactorySymbolSideMap(t *testing.T) {
	t.Parallel()
	f := ast.NewFactory(ast.FactoryHooks{})
	id := f.Identifier("x")
	sym := &ast.Symbol{Name: "x"}
	id.SetSymbol(sym)
	assert.Equal(t, sym, id.Symbol())
	id.SetSymbol(nil)
	assert.Assert(t, id.Symbol() == nil)
}

func TestFactoryOnCreateHook(t *testing.T) {
	t.Parallel()
	var n int
	f := ast.NewFactory(ast.FactoryHooks{
		OnCreate: func(ast.Handle) { n++ },
	})
	f.Identifier("a")
	f.Token(ast.KindPlusToken)
	assert.Equal(t, 2, n)
}

func TestFactoryArrayLiteralListAndTrailingComma(t *testing.T) {
	t.Parallel()
	f := ast.NewFactory(ast.FactoryHooks{})
	a := f.Identifier("a")
	a.SetLoc(core.NewTextRange(1, 2))
	b := f.Identifier("b")
	b.SetLoc(core.NewTextRange(4, 5))
	elems := f.List(core.NewTextRange(1, 6), a, b)
	arr := f.ArrayLiteral(ast.ArrayLiteralParts{Elements: elems, Loc: core.NewTextRange(0, 7)})
	arr.SetParentsInChildren()

	assert.Equal(t, elems, arr.ElementList())
	assert.Equal(t, 2, f.Store().ListLen(arr.ElementList()))
	assert.Assert(t, f.Store().ListHasTrailingComma(arr.ElementList()))
	assert.Equal(t, arr.Ref(), a.Parent().Ref())
	assert.Equal(t, arr.Ref(), b.Parent().Ref())

	var kinds []ast.Kind
	ast.Walk(arr, func(h ast.Handle) bool {
		kinds = append(kinds, h.Kind)
		return false
	})
	assert.DeepEqual(t, []ast.Kind{
		ast.KindArrayLiteralExpression,
		ast.KindIdentifier,
		ast.KindIdentifier,
	}, kinds)
}

func TestFactoryReplaceListAfterCreate(t *testing.T) {
	t.Parallel()
	f := ast.NewFactory(ast.FactoryHooks{})
	x := f.Identifier("x")
	fn := f.FunctionExpression(core.NewTextRange(0, 10), f.List(core.NewTextRange(1, 2), x))
	thisName := f.Identifier("this")
	thisParam := f.Parameter(ast.ParameterParts{Name: thisName, Loc: core.NewTextRange(1, 5)})
	fn.SetFunctionExpressionParameters(f.List(core.NewTextRange(1, 8), thisParam, x))
	parameters := fn.FunctionExpressionParameters()
	assert.Equal(t, 2, f.Store().ListLen(parameters))
	assert.Equal(t, thisParam.Ref(), f.Store().ListAt(parameters, 0).Ref())
	assert.Equal(t, x.Ref(), f.Store().ListAt(parameters, 1).Ref())
}

func TestFactoryParamTypeWrittenAfterCreate(t *testing.T) {
	t.Parallel()
	f := ast.NewFactory(ast.FactoryHooks{})
	name := f.Identifier("p")
	param := f.Parameter(ast.ParameterParts{Name: name, Loc: core.NewTextRange(0, 1)})
	assert.Equal(t, ast.NodeRef(0), param.ParamType().Ref())
	typ := f.Identifier("string")
	param.SetParamType(typ)
	assert.Equal(t, typ.Ref(), param.ParamType().Ref())
	q := f.Token(ast.KindQuestionToken)
	param.SetParamQuestion(q)
	assert.Equal(t, q.Ref(), param.ParamQuestion().Ref())
}

func TestFactoryTokenFlagsAndFlowSideMap(t *testing.T) {
	t.Parallel()
	f := ast.NewFactory(ast.FactoryHooks{})
	lit := f.Identifier("'x'")
	lit.SetTokenFlags(ast.TokenFlagsSingleQuote)
	assert.Equal(t, ast.TokenFlagsSingleQuote, lit.TokenFlags())
	flow := &ast.FlowNode{Flags: ast.FlowFlagsStart}
	lit.SetFlowNode(flow)
	assert.Equal(t, flow, lit.FlowNode())
	lit.SetFlowNode(nil)
	assert.Assert(t, lit.FlowNode() == nil)
}

func TestFactoryOnExistingStore(t *testing.T) {
	t.Parallel()
	parse := ast.NewFactory(ast.FactoryHooks{})
	id := parse.Identifier("a")
	synth := ast.NewFactoryOn(parse.Store(), ast.FactoryHooks{})
	extra := synth.Identifier("b")
	assert.Equal(t, parse.Store(), extra.Store())
	assert.Assert(t, extra.Ref() != id.Ref())
	assert.Equal(t, 2, parse.Store().Len())
}

func TestGeneratedHandleFactoryAccessorsAndFinish(t *testing.T) {
	t.Parallel()
	f := ast.NewFactory(ast.FactoryHooks{})

	name := f.Finish(f.NewIdentifier("answer"), core.NewTextRange(6, 12))
	value := f.Finish(
		f.NewNumericLiteral("0x2a", ast.TokenFlagsHexSpecifier),
		core.NewTextRange(15, 19),
	)
	declaration := f.Finish(
		f.NewVariableDeclaration(name, ast.Handle{}, ast.Handle{}, value),
		core.NewTextRange(6, 19),
	)
	declarations := f.List(core.NewTextRange(6, 19), declaration)
	declarationList := f.Finish(
		f.NewVariableDeclarationList(declarations, ast.NodeFlagsConst),
		core.NewTextRange(0, 19),
	)
	statement := f.Finish(
		f.NewVariableStatement(0, declarationList),
		core.NewTextRange(0, 20),
	)
	statements := f.List(core.NewTextRange(0, 20), statement)
	block := f.Finish(f.NewBlock(statements, true), core.NewTextRange(0, 22))

	assert.Equal(t, "answer", name.IdentifierText())
	assert.Equal(t, "0x2a", value.NumericLiteralText())
	assert.Equal(t, ast.TokenFlagsHexSpecifier, value.NumericLiteralTokenFlags())
	assert.Equal(t, name.Ref(), declaration.VariableDeclarationName().Ref())
	assert.Equal(t, value.Ref(), declaration.VariableDeclarationInitializer().Ref())
	assert.Equal(t, declarations, declarationList.VariableDeclarationListDeclarations())
	assert.Equal(t, ast.NodeFlagsConst, declarationList.Flags())
	assert.Equal(t, declarationList.Ref(), statement.VariableStatementDeclarationList().Ref())
	assert.Equal(t, statements, block.BlockStatements())
	assert.Assert(t, block.BlockMultiLine())
	assert.Equal(t, block.Ref(), statement.Parent().Ref())
	assert.Equal(t, statement.Ref(), declarationList.Parent().Ref())
	assert.Equal(t, declaration.Ref(), name.Parent().Ref())
	assert.Equal(t, core.NewTextRange(0, 22), block.Loc())

	typ := f.Finish(f.NewKeywordTypeNode(ast.KindNumberKeyword), core.NewTextRange(13, 13))
	declaration.SetVariableDeclarationType(typ)
	assert.Equal(t, typ.Ref(), declaration.VariableDeclarationType().Ref())
	assert.Equal(t, declaration.Ref(), typ.Parent().Ref())
}

func TestFinishParentsNamedAndListChildren(t *testing.T) {
	t.Parallel()
	f := ast.NewFactory(ast.FactoryHooks{})
	left := f.NewIdentifier("a")
	right := f.NewIdentifier("b")
	op := f.NewToken(ast.KindPlusToken)
	bin := f.Finish(f.NewBinaryExpression(0, left, ast.Handle{}, op, right), core.NewTextRange(0, 5))
	assert.Equal(t, bin.Ref(), left.Parent().Ref())
	assert.Equal(t, bin.Ref(), right.Parent().Ref())
	assert.Equal(t, bin.Ref(), op.Parent().Ref())

	a := f.NewIdentifier("x")
	b := f.NewIdentifier("y")
	elems := f.List(core.NewTextRange(1, 5), a, b)
	assert.Assert(t, a.Parent().IsNil())
	assert.Assert(t, b.Parent().IsNil())
	arr := f.Finish(f.NewArrayLiteralExpression(elems, false), core.NewTextRange(0, 6))
	assert.Equal(t, arr.Ref(), a.Parent().Ref())
	assert.Equal(t, arr.Ref(), b.Parent().Ref())
}

func TestFactoryUpdateReusesHandleAndListRef(t *testing.T) {
	t.Parallel()
	f := ast.NewFactory(ast.FactoryHooks{})
	left := f.Identifier("a")
	right := f.Identifier("b")
	op := f.Token(ast.KindPlusToken)
	bin := f.NewBinaryExpression(0, left, ast.Handle{}, op, right)
	same := f.UpdateBinaryExpression(bin, 0, left, ast.Handle{}, op, right)
	assert.Equal(t, bin.Ref(), same.Ref())

	other := f.Identifier("c")
	changed := f.UpdateBinaryExpression(bin, 0, left, ast.Handle{}, op, other)
	assert.Assert(t, changed.Ref() != bin.Ref())
	assert.Equal(t, other.Ref(), changed.BinaryExpressionRight().Ref())
	assert.Equal(t, bin.Loc(), changed.Loc())
	assert.Equal(t, bin.Flags(), changed.Flags())

	a := f.Identifier("x")
	b := f.Identifier("y")
	list := f.List(core.NewTextRange(0, 3), a, b)
	block := f.NewBlock(list, true)
	sameBlock := f.UpdateBlock(block, list, true)
	assert.Equal(t, block.Ref(), sameBlock.Ref())

	list2 := f.List(core.NewTextRange(0, 3), a, b)
	newBlock := f.UpdateBlock(block, list2, true)
	assert.Assert(t, newBlock.Ref() != block.Ref())
	assert.Equal(t, list2, newBlock.BlockStatements())
}
