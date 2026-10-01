package storeharness

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/storetest"
	"github.com/microsoft/TypeScript/tsc/internal/storeparser"
)

// TestPolymorphicAccessors holds the methods of store.Node to the methods of
// *ast.Node of the same name (store-checker-7d2b2-implementation-instructions-20261001.md
// 4.1): every method that takes no argument and returns one value on both
// sides, found by reflect, called on every pair of nodes of the corpus. It
// knows nothing of the generator, so it does not share its blind spots: the
// generated compareRoles compares a role only on the kinds with a slot, and
// missed the Pointer QuestionToken's fallback to PostfixToken. Strict.
//
// A Pointer panic is recovered: with a nil or zero Store result it agrees
// (a generated default, counted by method and kind), with any other it does
// not. A Store panic agrees only with a Pointer panic.
func TestPolymorphicAccessors(t *testing.T) {
	if testing.Short() {
		t.Skip("the accessors are not compared on the corpus with -short")
	}
	accessors, pointerOnly, storeOnly := polymorphicAccessors(t)
	names := make([]string, len(accessors))
	for i, a := range accessors {
		names[i] = a.name
	}
	t.Logf("%d methods compared: %s", len(accessors), strings.Join(names, " "))
	t.Logf("%d methods not compared, bind results: %s", len(accessorsOfBind), strings.Join(slices.Sorted(func(yield func(string) bool) {
		for name := range accessorsOfBind {
			if !yield(name) {
				return
			}
		}
	}), " "))
	t.Logf("%d methods of *ast.Node only: %s", len(pointerOnly), strings.Join(pointerOnly, " "))
	t.Logf("%d methods of store.Node only: %s", len(storeOnly), strings.Join(storeOnly, " "))

	files := corpusUnder(t, true)
	start := time.Now()
	var (
		mu                          sync.Mutex
		total                       = accessorStats{}
		nodes, jsdoc                int
		pointerPanics, notInProgram int
	)
	next := make(chan int)
	var wg sync.WaitGroup
	for range envInt(t, "STORE_CHECKER_CORPUS_WORKERS", 4) {
		wg.Go(func() {
			for i := range next {
				f := files[i]
				pointer, panicked := tryPointerProgram(map[string]string{f.name: f.text}, stage1Options())
				if panicked != nil {
					mu.Lock()
					pointerPanics++
					mu.Unlock()
					continue
				}
				pf := pointer.GetSourceFile(f.name)
				if pf == nil {
					mu.Lock()
					notInProgram++
					mu.Unlock()
					continue
				}
				sf := storeparser.ParseSourceFileIn(store.NewIDSpace(1), 0, pf.ParseOptions(), pf.Text(), pf.ScriptKind)
				pairs, ofJSDoc := filePairs(t, f.origin, pf, sf)
				stats := compareAccessors(accessors, pairs, f.origin)
				mu.Lock()
				total.merge(stats)
				nodes += len(pairs)
				jsdoc += ofJSDoc
				mu.Unlock()
			}
		})
	}
	for i := range files {
		next <- i
	}
	close(next)
	wg.Wait()

	t.Logf("%d files (%d whose Pointer program panics, %d not in their program), %d pairs of nodes (%d of JSDoc), %v",
		len(files), pointerPanics, notInProgram, nodes, jsdoc, time.Since(start).Round(time.Second))
	total.log(t)
}

// accessorsOfBind are the methods of both sides that read the binder's
// results: the bind equivalence of 7c compares them.
var accessorsOfBind = map[string]bool{"Symbol": true, "LocalSymbol": true, "Locals": true}

// accessor is a method of both sides, as functions of the node.
type accessor struct {
	name    string
	pointer func(*ast.Node) accessorValue
	store   func(store.Node) accessorValue
}

// polymorphicAccessors are the methods of *ast.Node and store.Node of the same
// name that take no argument and return one value, but the conversions to a
// typed view (As*) and accessorsOfBind, and the methods of each side only.
func polymorphicAccessors(t *testing.T) (accessors []accessor, pointerOnly, storeOnly []string) {
	pt, st := reflect.TypeFor[*ast.Node](), reflect.TypeFor[store.Node]()
	nullary := func(m reflect.Method) bool {
		return !strings.HasPrefix(m.Name, "As") && m.Type.NumIn() == 1 && m.Type.NumOut() == 1
	}
	for i := range pt.NumMethod() {
		m := pt.Method(i)
		if !nullary(m) {
			continue
		}
		sm, ok := st.MethodByName(m.Name)
		if !ok || !nullary(sm) {
			pointerOnly = append(pointerOnly, m.Name)
			continue
		}
		if accessorsOfBind[m.Name] {
			continue
		}
		a := accessor{name: m.Name, pointer: pointerAccessor(m.Func.Interface()), store: storeAccessor(sm.Func.Interface())}
		if a.pointer == nil || a.store == nil {
			t.Fatalf("%s: no comparison for the results %v and %v", m.Name, m.Type.Out(0), sm.Type.Out(0))
		}
		accessors = append(accessors, a)
	}
	for i := range st.NumMethod() {
		m := st.Method(i)
		if _, ok := pt.MethodByName(m.Name); nullary(m) && !ok {
			storeOnly = append(storeOnly, m.Name)
		}
	}
	return accessors, pointerOnly, storeOnly
}

// accessorValue is a result of either side: a node is a list of one, which
// may be nil. A Pointer *NodeList or *ModifierList, and a List, say whether
// they are nil.
type accessorValue struct {
	panicked  bool
	list      bool
	hasNil    bool // the list is a *NodeList, a *ModifierList or a List
	nilList   bool
	pointers  []*ast.Node
	nodes     []store.Node
	scalar    any // string, bool, int64 or ast.ModifierFlags
	scalarSet bool
}

func pointerAccessor(f any) func(*ast.Node) accessorValue {
	switch f := f.(type) {
	case func(*ast.Node) *ast.Node:
		return func(n *ast.Node) accessorValue { return accessorValue{pointers: []*ast.Node{f(n)}} }
	case func(*ast.Node) *ast.NodeList:
		return func(n *ast.Node) accessorValue {
			l := f(n)
			if l == nil {
				return accessorValue{list: true, hasNil: true, nilList: true}
			}
			return accessorValue{list: true, hasNil: true, pointers: l.Nodes}
		}
	case func(*ast.Node) *ast.ModifierList:
		return func(n *ast.Node) accessorValue {
			l := f(n)
			if l == nil {
				return accessorValue{list: true, hasNil: true, nilList: true}
			}
			return accessorValue{list: true, hasNil: true, pointers: l.Nodes}
		}
	case func(*ast.Node) []*ast.Node:
		return func(n *ast.Node) accessorValue { return accessorValue{list: true, pointers: f(n)} }
	case func(*ast.Node) string:
		return func(n *ast.Node) accessorValue { return accessorValue{scalar: f(n), scalarSet: true} }
	case func(*ast.Node) bool:
		return func(n *ast.Node) accessorValue { return accessorValue{scalar: f(n), scalarSet: true} }
	case func(*ast.Node) int:
		return func(n *ast.Node) accessorValue { return accessorValue{scalar: int64(f(n)), scalarSet: true} }
	case func(*ast.Node) ast.ModifierFlags:
		return func(n *ast.Node) accessorValue { return accessorValue{scalar: f(n), scalarSet: true} }
	}
	return nil
}

func storeAccessor(f any) func(store.Node) accessorValue {
	switch f := f.(type) {
	case func(store.Node) store.Node:
		return func(n store.Node) accessorValue { return accessorValue{nodes: []store.Node{f(n)}} }
	case func(store.Node) store.List:
		return func(n store.Node) accessorValue {
			l := f(n)
			if l.IsNil() {
				return accessorValue{list: true, hasNil: true, nilList: true}
			}
			return accessorValue{list: true, hasNil: true, nodes: l.Nodes()}
		}
	case func(store.Node) []store.Node:
		return func(n store.Node) accessorValue { return accessorValue{list: true, nodes: f(n)} }
	case func(store.Node) string:
		return func(n store.Node) accessorValue { return accessorValue{scalar: f(n), scalarSet: true} }
	case func(store.Node) bool:
		return func(n store.Node) accessorValue { return accessorValue{scalar: f(n), scalarSet: true} }
	case func(store.Node) int32:
		return func(n store.Node) accessorValue { return accessorValue{scalar: int64(f(n)), scalarSet: true} }
	case func(store.Node) ast.ModifierFlags:
		return func(n store.Node) accessorValue { return accessorValue{scalar: f(n), scalarSet: true} }
	}
	return nil
}

func callPointer(a accessor, n *ast.Node) (v accessorValue) {
	defer func() {
		if recover() != nil {
			v = accessorValue{panicked: true}
		}
	}()
	return a.pointer(n)
}

func callStore(a accessor, n store.Node) (v accessorValue) {
	defer func() {
		if recover() != nil {
			v = accessorValue{panicked: true}
		}
	}()
	return a.store(n)
}

// zero is a nil node, a nil List, an empty []Node, or a zero scalar.
func (v accessorValue) zero() bool {
	switch {
	case v.scalarSet:
		return reflect.ValueOf(v.scalar).IsZero()
	case v.list && v.hasNil:
		return v.nilList
	case v.list:
		return len(v.nodes) == 0
	}
	return v.nodes[0].IsNil()
}

// differs is "" when the Store value is the Pointer value, the Pointer nodes
// mapped by the pairs, or why it is not.
func differs(p, s accessorValue, paired map[*ast.Node]store.Node) string {
	switch {
	case p.panicked && s.panicked:
		return ""
	case p.panicked:
		if s.zero() {
			return ""
		}
		return "the Pointer panics, the Store gives " + s.storeString()
	case s.panicked:
		return "the Store panics, the Pointer gives " + p.pointerString()
	case p.scalarSet:
		if p.scalar != s.scalar {
			return fmt.Sprintf("Pointer %q, Store %q", fmt.Sprint(p.scalar), fmt.Sprint(s.scalar))
		}
		return ""
	case p.list && p.hasNil && p.nilList != s.nilList:
		return fmt.Sprintf("Pointer %s, Store %s", p.pointerString(), s.storeString())
	}
	if len(p.pointers) != len(s.nodes) {
		return fmt.Sprintf("Pointer %s, Store %s", p.pointerString(), s.storeString())
	}
	for i, pn := range p.pointers {
		if pn == nil {
			if !s.nodes[i].IsNil() {
				return fmt.Sprintf("Pointer %s, Store %s", p.pointerString(), s.storeString())
			}
			continue
		}
		mapped, ok := paired[pn]
		if !ok {
			return fmt.Sprintf("the Pointer result %s is not a node of the pairs; Store %s", p.pointerString(), s.storeString())
		}
		if !store.SameNode(mapped, s.nodes[i]) {
			return fmt.Sprintf("Pointer %s, Store %s", p.pointerString(), s.storeString())
		}
	}
	return ""
}

func (v accessorValue) pointerString() string {
	if v.scalarSet {
		return fmt.Sprintf("%q", fmt.Sprint(v.scalar))
	}
	if v.list && v.nilList {
		return "nil list"
	}
	parts := make([]string, len(v.pointers))
	for i, n := range v.pointers {
		if n == nil {
			parts[i] = "nil"
		} else {
			parts[i] = fmt.Sprintf("%v %d-%d", n.Kind, n.Pos(), n.End())
		}
	}
	if v.list {
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return parts[0]
}

func (v accessorValue) storeString() string {
	if v.scalarSet {
		return fmt.Sprintf("%q", fmt.Sprint(v.scalar))
	}
	if v.list && v.nilList {
		return "nil list"
	}
	parts := make([]string, len(v.nodes))
	for i, n := range v.nodes {
		if n.IsNil() {
			parts[i] = "nil"
		} else {
			parts[i] = fmt.Sprintf("%v %d-%d", n.Kind(), n.Pos(), n.End())
		}
	}
	if v.list {
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return parts[0]
}

// accessorKey is a method on a kind.
type accessorKey struct {
	method string
	kind   ast.Kind
}

type accessorCount struct {
	compared      int
	pointerPanics int // the Pointer panics and the Store gives nil or zero
	bothPanic     int
	mismatches    int
	first         string // the first mismatch: where, and the two values
}

type accessorStats map[accessorKey]*accessorCount

func (s accessorStats) at(k accessorKey) *accessorCount {
	c := s[k]
	if c == nil {
		c = &accessorCount{}
		s[k] = c
	}
	return c
}

func (s accessorStats) merge(other accessorStats) {
	for k, o := range other {
		c := s.at(k)
		c.compared += o.compared
		c.pointerPanics += o.pointerPanics
		c.bothPanic += o.bothPanic
		if c.mismatches == 0 && o.mismatches > 0 {
			c.first = o.first
		}
		c.mismatches += o.mismatches
	}
}

func compareAccessors(accessors []accessor, pairs []storetest.Pair, origin string) accessorStats {
	paired := make(map[*ast.Node]store.Node, len(pairs))
	for _, pair := range pairs {
		paired[pair.P] = pair.N
	}
	stats := accessorStats{}
	for _, pair := range pairs {
		for _, a := range accessors {
			c := stats.at(accessorKey{a.name, pair.P.Kind})
			c.compared++
			p, s := callPointer(a, pair.P), callStore(a, pair.N)
			if d := differs(p, s, paired); d != "" {
				if c.mismatches == 0 {
					c.first = fmt.Sprintf("%s at %d: %s", origin, pair.P.Pos(), d)
				}
				c.mismatches++
				continue
			}
			switch {
			case p.panicked && s.panicked:
				c.bothPanic++
			case p.panicked:
				c.pointerPanics++
			}
		}
	}
	return stats
}

func (s accessorStats) log(t *testing.T) {
	t.Helper()
	keys := slices.SortedFunc(func(yield func(accessorKey) bool) {
		for k := range s {
			if !yield(k) {
				return
			}
		}
	}, func(a, b accessorKey) int {
		return cmp.Or(cmp.Compare(a.method, b.method), cmp.Compare(a.kind, b.kind))
	})
	var compared, pointerPanics, bothPanic, mismatches int
	for _, k := range keys {
		c := s[k]
		compared += c.compared
		pointerPanics += c.pointerPanics
		bothPanic += c.bothPanic
		mismatches += c.mismatches
	}
	t.Logf("%d calls on each side: %d where the Pointer panics and the Store gives nil or zero, %d where both panic, %d mismatches", compared, pointerPanics, bothPanic, mismatches)
	t.Log("the Pointer panics and the Store gives nil or zero (method, kind, count):")
	for _, k := range keys {
		if c := s[k]; c.pointerPanics > 0 {
			t.Logf("  %-20s %-32v %d", k.method, k.kind, c.pointerPanics)
		}
	}
	t.Log("both panic (method, kind, count):")
	for _, k := range keys {
		if c := s[k]; c.bothPanic > 0 {
			t.Logf("  %-20s %-32v %d", k.method, k.kind, c.bothPanic)
		}
	}
	for _, k := range keys {
		if c := s[k]; c.mismatches > 0 {
			t.Errorf("%s on %v: %d mismatches, first %s", k.method, k.kind, c.mismatches, c.first)
		}
	}
}
