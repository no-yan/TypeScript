# Store parser (TODO 7b) 検証レポート

作成日: 2026-09-22。手順は [store-parser-verification-instructions-20260922.md](store-parser-verification-instructions-20260922.md) (以下「検証指示」)、実装は [store-parser-implementation-instructions-20260922.md](store-parser-implementation-instructions-20260922.md) (以下「実装指示書」) に従った `tsc/internal/storeparser/` と `tsc/internal/ast/store/` の追加分。設計は [store-ast-design-20260922.md](store-ast-design-20260922.md) §2.7。前段は [store-generator-verification-report-20260922.md](store-generator-verification-report-20260922.md) (以下「7a」)。

実装と検証を同じセッションで行った。計測前に source を確定し (`_store-parser-results/sha256.txt`、計測後に取得したが計測以降に編集していない)、計測のために実装を変えていない。正しさのために直したのは実装中の 2 点 (§3.5) で、いずれも計測前。

KPC はユーザーが別ターミナルで実行した (`kpc-20260922-1602.txt`、load average 1.7〜3.6)。ns / retained / gctrace は私が続けて取った (load average 3.35、§2)。

**訂正 (同日 16:34)**: 評価で retained の数字に `sync.Pool` の scratch が混じっていることが分かり (§7.2)、`BenchmarkStoreParseRetainedV1` の `runtime.GC()` を 2 回にして retained / gctrace を取り直した (`bench_test.go` だけ変更、`sha256.txt` 更新)。allocs の内訳も取った (§6.3)。§1 の表と §7 はその数字に差し替えてあり、初回の値は §7.2 に残す。

**追記 (同日 19:13)**: §6.2 で未帰属だった IPC 低下は `View` だった。`Builder` に `Store` を埋め込んで `View` の詰め直しを消した (S1) 後の KPC と、その帰属を §11 に足した。§6.2 と §9 の該当行にも印を付けた。§1 の表は 7b 時点のままにしてある。

## 1. 結論

1. **正しさは全項目合格。** TS / TSX / D.TS / JSON は corpus 17,415 file (7a と同じ入力、skip なし) で木・flags・診断が完全一致。JS は Pointer の JSDoc 由来の差 (§3.3) を除いて一致。生成物は再現し、`internal/parser` / `scanner` / `ast` (store 以外) に差分なし、production からの import なし。
2. **top-level await の reparse は動かない (7c)。** ExternalModuleIndicator が無いので `reparseTopLevelAwait` は移植済みだが未呼出。corpus の 8 file (module で `await` 識別子を含む) を等価テストから除外した (§3.4)。
3. **移植は機械的。** diff は 166 hunk / −1,221 +1,223 行で、constructor の折り畳み 200 箇所、子の受け直し 46 箇所、§4.3 の書き換え 9 箇所は全部 constructor で吸収 (`SetParent` 相当なし)。分類 (f) は 22 項目で §4.3 に列挙した。`store` に足した API のうち §3 に無いものは 5 つ (§5.1)、いずれも型上の必要から。
4. **cycles は同等、命令は −11.6%。** KPC (checker.ts、20x × 5 run 平均) で pointer 260.2M inst / 66.77M cycles / IPC 3.90、store **230.1M inst / 66.51M cycles / IPC 3.46**。cycles 比 0.996 で提案線 ≤ 1.00 の内側だが、run の幅 (pointer 66.63〜66.95、store 66.30〜66.80) が重なるので**速くはなっていない**。inst の減少が IPC の低下で相殺されており、Walk (7a) と同じ形 (§6.2)。
5. **メモリと GC の賭けは確認できた。** B/op 0.487 (GC オフ) / 0.38〜0.40 (GC あり)、allocs 11,931 → 582、retained 94.0 → **35.1 B/node** (0.37、7a の footprint 33〜34 と `Footprint()` 合計 34.3 に一致)。保持した状態の `runtime.GC()` は 9.2〜15.3 ms → 0.25〜0.37 ms (**0.03 倍**)、gctrace の mark は 54 ms cpu → 0.4 ms cpu、live 111 MB → 49 MB (§7)。
6. **提案線に対する不合格は無い。** allocs/op 582 は線 200 を超えるが、9 割は speculation 中に作って rewind で捨てる診断で Pointer と共通 (§6.3)。Store 固有は 25 程度で、線 200 の前提 (`Finish` 3 + `File` + diagnostics + scanner) に収まる。retained は初回 45.9 B/node で不合格と書いたが、`sync.Pool` に戻した Builder scratch の混入で (§7.2)、GC 2 回で 35.1。

| 指標 | pointer | store | 比 | 提案線 | 判定 |
| --- | ---: | ---: | ---: | --- | --- |
| cycles/op (KPC) | 66.77M | 66.51M | 0.996 | ≤ 1.00 | 合格 (同等) |
| inst/op (KPC) | 260.2M | 230.1M | 0.884 | 報告 | |
| B/op (KPC、GC オフ) | 26.14 MB | 12.72 MB | 0.487 | ≤ 0.5 | 合格 |
| allocs/op | 11,931 | 582 (Store 固有は約 25) | | ≤ 200 | 合格 (Pointer 共通分を除く) |
| retained B/node | 94.04 | 35.07 | 0.37 | ≤ 40 | 合格 |
| 保持中の GC 1 回 | 11.1 ms (中央値) | 0.30 ms | 0.03 | ≤ 0.25 | 合格 |

## 2. 環境と入力

- commit `7ae34f98cb` (branch `store-ast`)、未コミットの変更は `generate-go-store.ts`、`internal/ast/store/`、`internal/storeparser/`、docs だけ。
- go1.27.1 darwin/arm64、Apple M1 (8 GB)。
- 負荷: KPC 15:58〜16:02 は load average 1.74 → 3.62 (KPC は GOMAXPROCS(1) + GC オフの Session で、run 間の幅は inst 0.18% / cycles 0.5%)。ns は 16:03、load average 3.35 (幅 0.2%、§6.1)。retained / gctrace の取り直しは 16:34、load average 2.4〜3.5 (retained は run 間で不変、gc-ms は幅が大きい、§7.1)。
- 入力: `fixtures.ASTBenchFixtures` (checker.ts 298,054 node、dom.generated.d.ts 109,605 node)、corpus は `testdata/fixtures` (166 file) + `tests/cases/{compiler,conformance}` (計 17,415 file、2,575,873 node visited)。retained ベンチは fixtures の 166 file。

## 3. 正しさ

### 3.1 検証指示 §1

| 項目 | 結果 |
| --- | --- |
| `node tools/scripts/tsc/generate.ts` | 差分なし。出力先が `storetest/equivalence_generated.go` (package `storetest`) に変わった以外、生成物 5 file に差分なし |
| `go build ./...`、`go vet ./internal/ast/... ./internal/storeparser/...` | OK |
| `git diff --stat HEAD -- internal/parser internal/scanner internal/ast ':!internal/ast/store' ':!internal/ast/docs'` | 空 |
| `go test ./internal/ast/store/... ./internal/storeparser/...` (tag なし) | PASS。`TestCorpus` 17,415 file、3.5 s |
| `-tags storechecks` | PASS |
| production からの import | なし |
| `go test ./internal/ast/ ./internal/parser/` | PASS |
| `unsafe` | `Ref()` と `Refs()` の 2 箇所だけ (`store_test.go` の `Sizeof` を除く) |

実装指示書 §1 のガード: package `storeparser`、`internal/parser` の import なし (テストのみ)、`doc.go` は 5 項目あり。**許可リストに無い import が 2 つ**: `internal/debug` (`debug.Assert` 3 箇所) と `internal/collections` (`notParenthesizedArrow`)。どちらも Pointer parser が使うものを機械的に残した。

### 3.2 等価テストの範囲

`storetest.Equivalent` は 7a の driver をそのまま移したもので、header (kind / pos / end / Flags / ModifierFlags)、親、定義ごとの全 member (466)、役割 accessor (180) を比べる。7a からの変更は次の 3 つ (いずれも `Options` で有効になり、TS 系では動かない):

- `SkipReparsed`: Pointer の `NodeFlagsReparsed` ノードを不在として扱い、list からは除く。要素が全部 reparsed の list は nil list と一致させる (reparser が `@template` 等から作った list を Store は持たない)。
- JSDoc cast の透過: `/** @type {T} */ (e)` を Pointer は `AsExpression(e, T)` に包むが、この AsExpression 自体は Reparsed flag を持たない (型だけが持つ)。`e` をその位置に、親は cast を飛ばして比べる。
- kind が違うノードでは member / role を比べない (typed view が別 kind の payload を読んで範囲外になる。7a には無かった防御で、除外 file (§3.4) で panic した)。

JS 系の `MaskFlags` は `HasJSDoc | PossiblyContainsDeprecatedTag | ThisNodeHasError | PossiblyContainsDynamicImport`。後ろ 2 つは実装指示書 §5 に無く、corpus で見つけた: reparser の `checkNonIdentifierName` が `Identifier_expected` を出すと次に finish されるノードに `ThisNodeHasError` が付く (8 file、EndOfFile 等)、JSDoc の `import("x")` 型が root に `PossiblyContainsDynamicImport` を付ける (43 file)。診断は別に比べるので (§3.3) mask して安全。

### 3.3 診断

比較単位は `(Pos, Len, Code, MessageText)` の列。

| | file 数 |
| --- | ---: |
| 完全一致 | 17,397 |
| JS で Store が部分列 | 10 |
| 一致しない | 0 |
| 除外 (§3.4) | 8 |

部分列の 10 file の欠けは全部 code 1003 (`Identifier_expected`、`MessageText` は空) で、reparser の `checkNonIdentifierName` が `@typedef {C~A} C~B` のような名前に出すもの。JSDoc 由来と説明できる。

### 3.4 除外: top-level await

`parseToplevelStatement` は Pointer と同じく `possibleAwaitSpans` を集めるが、`reparseTopLevelAwait` の起動条件 `ExternalModuleIndicator != nil` を 7b では作れない。Pointer 側が reparse した file (`!IsDeclarationFile && ExternalModuleIndicator != nil && len(possibleAwaitSpans) > 0`) は木も診断も違うので除外し、テストが番号と名前を log する: #713、#714、#3037、#5255、#7521、#8555、#12580、#12584 (計 143 mismatch)。`reparseTopLevelAwait` の移植は §4.3 の (f) にある形で、7c で indicator を入れたら有効化する。

### 3.5 実装中に直した正しさのバグ

計測前。両方とも設計上の穴で、実装指示書に無い。

1. **`missingLists` は Set にできない。** speculation 中に作った missing list を truncate した後、同じ index に別の block ができると Set が誤判定する。昇順の `[]ListRef` にして `rewind` が mark 以降を pop する。
2. **literal の `TokenFlags` は Pointer の constructor がマスクする** (`& TokenFlagsStringLiteralFlags` 等)。同じマスクを掛けた。無いと dom で 569 件不一致。

### 3.6 dead node

| | Store NodeCount | visits | dead | Pointer NodeCount | Pointer dead |
| --- | ---: | ---: | ---: | ---: | ---: |
| checker.ts | 298,884 | 298,054 | 830 (0.28%) | 302,587 | 4,533 |
| dom.generated.d.ts | 110,435 | 109,605 | 830 (0.76%) | 113,504 | 3,899 |
| corpus 全体 | 2,598,034 | 2,575,873 | 22,161 (0.86%) | 2,822,645 | 246,772 |

kind 別: checker.ts は `ExpressionWithTypeArguments` 109 (`f<T>()` の吸収) + `Identifier` 721。dom は `ExpressionWithTypeArguments` 830 (`interface extends` の TypeReference 変換)。**721 の Identifier は `parseElementAccessExpressionRest` が `createMissingIdentifier()` を先に作って通常経路で捨てる Pointer の癖**で、規則 4 に従って直していない。else 側に移せば checker.ts の dead node は 109 になる (§8)。

speculation の truncate: 8 種の小 source (`(a, b) => c`、`a < b > c`、`x!!.y`、`a ? (b) : c`、`f(a < b, c > d)` 等) で dead 0。rewind を伴わずに捨てる 6 種は期待数どおり (`TestDeadNodes`)。

### 3.7 `store.File` の field

corpus 全体で `FileName` / `Path` / `Text` / `ScriptKind` / `LanguageVariant` / `IsDeclarationFile` / `Flags` (root と同値) / `CommentDirectives` / `Pragmas` / `ReferencedFiles` / `TypeReferenceDirectives` / `LibReferenceDirectives` / `CheckJsDirective` が一致。`IdentifierCount` は JS と、TS で `@see` / `@link` を含む file (checker.ts: 10 件、Store 126,455 vs Pointer 126,477) で Pointer が大きい。Pointer が eager parse した JSDoc の識別子を数えるためで、テストはその file だけ比較を外して log する。

### 3.8 scratch の再利用

checker.ts を同じ parser で 3 回: 651 → 577 → 577 allocations。2 回目以降の 577 の内訳は取っていない (§6.3)。

## 4. 移植の機械性

### 4.1 diff の統計

`_store-parser-results/parser.diff`: 5,459 行、166 hunk、−1,221 / +1,223 行 (6,854 → 6,849 行)。関数の順序は同じ。関数名の差は 12: 削除 10 (`initializeClosures`、`ParseIsolatedEntityName`、`createJSDocCache`、`unparseExpressionWithTypeArguments`、`newNodeList`、`newModifierList`、`finishNode`、`finishNodeWithEnd`、`overrideParentInImmediateChildren`、`attachFileToDiagnostics`)、追加 2 (`jsdocFlags`、`flags`)。free 関数からメソッドになったもの 3 (`isMissingNodeList`、`modifierListHasAsync`、`typeHasArrowFunctionBlockingParseError`)。

### 4.2 hunk の分類

| 分類 | 数 (箇所) |
| --- | ---: |
| (a) 型の置換 (`*ast.Node` → `NodeRef`、`nil` → `NoNodeRef` 114 / `NoListRef` 45、`.Kind` / `.Pos()` / `.Text()` の `View` 化 91) | 約 250 |
| (b) constructor の折り畳み (`p.b.New` 200、`p.b.List` 18、`withJSDoc` → `AddFlags(jsdocFlags)` 56) | 274 |
| (c) 子をローカルに受け直し | 46 (script 41 引数 / 36 行 + 手作業 5) |
| (d) §4.3 の書き換え 9 箇所 | 9、全部 constructor で吸収 |
| (e) JSDoc / reparse の削除 | 12 関数 + field 11 |
| (f) それ以外 | 22 項目 (§4.3) |

(c) の一覧は実装報告のとおり: parseBlock、parseForOrForInOrForOfStatement ×3、parseExpressionOrLabeledStatement、parseTypeOperator、parseInferType、parseThisTypePredicate、parseJSDocNonNullableType、parseJSDocNullableType、parseTypeReference ×2、parseEntityName、parseTypeOrTypePredicate、parseTypeLiteral、parseTupleType、parseTupleElementType、parseTemplateType ×2、parseTemplateTypeSpan ×2、parseYieldExpression ×2、parseUpdateExpression、parseJsxElementOrSelfClosingElementOrFragment (fragment ×2)、parseJsxElementName、parseJsxTagName、parseJsxAttributes、parseJsxAttribute ×2、parseJsxAttributeName、parsePrefixUnaryExpression、parseDeleteExpression、parseTypeOfExpression、parseVoidExpression、parseAwaitExpression、parseLeftHandSideExpressionOrHigher、parseSuperExpression、parseTemplateExpression ×2。手作業: parseParameterEx ×2、convertEntityNameExpressionToEntityName、parseTypeHeritageClauseElement、JSX の閉じタグ再構成。

`finishNodeWithEnd` の 3 箇所 (JSX の `missingIdentifier` / `newClosingElement` / `newLast`、`parseTupleElementType`、`convertEntityNameExpressionToEntityName`) は `end` がそのまま渡っている。

### 4.3 分類 (f) の一覧

Pointer が「生成 → 何か → finishNode」の順に依存している箇所は、finishNode の時点 (end と `hasParseError`) に生成を動かした。

| # | 箇所 | 内容 |
| --- | --- | --- |
| 1 | parseKeywordTypeNode、parseKeywordExpression、parseLiteralExpression、parseTemplateHead、parseTemplateMiddleOrTail、parseJsxText、tryParseModifier、parseModifiersForConstructorType | 「生成 → nextToken → finishNode」を「値を読む → nextToken → 生成」に |
| 2 | parsePropertyOrMethodSignature | finishNode がセミコロンの後なので `parseTypeMemberSemicolon()` を両分岐に入れて生成を後ろに |
| 3 | parsePropertyAccessExpressionRest、parseParameterEx (this) | エラー報告の後に finishNode されるので生成をエラー報告の後に |
| 4 | parseLiteralExpression、parseTemplateHead、parseTemplateMiddleOrTail | Pointer constructor の `TokenFlags` マスク (§3.5) |
| 5 | parseJsxElementOrSelfClosingElementOrFragment | 合成 `,` token の flags は `None` (Pointer が finishNode を通さない。実装指示書は `p.flags()` と書いていた) |
| 6 | parseModuleOrNamespaceDeclaration | 暗黙 export の token は flags `Reparsed` だけ (contextFlags なし、Pointer と同じ) |
| 7 | parseListIndex | `[]*ast.Node` の代わりに `p.elems` の mark を返し、呼び側が `List` を作る |
| 8 | parseUnionOrIntersectionType、parseTemplateSpans、parseTemplateTypeSpans、parseModifiersEx、parseJsxChildren | ローカル slice を `p.elems` scratch に |
| 9 | createUnionOrIntersectionTypeNode | `pos` 引数を追加 (生成が finishNode の位置に移ったため) |
| 10 | newIdentifier | `(text, pos, end)` を取る (Store のノードは生成時に確定) |
| 11 | validateJsonValue、validateJsonObjectLiteral | `*ast.SourceFile` 引数を落とし、`store.Node` を取る。診断の file は nil |
| 12 | isMissingNodeList | メソッドと `[]ListRef` (§3.5) |
| 13 | modifierListHasAsync、typeHasArrowFunctionBlockingParseError | `p.b` が要るのでメソッドに |
| 14 | isDeclareModifier / isExportModifier / isAsyncModifier | `store.Node` を取る。`core.Some` / `core.FindIndex` は `p.someModifier` / `p.findModifierIndex` に |
| 15 | modifiers.ModifierFlags | `p.modifierFlags(list)` で fold (ModifierList の事前計算が無い) |
| 16 | parseTypeHeritageClauseElement、convertEntityNameExpressionToEntityName、parseMemberExpressionRest、parseCallExpressionRest、parseNewExpressionOrNewDotTarget | 吸収する子の Ref を先に View から取り出す。`unparseExpressionWithTypeArguments` は削除 (constructor が親を書く) |
| 17 | parseTupleElementType | `NewOptionalTypeNode(v.Flags(), v.Pos(), v.End(), inner)`。JSDocNullableType は dead node |
| 18 | tryReparseOptionalChain | `AddFlags` + `View(node).Expression().Ref()` で下る |
| 19 | reparseTopLevelAwait | 旧 statements の Ref を `slices.Clone` で写してから reparse。rewind 前に `state.nodes, state.extra = p.b.Mark()` でノードを残す (diagnosticsLen と同じ扱い)。**未呼出** |
| 20 | parseSourceFileWorker、finishSourceFile | `reparseTopLevelAwait` の起動と `collectExternalModuleReferences` / `SetExternalModuleIndicator` / JSDoc cache を呼ばない (7c)。`finishSourceFile` が `Finish` を呼んで `*store.File` を返す |
| 21 | putParser | `scanner` に加えて `b`、`elems[:0]`、`missingLists[:0]` を残す |
| 22 | checkJSSyntax | `node.QuestionToken()` の Pointer は PostfixToken に fall back するので Store でも `PostfixToken()` を見る。`IsTypeOnly` は typed view で |

### 4.4 §4.3 の表との照合

9 箇所とも現物と一致し、`SetParent` 相当は無い。行 475 は `finishSourceFile` の `AddFlags(root, sourceFlags)`、1139 / 1928 は `ViewList(modifiers).Refs()` ループの `AddFlags`、2269 は `NewToken(Export, Reparsed, pos, pos)`、3706 は #17、4818〜4841 は constructor の adopt、4866 は #5、5484 は #18、5794 は #16、608 は #19。

### 4.5 `View` の規則

`grep -n 'View('`: parser.go 78 行 (+ `ViewList(` 9 行)、utilities.go 5 行。戻り値を変数に受けるのは 9 箇所で、いずれも append の前に値 (Ref / Pos / End) を取り出すか、append しない範囲でしか使わない: reparseTopLevelAwait の `file` (Refs を `slices.Clone`)、parseTypeHeritageClauseElement / convertEntityNameExpressionToEntityName / parseTupleElementType の `v`、JSX の `last`、parseJsxChild の `tag`、parseMemberExpressionRest の `original`、checkJSDecoratorSyntax の `modifiers`、checkJSSyntax の `v`。`View` の呼び出し 157 箇所は全部 inline されている (§5.2)。

## 5. Builder と constructor

### 5.1 `store` に足した API

§3 どおり: `Builder.Reset` / `Finish` (`strings.Clone`) / `View` / `AddFlags` / `SetFlags` / `SetLoc` / `Mark` / `Truncate` / `Len`、`texts` は `strings.Builder`、`storetest`。`SetFlags` と `SetLoc` は parser が使っていない (§4.3 の 9 箇所が全部 constructor で吸収されたため)。

§3 に無いもの 5 つ:

| API | 理由 |
| --- | --- |
| `Builder.ViewList(ListRef) List` | 実装指示書の例 `View(modifiers).Refs()` は ListRef に対して型上不可能 |
| `List.Ref() ListRef` | 型引数の吸収 5 箇所で typed view から ListRef が要る。無いと block を複製するしかない |
| `File.SetDiagnostics` / `SetJSDiagnostics` | field が非公開 |
| `inspect.go` (`Footprint` / `NodeCount` / `IdentifierTextInTexts` / `Shape`) | `export_test.go` から移動。storeparser のテストが読む |
| `store.File` (`SourceFile` の改名) | typed view `store.SourceFile` と衝突 |

### 5.2 inline

| 対象 | 結果 |
| --- | --- |
| `View` (45)、`ViewList` (37)、`AddFlags` (7)、`SetFlags` (7)、`SetLoc` (13)、`Mark` (7)、`Truncate` (13)、`Len` (4)、`header` (30)、`adopt` (11)、`adoptList` (26)、`List` (36)、`Reset` (61)、`Finish` (71)、`NewToken` (39)、`List.Ref` (3) | can inline。parser 側で `View` 157、`AddFlags` 60、`List` 29、`ViewList` 13、`Mark` 13、`NewToken` 19 箇所が実際に inline されている |
| 7a の accessor | 7a と同じ。cannot は `Node.Text` (157)、`identifierData` (119)、`textWords` (89)、`modifierFlags` (102)、`assertKinds` (83) |
| 生成 constructor | **can inline 41 / cannot inline 138**。payload 2 word までが 80 に収まり、3 word 以上は超える (`NewIdentifier` 99、`NewPropertyAccessExpression` 106、`NewCallExpression` 152、`NewSourceFile` 105、`NewStringLiteral` 125) |

constructor の大半が call になるのは 7a の設計 (post-order の constructor) の帰結で、検証指示 §3 の期待表は「inline される」前提で書かれていない。call 1 回あたりの prologue / epilogue は §6.2 の inst 差の一部を占める。

### 5.3 命令列 (`_store-parser-results/asm-*.txt`)

inline された constructor はシンボルが無いので、呼び側で見た。

| 対象 | 結果 |
| --- | --- |
| `newIdentifier` (52 命令) | `NewIdentifier` への CALL 1 |
| `identifierData` (96 命令) | `memequal` 1 (source の部分文字列なら pointer 一致で終わる)、`textWords` 1 (escape のときだけ)、`growslice` 1 (`extra` の append)、`gcWriteBarrier2` 1 (slice header の書き戻し)、`panicBounds` 2 |
| `parseCallExpressionRest` (380 命令) | `NewCallExpression` 1、`NewPropertyAccessExpression` 1、`growslice` 0 (append は constructor の中)。`panicunsafestring*` 6 は `View` が `texts.String()` を詰めるときの検査 |
| `parseList` (128 命令) | `growslice` 2 (`extra` の header と要素の 2 段 append)、`make` なし |
| `parseSourceFileWorker` (280 命令) | `NewSourceFile` 1、`finishSourceFile` 1 |

`Finish` は 3 本の copy + `strings.Clone` で、B/op (§6.1) がそれに近い。

## 6. parse の費用

### 6.1 ns / B/op / allocs (GC あり、`-benchtime 2s -count 5`)

| | pointer | store | 比 |
| --- | ---: | ---: | ---: |
| checker.ts ns/op | 23.33〜24.01 ms | 20.81〜20.85 ms | 0.89 |
| checker.ts B/op | 26.14 MB | 9.94〜10.51 MB | 0.38〜0.40 |
| checker.ts allocs/op | 11,931 | 578 | |
| dom.generated.d.ts ns/op | 10.36〜10.40 ms | 8.67〜8.69 ms | 0.84 |
| dom.generated.d.ts B/op | 9.90 MB | 4.00 MB | 0.40 |
| dom.generated.d.ts allocs/op | 2,787 | 1,527 | |

GC ありの B/op が KPC (GC オフ) の 12.72 MB より小さいのは、GC が `sync.Pool` を空にして新しい parser (小さい scratch) が使われる run が混じるため。GC オフでは同じ parser が返り、その scratch が大きい側 (cap の倍化) の数字になる。

### 6.2 KPC (checker.ts、20x × 5 run)

| | pointer | store | 比 |
| --- | ---: | ---: | ---: |
| inst/op | 260.2M (259.9〜260.4) | 230.1M (230.07〜230.22) | 0.884 |
| cycles/op | 66.77M (66.63〜66.95) | 66.51M (66.30〜66.80) | 0.996 |
| IPC | 3.90 | 3.46 | |
| per node (298,054) | 873 inst / 224 cycles | 772 inst / 223 cycles | |

pointer は基準値 (262.5M inst、69.3M cycles) を inst −0.9% で再現した。cycles は基準より 3.7% 少ないが、run 内の幅 0.5% で、判定は同じ binary の pointer に対する比で行う。

**命令が 30M 減って cycles が動かない。** IPC 3.90 → 3.46。この差の帰属は取っていない。候補は (1) constructor の call 138 種 (§5.2)、(2) `header` と `extra` の 2 本を伸ばす append の依存、(3) `adopt` の親書き込み (子の header への store)、(4) `identifierData` の `memequal`、(5) `View` が毎回 `Store` 構造体 (5 word) を詰め直す 157 箇所。関数別の retired instructions か Instruments の delivery 分解が要る (§9)。

**→ §11 で確定: (5) の `View` がほぼ全部。**

### 6.3 allocs

allocs 582 の内訳 (`-memprofilerate=1`、20 回、`allocs-store-checker.txt`。数字は 1 回あたり):

| 経路 | allocs | 由来 |
| --- | ---: | --- |
| `parseExpectedWithDiagnostic` → `parseErrorAtCurrentToken(X_0_expected, TokenToString(kind))` | 約 390 | speculation (`tryParse`) 中に `parseExpected` が失敗して `args ...any` を箱詰めする。rewind で診断は捨てるが割り当ては残る。Pointer も同じ経路 |
| `ast.NewDiagnostic` | 134 | 同上 (checker.ts の最終診断は 0) |
| `strings.(*Builder).WriteString` | 19 | `texts` の再成長 (`Reset` が buf を捨てる) |
| `diagnostics.StringifyArgs`、`strconv.FormatInt` | 12 | 同じ speculation 診断 |
| `Finish`、`finishSourceFile`、`createIdentifierWithDiagnostic` | 約 10 | |

Store 固有は `texts` と `Finish` の 25 程度で、残りは Pointer から持ち越した speculation の副産物である。線 200 を「Store 固有」と読めば合格。dom の 1,527 も同じ経路と見られる (string literal ではなく `;` / `,` の期待失敗) が、profile は取っていない。

Pointer の JSDoc 余分: checker.ts に `@see` / `@link` は 10 件で、Pointer はその分だけ eager parse する (IdentifierCount の差 22、§3.7)。inst 260M に対して無視できる。

## 7. retained heap と GC

### 7.1 数字 (fixtures 166 file、`-benchtime 1x -count 5`)

| | pointer | store | 比 | 提案線 |
| --- | ---: | ---: | ---: | --- |
| nodes (各 NodeCount) | 1,097,788 | 1,075,673 | | |
| retained MB | 103.2 | 37.7 | 0.37 | |
| retained B/node | 94.04 | 35.07 | 0.37 | store ≤ 40 |
| `Footprint()` 合計 (nodes 25.82 + extra 10.36 + texts 0.72 MB) | | 34.30 B/node | | |
| 保持中の `runtime.GC()` 1 回 | 9.23 / 9.99 / 11.12 / 11.35 / 15.29 ms | 0.25 / 0.29 / 0.30 / 0.30 / 0.37 ms | 0.03 | ≤ 0.25 |

GC 2 回の取り直し (16:34、load average 2.4〜3.5)。retained は run 間で不変、gc-ms は負荷で幅が出る (初回 16:03 は pointer 7.5〜9.4 / store 0.23〜0.31 ms で比は同じ)。retained と `Footprint()` の差 0.77 B/node (0.8 MB) が `File`、diagnostics (1,220 件)、pragmas、size class の分で、retained の比 0.37 は設計文書 §1 の footprint 39〜41% の内側。

gctrace (強制 GC、`gctrace-{store,pointer}.txt`): pointer は live 111 MB で mark 6.9 ms clock / 54 ms cpu (assist 0 + background 13 + idle 40)、store は live 49 MB (計測後の forced GC の行 `49->49->49`) で mark 0.17 ms clock / 0.4 ms cpu。**GC が辿る量は 2 桁減った**。これが設計文書 §1 の賭けの最初の実測で、合否に関わらず残す。

### 7.2 初回の 45.91 B/node は `sync.Pool` の混入

初回 (16:03) は `runtime.GC()` 1 回の後に `HeapAlloc` を読み、store 49.38 MB / 45.91 B/node、pointer 103.5 MB / 94.27 B/node だった。差の 11.7 MB は `putParser` が `parserPool` (`sync.Pool`) に戻した Builder の scratch (cap を倍化した `nodes` / `extra`、最大 file の checker.ts 分) で、`sync.Pool` は GC 1 回目で victim cache に移るだけで解放されない。初回の gctrace で最後の forced GC の行が `60->60->49 MB` と落ちていたのがそれで、本文が「live 60 MB」と書いたのは 1 回目の行を読んだため。Pointer parser は pool に大きな scratch を持たないので 103.5 → 103.2 MB とほぼ動かない。

確認は 2 通り: GC 2 回で 35.07 B/node、`Store.Footprint()` の合計 34.30 B/node (§7.1)。7a の footprint 32.95 (checker) / 34.18 (dom) とも一致する。ベンチは GC 2 回に直した (検証指示 §0 の「実装を直さない」は守っており、直したのはベンチの計測手順)。

## 8. 設計文書への反映案

1. §2.7 に「post-order の constructor は payload 3 word 以上で inline budget を超える (41 / 179 が inline)」を足す。IPC 低下の候補として、call の prologue と 2 本の append の依存を挙げる。
2. §5「死にノードの切り詰め」の実測: fixtures 0.35%、corpus 0.86%。うち `parseElementAccessExpressionRest` の先行 `createMissingIdentifier` が checker.ts の 87%。切り詰めより parser 側の 1 行移動 (else 側で作る) の方が安い。
3. §1 の「GC が辿る pointer」: 保持中の mark cpu 57 ms → 0.6 ms (166 file、1.1M node)。
4. §2.7 の「使い回しの scratch」に注記: pool に戻した scratch は GC 1 回では解放されない (victim cache)。retained や live heap を測るときは GC 2 回か `Footprint()` と照合する (§7.2)。
5. `Reset` が `strings.Builder` の buf を捨てる点: file ごとに texts が再成長する (allocs 数本)。ゼロコピーの安全性のための設計 (§2.4) で、変えるなら `Finish` で `strings.Clone` しているので buf の保持は安全だが、`strings.Builder` に truncate が無いので `[]byte` + `unsafe.String` に戻すことになる。

## 9. 限界

- 実装者と検証者が同じ。diff の hunk は移植中に全部読んだが、独立した目は入っていない。
- IPC 低下の帰属 (§6.2) は未計測 (→ 同日 19:13 に §11 で計測、`View`)。dom の allocs 1,527 の profile も取っていない (§6.3)。
- top-level await の 8 file は比べていない (§3.4)。
- JS の等価は Pointer の JSDoc 由来を除いた「部分一致」で、除外規則 (§3.2) の正しさは corpus の JS file (約 3,000) が 0 mismatch で通ることによる。
- load average が高い環境 (1.7〜3.6) で取った。KPC は GOMAXPROCS(1) + GC オフなので幅は小さいが、ns は参考値。
- `Node.Text` は cannot inline のまま (7a と同じ)。parser では識別子の比較 (`"type"`、`"defer"`、`"await"`) で呼ぶ。

## 10. 再現手順と生データ

`tsc/internal/ast/docs/_store-parser-results/`:

| file | 内容 |
| --- | --- |
| `env.txt` | go version、commit、uptime |
| `sha256.txt` | 計測対象 source の sha256 |
| `kpc-20260922-1602.txt` | KPC 5 run (ユーザー実行) |
| `kpc-20260922-1908-s1.txt` | S1 後の KPC 5 run (ユーザー実行、§11) |
| `parse-ns.txt` | ns / B/op / allocs 5 run |
| `retained.txt` | retained 5 run (16:34、GC 2 回版) |
| `gctrace-store.txt`、`gctrace-pointer.txt` | テストバイナリを直接実行した gctrace (`go test` 経由だと go コマンド自身の GC が混じる)。16:34、GC 2 回版 |
| `allocs-store-checker.txt` | checker.ts / store の `-memprofilerate=1` の alloc_objects top と `parseExpectedWithDiagnostic` の行 |
| `parser.diff` | `diff -u internal/parser/parser.go internal/storeparser/parser.go` |
| `inline-store.txt`、`inline-parser.txt` | `-gcflags=-m=2` |
| `asm-*.txt` | §5.3 の objdump |
| `storeparser.test`、`storeparser.kperf.test` | テストバイナリ |

コマンドは検証指示 §1 / §3 / §4 / §5 のとおり。gctrace だけは `go test -c` したバイナリを `internal/storeparser` で直接実行する。

次は TODO 7c (binder)。7c の前に決めること: IPC 低下の帰属 (§6.2) を先に取るか、7c と並行にするか。7c の設計に入る前提として、`ExternalModuleIndicator` / `collectExternalModuleReferences` の実装と除外 8 file の再検証 (§3.4)、JS の JSDoc reparse ノード (binder が bind する `@typedef` / `@template` 等) の置き場、診断の file が nil である点 (§4.3 #11) を挙げておく。

## 11. 追記 (同日 19:13): `View` の除去 (S1) と IPC 低下の帰属

§6.2 の候補 (5) を単独で消して測った。実装は [store-builder-embed-implementation-instructions-20260922.md](store-builder-embed-implementation-instructions-20260922.md) (S1)。`Builder` の列 (`nodes` / `extra` / `src` / `texts` / `file`) を `View` が返す `Store` そのもの (`s Store`) にし、`View` を `b.s.node(id)` だけにした。generator は 2 行、`storeparser` は無変更、等価テスト (`storechecks` あり / なし) は無変更で通過。

### 11.1 変更前の `View` の費用

checker.ts の parse 1 回で `View` は 338,615 回 (node 298,884、Scan 362,421。overlay のカウンタで計数)。呼び出し元は parseAssignmentExpressionOrHigherWorker 135k、tryReparseOptionalChain 87k、checkJSSyntax 43k、parsePropertyAccessExpressionRest 25k、parseCallExpressionRest 18k。

`b.view = Store{...}` の複合リテラル代入は、objdump で 88B の stack temp のゼロ化、フィールド転記、`runtime.writeBarrier` の判定、`runtime.wbMove` 付きの typed copy、直後の slice header 再ロードと bounds check になる (呼び出し箇所ごとに約 50 命令。tryReparseOptionalChain は 5 箇所で `wbMove` 5 つ、`store.go:198` 帰属 200 命令)。単価はマイクロベンチで 6.1ns (直接 `b.nodes[id].kind` を読む 0.40ns の 15 倍)。回数 × 単価 ≈ 2.1ms で、pprof の flat 16.9% (≈3.5ms) と同じ桁。

Pointer parser に対応物はない (`node.Kind` は deref 1 つ)。Store の自己負担なので、消しても比較は Store に有利に歪まない。

### 11.2 KPC (checker.ts、5 run、`kpc-20260922-1908-s1.txt`)

| | pointer (7b → S1) | store 7b | store S1 | store の差 |
| --- | ---: | ---: | ---: | ---: |
| inst/op | 260.2M → 260.5M | 230.1M | 214.3M (214.29〜214.30) | −15.8M (−6.9%) |
| cycles/op | 66.77M → 67.35M | 66.51M | 54.72M (54.66〜54.81) | −11.8M (−17.7%) |
| IPC | 3.90 → 3.87 | 3.46 | 3.92 | |
| ns/op | 21.2ms | | 17.19ms | |
| Pointer 比 | | inst 0.884 / cycles 0.996 | **inst 0.823 / cycles 0.813** | |

pointer は 1% 以内で不変 (対照)。store の run 幅は inst 0.01M、cycles 0.15M。

- **IPC 低下は `View` が全部だった。** `View` だけを消して IPC が 3.46 → 3.92 になり、pointer の 3.87 と並んだ。§6.2 の候補 (1) constructor の call、(2) append 2 本の依存、(3) `adopt`、(4) `memequal` は IPC には効いていない (命令数には残っている)。
- **`View` 1 回あたり 47 inst / 35 cycles。** 単価ベンチの 19.5 cycles (6.1ns) より大きい。parser では直後に header を読むので store→load の依存が露出し、GC 中は `wbMove` が実行されるため。「マイクロベンチ単価 × 回数」は write barrier と依存連鎖を含む経路では下限見積になる。
- 7b の「命令は減るが cycles は同等」は「命令も cycles も 2 割減」になった。

### 11.3 wall (`BenchmarkStoreParseV1`、GC あり) の読み方

`--count 5` で store 18.1〜20.4ms とばらつくが、B/op と相関する。9.94MB (578 allocs) の run は Finish の copy だけで 17.3ms、10.8MB 超 (579 allocs) の run は `sync.Pool` が parser を落として scratch を cap 1024 から作り直した run で +1〜2ms。どちらになるかは GC と Pool の位相で決まり (GOGC=400 で 9.94MB 水準に揃う)、`-tags kperf` の有無は無関係。7b の変更前 (20.8ms) は 5 run とも 10.9MB 水準だったので、同じ水準で比べて 20.8 → 18.2ms (−12%)。KPC の Session は GC オフなのでこの 2 水準は出ない。

### 11.4 残る費用と次の候補

S1 後の Store parse の内訳 (S1 前の pprof、focus ParseSourceFile、`View` を除いた比率): scanner 約 35% (`Scan` cum。うち `scanASCIIWhile` 18%、`GetIdentifierToken` の map 5.4%)、ノード構築 (`header` / `adopt` / `List` / `identifierData`) 約 8%、`Finish` の Compact 3.8%、残りは parser の構造。

**Store 固有** (Pointer に対応物なし): `identifierData` の `b.src[start:end] == text` (識別子ごとの byte 比較、pointer 同一性で O(1) にできる、1.5%)、`Builder.List` の 1 要素ずつの append (1.3%)。合わせて 3% 弱で、KPC の幅から見て測れる下限に近い。`Finish` は RSS のための設計判断で対象外。

**Pointer と共通** (両方に入れるか、入れないか。Store だけに入れると比較が歪む): `scanASCIIWhile` の識別子述語のテーブル化。現状は closure が inline 済みでも小文字 1 byte あたり 11 命令・条件分岐 4 本 (`>= 0x80`、a-z、A-Z、0-9、`_`、`$` の順に脱落) で、大文字混じりの識別子で分岐パターンが変わり mispredict する。`[256]bool` の package 変数にすると、述語の比較 4〜7 本が `MOVBU` 1 load になり、`>= 0x80` の判定は 0x80 以上を false にしたテーブルに吸収され、添字が byte なので bounds check も出ない。1 byte あたりの分岐は「ループ継続」と「テーブルの真偽」の 2 本になる。

```go
var identPart = func() (t [256]bool) {
	for b := 0; b < 128; b++ {
		c := byte(b)
		t[b] = (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '$'
	}
	return
}()

for i < len(t) && identPart[t[i]] {
	i++
}
```

`scanASCIIWhile` は closure を受ける汎用関数なので、テーブル版は述語ごと (識別子の続き、改行後の空白、行コメント) に配列を 1 つずつ持つ。package 変数として init 時に一度作るだけで、実行時の生成費用はない。マイクロベンチ (checker.ts の識別子 227k、平均 8.6 byte、scratchpad の scanbench): 現状 2.30 ns/byte・19.7 ns/識別子 → テーブル 1.59 ns/byte・13.7 ns/識別子 (−30%)。`(b|0x20)-'a' < 26` の範囲畳み込みは −25%、128-bit mask 2 word は −18%。Rust (oxc) は LLVM が a-z / A-Z を `and 0xdf; sub 65; cmp 26` に畳んで `ccmp` で分岐無しに繋ぐが、Go の ssa はこの畳み込みをしない。テーブル化後の 13.7 ns/識別子 ≈ 44 cycles は識別子末尾の exit mispredict (≈15 cycles) と呼び出し固定費が支配で、述語をこれ以上削っても効かない。実 parse での期待は `scanASCIIWhile` flat 分の 1/3、parse 全体の −5〜7%。`scanner` は Pointer / Store 共通なので、入れれば両方が同じだけ縮む。

