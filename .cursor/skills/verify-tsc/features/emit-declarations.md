# Emit declarations

Emit declarations lets a user generate `.d.ts` files that describe the public
types of their program.

## Sub-features

- `decl-with-js` writes `.d.ts` beside emitted `.js` when `--declaration` is
  set.
- `decl-only` writes only `.d.ts` when `--emitDeclarationOnly` is set.
- `decl-dir` honors `--declarationDir` as a separate output directory.
- `decl-contents` includes exported function names and types in the `.d.ts`.
- `decl-noemit` type-checks with `--declaration --noEmit` so declaration diagnostics run without writing files.

## How to get to it (user POV)

- Run `tsc --declaration --outDir dist app.ts`.
- Run `tsc --declaration --emitDeclarationOnly --outDir dist app.ts`.
- Run `tsc -p ./path/to/tsconfig.json` when that config sets `declaration`.
- Run `tsc --declaration --declarationDir types --outDir dist app.ts`.

## Driving it with control-tsc

Preconditions:

- `control-tsc doctor` is healthy for this checkout.

- **Create fixture.** Run `control-tsc fixture emit-declarations`. The fixture
  exports `greet(name: string): string` and its tsconfig sets `"declaration": true`
  and `"outDir": "dist"`.
- **Emit with JS.** Run `control-tsc cli -- -p /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/emit-declarations`. Exit code is `0`. Both `dist/index.js` and `dist/index.d.ts` exist.
- **Check dts text.** `dist/index.d.ts` contains `greet` and `string`.
- **Declaration dir.** Run `control-tsc cli -- --ignoreConfig --declaration --declarationDir /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/emit-declarations/types --outDir /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/emit-declarations/js --module commonjs /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/emit-declarations/src/index.ts`. Exit code is `0`. `types/index.d.ts` exists. `js/index.js` exists.
- **Emit only dts.** Run `control-tsc cli -- --ignoreConfig --declaration --emitDeclarationOnly --outDir /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/emit-declarations/dts-only --module commonjs /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/emit-declarations/src/index.ts`. Exit code is `0`. `dts-only/index.d.ts` exists. No `dts-only/index.js`.
- **Declaration diagnostics without emit.** Run `control-tsc cli -- -p /tmp/verify-tsc-$VERIFY_TSC_RUN_ID/emit-declarations --noEmit --declaration`. Exit code is `0`. No extra `.d.ts` is required from this drive.
- **Proof.** Keep the `.d.ts` text in the artifact directory and the `cli`
  transcripts. A `.js` file alone does not prove this feature.

## Gotchas

- `--declaration` without a successful type-check may skip emit (exit 1) or
  still emit (exit 2) depending on `noEmitOnError`. The fixture is well-typed
  so the expected exit is 0.
- `--emitDeclarationOnly` without `--declaration` is not a valid substitute.
  Pass both, or set them in tsconfig.
- Do not use checker `*.d.ts` baselines under `tsc/testdata/` as the user
  output path.
