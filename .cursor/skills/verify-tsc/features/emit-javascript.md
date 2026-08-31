# Emit JavaScript

Emit JavaScript lets a user compile TypeScript sources to `.js` files they can
run or ship.

## Sub-features

- `emit-files` writes `.js` for files named on the command line.
- `emit-project` writes `.js` under `outDir` from `-p`.
- `emit-contents` produces readable JavaScript that contains the program's
  exported symbols.
- `emit-no-dts` does not write `.d.ts` unless `--declaration` is set.

## How to get to it (user POV)

- Run `tsc app.ts --outDir dist`.
- Run `tsc -p ./path/to/tsconfig.json`.
- Run `tsc` in a directory whose `tsconfig.json` sets `outDir`.

## Driving it with control-tsc

Preconditions:

- `control-tsc doctor` is healthy for this checkout.
- Scratch has no leftover `dist` from another feature.

- **Create fixture.** Run `control-tsc fixture emit-javascript`. The fixture
  `src/index.ts` exports `greet`, and `tsconfig.json` sets `"outDir": "dist"`,
  `"rootDir": "src"`, `"module": "commonjs"`, `"target": "es2020"`, and does not set `noEmit`.
- **Emit project.** Run `control-tsc cli -- -p /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/emit-javascript`. Exit code is `0`.
- **Read output.** Open
  `/tmp/verify-tsc-$VERIFY_TSC_RUN_ID/emit-javascript/dist/index.js`. The file
  is nonempty and contains `greet` and `Hello`.
- **Emit files.** Run `control-tsc cli -- --ignoreConfig --outDir /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/emit-javascript/from-files --module commonjs --target es2020 /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/emit-javascript/src/index.ts`. Exit code is `0`. `from-files/index.js` exists and contains `greet`.
- **No declarations.** Confirm no `index.d.ts` next to those `.js` files.
- **Proof.** Copy or head-dump the `.js` into the artifact directory. Capture
  `outputs.txt` from `control-tsc cli`. A zero exit without a nonempty `.js`
  is a failed proof.

## Gotchas

- `--noEmit` from a copied tsconfig silently skips this feature. Use the
  `emit-javascript` fixture, not `type-check`.
- Default emit without `outDir` writes `.js` next to the `.ts` file. Always
  pass `--outDir` under scratch so cleanup can delete it.
- Incremental `tsbuildinfo` beside `outDir` is output, not proof of JS emit.
- Do not treat `npx hereby test` baseline `.js` files as this user path.
