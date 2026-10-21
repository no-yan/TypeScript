package storeharness

import (
	"context"
	"os"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/kperf"
)

// The measurements of store-checker-7d2c-implementation-instructions-
// 20261002.md 4 and 5 on the compiler project. The Store side runs on the
// FixtureProgram of the fixture STORE_CHECKER_FIXTURE names, which
// TestWriteCompilerProjectFixture writes: the process that measures it has no
// Pointer program.
//
// The work is the check of the compiler (9's 2): NewChecker, GetDiagnostics
// of each file of the diagnostic targets, GetGlobalDiagnostics. No
// suggestion: compiler.Program.GetSemanticDiagnostics has none.

const fixtureEnv = "STORE_CHECKER_FIXTURE"

// TestWriteCompilerProjectFixture records the H1 Program of the compiler
// project on the driver's whole work (the check, the suggestions and the
// walks: what the measurements ask is a part of it) and writes the fixture to
// STORE_CHECKER_FIXTURE.
func TestWriteCompilerProjectFixture(t *testing.T) {
	name := os.Getenv(fixtureEnv)
	if name == "" {
		t.Skip(fixtureEnv + " is not set")
	}
	pointer := newCompilerProjectProgram(t)
	diagFiles, walkFiles := projectTargets(pointer)
	class, detail, fixture, err := fixtureIdentity(pointer, diagFiles, walkFiles)
	if err != nil {
		t.Fatal(err)
	}
	if class != ClassEqual {
		t.Fatalf("class %q: %+v", class, detail)
	}
	if err := WriteFixture(fixture, name); err != nil {
		t.Fatal(err)
	}
	t.Logf("files %d, modules %d, questions %d, dropped resolution diagnostics %d", len(fixture.Files), len(fixture.Modules), fixture.questions(), fixture.DroppedResolutionDiagnostics)
}

func readFixture(tb testing.TB) *Fixture {
	tb.Helper()
	name := os.Getenv(fixtureEnv)
	if name == "" {
		tb.Skip(fixtureEnv + " is not set")
	}
	f, err := ReadFixture(name)
	if err != nil {
		tb.Fatal(err)
	}
	return f
}

// fixtureDiagFiles are projectTargets's diagnostic targets on the fixture's
// files: all but the default libs when the options skip their check.
func fixtureDiagFiles(f *Fixture, p *FixtureProgram) []*store.File {
	var out []*store.File
	skip := f.Options.SkipDefaultLibCheck.IsTrue()
	for _, sf := range p.SourceFiles() {
		if !(skip && p.IsSourceFileDefaultLibrary(sf.PathKey())) {
			out = append(out, sf)
		}
	}
	return out
}

func storeNodes(files []*store.File) int {
	n := 0
	for _, f := range files {
		n += f.Store.NodeCount()
	}
	return n
}

// checkPointer is the work on the Pointer side.
func checkPointer(tb testing.TB, program *compiler.Program, diagFiles []*ast.SourceFile) *checker.Checker {
	ctx := context.Background()
	c, _ := checker.NewChecker(program, nil)
	n := 0
	for _, f := range diagFiles {
		n += len(c.GetDiagnostics(ctx, f))
	}
	n += len(c.GetGlobalDiagnostics())
	if n != 0 {
		tb.Fatalf("%d diagnostics", n)
	}
	return c
}

// checkStore is the work on the Store side.
func checkStore(tb testing.TB, program storechecker.Program, diagFiles []*store.File) *storechecker.Checker {
	ctx := context.Background()
	c, _ := storechecker.NewChecker(program, nil)
	n := 0
	for _, f := range diagFiles {
		n += len(c.GetDiagnostics(ctx, f))
	}
	n += len(c.GetGlobalDiagnostics())
	if n != 0 {
		tb.Fatalf("%d diagnostics", n)
	}
	return c
}

func reportPerNode(b *testing.B, session *kperf.Session, nodes int) {
	totals := session.Totals()
	b.ReportMetric(float64(totals.Instructions)/float64(b.N)/float64(nodes), "inst/node")
	b.ReportMetric(float64(totals.Cycles)/float64(b.N)/float64(nodes), "cycles/node")
}

func BenchmarkCheckKPCPointer(b *testing.B) {
	session := kperf.Start(b)
	defer session.Stop()
	nodes := storeNodes(NewFixtureProgram(readFixture(b)).SourceFiles())
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		program := newCompilerProjectProgram(b)
		program.BindSourceFiles()
		diagFiles, _ := projectTargets(program)
		b.StartTimer()
		session.Measure(func() { checkPointer(b, program, diagFiles) })
		b.StopTimer()
	}
	session.Report()
	reportPerNode(b, session, nodes)
}

func BenchmarkCheckKPCStore(b *testing.B) {
	session := kperf.Start(b)
	defer session.Stop()
	fixture := readFixture(b)
	var nodes int
	calls := map[string]int{}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		program := NewFixtureProgram(fixture)
		program.BindSourceFiles()
		diagFiles := fixtureDiagFiles(fixture, program)
		nodes = storeNodes(program.SourceFiles())
		program.Calls = [numCalls]int{}
		b.StartTimer()
		session.Measure(func() { checkStore(b, program, diagFiles) })
		b.StopTimer()
		calls = program.CallCounts()
	}
	session.Report()
	reportPerNode(b, session, nodes)
	for method, n := range calls {
		b.Logf("calls %s %d", method, n)
	}
}

// BenchmarkCheckKPCProgram is compiler's BenchmarkASTCheckKPCV1 in this
// binary: the program's GetSemanticDiagnostics, its own checkers.
func BenchmarkCheckKPCProgram(b *testing.B) {
	session := kperf.Start(b)
	defer session.Stop()
	nodes := storeNodes(NewFixtureProgram(readFixture(b)).SourceFiles())
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		program := newCompilerProjectProgram(b)
		program.BindSourceFiles()
		b.StartTimer()
		var diagnostics []*ast.Diagnostic
		session.Measure(func() { diagnostics = program.GetSemanticDiagnostics(context.Background(), nil) })
		b.StopTimer()
		if len(diagnostics) != 0 {
			b.Fatalf("%d semantic diagnostics", len(diagnostics))
		}
	}
	session.Report()
	reportPerNode(b, session, nodes)
}
