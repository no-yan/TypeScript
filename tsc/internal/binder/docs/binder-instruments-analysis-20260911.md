# 最新BinderのInstruments CPU Profiler分析（2026-09-11）

## 結論

Storeの遅さを単独で説明する一つの関数は見つからなかった。広いhot pathは引き続きwalk/helpersで、その内部にStore表現の解決・検査・意味情報の間接参照が分散している。生成した構文アクセサへの移行後も、Locals/NextContainerの整数map、Flags/Kindのheader再取得、list要素取得、Flowのpointer→ID変換が残る。

新たに具体化できた優先候補は**Locals/NextContainerのmap経由アクセス**。単なる「ChildRefが遅い」という説明では不十分。CPU Profilerの帰属とソースからこの経路の余分な処理は確認できるが、各経路がwall差の何msを生んだかは介入実験なしには確定できない。

## 対象・既存証拠の確認

- 選択Store repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、HEAD `32598cba146fa4dd7b6162b838630c90d865ab28`。
- pointer対照: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、HEAD `8ac035a394c79e693a3a7d74cb170448503ee894`。
- 両版のtsgolint_git_revはnull。repo_root・両revisionが一致し、今回のartifactは`current`。採取前後にdirtyを含む全Goソースと既存binary SHAの一致を確認。
- 候補cloneはflownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr系、store-redesign等。今回未使用。全候補はartifactのworktrees.txt。
- 旧Instrumentsの結果は先に確認した。今回と異なるidentityの旧profileは`stale`。20260910-binder-investigation-02の旧profileは現行アクセサ以前のソースを対象としており、現行の費用割合として使わない。
- 最新の無instrumentation基準は[負荷低下後の再測定](bind-batch-recheck-20260911.md)。今回その同じbinaryを使用し、プロファイリングのために本体・ハーネス・最適化フラグを変更していない。

通常GC、前回の主比較中央値：

| 入力 | pointer ns/op | Store ns/op | pointer B/op | Store B/op | pointer allocs/op | Store allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| checker | 11,761,141.5 | 15,453,223 | 7,424,013 | 12,798,288 | 13,950 | 14,163 |
| dom | 4,377,387.5 | 5,903,114.5 | 5,285,994 | 7,866,128 | 16,558 | 16,682 |

benchstatで時間+31.39%/+34.85%、各p=.002。ただし前回A/A精度gateは未達。GC無効でも+33.72%/+29.31%が残った。これが速度差の証拠であり、今回のprofile窓内のns/opは比較に使わない。

## 採取と集計

Instrumentsの**CPU Profiler**テンプレートを使用（Time Profilerやpprofへの置換なし）。同じfixture manifest、Go 1.26.0、CGO_ENABLED=1、GOMAXPROCS=8、GOGC=100、GOMEMLIMIT=off。pointer-checker → Store-checker → Store-dom → pointer-domの順に各20秒、実行中の対象PIDへattachした。

対象は修正済みの最大10 AST batchを繰り返す。各batch終了後に登録解除する。同一ASTを再bindしない。`-test.benchtime=10x -test.count=10000`で採取中の終了を避け、trace保存後に今回起動した対象だけを停止した。これはCPU profile用の連続負荷で、10000回のbenchmark完走を主張するものではない。4 traceの保存・XML exportは成功し、対象は全て停止した。

RunningかつBinder/BindSourceFileを含むstackだけをbindに分類。割合は**bind内cycle-weightに対するexclusive leaf割合**。inclusiveは別表で明示し、親子を足さない。背景GC・parse・準備・cleanupは主表から分離。bind窓と無関係なbackground GCをbindへ割り当てない。

| 対象 | bindのRunningサンプル数 |
|---|---:|
| pointer checker | 17,263 |
| Store checker | 21,970 |
| pointer dom | 14,784 |
| Store dom | 19,424 |

XML参照の未解決サンプルは0。既存集計器ともStore-domのbind+bindGC weight総量を照合した。

各窓で完了したbind数は同数ではないので、weight総量やサンプル数の版間比を速度差と解釈しない。構成比×別測定wallを実測時間として扱わない。各条件1 traceのためprofile比率自体のbenchstat比較はできない。プロファイルのA/A／独立反復は`missing`。

## 1. Localsとcontainer情報のmapが明確な追加経路

map関数をleafに持つサンプルからcallerを抽出し、callerのreturn PCと通常ビルドobjdumpを照合した。

| Storeの経路 | checker | dom | 対応ソース |
|---|---:|---:|---|
| Localsのmap読取 | 1.69% | 2.17% | store.go:1062 |
| Localsのmap登録 | 0.34% | 0.64% | store.go:1055 |
| NextContainerのmap登録 | 0.94% | 1.33% | store.go:1077 |
| 上記の合計 | **2.97%** | **4.14%** | exclusive、重複なし |
| 全mapaccess1_fast32＋mapassign_fast32 leaf | 3.83% | 5.34% | 他の用途・未帰属も含む |

`getLocals(ref)`は毎回`s.locals[ref]`を引き、初回はmapを作り`SetLocals`で登録する。container chainも`map[NodeRef]NodeRef`経由。pointer版の対応するnodeフィールドアクセスに対し、ハッシュ表の検索・挿入が必要になる。pointer traceでは上記fast32 leafは観測されなかった。

`getLocals`全体のinclusiveはStore 4.15%/7.16%、pointerの`ast.GetLocals`は1.74%/2.92%。必要なSymbolTable生成等も含むため、この差を純粋なmap削減可能量とは扱わない。

構文の`foreignListSlots`もmapだが、今回のmap全体の主因はそれだけではない。未帰属のfast32サンプルはchecker 0.36%、dom 0.75%残る。NextContainerと同じcallerにはEndFlow/ReturnFlow等の更新もあり、caller名だけで全てNextContainerに割り当てていない。

**次の候補:** Store内のLocals index／NextContainerを、整数列と既存pointer poolなどの形で直接引けるようにする。noscanを守るためnode整数配列へSymbolTable pointerを埋め込まない。疎なnodeに対する列の割当増を必ず対照する。Binderの宣言処理順・container chainの意味・lazy生成条件は維持する。

## 2. headerの再取得がinline後も残る

`bindKind`の末尾は、最初のkind dispatchと各処理の後に`FlagsAt(id)`を読む。FlagsAtはinlineされるため関数一覧に現れにくいが、サンプルPCは以下の行に対応した。

| bindKind内のFlagsAt相当行 | checker | dom |
|---|---:|---:|
| store.go:544/547（nil/ref検査とheader参照） | **1.72%** | **1.49%** |

binder.go:175の大きいkind switchにも5.08%/3.07%が帰属する。ただしpointerにもkind switchはあり、これをStore固有の除去可能な費用とはしない。

Store実装の`store*.go`行に対応したbind leafは、inline分を含めchecker **21.64%**、dom **16.07%**。runtime内のmap処理はこの値に含まない。これはStore行でCPUが使われている割合であり、必要な読取・更新も含むので全額が無駄ではない。

**次の候補:** kindを読んだ直後から消費する経路でheader位置の再利用を検討する。Flagsはmutableなので値を早期snapshotして後段の更新を見落とす変更は不可。構文配列安定性と意味情報更新を分ける契約の下で、位置を保持し最新Flagsを読む。

## 3. listはSpan導入済みだが、modifierと要素の経路が残る

| 関数／経路 | checker | dom | 集計 |
|---|---:|---:|---|
| bindListRef | 2.31% | 3.12% | exclusive |
| うちstore.go/store_bind_span.go行 | 1.03% | 1.25% | PC行、上行の内数 |
| modifierFlagsRef | 1.80% | 4.71% | inclusive |
| ListSlotAt | 0.87% | 1.79% | exclusive |

現在のlocal listループは既に`TryBindListSpan`を一度呼び、各要素を`BindListSpanElem`で読む。したがって「毎要素ListElemでownerをatomic再取得」が現行local hot pathの説明ではない。

一方、各要素の`children[start+i]`参照と`KindAt(id)`、modifier取得時の`modifiersRefGenerated → ListSlotAt`は残る。domでListSlotAt leafの1.39%はmodifiersRefGeneratedからの呼出し。生成Accessorで取得済みのModifiersを後続helperまで渡せるかが次の監査対象。modifierFlagsの全inclusiveを再解決だけの費用とは扱わない。

**次の候補:** modifier listの位置／ListRefを取得済みのcallerから渡し、同じ経路の再取得をなくす。Span要素取得では構文配列の安定区間に限るslice借用と現行整数Spanを同経路で比較する。bind順やfunctions-firstの2passは維持する。

## 4. Flow更新はpointerの直接代入より多い処理を行う

checkerで`Store.SetFlow` inclusive **3.31%**、そのcalleeを含む経路のうちSetFlow自身のexclusiveは1.90%、`flowID`は全caller合計exclusive1.70%。親子の割合は加算しない。

pointerはIdentifier等にFlowNode pointerを代入する。Storeは`mustMutate → flowID → putCol`で、Flow pointerが同じStoreのarenaに属するかの確認、chunk/indexの変換、列への書込みを行う。直前Flowのcacheは既にあるがmiss経路は残る。必須の所有確認を無条件削除する設計は採用しない。

**次の候補:** binderが作成したlocal FlowのIDを、所有契約と共に更新箇所まで渡せるかを限定監査する。外部／sentinel Flowのfallback、列のゼロ値、Flow同一性を維持して比較する。

## 5. 識別子処理はhotだが、単独の根本原因ではない

checkerのcheckContextualIdentifierはpointer inclusive13.75%、Store14.12%。Store自身のexclusiveは6.13%（pointer4.49%）で、Store版ではflags、parent、parent kind、text index、internOff、internBufを読む。

Store版のこの関数にinlineされたstore.go行がbind全体の1.61%に対応した。ただしscannerのkeyword判定や文字列hashも共通に必要で、inclusiveのほぼ全体を除去できるわけではない。名前解決やSymbol mapの文字列hashも両版に存在する。

## 割当とGC

B/op差は前回の通常GC基準でchecker +72.39%、dom +48.81%。既知のdriverはsymbolIdx/flows列、symbolRefs、FlowNode/Symbol/Handleの大型化。今回CPU Profilerで割当bytesの原因を改めて全額分解したわけではない。Allocations instrumentは使用していない。

今回のtraceには事前parseと明示GCがあるので、全traceのGC割合から通常アプリのnoscan効果を推定しない。前回のGC無効でもwall差が残ることと合わせ、GCだけが全差の原因という説明は支持されない。

## 改善量の予算と受け入れ条件

通常GCの中央値からpointer同等まで必要なStore側短縮はchecker約23.9%、dom約25.8%。Locals/NextContainerのmap leaf約3〜4%だけでも、Flowだけでも足りない。広いwalk/helper経路のStore表現費用を複数箇所で減らす必要があるという仮説が妥当。

次はLocals/NextContainerの表現対照を小さく行い、modifierの取得済み値の受渡しを続ける。これはStore表現の変更であり、pointerにも適用できる宣言algorithmの変更や2passの1pass化は含めない。

受け入れ条件:

1. 同一入力・bind順・Symbol/Flow/診断・foreign fallbackが一致する。
2. 現在の寿命ハーネスでbefore/afterを固定し、通常GCのns/op、B/op、allocs/opをraw保存してbenchstat比較する。A/A精度条件を緩めない。
3. 消すべきmap／再解決が機械語または同じ条件のprofileから消えることを確認する。
4. 列の追加によるB/op増を確認し、parse+bindとGCを悪化させていないか別途検証する。

今回の依頼では原因分析までを行い、これらの実装修正・追加のwall benchmarkは実行していない。

## Artifactと再現

artifact: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/.cursor/skills/verify-tsc/artifacts/20260911-binder-instruments/`

- `protocol.json`, `identities.json`, `verification-final.json`: 条件、元binaryのsource identity、終了時一致。
- `record.py`: 対象の起動、各20秒attach、対象停止、XML export。
- 各対象ディレクトリの`cpu.trace`, `cpu.xml`, `toc.xml`, `commands.json`, `record.txt`, `target-exit.json`: 生の記録。
- `analyze.py`, `analysis.json`: bind sample、exclusive/inclusive、caller、PCの帰属。
- `map-pc.py`, `pc-lines.json`, `map-sites.json`, `store.objdump.txt`, `pointer.objdump.txt`: ASLR補正とソース・機械語照合。

PCはMach-O __TEXTの0x100000000とtraceのload addressで補正し、ARM64の4 byte境界へ切り下げた。map callerはCALL前後のreturn PCを照合した。sampling skid、compilerの行割当、inline frame表示の限界があるため「その命令のstall時間」や「bounds checkだけの時間」は測定していない。

今回取得したCPU profileは`current`。未取得の反復profile、最新KPC、実装介入の因果効果は`missing`。要求したbenchmark regexに対応する行がない`unsupported` stageは今回なし。次回のprofile差分を比較する場合も、各窓の処理量を揃えるか構成比の比較に限定する。

## 後続の実装差分確認による補足

[4経路の詳細設計](binder-four-bottlenecks-design-20260911.md)を作成した。pointer版は`NewModifierList`でModifierFlagsを集計し、`Node.ModifierFlags()`では保持値を読む。一方、Store版の`modifierFlagsRef`は問合せごとにlistを集計する。上記「list再解決」の説明に加え、**集計値を保持する表現が失われた差分**も改善対象である。既取得のCPU割合は変えず、これだけの改善量を実測したとは扱わない。新設計・新benchmarkは未実施。

Localsについても、pointer版は`LocalsContainerData()`のinterface呼出しを経てpayload内のフィールドへアクセスする。「直接フィールドアクセス」はその後の記憶位置を指し、interface/payloadアクセスの費用がないことを意味しない。
