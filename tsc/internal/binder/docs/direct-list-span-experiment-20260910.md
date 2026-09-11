# list slotから直接Spanを得る実験（2026-09-10）

## 問題と仮説

先行実験では配列分離の追加改善を確認できず、同じlistの再解決をloop外へ出す整数Spanは命令を減らした。
今回の候補は、callerが既に知るStoreとlist slotのlocal indexを保持し、ListRefのStore ID付加と再確認を省く。
list index→headerの依存load自体を消す配置変更ではない。

## 4条件

- normal: 保存baselineのBinderと生成walker。
- span: 既存bindListRefの整数Span。
- control: spanをベースに、生成walkerの129箇所をbindListSlotSpanへ変更。
  ResolveBindListSlotSpanは従来のListSlotAt→TryBindListSpanで解決する。
- direct: controlと同じwalker、同じBinder source。ResolveBindListSlotSpanだけを直接slot解決へ置換。

normal→spanは既存効果、span→controlはhelper/経路変更の費用、control→directはresolverの変更効果、
span→directは今回の追加価値を評価する。compilerのinlineやspillの変化も含む実装対照であり、atomic単命令の費用測定ではない。
元のnode/list配置、Go境界検査、setter検査、2pass、訪問順、意味判定を維持する。
通常のnamed child getterや生成walkerの関数分割は変更しない。

直接resolverはslot範囲を検査し、local indexからlistsのstart/lenを読み、整数Spanを返す。
local ListRefを組み立てないので、この経路でStore.IDを確認する必要がない。
空slotは空Span、foreign slotは元のListRefを返して既存bindListRefへfallbackする。
整数SpanはStoreの現在のchildrenを読むので配列拡張後の更新を読めるが、走査中の当該listのstart/len不変が契約。
本番の全writer契約の証明ではなく、既存の限定Spanと同じ範囲の実験である。

## 実行条件・artifact

tools/scripts/tsc/binder_direct_list_span_experiment.pyで生成したoverlay。
artifact: .cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/direct-list-span-experiment-02/。
元の実装は変更しない。codegenの該当1行の変更案もartifact内generate-go-ast.tsへ保存。
本実験のwalkerは対応する129箇所を機械置換し、制御順を保持する。generator全体の再実行・本体採用は未実施。
初回準備はaudit専用walkerでsが未使用になりビルド失敗したため、auditだけで変数使用を明示して02を作成した。
初回artifactには性能計測がない。比較値は02の同一campaignから取得する。

Go1.26.0 darwin/arm64、Apple M1、GOMAXPROCS=8、checker/dom各10 bind×6 rounds。
wall GOGC=100、KPC GOGC=offを順次実行。parseはtimer外。
A/Aの許容幅は命令±1%、cycles/wall±1.5%。失敗した指標も固定の探索比較を保存するが採用の根拠にはしない。
追加roundやsample除外は行わない。5pairをbenchstatで比較する。

## 検証・結果

4条件×20入力で構文・診断・Symbol・Flow・訪問順・node計数が一致。
候補のAST/Binder/Compilerテストと追加Span/slot境界試験が成功。
control/directのBinderとwalkerのsource SHAはwall/kpc/auditの各modeで完全一致。
resolverを除くconsumerのsource差を排除した。生成walkerのChildRef CALLは全条件0のまま。

対象は129箇所、実行回数はchecker 72,074 / dom 48,138。
controlとspanの既存アクセサ計数は一致し、ResolveBindListSlotSpanの追加だけ。
directではcontrol比でListSlotAt −72,074 / −48,138、TryBindListSpan −31,353 / −18,172、
ID −62,706 / −36,344。後者はListRef作成とSpan受取時の確認の各1回に対応する。
この対象は既存Spanで解決するnonzero listの約95% / 100%を含む。
listOwnerの削減は既存Spanと同じ−83,520 / −53,596であり、今回さらに減ったわけではない。

機械語ではcontrolのresolverはCALL、directのresolverはbindListSlotSpanへinlineされた。
controlにはListSlotAt CALLとID確認が残り、directのlocal経路にはいずれもない。
foreign/空slotではmap参照が残る。結果はowner再確認削減とCALL/inline/配置等を合わせた効果。
同じsource consumerであっても同じ機械語・spillとは限らず、保持費やatomicだけを分解した対照ではない。

## A/A精度

paired mean log ratio bootstrap 95%区間（20,000回、seed=20260910）:

| 指標 | checker | dom | 判定 |
|---|---:|---:|---|
| EL0命令 | −0.26〜−0.03% | −0.14〜+0.56% | 両入力±1%内 |
| EL0 cycles | −0.42〜−0.17% | +0.10〜+1.10% | 両入力±1.5%内 |
| 通常GC wall | −12.80〜+0.49% | −2.54〜+0.73% | 両入力不合格 |

wallは一部sampleのばらつきが大きく、p値にかかわらず採用根拠としない。
KPCのns/opはGOGC=off・計測器付きで、通常GC時間の代替にしない。
事前protocolどおり固定比較を実行し、追加roundやsample除外はしていない。

## 比較結果

同一campaignの中央値比。benchstat各n=6。p値は探索的・多重比較補正なし。

| 比較 | 命令 checker | 命令 dom | 通常GC時間 checker | 通常GC時間 dom |
|---|---:|---:|---:|---:|
| normal→span | −1.73% (.002) | −2.71% (.002) | −0.87% (.065) | −0.84% (.026) |
| span→control | +0.59% (.002) | +1.21% (.002) | −0.89% (.093) | +1.18% (.004) |
| control→direct | −1.13% (.002) | −1.62% (.002) | −0.04% (.937) | −1.97% (.041) |
| span→direct：追加価値 | −0.55% (.093) | −0.43% (.041) | −0.93% (.065) | −0.81% (.818) |
| normal→direct：合計 | −2.27% (.002) | −3.12% (.002) | −1.79% (.004) | −1.64% (.041) |

control比だけなら良く見えるが、helper化の命令増を相殺する部分が大きい。
採用判断に重要な既存Span比ではcheckerの命令差は非有意、domも0.43%に留まる。
追加wall差は両入力で非有意かつA/A精度不足。cyclesも全5pairの両入力で非有意。
複数の中央値比の差を検定済みのinteraction効果とは扱わない。

実測中央値（時間は通常GC、命令は別KPC campaign）:

| 入力 | 条件 | ns/op | B/op | allocs/op | EL0命令/op |
|---|---|---:|---:|---:|---:|

| checker.ts | normal | 16,380,373.0 | 12,799,486.0 | 14,165.0 | 185,178,027.0 |
| checker.ts | span | 16,237,620.5 | 12,799,460.0 | 14,165.0 | 181,977,764.5 |
| checker.ts | control | 16,093,102.0 | 12,799,460.0 | 14,165.0 | 183,049,724.0 |
| checker.ts | direct | 16,087,396.0 | 12,799,473.0 | 14,165.0 | 180,977,022.0 |
| dom.generated.d.ts | normal | 5,809,673.0 | 7,870,409.0 | 16,684.0 | 77,759,592.0 |
| dom.generated.d.ts | span | 5,760,698.0 | 7,864,831.5 | 16,684.0 | 75,652,796.0 |
| dom.generated.d.ts | control | 5,828,858.5 | 7,866,665.0 | 16,684.0 | 76,571,092.5 |
| dom.generated.d.ts | direct | 5,714,148.0 | 7,868,537.0 | 16,684.0 | 75,330,017.0 |

B/op・allocs/opは実質同じ。今回のSpan・resolverは整数を一時保持するだけで、永続node/list表現は増やさない。
GC CPU・scan・assistは未測定。残存allocation driverはsymbolIdx/flows列、FlowNode 32→48B、Symbol 96→104B、Handle 8→16B。

## 診断・次の行動・受け入れ条件

既知のlocal slotからListRefを組み立て直す必要はなく、直接解決でID確認とCALLを省けることは確認した。
ただし生成walkerの多数のlist slotをカバーしても、既存Spanへの実Binder命令削減は約0.4〜0.6%。
この経路の微調整を主な到達手段にしない。候補を保存し、本体採用を見送る。

広いhot pathはwalk/helpersであり、今回の対象はその中のlist経路。
次はChildRef/KindAt/FlagsAt等の残存呼出と再解決量、構文writer契約を監査し、
必要な約29〜35% wall短縮に結び付く対象予算を先に見積もる。
list resolverをさらに細分化して小さい実験を続ける優先度は下げる。

採用には、全callerでSpan有効期間とforeign/共有listの意味を保証し、独立した精度内wall比較、
8入力の非悪化、parse+bind全体とGC評価を満たす必要がある。
今回の合計命令削減をpointer同等やGC高速化の証明として扱わない。

## repo・artifact状態

selected repo: /Volumes/SanDisk1TB/worktree/cursor-ast-store-tests、32598cba146fa4dd7b6162b838630c90d865ab28。
pointer checkout: /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript、8ac035a394c79e693a3a7d74cb170448503ee894。
tsgolint_git_revは両者null。candidate clonesはflownode、land-44-45、lock-design-inv、lock-profile、
merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜5、
store-pr-6-perf、store-pr-7、store-pr-7-attach-parent-fix、store-pr-7-nodeseq-t10、store-redesign。
全pathはworktrees.txt。別介入を混ぜないため不使用。

02のartifactはrepo_root/tsgolint_git_rev/typescript_go_git_revが選択checkoutと一致しcurrent。
HEAD判定とdirty/overlay一致を区別し、dirty.patch、go-sources.json、build/overlay/source/binary SHAを保存。
保存baseline Binderを使用し、現在のdirty本体Binderは変更していない。
旧profileは今回候補への帰属にはstale。初回準備の性能結果、今回のpointer同時比較、parse+bind、GC専用指標、
全writer契約証明はmissing。今回の全rawに要求Benchmark行がありunsupported stageはない。
raw、5pair benchstat、summary区間、80入力監査、テスト、機械語、実行scriptを02に保存。

## 再現

scriptのDを新規出力先へ変更してprepare、runtime <新規temp先>、wall <temp先>、kpc <temp先>を順次実行。
KPCはmacOS管理者認証が必要。新しい結果で保存rawを上書きしない。
初回のaudit未使用変数修正は監査専用で、性能版に追加の処理はない。
