package compiler_test

import (
	"os"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/osvfs"
)

// STORE_BENCH_PROJECT selects a real workload. One operation includes config
// loading, parse, bind, check, and close, with fresh Program state. This is not
// CLI process startup time. Use -benchtime=1x and save raw output for benchstat.
func BenchmarkStoreMonaco(b *testing.B) {
	project := os.Getenv("STORE_BENCH_PROJECT")
	if project == "" {
		b.Skip("set STORE_BENCH_PROJECT to a tsconfig.json")
	}
	for _, checkers := range []int{1, 4} {
		name := "c1"
		if checkers == 4 {
			name = "c4"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				host := compiler.NewCompilerHost("/", bundled.WrapFS(osvfs.FS()), bundled.LibPath(), nil, nil, nil)
				config, errors := tsoptions.GetParsedCommandLineOfConfigFile(project, &core.CompilerOptions{NoEmit: core.TSTrue, Checkers: &checkers}, nil, host, nil)
				if len(errors) != 0 {
					b.Fatal("config errors", errors)
				}
				p := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host})
				diags := p.GetSemanticDiagnostics(b.Context(), nil)
				b.ReportMetric(float64(len(diags)), "diagnostics/op")
				b.ReportMetric(float64(len(p.SourceFiles())), "files/op")
				p.Close()
				// These fresh parse Stores have no surviving Program consumers. Release
				// their registry roots so repeated benchmark operations do not retain them.
				for _, file := range p.SourceFiles() {
					ast.UnregisterStore(file.ParseStore())
				}
			}
		})
	}
}
