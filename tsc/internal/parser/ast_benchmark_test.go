package parser_test

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/kperf"
)

func BenchmarkASTParseV1(b *testing.B) {
	for _, fixture := range fixtures.ASTBenchFixtures {
		b.Run(fixture.Name(), func(b *testing.B) {
			opts, source, scriptKind := fixtures.ASTBenchParseInput(b, fixture)
			var sink *ast.SourceFile
			b.ReportAllocs()
			for b.Loop() {
				sink = parser.ParseSourceFile(opts, source, scriptKind)
			}
			if sink == nil || sink.AsNode().Kind != ast.KindSourceFile {
				b.Fatal("parse returned no source file")
			}
		})
	}
}

func BenchmarkASTParseKPCV1(b *testing.B) {
	session := kperf.Start(b)
	defer session.Stop()
	fixture := fixtures.ASTBenchCheckerFixture
	opts, source, scriptKind := fixtures.ASTBenchParseInput(b, fixture)
	var sink *ast.SourceFile
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		session.Measure(func() { sink = parser.ParseSourceFile(opts, source, scriptKind) })
	}
	b.StopTimer()
	if sink == nil {
		b.Fatal("parse returned nil")
	}
	session.Report()
}
