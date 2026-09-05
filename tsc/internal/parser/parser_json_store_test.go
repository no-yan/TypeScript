package parser

import (
	"runtime"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

const nativeJSONBenchmarkText = `{
	"compilerOptions": {
		"strict": true,
		"target": "esnext",
		"paths": {
			"@app/*": ["src/*", "generated/*"]
		}
	},
	"include": ["src/**/*.ts", "tests/**/*.ts"],
	"exclude": ["dist", "node_modules"],
}`

func BenchmarkParseJSONHandleNativeBridge(b *testing.B) {
	opts := ast.SourceFileParseOptions{FileName: "/tsconfig.json", Path: "/tsconfig.json"}

	b.Run("HandleOnly", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			p := getParser()
			p.initializeState(opts, nativeJSONBenchmarkText, core.ScriptKindJSON)
			p.nextToken()
			factory := ast.NewFactoryHint(ast.FactoryHooks{}, 64)
			root, ok := p.tryParseJSONTextHandle(factory)
			if !ok {
				b.Fatal("native JSON benchmark input was not accepted")
			}
			factory.Seal()
			putParser(p)
			runtime.KeepAlive(root)
		}
	})

	b.Run("WithPointerBridge", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			file := ParseSourceFile(opts, nativeJSONBenchmarkText, core.ScriptKindJSON)
			runtime.KeepAlive(file)
		}
	})
}
