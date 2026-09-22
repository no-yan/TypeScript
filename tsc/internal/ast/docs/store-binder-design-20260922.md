# Store binder (TODO 7c) 設計: 決定事項と検証計画

作成日: 2026-09-22。設計ドキュメントのみ。コードは変更しない。

Store AST ([store-ast-design-20260922.md](store-ast-design-20260922.md)、以下「設計文書」) の上で binder を動かす TODO 7c (設計文書 §6 の 8) の設計。7b ([store-parser-verification-report-20260922.md](store-parser-verification-report-20260922.md)) が作る `store.File` を入力に、bind の出力をどこに置くかと、それをどう検証するかを決める。決定の順序と問いは [store-binder-design-prompt-20260922.md](store-binder-design-prompt-20260922.md)。

数字の出所: 断りが無ければ 2026-09-22 に Pointer の binder で checker.ts (298,054 node) と dom.generated.d.ts (109,605 node) を bind して数えたもの、および corpus (7b と同じ 17,415 file) の JS 1,456 file。手順は §6。

## 1. 目標とゲート

設計文書 §1 のゲート 2。`BenchmarkASTBindKPCV1` と同じ契約で checker.ts、**cycles/node** で Pointer の 1.1 倍以内。Pointer は 33.8M cycles / 298,054 node ≈ 113.5 cycles/node (GC オフの Session、`_kpc-baselines/kpc-after.txt`)、棄却線は約 125。inst では判定しない (Walk の +20 inst/visit が先に余裕を使う)。retained / GC の線は §5 で決める。

§2.3 で header を 32B にするので、7a の Walk ゲート (24B で cycles 1.137) と 7b の Parse (24B で cycles 0.813) も 32B で取り直し、24B の before と並べる (§5)。

前回の Store branch (b719) で bind が 1.5 倍になった内訳は、命令数 1.79 倍 (メモリ待ちではない)。4 割が走査機構 (kind の 2 段 switch、`childAt`、`listOwner`)、side map +54M inst、`SetFlow` / `flowID` +57M。binder の逐語訳 (NodeRef を渡して毎回 `Kind()` / `Parent()` を引き直す形) は Monaco で inst +21%。この文書の決定はどれも「同じ穴に落ちない」ことを根拠に書く。

## 2. 決定事項

### 2.1 範囲

**binder.go だけを移植する。** nameresolver.go と referenceresolver.go は 7c に含めない。binder.go はどちらも呼ばず (`lookupName` は binder 内の独自実装)、呼び手は `NameResolver.Resolve` が checker 2 箇所と ls/autoimport、`ReferenceResolver` が transformers 8 file・printer・emitter で、7c の段階でこれらを呼ぶ package は無い。nameresolver は 7d (checker) と、referenceresolver は emit の設計と一緒に port する。4.3 の Locals slot は nameresolver の読み方 (`Resolve` 配下の locals map が前回 0.78G inst) を前提に決めるが、port はしない。

**JS file は等価テストに入れる。ただし symbol を持つ Reparsed ノードがある file は除外する。** corpus の JS 1,456 file のうち Reparsed ノードを含むのは 573、そのうち binder が symbol を付けた Reparsed ノードを含むのは 293 (corpus 全体の 1.7%)。kind は `JSTypeAliasDeclaration` 288 file、`TypeLiteral` 210、`TypeParameter` 177、`PropertySignature` 159、`FunctionType` 120、`FunctionDeclaration` 23、`ModuleDeclaration` 20 など。binder の JS 専用経路のうち CommonJS 判定 (311 file)・expando・`this.x` は構文木だけで動くので、残る 1,163 file で検証される。JSDoc 由来なのは `JSTypeAliasDeclaration` の遅延 bind と `IsImplicitlyExportedJSDocDeclaration` の 2 つで、7d が JSDoc の置き場 (設計文書 §5) を決めたときに 293 file を再検証する。除外条件は「`NodeFlagsReparsed` かつ `DeclarationData().Symbol != nil` のノードを 1 つでも持つ」と機械的で、テストが file を log する。7b の `SkipReparsed` + `MaskFlags` の規則はそのまま使う。

**flow の型は 7c で新設する。index 連結の noscan slab にし、packed 計画の最適化は含めない。** 「Pointer の `ast.FlowNode` arena をそのまま使う」案は成立しない: `FlowNode.Node` が `*ast.Node` で、Store parse には Pointer ノードが無い。したがって新型は必須で、問いは pointer 連結か index 連結かだけになる。

| | checker.ts | dom |
| --- | ---: | ---: |
| flow を持つノード | 172,843 (58%、うち Identifier 123,554) | 37,215 (34%) |
| 到達可能な FlowNode | 45,694 (0.15/node) | 4,841 |
| FlowList | 15,402 | 0 |
| 現行の大きさ (32B + 16B) | 1.71 MB = 5.7 B/node | 1.4 B/node |
| 合成 Node (SwitchClause / ReduceLabel) | 878 | 0 |

- `FlowNode{Flags, Node NodeRef, Antecedent, Antecedents uint32}` 16B を `[]FlowNode`、`FlowList{Flow, Next uint32}` 8B を `[]FlowList` に置く。どちらも noscan。checker.ts で 0.85 MB。
- node → flow は密でなければならない: 58% のノードが flow を持つので疎列に意味が無く、`[]*FlowNode` (8 B/node = 2.4 MB) は flow graph 本体より大きい。置き場 (`[]uint32` の側列か header か) は §2.3 で決め、**header に置く**。
- binder の `currentFlow` / label 類は slab の index になる (slab は append で動くので pointer は持てない)。増分は bounds check で、flow の生成と label 書き込みは約 0.7 回/node なので ≤ 3 inst/node (Pointer 371 の 0.8%)。数えられる量なので実験はしない。
- 合成 Node (`FlowSwitchClauseData` / `FlowReduceLabelData` を `Node` field に詰める Pointer の形) の代替表現は §2.3 (flow の行) で決める。
- 列の名前と置き場所 (どの package の struct か) は §2.3 で決める。

### 2.2 移植の規則 (通貨)

**訪問ノードと binder の container 系ローカル (`container`、`blockScopeContainer`、`thisContainer`、`lastContainer`) は `store.Node{s,h}`。side 列を引くときだけ `Ref()` を呼ぶ。** flow 系ローカル (`currentFlow`、`current*Target`、label) は §2.1 のとおり slab の index (`FlowRef uint32`)。

根拠は bind の hot loop で訪問ノード 1 つの header を読む回数と、関数境界ごとの通貨の費用の 2 つ。

| 場所 (Pointer の関数境界のまま) | 読む field | 回数 |
| --- | --- | ---: |
| `bind`: nil 判定、`switch node.Kind`、`Flags&ThisNodeHasError`、`Kind > LastToken` | kind ×3、flags ×1 | 4 |
| `GetContainerFlags`: `switch node.Kind` (Block は `Parent.Kind` も) | kind ×1 | 1 |
| `bindChildren`: `FirstStatement <= Kind`、`switch node.Kind` | kind ×2 | 2 |
| `ForEachChild`: `switch kind` | kind ×1 | 1 |
| 合計 (token 以外、4 関数にまたがる) | | 8 |

通貨の費用は、checker.ts の Store を線形に回して関数境界 2 段を noinline で越えるマイクロベンチで測った (scratchpad `measure/currency-bench.txt`、`zz_currency_test.go`):

| 通貨 | 2 段の inst (objdump) | 境界 1 回あたりの差 | ns/node |
| --- | ---: | ---: | ---: |
| `*NodeHeader` (1 word) | 24 + 8 = 32 | 0 | 4.65〜4.69 |
| `Node{s,h}` (2 word) | 28 + 12 = 40 | +2 (引数のスタック退避 2 本。leaf でも出る) | 4.68〜4.71 (+1%、run 幅の内側) |
| `NodeRef` + 呼び先で `s.node(id)` | 40 + 36 = 76 | +18 (bounds check 込みの解決 7〜8 と、その値の保持) | 4.92〜5.09 (+5%、call 支配のベンチで) |

- **`NodeRef` を渡して呼び先で解決する形は採らない。** 境界 4 つで約 +70 inst/node (Pointer 371 の 19%) になり、前回の rewrite +21% と一致する。
- **1 word の `*NodeHeader` は採らない (差し戻し可)。** binder は単一 Store なので `b.s` から `Node` を作り直せて、理屈の上では通貨を 1 word にできる。差は境界 1 回 +2 inst (スタック退避) で、bind の境界を 5 つと見て約 10 inst/node (2.7%)、ns では 1% 以下。header のスカラ accessor を `*NodeHeader` にも生やすか `Store.Node(h)` を公開する API の追加に見合わない。`View` が 47 inst / 35 cycles だったのは 5 word の `Store` を heap の field に書き戻していたためで、レジスタに載る 2 word の構造体の費用ではない。**Bind ゲートを cycles で落とした場合に最初に試す案として残す。**
- `Ref()` が要るのは side 列を引くときだけで、§2.3 (flow と symbol を header に置く) の下では flow の生成時の `FlowNode.Node` (0.15〜0.3 回/node)、宣言の `FileRef()` (6%)、`containers` (2%)、pattern ambient module の名前、で合わせて **約 0.25〜0.3 回/node**。24B なら magic 乗算 6 inst で約 2 inst/node、32B (§2.3) なら shift 1〜2 inst で 0.5 inst/node。
- Identifier (41%) の `IsIdentifierName` (Parent 1 段 +8.7 inst、`Name()` 在 +12) は 0.41 × 約 20 ≈ +8 inst/node (2%) で既知の負債。`checkContextualIdentifier` の条件順を入れ替えて避ける案は Pointer 版でも関数単体 −26% (2026-09-16) なので、7b §11.4 の scanASCIIWhile と同じく「両方に入れるか、入れないか」とし、7c では入れない。

**走査は生成 `ForEachChild` と Pointer と同じ訪問順** (statements の functions-first、flow 用の手書き順もそのまま)。線形 pass は採らない: flow 付け (58%)、container stack、symbol 宣言が訪問順に依存するので木の走査は無くならず、線形 pass に移せるのは `ThisNodeOrAnySubNodesHasError` の伝播 (save / restore + or で約 6 inst/node) 程度で、別 pass の費用 (parent index を読んで or、約 5 inst/node) と相殺する。線形 pass の候補は §2.5 の `ExternalModuleIndicator` (statements の kind を見るだけ) に限る。

**移植の記録は 7b と同じく diff。** `internal/binder/binder.go` との diff を記録にし、hunk を次に分類して (f) と (g) を全部列挙する。

| 分類 | 内容 |
| --- | --- |
| (a) 型の置換 | `*ast.Node` → `store.Node`、`*ast.FlowNode` → `FlowRef`、nil → `IsNil()` / `NoFlow` |
| (b) 出力先の置換 | node の field への書き込み → §2.3 の列 (`symbolOf[ref]`、`flowOf[ref]`、`h.flags |=`) |
| (c) 子の受け直し | typed view の accessor に |
| (d) flow の index 化 | `newFlowNode` 系、`addAntecedent`、label の書き込み |
| (e) JSDoc 系の削除 | Reparsed ノードにしか効かない分岐 |
| (f) それ以外 | 逐語でない書き換え。**Pointer 版と Store 版の header の読み回数を並べて理由にする。** Pointer にも効く書き換えは (f) に入れず別提案にする |
| (g) 読みのまとめ | 逐語移植の後に 1 pass で行う。同じノードに対する同じ accessor の呼び出し (例: `if n.Kind() != A {} else if n.Kind() != B {}`、`Name()`、`Parent()`、slot 読み) を、間に mutation が無ければローカルに 1 回で受ける。**parse 後不変のもの (kind、pos、end、parent、子、text) だけが対象。bind 中に変わるもの (`flags`、symbol / locals / flow の列、`file` の field) はその場で読む** |

(g) を分ける理由: 逐語移植は Pointer の deref 1 回 (1〜3 inst) を Store の accessor (`Parent()` 11、`Name()` 22、slot 12) に置き換えるので、Pointer では気にならなかった繰り返しが Store では倍加する。7b にこの分類が無かったのは parser が子をほぼ 1 回しか読まないため。

### 2.3 bind の出力の置き場所

**header を 32B にし、node → flow と node → symbol を header に置く。残りの疎な出力は kind ごとの予約 slot (`extra`) に置く。** 設計文書 §2.5 再検討 4 で「§5 と一緒に決める」と保留していた 32B header は、ここで採用に決まる。

前提の訂正 2 つ:

- 予約 slot は schema から出せる。`ast.json` の base に `Symbol` / `LocalSymbol` / `Locals` / `NextContainer` / `FlowNode` / `EndFlowNode` / `ReturnFlowNode` の field が `goOnly` で既にあり、generator は今それを「side table 行き」の TODO にしている。slot class を 1 つ足せば、どの kind に何の slot があるかは Pointer と同じ集合で生成され、不変条件 2.8.2 (offset は全部生成) が保てる。
- flow を予約 slot で持つ案は Identifier で破綻する。flow を持つノードの 71% が Identifier で、Identifier は payload 0 word (`data` = text の開始位置、設計文書 §2.4)。slot を足すと 41% のノードに `extra` の割り当てが増え、text の置き場も変わる。

決め手の数字 (checker.ts、check の動的回数は [accessor-dynamic-frequency-20260921.md](accessor-dynamic-frequency-20260921.md) §5.5):

| 出力 | 持つノード | check の読み | 書き (bind) |
| --- | ---: | ---: | ---: |
| node → flow | 58% (Identifier 41%) | 321k | 173k |
| node → symbol | 6.2% (dom 23.5%)、slot を持つ kind は 18% | 898k (51% VariableDeclaration) | 18k |
| locals | 1.9% (slot を持つ kind は 4.6%) | 2,299k (60% が不在 kind) | 5.6k |
| LocalSymbol | 0.006% (dom 0.1%) | 少 (checker 1 file) | 19 |
| EndFlow / ReturnFlow / Fallthrough | ≈ 1.3% / 0.5% / 0.05% | 524k (`BodyData`) | 3k |

```go
type NodeHeader struct {            // 32B、noscan
    kind          Kind
    modifierFlags uint16
    flags         NodeFlags
    pos, end      int32
    parent        NodeRef
    data          uint32
    flow          FlowRef             // bind が書く。0 = nil。全 kind
    symbol        SymbolId            // bind が書く。0 = nil。全 kind
}
```

| 出力 | 置き場 | 備考 |
| --- | --- | --- |
| node → flow | `NodeHeader.flow` | 全 kind。Pointer の `FlowNodeData() != nil` 判定は不要 (無い kind は 0 のまま)。これが成り立つのは binder の 4 呼び出し箇所が kind でガードされている (statement は `StatementBase` が `FlowNodeBase` を埋め込む、他は kind の分岐の中) からで、ガード無しの書き込みが増えたら flow を持たない kind に書くことになる。読み書き 1 load / 1 store で、`h` が手元にあるので `Ref()` が要らない |
| node → symbol | `NodeHeader.symbol` | 全 kind。JS の `BinaryExpression` / `CallExpression` の expando symbol も同じ場所で、kind の列挙が要らない |
| symbols の実体 | `Bound.symbols` (file ごとの slab、id は 1 始まり) | 型と slab の形は §2.4 |
| Locals | container kind (`LocalsContainerBase` を埋め込む定義、直接 13 + `FunctionLikeBase` 経由) の予約 slot → `Bound.locals []SymbolTable` の index。`Locals()` は kind 表の番兵 (0xFF) で不在 kind を 1 比較で弾く | 表は `SymbolTable` (map) のまま。7c の等価テストは key 集合を比べ、hot な読みは 7d の nameresolver なので、map をやめるかは 7d で `Resolve` の実測で決める |
| LocalSymbol | `ExportableBase` を埋め込む定義 (13、checker.ts で 3.5% のノード) の予約 slot | Symbol 側に逆リンクを持つ案 (18k symbol × 4B) より小さく、Pointer と同じ場所 |
| EndFlow / ReturnFlow / Fallthrough | `BodyBase` (2 定義 + 合成) / 4 kind / CaseClause・DefaultClause の予約 slot | checker が `BodyData()` 経由で 524k 回読む。typed view の 1 slot 読み |
| NextContainer | `Bound.containers []NodeRef` (宣言順の列、checker.ts で 5.6k) | 読み手は printer 1 箇所。連結リストを列にするだけ |
| 合成 flow (SwitchClause 878 / ReduceLabel) | `Bound.flowData []struct{a, b, c uint32}`。`FlowNode.Node` を flags で index に読み替える | Pointer が `*Node` に詰める形の index 版。SwitchClause は (switchStatement, clauseStart, clauseEnd)、ReduceLabel は (target, antecedents) |
| `Flags` 書き (binder.go で `node.Flags` に書く 12 行。他の `Flags |=` は symbol / flow / `emitFlags`) | `Node.AddFlags` / `Node.ClearFlags` を store に足す。`storeChecks` build では `Store.Seal()` 後の呼び出しで panic | Builder は bind まで生かさない (Compact 後の `{s,h}` を配る契約、設計文書 §2.7)。予約 slot の setter (`SetLocals` 等、生成) も同じ規則。header の `flow` / `symbol` も `Node.SetFlow` / `SetSymbol` で書く |
| file の field | file の `Symbol` は Pointer でも独立 field ではなく root ノードの `DeclarationBase.Symbol` (`SourceFile` が `DeclarationBase` を埋め込む。JSON 経路は root の symbol を一時的に上書きして復元する) なので、Store でも **root header の `symbol`** で、`File.Symbol()` はその読み。`Bound` には置かない。checker / LS / compiler が読む `SymbolCount`、`PatternAmbientModules`、`GlobalExports`、bind diagnostics、`CommonJSModuleIndicator` は `File.Bound`。`IsBound` は Pointer と同じく bind 完了後に atomic で立てる (`Bound` は bind 開始時に付くので `Bound != nil` では bind 中に true になる)。`ExternalModuleIndicator` は parser の出力なので `File` 直下 (§2.5)。`notConstEnumOnlyModules`、`expandoAssignments`、`symbolCount` は Binder に留める | 読み手 (grep、binder 外): PatternAmbientModules checker + ls/autoimport、GlobalExports checker + ls、SymbolCount checker + compiler + tsc、BindDiagnostics / IsBound compiler、CommonJSModuleIndicator checker + ls + transformers、NextContainer printer、EndFlow / ReturnFlow / Fallthrough checker、LocalSymbol checker |
| 列の所有者 | 型 (`FlowRef`、`FlowNode`、`FlowList`、`SymbolId`、`Symbol`、`SymbolTable`、`Bound`) は `store` package。書き手は `storebinder` package | 前例: Pointer では `ast` が `Symbol` / `FlowNode` を持ち、`binder` が書く。Store 本体は header の 2 word と slot の uint32 を「意味を知らない番号」として持つだけで noscan のまま。`Bound` だけが scan 対象 |

費用の見積り (checker.ts): header +8 B/node、予約 slot ≈ 0.4 B/node、flow slab 0.85 MB (2.9 B/node)、symbol は §2.4。symbol の 4B は 94% のノードで空だが、それと引き換えに `Ref()` が shift 1〜2 inst、`node()` −1 inst、parent 1 段 2.64 → 2.09 ns (−21%、storeexp D 案の実測) になり、check の `Parent` 49 回/node の支配項に効く。

32B にすると 7a の Walk ゲート (24B で cycles 1.137) と E10 (footprint) の数字が変わるので、7c の検証で Walk KPC を 32B で取り直す (§5)。

### 2.4 Symbol の型

**`store.Symbol` を新設する。** `ast.Symbol` と同じ field 構成で、`Declarations []Ref`、`ValueDeclaration Ref` だけが変わる (どちらも 8B なので 96B のまま)。binder は新型だけを書き、checker 着手 (7d) まで両方の型が並存する。

`ast.Symbol` で Store に持ち込めないのはこの 2 field だけである。symbol 同士の参照 (`Parent`、`ExportSymbol`、`SymbolTable` の値、`PatternAmbientModule.Symbol`) は file をまたぎ得て、checker は merge で `Parent` 15 箇所・`Declarations` 14 箇所・`Members` / `Exports` 各 4 箇所を書き、transient symbol を自分で作って table に入れる。file ごとの slab の index では checker の globals を表せないので、**symbol 同士の参照は `*Symbol` のまま**にする。

| 項目 | 決定 | 理由 |
| --- | --- | --- |
| symbol 同士の参照 | `*Symbol`。`SymbolTable = map[string]*Symbol` | 上記 |
| header の `SymbolId` → `*Symbol` | `Bound.symbols []*Symbol` (id = index、0 = nil)、実体は `core.Arena[Symbol]` | id → pointer が 1 load。表は 8B/symbol (checker.ts で 0.5 B/node)。固定 chunk の arena (`chunks[id>>8][id&255]`) は読みが同じ 2 load で新しい arena 型が要るので採らない |
| `id` | `atomic.Uint64` の遅延採番をやめ、生成時に slab の index を `id uint32` に入れる (file 内で密) | binder が private 名 (`#<id>@name`) に使う id の要件は「同じ symbol なら同じ」だけ。checker の links の key を global にする方法 (file index との組か、transient の採番か) は 7d |
| `Declarations` | `[]Ref` (file + id)。7c では全て同一 file だが、7d の merge が file をまたぐので最初から `Ref`。`singleDeclarationsArena` は `core.Arena[Ref]` | `[]NodeRef` にして後で変えると 7d のコードを 2 回書く |
| `ValueDeclaration` | `Ref` (zero = nil) | `SetValueDeclaration` の `Kind` 比較は `Ref` を Store で解決して読む。7c は 1 file なので `File.Store` |
| `PatternAmbientModule` | `store.PatternAmbientModule{Pattern, Symbol *Symbol}` | 型の付け替え |
| `Members` / `Exports` の遅延生成 (`GetExports` 等) | `store` に同名の関数 | 変える理由が無い |

### 2.5 7b からの引き継ぎ

**`ExternalModuleIndicator` と module references は parser の末尾で計算する (Pointer と同じ位置)。** `finishSourceFile` で `Builder.View` を通して indicator を出し、`reparseTopLevelAwait` を起動し、`Finish` の後に Store 上で `collectExternalModuleReferences` を走らせる。reparse は木を書き換えるので Compact の前でなければならず (設計文書 §2.7)、bind の先頭に置く案は成立しない。

- `isFileProbablyExternalModule` は top-level statements のループで木を歩かない。木を歩くのは `import.meta` (flag `PossiblyContainsImportMeta` のときだけ) と JSX tag (opts.JSX かつ statements に indicator が無い TSX だけ) の 2 経路で、生成 `ForEachChild` の pre-order で逐語に移す。JSX 経路は Pointer が `SubtreeFacts` で枝刈りするが Store には無いので全走査になる。稀な経路で 7c に parse のゲートは無い。読み手は nil 判定・`== file`・`Kind == SourceFile` の 3 種だけで、どのノードかは挙動に効かないが、等価テストが位置で比べられるよう pre-order を保つ。
- `collectExternalModuleReferences` の動的 import は text 走査 (`findImportOrRequire`) + `GetNodeAtPosition` の降下。`GetNodeAtPosition` の Store 版 (pos / end で `ForEachChild` を降りる) は `store` に置く (LS も使う形)。
- `store.File` に足す field: `ExternalModuleIndicator NodeRef`、`Imports []NodeRef`、`ModuleAugmentations []NodeRef`、`AmbientModuleNames []string`、`UsesUriStyleNodeCoreModules core.Tristate`。読み手は compiler 5・ls 5・checker 2 file など。

**top-level await**: `reparseTopLevelAwait` の起動を有効にし、7b で除外した 8 file (#713、#714、#3037、#5255、#7521、#8555、#12580、#12584) を等価テストの除外から外す。`Mark()` を rewind の前に取ってノードを残す 7b の書き方 (7b 報告 §4.3 #19) が dead node を増やしていないかを `TestDeadNodes` に 1 ケース足して確認する。先に片付けず 7c の parser 側変更と同じ commit で行う。

**診断の file は 7c では nil のまま** (parser・binder とも)。`ast.Diagnostic.file` は `*ast.SourceFile` で、Store の file を指す形は `ast.Diagnostic` の型変更 (program・LS・出力側が読む) を伴い、7c の範囲ではない。等価テストは 7b と同じく (pos, len, code, message) で比べる。binder が file を渡す 3 関数 (`scanner.GetRangeOfTokenAtPosition`、`GetErrorRangeForNode`、`GetSourceTextOfNodeFromSourceFile`) は text と node しか読まないので `storebinder` に Store 版を置く ((f) に列挙)。program 側で file を付ける経路は 7d の設計項目。

**7b レビューの残り**: `Builder.SetFlags` / `SetLoc` は使い手が無いので 7c の store 変更の中で削る。`reparseTopLevelAwait` の未検証は上記で解消。

## 3. 採らなかった案

| 案 | 理由 |
| --- | --- |
| bind 出力を密な side 列 (`Bound{symbol, flow, locals []uint32}`) に置き、24B header のまま | +12 B/node。書き 0.64 回/node と読みごとに `Ref()` 6 + bounds 3 を払い、parent 連鎖は Pointer の 2 倍のまま |
| bind 出力を全部 予約 slot に置き、24B header のまま | ≈ +5 B/node で最小だが、Identifier の payload 0 word が崩れる (flow の 71% が Identifier)。symbol / flow の読みが kind 表経由 (在 22 inst、不在 11)。parent 連鎖と `Ref()` は据え置き |
| `[]*Symbol` / `[]*FlowNode` の pointer 列 (設計文書 §5 の移行形の原文) | 16 B/node が全部 scan 対象。7b で得た「GC が辿る量 2 桁減」を bind で失う |
| Locals を map 以外にする | 7c では読み手が無い。7d の `Resolve` の実測で決める |
| LocalSymbol を Symbol 側の逆リンクにする | 18k symbol × 4B で、予約 slot (3.5% のノード × 4B) より大きい |
| `ast.Symbol` に Store 用 field を足して併存させる | 96 → 128B。「どちらが真か」が型で示されず、Pointer 側 field が nil の symbol が production 型に混ざる。7c は checker を触らないので 72 file を守る理由が無い |
| ジェネリック `Symbol[N]` | 72 file の instantiation が要り、`store.Symbol` と同じ編集量の上に型引数が残る |
| symbol 同士の参照を `SymbolId` にする | file をまたぐ merge と checker の transient symbol を表せない |
| indicator を bind の先頭で計算する | reparse が Compact 後の木を書き換えることになる (設計文書 §2.7 に反する) |
| indicator の木を歩く 2 経路を `nodes` の線形 pass にする | post-order で最初に見つかるノードが pre-order と違い、等価テストが位置で比べられない。稀な経路で得るものが無い |
| 診断に file を付ける経路を 7c で作る | `ast.Diagnostic.file` の型変更が program・LS・出力側に及ぶ |
| nameresolver を 7c に含める | 呼び手が無い。比較対象の無い合成ベンチを新設することになり、checker の `Resolve` の呼び出し分布を再現できない |
| JSDoc reparse を parse の Store に入れてから 7c | jsdoc.go 1,355 行 + reparser.go 752 行の移植が parser 側の仕事として先に要り、JSDoc の置き場 (設計文書 §5、前回は synth Store で Monaco Bind −20〜29%) を先に決める必要がある。7c のゲートは checker.ts (TS) なので JS は合否に関わらない |
| 7c は TS のみ | binder.go の JS 専用経路 (約 200 行) を移植しながら 1,163 file の検証を捨てる理由が無い |
| flow を pointer 連結の新型にする (`Antecedent *FlowNode`、24B) | 1.34 MB + 密列 2.4 MB。pointer 約 17 万本が scan 対象で、Store は noscan という賭け (設計文書 §1) に反する。7d で index 連結に変えるなら checker の flow ループを 2 回書き換える |
| flow-packed 計画 (12B、`FlowNode` 値型、重複除去) を 7c に含める | 効くのは −0.2 MB 程度でゲートに効かない。index 連結の型の上で後から slab 幅の変更として入れられる |

## 4. 不変条件

設計文書 §2.8 の 6 つに加えて:

7. `NodeHeader` は 32B。`flow` と `symbol` は bind だけが書き、0 が nil。parse は 0 で作る。
8. **bind 完了後は Store に書かない (設計文書 §2.8 の 5) を API で守る。** 書き込み API は `Node.AddFlags` / `ClearFlags` / `SetFlow` / `SetSymbol` と生成された予約 slot の setter だけで、binder が `Store.Seal()` を呼んだ後は `storeChecks` build で panic する。Builder は bind まで生かさない。
9. `Bound` と `Symbol` に保存するノードの手掛かりは `Ref` (file + id) だけ。`Node{s,h}` を `Bound` / `Symbol` / `SymbolTable` に入れない (設計文書 §2.5)。
10. bind の出力型 (`FlowRef`、`FlowNode`、`FlowList`、`SymbolId`、`Symbol`、`SymbolTable`、`Bound`) は `store` package が持ち、`storebinder` が書く。`store` は `storebinder` を import しない。Store 本体 (`nodes` / `extra`) は id の意味を知らず noscan のまま。
11. flow は slab の index で参照する。`FlowRef` 0 = nil、`unreachableFlow` は最初に作る (index 1)。slab は append で動くので `*FlowNode` を bind 中に保持しない。
12. `SymbolId` は file 内で密 (生成順、1 始まり)。`Bound.symbols[id]` が `*Symbol`。
13. binder に `map[NodeRef]T` を置かない。Pointer が `GetNodeId(attributes)` に使っていた 1 箇所 (pattern ambient module の symbol 名 `…pattern@<id>`) は **`FileRef()` (file + id)** を使う。`NodeRef` だけだと別 file の同名 pattern が 7d の globals merge で同じ symbol に merge される (Pointer の id はプロセス一意)。名前が Pointer と一致しなくなるので、等価テストは private 名の `#<id>@` と同じくこの id を正規化する。
14. 走査は生成 `ForEachChild` と Pointer と同じ訪問順。線形 pass は `ExternalModuleIndicator` の statements ループだけ。

## 5. 検証計画

手順と判定は [store-binder-verification-instructions-20260922.md](store-binder-verification-instructions-20260922.md)。要点:

| 項目 | 方法 | 線 |
| --- | --- | --- |
| 等価 (bind) | `storetest.Pairs` で対にした (Pointer, Store) ノードを `storebinder` のテストが比べる: bind 後の flags、symbol (Name / Flags / Declarations の位置列 / ValueDeclaration / Parent / ExportSymbol / Members / Exports を memo 付きで再帰)、LocalSymbol、Locals の key 集合、flow graph (Flags / Node の位置 / antecedents の順序 / SwitchClause と ReduceLabel の中身)、EndFlow / ReturnFlow / Fallthrough、file の field (SymbolCount、GlobalExports、PatternAmbientModules、bind diagnostics、両 indicator、Imports / ModuleAugmentations / AmbientModuleNames / UsesUriStyleNodeCoreModules)。private 名は `#<id>@` を `#@` に正規化 | corpus 17,415 file で mismatch 0。除外は §2.1 の JS 293 file (log)。7b の await 8 file は除外を外す |
| 等価 (parse) | 7b のテストがそのまま通る (32B、reparse 有効、indicator あり) | mismatch 0 |
| Walk KPC (32B) | `BenchmarkStoreWalkKPCV1`、24B の before と比較 | cycles ≤ 1.15 × pointer (ゲート 1)。24B (1.137) より悪化しないこと |
| Parse KPC (32B) | `BenchmarkStoreParseKPCV1`、24B の before と比較 | cycles ≤ 1.00 × pointer (7b の線。S1 後 0.813) |
| Bind KPC | `BenchmarkStoreBindKPCV1` (`BenchmarkASTBindKPCV1` の形、parse は区間外、bind だけ `Measure`)。分母は両方 Pointer の visits 298,054 | **cycles ≤ 1.10 × pointer (決定済み)**。inst / IPC は報告 |
| 命令列 | `Ref()` に乗算が無い、`bind` → `bindChildren` → `ForEachChild` で `node()` が辺 1 本に 1 回、setter と `Symbol()` / `FlowNode()` / `Locals()` の inline、表引きの bounds check なし | 期待どおり。外れたら命令列で理由 |
| retained / GC | `BenchmarkStoreBindRetainedV1` (7b の retained ベンチを bind まで伸ばす。GC 2 回)。入力は 7b と同じ fixtures 166 file に加えて checker.ts 単独と dom 単独 | **提案 (未決)**。比は symbol の密度で動く (symbol 96B と `SymbolTable` は両側に同量乗る): checker.ts (6.2%) は Store ≈ 52.8 + T、Pointer ≈ 105.6 + T (T ≈ 10 は map 等の共通分、Pointer の bind B/op 24.9 B/node から flow 5.7 と symbol 5.9 を引いた残り) で 0.54、dom (23.5%) は 69 + 15 / 118 + 15 で 0.63。fixtures 166 file は dom 系が多い。線: retained は checker.ts ≤ 0.60、fixtures ≤ 0.70、dom は報告。GC ms は Store 側で scan されるのが symbol + map + `symbols` 表 (S ≈ 16〜19 B/node) だけで、Pointer は全部 (≈ 100 + S) なので比 ≈ S / (100 + S) = 0.14〜0.16 (checker.ts)、dom 密度で約 0.25。map は string key + pointer 値で bytes 比より重い。線: checker.ts ≤ 0.25、fixtures ≤ 0.35。B/op と allocs は報告 |
| 移植の機械性 | diff と分類 (a)〜(g)、`internal/binder` / `parser` / `ast` (store 以外) に差分なし、production からの import なし、generator の再現、`-tags storechecks`、`unsafe` は `Ref()` / `Refs()` だけ | 7b と同じ |

before の取り方: 7c の変更前 (この文書の commit) で、ユーザーが別ターミナルで Walk KPC (`internal/ast/store`)、Parse KPC (`internal/storeparser`)、Pointer の Bind KPC (`internal/binder`) を取り、`internal/ast/docs/_kpc-baselines/store-24b-before-<日付>.txt` に保存する。after は同じコマンドを 7c の binary で取る。

## 6. 計測の手順

- 構造の個数 (§2.1 の表): Pointer で fixtures を parse → bind し、全ノードを `ForEachChild` で歩いて `FlowNodeData().FlowNode`、`DeclarationData().Symbol`、`LocalsContainerData().Locals` の有無を数え、ノードに付いた flow (`FlowNode`、`EndFlowNode`、`ReturnFlowNode`、`FallthroughFlowNode`) から antecedent を辿って到達可能な `FlowNode` / `FlowList` を集合で数えた。symbol は node の `Symbol` / `LocalSymbol` / locals から Members / Exports / Parent / ExportSymbol を推移的に集めた。JS corpus は 7b の `corpus` と同じ列挙で `.js` / `.jsx` / `.mjs` / `.cjs` だけを取り、`NodeFlagsReparsed` かつ `Symbol != nil` のノードを数えた。テストは binder package の外部テストとして書き捨て (scratchpad `measure/zz_measure_test.go`、出力 `bind-structure-counts.txt`)。

## 7. TODO

実装の順序 (実装指示書の §8 と同じ):

1. before の KPC 3 本を取る (ユーザー、§5)。
2. `store`: header 32B、`Node` の setter、`Seal`、`Bound` / `Symbol` / flow の型、`GetNodeAtPosition`、`SetFlags` / `SetLoc` の削除。generator: 予約 slot (`bound` class)、setter、`Locals()` 等の役割 accessor。7a / 7b のテストが通る。
3. `storeparser`: `references.go` の移植、indicator、`reparseTopLevelAwait` の起動、`File` の field。7b の等価テストで await 8 file の除外を外して通す。
4. `storebinder`: binder.go の逐語移植 (分類 (a)〜(f))、`storetest.Pairs` と等価テスト。
5. (g) のまとめ pass。diff を取り直す。
6. 検証指示書の全項目。after の KPC 3 本 + Bind KPC (ユーザー)。
7. レポート。設計文書 (この文書と store-ast-design) への反映案。

7c の後に残るもの: JSDoc の置き場と JS 293 file の再検証、nameresolver / referenceresolver の port、診断の file、`SymbolId` の global 化、Locals の map 以外の形、flow の packed 化 (CSR は [flow-antecedents-csr-20260922.md](flow-antecedents-csr-20260922.md))、32B header の空き活用の再検討。いずれも 7d (checker) の設計で扱う。
