package parser

import (
	"os"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"gotest.tools/v3/assert"
)

func TestNativeStatementProducerAcceptsSubset(t *testing.T) {
	t.Parallel()
	opts := ast.SourceFileParseOptions{FileName: "/mod.ts", Path: "/mod.ts"}
	sources := []string{
		"const value: number = 1;\n",
		"export function add(a: number, b: number): number { return a + b; }\n",
		"if (ready) { run(); } else { wait(); }\n",
		"type Id = string | number;\n",
		"type.debugName = name;\n",
		"interface Point { x: number; y?: number; distance(): number; }\n",
		"class Box<T> { value: T; constructor(value: T) { this.value = value; } }\n",
		"import { read } from \"./io\";\nexport { read as load };\n",
		"enum Color { Red = 1, Green }\n",
		"const enum ReferenceHint { Unspecified, Identifier }\n",
		"const enum IterationUse { AllowsSyncIterablesFlag = 1 << 0 }\n",
		"/** @internal */ export function tagged() { return 1; }\n",
		"namespace Util { export const n = 1; }\n",
		"namespace = host;\n",
		"x = y + 1;\n",
		"const ids = items.map(item => item.id);\n",
		"const run = (arg: () => void) => { arg(); };\n",
		"const api = { add(a: number, b: number) { return a + b; }, get n(): number { return 1; } };\n",
		"for (const x of xs) { use(x); }\n",
		"const tuple = [1, 2] as const;\n",
		"function isSym(symbol: object): symbol is Symbol { return true; }\n",
		"const pick = (d): d is Tag => true;\n",
		"function* gen() { yield 1; }\n",
		"const { a } = obj;\n",
		"for (const [left, right] of pairs) { use(left, right); }\n",
		"try { run(); } catch (err) { throw err; } finally { done(); }\n",
	}
	for _, sourceText := range sources {
		p := getParser()
		p.initializeState(opts, sourceText, core.ScriptKindTS)
		p.nextToken()
		created := 0
		factory := ast.NewFactory(ast.FactoryHooks{OnCreate: func(ast.Handle) { created++ }})
		root, ok := p.tryParseSourceHandle(factory)
		assert.Assert(t, ok, sourceText)
		assert.Equal(t, ast.KindSourceFile, root.Kind(), sourceText)
		assert.Equal(t, factory.Store().Len(), created, sourceText)
		assert.Equal(t, 0, len(p.diagnostics), sourceText)
		file, refs, _ := ast.MaterializeSourceFile(root, opts, sourceText)
		assert.Assert(t, file != nil, sourceText)
		assert.Assert(t, len(refs) > 0, sourceText)
		putParser(p)
	}
}

func TestNativeStatementProducerRejectsUnsupported(t *testing.T) {
	t.Parallel()
	opts := ast.SourceFileParseOptions{FileName: "/mod.ts", Path: "/mod.ts"}
	for _, sourceText := range []string{
		"@dec class C {}\n",
	} {
		p := getParser()
		p.initializeState(opts, sourceText, core.ScriptKindTS)
		p.nextToken()
		factory := ast.NewFactory(ast.FactoryHooks{})
		_, ok := p.tryParseSourceHandle(factory)
		assert.Assert(t, !ok, sourceText)
		assert.Equal(t, 0, len(p.diagnostics), sourceText)
		putParser(p)
	}
}

func TestParseSourceFileUsesNativeStatementPath(t *testing.T) {
	t.Parallel()
	opts := ast.SourceFileParseOptions{FileName: "/mod.ts", Path: "/mod.ts"}
	sourceText := "export function hello(name: string): string {\n  return \"Hello, \" + name;\n}\n"
	file := ParseSourceFile(opts, sourceText, core.ScriptKindTS)
	assert.Assert(t, file != nil)
	assert.Assert(t, file.ParseStore() != nil)
	assert.Equal(t, 1, len(file.Statements.Nodes))
	assert.Equal(t, ast.KindFunctionDeclaration, file.Statements.Nodes[0].Kind)
	assert.Equal(t, 0, len(file.Diagnostics()))
}

func TestParseSourceFileUsesNativeStatementPathForJavaScript(t *testing.T) {
	t.Parallel()
	opts := ast.SourceFileParseOptions{FileName: "/mod.js", Path: "/mod.js"}
	sourceText := "export function hello(name) {\n  return \"Hello, \" + name;\n}\n"
	file := ParseSourceFile(opts, sourceText, core.ScriptKindJS)
	assert.Assert(t, file != nil)
	assert.Assert(t, file.ParseStore() != nil)
	assert.Equal(t, 1, len(file.Statements.Nodes))
	assert.Equal(t, ast.KindFunctionDeclaration, file.Statements.Nodes[0].Kind)
	assert.Equal(t, 0, len(file.Diagnostics()))
}

func TestNativeExportTypeAliasShape(t *testing.T) {
	opts := ast.SourceFileParseOptions{FileName: "/main.ts", Path: "/main.ts"}
	file := ParseSourceFile(opts, "export type x = any;\n", core.ScriptKindTS)
	assert.Assert(t, file != nil)
	assert.Equal(t, 1, len(file.Statements.Nodes))
	stmt := file.Statements.Nodes[0]
	t.Logf("kind=%v modifierFlags=%v external=%v", stmt.Kind, stmt.ModifierFlags(), file.ExternalModuleIndicator != nil)
	for i, m := range stmt.ModifierNodes() {
		t.Logf("mod[%d]=%v", i, m.Kind)
	}
	assert.Equal(t, ast.KindTypeAliasDeclaration, stmt.Kind)
	assert.Assert(t, stmt.ModifierFlags()&ast.ModifierFlagsExport != 0)
	assert.Assert(t, file.ExternalModuleIndicator != nil)
	alias := stmt.AsTypeAliasDeclaration()
	assert.Equal(t, ast.KindAnyKeyword, alias.Type.Kind)
	assert.Equal(t, "x", alias.Name().Text())
	binder.BindSourceFile(file)
	assert.Assert(t, file.Symbol != nil)
	if file.Symbol.Exports != nil {
		for name, sym := range file.Symbol.Exports {
			t.Logf("export %q flags=%v", name, sym.Flags)
		}
	} else {
		t.Log("exports table is nil")
	}
	_, hasX := file.Symbol.Exports["x"]
	assert.Assert(t, hasX, "module exports should contain x")
}

func TestNativeReexportModuleSpecifierText(t *testing.T) {
	opts := ast.SourceFileParseOptions{FileName: "/main.ts", Path: "/main.ts"}
	file := ParseSourceFile(opts, "export { x } from \"other\";\n", core.ScriptKindTS)
	stmt := file.Statements.Nodes[0]
	assert.Equal(t, ast.KindExportDeclaration, stmt.Kind)
	assert.Assert(t, stmt.Modifiers() == nil || len(stmt.ModifierNodes()) == 0, "export { } must not treat export as a modifier")
	spec := stmt.AsExportDeclaration().ModuleSpecifier
	assert.Assert(t, spec != nil)
	t.Logf("specifier kind=%v text=%q", spec.Kind, spec.Text())
	assert.Equal(t, "other", spec.Text())
	assert.Assert(t, len(file.Imports()) > 0, "external module references should be collected")
	t.Logf("imports[0] text=%q", file.Imports()[0].Text())
	clause := stmt.AsExportDeclaration().ExportClause
	assert.Assert(t, clause != nil)
	el := clause.AsNamedExports().Elements.Nodes[0]
	t.Logf("specifier parent=%v grandparent=%v", el.Parent.Kind, el.Parent.Parent.Kind)
	sf := ast.GetSourceFileOfNode(el)
	assert.Assert(t, sf != nil, "export specifier must reach SourceFile")
	assert.Equal(t, file, sf)
}

func TestCheckerTsNativeRejectSite(t *testing.T) {
	text, err := os.ReadFile("../../testdata/fixtures/compiler/checker.ts")
	assert.NilError(t, err)
	src := string(text)
	opts := ast.SourceFileParseOptions{FileName: "/checker.ts", Path: "/checker.ts"}
	p := getParser()
	defer putParser(p)
	p.initializeState(opts, src, core.ScriptKindTS)
	p.nextToken()
	created := 0
	factory := ast.NewFactory(ast.FactoryHooks{OnCreate: func(ast.Handle) { created++ }})
	root, ok := p.parseSourceHandle(factory)
	if !ok {
		pos := p.nodePos()
		if pos < 0 {
			pos = 0
		}
		end := pos + 80
		if end > len(src) {
			end = len(src)
		}
		start := pos - 40
		if start < 0 {
			start = 0
		}
		t.Fatalf("native reject token=%v pos=%d around=%q", p.token, pos, src[start:end])
	}
	assert.Equal(t, ast.KindSourceFile, root.Kind())
	assert.Equal(t, factory.Store().Len(), created)
	assert.Equal(t, 0, len(p.diagnostics))
	file, refs, stats := ast.MaterializeSourceFile(root, opts, src)
	assert.Assert(t, file != nil)
	assert.Assert(t, stats.NodeCount > 0)
	assert.Assert(t, len(refs) > 0)

	prod := ParseSourceFile(opts, src, core.ScriptKindTS)
	assert.Assert(t, prod != nil)
	assert.Assert(t, prod.ParseStore() != nil)
	assert.Equal(t, created, prod.ParseStore().Len())
	assert.Equal(t, stats.NodeCount, prod.NodeCount)
	t.Logf("checker.ts native path storeLen=%d created=%d nodeCount=%d statements=%d", created, created, stats.NodeCount, len(prod.Statements.Nodes))
}
