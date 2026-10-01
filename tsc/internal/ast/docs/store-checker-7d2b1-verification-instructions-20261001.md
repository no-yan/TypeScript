# Store checker 等価 段 1 (TODO 7d-2b-1) 検証指示

作成日: 2026-10-01。対象は検証を担当するエージェント。何を作ったかは [store-checker-7d2b1-implementation-instructions-20261001.md](store-checker-7d2b1-implementation-instructions-20261001.md) (以下「実装指示書」)。設計は [7d 設計](store-checker-design-20260923.md) §2.1・§2.7 と [レビュー契約](store-checker-review-contracts-20260926.md)。

## 目的

2b-1 は不一致を直さず、分類して報告する段 (実装指示書 §0)。なので、**合否を決めるのは「checker が一致したか」ではない。** 確かめるのは次の 4 つ。

1. **比較が本当に比べている。** walker・診断の比較器・symbol の対応が、わざと入れた差を検出する。Pointer 側の walker が baseline の規則と同じである。
2. **分類が正しく、再現する。** class の鍵が機械的に決まり、同じ入力で同じ表が出る。class の中身が鍵の言うとおりである。
3. **§3 の P2 が Pointer と等価である。**
4. **ガードを守っている。** 直してはいけないものを直していない。

## 0. 規則

7d-2a の検証指示 §0 と同じ。

- 実装を直して結果を良くしない。直してよいのは、ハーネスの正しさのバグだけ。直したら報告に書く。
- コマンドは絶対パスで書く。
  - `ROOT=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`
  - `TSC=$ROOT/tsc`
  - `OUT=$TSC/internal/ast/docs/_store-checker-7d2b1-results`
- 生の出力は `$OUT` に置く。一時 binary と大きな出力は scratchpad に置く。build は `GOWORK=off`。
- 検証の最初と最後に、source の sha256 を取って不変を確かめる。
- before は **5c0e86b580**。
- 既知の失敗は `FuzzEquivalence/a708303f611b95c8` の 1 件だけ。
- **background で長いテストを走らせるときは、終了をプロセスの有無で確かめる。** 完了の通知だけを信じない。2026-09-30 に、通知が出た後もテストが走り続けていたことがある。

## 1. 再現とガード

```sh
cd $ROOT && node tools/scripts/tsc/generate.ts && (cd tools && go run ./cmd/storetranslate) && git status --short
cd $TSC && go build ./... && go vet ./internal/ast/... ./internal/store... ./internal/storeharness/...
cd $ROOT/tools && go vet ./cmd/storetranslate/... && go test -count=1 ./cmd/storetranslate/
cd $TSC && git diff --stat 5c0e86b580 -- . ':!internal/storechecker' ':!internal/storeharness' ':!internal/ast/docs'
cd $TSC && go test -count=1 -timeout 30m -v ./internal/ast/store/... ./internal/store... > $OUT/test-notag.txt 2>&1
cd $TSC && go test -tags storechecks -count=1 -timeout 30m -v ./internal/ast/store/... ./internal/store... > $OUT/test-storechecks.txt 2>&1
```

確かめること:

1. **再現**: 再生成の後で `git status` が変わらない。
2. **ガード** (実装指示書 §1)。4 行目の diff に出てよいのは、次の 2 つだけ。
   - 変換器
   - 変換器の出力の変化。変わった関数を全部挙げ、§3.2 と §4 で説明がつくかを見る。

   storechecker の中の変更は、次の 2 つだけであること。
   - `*_manual.go` の §3 の 3 関数
   - 生成物 (§4 の根の追加による)
3. **直していないこと**: 変換の規則・ほかの P2・store の API に、分類の結果を良くする変更が無い。
4. **既存のテスト**: 両方の tag で、既知の失敗だけ。

## 2. 比較の感度 (わざと壊す)

scratchpad の複製で、Store 側の出力に 1 か所だけ差を入れる。そのたびに、該当の class で検出されることを確かめる。repo には入れない。

| # | 入れる差 | 期待する検出 |
| --- | --- | --- |
| 1 | Store の `GetDiagnostics` の結果から、最後の 1 件を落とす | `diag file: count` |
| 2 | 診断の `MessageChain` の 2 段目の引数を 1 つ変える | `diag file: MessageChain…` |
| 3 | `RelatedInformation` の 1 件の `Pos` を 1 ずらす | related の field |
| 4 | suggestion の `ReportsUnnecessary` を反転する | `diag suggestion: ReportsUnnecessary` |
| 5 | walker の型の印字の flags から `NoTruncation` を外す | `walk: type` (長い型のある file で) |
| 6 | walker が、ある kind の識別子を 1 つ飛ばす | `walk: node set` |
| 7 | `GetSymbolAtLocation` の結果を、同じ名前の別の symbol に差し替える (例: 外側の scope の同名の宣言) | `symbol correspondence` か `walk: symbol` |
| 8 | adapter の `GetModeForUsageLocation` で、import type の override を無視する | 実装指示書 §8 の 1 の失敗 |
| 9 | §6.5 の fixture の 5 で、派生の class の private の lookup が基底の symbol を返すようにする | fixture の失敗 |

各差について、検出した class・件数・最初の file を記録する。**検出しない差が 1 つでもあれば不合格。**

## 3. 比較の相手の正しさ

1. **Pointer 側の walker**: 実装指示書 §8 の 4 の結果 (対象の test の数、不一致 0) を再実行して確かめる。
2. **規則の読み**: `tsbaseline/type_symbol_baseline.go` の規則 (実装指示書 §6.3 の 1〜4) と、storeharness の Store 側の walker を、1 規則ずつ並べて読む。条件の抜け・順序の違いを探す。
3. **手書きの証拠**: Store 側の walker が変換器の出力でなく手書きであることを確かめる (規則 6)。変換器の出力の識別子や comment の形が混じっていないか。
4. **手書きの述語**: 実装報告の一覧を、Pointer の元と 1 つずつ比べる。
5. **adapter の C の 3 method**: fileloader.go:1018-1085 と program.go:615 と並べて読み、分岐の順序と条件が同じであることを確かめる。

## 4. §3 の P2

1. **読み**: `checkIfExpressionRefinesParameter` と store-only の 2 つを、Pointer の次の 4 関数と並べて読む。写しの部分の文の順序・early return・`f` の状態の読み書きが同じであることを確かめる。
   - `checkIfExpressionRefinesParameter`
   - `getFlowTypeOfReferenceEx`
   - `getTypeAtFlowNode`
   - `getTypeAtFlowCondition`
2. **根拠の検算**: 2a 実装報告 §8.1 の根拠を、現在の Pointer のコードで確かめる。
   - 合成の flow は、Condition の枝で `Flags`・`Node`・`Antecedent` しか読まれない。
   - `Shared` が立たない。
3. **到達の数**: overlay で Store 側の P2 に counter を足し、corpus で呼ばれた回数と、その呼び出しを含む file の class の内訳を取る。
   - 呼ばれた file のうち `equal` の割合を、corpus 全体の割合と並べる。
   - P2 が原因と読める不一致があれば挙げる。
4. **hash**: 4 関数の hash を独立に計算し、P2 の記録と一致することを確かめる。
5. **変換器の拡張**: scratchpad の複製で、次の 2 つを確かめる。
   - (a) `getTypeAtFlowCondition` の本体を 1 行変えると、変換器が止まる。
   - (b) store-only の印を外すと、変換器が止まる。

## 5. 分類

1. **再現**: `TestCheckerCorpus` を 2 回走らせる。JSONL の (file, class) が 2 回で一致することを確かめる。並列の順序で class が変わる file があれば、全部挙げる。
2. **鍵の機械性**: 鍵の組み立てのコードを読み、次を確かめる。
   - 手で書いた例外や、名前の対応表が無い。
   - 実装指示書 §7.2 の表のとおりに決まる。
3. **中身の抜き取り**: 上位 10 class から 5 件ずつ、seed を記録して選ぶ。JSONL の詳細と、実際の両側の値を見て、class の名前どおりの差であることを確かめる。
4. **`internal id token`**: この class の全件で、数字の並び以外が一致していることを確かめる。ほかの差を含んでいないか。
5. **除外**: 除外の理由ごとの件数と、先頭の file を確かめる。Pointer の panic の除外は、その file を Pointer の checker 単独で走らせて panic を再現する。
6. **原因の読み**: 実装報告の上位 10 class の原因の読みのうち、3 つを選んで独立に読む。一致するか。

## 6. 時間

実装報告の sample と全体の時間・RSS を、同じ worker の数で 1 回再現する。load average も記録する。性能の線は無い (2c)。

## 7. レポート

`store-checker-7d2b1-verification-report-<日付>.md` に、次を書く。

1. 結論 (合格 / 不合格と理由)。
2. §1〜§6 の結果。
3. §2 の壊し方ごとの検出。
4. 分類の表の再現結果。
5. 判断を仰ぐこと。
6. 再現の手順。
