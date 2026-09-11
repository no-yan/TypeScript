# Binderの4経路：pointer版との差分、新規設計、改善量の評価

作成日: 2026-09-11。対象は最新のStore Binderとpointer対照。**設計文書であり、以下の新API・表現は未実装。新規benchmarkは実行していない。**

## 0. 結論と重要な補足

優先する設計は次の4つである。

| 項目 | Store化で増えた処理 | 提案する設計 |
|---|---|---|
| 1. Locals / NextContainer | node indexをキーにした外側mapの検索・登録 | 整数container indexと、意味情報を持つ専用配列 |
| 2. Flags / header | 有効性の再確認、header位置の再解決 | bind区間の構文安定契約と、位置を保持する短命なaccessor |
| 3. list / modifiers | list位置・要素の解決、modifier flagsの再集計 | 取得済みListRefの受渡し、slice借用、pointer版と同じModifierFlags保持 |
| 4. Flow書込み | Flow pointerからIDへの変換・所有確認、汎用列拡張処理 | Storeに結び付くFlow値と、準備済み列への専用書込み |

**新たに確認した差分:** pointer版は`ModifierList`の作成時にModifierFlagsを集計して保持する。Store版のBinderは呼び出しごとにmodifier listを走査して集計する。以前の「list再解決が残る」という説明だけでは、この差分を十分に説明していなかった。3-Cはpointerにも導入可能な新しいalgorithm最適化ではなく、**pointer版が既に持つ表現・計算の分担をStoreに戻す案**である。

この4項目だけでpointer同等を達成できるとはまだ言えない。CPU Profilerは費用の所在を示すが、無駄な処理の全額や変更後の時間は測定していない。各案の独立対照で削減できた量を積み上げる。

## 1. 対象、測定基準、証拠の状態

### 1.1 選択repoとソース

- Store: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、HEAD `32598cba146fa4dd7b6162b838630c90d865ab28`。
- pointer: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、HEAD `8ac035a394c79e693a3a7d74cb170448503ee894`。
- tsgolint_git_revは両版null。本書作成時にもprofileのsource SHAと両checkoutの全Goソースの一致を確認した。HEADだけでdirtyの実装を特定していない。
- 候補clone: flownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr系、store-redesign等。今回未使用。完全な一覧はprofile artifactの`worktrees.txt`。

### 1.2 時間・割当の基準

[修正済み寿命ハーネスの再測定](bind-batch-recheck-20260911.md)の通常GC主比較。各サンプルで10個の独立した未bind ASTを準備し、bindだけを計時、終了後にStore登録とAST参照を解除する。6固定round、同一binaryのA/Aを含む。

| 入力 | pointer ns/op | Store ns/op | pointer B/op | Store B/op | pointer allocs/op | Store allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| checker | 11,761,141.5 | 15,453,223 | 7,424,013 | 12,798,288 | 13,950 | 14,163 |
| dom | 4,377,387.5 | 5,903,114.5 | 5,285,994 | 7,866,128 | 16,558 | 16,682 |

benchstatは時間+31.39% / +34.85%、各p=.002。ただしA/Aの±1.5%精度gateは全体として未達。倍率の精度は留保する。GC無効でも+33.72% / +29.31%が残る。

Storeの現在時間を基準にpointer同等まで必要な短縮率は、`1 - pointer / Store`で**checker約23.9%、dom約25.8%**。時間増加率31〜35%と、必要短縮率24〜26%を混同しない。

### 1.3 CPU Profiler

[CPU分析](binder-instruments-analysis-20260911.md)では同じbinaryをInstruments **CPU Profiler**で各20秒attach採取した。Go 1.26.0、darwin/arm64、Apple M1、GOMAXPROCS=8、GOGC=100、GOMEMLIMIT=off。bindを含むRunning stackを抽出し、cycle-weightで集計。bindサンプルはpointer checker 17,263、Store checker 21,970、pointer dom 14,784、Store dom 19,424。

以下の百分率は原則として**各Store traceのbind CPU内の割合**。exclusiveはleafだけ、inclusiveはcallee込み。親子は足さない。inlineされた関数はPCをobjdumpのソース行へ対応付けた。PCの行帰属は、個々のloadの待ち時間やbounds checkだけの時間を測るものではない。

| artifact set | 状態 | 用途 |
|---|---|---|
| `20260911-bind-batch-recheck-02` | `current`、ソース一致 | ns/op、B/op、allocs/op、benchstat |
| `20260911-binder-instruments` | `current`、ソース一致 | CPU帰属、caller、PC、通常ビルドobjdump |
| 本書の新設計の性能・意味試験 | `missing` | 未実装・未測定 |
| profileの反復・A/A、現行KPC | `missing` | 構成比の再現性・命令数の評価は未実施 |

artifactはrepo内`.cursor/skills/verify-tsc/artifacts/`以下。`current`はrepo_root / tsgolint_git_rev / typescript_go_git_revが一致するもの。dirty source一致は追加条件として別途確認した。異なるidentityの旧結果は`stale`。対象regexにBenchmark行のないstageは`unsupported`とするが、今回該当する新規stageはない。

## 2. 共通設計原則

1. Binderの2pass、functions-first順序、宣言・診断・Flow生成・副作用の順序を変えない。
2. AST・listの整数配列にpointerを埋め込まない。SymbolTableやFlowNodeの必要なpointer領域は分離する。Store全体がnoscanになるという意味ではない。
3. 汎用APIの検査を一律に消さない。前提を満たすbind経路と、foreign/synthetic/transformの汎用経路を分ける。
4. 「既知の位置」と「読み取った値」を区別する。Flags等のmutableな値は必要な時点で再読する。
5. 構文配列のappend/reallocation、Compact、Restore、構造更新を禁止できる区間を監査する。現行のbuild phaseは構文変更も許すため、今のphaseだけで借用の安全性を保証しない。
6. 命名は用途を表す。`ParameterAccessor`等の生成APIを使い、`SyntaxChildren5`や巨大な全node snapshotへ戻さない。
7. スピル、escape、frame増大、追加列のメモリ費用も結果として数える。短命slice/pointerの使用自体は、参照先の整数配列のnoscan性を失わせない。

以下のコードは設計を説明する擬似コードであり、公開APIや正確な型配置の確定ではない。

## 3. ボトルネック1：Locals / NextContainer

### 3.1 pointer版の仕組みと意義

スコープを持つノードは、その中に`LocalsContainerBase`を持つ。

```go
// pointer版 ast_generated.go:139
type LocalsContainerBase struct {
    Locals        SymbolTable
    NextContainer *Node
}
```

`Locals`はスコープ内の宣言の表。例えば関数の引数・ローカル変数を登録する。`NextContainer`はBinderが作るcontainer chainの次のノードであり、親スコープを指すものではない。

関数、SourceFile等では具体的なpayloadの構造が異なる。`Node.LocalsContainerData()`は`n.data`のinterfaceメソッドを呼び、具体的なpayloadに埋め込まれた共通部分を返す。非対応nodeはnilを返す。Binderは種類別switchを繰り返さず、同じ入口からスコープ情報を扱える。

```go
// pointer版 utilities.go:61
return GetSymbolTable(&container.LocalsContainerData().Locals)

// pointer版 binder.go:2527
b.lastContainer.LocalsContainerData().NextContainer = next
```

GetSymbolTableは未作成時だけ名前→Symbolのmapを作り、Localsフィールドへ保存する。**interface dispatchとpayloadへの間接アクセスは存在する**。「費用ゼロのnode直下フィールド」とは言わない。しかし「nodeからSymbolTableの所在を探すmap」は存在しない。

### 3.2 Store版との差分

```go
// Store版 store.go:1043以降
locals := s.locals[ref]       // map[NodeRef]SymbolTable
s.locals[ref] = locals
s.nextContainer[ref] = next  // map[NodeRef]NodeRef
```

名前をキーとするSymbolTableは元々必要。それに加え、Storeでは外側にNodeRefをキーとするmapがある。`b.getLocals(ref)`の呼出しごとに外側mapを検索し、初回は外側mapへの登録も必要。container chainの更新にもmap挿入が必要になる。

| callsiteで確認できたmap leaf | checker | dom |
|---|---:|---:|
| Locals読取（store.go:1062） | 1.69% | 2.17% |
| Locals登録（store.go:1055） | 0.34% | 0.64% |
| NextContainer登録（store.go:1077） | 0.94% | 1.33% |
| 合計、重複なし | **2.97%** | **4.14%** |

getLocals inclusiveはStore 4.15%/7.16%、pointerのGetLocalsは1.74%/2.92%。SymbolTable生成なども含むため、このinclusive差を削除可能量にしてはいけない。

### 3.3 提案：container indexによる意味情報アクセス

最初の対照は既存APIの意味を変えず、内部の2つのmapを以下へ置き換える。

```go
containerIndex []uint32    // NodeRef -> ContainerIndex。0は未作成
containerNext  []NodeRef   // ContainerIndex -> 次のcontainer。整数のみ
containerLocals []SymbolTable // ContainerIndex -> 表。pointer領域として分離
```

containerIndexが0でも、Localsの読取だけならnilを返し、割り当てない。LocalsまたはNextContainerへの最初の非zero書込みでrecordを確保する。SymbolTable自体のmakeは現在と同じく初回getLocals時。`SetLocals(nil)`はLocalsだけを消し、NextContainerを消さない。その逆も同様。record番号を保持しても外部から見えるゼロ値の意味を変えない。

NodeRefからは`containerIndex[ref]`、次に`containerLocals[index]`を読む。mapは消えるが、依存loadは2段残る。単にmapを配列にするだけでpointerのpayloadアクセスを必ず上回るとは限らない。

初回対照ではBinder側の保持方法を変えない。効果が確認できた場合だけ、現在のcontainerを設定する箇所でContainerIndexを取得し、同じcontainerの宣言処理へ渡す案を別対照にする。これは無制限なnode cacheではなく、現在のscopeの位置の保持。refとindexの取り違えを避ける用途別型とaccessorを使う。

### 3.4 メモリ費用と境界

Node数N、作成したcontainer record数C、64-bitでSymbolTableが1 pointer wordなら、長さベースの概算は`4N + 4C + 8C` bytes。容量余剰、sentinel、slice header、alignment、実際の型サイズは別途測定する。置き換える2つのmapの費用を差し引いた**純増減**が重要。

Nが大きくCが少ないとdenseな4N列が不利。現行nodeHeaderへindexを追加して全nodeのstrideを増やす案は初手にしない。必要ならchunk化したindex列を比較するが、追加の依存loadが入る。まず単純な列で費用を測り、都合のよいlayoutを推測で確定しない。

Compact/Restore、clone、再parse、synthetic nodeの追加、Storeの破棄・GC保持を監査する。全reader/writerを同じ表現へ移し、旧mapと新列の二重の正本を作らない。

### 3.5 改善量と最小実験

確実に狙うのはmap関数の呼出し除去。観測済みの直接scopeは2.97%/4.14%で、その全てが純減になるわけではない。配列load、index取得、record確保が置換費用になる。map内部の未帰属calleeやcache挙動への副次効果も未測定。

同じ`getLocals`の初回／既存取得、NextContainer更新を分けた小さい対照でアクセスを確認し、その後checker/domの実bindで評価する。生成する宣言数、SymbolTable内容、container chainの順序・終端が一致することを先に確認する。symbol syntheticだけで実workloadの改善を主張しない。

## 4. ボトルネック2：Flags / header

### 4.1 pointer版の仕組み

pointer版の`bind(node)`は、そのnodeのKindでdispatchし、処理後の同じ位置で`node.Flags`を読む。

```go
thisNodeOrAnySubnodesHasError := node.Flags & ThisNodeHasError != 0
// 子をbindし、エラー状態を伝播する
if thisNodeOrAnySubnodesHasError {
    node.Flags |= ThisNodeOrAnySubNodesHasError
}
```

Flagsの読取・書込みは意味処理に必要。pointer版でもKind分岐、再帰呼出し、error伝播は行う。

### 4.2 Store版との差分

`bindKind(id, kind, parentKind)`はkindを受け取っているが、後段で`b.store.FlagsAt(id)`を呼ぶ。FlagsAtはnil/zeroを確認し、nodesのindexからheaderを解決する。inlineされた通常コードでもこの処理は残る。

bindKind内のFlagsAt相当行（store.go:544/547）へのPC帰属はchecker **1.72%**、dom **1.49%**。これはnil/ref検査、配列アクセス等の行帰属で、bounds check単独の割合ではない。

bindKindの大きいkind switch自体への帰属もあるが、pointer版にも同等のdispatchがある。その割合を丸ごとStoreの無駄として計上しない。

### 4.3 提案：位置の保持と、値の最新読取

有効なrefを確認した入口でheader位置を一度取得し、短命な`BindNodeAccessor`等で保持する。Flagsを読むメソッドは、その位置から**呼出し時点のFlags**を読む。生成ParameterAccessor等は構文slotを扱い、共通header accessorと責務を分ける。

```go
node := syntax.AccessNode(ref) // 初回の位置解決・検査
// 同じnodeを使う意味処理。Flagsを更新してよい
flags := node.Flags()         // 値snapshotではなく、同じ時点の最新値
```

型名・APIは案。第一段階はbindKindの必要な区間だけで使い、全recursive frameへ大きいcontextを配布しない。

安全な短命pointerを使うには、借用区間にnodes配列のreallocationが起きない契約が必要。`storePhaseBuild`だけでは不十分。bindが呼ぶ全推移的calleeとdeferred処理を監査し、構文writerを入口で検出する検証モードを用意する。構文appendが必要な経路は借用区間を終え、再取得する。

位置を整数offsetとして保持する案はreallocationへの耐性がある一方、配列baseと範囲チェックの再解決が残り得る。pointer借用・整数保持・現状の直接getterを同じcallerで比較し、pointer案を先に採用したことにしない。

### 4.4 削除しないもの

- pointer版と同じ時点のFlags読取、子bind後のerror伝播。
- Flagsの更新を見逃さないための最新値読取。
- 不明な入力に対する汎用APIの境界検査。
- 初回の正しいheader位置取得。

nil/zero判定を減らすのは「この入口を通った有効ref」という契約に基づく。`-B`で全体の検査を消すことを本番設計の代替にしない。

### 4.5 改善量と最小実験

今回の限定scopeは1.72%/1.49%。この全額を消せず、headerの実際のloadは残る。他のcallerへ広げた場合の費用はまだ集計していないので、この値に任意の係数を掛けて見積もらない。

保持による追加引数、frame、spill、escapeが節約量を上回る可能性がある。最初はbindKind一箇所の対照で、inline後のnil判定、bounds check、header再解決とframeを確認する。Flagsをcalleeが更新する意味試験を含める。単なるgetter単体microだけで採用しない。

## 5. ボトルネック3：list / modifiers

### 5.1 pointer版：子の走査

pointer版はNodeListの`Nodes []*Node`を通常のsliceとして走査する。

```go
for _, node := range nodes {
    b.bind(node)
}
```

functions-firstは同じsliceを2回走査する。1回目にFunctionDeclaration、2回目にそれ以外をbindする。Kind読取と要素取得は必要だが、Storeのowner、ListRef、listHeaderの解決は不要。

### 5.2 pointer版：modifier flagsは保持済み

pointer版ast.go:154以降は、ModifierListに集計値を保持する。

```go
type ModifierList struct {
    NodeList
    ModifierFlags ModifierFlags
}

// NewModifierList
list.Nodes = nodes
list.ModifierFlags = ModifiersToFlags(nodes)

// Node.ModifierFlags
modifiers := n.Modifiers()
if modifiers != nil {
    return modifiers.ModifierFlags
}
return ModifierFlagsNone
```

`Modifiers()`には具体的payloadへのinterface呼び出しがあり得る。しかし、その後のFlags読取はlist長に依存しない。cloneも集計値をコピーする。modifier nodeのbindやdecoratorの処理は別であり、集計値を持つからといって省略しない。

### 5.3 Store版との差分

現在のlocal bindListRefは既にTryBindListSpanでowner/headerを一度解決し、ループではBindListSpanElemとKindAtを呼ぶ。毎要素でlistOwnerを解決する旧経路を、現在のlocal経路の説明に使わない。foreignではListLen/ListElem fallbackが残る。

ただし、Spanにはstart/lengthだけがあり、各要素でchildren配列へのアクセスを再構成する。さらに`modifierFlagsRef`はmodifiersRefGeneratedからListRefを取得し、**呼出しごとに全modifierのKindを集計する**。Handle.ModifierFlagsとparser.modifiersToFlagsにも走査する実装がある。

| 観測scope | checker | dom | 注意 |
|---|---:|---:|---|
| bindListRef | 2.31% | 3.12% | exclusive。子bindの費用を含めない |
| そのうちStore/Spanのソース行 | 1.03% | 1.25% | 上行の内数 |
| modifierFlagsRef | 1.80% | 4.71% | inclusive、list取得も含む |
| ListSlotAt | 0.87% | 1.79% | exclusive。modifier経路との重複あり |

### 5.4 案3-A：取得済みModifiersの受渡し

ParameterAccessor等の`Modifiers ListRef`を、既にその値を取得したcallerから後続helperへ渡す。helperがrefとkindへ戻って同じlist slotを取得する経路を除く。別nodeのmodifierが必要なrootDeclaration等の処理では、誤って元nodeのlistを流用しない。

全ての生成Accessorに未使用フィールドを追加しない。Binaryのようにmodifierを使わない経路は専用の狭いAccessorを維持する。

### 5.5 案3-B：local listのslice借用

syntax read契約の中で、localなListRefから`[]NodeRef`を一回借用する。

```go
refs, local := syntax.BorrowLocalList(list)
if local {
    for _, ref := range refs {
        if ref != 0 {
            b.bindRef(ref, parentKind)
        }
    }
} else {
    // 現行のforeign semanticsを維持する経路
}
```

sliceの初回切出しには検査が必要。range内の明示的な`i < span.length`検査と毎要素のstart加算は省ける可能性があるが、compilerの結果を確認する。要素のNodeRefとKind取得は残る。

children配列の再配置、list範囲変更、Compact/Restoreを借用中に許さない。要素更新を許すかは別の契約とし、初回案では構文を固定する。両passは同じ順で維持。局所実験でsliceが遅かった履歴もあるため、同じ呼出し区間で整数Spanと対照し、先験的に高速とは扱わない。

### 5.6 案3-C：ModifierFlagsの保持を復元する

pointer版と同じく、modifier listの構築完了時に一度集計し、listに対応する整数metadataとして保持する。まず案3-A/Bとは独立に評価する。

候補の最小表現は、Store内のlist indexに対応する`[]ModifierFlags`とvalid bitset。Flags=0の有効なlist（例: decoratorだけ）と未構築を区別する。全list数Lに対して概算`4L + ceil(L/8)` bytesとなる前提はModifierFlagsが4 bytesの場合で、実装時に型サイズを確認する。

listHeaderへ直接4-byte fieldを追加する案もあるが、現在16-byteの全listHeaderを20-byteへ増やすため、modifier以外のlistにも25%のheader増を課す。別列案との性能・容量を比較して選ぶ。疎map案はメモリを抑え得るが、今回減らしたいmap検索を再導入するので対照候補に留める。

専用のmodifier-list構築入口でvalid metadataを確定する。parserだけを修正して終えず、Factory、変換、clone、SetModifiers、SetListAt、Compact、Restore、foreign list共有を監査する。foreignはowner側のmetadataを読む。構文変更後は集計値を再構築またはinvalid化し、Freeze後のreaderがlazyにmap/列へ書き込む設計にはしない。

ModifierFlagsの問合せは保持値を読むが、modifier/decorator nodeの走査・bind自体は維持する。pointer版に存在する構築時の集計を復元するため、比較の公平性を損なうStore限定algorithm最適化には当たらない。ただしparse側の追加費用をbind改善から隠さず、parse+bindで必ず評価する。

### 5.7 案3-D：空listのforeign map呼出し

現在のlistSlotはlocal indexが0なら`s.foreignLists[index]`を読む。foreignListsがnilと分かるときは直接0を返す分岐を対照できる。foreign mapが存在する場合は、slotの0をmissingと決めつけない。

これは小さい局所対照で、3-Cの代わりではない。追加分岐が逆効果になる可能性もあり、通常ビルドの機械語でCALLと分岐を確認する。

### 5.8 改善量

3-Aと3-Cの予算を別々に足さない。両者はmodifierFlagsRef inclusive 1.80%/4.71%の一部を共有する。集計値保持はループを取り除けるが、list位置・owner・Flags列の取得は残る。短い／空のmodifier listでは利益が小さい可能性がある。非empty list数、問合せ回数、合計走査要素数を次の監査で取得する。

3-Bの対象をbindListRef内のPC行に限定すると1.03%/1.25%。functions-firstや他のhelperへの横展開は別のscopeとして集計する。ListSlotAt割合、modifier inclusive、list loop割合を無条件に合算しない。

## 6. ボトルネック4：Flow更新

### 6.1 pointer版の仕組み

FlowNodeはarenaから確保し、nodeのFlowNodeフィールドへ直接pointerを代入する。

```go
// newFlowNode
result := b.flowNodeArena.New()
result.Flags = flags

// Identifierをbind
node.AsIdentifier().FlowNode = b.currentFlow
```

Flowの共有・同一性はpointerで表現する。nodeの保存先によってpayload取得があるが、Store内のIDへの変換は不要。

### 6.2 Store版との差分

StoreはnodeごとのFlowを整数列に保持し、FlowNode本体を安定したchunk arenaで保持する。

```go
s.mustMutate()
putCol(&s.flows, ref, s.flowID(flow))
```

flowIDはnil、直前Flow cacheを確認し、必要ならFlowのid、arena範囲、chunk内のアドレスを確認する。localでないFlowはforeignFlowsへ保持する。putColは準備されていないStoreも扱うため、必要なら列を伸ばす。

| 観測scope | checker | dom |
|---|---:|---:|
| SetFlow inclusive | 3.31% | 1.53% |
| SetFlow exclusive | 1.90% | 1.03% |
| flowID全caller exclusive | 1.70% | 0.68% |

SetFlow inclusiveにそのcalleeのflowIDを足してはいけない。またflowID全callerにはSetFlow以外も含まれる。

### 6.3 提案：所有が確定したFlow値を渡す

ast側で構築する用途別のFlow値を導入し、pointerと保存用IDを一体で扱う。初回案はBinderのcurrentFlowとその保存・復元、nodeへのSetFlow経路に限定し、Flow graph全体を一度に書き換えない。

```go
// 概念。外部からidを書き換えられない型とする
BindFlowValue { pointer, encodedID }
```

local Flow作成時にIDが既知ならそこで値を作る。pointerのまま返る既存helperから取り込むときは、所有を確認して値へ変換する。比較・Antecedent設定でpointerが必要な箇所ではpointerを取り出す。currentFlowの変更・保存・復元は値単位で行い、pointerとIDを別々の代入で同期しない。

Storeに結び付くBindWriteSessionが値を作り、同じsessionにだけ書き込む契約にする。Goの型だけでStoreごとの所属を静的に完全保証できるとは言わない。外部入力での所属検査とdebug検証、作成元の監査が必要。foreign/sentinel/nilを取り込む経路を明示し、`flow.id`の生読みによる所有確認の回避はしない。

foreign IDの再利用が既存のforeignFlows追記やFlowCount等の観測に与える影響も確認する。初回対照では不明なforeign経路を現行setterへ戻し、local経路の効果だけを評価する。

### 6.4 提案：準備済み列への書込み

Binder開始時には既にPrepareBindTablesでflows列を確保する。構文NodeRefの範囲がbind区間で増えないことを確認できれば、session入口のphase/範囲確認を根拠に、nodeごとの汎用拡張処理を省く。

```go
// 概念的な到達形
flows[ref] = flow.encodedID
```

初回のbind専用APIはGoの通常配列アクセスを使う。compilerのbounds checkは別途確認する。手動のputCol拡張分岐が消えることと、全bounds checkが消えることは同じではない。

generic SetFlowはchecker synthetic、foreign、その他のphase用に残す。構文が増えるcalleeがある場合は、専用区間を区切るか、再準備してから続ける。mustMutateを全Storeから一律に削除しない。

### 6.5 改善量・保持費

限定scopeはSetFlow inclusive 3.31%/1.53%。実際の整数書込み、必要なbounds check、Flow値の取込みは残る。既存のlastFlow cacheが効く経路では、ID保持の追加費用が利益を上回る可能性がある。

64-bitではpointer+uint32の値がpadding込み16 bytesとなり、元のpointer8 bytesより保存・引数・frameを増やす可能性がある。実際のsize、escape、spillを測る。currentFlowの切替回数とSetFlow回数、cache hit率、foreign割合を測り、変換位置を移すだけで総変換回数が減らない案は採用しない。

FlowNode本体の大型化、chunk allocation、NewFlowの費用はこの案で消えない。SetFlowだけの改善をFlow全体の改善と言わない。

## 7. 改善量の読み方と感度計算

### 7.1 数値として言える範囲

現時点で新設計のns/op、B/op、allocs/op、Δinstructionsは全て`missing`。下表は**改善予測ではなく、選んだscopeのCPU費用が仮に半分になった場合の感度計算**である。CPU cycle-weight比率をwall短縮率へ変換していない。

| 独立に見るscope | 観測CPU比 checker / dom | scopeを50%純減できた場合のbind CPU減のモデル |
|---|---:|---:|
| 1. Locals/NextContainerのmap leaf | 2.97% / 4.14% | 1.49% / 2.07% |
| 2. bindKind内FlagsAt行 | 1.72% / 1.49% | 0.86% / 0.75% |
| 3. bindListRef内Store/Span行 | 1.03% / 1.25% | 0.52% / 0.63% |
| 3. modifierFlagsRef inclusive | 1.80% / 4.71% | 0.90% / 2.36% |
| 4. SetFlow inclusive | 3.31% / 1.53% | 1.66% / 0.77% |

式は`ΔCPU ≈ p × r − a`。pはscope比率、rは除去割合、aは追加された処理のCPU比率。上表は純減50%という仮定でaを織り込んだ場合の算術例にすぎない。実際のr/aは介入実験で測る。profile窓ごとの処理量が等しくないため、raw cycle-weightの差から求めない。

表の合計を実装全体の予測にしない。相互作用・scopeの重複を除いたsample unionの集計と、組合せの独立測定が必要。範囲外のcache/GC/codegen変化もあるので、観測scopeを厳密なwall上限にも使わない。

### 7.2 pointer同等への残り

必要なwall短縮は約24〜26%。現在確認した局所費用を一つ消すだけでは届かない。Storeソース行に対応するbind leafはchecker21.64%、dom16.07%だったが、必要な読取・更新も含むのでこれを全額節約予算にはしない。

1〜4で得た実測改善を組合せた後、最新profileで残存経路を確認する。目標未達なら広いwalk/helpersと意味情報表現を再評価する。「各項目を実装したのでpointer同等」とは完了判定しない。

## 8. 実装・検証の順序

### 8.1 最初に行う監査

- container数Cとnode数N、Localsの初回/再取得回数、NextContainerの更新数。
- modifier list数、非empty list数、Flags問合せ回数、総走査要素数。pointerの構築時集計との対応。
- SetFlow回数、currentFlow切替回数、flowID cache hit/miss、foreign割合。
- bindから到達する構文writer、NodeRef範囲の増加、配列再配置。生成walkerだけでなくdeferred/helper/Factoryの推移的呼出しも確認。

計数instrumentationは専用audit buildに限定し、通常wall binaryに混ぜない。

### 8.2 独立対照

| 順 | 変更 | 変更しないもの | 主な確認 |
|---|---|---|---|
| A | Locals/NextContainerのmapを専用列へ | Binder宣言algorithm、container保持方法 | map CALL消失、純B/op、chain一致 |
| B | ModifierFlags保持の復元（3-C） | list走査API、2pass、modifier nodeのbind | getterの再集計ループ消失、parser/clone更新整合 |
| C | 取得済みModifiers受渡し（3-A） | 集計方式は対照間で固定 | ListSlotAt再取得の消失、未使用fieldなし |
| D | 一つのlocal list区間のslice借用（3-B） | foreign fallback、訪問順 | 境界、再配置、escape、Span対照 |
| E | local Flow値保持、次に専用書込み | Flow graph構築algorithm | 所有、ID同一性、変換回数、frame |
| F | bindKindのheader位置保持 | Flagsの読取時点、意味処理 | mutable Flags可視性、frame、再解決 |

既存計画のwalker codegen対照は完了済み。本書はそこを再開せず、その後の表現・契約の対照を具体化する。依存する構文安定契約が未監査ならD/E/Fの該当部分を先に採用しない。

### 8.3 共通受け入れ条件

1. 生の構文・Symbol・Flow・診断の意味digest、訪問順、各種境界試験が一致する。既存pointer対照の既知差分があれば一覧化し、新規差分と分ける。
2. nil、empty、foreign、synthetic、構文変更、Freeze後利用、Compact/Restore、cloneの対象条件を確認する。
3. 通常ビルドobjdumpで意図したCALL/loop/検査の消失を確認。最適化を無効にした監査buildのframe値を通常buildの値として扱わない。
4. 両版の最新source SHAとbinaryを固定し、修正済み寿命ハーネスで通常GC・GC無効を分ける。10 binds × 6固定round、A/Aを含み、rawを保存する。benchstat old.txt new.txtで比較。精度gate未達時に有意になるまでroundを足さない。
5. parse+bind、B/op、allocs/op、GC CPU/scan/liveを別途確認する。parserへ移した費用や列の増加を隠さない。
6. 有望な個別案だけを組合せ、同じpointer対照と独立に再比較する。差分CPU profileは同じ処理量または構成比の制限を明示する。

低コストモデルへの実装委譲時も、まずAまたはBの一項目だけを渡す。複数の表現変更を一度に実装して原因を追えなくしない。本書では実装・agent起動は行っていない。

## 9. 割当driverと本案の限界

既知の割当driverはsymbolIdx/flows列、symbolRefs、FlowNode/Symbol/Handleの大型化。1は外側mapを除く代わりに列を増やし、3-Cは集計metadataを増やす。2/3-B/4もescapeすると割当を増やし得る。今回のCPU profileはAllocations instrumentではないため、割当bytesの全額帰属は未完了。

noscanな配列が増えても、その確保・ゼロクリア・copy・メモリ帯域の費用は残る。短いbindだけでなく、実プロジェクトでStore版が速かったparse+bindの性質を保てるかを確認する。

## 10. 実装根拠へのリンク

| 項目 | pointer | Store |
|---|---|---|
| Locals取得 | [utilities.go](/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/ast/utilities.go:61) | [getLocals](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/internal/binder/binder.go:147) |
| LocalsContainerBase | [構造体](/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/ast/ast_generated.go:139) | [意味情報列とmap](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/internal/ast/store.go:203) |
| NextContainer | [addToContainerChain](/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/binder/binder.go:2527) | [SetNextContainer](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/internal/ast/store.go:1065) |
| Flags読取 | [bind後段](/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/binder/binder.go:726) | [bindKind後段](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/internal/binder/binder.go:297) |
| list走査 | [bindEach](/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/binder/binder.go:1741) | [bindListRef](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/internal/binder/binder.go:2209) |
| ModifierFlags構築 | [NewModifierList](/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/ast/ast.go:159) | [modifierFlagsRef](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/internal/binder/binder.go:524) |
| ModifierFlags読取 | [Node.ModifierFlags](/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/ast/ast.go:612) | [Handle.ModifierFlags](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/internal/ast/store_query_manual.go:226) |
| Flow生成 | [newFlowNode](/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/binder/binder.go:449) | [NewFlowへの委譲](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/internal/binder/binder.go:778) |
| Flow格納 | [Identifier分岐](/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/binder/binder.go:596) | [SetFlow](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/internal/ast/store.go:902)、[flowID](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/internal/ast/store.go:983) |

リンク先は本書作成時の行番号。後日の変更で行が動いた場合は、artifactに固定したsource snapshotとidentityを正本とする。
