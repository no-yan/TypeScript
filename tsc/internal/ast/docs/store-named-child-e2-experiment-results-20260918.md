# Store tree E2: named child の一括束縛実験

作成日: 2026-09-18。結論: **BinaryExpression の4つのnamed child slotについて、親headerとchildStartを一度だけ読み、各child refを同じspanから取得する方法は有意に速い。** コード配置を同一にした診断binaryで、16,384-node mixed full-treeは276.9 µs/opから260.4 µs/op、**-5.96%、benchstat p<0.001、n=12/arm**。256-nodeでも -6.45%、p<0.001。変更経路を通らないexpression visitorとBinaryExpressionを含まないdeep treeは差が未解決だった。pprofは取得・使用していない。

この文書はproduction patchの採用結果ではない。生成済みswitchへ診断分岐を重ね、同一binary内でlegacy/bound pathを切り替えた要因分離である。次にgeneratorから同じ形を生成し、実parser ASTで検証する。

## 1. 対象とartifact status

| 項目 | 値 |
|---|---|
| selected repo | `/Volumes/SanDisk1TB/worktree/ast-traversal-bench-design` |
| TypeScript-Go revision | `6843f9b8e26` |
| TSGolint revision | null。今回の対象にTSGolint checkoutはない |
| host | macOS 26.6.2、darwin/arm64、Apple M1、Go 1.25.0 |
| primary | full-tree / mixed / 16,384 nodes |
| controls | full-tree mixed 256、expression mixed 16,384、full-tree deep 4,096、A/A |

候補cloneは `cursor-ast-store-tests`、`binder-rewrite`、`profile`、`flownode`、`lock-design-inv`、`lock-profile`、`store-pr-7`、`store-pr-7-nodeseq-t10`、`store-pr-7-attach-parent-fix`、`store-redesign`、`store-schema-foreach-child`、`store-nolock-exp`、`nolock97`、`putcol-bce`。結果は混ぜていない。

| artifact set | status | 根拠 |
|---|---|---|
| `tsc/before.txt` | stale | revisionとdirty source hashがなく不使用 |
| E1 list fast path | current | clean commitと実AST/end-to-end結果を別文書に保存済み |
| E2 separate-binary | current・診断不採用 | source/binary hashは揃うが、負の対照も動きcode layoutが交絡 |
| E2 same-binary | current・主証拠 | 同一binary・同一code layoutで環境変数だけを切替。identity、patch、rawを保存 |
| E2 parser生成実AST | missing | production生成器変更後に実施 |
| hardware instruction/cycle counters | missing | wall timeの一要因比較で方向を判定。未校正counterで代用しない |
| pprof | unsupported | ユーザー指定により実験手段から除外 |

identityは [identity.json](artifacts/store-named-child-e2-20260918/identity.json)、介入sourceのhashと統合patch hashはidentity、raw・benchstat・runnerは [artifact directory](artifacts/store-named-child-e2-20260918/) に保存した。診断patch本体はproduction変更と誤認しないようcommitへ含めていない。

## 2. 仮説と介入

baselineの `childAt` はchildごとに `nodes[parent].childStart` を読み、`children[start+slot]` を取得し、`nodes[child].kind` を読む。BinaryExpressionのfull-tree pathはmodifier listの後にLeft、Type、OperatorToken、Rightを別accessorで取得する。

candidateはmodifier list処理後にparent headerを一度読み、4要素のchildren spanを作る。各要素値はcallback直前にspanから読み、非ゼロrefは同じStoreのHandleへ復元する。0 refは既存の`childAtSlow(slot)`へ戻すため、nilとexternal childを維持する。訪問順、operator token、early exitは変えない。

最初に別binary比較を行うと、full mixed 16,384は -5.61%だった一方、変更経路を通らないexpression mixedも -1.27%、BinaryExpressionのないdeepも -3.77%となった。巨大なgenerated switch内のcaseサイズが変わり、code layout差が負の対照へ波及したと診断した。この比較はE2への帰属に使わない。

主実験ではlegacyとboundの両方を同じbinaryへ含め、process開始時の環境変数から選ぶglobal branchをBinaryExpression caseへ置いた。両armに同じ追加branchがあり、binary hashとcode layoutは同じである。各cellで両modeをwarmup後、独立processのAB/BAを交互に12 pair、bound/boundのA/Aを12 pair測った。

## 3. 主結果

| visitor / shape / nodes | legacy ns/op | bound ns/op | 差 | benchstat |
|---|---:|---:|---:|---:|
| full-tree / mixed / 256 | 4,213 | 3,941 | **-6.45%** | **p<0.001、n=12** |
| full-tree / mixed / 16,384 | 276,900 | 260,400 | **-5.96%** | **p<0.001、n=12** |
| expression / mixed / 16,384 | 132,200 | 132,900 | unresolved | p=0.291、n=12 |
| full-tree / deep / 4,096 | 138,100 | 139,100 | unresolved | p=0.478、n=12 |

全cell・全sampleで0 B/op、0 allocs/op。visits、logical nodes、edge reads、attribute reads、checksumは各armで一致した。mixed 16,384は4,095個、mixed 256は63個のBinaryExpressionを持つ。中央値差はそれぞれBinaryExpression 1件あたり約4.0 nsと4.3 nsで、サイズ間で整合する。

full mixed 16,384は12 pairすべてboundが速く、pair差は -7.81%〜-4.27%。ABは279.6対261.5 µs、-6.49%、p=0.002。BAは272.9対257.8 µs、-5.56%、p=0.002。full mixed 256も12/12 pairで改善し、AB/BAともp=0.002だった。

負の対照は方向が混在した。expressionは改善5/12 pair、-1.46%〜+1.71%。deepは改善4/12 pair、-1.18%〜+1.11%。A/A controlも全cellで差が未解決だった。

## 4. 正しさと制約

candidateはschema order、early exit、foreign/external childを含む対象テストをlegacy/bound両modeで通した。mixed、deep、expressionの完全trace検証もpassした。診断patchはproductionへ残さない。

spanはcallbackをまたいでparentのstartとlengthを固定する。element ref自体はcallback直前に再読するため同じslotの置換には追従するが、callback中のRestore、Compact、childStart変更は保証しない。E1と同じread-only traversal契約が必要である。

## 5. 診断と次の行動

hot path候補は `ForEachChild → forEachChildSchema(BinaryExpression) → childAt × 4 → parent header / children / child header`。same-binary介入により、parent headerとchildStartの重複loadがmixed full-treeの有意な固定費であることを確認した。allocation driverはなく、両modeとも0 allocationだった。命令数・cache missは測っていないため、削減量やmicroarchitecture原因は断定しない。

次はAST generatorに、複数named childを持つcase用の一括span生成を実装する。まずBinaryExpressionだけへproduction形を生成し、生成再現性、全AST tests、foreign/external/nil/early-exitを確認する。その後 `checker.ts` の実Store AST走査をclean commit間でAB/BA比較する。効果が再現すれば、kindごとのchild数と実入力頻度を使い、2個以上のnamed childを持つcaseへ段階的に広げる。全case一括変更はcode sizeとlayoutを再び交絡させるため行わない。
