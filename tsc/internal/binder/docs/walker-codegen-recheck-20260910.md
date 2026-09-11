# 他ベンチマーク停止後のwalker A/A再確認（2026-09-10）

以下はrecheck-01の記録。後続の[再計測02](walker-codegen-recheck-02-20260910.md)ではKPCのA/Aが通過し、
3者比較を完了した。rawと判断はセッションごとに分けて保存している。

## 問題・結論

ユーザーから、並行していた別のベンチマークを停止したとの連絡を受けた。
負荷条件が変わった独立セッションとして、保存済みの同じbinary・入力・測定規模でA/Aを再確認した。

**domのEL0 cyclesは今回は精度条件を通過したが、checkerのcyclesと両入力のwallは不合格。**
EL0命令数は両入力とも通過した。事前protocolに従い、wall/KPCとも3者比較には進んでいない。
前回のrawを残し、今回のデータと合算していない。回数の延長、外れ値の除外、gateの事後変更もない。

これは測定精度の確認であり、walker分割や新アクセサの性能評価ではない。
本体不採用、pointer同等性能・GC改善が未証明という判断は変わらない。

## 選択repo・候補clone・artifact

Storeは `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests` @
`32598cba146fa4dd7b6162b838630c90d865ab28`。
pointerは `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript` @
`8ac035a394c79e693a3a7d74cb170448503ee894`。TSGolint revisionは両方null。
repo_root / tsgolint_git_rev / typescript_go_git_revと保存identityの一致を確認した。

候補cloneは前回と同じflownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、
nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜5、store-pr-6-perf、
store-pr-7、store-pr-7-attach-parent-fix、store-pr-7-nodeseq-t10、store-redesign。
別介入を混ぜず、保存worktrees.txtに全パス・revisionを残した。

repoルートから `A = .cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/`。
今回のartifact setは [A/walker-codegen-recheck-01/](../../../../.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/walker-codegen-recheck-01/)。

| artifact / stage | status | 根拠・用途 |
| --- | --- | --- |
| 前回walker-codegen-experimentのwall/KPC/audit binaryと結果 | `current` | 同じcheckout identity。再ビルドせずwall/KPC全6 binaryとoverlay sourceのhashを照合 |
| 今回wall-aa-6、kpc/kpc-aa-6 | `current` | checker/dom各6件の対象Benchmark行。raw、benchstat、補助区間を保存 |
| 今回wall/KPCの3者比較 | `missing` | A/A gate不合格により未実行 |
| 今回pointer比較、新アクセサ、全phase、通常GC CPU/assist/scan/live | `missing` | 今回の範囲外で未測定 |
| 旧eval・旧Instruments | `stale` | 今回の精度や効果の証拠には使わない |
| 初期KPC v5のBenchmark stage | `unsupported` | 既存記録で対象Benchmark行なし。比較から除外 |

`current`はdirty treeの同一性を表さない。今回も保存baseline overlayで既存PropertyAccess候補を外した
binaryを使った。コンパイラsourceと生成walkerは変更せず、20入力の意味・訪問・アクセサ数監査と
AST/Binder/Compilerテストは前回の検証結果を利用した。今回これらのテストを再実行したとは扱わない。

## 再現・事前条件

Go1.26.0、Apple M1、darwin/arm64、CGO=1、GOMAXPROCS=8、GOMEMLIMIT=off。
checker/dom各10 bind × 6 rounds。A/Bは同じbaseline binaryで、roundごとに実行順を反転。
wallはGOGC=100、KPCはGOGC=off。両セッションを直列に実行し、ビルドを同時実行していない。
parseと強制GCはtimer外。KPCのwallを通常GC wallの代わりにしない。

wallは両入力のA/A補助95%区間が±1.5%内なら3者比較へ進む条件。
KPCは全3者の自己検証を新規processで各2回実施し、その後、両入力のEL0 instが±1%、
EL0 cyclesが±1.5%内なら3者比較へ進む条件。KPCの自己検証6回はすべて成功した。

条件と環境変更の理由は測定前のprotocol.jsonに保存。
実行はwall.py、および新規runtime `/private/tmp/binder-walker-codegen-kpc-recheck-01/run.py`。
元スクリプトは `tools/scripts/tsc/binder_walker_codegen_experiment.py`。
wall/KPCの各variantディレクトリは前回buildへの参照で、manifestと全fixture hashも照合済み。
再実行時も新規出力先を使い、既存rawを上書きしない。
各A/A pairで `benchstat a.txt b.txt` を実行。補助区間はround対応mean log-ratioのbootstrap95%区間。

## A/Aの結果

以下の差は同一binary間のばらつきであり、最適化の効果量ではない。

| 指標 | checkerの95%区間 | 判定 | domの95%区間 | 判定 |
| --- | ---: | --- | ---: | --- |
| 通常GC wall（±1.5%） | [+1.40%, +24.04%] | 不合格 | [−2.30%, +0.32%] | 不合格 |
| EL0 inst（±1%） | [−0.40%, +0.21%] | 通過 | [−0.37%, +0.07%] | 通過 |
| EL0 cycles（±1.5%） | [−4.29%, −0.61%] | 不合格 | [−1.27%, +0.42%] | 通過 |

fixed EL0+EL1のinstは両入力で通過。fixed cyclesはchecker [−4.82%, −0.20%]、
dom [−1.88%, +0.59%]で不合格。EL0とfixedを混同せず、全列をrawに保存した。

通常GCの中央値は次のとおり。

| 入力・同一binaryのラベル | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| checker A | 16,412,158.5 | 12,799,511.5 | 14,165 |
| checker B | 16,939,564 | 12,799,550 | 14,165 |
| dom A | 5,820,902 | 7,870,371 | 16,684 |
| dom B | 5,732,400 | 7,866,716.5 | 16,684 |

wallのbenchstatはcheckerで中央値比+3.21%（p=.041、n=6）、domで非有意（p=.394）。
同一binaryのcheckerで差が出ているため、この条件のwall差を実装差へ帰属することはできない。
checker Bには23,910,966 ns/opと18,833,467 ns/opのroundがあり、除外せず残した。
中央値比とmean log-ratio区間の中心は異なる。allocs中央値は同じで、B/op差をメモリ縮小とは扱わない。

## 負荷の観測と診断

1分load averageは準備前5.07、wall終了後4.91、全測定後のprocess確認時4.25だった。
平均負荷が低下していることと、各bind区間が十分安定していることは別である。

測定終了後の一回のprocess snapshotでは、mediaanalysisd 101.4%、WindowServer 45.5%、
Codex Renderer 25.5%、Codex Service 23.6%のCPU使用が観測された。
snapshotは全測定の後に取得したので、これを測定中のばらつきの原因と断定しない。
負荷の手掛かりとしてprocesses-after.txt、host-after-processes.jsonに保存した。

今回言えるのは「命令数の精度は両入力で成立し、cyclesはdomだけ成立、wallは未成立」まで。
スケジューリング、CPU状態、GC、他processのどれが主因かは未分離である。

Binderの広いhot pathは既存Instrumentsのwalk/helpers。generated walkerのcodegenはその一部であり、
今回のA/Aはhot pathへの新しい帰属を与えない。pprof・symbol syntheticは使用していない。
既存の割当増加要因であるsymbolIdx/flows列、symbolRefs、FlowNode 32→48 B、Symbol 96→104 B、
宣言Handle 8→16 Bは残り、今回も変更していない。

## 次の行動・受け入れ条件

今回のwall/KPC候補比較はmissingとして固定し、結果を見た追加roundは実施しない。
次の性能測定では、命令数だけの機構診断を独立に行えるprotocolを先に定める余地がある。
その場合もcyclesやwallの改善、pointer同等性、採用条件を満たしたとは扱わない。
wall/cyclesの判断には、負荷が落ち着いた区間で該当指標のA/Aが通る必要がある。

並行ベンチマークを追加せず進められる本作業は、構文writerの推移的監査、writer guardの検証、
直接getterへ渡すowner/shapeの契約確認。D/I/Bは元のwalkerを共通に保つ。
本体採用はwall有意3%以上短縮、inst/cycles低下、独立再現、8入力非悪化区間、
全回帰・全phase・通常GCの確認を要する。今回この条件を緩めていない。
