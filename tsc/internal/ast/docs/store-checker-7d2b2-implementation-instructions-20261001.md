# Store checker 等価 段 1 の修正と strict (TODO 7d-2b-2) 実装指示書

作成日: 2026-10-01。対象は実装を担当するエージェント。

## 前提の文書

| 呼び名 | 文書 |
| --- | --- |
| 7d 設計 | [store-checker-design-20260923.md](store-checker-design-20260923.md) (H1・Q1 は §2.1・§2.7) |
| 残りの設計 | [store-checker-7d2-design-remainder-20260929.md](store-checker-7d2-design-remainder-20260929.md) (§4 の 2b の完了条件) |
| レビュー契約 | [store-checker-review-contracts-20260926.md](store-checker-review-contracts-20260926.md) (§2) |
| 2b-1 指示書 | [store-checker-7d2b1-implementation-instructions-20261001.md](store-checker-7d2b1-implementation-instructions-20261001.md) |
| 2b-1 実装報告 | [store-checker-7d2b1-implementation-report-20261001.md](store-checker-7d2b1-implementation-report-20261001.md) (§3 の分類と原因) |
| 2b-1 検証報告 | [store-checker-7d2b1-verification-report-20261001.md](store-checker-7d2b1-verification-report-20261001.md) (§7 の欠陥、§8 の決定) |
| 検証指示書 | [store-checker-7d2b2-verification-instructions-20261001.md](store-checker-7d2b2-verification-instructions-20261001.md) |

- before は **830b69fbf1** (7d-2b-1)。
- 設計判断は上の文書と本書で済ませてある。書かれていない判断が必要になったら、実装を進めずに報告する。
- commit はしない。

## 0. 作るものの性格

2b-1 は不一致を分類して報告した。2b-2 は、その分類で原因が分かったものを直し、**段 1 の corpus を strict で通す。**

| 段 | 入力 | 指示書 |
| --- | --- | --- |
| **段 1 (本書)** | 2a と同じ corpus (17,415 file)、1 file 1 program、`AllowJs`・`CheckJs` | 2b-1 で分類、**2b-2 で strict** |
| 段 2 | testrunner の compiler / conformance の全構成 | 次の指示書 (2b-3) |

段 2 を本書に入れないのは、2b-1 と同じ理由。初めての入力を広げるのと修正を同じ段で行うと、不一致の原因を切り分けられない (2026-10-01、ユーザー)。

2b-1 の分類で、不一致 749 file の原因は次のとおり読めている。

| 原因 | file | 本書 |
| --- | ---: | --- |
| `store.Node.QuestionToken()` に `PostfixToken()` への fallback が無い | 507 + 両方 49 | §3.1 |
| `OrderedNodeSet.Values()` が反復中の追加を回らない | 184 + 両方 49 | §3.2 |
| `findResolutionCycleStartIndex` の interface の `==` に `store.Node` (panic) | 1 | §3.3 |
| 合成の import (pragma による。段 2 の範囲) | 8 | §5.2 で除外 |
| Pointer 自身の非決定 (`findInMap` の map の反復) | 1 | §5.2 で除外 |

2b-1 の実装は overlay で最初の 2 つを差し替え、17,404 / 17,414 が equal になることを確かめている。本書の修正の後に残る不一致は 0 の見込み。

**見込みと違う不一致が出たら、決めていない直し方で直さない。** 分類して報告し、strict は満たさないまま止める (§9 の 4)。

作らないもの:

- 段 2 の testrunner への接続。合成の import specifier の Store のノード。
- ID の token の照合 (レビュー契約 §2 の 3)。出現を数えるだけ (§5.4)。
- 採番順の trace の補助テスト (同 §2 の 4 の後半)。順序の差が出たときに設計する (§6)。
- 性能の計測 (2c)。

## 1. ガード

1. **変更してよいもの**
   - `tsc/internal/ast/store/` の手書きの file: §3.1 の `QuestionToken`、§3.2 の `OrderedNodeSet`。
   - `tools/scripts/tsc/generate-go-store.ts`: §3.1 の、生成しない多相 accessor の追加だけ。
   - `tsc/internal/storechecker/checker_manual.go`: §3.3 の P2 だけ。
   - `tools/cmd/storetranslate/reach.go`: §6 の根の追加だけ。
   - 2 つの生成器の出力 (再生成したもの)。
   - `tsc/internal/storeharness/` (§4〜§7)。
   - `tsc/internal/ast/store/` の test file (§4.2)。
   - `tsc/internal/ast/docs/`。
2. **変更しないもの**
   - Pointer の package (`ast` (store 以外)・`checker`・`printer`・`binder`・`parser`・`compiler`・`collections`・`testrunner`・`testutil` など)。test file を足すことも含む。
   - 変換の規則 (`tools/cmd/storetranslate/` の reach.go の根の表以外)。ほかの P2。
   - `storeparser`・`storebinder` の production のコード。生成物が変わる場合を除く。変わったら報告に書く。
   - `tsc/go.mod`、`tools/go.mod`。
   - production (store 系でも storeharness でもない package) から、store 系の package と `storeharness` を import しない。
3. 既存のテストは、除外と緩めを増やさずに通す。既知の失敗は `FuzzEquivalence/a708303f611b95c8` の 1 件だけ。
4. 生成物は `node tools/scripts/tsc/generate.ts` と `(cd tools && go run ./cmd/storetranslate)` で再現すること。
5. `unsafe` を増やさない。テストの `reflect` は使ってよい。
6. 一時ファイル・binary・大きな出力は scratchpad に置く。build は `GOWORK=off`。

## 2. ファイル

```
tools/scripts/tsc/generate-go-store.ts   §3.1 生成しない多相 accessor に QuestionToken
tsc/internal/ast/store/checker_api.go    §3.1 QuestionToken (手書き)、§3.2 OrderedNodeSet.Values
tsc/internal/ast/store/*_test.go         §4.2 collection の写しの試験
tsc/internal/storechecker/checker_manual.go  §3.3 P2
tools/cmd/storetranslate/reach.go        §6 根 checker.Checker.CompareSymbols
tsc/internal/storeharness/
  accessor_test.go                       §4.1 多相 accessor の照合
  driver.go・compare.go・corpus_test.go  §5 strict、除外、token、JSONL
  symbolorder.go (名前は任意)            §6 CompareSymbols の性質
  baseline_test.go                       §7
```

## 3. store の API と P2 の修正

### 3.1 `QuestionToken` (2026-10-01、ユーザー: store に手書き)

**Pointer の `(*Node).QuestionToken()` (ast.go:1094-1110) と同じ形で手書きする。**

- Parameter・ConditionalExpression・MappedType・NamedTupleMember は自分の member を返す。
- それ以外は、`PostfixToken()` が `?` (KindQuestionToken) ならそれを返し、そうでなければ nil。

置き場は `checker_api.go` の、Pointer の手書きの多相 accessor を写した `Attributes`・`CanHaveStatements` などの並び。

生成器は、多相の `func (n Node) QuestionToken() Node` を出さない。

- 今の `Text` の扱い (generate-go-store.ts の「Text is hand-written in store.go」で role accessor の出力を飛ばす) と同じ仕組みで、飛ばす名前に `QuestionToken` を足す。
- 理由を comment に書く: Pointer の accessor が member に無いノード (postfix の token) を返し、スキーマから導けないため。
- `questionTokenSlot` の表は残す。Builder の setter と、手書きの accessor が使う。
- 型付きの view の `QuestionToken()` は今のまま生成する。

理由: Pointer の accessor が持つ fallback はスキーマから導けない知識なので、生成器に表を足すのではなく、手書きで Pointer と同じ形に写す。

見落とした原因も報告に書く。生成されたテストの `compareRoles` は、Store に slot のある kind でしか Pointer の同名の method と比べない。slot の無い kind で Pointer が fallback を返す場合は、比較の外だった。§4.1 の試験がこの穴を塞ぐ。

### 3.2 `OrderedNodeSet.Values`

- `Values()` を、Pointer の `OrderedMap.Keys()` (collections/ordered_map.go:129-143) と同じく index で回す。反復中に足された要素も数える。
- ほかの method (`Add`・`Has`・`Delete`・`Clear`・`Size`)、`NodeSet` の全 method も、`collections.Set` / `collections.OrderedSet` と意味が同じであることを §4.2 で確かめる。違えば同じ形に直し、報告に列挙する。

### 3.3 `findResolutionCycleStartIndex` の P2 (2026-10-01、ユーザー: P2)

- Pointer は checker.go:19115、hash は `7215403e5af99e9c` (2026-10-01 の値。書くときに `-hash` で取り直す)。
- `TypeSystemEntity` は `any`。`pushTypeResolution(declaration, TypeSystemPropertyNameInitializerIsUndefined)` が `store.Node` を入れる。`resolution.target == target` は、両方が `store.Node` のとき実行時に panic する (`store.Node` は `_ [0]func()` で比較できない)。
- P2 では、この比較を次の意味に置き換える。
  - 両方が `store.Node` なら `store.SameNode`。
  - それ以外は `==`。dynamic type が違えば false で、panic しない。
- 文の順序と early return は Pointer のまま。補助の関数を別の宣言にしない (P2 の file の宣言は全部 hash を要る)。
- go/types で数えた範囲では、`store.Node` が入りうる interface どうしの `==` はこの 1 か所だけ (2b-1 検証、2026-10-01)。ほかの候補は次のとおりで、どれも panic しない。
  - relater の `chainArgsMatch`: 文字列と比べるだけ。
  - literal の値: string・number・bool・PseudoBigInt。
  - generic の comparable。
  - factory の `SyntheticExpression.Type`: `*Type`。

  同じ形の箇所を見つけたら、直さずに報告する。

## 4. API の照合の試験 (新設)

### 4.1 多相 accessor の照合 (`storeharness/accessor_test.go`、strict)

`*ast.Node` と `store.Node` に同じ名前で「引数なし・戻り値 1 つ」の method を reflect で列挙し、corpus の全ノードの対で両方を呼んで比べる。生成器の知識を使わないので、生成器と同じ盲点を持たない。

- **対象の method**: 2026-10-01 の時点で、`As` で始まるもの (型付きの view への変換) を除いて 50。
  - `Symbol`・`LocalSymbol`・`Locals` も除く。bind の結果で、7c の bind 等価が見ている。
  - 残りは 47 の見込み。列挙した数と名前を log する。
  - 片側にしか無い method (Pointer の `BodyData` など 14) も log する。
- **ノードの対**: `storetest.Pairs` で対にする。JSDoc は host ごとに対にする (`mode_test.go` の `specifierPairs` と同じやり方)。corpus は storeharness の `corpusUnder` を使う。
- **比べ方** (結果の型ごと):

  | 結果の型 | 比べ方 |
  | --- | --- |
  | ノード | Pointer の結果を対の表で写したものと、Store の結果が `SameNode`。nil どうしは一致 |
  | list (`*NodeList`・`[]*Node`・`*ModifierList` と `List`・`[]Node`) | 長さと各要素 (同じ表で写す)。`*NodeList` と `*ModifierList` は nil かどうかも比べる |
  | string・bool・int・`ModifierFlags` | 値 (int と int32 は数値) |

- **panic の扱い** (Pointer の panic は recover する):

  | Pointer | Store | 結果 |
  | --- | --- | --- |
  | panic | nil・零 | 一致扱い。(method, kind) ごとに数えて log する (生成の既定値) |
  | panic | nil・零以外 | 不一致 |
  | 値 | panic | 不一致 |
  | 値 | 違う値 | 不一致 |

- **失敗**: 不一致が 1 件でもあれば失敗。(method, kind) ごとに件数と最初の file・位置を出す。
- **修正前の確認**: §3.1 の修正の前にこの試験を書いて走らせ、QuestionToken の不一致が出ることを確かめる。結果を報告に書く。
- **ほかの不一致が出たとき**:
  - 「Pointer が値を返し、Store が違う」なら、§3.1 と同じ形 (Pointer の accessor の写しを手書き) で直してよい。直したものを報告に列挙する。
  - それ以外の形の不一致は、直さずに止めて報告する。
- `-short` では走らせない。時間を報告に書く。

### 4.2 collection の写しの試験 (`ast/store` の test、`store_test` package)

`NodeSet` と `collections.Set[*ast.Node]`、`OrderedNodeSet` と `collections.OrderedSet[*ast.Node]` に、同じ操作の列を当てて結果を比べる。

- ノードは小さな source を両方で parse して、`storetest.Pairs` で対にする。
- 操作: `Add`・`Has`・`Delete`・`Clear`・`Size` / `Len`・`AddIfAbsent`、反復 (`Values` / `Keys`)。
- **反復の途中で `Add` と `Delete` を呼ぶ列を必ず含める。**
- 列は固定の seed で作る。seed と列の数を test に書く。
- 比べるもの:
  - 各操作の返り値。
  - 反復で出た要素の列 (対で写す)。`Set` の `Keys` は順が決まらないので、集合で比べる。
- §3.2 の修正の前に走らせ、今の `Values` で失敗することを確かめる。

## 5. strict と除外 (ハーネス)

### 5.1 strict

- `TestCheckerCorpus` は既定で strict にする。除外 (§5.2) 以外の file が `equal` でなければ失敗。
- `STORE_CHECKER_CORPUS_REPORT=1` のときは 2b-1 と同じ報告の mode (分類して log、失敗にしない)。原因の調査に使う。
- class の表と例の log は、両方の mode で出す。

### 5.2 除外

理由は 4 つで、理由ごとに件数と先頭の file を log する。

| 理由 | 条件 | 2b-1 での数 |
| --- | --- | ---: |
| root が Pointer の program に無い | 今と同じ | 0 |
| Pointer が panic する | program の作成か checker の実行で panic (今と同じ) | 1 |
| 合成の import (段 2) | Store 側で `syntheticImportPanic` が出た。今の class を除外に移す | 8 |
| Pointer が非決定 | 下の手順 | 1 |

**Pointer の非決定の判定**:

1. `equal` でない file についてだけ、Pointer 側 (program の作成から walker まで) を最大 10 回走らせ直す。
2. どれか 1 回の記録が最初の回の記録と違えば、Pointer の非決定として除外する。比べ方は corpus と同じ比較器 (`compareRuns`) を Pointer どうしに使う。
3. 10 回とも同じなら、元の class のまま (strict では失敗)。
4. 除外した file は、Pointer の 2 つの記録の最初の違いと、Store の記録の最初の違いを JSONL の detail に残す。

非決定の Pointer と本当の Store の誤りが重なった file は、この除外で見えなくなる。件数を log し、検証で中身を見る。

### 5.3 ハーネスの修正 (2b-1 検証報告 §7 の 1〜3、2026-10-01、ユーザー: 2b-2 で直す)

1. **ID の token** (2026-10-01、ユーザー: 出たら設計する)
   - 数字を消して比べる仕組み (`withoutIDs`、`diagDiff` の `ids`、`internal id token` の class) をやめる。生の文字列は完全一致で比べる (レビュー契約 §2 の 5)。
   - 代わりに、両側の全記録で token の出現を数える (§5.4)。
2. **鍵の field**: 1 で `ids` の比較がなくなるので、鍵は常に最初に違った field になる。追加の修正は要らない。
3. **JSONL の key**: 各行に corpus の index (`index`) を足す。`file` は一意でない (同じ `@filename` を 2 回持つ test が 5 つある)。

### 5.4 ID の token の出現

- 数える場所: 両側の全記録の文字列 (診断の message・引数・連鎖・related、walk の型と symbol の文字列)。equal の file も含む。
- 形:
  - `#<数字>@`
  - `__@<名前>@<数字>`
  - 内部の prefix の生の byte の形。Go の regexp では生の byte 0xFE は U+FFFD として読まれるので、`\x{FFFD}@<名前>@<数字>` で当てる (2b-1 検証報告 §7 の 1)。
- 正規表現は見つけるためだけに使う。消して比べることには使わない。
- 出現が 1 つでもあれば、その file を class `internal id token present` として strict で失敗にする。両側の文字列が一致していても失敗。
- 全体の出現数を log する。reference の baseline の全体と 2b-1 の corpus では 0。0 でなければ、照合の設計に戻る (§6 と同じく止めて報告)。
- 正規表現の単体の試験を足す。生の 0xFE の形・`__` の形・private の形が当たること、名前だけ違う文字列に当たらないこと。

## 6. `CompareSymbols` の性質 (レビュー契約 §2 の 4)

Pointer は export の `(*Checker).CompareSymbols` を持つ (checker/exports.go:394、hash `1c693425b85f21ae`)。Store に無いので、変換器の根 (`checkerRoots`) に `checker.Checker.CompareSymbols` を足して再生成する。

各側で、出力を全部記録した **後に** 次を検査する。`CompareSymbols` は ID の fallback で採番を起こしうるので、記録の前には呼ばない。

| 性質 | 対象 | 条件 |
| --- | --- | --- |
| 同一性 | symbol walk で観測した全 symbol | `cmp(s, s) == 0` |
| 反対称性 | walk の順で隣り合う、違う symbol の組 | `sign(cmp(a, b)) == -sign(cmp(b, a))` |
| 反対称性 | 宣言が無く名前が同じ symbol の組の全部 (ID の fallback に届く組) | 同上 |
| 安定性 | 上の組の全部 | 同じ組を 2 回比べて同じ結果 |

- 違反があれば、その file を class `compareSymbols: <性質> (<側>)` として strict で失敗にする。
- 検査した組の数と、fallback に届いた組の数を log する。
- 両側の符号が一致することは比べない (契約 §2 の 4: I1 の lazy gid では保証しない)。
- 公開の出力 (walk・診断) の順序は、既存の比較が完全一致で見ている。順序に原因がある不一致が出たら、採番順の trace を設計してから進む。直さずに止めて報告する。

## 7. `TestPointerWalkerBaselines` (2b-1 実装報告 §10 の 1)

- **既定の条件を、2b-1 の広げた mode にする。** option を harnessutil と同じく既定の後に適用し、構成が 1 つで harness の option を持たない test。今の 14 本の条件はその部分集合なので、別の mode は残さない。`STORE_CHECKER_BASELINE_OPTIONS` は消す。
- **この検定の中だけ、`hadErrors` を「reference に `<名前>.errors.txt` があるか」で決める。**
  - tsbaseline の `hadErrorBaseline` は、testrunner の `len(c.result.Diagnostics) > 0` (compiler_runner.go:527)。
  - `.errors.txt` は診断が 1 件以上のときだけ書かれる (error_baseline.go の `NoContent`)。両者は、content mapper を使う test を除いて同じ。
  - driver の `runPointer` に「`hadErrors` を外から与える」口を足す。corpus の driver の規則 (H1: comment directive の前の件数) は変えない。
- 期待は 4,671 本前後で不一致 0 (2b-1 の `@ts-ignore` の 2 本が一致する)。

## 8. 再生成とテスト

- 2 つの生成器を回し、`git status` と生成物が再現することを確かめる。
- 生成物の変化を全部報告する。見込みは次のとおり。
  - `views_generated.go` から多相の `QuestionToken` が消える。
  - `storechecker` に `CompareSymbols` が出る。
  - ほかは変わらない。
- 既存のテストを両方の tag で回す: `./internal/ast/store/... ./internal/store...` と `(cd tools && go test ./cmd/storetranslate/)`。

## 9. 完了条件

1. 生成物が再現する。build と、store 系・storeharness の `go vet` が通る。
2. 既存のテストが両方の tag で通る。失敗は既知の a708 の 1 件だけ。
3. §4.1 の多相 accessor の照合が不一致 0。§4.2 の collection の試験が通る。どちらも、修正の前に失敗することを確かめてある。
4. `TestCheckerCorpus` が strict で通る。
   - 除外は §5.2 の 4 つの理由だけで、件数を log する。見込みは合成の import 8・非決定 1・Pointer panic 1・root 無し 0。
   - ID の token の出現 0。`CompareSymbols` の性質の違反 0。
   - 見込みと違う不一致が残ったら、この条件は満たさないまま止めて報告する (§0)。
5. `TestModeCorpus`・`TestPrivateFixtures` が今のまま通る。`TestPointerWalkerBaselines` が §7 の条件で不一致 0。
6. 変換器のテストが通る。

## 10. 報告

`store-checker-7d2b2-implementation-report-<日付>.md` に次を書く。

1. 結論と、完了条件ごとの結果。
2. §3 の修正ごとの diff の要約と、Pointer の元の位置。
3. §4.1 の結果。
   - 列挙した method の数と名前、対にしたノードの数。
   - 「Pointer panic と Store 零」の (method, kind) の表。
   - 修正前に出た不一致。
   - 時間。
4. §4.2 の結果。seed、列の数、修正前の失敗。
5. corpus の結果。
   - class の表 (strict の前の報告の mode で 1 回)。
   - 除外の理由ごとの件数と file。
   - token の出現数。
   - `CompareSymbols` の検査した組と fallback の組の数。
   - 時間と RSS (worker 4)。
6. §7 の結果。本数と不一致。
7. 生成物の変化の一覧。
8. 本書に無い判断の一覧。

## 11. 本書で決めたこと (設計文書に無いもの)

1. **2b-2 の範囲は段 1 の strict まで** (2026-10-01、ユーザー)。段 2 は 2b-3。
2. **QuestionToken は store に手書き** (2026-10-01、ユーザー)。生成器は多相の QuestionToken を出さない。スキーマから導けない Pointer の fallback を、生成器の表でなく参照実装と同じ形で持つ。
3. **`findResolutionCycleStartIndex` は P2** (2026-10-01、ユーザー)。型から導けない知識 (`any` に Node が入る) なので、変換規則にしない。
4. **ID の token は出たら設計する** (2026-10-01、ユーザー)。数字を消して比べる仕組みをやめ、出現を数えて 0 を確かめる。理由は 2 つ: reference の baseline の全体と 2b-1 の corpus で 0 件だったこと、契約 §2 の 5 の「正規化で隠さない」と合うこと。
5. **ハーネスの欠陥 3 件は 2b-2 で直す** (2026-10-01、ユーザー)。2 は 4 で不要になる。
6. **多相 accessor の照合は reflect で両側の method を列挙する**。生成された `compareRoles` は slot のある kind しか比べず、QuestionToken を見逃した。生成器の知識を使わない比較で塞ぐ。
7. **Pointer の非決定は、不一致の file だけ Pointer を走らせ直して機械的に判定する**。名指しの表を持たない。
8. **`CompareSymbols` の性質は、出力の記録の後に各側で検査する**。両側の符号は比べない。採番順の trace は、順序に原因がある不一致が出たときに設計する。
9. **`TestPointerWalkerBaselines` は広げた条件を既定にし、`hadErrors` を `.errors.txt` の有無で決める** (2b-1 実装報告 §10 の 1 への回答)。
