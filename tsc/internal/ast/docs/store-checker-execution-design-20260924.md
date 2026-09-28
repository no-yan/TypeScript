# Checker の合成ノード・JSDoc の動作設計

2026-09-24。設計のみ。以下の API とコードは提案であり、まだ実装・実行していない。[領域設計](store-checker-regions-design-20260924.md)を、現在の checker の呼び出しに当てはめて具体化する。

今回確定するのは、Checker の初期化、二種類の合成参照、名前解決、lazy JSDoc、links、nodebuilder の印字と寿命。領域設計からの変更は **Store の全件保持をやめること** と **Factory ごとに割り当て中の Store を持つこと**。`ReleaseArenas` と実際の nodebuilder cache を追った結果による変更である。

## 1. Checker に置くものと API

```go
// 概念上の追加・置換部分。既存の型解決用 field は省略する。
type Checker struct {
    files   []*store.File
    nodes   *store.NodeRegion
    factory *store.Factory
    docs    *jsdocReader

    nodeLinks       nodeLinks[NodeLinks]
    typeNodeLinks   nodeLinks[TypeNodeLinks]
    symbolNodeLinks pagedNodeLinks[SymbolNodeLinks]

    // SyntheticExpression.Type のみ。store から checker を import しない。
    syntheticTypes map[store.NodeKey]*Type
}

// store 内部。token は identity のため非ゼロサイズにする。
type regionToken struct { marker byte }
type NodeRegion struct {
    token       *regionToken
    sourceSpace *SourceSpace  // Program の不変な source StoreID 名前空間
    nextStoreID StoreID
    // []*Store は持たない。NodeKey -> Node の逆引きも提供しない。
}
type Factory struct {
    region *NodeRegion
    active *Store
    hooks  NodeFactoryHooks
    // NodeCount / TextCount 等は従来の契約を保つ。
}
```

`SourceSpace` は source Store と所属 program の対応を、program 構築時に確定した読み取り専用情報として表す。checker ごとに source の番号を付け直さない。private StoreID はこの番号域の後ろに採番し、checker の生存中は再利用しない。source Store は共有、Factory が作る Store は同じ region token を持つ。

Store は所属 token、StoreID、配列、外部参照表を持つ。**Store から Factory / NodeRegion / Checker / hooks への逆参照は置かない。** token もそれらを参照しない。生きている Node が領域内の無関係な Store を生かし続けるのを防ぐ。

読み手の API は owner を内部で解決する。checker は保存先を選ばない。

| 操作 | 提案 API | 契約 |
| --- | --- | --- |
| ノードの構築 | `f.NewXxx(...Node, ...List) Node` | 生成された役割別 API。必要なら f.active を切り替える |
| 親の設定 | `f.SetParent(child, parent)` | child だけを更新する。parent 側は更新しない |
| 位置の設定 | `f.SetRange(node, range)` | node の Loc を設定する |
| flow の設定 | `f.SetNodeFlow(node, flow)` | `flow` は `store.Flow{bound, ref}` 相当。裸の FlowRef は受けない |
| symbol / locals の設定 | `f.SetNodeSymbol(node, symbol)` / `f.SetLocals(node, locals)` | 解決済みの *Symbol / map を私有表に保存 |
| list 要素の更新 | `f.SetListElement(list, i, node)` | list の owner と element storage を解決して書く |
| 親・子・名前・list の読み | `n.Parent()` / `n.Expression()` / `n.Name()` / typed accessor | local / foreign の区別を呼び手に返さない |
| 意味情報の読み | `n.FlowNode()` / `n.Symbol()` / `n.Locals()` | source は元 Bound、private はその Store の表を読む |
| 返却点の flow | `constructor.AsConstructorDeclaration().ReturnFlowNode()` | 通常の `FlowNode()` とは別 slot。返す Flow は owner 付き |
| links | `c.nodeLinks.Get(n)` / `c.symbolNodeLinks.Get(n)` | wrapper 内で n.Key() にする。checker 各所で key を手組みしない |
| JSDoc | `c.docs.JSDoc(host, optionalFile)` / `c.docs.EagerJSDoc(...)` | 後者は parse を起動しない |

writer は宛先が自分の region の可変 Store であることを確認する。import する Node / List は同じ region または同じ SourceSpace に限定する。別 checker の private ノードと別 program の source ノードは拒否する。source 用 Builder / binder の書き込みは別 API のまま。

Node 同士と List 同士の比較は `SameNode` / `SameList` を生成する。別 Store の nil sentinel 同士も等しい。`Node.Key()` も nil なら一律に zero NodeKey を返す。links の Get / Has / TryGet はこの同じ正規化を使い、Store ごとに異なる nil key を作らない。非 nil の比較は正規 owner と実体の一致であり、foreign 表の entry 番号ではない。

flow の保存層は具体的に次のように分ける。header の 32bit word は source では元 Bound の FlowRef、private ではその Store の `flowImports []Flow` の slot。writer は後者へ owner 付き Flow を登録して slot を書く。source 専用の raw FlowRef API は binder のみに残す。

```go
func (n Node) FlowNode() Flow {
    slot := n.h.flow
    if slot == 0 { return NilFlow }
    if n.s.bound != nil { return Flow{n.s.bound, FlowRef(slot)} }
    return n.s.flowImports[slot]
}
```

`ReturnFlowNode` も、その役割 slot を読む点以外は同じ解決規則。Flow の Node / Antecedent / FlowData の accessor は必ず返された Bound を使い、参照元の private Store で番号を解かない。JSDoc の未設定 slot は 0 なので Bound がなくても nil を返せる。Symbol / Locals も owner ごとの表で解決するが、外へ返すのは *Symbol / map でよく、さらに別の解決 context は要らない。

## 2. 初期化から file の検査まで

現在の `NewChecker` は最初に `program.BindSourceFiles()` を呼ぶ。`checker/checker.go:912`。この順を維持する。

```go
// NewChecker の既存の初期化へ差し込む位置を表す。
program.BindSourceFiles()
c.files = program.SourceFiles()
c.nodes = store.NewNodeRegion(program.SourceSpace())
c.factory = store.NewFactory(c.nodes, store.NodeFactoryHooks{})
c.docs = newJSDocReader(c.nodes, storeparser.ParseLazyJSDoc)
// この後で initializeClosures / initializeChecker を実行する。
```

Factory と JSDoc reader はまだ Store を確保しない。初回の New または lazy parse が確保する。nodebuilder 用 Factory は必要になった時に、その EmitContext の hooks で別に作る。**c.factory と nodebuilder の Factory は region を共有し、active Store と hooks は共有しない。** 現在も checker の Factory と printer/EmitContext の Factory は別である。`checker.go:674`、`checker/nodebuilderimpl.go:125`、`printer/factory.go:18`。

`GetDiagnostics(ctx, fileA)` → `checkSourceFile` → `checkSourceElements` は現状のまま。file 単位の `typeChecked` / `unusedChecked` も維持する。検査終了時に region、docs、links、private id を reset しない。次の `GetDiagnostics(ctx, fileB)` は同じ checker の状態を使う。`checker.go:2238,14179`。

同じ checker は既存 mutex の下で直列に使用する。別 checker は別 region と reader を持つ。キャンセル時は現状どおり `wasCanceled` を立てて以後の使用を拒否し、途中の node 番号だけ巻き戻して再利用する仕組みは作らない。`checker/utilities.go:1705`。

## 3. constructor の初期化検査で容量境界を越える

対象は `isPropertyInitializedInConstructor`。`checker/checker.go:5065`。例えば `class C { x: number; constructor() { this.x = 1; } }` の `x` について、checker は source にない参照 `this.x` を作り、constructor の最後まで流れた型を調べる。以下は名前が通常の Identifier の場合。

```go
func (c *Checker) isPropertyInitializedInConstructor(
    propName store.Node, propType *Type, constructor store.Node,
) bool {
    this := c.factory.NewKeywordExpression(ast.KindThisKeyword)
    reference := c.factory.NewPropertyAccessExpression(
        this, store.NilNode, propName, ast.NodeFlagsNone,
    )
    c.factory.SetParent(this, reference)
    c.factory.SetParent(reference, constructor)
    c.factory.SetNodeFlow(reference,
        constructor.AsConstructorDeclaration().ReturnFlowNode())

    flowType := c.getFlowTypeOfReferenceEx(
        reference, propType, c.getOptionalType(propType, false),
        store.NilNode, store.NilFlow,
    )
    return !c.containsUndefinedType(flowType)
}
```

ここで `this` を置いた S17 が満杯になった、とする。source の名前を P:x、constructor を P:ctor と書く。各 Store は sentinel を別に持ち、下の local id は実ノードの番号。

| 操作 | 保存される値 | 既存ノードへの影響 |
| --- | --- | --- |
| `NewKeywordExpression` | S17:1 = this | なし |
| `NewPropertyAccessExpression` | S18:1 = reference。Expression は S17:1、Name は P:x への foreign edge | S17:1 と P:x は複製しない |
| `SetParent(this, reference)` | **S17** の foreign 表へ S18:1 を追加し、S17:1.parent にその edge を保存 | this の親だけ変える。S17 が満杯でも可能 |
| `SetParent(reference, constructor)` | S18:1.parent は P:ctor への foreign edge | P:ctor は変更しない |
| `SetNodeFlow(reference, returnFlow)` | S18 の flow 表に `{P.Bound, returnFlowID}` を保存 | P.Bound は変更しない |

その後 `reference.Expression()` は最初の this そのもの、`this.Parent()` は reference そのものを返す。source の P:x の Parent は元の property declaration のまま。Factory は child の Parent を自動で付け替えない。

`getFlowTypeOfReferenceEx` は引数の flow が nil のとき `reference.FlowNode()` を読む。返るのは元の P.Bound を伴った Flow なので、antecedent を辿っても flow 内の Node を読んでも P の表で解決する。`reference.Parent()` は source constructor に到達するため、flow の既存の親判定も通常 API で動く。`checker/flow.go:81–111`。

computed property は元の分岐どおり ElementAccess を構築し、argument に `propName.Expression()` を渡す。static block はその型付き accessor の `ReturnFlowNode()` を渡す。同じ保存機構であり、特殊な root→host 表は足さない。

## 4. 分割代入では source の式を子として使う

対象は `getSyntheticElementAccess`。`checker/checker.go:18184`。constructor の例とは異なり、ここでコピーする flow は `parentAccess` の**通常の FlowNode**である。

```go
parentAccess := c.getParentElementAccess(node) // source または再帰で作った private Node
if parentAccess.IsNil() { return store.NilNode }
flow := parentAccess.FlowNode()
if flow.IsNil() { return store.NilNode }
propName, ok := c.getDestructuringPropertyName(node)
if !ok { return store.NilNode }

literal := c.factory.NewStringLiteral(propName, ast.TokenFlagsNone)
c.factory.SetRange(literal, node.Loc())
lhs := parentAccess
wrapped := !store.IsLeftHandSideExpression(parentAccess)
if wrapped {
    lhs = c.factory.NewParenthesizedExpression(parentAccess)
    c.factory.SetRange(lhs, node.Loc())
}
result := c.factory.NewElementAccessExpression(lhs, store.NilNode, literal, ast.NodeFlagsNone)
c.factory.SetRange(result, node.Loc())
c.factory.SetParent(literal, result)
c.factory.SetParent(result, node)
if wrapped {
    c.factory.SetParent(lhs, result)
}
c.factory.SetNodeFlow(result, flow)
```

`parentAccess` が source でも再帰で作った private node でも、そのまま子にする。**`SetParent(parentAccess, result)` は呼ばない。** source の名前解決と flow の照合が元のノードで続く。各 New の間で Store が切り替わっても、この操作列を変えない。

New の実装は、引数の評価後に宛先 Store を一度選び、header・payload・foreign entries を完成させてから OnCreate hook を呼ぶ。hook の再入で Factory.active が変わっても、返すのは確保時に保存した Node。setter も「今の active」ではなく、引数 node の Store に書く。

## 5. 名前解決と長寿命の値

nodebuilder の fakeScope は、次のように作る。元の位置は `checker/nodebuilderscopes.go:142–147`。

```go
scope := b.f.NewBlock(b.f.NewNodeList(nil), false)
b.f.SetLocals(scope, locals) // checker が作った同じ map を保持
b.f.SetParent(scope, b.ctx.enclosingDeclaration)
b.ctx.enclosingDeclaration = scope
```

`NameResolver.Resolve` は従来どおり `location.Locals()` を探し、見つからなければ `location.Parent()` を辿る。scope の locals は private 表から、source に達した後の locals はその source の Bound から返る。NameResolver に synth/JSDoc の種別判定や region 引数は追加しない。`binder/nameresolver.go:24–60`。

| 保存する値 | 保持先 | file 検査終了後 |
| --- | --- | --- |
| `newCallSignature` の FunctionTypeNode | `Signature.declaration` に正規 Node | Signature が使う限り Store は生存。Parent が nil なら File も nil |
| JSX children の PropertySignature | checker の transient `Symbol.ValueDeclaration` に正規 Node | symbol / 型が使う限り生存。Parent は source attributeParent |
| 型から作った AST | `SerializedTypeEntry.node` に正規 Node | 次の印字時に cache hit で読める |

JSX の書き込みは `symbol.ValueDeclaration = property`、`c.factory.SetParent(property, attributeParent)`、`c.factory.SetNodeSymbol(property, symbol)` の三操作。source symbol の declarations へ private Node を追加しない。D2 に従い store.Symbol の node fields は一律 Node に移す。transient symbol だけ別表現にする必要はない。`checker/jsx.go:846`。

## 6. JSDoc reader の状態と公開操作

```go
// store に置く小さい読み取り interface。実装は storechecker に置く。
type JSDocReader interface {
    JSDoc(host Node, file *File) []Node
    EagerJSDoc(host Node, file *File) []Node
}

// storechecker 内。引数 file は Pointer 同様、nil なら host.File()。
type docKey struct {
    file *store.File
    host store.NodeKey
}
type docEntry struct {
    state docState // parsing または ready。不在は map の !ok。
    roots []store.Node
}
type jsdocReader struct {
    region  *store.NodeRegion
    parse   func(*store.File, store.Node) *store.JSDocBatch
    entries map[docKey]docEntry
}
```

file も key に含める。Pointer は file ごとの map に host を入れており、明示 file を受ける API と同じ区分になる。source host の場合は冗長でも、private host の明示 file を黙って他の file の cache と混ぜない。通常の checker 呼び出しは file=nil、既に正しい file を持つ経路だけそれを渡す。`currentNode` や「検査中の file」を代用しない。

reader の境界では、host が同じ region または SourceSpace、明示 file が同じ SourceSpace に属することを通常 build でも確認する。host.File() が得られる場合は明示 file と一致しなければ拒否する。detached な private host は明示 file がある場合だけ parse でき、なければ nil を返す。この場合も root.Parent は実際の host のままで、parse に使った file を理由に架空の Parent を足さない。通常の checker の source host 読みはこの例外を必要としない。

JSDoc reader の遷移は以下で固定する。roots の返却 slice は借用・読み取り専用とする。

| 状態・入力 | `JSDoc` | `EagerJSDoc` |
| --- | --- | --- |
| host が nil / HasJSDoc なし / file が得られない | nil。cache を作らない | 同左 |
| source の eager 索引に entry がある | 空を含めてその結果 | 同左 |
| JS の eager 索引に entry がない | nil。lazy parse しない | 同左 |
| TS、私有 entry が ready | roots を返す。nil の roots も完成結果 | 同左 |
| TS、私有 entry がない | parsing を記録し、parse・公開後に ready | nil。状態は変えない |
| 同じ key が parsing | 内部契約違反として失敗。未完成結果は返さない | 未完成結果は見えないので nil。parse しない |

source の eager 索引は完成後に変えない authoritative な結果であり、その entry がある key について私有 cache entry は作らない。source eager と私有 ready のどちらを採るかが時刻によって変わる状態を作らない。

parser と公開の責務を次の二 API に分ける。

```go
// storeparser。source の bytes/options と本物の host を読み、非公開の完成 batch を返す。
func ParseLazyJSDoc(file *store.File, host store.Node) *store.JSDocBatch

// store。batch の所有権を一度だけ受け取り、採番・親接続・seal の後に Node を返す。
func (r *NodeRegion) PublishJSDoc(batch *JSDocBatch) []Node
```

batch は source text、host、完成した columns、root ids を内部に持つ。呼び手は columns の Node view を取得できない。parser は全 comment を Builder 内で作り、`Finish` で scratch から独立させる。途中の構築は local id を使い、Builder の可動 header を checker に返さない。必要な host の読み取りは本物の Node を別入力で行う。

`PublishJSDoc` は StoreID を採番し、全 root の Parent を host の foreign edge にし、内部の親子関係を含めて完成した後に seal し、初めて root Node 列を返す。その時点でのみ ready entry を保存する。空結果は ready(nil) にして Store を保持しない。Publish は batch を消費し、同じ batch の再公開は拒否する。完成 Store を region の全件表には追加しない。cache の roots がそれを保持する。

parse 中の panic では ready を書かず、defer で parsing entry を削除して panic を伝える。文法的に誤った JSDoc は通常の parser の回復結果として扱う。lazy TS の parser diagnostics を source file へ後付けしない。現 Pointer の `parseJSDocComment` は一時診断を戻し、JS のときだけ jsdocDiagnostics に移す。`parser/jsdoc.go:170–175`。

## 7. checker が JSDoc を読む二経路

### 関数の @param を検査する

```text
GetDiagnostics(fileA)
  checkSourceFile → checkSignatureDeclaration(function)
    checkUnmatchedJSDocParameters
      c.getAllJSDocTags(function)
        c.docs.JSDoc(currentHost, nil)

GetDiagnostics(fileA)                       // fileB の関数式に届く経路
  getTypeOfSymbol(fileB の型注釈の無い宣言)
    checkDeclarationInitializer / getReturnTypeFromBody
      checkFunctionExpressionOrObjectLiteralMethod(fileB の関数式)
        contextuallyCheckFunctionExpressionOrObjectLiteralMethod
          checkSignatureDeclaration → checkUnmatchedJSDocParameters
            c.docs.JSDoc(hostB, nil)
```

二つ目は `contextuallyCheckFunctionExpressionOrObjectLiteralMethod` (`checker.go:10392`) が型計算のついでに `checkSignatureDeclaration` を呼ぶ経路で、`/** doc */ export const f = (x: number) => x` のような宣言を fileA から参照するだけで fileB の JSDoc を parse する。tag の門前払いは無い。fileB を担当する checker も同じ host を parse するので、private cache では checker 間で重複する (2026-09-27 追記。当初は検査中の file に閉じる経路として書いていた)。

`getAllJSDocTags` は checker の method にし、祖先を調べる `GetNextJSDocCommentLocation` のロジックは変えない。`current.JSDoc(nil)` の部分だけ `c.docs.JSDoc(current, nil)` に替える。全 root を cache するが、既存 helper は最後の JSDoc の tags を選ぶので、その選択も維持する。`checker/jsdoc.go:86`。

初回の cache miss では §6 の公開操作を行う。次の lookup は同じ root/tag Node を返す。tag 内の name の位置を診断に使うと、通常の `name.Parent()` → tag → JSDoc root → host → source で host の file に着く (二つ目の経路では fileA ではなく fileB)。cache key の file も host から選び、検査中の file を使わない。文字位置は parser が source text 全体に対して付けたものなので補正しない。

TS の不一致 @param は経路によって suggestion になる。`GetDiagnostics` の戻り値に必ず現れるとはしない。既存の `errorOrSuggestion(isJs, ...)` を維持し、`GetSuggestionDiagnostics` での結果も等価検証する。`checker/jsdoc.go:69`。

### fileA の検査中に fileB の @deprecated を読む

`Checker.IsDeprecatedDeclaration` から、cached flags と reader を free utility へ渡す。

```go
return store.IsDeprecatedDeclarationWithCachedFlags(
    c.docs, declaration, c.getCombinedNodeFlagsCached(declaration),
)
// related information を作る側も同じ reader を使う。
tag := store.GetJSDocDeprecatedTag(c.docs, declaration)
```

cheap flag がなければ従来どおり parse しない。flag がある祖先 hostB に到達した場合は `hostB.File()` から fileB を選び、fileA を使わない。cache key は `(fileB, hostB.Key())`。診断本体は fileA の使用位置、related information は fileB の deprecated tag の位置になる。`checker.go:14261–14278`、`ast/utilities.go:1246`。

別 checker が同じ hostB を読むと、その checker の reader でも一回 parse する。JSDoc の NodeKey は checker 間で交換しない。一方 source の eager JSDoc は共通の Node を読む。

`checkSourceElementWorker` の `EagerJSDoc` と JS augments 検査も reader に付け替える。前者の JSDoc comment 検査は `@link` による参照記録を行う。`@see` / `@link` は source の eager parse に残すため、別 checker の lazy cache が見えないことを補うための追加 parse はしない。`checker.go:2297,2583,4426`。この所有範囲の差は、checker 間で検査順序を変えた等価試験にも含める。

JSDoc を読む残りの free utility には reader を明示引数で足す。具体的には `GetJSDocDeprecatedTag`、二つの `IsDeprecatedDeclaration` 系、`GetNodeAtPosition`、`ForEachChildAndJSDoc`。後ろ二つは変換の到達範囲に残る場合だけ出す。`GetNodeAtPosition(..., false)` は reader を受け取っても lookup を行わない。NameResolver と純粋な Node accessor 全体に context を増やさない。

## 8. links と nodebuilder の印字後の寿命

links の呼び出し側は Node を渡す形のままにする。

```go
func (s *nodeLinks[V]) Get(n store.Node) *V {
    return s.inner.Get(n.Key()) // inner は core.LinkStore[NodeKey, V]
}
func (s *pagedNodeLinks[V]) Get(n store.Node) *V {
    key := n.Key()
    pageStore := s.ensureStoreSlot(key.StoreID)
    return pageStore.Get(uint64(key.LocalID))
}
```

`ensureStoreSlot` は links の slot/page metadata を必要時に増やすだけで、Store 自体への pointer を保存しない。NodeKey は同じ region に属する private map の中だけで使う 64bit の identity。別 region に map/key を持ち出す API は作らない。入口と writer で所有範囲を検査し、内部 links も storechecks でこの前提を検査する。将来 shared map を作るなら、そこで初めて region identity を key に含める。

**NodeKey から Node を復元する機能は設けない。** 後でノードを読む cache は key だけでなく Node を値に保存する。store.Symbol は D2、Signature と `SerializedTypeEntry` は N1' によりすでにこの形にする。これにより、全 Store registry が不要になる。

印字は現在の `typeToStringEx` に沿って次の順とする。`checker/printer.go:59`。

1. nodebuilder を取得し、既存の再入回数制限と context stack を使う。
2. nodebuilder 専用 Factory で Store AST を作る。source の再利用と private の cached AST は通常の Node 入力になる。
3. 一回の print adapter が、必要な Node / List / EmitContext の情報を同じ対応表で Pointer へ移す。source / Original / text / range も Store から変換できるものとし、比較用 Pointer source 対応は必須依存にしない。
4. Pointer printer に渡して文字列を得る。adapter の一時対応表を checker の永続 cache に入れない。
5. defer の `nodebuilder.Factory.ReleaseArenas()` で、その Factory の `active = nil` にする。hooks、region、累積カウンタは保持する。

これは現在の `ast.NodeFactory.ReleaseArenas()` が arena への保持だけを外す意味に対応する。**古い Store の clear、seal、id 再利用、返却済み Node の無効化は行わない。** `ast/ast.go:3183`。

実際に `SerializedTypeEntry.node` は印字を越えて Node を保持し、cache hit ではその Node に `DeepCloneNode` を行う。`nodebuilderimpl.go:35,3233–3271`。したがって「nodebuilder 出力はすべて print 後に捨てられる」と仮定してはいけない。この cache が生きる Store は残り、どの Node / cache / foreign entry / Factory からも到達できなくなった Store は Go GC の対象になる。

再入した TypeToString が同じ nodebuilder Factory の active を外しても、外側が保持する Node はその Store を保持する。外側が次に New すると新しい Store を使う。一時的な `getNodeBuilderEx` は専用 Factory ごと到達不能になれるため、checker の region がその最後の Store を引き留めることもない。escape 検出・別の print 寿命型・promotion は導入しない。

この形でも Store 単位の保持と foreign 参照の連結による余分な retained memory は残る。NodeKey だけの古い cache entry / page も自動削除しない。GC 可能性を確保する設計であり、Pointer と同量のメモリになる保証ではない。

2026-09-26 の [レビュー追加契約 §3〜4](store-checker-review-contracts-20260926.md) に従い、retained / GC は比較用 Pointer AST を持たない variant 別プロセスで測る。印字性能は上の2〜4の nodebuilder・adapter・printer・結果取得を全て含め、cache miss / hit と診断ありの実入力を区別する。印字に必要な一時 Pointer AST は測定対象であり、比較用 AST とともに区間外へ追い出してはならない。

## 9. 実装箇所と確認する操作列

| 場所 | 具体的な変更 |
| --- | --- |
| `store` の Factory / NodeRegion | 安定 header、foreign import、writer、Factory 別 active、ReleaseArenas、JSDocBatch の公開 |
| `store` の Node / Flow / List と生成器 | owner 付きの読み、SameNode / SameList、通常 flow と ReturnFlow の区別。内部 list 共有は領域設計を使用 |
| `storeparser` | host と file を受けて非公開の JSDocBatch を返す。source parse/reparse は従来の Builder |
| Program / H1 adapter | source StoreID と program の所属を表す読み取り専用 SourceSpace を供給。private Store の registry は持たない |
| `storechecker` 初期化・links | region/Factory/reader の生成、Get(Node) wrapper、NodeKey は private scope に限定 |
| checker と utility の変換 | Parent/Loc/Flow/Symbol/Locals/list の書き込みを writer に変換。JSDoc 読みだけ明示 reader を渡す |
| Store 版 EmitContext / nodebuilder | Factory の生成時に c.nodes を渡し、既存 hooks を維持。ReleaseArenas は保持解除 |
| printer 境界 | 同一性と metadata を保つ adapter。これ自体の各 kind の実装は NB1 の別の生成作業 |

最初に以下の操作列を storage 単体で実行できるようにし、その後 checker を移植する。容量を実ノード 1 行にして各 New 間の Store 境界を強制する。物理 StoreID の数値一致は要求せず、Node の共有・親・flow の owner と出力を比較する。

- constructor の合成 this と reference を別 Store に置く。古い Store の this.Parent を後から設定し、両側の accessor と保存済み Node が同じ実体を返す。
- 分割代入の source parentAccess を子に使う。source の Parent / flow / symbol を変えず、result には正しい owner の flow が付く。
- fakeScope の map を更新してから Resolve を繰り返す。private scope の更新と、source への祖先探索の双方を観測する。
- JSDoc を初回 lookup、再 lookup、空結果、Eager-before/after の順で読む。parse 回数、同一 Node、root.Parent、file、診断の位置を確認する。
- fileA の使用位置から fileB の deprecated 宣言を読み、別 checker でも同じ順・逆順に検査する。private cache の独立性と診断本体 / related information の file を確認する。
- fileA から fileB の型注釈の無い関数式 (`/** doc */ export const f = (x: number) => x`) を参照し、fileA だけ検査して fileB の host の JSDoc が parse されること、その後 fileB を別 checker と同じ checker で検査したときの @param 診断の file と件数を確認する。
- `ReleaseArenas` の前に Node を手元と `SerializedTypeEntry` に保存する。解除後に読み・更新・DeepClone が可能で、次の allocation は別 StoreID になる。
- 再入した TypeToString の内側で ReleaseArenas を呼び、外側の保持済み Node に新しい子を設定する。外側の参照・hooks/context が変わらない。
- storage の所有グラフを検査して、region から全 Store への保持がないことを確認する。実 heap の回収量は後の retained/GC 測定で検証し、即時回収や finalizer の実行時刻を正しさの条件にしない。

checker の等価試験は通常診断だけでなく suggestion と related information も含める。実際の診断を伴う nodebuilder cache、明示 DeepClone、Original/comments を通す。性能はまだ未測定であり、Factory ごとの chunk 使用率、foreign 表、JSDoc Store 数、残る cache/page を計数してから既定容量を決める。固定の容量値を正しさの根拠にはしない。
