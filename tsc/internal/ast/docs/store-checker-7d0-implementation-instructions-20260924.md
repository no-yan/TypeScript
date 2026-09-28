# Store checker 基盤 (TODO 7d-0) 実装指示書

作成日: 2026-09-24。対象は実装を担当するエージェント。設計の根拠は [store-checker-design-20260923.md](store-checker-design-20260923.md) (以下「7d 設計」。§番号はこの文書のもの)、[store-checker-regions-design-20260924.md](store-checker-regions-design-20260924.md) (以下「再設計案」)、[store-checker-execution-design-20260924.md](store-checker-execution-design-20260924.md) (以下「動作設計」)。再設計案と動作設計のうち 7d 設計が採らなかったもの (checker ごとの JSDoc reader、私有 StoreID の範囲分け、list の alias) は作らない。7d 設計が優先する。前段は 7c ([store-binder-verification-report-20260923.md](store-binder-verification-report-20260923.md))。検証の手順と判定は [store-checker-7d0-verification-instructions-20260924.md](store-checker-7d0-verification-instructions-20260924.md)。

設計判断は 7d 設計で済ませてある。書かれていない判断が必要になったら、実装を進めずに報告する。

2026-09-26 の [レビュー追加契約](store-checker-review-contracts-20260926.md) も適用する。7d-0 は private 名・gid・隔離した parse / bind メモリ計測、7d-2 は checker の symbol 対応・Pointer AST を保持しない H1 adapter・印字性能を担当する。

## 0. 作るものの性格

7d-0 は checker を動かさない。7d-1 (JSDoc) と 7d-2 (checker の変換) が載る **store の型と API** を作り、既存の parse / bind が同じ結果のまま動くことと、その費用を確かめる。目的は 4 つ:

1. **辺の foreign 解決** (FR1、K3): 合成ノードの子・親・list が別の Store のノードを複製せずに指せる。source の Store の辺はローカルのまま。
2. **id と所有者**: `NodeKey` と StoreID (Y3)、`File()` (F1 の見直し)、所有者を運ぶ解決層 (A1、V1)、`store.Symbol` の宣言を `Node` に (D2)、`gid` (I1)、`SameNode` (E1)。
3. **合成ノードの Factory / NodeRegion** (R): 満杯なら次の Store を足す。checker 無しの storage の操作列で、容量を 1〜3 にして境界を強制しても結果が変わらないことを示す。
4. **変換器の試作と `store/utilities.go` の出し直し** (§2.1 の移植の方法、U2、E3、P2)。binder.go で自動化率を測り、ast の utility を変換で出す。

これで **FR1 と V1 の hot path の費用が Walk / Parse / Bind の KPC で初めて実測になる** (7d 設計 §2.1)。

| | 7c | 7d-0 |
| --- | --- | --- |
| 辺 (slot、`parent`、list の要素) | 同じ Store の `NodeRef` だけ | `NodeRef` または最上位 bit を立てた foreign 表の index。source の Store は foreign 表を持たない |
| heap に保存する node の手掛かり | `Ref{file, id}` | `Node` (D2、N1')。map の key は `NodeKey{StoreID, NodeRef}` |
| node → symbol / flow / locals の読み | `Symbol() SymbolId`、`FlowNode() FlowRef`、`LocalsSlot()` + `Bound` で解決 | id 層 `SymbolId()` / `FlowRef()` / `LocalsRef()`、解決層 `Symbol() *Symbol` / `FlowNode() Flow` / `Locals() SymbolTable` |
| `store.Symbol` | `Declarations []Ref`、`ValueDeclaration Ref`、96B | `Declarations []Node`、`ValueDeclaration Node`、`gid` (64bit)、112B (大きさは計測の対象で、固定の条件にしない) |
| 合成ノード | 無い | `Factory` / `NodeRegion`、容量固定の Store を足していく |
| `store/utilities.go` | 手書き 165 関数 | 変換器の出力 + P2 の差し替え |

作らないもの: JSDoc の移植と遅延 cache (7d-1)、checker の変換、nameresolver、Store → Pointer の変換器、`EmitContext` の Store 版、H1 のハーネス (7d-2)。

## 1. ガード

1. 変更してよいのは `tsc/internal/ast/store/` (手書きと生成物)、`tsc/internal/storeparser/` と `tsc/internal/storebinder/` (§4 の API の追従だけ)、`tools/scripts/tsc/generate-go-store.ts`、新設の `tools/cmd/storetranslate/` (§5)。**`tsc/internal/ast` (store 以外)、`binder`、`parser`、`checker`、`scanner` と `tsc/go.mod` を変更しない。** 足したい API が出たら、足さずに報告する。
2. 7b / 7c の parse / bind の等価テストは、除外と緩めを増やさずに通す (比べる項目の型が変わる箇所だけ直す。§4 の 4)。
3. `unsafe` は `Node.Ref()` と list の要素の読み (`List` の raw 経路) だけ (7c と同じ)。foreign 表を引く経路に `unsafe` を入れない。
4. 生成物は `node tools/scripts/tsc/generate.ts` と `(cd tools && go run ./cmd/storetranslate)` (§5。`tools` は別の Go module) で再現すること。
5. 各 file の先頭の doc comment (英語) に、7d 設計の該当節へのパスを書く。**K3 の実装コメントは必須** (§3.6)。

## 2. ファイル

```
tsc/internal/ast/store/
  store.go            §3.1〜3.3 StoreID、NodeKey、辺の解決、SameNode、File()、List
  symbol.go           §3.4 Symbol (D2、I1)
  bound.go            §3.5 Bound.store、Flow
  factory.go          §3.6 NodeRegion、Factory、writer、NodeList.Clone
  idspace.go          §3.1 StoreID の採番
  utilities.go        §5 変換器の出力 (手書きの 165 関数を置き換える)
  utilities_manual.go §5 P2 の差し替え (hash 付き)
  node_helpers.go     §5.1 comparable helper の Node 版
  *_generated.go      §3.7
  storage_test.go     §6.2 storage の操作列
tsc/internal/storeparser/  §4 追従
tsc/internal/storebinder/  §4 追従、等価テストの手直し
tools/scripts/tsc/generate-go-store.ts  §3.7
tools/cmd/storetranslate/  §5
```

## 3. `store` package

### 3.1 StoreID、`NodeKey`、`File()` (Y3、F1 の見直し)

```go
type StoreID uint32

type NodeKey struct {          // 8B。旧 Ref{file, id}。links と map の key だけに使う
    store StoreID
    id    NodeRef
}
func (n Node) Key() NodeKey    // nil は NodeKey{} に正規化 (どの Store の番兵でも同じ key)
func (k NodeKey) StoreID() StoreID
func (k NodeKey) ID() NodeRef   // NodeKey から Node への逆引きは作らない

type Store struct {
    nodes        []NodeHeader
    extra        []uint32
    src, texts   string
    id           StoreID
    file         *File           // source と遅延 JSDoc の Store。合成の Store は nil
    bound        *Bound          // source の Store。bind 前と合成の Store は nil
    foreignNodes []Node          // 合成の Store だけ。source は nil
    foreignLists []List
    private      *privateTables  // 合成の Store の flow / symbol / locals の表 (§3.5)。source は nil
    sealed       bool
    owner        *regionToken    // 合成の Store の持ち主。source は nil (§3.6)
    space        *IDSpace        // program の token。全ての Store が持つ (§3.6 の取り込みの検査)
}
func (s *Store) ID() StoreID
func (n Node) File() *File
```

- `Ref`、`FileRef()`、`Ref.File()`、`Ref.Id()` を削る。使い手 (storebinder の 14 箇所ほど) は §4 で直す。
- **採番** (`idspace.go`): `type IDSpace struct{ next atomic.Uint32 }`。program ごとに 1 つ。source の Store は program が 0..nFiles−1 を渡す (今の `NewBuilder(src, file uint32)` の引数を `StoreID` にする)。それ以外 (遅延 JSDoc、合成) は `space.Next()` で nFiles から振る。番号は使い直さない。7d-0 のテストは program の代わりに `IDSpace` を直接作る。
- `File()`: `s.file != nil` なら返す (1〜2 load)。nil なら `Parent()` を辿り、`file` を持つ Store のノードに着いたらその file、nil に着いたら nil。作成時に処理中だった file や `Original` から推測しない (7d 設計 §2.3)。
- `File` に `Store` を渡すときに `Store.file` を設定する (storeparser の `finishSourceFile`)。File ↔ Store は循環してよい。

### 3.2 辺の解決 (FR1、K3)

`extra` の子の slot、header の `parent`、list block の要素の値を「辺」と呼ぶ。0 は nil、`< len(nodes)` はローカル、最上位 bit (`foreignBit = 1 << 31`) が立っていれば `foreignNodes` / `foreignLists` の index。

```go
func (s *Store) node(e NodeRef) Node {
    if uint32(e) < uint32(len(s.nodes)) {
        return Node{s, &s.nodes[uint32(e)]}
    }
    return s.foreignNode(e)          // 印を確かめ、無ければ panic。foreign 表の index も bounds check
}
```

- **`foreignNode` は inline される形にする** (呼び出しにしない)。7d 設計 §2.6 の計測で、遅い側を関数呼び出しにすると `Parent()` の inline cost が 86 で予算 80 を超えた。bit 検査と表引きを `node()` の中に書き、`node()` / `Parent()` / 生成の子 accessor が inline されることを `-gcflags=-m=2` で確かめる。超えるなら実装を止めて報告する。
- 印の無い範囲外の値は通常 build でも panic する (`s.foreignNodes[…]` の bounds check だけに頼らない: foreign 表を持つ Store で、壊れた小さい値が別の項目を黙って指すため)。
- local の行数は `foreignBit` 未満。Builder と Factory が行を足すときに検査する。
- list: slot の値も同じ規則 (`ListRef` か foreign)。`resolveList(slot) List` が正規の `List{owner, at}` を返す。**要素は list の owner の Store で解く** (親の Store で解かない)。
- foreign 表には正規の `Node` / `List` を入れる (foreign 表の項目どうしの連鎖を作らない)。重複の除去はしない。
- source の Store の Builder (`adopt` など) はローカル専用のまま。foreign の値を書かない。

### 3.3 `Node` と `List` の API

```go
func SameNode(a, b Node) bool         // a.h == b.h || (a.IsNil() && b.IsNil()) (E1)
func SameList(a, b List) bool         // owner と at の一致、両方 nil も一致
func (l List) Len() int
func (l List) At(i int) Node          // owner の Store で要素を解く
func (l List) Refs() []NodeRef        // raw。source の Store 専用。foreign 表を持つ Store では storeChecks で panic
```

- `Parent()`、生成の子 accessor、list の要素、生成 `ForEachChild` は §3.2 の解決を通る。header のスカラ読みと typed view の型の付け替えは変えない。
- **零値は [Z1 の設計案](store-node-zero-design-20260926.md) を参照する** (2026-09-26、本番未実装)。`IsNil` は `h == nil || h.kind == KindUnknown`、通常の getter と `node()` は維持する。D2 / N1' の field の番兵初期化を要求しない。Node の非比較型化と view の生成、nil の比較・key・辺エンコード、writer の番兵保護を一貫させ、Z1 §7 の受け入れ条件を検証する。Z1 の測定は IsNil-only overlay であり、この最終形の性能を保証していない。
- `Refs()` を使ってよいのは、source の Store を読む parse / bind の経路だけ (storebinder、storeparser)。変換器の出力 (utility) は `Len()` / `At()` を使う (§5)。

### 3.4 `Symbol` (D2、I1)

```go
type Symbol struct {
    Flags            ast.SymbolFlags
    CheckFlags       ast.CheckFlags
    Name             string
    Declarations     []Node           // []Ref → []Node
    ValueDeclaration Node             // Ref → Node。未設定 (零値) の扱いは §3.3 の注記
    Members          SymbolTable
    Exports          SymbolTable
    id               SymbolId         // Bound の表の local index 専用。private 名には使わない
    gid              atomic.Uint64    // GetSymbolId のプロセス全体の遅延採番。Pointer の ast.SymbolId は uint64 (ast/ids.go:5)
    Parent           *Symbol
    ExportSymbol     *Symbol
}
func GetSymbolId(s *Symbol) uint64   // ast.GetSymbolId と同じ形 (0 なら nextSymbolId を Add して CAS)。幅を縮めない: 32bit は周回すると未採番の 0 と過去の id を再発行し、型の cache の key で別の symbol を混同する
```

- 大きさは 112B の見込み (`ValueDeclaration` が +8B、`gid` の 64bit が +8B。96B → 112B)。**大きさは報告する計測値で、固定の条件にしない** (2026-09-24 のレビューで訂正: 当初は `gid` を 32bit にして 104B に収める指示だったが、Pointer の id は 64bit で、幅を縮めると同一性を損なう)。
- `singleDeclarationsArena` は `core.Arena[Node]`。`IsStatic(st *Store)` / `SymbolName(st, …)` の `*Store` 引数は不要になる (宣言が自分の Store を持つ) ので外す。

- `gid` は bind / checker が共用する非零・一意なプロセス内64bit遅延 ID。file / checker / benchmark iteration ごとに counter を reset しない。`GetSymbolId` は同じ symbol へ並列に初回呼び出ししても同じ ID を返す。Pointer と Store の数値一致は保証せず、比較契約はレビュー追加契約 §2 に従う。

### 3.5 所有者を運ぶ解決層 (A1、V1)

```go
// id 層 (bind と、source 専用の経路)
func (n Node) SymbolId() SymbolId        // h.symbol (旧 Symbol())
func (n Node) FlowRef() FlowRef          // h.flow (旧 FlowNode())
func (n Node) LocalsRef() (uint32, bool) // 旧 LocalsSlot()。生成
func (n Node) SetSymbolId(SymbolId) / SetFlowRef(FlowRef) / SetLocalsRef(uint32)   // 旧 SetSymbol / SetFlow / SetLocalsSlot。bind 専用

// 解決層 (Pointer と同名)
func (n Node) Symbol() *Symbol           // source: s.bound.symbols[id]、合成: s.private.symbols[slot]
func (n Node) FlowNode() Flow            // source: Flow{s.bound, ref}、合成: s.private.flows[slot]
func (n Node) Locals() SymbolTable       // 不在の kind は表と比較 1 回で nil (7c と同じ)
func (n Node) LocalSymbol() *Symbol
// 役割 (生成): ReturnFlowNode() / EndFlowNode() / FallthroughFlowNode() Flow

type Flow struct { bound *Bound; ref FlowRef }   // 16B
func (f Flow) IsNil() bool
func (f Flow) Flags() ast.FlowFlags
func (f Flow) Node() Node                // f.bound.store で解く。SwitchClause / ReduceLabel は FlowData を読む別の accessor
func (f Flow) Antecedent() Flow
func (f Flow) Antecedents() FlowList     // FlowList{bound, ref} を返し、Next / Flow も Bound を運ぶ
func (f Flow) SwitchClause() (switchStatement Node, clauseStart, clauseEnd int)
func (f Flow) ReduceLabel() (target Flow, antecedents FlowList)
```

- `Bound` に `store *Store` を足す (`FlowNode.Node` と `containers` などの `NodeRef` はこの Store の番号、不変条件 21)。
- 解決層の分岐 (`s.bound != nil`) は 1 つ。`privateTables` は scan される側表で、noscan の header / extra に pointer を入れない。未設定の slot 0 は nil。遅延 JSDoc の Store (7d-1) は `bound` も `private` も持たないので、全部 nil を返すこと (panic しない)。
- `Bound.Flow(r) *FlowNode` など裸の行 pointer を返す API は storebinder の中だけで使う (bind 中の slab)。`Seal` 後の読みは `Flow` を通す。

### 3.6 `NodeRegion`、`Factory`、writer (R、K3)

動作設計 §1・§3〜§5・§8 の形。**Store の全件表と、Store から Factory / Region / checker への逆参照を持たない。**

```go
type NodeRegion struct { token *regionToken; space *IDSpace }   // token は非ゼロサイズ
func NewNodeRegion(space *IDSpace) *NodeRegion

type Factory struct { region *NodeRegion; active *Store; capacity int; hooks FactoryHooks }
func NewFactory(r *NodeRegion, capacity int, hooks FactoryHooks) *Factory   // capacity はテストで 1〜3 を渡す
func (f *Factory) ReleaseArenas()        // active = nil だけ。古い Store の clear、seal、番号の使い直しはしない

// 生成: 定義ごとの New / Update / Clone (§3.7)
func (f *Factory) NewPropertyAccessExpression(expression, questionDotToken, name Node, flags ast.NodeFlags) Node
func (f *Factory) NewNodeList(nodes []Node) List
// writer: 引数のノードの Store に書く (active ではない)
func (f *Factory) SetParent(child, parent Node)
func (f *Factory) SetRange(n Node, pos, end int32)
func (f *Factory) AddFlags(n Node, flags ast.NodeFlags)
func (f *Factory) SetNodeFlow(n Node, flow Flow)
func (f *Factory) SetNodeSymbol(n Node, s *Symbol)
func (f *Factory) SetLocals(n Node, locals SymbolTable)
func (f *Factory) SetListElement(l List, i int, n Node)
func CloneNodeList(f *Factory, l List) List
```

- **New は子の `parent` を書かない** (Pointer の factory と同じ、不変条件 18)。header・payload・foreign 表の項目を全部書き終えてから hook を呼び、確保時に得た `Node` を返す (hook の再入で `active` が変わってもよい)。
- 満杯なら `space.Next()` で新しい Store を作り `active` を差し替える。古い header は動かさない。子・親が別の Store にあれば、**書き込み先の Store の** foreign 表に項目を足す。古い Store のノードに新しい Store の親を書くときは古い Store の foreign 表に足す。
- writer は、書き込み先が同じ region の合成の Store であることを通常 build でも確かめ、source の Store、seal 済みの Store、別の region の Store への書き込みは panic する。
- **取り込みの境界の検査** (2026-09-24 のレビューで追加): `New` / `Update` / `Clone` の引数、`SetParent` の親、`SetListElement` の要素、`SetNodeFlow` の Flow など、**別の Store のノードを foreign 表に入れる全ての入口**で、そのノードの Store が (a) 同じ program の source か遅延 JSDoc の Store (`Store.space == region.space`)、または (b) 同じ region の合成の Store であることを通常 build で確かめる。StoreID は program の中でしか一意でないので、別の program の同じ数値の StoreID のノードを取り込むと `NodeKey` が衝突する。別の checker (別の region) の合成ノードも拒否する。そのため `Store` に program の token (`space *IDSpace`) を持たせる (source の Store は Builder が、合成の Store は Factory が設定する)。同じ region の別の Factory のノードは許可する。
- `extra` は伸ばしてよい (offset で保存し、毎回 owner の現 slice から読む)。`texts` は末尾に足すだけで、既存の bytes を上書きしない。`nodes` は `capacity` で確保して伸ばさない。
- **`Update` は子と list を `SameNode` / `SameList` で比べ、無変更なら元のノードを返す** (Pointer の `updateNode` と同じく、新規確保も hook も起動しない)。**`Clone` は新しい root を作り、子と list を共有する** (浅い)。`parent`、symbol / flow / locals は元から写さない (Pointer の `cloneNode` と同じ)。`Flags` と `Loc` は写す。
- **`CloneNodeList` は要素の辺を写した新しい block を作る (K3)。実装コメント必須**: 「Pointer の `NodeList.Clone` は要素の slice を共有するが、ここでは写す。Clone した list の要素を書き換えて元の list に見えることに頼るコードは到達範囲に無い (2026-09-24 の調査、nodebuilderimpl.go:3104〜3115 の書き換えは nodebuilder が自分で作った list)。頼るコードが増えたら `SetListElement` の storeChecks の検査で落ちる。Pointer では Clone の元と先のどちらに書いても他方に見えるので、両方を拒否する」の趣旨。**`storeChecks` build では Clone の元と先の両方を記録し、どちらへの `SetListElement` も panic させる** (2026-09-26、ユーザー。先だけの拒否は `a=[x]; b=Clone(a); SetListElement(a, 0, y)` を通し、Pointer の `b[0]` は `y`、Store は `x` になる)。記録は `NodeRegion` の側の集合 (list の同一性を key) に置き、元の block には書かない。元が source や seal 済みの Store の list なら、writer の通常の検査が既に拒否するので記録は要らない。

### 3.7 generator (`generate-go-store.ts`)

生成物の変更は次に限る:

1. 子の slot と list の slot の読み (`views_generated.go`、`foreach_generated.go`) を §3.2 の解決に通す。list の要素は owner で解く。
2. 役割 accessor の名前 (§3.5): id 層 `LocalsRef` / `SetLocalsRef` など、解決層 `ReturnFlowNode()` / `EndFlowNode()` / `FallthroughFlowNode()` は `Flow` を返す。
3. `factory_generated.go` を新設: 定義ごとの `Factory.NewXxx` / `UpdateXxx` / `CloneXxx`。引数は `Node` / `List`、text は値。Builder の constructor (source 用、`parent` を書く) とは別物で、共有しない。
4. `storetest/equivalence_generated.go`: `bound` slot の除外は据え置き。
5. `convert` (Pointer → Store) は source 用の Builder を使うので変えない。

## 4. `storeparser` / `storebinder` の追従

1. `NewBuilder(src, file uint32)` → `NewBuilder(src, id StoreID)`。`finishSourceFile` で `Store.file = file`。
2. storebinder: `SetSymbol` / `SetFlow` / `SetLocalsSlot` / `Symbol()` / `FlowNode()` / `LocalsSlot()` を id 層の新しい名前に置き換える (bind は id 層を使う。解決層は `Seal` の後)。`symbolOf` ヘルパはそのまま `Bound.SymbolOf` を使う。
3. D2: `symbol.Declarations = append(…, node.FileRef())` → `append(…, node)`、`ValueDeclaration` も `Node`。`SetValueDeclaration(s, symbol, node)` の `*Store` 引数を外す。pattern ambient module の名前 (`pattern@<id>`、7c 設計 §4 の 13) は `node.Key()` の `StoreID:NodeRef` の 10 進にする。
4. 等価テスト (`bindequiv_test.go`): `Declarations` / `ValueDeclaration` の (kind, pos, end) の比較はそのまま `Node` で読む。`Ref.file` の検査 (7c 報告 §3.2 の 708,382 Ref) は「`Declarations` の各 `Node` の Store がその file の Store と同じ」に置き換える。
5. `Bound` の生成時に `store` を渡す。

5. **private 名**: `GetSymbolNameForPrivateIdentifier` を local `Symbol.Id()` から `store.GetSymbolId(containingClassSymbol)` に変更し、64bitの10進表記を使う。既存の同関数を単一の生成入口にし、7d-2 の checker の lookup / flow / relation も同じ関数へ変換する。別 file で local id が同じ class の同名 private が衝突しないこと、保存した Members / Exports の key と後続 helper の key が raw で一致することを追加テストで確認する。既存の数字を消す比較だけで合格にしない。

これ以外の書き換えをしない。storebinder の `binder.go` の hunk は、7c の diff に対して (a) 型と名前の置換に加え、上の private 名の ID source の変更を含む。この変更により bind でも gid の初回採番が発生するため、その費用を KPC に含めて報告する。

## 5. 変換器 (`tools/cmd/storetranslate`)

Go の program。`tools/go.mod` (x/tools v0.50.0 を持つ) に置き、`golang.org/x/tools/go/packages` で `tsc` の package を型付きで読む。`tsc/go.mod` は変えない。

### 5.1 規則 (7d 設計 §2.1、E1、F1、L1)

型から機械的に決まるものだけ。規則にならない書き換えは P2 (§5.3)。

| Pointer | Store |
| --- | --- |
| `*ast.Node` (と `ast.Declaration` などの alias) | `store.Node` |
| `x == nil` / `!= nil` (x がノード) | `x.IsNil()` / `!x.IsNil()` |
| `a == b` / `!=` (両辺がノード) | `store.SameNode(a, b)` / `!…` |
| `x.Kind`、`x.Flags`、`x.Pos()`、`x.End()`、`x.Parent`、`x.Loc` | `x.Kind()`、`x.Flags()`、`x.Pos()`、`x.End()`、`x.Parent()`、`x.Loc()` |
| `x.AsXxx().Field` (子) | `x.AsXxx().Field()` |
| `*ast.NodeList`、`list.Nodes` の range / index / len | `store.List`、`for i := range l.Len() { e := l.At(i) }`、`l.At(i)`、`l.Len()` |
| `ast.GetSourceFileOfNode(x)` | `x.File()` |
| `ast.GetNodeId(x)`、`map[*ast.Node]T` の key | `x.Key()`、`map[store.NodeKey]T` |
| `slices.Contains` / `Index` (ノードの slice) | `SameNode` で探す版 |
| `core.FirstNonNil` / `MapNonNil` / `OrElse`（零値比較の型引数が Node になるもの） | `store.FirstNonNil` / `MapNonNil` / `OrElse`。零値比較を `IsNil` にする |
| `core.SameMap` / `SameMapIndex` / `Deduplicate` / `AppendIfUnique`（比較の型引数が Node になるもの） | 同名の `store` の Node 版。等値比較を `SameNode` にする |
| map key / `collections.Set` の要素である key 専用 struct 内の Node field | field を `NodeKey`、構築・代入の Node 値を `Key()` にする。後述の使用箇所検査を必須とする |
| `*ast.Symbol`、`ast.SymbolTable`、`*ast.FlowNode` | `*store.Symbol`、`store.SymbolTable`、`store.Flow` |
| `x.Symbol()`、`x.Locals()`、`x.FlowNodeData().FlowNode` | `x.Symbol()`、`x.Locals()`、`x.FlowNode()` (2 層化で恒等) |
| `ast.Xxx(…)` (utility) | `store.Xxx(…)` |

**generic helper の規則（Z1）**: `go/types` の関数の同一性と実際の型引数で判定する。例えば `MapNonNil` は入力 T が Node というだけでは変えず、零値比較を行う結果 U が Node になるときに変える。Node 以外の呼び出しと core 本体は維持する。Node 版は `node_helpers.go` に置き、FirstNonNil / MapNonNil の入力 T は any、結果は Node、その他は Node / []Node に特化する。引数の評価順（OrElse の default 引数は eager）、callback の順序・回数と早期終了、要素順、nil slice、SameMap の変更がないときの元 slice の再利用、Deduplicate / AppendIfUnique の既存の再利用契約を保つ。Node を含む複合型の comparable 制約や一覧にない helper も列挙し、規則がなければ未対応として報告する。`any` への変換で回避しない。

**複合 key の規則（Z1）**: map key と `collections.Set` の実際の要素型を起点に、`go/types` の型・field の同一性と AST の使用箇所を調べる。既存の対象は `NonExistentPropertyKey.propNode` と `accessibleChainCacheKey.location`。どちらも同一性の key としてだけ使われ、Node として読み戻されないため、field は `NodeKey` にできる。keyed / positional literal、field 代入を漏れなく `Key()` に変換し、元の nil literal は `NodeKey{}` にする。nested struct / array は再帰的に調べるが、単に Node を含む全 struct を変えてはならない。key 以外の使用・Node としての読み戻し・解析範囲外への流出があって key 専用と確認できない場合は、key と値の分離など別規則が必要な入力として報告する。型情報だけで使用目的まで証明できるとは扱わない。

この2規則の変換テストを持つ。各 helper は零値・複数 Store の番兵・同一/異なる実ノードを混ぜ、判定だけでなく上記の評価・再利用契約も比較する。二つの既存複合 key は構築と lookup / Set 操作を回帰例にする。Node 以外の呼び出しが変わらない例と、Node を読み戻す key の未対応診断も確認する。

- 出力には元の関数の名前・順序・コメントを保つ。gofmt を通す。
- 到達可能性 (E3): RTA (`x/tools/go/callgraph/rta`) に全 package の `init` を根として足し、変換しない package (printer、parser、transformers、compiler、ls) の先は辿らない。7d-0 の根は §5.2 の 2 つの用途の入口。

### 5.2 7d-0 の 2 つの用途

1. **binder.go の試作** (自動化率): `internal/binder/binder.go` を変換し、7c の手書きの逐語版 (`_store-binder-results/binder-verbatim.diff` を `internal/binder/binder.go` に当てて復元したもの) と関数ごとに比べる。**出力は storebinder に使わない** (binder は手書きのまま、7d 設計 §2.1)。報告: 7c の hunk の分類 (a)〜(d) ごとの一致率、規則にならなかった箇所の一覧。線 (案) は「(a)〜(d) 相当の 9 割以上が一致」で、判定は検証が行う。7c の API 名 (`Ref`、`SetSymbol` など) は 7d-0 で変わるので、比較の前に名前の対応を 1 つの表で正規化する。
2. **`store/utilities.go` の出し直し** (U2): 根 = 今の `store/utilities.go` の 165 関数 + storebinder と storeparser が呼ぶ ast の関数。`internal/ast/utilities.go` などから到達する関数を変換して `store/utilities.go` に出す。

### 5.3 P2 の差し替え

`store/utilities_manual.go` に同名の関数があれば変換はその関数を出さない。各関数の直前に、書いたときの Pointer 版の関数本体の hash (`// storetranslate:pointer-hash <sha256 の先頭 16 桁> <file>:<func>`) を書く。変換器は hash を照合し、違えば止まる。

7c の (f) / (g) のうち utility に入れたもの (実装報告 §2 の #33〜#36、§3 の (g) 8 関数、§4.2 の 3 件) は、1 つずつ規則にできるかを見て、できなければ P2 にする。**(g) (読みのまとめ) は規則にしない** (7d 設計 §2.1): 変換の逐語の出力で 7c の等価テストが通るなら P2 にしない。1 件ごとに「規則 / P2 / 逐語で足りた」を報告に書く。

## 6. テスト

### 6.1 既存

7a / 7b / 7c の全テストを、tag なしと `-tags storechecks` で通す。parse / bind の等価の除外は 7c と同じ (JS 330 file、JS の緩め 2 件。7d-1 で外す)。

### 6.2 storage の操作列 (`store/storage_test.go`)

動作設計 §9 の操作を checker 無しで、手組みのノード (source は小さな TS を storeparser で parse + storebinder で bind、合成は `Factory`) で行う。**全部を `capacity` 1、2、3 と大きな値の 4 通りで回し、正規化した構造 (kind、辺の `SameNode` 関係、`parent`、`File()`、flow の所有者) が同じであることを比べる。** StoreID の数値の一致は求めない。

1. `this.x` の形: `NewKeywordExpression(This)` → `NewPropertyAccessExpression(this, nil, 源の名前, 0)` → `SetParent(this, ref)`、`SetParent(ref, 源の constructor)`、`SetNodeFlow(ref, 源の constructor の ReturnFlowNode())`。`ref.Name()` が源の名前と `SameNode`、源の名前の `Parent()` が変わらない、`this.Parent()` が `ref`、`ref.Parent()` が constructor、`ref.FlowNode().Node()` が源の Store のノード、`ref.File()` が源の file。
2. `getSyntheticElementAccess` の形: 源の式を子に持つ `ElementAccessExpression`、`SetParent(result, 源)`。子の `Parent()` が源のまま。
3. 古い Store のノードに新しい Store の親を後から書く (容量 1 で必ず境界をまたぐ)。保持していた `Node` と accessor の返す `Node` が同じ更新を読む。
4. `Update` の無変更 (別の Store の nil どうしを含む) で元のノードが返る。`Clone` が子と list を共有する (`SameNode` / `SameList`)。
5. `CloneNodeList` が要素の辺を写し、元の list と `SameList` でない。`storeChecks` build で Clone の先への `SetListElement` と、合成の Store にある Clone の元への `SetListElement` がどちらも panic する。Clone に関わらない合成の list への `SetListElement` は通る。
6. `SameNode` / `Key()`: 別の Store の nil どうしが等しく、`Key()` がどちらも `NodeKey{}`。
7. 壊れた辺: 印の無い範囲外の値と foreign 表の範囲外の index が、通常 build でも panic する。
8. writer の所有者: source の Store、seal 済みの Store、別の region の Store への `SetParent` などが通常 build でも panic する。
9. `ReleaseArenas`: 解除前に作った `Node` を読み、書け、解除後の `New` は別の StoreID の Store に入る。
10. fakeScope の形: `NewBlock` → `SetLocals(block, m)` → `SetParent(block, 源の宣言)`。`block.Locals()` が同じ map (後から足した key が見える)、`block.Parent().Locals()` が源の Bound の locals。
11. 大きさ: `unsafe.Sizeof` で `NodeHeader` 32、`Node` 16、`Flow` 16、`FlowNode` 16、`Symbol` を記録する (`Symbol` は報告値。見込み 112)。

2026-09-24 のレビューで足すもの:

12. **flow graph の独立な比較**: Pointer の flow graph (`ast.FlowNode`) と、V1 の API (`FlowNode()` / `Antecedent()` / `Antecedents()` / `SwitchClause()` / `ReduceLabel()` / `Flow.Node()`) だけで辿った Store の graph を、`Bound` の内部表を読まずに比べる。既存の bind の等価テスト (`bindequiv_test.go` は `Bound.Flow` などの内部表を直接読む) は残す。入力は分岐、ループ、switch、reduce label (try / finally)、nil を含む source。**2 つの Bound で同じ数値の `FlowRef` を使い** (2 file を bind する)、片方の Bound の flow を合成ノードに `SetNodeFlow` で付けて、その合成ノードから辿った graph がもう片方の Bound に解かれないことを確かめる。
13. **program の token**: 2 つの `IDSpace` (program A / B) で同じ数値の StoreID を持つ source の Store を作り、B のノードを A の region の Factory の `New` の子、`SetParent` の親、`SetListElement` の要素に渡すと通常 build で panic する。別の region (別の checker) の合成ノードも panic。同じ region の別の Factory のノードは通る。
14. **hook の再入**: `New` の hook の中で同じ Factory の `New` を呼び、容量 1 で `active` を差し替えさせる。外側の `New` が返した `Node` が確保時の Store のノードで、hook の中で作ったノードと取り違えないこと。
15. **`extra` と `texts` の強制再確保**: 容量を大きくしたまま `extra` と `texts` の初期 cap を最小にして、setter の途中で再確保を起こす (tuple の list の更新の形: 右辺でノードを連続して作ってから `SetListElement`)。保持していた `List` が最新の要素を読み、古い `Text()` の値が変わらないこと。
16. **`GetSymbolId` の周回**: counter を上限の直前に設定し、次の採番で 0 や過去の id を返さない (上限では panic する) こと。

17. **gid と private 名**: 同じ symbol への反復・並列初回呼び出しは同じ gid、別 symbol は異なる非零 gid。別 Bound で同じ local SymbolId の class、入れ子・同じ #x・static / instance を含め、private key の一意性と Members / Exports への lookup を数字の正規化なしで検査する。gid を先に求める class の順序を変えても宣言と lookup が整合すること。counter 境界の試験は隔離し、生きた symbol のある本番プロセスで reset する API を作らない。

### 6.3 正しさと性能を分ける

inline の判定、`Symbol` の大きさ、KPC の線は性能の判定で、正しさの判定とは別に扱う。**正しい実装が性能の線を外したら、その実装と測定結果を残して「性能未達」と報告する。安全の検査 (辺の印、取り込みの境界、writer の所有者) や共有の意味を弱めて線を通さない。** §3.2 の「inline されなければ止めて報告」は、この判断をユーザーに預けるためで、検査を外して inline させるためではない。

## 7. ベンチ

既存の `BenchmarkStoreWalkKPCV1` / `BenchmarkStoreParseKPCV1` / `BenchmarkStoreBindKPCV1` / `BenchmarkStoreBindRetainedV1` と ns 版を、名前を変えずに通す。**`BenchmarkStoreParseKPCV1` に dom の sub-benchmark を足す** (7d 設計 §2.2: checker.ts には JSDoc がほとんど無い。7d-0 では JSDoc は変わらないが、7d-1 の before になる)。

## 8. 完了条件

1. 生成物が再現し (`node tools/scripts/tsc/generate.ts`、`(cd tools && go run ./cmd/storetranslate)` の後に `git status` 不変)、`git status` の変更が §1 の範囲だけ。
2. `go build ./...` と `go vet ./internal/ast/... ./internal/storeparser/... ./internal/storebinder/...` が通る (tsc)。`go vet ./cmd/storetranslate/...` (tools)。production から store 系 package の import なし。
3. §6 の全テストが両 tag で通る。
4. §7 のベンチが完走。
   private 名の gid 初回採番の費用と、Pointer AST を持ち込まない variant 別プロセスの retained / GC を報告する。7d-2 の symbol 順序・印字性能・H1 の隔離は別の未完了項目として引き継ぐ。
5. 報告に: (a) `store` の API の変更一覧 (足したもの、改名、削ったもの) と §3 に無いものの理由、(b) generator の出力差分の要約、(c) `node()` / `Parent()` / 生成の子 accessor / `List.At` / `FlowNode()` / `Symbol()` の inline 判定と cost、(d) storebinder / storeparser の hunk の一覧 (§4 以外が無いこと)、(e) 変換器の規則の一覧、binder.go の試作の分類ごとの一致率と、規則にならなかった箇所、(f) `store/utilities.go` の出し直しで P2 にした関数と理由、7c の (f) / (g) の 1 件ごとの扱い、(g) storage テストの一覧と容量ごとの結果、(h) 7d 設計に書かれていない判断をした箇所 (無いはず。あれば実装を止めて報告したか)。
