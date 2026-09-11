# 寿命を揃えたBinderのPMU計測（2026-09-11）

現行Storeはpointerより命令数が多い。GC無効条件の主比較はcheckerでEL0命令数+50.64%、cycles+33.18%、domで+42.22%、+29.18%。この条件では両版・両入力の命令数とcyclesのA/Aがすべて合格した。L1D load missは両入力で約13%少なく、miss増加だけで遅さを説明する仮説は支持されない。追加命令をどの関数が生むかは、この総量計測だけでは分解できない。

## 対象・証拠の状態

- 選択repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、branch `codex/binder-accessor-checkpoint`、HEAD `32598cba146fa4dd7b6162b838630c90d865ab28`。未commitのAccessor等を含む現在ソースを固定。
- pointer: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、HEAD `8ac035a394c79e693a3a7d74cb170448503ee894`。tsgolint_git_revは両版null。
- 候補clone: flownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜7系、store-redesign等。今回未使用。完全な列挙はartifactの`worktrees.txt`。
- artifact set: `.cursor/skills/verify-tsc/artifacts/20260911-bind-batch-pmu/`。両版ともrepo_root／tsgolint_git_rev／typescript_go_git_rev一致により`current`。さらに全Goソース・使用binary・overlayの一致を終了時に確認（`verification-final.json`）。
- 旧PMUはidentityが一致していても旧ソース／旧寿命ハーネスの証拠であり、今回の前後比較には使わない。旧通常wallの`20260911-bind-batch-recheck-02`も別の計測条件。本稿の比率は今回の同一セッションから計算した。
- 初回準備時のplist属性コピー失敗は`20260911-bind-batch-pmu-setup-failed`に保存した。これは計測未実行（`missing`）であり、サンプル除外／再抽選ではない。今回の全実行stageには対象Benchmark行があり、`unsupported`はない。

## 方法と再現

Apple M1、Go 1.26.0、通常最適化、`binderinvestigation,kperf`。GOMAXPROCS=8、GOGC=100、GOMEMLIMIT=off。fixtureはchecker.tsとdom.generated.d.ts。両版とも10個のdistinct/unbound ASTを事前生成し、明示GC後にbatch全体をbind、終了後に解放。Store登録数の復帰・fake counterでの境界テストは両版成功。アルゴリズムやBinder本体は今回変更していない。

PMUを開いた後に作成するfresh pthread上で、batch直前／直後にcounterを読む。timer操作、parse、明示GC、登録解除はcounter区間外。`ns/op`は両counter読取を含む。固定counterはEL0+EL1、主指標はEL0だけを有効化したconfigurable counter。イベント定義と読戻し設定は`a14.plist`／`instructions.json`に保存。counterはbind threadの仕事のみを数え、別threadのGC worker等を合算しない。cyclesをCPU周波数で割ってprocess wallへ換算しない。

両版それぞれ2つの独立processで、3 fresh pthreads ×100短／長workload、GC境界、単調増加、命令数の規模比例、close後read失敗・同process reopen失敗を検証した。4 processすべて成功。

各GC条件でpointer_a/store_a/pointer_b/store_bを順序回転・反転し、6ラウンド×2入力×4ラベル=48行。通常GC、準備後のみGC無効の2条件、合計96行。主比較a/aと確認用b/bは別々にbenchstatで比較し、12独立標本として合算しない。A/Aのpaired bootstrap 95% CI全域が命令±1%、cycles/wall±1.5%内なら合格。ラウンド追加は行っていない。

再現ドライバは`tools/scripts/tsc/binder_batch_pmu.py`と`binder_batch_pmu_runner.py`。prepareでソースを固定して通常権限でbuild/validateし、作成された`/tmp/binder-batch-pmu-20260911/run.py`を管理者権限で実行、collectでbenchstat・A/A・source一致を集計する。再実行時は新規artifact/runtimeディレクトリへ変更し、既存結果へ追記しない。rawは各modeの`pointer_a.txt`等、順序は`order.jsonl`、比較は`*.benchstat.txt`、自己検証は`selftest/`。

## PMU結果

主比較の中央値。Mは百万/op。命令とcyclesは両GC条件・両入力でbenchstat p=.002（各n=6）。通常GCのdomはA/A精度未達なので、細かな倍率の採用を留保する。

| 条件 | 入力 | pointer命令 M | Store命令 M | 増減 | pointer cycles M | Store cycles M | 増減 |
|---|---|---:|---:|---:|---:|---:|---:|
| natural | checker.ts | 109.874 | 165.728 | +50.83% | 59.220 | 78.950 | +33.32% |
| natural | dom.generated.d.ts | 48.108 | 70.087 | +45.69% | 23.012 | 30.482 | +32.46% |
| gc_off | checker.ts | 109.976 | 165.673 | +50.64% | 59.382 | 79.084 | +33.18% |
| gc_off | dom.generated.d.ts | 48.143 | 68.468 | +42.22% | 22.982 | 29.687 | +29.18% |

GC無効条件の確認用b/bでも、checker命令+50.32%／cycles+33.01%、dom命令+42.72%／cycles+29.64%と同方向だった。

| GC無効・主比較 | 分岐数 | 分岐miss数 | L1D load miss数 | IPC pointer → Store |
|---|---:|---:|---:|---:|
| checker.ts | +47.43% | -5.40% | -13.26% | 1.852 → 2.095 |
| dom.generated.d.ts | +39.26% | -7.09% | -13.31% | 2.095 → 2.306 |

上の分岐・missの増減も主比較benchstat p=.002。ただしこれら専用のA/A受け入れ幅は事前定義していない。IPCは命令数とcyclesそれぞれの中央値の比による記述値で、独立の有意差検定ではない。L1D miss減少は総load数、依存load段数、latencyの減少を直接証明しない。

## A/A精度

| 条件・版・入力 | 命令95% CI (%) | cycles95% CI (%) | wall95% CI (%) |
|---|---|---|---|
| natural / pointer / checker.ts | -0.185〜+0.399 合格 | -0.929〜+0.528 合格 | -3.389〜+0.259 不合格 |
| natural / pointer / dom.generated.d.ts | -0.296〜+0.577 合格 | -0.590〜+0.589 合格 | -0.764〜+0.939 合格 |
| natural / store / checker.ts | -0.187〜+0.081 合格 | -0.454〜+0.965 合格 | -0.487〜+1.501 不合格 |
| natural / store / dom.generated.d.ts | -0.735〜+2.184 不合格 | -1.639〜+2.395 不合格 | -2.233〜-0.670 不合格 |
| gc_off / pointer / checker.ts | -0.370〜+0.200 合格 | -0.549〜+0.948 合格 | -0.909〜+1.212 合格 |
| gc_off / pointer / dom.generated.d.ts | -0.436〜+0.500 合格 | -0.699〜-0.059 合格 | -0.716〜+0.008 合格 |
| gc_off / store / checker.ts | -0.319〜+0.077 合格 | -0.760〜+0.332 合格 | -0.619〜+1.971 不合格 |
| gc_off / store / dom.generated.d.ts | -0.002〜+0.422 合格 | -0.373〜+0.280 合格 | -0.452〜+0.095 合格 |

GC無効の命令／cyclesは8/8合格。通常GCは6/8合格、Store/domの2指標が不合格。wallは通常GC1/4、GC無効3/4合格。Store/checker通常GC wall上限+1.500835%は丸めて合格にしない。

## ns/op・割当

下記はPMU有効・fresh pthread版の主比較中央値である。通常のwall benchmarkより絶対時間が大きく、PMU読取・計測有効化・threadの実行条件等の影響を分離できていない。差の全額をcounter読取2回の費用と断定しない。通常運用の絶対時間として引用せず、既存の非PMU結果と混合しない。

| 条件 | 入力 | 版 | ns/op | B/op | allocs/op |
|---|---|---|---:|---:|---:|
| natural | checker.ts | pointer | 30,874,479.5 | 7,423,951.0 | 13,950 |
| natural | checker.ts | Store | 41,493,162.5 | 12,798,226.0 | 14,163 |
| natural | dom.generated.d.ts | pointer | 12,392,629.0 | 5,286,010.0 | 16,558 |
| natural | dom.generated.d.ts | Store | 17,162,237.5 | 7,867,737.5 | 16,683 |
| gc_off | checker.ts | pointer | 30,953,583.0 | 7,423,951.0 | 13,950 |
| gc_off | checker.ts | Store | 41,780,904.0 | 12,798,226.0 | 14,163 |
| gc_off | dom.generated.d.ts | pointer | 12,423,771.0 | 5,286,010.0 | 16,558 |
| gc_off | dom.generated.d.ts | Store | 16,098,819.0 | 7,865,496.0 | 16,682 |

## 診断と次の限定対照

今回のPMUは全batch総量であり、hot pathの新しい関数別帰属は行っていない。既存Instrumentsでは広いhot pathはwalk/helpersで、Locals/NextContainerのmap、FlagsAt、list/modifier、Flow ID変換が候補。狭いslotアクセサだけが全差分を作るとは言えない。割当driversは既存分析のsymbolIdx/flows列、symbolRefs、FlowNode/Symbol/Handleの拡大であり、今回もB/op増（checker約72%、dom約49%）が残る。

GCを無効にしても命令+42〜51%、cycles+29〜33%が残るため、差をGCだけでは説明できない。一方、miss数減少とIPC上昇はStoreの配置の利点と整合するが、noscanによる全体GC改善の証明ではない。主な仮説は、再解決・所有者確認・意味情報map変換等が追加命令を生み、その費用が残ること。個々の原因を確定するには限定した実装対照が必要。

次は詳細設計書の順序どおり、Locals/NextContainerの表現変更と、pointerと同じModifierFlags事前保持を独立に比較する。今回の固定binaryを対照として保持し、2pass・bind順を変えず、意味digest一致、命令/cycles A/A、通常wall、parse+bind、割当/GCの評価を行う。PMUの関数別削減量やCLI全体高速化は現時点でmissing。
