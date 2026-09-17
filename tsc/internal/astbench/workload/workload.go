// Package workload contains deterministic, child-edge AST traversal workloads.
package workload

import (
	_ "embed"
	"errors"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

const (
	CaseFullTree               = "full-tree"
	CaseExpression             = "expression"
	ShapeWide                  = "wide"
	ShapeDeep                  = "deep"
	ShapeMixed                 = "mixed"
	ShapeFixture               = "fixture"
	LayoutConstruction         = "construction"
	RepresentationStore        = "store"
	RepresentationPointer      = "pointer"
	RepresentationPointerModel = RepresentationPointer
)

type Config struct {
	Case, Shape            string
	Nodes                  int
	Seed, LayoutSeed       int64
	Layout, Representation string
}

func DefaultConfig() Config {
	return Config{Case: CaseFullTree, Shape: ShapeMixed, Nodes: 1024, Seed: 1, LayoutSeed: 1, Layout: LayoutConstruction, Representation: RepresentationStore}
}
func Validate(c Config) error {
	if c.Case != CaseFullTree && c.Case != CaseExpression {
		return fmt.Errorf("workload: unsupported case %q", c.Case)
	}
	if c.Shape != ShapeWide && c.Shape != ShapeDeep && c.Shape != ShapeMixed && c.Shape != ShapeFixture {
		return fmt.Errorf("workload: unsupported shape %q", c.Shape)
	}
	if c.Layout != LayoutConstruction {
		return fmt.Errorf("workload: unsupported layout %q", c.Layout)
	}
	if c.LayoutSeed != 0 && c.LayoutSeed != 1 {
		return errors.New("workload: layout seed requires an implemented non-construction layout")
	}
	if c.Representation != RepresentationStore && c.Representation != RepresentationPointer {
		return fmt.Errorf("workload: unsupported representation %q", c.Representation)
	}
	if c.Shape == ShapeFixture {
		if c.Nodes != 0 {
			return errors.New("workload: fixture node count is discovered; set Nodes to zero")
		}
		if c.Representation != RepresentationStore {
			return errors.New("workload: fixture is Store-only; parser has no pointer AST mode")
		}
		return nil
	}
	if c.Nodes < 1 || c.Nodes > 1000000 {
		return fmt.Errorf("workload: Nodes must be in [1,1000000], got %d", c.Nodes)
	}
	if c.Shape == ShapeDeep && c.Nodes > 8192 {
		return fmt.Errorf("workload: deep Nodes must be <= 8192, got %d", c.Nodes)
	}
	return nil
}

type Sample struct {
	NsPerOp, BytesPerOp, AllocsPerOp                                                                           float64
	Visits, Checksum, LogicalNodes, EdgeReads, AttributeReads, Revisits, GCCycles, AllocatedBytes, Allocations uint64
	Valid                                                                                                      bool
	Reason                                                                                                     string
}
type Result struct{ Visits, Checksum, LogicalNodes, EdgeReads, AttributeReads, Revisits uint64 }
type TraceEntry struct {
	ID       uint32
	Kind     ast.Kind
	Flags    ast.NodeFlags
	Pos, End int
	Text     string
	Children uint32
}
type logical struct {
	id   uint32
	kind ast.Kind
	kids []*logical
}
type tree struct {
	store   ast.Handle
	pointer *ast.Node
	owner   *ast.Store
	count   uint64
}

func loc(id uint32) core.TextRange { p := int(id) * 2; return core.NewTextRange(p, p+1) }

func recipe(c Config) (*logical, uint64) {
	var id uint32
	new := func(k ast.Kind) *logical { id++; return &logical{id: id, kind: k} }
	var makeNode func(int) *logical
	makeNode = func(n int) *logical {
		if n <= 1 {
			return new(ast.KindNumericLiteral)
		}
		switch c.Shape {
		case ShapeDeep:
			x := new(ast.KindParenthesizedExpression)
			x.kids = []*logical{makeNode(n - 1)}
			return x
		case ShapeWide:
			x := new(ast.KindArrayLiteralExpression)
			x.kids = make([]*logical, n-1)
			for i := range x.kids {
				x.kids[i] = makeNode(1)
			}
			return x
		default:
			if n >= 4 && ((int64(id)+c.Seed)&1) == 0 {
				x := new(ast.KindBinaryExpression)
				rest := n - 2
				left := rest / 2
				if rest%2 != 0 && ((int64(x.id)+c.Seed)&1) != 0 {
					left++
				}
				right := rest - left
				if left > 0 && right > 0 {
					x.kids = []*logical{makeNode(left), new(ast.KindPlusToken), makeNode(right)}
					return x
				}
			}
			x := new(ast.KindParenthesizedExpression)
			x.kids = []*logical{makeNode(n - 1)}
			return x
		}
	}
	r := makeNode(c.Nodes)
	return r, uint64(id)
}
func renderStore(n *logical, f *ast.Factory) ast.Handle {
	var h ast.Handle
	switch n.kind {
	case ast.KindNumericLiteral:
		h = f.NewNumericLiteral(strconv.FormatUint(uint64(n.id), 10), 0)
	case ast.KindPlusToken:
		h = f.NewToken(ast.TokenSyntaxKind(ast.KindPlusToken))
	case ast.KindParenthesizedExpression:
		h = f.NewParenthesizedExpression(renderStore(n.kids[0], f))
	case ast.KindArrayLiteralExpression:
		a := make([]ast.Handle, len(n.kids))
		for i, k := range n.kids {
			a[i] = renderStore(k, f)
		}
		h = f.NewArrayLiteralExpression(f.List(loc(n.id), a...), false)
	case ast.KindBinaryExpression:
		h = f.NewBinaryExpression(0, renderStore(n.kids[0], f), ast.Handle{}, renderStore(n.kids[1], f), renderStore(n.kids[2], f))
	}
	h.SetLoc(loc(n.id))
	if n.id%3 == 0 {
		h.SetFlags(ast.NodeFlagsSynthesized)
	}
	return h
}
func renderPointer(n *logical, f *ast.NodeFactory) *ast.Node {
	var h *ast.Node
	switch n.kind {
	case ast.KindNumericLiteral:
		h = f.NewNumericLiteral(strconv.FormatUint(uint64(n.id), 10), 0)
	case ast.KindPlusToken:
		h = f.NewToken(ast.TokenSyntaxKind(ast.KindPlusToken))
	case ast.KindParenthesizedExpression:
		h = f.NewParenthesizedExpression(renderPointer(n.kids[0], f))
	case ast.KindArrayLiteralExpression:
		a := make([]*ast.Node, len(n.kids))
		for i, k := range n.kids {
			a[i] = renderPointer(k, f)
		}
		h = f.NewArrayLiteralExpression(f.NewNodeList(a), false)
	case ast.KindBinaryExpression:
		h = f.NewBinaryExpression(nil, renderPointer(n.kids[0], f), nil, renderPointer(n.kids[1], f), renderPointer(n.kids[2], f))
	}
	h.Loc = loc(n.id)
	if n.id%3 == 0 {
		h.Flags = ast.NodeFlagsSynthesized
	}
	return h
}
func pointerParents(n, p *ast.Node) {
	if n == nil {
		return
	}
	n.Parent = p
	n.ForEachChild(func(k *ast.Node) bool { pointerParents(k, n); return false })
}
func build(c Config) (*tree, error) {
	if err := Validate(c); err != nil {
		return nil, err
	}
	if c.Shape == ShapeFixture {
		return fixture()
	}
	r, count := recipe(c)
	if c.Representation == RepresentationPointer {
		p := renderPointer(r, ast.NewNodeFactory(ast.NodeFactoryHooks{}))
		pointerParents(p, nil)
		return &tree{pointer: p, count: count}, nil
	}
	f := ast.NewFactoryHint(ast.FactoryHooks{}, int(count)+8)
	s := renderStore(r, f)
	s.SetParentsInChildren()
	f.Store().Freeze()
	return &tree{store: s, owner: f.Store(), count: count}, nil
}

//go:embed testdata/fixture.ts
var fixtureSource string

func fixture() (*tree, error) {
	src := fixtureSource
	f := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/traversal-fixture.ts", Path: tspath.Path("/traversal-fixture.ts")}, src, core.ScriptKindTS)
	if f == nil || f.ParseRoot().IsNil() {
		return nil, errors.New("workload: fixture parse failed")
	}
	r := f.ParseRoot()
	var n uint64
	var count func(ast.Handle)
	count = func(h ast.Handle) { n++; h.ForEachChild(func(k ast.Handle) bool { count(k); return false }) }
	count(r)
	r.SetParentsInChildren()
	r.Store().Freeze()
	return &tree{store: r, owner: r.Store(), count: n}, nil
}
func release(t *tree) {
	if t != nil && t.owner != nil {
		ast.UnregisterStore(t.owner)
	}
}

func storeText(h ast.Handle) string {
	switch h.Kind {
	case ast.KindNumericLiteral:
		return h.NumericLiteralText()
	case ast.KindIdentifier:
		return h.Text()
	}
	return ""
}
func pointerText(n *ast.Node) string {
	switch n.Kind {
	case ast.KindNumericLiteral, ast.KindIdentifier:
		return n.Text()
	}
	return ""
}

type walker struct {
	expr    bool
	r       Result
	tracing bool
	entries []TraceEntry
	current int
}

func (w *walker) record(k ast.Kind, flags ast.NodeFlags, pos, end int, text string) {
	w.r.Visits++
	w.r.AttributeReads += 4
	if k == ast.KindNumericLiteral || k == ast.KindIdentifier {
		w.r.AttributeReads++
	}
	// The cheap sink preserves reads. Ordered equivalence uses complete traces.
	v := uint64(k) + uint64(flags) + uint64(uint32(pos)) + uint64(uint32(end)) + uint64(len(text))
	if len(text) > 0 {
		v += uint64(text[0]) + uint64(text[len(text)-1])
	}
	w.r.Checksum += v
	if w.tracing {
		w.current = len(w.entries)
		w.entries = append(w.entries, TraceEntry{ID: uint32(w.r.Visits), Kind: k, Flags: flags, Pos: pos, End: end, Text: text})
	}
}
func (w *walker) edge() {
	w.r.EdgeReads++
	if w.tracing {
		w.entries[w.current].Children++
	}
}
func (w *walker) storeChild(h ast.Handle) bool {
	if !h.IsNil() {
		w.edge()
		w.storeWalk(h)
	}
	return false
}
func (w *walker) pointerChild(n *ast.Node) bool {
	if n != nil {
		w.edge()
		w.pointerWalk(n)
	}
	return false
}
func (w *walker) storeWalk(h ast.Handle) {
	parent := w.current
	loc := h.Loc()
	w.record(h.Kind, h.Flags(), loc.Pos(), loc.End(), storeText(h))
	if !w.expr {
		h.ForEachChild(w.storeChild)
	} else {
		switch h.Kind {
		case ast.KindBinaryExpression:
			w.storeChild(h.BinaryExpressionLeft())
			w.storeChild(h.BinaryExpressionRight())
		case ast.KindParenthesizedExpression:
			w.storeChild(h.ParenthesizedExpressionExpression())
		case ast.KindArrayLiteralExpression:
			for _, k := range h.Store().ListSlice(h.ArrayLiteralExpressionElements()).All() {
				w.storeChild(k)
			}
		case ast.KindCallExpression:
			w.storeChild(h.CallExpressionExpression())
			for _, k := range h.ArgumentsSeq().All() {
				w.storeChild(k)
			}
		case ast.KindPropertyAccessExpression:
			w.storeChild(h.PropertyAccessExpressionExpression())
			w.storeChild(h.PropertyAccessExpressionName())
		default:
			h.ForEachChild(w.storeChild)
		}
	}
	w.current = parent
}
func (w *walker) pointerWalk(n *ast.Node) {
	parent := w.current
	w.record(n.Kind, n.Flags, n.Loc.Pos(), n.Loc.End(), pointerText(n))
	if !w.expr {
		n.ForEachChild(w.pointerChild)
	} else {
		switch n.Kind {
		case ast.KindBinaryExpression:
			b := n.AsBinaryExpression()
			w.pointerChild(b.Left)
			w.pointerChild(b.Right)
		case ast.KindParenthesizedExpression:
			w.pointerChild(n.AsParenthesizedExpression().Expression)
		case ast.KindArrayLiteralExpression:
			for _, k := range n.Elements() {
				w.pointerChild(k)
			}
		case ast.KindCallExpression:
			w.pointerChild(n.AsCallExpression().Expression)
			for _, k := range n.Arguments() {
				w.pointerChild(k)
			}
		case ast.KindPropertyAccessExpression:
			p := n.AsPropertyAccessExpression()
			w.pointerChild(p.Expression)
			w.pointerChild(p.Name())
		default:
			n.ForEachChild(w.pointerChild)
		}
	}
	w.current = parent
}
func (w *walker) run(t *tree) Result {
	w.r = Result{LogicalNodes: t.count}
	w.current = -1
	if !t.store.IsNil() {
		w.storeWalk(t.store)
	} else {
		w.pointerWalk(t.pointer)
	}
	return w.r
}
func walk(t *tree, expr bool) Result { w := walker{expr: expr}; return w.run(t) }
func trace(c Config) ([]TraceEntry, Result, error) {
	t, err := build(c)
	if err != nil {
		return nil, Result{}, err
	}
	defer release(t)
	w := walker{expr: c.Case == CaseExpression, tracing: true}
	r := w.run(t)
	return w.entries, r, nil
}
func Trace(c Config) ([]TraceEntry, error) { a, _, err := trace(c); return a, err }
func Verify(c Config) error {
	if err := Validate(c); err != nil {
		return err
	}
	a, ra, err := trace(c)
	if err != nil {
		return err
	}
	other := c
	if c.Shape != ShapeFixture {
		if c.Representation == RepresentationStore {
			other.Representation = RepresentationPointer
		} else {
			other.Representation = RepresentationStore
		}
	}
	b, rb, err := trace(other)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(a, b) || ra != rb {
		return errors.New("workload: exact traversal trace mismatch")
	}
	return nil
}
func Measure(c Config, batch int) (Sample, error) {
	var s Sample
	if e := Validate(c); e != nil {
		return s, e
	}
	if batch < 1 {
		return s, errors.New("workload: batch must be positive")
	}
	oldP := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(oldP)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	t, e := build(c)
	if e != nil {
		return s, e
	}
	defer release(t)
	runtime.GC()
	oldGC := debug.SetGCPercent(-1)
	oldLimit := debug.SetMemoryLimit(math.MaxInt64)
	defer debug.SetGCPercent(oldGC)
	defer debug.SetMemoryLimit(oldLimit)
	expr := c.Case == CaseExpression
	worker := &walker{expr: expr}
	_ = worker.run(t)
	want := worker.run(t)
	runtime.KeepAlive(t)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_ = time.Now()
	runtime.ReadMemStats(&before)
	start := time.Now()
	for range batch {
		if got := worker.run(t); got != want {
			return s, errors.New("workload: timed traversal changed result")
		}
	}
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(t)
	s = Sample{NsPerOp: float64(elapsed.Nanoseconds()) / float64(batch), BytesPerOp: float64(after.TotalAlloc-before.TotalAlloc) / float64(batch), AllocsPerOp: float64(after.Mallocs-before.Mallocs) / float64(batch), Visits: want.Visits, Checksum: want.Checksum, LogicalNodes: want.LogicalNodes, EdgeReads: want.EdgeReads, AttributeReads: want.AttributeReads, Revisits: want.Revisits, GCCycles: uint64(after.NumGC - before.NumGC), AllocatedBytes: after.TotalAlloc - before.TotalAlloc, Allocations: after.Mallocs - before.Mallocs, Valid: true}
	if after.StackInuse > before.StackInuse {
		s.Valid = false
		s.Reason = fmt.Sprintf("stack memory grew in measurement: %d bytes", after.StackInuse-before.StackInuse)
		return s, errors.New(s.Reason)
	}
	if s.GCCycles != 0 || s.Allocations != 0 || s.AllocatedBytes != 0 {
		s.Valid = false
		s.Reason = "timed traversal allocated or triggered GC"
		return s, errors.New("workload: timed traversal violated zero-allocation/GC contract")
	}
	return s, nil
}
