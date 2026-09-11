# generated walkerの分割とChildRef CALL対照（2026-09-10）

追記: 並行ベンチマーク停止後の[独立A/A再確認](walker-codegen-recheck-20260910.md)を別artifactへ保存した。
命令数は両入力、EL0 cyclesはdomが通過したが、checkerのcyclesと両入力のwallは不合格。
さらに[再計測02](walker-codegen-recheck-02-20260910.md)でKPC精度が通過し、3者比較を完了した。
分割はcycles増、CALL化は同じ分割比で約1%の命令増。以下は初回セッションの記録であり、
新しいrawと合算していない。初回のKPC候補比較はmissingのまま保持する。

## 問題・結論

親ref引数の既存実験では、generated walkerがGoのbig caller判定を受け、
ChildRefがinlineからCALLへ変わった。親再利用の利益と、この副作用を混同しないため、
親引数を追加せずwalkerの分割だけを調べた。

**分割による高速化は未証明。本体には採用しない。**
20入力で意味・訪問順・論理アクセサ呼出数が一致し、意図したinline/CALLの対照は成立した。
通常GC wallは全pairで非有意。KPCは自己検証には成功したが、今回のA/Aでcyclesの精度条件を
満たさず、事前protocolどおり3者比較を実行しなかった。動的命令数の削減・増加は未判定である。

この固定規模の探索はここで終了する。分割幅の調整や有意になるまでの延長はしない。
分割をアクセサ設計の必須条件にせず、元のwalkerで直接getterを評価できる構成を維持する。
これは分割一般の否定でも、分割の費用がゼロという証明でもない。

## 選択repo・候補clone・artifact

Storeは `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests` @
`32598cba146fa4dd7b6162b838630c90d865ab28`。
pointer対照は `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript` @
`8ac035a394c79e693a3a7d74cb170448503ee894`。TSGolint revisionは両者ともnull。
両checkoutと保存identityのrepo_root / tsgolint_git_rev / typescript_go_git_revを照合した。

候補cloneはflownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、
nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜5、
store-pr-6-perf、store-pr-7、store-pr-7-attach-parent-fix、store-pr-7-nodeseq-t10、store-redesign。
他の介入を含むため今回の対照に混ぜない。全パスとrevisionは保存worktrees.txtにある。

artifact setはrepoルートから
`A = .cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/`、
今回の結果は [A/walker-codegen-experiment/](../../../../.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/walker-codegen-experiment/)。

| artifact / stage | status | 扱い |
| --- | --- | --- |
| A/paired-8-20、既存parent-argument-experiment | `current` | 実験前にidentityと既存結果を確認。今回の3者比較とは別セッション |
| 今回のwall-aa-6、wall-6 | `current` | checker/domの対象Benchmark行が各6件。rawと全pairのbenchstatを保存 |
| 今回のkpc/kpc-aa-6 | `current` | 対象Benchmark行が各6件。精度不足の結果も保存 |
| 今回のKPC 3者比較 | `missing` | A/A gate不合格により未実行。対象行なしの既存stageではない |
| 今回のpointer同時比較、Instruments、全phase、通常GC CPU/assist/scan/live | `missing` | 比較・効果量を主張しない |
| 旧eval・旧Instruments | `stale` | 広い経路の背景に限る。今回の効果の帰属には使わない |
| 初期KPC v5のBenchmark stage | `unsupported` | 既存記録で対象Benchmark行なし。今回も性能比較から除外 |

`current`とdirty variantの同一性は別である。保存baseline Binderのoverlayを使い、
dirtyなPropertyAccess候補を今回の介入から外した。本体・generator本体には変更していない。
identity-check、dirty.patch、status、未追跡Go hash、overlay/source/binary hash、fixture hash、
protocol、実行順、raw、benchstat、compiler診断、機械語、監査結果を保存した。

## 仮説・提案修正と対照

仮説は「walkerを小さな関数に分ければinline条件を維持しやすくなるが、分割自身にも費用がある」。
Go 1.26.0のローカルcompiler実装 `cmd/compile/internal/inline/inl.go:58` で、
big functionが5000 IR node、そこへのinline上限が20であることを確認した。
inline costとIR node数は別物なので、helperのcostだけからbig callerかを判定しない。

| variant | 変更 |
| --- | --- |
| baseline | 保存baseline Binderと元のgenerated walker |
| split | 167個のcaseを、1群最大48個のbind呼出しを目安に10関数へ分割。case本体・case順・default fallbackを維持 |
| split_call | splitと同じ分割。walker内のChildRefだけ、元と同じ本体のnoinline関数へ置換 |

親引数、直接getter、構文読取契約、Slot/Span、値のmemo、2pass変更は混ぜていない。
split対baselineは分割全体の収支、split_call対splitはwalker内のCALL化を含むcodegen介入の収支である。
後者には引数移送・spill・周囲のcodegen・コード配置も含まれ、裸のCALL命令の費用とは扱わない。
過去の親引数controlの追加命令を、今回の差ですべて説明できるとも置かない。

分割はpointer側にも適用し得るため、Store固有の優位性を示す主候補ではなく機構の対照である。

## 意味・機械語の検証

3者それぞれで8実入力＋9境界入力＋3追加入力、計20入力を保存baselineと照合した。
構文、診断、Symbol、node-Symbol対応、CFG、訪問順、操作数、全アクセサ呼出数が一致した。
AST/Binder/Compilerの単体テストも3者すべて成功した。全コンパイラ回帰試験やpointerとの完全な意味同値は未検証。

| walkerの静的観測 | baseline | split | split_call |
| --- | ---: | ---: | ---: |
| objdumpの命令行数（入口＋全分割helper） | 10,292 | 11,104 | 4,104 |
| ChildRef系CALL箇所 | 0 | 0 | 291 |
| 分割helperへのCALL箇所 | 0 | 10 | 10 |
| 入口frame | 48 B | 32 B | 32 B |
| 分割helperのframe | — | 各48 B | 各48 B |

wall/KPC binaryで同じ結果。splitの入口は再帰中も残るため、分割経路は入口32 B＋helper48 Bであり、
baselineの48 Bと等費用ではない。命令行数はwalker部分の静的な数であり、動的命令数やbinary全体の大きさではない。
split_callの専用ChildRef本体もこのwalker行数の外にある。

元のwalkerでは既にChildRefがinlineされている。splitはそこをさらにinline化した変更ではない。
分割後のCALL対照とともに、今後のアクセサ変更で周辺codegenを確認するための観測基準を得た。
証拠はcodegen.json、wall/kpc-*.asm.txt、*-compiler.txt、*-inline-json/。

## 通常GC wallと割り当て

Go1.26.0 / Apple M1 / darwin-arm64 / CGO=1 / GOMAXPROCS=8 / GOMEMLIMIT=off / GOGC=100。
checker/dom各10 bind × 6 rounds。3者の6通りの順序を各1回。parseと強制GCはtimer外。
同じbaseline binaryによるA/Aを先行させた。補助区間はround対応mean log-ratioのbootstrap95%区間。

A/Aのwall区間はchecker [−1.22%, +1.42%]、dom [−0.17%, +1.28%]で、±1.5%条件を通過。
しかし、その後の3者比較ではばらつきが拡大した。先行A/Aの成功を後続測定の精度保証にしない。
外れたroundの除去や取り直しはしていない。ばらつきの原因は今回のデータでは確定していない。

| 入力・variant | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| checker baseline | 17,753,437.5 | 12,799,550 | 14,165 |
| checker split | 17,056,389.5 | 12,799,511.5 | 14,165 |
| checker split_call | 17,910,881.5 | 12,799,525 | 14,165 |
| dom baseline | 5,945,681.5 | 7,868,486 | 16,684 |
| dom split | 5,986,848 | 7,866,691 | 16,684 |
| dom split_call | 6,176,112.5 | 7,868,499 | 16,684 |

中央値。全pairを保存rawからbenchstatで比較した。

| wall比較 | checker 中央値比 / p | dom 中央値比 / p |
| --- | ---: | ---: |
| split / baseline | −3.93% / .937 | +0.69% / .818 |
| split_call / split | +5.01% / .310 | +3.16% / .180 |
| split_call / baseline | +0.89% / 1.000 | +3.88% / .394 |

すべて非有意。split / baselineの補助95%区間はchecker [−15.87%, +8.04%]、dom [−6.44%, +1.38%]。
split_call / splitのdom補助区間だけは [ +0.90%, +4.96% ]だが、benchstat p=.180を置き換えない。
区間はmean log-ratio、表の比は中央値比なので中心が異なる。
allocs中央値は不変。B/opの小差は表現縮小やGC高速化の証拠にしない。

## KPCの自己検証と停止条件

GOGC=off、今回の3者を新規processで各2回自己検証し、すべて成功。
非対話sudoは認証不足で実行できず、macOS標準の管理者認証で準備済みスクリプトを起動した。
新規OS thread上のカウンタ単調性・仕事量への応答・GC境界を確認した。

続くbaseline A/Aも10 bind × 6 rounds。以下はA/Aの補助95%区間であり、候補の性能差ではない。

| 入力 | EL0 inst区間（許容±1%） | EL0 cycles区間（許容±1.5%） |
| --- | ---: | ---: |
| checker | [−0.51%, +0.26%]、通過 | [−2.51%, +2.07%]、不合格 |
| dom | [−0.08%, +0.39%]、通過 | [−3.41%, +2.55%]、不合格 |

fixed EL0+EL1も別列に保存し、EL0と混同しない。fixedもinstは通過、cyclesは不合格だった。
今回の事前protocolはEL0 instとcyclesの両方を3者比較のgateにしたため、KPC候補比較はmissing。
**命令数の精度は通ったが、候補の命令数は測っていない。** 機械語の短縮をその代わりにはしない。
有意になるまでの延長や、失敗を見た後のgate変更はしていない。

## hot path・割当要因・到達予算

広いhot pathは既存Instrumentsのwalk/helpersであり、今回の狭い対象はgenerated walkerのcodegenである。
walker内ChildRefはcheckerで全558,230回中189,849回、domで全196,338回中74,321回。
論理アクセサ呼出しは今回1回も減らしていない。

既存の割当増加要因はnode-wide symbolIdx/flows列、symbolRefs、FlowNodeの32→48 B化、
Symbolの96→104 B化、宣言Handleの8→16 B化で、今回も残る。symbol syntheticは使用していない。

既存pointer対Storeの同条件比較から必要なwall短縮はchecker34.65%、dom28.51%。
今回の別セッションのStore時間を旧pointer時間へ直接割って、到達率を更新しない。
本実験では削減可能な動的命令数もwall利益も確定せず、その予算を埋めたとは言えない。
予算と広い候補範囲は[アクセサ設計v2](binder-slot-accessor-design-20260910.md)を参照する。

## 再現・受け入れ条件・次の行動

実験生成・監査・実行は `tools/scripts/tsc/binder_walker_codegen_experiment.py`。
保存prepare.pyとprotocol.jsonが今回の仕様。prepare / wall / runtime / kpcの順で実行した。
prepareは既存baseline、wall/KPC/audit overlay、20入力manifestを含むAを入力にする。
再実行は必要な入力一式を持つ新規artifact rootを用意し、保存結果を上書きしない。
wallは各wall-variant/bench.test、KPCは各kpc-variant/bench.testを使い、fixtureとbinary hashを照合する。
KPC runtimeのsource.jsonは保存artifactを示す。kpc/run.py等は今回のruntime実行スクリプトの写しである。
raw比較は各pair内で `benchstat baseline.txt split.txt` 等。今回の分析結果も同じ場所に保存済み。

本体採用には、wall有意3%以上短縮、inst/cycles低下、独立再現、8入力非悪化区間、全回帰、
parse/bind/parse+bindと通常GCの確認が必要。今回は満たさず、pointer同等・GC改善も未証明。

次は構文writerの推移的監査とwriter guardの動的検証、直接getterへ渡すowner/shapeの契約確認を進める。
最初のD/I/B対照は元のwalkerを共通に保ち、同じParameter経路の直接getter・整数位置保持・Go借用を比較する。
child readとlist spanは別介入にし、取得値・2pass・判定・訪問順を維持する。
比較測定前に測定条件を整え、次の独立protocolでは命令数とcyclesの可測性を別々に判定する。
命令数だけを診断できる条件を先に固定すれば、cycles精度不足を命令削減の有無と混同せずに進められる。
今回のKPC比較を実施済みにしたり、今回のgateを事後変更したりはしない。
