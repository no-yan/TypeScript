package storeprinter

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
)

// The P2 declarations of printer/factory.go and printer/emitcontext.go:
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4 and 5.1. A Store Factory needs a region, so the EmitContext and the
// NodeFactory are made in one (NewEmitContextIn, NewNodeFactoryIn, region.go);
// the versions without one panic.

// NewNodeFactory has no region to make its Factory in.
//
// storetranslate:pointer-hash 70716b60a9b2451b factory.go:NewNodeFactory
func NewNodeFactory(context *EmitContext) *NodeFactory {
	panic("storeprinter: NewNodeFactory has no region; use NewNodeFactoryIn")
}

// NewEmitContext has no region to make its Factory in.
//
// storetranslate:pointer-hash ffca9a64d7a4b9fe emitcontext.go:NewEmitContext
func NewEmitContext() *EmitContext {
	panic("storeprinter: NewEmitContext has no region; use NewEmitContextIn")
}

// onCreate writes the flag through the context's Factory (R8: the context
// holds a *NodeFactory, which embeds the Factory, not a NodeFactory field).
//
// storetranslate:pointer-hash 2e9d01c5145bbeea emitcontext.go:EmitContext.onCreate
func (c *EmitContext) onCreate(node store.Node) {
	c.Factory.AddFlags(node, ast.NodeFlagsSynthesized)
}
