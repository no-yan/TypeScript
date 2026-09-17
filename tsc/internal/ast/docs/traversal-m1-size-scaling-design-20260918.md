# M1追加設計: AST走査のサイズ依存性を短時間で検証する

作成日: 2026-09-18。状態: 追加設計。今回の変更は文書のみで、新規の性能測定は行っていない。

親設計: [ASTツリートラバーサルの実験ハーネス・合成ベンチマーク設計](traversal-measurement-design-20260917.md)。既存実装の説明: [Traversal harness implementation](traversal-harness-implementation-20260917.md)。本書はM1のサイズ変更・計測・完了条件を補足する。親設計の同値性、計測器込みのゼロallocation・GC、Go/shell限定、artifact管理、採否方針を引き継ぐ。

## 1. 主問題とゴール

**主問題は、固定した木の走査がデータ規模によってどれだけ遅くなるかを、同じ仕事で比較する仕組みがM1に必要なことである。** 大きな入力一つの勝敗だけでは、storeの小さい表現が有利になる範囲と、参照復元などの追加費用を区別できない。

M1のゴールは、論理ASTの規模を変えたときの`ns/visit`を再現可能な条件で比較し、日常の変更で規模依存の悪化を見つけること。storeが必ず勝つこと、大規模で差が必ず広がること、cache localityの因果を完全に立証することは完了条件にしない。GC改善は立証済みの前提とする。

M1で主張できるのは「この生成規則・配置・visitor・warmup条件では、サイズに対してこのように性能が変わった」。L1D/上位cache/TLBのどれが原因か、実parser全体や実checkerも速くなるかはM2以降で検証する。

## 2. 作業先と既存状態

| 項目 | 値 |
|---|---|
| 作業ワークツリー | `/Volumes/SanDisk1TB/worktree/ast-traversal-bench-design` |
| ブランチ | `codex/ast-traversal-bench-design` |
| 作成元commit | `e9beda12c346a78aca06e332898129b926868b62` |
| 実装言語 | Go。shellは薄い起動入口。Pythonは使わない |
| 後続実装の対象候補 | `tsc/internal/astbench/`、そのworkload、`tsc/internal/ast/traversal_bench_test.go` |

2026-09-18の読み取り確認では、既存`plan.go`にsmall=256、large=16,384、construction配置、full-tree/expression、GOGC=off、P=1、dailyのAB順2・BA順2とA/Aがある。これらのサイズは**暫定値**で、cache容量に対応することを確認した値ではない。既存実装は未コミットの変更を含むため、後続runではHEADだけでなくsource snapshot/hashを保存する。本書では実装済みの全機能や性能を検証済みとは扱わない。

候補checkoutは親設計に記載した`cursor-ast-store-tests`、`binder-rewrite`、`profile`、`flownode`、`lock-design-inv`、`lock-profile`、`store-pr-*`、`store-redesign`、`store-schema-foreach-child`、main。今回は再選定しない。

本追加設計向けのサイズ探索artifact setは未確認で、提供済みのcurrent証拠はない。`ns/op`、`B/op`、`allocs/op`とサイズ曲線は未取得（missing）。旧hot path候補はParent/Expression/Name/Text/listOwnerで、currentなtop 10は未確認。構築・list materialization等はallocation driver候補だが、走査区間外であり本書では定量評価していない。診断は「規模依存性は未解決」。次の行動は既存artifactのinspect、その後のサイズ生成・出力対応である。

後続作業では既存artifactを先に調べ、repo_root・tsgolint_git_rev・typescript_go_git_revが選択checkoutと全一致する場合のみcurrentとする。条件不一致はstale、未取得はmissing、stage artifactが存在してもbench.txtに要求regexのBenchmark行がなければunsupportedとする。今回は互換なold/new出力を使ったbenchstat比較は行っていない。

## 3. M1へ追加する四つの機能

1. 同じ生成規則でサイズを2倍刻みに変更するsize sweep。
2. 実構築順の配置と、rootからedgeを辿る訪問契約の検証。
3. サイズ別の`ns/visit`・訪問数・メモリ量の保存と性能曲線。
4. sweepから選んだsmall/largeだけを4ペアで回すdaily preset。

size sweepはdaily内の別モードとして扱い、意思決定用の精度を保証するlaneにはしない。既存のdailyペア規則を流用する。新しいCLI option名を本書だけで実装済みと見なさない。

## 4. サイズ生成と比較同値性

### 形状を保って規模を増やす

初期方式は、同じ構成規則の小さなsubtreeをrootの子として増やす方式とする。各subtreeは同じ深さ・分岐・kind構成比を持つ。rootの子list長は増えるため、list走査の規模効果を含むことを明示する。「サイズ以外の全特性が完全に同じ」とは呼ばない。

subtree内のpayloadは固定seedから生成し、同一値だけの木への過度な最適化を避ける。subtree間でnodeを共有せず、実体のある独立nodeを確保する。大きい木は小さい木のsubtree列をprefixとして含むようにし、サイズ変更に伴う内容変化を抑える。shape seedとlayout seedは別欄に保存する。

入力指定はsubtree数を2倍ずつ増やす。論理node数にはroot/token等を含め、要求数と実数を両方記録する。実数が厳密に2倍にならなくても丸めて隠さない。現在の`nodes`指定を使うなら生成可能なサイズへ解決した値をplanに保存する。

単一の巨大で深い木、多数の小さいファイルを個別処理、複数ASTの交互再訪は別のworkloadである。初期方式の結果をそれらへ一般化しない。これらの追加はM3の代表性検証へ送る。

### 表現とvisitor

- pointer/storeで同じ論理node数・edge・payload・訪問順・読取属性を使う。メモリ量を揃えるためにnode数を変えない。
- construction配置を主条件とし、実際のfactory/allocatorの構築順を使う。訪問順への並べ替えやshuffleはM1の必須条件にしない。
- rootからchild edgeを読んで次のnodeを発見する。事前に生成した訪問node配列を走査する方式は禁止する。
- full-treeを対照、expression等の主visitorを一つ置く。top 10で代表性が確認されるまでは主visitorをprovisionalと表示する。
- 各サイズ・表現の検証runで完全な訪問列と属性を比較する。timed runではchecksumと訪問数を照合する。検証専用IDのためにproduction nodeを拡大しない。
- 簡略pointer model、実pointer AST、実store AST、store変更前後を別比較として表示する。利用できないadapterの結果を既存の別表現で代用しない。modelの勝利を実ASTの証明にしない。

## 5. size sweepとdailyの選択

### 一度広く調べる

最初のpilotでは、数百node程度を出発点にsubtree数を2倍ずつ増やす。上限は事前のメモリ予算・sample timeout・最大case数で決める。初期案は最大10点で、必要なcache領域まで届かなければ未到達と記す。上限や点数を変更したsweepは新planとして保存する。

狙う領域は、両表現がL1Dに収まる小規模、L1Dを超える領域、上位cacheの容量境界付近、可能なら両表現とも最終段cacheを超える領域である。CPUのcache構成・line size・P/Eコアの違いをhost情報に記録し、取得できない値を推測で埋めない。

容量の見積りだけでcache residencyを断定しない。上位cache容量が不明、対象コアが不明、訪問する領域が一部のみなら、その限定を記す。観測された曲線の折れ曲がりも「L1D境界」と即断しない。

### 二点へ絞る

dailyはsmall/largeを固定する。smallは両表現の走査対象がL1Dに収まることを狙い、largeは両方がL1Dを十分超えつつ、メモリ圧を起こさず日常の時間予算に収まる点を選ぶ。largeがLLCを超えることはdailyの必須条件ではない。

選択規則は、メモリ見積り、CPU情報、実行時間、汚染の有無に基づく。storeの勝ち幅が最大の二点を選ばない。採用した点・理由・選択元sweep IDを保存する。layout変更のたびに点を選び直さず、同じnode数で比較する。CPU/生成規則/visitorを変えた場合の再選定はpreset versionを上げる。

各cellは4 A/Bペア（AB順2・BA順2）を保存seedでshuffleし、A/A controlを別途残す。1 processの内部反復は1標本。両表現は別processで保持する。バッチ回数はpilotで計測器overheadと時間を確認し、同一cellのA/Bで一致させる。異なるサイズ間のバッチ回数は変えてよいが、値を保存して一訪問当たりへ正規化する。

dailyの目標はbuild除外で30〜90秒。予算超過を避けるために失敗したsampleやA/Aを黙って省かない。主visitorの二点を最短presetにし、full-tree対照は明示した別presetに分けられる。サイズ曲線全体は生成規則変更時や候補の確認時に回す。

## 6. 計測境界とメモリ量

parse/build・fixture生成・visitor scratch・counter buffer・検証を区間外で済ませ、不要な生成用graphを解放する。両readを含む計測器の正常経路とvisitorでゼロallocation・GCを検証する。metrics取得も事前確保・warmupする。既存ReadEventsの呼出しごとの確保を許容せず、親設計の事前確保buffer方式を守る。

同じworker上で固定回数warmupしたwarm-repeatをM1の主条件とする。巨大データを温めたことは全データがcacheに残ることを意味しない。cache purgeやeviction bufferを追加して「cold」を保証したとしない。1 worker、GOMAXPROCS=1、GOGC=off、memory limit条件固定を引き継ぐ。

メモリは以下を区別する。

| 指標 | 定義・取得方針 |
|---|---|
| logical_nodes / visits | 全論理node数と実訪問数。再訪・skipを区別 |
| representation_used_bytes | 使用中のnode/edge/list/payloadの論理的な格納bytes。共有分は重複しない |
| representation_capacity_bytes | slice等の余剰capacityを含む予約領域。usedと分離 |
| reachable_heap_bytes_estimate | pointer先や文字列を含む保持量の見積り。算定式と漏れを記す |
| process peak RSS | process全体の常駐量。AST単独の使用量ではない |
| access_footprint_estimate | visitorが触るnode/field/edge領域の見積り。実cache residencyではない |

全メモリ指標を一つの「working set」にまとめない。Go objectのallocator丸めや管理費を論理bytesへ混ぜず、必要なら別欄とする。測れない値はnullと理由を保存する。詳細なアドレス列・cache line利用率の収集はM2以降で、timed visitorに計装を足さない。

## 7. 保存と解析

既存plan/cell/sampleのschemaをversion付きで拡張する。追加欄はgenerator version、subtree仕様・数、要求/実node数、最大深さ、kind構成、shape/layout seed、構築順、visitor契約hash、batch/warmup回数、メモリ指標と算定方法、preset version、選択元sweep ID。既存欄に対応する値は重複定義しない。

rawの`ns/op`・`B/op`・`allocs/op`を保持する。1 opはrootからの1 traversal、`ns/visit = ns/op ÷ visits/op`。既存の`ns/node`を使う場合も分母がvisitsであることを明示し、単位の異なる旧結果と混ぜない。訪問数不一致のA/Bを正規化で救済せず、同値性違反として扱う。

GoでTSVと静的SVGを生成する。横軸は実論理node数（対数軸）、縦軸はns/visit。A/Bを同じ図に重ね、標本と不確実性を表示する。メモリ量は別表/図とし、未確認のcache境界線を確定値として描かない。各サイズでrawのbefore/afterを保存してbenchstatを使い、異なるサイズを一つの母集団へpoolしない。

必要な出力はサイズごとの時間、比率と不確実性、メモリ量、A/A、validity、attempt順序。4ペアで精度が足りなければdirectional/noisyとし、改善確定にはしない。小さいサイズを基準にした劣化率は補助指標として出せるが、絶対ns/visitとA/B比を必ず併記する。低下率が小さいだけで絶対的に速いとは言わない。

解釈例:

- pointerが悪化するサイズでもstoreが維持: コンパクトな表現の利益と整合するが、cache原因の立証は未完了。
- 両方同程度に悪化: このcaseで規模への耐性差は示せない。
- storeが全サイズで遅いが悪化率は小さい: 絶対性能の退行と規模依存性を分けて報告する。
- warm-repeatだけ改善: 一度だけのwalkや複数AST切替への一般化は保留する。

## 8. M1完了条件と後続への境界

- [ ] 同じ規則の木を複数サイズで生成でき、各サイズの実node数・深さ・構成を検証できる。
- [ ] rootからのedge traversalであり、各表現の訪問列・属性が一致する。
- [ ] 計測器込みのゼロallocation・GC、非ゼロ訪問数、処理消去防止が検証される。
- [ ] size sweepのrawからbenchstatと性能曲線を再生成できる。
- [ ] 根拠を保存したsmall/large presetがあり、dailyの4ペア・A/Aを実行できる。
- [ ] 比較種別を明示し、利用できない実pointer比較や未取得metricを代用品で隠さない。
- [ ] 欠測pair、サイズ/visitor不一致、生成数の丸め、ゼロ訪問、単位混同を検出する。

M1は「サイズ依存の悪化を検出できる測定系」の完成である。特定の高速化率は要求しない。実pointer adapterが不足する場合はモデル比較部分のみ完成と記し、実AST比較まで完了したとは言わない。

M2へ送るもの: KPC event校正、instructions/cycles/cache refill/TLB、対象thread/コア帰属、cache line利用の診断。M3へ送るもの: top 10への対応、配置shuffle、再訪距離、多数の小AST・ファイル切替、holdout。M4〜M5へ送るもの: layout変更と実parser構築・実入力CLI・GOGC感度・実用並列度の検証。

サイズによる差が見つからなくても、storeが勝つまで生成規則を変更し続けない。新しい仮説は「問題・証拠・再現・仮説・提案修正・受入条件・主ゴールとの関係」を持つ別caseとして追加し、元の主caseを保持する。型/linkやGCの再最適化へ目的を移さない。
