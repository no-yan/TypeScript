# Attempt 018: nodeHeader を 24 バイトにし Symbol 列を index 化

作成日: 2026-09-04  
作業: `/Volumes/SanDisk1TB/worktree/store-pr-7-attach-parent-fix` `perf/node-header-24b`  
コミット: `bd3d992865`

## 結論

**keep.** full `src/tsconfig.json` を `--noCheck --noEmit` で 12 往復した対応あり比較で Bind **−16%**（95% CI が 0 を含まない）、Parse 差なし、RSS −4%。CPU プロファイルでは `AllocSlots` −29%、GC mark −13%、Store 常駐 −31%。

## 変更

`nodeHeader` 44B → 24B。

| 手 | 根拠 |
|---|---|
| `childLen` / `listLen` を uint8 に | schema 最大は 6 と 4 |
| `listStart` を消し、list slot を child slot の直後に `children` 列へ置く | 両方 u32 なので 1 本の列で足りる。`listSlots` 列と checkpoint field を削除 |
| `identText` を `childStart` と共用 | primary string slot を持つ 11 kind はすべて slot 数 0。`Ident` は `childLen\|listLen == 0` のときだけ読む。slot なしノードの `childStart` は `AllocSlots` が 0 にする |
| `tokenFlags` を `map[NodeRef]TokenFlags` に | 充填率 3.9% |

Symbol 列: `[]*Symbol`（ノード数ぶん）→ `symbolIdx []uint32`（noscan）+ `symbolRefs []*Symbol`（充填ぶんだけ、1-based）。充填率 12.5% なので GC が走査するポインタ数は 1/8。

FlowNode 列は据え置き。充填率 50% なので index 化してもバイト数は減らず、checker の `Flow()` に依存ロードが 1 段増えるだけ。FlowNode 自体の index 化（arena を noscan にする）は binder 全体に触るので別作業。

## 計測

workload: `/Volumes/SanDisk1TB/ghq/github.com/microsoft/vscode` `src/tsconfig.json`（9788 files）、`--noCheck --noEmit --extendedDiagnostics`、base/new 交互 12 往復。base は HEAD (`08457114d8`) + worktree の未コミット差分（`parser.go` storeHint `/5`、pprof session）を含む。

| stage | base med | new med | ratio | 対応あり差 new−base (95% CI, t 分布 df=11) |
|---|---:|---:|---:|---:|
| Parse | 1.156s | 1.169s | 1.011 | +0.001s [−0.051, +0.053] |
| Bind | 0.851s | 0.712s | 0.837 | **−0.152s [−0.261, −0.043]** |
| Total | 2.260s | 2.141s | 0.947 | −0.150s [−0.315, +0.015] |
| max RSS | 1922MB | 1837MB | 0.956 | −57MB [−160, +46] |

CPU プロファイル（各 1 回、`--pprofDir`）:

| 項目 | base | new |
|---|---:|---:|
| Total samples | 15.61s | 14.39s |
| `Store.AllocSlots` cum | 2.55s | 1.80s |
| `runtime.gcBgMarkWorker` cum | 4.01s | 3.50s |
| `binder.bindKind` cum | 2.04s | 1.01s |
| `runtime.madvise` | 2.39s | 3.60s |

`madvise` の増加は 1 回計測のノイズと見ている（scavenger のタイミング依存）。往復計測の Total では増えていない。

heap inuse（memprofile、bind 終了時）:

| 項目 | base | new |
|---|---:|---:|
| `NewStore`（nodes/children/intern 事前確保） | 1.31GB | 902MB |
| `ensureCol[*T]`（symbols+flows / flows のみ） | 245MB | 99MB |
| `ensureCol[uint32]`（symbolIdx） | – | 66MB |
| heap 合計 | 2.49GB | 2.10GB |

Parse が動かないのは、AllocSlots の減少分（初回タッチ）を madvise / page fault の揺らぎが埋めているため。Bind の改善は header が 1 cache line に 2.67 行入るようになり binder の `bindKind` 走査が半減したことによる。

## ゲート

- `go test ./internal/ast ./internal/parser ./internal/binder ./internal/checker ./internal/compiler ./internal/printer ./internal/scanner` PASS
- `./internal/transformers/tstransforms` の `TestImportElision` 2 件は HEAD でも同じく失敗する既存問題（store.go を HEAD に戻して再現確認済み）
- 追加テスト: `store_layout_test.go`（`unsafe.Sizeof(nodeHeader{}) == 24`、Ident と child slot の非干渉、list slot の配置、tokenFlags 側表、Symbol index 列）

## 次

1. FlowNode の index 化。`FlowNode` は 48B で 4 ポインタ（Handle 内 *Store、*Node、*FlowNode、*FlowList）。NodeRef と arena index に置き換えれば 24B noscan になり、GC mark の残り（3.5s）の主因が消える。
2. `madvise` 3.6s は GOGC / メモリ制限の調整対象。Store の事前確保が大きいほど scavenger が返却と再確保を繰り返す。
