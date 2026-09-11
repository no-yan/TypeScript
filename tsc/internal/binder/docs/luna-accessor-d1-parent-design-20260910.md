# D1：親情報の受渡し範囲（設計待ち）

カード種別: 設計のみ。Binder全体の親引数化は未実装。実装はレビュー後の別カード。

## 背景

[parent-argument実験](parent-argument-experiment-20260910.md)は、generated walkerが既に持つ親refを識別子判定へ渡す対照だった。
再取得対照比でchecker命令 −1.14%。baseline比は非有意、domは +0.51%の有意悪化。採用なし。
全祖先stackは別実験で維持費が勝ち、候補外。

旧`isIdentifierNameRef`のParentRef 123,553回は参考頻度であり、全訪問へ引数を増やす根拠にしない。

## 取得済み親を持てるcaller（現状）

| caller | 既知の親情報 | 保持期間 | 消費先候補 |
|---|---|---|---|
| `forEachBindChildGenerated` / list helpers | parentKind（現状）、parent refは生成時に分かる | 1 child/list要素のbind呼び出し | Identifierのcontextual判定のみ |
| `bindN` / `bindKind` | parentKind引数 | 呼び出し中 | `checkContextualIdentifier*`、`isIdentifierName*` |
| OptionalChain helpers（P9済） | parent ref/kindを局所で一度 | helper内 | outermost判定 |
| declare経路 | container Symbolは別問題（D2側） | — | 親構文ではない |

## 変更シグネチャ見込み

| 案 | 変更関数数（概算） | escape/frame見込み |
|---|---:|---|
| A. Identifier判定だけに親refを追加（実験と同型） | walker生成 + 識別子helper数個 | parent-argument実験でinline境界変化を観測済み。frame増の可能性 |
| B. 全`bindKind`に親refを必須化 | 広範 | 引数圧・spill増大リスク。採用しない |
| C. Binder fieldに現在親を保持 | 少数 | 再入・遅延bindで不正参照のリスク。採用しない |
| D. 祖先stack | 多数 | 実験で不採用済み |

## 限定patch案（レビュー対象）

1. 生成walkerのIdentifier caseだけ、`bindKindWithParent(id, kind, parentRef, parentKind)`へ分岐。
2. `checkContextualIdentifierRef` / `isIdentifierNameRef`に Resolved 版を追加。`parentRef==0`は従来の`ParentRef`へfallback。
3. 診断・Flags・訪問順は共有core。複製しない。
4. 20入力意味監査 + checker/dom KPCを同一セッションでA/A付き測定してから本体採用を判断。
5. 全訪問の親引数化、ancestor stack、Flags memoは含めない。

## 状態

**設計待ち**。P9までの実装カードとは独立。次の実装は上記限定patchのレビュー合意後。
