# 7d-2a 検証指摘の修正 実装指示書

作成日: 2026-09-30。対象は実装を担当するエージェント。発端は 7d-2a の検証報告 2 本、[store-checker-7d2a-verification-report-20260930-claude.md](store-checker-7d2a-verification-report-20260930-claude.md) (以下「Claude 報告」) と [store-checker-7d2a-verification-report-20260930.md](store-checker-7d2a-verification-report-20260930.md) (以下「Codex 報告」)。前提は [実装指示書](store-checker-7d2a-implementation-instructions-20260929.md) とその実装 (未コミットの working tree、before **2341effa51**)。

設計判断は本書で済ませてある。書かれていない判断が必要になったら、実装を進めずに報告する。修正は 7d-2a と同じ working tree に入れる。commit はしない (7d-2a と 1 commit にまとめるのはユーザーが決める)。

## 0. 直すものと直さないもの

| # | 指摘 | 扱い |
| --- | --- | --- |
| 1 | `ParseIsolatedEntityName` の識別子の `Text()` が panic する (両報告) | **コードを直す** (§1) |
| 2 | `jsdocPairs` が形の違いを黙って飛ばす (両報告) | **試験を直す** (§2) |
| 3 | zero `List{}` を渡すと List helper が panic する (Codex §6.2) | **comment だけ直す** (§3.1)。ユーザー決定 |
| 4 | `HasFlowNodeData` の comment は無条件には成り立たない (両報告) | **生成器の comment だけ直す** (§3.2) |
| 5 | 変換器が `var _ I = (*T)(nil)` を黙って落とす (Claude §3.1) | **comment だけ直す** (§3.3) |
| 6 | nameresolver の `JSDocParameterTag` / `JSDocReturnTag` の分岐を通る回数が 0 (両報告) | **要件から外す** (§4.1)。ユーザー決定 |
| 7 | 実装報告の誤記 | **訂正の節を足す** (§4.2) |
| 8 | storechecks の `TestPrinterCorpus` が負荷の下で 10 分を超える | **検証指示書のコマンドに timeout を足す** (§4.3) |

次のものは **やらない**。

- 変換器の R12〜R17 と、あとから足した規則のテスト (Claude §3.1)。ユーザー決定で見送る。2b で checker を corpus で動かせば直接確かめられる。
- zero List のために helper に `IsNil` の guard を足すこと。§3.1 の理由による。
- Codex 報告 §5.2 の「emit flags の fixture は感度が無い」。これは誤読。printer_test.go:303 は `with: "\"あ\""`、`out: "\"あ\""` で、違う出力を期待している。実装報告 §5.3 の表で escape が落ちていたのを読んだもの。
- KPC の再測定。本書の変更は Walk / Parse / Bind の経路にある関数に触れない (§5)。
- 2b / 2c に回すもの (§4.2 の表に書き足すだけ)。`NextContainer` の root、`VisitSliceList` の余分な確保、`isUniqueLocalName` の O(C²)、刈り残しの 56 関数 (`reach.go:356` の `methodsNamed` が名前だけで照合するため)。

## 1. `ParseIsolatedEntityName` (P1)

**症状**: nil でない結果の識別子で `Text()` を呼ぶと、必ず `slice bounds out of range [:-1]` で panic する。`Builder.FinishSynthetic` (store/checker_api.go:510) が全ノードの end を -1 にするが、`Node.identifierText` (store/store.go:266) は textInTexts が立っていない識別子の text を `src[data:end]` で読むため。storechecker の jsx.go:1331・1355・1376・1405・1411 (`jsxFactory`・`jsxFragmentFactory` と `@jsx`・`@jsxFrag` の pragma) がこの経路を通るので、2b で JSX の file を checker にかけると panic する。

**直し方**: `FinishSynthetic` の中で、位置を消す前に、textInTexts が立っていない識別子の text を `texts` に移す。Claude 報告 §4.4 の案をそのまま使う。

```go
func (b *Builder) FinishSynthetic(root NodeRef) Node {
	s := b.Finish()
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
	s.sealed = true
	return s.node(root)
}
```

決めてあること:

1. **直す場所は `FinishSynthetic` だけ。** parser の `identifierData` (パースの hot path) には触れない。`FinishSynthetic` の呼び出し元は `ParseIsolatedEntityName` だけで、入力は entity name 1 つなので、`s.texts +=` の再確保は問題にならない。
2. **escaped の識別子** (`a` など) は、parse の時点で `identifierData` が textInTexts を立てているので、この分岐を通らず今のまま読める。
3. **識別子以外の text** (`textWords` の [offset, length]) は end を使わないので、移さなくてよい。
4. **関数の doc comment** に、識別子の text を texts に移すことを 1 文足す。

**試験**: `storeparser` に `TestParseIsolatedEntityName` を足す。雛形は `_store-checker-7d2a-results/claude/verify-copy/zz_verify_isolated_test.go.txt` (`TestVerifyParseIsolatedEntityName`)。名前から Verify を除き、空の `TestVerifyEmitContextNoRegionPanics` は持ち込まない。

- 入力は雛形の 18 個をそのまま使う (検証指示 §3.3 の 6 個 + 12 個。`a` を含む)。
- nil でない結果について、次を確かめる。
  - kind と識別子の text の木が、Pointer の `parser.ParseIsolatedEntityName` と一致すること。
  - 全ノードの位置が (-1, -1) であること。Pointer の公開関数は位置を残すので、位置は Pointer と比べない (Pointer では checker/jsx.go の wrapper の markAsSynthetic が消す。Store は R16 でこれを parser 側に寄せた)。
  - `File()` が nil であること。
  - 別の Factory からの `SetRange` が拒否されること。
- nil になる入力は、Pointer と同じく nil であること。
- 雛形の `t.Logf` は、失敗したときの `t.Errorf` の message に畳んでよい。
- 修正の前にこの試験が落ちること (panic すること) を一度確かめてから、修正を入れる。

## 2. `jsdocPairs` の黙った skip

`storebinder/nameresolver_test.go:321` の `jsdocPairs` は、host の JSDoc の数か、JSDoc の preorder の長さが両側で違うと `continue` で飛ばす。今の corpus で飛ばしている数は 0 だが (両報告で確認済み)、将来の不一致を隠す。

**直し方**: 戻り値を `([]storetest.Pair, bool)` にし、どこかで形が違ったら `false` を返す (飛ばした host の分は出力に入れない)。呼び出し元は `false` を不一致として数える。

- `resolveFile` (nameresolver_test.go:364): `st.mismatch("jsdoc shape", where)` を呼ぶ。数えたうえで、それ以外の pair の比較は続ける。
- `TestHasDataCorpus` (hasdata_test.go:44): `t.Errorf("%s #%d: JSDoc shape differs", f.name, i)` を出す。
- doc comment に「形が違う host は false で知らせる」を足す。

## 3. comment だけ直すもの

### 3.1 List helper の nil 契約 (store/node_helpers.go:124-128)

今の comment「treats the nil List as the nil slice」は、zero `List{}` (Store が nil) については誤り。`List.Len` は所有者の `extra` を読むため。ただし動作のバグではない。

- 生成された accessor は、member が無いと `List{n.s, NoListRef}` (所有者付き、block 0) を返す。これは `Len()` が 0 で読める。
- zero `List{}` が出てくるのは、明示的な `store.List{}` と `var l store.List` だけ。これは Pointer の nil `*ast.NodeList` に当たり、Pointer も `nil.Nodes` の時点で panic する。
- helper の呼び出しのうち、変数を渡す 16 箇所 (flow.go:1159・1184、checker_manual.go:181・197、relater.go:1970、grammarchecks.go:1231、checker.go:6633・6646・17172・17184・22688、storeprinter/printer.go:1306・1309・1379、storeprinter/utilities.go:434・435) は、どれも accessor から来ているか、`IsNil` で守られている。

comment を次の趣旨に書き換える (英語、既存の文体で):

> A List that an accessor returns for an absent member belongs to its Store (block 0) and reads as empty, as the Pointer's nil slice from the accessor does. The zero List{} is the Pointer's nil *ast.NodeList: like nil.Nodes, it cannot be read, and these helpers panic on it.

guard は足さない。直接 `.Len()` を呼ぶ約 240 箇所と契約が食い違い、Pointer より寛容な挙動だけが増えるため。

### 3.2 `HasFlowNodeData` の comment (tools/scripts/tsc/generate-go-store.ts:1254)

生成される comment「Has<X>Data is (*ast.Node).<X>() != nil.」は、kind だけで決まる述語について無条件に言っている。Pointer は具象の data 型で決まるので、`printer.NodeFactory.NewToken` で作った `Null`・`True`・`False`・`This`・`Super`・`Import` の 6 kind は、Pointer では nil、Store では真になる (Codex 報告 §7.1)。

- 生成器の `writeBaseTests` の comment を、「By kind: (*ast.Node).<X>() != nil for the nodes the parser and binder make. A node the Pointer makes with NewToken for a kind that is also a KeywordExpression has no data there, but true here.」の趣旨に変える。述語ごとに 1 行でよい。`FlowNodeData` 以外の 8 述語はこの食い違いが無いが、comment は共通の文でよい。
- 生成器の `basesOf` の上の comment (1205-1209 行) にも同じ限界を 1 文足す。
- 今の呼び出し元は storechecker/flow_manual.go:13 と checker_manual.go:432 の 2 つ。どちらも program の file のノードしか受け取らないことを確かめ、報告に書く。確かめられなければ、進めずに報告する。
- 再生成して、差分が comment だけであることを確かめる。

### 3.3 変換器の `var _` (tools/cmd/storetranslate/world.go:145)

`_` を飛ばす箇所に、意図して落としていることと理由を comment で書く。

- 理由: interface の assertion は到達計算の根にしていない。Store 側の型は到達した method しか持たないので、出すとコンパイルが通らない。例えば `storechecker.EmitResolver` は `storeprinter.EmitResolver` の 37 method のうち 6 つしか持たない (overlay で確かめた。`missing method CreateLateBoundIndexSignatures`)。
- emit を範囲に入れるときは、これを根にする。
- 対象の 5 件 (checker/emitresolver.go:17、checker/tracer.go:79、printer/textwriter.go:12、printer/singlelinestringwriter.go:19、binder/referenceresolver.go:29) は comment に列挙しない。数えれば分かるので。

## 4. 文書

### 4.1 JSDoc の 2 分岐を要件から外す

検証指示書 [store-checker-7d2a-verification-instructions-20260929.md](store-checker-7d2a-verification-instructions-20260929.md) §3.1 の 3 の「`KindJSDocParameterTag`・`KindJSDocReturnTag` になる呼び出しがそれぞれ 1 回以上」を外す。代わりに、Go 版では到達しない理由を注記する (2026-09-30 ユーザー決定)。

- この分岐に入るには、lastLocation が function-like な location の直接の子で、かつその location の locals で名前が見つかる必要がある。
- JSDocParameterTag / ReturnTag を直接の子に持つ function-like な kind は `JSDocSignature` だけ (`Parameters` と `Type`)。
- binder の `GetContainerFlags` (binder/binder.go:2580) は `JSDocSignature` に `ContainerFlagsNone` を返す。なので `b.container` にも `b.blockScopeContainer` にもならず、`GetLocals` の対象にならない。locals は常に nil で、`result` が立たない。
- corpus で 0 回 (両報告)。Pointer の nameresolver.go:313-316 の comment も、この経路を no-op としている。
- Store 側の 2 項は Pointer からの機械的な変換のまま残す。

`KindJSDoc` の分岐 (7,752 回) の要件は残す。

### 4.2 実装報告の訂正

[実装報告](store-checker-7d2a-implementation-report-20260930.md) の末尾に「## 9. 検証後の訂正 (2026-09-30)」を足す。本文は書き換えない。次を 1 行ずつ書く。

- §5.3: emit flags の期待の出力は `"あ"` / `"あ"`、nil の fixture は `a` / `a` (escape が落ちていた)。
- §5.2: `JSDocParameterTag` 329・`JSDocReturnTag` 126 は、親の鎖にその tag がある識別子の数で、分岐を通った数ではない。分岐は Go 版では到達しない (§4.1)。
- §2.2: store の「7d-1 以前から 106 行」は 127 行。List 版の「どれも list と slice の両方の呼び出し元を持つ」は 11 中 6 で誤り (`checkTypeParameters`・`reportUnusedVariableDeclarations`・`appendTypeParameters`・`getInstantiatedConstructorsForTypeArguments` は呼び出し元が 0、`checkTypeParametersNotReferenced`・`getConstructorsForTypeArguments` は死んだ関数からしか呼ばれない)。
- §4: 書き込みは「変換 35、P2 7」ではなく「36、6」。nodecopy.go:886 の根拠の行は 313 ではなく 878。nodebuilderimpl.go:3369 は静的に証明できる (ImportType の出どころが `b.f.NewImportTypeNode` だけ)。
- §1.3: `changetrackerwriter.go` が入ったのは CHA ではなく、P2 の `p.writer.HasTrailingComment()` (printer_manual.go:55) の名前の一致による。
- §7.2 の 1: `referenceresolver.go` が要るのは storechecker の型の宣言を全部出すため (`EmitResolver` の field の型)。`getReferenceResolver` と `GetReferenced…` の 7 method は CHA だけで到達し、呼び出し元は無い。
- §7.2 の 2: `storemodule.GetResolutionDiagnostic` の返り値は `*diagnostics.Message`。診断を含むのは引数の `ResolvedModule` の側。

§8.3「そのほか」に、2b / 2c に回すもの (§0 の最後の項目) を足す。出典は Claude 報告 §3.2・§3.3。

### 4.3 検証指示書のコマンド

検証指示書の `go test` の 2 行 (24・25 行) に `-timeout 30m` を足す。

## 5. 確かめること

1. `node tools/scripts/tsc/generate.ts` と `(cd tools && go run ./cmd/storetranslate)` の後、生成物の差分が §3.2 の comment だけであること。
2. `go build ./...`、store 系の `go vet` が通ること。
3. tag なしと `-tags storechecks` の両方で、`go test -count=1 -timeout 30m ./internal/ast/store/... ./internal/store...` を走らせる。失敗は既知の `FuzzEquivalence/a708303f611b95c8` だけであること。§1 と §2 の新しい試験が通ること。
4. `(cd tools && go test -count=1 ./cmd/storetranslate/)` が通ること。
5. 変更した関数が Walk / Parse / Bind の経路に無いこと。`git diff` で、store / storeparser / storebinder の production の変更が `FinishSynthetic` だけであることを確かめる。これが崩れたら、KPC を依頼する前に報告する。

一時ファイル・binary・`-m=2` の出力は scratchpad に置く。`GOWORK=off` で実行する。

## 6. 報告

`store-checker-7d2a-fix-report-20260930.md` に次を書く。

- §5 の結果と、コマンドの出力の要点。
- §1 の試験が修正前に落ちたことの記録。
- §3.2 の呼び出し元の確認の結果。
- 本書に無い判断をしたなら、その一覧。
