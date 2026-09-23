package storebinder_test

import (
	"runtime"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/storebinder"
	"github.com/microsoft/TypeScript/tsc/internal/storeparser"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/kperf"
)

// The two binders on the same input. parse is outside the timed region: the
// Pointer file binds once, so every iteration parses again. bind returns the
// bound file, which the caller keeps in a sink.
type bind struct {
	name  string
	parse func(opts ast.SourceFileParseOptions, text string, kind core.ScriptKind) any
	bind  func(parsed any)
	nodes func(parsed any) int
}

var binds = []bind{
	{
		"pointer",
		func(opts ast.SourceFileParseOptions, text string, kind core.ScriptKind) any {
			return parser.ParseSourceFile(opts, text, kind)
		},
		func(parsed any) { binder.BindSourceFile(parsed.(*ast.SourceFile)) },
		func(parsed any) int { return parsed.(*ast.SourceFile).NodeCount },
	},
	{
		"store",
		func(opts ast.SourceFileParseOptions, text string, kind core.ScriptKind) any {
			return storeparser.ParseSourceFile(opts, text, kind)
		},
		func(parsed any) { storebinder.BindSourceFile(parsed.(*store.File)) },
		func(parsed any) int { return parsed.(*store.File).NodeCount },
	},
}

// BenchmarkStoreBindV1 has the form of BenchmarkASTBindV1 in
// internal/binder/ast_benchmark_test.go, for both binders.
func BenchmarkStoreBindV1(b *testing.B) {
	for _, fixture := range fixtures.ASTBenchFixtures {
		b.Run(fixture.Name(), func(b *testing.B) {
			opts, text, kind := fixtureInput(b, fixture)
			for _, bd := range binds {
				b.Run(bd.name, func(b *testing.B) {
					b.StopTimer()
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						parsed := bd.parse(opts, text, kind)
						b.StartTimer()
						bd.bind(parsed)
						b.StopTimer()
					}
				})
			}
		})
	}
}

// The visits of the Pointer walk of checker.ts (walkBaselines in
// internal/ast/store/store_test.go): the denominator of the per-node metrics
// for both binders.
const checkerVisits = 298054

// BenchmarkStoreBindKPCV1 has the form of BenchmarkASTBindKPCV1: the parse is
// outside the measured interval, which holds only the bind.
func BenchmarkStoreBindKPCV1(b *testing.B) {
	fixture := fixtures.ASTBenchCheckerFixture
	opts, text, kind := fixtureInput(b, fixture)
	for _, bd := range binds {
		b.Run(bd.name, func(b *testing.B) {
			b.StopTimer()
			session := kperf.Start(b)
			defer session.Stop()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				parsed := bd.parse(opts, text, kind)
				b.StartTimer()
				session.Measure(func() { bd.bind(parsed) })
				b.StopTimer()
			}
			session.Report()
			totals := session.Totals()
			b.ReportMetric(float64(totals.Cycles)/float64(b.N)/checkerVisits, "cycles/node")
			b.ReportMetric(float64(totals.Instructions)/float64(b.N)/checkerVisits, "inst/node")
		})
	}
}

// BenchmarkStoreBindRetainedV1 is BenchmarkStoreParseRetainedV1 of
// internal/storeparser extended to the bind: it parses and binds every file
// of the input, keeps the results, and measures what they cost the
// collector: the heap they retain per node and the time of one collection
// while they are live. The inputs are the fixtures part of the corpus (the
// -short set) and the two benchmark fixtures alone, because the ratio moves
// with the density of symbols (store-binder-design-20260922.md 5).
func BenchmarkStoreBindRetainedV1(b *testing.B) {
	type input struct {
		name  string
		files func(b *testing.B) []corpusFile
	}
	inputs := []input{
		{"fixtures", func(b *testing.B) []corpusFile { return corpusUnder(b, false) }},
	}
	for _, fixture := range fixtures.ASTBenchFixtures {
		inputs = append(inputs, input{fixture.Name(), func(b *testing.B) []corpusFile {
			_, text, _ := fixtureInput(b, fixture)
			return []corpusFile{{"/" + fixture.Name(), text}}
		}})
	}
	for _, in := range inputs {
		b.Run(in.name, func(b *testing.B) {
			files := in.files(b)
			for _, bd := range binds {
				b.Run(bd.name, func(b *testing.B) {
					var results []any
					var nodes int
					for i := 0; i < b.N; i++ {
						results = nil
						nodes = 0
						runtime.GC()
						var before, after runtime.MemStats
						runtime.ReadMemStats(&before)
						for _, f := range files {
							opts, text, kind := sourceInput(f.name, f.text)
							parsed := bd.parse(opts, text, kind)
							bd.bind(parsed)
							results = append(results, parsed)
							nodes += bd.nodes(parsed)
						}
						// Twice: the first collection only moves the pooled parser's
						// and binder's scratch (sync.Pool) to the victim cache.
						runtime.GC()
						runtime.GC()
						runtime.ReadMemStats(&after)
						retained := float64(after.HeapAlloc) - float64(before.HeapAlloc)
						start := time.Now()
						runtime.GC()
						gc := time.Since(start)
						b.ReportMetric(retained/float64(nodes), "retained-B/node")
						b.ReportMetric(retained/1e6, "retained-MB")
						b.ReportMetric(float64(gc.Microseconds())/1e3, "gc-ms")
						b.ReportMetric(float64(nodes), "nodes")
					}
					runtime.KeepAlive(results)
				})
			}
		})
	}
}
