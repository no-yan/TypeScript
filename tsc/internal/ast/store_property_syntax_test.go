package ast

import (
	"os"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// Keep a call between reads, as recursive binding prevents unrestricted load
// sharing across child processing. This is an access experiment, not a model
// of the cost or semantics of the complete recursive binder.
//
//go:noinline
func propertySyntaxVisit(s *Store, ref NodeRef) uint64 {
	return uint64(s.KindAt(ref))
}

//go:noinline
func propertySyntaxBaseline(s *Store, ref NodeRef) uint64 {
	a := propertySyntaxVisit(s, s.ChildRef(ref, 0))
	b := propertySyntaxVisit(s, s.ChildRef(ref, 1))
	c := propertySyntaxVisit(s, s.ChildRef(ref, 2))
	return (a*257+b)*257 + c
}

//go:noinline
func propertySyntaxScalars(s *Store, ref NodeRef) uint64 {
	x, y, z := s.PropertyAccessExpressionRefs(ref)
	a := propertySyntaxVisit(s, x)
	b := propertySyntaxVisit(s, y)
	c := propertySyntaxVisit(s, z)
	return (a*257+b)*257 + c
}

func buildPropertySyntax(n int) (*Store, []NodeRef) {
	s := NewStore(n * 4)
	refs := make([]NodeRef, n)
	loc := core.NewTextRange(0, 1)
	for i := range refs {
		expr := s.Alloc(KindIdentifier, 0, loc, 0)
		token := s.Alloc(KindQuestionDotToken, 0, loc, 0)
		name := s.Alloc(KindIdentifier, 0, loc, 0)
		parent := s.Alloc(KindPropertyAccessExpression, 0, loc, 3)
		parent.SetChild(0, expr)
		parent.SetChild(2, name)
		if i%2 == 0 {
			parent.SetChild(1, token)
		}
		refs[i] = parent.Ref()
	}
	return s, refs
}

var propertySyntaxSink uint64

// PROPERTY_SYNTAX_VARIANT changes only the measured reader, retaining identical
// benchmark names for benchstat old.txt new.txt. One op reads one parent.
func BenchmarkPropertySyntaxAccess(b *testing.B) {
	read := propertySyntaxBaseline
	switch os.Getenv("PROPERTY_SYNTAX_VARIANT") {
	case "scalars":
		read = propertySyntaxScalars
	case "", "baseline":
	default:
		b.Fatal("unknown PROPERTY_SYNTAX_VARIANT")
	}
	for _, size := range []struct {
		name string
		n    int
	}{{"small", 64}, {"large", 65536}} {
		b.Run(size.name, func(b *testing.B) {
			s, refs := buildPropertySyntax(size.n)
			var sum uint64
			i := 0
			b.ReportAllocs()
			for b.Loop() {
				sum += read(s, refs[i&(len(refs)-1)])
				i++
			}
			propertySyntaxSink = sum
		})
	}
}

func TestPropertySyntax(t *testing.T) {
	s, refs := buildPropertySyntax(32)
	for _, ref := range refs {
		x, y, z := s.PropertyAccessExpressionRefs(ref)
		if [3]NodeRef{x, y, z} != [3]NodeRef{s.ChildRef(ref, 0), s.ChildRef(ref, 1), s.ChildRef(ref, 2)} || propertySyntaxBaseline(s, ref) != propertySyntaxScalars(s, ref) {
			t.Fatal("snapshot differs")
		}
	}
	for _, store := range []*Store{nil, s} {
		x, y, z := store.PropertyAccessExpressionRefs(0)
		if x != 0 || y != 0 || z != 0 {
			t.Fatal("missing parent differs")
		}
	}
	x, y, z := s.PropertyAccessExpressionRefs(refs[0])
	for range 1024 {
		s.Alloc(KindPropertyAccessExpression, 0, core.NewTextRange(0, 1), 3)
	}
	a, b, c := s.PropertyAccessExpressionRefs(refs[0])
	if [3]NodeRef{x, y, z} != [3]NodeRef{a, b, c} {
		t.Fatal("growth changed snapshot")
	}
	t.Run("invalid schema", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		n := s.Alloc(KindPropertyAccessExpression, 0, core.NewTextRange(0, 1), 2)
		s.PropertyAccessExpressionRefs(n.Ref())
	})
}
