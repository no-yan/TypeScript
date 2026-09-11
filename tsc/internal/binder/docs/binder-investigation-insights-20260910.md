# Store AST binder調査：現時点のinsight

更新日: 2026-09-10。対象はpointer ASTからStore/index ASTへの変更に伴うbinder退行。
本書は実験横断の結論と設計判断をまとめる。条件・raw・検定・再現手順はリンク先の個別ノートを参照。

## 1. 現時点の結論

**Store固有の再解決は、複数の経路で実命令数を増やしている。必要な情報を短い有効期間で再利用する設計には根拠がある。ただし、全体を速くするには、情報を保持・受け渡す費用とGoのコード生成を同時に扱う必要がある。**

この結論は、宣言名memo、list範囲の再利用、helper間の名前受け渡し、親情報の再利用という異なる対照から得た。
一方、全nodeのancestorスタックは維持費を回収できず、親refの引数追加も別のアクセサのinline条件を変えた。
したがって「全getterでcacheすれば速い」「情報を引数にすれば速い」という普遍則は得られていない。

続く[walker codegen対照](walker-codegen-experiment-20260910.md)では、親引数を追加せず分割とCALL化を切り分けた。
20入力の意味・訪問順・アクセサ呼出数は一致し、機械語の対照も成立したが、wallの改善は未証明。
KPCはA/Aのcycles精度不足で候補比較がmissingとなった。分割は不採用で、アクセサ設計の必須条件にしない。
並行ベンチマーク停止後の[独立A/A再確認](walker-codegen-recheck-20260910.md)でも、命令数は両入力で通過したが、
cyclesはdomのみ通過、wallは両入力で不合格だった。このセッションの候補比較はmissing。測定後にも別processのCPU使用があった。
その後の[再計測02](walker-codegen-recheck-02-20260910.md)でKPC精度が通過し、3者比較を完了した。
分割は命令数の有意減がなく、cycles +1.33% / +2.18%。同じ分割でのCALL化は命令数+1.00% / +1.07%。
分割の不採用を維持し、今後のAPI変更でも周辺のinlineを確認する。通常GC wallの候補比較は今回もmissing。
さらに[-Bの適用範囲対照](binder-bounds-check-experiment-20260910.md)で、元のwalkerはBinderのみ-Bで
命令−4.65% / −3.57%、Binder＋astで−6.72% / −4.81%となった。Goの自動境界チェックには数%の寄与がある。
ただし手書きslot検査は残り、両packageに-BでもCALL化の増分は+1.30% / +1.05%残った。
これは広いpackageへの診断介入であり、特定アクセサやStore固有の費用への全額帰属ではない。
最新の主設計は[構文読取契約・直接getter・Slot/Spanのv2](binder-slot-accessor-design-20260910.md)。
pointerにも適用できる名前memo等の共通最適化を除き、表現の再解決費用を対象にする。

| 判断対象 | 現在の到達点 |
| --- | --- |
| Storeの追加命令が複数の解決経路に存在する | 対照実験で支持される |
| 短い範囲で既知情報を共有する内部APIの再設計 | 試す根拠がある。設計段階 |
| すべての関数で再利用が正味の利益になる | 未証明。費用が利益を上回る実例がある |
| 実binderのwall 3%以上改善 | owner＋nameでdomの初回探索に限り確認。独立確認等が未完了 |
| 最適化の本体採用 | 新規候補は未採用。実験用overlayに保存 |
| pointer同等性能 | 未達・未証明 |
| メモリアクセスとGC双方の高速化 | 未証明。cache miss低下やnoscanだけでは判定しない |

## 2. 比較基準と数値の読み方

選択repoは `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、Store revisionは
`32598cba146fa4dd7b6162b838630c90d865ab28`。
pointer対照は `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、revisionは
`8ac035a394c79e693a3a7d74cb170448503ee894`。TSGolint revisionはnull。
本書作成時も両HEADが一致することを再確認した。

候補cloneにはflownode、store-redesign、store-nolock-exp、lock-profile、profile、store-pr-*等がある。
別の介入を含むため対照に混ぜない。全パスとrevisionは
[checkout一覧](../../../../.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/worktrees.txt)に保存している。

主要artifact setは
[20260910-binder-investigation-02](../../../../.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/)。
各ノートから個別artifactを特定できる。これらはローカル保存物であり、文書だけを別checkoutへ移す場合は別途共有が必要。

| 状態 | 本調査での扱い |
| --- | --- |
| `current` | repo_root / tsgolint_git_rev / typescript_go_git_revが選択checkoutに一致。今回の主要実験は各記録で確認済み |
| `stale` | 古いeval・旧Instruments等。仮説の背景には使うが、現在の候補の効果の証拠とはしない |
| `unsupported` | artifactはあるが要求stageのBenchmark行がない。例: 初期KPC v5のBenchmark stage |
| `missing` | 独立採用確認、候補の全phase・通常GC CPU等、まだ取得していない検証 |

`current`はdirty variantの同一性を保証しない。各buildのdirty diff、未追跡ソース、overlay、binary hash、fixture hashを別に管理する。
既存のPropertyAccess候補は保存baseline overlayで除き、ユーザーの本体変更を保持した。

数値の解釈は次を固定する。

- 比較は保存rawに対するbenchstat。表の改善率は原則として中央値比で、各実験の対照を明示する。
- **別セッションの改善率を順位付け・加算しない。** 例えばancestorと親ref引数は同じセッションの直接対照ではない。
- 通常GCのwallと、GOGC=offのKPC命令数・cyclesは別指標・別セッション。fixed EL0+EL1とEL0も分ける。
- 初期はA/Aと広い入力比較、その後はユーザー指定に従い主に2入力・10 bind × 4〜6 roundsへ縮小した。過去の30 bind A/Aが小規模探索の精度を保証するとは扱わない。
- 非有意差は効果ゼロ・同等性・非悪化の証明ではない。補助bootstrapだけが有利でも事前のbenchstat判定を置き換えない。
- CPUの帰属にはInstrumentsを使い、pprofは根拠から除外する。

測定の詳細は[初期比較・A/A](binder-investigation-results-20260910.md)、[KPC信頼性](kpc-reliability-20260910.md)、[KPC入力別比較](kpc-binder-results-20260910.md)を参照。

## 3. 問題の全体像

初期の同条件Store/pointer比較では、checkerの通常GC wallは約1.53倍、domは約1.40倍だった。
会話冒頭の「1.7倍」や別セッションの値とは直接比較しない。

| 入力・指標 | pointer | Store |
| --- | ---: | ---: |
| checker ns/op | 12,458,733.5 | 19,064,295 |
| checker B/op | 7,425,344 | 12,799,524 |
| checker allocs/op | 13,954 | 14,165 |
| dom ns/op | 4,874,793.5 | 6,819,245 |
| dom B/op | 5,287,312 | 7,867,930 |
| dom allocs/op | 16,562 | 16,684 |

KPCではcheckerのEL0命令が約110.94M→185.30M、domが49.14M→77.76M。
6入力で追加命令は約225〜261 inst/nodeと近く、CFGが少ない入力にも退行がある。
**共通walk/helpersとStore解決が優先仮説であり、CFGや識別子判定だけでは全体を説明できない。**
ただし8入力の相関は、固定単価や因果の証明ではない。

checker/domではL1D load missが約13%減った一方、load/storeと分岐が増えた。
これは「配置が改善しても、アクセスする回数・解決する仕事が増えれば遅くなり得る」ことを示す。
load/storeとload missを割って通常のmiss率とは呼ばない。どの機構が時間差の何割を説明するかは未確定。

広いhot pathはInstrumentsのwalk/helpers。狭い介入先として、owner・header・child/list・名前・親の解決を扱った。
現在までの局所改善でStore/pointer差の大部分を説明・解消できたわけではない。
根拠は[初期実験](binder-investigation-results-20260910.md)と[KPC比較](kpc-binder-results-20260910.md)。

## 4. 個別実験から何が分かったか

以下は実験の索引を兼ねる。各行の値はそのノート内の対照に限った結果であり、横方向のランキングではない。

| 実験 | 観測 | 得られたinsight・現在の判断 |
| --- | --- | --- |
| [named-child sliceの一括取得](binder-investigation-results-20260910.md) | Store microがchecker +11.75%、dom +13.67%悪化 | 一括取得の外形だけでは不十分。list/ownerの費用を残したこの構成は不採用 |
| [local owner fast path](accessor-exploration-20260910.md) | checker/domの実命令が約1.51% / 1.08%減 | 汎用解決関数との境界を通常経路で避ける利益がある。wall採用条件は未達 |
| [Flags取得の再利用](accessor-fields-experiment-20260910.md) | FlagsAtを123,554 / 36,271回省いても命令は減らず | getter計数だけでは評価できない。保持・spill等の費用を含める必要がある |
| [ListSlotAtのinline/noinline](listslot-experiment-20260910.md) | inlineでCALLは消えたが実命令は減らず。同じ本体のnoinlineはcheckerでinline比0.65%命令減 | CALL削減・inline拡大は目的ではない。コード膨張を含む収支が必要 |
| [宣言名の一件memo](name-memo-experiment-20260910.md) | checker/dom命令 −0.90% / −2.49%。dom cycles −2.67% | 安定した構文名の多段解決を小さな状態で省ける。単独wall条件は未達 |
| [owner＋宣言名memo](owner-name-experiment-20260910.md) | dom wall −3.32%（p=.004）、命令 −3.66%、cycles −3.52% | 最も採用検証に近い候補。ただし独立再現・8入力性能等は未完了 |
| [list span](list-span-experiment-20260910.md) | checker/dom命令 −1.42% / −2.68%、dom cycles −2.63% | 一走査のowner/start/length再利用は成立。通常wallは非有意 |
| [名前をhelper間で渡す](threaded-name-experiment-20260910.md) | dom命令は再取得対照比−1.62%、baseline比−1.46%、ともに有意 | 全Binder memoなしでも呼び出し経路内の再利用が成立。checker・cycles・wallは未確定 |
| [ancestorスタック](ancestor-experiment-20260910.md) | checkerは維持対照比−0.90%だがbaseline比+1.91%。dom +2.52% | 再利用は効くが全nodeの維持費を回収できない。この用途・構成は不採用 |
| [親refを引数で渡す](parent-argument-experiment-20260910.md) | checkerは再取得対照比−1.14%、baseline比−0.35%は非有意。dom +0.51% | 配列をなくしても、引数追加による別のcodegen変化が利益を打ち消し得る |
| [walker分割とCALL化](walker-codegen-recheck-02-20260910.md) | 分割は命令数の有意減なし、cycles +1.33% / +2.18%。同じ分割のCALL化は命令数+1.00% / +1.07% | inline喪失には費用があるが、分割自体を改善策にはできない。元のwalkerを維持 |
| [Go境界チェックの-B対照](binder-bounds-check-experiment-20260910.md) | 元walkerの命令はBinderのみ-Bで−4.65% / −3.57%、Binder＋astで−6.72% / −4.81%。CALL化の増分は残る | 自動チェックの寄与はあるが、手書きschema検査や全再解決とは別。通常buildで安全な償却を検証する |

## 5. 成功した変更に共通する仕組み

### 5.1 既に知っている情報を、必要な範囲で使い切る

呼び出し側がkind・構文名・list範囲を知っていても、helperへ汎用refだけ渡すと、その情報を取り直す。
この境界をまたいで情報を残すことが、複数の実験で命令削減につながった。

名前memoはname slot/child/kindの解決を省き、list spanはowner/header解決を要素ごとから走査ごとへ変える。
名前の受け渡し実験は、同じ関数構成の再取得対照よりも安くなったため、単なる配置変更より再解決除去を支持する。
親の再利用も、ancestor方式と引数方式の両方で、それぞれの維持・受け渡し対照に勝った。

ただしlocal owner fast pathでは、元のforeign lookupも既に必要時だけだった。
その利益を「不要なforeign lookupを毎回していた」と説明しない。local判定と関数境界の変更が介入である。

### 5.2 必要な情報の寿命が短く、無効化が単純

有望だった情報は、同じ宣言の構文名、一走査のlist範囲、現在nodeの親など。
既存の処理が情報を必要とした時点で得て、その短い範囲で共有できる。

この設計は、未使用情報の先読みや全nodeのcacheを必須にしない。
一方、可変Flags・Symbol・Flowを同じ寿命とみなす根拠はない。再帰・更新をまたぐ保存には別の検証が必要。

### 5.3 永続表現と処理中の表現は別にできる

永続ASTのindex表現を保ったまま、処理中だけ少数の解決済み値を持てる。
今回のlist span・名前の受け渡しはその例であり、AST全体をpointerへ戻すことを要求しない。
ただしBinderは既にStoreを、既存引数/Handleはkindを持つ。単に別のReader型へ包むだけでは仕事は減らない。

## 6. うまくいかなかった変更からの制約

### 6.1 再利用の費用は利用頻度で回収する必要がある

ancestor実験のcheckerは、298,054 nodeでstackを更新して123,553回の親取得に利用した。
同一セッションの中央値差では、維持等の追加約5.25M instに対し、再取得除去は約1.71M instだった。
利用のないdomにも維持費が発生した。

したがって全nodeに状態を持たせるなら、複数の実需要から費用を回収できる根拠が必要。
今回の不採用は、深い祖先検索へ広く共有する案や、再帰走査自体を明示stackへ置き換える案まで否定しない。
既存再帰に追加stackを重ねる実験と、走査アルゴリズムを置き換える実験は異なる。

### 6.2 ソース上の小さな変更が、離れたアクセサの機械語を変える

親ref引数実験では、generated walkerのChildRefがinlineからCALLへ変わった。
Go compiler JSON診断は、ChildRefのcost36がcallerのinline上限20を超えると報告した。
使用中のGo1.26.0には、IR規模を基準にbig functionのinline予算を制限する規則があり、変更後のwalkerが該当した。

これは、情報の受け渡し設計をfield数やgetter数だけで評価できない理由である。
ListSlotAtの逆の対照でも、inlineによるコード膨張が利益にならなかった。
**inlineするほどよいのでも、しないほどよいのでもない。実行回数、コード規模、call/保持費用の収支が必要。**

続くwalker対照では、同じ分割構成でChildRefだけをCALL化すると命令数が約1%増えた。
これは引数移送・spill・周辺codegen・配置を含む介入の収支であり、裸のCALL命令の費用ではない。
親ref対照の追加命令すべてを、この別構成の差へ帰属したわけでもない。

### 6.3 命令数、cycles、wallは同率で動かない

list spanや名前の受け渡しには、命令削減を確認してもcycles/wallを確認できない例がある。
命令数は機構を検証する指標だが、性能目標の代用にはならない。
静的命令行数やCALL数も動的命令数の代用ではない。

## 7. 探索方法でうまくいったこと・限界

有効だったのは、最初に広い入力比較で仮説を絞り、次に小さい対照で機構を検証したこと。
特に「baseline / 状態を維持・渡すだけ / 実際に再利用」の3者比較によって、利益と費用を分けられた。
domはある実験では名前再利用の利益が大きい入力となり、別の実験では親判定がない負の対照になった。
入力の役割は実験ごとに操作数で判断する必要がある。

計数は別binaryに分離し、意味・訪問順を監査した。最近の親関連実験では20入力を確認している。
KPCの不正なcounter挙動も事前検証で検出し、新規OS thread方式とA/Aで確認し直した。
これらにより、誤った計測や意味変更を性能改善と取り違える危険を減らせた。

限界は、小規模wallの区間がしばしば広いこと、全体の追加命令を機構別に配分できていないこと、
採用に近い候補の独立確認がまだ残っていること。
非有意結果を効果ゼロとせず、逆に有意な小さな命令削減だけで大きなwall改善を約束しない。
walker codegenの動的対照は再計測02で完了した。分割はcyclesが増え、CALL化は同じ分割比で命令数が約1%増えた。
次段階では分割幅の探索を増やさず、
最新の比較条件に沿う構文の有効期間と直接getterを監査し、性能測定の精度を整える。

## 8. メモリ・GCでまだ言えないこと

既存のallocation driverは、node-wideなsymbolIdx/flows列、symbolRefs、FlowNodeの32→48 B化、
Symbolの96→104 B化、declaration Handleの8→16 B化。
アクセス経路の最適化はこれらの表現縮小とは別であり、今回それらを解消していない。
詳細は[allocation driverの帰属](binder-investigation-results-20260910.md)。

整数だけの一時配列は要素領域をnoscanにできても、新たな割り当て・書き込みを追加し得る。
ancestor配列では実際にallocs中央値がcheckerで7、domで5増えた。
一方、親ref引数ではallocs中央値は不変だった。これはGC CPUの改善を意味しない。

BindHotはparseと強制GCをtimer外に置くため、これだけでGC高速化を判定できない。
同じworkload・保持条件・処理量で通常GCを動かし、累積GC CPU、assist、scan、live、総時間を測る必要がある。
parse/bind/parse+bindの比較も未完了で、別phaseへ費用を移していないことはまだ確認できていない。

## 9. 再設計への方針

一般的な方針は、**必要時に解決し、短い有効期間で共有し、共有の費用を払う範囲も限定する**こと。
現時点では次を設計上の条件とする。

1. 汎用入口と解決済み情報を受け取る内部coreを分け、診断・merge等の本体は一本化する。
2. 構文名やlist範囲など、安定した小さな情報から始める。未取得と「取得したが存在しない」を区別する。
3. 任意node・foreign Store・遅延処理を現在の走査文脈で代用しない。必要なfallbackを残す。
4. 全nodeの汎用ancestor/cacheを前提にしない。iteratorも構築・更新費用を含めて評価する。
5. 巨大generated関数のIR規模、inline、spill、コード量を確認する。抽象化の追加で離れた経路が変わる可能性を扱う。
6. 判定・診断・走査順を維持した情報フローの変更を先に行い、走査アルゴリズムの変更は別介入にする。

設計の詳細と更新履歴は[解決済み情報を共有する内部API](resolved-access-design-20260910.md)。

最新のユーザー条件により、直近の作業は次のように分ける。

| 系統 | 次の行動 | 判定したいこと |
| --- | --- | --- |
| 主設計の機構 | 構文writer・owner/shape契約を監査し、元のwalkerを共通に直接getter/整数位置保持/Go借用を比較 | Storeの表現解決だけを減らせるか。保持は直接getterに対する正味の利益を要する |
| 測定の精度 | KPCは再計測02で精度確認と候補比較を完了。wallの精度を別途確認する | KPCの成功を通常GC wallの精度保証や高速化の証明にしない。過去の未実行stageはmissingのまま保持 |
| 既存研究の未完了項目 | owner＋nameのdom独立確認、精度確認後の8入力比較 | 初回wall3.32%の再現性。共通memoを含むため今回のStore固有の主候補には入れない |

名前core化の残作業も保持する。すべてのアクセサへの横展開を先に始めない。

## 10. 採用条件と残課題

既存探索のowner＋nameはdomの初回wall中央値−3.32%を得たが、そのpaired95%区間は
[−4.60%, −2.86%]であり、区間全体が3%以上の改善を保証しているわけではない。
独立確認・8入力性能・全phase/GCが未完了なので本体未採用である。
共通の名前memoを含むため、最新のStore固有の主比較にはこの効果を取り込まない。

採用には、事前指定の実入力でwallが3%以上短縮しbenchstatで有意、inst/cyclesも低下すること、
独立セッションでの再現、代表8入力で3%以上の退行がないことの区間による確認を求める。
さらにAST/binderテスト、コンパイラ全回帰、診断・Symbol・CFG・訪問順、全phase、通常GCを確認する。

Store baselineとの監査一致はpointerとの完全一致を意味しない。
既存の匿名classのSymbol.Name差も残っており、全体の正しさの残課題として扱う。

実験順序・進捗は[調査計画](binder-investigation-plan-20260910.md)を参照。
本書の時点では、新規最適化の採用、pointer同等性能、メモリアクセス・GC双方の高速化は、いずれも完了していない。


### setter検査削減の追加診断（2026-09-10）

[setter対照](binder-setter-check-experiment-20260910.md)を元のwalkerの4条件で完了。
Binder＋astの-Bにsetter phase/live/local所有権検査削減を加えると、EL0命令は追加−0.59% / −0.39%、
通常版比の合計−7.29% / −5.07%。通常GC wallは合計−3.03% / −1.30%の中央値だが非有意かつA/A不合格。
4条件の20入力監査と追加local所有権assert監査は一致。実験用overlayのみで本体不採用。
この小さい追加効果を主な到達手段とはせず、構文writer監査・直接getterの既定順序へ戻る。


### list要素配列分離の実Binder対照（2026-09-10）

[配置×Spanの4条件実験](list-layout-experiment-20260910.md)を完了。
分離だけの命令差−0.43% / −0.16%は非有意。共有配置Spanの命令−1.74% / −2.87%は有意。
Spanへ分離を追加しても命令・wallの追加改善なし。20入力監査、core test、scratch/Restore/CompactとSpan境界試験成功。
checker wallは分離−0.97%、Span−1.61%だが、前者はA/Aの中心ずれ約−0.88%と同程度。dom wallは精度不足。
分離は本体不採用。parse+bind費用は未測定。単純分離を広げず、構文読取契約と直接getter/Spanへ戻る。


### 生成walkerの直接list-slot Span対照（2026-09-10）

[4条件の実装対照](direct-list-span-experiment-20260910.md)を完了。
129箇所を同じhelperに接続するcontrol/directで、resolverのみ変更。20入力監査・core/境界試験成功。
直接版はcontrol比命令−1.13% / −1.62%だが、helper化自体が+0.59% / +1.21%。
既存Span比の純追加は−0.55%（p=.093）/ −0.43%（p=.041）。wall/cycles追加改善は未確認、wall A/A不合格。
本体不採用。list経路の細分化を続けず、残存ChildRef/KindAt/FlagsAtとwriter契約の削減可能量監査へ戻る。


### 残存3getterと構文writerの監査（2026-09-10）

[残存アクセサ監査](remaining-accessor-audit-20260910.md)を完了。8入力のcaller総数を再検証し、実Store codegen probeで単純guard省略の桁を確認。
KindAt/FlagsAtにowner再解決はなく、ChildRefの生成範囲は約71% / 96%。guard省略だけでは必要wall短縮の予算を裏付けられない。
27構文入口の20入力Bind監査で構文writer計数0、Flags更新はchecker4,980 / dom6,391回。意味・訪問順・既存accessor計数一致。
次の設計対象は構文読取契約とheader位置の短期借用。Flagsはfresh readを維持し、値memoにしない。
全writer/並列writer契約と保持費の回収は未証明。新規速度benchmarkは未実行、本体未変更。


### 生成walkerのchildren直接load（2026-09-10）

[直読対照](direct-children-experiment-20260910.md)を完了。291箇所をcaseごとの固定長配列借用にし、位置/配列helperのCALLが0であることを確認。
実Binder命令はchecker −1.12%、dom −0.50%（各p=.002、命令A/A合格）。通常wall −1.90% / +0.13%は非有意かつA/A不合格。
20入力一致、core test成功。静的コード減少と同時にframe 48→352 B、L1D miss約+2%があり、最適性・時間短縮は未証明。
実験overlayのみ。次に同一経路の保持変数共有等でframeを縮められるかを機械語で確認し、差がなければ測定を増やさない。


### 整数child一括取得：If＋高頻度walker（2026-09-10）

[整数一括取得](syntax-scalars-experiment-20260910.md)をworktreeと生成元へ実装。ChildRef連続呼出しでは共有されなかったheader/startをStore内で一回解決し、NodeRefを返す。
Ifに加え147 case/291 siteへ展開。今回の共通PropertyAccess変更込みでChildRef削減135,117 / 74,171回。helper CALL0、walker frame 48→80 B。
3条件20入力一致、AST/Binder/Compilerテスト成功。実Binder命令−1.69% / −1.76%（各p=.002、A/A合格）。Ifだけからwalker追加分−1.38% / −1.51%。
dom cycles−2.04%（p=.041、A/A合格）。通常wall−1.40% / −1.89%は非有意、checker wall/cycles精度不足。
実装はworktreeに保持。全面採用・pointer同等・GC高速化は未証明。次は独立wall精度確認と構文snapshot契約、parse+bind/GC検証。


### 残存する再解決の棚卸し（2026-09-10）

[15分類の一覧](repeated-access-inventory-20260910.md)を作成。Binary/Call、Parameter等の専用走査、宣言名の受渡し、list owner/startの再解決を優先。
同じ値の再取得と同じheader上の別field取得、可変Flagsのfresh readを区別した。現行Go sourceとsyntax-scalars artifactのhash一致を確認。
新規benchmark・実装変更なし。局所変更は意味・機械語で確認し、まとまった改修後に全体benchstatを取る方針。
