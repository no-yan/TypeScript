// Command _e2e parses and binds every file of a project with one of the two
// ASTs, the Pointer (parser, binder) or the Store (storeparser, storebinder),
// and reports the wall, CPU, allocation and retained heap of each run.
//
// The tsc command does not reach the Store parser and binder, so this is the
// end-to-end comparison of the two front ends: the files are those of the
// Program of the project (its lib and node_modules declarations included),
// collected once with the Pointer and then dropped. Each run parses every
// file in parallel, then binds every file in parallel, as the Program does.
// Module resolution, file I/O and diagnostics are outside it.
//
// One process runs one mode, so the two modes do not share a heap; run.sh
// alternates them.
//
//	go run ./internal/storebinder/_e2e -mode store -project ~/vscode/src/tsconfig.monaco.json
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"slices"
	"syscall"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/storebinder"
	"github.com/microsoft/TypeScript/tsc/internal/storeparser"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/osvfs"
)

type input struct {
	opts ast.SourceFileParseOptions
	text string
	kind core.ScriptKind
}

// A front end parses one file and binds a parsed file. The results are kept
// in a slice indexed like the inputs, so both phases write disjoint slots.
type frontEnd struct {
	parse func(in input) any
	bind  func(parsed any)
	nodes func(parsed any) int
}

var frontEnds = map[string]frontEnd{
	"pointer": {
		func(in input) any { return parser.ParseSourceFile(in.opts, in.text, in.kind) },
		func(parsed any) { binder.BindSourceFile(parsed.(*ast.SourceFile)) },
		func(parsed any) int { return parsed.(*ast.SourceFile).NodeCount },
	},
	"store": {
		func(in input) any { return storeparser.ParseSourceFile(in.opts, in.text, in.kind) },
		func(parsed any) { storebinder.BindSourceFile(parsed.(*store.File)) },
		func(parsed any) int { return parsed.(*store.File).NodeCount },
	},
}

func main() {
	mode := flag.String("mode", "store", "pointer or store")
	project := flag.String("project", "", "tsconfig.json of the project")
	runs := flag.Int("runs", 5, "number of runs")
	singleThreaded := flag.Bool("singleThreaded", false, "parse and bind on one goroutine")
	cpuprofile := flag.String("cpuprofile", "", "write a CPU profile of the runs")
	gap := flag.Duration("gap", 0, "idle time before each run, to find the runs in a trace")
	flag.Parse()

	fe, ok := frontEnds[*mode]
	if !ok || *project == "" {
		flag.Usage()
		os.Exit(2)
	}
	inputs := collect(*project)

	var jsFiles int
	for _, in := range inputs {
		if in.kind == core.ScriptKindJS || in.kind == core.ScriptKindJSX {
			jsFiles++
		}
	}
	fmt.Printf("# mode=%s files=%d js=%d singleThreaded=%v GOGC=%s GOMAXPROCS=%d\n",
		*mode, len(inputs), jsFiles, *singleThreaded, os.Getenv("GOGC"), runtime.GOMAXPROCS(0))
	if jsFiles > 0 {
		fmt.Println("# warning: the Store parser does not parse the JSDoc of JS files, which the Pointer parser does")
	}
	fmt.Println("mode\trun\tparse_ms\tbind_ms\ttotal_ms\tuser_ms\tsys_ms\talloc_MB\tmallocs_k\tgcs\tretained_MB\tscan_MB\tobjects_k\tgc_ms\tgc_cpu_ms\tnodes")

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			fatal(err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			fatal(err)
		}
		defer pprof.StopCPUProfile()
	}
	var parseMs, bindMs, totalMs, userMs, allocMB, retainedMB []float64
	for run := range *runs {
		r := measure(fe, inputs, *singleThreaded, *gap)
		// The offsets from the start of the process, to select the phases in a
		// trace of the whole process.
		fmt.Fprintf(os.Stderr, "# run %d: parse %.3f-%.3f s, bind %.3f-%.3f s\n", run,
			r.start.Sub(processStart).Seconds(), r.parseDone.Sub(processStart).Seconds(),
			r.parseDone.Sub(processStart).Seconds(), r.bindDone.Sub(processStart).Seconds())
		fmt.Printf("%s\t%d\t%.1f\t%.1f\t%.1f\t%.0f\t%.0f\t%.0f\t%.0f\t%d\t%.0f\t%.0f\t%.0f\t%.1f\t%.1f\t%d\n",
			*mode, run, r.parseMs, r.bindMs, r.parseMs+r.bindMs, r.userMs, r.sysMs,
			r.allocMB, r.mallocsK, r.gcs, r.retainedMB, r.scanMB, r.objectsK, r.gcMs, r.gcCPUMs, r.nodes)
		parseMs = append(parseMs, r.parseMs)
		bindMs = append(bindMs, r.bindMs)
		totalMs = append(totalMs, r.parseMs+r.bindMs)
		userMs = append(userMs, r.userMs)
		allocMB = append(allocMB, r.allocMB)
		retainedMB = append(retainedMB, r.retainedMB)
	}
	fmt.Printf("# median %s: parse_ms=%.1f bind_ms=%.1f total_ms=%.1f user_ms=%.0f alloc_MB=%.0f retained_MB=%.0f\n",
		*mode, median(parseMs), median(bindMs), median(totalMs), median(userMs), median(allocMB), median(retainedMB))
}

// collect returns the files of the Program of the project, in its order.
func collect(project string) []input {
	configFileName, err := filepath.Abs(project)
	if err != nil {
		fatal(err)
	}
	configFileName = tspath.NormalizeSlashes(configFileName)
	root := tspath.GetDirectoryPath(configFileName)
	fs := bundled.WrapFS(osvfs.FS())
	host := compiler.NewCompilerHost(root, fs, bundled.LibPath(), nil, nil, nil)
	config, errors := tsoptions.GetParsedCommandLineOfConfigFile(configFileName, &core.CompilerOptions{}, nil, host, nil)
	if config == nil || len(errors) != 0 {
		fatal(fmt.Errorf("parse %s: %d diagnostics", configFileName, len(errors)))
	}
	program := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host})
	files := program.SourceFiles()
	inputs := make([]input, len(files))
	for i, f := range files {
		inputs[i] = input{f.ParseOptions(), f.Text(), core.EnsureScriptKindFromFileName(f.FileName())}
	}
	return inputs
}

var processStart = time.Now()

type result struct {
	start, parseDone, bindDone      time.Time
	parseMs, bindMs, userMs, sysMs  float64
	allocMB, mallocsK, retainedMB   float64
	scanMB, objectsK, gcMs, gcCPUMs float64
	gcs                             uint32
	nodes                           int
}

func measure(fe frontEnd, inputs []input, singleThreaded bool, gap time.Duration) result {
	// Twice: the first collection only moves the pooled scratch of the
	// previous run (sync.Pool) to the victim cache.
	runtime.GC()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	gcBefore := readGCMetrics()
	ruBefore := rusage()

	parsed := make([]any, len(inputs))
	// After the collections, so the end of the idle time in a trace is the
	// start of the parse.
	time.Sleep(gap)
	start := time.Now()
	wg := core.NewWorkGroup(singleThreaded)
	for i, in := range inputs {
		wg.Queue(func() { parsed[i] = fe.parse(in) })
	}
	wg.RunAndWait()
	parseDone := time.Now()
	wg = core.NewWorkGroup(singleThreaded)
	for _, p := range parsed {
		wg.Queue(func() { fe.bind(p) })
	}
	wg.RunAndWait()
	bindDone := time.Now()

	ruAfter := rusage()
	runtime.ReadMemStats(&after)
	r := result{
		start:     start,
		parseDone: parseDone,
		bindDone:  bindDone,
		parseMs:   ms(parseDone.Sub(start)),
		bindMs:    ms(bindDone.Sub(parseDone)),
		userMs:    ms(time.Duration(ruAfter.Utime.Nano() - ruBefore.Utime.Nano())),
		sysMs:     ms(time.Duration(ruAfter.Stime.Nano() - ruBefore.Stime.Nano())),
		allocMB:   float64(after.TotalAlloc-before.TotalAlloc) / 1e6,
		mallocsK:  float64(after.Mallocs-before.Mallocs) / 1e3,
		gcs:       after.NumGC - before.NumGC,
	}
	for _, p := range parsed {
		r.nodes += fe.nodes(p)
	}
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	r.retainedMB = (float64(after.HeapAlloc) - float64(before.HeapAlloc)) / 1e6
	// One more collection with the results live: what they cost each cycle
	// of the collector, which is what every collection of a check would pay.
	live := readGCMetrics()
	gcStart := time.Now()
	runtime.GC()
	r.gcMs = ms(time.Since(gcStart))
	final := readGCMetrics()
	r.gcCPUMs = (final.cpu - live.cpu) * 1e3
	r.scanMB = float64(final.scanHeap-gcBefore.scanHeap) / 1e6
	r.objectsK = float64(final.objects-gcBefore.objects) / 1e3
	runtime.KeepAlive(parsed)
	return r
}

type gcMetrics struct {
	scanHeap, objects int64   // as of the last collection
	cpu               float64 // seconds, cumulative
}

// readGCMetrics reads the scannable heap (the bytes of the objects that hold
// pointers, which the collector has to scan), the objects of the heap and
// the CPU time the collector has spent.
func readGCMetrics() gcMetrics {
	samples := []metrics.Sample{
		{Name: "/gc/scan/heap:bytes"},
		{Name: "/gc/heap/objects:objects"},
		{Name: "/cpu/classes/gc/total:cpu-seconds"},
	}
	metrics.Read(samples)
	return gcMetrics{int64(samples[0].Value.Uint64()), int64(samples[1].Value.Uint64()), samples[2].Value.Float64()}
}

func rusage() syscall.Rusage {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		fatal(err)
	}
	return ru
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1e3 }

func median(xs []float64) float64 {
	s := slices.Clone(xs)
	slices.Sort(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
