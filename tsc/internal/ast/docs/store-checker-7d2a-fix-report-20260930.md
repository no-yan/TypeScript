# 7d-2a 検証指摘の修正 実装報告

作成日: 2026-09-30。[修正指示書](store-checker-7d2a-fix-instructions-20260930.md) の実装の報告。修正は 7d-2a と同じ working tree に入れ、commit はしていない。

## 0. 結論

- 指示書の §1〜§4 を全部入れた。§5 の確認はすべて通った。ただし 5 は、指示書の手順 (`git diff`) では確かめられなかったので、別の方法で確かめた (§1 の 5)。
- `FinishSynthetic` の修正の前に、`TestParseIsolatedEntityName` が `slice bounds out of range [:-1]` で panic することを確かめた (§2)。
- `HasFlowNodeData` の 2 つの呼び出し元は、program の file のノードの **ほかに**、checker が自分で作るノードも受け取る。ただし、それらは食い違いの起きない作り方をしているので、結論は変わらない (§3)。
- 本書に無い判断は 4 件 (§4)。

## 1. §5 の結果

1. **再生成**: `node tools/scripts/tsc/generate.ts` と `(cd tools && go run ./cmd/storetranslate)` の前に `tsc/internal` の `*.go` を scratchpad に写し、後で `diff -r` で比べた。違ったのは `ast/store/visit_generated.go` の `Has<X>Data` 9 つの comment だけ (1 つにつき 1 行が 4 行になった)。storetranslate の出力 package は byte 一致。`git status --short` の行数も前後で同じ (64)。
2. **build / vet**: `go build ./...` は通った。検証指示書 §1 の `go vet` (ast・store 系 10 package) は出力なしで通った。`(cd tools && go vet ./cmd/storetranslate/...)` も通った。
3. **試験** (`go test -count=1 -timeout 30m -v ./internal/ast/store/... ./internal/store...`):

   | | tag なし | `-tags storechecks` |
   | --- | --- | --- |
   | ast/store | ok 5.6s | ok 6.9s |
   | storebinder | ok 112.6s | ok 59.7s |
   | storechecker | ok 1.5s | ok 0.6s |
   | storeparser | FAIL (`FuzzEquivalence/a708303f611b95c8` だけ) | 同じ |
   | storeprinter | ok 160.3s | ok 108.8s |

   - `TestParseIsolatedEntityName` は両方で PASS。
   - `TestNameResolverCorpus` は両方で PASS (17,415 file、Resolve 10,235,856 回)。新しい `jsdoc shape` の不一致は 0 件。
   - `TestHasDataCorpus` は両方で PASS (17,415 file、2,638,926 node)。`JSDoc shape differs` は出ていない。
   - tag なしの実行は、途中で試験の重複した実行が 2 本重なって、負荷の下で走った (§5)。`TestPrinterCorpus` は 157.8s で、30 分の timeout には届いていない。
4. **storetranslate**: `(cd tools && go test -count=1 ./cmd/storetranslate/)` は ok (23.9s)。
5. **Walk / Parse / Bind の経路**: `checker_api.go` は 7d-2a で足した未追跡の file なので、`git diff` では 7d-2a の変更と本修正を分けられない。そこで、このセッションで編集した file を数え上げて確かめた。store / storeparser / storebinder の production で変えたのは次の 3 つだけ。
   - `ast/store/checker_api.go` の `FinishSynthetic`: 本体と doc comment。呼び出し元は `storeparser.ParseIsolatedEntityName` だけ (grep で確かめた)。
   - `ast/store/node_helpers.go`: comment だけ。
   - `ast/store/visit_generated.go`: 生成された comment だけ。

   どれも Walk / Parse / Bind の経路にないので、KPC は依頼しない。

## 2. §1 の試験は修正の前に落ちた

`storeparser/isolated_test.go` の `TestParseIsolatedEntityName` を先に足して、修正の前に走らせた。

```
--- FAIL: TestParseIsolatedEntityName (0.00s)
panic: runtime error: slice bounds out of range [:-1] [recovered, repanicked]
github.com/microsoft/TypeScript/tsc/internal/ast/store.Node.identifierText(...)
	.../internal/ast/store/store.go:266
github.com/microsoft/TypeScript/tsc/internal/ast/store.Node.Text(...)
	.../internal/ast/store/store.go:282
```

修正の後は PASS。入力は雛形の 18 個 (`\u0061` を含む)。Pointer と比べるのは kind と識別子の text の木だけで、次も確かめる。

- 全ノードの位置が (-1, -1) であること。
- `File()` が nil であること。
- 別の Factory から `SetRange` すると拒否されること。

## 3. §3.2 の呼び出し元の確認

`HasFlowNodeData` の呼び出し元は 2 つ。

- `getFlowNodeOfNode` (flow_manual.go:13)。呼ばれる場所は次の 4 つ。
  - flow.go:83 (`getFlowTypeOfReferenceEx` の reference)
  - checker.go:13145・13189 (`location`)
  - checker.go:17070・17095 (`parentAccess`)
- `checkThisBeforeSuper` (checker_manual.go:432)。呼ばれる場所は checker.go:7339 (`checkSuperExpression`) と 11458 (`checkThisExpression`)。

受け取るノードは、program の file のノード (source と遅延 JSDoc の Store) だけではない。**checker が c.factory で作るノードも受け取る。**

- `getSyntheticElementAccess` が作る ElementAccessExpression。`parentAccess` を経由して再帰する。
- `isPropertyInitializedIn*`・`getFlowTypeIn*` が作る PropertyAccess / ElementAccess。これらの `this` は `NewKeywordExpression(ast.KindThisKeyword)` で作られ、reference として `getFlowTypeOfReferenceEx` に渡る。

ただし、食い違いが起きるのは、Pointer が **`NewToken`** で作ったノードが、6 つの kind のどれかであるときだけ。checker の中を調べると、次の結果だった。

- 6 つの kind のノードは、Pointer でも Store でも `NewKeywordExpression` で作っている。KeywordExpression なので、Pointer でも `FlowNodeData() != nil`。
- 変数の kind で `NewToken` を呼ぶのは、nodebuilderimpl.go の `ReadonlyToken().Kind()` と `QuestionToken().Kind()` の 2 箇所だけ。kind は readonly / + / - / ? で、6 つに入らない。しかも型のノードで、flow の経路に来ない。
- Pointer の checker を grep しても、同じ 2 箇所しか無い (checker/nodebuilderimpl.go:1535・1539)。

よって、今の 2 つの呼び出し元では Pointer と Store の答えは一致する。指示書は前提を「program の file のノードしか受け取らない」としていたが、これは成り立たなかった。ただし、確かめたかった「食い違いに当たらない」は成り立つので、止めずに進めた (§4 の 1)。

## 4. 本書に無い判断

1. **§3.2 の前提の読み替え**: §3 のとおり、受け取るのは「program の file のノードだけ」ではない。確かめる対象を「6 つの kind のノードを Pointer の `NewToken` で作る経路が無いこと」に読み替えて、確かめた。生成される comment の「for the nodes the parser and binder make」は、checker の `NewKeywordExpression` のノードについても正しいので、そのままにした。
2. **`jsdocPairs` の飛ばし方**: 元のコードは、preorder の長さが違うときは JSDoc 1 つだけを飛ばしていた。指示書の「飛ばした host の分は出力に入れない」に合わせ、host の JSDoc を全部落とすようにした。
3. **`TestParseIsolatedEntityName` の対象**: 識別子の text を比べるとき、`PrivateIdentifier` も含めた。雛形は `Identifier` だけだった。今の入力で PrivateIdentifier が出るものは無く (`a.#b` は両側とも nil)、将来の入力のための変更。
4. **実装報告 §9 の §5.3 の行**: 指示書の文面 (「`"あ"` / `"あ"`」「`a` / `a`」) にも escape の欠落があった。そこで printer_test.go:253・254・303 から実際の値を取って書いた。
   - emit flags: 設定あり `"あ"` / 設定なし `"\u3042"`
   - nil の fixture: file あり `\u0061` / nil `a`

## 5. 気付いたこと

- **`\uXXXX` の escape は、ツールを通すと文字に置き換わる。** heredoc で `\u3042` と書いた行が、file では「あ」になった。実装報告 §5.3 と本指示書の escape の欠落も、同じ原因の見込みが高い。この報告の中の escape は、python の `chr(92)` で書いたあと byte を確かめた。
- **background の試験が重複した。** 最初に background で走らせた試験について「完了、出力なし」という通知が来たが、実際は走り続けていた。これを見て 2 回走らせ直したため、一時は 3 本が同時に走った。重複した 2 本は止め、最初の 1 本の結果を使っている (§1 の 3)。
