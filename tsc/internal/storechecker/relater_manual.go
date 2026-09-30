package storechecker

import "github.com/microsoft/TypeScript/tsc/internal/ast/store"

// The P2 declarations of checker/relater.go:
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4.

// asRecursionId takes the Store's Node, *Symbol or *Type. A RecursionId is
// compared with ==, and a Node is not comparable (Z1): a node's id is its
// NodeKey (R6).
//
// storetranslate:pointer-hash 675d4c85f6cd3ea6 relater.go:asRecursionId
func asRecursionId[T store.Node | *store.Symbol | *Type](value T) RecursionId {
	if node, ok := any(value).(store.Node); ok {
		return RecursionId{value: node.Key()}
	}
	return RecursionId{value: value}
}
