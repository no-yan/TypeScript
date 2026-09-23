// Package storetest compares a Store with the Pointer AST it was built from,
// node by node. It is shared by the tests of store (the converter) and of
// storeparser (the parser). The member and role comparisons are generated
// into equivalence_generated.go.
package storetest

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
)

type Options struct {
	// SkipReparsed treats a Pointer node with NodeFlagsReparsed as nil, and
	// leaves it out of lists: the Store parser does not build the nodes that
	// the Pointer parser reparses from JSDoc in JS files.
	SkipReparsed bool
	// MaskFlags are left out of the Flags comparison.
	MaskFlags ast.NodeFlags
}

// Mismatches counts every difference by kind and label, so one run shows
// everything.
type Mismatches map[string]int

func (m Mismatches) Report(t *testing.T) {
	t.Helper()
	if len(m) == 0 {
		return
	}
	lines := make([]string, 0, len(m))
	for label, count := range m {
		lines = append(lines, fmt.Sprintf("%6d  %s", count, label))
	}
	sort.Strings(lines)
	t.Errorf("Store differs from Pointer:\n%s", strings.Join(lines, "\n"))
}

type comparer struct {
	Mismatches
	opts Options
}

func (m *comparer) add(owner *ast.Node, label string) {
	m.Mismatches[owner.Kind.String()+" "+label]++
}

// isJSDocCast reports a JSDoc type cast (/** @type {T} */ (e), @satisfies):
// the Pointer parser wraps e in an AsExpression or SatisfiesExpression that
// is not itself flagged Reparsed, only its type is.
func isJSDocCast(p *ast.Node) bool {
	return (p.Kind == ast.KindAsExpression || p.Kind == ast.KindSatisfiesExpression) && p.Type().Flags&ast.NodeFlagsReparsed != 0
}

// skip is the Pointer node as the Store should see it: nil for a reparsed
// node, the wrapped expression for a JSDoc cast.
func (m *comparer) skip(p *ast.Node) *ast.Node {
	for p != nil && m.opts.SkipReparsed {
		if p.Flags&ast.NodeFlagsReparsed != 0 {
			return nil
		}
		if isJSDocCast(p) {
			p = p.Expression()
			continue
		}
		return p
	}
	return p
}

// parentOf is the parent as the Store should see it, past any JSDoc cast.
func (m *comparer) parentOf(p *ast.Node) *ast.Node {
	parent := p.Parent
	for m.opts.SkipReparsed && parent != nil && isJSDocCast(parent) {
		parent = parent.Parent
	}
	return parent
}

// kept is the Pointer list as the Store should see it: without the reparsed
// elements.
func (m *comparer) kept(p []*ast.Node) []*ast.Node {
	if !m.opts.SkipReparsed {
		return p
	}
	return slices.DeleteFunc(slices.Clone(p), func(n *ast.Node) bool { return n.Flags&ast.NodeFlagsReparsed != 0 })
}

func (m *comparer) node(label string, owner *ast.Node, p *ast.Node, n store.Node) {
	p = m.skip(p)
	if p == nil && n.IsNil() {
		return
	}
	if p == nil || n.Kind() != p.Kind || int(n.Pos()) != p.Pos() || int(n.End()) != p.End() {
		m.add(owner, label)
	}
}

func (m *comparer) elements(label string, owner *ast.Node, p []*ast.Node, l store.List) {
	if l.Len() != len(p) {
		m.add(owner, label)
		return
	}
	for i, elem := range p {
		m.node(label+" element", owner, elem, l.At(i))
	}
}

// list compares a list. A Pointer list that holds only reparsed nodes may be
// one the reparser made where the Store has none, so it matches a nil list.
func (m *comparer) list(label string, owner *ast.Node, p *ast.NodeList, l store.List) {
	if p == nil && l.IsNil() {
		return
	}
	if p != nil && l.IsNil() && m.opts.SkipReparsed && len(m.kept(p.Nodes)) == 0 {
		return
	}
	if p == nil || l.IsNil() || int(l.Pos()) != p.Pos() || int(l.End()) != p.End() {
		m.add(owner, label)
		return
	}
	m.elements(label, owner, m.kept(p.Nodes), l)
}

func (m *comparer) modifiers(label string, owner *ast.Node, p *ast.ModifierList, l store.List) {
	if p == nil {
		m.list(label, owner, nil, l)
	} else {
		m.list(label, owner, &p.NodeList, l)
	}
}

// A raw list has no Loc.
func (m *comparer) raw(label string, owner *ast.Node, p []*ast.Node, l store.List) {
	if p == nil && l.IsNil() {
		return
	}
	if p != nil && l.IsNil() && m.opts.SkipReparsed && len(m.kept(p)) == 0 {
		return
	}
	if p == nil || l.IsNil() || l.Pos() != 0 || l.End() != 0 {
		m.add(owner, label)
		return
	}
	m.elements(label, owner, m.kept(p), l)
}

// guard runs a comparison whose Pointer side may panic on this kind.
func (m *comparer) guard(owner *ast.Node, label string, compare func()) {
	defer func() {
		if recover() != nil {
			m.add(owner, label+" (Pointer panics)")
		}
	}()
	compare()
}

// Preorder is the order of the Pointer walk, so the two walks line up.
func Preorder(root store.Node) []store.Node {
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

func preorderPointer(root *ast.Node, skipReparsed bool) []*ast.Node {
	var nodes []*ast.Node
	var visit ast.Visitor
	visit = func(n *ast.Node) bool {
		if skipReparsed && n.Flags&ast.NodeFlagsReparsed != 0 {
			return false
		}
		if skipReparsed && isJSDocCast(n) {
			return visit(n.Expression())
		}
		nodes = append(nodes, n)
		n.ForEachChild(visit)
		return false
	}
	visit(root)
	return nodes
}

// Pair is a Pointer node and the Store node at the same preorder position.
type Pair struct {
	P *ast.Node
	N store.Node
}

// Pairs walks the Pointer AST and the Store in preorder together (reparsed
// nodes and JSDoc casts skipped as opts says) and pairs the nodes up. When the
// two walks visit a different number of nodes, the mismatch is reported and
// no pairs are returned.
func Pairs(file *ast.SourceFile, s *store.Store, opts Options) ([]Pair, Mismatches) {
	m := Mismatches{}
	pointers := preorderPointer(file.AsNode(), opts.SkipReparsed)
	nodes := Preorder(s.Root())
	if len(pointers) != len(nodes) {
		m[fmt.Sprintf("visits %d, want %d", len(nodes), len(pointers))]++
		return nil, m
	}
	pairs := make([]Pair, len(nodes))
	for i, n := range nodes {
		pairs[i] = Pair{pointers[i], n}
	}
	return pairs, m
}

// Equivalent compares every pair of Pairs: header, parent, then every member
// of its definition and every role accessor. The second result is the number
// of Store nodes visited.
func Equivalent(file *ast.SourceFile, s *store.Store, opts Options) (Mismatches, int) {
	pairs, mismatches := Pairs(file, s, opts)
	if pairs == nil {
		return mismatches, len(Preorder(s.Root()))
	}
	m := &comparer{mismatches, opts}
	for _, pair := range pairs {
		p, n := pair.P, pair.N
		m.node("self", p, p, n)
		m.node("Parent", p, m.parentOf(p), n.Parent())
		if (n.Flags()^p.Flags)&^opts.MaskFlags != 0 {
			m.add(p, "Flags")
		}
		if n.ModifierFlags() != ast.ModifiersToFlags(m.kept(p.ModifierNodes()))&0xFFFF {
			m.add(p, "ModifierFlags")
		}
		// The typed views read the payload of p.Kind, so they are only
		// meaningful when the kinds agree; "self" has already counted the case.
		if n.Kind() == p.Kind {
			compareMembers(m, p, n)
			compareRoles(m, p, n)
		}
	}
	return m.Mismatches, len(pairs)
}
