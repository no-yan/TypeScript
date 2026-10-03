package storeprinter

import "github.com/microsoft/TypeScript/tsc/internal/ast/store"

// The EmitContext and NodeFactory of a region: the Store's Factory needs one
// (internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4 and 5.1). The checker's node builder makes its EmitContext in the
// checker's region; the tests make one per program.

// The node capacity of a Store of the EmitContext's Factory: the first after
// a release holds synthStoreMinCapacity, and each after a full one twice as
// many, up to synthStoreCapacity (store-checker-7d2-design-remainder-
// 20260929.md 2.5, store-checker-7d2d-implementation-instructions-20261003.md
// 1). The node builder releases after each typeToString, which makes 8 nodes
// at the median in the check of the compiler project.
const (
	synthStoreMinCapacity = 16
	synthStoreCapacity    = 4096
)

// NewEmitContextIn is printer.NewEmitContext with its Factory in region.
func NewEmitContextIn(region *store.NodeRegion) *EmitContext {
	c := &EmitContext{}
	c.Factory = NewNodeFactoryIn(c, region)
	return c
}

// NewNodeFactoryIn is printer.NewNodeFactory with its Factory in region.
func NewNodeFactoryIn(context *EmitContext, region *store.NodeRegion) *NodeFactory {
	return &NodeFactory{
		Factory: store.NewGrowingFactory(region, synthStoreMinCapacity, synthStoreCapacity, store.FactoryHooks{
			OnCreate: context.onCreate,
			OnUpdate: context.onUpdate,
			OnClone:  context.onClone,
		}),
		emitContext: context,
	}
}
