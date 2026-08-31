# About the Store-backed AST

This note records an **experimental** layout for a Store-backed syntax tree next to today’s pointer `*Node` AST. It fixes vocabulary and known constraints so the next change has a shared map. It is **not** a finished architecture decision record and **not** an API reference.

Motivation and early sketch: [TypeScript#63807](https://github.com/microsoft/TypeScript/issues/63807).

## Status

Safe to keep on a branch as a **package-local experiment**. PR-3 (unlanded) has `ParseSourceFile` allocate through `NewFactory` and `ExpandStore` at the parse boundary. The production tree returned to binder is still `*Node`. Do not land `ExpandStore` on microsoft/main; PR-6 deletes it.

Do **not** treat this document as a merged, settled design for the whole compiler until the [Open questions](#open-questions) below have written answers. The layout bet (packed header, noscan columns) has package-level evidence. Identity across files, mutation rules, incremental parse, emit sharing, and an end-to-end stop criterion do not.

## The problem

Go’s garbage collector is non-generational. `tsgo` keeps most parse and check data alive until the compile finishes. On large programs, each GC cycle scans a large live heap. In the issue write-up, `GOGC=off` on a VS Code-sized check made wall time about 1.24× faster. That number is an observed upper bound on GC-related cost for that workload. It is **not** proof that a Store layout recovers most of that gap, and it has not been compared against process-level tunables alone (`GOGC`, `GOMEMLIMIT`) as a no-code alternative on the same harness.

Profiles pointed at object scan time, with the parser AST a large share of in-use space. The pointer tree (`*Node`, `Parent *Node`, `[]*Node` lists, `nodeData` interfaces) gives the collector many pointers to chase. The experiment targets scan cost and live size for the long-lived tree.

## What Store is (β)

A `Store` owns one file’s syntax tree for one parse (or one emit rewrite). Nodes are dense `NodeRef` indices (`uint32`). `0` means missing. Stack code uses `Handle` (`*Store` plus `NodeRef`). Heap maps and slices should store `NodeRef`, not `Handle`.

Each node is one packed, pointer-free header row (`kind`, `flags`, `pos`, `end`, `parent`, child range, optional text intern id). Children live in a packed `[]NodeRef` column. Variable-length lists use `ListRef` into the same column. Headers intentionally contain no Go pointers so those backing arrays can be noscan.

`Seal` drops only the construction-time `internIdx` map. It does **not** freeze the tree: `SetChild`, `SetFlags`, `SetSymbol`, and similar mutators still run. Binder today mutates `Node.Flags` heavily on the pointer AST; a Store-backed binder would need an explicit mutation story (who may write, when, and with what synchronization).

`Factory` allocates only into its owned `Store` and returns `Handle` values. `CopySubtree` deep-copies a subtree into another (unsealed) `Factory`/`Store` and remaps `NodeRef`. Cross-store `SetChild` panics via `refInStore`.

`NodeRef` and `NodeId` (`uint64` on `*Node`) are different types. Do not cast between them.

## Identity across stores (β decision)

`NodeRef` is store-local, so anything that crosses files (`Symbol.Declarations`, checker caches keyed on `NodeId`) needs a process-wide identity. The β answer is `GlobalRef`: a `uint64` packing `StoreID` (high 32 bits) and `NodeRef` (low 32 bits). `StoreID` is assigned by a `StoreSet`, the identity domain a program would own. A Store belongs to at most one StoreSet; double registration panics.

Properties that drove the choice:

- Pointer-free. A `map[GlobalRef]V` with a pointer-free `V` stays noscan, which is the whole bet. Keying maps on `Handle` (or `*Node` as today) puts pointers back into every long-lived map.
- Deterministic. Register stores in file order and `GlobalRef` is stable across runs. Today’s `NodeId` is an atomic counter whose values depend on binder scheduling.
- No per-node cost. Identity is derived from position, not stored. Today’s `NodeId` costs an atomic CAS per first use per node.

Rejected: `Handle` as map key (pointer contamination, nondeterministic ordering), and a per-node global id column (reintroduces atomic assignment plus a reverse map for resolution).

Migration rule for existing consumers: `map[ast.NodeId]V` and `map[*ast.Node]V` become `map[ast.GlobalRef]V`; resolution back to a node goes through the program-owned `StoreSet.At`. `Symbol.Declarations []*Node` becomes `[]GlobalRef` when symbols migrate (step D).

Open in β: `StoreSet` lifetime for LS scenarios (stores die on edit; nothing today evicts a dead Store from the set). Emit and checker synthetics append into the parse Store (constraints 7 and 9), so they do not need a second registered identity domain.

## What we tried and rejected

**Column-per-field SoA** (separate `[]Kind`, `[]Flags`, …). Multi-field visits lost to a packed header on the package benches. Packed header stays the β layout.

**Steady-state dual representation** (long-lived `*Node` view over Store, or the reverse kept forever). Two live trees share identity and lifetime, grow memory, and erase the GC bet. Temporary dual shapes can appear inside a migration wave. Delete them in that wave. Expanding Store → `*Node` for one phase is a **different mechanism** from “`*Node` façade over Store”, but it shares the same failure mode if both stay alive.

**Moving `SourceFile` text into Store without a measured win.** See ownership.

## Ownership boundaries

Store owns the syntax tree: headers, child edges, lists, and identifier/literal intern bytes used as node text.

`SourceFile` metadata stays outside Store: source text, diagnostics, line maps, hashes, and similar file-level state.

Identifier interning inside Store is allowed. Owning the file’s full source string is not, until an A/B shows that file-offset interning beats `internBuf`.

## Transforms and printers (intent)

Parse would build a Store. `Seal` drops the intern map only. Binder writes flags and side maps on that Store. Checker synthetics and emit updates that share parse children also **append into the same Store** (see constraints 7 and 9). A second emit Store plus `CopySubtree` of the whole file is incompatible with `Update*` reuse. `CopySubtree` remains the primitive for the rare deep clone across files (`deepclone.go` comment). Slot reuse and in-place delete remain out of scope.

`CopySubtree` today walks child `NodeRef`s only. It does not remap `ListRef` payloads hung off kinds, because β has no kind that stores a `ListRef` in the header. Migration A/B must close that gap before list-bearing kinds ship.

## Concurrency

A `Store` is single-writer. Parse, bind, check, and emit transfer exclusive
ownership of a file's Store in phase order; readers may overlap only while no
phase is mutating it. Concurrent parsers and checker workers may write
different file Stores. `NewFactoryOn` means "append under the current phase's
ownership", not that two factories may append concurrently. This keeps locks
out of the per-node allocation and access path.

`StoreSet` is the synchronized cross-file identity and metadata index.
SourceFile bridge maps that are shared by checker workers require their own
synchronization; Store's single-writer rule does not make those maps safe.
`TestStoreParallelFileWriters` exercises the allowed topology under `-race`.
Peak memory for the one-Store-per-file policy is **unmeasured**.

## Migration sketch (backcast)

Useful as a dependency order, not as a committed ship plan:

| Step | Depends on |
| --- | --- |
| E. Delete `*Node` / old `NodeFactory` | D |
| D. Binder / checker / LS on Store identity | C, and a global identity design |
| C. Printer / transformers on Store | B, and list-aware copy |
| B. Parser writes Store | A, and a shippable slice definition |
| A. Kind schema covers parse | β slice + list slots |

Temporary bridges such as `FlattenNode` are **measurement-only**. They keep Kind, Flags, Loc, a small set of text kinds, and `ForEachChild`. They drop symbols, flow, most literals, `TokenFlags`, and other `nodeData` fields. Do not use `FlattenNode` as a production bridge.

## Current code

| Path | Role |
| --- | --- |
| `store.go` | `Store`, `NodeRef`, `ListRef`, `Handle`, walk, parents, Symbol/LocalSymbol/FlowNode/EndFlowNode/ReturnFlowNode/Locals/NextContainer side maps, packed `listSlots` |
| `store_identity.go` | `StoreID`, `GlobalRef`, `StoreSet` (cross-store identity, SourceFile metadata) |
| `store_schema.go` | BinaryExpression, Parameter, ArrayLiteral slot layout (β slice) |
| `store_schema_generated.go` | Generated child, list, and kind-specific value slots for every factory kind |
| `store_factory.go` | Store-only `Factory`, `NewFactoryOn` |
| `store_bridge.go` | `NodeFactory.AttachStore` dual-write into Store during parse |
| `store_expand.go`, `store_expand_generated.go` | Exhaustive temporary Store → `*Node` copy; unknown non-token kinds panic (delete in PR-6) |
| `store_copy.go` | `Factory.CopySubtree` (cross-store remap) |
| `store_flatten.go` | Lossy `*Node` → Store copy for benches |
| `store_*_test.go`, `store_*_bench_test.go` | Unit, copy, adversarial, and e2e benches |

## How to re-check the layout bet

From `tsc/`:

```bash
go test ./internal/ast -run 'TestStore|TestFactory|TestFlatten' 
go test ./internal/ast -run TestE2ELayoutReport -v
go test ./internal/ast -run '^$' -bench E2E -count 3
```

Use e2e numbers only for pointer-tree vs Store layout on a flattened file. They do not prove parser wiring, offset interning, or compile wall-time wins.

## cmd/tsc GOGC baseline

`TestTsgoGOGCBaseline` rebuilds the noembed CI binary and checks the TypeScript
v6.0.3 compiler project used by the repository smoke job. Set
`STORE_TSGO_PROJECT` when that checkout is outside
`smoke/typescript-6.0/src/compiler`. Runs are interleaved and rotated across
default GOGC, `GOGC=off`, `GOGC=200`, and `GOMEMLIMIT=8GiB`; every child must
exit 0, including an untimed preflight.

The earlier 2026-08-27 numbers are discarded. That harness accepted exit 2
while compiling `tsc/testdata/fixtures/compiler/checker.ts`, whose unresolved
`./_namespaces/ts.js` import stopped the run in module resolution. It therefore
did not measure a completed check and could not support its recorded verdict.

The generated-workload medians recorded earlier on 2026-08-31 are also
superseded: that workload completed successfully but did not represent the CI
compiler check.

CI smoke-project medians on 2026-08-31:

| Environment | median |
| --- | --- |
| default GOGC | 146ms |
| `GOGC=off` | 94ms |
| `GOGC=200` | 128ms |
| `GOMEMLIMIT=8GiB` | 149ms |

Default / `GOGC=off` is 1.55, and `GOMEMLIMIT` / `GOGC=off` is 1.58.
`GOMEMLIMIT` does not recover the GC-off gap, so the corrected PR-1 rule passes.
The harness enforces both ratios.

## Open questions

Answer these in writing before calling the design “settled” or merging it as architecture guidance.

1. **Global identity.** Answered at the β level: `GlobalRef` = `StoreID` + `NodeRef` via `StoreSet` (see [Identity across stores](#identity-across-stores-β-decision)). Still open inside it: StoreSet eviction for LS edits, and whether emit stores register.
2. **Shippable migration unit.** The four-condition rule still holds. A JSON-only wave is **not** a β gate. Parser + AST can move together if that is what makes the functional constraints testable. JSON remains a possible later slice, not a prerequisite. See [Shippable migration unit](#shippable-migration-unit-β-decision).
3. **Mutation after parse.** Answered: `Seal` does not freeze the tree. Binder writes flags in place (`binder.go` reachability reset and `|=` of `HasImplicitReturn` / `ContainsThis`). Pointer payloads (Symbol, Locals, FlowNode) live in side maps, not in the noscan header. Concurrent bind is one Store per file (`program.go` `BindSourceFiles` work group), so no cross-store write lock. See [Functional constraints](#functional-constraints-β).
4. **`ListRef` on nodes.** Still open as schema work. Parser builds Go slices then wraps a `NodeList` once (`parseListIndex`), so a Store parser can `AllocList` at list-finish rather than grow in place. JSDoc reparse injects into those slices **during** parse, before the list is wrapped (`parser.go:445`, `parser.go:622`).
5. **Full-fidelity flatten or native parse.** Answered: native parse. `FlattenNode` stays measurement-only. It already drops TokenFlags, most literals, symbols, and flow (`store_flatten.go`).
6. **Synthetic nodes.** Revised: they share **parse children**, so they cannot live in a separate Store under today's `NodeRef` child column. Append into the parse Store, or change the child encoding. Parallel checkers make unsynchronized append a live constraint. See constraint 7.
7. **Incremental parse.** Answered for current tsgo: not a kill. `project/parsecache.go` keys whole files by content hash and reparses. Node-object reuse from the JS compiler is not implemented. Binder's "reset flags for incremental" comment is leftover; bind runs on the freshly parsed file.
8. **Stop criterion.** Still open as a measurement, not as a functional blocker. Layout evidence is package-local. A `GOGC` / `GOMEMLIMIT`-only baseline on a large `tsgo` run can still abandon the **perf** bet. It does not decide the functional constraints below.

## Shippable migration unit (β decision)

A migration unit is shippable only if all four hold in one wave:

1. The kind schema it needs is in (`store_schema.go` slots plus list slots if any).
2. Exactly one producer writes Store natively (no `FlattenNode` on the shipped path).
3. Every consumer of that producer reads `Handle` / `NodeRef`.
4. The old `*Node` path for that producer is deleted in the same wave. No flags, no permanent dual tree.

Parser + AST in one wave is allowed. That is the intended first wave if the functional constraints are encoded in Store first. A JSON-only slice is optional later work, not a β exit.

### Blast-radius result for the JSON candidate (historical)

Checked, and the naive framing fails the rule. There is no standalone `ParseJSONText` producer. `Parser.parseJSONText` (`parser/parser.go:157`) is an entry point on the shared parser that calls the shared `parseObjectLiteralExpression`, `parseArrayLiteralExpression`, `parseLiteralExpression`, `parsePrefixUnaryExpression`, and `parseTokenNode`, and allocates through the shared `p.factory` (`NodeFactory`). Migrating that entry point to Store cannot delete its old path, because TypeScript files reach the same functions. Condition 2 (one native producer) and condition 4 (delete old path in the same wave) both break.

Its kind set is also wider than assumed: `SourceFile`, `ExpressionStatement`, `NodeList`, an EOF token, and `PrefixUnaryExpression` for negative numbers, on top of object/array/property-assignment/literals.

`packagejson` is not a consumer of this tree. Only its test file mentions JSON parsing; the package itself does not import `ast` or `parser`.

A JSON-only wave is **not** required to finish β. Parser + AST may move in one wave if that is what makes the constraints below testable. The four-condition rule still applies to whatever wave ships. Native producer, all its consumers, old path deleted together.

## Functional constraints (β)

Checked against the live `*Node` pipeline (parser, binder, checker, printer). None of these is a reason to abandon a Store-backed tree **if** the matching rule is kept. Each row is a reason to abandon the **current header-only β slice** if it is treated as the full node.

### Must keep, or the pipeline is wrong

1. **`NodeRef(0)` is optional-absent, not a missing token.** `NodeIsMissing` (`utilities.go:66`) treats a real node with zero-width loc as present-but-missing (error recovery). Optional fields are `nil`. Those are different states. Allocating a zero-width node for the latter, using `0` only for the former.

2. **Kind-specific payload is not all children.** `nodeHeader` has kind, flags, loc, parent, child range, intern id. Live nodes also carry `TokenFlags` (`tokenflags.go`), `ModifierList.ModifierFlags`, `LiteralLike` text beyond the five kinds `FlattenNode` copies, `DeclarationBase.Symbol`, `ExportableBase.LocalSymbol`, `LocalsContainerBase.Locals` + `NextContainer`, `FlowNodeBase.FlowNode`, and `CompositeBase` subtree facts. Generated value slots preserve every factory argument: integer-like scalars use a pointer-free `map[uint64]uint64`, strings use Store intern ids, and the few pointer/slice values use a sparse side map. Token flags remain in the packed header. Dropping any generated value is a functional break.

3. **Lists have their own loc.** `NodeList.HasTrailingComma` is `last.End() < list.End()` (`ast.go:138`). `listHeader` already stores loc. Copy and schema must preserve it.

4. **`GetSourceFileOfNode` walks `Parent` to `KindSourceFile`** (`utilities.go:861`) and callers then read file metadata (`Text`, diagnostics, hash). The SourceFile **node** is the tree root inside Store. Metadata stays outside, keyed by `StoreID`. A root whose parent is `0` with no Store-to-metadata map makes LS and checker file lookup return nil.

5. **JSDoc reparse mutates already-created hosts, not only unfinished lists.** `@param` writes `Parameter.Type` and `QuestionToken` after the parameter exists (`reparser.go:480`). `@this` replaces `FunctionLikeData().Parameters` with a longer `NodeList` (`reparser.go:514`). `@template` can assign `class.TypeParameters` after the class node exists (`reparser.go:471`). Statement-level `@typedef` still splices via `reparseList` during `parseListIndex` (`parser.go:622`, `parser.go:445`). Store: named slots stay writable (`SetChild`); `list0` is replaced with a new `ListRef` (old list abandoned). `Intern` after `Seal` appends without dedup so lazy TS JSDoc can still add text. `jsdocCache` keys stay `NodeRef`.

6. **Binder mutates flags and side data, not the tree shape.** Flags are cleared and reset (`binder.go:1546`). `Symbol` / `LocalSymbol` / `Locals` / `NextContainer` / `FlowNode` / `EndFlowNode` / `ReturnFlowNode` are written on existing nodes (`binder.go:598`, `binder.go:2534`). Parent and child lists are not rewritten. `BindSourceFiles` queues one bind per unbound file (`compiler/program.go:554`), gated by `SourceFile.BindOnce`. Side maps plus mutable header flags are enough. `Seal` dropping `internIdx` is fine. Flow also allocates `KindUnknown` payload nodes (`FlowSwitchClauseData`, `flow.go:50`) that hang off `FlowNode.Node` and never enter statement lists.

7. **Checker synthetics share children with the parse tree, not only identity space.** `isPropertyInitializedInConstructor` builds a synthetic access whose name child is the parse-tree `propName` (or `propName.Expression()`), then sets `reference.Parent = constructor` (`checker.go:5053`). The same pattern is used for static blocks (`checker.go:5040`). `children []NodeRef` is store-local, and `SetChild` panics across stores. A separate checker Store cannot hold those edges without deep-copying the shared parse nodes (changes `==` used in flow) or encoding `GlobalRef` in the child column (abandons dense `NodeRef`). The shape that preserves today's semantics is: synthetics **append into the parse Store**. `Checker.factory` is a per-checker value `NodeFactory` (`checker.go:673`). Up to four checkers run in parallel (`checkerpool.go`) and must not mix types (`program.go:583`). Parallel append into one parse Store needs a policy.

8. **Flow is a second pointer graph.** `FlowNode` holds `*Node` plus `*FlowNode` antecedents (`flow.go:27`). Noscan headers do not remove it. Putting `*FlowNode` in `nodeHeader` would make `[]nodeHeader` scannable and erase the layout bet. Side map `map[NodeRef]*FlowNode` is the only shape that keeps headers noscan. Abandon "every node field lives in the packed header" if that claim is still in play.

9. **Emit structurally shares unchanged parse nodes.** `Update*` returns the same `*Node` when every child pointer is unchanged (`ast_generated.go` `Update*` family). Transformers then `return node` / `VisitEachChild`. The output tree is a mix of parse nodes and emit-factory nodes. `EmitContext` maps (`original`, `emitNodes`) key on `*Node` (`emitcontext.go:17`). Comments and source maps walk `ParseNode` / `MostOriginal`. A second emit Store plus mandatory `CopySubtree` would clone the unchanged spine, break `==` reuse, and force every original link to be rewritten. That is allowed only if emit keeps parse nodes as the shared spine (append new nodes into the parse Store, or let the emit tree hold parse `GlobalRef`s). The earlier β line "emit is a second Store and `CopySubtree` is the primitive" is incompatible with today's sharing.

### Not a functional kill (checked)

- **Lexer.** `Scanner` returns `Kind`, `TokenFlags`, and text (`scanner.go`). LS token-at-position rescan uses text+pos (`astnav/tokens.go`). Token objects, when created, live in `SourceFile.tokenCache` keyed by parent `*Node` (`ast.go:2917`). Key becomes `NodeRef`.
- **Node-level incremental parse.** Overlay changes rewrite content+hash (`overlayfs.go`). `ParseCacheKey` includes the hash (`parsecache.go`). Edits reparse the file. `parser.go` still has a commented `!!! incremental parsing` stub; it is unused.
- **Same-snapshot LS caches.** `jsdocCache` / `tokenCache` / `declarationMap` key on `*Node` while that `SourceFile` lives. They do not survive `DidChangeFile`. Key substitution is enough.
- **`GetReparsedNodeForNode` pointer identity.** It searches by loc containment and kind+loc.
- **In-file `==` on `*Node`.** Becomes `NodeRef` equality in one Store. Cross-file `Symbol.Declarations` / `ValueDeclaration` already have `GlobalRef`.
- **`PagedLinkStore` keyed by `GetNodeId`.** `GlobalRef` works as a `uint64` key (high bits go through `pageMap` in `core/linkstore.go`). Per-Store `NodeRef` pages are denser. Neither is a correctness break.
- **JSON config path.** Independent of whether Store can represent a TypeScript file.

## What "β is done" means

β is done when the functional constraints above are written and the layout+identity code matches them, so a parser wave can start without discovering a silent semantic hole. JSON is out of that bar.

| Exit item | State |
| --- | --- |
| Layout bet has package-level evidence | done |
| Cross-store identity exists in code | done (`store_identity.go`) |
| Functional constraints written from live parser/binder/checker | done (this section) |
| Header/side-map split implemented for TokenFlags, Locals, FlowNode, NextContainer, extra intern kinds | TokenFlags on header; generated scalar/string/object value slots; FlowNode / Locals / NextContainer side maps; Intern after Seal |
| Child slots and lists remain writable after the host exists (JSDoc reparse + lazy TS JSDoc) | done (`SetChild`, `SetList`, Intern after Seal) |
| Synthetics and emit updates append into the parse Store (no cross-store child edges) | `NewFactoryOn` |
| Store-to-SourceFile metadata map | `Store.SetSourceFile` keeps the per-file metadata owner; `StoreSet.SetFile` / `File` resolves it across stores |
| `ListRef` in schema + `CopySubtree` remaps lists | done (`list0`, ArrayLiteral, FunctionExpression params, `copyList`) |
| `GOGC` / `GOMEMLIMIT`-only baseline on a large `tsgo` run | PASS-PERF on the TypeScript v6.0.3 CI smoke project (see [cmd/tsc GOGC baseline](#cmdtsc-gogc-baseline)) |

`BenchmarkNewProgram` (`compiler/program_test.go:308`) is too small for the perf baseline.
