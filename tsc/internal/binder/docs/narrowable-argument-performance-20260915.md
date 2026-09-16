# hasNarrowableArgument の直接走査の評価（2026-09-15）

## 採否

**採用。測定・検証済みの1行を本体へ取り込んだ。**

checker.ts の割り当ては24,065→14,165 allocs/op（9,900回、41.14%減）、KPC命令数は1.78%減り、確認用比較でも削減が再現した。不要な一時スライスを除く1行の変更で、12入力の意味監査と4 package testも通過している。この明確な割り当て・命令数削減を採用根拠とする。

通常GC wallは主比較で2.01%短縮、確認用で1.20%短縮（非有意）。候補側wallのA/A精度は未達であり、厳密な高速化率は留保する。当初は既存計画の3%時間基準で保留したが、ユーザーの指示により、今回は明確な割り当て・命令数の改善をもって採用した。測定計画・raw・精度判定は変更せず、採否判断の更新を記録する。domは対照入力。

## 候補と比較対象

変更は `hasNarrowableArgument` の1行だけ。

```diff
- for _, argument := range expr.Arguments() { //nolint:modernize
+ for _, argument := range expr.ArgumentsSeq().All() {
```

`Arguments()` は `ArgumentsSeq().Slice()` を返す。`Slice()` 自体も同じ `All()` で列を走査するため、引数順と空列の動作は同じ。`All()` は `iter.Seq2[int, Handle]` なので、添字を捨てて要素を受け取る。`return true` はiteratorへ停止を伝える。読み取りだけの判定であり、引数列を変更する処理はない。

- 選択repo: `/Volumes/SanDisk1TB/worktree/binder-rewrite`。
- 比較前: HEAD `6f6f3ae597fdd73bb3a28b172fe8b647a2694bb8`。比較後: 上記1行を変更した固定overlay。
- `tsgolint_git_rev=null`。TSGolintは実行対象外。
- build用固定コピー: `/Volumes/SanDisk1TB/Library/Caches/binder-foundation-20260915-164851/candidate-repo`、Git metadataは `d7505ce59277cb0117f900e46580d12a58d7810e`。このmetadataと選択HEADを区別してidentityに保存した。
- 前回保存した5127個のGo/module source hashを固定コピーと再照合。選択HEADのbinderも内容一致を確認し、選択作業木との差は候補1行だけと検査した。前後共通のrepo-root探索用test overlayを使用し、foundationテストは両版とも残した。
- 候補clone一覧は `worktrees.txt` に保存。同HEADのコピー、cursor-ast-store-tests、main、flownode、lock系、store-pr系等の別checkoutを無断で比較の起点にしていない。

| Artifact | status | 扱い |
|---|---|---|
| 今回のwall/KPC比較 | `current` | 選択repo_root・両revision欄が一致。計測終了時にsource、overlay、binary、fixture/config、driverのhash一致を再確認 |
| 前回の基盤移行性能比較 | `stale` | 選択HEADが85506e8b7d→6f6f3ae597へ変化。測定値を混ぜず、固定コピーとdriverだけ検証して再利用 |
| 今回の関数別CPU profile | `missing` | KPCはbind区間の総量。Instrumentsの新規採取はしていない |

`current` は今回指定した前後snapshotの来歴・内容照合に対する判定。採用した本体は測定したafter sourceとhash一致を確認し、両版のsourceとpatchも保存した。全stageで期待するBenchmark行を取得できたため `unsupported` はない。

## 方法と検証

[共通化文書](kpc-measurement-workflow.md)に記載した既存ハーネス・保存済みdriverを再利用した。未実装の共通CLIを実装済みとして使ってはいない。

- Apple M1、8GB、Go 1.26.0 darwin/arm64、CGO有効。GOMAXPROCS=8、GOGC=100、GOMEMLIMIT=off。
- checker.ts / dom.generated.d.ts の内容・pathを前後で固定。10個のdistinct/unbound ASTを事前準備、1回GC、bind batchのみ測定し、consumer検査後に登録解除。N=1校正も独立したASTを解放する。
- 通常wallは通常GCとbatchだけ自動GC無効の2条件で96行。KPCはbatch GC無効で48行。計144行、各条件6固定round。各roundでbefore_a/after_a/before_b/after_bを回転・反転し、sampleごとにfresh processを使う。build・テストと性能計測は重ねない。
- a/aが主比較、b/bが確認。12標本へ合算せず、rawをbenchstatで比較。A/Aは同roundの対数比を20,000回bootstrapし、95% CI全体がwall/cycles±1.5%、命令±1%内なら合格。固定回数後の追加測定なし。
- KPCイベント定義はhost DBとのhash一致を確認。前後各2 processで実counter自己検証に成功。PMU設定後のfresh pthreadで計数し、逆行・read失敗を補正しない。EL0可変counterとEL0+EL1固定counterを分離する。
- KPC下のns/opはcounter readを含む別条件。通常wallと合算しない。pprofは使っていない。

| 正確性検証 | 結果 |
|---|---|
| 両版BatchLifetime / KPCBatch | PASS、登録数の復帰と読取境界を確認 |
| 候補のast / binder / checker / compiler package test | 全4 package PASS |
| 前後の意味監査 | 12入力、診断・訪問順・Symbol・Flow・AST保存先の差0 |
| 追加監査入力 | 引数なし、literalのみ、途中の引数、optional call、spread、入れ子、switch条件を含む |

今回の検証範囲は4 package testと12入力の意味監査。全conformance、parse+bind、live/scanは追加実行していない。package testと12入力の監査を全入力の同等性証明とは扱わない。

## Wallと割り当て

主比較の中央値。時間は精度基準を満たさないため観測値として扱う。

| 条件 | 入力 | 前 ns/op | 後 ns/op | 増減 | p |
|---|---|---:|---:|---:|---:|
| wall-natural | checker.ts | 18,121,606.5 | 17,757,948 | -2.01% | .026 |
| wall-natural | dom.generated.d.ts | 6,674,877 | 6,633,958.5 | -0.61% | .699 |
| wall-gc_off | checker.ts | 18,044,510 | 17,847,839.5 | -1.09% | .180 |
| wall-gc_off | dom.generated.d.ts | 6,233,166.5 | 6,361,306 | +2.06% | .240 |

確認用b/bのcheckerは、通常GC −1.20%（p=.180）、GC無効 −1.62%（p=.310）。domは通常GC +1.29%、GC無効 −0.64%でともに非有意。通常wallのA/Aは8条件中1条件だけ合格した（通常GCのbefore/checker）。

| 条件 | 入力 | 前 B/op | 後 B/op | 前 allocs/op | 後 allocs/op |
|---|---|---:|---:|---:|---:|
| wall-natural | checker.ts | 12,994,156 | 12,777,516 | 24,065 | 14,165 |
| wall-natural | dom.generated.d.ts | 7,867,441.5 | 7,867,441.5 | 16,682 | 16,682 |
| wall-gc_off | checker.ts | 12,994,156.5 | 12,777,516 | 24,065 | 14,165 |
| wall-gc_off | dom.generated.d.ts | 7,869,172 | 7,865,479 | 16,682 | 16,681 |

checkerは通常GC・GC無効・KPC、主比較・確認用のすべてで9,900 allocs/op減少（p=.002）。通常GCのB/opは216,640 bytes（1.67%）減少した。domの割り当てはほぼ不変。

## KPC

| 入力 | 前 EL0 inst/op | 後 EL0 inst/op | 増減 | p | 前 EL0 cycles/op | 後 EL0 cycles/op | 増減 | p |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| checker.ts | 203,998,673 | 200,360,746 | -1.78% | .002 | 98,974,499.5 | 98,484,233.5 | -0.50% | 1.000 |
| dom.generated.d.ts | 83,557,693 | 83,475,992 | -0.10% | .310 | 35,784,192.5 | 35,280,504.5 | -1.41% | .485 |

checkerの命令数は確認用でも1.99%減少した。命令A/Aは全4条件合格し、削減は再現した。domの主比較は非有意、確認用は+0.007%で、安定した命令数削減は確認していない。cyclesのA/Aは全4条件不合格で、cycles改善は未確定。

| A/A・版・入力 | 命令95% CI (%) | cycles95% CI (%) |
|---|---|---|
| before / checker.ts | -0.062〜+0.421 合格 | -1.328〜+1.579 不合格 |
| after / checker.ts | -0.126〜+0.145 合格 | -3.260〜+1.765 不合格 |
| before / dom.generated.d.ts | -0.108〜+0.310 合格 | -4.133〜+2.193 不合格 |
| after / dom.generated.d.ts | -0.111〜+0.222 合格 | +0.558〜+4.643 不合格 |

補助イベントのchecker主比較は、分岐数−1.60%、L1D load miss−2.83%、分岐ミス+0.92%。専用の精度gateは設定していない。補助イベント、固定counter、KPC下のns/op・B/op・allocs/opの全値と比較はartifactに保存した。

## 診断と次の行動

現在の狭い改善箇所は `hasNarrowableArgument → Handle.Arguments → NodeSeq.Slice` の一時スライス生成。1行だけを変えた対照で、前回の割り当て診断と同じ9,900回の削減を確認した。命令数の改善は約1.8〜2.0%であり、基盤移行時に増えた命令数全体を解消する規模ではない。

広いhot pathは以前のInstrumentsではwalk/helpersだったが、そのCPU配分は今回には `stale`。今回のKPC総量から残りの時間を個別helperへ配賦しない。Flow/列/Symbol/Localsの割り当てはこの変更の対象外。symbol synthetic benchmarkは実施していない。

この1行を採用し、残る広いwalk/helper境界の費用は別候補として独立比較する。明確な割り当て・命令数の削減と、wallの精度不足を分けて報告する。今回の採用判断を変えるための追加roundや、別最適化の混入は行っていない。

## Artifact

保存先: [/Volumes/SanDisk1TB/Library/Caches/binder-narrowable-20260915](/Volumes/SanDisk1TB/Library/Caches/binder-narrowable-20260915)。実行時runtimeは `/private/tmp/binder-narrowable-20260915`。

- [identity.json](/Volumes/SanDisk1TB/Library/Caches/binder-narrowable-20260915/identity.json)、[protocol.md](/Volumes/SanDisk1TB/Library/Caches/binder-narrowable-20260915/protocol.md)、[worktrees.txt](/Volumes/SanDisk1TB/Library/Caches/binder-narrowable-20260915/worktrees.txt): 対象・固定した条件・候補clone一覧。
- [summary.json](/Volumes/SanDisk1TB/Library/Caches/binder-narrowable-20260915/summary.json)、[aa-gates.json](/Volumes/SanDisk1TB/Library/Caches/binder-narrowable-20260915/aa-gates.json): 全数値と精度判定。
- [通常wall](/Volumes/SanDisk1TB/Library/Caches/binder-narrowable-20260915/analysis/wall-natural/primary.benchstat.txt)、[GC無効wall](/Volumes/SanDisk1TB/Library/Caches/binder-narrowable-20260915/analysis/wall-gc_off/primary.benchstat.txt)、[KPC](/Volumes/SanDisk1TB/Library/Caches/binder-narrowable-20260915/analysis/pmu-gc_off/primary.benchstat.txt): 主比較。各directoryに確認用とA/Aも保存。
- [候補patch](/Volumes/SanDisk1TB/Library/Caches/binder-narrowable-20260915/candidate.patch)、`sources/`、`bin/`、`raw/`、`selftest/`: 候補・固定source・binary・全raw・自己検証。
- [意味監査](/Volumes/SanDisk1TB/Library/Caches/binder-narrowable-20260915/audit-comparison/summary.json)、[package test](/Volumes/SanDisk1TB/Library/Caches/binder-narrowable-20260915/packages-after.log): 正確性の検証。

`prepare.py`、`build.sh`、`audit.sh`、`runner.py`、`collect.py` を保存した。新規実行では空のruntime/artifactを作り、絶対pathとmanifest/overlayを合わせる。今回のrawに追記しない。

最終の[採用判断更新](/Volumes/SanDisk1TB/Library/Caches/binder-narrowable-20260915/decision-update.json)を別ファイルで保存した。元のidentity・protocol・rawと当初の保留記録は改変せず保持している。
