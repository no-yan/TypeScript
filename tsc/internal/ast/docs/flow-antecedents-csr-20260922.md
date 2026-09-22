# FlowList (label の antecedents) の配列化: 操作の棚卸しと CSR 案

作成日: 2026-09-22。議論の記録のみ。コードは変更しない。

Pointer AST (`tsc/internal/ast`) の `FlowNode` が label の antecedents を単連結リスト `FlowList` で持つ理由と、それを配列 (CSR) に置き換えられるかを整理した。結論は「単独では性能に出ないので今は触らない。FlowNode の packed 化 (noscan) に着手する時に、packed 連結リストではなく CSR を第一候補にする」。7c の設計 ([store-binder-design-20260922.md](store-binder-design-20260922.md) §2.1) は `FlowList{Flow, Next uint32}` 8B の index 連結 slab を採っており、この文書はその後継の選択肢を書いたもの。

## 1. 現行の構造と操作

```go
// ast/flow.go:27-37
type FlowNode struct {
	Flags       FlowFlags
	Node        *Node
	Antecedent  *FlowNode // label 以外 (入次数 1)
	Antecedents *FlowList // BranchLabel / LoopLabel
}
type FlowList struct {
	Flow *FlowNode
	Next *FlowList
}
```

`FlowList` を触るのは binder/binder.go と checker/flow.go の 2 file だけ。他 package には出てこない。

### 1.1 binder (書き込み側)

| 操作 | 場所 | 内容 |
| --- | --- | --- |
| 末尾追加 + 重複検査 | binder.go:547-565 `addAntecedent` | 先頭から `Next` で線形走査し、同一 `*FlowNode` があれば何もしない。無ければ末尾に新セル。追加時のみ `setFlowNodeReferenced` (Referenced → Shared) |
| cons | binder.go:520 `newFlowList` | arena 確保して `Next = tail` |
| 複製結合 | binder.go:527-532 `combineFlowLists` | `head` の全セルを新規複製し末尾に `tail` を繋ぐ。呼び出しは try-finally の 1 箇所 (binder.go:2065) |
| 単一要素判定 | binder.go:567-574 `finishFlowLabel` | `Antecedents == nil` なら unreachable、`Next == nil` なら label を捨てて `Antecedents.Flow` を直接返す |

### 1.2 checker (読み取り側、書き込み無し)

| 操作 | 場所 |
| --- | --- |
| `Next == nil` なら label をスキップして `.Flow` へ | flow.go:164 (BranchLabel)、flow.go:170 (LoopLabel) |
| 全要素を順に走査 | flow.go:1258 `getTypeAtFlowBranchLabel` (途中 return あり)、flow.go:1357 `getTypeAtFlowLoopLabel`、flow.go:2556 `isReachableFlowNodeWorker` (いずれかで true)、flow.go:2633 `isPostSuperFlowNodeWorker` (すべてで true) |
| 先頭だけ読む | flow.go:2567、2641 (LoopLabel の入口経路) |

`getBranchLabelAntecedents` (flow.go:208) は ReduceLabel が有効なら `FlowReduceLabelData.Antecedents` を、そうでなければ `flow.Antecedents` を返す。リストの差し替えはしない。

### 1.3 まとめ

- 走査パターンは「先頭から全部」「先頭だけ」「長さ 1 か」の 3 つ。ランダムアクセス・中間挿入・削除・逆順走査は無い。
- 順序に意味がある。LoopLabel は先頭が「ループに入る非ループ経路」、2 番目以降が back edge (flow.go:1359-1366、2566 が前提)。BranchLabel は追加順。
- 重複検査は同一ポインタ比較のみ。

## 2. copy は障害ではない

`combineFlowLists` が複製するのは、元の 3 リスト (normalExit / exception / return label の antecedents) が `finallyLabel` 作成後も `ReduceLabel` のデータとして参照され続けるから (binder.go:2075、2080、2086)。共有すると `finallyLabel` への追加が元リストを汚す。

配列なら 3 範囲を新しい領域に連続コピーするだけで、セル単位の再帰複製より安い。try-finally 1 つにつき 1 回なので量も問題にならない。

さらに、平坦化せず `finallyLabel` の antecedent に 3 つの label をそのまま繋いでも意味は変わらない。BranchLabel は checker では純粋な join (型の union、到達可能性の OR、post-super の AND) なので、join の join を潰す辺縮約は結果を変えない。平坦化は checker が label を 1 段余分に辿る (union + shared-flow cache 1 回) のを避けるためと見られるが、try-finally 1 つにつき 1 hop で無視できる。3 label 構成にすると `FlowReduceLabelData` は「finallyLabel を通るときは returnLabel だけを辿る」となり、リスト参照が label id 1 つに置き換わる。

## 3. 本当の制約: open な label への append が交互に来る

while 文 (binder.go:1860-1869) を例にすると:

1. `preWhileLabel` (loop) と `postWhileLabel` (branch) を先に作る
2. body を bind する間、`break` は `postWhileLabel` へ、`continue` は `preWhileLabel` へ、call / mutation は `currentExceptionTarget` へ、`return` は `currentReturnTarget` へと、複数の label に交互に `addAntecedent` が走る
3. body が終わってから back edge を `preWhileLabel` に追加し (1868)、確定する

同時に open な label は入れ子深さ × 3 個ほど。単一の連続 arena に `(off, len)` で置いて in-place append する形は成り立たない。`preWhileLabel` は list 未確定のまま `currentFlow` として body 内の flow node から参照されるので、id は先に固定し、antecedents の payload だけ後で書く必要がある。

これは「連結リストだから」ではなく「隣接配列を増分構築する」こと自体の制約で、グラフ表現に変えても同じ問題が残る。

## 4. CSR とは

`[[1], [2,5,8], [3,8], [], [0,3]]` のような slice of slices は隣接リスト。CSR (Compressed Sparse Row) はそれを 2 本の平坦な配列に潰したもの。

```
indices = [1, 2,5,8, 3,8, 0,3]   // 全ノードの辺を node 順に連結
offsets = [0, 1, 4, 6, 6, 8]      // len(nodes)+1 個、prefix sum
node i の辺 = indices[offsets[i] : offsets[i+1]]
```

隣接リストとの違いは、内側 slice ごとの header 24B と個別確保が無く、全体が連続メモリ 1 本 (uint32 なら辺 1 本 4B) で、ポインタを含まないので GC が中を辿らないこと。

構築は二段階になる。(1) 辺を `(from, to)` の対として来た順に貯める、(2) `from` ごとの件数から `offsets` を prefix sum で作る、(3) 辺を `offsets[from]` の位置へ置く (counting sort、同じ from の中では追加順が保たれる)。

## 5. FlowNode 用の設計案

flow node の大半は predecessor が 1 本なので、全 node 分の `offsets` は持たず、label のレコード側に `(off, len)` を書く変種にする。

```
flowRec  {Flags, node, link}   // 12B noscan
                               //   非 label: link = antecedent
                               //   label:    link = off、len は Flags の空きビット (現状 14 bit 程度しか使っていない)
edges    []uint32              // 件数 2 以上の label の antecedents だけを連結
```

- **bind 中**: `addAntecedent` は辺配列 `[]struct{label, antecedent uint32}` に append するだけ。label の close 点を追跡しない。
- **file の bind 終了時**: label id で安定な counting sort をして `edges` にする。O(E)。LoopLabel の「先頭が入口辺」という順序不変条件は安定 sort で保たれる。
- **label レコード**: 先頭 antecedent と件数を持たせる。`finishFlowLabel` の nil / 単一判定、checker の `len == 1` スキップ、LoopLabel の先頭だけ参照が `edges` に触れず O(1) で済む。件数 2 以上の label だけが `edges` を使う。
- **ReduceLabel**: リストではなく label id を持ち、問い合わせ時にその label の範囲へ解決する。§2 の 3 label 構成なら単一の label id で済む。
- **try-finally**: 3 label は `finallyLabel` 作成時点 (2064) で close 済み。平坦化するなら 3 範囲を連続コピー、3 label 構成なら辺 3 本を追加するだけ。

checker の 3 操作 (§1.3) はすべて改善する。`len == 1` 判定と先頭参照は O(1)、走査は連続メモリ、`getBranchLabelAntecedents` は `(off, len)` を返すだけになる。

### 5.1 重複除去と Shared flag

今の `addAntecedent` は既存リストを線形走査して同一 antecedent を捨て、その結果として `setFlowNodeReferenced` の Referenced / Shared の立ち方が決まる。sort 段で隣接重複を落とせば辺集合は同じになるが、重複追加のときに Shared が余分に立つ。Shared は checker の cache 対象 (`sharedFlows`) を増やすだけで正しさには効かない。許容するか、scratch で label ごとの直近辺を見るかの選択。重複が実際どれだけ起きるかは数えていない。

### 5.2 注意

- `isReachableFlowNodeWorker` の `flow.Antecedents == nil` (flow.go:2563) は `len == 0` に読み替える。
- 厳密には基本ブロックの CFG ではなく、narrowing に関係する事象 (代入、呼び出し、条件、switch 節) をノードにした predecessor 専用のグラフ。表現の議論には影響しないが、判断は「CFG の一般論」ではなく checker が何を辿るかで行う。

## 6. 効果の見積もりと判断

| 計測 | FlowList | 比較対象 |
| --- | ---: | --- |
| Monaco 1729 file parse+bind 後 (2026-09-08、Pointer) | 2.8 MB | 走査対象 heap 107.7 MB の 2.6%、flow 構造 30.4 MB の 9% |
| checker.ts (2026-09-22、7c 設計 §2.1) | 15,402 セル | 到達可能 FlowNode 45,694 |
| dom.generated.d.ts | 0 | |

単独の変更としては、GC mark の削減も Check 時間の削減も測定に出ない。checker の flow 解析のうち label の antecedents を辿る部分はごく一部。

ただし FlowNode の packed 化 (12B noscan) では `Antecedents` がポインタのまま残ると flowRec は noscan になれず、計画の主目的が成立しない。その時点で「packed 連結リスト (7c 設計の `FlowList{Flow, Next uint32}`)」か「CSR」かを選ぶことになり、CSR を第一候補にする。効果は GC mark と常駐メモリに出る種類のもので、sequential bench では判定しない。

## 7. 未検証

- `addAntecedent` での重複の発生頻度 (§5.1 の判断に必要)。
- 3 label 構成 (§2) にした場合の checker 側の hop 増加。try-finally の個数で上限が決まるので測るまでもないと見ているが、数字は無い。
- `Flags` の空きビットに `len` を置く場合の最大 antecedent 数。switch の case 数や break の本数で決まるので、大きな switch を含む file で最大値を数える必要がある。
