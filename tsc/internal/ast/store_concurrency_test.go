package ast

import (
	"runtime"
	"sync"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
)

func TestStoreSetPublishesBeforeID(t *testing.T) {
	ss := NewStoreSet()
	const count = 1100 // crosses page and directory growth boundaries
	stores := make([]*Store, count)
	for i := range stores {
		stores[i] = NewStore(1)
	}
	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Go(func() {
			for i := worker; i < count; i += 4 {
				ss.Add(stores[i])
			}
		})
	}
	for _, store := range stores {
		for store.ID() == 0 {
			runtime.Gosched()
		}
		if ss.Store(store.ID()) != store {
			t.Fatal("ID visible before Store publication")
		}
	}
	wg.Wait()
	if ss.liveCount() != count {
		t.Fatal("incorrect live count")
	}
}

func TestConcurrentRegisterStoreIsIdempotent(t *testing.T) {
	s := NewStore(1)
	defer UnregisterStore(s)
	var wg sync.WaitGroup
	ids := make([]StoreID, 32)
	for i := range ids {
		wg.Go(func() { ids[i] = RegisterStore(s) })
	}
	wg.Wait()
	for _, id := range ids {
		if id != s.ID() {
			t.Fatal("registration assigned multiple IDs")
		}
	}
	if identitySet().Store(s.ID()) != s {
		t.Fatal("missing registered Store")
	}
}

func TestStoreSetRemoveAndRestore(t *testing.T) {
	ss := NewStoreSet()
	s := NewStore(1)
	id := ss.Add(s)
	other := NewStoreSet()
	foreign := NewStore(1)
	other.Add(foreign)
	ss.Remove(foreign)
	if ss.Store(id) != s {
		t.Fatal("foreign Remove removed a local identity")
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 10000 {
			if got := ss.Store(id); got != nil && got != s {
				t.Error("identity changed")
			}
		}
	})
	ss.Remove(s)
	ss.Remove(s)
	wg.Wait()
	if ss.Store(id) != nil || ss.liveCount() != 0 {
		t.Fatal("Remove did not clear identity")
	}
	if ss.register(s, false, false) != id || ss.Store(id) != nil {
		t.Fatal("registration resurrected removed Store")
	}
	ss.register(s, false, true)
	if ss.Store(id) != s || ss.liveCount() != 1 {
		t.Fatal("same-domain restore failed")
	}
	defer func() {
		if recover() == nil {
			t.Error("foreign adoption accepted")
		}
	}()
	other.register(s, false, true)
}

func TestStoreSetExhaustionDoesNotPublish(t *testing.T) {
	ss := NewStoreSet()
	ss.highWater = uint64(^StoreID(0))
	s := NewStore(1)
	defer func() {
		if recover() == nil {
			t.Error("expected exhaustion")
		}
		if s.ID() != 0 {
			t.Error("failed registration published ID")
		}
	}()
	ss.Add(s)
}

func TestSourceFilePublishesCoherentTree(t *testing.T) {
	file := &SourceFile{}
	a, b := NewStore(1), NewStore(1)
	ra := a.Alloc(KindSourceFile, 0, core.NewTextRange(0, 1), 0)
	rb := b.Alloc(KindSourceFile, 0, core.NewTextRange(0, 2), 0)
	file.SetParseRoot(ra)
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 10000 {
			file.SetParseRoot(rb)
			file.SetParseRoot(ra)
		}
	})
	for range 10000 {
		s, ref := file.ParseTreeRef()
		if (s != a && s != b) || ref != 1 {
			t.Fatal("torn tree publication")
		}
		root := file.ParseRoot()
		if root != ra && root != rb {
			t.Fatal("torn root publication")
		}
	}
	wg.Wait()
}

func TestPrivateStoreSharesFrozenForeignTree(t *testing.T) {
	parse := NewFactory(FactoryHooks{})
	name := parse.NewIdentifier("original")
	root := parse.NewSourceFile(parse.NewList([]Handle{name}), parse.NewToken(KindEndOfFile))
	file := NewSourceFileMetadata(SourceFileParseOptions{FileName: "/a.ts", Path: "/a.ts"}, "")
	file.SetParseStore(parse.Store(), root)
	parse.Store().Freeze()
	defer UnregisterStore(parse.Store())
	before := parse.Store().Len()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			f := NewFactory(FactoryHooks{})
			defer UnregisterStore(f.Store())
			for range 100 {
				// Whole-list sharing and mixed-list sharing must retain parse identity.
				shared := f.NewSourceFile(root.List(), root.SourceFileEndOfFileToken())
				if shared.List() != root.List() || shared.Statements()[0] != name {
					t.Error("foreign list identity lost")
				}
				mixed := f.NewSourceFile(f.NewList([]Handle{name, f.NewIdentifier("generated")}), root.SourceFileEndOfFileToken())
				mixed.SetParent(root)
				if mixed.Parent() != root || mixed.Statements()[0] != name {
					t.Error("foreign edge identity lost")
				}
				if name.Parent() != root || name.Text() != "original" {
					t.Error("parse tree mutated")
				}
			}
		})
	}
	wg.Wait()
	if parse.Store().Len() != before {
		t.Fatal("private allocation grew parse Store")
	}
}

func TestForeignEdgesRetainOwnerAfterUnregister(t *testing.T) {
	a, b := NewFactory(FactoryHooks{}), NewFactory(FactoryHooks{})
	name := a.NewIdentifier("retained")
	list := a.NewList([]Handle{name})
	root := b.NewSourceFile(list, Handle{})
	child := b.NewIdentifier("child")
	child.SetParent(name)
	root.SetChild(0, name)
	seq := NewStore(1).ListSlice(list)
	UnregisterStore(a.Store())
	if seq.First() != name {
		t.Fatal("NodeSeq did not retain list owner")
	}
	defer UnregisterStore(b.Store())
	if root.Statements()[0] != name || root.Child(0) != name || child.Parent() != name {
		t.Fatal("unregistration broke live structural references")
	}
}

func BenchmarkStoreSetReadParallel(b *testing.B) {
	ss := NewStoreSet()
	s := NewStore(1)
	id := ss.Add(s)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if ss.Store(id) != s {
				b.Fatal("lost Store")
			}
		}
	})
}

func BenchmarkParseRootParallel(b *testing.B) {
	s := NewStore(1)
	root := s.Alloc(KindSourceFile, 0, core.UndefinedTextRange(), 0)
	file := &SourceFile{}
	file.SetParseStore(s, root)
	defer UnregisterStore(s)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if file.ParseRoot() != root {
				b.Fatal("lost root")
			}
		}
	})
}

func TestRestoreDropsSpeculativeForeignEdges(t *testing.T) {
	src, dst := NewFactory(FactoryHooks{}), NewFactory(FactoryHooks{})
	defer UnregisterStore(src.Store())
	defer UnregisterStore(dst.Store())
	name := src.NewIdentifier("foreign")
	list := src.NewList([]Handle{name})
	cp := dst.Store().Checkpoint()
	root := dst.NewSourceFile(list, name)
	root.SetParent(name)
	dst.NewList([]Handle{name})
	dst.Store().Restore(cp)
	replacement := dst.NewSourceFile(0, Handle{})
	empty := dst.Store().AllocList(core.UndefinedTextRange(), 1)
	if replacement.List() != 0 || !replacement.Parent().IsNil() || !replacement.SourceFileEndOfFileToken().IsNil() || !dst.Store().ListAt(empty, 0).IsNil() {
		t.Fatal("speculative foreign reference survived Restore")
	}
}
