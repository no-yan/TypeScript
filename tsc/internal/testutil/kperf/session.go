package kperf

import (
	"fmt"
	"runtime"
	"testing"
)

// Session is one KPC benchmark run. It owns the thread pinning, the counter
// lifetime, and the measurement contract shared by every KPC benchmark: each
// interval must be strictly positive, and the Read pair overhead is not
// subtracted. BenchmarkReadPair reports that overhead.
type Session struct {
	b        *testing.B
	totals   Totals
	oldProcs int
}

// Callers must defer Stop.
//
// Builds without the kperf tag skip: nobody asked for counters. Builds with
// the tag fail when the counters cannot be opened, so a partial run is never
// mistaken for a complete one.
func Start(b *testing.B) *Session {
	b.Helper()
	if !available {
		b.Skip("kperf: build with -tags kperf (darwin/arm64, cgo, root)")
	}
	runtime.LockOSThread()
	s := &Session{b: b, oldProcs: runtime.GOMAXPROCS(1)}
	if err := Open(); err != nil {
		s.Stop()
		b.Fatalf("kperf unavailable: %v", err)
	}
	return s
}

func (s *Session) Stop() {
	Close()
	runtime.GOMAXPROCS(s.oldProcs)
	runtime.UnlockOSThread()
}

// Measure leaves the benchmark timers to the caller.
func (s *Session) Measure(fn func()) {
	before, err := Read()
	if err != nil {
		s.b.Fatal(err)
	}
	fn()
	after, err := Read()
	if err != nil {
		s.b.Fatal(err)
	}
	if err := s.totals.Add(before, after); err != nil {
		s.b.Fatal(err)
	}
}

func (s *Session) Report() {
	s.b.Helper()
	instPerOp, cyclesPerOp, ipc, err := metrics(s.totals, s.b.N)
	if err != nil {
		s.b.Fatal(err)
	}
	s.b.ReportMetric(instPerOp, "inst/op")
	s.b.ReportMetric(cyclesPerOp, "cycles/op")
	s.b.ReportMetric(ipc, "IPC")
}

type Totals struct {
	Instructions uint64
	Cycles       uint64
}

func (t *Totals) Add(before, after Counters) error {
	if after.Instructions <= before.Instructions || after.Cycles <= before.Cycles {
		return fmt.Errorf("counter delta must be positive: before=%+v after=%+v", before, after)
	}
	t.Instructions += after.Instructions - before.Instructions
	t.Cycles += after.Cycles - before.Cycles
	return nil
}

func metrics(totals Totals, n int) (instPerOp, cyclesPerOp, ipc float64, err error) {
	if n <= 0 || totals.Instructions == 0 || totals.Cycles == 0 {
		return 0, 0, 0, fmt.Errorf("no positive KPC samples: totals=%+v n=%d", totals, n)
	}
	return float64(totals.Instructions) / float64(n),
		float64(totals.Cycles) / float64(n),
		float64(totals.Instructions) / float64(totals.Cycles),
		nil
}
