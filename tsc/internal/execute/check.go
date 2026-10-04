package execute

import (
	"context"
	"runtime"
	"strconv"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/diagnosticwriter"
	"github.com/microsoft/TypeScript/tsc/internal/execute/tsc"
	"github.com/microsoft/TypeScript/tsc/internal/storecompiler"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
)

// The two programs of the check mode (store-cli-design-20261004.md 5.1):
// each builds its program, gives its stages to tsc.CollectCheckDiagnostics
// and its report to tsc.ReportCompilation, and keeps the program alive until
// the report is done (the statistics may make the checker).

// performPointerCheck is the check mode on compiler.Program, with the stages
// that EmitFilesAndReportErrors asks of it.
func performPointerCheck(
	ctx context.Context,
	sys tsc.System,
	config *tsoptions.ParsedCommandLine,
	reporters tsc.CheckReporters,
	extendedConfigCache tsoptions.ExtendedConfigCache,
	compileTimes *tsc.CompileTimes,
	testing tsc.CommandLineTesting,
) tsc.CommandLineResult {
	host := compiler.NewCachedFSCompilerHost(sys.GetCurrentDirectory(), sys.FS(), sys.DefaultLibraryPath(), extendedConfigCache, getTraceFromSys(sys, config.Locale(), testing), nil)
	parseStart := sys.Now()
	program := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host})
	compileTimes.ParseTime = sys.Now().Sub(parseStart)

	diagnostics, run := tsc.CollectCheckDiagnostics(ctx, sys, compileTimes, tsc.CheckStages[*ast.Diagnostic]{
		Config:    program.GetConfigFileParsingDiagnostics,
		Syntactic: func(ctx context.Context) []*ast.Diagnostic { return program.GetSyntacticDiagnostics(ctx, nil) },
		Program:   program.GetProgramDiagnostics,
		// Options diagnostics include global diagnostics (even though we collect them separately),
		// and global diagnostics create checkers, which then bind all of the files. Do this binding
		// early so we can track the time.
		Bind:               func(ctx context.Context) { program.GetBindDiagnostics(ctx, nil) },
		Global:             program.GetGlobalDiagnostics,
		Semantic:           func(ctx context.Context) []*ast.Diagnostic { return program.GetSemanticDiagnostics(ctx, nil) },
		SortAndDeduplicate: compiler.SortAndDeduplicateDiagnostics,
	})
	emitSkipped := tsc.CheckEmit(sys, compileTimes, func() bool {
		result := program.Emit(ctx, compiler.EmitOptions{})
		if len(result.Diagnostics) != 0 {
			panic("execute: the emit of a --noEmit check has diagnostics")
		}
		if testing != nil {
			testing.OnEmittedFiles(result, nil)
		}
		return result.EmitSkipped
	})
	fileNames := make([]string, 0, len(program.GetSourceFiles()))
	for _, file := range program.GetSourceFiles() {
		fileNames = append(fileNames, file.FileName())
	}
	status := tsc.ReportCompilation(sys, config.CompilerOptions(), reporters, tsc.CompilationReport{
		Diagnostics: diagnosticwriter.FromASTDiagnostics(diagnostics),
		FileNames:   fileNames,
		Statistics: func() tsc.ProgramStatistics {
			return tsc.ProgramStatistics{
				Files:          len(program.SourceFiles()),
				Lines:          program.LineCount(),
				Identifiers:    program.IdentifierCount(),
				Symbols:        program.SymbolCount(),
				Types:          program.TypeCount(),
				Instantiations: program.InstantiationCount(),
			}
		},
		Times:         compileTimes,
		EmitSkipped:   emitSkipped,
		CheckExecuted: run.CheckExecuted,
	}, testing)
	runtime.KeepAlive(program)
	return tsc.CommandLineResult{Status: status}
}

// performStoreCheck is the check mode on storecompiler.Program: the same
// stages, the Store's diagnostics and the Store's sort.
func performStoreCheck(
	ctx context.Context,
	sys tsc.System,
	config *tsoptions.ParsedCommandLine,
	reporters tsc.CheckReporters,
	extendedConfigCache tsoptions.ExtendedConfigCache,
	compileTimes *tsc.CompileTimes,
	testing tsc.CommandLineTesting,
) tsc.CommandLineResult {
	host := storecompiler.NewCachedFSCompilerHost(sys.GetCurrentDirectory(), sys.FS(), sys.DefaultLibraryPath(), extendedConfigCache, getTraceFromSys(sys, config.Locale(), testing))
	parseStart := sys.Now()
	program := storecompiler.NewProgram(storecompiler.ProgramOptions{Config: config, Host: host})
	compileTimes.ParseTime = sys.Now().Sub(parseStart)

	diagnostics, run := tsc.CollectCheckDiagnostics(ctx, sys, compileTimes, tsc.CheckStages[*store.Diagnostic]{
		Config:             program.GetConfigFileParsingDiagnostics,
		Syntactic:          func(ctx context.Context) []*store.Diagnostic { return program.GetSyntacticDiagnostics(ctx, nil) },
		Program:            program.GetProgramDiagnostics,
		Bind:               func(ctx context.Context) { program.GetBindDiagnostics(ctx, nil) },
		Global:             program.GetGlobalDiagnostics,
		Semantic:           func(ctx context.Context) []*store.Diagnostic { return program.GetSemanticDiagnostics(ctx, nil) },
		SortAndDeduplicate: storecompiler.SortAndDeduplicateDiagnostics,
	})
	// The Store program has no emitter: its --noEmit emit is
	// compiler.HandleNoEmitOptions's, an empty result.
	emitSkipped := tsc.CheckEmit(sys, compileTimes, func() bool {
		if testing != nil {
			testing.OnEmittedFiles(&compiler.EmitResult{}, nil)
		}
		return false
	})
	fileNames := make([]string, 0, len(program.SourceFiles()))
	for _, file := range program.SourceFiles() {
		fileNames = append(fileNames, file.FileName())
	}
	status := tsc.ReportCompilation(sys, config.CompilerOptions(), reporters, tsc.CompilationReport{
		Diagnostics: storecompiler.WrapDiagnostics(diagnostics),
		FileNames:   fileNames,
		Statistics: func() tsc.ProgramStatistics {
			return tsc.ProgramStatistics{
				Files:          len(program.SourceFiles()),
				Lines:          program.LineCount(),
				Identifiers:    program.IdentifierCount(),
				Symbols:        program.SymbolCount(),
				Types:          program.TypeCount(),
				Instantiations: program.InstantiationCount(),
			}
		},
		Times:         compileTimes,
		EmitSkipped:   emitSkipped,
		CheckExecuted: run.CheckExecuted,
	}, testing)
	runtime.KeepAlive(program)
	return tsc.CommandLineResult{Status: status}
}

// checkModeUnsupported are the reasons that a merged config is outside the
// check mode (store-cli-design-20261004.md 3), one diagnostic each: the Store
// program rejects them, and the Pointer program takes the compile path.
func checkModeUnsupported(config *tsoptions.ParsedCommandLine) []*ast.Diagnostic {
	options := config.CompilerOptions()
	var reasons []*ast.Diagnostic
	requires := func(name string) {
		reasons = append(reasons, ast.NewCompilerDiagnostic(diagnostics.The_store_AST_backend_requires_0, name))
	}
	unsupported := func(name string) {
		reasons = append(reasons, ast.NewCompilerDiagnostic(diagnostics.The_store_AST_backend_does_not_support_0, name))
	}
	if !options.NoEmit.IsTrue() {
		requires("--noEmit")
	}
	if !options.SingleThreaded.IsTrue() {
		requires("--singleThreaded")
	}
	if options.Checkers != nil && *options.Checkers != 1 {
		unsupported("--checkers " + strconv.Itoa(*options.Checkers))
	}
	for _, option := range []struct {
		name    string
		enabled bool
	}{
		{"declaration", options.Declaration.IsTrue()},
		{"emitDeclarationOnly", options.EmitDeclarationOnly.IsTrue()},
		{"declarationMap", options.DeclarationMap.IsTrue()},
		{"isolatedDeclarations", options.IsolatedDeclarations.IsTrue()},
		{"composite", options.Composite.IsTrue()},
		{"incremental", options.Incremental.IsTrue()},
		{"watch", options.Watch.IsTrue()},
		{"noCheck", options.NoCheck.IsTrue()},
		{"listFilesOnly", options.ListFilesOnly.IsTrue()},
		{"explainFiles", options.ExplainFiles.IsTrue()},
		{"generateTrace", options.GenerateTrace != ""},
		{"references", len(config.ProjectReferences()) != 0},
		{"contentMappers", len(config.ContentMappers()) != 0},
	} {
		if option.enabled {
			unsupported(option.name)
		}
	}
	return reasons
}
