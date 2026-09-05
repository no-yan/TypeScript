# Type-check without emit

Type-check without emit lets a user ask the compiler to report type and parse
errors without writing `.js` or `.d.ts` files.

## Sub-features

- `check-files` type-checks files named on the command line with `--noEmit`.
- `check-project` type-checks a `tsconfig.json` via `-p` and `--noEmit`.
- `check-clean` exits 0 and prints no diagnostics on a well-typed program.
- `check-smoke` type-checks the CI TypeScript v6.0.3 compiler project when
  that tree is present.

## How to get to it (user POV)

- Run `tsc --noEmit app.ts`.
- Run `tsc --noEmit` in a directory that contains `tsconfig.json`.
- Run `tsc -p ./path/to/tsconfig.json --noEmit`.
- Run `tsc -p ./path/to/project --noEmit`.

## Driving it with control-tsc

Preconditions:

- `control-tsc doctor` is healthy for this checkout.
- Scratch `/tmp/verify-tsc-$VERIFY_TSC_RUN_ID/` is empty of prior fixtures.
- The npm `typescript` package is not on `PATH` as `tsc`.

- **Create fixture.** Run `control-tsc fixture type-check`. The printed
  `SCRATCH` directory contains `src/index.ts` and `tsconfig.json` with
  `"noEmit": true` and `"strict": true`.
- **Check project.** Run `control-tsc cli -- -p /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/type-check --noEmit`. Exit code is `0`. stdout and stderr contain no `error TS`.
- **Check files.** Run `control-tsc cli -- --noEmit --strict --ignoreConfig /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/type-check/src/index.ts`. Exit code is `0`. No `.js` file appears next to `src/index.ts` or under the fixture.
- **Confirm no emit.** Run `find /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/type-check -name '*.js' -o -name '*.d.ts'`. The only hits, if any, are not compiler output from this drive.
- **Optional smoke.** If `smoke/typescript-6.0/src/compiler` or
  `/tmp/typescript-6.0/src/compiler` exists, run
  `control-tsc cli -- -p <that-path> --noEmit`. Exit code is `0`. This is
  the CI smoke from `.github/workflows/ci.yml`. Do not substitute a one-line
  file when claiming Store or performance e2e.
- **Proof.** Keep the `cli` transcript that shows argv `--noEmit`, exit `0`,
  and empty diagnostics. Re-read the fixture and confirm no new `.js`.

## Gotchas

- `hereby build` uses `-tags=noembed`. If `lib.es5.d.ts` is missing next to
  the binary, type-check fails with missing lib diagnostics that are not a
  user-program error.
- Naming files on the command line while a `tsconfig.json` exists in cwd
  errors unless `--ignoreConfig` is set. Drive file lists from scratch or
  pass `--ignoreConfig`.
- Exit 0 with a missing `./_namespaces/ts.js` or similar module-resolution
  failure is not a clean check. Read stdout.
- `go test ./internal/checker` passing is not this feature.
