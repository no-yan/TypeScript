package ast

import (
	"fmt"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
)

func TestTryBindListSpanBoundaries(t *testing.T) {
	loc := core.NewTextRange(0, 1)
	s := NewStore(1)
	defer UnregisterStore(s)
	other := NewStore(1)
	defer UnregisterStore(other)

	foreign := other.AllocList(loc, 3)
	if _, ok := s.TryBindListSpan(foreign); ok {
		t.Fatal("foreign list accepted")
	}
	if _, ok := s.TryBindListSpan(0); ok {
		t.Fatal("missing accepted")
	}
	var absent *Store
	if _, ok := absent.TryBindListSpan(foreign); ok {
		t.Fatal("nil receiver accepted")
	}
	if s.ListLen(foreign) != 3 {
		t.Fatal("original foreign fallback changed")
	}

	first := s.Alloc(KindIdentifier, 0, loc, 0)
	second := s.Alloc(KindIdentifier, 0, loc, 0)
	type outcome struct {
		ref       NodeRef
		panicText string
	}
	call := func(fn func() NodeRef) (r outcome) {
		defer func() {
			if p := recover(); p != nil {
				r.panicText = fmt.Sprint(p)
			}
		}()
		r.ref = fn()
		return
	}
	for _, n := range []int{0, 1, 7, 65} {
		list := s.AllocList(loc, n)
		if n > 0 {
			s.SetListAt(list, 0, first)
		}
		for _, ref := range []ListRef{list, ListRef(uint32(list))} {
			span, ok := s.TryBindListSpan(ref)
			if !ok || span.Len() != s.ListLen(ref) {
				t.Fatal("local length mismatch")
			}
			check := func() {
				for i := -1; i <= n; i++ {
					a := call(func() NodeRef { return s.ListElem(ref, i) })
					b := call(func() NodeRef { return s.BindListSpanElem(span, i) })
					if a != b {
						t.Fatalf("n=%d i=%d: old=%+v span=%+v", n, i, a, b)
					}
				}
			}
			check()
			s.AllocList(loc, 4096)
			check()
			if n > 0 {
				s.SetListAt(list, 0, second)
				s.SetFlagsAt(second.Ref(), NodeFlagsAmbient)
				check()
			}
		}
	}
}
