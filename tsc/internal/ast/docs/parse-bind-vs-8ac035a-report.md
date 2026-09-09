# Parse, bind, and GC vs 8ac035a. Report

Filled from `.cursor/skills/verify-tsc/artifacts/eval-8ac035a-20260909`. How to collect the numbers is `parse-bind-vs-8ac035a.md`.

The decision this sheet supports. Continue Store layout into Check only if parse or bind is not a disaster and vscode-scale GC scan work improves. Check time itself is not the gate.

## Identity

| Field | Value |
| --- | --- |
| Date | 2026-09-09 |
| Machine | Apple M1, darwin arm64 |
| Go | go1.26.6 darwin/arm64 (`/opt/homebrew/opt/go/bin/go`) |
| Parent tree | `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript` |
| Parent SHA | `8ac035a394c79e693a3a7d74cb170448503ee894` |
| Parent `git status --short` after overlay | `?? .worktree/`, plus untracked overlay `kperf/` and `bind_*_bench_test.go`. After restore, overlay paths are gone (`parent.status.after.txt` empty). |
| Store tree | `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests` |
| Store SHA | `5d7ed2c7115a5f3158747678697f0b9dfec786f8` |
| Store dirty files | At identity: untracked eval docs and scripts under `tools/scripts/tsc/` and `tsc/internal/ast/docs/`, plus `__pycache__`. Working tree also had other Store work not in that snapshot. |
| `checker.ts` sha256 under `KPERF_TESTDATA` | `4fb2f7e7d898a1729a24b9ad2507b697b747bc8c1315f27cd7e72861115a83e9` |
| vscode root | `/Users/noyan/ghq/github.com/microsoft/vscode` |
| Store `tsc --version` | `Version 7.1.0-dev` |
| Parent `tsc --version` | `Version 7.1.0-dev` |
| Store embed (`go version -m`, no `noembed`) | no `noembed` build tag. `CGO_ENABLED=1`. |
| Parent embed | no `noembed` build tag. `vcs.revision=8ac035a394c79e693a3a7d74cb170448503ee894`. `CGO_ENABLED=0`. |
| Artifact dir | `.cursor/skills/verify-tsc/artifacts/eval-8ac035a-20260909` |
| kpc sudo | `ok` (macOS admin dialog. `sudo -n` still needs a password) |

Same-binary kpc sanity, Store `BenchmarkParseKPC/checker.ts`, two runs.

| Run | inst/op | cycles/op |
| --- | ---: | ---: |
| A | 281761713 | 141446996 |
| B | 282295861 | 139931698 |
| relative inst delta | 0.19% | `<1%`, continue |

## vscode `src/tsconfig.json` audit

`--showConfig` JSON is `vscode.showConfig.json` in the artifact dir.

| Check | Expected | Observed |
| --- | --- | --- |
| `incremental` | false or absent | `false` |
| `composite` | false or absent | absent |
| `tsBuildInfoFile` | absent | absent |
| `noEmit` | true | `true` |
| `--incremental false` on argv | present | present |
| `BuildInfo read time` in timed stdout | absent | absent |
| new `*.tsbuildinfo` after run | none | none under `src/` |
| new compiler `.js` under `src/` | none | not checked file-by-file. `--noEmit` and `outDir` is `../out/vs`. |
| `Files` parent vs Store | same band | 9733 vs 9733 |

## Hypotheses before the run

These lines follow `STORE.md` and the plan. They were written into this sheet after wall and monaco existed, not before the first bench.

- Parse mutator on checker.ts is about even versus parent on wall time.
- Bind mutator on checker.ts is slower versus parent (BindHot, Freeze sits in CLI Bind).
- monaco Check time may move, and a move is not a Store layout claim.
- vscode-scale scannable fraction `heapScan/initialHeapLive` is lower on Store than parent, and the gap is at least as large as monaco check-on.
- vscode-scale mark CPU or `heapScanWork` is lower on Store than parent.

## Layer A. Phase kpc, `GOGC=off`, median of 3, `30x`

| Bench | Parent inst/op | Store inst/op | inst delta | Parent cycles/op | Store cycles/op | cycles delta | Parent IPC | Store IPC | Both fell? |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| ParseKPC checker.ts | 261872147 | 281970922 | +7.7% | 126021095 | 143173579 | +13.6% | 2.078 | 1.969 | no |
| ParseKPC empty.ts | 150591 | 157136 | +4.3% | 111394 | 115386 | +3.6% | 1.356 | 1.363 | no |
| ParseKPC dom.generated.d.ts | 124619046 | 136531177 | +9.6% | 53003445 | 64018093 | +20.8% | 2.351 | 2.133 | no |
| BindKPC checker.ts | 111074369 | 188791949 | +70.0% | 62313059 | 98303072 | +57.8% | 1.782 | 1.921 | no |
| BindKPC empty.ts | 144839 | 148267 | +2.4% | 106994 | 112944 | +5.6% | 1.354 | 1.318 | no |
| BindKPC dom.generated.d.ts | 49586359 | 79042260 | +59.4% | 25019857 | 35926306 | +43.6% | 1.985 | 2.207 | no |

Cite bind-algorithm speed only from BindKPC or BindHot. IPC is diagnostic. No phase had both inst/op and cycles/op fall. Parse checker IPC fell while cycles rose, so it is a reject for a faster parse mutator.

## Layer B. Wall microbench, default `GOGC`, `benchstat`

Record `p` and `n` on every time delta.

| Bench | Parent sec/op | Store sec/op | time delta | p | n | Parent B/op | Store B/op | Parent allocs/op | Store allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Parse checker.ts | 25.44m | 26.34m | ~ +3.5% | 0.089 | 10 | 24.93Mi | 12.90Mi | 11.932k | 1.011k |
| Parse empty.ts | 1.150µ | 2.179µ | +89.48% | 0.000 | 10 | 1.148Ki | 2.272Ki | 4.000 | 10.000 |
| Parse dom.generated.d.ts | 11.38m | 11.55m | ~ | 0.089 | 10 | 9.445Mi | 6.937Mi | 2.790k | 1.841k |
| BindHot checker.ts | 13.20m | 20.26m | +53.41% | 0.000 | 20 | 7.081Mi | 12.207Mi | 13.95k | 14.16k |
| BindHot empty.ts | 3.206µ | 4.058µ | ~ | 0.218 | 10 | 1.378Ki | 1.745Ki | 4.000 | 7.000 |
| BindHot dom.generated.d.ts | 4.950m | 7.469m | +50.89% | 0.000 | 10 | 5.042Mi | 7.503Mi | 16.56k | 16.68k |

Parse checker B/op −48.24% (p=0.000). allocs/op −91.53% (p=0.000). BindHot checker B/op +72.38% (p=0.000). allocs/op +1.51% (p=0.000).

`GOGC=off` checker.ts companion.

| Bench | Parent sec/op | Store sec/op | p | n |
| --- | ---: | ---: | ---: | ---: |
| Parse checker.ts | 26.72m | 27.05m | 0.818 | 6 |
| BindHot checker.ts | 13.31m | 20.97m | 0.002 | 6 |

## Layer C. monaco, median

`--noCheck` n=5. Check-on n=3, default threads only. Median is the middle sorted value.

Process inst and cycles come from `/usr/bin/time -l`. They are whole-process.

### `--noCheck`, default threads

| Metric | Parent | Store | delta |
| --- | ---: | ---: | ---: |
| Files | 1629 | 1629 | 0 |
| Parse time s | 0.199 | 0.133 | −33% |
| Bind time s | 0.041 | 0.064 | +56% |
| Total time s | 0.272 | 0.224 | −18% |
| real s | 0.43 | 0.31 | −28% |
| user s | 1.96 | 1.26 | −36% |
| Memory used | 357518K | 253817K | −29% |
| Memory allocs | 960238 | 746801 | −22% |
| last heapScan MB | 325 | 105 | −68% |
| last initialHeapLive MB | 349 | 247 | −29% |
| last scannable fraction | 0.931 | 0.425 | −54% relative |
| last noscan-ish `1-scan/live` | 0.069 | 0.575 | higher noscan |
| last heapScanWork B | 179147224 | 42097168 | −77% |
| gctrace GC count | 10 | 13 | +30% |
| gctrace last GC CPU % | 15 | 9 | −40% |

### `--noCheck`, `--singleThreaded`

| Metric | Parent | Store | delta |
| --- | ---: | ---: | ---: |
| Parse time s | 0.448 | 0.372 | −17% |
| Bind time s | 0.115 | 0.161 | +40% |
| Total time s | 0.576 | 0.551 | −4% |
| user s | 1.39 | 0.74 | −47% |
| last scannable fraction | 0.934 | 0.420 | −55% relative |

### `--noCheck`, `GOMAXPROCS=1 --singleThreaded`

| Metric | Parent | Store | delta |
| --- | ---: | ---: | ---: |
| Parse time s | 0.626 | 0.389 | −38% |
| Bind time s | 0.111 | 0.178 | +60% |
| Total time s | 0.764 | 0.600 | −21% |
| user s | 1.05 | 0.65 | −38% |
| last scannable fraction | 0.931 | 0.416 | −55% relative |

### Check on, default threads

| Metric | Parent | Store | delta |
| --- | ---: | ---: | ---: |
| Files | 1629 | 1629 | 0 |
| Parse time s | 0.205 | 0.139 | −32% |
| Bind time s | 0.043 | 0.063 | +47% |
| Check time s | 1.460 | 1.661 | +14% |
| Total time s | 1.735 | 1.895 | +9% |
| user s | 8.65 | 8.51 | −2% |
| last scannable fraction | 0.944 | 0.773 | −18% relative |
| last heapScanWork B | 648092856 | 444390272 | −31% |
| gctrace last GC CPU % | 11 | 6 | −45% |

Check time delta is recorded. It is not a Store layout claim.

## Layer D. vscode `src/tsconfig.json`, check on, n=3

Default `GOMAXPROCS`. `--noEmit --incremental false`.

| Metric | Parent | Store | delta |
| --- | ---: | ---: | ---: |
| Files | 9733 | 9733 | 0 |
| Parse time s | 1.662 | 1.147 | −31% |
| Bind time s | 0.261 | 0.336 | +29% |
| Check time s | 24.278 | 13.884 | −43% |
| Total time s | 26.349 | 15.750 | −40% |
| real s | 45.25 | 24.65 | −46% |
| user s | 71.34 | 63.17 | −11% |
| Memory used | 5461601K | 4928768K | −10% |
| Memory allocs | 30908855 | 29571380 | −4% |
| RSS | 3029794816 | 3134914560 | +3% |
| last heapScan MB | 5024 | 3710 | −26% |
| last initialHeapLive MB | 5333 | 4827 | −9% |
| last scannable fraction | 0.942 | 0.768 | −18% relative |
| last noscan-ish `1-scan/live` | 0.058 | 0.232 | higher noscan |
| last heapScanWork B | 4578774840 | 3000360200 | −34% |
| gctrace GC count | 23 | 26 | +13% |
| gctrace last GC CPU % | 19 | 13 | −32% |

Raw parent Files, parse, bind, check, total, scan/live per round. (9733, 1.662, 0.345, 27.644, 30.269, 0.942), (9733, 1.611, 0.257, 24.278, 26.349, 0.942), (9733, 1.676, 0.261, 22.382, 24.507, 0.942).

Raw Store Files, parse, bind, check, total, scan/live per round. (9733, 1.095, 0.310, 14.611, 16.382, 0.768), (9733, 1.147, 0.337, 13.884, 15.750, 0.768), (9733, 1.332, 0.336, 12.869, 14.907, 0.771).

Both sides report one vscode type error in `mcpSamplingLog.test.ts`. tsc exit is 2. File counts match.

## Scale contrast

| | monaco check-on last scannable fraction | vscode check-on last scannable fraction |
| --- | ---: | ---: |
| Parent | 0.944 | 0.942 |
| Store | 0.773 | 0.768 |
| Store minus parent | −0.171 | −0.174 |

The non-generational claim needs the vscode gap at least as large as monaco, not a monaco-only heap blip. The vscode gap matches monaco check-on. monaco `--noCheck` shows a larger Store noscan (0.93 to 0.43) because Check has not rebuilt a pointer graph yet.

## Claims

Write one sentence per claim. Attach the layer that supports it.

- Parse mutator. Layer A ParseKPC checker.ts is +7.7% inst/op and +13.6% cycles/op. Layer B wall is statistically even (p=0.089). CLI parse times fall because alloc and GC moved, not because the mutator retired fewer instructions.
- Bind mutator. Layer A BindKPC checker.ts is +70.0% inst/op and +57.8% cycles/op. Layer B BindHot checker.ts is +53.41% sec/op (p=0.000, n=20). CLI Bind is slower too and includes Store Freeze.
- GC scan fraction at vscode scale. Layer D last scannable fraction 0.942 to 0.768. heapScan 5024 MB to 3710 MB.
- GC mark work at vscode scale. Layer D last heapScanWork 4.58e9 B to 3.00e9 B (−34%). last GC CPU % 19 to 13.
- Size trend monaco versus vscode. Check-on scannable-fraction gap is about 0.17 on both projects. `--noCheck` monaco is not the scale claim.
- monaco Check time. +14% on monaco, −43% on vscode. Not a layout claim. Flow, Symbol, and SymbolTable are still pointer structures.
- Continue into Check layout? yes, with the mutator tax named. Parse kpc did not get cheaper. Bind kpc is a large regression. vscode scan fraction and mark work still improve, which is the Store GC bet.

## Rejected readings

- monaco or vscode Bind time as bind-algorithm speed. Store `BindSourceFiles` also `Freeze`s parse Stores.
- Check time as Store AST speed. Flow, Symbol, and SymbolTable are still pointer structures.
- `--singleThreaded` user CPU as single-thread mutator unless `GOMAXPROCS=1`.
- `/usr/bin/time -l` instructions as BindKPC.
- `Memory used` as noscan fraction. Use gcpacertrace `scan X MB in Y->Z MB`.
- empty.ts wall as the product result.
- A vscode run with `BuildInfo read time` or a File-count mismatch.

## Operator

| Item | Value |
| --- | --- |
| Plan followed | `parse-bind-vs-8ac035a.md` |
| Script | `tools/scripts/tsc/eval-parse-bind-vs-8ac035a.sh` |
| GC parser | `tools/scripts/tsc/summarize-gc-trace.py` |
| Parent overlay restored | yes |
| Notes | vscode warmup used to abort under `set -e` because tsc exits 2 on the known vscode error. Warmup now ignores that exit. `summarize-gc-trace.py` now matches `gc` lines with `re.M`. First combined wall-plus-monaco run skipped vscode for that warmup exit. kpc ran as root via a /tmp driver after osascript could not execute the SanDisk script. Artifacts also live under `/tmp/eval-kpc-8ac035a`. Do not compare `1f70213d`. |
