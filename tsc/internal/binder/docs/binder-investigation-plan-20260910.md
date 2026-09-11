# Binder 性能調査の進め方（2026-09-10）

ユーザーから「進める順序を覚えておいて」と指定された調査計画。
目的は、index-based AST の binder 退行を切り分け、メモリアクセス高速化と
noscan による GC 改善の両立を検証すること。

## 現在位置

最新の設計依頼では、pointerにも適用できる共通ロジックの最適化を主比較へ混ぜない条件が明示された。
[アクセサ設計v2](binder-slot-accessor-design-20260910.md)に[レビュー](binder-slot-accessor-review-20260910.md)の5点を反映した。
基本形を構文読取契約とschema既知の直接getterへ変更し、同じ経路で整数Slot/SpanとGo pointer/slice借用を比較する。
8入力の既存call-site集計を保存し、削減対象・残存費用・到達予算のgateを明示した。
構文writerの初期静的監査ではparser内部入口、bulk copy、Compact/Restore、別StoreのJSDoc/tokenも確認したが、
全推移的call graphとwriter guardの動的検証は未完了。2pass・値の取得・意味判定は維持する。
名前値memoを含むowner＋nameは既存研究の未完了項目として保持し、今回の主候補には組み込まない。
続く[walker codegen対照](walker-codegen-experiment-20260910.md)は固定規模で終了した。
baseline / 10関数への分割 / 同じ分割でwalkerのChildRefだけCALL化する3者をoverlayで比較。
20入力の意味・訪問順・全アクセサ呼出数が一致し、AST/Binder/Compilerテストも成功。
baselineと分割版はChildRefがinline、CALL対照は291箇所がCALLとなることを機械語で確認した。
通常GC wallは全pairで非有意。先行wall A/Aは通過したが、後続比較でばらつきが拡大した。
KPC自己検証は全3者成功。今回のA/Aは命令数精度を満たす一方、cycles精度が両入力で不合格となり、
事前protocolに従いKPCの3者比較を未実行（missing）として終了。分割の高速化・動的命令差は未証明、本体不採用。
その後、ユーザーが並行ベンチマークを停止したため、同じbinary・入力・10 bind×6 roundsで
[独立A/A再確認](walker-codegen-recheck-20260910.md)を実施。命令数は両入力、EL0 cyclesはdomが通過したが、
checkerのcyclesと両入力のwallは不合格。今回も3者比較はmissing、追加roundやgate変更はしていない。
測定後にmediaanalysisd等のCPU使用を確認したが、測定中の原因への帰属は未確定。
ユーザーの次の再計測依頼で[recheck-02](walker-codegen-recheck-02-20260910.md)を実施し、KPCのA/Aが両入力で通過。
保存binaryの固定6ラウンド3者比較まで完了した。分割は命令数の有意減なし、cyclesがchecker +1.33%（p=.015）、
dom +2.18%（p=.041）。同じ分割でのChildRef CALL化は命令数+1.00% / +1.07%（各p=.002）。
walker codegenの動的対照は完了し、今回の分割は本体不採用。通常GC wallは今回もA/A不合格で候補比較missing。
続いてユーザー指定の[-gcflags=-B対照](binder-bounds-check-experiment-20260910.md)を実施した。
3 walker×normal/Binderのみ-B/Binder＋astに-Bの9条件、2入力×10 bind×6 rounds。
新規6 buildの20入力監査一致とGo自動チェックの除去を機械語で確認。命令数A/Aが通過し比較を完了した。
元のwalkerはBinderのみ-Bで命令−4.65% / −3.57%、両packageで−6.72% / −4.81%（各p=.002）。
両packageに-BでもCALL化は同じ分割比で+1.30% / +1.05%残る。手書きslot検査は-Bで消えない。
cyclesはcheckerのA/A不合格のため結論を命令数に限定。フラグは診断専用、本体未変更。
分割幅の探索は広げず、次は構文writerの推移的監査・writer guardの動的検証とowner/shapeの契約確認へ進む。
その後の直接getter/整数保持/Go借用の対照では元のwalkerを共通に保ち、分割を必須条件にしない。
測定条件を整え、次の独立protocolでは命令数とcyclesの可測性を別々に事前判定する。
新アクセサの候補実装・採用検証は未実施。
未使用controlのDCEとlive rangeを機械語で確認できなければ、保持費の厳密な因果分解とは扱わない。

実験横断の結論は [現時点のinsight](binder-investigation-insights-20260910.md) に集約。個別ノートへのリンクと、再設計・採用検証の残課題を整理している。

**1. wall の比較基準・測定精度確認と、2. 8入力の wall 比較・操作数監査を実施済み**。
A/A は20 roundsでは不合格、事前に定めた40 roundsの独立確認では両入力とも
paired log-ratioの95%区間が±1.5%以内となった。raw・benchstat・identityは
`.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/` に保存。

Instruments CPU Profilerによるchecker/domの帰属と三者の共通走査microも実施済み。
優先仮説は共通child/list/header解決。子スロット借用sliceの候補はmicroで
11.75% / 13.67%悪化し、採用しない。KPCは事前検証でcounter逆行を検出し、
追試では初期化後の新規OS threadで逆行を解消し、両版・両イベント群の
GC込み自己検証とKPC A/A精度確認が通過した。8入力の命令数・cyclesと
4入力のload/store・cache比較が完了。6入力で追加225〜261inst/node。
cache missは減るが、命令数・load/store・cyclesが増えている。
walk/helper間の重複解決を呼出箇所別に監査し、構文上の宣言名を直前一件だけ
再利用する候補を実装・検証した。domでEL0命令数2.49%、cycles2.67%減少。
通常GC wallは非有意かつ区間が広く、wall3%短縮の採用条件は未達。
候補はoverlayに保存し、本体には採用していない。
その後、ユーザー指定で探索規模を2入力・10 bind × 4 roundsに縮小し、
アクセサ内部のlocal list owner呼び出しを除く対照を実施。実binder命令数は
checkerで1.51%、domで1.08%減ったが、cycles / wall改善の採用根拠は未達。
取得フィールド・呼出箇所の計数と、識別子Flags再利用B / A+Bの4者対照も完了。
BはFlagsAtを123,554 / 36,271回減らしたが命令数は減らず、スタック保存等が追加。
Aの命令削減は今回も約1.3%。B / A+Bは採用しない。
ListSlotAtのinline costを93→75に下げてCALLを除いたが、実binder命令数は減らなかった。
同じ本体のnoinline対照ではcheckerの命令数がinline比0.65%減り、展開の追加コストを示唆。
ListSlotAt候補は保留し、inline対象の網羅拡大を止める。
local owner A＋宣言名memo Nの4者比較（2入力・10 bind × 6 rounds）を実施。
domの組合せはwall 3.32%短縮（p=0.004）、命令3.66%・cycles3.52%減。
checkerは命令2.22%・cycles1.82%減、wall1.42%短縮は非有意。17入力の意味監査と単体テスト成功。
初回探索で次段階に進める候補を得た。本体未採用。次は測定精度を確認した独立セッションで
domを再確認し、baseline / 組合せの性能比較を8入力へ広げる。phase全体とGCの採用条件は未検証。
詳細は [owner＋宣言名memoの組合せ](owner-name-experiment-20260910.md)。
ユーザーのlazyな解決案に沿い、bindListRef一経路でowner/start/lengthを走査開始時だけ
解決するspanを探索（2入力・10 bind × 6 rounds）。命令はchecker1.42%、dom2.68%減、
dom cycles2.63%減。通常wallは−0.26% / −1.24%で非有意、3%条件未達。
17入力の意味監査・単体テスト・span境界試験成功。本体未採用。
短い有効期間内の解決再利用は有望だが、全体解決やGC改善は未証明。
owner＋nameの独立確認を引き続き優先し、spanの横展開は残存回数と追加価値を確認して判断する。
詳細は [list spanの限定実験](list-span-experiment-20260910.md)。
続いてユーザー指定の呼び出し経路対照を実施。Parameterで取得済みの名前を
Symbol宣言の後段まで渡す3者比較（baseline / 渡すが再取得 / 渡して再利用、2入力×10 bind×6 rounds）。
dom命令数は再取得対照比1.62%、baseline比1.46%減（ともにp=.002）。checkerは非有意。
両候補の17入力監査と単体テスト成功。cycles/wall改善は未確認、wall精度不足。
この限定的な機構確認を根拠に、ユーザーの条件付き指示に従って内部APIの再設計を仕様化した。
本体未採用。次は共通core化で診断ロジックの複製をなくし、同じ効果が残ることを確認する。
owner＋nameの独立確認・全採用ゲートは未完了のまま保持する。
詳細は [情報の受け渡し対照](threaded-name-experiment-20260910.md)、
[再設計の初稿](resolved-access-design-20260910.md)。
ユーザー提案のancestorスタックを3者対照（維持なし / 維持のみ / 維持＋識別子の親取得再利用）で検証。
20入力の親列・意味監査と単体/fallback試験成功。checkerでParentRef/KindAtを各123,553回削減。
ただし全298,054 nodeの維持費を回収できず、candidate命令はbaseline比+1.91%（p=.002）。
domは対象queryが0で命令+2.52%（p=.002）。通常wallは非有意、allocsは+7 / +5。
この全nodeスタック＋一消費箇所の構成は本体不採用。次候補は全祖先sliceではなく直近親情報の受け渡し。
詳細は [ancestorスタックの対照](ancestor-experiment-20260910.md)。
続いてgenerated walkerの既知親refを引数で渡す3者対照を実施（2入力×10 bind×6 rounds）。
20入力の親情報・意味監査と単体/fallback試験成功。checkerの親取得104,998回（約85%）を置換。
再取得対照比の命令は1.14%減（p=.002）だが、baseline比−0.35%は非有意、domは+0.51%（p=.002）。
通常wallは非有意、allocs中央値は不変。本体不採用。
compiler JSON診断で、引数追加後のwalkerがbig callerとなり、ChildRefのcost36がinline上限20を
超えることを確認。次は親引数を広げず、このcodegen変化の費用を独立に切り分ける。
詳細は [親ref引数の対照](parent-argument-experiment-20260910.md)。




詳細は [ListSlotAtとnoinline対照](listslot-experiment-20260910.md)。
詳細は [取得フィールドと呼出側の対照](accessor-fields-experiment-20260910.md)。
詳細は [アクセサ全体の小規模探索](accessor-exploration-20260910.md)。
詳細は [宣言名再利用実験](name-memo-experiment-20260910.md)。
生成walker内ChildRefは全体の約34%（checker）であり、これだけを全ChildRefの寄与として扱わない。
詳細は [KPC入力別結果](kpc-binder-results-20260910.md)。
詳細は [KPC信頼性の追試](kpc-reliability-20260910.md)。
詳細は [今回の実験ノート](binder-investigation-results-20260910.md) を参照。
pprofは帰属が不正確なため使用しない（ユーザー指定）。既に取得したpprofは根拠から除外。
現時点で新しいbinder最適化は採用していない。

IfStatement と PropertyAccessExpression の子参照一括取得は、micro では短縮したが
実 binder の有意な改善は未確認。PropertyAccessExpression の候補はワークツリーに
残っている。次の独立実験では保存済み baseline overlay で外し、介入を混ぜない。

関連資料:
- `store-bind-bottleneck-audit-20260909.md`
- `parent-syntax-bind-20260909.md`
- `parent-syntax-property-access-20260909.md`

## 進める順序

### 1. 比較基準を固定し、測定精度を確認する

- 既存 artifact を先に分析し、identity と dirty 差を確認する。
- Store baseline と未改変 pointer の比較条件、fixture、Go/CGO、GC 設定を固定する。
- 同一バイナリを交互に測る A/A で、wall time と KPC のばらつきを確認する。
- 意思決定に必要な最小改善量を測定前に定める。今回はユーザー確認により **binder 全体で3%以上** を採用。最適化範囲はStore固有の追加コストに限定する。
- その差を判断できない精度なら、局所変更を増やす前に測定条件を整える。
- wall time が非有意でも命令数測定を一律に打ち切らない。命令数が減っているが効果が小さい場合と、実 binder では減っていない場合を分ける。
- fixed EL0+EL1 と EL0 限定 counter を混同しない。

### 2. 代表的な実入力を6〜10個程度に広げる

- 宣言・型定義、関数本体・分岐・ループ、property access・call、import/export、JavaScript、JSX/TSX を含むよう選ぶ。
- 各入力で ns/op、B/op、allocs/op を比較する。命令数・cycles は特徴の異なる代表入力から測る。
- 計測とは別のバイナリで node 訪問数、child/list アクセス数、識別子判定回数、FlowNode 数などを数える。
- 入力ごとの比率と絶対差を記録する。単に多数のベンチを回すことを完了条件にしない。

### 3. 傾向から最小の対照実験を選ぶ

| 観測 | 次の対象 |
| --- | --- |
| 多くの入力で node/edge 当たりの追加命令が似る | 共通走査・Store 解決 |
| 識別子判定が多い入力で追加の退行 | 親 header＋name、text 取得 |
| CFG が多い入力で追加の退行 | 条件式処理、Flow 参照・生成 |
| 時間より割り当て・scan の差が大きい | FlowNode／Symbol の表現縮小 |

- 相関だけで原因と断定せず、疑わしい経路を分離した最小ベンチで pointer／Store を比較する。
- profile の構成比だけでなく、両版の絶対時間・命令数の差を見る。
- 広い hot path と、狭い修正対象を区別する。symbol synthetic は方向確認に限定する。

### 4. 一括取得は範囲を限定して追加検証する

- 1〜3の結果で優先度を確認してから進める。kind を順番に増やす調査にはしない。
- PropertyAccessExpressionの親header＋nameは候補の一つ（checkerで46,304回）。今回の優先仮説は共通走査であり、追加証拠なしにこの候補へ切り替えない。
- 先に対象経路の差と削減可能量を見積もり、keyword 判定・診断条件は維持する。
- 実 binder の命令数がほとんど減らない、または成功しても全体への寄与が小さいなら当面保留する。
- 設計全般を完全に否定するまで続けるのではなく、目標に必要な削減量を得られるかで継続判断する。

## CFG／FlowNode の扱い

CFG の最適化を先に決めず、入力間の切り分けから優先度を判断する。
宣言ファイルにも退行があるため、CFG 単独で共通の退行を説明できるとは限らない。

FlowNode 縮小は「表現の追加コストを減らす実験」として扱う。
既存の allocation driver は symbolIdx／flows 列、symbolRefs、FlowNode の32→48 B化、
Symbol／Declarations の Handle 拡大。縮小によるメモリ、cache、GC、CPU の効果は
それぞれ測定し、CPU 主因の解決を先に約束しない。

## 判断・記録

- 次の調査単位の完了条件は「代表入力ごとの退行表を作り、優先する原因仮説を一つ選ぶこと」。
- raw を保存し、比較は benchstat。非有意差を効果ゼロ・同等性の証明としない。
- artifact は current / stale / missing / unsupported を使い、current の identity 条件を守る。
- 採用時は実 binder の改善、命令数・cycles、parse+bind、live/scan、診断・symbol・CFG・走査順を確認する。
- pointer 同等性能と、メモリアクセス・GC 双方の高速化は、局所改善と分けて判定する。


### setter検査削減の追加診断（2026-09-10）

[setter対照](binder-setter-check-experiment-20260910.md)を元のwalkerの4条件で完了。
Binder＋astの-Bにsetter phase/live/local所有権検査削減を加えると、EL0命令は追加−0.59% / −0.39%、
通常版比の合計−7.29% / −5.07%。通常GC wallは合計−3.03% / −1.30%の中央値だが非有意かつA/A不合格。
4条件の20入力監査と追加local所有権assert監査は一致。実験用overlayのみで本体不採用。
この小さい追加効果を主な到達手段とはせず、構文writer監査・直接getterの既定順序へ戻る。


### list/child配列共有の依存load監査（2026-09-10）

[現行sourceと最小機械語対照](list-layout-access-audit-20260910.md)で、単純な配列分離では依存段数が減らないことを確認。
index slotからlist headerへの間接参照をなくす直接descriptor配置か、loop外のSpan/slice解決が必要。
ListSlotAtのatomicはlistRef→ID、ListLen/ListElemはlistOwner→ID。新規実Binder benchmarkは未実行。
共有list identity/aliasの契約を維持する設計を先行し、配列分離とSpanの効果は独立対照にする。


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


### Luna向け実装計画（2026-09-10）

[具体的なカードと初回依頼文](luna-accessor-implementation-plan-20260910.md)、[進捗表](luna-accessor-implementation-progress-20260910.md)を作成。
P0で現在snapshotを検証する共通driverを整備し、P1〜P5の局所走査、P6〜P8の共通helper、P9以降の契約確認へ進む。
最新のユーザー方針に合わせ、局所カードのwall有意差を進行条件にしない。意味・訪問順と機械語を確認し、checkpointで累積benchstatを取る。
親/headerの横断設計は別カードで前提を確定する。今回実装・benchmark・Lunaへの作業送信は未実施。


### API命名・生成方針の改訂（2026-09-10）

ユーザーの命名方針を反映し、今後のcallerは`AccessParameter(ref)`→`ParameterAccessor`のようなnode別生成APIを使う。
数付きSyntaxChildrenNの新規横展開は行わない。返却は名前付きNodeRef/ListRef fieldを持つ値snapshotで、内部でkind/schemaを検査しheader/startを一度解決する。
Walkは訪問処理の名前として区別する。既存arity APIは移行中の互換用に保持する。
[改訂実装指示書](luna-accessor-implementation-plan-20260910.md)へG0（生成）、G1（完了済みP1の移行）、G2（walker等の移行）を追加。
P0/P1の完了履歴とartifactは保持する。struct返却・未使用list解決・inline/spillを新たに検証し、旧結果を新APIの性能根拠にしない。今回の変更は計画文書のみ。


### 生成Accessorレビュー修正（2026-09-11）

[修正記録](accessor-review-fixes-20260911.md)。未使用list解決、OptionalChain後段、FunctionExpression名の再取得を修正。任意関数の通常ビルドobjdumpとG0境界試験を追加。Binary専用アクセサのmap解決消失を確認した一方、OptionalChain親側frameは112→176 B。局所の機械語削減を全体の速度向上とは扱わず、V3のwall/KPC・parse+bind・GCはmissingのまま追跡する。


### 2026-09-11 固定規模の測定完了

[測定結果](accessor-review-measurement-20260911.md)。レビュー修正前後でcheckerの命令−1.24%、cycles−2.09%（各p=.002、KPC A/A合格）。domはbenchstat有意差なし。bind/parse+bindのwallは非有意かつA/A不合格。GC probeはcurrentだが、Store保持とA/A変動のためGC改善は未証明。旧missing記載は測定前の履歴として保持する。pointer同一セッション比較・制御したGC評価は未完了。


### 現行Store対pointerのbind単体再計測（2026-09-11）

[直接比較](current-pointer-bind-recheck-20260911.md)。同一ハーネスで両版を新規ビルドし、checkerは1.320倍、domは1.277倍の時間（benchstat p=.002/.004）。wall A/Aは両版とも不合格なので倍率の精度は留保。bind割当bytesも+72.38%/+48.85%残る。CLIの全プロジェクトBind timeは並列処理等を含む別の範囲であり、次は同一プロジェクトの逐次／並列条件を対照する。


### 測定方法レビューによる留保（2026-09-11）

[方法レビュー](bind-benchmark-method-review-20260911.md)で、Store登録未解除によるiteration間のAST保持と、反復ごとの強制GCの影響を確認。約1.3倍は元ハーネスでの観測値として保持し、Binder固有の退行率としての採用は保留する。次はASTの寿命条件を揃え、登録数を監査してから再計測する。今回の追記に伴うbenchmark再実行・実装変更はない。


### 寿命を揃えるハーネス修正（2026-09-11）

[実装・検証記録](bind-batch-lifetime-fix-20260911.md)。最大10個のdistinct/unbound ASTを事前準備し、batch全体を一度計測してから登録解除する。両版の寿命テスト・上限検査成功。Store登録数は毎batchで0へ復帰。通常GC／GC無効の計96行は保存したが、A/Aは全条件不合格。測定中のlive binder.go変更も検出したため、固定snapshotの観測値として扱い、現在コードの確定倍率には使わない。


### 負荷低下後の最新ソース再測定（2026-09-11）

[再測定結果](bind-batch-recheck-20260911.md)。修正済み寿命ハーネスで通常GC／GC無効の96行を取得し、両版のliveソース・binary・overlay一致を確認。通常GCは主比較checker +31.39%、dom +34.85%（benchstat各p=.002）、確認用比較も同方向。GC無効でも約29〜34%の差が残った。A/Aは8条件中2条件のみ合格で精度gate全体は未達。最新コードで約1.3倍の遅さが再現する方向は支持されるが、確定倍率の採用は保留。追加roundなし。


### 最新Instruments CPU Profilerによる帰属（2026-09-11）

[CPU分析](binder-instruments-analysis-20260911.md)。前回再測定と同じbinaryでchecker/domの両版を各20秒attach採取。4 trace成功、終了時のliveソース・binary一致。広いhot pathはwalk/helpers。新たにLocals取得/登録とNextContainer登録のmap leafをchecker 2.97%、dom 4.14%へ帰属した。inlineされたFlagsAt、Span要素参照、modifier list再解決、Flow ID変換も残る。これらの構成比は削減可能wallではなく、約30%差の全額因果分解は未完了。次の限定対照はLocals/NextContainerのStore表現とmodifierの値受渡し。今回本体修正・追加wall benchmarkなし。


### 4つのCPU経路の詳細設計（2026-09-11）

[詳細文書](binder-four-bottlenecks-design-20260911.md)にpointerの仕組み、Storeとの差分、新規表現/API、メモリ費用、改善量の感度計算、独立対照と受け入れ条件を記載した。追加確認としてpointerはModifierListのFlagsを構築時に保持する一方、Store Binderは再集計することを特定。Locals/NextContainerの表現対照に続き、pointer同等のModifierFlags保持を独立候補とする。新規実装・測定・agent委譲は未実施。従来のCPU割合をwall削減量とは扱わない。


### 修正済み寿命ハーネスによるPMU計測（2026-09-11）

[PMU結果](bind-batch-pmu-20260911.md)。両版の実counter自己検証4 process成功、通常GC／GC無効で固定96行を取得。GC無効の命令/cycles A/Aは全8条件合格。主比較でchecker命令+50.64%・cycles+33.18%、dom命令+42.22%・cycles+29.18%（各benchstat p=.002）。L1D load missは約13%減少するため、miss増加だけでは遅さを説明できない。両版のidentity・全Goソース・binary・overlay一致。通常GC Store/domの命令/cyclesと一部wallはA/A不合格。PMU下の絶対wallは非PMUより大きく、通常wallと混合しない。次はLocals/NextContainer表現とpointer同等のModifierFlags保持を独立対照する。本体変更・追加round・commitはなし。
