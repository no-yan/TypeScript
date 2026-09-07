# Prove Store AST speed to maintainers

This program makes each compile phase fast enough and correct enough that a microsoft maintainer can trust the flat-array Store bet. It is for the engineer who will open the upstream request after local proof. The rule is that wall time and diagnostics come from `./built/local/tsc`, not from package benches alone. PR ids in order are PR-1, PR-2, PR-3, PR-4, then PR-5.

## How to read this

One box is one unit of work. Every box names the evidence that checks it. A nested box is a sub-step of the box above it. Check a box only when its evidence exists, a file, a log line, a screenshot, a test run, or a SHA. The body is a how-to. The appendices explain and record.

The program runs `pstack/skills/poteto-mode/playbooks/autopilot-stack.md`. The operator lands every PR. Owners stop at merge-ready. No owner squash-merges. PR-5 is the maintainer-facing proof tip. This plan does not replace `store-upstream-plan.md`. It sits on top of the Handle-native compile path and closes the two blockers that keep that path from scaling or from matching trunk emit.

Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

## Program checklist

### Arm the program

- [ ] State the protocol and this plan to the operator, then stop. Start execution only on her explicit go.
- [ ] On her go, arm a `/goal` with this exact text. "`tsc/internal/ast/docs/store-maintainer-proof-plan.md`. PR-1 then PR-2 and PR-3 in parallel after PR-1, then PR-4 after PR-3, then PR-5 after PR-2 and PR-4. Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. The operator lands every PR. Done when PR-5 records head wall time at most 1.05 times trunk on the named smoke, and diagnostic equality plus `TestLocal/alias` pass at that SHA."
- [ ] Read these from trunk at program start. Re-read them at every tick.
  - [ ] `git show origin/main:pstack/skills/poteto-mode/playbooks/autopilot-stack.md`
  - [ ] `git show origin/main:pstack/skills/swarm/SKILL.md`
  - [ ] `git show origin/main:pstack/skills/poteto-mode/playbooks/opening-a-pr.md`
  - [ ] `git show origin/main:pstack/skills/principle-prove-it-works/SKILL.md`
  - [ ] `git show origin/main:pstack/skills/principle-sequence-verifiable-units/SKILL.md`
  - [ ] `git show origin/main:pstack/skills/principle-separate-before-serializing-shared-state/SKILL.md`
  - [ ] `git show origin/main:.cursor/skills/verify-tsc/SKILL.md`
- [ ] Arm the 30-minute audit tick. In a local session, a real terminal `/loop`. In a cloud root, a cloud-sleeper wake chain. Never leave the cadence to memory.
- [ ] Use this tick prompt, verbatim. "Re-read the execution playbook from trunk and the armed /goal. Audit the operation against both and fix drift in this tick. Probe every active lane and judge progress by side effects only. Stand down a stuck lane and dispatch its replacement now. Then send the operator a status message, whether or not anything changed, with the queue table of PR, owner, state, and head SHA, the verdicts since the last tick, what merged, open operator gates, and blockers."
- [ ] On the operator's hold or stand-down, send every owner a zero-writes order at once.

### Spawn owners

- [ ] Spawn one owner per PR with the full lifecycle the execution playbook names.
- [ ] Follow this dependency graph. Start dependent work only after its parent merges, or base it on the parent branch when the execution playbook stacks.
  - [ ] PR-1 branches from `main`.
  - [ ] PR-2 and PR-3 are independent after PR-1. Both branch from the PR-1 tip.
  - [ ] PR-4 after PR-3.
  - [ ] PR-5 after PR-2 and PR-4. Stop the program before PR-5 if PR-2's checkers scaling rule fails.
- [ ] Hold the file boundaries. PR-1 touches `tsc/internal/ast/**`, `tsc/internal/compiler/**` harness only, `tsc/internal/ast/STORE.md`, and `tsc/.audit/**`. PR-2 touches `tsc/internal/ast/store_identity.go`, `tsc/internal/ast/store_identity_test.go`, `tsc/internal/ast/STORE.md`, and `tsc/.audit/store-open-questions.tsv`. PR-3 touches `tsc/internal/ast/STORE.md`, `tsc/internal/checker/checker.go` asserts only, `tsc/internal/compiler/store_ownership_test.go`, and `tsc/.audit/store-open-questions.tsv`. PR-4 touches `tsc/internal/checker/nodebuilder*.go`, `tsc/internal/printer/emitcontext.go`, `tsc/internal/compiler/emitter.go`, and related tests under those packages. PR-5 touches proof harness files under `tsc/internal/ast/**` and `tsc/internal/execute/**`, plus `STORE.md` and `.audit`.
- [ ] Hold the review gate. PR-5 changes the maintainer proof numbers and waits for the operator's review in chat with screenshots and a video before merge.

### PR mechanics, for every PR

- [ ] Resolve the forge once. Default to `gh`; if `command -v origin` succeeds and Origin can resolve the repository, use `origin pr` for every PR operation. Record any fallback to `gh`. Never require `gt`.
- [ ] Open the PR ready, never draft, with `origin pr create --status open --base <base-branch>` or `gh pr create --base <base-branch>` according to the resolved forge. A stack child targets its parent branch.
- [ ] Run the repo's lint and typecheck once before the PR-facing push. Push with hooks on.
- [ ] Run `/deslop` before each commit and `/no-comments` before review.
- [ ] Triage every Bugbot and security-reviewer comment per `../references/bugbot-triage.md`.
- [ ] Rebase onto current trunk before babysit and again before the merge-ready report.

### Verdict and merge, for every PR

- [ ] At the merge-ready head SHA, run the swarm per `pstack/skills/swarm/SKILL.md`. One gates lane. The ten live lanes from the PR's **Verify, live** block. The perf lane from its **Verify, perf** block. One audit lane that reads the diff and the receipts and distrusts the PR body.
- [ ] Clean only when every lane is `PASS`. Findings go back to the owner. A new head gets a fresh swarm and a fresh verdict.
- [ ] The root appends the PR to the base-branch stack and compares `git patch-id` at the verdict SHA against the new head per `playbooks/shipping.md`. The operator lands every PR bottom-up.

### Boot recipe, for every live lane

Each live lane runs on its own cloud VM at the PR head. Drive through the shell that runs `npx hereby build`, `./built/local/tsc`, and `go test`. This repo has no `control-cli` on trunk. Prefer `./built/local/tsc`.

- [ ] `git fetch origin <head-branch> && git checkout <head SHA>`.
- [ ] From the repo root, run `npx hereby build`. Wait until `built/local/tsc` exists.
- [ ] Deliver input only through `./built/local/tsc`, `go test`, and `npx hereby test`. Read-only diagnostics are `-v` logs and `t.Logf` lines.
- [ ] Save every screenshot to `/tmp/swarm-<pr-id>/worker-<n>/<slug>.png` and return the paths with the report.

## Measure checkers scaling (PR-1)

**Depends on.** None.

**Files.**

- [ ] Create `tsc/internal/ast/store_checkers_scale_test.go`.
- [ ] Edit `tsc/internal/ast/STORE.md`.
- [ ] Edit `tsc/.audit/store-open-questions.tsv`.

**Build.**

- [ ] Add `TestCheckersScaleReport` that builds once, then runs `./built/local/tsc --noEmit` on a multi-file project under `--checkers 1` and `--checkers 4`. Prefer the TypeScript compiler smoke tree when present. Fall back to a checked-in multi-file fixture under `tsc/testdata` if smoke is missing. Record five interleaved medians per mode. Count `StoreSet` `RLock` entries through a test-only counter on `Store`/`At` when `-tags=storecount` is set.
- [ ] Write the medians, the ratio `checkers4/checkers1`, and the RLock count into `STORE.md`.

**You see.**

- [ ] `go test ./internal/ast -run TestCheckersScaleReport -v` prints `checkers=1 median=`, `checkers=4 median=`, and `ratio=`.
- [ ] `STORE.md` contains those three numbers.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `store_checkers_scale_test.go` `TestCheckersScaleReport`. Run `go -C ./tsc test ./internal/ast -count=1 -run TestCheckersScaleReport -v`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Regression lane against trunk. Run `TestCheckersScaleReport` at trunk and head. If trunk lacks the test, record that and gate that head prints the three median lines. Save `pr1-lane1-regress.png`. Pass when head prints `ratio=`.
- [ ] Lane 2. Run with `--checkers 1` only through the harness helper. Save `pr1-lane2-c1.png`. Pass when the log contains `checkers=1 median=`.
- [ ] Lane 3. Run with `--checkers 4` only. Save `pr1-lane3-c4.png`. Pass when the log contains `checkers=4 median=`.
- [ ] Lane 4. Run `./built/local/tsc --help --all` and confirm `checkers` appears. Save `pr1-lane4-help.png`. Pass when the option text is present.
- [ ] Lane 5. Run `go -C ./tsc test ./internal/ast -count=1 -run 'TestStoreParallelFileWriters|TestSourceFileRefsAreSafeAcrossParallelCheckers'`. Save `pr1-lane5-raceish.png`. Pass when the last line contains `ok`.
- [ ] Lane 6. Run `./built/local/tsc --noEmit --checkers 1` on the harness project. Save `pr1-lane6-cli1.png`. Pass when the process exits 0 or prints diagnostics without `Unknown compiler option`.
- [ ] Lane 7. Run the same CLI with `--checkers 4`. Save `pr1-lane7-cli4.png`. Pass when the process exits 0 or prints diagnostics without `Unknown compiler option`.
- [ ] Lane 8. Confirm `STORE.md` lists the three numbers. Save `pr1-lane8-doc.png`. Pass when all three strings appear.
- [ ] Lane 9. Run `go -C ./tsc test ./internal/ast -count=1 -run TestTsgoGOGCBaseline` if present. Save `pr1-lane9-gogc.png`. Pass when the log contains `median=` or the test is absent and the lane records that.
- [ ] Lane 10. `git grep -n 'TestCheckersScaleReport' tsc/internal/ast`. Save `pr1-lane10-grep.png`. Pass when the new test file is listed.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Wall time of the multi-file `--noEmit` under `--checkers 1` and `--checkers 4`, plus optional `StoreSet` RLock count.
- [ ] Probe. `TestCheckersScaleReport` five runs per mode, interleaved. Both modes must produce a median.
- [ ] Baseline. Record the PR-1 HEAD `checkers=1` median first.
- [ ] Rule. Fail if either mode lacks a median. Do not fail on `ratio > 1` yet. The job of PR-1 is to publish the true ratio. Stop and re-pick the multi-file workload if single-file `checker.ts` is the only target, because that fixture already showed `checkers4/checkers1 ≈ 0.65` and cannot prove lock inversion.

**Review gate.** None. PR-1 is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends PR-1 to the base-branch stack. The operator lands it.

## Make StoreSet.At lock-free for readers (PR-2)

**Depends on.** PR-1.

**Files.**

- [ ] Edit `tsc/internal/ast/store_identity.go`.
- [ ] Edit `tsc/internal/ast/store_identity_test.go`.
- [ ] Edit `tsc/internal/ast/STORE.md`.
- [ ] Edit `tsc/.audit/store-open-questions.tsv`.

**Build.**

- [ ] Replace `StoreSet.mu` reader paths on `Store`, `At`, and `File` with an atomic published snapshot of the `stores` and `files` slices. Writers (`Add`, `Remove`, `adopt`, `SetFile`) still take a writer lock, copy the slices, then publish the new snapshot. Callers of `NodeOf` stay unchanged.
- [ ] Keep `RegisterStore` for `Checker.synth` and `UnregisterStore` on `Close`. Do not seal the identity table in this PR.
- [ ] Extend race tests so parallel `NodeOf` runs while another goroutine registers and unregisters a synth Store.

**You see.**

- [ ] `go test -race ./internal/ast -run 'TestSourceFileRefs|TestStoreParallel'` passes.
- [ ] `TestCheckersScaleReport` prints a lower `ratio` than PR-1, or a lower RLock count of zero on the reader path.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] New or extended cases in `store_identity_test.go` for concurrent `At` during `Add`/`Remove`. Run `go -C ./tsc test -race ./internal/ast -count=1 -run 'TestSourceFileRefs|TestStoreParallel|TestStoreSet'`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Regression lane against trunk. Run `TestCheckersScaleReport` at the PR-1 tip and at PR-2 HEAD. Save `pr2-lane1-regress.png`. Pass when both print `ratio=` and head's reader path no longer takes `RLock` on `At`.
- [ ] Lane 2. `go -C ./tsc test -race ./internal/ast -count=1 -run TestSourceFileRefsAreSafeAcrossParallelCheckers`. Save `pr2-lane2-refs.png`. Pass when the last line contains `ok`.
- [ ] Lane 3. `go -C ./tsc test -race ./internal/ast -count=1 -run TestStoreParallelFileWriters`. Save `pr2-lane3-writers.png`. Pass when the last line contains `ok`.
- [ ] Lane 4. `go -C ./tsc test -race ./internal/compiler -count=1 -run 'StoreOwnership|CloseUnregister'`. Save `pr2-lane4-own.png`. Pass when the last line contains `ok` or `PASS`.
- [ ] Lane 5. CLI `--checkers 4` on the PR-1 multi-file project. Save `pr2-lane5-cli4.png`. Pass when exit is 0 or diagnostics print without panic.
- [ ] Lane 6. CLI `--checkers 1` on the same project. Save `pr2-lane6-cli1.png`. Pass when exit is 0 or diagnostics print without panic.
- [ ] Lane 7. `go -C ./tsc test ./internal/ast -count=1 -run TestCheckersScaleReport -v`. Save `pr2-lane7-scale.png`. Pass when `ratio=` prints.
- [ ] Lane 8. Confirm `STORE.md` names the atomic snapshot rule. Save `pr2-lane8-doc.png`. Pass when the concurrency section mentions published snapshot.
- [ ] Lane 9. `git grep -n 'RLock' tsc/internal/ast/store_identity.go`. Save `pr2-lane9-rlock.png`. Pass when reader helpers `Store`/`At`/`File` no longer call `RLock`.
- [ ] Lane 10. Run `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts --checkers 4`. Save `pr2-lane10-fixture.png`. Pass when the process completes without `write to frozen Store` or identity panic.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Multi-file `--noEmit` wall time under `--checkers 4` and `--checkers 1`, using `TestCheckersScaleReport`.
- [ ] Probe. Five interleaved runs at the PR-1 tip and at PR-2 HEAD.
- [ ] Baseline. Record the PR-1 tip `checkers=4` median and `ratio` first.
- [ ] Rule. Fail if PR-2 `checkers=4` median is worse than PR-1 by more than 1.02 times. Fail if PR-2 `ratio` is greater than PR-1 `ratio` when PR-1 `ratio` was above 1.0. If PR-1 `ratio` was already below 1.0, fail only when `checkers=4` regresses versus PR-1, and record that lock inversion was never present on that workload.

**Review gate.** None. PR-2 is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends PR-2 to the base-branch stack. The operator lands it.

## Codify synthetic ownership (PR-3)

**Depends on.** PR-1.

**Files.**

- [ ] Edit `tsc/internal/ast/STORE.md`.
- [ ] Edit `tsc/internal/checker/checker.go` only for ownership asserts already sketched.
- [ ] Edit `tsc/internal/compiler/store_ownership_test.go`.
- [ ] Edit `tsc/.audit/store-open-questions.tsv`.

**Build.**

- [ ] Rewrite the STORE constraints so check synthetics live on `Checker.synth`, cross-store children use sparse `GlobalRef` maps, and emit appends only under `LockParseStoreWriter` plus `EnterEmit`. Delete the stale claim that check synthetics append into the parse Store.
- [ ] Keep or tighten `AssertBinderSymbolsStayOnParseStores`. Add a package test that a frozen parse Store panics on append without `EnterEmit`, and that `Checker.synth` accepts append during check.

**You see.**

- [ ] `STORE.md` states one ownership table for parse, synth, and emit.
- [ ] Ownership tests pass under `-race`.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Ownership and phase tests. Run `go -C ./tsc test -race ./internal/ast ./internal/compiler -count=1 -run 'TestEnterEmit|TestFreeze|StoreOwnership|PrivateStore|AssertBinder'`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Regression lane against trunk. Diff `STORE.md` ownership section at trunk and head. Save `pr3-lane1-docdiff.png`. Pass when head names `Checker.synth` and `EnterEmit` in one table.
- [ ] Lane 2. `go -C ./tsc test ./internal/ast -count=1 -run TestEnterEmit`. Save `pr3-lane2-enter.png`. Pass when the last line contains `ok`.
- [ ] Lane 3. `go -C ./tsc test ./internal/ast -count=1 -run 'TestFreeze'`. Save `pr3-lane3-freeze.png`. Pass when the last line contains `ok`.
- [ ] Lane 4. `go -C ./tsc test ./internal/compiler -count=1 -run 'StoreOwnership|PrivateStore'`. Save `pr3-lane4-own.png`. Pass when the last line contains `ok` or `PASS`.
- [ ] Lane 5. `go -C ./tsc test ./internal/printer -count=1 -run TestEmitContextAppendsIntoParseStore`. Save `pr3-lane5-emit.png`. Pass when the last line contains `ok` or the case is absent and the lane records that.
- [ ] Lane 6. Confirm audit TSV gained a settled row for synth ownership. Save `pr3-lane6-audit.png`. Pass when the new row mentions `Checker.synth`.
- [ ] Lane 7. CLI check on `checker.ts`. Save `pr3-lane7-cli.png`. Pass when no `write to frozen Store` panic appears.
- [ ] Lane 8. `git grep -n 'append into the parse Store' tsc/internal/ast/STORE.md`. Save `pr3-lane8-stale.png`. Pass when check-phase append claims are gone or limited to emit.
- [ ] Lane 9. `go -C ./tsc test ./internal/checker -count=1 -run AssertBinder`. Save `pr3-lane9-assert.png`. Pass when `ok` or no matching test and the assert still compiles.
- [ ] Lane 10. Read `STORE.md` Current code table and confirm deleted bridge files are not listed. Save `pr3-lane10-current.png`. Pass when materialize bridge names are absent.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Wall time of `./built/local/tsc --noEmit tsc/testdata/fixtures/compiler/checker.ts`.
- [ ] Probe. Five interleaved runs at the PR-1 tip and at PR-3 HEAD.
- [ ] Baseline. Record the PR-1 tip median first.
- [ ] Rule. Fail if PR-3 median exceeds PR-1 by more than 1.02 times. Docs and asserts must not change wall time.

**Review gate.** None. PR-3 is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends PR-3 to the base-branch stack. The operator lands it.

## Harden emit and NodeBuilder leases (PR-4)

**Depends on.** PR-3.

**Files.**

- [ ] Edit `tsc/internal/checker/nodebuilderimpl.go` and related nodebuilder files as needed.
- [ ] Edit `tsc/internal/printer/emitcontext.go` if the lease helper must be shared.
- [ ] Edit `tsc/internal/compiler/emitter.go` only if call sites miss the lease.
- [ ] Create or edit tests under `tsc/internal/checker` and `tsc/internal/testrunner`.

**Build.**

- [ ] Fix `EmitContext` pool `Reset` so it does not keep a `Factory` bound to a prior parse Store. That stale factory is what makes `NodeBuilderImpl.newIdentifier` hit `write to frozen Store` during type baselines (`type_symbol_baseline.go` → `GetEmitContext`).
- [ ] After Reset, NodeBuilder and type baseline paths use a private Factory, or take `LockParseStoreWriter` plus `EnterEmit` before allocating into a parse Store.
- [ ] Keep `DefaultVisitEmbeddedStatement` as the non-reentrant visit path. Close the generated `VisitEachChild` gap that still routes bodies through `VisitNode` when `TestLocal` overflows on emit.
- [ ] Fix `TestLocal/alias` frozen-Store panic end to end.

**You see.**

- [ ] `go -C ./tsc test ./internal/testrunner -count=1 -run 'TestLocal/alias'` passes.
- [ ] No `write to frozen Store` in that run.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] NodeBuilder and printer regressions. Run `go -C ./tsc test ./internal/printer ./internal/checker -count=1 -run 'TestVisitIterationBodyDoesNotRecurse|TestEmitContext|NodeBuilder|alias'`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Regression lane against trunk. Run `TestLocal/alias` at trunk and head. If trunk lacks Store and panics differently, record that and gate head exit without frozen-Store panic. Save `pr4-lane1-alias.png`. Pass when head's last lines contain `PASS` or `ok`.
- [ ] Lane 2. `go -C ./tsc test ./internal/printer -count=1 -run TestVisitIterationBodyDoesNotRecurse`. Save `pr4-lane2-visit.png`. Pass when the last line contains `ok`.
- [ ] Lane 3. `go -C ./tsc test ./internal/printer -count=1 -run TestEmitContextAppendsIntoParseStore`. Save `pr4-lane3-append.png`. Pass when `ok` or absent-with-note.
- [ ] Lane 4. Declaration baseline path that previously wrote during check. Save `pr4-lane4-baseline.png`. Pass when no frozen-Store panic.
- [ ] Lane 5. CLI emit on a small fixture that triggers transformers. Save `pr4-lane5-emit.png`. Pass when exit is 0.
- [ ] Lane 6. CLI `--noEmit` on `checker.ts`. Save `pr4-lane6-check.png`. Pass when no frozen-Store panic.
- [ ] Lane 7. `go -C ./tsc test -race ./internal/compiler -count=1 -run TestSourceFileSerializesParseStoreWriters` if reachable, else printer lease test. Save `pr4-lane7-lease.png`. Pass when `ok`.
- [ ] Lane 8. Confirm NodeBuilder factory selection is documented in a test name or STORE note. Save `pr4-lane8-doc.png`. Pass when the lease rule is stated once.
- [ ] Lane 9. `go -C ./tsc test ./internal/testrunner -count=1 -run 'TestLocal/alias' -v`. Save `pr4-lane9-verbose.png`. Pass when the case passes.
- [ ] Lane 10. Spot-check one other `TestLocal` case that builds type display. Save `pr4-lane10-local.png`. Pass when that case passes or is skipped with a named reason.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Wall time of `./built/local/tsc --noEmit` on the PR-1 multi-file project under `--checkers 4`.
- [ ] Probe. Five interleaved runs at the PR-3 tip and at PR-4 HEAD.
- [ ] Baseline. Record the PR-3 tip median first.
- [ ] Rule. Fail if PR-4 median exceeds PR-3 by more than 1.05 times. Correctness fixes may cost a little. They must not erase the check-phase work.

**Review gate.** None. PR-4 is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends PR-4 to the base-branch stack. The operator lands it.

## Prove head against trunk for maintainers (PR-5)

**Depends on.** PR-2 and PR-4.

**Files.**

- [ ] Edit or create the e2e harness under `tsc/internal/ast/**` and `tsc/internal/execute/**`.
- [ ] Edit `tsc/internal/ast/STORE.md`.
- [ ] Edit `tsc/.audit/store-open-questions.tsv`.

**Build.**

- [ ] Run interleaved `./built/local/tsc` smoke against `origin/main` and PR-5 HEAD on the named multi-file project. Record wall medians, diagnostic equality, and `TestLocal` status.
- [ ] Write the maintainer table into `STORE.md`. Cite PR-2 ratio and PR-4 alias receipts.
- [ ] Do not open the microsoft/TypeScript pull request in this PR. That stays `store-upstream-plan.md` PR-10.

**You see.**

- [ ] `STORE.md` shows head/trunk ≤ 1.05 on the smoke command.
- [ ] Diagnostic diff is empty on that command.
- [ ] `TestLocal/alias` is green at the same SHA.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] E2e harness test. Run `go -C ./tsc test ./internal/ast -count=1 -run 'TestTsgoStoreE2E|TestCheckersScaleReport|TestDiagnosticEquality' -v`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Regression lane against trunk. Run the smoke `--noEmit` at `origin/main` and at PR-5 HEAD. Save `pr5-lane1-smoke.png`. Pass when both complete and head diagnostics match trunk.
- [ ] Lane 2. `TestLocal/alias` at HEAD. Save `pr5-lane2-alias.png`. Pass when the case passes.
- [ ] Lane 3. `TestCheckersScaleReport` at HEAD. Save `pr5-lane3-scale.png`. Pass when `ratio=` prints and `checkers=4` is not slower than `checkers=1` by more than 1.05 times after PR-2.
- [ ] Lane 4. Full `TestLocal` slice the owner can finish inside the lane budget, or a recorded subset with reasons. Save `pr5-lane4-local.png`. Pass when every run case passes.
- [ ] Lane 5. `npx hereby test` package subset named in the PR body. Save `pr5-lane5-hereby.png`. Pass when the subset is green.
- [ ] Lane 6. CLI `--checkers 4` smoke. Save `pr5-lane6-c4.png`. Pass when exit matches trunk's success class.
- [ ] Lane 7. CLI `--checkers 1` smoke. Save `pr5-lane7-c1.png`. Pass when exit matches trunk's success class.
- [ ] Lane 8. Confirm `STORE.md` maintainer table lists trunk median, head median, and ratio. Save `pr5-lane8-doc.png`. Pass when all three numbers appear.
- [ ] Lane 9. Audit TSV row marks the 1.05 gate settled or failed with the SHA. Save `pr5-lane9-audit.png`. Pass when the row exists.
- [ ] Lane 10. `go -C ./tsc test -race ./internal/ast ./internal/compiler -count=1 -run 'TestStoreParallel|TestSourceFileRefs|StoreOwnership'`. Save `pr5-lane10-race.png`. Pass when `ok`.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. Wall time of the multi-file smoke `./built/local/tsc --noEmit` at trunk and at head, plus checkers scaling ratio at head.
- [ ] Probe. Five runs at `origin/main`, then five at PR-5 HEAD, interleaved.
- [ ] Baseline. Record the trunk median first.
- [ ] Rule. Fail if head median is more than 1.05 times trunk. Fail if head `checkers=4` median is more than 1.05 times head `checkers=1` on the multi-file project after PR-2 claimed a fix. Stop the program on fail. Do not open upstream on a failed gate.

**Review gate.** The operator reviews before merge.

- [ ] Copy lane 1, lane 3, and lane 8 screenshots into `tsc/.audit/media/pr-5-review-<slug>.png`.
- [ ] Record a 30 to 60 second video of the smoke interleaved probe on a lane VM. Save it as `tsc/.audit/media/pr-5-review.mp4`.
- [ ] Post the screenshots and the video in chat. Stop at merge-ready. Wait for the operator's click.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends PR-5 to the base-branch stack. The operator lands it.

## Close the program

- [ ] Every box above is checked with its evidence.
- [ ] Reply to the operator with the report the execution playbook names.

## Appendix A. Prototype evidence

Measured on this machine before the plan, five interleaved runs of `./built/local/tsc --noEmit --checkers N tsc/testdata/fixtures/compiler/checker.ts`.

- `checkers=1` median ≈ 1.88s
- `checkers=4` median ≈ 1.22s
- `ratio 4/1 ≈ 0.65`

So single-file `checker.ts` does not show lock inversion. The user's claim needs a multi-file project. PR-1 must pick that project and publish the true ratio. Unproven until PR-1.

Code reading settled that `Store.Freeze` does not publish an immutable `StoreSet`. Check still calls `RegisterStore` for `Checker.synth` and `UnregisterStore` on `Close`, so `stores` stays mutable. Lock-free `At` needs an identity publish rule. That is PR-2.

Candidate designs weighed in exploration, not coded.

- A. Atomic published slice for readers. Smallest file surface. Chosen for PR-2.
- B. Seal parse identity after bind and keep synth in a side domain. Larger. Kept as the follow-up if PR-2's rule fails.
- C. Per-checker private snapshot. Rejected for first try because every `NodeOf` call site must learn the snapshot.

Emit ownership prototype is documentary. HEAD already splits `Checker.synth`, sparse `externalChild`, and emit `EnterEmit`. The open bug for `TestLocal/alias` is a pooled `EmitContext` whose `Reset` leaves `Factory = NewFactoryOn(parseStore)` from a prior emit, so baseline `newIdentifier` writes into a frozen Store. VisitEmbedded reentry has a unit fix (`TestVisitIterationBodyDoesNotRecurse`) but generated `VisitEachChild` may still miss the hook on full `TestLocal`. Both stay unproven until PR-4.

## Appendix B. Alternatives rejected

Fixing emit and locks in one PR. Different failure modes. A red alias case would hide a scaling win.

Making `StoreSet.mu` finer-grained while keeping `RLock` on every `NodeOf`. That still serializes the hot path. Separate the shared table first.

Putting check synthetics back onto the parse Store under a per-file lease during check. That reintroduces shared writers across checkers and fights Freeze. HEAD already rejected it.

Dual-write or materialize bridges for a temporary speed story. Prior preflight showed head/trunk ≈ 2.8. Bridges cannot carry the maintainer claim.

Asking which lock-free design to pick before measuring. PR-1 publishes the ratio. PR-2 ships the smallest reader fix. Escalate to seal-plus-synth only if the gate fails.

## Appendix C. Risks

PR-1 picks a workload that is still single-file in disguise. Owner watches file count and `NodeOf` volume.

PR-2 atomic snapshots keep old slice pointers alive across `Remove`. Owner watches synth `Close` races and `-race` on identity tests.

PR-2 helps little if synth `Add`/`Remove` traffic dominates. Owner watches the PR-2 perf rule and is ready with design B as a new PR, not a silent scope expand.

PR-4 lease wrapping slows type display paths. Owner watches the 1.05 rule versus PR-3.

PR-5 diagnostic equality may fail for reasons outside Store. Owner bisects with PR-4 alias first, then unrelated trunk drift.

## Appendix D. Links and reading list

Read before editing.

- `tsc/internal/ast/STORE.md`
- `tsc/internal/ast/docs/store-upstream-plan.md`
- `tsc/.audit/store-open-questions.tsv`
- `tsc/internal/ast/store_identity.go`
- `tsc/internal/checker/checker.go` (`NewChecker`, `Close`, synth factory)
- `tsc/internal/printer/emitcontext.go`
- `tsc/internal/compiler/checkerpool.go`
- `.cursor/skills/verify-tsc/SKILL.md`
- `.github/skills/store-ast-verification/SKILL.md`

PR-2 and PR-5 get `pstack/skills/how/SKILL.md` and `pstack/skills/interrogate/SKILL.md` before the first push. Every owner keeps a `decisions.tsv` trail per `pstack/skills/show-me-your-work/SKILL.md`. Keep it local unless the operator asks to commit it.
