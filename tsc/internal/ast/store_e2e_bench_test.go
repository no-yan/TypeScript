package ast_test

import (
	"runtime"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/osvfs"
	"gotest.tools/v3/assert"
)

func parseBenchFixture(t testing.TB, name string) *ast.SourceFile {
	t.Helper()
	for _, fx := range fixtures.BenchFixtures {
		if fx.Name() != name {
			continue
		}
		fx.SkipIfNotExist(t)
		fileName := tspath.GetNormalizedAbsolutePath(fx.Path(), "/")
		path := tspath.ToPath(fileName, "/", osvfs.FS().UseCaseSensitiveFileNames())
		return parser.ParseSourceFile(ast.SourceFileParseOptions{
			FileName: fileName,
			Path:     path,
		}, fx.ReadFile(t), core.GetScriptKindFromFileName(fileName))
	}
	t.Fatalf("fixture %q not found", name)
	return nil
}

func countAstNodes(h ast.Handle) int {
	if h.IsNil() {
		return 0
	}
	total := 1
	h.ForEachChild(func(c ast.Handle) bool {
		total += countAstNodes(c)
		return false
	})
	return total
}

func walkParsed(h ast.Handle, visit func(ast.Handle)) {
	ast.Walk(h, func(n ast.Handle) bool {
		visit(n)
		return false
	})
}

func TestFlattenMatchesWalkCount(t *testing.T) {
	t.Parallel()
	opts := ast.SourceFileParseOptions{FileName: "/index.ts", Path: "/index.ts"}
	sf := parser.ParseSourceFile(opts, "const x = 1;\nfunction f(y: number) { return x + y; }\n", core.ScriptKindTS)
	root := sf.ParseRoot()
	want := countAstNodes(root)

	var got int
	walkParsed(root, func(ast.Handle) { got++ })
	assert.Equal(t, want, got)
	assert.Equal(t, want, sf.ParseStore().Len())
}

func BenchmarkE2EWalkFactory(b *testing.B) {
	sf := parseBenchFixture(b, "checker.ts")
	root := sf.ParseRoot()
	var sink int
	for b.Loop() {
		sink = 0
		walkParsed(root, func(n ast.Handle) {
			loc := n.Loc()
			sink += int(n.Kind()) + int(n.Flags()) + loc.Pos() + loc.End()
		})
	}
	runtime.KeepAlive(sink)
	runtime.KeepAlive(sf)
}

func BenchmarkE2EWalkStore(b *testing.B) {
	sf := parseBenchFixture(b, "checker.ts")
	s := sf.ParseStore()
	root := sf.ParseRoot()
	sf = nil
	runtime.GC()

	var sink int
	for b.Loop() {
		sink = 0
		walkParsed(root, func(h ast.Handle) {
			loc := h.Loc()
			sink += int(h.Kind()) + int(h.Flags()) + loc.Pos() + loc.End()
		})
	}
	runtime.KeepAlive(sink)
	runtime.KeepAlive(s)
}

func BenchmarkE2EGCBaseline(b *testing.B) {
	runtime.GC()
	b.ResetTimer()
	for b.Loop() {
		runtime.GC()
	}
}

func BenchmarkE2EGCFactory(b *testing.B) {
	sf := parseBenchFixture(b, "checker.ts")
	runtime.GC()
	b.ResetTimer()
	for b.Loop() {
		runtime.GC()
	}
	runtime.KeepAlive(sf)
}

func BenchmarkE2EGCStore(b *testing.B) {
	sf := parseBenchFixture(b, "checker.ts")
	s := sf.ParseStore()
	root := sf.ParseRoot()
	sf = nil
	runtime.GC()
	b.ResetTimer()
	for b.Loop() {
		runtime.GC()
	}
	runtime.KeepAlive(s)
	runtime.KeepAlive(root)
}

func BenchmarkE2EGCFactoryWithBallast(b *testing.B) {
	ballast := makeBallast()
	sf := parseBenchFixture(b, "checker.ts")
	runtime.GC()
	b.ResetTimer()
	for b.Loop() {
		runtime.GC()
	}
	runtime.KeepAlive(ballast)
	runtime.KeepAlive(sf)
}

func BenchmarkE2EGCStoreWithBallast(b *testing.B) {
	ballast := makeBallast()
	sf := parseBenchFixture(b, "checker.ts")
	s := sf.ParseStore()
	root := sf.ParseRoot()
	sf = nil
	runtime.GC()
	b.ResetTimer()
	for b.Loop() {
		runtime.GC()
	}
	runtime.KeepAlive(ballast)
	runtime.KeepAlive(s)
	runtime.KeepAlive(root)
}
