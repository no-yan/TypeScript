package storeclitest

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
	"github.com/microsoft/TypeScript/tsc/internal/storecompiler"
)

// TestXctraceNativeCheck is the process xctrace records for
// store-checker-perf-analysis-instructions-20261005.md 5.3: the interval of
// BenchmarkNativeCheckKPC on the first input of benchInputs, for the backend
// STORE_CLI_XCTRACE_BACKEND (pointer|store), K times (STORE_CLI_XCTRACE_K, 10
// by default) under the KPC conditions (the GC off, GOMAXPROCS 1, the thread
// locked, a GC before each interval). The samples of the interval are the
// ones with xctraceCheckPointer or xctraceCheckStore on their stack.
// xctrace does not show the process's output: the log is STORE_CLI_XCTRACE_LOG.
func TestXctraceNativeCheck(t *testing.T) {
	backend := core.ASTBackendPointer
	switch os.Getenv("STORE_CLI_XCTRACE_BACKEND") {
	case "":
		t.Skip("STORE_CLI_XCTRACE_BACKEND is not set")
	case "store":
		backend = core.ASTBackendStore
	case "pointer":
	default:
		t.Fatalf("STORE_CLI_XCTRACE_BACKEND %q", os.Getenv("STORE_CLI_XCTRACE_BACKEND"))
	}
	k := 10
	if s := os.Getenv("STORE_CLI_XCTRACE_K"); s != "" {
		var err error
		if k, err = strconv.Atoi(s); err != nil {
			t.Fatal(err)
		}
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	defer debug.SetGCPercent(debug.SetGCPercent(-1))

	logf := t.Logf
	if name := os.Getenv("STORE_CLI_XCTRACE_LOG"); name != "" {
		f, err := os.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		logf = func(format string, args ...any) { fmt.Fprintf(f, format+"\n", args...) }
	}

	input := benchInputs(t)[0]
	ctx := context.Background()
	var total time.Duration
	diagnostics := 0
	for i := range k {
		setup := time.Now()
		p := buildProgram(t, backend, input.config)
		var check func() int
		if backend == core.ASTBackendStore {
			p.store.BindSourceFiles()
			var targets []*store.File
			for _, f := range p.store.SourceFiles() {
				if !p.store.IsSourceFileDefaultLibrary(f.PathKey()) {
					targets = append(targets, f)
				}
			}
			check = func() int { return xctraceCheckStore(ctx, p.store, targets) }
		} else {
			p.pointer.BindSourceFiles()
			var targets []*ast.SourceFile
			for _, f := range p.pointer.SourceFiles() {
				if !p.pointer.IsSourceFileDefaultLibrary(f.PathKey()) {
					targets = append(targets, f)
				}
			}
			check = func() int { return xctraceCheckPointer(ctx, p.pointer, targets) }
		}
		runtime.GC()
		start := time.Now()
		diagnostics = check()
		total += time.Since(start)
		logf("run %d: setup %v, check %v", i, start.Sub(setup), time.Since(start))
	}
	logf("input %s, backend %s, K %d, check %v, diagnostics %d", input.name, backendName(backend), k, total, diagnostics)
}

// xctraceCheckPointer and xctraceCheckStore are the interval of
// BenchmarkNativeCheckKPC, apart so that the samples can be told by their
// stacks.
//
//go:noinline
func xctraceCheckPointer(ctx context.Context, program *compiler.Program, targets []*ast.SourceFile) int {
	var ds [][]*ast.Diagnostic
	c, _ := checker.NewChecker(program, nil)
	for _, f := range targets {
		ds = append(ds, c.GetDiagnostics(ctx, f))
	}
	ds = append(ds, c.GetGlobalDiagnostics())
	return countAll(ds)
}

//go:noinline
func xctraceCheckStore(ctx context.Context, program *storecompiler.Program, targets []*store.File) int {
	var ds [][]*store.Diagnostic
	c, _ := storechecker.NewChecker(program, nil)
	for _, f := range targets {
		ds = append(ds, c.GetDiagnostics(ctx, f))
	}
	ds = append(ds, c.GetGlobalDiagnostics())
	return countAll(ds)
}
