package storeparser

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/storetest"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/repo"
	"github.com/microsoft/TypeScript/tsc/internal/testrunner"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/filefixture"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// Copied from astBenchmarkBaselines in internal/ast/ast_benchmark_test.go and
// walkBaselines in internal/ast/store/store_test.go.
var astBenchmarkBaselines = map[string]struct {
	visits      uint64
	fingerprint string
}{
	"checker.ts":         {visits: 298054, fingerprint: "a59b2db86e71f892d323ee3324f97e420c48768d4464066f8fe2a0a48444404a"},
	"dom.generated.d.ts": {visits: 109605, fingerprint: "c77d4af443cb5ccb5fa271cd64e7404a39e144bc783f9e516c82306ab120cf25"},
}

// Copied from TestASTBenchmarkSmallWalk in internal/ast/ast_benchmark_test.go.
const smallSource = `const result = (left + right); empty([]); fn<T>(); fn?.<T>();`

func fixtureInput(tb testing.TB, fixture filefixture.Fixture) (ast.SourceFileParseOptions, string, core.ScriptKind) {
	tb.Helper()
	return fixtures.ASTBenchParseInput(tb, fixture)
}

func sourceInput(name, source string) (ast.SourceFileParseOptions, string, core.ScriptKind) {
	return ast.SourceFileParseOptions{FileName: name, Path: tspath.Path(name)}, source, core.GetScriptKindFromFileName(name)
}

// parseBoth parses the same input with the Pointer parser (its defaults) and
// the Store parser.
func parseBoth(opts ast.SourceFileParseOptions, text string, kind core.ScriptKind) (pointer *ast.SourceFile, file *store.File) {
	return parser.ParseSourceFile(opts, text, kind), ParseSourceFile(opts, text, kind)
}

func TestFingerprint(t *testing.T) {
	for _, fixture := range fixtures.ASTBenchFixtures {
		t.Run(fixture.Name(), func(t *testing.T) {
			file := ParseSourceFile(fixtureInput(t, fixture))
			nodes := storetest.Preorder(file.Root())
			h := sha256.New()
			for _, n := range nodes {
				fmt.Fprintf(h, "%s|%d|%d\n", n.Kind(), n.Pos(), n.End())
			}
			fingerprint := hex.EncodeToString(h.Sum(nil))
			want := astBenchmarkBaselines[fixture.Name()]
			if uint64(len(nodes)) != want.visits || fingerprint != want.fingerprint {
				t.Fatalf("baseline mismatch: visits=%d fingerprint=%s", len(nodes), fingerprint)
			}
			t.Logf("%s: %d nodes, %d dead", fixture.Name(), file.NodeCount, file.NodeCount-len(nodes)-eagerJSDocNodes(file))
		})
	}
}

func TestEquivalence(t *testing.T) {
	t.Logf("comparing %d members and %d roles per kind", storetest.ComparedMembers, storetest.ComparedRoles)
	type input struct {
		opts ast.SourceFileParseOptions
		text string
		kind core.ScriptKind
	}
	inputs := map[string]input{}
	opts, text, kind := sourceInput("/ast-benchmark-small.ts", smallSource)
	inputs["small"] = input{opts, text, kind}
	opts, text, kind = sourceInput("/rewound-typedef.js", "x = (/** @typedef {number} T */ a, b);\n/** @type {T} */\nvar y;")
	inputs["rewound-typedef"] = input{opts, text, kind}
	for _, fixture := range fixtures.ASTBenchFixtures {
		opts, text, kind := fixtureInput(t, fixture)
		inputs[fixture.Name()] = input{opts, text, kind}
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			pointer, file := parseBoth(in.opts, in.text, in.kind)
			m, _ := storetest.Equivalent(pointer, file.Store)
			m.Report(t)
			jm, _ := storetest.EquivalentJSDoc(pointer, file)
			jm.Report(t)
			compareFields(t, pointer, file)
		})
	}
}

// TestRewoundReparse puts a @typedef, @callback or @import in the JSDoc of a
// node made in a speculation. The Pointer parser keeps the reparse list across
// a rewind, so the statement reparsed in a discarded speculation stays in the
// enclosing statement list; the Store parser must keep its rows.
func TestRewoundReparse(t *testing.T) {
	tags := map[string]string{
		"typedef":  "/** @typedef {number} T */",
		"callback": "/** @callback F @returns {string} */",
		"import":   "/** @import { A } from 'a' */",
	}
	positions := []string{
		"x = (%s a, b);",
		"x = (%s a);",
		"x = (%s a, b) => a;", // the speculation succeeds
		"x = (%s a = 1, b);",
		"x = (%s {a}, b);",
		"x = async (%s a, b);",
		"x = c ? (%s a) : d;",
		"x = c ? (%s a) : d => e;",
		"x = ((%s a, b), c);",
		"x = (a = class {\n%s\nm() {} }, b);", // pushed out of the class member list; on the line of { the comment is not the member's JSDoc
		"x = (a = {\n%s\nm() {} }, b);",
		"f(%s a, b);", // no speculation
		"x = <div>{(%s a, b)}</div>;",
	}
	for i, position := range positions {
		name := "/a.js"
		if strings.Contains(position, "<div>") {
			name = "/a.jsx"
		}
		for tag, comment := range tags {
			source := fmt.Sprintf(position, comment)
			t.Run(fmt.Sprintf("%d/%s", i+1, tag), func(t *testing.T) {
				pointer, file := parseBoth(sourceInput(name, source))
				m, _ := storetest.Equivalent(pointer, file.Store)
				m.Report(t)
				jm, _ := storetest.EquivalentJSDoc(pointer, file)
				jm.Report(t)
				compareFields(t, pointer, file)
				want, got := diagnosticKeys(pointer.Diagnostics()), diagnosticKeys(file.Diagnostics())
				if !slices.Equal(want, got) {
					t.Errorf("diagnostics differ:\n  pointer: %v\n  store:   %v", want, got)
				}
			})
		}
	}
}

// compareFields checks the fields of store.File against ast.SourceFile.
func compareFields(t *testing.T, pointer *ast.SourceFile, file *store.File) {
	t.Helper()
	for label, differs := range fieldMismatches(pointer, file) {
		if differs {
			t.Errorf("%s differs from the Pointer file", label)
		}
	}
}

// fieldMismatches compares the fields of store.File with ast.SourceFile, the
// JSDoc diagnostics and the reparsed clones (position and kind) included.
func fieldMismatches(pointer *ast.SourceFile, file *store.File) map[string]bool {
	refs := func(a, b []*ast.FileReference) bool {
		return !slices.EqualFunc(a, b, func(x, y *ast.FileReference) bool { return *x == *y })
	}
	node := func(p *ast.Node, r store.NodeRef) bool {
		if p == nil || r == store.NoNodeRef {
			return (p == nil) != (r == store.NoNodeRef)
		}
		n := file.Store.Node(r)
		return n.Kind() != p.Kind || int(n.Pos()) != p.Pos()
	}
	nodes := func(ps []*ast.Node, rs []store.NodeRef) bool {
		if len(ps) != len(rs) {
			return true
		}
		for i := range ps {
			if node(ps[i], rs[i]) {
				return true
			}
		}
		return false
	}
	m := map[string]bool{
		"FileName":                    file.FileName != pointer.FileName(),
		"Path":                        file.Path != pointer.Path(),
		"Text":                        file.Text != pointer.Text(),
		"ScriptKind":                  file.ScriptKind != pointer.ScriptKind,
		"LanguageVariant":             file.LanguageVariant != pointer.LanguageVariant,
		"IsDeclarationFile":           file.IsDeclarationFile != pointer.IsDeclarationFile,
		"Flags":                       file.Flags != pointer.Flags || file.Flags != file.Root().Flags(),
		"IdentifierCount":             file.IdentifierCount != pointer.IdentifierCount,
		"CommentDirectives":           !slices.Equal(file.CommentDirectives, pointer.CommentDirectives),
		"Pragmas":                     len(file.Pragmas) != len(pointer.Pragmas),
		"ReferencedFiles":             refs(file.ReferencedFiles, pointer.ReferencedFiles),
		"TypeReferenceDirectives":     refs(file.TypeReferenceDirectives, pointer.TypeReferenceDirectives),
		"LibReferenceDirectives":      refs(file.LibReferenceDirectives, pointer.LibReferenceDirectives),
		"CheckJsDirective":            (file.CheckJsDirective == nil) != (pointer.CheckJsDirective == nil) || file.CheckJsDirective != nil && *file.CheckJsDirective != *pointer.CheckJsDirective,
		"ExternalModuleIndicator":     node(pointer.ExternalModuleIndicator, file.ExternalModuleIndicator),
		"Imports":                     nodes(pointer.Imports(), file.Imports),
		"ModuleAugmentations":         nodes(pointer.ModuleAugmentations, file.ModuleAugmentations),
		"AmbientModuleNames":          !slices.Equal(pointer.AmbientModuleNames, file.AmbientModuleNames),
		"UsesUriStyleNodeCoreModules": pointer.UsesUriStyleNodeCoreModules != file.UsesUriStyleNodeCoreModules,
		"JSDocDiagnostics":            !slices.Equal(diagnosticKeys(pointer.JSDocDiagnostics()), diagnosticKeys(file.JSDocDiagnostics())),
		"ReparsedClones":              nodes(pointer.ReparsedClones, file.ReparsedClones),
	}
	for i := range min(len(file.Pragmas), len(pointer.Pragmas)) {
		a, b := file.Pragmas[i], pointer.Pragmas[i]
		if a.Name != b.Name || a.CommentRange != b.CommentRange || len(a.Args) != len(b.Args) {
			m["Pragmas"] = true
		}
	}
	return m
}

type corpusFile struct {
	name, text string
}

// corpus is the seed corpus of FuzzParser in internal/parser/parser_test.go:
// every parsable file under testdata/fixtures and, unless -short, every unit
// of the compiler and conformance test cases.
func corpus(tb testing.TB) []corpusFile {
	tb.Helper()
	return corpusUnder(tb, !testing.Short())
}

// corpusUnder is corpus with the test cases included on demand.
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

// A diagnostic as compared between the two parsers: the Store one has no file.
type diagnosticKey struct {
	pos, length int
	code        int32
	message     string
}

func diagnosticKeys(ds []*ast.Diagnostic) []diagnosticKey {
	keys := make([]diagnosticKey, len(ds))
	for i, d := range ds {
		keys[i] = diagnosticKey{d.Pos(), d.Len(), d.Code(), d.MessageText()}
	}
	return keys
}

func TestCorpus(t *testing.T) {
	files := corpus(t)
	all := storetest.Mismatches{}
	example := map[string]int{}
	fields := map[string]int{}
	var visits, pointerNodes, storeNodes int
	var jsdocNodes, eagerNodes, jsFiles, jsLive, jsDead int
	diagnosticsSame, diagnosticsDiffer := 0, 0
	for i, f := range files {
		opts, text, kind := sourceInput(f.name, f.text)
		pointer, file := parseBoth(opts, text, kind)
		m, n := storetest.Equivalent(pointer, file.Store)
		visits += n
		pointerNodes += pointer.NodeCount
		storeNodes += file.NodeCount
		eager := eagerJSDocNodes(file)
		eagerNodes += eager
		if kind == core.ScriptKindJS || kind == core.ScriptKindJSX {
			jsFiles++
			jsLive += n + eager
			jsDead += file.NodeCount - n - eager
		}
		jm, jn := storetest.EquivalentJSDoc(pointer, file)
		jsdocNodes += jn
		for _, mm := range []storetest.Mismatches{m, jm} {
			for label, count := range mm {
				if all[label] == 0 {
					example[label] = i
				}
				all[label] += count
			}
		}
		for label, differs := range fieldMismatches(pointer, file) {
			if differs {
				if fields[label] == 0 {
					t.Logf("%s differs first in %s #%d", label, f.name, i)
				}
				fields[label]++
			}
		}
		want, got := diagnosticKeys(pointer.Diagnostics()), diagnosticKeys(file.Diagnostics())
		if slices.Equal(want, got) {
			diagnosticsSame++
		} else {
			diagnosticsDiffer++
			if diagnosticsDiffer <= 10 {
				t.Errorf("diagnostics differ in %s #%d:\n  pointer: %v\n  store:   %v", f.name, i, want, got)
			}
		}
	}
	live := visits + eagerNodes
	t.Logf("%d files (%d JS), %d nodes visited, %d JSDoc nodes compared (%d eager, in the file's Store); Pointer NodeCount %d, Store NodeCount %d (dead %d = %.2f%%)",
		len(files), jsFiles, visits, jsdocNodes, eagerNodes, pointerNodes, storeNodes, storeNodes-live, 100*float64(storeNodes-live)/float64(live))
	t.Logf("JS files: %d live nodes, dead %d = %.2f%%", jsLive, jsDead, 100*float64(jsDead)/float64(jsLive))
	t.Logf("diagnostics: %d files same, %d differ", diagnosticsSame, diagnosticsDiffer)
	for label, count := range fields {
		t.Errorf("%s differs in %d files", label, count)
	}
	located := storetest.Mismatches{}
	for label, count := range all {
		located[fmt.Sprintf("%s (first in %s #%d)", label, files[example[label]].name, example[label])] = count
	}
	located.Report(t)
}

// eagerJSDocNodes counts the nodes of the JSDoc parsed with the file, which
// live in its Store: the JSDoc of every host before any lazy parse, each
// comment once (a @typedef's comment belongs to its type alias too).
func eagerJSDocNodes(file *store.File) int {
	n := 0
	seen := map[store.NodeKey]bool{}
	for _, host := range storetest.Preorder(file.Root()) {
		for _, jsdoc := range host.EagerJSDoc(file) {
			if !seen[jsdoc.Key()] {
				seen[jsdoc.Key()] = true
				n += len(storetest.Preorder(jsdoc))
			}
		}
	}
	return n
}

// FuzzEquivalence parses each input with both parsers and holds it to the
// rules of TestCorpus. The seed corpus is TestCorpus's; the arguments are
// FuzzParser's in internal/parser/parser_test.go.
func FuzzEquivalence(f *testing.F) {
	for _, file := range corpus(f) {
		f.Add(tspath.TryGetExtensionFromPath(file.name), file.text, false, false)
	}
	var extensions []string
	for _, es := range tspath.AllSupportedExtensionsWithJson {
		extensions = append(extensions, es...)
	}
	f.Fuzz(func(t *testing.T, extension string, text string, jsx bool, force bool) {
		if !slices.Contains(extensions, extension) {
			t.Skip()
		}
		opts, _, kind := sourceInput("/index"+extension, "")
		opts.ExternalModuleIndicatorOptions = ast.ExternalModuleIndicatorOptions{JSX: jsx, Force: force}
		pointer, file := parseBoth(opts, text, kind)
		m, _ := storetest.Equivalent(pointer, file.Store)
		m.Report(t)
		jm, _ := storetest.EquivalentJSDoc(pointer, file)
		jm.Report(t)
		for label, differs := range fieldMismatches(pointer, file) {
			if differs {
				t.Errorf("%s differs from the Pointer file", label)
			}
		}
		want, got := diagnosticKeys(pointer.Diagnostics()), diagnosticKeys(file.Diagnostics())
		if !slices.Equal(want, got) {
			t.Errorf("diagnostics differ:\n  pointer: %v\n  store:   %v", want, got)
		}
	})
}

// TestDeadNodes checks that a rewound speculation leaves no node behind, and
// documents the constructs that leave one without a rewind. The JSDoc parsed
// with the file is live.
func TestDeadNodes(t *testing.T) {
	deadNodes := func(name, source string) int {
		file := ParseSourceFile(sourceInput(name, source))
		return file.NodeCount - len(storetest.Preorder(file.Root())) - eagerJSDocNodes(file)
	}
	for _, source := range []string{
		"(a, b) => c",
		"a < b > c",
		"x!!.y",
		"a ? (b) : c",
		"f(a < b, c > d)",
		"let x: string.length",
		"function f(): asserts is {}",
		"type T = infer U extends string ? 1 : 2",
	} {
		if dead := deadNodes("/a.ts", source); dead != 0 {
			t.Errorf("%q leaves %d dead nodes after its speculation", source, dead)
		}
	}
	for _, c := range []struct {
		source string
		dead   int
	}{
		{"f<T>()", 1},                     // ExpressionWithTypeArguments absorbed into the call
		{"new C<T>()", 1},                 // absorbed into the new expression
		{"f<T>`x`", 1},                    // absorbed into the tagged template
		{"interface I extends A.B {}", 2}, // ExpressionWithTypeArguments and PropertyAccessExpression become a TypeReference
		{"type T = [a?]", 1},              // JSDocNullableType becomes an OptionalTypeNode
		{"<div><span></div>", 1},          // the mismatched JsxElement is rebuilt
		// The top-level await reparse (await is an identifier here, so the
		// module's statements are reparsed in an await context): the old root
		// and the two old statements (ExpressionStatement + Identifier,
		// ExpressionStatement + NumericLiteral) stay; the rewinds that keep
		// the reparsed nodes leave nothing extra.
		{"export {};\nawait\n1;", 5},
		// JS: the JSDoc parse drops what the Pointer parser drops, and the
		// reparse rewrites lists (old blocks, not nodes) and fills empty
		// slots, so only a slot that held a node leaves one.
		{"/** @type {number} */\nvar x = 1;", 0},
		{"/** @readonly */\nthis.x = 1;", 0},
		{"class C {\n/** @readonly */\nx = 1;\n/** @private */\nm() {}\n}", 0},
		{"/** @this {C} */\nfunction f(a) {}", 0},
		{"/** @implements {I} */\nclass C extends B {}", 0},
		{"/** @template T\n * @param {T} x\n * @returns {T}\n */\nfunction f(x) { return x }", 0},
		{"/** @satisfies {T} */\nvar x = {};", 0},
		{"return /** @type {T} */ (x);", 0},
		{"/** @import { A } from 'a' */", 0},
		{"/** @overload\n * @param {number} a\n */\nfunction f(a) {}", 0},
		{"/** see {@link A.B} and {@linkcode C} */\nvar x;", 0},
		{"/** @param {number} [x=1] */\nfunction f(x) {}", 2},                       // the default: EqualsToken and NumericLiteral
		{"/** @param {Object} o\n * @param {string} o.a\n */\nfunction f(o) {}", 3}, // {Object}, replaced by the nested type literal
		{"/** @typedef {Object} T\n * @property {string} a\n */", 3},                // {Object}, replaced by the type literal
		// {Object}, the property tag (5 nodes) and the @type tag (2) whose type the typedef takes
		{"/** @typedef {Object} T\n * @property {string} a\n * @type {number}\n */", 10},
		// the reparse: the function type made with an any return type, replaced by the @returns type
		{"/** @callback F\n * @param {number} a\n * @returns {string}\n */", 1},
		// The arrow speculation is rewound but keeps its rows for the reparsed
		// @typedef: the two Parameters with their Identifiers, and the JSDoc of
		// a (JSDoc, JSDocTypedefTag, its two Identifiers, JSDocTypeExpression,
		// NumberKeyword).
		{"x = (/** @typedef {number} T */ a, b);", 10},
	} {
		name := "/a.ts"
		if strings.HasPrefix(c.source, "<") {
			name = "/a.tsx"
		}
		if strings.HasPrefix(c.source, "/**") || strings.HasPrefix(c.source, "class") || strings.HasPrefix(c.source, "return") || strings.HasPrefix(c.source, "x = (") {
			name = "/a.js"
		}
		if dead := deadNodes(name, c.source); dead != c.dead {
			t.Errorf("%q leaves %d dead nodes, want %d", c.source, dead, c.dead)
		}
	}
	for _, fixture := range fixtures.ASTBenchFixtures {
		file := ParseSourceFile(fixtureInput(t, fixture))
		visits := len(storetest.Preorder(file.Root()))
		eager := eagerJSDocNodes(file)
		pointer := parser.ParseSourceFile(fixtureInput(t, fixture))
		t.Logf("%s: Store NodeCount %d, visits %d, eager JSDoc %d, dead %d; Pointer NodeCount %d",
			fixture.Name(), file.NodeCount, visits, eager, file.NodeCount-visits-eager, pointer.NodeCount)
	}
}

// mallocs counts the heap allocations of one call of fn.
func mallocs(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.Mallocs - before.Mallocs
}

// TestScratchReuse parses the same file twice with one parser: the second
// parse allocates only what Finish copies, the result and its diagnostics.
func TestScratchReuse(t *testing.T) {
	fixture := fixtures.ASTBenchCheckerFixture
	opts, text, kind := fixtureInput(t, fixture)
	p := newParser()
	var file *store.File
	parse := func() {
		p.initializeState(nil, 0, opts, text, kind)
		p.nextToken()
		file = p.parseSourceFileWorker()
		*p = Parser{scanner: p.scanner, b: p.b, elems: p.elems[:0], missingLists: p.missingLists[:0]}
	}
	first := mallocs(parse)
	second := mallocs(parse)
	third := mallocs(parse)
	t.Logf("%s: allocations per parse: first %d, second %d, third %d (%d nodes, %d diagnostics, %d pragmas)",
		fixture.Name(), first, second, third, file.NodeCount, len(file.Diagnostics()), len(file.Pragmas))
	if second >= first {
		t.Errorf("the second parse allocated %d times, the first %d: the scratch is not reused", second, first)
	}
}

// The counterpart of TestTexts in internal/ast/store/store_test.go, parsed
// directly.
func TestTexts(t *testing.T) {
	file := ParseSourceFile(sourceInput("/a.ts", "\\u0061bc; abc; 'a\\nb'; 'cd';"))
	var identifiers, literals []string
	var inTexts int
	for _, n := range storetest.Preorder(file.Root()) {
		switch n.Kind() {
		case ast.KindIdentifier:
			identifiers = append(identifiers, n.AsIdentifier().Text(), n.Text())
			if store.IdentifierTextInTexts(n) {
				inTexts++
			}
		case ast.KindStringLiteral:
			literals = append(literals, n.AsStringLiteral().Text(), n.Text())
		}
	}
	if !slices.Equal(identifiers, []string{"abc", "abc", "abc", "abc"}) || inTexts != 1 {
		t.Errorf("identifiers = %q with %d outside the source, want 2 x abc with 1", identifiers, inTexts)
	}
	if !slices.Equal(literals, []string{"a\nb", "a\nb", "cd", "cd"}) {
		t.Errorf("literals = %q", literals)
	}
	if _, _, texts := file.Store.Footprint(); texts != len("abc")+len("a\nb")+len("cd") {
		t.Errorf("texts holds %d bytes, want %d", texts, len("abc")+len("a\nb")+len("cd"))
	}
}

// TestComposedText: the Text role of the kinds without a text slot whose
// Pointer Text is built from the children (MetaProperty, JsxNamespacedName).
// The generated role comparison covers only the kinds with a text slot.
func TestComposedText(t *testing.T) {
	sources := map[string]string{
		"/a.tsx": `function f() { return [import.meta, new.target, <a ns:tag x:y="1"/>, <p:q></p:q>] }`,
		"/b.ts":  `const u = import.meta.url; function g() { new.target }`,
	}
	for name, source := range sources {
		opts, text, kind := sourceInput(name, source)
		pointer, file := parseBoth(opts, text, kind)
		pairs, mismatches := storetest.Pairs(pointer, file.Store)
		if pairs == nil {
			t.Fatalf("%s: the walks do not line up: %v", name, mismatches)
		}
		seen := 0
		for _, pair := range pairs {
			if pair.P.Kind != ast.KindMetaProperty && pair.P.Kind != ast.KindJsxNamespacedName {
				continue
			}
			seen++
			if got, want := pair.N.Text(), pair.P.Text(); got != want {
				t.Errorf("%s: %v Text = %q, want %q", name, pair.P.Kind, got, want)
			}
		}
		if seen == 0 {
			t.Errorf("%s: no MetaProperty or JsxNamespacedName", name)
		}
	}
}
