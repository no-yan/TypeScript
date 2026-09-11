//go:build binderinvestigation && kperf && darwin && arm64 && cgo

package binder

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/testutil/kperf"
)

type investigationEventConfig struct {
	Words   [8]uint64      `json:"words"`
	Metrics map[string]int `json:"metrics"`
}

var investigationKPCSink uint64

// Exercise real counter reads and release the PMU. Run twice in fresh processes
// before campaigns, as root, in a dedicated process (never in timed binaries).
func TestInvestigationKPC(t *testing.T) {
	config := openInvestigationEvents(t)
	for worker := 0; worker < 3; worker++ {
		err := kperf.OnFreshThread(func() error {
			measure := func(n int) ([10]uint64, error) {
				before, err := kperf.ReadEvents()
				if err != nil {
					return before, err
				}
				x := uint64(1)
				for i := 0; i < n; i++ {
					x = x*6364136223846793005 + uint64(i)
				}
				investigationKPCSink = x
				after, err := kperf.ReadEvents()
				if err != nil {
					return after, err
				}
				for i := range after {
					if after[i] < before[i] {
						return after, fmt.Errorf("counter %d decreased: before=%v after=%v", i, before, after)
					}
					after[i] -= before[i]
				}
				return after, nil
			}
			for round := 0; round < 100; round++ {
				// Exercise scheduler/GC boundaries as well as uninterrupted computation.
				if round%10 == 0 {
					runtime.GC()
				}
				short, err := measure(10000)
				if err != nil {
					return err
				}
				long, err := measure(1000000)
				if err != nil {
					return err
				}
				if short[1] == 0 || long[1] < short[1]*20 {
					return fmt.Errorf("fixed instructions did not scale: %v / %v", short, long)
				}
				if index, ok := config.Metrics["el0-inst/op"]; ok {
					if short[index] == 0 || long[index] < short[index]*20 {
						return fmt.Errorf("EL0 instructions did not scale: %v / %v", short, long)
					}
					if long[index] > long[1]+long[1]/20 {
						return fmt.Errorf("EL0 exceeds fixed instructions: %v", long)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	kperf.CloseEvents()
	if _, err := kperf.ReadEvents(); err == nil {
		t.Fatal("read after close must fail")
	}
	if err := kperf.OpenEvents([8]uint64{}); err == nil {
		kperf.CloseEvents()
		t.Fatal("same process reopen must fail")
	}
	t.Log("3 fresh pthreads x 100 short/long pairs including GC passed")
}

func openInvestigationEvents(tb testing.TB) investigationEventConfig {
	tb.Helper()
	data, err := os.ReadFile(os.Getenv("BINDER_KPC_CONFIG"))
	if err != nil {
		tb.Fatal(err)
	}
	var config investigationEventConfig
	if err := json.Unmarshal(data, &config); err != nil {
		tb.Fatal(err)
	}
	seen := map[int]bool{}
	for metric, index := range config.Metrics {
		if index < 2 || index >= 10 || config.Words[index-2] == 0 || seen[index] {
			tb.Fatalf("invalid event mapping: %s=%d", metric, index)
		}
		seen[index] = true
	}
	if len(seen) == 0 {
		tb.Fatal("empty event mapping")
	}
	if err := kperf.OpenEvents(config.Words); err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(kperf.CloseEvents)
	return config
}

func BenchmarkBindInvestigationKPC(b *testing.B) {
	config := openInvestigationEvents(b)
	for _, f := range investigationFixtures(b) {
		b.Run(f.Name, func(b *testing.B) {
			b.StopTimer()
			text := f.source(b)
			files, release := investigationBatch(b, f, text, b.N)
			defer release() // Also releases after counter errors or failed validation.
			runtime.GC()
			if os.Getenv("BINDER_BENCH_GC_OFF") == "1" {
				previous := debug.SetGCPercent(-1)
				defer debug.SetGCPercent(previous)
			}
			var total [10]uint64
			b.ReportAllocs()
			b.ResetTimer()
			err := kperf.OnFreshThread(func() error {
				// Timer bookkeeping is outside the counter interval. The wall/allocation
				// metrics still include the two reads; use the wall harness for timing.
				b.StartTimer()
				var err error
				total, err = measureInvestigationBindBatch(files, kperf.ReadEvents)
				b.StopTimer()
				return err
			})
			runtime.KeepAlive(files)
			if err != nil {
				b.Fatal(err)
			}
			for _, file := range files {
				if !file.IsBound() {
					b.Fatal("AST was not bound")
				}
			}
			release()
			if total[0] == 0 || total[1] == 0 {
				b.Fatal("fixed counters did not advance")
			}
			b.ReportMetric(float64(total[0])/float64(b.N), "fixed-cycles/op")
			b.ReportMetric(float64(total[1])/float64(b.N), "fixed-inst/op")
			for metric, index := range config.Metrics {
				if (metric == "el0-inst/op" || metric == "el0-cycles/op") && total[index] == 0 {
					b.Fatalf("%s did not advance", metric)
				}
				b.ReportMetric(float64(total[index])/float64(b.N), metric)
			}
		})
	}
}

// read is injectable so the measured interval and failure paths can be tested
// without opening the PMU. Counter-read boundary overhead is not subtracted.
func measureInvestigationBindBatch(files []*ast.SourceFile, read func() ([10]uint64, error)) ([10]uint64, error) {
	before, err := read()
	if err != nil {
		return [10]uint64{}, err
	}
	for _, file := range files {
		BindSourceFile(file)
	}
	after, err := read()
	if err != nil {
		return [10]uint64{}, err
	}
	for i := range after {
		if after[i] < before[i] {
			return [10]uint64{}, fmt.Errorf("counter %d decreased: before=%v after=%v", i, before, after)
		}
		after[i] -= before[i]
	}
	return after, nil
}
