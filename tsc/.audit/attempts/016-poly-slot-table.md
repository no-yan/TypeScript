# Attempt 016: 多相 accessor を kind→slot テーブルへ

作成日: 2026-09-03  
作業: `/Volumes/SanDisk1TB/worktree/store-pr-7-attach-parent-fix` `exp/handle-kind-field`  
設計: `tsc/.audit/store-accessor-design.md` 提案 2

## 結論

**revert.** 同一プロセス A/B（`BenchmarkAccPolyExpression` テーブル vs `BenchmarkAccPolyExpressionSwitch` 旧 switch）で med **−0.9%**、benchstat **p=0.076（有意差なし）**。E2E も改善せず。提案 2 は取り込まない。

## 仮説

`Expression()` 等の巨大 `switch h.Kind` を `[KindCount]uint8` の slot テーブル + `childAt` に置き換えると、比較木が消え、kind-specific getter 経由の二重呼び出しも消えて速くなる。

## 変更（一時）

`generate-go-ast.ts` の `generateStorePolymorphic` をテーブル生成に変更し、`store_polymorphic_generated.go` を再生成。getter は:

```go
slot := polyExpressionChildSlot[h.Kind]
if slot == 0xff { return Handle{} }
return h.childAt(uint32(slot))
```

## 計測

ハーネス: `internal/ast/acc_poly_expr_bench_test.go`（64K・8 kind 混在・固定乱数順）。

| 比較 | 結果 |
|---|---|
| before/after 別プロセス | AccPolyExpression **+31%**（ノイズ。KindSpecific も +34% で汚染） |
| 同一プロセス table vs switch（count=11） | table med 399674 ns / switch 403282 ns → **−0.89%**, p=0.076 |

ゲート: `go test ./internal/ast` PASS（テーブル実装時）。

## 判定

設計の「E2E で有意差が出ない段は取り込まない」と hillclimb の「ノイズを超えたときだけ keep」に従い **全 revert**（generator + generated）。ハーネスと本メモは残す。

## 次

提案 3（hot field を slot 0 に寄せる）の方がテーブル無しで効く可能性がある。提案 2 を再試すなら、まず `childAtSlow` 除去や nil 子経路の短縮後。
