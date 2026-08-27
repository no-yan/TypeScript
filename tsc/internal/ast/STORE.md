# About the Store-backed AST

This note explains why `tsc/internal/ast` is growing a Store-backed tree next to the pointer `*Node` AST, what the target shape is, and which migration order that target forces. It is not an API reference and not a how-to.

Motivation and early sketch: [TypeScript#63807](https://github.com/microsoft/TypeScript/issues/63807).

## The problem

Go’s garbage collector is non-generational. `tsgo` keeps most parse and check data alive until the compile finishes. On large programs, each GC cycle scans a large live heap. Forced `GOGC=off` on a VS Code-sized check made wall time about 1.24× faster in the issue write-up. That gap is an upper bound on how much GC work can cost.

Profiles pointed at object scan time, with the parser AST a large share of in-use space. The pointer tree (`*Node`, `Parent *Node`, `[]*Node` lists, `nodeData` interfaces) gives the collector many pointers to chase. Green Tea’s span-scan batching may also pay for that pointer density. The redesign targets scan cost and live size for the long-lived tree, not a micro-optimization of one visitor.

## What Store is

A `Store` owns one file’s syntax tree for one parse (or one emit rewrite). Nodes are dense `NodeRef` indices. `0` means missing. Callers that walk the tree use a stack `Handle` (`*Store` plus `NodeRef`). Heap maps and slices store `NodeRef`, not `Handle`, so they stay free of extra pointer words.

Each node is one packed, pointer-free header row (`kind`, `flags`, `pos`, `end`, `parent`, child range, optional text intern id). Children live in a packed `[]NodeRef` column. Variable-length lists use `ListRef` into the same column. After `Seal`, the construction-time intern map is dropped. Surviving node, list, and intern arrays contain no pointers, so Go can leave their backing stores unscanned.

Binder and checker data that must stay pointerful (`*Symbol`, and later flow and locals) sit in sparse maps keyed by `NodeRef`. Those maps are scanned on purpose. They are much smaller than putting a pointer on every header.

`Factory` (`store_factory.go`) allocates only into its owned `Store`. It returns `Handle` values. It does not build `*Node`.

## What we tried and rejected

**Column-per-field SoA** (separate `[]Kind`, `[]Flags`, …). Multi-field visits touched one cache line per column. Against a packed header, that layout lost on realistic walks. Packed header keeps noscan and keeps one line per node for kind+flags+span.

**Keeping `*Node` as the public type with Store underneath.** Two live representations share identity and lifetime. That doubles memory and hides the GC win. The target is one tree representation, not a façade.

**Putting `SourceFile.Text()` inside Store without a measured win.** See ownership below.

## Ownership boundaries

Store owns the **syntax tree**: headers, child edges, lists, and identifier/literal intern bytes used as node text.

`SourceFile` metadata stays outside Store: source text, diagnostics, line maps, hashes, and similar file-level state.

Identifier interning inside Store is allowed. That deduplicates node text. It is not the same as owning the file’s full source string.

A later optimization may store intern ids as offsets into `SourceFile` text instead of copying bytes into `internBuf`. Do that only after an A/B measurement shows a win on real compiles. Until then, do not move `SourceFile` text into Store.

## Identity

`NodeRef` is the Store-local identity. It replaces process-wide lazy `GetNodeId` for Store-backed trees.

Existing `NodeId` (`uint64` on `*Node`) stays for the pointer AST until that path is deleted. Do not treat `NodeRef` and `NodeId` as interchangeable types.

## Transforms and printers

Parse builds a Store and calls `Seal`. That Store is read-only afterward.

`printer.EmitContext` today allocates rewrite nodes through its own `NodeFactory`. The Store design keeps that split. Emit uses a **second** `Factory` / `Store`. `VisitEachChild` / `Update*` allocate into the emit Store. Needed subtrees are copied in and get new `NodeRef`s. A `Handle` from the parse Store must not be stored as a child of the emit Store (`refInStore` panics across stores). Slot reuse and in-place delete are out of scope for v1.

## Concurrency

One `Factory` / `Store` per file parse. Concurrent parsers do not share a Store.

## Migration order (backcast)

End state: the compile pipeline uses Store / `Handle` / `NodeRef` only. No long-lived `*Node` façade.

Working backward, each step requires the one above it to be true:

| Step | Must already be true |
| --- | --- |
| E. Delete `*Node` / old `NodeFactory` | D. Binder, checker, and LS use `Handle` |
| D. Binder / checker / LS on `Handle` | C. Printer and transformers read and rewrite Store |
| C. Printer / transformers on Store | B. Parser output is Store, and an emit `Factory` exists |
| B. Parser writes Store | A. Kind schema covers what the parser emits |
| A. Kind schema complete enough for parse | Current β slice (`Identifier`, `Token`, `BinaryExpression`, `List`) |

Run the work **forward** as A → B → C → D → E. Temporary bridges such as `FlattenNode` may exist inside a wave. Delete them in that wave. Do not leave a dual representation as the steady state.

## Current code

| Path | Role |
| --- | --- |
| `store.go` | `Store`, `NodeRef`, `ListRef`, `Handle`, walk, parents, symbol side map |
| `store_schema.go` | BinaryExpression slot layout |
| `store_factory.go` | Store-only `Factory` |
| `store_flatten.go` | Copy `*Node` → Store for measurement |
| `store_*_test.go`, `store_*_bench_test.go` | Unit, adversarial, and e2e benches |

The pointer AST in `ast.go` / `ast_generated.go` remains the production tree until migration B lands.

## How to re-check the layout bet

From `tsc/`:

```bash
go test ./internal/ast -run TestE2ELayoutReport -v
GOGC=off go test ./internal/ast -run TestE2ELayoutReport -v
go test ./internal/ast -run '^$' -bench E2E -count 3
```

`TestE2ELayoutReport` parses `checker.ts`, flattens into a Store, drops the pointer tree, and reports heap in-use and forced GC cost for both shapes. Use those numbers to argue about layout. Do not treat them as proof of file-text offset interning or of an end-to-end compile with a Store-native parser.

## Open implementation risks

- Emit Store must deep-copy and remap refs. That path is specified here and not implemented yet.
- Peak memory of one Store per concurrent file is not measured yet.
- Full Kind schema generation and parser wiring are the next large units (migration A, then B).
