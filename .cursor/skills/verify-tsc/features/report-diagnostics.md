# Report diagnostics

Report diagnostics lets a user see compiler errors for bad programs, bad
flags, and missing config files instead of a silent success.

## Sub-features

- `diag-type` prints a type error for a well-known mismatch.
- `diag-exit` uses exit code 1 when emit is skipped because of errors.
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
- **Type error.** Run `control-tsc cli -- -p /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/report-diagnostics --noEmit`. Exit code is nonzero (`1` if emit was skipped, `2` if the compiler still classified outputs as generated). stdout or stderr contains `error TS` and `not assignable`.
- **No output files.** Confirm the fixture has no new `.js`.
- **Missing project.** Run `control-tsc cli -- -p /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/does-not-exist`. Exit code is `1`. Output mentions that the specified path does not exist or that `tsconfig.json` cannot be found.
- **Mixed -p and files.** Run `control-tsc cli -- -p /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/report-diagnostics /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/report-diagnostics/src/index.ts`. Exit code is `1`. Output says option `project` cannot be mixed with source files.
- **Proof.** Keep the transcript that shows the type error text and a
  nonzero exit. Exit `0` on this fixture is a failed proof, even if tests
  elsewhere pass.

## Gotchas

- Pretty diagnostics include file paths and line numbers. Assert on
  `error TS` and `not assignable`, not on a full pretty-printed box.
- Locale flags change the diagnostic language. Drive without `--locale`
  unless the feature under test is localization.
- Watch mode reprints errors on change. Do not use `--watch` here.
- A panic or race failure is not a diagnostic. Capture stderr and fail the
  proof; do not treat a crash as "reported an error".
