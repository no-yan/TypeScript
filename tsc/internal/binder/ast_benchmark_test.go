package binder

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/kperf"
)

func BenchmarkASTBindV1(b *testing.B) {
	for _, fixture := range fixtures.ASTBenchFixtures {
		b.Run(fixture.Name(), func(b *testing.B) {
			b.StopTimer()
			opts, source, scriptKind := fixtures.ASTBenchParseInput(b, fixture)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sourceFile := parser.ParseSourceFile(opts, source, scriptKind)
				b.StartTimer()
				BindSourceFile(sourceFile)
				b.StopTimer()
			}
		})
	}
}

func BenchmarkASTBindKPCV1(b *testing.B) {
	b.StopTimer()
	session := kperf.Start(b)
	defer session.Stop()
	fixture := fixtures.ASTBenchCheckerFixture
	opts, source, scriptKind := fixtures.ASTBenchParseInput(b, fixture)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		sourceFile := parser.ParseSourceFile(opts, source, scriptKind)
		b.StartTimer()
		session.Measure(func() { BindSourceFile(sourceFile) })
		b.StopTimer()
	}
	session.Report()
}

// Binder semantics are covered by the binder and conformance tests. This only
// guards against BenchmarkASTBindV1 timing a bind that did nothing.
func TestASTBenchmarkBind(t *testing.T) {
	for _, fixture := range fixtures.ASTBenchFixtures {
		t.Run(fixture.Name(), func(t *testing.T) {
			sourceFile := parser.ParseSourceFile(fixtures.ASTBenchParseInput(t, fixture))
			BindSourceFile(sourceFile)
			if len(sourceFile.AsNode().Locals()) == 0 {
				t.Fatal("bind produced no source-level locals")
			}
		})
	}
}
