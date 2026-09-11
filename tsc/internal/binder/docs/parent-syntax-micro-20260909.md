# 親構文情報の一括解決：初期 microbenchmark（2026-09-09）

## 問題と仮説

監査 `store-bind-bottleneck-audit-20260909.md` を読み、既存 artifacts を確認してから実験した。親の子 slot を別々の accessor で読むと、再帰呼び出しを挟むたびに Store の nodes / children / childStart / childLen を取り直す。このコストを、同じ親の必要な構文情報を一度解決することで減らせるかを調べる。

今回は `IfStatement` の expression → then → else の一経路に限定した。識別子・keyword 判定の省略、list-only hoist、symbol microbenchmark、binder 全体の融合は行っていない。

## 対象と artifact の状態

- selected repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、`32598cba146fa4dd7b6162b838630c90d865ab28`。
- pointer comparator: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、`8ac035a394c79e693a3a7d74cb170448503ee894`。tracked dirty diff は空。Go overlay でテストだけを追加し、checkout は変更していない。
- candidate clones: 上記に加えて flownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜5、store-pr-6-perf、store-pr-7、store-pr-7-attach-parent-fix、store-pr-7-nodeseq-t10、store-redesign。全パスと revision は `identity.json` の worktrees に保存した。
- artifact set: `.cursor/skills/verify-tsc/artifacts/20260909-parent-syntax/`。
- 今回の micro artifact は `current`。Store / pointer 各 identity の `repo_root`、`tsgolint_git_rev`、`typescript_go_git_rev` が選択 checkout と一致する。TSGolint は非関与のため null。dirty diff、テストソースとその SHA256、binary SHA256、Go/CGO 設定も保存した。第一実験は `identity.json`、scalar 追加実験は `scalars-identity.json` に分けた。
- 既存 `eval-8ac035a-20260909` と監査対象 Instruments/A-B は `stale`。監査 artifact 自体は現 HEAD の identity を持つが、元の測定が過去なので性能測定としては `stale`。
- 今回の artifact の BindHot / BindKPC stage は `unsupported`（`bench.txt` に該当 Benchmark 行がない）。候補を実際の binder に適用した paired BindHot / BindKPC artifact は `missing`。EL0 cache/branch counters、候補の GC/scan 計測も `missing`。

## 実装と意味の一致

`tsc/internal/ast/parent_syntax_bench_test.go` にテスト専用の以下を置いた。本番 binder や公開 Store API は変更していない。

1. baseline: `ChildRef(ref, 0/1/2)` を、子処理の呼び出しを挟んで別々に読む。
2. snapshot: 親 header を一度解決し、3 slot を `[3]NodeRef` にコピーして返す。
3. scalars: 同じ slot 範囲を一度解決し、3つの `NodeRef` を個別の戻り値で返す。

pointer では実際の `NodeFactory.NewIfStatement` と `AsIfStatement()` のフィールドを使う。両表現で同じ kind の子と欠損 else を作り、同じ順序で kind を読み、順序依存の checksum を返す。全方式で child processor は noinline。処理間の呼び出し境界を残すためで、完全な binder 再帰のモデルではない。外側の benchmark loop は両バイナリとも間接 CALL を使うことを逆アセンブルで確認した。

Store のテストは全 slot の値、順序を含む checksum、欠損 else、nil/zero 親（配列版）、Store 拡張前後の値を確認して PASS。子の診断・symbol・CFG の意味の一致は、まだ本番 binder に適用していないため未検証。

snapshot は pointer/slice を返さず、整数の値を保持するので backing array の再確保で参照が dangling にならない。ただし読み取り後の構文変更は反映されない。利用条件は同じ Store の schema が既知の IfStatement で、処理中にその親の子 slot を変更しないこと。foreign child は既存 ChildRef と同じゼロ表現を保つだけで、外部 Handle の解決機能は追加しない。

## 測定方法

Apple M1 / darwin arm64 / Go 1.26.0 / CGO_ENABLED=1。既存レポートの Go 1.26.6 と違うため、過去の値との before/after 比は算出しない。

1 op は1親の3 slot を読み、その子の kind を処理する単位。small は64親、large は65,536親の順次反復。半数は else が欠損。構築は `b.Loop()` の timer 外。working set は表現によって異なるため、large の結果を cache miss 改善の直接証拠にはしない。

第一実験は baseline / snapshot / 未改変 pointer を順序ローテーションして12 rounds、各 subbenchmark 500ms。scalar 追加実験は同じ新バイナリの baseline / scalars を交互に12 rounds、各500ms。生データを保存して benchstat で比較した。最初の `warmup-*.txt` はビルドと重なり変動が大きかったため、比較から外して別保存した。第一実験と追加実験の中央値を手で比較して効果量を作らない。

## 結果と診断

**3つの index を個別の戻り値で返す試作は約13%短縮した。配列を返す試作では有意な改善がなかった。親構文の一括解決を支持する初期証拠だが、実 binder の性能・GC との両立は未検証。**

第一実験（全列 ns/op、n=12）：

| working set | pointer | Store baseline | array snapshot | baseline → snapshot |
| --- | ---: | ---: | ---: | --- |
| small | 4.986 | 8.962 | 9.118 | 有意差なし、p=0.114 |
| large | 11.20 | 10.44 | 10.85 | 有意差なし、p=0.128 |

追加実験（同じ新バイナリ内の比較、n=12）：

| working set | Store baseline ns/op | scalar snapshot ns/op | benchstat |
| --- | ---: | ---: | --- |
| small | 7.695 | 6.712 | −12.78%、p=0.000 |
| large | 8.062 | 6.981 | −13.42%、p=0.000 |

全方式・両 working set で **0 B/op、0 allocs/op**。p=0.000 は benchstat の表示（数学的な確率ゼロという意味ではない）。第一実験と追加実験は別時間帯・別バイナリなので array と scalars の直接比較によるコピーコストの因果推定はしていない。baseline 自体も変動しているため、それぞれの実験内の A/B のみを効果量とする。

逆アセンブルでは、baseline の nodes header 解決と childLen / childStart 取得は3回、両 snapshot は1回。bounds panic の静的 call site は6→2。子を処理する CALL は全方式3回のまま。accessor は inline されており、削減対象は独立 accessor の呼び出し回数ではなく、その展開先のロードとチェック。

配列版には `[3]NodeRef` のゼロ初期化とスタック間コピーが残り、stack frame は baseline 48 B に対して80 B。scalar 版はこの配列コピーがなく64 Bで、子2つの取得に `LDPW` が使われる。これは「解決済みの値をどう渡すかも重要」という仮説に整合するが、copies だけに時間差を厳密に帰属したわけではない。静的命令行数や panic call site 数を inst/op として扱わない。

今回の提案は、scalar 戻り値の schema 専用アクセサを次の binder A/B の候補にすること。配列版はこの環境で採用根拠なし。一括解決全般の効果や未改変 pointer への到達を結論づけない。


## ホットパスと allocation driver

既存 profile で最も広い領域は walk + helpers。今回の狭い修正対象は `bindIfStatementRef` に相当する3つの named child の Store 解決で、最大ホット領域そのものではない。synthetic micro の削減率を Bind 全体へ掛けたり、実 workload の支配的ボトルネックの証明に使ったりしない。

既存 BindHot/checker.ts は pointer 13,203,727 ns/op、7,425,296 B/op、13,954 allocs/op に対し、Store 20,256,109 ns/op、12,799,476 B/op、14,165 allocs/op（いずれも stale、監査の benchstat で時間 +53.41%）。割り当て増の候補は symbolIdx / flows 列、symbolRefs、FlowNode 32→48 B、Symbol の Handle と declaration arena の拡大。今回の accessor 実験はこれらを変えない。

## 次の検証と採用条件

初期実験の成果は accessor の一括解決と戻り値表現を切り分けたこと。本番への採用判断には以下が必要。

- 有効な戻り値形式を `bindIfStatementRef` 一箇所に適用し、expression / then / else の bind 順序と同じ CFG 操作を維持する。
- checker.ts と dom.generated.d.ts の BindHot / BindKPC を同一 fixture hash、Go/CGO、dirty diff の記録付きで交互実行し、benchstat 比較。KPC は最低6 rounds。Instruments と同時実行しない。
- BindHot の有意な改善、inst/op と cycles/op の低下を確認。EL0 instructions / load-store / branch / cache miss は fixed EL0+EL1 と分ける。
- 診断・symbol・CFG・走査順の一致、parse+bind 合計、live heap / scan work の非悪化を確認する。最終性能は未改変 `8ac035a` と比較する。

AST の noscan 性質を維持できる整数 snapshot でも、GC 高速化と binder 高速化の両立はこの micro だけでは未証明。

## 再現

```sh
cd /Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc
go test ./internal/ast -run '^TestParentSyntaxIf$' -count=1
PARENT_SYNTAX_VARIANT=baseline go test ./internal/ast -run '^$' -bench '^BenchmarkParentSyntaxIf$' -benchmem -benchtime=500ms -count=12 > /tmp/parent-old.txt
PARENT_SYNTAX_VARIANT=scalars go test ./internal/ast -run '^$' -bench '^BenchmarkParentSyntaxIf$' -benchmem -benchtime=500ms -count=12 > /tmp/parent-new.txt
benchstat /tmp/parent-old.txt /tmp/parent-new.txt
```

実測の交互実行手順は artifact の `run_micro.py` と `run_scalars.py`。これらは raw に追記するため再実行時は別出力ディレクトリを使う。第一実験のソースは `parent_syntax_bench_test.go`、追加実験のソースは `scalars_parent_syntax_bench_test.go` を artifact 内に保存した。pointer 用ソース・overlay も同じ場所にある。逆アセンブルは `accessors.asm`、`scalars-accessors.asm`、`benchmark-loops.asm`。

実 binder への適用結果は [IfStatement 親構文一括取得の実 binder A/B](parent-syntax-bind-20260909.md) を参照。BindHot / BindKPC の有意な改善がなく、この一箇所の変更は不採用となった。
