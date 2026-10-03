package store_test

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/storetest"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/storebinder"
	"github.com/microsoft/TypeScript/tsc/internal/storeparser"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// The storage contract of the synthetic Stores, without a checker: the
// operation sequences of internal/ast/docs/store-checker-execution-design-
// 20260924.md 9 on hand-built nodes (the sources are small TS files parsed by
// storeparser and bound by storebinder; the synthetic nodes come from a
// Factory). See store-checker-7d0-implementation-instructions-20260924.md
// 6.2 for the list. Every sequence that depends on the Store capacity runs
// with the capacities below, and the normalized structure (kinds, SameNode
// relations of the edges, parents, File, the owners of the flows) must be
// the same for all of them; the StoreID numbers need not be.

var capacities = []int{1, 2, 3, 1 << 12}

// program stands in for the program: an IDSpace and its bound source files.
type program struct {
	space *store.IDSpace
	files []*store.File
}

func newProgram(t *testing.T, texts ...string) *program {
	t.Helper()
	p := &program{space: store.NewIDSpace(len(texts))}
	for i, text := range texts {
		p.files = append(p.files, parseAndBind(p.space, store.StoreID(i), fmt.Sprintf("/f%d.ts", i), text))
	}
	return p
}

func parseAndBind(space *store.IDSpace, id store.StoreID, name, text string) *store.File {
	file := storeparser.ParseSourceFileIn(space, id, ast.SourceFileParseOptions{FileName: name, Path: tspath.Path(name)}, text, core.ScriptKindTS)
	storebinder.BindSourceFile(file)
	return file
}

// find is the first node in preorder of kind, from the root of file.
func find(t *testing.T, file *store.File, kind ast.Kind) store.Node {
	t.Helper()
	for _, n := range storetest.Preorder(file.Root()) {
		if n.Kind() == kind {
			return n
		}
	}
	t.Fatalf("no %v in %s", kind, file.FileName())
	return store.Node{}
}

// findAll is every node of kind in preorder.
func findAll(file *store.File, kind ast.Kind) []store.Node {
	var nodes []store.Node
	for _, n := range storetest.Preorder(file.Root()) {
		if n.Kind() == kind {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

func mustPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		if recover() == nil {
			t.Errorf("%s did not panic", what)
		}
	}()
	f()
}

func mustNotPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", what, r)
		}
	}()
	f()
}

func storeID(n store.Node) store.StoreID { return n.Key().StoreID() }

// describe is the normalized structure of the synthetic nodes reachable from
// roots through children: each gets a label by order of first visit, and a
// source node is named by its file, kind and range. StoreIDs do not appear.
type describer struct {
	labels map[store.NodeKey]string
	queue  []store.Node
	out    strings.Builder
}

func describe(roots ...store.Node) string {
	d := &describer{labels: map[store.NodeKey]string{}}
	for _, root := range roots {
		fmt.Fprintf(&d.out, "root %s\n", d.label(root))
	}
	for len(d.queue) > 0 {
		n := d.queue[0]
		d.queue = d.queue[1:]
		var children []string
		n.ForEachChild(func(c store.Node) bool {
			children = append(children, d.label(c))
			return false
		})
		fmt.Fprintf(&d.out, "%s: %v %d-%d flags=%v parent=%s children=%v file=%s flow=%s\n",
			d.label(n), n.Kind(), n.Pos(), n.End(), n.Flags(), d.label(n.Parent()), children, fileName(n.File()), d.flow(n.FlowNode()))
	}
	return d.out.String()
}

func (d *describer) label(n store.Node) string {
	if n.IsNil() {
		return "nil"
	}
	if !store.IsSynthetic(n) {
		return fmt.Sprintf("src(%s %v %d-%d)", fileName(n.File()), n.Kind(), n.Pos(), n.End())
	}
	if l, ok := d.labels[n.Key()]; ok {
		return l
	}
	l := fmt.Sprintf("syn#%d", len(d.labels))
	d.labels[n.Key()] = l
	d.queue = append(d.queue, n)
	return l
}

func (d *describer) flow(f store.Flow) string {
	if f.IsNil() {
		return "nil"
	}
	owner := store.FlowOwner(f)
	if f.Flags()&(ast.FlowFlagsSwitchClause|ast.FlowFlagsReduceLabel) != 0 {
		return fmt.Sprintf("(%s %v)", fileName(owner.Root().File()), f.Flags())
	}
	return fmt.Sprintf("(%s %v %s)", fileName(owner.Root().File()), f.Flags(), d.label(f.Node()))
}

func fileName(f *store.File) string {
	if f == nil {
		return "nil"
	}
	return f.FileName()
}

// eachCapacity runs a sequence with every capacity and compares the
// structures it returns.
func eachCapacity(t *testing.T, run func(t *testing.T, capacity int) string) {
	t.Helper()
	var first string
	for i, capacity := range capacities {
		t.Run(fmt.Sprintf("capacity=%d", capacity), func(t *testing.T) {
			got := run(t, capacity)
			if i == 0 {
				first = got
			} else if got != first {
				t.Errorf("structure with capacity %d differs from capacity %d:\n%s\nwant\n%s", capacity, capacities[0], got, first)
			}
		})
	}
}

// 1. The reference this.x of isPropertyInitializedInConstructor.
func TestStorageThisPropertyReference(t *testing.T) {
	eachCapacity(t, func(t *testing.T, capacity int) string {
		p := newProgram(t, "class C { x: number; constructor() { this.x = 1; } }")
		file := p.files[0]
		f := store.NewFactory(store.NewNodeRegion(p.space), capacity, store.FactoryHooks{})
		property := find(t, file, ast.KindPropertyDeclaration)
		propName := property.Name()
		constructor := find(t, file, ast.KindConstructor)
		returnFlow := constructor.AsConstructorDeclaration().ReturnFlowNode()
		if returnFlow.IsNil() {
			t.Fatal("the constructor has no return flow")
		}

		this := f.NewKeywordExpression(ast.KindThisKeyword)
		reference := f.NewPropertyAccessExpression(this, store.Node{}, propName, ast.NodeFlagsNone)
		f.SetParent(this, reference)
		f.SetParent(reference, constructor)
		f.SetNodeFlow(reference, returnFlow)

		access := reference.AsPropertyAccessExpression()
		if !store.SameNode(access.Name(), propName) {
			t.Error("reference.Name() is not the source name")
		}
		if !store.SameNode(access.Expression(), this) {
			t.Error("reference.Expression() is not this")
		}
		if !store.SameNode(propName.Parent(), property) {
			t.Error("the source name's parent changed")
		}
		if !store.SameNode(this.Parent(), reference) {
			t.Error("this.Parent() is not the reference")
		}
		if !store.SameNode(reference.Parent(), constructor) {
			t.Error("reference.Parent() is not the constructor")
		}
		flow := reference.FlowNode()
		if flow != returnFlow {
			t.Error("reference.FlowNode() is not the constructor's return flow")
		}
		if store.FlowOwner(flow) != file.Store || flow.Node().File() != file {
			t.Error("the flow is not resolved in the source Bound")
		}
		if reference.File() != file || this.File() != file {
			t.Error("File() of the synthetic nodes is not the source file")
		}
		if capacity == 1 && storeID(this) == storeID(reference) {
			t.Error("capacity 1 did not put this and the reference in two Stores")
		}
		return describe(reference)
	})
}

// 2. The element access of getSyntheticElementAccess: a source expression
// is the child, and its parent does not change.
func TestStorageSyntheticElementAccess(t *testing.T) {
	eachCapacity(t, func(t *testing.T, capacity int) string {
		p := newProgram(t, "declare const obj: any; const { a } = obj; const { b } = obj || {};")
		file := p.files[0]
		f := store.NewFactory(store.NewNodeRegion(p.space), capacity, store.FactoryHooks{})
		var results []store.Node
		for _, element := range findAll(file, ast.KindBindingElement) {
			parentAccess := element.Parent().Parent().Initializer() // the variable's initializer
			sourceParent := parentAccess.Parent()
			flow := parentAccess.FlowNode()
			literal := f.NewStringLiteral(element.Name().Text(), ast.TokenFlagsNone)
			f.SetRange(literal, element.Pos(), element.End())
			lhs := parentAccess
			wrapped := !store.IsLeftHandSideExpression(parentAccess)
			if wrapped {
				lhs = f.NewParenthesizedExpression(parentAccess)
				f.SetRange(lhs, element.Pos(), element.End())
			}
			result := f.NewElementAccessExpression(lhs, store.Node{}, literal, ast.NodeFlagsNone)
			f.SetRange(result, element.Pos(), element.End())
			f.SetParent(literal, result)
			f.SetParent(result, element)
			if wrapped {
				f.SetParent(lhs, result)
			}
			f.SetNodeFlow(result, flow)

			if !store.SameNode(parentAccess.Parent(), sourceParent) {
				t.Error("the source expression's parent changed")
			}
			expression := result.AsElementAccessExpression().Expression()
			if wrapped {
				if !store.SameNode(expression.Expression(), parentAccess) || !store.SameNode(expression.Parent(), result) {
					t.Error("the parenthesized source expression is not the child")
				}
			} else if !store.SameNode(expression, parentAccess) {
				t.Error("result.Expression() is not the source expression")
			}
			if !store.SameNode(literal.Parent(), result) || !store.SameNode(result.Parent(), element) {
				t.Error("the parents of the synthetic nodes are wrong")
			}
			if result.FlowNode() != flow {
				t.Error("result.FlowNode() is not the source expression's flow")
			}
			if result.File() != file {
				t.Error("result.File() is not the source file")
			}
			results = append(results, result)
		}
		if len(results) != 2 {
			t.Fatalf("%d binding elements, want 2", len(results))
		}
		return describe(results...)
	})
}

// 3. A node of an old Store gets a parent in a new Store afterwards. The
// Node held from before and the Node the accessor returns read the same
// updates.
func TestStorageLateParentAcrossStores(t *testing.T) {
	eachCapacity(t, func(t *testing.T, capacity int) string {
		p := newProgram(t, "let v = 1;")
		f := store.NewFactory(store.NewNodeRegion(p.space), capacity, store.FactoryHooks{})
		a := f.NewIdentifier("a")
		b := f.NewIdentifier("b")
		c := f.NewIdentifier("c")
		parent := f.NewParenthesizedExpression(a)
		if capacity == 1 && (storeID(a) == storeID(parent) || storeID(a) == storeID(b)) {
			t.Error("capacity 1 did not cross a Store boundary")
		}
		f.SetParent(a, parent)
		f.SetRange(a, 5, 6)
		child := parent.Expression()
		if !store.SameNode(child, a) {
			t.Error("parent.Expression() is not the held node")
		}
		if child.Pos() != 5 || a.Pos() != 5 {
			t.Error("the held node and the accessor's node read different ranges")
		}
		if !store.SameNode(a.Parent(), parent) || !store.SameNode(child.Parent(), parent) {
			t.Error("the late parent is not seen through both nodes")
		}
		f.SetParent(b, parent)
		f.SetParent(c, b)
		if !store.SameNode(c.Parent().Parent(), parent) {
			t.Error("a chain of late parents across Stores is broken")
		}
		return describe(parent, b, c)
	})
}

// 4. Update without a change returns the original, nils of different Stores
// included; Clone shares the children and lists.
func TestStorageUpdateAndClone(t *testing.T) {
	eachCapacity(t, func(t *testing.T, capacity int) string {
		p := newProgram(t, "declare const o: any; o.m(1, 2);")
		file := p.files[0]
		created := 0
		f := store.NewFactory(store.NewNodeRegion(p.space), capacity, store.FactoryHooks{OnCreate: func(store.Node) { created++ }})
		call := find(t, file, ast.KindCallExpression)
		sourceName := call.Expression().Name()
		expression := f.NewIdentifier("o")
		access := f.NewPropertyAccessExpression(expression, store.Node{}, sourceName, ast.NodeFlagsNone)
		f.SetParent(access, call)
		before := created
		sourceNil := file.Store.Node(store.NoNodeRef)
		updated := f.UpdatePropertyAccessExpression(access.AsPropertyAccessExpression(), expression, sourceNil, sourceName, access.Flags())
		if !store.SameNode(updated, access) {
			t.Error("Update without a change made a new node")
		}
		if created != before || f.NodeCount() != before {
			t.Error("Update without a change created a node")
		}
		changed := f.UpdatePropertyAccessExpression(access.AsPropertyAccessExpression(), f.NewIdentifier("p"), store.Node{}, sourceName, access.Flags())
		if store.SameNode(changed, access) || !changed.Parent().IsNil() {
			t.Error("Update with a change did not make a fresh node without a parent")
		}

		// Clone shares the children and the lists, and takes flags and range
		// but not the parent, flow or symbol.
		arguments := call.AsCallExpression().Arguments()
		synthCall := f.NewCallExpression(access, store.Node{}, store.List{}, arguments, ast.NodeFlagsNone)
		f.SetParent(synthCall, call)
		f.SetRange(synthCall, 3, 9)
		f.AddFlags(synthCall, ast.NodeFlagsSynthesized)
		f.SetNodeFlow(synthCall, call.FlowNode())
		clone := f.CloneNode(synthCall)
		if store.SameNode(clone, synthCall) {
			t.Error("Clone returned the original")
		}
		if !store.SameNode(clone.Expression(), access) || !store.SameList(clone.Arguments(), arguments) || !store.SameList(clone.Arguments(), synthCall.Arguments()) {
			t.Error("Clone does not share the children and lists")
		}
		if !clone.Parent().IsNil() || !clone.FlowNode().IsNil() || clone.Symbol() != nil {
			t.Error("Clone took the parent, flow or symbol")
		}
		if clone.Pos() != 3 || clone.End() != 9 || clone.Flags() != synthCall.Flags() {
			t.Error("Clone did not take the range and flags")
		}
		if capacity == 1 && storeID(clone) == storeID(synthCall) {
			t.Error("capacity 1 did not cross a Store boundary")
		}
		return describe(access, changed, synthCall, clone)
	})
}

// 5. CloneNodeList copies the element edges into a new block (K3). A
// storeChecks build refuses SetListElement on both the clone and its
// original; a list not involved in a clone takes it.
func TestStorageCloneNodeList(t *testing.T) {
	eachCapacity(t, func(t *testing.T, capacity int) string {
		p := newProgram(t, "f(1, 2);")
		file := p.files[0]
		f := store.NewFactory(store.NewNodeRegion(p.space), capacity, store.FactoryHooks{})
		a := f.NewIdentifier("a")
		b := f.NewIdentifier("b")
		original := f.NewNodeList([]store.Node{a, b})
		clone := store.CloneNodeList(f, original)
		if store.SameList(clone, original) {
			t.Error("the clone is the original list")
		}
		if clone.Len() != 2 || !store.SameNode(clone.At(0), a) || !store.SameNode(clone.At(1), b) {
			t.Error("the clone's elements are not the original's")
		}
		sourceArguments := find(t, file, ast.KindCallExpression).AsCallExpression().Arguments()
		sourceClone := store.CloneNodeList(f, sourceArguments)
		if sourceClone.Len() != 2 || !store.SameNode(sourceClone.At(0), sourceArguments.At(0)) {
			t.Error("the clone of a source list does not share the source elements")
		}
		x := f.NewIdentifier("x")
		if store.StoreChecks {
			mustPanic(t, "SetListElement on the clone", func() { f.SetListElement(clone, 0, x) })
			mustPanic(t, "SetListElement on the original", func() { f.SetListElement(original, 0, x) })
			mustPanic(t, "SetListElement on the clone of a source list", func() { f.SetListElement(sourceClone, 0, x) })
		}
		mustPanic(t, "SetListElement on a source list", func() { f.SetListElement(sourceArguments, 0, x) })
		other := f.NewNodeList([]store.Node{a})
		mustNotPanic(t, "SetListElement on a list without a clone", func() { f.SetListElement(other, 0, x) })
		if !store.SameNode(other.At(0), x) {
			t.Error("SetListElement did not write")
		}
		holder := f.NewArrayLiteralExpression(clone, false)
		return describe(holder, f.NewArrayLiteralExpression(other, false), f.NewArrayLiteralExpression(sourceClone, false))
	})
}

// 6. Every nil is the same node and has the same key.
func TestStorageNilIdentity(t *testing.T) {
	p := newProgram(t, "let a = 1;", "let b = 2;")
	f := store.NewFactory(store.NewNodeRegion(p.space), 1, store.FactoryHooks{})
	synthetic := f.NewIdentifier("s")
	syntheticNil := synthetic.Parent()
	nils := []store.Node{{}, p.files[0].Store.Node(store.NoNodeRef), p.files[1].Store.Node(store.NoNodeRef), syntheticNil}
	for i, a := range nils {
		if !a.IsNil() {
			t.Errorf("nil %d is not IsNil", i)
		}
		if a.Key() != (store.NodeKey{}) {
			t.Errorf("nil %d has key %v", i, a.Key())
		}
		if a.File() != nil {
			t.Errorf("nil %d has a file", i)
		}
		for j, b := range nils {
			if !store.SameNode(a, b) {
				t.Errorf("nil %d and nil %d are not the same", i, j)
			}
		}
		if store.SameNode(a, synthetic) || store.SameNode(synthetic, a) {
			t.Errorf("nil %d is the same as a real node", i)
		}
	}
	real0 := p.files[0].Root()
	real1 := p.files[1].Root()
	if store.SameNode(real0, real1) || real0.Key() == real1.Key() {
		t.Error("the roots of two files are the same")
	}
	if !store.SameNode(real0, p.files[0].Store.Root()) || real0.Key() != p.files[0].Store.Root().Key() {
		t.Error("the same node is not the same")
	}
	var zeroSlice []store.Node = make([]store.Node, 2)
	if !zeroSlice[1].IsNil() || !store.SameNode(zeroSlice[0], p.files[1].Store.Node(0)) {
		t.Error("the elements of make([]Node, n) are not nil")
	}
	var symbol store.Symbol
	if !symbol.ValueDeclaration.IsNil() {
		t.Error("the zero ValueDeclaration is not nil")
	}
}

// 7. A corrupt edge panics in every build: a value without the foreign mark
// past the rows, and a foreign index past the foreign table.
func TestStorageCorruptEdges(t *testing.T) {
	p := newProgram(t, "let a = 1;")
	file := p.files[0]
	root := file.Root()
	mustPanic(t, "an unmarked edge just past the rows", func() {
		n := root.AsSourceFile().Statements().At(0)
		store.SetRawParent(n, store.NodeRef(file.Store.NodeCount()+1))
		n.Parent()
	})
	mustPanic(t, "an unmarked edge far past the rows", func() {
		n := root.AsSourceFile().Statements().At(0)
		store.SetRawParent(n, store.NodeRef(1<<30))
		n.Parent()
	})
	mustPanic(t, "a foreign edge in a source Store", func() {
		n := root.AsSourceFile().Statements().At(0)
		store.SetRawParent(n, store.ForeignBit)
		n.Parent()
	})
	f := store.NewFactory(store.NewNodeRegion(p.space), 4, store.FactoryHooks{})
	synthetic := f.NewIdentifier("x")
	f.SetParent(synthetic, root) // one foreign entry
	if store.ForeignNodeCount(store.StoreOf(synthetic)) != 1 {
		t.Fatal("SetParent to a source node did not add one foreign entry")
	}
	mustPanic(t, "a foreign index past the foreign table", func() {
		store.SetRawParent(synthetic, store.ForeignBit|1)
		synthetic.Parent()
	})
	mustPanic(t, "a small unmarked value in a Store with a foreign table", func() {
		store.SetRawParent(synthetic, 5)
		synthetic.Parent()
	})
}

// 8. The writers refuse a source Store, a sealed Store, a Store of another
// region and the nil node, in every build.
func TestStorageWriterOwner(t *testing.T) {
	for _, capacity := range capacities {
		t.Run(fmt.Sprintf("capacity=%d", capacity), func(t *testing.T) {
			p := newProgram(t, "let a = 1;")
			file := p.files[0]
			region := store.NewNodeRegion(p.space)
			f := store.NewFactory(region, capacity, store.FactoryHooks{})
			source := file.Root().AsSourceFile().Statements().At(0)
			synthetic := f.NewIdentifier("x")
			writes := map[string]func(n store.Node){
				"SetParent":     func(n store.Node) { f.SetParent(n, synthetic) },
				"SetRange":      func(n store.Node) { f.SetRange(n, 1, 2) },
				"AddFlags":      func(n store.Node) { f.AddFlags(n, ast.NodeFlagsSynthesized) },
				"SetNodeFlow":   func(n store.Node) { f.SetNodeFlow(n, store.Flow{}) },
				"SetNodeSymbol": func(n store.Node) { f.SetNodeSymbol(n, nil) },
			}
			// SetLocals takes a locals container and SetListElement a list, so
			// each owner also has a block and a list.
			newBlock := func(f *store.Factory) store.Node { return f.NewBlock(f.NewNodeList(nil), false) }
			newList := func(f *store.Factory) store.List { return f.NewNodeList([]store.Node{f.NewIdentifier("e")}) }
			otherF := store.NewFactory(store.NewNodeRegion(p.space), capacity, store.FactoryHooks{})
			other, otherBlock, otherList := otherF.NewIdentifier("y"), newBlock(otherF), newList(otherF)
			sealedF := store.NewFactory(region, capacity, store.FactoryHooks{})
			sealed, sealedBlock, sealedList := sealedF.NewIdentifier("z"), newBlock(sealedF), newList(sealedF)
			store.StoreOf(sealed).Seal()
			store.StoreOf(sealedBlock).Seal()
			store.ListStoreOf(sealedList).Seal()
			for name, write := range writes {
				mustPanic(t, name+" to a source node", func() { write(source) })
				mustPanic(t, name+" to the source root", func() { write(file.Root()) })
				mustPanic(t, name+" to a node of another region", func() { write(other) })
				mustPanic(t, name+" to a sealed synthetic Store", func() { write(sealed) })
				mustPanic(t, name+" to the zero Node", func() { write(store.Node{}) })
				mustPanic(t, name+" to a source sentinel", func() { write(file.Store.Node(0)) })
				mustNotPanic(t, name+" to a synthetic node of the region", func() { write(synthetic) })
			}
			setLocals := func(n store.Node) { f.SetLocals(n, store.SymbolTable{}) }
			mustPanic(t, "SetLocals to the source root", func() { setLocals(file.Root()) })
			mustPanic(t, "SetLocals to a block of another region", func() { setLocals(otherBlock) })
			mustPanic(t, "SetLocals to a sealed synthetic block", func() { setLocals(sealedBlock) })
			mustPanic(t, "SetLocals to the zero Node", func() { setLocals(store.Node{}) })
			mustPanic(t, "SetLocals to a source sentinel", func() { setLocals(file.Store.Node(0)) })
			mustNotPanic(t, "SetLocals to a synthetic block of the region", func() { setLocals(newBlock(f)) })
			setElement := func(l store.List) { f.SetListElement(l, 0, synthetic) }
			mustPanic(t, "SetListElement to a source list", func() { setElement(file.Root().AsSourceFile().Statements()) })
			mustPanic(t, "SetListElement to a list of another region", func() { setElement(otherList) })
			mustPanic(t, "SetListElement to a sealed synthetic list", func() { setElement(sealedList) })
			mustPanic(t, "SetListElement to the nil list", func() { setElement(store.List{}) })
			mustNotPanic(t, "SetListElement to a synthetic list of the region", func() { setElement(newList(f)) })
			// The binder's writers refuse the sentinel in every build too.
			mustPanic(t, "binder AddFlags to the sentinel", func() { file.Store.Node(0).AddFlags(ast.NodeFlagsSynthesized) })
			mustPanic(t, "binder SetFlowRef to the sentinel", func() { file.Store.Node(0).SetFlowRef(1) })
		})
	}
}

// 9. ReleaseArenas: the nodes made before are still read and written, and
// the next New goes to a new Store.
func TestStorageReleaseArenas(t *testing.T) {
	eachCapacity(t, func(t *testing.T, capacity int) string {
		p := newProgram(t, "let a = 1;")
		f := store.NewFactory(store.NewNodeRegion(p.space), capacity, store.FactoryHooks{})
		before := f.NewIdentifier("before")
		held := f.NewParenthesizedExpression(before)
		f.SetParent(before, held)
		f.ReleaseArenas()
		if before.Text() != "before" || !store.SameNode(held.Expression(), before) {
			t.Error("a node made before ReleaseArenas does not read")
		}
		f.SetRange(before, 7, 8)
		after := f.NewIdentifier("after")
		if storeID(after) == storeID(held) || storeID(after) == storeID(before) {
			t.Error("the New after ReleaseArenas went to an old Store")
		}
		f.SetParent(after, held)
		if !store.SameNode(after.Parent(), held) || held.Expression().Pos() != 7 {
			t.Error("a node made before ReleaseArenas cannot be written or linked")
		}
		return describe(held, after)
	})
}

// 10. The fakeScope of the node builder: the Block keeps the map, and the
// scope's parent reads the source Bound's locals.
func TestStorageFakeScope(t *testing.T) {
	eachCapacity(t, func(t *testing.T, capacity int) string {
		p := newProgram(t, "function g(p: number) { let q = p; }")
		file := p.files[0]
		f := store.NewFactory(store.NewNodeRegion(p.space), capacity, store.FactoryHooks{})
		declaration := find(t, file, ast.KindFunctionDeclaration)
		locals := store.SymbolTable{}
		scope := f.NewBlock(f.NewNodeList(nil), false)
		f.SetLocals(scope, locals)
		f.SetParent(scope, declaration)
		late := &store.Symbol{Name: "late"}
		locals["late"] = late
		if scope.Locals()["late"] != late || reflect.ValueOf(scope.Locals()).UnsafePointer() != reflect.ValueOf(locals).UnsafePointer() {
			t.Error("scope.Locals() is not the map that was set")
		}
		sourceLocals := scope.Parent().Locals()
		if sourceLocals["p"] == nil || sourceLocals["p"] != declaration.Locals()["p"] {
			t.Error("the scope's parent does not read the source locals")
		}
		if scope.Parent().Locals()["late"] != nil {
			t.Error("the source locals see the private map")
		}
		// Resolve's walk: the private scope first, then the source ancestors.
		var seen []string
		for n := scope; !n.IsNil(); n = n.Parent() {
			if l := n.Locals(); l != nil {
				for _, name := range []string{"late", "p", "q"} {
					if l[name] != nil {
						seen = append(seen, fmt.Sprintf("%v:%s", n.Kind(), name))
					}
				}
			}
		}
		if strings.Join(seen, " ") != "KindBlock:late KindFunctionDeclaration:p KindFunctionDeclaration:q" {
			t.Errorf("the walk of the locals saw %v", seen)
		}
		return describe(scope)
	})
}

// 11. The sizes. Symbol is recorded, not required.
func TestStorageSizes(t *testing.T) {
	for _, c := range []struct {
		name       string
		size, want uintptr
	}{
		{"NodeHeader", unsafe.Sizeof(store.NodeHeader{}), 32},
		{"Node", unsafe.Sizeof(store.Node{}), 16},
		{"Flow", unsafe.Sizeof(store.Flow{}), 16},
		{"FlowNode", unsafe.Sizeof(store.FlowNode{}), 16},
		{"NodeKey", unsafe.Sizeof(store.NodeKey{}), 8},
	} {
		if c.size != c.want {
			t.Errorf("%s is %d bytes, want %d", c.name, c.size, c.want)
		}
	}
	t.Logf("Symbol is %d bytes (ast.Symbol %d); gid is %d bytes", unsafe.Sizeof(store.Symbol{}), unsafe.Sizeof(ast.Symbol{}), unsafe.Sizeof(uint64(0)))
}

// 12. The flow graph through the resolving layer only (FlowNode, Antecedent,
// Antecedents, SwitchClause, ReduceLabel, Flow.Node), against the Pointer
// graph, without reading the Bound's tables. Two files use the same FlowRef
// numbers; a flow of the second, set on a synthetic node, is still resolved
// in the second.
const flowSourceA = `
declare function cond(): boolean;
function a(x: string | number, list: number[]) {
	let y: string | number = 0;
	if (typeof x === "string") { y = x; } else if (cond()) { return; }
	for (const n of list) { if (n > 1) break; y = n; }
	while (cond()) { if (cond()) continue; y = "w"; }
	switch (y) { case 0: y = 1; case 1: break; default: y = "d"; }
	try { y = 2; } catch (e) { y = 3; } finally { y = 4; }
	do { y = 5; } while (cond());
	return y;
}
`

const flowSourceB = `
declare function g(): number;
function b(v: number | undefined) {
	let w = v;
	try { if (v === undefined) throw 0; w = v + 1; } finally { w = w ?? g(); }
	switch (w) { case 1: case 2: return w; }
	return w && w > 0 ? w : 0;
}
`

func TestStorageFlowGraph(t *testing.T) {
	space := store.NewIDSpace(2)
	sources := []string{flowSourceA, flowSourceB}
	var files []*store.File
	var pointers []*ast.SourceFile
	for i, text := range sources {
		name := fmt.Sprintf("/flow%d.ts", i)
		files = append(files, parseAndBind(space, store.StoreID(i), name, text))
		pointer := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: name, Path: tspath.Path(name)}, text, core.ScriptKindTS)
		binder.BindSourceFile(pointer)
		pointers = append(pointers, pointer)
	}
	compared := 0
	for i := range files {
		pairs, mismatches := storetest.Pairs(pointers[i], files[i].Store)
		mismatches.Report(t)
		w := newFlowWalker(t, files[i])
		for _, pair := range pairs {
			if data := pair.P.FlowNodeData(); data != nil {
				w.compare(data.FlowNode, pair.N.FlowNode())
				compared++
			}
			if pair.P.Kind == ast.KindFunctionDeclaration {
				w.compare(pair.P.AsFunctionDeclaration().ReturnFlowNode, pair.N.AsFunctionDeclaration().ReturnFlowNode())
				w.compare(pair.P.BodyData().EndFlowNode, pair.N.EndFlowNode())
			}
		}
		t.Logf("%s: %d flows compared through %d node flows", files[i].FileName(), len(w.seen), compared)
		var flags ast.FlowFlags
		for p := range w.seen {
			flags |= p.Flags
		}
		wants := []ast.FlowFlags{ast.FlowFlagsBranchLabel, ast.FlowFlagsLoopLabel, ast.FlowFlagsCondition, ast.FlowFlagsSwitchClause, ast.FlowFlagsReduceLabel, ast.FlowFlagsAssignment}
		if i == 1 {
			wants = []ast.FlowFlags{ast.FlowFlagsSwitchClause, ast.FlowFlagsReduceLabel}
		}
		for _, want := range wants {
			if flags&want == 0 {
				t.Errorf("%s: no flow with %v was compared", files[i].FileName(), want)
			}
		}
	}

	// A flow of B on a synthetic node, whose number is also a flow of A.
	flowsA, _, _ := files[0].Bound.FlowCounts()
	var sourceB store.Node
	var pointerB *ast.Node
	pairs, _ := storetest.Pairs(pointers[1], files[1].Store)
	for _, pair := range pairs {
		if r := pair.N.FlowRef(); r > 1 && int(r) < flowsA && pair.N.Kind() == ast.KindIdentifier {
			sourceB, pointerB = pair.N, pair.P
		}
	}
	if sourceB.IsNil() {
		t.Fatal("no identifier of B with a flow number that A also has")
	}
	for _, capacity := range capacities {
		f := store.NewFactory(store.NewNodeRegion(space), capacity, store.FactoryHooks{})
		synthetic := f.NewIdentifier(sourceB.Text())
		f.SetNodeFlow(synthetic, sourceB.FlowNode())
		flow := synthetic.FlowNode()
		if store.FlowOwner(flow) != files[1].Store {
			t.Errorf("capacity %d: the synthetic node's flow is not B's", capacity)
		}
		w := newFlowWalker(t, files[1])
		w.compare(pointerB.FlowNodeData().FlowNode, flow)
	}
}

// flowWalker pairs Pointer flows with Store flows once each (a flow graph is
// cyclic) and checks that every node a Store flow reaches is in the file.
type flowWalker struct {
	t    *testing.T
	file *store.File
	seen map[*ast.FlowNode]store.Flow
}

func newFlowWalker(t *testing.T, file *store.File) *flowWalker {
	return &flowWalker{t: t, file: file, seen: map[*ast.FlowNode]store.Flow{}}
}

func (w *flowWalker) node(label string, p *ast.Node, n store.Node) {
	w.t.Helper()
	if p == nil || n.IsNil() {
		if (p == nil) != n.IsNil() {
			w.t.Errorf("%s: nil differs", label)
		}
		return
	}
	if n.Kind() != p.Kind || int(n.Pos()) != p.Pos() || int(n.End()) != p.End() {
		w.t.Errorf("%s: %v %d-%d, want %v %d-%d", label, n.Kind(), n.Pos(), n.End(), p.Kind, p.Pos(), p.End())
	}
	if n.File() != w.file {
		w.t.Errorf("%s: the node is in %s, want %s", label, fileName(n.File()), w.file.FileName())
	}
}

func (w *flowWalker) compare(p *ast.FlowNode, f store.Flow) {
	w.t.Helper()
	// Every nil is the zero Flow, whichever resolver gave it, so that == is
	// the Pointer == for nils too.
	if p == nil || f.IsNil() {
		if (p == nil) != (f == store.Flow{}) {
			w.t.Errorf("flow nil differs, or a nil is not the zero Flow: %v", f)
		}
		return
	}
	if prev, ok := w.seen[p]; ok {
		if prev != f {
			w.t.Errorf("a Pointer flow pairs with two Store flows")
		}
		return
	}
	w.seen[p] = f
	if store.FlowOwner(f) != w.file.Store {
		w.t.Errorf("a flow is owned by another Bound")
	}
	if f.Flags() != p.Flags {
		w.t.Errorf("flow flags %v, want %v", f.Flags(), p.Flags)
	}
	switch {
	case p.Flags&ast.FlowFlagsSwitchClause != 0:
		data := p.Node.AsFlowSwitchClauseData()
		statement, start, end := f.SwitchClause()
		w.node("SwitchStatement", data.SwitchStatement, statement)
		if start != int(data.ClauseStart) || end != int(data.ClauseEnd) {
			w.t.Errorf("clause range %d-%d, want %d-%d", start, end, data.ClauseStart, data.ClauseEnd)
		}
	case p.Flags&ast.FlowFlagsReduceLabel != 0:
		data := p.Node.AsFlowReduceLabelData()
		target, antecedents := f.ReduceLabel()
		w.compare(data.Target, target)
		w.list(data.Antecedents, antecedents)
	default:
		w.node("Node", p.Node, f.Node())
	}
	w.compare(p.Antecedent, f.Antecedent())
	w.list(p.Antecedents, f.Antecedents())
}

func (w *flowWalker) list(p *ast.FlowList, l store.FlowList) {
	w.t.Helper()
	for p != nil && !l.IsNil() {
		w.compare(p.Flow, l.Flow())
		p, l = p.Next, l.Next()
	}
	if p != nil || !l.IsNil() {
		w.t.Errorf("flow list lengths differ")
	}
}

// 13. The program token: a node of another program with the same StoreID
// numbers, or of another region, is refused at every import; a node of
// another Factory of the same region is taken.
func TestStorageImportBoundary(t *testing.T) {
	for _, capacity := range capacities {
		t.Run(fmt.Sprintf("capacity=%d", capacity), func(t *testing.T) {
			testImportBoundary(t, capacity)
		})
	}
}

func testImportBoundary(t *testing.T, capacity int) {
	a := newProgram(t, "let a = 1;")
	b := newProgram(t, "let a = 1;")
	sourceB := b.files[0].Root().AsSourceFile().Statements().At(0)
	if a.files[0].Root().Key() != b.files[0].Root().Key() {
		t.Fatal("the two programs do not reuse the StoreID numbers")
	}
	region := store.NewNodeRegion(a.space)
	f := store.NewFactory(region, capacity, store.FactoryHooks{})
	sameRegion := store.NewFactory(region, capacity, store.FactoryHooks{}).NewIdentifier("same")
	otherRegion := store.NewFactory(store.NewNodeRegion(a.space), capacity, store.FactoryHooks{}).NewIdentifier("other")
	outside := storeparser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/outside.ts", Path: "/outside.ts"}, "let a = 1;", core.ScriptKindTS)
	target := f.NewIdentifier("t")
	list := f.NewNodeList([]store.Node{target})
	for name, foreign := range map[string]store.Node{
		"a node of another program":         sourceB,
		"a node of another region":          otherRegion,
		"a node parsed outside any program": outside.Root().AsSourceFile().Statements().At(0),
	} {
		mustPanic(t, "New with "+name, func() { f.NewParenthesizedExpression(foreign) })
		mustPanic(t, "NewNodeList with "+name, func() { f.NewNodeList([]store.Node{foreign}) })
		mustPanic(t, "SetParent to "+name, func() { f.SetParent(target, foreign) })
		mustPanic(t, "SetListElement with "+name, func() { f.SetListElement(list, 0, foreign) })
		mustPanic(t, "Update with "+name, func() {
			f.UpdateParenthesizedExpression(f.NewParenthesizedExpression(target).AsParenthesizedExpression(), foreign)
		})
	}
	var flowB store.Flow
	for _, n := range storetest.Preorder(b.files[0].Root()) {
		if !n.FlowNode().IsNil() {
			flowB = n.FlowNode()
		}
	}
	if flowB.IsNil() {
		t.Fatal("no flow in the other program")
	}
	mustPanic(t, "SetNodeFlow with a flow of another program", func() { f.SetNodeFlow(target, flowB) })
	sourceA := a.files[0].Root().AsSourceFile().Statements().At(0)
	mustNotPanic(t, "the imports of the same program and region", func() {
		f.NewParenthesizedExpression(sameRegion)
		f.NewParenthesizedExpression(sourceA)
		f.SetParent(target, sameRegion)
		f.SetParent(target, sourceA)
		f.SetListElement(list, 0, sameRegion)
		f.SetListElement(list, 0, sourceA)
		f.SetParent(target, b.files[0].Store.Node(0)) // a nil of another program is nil
	})
	if !target.Parent().IsNil() {
		t.Error("SetParent to a nil did not clear the parent")
	}
}

// 14. A hook that re-enters New: the outer New returns the node of its own
// row, whatever the hook made.
func TestStorageHookReentry(t *testing.T) {
	eachCapacity(t, func(t *testing.T, capacity int) string {
		p := newProgram(t, "let a = 1;")
		var f *store.Factory
		var inner []store.Node
		f = store.NewFactory(store.NewNodeRegion(p.space), capacity, store.FactoryHooks{OnCreate: func(n store.Node) {
			if n.Kind() == ast.KindParenthesizedExpression {
				inner = append(inner, f.NewIdentifier("inner"))
			}
		}})
		child := f.NewIdentifier("child")
		outer := f.NewParenthesizedExpression(child)
		if outer.Kind() != ast.KindParenthesizedExpression || !store.SameNode(outer.Expression(), child) {
			t.Error("the outer New did not return its own node")
		}
		if len(inner) != 1 || store.SameNode(inner[0], outer) || inner[0].Text() != "inner" {
			t.Fatal("the hook's node is confused with the outer node")
		}
		if capacity == 1 && storeID(inner[0]) == storeID(outer) {
			t.Error("capacity 1 did not change the active Store in the hook")
		}
		next := f.NewIdentifier("next")
		f.SetParent(child, outer)
		f.SetParent(inner[0], outer)
		return describe(outer, next, inner[0])
	})
}

// 15. extra and texts are reallocated in the middle of a setter: the List
// held reads the newest elements, and a Text read before keeps its value.
func TestStorageColumnGrowth(t *testing.T) {
	eachCapacity(t, func(t *testing.T, capacity int) string {
		p := newProgram(t, "let a = 1;")
		f := store.NewFactory(store.NewNodeRegion(p.space), capacity*64, store.FactoryHooks{})
		store.SetInitialCaps(f, 3, 0)
		first := f.NewIdentifier("first")
		firstText := first.Text()
		list := f.NewNodeList([]store.Node{first, first, first})
		owner := store.ListStoreOf(list)
		extraCap, textsCap := store.ExtraCap(owner), store.TextsCap(owner)
		// The tuple rewrite: the right-hand side makes nodes, then the element
		// is written.
		for i := range 3 {
			f.SetListElement(list, i, f.NewParenthesizedExpression(f.NewIdentifier(strings.Repeat("e", 20+i))))
		}
		if store.ExtraCap(owner) == extraCap {
			t.Error("extra was not reallocated")
		}
		if store.TextsCap(owner) == textsCap {
			t.Error("texts was not reallocated")
		}
		for i := range 3 {
			if got := list.At(i).Expression().Text(); got != strings.Repeat("e", 20+i) {
				t.Errorf("element %d reads %q", i, got)
			}
		}
		if firstText != "first" || first.Text() != "first" {
			t.Error("a text changed when texts grew")
		}
		return describe(f.NewArrayLiteralExpression(list, false))
	})
}

// 16. GetSymbolId at the end of the range: the last id is given, then it
// panics without reissuing 0 or an earlier id. The counter is moved in a
// subprocess only.
func TestSymbolIdLimit(t *testing.T) {
	if os.Getenv("STORE_SYMBOL_ID_LIMIT") == "1" {
		store.SetNextSymbolId(math.MaxUint64 - 2)
		a, b, c := &store.Symbol{}, &store.Symbol{}, &store.Symbol{}
		if id := store.GetSymbolId(a); id != math.MaxUint64-1 {
			t.Fatalf("id %d", id)
		}
		if id := store.GetSymbolId(b); id != math.MaxUint64 {
			t.Fatalf("id %d", id)
		}
		for range 2 {
			mustPanic(t, "an id past the range", func() { store.GetSymbolId(c) })
		}
		if store.GetSymbolId(a) != math.MaxUint64-1 || store.GetSymbolId(b) != math.MaxUint64 {
			t.Fatal("an id given before changed")
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSymbolIdLimit$", "-test.count=1")
	cmd.Env = append(os.Environ(), "STORE_SYMBOL_ID_LIMIT=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "PASS") {
		t.Fatalf("the subprocess failed: %v\n%s", err, out)
	}
}

// 17. The gid and the private names: one id per symbol however called, and
// the private name of a class member uses it, so that classes of two files
// with the same local SymbolId do not collide, and the bind and a later
// lookup build the same key.
func TestSymbolIdAndPrivateNames(t *testing.T) {
	a, b := &store.Symbol{}, &store.Symbol{}
	if store.GetSymbolId(a) == 0 || store.GetSymbolId(a) != store.GetSymbolId(a) || store.GetSymbolId(a) == store.GetSymbolId(b) {
		t.Error("GetSymbolId is not a stable unique non-zero id")
	}
	for range 20 {
		s := &store.Symbol{}
		ids := make([]uint64, 16)
		var wg sync.WaitGroup
		for i := range ids {
			wg.Go(func() { ids[i] = store.GetSymbolId(s) })
		}
		wg.Wait()
		for _, id := range ids {
			if id == 0 || id != ids[0] {
				t.Fatalf("parallel first calls gave %v", ids)
			}
		}
	}

	const source = `
class C {
	#x = 1;
	static #y = 2;
	#m() { return this.#x; }
	n() {
		class Inner { #x = 3; }
		return new Inner();
	}
}
class D extends C { #x = 4; static #y = 5; }
`
	for _, order := range [][]int{{0, 1}, {1, 0}} {
		space := store.NewIDSpace(2)
		files := make([]*store.File, 2)
		// Bind in the given order: the gids are given in bind order.
		for _, i := range order {
			files[i] = parseAndBind(space, store.StoreID(i), fmt.Sprintf("/p%d.ts", i), source)
		}
		keys := map[string]string{}
		var classIds []store.SymbolId
		for i, file := range files {
			for _, cls := range findAll(file, ast.KindClassDeclaration) {
				symbol := cls.Symbol()
				classIds = append(classIds, symbol.Id())
				members := cls.Members()
				for j := range members.Len() {
					member := members.At(j)
					name := member.Name()
					if name.IsNil() || name.Kind() != ast.KindPrivateIdentifier {
						continue
					}
					key := storebinder.GetSymbolNameForPrivateIdentifier(symbol, name.Text())
					want := ast.InternalSymbolNamePrefix + fmt.Sprintf("#%d@%s", store.GetSymbolId(symbol), name.Text())
					if key != want {
						t.Errorf("private name %q, want %q", key, want)
					}
					table := symbol.Members
					if store.HasStaticModifier(member) {
						table = symbol.Exports
					}
					declared := table[key]
					if declared == nil || declared != member.Symbol() || !store.SameNode(declared.ValueDeclaration, member) {
						t.Errorf("file %d class %s: %s does not look up its declaration", i, cls.Name().Text(), key)
					}
					if other, ok := keys[key]; ok {
						t.Errorf("%s is the key of %s and of file %d %s", key, other, i, cls.Name().Text())
					}
					keys[key] = fmt.Sprintf("file %d %s", i, cls.Name().Text())
				}
			}
		}
		if len(keys) != 2*6 {
			t.Errorf("%d private keys, want 12", len(keys))
		}
		// The same local SymbolIds in both files: the collision the gid avoids.
		half := len(classIds) / 2
		for i := range half {
			if classIds[i] != classIds[half+i] {
				t.Fatalf("the classes of the two files have different local ids: %v", classIds)
			}
		}
	}
}

// 18. A growing Factory, built as the node builder's EmitContext builds it
// (storeprinter/region.go): the first Store, and the first after a release,
// holds 16 nodes, and the Store after a full one twice as many, up to 4,096,
// with extra and texts in proportion. Each node is in the Store it was made
// in, before and after a release and a change of capacity, and the edges
// across the boundaries read.
func TestStorageGrowingFactory(t *testing.T) {
	p := newProgram(t, "let a = 1;")
	f := store.NewGrowingFactory(store.NewNodeRegion(p.space), 16, 4096, store.FactoryHooks{})
	type made struct {
		node store.Node
		text string
	}
	var all []made
	newIdentifier := func() store.Node {
		text := fmt.Sprintf("n%d", len(all))
		n := f.NewIdentifier(text)
		all = append(all, made{n, text})
		return n
	}
	// fill makes count identifiers and checks that they are in one new Store
	// of rows nodes, after the Store of last.
	fill := func(count, rows int, last store.Node) []store.Node {
		t.Helper()
		nodes := make([]store.Node, count)
		for i := range nodes {
			nodes[i] = newIdentifier()
		}
		s := store.StoreOf(nodes[0])
		if !last.IsNil() && s == store.StoreOf(last) {
			t.Fatalf("the Store of %d nodes is the Store before", rows)
		}
		if store.NodeCapacity(s) != rows || store.ExtraCap(s) != 16*rows || store.TextsCap(s) < 8*rows {
			t.Fatalf("a Store of %d nodes, %d extra, %d texts; want %d, %d, at least %d",
				store.NodeCapacity(s), store.ExtraCap(s), store.TextsCap(s), rows, 16*rows, 8*rows)
		}
		for _, n := range nodes {
			if store.StoreOf(n) != s {
				t.Fatalf("the nodes of a Store of %d nodes are in two Stores", rows)
			}
		}
		return nodes
	}

	last := store.Node{}
	for _, rows := range []int{16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 4096} {
		nodes := fill(rows, rows, last)
		last = nodes[len(nodes)-1]
	}
	first := all[0].node
	partial := fill(5, 4096, last)

	// A release from a Store that is not full: the next node is in a new
	// Store of 16 nodes, and the nodes before are read and written.
	f.ReleaseArenas()
	small := fill(16, 16, partial[0])
	f.SetRange(first, 3, 4)
	f.SetRange(partial[4], 5, 6)
	if first.Pos() != 3 || partial[4].Pos() != 5 || small[0].Pos() != -1 {
		t.Error("a write after the release went to another node")
	}

	// Across the boundary of the full Store of 16 and the next, of 32: the
	// child edge of the new parent and the parent edge written into the old
	// Store are foreign entries.
	parent := f.NewParenthesizedExpression(small[15])
	next := store.StoreOf(parent)
	if next == store.StoreOf(small[15]) || store.NodeCapacity(next) != 32 {
		t.Fatalf("the node after a full Store of 16 is in a Store of %d nodes", store.NodeCapacity(next))
	}
	f.SetParent(small[15], parent)
	f.SetParent(first, parent)
	if !store.SameNode(parent.Expression(), small[15]) || !store.SameNode(small[15].Parent(), parent) ||
		!store.SameNode(first.Parent(), parent) {
		t.Error("an edge across a Store boundary does not read")
	}
	if store.ForeignNodeCount(next) != 1 || store.ForeignNodeCount(store.StoreOf(small[15])) != 1 ||
		store.ForeignNodeCount(store.StoreOf(first)) != 1 {
		t.Error("an edge across a Store boundary is not a foreign entry")
	}
	// A node of the new Store whose child was made before the release.
	if wrapped := f.NewParenthesizedExpression(first); !store.SameNode(wrapped.Expression(), first) || store.StoreOf(wrapped) != next {
		t.Error("a node after the release does not read its child from before it")
	}

	for i, m := range all {
		if m.node.Text() != m.text {
			t.Fatalf("node %d reads %q, want %q", i, m.node.Text(), m.text)
		}
	}
}
