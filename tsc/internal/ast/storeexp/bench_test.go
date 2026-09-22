//go:build storeexp

package storeexp

import (
	"runtime"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/fixtures"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/kperf"
)

// measured is one benchmark body. run returns a checksum that must equal want.
type measured struct {
	name string
	run  func() uint64
	want uint64
	// count of per ("visit", "access") happen in one run, reported under
	// countUnit. detail is an optional second count, reported as detail+"/op".
	per, countUnit string
	count          uint64
	detail         string
	detailCount    uint64
}

func (m measured) report(b *testing.B) {
	b.ReportMetric(float64(m.count), m.countUnit)
	if m.detail != "" {
		b.ReportMetric(float64(m.detailCount), m.detail+"/op")
	}
}

func (m measured) bench(b *testing.B) {
	b.ReportAllocs()
	runtime.GC()
	m.run()
	var got uint64
	for b.Loop() {
		got = m.run()
	}
	if got != m.want {
		b.Fatalf("checksum = %d, want %d", got, m.want)
	}
	m.report(b)
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(m.count), "ns/"+m.per)
}

// benchKPC has the form of BenchmarkASTWalkKPCV1.
func (m measured) benchKPC(b *testing.B) {
	session := kperf.Start(b)
	defer session.Stop()
	runtime.GC()
	m.run()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var got uint64
		session.Measure(func() { got = m.run() })
		if got != m.want {
			b.Fatalf("checksum = %d, want %d", got, m.want)
		}
	}
	b.StopTimer()
	session.Report()
	m.report(b)
}

// Walk

// pointerWalker is astWalker from internal/ast/ast_benchmark_test.go: the
// callback is bound once, and a visit only counts.
type pointerWalker struct {
	visit  ast.Visitor
	visits uint64
}

func newPointerWalker() *pointerWalker {
	w := &pointerWalker{}
	w.visit = func(node *ast.Node) bool {
		w.walk(node)
		return false
	}
	return w
}

func (w *pointerWalker) walk(node *ast.Node) {
	if node == nil {
		return
	}
	w.visits++
	node.ForEachChild(w.visit)
}

func (w *pointerWalker) run(root *ast.Node) uint64 {
	w.visits = 0
	w.walk(root)
	return w.visits
}

// The Store walkers have no nil check: ForEachChild never passes the nil node.

type switchWalker struct {
	visit  Visitor
	visits uint64
}

func newSwitchWalker() *switchWalker {
	w := &switchWalker{}
	w.visit = func(n Node) bool {
		w.walk(n)
		return false
	}
	return w
}

func (w *switchWalker) walk(n Node) {
	w.visits++
	n.ForEachChild(w.visit)
}

func (w *switchWalker) run(root Node) uint64 {
	w.visits = 0
	w.walk(root)
	return w.visits
}

type shapeWalker struct {
	visit  Visitor
	visits uint64
}

func newShapeWalker() *shapeWalker {
	w := &shapeWalker{}
	w.visit = func(n Node) bool {
		w.walk(n)
		return false
	}
	return w
}

func (w *shapeWalker) walk(n Node) {
	w.visits++
	n.ForEachChildShape(w.visit)
}

func (w *shapeWalker) run(root Node) uint64 {
	w.visits = 0
	w.walk(root)
	return w.visits
}

func walkBenchmarks(file *ast.SourceFile, visits uint64) []measured {
	root, storeRoot := file.AsNode(), Convert(file, 0).Root()
	pointer, switched, shaped := newPointerWalker(), newSwitchWalker(), newShapeWalker()
	m := measured{want: visits, per: "visit", countUnit: "visits/op", count: visits}
	runs := []measured{m, m, m}
	runs[0].name, runs[0].run = "pointer", func() uint64 { return pointer.run(root) }
	runs[1].name, runs[1].run = "switch", func() uint64 { return switched.run(storeRoot) }
	runs[2].name, runs[2].run = "shape", func() uint64 { return shaped.run(storeRoot) }
	return runs
}

func BenchmarkStoreExpWalkV1(b *testing.B) {
	for _, fixture := range fixtures.ASTBenchFixtures {
		b.Run(fixture.Name(), func(b *testing.B) {
			for _, m := range walkBenchmarks(parseFixture(b, fixture), walkBaselines[fixture.Name()].visits) {
				b.Run(m.name, m.bench)
			}
		})
	}
}

func BenchmarkStoreExpWalkKPCV1(b *testing.B) {
	fixture := fixtures.ASTBenchCheckerFixture
	for _, m := range walkBenchmarks(parseFixture(b, fixture), walkBaselines[fixture.Name()].visits) {
		b.Run(m.name, m.benchKPC)
	}
}

// Access
//
// An operation is a named function so that the benchmark loop inlines it and
// asm_test.go can wrap the same body for objdump. The Pointer side uses the
// current public API as it is; it is not rewritten to be faster.

func opBaselinePointer(n *ast.Node) uint64 {
	if n != nil {
		return 1
	}
	return 0
}

func opBaselineStore(n Node) uint64 {
	if n.h != nil {
		return 1
	}
	return 0
}

func opHeaderPointer(n *ast.Node) uint64 {
	return uint64(n.Kind) + uint64(n.Flags) + uint64(n.Pos()) + uint64(n.End())
}

func opHeaderStore(n Node) uint64 {
	return uint64(n.Kind()) + uint64(n.Flags()) + uint64(n.Pos()) + uint64(n.End())
}

func opPolyPresentPointer(n *ast.Node) uint64 { return uint64(n.Name().Kind) }
func opPolyPresentStore(n Node) uint64        { return uint64(n.Name().Kind()) }

func opPolyAllPointer(n *ast.Node) uint64 {
	if name := n.Name(); name != nil {
		return uint64(name.Kind)
	}
	return 0
}

// The nil node's kind is 0, so the Store side needs no branch.
func opPolyAllStore(n Node) uint64 { return uint64(n.Name().Kind()) }

func opTextPointer(n *ast.Node) uint64 {
	text := n.Text()
	if len(text) == 0 {
		return 0
	}
	return uint64(len(text)) + uint64(text[0])
}

func opTextStore(n Node) uint64 {
	text := n.Text()
	if len(text) == 0 {
		return 0
	}
	return uint64(len(text)) + uint64(text[0])
}

func opParentChainPointer(n *ast.Node) (depth uint64) {
	for p := n.Parent; p != nil; p = p.Parent {
		depth++
	}
	return depth
}

func opParentChainStore(n Node) (depth uint64) {
	for p := n.Parent(); !p.IsNil(); p = p.Parent() {
		depth++
	}
	return depth
}

// opParentChainRefStore is the way of writing the loop that the verification
// instructions (section 5, E11) ask to compare: walk by ref and keep nodes local.
func opParentChainRefStore(n Node) (depth uint64) {
	nodes := n.s.nodes
	for ref := n.h.parent; ref != 0; ref = nodes[ref].parent {
		depth++
	}
	return depth
}

func opRefRoundTripStore(n Node) uint64 { return uint64(n.Ref()) }

// One loop per operation: a loop that took the operation as a func value would
// not inline it.
//
// known, list and modflags have no op function. As functions, the Store side of
// known (inline cost 223) and list (82) and the Pointer side of modflags (81)
// exceed the inline budget of 80, which would charge one side a call per
// access. Their loops spell the body out, and asm_test.go repeats it.

func sumBaselinePointer(nodes []*ast.Node) (sum uint64) {
	for _, n := range nodes {
		sum += opBaselinePointer(n)
	}
	return sum
}

func sumBaselineStore(nodes []Node) (sum uint64) {
	for _, n := range nodes {
		sum += opBaselineStore(n)
	}
	return sum
}

func sumHeaderPointer(nodes []*ast.Node) (sum uint64) {
	for _, n := range nodes {
		sum += opHeaderPointer(n)
	}
	return sum
}

func sumHeaderStore(nodes []Node) (sum uint64) {
	for _, n := range nodes {
		sum += opHeaderStore(n)
	}
	return sum
}

func sumKnownPointer(nodes []*ast.Node) (sum uint64) {
	for _, n := range nodes {
		switch n.Kind {
		case ast.KindCallExpression:
			sum += uint64(n.AsCallExpression().Expression.Kind)
		case ast.KindBinaryExpression:
			b := n.AsBinaryExpression()
			sum += uint64(b.Left.Kind) + uint64(b.OperatorToken.Kind) + uint64(b.Right.Kind)
		default:
			p := n.AsPropertyAccessExpression()
			sum += uint64(p.Expression.Kind) + uint64(p.Name().Kind)
		}
	}
	return sum
}

func sumKnownStore(nodes []Node) (sum uint64) {
	for _, n := range nodes {
		switch n.Kind() {
		case ast.KindCallExpression:
			sum += uint64(n.AsCallExpression().Expression().Kind())
		case ast.KindBinaryExpression:
			b := n.AsBinaryExpression()
			sum += uint64(b.Left().Kind()) + uint64(b.OperatorToken().Kind()) + uint64(b.Right().Kind())
		default:
			p := n.AsPropertyAccessExpression()
			sum += uint64(p.Expression().Kind()) + uint64(p.Name().Kind())
		}
	}
	return sum
}

func sumPolyPresentPointer(nodes []*ast.Node) (sum uint64) {
	for _, n := range nodes {
		sum += opPolyPresentPointer(n)
	}
	return sum
}

func sumPolyPresentStore(nodes []Node) (sum uint64) {
	for _, n := range nodes {
		sum += opPolyPresentStore(n)
	}
	return sum
}

func sumPolyAllPointer(nodes []*ast.Node) (sum uint64) {
	for _, n := range nodes {
		sum += opPolyAllPointer(n)
	}
	return sum
}

func sumPolyAllStore(nodes []Node) (sum uint64) {
	for _, n := range nodes {
		sum += opPolyAllStore(n)
	}
	return sum
}

func sumListPointer(nodes []*ast.Node) (sum uint64) {
	for _, n := range nodes {
		for _, arg := range n.AsCallExpression().Arguments.Nodes {
			sum += uint64(arg.Kind)
		}
	}
	return sum
}

func sumListStore(nodes []Node) (sum uint64) {
	for _, n := range nodes {
		for _, ref := range n.AsCallExpression().Arguments().Refs() {
			sum += uint64(n.s.node(ref).Kind())
		}
	}
	return sum
}

func sumTextPointer(nodes []*ast.Node) (sum uint64) {
	for _, n := range nodes {
		sum += opTextPointer(n)
	}
	return sum
}

func sumTextStore(nodes []Node) (sum uint64) {
	for _, n := range nodes {
		sum += opTextStore(n)
	}
	return sum
}

func sumModFlagsPointer(nodes []*ast.Node) (sum uint64) {
	for _, n := range nodes {
		sum += uint64(n.ModifierFlags())
	}
	return sum
}

func sumModFlagsStore(nodes []Node) (sum uint64) {
	for _, n := range nodes {
		sum += uint64(n.ModifierFlags())
	}
	return sum
}

func sumParentChainPointer(nodes []*ast.Node) (sum uint64) {
	for _, n := range nodes {
		sum += opParentChainPointer(n)
	}
	return sum
}

func sumParentChainStore(nodes []Node) (sum uint64) {
	for _, n := range nodes {
		sum += opParentChainStore(n)
	}
	return sum
}

func sumParentChainRefStore(nodes []Node) (sum uint64) {
	for _, n := range nodes {
		sum += opParentChainRefStore(n)
	}
	return sum
}

func sumRefRoundTripStore(nodes []Node) (sum uint64) {
	for _, n := range nodes {
		sum += opRefRoundTripStore(n)
	}
	return sum
}

// accessBenchmarks returns "<operation>/pointer" and "<operation>/store" for
// each operation, after checking that both sides produce the same checksum.
func accessBenchmarks(tb testing.TB) []measured {
	file := parseFixture(tb, fixtures.ASTBenchCheckerFixture)
	var all, known, named, calls, identifiers struct {
		pointer []*ast.Node
		store   []Node
	}
	var arguments, knownReads uint64
	pointers := preorderPointer(file.AsNode())
	for i, n := range preorderStore(Convert(file, 0).Root(), Node.ForEachChild) {
		p := pointers[i]
		for _, subset := range []struct {
			dst *struct {
				pointer []*ast.Node
				store   []Node
			}
			in bool
		}{
			{&all, true},
			{&known, p.Kind == ast.KindCallExpression || p.Kind == ast.KindBinaryExpression || p.Kind == ast.KindPropertyAccessExpression},
			{&named, p.Name() != nil},
			{&calls, p.Kind == ast.KindCallExpression},
			{&identifiers, p.Kind == ast.KindIdentifier},
		} {
			if subset.in {
				subset.dst.pointer = append(subset.dst.pointer, p)
				subset.dst.store = append(subset.dst.store, n)
			}
		}
		switch p.Kind {
		case ast.KindCallExpression:
			arguments += uint64(len(p.AsCallExpression().Arguments.Nodes))
			knownReads++
		case ast.KindBinaryExpression:
			knownReads += 3
		case ast.KindPropertyAccessExpression:
			knownReads += 2
		}
	}

	var out []measured
	add := func(name string, count int, pointer, store func() uint64, detail string, detailCount uint64) {
		want := store()
		if pointer != nil && pointer() != want {
			tb.Fatalf("%s: Store checksum = %d, Pointer = %d", name, want, pointer())
		}
		m := measured{want: want, per: "access", countUnit: "ops/op", count: uint64(count), detail: detail, detailCount: detailCount}
		if pointer != nil {
			m.name, m.run = name+"/pointer", pointer
			out = append(out, m)
		}
		m.name, m.run = name+"/store", store
		out = append(out, m)
	}
	add("baseline", len(all.store),
		func() uint64 { return sumBaselinePointer(all.pointer) },
		func() uint64 { return sumBaselineStore(all.store) }, "", 0)
	add("header", len(all.store),
		func() uint64 { return sumHeaderPointer(all.pointer) },
		func() uint64 { return sumHeaderStore(all.store) }, "", 0)
	add("known", len(known.store),
		func() uint64 { return sumKnownPointer(known.pointer) },
		func() uint64 { return sumKnownStore(known.store) }, "childreads", knownReads)
	add("polyPresent", len(named.store),
		func() uint64 { return sumPolyPresentPointer(named.pointer) },
		func() uint64 { return sumPolyPresentStore(named.store) }, "", 0)
	add("polyAll", len(all.store),
		func() uint64 { return sumPolyAllPointer(all.pointer) },
		func() uint64 { return sumPolyAllStore(all.store) }, "", 0)
	add("list", len(calls.store),
		func() uint64 { return sumListPointer(calls.pointer) },
		func() uint64 { return sumListStore(calls.store) }, "elements", arguments)
	add("text", len(identifiers.store),
		func() uint64 { return sumTextPointer(identifiers.pointer) },
		func() uint64 { return sumTextStore(identifiers.store) }, "", 0)
	add("modflags", len(all.store),
		func() uint64 { return sumModFlagsPointer(all.pointer) },
		func() uint64 { return sumModFlagsStore(all.store) }, "", 0)
	add("parentChain", len(identifiers.store),
		func() uint64 { return sumParentChainPointer(identifiers.pointer) },
		func() uint64 { return sumParentChainStore(identifiers.store) }, "steps", sumParentChainPointer(identifiers.pointer))
	add("parentChainRef", len(identifiers.store), nil,
		func() uint64 { return sumParentChainRefStore(identifiers.store) }, "steps", sumParentChainPointer(identifiers.pointer))
	add("refRoundTrip", len(all.store), nil,
		func() uint64 { return sumRefRoundTripStore(all.store) }, "", 0)
	return out
}

func BenchmarkStoreExpAccess(b *testing.B) {
	for _, m := range accessBenchmarks(b) {
		b.Run(m.name, m.bench)
	}
}

func BenchmarkStoreExpAccessKPC(b *testing.B) {
	for _, m := range accessBenchmarks(b) {
		b.Run(m.name, m.benchKPC)
	}
}
