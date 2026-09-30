package storeprinter

import "github.com/microsoft/TypeScript/tsc/internal/ast/store"

// The EmitContext and NodeFactory of a region: the Store's Factory needs one
// (internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4 and 5.1). The checker's node builder makes its EmitContext in the
// checker's region; the tests make one per program.

// synthStoreCapacity is the node capacity of a Store of the EmitContext's
// Factory (store-checker-7d2-design-remainder-20260929.md 2.5).
const synthStoreCapacity = 4096

// NewEmitContextIn is printer.NewEmitContext with its Factory in region.
func NewEmitContextIn(region *store.NodeRegion) *EmitContext {
	c := &EmitContext{}
	c.Factory = NewNodeFactoryIn(c, region)
	return c
}

// NewNodeFactoryIn is printer.NewNodeFactory with its Factory in region.
func NewNodeFactoryIn(context *EmitContext, region *store.NodeRegion) *NodeFactory {
	return &NodeFactory{
		Factory: store.NewFactory(region, synthStoreCapacity, store.FactoryHooks{
			OnCreate: context.onCreate,
			OnUpdate: context.onUpdate,
			OnClone:  context.onClone,
		}),
		emitContext: context,
	}
}
