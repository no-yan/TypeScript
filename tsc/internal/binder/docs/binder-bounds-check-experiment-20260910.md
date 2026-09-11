# -gcflags=-Bによる境界チェックの命令数対照（2026-09-10）

## 問題・結論

ユーザーの「Bounds Checkが命令数を増やしているのではないか。-gcflags=-Bで何%変わるか」に対し、
既存の3つのwalkerと境界チェックの適用範囲3条件、計9条件を同一セッションで測定した。

**元のwalkerは、指定どおりの`-gcflags=-B`でchecker −4.65%、dom −3.57%の命令数減。
Binderとastの両方へ適用すると−6.72% / −4.81%。一方、CALL化による追加命令は消えなかった。**
Goが挿入する境界チェックには数%の寄与があるが、CALL化の追加費用やStoreの再解決全体をそれだけで説明できない。
手書きのslot長検査はこのフラグでは消えず、この実験の削減率に含まれない。

これはビルドフラグによる診断実験であり、本体への採用・source変更はしていない。
通常GC wall、pointerとの同条件比較、GC高速化は今回測定していない。

## 選択repo・候補clone・artifact

Store: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests` @
`32598cba146fa4dd7b6162b838630c90d865ab28`。
pointer対照: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript` @
`8ac035a394c79e693a3a7d74cb170448503ee894`。TSGolint revisionは両方null。
repo_root / tsgolint_git_rev / typescript_go_git_revと保存identityの一致を確認した。

候補cloneはflownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、
opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜5、store-pr-6-perf、store-pr-7、
store-pr-7-attach-parent-fix、store-pr-7-nodeseq-t10、store-redesign。別介入を含むため混ぜない。
全パス・revisionは保存worktrees.txtにある。

repoルートから `A = .cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/`。
今回のartifact setは [A/bounds-check-experiment/](../../../../.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/bounds-check-experiment/)。

| artifact / stage | status | 扱い |
| --- | --- | --- |
| 元のwalker-codegen-experimentとrecheck-02 | `current` | 実験前に既存結果とidentityを確認。旧rawを今回と合算しない |
| 今回のbuild、20入力監査、機械語、KPC A/A・9条件比較 | `current` | 同じcheckout identity。入力・variantごと6件のBenchmark行、raw、18pairのbenchstatを保存 |
| 今回の通常GC wall、pointer比較、全phase、GC CPU/assist/scan/live | `missing` | 未測定で効果を主張しない |
| 旧eval・旧Instruments | `stale` | 今回の効果の帰属に使わない |
| 初期KPC v5のBenchmark stage | `unsupported` | 既存記録で対象行なし。比較から除外 |

`current`はdirty sourceの一致を保証しない。今回、元build時のtracked Go差分と全8件のuntracked Go hashも
現在と一致することをsource-parity.jsonへ記録した。normalは検証済みbinaryを再利用し、
6つのflag付き条件を同じoverlay・sourceから作った。保存baseline overlayで既存PropertyAccess候補を除外した。

## フラグの範囲と比較条件

`go help build`によると、patternなしの`-gcflags`はコマンドラインで指定したpackageにだけ適用される。
今回のbuild対象は`./internal/binder`なので、文字どおりの`-gcflags=-B`はBinderに適用される。
astからinlineされたコードのGo境界チェックはBinderのコード生成で消えるが、ast内のCALL先には残る。
適用範囲を混同しないため、次の3条件を作った。

| scope名 | 追加フラグ |
| --- | --- |
| normal | なし |
| binder_b | `-gcflags=-B` |
| binder_ast_b | `-gcflags=-B -gcflags=github.com/microsoft/TypeScript/tsc/internal/ast=-B` |

runtime・標準ライブラリ・他package全体には`-B`を付けていない。Binder scopeには同packageの計測harnessも含む。
baseline / split / split_callは前回と同じ。splitは167 caseを10関数へ分割、split_callは同じ分割で
walker内のChildRefだけ本体が同じnoinline関数に変更した版。親引数・値memo・走査順・2passは変えていない。

Go1.26.0 / Apple M1 / darwin-arm64 / CGO=1 / GOMAXPROCS=8 / GOMEMLIMIT=off / GOGC=off。
checker/dom各10 bind×6 rounds。9条件の順序は事前固定し、3条件ずつの循環移動と逆順で位置を釣り合わせた。
KPC測定前に全ビルドと監査を終了。parseと強制GCはtimer外、EL0とfixed EL0+EL1は別列。

主指標はユーザー指定のEL0 inst/op。今回の独立protocolでは、両入力の命令数A/A区間が±1%内なら比較を進める。
cyclesは副指標として±1.5%を別判定する。結果を見たgate変更・round追加・外れ値除外はしていない。
全9 binaryを新規processで各2回、KPC自己検証して成功した。

初回の自動承認審査がタイムアウトしたが、許可された一度の再試行で起動した。
タイムアウト時点ではKPC測定は始まっておらず、測定値には含まれない。

## 意味と機械語の検証

6つのflag付き条件で、8実入力＋9境界入力＋3追加入力、計20入力を監査した。
構文・診断・Symbol・node-Symbol対応・CFG・訪問順・操作数・全アクセサ呼出数が保存baselineと一致した。
これは監査した入力の一致であり、unchecked buildの一般的な正しさや全回帰の完了を意味しない。

| 機械語の静的観測 | normal | binder_b | binder_ast_b |
| --- | ---: | ---: | ---: |
| baseline walkerのGo境界panic経路 | 582 | 0 | 0 |
| baseline walkerの手書きpanic経路 | 291 | 291 | 291 |
| baseline walkerの命令行数 | 10,292 | 8,256 | 8,256 |
| split walkerの命令行数 | 11,104 | 9,068 | 9,068 |
| split_callのCALL先ChildRefのGo境界panic経路 | 2 | 2 | 0 |
| 同CALL先の手書きpanic経路 | 1 | 1 | 1 |

Go1.26のこのbinaryでは自動チェックの失敗先は`runtime.panicBounds`。
表はその静的CALL箇所であり、正常な入力でpanicが実行された回数ではない。
分岐と比較が消えたことも機械語で確認した。静的命令行数を動的な削減率に代用していない。

baseline/splitは全scopeでwalker内ChildRefがinlineのまま、split_callは全scopeで291箇所がCALL。
`if slot >= uint32(n.childLen) { panic(...) }`というsource上の検査は`-B`で消えていない。
証拠はcodegen.json、*.asm.txt、*-buildinfo.txt、各build identityとcompiler_flagsにある。

## A/Aと命令数の削減率

| 指標 | checkerの補助95%区間 | domの補助95%区間 |
| --- | ---: | ---: |
| EL0 inst（許容±1%） | [−0.03%, +0.58%]、通過 | [−0.10%, +0.38%]、通過 |
| EL0 cycles（許容±1.5%） | [−0.53%, +3.31%]、不合格 | [−0.04%, +0.87%]、通過 |

事前条件に従い命令数の比較を実施し、cyclesの全体改善は判定しない。
各行は、そのwalkerのnormalに対するEL0 inst/op中央値比。以下の削減はすべてbenchstat p=.002、n=6。

| walker | checker: binder_b | dom: binder_b | checker: binder_ast_b | dom: binder_ast_b |
| --- | ---: | ---: | ---: | ---: |
| baseline | −4.65% | −3.57% | −6.72% | −4.81% |
| split | −4.67% | −3.54% | −6.68% | −4.53% |
| split_call | −4.19% | −2.88% | −6.44% | −4.45% |

baselineのbinder_bは約8.60M / 2.78M inst/bindを削減した。
binder_ast_bはnormalから約12.44M / 3.75M inst/bindを削減した。
binder_bからastにも適用した追加効果は−2.17% / −1.29%（各p=.002）。削減率の分母は比較ごとに異なり、単純加算しない。

baselineのbinder_b対normalの補助区間はchecker [−4.74%, −4.33%]、dom [−3.85%, −3.25%]。
binder_ast_b対normalは [−6.93%, −6.49%] / [−4.91%, −4.59%]。
補助区間はround対応mean log-ratioのbootstrap20,000回、表は中央値比なので中心が異なる。
全18pairをbenchstatで比較した。pは多重比較補正なしの探索値で、採用の独立確認ではない。

## 分割・CALL化による増減は小さくなったか

同一セッション・同一scope内の比較。旧セッションの値と直接差し引いていない。

| scope | split / baseline: checker / p | split / baseline: dom / p | split_call / split: checker | split_call / split: dom |
| --- | ---: | ---: | ---: | ---: |
| normal | +0.05% / .818 | −0.02% / .699 | +1.03% | +0.96% |
| binder_b | +0.03% / .699 | +0.00% / .589 | +1.54% | +1.66% |
| binder_ast_b | +0.09% / .485 | +0.27% / .041 | +1.30% | +1.05% |

CALL化の増加は全scope・両入力でp=.002。両packageのGo境界チェックを消しても追加命令が残る。
フラグ間の差の有意性を、この表の点推定や各pairのpだけから判定したわけではない。
「CALL化の増分が境界チェックを消せばなくなる」という説明は支持されない。

binder_bだけの対照にはCALL先のGoチェックが残るため、それを純粋なCALLコストとして扱わない。
binder_ast_bでも引数移送・spill・周囲のcodegen・コード配置・手書き検査を含む収支である。
分割による改善も得られていない。domの小さい名目有意増を一般的な効果へ拡張しない。

## ns/op・割り当て・残る費用

以下はGOGC=offのKPC harnessの中央値で、通常GC wallではない。今回の結論は命令数に限定する。

| 入力・baselineのscope | EL0 inst/op | KPC ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| checker normal | 185,168,668 | 44,110,772.5 | 12,799,459 | 14,165 |
| checker binder_b | 176,563,685.5 | 43,223,298 | 12,799,433 | 14,165 |
| checker binder_ast_b | 172,723,939.5 | 43,653,043.5 | 12,799,446 | 14,165 |
| dom normal | 77,876,318.5 | 16,433,685 | 7,866,716.5 | 16,684 |
| dom binder_b | 75,096,147.5 | 16,140,641.5 | 7,862,998 | 16,683.5 |
| dom binder_ast_b | 74,129,186 | 15,993,475 | 7,868,550 | 16,684 |

全9条件×2入力のns/op・B/op・allocs/op・counter中央値は
[metrics.md](../../../../.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/bounds-check-experiment/metrics.md)とmedians.jsonに保存。
割り当ての小差を表現縮小やGC高速化の証拠にはしない。

広いhot pathは既存Instrumentsのwalk/helpersで、今回のフラグはその内部のgetterだけでなくBinder/ast内の
他の配列・sliceアクセスにも適用される。削減をすべてStoreのchild解決へ帰属できない。
node-wide symbolIdx/flows列、symbolRefs、FlowNode 32→48 B、Symbol 96→104 B、宣言Handle 8→16 Bの割当増は残る。
pprof・symbol syntheticは使用していない。

## 診断・提案修正・受け入れ条件

Goの自動境界チェックを安全に償却・除去できる設計には、今回の対象範囲で数%の命令削減を検討する根拠がある。
ただし`-B`の差は周辺codegenも変わる介入の正味の差で、チェック単体の費用や厳密な削減上限ではない。
この値を直接getter一経路の予算へ全額計上したり、命令数の削減率をwallの短縮率へ変換したりしない。
pointerにも境界チェックがあり、本測定だけでStore固有の増分やpointer同等性を判定できない。

次は通常のbounds-check有効buildを基準に、構文の安定区間と既知shapeから安全にチェックを償却できるかを調べる。
直接getterは手書きslot検査とGoの配列検査を分けて機械語で確認し、Slot/Span・Go借用は直接getterとの正味比較にする。
構文writer・owner/shape契約の監査を先に行い、元のwalker・論理read・2passを維持する。

実装の採用条件は通常buildでの意味・全回帰、wall有意3%以上、inst/cycles低下、独立再現、8入力非悪化区間、
全phase・通常GCの確認。`-B`自体を本体へ採用しない。

## 再現入口

`tools/scripts/tsc/binder_bounds_experiment.py`のprepare / runtime / run。
保存prepare.py、protocol.json、各build_command、overlay・source・binary hashで条件を追跡できる。
新規artifact rootへ既存baseline入力一式を用意し、既存rawを上書きしない。
runtimeのrun.py、manifest.json、instructions.json、source.jsonも保存済み。
比較は各pairの`benchstat <old>.txt <new>.txt`。全条件は同じ固定ラウンド内で測り、旧rawと合算していない。
