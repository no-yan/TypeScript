# Luna accessor V3：実装済み / 残存 / 設計待ち

旧累積候補: `.cursor/skills/verify-tsc/artifacts/20260910-luna-accessor-p9-candidate`
意味監査: 20入力 equal（P0保存baseline比）。wall/KPCまとめ・parse+bind・GC live/scanは **missing**。
pointer同等・全面採用の結論は出さない。

## 15分類対応

| # | 分類 | 状態 | 残存・注記 |
|---:|---|---|---|
| 1 | Binary Flow | 実装済み（P3） | bindKind前段とbindChildren後段でAccessが2回（意図的・境界跨ぎ共有なし） |
| 2 | skipParentheses | 実装済み（P2） | paren childはChildRef(0) |
| 3 | Call非optional | 実装済み（P2） | optional後段は従来取得 |
| 4 | Parameter/BindingElement | 実装済み（G1） | 監査ビルドParameter frame 96→144B。通常ビルド112B |
| 5 | 宣言名 | 実装済み（P6a/b/c） | 文字列memoなし。binding pattern子は再取得あり |
| 6 | Variable/単項 | 実装済み（P4a） | |
| 7 | Conditional/loop/try | 実装済み（P4b） | |
| 8 | Switch/Case | 実装済み（P5） | |
| 9 | list descriptor | 実装済み（P7a） | `BindListSpan` local-only |
| 10 | list consumer | 実装済み（P7b） | foreignはListLen/ListElem fallback。ListSlotAt→qualified ListRef往復は未着手 |
| 11 | narrowing Handle | 実装済み（P8） | foreign境界試験あり |
| 12 | 親情報受渡し | **設計待ち（D1）** | [設計メモ](luna-accessor-d1-parent-design-20260910.md)。全訪問引数化はしない |
| 13 | modifier/root構文 | 実装済み（P9＋P7） | modifierFlags結果cacheなし。rootは同一宣言内で一回 |
| 14 | header位置/Flags | **設計待ち（D2）** | [設計メモ](luna-accessor-d2-header-design-20260910.md)。契約なしpointer禁止 |
| 15 | OptionalChain構文 | 実装済み（P9） | expression/questionDot共有。Flagsは判定時にread |

## 生成API

- `Access*` / `*Accessor`: `store_accessors_generated.go`
- 旧`SyntaxChildrenN` / If/PropertyAccess arity API: 定義とASTテストは互換残存。Binder callerなし

## 性能ステータス

| 項目 | 状態 |
|---|---|
| 20入力意味digest | 各カード＋V1/V2/V3累積で一致 |
| G1 Parameter frame | 監査144B、通常112B。種別を区別する |
| V1/V2 wall/KPC | missing（計画どおり局所カードでは中断せず記録） |
| parse+bind全体 | missing |
| GC CPU/assist/scan/live | missing |

## 合意済みとして言えること

構文再解決削減のための名前付きAccess・宣言名Resolved・list Span・narrowing局所Handle・Optional/root構文渡しは、現行20入力で意味を崩さず本体に入った。
全体wall採用条件・pointer同等は未証明。D1/D2は設計待ちのまま追跡する。

## 2026-09-11 追記

レビュー修正と通常ビルドの結果は[修正記録](accessor-review-fixes-20260911.md)を参照。V3は意味検証済み／性能評価未完了。旧P9 artifactは修正後sourceと異なり、新実装の証拠へ流用しない。
