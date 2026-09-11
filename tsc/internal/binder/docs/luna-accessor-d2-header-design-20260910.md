# D2：header位置とmutable意味情報（設計待ち）

カード種別: 設計のみ。契約なしのheader pointer/slice保持は実装しない。

## 問題

同じnodeのheaderをKindAtの後にFlagsAt/Symbol/Localsで再解決する経路がある。
ただしFlags・Symbol・LocalsはBind中に更新される。構文child snapshotとは別契約が要る。

## writer介在の区分

| 読み取り | Bind中に変わりうるか | 共有可否 |
|---|---|---|
| kind / childStart / list slots / list start+len | 構文編集がない限り不変（通常Bind） | 短命snapshot可（Access*/BindListSpan） |
| Flags | 再帰前後でSetFlagsあり | 値のcache不可。位置だけ渡してfresh read |
| Symbol / Declarations | declareで作成・差替え | 短い直線経路でのみ取得済みポインタ渡し候補。表全体cacheは禁止 |
| Locals / Exports tables | 遅延作成・差替え | 同上 |
| FlowNode | 常に更新 | 共有禁止 |

## header位置を保持する場合の排除すべき前提

1. `nodes`配列の`append`再配置後に古い`*nodeHeader`を使わない。
2. Compact / Restore をまたがない。
3. 別goroutineのwriterをBind読取と並行させない（現行Binderは単一goroutine前提を明示）。
4. 構文childの整数Spanと、header pointerを同一ライフタイムで混ぜない。

## 契約案（最小API候補・未実装）

```text
type NodeHeaderView struct { /* unexported index or offset; no exported *header */ }

// Bind中・単一goroutine・Compact/Restore外でのみ有効。
// Kindは取得時の値。Flags/Symbolは毎回 Store 経由で fresh read。
func (s *Store) ViewNode(ref NodeRef) NodeHeaderView
func (v NodeHeaderView) Kind() Kind
func (v NodeHeaderView) Flags(s *Store) NodeFlags  // always re-read
```

検査入口案:

- debug/assert buildで View 取得後に `SetFlags`/`declare` を挟んだら Flags の再読が新しい値になること。
- Compact/Restore 後に View を使うと panic する世代カウンタ（任意）。

## 実装範囲

契約と世代検査の最小APIだけを別カードで提案可能。Bind全体へheader pointerを広げる実装はレビュー後。
Symbol/Localsの取得済み渡しは「declare直線でwriterが介在しないこと」を経路ごとに監査してから。

## 状態

**設計待ち**。今回のLuna accessor実装（P0〜P9）では構文整数snapshotのみ採用し、本契約は未適用。
