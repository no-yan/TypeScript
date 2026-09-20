package ast_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/filefixture"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/kperf"
)

// astWalker is deliberately tied to the Pointer AST. The callback is bound
// once, before timing starts, so each child edge only performs the walk and
// scalar visit work that this baseline measures.
type astWalker struct {
	visit  ast.Visitor
	visits uint64
}

func newASTWalker() *astWalker {
	w := &astWalker{}
	w.visit = func(node *ast.Node) bool {
		w.walk(node)
		return false
	}
	return w
}

func (w *astWalker) walk(node *ast.Node) {
	if node == nil {
		return
	}
	w.visits++
	node.ForEachChild(w.visit)
}

func (w *astWalker) run(root *ast.Node) uint64 {
	w.visits = 0
	w.walk(root)
	return w.visits
}

func parseBenchmarkFile(tb testing.TB, fixture filefixture.Fixture) *ast.SourceFile {
	tb.Helper()
	opts, source, scriptKind := fixtures.ASTBenchParseInput(tb, fixture)
	return parser.ParseSourceFile(opts, source, scriptKind)
}

func BenchmarkASTWalkV1(b *testing.B) {
	for _, fixture := range fixtures.ASTBenchFixtures {
		b.Run(fixture.Name(), func(b *testing.B) {
			b.ReportAllocs()
			root := parseBenchmarkFile(b, fixture).AsNode()
			walker := newASTWalker()
			// A fixture without a literal baseline has want == 0 and fails below.
			want := astBenchmarkBaselines[fixture.Name()].visits
			runtime.GC()
			walker.run(root)
			var got uint64
			for b.Loop() {
				got = walker.run(root)
			}
			if got != want {
				b.Fatalf("visits = %d, want %d", got, want)
			}
			b.ReportMetric(float64(want), "visits/op")
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(want), "ns/visit")
		})
	}
}

func BenchmarkASTWalkKPCV1(b *testing.B) {
	session := kperf.Start(b)
	defer session.Stop()
	fixture := fixtures.ASTBenchCheckerFixture
	root := parseBenchmarkFile(b, fixture).AsNode()
	walker := newASTWalker()
	want := astBenchmarkBaselines[fixture.Name()].visits
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var got uint64
		session.Measure(func() { got = walker.run(root) })
		if got != want {
			b.Fatalf("visits = %d, want %d", got, want)
		}
	}
	b.StopTimer()
	session.Report()
}

// astVisit is the walk contract: which node is visited, in which order. Kind
// plus Loc distinguishes same-kind siblings, and nesting is recoverable from
// range containment, so exit events and node text are not recorded.
type astVisit struct {
	kind     ast.Kind
	pos, end int
}

// walkVisits is the readable reference walker. TestASTBenchmarkSmallWalk pins
// its output as a literal, and fingerprintAST hashes the same stream.
func walkVisits(root *ast.Node, emit func(astVisit)) {
	var visit ast.Visitor
	var walk func(*ast.Node)
	visit = func(node *ast.Node) bool { walk(node); return false }
	walk = func(node *ast.Node) {
		if node == nil {
			return
		}
		emit(astVisit{node.Kind, node.Loc.Pos(), node.Loc.End()})
		node.ForEachChild(visit)
	}
	walk(root)
}

func fingerprintAST(root *ast.Node) (visits uint64, fingerprint string) {
	h := sha256.New()
	walkVisits(root, func(v astVisit) {
		visits++
		// Kind is written by name so renumbering the enum keeps the baseline.
		fmt.Fprintf(h, "%s|%d|%d\n", v.kind, v.pos, v.end)
	})
	return visits, hex.EncodeToString(h.Sum(nil))
}

// TestASTBenchmarkSmallWalk is the human-readable form of the walk contract.
// The source covers a token child (+), same-kind siblings (left, right), empty
// lists ([] and its call), type arguments, and an optional-chain token.
func TestASTBenchmarkSmallWalk(t *testing.T) {
	const source = `const result = (left + right); empty([]); fn<T>(); fn?.<T>();`
	root := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: "/ast-benchmark-small.ts",
		Path:     "/ast-benchmark-small.ts",
	}, source, core.ScriptKindTS).AsNode()

	var got []astVisit
	walkVisits(root, func(v astVisit) { got = append(got, v) })
	want := []astVisit{
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
	if !slices.Equal(got, want) {
		t.Fatalf("visits = %v, want %v", got, want)
	}
	if visits := newASTWalker().run(root); visits != uint64(len(want)) {
		t.Fatalf("benchmark walker visits = %d, want %d", visits, len(want))
	}
}

type astBenchmarkBaseline struct {
	visits      uint64
	fingerprint string
}

var astBenchmarkBaselines = map[string]astBenchmarkBaseline{
	"checker.ts":         {visits: 298054, fingerprint: "a59b2db86e71f892d323ee3324f97e420c48768d4464066f8fe2a0a48444404a"},
	"dom.generated.d.ts": {visits: 109605, fingerprint: "c77d4af443cb5ccb5fa271cd64e7404a39e144bc783f9e516c82306ab120cf25"},
}

func TestASTBenchmarkFixtures(t *testing.T) {
	for _, fixture := range fixtures.ASTBenchFixtures {
		t.Run(fixture.Name(), func(t *testing.T) {
			root := parseBenchmarkFile(t, fixture).AsNode()
			visits, fingerprint := fingerprintAST(root)
			want := astBenchmarkBaselines[fixture.Name()]
			if visits != want.visits || fingerprint != want.fingerprint {
				t.Fatalf("baseline mismatch: visits=%d fingerprint=%s", visits, fingerprint)
			}
			if walkerVisits := newASTWalker().run(root); walkerVisits != want.visits {
				t.Fatalf("benchmark walker visits = %d, want %d", walkerVisits, want.visits)
			}
		})
	}
}
