# Binderで残るAST再解決の一覧（2026-09-10）

現在の整数一括取得版を対象にしたsource監査。新しいbenchmark・コード変更は行っていない。
優先順は **Binary/Call → Parameterなどの手書き走査 → 宣言名の受渡し → list共通経路**。
同じ値の再取得と、別の値を読むために同じheaderを再解決する費用を区別する。

## 対象の考え方

- **値の再取得**: 同じref・slot・kindを再取得する。取得済みの整数やbindNodeを渡す候補。
- **位置の再解決**: 同じ親の別slot、同じlistの別要素など。値自体は必要だが、header/start/ownerは共有できる。
- **可変値のfresh read**: Flags/Symbol/Flow等。古い値へ置換せず、必要なら解決済みの位置だけ共有する。

同じhelper呼出し自体はpointer版にも存在し得る。Store化で加わったindex→header→slot→子header等の解決を省くことが主対象。
判定結果のmemo、文字列名のcache、2pass→1passなどをこの一覧の実装方針に含めない。
以下の「渡す」は短い呼出し経路に限り、全node cacheや大きなcontextを作る意味ではない。
構文参照の先取りはBind中にその構文が変わらないことが前提。

## 優先してまとめて書き換えられる箇所

### 1. BinaryExpressionの判定からFlow処理まで【値＋位置】

入口: [bindBinaryExpressionKind](../binder.go:1560)、[bindChildrenRef](../binder.go:1905)、[bindBinaryExpressionFlowRef](../binder.go:2739)。

`bindKind → bindBinaryExpressionKind`でoperator(slot 2)とleft(slot 0)を取得。
その後、同じnodeの`bindChildrenRef → isDestructuringAssignmentRef`でoperatorと条件付きでleftを再取得。
さらに`bindBinaryExpressionFlowRef → binaryOperatorKindAt`でoperatorを再取得する。
非logical経路はslot 0〜3をそれぞれ読み、operatorのbindでslot 2のkindも再取得する。
logical経路は`bindLogicalLikeExpressionRef`がleft/operator/rightを再取得する。

変更単位: Binaryの4 child refsと必要なchild kindを一度取得し、意味判定・destructuring判定・Flow分岐へ渡す。
大きなbindKind全体に全node用contextを増やすより、Binary専用の呼出経路をまとめる。
`binaryOperatorKindAt`単体だけの改修では、後段との重複が残る。

頻度の旧caller参考値（checker / dom）: operator ChildRef 49,579 / 0、Flow直下ChildRef 40,904 / 0、logical直下18,897 / 0。
これらは重複readの確定数でも、互いに独立した高速化量でもない。

### 2. CallExpressionのcalleeとkind【値＋位置】

[bindCallExpressionFlowRef](../binder.go:2998)。
非optional経路で`exprRef := expressionRefGenerated(ref, kind)`を取得するが、後半で同じexpressionを再取得して`b.at`する。
通常経路の生成walkerも同じcallee slotを読み、子kindを読む。
`skipParenthesesRef(exprRef)`の後のkind取得、super判定の`KindAt(exprRef)`、末尾の`b.at`にも同一nodeの再取得が含まれる。

変更単位: calleeのbindNodeと、括弧を除去したcalleeのbindNodeを別々に保持し、訪問と後続判定へ渡す。
括弧除去前後は別nodeの場合があるので混同しない。IIFEのarguments先行bind順序は維持する。
旧caller参考: この関数直下のKindAt 36,703 / 0。

### 3. skipParenthesesの返却情報【値、局所で明確】

[skipParenthesesRef](../binder.go:2620)、[maybeBindExpressionFlowIfCallN](../binder.go:2606)、上記Call。
loopを終了させた`KindAt(ref)`の値を捨て、callerが直後に同じrefのKindAtを実行する。

変更単位: refだけでなく最後のkindも返す。既知の入力kindがあるcallerはそれも引数に渡す。
初回に読むべきkindや、次の別nodeのkindまで「無駄」として消さない。
旧caller参考: skip内部KindAt 20,974 / 0。全呼出しが直後にkindを再取得するとは限らない。

### 4. Parameter / BindingElementの手書き走査【位置＋前後の値】

[bindParameterFlowRef](../binder.go:3056)、[bindBindingElementFlowRef](../binder.go:3044)。
Parameterでは同じ親からslot 0、questionToken、type、initializer、nameを個別getterで取得。
BindingElementもslot 0/1、initializer、nameを別々に取得する。
一般の生成walkerに入らない専用経路なので、今回のSyntaxChildren一括化後も残っている。
さらに`bindParameterRef`や`bindVariableDeclarationOrBindingElementRef`で取得済みのnameを後で再取得する。

変更単位: まず専用Flow関数内をschemaに沿った一括取得へ変更。nameの前段からの受渡しは次の宣言経路改修と接続する。
modifiers listは別descriptorとして取得し、初期化式とnameのbind順序はそのままにする。
旧caller参考: ParameterFlow直下ChildRef 5,788 / 8,446。これはslot 0だけで、生成helper内のreadを含まない。

### 5. 宣言名を複数helperで取り直す経路【値＋位置】

[bindParameterRef](../binder.go:1363)、[bindVariableDeclarationOrBindingElementRef](../binder.go:1290)、
[bindPropertyOrMethodOrAccessorRef](../binder.go:1084)、[declareSymbolRef](../binder.go:332)、[nameOfDeclarationRef](../binder.go:587)。

Parameter/Variableで既に得たnameRef/nameKindが、宣言workerへ渡らない。
`hasDynamicNameRef → nameOfDeclarationRef`と、`getDeclarationNameRef → nameOfDeclarationRef`が同じnameを解決する。
Property/Methodもdynamic-name判定後にdeclare経路が同じnameを読み直す。
export経路では[declareModuleMemberRef](../binder.go:639)がlocal/exportの2回declareを行い、それぞれ同じ構文情報を解決する。

変更単位: nameRef/nameKindを一度解決して共通declare coreへ渡す。
必要なら既知のmodifier list位置も渡す。名前文字列や判定結果のglobal memoにはしない。
computed/default/export/private nameの意味分岐と、2つのSymbolを生成する処理は維持する。
旧caller参考: nameOfDeclarationのKindAt 50,632 / 67,413、nameRefGenerated内ChildRef 112,417 / 75,859。
既存threaded-name実験は参考にできるが、その結果を今のsourceへ無条件に転用しない。

### 6. VariableDeclarationと単項演算の「走査後に子を取り直す」【値】

[bindVariableDeclarationFlowRef](../binder.go:2858)、[bindInitializedVariableFlowRef](../binder.go:2875)、
[bindPrefixUnaryExpressionFlowRef](../binder.go:2678)、[bindPostfixUnaryExpressionFlowRef](../binder.go:2699)、
[bindDeleteExpressionFlowRef](../binder.go:2817)。

Variableは生成walkerがname/type/initializerを取得・bindした後、initializerを再取得し、初期化Flow側でnameを再取得。
++/--はwalkerの訪問後にoperand slot 0を再取得。deleteも訪問後にexpressionを取り直す。

変更単位: そのnodeの整数参照を一括取得し、同じ値で元の順序のbindと後処理を行う専用worker。
単にwalkerの返り値を巨大化するのではなく、後処理があるnodeだけを対象にする。

### 7. Conditional / loop / tryの専用走査【位置】

[bindConditionalExpressionFlowRef](../binder.go:2829)、[bindWhileStatementRef](../binder.go:2192)、
[bindDoStatementRef](../binder.go:2205)、[bindForStatementRef](../binder.go:2218)、
[bindForInOrForOfStatementRef](../binder.go:2243)、[bindTryStatementRef](../binder.go:2407)。

同じ親の複数slotを、label/flow更新を挟んで個別に解決する。多くは値の重複ではなくheader/startの重複。

変更単位: Ifと同様のnode別一括取得。Flow、例外、finally、loopの訪問順は変更しない。
Binary/Call/Parameterほど優先頻度を裏付けていないため、上記のまとまった改修と同時に機械的に対応する候補。

### 8. Switch / CaseBlock【値＋位置】

[bindSwitchStatementRef](../binder.go:2482)はcaseBlockRefをbind時と後処理で再取得。
clauses loopは条件式で毎回ListLenを呼ぶ。
[bindCaseBlockRef](../binder.go:2542)はswitchExpressionのKindAtを、true判定失敗時に同じ式で再度呼ぶ。
内側loopで得たclauseRefをscope外へ出さず、その後に同じListElem(clauses, i)を再取得する。
kindを得たclauseRefを`bindRef`へ渡すため、kindもそこで再取得される。

変更単位: caseBlockRef、switchExpressionのkind、現在clauseのbindNodeを保持。list descriptorをloop外で解決。
空clauseのグループ化、default検出、fallthroughの算法は変えない。

## 共通APIを直す対象

### 9. list走査のowner/start/len【位置、Store固有性が明瞭】

[bindListRef](../binder.go:2026)、[eachList](../binder.go:2039)、[listIndexRef](../binder.go:2059)、
[modifierFlagsRef](../binder.go:503)、[hasExportDeclarationsRef](../binder.go:996)、[bindInitializedVariableFlowRef](../binder.go:2875)。

ListLenで解決したownerとlist headerをListElemへ渡せず、要素ごとにlistOwner・start・lenを取り直す。
生成walkerのlist slot解決でも、local indexからqualified ListRefを作り、その直後にownerを調べ直す。

変更単位: local owner / list index / start / lenをloop外で解決するSpan等。
foreign listのownerは保持し、要素refを違うStoreで解釈しない。長さ・構造の有効期間を契約化する。
ListSlotAt側のatomicはlistRef→ID、ListElem/ListLen側はlistOwner→ID。ChildRef/KindAtにatomicはない。
旧caller参考: bindListRefのListElem 50,646 / 35,424。全list consumerの現在合計は104,178 / 47,961。
以前のlist Span実験は実装の出発点にできるが、本体には未導入。

### 10. functions-firstの2つのloop【位置】

[bindEachStatementFunctionsFirstRef](../binder.go:2075)。同じlistの両passでListElemがowner/startを毎回解決する。

変更単位: 2passをそのまま残し、両loopが同じ解決済みlist descriptorを使う。
全要素のkind cacheや1pass化は今回の候補外。2passが必要なことと、毎回ownerを解決することは別。

### 11. Handleベースのnarrowing helper【値＋位置、foreignに注意】

[isNarrowableReference](../binder.go:3241): ElementAccessのArgumentExpressionをORの後半で再取得、BinaryのOperatorを別条件で再取得。
[hasNarrowableArgument](../binder.go:3256): call.Expressionを判定後にもう一度取得。
[isNarrowableOperand](../binder.go:3290)から別helperへ渡す際も、同じBinaryのoperator/operandを取り直す経路がある。

Handleは親headerそのものを保持しない。[childAt](../../ast/store.go:1297)は親index→header/start→child index→child kindを毎回解決する。
Handleを作るだけではこの再解決をなくせない。

変更単位: 現在のhelper内で既に得たchild Handle/bindNodeを保持・受渡しし、必要なchildはまとめて読む。
Handle.childAtの外部child fallbackを、same-Store NodeRef版へ機械的に置換しない。
この種の重複する論理queryはpointer版にもあるため、判定結果のmemoや再帰算法の変更は混ぜない。

## 契約を確認してから進める対象

### 12. 識別子から親を逆引きし直す【値＋位置、旧実験で効果限定】

[isIdentifierNameRef](../binder.go:1516): 親ref→親kind→name slotを取得して現在の子と照合。
訪問側は親ref/kind/slotを知っていたが、bindChildRefへ渡すのは親kindのみ。
旧caller参考: ParentRef/KindAt各123,553 / 0。

変更単位: 直近親の既知情報を識別子判定へ渡す範囲を限定する。
全祖先stackは不要。既存parent-argument実験では全体効果が小さかったため、保持・引数費を無視して最優先とはしない。

### 13. modifier・rootDeclaration・親chain【位置＋値、算法cacheはしない】

[combinedModifierFlagsRef](../binder.go:518)、[combinedNodeFlagsRef](../binder.go:545)、[rootDeclarationRef](../binder.go:536)、declare各worker。
同じ宣言に対する修飾子判定ごとにlist slot、root、親のkindを再解決。
旧caller参考: modifierFlagsRefはListLen 28,548 / 32,866回に対して、ListElemは142 / 3,780回。
従って「長いmodifier listの走査」より、空listの再取得・入口費用も重要。

変更単位: 同じ宣言処理内で既知のroot/parent bindNodeとmodifier list位置を渡す。
modifierの計算結果cacheはpointerにも適用できる算法側の改善なので別比較。
combinedNodeFlagsのFlags値は可変であり、親chain位置の共有と区別する。

### 14. kind取得からbindKindのFlagsまで【位置、値の共有ではない】

[bindChildRef](../binder.go:1994)、[at](../binder.go:1894)、[bindKind](../binder.go:168)、[bindContainer](../binder.go:1707)。
子kindを得るためにheaderを解決し、その後bindKindが同じnodeのFlags等を読むためheaderを再解決する。
ただしkindとFlagsは別情報であり、Flagsは意味処理・再帰を挟んで更新される。

変更単位: 必要なら短命なheader位置を渡し、Flagsは元の時点でfresh readする。
先行Flags値の再利用には既存実験で利益がなく、今回も古いFlagsへの置換を候補にしない。
KindAtの全呼出しを重複とみなさない。別childのkindの初回readは必要。

### 15. OptionalChainとcontainer意味情報【一部位置、低優先】

[isOptionalChainRef](../binder.go:2650)、[isOutermostOptionalChainRef](../binder.go:2665)、[bindOptionalChainRef](../binder.go:2933)。
同じnodeのoptional判定・questionDot・expression・親kindを複数helperで解決。
Flagsのreadをまたぐ更新可否を分け、まずexpression/questionDot/親bindNodeの構文情報だけを渡す。

[declareSymbolAndAddToSymbolTableRef](../binder.go:689)とdeclareSourceFile/Module/Classの経路では、同じcontainerのSymbolを再取得する経路もある。
Symbol/Localsは遅延作成・差替えがあるため、変化しない短い経路だけで取得済み値を渡す候補。
全containerのSymbol/Locals cacheを提案しているのではない。頻度・書込み介在は未確定。

## 対象外・対応済み

- IfStatementと生成walkerのchild slot一括取得、非optional PropertyAccessの一括取得は対応済み。
- 生成walker後に同じchildを読み直す経路や、list経路は対応済みではない。
- bindChildrenOf fallbackのNumChildrenAt/NumListSlotsAtがloop条件に残るが、代表入力でのhot path根拠がない。網羅的候補には含めるが優先度は低い。
- 2passを1passへ変える、narrowing結果や宣言名文字列をmemoする、意味情報の最新readを消す、foreign対応を捨てる変更は含めない。

## 証拠・再現・性能の扱い

一覧は現行sourceのcall chainを確認したもの。削減可能な動的命令数やwall寄与の見積りではない。
caller頻度は旧audit-sitesの参考値で、現在版の正確なcaller別計数として再ラベルしない。
ただし現在の全体計数は、最終syntax-scalars版のsource hashと一致を検証したartifactから取得した。

| 現在のaccessor計数 / bind | checker | dom |
|---|---:|---:|
| ChildRef | 349,295 | 122,017 |
| KindAt | 688,528 | 200,203 |
| FlagsAt | 530,841 | 164,574 |
| ParentRef | 160,977 | 13,113 |
| ListLen | 84,229 | 51,046 |
| ListElem | 104,178 | 47,961 |
| ListSlotAt | 110,158 | 87,747 |

直近all_batchの通常GC中央値（今回再測定していない）:

| 入力 | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| checker | 16,362,465 | 12,799,473 | 14,165 |
| dom | 5,896,931 | 7,866,729 | 16,684 |

保存benchstatでは基準比命令−1.69% / −1.76%、通常wallは非有意。
割当driverはsymbolIdx/flows列、symbolRefs、FlowNode/Symbol/Handle拡大のまま。この一覧の大部分はCPU側の再解決を対象とする。

選択Store repo `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、rev `32598cba146fa4dd7b6162b838630c90d865ab28`。
pointer `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、rev `8ac035a394c79e693a3a7d74cb170448503ee894`。tsgolint_git_revは双方null。
候補clone: flownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜5、store-pr-6-perf、store-pr-7、store-pr-7-attach-parent-fix、store-pr-7-nodeseq-t10、store-redesign。別介入として不使用。

artifact root `.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/`。
`syntax-scalars-experiment`は3 identity条件一致でcurrent、現行Go source snapshotも一致。
`remaining-accessor-audit`と`audit-sites`は旧sourceの参考。identityとsource世代を区別する。
別root/revisionの結果はstale。今回候補の新規benchmarkはmissing、対象Benchmark行のない既存stageを使う場合はunsupportedとする。
証拠と照合結果は`repeated-access-inventory/inventory-evidence.json`に保存。

次の作業は1〜4と6の専用workerをまとまった単位で実装し、意味一致・機械語の一回解決を確認。
その後5と9の共通helperを変更する。局所変更ごとにwall有意差を要求せず、まとまった改修で全体benchstatを取る。
受入条件は診断/Symbol/Flow/訪問順一致、構文snapshotの有効期間、foreign/null表現の維持、不要readを消せた機械語、過大なspillがないこと。
