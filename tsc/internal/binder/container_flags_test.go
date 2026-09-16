package binder

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

var containerFlagsSink ContainerFlags

func TestContainerFlagsKnownKindMatchesStoreHandle(t *testing.T) {
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/flags.ts", Path: tspath.Path("/flags.ts")}, `
class C { accessor a = 1; m() { { let x = 1 } } }
const value = { get x() { return 1 }, set x(v) {}, m() {} };
`, core.ScriptKindTS)
	store, root := file.ParseTreeRef()
	ast.Walk(store.At(root), func(node ast.Handle) bool {
		known := ast.HandleOf(store, node.Ref(), node.Kind)
		if got, want := GetContainerFlags(known), GetContainerFlags(node); got != want {
			t.Fatalf("kind=%v: known=%v store=%v", node.Kind, got, want)
		}
		return false
	})
}

func BenchmarkBinderCallContainerFlags(b *testing.B) {
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/flags.ts", Path: tspath.Path("/flags.ts")}, `
class C { accessor a = 1; m() { { let x = 1 } } }
const value = { get x() { return 1 }, set x(v) {}, m() {} };
`, core.ScriptKindTS)
	store, root := file.ParseTreeRef()
	type sample struct {
		ref  ast.NodeRef
		kind ast.Kind
	}
	var samples []sample
	ast.Walk(store.At(root), func(node ast.Handle) bool { samples = append(samples, sample{node.Ref(), node.Kind}); return false })
	for _, variant := range []struct {
		name string
		make func(sample) ast.Handle
	}{
		{"storeAt", func(s sample) ast.Handle { return store.At(s.ref) }},
		{"knownKind", func(s sample) ast.Handle { return ast.HandleOf(store, s.ref, s.kind) }},
	} {
		b.Run(variant.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				containerFlagsSink ^= GetContainerFlags(variant.make(samples[i%len(samples)]))
			}
		})
	}
}
