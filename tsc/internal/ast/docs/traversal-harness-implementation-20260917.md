# Traversal harness implementation

## Problem and evidence

This implements the first reusable measurement loop from
[the measurement design](traversal-measurement-design-20260917.md). Its purpose is
to compare the cost of traversing the same AST edges.
Construction, type inference, symbol graphs, and checker links are outside the
synthetic timed region.

Selected repository: `/Volumes/SanDisk1TB/worktree/ast-traversal-bench-design`.
Initial TypeScript revision: `e9beda12c346a78aca06e332898129b926868b62`.
TSGolint revision: `null`. The implementation is additional working-tree source;
an experiment must record that source, not only the initial revision.

The existing artifact inventory was examined before any new measurement:

| Artifact | Identity | Requested traversal stage | Use |
| --- | --- | --- | --- |
| `binder-rewrite/tsc/internal/ast/docs/artifacts/vscode-store-pointer-b719-20260916` | stale | unsupported: no traversal `bench.txt` | Candidate hot paths only |
| Selected checkout's new traversal campaign at implementation start | missing | missing | Must be generated with this harness |
| Selected checkout's check-phase top ten | missing | missing | Expression visitor remains provisional |

Candidate checkouts include `cursor-ast-store-tests`, `binder-rewrite`, `profile`,
`flownode`, `lock-design-inv`, `lock-profile`, `store-pr-*`, `store-redesign`,
`store-schema-foreach-child`, and the main checkout at
`/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`. This is an inventory, not
an assertion that their artifacts are comparable.

The proposed main pointer baseline, `8ac035a394c79e693a3a7d74cb170448503ee894`,
differs from the selected revision in 630 files. Parser, binder, and checker alone
account for 137 changed files. An end-to-end comparison against it cannot isolate
AST representation. Same-checkout synthetic construction is the narrower useful
comparison; parser-derived input exercises the current store parser.

## Hypothesis and boundaries

The hypothesis is that representation and accessor changes can reduce the cost
of following actual tree edges. A same-work comparison with no reduction, or a
regression, rejects that hypothesis for the measured case. A model or synthetic
win does not establish a representative checker or end-to-end win.

Old profile candidates are `Handle.Parent`, `Expression`, `Name`, `Text`, and
`Store.listOwner`. The broader checker/map/link stage may dominate total time;
AST access is the narrower actionable stage. Allocation drivers outside the
measured traversal include AST construction, list materialization, and checker
type/inference work. None is quantified by an old CPU profile's malloc weight.

Source inspection gives the following access contracts, without assigning a
profile rank or frequency:

| Source | AST access | Work excluded from a synthetic visitor |
| --- | --- | --- |
| `checker/checker.go`, `checkBinaryExpression` | left, operator token, right | operand types and operator checking |
| `checker/checker.go`, `checkCallExpression` | callee and argument expressions | overload resolution, signatures, inference |
| `checker/checker.go`, `checkPropertyAccessExpression` | expression and property name | symbol lookup and type/member checks |

Current `self`, `inclusive`, and top-ten ranks are **missing**. Source presence
does not make these functions the hottest checks or establish workload coverage.

## Implementation and acceptance

The measurement worker owns one representation at a time. Complete trace checks
run separately from fixed-batch samples. The driver owns immutable input plans,
source snapshots, build output, process attempts, and comparison validation.
Production AST fields are not extended for measurement IDs.

Acceptance requires matching trace order and attributes, nonzero work, no
allocation or GC in the synthetic measured interval, preserved failed attempts,
and refusal to compare incomplete or incompatible pairs. Reports must include
timing uncertainty. Raw Go benchmark output and `benchstat` are the
comparison interface; four daily pairs are direction checks, not an adoption
threshold.

KPC needs calibrated host capabilities and real successful counter reads before
decision use. The buffer API prerequisite alone is not that proof. Check-phase
top ten, holdouts, representative CLI runs, GC sensitivity, thermal/pressure
telemetry, and a layout optimization remain separate evidence requirements.

`kperf.ReadEventsInto` now accepts a caller-owned `[10]uint64` buffer. Allocate
separate start/end buffers before warmup; do not create a new local buffer on
each read. The legacy `ReadEvents` API remains available for existing callers.
The tagged tests exercise nil-buffer and unopened-PMU failures without reserving
the PMU:

```sh
cd tsc
go test -tags=binderinvestigation,kperf ./internal/testutil/kperf
```

These tests do not establish the allocation behavior of a successful PMU read.
That requires the host-specific M2 capability probe and measured calibration.

The next action after harness verification is to capture current checker access
evidence and select a representative expression visitor before judging a layout
change.

## Running the implementation

From the repository root, build the driver once outside the measured interval:

```sh
(cd tsc && go build -o /tmp/astbench ./cmd/astbench)
/tmp/astbench prepare --repo "$PWD" --before HEAD --after-working-tree \
  --out /tmp/astbench-runs --run-id daily-1
/tmp/astbench run --run /tmp/astbench-runs/daily-1 --lane daily
/tmp/astbench collect --run /tmp/astbench-runs/daily-1
/tmp/astbench report --run /tmp/astbench-runs/daily-1
/tmp/astbench inspect --repo "$PWD" --artifacts /tmp/astbench-runs/daily-1 \
  --bench BenchmarkTraversal
```

`--out` is the artifact parent and must be outside the selected repository,
including through symlinks. `prepare` rejects nested outputs before creating
files to keep generated snapshots out of source inputs.
`--run-id` names a new directory. Explicit
`--before REF --after REF` compares committed snapshots, even with unrelated
working-tree changes. Only `--after-working-tree` includes working-tree production
source. Both snapshots receive the same recorded harness overlay. A failed build
leaves its logs and partial snapshot; retry with a new run ID. Never edit a frozen
plan to resume it. `run` adds attempts for unfinished slots and preserves failures.

The default plan compares store against store: full-tree and expression,
256 and 16384 logical nodes, 256 traversals per process, four balanced A/B pairs
and four A/A controls per cell. Sizes are provisional; cache residency is not
asserted. To compare native legacy pointer against store from the same revision,
pass `--before HEAD --after HEAD` and
`--plan tsc/internal/astbench/examples/native-pointer-store.json` to `prepare`.
`representation` applies to before and A/A; optional `after_representation`
applies only to after. Both representations use the same logical recipe and
child order. This is not a comparison of two production parsers.

`wide`, `deep` (up to 8192 nodes), and `mixed` synthetic shapes are supported.
`fixture` uses the embedded `internal/astbench/workload/testdata/fixture.ts`,
requires `nodes: 0`, and is store-only. Full-tree follows schema child edges;
expression uses direct operand/callee/argument/property access and excludes
binary operator tokens and call type/optional-chain tokens. Other node kinds use
schema edges to discover nested expressions. Exact traces use the same walker,
including complete selected text payload. Timed checksums only detect changes;
the full traces establish equivalence. Kind, flags, location and selected text are
read; type inference and checker link work are omitted.

Only `construction` layout is implemented. Input `seed` changes the recipe;
`layout_seed` is recorded but has no effect for construction layout. Shuffled and
visit-order layouts, decision/KPC collection, ancestor/revisit and selective
visitors are rejected or unavailable. The conditional trace hook remains in the
concrete walker, disabled during measurement, with equal semantics on both
representations. Its overhead is part of this synthetic contract.

`collect` and `report` do not launch workers. Explicit
`collect --run RUN --out /tmp/reanalysis` regenerates derived raw benchmark files,
benchstat output and deterministic paired bootstrap estimates in a separate
output directory. It requires `benchstat` on PATH; otherwise it records missing
comparison support and retains raw files. The exact executable hash and Go build
information are saved. Four A/B pairs do not establish equivalence or adoption.

The cooperative campaign lock uses an OS advisory lock on macOS and Linux in
the current user's temporary directory, with a bounded wait. Other operating
systems report unsupported. SIGINT stops the current child and saves its attempt.
The OS releases lock ownership when its process exits, including after SIGKILL.
The lock file remains so all contenders share the same inode; do not delete it.
An unowned legacy PID file does not block acquisition. The lock cannot exclude
external profilers or unrelated machine load. Thermal/pressure/swap telemetry remains missing,
so results are daily direction checks only.

## Verification on 2026-09-17

The final package checks passed on Go 1.26.0 / darwin-arm64:

```sh
cd tsc
go test ./internal/astbench/... ./cmd/astbench ./internal/ast -count=1
go test -tags=binderinvestigation,kperf ./internal/testutil/kperf -count=1
```

The full CLI campaign `harness-native-20260917` completed with 64 valid process
samples and one preserved SIGINT attempt. Resuming a completed run left the
attempt count at 65. Recollection to a separate directory reproduced
`before.txt`, `after.txt`, and `paired.json` byte-for-byte. `inspect` reports
identity=current and stage=current for the selected repository and revision.
A preceding invalid campaign caught an orphan logical-ID count; the generator
was corrected and campaign-size regression tests were added before this run.

[Saved report](/Volumes/SanDisk1TB/worktree/ast-traversal-bench-evidence-20260917/report.md)
and [benchstat output](/Volumes/SanDisk1TB/worktree/ast-traversal-bench-evidence-20260917/analysis/benchstat.txt).
The complete campaign, including frozen sources, binaries, raw attempts and
proofs, is archived in that evidence directory. Paths in the original identity
are retained rather than rewritten; restore the recorded paths for the original
CLI, or regenerate the statistics directly from the saved raw text files.

These are rounded benchstat medians from an integration smoke run, not adoption
measurements:

| Visitor / logical nodes | Native legacy pointer ns/op | Store ns/op | B/op, both | allocs/op, both |
| --- | ---: | ---: | ---: | ---: |
| full-tree / 256 | 3149 | 4692 | 0 | 0 |
| expression / 256 | 1584 | 2344 | 0 | 0 |
| full-tree / 16384 | 190400 | 279500 | 0 | 0 |
| expression / 16384 | 99240 | 137910 | 0 | 0 |

All successful samples observed zero GC cycles. The direction in this limited
synthetic run favors the legacy pointer representation. There are only four A/B
pairs per cell, so benchstat cannot report a finite 95% median interval at this
sample count. A/A wall results also vary. This does not establish real-workload
performance, counter-level causes, cache residency, or an adoption decision.
Current hot-path ranks and construction/retained-memory allocation drivers
remain missing; the next action is representative checker-access profiling and
calibrated follow-up, not changing the layout on this smoke result alone.
