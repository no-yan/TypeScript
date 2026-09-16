# Binder KPC 計測の共通化

2026-09-15。状態: **設計文書。共通CLIは未実装**。

## 1. 目的と範囲

KPCによる前後比較の準備を、毎回の探索・スクリプト作成から、固定した手順の実行へ移す。実装本体の最適化は含めない。

共通化すると短縮できるのは、過去の設定の探索、比較用コピーの作成、管理者実行の準備、rawの解析、精度判定、結果文書の組み立てである。ビルド、管理者認証、実カウンタの自己検証、固定回数の計測は引き続き必要になる。短縮時間の実測値はまだない。

まず同じStore表現でのBinder前後比較に限定する。pointer版との比較、TSGolint全体、別CPUのイベント対応、InstrumentsによるCPU帰属は追加の対応範囲とする。

## 2. 今回時間がかかった理由

| 問題 | 共通化で行うこと |
|---|---|
| 古い文書が参照するdriverがリポジトリになく、一時ディレクトリやcacheを探索した | driver・設定・利用手順をGitで管理する |
| 日付・絶対path・入力数がスクリプトに埋め込まれていた | 引数とplanに集約し、runごとに生成する |
| KPC設定が現在のhostで使えるか毎回読み直した | 保存済みイベント定義とhost DBを機械照合し、実行時readbackも検証する |
| 外付けvolume上のファイルを管理者processが読めないことがある | 内蔵ディスク上のruntime一式を自動作成し、元とのhash一致を確認する |
| rootが生成したrawと通常権限の集計出力の保存先が混ざった | rawを読み取り専用の入力とし、通常権限のanalysisへ集計する |
| A/Aやbenchstatの集計を毎回作り直した | 同じcollectorと回帰テストを使う |
| 全Goファイルに対して個別にGitを起動すると準備が遅い | Gitの差分一覧・一括取得と内容hashを使い、ファイルごとのprocess起動を避ける |

2026-09-15の実行結果と制限は[基盤移行の性能比較](foundation-migration-performance-20260915.md)を参照する。過去の性能値を新しいcheckoutの測定結果として再利用しない。

## 3. 再利用する実装と追加するファイル

### 既存のGoハーネス

| ファイル | 役割 |
|---|---|
| [bind_bench_test.go](../bind_bench_test.go) | fixtureのhash検証、最大10個の独立AST、wall計測 |
| [bind_bench_store_test.go](../bind_bench_store_test.go) | Store登録数の取得と登録解除 |
| [bind_bench_lifetime_test.go](../bind_bench_lifetime_test.go) | 解放、登録数復帰、再利用による早期returnを検査 |
| [bind_investigation_kperf_test.go](../bind_investigation_kperf_test.go) | 実counter自己検証とbind区間のKPC計測 |
| [bind_investigation_kperf_batch_test.go](../bind_investigation_kperf_batch_test.go) | read境界・失敗・counter減少時の検査 |
| [investigation_darwin.go](../../testutil/kperf/investigation_darwin.go) | event設定、readback、fresh pthread、counter読取・解放 |

このハーネスを共通driverから呼ぶ。古い `BenchmarkBindKPC` の反復ごとのGC・寿命管理をそのまま前後比較へ使わない。

### 追加する構成（未実装）

```text
tools/scripts/tsc/
  binder_measurement.py             # inspect / prepare / wall / kpc / collect
  binder_measurement_test.py        # identity、順序、解析、精度判定の検査
  binder_measurement/
    m1-el0.json                     # event名・番号・配置・host DBの識別情報
    README.md                       # 最短の利用手順と本設計へのリンク
```

既存の実行用素材は以下に保存されている。

`/Volumes/SanDisk1TB/Library/Caches/binder-foundation-20260915-164851/performance/`

`build.sh`、`runner.py`、`collect.py`、`run-pmu.sh`、`instructions.json` が移植元になる。これらは日付とローカルpathを含むため、そのまま汎用CLIとして登録しない。移植・検証後はリポジトリ内の実装を正とし、cacheの存在を実行条件にしない。

## 4. 利用の流れ

以下は**実装予定のCLI仕様**であり、現時点で実行できるコマンドではない。

1. `inspect --repo REPO --artifacts DIR --bench REGEX`: 候補checkout、既存artifact、利用可能なGo/benchstat/hostを確認する。計測は起動しない。
2. `prepare --repo REPO --before REF --after REF_OR_WORKING_TREE --out NEW_DIR`: source、fixture、planを固定し、通常権限でwall/PMU binaryを作成する。
3. `wall --run DIR`: 固定planに従って通常wallを計測する。
4. `kpc --run DIR`: runtimeのhashを検証し、macOS認証経由で自己検証とKPC計測を実行する。
5. `collect --run DIR`: rawを検査してbenchstat、A/A、レポートを生成する。計測processを起動しない。

`REF` は最初にcommitへ解決して記録する。`working-tree` は明示指定とし、trackedの変更とbuildに使うuntracked sourceを含めてコピーする。dirty状態をHEADそのものとして記録しない。

`prepare` は両版を独立した固定コピーとして扱うことを基本とする。同じ周辺ソースに変更ファイルだけoverlayする方法は、差分一覧を検証し、共有部分が一致すると確認できた場合に使う。比較版でテストがコンパイルできない場合の除外・adapterは、両版の差と理由を明記し、黙って適用しない。

### 固定planの初期値

| 項目 | 初期値 |
|---|---|
| 入力 | checker.ts / dom.generated.d.ts、内容hashを固定 |
| batch | 10個のdistinct/unbound AST。GoのN=1校正も解放する |
| wall | 通常GC、batch区間だけ自動GC無効の2条件 |
| KPC | batch区間だけ自動GC無効 |
| round | 6回。各回before_a / after_a / before_b / after_b |
| 実行順序 | 最初の4roundは4通りのrotation、最後2roundは逆順と元の順序 |
| 主比較・確認 | a/aが主比較、b/bが確認。12標本に合算しない |
| 環境 | GOMAXPROCS=8、GOGC=100、GOMEMLIMIT=off。実際の値を保存 |
| A/A | paired log ratioのbootstrap 20,000回、95% CI |
| 精度基準 | 命令数±1%、wall/cycles±1.5%の範囲にCI全体が収まること |

対象hostに合わせた条件変更は計測前にplanへ保存する。fixtureごとに名前、regex、実際のbenchmark名を照合し、行数だけで入力一致を判断しない。bootstrapのseedと実装versionも固定する。

## 5. 毎回必ず行う検証

### sourceとartifactの識別

`identity.json` に次を保存する。

- 選択repoの正規化したroot、`tsgolint_git_rev`、`typescript_go_git_rev`。不使用のrepositoryはnull。
- 前後のcommit、dirty情報、固定コピーのroot、build対象sourceのhash一覧。
- 共通ハーネス、overlay、fixture、event設定、host DB、binary、driver、collectorのhash。
- CPU/OS、Goとbenchstatのversion、build tag、GC/並列度、実行時刻。

再利用判定にはsourceだけでなくbuild条件・fixture・測定planの一致も必要とする。commit後にrevisionが変わったartifactを、sourceが同じという理由だけで自動的に `current` としない。固定snapshotについての過去の証拠は保持する。

| status | 判定 |
|---|---|
| `current` | repo_rootと両revision欄が選択checkoutに一致し、対象source・条件も検証済み |
| `stale` | artifactは存在するが、選択checkoutまたは対象条件と一致しない |
| `missing` | 対象artifactが存在しない |
| `unsupported` | 対象stageのartifactはあるが、bench.txtに要求regexのBenchmark行がない |

identityの新しさと、実行の完了・精度の合否は別に記録する。途中まで有効行がある実行は `complete=false` とし、完了した比較として集計しない。

### ハーネスとKPC

1. 両版のBatchLifetime / KPCBatchテストを通す。bind済みASTを再使用せず、batch後のStore登録数が開始値へ戻ることを確認する。
2. KPCは対象イベント群ごとに、両版で各2回のfresh processによる自己検証を行う。1process内で3 fresh pthread × 100 short/long pairs、GC境界、counterの増加・命令数のスケールを検証する。
3. PMU設定後に作成したfresh pthreadで測る。既存threadの `LockOSThread` だけで代用しない。[逆行の調査](kpc-reliability-20260910.md)を参照。
4. parse、明示GC、thread作成・join、timer操作、解放をcounter区間へ入れない。counter区間は2回のreadでbind batchを囲む。
5. read失敗、counter減少、設定readback不一致では測定を失敗にする。負値補正・wrap加算・失敗sampleの無言除外はしない。
6. 自己検証の成功は測定精度の保証ではない。実行中のA/Aも毎回判定し、命令とcyclesを別々に評価する。

EL0のみの可変counterとEL0+EL1の固定counterは別指標として保存する。KPC下のns/opにはcounter readが含まれるため、通常wallのns/opと混ぜない。別threadの処理を合算した全体コストとも扱わない。

### 現在検証済みのイベント構成

M1 / `/usr/share/kpep/a14.plist` に限定した既存構成。別CPUへ同じ番号を流用しない。

| event | number | counter index | 出力名 |
|---|---:|---:|---|
| CORE_ACTIVE_CYCLE | 2 | 2 | el0-cycles/op |
| L1D_CACHE_MISS_LD | 163 | 3 | el0-l1d-miss/op |
| INST_BRANCH | 141 | 5 | el0-branch/op |
| BRANCH_MISPRED_NONSPEC | 203 | 6 | el0-branch-miss/op |
| INST_ALL | 140 | 7 | el0-inst/op |

各config wordは `0x20000 | number`、未使用slotは0。設定ファイルにはevent名・番号・使用可能counter mask・配置を記録する。host DBのhash一致と内容照合を行い、不一致なら理由を出して止める。新しいDBやCPUの対応追加は別途検証する。実行時には既存Go実装のcounter数と設定readbackの検証も通す。

## 6. 実行と保存の契約

- ビルドは通常権限。管理者権限はKPCの自己検証と測定に限定する。
- `sudo -n` が使えなくても測定不能と決めず、macOSの標準認証経路を使う。拒否・取消はそのまま記録し、無断で再要求しない。
- binary、fixture、manifest、driverを内蔵ディスクのrun専用runtimeへコピーし、元のhashと照合する。repo-root探索用の共通test overlayもruntimeへ対応させる。runtime pathと元repo_rootを区別する。
- 引数は配列・構造化データで渡す。管理者launcherに渡すpathの引用処理を検査し、ユーザー入力をそのままshell文字列へ連結しない。
- wall、KPC、計測用buildは同じ排他機構で直列化する。Instrumentsとの併用も避ける。driverのlockが外部の全プロセスを制御できるとは仮定しない。
- 異常終了時にもcounter解放を試み、解放処理の結果を保存する。回復できない状態で自動的に次の測定へ進まない。
- run directoryは新規作成を必須にし、完了rawへ追記しない。各sampleの開始・終了・exit code・順序・binary hashを保存する。
- 各stageに `bench.txt` を作り、個別logと対応させる。集計物は別の `analysis/` へ書き、root所有rawを通常権限から変更する必要をなくす。
- 一時runtimeを消しても、source snapshot・設定・raw・再実行方法が永続artifactから復元できるようにする。利用者固有の絶対pathを共通スクリプトに埋め込まない。

## 7. 集計とレポート

collectorはrawだけで再実行できるようにする。benchmark名・iteration数・単位・標本数・round対応・欠落・重複・非有限値を検査してから集計する。

出力は主比較、確認用比較、両版のA/Aのbenchstat、中央値と増減、A/AのCIと判定、実行完了状態、identity照合結果とする。精度未達でもrawは残す。固定回数終了後に精度が通るまでroundを追加せず、次回は別の計画として扱う。

レポートには選択repo、候補clone、artifact setとstatus、ns/op・B/op・allocs/op、KPC指標、hot path、割り当て元、診断、次の行動を含める。関数別帰属を採っていなければ `missing`、古いprofileなら `stale` と記載する。KPC総量から個別helperのCPU寄与を断定しない。CPU帰属にはInstrumentsを使い、pprofは使わない。

割り当て診断は通常の測定とは別実行とする。`runtime.MemProfile` を使う場合は同じPC stackのサイズ別bucketを合算し、診断自身のallocationを区別する。rate=1でのbytesを通常B/opへ代入しない。この診断機能は共通CLIの最初の実装には含めなくてよい。

## 8. 実装順序と完了条件

1. 保存済みdriverとM1設定を引数化し、リポジトリへ移す。既存Goハーネスを再利用する。
2. identity、runtime作成、自己検証、排他・失敗記録を接続する。
3. collectorとレポート生成を接続し、READMEから一つの入口へ案内する。

完了条件:

- 一時ディレクトリや過去のcacheがなくても、新しいcheckoutからprepareできる。
- root権限なしで、identity不一致、dirty source、fixture不一致、行の欠落・重複、単位違い、順序・A/A計算、途中失敗を検査する自動テストが通る。
- 保存済みの有効rawから同じ中央値・benchstat比較・精度判定を再生成できる。bootstrapの仕様を変更する場合はversionを分け、結果差を記録する。
- M1の実機で自己検証と固定planを完走し、source/binary/eventの一致、登録数復帰、counter解放、全期待行を確認できる。
- 新規rawの同一binary比較で、実装した計測・集計経路を検査する。環境由来のA/A未達を隠さず報告できる。
- 最適化の採否を、自己検証の成功だけで決めない。

## 9. この文書作成時点

共通CLIの実装、新規benchmark、Binderの最適化は行っていない。今回確認したのは既存ハーネスと保存済みdriverの構成である。

以後のKPC準備は本書を入口とし、過去の結果文書は測定根拠として参照する。古い「前後各1回で十分」という手順より、本書の実counter自己検証と同じ計画内のA/Aを優先する。
