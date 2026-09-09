# How to compare parse, bind, and GC against 8ac035a

This how-to compares the Store compiler in this worktree with pointer-AST `tsc` at `8ac035a394`. That commit is the merge-base of this branch with the parent pointer tree. Fill the report at `parse-bind-vs-8ac035a-report.md`. Do not treat `microsoft/typescript` `main` as Original.

The consumer is a microsoft maintainer who must decide whether the Store layout bet is worth continuing. The bet is that a non-generational GC scans less as the live tree grows, because node rows are noscan. Parse and bind are the Store-backed phases today. Check still sits on pointer `FlowNode`, `Symbol`, and `SymbolTable`. A Check-time win is not required. If parse, bind, and GC scan work improve at VS Code scale, Check is the next layout target, not a reason to stop.

The next owner reruns `tools/scripts/tsc/eval-parse-bind-vs-8ac035a.sh` instead of reconstructing argv.

## What you measure

Run the layers in order. Stop a layer if its gate fails.

1. Identity. Both SHAs, Go version, dirty files, same `checker.ts` bytes. vscode config audit.
2. Same-binary kpc sanity. One Store `BenchmarkParseKPC/checker.ts` binary twice. Median `inst/op` must differ by less than 1%.
3. Phase kpc. `ParseSourceFile` and `BindSourceFile` on shared fixtures, `GOGC=off`, root, interleaved.
4. Wall microbench. `BenchmarkParse` and `BenchmarkBindHot` with default `GOGC`, then checker.ts again with `GOGC=off`.
5. monaco small e2e. `--project src/tsconfig.monaco.json`. `--noCheck` for parse and bind wall. Then the same argv without `--noCheck` so Check is present. Expect Check not to move.
6. vscode scale e2e. `--project src/tsconfig.json` with check on. This is the scale claim. monaco is too small to show non-generational scan cost growing with live heap.

Every CLI e2e sets `GODEBUG=gctrace=1,gcpacertrace=1`. Parse those lines with `tools/scripts/tsc/summarize-gc-trace.py`. Do not invent noscan counts from `Memory used` alone.

kpc answers "did this function retire fewer instructions and cycles." IPC is a diagnostic column, not a ship gate. gcpacertrace answers "how much of the live heap was scannable." monaco and vscode wall answer user time at two sizes.

## Fixed names

- Parent tree. `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript` at `8ac035a394c79e693a3a7d74cb170448503ee894`.
- Store tree. This checkout. Record `git rev-parse HEAD` and `git status --short`.
- Shared fixtures. `KPERF_TESTDATA=/tmp/kperf-testdata`. Copy `checker.ts` and `dom.generated.d.ts` from this worktree's `tsc/testdata/fixtures/` into that dir before the first run if they are missing.
- vscode. `$VERIFY_TSC_VSCODE_ROOT` or `/Users/noyan/ghq/github.com/microsoft/vscode`.
- Go. The `go` line from `tsc/go.mod`. Today that is 1.26. Use one binary for both trees. Homebrew `go1.26.6` is acceptable.

## vscode config audit

`src/tsconfig.json` extends `src/tsconfig.base.json`. Neither file sets `incremental` or `composite`. tsgo's default for `incremental` is false unless `composite` is set. `skipLibCheck` is true. `outDir` is `../out/vs`, so you must pass `--noEmit` or the run writes the vscode tree.

Always pass these flags on the large project, even though they match the defaults:

```sh
--project src/tsconfig.json --noEmit --declaration false --incremental false --extendedDiagnostics
```

Do not pass `--noCheck`. Do not pass `--build`.

Before the first timed round, run `--showConfig` with the same flags and keep the JSON. Confirm all of these.

- `incremental` is false or absent.
- `composite` is false or absent.
- `tsBuildInfoFile` is absent.
- `noEmit` is true in the effective config.
- stdout of a timed run has no `BuildInfo read time`.
- After the run, no new `*.tsbuildinfo` under the vscode tree, and `src/` has no new compiler `.js`.

`src/tsconfig.json` lists a `tsec` plugin. Native `tsc` does not load it. Ignore plugin work.

File count in `--extendedDiagnostics` must stay in the same band on parent and Store. A missing program (wrong cwd, skipped files) invalidates the scale claim.

## Confounds you must keep visible

- Parent `8ac035a` has no `WarmJSDoc`. TS JSDoc without `@see` or `@link` is deferred until `Node.JSDoc()`, the same as upstream. Store `BindSourceFiles` binds, then `Freeze`s each parse Store. That Freeze sits inside the Bind timer. monaco and vscode `Bind time` include it. `BenchmarkBindHot` and BindKPC do not.
- `--singleThreaded` only serializes the compiler workgroup. Go GC still uses extra threads. `GOMAXPROCS=1 --singleThreaded` is the mutator-plus-GC-assist case. Use it on monaco. Do not require it on full vscode check. That run is too long for the scale claim, which needs a large live heap under default `GOMAXPROCS`.
- `Store.Compact` memcpy shows up in one-file `GOGC=off` cycles. monaco and vscode heap can still improve.
- Parent `checker.ts` in testdata may differ by one byte from this tree. Always read fixtures through `KPERF_TESTDATA` or copy those bytes over parent testdata for wall benches, then restore.
- This worktree is about 200 commits past `8ac035a`. The result is this fork versus pointer parent, not a one-column A/B.
- Check still uses pointer flow, symbol, and symbol table. Do not treat a flat Check time as a Store failure. Do not treat a Check drop as a Store AST win unless gctrace mark CPU explains it.

## GC traces

Set `GODEBUG=gctrace=1,gcpacertrace=1` on every CLI process. Both traces go to stderr and mix with `/usr/bin/time -l`. Keep the whole stderr file.

`gctrace=1` prints one line per cycle. The CPU field is GC CPU since start. The `A->B->C MB` triple is heap at start, after GC, and live. Mark CPU is the middle wall and CPU group in `#+#+# ms clock` and `#+#/#/#+# ms cpu`.

`gcpacertrace=1` prints pacer lines. The line that names noscan-related scan work is:

```
pacer: assist ratio=... (scan X MB in Y->Z MB) workers=...
```

`X` is `gcController.heapScan`, the live heap omitting noscan objects and noscan tails (`runtime/mgcpacer.go`). `Y` is `work.initialHeapLive`. `Z` is the heap goal. The scannable fraction for that cycle is `X/Y`. The noscan-ish remainder is `1 - X/Y`. It is not an object count. It is a byte fraction of live heap that the pacer will not scan.

A later pacer line prints `heapScanWork+stackScanWork+globalsScanWork B work`. Record the last cycle's `heapScanWork` as done scan work.

Run `python3 tools/scripts/tsc/summarize-gc-trace.py STDERR.txt` and paste the medians into the report. If kpc is unreachable, still run these traces. They do not need root.

## Prepare the parent overlay

Parent has no `kperf` package. Copy these files from this worktree. Leave them untracked. Restore with `git clean` on those paths after the run.

- `tsc/internal/testutil/kperf/kperf_darwin.go`
- `tsc/internal/testutil/kperf/kperf_stub.go`
- `tsc/internal/binder/bind_kperf_bench_test.go`
- `tsc/internal/binder/bind_hot_bench_test.go`

Do not copy Store `parser.go` or `binder.go` into the parent.

## Build

Serialize every `go test -c` and `go build` of module `github.com/microsoft/TypeScript/tsc`. Parent and Store share that module path. Concurrent compiles corrupt `GOCACHE`.

Parent, from the parent `tsc/` directory, with the same `go` as Store:

```sh
CGO_ENABLED=1 go test -tags kperf -c -o /tmp/orig.binder.kperf.test ./internal/binder
CGO_ENABLED=0 go test -c -o /tmp/orig.parser.bench.test ./internal/parser
CGO_ENABLED=0 go test -c -o /tmp/orig.binder.bench.test ./internal/binder
CGO_ENABLED=0 go build -trimpath -o /tmp/tsc-orig ./cmd/tsc
```

Store, from this repo root. Do not overlap with `control-tsc launch`.

```sh
CGO_ENABLED=1 ./.cursor/skills/verify-tsc/scripts/control-tsc go -- -C ./tsc test -tags kperf -c -o /tmp/store.binder.kperf.test ./internal/binder
CGO_ENABLED=0 ./.cursor/skills/verify-tsc/scripts/control-tsc go -- -C ./tsc test -c -o /tmp/store.parser.bench.test ./internal/parser
CGO_ENABLED=0 ./.cursor/skills/verify-tsc/scripts/control-tsc go -- -C ./tsc test -c -o /tmp/store.binder.bench.test ./internal/binder
VERIFY_TSC_RUN_ID=eval-8ac035a ./.cursor/skills/verify-tsc/scripts/control-tsc launch --embed
cp -f built/local/tsc /tmp/tsc-store
```

Doctor must print `tsc-embed` pass for the Store binary. Parent `go version -m /tmp/tsc-orig` must not list `noembed`.

## Run kpc

Needs root. Do not run `xctrace` in the same window. Export `KPERF_TESTDATA=/tmp/kperf-testdata` and `GOGC=off`.

Interleave parent then Store, `count 1` per round, 3 rounds. Benchtime `30x`. Names:

- `^BenchmarkParseKPC$/^checker.ts$`
- `^BenchmarkParseKPC$/^empty.ts$`
- `^BenchmarkParseKPC$/^dom.generated.d.ts$`
- `^BenchmarkBindKPC$/^checker.ts$`
- `^BenchmarkBindKPC$/^empty.ts$`
- `^BenchmarkBindKPC$/^dom.generated.d.ts$`

Both binaries live in `./internal/binder` for these names.

If sudo is missing, write `kpc=unreachable` in the report and continue wall plus CLI e2e. Do not invent inst/op from `/usr/bin/time -l`. Do not fill kpc IPC.

## Run wall microbenches

Default `GOGC`. Interleave 10 rounds, `-test.benchtime 30x -test.benchmem`. If `sec/op` has `p>0.05` while `B/op` has `p≈0`, rerun that fixture at `n=20` before a time claim. checker.ts bind uses `n=20` from the start.

`GOGC=off` checker.ts parse and BindHot, 6 interleaved rounds. This is the mutator companion to kpc, not the user-visible number.

Compare with `benchstat orig.txt store.txt`.

## Run monaco

Cwd is the vscode tree. Warm each binary once per argv family. Then 5 interleaved rounds for `--noCheck`, 3 for check-on. Default threads, `--singleThreaded`, and `GOMAXPROCS=1 --singleThreaded` apply to `--noCheck` only. Check-on uses default threads.

```sh
GODEBUG=gctrace=1,gcpacertrace=1 /usr/bin/time -l /tmp/tsc-orig \
  --project src/tsconfig.monaco.json --noEmit --noCheck --declaration false --extendedDiagnostics
```

Check-on drops `--noCheck`. Same argv for `/tmp/tsc-store`.

Record stdout `Files`, `Parse time`, `Bind time`, `Check time`, `Total time`, `Memory used`, `Memory allocs`. Record `time -l` `real`, `user`, `sys`, `instructions retired`, `cycles elapsed`, `maximum resident set size`. Process inst and cycles include config, runtime, and GC. They are not BindSourceFile.

Do not use `go tool pprof` for this comparison.

## Run vscode scale

Same cwd. Warm once. Three interleaved rounds, default `GOMAXPROCS`.

```sh
GODEBUG=gctrace=1,gcpacertrace=1 /usr/bin/time -l /tmp/tsc-orig \
  --project src/tsconfig.json --noEmit --declaration false --incremental false --extendedDiagnostics
```

Same argv for `/tmp/tsc-store`. Check is on.

Record the same stdout fields as monaco, plus GC medians from `summarize-gc-trace.py`. Compare monaco check-on against this run for the scale claim. The interesting GC numbers are cycle count, last-cycle scannable fraction `heapScan/initialHeapLive`, last-cycle `heapScanWork`, cumulative gctrace GC CPU percent, and user CPU. Parse and Bind may still move. Check time is recorded, then set aside unless gctrace says the drop was mark work.

## Verdict rules

- A phase mutator is faster only if kpc `inst/op` and `cycles/op` both fall. Median of 3. inst gate is 1%. cycles gate is 3%. IPC falling while cycles stay is a reject for that phase. IPC rising is not a ship requirement.
- Wall `sec/op` needs `p<0.05` at the n you report. Alloc deltas with `p≈0` may stand when time does not.
- GC is promising when vscode scale shows a higher noscan-ish fraction (`1 - scan/live`) and lower mark CPU or `heapScanWork` than parent, with File count matched. monaco may show a smaller gap. That size trend is the non-generational claim.
- monaco or vscode Check time is not a Store success or failure by itself.
- monaco Bind time includes Store `Freeze`. Do not treat a Bind-timer delta as bind-algorithm speed.
- Empty file benches measure fixed cost. Do not headline them.
- The continue-to-Check-layout sentence is allowed only if parse or bind mutator is not a disaster and the vscode GC scan fraction or mark work improves. Otherwise the layout bet did not show up where Store already owns the tree.

## Restore

Remove the overlay files from the parent tree. Confirm `git status` in the parent is clean for those paths. Keep artifacts under `.cursor/skills/verify-tsc/artifacts/` or `$OUT`. Do not delete them in cleanup of `/tmp/verify-tsc-*`.
