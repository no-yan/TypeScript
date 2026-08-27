# Ship Store AST upstream plan

This program replaces the pointer AST with a Store-backed tree through parser, binder, checker, and printer. It is for the engineer who will open a pull request on microsoft/TypeScript. The rule is that microsoft/main never receives a dual tree, and the upstream PR opens only after a real `cmd/tsc` compile and `npx hereby test` match trunk diagnostics. PR ids in order are PR-1, PR-2, PR-3, PR-4, PR-5, PR-6, PR-7, and PR-8.

## How to read this

One box is one unit of work. Every box names the evidence that checks it. A nested box is a sub-step of the box above it. Check a box only when its evidence exists, a file, a log line, a screenshot, a test run, or a SHA. The body is a how-to. The appendices explain and record.

The program runs `pstack/skills/poteto-mode/playbooks/autopilot-stack.md`. The operator lands PR-1 and PR-2 on the working branch. Owners stop at merge-ready. No owner squash-merges. The operator does not land PR-3 through PR-6 on microsoft/main. She opens PR-8 against microsoft/TypeScript from the PR-7 tip.

Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

## Program checklist

### Arm the program

- [ ] State the protocol and this plan to the operator, then stop. Start execution only on her explicit go.
- [ ] On her go, arm a `/goal` with this exact text. "`tsc/internal/ast/docs/store-upstream-plan.md`. PR-1 then PR-2 then PR-3 then PR-4 then PR-5 then PR-6 then PR-7 then PR-8. Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. The operator lands PR-1 and PR-2. She does not land PR-3 through PR-6 on microsoft/main. Done when PR-8 exists against microsoft/TypeScript and its body cites PR-7 e2e receipts."
- [ ] Read these from trunk at program start. Re-read them at every tick.
  - [ ] `git show origin/main:pstack/skills/poteto-mode/playbooks/autopilot-stack.md`
  - [ ] `git show origin/main:pstack/skills/swarm/SKILL.md`
  - [ ] `git show origin/main:pstack/skills/poteto-mode/playbooks/opening-a-pr.md`
  - [ ] `git show origin/main:pstack/skills/principle-prove-it-works/SKILL.md`
  - [ ] `git show origin/main:pstack/skills/principle-sequence-verifiable-units/SKILL.md`
  - [ ] `git show origin/main:pstack/skills/principle-migrate-callers-then-delete-legacy-apis/SKILL.md`
- [ ] Arm the 30-minute audit tick. In a local session, a real terminal `/loop`. In a cloud root, a cloud-sleeper wake chain. Never leave the cadence to memory.
- [ ] Use this tick prompt, verbatim. "Re-read the execution playbook from trunk and the armed /goal. Audit the operation against both and fix drift in this tick. Probe every active lane and judge progress by side effects only. Stand down a stuck lane and dispatch its replacement now. Then send the operator a status message, whether or not anything changed, with the queue table of PR, owner, state, and head SHA, the verdicts since the last tick, what merged, open operator gates, and blockers."
- [ ] On the operator's hold or stand-down, send every owner a zero-writes order at once.

### Spawn owners

- [ ] Spawn one owner per PR with the full lifecycle the execution playbook names.
- [ ] Follow this dependency graph. Start dependent work only after its parent merges, or base it on the parent branch when the execution playbook stacks.
  - [ ] PR-1 branches from `main`.
  - [ ] PR-2 after PR-1. Stop the program before PR-2 if PR-1's perf rule fails.
  - [ ] PR-3 after PR-2.
  - [ ] PR-4 after PR-3.
  - [ ] PR-5 after PR-4.
  - [ ] PR-6 after PR-5.
  - [ ] PR-7 after PR-6.
  - [ ] PR-8 after PR-7.
- [ ] Hold the file boundaries. PR-1 touches `tsc/internal/ast/**` and `tsc/.audit/**`. PR-2 touches `tsc/internal/ast/store*.go`, `tsc/internal/ast/STORE.md`, and `tsc/.audit/store-open-questions.tsv`. PR-3 touches `tsc/internal/ast/**`, `tsc/internal/parser/**`, and `tools/scripts/tsc/**`. PR-4 touches `tsc/internal/binder/**` and `tsc/internal/ast/**`. PR-5 touches `tsc/internal/checker/**` and `tsc/internal/ast/**`. PR-6 touches `tsc/internal/printer/**`, `tsc/internal/transformers/**`, and `tsc/internal/ast/**`. PR-7 touches test harness files under `tsc/internal/ast/**` and `tsc/internal/execute/**`. PR-8 touches `tsc/internal/ast/STORE.md` only.
- [ ] Hold the review gate. PR-7 and PR-8 change compile output and the public PR. They wait for the operator's review in chat with screenshots and a video before merge.

### PR mechanics, for every PR

- [ ] Open the PR ready, never draft, with `gh pr create` and `draft: false`, or with Graphite `gt` for a stack.
- [ ] Run the repo's lint and typecheck once before the PR-facing push. Push with hooks on.
- [ ] Run `/deslop` before each commit and `/no-comments` before review.
- [ ] Triage every Bugbot and security-reviewer comment per `../references/bugbot-triage.md`.
- [ ] Rebase onto current trunk before babysit and again before the merge-ready report.

### Verdict and merge, for every PR

- [ ] At the merge-ready head SHA, run the swarm per `pstack/skills/swarm/SKILL.md`. One gates lane. The ten live lanes from the PR's **Verify, live** block. The perf lane from its **Verify, perf** block. One audit lane that reads the diff and the receipts and distrusts the PR body.
- [ ] Clean only when every lane is `PASS`. Findings go back to the owner. A new head gets a fresh swarm and a fresh verdict.
- [ ] The root appends the PR to the Graphite stack. Compare `git patch-id` at the verdict SHA against the new head per `playbooks/shipping.md`. The operator lands PR-1 and PR-2. She does not land PR-3 through PR-6 on microsoft/main. PR-8 is the microsoft/TypeScript pull request.

### Boot recipe, for every live lane

Each live lane runs on its own cloud VM at the PR head. Drive through the shell that runs `npx hereby build`, `./built/local/tsc`, and `go test`. This repo has no `control-cli` on trunk. `hereby build` uses `-tags=noembed`. lib files must sit next to `built/local/tsc`. `go run ./cmd/tsc` from `tsc/` is a fallback with embedded libs. Prefer `./built/local/tsc`.

- [ ] `git fetch origin <head-branch> && git checkout <head SHA>`.
- [ ] From the repo root, run `npx hereby build`. Wait until `built/local/tsc` exists.
- [ ] Deliver input only through `./built/local/tsc`, `go test`, and `npx hereby test`. Read-only diagnostics are `-v` logs and `t.Logf` lines.
- [ ] Save every screenshot to `/tmp/swarm-<pr-id>/worker-<n>/<slug>.png` and return the paths with the report.

## Measure real tsgo GC (PR-1)

**Depends on.** None.

**Files.**

- [ ] Create `tsc/internal/ast/store_tsgo_gogc_test.go`.
- [ ] Edit `tsc/internal/ast/STORE.md`.
- [ ] Edit `tsc/.audit/store-open-questions.tsv`.

**Build.**

- [ ] Add `TestTsgoGOGCBaseline` that shells `npx hereby build` once, then `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts` under default `GOGC`, `GOGC=off`, `GOGC=200`, and `GOMEMLIMIT=8GiB`. Record wall time. Do not flatten through `FlattenNode`. This is the CI binary (`Herebyfile.mjs` uses `-tags=noembed`).
- [ ] Write the four medians into `STORE.md`.

**You see.**

- [ ] `go test ./internal/ast -run TestTsgoGOGCBaseline -v` prints four `median=` lines.
- [ ] `STORE.md` contains those four numbers.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `store_tsgo_gogc_test.go` `TestTsgoGOGCBaseline`. Run `go -C ./tsc test ./internal/ast -count=1 -run TestTsgoGOGCBaseline -v`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Run `TestTsgoGOGCBaseline` with default env. Save `pr1-lane1-default.png`. Pass when the log contains `median=`.
- [ ] Lane 2. Run with `GOGC=off`. Save `pr1-lane2-gogc-off.png`. Pass when the log contains `GOGC=off`.
- [ ] Lane 3. Run with `GOGC=200`. Save `pr1-lane3-gogc-200.png`. Pass when the log contains `GOGC=200`.
- [ ] Lane 4. Run with `GOMEMLIMIT=8GiB`. Save `pr1-lane4-memlimit.png`. Pass when the log contains `GOMEMLIMIT`.
- [ ] Lane 5. From the repo root, run `./built/local/tsc --help`. Save `pr1-lane5-help.png`. Pass when the process prints usage or exits 0.
- [ ] Lane 6. From the repo root, run `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts`. Save `pr1-lane6-check.png`. Pass when the process exits 0.
- [ ] Lane 7. Run `go -C ./tsc test ./internal/ast -count=1 -run 'TestStore|TestFactory|TestGlobal'`. Save `pr1-lane7-unit.png`. Pass when the last line contains `ok`.
- [ ] Lane 8. Run `go -C ./tsc test ./internal/compiler -count=1 -run TestProgram`. Save `pr1-lane8-program.png`. Pass when the last line contains `ok` or `PASS`.
- [ ] Lane 9. `git grep -n 'NewFactory' tsc/internal -- ':!tsc/internal/ast/*'`. Save `pr1-lane9-no-prod.png`. Pass when packages outside `ast` still do not call `ast.NewFactory`.
- [ ] Lane 10. Confirm `STORE.md` lists four `median=` numbers. Save `pr1-lane10-doc.png`. Pass when four numbers are present.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Wall time of `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts`.
- [ ] Probe. `TestTsgoGOGCBaseline` once per env, interleaved with a trunk checkout of the same command.
- [ ] Baseline. Record the trunk default-GOGC median first.
- [ ] Rule. Fail if default-GOGC median divided by `GOGC=off` median is under 1.10, or if `GOMEMLIMIT` median divided by `GOGC=off` median is over 0.95. Stop the program on fail. Tunables already ate the GC gap.

**Review gate.** None. PR-1 is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends PR-1 to the Graphite stack. The operator lands it.

## Finish Store side tables (PR-2)

**Depends on.** PR-1.

**Files.**

- [ ] Edit `tsc/internal/ast/store.go`.
- [ ] Edit `tsc/internal/ast/store_factory.go`.
- [ ] Edit `tsc/internal/ast/store_identity.go`.
- [ ] Edit `tsc/internal/ast/store_factory_test.go`.
- [ ] Edit `tsc/internal/ast/store_identity_test.go`.
- [ ] Edit `tsc/internal/ast/STORE.md`.
- [ ] Edit `tsc/.audit/store-open-questions.tsv`.

**Build.**

- [ ] Add `Locals` and `NextContainer` side maps on `Store`, with `Handle` getters and setters, matching `LocalsContainerBase` in `ast_generated.go`.
- [ ] Add `NewFactoryOn(*Store, FactoryHooks)` so checker synthetics and emit updates append into the parse Store.
- [ ] Add `StoreSet.SetFile` and `StoreSet.File` so file metadata resolves by `StoreID`.
- [ ] Keep `SetChild` panicking across stores.

**You see.**

- [ ] `TestFactoryOnExistingStore` allocates a second `Factory` on one `Store`.
- [ ] `TestStoreSetFile` returns the same `*SourceFile` that `SetFile` stored.
- [ ] `TestStoreLocalsAndNextContainer` round-trips a `SymbolTable` and a `NodeRef`.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Named cases in `store_factory_test.go` and `store_identity_test.go`. Run `go -C ./tsc test ./internal/ast -count=1 -run 'TestFactory|TestStore|TestGlobal'`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Run `TestFactoryOnExistingStore`. Save `pr2-lane1-factory-on.png`. Pass when the log contains `PASS`.
- [ ] Lane 2. Run `TestStoreSetFile`. Save `pr2-lane2-setfile.png`. Pass when the log contains `PASS`.
- [ ] Lane 3. Run `TestStoreLocalsAndNextContainer`. Save `pr2-lane3-locals.png`. Pass when the log contains `PASS`.
- [ ] Lane 4. Run `TestStoreCrossStoreChildPanics`. Save `pr2-lane4-cross-store.png`. Pass when the log contains `PASS`.
- [ ] Lane 5. Run `TestFactoryCopySubtreeRemapsList`. Save `pr2-lane5-copy-list.png`. Pass when the log contains `PASS`.
- [ ] Lane 6. Run `TestFactoryParamTypeWrittenAfterCreate`. Save `pr2-lane6-param.png`. Pass when the log contains `PASS`.
- [ ] Lane 7. Run `TestFactoryReplaceListAfterCreate`. Save `pr2-lane7-setlist.png`. Pass when the log contains `PASS`.
- [ ] Lane 8. Run `go -C ./tsc test ./internal/ast -count=1`. Save `pr2-lane8-package.png`. Pass when the last line contains `ok`.
- [ ] Lane 9. From the repo root, run `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts`. Save `pr2-lane9-tsc.png`. Pass when the process exits 0.
- [ ] Lane 10. `git grep -n 'NewFactoryOn' tsc/internal/parser tsc/internal/binder tsc/internal/checker`. Save `pr2-lane10-no-callers.png`. Pass when those packages have zero hits.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. `BenchmarkWalkStore` ns/op.
- [ ] Probe. `go -C ./tsc test ./internal/ast -run '^$' -bench BenchmarkWalkStore -count 3` at the parent branch, then at HEAD, interleaved.
- [ ] Baseline. Record the parent-branch median ns/op first.
- [ ] Rule. Fail if HEAD median is more than 1.20 times the parent median.

**Review gate.** None. PR-2 is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends PR-2 to the Graphite stack. The operator lands it.

## Parse into Store with expand (PR-3)

**Depends on.** PR-2.

**Files.**

- [ ] Edit `tsc/internal/parser/parser.go`.
- [ ] Create `tsc/internal/ast/store_expand.go`.
- [ ] Edit `tsc/internal/ast/store_schema.go`.
- [ ] Edit `tsc/internal/parser/parser_test.go`.
- [ ] Edit `tools/scripts/tsc/generate-go-ast.ts`.
- [ ] Edit `tools/scripts/tsc/ast.json`.

**Build.**

- [ ] Extend `generate-go-ast.ts` so `npx hereby generate` emits Store slot layouts from `ast.json`. Do not hand-write 193 `New*` kinds. Parser currently has about 273 `p.factory.New*` calls across `parser.go`, `jsdoc.go`, and `reparser.go`.
- [ ] Make `ParseSourceFile` allocate into a Store through `NewFactory`.
- [ ] Add `ExpandStore` that copies the Store tree back to `*Node` at the parse boundary so binder, checker, and printer stay on `*Node` for this PR only. STORE.md forbids this as a landed production bridge. It exists only on the unlanded stack.
- [ ] Delete `ExpandStore` in PR-6. Do not land this PR on microsoft/main.

**You see.**

- [ ] `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts` still exits 0.
- [ ] A parser test asserts the Store `Len()` is nonzero before expand.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Parser test that Store is nonempty before expand. Run `go -C ./tsc test ./internal/parser -count=1 -run TestParse`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Run parser tests. Save `pr3-lane1-parser.png`. Pass when the last line contains `ok`.
- [ ] Lane 2. From the repo root, run `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts`. Save `pr3-lane2-tsc.png`. Pass when the process exits 0.
- [ ] Lane 3. Run `go -C ./tsc test ./internal/testrunner -count=1 -run 'TestLocal/alias'`. Save `pr3-lane3-local.png`. Pass when the last line contains `ok` or `PASS`.
- [ ] Lane 4. Confirm `ExpandStore` exists. Save `pr3-lane4-expand.png`. Pass when `git grep ExpandStore tsc/internal/ast` hits.
- [ ] Lane 5. Confirm parser calls `NewFactory`. Save `pr3-lane5-factory.png`. Pass when `git grep NewFactory tsc/internal/parser` hits.
- [ ] Lane 6. Run `go -C ./tsc test ./internal/binder -count=1`. Save `pr3-lane6-binder.png`. Pass when the last line contains `ok`.
- [ ] Lane 7. Run `go -C ./tsc test ./internal/ast -count=1 -run 'TestStore|TestFactory'`. Save `pr3-lane7-ast.png`. Pass when the last line contains `ok`.
- [ ] Lane 8. Run `gofmt -l tsc/internal/parser tsc/internal/ast/store_expand.go`. Save `pr3-lane8-fmt.png`. Pass when the output is empty.
- [ ] Lane 9. `git grep -n 'FlattenNode' tsc/internal/parser`. Save `pr3-lane9-no-flatten.png`. Pass when parser has zero hits.
- [ ] Lane 10. From the repo root, run `./built/local/tsc --help`. Save `pr3-lane10-help.png`. Pass when the process prints usage or exits 0.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Wall time of `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts`.
- [ ] Probe. Three runs at PR-2 HEAD, then three at PR-3 HEAD, interleaved.
- [ ] Baseline. Record the PR-2 median first.
- [ ] Rule. Fail if PR-3 median is more than 1.50 times PR-2. Expand is temporary and may cost, but more than 1.50 times means the parse path is wrong.

**Review gate.** None. PR-3 is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends PR-3 to the Graphite stack. The operator does not land it on microsoft/main.

## Bind on Store (PR-4)

**Depends on.** PR-3.

**Files.**

- [ ] Edit `tsc/internal/binder/binder.go`.
- [ ] Edit `tsc/internal/ast/store.go`.
- [ ] Edit `tsc/internal/binder/binder_test.go`.

**Build.**

- [ ] Change `BindSourceFile` to walk `Handle` values. Write Flags, Symbol, Locals, NextContainer, and FlowNode on the Store.
- [ ] Move `ExpandStore` to after bind so checker still receives `*Node` in this PR.
- [ ] Keep one Store per file. Do not share writers across the `BindSourceFiles` work group.

**You see.**

- [ ] Binder tests pass.
- [ ] `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts` still exits 0.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Binder package tests. Run `go -C ./tsc test ./internal/binder -count=1`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Run binder tests. Save `pr4-lane1-binder.png`. Pass when the last line contains `ok`.
- [ ] Lane 2. From the repo root, run `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts`. Save `pr4-lane2-tsc.png`. Pass when the process exits 0.
- [ ] Lane 3. Run a testrunner local case. Save `pr4-lane3-local.png`. Pass when the last line contains `ok` or `PASS`.
- [ ] Lane 4. Confirm binder writes `SetFlowNode` or `SetSymbol` on Handle. Save `pr4-lane4-side.png`. Pass when `git grep SetFlowNode tsc/internal/binder` or `SetSymbol` hits.
- [ ] Lane 5. Confirm `BindSourceFiles` still queues per file. Save `pr4-lane5-wg.png`. Pass when `compiler/program.go` still calls `BindSourceFile` per file.
- [ ] Lane 6. Run `go -C ./tsc test ./internal/parser -count=1`. Save `pr4-lane6-parser.png`. Pass when the last line contains `ok`.
- [ ] Lane 7. Run `go -C ./tsc test ./internal/ast -count=1 -run 'TestStore|TestFactory'`. Save `pr4-lane7-ast.png`. Pass when the last line contains `ok`.
- [ ] Lane 8. Confirm Parent and child lists are not rewritten in binder. Save `pr4-lane8-shape.png`. Pass when `git grep 'SetChild' tsc/internal/binder` is empty.
- [ ] Lane 9. From the repo root, run `./built/local/tsc --help`. Save `pr4-lane9-help.png`. Pass when the process prints usage or exits 0.
- [ ] Lane 10. Run `gofmt -l tsc/internal/binder`. Save `pr4-lane10-fmt.png`. Pass when the output is empty.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Wall time of `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts`.
- [ ] Probe. Three runs at PR-3 HEAD, then three at PR-4 HEAD, interleaved.
- [ ] Baseline. Record the PR-3 median first.
- [ ] Rule. Fail if PR-4 median is more than 1.20 times PR-3.

**Review gate.** None. PR-4 is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends PR-4 to the Graphite stack. The operator does not land it on microsoft/main.

## Check on Store (PR-5)

**Depends on.** PR-4.

**Files.**

- [ ] Edit `tsc/internal/checker/links.go`.
- [ ] Edit `tsc/internal/checker/checker.go`.
- [ ] Edit `tsc/internal/checker/types.go`.
- [ ] Edit `tsc/internal/ast/symbol.go`.

**Build.**

- [ ] Key checker link stores on `GlobalRef` or per-Store `NodeRef`, not `GetNodeId`.
- [ ] Change `Symbol.Declarations` and `ValueDeclaration` to `[]GlobalRef` / `GlobalRef`.
- [ ] Allocate checker synthetics with `NewFactoryOn` on the parse Store so shared children stay in one Store.
- [ ] Move `ExpandStore` to after check so printer still receives `*Node` in this PR.

**You see.**

- [ ] Checker tests pass.
- [ ] `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts` still exits 0.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Checker package tests. Run `go -C ./tsc test ./internal/checker -count=1`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Run checker tests. Save `pr5-lane1-checker.png`. Pass when the last line contains `ok`.
- [ ] Lane 2. From the repo root, run `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts`. Save `pr5-lane2-tsc.png`. Pass when the process exits 0.
- [ ] Lane 3. Run a testrunner local case. Save `pr5-lane3-local.png`. Pass when the last line contains `ok` or `PASS`.
- [ ] Lane 4. Confirm `GetNodeId` is gone from `checker/links.go`. Save `pr5-lane4-links.png`. Pass when `git grep GetNodeId tsc/internal/checker/links.go` is empty.
- [ ] Lane 5. Confirm `NewFactoryOn` is used in checker. Save `pr5-lane5-synth.png`. Pass when `git grep NewFactoryOn tsc/internal/checker` hits.
- [ ] Lane 6. Confirm `Declarations` is `[]GlobalRef`. Save `pr5-lane6-decls.png`. Pass when `symbol.go` contains `[]GlobalRef`.
- [ ] Lane 7. Run binder tests. Save `pr5-lane7-binder.png`. Pass when the last line contains `ok`.
- [ ] Lane 8. Run `go -C ./tsc test ./internal/ast -count=1 -run TestGlobal`. Save `pr5-lane8-ident.png`. Pass when the last line contains `ok`.
- [ ] Lane 9. From the repo root, run `./built/local/tsc --help`. Save `pr5-lane9-help.png`. Pass when the process prints usage or exits 0.
- [ ] Lane 10. Run `gofmt -l tsc/internal/checker tsc/internal/ast/symbol.go`. Save `pr5-lane10-fmt.png`. Pass when the output is empty.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Wall time of `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts`.
- [ ] Probe. Three runs at PR-4 HEAD, then three at PR-5 HEAD, interleaved.
- [ ] Baseline. Record the PR-4 median first.
- [ ] Rule. Fail if PR-5 median is more than 1.20 times PR-4.

**Review gate.** None. PR-5 is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends PR-5 to the Graphite stack. The operator does not land it on microsoft/main.

## Print on Store and delete expand (PR-6)

**Depends on.** PR-5.

**Files.**

- [ ] Edit `tsc/internal/printer/emitcontext.go`.
- [ ] Edit `tsc/internal/printer/printer.go`.
- [ ] Edit `tsc/internal/transformers/**/*.go`.
- [ ] Delete `tsc/internal/ast/store_expand.go`.
- [ ] Delete unused `*Node` factory paths in `tsc/internal/ast` that printer no longer calls.

**Build.**

- [ ] Change `EmitContext` maps to `GlobalRef` keys. `Update*` returns the same `NodeRef` when children are unchanged.
- [ ] Append emit nodes with `NewFactoryOn` on the parse Store.
- [ ] Delete `ExpandStore` and every remaining production `*Node` allocation on the compile path.
- [ ] Keep `FlattenNode` only under `store_flatten.go` for benches.

**You see.**

- [ ] `git grep ExpandStore tsc` is empty.
- [ ] `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts` still exits 0.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Printer and transformer tests. Run `go -C ./tsc test ./internal/printer ./internal/transformers/... -count=1`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Run printer tests. Save `pr6-lane1-printer.png`. Pass when the last line contains `ok`.
- [ ] Lane 2. From the repo root, run `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts`. Save `pr6-lane2-tsc.png`. Pass when the process exits 0.
- [ ] Lane 3. From the repo root, run `./built/local/tsc tsc/testdata/fixtures/compiler/checker.ts --outFile /tmp/store-emit.js`. Save `pr6-lane3-emit.png`. Pass when `/tmp/store-emit.js` exists and is nonempty.
- [ ] Lane 4. Confirm `ExpandStore` is gone. Save `pr6-lane4-no-expand.png`. Pass when `git grep ExpandStore tsc` is empty.
- [ ] Lane 5. Confirm `FlattenNode` lives only in `store_flatten.go`. Save `pr6-lane5-flatten.png`. Pass when `git grep FlattenNode tsc/internal -- ':!tsc/internal/ast/store_flatten.go' ':!tsc/internal/ast/store_*bench*' ':!tsc/internal/ast/store_e2e*'` is empty.
- [ ] Lane 6. Run a testrunner local case. Save `pr6-lane6-local.png`. Pass when the last line contains `ok` or `PASS`.
- [ ] Lane 7. Run checker tests. Save `pr6-lane7-checker.png`. Pass when the last line contains `ok`.
- [ ] Lane 8. Run binder tests. Save `pr6-lane8-binder.png`. Pass when the last line contains `ok`.
- [ ] Lane 9. From the repo root, run `./built/local/tsc --help`. Save `pr6-lane9-help.png`. Pass when the process prints usage or exits 0.
- [ ] Lane 10. Run `gofmt -l tsc/internal/printer tsc/internal/ast`. Save `pr6-lane10-fmt.png`. Pass when the output is empty.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Wall time of `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts`.
- [ ] Probe. Three runs at PR-5 HEAD, then three at PR-6 HEAD, interleaved.
- [ ] Baseline. Record the PR-5 median first.
- [ ] Rule. Fail if PR-6 median is more than 1.10 times PR-5. Deleting expand should not slow the check.

**Review gate.** None. PR-6 is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends PR-6 to the Graphite stack. The operator does not land it on microsoft/main.

## Run e2e compile and tests (PR-7)

**Depends on.** PR-6.

**Files.**

- [ ] Create `tsc/internal/ast/store_e2e_tsgo_test.go`.
- [ ] Edit `tsc/internal/ast/STORE.md`.
- [ ] Edit `tsc/.audit/store-open-questions.tsv`.

**Build.**

- [ ] Add `TestTsgoStoreE2E` that runs `npx hereby build`, then `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts`, then `go -C ./tsc test ./internal/testrunner -count=1 -run TestLocal`.
- [ ] Record wall time and `GOGC=off` wall time next to the PR-1 numbers in `STORE.md`.
- [ ] Run `npx hereby test` once on the owner machine and attach the log path in the PR body.

**You see.**

- [ ] `TestTsgoStoreE2E` passes.
- [ ] `STORE.md` has a Store versus trunk table for `cmd/tsc` wall time.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `store_e2e_tsgo_test.go` `TestTsgoStoreE2E`. Run `go -C ./tsc test ./internal/ast -count=1 -run TestTsgoStoreE2E -v`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Run `TestTsgoStoreE2E`. Save `pr7-lane1-e2e.png`. Pass when the log contains `PASS`.
- [ ] Lane 2. From the repo root, run `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts`. Save `pr7-lane2-check.png`. Pass when the process exits 0.
- [ ] Lane 3. From the repo root, run `./built/local/tsc tsc/testdata/fixtures/compiler/checker.ts --outFile /tmp/store-e2e.js`. Save `pr7-lane3-emit.png`. Pass when the outfile is nonempty.
- [ ] Lane 4. Run `go -C ./tsc test ./internal/testrunner -count=1 -run TestLocal`. Save `pr7-lane4-local.png`. Pass when the last line contains `ok` and no parent-pointer failures from `verifyParentPointers` in `compiler_runner.go`.
- [ ] Lane 5. Run `npx hereby test:tsc` from repo root. Save `pr7-lane5-hereby-tsc.png`. Pass when hereby reports success.
- [ ] Lane 6. Diff diagnostics of lane 2 against trunk on the same file. Save `pr7-lane6-diag.png`. Pass when diagnostic text matches trunk.
- [ ] Lane 7. Confirm `ExpandStore` is still gone. Save `pr7-lane7-no-expand.png`. Pass when `git grep ExpandStore tsc` is empty.
- [ ] Lane 8. Confirm production parse does not call `FlattenNode`. Save `pr7-lane8-no-flatten.png`. Pass when `git grep FlattenNode tsc/internal/parser tsc/internal/compiler` is empty.
- [ ] Lane 9. From the repo root, run `./built/local/tsc -p ./smoke/typescript-6.0/src/compiler --noEmit`. Save `pr7-lane9-smoke.png`. Pass when the process exits 0. This is the CI smoke project from `.github/workflows/ci.yml`.
- [ ] Lane 10. Open `STORE.md` e2e table. Save `pr7-lane10-table.png`. Pass when Store and trunk wall times are both present.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Wall time of `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts` under default GOGC.
- [ ] Probe. Five runs at origin/main, then five at PR-7 HEAD, interleaved.
- [ ] Baseline. Record the origin/main median from the same machine as PR-1.
- [ ] Rule. Fail if HEAD median is more than 1.05 times trunk, or if HEAD default-GOGC divided by HEAD `GOGC=off` is not at least 0.05 lower than trunk's same ratio. Store must not lose wall time. It must shrink the GC gap.

**Review gate.** The operator reviews before merge.

- [ ] Copy lane 2 and lane 6 screenshots into `tsc/internal/ast/docs/media/pr-7-review-check.png` and `tsc/internal/ast/docs/media/pr-7-review-diag.png`.
- [ ] Record a 30 to 60 second video of `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts` on a lane VM. Save it as `tsc/internal/ast/docs/media/pr-7-review.mp4`.
- [ ] Post the screenshots and the video in chat. Stop at merge-ready. Wait for the operator's click.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends PR-7 to the Graphite stack. The operator does not land it on microsoft/main.

## Open the upstream pull request (PR-8)

**Depends on.** PR-7.

**Files.**

- [ ] Edit `tsc/internal/ast/STORE.md`.
- [ ] Edit `tsc/.audit/store-open-questions.tsv`.

**Build.**

- [ ] Mark STORE.md as the experiment that landed in the compile path. Cite PR-7 receipts.
- [ ] Open one pull request against `microsoft/TypeScript` from the PR-7 tip with `gh pr create`. Title and body follow CONTRIBUTING.md. Include the problem, the Store layout, the tests, and the e2e numbers.

**You see.**

- [ ] `gh pr view` prints a URL under `https://github.com/microsoft/TypeScript/pull/`.
- [ ] The PR body cites `TestTsgoStoreE2E` and the wall-time table.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `npx hereby lint` and `npx hereby check:format` at the PR-7 tip. Run `npx hereby lint && npx hereby check:format`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. `gh pr view --json url,title,baseRepository`. Save `pr8-lane1-pr.png`. Pass when the URL contains `microsoft/TypeScript`.
- [ ] Lane 2. Confirm the PR is not draft. Save `pr8-lane2-ready.png`. Pass when `isDraft` is false.
- [ ] Lane 3. Confirm the base branch is `main`. Save `pr8-lane3-base.png`. Pass when base is `main`.
- [ ] Lane 4. Re-run `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts` from the repo root. Save `pr8-lane4-tsc.png`. Pass when the process exits 0.
- [ ] Lane 5. Re-run `TestTsgoStoreE2E`. Save `pr8-lane5-e2e.png`. Pass when the log contains `PASS`.
- [ ] Lane 6. Confirm CLA bot or CONTRIBUTING checklist is mentioned in the body. Save `pr8-lane6-cla.png`. Pass when the body mentions tests and the problem.
- [ ] Lane 7. Confirm `ExpandStore` is absent at the tip. Save `pr8-lane7-no-expand.png`. Pass when `git grep ExpandStore tsc` is empty.
- [ ] Lane 8. Confirm production still does not call `FlattenNode`. Save `pr8-lane8-no-flatten.png`. Pass when parser and compiler greps are empty.
- [ ] Lane 9. Run `npx hereby check:format`. Save `pr8-lane9-format.png`. Pass when the command exits 0.
- [ ] Lane 10. Run `git diff --exit-code` after format. Save `pr8-lane10-clean.png`. Pass when the worktree is clean.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Wall time of `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts`.
- [ ] Probe. Repeat the PR-7 probe at the exact SHA used for `gh pr create`.
- [ ] Baseline. Use the PR-7 recorded trunk median.
- [ ] Rule. Fail if this SHA's median differs from the PR-7 HEAD median by more than 1.02 times. The upstream tip must be the verified tip.

**Review gate.** The operator reviews before merge.

- [ ] Copy lane 1 and lane 4 screenshots into `tsc/internal/ast/docs/media/pr-8-review-url.png` and `tsc/internal/ast/docs/media/pr-8-review-tsc.png`.
- [ ] Record a 30 to 60 second video of opening the GitHub PR page and the `cmd/tsc` run. Save it as `tsc/internal/ast/docs/media/pr-8-review.mp4`.
- [ ] Post the screenshots and the video in chat. Stop at merge-ready. Wait for the operator's click.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root does not squash-merge. The operator owns the microsoft/TypeScript merge click.

## Close the program

- [ ] Every box above is checked with its evidence.
- [ ] Reply to the operator with the report the execution playbook names.

## Appendix A. Prototype evidence

Packed-header Store beat column-per-field SoA and shrank flattened `checker.ts` heap in `store_e2e_report_test.go`. That is parse plus flatten, not a full `./built/local/tsc` check.

`GlobalRef` pack and `StoreSet` resolve are proven in `store_identity_test.go`.

`SetList`, late `SetChild` on Parameter, Intern after Seal, and `copyList` are proven in `store_factory_test.go` and `store_copy_test.go`.

JSDoc reparser mutates created hosts. Evidence in `parser/reparser.go` at the Parameters replace and Type write. Lazy TS JSDoc evidence in `parser/jsdoc.go` `parseJSDocForNode`.

Checker synthetics share parse children. Evidence in `checker.go` `isPropertyInitializedInConstructor`.

Emit `Update*` reuses unchanged nodes. Evidence in `ast_generated.go` Update methods.

Unproven until PR-1. Default GOGC versus `GOGC=off` versus `GOMEMLIMIT` on `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts`. `BenchmarkNewProgram` in `compiler/program_test.go` is one `index.ts` and skips without embedded libs. Do not use it. `TestE2ELayoutReport` is parse plus forced GC, not a full check.

Unproven until PR-7. Diagnostic equality versus trunk on that same command, `TestLocal` (includes `verifyParentPointers` in `compiler_runner.go`), and CI smoke `./built/local/tsc -p ./smoke/typescript-6.0/src/compiler`.

## Appendix B. Alternatives rejected

JSON-only first wave. `Parser.parseJSONText` shares expression parsers and `NodeFactory`. The old path cannot die in one wave.

Landing PR-3 through PR-6 on microsoft/main. Each would ship a dual tree or a broken binder. The stack stays off microsoft/main until PR-8.

Second emit Store plus mandatory `CopySubtree` of the file. `Update*` reuses parse nodes. Child edges are `NodeRef` and cannot cross stores.

Steady `*Node` facade over Store. Two live trees erase the GC bet.

Column-per-field SoA. Multi-field walks lost to packed `nodeHeader`.

`npx hereby test` as PR-1's only probe. It does not isolate GOGC. PR-1 uses `./built/local/tsc` under explicit env.

## Appendix C. Risks

This repo's `origin/main` has no `pstack/` tree. Owners read playbooks from the local Cursor plugin cache when `git show origin/main:pstack/...` fails. Watch every PR.

No `control-cli` skill is in this repo. Live lanes drive `npx hereby build` and `./built/local/tsc`. Watch every live block.

PR-1 may stop the program. If tunables match `GOGC=off`, do not start PR-2.

`ExpandStore` in PR-3 through PR-5 is a dual tree. If it leaks into PR-8, the GC bet is already lost. Watch PR-6 lane 4 and PR-8 lane 7.

`NewFactoryOn` races if two checkers append to one Store. `program.go` documents not mixing types across checkers. Watch PR-5.

Kind schema in PR-3 must cover every parser `New*` kind or expand will drop nodes. Generate from `tools/scripts/tsc/ast.json` (about 102 node types, 193 `New*` methods). Do not hand-write slots. Watch `npx hereby generate` in PR-3.

`npx hereby test` is slow. PR-7 lane 5 may substitute package tests. The owner still runs full hereby once for the PR body.

microsoft/TypeScript requires CLA, `npx hereby generate`, `npx hereby build`, `npx hereby test`, and `npx hereby lint` per CONTRIBUTING.md. Watch PR-8.

## Appendix D. Links and reading list

Read `tsc/internal/ast/STORE.md` before every PR.

Read `tsc/internal/ast/docs/store-beta-plan.md` for the package-only beta exit. This program supersedes it for upstream.

Read `CONTRIBUTING.md` Common tasks and Before submitting a pull request.

Read `tsc/cmd/tsc/main.go` and `tsc/internal/execute` for the compile entry. CI smoke is `./built/local/tsc -p ./smoke/typescript-6.0/src/compiler` in `.github/workflows/ci.yml`.

Read `tsc/internal/testrunner/compiler_runner.go` `verifyParentPointers`. That is the AST parent check inside `TestLocal`.

Read `tools/scripts/tsc/ast.json` and `tools/scripts/tsc/generate-go-ast.ts` before PR-3.

Read `tsc/internal/parser/parser.go`, `tsc/internal/binder/binder.go`, `tsc/internal/checker/links.go`, `tsc/internal/printer/emitcontext.go`.

Issue write-up at `https://github.com/microsoft/TypeScript/issues/63807`.

PR-3 uses `pstack/skills/how/SKILL.md` on `ParseSourceFile`.

PR-5 uses `pstack/skills/interrogate/SKILL.md` if `GlobalRef` versus per-file `NodeRef` link stores is contested.

Decision trail is `tsc/.audit/store-open-questions.tsv` per `pstack/skills/show-me-your-work/SKILL.md`.
