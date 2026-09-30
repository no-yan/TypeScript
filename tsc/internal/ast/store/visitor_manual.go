package store

import (
	"slices"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
)

// The P2 declarations of the translation of ast/visitor.go (visitor.go):
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4. Each carries the hash of the Pointer declaration it replaces.

// NewNodeVisitor takes a Factory: a Store Factory needs a region, so there
// is no zero Factory to default to (5.1). Every caller in the reach passes
// one.
//
// storetranslate:pointer-hash 7e6243e7df389a70 visitor.go:NewNodeVisitor
func NewNodeVisitor(visit func(node Node) Node, factory *Factory, hooks NodeVisitorHooks) *NodeVisitor {
	if factory == nil {
		panic("store: NewNodeVisitor needs a Factory")
	}
	return &NodeVisitor{Visit: visit, Factory: factory, Hooks: hooks}
}

// VisitSliceList is VisitSlice on a list (3.4, List version): the Pointer body
// with the List rules (Len, At, and Nodes() where a list flows into a
// []Node, R4').
//
// storetranslate:pointer-hash a5a7f52fbced27b8 visitor.go:NodeVisitor.VisitSlice
func (v *NodeVisitor) VisitSliceList(nodes List) (result []Node, changed bool) {
	if nodes.IsNil() || v.Visit == nil {
		return nodes.Nodes(), false
	}

	for i := 0; i < nodes.Len(); i++ {
		node := nodes.At(i)
		if v.Visit == nil {
			break
		}

		visited := v.Visit(node)
		if visited.IsNil() || !SameNode(visited, node) {
			updated := slices.Clone(nodes.Nodes()[:i])

			for {
				// finish prior loop
				switch {
				case visited.IsNil(): // do nothing
				case visited.Kind() == ast.KindSyntaxList:
					updated = append(updated, visited.AsSyntaxList().Children().Nodes()...)
				default:
					updated = append(updated, visited)
				}

				i++

				// loop over remaining elements
				if i >= nodes.Len() {
					break
				}

				if v.Visit != nil {
					node = nodes.At(i)
					visited = v.Visit(node)
				} else {
					updated = append(updated, nodes.Nodes()[i:]...)
					break
				}
			}

			return updated, true
		}
	}

	return nodes.Nodes(), false
}
