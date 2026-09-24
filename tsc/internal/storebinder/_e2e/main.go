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
	fmt.Println("mode\trun\tparse_ms\tbind_ms\ttotal_ms\tuser_ms\tsys_ms\talloc_MB\tmallocs_k\tgcs\tretained_MB\tnodes")

	var parseMs, bindMs, totalMs, userMs, allocMB, retainedMB []float64
	for run := range *runs {
		r := measure(fe, inputs, *singleThreaded)
		fmt.Printf("%s\t%d\t%.1f\t%.1f\t%.1f\t%.0f\t%.0f\t%.0f\t%.0f\t%d\t%.0f\t%d\n",
			*mode, run, r.parseMs, r.bindMs, r.parseMs+r.bindMs, r.userMs, r.sysMs,
			r.allocMB, r.mallocsK, r.gcs, r.retainedMB, r.nodes)
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

type result struct {
	parseMs, bindMs, userMs, sysMs float64
	allocMB, mallocsK, retainedMB  float64
	gcs                            uint32
	nodes                          int
}

func measure(fe frontEnd, inputs []input, singleThreaded bool) result {
	// Twice: the first collection only moves the pooled scratch of the
	// previous run (sync.Pool) to the victim cache.
	runtime.GC()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	ruBefore := rusage()

	parsed := make([]any, len(inputs))
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
		parseMs:  ms(parseDone.Sub(start)),
		bindMs:   ms(bindDone.Sub(parseDone)),
		userMs:   ms(time.Duration(ruAfter.Utime.Nano() - ruBefore.Utime.Nano())),
		sysMs:    ms(time.Duration(ruAfter.Stime.Nano() - ruBefore.Stime.Nano())),
		allocMB:  float64(after.TotalAlloc-before.TotalAlloc) / 1e6,
		mallocsK: float64(after.Mallocs-before.Mallocs) / 1e3,
		gcs:      after.NumGC - before.NumGC,
	}
	for _, p := range parsed {
		r.nodes += fe.nodes(p)
	}
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	r.retainedMB = (float64(after.HeapAlloc) - float64(before.HeapAlloc)) / 1e6
	runtime.KeepAlive(parsed)
	return r
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
