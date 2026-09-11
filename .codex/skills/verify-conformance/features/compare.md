# Regression comparison

以前のcommitで存在した失敗を認識した上で、新しい退行と改善を区別する。

## Sub-features

- pass→fail、fail→pass、skip変化、追加/消失ケース。
- fail→failでもactual/referenceや失敗内容が変われば要確認。
- 入力・参照baseline・ハーネス・Go環境が違えば比較不能。

## How to get to it (user POV)

2つのsnapshotを選び、`conformance.py compare --old ... --new ... --out ...`を実行する。

## Driving it with conformance.py

最初に同じprepared commitのA/Aを比較する。次に意図した旧commitと新commitを比較する。comparison.jsonのverdictと全change keyを確認し、content_changedのcapturesから両snapshotの`contents/<actual>.txt`をdiffする。rawログが変わっただけか、生成結果が変わったかを区別する。比較後も両snapshotの内容が変わっていないことを確認する。

## Gotchas

新規退行なしは全テスト成功を意味しない。追加/消失や期待値変更を自動的に改善としない。失敗件数の差し引きだけで判定しない。同じ失敗の内容変化は必ずしも悪化ではなく、レビュー対象。
