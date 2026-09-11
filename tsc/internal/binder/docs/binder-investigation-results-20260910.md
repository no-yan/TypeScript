# Store binder調査結果（2026-09-10）

追試更新: KPCの逆行を解消し、v7でA/A・8入力の命令数比較が完了した。
以下のKPC欠測は初回実験時点の状態。最新値は
[KPC入力別結果](kpc-binder-results-20260910.md)を参照。

## 問題・結論

同じ入力・Go・測定コードでStoreのbinder退行を再現した。8入力でpointerの
1.29〜1.61倍、checkerは1.53倍。最初の完了条件である入力別退行表と
優先仮説一件の選択は完了。**最適化採用・GC高速化の検証は未完了**。

優先仮説は共通child/list/header解決に分散する追加コスト。
型判定・CFGを除いた走査microにも遅延が残った。ただし、その差を実binderの
原因寄与と同一視しない。子スロット借用sliceの候補はmicroで悪化し、採用しない。
KPCは事前検証でカウンタ逆行を検出したため、動的命令数の比較は欠測。

## 比較条件・artifact

artifact root（以下A）:
`.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/`

- Store: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`
  / `32598cba146fa4dd7b6162b838630c90d865ab28`。
- pointer: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`
  / `8ac035a394c79e693a3a7d74cb170448503ee894`。
- 両者のtsgolint_git_revはnull。currentは各版のrepo_root・両revision一致で判定。
  dirty variant・overlay・binary・fixtureの同一性は別のSHA256で確認。
- 候補checkoutの全パスとrevisionはA/worktrees.txt。
  flownode、store-redesign、store-nolock-exp、lock-profile、profile、store-pr-*等は
  別介入があるため対照にしない。
- A/{store,pointer}-identity.json、dirty.patch、status.txt、store-untracked、
  manifest.jsonに比較条件を保存。既存PropertyAccessExpression候補は保存済
  baseline overlayで除外し、作業ツリーの変更を保持した。
- Go1.26.0、CGO_ENABLED=1、GOMAXPROCS=8、GOGC=100、GOMEMLIMIT=off。
  新規parseと強制GCは各bindのtimer外。両版に同じ測定コードをoverlayした。

| stage | status | 扱い |
|---|---|---|
| 過去eval-8ac035a-20260909・Instruments | stale | 背景証拠のみ |
| 20260909-parent-frequent micro/BindHot/audit | current | 保存済baseline/candidate比較に限定 |
| 同BindKPC | unsupported | 対象Benchmark行なし |
| 今回aa-20、aa-40、paired-8-20 | current | raw・benchstat・identity保存 |
| audit-build-v4 | current | 計数専用、時間比較に使用しない |
| instruments-attach CPU profile | current | wall binaryのhash一致、10秒窓 |
| profile内のpprof | current | identityは一致するがユーザー指定で証拠から除外 |
| kpc-validation-failed | unsupported | counter 2 decreased、Benchmark未実行 |
| fixed/EL0命令数・cycles・load/store・branch・cache比較 | missing | 計測器の検証失敗 |
| 対応区間の絶対CPU時間比較・通常GC CPU・全phase比較 | missing | 未判定 |

CPU profileとBenchmark stageは別。窓記録後に終了させたプロセスのbench.txtには
Benchmark行がなく、そのbenchmark stageはunsupported。失敗したInstruments
launch traceも除外した。pprofによる帰属は使用しない。

## 測定精度・入力別退行

20 pairs × 30 bindのA/Aは、checker CI95 [-0.783,+2.548]%、
dom [-4.459,+0.827]%で不合格。計画どおり独立した40 pairsを一度実行し、
checker [-0.984,+0.810]%、dom [-0.509,+0.396]%で±1.5%を満たした。
隣接ペアのlog比平均を20,000回bootstrapした区間（seed20260910）。
pooled medianのドリフトをpaired区間と混同しない。benchstatも別途保存。
AC給電、thermal警告履歴なし、背景負荷はenvironment-review.jsonに記録した。

全8入力を20rounds、順序反転して比較。小入力はpilotで反復数を決定し、
両版に同じ回数を適用（iterations-8.json）。TSXは小規模特徴テスト。
以下は中央値。全入力のns/op差はbenchstatで有意（表示p=0.000、n=20）。

| 入力 | pointer ns/op | Store ns/op | Δns/op | 比率 | pointer B/op | Store B/op | pointer allocs/op | Store allocs/op |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| Herebyfile.mjs | 261,998.0 | 337,438.5 | 75,440.5 | 1.2879 | 166,014.5 | 223,735.0 | 349 | 596 |
| api.ts | 649,086.0 | 855,368.5 | 206,282.5 | 1.3178 | 415,071.0 | 661,495.0 | 1041 | 1106 |
| checker.ts | 12,458,733.5 | 19,064,295.0 | 6,605,561.5 | 1.5302 | 7,425,344.0 | 12,799,524.0 | 13954 | 14165 |
| client.ts | 185,911.0 | 266,495.5 | 80,584.5 | 1.4335 | 139,444.0 | 198,470.0 | 294 | 334 |
| dom.generated.d.ts | 4,874,793.5 | 6,819,245.0 | 1,944,451.5 | 1.3989 | 5,287,312.0 | 7,867,930.0 | 16562 | 16684 |
| jsxComplexSignatureHasApplicabilityError.tsx | 99,725.0 | 160,192.0 | 60,467.0 | 1.6063 | 109,143.0 | 142,026.0 | 272 | 310 |
| mapCode.ts | 84,743.0 | 128,269.5 | 43,526.5 | 1.5136 | 46,802.0 | 82,861.0 | 123 | 156 |
| proto.generated.ts | 164,673.5 | 221,586.0 | 56,912.5 | 1.3456 | 156,294.0 | 205,808.0 | 274 | 293 |

raw: A/paired-8-20/{pointer,store}.txt、比較: benchstat.txt、区間: summary.json。

## 操作数・正しさ

| 入力 | node訪問 | syntax edge | identifier name判定 | condition | Flow生成 | Symbol生成 | Store ChildRef | Store ListElem |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| checker.ts | 298054 | 298053 | 123553 | 21187 | 79990 | 18444 | 558230 | 104178 |
| dom.generated.d.ts | 109605 | 109604 | 0 | 0 | 4845 | 25112 | 196338 | 47961 |
| Herebyfile.mjs | 3602 | 3601 | 1163 | 47 | 460 | 667 | 7759 | 1707 |
| jsxComplexSignatureHasApplicabilityError.tsx | 1417 | 1416 | 505 | 2 | 48 | 375 | 2416 | 700 |
| mapCode.ts | 1163 | 1162 | 480 | 47 | 263 | 142 | 2468 | 434 |
| api.ts | 12961 | 12960 | 4411 | 233 | 1700 | 1709 | 26350 | 4703 |
| client.ts | 3223 | 3222 | 982 | 82 | 583 | 345 | 6472 | 1296 |
| proto.generated.ts | 3346 | 3345 | 1152 | 0 | 2 | 791 | 6463 | 1706 |

共通論理操作は両版同数。syntax edgeは構造の辺数で、重複するStore内部の
ChildRef解決回数とは異なる。nodeとedgeを加算して正規化しない。
domはidentifier-name判定・conditionが0でも1.40倍。識別子名判定やCFGだけでは
全入力の退行を説明できない。Δinstructions/node・/edgeはKPC欠測により未算出。

全8入力でbinder訪問順、正規化構文グラフ、診断、CFG、node→symbol attachmentは
一致。**checkerのSymbol.Nameは既存差異一件**: 匿名class式でpointerは
`\xfeclass`、Storeは`SymbolLinks`。StoreのbindClassLikeDeclarationRefが
宣言名fallback経由で代入先名を得るため。最適化前から存在し、未修正。
Symbolの完全同等性は成立していない。詳細はaudit-build-v4と
audit-build-v3/checker-symbols.diff。

## Instruments CPU Profilerの帰属

attachで10秒記録。以下はbinderに分類したRunningサンプルのcycle-weightに対する
exclusive leaf割合。inclusiveの親子を合算しない。

| 入力 | pointer主なleaf | Store主なleaf |
|---|---|---|
| checker | bind16.03%、bindChildren5.63%、checkContextualIdentifier4.54%、Node.ForEachChild3.04% | bindKind12.35%、checkContextualIdentifierRef5.03%、bindChildrenRef4.58%、forEachBindChildGenerated3.49%、SetFlow2.15%、isIdentifierNameRef2.15% |
| dom | bind12.90%、bindChildren4.60%、declareSymbolEx4.16%、Node.ForEachChild3.77% | bindKind8.97%、forEachBindChildGenerated3.65%、ListSlotAt3.64%、nameRefGenerated3.59%、declareSymbolRef3.05% |

広いhot pathはwalkとhelpers。狭く介入できる候補は親headerとchild/list解決。
Symbol処理だけが支配する観測ではなく、symbol syntheticは優先しない。

絶対cycle-weightは各summary.txtに保存。ただし窓内のbind処理量と対応CPU時間は
揃っていない。構成比×別測定wallを実測絶対時間として扱わない。
StoreではGCサンプルが多いが、強制GCを含むこの負荷から通常GCの優劣を判定しない。
pprofの既取得結果はEXCLUDED.txtで除外を明示した。

## Allocation driver

| 項目 | pointer | Store | 帰属・制限 |
|---|---:|---:|---|
| FlowNode sizeof | 32 B | 48 B | checkerの79,990個×追加16B=1,279,840B、容量差とは別 |
| Symbol sizeof | 96 B | 104 B | Handle等の拡大 |
| declaration ref sizeof | 8 B | 16 B | Declarations容量等に影響 |
| checker symbolIdx capacity | — | 1,210,352 B | bind前0、bindで列確保 |
| checker flowIdx capacity | — | 1,210,352 B | bind前0、bindで列確保 |
| checker symbolRefs capacity | — | 302,584 B | len18,380、pointer保持 |
| checker flow chunks capacity | — | 3,846,144 B | 全量がpointer比の追加分ではない |

allocator size class、容量余剰、map/arena等の残差は未帰属。
B/op差を上記に無条件で全額割り当てない。noscanやscan量はGC CPU実測を代替しない。

## 最小因果実験・提案修正

同じ親列・子順序・checksumで型判定とCFGを除く。Storeは実binderの生成walker、
pointerはForEachChild。全8入力の三者checksum・子数が一致。
親収集・sortはtimer外、計測中0 B/op・0 allocs/op。

候補はnamed-childアクセスだけを一度の借用slice取得に置換。
list処理・kind判定・binderアルゴリズムは維持。旧list-only hoistの再試行ではない。
全候補ソースはA/walk-experiment内のoverlayに隔離し、本体には適用していない。

| micro | pointer ns/op | Store baseline ns/op | candidate ns/op | baseline→candidate |
|---|---:|---:|---:|---:|
| checker.ts | 2,500,699 | 3,739,368 | 4,178,612 | +11.75% |
| dom.generated.d.ts | 643,487 | 1,248,460 | 1,419,112 | +13.67% |

各200ms×12rounds、三者順序を循環・反転。pointer/baselineとbaseline/candidateを
それぞれbenchstatで比較。候補は両入力で有意に悪化。
逆アセンブルでInvestigationChildRefsへのCALLが残ることを確認した。
静的命令行数を動的命令数として扱わない。

生成walker自身のexclusive帰属は約3.5%。単純な局所割合モデルでは半減しても
全体約1.75%で、named-childだけで3%に届く根拠は弱い（inline/間接効果もあり、
厳密な上限ではない）。局所測定も悪化したため実binder採用試験へ進めない。
共通走査仮説全体を否定する結果ではない。

## 採用条件・未完了

- 最適化採用なし。局所3%改善、pointer同等性能、メモリアクセス高速化、
  GC高速化はいずれも未達/未判定。
- candidateの実binder inst/op・cycles/opはmissing。wall非有意を理由に
  命令数測定を打ち切ったのではなく、計測器の事前検証に失敗した。
  カウンタ逆行を0やunsigned wrapで補正しない。
- 3%以上の有意な実binder短縮、別セッション再現、代表入力の3%非退行区間、
  parse・bind・parse+bind、通常GC CPU/assist/live/scanの採用条件は保持。
- 測定集計の6テストとAST・binder・compiler単体テストは通過。コンパイラ回帰は
  作業ツリーとbaseline overlayの両方で失敗し、失敗集合は一致（親testを含む
  11,115件）。今回の候補を採用する根拠にはしない。regression-comparison.jsonと
  compiler-regression-{baseline-,}tests.txtに保存。baseline-acceptは実行していない。

## 再現方法

repoルートで実行。OUTは新しい絶対パス（既存出力の上書きは拒否）。
小入力の反復数JSONはA/iterations-8.jsonを参照。

```sh
python3 tools/scripts/tsc/binder_investigation.py prepare --out "$OUT"
python3 tools/scripts/tsc/binder_investigation.py run --out "$OUT" --campaign aa-20 --rounds 20
# aa-20が精度不足の場合のみ、事前指定の独立確認を一度実行
python3 tools/scripts/tsc/binder_investigation.py run --out "$OUT" --campaign aa-40 --rounds 40
python3 tools/scripts/tsc/binder_investigation.py run --out "$OUT" --campaign paired-8-20 --mode paired --fixtures all --rounds 20 --iteration-map /absolute/path/to/iterations-8.json
python3 tools/scripts/tsc/binder_investigation.py prepare-audit --out "$OUT" --audit-build audit-build-v4
python3 tools/scripts/tsc/binder_walk_experiment.py --out "$OUT"
python3 tools/scripts/tsc/binder_walk_experiment.py --out "$OUT" --run
python3 -m unittest discover -s tools/scripts/tsc -p test_binder_investigation.py
```

KPC: prepare-kpcはbuildのみ。管理者権限による自己検証を両版・各2新規processで通し、
A/A精度を確認してからpairedを実行する。現ホストでは自己検証不合格。
Instrumentsと同時実行しない。M1 kpep DBと
[Apple XNU kpc.c](https://raw.githubusercontent.com/apple-oss-distributions/xnu/main/osfmk/arm64/kpc.c)
のEL0 A64 maskを記録した。

Instruments: wall binaryを通常起動し、実行中PIDに
`xcrun xctrace record --template "CPU Profiler" --attach PID --no-prompt --time-limit 10s --output TRACE`。
XML exportと実行コマンドはA/instruments-attach内を参照。
launch方式はこのホストで終了時hangしたため採用しなかった。

## 次の行動

優先仮説は共通走査のまま。まずKPCの構成可能カウンタの逆行を専用の小さいprocessで
切り分ける（core移動・PMU所有/状態・累積baselineは未確定の候補）。
計測器の検証後にΔinstructions/node・/edgeを取得し、関数境界・bounds checkの
変更が全体3%に届くか再評価する。新しいkindごとの一括取得は開始しない。
