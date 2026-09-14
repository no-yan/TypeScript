# Remove the pointer `*ast.Node` tree

Status: plan, not started. Written 2026-09-14 on `cursor/ast-store-tests`.

Store is the production tree. Nothing on the compile path allocates a pointer
tree, yet `ast_generated.go` (10,243 lines) and roughly 2,300 lines of
`ast.go` still define `*Node`, `NodeFactory`, `NodeList`, `nodeData`, and the
per-kind `computeSubtreeFacts` methods. This document records why they cannot
be deleted in one commit, where every remaining reference is, and the order
in which to remove them so that each step builds and tests green on its own.

## Why a single delete does not compile

Three production couplings keep the pointer world alive. Everything else in
the `ast` package is reachable only through these three.

1. **Flow synthetic payloads.** `FlowNode.Data` is `*Node`
   (`flow.go:31`). `FlowSwitchClauseData` and `FlowReduceLabelData` embed
   `NodeBase` and are allocated through `newNode` (`flow.go:46-79`). The
   binder allocates them (`binder/binder.go:783,799,834`) and the checker
   reads them through `flow.Data.AsFlowSwitchClauseData()` and
   `AsFlowReduceLabelData()` (`checker/flow.go:384,1149,1336,1357,2475,2482,2540`).
   STORE.md rule 6 calls this "the one remaining pointer `*Node` allocation
   on the compile path".
2. **`SourceFile` embeds the pointer bases.** The metadata struct embeds
   `NodeBase`, `DeclarationBase`, `LocalsContainerBase`, and `CompositeBase`
   (`ast.go:2560-2563`). `file.Symbol`, `file.Flags`, and `file.Locals`,
   read across binder, checker, parser, and ls, are fields of the embedded
   pointer `Node`. Because `SourceFile` satisfies `nodeData`, it keeps the
   pointer `ForEachChild`, `VisitEachChild`, `Clone`, `computeSubtreeFacts`,
   `Statements *NodeList`, and `EndOfFileToken *Node`.
3. **`ast.GetNodeId(containingFile.AsNode())`**
   (`checker/symbolaccessibility.go:126`) keys a per-file cache on the pointer
   node id of the file. `Handle.NodeId()` already exists
   (`store_query_manual.go:336`).

`deadcode ./cmd/tsc` reports zero unreachable functions in `ast_generated.go`.
That is an artifact of coupling 2: `&SourceFile{}` instantiates a `nodeData`,
so RTA treats every `VisitEachChild` and factory method as reachable through
interface dispatch. Once `SourceFile` stops embedding `NodeBase`, the whole
generated file becomes unreachable. Do not read the deadcode output as
evidence that the generated code is used.

## Inventory of remaining references

### Production code outside `ast`

| File | What |
| --- | --- |
| `internal/binder/binder.go:783` | `newFlowData(flags, data *ast.Node, antecedent)` |
| `internal/checker/flow.go` (7 sites) | `flow.Data.AsFlowSwitchClauseData()` / `AsFlowReduceLabelData()` |
| `internal/checker/symbolaccessibility.go:126` | `ast.GetNodeId(containingFile.AsNode())` |
| `internal/parser/parser.go:693` | `result.Flags \|= p.sourceFlags` (embedded `Node.Flags`) |
| `internal/parser/references.go:19`, `internal/checker/checker.go:9254` | read `file.Flags` |
| binder, checker, ls (about 15 sites) | read or write `file.Symbol` |
| `internal/printer/utilities.go:524` | dead `*ast.ModifierList` case in `tryGetEnd` |
| `internal/modulespecifiers/preferences.go:39` | comment only |

### Inside `ast`, pointer-only

- `ast_generated.go`: 226 structs, 359 `NodeFactory` methods, 602 `Node`
  methods. All of it.
- `ast.go:190-2479`: `Node` and about 150 methods, `NodeList`,
  `ModifierList`, `NodeFactory`, `NodeFactoryHooks`, `NewNodeFactory`,
  `updateNode`, `cloneNode`, `nodeData`, `NodeDefault`, every `*Base` type,
  and the per-kind `computeSubtreeFacts` / `propagateSubtreeFacts` methods.
  Also `Visitor` and `visit*` at `ast.go:29-59`.
- `ast.go` `SourceFile` section: `Statements *NodeList`,
  `EndOfFileToken *Node`, `jsdocCache map[*Node][]*Node`,
  `ReparsedClones []*Node` (never written), `declarationMap
  map[string][]*Node`, `GetDeclarationMap` (no callers), `SetJSDocCache` (no
  callers), `resolveJSDoc(*Node)`, `Node.JSDoc`, `Node.EagerJSDoc`, pointer
  `NewSourceFile`, `UpdateSourceFile`, `ForEachChild`, `VisitEachChild`,
  `Clone`, `computeSubtreeFacts`.
- `visitor.go`: all 294 lines (`NodeVisitor`).
- `deepclone.go:72-90`: `NodeFactory.DeepCloneNode`, `DeepCloneReparse`,
  `DeepCloneReparseModifiers`. The Store versions live in
  `store_factory.go:438` and `parser/reparser.go`.
- `subtreefacts.go:89-131`: pointer `propagate*` helpers. Store versions are
  in `store_subtree.go`.
- `utilities.go`: `GetNodeId`, `newParentInChildrenSetter`,
  `SetParentInChildren`, `ReplaceModifiers`, `CompareNodePositions`,
  `GetReparsedNodeForNode`, `findCloneInNode`, `sourceFileOfPointer`,
  `pointerDeclarationName`, `pointerDeclarationNameText`,
  `pointerLiteralComputedPropertyDeclarationName`, `literalIsName`,
  `isArgumentOfElementAccessExpression`.
- `store_flatten.go`: `FlattenNode`, no callers.
- `flow.go:46-79`: the two payload structs and their constructors.

### Tests and helpers

- `internal/testutil/parsetestutil/parsetestutil.go:48-88`:
  `newSyntheticRecursiveVisitor`, `MarkSyntheticRecursive`. No callers.
- `internal/ast/store_bench_test.go:59`: `walkAst(*ast.Node, ...)`. No callers.
- `internal/parser/parser_test.go:371-376`: iterates `file.ReparsedClones`,
  which is always empty.

### Generator

`tools/scripts/tsc/generate-go-ast.ts` writes both worlds. The pointer
output is `generate()` (line 1843 onward) and its helpers:
`generateNodeFactoryStruct`, `generateHeader`, `generateStructDef`,
`generateBaseStructDefs`, `generateSubtreeFacts`, `generateAsCast`,
`generateNewFactory`, `generateUpdateFactory`, `generateForEachChild`,
`generateForEachChildDispatch`, `generateVisitEachChild`, `generateClone`,
`generateIsFunction`, `generateKindAliasGuards`, `generateNameAccessor`,
plus the layout checks `verifyNoDuplicateBases` and
`verifyNodeBaseAtOffsetZero`. `kind_generated.go` and every
`store_*_generated.go` come from separate functions in the same script and
stay.

## Precondition: `internal/api/encoder` does not build

`encoder_generated.go` and `decoder_generated.go` were never regenerated for
Store (`node.Kind()` called as a method, `[]ast.Handle` passed where
`ast.ListRef` is expected, `nil` passed as `ast.Handle`). `internal/api` and
therefore `internal/lsp` fail to compile today; `cmd/tsc` builds because it
does not import them. `go build ./...` cannot be the gate until this is
resolved. Decide one of:

- port `tools/scripts/tsc/generate-encoder.ts` to the Store API, regenerate,
  and fix `internal/api` call sites, or
- exclude `internal/api` and `internal/lsp` from the gate for the duration of
  this program and note it in STORE.md.

Every step below uses this gate:

```
go build ./cmd/... && go vet ./internal/ast/... ./internal/binder/... ./internal/checker/... ./internal/parser/... ./internal/printer/...
go test ./internal/ast/... ./internal/binder/... ./internal/checker/... ./internal/parser/... ./internal/printer/... ./internal/transformers/...
go test ./internal/testrunner -run TestLocal -parallel 1   # see testlocal-store-race note; compare against base
```

## Steps

Each step is one PR. Each leaves the tree building and green under the gate
above. Steps 1 and 2 are the substantive changes; 3 and 4 are mechanical
deletion.

### Step 1. Flow payloads stop being `*Node`

Replace `FlowNode.Data *Node` with `Data *FlowData` where

```go
type FlowData struct {
	SwitchStatement Handle
	ClauseStart     int32
	ClauseEnd       int32
	Target          *FlowLabel
	Antecedents     *FlowList
}
```

One struct carries both payload shapes; only the fields for the node's
`Flags` are meaningful. `FlowNode` stays 48 bytes with one pointer word for
`Data`, so the arena layout and `flows` column are untouched. Keep
`IsEmpty()` on `FlowData`.

Changes:

- `flow.go`: delete `FlowSwitchClauseData`, `FlowReduceLabelData`, and the
  two `New*` constructors that call `newNode`. Add `FlowData` and
  `NewFlowSwitchClauseData` / `NewFlowReduceLabelData` returning `*FlowData`.
- `ast.go:1183-1190`: delete `AsFlowSwitchClauseData` /
  `AsFlowReduceLabelData` on `*Node`.
- `binder/binder.go:783`: `newFlowData(flags, data *ast.FlowData, ...)`.
- `checker/flow.go` 7 sites: drop the `As*` call and use `flow.Data`
  directly. Where the code stored the `*FlowReduceLabelData` in
  `f.reduceLabels`, store `*ast.FlowData`.

This also aligns with `flow-packed-plan.md`, which assumes a value-typed
payload; do not widen `FlowNode` to hold an interface.

Verify: gate plus `go test ./internal/checker -run 'Flow|Narrow'` and the
`TestTsgoStoreE2E` smoke. Add a size assertion to `flow_arena_test.go`
(`unsafe.Sizeof(ast.FlowNode{}) == 48`); none exists today, and the point of
the union struct is that the size does not move.

### Step 2. `SourceFile` becomes a plain struct

Remove the four embedded bases and add explicit fields that the rest of the
compiler already addresses by name:

```go
type SourceFile struct {
	Symbol *Symbol
	Locals SymbolTable
	Flags  NodeFlags
	Loc    core.TextRange
	// existing metadata fields ...
}
```

`file.Symbol`, `file.Flags`, and `file.Locals` call sites compile unchanged.
`Pos()` and `End()` already prefer `ParseRoot()` and fall back to `Loc`.

Delete from the `SourceFile` section of `ast.go`: `Statements`,
`EndOfFileToken`, `jsdocCache`, `ReparsedClones`, `declarationMap`,
`declarationMapMu`, `GetDeclarationMap`, `computeDeclarationMap`,
`SetJSDocCache`, `resolveJSDoc`, `ForEachChild`, `VisitEachChild`, `Clone`,
`computeSubtreeFacts`, and the `NodeFactory` versions of `NewSourceFile` and
`UpdateSourceFile`. `CloneWrapper` and `copyFrom` keep copying `Symbol`,
`Locals`, and `Flags` as they do now. `NodeCount` / `TextCount` are set by
the parser and stay.

Other edits in this step:

- `checker/symbolaccessibility.go:126`: `id := containingFile.ParseRoot().NodeId()`.
  The cache key stays `ast.NodeId`; the value is now the root's `GlobalRef`
  and is unique per file.
- `ast.go:1650-1685`: delete `Node.JSDoc` and `Node.EagerJSDoc`. The Handle
  versions (`Handle.JSDoc`, `Handle.JSDocIn`, `Handle.EagerJSDoc`) are the
  real ones.
- `utilities.go`: delete `sourceFileOfPointer`, `GetReparsedNodeForNode`,
  `findCloneInNode`, `pointerDeclarationName*`, `literalIsName`,
  `isArgumentOfElementAccessExpression`, `CompareNodePositions`.
- `parser/parser_test.go:371-376`: delete the `ReparsedClones` loop. Keep
  the `Imports()` parent-chain check below it.

After this step `SourceFile` no longer implements `nodeData`, and
`deadcode ./cmd/tsc` should list nearly all of `ast_generated.go`. Record the
count in the PR description; it is the evidence that step 3 is safe.

Verify: gate. Also `go test ./internal/ls/... ./internal/project/...` since
`file.Symbol` reads live there.

### Step 3. Generator stops emitting the pointer tree

In `generate-go-ast.ts`:

- Delete `generate()` and the pointer-only helpers listed in the inventory.
- Delete `verifyNoDuplicateBases` and `verifyNodeBaseAtOffsetZero`.
- Remove the `ast_generated.go` write at line 2492. Keep the
  `kind_generated.go` write and every `store_*` write.
- `pointerGoRef` (line 485) is used only by the pointer emitters and goes
  with them. Confirm any other shared helper with the TypeScript compiler,
  not by eye.

Run the generator, delete `internal/ast/ast_generated.go`, and fix whatever
the compiler now reports in `ast.go`. Expect the base structs
(`DeclarationBase`, `LocalsContainerBase`, and friends) to be the first
errors; they are deleted in step 4 but can be stubbed here if it keeps the
PR smaller.

Verify: gate, plus `npx hereby generate:ast` runs clean and `git status`
shows only the intended generated files changed. That task runs
`tools/scripts/tsc/generate.ts`, which calls `generate-encoder.ts` before
`generate-go-ast.ts`, so it also rewrites `internal/api/encoder/*_generated.go`.
If the precondition was resolved by excluding `internal/api` rather than
porting the encoder generator, expect those files to change and revert them.

### Step 4. Delete the pointer body of `ast`

Delete:

- `ast.go:29-59` (`Visitor`, `visit`, `visitNodes`, `visitNodeList`,
  `visitModifiers`) and `ast.go:60-125` (`NodeFactoryHooks`,
  `NodeFactoryCoercible`, `NewNodeFactory`, `newNode`, `updateNode`,
  `cloneNode`, `NodeFactory` methods).
- `ast.go:128-190` (`NodeList`, `ModifierList` and their methods).
- `ast.go:190-2479` (`Node`, `MutableNode`, `nodeData`, `NodeDefault`,
  every `*Base`, every pointer `computeSubtreeFacts`). Before deleting the
  alias block at `ast.go:1267` (`NamedMember = Node` and the rest), grep each
  alias name outside `ast`; any that is still referenced becomes
  `= Handle` or is removed with its callers.
- `visitor.go`, `store_flatten.go`.
- `deepclone.go:72-90` and `subtreefacts.go:89-131`.
- `utilities.go`: `GetNodeId`, `newParentInChildrenSetter`,
  `SetParentInChildren`, `setParentInChildrenPool`, `ReplaceModifiers`.
- `printer/utilities.go:524` `*ast.ModifierList` case.
- `parsetestutil.go:48-88` and `store_bench_test.go:59-68`.
- The `modulespecifiers/preferences.go:39` comment can go or stay; it is
  not code.

Verify: gate, then `deadcode ./cmd/tsc | grep internal/ast/` should list
only the pre-existing unreachable helpers (`IsWriteAccessForReference`,
`GetDeclarationFromName`, `NewSourceFileDataKey`, the `StoreSet` helpers,
and the `utilities.go` predicates). Anything new in that list is a leftover
from this program.

### Step 5. Docs

- STORE.md "Status": replace the paragraph that begins "Inside `ast`, the
  pointer `Node` type ..." with the SHA at which it was removed.
- STORE.md rule 6: drop the sentence about `KindUnknown` payload nodes; the
  payload is `*FlowData`.
- STORE.md rule 8: `FlowNode{Flags, Node Handle, Data *FlowData, ...}`.
- `store-upstream-plan.md` PR-9 skip record: note that the in-package
  deletion happened here.

## Out of scope

- `flow-packed-plan.md` (value-typed `FlowNode`, 12-byte `flowRec`). Step 1
  is compatible with it but does not implement it.
- Regenerating `internal/api/encoder` for Store. That is the precondition
  above, not part of this program.
- Any change to Store layout, `Handle`, or the generated Store accessors.
