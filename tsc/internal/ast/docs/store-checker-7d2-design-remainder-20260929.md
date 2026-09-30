# Store checker 7d-2 の設計の残りと小段の切り方

作成日: 2026-09-29。[7d 設計](store-checker-design-20260923.md) (以下「7d 設計」) §4 の TODO 5 (7d-2 の設計の残り: checker の変換規則の一覧と P2 の差し替えの一覧、Store → Pointer の変換器と EmitContext、合成の Store の既定の容量、(g) を入れるか) を閉じる。前提は 7d-0 (46949d73e7) と 7d-1 (2341effa51) の実装と、7d 設計 §2 の決定 (N1'、E1、F1/Y3、L1、I1、K1、D2、A1/V1、G1、NB1、K3、R、FR1、H1、Q1、Check ゲート) と [レビュー契約](store-checker-review-contracts-20260926.md)。この文書は決定済みの項目を繰り返さない。

2026-09-29 のユーザーの決定: 7d-2 の指示書の前にこの文書で設計の残りを閉じる。7d-2 は検証の単位で 3 つの小段 (2a / 2b / 2c) に分け、指示書 (実装と検証) を小段ごとに書く (§4)。

状態: **2026-09-29 に全部決定 (ユーザー)。** §2 の推奨をそのまま決定にし、§3 の 4 件は §3 のとおり。

## 1. 調査: checker に今の変換器をかけた結果

`tools/cmd/storetranslate` の file 単位の変換 (7d-0 の binder.go 試作と同じ `translateFile`) を、`-overlay` で足した試行モードで `internal/checker` の非テスト 25 file (60,980 行) にかけた。repo は変えていない。規則は 7d-0 のまま、`store` の関数は今ある全部 (7d-1 の utilities を含む) を使える前提。材料と再現手順は `_store-checker-7d2-survey/` (ignore 対象、ローカルのみ)。HEAD 2341effa51、2026-09-29。

### 1.1 変換器が報告した箇所 (1,439)

| 種類 | 箇所 | 異なる名前 | 規則で済むか |
| --- | ---: | ---: | --- |
| まだ `store` に訳していない ast の関数を呼ぶ | 999 | 231 関数 | 済む: U2 の到達計算の根に storechecker を足す (R1) |
| list を `[]*Node` の引数に渡す・返す・代入する | 133 | — | 呼び出し先の 9 割は core / slices の helper (R4) |
| `store` に無い node の method | 111 | 24 | method を足せば恒等 (R2) |
| `SourceFile` の method / field | 96 | 14 | `File` の API を足せば恒等 (R3) |
| node の field への書き込み | 27 | 5 field | 書き込みの規則が要る (R8) |
| `Symbol` の field | 18 | 5 | 変換器の不備 (`core.OrElse` の型引数が Symbol のとき)。規則は要らない |
| node を key にする collection の型 | 21 | 13 LinkStore + Set / OrderedSet | L1 の変換規則 (R5) |
| 比較できる Node を要する generic | 4 | 3 | R6 |
| type switch | 6 | — | node の型の switch ではない (literal の値など)。変換器の不備 |
| その他 (list の `Loc` / `HasTrailingComma`、`ImportClause` などの埋め込み field、method value) | 24 | — | 個別 (R2 / R3 に含める) |

file ごと: checker.go 871、grammarchecks.go 110、nodebuilderimpl.go 68、jsx.go 62、flow.go 60、emitresolver.go 60、utilities.go 56、nodecopy.go 34、services.go 30、relater.go 29、ほか 59。

### 1.2 変換器が報告しない、ほかの package への呼び出し

変換器は ast の型に触れる構文だけを見るので、ほかの package の関数や method にノードを渡す呼び出しは黙って通る (出力がコンパイルできないことで初めて分かる)。go/types で、checker から checker 以外への呼び出しのうち、signature が ast のノード・list・file・symbol・flow を含むものを数えた (`foreign-calls.txt`)。

| 呼び先 | 呼び出し | 種類 | 行き先 |
| --- | ---: | ---: | --- |
| `ast.NodeFactory` | 457 | 102 method | 7d-0 の `store.Factory` に 100 method がある。無いのは `NewModifier`・`DeepCloneNode` (R7) |
| `core.LinkStore` (node / symbol / file の key) | 189 | 38 | node の key は L1 で `NodeKey`、symbol は I1 で `*store.Symbol` (R5) |
| `ast.NodeDefault.AsNode` | 98 | 1 | 規則あり (`store.Node(view)`) |
| `scanner` | 70 | 7 | `DeclarationNameToString` 44、`GetTextOfNode` 16、`GetRangeOfTokenAtPosition` 6 など (R14) |
| `printer.EmitContext` | 53 | 11 | nodebuilder 群だけ。printer ごと変換する (§2.4) |
| `nodebuilder.SymbolTracker` | 45 | 4 | interface の引数にノード (R13) |
| `ast.NodeVisitor` | 36 | 4 | nodecopy (R12) |
| `collections.Set` / `OrderedSet` (node / symbol) | 34 | 11 | R5 |
| `binder` | 14 | 9 | `NameResolver.Resolve` 2 と `ReferenceResolver` 6 (emitresolver だけ) ほか (R15) |
| `printer.Printer` | 7 | 2 | checker/printer.go の印字の口。printer ごと変換する (§2.4) |
| `pseudochecker` / `modulespecifiers` | 9 | 6 | nodebuilder だけ (R13) |
| `parser.ParseIsolatedEntityName` / `astnav` | 2 | 2 | R16、`astnav` は services.go (E3 で落ちる見込み) |

### 1.3 読み取れること

- **7d-0 の規則は checker の大半を覆う。** 1,439 のうち 999 は到達計算を広げれば消え、残りも大半は method や API を足せば恒等の規則で済む。規則にならない書き換えが要るのは書き込み (27) と list を slice として渡す箇所の一部 (§2.1 の R4、R8) に絞られる。133 の内訳は problems.txt では「引数に渡す 124、`[]Node` の変数・field に代入 5、`[]Node` として返す 4、slice する 2」で、後の 11 は R4 では消えない (2026-09-30 のレビューで判明、R4' と §3 の 5)。
- **本当の量は package の境界の外にある。** Factory 457、LinkStore 189、scanner 70、EmitContext 53、SymbolTracker 45、NodeVisitor 36 は、変換器が今は黙って通すもので、変換の対象 package を広げるか、store 側に型を足す必要がある。
- 数えたのは静的な出現で、E3 で刈る前。emitresolver.go (60)、services.go (30)、exports.go (3) の大半は E3 で落ちる見込み (7d 設計 §2.1 の試作: 到達は emitresolver 20 / 64、services 4 / 64、exports 2 / 92 関数)。

## 2. 推奨する決定

### 2.1 checker の変換規則の一覧 (TODO 5 の 1)

7d-0 の規則表 (7d-0 実装指示書 §5.1) に足すもの。どれも go/types の型と store の API から決まる (7d 設計 §2.1 の規則の条件)。

| # | Pointer | Store | 静的な数 | 形 |
| --- | --- | --- | ---: | --- |
| R1 | 訳していない `ast.Xxx(...)` | `store.Xxx(...)` | 999 / 231 関数 | U2 の到達計算の根に storechecker (と R13〜R15 の package) が名指す ast の関数を足す。規則は既存 |
| R2 | `x.ModifierNodes()`・`x.TypeArgumentList()`・`x.ArgumentList()` など node の method | 同名の Store の method | 111 / 24 | list を返すもの (`ModifierNodes`・`TypeArgumentList`・`ArgumentList`・`ElementList`・`TypeParameterList`・`StatementList`・`PropertyList`・`ParameterList`・`Decorators`) は、生成済みの多相 accessor (`Arguments()` などが既に `List` を返す) を呼ぶ同名の method を `store` に手書きで足す。data の interface を返すもの (`FunctionLikeData`・`ClassLikeData`・`BodyData`・`LocalsContainerData`・`DeclarationData`) と `IsTypeOnly`・`Attributes`・`GetResolutionModeOverride`・`CanHaveStatements`・`Comments` は手書きで `store` に足す。`Clone` / `VisitEachChild` は R7 / R12 |
| R3 | `*ast.SourceFile` の method / field (`AsNode`・`Text`・`FileName`・`Path`・`Imports`・`Symbol`・`Locals`・`ExternalModuleIndicator` など) | `*store.File` の同名の method | 96 / 14 | `AsNode()` → `Root()`。`NodeRef` を持つ field (`ExternalModuleIndicator`・`CommonJSModuleIndicator` など) は `f.Store.Node(f.X)` の規則。`Symbol` / `Locals` は `Bound` の解決層。`FileName`・`Path`・`Text`・`Imports` は `File` の公開 field と名前がぶつかり、checker は `*SourceFile` を `ast.HasFileName` として渡すので、field を小文字にして Pointer と同名の method にする (2026-09-29 のレビューで決定、2a 実装指示書 §4.2)。`Imports()` は `[]NodeRef` を返す method にして、呼び出し側は `ModuleAugmentations` と同じ `[]NodeRef` の規則 (要素ごとに `f.Store.Node(r)`) で読む。毎回 `[]Node` を作る案は、呼び出し元 `getAlternativeContainingModules` (symbolaccessibility.go:170-185) が (symbol, file) の cache miss ごとに走るので退けた (2026-09-30 のレビューで訂正。最初は「file ごとの 2 箇所」と書いた) |
| R4 | list を `[]*Node` の引数に | core / slices の helper は `List` 版 (`store.SomeList` など) を実際の型で選ぶ。checker の関数は P2 | 133 | helper は `core.Some` 23、`slices.Index` 17、`core.Map` 9、`core.Every` 8、`core.LastOrNil` 7、`core.Filter` 5 など 13 種、約 90 箇所。7d-0 の `SameMap` などと同じ選び方。checker の関数 (`checkSourceElements` 19、`checkTypeParameters` 4、`reportUnusedVariableDeclarations` 3 など 5 関数) は P2 (§3 の 2) |
| R4' | List が `[]*Node` の変数・field・返り値・slice 式に流れる箇所 (`s.typeArguments = node.TypeArguments()`、`return node.Members()`、`args[:i]`) | `l.Nodes()` (Len 個を `At` で写した `[]Node`、nil List は nil) を挿入 | 11 (代入 5、返却 4、slice 2) | 型で決まる (式の Store の型が `List` で、行き先の型が `[]Node`)。コピーを 1 回払う (案 A、§3 の 5)。`getEffectiveCallArguments` (checker.go:30440) はこれで P2 にならない: default 枝の `args` は `List` のローカルになり、`len` / index / slice / return は `Len` / `At` / `Nodes()` の規則で書ける |
| R5 | `core.LinkStore[*ast.Node, V]`、`collections.Set[*ast.Node]` / `OrderedSet[*ast.Node]` | `LinkStore[store.NodeKey, V]` (L1) と key を `x.Key()` に。Set / OrderedSet は `Node` を読み戻す (`Keys()`・`Values()`) ので `store.NodeSet` / `OrderedNodeSet` (`map[NodeKey]Node` と順序) を手書きで足す | 21 型、LinkStore 189 呼び出し | symbol の key は `*store.Symbol` のまま (I1) |
| R6 | 比較できる Node を要する generic (`slices.Compact`、`core.Coalesce`、checker の `asRecursionId`) | `store.Compact` など SameNode 版。`asRecursionId` は `Key()` を id にする P2 | 4 | Z1 で Node は比較できないので、漏れはコンパイルで落ちる |
| R7 | `*ast.NodeFactory` の field (`c.factory`、`b.f`) と method、`x.Clone(f)` | `*store.Factory` と同名の method、`f.Clone(x)` | 457 | 無い 2 method (`NewModifier`、`DeepCloneNode`) を足す |
| R8 | node の field への書き込み (`x.Parent = y`、`x.Loc = r`、`x.AsXxx().Symbol = s` など) | 持ち主の Factory の writer (`f.SetParent(x, y)`、`f.SetRange(x, …)`、`f.SetNodeSymbol(x, s)` など) | 27 (Parent 14、Loc 10、Symbol・`IsTypeOf`・`TokenFlags` 各 1)、14 関数 | §3 の 1 |
| R9 | checker が作る transient symbol (`core.Arena[ast.Symbol]`、`&ast.Symbol{...}`) | `store.Symbol` (field は同名) | — | D2 / I1 で済み。1.1 の `Symbol` の 18 件は変換器の不備で直す |
| R10 | ast / checker の定義の中の node を含む struct field | N1' (checker の struct は `Node`、key だけの struct は `NodeKey`) | — | 7d-0 の規則。変換器の型の変換で済む |
| R11 | `ast.NewDiagnostic` など ast/diagnostic.go | `store.Diagnostic` (G1) | 20 | ast/diagnostic.go を U2 に含める (R1 と同じ)。`store.File` の parse / bind の診断は `*ast.Diagnostic` のまま (7d 設計 §2.5) なので、それを読む関数値 (checker.go:12733 の `core.Some(sf.Diagnostics(), func(d *ast.Diagnostic) …)`) の引数の型は Store 側の signature から取る (2026-09-29 のレビューで追加、2a 実装指示書 §3.3) |
| R12 | `ast.NodeVisitor` (nodecopy) | `store.NodeVisitor` | 36 | ast/visitor.go (278 行) を file 単位で変換して `store` に出す |
| R13 | `nodebuilder.SymbolTracker`、`pseudochecker`、`modulespecifiers` の node を扱う部分 | 変換 (7d 設計 §2.6 の決定) | 54 | 置き場は §3 の 3 |
| R14 | `scanner` の node を読む関数 (`DeclarationNameToString`、`GetTextOfNode`、`GetErrorRangeForNode`、`GetTokenPosOfNode`、`GetRangeOfTokenAtPosition`、`ScanTokenAtPosition`) | 変換 | 70 | 置き場は §3 の 3 |
| R15 | `binder.NameResolver` と binder の free 関数 | binder/nameresolver.go (506 行) を変換して storebinder に出す。free 関数は storebinder の手書き版 (`FindUseStrictPrologue` は引数が `[]NodeRef` なので呼び出し側の P2 か規則) | 14 | `ReferenceResolver` は emitresolver だけで E3 で落ちる見込み。落ちなければ報告 |
| R16 | `parser.ParseIsolatedEntityName` (jsx.go:1436、`_jsxFactoryEntity`) | `storeparser.ParseIsolatedEntityName`: 文字列を自分の Store に parse し seal して返す (遅延 JSDoc の Store と同じく program の counter の StoreID) | 1 | jsx.go:1444 が結果の `Loc` を書き換えるので、位置を (-1, -1) で作る引数を持たせ、seal 後の書き込みを避ける |
| R17 | `evaluator` (168 行) | `storeevaluator` に変換 (7d 設計 §2.1 の置き場) | — | 変換器が関数値 (`evaluator.Evaluator`) の呼び出しを数えないので 1.2 に出ない |

`ModifierFlags` の bit 16 以降 (7d 設計 §4 の未決): **何もしない。** Go の Pointer は bit 16 以降をどこでも立てない (`ModifierFlagsDeprecated`・JSDoc 系・`HasComputed*` を使うのは api/enum_values_generated.go の enum の書き出しと、ls/lsutil/symbol_display.go:378-424 の LS のローカルな flags 変数 (node の header には書かない) だけ。TS 版の `modifierFlagsCache` は Go には無い)。header の `uint16` で Pointer と同じ値になる。`store.go` の 2 つの TODO をこの事実に書き換える。

### 2.2 到達計算 (E3) の方法

**7d-0 の構文上の到達計算を method に広げる。RTA は使わない。** 根は 7d 設計 §2.7 の 9 つ (`NewChecker`、`GetDiagnostics`、`GetSuggestionDiagnostics`、`GetGlobalDiagnostics`、`GetTypeAtLocation`、`GetSymbolAtLocation`、`TypeToString`、`NewNodeBuilder`、`IsTypeAny`)。

- 関数と method の本体に現れる識別子を `TypesInfo.Uses` で辿る (7d-0 と同じ)。static な method の呼び出しはそのまま `*types.Func` になる。
- interface の method の呼び出し (`SymbolTracker`、`Program`、`FactoryHooks` など) は、変換の対象 package の中でその interface を満たす具体型の同名 method を全部足す (対象 package に閉じた CHA)。
- 関数値は 7d-0 と同じく、名前が本体に現れた時点で到達とする。
- 根拠: 7d-0 で RTA から使っていたのは静的な辺だけだった。checker で要るのは interface の呼び出しだけで、それは型から閉じて求まる。刈りすぎはコンパイルで落ち、残りすぎはコード量が増えるだけ (7d 設計 §2.1)。

### 2.3 P2 の差し替えの一覧 (TODO 5 の 1)

**P2 は関数単位で storechecker の `*_manual.go` に置き、Pointer の関数の hash を記録する (7d-0 と同じ仕組み)。** 今の時点で P2 が要ると分かっているもの:

| 関数 | 理由 |
| --- | --- |
| R4 の checker の 5 関数と `checkTypeParametersNotReferenced` の List 版 | list を slice として受ける。slice の呼び出し元もあるので元の関数は変換で残す (§3 の 2) |
| `asRecursionId` | 比較できる key を型引数に要る (R6) |
| R15 の `FindUseStrictPrologue` の呼び出し元 (grammarchecks.go の 1 関数) | storebinder の版は `[]NodeRef` を受ける |

一覧は 2a の変換器が出す problems で確定する。**自動化率の線は置かず、P2 の関数の数と行数を報告する** (7d-0 の binder 試作の線はユーザーが後回しにした。checker の正しさは 2b の等価で、費用は 2c のゲートで測るので、自動化率は変換の保守の費用の目安にとどまる)。

### 2.4 印字: printer を変換する (NB1'、TODO 5 の 2)

**Store → Pointer の変換器を作らず、printer package を storechecker と同じ変換器で `storeprinter` に変換して、nodebuilder の Store の出力をそのまま印字する (NB1')。** (2026-09-29、ユーザー。7d 設計 §2.6 の NB1 のうち「print の直前に Store → Pointer の変換器で Pointer の木にして printer に渡す」を置き換える。nodebuilder の出力を合成の Store に組む点は NB1 のまま)

- 経緯: 変換器の設計で、nodebuilder が再利用した source のノードを Pointer にするとき、Pointer の printer が親を読むことが問題になった。printer が親から読む箇所 (2026-09-30 のレビューで数え直し。最初は 3 つと書いたが 5 つ): (1) ノードの file (source の text を `text[pos:end]` で切り出す前に、親を辿って SourceFile を求め今の file と比べる。printer.go:241-247、utilities.go:203)、(2) 並んだ 2 つのノードの親の同一性 (`originalNodesHaveSameParent` utilities.go:569-573、行を保つ list の改行の判定)、(3) 親の kind (comment の判定で括弧式か、printer.go:3083)、(4) list の先頭・末尾の子の親が nil か list の持ち主か (printer.go:542 `firstChild.Parent == nil`、612 `lastChild.Parent == nil || lastChild.Parent == parentNode`。`PreserveSourceNewlines` の外側の条件で、checker の設定でも通る)、(5) 兄弟の親の同一性 (utilities.go:429-430、`PreserveSourceNewlines` のときだけ)。親の list を読む `getContainingNodeArray` も `PreserveSourceNewlines` のときだけで、checker の printer は立てない。親への書き込みは printer/factory.go:289 / 297 (clone の親を元の親にする。R8 で receiver の `NodeFactory` の埋め込みが `f`)。ユーザーの指摘: どれも Store が直接答えられる (`File()`、親の `SameNode`、`Parent().Kind()`、`Parent().IsNil()`)。Store の木では再利用された source のノードは FR1 の foreign ref で本物のノードなので、printer を Store で動かせば親も file も本物になり、stub も変換も要らない。
- 7d 設計 §2.1 の「printer を 7d で移植しない限り、message を比べるには nodebuilder の出力が Pointer ノードである必要がある」は条件付きの記述で、printer の変換を退けた決定は無い。
- 量: 試行 (§1 と同じ方法) で printer package 15,165 行 (printer.go 6,345) の problems は 279 (うち ast の関数の未変換 74)。種類は checker と同じ (node の method 68、SourceFile 44、list の field 27、書き込み 24 など)。到達計算の根は `(*Printer).Write` で、これは checker の印字と emit の共通の入口 (`emit` の kind switch が全 kind を名指し、`emitSourceFile` から helper の出力に届き、引数に `*sourcemap.Generator` がある) なので、printer.go の大半は残ると見込む (2026-09-30 のレビューで訂正。最初は「emit 用の helper などは落ちる」と書いた)。落ちるのは `Write` から届かない file (changetrackerwriter.go など) と関数だけで、数は 2a の報告で出す。
- 上の (1)〜(3) は既存の規則で `node.File()`、`store.SameNode(a.Parent(), b.Parent())`、`store.IsParenthesizedExpression(n.Parent())` になる。`IsSibling` のような API は足さない。
- EmitContext は printer/emitcontext.go の到達分を変換して `storeprinter` に入る (key は `NodeKey`、L1 と同じ規則)。
- 得: Store → Pointer の変換器、再利用ノードの親の扱い、印字の一時 Pointer AST が要らない。レビュー契約 §3・§4 の「印字に要る一時 Pointer AST の費用」は無くなり、印字の全区間は nodebuilder + storeprinter + 文字列の取得になる。
- 払うもの: printer の到達分の変換と、その P2。
- 比較の独立性は保たれる: 2b で比べる相手は Pointer の checker と Pointer の printer の出力で、変換器とは独立。
- **2a の単体等価**: corpus の全 file を、Pointer の printer (Pointer の file) と storeprinter (Store の file) で、checker/printer.go の 5 つの printer の設定 (`createPrinterWithRemoveCommentsNeverAsciiEscape` には checker に呼び出し元が無く、使われるのは 4 つ。5 つとも試験する) それぞれで印字し、文字列が一致すること。file の root だけでなく、中の statement・型のノード・entity name と、その合成の複製を source file あり / nil で直接印字する (root を印字すると printer が `currentSourceFile` を設定し、nil の経路を通らないため。形は 2a 実装指示書 §6.3)。checker 無しで printer の変換を確かめられる。

| 採らなかった案 | 理由 |
| --- | --- |
| NB1 の変換器 + file ごとの丸ごと変換 | 再利用ノードの親の連鎖と兄弟を揃えるために file 全体を Pointer にし、checker ごとに cache する。変換と保持の費用が診断の多い入力で増える。再設計案 §4 の「祖先・兄弟を丸ごと変換しない」に反する |
| NB1 の変換器 + 再利用の root に親の stub | printer が stub のほかの field を読む経路が増えると出力が黙って変わる。printer の読む関係の列挙と検査を保守する |

### 2.5 合成の Store の既定の容量 (TODO 5 の 3)

**仮に 4,096 ノードで始め、2c で数えて決め直す。** R (満杯なら次の Store) なので正しさは容量によらない (7d-0 の storage テストで 1〜3 を強制済み)。compiler プロジェクトで checker が作る合成ノードは遅延 JSDoc を除いて約 5,700 (7d 設計 §2.6)。4,096 なら 2 Store で、1 Store の確保は header 32B × 4,096 = 128KB と extra / texts。2c で Q1 の corpus と compiler プロジェクトについて、checker あたりの Store の数・使用率・links の外側の枠の長さを報告する。

### 2.6 (g) (TODO 5 の 4)

**7d-2 では入れない。** 7d 設計 §2.1 の方針 (性能の手段は store の API と変換規則の側に置く) のとおり、Check ゲートは (g) 無しの変換出力で判定する。線を越えたら 7d 設計 §2.7 の切り分けの順で原因を探し、(g) を足すかはそのとき決める。

## 3. 決めたこと (2026-09-29、ユーザー)

1. **node への書き込み (R8): 持ち主の Factory を型で探す。** `x.F = v` → `f.SetF(x, v)` (7d-0 の Factory の writer: `SetParent`・`SetRange`・`SetNodeSymbol`・`SetNodeFlow`・`SetLocals`・`AddFlags`。`IsTypeOf` と `TokenFlags` の setter と、`NodeList` の `Loc` への書き込み (nodecopy.go:886 `res.Loc = core.NewTextRange(-1, -1)`。`store.List` の pos / end は extra の at+1 / at+2 にあり setter が無い) のための `SetListRange(l, pos, end)` は足す)。`f` は、書き込みを含む関数の receiver か引数のうち、struct に `ast.NodeFactory` / `*ast.NodeFactory` 型の field を持つ唯一のもの (`Checker.factory`、`NodeBuilderImpl.f`) の、その field。無いか 2 つ以上なら problem (P2)。14 関数のうち 13 が該当し、残る `markAsSynthetic` は R16 で消える。7d-0 の writer の検査 (source・seal 済み・別の region の Store への書き込みは通常 build で panic) をそのまま使う。
   - **訂正の記録**: 最初に推奨した「合成の Store が自分の Factory を覚え、`Node` に書き込みの method を足す」は、[動作設計](store-checker-execution-design-20260924.md) §1 と 7d-0 実装指示書 §3.6 の決定「Store から Factory / NodeRegion / Checker / hooks への逆参照を置かない (生きている Node が領域内の無関係な Store を生かし続けるのを防ぐ)」に反するので撤回した。また「factory の変数は型から決まらない」としたのも誤りで、持ち主の struct の `NodeFactory` 型の field は型から決まる。
   - 採らなかった案: Store の field (所属 token、sealed、space) だけで書く `Node` の method (「書くのが持ち主の checker か」の検査が消える)、14 関数の P2 (7d 設計 §2.6 の FR3 を退けた理由と同じ)。
2. **list を slice として受ける checker の 5 関数 (R4 の残り): P2 で `store.List` を受ける版を手書き。** (2026-09-29 のレビューで補足: 5 関数は slice を渡す呼び出し元も持つので、元の関数は通常の変換で残し、List 版 `<名前>List` を P2 で足して、呼び出し元は引数の型で選ぶ。`checkTypeParametersNotReferenced` の List 版も要る。形は 2a 実装指示書 §3.4) 採らなかった案: `List.Slice()` で写す規則 (呼ぶたびに確保し、`checkSourceElements` の費用が Check ゲートに乗る)。
3. **ほかの package から変換するものの置き場: Pointer の package 配置を写す。** `storescanner`、`storenodebuilder` (nodebuilder/types.go)、`storepseudochecker`、`storemodulespecifiers` (到達分)、`storeprinter` (printer の到達分、EmitContext を含む。§2.4)、`NodeVisitor` と `Diagnostic` は `store`。採らなかった案: 全部 storechecker に置く (Pointer との対応が崩れる、7d 設計 §2.1 で退けた形)。
5. **List が `[]Node` に流れる 11 箇所 (R4'): 移植は案 A (境界で `Nodes()` のコピー)。** (2026-09-30、ユーザー: まず動かす、次に正確性、その後性能) 最適化は 2c の Check ゲートの結果を見て、案 B (流入する式が全部 List か nil の変数・field・返り値を dataflow で `List` 型にする。変換器に fixpoint の型付けが要り、List を受ける関数の signature が連鎖する) と案 E (resolveCall 系の `args` を Checker の LIFO scratch の `[]Node` に写し、出口で戻す。確保 0 で index は Pointer 並だが `args` の生存範囲は静的に決まらない) を検討する。判断材料として 2c で、call expression あたりの `Nodes()` の確保数と bytes、`getEffectiveCallArguments` の結果が resolveCall の外へ出る経路の有無を報告する。
   - 採らなかった案: C (混在する関数でも `List` にし、slice を組む枝は `c.factory.NewNodeList` で synth Store に作る。Pointer が GC で回収する一時 slice が checker の寿命の間 extra と foreign table に残る)、D (`NodeSeq{list, slice}` の二形態の値型。5〜7 word の値を全関数が運び、index ごとに分岐、`append` と `slices.*` の全部と `[]*ast.Node` を持つ約 59 関数・37 ローカル・9 field を書き換える)。
   - 単価の目安 (未計測): `[]Node` の index は Pointer と同じ (16B の load + bounds check)。`List.At` は len の load・bounds check・ref の load・`node()` の比較と address 計算で 2〜3 倍。走査だけなら差は小さく、同じ列を何度も index する resolveCall 系で効く。A のコピーは 16B × n の確保 1 回で、resolveCall は `reorderCandidates` などで既に確保している。
6. **§2 の推奨を全部決定にする** (到達計算、P2 の一覧を 2a の problems で確定し自動化率は報告だけ、容量 4,096、(g) を入れない、ModifierFlags は何もしない)。§2.4 は最初に推奨した「Store → Pointer の変換器と EmitContext の側表」を、同じ日のユーザーの指摘で printer の変換 (NB1') に置き換えた。

## 4. 小段の切り方 (2026-09-29 決定) と各段の完了条件

コードは 2a で全部変換する。7d 設計 §2.1 で退けた (b') (nodebuilder を stub にして本体と分ける) とは違い、分けるのは検証の単位。

| 段 | 作るもの | 完了条件 |
| --- | --- | --- |
| **2a 変換** | 変換器の拡張 (§2.1 の規則、§2.2 の到達計算、package をまたぐ出力、P2)、storechecker・storeprinter と §3 の 3 の package の生成物、store / storebinder / storeparser への追加 (R2・R3・R5〜R8・R12・R15・R16) | 生成の再現 (再生成して `git status` 不変)。`go build ./...` と `go vet`。7b / 7c / 7d-1 の parse / bind の等価と Walk / Parse / Bind の KPC が線の内側のまま。**nameresolver の単体等価** (corpus の全ての識別子で Pointer と Store の `Resolve` が対応する symbol を返す)。**printer の単体等価** (corpus の全 file を checker の 5 つの printer の設定で印字して文字列が一致、§2.4)。P2 の関数の数と行数の報告 |
| **2b 等価** | H1 のハーネス (`storechecker.Program` の adapter)、Q1 の比較 (生の診断と、型・symbol の walk)、symbol の一対一対応 (レビュー契約 §2)、private の lookup の試験 (同 §1) | compiler / conformance の全テストの全構成で、診断 (位置・code・category・message の連鎖・related information・suggestion) と型の文字列・symbol が一致。除外は理由ごとの機械的な条件で、テストが log する |
| **2c 性能** | Check KPC の driver (H1)、Pointer AST を持たない retained / GC の測定プロセス (レビュー契約 §3)、印字の全区間の計測 (同 §4)、合成の Store の数と使用率、R4' のコピーの確保数と bytes と B / E の判断材料 (§3 の 5) | Check cycles ≤ 1.10 × Pointer、retained ≤ 0.85、mark CPU ≤ 0.75 (7d 設計 §2.7)。印字は線を置かず、全区間と内訳を報告 |

2b と 2c は 2a の生成物に依存し、互いには依存しない。2b の等価が落ちたまま 2c の数字を採らない (違う計算をしている checker の速さは比べられない)。

## 5. 限界

- §1 の数は静的な出現で、E3 で刈る前。1.2 の数え方は signature だけを見るので、interface に包まれた呼び出し (`evaluator.Evaluator` の関数値など) は数えていない。
- 変換器の試行は store の今の API で行った。R2 / R3 の method を足した後で出る problems (足した method の先で変換できない構文) は数えていない。2a の変換器が出す problems が最終の一覧になる。
- R16 の jsx.go:1444 のように、Pointer がほかの package で作ったノードを checker が書き換える箇所は、書き込みの 27 件のうち node の出どころを追ったものだけを挙げた。2a は checker を動かさないので (2a 実装指示書 §0)、writer の実行時の検査で残りが分かるのは 2b で、それまでは静的な分類しか無い。2a の実装報告で 27 件の書き込み先を「持ち主の Factory が作ったノード / source のノード / 追えない」に分類する (2a 実装指示書 §8 の (h))。2026-09-30 のレビューで訂正 (最初は「2a で panic で分かる」と書いた)。読んだ範囲では checker.go:9509 の `errorNode` は `createSyntheticExpression` の結果、printer/factory.go:287-297 は clone で、どちらも Factory 側。
