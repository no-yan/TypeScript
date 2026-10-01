# Store checker 等価 段 1 (TODO 7d-2b-1) 検証報告

作成日: 2026-10-01。指示は [store-checker-7d2b1-verification-instructions-20261001.md](store-checker-7d2b1-verification-instructions-20261001.md) (以下「検証指示書」)。対象は [実装報告](store-checker-7d2b1-implementation-report-20261001.md) の作業ツリー (before は **5c0e86b580**、未コミット)。生の出力は `_store-checker-7d2b1-results/` (ignore 対象)。

## 1. 結論

**合格。**

- 比較は本当に比べている。§2 の 9 種の差を全部検出し、差を入れた program の取りこぼしは 0。
- 分類は再現する。class が実行ごとに変わるのは、実装報告 §4 の非決定の 1 file だけ。抜き取った 45 件は class の名前どおりの差だった。
- §3 の P2 は Pointer と等価。読み・hash・到達・変換器の停止の 4 つで確かめた。
- ガードは守られている。直してはいけないものは直していない。

ハーネスの小さな欠陥を 3 つと、報告に無い変更を 1 つ見つけた (§7)。どれも今の分類の結果は変えない。ハーネスは直していない (§8 の 1)。

source の sha256 (tsc と tools の追跡・未追跡の `.go`・`.ts`・`.mod`・`.sum`・`.json`、18,086 file) は、検証の最初・再生成の後・最後で一致した。`git status` も最初と同じ。

## 2. §1 再現とガード

| 項目 | 結果 |
| --- | --- |
| 再生成 | 2 つの生成器の後で `git status` が同じ。18,086 file の sha256 も一致 |
| build・vet | `go build ./...`、`go vet ./internal/ast/... ./internal/store... ./internal/storeharness/...`、`go vet ./cmd/storetranslate/...` が通る |
| 変換器のテスト | 9 本とも通る (足した 4 本を含む)。`translate_test.go` の変更は追加だけで、既存のテストは緩めていない |
| tag なし | 失敗は `FuzzEquivalence/a708303f611b95c8` だけ。7m09s、max RSS 1.64 GiB |
| `-tags storechecks` | 同じく a708 だけ。9m20s、max RSS 1.66 GiB |

両方の tag の中で、storeharness のテストは次のとおり。

- `TestCheckerCorpus` は 15 class・equal 16,665。
- `TestModeCorpus` は 4,996 位置で不一致 0。
- `TestPrivateFixtures` は 6 本とも通る。
- `TestPointerWalkerBaselines` は 14 本で不一致 0。

### 2.1 ガードの diff

`git diff --stat 5c0e86b580` で、storechecker・storeharness・docs の外に出るのは `storeprinter/emitcontext.go` の +6 行だけだった。

| file | 変更 | 説明 |
| --- | --- | --- |
| `storeprinter/emitcontext.go` | `EmitContext.Reset` (生成) | §4 の根 `printer.EmitContext.Reset`。中身は Pointer の Reset と同じ |
| `storechecker/printer.go` | `SymbolToStringEx` (生成) | §4 の根 |
| `storechecker/types.go` | `IntrinsicType.IntrinsicName` (生成) | §4 の根 |
| `storechecker/checker_manual.go` | `checkIfExpressionRefinesParameter` の 1 hunk | §3 の P2 |
| `storechecker/flow_manual.go` | store-only の 2 関数を追加。末尾の comment 3 行を削除 | §3 の P2。削除は §7 の 4 |
| `tools/cmd/storetranslate/` | `manual.go`・`reach.go`・`translate_test.go` | §3.2 と §4 |

- 変わった生成物は上の 3 つで、全部 §4 の根で説明がつく。
- store-only の P2 の本体が呼ぶ関数は、ほかの根から既に到達していた。新しい生成物は無い。
- 変換の規則 (`translate.go`・`calls.go` など) は変わっていない。
- ほかの P2、`ast/store`・`storeparser`・`storebinder` の production のコード、`go.mod` も変わっていない。

## 3. §2 比較の感度

**検出しない差は 0 で、合格。**

- 差 1〜7: sample 3000 (固定の間隔) を worker 4 で回した。
- 差 8: `TestModeCorpus` の全体で確かめた。
- 差 9: fixture で確かめた。

差は scratchpad に置いた複製を `go test -c -overlay` で差し替えて入れ、repo は変えていない。

- 1 本の binary の中で、env `STORE_MUT` で差を選ぶ。
- 差が入った program には class に `[mut]` を付けた。取りこぼしは `[mut] equal` として数える。
- 差なし (`STORE_MUT=0`) の同じ binary は、元の binary と 3000 / 3000 で class が一致した。

| # | 入れた差 | 入れた場所 | 差が入った program | base で equal | 検出した class | 件数 | 取りこぼし | 最初の file |
| --- | --- | --- | ---: | ---: | --- | ---: | ---: | --- |
| 1 | `GetDiagnostics` の最後の 1 件を落とす | `runStore`。変換の前の `[]*store.Diagnostic` | 1,721 | 1,638 | `diag file: count` | 1,638 | 0 | `fixtures/compiler/_namespaces/ts.moduleSpecifiers.ts` |
| 2 | 診断の `MessageChain` の要素の引数を 1 つ変える | 変換の後の記録 | 168 | 143 | `diag file: MessageChain.MessageArgs` | 143 | 0 | `arrayAssignmentTest2.ts` |
| 2' | chain の chain (2 段下) の引数を 1 つ変える | 同上 | 72 | 61 | `diag file: MessageChain.MessageChain.MessageArgs` | 61 | 0 | `arrayBestCommonTypes.ts` |
| 3 | `RelatedInformation` の 1 件の `Pos` を +1 | 同上 | 196 | 179 | `diag file: RelatedInformation.Pos` | 179 | 0 | `fixtures/typescript/…/--clean and --force together is invalid.js` |
| 4 | suggestion の `ReportsUnnecessary` を反転 | 同上 | 988 | 933 | `diag suggestion: ReportsUnnecessary` | 933 | 0 | 同上 |
| 5 | 型の印字の flags から `NoTruncation` を外す | `walker.go` | 2,888 | 2,771 | `walk: type` | 27 | (注) | `blockScopedVariablesUseBeforeDef.ts` (`() => () => any` と `() => () => ...`) |
| 6 | type walk で、property access の name の識別子を 1 つ飛ばす | `walker.go` | 856 | 786 | `walk: node set` | 786 | 0 | `fixtures/compiler/transformers/classThis.ts` |
| 7 | `GetSymbolAtLocation` の結果を、先に出た同名の別の symbol に差し替える (program に 1 回) | `walker.go` | 1,127 | 1,028 | `walk: symbol` 986・`symbol correspondence` 42 | 1,028 | 0 | `corePublic.ts` (`T` の Decl の位置が違う)・`chainedCallsWithTypeParameterConstrainedToOtherTypeParameter2.ts` (印字が同じで、対応の検査が捕まえた) |
| 8 | adapter の `GetModeForUsageLocation` で import type の override を無視 | `mode.go` | — | — | `TestModeCorpus` の失敗 | GetMode 11・GetResolved 11 | — | `importAttributeTypeOnlyImports.ts#/index.ts` の 290 |
| 9 | `lookupSymbolForPrivateIdentifierDeclaration` が、派生の class で基底の private を先に返す | `storechecker/checker.go` | — | — | `TestPrivateFixtures/5-derived` だけ失敗。`diag suggestion: count` (Store だけに `#x` の 6133) | 1 | — | `5-derived/derived.ts` |

- (注) 差 5 の印は、型を印字するたびに立つ。差が出るのは切り詰めの長さを超える型がある file だけなので、取りこぼしの数には意味が無い。
- 差 2〜4 は変換の後の記録に入れたので、確かめたのは比較器。変換 (`pointerDiags`・`storeDiags`) は読んで確かめた。両方とも同じ field を全部写している。
- 差 7 は、両側の印字が偶然一致する場合 (42 件) に対応の検査が効くことも示した。

## 4. §3 比較の相手の正しさ

### 4.1 Pointer 側の walker (§8 の 4)

**既定の条件** は 14 本で不一致 0 (tag なし・storechecks・単独の 3 回)。

**広げた mode** (`STORE_CHECKER_BASELINE_OPTIONS=1`) は 4,671 本で不一致 2、採らなかった test は 28 本。実装報告のとおり。

- 2 本 (`elidedEmbeddedStatementsReplacedWithSemicolon.ts`・`jsxLibraryManagedAttributesUnusedGeneric.tsx`) はどちらも `@ts-ignore` を持つ。
- reference に `.errors.txt` が無いので、baseline の `hadErrorBaseline` は偽で、any の型を `error` と印字している。driver は comment directive の前の件数で真になり、`any` と印字する。
- 差は baseline の側の条件で、walker の写しの規則の違いではない。

option の無い test の数も独立に数えた。

- tests/cases/compiler は 6,831 本。`// @x:` の行が無いのは 15 本 (BOM や UTF-16 の file を含む grep の数え方)、`@target` を持つのは 6,514 本。
- 実装報告の 6,832・14・6,515 と数え方の差の範囲で合う。14 本で規則を確かめたとは言いにくい、という報告の判断に同意する。

### 4.2 規則の読み (`type_symbol_baseline.go` と `walker.go`)

規則 1〜4 を 1 つずつ並べて読み、条件の抜けと順序の違いは無かった。

- **訪問**: `forEachStoreNode` は `forEachASTNode` と同じ。Reparsed の 2 段の条件、As / Satisfies の例外、子の反転の順が同じ。`elem == elem.Parent.Expression()` は `SameNode` で書いている。
- **対象**: `visitNode` の 4 条件が同じで、順も同じ。
- **型**: 次が全部同じ。
  - 除外の 3 条件と OmittedExpression
  - extends の `ExpressionWithTypeArguments` の親を先に試すこと
  - any の短絡と 9 つの例外 (順も同じ)
  - flags、`InTypeAlias` での作り直し、`RemoveComments` の printer
- **symbol**: `SymbolToStringEx(sym, node.Parent, SymbolFlagsNone, AllowAnyNodeKind)`、`EscapeAllInternalSymbolNames`、宣言 5 件、lib の `--, --` が同じ。

規則を変えない違いは 3 つあった。

1. tsbaseline の `declarationTextCache` は読まれるだけで書かれない。写さなくてよい。
2. EmitContext は、Pointer が node ごとに pool から取り (put で Reset 済み)、Store は 1 つを node ごとに Reset する。Reset の中身は同じ (`*c = EmitContext{Factory: c.Factory}`)。
3. `present: false` の記録を持つ (実装報告 §8 の 8)。

walker の node の選別は store の述語 (`IsExpressionNode` など、変換器の出力) を使う。それでも、述語の誤りは比較で見える。Pointer 側は ast の述語で選ぶので、誤りは `walk: node set` に出る。corpus では 0 件だった。

### 4.3 手書きの証拠 (規則 6)

- 変換器の入力の package の表 (`pkgs.go`) に `testutil/tsbaseline` が無い。変換器はこの walker を出力できない。
- storeharness の file に、生成物の header (`Code generated by tools/cmd/storetranslate`) も `storetranslate:` の印も無い。
- store に無い述語を小文字で手書きしている。変換器なら U2 の規則で `store.GetMeaningFromDeclaration` などを export で出す。
- 手書きらしい形がある: `parent := node.Parent()` の局所変数、`IsJsxClosingElement` の代わりの `Kind()` の判定、`strings.Clone`、記録の struct。
- `x.File()` (`GetSourceFileOfNode`) と `SameNode` は変換器の規則とも重なる。ただし Store での自然な書き方なので、どちらの証拠にもならない。

### 4.4 手書きの述語

実装報告 §7.2 の一覧を、Pointer の元と 1 つずつ比べた。全部、同じ判定の順。

| 述語 | 元 | 結果 |
| --- | --- | --- |
| `getMeaningFromDeclaration` | ast/utilities.go:2250-2306 | case の集合・順・ModuleDeclaration の 3 分岐が同じ |
| `isLabelName`・`isLabelOfLabeledStatement`・`isJumpStatementTarget` | 2312-2338 | 同じ。`NodeKindIs(…, Break, Continue)` は `IsBreakOrContinueStatement` の中身そのまま |
| `isImportStatementName`・`isExportStatementName`・`isIntrinsicJsxTag` | tsbaseline | 同じ。JsxClosingElement は `Kind()` で判定 |
| `isDefaultLibraryFile` | tsbaseline/util.go:51 | 同じ |

### 4.5 adapter の C の 3 method

- `getModeForUsageLocation`・`getEmitSyntaxForUsageLocationWorker`・`importSyntaxAffectsModuleResolution` は fileloader.go:1018-1085 と、分岐の順・条件・return の値が同じ。`ast.IsJSDocImportTag` は `Kind()` の判定で書いている。
- `GetResolvedModuleFromModuleSpecifier` は program.go:615 と同じ。
- file ごとの options も Pointer と同じになる。
  - Pointer は `getCompilerOptionsForFile` = `GetCompilerOptionsWithRedirect(opts.Config.CompilerOptions(), getRedirectParsedCommandLineForResolution(file))`。
  - adapter は `p.pointer.Options()` (= `opts.Config.CompilerOptions()`) と `GetRedirectForResolution` (同じ関数) を使う。
- A の委譲先が `FileName()`・`Path()` 以外を読まないことも確かめた (`getRedirectForResolution`・`getSourceToDtsIfSymlink`・`GetResolvedModule`・`GetDefaultResolutionModeForFile`・`GetSourceOfProjectReferenceIfOutputIncluded`)。

## 5. §4 の P2

**等価で、合格。**

### 5.1 読み

`checkIfExpressionRefinesParameter`・`getFlowTypeOfReferenceUnderCondition`・`getTypeAtSyntheticCondition` を、Pointer の 4 関数と並べて読んだ。

- 文の順序、early return、`f` の読み書き (`reference`・`declaredType`・`initialType`・`flowContainer`・`sharedFlowStart`・`depth`、`flowInvocationCount`・`sharedFlows` の切り詰め・`putFlowState`) は Pointer と同じ。
- Pointer の合成の Start (`Flags: Start`、`Node: nil`) は、`getTypeAtFlowNode` の Start の枝で container が nil なので `f.initialType` を返す。Store の nil の antecedent の枝は同じ値を返す。
- 違いは、Pointer では Start に 1 段分の `getTypeAtFlowNode` (depth の検査と増減) が増えることだけ。`f` は `getFlowState` で取った直後 (`putFlowState` が depth を 0 にする) で、その時点の depth は 1。2000 の検査は当たらず、観測できない。
- 写していない `flowNode == nil` の枝は、合成の flow では通らない。

### 5.2 根拠の検算 (現在の Pointer)

- flow.go:117-206: 合成の Condition で読まれるのは `Flags` (Shared の判定と switch)、`Condition` の枝の `getTypeAtFlowCondition` の `Antecedent`・`Flags`・`Node` だけ。
- 合成の flow は `Flags` に Shared を持たない。shared の cache は読まれも書かれもしない。合成の flow の pointer は `getTypeAtFlowNode`・`getTypeAtFlowCondition` の外に出ない (`narrowType` に渡るのは `flow.Node`)。

### 5.3 到達の数 (overlay の counter、corpus 全体)

Store の P2 に counter を足した binary で、全体を 1 回回した。分類は全体の表と同じ 15 class・equal 16,665。

| 項目 | 回数 | file |
| --- | ---: | ---: |
| `checkIfExpressionRefinesParameter` の呼び出し | 884 | 117 |
| うち antecedent が nil (Pointer の合成の Start) | 485 | — |
| うち return 文の flow を antecedent に使う | 376 | — |
| 偽の条件まで評価した (`trueType != initType`) | 162 | — |
| 述語を返した | 71 | — |

| | file | equal |
| --- | ---: | ---: |
| corpus 全体 | 17,414 | 95.7% |
| P2 を呼んだ file | 117 | 65.0% (76) |
| うち fixtures | 40 | 32.5% |
| うち tests | 77 | 81.8% |

- 呼んだ file の不一致は 41: `diag file: count` 18・`diag suggestion: count` 17・`walk: type` 6。
- **41 件とも、実装の overlay の「store の API 2 つを差し替えた」実行 (`_store-checker-7d2b1-impl/overlay/corpus-both.jsonl.gz`) で equal になる。** その実行でも P2 は同じ。
- **P2 が原因と読める不一致は無い。**
- equal の割合が低いのは、P2 を呼ぶのが関数の多い大きな file (compiler の fixtures) に偏るためと読む。
- equal の 76 file の中でも全経路を通っている: nil の antecedent 33 file、return 文の flow 44 file、偽の条件 16 file、述語を返す 11 file (38 回)。

### 5.4 hash

4 関数の hash を、変換器を使わずに計算した (`func` から閉じ括弧までの bytes を awk で切り出し、`shasum -a 256` の先頭 16 字)。記録と全部一致した。

| 関数 | 計算 | 記録 |
| --- | --- | --- |
| `checkIfExpressionRefinesParameter` | `867f582282b847d0` | 同じ |
| `getFlowTypeOfReferenceEx` | `e5bdb3241f089644` | 同じ |
| `getTypeAtFlowNode` | `d7c60542a1edc4fe` | 同じ |
| `getTypeAtFlowCondition` | `fbb9c4044926e489` | 同じ |

### 5.5 変換器の拡張

scratchpad に tsc と tools の source を複製して回した。無変更では成功し、出力は repo と byte で一致した。

- **(a)** `getTypeAtFlowCondition` の本体の 1 行 (`!= 0` を `== 0`) を変えた。変換器は `getTypeAtSyntheticCondition: the Pointer declaration Checker.getTypeAtFlowCondition changed (hash 470257cb3066c33f, recorded fbb9c4044926e489)` で止まる。
- **(b)** 2 つの store-only の印を 1 つずつ外した。どちらも `the hash lines name …, and one must name the declaration it replaces (…; a helper of the Store's own is marked storetranslate:store-only)` で止まる。

## 6. §5 分類

### 6.1 再現

全体の 4 回 (検証の full1・full2、実装の run 5・run 4) を、JSONL の行の順で比べた。行の順は corpus の順で、17,415 行が同じ順に並ぶ。

| 比較 | class が違う行 |
| --- | --- |
| full1 と full2 | 1 (非決定の file) |
| full1 と run 5 | 1 (同じ file) |
| full1 と run 4 | 2 (同じ file と、run 4 でハーネスのエラーだった vfs の file。実装報告 §9 の 3 で除外に直したもの) |

**並列の順で class が変わる file は `jsxNamespaceImplicitImportJSXNamespace.tsx#/node_modules/preact/jsx-runtime/index.d.ts` の 1 つだけ。** class は実行によって次のように変わった。

| 実行 | class |
| --- | --- |
| full1 | `walk: type` |
| full2 | `diag file: MessageArgs` |
| テスト一式 (tag なし・storechecks) | 2 回とも `diag file: MessageArgs` |
| 実装の run 5 | `diag file: count` |

ほかの 14 class の件数は 6 回とも同じ。

**JSONL の `file` は一意でない。** 5 つの origin が 2 行ずつある (`augmentExportEquals2.ts#file3.ts` など)。test が同じ `@filename` を 2 回持ち、空の unit と本体の 2 つになるため。5 組とも 2 行とも equal (§7 の 3)。

### 6.2 鍵の機械性

`compare.go` を読んだ。

- 鍵は次の部品だけでできている。
  - 列の名前 (`file`・`suggestion`・`global`)
  - 比べる順の field の名前と `.count`
  - `walk: ` + `type`/`symbol`
  - 固定の文字列 (panic・合成の import・対応・ID の token・equal)
- 手で書いた例外や、差を class に写す名前の表は無い。
- field の名前の並びは「比べる field の列挙」で、差から class を引く表ではない。
- 実装指示書 §7.2 の表のとおり。入れ子の path (`MessageChain.MessageChain.MessageArgs`) は実装報告 §8 の 9 の細部。

鍵の精度の注意が 2 つある (§7 の 2)。

1. 同じ診断で、前の field の差が ID だけ、後ろの field に本当の差があるとき、鍵は前の field (ID だけの差) を名指す。検証用のテストで確かめた: MessageArgs が ID だけ違い、RelatedInformation.Pos が本当に違うと、class は `diag file: MessageArgs`。
2. panic の鍵は message の先頭 60 字を含む。`index out of range [5]` のような実行時の値で class が割れうる。今は panic の class が 1 つなので影響は無い。

### 6.3 中身の抜き取り

seed 20261001 で、上位 10 class から 5 件ずつ選んだ。5 件に満たない 2 class (3 件と 2 件) は全部を取った (`class-samples.txt`)。**45 件とも class の名前どおりの差だった。**

- `walk: type` 5 件: どれも同じ node で、型の文字列が `| undefined` の有無で違う。
- `diag file: count` 5 件: 件数が違う (11 と 6、15 と 14、48 と 36、0 と 1、0 と 2)。
- `diag file: MessageChain.Code` 5 件: 2326 と 2327。
- 残りの class も、詳細の両側の値が class の名前の field で違った。

source で位置も確かめた。`override18.ts` の 14-17 は `foo?: string`、`isolatedDeclarationsAddUndefined.ts#file1.ts` の 30 は `c? = …`、`i3.ts` は `one?: number`。どれも `?` の付いた property で、§6.6 の #1 の読みと合う。

### 6.4 `internal id token`

- 全部の実行で 0 件。
- JSONL の詳細に、ID の形 (`#<数字>@`・`__@…@<数字>`) や内部の prefix を含む行も 0。今の分類には ID の差が出ていない。
- `withoutIDs` を検証用のテストで確かめたところ、**正規表現の `\xFE` の alternative は一度も当たらない** (§7 の 1)。今の結果には影響しない。

### 6.5 除外

- 「root が Pointer の program に無い」: 0。
- 「Pointer panics」: 1 (`pathMappingBasedModuleResolution_rootImport_aliasWithRoot_differentRootTypes.ts#/root/a.ts`)。
  - ハーネスを通さず、Pointer の program と checker だけを組む検証用のテストで再現した。
  - panic は checker ではなく **program の作成** で起きる。module 解決が `//server/foo` の directory を引き、test 用の vfs (`iovfs`) が `Sub("//server")` で `invalid argument` を返す。
  - testrunner では tsconfig の `paths` が先に写すので起きない。1 file の program に特有の panic。
  - 検証指示書は「checker 単独で」と書くが、checker まで届かない。除外の理由は program の作成を含む読みで正しい (実装報告 §9 の 3)。

### 6.6 原因の読み (3 つ)

| # | class | 独立に読んだ結果 |
| --- | --- | --- |
| 1 | `walk: type` ほか (QuestionToken) | **一致**。`store.Node.QuestionToken()` (views_generated.go:5508) は `questionTokenSlot` の 4 kind しか引かず、`PostfixToken()` への fallback が無い。`HasQuestionToken`・`isOptionalDeclaration` は Pointer の写しなので、PropertySignature などの `?` で偽になる。なお、Pointer の `PostfixToken()` にある EnumMember・GetAccessor・SetAccessor の 3 kind は store の slot に無い。ただし Pointer の parser はこの 3 kind に postfix の token を作らない (`NewEnumMember`・`NewGet/SetAccessorDeclaration` に引数が無い) ので、実害は無い |
| 2 | `diag file: count` (OrderedNodeSet) | **一致**。`OrderedNodeSet.Values()` (ast/store/checker_api.go:290) の `for _, n := range s.keys` は、反復の開始時の slice しか回らない。Pointer の `OrderedMap.Keys()` は index で回り、反復中の追加も数える。`checkDeferredNodes` は両側とも反復中に足す |
| 11 | store panic (`findResolutionCycleStartIndex`) | **一致**。`store.Node` は `_ [0]func()` で意図して比較できない。`TypeSystemEntity` (any) どうしの `==` は、両方が `store.Node` のとき実行時に panic する。target に Node を push するのは `InitializerIsUndefined` の 1 か所 (checker.go:10641) だけ。panic には、resolution の stack に Node が 2 つ積まれる循環が要る |

## 7. 見つけたこと

1. **ハーネス: ID の token の正規表現が、内部の prefix に当たらない。**
   - `uniqueSymbolIDRe` の `(__|\xFE)` は、Go の regexp では U+00FE (þ) の意味になる。内部の prefix は生の byte 0xFE (`ast.InternalSymbolNamePrefix`、不正な UTF-8) で、regexp はこれを U+FFFD として読む。
   - そのため escape 前の unique symbol の名前 (`\xFE@name@<id>`) の ID の差は、`internal id token` ではなく本当の差として分類される。
   - 今は該当が 0 で、誤りの向きも厳しい側 (合格扱いにはならない)。ただし 2b-2 で ID を照合するときに効く。
   - 直し方: `\xFE` を `\x{FFFD}` に替えると、生の byte を保ったまま数字だけを落とせる。scratchpad で確かめた (`"\xfe@iterator@17"` は `"\xfe@iterator@"` になる)。
2. **ハーネス: 鍵の field の精度** (§6.2 の 1)。
   - 直し方: `diagList` で、`ids` を立てた比較が見つけた field (本当の差) を鍵に使う。
3. **ハーネス: JSONL の `file` が一意でない** (§6.1)。
   - 2b-2 で file を名指して除外や比較をするなら、corpus の index か unit の順番を鍵に足す。
4. **ガード: 報告に無い削除が 1 つ。**
   - before の `flow_manual.go` の末尾に、宣言の無い comment があった。`checkGrammarForUseStrictSimpleParameterList` が NodeRef を `FindUseStrictPrologue` に渡す理由 (R15) の説明で、関数の本体は `grammarchecks_manual.go` にある。
   - 2b-1 はこれを削除し、移していない。結果には影響しないが、R15 の説明が repo から消えた。

## 8. 判断を仰ぐこと

**決定 (2026-10-01、ユーザー)**

- §7 の 1〜3 (ハーネスの修正) は 2b-2 で行う。
- §7 の 4 の comment は復活させる。宣言の無い `flow_manual.go` の末尾ではなく、`grammarchecks_manual.go` の `checkGrammarForUseStrictSimpleParameterList` の doc comment (hash の行の上) に戻した。
  - 変換器を回し直して通ること、生成物が変わらないこと、gofmt・vet が通ることを確かめた。
  - source の sha256 で変わったのはこの file だけ。

以下は決定の前に挙げた項目。

1. **§7 の 1〜3 を今直すか、2b-2 の最初に入れるか。**
   - 検証の規則ではハーネスのバグは直してよい。それでも直さなかった理由は 2 つ。今の分類の結果を変えないこと。1 と 2 は 2b-2 で設計する ID の照合の扱いに触れること。
   - 私の推奨は 2b-2 の最初。1 は 1 token の修正なので、今入れても費用は小さい。
2. **§7 の 4 の comment** を `grammarchecks_manual.go` の該当の関数に戻すか。
3. **実装報告 §10 の 4 件への意見。**
   - 1 (§8 の 4 の条件): 広げた mode を既定にするのがよい。`@ts-ignore` の 2 本は名指して除くのではなく、検定の中だけで `hadErrors` を「reference に `.errors.txt` があるか」にする。tsbaseline の `hadErrorBaseline` そのものなので、driver の規則 (H1) を変えずに条件をそろえられる。
   - 2 (2b-2 の最初の 2 件): 同意する。§6.6 の #1 の 3 kind は Pointer の parser が作らないので、照合の試験に入れても差は出ない見込み。
   - 3 (非決定の file): Pointer 自身の非決定なので、除外の理由を足すことに同意する。名指しの表より、不一致が出た file だけ Pointer 側をもう 1 回走らせて自分と比べる、のように機械的に決めるほうが §5.2 の方針に合う。費用は不一致の 1% 程度の file だけ。
   - 4 (合成の import): 段 2 のままでよい。8 件はどれも pragma によるもので、段 1 の option では作られない、という指示書の見込みの外の経路。

## 9. 時間 (§6)

この機械 (M1、8GB)、worker 4。

| 実行 | file | wall | file あたり | 最大 RSS | peak footprint | file の時間 (中央値 / p99 / 最大) | load average (開始 → 終了) |
| --- | ---: | --- | ---: | ---: | ---: | --- | --- |
| sample 500 | 500 | 5s | 9.2 ms | 0.27 GiB | — | 33ms / 94ms / 462ms | 7.4 → 7.0 |
| 全体 full1 | 17,415 | 4m49s | 16.6 ms | 1.69 GiB | 2.61 GiB | 42ms / 182ms / 57.2s | 7.0 → 12.3 |
| 全体 full2 (参考) | 17,415 | 5m43s | 19.7 ms | 1.79 GiB | — | — | 12.3 → 9.5 |

- 最大 RSS はテストの log (getrusage、GiB) の値。`/usr/bin/time -l` の値も同じ (full1 は 1,813,086,208 bytes)。
- 実装報告 (sample 5s・9.8 ms・0.30 GB、全体 5m12s〜6m33s・2.19〜2.28 GB) と同じ桁。
- 全体は報告より速く、RSS は小さい。報告の測定は load 9〜40、こちらは 7〜12。
- full1 の最後の 1〜2 分は、私の grep (tests/cases の数え上げ) と重なった。
- 性能の線は無い (2c)。

## 10. 再現の手順

```sh
ROOT=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript
TSC=$ROOT/tsc
OUT=$TSC/internal/ast/docs/_store-checker-7d2b1-results
SCR=<scratchpad>
# §1
cd $ROOT && node tools/scripts/tsc/generate.ts && (cd tools && GOWORK=off go run ./cmd/storetranslate) && git status --short
cd $TSC && GOWORK=off go test -count=1 -timeout 30m -v ./internal/ast/store/... ./internal/store... > $OUT/test-notag.txt 2>&1
cd $TSC && GOWORK=off go test -tags storechecks -count=1 -timeout 30m -v ./internal/ast/store/... ./internal/store... > $OUT/test-storechecks.txt 2>&1
# §2・§4.3・§5・§6: overlay の差分は $OUT/overlays/ (mut/・p2/・verify/)。
# 複製を差分から作って overlay の JSON で差し替える。
cd $TSC && GOWORK=off go test -c -o $SCR/storeharness.test ./internal/storeharness/
cd $TSC && GOWORK=off go test -c -overlay $SCR/mut/overlay.json -o $SCR/mut/storeharness-mut.test ./internal/storeharness/
cd $TSC && GOWORK=off go test -c -overlay $SCR/p2/overlay.json -o $SCR/p2/storeharness-p2.test ./internal/storeharness/
$OUT/overlays/runs.sh     # sample 500 → full1 → full2 → p2 → 差 1〜7 (sample 3000) → 差なし → sample 3000
cd $TSC/internal/storeharness && STORE_MUT=8 $SCR/mut/storeharness-mut.test -test.run TestModeCorpus -test.v
cd $TSC/internal/storeharness && STORE_MUT=9 $SCR/mut/storeharness-mut.test -test.run TestPrivateFixtures -test.v
# §4.1
cd $TSC/internal/storeharness && STORE_CHECKER_BASELINE_OPTIONS=1 $SCR/storeharness.test -test.run '^TestPointerWalkerBaselines$' -test.v
# §5.4 の hash (変換器を使わない)
awk -v n=getTypeAtFlowCondition '$0 ~ "^func \\(c \\*Checker\\) " n "\\(" {p=1} p{print} p && /^}$/ {exit}' $TSC/internal/checker/flow.go | perl -pe 'chomp if eof' | shasum -a 256 | cut -c1-16
# 集計: $OUT/overlays/ の sample_classes.py (seed 20261001)・p2_analyze.py・repro_compare.py。JSONL は $OUT/jsonl/
```

生の出力は `$OUT` にある。主な file は次のとおり。

| file | 中身 |
| --- | --- |
| `test-*.txt` | §1 のテスト |
| `mutation-results.txt` | §2 の集計 |
| `mut-8-mode.txt`・`mut-9-private.txt` | §2 の差 8・9 |
| `p2-mutation-*.txt` | §5.5 の変換器の停止 |
| `p2-reach.txt` | §5.3 |
| `repro-compare.txt` | §6.1 |
| `class-samples.txt` | §6.3 |
| `pointer-panic-repro.txt` | §6.5 |
| `baseline-*.txt` | §4.1 |
| `corpus-*.txt`・`.time` | §9 |
| `sha256-start.txt`・`sha256-end.txt` | source の不変 |
