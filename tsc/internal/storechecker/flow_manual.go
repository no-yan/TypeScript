package storechecker

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tracing"
)

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

// getFlowTypeOfReferenceUnderCondition is getFlowTypeOfReferenceEx whose flow
// is the Pointer's synthetic Condition on expr (TrueCondition when assumeTrue)
// with antecedent: the walk starts at getTypeAtSyntheticCondition instead of
// getTypeAtFlowNode on that flow
// (store-checker-7d2b1-implementation-instructions-20261001.md 3.1). The flow
// is never nil, so the fallback to the reference's own flow is not copied.
//
// storetranslate:store-only
// storetranslate:pointer-hash e5bdb3241f089644 flow.go:Checker.getFlowTypeOfReferenceEx
func (c *Checker) getFlowTypeOfReferenceUnderCondition(reference store.Node, declaredType *Type, initialType *Type, flowContainer store.Node, antecedent store.Flow, expr store.Node, assumeTrue bool) *Type {
	if c.flowAnalysisDisabled {
		return c.errorType
	}
	f := c.getFlowState()
	f.reference = reference
	f.declaredType = declaredType
	f.initialType = core.Coalesce(initialType, declaredType)
	f.flowContainer = flowContainer
	f.sharedFlowStart = len(c.sharedFlows)
	c.flowInvocationCount++
	evolvedType := c.getTypeAtSyntheticCondition(f, antecedent, expr, assumeTrue).t
	c.sharedFlows = c.sharedFlows[:f.sharedFlowStart]
	c.putFlowState(f)
	// When the reference is 'x' in an 'x.length', 'x.push(value)', 'x.unshift(value)' or x[n] = value' operation,
	// we give type 'any[]' to 'x' instead of using the type determined by control flow analysis such that operations
	// on empty arrays are possible without implicit any errors and new element types can be inferred without
	// type mismatch errors.
	var resultType *Type
	if evolvedType.objectFlags&ObjectFlagsEvolvingArray != 0 && c.isEvolvingArrayOperationTarget(reference) {
		resultType = c.autoArrayType
	} else {
		resultType = c.finalizeEvolvingArrayType(evolvedType)
	}
	if resultType == c.unreachableNeverType || !reference.Parent().IsNil() && store.IsNonNullExpression(reference.Parent()) && resultType.flags&TypeFlagsNever == 0 && c.getTypeWithFacts(resultType, TypeFactsNEUndefinedOrNull).flags&TypeFlagsNever != 0 {
		return declaredType
	}
	return resultType
}

// getTypeAtSyntheticCondition is getTypeAtFlowNode on the Pointer's synthetic
// Condition flow: its depth guard, and the Condition case, which is
// getTypeAtFlowCondition with the flow's Flags, Node and Antecedent read from
// assumeTrue, expr and antecedent. The flow is not Shared, so the shared
// cache is neither read nor written. A nil antecedent is the Pointer's
// synthetic Start, which has no node: its type is the initial type
// (store-checker-7d2b1-implementation-instructions-20261001.md 3.1).
//
// storetranslate:store-only
// storetranslate:pointer-hash d7c60542a1edc4fe flow.go:Checker.getTypeAtFlowNode
// storetranslate:pointer-hash fbb9c4044926e489 flow.go:Checker.getTypeAtFlowCondition
func (c *Checker) getTypeAtSyntheticCondition(f *FlowState, antecedent store.Flow, expr store.Node, assumeTrue bool) FlowType {
	if f.depth == 2000 {
		// We have made 2000 recursive invocations. To avoid overflowing the call stack we report an error
		// and disable further control flow analysis in the containing function or module body.
		if tr := c.tracer; tr != nil {
			tr.Instant(tracing.PhaseCheckTypes, "getTypeAtFlowNode_DepthLimit", map[string]any{"depth": f.depth})
		}
		c.flowAnalysisDisabled = true
		c.reportFlowControlError(f.reference)
		return FlowType{t: c.errorType}
	}
	f.depth++
	// getTypeAtFlowCondition, called in place so that its early returns stay
	// returns.
	t := func() FlowType {
		var flowType FlowType
		if antecedent.IsNil() {
			flowType = FlowType{t: f.initialType}
		} else {
			flowType = c.getTypeAtFlowNode(f, antecedent)
		}
		if flowType.t.flags&TypeFlagsNever != 0 {
			return flowType
		}
		// If we have an antecedent type (meaning we're reachable in some way), we first
		// attempt to narrow the antecedent type. If that produces the never type, and if
		// the antecedent type is incomplete (i.e. a transient type in a loop), then we
		// take the type guard as an indication that control *could* reach here once we
		// have the complete type. We proceed by switching to the silent never type which
		// doesn't report errors when operators are applied to it. Note that this is the
		// *only* place a silent never type is ever generated.
		nonEvolvingType := c.finalizeEvolvingArrayType(flowType.t)
		narrowedType := c.narrowType(f, nonEvolvingType, expr, assumeTrue)
		if narrowedType == nonEvolvingType {
			return flowType
		}
		return c.newFlowType(narrowedType, flowType.incomplete)
	}()
	f.depth--
	return t
}
