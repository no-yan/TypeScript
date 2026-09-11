# walker再計測02：KPC精度通過と3者比較（2026-09-10）

## 問題・結論

ユーザーの「再計測」に従い、前回と同じbinary・入力・条件で独立セッションを実施した。
**今回はKPCのA/Aが両入力で通過し、固定6ラウンドの3者比較まで完了した。**
通常GC wallのA/Aは不合格のため、wallの3者比較は実行していない。

分割版はbaselineより命令数が有意に減らず、cyclesはchecker +1.33%、dom +2.18%だった。
同じ分割でwalkerのChildRefをCALL化すると、inline版より命令数が+1.00% / +1.07%増えた。
CALL化の追加費用を確認できたが、今回の分割は改善策になっていない。本体不採用を維持する。
元のwalkerは既にChildRefをinlineしているため、CALLを消すこと自体を現在のbaselineからの改善余地には数えない。

## 選択repo・候補clone・artifact

Storeは `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests` @
`32598cba146fa4dd7b6162b838630c90d865ab28`。
pointerは `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript` @
`8ac035a394c79e693a3a7d74cb170448503ee894`。TSGolint revisionは両者ともnull。
保存identityのrepo_root / tsgolint_git_rev / typescript_go_git_revを選択checkoutと照合した。

候補cloneは前回と同じflownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、
nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜5、store-pr-6-perf、
store-pr-7、store-pr-7-attach-parent-fix、store-pr-7-nodeseq-t10、store-redesign。
別介入を含むため比較に混ぜず、全パス・revisionは保存worktrees.txtに残した。

repoルートから `A = .cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/`。
今回のartifact setは [A/walker-codegen-recheck-02/](../../../../.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/walker-codegen-recheck-02/)。

| artifact / stage | status | 扱い |
| --- | --- | --- |
| 元のwalker-codegen-experimentのbinary・意味監査・機械語 | `current` | 同一identity。wall/KPC全6 binary、overlay source、実行scriptのhashを照合 |
| 前回recheck-01のA/A | `current` | 実験前に確認。今回と合算しない |
| 今回wall-aa-6、kpc/kpc-aa-6、kpc/kpc-6 | `current` | 対象Benchmark行が入力・variantごとに6件。raw、benchstat、区間、順序を保存 |
| 今回wallの3者比較 | `missing` | wall A/A不合格により未実行 |
| 今回pointer比較、Instruments、全phase、通常GC CPU/assist/scan/live | `missing` | 効果を主張しない |
| 旧eval・旧Instruments | `stale` | 今回の費用帰属には使わない |
| 初期KPC v5のBenchmark stage | `unsupported` | 既存記録で対象Benchmark行なし。今回も比較から除外 |

`current`とdirty treeの一致は別である。既存PropertyAccess候補を外した保存baseline overlayの
binaryを再利用し、コンパイラsourceを変更・再ビルドしていない。
20入力の構文・診断・Symbol・CFG・訪問順・全アクセサ数の一致とAST/Binder/Compilerテストは、
同じbinaryを作成した前回の検証を利用した。今回新たに全意味監査を回したとは扱わない。

## 条件・再現

Go1.26.0、Apple M1、darwin/arm64、CGO=1、GOMAXPROCS=8、GOMEMLIMIT=off。
checker/dom各10 bind × 6 rounds。wallはGOGC=100、KPCはGOGC=offの独立セッション。
A/Aは同じbaseline binaryで交互順、3者比較は6通りの順序を各1回。
parseと強制GCはtimer外。KPCのwallを通常GC wallへ代用しない。

判定は前回どおり、wall A/Aの95%区間が両入力で±1.5%以内、KPC A/AのEL0 instが±1%、
EL0 cyclesが±1.5%以内。対応するgateを通過した場合だけ、そのセッションの3者比較へ進む。
測定前のprotocol.jsonへ保存し、結果を見た延長・外れ値除外・gate変更はしていない。

一時ディレクトリの旧測定データが消えていたため、保存artifactのfixture・event config・binary・scriptから
`/private/tmp/binder-walker-codegen-kpc-recheck-02/`を復元した。fixtureとevent/binary内容は保持し、repo root用の
一時ディレクトリを再作成した。runtime-smoke.txtは初期化確認のみで、性能値や意味監査ではない。
復元内容はkpc/source.json、manifest、各hashで追跡できる。

wall.pyと保存kpc/run.pyが実行入口。測定部分は既存の
`tools/scripts/tsc/binder_walker_codegen_experiment.py`と同じで、再実行には新規出力先を使う。
全pairをrawからbenchstatで比較。補助区間はround対応mean log-ratioのbootstrap95%区間。
以下のp値はbenchstatの名目値で、多重比較補正なしの探索結果。独立確認はまだない。

## A/Aの精度

KPCの自己検証は新規processで3者各2回、全6回成功した。

| 指標 | checkerの95%区間 | 判定 | domの95%区間 | 判定 |
| --- | ---: | --- | ---: | --- |
| 通常GC wall（±1.5%） | [−1.30%, +2.53%] | 不合格 | [−4.31%, +1.44%] | 不合格 |
| EL0 inst（±1%） | [−0.08%, +0.37%] | 通過 | [−0.09%, +0.11%] | 通過 |
| EL0 cycles（±1.5%） | [−0.22%, +0.80%] | 通過 | [−0.33%, +0.32%] | 通過 |

fixed EL0+EL1のinst/cyclesも両入力で通過した。全列をrawへ保存し、EL0とは区別する。
準備前と測定終了後の1分load averageは16.13 / 2.82だったが、平均負荷だけで精度を判断していない。
今回は新たなprocess snapshotによる原因調査はしておらず、前回のmediaanalysisd観測を今回の原因にはしない。

## KPCの3者比較

baselineは元のwalker、splitは167 caseをcase本体・順序を保って10関数へ分割した版、
split_callは同じ分割でwalker内ChildRefだけ元と同じ本体のnoinline関数へ置き換えた版。
親引数、直接getter、構文読取契約、Slot/Span、値のmemo、2pass変更は混ぜていない。

GOGC=off、EL0、中央値。

| 入力・variant | inst/op | cycles/op |
| --- | ---: | ---: |
| checker baseline | 185,561,051.5 | 87,822,754.5 |
| checker split | 185,156,082.5 | 88,989,621 |
| checker split_call | 187,005,319.5 | 89,828,658.5 |
| dom baseline | 77,850,541 | 32,832,841 |
| dom split | 77,811,171.5 | 33,547,167.5 |
| dom split_call | 78,647,367 | 33,834,042.5 |

| 比較 | checker inst / p | dom inst / p | checker cycles / p | dom cycles / p |
| --- | ---: | ---: | ---: | ---: |
| split / baseline | −0.22% / .394 | −0.05% / .937 | +1.33% / .015 | +2.18% / .041 |
| split_call / split | +1.00% / .002 | +1.07% / .002 | +0.94% / .310 | +0.86% / .394 |
| split_call / baseline | +0.78% / .002 | +1.02% / .002 | +2.28% / .002 | +3.05% / .009 |

split / baselineのcycles補助区間はchecker [+1.03%, +2.51%]、dom [+1.10%, +2.69%]。
同pairのinst区間は [−0.29%, +0.19%] / [−0.08%, +0.29%]で、命令数の減少は未確認。
split_call / splitのinst区間は [+0.90%, +1.11%] / [+0.91%, +1.17%]。
同pairのcyclesは補助区間が正でもbenchstatでは非有意なので、有意なcycles増とは扱わない。
区間はmean log-ratio、表は中央値比で、中心は異なる。

CALL化による命令差は約1.85M / 0.84M inst/bind。
ただし引数移送・spill・周辺のcodegen・コード配置も含む介入の収支であり、裸のCALL命令の費用ではない。
過去の親引数controlの増分すべてを、この別セッション・別構成の差に帰属することもできない。

## 通常GCの時間・割り当て

以下はA/Aの中央値であり、候補の性能値ではない。

| 入力・同じbaselineのラベル | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| checker A | 16,395,217 | 12,799,511.5 | 14,165 |
| checker B | 16,271,323 | 12,799,524 | 14,165 |
| dom A | 5,722,506 | 7,868,562.5 | 16,684 |
| dom B | 5,753,500 | 7,866,742 | 16,684 |

wallはbenchstatで非有意（checker p=.485、dom p=.589）だが、上記A/A精度条件は満たさない。
非有意を精度合格や同等性とみなさず、wallの候補比較はmissingのままにした。
allocs中央値は同じ。B/opの小差を表現縮小やGC改善とは扱わない。

## hot path・割当要因・診断

広いhot pathは既存Instrumentsのwalk/helpersで、generated walkerのcodegenはその一部である。
walker内ChildRefはchecker 189,849回、dom 74,321回。今回も論理read数は変わっていない。
既存のnode-wide symbolIdx/flows列、symbolRefs、FlowNode 32→48 B、Symbol 96→104 B、
宣言Handle 8→16 Bという割当増加要因は残る。pprofとsymbol syntheticは使っていない。

機械語は前回確認したとおり、baselineとsplitでwalker内ChildRefのCALLが0、split_callで291箇所。
splitは入口32 B＋各helper48 Bのframeで、baselineの48 Bに対して追加の呼出し区間を持つ。
今回の動的測定では分割にcyclesの増加があり、位置情報を保持するために分割を必須化する根拠にはならない。
原因を追加frameやdispatchの一つだけに絞る帰属は未実施である。

一方、同じ分割構成でinlineが失われると命令数が約1%増えた。
今後のアクセサ設計でも、API境界の変更が周辺のinline条件を変えていないか確認する理由になる。
分割はpointerにも適用可能な機構対照であり、Store固有の優位性としてこの実験を数えない。
既存のpointer同等までに必要なwall短縮34.65% / 28.51%を埋めたとは言えない。

## 次の行動・受け入れ条件

walker codegenの動的対照を完了し、今回の10関数分割は不採用とする。
構成Cは元のwalkerに固定。次は構文writerの推移的監査・writer guardの検証とowner/shape契約の確認、
その後に同じ経路で直接getter / 整数位置保持 / Go借用を比較する。
名前・Flagsの値memoや1pass化を混ぜず、Storeの表現を再解決する費用を対象にする。

採用にはwall有意3%以上短縮、inst/cycles低下、独立再現、8入力非悪化区間、全回帰・全phase・通常GCの確認が必要。
今回のKPC精度通過をwallの精度保証には使わない。新アクセサ・pointer同等性能・GC高速化は未証明のままである。
