package binder

import (
	"fmt"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
)

func exportHeavySource(n int) string {
	var b strings.Builder
	b.Grow(n * 80)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "export const e%d = %d;\n", i, i)
		fmt.Fprintf(&b, "export function f%d(x: number) { return x + %d; }\n", i, i)
	}
	return b.String()
}

func BenchmarkBindExportHeavy(b *testing.B) {
	const decls = 2000
	sourceText := exportHeavySource(decls)
	opts := ast.SourceFileParseOptions{FileName: "/exports.ts", Path: "/exports.ts"}

	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		file := parser.ParseSourceFile(opts, sourceText, core.ScriptKindTS)
		b.StartTimer()

		BindSourceFile(file)
	}
}
