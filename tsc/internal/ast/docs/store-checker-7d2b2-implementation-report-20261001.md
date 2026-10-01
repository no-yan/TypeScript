# Store checker 等価 段 1 の修正と strict (TODO 7d-2b-2) 実装報告

作成日: 2026-10-01。指示書は [store-checker-7d2b2-implementation-instructions-20261001.md](store-checker-7d2b2-implementation-instructions-20261001.md) (以下「指示書」)。before は **830b69fbf1**。commit はしていない (作業ツリーの変更のまま)。生の出力は `_store-checker-7d2b2-impl/` (ignore 対象、ローカルのみ)。

## 1. 結論と完了条件ごとの結果

- **段 1 の corpus が strict で通った。** 比較した 17,405 file が全部 `equal`。除外は 10 file で、見込みどおり 4 つの理由だけ (合成の import 8・Pointer の非決定 1・Pointer panic 1・root 無し 0)。
- ID の token の出現は 0。`CompareSymbols` の性質の違反は 0 (各側 554,158 組を検査し、そのうち 406 組が ID の fallback に届く)。
- §4.1 の多相 accessor の照合で、**指示書が見込んでいない不一致が 2 つ見つかった** (QuestionToken のほかに `Type` と `TypeExpression`)。`TypeExpression` は指示書が「直さずに止める」とした形 (Pointer が panic し、Store が値を返す) だった。止めて判断を仰ぎ、2 つともユーザーの決定 (2026-10-01) で直した (§3.4)。
- ハーネスは、ID の数字を消して比べるのをやめた。代わりに token を数え、非決定は Pointer を走らせ直して判定する。

| # | 完了条件 (指示書 §9) | 結果 |
| --- | --- | --- |
| 1 | 生成物の再現、build、store 系・storeharness の vet | 合格。2 つの生成器を回した前後で `git status` と diff の sha1 が変わらない (§7)。`go build ./...`、`go vet ./internal/ast/store/... ./internal/store...`、`(cd tools && go vet ./cmd/storetranslate/...)` が通る |
| 2 | 既存のテストが両方の tag で通る | 合格。tag なしと storechecks の両方で、失敗は既知の a708 の 1 件だけ (§8) |
| 3 | §4.1 が不一致 0、§4.2 が通る。どちらも修正の前に失敗することを確かめてある | 合格。修正前: §4.1 は 5,710 件 (10 の (method, kind))、§4.2 は `OrderedNodeSet` の 3,000 列のうち 1,877 列 |
| 4 | `TestCheckerCorpus` が strict で通る | 合格 (§5)。除外は 4 つの理由だけで、件数は見込みどおり。token 0、性質の違反 0 |
| 5 | `TestModeCorpus`・`TestPrivateFixtures` が今のまま通る。`TestPointerWalkerBaselines` が §7 の条件で不一致 0 | 合格。baseline は 4,671 本で不一致 0 (§6) |
| 6 | 変換器のテスト | 合格 (`go test ./cmd/storetranslate/`) |

## 2. §3 の修正

### 2.1 QuestionToken (指示書 §3.1)

- Pointer: `(*Node).QuestionToken()` (ast/ast.go:1094-1110)。
- Store: `checker_api.go` に手書きした。`Attributes`・`CanHaveStatements` の並びに置いた。
  - Parameter・ConditionalExpression・MappedType・NamedTupleMember は型付きの view の `QuestionToken()` を返す。
  - それ以外は、`PostfixToken()` が nil でなく kind が `KindQuestionToken` ならそれを返し、そうでなければ nil。
- 生成器: 飛ばす名前を `handWrittenRoles` という集合にし、`QuestionToken` を足した。今までの `if (r.name === "Text") continue` と同じ仕組み。理由は集合の comment に書いた。
- `questionTokenSlot` の表は残した。型付きの view の `QuestionToken()` も今のまま生成される。
- 見落とした原因: 生成された `compareRoles` は、Store に slot のある kind でしか Pointer の同名の method と比べない。`PropertySignature` などの、slot が無い kind で Pointer が fallback を返す場合は、比較の外だった。§4.1 の照合がこの穴を塞ぐ (修正前に 6 kind、4,540 件を検出した)。

### 2.2 OrderedNodeSet.Values (指示書 §3.2)

- Pointer: `collections.OrderedSet.Values()` → `OrderedMap.Keys()` (collections/ordered_map.go:129-143)。
- Store: `range s.keys` を、反復のたびに長さと要素を読む index の loop に替えた。ほかの method は変えていない。
- §4.2 の試験で、ほかの method が `collections.Set` / `collections.OrderedSet` と同じ意味であることを確かめた。違いは `Values` だけだった。
  - 対象は `NodeSet` の全 method と、`OrderedNodeSet` の `Add`・`Has`・`Delete`・`Clear`・`Size`。
  - `Delete` は実装が違う (Store は `slices.DeleteFunc`、Pointer は `slices.Index` で先頭と末尾を特別に扱う)。反復中の `Delete` を含む列で、出る要素の列が一致した。
  - nil の受け手でも、各 method の振る舞いが一致した (panic するかどうかと、返す値)。

### 2.3 findResolutionCycleStartIndex の P2 (指示書 §3.3)

- Pointer: checker/checker.go:19115。hash は `7215403e5af99e9c` で、2026-10-01 に `-hash` で取り直しても同じだった。
- Store: `storechecker/checker_manual.go`。文の順序と early return は Pointer のまま。`resolution.target == target` だけを次に置き換えた。
  - `resolution.target` が `store.Node` のとき: `target` も `store.Node` で、かつ `SameNode`。
  - それ以外: `==` (dynamic type が違えば false で、panic しない)。
- 補助の関数は作っていない。
- 同じ形の箇所は新たには探していない。2b-1 の検証で go/types が数えた結果に頼った。

### 2.4 指示書の外の 2 つの accessor (§4.1 で見つけ、ユーザーの決定で直した)

修正前の §4.1 で、QuestionToken のほかに次の不一致が出た。

| method | kind | 件数 | Pointer | Store (修正前) | 指示書 §4.1 の形 |
| --- | --- | ---: | --- | --- | --- |
| `Type` | JSDocParameterTag | 906 | `TypeExpression` を返す (ast.go:669-670) | nil (`typeSlot` は名前が `Type` の member だけ) | 「Pointer が値、Store が違う」(直してよい) |
| `Type` | JSDocPropertyTag | 199 | 同上 | 同上 | 同上 |
| `TypeExpression` | JSDocOverloadTag | 48 | panic (ast.go:1126-1143 に case が無い) | member を返す | **「それ以外」(止めて報告)** |
| `TypeExpression` | JSDocThisTag | 17 | 同上 | 同上 | 同上 |

止めて判断を仰ぎ、次のとおり決まった (2026-10-01、ユーザー)。

1. **`Type` は、表に例外を足して手書きする。**
   - Pointer の switch の形 (約 25 の case と FunctionLikeData の既定) にはしない。`Type()` はよく呼ばれる accessor なので、生成版と同じく表を 1 回引く形を保つ。
   - 実装は `store.go` の `typeRoleSlot` (`typeSlot` を写し、JSDocParameterTag と JSDocPropertyTag に `TypeExpression` の slot を足した表) を引く。
   - 最初に書いた形 (`typeSlot` を引いた後に、2 kind を比べる枝を足す) は、inline の cost が 104 で budget の 80 を超えた。2 つ目の表を引く形も 88 だった。表を 1 つにまとめると、生成版と同じ 75 で inline される。
   - 生成された表は、生成した file の `init()` で埋まる。`typeRoleSlot` は `store.go` の `init()` で写す。Go は package の `init()` を file 名の順に走らせ、`shapes_generated.go` は `store.go` より前に来る。この順の前提は単体の試験 `TestTypeRoleSlot` (`roles_test.go`) で確かめる。
2. **`TypeExpression` は、Pointer と同じ形で手書きする。** case に無い kind では同じ message で panic する。
   - 照合では、両方が panic して一致する。
   - Store の production のコードが多相の `TypeExpression()` を呼ぶのは、Pointer が同じ所で呼ぶ kind の上だけだった (reparser は Pointer の写し)。なので、panic まで写しても parser は壊れない。既存のテストも通る (§8)。

生成器の `handWrittenRoles` は `Text`・`QuestionToken`・`Type`・`TypeExpression` の 4 つになった。

inline の判定 (`-gcflags=-m=2`):

| accessor | before (生成) | after |
| --- | --- | --- |
| `Type` | 75、inline | 75、inline (`typeRoleSlot`) |
| `QuestionToken` | 75、inline | 356、inline されない (Pointer と同じ switch) |
| `TypeExpression` | 75、inline | 496、inline されない (同上) |

QuestionToken はユーザーが「Pointer と同じ形」と決めたもの。性能は 2c で測る。

## 3. §4.1 多相 accessor の照合 (`storeharness/accessor_test.go`、`TestPolymorphicAccessors`)

- `*ast.Node` と `store.Node` の method を reflect で列挙する。method 式を、具体的な func の型 (`func(*ast.Node) *ast.Node` など) に型 assert して呼ぶ。知らない型の組が出たら `t.Fatal` する。
- **照合した method: 47**。
  - 同名で「引数なし・戻り値 1 つ」の組は、`As*` を除いて 50。そこから bind の結果の `Symbol`・`LocalSymbol`・`Locals` を除いた。
  - 照合した 47: ArgumentList Arguments Attributes Body CanHaveStatements Children ClassName Comments Decorators ElementList Elements End Expression ImportClause Initializer IsTypeOnly KindString Label MemberList Members ModifierFlags ModifierNodes Modifiers ModuleSpecifier Name ParameterList Parameters Pos PostfixToken Properties PropertyList PropertyName PropertyNameOrName QuestionDotToken QuestionToken RawText Statement StatementList Statements TagName Text Type TypeArgumentList TypeArguments TypeExpression TypeParameterList TypeParameters
- **片側だけの method**:
  - `*ast.Node` だけ (14): BodyData ClassLikeData CommentList DeclarationData ExportableData FlowNodeData FunctionLikeData IsJSDoc IterChildren KindValue LiteralLikeData LocalsContainerData SubtreeFacts TemplateLiteralLikeData
  - `store.Node` だけ (42): Comment Condition Constraint DotDotDotToken EndFlowNode EndFlowRef FallthroughFlowNode FallthroughFlowRef File Flags FlowNode FlowRef FullSignature HasBodyData HasClassLikeData HasDeclarationData HasExportableData HasFlowNodeData HasFunctionLikeData HasLiteralLikeData HasLocalsContainerData HasTemplateLiteralLikeData Head HeritageClauses IsNil Key Kind Left Literal Loc LocalSymbolId NextContainer Operand Parent Ref ReturnFlowNode ReturnFlowRef Right SymbolId TemplateSpans TypeParameter Types
- **対にしたノード: 2,639,008** (うち JSDoc 54,459)。corpus 17,415 file のうち、Pointer の program が panic する 1 file を除いた。
  - Pointer 側は corpus の driver と同じ program (`tryPointerProgram`) の root の file を使う。
  - Store 側は、その file だけを同じ parse の option で `storeparser.ParseSourceFileIn` で parse する。lib は parse しないので、比べるのは corpus の file のノードだけ。
  - JSDoc は host ごとに対にする。そのために mode_test.go の対の作り方を `filePairs` に切り出し、`specifierPairs` はその filter にした。
- **呼び出し: 各側 124,033,376 回**。

| 結果 | 修正前 | 修正後 |
| --- | ---: | ---: |
| Pointer panic・Store nil / 零 (一致扱い) | 76,114,267 | 73,479,053 |
| 両方 panic (一致扱い) | 2,633,133 | 5,268,412 |
| 不一致 | 5,710 | **0** |

- 両方 panic の増分は、`TypeExpression` が JSDoc でない kind で Pointer と同じく panic するようになった分。
- 両方 panic は指示書の表に無い。Store が Pointer の panic まで写した手書き (`Attributes`、`TypeExpression`) なので、一致として数えた (§9 の 4)。

**修正前の不一致** (5,710 件):

| method | kind | 件数 | 最初の file |
| --- | --- | ---: | --- |
| QuestionToken | PropertySignature | 4,086 | fixtures/compiler/builderPublic.ts at 568 |
| QuestionToken | MethodSignature | 368 | fixtures/compiler/builderPublic.ts at 1326 |
| QuestionToken | PropertyDeclaration | 62 | classUsedBeforeInitializedVariables.ts at 72 |
| QuestionToken | MethodDeclaration | 16 | declarationEmitComputedNameWithQuestionToken.ts at 114 |
| QuestionToken | PropertyAssignment | 5 | objectTypeWithOptionalProperty1.ts at 13 |
| QuestionToken | ShorthandPropertyAssignment | 3 | parserShorthandPropertyAssignment1.ts at 81 |
| Type | JSDocParameterTag | 906 | fixtures/compiler/moduleNameResolver.ts at 20737 |
| Type | JSDocPropertyTag | 199 | fixtures/typescript/Herebyfile.mjs at 5796 |
| TypeExpression | JSDocOverloadTag | 48 | jsDocOverloadAnonDefaultExport.ts#…js at 4 (Pointer panic、Store 値) |
| TypeExpression | JSDocThisTag | 17 | jsDocTypedefWithThisTag.ts#bug.js at 15 (同上) |

**「Pointer panic・Store 零」の (method, kind)**:

- 全部の組は log の表にある (`_store-checker-7d2b2-impl/accessor-after.txt`)。
- method ごとに、組の数と回数をまとめると次のとおり。どれも Pointer の accessor の switch に case の無い kind で、Store は生成の既定値 (nil・nil の List・`""`) を返す。

| method | kind の数 | 回数 |
| --- | ---: | ---: |
| ClassName | 270 | 2,638,947 |
| Children | 270 | 2,638,118 |
| ImportClause | 269 | 2,635,965 |
| Label | 269 | 2,635,864 |
| Statement | 265 | 2,634,150 |
| ModuleSpecifier | 268 | 2,634,493 |
| RawText | 269 | 2,633,820 |
| TagName | 247 | 2,628,714 |
| ElementList / Elements | 266 | 2,623,419 |
| PropertyList / Properties | 270 | 2,621,122 |
| Comments | 249 | 2,617,812 |
| MemberList / Members | 266 | 2,606,512 |
| ParameterList / Parameters | 258 | 2,570,997 |
| ArgumentList / Arguments | 270 | 2,560,171 |
| StatementList / Statements | 267 | 2,551,854 |
| TypeParameterList / TypeParameters | 252 | 2,544,581 |
| QuestionDotToken | 268 | 2,446,827 |
| TypeArgumentList / TypeArguments | 263 | 2,435,576 |
| Initializer | 261 | 2,420,498 |
| Expression | 237 | 2,280,283 |
| Text | 256 | 1,605,098 |

- 回数の上位 10 組は、どれも KindIdentifier の上 (853,018 回ずつ: TypeParameters・TypeParameterList・TypeArguments・TypeArgumentList・TagName・Statements・StatementList・Statement・RawText・QuestionDotToken)。
- 両方が panic するのは `Attributes` (269 kind、2,633,133 回) と `TypeExpression` (264 kind、2,635,279 回)。

**時間**: worker 4 で、修正前 21 秒 (テストの中の計測。プロセスの wall は 24.6 秒)、修正後 27 秒 (30.5 秒)。`-short` では走らない。

## 4. §4.2 collection の写しの試験 (`ast/store/collections_test.go`)

- 小さな source (`collections.ts`、class・関数・配列を含む 1 行) を Pointer で parse し、`convert.Convert` で Store にし、`storetest.Pairs` で対にした。そのうち 12 ノードを、決まった間隔で選んで使う。
- **seed は `20261001`、列は型ごとに 3,000 本**。1 本は 1〜30 の操作。
  - `NodeSet` の列は、`AddIfAbsent` を含む別の stream で作る。
  - 操作は `Add`・`Has`・`Delete`・`Len` / `Size`・`Clear`・`AddIfAbsent` (Set のみ)・反復。
  - 反復は、各歩の後に `Add`・`Delete`・`Clear`・何もしない、のどれかを行う。3 本に 1 本は途中で止める。
- **`OrderedNodeSet`** (`TestOrderedNodeSetIsOrderedSet`): 各操作の返り値と、反復で出た要素の列 (pool の index) を完全に比べる。反復は 64 歩で打ち切る (足し直しで終わらないことがあるため)。
- **`NodeSet`** (`TestNodeSetIsSet`):
  - Go の map は、反復中に足した要素を出すかどうかが決まらない。そのため、反復中に変更する列で出た要素の列をそのまま比べることはできない。そこで次のように比べた (§9 の 7)。
  - 変更は、両側が必ず通る歩 (反復の開始時の要素数の半分まで) にだけ置く。そうすると、反復の後の中身は決まる。それを集合で比べる。
  - 出た要素は、各側で map の反復の規則を満たすかを見る。
    - 出た時点で `Has` が真。
    - 反復の開始時にあるか、反復中に足したもの。
    - 反復中に一度も消されなかった最初の要素は、ちょうど 1 回出る (止めなかったとき)。
  - Pointer 側がこの規則を破れば、試験の前提の誤りとして失敗する。
- **nil の受け手** (`TestNodeSetsNilReceiver`): 各 method を nil の set で呼び、両側が同じく panic するか、同じ値を返すことを確かめた。
- **修正前の失敗**: 修正前の `Values` では、`TestOrderedNodeSetIsOrderedSet` が 3,000 列のうち **1,877 列**で失敗した。
  - 例: `Pointer "range 5 3 4 10", Store "range 5 3"` (反復中に足した要素が出ない)。
  - 例: `Pointer "range 5 7", Store "range 5 7 -1"` (`Delete` の後に、詰めた slice の末尾の nil のノードが出る)。
  - `NodeSet` と nil の受け手は、修正前から通った。

## 5. corpus (`TestCheckerCorpus`)

### 5.1 class の表 (報告の mode、`STORE_CHECKER_CORPUS_REPORT=1`)

| class | file |
| --- | ---: |
| equal | 17,405 |

比較した 17,405 file が全部 equal (100.0%)。class は 1 つだけ。strict の実行 (最終のコード) でも同じ。

### 5.2 除外

| 理由 | 件数 | file (corpus の index) |
| --- | ---: | --- |
| root が Pointer の program に無い | 0 | |
| Pointer が panic する | 1 | 7210 `pathMappingBasedModuleResolution_rootImport_aliasWithRoot_differentRootTypes.ts#/root/a.ts` |
| 合成の import (段 2) | 8 | 5564 `jsxImportSourceNonPragmaComment.tsx`、5600 `jsxNamespaceImplicitImportJSXNamespaceFromPragmaPickedOverGlobalOne.tsx#/index.tsx`、5625 `jsxRuntimePragma.ts#two.tsx`、5626 `#three.tsx`、13832 `jsxJsxsCjsTransformCustomImportPragma.tsx#preact.tsx`、13833 `#react.tsx`、13836 `jsxJsxsCjsTransformKeyPropCustomImportPragma.tsx#preact.tsx`、13837 `#react.tsx` |
| Pointer が非決定 | 1 | 5591 `jsxNamespaceImplicitImportJSXNamespace.tsx#/node_modules/preact/jsx-runtime/index.d.ts` |

非決定の file について:

- 元の class は `diag file: MessageArgs` (root 0 の 8 番目の診断の引数)。2b-1 の報告 §4 の file と同じ。
- Pointer の記録が最初の回と変わったのは、報告の mode では 1 回目の走らせ直し (違いは `diag file: count`)、strict では 3 回目だった。
- 非決定の除外で隠れる「Store の本当の誤り」は、この 1 file の中だけに限られる。

### 5.3 token と CompareSymbols

- ID の token の出現: **0** (両側の全記録、equal の file も含む)。
- `CompareSymbols` の検査: Pointer 側 554,158 組 (うち ID の fallback に届く組 406)、Store 側 554,158 組 (406)。違反 0。
  - 組は、symbol の walk で隣り合う違う symbol と、宣言が無く名前が同じ symbol の全組。
  - fallback に届く組の数え方は §9 の 9。

### 5.4 時間と RSS (worker 4)

| 実行 | wall | file あたり | 最大 RSS (rusage) | `/usr/bin/time` 最大 RSS / peak footprint | load average |
| --- | --- | ---: | ---: | --- | --- |
| 報告の mode | 4m5s | 14.0 ms | 2.10 GB | 2.26 GB / 2.69 GB | 6〜12 |
| strict | 4m35s | 15.8 ms | 1.86 GB | 2.00 GB / 2.85 GB | 5〜9 |

- file の時間: 中央値 36〜43 ms、p99 132〜141 ms、最大 51〜55 s (checker.ts)。
- 2b-1 (6〜7 分) より短い。非決定の判定の走らせ直しは不一致の file にしか起きないので、時間への影響は無い。load は 2b-1 と同じく別のプロセスで高かった。

## 6. §7 `TestPointerWalkerBaselines`

- 2b-1 の広げた条件を既定にした。option を harnessutil と同じく既定の後に適用し、構成が 1 つで harness の option を持たない test。`STORE_CHECKER_BASELINE_OPTIONS` は消した。
- この検定の中だけ、`hadErrorBaseline` を「reference に `<名前>.errors.txt` があるか」で決める。
  - `runPointer` に `hadErrorBaseline *bool` の口を足した。nil のときは、今までの driver の規則 (H1: comment directive の前の件数) のまま。corpus・private・非決定の走らせ直しは nil を渡す。
- **4,671 本で不一致 0** (2b-1 の広げた mode と同じ本数。2b-1 の不一致 2 本は `@ts-ignore` のもので、一致するようになった)。option を取らなかった test は 28 本。時間は 21.8 秒。

## 7. 生成物の変化

| 生成器 | file | 変化 |
| --- | --- | --- |
| `node tools/scripts/tsc/generate.ts` | `ast/store/views_generated.go` | 多相の `Node.Type`・`Node.QuestionToken`・`Node.TypeExpression` が消えた (24 行)。ほかの生成物は変わらない |
| `(cd tools && go run ./cmd/storetranslate)` | `storechecker/checker.go` | `findResolutionCycleStartIndex` が消えた (P2 になった) |
| 同上 | `storechecker/exports.go` | `CompareSymbols` が出た |

- 指示書 §8 の見込みとの違いは、消えた多相の accessor が `QuestionToken` に加えて `Type`・`TypeExpression` もあること (§2.4)。
- storeparser・storebinder の生成物は変わらない。ただし、両者が呼ぶ多相の `Type()`・`QuestionToken()`・`TypeExpression()` は手書きに替わった。意味の違いは §2.1 と §2.4 の kind だけで、既存の parse・bind の等価は通る (§8)。
- 2 つの生成器を最後にもう一度回し、`git status` と diff (sha1) が変わらないことを確かめた (§8)。

## 8. テスト

| テスト | 結果 | 時間 |
| --- | --- | --- |
| `go test ./internal/ast/store/... ./internal/store...` (tag なし) | 失敗は既知の `FuzzEquivalence/a708303f611b95c8` (`ExternalModuleIndicator differs from the Pointer file`) だけ | 13m44s |
| 同 (`-tags storechecks`) | 同上 | 13m0s |
| `(cd tools && go test ./cmd/storetranslate/)` | ok | 23.3 秒 |

両方の tag の一式の中で、新しい試験と strict の corpus も通った (`TestCheckerCorpus` 545 秒 / 412 秒、`TestPolymorphicAccessors` 84 秒 / 79 秒、`TestPointerWalkerBaselines` 50 秒 / 42 秒、`TestModeCorpus`・`TestPrivateFixtures`・`TestNodeSetIsSet`・`TestOrderedNodeSetIsOrderedSet`・`TestNodeSetsNilReceiver`・`TestTypeRoleSlot`・`TestInternalIDTokenRe`・`TestIDTokens`)。一式の中の時間は、package を並べて走らせるので単独の実行より長い。

一式の後で、QuestionToken の doc comment の英語を 1 か所直した (comment だけ)。その後で build・vet・`./internal/ast/store/...` の `-short` のテストを回し直し、2 つの生成器を回して `git status` と diff の sha1 が変わらないことを確かめた。ログは `_store-checker-7d2b2-impl/test-notag.txt.gz`・`test-storechecks.txt.gz`。

## 9. 指示書に無い判断

1. **`Type` と `TypeExpression` の手書き** (§2.4)。どちらも §4.1 の修正前の照合で見つけた。止めて判断を仰ぎ、ユーザーが決めた (2026-10-01)。
2. **`typeRoleSlot` は `store.go` の `init()` で埋める** (§2.4)。生成した file の `init()` の後に走ることを file 名の順に頼り、`TestTypeRoleSlot` で確かめる。生成器には手を入れていない。
3. **手書きの `QuestionToken` は型付きの view を引く switch** にした (Pointer と同じ形)。指示書は「`questionTokenSlot` の表は Builder の setter と手書きの accessor が使う」と書いていた。しかし switch の形では、手書きの accessor は表を引かない。表を使うのは Builder の setter だけになる。
4. **§4.1 の「両方 panic」は一致として数えた**。指示書の panic の表に無い組み合わせ。Store が Pointer の panic まで写した `Attributes` と `TypeExpression` で起きる。
5. **§4.1 の Store 側は、root の file だけを parse した** (`newProgram` は使わない)。`newProgram` は lib まで Store に parse し直すので、17k 回は重い。parse の option は Pointer の program の file のものを使う。
6. **§4.1 は method 式を具体的な func の型に型 assert して呼ぶ** (`reflect.Value.Call` より速い)。知らない型の組が出たら `t.Fatal` するので、method が増えても黙って外れない。
7. **§4.2 の `NodeSet` の反復中の変更は、規則と反復の後の中身で比べた** (§4)。指示書の「集合で比べる」を、反復中に足した要素の出方が決まらない場合へ広げた読み。
8. **§4.2 に nil の受け手の試験を足した** (`TestNodeSetsNilReceiver`)。`collections.Set` は nil の受け手を扱う method を持つため。
9. **fallback に届く組の数え方** (§5.3): 名前が同じで、どちらも宣言が無いか、最初の宣言の file と位置が同じもの。Pointer の `compareNodes` は、同じ file・同じ位置なら別のノードでも 0 を返すので、そこから名前の比較と ID に進む。検査の対象 (反対称性の 2 つ目の組) は、指示書どおり「宣言が無く名前が同じ」組。
10. **非決定の判定の走らせ直しは、記録の差のときだけ行う**。次の 3 つでは走らせ直さない。
    - Store の panic。
    - token がある。
    - `CompareSymbols` の性質の違反 (性質は記録が一致したときにだけ見るので、違反の class では記録は一致している)。

    どれも Pointer の非決定では生じない。走らせ直すと、Pointer が非決定な file で本当の誤りを隠すので外した。そのために `compareRuns` を、記録の比較 `compareRecords` と性質の違反 `symbolOrderViolation` に分けた。走らせ直しは `compareRecords` で比べる (指示書の「corpus と同じ比較器」の、記録の部分)。最初に書いた版は `compareRuns` のまま走らせ直していた。その版では、Pointer に決定的な違反があると、Pointer どうしでも「違う」と出て非決定に入ってしまう。差分を読み直して見つけ、直した。
11. **token があれば、記録の差より `internal id token present` を優先する**。token を含む文字列は、数字の振り方の違いだけで生の比較が違いうるので、その file の比較は照合を設計するまで信用できないため。性質の違反の class は、記録が一致したときだけ付ける。
12. **JSONL の行に足した field**: `index` (指示書)、`reruns`・`rerun` (非決定の判定。`rerun` は Pointer の 2 回の記録の最初の違いで、`pointer` が最初の回、`store` が走らせ直した回)、`tokens`、`pairs`・`fallback` (`CompareSymbols`、Pointer・Store の順)。
13. **class `internal id token` と `synthetic import (stage 2)` は無くなった**。前者は §5.3 の 1 でやめた。後者は除外の理由に移した。`Pointer 非決定` の除外のときも、`Class` には元の class を残す。

## 10. 再現の手順

```sh
ROOT=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript
SCR=<scratchpad>
cd $ROOT && node tools/scripts/tsc/generate.ts && (cd tools && GOWORK=off go run ./cmd/storetranslate) && git status --short
cd $ROOT/tsc && GOWORK=off go build ./... && GOWORK=off go vet ./internal/ast/store/... ./internal/store...
cd $ROOT/tools && GOWORK=off go vet ./cmd/storetranslate/... && GOWORK=off go test -count=1 ./cmd/storetranslate/
# §4.1・§4.2
cd $ROOT/tsc && GOWORK=off go test -c -o $SCR/harness.test ./internal/storeharness/
cd $ROOT/tsc/internal/storeharness && $SCR/harness.test -test.run '^TestPolymorphicAccessors$' -test.v
cd $ROOT/tsc && GOWORK=off go test -count=1 -v -run 'TestNodeSetIsSet|TestOrderedNodeSetIsOrderedSet|TestNodeSetsNilReceiver|TestTypeRoleSlot' ./internal/ast/store/
# corpus (報告の mode と strict)
cd $ROOT/tsc/internal/storeharness && STORE_CHECKER_CORPUS_REPORT=1 STORE_CHECKER_CORPUS_OUT=$SCR/corpus-report.jsonl $SCR/harness.test -test.run '^TestCheckerCorpus$' -test.v -test.timeout 120m
cd $ROOT/tsc/internal/storeharness && STORE_CHECKER_CORPUS_OUT=$SCR/corpus-strict.jsonl $SCR/harness.test -test.run '^TestCheckerCorpus$' -test.v -test.timeout 120m
# §7
cd $ROOT/tsc/internal/storeharness && $SCR/harness.test -test.run '^TestPointerWalkerBaselines$' -test.v
# 一式
cd $ROOT/tsc && GOWORK=off go test -count=1 -timeout 60m -v ./internal/ast/store/... ./internal/store...
cd $ROOT/tsc && GOWORK=off go test -tags storechecks -count=1 -timeout 60m -v ./internal/ast/store/... ./internal/store...
```

修正前の照合と試験の出力は `_store-checker-7d2b2-impl/` の `accessor-before.txt`・`collections-before.txt`。どちらも、その修正を入れる前の作業ツリーで取った。
- `accessor-before.txt`: store に手を入れる前。before に accessor_test.go と mode_test.go の `filePairs` を足しただけ。
- `collections-before.txt`: §2.1・§2.4 の accessor を直した後、`Values` を直す前。
