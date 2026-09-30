# Store checker 変換 (TODO 7d-2a) 実装報告

作成日: 2026-09-30。指示書は [store-checker-7d2a-implementation-instructions-20260929.md](store-checker-7d2a-implementation-instructions-20260929.md) (以下「指示書」)、設計は [store-checker-7d2-design-remainder-20260929.md](store-checker-7d2-design-remainder-20260929.md) (以下「残りの設計」) と [store-checker-design-20260923.md](store-checker-design-20260923.md) (以下「7d 設計」)。before は **2341effa51**。コミットはしていない (作業ツリーの変更のまま)。

## 0. 結論

- 変換器は **problems 0** で完走し、`go build ./...` と vet が通る。生成物は `node tools/scripts/tsc/generate.ts` と `(cd tools && go run ./cmd/storetranslate)` で再現する (再実行の前後で、変更・追加した非テストの file の sha1 が全部一致)。
- テスト (tag なしと `-tags storechecks` の両方、`./internal/ast/store/... ./internal/store...`、変換器のテスト): 失敗は既知の `FuzzEquivalence/a708303f611b95c8` (`ExternalModuleIndicator differs from the Pointer file`) の 1 件だけ。
- parse の等価 17,415 file (dead node 22,793) と bind の等価 17,415 file が mismatch 0、宣言の Store の検査 717,627 件は 7d-1 と同じ。
- **§6.2 nameresolver の単体等価**: 17,415 file、`Resolve` 10,235,856 回 / 側、結果と callback の列の不一致 0。fixture 9 本が通る (§5)。
- **§6.3 printer の単体等価**: 17,415 file × 5 設定 × 11 cell = 55 cell で不一致 0 (1m31s)。nil の fixture と EmitContext の 4 項目が通る (§5)。
- §6.5 の空の Program で `NewChecker` が panic しない (Pointer と並べて確かめた)。
- 報告を書く途中で、生成した `HasFlowNodeData` が `this`・`super`・`true`・`false`・`null`・`import` の 6 kind を落としていたのを見つけて直した (§5.5)。P2 の `getFlowNodeOfNode` と `checkThisBeforeSuper` に効く。照合の試験 `TestHasDataCorpus` を足した (corpus の 2,638,926 ノードで一致)。
- ガード (§1): Pointer 側の package (`ast` の store 以外、checker、printer、binder、parser、scanner、evaluator、nodebuilder、pseudochecker、modulespecifiers) と `tsc/go.mod`・`tools/go.mod` に diff は無い。production から store 系の package への import は無く、`unsafe` も増やしていない。
- **2b の前に決めることが 1 つある** (§8.1): `checkIfExpressionRefinesParameter` を panic する P2 にした (2026-09-30 のユーザーの決定)。これは型注釈の無い、return が 1 つで boolean を返す関数 (`xs.filter(x => x != null)` の形) の全部で呼ばれるので、2b の checker の等価は corpus のほとんどの file でこの panic に当たる見込み。
- 指示書に無い判断をした (§7)。ユーザーに確かめたのは 7 件。確かめずに進めたものもある (§7.2)。storebinder に `referenceresolver.go` を足したこと、`storemodule` を新設したこと、`Has<X>Data` の述語を生成したことなど。

## 1. (a) 変換器の変更

### 1.1 構成

変換器 (`tools/cmd/storetranslate/`) は 7d-0 の binder の試行 (file 単位の変換) を一般化して、表 (`pkgs.go`) の package を 1 回で変換する形に作り直した。

| file | 中身 |
| --- | --- |
| `pkgs.go` | 出力 package の表 (Pointer → Store、mode、file 単位で出す file) と `*_manual.go`・手書き・生成物の file の把握 |
| `world.go` | go/packages での読み込み。**2 段**: 1 段目は出力 package を読まずに Pointer 側だけを読み、到達を計算して仮の出力 (stub) を作る。2 段目は出力 package の生成物を stub に差し替えて読み、関数の本体が Store 側の本当の型 (手書きの API、他の出力 package の signature) を見る。生成物は disk のものを読まない (古い出力や壊れた出力が次に効かない) |
| `reach.go` | 到達計算 (§1.3) |
| `manual.go` | P2 の読み取り、`pointer-hash` の照合、P2 の本体が名指すものを根にする |
| `translate.go`・`calls.go`・`writes.go`・`types.go`・`keys.go` | 規則 (§1.2) |
| `run.go` | 出力 (元の file ごと、元の宣言の順序と comment を保つ)、到達表と R4' の一覧の出力、`STORETRANSLATE_DUMP` で R8 の一覧と problems を書き出す |
| `binder.go` | 7d-0 の binder の試行 (`-binder`)。新しい構成に合わせただけ |

mode は 4 つ: `modeAST` (ast → store。到達した package-level 関数を `utilities.go` に、`visitor.go`・`diagnostic.go` は file 単位で全部)、`modeFull` (checker・printer。型は全部、関数と method は刈る)、`modeFile` (binder の `nameresolver.go`・`referenceresolver.go` と evaluator。file 単位)、`modeChanged` (scanner・nodebuilder・pseudochecker・modulespecifiers・module。到達したもののうち型が変わるものだけ。変わらないものは元の package を呼ぶ)。

### 1.2 規則

§3.3 の各行:

| # | 実装 |
| --- | --- |
| R1 | 到達計算の根に storechecker・storeprinter などの本体が名指す ast の関数を入れる (§1.3)。出力は `store/utilities.go` に 450 関数 (before は 177)。**before から在る 177 関数の本体は byte 一致** (§6) |
| R2 | 呼び出し先の method の名前と Store の `Node` の method の集合で決める (恒等)。足した method は §3.1。`x.XxxData().F` (返り値が data の interface で field F を読む) は `x.F()`。`XxxData()` を単独で使う箇所は problem → P2 (§2) |
| R3 | `*ast.SourceFile` → `*store.File`。`AsNode()` → `Root()`。`NodeRef` の field は `f.Store.Node(f.X)`、`[]NodeRef` の field は range・index・len のときだけ要素ごとに解決し、それ以外 (関数に渡す、append) は problem。`*store.File` を `ast.HasFileName` に渡す箇所は恒等 |
| R4 | `core` / `slices` の helper に list を渡す箇所は、実際の型引数が Node のときに `store` の `<名前>List` を選ぶ (7d-0 の `SameMap` と同じ選び方)。出力 package の関数 F に list を渡す箇所は、P2 に `<F>List` があり他の引数の型が同じならそれを呼び、無ければ problem |
| R4' | 式の Store の型が `List` で、行き先 (代入先・返り値・`append` の展開・slice 式) の型が `[]Node` のときだけ `.Nodes()` を挿入。`getEffectiveCallArguments` は変換の出力のまま (P2 にしない) |
| R5 | `core.LinkStore[*ast.Node, V]` → `core.LinkStore[store.NodeKey, V]` と `Get(n.Key())` など。`collections.Set` / `OrderedSet` の型引数が node → `store.NodeSet` / `OrderedNodeSet`。型引数が comparable を要する generic の型引数が node なら `NodeKey` (L1) |
| R6 | `slices.Compact` と `core.Coalesce` を `store` の `SameNode` 版に。`asRecursionId` は P2 |
| R7 | `ast.NodeFactory` (値・pointer・埋め込み) → `*store.Factory`。`&x.factory` → `x.factory`。`x.Clone(f)` → `f.Clone(x)`。`ast.NodeFactory{}` の literal は problem (P2) |
| R8 | §4 |
| R9 | 変換器の不備として直した (Symbol の field の型の対応、node でない type switch)。規則ではない |
| R10 | 7d-0 の規則のまま |
| R11 | 関数値の引数の型を、Pointer の型を写さず、**呼び出し先の Store 側の signature を実際の引数で instantiate したもの**から取る (`calls.go` の `unify`)。checker.go:12733 の `core.Some(sf.Diagnostics(), func(d *ast.Diagnostic) …)` は `*ast.Diagnostic` のまま (テストで固定) |
| R12 | `ast/visitor.go` を file 単位で `store/visitor.go` に。`VisitSlice` の List 版と `NewNodeVisitor` は P2 |
| R13〜R14、R17 | 表 (`pkgs.go`) の出力 package の同名。出力に無ければ元の package を呼び、型が変わるのに行き先に無ければ problem |
| R15 | `binder.NameResolver` → `storebinder.NameResolver` (変換)。`FindUseStrictPrologue` の呼び出し元は P2 |
| R16 | `parseIsolatedEntityName` の P2 から `storeparser.ParseIsolatedEntityName(c.program.IDSpace(), name)` |
| printer の型 | `storeprinter` の同名 (変換) |

足した規則 (§3.3 に行の無いもの。どれも go/types の型と store の API で決まる):

| 規則 | 理由 |
| --- | --- |
| node の struct (`*ast.Identifier` など) と base の pointer (`*ast.NodeBase` など) の型 → `store.Node`。view の無い `AsXxx()` は恒等 | Pointer は typed node の pointer を値として持ち回る。Store は `Node` だけ |
| `&data.Node` → `data` の値 | typed node から `*ast.Node` を取る慣用句 |
| node を key にする map を key ごと range で読み戻す箇所は problem | `map[NodeKey]` では key から `Node` に戻れない。2 件 (§2 の `checkKindsOfPropertyMemberOverrides`・`checkUnusedLocalsAndParameters`) |
| node の値の `switch` (tag が node) → `case` ごとに `SameNode` の `if` の列 | Z1 (Node は比較できない) |
| node の型の type switch → view の述語の列。node でない型の type switch はそのまま | R9 の不備の修正を規則にした |
| 同名で形の同じ store の struct / 型に写す (`sameShape`: field の名前と型が変換後に一致) | `FlowSwitchClauseData`・`NodeVisitor`・`PatternAmbientModule` など。名前だけでは写さない (`ast.SymbolId` と `store.SymbolId` は形が違うので写さない) |
| `ast.NodeId` の整数への変換 → `n.Key().Uint64()`、literal の `0` の NodeId → `store.NodeKey{}` | nodeLinkStore の key を詰める (ユーザーの決定、§7.1) |
| 結果の型が基本型どうしで違う呼び出しの変換 (`GetSymbolId` の `uint64` → `ast.SymbolId`) | Store の API が基本型で返す箇所 |
| `flow.Node.M()` → `flow.M()` | 7d-0 の `Flow` の API は flow に直接 method を持つ |
| node を持つ ast の型 (`AllAccessorDeclarations` など) を `store` に出す | 到達した ast の utility の返り値 |
| `modeChanged` の package で、出力する関数が元の package の非公開の識別子を使うなら problem | 別 package から読めない。`scanner` の 3 関数を P2 にした |
| P2 と手書きの file が名指すものを到達の根にする | P2 が呼ぶ関数が刈られないように |

変換器が名前で決める箇所 (検証 §2.1 の点検のため): R8 の field → setter の表 (`writes.go` の `nodeWriters`、7 項目。指示書の R8 の行の対応をそのまま表にした)、7d-0 から在る `ruleRewritten` (`GetSourceFileOfNode`・`GetNodeId`)、ast の核の型の名前 (`Node`・`NodeList`・`SourceFile`・`Symbol`・`FlowNode`・`NodeFactory` など)、恒等の対応の method (`AsNode`・`Clone`・`Pos`・`End`・`Loc`・`Nodes`・`AsNodeFactory`)、`slices.Contains`・`Index`・`Compact` (R4 / R6)、`collections` の集合の型の名前 (`keys.go`)。関数名・変数名の表 (特定の checker の関数を名指すもの) は無い。

### 1.3 到達計算

根は §3.2 のとおり (`checkerRoots`: `NewChecker`、`GetDiagnostics`、`GetSuggestionDiagnostics`、`GetGlobalDiagnostics`、`GetTypeAtLocation`、`GetSymbolAtLocation`、`TypeToString`、`NewNodeBuilder`、`IsTypeAny`、`printer.NewPrinter`、`(*printer.Printer).Write`、`(*binder.NameResolver).Resolve`)、出力 package の `init`、storebinder / storeparser が呼ぶ ast の関数 (7d-0 の roots)、P2 と手書きの file が名指すもの。interface は §3.2 の CHA (表の package の中の named type について、T か *T が満たせば method を全部到達)。

到達の表 (関数と method。「変換」は変換で出した数、「P2」は Pointer の宣言を置き換えた P2 の数。List 版など新しい名前の P2 は含まない):

| package / file | 変換 | P2 | 全体 |
| --- | ---: | ---: | ---: |
| **ast** | **521** | **16** | **2,197** |
| ast/ast.go | 7 | 3 | 307 |
| ast/ast_generated.go | 139 | 0 | 1,386 |
| ast/diagnostic.go | 54 | 0 | 54 |
| ast/functionflags.go | 0 | 1 | 1 |
| ast/precedence.go | 4 | 0 | 6 |
| ast/symbol.go | 1 | 0 | 7 |
| ast/utilities.go | 299 | 11 | 418 |
| ast/visitor.go | 17 | 1 | 18 |
| **binder** | **28** | **0** | **28** |
| binder/nameresolver.go | 13 | 0 | 13 |
| binder/referenceresolver.go | 15 | 0 | 15 |
| **checker** | **2,534** | **25** | **2,889** |
| checker/checker.go | 1,481 | 9 | 1,509 |
| checker/emitresolver.go | 27 | 0 | 64 |
| checker/exports.go | 2 | 0 | 92 |
| checker/flow.go | 129 | 1 | 130 |
| checker/grammarchecks.go | 72 | 6 | 78 |
| checker/inference.go | 76 | 1 | 77 |
| checker/jsdoc.go | 2 | 0 | 2 |
| checker/jsx.go | 57 | 1 | 59 |
| checker/links.go | 6 | 0 | 6 |
| checker/mapper.go | 37 | 0 | 37 |
| checker/nodebuilder.go | 14 | 1 | 29 |
| checker/nodebuilder_hover.go | 1 | 0 | 18 |
| checker/nodebuilderimpl.go | 111 | 0 | 114 |
| checker/nodebuilderscopes.go | 3 | 1 | 4 |
| checker/nodecopy.go | 27 | 0 | 28 |
| checker/printer.go | 18 | 0 | 27 |
| checker/pseudotypenodebuilder.go | 9 | 0 | 9 |
| checker/relater.go | 188 | 1 | 189 |
| checker/services.go | 3 | 0 | 66 |
| checker/symbolaccessibility.go | 36 | 0 | 37 |
| checker/symboltracker.go | 14 | 0 | 14 |
| checker/tracer.go | 33 | 4 | 38 |
| checker/types.go | 63 | 0 | 112 |
| checker/utilities.go | 125 | 0 | 150 |
| **evaluator** (evaluator.go) | **5** | **0** | **5** |
| **module** (util.go) | **0** | **1** | **10** |
| **modulespecifiers** | **37** | **0** | **52** |
| modulespecifiers/preferences.go | 7 | 0 | 7 |
| modulespecifiers/specifiers.go | 19 | 0 | 24 |
| modulespecifiers/util.go | 11 | 0 | 21 |
| **printer** | **591** | **11** | **766** |
| printer/changetrackerwriter.go | 27 | 0 | 36 |
| printer/emitcontext.go | 30 | 2 | 87 |
| printer/factory.go | 2 | 1 | 91 |
| printer/generatedidentifierflags.go | 5 | 0 | 9 |
| printer/helpers.go | 1 | 0 | 1 |
| printer/namegenerator.go | 23 | 2 | 25 |
| printer/printer.go | 370 | 4 | 378 |
| printer/semicolon_writer.go | 28 | 0 | 28 |
| printer/singlelinestringwriter.go | 27 | 0 | 27 |
| printer/textwriter.go | 32 | 0 | 33 |
| printer/utilities.go | 46 | 2 | 51 |
| **pseudochecker** | **70** | **1** | **72** |
| pseudochecker/checker.go | 1 | 0 | 1 |
| pseudochecker/lookup.go | 34 | 1 | 36 |
| pseudochecker/type.go | 35 | 0 | 35 |
| **scanner** | **18** | **3** | **124** |
| scanner/scanner.go | 10 | 3 | 112 |
| scanner/utilities.go | 8 | 0 | 12 |

- checker.go はほぼ全部 (1,490 / 1,509)。services.go (3 / 66)・exports.go (2 / 92)・emitresolver.go (27 / 64) は大半が落ちた。残りの設計の見込みどおり。
- printer.go は 374 / 378 が残る (`Write` が emit と共通の入口)。`changetrackerwriter.go` の 27 は、`EmitTextWriter` の interface を満たす型として CHA で入った (§3.2 の interface の規則)。
- ast の `ast_generated.go` 139 は `IsXxx` などの述語で、`store/utilities.go` に出る。

### 1.4 problems の推移

試行 (残りの設計 §1.1) は checker 1,439 + printer 279。本実装の変換器の数え方は同じ (構文の箇所ごと) だが、最初の全体の実行の時点で R1 の到達計算 (試行の 999 件と printer の 74 件) が入っていたので、そこから始まる。

| 時点 | problems | その間にしたこと |
| --- | ---: | --- |
| 試行 | 1,439 + 279 | — |
| 最初の全体の実行 | 781 | R1 (到達計算の根と U2)。残りの内訳は試行と同じ形 (store に無い node の method、SourceFile、list を `[]*Node` に渡す箇所、書き込み、collection の型) |
| | 728 | 出力の整形の不備を直した |
| | 432 | store の手書きの API (§3) を足した: R2 の method、`List` の method、`File` の method、`NodeSet`、Factory の追加 |
| | 304 | node の data・base の struct の規則、`field()` の書き直し (R3 の `NodeRef` の field、R2 の `XxxData().F`) |
| | 208 | R4 (List 版の選択)、R11 (Store 側の signature から関数値の引数の型)、R4'、R8 |
| | 96 | node の switch と type switch、`List` の helper |
| | 2 | 同名の store の struct の写し、flow の method の規則、generator の変更 (VisitEachChild・SyntheticExpression)、node を持つ ast の型の出力、`Has<X>Data`、P2 (§2) を書き、`STORETRANSLATE_FORCE` で出力をコンパイルしてコンパイルエラーを 0 にした |
| 最終 | 0 | `asRecursionId` を P2 にした |

**種類ごとの途中の数は記録していない** (各時点の合計だけ残した)。96 から 2 の間は problems ではなく出力のコンパイルエラーで進めた (storechecker は 164 件から) ので、その間の推移も数としては無い。最終の 0 は種類によらず 0。

## 2. (b) P2 と R4'

### 2.1 出力 package ごとの数

「宣言の行数」は P2 の宣言 (doc comment を除く) の行数、「file の行数」は `*_manual.go` 全体。

| 出力 package | 変換した関数 | P2 (Pointer を置き換え) | P2 (新しい名前: List 版・型) | 宣言の行数 | file の行数 |
| --- | ---: | ---: | ---: | ---: | ---: |
| store | 521 | 16 (うち 7d-1 以前から 11) | 4 | 270 | 408 |
| storechecker | 2,534 | 25 | 11 List 版 + 型 `Program` 1 | 1,359 | 1,667 |
| storeprinter | 591 | 11 | 0 | 232 | 323 |
| storebinder | 28 | 0 | 0 | 0 | 0 |
| storeevaluator | 5 | 0 | 0 | 0 | 0 |
| storescanner | 18 | 3 | 0 | 16 | 45 |
| storepseudochecker | 70 | 1 | 1 List 版 | 22 | 39 |
| storemodulespecifiers | 37 | 0 | 0 | 0 | 0 |
| storenodebuilder | (型だけ) | 0 | 0 | 0 | 0 |
| storemodule | 0 | 1 | 0 | 54 | 73 |

### 2.2 一覧

理由の「4.1」は、Pointer が `XxxData()` を単独で使う (nil 比較・引数・代入) 箇所。指示書 §4.1 は「problem にして報告、method は足さない」なので P2 にし、本体では生成した `Has<X>Data()` (§3.3) と多相 accessor で書いた。規則にしなかったのは、`XxxData() != nil` を `HasXxxData()` に写す規則は作れるが、代入・引数に渡す形は写せず、指示書が method を足さないと決めているため。

**store** (`utilities_manual.go`、`visitor_manual.go`)

| P2 | 行 | 理由 |
| --- | ---: | --- |
| `IsDeclarationNode` | 3 | 4.1 (`DeclarationData() != nil`) |
| `GetFunctionFlags` | 24 | 4.1 (`BodyData()` の member) |
| `IsUnterminatedLiteral` | 4 | `LiteralLikeNodeBase.TokenFlags` と `TemplateFlags` を持つ view だけで読む (Pointer の base の field は Store に無い) |
| `GetAllAccessorDeclarationsList` | 21 | List 版 (checker が list を渡す) |
| `GetReparsedNodeForNode` | 19 | reparse の clone は file の Store の `NodeRef` で、位置で探す |
| `IndexOfNodeList` | 7 | List 版 |
| `GetSemanticJsxChildrenList` | 12 | List 版 |
| `NewNodeVisitor` | 6 | Store の Factory に zero 値が無い (§5.1)。nil で panic |
| `VisitSliceList` | 47 | List 版 (`VisitNodes`・`VisitModifiers` が list を渡す) |
| 7d-1 以前から (変更なし) | 106 | `GetLocals`、`GetNodeAtPosition`、`SetParseJSDocForNode`、`GetSymbolId`、`IsLocalsContainer`、`IsExternalModule`、`IsExternalOrCommonJSModule`、`GetExternalModuleName`、`NodeHasName`、`IsPotentiallyExecutableNode`、`IsAsyncFunction` |

**storechecker**

| P2 | 行 | 理由 |
| --- | ---: | --- |
| List 版 11: `checkSourceElementsList` 9、`checkTypeParametersList` 19、`checkTypeParametersNotReferencedList` 17、`reportUnusedVariableDeclarationsList` 13、`getEntityNameForDecoratorMetadataFromTypeListList` 32、`getSpreadArgumentIndexList` 3、`appendTypeParametersList` 7、`getConstructorsForTypeArgumentsList` 6、`getInstantiatedConstructorsForTypeArgumentsList` 10、`checkTypeArgumentsList` 33、`hasCorrectTypeArgumentArityList` 7 | 156 | §3.4 の List 版。指示書の 6 (5 + `checkTypeParametersNotReferenced`) に、変換器が「list を `[]*Node` の引数に渡す」と報告した 5 (`appendTypeParameters`・`getConstructorsForTypeArguments`・`getInstantiatedConstructorsForTypeArguments`・`checkTypeArguments`・`hasCorrectTypeArgumentArity`) を足した。どれも list と slice の両方の呼び出し元を持つ。下書きは `-listdraft` で出し、List 版どうしの呼び出しだけ手で List 版に直した |
| `initializeChecker` | 94 | file の `ModuleAugmentations` (`[]NodeRef`) を list の list に入れる。R3 は range と index でしか解決しない |
| `checkAllCodePathsInNonVoidFunctionReturnOrThrow` | 51 | 4.1 |
| `checkTypeParameterListsIdentical` | 21 | method value `(*ast.Node).TypeParameters` を関数値として渡す。Store の版は `List` を返すので `Nodes()` で包む closure にした |
| `checkThisBeforeSuper` | 11 | 4.1 |
| `getSymbolOfNode` | 6 | 4.1 |
| `checkIfExpressionRefinesParameter` | 3 | **panic する** (§8.1)。自前の flow node を作るが、Store の flow は source の Bound の行で、seal され checker 間で共有される。ユーザーの決定で 2a の範囲外 |
| `checkKindsOfPropertyMemberOverrides` | 159 | node を key にする map の key を読み戻す。同じ key の 2 つ目の map に node を持つ |
| `checkUnusedLocalsAndParameters` | 43 | 同上 |
| 型 `Program` | 26 | §3.4 (`IDSpace()` を足す)。`GetResolvedModule` などは `*module.ResolvedModule` のまま (§7.2) |
| `NewChecker` | 218 | §5.1 (region と Factory の 2 行) |
| `getFlowNodeOfNode` (flow.go) | 6 | 4.1 |
| `checkGrammarForUseStrictSimpleParameterList` | 35 | §3.4 (storebinder の `[]NodeRef` 版の `FindUseStrictPrologue`) |
| `checkGrammarFunctionLikeDeclaration` | 7 | 4.1 |
| `checkGrammarClassDeclarationHeritageClauses` | 42 | 4.1 |
| `checkGrammarForGenerator` | 15 | 4.1 |
| `checkGrammarObjectLiteralExpression` | 141 | 4.1 (Pointer の単独の `ClassLikeData()` は効果の無い呼び出し)。R4' の grammarchecks.go:1003 もここ |
| `checkGrammarAccessor` | 50 | 4.1 |
| `createReverseMappedType` | 56 | `core.SameMap` を `TupleElementInfo` (node を持つ struct) に使う。Store では比較できない (Z1) ので、ループを書き出して `SameNode` で比べる |
| `parseIsolatedEntityName` (jsx.go) | 3 | §3.4 (R16) |
| `getNodeBuilderEx` | 4 | §3.4 (§5.1) |
| `enterNewScope` (nodebuilderscopes.go) | 194 | 4.1 (`LocalsContainerData()` を単独で使う) と、偽の scope に `Locals`・`Parent` を書く (R8、`b.f`) |
| `asRecursionId` | 6 | §3.4 (R6) |
| `tracedTypeAdapter` の `Symbol`・`AliasSymbol`・`ReferenceNode`・`Pattern` | 12 | `tracing.TracedType` は Pointer の node と symbol を受ける。nil を返す (ユーザーの決定) |

**storeprinter**

| P2 | 行 | 理由 |
| --- | ---: | --- |
| `NewNodeFactory`・`NewEmitContext` | 6 | §3.4 (region が無い。panic)。region を受ける版は `region.go` |
| `(*EmitContext).onCreate` | 3 | R8: `EmitContext` は `*NodeFactory` を持ち、その中に Factory を埋め込む。R8 の探し方 (receiver か引数の struct の `NodeFactory` 型の field) に当たらない |
| `emitConciseBody`・`emitPropertyAccessExpression`・`parenthesizeExpressionForNoAsi` | 160 | R8: 作ったノードの位置を書く。`Printer` は `NodeFactory` 型の field を持たない (`emitContext.Factory` 経由) |
| `emitSignature` | 13 | 4.1 |
| `nextContainer`・`isUniqueLocalName` (namegenerator.go) | 20 | 4.1 (`LocalsContainerData()`) |
| `tryGetEnd`・`greatestEnd` | 30 | `any` を受ける形に。Store の `Node`・`List` の `End()` は `int32`、`core.TextRange` は `int` で、1 つの interface で受けられない |

**そのほか**: storescanner の `GetScannerForSourceFile`・`ScanTokenAtPosition`・`GetRangeOfTokenAtPosition` (Pointer は `scanner.Scanner` の非公開 field を書く。公開の API で同じ field を書く)、storepseudochecker の `createReturnFromSignature` (4.1) と `lastRequiredParamIndexList` (List 版、`slices.Backward` を `Len()-1` からのループに)、storemodule の `GetResolutionDiagnostic` (`*module.ResolvedModule` を受ける。§7.2)。

### 2.3 R4' (`Nodes()` の挿入)

変換器の出力 (`run.go` が印字) と P2 の中:

| 箇所 (Pointer) | 形 | 指示書の 11 |
| --- | --- | --- |
| checker.go:9034 | 代入 | ✓ |
| checker.go:9718 | 代入 | ✓ |
| grammarchecks.go:1003 | 代入 (P2 の `checkGrammarObjectLiteralExpression` の中、手で) | ✓ |
| jsx.go:810 / 814 | 代入 | ✓ ✓ |
| checker.go:30502 | 返却 | ✓ |
| utilities.go:1143 / 1145 | 返却 | ✓ ✓ |
| jsdoc.go:95 | 返却 | ✓ |
| checker.go:30473 | slice | ✓ |
| flow.go:1215 | slice | ✓ |
| ast/utilities.go:1753 | 返却 | (checker の外) |
| ast/visitor.go:157 | `append` の展開 | (checker の外) |
| ast/visitor.go:264 | 代入 | (checker の外) |
| printer/printer.go:1425 | slice | (checker の外) |
| printer/printer.go:4790 | slice | (checker の外) |
| checker.go の `checkTypeParameterListsIdentical` (P2) | method value を包む closure の返却 | (P2、§2.2) |
| ast/visitor.go の `VisitSlice` (P2 の `VisitSliceList`) | slice・`append` の展開・返却 (5 箇所) | (P2) |

checker の 11 は指示書と一致した。増えた分は、指示書の 11 が checker の試行だけから数えたもので、ast と printer は数えていなかったため。`getEffectiveCallArguments` は変換の出力 (checker.go:30473 / 30502 はその中)。

## 3. (c) 手書きで足した API

§4 にあるもの:

- **§4.1** `Node` の method (`checker_api.go`): `ModifierNodes`・`TypeArgumentList`・`ArgumentList`・`ElementList`・`TypeParameterList`・`StatementList`・`PropertyList`・`ParameterList`・`MemberList`・`Decorators` (`List` を返す)、`Comments`、`IsTypeOnly`・`Attributes`・`CanHaveStatements`・`GetResolutionModeOverride`。flow の data は `FlowSwitchClauseData`・`FlowReduceLabelData` の struct と `Flow.AsFlowSwitchClauseData`・`AsFlowReduceLabelData` (7d-0 の `Flow` の API で表せたので、同名 struct の写しの規則に乗せた)。`FlowNodeData` は R8 / 4.1 の形で消えるので足していない。
- **§4.2** `File`: field の移行 (`fileName`・`path`・`text`・`imports`) と `NewFile(fileName, path, text)`・`FileName()`・`Path()`・`Text()`・`Imports() []Node` (§7.1)・`ImportRefs() []NodeRef`・`AppendImport(ref)`、`var _ ast.HasFileName = (*File)(nil)`。ほかに `Statements`・`Locals`・`GlobalExports`・`PatternAmbientModules`・`CommonJSModuleIndicator`・`ForEachChild`・`ECMALineMap`・`OriginalText`・`SpanMap`・`HasIdentifier`・`IsJS`。試行の SourceFile の 14 種は全部、この節か R3 の規則で消えた。
- **§4.3** `List` の helper (`node_helpers.go`): `SomeList`・`EveryList`・`FindList`・`FindIndexList`・`FindLastIndexList`・`MapList`・`FilterList`・`FirstOrNilList`・`LastOrNilList`・`ElementOrNilList`・`IndexList`・`ContainsList`・`BinarySearchFuncList`。`List.Nodes()`・`Loc()`・`HasTrailingComma()`。`NodeSet`・`OrderedNodeSet`。`Compact`・`Coalesce` (`SameNode` 版)。Factory の `Clone`・`NewModifier`・`Region`・`SetIsTypeOf`・`SetTokenFlags`・`SetListRange`・`NewCommentRange`、`DeepCloneNode` (`deepclone.go`)。`store.go` の `ModifierFlags` の TODO 2 つを書き換えた。
- **§4.4** `storeparser.ParseIsolatedEntityName(space, text)` (`isolated.go`) と、そのための `Builder.FinishSynthetic` (全ノードを (-1, -1) にして seal、`File()` は nil)。

§4 に無いもので足したもの:

| API | 理由 |
| --- | --- |
| `Has<X>Data()` (生成、`visit_generated.go` の末尾。`Declaration`・`Exportable`・`LocalsContainer`・`FunctionLike`・`ClassLike`・`Body`・`FlowNode`・`LiteralLike`・`TemplateLiteralLike`) | 4.1 の P2 の本体で `XxxData() != nil` を書くため。schema の base から kind の集合を生成する。`storebinder/hasdata_test.go` (`TestHasDataCorpus`) が corpus の全ノード (JSDoc を含む) で Pointer の `XxxData() != nil` と照合する (§5.5) |
| `VisitEachChild` (生成、`visit_generated.go`) と `visit.go` (JSDoc の parameter / property tag と raw list の手書き) | `ast/visitor.go` の変換が名指す。ユーザーの決定 (§7.1) |
| `Factory` の私有表 `anys` (`SyntheticExpression.Type`) | ユーザーの決定 (§7.1)。slot の class `any` を生成器に足した |
| `NextContainer`・`KindString`・`NodeKey.Uint64` | それぞれ namegenerator の P2、`Kind.String()` の写し、NodeId の整数への変換の規則 |
| `IsDeclarationNode` ほか utility の P2 (§2.2) | 4.1 と List 版 |
| `storechecker/region.go` (`synthStoreCapacity = 4096`)、`storeprinter/region.go` (`NewEmitContextIn`・`NewNodeFactoryIn`) | §5.1 (指示書どおり。file を分けただけ) |

## 4. (h) R8 の書き込み

変換の出力に 35 件、P2 の中に 7 件。残りの設計の 27 件 (Parent 14、Loc 10、Symbol・IsTypeOf・TokenFlags 各 1) は全部この中にある (Parent の 1 件は P2 の `enterNewScope`)。27 件に入っていないのは flow の 5 件 (`FlowNodeData().FlowNode`、R8 の行の「flow と locals の書き込みも同じ形」)、list の要素 3 件、ast/visitor.go の 2 件、P2 の `Locals` 1 件と printer の 4 件。

| 箇所 (Pointer) | 書き込み | `f` | 書き込み先の出どころ | 分類 |
| --- | --- | --- | --- | --- |
| ast/visitor.go:101 / 123 | `list.Loc` → `SetListRange` | `v.Factory` | 直前の `v.Factory.NewNodeList` / `NewModifierList` | Factory |
| checker.go:18190 / 18194 / 18197 | `literal.Loc`・`lhsExpr.Loc`・`result.Loc` | `c.factory` | 同じ関数で `c.factory.New…` | Factory |
| checker.go:18198 / 18199 / 18201 | `.Parent` | `c.factory` | 同上 | Factory |
| checker.go:18203 | `result.FlowNodeData().FlowNode` → `SetNodeFlow` | `c.factory` | 同上 (flow は source の Bound の行) | Factory |
| checker.go:30516 / 30517 | `result.Loc`・`result.Parent` | `c.factory` | `c.factory.NewSyntheticExpression` | Factory |
| checker.go:5053〜5055、5072〜5074 | `reference.Expression().Parent`・`reference.Parent`・`FlowNode` | `c.factory` | 同じ関数で `c.factory.NewPropertyAccessExpression` / `NewElementAccessExpression` | Factory |
| checker.go:9509 | `errorNode.Loc` | `c.factory` | `createSyntheticExpression` の結果 | Factory |
| flow.go:2474〜2476、2497〜2499 | `reference.…` (同上) | `c.factory` | 同じ関数で `c.factory.New…` | Factory |
| jsx.go:847 / 848 | `ValueDeclaration.Parent`・`.Symbol` → `SetNodeSymbol` | `c.factory` | 直前の `c.factory.NewPropertySignatureDeclaration` | Factory |
| nodebuilderimpl.go:1441 | `range_.Loc` | `b.f` | 直前の `range_.Clone(b.f)` | Factory |
| nodebuilderimpl.go:1460 / 1463 | `range_.Loc` | `b.f` | clone か、clone しなかった場合 (位置が合成・`NodeFlagsSynthesized`・同じ file)。`NodeFlagsSynthesized` は EmitContext の Factory の `onCreate` だけが付けるので、region の Factory のノード。`ParseIsolatedEntityName` の結果は file を持たず `enclosingFile` と一致しないので clone される | Factory (条件から) |
| nodebuilderimpl.go:2196 | `sig.Loc` | `b.f` | 直前の `b.f.NewPropertySignatureDeclaration` | Factory |
| nodebuilderimpl.go:3104 / 3113 / 3115 | `tupleConstituentNodes.Nodes[i]` → list の要素の writer | `b.f` | `b.mapToTypeNodes` の結果 (`b.f.NewNodeList`) | Factory |
| nodebuilderimpl.go:3369 | `parentName.AsImportTypeNode().IsTypeOf` → `SetIsTypeOf` | `b.f` | `b.symbolToTypeNode` の結果。Pointer の comment が「freshly manufactured」とする。呼び出し先を静的には全部追っていない | Factory (comment と経路から。静的な証明ではない) |
| nodecopy.go:818 | `c.AsStringLiteral().TokenFlags ^=` → `SetTokenFlags` | `b.f` | 直前の `node.Clone(b.f)` | Factory |
| nodecopy.go:886 | `res.Loc` → `SetListRange` | `b.f` | `nodes.Clone(b.f)` か、`VisitNodes` が作った新しい list (visitor の Factory は `b.f`、nodecopy.go:313) | Factory |
| P2 `enterNewScope` | `Locals`・`Parent` | `b.f` | 直前の `b.f.NewBlock` | Factory |
| P2 printer の 3 関数 | `SetRange` | `p.emitContext.Factory` | 同じ関数で作った括弧・token | Factory |
| P2 `onCreate` | `AddFlags` | `c.Factory` | 作った直後のノード | Factory |

**source のノードへの書き込み: 0 件。追えないもの: 0 件** (3369 は comment と呼び出しの経路による)。writer の所有者の検査は region 単位 (`s.owner != f.region.token || s.sealed`) なので、checker の Factory と nodebuilder の EmitContext の Factory が互いのノードに書いても通る (同じ region)。

## 5. (d)(e) 生成物の大きさとテストの結果

### 5.1 (d) 生成物

| package | 変換器の出力 | 行 | 生成器 (generate-go-store.ts) の出力 | 手書き (P2 以外) |
| --- | ---: | ---: | --- | --- |
| store | 3 file (`utilities.go`・`visitor.go`・`diagnostic.go`) | 4,510 | `visit_generated.go` 1,095 行 (新規)、既存 5 file の SyntheticExpression の変更 | `checker_api.go` 517、`deepclone.go` 76、`visit.go` 34、`file.go` +56、`node_helpers.go` +129、`factory.go` +28 |
| storechecker | 24 file | 54,827 | — | `region.go` 6 |
| storeprinter | 16 file | 8,535 | — | `region.go` 31 |
| storebinder | 2 file (`nameresolver.go`・`referenceresolver.go`) | 776 | — | — |
| storeevaluator | 1 | 175 | — | — |
| storescanner | 2 | 495 | — | — |
| storenodebuilder | 1 | 26 | — | — |
| storepseudochecker | 3 | 1,031 | — | — |
| storemodulespecifiers | 4 | 1,734 | — | — |
| storeparser | — | — | — | `isolated.go` 27 |

### 5.2 (e) §6.2 nameresolver の単体等価

`TestNameResolverCorpus` (31s) と `TestNameResolverFixtures`。

| 項目 | 値 |
| --- | ---: |
| file | 17,415 |
| `Resolve` の呼び出し (各側) | 10,235,856 |
| nil 以外の結果 | 1,803,200 (Value 1,412,216、Type 355,476、Namespace 35,508) |
| 結果の不一致 | 0 |
| callback の列の不一致 | 0 |
| callback `Lookup` | 33,552,972 |
| `GetSymbolOfDeclaration` | 7,573,024 |
| `OnFailedToResolveSymbol` | 4,214,388 |
| `OnSuccessfullyResolvedSymbol` | 900,836 |
| `SymbolReferenced` | 696,214 |
| `GetRequiresScopeChangeCache` / `SetRequiresScopeChangeCache` | 308 / 53 |
| `OnPropertyWithInvalidInitializer` | 266 |
| `Error` | 80 |
| JSDoc の分岐 (nameresolver.go:64-74) を通って関数に届いた識別子 | `KindJSDoc` 7,295、`KindJSDocParameterTag` 329、`KindJSDocReturnTag` 126 |

最後の `lastLocation` が `KindJSDoc` の成功は 548、`JSDocParameterTag` / `JSDocReturnTag` の成功は 0 (その 2 つは分岐を通っても最終の `lastLocation` にならない) なので、分岐を通ったかは経路で数えた。除外は無い。

fixture (各 12 呼び出し。戻り値と callback の列が一致し、Pointer の列にその callback が現れる):

| fixture | Pointer の列に現れた callback |
| --- | --- |
| globals | `Lookup` 12、`OnSuccessfullyResolvedSymbol` 2、`OnFailedToResolveSymbol` 4 |
| excludeGlobals | `OnFailedToResolveSymbol` 6 |
| alias import | `Lookup` 12、`OnSuccessfullyResolvedSymbol` 6、`SymbolReferenced` 6 |
| alias export | `GetSymbolOfDeclaration` 8、`Lookup` 28、`SymbolReferenced` 2 ほか |
| isUse | `SymbolReferenced` 2 ほか |
| not found | `OnFailedToResolveSymbol` 6 |
| error | `Error` 2 ほか |
| invalid initializer | `OnPropertyWithInvalidInitializer` 2 ほか |
| scope change cache | `SetRequiresScopeChangeCache` 1、`GetRequiresScopeChangeCache` 4 ほか |

(alias import / alias export は §6.2 の (3) を 2 本に分けたもの。)

途中で、corpus の callback の列が 3 万件食い違ったことがある。原因は試験の側で、lazy JSDoc の Store が program の外で StoreID 0 を取り、file の Store の `NodeKey` と衝突していた。`store.NewIDSpace(1)` で両方を同じ program の IDSpace に入れて直した (実装の変更は無い)。

### 5.3 (e) §6.3 printer の単体等価

`TestPrinterCorpus`: 17,415 file、**1m31s**。55 cell 全部で不一致 0。1 cell の印字の数は設定によらず同じなので、設定ごとの差だけを別に書く。

| 種類 | source file | 印字 / 設定 | 不一致 | 両方 panic | Pointer の file / nil の差 (Defaults / RemoveComments / …NeverAsciiEscape / …OmitTrailingSemicolon / …OmitTrailingSemicolonNeverAsciiEscape) |
| --- | --- | ---: | ---: | ---: | --- |
| root | あり | 17,415 | 0 | 28 | — |
| statement | あり | 261,137 | 0 | 33 | — |
| statement | nil | 261,137 | 0 | 33 | 63,669 / 18,557 / 18,321 / 18,551 / 18,315 |
| 型 | あり | 275,964 | 0 | 682 | — |
| 型 | nil | 275,964 | 0 | 682 | 2,017 / 1,704 / 1,663 / 1,704 / 1,663 |
| entity name | あり | 847,307 | 0 | 0 | — |
| entity name | nil | 847,307 | 0 | 0 | 9,050 / 183 / 183 / 183 / 183 |
| 型の複製 | あり | 275,964 | 0 | 682 | — |
| 型の複製 | nil | 275,964 | 0 | 682 | 9 (全設定) |
| entity name の複製 | あり | 847,307 | 0 | 0 | — |
| entity name の複製 | nil | 847,307 | 0 | 0 | 0 |

- 両方 panic で埋まった cell は無い (最大で 682 / 275,964)。panic の原因の内訳は取っていない。
- comment を残す `Defaults` × nil は、printer が `currentSourceFile == nil` で comment を読まずに返る (printer.go:639・859・877・3070) ので `RemoveComments` × nil と同じ経路になる。それでも file / nil の差が `Defaults` で大きいのは、file ありの側が comment を印字するため。

`TestPrinterNilSourceFile` (fixture): `type T = a;` の型参照を印字して、元のノードは Pointer が file あり `a` / nil `a` と違い、Store がそれぞれに一致。`DeepCloneNode` の複製は file あり / nil の両方で `a`。

`TestPrinterEmitContext` (Pointer の出力が設定のあり / なしで違う入力を選び、期待の文字列で固定):

| 項目 | 設定あり | 設定なし |
| --- | --- | --- |
| emit flags (`EFNoAsciiEscaping`) | `"あ"` | `"あ"` |
| textSource | `'ab'` | `"ab"` |
| original | `if /*c*/ (x) { }` | `if (x) { }` |
| 合成の comment (`AddSyntheticLeadingComment`) | `/*c*/ z` | `z` |

textSource は、`NewStringLiteralFromNode` が到達しない (Store に出ない) ので、Pointer は `NewStringLiteralFromNode`、Store は同じ本体 (literal を作って `textSource` を設定する) を試験の中で書いた。original の入力は、最初に選んだものが Pointer でも差を出さなかったので `if /*c*/ (x) {}` に替えた。

### 5.4 §6.5

`TestNewCheckerEmptyProgram`: file 0 件の Program (storechecker の `Program` を埋め込んだ stub。`Options`・`SourceFiles`・`BindSourceFiles`・`IDSpace` などだけ実装) で、Store と Pointer の `NewChecker` がどちらも panic せずに終わる。

### 5.5 `Has<X>Data` の照合 (指示書に無い試験)

報告を書く途中で、`Has<X>Data` を Pointer と照合する試験が無いことに気付き、`TestHasDataCorpus` を足した。**最初の実行で食い違いが出た**: `KindFalseKeyword` で Pointer の `FlowNodeData() != nil` が真、Store の `HasFlowNodeData()` が偽。

原因は生成器: schema では `FalseKeyword` などを `Token` と `KeywordExpression` (`FlowNodeBase` を継ぐ) の両方が定義していて、生成器は kind ごとに 1 つの定義 (`kindDefinition`、payload の大きい方で、同じなら `Token`) の base しか見ていなかった。Pointer の parser はこれらを `KeywordExpression` で作る。`kind` を定義する全ての定義の base を合わせるように直した (generate-go-store.ts の `writeBaseTests`)。増えたのは `HasFlowNodeData` の 6 kind (`NullKeyword`・`TrueKeyword`・`FalseKeyword`・`ThisKeyword`・`SuperKeyword`・`ImportKeyword`) だけで、ほかの 8 つの述語は変わらない。

修正の後: 17,415 file、2,638,926 ノード (272 kind) で全部一致。真の数は `DeclarationData` 576,627、`ExportableData` 141,694、`LocalsContainerData` 171,866、`FunctionLikeData` 67,998、`ClassLikeData` 10,950、`BodyData` 52,075、`FlowNodeData` 1,329,039、`LiteralLikeData` 169,109、`TemplateLiteralLikeData` 5,716。

修正の前のままなら、P2 の `getFlowNodeOfNode` (flow.go) が `this`・`super`・`true` などの flow を nil と答え、`checkThisBeforeSuper` は `super()` の前の `this` を見逃した。2a は checker を動かさないので、今の試験では出ない。この修正で変わった生成物は `visit_generated.go` だけ (変換器の出力は byte 一致)。

限界: 照合は parser が作るノードだけ。Pointer が同じ kind を `Token` で作る経路では、kind だけで決める述語は Pointer と違う答え (Pointer は nil、Store は真) を返す。grep で見つかった経路は emit 側だけ: printer/factory.go:1304 (`NewRewriteRelativeImportExtensionsHelper` の `NewToken(KindTrueKeyword)`) と transformers/estransforms/esdecorator.go の `NewToken(KindNullKeyword)` 8 箇所。parser・checker・nodebuilder には無い。checker はこれらの emit helper のノードを検査しないので、2b の範囲では差が出ない見込み。出た場合、Store の側は flow を設定していないので `getFlowNodeOfNode` は nil の Flow を返し (Pointer と同じ)、`checkThisBeforeSuper` だけが nil の Flow で `isPostSuperFlowNode` を呼ぶ。

## 6. (f) store / storeparser / storebinder の既存の関数の変更

2341effa51 と作業ツリーを、宣言 (関数・method・型・変数) ごとに doc comment を除いて go/printer で整形して比べた。

| package | before の宣言 | 変わった | 消えた | 足した |
| --- | ---: | ---: | ---: | ---: |
| store | 2,596 | 14 | 1 | 632 |
| storeparser | 593 | 6 | 0 | 1 |
| storebinder | 222 | 6 | 0 | 50 |

**`store/utilities.go`**: before の 177 関数の本体は全部一致し、順序も同じ。足した関数は 273。

**store の変わった宣言**:

| 宣言 | 変更 | 理由 |
| --- | --- | --- |
| `File` (型) | field の移行と cache の field | §4.2 |
| `NodeHeader` (型) | struct の中の comment だけ | §4.3 の TODO の書き換え (`Builder.header` の中の comment も同じ。関数の比較は comment を見ないので数に出ない) |
| `privateTables` (型) | `anys []any` を足した | SyntheticExpression.Type (§7.1) |
| `Builder.NewSyntheticExpression`・`deepCloneReparse`・`Factory.NewSyntheticExpression`・`CloneSyntheticExpression`・`UpdateSyntheticExpression`・`shapes`、消えた `syntheticExpressionIsSpreadSlot`・`…TupleNameSourceSlot` | SyntheticExpression の extra を 2 語から 3 語に (先頭が `Type` の私有表の index)。slot の名前の変数は生成器が別の形で出す | 同上 (生成物) |
| **`Node.ForEachChild`** | `KindSyntheticExpression` の case の extra の offset が `+1` から `+2` に。ほかの case は同じ | 同上。**Walk の KPC を取り直すかは検証で決める** (SyntheticExpression は parse / bind に現れないが、`ForEachChild` の関数の本体は変わった) |
| `storage_test.go` の 4 関数 | File の field の移行の置き換え | §4.2 |

**storeparser** (`finishSourceFile`・`parseJSDocForNode`・`collectExternalModuleReferences`・`collectModuleReferences`・`forEachDynamicImportOrRequireCall`、テストの `fieldMismatches`) と **storebinder** (`bindSourceFileAsExternalModule`・`getErrorRangeForArrowFunction`・`getErrorRangeForNode`・`getScannerForSourceFile`・`getSourceTextOfNodeFromSourceFile`、テストの `fileFields`): 全部 §4.2 の置き換え。`git diff --word-diff` の語の差分は次だけで、ほかの変更は無い:

- `file.Text` → `file.Text()` (18)、`file.FileName` → `file.FileName()` (6)、`file.Path` → `file.Path()` (2)
- `file.Imports = append(file.Imports, x.Ref())` → `file.AppendImport(x.Ref())` (2)
- `file.Imports` (読み) → `file.ImportRefs()` (2)
- `&store.File{FileName: …, Path: …, Text: p.sourceText}` → `store.NewFile(p.opts.FileName, …, p.sourceText)` (1)

`storebinder/_e2e/main.go` は変更不要だった (field を使っていない。build を確かめた)。

## 7. (g) 指示書と残りの設計に無い判断

### 7.1 ユーザーに確かめたもの (2026-09-30)

| 項目 | 決定 |
| --- | --- |
| `ast.VisitEachChild` (ast/visitor.go が名指す、kind ごとの手書き 1,000 行超) | generate-go-store.ts で生成 (`visit_generated.go`)。JSDoc の parameter / property tag と raw list は `visit.go` に手書き |
| `SyntheticExpression.Type` (`*checker.Type` を `any` で持つ field) | store の私有表に `any` で持つ (slot の class `any`) |
| nodeLinkStore (`core.LinkStore[uint64, …]` の key が `ast.NodeId`) | `NodeKey` を `StoreID<<32 | NodeRef` に詰める (`NodeKey.Uint64`)。`symbolTableID` (上位 3 bit が種類) と `keyBuilder.writeNodeId` も同じ値を使う。StoreID が 2^29 未満である前提 (Pointer の NodeId の連番と同じ種類の前提) |
| content mapper (`OriginalText`・`SpanMap`) | mapper の無い値を返す (`OriginalText` = `Text()`、`SpanMap` = nil) |
| `checkIfExpressionRefinesParameter` の合成の flow | 2a の範囲外。P2 で panic (§8.1) |
| `Imports` | `Imports() []Node` を file ごとに 1 回だけ作って cache する (modulespecifiers の host が `[]Node` を受ける)。指示書 §4.2 の `Imports() []NodeRef` とは違い、`[]NodeRef` は `ImportRefs()` に。symbolaccessibility.go の読みは cache を読む (呼ぶたびの確保は無い) |
| tracing の adapter | P2 で nil を返す |

### 7.2 確かめずに進めたもの

実装を止めずに進めた。どれも出力のコンパイルが通るのに要ったもので、戻すなら P2 か規則の置き換えになる。**2026-09-30、報告の後にユーザーが了承した** (1〜4)。

1. **storebinder に `referenceresolver.go` を足した** (§3.1 は `nameresolver.go` だけ)。emitresolver.go の到達した関数が `binder.NewReferenceResolver` などを呼び、型に node を含む。file 単位で変換した (15 関数)。
2. **`storemodule` を新設した** (§3.1 の表に無い)。`module.GetResolutionDiagnostic` が `*ast.Diagnostic` を含む struct を返す関数で、R11 の診断の型が変わる。P2 1 つだけの package。`module.ResolvedModule` の型は変えず、Program の P2 も `*module.ResolvedModule` を返す (resolver の診断は Pointer の `*ast.Diagnostic` のまま。R11 の parse の診断と同じ扱い)。
3. **`Has<X>Data()` を生成した** (§3)。指示書 §4.1 は「method は足さない」。足したのは述語で、data を返す method ではない。P2 の本体を書くのに使うだけで、規則には使っていない。
4. **List 版を 5 つ増やした** (§2.2)。指示書 §3.4 の List 版の仕組みの中。
5. **`NewNodeVisitor` を P2 にして nil の Factory で panic させた**。Pointer は nil なら zero 値の factory を使うが、Store の Factory は zero 値が無い (§5.1 と同じ理由)。到達した呼び出し元は全部 Factory を渡す。
6. `TestHasDataCorpus` を足した (§5.5)。指示書 §6 に無い試験。
7. `tryGetEnd`・`greatestEnd` を `any` を受ける P2 にした (§2.2)。
8. `File` に cache の field を足した (`importsOnce`・`importNodes`・`ecmaLineMapMu`・`ecmaLineMap`・`identifiersOnce`・`identifiers`)。file は checker の間で共有されるので、どれも once か mutex で守る。`File` の大きさが増える (file ごとで、ノードごとではない)。
9. nameresolver の試験で、両側を `store.NewIDSpace(1)` に入れた (§5.2)。

## 8. 2b / 2c への引き継ぎ

### 8.1 `checkIfExpressionRefinesParameter` の panic (2b の最初に直す、2026-09-30 決定)

`getTypePredicateFromBody` は、構築子・accessor 以外で、async / generator でなく、return が 1 つ (arrow の式の本体を含む) の関数ごとに `checkIfExpressionRefinesAnyParameter` を呼び、返り値が boolean で parameter があればこの関数に届く。型注釈の無い `x => x != null` の形はほとんどの TS の file にあるので、**直さなければ 2b の checker の等価は corpus の大半の file でこの panic に当たる見込み**。

**決定 (報告の後、ユーザー): 合成の flow を作らず、P2 の中で一番上の条件を直接評価する。2b の最初の項目として入れ、2b の checker の等価で確かめる** (2a の中では checker を動かさないので確かめられない)。

- 根拠: Pointer の checker が flow node を作るのはこの関数の 3 行 (checker.go:20923・20925・20932) だけ。合成の flow は起点にしか現れず、`getTypeAtFlowNode` は Condition の枝で `getTypeAtFlowCondition` を呼んで返すだけで、それが flow から読むのは `Flags`・`Node` (= `expr`)・`Antecedent` だけ。`Shared` が立たないので `sharedFlows` にも入らない。antecedent が nil のときの Start (node 無し) は `f.initialType` を返すだけ。よって置き換えは厳密に等価。
- P2 は 3 つ (計 70 行程度): `checkIfExpressionRefinesParameter` (antecedent を求め、下を `assumeTrue` の真 / 偽で 2 回呼ぶ)、`getFlowTypeOfReferenceUnderCondition` (`getFlowTypeOfReferenceEx` の写しで、`getTypeAtFlowNode(f, flowNode)` の呼び出しだけを差し替え)、`getTypeAtSyntheticCondition` (`getTypeAtFlowNode` の depth の増減と `getTypeAtFlowCondition` の本体。antecedent が nil なら `FlowType{t: f.initialType}`)。
- 上流の変更の検出: 変換器を拡張して、1 つの P2 に写した元の hash を複数書けるようにし、`checkIfExpressionRefinesParameter`・`getFlowTypeOfReferenceEx`・`getTypeAtFlowCondition`・`getTypeAtFlowNode` の hash を持たせる (今の P2 は hash 1 つで、名前は元か `<名前>List` に限る)。
- 採らない案: Store に合成の flow (region の私有の Bound と外の antecedent) は `Flow.Antecedent()` の分岐が flow の走査のホットパス全部に乗る。`Flow` を 3 語にする案はさらに重い。source の Bound への追記は seal と共有で race、書き込む前の複製は呼ぶたびに file の flow の全部を写す。

### 8.2 性能 (2c)

2a は性能の線を持たないので測っていない。コードを読んで気付いたもの:

- printer の `emitList` の中の R4' (printer.go:4790 の `children.Nodes()[start:end]`) は list を印字するたびに確保する。checker の印字 (`typeToString`) と emit の両方に乗る。
- `VisitSliceList` (P2) は、変わらなかったときも `nodes.Nodes()` を返す (Pointer は元の slice を返し、確保しない)。呼び出し元の `VisitNodes`・`VisitModifiers` は変わらなかったときその結果を捨てるので、nodecopy の visit ごとに捨てる確保が 1 つある。変わったときは `slices.Clone(nodes.Nodes()[:i])` で 2 回写す。
- R4' の 11 箇所 (案 A) は指示書どおり。案 B / E は 2c の後。

### 8.3 そのほか

- 検証の R8 の照合 (§4) で、nodebuilderimpl.go:3369 の `symbolToTypeNode` の出どころは comment と経路からの分類で、静的な証明ではない。2b の writer の実行時の検査で確かめられる。
- 既知の失敗 `FuzzEquivalence/a708303f611b95c8` はそのまま。
- 検証 (Claude 報告 §3.2・§3.3) で見つかり、2b / 2c に回すもの:
  - `Node.NextContainer()` (checker_api.go:467) は root の SourceFile に `Node{}` を返す (Pointer は最初の入れ子の container)。今の呼び出し元は ModuleDeclaration / EnumDeclaration しか渡さないので結果は変わらない。潜在的な不一致。
  - `VisitSliceList` は変わらなかったときも `nodes.Nodes()` を確保し、変わったときは 2 回写す (2c)。
  - `isUniqueLocalName` は `NextContainer` の線形探索で O(C²) (2c)。
  - 刈り残しの 56 関数。主因は `reach.go:356` の `methodsNamed` が P2 の本体の selector を名前だけで照合すること。コンパイルには無害。

## 9. 検証後の訂正 (2026-09-30)

検証報告 2 本 ([Claude 報告](store-checker-7d2a-verification-report-20260930-claude.md)、[Codex 報告](store-checker-7d2a-verification-report-20260930.md)) による。本文は書き換えていない。

- §5.3: emit flags の期待の出力は設定あり `"あ"` / 設定なし `"\u3042"`、`TestPrinterNilSourceFile` の元のノードは file あり `\u0061` / nil `a` (escape が落ちていた)。
- §5.2: `JSDocParameterTag` 329・`JSDocReturnTag` 126 は、親の鎖にその tag がある識別子の数で、分岐を通った数ではない。分岐は Go 版では到達しない ([修正指示書](store-checker-7d2a-fix-instructions-20260930.md) §4.1)。
- §2.2: store の「7d-1 以前から 106 行」は 127 行。List 版の「どれも list と slice の両方の呼び出し元を持つ」は 11 中 6 で誤り (`checkTypeParameters`・`reportUnusedVariableDeclarations`・`appendTypeParameters`・`getInstantiatedConstructorsForTypeArguments` は呼び出し元が 0、`checkTypeParametersNotReferenced`・`getConstructorsForTypeArguments` は死んだ関数からしか呼ばれない)。
- §4: 書き込みは「変換 35、P2 7」ではなく「36、6」。nodecopy.go:886 の根拠の行は 313 ではなく 878。nodebuilderimpl.go:3369 は静的に証明できる (ImportType の出どころが `b.f.NewImportTypeNode` だけ)。
- §1.3: `changetrackerwriter.go` が入ったのは CHA ではなく、P2 の `p.writer.HasTrailingComment()` (printer_manual.go:55) の名前の一致による。
- §7.2 の 1: `referenceresolver.go` が要るのは storechecker の型の宣言を全部出すため (`EmitResolver` の field の型)。`getReferenceResolver` と `GetReferenced…` の 7 method は CHA だけで到達し、呼び出し元は無い。
- §7.2 の 2: `storemodule.GetResolutionDiagnostic` の返り値は `*diagnostics.Message`。診断を含むのは引数の `ResolvedModule` の側。
