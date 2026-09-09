//go:build kperf && darwin && arm64

package parser

import (
	"os"
	"runtime"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/kperf"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/osvfs"
)

// BenchmarkParseKPC counts cycles and instructions around ParseSourceFile with
// kpc thread counters (no sampling). See binder/docs/kperf-bind-measurement.md.
func BenchmarkParseKPC(b *testing.B) {
	if os.Geteuid() != 0 {
		b.Skip("kpc requires root")
	}
	if err := kperf.Open(); err != nil {
		b.Skip(err)
	}
	defer kperf.Close()

	for _, f := range fixtures.BenchFixtures {
		b.Run(f.Name(), func(b *testing.B) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()

			f.SkipIfNotExist(b)
			fileName := tspath.GetNormalizedAbsolutePath(f.Path(), "/")
			path := tspath.ToPath(fileName, "/", osvfs.FS().UseCaseSensitiveFileNames())
			sourceText := f.ReadFile(b)
			parseOptions := ast.SourceFileParseOptions{FileName: fileName, Path: path}
			scriptKind := core.GetScriptKindFromFileName(fileName)

			var inst, cyc uint64
			for range b.N {
				b.StopTimer()
				runtime.GC()
				before := kperf.Read()
				b.StartTimer()
				ParseSourceFile(parseOptions, sourceText, scriptKind)
				b.StopTimer()
				after := kperf.Read()
				if after.Instructions < before.Instructions || after.Cycles < before.Cycles {
					b.Fatalf("negative kpc delta (thread migrate?): inst %d→%d cycles %d→%d",
						before.Instructions, after.Instructions, before.Cycles, after.Cycles)
				}
				inst += after.Instructions - before.Instructions
				cyc += after.Cycles - before.Cycles
			}
			n := float64(b.N)
			b.ReportMetric(float64(inst)/n, "inst/op")
			b.ReportMetric(float64(cyc)/n, "cycles/op")
			if cyc > 0 {
				b.ReportMetric(float64(inst)/float64(cyc), "IPC")
			}
		})
	}
}
