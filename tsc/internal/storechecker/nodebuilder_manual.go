package storechecker

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/storeprinter"
)

// The P2 declarations of checker/nodebuilder.go:
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4.

// getNodeBuilderEx makes the node builder's EmitContext in the checker's
// region (5.1).
//
// storetranslate:pointer-hash bfbf5509f27815b4 nodebuilder.go:Checker.getNodeBuilderEx
func (c *Checker) getNodeBuilderEx(idToSymbol map[store.NodeKey]*store.Symbol) *NodeBuilder {
	b := NewNodeBuilderEx(c, storeprinter.NewEmitContextIn(c.factory.Region()), idToSymbol)
	return b
}
