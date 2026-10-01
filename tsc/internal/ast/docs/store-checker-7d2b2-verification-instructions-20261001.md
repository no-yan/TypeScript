# Store checker 等価 段 1 の修正と strict (TODO 7d-2b-2) 検証指示

作成日: 2026-10-01。対象は検証を担当するエージェント。何を作ったかは [store-checker-7d2b2-implementation-instructions-20261001.md](store-checker-7d2b2-implementation-instructions-20261001.md) (以下「実装指示書」)。設計は [7d 設計](store-checker-design-20260923.md) §2.1・§2.7 と [レビュー契約](store-checker-review-contracts-20260926.md) §2。

## 目的

2b-2 は、2b-1 の分類で原因が分かった不一致を直し、段 1 の corpus を strict で通す段。確かめるのは次の 5 つ。

1. **直したものが Pointer と同じ意味である。** QuestionToken・OrderedNodeSet・P2 を、Pointer の元と並べて読む。
2. **新しい試験が本当に検出する。** 多相 accessor の照合・collection の試験・token の出現・`CompareSymbols` の性質・strict の corpus。わざと入れた差で、それぞれが失敗する。
3. **除外が本物の不一致を飲み込んでいない。** 除外の 4 つの理由ごとに、除外した file の理由を独立に再現する。決定的な file の差を「非決定」として除外しない。
4. **strict の合格が再現する。**
5. **ガードを守っている。**

## 0. 規則

2b-1 の検証指示 §0 と同じ。

- 実装を直して結果を良くしない。直してよいのは、ハーネスの正しさのバグだけ。直したら報告に書く。
- コマンドは絶対パスで書く。
  - `ROOT=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`
  - `TSC=$ROOT/tsc`
  - `OUT=$TSC/internal/ast/docs/_store-checker-7d2b2-results`
- 生の出力は `$OUT` に置く。一時 binary と大きな出力、repo の複製は scratchpad に置く。build は `GOWORK=off`。
- 検証の最初と最後に、source の sha256 を取って不変を確かめる (tsc と tools の追跡・未追跡の `.go`・`.ts`・`.mod`・`.sum`・`.json`)。
- before は **830b69fbf1**。
- 既知の失敗は `FuzzEquivalence/a708303f611b95c8` の 1 件だけ。
- background で長いテストを走らせるときは、終了をプロセスの有無で確かめる。
- corpus の結果を実行の間で比べるときは、JSONL の `index` (corpus の順) で対にする。`file` は一意でない。

## 1. 再現とガード

```sh
cd $ROOT && node tools/scripts/tsc/generate.ts && (cd tools && GOWORK=off go run ./cmd/storetranslate) && git status --short
cd $TSC && GOWORK=off go build ./... && GOWORK=off go vet ./internal/ast/... ./internal/store... ./internal/storeharness/...
cd $ROOT/tools && GOWORK=off go vet ./cmd/storetranslate/... && GOWORK=off go test -count=1 ./cmd/storetranslate/
cd $ROOT && git diff --stat 830b69fbf1 -- tsc tools ':!tsc/internal/ast/docs'
cd $TSC && GOWORK=off go test -count=1 -timeout 40m -v ./internal/ast/store/... ./internal/store... > $OUT/test-notag.txt 2>&1
cd $TSC && GOWORK=off go test -tags storechecks -count=1 -timeout 40m -v ./internal/ast/store/... ./internal/store... > $OUT/test-storechecks.txt 2>&1
```

確かめること:

1. **再現**: 再生成の後で `git status` が変わらない。source の sha256 も変わらない。
2. **ガード**: diff に出てよいのは実装指示書 §1 の 1 だけ。
   - 生成物の変化を全部挙げる。実装指示書 §8 の見込み (多相の `QuestionToken` が生成物から消える、`CompareSymbols` が出る) で説明がつくかを見る。
   - storeparser・storebinder の生成物が変わっていれば、変わった関数を挙げる。
3. **直していないこと**: 次に、strict を通すための変更が無い。
   - 変換の規則 (reach.go の根の表以外)
   - ほかの P2
   - Pointer の package (test file の追加を含む)
   - 除外の理由と比較の field
4. **既存のテスト**: 両方の tag で、既知の失敗だけ。

## 2. 修正の読み (実装指示書 §3)

1. **QuestionToken**: 手書きの `store.Node.QuestionToken()` を Pointer の ast.go:1094-1110 と並べて読む。
   - switch の kind の集合と、fallback の条件 (`PostfixToken()` が nil でなく、kind が `?`) が同じであること。
   - 生成器の変更が「多相の QuestionToken を出さない」だけで、`questionTokenSlot` の表と型付きの view の accessor が残っていること。
2. **OrderedNodeSet**: `Values` を collections/ordered_map.go:129-143 の `Keys` と並べて読む。ほかの method を `OrderedSet`・`OrderedMap` と、`NodeSet` を `collections.Set` と比べる。`Delete` の後の順・`Clear` の後の再利用を含む。
3. **P2**: `findResolutionCycleStartIndex` の P2 を Pointer の checker.go と並べて読む。
   - 文の順序と early return が同じ。比較だけが「両方 Node なら `SameNode`、それ以外は `==`」になっている。
   - hash を変換器を使わずに計算し、記録と一致することを確かめる (`func` から閉じ括弧までの bytes の sha256 の先頭 16 字)。
   - scratchpad の複製で Pointer の本体を 1 行変え、変換器が止まることを確かめる。
4. **同じ形の箇所**: interface どうしの `==`、interface の key の map を、Store の package (`storechecker`・`storeprinter`・`storenodebuilder`・`storeevaluator`・`storepseudochecker`・`storemodulespecifiers`・`storescanner`・`ast/store`) について go/types で独立に数える。`store.Node` が入りうる箇所が P2 のほかに無いことを確かめる。2b-1 検証で使った scan は `_store-checker-7d2b1-results/` に無いので、書き直してよい。

## 3. 試験の感度 (わざと壊す)

scratchpad の複製を `go test -c -overlay` で差し替えて、1 か所ずつ差を入れる。repo には入れない。差を入れた program には class に印を付けて、取りこぼしを数えられるようにする (2b-1 検証の `[mut]` の方式)。

| # | 入れる差 | 期待する検出 |
| --- | --- | --- |
| 1 | QuestionToken の fallback を外す (修正前に戻す) | §4.1 の照合が失敗 (QuestionToken、PropertySignature などの kind)。corpus が strict で失敗 |
| 2 | ある多相 accessor が、Pointer が値を返す kind の 1 つで nil を返す (QuestionToken 以外。例: `Name` を 1 kind で) | §4.1 の照合が失敗 |
| 3 | ある多相 accessor が、Pointer が値を返す kind の 1 つで panic する | §4.1 の照合が失敗 |
| 4 | `OrderedNodeSet.Values` を修正前の `range s.keys` に戻す | §4.2 の試験が失敗。corpus が strict で失敗 |
| 5 | P2 の比較を `==` に戻す | corpus で `circularOptionalityRemoval.ts` が store panic、strict で失敗 |
| 6 | Store の 1 つの診断の message の引数に `__@x@1` を足す | `internal id token present` で失敗 |
| 7 | 6 と同じ場所に、生の byte 0xFE の形 (`"\xFE@x@1"` を Go の文字列で) を足す | 同上 |
| 8 | Store の `CompareSymbols` が、宣言の無い同名の組に、引数の順によらず 1 を返す | `compareSymbols: 反対称性 (Store)` で失敗。該当の組が corpus に無ければ、§6 で報告し、同一性を壊す差 (`cmp(s, s)` が 1) に替える |
| 9 | Pointer が決定的な file (2b-1 で equal、Pointer を 10 回走らせても同じ) の Store の記録に差を入れる | 非決定として除外されず、strict で失敗 |
| 10 | 非決定の判定の Pointer の走らせ直しを 0 回にする | `jsxNamespaceImplicitImportJSXNamespace.tsx#/node_modules/preact/jsx-runtime/index.d.ts` が strict で失敗 (この file の Store の記録が Pointer の最初の回と違う回で) |
| 11 | Pointer 側の walker が識別子を 1 つ飛ばす | `TestPointerWalkerBaselines` が失敗 |

- 各差について、検出したテストと class・件数・最初の file を記録する。**検出しない差が 1 つでもあれば不合格。**
- 10 は Pointer の非決定に依存して確率的なので、最大 5 回試して 1 回でも失敗すれば検出とする。回数を記録する。

## 4. strict の corpus と除外

1. **再現**: `TestCheckerCorpus` (strict) を 2 回走らせる。2 回とも通り、除外の理由ごとの件数と file が同じであることを確かめる。
   - 非決定の file は回によって除外されないことがありうる。その場合は strict で失敗するので、回数と結果を記録し、§4.3 で原因を読む。
2. **報告の mode**: `STORE_CHECKER_CORPUS_REPORT=1` で 1 回走らせ、class の表が `equal` と除外だけであることを確かめる。
3. **除外の独立の再現**: ハーネスの判定を使わずに、理由ごとに確かめる。
   - **合成の import**: 除外した file ごとに、Pointer の program の `GetJSXRuntimeImportSpecifier` / `GetImportHelpersImportSpecifier` が nil でないことを確かめる。2b-1 の 8 file と同じかも見る。
   - **Pointer の非決定**: Pointer 側だけを単独で 20 回走らせ、記録が 2 種類以上になることを確かめる。Store の記録が、Pointer の記録のどれかと一致するかも見る。一致しなければ、Store の誤りが重なっている疑いとして報告する。
   - **Pointer panic**: Pointer の program と checker だけを組んで panic を再現する。
   - **root が無い**: 0 件であること。
4. **token と `CompareSymbols`**:
   - token の出現数 (0 の見込み) を確かめる。
   - `CompareSymbols` の検査した組と、fallback に届いた組の数を記録する。
   - 検査が出力の記録の後に呼ばれていることを、コードで確かめる (記録の前に呼ぶと採番の順が変わる)。

## 5. 多相 accessor の照合 (実装指示書 §4.1)

1. **列挙**: 照合した method の数と名前を、独立に reflect で数えた結果と比べる。2026-10-01 の時点で、`As` で始まるものを除いて同名の組は 50。そこから `Symbol`・`LocalSymbol`・`Locals` を除いて 47 の見込み。除いた理由が実装指示書どおりであること。
2. **ノードの対**:
   - JSDoc のノードが含まれていること。
   - 対にしたノードの数が corpus の規模と合うこと。
3. **「Pointer panic と Store 零」の表**: (method, kind) の組を抜き取り、Pointer の ast.go の該当の accessor が本当にその kind で panic することを読んで確かめる。上位 10 組。
4. **修正前の失敗**: 実装報告の「修正前に出た不一致」が QuestionToken を含むこと。§3 の 1 で同じ失敗を再現する。

## 6. `TestPointerWalkerBaselines` (実装指示書 §7)

- 本数と不一致 0 を再現する。2b-1 の広げた mode (4,671 本、不一致 2) と比べ、本数の差と `@ts-ignore` の 2 本が一致したことを確かめる。
- `hadErrors` を `.errors.txt` の有無で決めることが、tsbaseline の `hadErrorBaseline` と同じであることを読んで確かめる。
  - compiler_runner.go の `DoTypeAndSymbolBaseline` の最後の引数 (`len(c.result.Diagnostics) > 0`)。
  - error_baseline.go の `NoContent`。
  - content mapper を使う test が対象に入っていないか。
- driver の `runPointer` の既定の規則 (comment directive の前の件数) が変わっていないこと。

## 7. 時間

実装報告の corpus の時間と RSS を、同じ worker の数で 1 回再現する。load average も記録する。§4.1 の照合の時間も記録する。性能の線は無い (2c)。

## 8. レポート

`store-checker-7d2b2-verification-report-<日付>.md` に、次を書く。

1. 結論 (合格 / 不合格と理由)。
2. §1〜§7 の結果。
3. §3 の壊し方ごとの検出。
4. 除外の理由ごとの独立の再現の結果。
5. 判断を仰ぐこと。
6. 再現の手順。
