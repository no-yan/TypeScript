package ast_test

import (
	"sync"
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

func registeredSourceFile() (*ast.SourceFile, ast.Handle) {
	factory := ast.NewFactory(ast.FactoryHooks{})
	eof := factory.NewToken(ast.KindEndOfFile)
	root := factory.NewSourceFile(0, eof)
	opts := ast.SourceFileParseOptions{FileName: "/index.ts", Path: "/index.ts"}
	file := ast.NewSourceFileMetadata(opts, "")
	file.SetParseStore(factory.Store(), root)
	return file, root
}

// TestSourceFileRefsAreSafeAcrossParallelCheckers is intended to run under
// -race. Checkers resolve RegisterFile identity (ParseRoot, Store.At, GlobalRef)
// on a shared file.
func TestSourceFileRefsAreSafeAcrossParallelCheckers(t *testing.T) {
	t.Parallel()
	file, root := registeredSourceFile()
	store := file.ParseStore()

	const checkers = 16
	handles := make([]ast.Handle, checkers)
	for i := range checkers {
		handles[i] = store.Alloc(ast.KindIdentifier, ast.NodeFlagsSynthesized, core.UndefinedTextRange(), 0)
	}

	var wg sync.WaitGroup
	for i := range checkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g := handles[i].Global()
			assert.Equal(t, handles[i].Ref(), ast.NodeOf(g).Ref())
			assert.Equal(t, handles[i].Ref(), store.At(handles[i].Ref()).Ref())
			assert.Equal(t, store, ast.NodeOf(g).Store())
			assert.Equal(t, root.Ref(), file.ParseRoot().Ref())
		}()
	}
	wg.Wait()

	for i := range checkers {
		assert.Equal(t, handles[i].Ref(), ast.NodeOf(handles[i].Global()).Ref())
	}
}

func TestSourceFileSerializesParseStoreWriters(t *testing.T) {
	t.Parallel()
	file, root := registeredSourceFile()
	store := file.ParseStore()
	before := store.Len()

	const writers = 4
	const nodesPerWriter = 128
	var wg sync.WaitGroup
	for writer := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := file.LockParseStoreWriter()
			defer unlock()

			factory := ast.NewFactoryOn(store, ast.FactoryHooks{})
			for node := range nodesPerWriter {
				factory.NewIdentifier(string(rune('a' + (writer+node)%26)))
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, before+writers*nodesPerWriter, store.Len())
	assert.Equal(t, root.Ref(), file.ParseRoot().Ref())
	assert.Equal(t, root.Ref(), store.At(root.Ref()).Ref())
	assert.Equal(t, root.Ref(), ast.NodeOf(root.Global()).Ref())
}

func TestUnregisterStoreNilsIdentitySlot(t *testing.T) {
	t.Parallel()
	s := ast.NewStore(2)
	id := ast.RegisterStore(s)
	h := s.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
	g := h.Global()
	assert.Equal(t, h.Ref(), ast.NodeOf(g).Ref())
	ast.UnregisterStore(s)
	assert.Equal(t, ast.Handle{}, ast.NodeOf(g))
	assert.Equal(t, id, s.ID())
	ast.UnregisterStore(s)
	ast.UnregisterStore(nil)
}

func TestGetSourceFileOfNodeWalksParentThenNil(t *testing.T) {
	t.Parallel()
	file, root := registeredSourceFile()
	ctor := file.ParseStore().Alloc(ast.KindConstructor, 0, core.UndefinedTextRange(), 0)
	ctor.SetParent(root)

	synthStore := ast.NewStore(4)
	ast.RegisterStore(synthStore)
	t.Cleanup(func() { ast.UnregisterStore(synthStore) })
	synth := synthStore.Alloc(ast.KindPropertyAccessExpression, ast.NodeFlagsSynthesized, core.UndefinedTextRange(), 2)
	synth.SetParent(ctor)
	assert.Equal(t, file, ast.GetSourceFileOfNode(synth))

	orphan := synthStore.Alloc(ast.KindFunctionType, ast.NodeFlagsSynthesized, core.UndefinedTextRange(), 0)
	assert.Assert(t, ast.GetSourceFileOfNode(orphan) == nil)
}
