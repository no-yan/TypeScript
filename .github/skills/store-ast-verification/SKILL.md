---
name: store-ast-verification
description: >
  Verify Store-backed AST changes with unit and race tests, generated-code
  reproducibility, real TypeScript compiler smoke checks and emit, and
  interleaved performance comparison against the parent or trunk.
---

# Verifying Store-backed AST changes

Use this skill for changes under `tsc/internal/ast`, parser Store producers,
binder/checker Store state, or transformer/printer Store consumers.

Tests alone are not sufficient. A change is verified only when its unit, race,
live, and performance gates all pass at the exact pushed SHA.

## 1. Establish the comparison

Record:

- current branch and SHA;
- required parent branch for stacked changes;
- clean worktree state;
- Go version required by `tsc/go.mod`;
- the exact workload used for both heads.

Fetch the parent only when its latest remote state is required. Never compare
different workloads, generated files, compiler options, or machines.

## 2. Prepare the real smoke project

The CI workload is the TypeScript v6.0.3 compiler project:

```bash
git clone --depth 1 --branch v6.0.3 \
  https://github.com/microsoft/TypeScript.git /tmp/typescript-6.0
npm ci --prefix /tmp/typescript-6.0
npx --prefix /tmp/typescript-6.0 hereby generate-diagnostics
```

Use `/tmp/typescript-6.0/src/compiler` as the project path. A generated toy
file may support a microbenchmark but must not replace this live workload.

If Go toolchain auto-download fails, obtain the exact released toolchain
required by `tsc/go.mod`; do not lower `go.mod` or `go.work`.

## 3. Unit and race gates

Run the packages touched by the change, then the complete Store pipeline:

```bash
go -C ./tsc test \
  ./internal/ast \
  ./internal/parser \
  ./internal/binder \
  ./internal/checker \
  ./internal/compiler \
  ./internal/printer \
  ./internal/transformers/... \
  -count=1

go -C ./tsc test -race \
  ./internal/ast \
  ./internal/parser \
  ./internal/binder \
  ./internal/checker \
  ./internal/printer \
  ./internal/compiler \
  -count=1
```

For config Store consumers, also run:

```bash
go -C ./tsc test ./internal/tsoptions -count=1
go -C ./tsc test -race ./internal/tsoptions -count=1
```

Race coverage must exercise same-file checker or emitter mutation, not merely
parallel writes to different Stores.

## 4. Generated-code gate

Run the AST generator and format its Go outputs:

```bash
node --experimental-strip-types tools/scripts/tsc/generate-go-ast.ts
gofmt -w \
  tsc/internal/ast/ast_generated.go \
  tsc/internal/ast/kind_generated.go \
  tsc/internal/ast/store_schema_generated.go \
  tsc/internal/ast/store_handles_generated.go
git diff --exit-code
```

A diff means the committed generated files are stale. Regenerate, commit, and
repeat all gates at the new SHA.

## 5. Live compile and emit gates

Build the same noembed binary used by CI:

```bash
npx hereby build
```

Then run both checking and real output generation:

```bash
./built/local/tsc \
  -p /tmp/typescript-6.0/src/compiler \
  --noEmit

./built/local/tsc \
  -p /tmp/typescript-6.0/src/compiler \
  --outDir /tmp/store-smoke-out \
  --declarationMap false \
  --sourceMap false
```

Both commands must exit zero. Confirm the output directory contains nonempty
JavaScript and declaration files. A module-resolution failure or accepted
nonzero exit is not a live check.

Run a compiler test to cover diagnostics, emit baselines, and parent pointers:

```bash
go -C ./tsc test ./internal/testrunner \
  -count=1 \
  -run 'TestLocal/alias'
```

Use `npx hereby test` for the final full-suite gate.

## 6. Store invariants

Verify the applicable invariants:

```bash
git grep -n ExpandStore -- 'tsc/**/*.go'
git grep -n FlattenNode -- \
  'tsc/internal/**/*.go' \
  ':!tsc/internal/ast/store_flatten.go' \
  ':!tsc/internal/ast/store_*bench*' \
  ':!tsc/internal/ast/store_e2e*'
git grep -n GetNodeId -- \
  tsc/internal/checker \
  tsc/internal/printer \
  tsc/internal/transformers
```

Expected:

- no production `ExpandStore`;
- no production `FlattenNode`;
- identity side tables use `GlobalRef`, with documented fallback only for
  detached test nodes;
- direct `SetChild` still rejects cross-Store handles;
- exceptional cross-file synthetic edges use sparse `GlobalRef` side tables;
- SourceFile metadata remains associated with the canonical Store;
- parser, checker, and emit writers hold the per-file ownership required by
  the Store concurrency policy.

## 7. Performance gate

Build one binary per head and place both beside the same lib files. Alternate
runs to avoid cache, load, and thermal bias:

```bash
for run in 1 2 3 4 5; do
  ./built/local/tsc-parent -p /tmp/typescript-6.0/src/compiler --noEmit
  ./built/local/tsc        -p /tmp/typescript-6.0/src/compiler --noEmit
done
```

Measure wall time externally and report every sample plus each median. Apply
the threshold from the active Store upstream plan:

- PR 2 through PR 5: compare with the direct parent;
- PR 6: head/PR-5 must be at most `1.10`;
- PR 7 and upstream: head/trunk must be at most `1.05`.

Also compare default GOGC and `GOGC=off`. The memory-limit stop rule is:

- fail when default/off is below `1.10`; or
- fail when `GOMEMLIMIT=8GiB`/off is at most `1.05`, because the no-code
  memory limit has then recovered nearly all of the GC-off result.

Do not hide a failed trunk gate behind a passing parent-relative gate.

## 8. Evidence and verdict

Append exact commands, medians, ratios, and the final SHA to
`tsc/.audit/store-open-questions.tsv`.

Verdict is `PASS` only when:

1. unit tests pass;
2. race tests pass;
3. generated output is reproducible;
4. real smoke check exits zero;
5. real smoke emit exits zero with nonempty output;
6. Store invariants hold;
7. the required performance ratio passes;
8. the worktree is clean and the verified SHA is pushed.

Any new commit invalidates the verdict and requires all applicable gates to be
rerun.
