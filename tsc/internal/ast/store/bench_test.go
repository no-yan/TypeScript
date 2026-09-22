package store_test

import (
	"runtime"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/convert"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/kperf"
)

// pointerWalker is astWalker from internal/ast/ast_benchmark_test.go: the
// callback is bound once, and a visit only counts.
type pointerWalker struct {
	visit  ast.Visitor
	visits uint64
}

func newPointerWalker() *pointerWalker {
	w := &pointerWalker{}
	w.visit = func(node *ast.Node) bool {
		w.walk(node)
		return false
	}
	return w
}

func (w *pointerWalker) walk(node *ast.Node) {
	if node == nil {
		return
	}
	w.visits++
	node.ForEachChild(w.visit)
}

func (w *pointerWalker) run(root *ast.Node) uint64 {
	w.visits = 0
	w.walk(root)
	return w.visits
}

// storeWalker has no nil check: ForEachChild never passes the nil node.
type storeWalker struct {
	visit  store.Visitor
	visits uint64
}

func newStoreWalker() *storeWalker {
	w := &storeWalker{}
	w.visit = func(n store.Node) bool {
		w.walk(n)
		return false
	}
	return w
}

func (w *storeWalker) walk(n store.Node) {
	w.visits++
	n.ForEachChild(w.visit)
}

func (w *storeWalker) run(root store.Node) uint64 {
	w.visits = 0
	w.walk(root)
	return w.visits
}

type walk struct {
	name string
	run  func() uint64
}

func walks(file *ast.SourceFile) []walk {
	root, storeRoot := file.AsNode(), convert.Convert(file, 0).Root()
	pointer, stored := newPointerWalker(), newStoreWalker()
	return []walk{
		{"pointer", func() uint64 { return pointer.run(root) }},
		{"store", func() uint64 { return stored.run(storeRoot) }},
	}
}

func BenchmarkStoreWalkV1(b *testing.B) {
	for _, fixture := range fixtures.ASTBenchFixtures {
		b.Run(fixture.Name(), func(b *testing.B) {
			want := walkBaselines[fixture.Name()].visits
			for _, w := range walks(parseFixture(b, fixture)) {
				b.Run(w.name, func(b *testing.B) {
					b.ReportAllocs()
					runtime.GC()
					w.run()
					var got uint64
					for b.Loop() {
						got = w.run()
					}
					if got != want {
						b.Fatalf("visits = %d, want %d", got, want)
					}
					b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(want), "ns/visit")
				})
			}
		})
	}
}

// BenchmarkStoreWalkKPCV1 has the form of BenchmarkASTWalkKPCV1.
func BenchmarkStoreWalkKPCV1(b *testing.B) {
	fixture := fixtures.ASTBenchCheckerFixture
	want := walkBaselines[fixture.Name()].visits
	for _, w := range walks(parseFixture(b, fixture)) {
		b.Run(w.name, func(b *testing.B) {
			session := kperf.Start(b)
			defer session.Stop()
			runtime.GC()
			w.run()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var got uint64
				session.Measure(func() { got = w.run() })
				if got != want {
					b.Fatalf("visits = %d, want %d", got, want)
				}
			}
			b.StopTimer()
			session.Report()
		})
	}
}
