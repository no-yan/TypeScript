//go:build storeexp

package storeexp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"unsafe"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/filefixture"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
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

func parseSmall() *ast.SourceFile {
	return parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: "/ast-benchmark-small.ts",
		Path:     "/ast-benchmark-small.ts",
	}, smallSource, core.ScriptKindTS)
}

type forEachChild func(Node, Visitor) bool

var forEachChildVariants = []struct {
	name string
	each forEachChild
}{
	{"switch", Node.ForEachChild},
	{"shape", Node.ForEachChildShape},
}

func preorderStore(root Node, each forEachChild) []Node {
	var nodes []Node
	var visit Visitor
	visit = func(n Node) bool {
		nodes = append(nodes, n)
		each(n, visit)
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
		store := Convert(parseFixture(t, fixture), 0)
		want := walkBaselines[fixture.Name()]
		for _, variant := range forEachChildVariants {
			t.Run(fixture.Name()+"/"+variant.name, func(t *testing.T) {
				nodes := preorderStore(store.Root(), variant.each)
				h := sha256.New()
				for _, n := range nodes {
					fmt.Fprintf(h, "%s|%d|%d\n", n.Kind(), n.Pos(), n.End())
				}
				fingerprint := hex.EncodeToString(h.Sum(nil))
				if uint64(len(nodes)) != want.visits || fingerprint != want.fingerprint {
					t.Fatalf("baseline mismatch: visits=%d fingerprint=%s", len(nodes), fingerprint)
				}
			})
		}
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
	store := Convert(parseSmall(), 0)
	for _, variant := range forEachChildVariants {
		var got []visit
		for _, n := range preorderStore(store.Root(), variant.each) {
			got = append(got, visit{n.Kind(), n.Pos(), n.End()})
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: visits = %v, want %v", variant.name, got, want)
		}
	}
}

// mismatches collects every difference by kind and label, so one run shows
// everything the generator's override tables need.
type mismatches map[string]int

func (m mismatches) node(label string, owner *ast.Node, p *ast.Node, n Node) {
	if p == nil && n.IsNil() {
		return
	}
	if p == nil || n.Kind() != p.Kind || int(n.Pos()) != p.Pos() || int(n.End()) != p.End() {
		m[owner.Kind.String()+" "+label]++
	}
}

func (m mismatches) list(label string, owner *ast.Node, p *ast.NodeList, l List) {
	if p == nil && l.IsNil() {
		return
	}
	if p == nil || l.IsNil() || l.Len() != len(p.Nodes) || int(l.Pos()) != p.Pos() || int(l.End()) != p.End() {
		m[owner.Kind.String()+" "+label]++
		return
	}
	for i, elem := range p.Nodes {
		m.node(label+" element", owner, elem, l.At(i))
	}
}

func (m mismatches) modifiers(label string, owner *ast.Node, p *ast.ModifierList, l List) {
	if p == nil {
		m.list(label, owner, nil, l)
	} else {
		m.list(label, owner, &p.NodeList, l)
	}
}

func TestEquivalence(t *testing.T) {
	files := map[string]*ast.SourceFile{"small": parseSmall()}
	for _, fixture := range fixtures.ASTBenchFixtures {
		files[fixture.Name()] = parseFixture(t, fixture)
	}
	for name, file := range files {
		t.Run(name, func(t *testing.T) {
			store := Convert(file, 0)
			pointers := preorderPointer(file.AsNode())
			nodes := preorderStore(store.Root(), Node.ForEachChild)
			if len(pointers) != len(nodes) {
				t.Fatalf("visits = %d, want %d", len(nodes), len(pointers))
			}
			m := mismatches{}
			for i, p := range pointers {
				n := nodes[i]
				m.node("self", p, p, n)
				m.node("Parent", p, p.Parent, n.Parent())
				if n.Flags() != p.Flags {
					m[p.Kind.String()+" Flags"]++
				}
				if n.ModifierFlags() != p.ModifierFlags()&0xFFFF {
					m[p.Kind.String()+" ModifierFlags"]++
				}
				if store.node(n.Ref()) != n {
					m[p.Kind.String()+" Ref round trip"]++
				}

				kind := p.Kind & 511
				m.node("Name", p, p.Name(), n.Name())
				if expressionSlot[kind] != 0xFF {
					m.node("Expression", p, p.Expression(), n.Expression())
				}
				if typeSlot[kind] != 0xFF {
					m.node("Type", p, p.Type(), n.Type())
				}
				if initializerSlot[kind] != 0xFF {
					m.node("Initializer", p, p.Initializer(), n.Initializer())
				}
				if bodySlot[kind] != 0xFF {
					m.node("Body", p, p.Body(), n.Body())
				}

				switch p.Kind {
				case ast.KindIdentifier:
					if got, want := n.AsIdentifier().Text(), p.AsIdentifier().Text; got != want {
						t.Errorf("Identifier at %d: Text = %q, want %q", p.Pos(), got, want)
					}
				case ast.KindBinaryExpression:
					pv, sv := p.AsBinaryExpression(), n.AsBinaryExpression()
					m.modifiers("view Modifiers", p, pv.Modifiers(), sv.Modifiers())
					m.node("view Left", p, pv.Left, sv.Left())
					m.node("view Type", p, pv.Type, sv.Type())
					m.node("view OperatorToken", p, pv.OperatorToken, sv.OperatorToken())
					m.node("view Right", p, pv.Right, sv.Right())
				case ast.KindCallExpression:
					pv, sv := p.AsCallExpression(), n.AsCallExpression()
					m.node("view Expression", p, pv.Expression, sv.Expression())
					m.node("view QuestionDotToken", p, pv.QuestionDotToken, sv.QuestionDotToken())
					m.list("view TypeArguments", p, pv.TypeArguments, sv.TypeArguments())
					m.list("view Arguments", p, pv.Arguments, sv.Arguments())
				case ast.KindPropertyAccessExpression:
					pv, sv := p.AsPropertyAccessExpression(), n.AsPropertyAccessExpression()
					m.node("view Expression", p, pv.Expression, sv.Expression())
					m.node("view QuestionDotToken", p, pv.QuestionDotToken, sv.QuestionDotToken())
					m.node("view Name", p, pv.Name(), sv.Name())
				case ast.KindFunctionDeclaration:
					pv, sv := p.AsFunctionDeclaration(), n.AsFunctionDeclaration()
					m.modifiers("view Modifiers", p, pv.Modifiers(), sv.Modifiers())
					m.node("view AsteriskToken", p, pv.AsteriskToken, sv.AsteriskToken())
					m.node("view Name", p, pv.Name(), sv.Name())
					m.list("view TypeParameters", p, pv.TypeParameters, sv.TypeParameters())
					m.list("view Parameters", p, pv.Parameters, sv.Parameters())
					m.node("view Type", p, pv.Type, sv.Type())
					m.node("view FullSignature", p, pv.FullSignature, sv.FullSignature())
					m.node("view Body", p, pv.Body, sv.Body())
				case ast.KindSourceFile:
					pv, sv := p.AsSourceFile(), n.AsSourceFile()
					m.list("view Statements", p, pv.Statements, sv.Statements())
					m.node("view EndOfFileToken", p, pv.EndOfFileToken, sv.EndOfFileToken())
				}
			}
			if len(m) != 0 {
				var lines []string
				for label, count := range m {
					lines = append(lines, fmt.Sprintf("%s: %d", label, count))
				}
				sort.Strings(lines)
				t.Fatalf("Store differs from Pointer:\n%s", strings.Join(lines, "\n"))
			}
		})
	}
}

// An escaped identifier's text is not a substring of the source.
func TestEscapedIdentifierText(t *testing.T) {
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/a.ts", Path: "/a.ts"}, "\\"+"u0061bc; abc;", core.ScriptKindTS)
	var texts []string
	var inTexts int
	for _, n := range preorderStore(Convert(file, 0).Root(), Node.ForEachChild) {
		if n.Kind() == ast.KindIdentifier {
			texts = append(texts, n.Text())
			if n.h.data&textInTexts != 0 {
				inTexts++
			}
		}
	}
	if !slices.Equal(texts, []string{"abc", "abc"}) || inTexts != 1 {
		t.Fatalf("texts = %q with %d outside the source, want [abc abc] with 1", texts, inTexts)
	}
}

func TestNodeHeaderSize(t *testing.T) {
	if size := unsafe.Sizeof(NodeHeader{}); size != 24 {
		t.Fatalf("NodeHeader is %d bytes, want 24", size)
	}
}

// The hand-written slot constants in views.go must agree with the generated
// tables: the slot count, which slots are lists, and the role slots.
func TestSlotConstants(t *testing.T) {
	for _, tt := range []struct {
		kind  ast.Kind
		slots int
		lists []int
	}{
		{ast.KindIdentifier, 0, nil},
		{ast.KindPlusToken, 0, nil},
		{ast.KindBinaryExpression, binaryRightSlot + 1, []int{binaryModifiersSlot}},
		{ast.KindCallExpression, callArgumentsSlot + 1, []int{callTypeArgumentsSlot, callArgumentsSlot}},
		{ast.KindPropertyAccessExpression, propertyAccessNameSlot + 1, nil},
		{ast.KindFunctionDeclaration, functionBodySlot + 1, []int{functionModifiersSlot, functionTypeParametersSlot, functionParametersSlot}},
		{ast.KindSourceFile, sourceFileEndOfFileTokenSlot + 1, []int{sourceFileStatementsSlot}},
	} {
		var mask uint16
		for _, slot := range tt.lists {
			mask |= 1 << slot
		}
		if got := shapes[tt.kind]; got != (shape{uint8(tt.slots), mask}) {
			t.Errorf("shapes[%v] = %+v, want {%d %#b}", tt.kind, got, tt.slots, mask)
		}
	}
	for _, tt := range []struct {
		name  string
		table *[512]uint8
		kind  ast.Kind
		want  uint8
	}{
		{"expressionSlot", &expressionSlot, ast.KindCallExpression, callExpressionSlot},
		{"expressionSlot", &expressionSlot, ast.KindPropertyAccessExpression, propertyAccessExpressionSlot},
		{"nameSlot", &nameSlot, ast.KindPropertyAccessExpression, propertyAccessNameSlot},
		{"nameSlot", &nameSlot, ast.KindFunctionDeclaration, functionNameSlot},
		{"typeSlot", &typeSlot, ast.KindFunctionDeclaration, functionTypeSlot},
		{"typeSlot", &typeSlot, ast.KindBinaryExpression, binaryTypeSlot},
		{"bodySlot", &bodySlot, ast.KindFunctionDeclaration, functionBodySlot},
		{"nameSlot", &nameSlot, ast.KindCallExpression, 0xFF},
		{"nameSlot", &nameSlot, ast.KindIdentifier, 0xFF},
	} {
		if got := tt.table[tt.kind]; got != tt.want {
			t.Errorf("%s[%v] = %d, want %d", tt.name, tt.kind, got, tt.want)
		}
	}
}

func TestStoreExpFootprint(t *testing.T) {
	for _, fixture := range fixtures.ASTBenchFixtures {
		store := Convert(parseFixture(t, fixture), 0)
		count := len(store.nodes) - 1
		nodes, extra := len(store.nodes)*24, len(store.extra)*4
		t.Logf("%s: %d nodes, headers %d B, extra %d B, texts %d B, total %.2f B/node",
			fixture.Name(), count, nodes, extra, len(store.texts), float64(nodes+extra)/float64(count))
	}
}
