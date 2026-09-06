# `flows` / `symbolIdx` を `NewStore` で事前確保する案の計測 (2026-09-06)

結論: **採用しない (係数 0 = 現状の `PrepareBindTables` での exact-size 確保を維持)**。
確保を parse 側へ寄せても CPU は減らず (parse +4〜19%、bind ±0)、live heap が +10〜14% 増える。
原因は hint の精度で、`len(sourceText)/5` から出す cap は実 node 数の約 3 倍あり、
bind 列を hint で確保すると exact-size の 3.06 倍のバイト数を parse 中に確保・memclr・GC scan することになる。

## 実験の変遷

1 回目は end-to-end の `tsc --noCheck` を hyperfine で回したが、`ensureCol` の reslice 経路で
`make` が既にゼロ化した領域を `clear` で二度目に memclr していた (指摘: 事前確保後は append/reslice で十分)。
この二重 memclr が事前確保側だけに乗っていたので 1 回目の数値は破棄した。
また 8GB マシンでは large (9788 files, heap 2GB 級) が paging に支配され、順序効果が最大 15% 出た。

2 回目は `ensureCol` を修正 (cap が足りれば reslice のみ、`truncateCol` が切った尾を `clear`) し、
end-to-end の代わりに Go benchmark で sequential に parse → bind を回して ns/op ≈ CPU として測った。
medium/large セットは per-file の効果が small と同質で、このマシンでは memory pressure のノイズしか足さないので落とした。

## ベンチ

`tsc/internal/binder/bind_column_presize_bench_test.go` `BenchmarkParseBindColumnPresize`。
`ast.BindColumnHintPct` (実験用 knob、production は 0) を 0 / 80 / 100 で切り替え、
`NewStore` で `symbolIdx` と `flows` を `cap = (hint+1) × pct/100 + 1` で確保する。
各 op はファイルセット全体を parse し、続けて bind する。op の前に前回の木を捨てて `runtime.GC()` (計測外)。
`live-MB` は parse+bind 後に GC を 2 回かけた HeapAlloc の増分。

```
cd tsc && TSGO_BENCH_FILELIST="small=/path/to/files-small.txt" \
  go test -run '^$' -bench ParseBindColumnPresize -benchtime 3x -count 8 ./internal/binder/ | benchstat -col /presize
```

M1 8core/8GB、Go 1.26.6、hint = `len/5` (worktree の未コミット変更)。benchstat、n=8。

| set | pct | sec/op | parse-ms | bind-ms | live-MB |
|---|---:|---:|---:|---:|---:|
| checker.ts (302.6k nodes) | 0 | 53.2m ±9% | 35.2 | 17.7 | 33.0 |
| | 80 | 51.3m (~) | 33.8 | 17.2 | 35.2 (+6.7%) |
| | 100 | 49.9m (~) | 32.3 | 17.2 | 36.6 (+10.8%) |
| dom.generated.d.ts (113.5k nodes) | 0 | 21.6m ±10% | 15.5 | 6.0 | 22.6 |
| | 80 | 23.6m (+9.1%, p=0.04) | 17.0 | 6.3 | 25.5 (+12.7%) |
| | 100 | 23.8m (+10.2%, p=0.01) | 17.2 | 6.3 | 26.5 (+17.2%) |
| small: `extensions/typescript-language-features` (377 files, 402.8k nodes) | 0 | 93.3m ±5% | 64.2 | 28.2 | 70.6 |
| | 80 | 97.2m (~, p=0.11) | 69.0 (+7.5%) | 28.3 | 77.4 (+9.6%) |
| | 100 | 106.0m (+13.6%, p=0.002) | 76.2 (+18.7%) | 30.3 | 80.2 (+13.6%) |

checker.ts の 100 は −6% に見えるが p=0.44 でノイズ内。
small を単独プロセスで回し直す (`-bench '.../small/presize=0$'`, n=4) と分散が消え、
0: 82.0 ms/op (parse 53.7 / bind 27.9)、100: 84.5 ms/op (parse 56.0 / bind 28.0) で +3%。
同一プロセスで 9 sub-benchmark を続けて回した上の表は heap 状態の持ち越しで小さいセットほど散る。
どちらの取り方でも bind は変わらず parse が遅くなる方向で一致する。

## pprof での確認 (small、presize=0 vs 100、`-memprofilerate 65536`)

alloc_space (4 count × 3 op + live pass の累計):

| 確保箇所 | pct=0 | pct=100 |
|---|---:|---:|
| `PrepareBindTables` → `ensureCol` symbolIdx (exact n) | 26.9 MB | 0 |
| `PrepareBindTables` → `ensureCol` flows (exact n) | 50.6 MB | 0 |
| `NewStore` symbolIdx (cap by hint) | 0 | 80.2 MB |
| `NewStore` flows (cap by hint) | 0 | 156.6 MB |
| 合計 | 77.4 MB | 236.8 MB (3.06 倍) |

CPU profile では mutator 側の `memclrNoHeapPointers` / `mallocgc` は 0.1s 前後でどちらも差が読めない。
差は `gcDrain` (3.46s → 4.58s) に出る。`[]*FlowNode` は pointer-bearing なので、hint で確保した 3 倍の
cap がゼロのまま parse 中ずっと mark 対象に乗り、heap も大きくなって GC が早く走る。
つまり事前確保の損は「確保コストの移動」ではなく「over-reserve 分の memclr + GC scan の純増」。

## 副産物: hint と実 node 数の比 (`len(nodes) / (hint+1)`, `Freeze` 時点、1 回目の計測から)

| workload | Store 数 | 合計 nodes/hint | p50 | p90 | p99 | max |
|---|---:|---:|---:|---:|---:|---:|
| small | 282 | 0.357 | 0.500 | 0.769 | 0.905 | 0.91 |
| medium | 1437 | 0.590 | 0.641 | 0.822 | 1.008 | 1.25 |
| large | 8070 | 0.592 | 0.606 | 0.774 | 0.947 | 2.07 |

`len/5` では `nodes` 自体が平均 1.7〜2.8 倍 over-reserve されている。
hint の精度が上がれば bind 列の事前確保も損益が変わりうるが、それは hint 係数の課題であって
bind 列の課題ではない。exact-size で確保できる `PrepareBindTables` を hint 依存にする理由はない。

## 残した変更 / 取り下げる変更

- `ensureCol`: cap が足りれば reslice のみ (二重 memclr を除去)。`putCol` もこれを使う。
- `truncateCol`: 切った尾を `clear` して「len より先は常にゼロ」の不変条件を保つ (`Restore` はテストのみが使う)。
- `NewStore` の `BindColumnHintPct` 分岐とベンチは実験の記録用。マージ前に消してよい。

## Bind 側で列コストを下げたい場合の代替

- `flows` を `*FlowNode` から FlowNode arena の `uint32` index に変える (8 B → 4 B、noscan 化)。→ 同日に実装・計測して採用 (`flow-index-column-bench.md`)。
- `symbolIdx` / flow index を `nodeHeader` に同居させる (header 24 B → 32 B とのトレードオフ)。
