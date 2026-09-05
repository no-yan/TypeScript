# Project mode

Project mode lets a user compile from `tsconfig.json`, scaffold that file with
`--init`, and build composite projects with `-b`.

## Sub-features

- `project-p` compiles the program named by `-p <dir>` or `-p <tsconfig>`.
- `project-init` writes a `tsconfig.json` in an empty directory.
- `project-build` builds a composite project with `tsc -b`.
- `project-help` shows common commands including `-p` and `--init` via
  `--help`.

## How to get to it (user POV)

- Run `tsc -p ./path/to/tsconfig.json`.
- Run `tsc -p ./path/to/project`.
- Run `tsc --init` in a directory.
- Run `tsc -b` in a composite project.
- Run `tsc --help`.

## Driving it with control-tsc

Preconditions:

- `control-tsc doctor` is healthy for this checkout.

- **Help entry.** Run `control-tsc cli -- --help`. Exit code is `0`. stdout
  contains `tsc: The TypeScript Compiler` and the examples `tsc -p` and
  `tsc --init`.
- **Create fixture.** Run `control-tsc fixture project-mode`. This writes a
  root `tsconfig.json` with `"files": []` and `"references"` to `pkg-a` and
  `pkg-b`, plus two composite packages where `pkg-b` references `pkg-a`.
- **Path to file.** Run `control-tsc cli -- -p /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/project-mode/pkg-a/tsconfig.json --noEmit`. Exit code is `0`.
- **Path to directory.** Run `control-tsc cli -- -p /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/project-mode/pkg-a --noEmit`. Exit code is `0`.
- **Init.** Run `control-tsc cli --cwd /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/project-mode/empty -- --init`. Exit code is `0`. `empty/tsconfig.json` exists and is nonempty.
- **Build mode.** Run `control-tsc cli -- -b /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/project-mode --force`. Exit code is `0`. `pkg-a/dist/index.js` and `pkg-b/dist/index.js` exist.
- **Proof.** Keep the `--help` transcript, the generated `tsconfig.json` from
  `--init`, and a listing of `-b` output files. `-p --noEmit` alone does not
  prove `--init` or `-b`.

## Gotchas

- `tsc` with no arguments and no `tsconfig.json` prints help and exits 1.
  That is not a successful project compile.
- `--init` writes into cwd. Always pass `--cwd` under scratch. Never run it
  in the repo root.
- `-b` without `composite` / `references` is the wrong entry. Use the
  `project-mode` fixture.
- `--build --help` is a different help text from `tsc --help`. Use `tsc --help`
  for the common-commands proof.
