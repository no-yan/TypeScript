# Parser Store build: NodeRef writes, Handle later

## Problem

The Store parser does the same token-to-tree work as microsoft/typescript `tsc/internal/parser/parser.go`, but each node costs more instructions. Factory constructors return `Handle`, then `SetChild` / `Flags` / `Finish` / `SetLoc` call `mustLive` and `mustMutate` before every column write. Those method calls keep `s.nodes` from staying in registers, so bound-check elimination on `append` and `s.nodes[id]` does not happen. Identifiers also interned twice (`SetStringValue` then `SetIdent(Intern(...))`).

Freeze, one Store per file, `BindSourceFile` as the only public bind entry, and exact `Compact` of parse columns are fixed for this landing.

## Compact (remeasured)

`Store.Compact` clones used prefixes into exact-size arrays and returns high-water scratch to the pooled parser. It exists so the long-lived parse Store is not 1.7–2.8× over-reserved, and so the next file on that parser can `append` without `make`.

kpc on `checker.ts`, `GOGC=off`, 30×3 (Apple M1), after the build-write change:

| | inst/op | cycles/op | IPC |
| --- | ---: | ---: | ---: |
| Compact on | 303.0M | 159.3M | 1.90 |
| Compact off (scratch dropped; Store keeps oversize) | 301.1M | 148.5M | 2.02 |

Instruction counts match. Compact costs about 7% cycles on this single-file mutator path (the memcpy). Earlier `--extendedDiagnostics` on vscode/monaco showed live heap −7% to −19% and better wall time when GC is on (`docs/store-scratch-compact-lockprofile-bench.md`). Keep Compact. Do not judge it from one-file `GOGC=off` inst/op.

## Usage (caller's view)

Unchanged:

```go
file := parser.ParseSourceFile(opts, sourceText, scriptKind)
```

List loops already push `NodeRef` into `Parser.listScratch`. Node constructors still return `Handle` so checker, emit, and JSDoc reparse keep compiling. The write path no longer goes through Handle mutators.

## Shape (this landing)

Build-phase Store helpers, no `mustMutate`:

- `appendSlots` / `appendList` / `intern`
- `linkChild` / `linkList` (same-store parent write; foreign children still `Handle.SetChild` for emit)
- `FinishParse` (OR context flags + loc on one header)

`Factory.createSlots` and generated `New*` use those helpers. Public `AllocSlots` / `AllocList` / `Intern` still check freeze for accidental writes after `Freeze`.

`finishHandleWithEnd` calls `FinishParse` instead of `Flags` / `SetFlags` / `Factory.Finish` / `SetLoc`.

Primary text (identifiers, literals) is `setIdent(intern(text))` once. Secondary strings (template raw text) stay on `SetStringValue`.

## kpc vs microsoft/typescript (checker.ts parse)

| | inst/op | cycles/op | IPC |
| --- | ---: | ---: | ---: |
| Original pointer AST | 263.2M | 122.9M | 2.14 |
| Store before this landing | 415.2M | 184.5M | 2.25 |
| Store this landing (Compact on) | 303.0M | 159.3M | 1.90 |

Both inst and cycles dropped versus the previous Store parser (−27% inst, −14% cycles). Original is still ahead (+15% inst, +29% cycles). IPC fell 2.25 → 1.90; do not regress cycles to chase IPC.

## Next implementation step

Parser currency should be `NodeRef`, not `Handle`. `New*` can keep returning `Handle` for emit/LS, or grow a parallel `New*Ref` used only by `parser`. Locals in `parse*` and JSDoc should be `NodeRef` until a file boundary (`SetParseStore`). Do not add a bind-session equivalent for parse. Do not put `mustMutate` back on the append/intern/link path.

Remaining Handle cost in parse: constructing `Handle{s,id,Kind}` in `createSlots` for the return value, and any `node.Foo()` getters the parser still uses while walking what it just built.

## Tradeoffs accepted

- Emit `New*` still takes `Handle` children. Same-store is the fast `linkChild`; cross-store falls back to `SetChild`.
- Freeze safety for non-Factory callers remains on `AllocSlots` / `Intern`. Factory parse/emit trusts build phase.
- Compact memcpy stays; parser scratch size stays one high-water set per pooled parser.

## Alternatives considered

- Drop Compact to save parse cycles: rejects the heap win on vscode `--noCheck`.
- Delete `mustMutate` everywhere: checker must not write a frozen parse Store; keep the check on the public mutators.
- Fuse generated `New*` into per-kind parser functions: unreviewable, same problem as fused `bindKind`.
