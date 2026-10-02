package store_test

import (
	"fmt"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/storetest"
)

// TestNewSyntheticImport: the synthetic import of a file is the Pointer
// fileloader's createSyntheticImport, in a Store of the program that every
// region may take and none may write (invariant 26), and the file's Store is
// as it was.
func TestNewSyntheticImport(t *testing.T) {
	p := newProgram(t, "import { a } from \"./a\";\nexport const b = a;\n", "export const a = 1;\n")
	file := p.files[0]
	describe := func(f *store.File) []string {
		var out []string
		for _, n := range storetest.Preorder(f.Root()) {
			out = append(out, fmt.Sprintf("%v %d-%d %v %v", n.Kind(), n.Pos(), n.End(), n.Flags(), n.Parent().Key()))
		}
		return out
	}
	before := describe(file)

	literal := store.NewSyntheticImport(file, "tslib")
	decl := literal.Parent()
	for _, n := range []store.Node{literal, decl} {
		if n.Pos() != -1 || n.End() != -1 || n.Flags() != 0 {
			t.Errorf("%v at %d-%d, flags %v; want -1, -1 and none", n.Kind(), n.Pos(), n.End(), n.Flags())
		}
		if n.File() != file {
			t.Errorf("%v: File is not the file", n.Kind())
		}
	}
	if literal.Kind() != ast.KindStringLiteral || literal.Text() != "tslib" || literal.AsStringLiteral().TokenFlags() != ast.TokenFlagsNone {
		t.Errorf("literal: %v %q", literal.Kind(), literal.Text())
	}
	if decl.Kind() != ast.KindImportDeclaration || !store.SameNode(decl.ModuleSpecifier(), literal) ||
		!decl.ImportClause().IsNil() || !decl.AsImportDeclaration().Attributes().IsNil() || !decl.ModifierNodes().IsNil() {
		t.Errorf("declaration: %v with members other than the specifier", decl.Kind())
	}
	if !store.SameNode(decl.Parent(), file.Root()) {
		t.Error("the declaration's parent is not the root")
	}
	statements := file.Root().Statements()
	for i := range statements.Len() {
		if store.SameNode(statements.At(i), decl) {
			t.Error("the declaration is a statement of the root")
		}
	}
	if after := describe(file); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Error("the file's nodes changed")
	}

	// Every node of the program has a key of its own: the file's, another
	// call's for the same file, and another file's.
	again := store.NewSyntheticImport(file, "tslib")
	other := store.NewSyntheticImport(p.files[1], "react/jsx-runtime")
	if store.SameNode(again, literal) {
		t.Error("a second call gives the same node")
	}
	keys := map[store.NodeKey]bool{}
	nodes := append(storetest.Preorder(file.Root()), storetest.Preorder(p.files[1].Root())...)
	nodes = append(nodes, literal, decl, again, again.Parent(), other, other.Parent())
	for _, n := range nodes {
		if keys[n.Key()] {
			t.Fatalf("%v: a key twice", n.Kind())
		}
		keys[n.Key()] = true
	}

	// A region of the program writes none of them, and takes them.
	f := store.NewFactory(store.NewNodeRegion(p.space), 1, store.FactoryHooks{})
	mustPanic(t, "SetParent of the literal", func() { f.SetParent(literal, file.Root()) })
	mustPanic(t, "SetRange of the declaration", func() { f.SetRange(decl, 0, 0) })
	mustPanic(t, "AddFlags of the literal", func() { f.AddFlags(literal, ast.NodeFlagsSynthesized) })
	mustNotPanic(t, "a child", func() {
		if paren := f.NewParenthesizedExpression(literal); !store.SameNode(paren.Expression(), literal) {
			t.Error("the child is not the literal")
		}
	})
	mustNotPanic(t, "a parent", func() {
		id := f.NewIdentifier("x")
		f.SetParent(id, decl)
		if !store.SameNode(id.Parent(), decl) {
			t.Error("the parent is not the declaration")
		}
	})
	// Another program's region does not.
	g := store.NewFactory(store.NewNodeRegion(store.NewIDSpace(2)), 4, store.FactoryHooks{})
	mustPanic(t, "a child in another program", func() { g.NewParenthesizedExpression(literal) })
}
