# Binder 基盤移行の前後比較（2026-09-15）

## 結論

移行後に実行量の退行を確認した。KPC の EL0 命令数は checker.ts **+23.31%**、dom.generated.d.ts **+20.90%**（ともに benchstat p=.002）。命令数の A/A は全4条件で ±1% の精度基準に合格し、確認用比較でも +23.62% / +21.04% と再現した。

checker の割り当ては **14,163 → 24,065 allocs/op（+69.91%）**、**12,798,208 → 12,994,156 B/op（+1.53%）**。別の割り当て診断で `hasNarrowableArgument → Handle.Arguments → NodeSeq.Slice` に **9,900 allocs/op** を確認し、実測増分9,902回のほぼ全部を説明する。dom の割り当てはほぼ同じ。

時間も増加方向だが、通常計測の A/A は0/8条件合格、KPC cycles は2/4条件合格に留まる。以下の時間・cycles の増加率は観測値として残し、厳密な退行倍率としての採用は保留する。命令数と割り当ての増加は支持される。

## 対象と artifact 状態

- 選択 repo: `/Volumes/SanDisk1TB/worktree/binder-rewrite`。
- 前: Store Binder HEAD `85506e8b7d51f2b05f84f9ba1a8c3309b2f30ce4`。後: 同 HEAD 上の未commit基盤移行コード。
- 後の固定ビルド用コピー: `/Volumes/SanDisk1TB/Library/Caches/binder-foundation-20260915-164851/candidate-repo`、commit `d7505ce59277cb0117f900e46580d12a58d7810e`。選択作業木とコピーの Go/module 関連5127ファイルの hash 一致を開始時・終了時に確認。
- 前後は同じコピーに `binder.go` / `bindwalk_generated.go` の固定 overlay を適用。前は `git show HEAD` の内容、後は作業木と一致。共通の test support として repo-root 環境変数対応、および移行専用テストの除外を両版へ同じように適用。計測対象の実装差は上記2ファイル。
- `tsgolint_git_rev=null`。TSGolint は今回のビルド・実行対象に含めない。`typescript_go_git_rev` は選択 HEAD を記録し、after の dirty 内容を全ソースhashで固定した。
- 候補 clone: 同 HEAD の `cursor-ast-store-tests`、pointer 側 main `8ac035a394`、flownode、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜7系、store-redesign、store-schema-foreach-child 等。比較用コピー以外は実行に使用していない。全候補・revision・path は artifact の `worktrees.txt`。

| Artifact | 状態 | 用途 |
|---|---|---|
| 今回の wall / KPC | `current` | repo_root・両revision欄・dirty hash・binary・overlay・fixture一致を確認した前後比較 |
| `/private/tmp/binder-kpc-fastpath-20260915` | `stale` | repo_rootが異なる。数値は今回の性能証拠に使わず、host DBと再照合したイベント定義だけ再利用 |
| 2026-09-11 の raw campaign | `missing` | 当時の文書は残るが今回への性能証拠は `stale` |
| 今回の Instruments CPU attribution | `missing` | 関数別の実行時間配分は未測定 |

今回の対象 regex は全 campaign で2入力の `Benchmark...` 行を取得した。`unsupported` に該当する stage はない。

比較範囲には署名、走査、helper 境界、upstream へ合わせた意味修正も含まれる。`bind(ref, kind)` の個別効果はこの比較から分離できず、引き続き別の実装候補とする。

## 条件・検証

Apple M1 / 8GB / 8 logical CPUs、macOS 26.6.2 (25G83)、Go 1.26.0 darwin/arm64、CGO有効。`GOMAXPROCS=8`, `GOGC=100`, `GOMEMLIMIT=off`。

- checker.ts / dom.generated.d.ts は選択 checkout の fixture と SHA256 一致。parse は区間外。
- 寿命修正済み `BenchmarkBindInvestigation` / `BenchmarkBindInvestigationKPC`。10個のdistinct/unbound ASTを事前準備し、1回GC、10 bindsを一括計測、consumer確認後に登録解除。GoのN=1校正も独立ASTを使い解放する。
- 前後とも BatchLifetime / KPCBatch テスト成功。Store登録数が開始値に戻ることを確認。
- 通常wallは通常GCと測定batchだけ自動GC無効の2条件、計96行。KPCはbatch GC無効で48行。合計144行。各条件6固定round、各roundで before_a/after_a/before_b/after_b を回転・反転して実行。最初の4roundは4通りのrotation、残り2roundは元の順序と逆順。各sampleはfresh process。ベンチマークとビルドの同時実行なし。
- a/aが主比較、b/bが確認用。12標本へ合算せず、全rawを保存してbenchstatで比較。A/Aは同roundの対数比を20,000回paired bootstrapした95% CI全体が閾値内なら合格（wall/cycles ±1.5%、命令 ±1%）。計測後の追加roundなし。
- KPCは管理者認証後、前後各2 fresh processで実counter自己検証成功。各processで3 fresh pthread × 100 short/long pairsとGC境界を検証。イベント設定後に作ったfresh pthread上でbindを囲む2回のcounter readを実行し、減少値はエラーにする。
- EL0のみの命令・cycles・分岐・分岐ミス・L1D load missを使用。固定counterはEL0+EL1として別記録。host `/usr/share/kpep/a14.plist` と設定のSHA・event番号・counter配置が一致し、実行時readbackも一致。別threadの処理は合算しない。
- KPCのns/opはcounter readを含み、通常wallとは別条件。割り当て診断は別バイナリ・別実行であり、wall/KPCの標本に含めない。pprofは使用していない。

## 時間・割り当て

主比較の中央値。時間の精度gate未達のため、増減は観測値。

| 条件 | 入力 | 前 ns/op | 後 ns/op | 増減 | benchstat p |
|---|---|---:|---:|---:|---:|
| wall-natural | checker.ts | 55,624,768.5 | 68,874,871 | +23.82% | .002 |
| wall-natural | dom.generated.d.ts | 20,312,250 | 24,569,241.5 | +20.96% | .065 |
| wall-gc_off | checker.ts | 58,014,808.5 | 69,139,079 | +19.17% | .093 |
| wall-gc_off | dom.generated.d.ts | 20,516,423 | 23,279,623 | +13.47% | .015 |

確認用b/bでは通常GC checker +19.02% / dom +6.21%、GC無効 +16.49% / +9.24%。通常GC dom の主比較は非有意、GC無効 checker は主・確認とも非有意。wall A/Aは全8条件不合格であり、ばらつきの原因の特定は未完了。

| 条件 | 入力 | 前 B/op | 後 B/op | 前 allocs/op | 後 allocs/op |
|---|---|---:|---:|---:|---:|
| wall-natural | checker.ts | 12,798,208 | 12,994,156 | 14,163 | 24,065 |
| wall-natural | dom.generated.d.ts | 7,867,448.5 | 7,867,441.5 | 16,682 | 16,682 |
| wall-gc_off | checker.ts | 12,798,208 | 12,994,156 | 14,163 | 24,065 |
| wall-gc_off | dom.generated.d.ts | 7,867,326.5 | 7,865,479 | 16,681.5 | 16,681 |

checkerのbytes/allocs増加は通常GC、GC無効、KPC、確認用比較のすべてで再現（p=.002）。domの割り当て差は非有意。

## KPC

| 入力 | 前 EL0 inst/op | 後 EL0 inst/op | 増減 | 前 EL0 cycles/op | 後 EL0 cycles/op | 増減 |
|---|---:|---:|---:|---:|---:|---:|
| checker.ts | 165,866,676.5 | 204,537,574.5 | +23.31% | 83,491,684.5 | 97,561,896 | +16.85% |
| dom.generated.d.ts | 69,097,370.5 | 83,537,787.5 | +20.90% | 31,193,860 | 35,216,188 | +12.89% |

上表の主比較はすべてp=.002。確認用b/bの命令数は checker +23.62% / dom +21.04%、cyclesは +18.61% / +14.12%。命令数の精度は合格。cyclesはafterの両入力で精度基準未達のため、厳密な効果量は保留。

| A/A | 命令95% CI (%) | cycles95% CI (%) |
|---|---|---|
| before / checker.ts | -0.380〜+0.062 合格 | -1.300〜+1.014 合格 |
| after / checker.ts | -0.230〜+0.206 合格 | +0.160〜+2.602 不合格 |
| before / dom.generated.d.ts | -0.389〜+0.101 合格 | -1.018〜+1.160 合格 |
| after / dom.generated.d.ts | -0.145〜+0.176 合格 | -2.017〜+1.961 不合格 |

| 入力 | 指標 | 前 /op | 後 /op | 増減 |
|---|---|---:|---:|---:|
| checker.ts | el0-branch/op | 42,246,339.5 | 54,167,581.5 | +28.22% |
| checker.ts | el0-branch-miss/op | 629,128.5 | 629,225 | +0.02% |
| checker.ts | el0-l1d-miss/op | 790,892.5 | 862,694 | +9.08% |
| dom.generated.d.ts | el0-branch/op | 16,454,870 | 21,586,122 | +31.18% |
| dom.generated.d.ts | el0-branch-miss/op | 143,590.5 | 149,263.5 | +3.95% |
| dom.generated.d.ts | el0-l1d-miss/op | 324,914 | 344,945 | +6.17% |

checkerの分岐ミスは非有意（p=.937）、他はp=.002。これらの補助イベントには独立した精度gateを事前設定していない。固定counter、KPC下ns/op・B/op・allocs/opを含む全数値は `summary.json` と各benchstatに保存。

## Hot path・割り当て原因

### 今回確認した割り当て経路

`hasNarrowableArgument`（binder.go:2714）が `expr.Arguments()` を呼ぶ。生成accessorは `ArgumentsSeq().Slice()` を返し、`NodeSeq.Slice` が `[]Handle` を確保する。変更前のRef経路はこのスライス化を行っていない。

通常の計測とは別に、`runtime.MemProfileRate=1`、parse後／bind後の累積allocation stack差分を観測した。checkerで前 **0**、後 **9,900 objects/Bind** が `NodeSeq.Slice → Handle.Arguments → hasNarrowableArgument` に帰属した。通常計測の増分9,902 allocs/opのほぼ全部に相当する。

診断は同じPC stackのサイズ別bucketを加算する。初版にはbucket上書きの集計欠陥があり、その診断ログは `superseded-diagnostics` に隔離して不採用にした。修正後の値だけを本文で使用。wall/KPCハーネスはこの診断コードを使わず、測定結果への影響はない。

診断でのSlice bytesは216,640 B/Bind。rate=1やtiny allocationの扱いが通常計測と異なるため、この値を通常のB/op増分195,948へ代入・単純一致させない。診断自身のallocationも全stack差分に含まれるため、全JSONの総和をBindの割り当て量とは扱わない。残り2 allocs/opの帰属は未確定。

従来の大きな割り当て元であるFlow chunk、列拡張、Symbol/Locals表も診断で確認した。今回はこれらのデータ表現を変更しておらず、slice materializationの新規増分を優先して対処できる。

### CPUの帰属と限界

以前のInstruments分析で広いhot pathはwalk/helpers、狭い経路はLocals/NextContainerのmap操作、FlagsAt、list参照だった。ただしその記録は今回に対して `stale` であり、構成比は引用しない。今回のKPCは関数別のサンプリングではなくbind区間の総量を示す。現在のCPU時間配分は `missing`。

命令数・分岐数の増加と、割り当てが変わらないdomでも命令数が増えたことから、次にwalk/helper境界の実行量を切り分ける。`NodeSeq.Slice` だけに全命令増加を帰属する根拠はない。

## 次の実装と受け入れ条件

1. 最小候補: `hasNarrowableArgument` の `expr.Arguments()` を `expr.ArgumentsSeq().All()` 等の直接走査へ変更する。読み取り専用でslice所有・変更を必要としないため、引数順を保ったままmaterializationを除ける。今回この修正は実装していない。
2. この候補だけを固定した前後比較で、checkerの約9,900 allocs/opの解消を確認。通常wallとKPCを再比較し、改善量を推測で採用しない。
3. 別候補として `bind(node ast.NodeRef, kind ast.Kind)` の伝搬、取得済みkindの再利用、Handle/query境界を一つずつ比較する。現結果から個々の効果を推定しない。広いwalk/helpersと狭いslice経路を分けて扱う。
4. 保存先・訪問順・Flow/Symbolの意味監査、ast/binder/checker/compilerテスト、conformance無退行を維持する。今回の計測でproduction codeを修正していないため、前回の正確性検証を置き換えるものではない。
5. wall/cyclesの厳密な倍率が必要なら、負荷条件を確認した別の固定計画でA/A精度を確保する。今回の標本への追加は行わない。

全体CLI、並列bind、TSGolintプロジェクト全体の性能は今回の測定範囲外。symbol synthetic benchmarkは実施していない。

## 保存物・再現

Artifact set: [/Volumes/SanDisk1TB/Library/Caches/binder-foundation-20260915-164851/performance](/Volumes/SanDisk1TB/Library/Caches/binder-foundation-20260915-164851/performance)。実行時rootは `/private/tmp/binder-foundation-perf-20260915`、保存先にはraw、固定source、overlay、binary、fixture、event DB、実行順序とhash、collector、自己検証ログを含む。

- [identity.json](/Volumes/SanDisk1TB/Library/Caches/binder-foundation-20260915-164851/performance/identity.json): 選択repo/revision、snapshot全Go hash、前後overlay・binaryの一致、hostとevent DB。
- [worktrees.txt](/Volumes/SanDisk1TB/Library/Caches/binder-foundation-20260915-164851/performance/worktrees.txt): 候補cloneの全一覧。
- [summary.json](/Volumes/SanDisk1TB/Library/Caches/binder-foundation-20260915-164851/performance/summary.json)、[aa-gates.json](/Volumes/SanDisk1TB/Library/Caches/binder-foundation-20260915-164851/performance/aa-gates.json): 全指標と精度判定。
- [通常wallのbenchstat](/Volumes/SanDisk1TB/Library/Caches/binder-foundation-20260915-164851/performance/analysis/wall-natural/primary.benchstat.txt)、[GC無効wall](/Volumes/SanDisk1TB/Library/Caches/binder-foundation-20260915-164851/performance/analysis/wall-gc_off/primary.benchstat.txt)、[KPC](/Volumes/SanDisk1TB/Library/Caches/binder-foundation-20260915-164851/performance/analysis/pmu-gc_off/primary.benchstat.txt)。各directoryに確認用・A/A比較も保存。
- [allocation-summary.json](/Volumes/SanDisk1TB/Library/Caches/binder-foundation-20260915-164851/performance/allocation-summary.json)、[DIAGNOSTICS.md](/Volumes/SanDisk1TB/Library/Caches/binder-foundation-20260915-164851/performance/DIAGNOSTICS.md): 別実行の割り当て帰属と制限。

`build.sh` が同一の固定ソースに前後overlayを適用する。`runner.py wall` / `runner.py pmu` が固定6roundを収集し、`collect.py` がbenchstatとA/Aを生成する。KPCは`run-pmu.sh`をmacOSの管理者認証経由で実行した。新しい比較では別のruntime/artifactディレクトリを用意し、manifestとoverlayの絶対pathを更新し、旧rawへ追記しない。通常計測とKPCとbuildは直列で実行する。
