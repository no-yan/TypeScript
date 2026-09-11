# 親refを引数で渡す対照（2026-09-10）

## 問題・結論

ancestor sliceを全nodeで維持すると、親取得一経路の利益より維持費が大きかった。
そこでgenerated walkerが既に持つ親refを、既存parentKindとともに識別子判定まで渡した。
**再取得対照比でcheckerの命令1.14%減。ただしbaseline比−0.35%は非有意、domは+0.51%の有意な増加。採用しない。**
追加allocationは中央値上認めず、全nodeのpush/popもない。一方、引数追加がwalkerのinline条件を変えた。
再解決除去の機構は確認できるが、最終構成の全体改善は未確認。

## 選択repo・artifact状態

Store: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests` @
`32598cba146fa4dd7b6162b838630c90d865ab28`。
pointer候補: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript` @
`8ac035a394c79e693a3a7d74cb170448503ee894`。TSGolint revisionはnull。
repo_root / tsgolint_git_rev / typescript_go_git_revの一致を確認しcurrent。
全候補checkoutはworktrees.txt（flownode、store-redesign、store-nolock-exp、lock-profile、profile、store-pr-*等）。
他の介入があるcloneを対照に混ぜない。今回pointerとの新規性能比較はmissing。

artifact set: `.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/parent-argument-experiment/`。
identity-check、各build identity、overlay/source/binary hash、dirty.patch、未追跡Go hash、
fixture hash/manifest参照、protocol、campaign、raw、benchstat、assembly、compiler diagnosticsを保存。
revisionのcurrentとdirty variantの同一性は別管理。保存baseline overlayで既存PropertyAccess候補を除外し、
ユーザーのbinder本体変更を保持。generated walkerもoverlayだけ変更し、本体とgenerator本体は未変更。
新しいInstruments profile、phase全体、通常GC CPU/assist/scan/liveはmissing。
過去のstale profileを今回の寄与の証拠にせず、pprofも使用しない。

## 仮説・提案修正と比較

generated walkerの既知parent refをchild/list helperからbindKindWithParentへ渡す。
その現在nodeがIdentifierなら、checkContextualIdentifierRefWithParent、isIdentifierNameRefWithParentへ伝える。
親ref=0は未提供を表し、元のParentRef/KindAtにfallbackする。
専用走査経路と遅延処理は従来入口を使える。Binder field、ancestor配列、memo、先読みは追加しない。

- baseline: 元の実装。
- control: 同じhelper・引数構成で親情報を渡すが、消費箇所は元どおり再取得。
- candidate: 同じ構成で、提供された親ref/kindを使う。

bindKindと識別子判定の本体は共通coreへ移し、旧入口を薄いwrapperにした。
診断ロジックの複製はしない。実験専用のchild/list/fallback走査helperを追加。
判定条件、診断、Flags更新、Symbol/Flow処理、訪問順は維持する。

2入力checker/dom、10 bind × 6 rounds、3者6通りの順序を各1回。結果を見た延長なし。
Go1.26.0、CGO=1、GOMAXPROCS=8、GOMEMLIMIT=off。
wallはGOGC=100、KPCはGOGC=offの別セッション。parse/強制GCはtimer外。
各KPC binaryの新規process自己検証2回、v7 baseline/event hash一致を確認。
fixed EL0+EL1は別列として保存し、KPC wallを通常GC wallに代用しない。
過去30 bind A/Aを今回10 bindの精度保証とは扱わない。
全rawにbenchstatを実行。補助区間はround対応mean-log-ratio bootstrap20,000回。

## 意味と提供情報の監査

8実入力＋9境界入力＋深い入れ子/遅延JS/分割代入3入力、計20入力で
構文・診断・Symbol・CFG・訪問順等がbaseline/control/candidateで一致。
AST/binder/compiler単体テスト、および既知親・fallback・親なし・旧入口の追加テスト成功。
コンパイラ全回帰試験とpointerとの完全な意味同値は未完了。

監査binaryだけで、渡されたすべてのparent ref/kindを実際のParentRef/KindAtと照合し、不一致0。
この照合の追加getterはcontrol/candidateの両方にあり、timed binaryにはない。
そのため生のbaseline対audit accessor差は削減数を表さない。consumer-delta.jsonは
両候補の差を取り、同じ監査コストを相殺したもの。

| 計数 | checker | dom |
| --- | ---: | ---: |
| 親情報を提供したnode | 170,667 | 87,969 |
| 識別子判定の既知親query | 104,998 | 0 |
| 同判定のfallback query | 18,555 | 0 |
| candidate対control ParentRef削減 | 104,998 | 0 |
| candidate対control KindAt削減 | 104,998 | 0 |

checkerで従来123,553 queryの約85%を置換。残りは既存経路に残す。
ParentRef/KindAtは一つの親解決の構成要素で、独立の寄与として足さない。
domは対象queryがないため、受け渡し・codegen変更の負の対照になる。

## 命令数・cycles

GOGC=off、EL0、中央値。pはbenchstat、n=6。

| 入力 | baseline inst/op | control inst/op | candidate inst/op | control対baseline | candidate対control | candidate対baseline |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| checker | 185,073,078 | 186,547,227.5 | 184,421,411.5 | +0.80%, p=.002 | −1.14%, p=.002 | −0.35%, p=.132 |
| dom | 77,760,767.5 | 78,293,971.5 | 78,161,016 | +0.69%, p=.002 | −0.17%, p=.394 | +0.51%, p=.002 |

checkerのcontrol対candidate削減約2.13M instに対し、baseline対control追加約1.47M inst。
差の約0.65M instは今回のbenchstatでは有意でない。構成の追加費用と局所利益を分ける。

| 入力 | baseline cycles/op | control cycles/op | candidate cycles/op | candidate対baseline / p |
| --- | ---: | ---: | ---: | ---: |
| checker | 86,794,241 | 86,560,678 | 87,018,267 | +0.26% / .818 |
| dom | 32,435,272 | 32,626,836.5 | 32,764,703.5 | +1.02% / .180 |

control対baseline cyclesは−0.27% (p=.937) / +0.59% (p=.240)、
candidate対controlは+0.53% (p=.589) / +0.42% (p=.240)。cycles改善は未確認。

## 通常GCの時間・割り当て

| 入力・variant | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| checker baseline | 16,356,268.5 | 12,799,524 | 14,165 |
| checker control | 16,353,187.5 | 12,799,499 | 14,165 |
| checker candidate | 16,227,371 | 12,799,537 | 14,165 |
| dom baseline | 5,848,556.5 | 7,870,383.5 | 16,684 |
| dom control | 5,787,902.5 | 7,866,716.5 | 16,684 |
| dom candidate | 5,776,929 | 7,866,729.5 | 16,684 |

candidate対baseline wallは−0.79% (p=.589) / −1.22% (p=.937)、非有意。
control対baselineは−0.02% (p=.937) / −1.04% (p=.310)。
candidate対controlは−0.77% (p=.310) / −0.19% (p=.818)。すべて非有意。
candidate対baselineのpaired95%区間はchecker [−1.83%, +1.07%]、dom [−1.64%, +0.90%]。
allocs中央値は不変。前のancestor実験との直接の有意差比較は今回行っていない。
B/opの小差を永続メモリ縮小の成果とはしない。GC高速化は未測定。

## 生成コードの副作用を切り分ける

主要frameサイズはbaseline/control/candidateで同じ:
bindKind144 B、contextual identifier240 B、identifier-name32 B、list64 B、generated walker48 B。
新たなslice拡張やcontext heap allocationは追加していない。

一方、generated walkerのChildRefはbaselineでinlineされるが、control/candidateではCALLになる。
静的に291箇所。walkerの命令行数は約10,292→3,816となったが、動的命令削減を意味しない。
このcodegen変化は対照と候補で共通なので、control対candidateが親再利用の対照になる。

測定完了後のGo compiler JSON診断で原因を確認した。
controlのbindwalk_generated.jsonには
`cost 36 of ...(*Store).ChildRef exceeds max caller cost 20` と記録される。
使用中のGo1.26.0 compilerのinline/inl.goでは、5000 IR node以上をbig functionとし、
そのcallerのinline上限を20に制限する。引数追加後のwalkerがこの制限に該当している。
これはソース上の引数追加が、別のaccessorのcodegenまで変える例である。

証拠はbaseline/control-compiler-diagnostics.txtとbaseline/control-inline-log/。
最初の*-inline.txtはstdoutだけの空ファイルであり、compiler診断の根拠には使わない。
JSON診断はstderrと併せて保存し直した。タイミング測定とは同時実行していない。

## hot path・allocation driver・診断

広いhot pathは既存Instrumentsのwalk/helpers。今回の狭い消費経路は識別子の親判定。
受け渡しの変更は広いgenerated walkerのcodegenへ影響しており、消費側だけでは全体効果を説明できない。
既存allocation driverはsymbolIdx/flows列、symbolRefs、FlowNode32→48 B、Symbol/DeclarationsのHandle拡大。
これらは未変更。symbol syntheticは使用せず、実入力を測った。

「既知情報の再利用は局所命令を減らす」は支持される。
「祖先配列の代わりに引数を足せば、必ず全体が速くなる」は支持されない。
IR規模によるinline予算の変化を放置すると、API境界の変更が他の取得経路を悪化させ得る。
ただしcontrolの追加命令すべてをinline変化だけへ帰属する実験はまだない。

## 再現・採用条件・次の行動

生成: `tools/scripts/tsc/binder_parent_argument_experiment.py`。
再測定は保存overlay/binaryとcampaign.pyの条件・入力・順序を使い、新規出力先へ保存する。
KPCは管理者権限の独立セッション。追加テストとassemblyはverify.py。
wall-6とkpc/kpc-6の3pairで `benchstat <old>.txt <new>.txt`。rawは上書きしない。

本体不採用。次はparent引数の横展開を止め、generated walkerのinline制限を独立に切り分ける。
例えばwalkerを小さな内部関数へ分ける場合も、それ自体のCALL費用を対照で測り、
親情報の再利用を混ぜずcodegenが安定する構成かを先に判断する。単なるinline強制はしない。

採用には実入力wall有意3%以上、inst/cycles低下、独立確認、8入力非悪化区間、全回帰、
parse/bind/parse+bind、通常GC CPU/assist/scan/liveが必要。pointer同等とGC高速化は別判定。
owner＋nameの独立確認と解決済み名前core化は未完了として保持する。
