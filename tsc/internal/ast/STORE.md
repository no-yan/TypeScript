# About the Store-backed AST

This note records an **experimental** layout for a Store-backed syntax tree next to today’s pointer `*Node` AST. It fixes vocabulary and known constraints so the next change has a shared map. It is **not** a finished architecture decision record and **not** an API reference.

Motivation and early sketch: [TypeScript#63807](https://github.com/microsoft/TypeScript/issues/63807).

## Status

Safe to keep on a branch as a **package-local experiment**. PR-6 froze emit on
Store and deleted `ExpandStore`. The 6B/6C split (native parse that keeps
`MaterializeSourceFile`, then a later Handle consumer PR) is rejected: that is
a dual tree, and the reviewable diff becomes the bridge. PR-7 rewrites parser,
binder, checker, and printer onto Store in one shot. GitHub ids increment 6,
7, 8, 9, 10. Production visitors still consume `*Node` until that PR lands.

**Resume in a new session:** read [PR-7 resume](#pr-7-resume-one-shot-no-bridge) before editing parser or checker. The armed `/goal` text still says “PR-8 against microsoft citing PR-7 e2e”. That numbering is superseded: microsoft is PR-10 citing PR-8 e2e. Do not mark the goal complete until that PR exists.

Do **not** treat this document as a merged, settled design for the whole compiler until the [Open questions](#open-questions) below have written answers. The layout bet (packed header, noscan columns) has package-level evidence. Identity across files, mutation rules, incremental parse, emit sharing, and an end-to-end stop criterion do not.

## The problem

Go’s garbage collector is non-generational. `tsgo` keeps most parse and check data alive until the compile finishes. On large programs, each GC cycle scans a large live heap. In the issue write-up, `GOGC=off` on a VS Code-sized check made wall time about 1.24× faster. That number is an observed upper bound on GC-related cost for that workload. It is **not** proof that a Store layout recovers most of that gap, and it has not been compared against process-level tunables alone (`GOGC`, `GOMEMLIMIT`) as a no-code alternative on the same harness.

Profiles pointed at object scan time, with the parser AST a large share of in-use space. The pointer tree (`*Node`, `Parent *Node`, `[]*Node` lists, `nodeData` interfaces) gives the collector many pointers to chase. The experiment targets scan cost and live size for the long-lived tree.

## What Store is (β)

A `Store` owns one file’s syntax tree for one parse (or one emit rewrite). Nodes are dense `NodeRef` indices (`uint32`). `0` means missing. Stack code uses `Handle` (`*Store` plus `NodeRef`). Heap maps and slices should store `NodeRef`, not `Handle`.

Each node is one packed, pointer-free header row (`kind`, `flags`, `pos`, `end`, `parent`, child range, optional text intern id). Children live in a packed `[]NodeRef` column. Variable-length lists use `ListRef` into the same column. Headers intentionally contain no Go pointers so those backing arrays can be noscan.

`Seal` drops only the construction-time `internIdx` map. It does **not** freeze the tree: `SetChild`, `SetFlags`, `SetSymbol`, and similar mutators still run. Binder today mutates `Node.Flags` heavily on the pointer AST; a Store-backed binder would need an explicit mutation story (who may write, when, and with what synchronization).

`Factory` allocates only into its owned `Store` and returns `Handle` values. `CopySubtree` deep-copies a subtree into another (unsealed) `Factory`/`Store` and remaps `NodeRef`. Direct cross-store `SetChild` still panics via `refInStore`. The generated NodeFactory bridge records the exceptional cross-file children used by checker synthetics in sparse, pointer-free `GlobalRef` side tables; ordinary tree edges remain dense `NodeRef`s.

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

Parse builds a Store. `Seal` drops the intern map only. Binder writes flags and side maps on that Store. Checker synthetics and emit updates that share parse children also **append into the same Store** (see constraints 7 and 9). `EmitContext.AttachStore` holds the per-file writer lease across transform and print, then publishes generated NodeRefs before returning the context to its pool. Emit original, emit-node, generated-name, text-source, assigned-name, and class-this tables key on `GlobalRef`. Generated `Update*` methods compare Store identity for child reuse, and `UpdateSourceFile` carries Store ownership to the transformed root. A second emit Store plus `CopySubtree` of the whole file is incompatible with `Update*` reuse. `CopySubtree` remains the primitive for the rare deep clone across files (`deepclone.go` comment). Slot reuse and in-place delete remain out of scope.

`CopySubtree` today walks child `NodeRef`s only. It does not remap `ListRef` payloads hung off kinds, because β has no kind that stores a `ListRef` in the header. Migration A/B must close that gap before list-bearing kinds ship.

## Concurrency

A `Store` is single-writer. Parse, bind, check, and emit transfer exclusive
ownership of a file's Store in phase order; readers may overlap only while no
phase is mutating it. Concurrent parsers and checker workers may write
different file Stores. `NewFactoryOn` means "append under the current phase's
ownership", not that two factories may append concurrently. This keeps locks
out of the per-node allocation and access path.

`StoreSet` is the synchronized cross-file identity and metadata index, and a
Store ID is published atomically. SourceFile's pointer↔`NodeRef` bridge is
guarded independently: each checker receives a private snapshot and atomically
merges its factory map after checking. A per-SourceFile writer lease serializes
the complete checker mutation window, including Store append; different file
Stores remain parallel. Store's single-writer rule does not make the bridge
maps safe by itself. `TestStoreParallelFileWriters`,
`TestSourceFileRefsAreSafeAcrossParallelCheckers`, and
`TestSourceFileSerializesParseStoreWriters` exercise the allowed topology under
`-race`. Peak memory for the one-Store-per-file policy is **unmeasured**.

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
| `store_schema.go` | Compatibility argument structs for the first Store-native factory experiments |
| `store_schema_generated.go` | Generated child, list, and kind-specific value slots for every factory kind |
| `store_handles_generated.go` | Generated Store-native `Factory.New*` constructors and named Handle getters/setters for every schema factory kind |
| `store_factory.go` | Store-only `Factory`, `NewFactoryOn` |
| `store_bridge.go` | `NodeFactory.AttachStore` dual-write into Store during parse |
| `store_materialize_json.go` | Temporary full-fidelity Store-to-pointer bridge for the Handle-native JSON parser |
| `store_copy.go` | `Factory.CopySubtree` (cross-store remap) |
| `store_flatten.go` | Lossy `*Node` → Store copy for benches |
| `store_*_test.go`, `store_*_bench_test.go` | Unit, copy, adversarial, and e2e benches |

## How to re-check the layout bet

From `tsc/`:

```bash
go test ./internal/ast -run 'TestStore|TestFactory|TestFlatten' 
go test ./internal/ast -run TestE2ELayoutReport -v
go test ./internal/ast -run '^$' -bench E2E -count 3
go test ./internal/ast -run '^$' -bench ParserShapedConstruction -benchmem -count 5
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

7. **Checker synthetics share children with the parse tree, not only identity space.** `isPropertyInitializedInConstructor` builds a synthetic access whose name child is a parse-tree node, and effective call arguments can carry a tuple-name source from another file. Synthetics append into the checked file's parse Store. Same-file children stay dense `NodeRef`s; exceptional cross-file child and list edges use sparse `GlobalRef` side tables. Direct `SetChild` remains strict and panics across stores. A per-file writer lease serializes parallel checker append.

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
| Synthetics and emit updates append into the parse Store | `NewFactoryOn`; cross-file shared children use sparse `GlobalRef` edge tables |
| Store-to-SourceFile metadata map | `Store.SetSourceFile` keeps the per-file metadata owner; `StoreSet.SetFile` / `File` resolves it across stores |
| `ListRef` in schema + `CopySubtree` remaps lists | done (`list0`, ArrayLiteral, FunctionExpression params, `copyList`) |
| `GOGC` / `GOMEMLIMIT`-only baseline on a large `tsgo` run | PASS-PERF on the TypeScript v6.0.3 CI smoke project (see [cmd/tsc GOGC baseline](#cmdtsc-gogc-baseline)) |

`BenchmarkNewProgram` (`compiler/program_test.go:308`) is too small for the perf baseline.

## 6A freeze (emit on Store, delete expand)

GitHub `#6` (`cursor/store-pr-6-a9c9`) is frozen as PR-6 at SHA `049214aa25`. Do not add statement, type, binding, or generator grammar to that branch. PR-7 is the one-shot Store compile path: one parser, Handle consumers, delete materialize and dual-write together. Do not verify a materialize-keeping native parse as a destination.

Standalone `tsc/testdata/fixtures/compiler/checker.ts --noEmit` still exits 2 (`TS2307` missing `./_namespaces/ts.js`). That is not a completed check. The live compile is the TypeScript v6.0.3 CI smoke project. `--outFile` was removed (`TS5102`); JavaScript emit proof is the `emit-javascript` fixture.

### Unit / race / generate

| Gate | Result |
| --- | --- |
| `go -C ./tsc test ./internal/printer ./internal/transformers/... -count=1` | ok (`printer`, `tstransforms`; other transformer packages have no tests) |
| `./internal/parser ./internal/ast ./internal/binder ./internal/checker ./internal/compiler ./internal/tsoptions` | ok |
| `./internal/testrunner -run TestLocal/alias` | ok |
| `-race` parser, ast, printer, binder, checker, compiler, tsoptions | ok |
| `node --experimental-strip-types tools/scripts/tsc/generate-go-ast.ts` + `gofmt` + `git diff --exit-code` | GENERATE_OK |
| `git grep ExpandStore -- 'tsc/**/*.go'` | empty |
| production `FlattenNode` grep | empty |

### Live (`VERIFY_TSC_RUN_ID=pr6a-6a-20260831T083220`, SHA `049214aa25`)

| Drive | Result |
| --- | --- |
| `control-tsc doctor` | PASS, `Version 7.1.0-dev` |
| `tsc --help` | exit 0 |
| type-check fixture `-p … --noEmit` | exit 0 |
| emit-javascript fixture `-p …` | exit 0, nonempty `dist/index.js` contains `greet` / `Hello` |
| emit from file list `--outDir …/from-files` | exit 0, `index.js` contains `greet`, no `.d.ts` |
| emit-declarations fixture | exit 0, nonempty `index.js` and `index.d.ts` |
| CI smoke `-p /tmp/typescript-6.0/src/compiler --noEmit` | exit 0 |
| CI smoke `--outDir /tmp/store-smoke-out` | exit 0, 78 nonempty `.d.ts` (`emitDeclarationOnly` in that tsconfig) |
| `checker.ts --noEmit` | exit 2, `error TS2307` |

### Perf vs PR-5 (`21fced2ca1`) and trunk binary

Five interleaved timed runs after one warmup per head. Wall time of `built/local/tsc`. Gate is PR-6 median ≤ 1.10× PR-5. The PR-8 1.05× trunk rule is recorded, not applied.

| Workload | PR-5 median | 6A median | trunk median | 6A/PR-5 | 6A/trunk |
| --- | --- | --- | --- | --- | --- |
| `--noEmit checker.ts` (exit 2 all heads) | 0.7903s | 0.6125s | 0.2919s | 0.775 PASS | 2.098 record |
| `-p typescript-6.0/src/compiler --noEmit` (exit 0) | 0.3949s | 0.2676s | 0.1470s | 0.678 PASS | 1.820 record |

Deleting `ExpandStore` did not slow the check versus PR-5. Dual-write plus materialize still miss the trunk 1.05× gate, as expected until PR-7 deletes the pointer tree.

## PR-7 resume (one-shot, no bridge)

This section is the working state for the next session. The 6B/6C split is dead. Do not verify a materialize-keeping native parse as a destination. Do not add another pointer↔Store facade.

### Why the split was wrong

`MaterializeSourceFile` plus `parser_*_store.go` plus `parser.go` is two trees and two parsers. STORE.md's shippable unit already required one native producer, every consumer on Handle, and the old `*Node` path deleted in the same wave. A `*Node` view over Store is the rejected dual tree (it keeps the GC-scanned objects). `*Node` has a `Kind` field, so it cannot grow a `Kind()` method; Handle already uses `Kind()`. Unifying types means `node.Kind` → `node.Kind()`, `node == nil` → `node.IsNil()`, and maps keyed on `*Node` → `GlobalRef` / `NodeRef`.

The reviewable microsoft diff should be packed `nodeHeader` / `Handle` / `NodeRef` plus in-place parser and checker edits, not a trail of bridges.

### GitHub ids (increment 6–10)

| Id | Branch | SHA | Role |
| --- | --- | --- | --- |
| 6 | `cursor/store-pr-6-a9c9` | compiler `049214aa25`, receipts `b1077b1663` | Frozen emit; GitHub `#6` vs `store-pr-5` |
| 7 | `cursor/store-pr-6b-native-parse-a9c9` | branch HEAD | One-shot Store compile path. GitHub `#7` vs `#6`. Still contains native parse **and** materialize; delete both extra parser and materialize here |
| 8 | not opened | | e2e, diagnostic equality, 1.05× trunk |
| 9 | not opened | | leftover LS/format `*Node` if PR-7 left any |
| 10 | not opened | | microsoft/TypeScript PR citing PR-8 receipts |

Do not land PR-3 through PR-9 on microsoft/main. Program done when PR-10 exists against microsoft/TypeScript and its body cites PR-8 e2e receipts.

### What HEAD still has

Production `ParseSourceFile` for TS/JS tries `tryParseSourceHandle`, then `MaterializeSourceFile`, else `parseSourceFileWorker` dual-write via `AttachStore`. JSON is the same shape. Decorators (`@`) and JS JSDoc still fall back. TS JSDoc stays lazy (`nativeJSDocBlocksParse` is false for `ScriptKindTS`).

`TestCheckerTsNativeRejectSite` (`parser_statement_store_test.go`) asserts native parse of `tsc/testdata/fixtures/compiler/checker.ts`: `Store.Len() == created == materialize NodeCount` (298047) and production `ParseSourceFile` matches. That proves the Handle producer. It does not prove a Store-only compile path.

`bindStore` copies pointer binder side data onto Handles after `bind(file.AsNode())`. That is a dual bind. Delete it when binder walks `file.ParseRoot()`.

`checker/links.go` `nodeLinkStore` already keys on `GlobalRef` when `HandleOf` hits. Checker walkers still take `*ast.Node` (`checker.go` is 32k lines).

### Grammar that must stay true when folding the native parser into `parser.go`

- Native parse must not call `parseExpected` (diagnostics). Return false and rewind.
- `export { … }` / `export *` / `export as namespace`: `export` is syntax, not a modifier. `parseNativeModifiers` uses `lookAhead` + `nextTokenCanFollowModifier`. `export type T =` still takes export as a modifier. `export type { x }` is type-only `ExportDeclaration`.
- Array binding holes are `BindingElement` with nil members, not `OmittedExpression` (printer panics).
- Dotted `namespace a.b {}` nests `ModuleDeclaration`s with a synthetic export on the inner one, not `QualifiedName` (printer panics).
- Contextual keywords (`accessor`, `async`, …) at statement start: `lookAhead(isStartOfDeclaration)` or they parse as expressions (`accessor = …` in checker.ts).
- `tryParseNativeTypeArguments` failure must not fail the LHS (`1 << 0`).
- Type-arg close: `consumeNativeGreaterThan()`. Do not `ReScanGreaterThanToken`.
- Type predicates: lookAhead-only (`isNativeTypePredicateStart`). Do not Alloc then Restore.
- Direct `SetChild` across stores still panics. Checker synthetics that share a child from another file use sparse `GlobalRef` edges.

### Next edits (one PR, one compile at the end)

1. Rewrite `parser.go` in place onto `ast.Factory` / Handle. Fold or delete `parser_statement_store.go`, `parser_type_store.go`, `parser_expression_store.go`. Recovery still allocates into Store.
2. Convert binder, checker, printer, transformers to Handle. `ParseSourceFile` returns `*SourceFile` metadata whose tree is the parse Store. No `parseNodeRef` pointer map on the compile path.
3. Delete `store_materialize_json.go` and `store_bridge.go` production dual-write. Delete unused `*Node` factory paths that production no longer calls.
4. LS/format leftover, if any, is PR-9.

Do not keep interned `*Node` shells with child pointers. That restores the dual tree.

### Environment and live proof (this VM only; gone on a new agent)

If this Cloud Agent VM is still running:

- Go: `/tmp/go1.26`, `PATH="/tmp/go1.26/bin:$PATH"`, `GOTOOLCHAIN=local` (matches `tsc/go.mod` 1.26).
- Frozen binaries: `/tmp/tsc-6a-freeze` (PR-6), `/tmp/ts-pr5/built/local/tsc` (PR-5 `21fced2ca1`), `/workspace/built/local/tsc-trunk`.
- Smoke project: `/tmp/typescript-6.0/src/compiler` (TypeScript v6.0.3). Standalone `checker.ts --noEmit` exits 2 (`TS2307`). Live completed-check is CI smoke `-p /tmp/typescript-6.0/src/compiler --noEmit` exit 0. `--outFile` was removed (`TS5102`). JS emit proof is the `emit-javascript` fixture (`greet`/`Hello` in `dist/index.js`).
- `built/local/tsc` may still be an older binary until `VERIFY_TSC_RUN_ID=… ./.cursor/skills/verify-tsc/scripts/control-tsc launch`.
- Plan markdown is **CRLF**. Python rewrites must keep `\r\n`. `control-tsc` files stay LF.
- Do not commit `tsc/testdata/fixtures/compiler/checker.js` if it reappears.
- `ManagePullRequest` accepts `https://github.com/no-yan/typescript/pull/7` (lowercase). `https://github.com/no-yan/TypeScript/pull/7` is rejected as "PR URL must belong to the current repository". `git push -u origin <branch>` works. `gh` is read-only for writes.

### Scale (so the rewrite is not under-scoped)

Approximate line counts on this branch: `parser.go` 6905, `parser_statement_store.go` 1750, `parser_type_store.go` 1464, `parser_expression_store.go` 1180, `store_materialize_json.go` 761, `store_bridge.go` 192, `binder/store_bind.go` 49, `checker.go` 32367. Compile-path `*ast.Node` hits are on the order of 2k across checker/binder/printer/compiler. `.Kind` field reads exist across checker, binder, printer, transformers, and LS. cmd/tsc verification does not require LS, but the repo must compile, so leftover `*Node` in LS is PR-9 only if PR-7 would otherwise not compile. Prefer converting LS in PR-7 if the type of `Node` changes.

### Operator follow-ups that override older plan text

These were given in chat and are now the rule:

1. Split remaining GitHub PRs and increment **6, 7, 8, 9, 10** (not 6A/6B/6C then program PR-7/PR-8).
2. Parser and checker rewrite **without a bridge**, in one shot. The dual parser plus materialize is overly complex. A large in-place rewrite is preferred over another migration wave.
3. Stop at a durable checkpoint so a new context can resume. That checkpoint is this section plus GitHub `#7`.

The original armed `/goal` string is historical. Success is: PR-10 exists against `microsoft/TypeScript` and its body cites PR-8 e2e receipts. Tests alone are never verification. Do not land PR-3 through PR-9 on microsoft/main. The operator lands PR-1 and PR-2 only.

### GitHub `#7` (this PR)

- URL: `https://github.com/no-yan/TypeScript/pull/7` (API/tooling: `https://github.com/no-yan/typescript/pull/7`).
- Title: `PR-7: one-shot Store compile path (no materialize)`.
- Base: `cursor/store-pr-6-a9c9`. Head: `cursor/store-pr-6b-native-parse-a9c9`. Keep this branch. Do not open a 6C branch. Do not rename unless the operator asks.
- Draft. Parser work is `fb5bcfaf55` then `00225bf344`. Later commits on the branch are the course-correction docs and `Handle.IsNil`.
- Old chat said “GitHub `#7` is 6B, not program PR-7”. After the increment, GitHub `#7` **is** program PR-7 (the one-shot). e2e is the next GitHub PR (program PR-8) once opened.

### Bugs already fixed in the native producer (`00225bf344`)

Do not regress these when folding into `parser.go`:

- Import elision / type eraser: native `export { x }` treated `export` as a **modifier**. `UpdateExportDeclaration(..., nil modifiers)` cloned the `SourceFile`, so tests comparing `currentSourceFile == file` saw the original tree. Dual-write had nil modifiers. Fix: export is syntax; `parseNativeModifiers` matches the pointer parser’s `lookAhead` + `nextTokenCanFollowModifier`.
- Printer `ArrayBindingPattern`: holes must be `BindingElement` with nil members, not `OmittedExpression`.
- Printer `ModuleDeclaration`: `namespace a.b {}` must nest `ModuleDeclaration`s with a synthetic export on the inner one, not a `QualifiedName` name.
- Materialize needed `KindNamespaceExportDeclaration` and `KindOutKeyword`. After materialize deletion those kinds must still exist as Store slots.

### What was verified, and what must not be re-run as a destination

At `00225bf344` (native parse, still materializing): `go -C ./tsc test` ok for parser (including FuzzParser seeds), ast, tsoptions, binder, printer, compiler, `transformers/tstransforms` (ImportElision + TypeEraser), checker. `GENERATE_OK`. `gofmt -l tsc/internal/parser tsc/internal/ast` empty. `git grep ExpandStore -- 'tsc/**/*.go'` empty. `MaterializeSourceFile` still exists.

`TestCheckerTsNativeRejectSite` logs `storeLen` / `created` / `nodeCount` / `statements` (observed 298047 / 298047 / 298047 / 51). It does not hard-code the count.

**6B live and perf were never run and must not be run as the PR destination.** Checking those boxes would certify the dual tree. PR-7 live/perf apply only after materialize and the pointer parser are gone from production.

### Factory and Store details the next edit will hit

- Native constructors are `factory.New*` on `ast.Factory` plus `finishNativeHandle`. Not `store.New*`.
- `Store.Checkpoint` / `Restore` exist and are unused by native parse. A failed native attempt uses a **new** factory, not Restore.
- `NodeFactory.New*` already dual-writes through `storeAlloc` when `AttachStore` is set. That is why pointer parse plus Store is slower than Handle-only, and why deleting dual-write is the perf bet.
- `Handle.IsNil` is `s == nil || id == 0`. `NodeRef(0)` is optional-absent, not `NodeIsMissing`.

### Skills and proof (repo paths, not `/tmp`)

- CLI drive: `.cursor/skills/verify-tsc/SKILL.md` and `features/README.md`. Launch with `VERIFY_TSC_RUN_ID=… ./.cursor/skills/verify-tsc/scripts/control-tsc launch` then `doctor`.
- Store gates: `.github/skills/store-ast-verification/SKILL.md`.
- Walkthrough images, if taken: `/opt/cursor/artifacts/` (lane names are `pr7-lane*.png`, not `pr6b-lane*`).

### New VM: `/tmp` binaries will be gone

Do not treat `/tmp/tsc-6a-freeze`, `/tmp/ts-pr5`, `/workspace/built/local/tsc-trunk`, or `/tmp/typescript-6.0` as durable. Rebuild from SHAs:

- PR-6 binary: checkout `049214aa25`, `npx hereby build`, copy `built/local/tsc` aside. Do not overwrite it when building PR-7.
- PR-5: `21fced2ca1` (also `origin/store-pr-5` / `cursor/store-pr-5-hardening-a9c9`).
- Trunk: `origin/main` (this checkout was `6d44e0584a` at handoff).
- Smoke: `git clone --depth 1 --branch v6.0.3 https://github.com/microsoft/TypeScript.git /tmp/typescript-6.0` then `npm ci` and `npx hereby generate-diagnostics` as the store-ast-verification skill says.
- Go: install 1.26 if `/tmp/go1.26` is missing. Do not lower `tsc/go.mod`. `GOTOOLCHAIN=local` when using a local 1.26.

On the handoff VM, `built/local/tsc` was still the PR-6 binary (same size/mtime as `/tmp/tsc-6a-freeze`). A PR-7 live drive must rebuild first.

### Branches to ignore

JSON and expression-only native slices already landed on frozen `#6`. Do not resume `cursor/handle-native-parser-36e9`, `cursor/handle-native-expressions-e498`, `cursor/expand-native-expressions-e498`, `cursor/native-parser-slice-36e9`, or `cursor/optimize-store-parser-20a2` as the PR-7 vehicle.

### Fork / tooling traps

- Canonical repo: `https://github.com/no-yan/TypeScript.git`. `origin` may report `https://github.com/no-yan/typescript`. Same fork.
- `gh` is read-only. Open/update PRs with `ManagePullRequest`. Push with `git push -u origin <branch>`.
- This origin has no `pstack/` on `main`. Playbook `git show origin/main:pstack/...` fails; that is expected.
- Operator-facing replies are Japanese. Repo docs stay English.
