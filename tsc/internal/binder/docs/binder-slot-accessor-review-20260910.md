# Binder slot accessor設計の敵対的レビュー

2026-09-10。対象: [binder-slot-accessor-design-20260910.md](binder-slot-accessor-design-20260910.md)。設計書、Store/Binder/generator実装、既存raw・identity・Instruments帰属を照合した。新しいbenchmark、候補実装、回帰テストは実行していない。対象設計書と既存の未コミット変更は変更していない。

**判断: 局所的な機構実験としては妥当だが、pointer同等を目指すアクセサ設計としては修正を求める。最優先は、追加する型の設計より、既知のshape・owner・構造の有効期間を失わない内部契約と、削減可能量の見積もりである。** 以下のP1/P2は設計上の優先度であり、未実装の疑似コードに本番バグが存在すると断定するものではない。

値と位置を区別すること、functions-firstの2passを維持すること、missing/foreignを区別すること、GC効果を別に判定することは維持したい。名前memoや共通アルゴリズムの変更をStore固有の改善へ混ぜない比較方針にも異論はない。

**1. [P1] 必要な削減量と、対象アクセサで除ける仕事を結ぶ予算がない**

対象: 設計書§1、§8、§9。必要なwall短縮はchecker 34.65%、dom 28.51%と正しく記載されている。しかし、typed slotとspanが合計で何回の解決を除き、何を残すかが未定のまま、局所候補の実装へ進む仕様になっている。「到達を保証しない」と「この候補を目標へ向けて優先する根拠がある」は別の条件である。

証拠: checkerの生成walker内ChildRefは189,849回、全体でも558,230回。既存KPCの追加命令は約74.36Mである。追加命令をすべて取り戻すという仮の命令予算なら、生成walkerの対象readだけでは約392命令/read、全ChildRefでも約133命令/readを除く必要がある。これはwallの厳密な下限・上限ではなく、対象範囲の桁を点検する計算である。命令がpointerと同数にならなくても時間は一致し得る。一方、先行spanの命令削減は実際に全体の数%で、wallは非有意だった。

再現: [KPC call-site表](kpc-binder-results-20260910.md)とA/paired-8-20のraw・benchstat、K/paired-instructionsを照合する。異なる実験の改善率は足さない。

仮説: 重複した親header解決だけでは、残るchild→header、text、Symbol/Flowの参照、関数境界などを含む追加費用の大部分を除けない可能性がある。現時点では主因全体の帰属は未確定。

提案修正: 対象callerごとに「論理query数、物理解決数、再利用できる区間、残る依存load、構築・保持費」を一覧化する。最初に手を入れる経路と、成功後に共通機構を適用できる範囲を明示する。現在の近接アクセス計数は候補抽出には使えるが、同じnodeへの近接アクセスをそのまま冗長な機械語load数としない。

受け入れ条件: 実装を横展開する前に、対象範囲の削減可能量が目標に対して十分大きいと考える根拠、または不足する場合に次に検討する表現変更を記載する。3%の局所改善とpointer同等到達を分ける現行条件は保持する。

**2. [P1] 「構造不変」をcallerごとの注意事項にしており、汎化を支える契約がない**

対象: 設計書§4「有効期間と安全性」、§5のspan有効期間。

証拠: 提案viewが保持するのはref/baseだけで、Storeとの対応や有効期間は型には表れない。例えばStore Aで開いたviewをStore Bへ渡すと、配列の範囲内なら別のchildを読める。Restore後に同じ領域を再使用しても、Goの配列境界検査では古いviewを検出できない。設計書はこれらの使用を禁止しているので、これはその契約を守る呼出しでのバグ指摘ではない。ただし「型付き」で保証されるのは主に構文shapeであり、owner・lifetimeの保証と混同できない。

現行Storeのphaseはbuild/checkの2種類で、[Freeze/mustMutate](../../ast/store.go)は構文変更とFlags等の更新をまとめて禁止する。BinderはFlags・Symbol・Flowを更新するため、そのままFreezeを前倒しできない。一方、[bindSourceFile/bindKind](../binder.go)の直接の更新は意味情報が中心で、構文slotの変更・append・Restoreは今回確認したBinder本体では見当たらない。遅延JSDocも[store_query_manual.go](../../ast/store_query_manual.go)と[SourceFile.warmSharedJSDoc](../../ast/ast.go)では別Storeへ配置する。全推移的call graphの不変性を証明したわけではないが、より強い契約を調べる根拠がある。

再現: Storeの構文writer（appendSlots、SetChild、SetListSlot、SetListAt、Restore等）とBinderの更新箇所を照合する。実装段階では、計数専用buildでparse Storeへの構造書き込みと再配置を検出し、代表8入力・境界入力・遅延処理を通す。単なる実行前後のlen比較では、途中の変更やRestore→再appendを検出できない。

仮説: 「Store全体は可変」という広すぎる契約が、構文アクセスにも可変性・再解決・fallbackの費用を残している。必要なのは構文の安定性と意味情報の可変性の分離である。

提案修正: parse完了後の構文読取区間を定義し、ownerと構文変更条件をその入口で確立する。Flags/Symbol/Flow等の更新は既存経路で許可する。最初は検証buildのwriter guardでもよい。違反を各getterのrelease時チェックへ移して高速化を相殺しない。全ASTの再検査passや、全nodeに新しいcontextを持たせる必要はない。

受け入れ条件: どのwriterが区間中に禁止され、どれが許されるかを列挙し、view/spanを共有するhelper群が同じ契約を使えること。局所的な契約に留める場合は、その局所scopeがどこで閉じるかを示す。各viewへStore pointerやepochを必ず追加せよ、という要求ではない。

**3. [P2] より単純な「既知shapeの直接getter」が比較にない**

対象: 設計書§4のTryBindParameterSlots、§6の単発getter方針、§8の三者対照。

証拠: [Store.ChildRef](../../ast/store.go)はnil/ref、childLen、配列境界を毎回確認する。[Handle.childAt](../../ast/store.go)は既知slotのchildLenチェックを省く既存の小さい入口である。ところが[generate-go-ast.ts](../../../../tools/scripts/tsc/generate-go-ast.ts)のemitBinderChildと各field helperは、schemaとkindを知っていても汎用ChildRefを呼ぶ。提案Tryは、既知kindのcallerで再びkind/childLen/listLenを確認し、viewと成功フラグを作る。

再現: store.goのChildRef/childAt、generatorのemitBinderChild、bindwalk_generated.goのKindParameter caseを比較する。実装段階では同一のcaller・compiler条件で生成機械語を確認する。

仮説: 一部の利益はbaseを保持しなくても、生成された既知shape用の直接getterで得られる。その場合、viewの構築・fallback・live rangeの追加は不要である。逆に複数回の解決が本当に残る区間ではviewが有利になり得る。

提案修正: 三者対照に「schemaから生成した直接getter、base保持なし」を加える。内部では `children[nodes[ref].childStart+定数slot]` を読む最小経路とする。Goの配列境界検査と、既存のmissing/external契約は維持する。任意のshapeを受け入れる公開APIのチェックは削除しない。trustedな入口を使える根拠は生成producer/dispatchの契約に置き、検証buildで照合する。

list slot→spanも、typed node viewの採用から独立させる。Blockなどlistが一つの経路では、nodeから一度だけ直接local listを開ければよく、ノード型ごとの保持viewは必須ではない。「既知nodeから直接開く」「既存ListRefから開く」の二入口を共通のspan実装へ収束させる。

受け入れ条件: viewを採用する区間では、最小の直接getterに対しても位置保持の正味の利益が確認できること。直接getterで十分ならそれを選び、型の種類や引数を増やさない。単発getterにもStore固有の改善余地があることを設計へ反映する。

**4. [P2] 一時pointer/sliceを一律に排除する前提が強すぎる**

対象: 設計書§4の再帰越しpointer/slice禁止、§7。

証拠: 現行のappend可能なStoreに対して、古いsliceが再配置後の更新を観測できないという懸念は正しい。しかしnoscanを決めるのは永続オブジェクトのpointer配置である。整数配列への短命なpointerやsliceをstackで保持しても、その配列の要素領域がpointer走査対象へ変わるわけではない。ローカルGo 1.26.0のruntime/mgcmark.goはnoscan spanを参照先として発見しても内部走査を省く。stack上の参照の走査・生存期間延長・escape費用は別に評価が必要である。

再現: ローカルtoolchainのruntime/mgcmark.goのnoscan分岐と、Storeの配列要素型を確認する。旧named-child slice実験はCALLを残す特定実装の失敗であり、構造が安定した区間での借用全般を棄却する証拠にはならない。

仮説: 指摘2で構文配列の再配置を禁止できれば、整数slotより単純な短命の `*nodeHeader` または `[]NodeRef` 借用を選べる区間がある。Flags更新も同じbacking arrayに対して行われるため、最新値を読める。速くなること自体は未測定である。

提案修正: 当面の再配置可能な経路は提案どおり整数slot/spanを使う。そのうえで、構文配列が安定した区間では普通のGo pointer/sliceによるborrowを候補から除外しない。全ASTのpointer mirror、unsafe、uintptr、O(nodes)の追加cacheは不要。再配置が本当に必須なら、その経路と理由を記載して整数方式を選ぶ。

受け入れ条件: 整数方式を選ぶ理由を「noscanだから」ではなく、実際に必要なmutation契約とcodegenの収支で説明する。借用を比較する場合も、構築費・frame/spill・escape・最新値の可視性を測る。現行のsliceをそのまま再帰越しに保持してよい、という変更要求ではない。

**5. [P2] 「viewを作るが元getterを使う」対照の保持費が定義されていない**

対象: 設計書§8、順序2。

証拠: viewのbaseを消費しないcontrolでは、その生成・受け渡しがcompilerに消される可能性がある。Tryの成功判定が残っても、candidateで再帰越しに保持するbaseのlive rangeやspillまでcontrolに残るとは限らない。逆に、人工的なnoinline sinkで保持を強制すれば、実候補にないCALLを追加してしまう。

再現: 実装段階でcontrol/candidateのassemblyを比較し、baseの生成、保持区間、stack保存と再load、引数が実在するか確認する。今回の文書には実装がないため、DCEが発生した事実を報告しているわけではない。

仮説: ソース上の三者だけでは、保持費と解決削減を独立に測ったことにならない。

提案修正: 対照の完了条件に機械語上のlive range確認を加える。対称な実装を作れなければ、最終candidate対baselineを総費用比較として扱い、対照差を厳密な構築費・償却効果と命名しない。不要な値が除去されないようにする工夫自体の費用も記録する。

受け入れ条件: 未使用viewの最適化除去を確認したうえで、因果分解として言える範囲を明示する。採否の基準は引き続き実binderのwall、命令、cycles、意味一致である。

**推奨する共通機構**

共通する原因として最も支持されるのは、callerが既に知るowner/shape/位置を、汎用refへ戻す境界で失い、consumerが再び解くことである。これは「全getterにcacheを追加する」設計では解消しない。保持対象・保持期間・受渡し費を同時に決める必要がある。

最小構成は、既存b.storeに結び付く構文読取契約と、schemaが生成する直接getter、必要な区間だけのSlot/Spanである。表現上は例えば次の三層にする。

| 層 | 担うこと | 残す／省く仕事 |
| --- | --- | --- |
| 汎用境界 | Handle/qualified ListRef、unknown shape、foreignの解決 | 必要な検証とowner保持を行う |
| 構文読取区間 | 同じStore、構造の有効期間、生成schemaの保証 | 意味情報の更新を許可。owner/shapeを各getterで再証明しない |
| 小さい読取primitive | 既知fieldの直接read、Slotの現在値read、Spanの要素read | 必要な論理readは維持し、表現解決だけを償却する |

これはwrapperを一つ足せば速くなるという提案ではない。現在のb.storeもStoreを保持している。省くべき具体的な操作は、既知schemaでのchildLen再確認、同じheader/baseの再取得、local listの資格付与と直後のowner再検証である。genericな意味処理は一本に保ち、型付きwrapperは必要な部分だけ生成する。全nodeにReader/contextを配ったり、callback/interfaceによる汎用walkerを追加したりしない。

§2の比較上の公平性は維持できる。名前ref/kind/textのmemo、1pass化、訪問省略は不要である。共通dispatchの変更を含めるなら設計書のPcommon対照で分離する。構文借用の契約強化はStore表現の扱いとして別variantで検証できる。

この機構でも足りない場合は、残存費用の帰属に従ってSymbol/Flow/Handleの表現を検討する。GC・割当の目標まで同時に追うなら、その別経路の検討は必要になり得る。過去のpacked childRecordなどを、古い測定値だけを根拠に再提案・採用しない。

**照合したrepo・artifact・性能基準**

selected repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests` @ `32598cba146fa4dd7b6162b838630c90d865ab28`。pointer対照: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript` @ `8ac035a394c79e693a3a7d74cb170448503ee894`。TSGolint revisionは両者ともnull。今回HEAD、repo_root、保存identityを再照合した。

候補clone/worktree: 上記2件のほか、`flownode`、`land-44-45`、`lock-design-inv`、`lock-profile`、`merged-symbols-guard`、`nolock97`、`opt-merge-symbol`、`profile`、`putcol-bce`、`store-nolock-exp`、`store-pr-1`〜`store-pr-5`、`store-pr-6-perf`、`store-pr-7`、`store-pr-7-attach-parent-fix`、`store-pr-7-nodeseq-t10`、`store-redesign`。今回git worktree一覧を確認した。他の介入があるため比較対象に加えない。全パス・revisionは[A/worktrees.txt](../../../../.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/worktrees.txt)。

A = repo直下 `.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/`。
K = repo直下 `.cursor/skills/verify-tsc/artifacts/20260910-kpc-reliability/campaign-v7/`。

| artifact set / stage | status | 今回の確認・扱い |
| --- | --- | --- |
| A/store-identity.json、pointer-identity.json、paired-8-20 | `current` | identity3項目一致、対象Benchmark行あり。保存rawからbenchstatを再実行 |
| A/kpc-build-v7、K/paired-instructions、paired-memory | `current` | build identity一致、checker/domの対象Benchmark行あり。保存結果を参照 |
| A/instruments-attachのStore checker/dom | `current` | identity3項目一致、保存summaryを読み帰属を確認 |
| A/list-span-experiment/candidate、parent-argument-experiment/wall-candidate | `current` | identity3項目一致。効果・制約は各ノートの保存benchstatに基づく |
| 旧eval・旧Instruments | `stale` | 現行候補の性能根拠から除外 |
| 初期KPC v5のBenchmark stage | `unsupported` | 既存信頼性ノートで対象Benchmark行なしと記録。今回は旧rawを再監査していない |
| 設計書の新candidate、本レビューの直接getter/構文借用候補 | `missing` | 未実装。性能比較・全phase・通常GCの検証はできない |

`current`は指定されたidentity3項目の一致であり、dirty source一致ではない。今回、保存Store baselineのbinder SHAと現在のbinder.goは不一致、pointer側は一致だった。保存baselineの時間を現在のdirty treeの測定値として扱っていない。

通常GC、A/paired-8-20の中央値。比較は今回のbenchstat再集計でも時間差p<.001、n=20。保存summaryにはwallの精度不足も記録されているため、細かな到達率の予測には使わない。

| 入力 | 版 | ns/op | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| checker.ts | pointer | 12,458,733.5 | 7,425,344 | 13,954 |
| checker.ts | Store | 19,064,295 | 12,799,524 | 14,165 |
| dom.generated.d.ts | pointer | 4,874,793.5 | 5,287,312 | 16,562 |
| dom.generated.d.ts | Store | 6,819,245 | 7,867,930 | 16,684 |

再集計の再現コマンド（repoルート。benchmarkの再実行ではない）:

```sh
benchstat .cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/paired-8-20/pointer.txt .cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/paired-8-20/store.txt
```

hot paths: 広い帰属はwalk/helpers。StoreのcheckerではbindKind 12.35%、checkContextualIdentifierRef 5.03%、bindChildrenRef 4.58%、generated walker 3.49%。domではbindKind 8.97%、generated walker 3.65%、ListSlotAt 3.64%、nameRefGenerated 3.59%。これはbind分類内のexclusive cycle-weight割合であり、別測定wallを掛けて絶対時間へ換算しない。header/child/list解決はその内部の狭い介入先で、最も広いhot path全体とは異なる。symbol syntheticは実workloadの主因の証拠に使わない。

allocation drivers: checkerのsymbolIdx/flowsは各capacity 1,210,352 B、symbolRefsは302,584 B。FlowNodeは32→48 B、Symbolは96→104 B、declaration Handleは8→16 B。FlowNodeの追加16 B×79,990個は1,279,840 Bだが、capacity差と混同しない。残差もあるためB/op増分をこれらへ全額帰属しない。本提案のslot/spanはこれらを縮めず、GC CPU低下も未実証。

diagnosis: 局所性の利益を追加命令・load/store・表現解決が相殺しているという仮説は支持される。しかしtyped slotだけで退行の大部分を除ける証拠はない。新candidateがmissingであること自体を欠陥とせず、より安価な対照と共通不変条件の検討が設計段階で欠けている点を問題とする。

next action: 既定のwalker codegen対照の順序は変更しない。その前後の読取監査で構文writerと対象callerの削減予算を確定し、構成を固定して「既知shapeの直接getter」と「位置保持view」を同じ一経路で比較できる仕様へ修正する。機構を横展開する判断と、統合候補のwall3%採用判定は区別する。統合後にS0/candidate/P0を同一セッションで比較し、独立再現・8入力・全回帰・通常GC/parse+bindを通す。既存の匿名class Symbol.Name差とbaseline回帰失敗は未解決のまま残す。
