package storeharness

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"testing"
	"time"
)

// TestXctraceCheck is the process xctrace records for store-checker-7d2c-
// xctrace-instructions-20261002.md: the work of BenchmarkCheckKPCPointer or
// BenchmarkCheckKPCStore, chosen by STORE_CHECKER_XCTRACE_SIDE, K times
// (STORE_CHECKER_XCTRACE_K, 10 by default) under the KPC conditions. The
// samples of the check are the ones with checkPointer or checkStore on their
// stack.
func TestXctraceCheck(t *testing.T) {
	side := os.Getenv("STORE_CHECKER_XCTRACE_SIDE")
	if side == "" {
		t.Skip("STORE_CHECKER_XCTRACE_SIDE is not set")
	}
	k := 10
	if s := os.Getenv("STORE_CHECKER_XCTRACE_K"); s != "" {
		var err error
		if k, err = strconv.Atoi(s); err != nil {
			t.Fatal(err)
		}
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	defer debug.SetGCPercent(debug.SetGCPercent(-1))

	// xctrace does not show the launched process's output: the log is
	// STORE_CHECKER_XCTRACE_LOG when it is set.
	logf := t.Logf
	if name := os.Getenv("STORE_CHECKER_XCTRACE_LOG"); name != "" {
		f, err := os.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		logf = func(format string, args ...any) { fmt.Fprintf(f, format+"\n", args...) }
	}

	var work func() func()
	switch side {
	case "pointer":
		work = func() func() {
			program := newCompilerProjectProgram(t)
			program.BindSourceFiles()
			diagFiles, _ := projectTargets(program)
			return func() { checkPointer(t, program, diagFiles) }
		}
	case "store":
		fixture := readFixture(t)
		work = func() func() {
			program := NewFixtureProgram(fixture)
			program.BindSourceFiles()
			diagFiles := fixtureDiagFiles(fixture, program)
			return func() { checkStore(t, program, diagFiles) }
		}
	default:
		t.Fatalf("side %q", side)
	}
	var total time.Duration
	for i := range k {
		setup := time.Now()
		check := work()
		runtime.GC()
		start := time.Now()
		check()
		total += time.Since(start)
		logf("run %d: setup %v, check %v", i, start.Sub(setup), time.Since(start))
	}
	logf("side %s, K %d, check %v", side, k, total)
}
