# Store checker 変換 (TODO 7d-2a) 検証報告

作成日: 2026-09-30。検証指示は [store-checker-7d2a-verification-instructions-20260929.md](store-checker-7d2a-verification-instructions-20260929.md) (以下「検証指示」)。対象は作業ツリー (未コミット、before **2341effa51**) と [実装報告](store-checker-7d2a-implementation-report-20260930.md) (以下「実装報告」)。生データは `_store-checker-7d2a-results/claude/` (ignore 対象)。

同じ時間帯に別の検証者 (Codex アプリ) が同じ tree と同じ `_store-checker-7d2a-results/` で検証を走らせていた。出力の衝突を避けるため、この報告の生データは `claude/` の下に置き、file 名に `-claude` を付けた。repo の file は一切変えていない。壊す試験・計数・修正案の試験は、全部 scratchpad の複製で行った。

## 1. 結論

**不合格 (正しさのバグ 1 件)。** ほかの線は合格か、ユーザーの判断待ち。KPC は 4 本とも線の内側 (§5)。

- **P1 (正しさ)**: `storeparser.ParseIsolatedEntityName` の結果の識別子は、`Text()` を呼ぶと必ず `slice bounds out of range [:-1]` で panic する。Store の識別子の text は source を `src[data:end]` で切り出すので (store.go:266)、`Builder.FinishSynthetic` (checker_api.go:510) が end を -1 にすると読めなくなる。Pointer の識別子は text を自分で持つので `markAsSynthetic` の後も読める。storechecker は jsx.go:1331・1355・1376・1405・1411 でこれを使う (`jsxFactory`・`jsxFragmentFactory` の設定と `@jsx`・`@jsxFrag` pragma)。2a は checker を動かさないので表に出ていないが、2b で JSX の file を checker にかけると panic する見込み。この関数の試験は repo に無い。修正案 (識別子の text を `texts` に移してから位置を消す、+10 行) を複製で試し、検証指示 §3.3 の入力 18 個で Pointer と一致した (§4.4)。**repo には入れていない** (別の検証者が同じ tree を検証中のため)。§7 の 1。
- **要件を満たしていないもの**: nameresolver.go:64-74 の JSDoc の 3 分岐のうち、`KindJSDocParameterTag` と `KindJSDocReturnTag` は corpus で **0 回**しか通らない (Pointer の nameresolver に計数を入れて数えた。`KindJSDoc` は 7,752 回)。実装報告の 329 / 126 は「親の鎖に JSDocParameterTag がある識別子」の数で、分岐を通った数ではない。狙った入力 6 個でも 0 回で、Go 版では到達しない分岐の可能性が高い。§7 の 2。
- 生成物は 2 つの生成器で byte 一致で再現し、build・vet が通る。Pointer 側の package と go.mod に diff は無く、production から store 系への import も `unsafe` の増加も無い。parse / bind の等価と宣言の検査の数は 7d-1 と同じ。
- 失敗は既知の `FuzzEquivalence/a708303f611b95c8` (`ExternalModuleIndicator differs`) だけ (tag なし・storechecks の両方)。storechecks の `TestPrinterCorpus` は負荷の下で既定の 10 分の timeout に掛かった。timeout を延ばした再実行では通った (6m40s、不一致 0)。
- 2 つの単体等価は数が再現し、不一致 0。壊す試験は nameresolver の 3 つ (locals の条件、globals の検索、`SymbolReferenced`) と printer の 1 つ (`canUseSourceFile`) で全部落ちた。
- P2 は 37 + 37 件の全部を Pointer と diff した (subagent に任せ、要点を自分で抜き取り検査した)。**理由の外の違いは 0 件**。hash は全部一致し、hash を書き換えても Pointer を変えても変換器は止まり、何も書かない。
- 到達計算の刈りすぎは 0 件 (20 件の再帰点検と全域の点検)。刈り残し (実行されないコード) は 56 関数ある (§3.2)。
- 既存の関数の本体が変わったのは store 6・storeparser 5・storebinder 5。`ForEachChild` は `SyntheticExpression` の case の offset (+1→+2) だけ、storeparser / storebinder は `File` の field の移行の置き換えだけ。before から在る store の 2,374 関数の inline の判定と cost は全部同じ。

| 線 | 結果 |
| --- | --- |
| 再生成で `git status` 不変 | 合格 (status と、変更・未追跡の 122 file の sha256 が一致) |
| build / vet | 合格 |
| ガード (Pointer 側の diff、go.mod、import、unsafe) | 合格 |
| 既知の fuzz seed 以外の失敗 | 合格 (storechecks の printer は timeout を延ばして通過) |
| parse / bind の等価、宣言の検査 | 合格 (17,415 / 17,415 / 717,627、dead 22,793) |
| 変換の機械性 (規則、R8、R4') | 合格。名前で決める箇所は §3.1 |
| 到達計算 | 合格 (刈りすぎ 0) |
| P2 の理由の外の違い | 合格 (0 件) |
| nameresolver の単体等価 | 数は合格。JSDoc の 2 分岐は 0 回 (§7 の 2) |
| printer の単体等価 | 合格 |
| 変換の境界 (§3.3) | **不合格** (`ParseIsolatedEntityName`) |
| 既存の関数の変更と inline | 本体の変更あり、inline は不変 |
| KPC Walk ≤ 1.15 / Parse checker.ts ≤ 1.00 / dom ≤ 1.00 / Bind ≤ 1.10 | 合格 (1.092 / 0.832 / 0.825 / 1.003) |

## 2. 再現・build・テスト (検証指示 §1)

環境: go1.27.1 darwin/arm64、Apple M1 8GB、HEAD 2341effa51 + 作業ツリー。load average は開始時 1.71。別の検証者の build が重なり、途中で 63 まで上がり swap を 2.9GB 使った (`env-start.txt`)。

1. `node tools/scripts/tsc/generate.ts` と `(cd tools && go run ./cmd/storetranslate)` の後、`git status --porcelain --untracked-files=all` と、変更・未追跡の 122 file の sha256 が前後で一致した (`status-*-regen.txt`、`worktree-sha-*-regen.txt`)。範囲外の変更は `mise.toml` (20:04:47 作成、中身は `codex = "latest"`) だけで、実装とは無関係。
2. 生成物の file は全部、先頭に `Code generated … DO NOT EDIT.` がある。手書きは `*_manual.go` と、実装指示書 §4 と §5.1 の file (`checker_api.go`・`deepclone.go`・`file.go`・`node_helpers.go`・`factory.go`・`store.go`・`storeparser/isolated.go`・2 つの `region.go`)、ユーザーが了承した `visit.go` だけ。
3. ガードの diff (`guard-diff.txt`) は空。`tools/go.mod` の diff も空。`go list -deps` で、store 系以外の package の依存に store 系は現れない。`unsafe` の出現は before と同じ store.go の 3 行で、増えた 3 件は診断の名前と comment。
4. `file-migration.diff` の hunk は全部 `File` の field の置き換えだった。語の差分: `file.Text` → `Text()`、`FileName` → `FileName()`、`Path` → `Path()`、`Imports = append(…)` → `AppendImport`、`Imports` の読み → `ImportRefs()`、`&store.File{…}` → `store.NewFile(…)`。file.go に `var _ ast.HasFileName = (*File)(nil)` があり、build が通る。
5. テスト (`test-notag.txt`、`test-storechecks.txt`、`test-storechecks-printer-rerun.txt`、`test-storetranslate.txt`): 失敗は `FuzzEquivalence/a708303f611b95c8` だけ (`parser_test.go:411: ExternalModuleIndicator differs from the Pointer file`)。
6. 数: parse 17,415 file、2,584,467 ノード、JSDoc 54,459、dead 22,793 (0.88%)、診断 17,415 file 同じ。bind 17,415 file、宣言の Store の検査 717,627。7d-1 と同じ。

## 3. 変換の機械性 (検証指示 §2)

### 3.1 規則

- 変換器に checker の関数名・変数名の表は無い。**名前で決める箇所**は次のとおり。どれも store の API か Pointer と同名の API への対応だが、型だけからは決まらない。
  - `writes.go:23-29` の `nodeWriters`: field 名 → writer 名の 7 項目 (実装指示書 R8 の行をそのまま表にしたもの)。
  - `translate.go:1285-1294` の `nodeHelpers`: core の helper 名と、比べる型引数の位置。7d-0 から在り、`Coalesce` を足した。
  - `reach.go:709` の `ruleRewritten` (7d-0 から)。`translate.go:787` の `FlowNodeData().FlowNode` → `FlowNode()`。`translate.go:1422` の「`As` で始まる method は恒等」。`translate.go:1494` の「store の型に `<name>List` の method があればそれを呼ぶ」。`slices` の `Contains`・`Index`・`Compact`。
- R8 の `f` の探し方 (`writes.go:170-217`) は、receiver と引数の struct の field で `ast.NodeFactory` 型のものを型で探し、変数名は見ない (subagent が確認し、私も読んだ)。27 件は全部 writer になり、`f` は `c.factory` / `b.f`。nodecopy.go:886 は `b.f.SetListRange(res, int32(-1), int32(-1))` (Store nodecopy.go:887)。
- 書き込みは全部で 42 件 (変換 36、P2 6)。実装報告の「変換 35、P2 7」は数え違いで、合計は合う。**source のノードへの書き込みは 0 件。** nodebuilderimpl.go:3369 は `symbolToTypeNode` の ImportType の出どころが `b.f.NewImportTypeNode` だけなので、静的に証明できる (実装報告は「静的な証明ではない」とした)。実装報告の nodecopy.go:886 の根拠「nodecopy.go:313」は別の visitor で、正しくは 878。
- R4': 生成物の `.Nodes()` は checker で 11 箇所。実装指示書の一覧と一致する。ほかに ast 3・printer 2 と P2 の中にある。`getEffectiveCallArguments` は変換の出力。
- 変換テスト: R2〜R8・R11・NodeId・switch と、R8 の Factory が 0 個・2 個の problem のテストはある。**R12〜R17 と、実装報告 §1.2 で「足した規則」とした sameShape・flow の method・`&data.Node`・`As` の恒等・基本型の結果の変換にはテストが無い** (出力のコンパイルと単体等価で間接に確かめているだけ)。
- (g) (読みのまとめ) の規則は無い。変換器が作る一時変数は、R8 の右辺の退避と R3 の ref の range の 2 つだけ。
- **変換器は `var _ I = (*T)(nil)` を黙って落とす** (`world.go:145` が `_` を飛ばす)。対象の 5 件は全部消えている: checker/emitresolver.go:17、checker/tracer.go:79、printer/textwriter.go:12、printer/singlelinestringwriter.go:19、binder/referenceresolver.go:29。最後の 1 件は「file 単位で全部」出す file の中にある。その結果、`storechecker.EmitResolver` は `storeprinter.EmitResolver` を満たさないまま build が通っている (Store 側に代入が無いため)。

### 3.2 到達計算

- 到達の表は実装報告 §1.3 と file 単位で一致した。生成物の関数の集合は、到達集合 (P2 を除く) と 3,804 = 3,804 で一致した。変換器の `main.go` を overlay で差し替え、repo に書かずに到達集合を出して比べた。
- 7d 設計の試作との差: checker.go は同じ (1,490)、exports.go は RTA と同じ (2)。emitresolver.go は +7、tracer.go は +31、types.go は +5。emitresolver の +7 は `binder.ReferenceResolver` の CHA。tracer の +31 は、`wrapType` が `tracing.TracedType` として返す型の全 method で、試作が落としていたもの。
- **刈りすぎ: 0 件。** seed 20260930 で選んだ 20 件は、全部 (a) を根に向かって辿り、(b) か参照 0 で終わった。全域の点検では、出力した関数が名指す未出力の関数は、規則で説明できる 3 種類 (ast の method、ast に残す 21 関数、binder の手書き版 3 関数) だけだった。interface の実装 (`SymbolTracker`、`EmitTextWriter`、`TracedType` など) は全部出ている。`checker.Program` の実装型は表の外。
- **刈り残し (実行されないコード) 56 関数**。コンパイルには無害。
  - P2 の本体の selector を名前だけで照合する (`reach.go:356` の `methodsNamed`) ため、同名の method が全部到達する: `changetrackerwriter.go` の 27 全部と、checker の 8 (`Signature.Flags` など)。実装報告は `changetrackerwriter.go` を「EmitTextWriter の CHA で入った」と書いたが、入口は P2 の `p.writer.HasTrailingComment()` (printer_manual.go:55) の名前一致。構築元の `NewChangeTrackerWriter` は刈られている。
  - `binder.ReferenceResolver` × `EmitResolver` の CHA の 8。どこからも呼ばれない (§7.2 の 1 を参照)。
  - 関数値として名指されるだけで実行されない emitresolver の 5。
  - List 版の元の関数 6 (`checkTypeParameters`・`reportUnusedVariableDeclarations`・`appendTypeParameters`・`getInstantiatedConstructorsForTypeArguments` は呼び出し元 0、`checkTypeParametersNotReferenced`・`getConstructorsForTypeArguments` は死んだ関数からだけ)。
- services.go・exports.go・emitresolver.go 由来の 32 関数の根からの経路は、subagent が file:line の鎖で全部出した (私は `emitresolver.go` の 7 method に呼び出し元が無いことを grep で確かめた)。名指しの経路が無いのは上の CHA の 7 と `getReferenceResolver` だけ。

### 3.3 P2

- 数と行数: storechecker 37 件 (置き換え 25 + List 版 11 + 型 `Program`)、宣言 1,359 行・file 1,667 行。storeprinter 11 / 232 / 323、store 16 + 4 / 270 / 408、storescanner 3 / 16 / 45、storepseudochecker 2 / 22 / 39、storemodule 1 / 54 / 73。実装報告と一致する。ただし実装報告 §2.2 の store の「7d-1 以前から 106 行」は 127 行が正しい (127 にすると合計 270 と合う)。
- hash: 変換器の定義 (`func` / `type` から閉じ括弧まで、doc comment を除く、sha256 の先頭 16 桁) で現在の Pointer から独立に計算し、74 件の全部が一致した。scratchpad の複製で、(1) `jsx_manual.go` の hash を 0 に書き換える、(2) Pointer の `parseIsolatedEntityName` の本体を 1 行変える、の両方で変換器が `the Pointer declaration … changed` で止まり、生成物を 1 file も書かなかった (`verify-copy/hash-run{1,2}.txt`)。
- **P2 の理由の外の違い: 0 件** (storechecker 37 件、ほかの package の全件)。subagent が Pointer と 1 行ずつ diff し、私は List 版の呼び出し数と `NewChecker` の region の 2 行を読んで確かめた。
  - `NewChecker`: 変換器に出させた版 (scratchpad で `checker_manual.go` から抜いて実行) との diff は、`region := store.NewNodeRegion(program.IDSpace())` と `c.factory = store.NewFactory(region, synthStoreCapacity, store.FactoryHooks{})` の 2 行だけ。同じ実行で出た storechecker の他の 23 file は、repo の生成物と byte 一致した。
  - List 版: list を渡す呼び出しは全部 List 版を、`[]store.Node` を渡す呼び出しは全部元の関数を呼ぶ。Pointer の呼び出し数と Store の (元 + List 版) の数は 11 関数とも一致した。**実装報告 §2.2 の「どれも list と slice の両方の呼び出し元を持つ」は 11 中 6 で誤り** (§3.2 の死んだ元の関数)。
  - 意味の違いではないが挙げておくもの:
    - `storeprinter.nextContainer` が使う手書きの `Node.NextContainer()` (checker_api.go:467) は、root の SourceFile に対して `Node{}` を返す。storebinder は最初の container (root) を `Bound.containers` に入れない (binder.go:2622) が、Pointer の root の NextContainer は最初の入れ子の container。今の呼び出し元 (`generateNameForModuleOrEnum`) は ModuleDeclaration / EnumDeclaration しか渡さないので結果は変わらない。潜在的な不一致。
    - `VisitSliceList` は変わらなかったときも `nodes.Nodes()` を確保する (Pointer は確保しない)。変わったときは 2 回写す。`isUniqueLocalName` は `NextContainer` の線形探索で O(C²)。どちらも 2c の性能の話。

### 3.4 生成物の読み

seed 20260930 で checker.go から 10、flow / relater / nodebuilderimpl / jsx から 10 を選び、nil の比較・ノードの `==`・list の range・map の key・書き込み・`.Nodes()` を含む関数を 5 つ足して、25 関数を Pointer と並べて読んだ (subagent。私は 3 関数を抜き取って読んだ)。**規則の置き換えの外の違いは 0 件。** node の nil 比較は `IsNil()` になり、symbol・type の nil 比較は `== nil` のまま残っている。`!=` の `SameNode` 化で否定が保たれ、短絡の順序も同じ。生成物に NodeKey の map を range する箇所は無い。

## 4. 単体等価 (検証指示 §3)

### 4.1 nameresolver

- 数は実装報告と一致した: 17,415 file、`Resolve` 10,235,856 回 / 側、nil 以外 1,803,200 (Value 1,412,216、Type 355,476、Namespace 35,508)、不一致 0。callback: `Lookup` 33,552,972、`GetSymbolOfDeclaration` 7,573,024、`OnFailedToResolveSymbol` 4,214,388、`OnSuccessfullyResolvedSymbol` 900,836、`SymbolReferenced` 696,214、`Get/SetRequiresScopeChangeCache` 308 / 53、`OnPropertyWithInvalidInitializer` 266、`Error` 80。fixture 9 本が通る。
- 比較が本当に比べていること:
  - callback は両側で同じ意味で、Store だけの正規化や skip は無い (コードを読んだ)。
  - symbol の対は `bindComparerFor` が bind の結果から作り、Resolve の結果は使わない。対の無い symbol が結果になると「result unpaired」で不一致になる。
  - ただし `jsdocPairs` は、JSDoc の数か部分木の長さが両側で違うと **黙って飛ばす** (nameresolver_test.go:328・333)。複製で数えると、nameresolver と HasData の corpus の両方で飛ばした数は 0 だった。今の結果は正しいが、試験としては不一致を数える形に直すべき (§7 の 4)。
- 識別子の位置 (複製の試験で、Pointer の祖先の鎖で分類): 入れ子の関数 263,569、class 80,574、namespace 89,374、enum 8,468、`with` 112、catch 738、型の parameter の宣言の中 23,000、`arguments` 659、JS の `require` の呼び出し 128、JSDoc の中 15,509。Pointer が `arguments` の番兵を返したのは 1,488 回、`require` の番兵は 1,528 回。locals で解決した location の kind は 26 種 (FunctionDeclaration 800,224 から ClassStaticBlockDeclaration 380 まで、CatchClause 1,456 を含む)。
- **JSDoc の分岐 (nameresolver.go:64-74)**: Pointer の nameresolver (複製) の分岐の入口に計数を入れ、function-like の location で locals に結果があったときの `lastLocation` の kind を数えた。`KindJSDoc` 7,752、**`KindJSDocParameterTag` 0、`KindJSDocReturnTag` 0**。`@callback` + `@template`、`@template` 付きの JS / TS の関数、`@typedef` など狙った 6 入力でも 0 だった (`verify-copy/zz_verify_jsdocbranch_test.go.txt`)。Pointer の nameresolver.go:313-316 の comment も、JSDoc の tag の location の扱いを「今は no-op」としている。
- 壊す試験 (複製、各回の後に sha256 で元に戻ったことを確認):

| 変更 | 結果 |
| --- | --- |
| 型の parameter の見える範囲の条件から `lastLocation.Kind() == ast.KindTypeParameter` を消す | corpus 不一致: 結果 38,160 + 688、callback の列 30,164 + 8,684。fixture は通る |
| globals の検索 (Pointer nameresolver.go:327 に当たる行) を消す | corpus の callback の列 8,430,304。fixture は `globals` を含む 7 本が落ちる (`excludeGlobals` と `alias import` は通る) |
| `SymbolReferenced` の呼び出し (322 に当たる行) を消す | corpus の callback の列 696,214 (その callback の全部)。fixture の `alias import`・`alias export`・`isUse` が落ちる |

### 4.2 printer

- 数は実装報告と一致した: 17,415 file、55 cell で不一致 0 (tag なし 1m58s、storechecks 6m40s)。両方 panic は root 28、statement 33、型と型の複製 682、entity name 0。どの cell も大半は panic していない。
- 比較の相手は Pointer の parser と Pointer の printer の出力で、Store どうしではない。設定は checker/printer.go の 5 関数と同じ。出力の長さの合計は、例えば Defaults × file で root 20.2MB・statement 46.6MB・型 3.9MB・entity name 8.1MB。
- **両方 panic の中身** (実装報告は取っていなかったので、複製で recover の値を集めた): `unhandled TypeNode: KindTemplateLiteralTypeSpan` 13,640 件と `unhandled statement: KindJSImportDeclaration` 470 件の 2 種類だけで、両側の message は同じ。どちらも、その kind を直接印字したときの Pointer の printer の既知の制約。
- **source file が nil の経路**: fixture (`TestPrinterNilSourceFile`) を読んだ。元のノードは Pointer の file あり `a` / nil `a` と違い、Store がそれぞれに一致する。複製は両方 `a`。fixture が突く nil 固有の分岐は `canUseSourceFile` (printer.go:241) の 1 つだけ。そこで複製の Pointer の printer の nil 固有の分岐 16 箇所に計数を入れ、corpus の nil の印字で通った回数を数えた。13 箇所は大量に通っていて、不一致 0 のまま比べられている (識別子の `getTextOfNode` 18,295,210、literal の `getLiteralText` 約 140 万、`linesBetweenNodes` 6,994,090、token の位置 6,085,820、`commentsBeforeToken` 1,457,064 など)。**通らない 3 つ**: `leadingLines` の先頭の子 (printer.go:539、`firstChild.Parent == nil` が要る)、`closingLines` の空 list、`isFileLevelUniqueNameInCurrentFile` (6148)。
- corpus で Pointer の出力が file あり / nil で違った印字の数: statement 63,669 (Defaults) / 約 18,300〜18,557 (他の 4 設定)、型 2,017 / 約 1,700、entity name 9,050 / 183、型の複製 9、entity name の複製 0。
- EmitContext の 4 項目は、Pointer の出力が設定のあり / なしで違う入力を使い、期待の文字列で固定している: emit flags `"あ"` / `"あ"`、textSource `'ab'` / `"ab"`、original `if /*c*/ (x) { }` / `if (x) { }`、合成の comment `/*c*/ z` / `z`。ノードは印字する EmitContext の Factory で作っている。**実装報告 §5.3 の表は escape が落ちて、emit flags が `"あ"` / `"あ"`、nil の fixture が `a` / `a` と書かれている** (報告の誤記。試験は正しい)。
- 壊す試験: `canUseSourceFile` から `!store.NodeIsSynthesized(node)` の項を消すと、corpus の 15 cell が落ちた。ただし 1 つの JS file (#8518) で Store だけが panic する形で、text の差としては出ず、fixture は通ったまま。この項の試験は弱い。

### 4.3 HasData (実装報告 §5.5、`TestHasDataCorpus`)

- 比較の相手は、Pointer の parser と binder が作ったノードの実行時の `XxxData() != nil` で、生成器と独立。9 つの述語の全部を比べている。17,415 file、2,638,926 ノード、272 kind で一致した (数は実装報告と同じ)。
- **corpus に現れない kind が 79 ある** (token・keyword の大半、`SyntheticExpression`・`SyntaxList`・`NotEmittedStatement`・`PartiallyEmittedExpression` など、`verify-copy/missing-kinds.txt`)。このうち述語が真になるのは `NotEmittedStatement` の `HasFlowNodeData` だけで、Pointer の struct も `StatementBase` 経由で `FlowNodeBase` を持つので一致する。ほかの 78 kind は Store が偽を返し、Pointer の struct もその base を持たない (`SyntheticExpression`・`PartiallyEmittedExpression`・`SyntheticReferenceExpression`・`SyntaxList`・`NotEmittedTypeElement`・`Token` を読んだ)。
- 既知の限界 (実装報告 §5.5 に書かれている): kind だけで決めるので、Pointer が `NewToken` で作る `TrueKeyword` などは Pointer が nil、Store が真になる。Pointer 全体を grep すると、その経路は printer/factory.go:1304 だけ (checker・nodebuilder は `NewKeywordExpression`)。生成物の comment「HasFlowNodeData is (*ast.Node).FlowNodeData() != nil」は無条件には成り立たない。

### 4.4 変換の境界 (検証指示 §3.3)

- **`ParseIsolatedEntityName`: 不合格 (P1)。** 検証指示の入力 (`React.createElement`・`h`・`a.b.c`・`a.`・`1`・空) に 12 個を足した 18 個で Pointer と比べる試験を複製に書いた (`verify-copy/zz_verify_isolated_test.go.txt`)。nil になる入力は Pointer と一致する。しかし nil でない結果は、木を辿って識別子の `Text()` を呼んだところで `panic: runtime error: slice bounds out of range [:-1]` (store.go:266、`identifierText` の `src[d:end]`)。修正案 (`verify-copy/fix-finishsynthetic.diff`):

```go
	for i := 1; i < len(s.nodes); i++ {
		h := &s.nodes[i]
		// An identifier's text is the source from data to end: move it to
		// texts before the range goes.
		if (h.kind == ast.KindIdentifier || h.kind == ast.KindPrivateIdentifier) && h.data&textInTexts == 0 {
			text := s.src[h.data:h.end]
			at := uint32(len(s.extra))
			s.extra = append(s.extra, textInTexts|uint32(len(s.texts)), uint32(len(text)))
			s.texts += text
			h.data = textInTexts | at
		}
		h.pos, h.end = -1, -1
	}
```

  修正後は 18 入力の全部で、kind と text の木が Pointer と一致した。位置は全部 (-1, -1)、`File()` は nil、別の Factory からの `SetRange` は `store: write to a Store that is not a synthetic Store of this region` で拒否され、StoreID は `space.Next()` から来る (nil になる入力も 1 つ消費する)。
- region と Factory: `NewChecker` の P2 は `store.NewNodeRegion(program.IDSpace())` で region を 1 つ作り (checker_manual.go:719)、`getNodeBuilderEx` は `storeprinter.NewEmitContextIn(c.factory.Region())` で同じ region に EmitContext を作る。region の無い `storeprinter.NewEmitContext` と `NewNodeFactory` は無条件に panic する。`TestNewCheckerEmptyProgram` は、file 0 件の stub で Pointer と Store の `NewChecker` がどちらも正常に終わることを確かめていて、通る。
- R11: checker.go:12733 に当たる storechecker/checker.go:11886 は `func(d *ast.Diagnostic) bool` のまま。変換テスト (`TestRules7d2`) で固定されている。

## 5. 性能 (検証指示 §4)

1. 既存の関数の本体の変更 (go/ast で receiver の型込みで対にし、doc comment を除いて比べた。`funcdiff-{store,storeparser,storebinder}.txt`、道具は `verify-copy/funcdiff.go.txt`):

| package | before の関数 | 変わった | 消えた | 足した |
| --- | ---: | ---: | ---: | ---: |
| store | 2,098 | 6 | 0 | 617 |
| storeparser | 540 | 5 | 0 | 1 |
| storebinder | 179 | 5 | 0 | 28 |

   - store: `(Node).ForEachChild` は `KindSyntheticExpression` の case の offset が `+1` → `+2` になっただけ。`SyntheticExpression` の 5 関数は、先頭に `Type` の語を足した変更。`store/utilities.go` の before からの関数は 1 つも変わっていない。
   - storeparser (`finishSourceFile`・`parseJSDocForNode`・references.go の 3 つ) と storebinder (`bindSourceFileAsExternalModule`・utilities.go の 4 つ) は、§2 の 4 の置き換えだけ。
2. 既存の関数の本体が変わり、`ForEachChild` は Walk の、storeparser / storebinder の関数は Parse / Bind の経路にある。そこで検証指示どおり Walk / Parse / Bind の KPC をユーザーに依頼した。**4 本とも線の内側で合格**。
   - 実行したのはユーザーで、別の検証者 (Codex) のスクリプトと binary (`/private/tmp/store-7d2a-perf/`) を使った。結果は `performance-audit/kpc-*-20260930-212434.txt`、集計は `claude/kpc-summary-20260930-212434.txt`。
   - 出どころは確かめた:
     - after の binary 3 本は、私が作業ツリーから作った binary と sha256 が byte 一致した。
     - before の binary は build した場所が違うので sha256 は違う。ただし before の tree (`/private/tmp/store-7d2a-perf/before/tsc`) は、testdata と docs を除いて私の `git archive 2341effa51` と全部一致した。
     - 作業ツリーの source の sha256 も、検証の開始時と同じだった。
   - load average は開始時 1.48、終了時 4.92 (終わりの方は線の 1.5 を超えていた)。値は 5 run の中央値、S/P は同じ binary の中の pointer との比。

| 段 (線) | before1 | after | before2 | Store の inst (before1 → after) | Pointer の inst (before1 → after) |
| --- | ---: | ---: | ---: | ---: | ---: |
| Walk cycles S/P (≤ 1.15) | 1.096 | **1.092** | 1.093 | 27.98M → 27.97M | 21.57M → 21.58M |
| Parse checker.ts (≤ 1.00) | 0.824 | **0.832** | 0.824 | 216.65M → 217.39M (+0.34%) | 260.19M → 261.14M (+0.37%) |
| Parse dom (≤ 1.00) | 0.826 | **0.825** | 0.826 | 112.19M → 112.41M (+0.20%) | 124.01M → 124.22M (+0.17%) |
| Bind (≤ 1.10) | 1.022 | **1.003** | 1.020 | 126.26M → 126.51M (+0.20%) | 110.39M → 110.72M (+0.30%) |

   - after の Store の inst は Parse と Bind で +0.2〜0.34% 増えた。ただしコードが変わっていない Pointer も同じ run で +0.17〜0.37% 増えたので、差し引くとほぼ 0 になる。7d-1 の検証で A/A の inst は ±0.5% 動くと分かっており、その範囲に入る。
   - Parse checker.ts の after は、Store の cycles が +2.4%、Pointer も +1.4% で、両方の IPC が下がった。session のずれとみる。Bind の after の S/P が 1.003 に下がったのも、Pointer の cycles が +2.3% 上がったため。
3. inline (`-gcflags=-m=2`、出力は scratchpad、判定の一覧は `inline-{before,after}.decisions`): before から在る 2,374 関数の can / cannot と cost は全部同じ。`(*Store).node` 38、`Node.Parent` 45、`List.At` 78、`Node.FlowNode` 48、`Node.Symbol` 40、`Node.Kind` 4、`Node.Key` 41、`PropertyAccessExpression.Expression` 52、`CallExpression.Arguments` 43。

## 6. 判断を仰ぐこと

1. **`ParseIsolatedEntityName` の修正 (P1)。** §4.4 の修正案を入れてよいか。検証指示 §0 は正しさのバグを直すことを許しているが、別の検証者が同じ tree を検証中なので repo に書かなかった。入れるなら、同じ入力の試験 (`zz_verify_isolated_test.go.txt` の形) を storeparser に足すことを勧める。
2. **nameresolver.go:64-74 の `JSDocParameterTag` / `JSDocReturnTag` の 2 分岐。** corpus でも狙った入力でも 0 回。案は 2 つ: (a) Go 版では到達しないと認めて要件から外す (Pointer の comment も no-op とする)、(b) 通る入力を探して fixture を足す。私は (a) を勧める。ただしその場合、Store 側のこの 2 項の変換は試験されないまま残る。
3. (解決済み) KPC はユーザーが 21:24 に取った (§5)。

4. `jsdocPairs` の黙った skip (nameresolver_test.go:328・333) を不一致として数える形に直すか。今は 0 件なので結果は変わらない。
5. 変換器が `var _ I = (*T)(nil)` を落とすこと (§3.1)。2b で `storechecker.EmitResolver` を `storeprinter.EmitResolver` として使うなら、コンパイルで初めて分かる。
6. storechecks の `TestPrinterCorpus` は負荷の下で 10 分を超える (再実行で 6m40s)。検証指示のコマンドに `-timeout` を足すか。

## 7. 実装報告 §7.2 の 4 件と §5.5

1. **`referenceresolver.go` を足した**: 必要。storechecker の型の宣言は全部出す決定 (実装指示書 §3.2) なので、`EmitResolver` の field の型 `binder.ReferenceResolver` に Store 版が要る。ただし実装報告の理由「emitresolver.go の到達した関数が `binder.NewReferenceResolver` などを呼ぶ」は不正確。呼ぶのは `getReferenceResolver` で、それと `EmitResolver` の `GetReferenced…` の 7 method は、その interface の CHA だけで到達する。どこからも呼ばれない (grep で呼び出し元 0)。足した 15 関数と CHA で入った 8 関数は実行されないコード。
2. **`storemodule` を新設した**: 必要。checker.go:14550 が `storemodule.GetResolutionDiagnostic` を呼ぶ。P2 1 つで、型以外は Pointer と 1 行ずつ同じ。
3. **`Has<X>Data()` を生成した**: 妥当。§4.3 のとおり corpus と静的な確認で Pointer と一致する。限界は kind だけで決まることで、emit の helper の 1 経路だけが食い違う。
4. **List 版を 5 つ増やした**: 仕組みの範囲内で、呼び出しの選び方も正しい。ただし足した 5 つのうち 3 つ (`appendTypeParameters`・`getInstantiatedConstructorsForTypeArguments`・`getConstructorsForTypeArguments`) は、元の関数に生きた slice の呼び出し元が無い。実装報告の理由「どれも list と slice の両方の呼び出し元を持つ」は、この 3 つと元からの 3 つで誤り。元の関数が死んだコードとして出る。
5. **`TestHasDataCorpus`** (§5.5): 比較は独立で、Pointer の実行時の値と比べている。JSDoc も含むが、`jsdocPairs` の黙った skip を共有している (実際は 0 件)。corpus に無い 79 kind は静的に補った (§4.3)。修正前の述語に対してこの試験を走らせれば落ちるはずだが、生成器の修正を戻して確かめてはいない。

## 8. 実装報告の誤記の一覧

- §5.3: emit flags の表と nil の fixture の文字列の escape が落ちている (§4.2)。
- §5.2: `JSDocParameterTag` 329・`JSDocReturnTag` 126 は分岐を通った数ではない (§4.1)。
- §2.2: store の「7d-1 以前から 106 行」は 127 行。List 版の「どれも両方の呼び出し元を持つ」は 6 件で誤り。
- §4: 書き込みの「変換 35、P2 7」は「36、6」。nodecopy.go:886 の根拠の行は 313 ではなく 878。3369 は静的に証明できる。
- §1.3: `changetrackerwriter.go` が入った理由は CHA ではなく P2 の名前一致。
- §7.2 の 1: `referenceresolver.go` の理由 (§7 の 1)。

## 9. 再現の手順

```sh
ROOT=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript; TSC=$ROOT/tsc; OUT=$TSC/internal/ast/docs/_store-checker-7d2a-results/claude
SP=/private/tmp/claude-501/-Volumes-SanDisk1TB-ghq-github-com-no-yan-TypeScript/0d46e3c6-3018-4769-bf0c-f8265961414c/scratchpad
# §2: env-start.txt、sha256-start.txt、status-*-regen.txt、worktree-sha-*-regen.txt、regen-*.txt、guard-diff.txt、file-migration.diff
#     build.txt、vet-*.txt、test-storetranslate.txt、test-notag.txt、test-storechecks.txt、test-storechecks-printer-rerun.txt (-timeout 90m)
# §3・§4 の計数と壊す試験は scratchpad の複製 ($SP/copy/tsc、testdata は symlink) で:
#     verify-copy/patch_nr.py (Pointer の nameresolver の計数)、patch_test.py (nameresolver の試験の計数)、patch_printer.py (Pointer の printer の nil 分岐と panic の記録)
#     verify-copy/copy-nameresolver.txt、copy-printer.txt、missing-kinds.txt
#     verify-copy/mutate.py と run-breaks.sh → break-{locals,globals,symref,canuse}.{diff,txt}、break-restore.txt (sha256 の復元の確認)
#     verify-copy/zz_verify_isolated_test.go.txt (ParseIsolatedEntityName)、zz_verify_jsdocbranch_test.go.txt、fix-finishsynthetic.diff
# hash の照合: $SP/copy2 で変換器の binary を実行 → verify-copy/hash-run{1,2}.txt
# §5: before は git archive 2341effa51 を $SP/beforefull に展開。funcdiff-*.txt (verify-copy/funcdiff.go.txt)、inline-{before,after}.decisions
#     KPC: $SP/kpc/run-kpc.sh (binary と source の sha256 を前後で照合、sudo -v の後で load < 1.5 を最大 30 分待つ)
```
