# Concurrent Store reads and private generation Stores

## Problem and selected checkout

Selected repository: `/Volumes/SanDisk1TB/worktree/lock-profile`.
Current base: `3e0eb48bd4a536a0cafbfade9106df44120381b6`, including merge
`243311ee7b` and its qualified-ListRef indexing fix. TSGolint is not involved.
The entry changes in `checker.go` and `links.go` are preserved and included in
verification. They change checker link-map keys and are distinct from registry lookup.

At the original task entry (`8067b4b419`), StoreSet.Store took an RWMutex on
every lookup. Parallel readers updated one shared counter. Independently, emit
appended into parse Store slices that other files could still read. Removing
only the registry's read lock cannot make those slice accesses safe.

Prior measurements and test logs in `.audit/store-concurrency/` are `stale`
for the selected HEAD. The old `/private/tmp/lock-monaco-20260905` artifact
set was `missing`. Candidate clones include `cursor-ast-store-tests`,
`lock-design-inv`, `store-redesign`, `store-nolock-exp`, and the main
`no-yan/TypeScript` checkout; the complete worktree inventory is saved in
`.audit/store-concurrency-3e0e/candidate-clones.txt`.

## Architecture

- **Registry:** immutable directory, stable fixed-size pages, atomic Store
  slots. Readers never update a shared counter. A writer mutex serializes
  registration and removal. Geometric directory growth avoids copying the
  whole registry on every registration.
- **Publication:** publish the slot and identity domain before the Store ID.
  Concurrent RegisterStore is idempotent. Foreign adoption is rejected;
  foreign Remove cannot clear another Store's slot. IDs are never reused.
- **File tree:** atomically publish an immutable Store/root/Kind tuple.
  ParseStore, ParseTreeRef and ParseRoot require no read lock. ParseRoot does
  not reload the owner's node slice header.
- **Hot declarations:** Symbol.Declarations and ValueDeclaration retain
  Handles, using the change from `3bf9e00dfd`. GlobalRef remains useful for
  identity keys; navigating a declaration no longer needs the registry.
- **Foreign edges:** retain a Handle for an external parent, child, or list
  element. The Store containing the edge is the only writer. Attachment does
  not copy or reparent the target, and existing Handles retain its lifetime
  even if its registration is removed.
- **Lists:** ListRef qualifies a 32-bit list index with its owner StoreID.
  Same-store node slots still contain only 32-bit indexes. Sparse foreign
  list slots and owner references preserve unchanged list identity across
  transforms. NodeSeq resolves and retains the actual owner once. Raw
  indexing must decode the low 32 bits; ListRefs and ListRefAt include the
  post-merge correction and a regression test for a foreign receiver.
- **Emit:** allocate in the context's private Store. Freeze is irreversible;
  EnterEmit and LeaveEmit are removed. Program.Emit follows SingleThreaded
  rather than forcing serial execution.
- **Metadata:** clone SourceFile wrappers without rebinding the shared parse
  Store. EmitContext.SourceFileOf resolves the current output view, while
  semantic readers retain the program file. OriginalSourceFile provides the
  canonical identity for view comparisons.
- **Original nodes:** cross-store DeepCloneNode invokes OnClone for every
  copied node, including descendants. This lets MostOriginal recover the
  bound parse declaration. Without it, the CommonJS export getter for an
  imported name used `createDog` instead of `dog_1.createDog`.
- **Reset:** a pooled EmitContext unregisters its old Store and installs a
  fresh one. Previously returned Handles do not have their metadata owner
  changed to the next emitted file.

Explicit CopySubtree retains its structural-copy contract. It does not acquire
emit original-node metadata unless invoked through DeepCloneNode. Speculative
Restore removes foreign edges belonging to discarded nodes and list slots,
so reused indexes cannot expose old references.

## Ownership and allocation costs

Registration does not synchronize subsequent AST writes. Build has one writer;
existing parse/bind/check barriers publish frozen trees. A foreign target must
be frozen or managed by the same writer. A private generation Store is not a
concurrent mutable container.

Handles and owner maps add GC-visible pointers. The expected benefit is fewer
registry lookups and avoided subtree copies; memory and GC effects must be
measured. The 24-byte nodeHeader and 32-bit packed children remain pointer-free.
Registry high-water marks do not shrink. Evicting obsolete parse Stores during
LS edits remains a separate lifetime-management problem.

## Reproduction and acceptance

Current-run metadata, entry diff, raw output and results belong to
`.audit/store-concurrency-3e0e/`. An artifact is `current` only when repo_root,
tsgolint_git_rev and typescript_go_git_rev match the selected checkout; record
working-tree diffs separately. Use `stale` for other revisions, `missing` for
uncollected output, and `unsupported` when bench.txt exists but has no matching
Benchmark line. Historical comparison baselines retain their actual revisions.

```sh
GOMAXPROCS=2 go test -p 1 ./tsc/internal/ast ./tsc/internal/printer ./tsc/internal/compiler
GOMAXPROCS=2 go test -p 1 -race ./tsc/internal/ast ./tsc/internal/printer ./tsc/internal/compiler

go test ./tsc/internal/ast -run '^$' \
  -bench '^(BenchmarkStoreSetReadParallel|BenchmarkParseRootParallel)$' \
  -benchmem -cpu 1,4 -count 6

STORE_BENCH_PROJECT=/path/to/vscode/src/tsconfig.monaco.json \
  go test ./tsc/internal/compiler -run '^$' -bench '^BenchmarkStoreMonaco$' \
  -benchmem -benchtime=1x -count=6
benchstat old.txt new.txt
```

Build test binaries sequentially to avoid the memory exhaustion encountered
when multiple large AST builds ran together. Run timing measurements without
race instrumentation or concurrent builds. Preserve raw data and use benchstat.

Functional acceptance covers registry publication/domain/removal, cross-store
identity and list sharing, irreversible Freeze, unchanged parse trees during
parallel emit, and identical serial/parallel JS, d.ts and source maps. Clone
original-node recovery has both a descendant test and an end-to-end CommonJS
regression test.

The real-workload benchmark includes config loading, parse, bind, check and
close for one fresh Program; it excludes CLI process startup. Go reports ns/op,
TotalAlloc-based B/op and Mallocs-based allocs/op. CLI wall and Check time are
separate measurements. Microbenchmarks isolate read-path cost and do not prove
a real-workload bottleneck. Report hot paths, allocation drivers, diagnosis and
next action with the measured results; do not claim speedups before measurement.

## Remaining conformance issue, isolated from the clone fix

`TestLocal/alias` still has type/symbol baseline failures. Repeating the suite
with the production clone changes replaced by their exact `3e0eb48bd4` versions
using a Go overlay produces the same failing test set. Both runs have no
frozen-Store write panic. Logs and the comparison are stored as
`alias-conformance.txt`, `alias-at-head.txt` and `alias-comparison.json`.

- **Problem:** type/symbol results can move backwards in source-line order;
  `iterateBaseline` then attempts an invalid slice range. Other cases merely
  reorder output, for example `B` before parameter `name` in
  `aliasOnMergedModuleInterface.types`.
- **Evidence:** both variants fail at `type_symbol_baseline.go:225`; the
  baseline walker uses Handle.ForEachChild, whose packed-field iteration
  visits named children before list children.
- **Reproduction:** `GOMAXPROCS=2 go test -p 1 ./tsc/internal/testrunner -run
  '^TestLocal$/alias' -count=1`. The saved overlay reproduces the pre-fix variant.
- **Hypothesis:** grouping children by storage column loses the syntactic order
  between parameter lists and return types, for example.
- **Proposed follow-up:** generate ordered child traversal from the AST schema,
  maintaining early termination and allocation-free iteration. Do not hide the
  ordering defect by accepting changed reference baselines.
- **Acceptance:** the alias type/symbol baselines match without slice panics,
  and traversal-order tests cover mixed named-child/list-child nodes.

This issue is not counted as a passing conformance gate. Core AST, printer and
compiler tests pass with and without race instrumentation for the current
implementation; see the current artifact logs for the full verification scope.
