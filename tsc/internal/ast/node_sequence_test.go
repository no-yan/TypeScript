package ast_test

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"gotest.tools/v3/assert"
)

func TestNodeSeqEmptyNil(t *testing.T) {
	t.Parallel()
	var nilSeq ast.NodeSeq
	assert.Equal(t, 0, nilSeq.Len())
	assert.Assert(t, nilSeq.First().IsNil())
	assert.Assert(t, nilSeq.Last().IsNil())
	assert.Assert(t, nilSeq.At(0).IsNil())
	assert.Assert(t, nilSeq.Slice() == nil)
	assert.Assert(t, nilSeq.Every(func(ast.Handle) bool { return false }))
	assert.Assert(t, !nilSeq.Some(func(ast.Handle) bool { return true }))

	n := 0
	for range ast.EmptyNodeSeq {
		n++
	}
	assert.Equal(t, 0, n)

	assert.Equal(t, 0, ast.NewStore(2).ListSlice(0).Len())
	for range ast.NewStore(2).ListSlice(0) {
		t.Fatal("nil list must not yield")
	}
	assert.Equal(t, 0, ast.DeclarationNodes(nil).Len())
	for range ast.DeclarationNodes(nil) {
		t.Fatal("nil symbol must not yield")
	}
	assert.Equal(t, 0, ast.DeclarationNodes(&ast.Symbol{}).Len())
}

func TestNodeSeqEarlyBreak(t *testing.T) {
	t.Parallel()
	f := ast.NewFactory(ast.FactoryHooks{})
	a := f.NewIdentifier("a")
	b := f.NewIdentifier("b")
	c := f.NewIdentifier("c")
	list := f.List(core.UndefinedTextRange(), a, b, c)
	seq := f.Store().ListSlice(list)

	seen := 0
	for i, h := range seq {
		seen++
		assert.Equal(t, seen-1, i)
		if h.Ref() == b.Ref() {
			break
		}
	}
	assert.Equal(t, 2, seen)
}

func TestNodeSeqDenseIndexAndNilDeclarationSkip(t *testing.T) {
	t.Parallel()
	s := ast.NewStore(8)
	ast.RegisterStore(s)
	a := s.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
	b := s.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
	sym := &ast.Symbol{
		Name: "x",
		Declarations: []ast.GlobalRef{
			a.Global(),
			0, // nil declaration — skipped
			b.Global(),
		},
	}
	var idxs []int
	var refs []ast.NodeRef
	for i, h := range ast.DeclarationNodes(sym) {
		idxs = append(idxs, i)
		refs = append(refs, h.Ref())
	}
	assert.DeepEqual(t, []int{0, 1}, idxs)
	assert.DeepEqual(t, []ast.NodeRef{a.Ref(), b.Ref()}, refs)
	assert.Equal(t, 2, ast.DeclarationNodes(sym).Len())
	assert.Equal(t, a.Ref(), ast.DeclarationNodes(sym).First().Ref())
	assert.Equal(t, b.Ref(), ast.DeclarationNodes(sym).Last().Ref())
	assert.Equal(t, a.Ref(), ast.DeclarationNodes(sym).At(0).Ref())
	assert.Equal(t, b.Ref(), ast.DeclarationNodes(sym).At(1).Ref())
	assert.Assert(t, ast.DeclarationNodes(sym).At(2).IsNil())
}

func TestNodeSeqExternalListElement(t *testing.T) {
	t.Parallel()
	a := ast.NewStore(4)
	b := ast.NewStore(4)
	ast.RegisterStore(a)
	ast.RegisterStore(b)
	local := a.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
	external := b.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
	external.SetIdent(b.Intern("ext"))
	list := a.AllocList(core.UndefinedTextRange(), 2)
	a.SetListAt(list, 0, local)
	a.SetExternalListAt(list, 1, external.Global())

	var got []ast.Handle
	for _, h := range a.ListSlice(list) {
		got = append(got, h)
	}
	assert.Equal(t, 2, len(got))
	assert.Equal(t, local.Ref(), got[0].Ref())
	assert.Equal(t, a, got[0].Store())
	assert.Equal(t, external.Ref(), got[1].Ref())
	assert.Equal(t, b, got[1].Store())
	assert.Equal(t, "ext", got[1].Ident())
	assert.Equal(t, 0, a.ListIndexOf(list, local))
	assert.Equal(t, 1, a.ListIndexOf(list, external))
	assert.Equal(t, -1, a.ListIndexOf(list, ast.Handle{}))
}

func TestNodeSeqHelpers(t *testing.T) {
	t.Parallel()
	f := ast.NewFactory(ast.FactoryHooks{})
	a := f.NewIdentifier("a")
	b := f.NewIdentifier("b")
	list := f.List(core.UndefinedTextRange(), a, b)
	seq := f.Store().ListSlice(list)

	assert.Equal(t, 2, seq.Len())
	assert.Equal(t, 1, seq.Count(func(h ast.Handle) bool { return h.Ref() == b.Ref() }))
	assert.Assert(t, seq.Some(func(h ast.Handle) bool { return h.Ref() == a.Ref() }))
	assert.Assert(t, seq.Every(func(h ast.Handle) bool { return h.Kind == ast.KindIdentifier }))
	assert.Equal(t, b.Ref(), seq.FirstMatching(func(h ast.Handle) bool { return h.Ref() == b.Ref() }).Ref())
	assert.Equal(t, b.Ref(), seq.LastMatching(func(h ast.Handle) bool { return h.Kind == ast.KindIdentifier }).Ref())

	var values []ast.NodeRef
	for h := range seq.Values() {
		values = append(values, h.Ref())
	}
	assert.DeepEqual(t, []ast.NodeRef{a.Ref(), b.Ref()}, values)

	sliced := seq.Slice()
	assert.Equal(t, 2, len(sliced))
	assert.Equal(t, a.Ref(), sliced[0].Ref())

	// Compat poly slice still materializes via Seq.Slice.
	arr := f.NewArrayLiteralExpression(list, false)
	assert.Equal(t, 2, len(arr.Elements()))
	assert.Equal(t, 2, arr.ElementsSeq().Len())
}
