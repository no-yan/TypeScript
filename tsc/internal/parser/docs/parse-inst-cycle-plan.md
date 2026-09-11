# Cut parser Store inst and cycles

This program finishes parse-tree construction on `NodeRef` so the parser stops boxing a `Handle` per node. The user of `built/local/tsc` sees the same `--noEmit` and `--noCheck` behavior. The rule is that checker.ts kpc inst/op and cycles/op must both fall or stay vs the parent, never rise. Order is P1, then P2, then P3.

## How to read this

One box is one unit of work. Every box names the evidence that checks it. A nested box is a sub-step of the box above it. Check a box only when its evidence exists, a file, a log line, a screenshot, a test run, or a SHA. The body is a how-to. The appendices explain and record.

The program runs `pstack/skills/poteto-mode/playbooks/autopilot-stack.md`. The operator merges P1, P2, and P3. Owners stop at STACK-READY.

Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

## Program checklist

### Arm the program

- [ ] State the protocol and this plan to the operator, then stop. Start execution only on her explicit go.
- [ ] On her go, arm a `/goal` with this exact text. "Run `tsc/internal/parser/docs/parse-inst-cycle-plan.md`. PR ids P1 then P2 then P3. A PR is verified only when its unit, live, and perf boxes are all checked. The operator merges. Done when parser.go jsdoc.go and reparser.go construct trees as NodeRef until SetParseStore, and checker.ts parse kpc inst/op and cycles/op do not rise vs a2038fc069."
- [ ] Read these from trunk at program start. Re-read them at every tick.
  - [ ] `git show origin/main:pstack/skills/poteto-mode/playbooks/autopilot-stack.md`
  - [ ] `git show origin/main:pstack/skills/swarm/SKILL.md`
  - [ ] `git show origin/main:.cursor/skills/verify-tsc/SKILL.md`
  - [ ] `git show origin/main:pstack/skills/poteto-mode/playbooks/opening-a-pr.md`
  - [ ] `git show origin/main:pstack/skills/how/SKILL.md`
  - [ ] `git show origin/main:pstack/skills/interrogate/SKILL.md`
  - [ ] `git show origin/main:pstack/skills/show-me-your-work/SKILL.md`
- [ ] Arm the 30-minute audit tick. In a local session, a real terminal `/loop`. In a cloud root, a cloud-sleeper wake chain. Never leave the cadence to memory.
- [ ] Use this tick prompt, verbatim. "Re-read the execution playbook from trunk and the armed /goal. Audit the operation against both and fix drift in this tick. Probe every active lane and judge progress by side effects only. Stand down a stuck lane and dispatch its replacement now. Then send the operator a status message, whether or not anything changed, with the queue table of PR, owner, state, and head SHA, the verdicts since the last tick, what merged, open operator gates, and blockers."
- [ ] On the operator's hold or stand-down, send every owner a zero-writes order at once.

### Spawn owners

- [ ] Spawn one owner per PR with the full lifecycle the execution playbook names.
- [ ] Follow this dependency graph. Start dependent work only after its parent merges, or base it on the parent branch when the execution playbook stacks.
  - [ ] P1 branches from `perf/parser-store-build-writes`.
  - [ ] P2 after P1.
  - [ ] P3 after P2.
- [ ] Hold the file boundaries. P1 touches only `tools/scripts/tsc/generate-go-ast.ts`, `tsc/internal/ast/store.go`, `tsc/internal/ast/store_factory.go`, and generated `tsc/internal/ast/store_handles_generated.go`. P2 touches only `tsc/internal/parser/parser.go` and `tsc/internal/parser/references.go`. P3 touches only `tsc/internal/parser/jsdoc.go` and `tsc/internal/parser/reparser.go`. Tests next to those files may change.
- [ ] Hold the review gate. No PR in this program changes an interaction.

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
- [ ] The root appends the PR to the base-branch stack. The operator lands it bottom-up. No owner merges. Re-verify if `git patch-id` of the base-to-head diff changed after rebase, per `playbooks/shipping.md`.

### Boot recipe, for every live lane

Each live lane runs on its own cloud VM at the PR head. Drive through `.cursor/skills/verify-tsc/scripts/control-tsc` as the CLI control skill. `cursor-team-kit` `control-cli` is absent in this repo.

- [ ] `git fetch origin <head-branch> && git checkout <head SHA>`.
- [ ] Export `VERIFY_TSC_RUN_ID` and `GOTOOLCHAIN=local`. Run `./.cursor/skills/verify-tsc/scripts/control-tsc launch --embed`. Run `control-tsc doctor`. Wait until doctor prints `DOCTOR run_id=` and `PASS` for `tsc-bin`.
- [ ] Deliver input only through `control-tsc fixture` and `control-tsc cli --`. Read-only diagnostics are `meta.txt`, `stdout.txt`, `stderr.txt`, and `pmc-summary.txt` under `.cursor/skills/verify-tsc/artifacts/$VERIFY_TSC_RUN_ID/`.
- [ ] Save every screenshot to `/tmp/swarm-<pr-id>/worker-<n>/<slug>.png` and return the paths with the report.

## Emit NodeRef constructors from the generator (P1)

**Depends on.** None. Base `perf/parser-store-build-writes` at `a2038fc069`.

**Files.**

- [ ] Edit `tools/scripts/tsc/generate-go-ast.ts`.
- [ ] Edit `tsc/internal/ast/store.go`.
- [ ] Edit `tsc/internal/ast/store_factory.go`.
- [ ] Edit `tsc/internal/ast/store_handles_generated.go` by running the generator, not by hand.

**Build.**

- [ ] Change `emitStoreFactory` so each kind gets `func (f *Factory) parse<Name>(...) NodeRef` that calls `appendSlots`, `linkChild`, `linkList`, and `setIdent` and returns the id. Keep `New<Name>` as `Handle{s: f.store, id: f.parse<Name>(...), Kind: kind}` plus `OnCreate`. Do not add a second intern or `mustMutate` on that path.

**You see.**

- [ ] `rg 'func \(f \*Factory\) parseIdentifier' tsc/internal/ast/store_handles_generated.go` prints one function. `NewIdentifier` contains `parseIdentifier` and `Handle{`.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `tsc/internal/ast/store_fidelity_test.go` still checks cooked text, raw text, and parents. Run `./.cursor/skills/verify-tsc/scripts/control-tsc go -- test -C ./tsc ./internal/ast -count=1`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Regression lane against trunk. Run monaco `--project src/tsconfig.monaco.json --noEmit --noCheck --declaration false` at `a2038fc069` and at head. If trunk `cursor/ast-store-tests` lacks this stack, record that and gate exit 0 plus stdout `Parse time`. Save `p1-monaco-reg.png`. Pass when both heads exit 0 and head stdout still contains `Parse time`.
- [ ] Lane 2. Drive `control-tsc fixture type-check` then `cli -- -p $SCRATCH/type-check --noEmit`. Save `p1-type-check.png`. Pass when exit is 0 and stdout has no `error TS`.
- [ ] Lane 3. Drive the emit-javascript fixture with `-p` and `--outDir` under scratch. Save `p1-emit-js.png`. Pass when `outputs.txt` lists a nonempty `.js`.
- [ ] Lane 4. Drive report-diagnostics with a known bad assignment. Save `p1-diag.png`. Pass when stdout contains `error TS` and exit is nonzero.
- [ ] Lane 5. Parse a `.json` file through `cli --`. Save `p1-json.png`. Pass when exit is 0.
- [ ] Lane 6. Parse a `.tsx` fixture. Save `p1-tsx.png`. Pass when exit is 0.
- [ ] Lane 7. Drive emit-declarations `--declaration --emitDeclarationOnly`. Save `p1-dts.png`. Pass when a `.d.ts` exists in scratch.
- [ ] Lane 8. Drive `cli -- --version`. Save `p1-version.png`. Pass when stdout starts with `Version `.
- [ ] Lane 9. Drive `cli -- --help`. Save `p1-help.png`. Pass when stdout contains `tsc: The TypeScript Compiler`.
- [ ] Lane 10. Drive an empty `.ts` file `--noEmit`. Save `p1-empty.png`. Pass when exit is 0.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. checker.ts `BenchmarkParseKPC` inst/op and cycles/op at parent `a2038fc069` and at head.
- [ ] Probe. `CGO_ENABLED=1 go test -tags kperf -c` then `GOGC=off sudo … -test.bench '^BenchmarkParseKPC$/^checker.ts$' -test.benchtime=30x -test.count 3` with `KPERF_TESTDATA=/tmp/kperf-testdata`, interleaved parent then head.
- [ ] Baseline. Record the trunk-stack parent first. Median inst/op 303.0M. Median cycles/op 159.3M.
- [ ] Rule. Fail the PR if head median inst/op is more than 1% above 303.0M or head median cycles/op is more than 3% above 159.3M.

**Review gate.** None. P1 is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends P1 to the stack. The operator squash-merges.

## Convert parser.go to NodeRef (P2)

**Depends on.** P1.

**Files.**

- [ ] Edit `tsc/internal/parser/parser.go`.
- [ ] Edit `tsc/internal/parser/references.go`.
- [ ] Edit `tsc/internal/parser/parser_test.go` only if a signature the tests call changed.

**Build.**

- [ ] Change `parse*` locals and returns in `parser.go` from `ast.Handle` to `ast.NodeRef`. Call `p.factory.parse<Name>` and `FinishParse`. Keep `SetParseStore` as the only Handle rebuild at file end. Delete `finishHandle` once it has no callers in this file. Leave `newList` on Handle only if a caller still needs it, else delete it. Keep `listScratch` as `[]NodeRef` and finish lists with `ListRefs`. Convert the densest `Handle` returns first. Those names are `parseOptionalToken`, `parseType`, `parseIdentifier`, `parseIdentifierName`, `parseTokenNode`, and `parseStatement`.

**You see.**

- [ ] `rg 'ast\.Handle' tsc/internal/parser/parser.go` count drops vs P1. `ParseSourceFile` still returns `*ast.SourceFile`. `p.factory.Store().At` appears only at `SetParseStore` and at tests.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `tsc/internal/parser/parser_test.go` `BenchmarkParse` and `TestJSONParseWalksStoreTree`. Run `./.cursor/skills/verify-tsc/scripts/control-tsc go -- test -C ./tsc ./internal/parser -count=1`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Regression lane against trunk. Run the monaco `--noCheck --noEmit` argv at P1 head and P2 head. Save `p2-monaco-reg.png`. Pass when both exit 0 and Bind time stays on stdout.
- [ ] Lane 2. Drive `control-tsc fixture type-check` then `cli -- -p $SCRATCH/type-check --noEmit`. Save `p2-type-check.png`. Pass when exit is 0.
- [ ] Lane 3. Drive emit-javascript `-p` into scratch `outDir`. Save `p2-emit-js.png`. Pass when the `.js` contains the exported symbol from the fixture.
- [ ] Lane 4. Drive a type error `--noEmit`. Save `p2-diag.png`. Pass when stdout contains `error TS` and exit is nonzero.
- [ ] Lane 5. Parse JSON. Save `p2-json.png`. Pass when exit is 0.
- [ ] Lane 6. Parse TSX. Save `p2-tsx.png`. Pass when exit is 0.
- [ ] Lane 7. Emit declarations. Save `p2-dts.png`. Pass when a `.d.ts` exists.
- [ ] Lane 8. Drive `--version`. Save `p2-version.png`. Pass when stdout starts with `Version `.
- [ ] Lane 9. Drive `--help`. Save `p2-help.png`. Pass when stdout contains `tsc: The TypeScript Compiler`.
- [ ] Lane 10. Empty file `--noEmit`. Save `p2-empty.png`. Pass when exit is 0.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. checker.ts `BenchmarkParseKPC` inst/op and cycles/op at P1 head and P2 head.
- [ ] Probe. Same kpc command as P1, interleaved P1 then P2, `GOGC=off`, count 3.
- [ ] Baseline. Record P1 medians first. Expect them near 303.0M inst and 159.3M cycles unless P1 moved them. Write the P1 numbers into the PR body.
- [ ] Rule. Fail P2 if head median inst/op or cycles/op is above the P1 median by more than 1% inst or 3% cycles. Do not ship if IPC falls and cycles do not fall.

**Review gate.** None. P2 is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends P2 onto P1. The operator squash-merges.

## Convert JSDoc and reparse to NodeRef (P3)

**Depends on.** P2.

**Files.**

- [ ] Edit `tsc/internal/parser/jsdoc.go`.
- [ ] Edit `tsc/internal/parser/reparser.go`.

**Build.**

- [ ] Change JSDoc and reparse builders to `NodeRef` the same way as P2. Keep deferred JSDoc off the parse Store. Link the host with `SetParent` only. Do not push JSDoc children into parse-Store lists. Do not grow the parse Store after `Compact`. Eager JSDoc and `CopySubtree` stay on the current Factory Store.

**You see.**

- [ ] `rg 'ast\.Handle' tsc/internal/parser/jsdoc.go tsc/internal/parser/reparser.go` is zero, or only a comment. `ParseSourceFile` still `Compact` then `Seal`.

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] `tsc/internal/parser/parser_test.go` lazy JSDoc and JS reparse cases. Run `./.cursor/skills/verify-tsc/scripts/control-tsc go -- test -C ./tsc ./internal/parser ./internal/binder -count=1`.

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `grok-4.6-fast-xhigh` at the PR head, per the boot recipe.

- [ ] Lane 1. Regression lane against trunk. Run monaco `--noCheck --noEmit` at P2 and P3. Save `p3-monaco-reg.png`. Pass when both exit 0.
- [ ] Lane 2. Type-check fixture `--noEmit`. Save `p3-type-check.png`. Pass when exit is 0.
- [ ] Lane 3. Emit JavaScript. Save `p3-emit-js.png`. Pass when `.js` exists.
- [ ] Lane 4. Known type error. Save `p3-diag.png`. Pass when `error TS` prints.
- [ ] Lane 5. JSON. Save `p3-json.png`. Pass when exit is 0.
- [ ] Lane 6. TSX. Save `p3-tsx.png`. Pass when exit is 0.
- [ ] Lane 7. Declarations. Save `p3-dts.png`. Pass when `.d.ts` exists.
- [ ] Lane 8. `--version`. Save `p3-version.png`. Pass when stdout starts with `Version `.
- [ ] Lane 9. `--help`. Save `p3-help.png`. Pass when the compiler banner prints.
- [ ] Lane 10. A `.js` file with a JSDoc typedef that reparses. Save `p3-jsdoc-reparse.png`. Pass when exit is 0 and no freeze panic on stderr.

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. checker.ts parse kpc inst/op and cycles/op at P2 and P3. monaco `--extendedDiagnostics` Parse time at both.
- [ ] Probe. kpc as in P1. monaco via `control-tsc cli --cwd $VERIFY_TSC_VSCODE_ROOT --slug monaco-nocheck -- --project src/tsconfig.monaco.json --noEmit --noCheck --declaration false --extendedDiagnostics`.
- [ ] Baseline. Record P2 kpc medians and monaco Parse time first.
- [ ] Rule. Fail if checker.ts inst/op or cycles/op rises vs P2 past 1% inst or 3% cycles. Fail if monaco Parse time rises more than 5% vs P2 on a median of 3.

**Review gate.** None. P3 is not review-gated.

**Merge.**

- [ ] Root's clean verdict at the exact head SHA.
- [ ] Bugbot triage done.
- [ ] Rebased onto current trunk after the verdict, patch-id unchanged.
- [ ] The root appends P3 onto P2. The operator squash-merges.

## Close the program

- [ ] Every box above is checked with its evidence.
- [ ] Reply to the operator with the report the execution playbook names.

## Appendix A. Prototype evidence

Compact on vs off after the build-write landing is on branch `perf/parser-store-build-writes` SHA `a2038fc069`. Artifacts live under `.cursor/skills/verify-tsc/artifacts/20260909T1210-parse-writer/`. Compact stays. inst/op 303.0M vs 301.1M. cycles/op 159.3M vs 148.5M.

Parent Store parse before that landing is in `.cursor/skills/verify-tsc/artifacts/20260909T1118-parse-bind/kperf/port-parse.txt`. checker.ts 415.2M inst, 184.5M cycles.

A source-slice intern vs `internIdx` map has no kpc run. Do not start it in this program. Revisit only if P3 still sits more than 10% inst above Original 263.2M.

Whether `parseIdentifier` beats wrapping `Handle` at `NewIdentifier` has no isolated kpc. P1 must not raise inst. P2 is the measurement that decides if NodeRef locals pay.

Leftover cost vs Original after `a2038fc069` is +15% inst and +29% cycles. Order of remaining parse cost is Handle wrap in `createSlots`, then `intern` through `internIdx`, then Compact memcpy, then getters on nodes just built. Freeze, Compact, and `BindSourceFile` as the only public bind entry stay fixed.

`parser.go` still has 104 `ast.Handle` mentions and 16 `ast.NodeRef`. `jsdoc.go` has 119 Handle and 0 NodeRef. `reparser.go` has 102 Handle and 0 NodeRef. Count is `rg -c`, not a profile.

## Appendix B. Alternatives rejected

Drop Compact. Rejected. vscode `--noCheck` live heap fell 7% to 19% in `tsc/internal/ast/docs/store-scratch-compact-lockprofile-bench.md`.

Change `New*` to return `NodeRef` for printer, checker, transformers, `ls`, `compiler`, and `api/encoder` in one wave. Rejected for this program. Those packages are not the parse hot path. Binder does not call `Factory.New*`. `New*` stays a Handle wrapper.

Fuse per-kind parse functions that inlined bind-like semantics. Rejected. Same review problem as fused `bindKind`.

Delete `mustMutate` on public `AllocSlots`. Rejected. Freeze still has to catch checker writes.

## Appendix C. Risks

P2 is a large `parser.go` edit. Watch `finishHandle` leftovers and list loops that still call `.Ref()`. Hot lists already use `listScratch` and `finishScratchList`. Do not route those loops through `Factory.List` Handle children.

P3 can freeze-panic if reparse allocates after `Compact`. Watch `jsdoc.go` and `reparser.go` for `appendSlots` after Seal.

`origin/main` does not host `pstack/`. The arm boxes still name `git show origin/main:pstack/...`. If that fails, read the plugin copy under `~/.cursor/plugins/cache/cursor-public/pstack/` and record the fallback.

Live lanes need a screenshot of CLI text. Capture the artifact `stdout.txt` in the terminal and save the PNG. There is no `control-ui` here.

kpc needs root. A Cursor shell has no TTY, so `sudo` password and `sudo -n` are the wrong probe. Show the macOS admin dialog with `osascript` `do shell script … with administrator privileges` after copying the driver and `go test -c` binaries to `/tmp`. Procedure is `tsc/internal/binder/docs/kperf-bind-measurement.md`. If that dialog is refused, the perf box is blocked. Do not substitute pprof.

## Appendix D. Links and reading list

Read `tsc/internal/parser/docs/parse-noderef-build.md` before P1.

Read `tools/scripts/tsc/generate-go-ast.ts` `emitStoreFactory` and `generateStoreHandles` before P1. Output is `tsc/internal/ast/store_handles_generated.go`. `createSlots` is `tsc/internal/ast/store_factory.go`. `appendSlots`, `intern`, `FinishParse`, `linkChild`, and Compact `cloneExact` are `tsc/internal/ast/store.go`.

Read `tsc/internal/ast/STORE.md` lifecycle (build then freeze) before P1.

Read `tsc/internal/ast/docs/store-scratch-compact-lockprofile-bench.md` before arguing Compact.

Read `.cursor/skills/verify-tsc/SKILL.md` and `features/monaco-nocheck.md` before live lanes.

P1 gets `pstack/skills/how/SKILL.md` on `emitStoreFactory` if the generator shape is unclear. P2 gets `pstack/skills/interrogate/SKILL.md` if Handle leftovers grow instead of shrink. The trail is `decisions.tsv` per `pstack/skills/show-me-your-work/SKILL.md`, uncommitted, returned in the STACK-READY report.
