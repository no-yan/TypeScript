# About the Store-backed AST

This note is the shared map for the Store-backed syntax tree in `tsc/internal/ast`. It fixes vocabulary, records the rules the compile path depends on, and points at the evidence. It is **not** an API reference; the generated `Handle` accessors in `store_handles_generated.go` and `store_polymorphic_generated.go` are that.

Motivation and early sketch: [TypeScript#63807](https://github.com/microsoft/TypeScript/issues/63807).

Repo docs stay English. Operator-facing chat is Japanese.

## Status (2026-09-07)

Branch `lock-profile`, based on `3e0eb48bd4` after merging `cursor/ast-store-tests`. Store is the **production tree** on this branch. The concurrency redesign is described in [store-concurrency-implementation.md](docs/store-concurrency-implementation.md).

- `parser.ParseSourceFile` allocates every node into one `Store` per file through `ast.Factory` and returns `*SourceFile` metadata whose tree is `ParseRoot()` / `ParseStore()`. There is no pointer parser, no `MaterializeSourceFile`, no `ExpandStore`, no dual-write.
- Binder walks `NodeRef` (`bindRef`) and writes Symbol, Flow, Locals and flags into Store columns and side maps.
- Checker, printer, transformers, LS, format and astnav consume `Handle`. Production `*ast.Node` outside the `ast` package is down to `binder.newFlowData` (Flow synthetic payload), one comment in `modulespecifiers/preferences.go`, and `testutil/parsetestutil`.
- Inside `ast`, the pointer `Node` type, `NodeFactory`, and `ast_generated.go` still exist. They back `FlowNode.Data`, `SourceFile.ReparsedClones`, the pointer `jsdocCache`, and `store_flatten.go`. Nothing on the compile path allocates a pointer tree. Deleting them is open work, not a blocker.

The upstream program (`docs/store-upstream-plan.md`) landed PR-1 through PR-8 on this fork as GitHub `#1`–`#8`. PR-9 (delete leftover pointer AST in LS/format) was a skip record because the grep was already empty. PR-10 (the microsoft/TypeScript request) has not been opened. `docs/store-maintainer-proof-plan.md` is the follow-on program that closes the two blockers below before that request.

Historical failures at `8067b4b419` (not a statement of current verification):

| Test | Failure | Owner |
| --- | --- | --- |
| `go test ./internal/testrunner -run TestLocal/alias` | `type` baselines panic `ast: write to frozen Store` in `Store.AllocSlots` from `tsbaseline` type/symbol generation. The `NodeBuilder` allocates on a frozen parse Store instead of a writer-leased or synth Store. | proof-plan PR-4 |
| `go test ./internal/transformers/tstransforms -run TestImportElision` | `ExportAssignment#2` and `ExportDeclaration#8` fail. Present on the 24-byte header ancestor too (`tsc/.audit/attempts/018-node-header-24b.md`). | unassigned |

The e2e gate that does pass is the TypeScript v6.0.3 CI smoke project `-p src/compiler --noEmit` exit 0 (`TestTsgoStoreE2E`).

## The problem

Go's garbage collector is non-generational. `tsgo` keeps most parse and check data alive until the compile finishes, so each GC cycle scans a large live heap. On a VS Code-sized check, `GOGC=off` was about 1.24× faster in the issue write-up; on the CI smoke project it is 1.55× (see [GOGC baseline](#gogc-baseline)). That gap is an upper bound on GC cost, and `GOMEMLIMIT` alone does not recover it.

Profiles pointed at object scan time, with the parser AST a large share of in-use space. The pointer tree (`*Node`, `Parent *Node`, `[]*Node`, `nodeData` interfaces) gives the collector many pointers to chase. Store targets scan cost and live size for the long-lived tree by keeping node rows pointer-free.

## Layout

A `Store` owns one file's syntax tree. Nodes are dense `NodeRef` indices (`uint32`); `0` means absent. Lists use owner-qualified `ListRef` (`uint64`: StoreID and a 32-bit list index); `0` means no list. Same-store list slots remain 32-bit indexes in `children`; sparse `foreignLists` slots preserve cross-store list identity.

`nodeHeader` is one pointer-free **24-byte** row (`TestNodeHeaderIs24Bytes`):

| Field | Type | Note |
| --- | --- | --- |
| `kind` | `Kind` (int16) | |
| `childLen`, `listLen` | `uint8` | schema max is 6 named children and 4 list slots; `AllocSlots` panics above 255 |
| `flags` | `NodeFlags` | mutable by binder |
| `pos`, `end` | `int32` | |
| `parent` | `NodeRef` | same-store parent; `0` when absent or cross-store |
| `childStart` | `uint32` | base index into `children`. Named child slots occupy `children[childStart : +childLen]`; list slots follow at `children[childStart+childLen : +listLen]` and hold `ListRef`s. Slotless text kinds (identifiers, literals, template parts, JsxText) reuse this field as the intern id. |

Two and a half rows fit a 64-byte cache line. Shrinking the header from 44 to 24 bytes cut Bind 16% on the VS Code `src/tsconfig.json` workload (attempt 018).

`Handle` is a 16-byte stack value: `*Store`, `NodeRef`, and a cached public `Kind` so callers can `switch h.Kind` without a Store load. Heap-resident structures should hold `NodeRef` (same store) or `GlobalRef` (cross store) and rebuild the Handle via `Store.At` / `ast.NodeOf`; a stored `Handle` puts a pointer word in every element and makes the container scannable. Checker still keeps 22 `map[ast.Handle]` fields and 12 `LinkStore[ast.Handle, …]` (see [Open work](#open-work)).

`NodeRef`, `GlobalRef`, and `NodeId` (`uint64`, `Handle.NodeId()`) are different types. Do not cast between them.

Columns and side tables on `Store`:

| Data | Shape | Why |
| --- | --- | --- |
| `nodes`, `lists`, `children`, `internBuf`, `internOff` | dense, noscan | the layout bet. Bind-hot sketch to pack Kind beside each child ref: [bind-store-layout-design.md](docs/bind-store-layout-design.md) |
| `internIdx` | `map[string]uint32` | construction-time dedup; dropped by `Seal` / `Freeze`. `Intern` after `Seal` appends without dedup during build; `Freeze` prohibits further interning |
| `symbolIdx []uint32` + `symbolRefs []*Symbol` | dense index column + 1-based fill-only pointer slice | about one node in eight has a Symbol; the GC scans 1/8 the pointers of a dense `[]*Symbol` |
| `flows []uint32` | dense noscan column of ids into the Store's chunked `FlowNode` arena (`NewFlow`), sized at `PrepareBindTables` | fill is about 50%; 4 B/node and no GC scan. FlowNodes not from this arena (copied from another Store, checker literals) get a slot in `foreignFlows`; see `docs/flow-index-column-bench.md` |
| `localSymbols`, `endFlows`, `returnFlows`, `locals`, `nextContainer` | `map[NodeRef]` | sparse |
| `tokenFlags` | `map[NodeRef]TokenFlags` | set on under 4% of nodes |
| `scalarValues`, `stringValues`, `objectValues` | `map[uint64]…` keyed by packed NodeRef/slot | generated kind-specific value slots: integer-like scalars, intern ids, and the few pointer/slice values |
| `externalChild`, `externalList`, `externalParent` | `map[…]Handle` | direct retained cross-store edges; no registry lookup or foreign mutation |
| `foreignLists`, `listOwners` | sparse list-slot identities and owner pointers | unchanged foreign lists are shared; owning Stores stay reachable |
| `subtreeFacts []uint32` | allocated at `Freeze`, atomics | checker/emit `SubtreeFacts` cache without writes to the header |
| `sourceFile *SourceFile` | one pointer | metadata owner; `SourceFile` fields stay outside Store |

## Lifecycle and mutation rules

A Store has two phases: **build** and **check**. Freeze is irreversible.

1. **Build.** Parser allocates and links (`AllocSlots`, `SetChild`, `SetList`, `Finish`). Same-store parents are written at attach time. `Factory.Seal` at the end of `ParseSourceFile` drops only `internIdx`; it does not freeze. Binder then mutates flags in place and fills the Symbol/Flow columns and side maps. Deferred TS JSDoc is never appended here; the parse Store's node count is final at the end of `ParseSourceFile`.
2. **Freeze** (`Program.BindSourceFiles`, after bind, one goroutine per file). `Freeze` sets phase check, records `frozenAt`, drops `internIdx`, allocates `subtreeFacts`. It is `sync.Once`. From here `mustMutate` panics on `Alloc`, `SetChild`, `SetFlags`, side-map writes, and `Intern`. Parallel checkers read without locks.
3. **Private emit Store.** `EmitContext.BeginFile` associates an output view with the context's own Store. Factories allocate only there, while unchanged parse nodes and lists remain shared. `Store.EnterEmit` and `LeaveEmit` have been removed. `Program.Emit` follows `SingleThreaded`; otherwise files can emit concurrently. Metadata views are resolved through `EmitContext.SourceFileOf`, without rebinding the parse Store's owner.

`Checkpoint` / `Restore` truncate node, list, child, and side columns to a watermark for speculative parsing. They exist and are tested; the production parser does not currently call them (a failed speculative parse rewinds the scanner and leaves dead nodes).

`SourceFile` can outlive a `Program`. Its frozen Store stays frozen across rebuilds; each output context owns its generated nodes.

## Identity across stores

`NodeRef` is store-local. Identity keys use `GlobalRef`, a `uint64` packing `StoreID` (high 32 bits) and `NodeRef` (low 32 bits); `0` is absent. Hot references (`Symbol.Declarations`, `Symbol.ValueDeclaration`, and foreign AST edges) hold `Handle` directly. Identity keys and navigation references have different cost and lifetime requirements.

`StoreSet` is the identity domain. There is one process-wide set (`identitySet()` in `store_identity.go`) behind `ast.RegisterFile`, `ast.RegisterStore`, `ast.UnregisterStore`, and `ast.NodeOf`. Registration points:

- `binder.bindSourceFile` calls `RegisterFile(file)` before binding, so every parse Store has a `StoreID` once bound and `StoreSet.File(id)` resolves back to metadata.
- `checker.NewChecker` registers its private `synth` Store; `Checker.Close` unregisters it (`Program.Close` closes the pool). `StoreID`s are never reused; `NodeOf` on an unregistered id returns `Handle{}`.
- `printer.NewEmitContext` registers the pooled emit factory's own Store; `BeginFile` keeps that private allocation Store; pooled `Reset` unregisters the old Store and installs a fresh one.

Properties that drove the choice, unchanged from β: pointer-free map keys (a `map[GlobalRef]V` with pointer-free `V` is noscan), deterministic ids for a fixed registration order (today's `NodeId` is an atomic counter that depends on binder scheduling), no per-node cost. `Handle.Global()` panics on an unregistered Store because a silent `0` would corrupt any map keyed by it.

Measured on this machine (`handle_key_bench_test.go`, 64K entries, one run): a `map[GlobalRef]` lookup sweep is about 1.8× faster than the same sweep on `map[Handle]`, and only the `Handle` map is scannable.

`StoreSet.Store` reads atomic slots through an immutable, geometrically grown directory. Only registration and removal take the writer mutex. The Store ID is published after the slot and domain; IDs are never reused. Parse Store eviction for LS edits remains a separate lifecycle concern.

## Ownership: parse, synth, emit

| Store | Owner | Writes | Lifetime |
| --- | --- | --- | --- |
| Parse Store, one per file | `SourceFile` | parser and binder before Freeze | as long as consumers retain the SourceFile or Store |
| `Checker.jsdoc` (`JSDocCache` on `Checker.synth`) | one `Checker` | deferred TS JSDoc parsed on first `JSDocIn` | with the checker |
| Shared JSDoc side Store | `SourceFile` | deferred TS JSDoc for consumers without a `JSDocCache` (LS, API, astnav); whole file parsed once, then frozen | with the SourceFile |
| `Checker.synth` | one `Checker` | checker synthetics via its private factory | registered until `Checker.Close` |
| Emit factory Store | one `EmitContext` | transforms and NodeBuilder, including diagnostics and type baselines | until the context resets; direct Handles can retain the old Store |

Cross-store parent, child, and list-element edges retain the target Handle. The Store containing the edge is the only writer; attaching a foreign child never changes the child's parent. Foreign list slots retain the list identity and owner. `NodeSeq` resolves and retains the actual list owner when constructed. Same-store node and list indexes remain packed and pointer-free.

`CopySubtree` is an explicit structural copy, not an implicit attachment operation. Foreign references remain shared. `DeepCloneNode` also calls the clone hook for nodes copied into another Store, including descendants, so emit can resolve original parse declarations and preserve import substitutions.

## Lists and NodeSeq

Lists are `listHeader{pos, end, start, len}` rows; elements live in `children[start : start+len]`. A list keeps its own loc so `HasTrailingComma` (`last.End() < list.End()`) survives. Named list slots (`Statements`, `Parameters`, `Members`, `Modifiers`, …) hold a `ListRef`, so JSDoc reparse can replace a list by writing a new `ListRef` into the slot (`SetList`), abandoning the old rows.

`NodeSeq` (`node_sequence.go`) is the allocation-free way to iterate a list, a `[]Handle`, including a Symbol declaration set:

```go
for i, h := range file.ParseStore().ListSlice(list).All() { … }
for _, d := range ast.DeclarationNodes(symbol).All() { … }
```

`Slice()` is the allocation boundary and should only be called where a `[]Handle` is genuinely owned (list construction, `RelocateList`, append/concat in transformers). Prefer `Store.ListLen` / `ListAt` / `ListIndexOf` when the `ListRef` is at hand. Since PR `#25` (`b20671e0b8`, struct `NodeSeq` with an inlinable `All`), `BenchmarkListSlice` and `BenchmarkDeclarationNodes` run at 0 B/op, 0 allocs/op; the earlier 32 B / 2 allocs from the func-typed iterator is gone.

## Concurrency

- One writer per file during build. Parse and bind of one file never run concurrently with each other; `BindSourceFiles` queues one bind per unbound file, gated by `SourceFile.BindOnce`.
- `Freeze` publishes the Store as immutable for parallel check. There is no cross-store write lock because check does not write parse Stores. `SubtreeFacts` uses atomics on the `subtreeFacts` column.
- Each `Checker` has its own synth Store, so parallel checkers never share a writer.
- Emit allocates in a context-private Store and can run across files in parallel. SourceFile publishes a coherent immutable Store/root/Kind state through an atomic pointer; reads do not acquire a mutex or reload the node header.
- StoreSet lookup loads atomic pointers; only writers acquire its mutex. Store registration does not synchronize subsequent AST writes. A shared target must be frozen or governed by the same single writer.
- Deferred TS JSDoc (every TS comment without `@see`/`@link`) is parsed lazily, like upstream, but into a Store other than the frozen parse Store. The checker parses on demand into its own synth Store through `Handle.JSDocIn(file, c.jsdoc)`; each checker keeps its own `JSDocCache`, so nothing is shared between parallel checkers. `Handle.JSDoc(file)` serves consumers without a cache (LS, API, astnav) from a per-file side Store that `warmSharedJSDoc` fills once and freezes before publishing through `sync.Once`. `Handle.EagerJSDoc` returns only parser-time JSDoc and never parses. The JSDoc root's parent edge is an `externalParent` on the owning Store; the parse Store is not written.

Race tests cover these (`TestFreezeConcurrent`, `TestFreezeAllowsParallelParseRead`, `TestStoreParallelFileWriters`, `TestSourceFileSerializesParseStoreWriters`, `TestSourceFileRefsAreSafeAcrossParallelCheckers`, `TestSetParentDoesNotRaceSourceStoreMaps`).

## Rules the pipeline depends on

These were derived from the live pointer pipeline before the cutover and are now encoded in Store. Each is a rule; breaking one is a silent semantic hole, not a perf regression.

1. `NodeRef(0)` **is optional-absent, not a missing token.** `NodeIsMissing` treats a real zero-width node as present-but-missing (error recovery). Optional fields are `0`. Never allocate a zero-width node for absence.
2. **Kind-specific payload is not all children.** The header carries kind, counts, flags, loc, parent, slot base. Everything else a factory argument carried (`TokenFlags`, `ModifierFlags`, literal text, `IsTypeOnly`, `MultiLine`, template flags, …) lives in generated value slots or side maps. Dropping a generated value is a functional break.
3. **Lists have their own loc** (trailing comma, list-level diagnostics). `listHeader`, `Factory.List`, `RelocateList`, and `CopySubtree.copyList` preserve it.
4. `GetSourceFileOfNode` **walks** `Parent()` **to** `KindSourceFile`, then resolves metadata through `Store.SourceFile()` / `StoreSet.File`. The `SourceFile` node is the tree root inside Store; metadata is the `*SourceFile` Go struct outside. A root whose Store has no `sourceFile` owner makes LS and checker file lookup return nil.
5. **JSDoc reparse mutates already-created hosts.** `@param` writes `Parameter.Type` and `QuestionToken` after the parameter exists; `@this` replaces the parameter list; `@template` assigns class type parameters. Named slots stay writable during build (`SetChild`, `SetList`); `Intern` after `Seal` appends. `jsdocHandleCache` (parser-time JSDoc) keys on `Handle` for the life of the `SourceFile` and is immutable after parse.
6. **Binder mutates flags and side data, not tree shape.** Flags are cleared and reset; Symbol, LocalSymbol, Locals, NextContainer, Flow, EndFlow, ReturnFlow are written on existing nodes. Parents and child lists are not rewritten. Flow also allocates `KindUnknown` payload nodes (`FlowSwitchClauseData`, `FlowReduceLabelData`) that hang off `FlowNode.Data` and never enter statement lists; that is the one remaining pointer `*Node` allocation on the compile path.
7. **Checker synthetics share children with the parse tree.** A synthetic access whose name child is a parse node lives on `Checker.synth`; the shared child is an `externalChild` `GlobalRef` on the synth Store, and the synthetic's parent (a parse node) is an `externalParent` on the synth Store. The frozen parse Store is never written. Synthetics never `CopySubtree` a parse node and never write a parse Store.
8. **Flow is a second pointer graph.** `FlowNode{Flags, Node Handle, Data *Node, Antecedent *FlowNode, Antecedents *FlowList}` is 48 bytes with four pointer words. Putting `*FlowNode` in `nodeHeader` would make `[]nodeHeader` scannable. The `flows` column holds arena ids, not pointers; FlowNodes themselves live in per-Store chunks (8, 8, 16, 32, 64, 128, then 256 entries) whose addresses never move. Pointers remain in `endFlows`/`returnFlows` side maps and inside `FlowNode`; the header stays noscan.
9. **Emit structurally shares unchanged parse nodes.** `Update*` returns the same `Handle` when every child is unchanged; transformers `return node` / `VisitEachChild`. The output tree is a mix of parse rows and emit rows in the **same** Store. `EmitContext` keys `original`, `emitNodes`, and `autoGenerate` on `GlobalRef`. A second emit Store plus mandatory `CopySubtree` would clone the unchanged spine and break `==` reuse; that design is rejected.

Checked and not a functional kill: the lexer (tokens are `Kind` + `TokenFlags` + text; `SourceFile.tokenCache` keys on `TokenCacheKey` and stores Handles), node-level incremental parse (`parsecache.go` keys whole files by content hash; there is no node reuse), same-snapshot LS caches (die with the `SourceFile`), `GetReparsedNodeForNode` (searches by loc containment), in-file `==` (becomes `NodeRef` equality), `PagedLinkStore` keyed by `GlobalRef` (`nodeLinkStore.key`; orphan Handles without a file fall back to a private counter), and the JSON config path (JSON parses into Store like everything else).

## Current code

| Path | Role |
| --- | --- |
| `store.go` | `Store`, `NodeRef`, `ListRef`, `Handle`, `nodeHeader`, `listHeader`, phases (`Seal`, `Freeze`, `EnterEmit`, `LeaveEmit`), `Checkpoint`/`Restore`, columns, side maps, external edges, walk |
| `store_identity.go` | `StoreID`, `GlobalRef`, `StoreSet`, the process-wide identity set (`RegisterFile`, `RegisterStore`, `UnregisterStore`, `NodeOf`) |
| `store_factory.go` | Store-only `Factory` (`NewFactory`, `NewFactoryHint`, `NewFactoryOn`), `HandleVisitor`, `Update*` reuse (`updateHandle`), list helpers |
| `store_schema.go` | Compatibility argument structs from the first Store-native experiments; delegate to the generated factory |
| `store_schema_generated.go` | Generated child, list, and kind-specific value slot tables for every factory kind |
| `store_handles_generated.go` | Generated `Factory.New*` / `Update*` and named `Handle` getters/setters (read through `childAt`) |
| `store_polymorphic_generated.go` | Generated polymorphic accessors (`Expression()`, `Name()`, …) as `switch h.Kind` |
| `store_query_manual.go` | Hand-written `Handle` queries: `Pos`/`End`, `Contains`, `JSDoc`/`JSDocIn`/`EagerJSDoc` and `JSDocCache`, `ListSlice`, `ListIndexOf`, `ModifierFlags`, `NodeId`, `GetReparsedHandle`, … |
| `store_subtree.go` | `Handle.SubtreeFacts` over the atomic `subtreeFacts` column |
| `store_copy.go` | `Factory.CopySubtree` (cross-store remap, list remap, external edges) |
| `node_sequence.go`, `symbol.go` | `NodeSeq` with allocation-free `All()`; `DeclarationNodes(symbol)` adapts `[]GlobalRef` declarations |
| `store_flatten.go` | `FlattenNode`: lossy `*Node` → Store copy kept for layout benches. No production or test caller at HEAD; delete with the pointer `Node` |
| `ast.go` | `SourceFile` metadata and its Store fields (`parseStore`, `parseRoot`, writer lease, shared JSDoc side Store); legacy pointer `Node` and `NewNodeFactory` |
| `ast_generated.go` | Legacy pointer `NodeFactory` and `nodeData` types (no compile-path caller) |
| `store_*_test.go`, `*_bench_test.go` | Unit, race, copy, layout, identity, adversarial GC, e2e, GOGC baseline, flow-layout and key-type benches |

Generated files come from `node --experimental-strip-types tools/scripts/tsc/generate-go-ast.ts` followed by `gofmt`; `git diff --exit-code` on them is a gate.

## How to verify

From `tsc/`:

```bash
go test ./internal/ast -run 'TestStore|TestFactory|TestFreeze|TestEnterEmit|TestGlobal|TestStoreSet|TestNodeHeader' -count=1
go test ./internal/ast ./internal/parser ./internal/binder ./internal/checker ./internal/compiler ./internal/printer -count=1
go test -race ./internal/ast ./internal/parser ./internal/binder ./internal/compiler -count=1
go test ./internal/ast -run '^$' -bench '^Benchmark(ListSlice|DeclarationNodes)$' -benchmem
go test ./internal/ast -run '^$' -bench E2E -count 3
go test ./internal/ast -run TestTsgoGOGCBaseline -v          # builds noembed tsc, needs the v6.0.3 smoke checkout
STORE_TSGO_E2E=1 go test ./internal/ast -run TestTsgoStoreE2E -v
```

`TestTsgoGOGCBaseline` and `TestTsgoStoreE2E` look for the TypeScript v6.0.3 checkout at `smoke/typescript-6.0/src/compiler`, then `/tmp/typescript-6.0/src/compiler`; set `STORE_TSGO_PROJECT` to override. Clone with `git clone --depth 1 --branch v6.0.3 https://github.com/microsoft/TypeScript.git` and run `npm ci` and `npx hereby generate-diagnostics` there.

Gates for any Store change are in `.github/skills/store-ast-verification/SKILL.md`: unit, race, generated-code reproducibility, live CLI smoke (`.cursor/skills/verify-tsc`), and interleaved wall time against the parent or trunk. Tests alone are not verification.

Package e2e benches compare a pointer tree and a Store on the same flattened fixture. They prove layout, not compile wall time. `BenchmarkNewProgram` in `compiler/program_test.go` is too small for a perf baseline.

## Performance record

### GOGC baseline

`TestTsgoGOGCBaseline` rebuilds the noembed CI binary and checks the v6.0.3 compiler project, interleaving default GOGC, `GOGC=off`, `GOGC=200`, and `GOMEMLIMIT=8GiB`. Every child must exit 0. Medians on 2026-08-31:

| Environment | median |
| --- | --- |
| default GOGC | 146ms |
| `GOGC=off` | 94ms |
| `GOGC=200` | 128ms |
| `GOMEMLIMIT=8GiB` | 149ms |

Default / `GOGC=off` is 1.55 and `GOMEMLIMIT` / `GOGC=off` is 1.58, so tuning alone does not recover the GC gap and the layout bet stays live. The harness enforces both ratios. Earlier numbers (2026-08-27, generated workload) were discarded because that harness accepted a `TS2307` exit 2 as a completed check.

### Compile wall time

CI smoke `-p src/compiler --noEmit`, default GOGC, `built/local/tsc`:

| Head | SHA | median | Note |
| --- | --- | --- | --- |
| PR-5 (bind on Store, `ExpandStore`) | `21fced2ca1` | 0.395s | 2026-08-31 run |
| PR-6 (emit on Store, delete expand) | `049214aa25` | 0.268s / 1.559s | 2026-08-31 run / 2026-09-02 run |
| PR-7 base (one-shot Store compile) | `4636d0b813` | 1.543s | 2026-09-02 run, 0.99× PR-6 |
| trunk (`origin/main` `6d44e0584a`) | | 0.147s (2026-08-31 run); 2026-09-02 run pending | the 1.05× rule against trunk has not been recorded on the Store-only path |

The two runs were on different hosts and are not comparable with each other. The maintainer-facing head-vs-trunk proof is proof-plan PR-5.

### Layout attempts (`tsc/.audit/attempts/`)

| Attempt | Verdict | Result |
| --- | --- | --- |
| 015 generated getters read `childAt` instead of `Child` | keep | getter −27% min / −29% median in the accessor microbench; public API unchanged |
| 016 polymorphic accessors as kind→slot table | revert | −0.9%, benchstat p=0.076; E2E unchanged |
| 018 `nodeHeader` 44B → 24B, Symbol column indexed | keep | VS Code `src/tsconfig.json` `--noCheck`: Bind −16% (CI excludes 0), Parse flat, `AllocSlots` −29%, GC mark −13%, `Memory used` −21% under `GOGC=off` |
| T10 NodeSeq integration | ISSUES then fixed | 256-Handle materialization gone; residual 32 B / 2 allocs from the func iterator removed by the struct `NodeSeq` (`#25`); now 0 allocs |

Design notes for the accessor work are in `tsc/.audit/store-accessor-design.md`.

### Lazy TS JSDoc (`docs/lazy-jsdoc-checker-store-bench.md`, 2026-09-07)

Moving deferred TS JSDoc out of `BindSourceFiles` (`WarmJSDoc` into the parse Store) and into per-checker `JSDocCache`s on `Checker.synth`: Monaco Bind −20% (`--singleThreaded`) / −29% (`--noCheck`), Check unchanged within noise, allocs −1.6…−2.6%. Checkers parse 3.1k of the 19.4k deferred hosts.

### Memory (`docs/store-memory-investigation.md`, 2026-09-05)

On the Monaco tsconfig, parse + bind final live heap is **−39.4 MiB (−9.0%)** versus the pointer AST. The checker adds **+12.2 MiB** on the Store path, so full-check live heap nets **−27.3 MiB (−3.4%)**. Causes, in order: the pointer AST was already arena-allocated per kind; Store moves fields into side columns rather than deleting them; `FlowNode` grew from about 32 to 48 bytes and `flows` is node-width; checker caches keyed on 16-byte pointer-bearing `Handle`; macOS compression makes RSS unreliable, so compare `Memory used` (`runtime.MemStats.Alloc` after GC), not RSS.

## Open work

Ordered by what unblocks the upstream request.

1. **Emit and NodeBuilder leases** (proof-plan PR-4). `TestLocal/alias` type baselines allocate on a frozen parse Store. Every writer after `Freeze` must be the emit lease or a synth Store. Also confirm generated `VisitEachChild` honours the `VisitEmbeddedStatement` hook on full `TestLocal`.
2. **Checkers scaling** (PR-1) and **lock-free `StoreSet.At`** (PR-2). `NodeOf` takes an RLock per call. Publish a read-only snapshot or seal parse identity after bind and keep synth in a side domain.
3. **Handle-keyed checker caches → `GlobalRef`.** 22 `map[ast.Handle]` fields and 12 `LinkStore[ast.Handle, …]` remain in `checker`. They are scannable and twice the key width.
4. **Pointer-free `FlowNode`.** Replace `Node Handle` with `NodeRef`/`GlobalRef`, `Data *Node` with an arena index or union, and antecedent pointers with arena indices. This removes the last compile-path `*Node` allocation and most of the remaining GC mark time (attempt 018 "次").
5. **Allocation hints.** `len(sourceText)/5` over-reserves `nodes` by 1.7–2.8× (`docs/bind-column-presize-bench.md`); accepted for now because growing the 24 B node column is the costlier failure. Pre-sizing bind columns from the hint was measured and rejected; the `flows` column itself is now 4 B noscan (`docs/flow-index-column-bench.md`).
6. **Delete the legacy pointer AST inside `ast`** (`Node`, `NodeFactory`, `ast_generated.go` pointer types, `store_flatten.go`, pointer `jsdocCache`, `ReparsedClones`). Only possible after item 4.
7. **`TestImportElision`** two red cases.
8. **PR-10**: the microsoft/TypeScript request, citing proof-plan PR-5 receipts. Do not land PR-3 through PR-9 on microsoft/main; the operator lands every PR.

## History and rejected approaches

| GitHub | Branch | Landed |
| --- | --- | --- |
| `#1` | `store-pr-1` | real tsgo GOGC baseline harness |
| `#2` | `store-pr-2` | Store locals, `NewFactoryOn`, `StoreSet.SetFile` |
| `#3` | `store-pr-3` | parse into Store through `NewFactory` with `ExpandStore` |
| `#4` | `store-pr-4` | bind Store handles then `ExpandStore` |
| `#5` | `store-pr-5` | checker links keyed on `GlobalRef` |
| `#6` | `cursor/store-pr-6-a9c9` (`049214aa25`) | emit on Store, `ExpandStore` deleted |
| `#7` | `cursor/store-pr-6b-native-parse-a9c9` (`4636d0b813`) | one-shot Store compile path: native parser, Handle consumers, materialize and dual-write deleted, `Freeze` after bind, `Checker.synth`, emit writer lease |
| `#8` | `cursor/store-pr-8-e2e-a9c9` | e2e smoke, PR-9 skip record |
| `#9`–`#13` | `fix/attach-parent-emit-phase`, `exp/handle-kind-field`, `perf/node-header-24b` | attach-phase parent fix, `childAt` getters, `VisitEmbeddedStatement` reentry fix, 24-byte header |
| `#14`–`#28` | `cursor/nodeseq-*` | allocation-free `NodeSeq` API and its migration across checker, LS, printer, transformers |

Rejected, with the reason, so they are not re-proposed:

- **Dual tree / materialize bridge** (`MaterializeSourceFile`, `parser_*_store.go` beside `parser.go`, `store_bridge.go` dual-write). Two parsers and two trees; the reviewable diff becomes the bridge, and the pointer objects stay GC-scanned. PR-7 rewrote parser, binder, checker, and printer in one compile instead.
- **`FlattenNode` as a production bridge.** It drops TokenFlags, most literals, symbols, and flow. Measurement only.
- **`Handle` as a long-lived map key.** Pointer contamination and nondeterministic ordering. Use `GlobalRef` (cross-store) or `NodeRef` (same store).
- **Per-node global id column.** Reintroduces atomic assignment plus a reverse map.
- **Synthetics appended into the parse Store under a per-file lease during check.** Shared writers across parallel checkers; fights `Freeze`.
- **Second emit Store with mandatory `CopySubtree`.** Clones the unchanged spine and breaks `Update*` reuse.
- **Polymorphic accessors as a kind→slot table** (attempt 016). No measurable win.
- **JSON-only first migration wave.** `parseJSONText` shares the expression parser, so the old path could not be deleted in the same wave.

The shippable-unit rule that governed the migration still applies to future waves: one native producer, every consumer on the new type, the old path deleted in the same change, no permanent flags or dual trees.

## Related documents

| Path | Content |
| --- | --- |
| `docs/store-upstream-plan.md` | PR-1…PR-10 program (PR-1…PR-8 landed) |
| `docs/store-maintainer-proof-plan.md` | follow-on PR-1…PR-5: checkers scaling, lock-free identity, synthetic ownership, emit leases, head-vs-trunk proof |
| `docs/store-beta-plan.md` | historical β close-out program |
| `docs/store-memory-investigation.md` | why the live-heap win is smaller than the AST size win |
| `docs/lazy-jsdoc-checker-store-bench.md` | deferred TS JSDoc parsed into the checker's synth Store instead of `WarmJSDoc` |
| `tsc/.audit/store-accessor-design.md`, `tsc/.audit/attempts/015…018`, `tsc/.audit/allocation-free-node-sequences-t10.md` | measured accessor, header, and NodeSeq experiments |
| `.github/skills/store-ast-verification/SKILL.md` | verification gates |
| `.cursor/skills/verify-tsc/SKILL.md` | CLI live drive (`control-tsc launch` / `doctor`) |
