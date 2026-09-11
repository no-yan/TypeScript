# bindベンチマークの寿命修正（2026-09-11）

## 修正内容

有効なハーネス`bind_bench_test.go`を修正した。コメントアウトされていた旧`bind_investigation_bench_test.go`は戻していない。

- 1サンプルに最大10個の独立した未bind ASTを事前parseする。
- batch全体の準備後に明示的GCを一回だけ実行する。bindとbindの間にはGC・parse・タイマー切替を入れない。
- 両版でbatch全体を最後まで保持し、計測終了後に参照を解放する。Store版は所有Storeも登録解除する。
- 登録数がbatch準備前の値へ戻ること、全ASTがdistinct/unboundで始まりboundで終わることを確認する。
- cleanupは明示呼出しとtesting.Cleanupの両方から安全に呼べる。GoのN=1予備実行も同じ寿命管理を通る。
- `-benchtime=10x`を使用する。1〜10以外は失敗させ、時間指定による自動反復増加で巨大なAST batchを作らない。
- `BINDER_BENCH_GC_OFF=1`で、準備と事前GCの後から自動GCを無効にする補助条件も選べる。終了時に元の設定を復元する。

Store固有の解除と登録数参照は`bind_bench_store_test.go`だけにある。
pointerビルドでは同ファイルを登録数0／解除no-opの補助関数へoverlayする。
共通の計測ループは一字一句同じで、解除処理は計測区間の外。Binderの処理ロジックはこの修正では変更していない。

## 検証

`TestInvestigationBatchLifetime`を両版で実行し、各入力についてbatchサイズ1→10→10→10を確認した。

| 検証項目 | 結果 |
|---|---|
| distinct/unbound入力・bound出力 | 両版成功 |
| Storeの登録数 | 各batchで0→1/10→0 |
| cleanup二重呼出し・slice内参照の解除 | 両版成功 |
| 独立AST間のSymbol数・診断数 | 同一かつ両版一致（checker 18,444/0、dom 25,112/0） |
| batch上限 | 両版とも11xを期待どおり拒否 |
| 生成・ビルド | 共通ハーネスで両版の新規ビルド成功 |

寿命probeで解放後にGCを2回実行しpool世代を整理した。これはbenchmark内のGC条件とは別の検証。
31回bind後のStore版heap liveはchecker 3,478,456 B、dom 2,676,120 B。
10個batchを繰り返した際も、ASTを保持したまま数百MBずつ積み上がる状態はない。
scan量は最終checker 245,616 B、dom 245,912 B。小さなruntime/registry directory等の増加は残る。
新たなFlow/構文の完全digest監査は実行していない。今回はハーネスの寿命・結果の安定性の検証で、Binder本体の意味変更は行っていない。

## 固定規模の再測定

各入力10 binds×6 rounds。同一バイナリ対照を各block内に入れた。
ラベルpointer_a/store_a/pointer_b/store_bの順序を事前に固定し、回転と反転で偏りを抑える。
主比較はa対a、b対bは確認用であり、12個の独立サンプルへ合算していない。
通常GCとGC無効の補助条件で各48行、計96 Benchmark行を保存した。
全条件でA/Aの±1.5%条件は不合格。今回の時間差を確定倍率・採用根拠にしない。追加roundなし。

主比較の観測値（6サンプル中央値）：

| 条件・入力 | pointer ns/op | Store ns/op | pointer B/op | Store B/op | pointer allocs/op | Store allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| 通常GC checker | 18,739,342 | 25,198,920.5 | 7,424,013 | 12,798,288 | 13,950 | 14,163 |
| 通常GC dom | 6,456,304 | 10,244,177 | 5,285,994 | 7,869,821 | 16,558 | 16,683 |
| GC無効 checker | 29,037,000 | 34,712,302.5 | 7,424,013 | 12,798,288 | 13,950 | 14,163 |
| GC無効 dom | 10,414,223 | 14,289,727 | 5,285,994 | 7,867,326.5 | 16,558 | 16,681.5 |

benchstatの主比較p値は、通常GC checker .065／dom .026、GC無効 checker .041／dom .065。
通常GCのA/A区間は、pointer checker −41.58〜+15.10%、dom −23.99〜+24.07%、Store checker −29.44〜+13.20%、dom −24.13〜+0.22%。
GC無効でも両版・両入力で精度未達。GCの2条件自体も別時間帯の測定なので、絶対時間差からGC無効化の効果を推定しない。
既知の広いhot pathはwalk/helpers、allocation driverはsymbolIdx/flows列、symbolRefs、FlowNode/Symbol/Handle大型化。今回新たなprofileは取得していない。

## 対象・artifactの状態

- Store repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、HEAD `32598cba146fa4dd7b6162b838630c90d865ab28`。
- pointer repo: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、HEAD `8ac035a394c79e693a3a7d74cb170448503ee894`。
- 両tsgolint revisionはnull。候補clone一覧はartifactのworktrees.txt。flownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr系、store-redesignは今回未使用。
- artifactはStore repo内`.cursor/skills/verify-tsc/artifacts/20260911-bind-batch-lifetime/`。
- repo_rootと両revisionが一致するためidentity上はcurrent。全binary・frozen overlayのSHAと96行を確認した。
- **GC無効測定の途中でliveのbinder.goが別途更新された。** source一致の最終検査がこれを検出して停止した。バイナリは変更されていないためrawは固定snapshotの結果として残す。更新後の現在Binderの測定値とは呼ばない。
- `verification-final.json`と`store-post-build-source-change.patch`に差異を保存。元の完了チェック失敗を隠して成功扱いにはしていない。
- unsupported stageはない。更新後Binderの測定、精度gate通過の結果、同じ実プロジェクトの逐次／並列対照はmissing。

次はコードとマシン負荷が安定したセッションで、この修正版ハーネスを使う。
旧寿命条件の約1.3倍や今回の大きく揺れた値をもとに、差が解消／拡大したとは判断しない。

## 手元で実行

Store repoの`tsc`ディレクトリで、最新ソースを使う場合：

```sh
env GOMAXPROCS=8 GOGC=100 GOMEMLIMIT=off CGO_ENABLED=1 GOFLAGS='' GOTOOLCHAIN=go1.26.0 \
  BINDER_BENCH_GC_OFF=0 \
  BINDER_INVESTIGATION_MANIFEST=/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/.cursor/skills/verify-tsc/artifacts/20260911-bind-batch-lifetime/manifest.json \
  go test -tags=binderinvestigation ./internal/binder \
  -run '^TestInvestigationBatchLifetime$' -v -count=1
```

同じ環境変数で、時間測定時は末尾を次に変える：

```sh
go test -tags=binderinvestigation ./internal/binder \
  -run '^$' -bench '^BenchmarkBindInvestigation$/^checker[.]ts$' \
  -benchmem -benchtime=10x -count=1
```

domはfixture regexを`dom[.]generated[.]d[.]ts`にする。GC無効の補助条件は`BINDER_BENCH_GC_OFF=1`。
測定は新規プロセスで繰り返し、同一プロセスでの`-count=6`と混ぜない。
pointer版を試す際は、共通`bind_bench_test.go`／`bind_bench_lifetime_test.go`と、artifact内`bind_bench_pointer_test.go`を使い、Store専用補助関数を同時に入れない。
この手順の最新ソース実行は、保存済み固定バイナリの再実行とは区別する。
