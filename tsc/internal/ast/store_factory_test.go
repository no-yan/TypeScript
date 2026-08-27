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

	assert.Equal(t, ast.KindBinaryExpression, bin.Kind())
	assert.Equal(t, "a", bin.Left().Text())
	assert.Equal(t, "b", bin.Right().Text())
	assert.Equal(t, ast.KindPlusToken, bin.Operator().Kind())
	assert.Equal(t, bin.Ref(), left.Parent().Ref())

	var kinds []ast.Kind
	ast.Walk(bin, func(h ast.Handle) bool {
		kinds = append(kinds, h.Kind())
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
