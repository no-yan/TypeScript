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
	"sort"
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
// the Store parser. reparsed reports whether the Pointer parser reparsed
// top-level await statements, which needs the external module indicator and
// is 7c: those files are compared but not required to match.
func parseBoth(opts ast.SourceFileParseOptions, text string, kind core.ScriptKind) (pointer *ast.SourceFile, file *store.File, reparsed bool) {
	pointer = parser.ParseSourceFile(opts, text, kind)
	p := getParser()
	defer putParser(p)
	p.initializeState(opts, text, kind)
	p.nextToken()
	if kind == core.ScriptKindJSON {
		return pointer, p.parseJSONText(), false
	}
	file = p.parseSourceFileWorker()
	reparsed = !pointer.IsDeclarationFile && pointer.ExternalModuleIndicator != nil && len(p.possibleAwaitSpans) > 0
	return pointer, file, reparsed
}

// options are the equivalence options for a file: JS files get their JSDoc
// parsed and reparsed by the Pointer parser, which the Store parser does not
// do (7c). Left out of the Flags comparison of JS files: HasJSDoc and
// PossiblyContainsDeprecatedTag, which the Pointer parser sets from the
// parsed JSDoc rather than from the scanner; ThisNodeHasError, which a
// diagnostic of the reparse sets on the node finished next (the diagnostics
// are compared separately); PossiblyContainsDynamicImport, which an import
// type inside a JSDoc comment sets on the root.
func options(kind core.ScriptKind) storetest.Options {
	if kind == core.ScriptKindJS || kind == core.ScriptKindJSX {
		return storetest.Options{SkipReparsed: true, MaskFlags: ast.NodeFlagsHasJSDoc | ast.NodeFlagsPossiblyContainsDeprecatedTag | ast.NodeFlagsThisNodeHasError | ast.NodeFlagsPossiblyContainsDynamicImport}
	}
	return storetest.Options{}
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
			t.Logf("%s: %d nodes, %d dead", fixture.Name(), file.NodeCount, file.NodeCount-len(nodes))
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
	for _, fixture := range fixtures.ASTBenchFixtures {
		opts, text, kind := fixtureInput(t, fixture)
		inputs[fixture.Name()] = input{opts, text, kind}
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			pointer, file, _ := parseBoth(in.opts, in.text, in.kind)
			m, _ := storetest.Equivalent(pointer, file.Store, storetest.Options{})
			m.Report(t)
			compareFields(t, pointer, file)
		})
	}
}

// compareFields checks the fields of store.File against ast.SourceFile.
func compareFields(t *testing.T, pointer *ast.SourceFile, file *store.File) {
	t.Helper()
	for label, differs := range fieldMismatches(pointer, file) {
		if differs && !(label == "IdentifierCount" && eagerJSDoc(pointer)) {
			t.Errorf("%s differs from the Pointer file", label)
		}
	}
	if eagerJSDoc(pointer) {
		t.Logf("IdentifierCount: Store %d, Pointer %d (the Pointer parser counts the identifiers of the JSDoc it parses eagerly)", file.IdentifierCount, pointer.IdentifierCount)
	}
}

// eagerJSDoc reports whether the Pointer parser may have parsed JSDoc: in a JS
// file always, in a TS file when a comment mentions @see or @link. The
// identifiers of that JSDoc count in its IdentifierCount.
func eagerJSDoc(pointer *ast.SourceFile) bool {
	return pointer.ScriptKind == core.ScriptKindJS || pointer.ScriptKind == core.ScriptKindJSX ||
		strings.Contains(pointer.Text(), "@see") || strings.Contains(pointer.Text(), "@link")
}

func fieldMismatches(pointer *ast.SourceFile, file *store.File) map[string]bool {
	refs := func(a, b []*ast.FileReference) bool {
		return !slices.EqualFunc(a, b, func(x, y *ast.FileReference) bool { return *x == *y })
	}
	m := map[string]bool{
		"FileName":                file.FileName != pointer.FileName(),
		"Path":                    file.Path != pointer.Path(),
		"Text":                    file.Text != pointer.Text(),
		"ScriptKind":              file.ScriptKind != pointer.ScriptKind,
		"LanguageVariant":         file.LanguageVariant != pointer.LanguageVariant,
		"IsDeclarationFile":       file.IsDeclarationFile != pointer.IsDeclarationFile,
		"Flags":                   file.Flags != pointer.Flags || file.Flags != file.Root().Flags(),
		"IdentifierCount":         file.IdentifierCount != pointer.IdentifierCount,
		"CommentDirectives":       !slices.Equal(file.CommentDirectives, pointer.CommentDirectives),
		"Pragmas":                 len(file.Pragmas) != len(pointer.Pragmas),
		"ReferencedFiles":         refs(file.ReferencedFiles, pointer.ReferencedFiles),
		"TypeReferenceDirectives": refs(file.TypeReferenceDirectives, pointer.TypeReferenceDirectives),
		"LibReferenceDirectives":  refs(file.LibReferenceDirectives, pointer.LibReferenceDirectives),
		"CheckJsDirective":        (file.CheckJsDirective == nil) != (pointer.CheckJsDirective == nil) || file.CheckJsDirective != nil && *file.CheckJsDirective != *pointer.CheckJsDirective,
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

// missingFrom returns the elements of want that are not in got in order, or
// nil when got is a subsequence of want.
func missingFrom(want, got []diagnosticKey) []diagnosticKey {
	var missing []diagnosticKey
	j := 0
	for _, w := range want {
		if j < len(got) && got[j] == w {
			j++
		} else {
			missing = append(missing, w)
		}
	}
	if j != len(got) {
		return want // got has something want does not: not a subsequence
	}
	return missing
}

func TestCorpus(t *testing.T) {
	files := corpus(t)
	all := storetest.Mismatches{}
	example := map[string]int{}
	fields := map[string]int{}
	var visits, pointerNodes, storeNodes, excluded, excludedMismatches int
	var excludedNames []string
	diagnosticsSame, diagnosticsSubset, diagnosticsDiffer := 0, 0, 0
	jsMissing := map[string]int{}
	var jsMissingExample []string
	rootFlagDiffs := map[ast.NodeFlags]int{}
	for i, f := range files {
		opts, text, kind := sourceInput(f.name, f.text)
		pointer, file, reparsed := parseBoth(opts, text, kind)
		m, n := storetest.Equivalent(pointer, file.Store, options(kind))
		if reparsed {
			excluded++
			excludedMismatches += len(m)
			if len(excludedNames) < 20 {
				excludedNames = append(excludedNames, fmt.Sprintf("#%d %s", i, f.name))
			}
			continue
		}
		visits += n
		pointerNodes += pointer.NodeCount
		storeNodes += file.NodeCount
		for label, count := range m {
			if all[label] == 0 {
				example[label] = i
			}
			all[label] += count
		}
		for label, differs := range fieldMismatches(pointer, file) {
			if label == "Flags" && options(kind).SkipReparsed {
				// The Pointer parser's JSDoc reparse can set flags on the root too
				// (PossiblyContainsDynamicImport from an import type in a comment).
				if diff := (file.Flags ^ pointer.Flags) &^ options(kind).MaskFlags; diff != 0 {
					rootFlagDiffs[diff]++
				}
				continue
			}
			if differs && !(label == "IdentifierCount" && eagerJSDoc(pointer)) {
				if fields[label] == 0 {
					t.Logf("%s differs first in %s #%d", label, f.name, i)
				}
				fields[label]++
			}
		}
		want, got := diagnosticKeys(pointer.Diagnostics()), diagnosticKeys(file.Diagnostics())
		switch {
		case slices.Equal(want, got):
			diagnosticsSame++
		case options(kind).SkipReparsed && missingFrom(want, got) != nil:
			diagnosticsSubset++
			for _, d := range missingFrom(want, got) {
				key := fmt.Sprintf("code %d %q", d.code, d.message)
				if jsMissing[key] == 0 && len(jsMissingExample) < 40 {
					jsMissingExample = append(jsMissingExample, fmt.Sprintf("%s #%d: %s at %d", f.name, i, key, d.pos))
				}
				jsMissing[key]++
			}
		default:
			diagnosticsDiffer++
			if diagnosticsDiffer <= 10 {
				t.Errorf("diagnostics differ in %s #%d:\n  pointer: %v\n  store:   %v", f.name, i, want, got)
			}
		}
	}
	t.Logf("%d files, %d nodes visited; Pointer NodeCount %d, Store NodeCount %d (dead %d = %.2f%%)",
		len(files), visits, pointerNodes, storeNodes, storeNodes-visits, 100*float64(storeNodes-visits)/float64(visits))
	t.Logf("%d files excluded (top-level await reparse, 7c) with %d mismatches; first: %s", excluded, excludedMismatches, strings.Join(excludedNames, ", "))
	t.Logf("diagnostics: %d files same, %d JS files with a subset, %d differ", diagnosticsSame, diagnosticsSubset, diagnosticsDiffer)
	if len(jsMissing) != 0 {
		var lines []string
		for message, count := range jsMissing {
			lines = append(lines, fmt.Sprintf("%6d  %s", count, message))
		}
		sort.Strings(lines)
		t.Logf("JS diagnostics the Store parser does not report (Pointer reports them from JSDoc):\n%s\nexamples:\n  %s", strings.Join(lines, "\n"), strings.Join(jsMissingExample, "\n  "))
	}
	for diff, count := range rootFlagDiffs {
		t.Logf("root Flags differ by %v in %d JS files", diff, count)
	}
	for label, count := range fields {
		t.Errorf("%s differs in %d files", label, count)
	}
	located := storetest.Mismatches{}
	for label, count := range all {
		located[fmt.Sprintf("%s (first in %s #%d)", label, files[example[label]].name, example[label])] = count
	}
	located.Report(t)
}

// TestDeadNodes checks that a rewound speculation leaves no node behind, and
// documents the constructs that leave one without a rewind.
func TestDeadNodes(t *testing.T) {
	deadNodes := func(name, source string) int {
		file := ParseSourceFile(sourceInput(name, source))
		return file.NodeCount - len(storetest.Preorder(file.Root()))
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
	} {
		name := "/a.ts"
		if strings.HasPrefix(c.source, "<") {
			name = "/a.tsx"
		}
		if dead := deadNodes(name, c.source); dead != c.dead {
			t.Errorf("%q leaves %d dead nodes, want %d", c.source, dead, c.dead)
		}
	}
	for _, fixture := range fixtures.ASTBenchFixtures {
		file := ParseSourceFile(fixtureInput(t, fixture))
		visits := len(storetest.Preorder(file.Root()))
		pointer := parser.ParseSourceFile(fixtureInput(t, fixture))
		t.Logf("%s: Store NodeCount %d, visits %d, dead %d; Pointer NodeCount %d, dead %d",
			fixture.Name(), file.NodeCount, visits, file.NodeCount-visits, pointer.NodeCount, pointer.NodeCount-visits)
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
		p.initializeState(opts, text, kind)
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
