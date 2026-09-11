//go:build binderinvestigation

package binder

import (
	"runtime"
	"runtime/metrics"
	"testing"
)

func TestInvestigationBatchLifetime(t *testing.T) {
	for _, f := range investigationFixtures(t) {
		t.Run(f.Name, func(t *testing.T) {
			text := f.source(t)
			baseline := investigationRegisteredStores()
			symbols, diagnostics := -1, -1
			// Include the calibration shape, then repeat the full bounded batch.
			for pass, size := range []int{1, 10, 10, 10} {
				files, release := investigationBatch(t, f, text, size)
				for _, file := range files {
					BindSourceFile(file)
					if !file.IsBound() {
						t.Fatal("AST was not bound")
					}
					if symbols == -1 {
						symbols, diagnostics = file.SymbolCount, len(file.BindDiagnostics())
					}
					if file.SymbolCount != symbols || len(file.BindDiagnostics()) != diagnostics {
						t.Fatal("bind result changed across independent AST lifetimes")
					}
				}
				liveStores := investigationRegisteredStores()
				release()
				release() // Explicit release and testing.Cleanup must be idempotent.
				for _, file := range files {
					if file != nil {
						t.Fatal("batch still retains a SourceFile")
					}
				}
				if investigationRegisteredStores() != baseline {
					t.Fatal("registration count did not return to baseline")
				}
				// Lifetime probe only: drain pool generations outside benchmark runs.
				runtime.GC()
				runtime.GC()
				samples := []metrics.Sample{{Name: "/gc/heap/live:bytes"}, {Name: "/gc/scan/heap:bytes"}}
				metrics.Read(samples)
				t.Logf("pass=%d batch=%d registered_before=%d registered_bound=%d registered_after=%d symbols=%d diagnostics=%d heap_live=%d heap_scan=%d",
					pass, size, baseline, liveStores, investigationRegisteredStores(), symbols, diagnostics,
					samples[0].Value.Uint64(), samples[1].Value.Uint64())
			}
		})
	}
}
