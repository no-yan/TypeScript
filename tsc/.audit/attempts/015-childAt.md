# Attempt 015: 生成 accessor を childAt（生列読み）へ

作成日: 2026-09-03  
作業: `/Volumes/SanDisk1TB/worktree/store-pr-7-attach-parent-fix` `exp/handle-kind-field`  
設計: `tsc/.audit/store-accessor-design.md` 提案 1

## 結論

**keep.** kind-specific getter を `h.Child(slot)` から `h.childAt(slot)` に切り替えた。同一プロセスのマイクロベンチで getter は Child 経由より **min −27% / med −29%**。公開 API は不変。generator を更新済み（291 getter）。

## 仮説

`Child` の `mustLive`・`childLen` 検査・常時 `ExternalChild` map 参照が kind 既知の accessor では冗長。列の直接読み + external は id==0 の cold path に倒せば hot path が短くなり、薄い wrapper が caller へ inline される。

## 変更

```go
func (h Handle) childAt(rel uint32) Handle {
	s := h.s
	id := s.children[s.nodes[h.id].childStart+rel]
	if id == 0 {
		return h.childAtSlow(rel)
	}
	return Handle{s: s, id: id, Kind: s.nodes[id].kind}
}

func (h Handle) childAtSlow(rel uint32) Handle { /* externalChild nil guard + map */ }

func (h Handle) Child(i int) Handle {
	h.mustLive()
	// bounds check…
	return h.childAt(uint32(i))
}
```

生成形:

```go
func (h Handle) IfStatementExpression() Handle { return h.childAt(slotIfStatementExpression) }
```

`generate-go-ast.ts` の `emitStoreAccessor` を上記に変更。`Handle.Kind` フィールド化に合わせ generator の `Kind()` も `.Kind` に直した（再生成で壊さないため）。

## 手動プローブの経緯

| 形 | AccIfExpr vs 変更前 Child ベース | inline |
|---|---|---|
| `return h.childAt(slot)` だが childAt が handleOf+external 一体（cost 188） | min −11%（ノイズ大） | accessor は inline、childAt は不可 |
| cold split（上の最終形、childAt cost 98） | min −15% / med −20% | accessor inline（cost 61） |
| accessor 本体に列読みを展開 | **悪化** +5〜11% | accessor 自体が cost 98 で非 inline |

展開形は却下。薄い `return h.childAt(slot)` + cold split を採用。

## 凍結ハーネス（最終 A/B）

`BenchmarkAccIfExpr`（childAt getter）対 `BenchmarkAccIfExprChild`（`Child(slot)`、mustLive+bounds+childAt）。64K IfStatement、固定乱数順、同一プロセス。

| | min ns | med ns |
|---|---|---|
| AccIfExpr (childAt) | 319366 | 344325 |
| AccIfExprChild (Child) | 437002 | 487236 |
| 比 | **−26.9%** | **−29.3%** |

再現: `tsc/.audit/scripts/acc_if_expr_bench_test.go.txt` を `internal/ast/` にコピーして

```bash
go test ./internal/ast -run '^$' -bench 'BenchmarkAccIfExpr' -benchmem -count 10 -benchtime 300ms
```

## Bind

絶対 ns はマシン負荷で 20〜46 ms と振れる。同時刻の pointer AST 比はおおよそ 1.6x のまま（port ~20 ms / base ~12 ms）。提案 1 の成否判定は上記マイクロ A/B を正とし、Bind は非悪化の確認に留めた。

## ゲート

`go test -count=1 ./internal/ast ./internal/parser ./internal/binder` 緑。

## 次

提案 2（kind→slot テーブル）と 3（hot field を slot 0 へ）。`childAt` を inline budget 80 未満に削れればさらなる伸び余地あり（現状 cost 98）。
