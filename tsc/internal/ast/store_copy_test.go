package ast_test

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"gotest.tools/v3/assert"
)

func TestFactoryCopySubtree(t *testing.T) {
	t.Parallel()
	parse := ast.NewFactory(ast.FactoryHooks{})
	left := parse.Identifier("a")
	right := parse.Identifier("b")
	op := parse.Token(ast.KindPlusToken)
	root := parse.BinaryExpression(ast.BinaryParts{
		Left: left, Operator: op, Right: right,
		Loc: core.NewTextRange(0, 5),
	})
	parse.Seal()

	emit := ast.NewFactory(ast.FactoryHooks{})
	copied := emit.CopySubtree(root)

	assert.Equal(t, ast.KindBinaryExpression, copied.Kind())
	assert.Equal(t, "a", copied.Left().Text())
	assert.Equal(t, "b", copied.Right().Text())
	assert.Equal(t, ast.KindPlusToken, copied.Operator().Kind())

	assert.Assert(t, emit.Store() != parse.Store())
	assert.Assert(t, copied.Ref() != root.Ref())
	assert.Assert(t, copied.Left().Ref() != left.Ref())

	copied.SetParentsInChildren()
	assert.Equal(t, copied.Ref(), copied.Left().Parent().Ref())

	copied.SetChild(0, left)
	got := copied.Child(0)
	assert.Equal(t, emit.Store(), got.Store())
	assert.Assert(t, got.Store() != left.Store())
	assert.Equal(t, "a", got.Text())
}

func TestFactoryCopySubtreeRemapsList(t *testing.T) {
	t.Parallel()
	parse := ast.NewFactory(ast.FactoryHooks{})
	a := parse.Identifier("a")
	a.SetLoc(core.NewTextRange(1, 2))
	a.SetTokenFlags(ast.TokenFlagsSingleQuote)
	b := parse.Identifier("b")
	b.SetLoc(core.NewTextRange(4, 5))
	arr := parse.ArrayLiteral(ast.ArrayLiteralParts{
		Elements: parse.List(core.NewTextRange(1, 6), a, b),
		Loc:      core.NewTextRange(0, 7),
	})
	parse.Seal()

	emit := ast.NewFactory(ast.FactoryHooks{})
	copied := emit.CopySubtree(arr)
	assert.Equal(t, ast.KindArrayLiteralExpression, copied.Kind())
	assert.Equal(t, 2, emit.Store().ListLen(copied.ElementList()))
	assert.Assert(t, emit.Store().ListHasTrailingComma(copied.ElementList()))
	ca := emit.Store().ListAt(copied.ElementList(), 0)
	cb := emit.Store().ListAt(copied.ElementList(), 1)
	assert.Equal(t, "a", ca.Text())
	assert.Equal(t, "b", cb.Text())
	assert.Equal(t, ast.TokenFlagsSingleQuote, ca.TokenFlags())
	assert.Assert(t, ca.Ref() != a.Ref())
	assert.Assert(t, ca.Store() != a.Store())
	assert.Equal(t, copied.Store(), ca.Store())
}
