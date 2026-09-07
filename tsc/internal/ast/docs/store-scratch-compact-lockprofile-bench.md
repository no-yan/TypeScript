# Parser scratch Store + exact Compact, remeasured after lazy JSDoc (2026-09-08)

`ParseSourceFile` used to allocate the parse Store with `NewStore(len(sourceText)/5)`, which over-reserves nodes / lists / children / intern columns by 1.7〜2.8× on average and keeps that capacity for the life of the Store. Now the parser owns an `ast.StoreScratch` (`Parser.storeScratch`, survives `putParser` with the pooled parser), `NewStoreOnScratch` lends its high-water buffers to the Store, and `Store.Compact` clones the used prefix into exact-size arrays and hands the buffers back right before `Seal`. Store identity is unchanged, so no Handle is re-pointed.

The same change was measured on 2026-09-07 (`store-scratch-compact-bench.md`, cursor/ast-store-tests) and rejected because `WarmJSDoc` appended into the parse Store after Compact (1.25× regrowth on 43% of files) and Bind was +10%. On lock-profile the deferred TS JSDoc goes to the checker's synth Store (`lazy-jsdoc-checker-store-bench.md`), so nothing writes to the parse Store after Compact on the compile path (`Intern`, `AllocSlots`, `AllocList` have no callers outside ast/parser).

## Setup

VS Code, noembed builds (`CGO_ENABLED=0 go build -trimpath -tags=noembed`), base = `5fe5a29239` via `-overlay`, M1 8 core / 8 GB, interleaved runs, 1 warmup, medians of `--extendedDiagnostics` + `/usr/bin/time -l`. Diagnostics output is identical between the two binaries on both projects.

## large `src/tsconfig.json` (9733 files), `--noEmit --noCheck --declaration false`, 6 rounds

| | base | scratch | Δ |
| --- | ---: | ---: | ---: |
| Memory used | 2017 MB | 1643 MB | −18.6% |
| Parse | 0.855 s | 0.807 s | −5.7% |
| Bind | 0.461 s | 0.445 s | −3.4% |
| Total | 1.546 s | 1.468 s | −5.0% |
| user | 8.05 s | 8.13 s | +0.9% |
| max RSS | 1585 MB | 1593 MB | +0.5% |

Memory allocs +0.2% (the Compact clone replaces the hint allocation, it does not add one).

## monaco `src/tsconfig.monaco.json` (1629 files), `--noEmit`, 5 rounds

| | base | scratch | Δ |
| --- | ---: | ---: | ---: |
| Memory used | 826 MB | 766 MB | −7.2% |
| Parse | 0.119 s | 0.111 s | −6.7% |
| Bind | 0.049 s | 0.052 s | +6% (noise: 0.039–0.082) |
| Check | 1.035 s | 1.053 s | +1.7% |
| Total | 2.040 s | 2.129 s | +4.4% |
| user | 7.43 s | 7.92 s | +6.6% |
| max RSS | 1187 MB | 1102 MB | −7.2% |

GC cycles (gctrace) 13 → 14. Same pacing effect as last time: a smaller live heap lowers the heap goal, so the same GOGC runs one more cycle and the extra mark work lands in Check.

Heap goal matched (base GOGC=100 vs scratch GOGC=115, 4 rounds): Memory used −7.3%, Total 2.050 → 1.962 s (−4.3%), user −2.4%, Check +0.7%, real ±0.

`--noCheck` with `GOGC=off` (mutator only, 3 rounds): Memory used 441 → 382 MB (−13.5%), Parse +3% (0.094 → 0.097 s: the Compact memmove), Bind −2.6%, user +1.7%.

## Conclusion

Exact-size parse columns cut the parse-phase live heap by 18.6% on the full VS Code build and 7% on monaco at no alloc cost. The `--noCheck` build is 5% faster; the checked monaco build is ±4% depending on where the GC goal lands, and equal-goal comparison is slightly in favour of scratch. The residual cost is the scratch itself: one high-water buffer per pooled parser (worker count × largest file), dropped by `sync.Pool` after two GCs.
