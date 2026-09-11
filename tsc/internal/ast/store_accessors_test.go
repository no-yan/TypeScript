package ast

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
)

func TestAccessParameterFields(t *testing.T) {
	s := NewStore(16)
	defer UnregisterStore(s)
	loc := core.UndefinedTextRange()
	rest := s.Alloc(KindDotDotDotToken, 0, loc, 0)
	name := s.Alloc(KindIdentifier, 0, loc, 0)
	question := s.Alloc(KindQuestionToken, 0, loc, 0)
	typ := s.Alloc(KindAnyKeyword, 0, loc, 0)
	init := s.Alloc(KindNumericLiteral, 0, loc, 0)
	mods := s.AllocList(loc, 1)
	parent := s.AllocSlots(KindParameter, 0, loc, slotParameterDeclarationCount, listSlotParameterDeclarationCount)
	parent.SetChild(slotParameterDeclarationDotDotDotToken, rest)
	parent.SetChild(slotParameterDeclarationName, name)
	parent.SetChild(slotParameterDeclarationQuestionToken, question)
	parent.SetChild(slotParameterDeclarationType, typ)
	parent.SetChild(slotParameterDeclarationInitializer, init)
	parent.SetListSlot(listSlotParameterDeclarationModifiers, mods)

	a := s.AccessParameter(parent.Ref())
	if a.Rest != rest.Ref() || a.Name != name.Ref() || a.Question != question.Ref() || a.Type != typ.Ref() || a.Initializer != init.Ref() || a.Modifiers != mods {
		t.Fatalf("parameter fields: %+v", a)
	}
}

func TestAccessParameterMissingAndNil(t *testing.T) {
	s := NewStore(4)
	defer UnregisterStore(s)
	loc := core.UndefinedTextRange()
	parent := s.AllocSlots(KindParameter, 0, loc, slotParameterDeclarationCount, listSlotParameterDeclarationCount)
	a := s.AccessParameter(parent.Ref())
	if a != (ParameterAccessor{}) {
		t.Fatalf("missing children should be zero: %+v", a)
	}
	for _, store := range []*Store{nil, s} {
		if store.AccessParameter(0) != (ParameterAccessor{}) {
			t.Fatal("nil store or ref=0")
		}
	}
}

func TestAccessParameterWrongKindSameArity(t *testing.T) {
	s := NewStore(8)
	defer UnregisterStore(s)
	loc := core.UndefinedTextRange()
	// Same 5+1 slot shape as Parameter, different kind.
	n := s.AllocSlots(KindShorthandPropertyAssignment, 0, loc, slotParameterDeclarationCount, listSlotParameterDeclarationCount)
	defer func() {
		if recover() == nil {
			t.Fatal("expected wrong-kind panic")
		}
	}()
	s.AccessParameter(n.Ref())
}

func TestAccessParameterWrongShape(t *testing.T) {
	s := NewStore(4)
	defer UnregisterStore(s)
	n := s.Alloc(KindParameter, 0, core.UndefinedTextRange(), 2)
	defer func() {
		if recover() == nil {
			t.Fatal("expected shape panic")
		}
	}()
	s.AccessParameter(n.Ref())
}

func TestAccessParameterSnapshotIndependence(t *testing.T) {
	s := NewStore(16)
	defer UnregisterStore(s)
	loc := core.UndefinedTextRange()
	name := s.Alloc(KindIdentifier, 0, loc, 0)
	parent := s.AllocSlots(KindParameter, 0, loc, slotParameterDeclarationCount, listSlotParameterDeclarationCount)
	parent.SetChild(slotParameterDeclarationName, name)
	snap := s.AccessParameter(parent.Ref())
	for range 1024 {
		s.Alloc(KindIdentifier, 0, loc, 0)
	}
	afterGrowth := s.AccessParameter(parent.Ref())
	if snap != afterGrowth || snap.Name != name.Ref() {
		t.Fatal("growth changed snapshot")
	}
	replacement := s.Alloc(KindIdentifier, 0, loc, 0)
	parent.SetChild(slotParameterDeclarationName, replacement)
	if snap.Name != name.Ref() {
		t.Fatal("snapshot reflected later syntax edit")
	}
	if s.AccessParameter(parent.Ref()).Name != replacement.Ref() {
		t.Fatal("fresh access should see new child")
	}
}

func TestAccessParameterForeignList(t *testing.T) {
	owner := NewStore(8)
	defer UnregisterStore(owner)
	foreign := NewStore(8)
	defer UnregisterStore(foreign)
	loc := core.UndefinedTextRange()
	mods := foreign.AllocList(loc, 1)
	parent := owner.AllocSlots(KindParameter, 0, loc, slotParameterDeclarationCount, listSlotParameterDeclarationCount)
	parent.SetListSlot(listSlotParameterDeclarationModifiers, mods)
	a := owner.AccessParameter(parent.Ref())
	if a.Modifiers != mods {
		t.Fatalf("foreign list identity: got %d want %d", a.Modifiers, mods)
	}
	if a.Modifiers == 0 {
		t.Fatal("foreign list must not collapse to zero")
	}
}

func TestAccessBindingElement(t *testing.T) {
	s := NewStore(16)
	defer UnregisterStore(s)
	loc := core.UndefinedTextRange()
	rest := s.Alloc(KindDotDotDotToken, 0, loc, 0)
	prop := s.Alloc(KindIdentifier, 0, loc, 0)
	name := s.Alloc(KindIdentifier, 0, loc, 0)
	init := s.Alloc(KindNumericLiteral, 0, loc, 0)
	parent := s.AllocSlots(KindBindingElement, 0, loc, slotBindingElementCount, 0)
	parent.SetChild(slotBindingElementDotDotDotToken, rest)
	parent.SetChild(slotBindingElementPropertyName, prop)
	parent.SetChild(slotBindingElementName, name)
	parent.SetChild(slotBindingElementInitializer, init)

	a := s.AccessBindingElement(parent.Ref())
	if a.Rest != rest.Ref() || a.PropertyName != prop.Ref() || a.Name != name.Ref() || a.Initializer != init.Ref() {
		t.Fatalf("binding element fields: %+v", a)
	}
	for _, store := range []*Store{nil, s} {
		if store.AccessBindingElement(0) != (BindingElementAccessor{}) {
			t.Fatal("nil/zero")
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("expected wrong-kind panic")
		}
	}()
	s.AccessBindingElement(name.Ref())
}
