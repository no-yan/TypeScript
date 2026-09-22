# 案 A: 元の Store (header + 共有 children 列)

作成日: 2026-09-20。設計ドキュメントのみ。共通前提は [README.md](README.md)。

## 1. 位置づけ

`cursor/ast-store-tests` (`41b348dc1b`、`/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`) に実装済みの layout である。native parser、binder、checker、emit まで Store で動く唯一の案で、実測がそろっている。この文書は新しい設計ではなく、比較の対照として現状を固定する。

walk ベンチでは「すでに分かっている結果を新しいハーネスで再現できるか」を確認する役目を持つ。

## 2. データ構造

```go
type NodeRef uint32            // Store 内の密な連番、0 = nil
type ListRef uint64            // 上位 32bit = StoreID、下位 32bit = lists の index、0 = nil

type nodeHeader struct {       // 24B、noscan
    kind       Kind            // int16
    childLen   uint8           // 名前付き子 slot の数 (kind で固定)
    listLen    uint8           // list slot の数 (kind で固定)
    flags      NodeFlags
    pos, end   int32
    parent     NodeRef
    childStart uint32          // children 内の先頭。slot を持たない text kind では intern id を兼ねる
}

type listHeader struct {       // 16B
    pos, end   int32
    start, len uint32          // children[start : start+len] が要素
}

type Store struct {
    nodes    []nodeHeader
    lists    []listHeader
    children []NodeRef         // 名前付き slot、list slot (ListRef の下位)、list 要素を 1 本に同居
    internBuf []byte; internOff []uint32
    // bind 列と side map
    symbolIdx []uint32; symbolRefs []*Symbol; flows []uint32
    locals map[NodeRef]SymbolTable; nextContainer map[NodeRef]NodeRef
    tokenFlags map[NodeRef]TokenFlags
    scalarValues map[uint64]uint64; stringValues map[uint64]uint32; objectValues map[uint64]any
    // store をまたぐ辺
    externalChild, externalList map[uint64]Handle; externalParent map[NodeRef]Handle
    ...
}

type Handle struct { s *Store; id NodeRef; Kind Kind }   // 16B の値型。公開 API の通貨
```

- 子 slot は kind ごとに数が固定で、`slotIfStatementExpression` のような生成定数で引く。つまり A も「固定 arity」である。B との違いは、slot が header の外の共有列にあることだけである。
- 非 child の member (`TokenFlags`、bool、operator 以外の scalar) は行に置かず、`NodeRef` と slot 番号を詰めた key の side map に置く。識別子とリテラルの text だけは `childStart` に intern id を入れて行内に持つ。
- list slot は `children` に `ListRef` の下位 32bit を入れ、`lists[i]` が要素の範囲を指す。

## 3. 構築

- native parser が `Factory.createSlots(kind, flags, loc, childLen, listLen)` で行を確保し、`SetChild` / `SetList` で slot を埋める。子は親より先に作られるので行の並びは post-order になる。
- list は `Parser.listScratch` (`[]NodeRef` の LIFO) に要素を集め、`ListRefs` で `children` の末尾へ連続して書く。長さは確定済みで、成長はしない。
- parse 後に `Seal`、bind 後に `Freeze`。`Compact` で列を実長に切り詰める (メモリ `store-scratch-compact-experiment`)。

walk ベンチでは native parser を持ち込まない。Pointer AST からの変換器が同じ `createSlots` / `SetChild` / `ListRefs` を post-order で呼ぶ。

## 4. 読み出し経路

| 操作 | 経路 | 依存 load |
| --- | --- | --- |
| 名前付きの子 | `nodes[id].childStart` → `children[start+slot]` → `nodes[child].kind` | 2 |
| list の要素 i | slot → `lists[l]` → `children[start+i]` → `nodes[child]` | 3 |
| text | `nodes[id].childStart` (intern id) → `internOff` → `internBuf` | 2 |
| parent | `nodes[id].parent` | 0 (header 内) |
| 非 child の値 | `scalarValues[key]` の map 引き | map |
| 全子走査 | 生成された Kind switch → slot ごとの accessor → `v(Handle)` | — |

- `childAt` は slot が 0 のとき `childAtSlow` に落ち、`externalChild` が nil でなければ map を引く。optional な子が nil の slot (宣言 slot の約 4 割) は毎回ここを通る。parse 直後の木では map が nil なので、費用は関数呼び出しと nil 判定で済む。
- `ListAt` / `ListLen` は毎回 `listOwner(list)` で StoreID を比べる。
- 子を返すたびに `Handle{s, id, Kind}` を組み立てるので、子の `kind` を読む load が accessor に含まれる。

## 5. Footprint

README §2 の分布で試算すると checker.ts で 33.2 B/node (Pointer の 39%)。内訳は header 24B、slot 1.47 × 4B、list header と要素。非 child の値を持つ side map と、bind 列 (`symbolIdx`、`flows` 各 4B/node) は含まない。

header を 24B から 16B に削る実験 (`nodeLoc` 列の切り出し) は e2e で有意差が出ず、不採用にしている (メモリ `node-loc-column-split-experiment`)。

## 6. 同一性と side table

`NodeRef` は密な連番なので、`symbolIdx []uint32` や `flows []uint32` をそのまま引ける。ファイルをまたぐ同一性は `GlobalRef` (StoreID と NodeRef を詰めた `uint64`) を使う。checker には `map[ast.Handle]` が 22 個、`LinkStore[ast.Handle, …]` が 12 個残っている。

## 7. 型安全性とメンテナ受容性

- header は Go の struct だが、子は slot 番号で引く。slot 定数を取り違えると、panic せずに別の子が返る。kind 別の struct が存在しないので、delve で見えるのは数値の列である。
- 手書きのコードが slot を直接触ることはなく、`h.IfStatementExpression()` のような生成 accessor を通る。安全性は生成器の正しさに依存する。
- 非 child の値が side map にあることは、上流の「フィールドを読む」モデルから最も遠い部分である。
- 「手で decode する AST は上流に通らない」を基準にするなら、A はその基準で落ちる。C の `extra` と同じ種類の弱点である。

## 8. Transform / Update

`Factory.Update*` は Pointer 版と同じ形で、子が 1 つでも変われば `New*` で新しい行を作る。emit は別の Store に書き、変わらなかった部分木は `externalChild` / `externalList` の map で元の Store を指す (STORE.md の不変条件 9)。transform 後の木は密ではなくなる。STORE.md には「同じ Store に混在させる」という記述 (:150) と「EmitContext ごとの Store」という記述 (:108) が併存しており、整理が要る。

## 9. 既知の実測

いずれもユーザーのメモリに記録された値で、`cursor/ast-store-tests` 系の revision に対するもの。

- bind は Monaco 規模で Original の 1.3〜1.6 倍。原因は命令数 1.79 倍 (6.10G vs 3.41G) で、IPC は Store のほうが高い (3.93 vs 3.10)。メモリ待ちではない (`bind-childkinds-column-rejected`)。
- 命令差の 4 割は走査機構 (`bindKind` の 2 段 switch、`childAt`、`listOwner`)、残りは side map と `SetFlow` / `flowID`。識別子 1 訪問は Store 約 265 命令、Original 約 160 命令 (`bind-chain-hotpath-analysis`)。
- 生成 walker に替えても bind の命令数は減らなかった。走査コストは関数間を移動しただけだった (`bind-generated-walk-no-inst-gain`)。
- VS Code 全体では accessor +7.5G、map +2.7G、書き換えた checker +1.9G (`vscode-trace-common-vs-store-only-split`)。
- flows を `uint32` 列にしたときは large e2e で Bind −15%、user CPU −9%。効いたのは GC が辿るポインタの数だった (`flow-index-column-adopted`)。

## 10. Walk ベンチへの接続

- 持ち込むもの: `nodeHeader`、`listHeader`、`children`、intern、`Handle`、生成 accessor、生成 `ForEachChild`、`createSlots` / `SetChild` / `ListRefs`。
- 持ち込まないもの: `Freeze`、identity domain、foreign edge、bind 列、side map、JSDoc cache。ただし `childAtSlow` と `listOwner` の分岐は production の走査費用なので、残したまま測る。
- 走査 API は production の `Handle.ForEachChild(StoreVisitor)` を使う。
- 変換器は B / C と同じ生成 switch の出力先を差し替えたものにする。

## 11. 仮説と棄却条件

- 仮説: 訪問数だけの full walk では Pointer より inst/visit が多い。子 1 つあたり依存 load が 1 段多く、`Handle` の組み立てで子の kind も読むため。bind で見た識別子 1 訪問 265 対 160 命令は binder の仕事を含むので、walk 単体の倍率はこれより小さいはずである。倍率の予測値は持っていない。
- A は対照なので、結果によって A を採否することはない。A の数字が既知の傾向 (命令は多いが IPC は高い) と合わなければ、ハーネスか変換器の側を疑う。

## 12. 未決事項

- 既存の worktree から最小部分を切り出すか、`store-ast` 上で書き直すか。切り出しは依存 (identity、Freeze) を剥がす作業が要る。
- `Handle` を経由しない内部走査 (`*Store` + `NodeRef`) を A にも用意するか。用意すると B / C との比較は公平になるが、A の production API ではなくなる。
