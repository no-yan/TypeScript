# Binder instruction reduction

## Problem

The Store binder does the same symbol and control-flow work as microsoft/typescript `tsc/internal/binder/binder.go`, but each node costs more instructions. Recursion is `NodeRef`-first, then many sites rebuild `Handle` for leftover helpers, CFG re-enters through `bind(Handle)` and reloads parent kind, and ordinary children walk packed named slots then list slots instead of schema/syntactic order. Freeze, parameters-before-body, functions-first, a single Store tree, and `BindSourceFile` as the only public entry are fixed.

## Usage (caller's view)

Outside the binder nothing changes:

```go
binder.BindSourceFile(file)
```

Ordinary recursion has one child-walk call. CFG and functions-first stay explicit binder policy:

```go
func (b *Binder) bindChildrenRef(ref ast.NodeRef, kind ast.Kind) {
    // CFG-special kinds keep their semantic walkers.
    // SourceFile / Block / ModuleBlock keep functions-first.
    // Every other kind:
    b.forEachBindChildGenerated(ref, kind)
}
```

Generated cases interleave `ChildRef` and `ListSlotAt` in schema member order (function-likes: parameters before body). `Handle` is not walk currency; `HandleOf` remains at Symbol/Flow payloads and unported helpers.

## Shape

Binder-private generated visitor `forEachBindChildGenerated(ref, kind)`, emitted from the same schema member sequence as pointer `ForEachChild` and Store factories. Recursion currency stays `(NodeRef, Kind)` already in `bindKind`. `bindChildRef` / `bindListRef` are the leaves. Freeze stays in Store setters (`mustMutate`). No bind-session type. No fused generated `bindKind`.

Later stages: CFG child getters by NodeRef so Handle is only `payload()` at Symbol/Flow edges. Remaining Handle edges: `createFlowCondition`/`createFlowCall`/`createFlowMutation`, `isTopLevelLogicalExpression`/`IsAssignmentTarget`/`IsDottedName`/`isNarrowableOperand`, unreachable `IsPotentiallyExecutableNode`, and leftover `bindKind` helpers.

## Synthesis decision

Arena candidates: C1 generated syntactic visitor; C2 `bindNode` currency plus generated walk plus delete `bind(Handle)`; C3 fuse bindKind/container/walk into per-kind generated functions.

Base is C1. Cross-judge scored C1 highest on a landable first step and on keeping bind semantics out of codegen. Graft from C2 for a later stage: `bindNode`, `payload()`, CFG Ref-first. Reject C3: fused generated bindKind is unreviewable (container save/restore duplicated per kind).

## Tradeoffs accepted

- We accept a larger generated kind switch in exchange for dropping runtime slot-count loops and the generic vs function-like split.
- We accept leaving `mustMutate`, flow-ID, and remaining `HandleOf` sites untouched in this landing.
- We accept a `default` fallback to `bindChildrenOf` until every slotted kind is in the switch.

## Alternatives considered

- NodeRef-only currency in one PR: right end state, too large to land with the walker.
- Bind mutation session: Freeze belongs on Store writes, not a binder ticket; walk stays hybrid.
- Fused generated bindKind: hides semantics in codegen; wrong interface depth.

## Open questions and risks

- Does schema order match microsoft `ForEachChild` for every non-CFG kind, including handwritten visitors?
- Is the generated switch i-cache positive on the binder corpus?

## Next implementation step

Porting leftover `bindKind` `HandleOf` (identifiers, binary, unreachable) added a parallel Ref helper surface and did not move monaco Bind time; A/B on `BenchmarkBind` was checker.ts −4% only. That step was reverted. Remaining Handle edges are flow payloads and unported predicates, not a new bindKind visitor.
