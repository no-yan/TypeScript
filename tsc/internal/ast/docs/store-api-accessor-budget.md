# Store AST の読み出し API と accessor 予算

作成日: 2026-09-21。設計ドキュメントのみ。コードは変更しない。

> 2026-09-22 追記: layout の主案が変わり、コード例の B1 の typed view は `{s, h}` の型の付け替えに置き換わる。規則と予算は有効。決定事項は [store-ast-design-20260922.md](store-ast-design-20260922.md)。

layout の候補は [store-layouts/](store-layouts/README.md) にある。この文書は layout の上に載せる読み出し API の形と、accessor 1 回あたりの予算、その確認手順を決める。対象は parse / walk / bind まで。checker の通貨は §7 に制約だけを書く。

次の 3 点は [B1](store-layouts/b-fixed-arity.md) の記述をこの文書が置き換える: `ForEachChild` の引数 (§9 の `ref` → `h`)、`ModifierFlags` の置き場所 (§2 の list header → node header)、checker の通貨 (§7 の `Handle` → §7 の制約)。C の §10 も同じ。

コード例は B1 (型付き slab) で書く。C でも §2 の規則と §4 の予算は同じで、typed view が `a` / `b` / `extra` の生成 accessor に変わるだけである。

## 1. 前回の Store から引き継ぐ事実

前回の bind は Pointer の 1.79 倍の命令を実行し、IPC は Pointer より高かった (3.93 対 3.10)。メモリ待ちではなく、余分な命令が原因である。したがって依存 load の段数だけを設計基準にすると、前回の設計は合格してしまう。基準は「アクセス回数 × 1 回の命令数」に置く。

| 前回の原因 | 実測 | この API での扱い |
| --- | --- | --- |
| accessor ごとの税 (nil ガード、bounds、kind / shape 検査、Handle 構築、kind dispatch) | VS Code で +7.5G inst | §2 規則 1〜3 |
| 宣言経路で名前を 2.3 回 / 宣言 解決、`At` 8 回 / 宣言 | dom +14% inst | §2 規則 4 |
| `ModifierFlags` が dispatch 経由 | dom 32.9k 回 | §3 header に置く |
| side map (`map[NodeRef]`) | VS Code で +2.7G inst | §6 |
| 箇所単位の微調整 (`-B`、識別子 fast path、Access* の整理) | どれも 1〜6%、多くはノイズ以下 | やらない。回数を減らす形だけを採る |

静的な数え上げは前回、実際の増分の 1/4 しか説明できなかった。この文書の命令数も、§5 の手順で実物に置き換えるまでは目安である。

## 2. 規則

1. **辺は `NodeRef`、手元では `*NodeHeader`。** 行に保存する子・親は `NodeRef` (4B) だが、読む側は 1 ノードにつき 1 回だけ `Header(ref)` で pointer に解決し、以後の header field はその pointer から読む。`s.Kind(ref)` のような「ref を受けて field を返す」accessor は作らない。作ると field を読むたびに bounds check と添字計算を払う (§4)。
2. **kind が分かっている場所では typed view を使う。** `bindKind` の `case KindCallExpression:` の中なら `d := s.CallExpression(h)` を 1 回取り、`d.Expression`、`d.Arguments` は field 読みにする。Pointer 版の `n.AsCallExpression()` と同じ形で、費用も同じである (§4)。
3. **header の accessor にガードを置かない。** `headers[0]` を番兵行にする (`Kind = KindUnknown`、`Parent = 0`)。`Header(0)` は有効なので、header を読む accessor は nil 分岐を持たない。**list は例外**: 要素列を切り出す前に `at != 0` で弾く。番兵 block で無条件に切り出す形は、nil list が多数 (Walk で list slot の 60%) のため inst で損だった (2026-09-22、設計文書 §2.3)。nil かどうかの意味が要る場所だけ、呼ぶ側が `ref == 0` を見る。これは Pointer 版が `n != nil` を書いている場所と同じである。
4. **kind を問わない `Name` は遅い道として扱う。** Pointer 版の `Name()` と `*Data()` 系は interface 呼び出し 1 回だが、Store は Kind switch (二分探索で深さ 6〜9) になる。走査の経路では使わない。宣言経路では 1 宣言につき 1 回だけ解決し、結果を `(ref, h, name)` の引数で下へ渡す。`Text`、`Type`、`Expression` は Pointer 版も Kind switch + type assert なので、Store で形は変わらない。
5. **typed view は kind を検査しない。** Pointer 版の type assert は取り違えで panic するが、Store は別の行を黙って返す。`const storeChecks = false` で囲った assert を生成し、test と CI だけ true で回す。release build の費用は 0 である。

## 3. API

```go
type NodeHeader struct {                // B1 §2 の nodeHeader を公開し、空き 2B を使う
    Kind          Kind                  // int16
    modifierFlags uint16                // syntactic ModifierFlags (bit 0〜15)
    Flags         NodeFlags
    Loc           core.TextRange
    Parent        NodeRef
    data          uint32                // kind 別 slab の index
}

func (s *Store) Header(ref NodeRef) *NodeHeader                       { return &s.headers[ref] }
func (h *NodeHeader) ModifierFlags() ModifierFlags                    { return ModifierFlags(h.modifierFlags) }
func (s *Store) CallExpression(h *NodeHeader) *CallExpression         { return &s.callExpressions[h.data] } // 生成、kind ごと
func (s *Store) List(l ListRef) []NodeRef                             // elems[start : start+len]
func (s *Store) ListLoc(l ListRef) core.TextRange
func (s *Store) IdentifierText(h *NodeHeader) string                  // source の部分文字列。確保しない
func (s *Store) ForEachChild(h *NodeHeader, v func(NodeRef) bool) bool // 生成
func (s *Store) Name(h *NodeHeader) NodeRef                           // 生成、Kind switch。規則 4
```

- `modifierFlags` は B1 の header にあった 2B の空きを使う。`ModifiersToFlags` が立てるのは bit 0〜15 (`Public` 〜 `Decorator`) だけで、ちょうど収まる。Pointer 版の `n.ModifierFlags()` (interface 呼び出し + nil 検査 + load) より安く、kind を問わず 1 load で読める。bit 16 以上はどこにも保存されていない (`Deprecated` は `NodeFlagsPossiblyContainsDeprecatedTag` から都度導出、JSDoc の `@private` などは reparser が合成 modifier ノードにする) ので、16bit で足りる。`SetModifiers` で list を差し替える経路 (`parser/reparser.go:564`、`transformers/declarations/transform.go:2674`、`:2855`) は header の flags も書き直す。
- `ForEachChild` は `ref` ではなく `h` を受ける。visitor はノードに入った時点で `Header(child)` を取っているので、`ref` を渡すと `ForEachChild` の中でもう 1 回解決することになる。純粋な walk は次の形になる。

```go
func (w *walker) visit(ref NodeRef) bool {
    h := w.s.Header(ref)                // 1 ノードにつき 1 回
    w.visits++
    return w.s.ForEachChild(h, w.visitFn)
}
```

- binder は `bind(ref NodeRef, h *NodeHeader)` のように両方を持ち回る。`ref` は symbol / flow の列を引くのに要り、`h` は field を読むのに要る。
- binder が `h.Flags |= ...` と書くのは許す。ファイル単位で直列なので競合しない。

### 借用の契約

`*NodeHeader` と typed view の pointer は `headers` / slab の内部を指す。`append` で再確保されると無効になる。

- parser は pointer を確保をまたいで持たない。書き込みは `s.headers[ref].X = ...` をその場で行う。
- parse 完了後 (Freeze / Compact の後) は再確保が起きないので、binder と checker は自由に持ってよい。Compact を入れるなら、その前に pointer を全部手放す。

## 4. 予算と実測

2026-09-22 に `internal/ast/storeexp` (Pointer AST から変換した Store、この文書の API の形) で KPC を取った。詳細は [storeexp-verification-report-20260922.md](storeexp-verification-report-20260922.md)。以下、「静的」は `go tool objdump` で hot path (panic 側を除く) を数えた命令数、「KPC」は checker.ts 上の microbench で baseline (slice を読むだけのループ) を引いた inst/access の中央値 (幅 ≤ 0.14%)。静的と KPC は ±1 命令で一致した。元の予算は「予算」列に残す。

| 操作 | Pointer 静的 / KPC | Store 静的 / KPC | 予算 | 判定 |
| --- | --- | --- | --- | --- |
| `node(ref)` (= `Header(ref)`) | 0 | 7〜8、分岐 1 (`LDP` base+len、`MOVD`、`CMP`、`BCS`、`UBFIZ`、`ADD`、`ADD`) | 6、分岐 1 | +1〜2。bounds check 3、×24 が 2、base 加算 1 |
| header field (`Kind` / `Flags` / `Pos` / `End`) | 1 / 4 field で 5.00 | 1 / 4 field で 4.00 | 1 | 予算内 |
| `ModifierFlags` | 5 + 間接 call / 14.89 | 1 / ≈1 | 1 | 予算内 |
| typed view (`AsXxx`) | 5、分岐 1 (type assert) | 0 | ≤ 7 | 予算内 |
| 名前付きの子の slot 読み (`extra[h.data+k]`) | 1 | 5 (`ADD`、`MOVW` 切り詰め、`CMP`、`BCS`、`MOVWU`) | 1 | **+4**。bounds check 2、切り詰め 1、添字 1 |
| 子を解決済み `Node` で受けて kind を読む (slot + `node()` + `MOVH`) | 2 / 子あたり 8.77 (type assert 込み) | 11 (初回 12〜13) / 16.34 | 7 | **+7.6 / 子** (E3 不合格) |
| `List(l)` の準備 | 3 (`CBZ` + `LDP`) / list あたり 19.55 (要素 6 × 1.65 込み) | 約 30 (slot 5 + `Refs()` 25) / 53.42 (要素 12 × 1.65 込み) | ≤ 18、分岐 3 | **+34 / list** (E3 不合格)。`Refs()` の slice 式 14 と `unsafe.Slice` の検査 7 |
| list の要素 1 つ (ref → kind) | 6 | 12 | 0 + `node()` | `nodes` の base/len の再 load、bounds check 3、×24 が 2 |
| `IdentifierText(h)` | call (inline されない) / 35.06 | 12 / 14.06 | ≤ 20 | 予算内。ただし Store 側は Identifier 専用で本物より軽い |
| `Name(h)` 在 | 4 + 間接 call / 10.26 | 22 (表引き 5、slot 7、`node()` 8) / 21.27 | 20〜30 | 予算内だが Pointer の 2 倍 (E3 不合格) |
| `Name(h)` 不在 | / 13.30 | 11 / 8.96 | — | 予算内 |
| `Parent()` 1 段 | 3 / 3.26 | 11 / 11.95 (ref で回すと 9 / 9.27) | (E11: +4 で Check +2%) | **+8.7 / 段**。cycles 3.72 → 7.99 で、依存連鎖 (load → ×24 → 加算 → load) が律速 |
| `Ref()` (`h` → `NodeRef`) | — | 6 / 4.00 | 約 6 | 予算内。magic 乗算 |
| Walk (`ForEachChild` 契約、inst/visit) | 72.38 | 98.15 (`switch`)、123.83 (`shape`)。nil list ガード後 92.53 / 117.92 | ≤ +10% | **+35.6% → +27.9%** (E1 不合格)。cycles は +14.6% → +13.7% |

`List` の予算 14 のうち 2 つは `start + len` の `uint32` への切り詰めで、`listHeader` を `{start, end}` にすれば消える。実物の 25 はそれに加えて `unsafe.Slice` の長さ・nil 検査 7 と slice 式の検査を払っている。

### 予算が外れた理由

1. **`node()` は「訪問 1 回につき 1」では済まない。** 規則 1 は header field の読みを 1 回の解決に畳めるが、子を辿る辺ごとに `node()` を払うのは変わらない。Walk では非 nil の子 0.742 本/visit、check では `known` 40 回/node と `Parent` 49 回/node がそれぞれ `node()` を払う。子 1 つの読みが Pointer の 2 命令に対して 11 命令なのは、slot 読みの bounds check (2) と `node()` の bounds check (3) と ×24 (2) の 7 命令が、Pointer の「pointer をそのまま deref」に対応物を持たないからである。
2. **list の番兵は inst で損だった。** `extra[0:3] = 0` にして nil list をガードなしで読む形にしたが、Walk の list slot の 60% が nil で、そのたびに `Refs()` の 25 命令を払った (+5.9 inst/visit)。Pointer は `CBZ` 1 つで抜ける。設計文書 §2.3 を直した。
3. **`Node{s, h}` の 2 word 渡し。** closure、walker、`ForEachChild` の各段で spill 2 と mov 1 が増え、Walk で約 +8 inst/visit。`*ast.Node` 1 word に対応物が無い。
4. typed view と type assert の相殺 (−5 × 0.486 = −2.4 inst/visit) は当たっていたが、1〜3 の合計 +25 に対して 1 割でしかない。

### 余裕の見積り (実測後)

- **Walk。** Pointer の基準値は 72.4 inst/visit (Session を GC オフにした後の `BenchmarkASTWalkKPCV1`、`internal/ast/docs/_kpc-baselines/kpc-after.txt`。以前の 70.3 は GC ありの値)。棄却線は 79.6。実測 98.15、nil list のガードを入れて 92.53 (−5.62、見積り −5.9)。残りの直せる分 (`unsafe.Slice` の検査 −1.0、切り詰め −1、bounds check の撤去 −5) を全部入れても約 +13〜14 (1.19 倍) の見積りで、`node()` の残りと 2 word 渡しが残る。**この API の形では Walk ゲートは通らない。** cycles は +14.6% → ガード後 +13.7% (IPC 3.17) で、「cycles の増分は inst の半分以下」は当たっている。nil list の 25 命令を消しても cycles は 2.9% しか減らず、消えたのは並列に実行されていた命令だった。inst で勝てない分を cycles が救うかは、逆に「inst を減らしても cycles が減らない」形でも現れる。
- **Check。** 検証レポート §7.2 の見積りは inst で +637 inst/node (+6.3%、上限)、支配項は `Parent` +426 と `known` +303。同じ重みで cycles を足すと −274 で符号が逆になるが、microbench の cycles は Store に有利に偏る (全ノードを 1 回ずつ順に読む)。inst と cycles のどちらを信じるかは決められていない (§8)。
- **Bind。** 372 inst/node に対して余裕 37。`node()` を子ごとに払うと、bind の子 1.1 本/visit × 7 だけで 8 を使う。E9 (ベンチのばらつき) は未着手。

## 5. 確認手順

中核 (Store、`NodeHeader`、代表 3 kind の typed view、`List`、`IdentifierText`、walk) を手で書いた時点で 1 回、generator の出力に対して 1 回行う。代表 3 kind は葉 (Identifier)、固定 arity (BinaryExpression)、list 持ち (Block)。

1. **inline。** `go build -gcflags='-m=2' <pkg> 2>&1 | grep -E 'can inline|cannot inline'`。§3 の accessor が全部 `can inline` であること。`ForEachChild` の kind 別関数は対象外 (Pointer 版も inline されない)。
2. **bounds check。** `go build -gcflags='-d=ssa/check_bce/debug=1' <pkg>`。報告が `Header` 1、typed view 1、`List` 2、`IdentifierText` 2 以下であること (slice 検査は報告 1 件で分岐は 2。inline された呼び出し元ごとに重複して出る)。これより多ければ accessor の書き方を直す。
3. **命令数。** `go test -c` した binary を `go tool objdump -s '<walker の関数名>'` で開き、葉 1 訪問と BinaryExpression 1 訪問の hot path を数える。§4 の表の目安を実数で上書きする。
4. **ゲート。** `BenchmarkASTWalkKPCV1` と同じ契約の Store 版で inst/visit と cycles/visit を取り、§4 の予測と比べる。Bind は binder 移植後に `BenchmarkASTBindKPCV1` で Pointer の 1.1 倍以内。通るまで checker に着手しない。**先に Bind ベンチのばらつきを潰す。** `internal/ast/docs/_kpc-baselines/kpc-before.txt` の Pointer 3 run は 114.5M / 148.4M / 185.1M inst/op で、幅が 1.1 倍のゲートの 6 倍ある (Walk は 20.88〜21.16M で 1.3%)。1 op が 7.4MB を確保するので、計測 thread に乗る GC の仕事量が run ごとに違うと見る。`GOGC=off` か区間の直前の `runtime.GC()` で inst が ±1% に収まることを確認してから基準値を取り直す。

## 6. side table

置き場所の 2 候補は [B1](store-layouts/b-fixed-arity.md) §6 にある。どちらでも `map[NodeRef]T` は hot path に置かない。

| | 密な列 `[]T` (ref で引く) | slab struct の field |
| --- | --- | --- |
| kind 不問の読み | bounds check 1 回 | Kind switch が要る |
| kind 既知の読み | 同上 | typed view 経由で 1 load |
| メモリ | 全ノード分。symbol を 4B にすると +4B/node (B1 の +11%) | 宣言 kind だけ (checker.ts 6%、dom 24%) |
| GC | `*Symbol` を置くと列全体が scan 対象 | slab が scan 対象になる |

推奨は、symbol を index (`uint32`) にした上での密な列である。checker は `node.Symbol()` を kind 不問で大量に呼ぶので、Kind switch を避けられる利点が大きい。ただし symbol の index 化は別の設計を要るので、binder 移植の設計時に決める。

## 7. checker の通貨 (制約だけ)

`NodeRef` は Store ローカルなので、ファイルをまたぐ checker はそのままでは使えない。前回の `Handle{*Store, NodeRef}` は、accessor が毎回 `Header` 相当を払う形だったので税になった。どの形にするにせよ、次を満たすこと。

- header field の読みが 1 load であること。つまり通貨は解決済みの `*NodeHeader` を含む。候補は `Node{s *Store, h *NodeHeader}` (16B、interface 値と同じ大きさ) で、bounds check は辺を辿るときの 1 回だけになる。
- ノードの同一性が pointer 比較 1 回で済むこと。
- links を引く密な index が安く得られること。`h` と `headers` の base の差から `ref` を戻すと、24B 行では除算が magic 乗算になって 6 命令 (実測) かかる。links を頻繁に引くなら `ref` も通貨に入れる (`Node` が 24B になる) か、行を 2 の冪にする。

決める時期は Bind のゲートを通った後、checker 着手の前である。

## 8. 未決事項

- Walk のゲートを inst と cycles のどちらに置くか。実測は inst +35.6%、cycles +14.6% で、どちらでも棄却線 (+10%) を超えた。役割アクセスは inst と cycles で符号が逆の操作がある (known、polyPresent、list)。cycles の run 間の幅は Walk で 1.1〜1.5%、Pointer 側の Access で最大 14% (list)。
- `IdentifierText` の予算 (確保 0) は、escape 付き識別子が side 表で解けることを前提にしている ([B1](store-layouts/b-fixed-arity.md) §11)。
- `storeChecks` を true にした build を CI のどこで回すか。
