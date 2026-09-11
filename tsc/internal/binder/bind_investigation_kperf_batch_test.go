//go:build binderinvestigation && kperf && darwin && arm64 && cgo

package binder

import (
	"errors"
	"testing"
)

// Injected reads exercise the real batch without PMU access.
func TestInvestigationKPCBatch(t *testing.T) {
	f := investigationFixture{Name: "small.ts", Path: "/small.ts"}
	for _, mode := range []string{"success", "before-error", "after-error", "decreasing"} {
		t.Run(mode, func(t *testing.T) {
			baseline := investigationRegisteredStores()
			func() {
				files, release := investigationBatch(t, f, "function f(x: number) { return x + 1; }", 2)
				defer release()
				reads := 0
				injected := errors.New("injected counter failure")
				delta, err := measureInvestigationBindBatch(files, func() ([10]uint64, error) {
					reads++
					for _, file := range files {
						if file.IsBound() != (reads == 2) {
							t.Fatal("counter boundary must surround all binds")
						}
					}
					if mode == "before-error" && reads == 1 || mode == "after-error" && reads == 2 {
						return [10]uint64{}, injected
					}
					value := uint64(reads * 100)
					if mode == "decreasing" && reads == 2 {
						value = 1
					}
					var counters [10]uint64
					for i := range counters {
						counters[i] = value
					}
					return counters, nil
				})
				if mode == "success" {
					if err != nil || delta[0] != 100 || delta[9] != 100 {
						t.Fatalf("delta=%v err=%v", delta, err)
					}
				} else if err == nil {
					t.Fatal("expected counter failure")
				}
				expectedReads := 2
				if mode == "before-error" {
					expectedReads = 1
				}
				if reads != expectedReads {
					t.Fatalf("reads=%d, want %d", reads, expectedReads)
				}
			}()
			if investigationRegisteredStores() != baseline {
				t.Fatal("batch leaked Store registration")
			}
		})
	}
}
