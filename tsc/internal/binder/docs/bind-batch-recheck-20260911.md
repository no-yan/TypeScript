# 負荷低下後のbind再測定（2026-09-11）

## 結論

現在のコードでも、この単一ファイルbind条件ではStoreが約1.3倍遅いという方向が再現した。通常GCの主比較はchecker +31.39%、dom +34.85%。確認用比較も+33.78%、+34.70%で、全てbenchstat p=.002。GC無効でも差が残る。ただしA/Aの±1.5%精度gateは全体として不合格であり、精密な倍率の採用条件は満たしていない。追加roundは行わなかった。

## 測定条件・検証

前回artifactを照合し、pointerは同一、Storeはbinder.goの変更を検出。両版を最新ソースから新規ビルドした。計測中・終了時とも全Goソース、binary、overlay SHAの一致を確認した。今回Binder本体やハーネスは変更していない。

修正済み共通ハーネスを使用。独立した未bind ASTを10個事前parseし、一回の事前GCの後でbatch全体のbindだけを計時する。両版とも全ASTを計測終了まで保持し、終了後に参照を解除する。Store登録解除は計時外。両版の寿命試験が成功し、Store登録数は各batch後にbaselineへ戻った。フル意味digestは今回再取得していない。

各入力10 binds × 6固定round。pointer_a/store_a/pointer_b/store_bを回転・反転した順序で実行。a対aが主比較、b対bが確認用。同一binaryのa対bがA/A。二組を12独立サンプルとして合算しない。通常GCとGC無効で合計96 Benchmark行。Go 1.26.0、darwin/arm64 Apple M1、GOMAXPROCS=8、GOGC=100、GOMEMLIMIT=off。GC無効は事前準備後に自動GCを止める補助条件。

## 主比較の中央値

| 条件・入力 | pointer ns/op | Store ns/op | 時間差 | pointer B/op | Store B/op | pointer allocs/op | Store allocs/op |
|---|---:|---:|---:|---:|---:|---:|---:|
| 通常GC checker.ts | 11,761,141.5 | 15,453,223.0 | +31.39% | 7,424,013 | 12,798,288 | 13,950 | 14,163 |
| 通常GC dom.generated.d.ts | 4,377,387.5 | 5,903,114.5 | +34.85% | 5,285,994 | 7,866,128 | 16,558 | 16,682 |
| GC無効 checker.ts | 11,656,190.0 | 15,586,102.5 | +33.72% | 7,424,013 | 12,798,288 | 13,950 | 14,163 |
| GC無効 dom.generated.d.ts | 4,415,743.5 | 5,710,071.0 | +29.31% | 5,285,994 | 7,869,173 | 16,558 | 16,682 |

全ての主比較および確認用比較はbenchstat p=.002。GC無効の確認用時間差はchecker +31.04%、dom +29.71%。条件間は別時間帯のため、通常GCとGC無効の絶対時間差をGC改善量とは解釈しない。

## A/A精度

paired log-ratio bootstrap 95%区間全体が±1.5%に収まることを要求する。

| 条件 | 版 | 入力 | A/A 95%区間 | gate |
|---|---|---|---:|---|
| 通常GC | pointer | checker.ts | -3.08〜+0.51% | 不合格 |
| 通常GC | pointer | dom.generated.d.ts | +0.35〜+1.19% | 合格 |
| 通常GC | store | checker.ts | -0.10〜+1.94% | 不合格 |
| 通常GC | store | dom.generated.d.ts | -0.95〜+2.02% | 不合格 |
| GC無効 | pointer | checker.ts | +0.37〜+3.63% | 不合格 |
| GC無効 | pointer | dom.generated.d.ts | -2.63〜+1.49% | 不合格 |
| GC無効 | store | checker.ts | -2.17〜+0.35% | 不合格 |
| GC無効 | store | dom.generated.d.ts | -0.23〜+1.45% | 合格 |

開始時load averageは1.74/4.84/6.55。前回開始時9.31/5.50/3.95から1分値は低下した。プロセス一覧はsandbox制約で取得できず、pprofの停止自体は独立確認していない。温度・電力状態も取得不可。今回pprofは起動していない。負荷低下と同時にソースも変わっているため、前回との差をpprof停止の効果とは定量化しない。

## 対象・証拠

- 選択Store repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、HEAD `32598cba146fa4dd7b6162b838630c90d865ab28`。
- pointer repo: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、HEAD `8ac035a394c79e693a3a7d74cb170448503ee894`。
- tsgolint_git_revは両版null。repo_root・両revision一致によりartifactは`current`。今回はdirtyソースもsnapshotと一致。
- 候補clone: flownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr系、store-redesign等。今回未使用。完全一覧はartifactのworktrees.txt。
- artifact set: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/.cursor/skills/verify-tsc/artifacts/20260911-bind-batch-recheck-02`。raw、benchstat、summary、A/A、identity、lifetime、host、固定ソースとドライバを保存。
- 前回bind-batch-lifetimeはidentity上`current`だが現行binder.goとは不一致。古い異なるidentityの結果は`stale`。今回は対象行なしの`unsupported` stageなし。現行KPC・profile・実プロジェクト逐次/並列比較・A/A全面合格の結果は`missing`。

## 診断・次の行動

寿命条件を揃え、GCを無効にしても約29〜34%の差が観測されるため、登録解除漏れや計測中GCだけで差全体を説明する仮説は支持されない。主比較・確認比較とも同方向だが、規定の精度gate未達のため確定倍率や最終採用根拠にはしない。これはprepared batch内の単一ファイルbindであり、CLIの並列project Bind timeとは範囲が異なる。

既知の広いhot pathはwalk/helpersで、slot解決はその一部。既知のallocation driverはsymbolIdx/flows列、symbolRefs、FlowNode/Symbol/Handle大型化。これらは過去profile由来で今回再確認していない。B/opはchecker +72.39%、dom +48.81%残る。

次は同一実プロジェクトの逐次/並列parse+bind条件を揃えてCLIとの違いを切り分ける。Accessorの効果を評価するなら同じ寿命ハーネスで変更前後を固定して比較する。追加の計測は今回実行していない。
