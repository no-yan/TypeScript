# 呼び出し側が持つ情報を子側で再取得している箇所の一覧（2026-09-16）

対象: `internal/binder/binder.go` と `internal/binder/bindwalk_generated.go`、branch `binder-rewrite`、HEAD `12800fbe4b` に未コミット差分（kind 引数化、LabeledStatement / super() の正確性修正）を加えた状態。source の静的読解のみで、新規計測・コード変更は行っていない。行番号はこの snapshot のもの。

[repeated-access-inventory-20260910.md](repeated-access-inventory-20260910.md) は `*Ref` 版を対象にした旧一覧で、その後の生成 walker 化・kind 引数化で解消した項目を含む。本書は現行 source を改めて数え直したもので、旧一覧の番号とは対応しない。

## 結論

再取得は 3 系統に分かれる。

1. 同じ node のヘッダ行（kind / flags / child slot）を子側で再ロードする。同じキャッシュラインへの L1 ヒットであり、1 箇所の利得は 1〜数命令。数は多いが、単独で計測に出る規模ではない。
2. 宣言経路で名前・modifier・container 情報を再解決する。map 参照（`scalarValues`、`locals`）、modifier リストの再走査、`GetNameOfDeclaration` の複数回実行を含み、こちらは 1 回あたりの費用が大きい。
3. 再取得に見えるが初回取得、または binder 内では閉じないもの。

ただし、これらの大半は pointer 版（`main` の `internal/binder/binder.go`）も同じ回数だけ `node.Kind` や `node.Parent` を読んでいる。pointer 版にない dedupe を Store 版の binder に入れると、比較が「データ構造の差」ではなく「アルゴリズムの差」を測ることになる。採否は次節の基準で決め、**採用するのは Store 層の表現差の是正と、移植時に pointer 版から逸れた箇所の復元だけ** とする。binder の論理を pointer 版より減らす項目（1.1〜1.2、1.4〜1.11、2.1、2.3 の binder 側、2.4〜2.6）は、一覧としては残すが不採用。

付随して確認した事実:

- trace ノート [trace-12800f-20260916.md](trace-12800f-20260916.md) が挙げた 2 件の正確性問題（`checkStrictModeLabeledStatement` の kind、`maybeBindExpressionFlowIfCall` の super 判定）は未コミット差分で修正済み。
- `createFlowCondition`（binder.go:505）は呼び出し元がない死んだコード。
- `debug.Assert(cond)` は Go の通常関数なので、引数式は常に評価される。assert 内の述語呼び出しは release でも走る。

## 費用の前提

| 操作 | 実体 | 費用の性質 |
| --- | --- | --- |
| `KindAt` / `FlagsAt` / `ParentRef` | `s.nodes[id]` の 24B ヘッダ行 1 フィールド | ロード 1 + nil/0 check |
| `At(ref)` = `handleOf` | 同上で kind を読み Handle を組む | ロード 1。`HandleOf(s, ref, kind)` なら 0 |
| `Access*` | ヘッダ行 + `children[start:start+n]` | ヘッダ 1 + 子 n。kind / shape の比較は同じ行なので追加ロードなし |
| `ChildRef(ref, slot)` | ヘッダ 1 + 子 1 | |
| `ListLen` / `ListElem` / `ListRefAt` | `listOwner` 解決 + `lists[i]` | owner 判定分岐が毎回。`TryBindListSpan` で 1 回に |
| `UintValue` / `UintValueAt` | `scalarValues map[uint64]uint64` | hash lookup。Prefix/Postfix の operator、ExportAssignment の isExportEquals |
| `Handle.ModifierFlags` | modifier list を `ListAt` で走査 | 要素ごとに owner 解決 + Handle 構築 |
| `Store.Locals` | `locals map[NodeRef]SymbolTable` | hash lookup |
| `Store.Symbol` | `symbolIdx` 列 + `symbolRefs` | ロード 2 |

## 採用基準と再分類

基準: **pointer 版の binder が同じ論理操作を同じ回数行っているなら、Store 版でも残す。** pointer 版が field 1 つの deref で済ませている操作を Store 版が map や list 走査で行っているなら、Store 層で表現を直す。移植時に pointer 版と評価順が変わった箇所は pointer 版に戻す。binder 側で値を先読みして渡す、走査を融合する、Binder フィールドにキャッシュする、といった pointer 版にない dedupe は不採用。

pointer 版との照合結果（`git show main:tsc/internal/binder/binder.go`、`main:tsc/internal/ast/ast.go`）:

| 項目 | pointer 版の対応する操作 | 判定 |
| --- | --- | --- |
| 1.1 kind 4 重 switch | `bind` switch + `GetContainerFlags` switch + `bindChildren` switch + `ForEachChild` の interface dispatch で 4 回 | 不採用（同数） |
| 1.2 Flow 系の accessor 二重ロード | `expr := node.AsX()` のあと `bindEachChild(node)` → `ForEachChild` で field を再読 | 不採用（同構造） |
| 1.3 単項 operator の map 参照 | `expr.Operator` は struct field。読む回数は Store 版と同じ 2 回 | **採用: Store 層**（field 相当の dense 表現へ）。binder 側の dedupe は不採用 |
| 1.4 Binary operator / left の複数解決 | 各 helper が `OperatorToken.Kind`、`Left` を読み直す | 不採用（同数） |
| 1.5 callee kind 3 回 | `node.Expression.Kind` を各所で読む | 不採用（同数） |
| 1.5 末尾 push/unshift の評価順 | pointer 版 2450 行は `IsIdentifier(name) && isNarrowableOperand(expr) && IsPushOrUnshiftIdentifier(name)` | **採用: 移植復元**（Store 版は text を先に読む） |
| 1.6 `bind` の戻り値 | pointer 版も `bool` を返し、`bindCondition` は `node.Kind` を再読 | 不採用 |
| 1.7 条件式の kind / 親の再読 | 各 helper が `node.Kind`、`node.Parent` を読む | 不採用 |
| 1.8 optional chain の kind 5 回 | 同構造 | 不採用 |
| 1.9 `isOptionalChain` の kind 判定 | `ast.IsOptionalChain` に同じ 4 kind 比較がある | 不採用 |
| 1.10 kind 引数化 | `node.Kind` の deref。既存の `bindChildren(node, kind)` 等はこれと等価なので現状維持、追加はしない | 不採用 |
| 1.11 `At` → `HandleOf` | pointer 版は `*Node` を渡し callee が `.Kind` を読む。ロード数は同じ | 不採用 |
| 1.12 bindContainer の flags RMW | pointer 版も `node.Flags \|= X` を 1551〜1562 行で個別に実行 | 不採用 |
| 1.13 list ループの owner 再解決 | `for _, c := range list.Nodes` で slice header を 1 回読む | **採用: 移植復元**（`bindEach` と同じ span 利用に揃える） |
| 2.1 GetNameOfDeclaration の複数回 | `debug.Assert(isComputedName \|\| !ast.HasDynamicName(node))`（153 行）と `getDeclarationName` が同じく 2 回 | 不採用（同数） |
| 2.2 ModifierFlags の list 走査 | `ModifierList.ModifierFlags` field を parse 時に計算済み（ast.go:154〜157、612〜618）。O(1) | **採用: Store 層**（list ヘッダに flags を持つ）。binder 側の受け渡しは不採用 |
| 2.3 container の kind / Symbol / locals | `container.Symbol()` は同じく複数回。`Locals()` は `LocalsContainerData().Locals` field | **採用: Store 層**（`locals` map を dense 列へ）。kind / Symbol のキャッシュは不採用 |
| 2.4 addDeclarationToSymbol | `node.Kind` を読む | 不採用 |
| 2.5 bindParameter | `node.Parent` を複数回読む | 不採用 |
| 2.6 switch / caseBlock | `node.Parent` と `Clauses.Nodes` を同様に読む。`len(clauses)` は slice なので毎回 0 費用 | 不採用（ループの `ListLen` だけ 1.13 に含める） |
| 3 の各項目 | 同構造 | 据え置き |

照合で新たに見つかった移植時の逸脱:

- **`checkContextualIdentifier` の評価順**（binder.go:1384〜1386）。pointer 版 1303〜1305 行は `!ast.IsIdentifierName(node)` を先に評価し、通らなかった identifier だけ `scanner.GetIdentifierToken` の map を引く。Store 版は map を先に引き、keyword だったときだけ `IsIdentifierName` を見る。プロパティ名・メソッド名など `IsIdentifierName` が真になる identifier では、pointer 版は map を引かない。**計測の結果、不採用**（下記「計測結果」）。pointer 順は checker.ts で関数単体 +26% 遅く、Store 版の順序の方が速い。つまり Store 版はここで pointer 版にない最適化を既に持っている。公平性を保つ選択肢は、Store 版を pointer 順に戻して bind 時間で約 3% を手放すか、pointer 版にも Store の順序を入れるかの 2 つで、後者を推す。trace ノートの第一候補（10 語 switch）も両版同時に入れる場合だけ検討する。

採用する 5 件をまとめると次の通り。

| 種別 | 変更 | 変更先 |
| --- | --- | --- |
| Store 層 | Prefix/Postfix の operator（と ExportAssignment の isExportEquals）を `scalarValues` map から dense 表現へ | ast Store / parser / generator |
| Store 層 | modifier list の flags を `listHeader` に持ち、`Handle.ModifierFlags` を O(1) にする | ast Store / parser |
| Store 層 | `locals` map を `symbolIdx` と同じ dense 列に | ast Store |
| 移植復元 | `bindCallExpressionFlow` 末尾の評価順を pointer 版に揃える | binder.go:2555〜2562 |
| 移植復元 | 残る list ループを `TryBindListSpan` に揃える | binder.go 1.13 の各行 |

`checkContextualIdentifier` の評価順復元は計測で棄却した（次節）。

## 計測結果: `checkContextualIdentifier` の評価順を pointer 版に戻す

2026-09-16、Apple M1 / 8GB、Go 1.27.1 darwin/arm64、`GOMAXPROCS=8 GOGC=100 GOMEMLIMIT=off`。base は現行作業ツリー、head は 1384 行の条件に `&& !ast.IsIdentifierName(b.store.At(node))` を移し、1386 行の `|| ast.IsIdentifierName(...)` を外したもの。base は overlay で同一ソースから作成。artifact は [artifacts/contextual-identifier-order-20260916/](artifacts/contextual-identifier-order-20260916/)。fixture は checker.ts / dom.generated.d.ts（SHA は `manifest.json`）。

仮説: pointer 順はプロパティ名で map を引かないので速い。**結果: 棄却。**

**識別子の分類**（`identifier_classes_scratch_test.go.txt`、1 AST を走査して数えたもの）:

| checker.ts | base | head |
| --- | ---: | ---: |
| 識別子（Ambient / JSDoc 除く） | 126,454 | 126,454 |
| `GetIdentifierToken` の map を引く | 62,276 | 49,004 |
| `IsIdentifierName` を呼ぶ | 8,795（keyword 命中時のみ） | 126,454（全識別子） |

dom.generated.d.ts は 39,250 識別子が全て Ambient で、この関数の先頭で抜けるため両版とも 0 回。この入力は本項目の対照にならない。

head は map を 13,272 回減らす代わりに `IsIdentifierName`（親ヘッダ + polymorphic `Name()` + 子ヘッダ）を 117,659 回増やす。

**関数単体 microbench**（`BenchmarkScratchContextualIdentifier`、非 Ambient 識別子全件に対して `checkContextualIdentifier` だけを回す。診断が増えないことを検証済み。base/head 交互 10 round、`-benchtime=200x`）:

| | base | head | 差 |
| --- | ---: | ---: | ---: |
| checker.ts sec/op | 2.512 ms ± 28% | 3.159 ms ± 21% | **+25.8% (p=0.007, n=10)** |
| 識別子 1 つあたり | 19.9 ns | 25.0 ns | +5.1 ns |

差分から逆算すると、map 1 回 ≈ 25 ns と置いて `IsIdentifierName` 1 回 ≈ 8 ns。map を減らせる識別子が全体の 1 割で、親判定を全識別子に足す方が高くつく。

**bind 全体**（`BenchmarkBindInvestigation`、10 AST batch、交互 10 round）: natural GC は ±214% で判定不能。GC off は checker.ts base 18.65 ms / head 20.02 ms（p=0.247）、中央値の向きは microbench と一致するが有意ではない。0.65 ms は checker.ts の bind 約 18.6 ms の 3.5% に相当する。

**実ワークロード**（VS Code `--project src/tsconfig.json --noEmit --noCheck`、base/head 交互 6 round）: singleThreaded の user 中央値は base 3.39 s / head 3.22 s、parallel の wall は 1.2〜3.4 s と ばらつき、いずれも差は雑音内。VS Code は .d.ts の比率が高く、識別子 1 つあたり 5 ns の差は総量で数十 ms、8GB 機の paging 雑音（±10%）の下に埋もれる。実ワークロードでは判定できない。

**判断**: Store 版の現行順序を維持する。pointer 版との公平性を保つには、pointer 版にも Store の順序（keyword map → 命中時のみ `IsIdentifierName`）を移植する方が、Store 版を遅い順序に戻すより良い。pointer 版でも `IsIdentifierName` は親 deref + switch + field 比較で、map の hash より安いとは限らないため、移植前に同じ microbench を pointer 版で取る。

## 1. 同じ node の kind / accessor / flags を子側で取り直す

以下は再取得の事実と、仮に binder で直すならの方法を記録したもの。判定は上表を優先する。

### 1.1 kind の 4 重 switch（構造）

`bind` の宣言 switch（657）、`GetContainerFlags`（790 → 2664）、`bindChildren` の switch（1710）、`forEachBindChildGenerated` の switch（bindwalk_generated.go:9）が、同じ kind で 4 回 dispatch する。呼び出し側が確定した kind を、子側が switch で引き直す構造的な二重化。

修正: hot な kind（Identifier、PropertyAccess/ElementAccess、Call、Binary、VariableDeclaration、Parameter）は、宣言処理と子走査を 1 関数に融合し `bind` から直接呼ぶ。残りは従来の汎用経路に残す。`bind` の epilogue（`seenParseError` の save/restore、`ThisNodeOrAnySubNodesHasError`）は helper に切り出して融合関数からも通す。[bind-hotpath-chain-analysis-20260914.md](bind-hotpath-chain-analysis-20260914.md) の提案と同じ方向で、以下 1.2〜1.5 の多くはこの融合で同時に消える。

### 1.2 Flow 系関数の accessor 二重ロード

| 関数 | accessor 取得 | 再ロード箇所 |
| --- | --- | --- |
| `bindPrefixUnaryExpressionFlow` | 2279 | `bindEachChild` → bindwalk_generated.go:296 |
| `bindPostfixUnaryExpressionFlow` | 2297 | 同 299 |
| `bindDeleteExpressionFlow` | 2388 | 同 411 |
| `bindVariableDeclarationFlow` | 2422 | 同 96（順序は逆で、walker が先） |
| `bindCallExpressionFlow` | 2534 | 同 350 |

修正: `bindEachChild` をやめ、手元の accessor から `b.bind(a.X)` / `b.bindEach(a.L)` を生成 walker と同じ順序で並べる。順序を変えないことが条件。

### 1.3 単項演算子の operator を map で 2 回引く

`checkStrictModePrefixUnaryExpression`（1473）と `bindPrefixUnaryExpressionFlow`（2280）がそれぞれ `At(node).PrefixUnaryExpressionOperator()` を呼び、実体は `scalarValues[key]` の hash lookup（store.go:1116）。Postfix も同じ（1469、2298）。さらに `isNarrowingExpression`（2721）、`IsLogicalExpression`、`isTopLevelLogicalExpression`（2873）が `!` 判定のたびに同じ map を引く。

修正（binder 内）: 1.1 の融合関数で 1 回だけ読み、両者に渡す。`At(node)` 経由ではなく `UintValueAt(node, slot)` を使えば Handle 構築も省ける。
修正（Store 側）: operator を Binary と同様に子 slot のトークン node にするか、dense 列へ移す。schema 変更なので binder 単独では閉じない。node 1 つ 24B 増と map entry 1 つの交換になり、採否は別途計測が要る。

### 1.4 BinaryExpression の operator kind と left を 4〜5 回解決

| 箇所 | 読むもの |
| --- | --- |
| `GetAssignmentDeclarationKind`（679） | operator token kind、left kind、left.Expression |
| `checkStrictModeBinaryExpression`（1451） | `AccessBinaryExpression`、left Handle、operator kind |
| `IsDestructuringAssignment`（1746） | operator token kind（1〜2 回）、left kind |
| `bindBinaryExpressionFlow`（2326〜2327） | `AccessBinaryExpression`、operator kind |
| `bindLogicalLikeExpression`（2368〜2370） | `AccessBinaryExpression`、operator kind |
| `bindDestructuringAssignmentFlow`（2306） | `AccessBinaryExpression` |
| `bindAssignmentTargetFlow(expr.Left)`（2356 → 1887） | left kind |

修正: `bind` の BinaryExpression case で `expr := AccessBinaryExpression(node)`、`opKind := KindAt(expr.Operator)`、`leftKind := KindAt(expr.Left)` を 1 回取り、上記すべてへ渡す。`bindLogicalLikeExpression` と `bindDestructuringAssignmentFlow` は accessor を引数で受ける。`GetAssignmentDeclarationKind` は ast 側の Handle API なので、binder ローカルに (opKind, left) を受ける版を置くか、TS ファイルでは `IsInJSFile` で早期に抜けることを利用して呼び出し自体を JS 限定にする。

### 1.5 CallExpression の callee kind を 3 回

`SkipParentheses`（2543）が callee の kind を読み、super 判定（2550）と PropertyAccess 判定（2555）が `KindAt(call.Expression)` を再読。加えて `maybeBindExpressionFlowIfCall`（2250〜2251）が親 statement 側から同じ callee を再取得する。

修正: `calleeKind` をローカルに 1 回。statement 側の再取得は node 境界を越えるため、1.6 で `bind` が kind を返すようにしても callee までは戻せない。これは残す。

末尾（2557〜2559）は `TextAt(access.Name)` を narrowable 判定より先に評価している。upstream は kind → narrowable → text の順。修正: 順序を upstream に揃える。

### 1.6 bind の戻り値が捨てられている

`bind` は常に `false` を返し（805）、`bindCondition`（1865〜1866）と `bindOptionalExpression`（2506〜2507）は直後に `KindAt` / `FlagsAt` を読み直す。

修正: 戻り値を `Kind` に変える。`doWithConditionalBranches` の関数値シグネチャも同時に変更。register 1 本の書き込みで済む。

### 1.7 条件式 1 つで kind を約 6 回、親ヘッダを約 3 回

`bindCondition`（1859〜1870）→ `isLogicalAssignmentExpression` / `IsLogicalExpression` / `IsOutermostOptionalChain` → `addConditionAntecedents`（533 の `KindAt`、535 の `IsExpressionOfOptionalChainRoot` と `IsNullishCoalesce(At(ParentRef))`）→ `isNarrowingExpression`。各 `At(node)` が kind を、各親 query が親ヘッダを読み直す。

修正: `HandleOf(store, node, kind)` を 1 回作り、親 Handle も 1 回取って binder ローカルの判定に置き換える（`parent := h.Parent(); IsOptionalChainRoot(parent) && parent.Expression() == h; IsNullishCoalesce(parent)`）。ast 側の述語 API は変えない。

### 1.8 optional chain 経路で同じ node の kind を 5 回

`bindOptionalChainFlow`（2460 `At`）→ `bindOptionalChain`（2489 `At`、2492 `KindAt`、2493 `expressionRefGenerated`、2499 `At`）→ `bindOptionalChainRest`（2515 `KindAt` + `Access*`）。callee slot も 2493 と 2517/2521/2525 で 2 回。

修正: kind を引数で下ろし、Access* を 1 回取って expression の bind と rest の bind の両方に使う。`bindOptionalChainRest` は関数値渡しなので `doWithConditionalBranches` の 4 行をインライン化する。

### 1.9 `isOptionalChain` の kind 判定が冗長

`bindAccessExpressionFlow`（2450〜2451）、`bindCallExpressionFlow`（2535〜2536）、`bindNonNullExpressionFlow`（2566〜2567）は dispatch 済みの kind を再度 4 kind と比較する。

修正: `FlagsAt(node)&NodeFlagsOptionalChain != 0` だけにする。

### 1.10 kind を持つ呼び出し側から kind なしで呼ばれる関数

| 関数 | 再取得 | 呼び出し側の kind |
| --- | --- | --- |
| `bindClassLikeDeclaration`（1013） | `KindAt(node)` | `bind` 740 |
| `bindVariableDeclarationOrBindingElement`（1243） | `KindAt(node)` | `bind` 709/712 |
| `bindFunctionExpression`（983、985） | `KindAt(node)`、`AccessFunctionExpression` 2 回 | `bind` 738 |
| `checkStrictModeFunctionName`（1437） | `KindAt(node)` | 984、1292（後者は定数） |
| `bindForInOrForOfStatement`（2006） | `KindAt(node)` | `bindChildren` 1718 |
| `bindInitializedVariableFlow`（2433） | `KindAt(node)` | 2427（VariableDeclaration 確定） |
| `bindAssignmentTargetFlow`（1887） | `KindAt(node)` | 2012、2357、2390、1919〜1925 が直前に同じ kind を読む |

修正: いずれも kind を引数に追加。`bindAssignmentTargetFlow` は kind を知らない呼び出し元（2291、2301、1893 以降の再帰）が `KindAt` して渡す。

### 1.11 `b.store.At(node)` で kind が既知の箇所

`handleOf` は kind をヘッダから読み直す（store.go:97）。`IsDestructuringAssignment`（1746）、`GetAssignmentDeclarationKind`（679、744）、`IsPotentiallyExecutableNode`（1698）、`IsAsyncFunction`（978、1047、1289）、`isNarrowableReference`（675）、`isTopLevelLogicalExpression`（2329、2460）など binder.go 内に多数。

修正: kind がスコープにある箇所は `ast.HandleOf(b.store, node, kind)` へ機械的に置換。1 箇所の利得は 1 ロードなので、単独では計測に出ない可能性が高い。1.1 の融合に含めて評価する。

### 1.12 bindContainer の flags 読み書き

1592、1598、1600、1606、1609 で `SetFlagsAt(node, FlagsAt(node)|…)` を連続実行し、毎回 `mustMutate` も通る。`bindChildren` の後で再帰はない。

修正: ローカルに 1 回読んで OR し、最後に 1 回書く。`bind` の 786/802 も同様に flags を 1 回読んで共有できる。

### 1.13 list ループの ListLen / owner 再解決

1890、1900、2181、2201、2204、2441、968、1650、1269 は条件式で毎回 `ListLen` を呼び、要素取得も毎回 owner 解決する。

修正: `bindEach`（1789）と同じ `TryBindListSpan` パターンに揃える。foreign list は fallback へ。

## 2. 宣言経路で名前・modifier・container 情報を取り直す

### 2.1 GetNameOfDeclaration が宣言 1 件で 2 回、export メンバでは 4 回

`declareSymbolEx` の `debug.Assert(isComputedName || !HasDynamicName(At(node)))`（168）は `isComputedName` が false の通常経路で常に `HasDynamicName` → `GetNameOfDeclaration` を走らせる。続く `getDeclarationName`（178 → 329）が同じ名前を再解決する。`declareModuleMember`（432〜433）は local と export で `declareSymbol` を 2 回呼ぶので倍。呼び出し前にも `bindPropertyOrMethodOrAccessor`（1053）と `bindVariableDeclarationOrBindingElement`（1243）が名前を解決している。

修正: `nameRef` と `nameKind` を `bind` の case で 1 回解決し、`declareSymbolEx` に渡す。assert は `isComputedName || nameKind != KindComputedPropertyName || literal 判定` の形へ書き換える。名前文字列の memo 化はしない。trace ノート「次点 1」と同じ対象。

### 2.2 ModifierFlags のリスト走査が宣言 1 件で 3〜5 回

| 箇所 | 用途 |
| --- | --- |
| `IsAsyncFunction`（978、1047、1289） | Async |
| `GetCombinedModifierFlags`（401） | Export |
| `HasSyntacticModifier(Default)`（169、424） | Default |
| `bindContainer` IIFE 判定（1565） | Async |
| `IsStatic`（441、1225） | Static |

各回 `ListAt` で要素ごとに owner 解決 + Handle 構築（store_query_manual.go:226）。`export async function` は 4〜5 回走査する。

修正（binder）: `bind` の case で `ModifierFlags` を 1 回計算し、`isDefaultExport` / `hasExport` / `isAsync` を宣言経路に渡す。
修正（Store 側）: `listHeader`（store.go:150）は 16B で、modifier list は node 数より桁違いに少ない。parse 時に計算した flags を list ヘッダへ持たせると走査自体が消える。upstream の pointer AST が `ModifierFlagsCache` を持つのと同じ役割。

### 2.3 container の kind / Symbol / locals を宣言ごとに再取得

`declareSymbolAndAddToSymbolTable`（456）は `KindAt(b.container)`、`bindBlockScopedDeclaration`（1320）は `KindAt(b.blockScopeContainer)`、`declareModuleMember` は `Symbol(container)` を最大 4 回（404、425、433）、`declareClassMember`（442、444）は 2 回、`getLocals`（146）は `locals` map を引く。container は `bindContainer` でしか変わらない。

修正: `containerKind` / `containerSymbol` / `containerLocals` / `blockScopeContainerKind` を Binder フィールドに持ち、`bindContainer` の save/restore（1520〜1522、1664〜1666）に同居させる。注意点は 2 つ。`getLocals` が lazily 作る table をキャッシュへ反映すること、`bindDeferredExpandoAssignments`（1104〜1105）が container を直接差し替えるので同時に更新すること。

### 2.4 addDeclarationToSymbol

2627、2629、2640 で `At(node)` を計 2〜3 回、2631 で `KindAt(node)`。呼び出し元（`declareSymbolEx`、`bindAnonymousDeclaration`、`bindFunctionOrConstructorType`）は kind を持つか定数。

修正: Handle か kind を引数で渡す。

### 2.5 bindParameter

1266 と 1280 で `ParentRef` を 2 回、1267 と 1281 で `KindAt(parent)` を 2 回、1261 と 1281 で `FlagsAt(node)` を 2 回、1265 で `KindAt(parameter.Name)` を 2 回。さらに `bindParameterFlow`（2589）が accessor を再ロードし、declare 経路が Name を再解決する。

修正: ローカル変数化と 2.1 の名前受け渡し。1.1 の融合対象。

### 2.6 bindSwitchStatement / bindCaseBlock

| accessor | 1 回目 | 2 回目 |
| --- | --- | --- |
| `AccessSwitchStatement` | 2170 | 2197（`ParentRef` 経由） |
| `AccessCaseBlock` | 2179 | 2198 |
| `AccessCaseOrDefaultClause` | 2204 | 2229 |

修正: switch の Expression を Binder フィールド（`preSwitchCaseFlow` と同じ扱い）で渡せば `ParentRef` と accessor 再ロードが消える。default 検出の別ループ（2181）は upstream も同じ形なので優先度は低い。

## 3. 修正しない、または binder 内では閉じないもの

- **子 node の kind を `bind(child)` 内で読む。** 生成 walker が `bind(a.Left)` を呼ぶたびに子側で `KindAt` する。親 accessor は子の ref しか持たず kind は持たないので、これは再取得ではなく初回取得。例外は `bindEachStatementFunctionsFirst`（1813、1819、1827、1833）で、2 pass で kind を読んだあと `bind` が 3 回目を読む。`bindWithKind(node, kind)` を足せば消せる。
- **Block の `GetContainerFlags` が親 kind を読む**（2698）。呼び出し側の親は自分の kind を知っているが、`bind` に親 kind を通す方が全 node で高くつく。Block 1 つあたり 1 ロードで据え置き。
- **Identifier の親判定**（`IsIdentifierName`、1386）。親 kind を渡せない事情は上と同じ。trace ノートの第一候補（10 語 switch）の方が先で、そちらは親 query 自体を省く。
- **`isNarrowableReference` のチェーン長 2 乗**（675、2739〜2755）。`a.b.c.d` で各段が下位を全部歩き直す。upstream も同型で、top-down 走査では下から memo できない。解決するなら parser が生成時に bottom-up で NodeFlags に 1 bit 立てる案だが、trace ノートが後回しにした parse-time flag と同じ判断になる。
- **Access\* の kind 検査**（store_accessors_generated.go:1514 など）。childStart を得るために同じヘッダ行を必ず読むので、kind 比較は追加ロードではなく 1 比較。取り除く価値はない。
- **`GetAssignmentDeclarationKind` と `IsInJSFile` の flags 二重読み**（744〜750）。TS ファイルでは最初の `IsInJSFile` で抜けるので JS 限定の話。据え置き。
- **`maybeBindExpressionFlowIfCall` の callee 再取得**（1.5）。node 境界を越えるため、CallExpression 側の情報を statement 側へ戻す仕組みがない。Binder フィールドで戻す案はあるが、1 文につき 2 ロードのために状態を増やす価値はない。

## 次の検証単位

採用基準に従い、binder の論理は pointer 版のまま保つ。

1. **移植復元 2 件**（push/unshift の評価順、list ループの span 化）を 1 commit にまとめ、既存の binder 回帰と比較 harness の digest で意味が変わらないことを確認したうえで、寿命安全な実入力 bind ハーネスで checker.ts / dom.generated.d.ts の ns/op・B/op・allocs/op と KPC 命令数を base と比較する。効果は小さい見込みなので、wall ではなく命令数で判定する。`checkContextualIdentifier` の順序復元は計測で棄却済み。
1. **pointer 版側の対応**: pointer 版に Store の `checkContextualIdentifier` 順序を移植する案を、pointer 版で同じ microbench を取ってから判断する。
2. **Store 層 2 件**（modifier flags を list ヘッダへ、`locals` を dense 列へ）。parser / generator の変更を伴うので、binder 以外の利用者（checker、LS）の回帰も走らせる。
3. **Store 層 1 件**（単項 operator の dense 化）。node 24B 増と map entry の交換になるため、採否は B/op と bind 時間の両方で判断する。

不採用とした dedupe 項目は、pointer 版と Store 版の両方に同じ変更を入れる場合にだけ再検討する。片側だけに入れて比較しない。

throughput checkpoint: n/a, read-only inventory
