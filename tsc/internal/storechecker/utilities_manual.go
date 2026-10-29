package storechecker

import "github.com/microsoft/TypeScript/tsc/internal/ast/store"

// The P2 declarations of checker/utilities.go:
// internal/ast/docs/perf-checkall-widening-report-20261022.md 6.

// compareNodes leaves out the Pointer's fast path for nodes of one parent:
// File is already cheap in the Store.
//
// storetranslate:pointer-hash 8c0a55b5ce9e734c utilities.go:Checker.compareNodes
func (c *Checker) compareNodes(n1, n2 store.Node) int {
	if store.SameNode(n1, n2) {
		return 0
	}
	if n1.IsNil() {
		return 1
	}
	if n2.IsNil() {
		return -1
	}
	s1 := n1.File()
	s2 := n2.File()
	if s1 != s2 {
		f1 := c.fileIndexMap[s1]
		f2 := c.fileIndexMap[s2]
		// Order by index of file in the containing program
		return f1 - f2
	}
	// In the same file, order by source position
	return int(n1.Pos()) - int(n2.Pos())
}
