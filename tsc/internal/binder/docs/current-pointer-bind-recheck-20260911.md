# 現行Store／pointerのbind単体再計測（2026-09-11）

当初の1.3〜1.6倍という遅さが現行コードにも残るかを、同じbind単体ハーネスで直接確認した。
今回の中央値は **checker 1.320倍、dom 1.277倍**。benchstatではそれぞれp=.002、p=.004。
ただしwall A/Aは両版・両入力とも不合格であり、倍率の精密な確定・採用gate通過とは扱わない。
この限定条件では「現行Storeのbind単体にも遅さが残る」という方向を支持する結果である。

## 対象・artifact

- Store: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、`typescript_go_git_rev=32598cba146fa4dd7b6162b838630c90d865ab28`、`tsgolint_git_rev=null`。未コミットの生成Accessor・レビュー修正を含む現在のコード。
- pointer: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、`typescript_go_git_rev=8ac035a394c79e693a3a7d74cb170448503ee894`、`tsgolint_git_rev=null`。
- 他候補: flownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr系、store-redesign。未使用。完全な一覧はartifactの`worktrees.txt`。
- artifact: Store repo内`.cursor/skills/verify-tsc/artifacts/20260911-current-pointer-bind-recheck/`。

新artifactはrepo_rootと両revisionが選択checkoutに一致し、**current**。
両版を新規ビルドし、記録した全Go source SHAが測定後も現在checkoutに一致すること、binary・overlayのSHA一致を確認した。
当初のartifactもidentity上はcurrentだが、StoreのBinder sourceは現在と異なる。旧値を今回のバイナリの結果として流用していない。
対象Benchmark行なしのunsupported stageはない。現行CLIによる同一プロジェクトの逐次／並列対照はmissing。

## 条件

- Apple M1、Go 1.26.0、GOMAXPROCS=8、GOGC=100、GOMEMLIMIT=off、CGO_ENABLED=1。
- 当初と同一の`BenchmarkBindInvestigation`ハーネスを両版へoverlay。監査hookなし、`-B`・アルゴリズム変更なし。
- 各iterationでparseし、GC実行後にタイマーを開始する。**計測対象は1ファイルのBindSourceFileのみ**。parseと直前GCは時間・割当計測から除外。
- checker.ts／dom.generated.d.ts、各10 iterations×6 rounds。毎roundで条件順を反転。
- pointer A/A、Store A/A、pointer対Storeの順。計72 Benchmark行を確認。追加roundなし。
- rawとbenchstat: `aa-pointer/`、`aa-store/`、`pairs/`。

## 結果

6サンプルの中央値。p値はbenchstat。

| 入力 | pointer ns/op | Store ns/op | Store/pointer | p |
|---|---:|---:|---:|---:|
| checker | 12,688,139.5 | 16,745,956 | **1.320（+31.98%）** | .002 |
| dom | 4,603,637.5 | 5,876,910.5 | **1.277（+27.66%）** | .004 |

| 入力 | pointer B/op | Store B/op | pointer allocs/op | Store allocs/op |
|---|---:|---:|---:|---:|
| checker | 7,425,345 | 12,799,499 | 13,954 | 14,165 |
| dom | 5,287,312 | 7,870,383.5 | 16,562 | 16,684 |

割当bytesはchecker +72.38%、dom +48.85%、alloc回数は+1.51% / +0.74%（各p=.002）。
これはbind段階で新たに発生する割当であり、ユーザー提示のCLI全体のMemory used／Memory allocsとは範囲が違う。

前後のpaired log-ratio bootstrap 95%区間はchecker +28.22〜+41.74%、dom +21.17〜+33.55%。
これは今回のサンプル間の不確実性を表し、ホストの系統的な揺れ全体を保証する区間ではない。

### A/A

合格条件はpaired log-ratioの95%区間全体が±1.5%以内。

| 版 | checker | dom |
|---|---:|---:|
| pointer | −17.29〜+2.13%、不合格 | −4.08〜+1.02%、不合格 |
| Store | +0.67〜+14.61%、不合格 | −3.08〜+5.37%、不合格 |

差の方向はbenchstat・paired比較で一致するが、精密な倍率や小差の採用判定は留保する。

## CLIでBind timeが近いこととの関係

ユーザー提示のCLIは9,733ファイルを含むプロジェクト全体であり、本測定の単一大型ファイルとは入力分布と実行経路が異なる。
コード確認では、両版の`Program.BindSourceFiles`は`core.NewWorkGroup(p.SingleThreaded())`を使う。
通常の並列経路はファイルごとにgoroutineを起動し、全処理の終了を待つ。
`--extendedDiagnostics`のBind timeは`GetBindDiagnostics`全体の経過時間であり、ファイルごとのCPU時間の合計ではない。
現在のStore経路ではbind後のStore Freezeと診断収集もこの区間に含まれる。

従って今回から言えるのは「大型2入力の単体bindでは約1.3倍の差が観測された」まで。
プロジェクト全体での近いBind timeを説明する原因を、並列性・GC・入力構成のいずれかに特定したわけではない。
ユーザーがHEADでビルドしたCLIと、今回の未コミット改修込みのStoreバイナリも区別する。

## 診断・次のアクション

前回のレビュー修正前後比較だけではpointerとの差を判断できなかった。今回の直接比較で、現行Storeが単体bindでpointer同等に到達したという証拠は得られなかった。
既知の広いhot pathはwalk/helpersで、slot解決は一部。今回新しいprofileは取得していない。
既知のallocation driverはsymbolIdx/flows列、symbolRefs、FlowNode/Symbol/Handleの大型化であり、アクセサ修正後もbind割当増が残っている。

次はCLIで使った同じプロジェクトについて、バイナリのsource対応を固定したうえで、通常並列と逐次bindを比較する。
それにより入力構成による差と、並列実行・GCを含む実行条件による差を切り分ける。今回のA/Aを通すためにroundは追加しない。

再現driverは`tools/scripts/tsc/binder_current_pointer_recheck.py`。artifactにも依存driver、build command、frozen AST/Binder sources、fixture SHA、環境、実行順を保存済み。


### 測定方法レビューによる留保（2026-09-11）

[方法レビュー](bind-benchmark-method-review-20260911.md)で、Store登録未解除によるiteration間のAST保持と、反復ごとの強制GCの影響を確認。約1.3倍は元ハーネスでの観測値として保持し、Binder固有の退行率としての採用は保留する。次はASTの寿命条件を揃え、登録数を監査してから再計測する。今回の追記に伴うbenchmark再実行・実装変更はない。
