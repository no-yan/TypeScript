package parser

import (
	"runtime"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"gotest.tools/v3/assert"
)

const nativeExpressionBenchmarkText = `client?.users[index + 1].map(
	{name: "Ada", scores: [1, 2, 3], active: true},
	...extras,
) ? total * rate + fee : fallback;`

const expandedNativeExpressionBenchmarkText = "new Registry<Map<string, Entry[]>>({\n" +
	"\t[/^user-/.source]: tag<string>`hello ${++index}: ${items[index++]?.name}`,\n" +
	"}).build();"

func TestNativeExpressionProducerAcceptsCompleteSubset(t *testing.T) {
	t.Parallel()
	opts := ast.SourceFileParseOptions{FileName: "/expression.ts", Path: "/expression.ts"}
	for _, sourceText := range []string{nativeExpressionBenchmarkText, expandedNativeExpressionBenchmarkText} {
		p := getParser()
		p.initializeState(opts, sourceText, core.ScriptKindTS)
		p.nextToken()

		created := 0
		factory := ast.NewFactory(ast.FactoryHooks{OnCreate: func(ast.Handle) { created++ }})
		root, ok := p.tryParseExpressionSourceHandle(factory)
		assert.Assert(t, ok, sourceText)
		assert.Equal(t, ast.KindSourceFile, root.Kind())
		assert.Equal(t, factory.Store().Len(), created)
		assert.Equal(t, 0, len(p.diagnostics))
		putParser(p)
	}
}

func TestNativeExpressionProducerRejectsMalformedAndUnsupportedSyntax(t *testing.T) {
	t.Parallel()
	opts := ast.SourceFileParseOptions{FileName: "/expression.ts", Path: "/expression.ts"}
	for _, sourceText := range []string{
		`value ? yes`,
		`target.`,
		`call(, value)`,
		`const value = 1`,
		`a<T,>(b)`,
		"optional?.`template`",
		`/** @type {string} */`,
	} {
		p := getParser()
		p.initializeState(opts, sourceText, core.ScriptKindTS)
		p.nextToken()
		factory := ast.NewFactory(ast.FactoryHooks{})
		_, ok := p.tryParseExpressionSourceHandle(factory)
		assert.Assert(t, !ok, sourceText)
		assert.Equal(t, 0, len(p.diagnostics), sourceText)
		putParser(p)
	}
}

func BenchmarkParseExpressionHandleNative(b *testing.B) {
	opts := ast.SourceFileParseOptions{FileName: "/expression.ts", Path: "/expression.ts"}
	sourceText := expandedNativeExpressionBenchmarkText

	b.Run("HandleOnly", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			p := getParser()
			p.initializeState(opts, sourceText, core.ScriptKindTS)
			p.nextToken()
			factory := ast.NewFactoryHint(ast.FactoryHooks{}, 64)
			root, ok := p.tryParseExpressionSourceHandle(factory)
			if !ok {
				b.Fatal("native expression benchmark input was not accepted")
			}
			factory.Seal()
			putParser(p)
			runtime.KeepAlive(root)
		}
	})

	b.Run("WithPointerMaterialization", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			p := getParser()
			p.initializeState(opts, sourceText, core.ScriptKindTS)
			p.nextToken()
			factory := ast.NewFactoryHint(ast.FactoryHooks{}, 64)
			root, ok := p.tryParseExpressionSourceHandle(factory)
			if !ok {
				b.Fatal("native expression benchmark input was not accepted")
			}
			file, refs, _ := ast.MaterializeSourceFile(root, opts, sourceText)
			factory.Seal()
			putParser(p)
			runtime.KeepAlive(file)
			runtime.KeepAlive(refs)
		}
	})

	b.Run("CurrentDualWrite", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			p := getParser()
			p.initializeState(opts, sourceText, core.ScriptKindTS)
			p.nextToken()
			factory := ast.NewFactoryHint(ast.FactoryHooks{}, 64)
			p.factory.AttachStore(factory.Store())
			file := p.parseSourceFileWorker()
			factory.Seal()
			putParser(p)
			runtime.KeepAlive(file)
		}
	})
}
