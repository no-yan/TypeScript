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

// BenchmarkBindHot binds one freshly parsed file per iteration so the working
// set stays at a single file (no b.N retained parses, no paging noise).
func BenchmarkBindHot(b *testing.B) {
	for _, f := range fixtures.BenchFixtures {
		b.Run(f.Name(), func(b *testing.B) {
			f.SkipIfNotExist(b)
			fileName := tspath.GetNormalizedAbsolutePath(f.Path(), "/")
			path := tspath.ToPath(fileName, "/", osvfs.FS().UseCaseSensitiveFileNames())
			sourceText := f.ReadFile(b)
			parseOptions := ast.SourceFileParseOptions{FileName: fileName, Path: path}
			scriptKind := core.GetScriptKindFromFileName(fileName)
			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				sf := parser.ParseSourceFile(parseOptions, sourceText, scriptKind)
				runtime.GC()
				b.StartTimer()
				BindSourceFile(sf)
			}
		})
	}
}
