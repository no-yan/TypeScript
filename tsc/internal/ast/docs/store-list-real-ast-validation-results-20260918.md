# Store tree list fast path: 実 AST 検証結果

作成日: 2026-09-18。結論: **local list の owner と header を走査要素ごとに再解決しない変更は、parser が `checker.ts` から生成した実 Store AST の全走査でも有意に高速だった。** `BenchmarkE2EWalkStore` は 4.794 ms/op から 4.560 ms/op、**-4.88%、benchstat p<0.001、n=12/arm**。12組すべての paired sample で candidate が速く、AB/BA の順序別比較もそれぞれ有意だった。同一 candidate binary の A/A 対照は差が未解決で、p=0.755だった。pprof は取得・使用していない。

これは TypeScript の parse、bind、check を含む end-to-end 結果ではない。parseを計測区間外に置き、実ファイルから作ったStore ASTをrootから全走査し、各nodeのkind・flags・位置を読むstageの結果である。

## 1. 対象と artifact status

| 項目 | 値 |
|---|---|
| selected repo | `/Volumes/SanDisk1TB/worktree/ast-traversal-bench-design` |
| baseline | `3b1ea86b74135c691e5d7025df326b439f680868`、candidate commitの親 |
| candidate | `613a54b22f8fe8276beea6ee10f085620158b230` |
| TSGolint revision | null。今回の対象にTSGolint checkoutはない |
| input | `testdata/fixtures/compiler/checker.ts`、SHA-256 `4fb2f7e7…15a83e9` |
| benchmark | `BenchmarkE2EWalkStore`、source SHA-256 `cf59e485…0061b` |
| host | macOS 26.6.2、darwin/arm64、Apple M1、Go 1.25.0 |

候補cloneは `cursor-ast-store-tests`、`binder-rewrite`、`profile`、`flownode`、`lock-design-inv`、`lock-profile`、`store-pr-7`、`store-pr-7-nodeseq-t10`、`store-pr-7-attach-parent-fix`、`store-redesign`、`store-schema-foreach-child`、`store-nolock-exp`、`nolock97`、`putcol-bce`。これらの結果は混ぜず、ユーザー指定repoのclean detached worktreeだけを測った。

| artifact set | status | 根拠 |
|---|---|---|
| `tsc/before.txt` | stale | revisionとdirty source hashがなく、今回の比較には使用していない |
| 合成treeのlist実験 | current | candidate commitに保存済み。ただし測定sourceは当時のdirty snapshot |
| `store-list-real-ast-validation-20260918/` | current | repo root、baseline/candidate revision、null TSGolint、source/binary hashがidentityと一致 |
| 実projectのparse/bind/check end-to-end | missing | 今回の計測区間外 |
| hardware instruction/cycle counters | missing | 未校正counterやCPU sample帰属で代用していない |
| pprof | unsupported | ユーザー指定により実験手段から除外 |

identityは [identity.json](artifacts/store-list-real-ast-validation-20260918/identity.json)、全raw、process順、pair差、benchstat出力は [artifact directory](artifacts/store-list-real-ast-validation-20260918/) に保存した。

## 2. 測定方法

baselineとcandidateを別のclean detached worktreeからtest binaryへビルドした。両revisionでfixtureとbenchmark sourceのhashは同じである。candidate commitには文書も含むが、実行コード差は `forEachChildList` のlocal-list fast pathである。

各sampleは独立processとし、`GOMAXPROCS=1`、`-test.cpu=1`、500ms/cellで測った。12 pairをAB、BAの順に交互実行し、各順序を6組ずつにした。内部反復を独立標本として数えていない。同じcandidate binaryを first/second の順で12 pair測るA/A対照も別blockで実行した。

benchmarkは `checker.ts` を一度parseし、Storeとrootを保持してGCを実行した後にtimer loopへ入る。計測区間は `ast.Walk` による全走査と、各handleのkind・flags・loc読み出しである。parse、AST構築、list分布収集は計測外である。

## 3. 時間とallocation

主比較の [benchstat-balanced.txt](artifacts/store-list-real-ast-validation-20260918/benchstat-balanced.txt):

| 指標 | baseline | candidate | 差 | 判定 |
|---|---:|---:|---:|---|
| ns/op | 4,794,000 | 4,560,000 | **-4.88%** | **p<0.001、n=12/arm** |
| B/op | 0 | 0 | 0 | 全sample同値 |
| allocs/op | 0 | 0 | 0 | 全sample同値 |

順序を分けても方向と有意性は一致した。

| process order | baseline | candidate | 差 | benchstat |
|---|---:|---:|---:|---:|
| AB、n=6/arm | 4.821 ms/op | 4.536 ms/op | -5.92% | p=0.002 |
| BA、n=6/arm | 4.763 ms/op | 4.562 ms/op | -4.21% | p=0.002 |

[pairs.tsv](artifacts/store-list-real-ast-validation-20260918/pairs.tsv) の12 pairはすべてcandidateが速く、pair差は -7.34%〜-3.21%だった。A/A対照はfirst 4.480 ms/op、second 4.484 ms/op、差は未解決（p=0.755、n=12/arm）。比較block間には水準差があるため、4.88%を環境に依存しない定数とは扱わない。一方、balanced pair、AB、BA、A/Aの全証拠は変更による有用なcost reductionと整合する。

## 4. 実 AST のlist分布

同じ一時probeをGo overlayで両clean worktreeへ重ね、workspaceとbenchmark binaryを変更せずに分布を取得した。before/afterのJSONは完全一致した。

| 指標 | 値 |
|---|---:|
| visited nodes | 298,054 |
| Store.Len | 302,587 |
| list slots | 105,860 |
| nil list slots | 63,151 |
| present local lists | 42,709 |
| foreign lists | 0 |
| list elements | 76,868 |
| zero-ref elements | 0 |
| external elements / nil holes | 0 / 0 |
| maximum list length | 2,350 |

今回の入力ではpresent listの全件と全要素がlocal fast pathの対象で、foreign-list fallbackとzero-ref element fallbackは発生しない。list長は1が25,150件、2が9,858件、3が3,016件で、小さいlistが多数を占める。一方で長さ2,350と1,148のlistも各1件あり、要素数加重でもfast pathへの露出がある。`Store.Len`とvisited nodesの差は未到達・lazy要素などを含み得るため、同値を正しさ条件にはしていない。

## 5. 診断と採否

実行経路は `ast.Walk → Handle.ForEachChild → forEachChildSchema → forEachChildList` である。介入はlistのnode、edge、順序、値の読み出しを保ち、local listのowner/start/length解決をloop外へ移した。before/afterで実ASTの分布が一致し、timed walkは両者とも0 allocationで、candidateだけが一貫して速い。したがって改善原因はallocationではなく、list elementごとの重複したowner/header解決と依存loadの削減に整合する。

hot pathの相対順位や、削減された命令数・cycle数は測っていない。pprofのCPU帰属から推定もしていない。このため「どの命令が何個減ったか」やcache missの減少は断定しない。

採否は **採用を支持** とする。単純なread-only fast pathで、既存のforeign/zero-ref fallbackを維持し、clean commitのpackage testsが通り、合成treeだけでなくparser生成の実AST stageでも再現性のある削減を示した。固定の最小改善率は要求しない。

## 6. 次の行動

次はこの変更を含む累積stackを、代表的な実projectのparse/bind/check全体で測る。今回の4.88%をend-to-endへ外挿せず、変更対象stageが全体の何割か、wall time差が解像できるか、最大RSS・割り当て・GCが悪化しないかを分けて記録する。差が統計的に未解決でも、このstageのdemonstrated improvementと0-allocation契約は保持する。

さらに機構を詰める場合は、pprofではなくcompilerのassembly/BCE出力と校正済みhardware counterを使い、`TryBindListSpan`、`BindListSpanElem`、`ListAt` fallbackの命令・load差を調べる。counterが利用できない場合は、今回の一要因介入とwall timeを根拠に留める。
