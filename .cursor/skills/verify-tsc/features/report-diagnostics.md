# Report diagnostics

Report diagnostics lets a user see compiler errors for bad programs, bad
flags, and missing config files instead of a silent success.

## Sub-features

- `diag-type` prints a type error for a well-known mismatch.
- `diag-exit` uses a nonzero exit: `1` when emit is skipped, `2` when the
  compiler still classified outputs as generated. This fixture's type error
  with `--noEmit` has been observed as `2`.
- `diag-project-missing` reports when `-p` points at a path with no
  `tsconfig.json`.
- `diag-flag` reports invalid flag combinations such as files plus `-p`.

## How to get to it (user POV)

- Run `tsc --noEmit` on a file that contains a type error.
- Run `tsc -p ./missing-project`.
- Run `tsc -p tsconfig.json app.ts`.

## Driving it with control-tsc

Preconditions:

- `control-tsc doctor` is healthy for this checkout.

- **Create fixture.** Run `control-tsc fixture report-diagnostics`.
  `src/index.ts` contains `const x: number = "hello";` and tsconfig sets
  `"strict": true` and `"noEmit": true`.
- **Type error.** Run `control-tsc cli -- -p /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/report-diagnostics --noEmit`. Exit code is nonzero (`1` or `2`). `stdout.txt` contains `error TS` and `not assignable`. Compiler diagnostics go to stdout, not stderr.
- **No output files.** Confirm the fixture has no new `.js`.
- **Missing project.** Run `control-tsc cli -- -p /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/does-not-exist`. Exit code is `1`. For a missing file path, stdout says the specified path does not exist. For a directory without `tsconfig.json`, stdout says it cannot find `tsconfig.json`.
- **Mixed -p and files.** Run `control-tsc cli -- -p /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/report-diagnostics /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/report-diagnostics/src/index.ts`. Exit code is `1`. Output says option `project` cannot be mixed with source files.
- **Proof.** Keep the transcript that shows the type error text and a
  nonzero exit. Exit `0` on this fixture is a failed proof, even if tests
  elsewhere pass.

## Gotchas

- Pretty diagnostics include file paths and line numbers. Assert on
  `error TS` and `not assignable` in `stdout.txt`, not on a full pretty-printed
  box and not on stderr (stderr is for crashes and harness noise).
- Locale flags change the diagnostic language. Drive without `--locale`
  unless the feature under test is localization.
- Watch mode reprints errors on change. Do not use `--watch` here.
- A panic or race failure is not a diagnostic. Capture stderr and fail the
  proof; do not treat a crash as "reported an error".
