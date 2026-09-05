# T10 NodeSeq allocation-free integration report

Date: 2026-09-05  
Status: **ISSUES** (integration compiles; benches keep 32 B/op / 2 allocs/op; VS Code profile missing)

## Artifact metadata

| Field | Value |
|---|---|
| `repo_root` | `/workspace` (github.com/no-yan/TypeScript) |
| `tsgolint_git_rev` | N/A |
| `typescript_go_git_rev` | `d616e5b4692681a13e0594ce23a354197f6e0c30` (compile/test tip on `cursor/nodeseq-integration-t10-3e72`; audit files may sit on a later docs commit) |
| `t0_git_rev` | `468c528422795cf5987b0ce854f7e0c20354080f` (`cursor/nodeseq-api-t0-0d32`) |
| Spec file `tsc/.audit/allocation-free-node-sequences-tasks.md` | **missing** on T0 and this checkout |
| This note | current |
| `ast-list-iteration-after.txt` | current |
| `ast-list-iteration-before-t0.txt` | current (recreated from T0; `/tmp/ast-list-iteration-before.txt` was absent) |
| `ast-list-iteration-benchstat.txt` | current |
| VS Code `src/tsconfig.json` diagnostics / CPU / mem profiles | **missing/unsupported** — no VS Code tree in this environment |

## Merges

All Wave 2 branches merged onto T0 in the suggested order. **No content conflicts.** Fast-forward T8, then merge commits for T9 and T1–T7.

Post-merge signature alignment (not conflicts):

- `GetAllAccessorDeclarations` / `ForDeclaration` take `NodeSeq` (T9). T5 still passed `Members()`; T2/T7 passed `DeclarationNodes(...).Slice()`. Fixed to `MembersSeq` / `DeclarationNodes` without materializing.

T2 `RelocateList`: T0 already has `Factory.RelocateList`. No `ListSeq` helper exists. Relocate still copies via `ListSlice(list).Slice()` because it builds a new packed list (allocation boundary).

`store_polymorphic_generated.go` is unchanged vs T0 (no generator drift).

## Compile / tests

- `go test ./internal/... -run '^$'` **PASS** after post-merge fixes.
- Extra Go 1.26 / Handle compile fixes (latent until printer compiled): `reflect.Value.Kind()`, `FileHandle.Kind()`, `jsontext.Token.Kind()`, `metrics.Value.Kind()`, `format/rulecontext.go` `isForContext` restored from `Kind(comments)` corruption, `CommonJSModuleIndicator.IsNil()`.
- Targeted tests PASS: ast, parser, binder, checker, compiler, printer, scanner, astnav, format, ls, execute/tsc.
- Residual: `TestImportElision` `ExportAssignment#2` and `ExportDeclaration#8` fail with empty emit vs `export default x;` / `export { x };`. **Same failure on `perf/node-header-24b` @ `b73b626011`** (documented in `attempts/018-node-header-24b.md`). Not introduced by NodeSeq integration.

## Microbench (count=10)

Before = T0 NodeSeq API (same benches). Pre-NodeSeq `/tmp/ast-list-iteration-before.txt` was not present; `perf/node-header-24b` does not contain these bench names.

benchstat T0 → T10: both benches **~** (no significant change).

| bench | after | allocs |
|---|---|---|
| ListSlice | 826.8n ± 0%, **32 B/op**, **2 allocs/op** | not 0 |
| DeclarationNodes | 4.154µ ± 0%, **32 B/op**, **2 allocs/op** | not 0 |

Honest: range-over-func / NodeSeq closure still allocates 32 B / 2 allocs. **256-Handle materialization is gone.** Side-by-side `ListSlice().Slice()`: **9368 B/op, 11 allocs/op** vs seq **32 B/op, 2 allocs/op**.

## pprof

`go test ./internal/ast -bench '^Benchmark(ListSlice|DeclarationNodes)$' -memprofile`: alloc_space is attributed to the `for range s.ListSlice(list)` / `DeclarationNodes` loop in the bench, **not** `NodeSeq.Slice`. No `Slice` routine in the profile.

## VS Code check profile

**missing/unsupported.** This VM has no microsoft/vscode checkout (`src/tsconfig.json`). Did not invent diagnostics or profile numbers. `built/local/tsc` **does** build (`control-tsc launch` LAUNCH_OK, Version from `core.Version()`).

## Remaining `.Slice()` hot-path concerns (one-liners)

- printer `emitListItems` still slices packed lists (`printer.go` list emit).
- checker `DeclarationNodes(symbol).Slice()` in a few declaration aggregation paths (`checker.go`).
- checker flow still materializes switch clauses (`flow.go`).
- transformers append/concat/VisitSlice still call `.Slice()` where they need `[]Handle` ownership.
- `Factory.RelocateList` copies via `ListSlice().Slice()`.
- ls definition/callhierarchy still pass `DeclarationNodes(...).Slice()` into APIs that take slices.

## Gaps vs acceptance

1. Benches are not `0 B/op` / `0 allocs/op` (32 B / 2 allocs from range-func).
2. VS Code full-check profiles not collected (tree absent).
3. Original task spec markdown missing from the repo.
4. `TestImportElision` 2 cases remain red on the 24b ancestor as well.
