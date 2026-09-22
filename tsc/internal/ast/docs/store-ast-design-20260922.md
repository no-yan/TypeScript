# Store AST 設計: 決定事項と検証計画

作成日: 2026-09-22。設計ドキュメントのみ。コードは変更しない。

2026-09-21〜22 の議論で決めたことを 1 か所にまとめる。根拠の実測は [accessor-dynamic-frequency-20260921.md](accessor-dynamic-frequency-20260921.md)、accessor の規則と予算は [store-api-accessor-budget.md](store-api-accessor-budget.md)、layout 候補の比較は [store-layouts/](store-layouts/README.md) にある。

この文書が既存の文書を置き換える点:

- layout の主案は B1 (型付き slab) ではなく、**24B header + 一様配置の `extra` 列**である (§2.2、理由は §3)。store-layouts の A 案から `childAt` の 2 段 switch、`listOwner`、スカラの side map を取り除いた形に当たる。
- budget 文書のコード例は B1 で書かれているが、規則 1〜5 と予算はそのまま有効である。typed view の中身が「slab の行への pointer」から「`{s, h}` の型の付け替え」に変わる (§2.5)。

## 1. 目標とゲート

AST をフラットな arena に置き換える。進め方は「最初に測れる指標から順に関門にする」。

| ゲート | 指標 | 基準値 (Pointer、M1) | 棄却線 |
| --- | --- | --- | --- |
| 1. Walk | `BenchmarkASTWalkKPCV1` と同じ契約の Store 版、**cycles/visit** (2026-09-22 に inst から変更) | 25.67 cycles/visit (72.4 inst、IPC 2.8。GC オフの Session、`internal/ast/docs/_kpc-baselines/kpc-after.txt`) | +15% 超 (29.5)。**2026-09-22 実測 29.20 (1.137 倍)、合格。** inst は 92.53 (1.279 倍) で、+20 inst/visit は IPC 3.17 で流れる整数命令 (`node()` と 2 word 渡し) |
| 1b. 役割アクセス | 代表 kind の accessor microbench、inst/access (§4 E3) | Pointer 版の同じ操作 | 類型ごとに Pointer 以下。超えたら命令列で理由を説明できること |
| 2. Bind | `BenchmarkASTBindKPCV1`、**cycles/node** (2026-09-22 に inst から変更) | Bind 110.5M inst / 298,054 node = 371 inst/node (GC オフの Session。cycles の基準値は `internal/ast/docs/_kpc-baselines/kpc-after.txt` から取る) | 1.1 倍超。E9 は解消済み。Walk の +20 inst/visit が inst の余裕 37 の 54% を先に使うので inst では判定しない |
| 3. Check / Emit | V1 ベンチ | 9.43G / 6.50G inst | binder 通過後に決める |

Walk を第一関門にする理由は、最初にテストできる指標だからである。ただし walk は不採用しか判定できない (前回の Store は walk ではなく accessor 税で負けた)。1b を同じ段階に置くのはそのためで、1b の成果物には accessor の形ごとの命令列と単価表を含める。「なぜ遅いか」を命令列で説明できない状態で先へ進まない。

## 2. 決定事項

### 2.1 データ構造

```go
type NodeRef uint32                 // nodes の index。密な連番、0 = nil

type NodeHeader struct {            // 24B、noscan
    kind          Kind              // int16
    modifierFlags uint16            // 構文由来の ModifierFlags (bit 0〜15)
    flags         NodeFlags         // uint32
    pos, end      int32
    parent        NodeRef
    data          uint32            // extra 内の payload 先頭。kind によっては別の意味 (§2.4)
}

type Store struct {
    nodes []NodeHeader              // nodes[0] は番兵行 (kind = Unknown、parent = 0)
    extra []uint32                  // 子 slot、data word、list block。extra[0] = 0
    src   string                    // source text
    texts string                    // source と一致しない text の追記領域 (§2.4)
    file  uint32                    // program 内の file index
}
```

- `kind` (2B) と `flags` (4B) の間の 2B は alignment で必ず空く。そこに構文由来の `ModifierFlags` を置く。`ModifierFlags()` は check で 92 万回、kind を問わず呼ばれ、modifiers を持てるのは 35 kind だけである。header に置けば不在の判定も表引きも要らない。**bit 16 以降 (Deprecated、JSDoc 系 bit 23〜27、`HasComputedJSDocModifiers`、`HasComputedFlags`) は header に入れず別経路にする。** どれも JSDoc を読んで遅延計算される側で、構文だけでは決まらない。別経路の設計は JSDoc の置き場所 (§5) と一緒に決める。それまでの間、`NodeHeader.modifierFlags` の宣言と、`ModifierFlags` を `uint16` に切り詰める箇所 (constructor) の両方に、「bit 16 以降は未対応、どこで決めるか」を書いた `TODO` コメントを必ず置く。
- `NodeFlags` は header に置く。読みは kind を問わない (bind 187 kind、check 110 kind、Identifier が最多。context flags は全ノードに付く)。
- `NodeRef` は密で、parse 完了後は不変。side table の設計はすべてこの性質に依存する (§2.8)。

### 2.2 payload の一様配置

全 kind が同じ形で payload を持つ。子 slot も非 child の data word も `extra[h.data + k]` にあり、`k` は kind ごとの定数で schema から生成する。

| 類型 | 形 | 依存 load |
| --- | --- | --- |
| kind 既知 `call.Expression()` | `extra[h.data + 定数]` | 2 (Pointer と同じ) |
| kind 多相 `n.Name()` | `extra[h.data + nameSlot[h.kind&511]]` | 2 + L1 の表引き |
| list `call.Arguments()` | slot → list block (§2.3) | 3 (Pointer と同じ) |
| text `ident.Text()` | `src[h.data:h.end]` | 1 |

- slot の並びは宣言順 (= source order)。`ForEachChild` の契約と一致させる。
- token と keyword (payload 0) は `extra` を使わない。
- スカラ (TokenFlags、operator など) を side map に置かない。前回の side map は +54M inst (bind) だった。

### 2.3 list

`extra` 内の block `[len, pos, end, elem0 … elem(len-1)]`。slot にはその index を 1 word で持つ。

- 0 は nil、空 list は `len = 0` の block。TS の AST は `typeArguments` の不在と空の `<>` を区別する。**nil list は `at != 0` で弾いてから読む。** 番兵 block (`extra[0:3] = 0`) でガードなしに読む形は 2026-09-22 の検証で取り下げた: Walk の list slot の 60% が nil で、そのたびに要素列を切り出す 25 命令を払っていた。ガードを呼び出し側に入れて実測 −5.62 inst/visit (98.15 → 92.53)、cycles は −2.9% (消えたのは並列に実行されていた整数命令)。Pointer は `CBZ` 1 つで抜ける。nil ノードの番兵 (`nodes[0]`) は据え置く。header の読みは 1 load で、ガードと同じ費用だからである。
- list の `Loc` は block 先頭に持つ (emit で 33 万回、check で 8 万回読まれ、空 list の pos は要素から復元できない)。list は 0.14 個/node なので約 0.4 word/node。
- 要素は連続。parser は scratch に集めてから 1 回で書く。長さは確定済みで成長しない。
- API は役割名で生成する (`Arguments()`、`Parameters()`)。返すのは要素 `NodeRef` の連続列で、呼ぶ側が 1 要素ずつ解決する。
- list の同一性 (transformer の Update 比較) は block index の一致で表す。

### 2.4 text

- Identifier は `data` = text の開始位置、text = `src[data:end]`。payload 0 word、alloc なし。現行 Pointer AST も source の部分文字列であり、同じ性質を保つ。`Text()` は check で 435 万回、98% が Identifier。
- source と一致しない text (escape 付き識別子、escape を含む文字列リテラル、正規化される数値リテラル) は `texts` に追記し、flag で切り替える。flag の置き場所は未決 (§7)。
- `texts` と parser の text 用 buffer を parser 間で共有しない (ゼロコピーなので次のファイルで上書きされる。2026-09-16 に再現済み)。

### 2.5 通貨と typed view

```go
type Node           struct{ s *Store; h *NodeHeader }     // 16B。引数とローカル変数の通貨
type CallExpression struct{ s *Store; h *NodeHeader }     // typed view。中身は Node と同じ
type Ref            struct{ file uint32; id NodeRef }     // 8B、noscan。ヒープに保存する形

func (n Node) AsCallExpression() CallExpression { return CallExpression(n) }   // 0 命令
func (c CallExpression) Expression() Node { return c.s.node(c.s.extra[c.h.data+0]) }
```

- **解決は辺 1 本につき 1 回。** `s.node(ref)` (6 命令、分岐 1) を子を辿るときに 1 回払い、以後の kind / parent / flags / pos / end は 1 load で読む。check の header スカラ読みは約 210 回/node で、`{s, id}` の形だと読むたびに +5 命令、Check 全体で +10% 前後の見積りになる。Go の SSA は呼び出しをまたいで `s.nodes` の load と bounds check をまとめられないので、コンパイラには期待しない。
- **typed view は型の付け替えで、作成時に何も load しない。** slot が何個ある kind でも同じ。各 accessor が自分の slot だけを読む。作成時に全 slot を読む decode はしない。payload 先頭を view に持たせる案も採らない (check で cast 23.6 回/node に対し typed field の読みは 17.1 回/node、cast 1 回あたり 0.7 回しか読まれない)。
- **typed view は kind を検査しない。** `const storeChecks = false` で囲った assert を生成し、test と CI だけ true にする (budget 文書 規則 5)。
- **accessor にガードを置かない。** `nodes[0]` が番兵行なので `s.node(0)` は有効で、accessor は nil 分岐を持たない。nil の意味が要る場所だけ呼ぶ側が判定する (budget 文書 規則 3)。
- **ヒープに保存するのは `Ref`。** `{s, h}` を links や cache に保存すると GC が辿る pointer が増える (flows の uint32 化で効いたのは pointer 数だった)。
- `h` から `NodeRef` への逆算は `(h - base) / 24` (magic 乗算で約 6 命令)。使うのは side column を引くときで約 10 回/node。binder のように頻繁に要る場所は `(ref, h)` を両方持ち回る。
- **子を返す accessor の返り値は解決済みの `Node`。** 受け取った側はほぼ必ず子の kind を読むので、解決を accessor の中で 1 回行う。list の要素だけは `NodeRef` の連続列で返し、呼ぶ側が解決する (§2.3)。nil 判定だけ・転送だけの呼び出しで解決が無駄になる量は E3 で数えるが、API の形は変えない。

#### 2.5 の再検討 (2026-09-22、storeexp の検証後)

検証 ([storeexp-verification-report-20260922.md](storeexp-verification-report-20260922.md)) で、子 1 つを解決済み `Node` で受けて kind を読む費用が Pointer 2 命令に対して 11 命令、`Parent()` 1 段が 3 対 11 命令と分かった。「解決済み `Node` を返すか `NodeRef` を返すか」を問い直したが、**数字が指しているのはその選択ではない。** 呼ぶ側が kind を読むなら `NodeRef` を返しても同じ `node()` を呼ぶ側で払うので、命令数は変わらない。差が出るのは nil 判定だけ・転送だけの呼び出しに限られ、その頻度は E3 で数えられていない (storeexp は計測していない)。返り値の形は据え置き、頻度を数えるまで変えない。

数字が指しているのは `node()` と slot 読みの単価である。`s.node(ref)` の 7 命令は bounds check 3 (`MOVD`、`CMP`、`BCS`)、×24 が 2 (`UBFIZ`、`ADD`)、base 加算 1、base/len の `LDP` 1。slot 読み `extra[h.data+k]` の 5 命令は bounds check 2、`uint32` の切り詰め 1、添字 1、load 1。この形は Pointer の「pointer をそのまま deref」に対応物を持たず、辺を渡るたびに払う (Walk で非 nil の子 0.74 本/visit、check で `known` 40 回と `Parent` 49 回/node)。

代替の形を objdump と実 Store (checker.ts、Identifier から root までの parent 連鎖 145 万段) の ns で比べた (`_storeexp-results/resolve-variants-ns.txt`、scratchpad の `nodeexp`):

| `node()` の形 | 子の kind 読み (slot 込み) | parent 1 段 (静的) | parent 1 段 (ns) | 代償 |
| --- | ---: | ---: | ---: | --- |
| A: `&s.nodes[ref]` (現行) | 11 | 11 | 2.64 | — |
| A + `int(h.data)+k` | 10 | 11 | | なし |
| B: `*[1<<32]NodeHeader` を 1 回 `unsafe` で作り、以後 `&s.arr[ref]` | 8 (nil probe `MOVB` 1 を含む) | 8 | 2.46 (−7%) | bounds check が消える = 壊れた ref は黙って隣を読む。`storeChecks` で検査を戻す |
| C: `unsafe.Add(base, ref*24)` | 7 | 8 | 2.46 (−7%) | B と同じ。accessor ごとに `unsafe` が要る |
| D: 32B header、slice | 10 | 9 | 2.09 (−21%) | +8 B/node (footprint 32.6 → 40.6、Pointer の 48%) |
| E: 32B header + B | 7 | 6 | 1.99 (−25%) | B と D の両方 |
| Pointer | 2 | 3 | 1.12 | |

読み取れること:

- **命令数は bounds check、cycles は ×24 が効く。** parent 連鎖は依存連鎖 (load → 添字 → load) が律速で、bounds check は連鎖の外にあるので cycles に効かない (−7%)。32B にすると ×32 が `ADD Rbase, Rref<<5` の 1 命令に畳まれ、連鎖が 1 段短くなる (−21%)。それでも Pointer の 1.8 倍で、残りは「index → address」の変換 1 段と、列が別に確保されることによる cache の差である。
- **どの形でも Pointer と同じ単価にはならない。** 最良の E でも子の読みは +5 命令、parent 1 段は +3 命令 (+0.9 ns)。index ベースの arena を Go で書く限り、辺 1 本につき base 加算 1 と load 1 は消えない。
- `Node{s, h}` の 2 word 渡し (Walk で約 +8 inst/visit) はこの表の外にあり、通貨を 1 word にしない限り残る。1 word にするには `h` だけを持ち回って `s` を別経路で得る形になるが、Store が複数 (file ごと + synth) なので `h` から `s` は逆算できない。

判断 (提案。決定はユーザー):

1. **返り値は解決済み `Node` のまま。** nil 判定だけ・転送だけの呼び出しの頻度を、checker 移植の設計時に数える。
2. **`int(h.data)+k` にする。** 代償なしで −1/slot。
3. ~~bounds check を release で消す~~ **却下 (2026-09-22、ユーザー)。** B / C / E の形は壊れた ref で panic せず別の行を黙って返す。debug build の assert だけで守る設計は安全ではなく、merge できる提案ではない。将来の再検討はあり得るが、今の設計には入れない。規則「`unsafe` は `Ref()` と `List.Refs()` だけ」は据え置き。
4. **32B header は symbol / locals 列の設計 (§5) と一緒に決める (2026-09-22、ユーザー: 可能性は高いが、基盤が固まってから)。** +8 B は予約 slot に使える (Identifier の symbol、宣言の symbol など、§5 で side column に置く予定のもの)。不変条件 2.8.2 (offset はすべて生成) が守られていれば header 幅の変更は schema だけで済み、変わるのは `Ref()` の magic 乗算だけなので、延期の代償は無い。**それまで parent 連鎖の 2 倍 (inst 3 → 12、cycles 3.7 → 8.0 / 段) は既知の負債として持ち越す。** check は `Parent` を 49 回/node 読み、Check 見積りの支配項 (+426 inst/node) である。checker 移植の設計 (§6 の 9) で親を多段に辿る経路 (`FindAncestor` 系、`GetSourceFileOfNode`) を数えるときの前提にする。
5. **Walk ゲートは cycles に置く (2026-09-22、ユーザー)。** nil list のガードは入れて実測 inst 92.53 (1.279 倍)、cycles 29.20 (1.137 倍)。inst は 2 を入れても約 91 (1.26 倍) で +10% に届かない。閾値は +15% (決定済み、§1)。cycles は +13.7% で合格。無検査と 32B でどこまで下がるかは KPC で測る。ゲートを inst に置くなら、この設計 (16B の通貨 + index arena) は棄却される。

### 2.6 kind 多相 accessor

kind → slot の静的データ表 (`[512]uint8`、schema から生成) を 1 回引く。ジャンプテーブルではない。kind ごとに変わるのが定数だけなので、コードは全 kind で 1 本、分岐なし、inline 可能になる。これが成り立つのは §2.2 の一様配置だからである。

- 役割を持たない kind は表の番兵 (0xFF) で弾く。比較 1 回。`Locals()` は check 230 万回の 60% が不在の kind に対する呼び出しなので、不在の経路も安くなければならない。動的にはほぼ単相 (`Text()` 98% Identifier、`Expression()` 90% が PropertyAccess と Call) なので、mask による分岐回避まではしない。
- `&511` は表引きの bounds check を消すため (E4 で確認)。
- `ForEachChild` は kind ごとにコードの形が違うので、全 kind を連番で網羅する生成 switch にする。Go がジャンプテーブルにすることを E5 で確認する。

### 2.7 構築

- parser は **id で書く** (`s.nodes[id].end = …`)。append 中は `h` を持たない。
- 構築は post-order (子が先、親が後)。parser が実際に作る順であり、左再帰で既存ノードを包む場合も成り立つ。親を作る時点で子の id が揃っているので、**生成された constructor が子の `parent` を書く** (`s.nodes[child].parent = id` を slot の数だけ、list は要素の数だけ)。parse 後の線形 pass にはしない。
- speculation の巻き戻しは `nodes` と `extra` の truncate。
- parser は使い回しの scratch に書き、完了時に正確なサイズへ Compact する (2026-09 に採用済みの形。large `--noCheck` で Memory −18.6%)。**`{s, h}` を配るのは Compact の後だけ。** Compact は番号を振り直さない。
- binder は parse 完了後に走るので `{s, h}` を使う。`h.flags |= …` の直接書き込みは許す (ファイル単位で直列)。

### 2.8 不変条件

1. `NodeRef` は密な連番で、parse 完了後は不変。bind 開始後の renumber と、word offset を ref にする設計は採らない。
2. slot の offset はすべて生成された定数。手書きの offset を 1 つも置かない。これを守る限り、後から予約 slot (symbol など) を足すのは schema の変更で済む。
3. parse の Store の辺はすべてローカル。accessor に foreign 検査を入れない。
4. `extra` は `[]uint32`。pointer を置けないので Store 本体は noscan のままになる。
5. bind 完了後は Store に書かない。checker は何本立てても読み取りだけで、競合も false sharing も無い。現行の bind 後の書き込みで確認できているのは `GetNodeId` の遅延採番 (check で 333 万回) で、`NodeRef` が密なので消える。checker の `node.Flags` への書き込みは grep では 0 件だった。他の field への書き込みは checker 移植の設計時に洗い出す。
6. `map[NodeRef]T` を hot path に置かない。

### 2.9 外部 Store (合成ノード)

Store への登録も中央の registry も持たない。

- checker / emitter の各インスタンスが自分の synth Store を持ち、自分用のテーブル `stores = program.stores + [synth]` を持つ。synth の file index は `nFiles`。同じ index が phase ごとに別の Store を指す。
- `Ref` → `Node` の解決は境界で 1 回 (`Node{c.stores[r.file], …}`)。ノードは自分の Store を運ぶので、accessor は「Store に無ければ外を探す」処理を持たない。
- side table も同じ形で `links[file][id]` に synth 用の 1 スロットを足す。
- `file == nFiles` の `Ref` を共有構造 (checker 間で共有される symbol の declarations、diagnostics) に漏らさない。
- Store をまたぐ辺 (合成 root の parent、emit での部分木再利用) の扱いは未決 (§5)。検査が要るのは辺の解決だけで、header 読みには及ばない。

## 3. 採らなかった案

| 案 | 理由 |
| --- | --- |
| B1 (kind 別の型付き slab) | kind 多相の accessor が「kind で slab を選ぶ switch」になる。kind 既知のアクセスには最適だが、多相で前回と同じ税を払う (check で多相 14.9 回/node) |
| C (inline `a`/`b` + extra) | 多相 accessor が 2 つの形と分岐を持つ。差が出る読みは 32 回/node で Check の約 1%。単純な方を既定にする。E3 で負けたときの次の候補 (E7) |
| sibling 連鎖 (first-child + next) | +4B/node、`args[i]` が O(n)、役割アクセスが連鎖の追跡になる |
| 通貨 `{s, id}` | §2.5。header 読みごとに解決の税 |
| `NodeFlags` の外部化 | 読みが kind 不問。id の逆算と 2 本目の bounds check を払って得るのは 24B → 20B だけ |
| `ModifierFlags` を payload に置く | 差は 1 回 5〜8 命令 × 約 1 回/node で検出不能。穴は alignment で必ず空くので使う |
| typed view の decode、payload 先頭の事前解決 | §2.5 |
| 長さ 1 の list の inline | shape 分岐が accessor に入る |
| 別配列の `lists` + `elems` | 配列が増えるだけで依存 load は同じ。nil と空の区別に別の手段が要る |
| 多相 accessor の mask trick | 動的にほぼ単相なので番兵の比較 1 回で足りる |
| 中央の Store registry / DI | 並列時の登録が競合点になる。§2.9 で不要 |

## 4. 実験で確認すること

| # | 確認すること | 方法 | 判定 |
| --- | --- | --- | --- |
| E1 | Walk が Pointer の +10% 以内 | Pointer AST から変換した Store で Walk 契約の KPC。fingerprint で順序と訪問数を照合 | inst/visit ≤ 79.6。予測は +8〜10% だった。**結果: 98.15 (+35.6%)、nil list ガード後 92.53 (+27.9%)、不合格。** 理由は budget 文書 §4 |
| E2 | accessor が全部 inline され、bounds check が予算内 | `-gcflags=-m=2`、`-d=ssa/check_bce/debug=1`、`go tool objdump` | `node()` 1、slot 読み 1、list 2、text 2 以下。命令数を budget 文書 §4 の表に実数で上書き |
| E3 | 役割アクセスの単価 (ゲート 1b) | 代表 6 kind。checker.ts 上で類型ごとの microbench、KPC で inst/access。重みは check の比 (header : cast+field : 多相 : 外付け ≒ 210 : 40 : 15 : 15) | 類型ごとに Pointer 以下 |
| E4 | `nameSlot[kind&511]` の bounds check が消える | check_bce | 報告 0 |
| E5 | 全 kind 網羅の生成 switch がジャンプテーブルになる | objdump | 間接分岐 1 回。二分探索なら case の並べ方を直す |
| E6 | `h` → `NodeRef` の逆算の費用 | objdump と microbench | 約 6 命令。binder で効くなら `(ref, h)` 持ち回りで回避済みか確認 |
| E7 | 一様配置と C 型の差 | E3 と同じ bench を C 型でも作る | **E3 が不合格のときだけ実施**。見積りは Check の約 1% |
| E8 | 辺の解決に foreign 検査 (`ref>>31`) を足したときの増分 | Walk に足して inst/visit の差 | synth Store の設計時。+2 以下なら単一の accessor、超えるなら emit 専用の型 |
| E9 | Bind ベンチのばらつき | `GOGC=off` か区間直前の `runtime.GC()` | inst が ±1% に収まること。**解消済み**: Session を GC オフにして Bind 110.5M inst、幅 ≤ 1.6% (`internal/ast/docs/_kpc-baselines/kpc-after.txt`、commit 5647a91eb8) |
| E11 | 親連鎖の 1 段の単価 | Identifier から root まで辿る microbench。`{s, h}` では 1 段ごとに `s.node(h.parent)` の解決 (bounds check + 添字計算) が要り、Pointer は 1 load で済む。check の `Parent` 読みは 49 回/node | Pointer との差を inst/段で出す。+4 命令なら Check の約 +2% の見積り。大きければ、連鎖を ref で回して `nodes` を局所変数に持つ形 (FindAncestor 系の書き方) を規約にする |
| E10 | footprint | 変換後の `len(nodes)*24 + len(extra)*4` | 試算 33〜35 B/node (Pointer 84.6)。store-layouts README §4 の A と同等 |

E1〜E6 は手書きの代表 kind に対して 1 回、generator の出力に対してもう 1 回行う。

代表 7 kind: 葉の token、Identifier (text)、BinaryExpression (固定 arity + operator)、CallExpression (固定 + list + optional)、PropertyAccessExpression (`Name()` と `Expression()` の最多の receiver)、FunctionDeclaration (modifiers、optional 多数、list、locals container)、SourceFile。

storeexp の出口 (2026-09-22、ユーザー): §6 の 7 で本物の generator ができるまで参照用に残し、その時点で削除する。

検証用の実装は [storeexp-implementation-instructions-20260922.md](storeexp-implementation-instructions-20260922.md)、測り方と判定は [storeexp-verification-instructions-20260922.md](storeexp-verification-instructions-20260922.md)。実装は `internal/ast/storeexp` に build tag 付きで置く書き捨てで、本番の配置や generator を先取りしない。

## 5. 後回しにしたもの

AST ノードの形が決まるまで決めない。§2.8 の不変条件を守っていれば後から入れられる。

### symbol / locals / links

| | 理想形 | 現実的な移行 |
| --- | --- | --- |
| node → symbol | `SymbolId` (uint32) を宣言 kind の予約 slot に。Store は noscan のまま | まず `symbols []*Symbol` の密な列 (8B/node、scan 対象)。map なしで binder を通し、Bind ゲート通過後に index 化 |
| locals | container kind の予約 slot → `s.locals[]`。不在は kind の bitset で弾く | 同じ。`Locals()` の 60% が不在 kind なので bitset は最初から入れる |
| checker links | `links[file][id]` の 2 段 (index 列 4B/node/checker → slab)。全ノード分の密な links は checker 数 × ノード数 × links の大きさで破綻する | checker 着手時に決める |
| flow node | flow 構造の packed 化は別計画 (前回の Store ブランチの `flow-packed-plan.md`。このブランチには無い) | node → flow は symbol と同じ扱い |

### synth Store

- checker / emit の synth Store は読みながら伸びる。再確保後の古い `h` は Go では生き残るので crash せず、古い内容を読む・書き込みが消える・同一性と id の逆算が壊れる、という静かな失敗になる。固定長 chunk の連結にして動かさない。id の逆算は chunk の探索になる (parse の Store は chunk 1 個)。
- Store をまたぐ辺の表現 (E8)。
- transformer の Update / 部分木再利用を append-only の arena でどう表すか。Emit は 6.50G inst あり、着手前に別文書で設計する。

### その他

- JSDoc ノードの置き場所 (parse の Store か、checker の synth Store での遅延構築か。前回は後者で Monaco Bind −20〜29%)。
- LS 向けの incremental reparse。
- api/encoder の追従。

## 6. TODO

1. schema の形式を決め、代表 6 kind を書く (役割、optional、list、data word、予約 slot の宣言)。上流の `tools/scripts/tsc/ast.json` (kinds / bases / nodes、generator は `generate-go-ast.ts`) がすでに member 定義を持ち、store-layouts README の分布実測もこれを使っている。新しい schema を作らず、これに Store 用の出力を足すのを既定とする。`definitions` の `members` が宣言順・`optional`・`list` を持つので、slot の並びはここから決まる。
   - **Walk ゲートは手書きの 6 kind だけでは測れない。** checker.ts には 149 kind が現れ (プロジェクト全体で 187)、E1 は全部を変換して走査できる必要がある。したがって `ast.json` を読んで「kind ごとの slot の並び (shape 表)・`ForEachChild`・Pointer → Store の変換器」だけを出す最小の generator を先に作る。typed view と多相 accessor は代表 6 kind を手書きし、ゲート 1b に使う。`ast.json` 自体は変更しない。
2. `Store`、`NodeHeader`、`Node`、代表 6 kind の typed view、list、text、`ForEachChild` を手書きする。手書きは「生成されるはずの出力の見本」として書く。
3. Pointer AST → Store の変換器と fingerprint テスト (Walk 契約)。
4. E1、E2、E4、E5 (Walk ゲート)。
5. E3、E6 (ゲート 1b)。単価表を budget 文書 §4 に反映する。
6. generator で全 kind に展開し、E1〜E6 をもう一度。
7. parser を Store に直接構築させる (id 書き、scratch、Compact)。Parse ベンチで Pointer と比較。
8. E9 を済ませてから binder を移植し、Bind ゲート。symbol は §5 の移行形。
9. checker の設計 (links、`Ref`、synth Store)。ここで §5 を別文書にする。
10. emit の設計 (synth Store、Update、E8)。

## 7. 未決事項

- source と一致しない text を示す flag の置き場所 (`NodeFlags` の空き bit か、`data` の最上位 bit か)。
- `ModifierFlags` の bit 16 以降の別経路の中身 (別経路にすること自体は §2.1 で決定済み)。候補は checker の links に遅延計算の結果を置く形で、現行の `HasComputedFlags` の cache と同じ役割になる。
- 32B header にするか (§2.5 の再検討 4)。§5 の symbol / locals 列の設計と一緒に決める。
- `storeChecks` を true にした build を CI のどこで回すか。
- `JSDocParameterOrPropertyTag` の子の順序。Pointer 版は `IsNameFirst` で `name` と `TypeExpression` の訪問順を実行時に入れ替えるが、固定 shape では表せない。JSDoc の置き場所 (§5) と一緒に決める。それまで生成 shape は宣言順。

## 8. 今後の最適化候補

実測の回数は check、936,205 ノードの compiler プロジェクト。どれも §2 の形が動いてから、回数 × 単価で効果を見積もって着手する。

| 候補 | 根拠 | 内容 |
| --- | --- | --- |
| 演算子 kind を親の payload に複写 | `BinaryExpression.OperatorToken` 129 万回、ほぼ `.Kind` を見るだけ | data word に operator kind を置き、子の header への依存 load を消す。token ノードは walk 契約と emit のために残す |
| token の有無を bit に | `QuestionToken` 60 万、`DotDotDotToken` 11 万、`ExclamationToken` 7 万回 | 有無だけを問う読みは flag 1 bit で足りる |
| SourceFile への到達を O(1) に | `GetSourceFileOfNode` 948 万回 (82% は EnumMember で入力固有の偏り) | `s` から 1 load。親連鎖を辿らない |
| `IsXxx` の複数 kind 述語を bitset 表に | `IsXxx` 72 回/node (check)、55 回/node (emit)。emit は 1 訪問ごとに 5 述語を順に試している | `kindClass[kind] & mask` の 1 load に畳む |
| `Locals()` の kind gate | 230 万回の 60% が不在 kind | §5 の bitset。名前解決の祖先上りで効く |
| header を 32B に | parent 連鎖 1 段の ns −21% (2.64 → 2.09)、`node()` −1 命令、`Ref()` の逆算が shift になる (§2.5 の再検討) | +8 B/node。予約 slot (symbol など) に使うなら純増ではない。§5 と一緒に決める |
| ~~`ForEachChild` を shape 表のループに~~ | E1 で計測: switch より inst +26%、cycles +11% | 取り下げ |
| 線形走査の活用 | `nodes` は連続で post-order | 親の設定、kind の集計、subtree facts の伝播などを木の走査ではなく配列の 1 pass にする |
| 識別子の名前ハッシュを 1 回に | symbol 表の lookup ごとに文字列を hash している | intern id か hash の保存。Identifier の payload が 0 word でなくなる代償がある |
| 死にノードの切り詰め | speculation の巻き戻し | §2.7 の truncate で自然に入る。計測して確認 |
| checker links の 2 段配列 | 前回 Handle キーの LinkStore が checker CPU の −7% 分 | §5 |
| `SymbolId` 化 | GC が辿る pointer 数 | §5 |
| `unsafe` で bounds check を消す | index 化で Go に固有の税 | 最後の手段。上流の受容性を捨てる。E2 / E3 で差が bounds check に帰属できた場合だけ |

やらないと決めているもの: 箇所単位の微調整 (`-B`、識別子 fast path など)。前回どれも 1〜6% で多くはノイズ以下だった。回数を減らす形だけを採る。
