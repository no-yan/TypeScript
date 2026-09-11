# Paired CPU Profiler: Store vs 8ac035a (monaco)

Date: 2026-09-09. Apple M1. Template: Instruments **CPU Profiler** (cycle-weight).
Workload: vscode `src/tsconfig.monaco.json` `--noEmit --noCheck --declaration false --singleThreaded --extendedDiagnostics`.
Binaries: `/tmp/tsc-store-paired` (this worktree embed), `/tmp/tsc-parent-8ac035a` (`8ac035a394`).
Artifacts: `.cursor/skills/verify-tsc/artifacts/20260909T2117-paired-cpu/{store,parent}-monaco/`.

## Caveats

- Parent process is **GC-dominated** (68% cycle-weight). Store is not (29% GC). Bind % of *process* is not comparable; use bind-bucket shares and BindKPC for absolute cost.
- Bind cycle-weight sum: Store 291067038, Parent 168239439, ratio **1.73×** (aligns with BindKPC ~+70% inst order of magnitude).
- Leaf names differ (`bindKind` vs `bind`). Rows below alias counterparts.

## Process buckets (% of all cycle-weight)

| bucket | Store | Parent | delta pp |
| --- | ---: | ---: | ---: |
| bind | 27.85% | 8.79% | +19.06 |
| parse | 25.53% | 13.56% | +11.97 |
| gc | 28.63% | 68.41% | -39.78 |
| runtime | 17.31% | 8.21% | +9.10 |
| other | 0.67% | 0.38% | +0.29 |
| bind+gc | 0.00% | 0.50% | -0.50 |
| parse+gc | 0.01% | 0.14% | -0.13 |

## Aliased bind exclusive (% of each side's bind bucket)

| role | Store %bind | Parent %bind | delta pp | Store-specific? |
| --- | ---: | ---: | ---: | --- |
| bind / bindKind (walk entry) | 13.29 | 11.97 | +1.32 | walk currency (Store shape) |
| child walk (generated vs ForEachChild) | 5.33 | 3.39 | +1.94 | walk currency (Store shape) |
| bindChildren | 3.32 | 3.66 | -0.34 | walk currency (Store shape) |
| list/functions-first walk | 2.86 | 2.35 | +0.51 | walk currency (Store shape) |
| bindChildRef (Store) | 2.30 | 0.00 | +2.30 | Store API / Ref |
| nameRefGenerated (Store) | 3.20 | 0.00 | +3.20 | Store API / Ref |
| ListSlotAt (Store) | 3.11 | 0.00 | +3.11 | Store API / Ref |
| ListElem (Store) | 1.24 | 0.00 | +1.24 | Store API / Ref |
| listOwner (Store) | 1.39 | 0.00 | +1.39 | Store API / Ref |
| Handle.childAt (Store) | 1.42 | 0.00 | +1.42 | Store API / Ref |
| flowID (Store) | 1.06 | 0.00 | +1.06 | Store API / Ref |
| checkContextualIdentifier | 2.19 | 1.09 | +1.10 | counterpart (shared work) |
| isIdentifierNameRef (Store) | 2.11 | 0.00 | +2.11 | Store API / Ref |
| GetContainerFlags | 1.91 | 2.78 | -0.87 | counterpart (shared work) |
| declareSymbol | 1.86 | 1.82 | +0.04 | counterpart (shared work) |
| isNarrowableReference | 1.71 | 1.62 | +0.09 | shared algorithm |
| aeshashbody | 5.14 | 8.68 | -3.54 | shared algorithm |
| mapaccess1_faststr | 2.53 | 6.05 | -3.52 | shared algorithm |

## What this says for Store-only work

1. **Absolute bind cost is higher on Store** (bind weight ~1.7×). That matches BindKPC; the gap is real.
2. **Store bind exclusive mass is the walk spine**: `bindKind` + generated children + `bindChildrenRef`/`bindListRef`/`bindChildRef` + `nameRefGenerated` + `ListSlotAt`/`ListElem`/`listOwner`. Parent's corresponding mass sits in `bind` + `ForEachChild` + `bindChildren`. This is the NodeRef currency tax, not one missing micro-opt.
3. **True Store-only leaves** (`ListSlotAt`, `ListElem`, `listOwner`, `Handle.childAt`, `flowID`) are each ~1–3% of Store bind — consistent with micro-A/Bs that moved ≤2–5%.
4. **Shared leaves** (hash/maps, `isNarrowableReference`, contextual identifier) appear on both sides; optimizing them does not prove Store > pointer.
5. Parent's large GC bucket means monaco process % is a bad Bind clock. Prefer BindKPC / BindHot for ship decisions; use this pair to **attribute** Store bind self-cost.

## Next Feature (Store-only)

Attack the **walk currency** as a whole (how Binder loads children/lists/kind), not isolated `flowID`/`HandleOf` sites. Any change must show BindKPC inst **and** cycles down versus **this** Store baseline, and must not be a rewrite pointer Bind can copy verbatim (e.g. skip contextual checks).

Paired profile does **not** justify landing MayBeReserved or other shared binder opts.
