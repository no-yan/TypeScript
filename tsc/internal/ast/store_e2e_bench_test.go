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

func countAstNodes(n *ast.Node) int {
	if n == nil {
		return 0
	}
	total := 1
	n.ForEachChild(func(c *ast.Node) bool {
		total += countAstNodes(c)
		return false
	})
	return total
}

func TestFlattenMatchesWalkCount(t *testing.T) {
	t.Parallel()
	sf := parseBenchFixture(t, "Herebyfile.mjs")
	root := sf.AsNode()
	want := countAstNodes(root)

	s := ast.NewStore(want)
	h := ast.FlattenNode(s, root)
	s.Seal()

	var got int
	walkStore(h, func(ast.Handle) { got++ })
	assert.Equal(t, want, got)
	assert.Equal(t, want, s.Len())
}

func BenchmarkE2EWalkFactory(b *testing.B) {
	sf := parseBenchFixture(b, "checker.ts")
	root := sf.AsNode()
	var sink int
	for b.Loop() {
		sink = 0
		walkAst(root, func(n *ast.Node) {
			sink += int(n.Kind) + int(n.Flags) + n.Pos() + n.End()
		})
	}
	runtime.KeepAlive(sink)
	runtime.KeepAlive(sf)
}

func BenchmarkE2EWalkStore(b *testing.B) {
	sf := parseBenchFixture(b, "checker.ts")
	s := ast.NewStore(countAstNodes(sf.AsNode()))
	root := ast.FlattenNode(s, sf.AsNode())
	s.Seal()
	// Drop the pointer AST so the walk measures Store alone.
	sf = nil
	runtime.GC()

	var sink int
	for b.Loop() {
		sink = 0
		walkStore(root, func(h ast.Handle) {
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
	s := ast.NewStore(countAstNodes(sf.AsNode()))
	root := ast.FlattenNode(s, sf.AsNode())
	s.Seal()
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
	s := ast.NewStore(countAstNodes(sf.AsNode()))
	root := ast.FlattenNode(s, sf.AsNode())
	s.Seal()
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
