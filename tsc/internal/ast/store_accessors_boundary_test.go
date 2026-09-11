package ast

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
)

func TestAccessSchemaBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name            string
		kind            Kind
		children, lists int
		access          func(*Store, NodeRef)
	}{
		{"binding kind", KindForInStatement, 4, 0, func(s *Store, r NodeRef) { s.AccessBindingElement(r) }},
		{"binding children", KindBindingElement, 3, 0, func(s *Store, r NodeRef) { s.AccessBindingElement(r) }},
		{"binding lists", KindBindingElement, 4, 1, func(s *Store, r NodeRef) { s.AccessBindingElement(r) }},
		{"loop kind", KindBindingElement, 4, 0, func(s *Store, r NodeRef) { s.AccessForInOrOfStatement(r) }},
		{"binary projection lists", KindBinaryExpression, 4, 0, func(s *Store, r NodeRef) { s.AccessBinaryExpressionChildren(r) }},
		{"call projection children", KindCallExpression, 1, 2, func(s *Store, r NodeRef) { s.AccessCallExpressionChildren(r) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStore(8)
			defer UnregisterStore(s)
			n := s.AllocSlots(tc.kind, 0, core.UndefinedTextRange(), tc.children, tc.lists)
			defer func() {
				if recover() == nil {
					t.Fatal("expected kind/shape rejection")
				}
			}()
			tc.access(s, n.Ref())
		})
	}
}

func TestAccessLoopAliases(t *testing.T) {
	s := NewStore(8)
	defer UnregisterStore(s)
	loc := core.UndefinedTextRange()
	for _, kind := range []Kind{KindForInStatement, KindForOfStatement} {
		n := s.AllocSlots(kind, 0, loc, 4, 0)
		child := s.Alloc(KindIdentifier, 0, loc, 0)
		n.SetChild(2, child)
		if s.AccessForInOrOfStatement(n.Ref()).Expression != child.Ref() {
			t.Fatal("alias lost expression")
		}
	}
}

func TestAccessForeignChildFallback(t *testing.T) {
	s, foreign := NewStore(8), NewStore(8)
	defer UnregisterStore(s)
	defer UnregisterStore(foreign)
	loc := core.UndefinedTextRange()
	n := s.AllocSlots(KindBindingElement, 0, loc, 4, 0)
	child := foreign.Alloc(KindIdentifier, 0, loc, 0)
	n.SetChild(slotBindingElementName, child)
	if s.AccessBindingElement(n.Ref()).Name != 0 {
		t.Fatal("foreign child must not become a local ref")
	}
	if n.Child(slotBindingElementName) != child {
		t.Fatal("Handle fallback lost foreign identity")
	}
}

func TestAccessChildrenProjections(t *testing.T) {
	s, foreign := NewStore(8), NewStore(8)
	defer UnregisterStore(s)
	defer UnregisterStore(foreign)
	loc := core.UndefinedTextRange()
	list := foreign.AllocList(loc, 1)
	for _, kind := range []Kind{KindBinaryExpression, KindCallExpression} {
		children, lists := 4, 1
		if kind == KindCallExpression {
			children, lists = 2, 2
		}
		n := s.AllocSlots(kind, 0, loc, children, lists)
		for i := range children {
			n.SetChild(i, s.Alloc(KindIdentifier, 0, loc, 0))
		}
		for i := range lists {
			n.SetListSlot(i, list)
		}
		if kind == KindBinaryExpression {
			full, part := s.AccessBinaryExpression(n.Ref()), s.AccessBinaryExpressionChildren(n.Ref())
			if part.Left != full.Left || part.Type != full.Type || part.Operator != full.Operator || part.Right != full.Right || full.Modifiers != list {
				t.Fatal("binary projection differs")
			}
		} else {
			full, part := s.AccessCallExpression(n.Ref()), s.AccessCallExpressionChildren(n.Ref())
			if part.Expression != full.Expression || part.QuestionDot != full.QuestionDot || full.Arguments != list || full.TypeArguments != list {
				t.Fatal("call projection differs")
			}
		}
	}
	for _, store := range []*Store{nil, s} {
		if store.AccessBinaryExpressionChildren(0) != (BinaryExpressionChildrenAccessor{}) || store.AccessCallExpressionChildren(0) != (CallExpressionChildrenAccessor{}) {
			t.Fatal("zero projection")
		}
	}
}
