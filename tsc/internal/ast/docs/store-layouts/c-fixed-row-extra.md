# 案 C: 固定長行 + extra 列 (Zig 型)

作成日: 2026-09-20。設計ドキュメントのみ。共通前提は [README.md](README.md)。

## 1. 位置づけ

全ノードを同じ長さの行にし、行には payload を 2 word だけ inline する。2 word に収まらない kind の残りと、すべての list を 1 本の `extra []uint32` に逃がす。Zig の `std.zig.Ast` が採っている形である。

- B1 に対する利点: slab が 1 本で済み、kind 別 slab の固定費が無い。payload が 2 word 以下の kind (checker.ts で 69.5%) は子まで依存 load 1 段で届く。
- B2 に対する利点: 行が Go の struct で、`NodeRef` が密な連番のままである。
- 弱点: `extra` の中身は型が無い。payload が 3 word 以上の kind (30.5%) は、hot な 1 slot を除いて 2 段になる。

## 2. Zig の実装と、TypeScript で変わる点

Zig の `std.zig.Ast`:

- `nodes` は `MultiArrayList(Node)` (列ごとの SoA)。`Node` は `tag` (1B)、`main_token` (u32)、`data` (u32 × 2) で 13B。
- `extra_data: []u32` に、2 つに収まらない子と list を置く。`data` の片方が `extra_data` の index になる。
- tag を arity で分ける。`block_two` は文 2 つまでを `data` に inline し、`block` は `extra_data` の範囲を持つ。`call_one` / `call`、`fn_proto_simple` / `fn_proto_multi` / `fn_proto_one` / `fn_proto`、`if_simple` / `if` も同じ。小さい場合が多数派なので、ほとんどのノードが `extra_data` に触れない。
- 読む側は `tree.fullIf(node)` のような関数で、tag の違いを吸収した `full.If` という型付きの view に decode する。`extraData(index, T)` は comptime reflection で struct を `extra_data` から読む。
- 位置は持たない。`main_token` から token 列の `start` を引き、span は first / last token を辿って計算する。

TypeScript (tsgo) でそのまま使えない点:

| Zig | tsgo | 帰結 |
| --- | --- | --- |
| token 列があり、位置はそこから引く | token 列が無く、scanner は都度走る | `pos` / `end` を行に持つ (+8B) |
| tag は内部型で、arity ごとに増やせる | `Kind` は公開 API で checker 全体が switch する | arity 別の Kind は足せない |
| operator は tag、token はノードではない | `OperatorToken`、`QuestionDotToken`、modifier はノードで、`ForEachChild` が列挙する | `BinaryExpression` は Left / OperatorToken / Right の 3 子で 2 word に収まらない |
| list に固有の位置が無い | `NodeList.Loc` を printer と診断が使う。nil と空を区別する (`new X` と `new X()`) | list block に `pos` / `end` を持ち、slot の 0 を nil に充てる |
| parent を持たない | checker が `Parent` を多用する | `parent` を行に持つ (+4B) |
| comptime で extra を型付きに読める | Go に相当する機能が無い | `extra` の読み出しは生成 accessor の定数 offset になる |

## 3. データ構造

```go
type NodeRef uint32                 // nodes の index、密な連番、0 = nil
type extraIdx uint32                // extra の index、0 = nil

type nodeRow struct {               // 28B、noscan
    Kind   Kind                     // int16 (+ 2B 空き)
    Flags  NodeFlags
    Loc    core.TextRange           // pos, end int32
    Parent NodeRef
    a, b   uint32                   // 意味は kind が決める (下の規則)
}

type Store struct {
    nodes []nodeRow
    extra []uint32                  // あふれた payload と list block
}

// extra 内の list block: [len][pos][end][elem0 … elem(len-1)]
```

payload = 子 slot + 非 child の data word (string は `{off, len}` の 2 word)。`a` / `b` の意味は kind ごとに静的に決め、`ast.json` から生成する。

1. **payload ≤ 2**: 宣言順に `a`、`b` へ置く。`ParenthesizedExpression{a: Expression}`、`Identifier{a: textOff, b: textLen}`、`TypeReference{a: TypeName, b: TypeArguments の list block}`。token と keyword は `a` / `b` を使わない。
2. **payload > 2**: `a` に hot な子を 1 つ置き、`b` を `extra` の index にする。残りの payload − 1 word は `extra[b:]` に宣言順で固定長に並べる。hot な子は既定で「最初の必須の子」とし、`ast.json` の指定で上書きできる。`PropertyAccessExpression{a: Expression, b → [QuestionDotToken, name]}`、`CallExpression{a: Expression, b → [QuestionDotToken, TypeArguments, Arguments]}`。
3. **list slot**: 値は list block の `extraIdx`。0 は nil で、空 list は `len = 0` の block を持つ。要素は block 内に連続する。

規則が kind で静的に決まるので、accessor は分岐を持たない。生成されるのは `n.a`、`n.b`、`s.extra[n.b + 定数]` のどれかである。

Zig のように「実際の子の数で形を変える」(`call_one` に当たる shape variant を `Kind` の隣の空き 16bit に持つ) 方式は採らない。accessor ごとに shape の分岐が入り、A で問題になった accessor 税と同じものを作る。§12 に残す。

## 4. 構築

- 変換器は post-order で確保する。親を作る時点で子の `NodeRef` も list の要素もそろっている。
- payload > 2 の kind は、`extra` に payload − 1 word を append してから行を append する。
- list は要素を確保し終えたあと、`extra` へ `[len, pos, end, elems…]` を 1 回で append する。長さは確定済みで成長しない。入れ子の list は内側が先に確定するので、block が混ざることはない。
- parser が後から子を差す場合も、slot は固定位置にあるのでその場で書ける。list を後から伸ばすことはできない (新しい block を作って slot を差し替える)。

## 5. 読み出し経路

| 操作 | 経路 | 依存 load |
| --- | --- | --- |
| 名前付きの子 (payload ≤ 2、または hot slot) | `nodes[ref].a` → `nodes[child]` | 1 |
| 名前付きの子 (あふれた slot) | `nodes[ref].b` → `extra[b+k]` → `nodes[child]` | 2 |
| list の要素 i (slot が `a` / `b`) | slot → `extra[l]` の len → `extra[l+3+i]` (同じ領域) → `nodes[child]` | 2 |
| list の要素 i (slot が extra 内) | `b` → `extra[b+k]` → list block → `nodes[child]` | 3 |
| text | `nodes[ref].a, .b` → source bytes | 1 |
| parent | `nodes[ref].Parent` | 0 |
| 全子走査 | 生成 Kind switch → kind 別に `a` / `b` / `extra` を宣言順に読む | — |

- checker.ts では 69.5% のノードが `extra` に触れない。残り 30.5% も hot な子は 1 段である。
- hot slot の指定が効く。`PropertyAccessExpression` (8.4%)、`CallExpression` (6.2%)、`BinaryExpression` (5.5%) の 3 つで payload > 2 のノードの 3 分の 2 を占める。`BinaryExpression` は `modifiers`、`Left`、`Type`、`OperatorToken`、`Right` の 5 slot で、`a` に置けるのは 1 つだけである。`Left` を置けば `Right` は 2 段になる。
- 全子走査は source order を守る必要があり、hot slot が宣言順の先頭とは限らない (`BinaryExpression` の `modifiers` は `Left` より前)。表引きのループにすると順序の調整が要るので、上流と同じ生成 Kind switch にする。
- Go は `nodes[ref]` と `extra[i]` の両方に bounds check を入れる。

## 6. Footprint

README §2 の分布からの試算で、checker.ts が 35.1 B/node (Pointer の 42%)、dom が 35.6 B/node (43%)。内訳は行 28B、あふれた payload、list block (header 3 word + 要素)。

- 葉の `a` / `b` は Identifier と literal なら text に使えるが、token と keyword (8.9〜16.9%) では 8B が空く。
- 行を 28B にすると 64B の cache line に 2.3 行しか入らない。`Parent` を別の列に出せば 24B (2.7 行) になるが、Pointer 版も `Parent` を行に持つので、まず同条件の 28B で測る。
- Zig は SoA だが、Go に `MultiArrayList` は無く、列を別々の slice にすると 1 ノードの読み出しが複数の bounds check になる。full walk は kind と子の両方を読むので、まず AoS で測る。SoA は結果を見てから検討する。

## 7. 同一性と side table

`NodeRef` は密な連番なので、symbol / flow / checker links の列をそのまま引ける。A と同じ扱いで、B2 の弱点を持たない。線形走査 (`for ref := 1; ref < len(nodes); ref++`) も自明で、`setParent` のような全ノード pass が書ける。

非 child の data は行か `extra` に入るので、A の `scalarValues` などの side map は要らない。

## 8. 型安全性とメンテナ受容性

- 行 (`nodeRow`) は Go の struct で、Kind、Flags、Loc、Parent は型付きで読める。
- `a`、`b`、`extra` の意味は kind 依存で、Go の型では表せない。生成 accessor の定数 offset が正しさを担う。slot を取り違えると、panic せずに別の値が返る。弱点の種類は A の `children` slot と同じで、範囲が「payload > 2 の kind と list」に限られる分だけ狭い。
- 緩和策として、Zig の `full.*` に倣って kind 別の view struct を生成し、`s.IfStatement(ref) IfStatementView` が値で返す形にできる。読む側の型は B1 と同じになるが、view の組み立てが毎回走る (hot path では field 単位の accessor を使い分けることになる)。
- 上流への説明は「Zig のコンパイラが同じ形を使っている」で通る可能性があるが、Go には comptime が無く、`extra` を型付きで読む手段が生成コードしかない。B1 より受容性は低い。

## 9. Transform / Update

B と同じ。`Update*` は path copying で、新しい行と `extra` block を append する。foreign edge は行にも accessor にも入れず、emit 用の Store だけが別表を持つ。list は不変なので、要素が 1 つ変われば block を作り直す (Pointer 版も `[]*Node` を作り直す)。

## 10. Walk ベンチへの接続

- 置き場所と生成器の分け方は B と同じ (新しい package、`generate-go-aststore.ts`)。B1 を先に作る前提なら、変換 switch と `ForEachChild` の生成を layout ごとの出力関数に分けるだけで C を足せる。
- 手書き: `Store`、`nodeRow`、`extra` への確保、`SourceFile` と `JSDocParameterOrPropertyTag` の変換。
- 生成: kind ごとの配置表 (どの member が `a` / `b` / `extra[k]` か)、accessor、変換 switch、`ForEachChild`。
- 走査 API は `func (s *Store) ForEachChild(ref NodeRef, v func(NodeRef) bool) bool`。

## 11. 仮説と棄却条件

- 仮説: full walk の inst/visit は B1 より少なく、Pointer と同等かやや少ない。7 割のノードで slab の間接が無くなるため。cycles は、`extra` が行と別の領域にあるぶん list の多い dom で不利になりうる。根拠は段数の比較だけで、実測ではない。
- C を作る条件: B1 が walk で Pointer に負け、原因が slab の間接だと特定できたとき。B1 が同等なら、C は型の弱さに見合う利得が無いので作らない。
- 棄却条件 (提案): inst/visit が B1 から 5% 以上減らない場合。

## 12. 未決事項

- hot slot の決め方。既定 (最初の必須の子) でよいか、binder / checker のアクセス頻度を数えて決めるか。
- shape variant (実際の子の数で形を変える) を入れるか。`PropertyAccessExpression` は `QuestionDotToken` がほぼ常に nil なので、variant があれば 8.4% のノードが `extra` に触れなくなる。代償は accessor ごとの分岐。静的な規則で測ってから判断する。
- `BinaryExpression` の `modifiers` / `Type` (TS のソースでは通常 nil。用途は未確認) のような稀な slot を side 表へ出して payload を減らすか。出せば `Left` / `OperatorToken` / `Right` の 3 word になるが、2 word には収まらない。
- AoS と SoA。kind だけを見て枝刈りする consumer (checker の一部) には SoA が効く可能性がある。
- text を `{off, len}` にした場合の escape 付き識別子の扱い (B と共通)。
