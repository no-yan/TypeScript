package store_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"unsafe"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/convert"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/repo"
	"github.com/microsoft/TypeScript/tsc/internal/testrunner"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/filefixture"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// Copied from astBenchmarkBaselines in internal/ast/ast_benchmark_test.go. The
// Store walk must reproduce the Pointer walk contract byte for byte.
var walkBaselines = map[string]struct {
	visits      uint64
	fingerprint string
}{
	"checker.ts":         {visits: 298054, fingerprint: "a59b2db86e71f892d323ee3324f97e420c48768d4464066f8fe2a0a48444404a"},
	"dom.generated.d.ts": {visits: 109605, fingerprint: "c77d4af443cb5ccb5fa271cd64e7404a39e144bc783f9e516c82306ab120cf25"},
}

// Copied from TestASTBenchmarkSmallWalk in internal/ast/ast_benchmark_test.go.
const smallSource = `const result = (left + right); empty([]); fn<T>(); fn?.<T>();`

func parseFixture(tb testing.TB, fixture filefixture.Fixture) *ast.SourceFile {
	tb.Helper()
	opts, source, scriptKind := fixtures.ASTBenchParseInput(tb, fixture)
	return parser.ParseSourceFile(opts, source, scriptKind)
}

func parseSource(name, source string) *ast.SourceFile {
	return parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: name,
		Path:     tspath.Path(name),
	}, source, core.GetScriptKindFromFileName(name))
}

func preorderStore(root store.Node) []store.Node {
	var nodes []store.Node
	var visit store.Visitor
	visit = func(n store.Node) bool {
		nodes = append(nodes, n)
		n.ForEachChild(visit)
		return false
	}
	visit(root)
	return nodes
}

func preorderPointer(root *ast.Node) []*ast.Node {
	var nodes []*ast.Node
	var visit ast.Visitor
	visit = func(n *ast.Node) bool {
		nodes = append(nodes, n)
		n.ForEachChild(visit)
		return false
	}
	visit(root)
	return nodes
}

func TestFingerprint(t *testing.T) {
	for _, fixture := range fixtures.ASTBenchFixtures {
		t.Run(fixture.Name(), func(t *testing.T) {
			nodes := preorderStore(convert.Convert(parseFixture(t, fixture), 0).Root())
			h := sha256.New()
			for _, n := range nodes {
				fmt.Fprintf(h, "%s|%d|%d\n", n.Kind(), n.Pos(), n.End())
			}
			fingerprint := hex.EncodeToString(h.Sum(nil))
			want := walkBaselines[fixture.Name()]
			if uint64(len(nodes)) != want.visits || fingerprint != want.fingerprint {
				t.Fatalf("baseline mismatch: visits=%d fingerprint=%s", len(nodes), fingerprint)
			}
		})
	}
}

func TestSmallWalk(t *testing.T) {
	type visit struct {
		kind     ast.Kind
		pos, end int32
	}
	want := []visit{
		{ast.KindSourceFile, 0, 61},
		{ast.KindVariableStatement, 0, 30},
		{ast.KindVariableDeclarationList, 0, 29},
		{ast.KindVariableDeclaration, 5, 29},
		{ast.KindIdentifier, 5, 12},
		{ast.KindParenthesizedExpression, 14, 29},
		{ast.KindBinaryExpression, 16, 28},
		{ast.KindIdentifier, 16, 20},
		{ast.KindPlusToken, 20, 22},
		{ast.KindIdentifier, 22, 28},
		{ast.KindExpressionStatement, 30, 41},
		{ast.KindCallExpression, 30, 40},
		{ast.KindIdentifier, 30, 36},
		{ast.KindArrayLiteralExpression, 37, 39},
		{ast.KindExpressionStatement, 41, 50},
		{ast.KindCallExpression, 41, 49},
		{ast.KindIdentifier, 41, 44},
		{ast.KindTypeReference, 45, 46},
		{ast.KindIdentifier, 45, 46},
		{ast.KindExpressionStatement, 50, 61},
		{ast.KindCallExpression, 50, 60},
		{ast.KindIdentifier, 50, 53},
		{ast.KindQuestionDotToken, 53, 55},
		{ast.KindTypeReference, 56, 57},
		{ast.KindIdentifier, 56, 57},
		{ast.KindEndOfFile, 61, 61},
	}
	var got []visit
	for _, n := range preorderStore(convert.Convert(parseSource("/ast-benchmark-small.ts", smallSource), 0).Root()) {
		got = append(got, visit{n.Kind(), n.Pos(), n.End()})
	}
	if !slices.Equal(got, want) {
		t.Errorf("visits = %v, want %v", got, want)
	}
}

// mismatches collects every difference by kind and label, so one run shows
// everything the generator's override tables need.
type mismatches map[string]int

func (m mismatches) add(owner *ast.Node, label string) {
	m[owner.Kind.String()+" "+label]++
}

func (m mismatches) node(label string, owner *ast.Node, p *ast.Node, n store.Node) {
	if p == nil && n.IsNil() {
		return
	}
	if p == nil || n.Kind() != p.Kind || int(n.Pos()) != p.Pos() || int(n.End()) != p.End() {
		m.add(owner, label)
	}
}

func (m mismatches) elements(label string, owner *ast.Node, p []*ast.Node, l store.List) {
	if l.Len() != len(p) {
		m.add(owner, label)
		return
	}
	for i, elem := range p {
		m.node(label+" element", owner, elem, l.At(i))
	}
}

func (m mismatches) list(label string, owner *ast.Node, p *ast.NodeList, l store.List) {
	if p == nil && l.IsNil() {
		return
	}
	if p == nil || l.IsNil() || int(l.Pos()) != p.Pos() || int(l.End()) != p.End() {
		m.add(owner, label)
		return
	}
	m.elements(label, owner, p.Nodes, l)
}

func (m mismatches) modifiers(label string, owner *ast.Node, p *ast.ModifierList, l store.List) {
	if p == nil {
		m.list(label, owner, nil, l)
	} else {
		m.list(label, owner, &p.NodeList, l)
	}
}

// A raw list has no Loc.
func (m mismatches) raw(label string, owner *ast.Node, p []*ast.Node, l store.List) {
	if p == nil && l.IsNil() {
		return
	}
	if p == nil || l.IsNil() || l.Pos() != 0 || l.End() != 0 {
		m.add(owner, label)
		return
	}
	m.elements(label, owner, p, l)
}

// guard runs a comparison whose Pointer side may panic on this kind.
func (m mismatches) guard(owner *ast.Node, label string, compare func()) {
	defer func() {
		if recover() != nil {
			m.add(owner, label+" (Pointer panics)")
		}
	}()
	compare()
}

// equivalent walks the Pointer AST and its Store in preorder together and
// compares every node: header, parent, Ref round trip, then every member of
// its definition and every role accessor (equivalence_generated_test.go).
func equivalent(file *ast.SourceFile) (m mismatches, visits int) {
	s := convert.Convert(file, 0)
	pointers := preorderPointer(file.AsNode())
	nodes := preorderStore(s.Root())
	m = mismatches{}
	if len(pointers) != len(nodes) {
		m[fmt.Sprintf("visits %d, want %d", len(nodes), len(pointers))]++
		return m, len(nodes)
	}
	for i, p := range pointers {
		n := nodes[i]
		m.node("self", p, p, n)
		m.node("Parent", p, p.Parent, n.Parent())
		if n.Flags() != p.Flags {
			m.add(p, "Flags")
		}
		if n.ModifierFlags() != p.ModifierFlags()&0xFFFF {
			m.add(p, "ModifierFlags")
		}
		if s.NodeAt(n.Ref()) != n {
			m.add(p, "Ref round trip")
		}
		compareMembers(m, p, n)
		compareRoles(m, p, n)
	}
	return m, len(nodes)
}

func (m mismatches) report(t *testing.T) {
	t.Helper()
	if len(m) == 0 {
		return
	}
	var lines []string
	for label, count := range m {
		lines = append(lines, fmt.Sprintf("%s: %d", label, count))
	}
	sort.Strings(lines)
	t.Errorf("Store differs from Pointer:\n%s", strings.Join(lines, "\n"))
}

func TestEquivalence(t *testing.T) {
	t.Logf("comparing %d members and %d roles per kind", comparedMembers, comparedRoles)
	files := map[string]*ast.SourceFile{"small": parseSource("/ast-benchmark-small.ts", smallSource)}
	for _, fixture := range fixtures.ASTBenchFixtures {
		files[fixture.Name()] = parseFixture(t, fixture)
	}
	for name, file := range files {
		t.Run(name, func(t *testing.T) {
			m, _ := equivalent(file)
			m.report(t)
		})
	}
}

type corpusFile struct {
	name, text string
}

// corpus is the seed corpus of FuzzParser in internal/parser/parser_test.go:
// every parsable file under testdata/fixtures and, unless -short, every unit
// of the compiler and conformance test cases.
func corpus(t *testing.T) []corpusFile {
	t.Helper()
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
			t.Fatal(err)
		}
		return true
	}
	if !each(filepath.Join(repo.TestDataPath(), "fixtures"), add) {
		t.Fatal("testdata/fixtures is missing")
	}
	if testing.Short() {
		return files
	}
	for _, dir := range []string{"tests/cases/compiler", "tests/cases/conformance"} {
		found := each(filepath.Join(repo.TestDataPath(), dir), func(path, text string) {
			units, _, _, _, err := testrunner.ParseTestFilesAndSymlinks(text, path,
				func(name, content string, _ map[string]string) (corpusFile, error) {
					return corpusFile{name, content}, nil
				})
			if err != nil {
				t.Fatal(err)
			}
			for _, unit := range units {
				add(unit.name, unit.text)
			}
		})
		if !found {
			t.Logf("skipped: testdata/%s is missing", dir)
		}
	}
	return files
}

func TestCorpus(t *testing.T) {
	files := corpus(t)
	all := mismatches{}
	example := map[string]int{}
	var visits int
	for i, f := range files {
		m, n := equivalent(parseSource(f.name, f.text))
		visits += n
		for label, count := range m {
			if all[label] == 0 {
				example[label] = i
			}
			all[label] += count
		}
	}
	t.Logf("%d files, %d nodes", len(files), visits)
	located := mismatches{}
	for label, count := range all {
		located[fmt.Sprintf("%s (first in %s #%d)", label, files[example[label]].name, example[label])] = count
	}
	located.report(t)
}

// An escaped identifier and a literal with an escape have texts that are not
// substrings of the source.
func TestTexts(t *testing.T) {
	file := parseSource("/a.ts", "\\u0061bc; abc; 'a\\nb'; 'cd';")
	s := convert.Convert(file, 0)
	var identifiers, literals []string
	var inTexts int
	for _, n := range preorderStore(s.Root()) {
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
	if _, _, texts := s.Footprint(); texts != len("abc")+len("a\nb")+len("cd") {
		t.Errorf("texts holds %d bytes, want %d", texts, len("abc")+len("a\nb")+len("cd"))
	}
}

func TestLayout(t *testing.T) {
	if size := unsafe.Sizeof(store.NodeHeader{}); size != 24 {
		t.Errorf("NodeHeader is %d bytes, want 24", size)
	}
	if ast.KindCount > 512 {
		t.Errorf("KindCount = %d does not fit the 512-entry tables", ast.KindCount)
	}
	if words, mask := store.Shape(ast.KindCallExpression); words != 4 || mask != 0b1100 {
		t.Errorf("shape of CallExpression = {%d %#b}, want {4 0b1100}", words, mask)
	}
	if words, mask := store.Shape(ast.KindIdentifier); words != 0 || mask != 0 {
		t.Errorf("shape of Identifier = {%d %#b}, want {0 0}", words, mask)
	}
}

func TestStoreFootprint(t *testing.T) {
	for _, fixture := range fixtures.ASTBenchFixtures {
		s := convert.Convert(parseFixture(t, fixture), 0)
		nodes, extra, texts := s.Footprint()
		t.Logf("%s: %d nodes, headers %d B, extra %d B, texts %d B, total %.2f B/node",
			fixture.Name(), s.NodeCount(), nodes, extra, texts, float64(nodes+extra)/float64(s.NodeCount()))
	}
}
