package fixtures

import (
	"path/filepath"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/repo"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/filefixture"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/osvfs"
)

// The AST benchmark suite (internal/*/ast_benchmark_test.go) keeps its inputs
// separate from BenchFixtures so that changes to either list do not silently
// change the other suite's work.
var (
	ASTBenchCheckerFixture = filefixture.FromFile("checker.ts", filepath.Join(repo.TestDataPath(), "fixtures/compiler/checker.ts"))
	astBenchDOMFixture     = filefixture.FromFile("dom.generated.d.ts", filepath.Join(repo.TestDataPath(), "fixtures/lib/dom.generated.d.ts"))
)

var ASTBenchFixtures = []filefixture.Fixture{
	ASTBenchCheckerFixture,
	astBenchDOMFixture,
}

// ASTBenchParseInput fails on a missing fixture instead of skipping, so a
// partial run is never mistaken for a complete comparison.
func ASTBenchParseInput(tb testing.TB, fixture filefixture.Fixture) (ast.SourceFileParseOptions, string, core.ScriptKind) {
	tb.Helper()
	fileName := tspath.GetNormalizedAbsolutePath(fixture.Path(), "/")
	opts := ast.SourceFileParseOptions{
		FileName: fileName,
		Path:     tspath.ToPath(fileName, "/", osvfs.FS().UseCaseSensitiveFileNames()),
	}
	return opts, fixture.ReadFile(tb), core.GetScriptKindFromFileName(fileName)
}
