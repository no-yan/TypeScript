# Bind-hot Store layout

How Binder should read the parse tree. This is an architect sketch, not a landed layout. Production `store.go` is still `[]NodeRef children`. Ship only after BindKPC (inst/op and cycles/op) and BindHot on `checker.ts`.

Arena: packed child records, kind-partitioned ident slabs, derived bind spine. Base is packed child records. Grounding: `why-store-bind-is-slower.md` (Instruments 2026-09-09) and `STORE.md`.

## Problem

Binder walks with `(NodeRef, Kind)`. Every named child and list element loads `children[i]` for the ref, then `nodes[ref].kind` for Kind. That is a second array gather on a header Bind already visits later for `FlagsAt`. Instruments puts Bind exclusive cost in `bindKind`, generated walkers, identifier helpers, and `ListSlotAt`, not in a 48% `KindAt` leaf.

Constraints that stay: 24-byte noscan `nodeHeader`; no `*FlowNode` / `*Symbol` in the header; emit shares unchanged parse `Handle`s; Binder does not name packing; no dual tree; no parse-time `PrepareBindTables` presize.

## Usage (caller's view)

Binder currency stays `(NodeRef, Kind)`. Root still uses `KindAt`. Child and list walks stop pairing `ChildRef`/`ListElem` with `KindAt`.

```go
func (b *Binder) bindRef(id ast.NodeRef, parentKind ast.Kind) bool {
    if id == 0 || b.store == nil {
        return false
    }
    return b.bindKind(id, b.store.KindAt(id), parentKind)
}

func (b *Binder) bindChildRef(parent ast.NodeRef, slot uint32, parentKind ast.Kind) {
    n := b.store.ChildBindAt(parent, slot)
    if n.Ref != 0 {
        b.bindN(n, parentKind)
    }
}

func (b *Binder) bindListRef(list ast.ListRef, parentKind ast.Kind) {
    s := b.store
    if list == 0 || s == nil {
        return
    }
    for i, n := 0, s.ListLen(list); i < n; i++ {
        elem := s.ListElemBindAt(list, i)
        if elem.Ref != 0 {
            b.bindN(elem, parentKind)
        }
    }
}
```

Functions-first two-pass compares `elem.Kind` from `ListElemBindAt`. Identifier `SetFlow`, `FlagsAt`, and `SetSymbol` stay on the header and bind columns.

Parser writes the packed slot at attach:

```go
s.linkChild(parent, 0, left)  // childRecords[slot] = {left.Ref, left.Kind}
s.SetListAt(members, i, stmt) // elem record {stmt.Ref, stmt.Kind}
```

Emit and checker keep `ChildRef`, `ListElem`, `KindAt`, and `Handle.childAt`. They do not call `ChildBindAt`.

## Shape

`[]NodeRef children` becomes `[]childRecord` (8 bytes, noscan): `{ref NodeRef, kind Kind}` plus padding to 8. The 24-byte `nodeHeader` is unchanged. Header `kind` is authoritative for `KindAt`, root `bindRef`, and Handle rebuild.

| Slot | Encoding |
| --- | --- |
| Named child | `{childRef, childKind}` |
| List element | `{elemRef, elemKind}` |
| List slot | `{NodeRef(listIndex), slotKindListRef}` or `ref==0` + `foreignLists` |

`ChildBindAt` / `ListElemBindAt` return `BindNode{Ref, Kind}` from one record. `bindKind` still `FlagsAt`s the header. `SetFlow` / `SetSymbol` / `SetFlagsAt` are unchanged.

Link-time invariant: for every node edge with `ref != 0`, `childRecords[k].kind == nodes[ref].kind`. Kind does not change after attach. `SetChild` / `SetListAt` rewrite the record. Debug/Seal checks the equality. List-slot sentinel never returns from `KindAt`.

Interface: two Store methods hide packing. Binder never names `childRecords` or the list-slot sentinel. Ref-only APIs stay for emit.

Deliberately not in this shape: ident slabs, bind spine, fused generated `bindKind`, Handle as walk currency, splitting kind/flags out of the 24-byte header.

## Synthesis decision

Base: **packed child records** (arena candidate 1). Cross-judge agreed: C1 pass on all six rubric rows; C2 weak on child-gather; C3 weak on CFG/order contract and Binder coordination.

C1 is the shape a maintainer can extend. Store owns layout. Binder receives `(Ref, Kind)` in one load. Parse copies kind at `linkChild`. Check/emit keep today's getters. The 24-byte packed header and Handle identity stay.

Rejected as the base:

- **Ident slabs** (candidate 2). Unchanged public API (deeper interface) but named child / list still `ChildRef` + `KindAt`. Encodes slab in `NodeRef` high bit, splits Freeze watermarks and `Len()`, and aims at ident cache lines rather than the walker gather Instruments named. Keep as a later orthogonal experiment only if packed slots do not move identifier-helper exclusive percent.
- **Derived bind spine** (candidate 3). Highest gather-reduction ceiling, but Binder must coordinate linear scan vs CFG, visit-order must match `bindwalk_generated` exactly, and a mismatch is a silent bind bug. `DropBindSpine` after bind is a good lifetime idea for *derived* data; packed slots are the tree, so they stay.

Grafted from losers:

- From C3: a Seal-time / test invariant that slot kind equals header kind (`TestChildRecordKindMatchesHeader`), and leave `bindConditionN` on `ChildRef`+Handle until a later Feature. Do not hoist functions-first visit order into a derive pass; two-pass over packed list records is enough for the first experiment.
- From C2: do not put slab bits in `NodeRef`. `Len()` remains a dense `NodeRef` domain. If ident locality is still the story after C1 measures, route `KindIdentifier` only through a private ident column without changing child packing.

## Tradeoffs accepted

- We accept ~4 extra bytes per named-child slot, list slot, and list element for the whole compile (Check/emit included) in exchange for removing one header gather per Bind child and list elem.
- We accept duplicated kind (header authoritative, slot copied at link) in exchange for stable `KindAt` and Handle identity without a Binder cache.
- We accept an internal list-slot sentinel inside Store in exchange for one contiguous slot column.
- We accept two new Bind readers in exchange for not widening emit's ref-only `ChildRef` / `ListElem`.

## Alternatives considered

- **Binder-local Kind cache.** Overlay, not storage. Wall A/B on functions-first already flat.
- **Column-per-field `[]Kind` beside `[]NodeRef`.** Same extra bytes as packed records, two distant loads. Lost to fused 8-byte records.
- **Ident slab as the Bind layout.** Wrong first bottleneck; `NodeRef` encoding tax.
- **Bind spine as the Bind layout.** Second visit-order contract; CFG double-bind risk.

## Open questions and risks

- Does +4B×slot count on checker.ts / monaco eat the BindKPC win, or does live heap / GC on `--noCheck` veto the shape?
- Should `Handle.childAt` read slot kind later, or keep header kind only?
- Is `slotKindListRef = -1` safe against every `Kind` value, or does the list slot need a flag word?
- KindAt hoist did not move Bind wall. Packed slots might not either. The experiment must be allowed to reject the shape.

## Measurement (2026-09-09)

Hypothesis: packing Kind next to each child/list-elem ref removes the Bind `KindAt` gather and therefore cuts checker.ts BindHot wall and BindKPC inst/op and cycles/op.

Overlay: `/tmp/bind-c1-verify/` (`store.go`, `store_factory.go`, `binder.go`, `bindwalk_generated.go`). `go test` `./internal/ast` `./internal/binder` `./internal/parser` passed. Production tree unchanged. Root via `osascript` admin dialog (`/tmp/run-kpc-c1.sh`). `KPERF_TESTDATA=/tmp/kperf-testdata`, `GOGC=off`.

### BindHot (wall)

Interleaved, n=6, benchtime 20x, Apple M1:

| | base sec/op | pack sec/op | p | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: |
| default GOGC | 16.38 ms ± 5% | 16.16 ms ± 5% | 0.180 | flat | flat |
| `GOGC=off` | 16.21 ms ± 4% | 15.99 ms ± 4% | 0.485 | flat | flat |

Point estimate about −1.3%. Not significant. Bind B/op unchanged (`children` grows at parse, outside the Bind timer).

### BindKPC

Same-binary sanity (base, 30x, two runs): 188.64M → 189.11M inst/op (**+0.25%**, under 1%).

A/B interleaved n=3, benchtime 30x. Medians:

| | base | pack | delta |
| --- | ---: | ---: | ---: |
| inst/op | 188.6M | 184.4M | **−2.2%** |
| cycles/op | 91.49M | 88.52M | **−3.2%** |
| IPC | 2.059 | 2.082 | higher |
| sec/op (kpc timer) | 45.80 ms | 43.01 ms | −6% (n=3, p=0.200) |

benchstat reports `~` on all rows (n=3). Point estimates meet the ship rule that **both** inst/op and cycles/op fall, with IPC not worse. BindHot wall still does not move.

**Do not land.** A ~2% Bind mutator instruction cut that does not show on BindHot is not worth +4 B per child/list slot for the whole compile (Check/emit live heap). Same class of result as the KindAt hoist A/B: the gather is real in kpc and invisible on wall.

## Next implementation step

Do not land packed `childRecord`. The next Bind layout bet is not another Kind gather cut. Revisit ident locality or leftover Handle CFG only with a new hypothesis that targets BindHot exclusive leaves (`bindKind`, identifier helpers, CFG), not child Kind loads.

## Type sketch

```go
type BindNode struct {
    Ref  NodeRef
    Kind Kind
}

type childRecord struct {
    ref  NodeRef
    kind Kind // header kind is authoritative; this is the Bind copy
}

func (s *Store) ChildBindAt(parent NodeRef, slot uint32) BindNode
func (s *Store) ListElemBindAt(list ListRef, i int) BindNode
func (s *Store) ChildRef(parent NodeRef, slot uint32) NodeRef // emit; ignores slot kind
func (s *Store) KindAt(id NodeRef) Kind                       // header
```

Arena working copies (not in git): `/tmp/arena-bind-store-layout/`.
