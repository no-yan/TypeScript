---
name: verify-tsc
description: >
  Drive the native TypeScript compiler CLI (built/local/tsc, also called tsgo)
  the way a user does: build it, type-check, emit JavaScript and declarations,
  and capture diagnostics. Use when proving compiler, parser, checker, printer,
  or CLI changes work; after Store AST work; before claiming a PR is verified;
  or when the user asks to launch, smoke, or demo tsc.
---

# Verify the TypeScript compiler CLI

This repo's user-facing app is the native compiler binary `built/local/tsc`.
A user type-checks and emits from a terminal. Package tests, fourslash cases,
and Store unit tests are not this skill. Drive the real binary against an
isolated project, then capture the command, the result, and any files it wrote.

Secondary surfaces exist and are out of scope unless a mapped feature names
them: `tsc --lsp --stdio`, `tsc --api`, the VS Code extension under
`packages/`, and the npm `typescript` package used to build this repo.

Do not use `npx tsc`, `npx typescript`, or `node_modules/typescript` as the
app under test. Those are a different compiler.

## Launch

There is no long-lived server. Launch means build the noembed CI binary once,
then run each drive as its own short-lived process.

From the repository root, with Go matching `tsc/go.mod` (currently 1.26) on
`PATH`. If the system Go cannot download that toolchain, use a local 1.26
install (this environment keeps one at `/tmp/go1.26`) and set
`GOTOOLCHAIN=local`:

```bash
export VERIFY_TSC_RUN_ID="${VERIFY_TSC_RUN_ID:-$(date +%Y%m%dT%H%M%S)-$$}"
./.cursor/skills/verify-tsc/scripts/control-tsc launch
```

Ready when all of these hold:

- `built/local/tsc` exists and is executable
- `built/local/lib.es5.d.ts` exists (hereby copies bundled libs next to the
  noembed binary)
- `./built/local/tsc --version` prints `Version ` plus `core.Version()`
  (today `Version 7.1.0-dev`)
- `control-tsc doctor` exits 0

Teardown is `control-tsc cleanup`. It deletes the run's scratch directory
under `/tmp/verify-tsc-<run-id>/` and any watch PIDs recorded in that
scratch. It does not delete `built/local/tsc` and it does not delete
evidence under `.cursor/skills/verify-tsc/artifacts/`.

## Doctor

Run this first whenever a drive looks wrong, after a rebuild, or before
taking proof:

```bash
./.cursor/skills/verify-tsc/scripts/control-tsc doctor
```

Doctor is read-only. It fails unless:

- `Herebyfile.mjs` is the repo root and `tsc/go.mod` is readable
- `go version` reports the `go` line from `tsc/go.mod`
- `built/local/tsc` is an executable regular file owned by this checkout
- `built/local/lib.es5.d.ts` sits next to the binary
- `built/local/tsc --version` stdout is `Version ` followed by the string in
  `tsc/internal/core/version.go`
- `built/local/tsc --help` stdout contains `tsc: The TypeScript Compiler`
- if a launch state file exists, its git SHA matches `git rev-parse HEAD`

Never drive a binary from another worktree, another checkout, or `PATH`.

## Drive

Read `features/README.md`, then the feature file for the path under test. A
proof that only hits one convenient entry point is incomplete when the map
lists others.

Harness:

```bash
./.cursor/skills/verify-tsc/scripts/control-tsc fixture <feature-id>
./.cursor/skills/verify-tsc/scripts/control-tsc cli -- -p "$SCRATCH/<feature-id>" <flags>
```

`control-tsc fixture` prints `SCRATCH=...`. `control-tsc cli` always records
cwd, argv, stdout, stderr, exit code, and duration under
`.cursor/skills/verify-tsc/artifacts/$VERIFY_TSC_RUN_ID/`.

Rules:

- Isolate every drive in `/tmp/verify-tsc-$VERIFY_TSC_RUN_ID/`. Put `outDir`,
  `declarationDir`, tsbuildinfo, and `--init` output there. Do not write
  compiler output into the git worktree.
- Invoke only `built/local/tsc` through `control-tsc cli`. Pass compiler
  flags after `--`.
- Prefer project mode (`-p <dir-or-tsconfig>`) and explicit file lists over
  relying on a hidden cwd tsconfig.
- Stable handles are compiler flags and paths: `--noEmit`, `--declaration`,
  `--outDir`, `-p`, `--init`. Do not drive by terminal coordinates.
- Exit 0 means success. Nonzero means diagnostics: `1` is skipped emit
  (`ExitStatusDiagnosticsPresent_OutputsSkipped`) and `2` is diagnostics
  with emit (`ExitStatusDiagnosticsPresent_OutputsGenerated`). `--noEmit`
  on a known type error is still a passing proof when the text contains
  `error TS` and the exit is nonzero; do not require a specific `1` vs `2`.
  Treat an unexpected 0 on a known-bad file as failure.
- Refuse `--watch` unless the feature file says to use it. Watch is a
  long-running PTY; record its PID in scratch and kill that PID in cleanup.
- Concurrent drives are safe when each uses its own scratch `outDir`. Do not
  share a watch instance or a tsbuildinfo file across runs.
- `npx hereby test` and `go test` can catch regressions. They do not replace
  a mapped user path.

## Evidence

Proof lives in `.cursor/skills/verify-tsc/artifacts/$VERIFY_TSC_RUN_ID/` and
must survive cleanup.

Each drive directory contains `meta.txt` (command, cwd, SHA, duration, exit
code), `stdout.txt`, `stderr.txt`, and when emit happened `outputs.txt` plus
copies or head dumps of the written `.js` / `.d.ts` files.

Standards:

- Exercise the real CLI path a user would run, not `parser.ParseSourceFile`,
  `execute.CommandLine` from a Go test, or Store flatten helpers.
- Capture the invocation and the resulting state: exit code, diagnostics
  text, and files on disk. A screenshot of `--help` is not proof of emit.
- For type-check, prove silence and exit 0, then prove a known error still
  reports.
- For emit, prove the output file exists, is nonempty, and contains the
  expected JavaScript or declaration text. Then prove a second invocation or
  a file read, not only the first process exit.
- Mocks are not used. The only allowed stand-in is an isolated fixture
  project; it is still compiled by the real binary. The CI smoke at
  `smoke/typescript-6.0/src/compiler` (TypeScript v6.0.3) is the large
  live workload when a feature file asks for it. A one-line toy file must
  not replace that workload for performance or Store e2e claims.

## Cleanup

```bash
./.cursor/skills/verify-tsc/scripts/control-tsc cleanup
```

Cleanup removes `/tmp/verify-tsc-$VERIFY_TSC_RUN_ID/` and sends `TERM` then
`KILL` to PIDs listed in that directory's `pids` file. It never `pkill tsc`.
It never deletes `built/` or `artifacts/`. After cleanup, confirm the
artifact directory still exists and `meta.txt` is readable.

## Helpers

All helpers are `.cursor/skills/verify-tsc/scripts/control-tsc`. Make sure it
is executable (`chmod +x`).

| Command | What it does |
| --- | --- |
| `control-tsc launch` | Ensures Go, builds `built/local/tsc` with `npx hereby build`, records run state |
| `control-tsc doctor` | Read-only health check of this checkout's binary and libs |
| `control-tsc fixture <id>` | Writes an isolated project for a mapped feature id |
| `control-tsc cli -- [tsc args]` | Runs `built/local/tsc` and writes a transcript under `artifacts/` |
| `control-tsc cleanup` | Deletes scratch and recorded child PIDs; keeps artifacts |

Example:

```bash
export VERIFY_TSC_RUN_ID=demo
./.cursor/skills/verify-tsc/scripts/control-tsc launch
./.cursor/skills/verify-tsc/scripts/control-tsc doctor
./.cursor/skills/verify-tsc/scripts/control-tsc fixture type-check
./.cursor/skills/verify-tsc/scripts/control-tsc cli -- -p /tmp/verify-tsc-demo/type-check --noEmit
./.cursor/skills/verify-tsc/scripts/control-tsc cleanup
ls .cursor/skills/verify-tsc/artifacts/demo
```

Store AST adversarial gates (unit, race, generate reproducibility, interleaved
perf vs parent/trunk) live in `.github/skills/store-ast-verification/SKILL.md`.
Use that skill in addition to this one when the change is Store-backed AST.
This skill still has to pass: a Store change that only has green `go test` is
not user-path verified.
