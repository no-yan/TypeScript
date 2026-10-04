package tsc

import (
	"context"
	"fmt"
	"runtime"
	"slices"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnosticwriter"
	"github.com/microsoft/TypeScript/tsc/internal/locale"
)

// The check mode of the CLI (store-cli-design-20261004.md 5.1): a one-shot,
// single-threaded --noEmit check whose collection and report are the same for
// the Pointer and the Store programs. Each backend gives the stages and the
// diagnostics in its own type D; the collector owns the order and the
// short-circuits of compiler.GetDiagnosticsOfAnyProgram, and the reporter the
// order of the output and the exit status.

// CheckStages are a program's diagnostics, in the program's file order. A
// stage returns values only: the collector decides which stages run.
// SortAndDeduplicate is the backend's ast.CompareDiagnostics and
// compiler.SortAndDeduplicateDiagnostics.
type CheckStages[D any] struct {
	Config             func() []D
	Syntactic          func(context.Context) []D
	Program            func() []D
	Bind               func(context.Context)
	Global             func(context.Context) []D
	Semantic           func(context.Context) []D
	SortAndDeduplicate func([]D) []D
}

// CheckStage names a stage in the record of a collection.
type CheckStage string

const (
	StageConfig    CheckStage = "config"
	StageSyntactic CheckStage = "syntactic"
	StageProgram   CheckStage = "program"
	StageBind      CheckStage = "bind"
	StageGlobal    CheckStage = "global"
	StageSemantic  CheckStage = "semantic"
)

// CheckRun is what a collection did: the stages in the order they ran, and
// whether the semantic stage (the check of the files) ran.
type CheckRun struct {
	Stages        []CheckStage
	CheckExecuted bool
}

// CollectCheckDiagnostics is the part of compiler.GetDiagnosticsOfAnyProgram
// that a --noEmit check reaches (no declaration diagnostics, no
// --listFilesOnly, no content mapper): the config diagnostics are the base;
// with no syntactic diagnostic, the program's, the bind and the global ones;
// with nothing more than the base, the semantic ones and the global ones
// again. The bind and the check are timed into times.
func CollectCheckDiagnostics[D any](ctx context.Context, sys System, times *CompileTimes, stages CheckStages[D]) ([]D, CheckRun) {
	var run CheckRun
	allDiagnostics := slices.Clip(stages.Config())
	run.Stages = append(run.Stages, StageConfig)
	configFileParsingDiagnosticsLength := len(allDiagnostics)

	allDiagnostics = append(allDiagnostics, stages.Syntactic(ctx)...)
	run.Stages = append(run.Stages, StageSyntactic)

	if len(allDiagnostics) == configFileParsingDiagnosticsLength {
		allDiagnostics = append(allDiagnostics, stages.Program()...)
		run.Stages = append(run.Stages, StageProgram)

		bindStart := sys.Now()
		stages.Bind(ctx)
		times.bindTime = sys.Now().Sub(bindStart)
		run.Stages = append(run.Stages, StageBind)

		allDiagnostics = append(allDiagnostics, stages.Global(ctx)...)
		run.Stages = append(run.Stages, StageGlobal)

		if len(allDiagnostics) == configFileParsingDiagnosticsLength {
			checkStart := sys.Now()
			allDiagnostics = append(allDiagnostics, stages.Semantic(ctx)...)
			times.checkTime = sys.Now().Sub(checkStart)
			run.Stages = append(run.Stages, StageSemantic)
			run.CheckExecuted = true
			// Ask for the global diagnostics again (they were empty above); we may have found new during checking, e.g. missing globals.
			allDiagnostics = append(allDiagnostics, stages.Global(ctx)...)
			run.Stages = append(run.Stages, StageGlobal)
		}
	}
	return stages.SortAndDeduplicate(allDiagnostics), run
}

// CheckEmit is the emit of a --noEmit check, timed as EmitFilesAndReportErrors
// times the emit, so that --diagnostics shows the same rows: emit gives
// whether the emit was skipped (compiler.HandleNoEmitOptions gives false for
// --noEmit).
func CheckEmit(sys System, times *CompileTimes, emit func() (skipped bool)) bool {
	start := sys.Now()
	skipped := emit()
	times.emitTime += sys.Now().Sub(start)
	return skipped
}

// ProgramStatistics are the program's counts of the --diagnostics table.
type ProgramStatistics struct {
	Files, Lines, Identifiers, Symbols, Types, Instantiations int
}

// CompilationReport is what the reporter prints of a check: no AST and no
// program. Statistics is asked only for --diagnostics, after the GCs and the
// memory snapshot; it may make the checker.
type CompilationReport struct {
	Diagnostics  []diagnosticwriter.Diagnostic
	FileNames    []string // in program order
	EmittedFiles []string // none with --noEmit
	Statistics   func() ProgramStatistics
	Times        *CompileTimes
	// EmitSkipped is compiler.EmitResult's: --noEmit gives false.
	EmitSkipped bool
	// EarlyExit, when set, is the status of a check that did not run (an
	// unsupported option): its diagnostics are printed, and nothing else.
	EarlyExit     *ExitStatus
	CheckExecuted bool
}

// CheckReporters print a report: the diagnostics as CreateDiagnosticReporter
// does, and the summary as CreateReportErrorSummary does.
type CheckReporters struct {
	Diagnostic   func(diagnosticwriter.Diagnostic)
	ErrorSummary func([]diagnosticwriter.Diagnostic)
}

// NewCheckReporters are the reporters of tscCompilation: the diagnostics by
// the command line's options, the summary by the program's.
func NewCheckReporters(sys System, commandLineOptions, options *core.CompilerOptions, loc locale.Locale) CheckReporters {
	return CheckReporters{
		Diagnostic:   createDiagnosticWriter(sys, sys.Writer(), loc, commandLineOptions),
		ErrorSummary: createErrorSummaryWriter(sys, loc, options),
	}
}

// ReportCompilation prints a report as EmitAndReportStatistics does: the
// diagnostics, the listed files, the error summary, then the statistics; and
// gives the exit status.
func ReportCompilation(sys System, options *core.CompilerOptions, reporters CheckReporters, report CompilationReport, testing CommandLineTesting) ExitStatus {
	for _, diagnostic := range report.Diagnostics {
		reporters.Diagnostic(diagnostic)
	}
	if report.EarlyExit != nil {
		return *report.EarlyExit
	}

	writer := sys.Writer()
	if testing != nil {
		testing.OnListFilesStart(writer)
	}
	if options.ListEmittedFiles.IsTrue() {
		for _, file := range report.EmittedFiles {
			fmt.Fprintln(writer, "TSFILE:", file)
		}
	}
	if options.ListFiles.IsTrue() {
		for _, file := range report.FileNames {
			fmt.Fprintln(writer, file)
		}
	}
	if testing != nil {
		testing.OnListFilesEnd(writer)
	}

	reporters.ErrorSummary(report.Diagnostics)

	report.Times.totalTime = sys.SinceStart()
	if options.Diagnostics.IsTrue() || options.ExtendedDiagnostics.IsTrue() {
		var memStats runtime.MemStats
		// GC must be called twice to allow things to settle.
		runtime.GC()
		runtime.GC()
		runtime.ReadMemStats(&memStats)
		counts := report.Statistics()
		statistics := &Statistics{
			files:          counts.Files,
			lines:          counts.Lines,
			identifiers:    counts.Identifiers,
			symbols:        counts.Symbols,
			types:          counts.Types,
			instantiations: counts.Instantiations,
			memoryUsed:     memStats.Alloc,
			memoryAllocs:   memStats.Mallocs,
			compileTimes:   report.Times,
		}
		statistics.Report(writer, testing)
	}

	switch {
	case len(report.Diagnostics) == 0:
		return ExitStatusSuccess
	case report.EmitSkipped:
		return ExitStatusDiagnosticsPresent_OutputsSkipped
	default:
		return ExitStatusDiagnosticsPresent_OutputsGenerated
	}
}
