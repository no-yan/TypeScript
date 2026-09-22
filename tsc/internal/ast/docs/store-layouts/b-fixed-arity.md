# 案 B: kind 別の固定 arity

作成日: 2026-09-20。設計ドキュメントのみ。共通前提は [README.md](README.md)。

## 1. 位置づけ

子 slot の数と並びを kind ごとに固定し、その形を行そのものに持たせる案である。A も slot 数は kind で固定だが、slot は header の外の共有列にあり、`childStart` を経由する。B はこの間接を無くすか、型で表す。

実現の仕方が 2 つある。

- **B1: 型付き slab (主案)。** kind ごとに Go の struct を生成し、kind 別の slice に確保する。上流の `ast_generated.go` の `*Node` を `NodeRef` に置き換えた形。
- **B2: word-packed 可変長行 (上限測定用)。** 1 ファイルを 1 本の `[]uint32` に詰め、`NodeRef` を word offset にする。依存 load は最少だが型が無く、提案には使わない。

以下、断りがなければ B1 を指す。

## 2. データ構造

### B1: 型付き slab

```go
type NodeRef uint32                 // headers の index、密な連番、0 = nil
type ListRef uint32                 // lists の index、0 = nil (空 list とは別)

type nodeHeader struct {            // 24B、noscan
    Kind   Kind                     // int16 (+ 2B 空き)
    Flags  NodeFlags
    Loc    core.TextRange           // pos, end int32
    Parent NodeRef
    data   uint32                   // kind 別 slab 内の index。payload の無い kind は 0
}

// ast.json から生成。現行の struct の *Node を NodeRef に、*NodeList を ListRef に替えたもの
type IfStatement struct {
    Expression, ThenStatement, ElseStatement NodeRef
}
type CallExpression struct {
    Expression, QuestionDotToken NodeRef
    TypeArguments, Arguments     ListRef
}
type Identifier struct {
    Text textRef                    // {off, len uint32}: source text か escape 用の別表を指す。string は置かない
}

type listHeader struct {            // 16B
    Loc        core.TextRange
    start, len uint32               // elems[start : start+len]
}

type Store struct {
    headers []nodeHeader
    lists   []listHeader
    elems   []NodeRef               // 全 list の要素。list ごとに連続
    // 生成: kind ごとに 1 本
    ifStatements    []IfStatement
    callExpressions []CallExpression
    identifiers     []Identifier
    ...
}
```

- token と keyword (payload 0。checker.ts で 8.9%、dom で 16.9%) は slab の行を持たない。header だけで表せる。
- `ModifierList` は `listHeader` に `ModifierFlags` を足した別の型にするか、flags を `lists` と並ぶ列に持つ。要素数は checker.ts で 55、dom で 4,662 (すべて長さ 1)。
- slab に `string` や pointer を置かない。置くとその slab が scan 対象になる。

### B2: word-packed 可変長行

```
words []uint32                      // NodeRef = word offset、0 = nil
node 行: [kind|spare][flags][pos][end][parent] [slot0 … slot(arity-1)] [data words…]
list 行: [kindList  ][len  ][pos][end]         [elem0 … elem(len-1)]
静的表:  shape[kind] = arity | listMask<<4 | dataWords<<20
```

行の長さは kind から決まるので `childStart` も `childLen` も要らない。list は独立した行で、作る時点の長さちょうどで確保する。

## 3. 構築

- 変換器は Pointer AST を post-order で辿り、子を先に確保してから親を確保する。parser が `NewX(children...)` を呼ぶ順と同じで、実際の parser が作れない並び (pre-order) にはしない。
- B1 の確保は 2 回の append である。`s.ifStatements = append(s.ifStatements, IfStatement{...})` と `s.headers = append(s.headers, nodeHeader{..., data: idx})`。
- list は要素を全部確保したあと、`elems` の末尾へ連続して append し、`lists` に `{Loc, start, len}` を足す。長さは確定済みで成長しない。
- parser が後から子を差す場合 (reparser、JSDoc の付与) は、optional な slot が常に存在するのでその場で書ける。
- 生成するもの: kind 別 struct、Store の slab field、変換 switch、`ForEachChild` の switch。後で native parser に載せるときは、同じ member 定義から `Factory.New*` を生成する。変換器が使う確保の中核 (`append` 2 回と list の追記) はそのまま factory の中身になる。

## 4. 読み出し経路

| 操作 | B1 | 依存 load | B2 | 依存 load |
| --- | --- | --- | --- | --- |
| 名前付きの子 | `headers[ref].data` → `s.ifStatements[i].Expression` → `headers[child]` | 2 | `words[ref+H+slot]` → `words[child]` | 1 |
| list の要素 i | slab の `ListRef` → `lists[l]` → `elems[start+i]` → `headers[child]` | 3 | slot → list 行 (要素は同じ行に連続) → `words[child]` | 2 |
| text | slab の `{off,len}` → source bytes | 2 | data word → source bytes | 1 |
| parent | `headers[ref].Parent` | 0 | `words[ref+4]` | 0 |
| 全子走査 | 生成 Kind switch → kind 別の関数 | — | `shape[kind]` の表引きループ (switch 不要) | — |

B1 について:

- 依存 load の段数は Pointer (`n.data` の type assert → field → 子) と同じで、bounds check が 2 回加わる。slab の base は `Store` struct にあり L1 に載る。
- accessor に分岐は無い。A の `childAtSlow` や `listOwner` に当たるものを持たない (foreign edge を行に入れない。§8)。
- `ForEachChild` は上流と同じ「Kind switch から kind 別メソッド」の形で生成する。Kind switch が二分探索になる点 (メモリ `bind-chain-hotpath-analysis`) は Pointer 版も同じなので、相対的な不利にはならない。
- 葉 (51%) は header だけを読んで終わる。Identifier の text を読まない walk なら slab に触れない。

## 5. Footprint

README §2 の分布からの試算 (checker.ts)。

| | B/node | Pointer 比 |
| --- | --- | --- |
| B1 | 37.5 | 44% |
| B2 | 33.5 | 40% |

B1 の内訳は header 24B、slot 1.47 × 4B、data 1.09 word × 4B (大半は Identifier の text 2 word)、list header と要素。

B1 には試算に入っていない固定費がある。`Store` が kind ごとに slice header (24B) を持つので、192 kind で 1 ファイルあたり約 4.6KB になる。1 万ファイルで 46MB。小さいファイルでは slab ごとの初回確保も効く。上流は parser を pool して arena をファイル間で使い回すので、この費用を払っていない。

## 6. 同一性と side table

- B1 の `NodeRef` は密な連番で、`symbol []uint32` や `flow []uint32` をそのまま引ける。A と同じ扱いができる。
- あるいは Pointer 版が `DeclarationBase` を埋め込むのと同じ要領で、symbol や flow を該当 kind の slab struct の field にできる。こうすると A の side map (VS Code で +2.7G 命令) が消える。どちらにするかは walk の範囲外。
- B2 の `NodeRef` は word offset なので疎になる。平均の行が 6〜7 word あり、`ref` で引く列は 6〜7 倍に膨れる。header に ordinal を 1 word 足すか、symbol / flow を data word にするしかない。checker links は密な ordinal が要る。B2 の最大の弱点である。

## 7. 型安全性とメンテナ受容性

- B1 は全部が Go の struct で、field 名と型が残る。型の強さは上流と同じ (上流も `Expression = Node` の alias で、子の kind は型で縛っていない)。delve で field 名つきで見られる。
- 生成物は現行の `ast_generated.go` とほぼ 1 対 1 に対応する。レビューする側は「pointer が index になった」と読めば足りる。
- 残る新しいバグの種類は 1 つで、`NodeRef` を別の Store で引くと panic せずに違うノードが返る。公開 API を `Handle{*Store, NodeRef}` にすれば防げるが、accessor ごとの税が戻る (メモリ `node-header-currency-design`)。ファイル内で完結する binder は `*Store` + `NodeRef` を直接使い、ファイルをまたぐ checker は `Handle` を使う、という切り分けが現実的である。
- B2 は bounds check があるのでメモリ安全だが、型安全ではない。slot 定数の取り違えも、行の途中を指す `ref` も検出できない。FlatBuffers を自作する形で、上流には通らないと見る。

## 8. Transform / Update

`Update*` は Pointer 版と同じ path copying で、B1 では生成 struct の field 比較になる。変わらなかった部分木をどう共有するかは A と同じ問題が残る (parse Store は Freeze 後に書けず、`NodeRef` は Store ローカル)。B では foreign edge を行や accessor に入れない。emit 用の Store だけが別表を持つ形にして、parse / bind / check の読み出し経路に分岐を足さない。詳細は walk の判定後に決める。

## 9. Walk ベンチへの接続

- 新しい package (`tsc/internal/ast` の外) に置き、上流との差分を追加だけにする。
- 生成器は新しいファイル (`generate-go-aststore.ts`) にして `schema.ts` を再利用する。`generate-go-ast.ts` には手を入れない。
- 手書き: `Store`、`nodeHeader`、`listHeader`、確保の中核、`SourceFile` と `JSDocParameterOrPropertyTag` の変換 (前者は handWritten、後者は子の順序が実行時に決まる)。
- 生成: kind 別 struct、slab field、変換 switch、`ForEachChild`。
- 走査 API は `func (s *Store) ForEachChild(ref NodeRef, v func(NodeRef) bool) bool` を主とする。`Handle` 経由の版は 2 本目の計測として足す。

## 10. 仮説と棄却条件

- B1 の仮説: full walk の inst/visit は Pointer と同等 (±5% 程度)。依存 load の段数が同じで、走査コードの形も同じだから。勝つ理由は無い。根拠は段数の比較だけで、実測ではない。
- B1 の棄却条件 (提案): checker.ts と dom の両方で inst/visit が Pointer の +10% を超え、原因が bounds check か slab の間接だと特定できた場合。その場合は C を試す。
- B1 が同等なら、採否は walk では決まらない。GC mark 時間と RSS を e2e で測って決める。flows の `uint32` 化で効いたのは GC が辿るポインタ数だった。
- B2 は「index ベースでどこまで速くなるか」の上限を知る必要が出たときだけ作る。

## 11. 未決事項

- B1 の kind 別 slab の固定費 (§5) をどう抑えるか。候補は、頻度の低い kind をまとめた汎用 slab、parse 後の Compact、Store を pool して slab の容量を使い回す、の 3 つ。汎用 slab は型を失うので避けたい。
- `data` index を header に置く (24B) か、`NodeRef` に kind と slab index を詰めて header 列を無くすか。後者は子の kind を load 無しで得られるが、`NodeRef` が密でなくなり、Flags / Loc / Parent を slab struct に埋め込むことになって kind を問わない header アクセスに switch が要る。現時点では前者。
- text を source の `{off, len}` にした場合の escape 付き識別子の扱い。
- list の `Loc` を `lists` に置くか、slab struct に `{start, len}` を直接埋めて `lists` を無くすか。後者は 1 段浅いが、`NodeList` の同一性 (`Update*` の比較) と nil / 空の区別を別に持つ必要がある。
