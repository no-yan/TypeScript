package storebinder_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/storetest"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/repo"
	"github.com/microsoft/TypeScript/tsc/internal/storebinder"
	"github.com/microsoft/TypeScript/tsc/internal/storeparser"
	"github.com/microsoft/TypeScript/tsc/internal/testrunner"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/filefixture"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

func fixtureInput(tb testing.TB, fixture filefixture.Fixture) (ast.SourceFileParseOptions, string, core.ScriptKind) {
	tb.Helper()
	return fixtures.ASTBenchParseInput(tb, fixture)
}

func sourceInput(name, source string) (ast.SourceFileParseOptions, string, core.ScriptKind) {
	return ast.SourceFileParseOptions{FileName: name, Path: tspath.Path(name)}, source, core.GetScriptKindFromFileName(name)
}

// bindBoth parses and binds the same input with the Pointer parser and binder
// and with the Store parser and binder.
func bindBoth(opts ast.SourceFileParseOptions, text string, kind core.ScriptKind) (*ast.SourceFile, *store.File) {
	pointer := parser.ParseSourceFile(opts, text, kind)
	binder.BindSourceFile(pointer)
	file := storeparser.ParseSourceFile(opts, text, kind)
	storebinder.BindSourceFile(file)
	return pointer, file
}

// options are the equivalence options of the Store parser's tests, with the
// error propagation flag added for JS files: the Pointer parser's JSDoc
// reparse sets ThisNodeHasError on nodes the Store does not have, and the
// binder propagates it to the ancestors.
func options(kind core.ScriptKind) storetest.Options {
	if kind == core.ScriptKindJS || kind == core.ScriptKindJSX {
		return storetest.Options{SkipReparsed: true, MaskFlags: ast.NodeFlagsHasJSDoc | ast.NodeFlagsPossiblyContainsDeprecatedTag | ast.NodeFlagsThisNodeHasError | ast.NodeFlagsThisNodeOrAnySubNodesHasError | ast.NodeFlagsPossiblyContainsDynamicImport}
	}
	return storetest.Options{}
}

// Small sources for the paths the fixtures do not take: private names,
// pattern ambient modules with attributes, CommonJS, expandos, a JSON file.
var smallSources = map[string]string{
	"/private.ts":   "class C { #x = 1; #m() { return this.#x } static #s = 2 } class D { #x = 3 }",
	"/pattern.d.ts": `declare module "foo*" with { type: "json" } { export const x: number } declare module "bar*" { } declare module "baz" { }`,
	"/flow.ts": `function f(a: string | number, b?: { c?: string }) {
	label: for (let i = 0; i < 10; i++) { if (typeof a === "string") { continue label } else { break } }
	while (a) { a = 1 }
	do { a = "" } while (b?.c)
	switch (a) { case 1: case 2: a = 3; default: return }
	try { throw 1 } catch (e) { a = 2 } finally { a = 3 }
	const g = () => { return b?.c ?? "" }
	(function () { return 1 })();
	for (const k in b) { }
	for (const v of [1, 2]) { }
	x ||= 1; [a, b] = [1, {}]; ({ a } = { a: 1 });
}
var x: number; export {};`,
	"/commonjs.js": `const fs = require("fs"); module.exports = { a: 1 }; exports.b = 2; function F() { this.x = 1 } F.prototype.m = function () {}; F.y = 2;`,
	"/module.ts":   `export default class { } export function f() { } export { f as g }; import * as ns from "./a"; import { a, b as c } from "node:fs"; export * from "./b"; export * as d from "./c"; export = f;`,
	"/enums.ts":    `const enum E { A } enum F { B = 1 } namespace N { export const x = 1; namespace M { } } namespace N { export function f() { } } declare global { interface Window { } }`,
	"/jsx.tsx":     `const a = <div x="1" y={2} ns:z="3">{a}</div>; export {}`,
	"/json.json":   `{ "a": 1, "b": [true, null] }`,
	"/strict.ts":   `"use strict"; function f(eval) { arguments = 1; delete x; with (y) { } } let await = 1; yield: 1;`,
}

func TestEquivalence(t *testing.T) {
	type input struct {
		opts ast.SourceFileParseOptions
		text string
		kind core.ScriptKind
	}
	inputs := map[string]input{}
	for name, source := range smallSources {
		opts, text, kind := sourceInput(name, source)
		inputs[name] = input{opts, text, kind}
	}
	for _, fixture := range fixtures.ASTBenchFixtures {
		opts, text, kind := fixtureInput(t, fixture)
		inputs[fixture.Name()] = input{opts, text, kind}
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			pointer, file := bindBoth(in.opts, in.text, in.kind)
			if !file.IsBound() || file.Bound == nil {
				t.Fatal("the Store file is not bound")
			}
			if pointer.SymbolCount == 0 {
				t.Fatal("the Pointer bind made no symbol")
			}
			BindEquivalent(pointer, file, options(in.kind)).Report(t)
		})
	}
}

type corpusFile struct {
	name, text string
}

// corpus is the corpus of the Store parser's tests (internal/storeparser/parser_test.go):
// every parsable file under testdata/fixtures and, unless -short, every unit
// of the compiler and conformance test cases.
func corpus(tb testing.TB) []corpusFile {
	tb.Helper()
	return corpusUnder(tb, !testing.Short())
}

func corpusUnder(tb testing.TB, testCases bool) []corpusFile {
	tb.Helper()
	var supported []string
	for _, es := range tspath.AllSupportedExtensionsWithJson {
		supported = append(supported, es...)
	}
	var files []corpusFile
	add := func(name, text string) {
		if ext := tspath.TryGetExtensionFromPath(name); slices.Contains(supported, ext) {
			files = append(files, corpusFile{"/index" + ext, text})
		}
	}
	each := func(root string, visit func(path string, text string)) bool {
		if _, err := os.Stat(root); err != nil {
			return false
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || tspath.TryGetExtensionFromPath(path) == "" {
				return nil
			}
			text, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			visit(path, string(text))
			return nil
		})
		if err != nil {
			tb.Fatal(err)
		}
		return true
	}
	if !each(filepath.Join(repo.TestDataPath(), "fixtures"), add) {
		tb.Fatal("testdata/fixtures is missing")
	}
	if !testCases {
		return files
	}
	for _, dir := range []string{"tests/cases/compiler", "tests/cases/conformance"} {
		found := each(filepath.Join(repo.TestDataPath(), dir), func(path, text string) {
			units, _, _, _, err := testrunner.ParseTestFilesAndSymlinks(text, path,
				func(name, content string, _ map[string]string) (corpusFile, error) {
					return corpusFile{name, content}, nil
				})
			if err != nil {
				tb.Fatal(err)
			}
			for _, unit := range units {
				add(unit.name, unit.text)
			}
		})
		if !found {
			tb.Logf("skipped: testdata/%s is missing", dir)
		}
	}
	return files
}

func TestCorpus(t *testing.T) {
	files := corpus(t)
	all := storetest.Mismatches{}
	example := map[string]int{}
	var compared, parseMismatch, excludedFiles, refs int
	reasonCounts := map[string]int{}
	excludedBy := map[string][]string{}
	for i, f := range files {
		opts, text, kind := sourceInput(f.name, f.text)
		pointer, file := bindBoth(opts, text, kind)
		reasons := reparsedEffects(pointer)
		for reason := range reasons {
			reasonCounts[reason]++
		}
		if excluded(reasons) {
			excludedFiles++
			for reason := range reasons {
				if len(excludedBy[reason]) < 400 {
					excludedBy[reason] = append(excludedBy[reason], fmt.Sprintf("#%d", i))
				}
			}
			continue
		}
		if pairs, m := storetest.Pairs(pointer, file.Store, options(kind)); pairs == nil {
			parseMismatch++
			for label := range m {
				t.Errorf("parse mismatch in %s #%d: %s", f.name, i, label)
			}
			continue
		}
		compared++
		refs += countRefs(file)
		for label, count := range BindEquivalent(pointer, file, options(kind)) {
			if all[label] == 0 {
				example[label] = i
			}
			all[label] += count
		}
	}
	t.Logf("%d files: %d compared, %d excluded (JS with a reparsed node the bind depends on), %d with a parse mismatch; %d Refs checked", len(files), compared, excludedFiles, parseMismatch, refs)
	var reasons []string
	for reason := range reasonCounts {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		t.Logf("files with %s: %d; excluded among them: %s", reason, reasonCounts[reason], strings.Join(excludedBy[reason], " "))
	}
	located := storetest.Mismatches{}
	for label, count := range all {
		located[labelWithExample(label, files[example[label]].name, example[label])] = count
	}
	located.Report(t)
}

// countRefs is the Refs of the file's symbols whose file index the driver checked.
func countRefs(file *store.File) int {
	n := 0
	seen := map[*store.Symbol]bool{}
	var visit func(s *store.Symbol)
	visit = func(s *store.Symbol) {
		if s == nil || seen[s] {
			return
		}
		seen[s] = true
		n += len(s.Declarations)
		if s.ValueDeclaration != (store.Ref{}) {
			n++
		}
		for _, m := range s.Members {
			visit(m)
		}
		for _, e := range s.Exports {
			visit(e)
		}
		visit(s.Parent)
		visit(s.ExportSymbol)
	}
	for _, node := range storetest.Preorder(file.Root()) {
		visit(file.Bound.SymbolOf(node.Symbol()))
		visit(file.Bound.SymbolOf(node.LocalSymbol()))
		if slot, ok := node.LocalsSlot(); ok && slot != 0 {
			for _, l := range file.Bound.Locals(slot) {
				visit(l)
			}
		}
	}
	return n
}

// TestFlowSlabs logs the size of the flow slabs and checks that they hold at
// least the reachable flow nodes of the Pointer binder
// (store-binder-design-20260922.md 2.1); a label no flow reaches is the
// difference.
func TestFlowSlabs(t *testing.T) {
	reachable := map[string][3]int{
		"checker.ts":         {45694, 15402, 878},
		"dom.generated.d.ts": {4841, 0, 0},
	}
	for _, fixture := range fixtures.ASTBenchFixtures {
		t.Run(fixture.Name(), func(t *testing.T) {
			file := storeparser.ParseSourceFile(fixtureInput(t, fixture))
			storebinder.BindSourceFile(file)
			flows, lists, data := file.Bound.FlowCounts()
			want := reachable[fixture.Name()]
			t.Logf("%s: flows %d, flowLists %d, flowData %d (sentinels included); reachable in the Pointer binder: %d / %d / %d; Bound slabs %d B",
				fixture.Name(), flows, lists, data, want[0], want[1], want[2], file.Bound.Footprint())
			if flows-1 < want[0] || lists-1 < want[1] || data-1 < want[2] {
				t.Errorf("the slabs hold fewer entries than the Pointer binder reaches")
			}
		})
	}
}

// TestBindOnce: a second bind is a no-op, and the Store is sealed.
func TestBindOnce(t *testing.T) {
	file := storeparser.ParseSourceFile(sourceInput("/a.ts", "let x = 1; export {}"))
	storebinder.BindSourceFile(file)
	bound := file.Bound
	storebinder.BindSourceFile(file)
	if file.Bound != bound {
		t.Error("the second bind replaced Bound")
	}
	if file.Symbol() == nil || file.Symbol().Flags&ast.SymbolFlagsValueModule == 0 {
		t.Errorf("file symbol = %v, want a value module", file.Symbol())
	}
}
