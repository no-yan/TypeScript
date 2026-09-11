# レビュー修正前後の測定（2026-09-11）

checkerでは修正後にEL0命令数が **1.24%**、EL0 cyclesが **2.09%**減った（benchstat各p=.002）。
domでは両指標ともbenchstatの有意差なし。通常GCでのbind時間・parse+bind時間にも有意差はなく、時間のA/A精度条件も未達。
この結果はレビュー修正の局所的な効果であり、Luna全改修の累積効果やpointer同等を証明するものではない。

## 対象・artifact

- 選択repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`。
- `typescript_go_git_rev`: `32598cba146fa4dd7b6162b838630c90d865ab28`、`tsgolint_git_rev`: null。
- normal = レビュー修正前P9相当、after = [レビュー修正版](accessor-review-fixes-20260911.md)。
- pointer候補: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、`8ac035a394c79e693a3a7d74cb170448503ee894`。今回未測定。
- その他候補clone: flownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr系、store-redesign。今回未使用。完全な一覧はartifactの`worktrees.txt`。
- artifact: 選択repo内の`.cursor/skills/verify-tsc/artifacts/20260911-accessor-review-measurement/`。

beforeは保存patchと保存済み生成ファイルから復元し、当時のsource SHAと全件一致させた。afterは現在checkoutと全件一致。
全バイナリとoverlayのSHAを実行後にも照合した。repo_rootと両revisionが選択checkoutに一致するため、今回のwall/KPC/parse+bind/GC probe artifactは **current**。
beforeが現在のdirty sourceと異なるのは意図した比較条件で、identityの`stale`とは区別する。
同一セッションのpointer比較は **missing**。対象Benchmark行なしの **unsupported** stageはない。
旧arity等の別sourceの数値を、この修正の基準値には使っていない。

## 条件と検証

Apple M1 / darwin arm64 / Go 1.26.0、GOMAXPROCS=8、GOMEMLIMIT=off。
wallとparse+bindはGOGC=100、KPCは既存手順のGOGC=off。監査hook・`-B`・noinline変更なし。
ビルド時の`-p=1`はビルドの並列数だけを制限する。
checker.tsとdom.generated.d.ts、それぞれ10 iterations × 6 rounds。各roundで条件順を反転する。
wall、parse+bind、KPCの順に実行し、認証後のKPCとparseが重ならないよう排他制御した。
A/Aと前後比較で計144 Benchmark行。増し測定なし。全rawとbenchstatは`runs/{wall,parse,kpc}-{aa,pairs}/`。
KPC自己検証は各バイナリ2つの新規プロセスで成功。
意味検証はレビュー修正版の20入力でP9／保存baseline双方と一致済み。測定中にAST/Binderソース変更なし。

A/Aはpaired log-ratio bootstrap 95%区間全体が所定範囲内なら通過する。
事前protocolどおり、精度不合格の指標も固定回数の探索比較を保存するが、採用根拠にはしない。

| A/A指標 | checker 95%区間 | dom 95%区間 | 条件・結果 |
|---|---:|---:|---|
| bind wall | −3.488〜+0.192% | −7.507〜+1.521% | ±1.5%、両方不合格 |
| parse+bind wall | −2.401〜+1.177% | −1.701〜+0.466% | ±1.5%、両方不合格 |
| EL0命令数 | −0.305〜+0.192% | −0.065〜+0.434% | ±1%、両方合格 |
| EL0 cycles | +0.031〜+1.265% | −0.537〜+1.226% | ±1.5%、両方合格 |

## bind時間・割当

値は6サンプルの中央値。変化率はrawの中央値から計算し、検定はbenchstatを使用。

| 入力 | ns/op 前→後 | 変化 | B/op 前→後 | allocs/op 前→後 |
|---|---:|---:|---:|---:|
| checker | 17,667,879 → 17,088,485.5 | −3.28%、p=.240 | 12,799,470 → 12,799,444 | 14,165 → 14,165 |
| dom | 6,211,575 → 6,288,898 | +1.24%、p=.180 | 7,868,537 → 7,868,551 | 16,684 → 16,684 |

時間・割当とも有意差なし。wall A/Aも不合格なので、checkerが3.28%高速になったとは結論しない。

## KPC

| 入力 | EL0 inst/op 前→後 | 変化・p | EL0 cycles/op 前→後 | 変化・p |
|---|---:|---|---:|---|
| checker | 168,435,193.5 → 166,346,666 | **−1.24%、.002** | 81,435,969.5 → 79,735,499 | **−2.09%、.002** |
| dom | 69,442,096 → 69,453,670 | +0.017%、.937 | 30,258,438 → 30,374,695 | +0.38%、.132 |

checkerのpaired区間は命令数−1.360〜−0.835%、cycles−2.678〜−1.471%。
補助指標のL1D missはchecker −2.05%（p=.002）。
dom cyclesのpaired区間は+0.033〜+0.703%と小さな増加を示すが、benchstatでは有意差なし。
KPC harnessのns/opは通常GCのbind wallと測定条件が異なるため、上の時間表と混ぜない。

## parse+bindとGC probe

| 入力 | ns/op 前→後 | 変化・p | B/op 前→後 | allocs/op 前→後 |
|---|---:|---|---:|---:|
| checker | 42,351,870.5 → 42,168,785.5 | −0.43%、.240 | 32,210,476 → 32,210,463 | 15,183 → 15,183 |
| dom | 17,669,447.5 → 17,496,391.5 | −0.98%、.589 | 18,758,546 → 17,927,175 | 18,539 → 18,536 |

上表は全項目でbenchstat有意差なし。parse+bind A/A自体の割当にもばらつきがあり、domのB/op中央値差を構造的な割当削減と解釈しない。

GC CPU/assistはparse+bindループ前後のruntime/metrics差分。Go runtimeの推定CPU値であり、OSのCPU時間と直接比較しない。
live/scanは計測タイマー停止後にGCを実行して採取し、最後のSourceFileはKeepAliveする。
ただしStoreSetはatomic.PointerでStoreを保持し、このprobeではUnregisterStoreしていない。
従って過去の登録済みStore・runtime・poolを含む**プロセス全体の値**で、単一ASTのfootprintではない。

| GC補助値 | checker 前→後 | dom 前→後 |
|---|---:|---:|
| GC推定CPU ns/op | 7,645,100.5 → 8,059,550.5（+5.42%、p=.818） | 3,810,376.5 → 3,980,640.5（+4.47%、p=.699） |
| GC assist ns/op | 289,423 → 238,200（−17.70%、p=.937） | 34,504 → 82,864.5（+140.16%、p=.004） |
| live heap B | 315,367,344 → 293,334,280（−6.99%、p=.041） | 158,622,728 → 158,631,488（p=.394） |
| scannable heap B | 103,600,464 → 103,599,016（p=.509） | 62,036,612 → 62,045,408（p=.589） |

GC probeは探索値であり、事前のGC精度gateはない。A/Aでもdom assistのpaired区間は−74.99〜−11.37%、checker assistは−81.12〜+414.95%に広がった。
このためdom assist増加やchecker live減少を実装の因果的な効果として採用判断に使わない。scan量の改善は確認できなかった。
制御したライフサイクルでのGC評価・noscan優位の判定は依然未完了。

## 診断・次のアクション

この組合せ修正はcheckerの命令数・cyclesを小さく削減した。未使用list解決と再取得を除去する方針を支持するが、各修正の寄与は分離していない。
OptionalChain親frame増加も含む**差し引き後の結果**であり、個別変更が全て有益だという証拠ではない。
domに同様の改善がないこと、通常時間では検出できていないことを併記する。

既存調査の広いhot pathはwalk/helpersで、今回のlist/child再解決はその一部。現行profileを新規取得したわけではない。
既知のallocation driverであるsymbolIdx/flows列、symbolRefs、FlowNode/Symbol/Handle大型化は本修正では残る。

次はpointer・Luna前の基準版・現行版を同一セッションで固定して累積効果を比較する。
GCは登録Storeのライフサイクルと保持する入力数を明示した独立protocolで測る。今回の不合格A/Aを通すための追加roundは行わない。
V3の状態は「レビュー修正の意味・機械語・固定規模の性能測定済み／全面採用・pointer同等・GC改善未証明」。

## 再現

実行driverは`tools/scripts/tsc/binder_review_measurement.py`。復元済みbefore・frozen sources・全build commandはartifactに保存。
`runs/`に固定入力manifest、binary SHA、測定driverと依存driver、PMU設定、raw、benchstat、6ラウンドのorderを保存した。
バイナリ実体は`wall-normal/bench.test`、`wall-after/bench.test`、`kpc-normal/bench.test`、`kpc-after/bench.test`。
parse測定は対応するwallバイナリ内の`BenchmarkParseBindInvestigation`を使う。
再実行は既存rawを上書きせず、別runtimeディレクトリへ必要なバイナリとfixtureを複製して行う。
