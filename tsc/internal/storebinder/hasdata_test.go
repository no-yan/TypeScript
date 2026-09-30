package storebinder_test

import (
	"fmt"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/storetest"
)

// The generated Has<X>Data predicates, which the P2 of 7d-2a use for a lone
// XxxData() (store-checker-7d2a-implementation-instructions-20260929.md 4.1),
// against the Pointer's XxxData() != nil on every node of the corpus, JSDoc
// included.
var hasData = []struct {
	name    string
	pointer func(*ast.Node) bool
	store   func(store.Node) bool
}{
	{"DeclarationData", func(n *ast.Node) bool { return n.DeclarationData() != nil }, store.Node.HasDeclarationData},
	{"ExportableData", func(n *ast.Node) bool { return n.ExportableData() != nil }, store.Node.HasExportableData},
	{"LocalsContainerData", func(n *ast.Node) bool { return n.LocalsContainerData() != nil }, store.Node.HasLocalsContainerData},
	{"FunctionLikeData", func(n *ast.Node) bool { return n.FunctionLikeData() != nil }, store.Node.HasFunctionLikeData},
	{"ClassLikeData", func(n *ast.Node) bool { return n.ClassLikeData() != nil }, store.Node.HasClassLikeData},
	{"BodyData", func(n *ast.Node) bool { return n.BodyData() != nil }, store.Node.HasBodyData},
	{"FlowNodeData", func(n *ast.Node) bool { return n.FlowNodeData() != nil }, store.Node.HasFlowNodeData},
	{"LiteralLikeData", func(n *ast.Node) bool { return n.LiteralLikeData() != nil }, store.Node.HasLiteralLikeData},
	{"TemplateLiteralLikeData", func(n *ast.Node) bool { return n.TemplateLiteralLikeData() != nil }, store.Node.HasTemplateLiteralLikeData},
}

func TestHasDataCorpus(t *testing.T) {
	files := corpus(t)
	nodes := 0
	trues := map[string]int{}
	kinds := map[ast.Kind]bool{}
	for i, f := range files {
		opts, text, kind := sourceInput(f.name, f.text)
		pointer, file := bindBothIn(opts, text, kind)
		pairs, _ := storetest.Pairs(pointer, file.Store)
		if pairs == nil {
			t.Fatalf("%s #%d: parse mismatch", f.name, i)
		}
		jsdoc, same := jsdocPairs(pointer, file, pairs)
		if !same {
			t.Errorf("%s #%d: JSDoc shape differs", f.name, i)
		}
		for _, pair := range append(pairs, jsdoc...) {
			nodes++
			kinds[pair.P.Kind] = true
			for _, h := range hasData {
				want := h.pointer(pair.P)
				if want {
					trues[h.name]++
				}
				if got := h.store(pair.N); got != want {
					t.Fatalf("%s #%d@%d %v: Has%s is %v, want %v", f.name, i, pair.P.Pos(), pair.P.Kind, h.name, got, want)
				}
			}
		}
	}
	var counts string
	for _, h := range hasData {
		counts += fmt.Sprintf(" %s %d", h.name, trues[h.name])
	}
	t.Logf("%d files, %d nodes of %d kinds; true:%s", len(files), nodes, len(kinds), counts)
}
