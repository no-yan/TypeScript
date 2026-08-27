package ast_test

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"gotest.tools/v3/assert"
)

func TestGlobalRefPackUnpack(t *testing.T) {
	t.Parallel()
	g := ast.MakeGlobalRef(3, 41)
	assert.Equal(t, ast.StoreID(3), g.StoreID())
	assert.Equal(t, ast.NodeRef(41), g.Ref())

	assert.Equal(t, ast.GlobalRef(0), ast.MakeGlobalRef(0, 41))
	assert.Equal(t, ast.GlobalRef(0), ast.MakeGlobalRef(3, 0))
}

func TestStoreSetAssignsSequentialIDs(t *testing.T) {
	t.Parallel()
	ss := ast.NewStoreSet()
	a := ast.NewStore(2)
	b := ast.NewStore(2)
	assert.Equal(t, ast.StoreID(0), a.ID())
	assert.Equal(t, ast.StoreID(1), ss.Add(a))
	assert.Equal(t, ast.StoreID(2), ss.Add(b))
	assert.Equal(t, ast.StoreID(1), a.ID())
	assert.Equal(t, a, ss.Store(1))
	assert.Equal(t, b, ss.Store(2))
	assert.Assert(t, ss.Store(0) == nil)
	assert.Assert(t, ss.Store(99) == nil)
}

func TestStoreSetResolvesGlobalRef(t *testing.T) {
	t.Parallel()
	ss := ast.NewStoreSet()
	a := ast.NewStore(2)
	b := ast.NewStore(2)
	ss.Add(a)
	ss.Add(b)

	inA := a.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
	inB := b.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)

	gA, gB := inA.Global(), inB.Global()
	assert.Assert(t, gA != gB)
	assert.Equal(t, inA.Ref(), ss.At(gA).Ref())
	assert.Equal(t, a, ss.At(gA).Store())
	assert.Equal(t, b, ss.At(gB).Store())
	assert.Equal(t, ast.NodeRef(0), ss.At(0).Ref())
}

func TestGlobalPanicsOnUnregisteredStore(t *testing.T) {
	t.Parallel()
	s := ast.NewStore(2)
	h := s.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	h.Global()
}

func TestStoreSetRejectsDoubleRegistration(t *testing.T) {
	t.Parallel()
	ss := ast.NewStoreSet()
	s := ast.NewStore(2)
	ss.Add(s)
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	ast.NewStoreSet().Add(s)
}

func TestZeroHandleGlobalIsZero(t *testing.T) {
	t.Parallel()
	var h ast.Handle
	assert.Equal(t, ast.GlobalRef(0), h.Global())
}

func TestStoreSetFile(t *testing.T) {
	t.Parallel()
	ss := ast.NewStoreSet()
	s := ast.NewStore(2)
	id := ss.Add(s)
	sf := &ast.SourceFile{}
	ss.SetFile(id, sf)
	assert.Equal(t, sf, ss.File(id))
	assert.Assert(t, ss.File(0) == nil)
}
