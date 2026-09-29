# Store checker JSDoc (TODO 7d-1) 実装指示書

作成日: 2026-09-28。対象は実装を担当するエージェント。設計の根拠は [store-checker-design-20260923.md](store-checker-design-20260923.md) (以下「7d 設計」。§番号はこの文書のもの) の §2.1 (段の表、7d-1 の移植の方法)、§2.2 (JSDoc reparse の形)、§3 の不変条件 17・20・22・24〜27。前段は 7d-0 ([実装指示書](store-checker-7d0-implementation-instructions-20260924.md)、[検証報告](store-checker-7d0-verification-report-20260927.md)。§12 の 2026-09-28 の修正を含む)。検証の手順と判定は [store-checker-7d1-verification-instructions-20260928.md](store-checker-7d1-verification-instructions-20260928.md)。

設計判断は 7d 設計と本書で済ませてある。書かれていない判断が必要になったら、実装を進めずに報告する。本書のうち 7d 設計に無く本書で決めたものは §9 に一覧した。

## 0. 作るものの性格

7d-1 は checker を動かさない。7b で (e) として外した JSDoc と JS の reparse を storeparser に戻し、**parse と bind の等価から JS の除外と緩めを全部外して mismatch 0** にする。あわせて、7d-2 の checker が TS の JSDoc を読むための遅延 parse と file ごとの共有 cache を作る。

1. **JSDoc の parse** (`parser/jsdoc.go` 1,355 行、54 関数): JS の JSDoc と、TS で `@see` / `@link` を含む comment は parse 時に本体の Store に入れる (eager)。TS の残りは遅延。
2. **JS の reparse** (`parser/reparser.go` 752 行、21 関数): `@typedef` / `@callback` / `@import` / `@overload` が作るノード、型注釈の複製、cast、modifier の追加などを本体の Store に入れる (B1)。
3. **遅延 JSDoc** (J-d): `node.JSDoc(file)` で、file ごとの共有 cache に RWMutex の下で parse し、完成して seal した Store だけを公開する。root の親は host への foreign 辺 (FR1)。

順序は 7d 設計 §2.1 のとおり、**1 と 2 (本体の Store に閉じ、Builder の API だけに依存) を先に、3 (7d-0 の region と FR1 に依存) を後に**行う。1 と 2 が等価テストを通ってから 3 に進む。

| | 7d-0 | 7d-1 |
| --- | --- | --- |
| `withJSDoc` の 56 箇所 | `p.b.AddFlags(x, p.jsdocFlags(info))`。JS も TS も scanner の結果で flag だけ | Pointer と同じ `withJSDoc`: JS は parse して reparse、TS は flag と `@see` / `@link` の eager parse |
| JSDoc ノード | 無い | JS と TS の `@see` / `@link` は本体の Store、TS の残りは遅延 JSDoc の Store |
| reparse が作るノード | 無い | 本体の Store。host より後の id (不変条件 20) |
| host → JSDoc | 無い | `File` の cache (`map[NodeRef][]Node`)。eager 分は parse の終わりに作り、遅延分は lock の下で足す |
| parse の等価 (JS) | `SkipReparsed`、JSDoc cast の透過、`MaskFlags` 4 bit ほか | 除外と緩め無し。JSDoc の木も比べる |
| bind の等価 | 330 file 除外、JS の緩め 2 件 | 除外と緩め無し |

作らないもの: checker の変換、nameresolver、`GetReparsedNodeForNode` などを checker 用に使う経路の検証 (7d-2)。ModifierFlags の bit 16 以降の置き場 (Pointer の parse と bind は bit 0〜15 しか書かない。`ModifiersToFlags` だけ。7d-2 で決める)。遅延 JSDoc の lock の待ちの計測 (7d-2)。

## 1. ガード

1. 変更してよいのは `tsc/internal/storeparser/` (JSDoc と reparse の移植、§4)、`tsc/internal/ast/store/` (§2・§3 の API、手書きと生成物)、`tools/scripts/tsc/generate-go-store.ts` (§2)、`tsc/internal/ast/store/storetest/` と storeparser / storebinder のテスト (除外と緩めを外す、§6)。**`storebinder` の本体 (`binder.go`) は変えない** (7c で JS の JSDoc 由来の case は逐語で移してある。変える必要が出たら報告する)。`tsc/internal/ast` (store 以外)、`binder`、`parser`、`checker`、`scanner`、`tsc/go.mod` を変更しない。`tools/cmd/storetranslate` は §5 の再実行だけで、規則を足す必要が出たら報告する。
2. **等価テストの除外と緩めを、JSDoc と reparse が理由のものは全部外す** (§6 に列挙)。外せないものが残ったら、理由と file の一覧を付けて報告する。新しい除外を足さない。
3. `unsafe` を増やさない (7d-0 の 2 箇所だけ)。
4. 生成物は `node tools/scripts/tsc/generate.ts` と `(cd tools && go run ./cmd/storetranslate)` で再現すること。
5. 各 file の先頭の doc comment (英語) に、7d 設計の該当節へのパスを書く。移植した関数は Pointer の関数と同じ名前・順序・コメントを保つ (7b の T1 と同じ)。
6. 既知の失敗は `FuzzEquivalence/a708303f611b95c8` (`ExternalModuleIndicator differs`) の 1 件だけで、SubtreeFacts の枝刈りが未実装なことによる。7d-1 では直さない。これ以外の失敗が出たら不合格として扱う。

## 2. スキーマと生成器

7b でスキーマに入れたが形が決まっていなかった 2 つを決める。

1. **`[]string` の text** (`JSDocText`、`JSDocLink` / `JSDocLinkPlain` / `JSDocLinkCode`): **連結した 1 つの文字列を text slot に持つ。** Pointer がこの field を読むのは `(*Node).Text()` の `strings.Join(…, "")` だけ (ast/ast.go:299〜302。checker / ls / printer に要素ごとの読みは無い、2026-09-28 に確認)。`classify` の `todo` を text slot に変え、Builder の constructor は連結済みの文字列を取る。`Factory.NewJSDocText` も同じ形にする。生成器の TODO コメントは消す。
2. **`JSDocParameterOrPropertyTag` の子の順**: Pointer の `ForEachChild` は `IsNameFirst` が false なら `TypeExpression` を `name` より先に訪ねる。固定の member 順では表せないので、**生成の `ForEachChild` (と `IterChildren` など子を順に辿る生成関数全部) にこの kind だけ手書きの case を出す** (`IsNameFirst` の bool slot で順を切り替える)。`memberOrderOverrides` の TODO はこの case への参照に書き換える。

どちらも、7d-1 の JSDoc の等価テスト (§6.2) の preorder の比較で確かめる。

## 3. `store` package

### 3.1 reparse の Builder API (B1、7d 設計 §2.2「reparse の Builder API」)

Pointer の reparse は、組み終えた host を書き換える (`.Type =`、`.TypeParameters =`、`.Parameters =`、`QuestionToken`、`bin.Right = makeNewCast(…)`、`Types.Nodes = append(…)`、`FullSignature`)。Builder に次を足す。**どれも `Finish` の前だけ使う** (Builder は source の Store を作るもので、公開した Store に書かない)。

1. **役割名の slot setter** (生成): `SetType(id, child)`、`SetTypeParameters(id, list)`、`SetParameters(id, list)`、`SetQuestionToken(id, child)`、`SetExpression(id, child)`、`SetInitializer(id, child)`、`SetRight(id, child)`、`SetModifiers(id, list)`、`SetFullSignature(id, child)` など、reparser.go が書く member の分。kind 多相 (7d-0 の子 accessor と同じ役割表) で、slot の無い kind は panic。新しい子の `parent` を host に書く (`adopt`)。**`SetModifiers` は header の `modifierFlags` を新しい block から計算し直す** (Pointer の `ModifierList.ModifierFlags` は list を作るときに `ModifiersToFlags` で決まり、reparse は新しい list を作って付け替える)。生成する setter の一覧は reparser.go が実際に書く member から決め、報告に載せる。
2. **`SetFlags(id, flags)` と `SetRange(id, pos, end)`**: `finishReparsedNode` (flags を `contextFlags | NodeFlagsReparsed` に置き換え、Loc を location node から写す) と `makeNewCast` の `finishNodeWithEnd` に使う。
3. **`ReparentChildren(id)`**: `finishMutatedNode` / `overrideParentInImmediateChildren` の Store 版。生成の `ForEachChild` で直下の子の `parent` を `id` に書き直す。
4. **`DeepCloneReparse(id) NodeRef`** (生成): `ast.NodeFactory.DeepCloneReparse` の Store 版。部分木を post-order で複製し、Loc は元のまま (合成位置にしない)、複製の子の `parent` を複製に、**根だけ** `NodeFlagsReparsed` を足す (子孫は元の flags、`NodeFlagsJSDoc` を含む)。list は新しい block。text は元の text を指す (再確保しない)。
5. list の作り直しは既存の `List(pos, end, elems)` で新しい block を作り、setter で付け替える。古い block と、付け替えで外れたノードは dead になる (7b の top-level await と同じ流儀)。

不変条件 20: JSDoc と reparse のノードは host より後の id を持つ。`Store.Root()` の「root が最後」だけは保つ (reparse は root を組む前に終わる。`parseSourceFileWorker` の末尾の `reparseList` の追加も root の前)。storeChecks build で `Finish` のときに「root が最後のノード」を検査する。

### 3.2 `File` の JSDoc の field と API

`ast.SourceFile` の field に合わせて `store.File` に足す (file.go の「The JSDoc cache is not here」のコメントは書き換える):

```go
jsdocDiagnostics []*ast.Diagnostic
jsdocCache       map[NodeRef][]Node // host (a node of f.Store) → its JSDoc
jsdocMu          sync.RWMutex
hasLazyJSDoc     bool
ReparsedClones   []NodeRef // sorted by position, as ast.SourceFile.ReparsedClones
```

API (Pointer と同名・同じ意味):

```go
func (n Node) JSDoc(file *File) []Node      // ast.(*Node).JSDoc
func (n Node) EagerJSDoc(file *File) []Node // ast.(*Node).EagerJSDoc: lazy mode では RLock の下で cache を読むだけ
func (f *File) JSDocDiagnostics() []*ast.Diagnostic
func (f *File) SetJSDocDiagnostics(d []*ast.Diagnostic)
func (f *File) SetJSDocCache(m map[NodeRef][]Node)
func (f *File) SetHasLazyJSDoc(v bool)
func SetParseJSDocForNode(parse func(f *File, host Node) []Node) // storeparser の init が登録する
```

- `JSDoc` の手順は ast.go:1563〜1577 と同じ: `HasJSDoc` が無ければ nil、file が nil なら `n.File()`、lazy なら `resolveJSDoc`、でなければ `jsdocCache[n.Ref()]`。
- **host の検査 (不変条件 27)**: `n` が `file.Store` のノードでなければ、lazy / eager のどちらでも**通常 build で panic** する。cache の key は file の source Store の `NodeRef` なので、別の Store の host は黙って別のノードの JSDoc を返す。合成の host (clone は `HasJSDoc` を含む flags を写す) がこの経路に届いたら、7d 設計 §2.2 のとおり共有 cache とは別の経路を設計することになるので、panic のまま 7d-2 に報告を回す。
- `resolveJSDoc` は ast.go:2745〜2770 の二重検査をそのまま移す: RLock で `ok` を見る、外して Lock、もう一度 `ok`、無ければ lock の中で parse し、**空の結果 (nil) も key を入れて記録**する。
- 本体の Store の JSDoc ノードを指す `Node` は、`Finish` (Compact) の後の Store を指さなければならない。eager 分の cache は `finishSourceFile` で `FinishFile` の後に、parser が集めた `jsdocInfos` (host と JSDoc の `NodeRef`) から作る (Pointer の `createJSDocCache`)。

### 3.3 遅延 JSDoc の Store (J-d)

storeparser の入口 (§4.3) が Builder で JSDoc を組んだ後、store の側で公開用に仕上げる関数を置く (Builder は local の辺しか書かないので、host への foreign 辺は store の中で書く):

```go
// FinishLazyJSDoc finishes b as the lazy JSDoc Store of host's file: the
// Store takes the next StoreID of the program, the file of host, a foreign
// parent edge from each of roots to host, and is sealed.
func FinishLazyJSDoc(b *Builder, host Node, roots []NodeRef) []Node
```

1. `Finish` の後で StoreID を振る: host の Store の `space.Next()` (不変条件 22)。空の結果は Store を作らない (StoreID も使わない)。
2. `Store.file` は host の file。`File()` は親を辿らずこれで答える (不変条件 25)。
3. 各 root (JSDoc ノード) の `parent` を host への foreign 辺にする (7d-0 の foreign 表。source の Store は foreign 表を持たないが、遅延 JSDoc の Store は持つ)。`Parent()` が host を返し、`GetJSDocHost` がそのまま動く。
4. `sealed` を立ててから返す。公開した Store には以後書かない。Factory の取り込みの境界 (7d-0 の `checkImport`) は、同じ program の遅延 JSDoc の Store を `owner == nil && space == region.space` で受け、別 program を拒否する。
5. **program の外の file** (`storeparser.ParseSourceFile` で parse し `space` が nil) の遅延 JSDoc は、同じく program の外の Store (StoreID 0、`space` nil) にする。どの Factory も取り込まない。parse の等価テストはこの形で遅延 JSDoc を比べる。

## 4. `storeparser`

### 4.1 移植 (T1)

`parser/jsdoc.go` を `storeparser/jsdoc.go`、`parser/reparser.go` を `storeparser/reparser.go` に手作業で逐語移植し、7b の diff の記録と同じく hunk を分類する。7b の検証報告の (e) (「12 関数 + field 11」、store-parser-verification-report-20260922.md の表) の中身は文書に列挙が無いので、Pointer の 2 file の 75 関数と `parser.go` 側の次を戻す:

- `Parser` の field: `jsdocDiagnostics`、`hasDeprecatedTag`、`jsdocInfos`、`jsdocCommentsSpace`、`jsdocCommentRangesSpace`、`jsdocTagCommentsSpace`、`jsdocTagCommentsPartsSpace`、`reparseList`、`reparsedClones`、`stringSliceArena` (Pointer で jsdoc.go だけが使う)。`ParserState` の `jsdocInfosLen`、`reparsedClonesLen` と `mark` / `rewind` での truncate。
- `GetJSDocCommentRanges` (parser/utilities.go:28)、`createJSDocCache`、`type JSDocInfo`。
- `parseListIndex` の `reparseList` の差し込み (`IsJSTypeAliasDeclaration || IsJSImportDeclaration` を外側の list へ押し出す枝を含む)、`parseSourceFileWorker` の末尾の `reparseList` の追加、`parseToplevelStatement` の `i += len(p.reparseList)`。
- `finishSourceFile` の `SetJSDocDiagnostics`、`SetJSDocCache(createJSDocCache())`、TS での `SetHasLazyJSDoc(true)`、位置で並べた `ReparsedClones`。
- 56 箇所の `p.b.AddFlags(x, p.jsdocFlags(info))` を Pointer と同じ `p.withJSDoc(x, info)` に戻し、`jsdocFlags` を消す。

Pointer の機構の置き換えは 7b / 7c の流儀に従う: `p.factory.NewXxx` → Builder の `NewXxx`、`nodeSliceArena` と `newNodeList` → Builder の `List`、`finishNode` → constructor の pos / end、`DeepCloneReparse` → §3.1 の 4、`overrideParentInImmediateChildren` → §3.1 の 3、node の field への代入 → §3.1 の setter。`reparsedClones` と `jsdocInfos` は `NodeRef` で持つ。

### 4.2 speculation

Pointer の `mark` / `rewind` は `jsdocInfos` と `reparsedClones` の長さを戻し、`jsdocDiagnostics`、`reparseList`、`hasDeprecatedTag`、ノードに適用済みの書き換えは戻さない (parser.go:341〜375)。**同じにする** (戻すものを増やさない、減らさない)。Builder の `Truncate` がノードと `extra` を戻すので、`jsdocInfos` / `reparsedClones` に truncate 後の `NodeRef` が残らないことを storeChecks build で検査する。

ただし `reparseList` が speculation の行を指す rewind では `Truncate` しない ([store-checker-7d1-rewind-fix-instructions-20260929.md](store-checker-7d1-rewind-fix-instructions-20260929.md))。

### 4.3 遅延 parse の入口

`parser/jsdoc.go:19〜37` の `parseJSDocForNode` を移す: pool の parser を取り、file の parse option・text・script kind で `initializeState` し (TS の `contextFlags` は None。host の context flags は写さない。Pointer と同じ)、`GetJSDocCommentRanges` の各 range を `parseJSDocComment` で組む。結果を §3.3 の `FinishLazyJSDoc` で仕上げて返す。空なら nil。`init` で `store.SetParseJSDocForNode` に登録する。diagnostics は Pointer と同じく捨てる (TS では `jsdocDiagnostics` に移らない)。`NodeCount` / `IdentifierCount` を更新しない。

## 5. 変換器

`(cd tools && go run ./cmd/storetranslate)` を再実行する。根は 7d-0 と同じく「7c の utilities + storeparser / storebinder が `store.X` で呼ぶ ast 関数」なので、移植した jsdoc.go / reparser.go が呼ぶ ast の utility が増える。規則で訳せないものは P2 (`utilities_manual.go`、hash の注記付き) にし、1 件ごとに理由を報告する。規則を足す必要が出たら足さずに報告する。

## 6. テスト

### 6.1 除外と緩めを外す

次を全部外し、JS を TS と同じ規則で比べる。外した後で使われなくなった option と helper は消す。

- `storetest.Options` の `SkipReparsed` と `isJSDocCast` の透過 (equivalence.go:18〜25、56〜92、120、143、176〜190)、`MaskFlags` (:232)。
- storeparser (parser_test.go): JS / JSX の option (:56〜69)、`eagerJSDoc` の `IdentifierCount` の skip (:130)、`fieldMismatches` の `fromJSDoc` (Imports と ModuleAugmentations から JSDoc 由来を落とす) と `ExternalModuleIndicator` の skip (:139〜181)、JS だけ root の Flags を log にする扱い (:327〜333)、JS の diagnostics を部分列で許す扱い (:346、`subsequence`)。`FuzzEquivalence` の同じ規則 (:390〜420)。
- storebinder: binder_test.go の option (:47〜56、`ThisNodeOrAnySubNodesHasError` の mask を含む)、bindequiv_test.go の `fromJSDoc` / `nodes` (:270〜287、:311、:314〜315)、330 file の除外 (`reparsedEffects` :371〜411、`isTypeOfExpandoOrRequireDeclaration` :414〜435、`excluded` :438〜440、binder_test.go :196〜209 と :225〜232 の log)。
- 行番号は 2026-09-28 の HEAD (3bb0252405) のもの。上に無い JSDoc / reparse 由来の緩めを見つけたら、それも外して報告に書く。

### 6.2 足すテスト

1. **JSDoc の木の等価** (corpus の parse 等価の中): 両方で `HasJSDoc` の host ごとに `host.JSDoc(file)` を Pointer と比べる (個数、各 JSDoc の preorder の kind・flags・pos・end・text、`Parent()` が host)。TS は遅延、JS と TS の `@see` / `@link` は eager を通る。`File.ReparsedClones` (位置と kind)、`JSDocDiagnostics` も比べる。これで §2 の text と子の順も確かめる。
2. **遅延 JSDoc の契約** (`store` か `storeparser` の単体テスト、`ParseSourceFileIn` で program を持たせる):
   - 同じ host を 2 回引いて parse が 1 回 (export_test の parse の回数で数える)。
   - 空の結果 (`HasJSDoc` があり range が無い、または parse が空) が記録され、2 回目に parse しない。
   - root の `Parent()` が host と `SameNode`、root から `File()` が host の file、root の Store の StoreID が source の範囲 (0..nFiles−1) の外、`sealed`。
   - 同じ program の region の Factory が遅延 JSDoc のノードを子に取れ、別 program の region は通常 build で panic。
   - 合成の host と別 file の host で `JSDoc(file)` が通常 build で panic (不変条件 27)。
   - 複数 goroutine から同じ file の別の host と同じ host を同時に引き、`-race` で走らせて、host ごとに parse 1 回・結果が同じ。
3. **Builder の API**: §3.1 の setter が slot の無い kind で panic、`SetModifiers` が header の `modifierFlags` を計算し直す、`DeepCloneReparse` の根だけ Reparsed・Loc が元のまま、`ReparentChildren` の後に直下の子の `Parent()` が host。
4. `TestDeadNodes` を JS も数えるようにし、reparse の付け替えで生じる dead node の数を報告する。

### 6.3 正しさと性能を分ける

7d-0 §6.3 と同じ。正しい実装が性能の線を外したら「性能未達」と報告し、検査や共有の意味を弱めて線を通さない。

## 7. ベンチ

既存の Walk / Parse / Bind の KPC と retained を名前を変えずに通す。**遅延 JSDoc の parse の費用のベンチを足す**: dom.generated.d.ts を parse し、全ての `HasJSDoc` の host で `JSDoc(file)` を引く (新しい file で毎回)。Pointer (`(*ast.Node).JSDoc`) と Store の ns / B/op / allocs を比べる (KPC 版も同じ形で足す)。線は置かず報告する (7d-2 の Check に含まれる費用の前見積り)。

## 8. 完了条件

1. 生成物が再現し、`git status` の変更が §1 の範囲だけ。
2. `go build ./...`、`go vet ./internal/ast/... ./internal/storeparser/... ./internal/storebinder/...` (tsc) と `go vet ./cmd/storetranslate/...` (tools) が通る。
3. §6 の全テストが tag なしと `-tags storechecks` で通る (既知の fuzz seed 1 件を除く)。§6.2 の 2 の並列テストは `-race` でも通る。
4. parse の等価: corpus 17,415 file、JS を含めて除外 0・mismatch 0、JSDoc の木の等価も 0。bind の等価: 除外 0・mismatch 0。
5. §7 のベンチが完走。
6. 報告 (`store-checker-7d1-implementation-report-<YYYYMMDD>.md`) に: (a) storeparser の hunk の分類 (7b の分類に合わせる) と、jsdoc.go / reparser.go で逐語にならなかった箇所と理由、(b) `store` の API の追加 (§3.1 の setter の一覧を含む) と生成器の変更、(c) 外した除外と緩めの一覧 (§6.1)、見つけて外した追加分、外せなかったもの、(d) 変換器の再実行で増えた関数と P2、(e) dead node の数、(f) inline の変化 (Builder の constructor と setter、`JSDoc`)、(g) 7d 設計と本書に書かれていない判断をした箇所 (無いはず。あれば実装を止めて報告したか)。

## 9. 本書で決めたこと (7d 設計に無いもの)

| 項目 | 決めたこと | 根拠 |
| --- | --- | --- |
| `[]string` の text | 連結した 1 つの文字列を text slot に持つ (§2 の 1) | Pointer の読みは `Text()` の `strings.Join` だけ |
| `JSDocParameterOrPropertyTag` の子の順 | 生成の子の走査に手書きの case (§2 の 2) | Pointer の `ForEachChild` が `IsNameFirst` で順を変える |
| Builder の setter の範囲 | reparser.go が書く member だけを生成、`SetModifiers` は `modifierFlags` を計算し直す (§3.1) | B1 の「Pointer と同じ形」。header の `modifierFlags` は list から計算した値のキャッシュ |
| `DeepCloneReparse` | Builder に生成 (§3.1 の 4) | 7d 設計 §2.2 は複製を reparse のノードに挙げるが API を書いていない |
| host → JSDoc の置き場 | `File` の `map[NodeRef][]Node`。host が file の source Store のノードでなければ通常 build で panic (§3.2) | Pointer の `SourceFile.jsdocCache`。不変条件 27 を eager 側にも広げる |
| 遅延 JSDoc の仕上げ | `store.FinishLazyJSDoc` (§3.3) | Builder は local の辺しか書かない |
| program 外の file の遅延 JSDoc | program 外の Store (StoreID 0、space nil) (§3.3 の 5) | 等価テストは program を持たない。Factory は取り込まない |
| speculation で戻すもの | Pointer と同じ (§4.2) | 逐語移植 |
