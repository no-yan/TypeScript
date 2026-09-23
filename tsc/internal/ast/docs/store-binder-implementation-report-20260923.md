# Store binder (TODO 7c) 実装報告

作成日: 2026-09-23。指示書は [store-binder-implementation-instructions-20260922.md](store-binder-implementation-instructions-20260922.md)、設計は [store-binder-design-20260922.md](store-binder-design-20260922.md)。生の出力と diff は `_store-binder-results/` (`binder.diff` = Pointer → まとめ版、`binder-verbatim.diff` = Pointer → 逐語版、`binder-fold.diff` = 逐語版 → まとめ版、`references.diff`、`symbol.diff`)。KPC は取っていない (sudo が要る。検証指示 §4 のコマンドをそのまま使える)。

## 0. 結論

- corpus 17,415 file のうち 17,085 file で bind の等価 (flags、symbol、locals、flow graph、file の field) は mismatch 0。除外は JS 330 file (§5)。parse の等価 (7b) は 32B header + reparse 有効 + indicator ありで mismatch 0、await 8 file の除外を外した。
- テストは tag なしと `-tags storechecks` の両方で store / storeparser / storebinder が通る。ast / parser / binder のテストも通る。`internal/binder` / `parser` / `scanner` / `ast` (store 以外) に差分なし。production から store 系 package の import なし。generator は再現する。
- 指示書に無い判断が 4 つ出た (§9)。いずれも実装は「報告に残す」側で進めた: JS の除外規則の拡張 (293 → 330)、`Bound` の slab の append 成長による B/op 2 倍、`kperf.Session.Totals()` の追加、`store.Node.Text()` の JsxNamespacedName 欠落の binder 側での補い。

## 1. 分類 (a)〜(g) の hunk 数

`binder-verbatim.diff` は 64 hunk (3,119 行)。hunk は近い変更を併合するので、分類は hunk 内の書き換えの主成分で数えた。

| 分類 | hunk | 内容 |
| --- | ---: | --- |
| (a) 型の置換 | 24 | `*ast.Node` → `store.Node`、`== nil` → `IsNil()`、flow 系 → `FlowRef` / `FlowListRef`、`[]*ast.Node` → `[]store.NodeRef` / `[]store.Ref`、ast の述語 → `store.` の同名 |
| (b) 出力先の置換 | 14 | `SetFlow` / `SetSymbol` / `AddFlags` / `ClearFlags`、役割 setter、`store.GetLocals(b.bound, …)`、`b.symbolOf`、`b.bound.*`、`FileRef()` |
| (c) 子の受け直し | 15 | `stmt.Expression` → `stmt.Expression()`、`.Nodes` → `.Refs()` + `b.s.Node(ref)`、`Arguments()[0]` → `At(0)` |
| (d) flow の index 化 | 8 | `newFlowNode*`、`addAntecedent`、`finishFlowLabel`、`combineFlowLists`、label の書き込み、`newFlowData` |
| (e) JSDoc 系の削除 | 0 | `KindJSTypeAliasDeclaration`、`IsImplicitlyExportedJSDocDeclaration`、`NodeFlagsJSDoc` の分岐は残した |
| (f) それ以外 | 3 (+ 他 hunk 内の 40 箇所、§2) | |
| (g) 読みのまとめ | 41 hunk (`binder-fold.diff`、674 行)、§3 | |

関数の名前と順序は Pointer と同じ。増えたのは `nilNode`、`symbolOf` (helper)、`newFlowData` (`ast.NewFlow*Data` の代替) の 3 つで、`setFlowNodeReferenced` と `getInitializerSymbol` は `Bound` / `Store` を読むため method になった。

## 2. 分類 (f) の一覧 (header の読み回数: Pointer / Store)

「header の読み」は kind / flags / pos / end / parent / data の load。Pointer 側の型 switch (`DeclarationData()` 等) は kind 相当の 1 読みと数えた。`node()` は index → pointer の 2 命令で header を読まない。

| # | 箇所 | 内容 | Pointer | Store |
| --- | --- | --- | --- | --- |
| 1 | `bindSourceFile` | container 4 本を番兵 Node で初期化 (零値 `Node{}` は `IsNil()` で落ちる) | 0 | 0 |
| 2 | `getDeclarationName` | pattern 名の `GetNodeId(attributes)` → `attributes.FileRef()` (`file:id` の 10 進) | atomic 1 (+CAS) | 0 (pointer 演算) |
| 3 | `getDeclarationName` | `IsJsxNamespacedName(name)` の枝を分け `jsxNamespacedNameText(name)` (`store.Node.Text()` に JsxNamespacedName の case が無い) | kind 3 | kind 1 + payload 2 + identifierText 2 |
| 4 | `getDisplayName` / `checkContextualIdentifier` / `checkPrivateIdentifier` | `scanner.DeclarationNameToString(node)` → `declarationNameToString(b.file, node)` | parent 連鎖 d 段 (kind d) + pos/end | pos/end のみ |
| 5 | `errorOnFirstToken` / `createDiagnosticForNode` | scanner ヘルパの Store 版 (`getRangeOfTokenAtPosition` / `getErrorRangeForNode`)。ArrowFunction は行頭表を毎回計算、SatisfiesExpression の JSDoc tag 探索は無し、diagnostic の file は nil | kind 1 + Name 1 + pos/end | 同じ (Name は表引き) |
| 6 | `isUseStrictPrologueDirective` / `FindUseStrictPrologue` | `*store.File` を取る。`GetSourceTextOfNodeFromSourceFile` の Store 版は JSDoc / reparser の枝なし | pos/end | pos/end |
| 7 | `bindParameter` | `slices.Index(node.Parent.Parameters(), node)` → `slices.Index(parent.Parameters().Refs(), node.Ref())` | parent 1 + data 型 switch 1 | parent 1 + kind 表 1 + extra 4 |
| 8 | `getThisClassAndSymbolTable` | `thisContainer.Parent.Symbol()` → `b.symbolOf(b.thisContainer.Parent())` | parent 1 + 型 switch 1 | parent 1 + symbol 1 + slab 1 |
| 9 | `SetValueDeclaration(s, symbol, node)` | `*store.Store` を取り `ValueDeclaration` の Ref を 1 回解決 | kind ≤3 | kind ≤3 |
| 10 | `isFunctionSymbol(s, symbol)` | 同上 (binder.go 内で未使用の関数、Pointer と同じく残した) | | |
| 11 | `getInitializerSymbol` | method 化 (`ValueDeclaration` の Ref を `b.s.Node` で解決) | kind 1〜3 | 同じ + node() 1 |
| 12 | `setFlowNodeReferenced` | method 化 (slab を読む) | 0 | 0 |
| 13 | `newFlowNodeEx(flags, node NodeRef, antecedent)` | `NodeRef` を取る (合成 flow の FlowData index と兼用)。呼び手で `Ref()` | 0 | 0 (+shift 1) |
| 14 | `addToContainerChain` | `NextContainer` の書き込み → `b.bound.AddContainer(next.Ref())` | 型 switch 1 | 0 |
| 15 | `ast.GetLocals(container)` → `store.GetLocals(b.bound, container)` | slot 経由。初回は `NewLocals` + `SetLocalsSlot` | 型 switch 1 | kind 表 1 + extra 1 (初回 +kind 表 1 + extra 書き 1) |
| 16 | `lookupName` | `LocalsContainerData()` / `DeclarationData()` → `LocalsSlot()` / `symbolOf` | 型 switch 2 | kind 表 1 + extra 1 + symbol 1 |
| 17 | `IsLocalsContainer` | `LocalsSlot()` の ok | 型 switch 1 | kind 表 1 |
| 18 | `bindContainer` | `BodyData()` + `.Body` → `node.Body()`、`bodyData.EndFlowNode =` → `SetEndFlowNode` | 型 switch 1 + field | kind 表 2 + extra 2 |
| 19 | `setReturnFlowNode` | kind switch → 役割 setter | kind 1 | kind 表 1 + extra 1 |
| 20 | `bindCaseBlock` | `clause.AsCaseOrDefaultClause().SetFallthroughFlowNode` (typed setter) | 0 | 0 |
| 21 | `bindChildren` (unreachable) | `FlowNodeData() != nil` ガード付き `= nil` → `SetFlow(0)` 無条件 (全 kind に flow word) | 型 switch 1 | 0 |
| 22 | `hasExportDeclarations` / `bindSwitchStatement` の `core.Some` | `Refs()` ループ + `b.s.Node(ref)` | 要素ごと kind 1〜2 | 同じ + node() |
| 23 | `hasNarrowableArgument` | `Arguments().At(i)` ループ (free 関数で `Store` が無い) | 0 | `Refs()` 3 読み × 要素数 |
| 24 | `bindEach` / `bindNodeList` / `bindModifiers` / `bindEachStatementFunctionsFirst` / `bindAssignmentTargetFlow` / `bindInitializedVariableFlow` | `[]*Node` → `Refs()` + `b.s.Node(ref)` | 要素ごと kind 1 | 同じ + node() |
| 25 | `getParentOfPropertyAssignment` | `Arguments()[0]` → `At(0)` | 型 switch 1 | kind 表 1 + extra 3 |
| 26 | `declareSymbolEx` / `bindClassLikeDeclaration` | `Declarations` の Ref を `b.s.Node(ref.Id())` で解決 | 0 | 0 (+node()) |
| 27 | `bindSourceFileIfExternalModule` (JSON) | `b.file.Symbol` の一時上書き → root header の `Symbol()` / `SetSymbol` | field 1 | header 1 |
| 28 | `setCommonJSModuleIndicator` | `!= b.file.AsNode()` → `!= b.file.Root().Ref()` | 0 | 0 |
| 29 | `store.SymbolName(b.s, symbol)` / `IsStatic(node)` / `(*Symbol).IsStatic(st)` | `ValueDeclaration` の Ref を解決 | kind 1〜3 + Name 1 | 同じ |
| 30 | `IsModuleAugmentationExternal(b.file, node)` / `IsImplicitlyExportedJSDocDeclaration(b.file, node)` / `IsJsonSourceFile(b.file)` | `node.Parent.AsSourceFile()` の代わりに `*File` を取る | parent 1 + kind 1 | parent 1 + kind 1 |
| 31 | `bindModuleDeclaration` | `PatternAmbientModules` は値型 (`&ast.PatternAmbientModule{}` の alloc 無し) | 0 | 0 |
| 32 | `combineFlowLists` / `addAntecedent` / `bindTryStatement` | slab の pointer を append の前に手放す (2 文に分ける) | 0 | 0 |
| 33 | `store/utilities.go` の `IsOuterExpression` | `OEKExcludeJSDocTypeAssertion` の枝なし (Store に JSDoc が無い) | | |
| 34 | `store/utilities.go` の `isVariableDeclarationInitializedWithRequireHelper` | `allowAccessedRequire` (呼び手なし) は panic | | |
| 35 | `store/utilities.go` の `GetModuleInstanceState` 系 | `map[NodeId]` → `map[NodeRef]` (呼び出し中だけの一時 map、invariant 13 の対象外)、ancestors は `[]Node` | | |
| 36 | `store/utilities.go` の `IsRequireCall` / `IsBindableObjectDefinePropertyCall` | `IsIdentifier` 判定後の `.Text()` を `.AsIdentifier().Text()` に (役割 `Text()` は inline されない、生成コメント参照) | | (g) と同時に |
| 37 | `storebinder/utilities.go` の `isLineBreak` | `stringutil` は import 不可 (§1 のガード) なので 4 文字の局所版 | | |
| 38 | `IsInJSFile(node)` | 各 node の flags を読む (Pointer と同じ。指示書の「root の flags」ではない: contextFlags に JavaScriptFile が乗るので全 node にある) | flags 1 | flags 1 |
| 39 | `bind()` の `node == nil` | `IsNil()` は kind の読み | 0 | kind 1 |
| 40 | `references.go` の `forEachDynamicImportOrRequireCall` / `findImportOrRequire` | `ast` の unexported 関数の写し、`GetNodeAtPosition` は JSDoc 引数なし | | |

Pointer にも効きそうな書き換えは入れていない。`checkContextualIdentifier` の条件順は据え置き。

## 3. 分類 (g) の一覧

`binder-fold.diff`。まとめたのは parse 後不変の accessor (kind、Parent、子、Text、slot 定数) だけで、`Flags()` / `Symbol()` / `FlowNode()` / `LocalsSlot()` / file と Bound の field はその場で読む。

binder.go (38 関数):

- `bind`: `kind := node.Kind()` (switch、ThisKeyword、`> LastToken` の 3 読み → 1)
- `bindChildren`: `kind` (範囲判定 2 + switch 1 → 1)
- `bindContainer`: `kind` (ClassStaticBlock / Constructor ×2 / SourceFile ×3 → 1)
- `GetContainerFlags` (Block): `parent`
- `bindVariableDeclarationOrBindingElement`: `name`
- `bindParameter`: `name`、`parent`
- `bindForInOrForOfStatement`: `initializer`
- `bindBinaryExpressionFlow`: `left`、`right`
- `bindLogicalLikeExpression`: `left`、`operatorToken`、`operator`、`right`
- `bindDestructuringAssignmentFlow`: `left`、`typeNode`、`operatorToken`、`right`
- `bindCallExpressionFlow`: `expression`、`exprKind`、`name`
- `bindTryStatement`: `catchClause`、`finallyBlock`
- `bindSwitchStatement`: `caseBlock`
- `bindCaseBlock`: `switchExpression`
- `bindCaseOrDefaultClause` / `bindExpressionStatement` / `bindDeleteExpressionFlow` / `maybeBindExpressionFlowIfCall` / `checkStrictModeDeleteExpression` / `lookupEntity`: `expression`
- `bindLabeledStatement`: `label`
- `bindPrefixUnaryExpressionFlow` / `bindPostfixUnaryExpressionFlow` / `checkStrictModePrefixUnaryExpression`: `operator`
- `hasNarrowableArgument`: `expression`
- `isStatementCondition` / `isTopLevelLogicalExpression` / `setContinueTarget`: `parent`
- `bindModuleDeclaration`: `nameText`
- `bindExportDeclaration`: `exportClause`
- `bindFunctionExpression`: `name` (if を入れ子に)
- `bindThisPropertyAssignment`: `left`、`kind` (thisContainer の kind)
- `checkStrictModeCatchClause`: `variableDeclaration`
- `checkStrictModeLabeledStatement`: `statement`
- `checkStrictModeBinaryExpression`: `left`

store/utilities.go (8 関数、同じ規則): `IsObjectLiteralOrClassExpressionMethodOrAccessor` (`parentKind`)、`IsExpressionOfOptionalChainRoot` (`parent`)、`IsModuleAugmentationExternal` (`parent`)、`GetElementOrPropertyAccessName` (`name`)、`GetAssignmentDeclarationKind` (`left`、`expression`、`leftKind`: `bin.Left()` 8 回 → 1)、`IsBindableObjectDefinePropertyCall` / `IsRequireCall` (`expression`、`arguments`)、`IsPotentiallyExecutableNode` (`kind`)、`getModuleInstanceStateWorker` (`exportClause`)。

## 4. `store` に足した API

§3 の一覧どおり: `NodeHeader` の `flow` / `symbol` (32B)、`Node.FlowNode` / `Symbol` / `AddFlags` / `ClearFlags` / `SetFlow` / `SetSymbol` / `FileRef`、`Ref.File` / `Id`、`Store.Node` / `Seal` (`sealed bool`)、`Builder.SetFlags` / `SetLoc` の削除、`bound.go` (`FlowRef` / `FlowNode` 16B / `FlowListRef` / `FlowList` 8B / `FlowData` / `Bound` と読み書き API)、`symbol.go` (`SymbolId` / `Symbol` 96B / `SymbolTable` / `PatternAmbientModule` / `SymbolName` / `Escape*` / `GetSymbolTable` / `GetMembers` / `GetExports` / `GetLocals`)、`file.go` の field と `IsBound` / `BindOnce` / `Symbol` / `IsExternalModule` / `IsExternalOrCommonJSModule`、`position.go` の `GetNodeAtPosition(file, pos)`、`utilities.go` (§6)、`storetest.Pairs`、generator の `bound` slot。

§3 に無いもの:

| API | 理由 |
| --- | --- |
| `store.NewBound()` | 番兵行 (`[0]`) を持つ `Bound` を作る。binder が bind の先頭で `file.Bound = store.NewBound()` する |
| `(*Bound).FlowCounts()` / `Footprint()` | テスト (§6.4 の slab の個数) と retained の照合用。`inspect.go` の `Footprint` と同じ性格 |
| `(*Symbol).IsStatic(st *Store)`、`SymbolName(st, symbol)` | 指示書 §3.2 のとおり `*Store` を取る |
| `GetLocals(bound *Bound, container Node)` | §5.2 の `b.getLocals` を store 側に置いた (binder は `store.GetLocals(b.bound, container)` と呼び、`ast.GetLocals(container)` との diff が小さい) |
| `IsModuleAugmentationExternal(file, node)`、`IsImplicitlyExportedJSDocDeclaration(file, node)`、`IsJsonSourceFile(file)` | `Node` から `*File` に辿れないので file を取る |
| `Node.nilNode()` (非公開) | utilities.go の中で nil を返すため |
| `kperf.Session.Totals()` | Bind KPC ベンチが cycles/node と inst/node を `ReportMetric` するのに総和が要る。`testutil/kperf` はガードの対象外だが §3 の一覧外なので記す |

`unsafe` は `Ref()` と `Refs()` だけ。`Node.Ref()` は `LSR $5` (乗算なし、`createFlowMutation` に inline された命令列で確認)。`storeChecks` 無しの build で setter の `sealed` 読みは無い (`checkUnsealed` は cost 0 で inline)。役割 accessor / setter、`Store.Node`、`Bound.Flow` / `FlowList` / `SymbolOf` / `Locals` / `NewFlow` / `NewFlowList` は can inline。`GetLocals` は cost 181 で inline されない (宣言ごとに 1 回)。

## 5. generator の出力差分

`generate-go-store.ts` +142/−9: slot class `bound` (`boundSlots` 表: member 名 → accessor 名と Go 型)、base 連鎖の goOnly field を拾う `baseFields`、`bound` slot を宣言の末尾に置く、`boundRoles` (5 本の `[512]uint8` 表 `localsSlot` / `localSymbolSlot` / `endFlowNodeSlot` / `returnFlowNodeSlot` / `fallthroughFlowNodeSlot`)、typed view の getter / setter、`Node` の役割 accessor (`LocalsSlot() (uint32, bool)` は表引き + 比較 1 回で不在を返す) と setter (不在 kind は no-op)、constructor は 0 埋め、convert は引数なし、equivalence は比較しない。

生成物の差分は `shapes_generated.go` (+151)、`views_generated.go` (+650)、`builder_generated.go` (74 行の書き換え) だけ。`convert_generated.go` と `equivalence_generated.go` は不変。

| slot | 定義数 | kind 数 |
| --- | ---: | ---: |
| Locals | 27 (直接 13 + `FunctionLikeBase` / `ClassLikeBase` 経由) | 29 |
| LocalSymbol | 14 (指示書は 13) | 15 |
| EndFlowNode | 8 (`BodyBase` 2 + `FunctionLikeWithBodyBase` 6) | 8 |
| ReturnFlowNode | 4 | 4 |
| FallthroughFlowNode | 1 (`CaseOrDefaultClause`) | 2 |
| いずれかを持つ kind | | 40 |

## 6. `store/utilities.go` に移植した関数 (165)

`internal/ast/utilities.go` の順: NodeIsMissing、NodeIsPresent、NodeKindIs、IsAssignmentExpression、IsDestructuringAssignment、IsBindingPattern、IsAssignmentTarget、GetAssignmentTarget、IsLogicalOrCoalescingBinaryExpression、IsLogicalOrCoalescingAssignmentExpression、IsLogicalExpression、IsPropertyNameLiteral、IsIdentifierName、IsPushOrUnshiftIdentifier、IsBooleanLiteral、IsStringLiteralLike、IsStringOrNumericLiteralLike、IsSignedNumericLiteral、IsOptionalChain、getQuestionDotToken、IsOptionalChainRoot、IsOutermostOptionalChain、IsExpressionOfOptionalChainRoot、IsNullishCoalesce、isLeftHandSideExpressionKind、IsLeftHandSideExpression、IsAccessExpression、isFunctionLikeDeclarationKind、IsFunctionLikeKind、IsFunctionLike、IsClassLike、IsClassElement、IsMethodOrAccessor、IsPrivateIdentifierClassElementDeclaration、IsObjectLiteralOrClassExpressionMethodOrAccessor、IsObjectLiteralMethod、IsAutoAccessorPropertyDeclaration、IsParameterPropertyDeclaration、isDeclarationStatementKind、IsDeclarationStatement、IsBlockOrCatchScoped、IsCatchClauseVariableDeclarationOrBindingElement、IsPrologueDirective、IsOuterExpression、SkipOuterExpressions、SkipParentheses、SkipPartiallyEmittedExpressions、FindAncestor、HasSyntacticModifier、HasAccessorModifier、HasStaticModifier、IsStatic、IsFunctionExpressionOrArrowFunction、GetRootDeclaration、GetCombinedModifierFlags、GetCombinedNodeFlags、IsImportMeta、IsInJSFile、IsLiteralImportTypeNode、IsExportsIdentifier、IsModuleIdentifier、IsBindableStaticAccessExpression、IsBindableStaticElementAccessExpression、IsLiteralLikeElementAccess、IsBindableStaticNameExpression、GetElementOrPropertyAccessName、GetNameOfDeclaration、GetNonAssignedNameOfDeclaration、GetAssignedName、GetAssignmentDeclarationKind、IsBindableObjectDefinePropertyCall、HasDynamicName、IsDynamicName、IsEntityNameExpression、IsEntityNameExpressionEx、IsPropertyAccessEntityNameExpression、isElementAccessEntityNameExpression、IsDottedName、IsAmbientModule、IsGlobalScopeAugmentation、IsModuleAugmentationExternal (file)、GetContainingClass、IsPartOfTypeQuery、IsPartOfParameterDeclaration、IsInTopLevelContext、GetThisContainer、GetImmediatelyInvokedFunctionExpression、IsEnumConst、ExpressionIsAlias、IsAnyImportOrReExport、IsImportNode、IsAnyImportSyntax、IsJsonSourceFile (file)、GetExternalModuleName、getImportTypeNodeLiteral、IsImportCall、pushAncestor、popAncestor、GetModuleInstanceState、getModuleInstanceState、getModuleInstanceStateCached、getModuleInstanceStateWorker、getModuleInstanceStateForAliasTarget、NodeHasName、ModuleExportNameIsDefault、IsRequireCall、IsVariableDeclarationInitializedToRequire、isVariableDeclarationInitializedWithRequireHelper、IsModuleExportsAccessExpression、IsJsxOpeningLikeElement、IsExpandoInitializer、IsImplicitlyExportedJSDocDeclaration (file)、IsPotentiallyExecutableNode、IsAsyncFunction、IsLocalsContainer (`ast.go`)。それに `Kind() == …` の 1 行述語 47 本 (IsIdentifier 〜 IsSourceFile、IsForInOrOfStatement)。`IsExternalModule` / `IsExternalOrCommonJSModule` は `File` の method。

`storebinder/utilities.go`: getScannerForSourceFile、getRangeOfTokenAtPosition、getErrorRangeForArrowFunction、getECMAEndLinePosition、isLineBreak、getErrorRangeForNode、getSourceTextOfNodeFromSourceFile、getTextOfNodeFromSourceText、declarationNameToString、jsxNamespacedNameText。

`storeparser/references.go`: collectExternalModuleReferences、collectModuleReferences、findImportOrRequire、forEachDynamicImportOrRequireCall、setExternalModuleIndicator、getExternalModuleIndicator、isFileProbablyExternalModule、isAnExternalModuleIndicatorNode、getImportMetaIfNecessary、findChildNode、isFileModuleFromUsingJSXTag、walkTreeForJSXTags (SubtreeFacts の枝刈り無しで全走査。コメントに書いた)。

## 7. JS の除外と診断

除外は `reparsedEffects` (bindequiv_test.go) の理由で数える (`TestCorpus` が理由ごとの file 番号を log する):

| 理由 | file 数 |
| --- | ---: |
| Reparsed かつ symbol を持つノード (設計 §2.1 の規則) | 293 |
| Reparsed ノードの下に symbol (`@import` の specifier: flag は宣言に、symbol は子に付く) | 57 |
| expando / require の initializer を持つ宣言の Reparsed 型注釈 (`IsExpandoInitializer` と `IsVariableDeclarationInitializedToRequire` が `Type()` を読む) | 11 |
| JSDoc の型 cast (`/** @type {T} */ (e)`: Store に無い AsExpression が narrowing を止める) | 48 |
| いずれか (除外の合計) | **330** |
| その他の Reparsed ノード (除外しない) | 638 |

設計 §2.1 の規則 (293) のままだと 37 file が構造的に一致しない (Store に無いノードの symbol / 型 / cast に bind が依存する)。規則の拡張は §9 の判断 1。

診断: bind diagnostics は (pos, len, code, message) の列で完全一致 (17,085 file)。parse diagnostics は 7b と同じく JS 10 file で Store 側が subset (JSDoc 由来の code 1003)。

## 8. flow slab と retained

| fixture | flows | flowLists | flowData | Pointer の到達可能数 | Bound の slab bytes |
| --- | ---: | ---: | ---: | --- | ---: |
| checker.ts | 79,991 | 49,789 | 879 | 45,694 / 15,402 / 878 | 1,925,676 |
| dom.generated.d.ts | 4,846 | 1 | 1 | 4,841 / 0 / 0 | 329,888 |

(番兵込み。差は到達不能な label と、`finishFlowLabel` で 1 antecedent に畳まれた label / list。)

`BenchmarkStoreBindRetainedV1` (1 run、参考値、検証が count 5 で取り直す):

| 入力 | Pointer B/node | Store B/node | 比 | GC ms Pointer / Store | 比 |
| --- | ---: | ---: | ---: | --- | ---: |
| fixtures (166 file) | 124.6 | 72.6 | 0.58 | 13.5 / 2.68 | 0.20 |
| checker.ts | 109.4 | 61.4 | 0.56 | 4.47 / 1.14 | 0.25 |
| dom | 128.4 | 89.4 | 0.70 | 1.83 / 1.10 | 0.60 |

`BenchmarkStoreBindV1/checker.ts` (2s × 5、KPC の代替の粗い比): Pointer 11.74〜12.68 ms、Store 11.26〜11.37 ms (wall 0.96)、B/op 7.43 MB / 14.22 MB、allocs 13,954 / 12,756。B/op の 2 倍は `Bound.flows` / `flowLists` の append 成長 (256 要素を超えると 1.25 倍なので総 alloc は最終長の約 5 倍。pprof: `NewFlow` 7.2 MB + `NewFlowList` 2.0 MB / bind)。Pointer の `core.Arena` は chunk を足すだけで copy しない。§9 の判断 2。

## 9. 指示書に無かった判断 (ユーザーの決定待ち)

1. **JS の除外規則**: 設計 §2.1 の 293 file に加えて 37 file (§7 の 3 理由) を除外した。理由は「Store に無いノードに bind が依存する」で、規則は `reparsedEffects` に機械的に書いてある。設計 §2.1 の「JSDoc 由来は 2 つ」は `@import`、`@type` (expando / require)、JSDoc cast を見落としている。据え置く (37 file を mismatch として残す) か、この規則を設計に反映するか。
2. **slab の成長**: `Bound.flows` / `flowLists` を append で伸ばすと B/op が Pointer の 2 倍 (時間は 0.96 で影響なし)。候補は (a) `NodeCount` からの事前確保 (dom は flow が node の 4% なので過剰確保が retained に乗る)、(b) `core.Arena` 型の chunk (`Flow(r)` が 2 load になる。設計 §2.4 が symbol で退けた形)、(c) `Seal` で exact に詰め直す (churn は残る、retained の slack は消える)。いずれも未実装。
3. **`kperf.Session.Totals()`**: §3 の一覧外の package に 3 行足した。
4. **`store.Node.Text()` の欠落**: Pointer の `Text()` にある JsxNamespacedName (`ns:name`) と MetaProperty (`Name().Text()`) の case が生成 `Text()` に無い。binder 側で `jsxNamespacedNameText` を分け、`IsImportCall` は `AsMetaProperty().Name().Text()` を読む。生成側に足すなら役割 `Text()` の分岐が増える (設計文書 §2.3 の inline 予算の話)。

## 10. 移植しなかったもの、気づいたこと

- 移植しなかった関数: binder.go は全関数を移植した (未使用の `isFunctionSymbol`、`getStrictModeBlockScopeFunctionDeclarationMessage` も)。nameresolver.go / referenceresolver.go は範囲外。`ast` 側で落とした枝: `IsOuterExpression` の JSDoc type assertion 除外、`isVariableDeclarationInitializedWithRequireHelper` の `allowAccessedRequire`、scanner ヘルパの JSDoc / reparser / SatisfiesExpression tag の枝、`GetNodeAtPosition` の JSDoc 降下。
- Pointer binder のバグ: 見つけていない。気づいたこと: `isFunctionSymbol` と `getStrictModeBlockScopeFunctionDeclarationMessage` は binder.go 内で呼ばれていない (dead code)。`IsInJSFile` の指示書の注 (「root の flags」) は Pointer の実装 (各 node の flags) と違うが、contextFlags が全 node に JavaScriptFile を乗せるので結果は同じ。
- `getErrorRangeForArrowFunction` は診断のたびに行頭表を計算する (Pointer は file の cache)。ArrowFunction への bind 診断は稀なので据え置いた。
- KPC 3 本 (Walk / Parse / Bind) は未計測 (sudo)。`go test -tags kperf -c` は通る。

## 11. 再現

```sh
cd $TSC/.. && node tools/scripts/tsc/generate.ts && git status --short
cd $TSC && go build ./... && go vet ./internal/ast/... ./internal/storeparser/... ./internal/storebinder/...
cd $TSC && go test -count=1 ./internal/ast/store/... ./internal/storeparser/... ./internal/storebinder/...
cd $TSC && go test -tags storechecks -count=1 ./internal/ast/store/... ./internal/storeparser/... ./internal/storebinder/...
cd $TSC && go test -run '^$' -bench 'StoreBindV1|StoreBindRetainedV1' -benchtime 1x ./internal/storebinder/
```
