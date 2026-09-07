# Lazy TS JSDoc parsed into the checker's synth Store (2026-09-07)

Before this change `Program.BindSourceFiles` called `SourceFile.WarmJSDoc` on every file after bind and before `Freeze`, parsing every deferred TS JSDoc into the parse Store under the writer lease. That made the parse Store grow after parse (breaking an exact `Compact`), paid the full JSDoc parse even under `--noCheck`, and ran regardless of whether a checker ever read the comment.

Now deferred TS JSDoc is parsed on first access, like upstream, but never into the parse Store:

- Checker: `Handle.JSDocIn(file, c.jsdoc)`; `c.jsdoc` is an `ast.JSDocCache` on `Checker.synth`, one per checker, keyed by `GlobalRef`. The JSDoc root's parent is an `externalParent` on the synth Store.
- LS / API / astnav: `Handle.JSDoc(file)` warms the whole file's deferred JSDoc once into a per-file side Store (`warmSharedJSDoc`), freezes it, then publishes through `sync.Once`. The compile path never creates it (`TestCheckParsesDeferredJSDocOutsideParseStore`).
- `Handle.EagerJSDoc` returns only parser-time JSDoc (JS files, TS comments with `@see`/`@link`).

## Measurement

VS Code `src/tsconfig.monaco.json`, noembed build (`CGO_ENABLED=0 go build -trimpath -tags=noembed`), M1 8 core / 8 GB, base = `7c97166f5e`, 5 interleaved runs per variant, medians of `--extendedDiagnostics`.

| Mode | Metric | base | new | Δ |
| --- | --- | --- | --- | --- |
| default (4 checkers) | Bind | 0.068s | 0.046s | −32% |
| default | Check | 1.072s | 1.104s | +3% (runs overlap: 0.918–1.111 vs 0.906–1.170) |
| default | Total | 2.070s | 2.131s | +3% (runs overlap: 1.987–2.144 vs 1.911–2.150) |
| default | Memory used | 850.1 MB | 845.7 MB | −0.5% |
| default | Memory allocs | 11.30 M | 11.12 M | −1.6% |
| `--singleThreaded` | Bind | 0.173s | 0.138s | −20% |
| `--singleThreaded` | Check | 2.002s | 2.015s | +0.6% (1.994–2.040 vs 2.003–2.024) |
| `--singleThreaded` | Total | 3.340s | 3.314s | −0.8% |
| `--singleThreaded` | Memory used | 686.0 MB | 681.5 MB | −0.6% |
| `--singleThreaded` | Memory allocs | 9.82 M | 9.64 M | −1.8% |
| `--noCheck` | Bind | 0.062s | 0.044s | −29% |
| `--noCheck` | Total | 1.343s | 1.329s | −1% |
| `--noCheck` | Memory used | 457.7 MB | 451.9 MB | −1.3% |
| `--noCheck` | Memory allocs | 8.38 M | 8.17 M | −2.6% |

The `WarmJSDoc` cost was inside the Bind phase timer, which is where the win shows. An overlay-instrumented build counted the work that moved into check: 19,442 deferred JSDoc hosts across the 1,629 files, of which checkers parsed 3,137 (`--singleThreaded`) or 3,167 (4 checkers; only 30 hosts parsed by more than one checker). So 84% of the JSDoc parse work is gone on this workload, and what remains is spread across the check phase, where it is within run-to-run noise. Per-file lib JSDoc is never parsed under `skipLibCheck`.

Not measured here: the LS path, which now pays a whole-file JSDoc parse into the side Store on the first JSDoc read of a file (the same work `WarmJSDoc` used to do at bind time, without the writer lease).
