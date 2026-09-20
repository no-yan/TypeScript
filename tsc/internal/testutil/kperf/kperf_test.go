package kperf

import "testing"

func TestReadBeforeOpenFails(t *testing.T) {
	Close()
	if _, err := Read(); err == nil {
		t.Fatal("Read before Open must return an error")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	Close()
	Close()
}

func TestTotalsAdd(t *testing.T) {
	tests := []struct {
		name   string
		before Counters
		after  Counters
		want   Totals
		fail   bool
	}{
		{name: "positive", before: Counters{Instructions: 10, Cycles: 20}, after: Counters{Instructions: 16, Cycles: 24}, want: Totals{Instructions: 6, Cycles: 4}},
		{name: "reversed instructions", before: Counters{Instructions: 10, Cycles: 20}, after: Counters{Instructions: 9, Cycles: 21}, fail: true},
		{name: "reversed cycles", before: Counters{Instructions: 10, Cycles: 20}, after: Counters{Instructions: 11, Cycles: 19}, fail: true},
		{name: "zero instructions", before: Counters{Instructions: 10, Cycles: 20}, after: Counters{Instructions: 10, Cycles: 21}, fail: true},
		{name: "zero cycles", before: Counters{Instructions: 10, Cycles: 20}, after: Counters{Instructions: 11, Cycles: 20}, fail: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var totals Totals
			err := totals.Add(test.before, test.after)
			if test.fail {
				if err == nil {
					t.Fatalf("Add(%+v, %+v) succeeded, want error", test.before, test.after)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if totals != test.want {
				t.Fatalf("totals = %+v, want %+v", totals, test.want)
			}
		})
	}
}

func TestTotalsAddAccumulates(t *testing.T) {
	totals := Totals{Instructions: 4, Cycles: 6}
	if err := totals.Add(Counters{Instructions: 10, Cycles: 20}, Counters{Instructions: 13, Cycles: 25}); err != nil {
		t.Fatal(err)
	}
	if want := (Totals{Instructions: 7, Cycles: 11}); totals != want {
		t.Fatalf("totals = %+v, want %+v", totals, want)
	}
}

func TestMetricsUsesAggregateTotalsForIPC(t *testing.T) {
	instPerOp, cyclesPerOp, ipc, err := metrics(Totals{Instructions: 30, Cycles: 12}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if instPerOp != 10 || cyclesPerOp != 4 || ipc != 2.5 {
		t.Fatalf("metrics = (%v, %v, %v), want (10, 4, 2.5)", instPerOp, cyclesPerOp, ipc)
	}
}

func TestMetricsRejectsEmptySamples(t *testing.T) {
	for _, test := range []struct {
		name   string
		totals Totals
		n      int
	}{
		{name: "no operations", totals: Totals{Instructions: 1, Cycles: 1}},
		{name: "no instructions", totals: Totals{Cycles: 1}, n: 1},
		{name: "no cycles", totals: Totals{Instructions: 1}, n: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, _, err := metrics(test.totals, test.n); err == nil {
				t.Fatal("metrics succeeded, want error")
			}
		})
	}
}

// BenchmarkReadPair reports the fixed cost that every Session.Measure interval
// includes and that the KPC benchmarks do not subtract. Trust inst/op only:
// cycles/op comes from a warm tight loop and underestimates the cost after a
// real workload has disturbed the caches. For an upper bound on cycles, take
// the maximum over -benchtime=1x -count=N.
func BenchmarkReadPair(b *testing.B) {
	session := Start(b)
	defer session.Stop()
	for i := 0; i < b.N; i++ {
		session.Measure(func() {})
	}
	session.Report()
}
