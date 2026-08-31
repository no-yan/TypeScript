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

// TestSourceFileRefsAreSafeAcrossParallelCheckers is intended to run under
// -race. Each checker owns its NodeFactory map, then merges newly allocated
// nodes into the SourceFile index while other checkers resolve GlobalRefs.
func TestSourceFileRefsAreSafeAcrossParallelCheckers(t *testing.T) {
	t.Parallel()
	store := ast.NewStore(64)
	parseFactory := ast.NewNodeFactory(ast.NodeFactoryHooks{})
	parseFactory.AttachStore(store)
	opts := ast.SourceFileParseOptions{FileName: "/index.ts", Path: "/index.ts"}
	root := parseFactory.NewSourceFile(opts, "", nil, nil)
	file := root.AsSourceFile()
	rootHandle := parseFactory.HandleOf(root)
	file.SetParseStore(store, rootHandle)
	file.SetParseNodeRef(parseFactory.TakeNodeRef())

	stores := ast.NewStoreSet()
	id := stores.Add(store)
	stores.SetFile(id, file)

	const checkers = 16
	nodes := make([]*ast.Node, checkers)
	refs := make([]ast.NodeRef, checkers)
	plainFactory := ast.NewNodeFactory(ast.NodeFactoryHooks{})
	for i := range checkers {
		nodes[i] = plainFactory.NewIdentifier("synthetic")
		refs[i] = store.Alloc(ast.KindIdentifier, ast.NodeFlagsSynthesized, core.UndefinedTextRange(), 0).Ref()
	}

	var wg sync.WaitGroup
	for i := range checkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			private := file.ParseNodeRef()
			private[nodes[i]] = refs[i]
			file.AbsorbNodeRef(private)
			assert.Equal(t, nodes[i], file.NodeFor(refs[i]))
			assert.Equal(t, refs[i], file.HandleOf(nodes[i]).Ref())
			assert.Equal(t, nodes[i], stores.NodeOf(ast.MakeGlobalRef(id, refs[i])))
			assert.Equal(t, root, file.NodeFor(rootHandle.Ref()))
		}()
	}
	wg.Wait()

	for i := range checkers {
		assert.Equal(t, nodes[i], file.NodeFor(refs[i]))
	}
}

func TestSourceFileSerializesParseStoreWriters(t *testing.T) {
	t.Parallel()
	store := ast.NewStore(512)
	parseFactory := ast.NewNodeFactory(ast.NodeFactoryHooks{})
	parseFactory.AttachStore(store)
	opts := ast.SourceFileParseOptions{FileName: "/index.ts", Path: "/index.ts"}
	root := parseFactory.NewSourceFile(opts, "", nil, nil)
	file := root.AsSourceFile()
	file.SetParseStore(store, parseFactory.HandleOf(root))
	file.SetParseNodeRef(parseFactory.TakeNodeRef())

	const writers = 4
	const nodesPerWriter = 128
	var wg sync.WaitGroup
	for writer := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := file.LockParseStoreWriter()
			defer unlock()

			factory := ast.NewNodeFactory(ast.NodeFactoryHooks{})
			factory.AttachStoreMap(store, file.ParseNodeRef())
			for node := range nodesPerWriter {
				factory.NewIdentifier(string(rune('a' + (writer+node)%26)))
			}
			file.AbsorbNodeRef(factory.TakeNodeRef())
		}()
	}
	wg.Wait()

	assert.Equal(t, 1+writers*nodesPerWriter, store.Len())
	assert.Equal(t, store.Len(), len(file.ParseNodeRef()))
}
