# BCE hot-path survey (ast / parser / binder)

Survey of Go bounds-check elimination opportunities on Store parse/bind
hot paths. Method: `-gcflags=-d=ssa/check_bce/debug=1`, isolated pattern
microbenches, then BindHot `checker.ts` A/B. Date 2026-09-09, Apple M1,
Go 1.26.6.

## Where checks remain

| Package | Remaining `IsInBounds` / `IsSliceInBounds` (approx) | Hot leaves (Instruments / known) |
| --- | ---: | --- |
| `ast/store.go` | ~86 | `KindAt`/`ChildRef`/`ListSlotAt`/`ListElem`/`putCol` |
| `binder/binder.go` | ~300 | `bindKind`, CFG walkers, `bindListRef` |
| `binder/bindwalk_generated.go` | ~450 | inlined `ChildRef`/`ListSlotAt` |
| `parser/parser.go` | ~420 | mostly JSX / await-reparse / pragma, not every token |
| `scanner/scanner.go` | ~190 | `Scan` (~52, mostly inlined `char`/`charAt`) |

`getCol` is already BCE-clean (`i < len` then index). `putCol` was not:
`ensureCol` is opaque to the prover, so the write after grow kept an
`IsInBounds` on the hot path too.

## Pattern verification (check_bce + micro)

Harness lived at `internal/ast/bceverify` during the run (removed after;
numbers below).

| Pattern | check_bce after hint | Micro vs baseline (median, n=10) |
| --- | --- | ---: |
| Scanner `pos < end` then `text[pos]` | dirty | — |
| Scanner `pos < end && pos < len(text)` | clean | **+1.7%** (worse) |
| Scanner window `text[:end]` as field | clean | **+2.8%** (worse) |
| `putCol` grow-then-index | dirty | — |
| `putCol` hot-first `i < len` then write | clean hot path | **−25.1%** |
| `KindAt` raw `nodes[id]` | dirty | — |
| `KindAt` explicit bound, return 0 on OOB | clean | −33.7% (fake: drops panic) |
| `KindAt` explicit bound, panic on OOB | clean | **~0%** vs raw |
| `ChildRef` per-call kids window | slice check once | **−0.4%** (flat) |
| `ChildRef` hoisted window + index | clean index | **−44.0%** |

## Realistic BindHot

`BenchmarkBindHot/checker.ts`, interleaved, benchtime 20x.

| Change | n | sec/op vs base | p | Verdict |
| --- | ---: | --- | ---: | --- |
| `putCol` hot-first only | 12 | −0.6% | 0.977 | **flat** — keep as codegen hygiene |
| `ListElems` hoist in `bindListRef` + functions-first | 20 | +2.3% | 0.495 | **flat** (earlier −7.5% was thermal: base drifted 19.8→22→38 ms) |

Hoisting list windows removes per-element `children[start+i]` checks, but
Instruments already said exclusive `bindChildRef`/`ListSlotAt` are a few
percent; `bindKind` dominates. Micro −44% on bare index loops does not
show up as Bind wall.

## Ranked conclusions

1. **Do land:** `putCol` hot-first rewrite. Eliminates hot-path
   `IsInBounds`, micro −25%, BindHot no regression. Column writes after
   `PrepareBindTables` are the intended beneficiary.
2. **Do not chase locally:** `KindAt`/`FlagsAt` panic-shaped BCE hints —
   same one compare as `panicIndex`. Prior KindAt hoist was already
   rejected on wall (`why-store-bind-is-slower.md`).
3. **Do not chase locally:** scanner `char`/`charAt` len-gate or window —
   check_bce goes clean, micro gets slower. Outer `Scan` already predicts
   the existing check well.
4. **Micro-only lead, BindHot unproven:** hoist `children[start:end]` (and
   list elems) once per parent in `forEachBindChildGenerated` /
   `bindListRef`. Large micro win; BindHot A/B flat. Only revisit with
   BindKPC inst/op if the generated walker is rewritten for another reason.
5. **Parser BCE density** is high in JSX / await-reparse / pragma helpers,
   not in `nextToken`. Not the first parse mutator lever vs NodeRef
   construction (`parse-inst-cycle-plan.md`).

## Ship rule reminder

Phase mutator ships only when kpc `inst/op` and `cycles/op` both fall
(`parse-bind-vs-8ac035a.md`). This survey did not produce a BindKPC
candidate that cleared that bar.
