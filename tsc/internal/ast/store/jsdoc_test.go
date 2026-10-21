package store_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/storetest"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/storeparser"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// The contract of the lazy JSDoc (J-d, invariants 22, 24, 25 and 27 of
// internal/ast/docs/store-checker-design-20260923.md, and
// store-checker-7d1-implementation-instructions-20260928.md 6.2): the
// program's TS files are parsed by storeparser, which registers the lazy
// parser.

func sourceOptions(name string) ast.SourceFileParseOptions {
	return ast.SourceFileParseOptions{FileName: tspath.ToRootedFilePath(name, "/"), PathKey: tspath.CaseSensitive.PathKey(tspath.ToRootedPath(name, "/"))}
}

// countParses wraps the registered lazy parser for the test.
func countParses(t *testing.T) *atomic.Int64 {
	t.Helper()
	parse := store.ParseJSDocForNodeFunc()
	var n atomic.Int64
	store.SetParseJSDocForNode(func(f *store.File, host store.Node) []store.Node {
		n.Add(1)
		return parse(f, host)
	})
	t.Cleanup(func() { store.SetParseJSDocForNode(parse) })
	return &n
}

// hosts are the nodes of file with HasJSDoc, in preorder.
func hosts(file *store.File) []store.Node {
	var hosts []store.Node
	for _, n := range storetest.Preorder(file.Root()) {
		if n.Flags()&ast.NodeFlagsHasJSDoc != 0 {
			hosts = append(hosts, n)
		}
	}
	return hosts
}

const jsdocSource = `/** f's doc
 * @param x the x
 */
function f(x: number) {}
/** @deprecated */
function g() {}
class C {
	/** m's doc */
	m() {}
}`

func TestLazyJSDocOnce(t *testing.T) {
	parses := countParses(t)
	p := newProgram(t, jsdocSource)
	file := p.files[0]
	hs := hosts(file)
	if len(hs) != 3 {
		t.Fatalf("%d hosts, want 3", len(hs))
	}
	for i, host := range hs {
		if eager := host.EagerJSDoc(file); eager != nil {
			t.Errorf("host %d has eager JSDoc %v before the first access", i, eager)
		}
		first := host.JSDoc(file)
		second := host.JSDoc(nil) // the host's own file
		if got := parses.Load(); got != int64(i+1) {
			t.Fatalf("after two reads of host %d: %d parses, want %d", i, got, i+1)
		}
		if len(first) != 1 || len(second) != 1 || !store.SameNode(first[0], second[0]) {
			t.Fatalf("host %d: %v then %v", i, first, second)
		}
		if eager := host.EagerJSDoc(file); len(eager) != 1 || !store.SameNode(eager[0], first[0]) {
			t.Errorf("host %d: EagerJSDoc after the parse is %v", i, eager)
		}
		root := first[0]
		s := store.StoreOf(root)
		switch {
		case root.Kind() != ast.KindJSDoc:
			t.Errorf("host %d: root is %v", i, root.Kind())
		case !store.SameNode(root.Parent(), host):
			t.Errorf("host %d: the root's parent is not the host", i)
		case !store.SameNode(root.Parent().Parent(), host.Parent()):
			t.Errorf("host %d: the walk up from the root does not continue in the source Store", i)
		case root.File() != file:
			t.Errorf("host %d: the root's File is not the host's", i)
		case s == file.Store || s.ID() < store.StoreID(len(p.files)):
			t.Errorf("host %d: the JSDoc Store has StoreID %d, inside the source range 0..%d", i, s.ID(), len(p.files)-1)
		case !store.IsSealed(s):
			t.Errorf("host %d: the JSDoc Store is not sealed", i)
		}
		for _, n := range storetest.Preorder(root) {
			if n.File() != file {
				t.Errorf("host %d: %v in the JSDoc has another File", i, n.Kind())
			}
		}
	}
	// Sealed: the Factory's writers refuse the Store in every build, the
	// binder's in a storeChecks build. (The Builder that made it was copied by
	// Finish and never reaches it.)
	root := hs[0].JSDoc(file)[0]
	f := store.NewFactory(store.NewNodeRegion(p.space), 1<<12, store.FactoryHooks{})
	mustPanic(t, "a Factory write to a lazy JSDoc node", func() { f.AddFlags(root, ast.NodeFlagsReparsed) })
	mustPanic(t, "a Factory SetParent of a lazy JSDoc node", func() { f.SetParent(root, store.Node{}) })
	if store.StoreChecks {
		mustPanic(t, "a binder write to a lazy JSDoc node", func() { root.AddFlags(ast.NodeFlagsReparsed) })
	}
	if tags := hs[0].JSDoc(file)[0].AsJSDoc().Tags(); tags.Len() != 1 || tags.At(0).Kind() != ast.KindJSDocParameterTag {
		t.Errorf("f's JSDoc tags: %d", tags.Len())
	}
	if tags := hs[1].JSDoc(file)[0].AsJSDoc().Tags(); tags.Len() != 1 || tags.At(0).Kind() != ast.KindJSDocDeprecatedTag {
		t.Errorf("g's JSDoc tags: %d", tags.Len())
	}
}

// A host whose flag comes from the scanner but that has no comment range: the
// empty result is recorded, and takes no StoreID.
func TestLazyJSDocEmpty(t *testing.T) {
	for _, source := range []string{
		"let i = 0;/** c */\nif (i) {}",          // a comment on the previous line's end is not leading
		"var z: /** c */ (x: number) => string;", // the function type starts after the comment
	} {
		t.Run(source, func(t *testing.T) {
			parses := countParses(t)
			p := newProgram(t, source)
			file := p.files[0]
			hs := hosts(file)
			if len(hs) != 1 {
				t.Fatalf("%d hosts, want 1", len(hs))
			}
			before := p.space.Next()
			if jsdoc := hs[0].JSDoc(file); jsdoc != nil {
				t.Fatalf("JSDoc of %v = %v, want nil", hs[0].Kind(), jsdoc)
			}
			if after := p.space.Next(); after != before+1 {
				t.Errorf("the empty result took StoreIDs %d..%d", before+1, after-1)
			}
			hs[0].JSDoc(file)
			if got := parses.Load(); got != 1 {
				t.Errorf("%d parses after two reads, want 1", got)
			}
		})
	}
}

// A region of the same program takes a lazy JSDoc node as a child; one of
// another program does not (invariant 26), nor does any region take the lazy
// JSDoc of a file parsed outside a program, whose Store is outside too.
func TestLazyJSDocImport(t *testing.T) {
	for _, capacity := range capacities {
		t.Run(fmt.Sprintf("capacity=%d", capacity), func(t *testing.T) {
			a := newProgram(t, jsdocSource)
			b := newProgram(t, jsdocSource)
			root := hosts(a.files[0])[0].JSDoc(nil)[0]
			same := store.NewFactory(store.NewNodeRegion(a.space), capacity, store.FactoryHooks{})
			other := store.NewFactory(store.NewNodeRegion(b.space), capacity, store.FactoryHooks{})
			mustNotPanic(t, "a JSDoc node of the same program", func() {
				n := same.NewParenthesizedExpression(root)
				if !store.SameNode(n.Expression(), root) {
					t.Error("the child is not the JSDoc root")
				}
			})
			mustPanic(t, "a JSDoc node of another program", func() { other.NewParenthesizedExpression(root) })

			outside := storeparser.ParseSourceFile(sourceOptions("/outside.ts"), jsdocSource, core.ScriptKindTS)
			outsideRoot := hosts(outside)[0].JSDoc(nil)[0]
			if s := store.StoreOf(outsideRoot); s.ID() != 0 {
				t.Errorf("the JSDoc Store of a file outside a program has StoreID %d", s.ID())
			}
			if !store.SameNode(outsideRoot.Parent(), hosts(outside)[0]) || outsideRoot.File() != outside {
				t.Error("the JSDoc root of a file outside a program does not lead to its host")
			}
			mustPanic(t, "a JSDoc node of a file outside a program", func() { same.NewParenthesizedExpression(outsideRoot) })
		})
	}
}

// Invariant 27: a host of another Store than the file's source Store panics in
// every build, on the lazy and the eager path.
func TestJSDocHostCheck(t *testing.T) {
	p := newProgram(t, jsdocSource, jsdocSource)
	f := store.NewFactory(store.NewNodeRegion(p.space), 1<<12, store.FactoryHooks{})
	synthetic := f.NewIdentifier("x")
	f.AddFlags(synthetic, ast.NodeFlagsHasJSDoc)
	other := hosts(p.files[1])[0]
	js := storeparser.ParseSourceFile(sourceOptions("/a.js"), "/** @type {number} */\nvar x = 1;", core.ScriptKindJS)
	for _, file := range []*store.File{p.files[0], js} {
		mustPanic(t, "JSDoc of a synthetic host", func() { synthetic.JSDoc(file) })
		mustPanic(t, "EagerJSDoc of a synthetic host", func() { synthetic.EagerJSDoc(file) })
		mustPanic(t, "JSDoc of a host of another file", func() { other.JSDoc(file) })
	}
	mustNotPanic(t, "JSDoc of a host of the file", func() { hosts(p.files[0])[0].JSDoc(p.files[0]) })
	if jsdoc := hosts(js)[0].JSDoc(nil); len(jsdoc) != 1 {
		t.Errorf("the eager JSDoc of a JS host: %v", jsdoc)
	}
}

// Many goroutines read the JSDoc of the same file, the same hosts and
// different ones: each host is parsed once and every reader sees the same
// nodes. Run with -race.
func TestLazyJSDocConcurrent(t *testing.T) {
	parses := countParses(t)
	var source string
	for i := range 64 {
		source += fmt.Sprintf("/** doc %d @param x a */\nfunction f%d(x: number) {}\n", i, i)
	}
	p := newProgram(t, source)
	file := p.files[0]
	hs := hosts(file)
	const readers = 16
	results := make([][]store.Node, readers)
	var wg sync.WaitGroup
	for r := range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := make([]store.Node, len(hs))
			for k := range hs {
				i := (k*7 + r*13) % len(hs) // a different order per reader
				jsdoc := hs[i].JSDoc(file)
				if len(jsdoc) != 1 {
					panic("a host without its comment")
				}
				got[i] = jsdoc[0]
			}
			results[r] = got
		}()
	}
	wg.Wait()
	if got := parses.Load(); got != int64(len(hs)) {
		t.Errorf("%d parses for %d hosts", got, len(hs))
	}
	for r := 1; r < readers; r++ {
		for i := range hs {
			if !store.SameNode(results[r][i], results[0][i]) {
				t.Fatalf("reader %d sees another JSDoc for host %d", r, i)
			}
		}
	}
}

// The Builder API of the reparse (instructions 3.1).
func TestBuilderReparseAPI(t *testing.T) {
	b := store.NewBuilder("a.b", nil, 0)
	token := b.NewToken(ast.KindAnyKeyword, 0, 0, 0)
	mustPanic(t, "SetType on a kind without Type", func() { b.SetType(token, token) })
	mustPanic(t, "SetObjectAssignmentInitializer on a kind without it", func() { b.SetObjectAssignmentInitializer(token, token) })
	mustPanic(t, "SetPhaseModifier on a kind without it", func() { b.SetPhaseModifier(token, ast.KindTypeKeyword) })

	// SetModifiers folds the header's modifier flags again.
	name := b.NewIdentifier(0, 0, 1, "a")
	property := b.NewPropertyDeclaration(0, 0, 1, store.NoListRef, name, store.NoNodeRef, store.NoNodeRef, store.NoNodeRef)
	readonly := b.NewToken(ast.KindReadonlyKeyword, ast.NodeFlagsReparsed, 0, 0)
	b.SetModifiers(property, b.List(0, 0, []store.NodeRef{readonly}))
	if b.View(property).ModifierFlags() != ast.ModifierFlagsReadonly {
		t.Errorf("SetModifiers: modifier flags %v", b.View(property).ModifierFlags())
	}
	if b.View(readonly).Parent().Ref() != property {
		t.Error("SetModifiers did not write the parent of the new modifier")
	}

	// DeepCloneReparse: the root alone gains Reparsed, the ranges and the
	// other flags stay, the copies' parents are the copies.
	left := b.NewIdentifier(ast.NodeFlagsJSDoc, 0, 1, "a")
	right := b.NewIdentifier(ast.NodeFlagsJSDoc, 2, 3, "b")
	qualified := b.NewQualifiedName(ast.NodeFlagsJSDoc, 0, 3, left, right)
	clone := b.DeepCloneReparse(qualified)
	c := b.View(clone)
	switch {
	case clone == qualified || c.Kind() != ast.KindQualifiedName:
		t.Fatal("DeepCloneReparse did not copy")
	case c.Flags() != ast.NodeFlagsJSDoc|ast.NodeFlagsReparsed:
		t.Errorf("clone flags %v", c.Flags())
	case c.Pos() != 0 || c.End() != 3:
		t.Errorf("clone range %d-%d", c.Pos(), c.End())
	}
	for i, child := range []store.Node{c.AsQualifiedName().Left(), c.AsQualifiedName().Right()} {
		original := b.View([]store.NodeRef{left, right}[i])
		switch {
		case child.Ref() == original.Ref():
			t.Errorf("child %d is shared with the original", i)
		case child.Flags() != ast.NodeFlagsJSDoc:
			t.Errorf("child %d flags %v", i, child.Flags())
		case child.Pos() != original.Pos() || child.End() != original.End() || child.Text() != original.Text():
			t.Errorf("child %d differs from the original", i)
		case child.Parent().Ref() != clone:
			t.Errorf("child %d's parent is not the clone", i)
		}
	}
	if b.View(left).Parent().Ref() != qualified {
		t.Error("DeepCloneReparse moved a parent of the original")
	}

	// ReparentChildren: the direct children's parents are the node again.
	b.SetParent(left, clone)
	b.ReparentChildren(qualified)
	if b.View(left).Parent().Ref() != qualified || b.View(right).Parent().Ref() != qualified {
		t.Error("ReparentChildren did not reset the parents")
	}
}
