package ast

import (
	"os"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// Experimental, test-only snapshot of the three schema-known IfStatement
// children. No borrowed slice or node pointer survives resolution. It preserves
// ChildRef's zero representation for missing/foreign children; it does not
// resolve foreign handles. Callers must supply an IfStatement from this Store.
func resolveIfSyntax(s *Store, ref NodeRef) [3]NodeRef {
	if s == nil || ref == NoNodeRef {
		return [3]NodeRef{}
	}
	n := &s.nodes[ref]
	if n.childLen != 3 {
		panic("ast: expected three IfStatement child slots")
	}
	start := int(n.childStart)
	return [3]NodeRef(s.children[start : start+3])
}

// Same resolution as the array snapshot, with scalar results to avoid the
// array return/copy ABI. The same Store/schema/stable-syntax contract applies.
func resolveIfSyntaxScalars(s *Store, ref NodeRef) (NodeRef, NodeRef, NodeRef) {
	if s == nil || ref == NoNodeRef {
		return 0, 0, 0
	}
	n := &s.nodes[ref]
	if n.childLen != 3 {
		panic("ast: expected three IfStatement child slots")
	}
	start := int(n.childStart)
	c := s.children[start : start+3]
	return c[0], c[1], c[2]
}

// Keep a call between reads, as recursive binding prevents unrestricted load
// sharing across child processing. This is an access experiment, not a model
// of the cost or semantics of the complete recursive binder.
//
//go:noinline
func parentSyntaxVisit(s *Store, ref NodeRef) uint64 {
	return uint64(s.KindAt(ref))
}

//go:noinline
func parentSyntaxBaseline(s *Store, ref NodeRef) uint64 {
	a := parentSyntaxVisit(s, s.ChildRef(ref, 0))
	b := parentSyntaxVisit(s, s.ChildRef(ref, 1))
	c := parentSyntaxVisit(s, s.ChildRef(ref, 2))
	return (a*257+b)*257 + c
}

//go:noinline
func parentSyntaxSnapshot(s *Store, ref NodeRef) uint64 {
	c := resolveIfSyntax(s, ref)
	a := parentSyntaxVisit(s, c[0])
	b := parentSyntaxVisit(s, c[1])
	d := parentSyntaxVisit(s, c[2])
	return (a*257+b)*257 + d
}

//go:noinline
func parentSyntaxScalars(s *Store, ref NodeRef) uint64 {
	x, y, z := resolveIfSyntaxScalars(s, ref)
	a := parentSyntaxVisit(s, x)
	b := parentSyntaxVisit(s, y)
	c := parentSyntaxVisit(s, z)
	return (a*257+b)*257 + c
}

func buildParentSyntax(n int) (*Store, []NodeRef) {
	s := NewStore(n * 4)
	refs := make([]NodeRef, n)
	loc := core.NewTextRange(0, 1)
	for i := range refs {
		expr := s.Alloc(KindIdentifier, 0, loc, 0)
		then := s.Alloc(KindEmptyStatement, 0, loc, 0)
		other := s.Alloc(KindBlock, 0, loc, 0)
		parent := s.Alloc(KindIfStatement, 0, loc, 3)
		parent.SetChild(0, expr)
		parent.SetChild(1, then)
		if i%2 == 0 {
			parent.SetChild(2, other)
		}
		refs[i] = parent.Ref()
	}
	return s, refs
}

var parentSyntaxSink uint64

// PARENT_SYNTAX_VARIANT changes only the measured reader, retaining identical
// benchmark names for benchstat old.txt new.txt. One op reads one parent.
func BenchmarkParentSyntaxIf(b *testing.B) {
	read := parentSyntaxBaseline
	switch os.Getenv("PARENT_SYNTAX_VARIANT") {
	case "snapshot":
		read = parentSyntaxSnapshot
	case "scalars":
		read = parentSyntaxScalars
	case "", "baseline":
	default:
		b.Fatal("unknown PARENT_SYNTAX_VARIANT")
	}
	for _, size := range []struct {
		name string
		n    int
	}{{"small", 64}, {"large", 65536}} {
		b.Run(size.name, func(b *testing.B) {
			s, refs := buildParentSyntax(size.n)
			var sum uint64
			i := 0
			b.ReportAllocs()
			for b.Loop() {
				sum += read(s, refs[i&(len(refs)-1)])
				i++
			}
			parentSyntaxSink = sum
		})
	}
}

func TestParentSyntaxIf(t *testing.T) {
	s, refs := buildParentSyntax(32)
	for _, ref := range refs {
		got := resolveIfSyntax(s, ref)
		for slot := range got {
			if got[slot] != s.ChildRef(ref, uint32(slot)) {
				t.Fatalf("ref %d slot %d differs", ref, slot)
			}
		}
		if parentSyntaxBaseline(s, ref) != parentSyntaxSnapshot(s, ref) {
			t.Fatal("ordered child checksum differs")
		}
		x, y, z := resolveIfSyntaxScalars(s, ref)
		if got != [3]NodeRef{x, y, z} || parentSyntaxBaseline(s, ref) != parentSyntaxScalars(s, ref) {
			t.Fatal("scalar child snapshot differs")
		}
	}
	if resolveIfSyntax(nil, 0) != [3]NodeRef{} || resolveIfSyntax(s, 0) != [3]NodeRef{} {
		t.Fatal("missing parent differs")
	}
	// A value snapshot survives reallocation. Structural edits are deliberately
	// not reflected, so production use would require stable syntax during bind.
	before := resolveIfSyntax(s, refs[0])
	for range 1024 {
		s.Alloc(KindIfStatement, 0, core.NewTextRange(0, 1), 3)
	}
	if before != resolveIfSyntax(s, refs[0]) {
		t.Fatal("snapshot changed after Store growth")
	}
}
