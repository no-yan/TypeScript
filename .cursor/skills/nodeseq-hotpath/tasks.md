# NodeSeq hot-path task list (Phase 2 only)

Updated: 2026-09-05  
**Do not start until** `tsc/.audit/check-phase-alloc-plan.md` Phase 1 is kept.  
A7 / B4 (Len/First → ListLen) are **cancelled** (struct NodeSeq makes them free).

Base: post–Phase-1 integration tip.  
Evidence profile: re-measure after Phase 1; 51718 is pre–Phase-1 reference only.

## Goal

Cut remaining check-phase NodeSeq tax after the ListSlice / DeclarationNodes
migration. Consumers and maintainers keep allocation-free iteration on hot
paths. Agents inherit exclusive file scopes and a frozen contract from
[SKILL.md](SKILL.md).

## Baseline (51718 alloc_space)

| symbol | flat / cum | note |
|---|---:|---|
| total | 990 MB | was 1220 MB before ContainedBy fix |
| `isDeclarationContainedBy` | gone | fixed |
| `ListSlice` closure | 13 MB | residual iterator tax |
| `NodeSeq.Slice` | 11.5 / 20.5 MB | mostly compat `Foo()` |
| `NodeSeq.Len` | 3.5 MB | leftover `*Seq().Len()` |
| `NodeSeq.First` | 5.5 MB | leftover `.First()` |

Slice parents (51718): `Arguments` 6.5 MB, `Parameters` 3 MB, `Statements` 2 MB,
`TypeParameters` 2 MB, `TypeArguments` 1.5 MB, `Elements` 1 MB, plus grammar /
function-symbol helpers.

## Inventory snapshot (Handle accessors in `tsc/internal/checker`)

Refresh with `scripts/inventory.sh`. Do **not** count `*Type.Types()` or
`AsInterfaceType().TypeParameters()`.

| accessor | remaining `Foo()` sites | `FooSeq()` | notes |
|---|---:|---:|---|
| `Arguments()` | 2 | 23 | both on call-args path |
| `Parameters()` | 3 | 30 | exclude Signature APIs in count |
| `Statements()` | 5 | 8 | includes tests |
| `Elements()` | 6 | 42 | |
| `Members()` | 1 | 25 | |
| `Properties()` | 3 | 18 | |
| `Tags()` | 0 | 1 | done |
| `TypeArguments()` | 10 | 12 | exclude `alias.TypeArguments` |
| `TypeParameters()` | 13 | 4 | exclude InterfaceType |

Also: **77** `*Seq().(Len|First|At|Last|Slice)` / `DeclarationNodes(...).(Len|First|Slice)` hits in checker. **3** explicit `DeclarationNodes(...).Slice()` in `checker.go`.

## Fan-out rules

- **Wave A** tasks that edit `checker.go` share **one exclusive agent** (or run
  serial). Never two writers on `checker.go`.
- **Wave B** supporting checker files may run in parallel with each other.
- **Wave C** is optional (printer / LS). Skip unless check profile still names them.
- Workers start from the operator-named `cloud_base_branch`.
- Do not reset dirty unrelated trees.

## Task graph

### Wave A — `checker.go` monopoly

One agent owns all of A1–A7 unless the operator splits them **serially**.

#### A1 — `getEffectiveCallArguments` / `Arguments()`

- [ ] Edit only `tsc/internal/checker/checker.go` (and helpers already in that file).
- [ ] `node.Arguments()` at ~7146 and ~25696 → `ArgumentList` + `ListLen`/`ListAt`/`ListSlice` range, or keep one materialize only if spread/`[]Handle` ownership requires it.
- [ ] Prefer changing callees that need `[]Handle` to take `NodeSeq` when cheap.
- [ ] Verify: `go test ./internal/checker -run '^$'`
- [ ] Handoff: remaining `.Slice()` + reason.

#### A2 — Handle `TypeArguments()`

- [ ] Sites that pass `node.TypeArguments()` into APIs needing `[]*Type` builders may keep a single local materialize; read-only loops → `TypeArgumentsSeq` / `TypeArgumentList`.
- [ ] Known lines (drift OK; re-rg): ~3797, 3822, 7313, 7614, 8138, 8275, 14586, 16357, 19668, 20150.
- [ ] Skip `alias.TypeArguments()` (checker type alias data).

#### A3 — Handle `TypeParameters()`

- [ ] `checkTypeParameters(node.TypeParameters())` → Seq or ListRef-aware helper.
- [ ] Known lines: ~2435, 3770, 4435, 5958, 16952, 18676, 18701, 20334, 20362.
- [ ] Skip `AsInterfaceType().TypeParameters()` and signature type-parameter slices.

#### A4 — `Elements()` / `Properties()` / `Members()`

- [ ] Elements ~2860, 6929, 10819, 14286, 15043, 15238.
- [ ] Properties ~2950, 10774, 26219 (`Filter` → Seq helper or one Slice with comment).
- [ ] Members ~4530.

#### A5 — `Parameters()` / `Statements()` Handle sites

- [ ] Handle `Parameters()` only (not `Signature.Parameters()`).
- [ ] Statements ~2171, 14057; leave `checker_test.go` to A7 or ListAt.

#### A6 — `DeclarationNodes(...).Slice()` in `checker.go`

- [ ] ~3074, 13593, 13595. Keep Slice only for `Concatenate` / indexed ownership; otherwise Seq helpers.

#### A7 — cancelled

- [x] skip: Phase 1 struct NodeSeq makes `Len`/`First` allocation-free without call-site rewrites.

---

### Wave B — supporting checker packages (parallel OK)

#### B1 — `jsx.go` Slice sites

- [ ] Files: `tsc/internal/checker/jsx.go` only.
- [ ] `ChildrenSeq().Slice()` / `PropertiesSeq().Slice()` feeding `GetSemanticJsxChildren` / `Filter`.
- [ ] If AST helpers require `[]Handle`, add Seq-capable overloads **only if** already patterned nearby; else one Slice at the helper boundary with a one-line reason.

#### B2 — grammar / flow Slice + Len

- [ ] Files: `grammarchecks.go`, `flow.go` (not `checker.go`).
- [ ] `checkGrammarForUseStrictSimpleParameterList` Slice; `getSwitchClauseTypes` if present here; `*Seq().Len` → ListLen.

#### B3 — utilities / emitresolver / nodebuilder leftovers

- [ ] Files: `utilities.go`, `emitresolver.go`, `nodebuilderimpl.go`, `symbolaccessibility.go`, `relater.go` **only for AST Handle Seq.First / ModifierNodesSeq.Slice**.
- [ ] Do not chase `Type.Types()` in relater.

#### B4 — cancelled

- [x] skip: same as A7 after Phase 1.

---

### Wave C — optional (skip unless profile demands)

#### C1 — printer partial-range Slice

- [ ] `printer.go` `ListSlice(...).Slice()[start:end]` algorithm boundary. Only chase if mem profile still attributes check-phase time here (unlikely).

#### C2 — LS definition `DeclarationNodes(...).Slice()`

- [ ] Public `[]Handle` API boundary. Leave unless LS memory profile is the goal.

---

### Wave I — integration

#### I1 — merge + inventory + re-profile

- [ ] Merge Wave A/B branches.
- [ ] Run `scripts/inventory.sh`; paste table into this file.
- [ ] `go test ./internal/... -run '^$'`
- [ ] Rebuild `built/local/tsc`; VS Code `src/tsconfig.json` mem+cpu profiles.
- [ ] Acceptance: Slice parents for Arguments/Parameters/Statements/TypeParameters/Elements absent or documented ownership; total alloc_space not regressed vs 51718 beyond noise.

## Agent brief template

Copy into each worker. Fill `TASK_ID`, files, and bullets from above.

```
You own NodeSeq hot-path task TASK_ID only.
Read .cursor/skills/nodeseq-hotpath/SKILL.md and tasks.md.
Base branch: <cloud_base_branch tip>.
Edit ONLY the files listed for TASK_ID.
Follow the frozen contract. Do not change NodeSeq API.
Verify with go test on your package(s).
Commit and push. Final report: PASS|ISSUES|BLOCKED, branch, SHA,
files, remaining .Slice() with reasons, test evidence.
```

## Out of scope

- Redesigning NodeSeq as a struct to kill ListSlice closures (separate design)
- Transformer mutation `.Slice()` at append/VisitSlice boundaries
- Checker `*Type` union member slices (`t.Types()`)
- Full VS Code profile in leaf agents
