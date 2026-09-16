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
		if h.Symbol() != nil {
			sawSymbol = true
		}
		if h.LocalSymbol() != nil {
			sawLocalSymbol = true
		}
		if h.FlowNode() != nil {
			sawFlow = true
		}
		if h.EndFlowNode() != nil {
			sawEndFlow = true
		}
		if h.ReturnFlowNode() != nil {
			sawReturnFlow = true
		}
		if h.Locals() != nil {
			sawLocals = true
		}
		if !h.NextContainer().IsNil() {
			sawNextContainer = true
		}
		return false
	})
	if !sawSymbol || !sawLocalSymbol || !sawFlow || !sawEndFlow || !sawReturnFlow || !sawLocals || !sawNextContainer {
		t.Fatalf(
			"missing bound Store data: symbol=%v localSymbol=%v flow=%v endFlow=%v returnFlow=%v locals=%v nextContainer=%v",
			sawSymbol, sawLocalSymbol, sawFlow, sawEndFlow, sawReturnFlow, sawLocals, sawNextContainer,
		)
	}
}

func TestBindParameterPropertyUsesStoredModifierFact(t *testing.T) {
	t.Parallel()
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/parameter-property.ts", Path: "/parameter-property.ts"}, `
class C {
  constructor(public id: number, private readonly name: string, protected active: boolean, plain: number) {}
}
`, core.ScriptKindTS)
	BindSourceFile(file)

	class := file.ParseRoot().Statements()[0]
	members := class.Symbol().Members
	for _, name := range []string{"id", "name", "active"} {
		if members[name] == nil || members[name].Flags&ast.SymbolFlagsProperty == 0 {
			t.Errorf("constructor parameter property %q was not declared", name)
		}
	}
	if members["plain"] != nil {
		t.Error("unmodified constructor parameter was declared as a property")
	}
}
