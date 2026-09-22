# Pointer AST の accessor 動的頻度

作成日: 2026-09-21。計測レポート。ツリーのコードは変更していない (`-overlay` で計数版に差し替えて実行)。対象 commit は `store-ast` の `f9535ec0b7`。

Store AST の accessor 設計 ([store-api-accessor-budget.md](store-api-accessor-budget.md)、[store-layouts/](store-layouts/README.md)) に重みを与えるために、現行 Pointer AST で「どの accessor が、どの kind に対して、どの phase で何回呼ばれるか」を数えた。

## 1. 結論

1. **回数の 6 割は header のスカラ読みである。** Check で 1 ノードあたり Kind 143.8 回、Parent 49.4 回、Loc 11.5 回、Flags 5.5 回。役割アクセス (typed field 17.1 + 多相メソッド 14.9) の 7 倍ある。accessor 設計で最も効くのは payload の置き方ではなく、**header を読む 1 回の単価**である。
2. **したがって通貨の形が layout の選択より 1 桁重い。** header 読みが毎回 `s.nodes[id]` (bounds check 込みで +5 命令程度) を払う形だと、Check で約 +1,070 inst/node、Check 全体 (約 10,070 inst/node) の **+10% 前後**になる見積り。解決済みの `*NodeHeader` を持ち回る形なら 0。一方、payload を一様配置にするか C 型 (inline `a`/`b`) にするかで変わるのは 32 回/node の読みで、1 回 +3 命令としても **約 +1%**。
3. **kind 多相の accessor は動的にはほぼ単相である。** `Text()` は 98% が Identifier、`Expression()` は 90% が PropertyAccess と Call、`Name()` は上位 3 kind で 68%。静的表引きで十分で、凝った分岐回避は要らない。
4. **「役割を持たない kind」への呼び出しが普通にある。** `Locals()` は 230 万回のうち 60% が locals を持てない kind (Identifier、CallExpression、IfStatement など) に対する呼び出しである。不在の経路も在る経路と同じ単価でなければならない。
5. **typed field は上位 21 個で 80%。** 突出は `BinaryExpression.OperatorToken` (Check で 129 万回) で、ほぼ全部が演算子の kind を見るためだけの読みである。
6. **静的な呼び出し箇所数は順位を誤らせる。** checker の箇所数は `.Parent` 1,163 > `.Kind` 638 だったが、実行回数は Kind が Parent の 2.9 倍。`.AsXxx()` は箇所数では最多 (1,453) だが回数では Kind の 1/6。

## 2. 方法

`_opcount/instrument.go` が `golang.org/x/tools/go/packages` で `./internal/...` を型付きで読み、書き換えたソースと `overlay.json` を作る。`go test -overlay` でそれをビルドする。

| 記号 | 数えるもの | 差し込み方 | kind 別 |
| --- | --- | --- | --- |
| `N:Node.F@where` | `*Node` の field `F` の読み | `n.F` → `OpN(i, n).F` | あり |
| `S:T.F@where` | ast package の struct `*T` の field 読み (`CallExpression.Arguments` など) | `x.F` → `OpF(i, x).F` | なし (T が kind を表す) |
| `M:Node.X()` | `*Node` を receiver とするメソッドの呼び出し | 関数冒頭に計数 | あり |
| `F:ast.X()` | 第 1 引数が `*Node` の ast package 関数 (`IsXxx`、`GetSourceFileOfNode` など) | 同上 | あり |

`@ast` は ast package の中での読み (accessor や utility の本体)、`@out` は binder / checker / printer / transformers など消費側での直接の読みである。

数え方の性質:

- **重複して数える。** `n.Text()` 1 回は `M:Node.Text()` 1、`N:Node.Kind@ast` 1 (switch)、`M:Node.AsIdentifier()` 1、`N:Node.data@ast` 1 (type assert)、`S:Identifier.Text@ast` 1 になる。field の行 (`N:` `S:`) が load の実数、`M:` `F:` の行は API の呼び出し回数で、両者を足してはいけない。§4 の合計は目安である。
- **ソース上の評価回数であって機械語の load 数ではない。** 同じ関数内で `n.Kind` を 3 回書けば 3 と数えるが、元のコードではコンパイラが 1 load にまとめうる。計数はその最適化を壊す。したがって header 読みの回数は load 数の上限である。
- **書き込みは数えない。** 代入の左辺、`++`、`&x.F`、range の左辺は書き換えない。`n.Flags |= x` の読みも落ちる。parser の回数が小さいのはこのためで、parse は評価対象にしない。
- 値 receiver (`node.F` で `node` が struct 値) と generic 関数は書き換えない。件数はわずか。
- `Symbol`、`FlowNode`、`NodeFactory`、`NodeVisitor` なども ast package の struct なので数えられる。ノードではないので §4 では別枠にした。

駆動は `_opcount/zz_opcount_test.go` (`internal/compiler` に overlay で追加)。V1 ベンチと同じ `loadASTBenchmarkProject` を `SingleThreaded` で 1 回走らせ、`NewProgram` / `BindSourceFiles` / `GetSemanticDiagnostics` / `Emit` の境界でカウンタを吐く。Emit は Emit ベンチと同じ JS emit 用 config の別 program で測る。カウンタは atomic。

2 回実行した差: parse / bind / census は 0、emit 0.0001%、check 0.018%。

## 3. 入力

`testdata/fixtures/compiler/tsconfig.json` (TypeScript compiler のソース)。124 ファイル (lib を含む)、**936,205 ノード**。checker.ts は 298,054 ノードで Walk ベンチの基準値と一致する。kind の構成は Identifier 41.0%、PropertyAccessExpression 7.3%、CallExpression 5.6%、TypeReference 4.4%、BinaryExpression 4.1%。

命令数の基準 (メモリ `ast-bench-v1-kpc-baseline`): Check 9.43G inst = 約 10,070 inst/node、Emit 6.50G = 約 6,940 inst/node。Bind ベンチは checker.ts 単体なのでこのレポートの bind (全 124 ファイル) とは母数が違う。

## 4. 類型別の回数 (1 ノードあたり)

| 類型 | parse | bind | check | emit |
| --- | ---: | ---: | ---: | ---: |
| header: `Kind` | 1.32 | 7.91 | **143.80** | **84.08** |
| header: `Parent` | 0.00 | 0.62 | **49.44** | 23.28 |
| header: `data` (type assert と interface 呼び出し) | 0.52 | 3.00 | 30.98 | 11.45 |
| header: `Loc` | 0.05 | 0.01 | 11.46 | 7.61 |
| header: `Flags` | 0.14 | 2.26 | 5.47 | 5.36 |
| header: `id` | 0.00 | 0.00 | 3.89 | 1.62 |
| `AsXxx()` cast | 0.03 | 1.72 | 23.57 | 6.26 |
| typed field (`CallExpression.Arguments` など) | 1.52 | 3.31 | 17.08 | 18.74 |
| kind 多相メソッド (`Name()` `Text()` `Expression()` …) | 0.01 | 1.44 | 14.92 | 4.02 |
| list (`NodeList.Nodes` など) | 0.16 | 0.19 | 1.15 | 1.98 |
| 外付けデータのメソッド (`Symbol()` `Locals()` `GetNodeId` `*Data()`) | 0.00 | 0.47 | 11.46 | 4.32 |
| 同 field (`DeclarationBase.Symbol` など) | 0.00 | 0.22 | 4.04 | 5.77 |
| `ast.IsXxx()` 述語の呼び出し | 0.45 | 2.53 | 72.36 | 55.13 |
| その他の ast 関数 (`FindAncestor` など) | 0.03 | 0.92 | 20.15 | 6.51 |
| 参考: ノード以外の struct (`Symbol.*`、`FlowNode.*`) | 2.18 | 1.53 | 67.03 | 24.58 |

総数は bind 2,474 万、check 4.56 億、emit 2.49 億。

- Kind 読み 1.35 億回 (check) のうち 6,770 万回は `IsXxx()` の中、残りは多相メソッドの switch、親連鎖のループ、消費側の直接比較 (2,150 万回) である。
- `data` 読み 2,900 万回の内訳は `AsXxx()` 2,210 万回と、`Name()` / `Modifiers()` / `*Data()` の interface 呼び出し約 700 万回である。Store ではどちらも消える (cast は無償、dispatch は表引き)。

## 5. 個別の観察

### 5.1 親連鎖

Check の `Parent` 読み 4,630 万回のうち 3,400 万回が ast package の中、つまり utility のループである。

| 関数 | check | emit |
| --- | ---: | ---: |
| `GetSourceFileOfNode` | 9,478,326 | 665,837 |
| `GetRootDeclaration` | 1,818,861 | 276,004 |
| `GetCombinedNodeFlags` | 876,199 | 168,998 |
| `FindAncestor` | 870,904 | 298,693 |
| `GetAssignmentTarget` | 870,901 | 161,266 |
| `GetCombinedModifierFlags` | 610,897 | 27,501 |
| `FindAncestorOrQuit` / `FindAncestorKind` | 362,568 | 128,681 |

`GetSourceFileOfNode` は 82% が EnumMember に対する呼び出しで、`Parent@ast` の 45% も EnumDeclaration と EnumMember が占める。**この入力に固有の偏りである** (巨大な enum を持つ compiler ソース。union のソートで `compareNodes` が宣言どうしを比べる経路と推定、未検証)。`AsSourceFile()` 960 万回も同じ原因。

Store ではノードから SourceFile へは親を辿らず Store から 1 回で引けるので、この 950 万回 × 約 3 段は消える。ただし一般の入力でどの程度あるかはこの計測からは言えない。

### 5.2 kind 多相メソッド

| メソッド | bind | check | emit | check での kind の内訳 |
| --- | ---: | ---: | ---: | --- |
| `Text()` | 448,520 | 4,350,223 | 1,541,781 | Identifier 98.0% |
| `Expression()` | 166,685 | 1,753,576 | 809,274 | PropertyAccess 46.3%、Call 44.1% |
| `Name()` | 374,863 | 1,330,360 | 342,419 | PropertyAccess 44.6%、Parameter 12.3%、FunctionDeclaration 10.7% |
| `Modifiers()` / `ModifierFlags()` | 142,840 | 995,877 / 916,854 | 141,435 | PropertySignature 53.5%、MethodSignature 10.6% |
| `Initializer()` | 17,297 | 983,834 | 194,089 | |
| `QuestionToken()` | 0 | 597,870 | 3,930 | (`HasQuestionToken` 576,458 回経由) |
| `Type()` | 0 | 469,685 | 48,847 | |
| `Arguments()` / `ArgumentList()` | 448 | 362,926 / 419,703 | 126,330 | |
| `TypeArguments()` / `TypeArgumentList()` | 0 | 273,039 / 377,830 | 29,767 | |
| `Body()` | 16 | 216,793 | 993 | |

- `ModifierFlags()` は check で 92 万回。budget 文書 §3 の「header に置く」を支持する。
- `Name()` は bind で 37.5 万回 = 0.40 回/node。宣言 kind は checker.ts で全ノードの 6% なので ([budget 文書](store-api-accessor-budget.md) §6 の表)、前回の Store で測った「1 宣言あたり複数回」と整合する。Pointer 版でも同じ傾向である。

### 5.3 役割の不在

`Locals()` は check で 2,298,806 回。receiver の kind は Block 19.7%、FunctionDeclaration 12.5%、Identifier 10.7%、CallExpression 7.5%、IfStatement 7.3%、BinaryExpression 6.1% と散らばり、**59.9% が locals を持てない kind** である。名前解決が祖先を 1 つずつ上がりながら毎回 `Locals()` を呼ぶため。前回の Store で `locals map[NodeRef]` が Resolve 配下 0.78G inst だったのはこの呼び出しである。

設計への要求は 2 つある。不在を返す経路が安いこと (kind の bitset で門前払いできること)、在る場合も map でないこと。

### 5.4 typed field (消費側での直接の読み)

typed field 全体 (ast package 内の accessor 本体での読みを含み、読む場所 `@ast` / `@out` を区別して数える) では、Check で 452 個が読まれ、上位 21 個で 80% になる。Bind は 323 個中 29 個、Emit は 508 個中 49 個。下の表は消費側 (`@out`) の上位である。

| field | bind | check | emit |
| --- | ---: | ---: | ---: |
| `BinaryExpression.OperatorToken` | 164,563 | **1,292,963** | 498,815 |
| `BinaryExpression.Right` | 53,407 | 267,957 | 192,857 |
| `BinaryExpression.Left` | 119,583 | 252,643 | 222,138 |
| `TypeReferenceNode.TypeName` | 0 | 205,405 | 594 |
| `PrefixUnaryExpression.Operand` | 10,548 | 142,624 | 44,280 |
| `PrefixUnaryExpression.Operator` | 32,945 | 123,461 | 51,445 |
| `ParameterDeclaration.DotDotDotToken` | 26,709 | 114,807 | 41,405 |
| `VariableDeclaration.ExclamationToken` | 0 | 72,468 | 28,416 |
| `PropertyAccessExpression.Expression` | 10,974 | 1,155 | 373,635 |
| `CallExpression.Expression` | 170,556 | 0 | 171,467 |

- `OperatorToken` は子ノードだが、読む目的はほぼ `.Kind` である。Store では演算子の kind を親の行 (header の空き 16bit か data word) に複写すれば、子の header への依存 load が 129 万回消える。token ノード自体は walk の契約と emit のために残す。
- `DotDotDotToken`、`ExclamationToken`、`QuestionToken` (§5.2) は「在るかどうか」だけを問う読みで、flag 1 bit で足りる。
- checker は `PropertyAccessExpression` と `CallExpression` の子を typed field ではなく多相の `Expression()` / `Name()` で読み (§5.2)、emit は typed field で読む。同じ slot に両方の経路から届く必要がある。

### 5.5 外付けデータ

| | bind | check | emit |
| --- | ---: | ---: | ---: |
| `GetNodeId()` | 81 | 3,330,919 | 1,517,692 |
| `Locals()` | 0 | 2,298,806 | 47,961 |
| `DeclarationData()` / `Symbol()` | 134,337 / 41,366 | 951,709 / 897,794 | 134,301 |
| `FlowNodeData()` | 133,897 | 320,850 | 68,404 |
| `BodyData()` / `FunctionLikeData()` | 19,513 | 523,905 | 11,496 |
| `SubtreeFacts()` + `propagateSubtreeFacts()` | 0 | 0 | 3,403,029 |

- `GetNodeId` 333 万回は links を引くための id 取得で、現行は atomic な遅延採番である。Store では `NodeRef` がそのまま密な id なので費用が消える。
- `Symbol()` は 51% が VariableDeclaration、17% が FunctionDeclaration。kind 不問で呼ばれるが、対象は宣言 kind に集中している。
- `SubtreeFacts` は emit 専用で 340 万回。Store に置くなら emit 側の列である。

### 5.6 phase ごとの性格

| | 総数 / node | 特徴 |
| --- | ---: | --- |
| parse | 5.3 | ほぼ factory の書き込みで、この計測の対象外 |
| bind | 24.9 | Kind 7.9、`AsIdentifier` 0.89、`Text` 0.48、`Name` 0.40。読みは少なく、費用は走査と symbol 表にある |
| check | 420 | header 読みが支配。入力の enum の偏りを含む (§5.1) |
| emit | 242 | Kind 84 と `IsXxx` 55。`IsMetaProperty` / `IsDecorator` / `IsHeritageClause` / `IsComputedPropertyName` / `IsForInOrOfStatement` が各 390 万回前後で並ぶ (visitor が 1 訪問ごとに 5 述語を順に試している)。`NodeVisitor.Visit` (visitor の関数 field) の読みが 644 万回 = 6.9 回/node あり、transformer が木を複数回辿っていることを示唆する (pass 数は未確認) |

アクセスを受ける kind は、check では Identifier 17.6% (ノード比 41.0%)、EnumMember 10.6% (同 0.2%、§5.1 の偏り)、SourceFile 8.8%、PropertyAccess 6.5%、Call 6.0%、Block 5.8%。1 ノードあたりでは FunctionDeclaration 約 2,190 回、VariableDeclaration 約 1,090 回、Block 約 875 回が重く、Identifier は 171 回である。

## 6. 設計への含意

1. **通貨は解決済みの header pointer を含むこと。** budget 文書 §7 の `Node{s *Store, h *NodeHeader}` を支持する。`Node{s, id}` で header field を読むたびに `s.nodes[id]` を解決する形は、check の 210 回/node の読みすべてに税をかける。前回の Store の accessor 税 (+7.5G inst) と同じ構造である。links 用の `ref` を通貨に含めるかは `GetNodeId` 3.6 回/node と header 読み 210 回/node の比で決まり、`h` から逆算 (6 命令) でも足りる可能性が高い。
2. **`Kind` は header の先頭 word に置き、`IsXxx` が 1 load + 比較で済むこと。** Kind 読みは他の全 accessor の合計より多い。
3. **payload の配置 (一様 / C 型) は二次的である。** 差が出る読みは 32 回/node。ゲート 1b で測る価値はあるが、結論 1 の 1/10 の話であり、単純な方を既定にしてよい。
4. **多相 accessor は kind → slot の静的表。** 動的にほぼ単相なので分岐予測は問題にならず、二分探索の switch (深さ 6〜9) を 1 回の表引きに置き換える利得がそのまま出る。不在は kind の bitset で先に弾く (§5.3)。
5. **header の空き bit に載せる候補:** syntactic `ModifierFlags` (92 万回)、BinaryExpression の演算子 kind (129 万回)、`?` / `!` / `...` token の有無 (計 約 80 万回)。
6. **SourceFile への到達を O(1) にする。** 入力依存だが、外れても損はない。
7. ゲート 1b の microbench は §4 の比で重み付けする。header スカラ : cast + typed field : 多相 : 外付け = 約 210 : 40 : 15 : 15 (check)。

## 7. 限界

- 入力は 1 プロジェクトで、enum の偏りがある (§5.1)。d.ts 中心の入力 (dom) や JS / JSX では構成が変わる。
- 回数であって費用ではない。1 回の単価は budget 文書 §5 の手順 (objdump) で別に測る。§1 の「+10%」「+1%」は回数 × 仮の単価による見積りである。
- ソース上の評価回数は load 数の上限である (§2)。
- checker 自身の表 (links の map、型の cache) へのアクセスは ast package の外なので数えていない。

## 8. 再現

```sh
cd tsc
OUT=$(mktemp -d)
go run internal/ast/docs/_opcount/instrument.go -out "$OUT" \
    -driver "$PWD/internal/ast/docs/_opcount/zz_opcount_test.go"
OPCOUNT_OUT="$OUT/counts.tsv" go test -overlay "$OUT/overlay.json" \
    -run 'TestOpCount$' -count=1 ./internal/compiler          # 約 2 分 (大半はビルド)
gzip "$OUT/counts.tsv"
python3 internal/ast/docs/_opcount/summarize.py "$OUT/counts.tsv.gz"
```

生データは `_opcount/counts-20260921.tsv.gz` (列は phase、op、kind、count。`census` 行はファイル別・kind 別のノード数)。`_` で始まるディレクトリなので `go build ./...` の対象にならない。
