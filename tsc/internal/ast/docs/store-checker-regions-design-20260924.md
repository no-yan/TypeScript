# Checker の JSDoc・合成ノード領域の再設計

2026-09-24。設計提案。実装・性能検証は未実施。

具体的な Checker の API、呼び出し順、容量境界の状態、JSDoc の公開手順は[動作設計](store-checker-execution-design-20260924.md)に記載した。nodebuilder の `ReleaseArenas` と cache を追い、この文書も「region が全 Store を保持する」初案から、Factory ごとの割り当て先と実 Node の到達可能性で寿命を決める形へ更新した。

対象は [2026-09-23 の checker 設計](store-checker-design-20260923.md) の §2.2、§2.3 F1、§2.4 の Store 採番、§2.6。今回の「本質的でない制約を外してよい」という依頼に基づき、過去の選択を再検討する。過去にユーザーが決めた内容を、新しいユーザー承認として扱うものではない。

提案は、**checker ごとに一つの所有領域を置き、その内部で複数のアドレスが安定した Store を管理する**こと。source AST は共有・不変のままにする。合成ノードの親・子・リストは source や別の Store の実物を参照できるようにし、容量境界での複製をなくす。lazy JSDoc は host ごとに非公開の Builder で完成させてから公開する。共通にするのは参照の意味と所有権であり、構築方法まで同じにする必要はない。

## 1. 何が制約で、何が設計変数か

### 守る意味

| 要件 | 根拠・破ると起きること |
| --- | --- |
| ノードの同一性を保存する | `getSyntheticElementAccess` は source の `parentAccess` をそのまま子にする。名前解決・flow・links がそのノードを読む。深い複製は意味を変える。`checker/checker.go:18184` |
| `Parent()` は実際の親を返す | 合成式の親は source、`fakeScope.Parent` は enclosing declaration、JSDoc root の親は host。これらを切ると祖先探索が変わる。`checker/nodebuilderscopes.go:146`、`parser/jsdoc.go:36` |
| 子の共有と親の設定は別操作 | Pointer の `NewXxx` は子の親を自動変更しない。共有した source の子を新しい親へ付け替えてはいけない。合成 AST は「全 child の Parent が自分」という木の不変条件を持たない。`ast/ast_generated.go:678`、`checker/checker.go:18196` |
| 同じ引数による Update は同じノードを返す | Pointer の Update は node/list の同一性を比較する。Clone は新しい root と既存の子・list を共有する。`ast/ast_generated.go:685` |
| 公開した `Node{s,h}` の `h` は生存中ずっと同じ行を指す | `Node.Ref()` も Store の base から逆算する。公開後の nodes 移動は、読み・書き・id を壊す。`store/store.go:52,111` |
| checker の私有ノードを共有 AST・共有 symbol に書き戻さない | 並列 checker の結果・寿命・id 名前空間が混ざる。Go の GC が回収を防いでも、所有関係や競合は直らない |
| file の切り替えは解放の境界ではない | 合成宣言が `Signature.declaration`、JSX の transient symbol、checker cache に残る。`checker/checker.go:30917`、`checker/jsx.go:846` |
| キャッシュの不在と計算済みの空結果を区別する | Pointer の `resolveJSDoc` は map の `ok` を見る。空の JSDoc を繰り返し parse してはいけない。`ast/ast.go:2745` |
| 参照の所有者を保つ | `NodeRef`、`ListRef`、`FlowRef`、`SymbolId` はそれぞれの表に対する番号。同じ整数を別 Store / Bound にコピーしても同じ対象にならない |

### 外してよい制約

| 現在の選択 | 再設計での扱い |
| --- | --- |
| checker ごとに物理 Store を一つだけ持つ | 一つ必要なのは所有領域。物理 Store の個数は allocator の詳細にする |
| 全合成ノードを予測した固定容量に収め、超えたら panic | 各 nodes 配列は固定するが、満杯なら別 Store を追加する。コーパス最大値を正しさの条件にしない |
| Store 境界を越えると深い複製 | 参照をそのまま保存する。明示的な Clone のみ、新しい root を作る |
| 全辺がローカルで、すべての accessor に foreign 処理がない | source に保存する辺はローカルのまま。共通の辺解決関数には外部参照の経路を認める。費用ゼロは要件にしない |
| `nodes`・`extra`・`texts` をすべて固定する | 固定が必須なのは公開した header の backing array。payload は offset、文字列は不変 bytes を参照する契約で伸ばせる。詳細は §4 |
| Store の id は source file の id でもある | 保存先の `StoreID` と意味上の `File()` を分ける。合成 Store は複数 file のノードを持てる |
| lazy JSDoc は (checker, file) ごとに一つの増築する Store | 最初は host 一件の parse 結果ごとに完成済み Store を公開する。結合は測定後の最適化候補 |
| lazy root の Parent を nil にし、特別な `JSDocHost()` で上がる | root に本物の host への参照を保存する。通常の `Parent()` と既存の `GetJSDocHost` が成立する |
| `host.JSDoc()` が Node 単体から checker を発見できる | checker 所有の resolver を明示的に渡す。共有 Store に「今の checker」を書かない |
| 合成ノードは生成後に不変 | 実コードは `Parent`、位置、flags、flow、locals、list 要素を更新する。安定アドレスと不変性を混同しない |

一方、JS reparse / TS の `@see`・`@link` を parse 本体に置くこと、TS の残りを lazy にすること、Node の 16B 表現、32B の noscan header、NB1 の Store 出力、生成による移植は維持する。これらを同時に変える必要はない。

## 2. 所有領域と保存先を分ける

概念上の境界を以下にする。名前は提案であり、現在の API ではない。

```text
Program
  source File -> 完成した source Store + Bound   複数 checker が読む

Checker
  NodeRegion
    所属 token・source 名前空間・単調な StoreID 採番
  synthetic Factory / nodebuilder Factory
    それぞれ割り当て中の Store だけを保持
  JSDocResolver
    (file, host の正規 identity) -> 未計算 / 完成結果（空を含む）
  links / Type / transient Symbol / EmitContext
    正規 Node または (StoreID, local NodeRef) を保持
```

`NodeRegion` は所属と採番だけを管理し、全 Store の表を持たない。Factory はそれぞれ割り当て中の Store を保持し、Node・cache の Node 値・foreign 表は実際に必要な Store を保持する。Store から region / Factory / Checker への逆参照は置かず、非ゼロサイズの独立した token で所属を表す。checker 内では直列に構築し、別 checker とは region/cache を共有しない。checker pool の checker 別 lock という現契約と一致する。`compiler/checkerpool.go:445`。

nodebuilder の `ReleaseArenas` はその Factory の割り当て先への保持だけを外す。古い Node の無効化や id の再利用はしない。`SerializedTypeEntry.node` 等が Node を保持していれば Store も生存する。全 Store の寿命を checker の寿命に揃える必要も、print 後にすべて消せるという仮定もない。詳細は動作設計 §8。

StoreID の採番は親設計 Y3 と不変条件22に従う。checker ごとに番号を重複させる旧案は採らない。2026-10-04 の 7e 改訂では、native Program の source・JSDoc・合成 Store を Program の同じ IDSpace から初回作成時に採番し、その後は変更・再利用しない。全 source 数が既知の fixture/H1 は source の固定予約方式を維持できる。SourceFiles の順序と Store の種類は ID の範囲から推定しない。詳細は [7e 設計 §4.3](store-cli-design-20261004.md)。

map の key は正規化後の `(StoreID, NodeRef)`。paged links は `links[StoreID].pages[localID >> pageBits]` とし、packed 64bit key を直接 page index にしない。私有 Store が増えてもページは初回使用時にだけ確保する。外側 slot の確保も links wrapper に閉じ込め、Store 自体への保持や NodeKey から Node への逆引き表を置かない。

型は `NodeRef` = local row、`NodeKey{StoreID, NodeRef}` = Program 内で一意な links 用、`Node` = owner 付きの実物と分ける。現 `Ref{file,id}` / `FileRef()` は `NodeKey` / `Key()` へ改名し、`Ref.File()` を診断の file として利用する余地をなくす。裸の `Node.Ref()` は store 内部と local-only の builder/binder API に限定する。D2 の `Symbol.Declarations` / `ValueDeclaration` は owner を持つ Node なので、再解決用の整数表を増やさずに済む。

## 3. 辺は所在を表し、解決結果は常に実物を返す

### Node の辺

`NodeRef` は「Store 内の実在する行の番号」に限定する。parent / child / list element に保存する値は別型 `NodeEdge uint32` とする。0 は nil、最上位 bit が 0 なら local NodeRef、1 ならその Store の `foreignNodes []Node` の index。foreign 表には**解決済みの正規 Node**を入れ、別の foreign entry を指す連鎖は作らない。

```go
// 概念コード。通常 build でも不正な index を黙って別表に解釈しない。
func (s *Store) resolve(e NodeEdge) Node {
    if uint32(e) < uint32(len(s.nodes)) {
        return Node{s, &s.nodes[uint32(e)]}
    }
    if uint32(e)&foreignBit == 0 {
        panic("invalid local node edge")
    }
    return s.foreignNodes[uint32(e)&^foreignBit]
}
```

これは添付の FR1 を安全条件まで明示したもの。無印の範囲外 ref を foreign として誤読しない。foreign index にも通常の bounds check を残す。local row 数は最上位 bit を使わない範囲に制限し、書き込み時の変換・長さ計算の overflow を検査する。nil は import 前に 0 に正規化する。変換器の nil 比較は `IsNil()`、Node 同士は `SameNode(a,b)` とし、「両方 nil、または同じ正規 Node」を返す。list も `SameList` を使う。単なる struct の `==` では、別 Store の nil sentinel 同士が異なり、無変更 Update まで変わるためである。nil の key も一つに正規化する。

Factory の `encodeNode(destination, node)` は、同じ Store なら local id、別 Store なら foreign entry を返す。**他 Store の子・親を指すだけで、複製しない。** foreign entry の重複排除は初期実装では不要。entry 番号を同一性として使わないので、同じ Node が複数登録されても意味は同じ。表サイズが問題になった場合にのみ interning を検討する。

`Parent()`、生成 child accessor、list の要素解決、walk はこの `resolve` を使う。scalar header 読みや typed view の cast は変えない。checker は foreign の bit や表を見ない。source Builder の `adopt` などは local NodeRef 専用のままでよい。foreign 対応 Factory と混用しない。

### リストも実物へ解決する

Node だけを対応させても浅い Clone は完成しない。source の `Parameters` 等を新しい Store に毎回コピーすると、Pointer の list identity と Update の判定が変わる。

list slot にも `ListEdge uint32` を設ける。local は従来の `extra` 内 block、foreign は `foreignLists []List` の正規 handle を指す。accessor は `s.resolveList(slot)` で canonical な `List{owner, at}` を返す。`ListRef` は local offset の意味に限定する。list の不在と空 block は引き続き区別する。

生成 walker は parent の Store で foreign list の要素を解決してはいけない。まず list を解決し、その list の owner で要素を読む。これを生成器の list アクセス規則に集める。既存の手書き `store/utilities.go` にも `Refs()` を直接読む箇所があるため、checker から到達する utility は同じ契約に直す。generator の変更だけで既存呼び出しがすべて直るとはしない。

**`Node.Clone` による list の共有と、明示的な `NodeList.Clone` は別の契約。** 後者は新しい list identity を作り、現 Pointer 実装では要素 slice は共有する。`ast/ast.go:147`。`checker/nodecopy.go:884` ではその Clone を行って位置だけを変更する。その共有を必要とする Clone では、新しい list descriptor が元の element storage を参照する形を私有 list 内部に用意する。位置は descriptor ごと、要素更新は共有先に作用する。単純な block 全体コピーで代用しない。

具体的には、local list block の length word の最上位 bit を alias 印とし、残る bit を長さ、pos/end を従来どおり保持し、追加の一 word を `listElements` 表の index にする。表の entry は canonical な要素格納先 `(Store, offset)` を保持する。alias の Clone は解決済みの格納先を引き継ぎ、連鎖しない。通常 list の要素は従来どおり block の直後に並ぶ。`At` / `SetAt` がこの違いを隠すが、一般の list 読みには判定の費用が加わるため測定対象とする。`Refs()` の source 専用経路は alias を受け取らない。

## 4. 合成 Factory は容量境界を隠す

Factory は自分の active Store に nodes を追加する。nodes が満杯になったら region から新しい StoreID を取得して Store を作り、以後の新規ノードをそちらに置く。古い header は移さず、番号も再利用しない。古い Store のノードを更新する場合も元の Store に書く。新しい子が別 Store にあれば、その**古い Store の** foreign 表に参照を追加する。Store が回収可能になるのは、Factory を含めて実際の参照がなくなったときだけである。

これにより、満杯は「新規ノードを置かなくなる」だけであり、`Seal` ではない。shared source / 完成済み JSDoc の Seal と、synthetic Store の満杯を分ける。checker-owned Factory が持つ書き込み権限で更新対象を限定し、共有 source や別 region への書き込みは通常 build でも拒否する。全読み accessor に所有権検査を足す必要はない。

header chunk の大きさは調整可能な性能変数。境界を小さくしても出力・同一性・名前解決は変わらない。大きな初期容量、入力ごとの上限予測、逆参照による既存ノードの一括更新はいずれも不要。上限が残るのは uint32 の表現限界と利用可能メモリであり、便宜的な「10 万ノードで落ちる」上限ではない。

| 領域 | 拡張と更新の契約 |
| --- | --- |
| `nodes` | 公開後は backing array を動かさない。別 Store を追加する |
| `extra` | offset を保存し、毎回 owner の現 slice から読む。append による移動は可。公開した mutable slice / pointer を保持させない |
| `texts` | `strings.Builder` 等で末尾に追加し、Store の string view を更新する。既存 bytes の上書き・再利用はしない。返した substring は古い backing bytes を保持するため再確保で壊れない |
| foreign 表 | entry は正規 Node / List の値。表の要素アドレスを外へ渡さないので伸ばせる |
| list | handle の owner と offset は安定。要素置換・位置変更は setter を通す。長さの異なる list は Pointer の操作に従って新規作成・付け替えする |

`extra` の再確保は `Node.h` の再確保と同じ問題ではない。ただし現在の `List.Refs()` は extra の生 slice を返すため、そのまま mutable Factory に公開できない。checker 側の変換は `Len` / `At` / `SetAt` を使用し、setter の右辺のノード生成を先に評価してから保存先を引き直す。`nodebuilderimpl.go:3104–3115` の tuple list 更新が実例。walk は開始時の list handle と長さを固定し、各要素だけ owner の現 slice から読み直す。callback 内での既存要素置換は後続 visit に見え、別 list への親 slot 付け替えや新しい要素の追加は現在の walk に入らない。早期停止は Pointer と同じ。sealed Store の parse/bind 専用 raw view は local-only な型/API に残せるが、一般の checker の List API からは raw edge slice を持たせない。

明示的な list Clone の要素共有もこの setter の背後で扱う。**source 由来の alias は位置だけを変更でき、要素の `SetAt` は対象外として通常 build でも拒否する。** Pointer の Factory が許す全操作に対する互換性は約束しない。共有 source を不変にする以上、source への書き込みを発生させる操作は許可できない。今回確認した `nodecopy.go:884` の用途は位置の変更なので、この制約に収まる。private list 同士の Clone では要素の更新を共有し、Pointer と同じ結果にする。

source list から変更用の list を作る場合は、`NewNodeList` に要素 Node の列を渡す明示的な新規作成を使う。Node identity は保ち、element storage の共有は切る。これは Clone の自動 copy-on-write ではない。移植の到達範囲に source の alias を書き換える経路が見つかった場合、機械的にこの新規作成へ置換して等価と主張せず、source への変更が後で観測されるかを確認して、その経路を私有化するまで移植の等価ゲートを通さない。

Factory の New は子の `Parent` を設定しない。`SetParent` はそのノードだけを書き換える。Clone / Update は Pointer の `updateNode`・`cloneNode` と hooks を再現し、header の生コピーで実装しない。特に text offset、symbol/flow id、parent は保存先依存であり、そのままコピーできない。`src` を持たない合成 Store の文字列は `Text()` の値から texts に保存する。

| Clone / 変更あり Update の情報 | 扱い |
| --- | --- |
| kind と typed constructor の引数 | 引数として再構築する。子・list は共有、text は値を保存。`SyntheticExpression.Type` も元 constructor の引数として引き継ぐ |
| Flags / Loc | `updateNode` と同じく元 root からコピー |
| Parent / symbol / flow / locals | 元 root から自動コピーしない。constructor の初期値から開始し、元と同じ明示 setter / hook のみ適用 |
| Original / emit flags / comments | 生成・更新・複製 hook の順序と EmitContext の伝播規則に従う。AST header の一括コピーで転記しない |
| 無変更 Update | 元の正規 Node をそのまま返し、新規確保・更新 hook を起動しない |

アルゴリズムが明示的に要求する `DeepCloneNode` / `DeepCloneReparse` は残す。`nodebuilderimpl.go:3043` に実際の呼び出しがあり、その visitor / hook の意味を再現する。撤回するのは「保存先が変わっただけで浅い Clone を深い Clone に変える」規則である。

## 5. lazy JSDoc は一回の parse を公開単位にする

JSDoc は host 一件について複数の comment root を返す。private Builder で全結果を組み、Finish 後に root の親を source host へ接続してから公開・Seal する。Builder は完成前に nodes を移動できる。結果が公開された後はその Store に追記しない。従来の parser の id ベース構築を流用でき、parser を最初から増設可能な Factory に作り替える必要がない。

`ParseJSDoc` の入力は source file と正規 host。戻り値は Store と root ids の非公開な完成結果とし、region が StoreID を与え、foreign host を接続し、`[]Node` に解決して cache へ公開する。公開前にしか許さない処理を builder/result 型に閉じ込める。root は複数なので `Store.Root()` の「最後の行」に頼らず、明示的な root 列を返す。空結果では Store を保持せず、cache に空の完成結果だけ残す。

parser が必要とする host の kind・位置・flags は正規 Node から読む。途中の JSDoc が host へ親を辿る必要がある場合も、private Builder に同じ foreign host 参照を登録する。host の row を借りたり複製したりして意味を変えない。speculation の巻き戻しは未公開の row / payload / host 索引に適用する。使われなくなった private foreign entry は公開前に捨てても残しても意味は変わらない。

resolver の動作は次の順に固定する。

1. `HasJSDoc`、渡された file、TS/JS の eager/lazy 区分を Pointer と同じ意味で確認する。
2. source の eager 索引に結果があれば返す。空の結果を記録した entry も「存在」として扱う。JS では eager 結果がなくても lazy parse に進まない。
3. checker cache に host の正規 identity があれば、空を含めて返す。
4. private Builder で parse し、完成した結果だけを checker cache に登録して返す。

同じ checker の同じ host は同じ Node を返す。異なる checker が同じ TS host を parse した結果は異なる Node でよい。共有構造へ漏らさないことが条件になる。source の eager JSDoc は従来どおり共有する。`EagerJSDoc` 相当は source 索引と**すでに完成した checker cache**だけを読み、parse は起動しない。

checker ごとの resolver は `store.JSDocReader` のような明示的な引数として JSDoc を読む utility に渡す。例は `GetJSDocDeprecatedTag(reader, node)`。checker の member からは `c.docs`、nodebuilder からは所属 checker の reader を渡す。parser callback は reader の生成時に注入し、store package から parser を import しない。必要な utility と callback にだけ引数・closure を足し、純粋な AST accessor には文脈を付けない。既存変換器の P2 差し替えの対象として、この限定された境界を列挙・検証する。共有 File に checker-specific provider を置く方法や goroutine-local/global registry は使わない。

reader は checker の既存の直列実行契約の下でのみ使う。単独で並行呼び出しできる service としては公開しない。parse 中の host 集合を私有状態に持ち、同じ host の再入は内部契約違反として検出する。異なる host の lookup は許せるが、parser callback 自体は reader を再帰呼び出ししない。parse panic では完成 cache に書かず、defer で parse 中の印を外して元の panic を伝える。構文として誤った JSDoc は Pointer の parser と同じ回復・診断を行い、通常の完成結果として扱う。再入時に空の結果を返すなど、黙って意味を変える逃げ道は設けない。

reader は canonical な host と明示 file を受ける。file が nil なら host.File() を使い、cache key は `(file, host.Key())` とする。foreign edge から来た source host も解決後は同じ key になる。private/detached な host を元ノードへ勝手に置き換えず、Pointer 同様に HasJSDoc と file の有無を判定する。新たな private host の parse が必要なら、この key に対応する別 batch を同じ公開手順で作る。shared source の eager entry は正規 host が一致する場合だけ利用する。

host ごとの Store は小さな確保と管理情報を増やす。これは隠せるが無料にはならない。最初はこの単純な公開方式を選び、lazy Store 数、利用 bytes、links の空き、GC を測る。問題になった場合は、公開前の batch を region に配置する方式へ置き換えられる。reader と checker の API は変えず、事前 remap の実装費用とコピー量を測ってから導入する。

## 6. `File()`・意味情報・printer 境界

### File と StoreID

source Store に対する F1 の shortcut は保つ。source tree に属する Node の `File()` は既知の source file を返す。私有ノードは `Parent()` を辿り、source に達したら同じ shortcut を使い、nil に達したら nil を返す。lazy JSDoc もこの規則で host の file に着く。必要なら、全公開ノードの親連鎖が同じ file に着くと証明できる sealed JSDoc Store だけに shortcut を追加できるが、初期実装には必須でない。

合成ノードの所属 file を、作成時に処理中だった file や `Original` から推測しない。`Original` と `Parent` は別の関係。子に source を持っていても自身の Parent が nil なら `File()` は nil になる。Parent は後で変わりうるので、私有ノードの File を無効化規則なしにキャッシュしない。detached な parse scratch / dead node にも source shortcut を機械的に適用しない。

### Symbol・flow・locals・checker の Type

foreign Node の解決は source の正規 Node を返すため、その Node からの Symbol / Flow / Locals の読みは source の Bound に戻る。これは source child を複製しない直接の利点である。

ただし、合成ノード**自身へ**設定する意味情報は別途扱う。`getSyntheticElementAccess` の `result.FlowNode = source.FlowNode`、fakeScope の locals、`SyntheticExpression.Type` がある。foreign AST 辺だけでは解決しない。

- `SetSymbol` は解決済み `*Symbol` を受け、synthetic Store の参照表へ登録する。source の `SymbolId` を別 Bound の index として使わない。
- `SetFlow` は owner を含む `FlowHandle{bound, ref}` を受ける。flow の node / antecedent を読む API もこの owner を維持する。現 `Bound.Flow(ref) *FlowNode` の裸の row pointer だけでは、その row 中の `NodeRef` をどこで解決するか決まらない。A1 の解決層は owner を運ぶ view にする必要がある。
- `SetLocals` は checker-owned map を登録する。fakeScope の map は現実に書き換わるため、その同一性を維持する。
- `SyntheticExpression.Type` は checker-owned の `Node -> *Type` 表に置く。store が checker の Type を import する必要はない。生成器にはすでにこの any field を side table に置く TODO がある。`generate-go-store.ts:163`。

Bound 自身に対応する source Store を持たせ、解決層は以下の名前空間を保つ。private ノードへ source の FlowHandle を付けても、その flow 内の参照はすべて元 Bound に属する。source Bound へ private Node の行を追加しない。

| Bound 内の値 | 解決先 |
| --- | --- |
| `FlowRef` / `FlowListRef` / antecedent | 同じ Bound。返す Flow / FlowList view も Bound を運ぶ |
| 通常の `FlowNode.Node` | その Bound の source Store の NodeRef |
| SwitchClause / ReduceLabel の `FlowNode.Node` | kind/flags に従い同じ Bound の FlowData index。通常 NodeRef と混同しない |
| SwitchClause の FlowData | switchStatement は Bound の source Store、clause 範囲は scalar |
| ReduceLabel の FlowData | target と antecedents は同じ Bound の Flow / FlowList |
| `containers`、CommonJSModuleIndicator 等の NodeRef | Bound の source Store |
| Symbol の declarations | D2 による正規 Node。別 Store に属してよいが、共有 symbol には私有 Node を入れない |

この owner を運ぶ view の変換も、checker 移植の基盤に含める。header の `SetFlow` だけ直して終わりにはしない。

synthetic Store の header の整数は、その Store の意味情報表への slot とする。source Bound の安定した領域への `FlowHandle` と symbol/map への参照は scanned side table にまとめ、noscan header / extra に pointer を埋めない。scanned な私有領域が多少増えることを許容し、source のための表現へ強引に詰め込まない。表の要素アドレスを長期保存せず、再確保で参照が古くなる問題を繰り返さない。未設定 slot は nil の意味を持つ。特に binder を通らない lazy JSDoc は、空の共通 sentinel を読む等により、Bound がないことを理由に Symbol / Flow / Locals の accessor が壊れないようにする。source と private の解決差はこの層の測定対象になる。

ModifierFlags の高 bit は別の未完了事項として残る。現在の Pointer の `Node.ModifierFlags()` は modifier list を読むだけで、TS lazy parse 後に header を更新する仕組みではない。`ast/ast.go:613`。JS の JSDoc modifier は reparse 時に扱う。追加の derived flags が必要な経路は checker の memo に計算済み状態とともに置き、shared header への lazy 書き込みを導入しない。高 bit を黙って uint16 へ切り落とす実装は受け入れない。

### Store 出力から Pointer printer へ

NB1 と pseudochecker / modulespecifiers の変換対象は維持する。Store → Pointer adapter は、正規 Node / List ごとに memoize して共有を保つ。AST child だけを無条件に再帰コピーする実装にしない。EmitContext の Original、emit flags、合成 comments、printer に必要な source file/text/range も同じ対応表で移す。Parent / Original の逆向き辺を扱う際は、先に変換先 identity を登録して循環を避ける。

source の巨大な祖先・兄弟を不用意に丸ごと変換しないため、printer が読む関係を adapter の契約に列挙する。2026-09-26 補足: H1 の比較用 Pointer source の対応付けは等価検証の補助に限る。adapter はそれに依存せず、Store から必要な source / Original / text / range を変換できるものとする。メモリ・GC・印字性能の測定では比較用 Pointer AST を保持せず、実際の印字に必要な一時 Pointer AST の費用を含める。[レビュー追加契約 §3〜4](store-checker-review-contracts-20260926.md) に従う。message の等価だけでなく Original / comment / 再利用部分木を含む診断を受け入れ試験にする。

## 7. 他案と比べて何を払うか

| 案 | 意味・容量 | 追加の仕組みと判断 |
| --- | --- | --- |
| C4' + foreign refs、固定巨大 Store | 共有を保存できるが、容量超過は残る | 最短の試作にはなる。初期容量だけで入力上限を決める最終設計にはしない |
| C2 の深い複製 | 容量境界で identity / Parent / flow が変わる | 不採用。境界の頻度を減らしても正しさの問題は残る |
| 単一 Store の nodes を chunk 化 | 容量問題は解ける | Node{s,h} から Ref を逆算する現契約も変更が必要。id 保持や owner 特定まで含む再設計になる |
| 私有 AST だけ Pointer、共通 Node を tagged union にする | Pointer の Factory を流用できる | scalar を含む accessor の分岐か、checker/nodebuilder の二種類化が必要。NB1 の一律変換を崩すため今回は選ばない |
| 全 AST を広い global ref にする | 所有者は明示できる | source の slot 幅・解決経路・registry まで変える。今回の範囲より大きい |
| 提案の region + foreign refs | identity を保持して必要時に増設できる | Store/表/links の管理、一般の辺解決、list alias、context の受け渡しを内部に実装する。API と生成規則に費用を集約する |

提案は物理実装の行数が最小とは限らない。減らすのは checker に散る例外と、容量によって意味が変わる状態である。deep clone の規則、特別な JSDoc の親、root→host の別系統、固定容量の推定、file ごとの synth 分割、参照の付け替えを不要にする。一方、foreign table、list identity、意味情報の owner、JSDoc reader という本当に必要な関係は残す。

FR1 の「既存 bounds check を利用するから費用ゼロ」「ns に出なかったから Check cycles も増えない」は採用根拠にしない。新しい分岐、inline、register pressure、list 解決、GC は実コードで検証する。添付の microbenchmark の最小値比較は、この実装の性能比較ではない。

## 8. 実装順と受け入れ条件

1. Store の意味と id 型を分ける。NodeRef / NodeKey / NodeEdge / ListEdge、正規化、foreign 解決、通常 build の範囲検査を実装し、source の parse/bind 等価を保つ。schema の node/list slot の分類から views / foreach / builder / convert を一貫して生成し直す。source Builder は local-only、読み手は共通 resolver という区別も generator で固定する。手書き utility と全 raw decode 経路を監査し、生成物の再現を確認する。
2. Factory と region を実装する。source Builder の parent 自動設定と、synthetic Factory の明示設定を分ける。list の共有・更新、text、意味情報の owner を含めて検証する。
3. 7d-1 の JSDoc 移植に private result の公開 API と明示 reader を入れる。JS reparse と eager 分は source 内で閉じ、lazy 分だけ採番して完成 Store の Node を cache へ公開する。
4. checker / nodebuilder を変換する。大きな checker 移植前でも、以下の storage 契約は小さな操作列で検証できる。ただし、実 workload の名前解決・診断の等価と性能は checker が動くまで確定しない。
5. H1 の同一 driver で Pointer と Store の生診断を比較し、printer 境界、代表的な Check、最後に end-to-end を評価する。

受け入れ条件は、通常容量だけでなく**容量 1、2、3 等で境界を強制しても、正規化した構造・参照の共有関係・診断が変わらないこと**。物理 id の数値が同じである必要はない。以下を含める。

| 操作・入力 | 確認する振る舞い |
| --- | --- |
| Node を保持し、別 Store を何度も追加し、古い Node を更新 | 保持した Node と accessor の返す Node が同じ更新を読む。Ref が変わらない |
| old parent が new child を指す / new parent が source child を指す | 両方向の参照が正規 Node に戻る。source child の Parent は変わらない |
| Clone / Update / private list 同士の明示的 Clone / 空 list | root・list・element storage の共有が Pointer と一致する。別 Store の nil 同士を含め、無変更 Update は元の Node |
| source list の明示的 Clone | 位置だけの更新は source に影響しない。alias の要素更新は明示的に拒否し、7d の実経路が要求しないことを確認 |
| extra と texts の再確保、tuple list の右辺で連続生成 | setter が現在の保存先に書く。古い Text は同じ値、保持した List は最新の要素を読む |
| walk callback での要素更新・親 slot 付け替え・別 list 生成 | 開始時の handle / 長さを保ち、既存要素への置換だけを後続 visit で観測する |
| synthetic element access、this.x、fakeScope、JSX、decorator signature | source の名前解決、flow、locals、宣言、file が Pointer と一致する |
| 同じ host の繰り返し lookup / 空結果 / 二 checker | checker 内では同一結果・一回の parse。checker 間は cache を共有せず source は不変 |
| JS reparse、TS eager、TS lazy、複数 comments、誤った JSDoc | parse/bind の現除外を外して等価。lazy tag → root → host → File が通常 API で成立する |
| nil と無印の範囲外 edge、foreign の範囲外 index | nil は安定して nil、不正 edge は通常 build でも失敗する |
| nodebuilder 再入・大量の診断・複数 file | 任意の固定上限や早期 reset に依存せず、診断の位置・code・message が等価 |
| checker 並列実行 | race 検査で共有 source/cache への私有書き込みがない |

性能の判定は別に行う。source の Parent / child / list の inline と命令列、JSDoc が少ない・多い入力、診断が多い入力、representative Check、retained heap と GC を測る。`ns/op`、`B/op`、`allocs/op` と、取得できる instructions/cycles を分けて報告する。region の segment 数・使用率・foreign entry 数・lazy Store 数・list alias 数を計数し、追加コストの帰属を残す。比較は raw 出力を保存して `benchstat old.txt new.txt` を使う。単なる非有意差を「遅くない」と判定しない。実装と保守の費用に見合うかを判断し、一律の改善率は要求しない。

## 9. 既存文書への反映範囲と調査記録

| 旧節 | この案を実装する場合の置換内容 |
| --- | --- |
| §2.2 JSDoc の置き場・X2' | eager/reparse の方針は維持。lazy は host 単位の完成 Store、Parent は host、cache は明示 reader |
| §2.3 F1 | source shortcut と synthetic の親探索を区別。StoreID と file を分離 |
| §2.4 L1 | file index を StoreID と読み替え、私有 Store 登録を region に集約。page は引き続き二段 |
| §2.5 A1 | 解決 API は所在を保持。特に Flow は裸の local ref/row だけで返さない |
| §2.6 NB1 | Store 出力は維持。「またぐ Clone は deep」を撤回し、浅い共有と hooks を再現 |
| §2.6 C4' | 一つの固定巨大 Store を、増設可能な所有領域へ変更。header のアドレス安定は維持 |
| 元 AST 設計 §2.8.3 / §2.9 | source の辺は local のまま。共通 resolver に foreign 経路を認める。共有 source の seal と private の更新を区別 |

調査対象は `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、HEAD `5a1ff82d3d6af2529bf94bdc199fb017f3d1a3f5` の作業ツリー。別 clone への切り替えは行っていない。元の checker 設計は調査開始時点で untracked だったため、その決定履歴を削除せず、本提案を別文書にした。

実装の根拠は `store/store.go`、`store/bound.go`、`store/file.go`、Pointer の `ast.go` / `ast_generated.go` / `utilities.go`、`parser/jsdoc.go`、checker の `checker.go` / `nodebuilderimpl.go` / `nodebuilderscopes.go` / `jsx.go` / `printer.go`、Store 生成器。提示された会話は問題の経緯として読み、性能の再現証拠とは分けた。

今回の作業は設計調査で、ベンチマークは再実行していない。本案の性能 artifact は `missing` であり、`ns/op`・`B/op`・`allocs/op` は未測定、before/after の比較もまだできない。既存 `_fieldcount` と設計内の過去の計数は `stale` として扱う。`repo_root`・`tsgolint_git_rev`・`typescript_go_git_rev` の一致を確認できず、現在の checkout の性能証拠には使えないためであり、過去の内容が誤りだと断定するものではない。次の作業は §8 の storage 契約の実装と検証であり、性能改善を達成したとは主張しない。
