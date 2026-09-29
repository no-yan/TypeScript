// Package storetest compares a Store with the Pointer AST it was built from,
// node by node. It is shared by the tests of store (the converter) and of
// storeparser (the parser). The member and role comparisons are generated
// into equivalence_generated.go.
package storetest

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
)

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
}

func (m *comparer) add(owner *ast.Node, label string) {
	m.Mismatches[owner.Kind.String()+" "+label]++
}

func (m *comparer) node(label string, owner *ast.Node, p *ast.Node, n store.Node) {
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

func (m *comparer) list(label string, owner *ast.Node, p *ast.NodeList, l store.List) {
	if p == nil && l.IsNil() {
		return
	}
	if p == nil || l.IsNil() || int(l.Pos()) != p.Pos() || int(l.End()) != p.End() {
		m.add(owner, label)
		return
	}
	m.elements(label, owner, p.Nodes, l)
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
	if p == nil || l.IsNil() || l.Pos() != 0 || l.End() != 0 {
		m.add(owner, label)
		return
	}
	m.elements(label, owner, p, l)
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

// Pair is a Pointer node and the Store node at the same preorder position.
type Pair struct {
	P *ast.Node
	N store.Node
}

// Pairs walks the Pointer AST and the Store in preorder together and pairs
// the nodes up. When the two walks visit a different number of nodes, the
// mismatch is reported and no pairs are returned.
func Pairs(file *ast.SourceFile, s *store.Store) ([]Pair, Mismatches) {
	return pairs(file.AsNode(), s.Root())
}

func pairs(p *ast.Node, n store.Node) ([]Pair, Mismatches) {
	m := Mismatches{}
	pointers := preorderPointer(p)
	nodes := Preorder(n)
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
func Equivalent(file *ast.SourceFile, s *store.Store) (Mismatches, int) {
	pairs, mismatches := Pairs(file, s)
	if pairs == nil {
		return mismatches, len(Preorder(s.Root()))
	}
	m := &comparer{mismatches}
	for _, pair := range pairs {
		m.pair(pair.P, pair.N)
	}
	return m.Mismatches, len(pairs)
}

func (m *comparer) pair(p *ast.Node, n store.Node) {
	m.node("self", p, p, n)
	m.node("Parent", p, p.Parent, n.Parent())
	if n.Flags() != p.Flags {
		m.add(p, "Flags")
	}
	if n.ModifierFlags() != ast.ModifiersToFlags(p.ModifierNodes())&0xFFFF {
		m.add(p, "ModifierFlags")
	}
	// The typed views read the payload of p.Kind, so they are only
	// meaningful when the kinds agree; "self" has already counted the case.
	if n.Kind() == p.Kind {
		compareMembers(m, p, n)
		compareRoles(m, p, n)
	}
}

// EquivalentJSDoc compares the JSDoc of every host of the tree (a node with
// HasJSDoc; JSDoc hosts no JSDoc): the number of comments, then each
// comment's tree as Equivalent compares one, the root's parent included (the
// host, or for a @typedef's type alias the host it was parsed for). JSDoc(file)
// parses the lazy JSDoc of a TS file on both sides. The second result is the
// number of JSDoc nodes compared.
func EquivalentJSDoc(pointer *ast.SourceFile, file *store.File) (Mismatches, int) {
	all, mismatches := Pairs(pointer, file.Store)
	if all == nil {
		return mismatches, 0
	}
	m := &comparer{mismatches}
	compared := 0
	for _, host := range all {
		if host.P.Flags&ast.NodeFlagsHasJSDoc == 0 {
			continue
		}
		ps, ns := host.P.JSDoc(pointer), host.N.JSDoc(file)
		if len(ps) != len(ns) {
			m.add(host.P, fmt.Sprintf("JSDoc count %d, want %d", len(ns), len(ps)))
			continue
		}
		for i, p := range ps {
			jsdocPairs, jm := pairs(p, ns[i])
			if jsdocPairs == nil {
				for label, count := range jm {
					m.Mismatches["JSDoc "+label] += count
				}
				continue
			}
			// A @typedef's comment is the JSDoc of its type alias too, but
			// its parent is the host it was parsed for, on both sides.
			if (p.Parent == host.P) != store.SameNode(ns[i].Parent(), host.N) {
				m.add(p, "JSDoc root's Parent differs in being the host")
			}
			if ns[i].File() != file {
				m.add(p, "JSDoc root's File is not the host's")
			}
			for _, pair := range jsdocPairs {
				m.pair(pair.P, pair.N)
			}
			compared += len(jsdocPairs)
		}
	}
	return m.Mismatches, compared
}
