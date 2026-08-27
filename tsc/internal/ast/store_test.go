package ast_test

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"gotest.tools/v3/assert"
)

func TestStoreAllocRefIsNonZero(t *testing.T) {
	t.Parallel()
	s := ast.NewStore(8)
	h := s.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
	assert.Assert(t, h.Ref() != 0)
	assert.Equal(t, ast.KindIdentifier, h.Kind())
}

func TestStoreZeroRefIsMissing(t *testing.T) {
	t.Parallel()
	var h ast.Handle
	assert.Equal(t, ast.NodeRef(0), h.Ref())
	assert.Equal(t, 0, h.NumChildren())
	assert.Equal(t, ast.NodeRef(0), h.Parent().Ref())
}

func TestStoreChildrenAndParent(t *testing.T) {
	t.Parallel()
	s := ast.NewStore(8)
	loc := core.NewTextRange(1, 4)
	left := s.Alloc(ast.KindIdentifier, ast.NodeFlagsConst, loc, 0)
	right := s.Alloc(ast.KindIdentifier, 0, loc, 0)
	bin := s.Alloc(ast.KindBinaryExpression, 0, loc, 2)
	bin.SetChild(0, left)
	bin.SetChild(1, right)
	left.SetParent(bin)
	right.SetParent(bin)

	assert.Equal(t, 2, bin.NumChildren())
	assert.Equal(t, left.Ref(), bin.Child(0).Ref())
	assert.Equal(t, right.Ref(), bin.Child(1).Ref())
	assert.Equal(t, bin.Ref(), left.Parent().Ref())
	assert.Equal(t, bin.Ref(), right.Parent().Ref())
	assert.Equal(t, ast.NodeFlagsConst, left.Flags())
	assert.Equal(t, 1, bin.Loc().Pos())
	assert.Equal(t, 4, bin.Loc().End())
}

func TestStoreInternDedups(t *testing.T) {
	t.Parallel()
	s := ast.NewStore(4)
	a := s.Intern("foo")
	b := s.Intern("bar")
	c := s.Intern("foo")
	assert.Assert(t, a != 0)
	assert.Assert(t, a != b)
	assert.Equal(t, a, c)

	h := s.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
	h.SetIdent(a)
	assert.Equal(t, "foo", h.Ident())
	assert.Equal(t, uint32(0), s.Intern(""))
	assert.Equal(t, "", s.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0).Ident())
}

func TestStoreAtRebuildsHandle(t *testing.T) {
	t.Parallel()
	s := ast.NewStore(4)
	h := s.Alloc(ast.KindIdentifier, 0, core.NewTextRange(2, 5), 0)
	ref := h.Ref()
	got := s.At(ref)
	assert.Equal(t, ast.KindIdentifier, got.Kind())
	assert.Equal(t, 2, got.Loc().Pos())
	assert.Equal(t, ast.NodeRef(0), s.At(0).Ref())
	assert.Equal(t, 0, s.At(0).NumChildren())
}

func TestStoreSealDropsInternIndex(t *testing.T) {
	t.Parallel()
	s := ast.NewStore(4)
	id := s.Intern("foo")
	h := s.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
	h.SetIdent(id)
	s.Seal()
	assert.Equal(t, "foo", h.Ident())
	bar := s.Intern("bar")
	assert.Assert(t, bar != id)
	again := s.Intern("foo")
	assert.Assert(t, again != id)
	lazy := s.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
	lazy.SetIdent(bar)
	assert.Equal(t, "bar", lazy.Ident())
}

func TestStoreGrowPastHint(t *testing.T) {
	t.Parallel()
	s := ast.NewStore(2)
	var last ast.Handle
	for range 256 {
		last = s.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
	}
	assert.Equal(t, ast.NodeRef(256), last.Ref())
	assert.Equal(t, ast.KindIdentifier, last.Kind())
}

func TestStoreWalkBinaryTree(t *testing.T) {
	t.Parallel()
	s := ast.NewStore(16)
	root := buildStoreTree(s, 3)
	var n int
	walkStore(root, func(ast.Handle) { n++ })
	assert.Equal(t, 7, n)
}

func TestStoreCrossStoreChildPanics(t *testing.T) {
	t.Parallel()
	a := ast.NewStore(2)
	b := ast.NewStore(2)
	parent := a.Alloc(ast.KindBinaryExpression, 0, core.UndefinedTextRange(), 1)
	child := b.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	parent.SetChild(0, child)
}

func buildStoreTree(s *ast.Store, depth int) ast.Handle {
	if depth <= 1 {
		h := s.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
		h.SetIdent(s.Intern("x"))
		return h
	}
	left := buildStoreTree(s, depth-1)
	right := buildStoreTree(s, depth-1)
	h := s.Alloc(ast.KindBinaryExpression, 0, core.UndefinedTextRange(), 2)
	h.SetChild(0, left)
	h.SetChild(1, right)
	left.SetParent(h)
	right.SetParent(h)
	return h
}

func walkStore(h ast.Handle, visit func(ast.Handle)) {
	if h.Ref() == 0 {
		return
	}
	visit(h)
	for i := range h.NumChildren() {
		walkStore(h.Child(i), visit)
	}
}
