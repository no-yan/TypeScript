# Store tree list 処理除去実験の結果

作成日: 2026-09-18。結論: **local list の owner と header を要素ごとに再解決する処理の除去は、この合成 repeated tree の full-tree 走査で統計的に有意である。** 事前登録した主評価セル 4096 subtrees は 407.7 µs/op から 321.2 µs/op、**-21.21%、benchstat p<0.001、n=12/arm**。12組すべての AB/BA pair で candidate が速かった。pprof は取得・使用していない。

この結果は list 自体をデータ構造から消したものではない。同じ list・node・edge・訪問順を保ち、local list の owner/start/length をループ前に一度だけ解決した介入である。したがって「list accessor の重複処理はこの workload で有意」という証拠であり、実プロジェクト全体が21%速くなるという証拠ではない。

## 1. 対象と artifact status

| 項目 | 値 |
|---|---|
| selected repo | `/Volumes/SanDisk1TB/worktree/ast-traversal-bench-design` |
| TypeScript-Go revision | `e6820c555897786acd17a15be84a19855a396629` |
| TSGolint revision | null。今回の選択対象に TSGolint checkout はない |
| baseline | ユーザーの既存 dirty 変更を含む。同じ状態から list 実験差分だけを除いた test binary |
| candidate | baseline に `forEachChildList` の local-list fast pathだけを追加した test binary |
| host | darwin/arm64、Apple M1、Go 1.26.0 |
| primary | `full-tree/subtrees-4096/store` |
| controls | full-tree 32/256、変更経路を通らない expression 32/256/4096 |

候補 clone は `cursor-ast-store-tests`、`binder-rewrite`、`profile`、`flownode`、`lock-design-inv`、`lock-profile`、`store-pr-7`、`store-pr-7-nodeseq-t10`、`store-pr-7-attach-parent-fix`、`store-redesign`、`store-schema-foreach-child`、`store-nolock-exp`、`nolock97`、`putcol-bce`。結果は混ぜず、ユーザー指定 checkout だけを測った。

| artifact set | status | 根拠 |
|---|---|---|
| `tsc/before.txt` | stale | revision・dirty source hash がなく、仮説生成に限定 |
| sequential before/after | current・補助 | 同じ checkout の一要因比較だが、全 baseline の後に全 candidate を測った |
| `store-list-elision-20260918/balanced/` | current・主証拠 | identity、source/binary hash、順序、独立process rawを保存。選択 repo/revision/null TSGolint と一致 |
| 実入力 stage / end-to-end | missing | 今回は合成 tree の要因分離まで |
| hardware counters | missing | wall time と介入で判定可能。未校正 counter を代用しなかった |

identity は [identity.json](artifacts/store-list-elision-20260918/identity.json)、全 raw と再実行スクリプトは [artifact directory](artifacts/store-list-elision-20260918/) にある。介入差分はこの結果と同じcommitの `store.go` hunkである。
測定に使った test binary はhash記録後にworkspaceから除去した。再実行時はidentityとcommitの差分に対応するbaseline/candidate binaryを同じ名前でartifact directoryへビルドする。
測定sourceのhashは既存dirty変更を含み、isolated commitの`store.go` hashとは異なる。コミットは介入hunkだけを保存し、測定時の既存dirty変更を取り込まない。

## 2. 介入と測定契約

repeated tree は root の `ArrayLiteralExpression` に長さ N の list が1本あり、各 subtree は7 nodeで内部 edge は named child である。baseline の `forEachChildList` は各要素で `ListAt` を呼び、owner と list header を再解決する。candidate は既存の `TryBindListSpan` / `BindListSpanElem` を使い、local list の start/length を一回だけ取得する。

意味は維持した。要素値は各反復で `children` から再読する。0 ref は `ListAt` へ戻して nil hole と external child を維持する。foreign-owner list は baseline loop 全体へ fallback する。callback順序と early exit は同じ。変更箇所は `internal/ast/store.go` の `forEachChildList` だけで、NodeSeq と生成schemaは変えていない。

baseline/candidate の test binary を固定し、別processで12 pairを実行した。順序は AB, BA を交互に各6組。各processは `GOMAXPROCS=1`、500ms/cell、1 process sample/cell。内部反復を独立標本に数えていない。raw の process順は [order.tsv](artifacts/store-list-elision-20260918/order.tsv)、各pairは [pairs.tsv](artifacts/store-list-elision-20260918/pairs.tsv) に保存した。

## 3. 結果

全体の benchstat は [benchstat-balanced.txt](artifacts/store-list-elision-20260918/benchstat-balanced.txt)。異なるサイズをpoolしたgeomeanは採否に使わない。

| visitor / subtrees | baseline ns/op | candidate ns/op | delta | benchstat |
|---|---:|---:|---:|---:|
| expression / 32 | 1,723 | 1,717 | -0.32% | p=0.034 |
| expression / 256 | 13,540 | 13,540 | unresolved | p=0.966 |
| expression / 4096 | 216,700 | 217,300 | unresolved | p=0.347 |
| full-tree / 32 | 2,896 | 2,530 | -12.64% | p<0.001 |
| full-tree / 256 | 25,370 | 20,060 | -20.91% | p<0.001 |
| full-tree / 4096 | 407,700 | 321,200 | **-21.21%** | **p<0.001** |

全セルで logical-nodes、visits/op は一致した。full-tree/4096 は両者とも 28,673 logical nodes、28,673 visits/op。全セル・全標本で **0 B/op、0 allocs/op**。workload の `Verify` が pointer/store の完全traceを区間外で比較し、timed loop は毎回 Result 全体を期待値と照合した。

順序別でも主評価の方向は一致した。

| order | baseline ns/op | candidate ns/op | delta | p |
|---|---:|---:|---:|---:|
| AB、n=6/arm | 368,000 | 321,000 | -12.75% | 0.002 |
| BA、n=6/arm | 408,600 | 322,100 | -21.17% | 0.004 |

12 pairすべてで candidate は速かったが、4096 の pair差は -1.84%〜-22.55%に分かれた。baseline 自体に約328µsと約408µsの二群がある。候補は概ね321µs付近に集中した。この変動要因は未解決なので、効果量を正確な単一値とは扱わない。ただし最小のpair差でも改善方向で、AB/BA双方が有意なため「差があるか」は demonstrated improvement と判断する。

expression/32 の -0.32% は p=0.034だが、事前登録した主評価ではなく、同じ経路を変更していない6 control中の1つだけである。多重比較と微小なcode-layout/環境差を排除できないため、実証した list 効果には数えない。expression 256/4096 は差が未解決だった。

list element 1件あたりの中央値差は、4096で約21.1ns、256で約20.7ns。32はbaselineの二峰性の影響が大きく、同じ比例則の根拠には使わない。

## 4. 正しさと既知の失敗

candidate で次を確認し、すべてpassした。

- schema順、SourceFile順、JSDoc runtime順、first/middle/last early exit
- foreign-owner list、external list element
- workload の完全trace parity、zero allocation/GC contract
- `git diff --check`

`go test ./internal/ast` 全体は baseline から green ではない。実験前後とも、既存 dirty 変更による `TestTryBindListSpanBoundaries`、Ident guard、Freeze guard、Global/zero Handle の失敗が残る。candidateの出力は [full-test-candidate.txt](artifacts/store-list-elision-20260918/full-test-candidate.txt)。今回の list 差分が対象とする狭いテストはpassしており、失敗集合に今回固有の追加は観測していない。ただし全 package green ではないため、この checkout の全体正当性を主張しない。

span は read-only traversal の借用契約である。callback中の要素置換は毎回再読されるが、callback中の Restore、Compact、list start/length変更は保証しない。現在の実験workloadは構築後にFreezeし、timed traversalはread-onlyである。production適用範囲を広げる場合はこの契約を確認する。

## 5. 診断と次の行動

hot path の事前候補は `ForEachChild → forEachChildList → ListLen/ListAt → listOwner/list header` だった。今回の一要因介入により、少なくとも repeated full-tree ではこの重複処理が有意なコストであることを確認した。pprofのCPU帰属は使っていない。

allocation driver はtimed traversal内ではない。両者とも0 B/op、0 allocs/opであり、改善はallocation削減ではなく、owner/header再解決と関連する命令・依存loadの除去に整合する。hardware instruction/cycleは未取得なので、命令削減量やcache原因を断定しない。

次の行動は、parserで作った実store ASTの list 長分布を保存し、同じ candidate を実入力 full-tree stageで AB/BA 比較すること。合成結果の21%をend-to-endへ外挿しない。実入力でも有用な削減が再現し、read-only契約が実callerに合う場合に採用候補とする。baselineの二峰性は、効果量を精密化する必要が出た時だけ、processごとの環境情報と再現条件を追加して追う。
