package compiler_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/repo"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/kperf"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/osvfs"
)

type benchmarkCompilerProject struct {
	config *tsoptions.ParsedCommandLine
	root   string
	fs     vfs.FS
}

// newASTBenchmarkJSEmitConfig deliberately clears every option which can make
// Emit visit or write declaration artifacts. Keep this helper shared by the
// benchmark and KPC path so their work is identical.
func newASTBenchmarkJSEmitConfig(config *tsoptions.ParsedCommandLine) *tsoptions.ParsedCommandLine {
	options := *config.CompilerOptions()
	options.NoEmit = core.TSFalse
	options.EmitDeclarationOnly = core.TSFalse
	options.Declaration = core.TSFalse
	options.DeclarationMap = core.TSFalse
	options.SourceMap = core.TSFalse
	options.InlineSourceMap = core.TSFalse
	options.InlineSources = core.TSFalse
	options.Composite = core.TSFalse
	options.Incremental = core.TSFalse
	options.IsolatedDeclarations = core.TSFalse
	result := config.WithFileNames(config.FileNames())
	result.SetCompilerOptions(&options)
	return result
}

func loadASTBenchmarkProject(tb testing.TB) benchmarkCompilerProject {
	tb.Helper()
	configFileName := tspath.NormalizeSlashes(filepath.Join(repo.TestDataPath(), "fixtures/compiler/tsconfig.json"))
	root := filepath.Dir(configFileName)
	fs := bundled.WrapFS(osvfs.FS())
	host := compiler.NewCompilerHost(root, fs, bundled.LibPath(), nil, nil, nil)
	config, errors := tsoptions.GetParsedCommandLineOfConfigFile(configFileName, &core.CompilerOptions{}, nil, host, nil)
	if len(errors) != 0 {
		tb.Fatalf("parse project config %s: %s", configFileName, formatDiagnostics(errors))
	}
	if config == nil {
		tb.Fatal("project config is nil")
	}
	return benchmarkCompilerProject{config: config, root: root, fs: fs}
}

func (p benchmarkCompilerProject) newProgram(config *tsoptions.ParsedCommandLine, singleThreaded core.Tristate) *compiler.Program {
	host := compiler.NewCompilerHost(p.root, p.fs, bundled.LibPath(), nil, nil, nil)
	return compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host, SingleThreaded: singleThreaded})
}

func formatDiagnostics(diagnostics []*ast.Diagnostic) string {
	if len(diagnostics) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		parts = append(parts, fmt.Sprintf("%d@%d:%d:%s", diagnostic.Code(), diagnostic.Pos(), diagnostic.End(), diagnostic.MessageText()))
	}
	return fmt.Sprint(parts)
}

func BenchmarkASTCheckV1(b *testing.B) {
	project := loadASTBenchmarkProject(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		program := project.newProgram(project.config, core.TSUnknown)
		program.BindSourceFiles()
		b.StartTimer()
		diagnostics := program.GetSemanticDiagnostics(context.Background(), nil)
		b.StopTimer()
		if len(diagnostics) != 0 {
			b.Fatalf("unexpected semantic diagnostics: %s", formatDiagnostics(diagnostics))
		}
	}
}

func BenchmarkASTCheckKPCV1(b *testing.B) {
	session := kperf.Start(b)
	defer session.Stop()
	project := loadASTBenchmarkProject(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		program := project.newProgram(project.config, core.TSTrue)
		program.BindSourceFiles()
		b.StartTimer()
		var diagnostics []*ast.Diagnostic
		session.Measure(func() { diagnostics = program.GetSemanticDiagnostics(context.Background(), nil) })
		b.StopTimer()
		if len(diagnostics) != 0 {
			b.Fatalf("unexpected semantic diagnostics: %s", formatDiagnostics(diagnostics))
		}
	}
	session.Report()
}

func BenchmarkASTEmitV1(b *testing.B) {
	project := loadASTBenchmarkProject(b)
	emitConfig := newASTBenchmarkJSEmitConfig(project.config)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		program := project.newProgram(emitConfig, core.TSUnknown)
		program.BindSourceFiles()
		if diagnostics := program.GetSemanticDiagnostics(context.Background(), nil); len(diagnostics) != 0 {
			b.Fatalf("unexpected semantic diagnostics before emit: %s", formatDiagnostics(diagnostics))
		}
		// Emit may invoke WriteFile concurrently. Atomics keep the timed callback
		// to the required scalar counters without changing the compiler path.
		var outputFiles, outputBytes atomic.Int64
		writeFile := func(_ string, text string, _ *compiler.WriteFileData) error {
			outputFiles.Add(1)
			outputBytes.Add(int64(len(text)))
			return nil
		}
		b.StartTimer()
		result := program.Emit(context.Background(), compiler.EmitOptions{WriteFile: writeFile})
		b.StopTimer()
		checkASTBenchmarkJSEmitResult(b, result, outputFiles.Load(), outputBytes.Load())
	}
}

func BenchmarkASTEmitKPCV1(b *testing.B) {
	session := kperf.Start(b)
	defer session.Stop()
	project := loadASTBenchmarkProject(b)
	emitConfig := newASTBenchmarkJSEmitConfig(project.config)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		program := project.newProgram(emitConfig, core.TSTrue)
		program.BindSourceFiles()
		if diagnostics := program.GetSemanticDiagnostics(context.Background(), nil); len(diagnostics) != 0 {
			b.Fatalf("unexpected semantic diagnostics before emit: %s", formatDiagnostics(diagnostics))
		}
		var outputFiles, outputBytes atomic.Int64
		writeFile := func(_ string, text string, _ *compiler.WriteFileData) error {
			outputFiles.Add(1)
			outputBytes.Add(int64(len(text)))
			return nil
		}
		b.StartTimer()
		var result *compiler.EmitResult
		session.Measure(func() { result = program.Emit(context.Background(), compiler.EmitOptions{WriteFile: writeFile}) })
		b.StopTimer()
		checkASTBenchmarkJSEmitResult(b, result, outputFiles.Load(), outputBytes.Load())
	}
	session.Report()
}

// The fixed project emits exactly this much JS. A mismatch means the inputs or
// the emitter changed the work this benchmark times, so results are no longer
// comparable with earlier runs and the literals must be updated deliberately.
// Output contents are covered by the conformance tests, not here.
const (
	astBenchmarkJSOutputFiles = 78
	astBenchmarkJSOutputBytes = 8841361
)

func checkASTBenchmarkJSEmitResult(tb testing.TB, result *compiler.EmitResult, outputFiles, outputBytes int64) {
	tb.Helper()
	if result == nil || result.EmitSkipped || len(result.Diagnostics) != 0 {
		tb.Fatalf("invalid emit result: %v", result)
	}
	if outputFiles != astBenchmarkJSOutputFiles || outputBytes != astBenchmarkJSOutputBytes {
		tb.Fatalf("JS output = %d files/%d bytes, want %d files/%d bytes", outputFiles, outputBytes, astBenchmarkJSOutputFiles, astBenchmarkJSOutputBytes)
	}
	if len(result.EmittedFiles) != int(outputFiles) {
		tb.Fatalf("emit result recorded %d paths for %d callback outputs", len(result.EmittedFiles), outputFiles)
	}
	for _, name := range result.EmittedFiles {
		if !tspath.HasJSFileExtension(name) {
			tb.Fatalf("non-JS output %q", name)
		}
	}
}
