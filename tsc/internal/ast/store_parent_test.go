package ast_test

import (
	"sync"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"gotest.tools/v3/assert"
)

func TestSetParentPreservesForeignIdentity(t *testing.T) {
	src := ast.NewFactory(ast.FactoryHooks{})
	dst := ast.NewFactory(ast.FactoryHooks{})
	ast.RegisterStore(src.Store())
	ast.RegisterStore(dst.Store())

	ctor := src.Identifier("C")
	class := src.Identifier("Class")
	ctor.SetParent(class)
	sym := &ast.Symbol{Name: "x"}
	ctor.SetSymbol(sym)

	ref := dst.Identifier("this")
	ref.SetParent(ctor)

	parent := ref.Parent()
	assert.Equal(t, ctor.Store(), parent.Store())
	assert.Equal(t, ctor.Ref(), parent.Ref())
	assert.Equal(t, ctor.Global(), parent.Global())
	assert.Equal(t, sym, parent.Symbol())
	assert.Equal(t, class.Ref(), parent.Parent().Ref())
	assert.Equal(t, class.Store(), parent.Parent().Store())
}

func TestSetChildPreservesForeignIdentity(t *testing.T) {
	src := ast.NewFactory(ast.FactoryHooks{})
	dst := ast.NewFactory(ast.FactoryHooks{})
	ast.RegisterStore(src.Store())
	ast.RegisterStore(dst.Store())

	name := src.Identifier("x")
	access := dst.NewPropertyAccessExpression(dst.NewKeywordExpression(ast.KindThisKeyword), ast.Handle{}, name, ast.NodeFlagsNone)
	got := access.Name()
	assert.Equal(t, name.Store(), got.Store())
	assert.Equal(t, name.Ref(), got.Ref())
	assert.Equal(t, name.Global(), got.Global())
	assert.Assert(t, name.Parent().IsNil())
}

func TestSetChildDoesNotParentForeignChild(t *testing.T) {
	src := ast.NewFactory(ast.FactoryHooks{})
	dst := ast.NewFactory(ast.FactoryHooks{})
	ast.RegisterStore(src.Store())
	ast.RegisterStore(dst.Store())

	name := src.Identifier("x")
	access := dst.NewPropertyAccessExpression(dst.NewKeywordExpression(ast.KindThisKeyword), ast.Handle{}, name, ast.NodeFlagsNone)
	dst.Finish(access, core.NewTextRange(0, 3))
	assert.Assert(t, name.Parent().IsNil())
}

func TestSetParentDoesNotRaceSourceStoreMaps(t *testing.T) {
	src := ast.NewFactory(ast.FactoryHooks{})
	dst := ast.NewFactory(ast.FactoryHooks{})
	ast.RegisterStore(src.Store())
	ast.RegisterStore(dst.Store())

	ctor := src.Identifier("C")
	sym := &ast.Symbol{Name: "x"}
	ctor.SetSymbol(sym)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 20000 {
			ctor.SetSymbol(sym)
		}
	}()
	for range 2000 {
		ref := dst.Identifier("this")
		ref.SetParent(ctor)
	}
	wg.Wait()
}

func TestEmitSetChildDoesNotRepointParseParent(t *testing.T) {
	t.Parallel()
	f := ast.NewFactory(ast.FactoryHooks{})
	loc := core.NewTextRange(0, 1)
	child := f.Store().Alloc(ast.KindIdentifier, 0, loc, 0)
	parent := f.Store().Alloc(ast.KindBinaryExpression, 0, loc, 2)
	parent.SetChild(0, child)
	assert.Equal(t, parent.Ref(), child.Parent().Ref())

	f.Store().Freeze()
	f.Store().EnterEmit()

	synth := f.Store().Alloc(ast.KindBinaryExpression, 0, loc, 2)
	synth.SetChild(0, child)
	assert.Equal(t, parent.Ref(), child.Parent().Ref())
	assert.Equal(t, child.Ref(), synth.Child(0).Ref())
}

func TestBuildSetChildStillSetsParent(t *testing.T) {
	t.Parallel()
	f := ast.NewFactory(ast.FactoryHooks{})
	loc := core.NewTextRange(0, 1)
	child := f.Store().Alloc(ast.KindIdentifier, 0, loc, 0)
	parent := f.Store().Alloc(ast.KindBinaryExpression, 0, loc, 2)
	parent.SetChild(0, child)
	assert.Equal(t, parent.Ref(), child.Parent().Ref())
}
