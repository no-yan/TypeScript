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

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	copied.SetChild(0, left)
}
