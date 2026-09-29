package storeparser

import (
	"runtime"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/filefixture"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/kperf"
)

// The two parsers on the same input. Each returns its result, which the
// caller keeps in a sink so the parse is not dead.
type parse struct {
	name string
	run  func(opts ast.SourceFileParseOptions, text string, kind core.ScriptKind) any
}

var parses = []parse{
	{"pointer", func(opts ast.SourceFileParseOptions, text string, kind core.ScriptKind) any {
		return parser.ParseSourceFile(opts, text, kind)
	}},
	{"store", func(opts ast.SourceFileParseOptions, text string, kind core.ScriptKind) any {
		return ParseSourceFile(opts, text, kind)
	}},
}

// BenchmarkStoreParseV1 has the form of BenchmarkASTParseV1 in
// internal/parser/ast_benchmark_test.go, for both parsers.
func BenchmarkStoreParseV1(b *testing.B) {
	for _, fixture := range fixtures.ASTBenchFixtures {
		b.Run(fixture.Name(), func(b *testing.B) {
			opts, text, kind := fixtureInput(b, fixture)
			for _, p := range parses {
				b.Run(p.name, func(b *testing.B) {
					var sink any
					b.ReportAllocs()
					for b.Loop() {
						sink = p.run(opts, text, kind)
					}
					if sink == nil {
						b.Fatal("parse returned nil")
					}
				})
			}
		})
	}
}

// BenchmarkStoreParseKPCV1 has the form of BenchmarkASTParseKPCV1: checker.ts
// as pointer and store, and dom.generated.d.ts under dom (7d design 2.2:
// checker.ts has almost no JSDoc, dom has the lazy TS JSDoc that 7d-1 moves).
func BenchmarkStoreParseKPCV1(b *testing.B) {
	parseKPC(b, fixtures.ASTBenchCheckerFixture)
	b.Run("dom", func(b *testing.B) {
		for _, fixture := range fixtures.ASTBenchFixtures {
			if fixture.Name() == "dom.generated.d.ts" {
				parseKPC(b, fixture)
				return
			}
		}
		b.Fatal("no dom.generated.d.ts in the AST bench fixtures")
	})
}

func parseKPC(b *testing.B, fixture filefixture.Fixture) {
	opts, text, kind := fixtureInput(b, fixture)
	for _, p := range parses {
		b.Run(p.name, func(b *testing.B) {
			session := kperf.Start(b)
			defer session.Stop()
			var sink any
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				session.Measure(func() { sink = p.run(opts, text, kind) })
			}
			b.StopTimer()
			if sink == nil {
				b.Fatal("parse returned nil")
			}
			session.Report()
		})
	}
}

// BenchmarkStoreParseRetainedV1 parses the fixtures part of the corpus (the
// -short set), keeps every result, and measures what the results cost the
// collector: the heap they retain per node, and the time of one collection
// while they are live. This is the first measurement of the design's bet
// that the Store is cheap to keep (store-ast-design-20260922.md 1).
func BenchmarkStoreParseRetainedV1(b *testing.B) {
	files := corpusFixtures(b)
	for _, p := range parses {
		b.Run(p.name, func(b *testing.B) {
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
					r := p.run(opts, text, kind)
					results = append(results, r)
					switch r := r.(type) {
					case *ast.SourceFile:
						nodes += r.NodeCount
					case *store.File:
						nodes += r.NodeCount
					}
				}
				// Twice: the first collection only moves the pooled parser's
				// scratch (parserPool, a sync.Pool) to the victim cache.
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
}

// corpusFixtures is the -short corpus: every parsable file under testdata/fixtures.
func corpusFixtures(tb testing.TB) []corpusFile {
	tb.Helper()
	return corpusUnder(tb, false)
}

// The lazy JSDoc of dom.generated.d.ts (7d-1 instructions 7): every host with
// HasJSDoc of a freshly parsed file is asked for its JSDoc once, which parses
// it. Only the JSDoc reads are measured; the parse and the host walk are not.
// This is the part of the Check that the lazy JSDoc costs, before 7d-2.
type lazyJSDoc struct {
	name string
	// prepare parses a new file and returns the reads of all its hosts.
	prepare func(opts ast.SourceFileParseOptions, text string, kind core.ScriptKind) (read func() int)
}

var lazyJSDocs = []lazyJSDoc{
	{"pointer", func(opts ast.SourceFileParseOptions, text string, kind core.ScriptKind) func() int {
		file := parser.ParseSourceFile(opts, text, kind)
		var hosts []*ast.Node
		var visit ast.Visitor
		visit = func(n *ast.Node) bool {
			if n.Flags&ast.NodeFlagsHasJSDoc != 0 {
				hosts = append(hosts, n)
			}
			return n.ForEachChild(visit)
		}
		visit(file.AsNode())
		return func() int {
			n := 0
			for _, host := range hosts {
				n += len(host.JSDoc(file))
			}
			return n
		}
	}},
	{"store", func(opts ast.SourceFileParseOptions, text string, kind core.ScriptKind) func() int {
		file := ParseSourceFile(opts, text, kind)
		var hosts []store.Node
		var visit store.Visitor
		visit = func(n store.Node) bool {
			if n.Flags()&ast.NodeFlagsHasJSDoc != 0 {
				hosts = append(hosts, n)
			}
			return n.ForEachChild(visit)
		}
		visit(file.Root())
		return func() int {
			n := 0
			for _, host := range hosts {
				n += len(host.JSDoc(file))
			}
			return n
		}
	}},
}

func domFixture(b *testing.B) filefixture.Fixture {
	for _, fixture := range fixtures.ASTBenchFixtures {
		if fixture.Name() == "dom.generated.d.ts" {
			return fixture
		}
	}
	b.Fatal("no dom.generated.d.ts in the AST bench fixtures")
	return nil
}

func BenchmarkStoreLazyJSDocV1(b *testing.B) {
	opts, text, kind := fixtureInput(b, domFixture(b))
	for _, l := range lazyJSDocs {
		b.Run(l.name, func(b *testing.B) {
			comments := 0
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				read := l.prepare(opts, text, kind)
				b.StartTimer()
				comments = read()
			}
			b.ReportMetric(float64(comments), "comments")
		})
	}
}

func BenchmarkStoreLazyJSDocKPCV1(b *testing.B) {
	opts, text, kind := fixtureInput(b, domFixture(b))
	for _, l := range lazyJSDocs {
		b.Run(l.name, func(b *testing.B) {
			session := kperf.Start(b)
			defer session.Stop()
			comments := 0
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				read := l.prepare(opts, text, kind)
				b.StartTimer()
				session.Measure(func() { comments = read() })
			}
			b.StopTimer()
			if comments == 0 {
				b.Fatal("no JSDoc read")
			}
			session.Report()
		})
	}
}
