package binder_test

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/osvfs"
)

// BenchmarkParseBindColumnPresize compares allocating the symbolIdx/flows bind
// columns lazily in PrepareBindTables (presize=0) against pre-sizing them in
// NewStore at (hint+1)*pct/100 entries. Each op parses then binds a whole file
// set sequentially so ns/op is CPU time, not wall time under parallelism.
//
// File sets: the checker.ts and dom.generated.d.ts fixtures, plus any
// TSGO_BENCH_FILELIST=name=path,... entries where path is a text file listing
// absolute source paths one per line (for example the output of tsc
// --listFiles). Reported metrics per op: parse-ms, bind-ms, and live-MB, the
// heap still reachable after bind (HeapAlloc after GC, minus the baseline).
func BenchmarkParseBindColumnPresize(b *testing.B) {
	sets := benchFileSets(b)
	pcts := []int{0, 80, 100}
	for _, set := range sets {
		for _, pct := range pcts {
			b.Run(fmt.Sprintf("%s/presize=%d", set.name, pct), func(b *testing.B) {
				prev := ast.BindColumnHintPct
				ast.BindColumnHintPct = pct
				defer func() { ast.BindColumnHintPct = prev }()

				var parseNs, bindNs, cpuNs time.Duration
				files := make([]*ast.SourceFile, len(set.files))
				for b.Loop() {
					// Drop the previous iteration's trees before parsing again so
					// peak heap stays at one file set; the GC is kept out of the
					// timing.
					b.StopTimer()
					clear(files)
					runtime.GC()
					b.StartTimer()
					c0 := cpuTime()
					t0 := time.Now()
					for i, f := range set.files {
						files[i] = parser.ParseSourceFile(f.opts, f.text, f.kind)
					}
					t1 := time.Now()
					for _, sf := range files {
						binder.BindSourceFile(sf)
					}
					parseNs += t1.Sub(t0)
					bindNs += time.Since(t1)
					cpuNs += cpuTime() - c0
				}
				b.ReportMetric(float64(parseNs.Milliseconds())/float64(b.N), "parse-ms/op")
				b.ReportMetric(float64(bindNs.Milliseconds())/float64(b.N), "bind-ms/op")
				// Process user+sys CPU over the timed window: unlike ns/op it
				// counts background GC mark work running on other cores.
				b.ReportMetric(float64(cpuNs.Milliseconds())/float64(b.N), "cpu-ms/op")

				// Live heap after bind, outside the timed loop: the files from the
				// last iteration are still referenced by files.
				b.StopTimer()
				var before, after runtime.MemStats
				files = make([]*ast.SourceFile, len(set.files))
				runtime.GC()
				runtime.GC()
				runtime.ReadMemStats(&before)
				for i, f := range set.files {
					files[i] = parser.ParseSourceFile(f.opts, f.text, f.kind)
				}
				for _, sf := range files {
					binder.BindSourceFile(sf)
				}
				runtime.GC()
				runtime.GC()
				runtime.ReadMemStats(&after)
				nodes := 0
				for _, sf := range files {
					nodes += sf.ParseStore().Len()
				}
				b.ReportMetric(float64(nodes), "nodes")
				b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc)/(1<<20), "live-MB")
				clear(files)
			})
		}
	}
}

func cpuTime() time.Duration {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

type benchFile struct {
	opts ast.SourceFileParseOptions
	text string
	kind core.ScriptKind
}

type benchFileSet struct {
	name  string
	files []benchFile
}

func benchFileOf(fileName, text string) benchFile {
	fileName = tspath.GetNormalizedAbsolutePath(fileName, "/")
	return benchFile{
		opts: ast.SourceFileParseOptions{
			FileName: fileName,
			Path:     tspath.ToPath(fileName, "/", osvfs.FS().UseCaseSensitiveFileNames()),
		},
		text: text,
		kind: core.GetScriptKindFromFileName(fileName),
	}
}

func benchFileSets(b *testing.B) []*benchFileSet {
	var sets []*benchFileSet
	for _, fx := range fixtures.BenchFixtures {
		if fx.Name() != "checker.ts" && fx.Name() != "dom.generated.d.ts" {
			continue
		}
		fx.SkipIfNotExist(b)
		sets = append(sets, &benchFileSet{name: fx.Name(), files: []benchFile{benchFileOf(fx.Path(), fx.ReadFile(b))}})
	}
	for entry := range strings.SplitSeq(os.Getenv("TSGO_BENCH_FILELIST"), ",") {
		name, listPath, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		set := &benchFileSet{name: name}
		lf, err := os.Open(listPath)
		if err != nil {
			b.Fatalf("TSGO_BENCH_FILELIST %s: %v", name, err)
		}
		sc := bufio.NewScanner(lf)
		for sc.Scan() {
			p := strings.TrimSpace(sc.Text())
			if p == "" || !filepath.IsAbs(p) {
				continue
			}
			text, err := os.ReadFile(p)
			if err != nil {
				b.Fatalf("read %s: %v", p, err)
			}
			set.files = append(set.files, benchFileOf(p, string(text)))
		}
		lf.Close()
		sets = append(sets, set)
	}
	return sets
}
