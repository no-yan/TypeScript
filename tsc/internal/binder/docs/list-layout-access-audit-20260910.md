# list/child配列共有と依存load（2026-09-10）

配列を分離するだけでは、多段アクセスはなくならない。原因は物理的な配列共有ではなく、
nodeのlist slotにstart/lenではなくlist indexを格納し、要素APIが毎回list headerを再解決すること。
現行sourceと最小Goモデルのarm64機械語で確認した。実Storeのlayoutは変更せず、新規benchmarkも実行していない。

## 現行経路

`store.go`のchildrenは、named child slot、list index slot、list要素の3用途を兼ねる。
`ListSlotAt`はnodeHeaderを読み、`listSlot(n.childStart+n.childLen+slot)`を呼ぶ。
local slotはchildrenからindexを取得し、`listRef`でStore IDを付加する。
`ListElem`はowner解決後、lists[index]のstart/lenを読み、children[start+i]を読む。

```
node header → slotのlist index → list headerのstart → 要素NodeRef
```

これはデータ依存の4読出段階（要素前のmetadata 3段）を表す。
Store内のslice base、境界検査のlen、owner処理等もあるため、実際のload命令総数やcache miss数が4という意味ではない。

atomicについては呼出元が異なる。

- ListSlotAt: local非zero slotでは `listSlot → listRef → ID`。listOwnerは呼ばない。
- ListLen / ListElem: `listOwner → ID`。owner ID=0ならshort circuitでID読出を省く。
- 登録済みStoreのqualified local ListRefでは、ListLenで1回、各ListElemで1回のID確認がある。
- 配列の分離はこのowner APIの契約を変えないため、atomic loadを除去しない。

`bindListRef`ではListSlotAtはcaller側、ListLenはloop外、ListElemは要素ごと。
上記4段すべてを要素ごとに再実行するわけではない。要素ごとに残る主な依存は
`list headerのstart → 要素NodeRef`とowner確認。長いlistで重要なのはこの反復側である。
また要素NodeRefからKindAtを読む次段は、今回の図の外に残る。
現行store.goにlist要素のsliceを返すListElems APIはない。

## 分離方法ごとの違い

| 変更 | metadataから要素までの経路 | 消える依存 |
|---|---|---|
| list要素だけ専用elementsへ移す | header → index slot → list header → elements | なし |
| list index slotも別のuint32配列へ移す | header → listSlots → list header → elements | なし |
| nodeが連続list descriptorのbaseを持ち、slotにstart/lenを直接配置 | header → descriptor[base+slot] → elements | list indexを読む1段 |
| loop前にowner/start/lenを解決し保持 | 初期解決後、start+i → elements | loop内のlist header再解決 |
| 安定区間で要素sliceを借用 | 初期解決後、slice[i] | loop内のowner/header再解決とStoreの配列base再取得 |

最後の2つはchildrenとの共有を維持したままでも実現できる。
整数SpanはStoreの現在の配列baseを再取得する一方、sliceは借用時のbaseを保持する。
後者は再配置後の更新可視性を含む契約が必要。参照先が[]NodeRefなら、短命sliceで借用してもbacking arrayはnoscanのまま。

直接descriptor配置は実装上の制約がある。現在のlistはnode slotとは独立に作成でき、複数slotで同じlistを共有できる。
metadataをslotへ複写するなら、list identity、loc、共有aliasの更新とforeign listを維持する必要がある。
identity対応表へ毎回戻る実装では、その経路に間接参照が再び入る。
全nodeHeaderへ独立uint32 listStartを素直に追加すると24→28 byteとなり、node列だけで約16.7%増える。
start/lenの二重保持や配列ごとの余剰capacityも含めて評価する。これは実測B/op増加率ではない。

## 機械語確認

artifact: `.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/list-layout-access-audit/`。
`layout.go`は依存関係だけの4関数。Go 1.26.0 darwin/arm64で通常buildと-Bのcompile -Sを保存。
owner、foreign、実ASTのvalidationを実装した性能モデルではない。通常版でも同じデータ依存が残り、
-B版で境界検査を外して読みやすく確認した。

- SharedElement: header → children[index slot] → lists[start] → children[element]。
- SplitElement: 同じ依存列で最後がelementsへ変わるだけ。さらに専用elementsのslice baseを読む命令がある。
- DirectElement: header → lists[start+slot] → elementsで、index slotのloadが消える。
- ResolvedElement: 渡されたslice base/startから要素を読む。準備段階の解決費はこの関数外。

小さい関数の命令数を実Binderの削減率へ換算しない。単純分離によるcache局所性やcapacityの効果はこの確認から判断できない。

## 既存artifactと性能上の位置づけ

selected repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、32598cba146fa4dd7b6162b838630c90d865ab28。
pointer checkout: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、8ac035a394c79e693a3a7d74cb170448503ee894。
tsgolint_git_revは両者null。candidate clonesはflownode、land-44-45、lock-design-inv、lock-profile、
merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜5、
store-pr-6-perf、store-pr-7系列、store-redesign。全pathは今回のworktrees.txt。別介入として不使用。

今回のsource監査artifactは3 identity項目が一致するためcurrent。dirty source SHAも別途保存。
先行list-span-experimentもidentity上currentだが、保存overlayの履歴結果であり、現在のdirty Binderや配列分離版の性能値ではない。
今回のlayout変更による実Binderのns/op、B/op、allocs/op、KPC計測はmissing。
要求regexを含まない既存bench.txtを流用しておらず、今回unsupported stageはない。古いprofileの候補帰属はstale。

先行Spanの保存rawに対するbenchstat結果（再計測なし）:

| 入力 | ns/op baseline → Span | B/op baseline → Span | allocs/op | EL0命令差 |
|---|---:|---:|---:|---:|
| checker | 16,261,064.5 → 16,219,170.5 | 12,799,498.5 → 12,799,524 | 14,165 → 14,165 | −1.42%, p=.002 |
| dom | 5,742,694 → 5,671,556.5 | 7,870,383.5 → 7,870,409 | 16,684 → 16,684 | −2.68%, p=.002 |

wall差は−0.26% / −1.24%で非有意。配列共有を維持したまま反復解決を除く機構は既に確認済みだが、高速化採用条件は未達。
この先行実験のbindListRef対象はListElem 50,646 / 35,424回。広いhot pathはwalk/helpersであり、その一部しか置換していない。
allocation driverであるsymbolIdx/flows列、FlowNode・Symbol・Handle大型化はどの案でも別途残る。

## 次の設計と受け入れ条件

依存段数削減を目的に、単純なelements分離だけを次の高速化候補にはしない。
まず共通の構文読取契約の下で、list slotから直接Spanを得るAPIを検討する。
現行配置のSpan、直接descriptor配置、安定区間のslice借用を同じcallerと所有権条件で比較する。
直接descriptor配置には共有listの意味を保つ書込設計が先に必要。

実装比較へ進む際は意味・訪問順・foreign/共有aliasの一致、機械語での依存除去、
通常GCのns/op・B/op・allocs/opとKPCの命令/cyclesを同じ入力でbenchstat比較する。
配列分離の効果を調べるなら単純分離を独立controlにし、Span導入の効果と混ぜない。
parse/Compact/Restore/Factory.ListRefsのbulk copy、scratch再利用も監査し、Binder外への費用移動を確認する。
