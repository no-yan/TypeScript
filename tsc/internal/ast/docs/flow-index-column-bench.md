# `flows` 列の `uint32` index 化 + Store 内 FlowNode arena の計測 (2026-09-06)

結論: **採用**。`flows []*FlowNode` (8 B/node、pointer-bearing) を `flows []uint32` (4 B/node、noscan) にし、
FlowNode 本体を Store が持つチャンク arena から確保する。
sequential な Go bench では live heap −2%、CPU 同等だが、large (VS Code src 全体、並列 bind、heap 2 GB) の e2e では
**Bind −15%、user CPU −9%、Memory used −50 MB**。差は GC の mark 量から来る (下記)。

## 設計

- `Store.NewFlow(flags)` が FlowNode を確保し、`FlowNode.id` (Flags 後ろの 4 B padding に収まるので 48 B のまま) に 1 始まりの arena index を書く。
- arena はチャンク列 `flowChunks [][]FlowNode`。サイズは 8, 8, 16, 32, 64, 128、以降 256 固定。
  index → (chunk, off) は `flowSlot` で bits.Len32 とシフトのみ (O(1)、メモリ参照なし)。チャンクは再確保しないので `*FlowNode` は安定。
- `SetFlow(ref, f)`: `f` が自 Store の arena のものかをアドレス比較で検証し、そうでなければ `foreignFlows []*FlowNode` に退避して
  `flowForeignBit | index` を書く。foreign が生じる経路は `store_copy.go` (別 Store からの複製) と checker の合成 flow
  (`&ast.FlowNode{...}`、`c.factory` の node に元ファイルの flow を付ける `checker.go:15165`) の 2 つ。
  検証は 1 エントリの (flow → id) cache で省く。`NewFlow` が cache を prime するので、binder が「作った直後の flow を付ける」通常経路は検証なし。
- binder の `flowNodeArena` (core.Arena、ファイルごとに捨てていた) は削除し `b.store.NewFlow` を使う。store が nil のときだけ `&ast.FlowNode{}`。
- `Flow(ref)` の読み取りは `flows[ref]` → `flowSlot` → `flowChunks[c][off]`。依存ロードは列 1 回 + チャンク要素 1 回で、旧の「列 → deref」と同じ深さ。

## ベンチ (`BenchmarkParseBindColumnPresize`, presize=0)

旧 (pointer 列 + binder arena) と新をそれぞれテストバイナリにし、set ごとに交互実行 (先行を毎ラウンド入れ替え、8 ラウンド)。
`cpu-ms/op` は `getrusage` の user+sys 差分で、別コアの GC mark を含む。M1 8core/8GB、Go 1.26.6。

| set | sec/op | bind-ms | cpu-ms | live-MB |
|---|---:|---:|---:|---:|
| checker.ts (302.6k nodes, 80.0k flows) | 48.17 → 48.19 (~) | 16.33 → 16.67 (~, p=0.07) | 48.0 → 48.2 (~) | 33.01 → 32.27 (−2.2%) |
| small: 377 files (402.8k nodes, 32.0k flows) | 79.11 → 79.28 (~) | 25.33 → 25.16 (~) | 91.5 → 89.3 (~) | 70.59 → 69.26 (−1.9%) |
| dom.generated.d.ts (113.5k nodes, 4.8k flows) | 19.30 → 19.37 (~) | 5.67 → 5.67 (~) | 19.3 → 19.2 (~) | 22.62 → 22.22 (−1.8%) |

(dom は cache prime 前の v3 の値。parse-ms は全 set で不変で、A/B の sanity として機能した。)

live の減少量は列の半減分 (4 B × nodes) にほぼ一致する。FlowNode 本体の総量は変わらない: small で flows は nodes の 8%、
到達可能なのはそのうち 79% だが、旧 arena もチャンク単位でしか回収できず、スロット総数は旧 42,602 / 新 42,416 で同じ。

## e2e (medium `src/tsconfig.monaco.json`, `--noEmit --extendedDiagnostics`, tsc / tsc-exp 交互 5 ラウンド、中央値)

| | Bind s | Check s | Total s | Memory used |
|---|---:|---:|---:|---:|
| 旧 | 0.062 | 2.212 | 2.724 | 838.2 MB |
| 新 | 0.064 | 2.159 | 2.727 | 830.7 MB |

診断出力は同一。checker の読み取り経路 (`Flow` → arena) に悪化は見えない。

## 途中で捨てた版

- v1: 256 固定チャンク。small で live +2.7%。小ファイル (平均 1,068 nodes、flow 85 個) では 12 KB チャンクの空きが列の節約 (4 KB) を上回る。→ 幾何級数チャンクに。
- v2〜v4: checker.ts の bind が +3% (p≈0.01)。80k flows × 約 6 ns で、cache miss 時の arena アドレス検証の分。
  `NewFlow` の inline 化 (cost 109 > 80 で不可) では消えず、`NewFlow` で cache を prime する v5 でノイズ内に。

## e2e (large `src/tsconfig.json`, 9788 files, `--noEmit --noCheck --extendedDiagnostics --declaration false`)

この worktree の他の未コミット変更 (hint `len/5`、ensureCol など) はそのままに arena だけ戻した `tsc-noarena` を作り、
`built/local/tsc` (arena) と交互に 6 ラウンド (warmup 1 回、`/usr/bin/time -l`)。中央値:

| | Parse s | Bind s | Total s | real s | user s | sys s | Memory used |
|---|---:|---:|---:|---:|---:|---:|---:|
| arena なし | 0.716 | 0.345 | 1.246 | 1.65 | 8.05 | 1.11 | 2066 MB |
| arena あり | 0.730 | 0.293 | 1.210 | 1.59 | 7.35 | 1.08 | 2016 MB |

Bind の 6 run は 0.286〜0.328 vs 0.302〜0.370、user は 7.05〜7.72 vs 7.57〜8.39 でほぼ重ならない。Parse は不変。

sequential bench (−2% live、CPU 同等) より効くのは、利得がバイト数ではなく **GC が辿るポインタ数** だから。
旧列は fill 50% の `*FlowNode` で、mark ごとに node 数の半分のポインタを greyobject する
(large で数百万個)。新列は noscan で、FlowNode へは chunk slice 1 本 (256 個に 1 ポインタ) と
FlowNode 内部のポインタ (旧と同じ) からだけ辿る。並列 bind 中は GC が回り続けるので user CPU に出る。
checker.ts の cpuprofile で `gcDrain` が 5.0 s → 2.6 s になっていたのはこれで、当初は bench の強制 GC のノイズと誤読した。

注意: ブランチ間の hyperfine 比較 (`lock-design-inv` = merge-base、hint `len/10`) は hint 変更などが混ざるので arena の効果としては読めない。

## 測っていないこと

- large の check あり e2e (checker の `Flow` 読み取り経路は medium でのみ確認)。
- `go test ./...` の失敗 (`internal/api/encoder` の `TestDecodeSourceFile_*EmptyParams`、fourslash の `TestTsxRename1` / `TestSmartSelection_JSDocTags4`) は変更前の状態でも同じく失敗する既存のもの。
