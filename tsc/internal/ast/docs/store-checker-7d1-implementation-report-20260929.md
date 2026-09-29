# Store checker JSDoc (TODO 7d-1) 実装報告

作成日: 2026-09-29。指示書は [store-checker-7d1-implementation-instructions-20260928.md](store-checker-7d1-implementation-instructions-20260928.md) (以下「指示書」)、設計は [store-checker-design-20260923.md](store-checker-design-20260923.md) (以下「7d 設計」) の §2.1・§2.2・§3。before は 3bb0252405。生の出力は `_store-checker-7d1-impl/` (ignore 対象、ローカルのみ)。KPC は取っていない (root が要る。検証指示書 §4 でユーザーが取る)。

## 0. 結論

- **parse の等価**: corpus 17,415 file (JS 1,456) で、JS を含めて除外 0・mismatch 0。JSDoc の木の等価も 0 (54,459 ノード。うち file の Store に入る eager 分 18,750)。`JSDocDiagnostics`・`ReparsedClones`・`IdentifierCount`・`Imports`・`ExternalModuleIndicator`・root の `Flags` と、診断 17,415 file が全部一致。
- **bind の等価**: 17,415 file 全部を比べて mismatch 0 (7d-0 は 17,085 file、330 file 除外)。宣言の Store の検査は 717,627 件 (7d-0 は 708,382)。
- tag なしと `-tags storechecks` の両方で store / storeparser / storebinder / 変換器のテストが通る。失敗は既知の `FuzzEquivalence/a708303f611b95c8` (`ExternalModuleIndicator differs`) の 1 件だけ。遅延 JSDoc の並列テストは `-race` でも通る。
- 移植は逐語: Pointer の jsdoc.go 54 関数・reparser.go 21 関数が同じ名前・同じ順序で移り、足したのは helper 3 つ (§1)。7b で (e) として外した parser.go 側の field と経路も全部戻した。
- 生成物は `node tools/scripts/tsc/generate.ts` と `go run ./cmd/storetranslate` で再現する (md5 一致)。
- 費用: Store の parse の ns に退行は見えない (load 12〜14 の下で before / after 交互 3 回、after ≤ before)。遅延 JSDoc (dom の全 host) は Pointer 比 **ns 1.06 倍、B/op 1.55 倍、allocs 1.14 倍** (§6)。
- 指示書に無い判断をいくつかした (§7)。実装を止めるほどのものは無いと判断した。大きいのは 2 つ: `Builder.SetParent` を足した (host は slot に JSDoc を持たないので、`parsed.Parent = node` を書く口が要る)、`SetParseJSDocForNode` を変換器の制約で `utilities_manual.go` (P2) に置いた。

## 1. storeparser の移植 (報告 (a))

### 1.1 jsdoc.go / reparser.go

| | Pointer | Store | diff |
| --- | ---: | ---: | --- |
| jsdoc.go | 1,355 行、54 関数 | 1,370 行 | 37 hunk、−248 / +261 行 |
| reparser.go | 752 行、21 関数 | 781 行 | 10 hunk、−347 / +373 行 |

関数の集合と順序は Pointer と同じ。足したのは `rawList` (jsdoc.go、`parseNestedTypeLiteral` の直後)、`isEmptyList` と `functionLikeHost` (reparser.go、使う関数の直前と `getFunctionLikeHost` の直後) の 3 つ。書き換えの内訳 (Store 側の出現数):

| 分類 (7b に合わせる) | jsdoc.go | reparser.go |
| --- | ---: | ---: |
| (a) 型の置換と `View` 化 (`p.b.View(` の数) | 28 | 164 |
| (b) constructor の折り畳み (`p.b.New*`) / `p.b.List` | 42 / 6 | 29 / 13 |
| (c) 子をローカルに受け直し (Pointer が constructor の引数の中で parse していたもの) | 9 (`parseTrailingTagComments` の結果 7、`parseJSDocLinkName` の `right` 1、`JSDoc` の comment list 1) | — |
| 7d-1 の置き換え: setter・`SetFlags`・`SetRange`・`ReparentChildren`・`AddFlags` | 6 | 42 |
| 7d-1 の置き換え: `DeepCloneReparse(Modifiers)` | 0 | 7 |
| 7d-1 の置き換え: list の要素を読んでから追記するときの `slices.Clone(l.Refs())` | 0 | 15 |
| 7d-1 の置き換え: text の連結 (`strings.Join`、`stringSliceArena.Clone` の代わり) | 7 | 0 |

reparser.go は Pointer が「factory で組む (範囲も flags も無し) → field に代入 → `finishReparsedNode`」の順なので、Store でも constructor に `(0, -1, -1)` を渡して組み、setter で埋め、`finishReparsedNode` が `SetFlags` / `SetRange` / `ReparentChildren` を呼ぶ形にした。Builder に append すると `View` が無効になるので、append をまたぐノードは全部 `NodeRef` で持ち、読むたびに `View` し直す。

逐語にならなかった箇所と理由:

| 箇所 | Pointer | Store | 理由 |
| --- | --- | --- | --- |
| `parseTypedefTag` の `JSDocTypeLiteral` | 常に `NewJSDocTypeLiteral` を作り、子の `@type` の型を使うときは finish せずに捨てる | 使うとき (else 側) だけ作る | Store の constructor は子の `parent` を書く。捨てる literal を作ると property tag の親が dead の literal になり、Pointer (親は nil のまま) とずれる |
| `parseTypedefTag` の `typeExpression.Parent = typedefTag` | 明示の代入 | 消した (コメントで説明) | constructor が書く (7b の規則) |
| `parseSimpleTag` の `createTag` | 引数 2 つの closure 6 個 | Builder の constructor の method value (`p.b.NewJSDocPublicTag` など) | constructor が flags・範囲を取るので形が変わる。closure を書くと本体が constructor の呼び出し 1 行になるだけ |
| `parseTemplateTagTypeParameters` | 零値の `ast.TypeParameterList` (範囲 0, 0) を返す | `p.b.List(0, 0, ...)` | 同じ値。コメントで明記 |
| `JSDocTypeLiteral.JSDocPropertyTags` | raw `[]*Node` | `rawList` (nil は `NoListRef`、それ以外は範囲 0, 0 の block) | raw list の Store の表現 (convert と同じ) |
| `fun.TypeParameters() == nil` (`@template`) | slice の nil (空の list も nil: `core.Arena.Clone`) | `isEmptyList` (`IsNil() \|\| Len() == 0`) | pointer の nil (`class.TypeParameters == nil`、`TypeParameterList() == nil`) は `IsNil()` のまま。slice の nil と区別した |
| `getClassLikeData` | `*ast.ClassLikeBase` | class の `Node` か零値の `Node` | Store に ClassLikeBase は無い。呼び出し側は `HeritageClauses()` を読む |
| `getFunctionLikeHost` | `*ast.Node` | `Node` (無ければ零値)。`functionLikeHost` が `NodeRef` にする | 零値の `Node` からは `Ref()` を取れない |
| `findMatchingParameter` | `*ast.ParameterDeclaration` | `NodeRef` | 呼び出し側が append をまたいで持つ |
| `parseJSDocForNode` の parse option | `sourceFile.ParseOptions()` | `FileName` と `Path` だけ | `store.File` は parse option を持たない。JSDoc の parse が読むのは file 名だけ (`initializeState` の panic の文言) |
| `withJSDoc` の `GetJSDocCommentRanges` | `*ast.Node` | `store.Node` (eager は `View`、遅延は host) | host が Builder の中と別 Store の 2 通り |

### 1.2 parser.go

7d-0 からの diff は 78 hunk。指示書 §4.1 の一覧どおり:

- `Parser` の field: `jsdocDiagnostics`、`hasDeprecatedTag`、`jsdocInfos`、`jsdocCommentsSpace`、`jsdocCommentRangesSpace`、`jsdocTagCommentsSpace`、`jsdocTagCommentsPartsSpace`、`reparseList`、`reparsedClones`。**`stringSliceArena` は戻していない**: text を連結して持つ (指示書 §2 の 1) ので、`[]string` を複製する arena の使い道が無い。
- `type JSDocInfo` (`NodeRef` で持つ)、`ParserState` の `jsdocInfosLen` / `reparsedClonesLen` と `mark` / `rewind` の truncate。
- `parseListIndex` の `reparseList` の差し込みと外側への押し出し、`parseSourceFileWorker` の末尾の追加、`parseToplevelStatement` の `i += len(p.reparseList)`。
- `finishSourceFile` の `SetJSDocDiagnostics`、TS の `SetHasLazyJSDoc(true)`、位置で並べた `ReparsedClones` (`store.CompareNodePositions` で `View` を比べる)。
- `createJSDocCache` は `FinishFile` の後で呼ぶ (`parseSourceFileWorker` と `parseJSONText`)。cache の値は Compact 後の Store の `Node` でなければならないため (指示書 §3.2)。
- 56 箇所の `p.b.AddFlags(x, p.jsdocFlags(info))` を `p.withJSDoc(x, info)` に戻し、`jsdocFlags` を消した。
- `GetJSDocCommentRanges` と `isJSDocLikeText` は utilities.go (Pointer と同じ位置)。

Pointer との diff で JSDoc / reparse に関わって残る差は、型の置換、`stringSliceArena`、診断の file (7b の規則: Store の診断は file を持たない) だけ。

### 1.3 speculation (指示書 §4.2)

`mark` / `rewind` が戻すものは Pointer と同じ (`jsdocInfos` と `reparsedClones` の長さだけ)。storeChecks build では `rewind` の `Truncate` の後に `checkNoTruncatedRefs` が `jsdocInfos` / `reparsedClones` / **`reparseList`** に消えたノードが残っていないことを確かめる。`reparseList` は指示書に無いが、Pointer は巻き戻さずに捨てたノードを生かしたまま持つ一方、Store では消えた行が次のノードで再利用されて別のノードを指すので、同じ検査に入れた (検査だけで動作は変えていない)。corpus 全体を storechecks で回して一度も発火しない。

## 2. `store` の API と生成器 (報告 (b))

### 2.1 生成器 (`tools/scripts/tsc/generate-go-store.ts`)

1. **`[]string` の text** (`JSDocText`、`JSDocLink` / `LinkPlain` / `LinkCode`): `classify` が text slot にする。4 kind は `Text` 役割に入り、`Node.Text()` がそのまま読む。`JSDocText` は payload を持つようになった (2 語)。Builder と Factory の constructor は連結済みの `string` を取る。生成器と views の TODO は消えた。
2. **`JSDocParameterOrPropertyTag` の子の順**: `swappedChildren` 表 (`IsNameFirst`、`Name`、`TypeExpression`) を足し、`ForEachChild` の生成で `IsNameFirst` が偽なら 2 つを入れ替える。子を順に辿る生成関数は `ForEachChild` だけ (convert は Pointer の field を読むので順に依存しない。`deepCloneReparse` の順は id にしか効かない)。`memberOrderOverrides` の TODO はこの表への参照に書き換えた。
3. **reparse の setter** (`reparseSetters` 表、builder_generated.go): reparser.go が書く member から決めた 14 個:

   | setter | class | 引き方 |
   | --- | --- | --- |
   | `SetType`、`SetExpression`、`SetInitializer`、`SetQuestionToken`、`SetRight`、`SetFullSignature` | child | 役割の表 |
   | `SetTypeParameters`、`SetParameters`、`SetModifiers`、`SetHeritageClauses`、`SetTypes`、`SetTypeArguments` | list | 役割の表 |
   | `SetObjectAssignmentInitializer` | child | 1 定義 (`ShorthandPropertyAssignment`) の kind の switch |
   | `SetPhaseModifier` | kind | 1 定義 (`ImportClause`) の kind の switch |

   slot の無い kind は panic。child / list の setter は新しい子 (list の要素) の `parent` を書く。`SetModifiers` は header の `modifierFlags` を新しい block から計算し直す。14 個とも storeparser が使う。
4. **`deepCloneReparse`** (builder_generated.go): payload を持つ定義ごとの case で、payload を丸ごと写し、child と list の語だけを先に複製してから行を足し、子の `parent` を複製に書く。payload の無い kind (token、identifier) は header だけ写す (identifier の data 語もそのまま = text は同じ bytes を指す)。
5. 等価テストの生成 (`equivalence_generated.go`) は text slot を `pv.Text()` (Pointer の `Join`) と比べるようになった (生成器の変更は無く、slot が増えた分)。

### 2.2 手書き

| API | 置き場 | 指示書 |
| --- | --- | --- |
| `Builder.SetFlags`、`SetRange`、`ReparentChildren`、`DeepCloneReparse` | builder_reparse.go | §3.1 |
| `Builder.DeepCloneReparseModifiers` | builder_reparse.go | §3.1 に無い。Pointer の reparser が使う (`@import`、`@overload`、`@template`) |
| **`Builder.SetParent`** | builder_reparse.go | §3.1 に無い。§7 |
| `FinishFile` の storeChecks の「root が最後」検査 | store.go | §3.1 (置き場は §7) |
| `File` の `jsdocDiagnostics`、`jsdocCache map[NodeRef][]Node`、`jsdocMu`、`hasLazyJSDoc`、`ReparsedClones` | file.go | §3.2 |
| `Node.JSDoc`、`Node.EagerJSDoc`、`File.JSDocDiagnostics`、`SetJSDocDiagnostics`、`SetJSDocCache`、`SetHasLazyJSDoc`、`resolveJSDoc`、`checkHost` | jsdoc.go | §3.2 |
| `SetParseJSDocForNode` | utilities_manual.go (P2) | §3.2 (置き場は §5) |
| `FinishLazyJSDoc` | jsdoc.go | §3.3 |

- `checkHost` (不変条件 27) は `n.s != f.Store` で通常 build でも panic する。lazy と eager の両方の経路 (`JSDoc` と `EagerJSDoc`) で呼ぶ。
- `FinishLazyJSDoc`: 空なら Store も StoreID も作らない。`Finish` の後に host の Store の `space` から `Next()`、`file` は host の file、foreign 表は host 1 項目で全 root の `parent` がそれを指す、`sealed` を立てて返す。program の外の file (space nil) は StoreID 0・space nil。`checkImport` は 7d-0 のまま (`owner == nil && space == r.space`) で、同じ program の遅延 JSDoc の Store を受け、別 program と program の外を拒否する (テストで確認、§4)。
- `unsafe` は増えていない。

## 3. 外した除外と緩め (報告 (c))

| 置き場 | 外したもの |
| --- | --- |
| `storetest/equivalence.go` | `Options` 型ごと (`SkipReparsed`、`MaskFlags`)、`isJSDocCast`、`skip`、`parentOf`、`kept`、list / raw の「reparse のノードだけの list を nil とみなす」緩め、`preorderPointer` の Reparsed と cast の飛ばし。`Equivalent` / `Pairs` は option を取らなくなった |
| storeparser `parser_test.go` | `options(kind)` (JS / JSX の option)、`compareFields` の `eagerJSDoc` の `IdentifierCount` の skip と `eagerJSDoc` 関数、`fieldMismatches` の `fromJSDoc` (Imports / ModuleAugmentations から落とす) と `ExternalModuleIndicator` の skip、JS の root の `Flags` を log に回す扱い (`rootFlagDiffs`)、JS の診断を部分列で許す扱い (`subsequence`、`missingFrom`、`jsMissing`)。`FuzzEquivalence` の同じ規則 |
| storebinder `binder_test.go` | `options(kind)` (`ThisNodeOrAnySubNodesHasError` の mask を含む)、330 file の除外と理由ごとの log |
| storebinder `bindequiv_test.go` | `bindComparer.opts`、`MaskFlags` の flags 比較、`fromJSDoc` と `nodes` の JSDoc 由来の除去、`ExternalModuleIndicator` の skip、`reparsedEffects` / `isTypeOfExpandoOrRequireDeclaration` / `excluded` と理由の定数 |

一覧に無い緩めは見つからなかった (`Reparsed|NodeFlagsJSDoc|isJavaScript|ScriptKindJS|jsdoc` で 3 package のテストを検索)。外せなかったものは無い。

足したテスト (指示書 §6.2):

1. **JSDoc の木の等価** (`storetest.EquivalentJSDoc`): `HasJSDoc` の host ごとに両方の `JSDoc(file)` を引き (TS は遅延、JS と `@see` / `@link` は eager)、個数、各 comment の preorder の組ごとに `Equivalent` と同じ比較 (header・flags・`Parent`・member 全部・役割) をする。root の親は「host かどうか」を Pointer と照合する (`@typedef` の comment は type alias の JSDoc でもあるが、親は parse した元の host。Pointer も同じ)。root の `File()` が host の file。`File.ReparsedClones` (位置と kind) と `JSDocDiagnostics` は `fieldMismatches` に入れた。corpus で比べた host は eager 1,991・遅延 11,162、`JSDocText` 10,897、link 114、param / property tag は `IsNameFirst` 真 1,071・偽 1,062。
2. **遅延 JSDoc の契約** (`store/jsdoc_test.go`、program は `ParseSourceFileIn`): parse の回数は登録された parser を包んで数える (cache の hit は数えない)。
   - 同じ host を 2 回 (file 明示と nil) 引いて parse 1 回、結果が同じ。`EagerJSDoc` は parse 前 nil・後は同じノード。
   - 空の結果 (`let i = 0;/** c */\nif (i) {}` と `var z: /** c */ (x: number) => string;` の 2 通り。corpus から探した): 2 回目に parse しない、StoreID を使わない。
   - root の `Parent()` が host と `SameNode`、さらに上が source の Store で続く、全ノードの `File()` が host の file、StoreID が 0..nFiles−1 の外、sealed。Factory の writer は通常 build で、binder の writer は storeChecks build で拒否する。
   - 同じ program の region の Factory が root を子に取れ、別 program と program の外の file の遅延 JSDoc は panic (容量 1 / 2 / 3 / 4096)。
   - 合成の host と別 file の host で `JSDoc` / `EagerJSDoc` が通常 build で panic (TS の file と JS の file の両方)。
   - 16 goroutine が 64 host を別々の順で引く: parse は host ごとに 1 回、全 goroutine が同じノードを見る。`-race` で通る。
3. **Builder の API** (`TestBuilderReparseAPI`): setter が slot の無い kind で panic (役割の表・switch・kind slot の 3 形)、`SetModifiers` が `modifierFlags` を計算し直し親を書く、`DeepCloneReparse` の根だけ Reparsed・子は元の flags (JSDoc)・範囲と text が元と同じ・子の親は複製・元の親は動かない、`ReparentChildren` が直下の子の親を戻す。
4. `TestDeadNodes` を JS も数える (§5)。

## 4. 変換器 (報告 (d))

`(cd tools && go run ./cmd/storetranslate)`: 166 → **177 関数**、P2 10 → **11**、根 171 → 183。

- 増えた変換関数 (storeparser の jsdoc.go / reparser.go / parser.go が `store.X` で呼ぶもの): `IsQualifiedName`、`IsJSImportDeclaration`、`IsMethodDeclaration`、`IsTypeReferenceNode`、`IsJSDocTemplateTag`、`IsJSDocCallbackTag`、`IsJSDocTypedefTag`、`GetRightMostAssignedExpression`、`IsThisIdentifier`、`HasSamePropertyAccessName`、`CompareNodePositions`。規則は足していない。
- P2 の変更:
  - **`SetParseJSDocForNode`** (新規、hash `e231d5a508aa9186 ast.go:SetParseJSDocForNode`): 指示書 §3.2 は jsdoc.go に置く想定だったが、変換器は「storeparser が `store.X` で呼び、ast に同名の関数がある」ものを根にし、根を P2 以外で手書きすると止まる (`SetParseJSDocForNode is a root and is written by hand outside utilities_manual.go`)。parser の型 (`func(*File, Node) []Node`) が ast と違い規則では訳せないので、P2 に置いた。登録先の変数 `parseJSDocForNode` は jsdoc.go。
  - **`GetNodeAtPosition`** (7d-0 の P2、hash は同じ `f2ca68b56aa87bec`): 7d-0 の版は JSDoc が無いので `includeJSDoc` を持たなかった。Pointer と同じく `includeJSDoc` を取り、`current.JSDoc(file)` に先に降りる形にした。P2 のままなのは root を `file.Root()` で取る (Pointer の `file.AsNode()`) ため。storeparser の `forEachDynamicImportOrRequireCall` は Pointer と同じく `isJavaScriptFile && includeTypeSpaceImports` を渡すので、JS の JSDoc の `import("x")` が `Imports` に入る (7d-0 まで `fromJSDoc` で落としていた差がこれで消えた)。

## 5. dead node (報告 (e))

`TestDeadNodes` と `TestCorpus` は file の Store に入った eager の JSDoc を live に数える。

- fixture: checker.ts dead 830 (eager JSDoc 63 ノード)、dom 830 — 7b / 7d-0 と同じ。
- corpus 全体: dead 22,793 (0.88%)。JS 1,456 file: live 107,489、dead 1,246 (1.16%)。
- JS の構文ごと (`TestDeadNodes` に固定):

  | 入力 | dead | 内訳 |
  | --- | ---: | --- |
  | `@type`、`@readonly` (class member と `this.x`)、`@private`、`@this`、`@implements`、`@template` + `@param` + `@returns`、`@satisfies`、`/** @type */ (x)`、`@import`、`@overload`、`{@link}` | 0 | |
  | `@param {number} [x=1]` | 2 | 既定値の `EqualsToken` と式 (Pointer も捨てる) |
  | `@param {Object} o` + `@param o.a` | 3 | 入れ子の type literal に差し替わる `{Object}` |
  | `@typedef {Object} T` + `@property` | 3 | 同上 |
  | `@typedef {Object} T` + `@property` + `@type {number}` | 10 | `{Object}` 3、使われない property tag 5、型を取られた `@type` tag 2 |
  | `@callback` + `@returns` | 1 | **reparse の付け替え**: `any` を戻り値にして作った function type の `Type` を `@returns` の複製で置き換える (Pointer も同じ) |

  reparse の付け替えで dead になるノードは `@callback` の `any` だけ。list の付け替え (modifiers、`@this` の引数、`@implements`) で dead になるのは古い block で、ノードではない。

## 6. inline と費用 (報告 (f))

inline (`-gcflags=-m`、store package、before 3bb0252405 と突き合わせ):

- 7d-0 の `can inline` 1,384 件は全部残り、増えた 28 件は新しい関数 (`SetFlags`、`SetParent`、`SetRange`、`DeepCloneReparse`、`DeepCloneReparseModifiers`、`cloneChildReparse`、`checkHost`、`File` の setter、JSDoc 系の view と `Text`、変換で増えた述語など)。
- Builder の生成 constructor の判定は before と同じ (inline されるのは 1 本のまま)。`NewJSDocText` (cost 136) と `NewJSDocLink*` (158) は text slot が増えた分 cost が上がったが、before も inline されていない。
- setter は inline されない (`SetType` 117、`SetTypeParameters` 134、`SetModifiers` 198 など)。JS の reparse でしか呼ばない。
- `Node.JSDoc` (179)、`Node.EagerJSDoc` (308) は inline されない。hot path ではない (7d 設計 §2.2: 3,618 回/check)。
- **`withJSDoc` は inline されない** (cost 612。Pointer も同形で inline されない)。7d-0 の `p.b.AddFlags(x, p.jsdocFlags(info))` は `jsdocFlags` (cost 24) ごと inline されていたので、56 箇所が呼び出しに変わった。一時的に計数して、checker.ts で 54,939 回 (JSDoc があるのは 226 回)、dom で 25,337 回 (6,151 回)。呼び出し 1 回を 10 命令前後とすると 0.55M inst で、checker.ts の Store parse (2 億命令台) の +0.25% 程度の見積り。逐語を優先して分割していない。KPC で Parse が退行していたら、`info&jsdocScannerInfoHasJSDoc == 0` の早期 return だけの inline できる入口に分ける (1 関数の変更) ことを提案する。

費用 (M1、`_store-checker-7d1-impl/`):

- Store の parse (ns、`-benchtime 100x`、before / after 交互 3 回、load 12〜14): checker.ts 18.0 / 18.1 / 21.3 ms → 17.9 / 18.0 / 18.4 ms、dom 8.44 / 8.53 / 9.53 ms → 8.06 / 8.09 / 8.19 ms。B/op checker.ts +23 KB (13.16 → 13.19 MB)、allocs 580 → 635 (`@see` / `@link` の eager JSDoc 63 ノードと cache)。dom は同じ (1,528 allocs)。load が高く、線の判定は検証の KPC に任せる。
- **遅延 JSDoc** (`BenchmarkStoreLazyJSDocV1`、dom の全 host 6,153 comment、JSDoc の読みだけを測る、5 run の中央値): Pointer 9.18 ms / 6.37 MB / 77,714 allocs、Store 9.73 ms / 9.90 MB / 88,496 allocs (**1.06 / 1.55 / 1.14 倍**)。KPC 版 `BenchmarkStoreLazyJSDocKPCV1` も同じ形で足した (root が要るので未計測)。
  - B/op の内訳 (memprofile、30 回): comment 1 つの parse ごとの `Builder.Finish` の複製 (nodes・extra) と Store と foreign 表で約 3.7 MB/op、**連結した text の 3 重の複製** (`strings.Join` → Builder の `textBuf` → `Finish` の `strings.Clone`) で約 4.5 MB/op、`pushComment` の `[]string` の成長約 2.1 MB/op (Pointer も同じ: `putParser` が space を捨てる)。Pointer は text を source の部分文字列の `[]string` で持ち、複製しない。
  - 7d-2 の Check に乗る前見積り: compiler プロジェクトで check 中に parse されるのは 2,845 host (7d 設計 §2.2) なので、dom の比で Pointer 比 +0.25 ms・+1.6 MB 程度。減らすなら、text を source の範囲で持つ形 (連結前の部分が source 上で連続するときは offset で持つ) か、`Finish` で `textBuf` を複製しない形。どちらも指示書の範囲外なので入れていない。

## 7. 指示書と 7d 設計に書かれていない判断 (報告 (g))

どれも実装を止めずに進めた。問題があれば指摘してほしい。

1. **`Builder.SetParent` を足した**。Pointer は `parsed.Parent = node` (eager の comment の親を host に)、`propJSDoc.Parent = node` (reparse した引数・property の JSDoc) を constructor の外で書く。host はその comment を slot に持たないので `ReparentChildren` では書けない。§3.1 の一覧に無い API。
2. **`DeepCloneReparseModifiers` を足した**。Pointer の `NodeFactory.DeepCloneReparseModifiers` の Store 版 (block と要素を複製、要素に Reparsed を付けない)。§3.1 の 4 の延長。
3. **`deepCloneReparse` の生成の形**: 定義ごとの case に payload の丸写しと子の複製を生成し、入口 (`DeepCloneReparse`) と `cloneRow` / list の複製は builder_reparse.go に手書き。constructor を呼ぶ形にしなかったのは、constructor が text を `textWords` で書き直す (元の bytes を指さなくなる) ため。
4. **`SetParseJSDocForNode` を P2 に置いた** (§4)。変換器の規則を足さずに済む置き場。
5. **root が最後の検査は `FinishFile` に置いた** (指示書は `Finish`)。`FinishLazyJSDoc` も `Finish` を使い、遅延 JSDoc の Store は root が複数で「最後が root」が成り立たないため。source の Store と convert (post-order) には効く。
6. **storeparser に storeChecks の定数を置いた** (`checks_on.go` / `checks_off.go`、store と同じ build tag)。§4.2 の検査に要る。
7. **`checkNoTruncatedRefs` に `reparseList` も入れた** (§1.3)。
8. **`parseTypedefTag` の `JSDocTypeLiteral` は使うときだけ作る** (§1.1)。
9. **`createJSDocCache` を `FinishFile` の後に呼ぶ** (§1.2)。指示書 §3.2 の「`finishSourceFile` で `FinishFile` の後に」を、`finishSourceFile` は `FinishFile` の前 (top-level await の reparse がまだ木を書き換える) なので呼び出し側で満たした。
10. **遅延 JSDoc の Store の foreign 表は host 1 項目を全 root で共有**する。「項目は重複を除かない」(store.go) の反対ではなく、1 項目で足りるので 1 つ作るだけ。
11. **`storetest.Options` を型ごと消した**。消した後に field が残らないため (指示書 §6.1 の「使われなくなった option と helper は消す」)。`Equivalent` / `Pairs` の引数が 1 つ減り、store / storeparser / storebinder のテストの呼び出しを直した。`EquivalentJSDoc` を足した。
12. **`EquivalentJSDoc` の root の親は Pointer と照合する** (§3 の 1)。`@typedef` の comment は type alias の JSDoc でもあり、親は元の host。指示書の「`Parent()` が host」をそのまま検査すると 288 件が Pointer と同じ形のまま落ちる。
13. 遅延 parse に渡す parse option は `FileName` と `Path` だけ (§1.1)。
14. 生成器のファイルの改行は HEAD と同じ LF のまま (dprint の設定は CRLF を求め、HEAD の版も `dprint check` を通らない。内容の整形差は無いことを確認した)。

## 8. 限界

- 実装者と検証者が同じではない前提の報告。diff の hunk は移植中に全部読んだが、独立した目は入っていない。
- KPC は未計測。ns は load 12〜14 の下で取った参考値。
- JS の parse と reparse の費用 (Pointer 比の ns / B/op / allocs) は測っていない (検証指示書 §4 の参考計測)。

## 9. 再現

```sh
cd $ROOT && node tools/scripts/tsc/generate.ts && (cd tools && go run ./cmd/storetranslate) && git status --short
cd $TSC && go test -count=1 -v ./internal/ast/store/... ./internal/storeparser/... ./internal/storebinder/...
cd $TSC && go test -tags storechecks -count=1 -v ./internal/ast/store/... ./internal/storeparser/... ./internal/storebinder/...
cd $TSC && go test -race -count=1 -run 'LazyJSDoc|JSDocHostCheck' ./internal/ast/store/
cd $TSC && go test -run '^$' -bench 'StoreLazyJSDocV1' -benchtime 20x -count 5 ./internal/storeparser/
```

## 10. 巻き戻された reparse の修正 (2026-09-29)

指示書は [store-checker-7d1-rewind-fix-instructions-20260929.md](store-checker-7d1-rewind-fix-instructions-20260929.md) (以下「修正指示書」)。生の出力は `_store-checker-7d1-results/rewindfix-*`。

### 10.1 `rewind` の diff (報告 (a))

修正指示書 §1 のとおり。7d-1 の working tree (2026-09-29 00:23、`kpc-source-sha256.txt` と一致を確かめてから着手) に対して:

```diff
 	p.reparsedClones = p.reparsedClones[0:state.reparsedClonesLen]
-	p.b.Truncate(state.nodes, state.extra)
+	// The Pointer parser keeps the reparse list across a rewind, so a @typedef,
+	// @callback or @import reparsed in a discarded speculation still goes into
+	// the enclosing statement list. Its rows must outlive the rewind: the
+	// speculation's nodes stay as dead rows rather than being truncated. The
+	// length test keeps the common rewind (an empty list, always so in TS) to
+	// one branch.
+	if len(p.reparseList) == 0 || !p.reparseListFrom(state.nodes) {
+		p.b.Truncate(state.nodes, state.extra)
+	}
 	if storeChecks {
-		p.checkNoTruncatedRefs(state.nodes)
+		p.checkNoTruncatedRefs(p.b.Len())
 	}
```

`reparseListFrom` は修正指示書 §1 の形のまま `checkNoTruncatedRefs` の前に置いた。呼び出し側の長さの判定 `len(p.reparseList) == 0 ||` は、最初の KPC の後でユーザーの判断で足した (§10.6)。`checkNoTruncatedRefs` のコメントの末尾を「keeps the third, whose rows the rewind then leaves untruncated」に書き換えた。

変更したファイル: `storeparser/parser.go`、`storeparser/parser_test.go` (`TestRewoundReparse`、`TestEquivalence` の `/rewound-typedef.js`、`TestDeadNodes` の 1 件と JS 判定の `x = (` の prefix)、`storebinder/binder_test.go` (`smallSources` の `/rewound-typedef.js`)、`testdata/fuzz/FuzzEquivalence/rewound-reparse-{typedef,callback,import}`。ほかに本報告と 7d-1 指示書 §4.2 の 1 行。生成器と変換器を再実行して生成物は不変 (着手前と後の sha256 の差は上の 5 ファイルと seed 3 本だけ、`rewindfix-{pre,post}-fix-tree.sha256`)。

### 10.2 行列 (報告 (b))

`TestRewoundReparse` の 13 位置 × 3 tag。位置 10 / 11 は、修正指示書の形 (`{ C m() {} }`) では JSDoc が host に付かないので、ユーザーの判断で comment の前後に改行を入れた形に差し替えた (下の注)。修正前は一時的に subtest の中で `recover` して全セルを記録した (`rewindfix-matrix-before-*.txt`)。panic は tag なしが `store: invalid node edge`、storechecks が `checkNoTruncatedRefs`。「不一致」は panic せずに比較が落ちたもの (再利用された行を statement に入れた。例: 13/typedef は `visits 33, want 21`)。

| # | 位置 | 修正前 tag なし (typedef / callback / import) | 修正前 storechecks | 修正後 (両 build) |
| --- | --- | --- | --- | --- |
| 1 | `x = (C a, b);` | panic / panic / panic | panic ×3 | 一致 |
| 2 | `x = (C a);` | panic / panic / panic | panic ×3 | 一致 |
| 3 | `x = (C a, b) => a;` | 一致 ×3 | 一致 ×3 | 一致 |
| 4 | `x = (C a = 1, b);` | panic / panic / panic | panic ×3 | 一致 |
| 5 | `x = (C {a}, b);` | panic / panic / panic | panic ×3 | 一致 |
| 6 | `x = async (C a, b);` | panic / panic / panic | panic ×3 | 一致 |
| 7 | `x = c ? (C a) : d;` | panic / panic / panic | panic ×3 | 一致 |
| 8 | `x = c ? (C a) : d => e;` | **不一致** / panic / panic | panic ×3 | 一致 |
| 9 | `x = ((C a, b), c);` | **不一致** / panic / panic | panic ×3 | 一致 |
| 10 | `x = (a = class {\nC\nm() {} }, b);` | **不一致** ×3 | panic ×3 | 一致 |
| 11 | `x = (a = {\nC\nm() {} }, b);` | **不一致** ×3 | panic ×3 | 一致 |
| 12 | `f(C a, b);` | 一致 ×3 | 一致 ×3 | 一致 |
| 13 | `x = <div>{(C a, b)}</div>;` (`/a.jsx`) | **不一致** / panic / panic | panic ×3 | 一致 |

修正前は 39 入力中 36 が失敗 (tag なし: panic 27、不一致 9。storechecks: panic 36)。失敗しないのは speculation が成功する 3 と speculation の無い 12 だけ。修正後は 39 入力とも両 build で一致。10 / 11 の修正前の欄は差し替え後の形を一時テストで測った値で、`rewindfix-matrix-before-*.txt` は差し替え前の形のもの (10 / 11 は一致)。

- tag なしの不一致 9 件は、truncate 後に再利用された行を `reparseList` が指し、別のノードを statement list に入れたもの。panic より悪い (黙って違う木を返す)。storechecks はこの 9 件も panic で捕まえていた。10 / 11 の不一致の例は `visits 21, want 23` で、import は `Imports` も違った。
- **10 と 11 の差し替え**: 修正指示書の形では `{` と同じ行の comment が method の JSDoc にならず、Pointer も Store も typedef を作らない (`x = class { /** @typedef {number} T */ m() {} };` でも statement は ExpressionStatement だけ)。差し替え前の形は修正前から一致していた。差し替え後の形では、Pointer の statement は 3 つで typedef が 2 回入る (rewind された speculation の分と、本 parse で class member の list から押し出された分)。Store も同じ木を作る。

### 10.3 テストと等価

- store / storeparser / storebinder を tag なしと `-tags storechecks` で: 失敗は既知の `FuzzEquivalence/a708303f611b95c8` (`ExternalModuleIndicator differs`) の 1 件だけ。新しい seed 3 本、`TestEquivalence/rewound-typedef`、storebinder の `TestEquivalence//rewound-typedef.js` は PASS。`go test ./internal/ast/ ./internal/parser/ ./internal/binder/` も PASS。`go build ./...`、`go vet` は通る。
- parse の等価: 17,415 file (JS 1,456)、mismatch 0、diagnostics 全 file 同じ。Store NodeCount 2,626,010・dead 22,793 は検証時 (`test-notag.txt`) と同じ。
- bind の等価: 17,415 file、parse mismatch 0、宣言の Store の検査 717,627 件 (不変)。
- storechecks で corpus 全体を通し、`checkNoTruncatedRefs` は発火しない。

### 10.4 dead node (報告 (c))

`x = (/** @typedef {number} T */ a, b);` は dead 10 (NodeCount 24、visit 14、eager JSDoc 0)。内訳は rewind された arrow の引数 2 つ (Parameter + Identifier ×2) と a の JSDoc 6 (JSDoc、JSDocTypedefTag、その Identifier 2 つ、JSDocTypeExpression、NumberKeyword)。typedef の JSTypeAliasDeclaration とその部分木 (reparse の clone) は statement として生きている。`TestDeadNodes` に 10 で固定した。typedef の無い `x = (a, b);` と `x = (/** @type {number} */ a, b);` は dead 0 のまま (Truncate する経路が変わっていないこと)。

### 10.5 inline (報告 (d))

`go test -gcflags=-m=2`:

- `reparseListFrom`: can inline (cost 16)。`rewind` の中で inline される (`inlining call to (*Parser).reparseListFrom`)。
- `rewind`: cannot inline、cost 120 → 142、長さの判定を足して 148 (修正前から budget 80 を超えている。`inline-storeparser-full.txt`)。
- inline されても、空の `reparseList` の経路は約 7 命令増えていた (§10.6)。長さの判定を足した後の objdump では、空の経路で増えるのは `ADD`・`LDP`・`CBZ` の 3 命令。
- `lookAhead`: cannot inline、cost 149 のまま (`rewind` を inline しないので変わらない)。

### 10.6 KPC (報告 (e))

binary とスクリプトは scratchpad (`kpc/`) に置いた。

- before: `_store-checker-7d1-results/storeparser.after.kperf.test` (sha256 `9f6d09d0…a0a38f`、`kpc-binary-sha256.txt` と一致)。
- after (1 回目、長さの判定なし): 修正後の source から `GOWORK=off go test -tags kperf -c` (`storeparser.rewindfix.kperf.test`、sha256 `f2702f95…c29b848`)。
- after (2 回目、長さの判定あり、位置 10 / 11 差し替え後): `storeparser.rewindfix2.kperf.test`、sha256 `cd26e9c5…b8cacc7b`。
- `run-kpc-rewindfix.sh`: source (60 file、`source-sha256.txt`) と 2 つの binary の sha256 を実行前後で照合、load < 1.5 を待ち (2 回目からは `sudo -v` の後で待つ)、`StoreParseKPCV1` を before1 → after → before2、`-test.benchtime 20x -test.count 5`。結果は `_store-checker-7d1-results/kpc-rewindfix-*-<stamp>.txt`、集計は `verify-scripts/kpc-summary.py`。

1 回目の結果 (長さの判定なし、stamp `20260929-093314`、各 5 run の中央値。before は before1 と before2 の平均):

| | cycles S/P (before1 / after / before2) | Store inst after/before | Store cycles after/before | Pointer inst after/before | A/A (before2/before1) Store inst / cycles |
| --- | --- | --- | --- | --- | --- |
| checker.ts | 0.831 / **0.827** / 0.830 | +0.52% | −0.08% | +0.43% | −0.02% / +0.71% |
| dom | 0.831 / **0.821** / 0.828 | +0.44% | +0.02% | +0.10% | +0.09% / +0.77% |

- **線 (cycles S/P ≤ 1.00) は両方とも内側。** 7d-1 の after (0.826 / 0.824) と同じ水準で、Store の cycles は before と差が無い。
- **Store の inst は見積り (+0.3% 以内) を dom で超えた。** 見積りの前提 (rewind 1 回あたり 2〜3 命令) が違っていた。`rewind` の objdump を before と after の binary で比べると、`reparseList` が空のときの経路で増えたのは約 7 命令 (slice の読み込み、loop の index の初期化、`JMP`、`CMP`、`BGE`、`Truncate` の前で `state.nodes` / `state.extra` を読み直す分)。rewind の回数を一時カウンタで数えると、1 回の parse で checker.ts 28,620 回・dom 66,862 回。7 命令をかけると、checker.ts +0.2M (+0.09%)・dom +0.47M (+0.42%) になる。
- 実測の Store の inst は checker.ts +0.52%・dom +0.44%。ただしコードが変わっていない Pointer も checker.ts で +0.43% (dom +0.10%) 動いた。Pointer の分を引くと、Store の増分は checker.ts +0.09%・dom +0.34% で、上の見積りと合う。Pointer の +0.43% の原因は確かめていない (binary を作り直したことによる配置の差、と推測)。
- **計測条件の不備**: スクリプトは load < 1.5 を待ってから `sudo -v` するので、パスワード入力を待つ間に待ち時間の判定が古くなった。load の判定は 09:33、結果のファイルは 16:45 に書かれ、終了時の 1 分 load は 2.77。A/A の cycles の差は 0.7〜0.8% で、線までの余裕 (0.82 対 1.00) に比べて小さいので、判定は変わらない。次からは `sudo -v` の後で load を待つ。
- これを受けて、ユーザーの判断で修正指示書 §1 の 6 の代替 (呼び出し側の長さの判定) を足した。空の経路の増分は 3 命令になり、見積りは checker.ts +0.09M (+0.04%)・dom +0.2M (+0.18%)。

2 回目の結果 (長さの判定あり、stamp `20260929-172421-early-check`、各 5 run の中央値。「Pointer 差し引き」は Store の after/before から Pointer の after/before を引いた値):

| | cycles S/P (before1 / after / before2) | Store inst after/before | Pointer inst after/before | Pointer 差し引き (見積り) | Store cycles after/before | A/A (before2/before1) Store inst / Pointer inst |
| --- | --- | --- | --- | --- | --- | --- |
| checker.ts | 0.823 / **0.820** / 0.829 | −0.20% | −0.20% | +0.00% (+0.04%) | −1.06% | +0.52% / +0.40% |
| dom | 0.824 / **0.827** / 0.824 | −0.05% | −0.20% | +0.15% (+0.18%) | −0.15% | +0.39% / +0.34% |

- **線 (cycles S/P ≤ 1.00) は両方とも内側。** checker.ts 0.820、dom 0.827。
- 今回は inst の A/A が +0.4〜0.5% と、1 回目 (−0.02% / +0.09%) より大きい。Store と Pointer が同じ向きに動くので、同じ session の中の binary によらないずれ。1 回目の Pointer の +0.43% もこのずれと読むのが自然 (原因は確かめていない)。そのため Store の inst の素の after/before (−0.20% / −0.05%) は A/A の幅の内側で、修正の費用は読めない。Pointer を差し引いた値 (+0.00% / +0.15%) は 3 命令 × rewind 回数の見積り (+0.04% / +0.18%) と合う。
- load: `sudo -v` の後で待つようにした。load の判定は通ったが、直後の `uptime` の 1 分 load は 1.90、終了時 1.84 (loadavg は 5 秒ごとの更新で、判定の瞬間だけ 1.5 を切った可能性がある)。A/A の cycles は +0.97% / +0.58% で、線までの余裕に比べて小さい。

### 10.7 修正指示書に無い判断 (報告 (f))

- 行列の修正前の記録のために、subtest に `recover` を一時的に入れて全セルを取り、記録の後で外した (最終の test に `recover` は無い)。
- `TestDeadNodes` の JS 判定に `strings.HasPrefix(c.source, "x = (")` を足した (既存の判定は `/**`・`class`・`return` の prefix で、新しい入力はどれにも当たらない)。
- 行列の位置 10 / 11 を、comment の前後に改行を入れた形に差し替えた (ユーザーの判断、§10.2)。修正指示書の形では JSDoc が host に付かず、意図 (member の list から外へ押し出される typedef) を試していなかった。
- `rewind` の呼び出し側に長さの判定 `len(p.reparseList) == 0 ||` を足した (ユーザーの判断、§10.6)。`reparseListFrom` は inline されていたが、1 回目の KPC で dom の inst が見積りを超えたため。
