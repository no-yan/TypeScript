package storecompiler_test

import (
	"fmt"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/storetest"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
	"github.com/microsoft/TypeScript/tsc/internal/storeparser"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// The dynamic numbering of store-cli-design-20261004.md 4.3: source Stores
// take their StoreIDs from the same IDSpace as the lazy JSDoc and synthetic
// Stores, in the order the program makes them, and no NodeKey changes or
// repeats afterwards.

const jsdocSource = "/** @deprecated use y */\nexport function x() {}\nexport const y = 1;\n"

func parseIn(space *store.IDSpace, fileName, text string) *store.File {
	opts := ast.SourceFileParseOptions{FileName: fileName, Path: tspath.Path(fileName)}
	return storeparser.ParseSourceFileIn(space, space.Next(), opts, text, core.GetScriptKindFromFileName(fileName))
}

// jsdocHosts are the nodes of f that have JSDoc.
func jsdocHosts(f *store.File) []store.Node {
	var out []store.Node
	for _, n := range storetest.Preorder(f.Root()) {
		if n.Flags()&ast.NodeFlagsHasJSDoc != 0 {
			out = append(out, n)
		}
	}
	return out
}

// snapshot is what a Store's nodes say of themselves: key, kind, parent key
// and file.
func snapshot(root store.Node) []string {
	var out []string
	for _, n := range storetest.Preorder(root) {
		out = append(out, fmt.Sprintf("%v %v parent=%v file=%s", n.Key(), n.Kind(), n.Parent().Key(), n.File().FileName()))
	}
	return out
}

func mustPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: no panic", what)
		}
	}()
	f()
}

func TestNativeIDSpace(t *testing.T) {
	space := store.NewIDSpace(0)
	type stage struct {
		label string
		id    store.StoreID
		root  store.Node
		file  *store.File
		nodes []string
	}
	var stages []stage
	add := func(label string, root store.Node, file *store.File) {
		stages = append(stages, stage{label, root.Key().StoreID(), root, file, snapshot(root)})
	}
	var files []*store.File
	for _, name := range []string{"/a.ts", "/b.ts", "/c.ts"} {
		f := parseIn(space, name, jsdocSource)
		files = append(files, f)
		add("source "+name, f.Root(), f)
		if name == "/c.ts" {
			break
		}
		host := jsdocHosts(f)[0]
		jsdoc := host.JSDoc(f)[0]
		if !store.SameNode(jsdoc.Parent(), host) || jsdoc.File() != f {
			t.Errorf("%s: the JSDoc root does not lead to its host", name)
		}
		add("lazy JSDoc "+name, jsdoc, f)
		specifier := store.NewSyntheticImport(f, "tslib")
		if !store.SameNode(specifier.Parent().Parent(), f.Root()) || specifier.File() != f {
			t.Errorf("%s: the synthetic import does not lead to the root", name)
		}
		add("synthetic "+name, specifier.Parent(), f)
	}

	// The StoreIDs are 0..6 in the order of making, whatever the kind.
	for i, s := range stages {
		if s.id != store.StoreID(i) {
			t.Errorf("%s: StoreID %d, want %d", s.label, s.id, i)
		}
	}
	// The JSDoc of a host is parsed once: asking again takes no StoreID.
	if again := jsdocHosts(files[0])[0].JSDoc(files[0])[0]; !store.SameNode(again, stages[1].root) {
		t.Error("the lazy JSDoc of a.ts was made twice")
	}
	if next := space.Next(); next != store.StoreID(len(stages)) {
		t.Errorf("the next StoreID is %d, want %d", next, len(stages))
	}

	// Every node keeps its key, parent and file, and no key repeats.
	keys := map[store.NodeKey]string{}
	for _, s := range stages {
		if got := snapshot(s.root); fmt.Sprint(got) != fmt.Sprint(s.nodes) {
			t.Errorf("%s: the nodes changed:\n%v\n%v", s.label, s.nodes, got)
		}
		for _, n := range storetest.Preorder(s.root) {
			if other, ok := keys[n.Key()]; ok {
				t.Errorf("%s: key %v of %s", s.label, n.Key(), other)
			}
			keys[n.Key()] = s.label
		}
	}

	// A JS file's JSDoc is eager, in its source Store: the parse takes one
	// StoreID, and reading the JSDoc takes none.
	before := space.Next()
	js := parseIn(space, "/d.js", "/** @type {number} */\nvar d = 1;\n")
	if js.Root().Key().StoreID() != before+1 {
		t.Errorf("the JS file has StoreID %d, want %d", js.Root().Key().StoreID(), before+1)
	}
	if doc := jsdocHosts(js)[0].JSDoc(js)[0]; doc.Key().StoreID() != js.Root().Key().StoreID() {
		t.Errorf("the eager JSDoc is in Store %d, not the file's", doc.Key().StoreID())
	}
	if next := space.Next(); next != before+2 {
		t.Errorf("the eager JSDoc took a StoreID: next %d, want %d", next, before+2)
	}

	// A region of another program refuses every Store of this one.
	other := store.NewFactory(store.NewNodeRegion(store.NewIDSpace(0)), 1, store.FactoryHooks{})
	for _, s := range stages {
		mustPanic(t, "another program takes "+s.label, func() { other.NewParenthesizedExpression(s.root) })
	}

	// The fixed reservation of a fixture: the reserved IDs are the caller's,
	// Next starts after them.
	fixed := store.NewIDSpace(2)
	f0 := storeparser.ParseSourceFileIn(fixed, 0, ast.SourceFileParseOptions{FileName: "/f0.ts", Path: "/f0.ts"}, jsdocSource, core.ScriptKindTS)
	f1 := storeparser.ParseSourceFileIn(fixed, 1, ast.SourceFileParseOptions{FileName: "/f1.ts", Path: "/f1.ts"}, jsdocSource, core.ScriptKindTS)
	if doc := jsdocHosts(f1)[0].JSDoc(f1)[0]; doc.Key().StoreID() != 2 || f0.Root().Key().StoreID() != 0 {
		t.Errorf("fixed reservation: the JSDoc has StoreID %d, the first file %d", doc.Key().StoreID(), f0.Root().Key().StoreID())
	}
	if empty := store.NewIDSpace(0); empty.Next() != 0 {
		t.Error("an empty fixture does not start at 0")
	}

	// The last StoreID is given once; every later call panics and reissues
	// none.
	last := store.NewIDSpace(int(^uint32(0) - 1))
	if id := last.Next(); id != store.StoreID(^uint32(0)-1) {
		t.Errorf("the last StoreID is %d", id)
	}
	mustPanic(t, "Next after the last", func() { last.Next() })
	mustPanic(t, "Next again after the last", func() { last.Next() })
}

// The native program numbers the files as it loads them, between the
// synthetic imports it makes; the checker's lazy JSDoc and its own Stores
// come after, and no key of the loaded program changes.
func TestNativeIDSpaceProgram(t *testing.T) {
	files := map[string]string{
		"/src/a.ts":                                   "import { y } from \"./b\";\nimport { x } from \"./b\";\nexport const a = x() ?? y;\n",
		"/src/b.ts":                                   jsdocSource,
		"/src/c.tsx":                                  "import { a } from \"./a\";\nexport const c = <div>{a}</div>;\n",
		"/node_modules/tslib/index.d.ts":              "export declare function __importDefault(m: any): any;\n",
		"/node_modules/tslib/package.json":            `{"name":"tslib","types":"index.d.ts"}`,
		"/node_modules/react/jsx-runtime.d.ts":        "export {};\ndeclare global { namespace JSX { interface IntrinsicElements { div: any } } }\n",
		"/node_modules/react/package.json":            `{"name":"react","exports":{"./jsx-runtime":{"types":"./jsx-runtime.d.ts"}}}`,
		"/node_modules/@types/react/jsx-runtime.d.ts": "export {};\n",
	}
	options := &core.CompilerOptions{
		Module: core.ModuleKindNodeNext, Target: core.ScriptTargetES2020, Strict: core.TSTrue, NoEmit: core.TSTrue,
		ImportHelpers: core.TSTrue, Jsx: core.JsxEmitReactJSX, Types: []string{},
	}
	pointer, s := newPrograms(t, files, []string{"/src/a.ts", "/src/c.tsx"}, options)
	if got, want := storeNames(s), pointerNames(pointer); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("SourceFiles:\nstore   %v\npointer %v", got, want)
	}

	// Every Store of the loaded program: the files and the synthetic imports,
	// each with a StoreID of its own.
	type loaded struct {
		label string
		root  store.Node
		nodes []string
	}
	var all []loaded
	ids := map[store.StoreID]string{}
	note := func(label string, root store.Node) {
		if other, ok := ids[root.Key().StoreID()]; ok {
			t.Errorf("%s: StoreID %d of %s", label, root.Key().StoreID(), other)
		}
		ids[root.Key().StoreID()] = label
		all = append(all, loaded{label, root, snapshot(root)})
	}
	synthetic := 0
	for _, f := range s.SourceFiles() {
		note(f.FileName(), f.Root())
		if specifier := s.GetImportHelpersImportSpecifier(f.Path()); !specifier.IsNil() {
			note("helpers of "+f.FileName(), specifier.Parent())
			synthetic++
			if specifier2 := s.GetImportHelpersImportSpecifier(f.Path()); !store.SameNode(specifier, specifier2) {
				t.Errorf("%s: a second ask gives another helpers specifier", f.FileName())
			}
		}
		if _, specifier := s.GetJSXRuntimeImportSpecifier(f.Path()); !specifier.IsNil() {
			note("jsx of "+f.FileName(), specifier.Parent())
			synthetic++
		}
		if again := s.GetSourceFile(f.FileName()); again != f {
			t.Errorf("%s: GetSourceFile gives another file", f.FileName())
		}
	}
	if synthetic < 3 {
		t.Errorf("%d synthetic imports, want the helpers of a.ts and c.tsx and the JSX runtime of c.tsx", synthetic)
	}
	var max store.StoreID
	for id := range ids {
		max = core.IfElse(id > max, id, max)
	}

	// The check makes the lazy JSDoc of b.ts (@deprecated) and the checker's
	// Stores after them; the loaded nodes stay as they were, and a symbol's
	// declaration is in its own file.
	c, _ := storechecker.NewChecker(s, nil)
	for _, f := range s.SourceFiles() {
		c.GetDiagnostics(t.Context(), f)
	}
	b := s.GetSourceFile("/src/b.ts")
	host := jsdocHosts(b)[0]
	if doc := host.JSDoc(b)[0]; doc.Key().StoreID() <= max {
		t.Errorf("the lazy JSDoc of b.ts has StoreID %d, not after the loaded Stores (%d)", doc.Key().StoreID(), max)
	}
	for _, l := range all {
		if got := snapshot(l.root); fmt.Sprint(got) != fmt.Sprint(l.nodes) {
			t.Errorf("%s: the nodes changed after the check", l.label)
		}
	}
	a := s.GetSourceFile("/src/a.ts")
	for _, n := range storetest.Preorder(a.Root()) {
		if !store.IsIdentifier(n) || (n.Text() != "x" && n.Text() != "y") {
			continue
		}
		symbol := c.GetSymbolAtLocation(n)
		if symbol == nil {
			t.Errorf("no symbol of %s at %d", n.Text(), n.Pos())
			continue
		}
		if again := c.GetSymbolAtLocation(n); again != symbol {
			t.Errorf("%s at %d: another symbol the second time", n.Text(), n.Pos())
		}
	}
}
