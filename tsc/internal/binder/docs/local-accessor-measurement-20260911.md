# アクセサ局所化の前後再計測（2026-09-11）

GC無効wallではA/Aが全4条件合格し、checker +0.30%、dom −0.84%でbenchstat有意差なし。確認用比較も有意差なし。GC無効PMUの命令数もchecker +0.14%、dom −0.02%で主比較は非有意、命令A/Aは全4条件合格。今回の観測では局所化による明確なwall退行・命令数増加は確認されていない。ただし有意差なしは同等性の証明ではない。通常GCのdomの主比較だけは−2.44%（p=.026）だが、A/A不合格と確認用比較の非再現により改善として採用しない。

## 対象・方法

- 選択repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`。前: commit `e38ef0b8093f9af5749ce5a5618233d36c78fbc8`のbinder.go、後: 同HEADのdirty局所化後binder.go。tsgolint_git_revはnull。変更差はbinder.goだけと固定overlayのSHAで確認した。
- 候補clone: flownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜7系、store-redesign等。今回すべて未使用。pointer対照も今回再計測していない。完全な一覧はartifactのworktrees.txt。
- artifact set: `.cursor/skills/verify-tsc/artifacts/20260911-local-accessors-measurement/`。wall2条件は`current`、live Goソース・binary・overlayの終了時一致を確認。前版は意図的なHEAD overlayであり、dirty afterと一致するとは扱わない。
- 旧`20260911-bind-batch-pmu`は選択HEADが変わったため`stale`。今回の局所化後の性能証拠には使用せず、同一sessionでbefore/afterを再build・再実行した。直前の20入力意味監査は`current`、意味digestと監査対象Accessor回数の一致を再確認した。
- Apple M1、Go 1.26.0、GOMAXPROCS=8、GOGC=100、GOMEMLIMIT=off。前後のnormal/PMU binaryを新規buildし、前後両版のBatchLifetime/KPCBatch試験成功。normal wallはPMUなしのbinary。10個のdistinct ASTを準備して1回GCし、10 bindsのbatchだけを計測、その後に登録解除する。GC無効条件は準備後のみ自動GCを無効化。
- checker.ts/dom.generated.d.ts、各6ラウンド、before_a/after_a/before_b/after_bの順序を回転・反転。通常GCとGC無効で計96行。a/aを主比較、b/bを確認用とし、12標本に合算しない。A/Aはpaired bootstrap95% CI全域が±1.5%内で合格。固定ラウンド終了後の追加測定は行わない。
- バックグラウンドの索引処理が見られた。ベンチマーク同士やbuildとの同時実行は避けた。初回wall起動は既存のroot所有lockを追記openできず、benchmark開始前に停止。read-only openで同じ排他lockを取得するよう修正してから実行した。この起動失敗をサンプルとして選別していない。

## Wall結果

中央値。増減は変更後/変更前。

| 条件 | 入力 | 前 ns/op | 後 ns/op | 増減 | benchstat p |
|---|---|---:|---:|---:|---:|
| wall-natural | checker.ts | 14,633,621.0 | 14,683,925.0 | +0.34% | .240 |
| wall-natural | dom.generated.d.ts | 5,748,552.0 | 5,608,325.0 | -2.44% | .026 |
| wall-gc_off | checker.ts | 14,657,262.5 | 14,701,816.5 | +0.30% | .818 |
| wall-gc_off | dom.generated.d.ts | 5,404,356.0 | 5,358,842.0 | -0.84% | .093 |

確認用b/bは通常GC checker +0.71%（p=.132）、dom −0.42%（p=.310）。GC無効checker −0.21%、dom +0.12%（各p=.589）。すべて非有意。

| 条件 | 入力 | 前 B/op | 後 B/op | 前 allocs/op | 後 allocs/op |
|---|---|---:|---:|---:|---:|
| wall-natural | checker.ts | 12,798,208.0 | 12,798,209.0 | 14,163.0 | 14,163.0 |
| wall-natural | dom.generated.d.ts | 7,866,128.0 | 7,864,554.0 | 16,682.0 | 16,682.0 |
| wall-gc_off | checker.ts | 12,798,208.0 | 12,798,208.0 | 14,163.0 | 14,163.0 |
| wall-gc_off | dom.generated.d.ts | 7,867,326.5 | 7,867,326.5 | 16,681.5 | 16,681.5 |

## A/A精度

| 条件 | 版 | 入力 | wallのpaired95% CI (%) | 判定 |
|---|---|---|---:|---|
| wall-natural | before | checker.ts | -3.503〜-0.033 | 不合格 |
| wall-natural | before | dom.generated.d.ts | -2.691〜+0.263 | 不合格 |
| wall-natural | after | checker.ts | -3.345〜+0.369 | 不合格 |
| wall-natural | after | dom.generated.d.ts | -1.185〜+0.401 | 合格 |
| wall-gc_off | before | checker.ts | -0.633〜+1.106 | 合格 |
| wall-gc_off | before | dom.generated.d.ts | -1.289〜+0.121 | 合格 |
| wall-gc_off | after | checker.ts | -0.470〜+0.287 | 合格 |
| wall-gc_off | after | dom.generated.d.ts | +0.129〜+1.305 | 合格 |

## PMU結果と残る検証

管理者認証後、前後各2 processで実counterの自己検証に成功し、GC無効条件で固定48行を取得した。総取得行数はwall96+PMU48=144。PMU artifactも`current`、liveソース・binary・overlay一致を確認済み。追加ラウンドは行っていない。

PMUはevent設定後に作ったfresh pthread上のbind batchを計数する。timer操作・parse・明示GC・解放はcounter区間外。ns/opにはcounter読取を含み、通常wallとは別の指標。別threadの処理は合算しない。PMUと集計は同じ排他lockで直列化した。

| 入力 | 前 EL0命令/op | 後 EL0命令/op | 増減 | p | 前 EL0 cycles/op | 後 EL0 cycles/op | 増減 | p |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| checker.ts | 165,482,760.5 | 165,711,488.0 | +0.14% | .180 | 83,961,662.0 | 81,405,276.0 | -3.04% | .240 |
| dom.generated.d.ts | 68,557,659.5 | 68,542,478.5 | -0.02% | .818 | 30,248,898.0 | 30,189,124.0 | -0.20% | .485 |

命令数のA/Aは±1%基準で全4条件合格。一方、cyclesおよびPMU下wallのA/Aは±1.5%基準で全4条件不合格。したがってchecker cycles −3.04%を改善とは採用しない。主比較の命令数は両入力ともbenchstat非有意で、確認用b/bの増減はchecker −0.27%、dom +0.34%と方向も一致しない。0.x%の方向を安定した効果として主張せず、命令数は概ね横ばいと評価する。

| PMU A/A・版・入力 | 命令95% CI (%) | cycles95% CI (%) |
|---|---|---|
| before / checker.ts | +0.003〜+0.308 合格 | -0.787〜+3.625 不合格 |
| before / dom.generated.d.ts | -0.236〜-0.045 合格 | -3.644〜+1.713 不合格 |
| after / checker.ts | -0.341〜-0.112 合格 | -3.617〜+4.827 不合格 |
| after / dom.generated.d.ts | -0.005〜+0.468 合格 | +0.624〜+2.343 不合格 |

| PMU主比較 | 前 ns/op | 後 ns/op | 前 B/op | 後 B/op | 前 allocs/op | 後 allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| checker.ts | 44,814,039.5 | 43,781,729.0 | 12,798,227.0 | 12,798,226.0 | 14,163.0 | 14,163.0 |
| dom.generated.d.ts | 16,484,914.5 | 16,560,452.0 | 7,867,342.5 | 7,867,342.5 | 16,682.0 | 16,682.0 |

既存CPU分析の広いhot pathはwalk/helpers。割当driverはsymbolIdx/flows、symbolRefs、FlowNode/Symbol/Handleの拡大。今回はそれらの表現を変えておらず、割当量も概ね同じ。診断は「レビューのための局所化で明確なwall退行・命令数増加は観測されない。ただし通常GC wallとcyclesの精度不足が残る」。次はこの読みやすい実装を候補としてレビューし、cyclesの厳密な同等性が採用条件なら、負荷を整えた別の固定計画で検証する。全体CLIやnoscanのGC改善は今回の測定範囲外。

再現ドライバ: `tools/scripts/tsc/binder_local_accessors_measurement.py`（prepare/collect）、`binder_local_accessors_runner.py`（wall/pmu）。新規実行には別のartifact/runtimeディレクトリを指定する。rawは各campaignのbefore_a.txt等、順序はorder.jsonl、benchstatは各比較名の.benchstat.txt、A/Aはaa-gates.json。旧rawへの追記や追加ラウンドは行わない。
