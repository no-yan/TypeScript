# binder-rewrite 改善計画

2026-09-15。既存の計測結果とユーザー提示の追加費用一覧に基づく計画。今回は文書作成までとし、追加計測・実装・候補の採用は行わない。

## 1. 問題と目標

`85506e8`（A、cursor版に相当）から `80d8b41`（B、rewrite版）への移行で、同じASTをbindする命令数が約21%増えた。node訪問数とFlow生成数は変わらず、kindの再取得、Handleを介するquery、listの所属・headerの繰り返し解決が増えている。

**取得済みの構文情報を短い範囲で再利用し、Store固有の追加アクセスを減らす。** 移行で修正された診断・Symbol・Flowの意味はBを正として維持する。各候補が21%のうち何%を説明するかは未確定であり、削減率を事前に約束しない。

実施順は、保存済みlist span候補 → 関数内のkind/Handle再利用 → 識別子 → modifier・宣言 → 条件式・Flowとする。関数間のkind伝搬は独立した性能例外として、その後に判断する。広いhot pathはwalk/helper境界全体にあり、最初のlist候補はそのうち狭く変更しやすい部分である。

## 2. 比較対象と既存証拠

| 項目 | 固定値 |
| --- | --- |
| 選択repo | `/Volumes/SanDisk1TB/worktree/binder-rewrite` |
| A | `85506e8b7d51f2b05f84f9ba1a8c3309b2f30ce4` |
| B（作成時HEAD） | `80d8b41ccfb046a551b721cb04f909205d66513a` |
| 中間M | `6f6f3ae597fdd73bb3a28b172fe8b647a2694bb8` |
| 候補checkout | cursor-ast-store-tests、pointer main、flownode、lock系、store-pr系など。全一覧は保存物の `worktrees.txt` |
| 使用したsource | 選択repoから作ったA/Bの独立git archive。別checkoutのdirty sourceは混ぜていない |
| artifact set | `/Volumes/SanDisk1TB/Library/Caches/binder-85506e8-80d8b41-20260915-run1` |
| 環境 | Apple M1 / 8GB、Go 1.26.0 darwin/arm64、macOS 26.6.2、GOMAXPROCS=8 |

主比較・操作数・Instruments・意味監査は、記録した `repo_root`、`tsgolint_git_rev=null`、`typescript_go_git_rev=B` とsourceを照合した `current` の証拠。ここでのcurrentは今回の固定A/B比較に対する状態であり、将来の候補実装の性能を証明しない。再開時にはidentityとsourceを再照合する。

| artifact / stage | status | 判断上の制限 |
| --- | --- | --- |
| 主A/Bのwall・KPC、操作数、Instruments、意味監査 | `current` | wallの精度合否とartifactの鮮度は別 |
| A/M・M/B・9入力拡張のwall、保存済みspan候補のwall | `current` | wall精度未達。対応するfollowup全体はcomplete=false |
| followupおよびspan候補のKPC | `missing` | 改善効果と中間コミットの命令数寄与は未確定 |
| 候補の全conformance、parse+bind、retained live/scan、CLI | `missing` | 採用に必要な全体検証は未完了 |
| 旧foundation・narrowableのartifact set | `stale` | 今回のA/B数値へ混ぜない |

今回完了したstageに `unsupported` はない。再利用時、stageのartifactは存在しても `bench.txt` に要求regexの `Benchmark...` 行がなければ `unsupported`、未取得の結果は `missing` と記録する。

### 主比較の測定値

1 opはparse済みの独立ASTを1回bindする処理。時間・メモリは通常GC、命令・cyclesはGC無効KPCの別stage。差は保存済みbenchstatによる。詳細・再現条件は[性能比較結果](commit-performance-results-20260915.md)。

| 入力 | 指標 | A | B | 差 |
| --- | --- | ---: | ---: | ---: |
| checker.ts | ns/op | 16,088,731.0 | 18,266,012.5 | +13.53% |
| checker.ts | B/op | 12,798,224.0 | 12,777,532.5 | −0.16% |
| checker.ts | allocs/op | 14,163.0 | 14,165.0 | +0.01% |
| checker.ts | EL0 inst/op | 165,461,457.5 | 200,163,250.0 | +20.97% |
| checker.ts | EL0 cycles/op | 78,310,183.0 | 90,931,889.5 | +16.12% |
| dom.generated.d.ts | ns/op | 6,059,273.0 | 6,880,706.0 | +13.56% |
| dom.generated.d.ts | B/op | 7,869,560.5 | 7,867,973.5 | −0.02% |
| dom.generated.d.ts | allocs/op | 16,682.5 | 16,683.0 | +0.00% |
| dom.generated.d.ts | EL0 inst/op | 68,632,573.5 | 82,910,635.0 | +20.80% |
| dom.generated.d.ts | EL0 cycles/op | 29,428,199.5 | 33,574,075.0 | +14.09% |

命令数・cyclesは前後両版のA/A精度条件を満たし、主比較・確認用ともp=.002。通常wallは精度未達のため約13.5%を確定した時間回帰率とはしない。

### 診断の根拠と限界

- checkerのBindEntriesは両版298,054、domは両版109,605。KindAtは554,467→1,173,328 / 156,084→426,319、Atは0→708,951 / 0→269,003。AもHandleOf等を使うため、At=0はHandle生成ゼロを意味しない。
- ListLenは40,101→113,697 / 29,088→60,375。TryBindListSpanは71,202→0 / 51,044→0。失われた経路と実行回数の両方を確認できている。
- InstrumentsのB側exclusive leafでは、checkerのbind 13.73%、bindChildren 6.05%、checkContextualIdentifier 5.16%、GetContainerFlags 2.34%。domのbind 10.55%、bindChildren 3.64%、declareSymbolEx 2.97%。構成比をそのまま削減可能率へ換算せず、inclusiveの重複分も加算しない。
- allocation driverはFlow・列・Symbol・Localsが候補だが、今回全allocation stackへ増分を帰属したわけではない。中間Mで増えた約9,900 allocs/opはBの `ArgumentsSeq().All()` 修正でほぼ解消済み。残る命令回帰をこの一時slice生成で説明しない。

## 3. 提示された11候補の扱い

ソース参照はB固定時点。呼出箇所数は動的な頻度や費用順位ではない。「KindAt 1回9命令」「識別子41%」はユーザー提示の機械語・入力に依存する値として扱い、今回の全入力へ一般化しない。分岐の短絡評価によって、同じ関数でも実行される取得回数は変わる。

提示されたAt 96箇所・KindAt 82箇所は今回の固定sourceの件数として採用しない。Bの `binder.go` を文字列 `b.store.At(` / `b.store.KindAt(` で数えると103 / 90箇所。関数全体の同じ集計ではbindChildrenのKindAtは4箇所、bindContainerは10箇所、checkContextualIdentifierのAtは6箇所・FlagsAtは3箇所となる。これは引数や分岐を区別しない静的集計であり、1回の呼出中に全部実行されるという意味ではない。

| # | 対象 | 追加費用の見立て | 改善単位 |
| --- | --- | --- | --- |
| 1 | `binder.go` の `At`、`GetContainerFlags`（2620） | `At`はheaderからkindを読んでHandleを作る。広い入口と共通query境界に分布 | 既知kindでHandleOf、関数内Handle再利用、必要なkindだけ親・initializerを取得（P1） |
| 2 | KindAt、bindChildren（1659）、bindContainer（1486）、bindEachChild | 同じnodeのkindを関数内・境界で読み直す | 関数内に集約（P1a）、関数間伝搬は別候補（P1c） |
| 3 | `ast/utilities.go:299` のIsIdentifierName | 親Handleの復元、多相Name、Handle同一性比較 | 同一Storeのref経路と境界fallback（P2a） |
| 4 | checkContextualIdentifier（1353） | Flags・Handleを複数箇所で取得。実行回数はkeywordと診断分岐に依存 | 安全な範囲でflags・Handleを再利用（P2b） |
| 5 | HasSyntacticModifier → `ast/store_query_manual.go:226` | Modifiersのdispatch後、ListAtで要素ごとにHandleを作る | modifier listのrefとkindで判定（P3a） |
| 6 | bindVariableDeclarationOrBindingElement（1212） | 3つの述語が別々に親を探索し得る | 共有可能な親・宣言情報のみ再利用（P3b） |
| 7 | declareSymbolAndAddToSymbolTable（455） | enum・type/object/interface等の分岐でcontainer Symbolを2回取得 | 分岐内の局所変数へ1回取得（P3c） |
| 8 | bindParameter（1229） | parameter property判定にnode・parent Handleを作る | 現在の親とmodifier情報で同じ判定（P3d） |
| 9 | createFlowCondition（505）、bindCondition（1809） | expression Handle、親取得、narrowing・括弧のHandle再帰 | 局所再利用から始め、再帰ref化は分離（P4a） |
| 10 | newFlowNodeEx（479） | FlowNode.Nodeへ保存するHandleのkind再取得 | 既知kindを使える経路だけ別評価（P4b） |
| 11 | bindEachStatementFunctionsFirst（1774） | ListLenを各passで取得、ListElemとKindAtを2passで実行 | list spanを共有し2passは維持（P0b） |

`Handle.Parent()` はlocal parentが存在すれば即returnする。externalParent mapを読むのはlocal parentが0の場合だけであり、Blockごとにmap lookupが発生するとは限らない。`FlowNode.Node` は実際にHandleであり、合成payload用の `Data *Node` とは別フィールドである。

## 4. 改善する単位と順序

各小項目を別差分にし、未評価の変更を積み上げない。最初はB対単独候補、組合せは有望な候補を選んでから扱う。

### P0. 既存list span経路を再利用する

**P0a：bindEachだけ。** 保存済み6行overlayを最初の候補とする。local listのowner/start/lengthをループ開始時に1回解決し、要素は既存のBindListSpanElemで読む。nil・foreign listでは元のListLen/ListElemへfallbackする。

- 根拠：A/Bの操作数と129箇所の生成walkerのlist呼出経路。候補はBとの11入力意味監査で差0。
- 現状：候補wallはchecker −2.39%、dom −2.81%だが精度未達、KPCはmissing。改善済みとは扱わない。
- 制約：spanを同じStoreだけで使い、list start/length変更、Compact/Restoreをまたいで保持しない。要素値は都度読む。Flags/Symbol/Flow更新はspan metadataを無効化しない。
- 機構の合格条件：対象local listでowner/headerの繰り返し解決が減り、訪問順・診断・Symbol・Flowが一致する。性能判断は第6節による。

**P0b：functions-firstを別差分で。** 同じlist spanを2passで使う。関数宣言を先にbindしてから他の文をbindする順序を維持し、要素とkindの2pass分の読取はまず残す。1pass化、関数一覧の追加slice、kind配列のキャッシュは含めない。

### P1. walk/helperの重複解決を減らす

**P1a：関数内でkindを1回取得。** bindChildren・bindContainerの同じnodeへのKindAtを局所変数へまとめ、既存生成getter/dispatchへそのnode自身のkindを渡す。既知kindがあるHandle境界では既存 `ast.HandleOf(store, ref, kind)` を使う。単に `At` の直前へ新しいKindAtを追加する置換は削減にならない。

**P1b：GetContainerFlagsの入口を軽くする。** まずbind内で取得済みkindからHandleを作る限定差分。その次に、kindだけで決まる分岐と、親・initializerの読取が必要な分岐を分ける。公開Handle入口とBinderのref入口を設ける場合、意味を決めるswitchは共通化する。Blockだけでなくmethod/accessor・PropertyDeclarationの判定を保ち、外部Storeを同一Storeのrefとして解釈しない。

**P1c：関数間のkind伝搬は独立対照。** 上記の後、bind→bindChildren等の残る再取得を調べる。既存の[変換規約](upstream-to-store-translation.md)は意味処理間の恒常的なkind伝搬を基本形に含めないため、対象境界・kindの有効期間・上流との対応を文書化した性能例外として扱う。旧bindKind層全体の復元、全node用の状態スタック、新しいref/kindの常設構造体へ広げない。

- 根拠：KindAtの動的増加とbind/helperの広いCPU占有。
- リスク：引数・変数の追加でlive rangeが延び、stack保存やCALLが増える。過去の親引数・Flags再利用実験では取得数削減が命令数削減に直結しなかった。
- 機構の合格条件：対象取得数が減り、同じ最適化条件の機械語で新しいCALL・spill・inline境界の変化を説明できる。取得数だけで合格としない。生成コード変更時はgeneratorと生成物を同時に更新する。

### P2. 識別子の共通queryを軽くする

**P2a：IsIdentifierNameの同一Store経路。** 親ref・親kindを取得し、必要な親だけ生成ref getterで名前を比較する。PropertyName、QualifiedNameのright、import/export、JSXの分岐をそのまま再現する。親が別Storeまたは同一Storeを証明できない境界は既存Handle queryへ戻す。全祖先を保持する仕組みは追加しない。

**P2b：checkContextualIdentifierの局所再利用。** flagsを読む区間に書込・再帰がないことを確認して再利用する。必要になったHandleを同じ分岐内で再利用し、keywordでない大半の識別子に余計な診断用データを先行取得しない。

- 根拠：checkerでcheckContextualIdentifierがexclusive 5.16%。ユーザー提示の識別子41%は普遍的な構成比として使わない。
- 正確性条件：strict mode、await/yield、ambient/JSDoc、既存診断による抑制、予約語を使ったproperty/import/export/JSXで診断内容・順序・spanがBと一致する。
- 機構の合格条件：親/名前の重複解決が減る。FlagsAtは主比較全体では増えていないため、これだけを回帰主因としない。

### P3. modifierと宣言の重複queryを減らす

**P3a：modifierをrefで走査。** 既存modifiersRefGeneratedとlist要素のkindを使い、同じModifierToFlagの規則で判定する。まず必要なqueryだけを置換し、永続的なModifierFlagsキャッシュは導入しない。nil、decorator、修飾子を持てるkind、別Store listの振る舞いを維持する。pointer版の1 loadとの差はA/B回帰と分ける。

**P3b：変数宣言の親情報を共有。** require初期化、block/catch scope、parameter所属の述語から共通読取を切り出す。単に直近親だけで結論を出さず、入れ子のBindingElementを辿る条件と診断順を維持する。

**P3c：container Symbolを分岐内で再利用。** enum・type/object/interface等の2回のSymbol取得を1回にする。GetMembers/GetExportsによるmapの遅延作成は元の位置に保つ。宣言・再帰・container切替をまたいでSymbolやmapを保持しない。この小差分は独立して評価できるが、全宣言で2回減るとは見積もらない。

**P3d：parameter property判定。** P3aのmodifier取得を利用し、constructor親・parameter名・修飾子の条件を既存queryと照合する。引数Symbolの宣言とclass propertyの宣言順、分割代入parameterの名前付けを維持する。

- 合格条件：対象queryの結果・Symbol/LocalSymbol/Exports/Members・重複宣言診断がBと一致し、一時sliceや長寿命キャッシュを追加しない。
- 必要なら最小microはexport-heavyな一意宣言から始める。ただしdeclareSymbolEx/GetSymbolTableが支配的でdeclareModuleMemberも目立つことを確認してから選ぶ。symbol syntheticの短縮は実入力のボトルネック証明にしない。

### P4. 条件式とFlow生成のHandle境界を減らす

**P4a：expressionの局所再利用を先行。** createFlowCondition・bindConditionで同じ式のkind/Handleを再利用する。isNarrowingExpression、containsNarrowableReference、SkipParentheses等のref化は別差分に分け、最も頻繁な経路から扱う。

**P4b：newFlowNodeExへの既知kind利用。** 呼出元がすでに正しいkindを持つ経路に限定する。FlowNode.NodeのHandle保存とDataの合成payloadを維持し、Flow全体の表現や所有権は変更しない。

- 正確性条件：短絡評価、optional chain、nullish coalescing、括弧、論理代入、true/false分岐、unreachable、exception/antecedentの順序を維持する。BのArgumentsSeq().All()をslice生成へ戻さない。
- 走査全体ではJSDocのname-first、関数宣言の先行処理、containerの保存・復帰、EndFlow/ReturnFlow/FallthroughFlowの保存先も不変条件にする。
- リスク：再帰のref化は変更面が広く、共有AST helperとの意味の二重管理を生む。局所再利用の効果が確認できるまで全面移植しない。

## 5. 今回の改善系列へ混ぜない費用

ユーザー提示のSetFlowの2段CALL、3つの二分探索switch、Access系検査、side map、PrepareBindTablesの列zero埋めは、cursor版にも残る共通費用として別系列で扱う。機械語上の段数・switch形状は対象binaryで再確認が必要である。

今回の約21%をこれらへ一括帰属しない。P0〜P4後にも支配的なら、列準備・map/hash・dispatchの個別計画を作る。unsafeなガード除去、診断用 `-B` の本番利用、構文writer契約を省略したslice借用は改善案に含めない。

## 6. 再開時の検証と受入条件

この節は将来の実施手順であり、今回新しく実行する作業ではない。[既存調査順序](binder-investigation-plan-20260910.md)と[KPC手順](kpc-measurement-workflow.md)を使う。共通CLIは未実装なので前提にせず、保存済みdriverを新しいrun用に適応する。

1. **証拠を再確認する。** 既存raw・benchstat・identityから開始する。新しい候補は別run directoryへ保存し、repo/revision、dirty差またはoverlay hash、compiler、binary、fixture、計測設定を記録する。旧rawへ追記しない。
2. **差分と意味を先に確認する。** Bとの11入力意味監査と対象に合う境界試験を行い、関連package testを通す。A/Bの15差（JSON同期、static block/caseのFlow所有、匿名関数Symbol名）は意図した修正として保ち、候補対Bの新しい未説明差は0にする。
3. **一候補・二入力で命令数の対照を取る。** 既存checker/domの同一fixture・寿命安全な10 AST batch、fresh process、固定6round・4ラベルの設計を再利用する。前後の自己比較も含め、命令数は95% CI全体が±1%、cyclesは±1.5%以内という精度条件を別々に判定する。負荷のあるbuild・profile・他benchmarkを同時実行しない。
4. **rawからbenchstatで判断する。** 前後の `bench.txt` を保存し、`benchstat old.txt new.txt` を使う。主比較と確認用は別に扱う。精度不足・欠測なら比較不能と明記し、roundを結果に応じて追加しない。命令が減ってもcyclesやwallの短縮を自動的に主張しない。
5. **有望な候補だけ範囲を広げる。** 個別効果を確認後、組合せの相互作用をB対累積候補で検証し、Aとの差がどこまで残るか報告する。保存済み9入力manifestを利用してJS/JSDoc・宣言・Flow・TSXへ広げる。A/M・M/Bのキャンペーンを毎回やり直さない。
6. **採用前に全体を確認する。** wall精度を確保した独立比較、parse+bind、保持時live/scan、代表project CLI、固定候補commitでのconformanceを行う。baselineの自動承認で差を消さない。

**探索を続ける条件**は、意味の一致に加え、精度を満たした命令数比較で削減が再現し、cycles・allocationに説明できない悪化がないこと。取得数だけ減る候補や、機械語のCALL/spill増で相殺される候補は積み上げず、その単位で保留する。

**採用条件**は既存計画の「Binder全体で3%以上のwall改善」を引き継ぐ。これは各小差分の必須効果量ではなく、選んだ最終候補に対する条件。通常GC wallの精度、対象実入力での再現、全体処理・メモリ・正確性の検証が揃うまで採用済みとしない。効果のない入力や悪化入力も省略しない。

## 7. 次の行動と完了状態

- 今回：11候補の整理、既存証拠との照合、優先順位・変更境界・受入条件の文書化まで完了。
- 再開時の最初の作業：保存済みP0a候補のidentity・差分を確認し、Bとの命令数対照でlist経路の寄与を確定する。
- 次の実装候補：P1aの関数内kind再利用。関数間のkind伝搬やFlow helperの全面ref化と同時に行わない。
- 未完了：改善の確定、追加実装・計測、候補採用。今回は開始しない。

参照：[性能比較結果](commit-performance-results-20260915.md)、[コミット比較計画](commit-performance-plan-20260915.md)、[Store変換規約](upstream-to-store-translation.md)、[基盤移行の意味差](foundation-migration-results-20260915.md)、[親引数対照](parent-argument-experiment-20260910.md)。
