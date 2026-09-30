package storechecker

import "github.com/microsoft/TypeScript/tsc/internal/ast/store"

// The P2 declarations of checker/flow.go:
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4.

// getFlowNodeOfNode tests FlowNodeData() != nil with HasFlowNodeData (4.1).
//
// storetranslate:pointer-hash 77a2443beac2039f flow.go:getFlowNodeOfNode
func getFlowNodeOfNode(node store.Node) store.Flow {
	if node.HasFlowNodeData() {
		return node.FlowNode()
	}
	return store.Flow{}
}

// checkGrammarForUseStrictSimpleParameterList passes the NodeRefs of the
// statements to storebinder.FindUseStrictPrologue, which takes them (R15). The
// body is a node of a source Store, whose list Refs gives.
