# Why Store Bind is slower than pointer Bind at 8ac035a

This explains the Bind mutator gap between this Store tree and pointer-AST `tsc` at `8ac035a394`. The numbers come from `parse-bind-vs-8ac035a-report.md` (2026-09-09, Apple M1). How to rerun them is `parse-bind-vs-8ac035a.md`.

The reader is a maintainer who must decide whether Bind cost is a layout tax they can keep paying for GC scan wins. This page is not a how-to and not a BindKPC runbook.

## Adoption criterion

Prove Store Bind faster than pointer Bind at `8ac035a`. Land only **Store-specific** fixes (columns, NodeRef/Handle tax, arena, Freeze, packed layout). Do **not** land binder/parse opts the pointer tree can mirror (shared algorithm wins shrink both sides).

## Overview

Store Bind does the same symbol and control-flow work as parent Bind. It walks a packed `NodeRef` tree instead of `*ast.Node` pointers. On `checker.ts`, that walk retires about 70% more instructions and about 58% more cycles than parent, and wall time is about 53% higher. IPC is not worse. The extra cost is more work per node, plus bind-time column allocation, not a slower pipeline.

CLI `Bind time` is a different clock. After `BindSourceFile`, Store `Program.BindSourceFiles` also `Freeze`s each parse Store. Parent has no Freeze. Cite algorithm speed only from `BenchmarkBindKPC` or `BenchmarkBindHot`.

empty.ts is nearly even. The tax shows on large files. That pattern matches per-node work. A fixed per-file tax alone would show up on empty.ts too.

## Key concepts

**Parent Bind.** `BindSourceFile` calls `bind(*Node)`. Recursion currency is a pointer. Kind, flags, parent, children, and many FlowNode slots live on the node or its `nodeData`. `ForEachChild` follows those pointers. `Program.BindSourceFiles` only binds.

**Store Bind.** `BindSourceFile` calls `bindRef` on the parse root. Recursion currency is `(NodeRef, Kind)`. Syntax rows are 24-byte noscan `nodeHeader`s. Symbol and Flow live in Store columns and maps. Children live in a packed `children` slice. `Handle` is a 16-byte stack value (`*Store`, `NodeRef`, cached `Kind`) rebuilt when leftover helpers still take a Handle.

**BindKPC.** Thread kpc around `BindSourceFile` only, `GOGC=off`, fresh parse outside the timer. Counts user and kernel instructions on that thread. Does not count other-thread GC. Does not include Freeze.

**BindHot.** Wall time of `BindSourceFile` on a fresh parse, default `GOGC`. Same function as BindKPC. Includes `PrepareBindTables` and column writes. Does not include Freeze.

**CLI Bind.** Compiler `Bind time` for `BindSourceFiles`. Store adds a second workgroup that `Freeze`s every parse Store.

## How pointer Bind walks

Entry is `bindSourceFile` in parent `tsc/internal/binder/binder.go`. It pools a `Binder`, then `bind(file.AsNode())`.

`bind` switches on `node.Kind`. Identifier writes `node.AsIdentifier().FlowNode = b.currentFlow`. That is a field store on a node the parser already allocated. After the declaration switch, non-tokens call `GetContainerFlags` and either `bindChildren` or `bindContainer`. Children are `*Node` fields or slices. The binder already holds the parent pointer, so Kind and flags are loads from that object.

There is no bind-time table the width of the file. Flow pointers sit on the kinds that need them. Symbol tables are still maps.

## How Store Bind walks

Entry is `bindSourceFile` in this tree's `tsc/internal/binder/binder.go`. After `ParseTreeRef`, it calls `PrepareBindTables`, then `bindRef(root, KindUnknown)`.

```text
BindSourceFile
  PrepareBindTables          // grow symbolIdx and flows to len(nodes)
  bindRef(root)
    KindAt(root)             // nodes[id].kind
    bindKind(id, kind, parentKind)
      kind switch            // declare / SetFlow / some HandleOf
      FlagsAt
      bindChildrenRef or bindContainer
        CFG kinds: dedicated *Ref walkers
        else: forEachBindChildGenerated
          ChildRef / ListSlotAt
          bindChildRef -> KindAt -> bindKind
```

`forEachBindChildGenerated` (`bindwalk_generated.go`) is a kind switch that emits one `ChildRef` or `ListSlotAt` per schema slot. The `default` arm still calls `bindChildrenOf`, which loops `NumChildrenAt` and `NumListSlotsAt`.

CLI then runs Freeze in `compiler/program.go` `BindSourceFiles`, after every file is bound.

## Why the mutator retires more instructions

Layer A, `GOGC=off`, median of 3, 30x, `checker.ts`.

| | Parent | Store | delta |
| --- | ---: | ---: | ---: |
| inst/op | 111074369 | 188791949 | +70.0% |
| cycles/op | 62313059 | 98303072 | +57.8% |
| IPC | 1.782 | 1.921 | higher on Store |

Layer B BindHot `checker.ts` is +53.41% sec/op (p=0.000, n=20). `GOGC=off` BindHot is 13.31 ms versus 20.97 ms. GC in the Bind timer is not the story. The mutator is.

The same Layer A row has **higher** IPC on Store. The CPU retires more instructions. It is not stalled more per instruction. Parse on the same file is only +7.7% inst. Store accessors are not 70% slower on every phase. Bind is the phase that writes columns and reloads headers on every child.

empty.ts BindKPC is +2.4% inst. The extra work scales with tree size.

### Per-node walk tax

Parent recursion is "follow this pointer, the Kind is already on it". The pointer `Node` and its payload share one allocation. Child `*Node` fields sit on that same object. Store recursion is index math plus a header load. Kind lives in `nodes[id]`. The child id lives in `children[childStart+slot]`. The child's Kind is a second header load. That is a two-array gather. Parent Bind reads Kind two or three times as well (`bind`, `GetContainerFlags`, `ForEachChild`), but those hits stay on one `*Node` already in L1.

On every child, `bindChildRef` calls `KindAt` even when the parent walk already knew the slot. `bindRef` at the root does another `KindAt`. `ChildRef` itself is O(1). The kind switch is the generated walker, not `ChildRef`.

`bindKind` is a large switch, then `FlagsAt`, then another switch in `bindChildrenRef` for CFG. Parent has the same two-level structure. Store pays it with Kind and flags reloaded from `nodes[id]` instead of registers on `*Node`.

Identifier and binary paths no longer build a Handle in `bindKind` (`ed27b179c6`). Tokens still call `SetFlow`. `SetFlow` is `mustMutate`, `flowID`, and `putCol` into a dense `[]uint32`. Parent assigns a pointer field on the identifier node. `flowID` has a last-flow fast path, then an address check against the arena. That is extra integer work on a very hot kind.

`mustMutate` is a phase load and compare on every bind write (`SetFlagsAt`, `SetSymbol`, `SetFlow`, `PrepareBindTables`). Parent has no freeze check.

### Handle rebuilds that remain

`HandleOf` is a stack struct. It is cheap next to a heap `*Node`. It is still work the parent does not do. Each call feeds helpers that still take a Handle.

`binder.go` still has 42 `HandleOf` call sites. They sit on strict-mode checks, some declarations, JS `CallExpression`, `ModuleDeclaration`, flow payloads (`createFlowCondition` and friends), `isNarrowableReference`, and diagnostics. `bind-instruction-reduction.md` lists the leftover edges and treats NodeRef-only currency as later work, not this landing.

Those sites are not the whole +70%. Identifiers skip them. They add a second currency on CFG and helper paths, which checker.ts has in volume.

If and while conditions still call `bindConditionN`, which builds a Handle through `payload` and re-enters `bind(Handle)`. `createFlowCondition` and `isNarrowableReference` stay Handle-only. The walk is NodeRef-first until CFG needs a predicate the AST helpers never ported.

The generated-walker landing (`63702a923a`) stated the leftover tax in writing. `mustMutate`, flow-id, and remaining `HandleOf` were left untouched on purpose. A later identifier and binary skip (`ed27b179c6`) cut checker.ts BindKPC from 201.6M to 188.5M inst/op. That is most of the way from the older ~195M snapshot to the report's 188.8M. It is not most of the way to parent's 111M.

### Bind-time columns

`PrepareBindTables` runs inside the BindKPC and BindHot timers. It grows `symbolIdx` and `flows` to `len(nodes)` and seeds `symbolRefs`. That is two node-width `[]uint32` allocations, zeroed, then touched as Bind writes. checker.ts is 302.6k nodes (`bind-column-presize-bench.md`), so those two columns are about 2.4 MiB. BindHot B/op still rises by about 5.1 MiB versus parent. Flow chunks (about 80k `FlowNode`s at 48 bytes) and `[]Handle` on symbols make up the rest of the byte gap. Columns are not the whole extra.

Moving that make to `NewStore` was measured and rejected. Bind CPU did not fall. Parse got slower and live heap grew because `len(sourceText)/5` over-reserves. Exact-size `PrepareBindTables` stays.

`Compact` and `internIdx` are parse. Bind does not intern. Do not put intern lookups on the Bind bill.

Parent Bind does not allocate file-width columns. It writes into parse nodes.

kpc fixed counters include EL1. First touch of those slices can count page faults and kernel work against Store Bind only. `kperf-bind-measurement.md` calls this out. It inflates Store inst/op relative to a pure user-mode comparison. It does not invent the wall-time gap. BindHot with default GOGC still shows +53% on checker.ts.

`SetSymbol` is an indexed pointer table (about one node in eight has a Symbol, per `STORE.md`). Lookup is `getCol` plus `symbolRefs[idx]`, not `node.Symbol()`. Sparse maps remain for locals, endFlows, returnFlows, nextContainer.

### What is still a pointer graph

`FlowNode` is still 48 bytes with Handle and pointer antecedents (`STORE.md`). `NewFlow` allocates in chunked Store arenas. The `flows` column stores ids, not pointers, so the node header stays noscan. The binder still chases `*FlowNode` the way parent does. Flow layout is not a Bind win today. It is extra mapping (`SetFlow` / `flowID`) around the same CFG object graph.

Symbol and `SymbolTable` are still pointer maps. BindHot allocs/op on checker.ts is +1.51% (13.95k to 14.16k). The binder is not allocating many more objects. It is allocating **larger** backing stores.

## Why BindHot allocates more bytes

Layer B `checker.ts`.

| | Parent | Store | delta |
| --- | ---: | ---: | ---: |
| sec/op | 13.20 ms | 20.26 ms | +53.41% |
| B/op | 7.081 MiB | 12.207 MiB | +72.38% |
| allocs/op | 13.95k | 14.16k | +1.51% |

Bytes up, allocation count almost flat. That matches `PrepareBindTables` and flow-chunk growth, not a new object per node.

Parse on the same file **drops** B/op 48% and allocs 92%. Bind then spends some of that back on columns. The Store bet is still fewer scannable pointers in the long-lived tree. Bind is the phase that materializes the extra noscan columns.

`dom.generated.d.ts` BindHot is +50.89% time and +49% B/op, same shape. empty.ts BindHot time is not significant (p=0.218).

## What CLI Bind time includes

monaco `--noCheck` Bind time +56% (0.041 s to 0.064 s). vscode check-on Bind time +29% (0.261 s to 0.336 s). Those clocks include Freeze.

Freeze (`store.go`) sets phase check, records `frozenAt`, drops `internIdx`, and allocates `subtreeFacts` as `[]uint32` the width of `nodes`. It is `sync.Once`. It is not in BindKPC. A CLI Bind regression that is smaller than BindHot (vscode +29% versus BindHot +53%) is consistent with parallel bind plus Freeze amortized across 9733 files, and with parse already having Compacted. Do not treat CLI Bind as a tighter bound on the algorithm than BindHot.

Parent `BindSourceFiles` has no second pass.

## Where things live

| Piece | Path |
| --- | --- |
| Store Bind entry, `bindKind`, CFG | `tsc/internal/binder/binder.go` |
| Generated child walk | `tsc/internal/binder/bindwalk_generated.go` |
| `PrepareBindTables`, `KindAt`, `ChildRef`, `HandleOf`, `SetFlow`, Freeze | `tsc/internal/ast/store.go` |
| CLI Bind plus Freeze | `tsc/internal/compiler/program.go` `BindSourceFiles` |
| Parent Bind | `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript` `tsc/internal/binder/binder.go` at `8ac035a394` |
| Accepted walk design | `tsc/internal/binder/docs/bind-instruction-reduction.md` |
| kpc method and EL1 caveat | `tsc/internal/binder/docs/kperf-bind-measurement.md` |
| Layout vocabulary | `tsc/internal/ast/STORE.md` |
| Filled numbers | `tsc/internal/ast/docs/parse-bind-vs-8ac035a-report.md` |
| Column presize reject | `tsc/internal/ast/docs/bind-column-presize-bench.md` |
| checker.ts node and flow counts | `tsc/internal/ast/docs/flow-index-column-bench.md` |

## What to work on first

The Bind mutator has **no single 48% function**. Instruments CPU Profiler (cycle-weight, Running samples, stacks that contain `BindSourceFile` / `(*Binder)` and not `(*Parser)`) puts the cost in the walk itself: `bindKind`, the generated children walkers, identifier helpers, Store slot/column loads, and map lookups. `KindAt`, `HandleOf`, `mustMutate`, and `SetFlagsAt` do not appear as named leaves (inlined or too small). `Freeze` is about 0.3% of monaco `--noCheck` cycle-weight. `NewFlow` is about 1% exclusive of BindHot bind samples.

That ranking is Store self-cost. **Paired** `8ac035a` CPU Profiler (2026-09-09T2117) is in `bind-paired-cpu-profiler-8ac035a.md`. Bind cycle-weight Store/Parent ≈ **1.73×**. Store-only leaves (`ListSlotAt`, `ListElem`, `flowID`, `Handle.childAt`, …) are each ~1–3% of Store bind; the gap is the **walk currency** (`bindKind` / generated children / Ref helpers), not one micro-site.

### Instruments (2026-09-09)

Artifacts: `.cursor/skills/verify-tsc/artifacts/20260909T1849-cpu-profiler/`. Weight is Instruments `cycle-weight`, not sample count. Summarizer: `.cursor/skills/verify-tsc/scripts/summarize-xctrace-cpu-profile.py`.

**monaco** (`src/tsconfig.monaco.json`, `--noEmit --noCheck --singleThreaded`, embed `tsc`). CLI clocks: Parse 0.593s, Bind 0.148s, Total 0.780s. Whole-process cycle-weight: bind-classified 30.7%, GC 29.6%, parse-classified 23.0%, other runtime 15.8%. For this CLI, parse plus GC still dominate the process. Bind is the next cluster, not a rounding error. Freeze is not the Bind clock.

Bind-classified exclusive leaves (percent of bind cycle-weight):

| % of bind | Leaf |
| ---: | --- |
| 10.7 | `(*Binder).bindKind` |
| 4.7 | `forEachBindChildGenerated` |
| 4.6 | `bindListRef` |
| 4.0 | `bindChildrenRef` |
| 3.1 | `aeshashbody` |
| 3.0 | `checkContextualIdentifierRef` |
| 3.0 | `nameRefGenerated` |
| 2.9 | `(*Store).ListSlotAt` |

**BindHot `checker.ts`** (40x). Do not read whole-process percents as Bind. The recording includes parse and `runtime.GC()` each iteration: GC 69.7%, parse 18.2%, bind 11.8%. Bind-classified exclusive:

| % of bind | Leaf |
| ---: | --- |
| 11.8 | `(*Binder).bindKind` |
| 5.1 | `checkContextualIdentifierRef` |
| 3.9 | `bindChildrenRef` |
| 3.9 | `aeshashbody` |
| 3.6 | `forEachBindChildGenerated` |
| 3.3 | `mapaccess1_faststr` |
| 2.6 | `addAntecedent` |
| 2.0 | `ListSlotAt` |
| 1.9 | `flowID` |
| 1.9 | `bindChildRef` |
| 1.1 | `(*Store).NewFlow` |

Inclusive (frame anywhere on a bind-classified stack), BindHot: `bindKind` / `bindChildrenRef` / `bindChildRef` ~99%; `bindConditionN` 23%; `ChildRef` 49%. High inclusive on `ChildRef` and `bindEachStatementFunctionsFirstRef` means they sit on the spine of the walk, not that they are exclusive hot. Exclusive `bindChildRef` is 1.9%.

Two captures agree on the exclusive shape: `bindKind` first, then walkers plus identifier helpers plus hash/maps plus Store slot loads. monaco has more `bindListRef`; checker.ts has more CFG (`addAntecedent`, `bindConditionN` inclusive).

### pprof KindAt 48% (rejected)

Do not treat Go CPU pprof flat percent as the Bind share. `StopTimer` does not stop the profiler, so BindHot profiles are mostly parse GC. Even inside `BindSourceFile`, inlined `KindAt` was attributed as 48% of bind samples. Instruments names `KindAt` at 0% exclusive. On this machine pprof has also blamed `runtime.madvise` at ~30% when Instruments showed ~3%.

A throwaway A/B on 2026-09-09 cached Kind once per statement in `bindEachStatementFunctionsFirstRef` (local `[]NodeRef` + `[]Kind`, still two bind passes, nested-safe). BindHot `checker.ts`, interleaved, n=6, benchtime 20x. Scratch overlay: `/tmp/bind-p0-verify/`.

| | base sec/op | cache sec/op | p | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: |
| BindHot checker.ts | 17.60 ms ± 3% | 17.50 ms ± 1% | 0.699 | +0.26% | +2.15% |

Cutting the extra `KindAt` on the hoist path did not move Bind wall time. P0 as "48% KindAt" is a pprof artifact.

### Rank after Instruments

| Priority | Attack | Status |
| --- | --- | --- |
| — | Hoist `KindAt` cache | Rejected. Wall A/B flat. Instruments exclusive 0% |
| P0 | Packed child records: store `{NodeRef, Kind}` per slot (`ChildBindAt` / `ListElemBindAt`) | Rejected. BindHot flat (p=0.180). BindKPC median inst −2.2%, cycles −3.2% (n=3). Not worth +4 B/slot for whole compile. See `bind-store-layout-design.md` |
| P1 | Identifier helpers (`checkContextualIdentifierRef`) | Upper bound real (−15% BindHot) but **not Store-specific**. MayBeReserved measured (−7.7% inst) then **reverted** — pointer Bind can take the same flag. See `bind-contextual-ident-design.md` |
| P2 | If/While NodeRef (`bindConditionN` / leftover Handle CFG) | 23% inclusive; exclusive spread. PropertyAccess `HandleOf`+narrowable skip upper bound: BindHot flat (p=0.382), BindKPC inst **−1.9%** — not a Bind wall lever alone |
| P3 | Store slot/column loads (`ListSlotAt`, `ListElem`, `flowID`, `SetFlow`, `Handle.childAt`) | `SetFlow` full no-op ceiling **−5%** (earlier). `flowIDBind` (skip arena address check + mustMutate): BindHot flat, inst **−1.4%** — reject. `mustMutate` no-op: BindHot flat — reject. Remaining tax is the column write / walk density, not the guards |
| P4 | Extra map lookups beyond parent `SymbolTable` | `aeshash` + `mapaccess*` ~13–15% exclusive. Parent Bind also hashes. Do not cut maps first without a parent Instruments diff |
| — | `NewFlow` / `memclr` | ~1% exclusive. Demoted. Not the first Feature |
| — | `SetFlagsAt` | Not a named Instruments leaf. Demoted |

**Do not start with these.**

| Item | Evidence |
| --- | --- |
| `PrepareBindTables` at parse (`BindColumnHintPct`) | Bind CPU flat, parse slower, live heap +10–14% (`bind-column-presize-bench.md`). Instruments inclusive ~0.3% of monaco bind |
| More `HandleOf` deletion on identifier and binary | Already landed (`ed27b179c6`, 201.6M → 188.5M inst). `HandleOf` not a named leaf now |
| Freeze / `subtreeFacts` as Bind algorithm | Not in BindKPC or BindHot. monaco Freeze ~0.3% of process cycle-weight |
| Treating monaco `--noCheck` whole-process CPU as Bind | Parse + GC are larger. Bind is ~31% of cycle-weight on this capture |
| Fused generated `bindKind` | Rejected in `bind-instruction-reduction.md` |
| Ranking work from BindHot `-cpuprofile` flat percent | pprof `KindAt` 48%. Instruments 0%. Wall A/B did not move |
| Attacking `ChildRef` because inclusive is ~49% | Exclusive `bindChildRef` ~2%. Spine of the walk |
| Ident slabs or a derived bind spine as the first Bind layout | Arena losers. Gather stays (slabs) or CFG visit-order is a silent bind bug (spine). See `bind-store-layout-design.md` |
| Landing packed `{NodeRef, Kind}` child slots | BindHot flat; BindKPC ~−2% inst. Same class as KindAt hoist |
| Binder intern keyword map cache | BindHot flat; B/op +1%. See `bind-ident-contextual-bench.md` |
| Skipping `checkContextualIdentifierRef` entirely | Upper bound only (−15% BindHot). Breaks reserved-word diagnostics; also not Store-specific |
| `flowIDBind` / skip `mustMutate` on SetFlow | Store-specific; BindHot flat; inst ≤2% — reject |
| `mustMutate` no-op (all bind writes) | Store-specific; BindHot flat — freeze check is not the wall |
| PropertyAccess skip `HandleOf`+narrowable | Store-specific Handle tax; BindHot flat; inst −1.9% — reject as sole Feature |
| NodeRef `isNarrowingExpression` (createFlowCondition + property SetFlow) | Store-specific Handle→ChildRef; BindHot ~ (p=0.161); BindKPC inst **−2.1%**, cycles **−2.8%** — reject as sole Feature; full CFG Handle purge still open but local win is small |

The +70% inst versus `8ac035a` is still real (kpc). Reproduce Instruments:

```sh
./.cursor/skills/verify-tsc/scripts/control-tsc launch --embed
VERIFY_TSC_RUN_ID=… ./.cursor/skills/verify-tsc/scripts/control-tsc pmc \
  --cwd /Users/noyan/ghq/github.com/microsoft/vscode \
  --slug monaco-cpu-profiler --template "CPU Profiler" -- \
  --project src/tsconfig.monaco.json --noEmit --noCheck \
  --declaration false --singleThreaded --extendedDiagnostics
xctrace export --input <trace> --xpath '/trace-toc/run/data/table[@schema="cpu-profile"]' \
  > cpu-profile.xml
python3 .cursor/skills/verify-tsc/scripts/summarize-xctrace-cpu-profile.py \
  --xml cpu-profile.xml --out cpu-profile-summary.txt
```

Hoist A/B (already rejected):

```sh
# cache overlay already at /tmp/bind-p0-verify/overlay.json
go test -C ./tsc -c -o /tmp/bind-p0-verify/base.test ./internal/binder
go test -C ./tsc -overlay /tmp/bind-p0-verify/overlay.json -c -o /tmp/bind-p0-verify/cache.test ./internal/binder
# interleave BenchmarkBindHot/checker.ts, then benchstat
```

## Gotchas

- WarmJSDoc is gone from Store Bind. Parent `8ac035a` also defers TS JSDoc without `@see` or `@link`. Do not blame Bind on JSDoc parse.
- This worktree is about 200 commits past `8ac035a`. The comparison is this fork versus that pointer parent, not a one-commit A/B.
- BindKPC IPC going **up** is not a Bind win. The ship rule in `kperf-bind-measurement.md` requires both inst/op and cycles/op to fall.
- `HandleOf` skipping on identifier and binary does not make Bind even. Those kinds still `SetFlow`.
- monaco Check +14% and vscode Check −43% are not Bind claims. Flow, Symbol, and SymbolTable are still pointer structures.
- `kperf-bind-measurement.md` still says the 2026-09-09 generated walker was rejected. `63702a923a` later landed `forEachBindChildGenerated` with a smaller kpc win. Trust the code and the 8ac035a report, not that one sentence.
- BindHot `-cpuprofile` includes parse and `runtime.GC()`. Always focus `BindSourceFile`.
- BindHot CPU Profiler has the same mix: classify stacks by `(*Binder)` / `BindSourceFile` versus `(*Parser)`, then ignore the GC bucket.
- pprof flat percent on this machine can disagree with Instruments (`madvise` ~30% versus ~3%). Do not rank Bind work from pprof alone.
- CPU Profiler XML `cycle-weight` is the sample weight. Do not treat row count as time.
