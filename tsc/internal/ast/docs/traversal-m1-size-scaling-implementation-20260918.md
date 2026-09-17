# M1 size-scaling implementation

## Problem and scope

The fixed two-size traversal campaign could not preserve subtree shape while
scaling input or regenerate a size curve. This extension adds a repeated-subtree
workload, bounded sweep plans, saved daily selection evidence, and per-size
analysis to detect size-dependent traversal costs. Cache causality requires
separate measurements.

Selected repository: `/Volumes/SanDisk1TB/worktree/ast-traversal-bench-design`.
The initial revision was `e9beda12c346a78aca06e332898129b926868b62`; during this
implementation an external commit advanced HEAD to `69179f4efb`. Measurement
identity and source snapshot hashes identify the inputs to each run.

## Existing evidence inspected before measurement

Candidate clones: `cursor-ast-store-tests`, `binder-rewrite`, `profile`,
`flownode`, `lock-design-inv`, `lock-profile`, `store-pr-*`, `store-redesign`,
`store-schema-foreach-child`, and the main TypeScript checkout. The selected
checkout was specified by the design and was not reselected.

| Artifact set | Identity / stage | Interpretation |
| --- | --- | --- |
| `/Volumes/SanDisk1TB/Library/Caches/ast-traversal-bench/harness-native-20260917` | Initially `current` / `current`; `stale` after HEAD advanced | Previous generator and schema; not new sweep evidence |
| `binder-rewrite/tsc/internal/ast/docs/artifacts/vscode-store-pointer-b719-20260916` | `stale` / `unsupported` | No `BenchmarkTraversal` line in `bench.txt` |
| New repeated-subtree sweep at implementation start | `missing` / `missing` | Requires a new frozen run |

`current` requires exact `repo_root`, `tsgolint_git_rev`, and
`typescript_go_git_rev` agreement. TSGolint is null for this TypeScript-only
campaign. Matching revisions alone does not establish identical dirty sources.

Historical benchstat medians (ns/op), before the repeated generator:

| Visitor / logical nodes | Native pointer | Store | B/op, both | allocs/op, both |
| --- | ---: | ---: | ---: | ---: |
| full-tree / 256 | 3149 | 4692 | 0 | 0 |
| expression / 256 | 1584 | 2344 | 0 | 0 |
| full-tree / 16384 | 190400 | 279500 | 0 | 0 |
| expression / 16384 | 99240 | 137910 | 0 | 0 |

These historical integration results use a different generator, so they cannot
serve as the baseline for an old/new comparison with the repeated generator.

Current real-input hot-path ranks remain `missing`. Historical candidates are
`Handle.Parent`, `Expression`, `Name`, `Text`, and `Store.listOwner`. Broader
checker/map/link work can dominate the narrower actionable AST traversal stage.
Construction and list materialization are possible allocation drivers outside
the measured interval; their costs have not been quantified here.

## Hypothesis and acceptance

A fixed seven-node subtree repeated as independent root children provides a
prefix-stable size series while holding subtree depth and kind mix fixed. The
root list grows: these inputs do not hold every structural property constant.
The same real legacy pointer and real store adapters follow child edges from the
root. Neither a simplified pointer model nor a production-parser comparison is
substituted for an unavailable adapter.

Acceptance requires exact trace equality, nonzero visits, zero measured
allocation/GC, explicit requested/actual sizes, preserved A/A and failed attempts,
per-size raw output plus benchstat, and deterministic curve regeneration. Four
A/B pairs remain directional/noisy evidence. Unknown memory quantities are null
with explanations; field-footprint estimates are not proof of cache residency.

The host observed during review was MacBookAir10,1 with 8 GiB memory and 128-byte
cache lines. Performance-core L1D/L2 were 128 KiB/12 MiB; Efficiency-core L1D/L2
were 64 KiB/4 MiB. L3 was not reported. Actual worker core is unknown. Saved plan
host observations, including any permission failures, are authoritative for a
run; unknown values must not be filled by guessing.

## Reproduction

Build the driver once from `tsc`, outside measurement:

```sh
go build -o /tmp/astbench ./cmd/astbench
```

From the repository root:

```sh
/tmp/astbench sweep-plan --repo "$PWD" --run-id expression-sweep \
  --out /tmp/sweep-plan.json --visitor expression --start-subtrees 32 \
  --max-cases 10 --batch 256 --memory-budget-bytes 268435456 \
  --sample-timeout 1m
/tmp/astbench prepare --repo "$PWD" --before HEAD --after-working-tree \
  --plan /tmp/sweep-plan.json --out /tmp/astbench-runs --run-id expression-sweep
/tmp/astbench run --run /tmp/astbench-runs/expression-sweep
/tmp/astbench select-daily --run /tmp/astbench-runs/expression-sweep \
  --small expression-subtrees-32 --large expression-subtrees-4096 \
  --reason 'Conservative host capacity, saved memory estimates and pilot time; cache fit remains provisional' \
  --l1d-target-bytes 65536 --preset-version 1 --out /tmp/daily-plan.json
/tmp/astbench prepare --repo "$PWD" --before HEAD --after-working-tree \
  --plan /tmp/daily-plan.json --out /tmp/astbench-runs --run-id expression-daily
/tmp/astbench run --run /tmp/astbench-runs/expression-daily
/tmp/astbench collect --run /tmp/astbench-runs/expression-sweep --out /tmp/reanalysis
```

Use new output paths/run IDs on each invocation; existing plans are not
silently overwritten. The example L1D value belongs to the recorded M1 host,
not a portable assumption. `select-daily` requires saved host capacity evidence;
unavailable host information prevents that selection. Use `--visitor full-tree`
for a separate control sweep. The small preset is provisional because
field-byte lower bounds cannot prove that all accesses fit L1D.

The default 256 MiB planning budget admits nine points (225 through 57,345
logical nodes), stopping before the tenth. The 4096 bytes/node admission estimate
includes temporary generation and verification overhead but is not a hard OS
RSS limit. Upper-cache coverage is explicitly not established.

Each size retains four A/B pairs (two AB, two BA) and four A/A pairs in a
seeded schedule. Saved fields distinguish the generator seed from layout seed,
actual/requested nodes, construction order, depth, kind counts, visitor contract,
batch count and fixed two-pass warmup. Schema v2 uses `ns/visit`; one op remains
one traversal. Old schema-v1 campaigns are not silently mixed into v2 analysis.

`analysis/cells/<cell>/` contains separate before/after and A/A raw text and
benchstat output. `samples.tsv` preserves process attempt order and validity;
`memory.tsv` separates five memory quantities; `scaling.tsv`, `paired.json` and
`scaling.svg` retain normalized costs, ratios and small-sample uncertainty.
Missing benchstat preserves raw output and says comparison was not performed.
No workers are launched by collection or report regeneration.

## Implementation verification

The final harness checks passed:

```sh
cd tsc
go test ./internal/astbench/... ./cmd/astbench -count=1
go test ./internal/ast -run '^$' -bench '^BenchmarkTraversalSizeScaling$' -benchtime=1x -count=1
```

The standalone benchmark exercised 32 expression/full-tree × size × adapter
combinations. These one-iteration runs establish execution, not timing quality.
`ReportAllocs` also makes zero B/op and allocs/op visible without `-benchmem`.
The tests cover subtree-prefix identity, independent nodes, depth/kind counts,
rounding, full trace parity, zero allocation/GC, missing samples, changed visitor
contracts, forged generation metadata, invalid presets, host changes, timing
limits, and deterministic TSV/SVG regeneration.

A first 144-sample integration pilot completed with valid work and zero measured
allocation/GC. Daily selection then rejected some 0 ns timer-overhead medians:
a single empty bracket was below the host clock's resolution. The final code
calibrates 31 batches of 1,024 clock brackets, records the conservative
loop-inclusive estimate, and does not subtract it from measured time. The pilot
remains saved as `m1-expression-sweep-20260918`; it is not used for daily selection.

Full `go test ./internal/ast` failed on the supplied working tree.
Existing changes in `store.go`, `store_factory.go`, and `store_identity.go` remove
checks exercised by `TestTryBindListSpanBoundaries`, `TestIdentDoesNotAliasChildSlots`,
Freeze tests, and Global tests; `TestZeroHandleGlobalIsZero` panics. This task did
not modify those production files. The benchmark-only command and harness tests
passed independently.

## Final sweep evidence

The final sweep at
`/Volumes/SanDisk1TB/Library/Caches/ast-traversal-bench/m1-expression-final-20260918`
completed 144 valid process samples in 50.32 seconds (prepare/build excluded).
`inspect` reports identity/stage `current` against revision
`69179f4efb7f04f41d6a4b96a2f6d1cbd95ff125` and null TSGolint.
Both source snapshots and hashes are saved. Every successful sample has
0 B/op, 0 allocs/op, and 0 GC cycles. Nine points span 225 to 57,345 logical nodes.

Selected points from the sweep use benchstat medians. The curve uses bootstrap
geometric means.

| Actual nodes | Visits/op | Pointer ns/op | Store ns/op | Pointer ns/visit | Store ns/visit |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 225 | 193 | 1486 | 1961 | 7.701 | 10.159 |
| 28,673 | 24,577 | 204600 | 270600 | 8.323 | 11.009 |

The small difference is unresolved by benchstat (p=0.057); large is directionally
slower for store in this synthetic run (+32.27%, p=0.029). Four samples per arm
cannot provide benchstat's finite 95% median confidence interval. A/A and the
plotted process samples retain noise; no equivalence, cache cause, real-workload
benefit, or optimization adoption is established. Selection did not maximize
speedup. The small logical storage estimate is 16,453 pointer bytes versus 9,521
store bytes, but omitted arena capacity and cache-line effects prevent proving
small cache residency.

Separate read-only reanalysis at
`/Volumes/SanDisk1TB/Library/Caches/ast-traversal-bench/m1-expression-final-reanalysis-20260918`
reproduced before/after and A/A raw text, `samples.tsv`, `memory.tsv`, `scaling.tsv`,
`paired.json`, and `scaling.svg` byte-for-byte. SVG layout was also rendered and
visually checked. Benchstat ran separately for each cell; no size pooling or
manual old/new comparison was used.

[Curve](/Volumes/SanDisk1TB/Library/Caches/ast-traversal-bench/m1-expression-final-20260918/analysis/scaling.svg),
[table](/Volumes/SanDisk1TB/Library/Caches/ast-traversal-bench/m1-expression-final-20260918/analysis/scaling.tsv),
[benchstat](/Volumes/SanDisk1TB/Library/Caches/ast-traversal-bench/m1-expression-final-20260918/analysis/benchstat.txt),
and [saved report](/Volumes/SanDisk1TB/Library/Caches/ast-traversal-bench/m1-expression-final-20260918/report.md).

The sweep records size-dependent costs and noise. It does not establish
monotonic cache-driven degradation or a store win. The next measurements are
current real-input top-ten profiles and M2 counter/core calibration, followed
by representative workloads.

## Daily completion and remaining boundaries

`m1-daily-final-20260918` ran the saved 225/28,673-node preset with 32 valid
process samples: four balanced A/B pairs plus four A/A pairs per size.
Elapsed driver time was 29.63 seconds, excluding prepare/build; allocation
and GC remained zero. Its source sweep ID, input digest, preset version 1,
CPU/cache observations and selection rationale are in
[plan.json](/Volumes/SanDisk1TB/Library/Caches/ast-traversal-bench/m1-daily-final-20260918/plan.json).
L1D residency remains unproven, so the preset is provisional.

The final report formatting and benchmark-zero-display fixes were validated
after the measurement snapshots were frozen; they do not change the measured
visitor, generator or timer-calibration implementation. Reports are regenerated
from the saved worker results. Tests and immutable source hashes distinguish
these tool revisions instead of rewriting a frozen campaign.

All seven M1 measurement-system acceptance items are exercised: reproducible
sizes and shape metadata; exact root-edge trace parity; zero allocation/GC and
nonzero work; raw-to-benchstat/curve regeneration; justified fixed daily points;
explicit real-adapter comparison and unavailable metrics; invalid-pair/size/
visitor/unit/zero-work guards. This is a synthetic real-pointer/real-store
harness, not evidence for production parser/checker performance.

Remaining measurements are explicit: retained heap and pointer arena capacity
are unavailable; small cache residency, last-level-cache coverage, worker-core
attribution and contamination are unproven; the main expression visitor lacks
current top-ten representative workload evidence. KPC calibration, alternative
layouts and real-input end-to-end validation remain M2 onward. No production
layout optimization was adopted.
