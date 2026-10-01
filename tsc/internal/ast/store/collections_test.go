package store_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/convert"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/storetest"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
)

// The sets of nodes of checker_api.go against the collections they copy
// (store-checker-7d2b2-implementation-instructions-20261001.md 4.2): NodeSet
// against collections.Set[*ast.Node], OrderedNodeSet against
// collections.OrderedSet[*ast.Node]. The same sequences of operations, made
// from a fixed seed, run on both, on the nodes of one source paired by
// storetest.Pairs, and every result is compared. A sequence ranges over the
// set and adds, deletes and clears while it does.
const (
	collectionsSeed      = 20261001
	collectionsSequences = 3000
	collectionsMaxOps    = 30
	collectionsPool      = 12 // the nodes a sequence picks from
	collectionsMaxSteps  = 64 // an ordered range stops there: adding back may go on
)

const collectionsSource = `const a = 1; let b = [x, y, z]; function f(p, q) { return p + q; } class C { m() { this.n = 2; } }`

type setOp int

const (
	opAdd setOp = iota
	opHas
	opDelete
	opSize
	opClear
	opAddIfAbsent
	opRange
)

// rangeStep is what a range does after the element of its step: add, delete
// or clear (node is unused), or nothing (opSize).
type rangeStep struct {
	op   setOp
	node int
}

type collectionOp struct {
	op    setOp
	node  int
	steps []rangeStep
	stop  int // the range stops after this step, or -1
}

// setUnderTest is one of the four sets, on the pool's indices.
type setUnderTest interface {
	add(i int)
	has(i int) bool
	delete(i int) string // the result, "" when Delete returns none
	size() int
	clear()
	addIfAbsent(i int) bool
	each(yield func(i int) bool)
}

type pointerSet struct {
	s     *collections.Set[*ast.Node]
	pool  []*ast.Node
	index map[*ast.Node]int
}

func (p *pointerSet) add(i int)      { p.s.Add(p.pool[i]) }
func (p *pointerSet) has(i int) bool { return p.s.Has(p.pool[i]) }
func (p *pointerSet) delete(i int) string {
	p.s.Delete(p.pool[i])
	return ""
}
func (p *pointerSet) size() int              { return p.s.Len() }
func (p *pointerSet) clear()                 { p.s.Clear() }
func (p *pointerSet) addIfAbsent(i int) bool { return p.s.AddIfAbsent(p.pool[i]) }
func (p *pointerSet) each(yield func(int) bool) {
	for n := range p.s.Keys() {
		if !yield(poolIndex(p.index, n)) {
			return
		}
	}
}

// poolIndex is the node's index in the pool, or -1 for a node that is not
// in it (a nil node out of a range).
func poolIndex[K comparable](index map[K]int, key K) int {
	if i, ok := index[key]; ok {
		return i
	}
	return -1
}

type storeSet struct {
	s     *store.NodeSet
	pool  []store.Node
	index map[store.NodeKey]int
}

func (p *storeSet) add(i int)      { p.s.Add(p.pool[i]) }
func (p *storeSet) has(i int) bool { return p.s.Has(p.pool[i]) }
func (p *storeSet) delete(i int) string {
	p.s.Delete(p.pool[i])
	return ""
}
func (p *storeSet) size() int              { return p.s.Len() }
func (p *storeSet) clear()                 { p.s.Clear() }
func (p *storeSet) addIfAbsent(i int) bool { return p.s.AddIfAbsent(p.pool[i]) }
func (p *storeSet) each(yield func(int) bool) {
	for n := range p.s.Keys() {
		if !yield(poolIndex(p.index, n.Key())) {
			return
		}
	}
}

type pointerOrderedSet struct {
	s     *collections.OrderedSet[*ast.Node]
	pool  []*ast.Node
	index map[*ast.Node]int
}

func (p *pointerOrderedSet) add(i int)            { p.s.Add(p.pool[i]) }
func (p *pointerOrderedSet) has(i int) bool       { return p.s.Has(p.pool[i]) }
func (p *pointerOrderedSet) delete(i int) string  { return fmt.Sprint(p.s.Delete(p.pool[i])) }
func (p *pointerOrderedSet) size() int            { return p.s.Size() }
func (p *pointerOrderedSet) clear()               { p.s.Clear() }
func (p *pointerOrderedSet) addIfAbsent(int) bool { panic("OrderedSet has no AddIfAbsent") }
func (p *pointerOrderedSet) each(yield func(int) bool) {
	for n := range p.s.Values() {
		if !yield(poolIndex(p.index, n)) {
			return
		}
	}
}

type storeOrderedSet struct {
	s     *store.OrderedNodeSet
	pool  []store.Node
	index map[store.NodeKey]int
}

func (p *storeOrderedSet) add(i int)            { p.s.Add(p.pool[i]) }
func (p *storeOrderedSet) has(i int) bool       { return p.s.Has(p.pool[i]) }
func (p *storeOrderedSet) delete(i int) string  { return fmt.Sprint(p.s.Delete(p.pool[i])) }
func (p *storeOrderedSet) size() int            { return p.s.Size() }
func (p *storeOrderedSet) clear()               { p.s.Clear() }
func (p *storeOrderedSet) addIfAbsent(int) bool { panic("OrderedNodeSet has no AddIfAbsent") }
func (p *storeOrderedSet) each(yield func(int) bool) {
	for n := range p.s.Values() {
		if !yield(poolIndex(p.index, n.Key())) {
			return
		}
	}
}

// collectionPools are collectionsPool nodes of the source, the Pointer nodes
// and the Store nodes paired, at a fixed interval of the preorder.
func collectionPools(t *testing.T) ([]*ast.Node, []store.Node) {
	t.Helper()
	pf := parseSource("/collections.ts", collectionsSource)
	pairs, m := storetest.Pairs(pf, convert.Convert(pf, 0))
	if pairs == nil {
		t.Fatalf("the trees do not pair: %v", m)
	}
	if len(pairs) < collectionsPool {
		t.Fatalf("%d nodes, want %d or more", len(pairs), collectionsPool)
	}
	var pointers []*ast.Node
	var nodes []store.Node
	for i := range collectionsPool {
		pair := pairs[i*len(pairs)/collectionsPool]
		pointers = append(pointers, pair.P)
		nodes = append(nodes, pair.N)
	}
	return pointers, nodes
}

func collectionSequences(withAddIfAbsent bool) [][]collectionOp {
	r := rand.New(rand.NewPCG(collectionsSeed, uint64(map[bool]int{false: 1, true: 2}[withAddIfAbsent])))
	ops := []setOp{opAdd, opAdd, opAdd, opHas, opDelete, opSize, opRange, opRange}
	if withAddIfAbsent {
		ops = append(ops, opAddIfAbsent)
	}
	sequences := make([][]collectionOp, collectionsSequences)
	for i := range sequences {
		for range 1 + r.IntN(collectionsMaxOps) {
			op := collectionOp{op: ops[r.IntN(len(ops))], node: r.IntN(collectionsPool), stop: -1}
			if r.IntN(40) == 0 {
				op.op = opClear
			}
			if op.op == opRange {
				for range collectionsMaxSteps {
					step := rangeStep{op: opSize, node: r.IntN(collectionsPool)}
					switch x := r.IntN(20); {
					case x < 6:
						step.op = opAdd
					case x < 12:
						step.op = opDelete
					case x == 12:
						step.op = opClear
					}
					op.steps = append(op.steps, step)
				}
				if r.IntN(3) == 0 {
					op.stop = r.IntN(collectionsMaxSteps / 4)
				}
			}
			sequences[i] = append(sequences[i], op)
		}
	}
	return sequences
}

// runSequence is the trace of a sequence: each operation's result. An
// ordered range gives the elements in order. An unordered range is not
// determined where it adds (a map entry made while it ranges may or may not
// be produced), so it changes the set only in the steps that every element
// it must produce covers (the first half of the elements it starts with), and
// gives the set after it, sorted; what it produces is checked against the
// rules of a map's range instead (violations).
func runSequence(s setUnderTest, ops []collectionOp, ordered bool) (trace []string, violations []string) {
	for _, op := range ops {
		switch op.op {
		case opAdd:
			s.add(op.node)
			trace = append(trace, "add")
		case opHas:
			trace = append(trace, fmt.Sprint("has ", s.has(op.node)))
		case opDelete:
			trace = append(trace, "delete "+s.delete(op.node))
		case opSize:
			trace = append(trace, fmt.Sprint("size ", s.size()))
		case opClear:
			s.clear()
			trace = append(trace, "clear")
		case opAddIfAbsent:
			trace = append(trace, fmt.Sprint("addIfAbsent ", s.addIfAbsent(op.node)))
		case opRange:
			if ordered {
				trace = append(trace, "range "+orderedRange(s, op))
			} else {
				got, v := unorderedRange(s, op)
				trace = append(trace, "range, then "+got)
				violations = append(violations, v...)
			}
		}
	}
	return trace, violations
}

func orderedRange(s setUnderTest, op collectionOp) string {
	var out []string
	step := 0
	s.each(func(i int) bool {
		out = append(out, fmt.Sprint(i))
		if step == collectionsMaxSteps {
			out = append(out, "limit")
			return false
		}
		applyStep(s, op.steps[step])
		if step == op.stop {
			out = append(out, "stop")
			return false
		}
		step++
		return true
	})
	return strings.Join(out, " ")
}

func unorderedRange(s setUnderTest, op collectionOp) (string, []string) {
	start := members(s)
	changing := len(start) / 2
	deleted, added := map[int]bool{}, map[int]bool{}
	produced := map[int]int{}
	var violations []string
	step := 0
	stopped := false
	s.each(func(i int) bool {
		if i < 0 {
			violations = append(violations, "produced a node that is not of the pool")
			return false
		}
		produced[i]++
		if !s.has(i) {
			violations = append(violations, fmt.Sprintf("%d produced while not in the set", i))
		}
		if !start[i] && !added[i] {
			violations = append(violations, fmt.Sprintf("%d produced, neither in the set at the start nor added", i))
		}
		if step < changing {
			st := op.steps[step]
			switch st.op {
			case opAdd:
				added[st.node] = true
			case opDelete:
				deleted[st.node] = true
			case opClear:
				for k := range members(s) {
					deleted[k] = true
				}
			}
			applyStep(s, st)
			if step == op.stop {
				stopped = true
				return false
			}
		}
		step++
		return true
	})
	for i := range start {
		if !deleted[i] && !stopped && produced[i] != 1 {
			violations = append(violations, fmt.Sprintf("%d, in the set throughout, produced %d times", i, produced[i]))
		}
	}
	after := slices.Sorted(func(yield func(int) bool) {
		for i := range members(s) {
			if !yield(i) {
				return
			}
		}
	})
	return fmt.Sprint(after), violations
}

func applyStep(s setUnderTest, st rangeStep) {
	switch st.op {
	case opAdd:
		s.add(st.node)
	case opDelete:
		s.delete(st.node)
	case opClear:
		s.clear()
	}
}

// members are the pool's indices that the set has, by Has.
func members(s setUnderTest) map[int]bool {
	m := map[int]bool{}
	for i := range collectionsPool {
		if s.has(i) {
			m[i] = true
		}
	}
	return m
}

func compareSequences(t *testing.T, sequences [][]collectionOp, ordered bool, newPointer, newStore func() setUnderTest) {
	t.Helper()
	failed := 0
	for i, ops := range sequences {
		pt, pv := runSequence(newPointer(), ops, ordered)
		st, sv := runSequence(newStore(), ops, ordered)
		for _, v := range pv {
			t.Errorf("sequence %d: the Pointer: %s", i, v)
		}
		for _, v := range sv {
			t.Errorf("sequence %d: the Store: %s", i, v)
		}
		if j := firstDifference(pt, st); j >= 0 {
			failed++
			if failed <= 10 {
				t.Errorf("sequence %d, operation %d of %d: Pointer %q, Store %q", i, j, len(ops), at(pt, j), at(st, j))
			}
		}
	}
	if failed > 0 {
		t.Errorf("%d of %d sequences differ", failed, len(sequences))
	}
}

func firstDifference(a, b []string) int {
	for i := range max(len(a), len(b)) {
		if at(a, i) != at(b, i) {
			return i
		}
	}
	return -1
}

func at(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return "<end>"
}

func TestNodeSetIsSet(t *testing.T) {
	pointers, nodes := collectionPools(t)
	pointerIndex, storeIndex := map[*ast.Node]int{}, map[store.NodeKey]int{}
	for i := range pointers {
		pointerIndex[pointers[i]], storeIndex[nodes[i].Key()] = i, i
	}
	compareSequences(t, collectionSequences(true), false, /*ordered*/
		func() setUnderTest {
			return &pointerSet{s: &collections.Set[*ast.Node]{}, pool: pointers, index: pointerIndex}
		},
		func() setUnderTest { return &storeSet{s: &store.NodeSet{}, pool: nodes, index: storeIndex} })
}

func TestOrderedNodeSetIsOrderedSet(t *testing.T) {
	pointers, nodes := collectionPools(t)
	pointerIndex, storeIndex := map[*ast.Node]int{}, map[store.NodeKey]int{}
	for i := range pointers {
		pointerIndex[pointers[i]], storeIndex[nodes[i].Key()] = i, i
	}
	compareSequences(t, collectionSequences(false), true, /*ordered*/
		func() setUnderTest {
			return &pointerOrderedSet{s: &collections.OrderedSet[*ast.Node]{}, pool: pointers, index: pointerIndex}
		},
		func() setUnderTest {
			return &storeOrderedSet{s: &store.OrderedNodeSet{}, pool: nodes, index: storeIndex}
		})
}

// TestNodeSetsNilReceiver calls each method on a nil set of each side: both
// panic, or both give the same.
func TestNodeSetsNilReceiver(t *testing.T) {
	pointers, nodes := collectionPools(t)
	p, n := pointers[0], nodes[0]
	call := func(f func() string) (result string) {
		defer func() {
			if recover() != nil {
				result = "panic"
			}
		}()
		return f()
	}
	var ps *collections.Set[*ast.Node]
	var ns *store.NodeSet
	var pos *collections.OrderedSet[*ast.Node]
	var nos *store.OrderedNodeSet
	for _, c := range []struct {
		name           string
		pointer, store func() string
	}{
		{"Set.Has", func() string { return fmt.Sprint(ps.Has(p)) }, func() string { return fmt.Sprint(ns.Has(n)) }},
		{"Set.Add", func() string { ps.Add(p); return "" }, func() string { ns.Add(n); return "" }},
		{"Set.Delete", func() string { ps.Delete(p); return "" }, func() string { ns.Delete(n); return "" }},
		{"Set.Len", func() string { return fmt.Sprint(ps.Len()) }, func() string { return fmt.Sprint(ns.Len()) }},
		{"Set.Keys", func() string { return fmt.Sprint(len(ps.Keys())) }, func() string { return fmt.Sprint(len(slices.Collect(ns.Keys()))) }},
		{"Set.Clear", func() string { ps.Clear(); return "" }, func() string { ns.Clear(); return "" }},
		{"Set.AddIfAbsent", func() string { return fmt.Sprint(ps.AddIfAbsent(p)) }, func() string { return fmt.Sprint(ns.AddIfAbsent(n)) }},
		{"OrderedSet.Add", func() string { pos.Add(p); return "" }, func() string { nos.Add(n); return "" }},
		{"OrderedSet.Has", func() string { return fmt.Sprint(pos.Has(p)) }, func() string { return fmt.Sprint(nos.Has(n)) }},
		{"OrderedSet.Delete", func() string { return fmt.Sprint(pos.Delete(p)) }, func() string { return fmt.Sprint(nos.Delete(n)) }},
		{"OrderedSet.Values", func() string { return fmt.Sprint(len(slices.Collect(pos.Values()))) }, func() string { return fmt.Sprint(len(slices.Collect(nos.Values()))) }},
		{"OrderedSet.Clear", func() string { pos.Clear(); return "" }, func() string { nos.Clear(); return "" }},
		{"OrderedSet.Size", func() string { return fmt.Sprint(pos.Size()) }, func() string { return fmt.Sprint(nos.Size()) }},
	} {
		if pr, sr := call(c.pointer), call(c.store); pr != sr {
			t.Errorf("%s on nil: Pointer %q, Store %q", c.name, pr, sr)
		}
	}
}
