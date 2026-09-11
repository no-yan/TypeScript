# KPC入力別結果（2026-09-10）

追試: 呼出箇所別の監査と、宣言名解決の再利用候補を検証した。
domでinst2.49%・cycles2.67%減少、通常GC wallは非有意で採用せず。
[宣言名再利用実験](name-memo-experiment-20260910.md)を参照。

## 結論

新規OS threadによる測定でカウンタ逆行を解消し、両イベント群のA/Aを通過した。8入力の命令数と4入力のload/store比較が完了。**Storeはcache missを減らす一方、load/store・分岐を含む追加命令のためbinderが遅い**という仮説を支持する。どのAPIが時間差の何割を説明するかは未確定。

## 比較条件・状態

- repo/revision/候補checkoutは[binder実験ノート](binder-investigation-results-20260910.md)と同じ。Store `32598cba146f`、pointer `8ac035a394c7`、TSGolint=null。
- artifact: `.cursor/skills/verify-tsc/artifacts/20260910-kpc-reliability/campaign-v7/`。
- binaries/overlay/source hash: `20260910-binder-investigation-02/kpc-build-v7/`。
- Go1.26.0、CGO=1、GOMAXPROCS=8、GOGC=off、GOMEMLIMIT=off、30bind×10rounds、順序反転。parse/強制GCは計数外。
- 新規pthread生成・joinは計数外。計数区間にはStartTimer/StopTimerのbookkeepingを含む。特に小入力ではこの共通固定費に注意し、未測定のoverheadを引き算しない。
- 両版のrepo_root/両revisionを確認、TSGolint=null。実行時のcwd/fixture pathはmacOS管理者processの外付けvolume制約により/tmpへ移動。fixture内容hashとbuild identityは維持。
- v6のinstructions A/Aはcurrentだが、memory A/Aは途中のsource-root探索失敗で未完了。v7とは合算しない。v5のBenchmark stageはunsupported（行なし）。
- v7のA/A、8入力instructions、4入力memoryはcurrent。通常GC CPU/assist、全phase、局所改善採用は依然missing/未判定。
- profileは既取得Instruments CPU Profilerのみ。KPCとの同時実行なし、pprofは根拠から除外。

## A/A精度（paired log-ratio 95%区間）

| group | fixture | EL0 inst % | fixed inst % | EL0 cycles % | fixed cycles % |
|---|---|---:|---:|---:|---:|
| instructions | checker.ts | [-0.053, +0.253] | [-0.239, +0.425] | [-0.452, +0.367] | [-0.677, +0.435] |
| instructions | dom.generated.d.ts | [-0.159, +0.069] | [-0.256, +0.151] | [-0.650, +0.088] | [-1.073, +0.225] |
| memory | checker.ts | — | [-0.285, +0.087] | [-0.395, +0.280] | [-0.546, +0.537] |
| memory | dom.generated.d.ts | — | [-0.212, +0.188] | [-0.246, +0.241] | [-0.525, +0.338] |

両groupでinst±1%、cycles±1.5%を満たした。rawとbenchstat、20,000回bootstrap集計を保存。同じbinary/event-config hashの合格A/Aがなければpairedを拒否する。

## 入力別EL0命令数（通常GC wallとは別表）

| 入力 | pointer inst/op | Store inst/op | 比率 | Δinst/node訪問 | Δinst/syntax edge | cycles比率 |
|---|---:|---:|---:|---:|---:|---:|
| Herebyfile.mjs | 2,286,865 | 3,158,244 | 1.3810 | 241.9 | 242.0 | 1.4131 |
| api.ts | 5,566,906 | 8,628,847 | 1.5500 | 236.2 | 236.3 | 1.4794 |
| checker.ts | 110,941,175 | 185,303,964 | 1.6703 | 249.5 | 249.5 | 1.4296 |
| client.ts | 1,554,456 | 2,395,078 | 1.5408 | 260.8 | 260.9 | 1.5008 |
| dom.generated.d.ts | 49,137,514 | 77,757,086 | 1.5824 | 261.1 | 261.1 | 1.3749 |
| jsxComplexSignatureHasApplicabilityError.tsx | 970,874 | 1,467,619 | 1.5116 | 350.6 | 350.8 | 1.5575 |
| mapCode.ts | 793,231 | 1,158,143 | 1.4600 | 313.8 | 314.0 | 1.4662 |
| proto.generated.ts | 1,731,972 | 2,485,566 | 1.4351 | 225.2 | 225.3 | 1.4859 |

全8入力のinst/op・cycles/op増加はbenchstatで有意（表示p=0.000、n=10）。nodeとedgeは別の分母であり加算しない。syntax edgeは構造上の辺数で、実行時の重複アクセス回数ではない。

checker/dom/Hereby/api/client/protoは追加225〜261inst/nodeで近い。小さいmapCode/TSXは314/351と大きい。8入力の相関だけで因果や固定単価を断定しない。domのidentifier-name判定・conditionはともに0、protoのconditionも0で、CFG/名前判定だけでは共通退行を説明できない。

## load/store・分岐・cache

| 入力 | load/store比率 | L1D load miss比率（memory群） | branch比率（instructions群） | branch miss比率（instructions群） |
|---|---:|---:|---:|---:|
| Herebyfile.mjs | 1.4109 | 0.8357 | 1.4939 | 1.0509 |
| checker.ts | 1.6253 | 0.8658 | 1.6920 | 1.0215 |
| dom.generated.d.ts | 1.5368 | 0.8672 | 1.6289 | 1.0203 |
| mapCode.ts | 1.4776 | 0.9226 | 1.5322 | 1.2066 |

全4入力でload/store増加とL1D load miss減少が有意。load/storeはload単独ではないので、この2値を割って通常のcache miss rateとは呼ばない。異なるイベント群の数値を同一区間の同時計数とも扱わない。

checkerはEL0命令数+67.03%、cycles+42.96%、分岐命令+69.20%、分岐予測ミス+2.15%。予測ミス単独より予測可能なguard/解決の追加と整合するが、bounds checkだけが原因だと断定しない。cache miss減少・CPI低下は、メモリアクセス全体の高速化や通常GC高速化の証明ではない。

## 通常GC wall・割り当てとの区別

通常GCでのcheckerは12,458,733.5→19,064,295ns/op、7,425,344→12,799,524B/op、13,954→14,165allocs/op。domは4,874,793.5→6,819,245ns/op。KPC下のwallとは混ぜず、詳細な8入力表は前ノートを参照。hot pathはInstrumentsのwalk/helpers。allocation driverはsymbolIdx/flowIdx列、FlowNode32→48B、Symbol96→104B、declaration ref8→16Bで、今回その表現は変更していない。

## 狭い候補の寄与を監査

audit-build-v5は生成walker内のnamed-child読み出しだけを別計数した。v4から論理操作数・訪問順・syntax/CFG/symbol/診断hashは不変。計数binaryの時間を性能値として使わない。

| 入力 | walker内ChildRef | 全ChildRef | 全体instの3%に相当する削減/対象read |
|---|---:|---:|---:|
| Herebyfile.mjs | 3,390 | 7,759 | 27.9 inst |
| api.ts | 11,298 | 26,350 | 22.9 inst |
| checker.ts | 189,849 | 558,230 | 29.3 inst |
| client.ts | 2,630 | 6,472 | 27.3 inst |
| dom.generated.d.ts | 74,321 | 196,338 | 31.4 inst |
| jsxComplexSignatureHasApplicabilityError.tsx | 1,113 | 2,416 | 39.6 inst |
| mapCode.ts | 868 | 2,468 | 40.0 inst |
| proto.generated.ts | 3,122 | 6,463 | 23.9 inst |

checkerの対象readは全558,230回中189,849回（約34%）。全ChildRefを対象回数として見積もると約2.9倍過大評価する。10inst/read削減の仮定でも約1.02%の全体命令数に相当する。**これは時間3%の厳密な上限ではない**。時間採用条件はwall3%以上、有意、inst/cycles低下のまま。

前回の借用slice候補はmicroで+11.75/+13.67%悪化し、本体へ適用していない。今回の監査も、生成walkerだけへの同じ介入を優先する根拠にはならない。

## 仮説・提案修正・採用条件・次の行動

優先仮説は共通walk/helper間に分散するStore解決の追加。次の実験は、共通のheader/child解決をhelper呼出し間で繰り返す最小経路を切り出し、同じ仕事のまま繰り返しを減らせるか検証する。特定kindの一括取得を順番に追加する運用や、既存Kind cache/list-only hoistの証拠なし再試行はしない。

採用には実入力wall3%以上・benchstat有意・inst/cycles減少・別セッション再現・代表入力の非退行区間・正しさ・全phase/通常GCの確認が必要。今回新しいbinder最適化は採用していない。既存の匿名class Symbol.Name差異とコンパイラ回帰baseline失敗も未修正。KPC修正をbinder高速化と数えない。
