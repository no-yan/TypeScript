package binder

import (
	"runtime"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/osvfs"
)

func BenchmarkBind(b *testing.B) {
	for _, f := range fixtures.BenchFixtures {
		b.Run(f.Name(), func(b *testing.B) {
			f.SkipIfNotExist(b)

			fileName := tspath.GetNormalizedAbsolutePath(f.Path(), "/")
			path := tspath.ToPath(fileName, "/", osvfs.FS().UseCaseSensitiveFileNames())
			sourceText := f.ReadFile(b)

			parseOptions := ast.SourceFileParseOptions{
				FileName: fileName,
				Path:     path,
			}
			scriptKind := core.GetScriptKindFromFileName(fileName)

			sourceFiles := make([]*ast.SourceFile, b.N)
			for i := range b.N {
				sourceFiles[i] = parser.ParseSourceFile(parseOptions, sourceText, scriptKind)
			}

			// The above parses do a lot of work; ensure GC is finished before we start collecting performance data.
			// GC must be called twice to allow things to settle.
			runtime.GC()
			runtime.GC()

			b.ResetTimer()
			for i := range b.N {
				BindSourceFile(sourceFiles[i])
			}
		})
	}
}

func TestBindStoreSideMaps(t *testing.T) {
	t.Parallel()
	opts := ast.SourceFileParseOptions{FileName: "/index.ts", Path: "/index.ts"}
	file := parser.ParseSourceFile(opts, `
export class C {
    constructor(x: number) { if (x) return; }
    method() { return 1; }
}
`, core.ScriptKindTS)
	BindSourceFile(file)
	store := file.ParseStore()
	if store == nil || store.Len() == 0 {
		t.Fatal("bind requires a nonempty parse Store")
	}
	var sawSymbol, sawLocalSymbol, sawFlow, sawEndFlow, sawReturnFlow, sawLocals, sawNextContainer bool
	ast.Walk(file.ParseRoot(), func(h ast.Handle) bool {
		node := file.NodeFor(h.Ref())
		if node == nil {
			t.Fatalf("missing pointer node for Store ref %d", h.Ref())
		}
		if symbol := node.Symbol(); symbol != nil {
			sawSymbol = true
			if h.Symbol() != symbol {
				t.Fatal("Store Symbol does not match bound node")
			}
		}
		if exportable := node.ExportableData(); exportable != nil {
			if h.LocalSymbol() != exportable.LocalSymbol {
				t.Fatal("Store LocalSymbol does not match bound node")
			}
			sawLocalSymbol = sawLocalSymbol || exportable.LocalSymbol != nil
		}
		if flow := node.FlowNodeData(); flow != nil {
			if h.FlowNode() != flow.FlowNode {
				t.Fatal("Store FlowNode does not match bound node")
			}
			sawFlow = sawFlow || flow.FlowNode != nil
		}
		if body := node.BodyData(); body != nil {
			if h.EndFlowNode() != body.EndFlowNode {
				t.Fatal("Store EndFlowNode does not match bound node")
			}
			sawEndFlow = sawEndFlow || body.EndFlowNode != nil
		}
		if locals := node.LocalsContainerData(); locals != nil {
			if !sameSymbolTable(h.Locals(), locals.Locals) {
				t.Fatal("Store Locals does not match bound node")
			}
			if h.NextContainer().Ref() != file.HandleOf(locals.NextContainer).Ref() {
				t.Fatal("Store NextContainer does not match bound node")
			}
			sawLocals = sawLocals || locals.Locals != nil
			sawNextContainer = sawNextContainer || locals.NextContainer != nil
		}
		var returnFlow *ast.FlowNode
		switch node.Kind {
		case ast.KindConstructor:
			returnFlow = node.AsConstructorDeclaration().ReturnFlowNode
		case ast.KindFunctionDeclaration:
			returnFlow = node.AsFunctionDeclaration().ReturnFlowNode
		case ast.KindFunctionExpression:
			returnFlow = node.AsFunctionExpression().ReturnFlowNode
		case ast.KindClassStaticBlockDeclaration:
			returnFlow = node.AsClassStaticBlockDeclaration().ReturnFlowNode
		}
		if h.ReturnFlowNode() != returnFlow {
			t.Fatal("Store ReturnFlowNode does not match bound node")
		}
		sawReturnFlow = sawReturnFlow || returnFlow != nil
		return false
	})
	if !sawSymbol || !sawLocalSymbol || !sawFlow || !sawEndFlow || !sawReturnFlow || !sawLocals || !sawNextContainer {
		t.Fatalf(
			"missing bound Store data: symbol=%v localSymbol=%v flow=%v endFlow=%v returnFlow=%v locals=%v nextContainer=%v",
			sawSymbol, sawLocalSymbol, sawFlow, sawEndFlow, sawReturnFlow, sawLocals, sawNextContainer,
		)
	}
}

func sameSymbolTable(left, right ast.SymbolTable) bool {
	if len(left) != len(right) {
		return false
	}
	for name, symbol := range left {
		if right[name] != symbol {
			return false
		}
	}
	return true
}
