# 解決済み情報を呼び出し経路で渡す対照（2026-09-10）

## 問題と判定

bindParameterRefが既に名前ref/kindを持つのに、Symbol宣言の後段helperが同じ名前を再解決する。
「短い呼び出し経路を通じて情報を共有すれば、保持コストを含めても仕事が減る」を検証した。
**domでは成立。checkerは減少方向だがbenchstatで非有意。全関数への一般化、cycles/wall改善、採用は未確定。**
ユーザー指定の条件に従い、この限定的な機構確認を根拠に再設計の仕様化に進む。
wall3%の製品採用条件は緩めない。

## 選択repo・artifact状態

Store: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests` @
`32598cba146fa4dd7b6162b838630c90d865ab28`。
pointer候補: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript` @
`8ac035a394c79e693a3a7d74cb170448503ee894`。TSGolint revisionはnull。
repo_root / tsgolint_git_rev / typescript_go_git_revが一致し、identityはcurrent。
全候補cloneはworktrees.txtに保存（flownode、store-redesign、store-nolock-exp、
lock-profile、profile、store-pr-*等）。別介入がある候補は対照に使わない。

artifact setは`.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/threaded-name-experiment/`。
identity-check.json、各buildのidentity.json、overlay/source/binary hash、dirty.patch、
untracked-go-sha256.json、protocol.json、campaign.py、raw、benchstat、計数、assemblyを保存。
revisionのcurrentとdirty variantの同一性は別管理。既存PropertyAccess候補はbaseline overlayで除外し、
ユーザーの本体変更は保持した。今回の変更はすべて実験overlay。
新しいpointer性能比較・Instruments profile・通常GC CPU/assist/scan/live・phase全体比較はmissing。
過去のstale profileを今回の因果根拠にしない。pprofは使っていない。

## 仮説と対照

対象経路:
`bindParameterRef → declareSymbolAndAddToSymbolTableRef → declareSymbolRef → hasDynamicNameRef / getDeclarationNameRef`。
Parameter入口が取得済みの{nameRef, nameKind}を8 byteの値で渡す。
新しい先読み、全Binder memo、永続pointer cacheは導入しない。

- baseline: 元のStore実装。
- control: 同じhelper群を実験用に複製し、情報を渡すが、末端2か所で元どおり再取得する。
- candidate: controlと同じ呼び出し経路で、末端2か所だけ渡された情報を使う。

source-control-check.jsonでcontrol/candidateの違いが複製側の名前取得2か所だけであることを確認。
診断・Symbol merge・動的名前判定の分岐・更新のアルゴリズムは複製元と同じ。
module/source/classへの分岐は元helperを維持。Parameter propertyの別の宣言経路も変更しない。
つまり全宣言名queryの置換ではなく、一つの実行経路に限定した対照。
コンパイラは未使用引数を最適化し得るため、controlの機械語が完全に同一とは仮定しない。
assemblyと実測で保持・call構成の差を確認する。

## 方法

事前指定: checker/dom、10 bind × 6 rounds、3者の順序6通りを各1回。
主判定はcontrol対candidateの実binder EL0命令数。baseline対candidateも確認する。
Go1.26.0、CGO=1、GOMAXPROCS=8、GOMEMLIMIT=off。
wallはGOGC=100、KPCはGOGC=offの独立セッション。parse/強制GCはtimer外。
KPCは各binaryを新規processで2回自己検証し、既存v7 baseline/event hashとの一致も検証。
fixed EL0+EL1は別指標として保存。KPC内wallを通常GCのwallに代用しない。
過去の30 bind A/Aが今回10 bindの精度を保証するとは扱わない。
全rawを保存してbenchstatを実行。補助集計はround対応mean-log-ratio bootstrap20,000回。
外れ値を除かず、結果を見てroundsを延長しない。

## 正しさと機構の証拠

control/candidateそれぞれ8実入力＋9境界入力で診断・Symbol・CFG・構文・訪問順等がbaselineと一致。
AST/binder/compilerの単体テスト成功。コンパイラ全回帰試験は未実施。
既存のpointer対Storeの匿名class Symbol.Name差を解消したという意味ではない。

| 計数 | checker | dom |
| --- | ---: | ---: |
| Parameter入口回数 | 5,788 | 8,446 |
| 後段の名前取得回数 | 11,534 | 16,892 |
| candidateのChildRef削減 | 11,534 | 16,892 |
| candidateのKindAt削減 | 11,534 | 16,892 |

controlの元アクセサ計数はbaselineと一致。candidateの違いは上表と実験カウンタだけ。
ChildRefとKindAtは同じ解決経路の構成要素であり、独立の全体寄与として足さない。

生成コードでも両末端からnameOfDeclarationRef CALLが消えた。入口の取得は残る。
stack frame: 入口96 Bは不変、受け渡しhelper80→96 B、declareSymbolRef272 Bは不変、
getDeclarationNameRef160→144 B、hasDynamicNameRef32 Bは不変。
局所取得コストだけでなく引数保存コストを含む対照である。
静的命令行数はdynamic inst/opの代用にしない。

## 性能結果

EL0、GOGC=off。差は中央値比、pはbenchstat、n=6。

| 入力 | baseline inst/op | control inst/op | candidate inst/op | candidate対control | candidate対baseline |
| --- | ---: | ---: | ---: | ---: | ---: |
| checker | 185,479,738.5 | 185,283,610 | 184,313,996 | −0.52%, p=.065 | −0.63%, p=.093 |
| dom | 77,697,210.5 | 77,822,163.5 | 76,563,223.5 | **−1.62%, p=.002** | **−1.46%, p=.002** |

control対baselineの命令差はchecker−0.11% (p=.485)、dom+0.16% (p=.093)。
control対candidateのpaired命令差95%区間はchecker [−0.75%, −0.04%]、dom [−1.94%, −1.37%]。
checkerの補助区間は0を跨がないが、事前のbenchstat判定と異なるため、有意な改善とは採用しない。

| 入力 | baseline cycles/op | control cycles/op | candidate cycles/op | candidate対control / p | candidate対baseline / p |
| --- | ---: | ---: | ---: | ---: | ---: |
| checker | 86,924,870.5 | 86,655,050.5 | 87,585,807 | +1.07% / .485 | +0.76% / .485 |
| dom | 32,267,423.5 | 32,261,961 | 32,161,203 | −0.31% / .394 | −0.33% / .394 |

通常GCの結果:

| 入力・variant | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| checker baseline | 16,606,387.5 | 12,799,486 | 14,165 |
| checker control | 16,762,181 | 12,799,499 | 14,165 |
| checker candidate | 16,601,390 | 12,799,524 | 14,165 |
| dom baseline | 5,850,796 | 7,868,524.5 | 16,684 |
| dom control | 5,834,841.5 | 7,870,358 | 16,684 |
| dom candidate | 5,783,152 | 7,866,703.5 | 16,684 |

candidate対baseline wallはchecker−0.03% (p=1.000)、dom−1.16% (p=.310)。
candidate対controlは−0.96% (p=.485) / −0.89% (p=.310)。すべて非有意。
checker controlの第1round25.37ms、candidate20.69msなど大きなばらつきがあり、原因は未特定。
wall-round-values.jsonに全値を保存。candidate対baselineのpaired95%区間は
checker [−1.00%, +10.95%]、dom [−9.90%, −0.05%]。補助区間だけを選んで採用しない。
小さなwall改善や非悪化の判定には精度不足。allocs中央値は不変、永続メモリ縮小は実装していない。

## 帰属・診断

広いhot pathは既存Instrumentsのwalk/helpers。今回の介入はそのうちParameterからSymbol宣言への
狭い経路であり、declareSymbol全体や他のhelperを支配的と断定する結果ではない。
symbol syntheticは使用せず、全結果は実入力のbinder測定。
allocation driverは引き続きsymbolIdx/flows列、symbolRefs、FlowNode32→48 B、
Symbol/DeclarationsのHandle拡大。本候補はこれらを縮めない。

domの改善は「同じhelper構成で情報を渡すだけ」のcontrolにも勝つため、単なる関数配置変更より
再解決除去という仮説を支持する。getter内部の高速化や全Binder memoが必須ではない。
ただし構文名という安定情報の一経路の証拠であり、可変Flags/Flow等や全関数への普遍性は未証明。
cyclesの低下は未確認で、命令数が減れば時間も同率で減るという仮説は支持していない。

## 再現・提案修正・採用条件・次の行動

生成: `tools/scripts/tsc/binder_threaded_name_experiment.py --out <新しいbaseline artifact root>`。
既存実験の再現は保存overlay/binaryを用い、campaign.pyの入力・環境・順序を新しい出力先に移す。
KPCは保存のrun.shと同じ管理者権限で別セッション。rawは上書きしない。
各wall-6、kpc/kpc-6配下の3つのpairで `benchstat <old>.txt <new>.txt` を実行できる。

提案修正は、取得済み情報を必要なhelperまで渡す内部API。実験の大きなhelper複製は採用しない。
[再設計の初稿](resolved-access-design-20260910.md)に共通core・有効期間・lazy取得の契約を記載した。
次は複製をなくした最小実装で今回の命令削減が維持されるかを確認し、その後別経路で再現性を検証する。
命令削減の成立を理由に、全APIの一括変更やwall3%未達候補の採用はしない。
採用には対象実入力wall有意3%以上、inst/cycles低下、独立確認、8入力の非悪化区間、
全回帰、parse/bind/parse+bind、通常GC CPU/assist/scan/liveの検証が必要。
owner＋nameの独立確認は未完了として保持。pointer同等、メモリアクセス高速化、GC高速化は別判定。
