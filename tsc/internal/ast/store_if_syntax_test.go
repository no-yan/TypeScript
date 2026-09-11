package ast

import (
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"testing"
)

func TestIfStatementRefs(t *testing.T) {
	s := NewStore(1)
	defer UnregisterStore(s)
	loc := core.UndefinedTextRange()
	parent := s.Alloc(KindIfStatement, 0, loc, 3)
	cond := s.Alloc(KindIdentifier, 0, loc, 0)
	then := s.Alloc(KindBlock, 0, loc, 0)
	parent.SetChild(0, cond)
	parent.SetChild(1, then)
	a, b, c := s.IfStatementRefs(parent.Ref())
	if a != cond.Ref() || b != then.Ref() || c != 0 {
		t.Fatal("condition/then/absent else")
	}
	for range 1024 {
		s.Alloc(KindIdentifier, 0, loc, 0)
	}
	parent.SetChild(2, then)
	x, y, z := s.IfStatementRefs(parent.Ref())
	if x != a || y != b || z != b || c != 0 {
		t.Fatal("snapshot or fresh read after growth")
	}
	for _, store := range []*Store{nil, s} {
		x, y, z := store.IfStatementRefs(0)
		if x != 0 || y != 0 || z != 0 {
			t.Fatal("missing parent")
		}
	}
	t.Run("invalid shape", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected shape check")
			}
		}()
		s.IfStatementRefs(cond.Ref())
	})
}
