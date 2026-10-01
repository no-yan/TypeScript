# Store checker 等価 段 1 (TODO 7d-2b-1) 実装報告

作成日: 2026-10-01。指示書は [store-checker-7d2b1-implementation-instructions-20261001.md](store-checker-7d2b1-implementation-instructions-20261001.md) (以下「指示書」)。before は **5c0e86b580**。commit はしていない (作業ツリーの変更のまま)。生の出力は `_store-checker-7d2b1-impl/` (ignore 対象、ローカルのみ)。

## 1. 結論と完了条件ごとの結果

- ハーネス (`internal/storeharness`)・§3 の P2・walker と比較器・symbol の対応を作り、corpus 17,415 file を回し終えた。**equal は 16,665 / 17,414 (95.7%)**、不一致は 15 class・749 file。
- **不一致 749 file のうち 739 は、store の API の 2 つの差で説明がつく** (§3)。どちらも直していない。repo を変えない overlay で 2 つだけを差し替えると、equal は 17,404 (99.9%) になった。
  1. 生成された `Node.QuestionToken()` に、Pointer の `(*Node).QuestionToken()` にある `PostfixToken()` への fallback が無い。optional な property が optional と扱われない (これだけで直る file 507)。
  2. 2a で手書きした `OrderedNodeSet.Values()` が、反復中に足された要素を回らない。`checkDeferredNodes` の途中で後回しにされたノードが検査されない (これだけで直る file 184)。
  3. 両方が要る file は 49。
  4. 計 740 のうち 1 は §4 の非決定の file で、差し替えた実行で偶然 equal になったもの。
- 残りは、合成の import (段 2) が 8、変換の規則の外の panic が 1、map の反復順による非決定が 1。
- `checkIfExpressionRefinesParameter` の panic の class は 0 件。
- 実装の途中で、ハーネスのバグを 4 つ見つけて直した (§9)。そのうち 1 つは、記録した型の文字列が printer の 3MB の buffer を抱え続けるもので、全体を 20 秒で OOM kill させていた。

| # | 完了条件 (指示書 §9) | 結果 |
| --- | --- | --- |
| 1 | 生成物の再現、build、store 系・storeharness の vet | 合格。2 つの生成器を回した前後で、store 系の非テストの `.go` の sha1 と `git status` が一致。`go build ./...`、`go vet ./internal/ast/... ./internal/store...`、`(cd tools && go vet ./cmd/storetranslate/...)` が通る |
| 2 | 既存のテストが両方の tag で通る | 合格。tag なしと storechecks の両方で、失敗は既知の `FuzzEquivalence/a708303f611b95c8` だけ (§2.3) |
| 3 | 変換器のテスト | 合格 (`go test ./cmd/storetranslate/`、足した 4 本を含む) |
| 4 | §8 の 1・2・4 が不一致 0 | 合格。ただし §8 の 4 は指示書の条件に当たる test が **14 本しかない** (§6.3、判断を仰ぐ) |
| 5 | `TestCheckerCorpus` がハーネスのエラー無しで全体を回り終える | 合格。ハーネスのエラー無しで全体を 3 回 (run 5 6m33s、テスト一式の tag なし 6m4s・storechecks 6m56s。§5) |
| 6 | `checkIfExpressionRefinesParameter` の panic の class が 0 件 | 合格 (0 件) |

## 2. 作ったもの

### 2.1 変換器 (§3.2・§4)

`tools/cmd/storetranslate/`:

- `manual.go`: P2 の宣言は hash の行を 1 つ以上持つ (`hashes []hashLine`)。全部の行を照合する。
  - 印の無い P2: 行のうちちょうど 1 つが、自分の名前か `<名前>List` を指すこと。0 か 2 以上なら止める (「store-only の印を付けよ」を含む message)。
  - `// storetranslate:store-only` の印の P2: 名前が Pointer の元の package に無いこと。あれば「置き換えにせよ」で止める。
- `reach.go`: store-only の P2 の本体を、手書きの file と同じく根として辿る。§4 の根を 3 つ足した (§7)。
- `translate_test.go`: 指示書の 4 本。
  1. `TestManualSecondHash`: commit した `flow_manual.go` の `getTypeAtSyntheticCondition` の 2 行目 (`getTypeAtFlowCondition`) の hash だけを 0 にすると、その宣言の変更として止まる。1 行目は報告されない。
  2. `TestManualUnmarkedName`: 印の無い `getTypeAtFlowNod` (打ち間違い) で止まる。
  3. `TestManualMarkedPointerName`: 印の付いた `getTypeAtFlowNode` で止まる。
  4. `TestManualStoreOnlyReach`: store-only の P2 の本体が呼ぶ `GetSymbolsInScope` (根からは到達しない、`TestReach`) が到達し、`from` がその P2 になる。

### 2.2 P2 (§3.1)

| P2 | 置き場 | 行 | hash |
| --- | --- | ---: | --- |
| `checkIfExpressionRefinesParameter` (置き換え) | `checker_manual.go` | 17 | 自身 `867f582282b847d0` |
| `getFlowTypeOfReferenceUnderCondition` (store-only) | `flow_manual.go` | 29 | `getFlowTypeOfReferenceEx` `e5bdb3241f089644` |
| `getTypeAtSyntheticCondition` (store-only) | `flow_manual.go` | 41 | `getTypeAtFlowNode` `d7c60542a1edc4fe`、`getTypeAtFlowCondition` `fbb9c4044926e489` |

- `checkIfExpressionRefinesParameter`: Pointer と同じ順に antecedent を求め (式の flow、無ければ return 文の flow)、下を `assumeTrue` の真と偽で 2 回呼ぶ。Pointer の Start (antecedent が無いとき) は nil の `Flow` のまま渡す。
- `getFlowTypeOfReferenceUnderCondition`: `getFlowTypeOfReferenceEx` の写し。`getTypeAtFlowNode(f, flowNode)` を `getTypeAtSyntheticCondition` に替えた。`flowNode == nil` のときの fallback の枝は写していない (合成の flow は nil にならないので死んだ枝、§8 の 1)。
- `getTypeAtSyntheticCondition`: `getTypeAtFlowNode` の 2000 の深さの検査と `depth` の増減を写し、その中で `getTypeAtFlowCondition` の本体を写した。早期の return を保つため、本体はその場で呼ぶ closure にした (§8 の 2)。antecedent が nil なら `FlowType{t: f.initialType}`。

commit しない smoke のテストで、型注釈の無い `x => x != null` の述語が両側で `(x: number | null) => x is number` になることを確かめた。P2 に入った回数は数えていない (検証 §4.3 の範囲)。

### 2.3 storeharness

| file | 中身 |
| --- | --- |
| `program.go` | `NewPrograms`。Pointer の program (VFS は `vfstest.FromMap` を `bundled.WrapFS` で包む、host の current directory は `/`、`SingleThreaded`) と、その `SourceFiles()` の順に `ParseSourceFileIn(space, i, …)` で作る Store の file。bundled の lib の Pointer の SourceFile だけを program の間で使い回す (`libCachingHost`) |
| `adapter.go` | `storechecker.Program` の実装。A (委譲)・B (path で写す)・合成の import・`BindSourceFiles`・`IDSpace` |
| `mode.go` | C の 3 method。fileloader.go:1018-1085 と program.go:615 を読んで Store の述語で書いた |
| `driver.go` | §6.1 の driver。`Compare` が Pointer の program を作り、両側を順に走らせて比べる。panic は側ごとに recover。Store の parse も Store 側の recover の中 |
| `walker.go` | Store 側の type / symbol walker (手書き) と、Store に無い述語の手書き |
| `pointerwalker.go` | Pointer 側の walker。`tsbaseline/type_symbol_baseline.go` の写し (指示書の file の表に無い file、§8 の 11) |
| `compare.go` | §6.2・§6.3・§6.4 の比較と §7.2 の class |
| `corpus_test.go` | `TestCheckerCorpus` と corpus の写し |
| `mode_test.go` | §8 の 1 |
| `private_test.go`、`testdata/private/` | §6.5・§8 の 2 |
| `baseline_test.go` | §8 の 4 |

既存のテスト (`go test -count=1 -timeout 60m -v ./internal/ast/store/... ./internal/store...`):

| package | tag なし | `-tags storechecks` |
| --- | --- | --- |
| ast/store | ok 5.2s | ok 8.9s |
| storebinder | ok 80.2s | ok 93.0s |
| storechecker | ok 1.3s | ok 0.9s |
| storeharness | ok 493.0s | ok 507.3s |
| storeparser | FAIL (`FuzzEquivalence/a708303f611b95c8` だけ) | 同じ |
| storeprinter | ok 195.4s | ok 262.3s |

- 全体の wall は tag なし 8m23s、storechecks 9m20s。検証指示書 §1 の 30 分の timeout に収まる。
- storeharness は `TestCheckerCorpus` (tag なし 365s、storechecks 417s) と `TestModeCorpus` (124s、86s) を含む。
- storechecks の下の `TestCheckerCorpus` の分類は tag なしと同じ。違いは §4 の非決定の file が `diag file: count` ではなく `diag file: MessageArgs` になったことだけ (count 224・MessageArgs 31)。**store の検査 (writer の所有者など) による新しい panic は 0。**

## 3. 分類

### 3.1 分類の表

run 5 (`TestCheckerCorpus` を単独で、2026-10-01)。17,415 file、除外 1 (Pointer panics)、比べたのは 17,414。

| class | file |
| --- | ---: |
| `equal` | **16,665 (95.7%)** |
| `walk: type` | 371 |
| `diag file: count` | 225 |
| `diag suggestion: count` | 81 |
| `diag file: MessageArgs` | 30 |
| `diag file: MessageChain.MessageChain.MessageArgs` | 11 |
| `diag file: MessageChain.Code` | 9 |
| `synthetic import (stage 2)` | 8 |
| `diag file: RelatedInformation.MessageArgs` | 5 |
| `diag file: MessageChain.MessageArgs` | 3 |
| `diag file: MessageChain.MessageChain.Code` | 2 |
| `diag file: Code` | 1 |
| `diag file: RelatedInformation.count` | 1 |
| `diag suggestion: Pos` | 1 |
| `store panic: runtime error: comparing uncomparable type store.Node @ storechecker.(*Checker).findResolutionCycleStartIndex` | 1 |

- 出なかった class: `walk: node set`、`walk: symbol`、`symbol correspondence`、`internal id token`、`store panic: storechecker: checkIfExpressionRefinesParameter …`。
- 除外: 「root が Pointer の program に無い」0、「Pointer panics」1 (`pathMappingBasedModuleResolution_rootImport_aliasWithRoot_differentRootTypes.ts#/root/a.ts`。Pointer の program の作成で vfs が `//server` の sub file system を作れず panic)。
- 同じ表が 4 回の全体で出た (run 4・run 5・テスト一式の中の tag なしと storechecks。storechecks では §4 の file の class だけが違う)。run 4 と 5 の JSONL の (file, class) は、run 4 でハーネスのエラーだった上の 1 file (§9 の 3 で除外に直した) を除いて全部一致。ただし 1 file は実行ごとに class が変わる (§4)。

### 3.2 class ごとの例 (上位 20 class、例 3 件ずつ)

class は 15 なので全部。値は 170 字で切った。`diag … : count` の例は、片側にだけある診断を示す。

**walk: type** (371)

| file | 位置 | Pointer | Store |
| --- | --- | --- | --- |
| `fixtures/compiler/builderPublic.ts` | type walk #24 at 568 Identifier "createHash" | `((data: string) => string) \| undefined` | `(data: string) => string` |
| `fixtures/compiler/builderStatePublic.ts` | type walk #12 at 325 Identifier "data" | `any` | `WriteFileCallbackData` |
| `fixtures/compiler/debug.ts` | type walk #3169 at 44793 Identifier "node" | `any` | `Node` |

**diag file: count** (225)

| file | Pointer | Store |
| --- | --- | --- |
| `fixtures/compiler/builder.ts` | 136 件、Pointer だけ: 15688-15697 TS2339 `["fileInfos" "BuilderProgramState"]` ほか 3 | 152 件、Store だけ: 11809-11827 TS2322 `["true \| undefined" "true"]` ほか 19 |
| `fixtures/compiler/builderState.ts` | 15 件、Pointer だけ: 20214-20220 TS2322 `["readonly string[] \| undefined" "readonly string[]"]` ほか 1 | 16 件、Store だけ: 11883-11891 TS2345 `["Readonly<BuilderState> \| undefined" …]` |
| `fixtures/compiler/checker.ts` | 788 件、Pointer だけ: 453838-453839 TS7006 `["e" "any"]` ほか 1 | 786 件、Store だけ: なし |

**diag suggestion: count** (81)

| file | Pointer | Store |
| --- | --- | --- |
| `fixtures/compiler/core.ts` | 1 件、Pointer だけ: なし | 5 件、Store だけ: 73920-73939 TS6133 `["compareWithCallback"]` ほか 3 |
| `fixtures/compiler/expressionToTypeNode.ts` | 0 件 | 1 件、Store だけ: 59918-59919 TS6133 `["m"]` |
| `fixtures/compiler/factory/nodeFactory.ts` | 4 件、Pointer だけ: なし | 44 件、Store だけ: 12353-12361 TS6133 `["operator"]` ほか 39 |

**diag file: MessageArgs** (30)

| file | 位置 | Pointer | Store |
| --- | --- | --- | --- |
| `assignmentCompatBug2.ts` | #3 (TS2741) | `… n?: number \| undefined; k?(a: any): any; }` | `… n?: number; k?(a: any): any; }` |
| `contextualTypeFunctionObjectPropertyIntersection.ts` | #0 (TS2353) | `… { "*"?: Action<…> \| undefined; }` | `… { "*"?: Action<…>; }` |
| `dataViewConstructor.ts` | #0 (TS2345) | `ArrayBufferLike & { BYTES_PER_ELEMENT?: undefined; }` | `ArrayBufferLike & { BYTES_PER_ELEMENT?: never; }` |

**diag file: MessageChain.MessageChain.MessageArgs** (11)

| file | Pointer | Store |
| --- | --- | --- |
| `assignmentCompatability11.ts` | `["string \| undefined" "number"]` | `["string" "number"]` |
| `assignmentCompatability15.ts` | `["string \| undefined" "boolean"]` | `["string" "boolean"]` |
| `assignmentCompatability17.ts` | `["string \| undefined" "any[]"]` | `["string" "any[]"]` |

**diag file: MessageChain.Code** (9)

| file | Pointer | Store |
| --- | --- | --- |
| `assignmentCompatability13.ts` | 2326 (Types of property are incompatible) | 2327 (Property is optional in type but required in type) |
| `assignmentCompatability27.ts` | 2326 | 2327 |
| `assignmentCompatability39.ts` | 2326 | 2327 |

**synthetic import (stage 2)** (8): 全部 `storechecker.(*Checker).getJSXRuntimeImportSpecifier` から。file は §7。

**diag file: RelatedInformation.MessageArgs** (5)

| file | Pointer | Store |
| --- | --- | --- |
| `assignmentCompatFunctionsWithOptionalArgs.ts` | `["name" "{ id: number; name?: string \| undefined; }"]` | `["name" "{ id: number; name?: string; }"]` |
| `contextualTypingGenericFunction2.ts` | `… unrelated?: ((arg: string) => void) \| undefined; }` | `… unrelated?: (arg: string) => void; }` |
| `stripMembersOptionality2.ts` | `["b" "ToStringOrUnd<{ a: 1; b?: 2 \| undefined; }>"]` | `["b" "ToStringOrUnd<{ a: 1; b?: 2; }>"]` |

**diag file: MessageChain.MessageArgs** (3)

| file | Pointer | Store |
| --- | --- | --- |
| `indexedAccessRelation.ts` | `["Foo" "S[\"a\"] & (T \| undefined)"]` | `["Foo" "S[\"a\"] & T"]` |
| `classIsSubtypeOfBaseType.ts` | `["{ bar?: string \| undefined; }" "{ bar: string; }"]` | `["{ bar?: string; }" "{ bar: string; }"]` |
| `recursiveTypeReferences2.ts#bug39372.js` | `… { $$?: Record<string, string> \| undefined; } …` | `… { $$?: Record<string, string>; } …` |

**diag file: MessageChain.MessageChain.Code** (2): `symbolProperty10.ts`、`subtypingWithObjectMembers3.ts`。Pointer 2322、Store 2741 (Property is missing in type but required)。

**store panic: … findResolutionCycleStartIndex** (1): `circularOptionalityRemoval.ts`。`runtime error: comparing uncomparable type store.Node`。

**diag suggestion: Pos** (1): `declarationsForFileShadowingGlobalNoError.ts#index.ts`。Pointer の 2 件目は 197 (`domNode`)、Store は 41 (`DOMNode`、Store だけの 6133)。

**diag file: RelatedInformation.count** (1): `nestedFreshLiteral.ts`。TS2561 の related が Pointer 0、Store 1。

**diag file: Code** (1): `destructuringParameterDeclaration8.ts`。#1 が Pointer 2339 (`["p" "{ p: \"a\" | \"b\"; } | undefined"]`)、Store 2322。

全部の例 (3 件ずつ) は `_store-checker-7d2b1-impl/examples.md`。

### 3.3 原因ごとの帰属 (overlay の実験)

§3.4 の読みを確かめるため、repo を変えずに `go test -c -overlay` で store の 2 つの関数だけを差し替えた binary を scratchpad に作り、corpus を回した。**直したのではない** (差し替えた版は `_store-checker-7d2b1-impl/overlay/`)。

| 差し替え | equal | 残った class |
| --- | ---: | --- |
| なし (run 5) | 16,665 | 15 class |
| `QuestionToken` の fallback だけ | 17,172 | count 147・suggestion count 85・synthetic 8・suggestion Pos 1・panic 1 |
| `OrderedNodeSet.Values` だけ | 16,849 | walk: type 409 ほか 12 class |
| 両方 | 17,404 (99.9%) | synthetic 8・panic 1・count 1 (§4 の非決定) |

run 5 の不一致の file を、どの差し替えで equal になるかで分けた:

| class | file | QuestionToken だけで | OrderedNodeSet だけで | 両方で | どちらでも直らない |
| --- | ---: | ---: | ---: | ---: | ---: |
| walk: type | 371 | 371 | 0 | 0 | 0 |
| diag file: count | 225 | 75 | 124 | 26 | 0 |
| diag suggestion: count | 81 | 0 | 59 | 22 | 0 |
| diag file: MessageArgs | 30 | 29 | 0 | 1 | 0 |
| MessageChain / RelatedInformation / Code の 6 class | 32 | 32 | 0 | 0 | 0 |
| synthetic import | 8 | 0 | 0 | 0 | 8 |
| store panic | 1 | 0 | 0 | 0 | 1 |
| diag suggestion: Pos | 1 | 0 | 1 | 0 | 0 |
| 計 | 749 | 507 | 184 | 49 | 9 |

§4 の非決定の 1 file は、この表では「QuestionToken だけで」に入っている (その実行で偶然 equal だった)。

### 3.4 上位 10 class の原因の読み

| # | class | 疑わしい元 | 読み |
| --- | --- | --- | --- |
| 1 | walk: type (371) | **store の API** | `store.Node.QuestionToken()` (views_generated.go、生成) は `questionTokenSlot` の表だけを引く。Pointer の `(*Node).QuestionToken()` (ast.go:1094-1110) は Parameter・ConditionalExpression・MappedType・NamedTupleMember 以外で `PostfixToken()` が `?` ならそれを返す。そのため `store.HasQuestionToken` (と `isOptionalDeclaration`、checker/utilities.go:298 の写し) が `PropertySignature`・`PropertyDeclaration`・`MethodSignature` などの `?` で偽になり、optional な property の型に `\| undefined` が付かない。walk: type の 371 の内訳は、`\| undefined` の欠落 355、unresolved な型の optional (Pointer `any`、Store 型名) 9、`undefined` と `never` 5、入れ子の `\| undefined` 1、`true` と `false` (optional の有無を問う条件型) 1。overlay で 371 全部が equal になる |
| 2 | diag file: count (225) | **store の API** (2 つ) | 124 は `OrderedNodeSet.Values()` (checker_api.go:290、2a の手書き)。`range s.keys` で反復の開始時の slice しか回らない。Pointer の `collections.OrderedSet.Values()` は `OrderedMap.Keys()` (ordered_map.go:129-143) で、「反復中に足された要素も数える」ため index で回す。`checkDeferredNodes` (checker.go:2534) は反復しながら `checkDeferredNode` が新しいノードを足すので、Store では後回しの中で後回しにされたノード (関数式・arrow・JSX 要素など) が検査されない。欠ける診断は 17004・7026 (JSX)、7006、18048・2532・2454・2683 など。75 は #1 と同じ QuestionToken (Store だけの 2564・2322・2790 など、optional を required と見る) |
| 3 | diag suggestion: count (81) | **store の API** (#2 の OrderedNodeSet) | 検査されないノードの中の参照が「使われた」と記録されず、Store だけに 6133 (declared but never read) が出る。79 が Store だけの 6133 |
| 4 | diag file: MessageArgs (30) | store の API (#1) | message の型の文字列で optional の `\| undefined` が欠ける、`undefined` が `never` になる |
| 5 | MessageChain.MessageChain.MessageArgs (11) | store の API (#1) | 同上 (連鎖の奥の型) |
| 6 | MessageChain.Code (9) | store の API (#1) | optional を required と見るので、関係の失敗の理由が 2326 ではなく 2327 になる |
| 7 | synthetic import (8) | ハーネス (段 2、設計どおり) | `@jsxImportSource` / `@jsxRuntime` の pragma で、段 1 の option でも Pointer の program が JSX runtime の合成の import を作る (fileloader.go:838-848 の `GetJSXImplicitImportBase` は pragma を見る)。指示書 §5.2 の「作られない見込み」は pragma を数えていなかった |
| 8 | RelatedInformation.MessageArgs (5) | store の API (#1) | #4 と同じ |
| 9 | MessageChain.MessageArgs (3) | store の API (#1) | #4 と同じ |
| 10 | MessageChain.MessageChain.Code (2) | store の API (#1) | 2322 ではなく 2741 (Property is missing but required) |
| 11 | store panic (1) | **変換の規則** | `TypeSystemEntity` は `any`。`pushTypeResolution(declaration, TypeSystemPropertyNameInitializerIsUndefined)` (checker.go:11416) が `store.Node` を入れ、`findResolutionCycleStartIndex` の `resolution.target == target` (checker.go:19121) が interface の比較で uncomparable な `Node` に当たって panic する。R6 は generic の comparable の制約を型で見るので、interface の `==` に `Node` が入る箇所を見ない |

直し方の案 (直していない):

1. QuestionToken: 生成器 (generate-go-store.ts) が、Pointer の手書きの多相 accessor の fallback を持つもの (QuestionToken など) を同じ形で出す。2b-2 の最初に、Pointer の `(*Node)` の手書きの accessor で `switch` の後に fallback を持つものを全部洗い、生成した多相 accessor と照合する試験を足す。生成器が slot の無い kind に黙って nil を返したことが、この欠落を隠していた。
2. OrderedNodeSet: `Values()` を index で回す。`NodeSet`・`OrderedNodeSet` のほかの method も collections の対応と照合し、反復中の追加の試験を足す。
3. panic: `findResolutionCycleStartIndex` を P2 にするか、`TypeSystemEntity` どうしの `==` を Node なら `SameNode` で比べる helper に写す規則を足す。同じ形 (interface に Node が入って `==`・map の key) がほかにあるかを 2b-2 で洗う。
4. 合成の import: 段 2 の指示書で adapter に Store のノードを作る (予定どおり)。

## 4. 非決定の file

`tests/cases/compiler/jsxNamespaceImplicitImportJSXNamespace.tsx#/node_modules/preact/jsx-runtime/index.d.ts` は実行ごとに class が変わる。単独で 6 回回して `walk: type` 2 回・`diag file: MessageArgs` 4 回。全体の実行では `diag file: count` (run 4・5・テスト一式の tag なし)、`diag file: MessageArgs` (テスト一式の storechecks)、`equal` (overlay の QuestionToken だけの回)。

- この file は `/index.d.ts` として置かれ、`'..'` から自分自身を import する。TS2460 (declares locally, but it is exported as) の 3 つ目の引数を `findInMap(moduleSymbol.Exports, …)` (checker.go:15158) で探す。
- `findInMap` は Go の map を range するので、同じ symbol を別名で export している 2 つ (`Fragment`・`JSX`) のどちらを返すかが実行ごとに変わる。**Pointer 側も変わる** (単独の 6 回のうち、MessageArgs で止まった 4 回の Pointer の値は `"Fragment"` 2 回、`"JSX"` 2 回)。alias の解決の順も変わり、Store だけに TS2303 が 1 件多い回がある。
- 上流 (Pointer) の非決定なので直すものではないが、2b-2 の strict な mode ではこの file の扱いを決める必要がある。

## 5. 時間と RSS

この機械 (M1、8GB) で、worker 4。1MB 以上の file (checker.ts・dom.generated.d.ts) は他と並べずに単独で走らせる (§9 の 2)。load average は測定の間 9〜40 と高かった (別のプロセス)。

| 実行 | file | wall | file あたりの wall | 最大 RSS | file の時間 (中央値 / p99 / 最大) |
| --- | ---: | --- | ---: | ---: | --- |
| sample 500 (§9 の修正の前) | 500 | 4s | 8.0 ms | 0.35 GB | 30ms / 90ms / 321ms |
| sample 500 (修正の後) | 500 | 5s | 9.8 ms | 0.30 GB | 36ms / 100ms / 445ms |
| 全体 run 4 | 17,415 | 5m12s | 17.9 ms | 2.19 GB | 44ms / 191ms / 1m5s |
| 全体 run 5 | 17,415 | 6m33s | 22.6 ms | 2.28 GB | 50ms / 625ms / 1m20s |
| 全体 (テスト一式の中、tag なし) | 17,415 | 6m4s | 20.9 ms | 2.12 GB | 49ms / 161ms / 1m7s |
| 全体 (テスト一式の中、storechecks) | 17,415 | 6m56s | 23.9 ms | 2.01 GB | 50ms / 225ms / 1m24s |

- sample からの見込みは 17,415 × 9.8 ms ≈ 2.8 分、RSS は 0.3 GB で、指示書の線 (60 分、6GB) の内側だったので全体を回した。
- **sample は大きい file を外すので、RSS を過小に見積もる。** 固定の間隔で間引いた 500 file に 1MB 超の fixture は入らない。全体の最大 RSS は checker.ts (3.1MB、単独で約 2GB) で決まる。最長の file も checker.ts (1〜1.3 分)。
- 最初の全体の実行は 20 秒で OOM kill された (peak memory footprint 43GB)。原因はハーネス (§9 の 1)。

## 6. §8 のテスト

### 6.1 mode の単体等価 (§8 の 1、`TestModeCorpus`)

17,415 file (Pointer の program が panic する 1 file を除く)、module specifier の位置 4,996、**不一致 0**。

| 位置 | 数 |
| --- | ---: |
| import (ImportDeclaration・JSImportDeclaration) | 2,985 |
| import= (external module reference) | 687 |
| export | 482 |
| import type | 340 |
| import() | 325 |
| require | 147 |
| JSDoc import (JSDocImportTag、JSDoc の木を host ごとに対にして) | 30 |

mode は ESNext 3,885・CommonJS 1,111。mode が emit syntax と違う位置 (resolution-mode の override が効いた位置) は 28。段 1 の option (bundler 解決) では `importSyntaxAffectsModuleResolution` が真で、`getEmitSyntaxForUsageLocationWorker` の枝を通る。`declare module "x"` の名前 (module augmentation) は指示書の対象の列挙に無いので入れていない。

### 6.2 private の fixture (§8 の 2、`TestPrivateFixtures`)

6 本とも `equal`。各 fixture が意図した経路を通ることを、Pointer の診断の有無で確かめる (2・4・6 は診断が出ること、ほかは出ないこと)。

| fixture | Pointer の診断 |
| --- | --- |
| 1-two-files (a.ts・b.ts、各 file の最初の class) | なし。`A.#x` と `B.#x` が別の symbol として対になる |
| 2-nested | 18014 (内側の `#x` が外側を隠す) |
| 3-same-name | なし |
| 4-static-instance | 2339 (`C.#i`、instance の private を class から) |
| 5-derived | なし。`Base.#x` と `Derived.#x` が別 |
| 6-invalid-access | 18013 × 4 |

### 6.3 Pointer 側の walker の検定 (§8 の 4、`TestPointerWalkerBaselines`)

- **指示書の条件 (option が無く、unit が 1 つで、`.types` と `.symbols` の reference がある) に当たる test は 14 本しかない。** 14 本で不一致 0。
  - tests/cases/compiler の 6,832 本のうち、`// @` の option の行を持たないのは 14 本だけ。6,515 本が `@target` を持つ (既定の target が変わったのに合わせて付けたもの、と読む)。
  - 14 本では、walker の写しの規則を確かめたとは言いにくい。
- 判断の材料として、`STORE_CHECKER_BASELINE_OPTIONS=1` で条件を広げる mode を足した (指示書に無い、§8 の 13)。option を harnessutil と同じく既定の後に適用し、構成が 1 つで harness の option (`@noTypesAndSymbols` など) を持たない test。**4,671 本で不一致 2**。
  - 2 本とも `// @ts-ignore` で program が診断を捨てる test (`elidedEmbeddedStatementsReplacedWithSemicolon.ts`、`jsxLibraryManagedAttributesUnusedGeneric.tsx`)。baseline の `hadErrorBaseline` は program の後処理の後の診断の件数 (compiler_runner.go:527) なので偽になり、any の型を intrinsic の名前 `error` で印字する。driver は後処理の前の件数なので真になり、nodebuilder を通って `any` になる。**baseline の側の条件なので、緩めずに報告する。**
  - emit の後に walk することによる差は、4,671 本では出なかった。
- 照合の読み方:
  - test は testrunner と同じく `osvfs` で読む (BOM を落としてから option を解釈するため)。最初に `os.ReadFile` で読んだときは、BOM の後ろの `// @target` を option と見ずに中身の 1 行目に数え、131 本を選んで 111 本が不一致になった (見た例は全部、行番号が 1 つずれていた)。
  - 内容に `/.lib/` を含む test は除く。harnessutil はその test にだけ test 用の lib を FS に足す (`includeLibDir`)。広げた mode で 49 本がこれだった。
  - 記録の値が改行を含むとき (source の文字列が symbol の名前に入る)、baseline の `>` の行はその 1 行目なので、1 行目で比べる。

## 7. 報告の個別の項目

### 7.1 §4 で足した根

`checkerRoots` (reach.go) に 3 つ。

| 根 | 理由 | 出た生成物 |
| --- | --- | --- |
| `checker.Checker.SymbolToStringEx` | symbol の walk (指示書 §4) | `storechecker/printer.go` に 4 行 |
| `checker.IntrinsicType.IntrinsicName` | any の型の短絡 (`t.AsIntrinsicType().IntrinsicName()`) | `storechecker/types.go` に 2 行 |
| `printer.EmitContext.Reset` | Pointer の walker は node ごとに pool から EmitContext を取り、Reset して使う。Store の walker は 1 つを node ごとに Reset する (`NewEmitContextIn` は node ごとに Store を確保するので使わない) | `storeprinter/emitcontext.go` に 6 行 |

ほかの生成物は変わらない (`git diff --stat 5c0e86b580` で storechecker の外の差は `storeprinter/emitcontext.go` だけ)。

### 7.2 §6.3 で手書きした述語

`walker.go`。Pointer の元と同じ判定の順に Store の述語で書いた。

| 述語 | 元 |
| --- | --- |
| `getMeaningFromDeclaration` | ast/utilities.go:2250 `GetMeaningFromDeclaration` |
| `isLabelName`・`isLabelOfLabeledStatement`・`isJumpStatementTarget` | ast/utilities.go:2312-2338 (`IsBreakOrContinueStatement` は `store.NodeKindIs` で書いた) |
| JsxClosingElement の判定 | `ast.IsJsxClosingElement` が store に無いので、`isIntrinsicJsxTag` の中で `Kind()` で判定 |
| `isImportStatementName`・`isExportStatementName`・`isIntrinsicJsxTag` | tsbaseline の同名の関数 (walker 自身の helper) |
| `isDefaultLibraryFile` | tsbaseline/util.go の写し (文字列だけ) |

指示書が例に挙げた `GetSourceTextOfNodeFromSourceFile` は `storescanner` にある (変換器の出力) ので、それを使った。`GetSourceFileOfNode` は `Node.File()`。

### 7.3 §5.2 の A の委譲先

`FileName()` と `Path()` 以外を読む委譲先は **無い**。HasFileName を取る委譲先を読んだ。`GetEmitModuleFormatOfFile`・`GetImpliedNodeFormatForEmit`・`GetDefaultResolutionModeForFile` (FileName・Path と `getCompilerOptionsForFile`)、`GetResolvedModule` (Path)、`GetRedirectForResolution` (`getRedirectForResolution`: Path、`getSourceToDtsIfSymlink` で FileName)、`GetSourceOfProjectReferenceIfOutputIncluded` (Path・FileName)。

### 7.4 合成の import の panic

**8 件**。全部 `GetJSXRuntimeImportSpecifier` (import helpers は 0)。

- `jsxImportSourceNonPragmaComment.tsx`
- `jsxNamespaceImplicitImportJSXNamespaceFromPragmaPickedOverGlobalOne.tsx#/index.tsx`
- `jsxRuntimePragma.ts#two.tsx`、`#three.tsx`
- `jsxJsxsCjsTransformCustomImportPragma.tsx#preact.tsx`、`#react.tsx`
- `jsxJsxsCjsTransformKeyPropCustomImportPragma.tsx#preact.tsx`、`#react.tsx`

## 8. 指示書に無い判断

1. `getFlowTypeOfReferenceUnderCondition` は `getFlowTypeOfReferenceEx` の `flowNode == nil` の枝を写していない。合成の flow は nil にならず、Pointer でもこの枝は通らない。
2. `getTypeAtSyntheticCondition` は `getTypeAtFlowCondition` の本体を、その場で呼ぶ closure に入れた。早期の return を return のまま保つため。4 つ目の関数 (P2 の数が増える) は避けた。
3. §4 の根に `IntrinsicType.IntrinsicName` と `printer.EmitContext.Reset` を足した (指示書 §4 の「同じ理由で足りない export」)。
4. store-only の P2 の本体は、到達したときだけでなく、手書きの file と同じく常に辿る。store-only の P2 には Pointer の宣言が無く、到達した宣言から名前で辿る口が無いため。
5. Pointer の lib の cache は bundled の lib の file だけ (harnessutil の cache は全 file)。指示書の「lib の SourceFile を使い回す」の読み。corpus の file まで保持すると 17k 本の Pointer の AST が残る。
6. `hadErrorBaseline` の件数: 各側の root の file の parse の診断 (`Diagnostics`・`JSDiagnostics`)・bind の診断・checker の file の診断・global の診断のどれかが 1 件以上。testrunner の件数 (program の診断) に近づけるため parse と bind を入れた。comment directive による除外はしない (H1)。
7. walk の順は、全 root の型の walk の後に全 root の symbol の walk (tsbaseline の type の baseline、次に symbol の baseline と同じ順)。
8. walk の記録は、`writeTypeOrSymbol` が何も返さない node も `present: false` で持つ。「node set」(訪問した node の違い) と「type / symbol」(同じ node で結果の違い、nil を含む) を分けるため。§8 の 4 の照合は present のものだけ。
9. 分類の鍵の細部:
   - 比べる順は、root ごとに file の診断・suggestion、global、型の walk、symbol の walk、symbol の対応。
   - 件数は field より先に比べる。入れ子の field は `MessageChain.MessageArgs` のように path で、入れ子の件数は `MessageChain.count`。
   - ID の token だけの差を見つけても、後ろに本当の差が無いかを探し続ける。無いときだけ `internal id token`。
10. JSONL の `file` は `testdata` からの path と `#unit`。`ms` (file の時間) も持つ。
11. `pointerwalker.go` を足した (指示書の file の表は Store 側の `walker.go` だけ)。
12. `TestCheckerCorpus` に env を足した: `STORE_CHECKER_CORPUS_WORKERS`、`STORE_CHECKER_CORPUS_TRACE` (file の開始と終了を unbuffered で書く。死んだ実行の原因の特定に使った)、`STORE_CHECKER_CORPUS_FILTER` (origin の部分文字列で絞る)。
13. §8 の 4 の広げた mode (`STORE_CHECKER_BASELINE_OPTIONS=1`、§6.3)。既定の条件は指示書のまま。
14. §8 の 4 の照合: test を `osvfs` で読む、`/.lib/` を含む test を除く、改行を含む値は 1 行目で比べる (§6.3)。
15. §8 の 1 で mode が emit syntax と違う位置の数も log する (override の枝を通っていることを示すため)。

## 9. 実装中に見つけたハーネスのバグ (直した)

1. **型の文字列が printer の buffer を抱える。** `printer.Write` は source file があると writer を file の長さだけ `Grow` する (Pointer の printer.go:5102-5106 と、その変換の storeprinter)。`writer.String()` はその buffer を共有するので、記録に残した型の文字列 1 つが file の長さ (checker.ts なら 3MB) を保持する。checker.ts の数十万の記録で peak memory footprint が 43GB になり、20 秒で kill された。記録は `strings.Clone` した文字列を持つ。tsbaseline は小さい file で baseline をすぐ作るので問題にならない。walker の規則は変わらない。
2. **1MB 以上の file を並べると 8GB に入らない。** checker.ts と dom.generated.d.ts は単独で約 2GB。重み付きの semaphore で、1MB 以上の file は worker の全部を取って単独で走る。
3. **Pointer の program の作成の panic を拾っていなかった。** `pathMappingBasedModuleResolution_rootImport_aliasWithRoot_differentRootTypes.ts#/root/a.ts` で vfs が panic し、ハーネスのエラーになった。program の作成も Pointer 側として recover し、「Pointer panics」の除外にした (指示書 §7.1 の 2 の理由)。
4. **§8 の 4 の選別で BOM を落としていなかった** (§6.3)。

## 10. 判断を仰ぐこと

1. **§8 の 4 の条件。** 指示書どおりだと 14 本。広げた mode (4,671 本、不一致は `@ts-ignore` の 2 本) を既定にするか。するなら、`@ts-ignore` の 2 本をどう扱うか (driver の `hadErrorBaseline` に comment directive を反映するか、この 2 本を理由つきで除くか)。
2. **2b-2 の最初の 2 件。** §3.4 の QuestionToken と OrderedNodeSet は、どちらも store の API で、直すと不一致の 98.7% (739 / 749) が消える見込み (overlay で確かめた)。2b-2 の指示書で、この 2 件と「生成・手書きの API を Pointer の手書きと照合する試験」を先に入れるか。
3. **非決定の file** (§4)。Pointer 自身が非決定なので、2b-2 の strict な mode で除外の理由を足すか。
4. **合成の import の見込み** (§3.4 の 7)。段 1 の option でも pragma で 8 件出る。段 2 の範囲のままでよいか。

## 11. 再現の手順

```sh
ROOT=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript
cd $ROOT && node tools/scripts/tsc/generate.ts && (cd tools && go run ./cmd/storetranslate) && git status --short
cd $ROOT/tools && GOWORK=off go test -count=1 ./cmd/storetranslate/
cd $ROOT/tsc && GOWORK=off go test -count=1 -timeout 60m -v ./internal/ast/store/... ./internal/store...
# sample と全体 (JSONL は scratchpad へ)
cd $ROOT/tsc && GOWORK=off go test -c -o $SCR/storeharness.test ./internal/storeharness/
cd $ROOT/tsc/internal/storeharness && STORE_CHECKER_CORPUS_SAMPLE=500 $SCR/storeharness.test -test.run TestCheckerCorpus -test.v
cd $ROOT/tsc/internal/storeharness && STORE_CHECKER_CORPUS_OUT=$SCR/corpus.jsonl $SCR/storeharness.test -test.run TestCheckerCorpus -test.v -test.timeout 120m
# §8 の 4 の広げた mode
cd $ROOT/tsc && STORE_CHECKER_BASELINE_OPTIONS=1 GOWORK=off go test -count=1 -run TestPointerWalkerBaselines -v ./internal/storeharness/
# §3.3 の overlay (repo を変えない)
cd $ROOT/tsc && GOWORK=off go test -c -overlay $ROOT/tsc/internal/ast/docs/_store-checker-7d2b1-impl/overlay/overlay-both.json -o $SCR/storeharness-both.test ./internal/storeharness/
```

overlay の JSON は `_store-checker-7d2b1-impl/overlay/` の中の差し替えの file を絶対 path で指す。差分は同じ場所の `*.diff`。
