package store_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// The Node versions of the comparable helpers (node_helpers.go, Z1) against
// core on a comparable stand-in: Key() is the node's identity with every nil
// as NodeKey{}, so the core helper run on the keys is what the Node helper
// must do. The inputs mix the zero Node, the sentinels of two Stores, real
// nodes of two Stores, and one real node reached twice (from its Store and
// through a foreign edge of a synthetic node).

func helperNodes(t *testing.T) []store.Node {
	p := newProgram(t, "let a = 1; let b = 2;", "let c = 3;")
	a := p.files[0].Root().AsSourceFile().Statements().At(0)
	b := p.files[0].Root().AsSourceFile().Statements().At(1)
	c := p.files[1].Root().AsSourceFile().Statements().At(0)
	f := store.NewFactory(store.NewNodeRegion(p.space), 1, store.FactoryHooks{})
	viaForeign := f.NewParenthesizedExpression(a).Expression()
	if !store.SameNode(viaForeign, a) {
		t.Fatal("a node reached through a foreign edge is not the same node")
	}
	return []store.Node{
		{},
		p.files[0].Store.Node(store.NoNodeRef),
		a,
		p.files[1].Store.Node(store.NoNodeRef),
		c,
		viaForeign,
		b,
		{},
		a,
	}
}

func keys(nodes []store.Node) []store.NodeKey {
	if nodes == nil {
		return nil
	}
	out := make([]store.NodeKey, len(nodes))
	for i, n := range nodes {
		out[i] = n.Key()
	}
	return out
}

// sameArray reports whether two slices share their first element's address,
// that is, whether a helper returned its input.
func sameArray[T any](a, b []T) bool {
	return len(a) > 0 && len(b) > 0 && &a[0] == &b[0]
}

func TestNodeHelpers(t *testing.T) {
	nodes := helperNodes(t)
	ks := keys(nodes)
	// Each subsequence, so that empty, nil and single inputs are in.
	var inputs [][]int
	for n := 0; n <= len(nodes); n++ {
		for start := 0; start+n <= len(nodes); start++ {
			idx := make([]int, n)
			for i := range idx {
				idx[i] = start + i
			}
			inputs = append(inputs, idx)
		}
	}
	pick := func(idx []int) ([]store.Node, []store.NodeKey) {
		if len(idx) == 0 {
			return nil, nil
		}
		ns := make([]store.Node, len(idx))
		kk := make([]store.NodeKey, len(idx))
		for i, j := range idx {
			ns[i], kk[i] = nodes[j], ks[j]
		}
		return ns, kk
	}
	// mapping: element i maps to element (i*5+3) mod len: nils and real nodes
	// on both sides, some changed and some not.
	mapIndex := func(i int) int { return (i*5 + 3) % len(nodes) }
	for _, idx := range inputs {
		ns, kk := pick(idx)
		label := fmt.Sprint(idx)
		positions := make([]int, len(idx))
		copy(positions, idx)

		// FirstNonNil and MapNonNil, T = int (the position), U = Node.
		var calls1, calls2 []int
		gotFirst := store.FirstNonNil(positions, func(i int) store.Node { calls1 = append(calls1, i); return nodes[mapIndex(i)] })
		wantFirst := core.FirstNonNil(positions, func(i int) store.NodeKey { calls2 = append(calls2, i); return ks[mapIndex(i)] })
		if gotFirst.Key() != wantFirst || !slices.Equal(calls1, calls2) {
			t.Errorf("%s: FirstNonNil = %v after %v, core %v after %v", label, gotFirst.Key(), calls1, wantFirst, calls2)
		}
		calls1, calls2 = nil, nil
		gotMap := store.MapNonNil(positions, func(i int) store.Node { calls1 = append(calls1, i); return nodes[mapIndex(i)] })
		wantMap := core.MapNonNil(positions, func(i int) store.NodeKey { calls2 = append(calls2, i); return ks[mapIndex(i)] })
		if !slices.Equal(keys(gotMap), wantMap) || (gotMap == nil) != (wantMap == nil) || !slices.Equal(calls1, calls2) {
			t.Errorf("%s: MapNonNil = %v, core %v", label, keys(gotMap), wantMap)
		}

		// SameMap and SameMapIndex: the same calls, and the input returned
		// exactly when core returns its input.
		var seen1, seen2 []store.NodeKey
		mapNode := func(n store.Node) store.Node {
			seen1 = append(seen1, n.Key())
			for j := range nodes {
				if store.SameNode(nodes[j], n) {
					return nodes[mapIndex(j)]
				}
			}
			return n
		}
		mapKey := func(k store.NodeKey) store.NodeKey {
			seen2 = append(seen2, k)
			for j := range ks {
				if ks[j] == k {
					return ks[mapIndex(j)]
				}
			}
			return k
		}
		gotSame := store.SameMap(ns, mapNode)
		wantSame := core.SameMap(kk, mapKey)
		if !slices.Equal(keys(gotSame), wantSame) || sameArray(gotSame, ns) != sameArray(wantSame, kk) || !slices.Equal(seen1, seen2) {
			t.Errorf("%s: SameMap = %v (reused %v), core %v (reused %v)", label, keys(gotSame), sameArray(gotSame, ns), wantSame, sameArray(wantSame, kk))
		}
		// An identity mapping reuses the input, nils of other Stores included.
		identity := store.SameMap(ns, func(n store.Node) store.Node {
			if n.IsNil() {
				return store.Node{}
			}
			return n
		})
		if len(ns) > 0 && !sameArray(identity, ns) {
			t.Errorf("%s: SameMap with an identity (nils normalized) did not reuse the input", label)
		}
		seen1, seen2 = nil, nil
		var at1, at2 []int
		gotIndex := store.SameMapIndex(ns, func(n store.Node, i int) store.Node { at1 = append(at1, i); return mapNode(n) })
		wantIndex := core.SameMapIndex(kk, func(k store.NodeKey, i int) store.NodeKey { at2 = append(at2, i); return mapKey(k) })
		if !slices.Equal(keys(gotIndex), wantIndex) || sameArray(gotIndex, ns) != sameArray(wantIndex, kk) || !slices.Equal(seen1, seen2) || !slices.Equal(at1, at2) {
			t.Errorf("%s: SameMapIndex = %v, core %v", label, keys(gotIndex), wantIndex)
		}

		// Deduplicate: the same elements in order, the input reused alike.
		gotDedup := store.Deduplicate(ns)
		wantDedup := core.Deduplicate(kk)
		if !slices.Equal(keys(gotDedup), wantDedup) || sameArray(gotDedup, ns) != sameArray(wantDedup, kk) || (gotDedup == nil) != (wantDedup == nil) {
			t.Errorf("%s: Deduplicate = %v, core %v", label, keys(gotDedup), wantDedup)
		}

		// AppendIfUnique, Contains and Index for every candidate.
		for j := range nodes {
			// Clip: an append reallocates, so a reused input is a found element.
			gotAppend := store.AppendIfUnique(slices.Clip(ns), nodes[j])
			wantAppend := core.AppendIfUnique(slices.Clip(kk), ks[j])
			if !slices.Equal(keys(gotAppend), wantAppend) || sameArray(gotAppend, ns) != sameArray(wantAppend, kk) {
				t.Errorf("%s: AppendIfUnique(%d) = %v, core %v", label, j, keys(gotAppend), wantAppend)
			}
			if store.Contains(ns, nodes[j]) != slices.Contains(kk, ks[j]) || store.Index(ns, nodes[j]) != slices.Index(kk, ks[j]) {
				t.Errorf("%s: Contains or Index of %d differs", label, j)
			}
		}
	}
	// OrElse: every pair; both arguments are the caller's (evaluated before
	// the call, as in core).
	for i := range nodes {
		for j := range nodes {
			if got, want := store.OrElse(nodes[i], nodes[j]).Key(), core.OrElse(ks[i], ks[j]); got != want {
				t.Errorf("OrElse(%d, %d) = %v, core %v", i, j, got, want)
			}
		}
	}
	// A nil input and a nil result keep their nil.
	if store.SameMap(nil, func(n store.Node) store.Node { return n }) != nil || store.Deduplicate(nil) != nil || store.MapNonNil([]int(nil), func(int) store.Node { return store.Node{} }) != nil {
		t.Error("a nil slice is not kept nil")
	}
	if store.FirstNonNil([]int{1, 2}, func(int) store.Node { return store.Node{} }).Key() != (store.NodeKey{}) {
		t.Error("FirstNonNil of only nils is not nil")
	}
}
