# tsc verification map

This directory is the maintained source for verifying the user-facing behavior
of the native TypeScript compiler CLI. Read this index before driving the app,
then use the matching feature file as the recipe.

## Baseline preconditions

- Work from the repository root that contains `Herebyfile.mjs`.
- Export a unique `VERIFY_TSC_RUN_ID` so concurrent runs do not share scratch.
- Run `./.cursor/skills/verify-tsc/scripts/control-tsc launch` then `doctor`.
- Require doctor to report this checkout's `built/local/tsc`, the git SHA of
  `HEAD`, `Version` matching `tsc/internal/core/version.go`, and
  `built/local/lib.es5.d.ts`.
- Put compiler output only under `/tmp/verify-tsc-$VERIFY_TSC_RUN_ID/`.
- Never drive a `tsc` from `PATH`, `npx`, or `node_modules`.

## Driving conventions

- Start every recipe from a fresh fixture unless its preconditions say
  otherwise.
- Treat every command as literal. Keep flags and quoted paths unchanged.
- Run compiler actions through `control-tsc cli -- `.
- Restore nothing in the git worktree; fixtures live in scratch.
- Do not remove proof artifacts during cleanup.

## Proof and skip reporting

- Capture the user action and the resulting state, not only the final exit
  code.
- CLI proof includes the command, stdout, stderr, and exit code.
- Mutation proof (emit) includes a read of the written `.js` or `.d.ts`.
- Record the feature ID and entry point used with every artifact.
- Report an unreachable path with the attempted command and the unmet
  precondition.
- Do not report a skipped entry point as verified through a different path.

## Feature entry contract

Each feature file starts with an H1 title and one paragraph describing the
user-visible behavior. It then uses exactly four H2 sections in this order.

1. `Sub-features` lists short IDs with one line for each behavior.
2. `How to get to it (user POV)` lists every user entry point.
3. `Driving it with control-tsc` starts with `Preconditions:` and uses labeled
   bullets that pair each user action with an exact command and observable
   result.
4. `Gotchas` lists traps that can waste or invalidate a verification run.

Keep implementation details out of the map. Name only user paths, stable
handles, required state, commands, and observable proof.

## Features

- [Type-check without emit](./type-check.md) covers `--noEmit` on a file list
  and on a project, including a clean program and the CI compiler smoke.
- [Emit JavaScript](./emit-javascript.md) covers writing `.js` from a file
  list and from `-p`, and checking the output on disk.
- [Emit declarations](./emit-declarations.md) covers `--declaration` and
  `--declarationDir` output a user would ship.
- [Report diagnostics](./report-diagnostics.md) covers type errors, bad flags,
  and missing project paths.
- [Project mode](./project-mode.md) covers `-p`, `--init`, and `--build`.
