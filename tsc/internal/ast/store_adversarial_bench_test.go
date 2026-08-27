package ast_test

// Adversarial counter-benchmarks for the Store layout hypothesis.
//
// Store is packed-header SoA (one noscan []nodeHeader + packed children).
// These benches still matter: they check whether GC wins survive ballast,
// and whether multi-field / random access regresses versus a bare ptr tree.
//
// Attacks:
//  1. GC "pause" benches lack a floor: forced runtime.GC() has a fixed cost
//     that must be subtracted before claiming a ratio.
//  2. The live heap is only the tree under test; with realistic scannable
//     ballast the relative GC advantage shrinks.
//  3. Walk benches that read only Kind cherry-pick SoA's best pattern.
//     Reading kind+flags+pos+end is the fairer CPU test for packed headers.
//  4. DFS walk order equals allocation order. Shuffled access models
//     checker-style non-linear traversal.

import (
	"math/rand/v2"
	"runtime"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
)

func BenchmarkAdvGCBaselineEmptyHeap(b *testing.B) {
	runtime.GC()
	b.ResetTimer()
	for b.Loop() {
		runtime.GC()
	}
}

// makeBallast models "the rest of the compiler heap": 16 MiB of pointer
// slots the GC must scan regardless of how the AST is laid out.
func makeBallast() []*int64 {
	ints := make([]int64, 1<<21)
	ballast := make([]*int64, 1<<21)
	for i := range ballast {
		ballast[i] = &ints[i]
	}
	return ballast
}

func BenchmarkAdvGCBallastOnly(b *testing.B) {
	ballast := makeBallast()
	runtime.GC()
	b.ResetTimer()
	for b.Loop() {
		runtime.GC()
	}
	runtime.KeepAlive(ballast)
}

func BenchmarkAdvGCStoreWithBallast(b *testing.B) {
	ballast := makeBallast()
	s := ast.NewStore(1 << 16)
	root := buildStoreTree(s, benchTreeDepth)
	runtime.GC()
	b.ResetTimer()
	for b.Loop() {
		runtime.GC()
	}
	runtime.KeepAlive(ballast)
	runtime.KeepAlive(s)
	runtime.KeepAlive(root)
}

func BenchmarkAdvGCPtrTreeWithBallast(b *testing.B) {
	ballast := makeBallast()
	root := buildPtrTree(benchTreeDepth)
	runtime.GC()
	b.ResetTimer()
	for b.Loop() {
		runtime.GC()
	}
	runtime.KeepAlive(ballast)
	runtime.KeepAlive(root)
}

func BenchmarkAdvWalkStoreAllFields(b *testing.B) {
	s := ast.NewStore(1 << 16)
	root := buildStoreTree(s, benchTreeDepth)
	var sink int
	for b.Loop() {
		sink = 0
		walkStore(root, func(h ast.Handle) {
			loc := h.Loc()
			sink += int(h.Kind()) + int(h.Flags()) + loc.Pos() + loc.End()
		})
	}
	runtime.KeepAlive(sink)
	runtime.KeepAlive(s)
}

func BenchmarkAdvWalkPtrTreeAllFields(b *testing.B) {
	root := buildPtrTree(benchTreeDepth)
	var sink int
	for b.Loop() {
		sink = 0
		walkPtr(root, func(n *ptrNode) {
			sink += int(n.kind) + int(n.flags) + n.loc.Pos() + n.loc.End()
		})
	}
	runtime.KeepAlive(sink)
	runtime.KeepAlive(root)
}

func shuffledStoreRefs(s *ast.Store, root ast.Handle) []ast.NodeRef {
	var refs []ast.NodeRef
	walkStore(root, func(h ast.Handle) { refs = append(refs, h.Ref()) })
	rng := rand.New(rand.NewPCG(1, 2))
	rng.Shuffle(len(refs), func(i, j int) { refs[i], refs[j] = refs[j], refs[i] })
	return refs
}

func shuffledPtrNodes(root *ptrNode) []*ptrNode {
	var nodes []*ptrNode
	walkPtr(root, func(n *ptrNode) { nodes = append(nodes, n) })
	rng := rand.New(rand.NewPCG(1, 2))
	rng.Shuffle(len(nodes), func(i, j int) { nodes[i], nodes[j] = nodes[j], nodes[i] })
	return nodes
}

func BenchmarkAdvRandomKindStore(b *testing.B) {
	s := ast.NewStore(1 << 16)
	root := buildStoreTree(s, benchTreeDepth)
	refs := shuffledStoreRefs(s, root)
	var sink int
	for b.Loop() {
		sink = 0
		for _, r := range refs {
			sink += int(s.At(r).Kind())
		}
	}
	runtime.KeepAlive(sink)
	runtime.KeepAlive(s)
}

func BenchmarkAdvRandomKindPtrTree(b *testing.B) {
	root := buildPtrTree(benchTreeDepth)
	nodes := shuffledPtrNodes(root)
	var sink int
	for b.Loop() {
		sink = 0
		for _, n := range nodes {
			sink += int(n.kind)
		}
	}
	runtime.KeepAlive(sink)
	runtime.KeepAlive(root)
}

func BenchmarkAdvRandomAllFieldsStore(b *testing.B) {
	s := ast.NewStore(1 << 16)
	root := buildStoreTree(s, benchTreeDepth)
	refs := shuffledStoreRefs(s, root)
	var sink int
	for b.Loop() {
		sink = 0
		for _, r := range refs {
			h := s.At(r)
			loc := h.Loc()
			sink += int(h.Kind()) + int(h.Flags()) + loc.Pos() + loc.End()
		}
	}
	runtime.KeepAlive(sink)
	runtime.KeepAlive(s)
}

func BenchmarkAdvRandomAllFieldsPtrTree(b *testing.B) {
	root := buildPtrTree(benchTreeDepth)
	nodes := shuffledPtrNodes(root)
	var sink int
	for b.Loop() {
		sink = 0
		for _, n := range nodes {
			sink += int(n.kind) + int(n.flags) + n.loc.Pos() + n.loc.End()
		}
	}
	runtime.KeepAlive(sink)
	runtime.KeepAlive(root)
}
