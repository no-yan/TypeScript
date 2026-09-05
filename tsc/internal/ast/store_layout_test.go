package ast

import (
	"testing"
	"unsafe"

	"github.com/microsoft/TypeScript/tsc/internal/core"
)

func TestNodeHeaderIs24Bytes(t *testing.T) {
	if got := unsafe.Sizeof(nodeHeader{}); got != 24 {
		t.Fatalf("nodeHeader is %d bytes, want 24", got)
	}
}

func TestIdentDoesNotAliasChildSlots(t *testing.T) {
	s := NewStore(4)
	// A slotted node whose childStart is a nonzero children index must still
	// read as text-less.
	id := s.Intern("x")
	ident := s.Alloc(KindIdentifier, 0, core.NewTextRange(0, 1), 0)
	ident.SetIdent(id)
	bin := s.AllocSlots(KindBinaryExpression, 0, core.NewTextRange(0, 3), slotBinaryExpressionCount, 0)
	if got := bin.Ident(); got != "" {
		t.Fatalf("slotted node Ident = %q, want empty", got)
	}
	if got := ident.Ident(); got != "x" {
		t.Fatalf("identifier Ident = %q, want x", got)
	}
	if got := s.TextAt(bin.Ref()); got != "" {
		t.Fatalf("slotted node TextAt = %q, want empty", got)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("SetIdent on a slotted node should panic")
		}
	}()
	bin.SetIdent(id)
}

func TestListSlotsFollowChildSlots(t *testing.T) {
	s := NewStore(8)
	cls := s.AllocSlots(KindClassDeclaration, 0, core.NewTextRange(0, 10), slotClassDeclarationCount, listSlotClassDeclarationCount)
	name := s.Alloc(KindIdentifier, 0, core.NewTextRange(6, 7), 0)
	members := s.AllocList(core.NewTextRange(8, 10), 2)
	cls.SetChild(slotClassDeclarationName, name)
	cls.SetListSlot(listSlotClassDeclarationMembers, members)
	if got := cls.Child(slotClassDeclarationName); got != name {
		t.Fatalf("child slot mismatch")
	}
	if got := cls.ListSlot(listSlotClassDeclarationMembers); got != members {
		t.Fatalf("list slot mismatch: %d want %d", got, members)
	}
	if got := s.ListSlotAt(cls.Ref(), listSlotClassDeclarationMembers); got != members {
		t.Fatalf("ListSlotAt mismatch: %d want %d", got, members)
	}
	if got := cls.NumListSlots(); got != listSlotClassDeclarationCount {
		t.Fatalf("NumListSlots = %d", got)
	}
}

func TestTokenFlagsSideTable(t *testing.T) {
	s := NewStore(4)
	lit := s.Alloc(KindNumericLiteral, 0, core.NewTextRange(0, 1), 0)
	if lit.TokenFlags() != 0 {
		t.Fatal("fresh node has token flags")
	}
	lit.SetTokenFlags(TokenFlagsOctal)
	if lit.TokenFlags() != TokenFlagsOctal {
		t.Fatal("token flags not stored")
	}
	lit.SetTokenFlags(0)
	if lit.TokenFlags() != 0 || len(s.tokenFlags) != 0 {
		t.Fatal("zero token flags should drop the entry")
	}
	lit.SetTokenFlags(TokenFlagsOctal)
	cp := s.Checkpoint()
	extra := s.Alloc(KindNumericLiteral, 0, core.NewTextRange(1, 2), 0)
	extra.SetTokenFlags(TokenFlagsHexSpecifier)
	s.Restore(cp)
	if _, ok := s.tokenFlags[extra.Ref()]; ok {
		t.Fatal("Restore kept token flags past the checkpoint")
	}
	if lit.TokenFlags() != TokenFlagsOctal {
		t.Fatal("Restore dropped token flags before the checkpoint")
	}
}

func TestSymbolIndexColumn(t *testing.T) {
	s := NewStore(4)
	a := s.Alloc(KindIdentifier, 0, core.NewTextRange(0, 1), 0)
	b := s.Alloc(KindIdentifier, 0, core.NewTextRange(1, 2), 0)
	if a.Symbol() != nil {
		t.Fatal("fresh node has a symbol")
	}
	sa, sb, sc := &Symbol{Name: "a"}, &Symbol{Name: "b"}, &Symbol{Name: "c"}
	a.SetSymbol(sa)
	b.SetSymbol(sb)
	if a.Symbol() != sa || b.Symbol() != sb {
		t.Fatal("symbol round trip failed")
	}
	// Overwrite reuses the slot instead of appending.
	a.SetSymbol(sc)
	if a.Symbol() != sc || len(s.symbolRefs) != 3 {
		t.Fatalf("overwrite appended: refs=%d", len(s.symbolRefs))
	}
	a.SetSymbol(nil)
	if a.Symbol() != nil || b.Symbol() != sb {
		t.Fatal("clearing a symbol disturbed another node")
	}
	a.SetSymbol(sa)
	if a.Symbol() != sa || len(s.symbolRefs) != 3 {
		t.Fatalf("re-set after clear should reuse the slot: refs=%d", len(s.symbolRefs))
	}
	s.PrepareBindTables()
	if b.Symbol() != sb {
		t.Fatal("PrepareBindTables dropped symbols")
	}
}
