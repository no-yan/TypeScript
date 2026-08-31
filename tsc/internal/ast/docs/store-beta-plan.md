# Close Store β plan

This program finishes the Store-backed AST experiment APIs in `tsc/internal/ast`. It is for the engineer who will start a parser wave later. The rule is that β ends when identity, list rewrite, binder side data, and a no-code GC baseline all exist in code or in a recorded run. PR ids in order are PR-1, PR-2, and PR-3. Parser, binder, checker, and printer stay on `*Node`.

## How to read this

One box is one unit of work. Every box names the evidence that checks it. A nested box is a sub-step of the box above it. Check a box only when its evidence exists, a file, a log line, a screenshot, a test run, or a SHA. The body is a how-to. The appendices explain and record.

The program runs `pstack/skills/poteto-mode/playbooks/autopilot-stack.md`. The operator lands every PR. Owners stop at merge-ready. No owner squash-merges.

Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

## Program checklist

### Arm the program

- [ ] State the protocol and this plan to the operator, then stop. Start execution only on her explicit go.
- [ ] On her go, arm a `/goal` with this exact text. "`tsc/internal/ast/docs/store-beta-plan.md`. PR-1 then PR-2 then PR-3. Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. The operator lands the Graphite stack. Done when STORE.md marks β complete and no parser file is edited."
- [ ] Read these from trunk at program start. Re-read them at every tick.
  - [ ] `git show origin/main:pstack/skills/poteto-mode/playbooks/autopilot-stack.md`
  - [ ] `git show origin/main:pstack/skills/swarm/SKILL.md`
  - [ ] `git show origin/main:pstack/skills/poteto-mode/playbooks/opening-a-pr.md`
  - [ ] `git show origin/main:pstack/skills/principle-prove-it-works/SKILL.md`
  - [ ] `git show origin/main:pstack/skills/principle-sequence-verifiable-units/SKILL.md`
- [ ] Arm the 30-minute audit tick. In a local session, a real terminal `/loop`. In a cloud root, a cloud-sleeper wake chain. Never leave the cadence to memory.
- [ ] Use this tick prompt, verbatim. "Re-read the execution playbook from trunk and the armed /goal. Audit the operation against both and fix drift in this tick. Probe every active lane and judge progress by side effects only. Stand down a stuck lane and dispatch its replacement now. Then send the operator a status message, whether or not anything changed, with the queue table of PR, owner, state, and head SHA, the verdicts since the last tick, what merged, open operator gates, and blockers."
- [ ] On the operator's hold or stand-down, send every owner a zero-writes order at once.

### Spawn owners

- [ ] Spawn one owner per PR with the full lifecycle the execution playbook names.
- [ ] Follow this dependency graph. Start dependent work only after its parent merges, or base it on the parent branch when the execution playbook stacks.
  - [ ] PR-1 branches from `main`.
  - [ ] PR-2 after PR-1. Stop the program before PR-2 if PR-1's perf rule fails.
  - [ ] PR-3 after PR-2.
- [ ] Hold the file boundaries. PR-1 and PR-3 touch only `tsc/internal/ast/**`. PR-2 touches only `tsc/internal/ast/store*.go`, `tsc/internal/ast/STORE.md`, and `tsc/.audit/store-open-questions.tsv`.
- [ ] Hold the review gate. No PR in this program changes an interaction.

### PR mechanics, for every PR

- [ ] Open the PR ready, never draft, with `gh pr create` and `draft: false`, or with Graphite `gt` for a stack.
- [ ] Run the repo's lint and typecheck once before the PR-facing push. Push with hooks on.
- [ ] Run `/deslop` before each commit and `/no-comments` before review.
- [ ] Triage every Bugbot and security-reviewer comment per `../references/bugbot-triage.md`.
- [ ] Rebase onto current trunk before babysit and again before the merge-ready report.

### Verdict and merge, for every PR

- [ ] At the merge-ready head SHA, run the swarm per `pstack/skills/swarm/SKILL.md`. One gates lane. The ten live lanes from the PR's **Verify, live** block. The perf lane from its **Verify, perf** block. One audit lane that reads the diff and the receipts and distrusts the PR body.
- [ ] Clean only when every lane is `PASS`. Findings go back to the owner. A new head gets a fresh swarm and a fresh verdict.
- [ ] The root appends the PR to the Graphite stack. Compare `git patch-id` at the verdict SHA against the new head per `playbooks/shipping.md`. The operator lands the chain. No owner merges.

### Boot recipe, for every live lane

Each live lane runs on its own cloud VM at the PR head. Drive through the shell that runs `go test` and `go run ./cmd/tsc` from `tsc/`. This repo has no `control-cli` on trunk.

- [ ] `git fetch origin <head-branch> && git checkout <head SHA>`.
- [ ] `cd tsc`. Wait until `go test ./internal/ast -count=0` compiles.
- [ ] Deliver input only through `go test` and `go run ./cmd/tsc`. Read-only diagnostics are `-v` logs and `t.Logf` lines.
- [ ] Save every screenshot to `/tmp/swarm-<pr-id>/worker-<n>/<slug>.png` and return the paths with the report.

## Record the GOGC-only baseline (PR-1)

**Depends on.** None.

**Files.**

- [ ] Create `tsc/internal/ast/store_gogc_baseline_test.go`.
- [ ] Edit `tsc/internal/ast/STORE.md`.

**Build.**

- [ ] Add `TestGOGCBaseline` in `store_gogc_baseline_test.go`. Reuse the factory forced-GC loop from `TestE2ELayoutReport` on `tsc/testdata/fixtures/compiler/checker.ts` via `parseBenchFixture`. Run default `GOGC`, `GOGC=off`, `GOGC=200`, and `GOMEMLIMIT=8GiB`. Do not reuse `gogcEnv` for the memlimit row. That helper only reads `GOGC`.
- [ ] Write the four medians into `STORE.md` under the β exit table.

**You see.**

- [ ] `go test ./internal/ast -run TestGOGCBaseline -v` prints four `median=` lines.
- [ ] `STORE.md` contains those four numbers.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `store_gogc_baseline_test.go` `TestGOGCBaseline`. Run `cd tsc && go test ./internal/ast -count=1 -run TestGOGCBaseline -v`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Run `TestGOGCBaseline` with default env. Save `pr1-lane1-default.png`. Pass when the log contains `median=`.
- [ ] Lane 2. Run with `GOGC=off`. Save `pr1-lane2-gogc-off.png`. Pass when the log contains `GOGC=off`.
- [ ] Lane 3. Run with `GOGC=200`. Save `pr1-lane3-gogc-200.png`. Pass when the log contains `GOGC=200`.
- [ ] Lane 4. Run with `GOMEMLIMIT=8GiB`. Save `pr1-lane4-memlimit.png`. Pass when the log contains `GOMEMLIMIT`.
- [ ] Lane 5. Run `go test ./internal/ast -count=1 -run 'TestStore|TestFactory|TestGlobal'`. Save `pr1-lane5-unit.png`. Pass when the last line contains `ok`.
- [ ] Lane 6. Run `go test ./internal/ast -run TestE2ELayoutReport -v`. Save `pr1-lane6-layout.png`. Pass when the log contains `store live`.
- [ ] Lane 7. Run `go test ./internal/ast -run '^$' -bench E2EWalkFactory -count 1`. Save `pr1-lane7-bench-factory.png`. Pass when the log contains `BenchmarkE2EWalkFactory`.
- [ ] Lane 8. Run `go test ./internal/ast -run '^$' -bench E2EWalkStore -count 1`. Save `pr1-lane8-bench-store.png`. Pass when the log contains `BenchmarkE2EWalkStore`.
- [ ] Lane 9. From `tsc/`, run `go run ./cmd/tsc --help`. Save `pr1-lane9-tsc-help.png`. Pass when the process exits 0 or prints usage.
- [ ] Lane 10. `git grep -n 'NewFactory' tsc/internal -- ':!tsc/internal/ast/*'`. Save `pr1-lane10-no-prod-wiring.png`. Pass when production packages outside `ast` still do not call `ast.NewFactory`.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Factory forced-GC median on `checker.ts` from `TestGOGCBaseline`.
- [ ] Probe. `cd tsc && go test ./internal/ast -count=1 -run TestGOGCBaseline -v`, once per env, interleaved with a trunk checkout of the same test if trunk lacks it, else record HEAD-only and mark Appendix A.
- [ ] Baseline. Record the trunk default-GOGC median first. If trunk has no test, record HEAD default-GOGC as the baseline and say so in the PR body.
- [ ] Rule. Fail if default-GOGC median divided by `GOGC=off` median is under 1.10, or if `GOMEMLIMIT` median divided by `GOGC=off` median is at most 1.05.

**Review gate.** None. PR-1 is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends PR-1 to the Graphite stack. The operator lands it.

## Add remaining Store side tables (PR-2)

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

- [ ] Add `Locals` and `NextContainer` side maps on `Store`, with `Handle` getters and setters, matching `LocalsContainerBase` in `ast_generated.go` and the writes in `binder.go`.
- [ ] Add `NewFactoryOn(*Store, FactoryHooks)` so checker synthetics and emit updates append into the parse Store.
- [ ] Add `StoreSet.SetFile` and `StoreSet.File` so `GetSourceFileOfNode` can resolve metadata by `StoreID`.
- [ ] Keep `SetChild` panicking across stores.

**You see.**

- [ ] `TestFactoryOnExistingStore` allocates a second `Factory` on one `Store` and the child `NodeRef` is valid in that Store.
- [ ] `TestStoreSetFile` returns the same `*SourceFile` that `SetFile` stored.
- [ ] `TestStoreLocalsAndNextContainer` round-trips a `SymbolTable` and a `NodeRef`.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `store_factory_test.go` and `store_identity_test.go` cases named above. Run `cd tsc && go test ./internal/ast -count=1 -run 'TestFactory|TestStore|TestGlobal'`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Run `TestFactoryOnExistingStore`. Save `pr2-lane1-factory-on.png`. Pass when the log contains `PASS`.
- [ ] Lane 2. Run `TestStoreSetFile`. Save `pr2-lane2-setfile.png`. Pass when the log contains `PASS`.
- [ ] Lane 3. Run `TestStoreLocalsAndNextContainer`. Save `pr2-lane3-locals.png`. Pass when the log contains `PASS`.
- [ ] Lane 4. Run a test that `SetChild` across two stores panics. Save `pr2-lane4-cross-store.png`. Pass when the log contains `PASS`.
- [ ] Lane 5. Run `TestFactoryCopySubtreeRemapsList`. Save `pr2-lane5-copy-list.png`. Pass when the log contains `PASS`.
- [ ] Lane 6. Run `TestFactoryParamTypeWrittenAfterCreate`. Save `pr2-lane6-param-type.png`. Pass when the log contains `PASS`.
- [ ] Lane 7. Run `TestFactoryReplaceListAfterCreate`. Save `pr2-lane7-setlist.png`. Pass when the log contains `PASS`.
- [ ] Lane 8. Run `TestStoreSealDropsInternIndex`. Save `pr2-lane8-intern-after-seal.png`. Pass when the log contains `PASS`.
- [ ] Lane 9. Run `go test ./internal/ast -count=1`. Save `pr2-lane9-package.png`. Pass when the last line contains `ok`.
- [ ] Lane 10. `git grep -n 'NewFactoryOn' tsc/internal/parser tsc/internal/binder tsc/internal/checker`. Save `pr2-lane10-no-callers.png`. Pass when those packages have zero hits.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. `BenchmarkWalkStore` ns/op.
- [ ] Probe. `cd tsc && go test ./internal/ast -run '^$' -bench BenchmarkWalkStore -count 3` at trunk of the parent branch, then at HEAD, interleaved.
- [ ] Baseline. Record the parent-branch median ns/op first.
- [ ] Rule. Fail if HEAD median is more than 1.20 times the parent median.

**Review gate.** None. PR-2 is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends PR-2 to the Graphite stack. The operator lands it.

## Mark Store β complete (PR-3)

**Depends on.** PR-2.

**Files.**

- [ ] Edit `tsc/internal/ast/STORE.md`.
- [ ] Edit `tsc/.audit/store-open-questions.tsv`.

**Build.**

- [ ] Flip every β exit row in `STORE.md` to done or to "out of program, parser wave".
- [ ] State that a parser wave is a new plan. Do not add parser files.

**You see.**

- [ ] `STORE.md` β table has no row still marked "not started".
- [ ] `git diff --name-only` against PR-2 contains no path under `tsc/internal/parser`.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `go test ./internal/ast -count=1 -run 'TestStore|TestFactory|TestGlobal'`. Run `cd tsc && go test ./internal/ast -count=1 -run 'TestStore|TestFactory|TestGlobal'`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Open `STORE.md` β table. Save `pr3-lane1-exit-table.png`. Pass when no exit row says `not started`.
- [ ] Lane 2. Run the ast package tests. Save `pr3-lane2-ast-ok.png`. Pass when the last line contains `ok`.
- [ ] Lane 3. `git diff origin/main --name-only -- tsc/internal/parser`. Save `pr3-lane3-no-parser.png`. Pass when the output is empty or equals the parent branch parser diff.
- [ ] Lane 4. `git diff origin/main --name-only -- tsc/internal/binder`. Save `pr3-lane4-no-binder.png`. Pass when the output is empty or equals the parent branch binder diff.
- [ ] Lane 5. `git diff origin/main --name-only -- tsc/internal/checker`. Save `pr3-lane5-no-checker.png`. Pass when the output is empty or equals the parent branch checker diff.
- [ ] Lane 6. `git diff origin/main --name-only -- tsc/internal/printer`. Save `pr3-lane6-no-printer.png`. Pass when the output is empty or equals the parent branch printer diff.
- [ ] Lane 7. Grep `STORE.md` for `β is done`. Save `pr3-lane7-status.png`. Pass when the file says β APIs are complete.
- [ ] Lane 8. Run `TestE2ELayoutReport -v`. Save `pr3-lane8-layout.png`. Pass when the log contains `store live`.
- [ ] Lane 9. Run `go run ./cmd/tsc --help` from `tsc/`. Save `pr3-lane9-cli.png`. Pass when the process prints usage or exits 0.
- [ ] Lane 10. Confirm `.audit/store-open-questions.tsv` has a `beta-complete` row. Save `pr3-lane10-audit.png`. Pass when that row exists.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. `BenchmarkWalkStore` ns/op.
- [ ] Probe. `cd tsc && go test ./internal/ast -run '^$' -bench BenchmarkWalkStore -count 3` at PR-2 HEAD, then at PR-3 HEAD.
- [ ] Baseline. Record the PR-2 median first.
- [ ] Rule. Fail if PR-3 median is more than 1.05 times the PR-2 median. Docs must not change walk cost.

**Review gate.** None. PR-3 is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends PR-3 to the Graphite stack. The operator lands it.

## Close the program

- [ ] Every box above is checked with its evidence.
- [ ] Reply to the operator with the report the execution playbook names.

## Appendix A. Prototype evidence

Package layout benches in `store_e2e_bench_test.go` and `store_e2e_report_test.go` showed packed-header Store smaller than the pointer tree on flattened `checker.ts`. They do not measure a full `tsgo` check.

Identity tests in `store_identity_test.go` proved `GlobalRef` pack and `StoreSet` resolve.

List tests in `store_factory_test.go` and `store_copy_test.go` proved `SetList`, late `SetChild` on Parameter, Intern after Seal, and `copyList`.

Unproven until PR-1. Default `GOGC` versus `GOGC=off` versus `GOMEMLIMIT` on `tsc/testdata/fixtures/compiler/checker.ts`. That is still a flattened-file GC loop, not a full `tsgo` check. `BenchmarkNewProgram` in `compiler/program_test.go` skips unless `bundled.Embedded` and compiles one `index.ts`. Do not use it as the Store baseline.

## Appendix B. Alternatives rejected

JSON-only first wave. `Parser.parseJSONText` in `parser/parser.go` shares expression parsers and `NodeFactory`. The old path cannot die in one wave.

Second emit Store plus mandatory `CopySubtree`. `Update*` reuses unchanged parse nodes. Child edges are `NodeRef` and cannot cross stores.

Column-per-field SoA. Multi-field walks lost to packed `nodeHeader` on the package benches.

## Appendix C. Risks

This repo's `origin/main` has no `pstack/` tree. Owners read playbooks from the local Cursor plugin cache when `git show origin/main:pstack/...` fails. Watch PR-1.

No `control-cli` skill is in this repo. Live lanes drive `go test` and `go run ./cmd/tsc`. Watch every PR's live block.

PR-1 may stop the program. If tunables match `GOGC=off`, do not start PR-2.

`NewFactoryOn` in PR-2 can race if two checkers append to one Store. This program only adds the API. Parallel append policy stays a parser-wave risk.

## Appendix D. Links and reading list

Read `tsc/internal/ast/STORE.md` before every PR.

Read `tsc/internal/ast/store.go`, `store_factory.go`, `store_identity.go`.

Read `tsc/internal/binder/binder.go` and `tsc/internal/ast/utilities.go` `GetLocals` before PR-2 Locals and NextContainer.

`NewFactory` always calls `NewStore`. There is no existing-Store constructor until PR-2.

PR-1 uses `pstack/skills/how/SKILL.md` on `TestE2ELayoutReport`.

PR-2 uses `pstack/skills/interrogate/SKILL.md` if `NewFactoryOn` versus mutating `Factory.store` is contested.

Decision trail is `tsc/.audit/store-open-questions.tsv` per `pstack/skills/show-me-your-work/SKILL.md`.
