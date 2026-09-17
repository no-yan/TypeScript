package checker

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/osvfs"
)

// boundBenchFixture parses and binds one of the shared bench fixtures so the
// tree has the same parent links a real checker sees.
func boundBenchFixture(tb testing.TB, name string) *ast.SourceFile {
	tb.Helper()
	for _, fx := range fixtures.BenchFixtures {
		if fx.Name() != name {
			continue
		}
		fx.SkipIfNotExist(tb)
		fileName := tspath.GetNormalizedAbsolutePath(fx.Path(), "/")
		path := tspath.ToPath(fileName, "/", osvfs.FS().UseCaseSensitiveFileNames())
		sf := parser.ParseSourceFile(ast.SourceFileParseOptions{
			FileName: fileName,
			Path:     path,
		}, fx.ReadFile(tb), core.GetScriptKindFromFileName(fileName))
		binder.BindSourceFile(sf)
		return sf
	}
	tb.Fatalf("fixture %q not found", name)
	return nil
}

func collectIdentifiers(root ast.Handle) []ast.Handle {
	var out []ast.Handle
	ast.Walk(root, func(n ast.Handle) bool {
		if n.Kind == ast.KindIdentifier {
			out = append(out, n)
		}
		return false
	})
	return out
}

var exportExpressionSink int

// BenchmarkIsExportOrExportExpression calls isExportOrExportExpression on
// every identifier of checker.ts. Real callers only reach it for alias
// references, but the ancestor walk itself does not depend on that, so every
// identifier is a fair proxy for the per-call cost.
func BenchmarkIsExportOrExportExpression(b *testing.B) {
	sf := boundBenchFixture(b, "checker.ts")
	ids := collectIdentifiers(sf.ParseRoot())
	if len(ids) == 0 {
		b.Fatal("no identifiers")
	}
	// Ancestor visits per call: FindAncestor walks to the root for almost
	// every identifier, so the chain length bounds the per-call work.
	visits := 0
	for _, id := range ids {
		for n := id; !n.IsNil(); n = n.Parent() {
			visits++
		}
	}
	b.ResetTimer()
	for b.Loop() {
		hits := 0
		for _, id := range ids {
			if isExportOrExportExpression(id) {
				hits++
			}
		}
		exportExpressionSink = hits
	}
	b.ReportMetric(float64(len(ids)), "idents/op")
	b.ReportMetric(float64(visits), "visits/op")
}
