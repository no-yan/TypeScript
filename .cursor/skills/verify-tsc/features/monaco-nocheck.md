# Bind-only monaco project (no check)

A user can load a `tsconfig.json` with `--project`, skip the checker with
`--noCheck`, and skip emit so the compiler still parses and binds a large
program. This is the vscode monaco workload used to measure that path.

## Sub-features

- `monaco-project` compiles `src/tsconfig.monaco.json` from a vscode tree via
  `--project`.
- `monaco-nocheck` skips full type-checking with `--noCheck`.
- `monaco-noemit` does not write `.js` or `.d.ts` (`--noEmit` and
  `--declaration false`).
- `monaco-pmc` records Instruments CPU Counters for that same argv. It does
  not use pprof.

## How to get to it (user POV)

- From a vscode checkout, run
  `tsc --project src/tsconfig.monaco.json --noEmit --noCheck --declaration false`.
- To measure CPU counters, record that launch with Instruments CPU Counters
  (`xctrace record --template 'CPU Counters'` or a saved `.tracetemplate`).

## Driving it with control-tsc

Preconditions:

- `control-tsc doctor` is healthy for this checkout.
- Launch used `control-tsc launch --embed`. `go version -m built/local/tsc`
  must not list `noembed`. xctrace relocates the launched file; a noembed
  binary then cannot find `lib.es5.d.ts` and prints `Cannot find global type
  'Array'`.
- A vscode tree exists at `$VERIFY_TSC_VSCODE_ROOT` (default
  `/Users/noyan/ghq/github.com/microsoft/vscode`) and contains
  `src/tsconfig.monaco.json`.
- macOS with `xcrun xctrace` for `monaco-pmc`. If xctrace is missing, report
  `verified-unreachable` for `monaco-pmc` only; still drive `monaco-project`
  with `control-tsc cli`.
- Do not use `go test`, `pprof`, or `pprof` CPU profiles as this proof.

- **Wall-clock compile.** Run
  `control-tsc cli --cwd "$VERIFY_TSC_VSCODE_ROOT" --slug monaco-nocheck -- --project src/tsconfig.monaco.json --noEmit --noCheck --declaration false`.
  Exit code is `0`. The vscode tree has no new compiler `.js` next to sources
  from this drive.
- **Bind wall time (optional companion).** Run the same command with
  `--extendedDiagnostics`. stdout contains `Bind time`. This is wall-clock
  phase time, not a PMC. It does not replace `monaco-pmc`.
- **PMC.** Run
  `control-tsc pmc --cwd "$VERIFY_TSC_VSCODE_ROOT" --slug monaco-pmc -- --project src/tsconfig.monaco.json --noEmit --noCheck --declaration false`.
  `xctrace` exit is `0`. The artifact contains `cpu-counters.trace`, `toc.xml`,
  and `pmc-summary.txt`. `pmc-summary.txt` names the launched process as `tsc`
  (or the binary basename) and `pprof=forbidden`.
- **Proof.** Keep the `cli` transcript (argv includes `--project`, `--noCheck`,
  `--noEmit`) and the `.trace` plus `pmc-summary.txt`. A `BenchmarkBind` or
  pprof profile is not this feature.

## Gotchas

- `--project` is the long name of `-p`. Project-mode recipes that only use
  `-p` do not prove this argv.
- `--noCheck` still parses and binds. It is not `--noEmit` and it is not a
  type-check. Do not file this under type-check.md.
- Default user template `cpu-counter.tracetemplate` and stock `CPU Counters`
  are Guided CPU Bottlenecks: time/PMI samples and bottleneck ratios, not a
  process-lifetime instruction retired total. For instruction totals, save a
  Custom CPU Counters template that counts instruction events and set
  `VERIFY_TSC_XCTRACE_TEMPLATE`.
- `xctrace record` overhead is often much larger than tsc wall time. Compare
  PMC totals (or bottleneck samples) from the `.trace`, not the harness
  `duration_s`.
- xctrace has `--target-stdout` but no `--target-stderr`. Compiler stderr may
  mix with xctrace logs in `xctrace-stderr.txt`.
- Never write emit into the vscode git tree. This recipe always passes
  `--noEmit`.
- Never invoke `go tool pprof` or `http://localhost:6060` for this proof.
- `npx hereby build` is always `-tags=noembed`. There is no hereby `--embed`.
  Use `control-tsc launch --embed`.
