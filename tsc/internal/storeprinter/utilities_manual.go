package storeprinter

import (
	"fmt"
	"slices"

	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// The P2 declarations of printer/utilities.go:
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4.

// tryGetEnd takes any: the Store's Node and List end in int32, a
// core.TextRange in int, so no one interface{ End() } takes them all. The
// cases of *ast.NodeList and *ast.ModifierList are both List.
//
// storetranslate:pointer-hash e146dcae737a7cc4 utilities.go:tryGetEnd
func tryGetEnd(node any) (int, bool) {
	// avoid using reflect (via core.IsNil) for common cases
	switch v := node.(type) {
	case (store.Node):
		if !v.IsNil() {
			return int(v.End()), true
		}
	case (store.List):
		if !v.IsNil() {
			return int(v.End()), true
		}
	case (*core.TextRange):
		if v != nil {
			return v.End(), true
		}
	case core.TextRange:
		return v.End(), true
	default:
		panic(fmt.Sprintf("unhandled type: %T", node))
	}
	return 0, false
}

// greatestEnd takes any, as tryGetEnd.
//
// storetranslate:pointer-hash cbeef555fca344d4 utilities.go:greatestEnd
func greatestEnd(end int, nodes ...any) int {
	for _, node := range slices.Backward(nodes) {
		if nodeEnd, ok := tryGetEnd(node); ok && end < nodeEnd {
			end = nodeEnd
		}
	}
	return end
}
