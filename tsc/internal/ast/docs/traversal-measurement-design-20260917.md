# ASTツリートラバーサルの実験ハーネス・合成ベンチマーク設計

作成日: 2026-09-17。状態: 設計。ハーネス・新規ベンチは未実装、今回の性能測定は未実施。

## 1. 解決する問題と固定するゴール

**主問題は、store ASTが、GCの利益を除いた実際の木の参照追跡でもpointer ASTより効率的かを、短い反復で検証できる測定系がないことである。** 配列を順番に読む速度や型チェック全体の速度を、この問いへの答えに代用しない。

GCによる改善はユーザーによって立証済みの前提とする。再立証を本計画の成功条件にしない。過去の1.3〜1.5倍を再現することも成功条件にしない。速くないという結果でも、再現可能で原因と限界を説明できれば測定系は成功である。

達成すること:

1. 同じ論理AST・訪問順・属性読取を行うpointer/storeの比較を作る。
2. 実checkerのASTアクセスから選んだ、GC非介入の小さな合成ベンチでlayout変更を高速に反復する。
3. 日常の方向確認と、採否判断用の統制した実験を別の入口・判定にする。
4. 有望な変更を同一条件の実入力、GOGC=off/100/50/200で検証する。GC改善と走査改善の因果を混ぜない。
5. 生データから別の人が同じ集計を再生成でき、未解決・退行も保存されるようにする。

対象外: 型関係判定、型推論、Symbol/Type graph、checker link cacheの最適化、binder全体の最適化、汎用ベンチ基盤の完成、全OSのPMU対応。これらが重くても本計画のゴールを置き換えない。

「deterministic」は二つに分ける。論理AST、seed、訪問列、処理回数、期待結果は厳密に決定的にする。実機のwall time・cycles・cache missは決定的にはならない。意思決定用は、固定した仕事と校正済み計測に統制・反復・不確実性の報告を組み合わせる。命令数もruntime介入などで変動し、無条件の決定値とは扱わない。

## 2. 作業ワークツリーと比較対象

| 項目 | 選択 |
|---|---|
| 設計・後続ハーネス実装の作業先 | `/Volumes/SanDisk1TB/worktree/ast-traversal-bench-design` |
| ブランチ | `codex/ast-traversal-bench-design` |
| 作成元 | `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests` の確定commit |
| 初期commit | `e9beda12c346a78aca06e332898129b926868b62` |
| モジュール | `tsc/go.mod`、`github.com/microsoft/TypeScript/tsc`、宣言Go版1.26 |
| typescript_go_git_rev | 上記commit。`tsc`はこのcheckoutの一部で独立submoduleではない |
| tsgolint_git_rev | `null`。今回の対象にTSGolintは含めない |
| 文書 | `tsc/internal/ast/docs/traversal-measurement-design-20260917.md` |

既存のdirtyな作業は取り込まず、変更もしない。別タスクの未コミット変更を性能差として混ぜない。今後変更した場合はHEADに加えdirty patch、対象untrackedファイル、source hashを保存する。

候補clone/worktreeとして `cursor-ast-store-tests`、`binder-rewrite`、`profile`、`flownode`、`lock-design-inv`、`lock-profile`、`store-pr-*`、`store-redesign`、`store-schema-foreach-child`、main checkout `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript` を確認した。作成元はbinder変更をmerge済みの確定commitを持つため選択した。候補全部の性能artifactは今回監査していない。

pointerの実用比較候補はmainの `8ac035a394c79e693a3a7d74cb170448503ee894`。**比較baselineとしては未承認・未検証**である。異なるcommit間にAST表現以外の差があれば、結果を「layoutだけの効果」と呼べない。実装開始時に差分を監査し、同じ入力・toolchain・周辺処理を揃える。各版への同じ意味のbenchmark移植とadapter差分を保存する。揃えられなければ、実装全体の比較として範囲を限定する。

## 3. 既存証拠と不足

新規計測より先に既存資料を確認した。以下のstatusは選択ワークツリーに対する判定であり、資料の無価値を意味しない。

| artifact set / 対象 | status | 判断 |
|---|---|---|
| `binder-rewrite/tsc/internal/ast/docs/artifacts/vscode-store-pointer-b719-20260916/` | stale | repo_root/revisionが本選択と不一致。旧profileの探索材料に限る |
| 選択checkoutの新規traversal生ベンチ・A/A・統制pointer/store比較 | missing | 未作成 |
| 選択checkoutのcheck phase top 10とアクセス対応表 | missing | 旧profileはあるがcurrentな順位は未確定 |
| 旧binder KPC文書・実装 | stale（性能証拠として） | 機構の再利用候補。旧数値を今回の結果へ転記しない |

`current`はrepo_root、tsgolint_git_rev、typescript_go_git_revが全一致した場合のみ使用し、さらに入力・source・build・plan一致を検査する。未取得は`missing`。存在するstage artifactの`bench.txt`に要求regexに対応する`Benchmark...`行がないときは`unsupported`。identityの状態とstage対応は別欄とし、同じartifactでidentity=stale、stage=unsupportedも表現できる。計測失敗・汚染・未完了は別のvalidity/completeness欄にする。

今回の `ns/op`、`B/op`、`allocs/op` はすべてmissing。互換なold/new生出力がないためbenchstat比較はできない。旧profileのhot path候補は `Handle.Parent`、`Expression`、`Name`、`Text`、`Store.listOwner`。より広いchecker/map/link処理も重いが、今回のより狭く操作可能な対象はAST参照経路である。型・linkの仕事は対象外として明示する。

allocation driver候補はAST確保、list materialization、checkerの型/推論処理。旧CPU profileのmalloc寄与は確保byte数・回数の実測ではない。合成走査ではallocationをゼロにし、構築・実入力についてはalloc profileで別途確認する。現時点の診断は「GC効果は前提として成立、tree traversalの効果は未解決」。次の行動は比較同値性と最小visitorの実装であり、AST layoutの全面変更ではない。

既存ベンチの注意点:

- [store_e2e_bench_test.go](../store_e2e_bench_test.go)の`BenchmarkE2EWalkFactory`と`BenchmarkE2EWalkStore`は、選択commitではどちらも`ParseRoot()`からHandleをwalkする。名称をpointer/storeの証明に使わない。
- [store_bench_test.go](../store_bench_test.go)の`ptrNode`は簡略化した対照。実際のpointer ASTの表現・確保戦略の代用ではない。
- [store_adversarial_bench_test.go](../store_adversarial_bench_test.go)のrandom refs走査は、既成配列の間接読み。親から子を発見するtree traversalの主証拠にはしない。
- [既存KPC設計](../../binder/docs/kpc-measurement-workflow.md)のPython driver案は採用しない。本計画はGoとshellで実装する。

## 4. 比較の単位と主張の強さ

比較を三つに分け、reportで混ぜない。

| 比較 | 用途 | 主張できる範囲 |
|---|---|---|
| 簡略pointer model vs store model | layout仮説の早い試作 | 同じモデル内の効果のみ |
| 実pointer AST vs 実store AST | 表現・accessor・walkerを含む比較 | 同一アクセス契約の実装差。周辺差分があれば限定を記す |
| 同じstoreの変更前 vs 変更後 | 日々のlayout改善 | 対象変更への帰属が最も明確 |

モデルで勝っただけでは採用しない。意思決定では実ASTを使う。共通の抽象interface呼出しをhot loopに挟んで本来のaccessorコストを隠さない。共通仕様から具体型ごとのvisitorを作り、分岐・読取・再訪が一致することを検証する。layoutだけを変える実験ではwalkerのアルゴリズムは固定する。両方変える場合は別要因として記録する。

## 5. check phase top 10から最小visitorへ

top 10は実装するベンチ数ではなく、代表アクセスを選ぶための調査出力である。

1. hashを固定した小規模・大規模の実入力を各一つ選ぶ。候補は既存fixtureとVS Codeだが、現存ファイルを無条件に固定入力と見なさない。
2. 既存artifactのcurrent性・対応stageを調べ、不足分だけ後続作業でprofileを取得する。profileと通常の時間比較は別run。
3. check区間をphase markerまたは検証したstack分類で限定し、selfとinclusiveを別集計する。再帰を重複計上せず、inclusive上位を足し算しない。単位がsamples/cycle weight/timeのどれか記す。
4. 重い検査top 10に対し、呼出元、必要フィールド、子の順番、pruning、親への遡行、list長、再訪、読取/書込、型/link依存を対応づける。inlineされたgetterはsource/assemblyで補う。
5. 型計算は除き、ASTアクセスを残したvisitorへ縮約する。入力依存の型判断で訪問先が変わる場合は、抽出した選択規則を計測外で固定し、実checkerそのものの再現とは呼ばない。
6. 重複パターンを2〜4種類へまとめ、最初は主visitor一つと対照の全体walkだけ実装する。各入力でカバーするASTアクセスと除外分を記す。

必須出力`hotpaths.tsv`: workload、rank、function、self、inclusive、unit、source location、AST access family、synthetic case、omitted work、artifact identity。top 10が未取得なら空欄を推測で埋めずmissingと記す。top 10確定を待たず共通runner・同値性の実装は進められる。

## 6. 合成テストの契約

### 入力と訪問

- ASTを構築してから、rootから実際のchild/parent edgeを辿る。訪問nodeの配列を事前に作って、その配列を走査する方式を主ベンチにしない。
- 同一の論理node ID・kind・flags・必要payload・edge順から両表現を作る。IDは検証用で、production nodeのサイズを検証専用フィールドで変えない。
- parser由来の実ASTと、木形状を固定したsyntheticの両方を使う。入力seedとlayout seedを分ける。
- 論理node数、実際の訪問数、edge読取数、再訪数、属性読取数を区別する。`ns/node`の分母は訪問数と明示する。
- correctness用runで完全な訪問列・属性値・順序を比較し、timed runでは軽いchecksumとvisit数を保持する。checksum一致だけを完全な同値性証明にはしない。
- checksum自体が依存鎖のボトルネックにならないよう最小限にし、寄与を診断する。副作用sink、KeepAlive、必要箇所のassembly確認で処理消去を防ぐ。

### 初期case

| case | 契約 | 優先 |
|---|---|---|
| full-tree | child edge経由のDFS、kindと必要な軽い属性を読む | 対照として最初に実装 |
| expression | expression kindごとのoperand/callee/argument等を辿る。型推論しない | top 10由来の主候補 |
| selective | 固定規則でsubtreeをskip。読まない枝は両版とも読まない | 実入力の裏付け後 |
| ancestor/revisit | 対象nodeからparentを遡行、または一部subtreeを再訪 | 旧Parent hot pathの確認後 |

配置は実際の構築順を主条件とする。訪問順に配置する最良寄りの条件と、同じ木のedgeを保って配置だけshuffleする条件は感度確認にする。広いlist、深い木、kind混在を含める。深い木だけ別のwalker方式へ変更するような非対称性を避ける。

サイズは論理node数でsmall/medium/largeを固定し、各表現のbytes/nodeとworking setを併記する。同じbytesに合わせてnode数を変える比較を主結果にしない。初期pilotで小さいcache常駐・cache境界付近・cacheを超えるケースを選び、その後固定する。LLC超過を確認できなければcache非常駐を主張しない。

### 計測区間

合成走査では、setup（parse/build、visitor scratch・計測器の開始/終了read用buffer確保、検証）→必要な明示GCを完了→GC停止とlimit確認→同じworker上の固定warmup→metrics/counter開始→固定回数walk→counter/metrics終了→checksum出力→解放。

合成の走査区間内はallocationなし、GCなし、I/Oなし、loggingなし。scratch・再帰stackはwarmupしておく。stack成長やlazy initializationが残ればそのsampleは契約不成立とする。`GOGC=off`だけで済ませず、GC回数差ゼロ・allocation差ゼロを確認する。検証範囲はvisitor単体ではなく、開始側counter readの呼出し直前から終了側readが戻るまでの計測器を含む区間とする。metrics取得処理も事前確保・warmupし、その読取自体の確保を別途検証して観測値を汚さない。計測外のsetup確保・`runtime.GC()`は禁止しない。このゼロGC・allocation契約は実入力CLIのend-to-endには適用しない。

独立processには片方の表現だけを保持し、もう片方や巨大な生成用graphを残さない。同値性検査は別processで済ませる。生成用scratchの解放・GCは区間外。AST/Storeの寿命をKeepAliveで保証し、runごとの登録解除・保持メモリ復帰を確認する。

warm-repeatを日常の主条件にする。意思決定では複数の独立ASTを順番に辿るboundedなケースも加え、過度なcache warm状態への特化を確認する。cache eviction用bufferやOS cache purgeは通常条件に混ぜない。cold条件を完全に保証したとは言わない。

## 7. 測定手法の比較と選択

ユーザーの「kcp」は既存コードにあるKPC/kperfを指すものとして設計する。

| 手法 | 正確性・帰属 | 反復速度/準備 | 他processの影響 | 採用先 |
|---|---|---|---|---|
| Go benchmarkのwall、単一worker | 利用者の待ち時間。off-CPUも含む | 最速、通常権限 | CPU競合、DVFS、cache、帯域、swapの影響が大きい | 日常の主測定、採否の実時間確認 |
| thread CPU time | 対象threadの実行時間。移動先の取りこぼしに注意 | 軽量だがOS実装が必要 | scheduling待ちは減るがDVFS/共有資源の影響は残る | 任意の補助 |
| KPC thread-scoped EL0 instructions | 対象threadのuser命令仕事量。読取境界・runtime混入を校正 | 短い反復向き。初期準備/権限/host依存あり | 他process命令の直接混入を避けられるが、間接影響やruntime変動は残る | 意思決定の安定した補助指標 |
| KPC cycles/cache/branch events | 対象eventの絶対数。hostごとの意味を検証 | event groupごとに別runが必要な場合あり | cycles・missはcache共有、コア移動、DVFS等に敏感 | layout仮説の確認 |
| Instruments Time/CPU Profiler | サンプルからhot pathを発見。正確な呼出回数ではない | 起動・記録・exportが重い | sampling/計装負荷と共有資源の影響あり | top 10調査、疑問の診断 |
| Instruments CPU Counters | thread/区間/コアに帰属できる範囲でCPU bottleneckを調査 | 日々の全変更には重い | 別process、区間結合、P/Eコア分類に注意 | 代表候補の局所性診断 |
| Linux perf + CPU affinity/隔離 | Linux専用の別campaignとして制御を強められる | 専用host準備が必要 | 制限できるが完全には消えない | macOSで判断不能な場合の追加策 |
| simulatorの命令/cacheモデル | モデル内では決定的にできる | 実行が遅くApple実機との差がある | host競合は結果値に混ざりにくい | 初期範囲外、Apple実機速度の証明にはしない |

**single threadは測定ツールではなく、各手法に直交する実行条件。** 合成では1 worker、`GOMAXPROCS=1`を基本とする。`runtime.LockOSThread`はgoroutineをOS threadに結びつけるだけで、物理コアへのpinningや他processの排除をしない。Go runtimeの補助threadも存在する。QoS指定もP-core固定の保証ではない。[Go runtime](https://pkg.go.dev/runtime)

実入力はsingle-checker/P=1による機構確認と、固定した実用並列度によるend-to-endを分ける。単一threadの勝利から並列スケーリングを推定しない。

実入力の主測定は新規processによる同一CLI引数のend-to-endとし、parse/bind/check区間は補助metricとして分ける。`--noEmit`などの範囲、対象ファイル数、diagnostics本文・件数、終了コードを保存して同じ仕事を確認する。既知のdiagnosticsで非ゼロ終了する入力は、事前に固定した期待結果との一致で判定し、panic・timeout・処理の早期打切りを高速化として扱わない。走査だけの証明に型チェックは不要だが、採用時の実入力検証から型チェックを削除しない。

### KPCの安全な再利用境界

[investigation_darwin.go](../../testutil/kperf/investigation_darwin.go)の`OpenEvents`/`OnFreshThread`と明示errorの方針を再利用候補にする。現行`ReadEvents`は呼出しごとのローカル`counters`をcgoへ渡し、ユーザーのescape解析でヒープ確保が確認されている。終了側readの確保がcounter区間に入るため、そのまま再利用しない。**呼出元がsetupで確保したbufferへ読み込むAPI（予定: `ReadEventsInto(dst *[10]uint64) error`）への変更を再利用の前提とする。** 開始用・終了用のbufferを別々に用意し、正常読取経路で呼出しごとの確保がないことをescape解析と動的allocation検査で確認する。エラー生成は失敗経路としてsampleを無効化し、ゼロ値へ置換しない。

現実装のM1用8 configurable slot設定を別SoCへ無検証で流用しない。Go/shellの新規driverから利用し、既存cgo境界は上記のbuffer方式へ修正したうえで再利用する。新しいPython依存は追加せず、既存Pythonも実行しない。

1. capability probeでCPU/OS、権限、event DB、user/kernel filter、slot数・readbackを確認する。未知のhostではcapabilityをunsupportedとし、数値ゼロに置換しない。
2. PMU初期化後にfresh pthreadを作り、Go callback内でLockOSThreadし、同threadの二つのread間だけを測る。通常の既存thread固定だけに置き換えない。
3. fixed counterのEL0+EL1とconfigurableのEL0-onlyを別metric名にする。retired instructions、µops、cyclesを同一視しない。
4. 空区間、既知の仕事量の短/長区間、複数fresh thread、counter単調性、event readback、読取失敗の検出を自己検証する。空区間の両readだけの経路と、両read+visitor batchの正常経路それぞれで、計測器込みのゼロallocation・GCを検証する。カウンタreadをnodeごとに呼ばず、batchを囲む。
5. 読取overheadを保存し、batchを十分大きくする。初期目安は主metricへの寄与1%未満。未達なら事前pilotでbatchを増やし固定する。空区間の値を無条件に差し引いて改善を作らない。
6. A/Aで再現性を確認する。instructionsの安定性はcyclesやwallの安定性の代用ではない。
7. InstrumentsとKPCは同時に使わず、PMUの排他を取る。キャンペーンのlockは協調するrunnerしか排除できないため、外部profilerも検出/事前確認する。

KPCのroot実行が必要なhostでは、通常権限でbuild/集計し、権限が必要なcollectorのみを明示して起動する。全ハーネスを常時rootにしない。権限やeventが利用できない場合も日常検証は使えるが、未取得の機構証拠を取得済み扱いしない。[XNU KPC](https://github.com/apple-oss-distributions/xnu/blob/main/osfmk/kern/kpc.h)

InstrumentsのProcessing割合の低下だけでL1D改善を断定しない。必要なeventを取得できる場合にmiss/visit、cycles/visit、instructions/visitを合わせて比較する。CPU ProfilerとCountersの別captureの絶対値は混ぜない。export後のrun・PID/TID・binary UUID・event単位の一致を確認する。[Apple CPU性能解析](https://developer.apple.com/videos/play/wwdc2025/308/)

## 8. 日常用と意思決定用の実行プロトコル

| 項目 | daily | decision |
|---|---|---|
| 目的 | 変更の方向と明白な退行を短時間で発見 | 採否判断に必要な証拠を揃える |
| 入力 | 主visitor、small/large、固定seed | 代表2〜4 visitor、3サイズ、固定した複数seed、未調整のholdout |
| GC/並列性 | off、P=1、1 worker | 合成off/P=1、実入力off/100/50/200・並列度別 |
| 測定 | wall・alloc・checksum。KPCは任意 | KPC自己検証+A/A、非計装wall、代表実入力 |
| 初期標本数 | 4 A/Bペア（AB順2・BA順2）、各processの固定batch | 10 A/Bペア/主cellを出発点、別sessionで確認 |
| 実行時間目標 | build除外で30〜90秒 | 対象数に依存。pilotで所要時間と上限を保存 |
| 判定 | directional / noisy / invalid。採用決定しない | improved / unresolved / regressedをmetricごとに出す |

反復数と時間は設計上の初期値であって精度の保証ではない。時間を超える場合は主caseに絞り、黙ってcellを落とさない。top 10全部×全seed×全GCの総当たりは避け、daily→decision synthetic→代表実入力→GC感度確認の順に絞る。

各cell（入力・visitor・サイズ・配置・GC・並列度・backend固定）内でAB/BAを同数用意し、保存seedでshuffleする。cell順もblock単位でbalanceする。A/Bを同時実行しない。1 processの内部反復は一標本であり、反復回数分の独立標本にしない。A/Aは同じbinaryの別labelで同じ経路を通し、主campaign内にもcontrol blockを残す。

意思決定用のbatch回数はpilot後に固定し、両版を同じ回数実行する。Go testingのadaptive calibrationを使う場合はその反復とsetupの意味を記録する。意思決定のcounter計測は専用fixed-batch entryを使い、N=1校正runまで暗黙に主標本へ混ぜない。日常の`testing.B.Loop`利用はtoolchain固定のもとで可能。[Go B.Loop](https://go.dev/blog/testing-b-loop)

A/Aの許容幅は判断したい効果とmetricに応じ、pilot後・A/B前にplanへ固定する。例としてinstructions CI±1%、wall/cycles CI±1.5%を出発点にできるが、普遍的な閾値でも採用下限でもない。目的の差を解像できなければunresolved。p値が下がるまで標本を足さず、追加実験は新plan/sessionとして保存する。

## 9. 環境統制・汚染の扱い

事前に同一toolchain/flags/PGO、電源接続・電源モード、同じOS/CPU、並列度、limit、入力snapshotを固定する。計測とbuild・profile・別ベンチは直列にする。マシン全体のCPU使用率20%以下や空きRAM>RSSを合格保証にしない。

実行中は低頻度の外部monitorでCPU負荷、thermal state、取得可能な周波数/コア情報、memory pressure、swap入出力、圧縮、peak RSS、page faultを記録する。取得不能欄はnullと理由を保存。monitor自体をA/Aで評価し、全版で同じ設定にする。全環境変数や無関係なprocess引数を収集せず、必要な設定をallowlistで保存する。

| 条件 | 日常 | 意思決定 |
|---|---|---|
| 期待結果/checksum不一致、counter失敗（当該backend） | invalid | invalid、採否に使わない |
| 合成走査の計測器込み区間にGC介入・allocationあり | invalid | invalid、ゼロGC・allocation契約違反 |
| 実入力CLIのAST構築等のallocation、GOGC=100/50/200でのGC | 対象に含める場合はmetricとして保存 | 正常な測定対象。確保bytes/回数、GC回数・CPU等を比較し、発生自体では失格にしない |
| thermal throttle、競合profiler、swap入出力、深刻なmemory pressure | contaminated表示 | 事前規則に従いcellを不成立。生データを残し別blockで再実行 |
| CPU競合やコア配置不明 | noisyの参考結果 | A/A/分散/診断を確認し、制御できなければwall/cyclesの判断を保留 |
| 単に遅い標本 | 保存 | 速度だけを理由に除外しない |

除外規則はA/B開始前に固定し、除外を含む全結果と有効集合の両方を出す。必要なtelemetryが欠けた場合、無汚染を証明したと扱わない。再実行の回数上限を設け、同じ環境障害が続けば専用host/時間帯へ移す。

実入力のGOGC=offでもallocationは正常な測定対象である。GCが観測された場合は明示GC・memory limit・設定不一致を区別し、planのGC条件を満たすかを判定する。合成走査のゼロGC規則を一律に適用せず、GC非介入の実入力cellとして計画した場合の違反は明示する。

十分なメモリ条件を主比較にし、意図的なメモリ圧は別campaignにする。片方だけ圧縮/swapを回避する効果は実用上の利益として保存するが、tree traversal固有の改善とは分離する。GOGC=offの大規模実入力はpeak memoryのpilotと実行上限を設ける。GOMEMLIMIT=offが収まらないならそのcellは未成立とし、無断でlimitを変えて比較しない。

## 10. Go/shellハーネスの構成

**主実装はGo。shellはbuild・起動の薄い入口だけ。Pythonは作成・実行しない。** JSON/TSV/XML処理、seed付きplan生成、子process管理、統計集計はGoで行う。既存kperfのcgoは低水準backendとして再利用し、制御・解析はGoに置く。

予定配置（すべて未実装）:

```text
tsc/cmd/astbench/                 Go driver: inspect/prepare/run/collect/report
tsc/internal/astbench/            plan、artifact、collector、集計
tsc/internal/ast/traversal_*_test.go
                                 同値性、合成visitor、fixed-batch entry
tsc/internal/testutil/kperf/      既存backendを再利用
tools/scripts/tsc/astbench.sh     薄い起動入口
tsc/internal/ast/testdata/traversal/
                                 論理fixture/seed/アクセス契約/期待結果
```

公開CLIの予定契約:

```sh
astbench inspect --repo /Volumes/SanDisk1TB/worktree/ast-traversal-bench-design --artifacts RUN_ROOT --bench REGEX
astbench prepare --repo REPO --before REF --after REF --plan PLAN_JSON --out NEW_RUN_DIR
astbench run --run RUN_DIR --lane daily
astbench run --run RUN_DIR --lane decision --backend kpc
astbench collect --run RUN_DIR
astbench report --run RUN_DIR
```

上記は仕様例で、まだ実行できない。`prepare`は明示したrefsをcommitへ解決し、source snapshotと各binaryを一度だけ作る。未コミット版は`--after-working-tree`の明示指定を必要とする。copy/overlayに含めたファイルをhash化する。build cache利用は可能だが計測中のbuildはしない。

`run`は子processごとのtimeout、campaign排他、SIGINT時の停止と部分結果保存を持つ。中断後の再開は欠測slotだけを新attemptとして追加し、失敗した試行を上書きしない。`collect/report`はread-onlyで計測を起動せず、入力の改変もしない。

主に必要なハーネス検査は、identity不一致、regex不一致、欠けたpair、counterゼロ/逆行/読取失敗、checksum不一致、合成走査の計測器込み区間のGC・allocation混入、中断・再開・二重集計の検出。実入力の正常なGC・allocationを失格にしないことも検査する。実装をなぞるだけのtestより、誤った採用判断を防ぐfailure caseを優先する。

## 11. 保存形式と結果解釈

出力先はrunごとに新規ディレクトリ。通常のworktree外部artifact rootの例は `/Volumes/SanDisk1TB/Library/Caches/ast-traversal-bench/<run-id>/`。保存後に削除され得るcacheだけを長期証拠にせず、判断に用いた一式は明示した永続保管先へコピーしhashを照合する。

```text
identity.json       repo_root、両revision、dirty/source/fixture/binary/tool hash
plan.json           完全なmatrix、seed、実行順、回数、除外規則、metric定義
build/              argv、選択env、stdout/stderr、build info
capabilities.json   CPU/OS/event DB/filter/権限/取得不能理由
attempts/000001/    metadata.json、stdout.txt、stderr.txt、bench.txt、metrics.json
                    counters.json、environment.jsonl、exit.json
control/            A/A、counter自己検証、empty-read/短長校正
profiles/           .trace、export XML、pprof、UUID対応（診断runのみ）
analysis/           before.txt、after.txt、benchstat.txt、paired.json、report.md
index.json          schema version、全ファイルhash、complete、attempt対応
```

stdout/stderrは逐次保存し、試行終了時にmetadataをatomicに確定する。途中ログを含め保存するが、timed visitor内の詳細loggingは行わない。全argv、cwd、環境allowlist、開始終了時刻、label、pair/block/session、GC設定、P、visit/checksum、counter単位とscopeを保存する。manifestがあれば結果を再実行可能と断定せず、入力snapshotと必要toolの復元可能性も点検する。

解析は次の順:

1. schema・identity・条件・completeness・regex・visit/checksum・scopeを検査。
2. rawから正しい単位の`ns/op`、`B/op`、`allocs/op`、`instructions/op`等を出す。trace durationを偽のGo benchmark行に変換しない。CLI wallと区間wallを別metricにする。
3. 条件が一致するbefore/afterのraw benchmarkを保存し、固定versionの`benchstat before.txt after.txt`を実行する。[benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)
4. Goでpair/block単位の比率・固定seedのbootstrap CI・時系列を補助出力する。内部反復をbootstrapの独立標本にしない。session間の異質性を示し、異なるGC・backend・event groupを合算しない。
5. 主metricとholdoutを事前指定し、都合のよいcaseだけ選ばない。A/Aが安定しないmetricや比較不能な条件はunresolved/比較不能と明示する。

レポートは必ずselected repo、candidate clones、artifact set/status、ns/op、B/op、allocs/op、hot paths、allocation drivers、診断、次の行動を含める。未取得値はmissing、計測外の構築allocationを走査のゼロallocationと混同しない。命令数・allocation減少から比例したwall改善を推定しない。

## 12. 採否基準・実装順・新しい課題の扱い

採用下限に固定3%を置かない。正確性、代表実入力の有用なコスト削減、保守・メモリ・寿命・並列性のtradeoffで判断する。小さな不要仕事除去は再現する命令数/確保削減を根拠にできるが、wall未解決を開示する。layoutの新表現やcacheを追加する変更には、holdout、構築費用、retained memory、実用並列度、end-to-endのより強い証拠を要求する。

| 段階 | 成果物 | 完了条件 |
|---|---|---|
| M0 比較を固定 | identity・plan・既存artifact inventory | 同じ仕事か説明できる。pointerの周辺差分が見えている |
| M1 最小反復 | full-tree+主visitor、Go daily runner、サイズ探索とsmall/large preset | 同値性、計測器込みゼロalloc/GC、A/A、raw保存が機能。複数サイズの性能曲線を再生成し、選定した二点をdailyで4ペア比較できる |
| M2 判断の校正 | KPC backend適合・decision lane | readback/自己検証/失敗検出/A/Aが機能。取得不能時も誤判定しない |
| M3 代表性 | top 10対応表、追加visitor、holdout | 選択・除外したアクセスが説明できる。配列走査へ退化していない |
| M4 一つのlayout変更 | before/after artifact | syntheticの改善/退行/未解決を識別し、実入力で検証 |
| M5 実用判断 | GOGC off/100/50/200、実入力レポート | correctness・構築/保持メモリ・並列度・wall不確実性を含む採否 |

M1のサイズ生成・daily選定・指標・完了条件は[追加設計: AST走査のサイズ依存性](traversal-m1-size-scaling-design-20260918.md)を参照する。サイズによる性能変化の検出をM1、cache localityの機構確認をM2以降に分ける。

M1から短い反復を始め、M2〜M3の全機能完成までlayout仮説の方向確認を待たない。ただし採用は必要な証拠が揃ってから行う。日常の全反復にInstrumentsや全GC matrixを要求しない。

新しい問題は次の三種類で扱う。

| 分類 | 例 | 対応 |
|---|---|---|
| 測定の成立を壊す | pointer比較が実はstore、訪問数不一致、KPC逆行 | 最小修正で先に解消。未解消なら該当laneの判断を止める |
| 主仮説の説明に必要 | 参照復元命令増、依存load、深さ依存の退行 | 独立した最小caseを一つ追加し、元の主caseも残す |
| 目的外の改善 | 型cache、link索引、binder最適化 | 別issueへ記録し、本計画へ取り込まない |

各issue/noteには「問題、証拠とidentity、再現手順、仮説、提案修正、受入条件」を必須とし、さらに主ゴールとの関係と次の行動を一行で書く。現在の仮説を棄却する条件も書く。新しい課題のために主metric・入力・採用基準を変更する場合はplanをversion更新し、旧結果を保持する。

本計画の完了は、最小visitor→layout変更→実入力検証を再利用可能な手順で一周でき、第三者がrawから同じ集計と限定つきの判断を再現できること。測定系の拡張を続けることや、必ずstoreが勝つ結果を得ることではない。
