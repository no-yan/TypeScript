# Store tree 高速化の実験計画

作成日: 2026-09-18。対象: AST の store 表現を用いた走査。状態: E1実施済み、E2以降は実験設計。初版では既存結果の benchstat 再解析と実装の読み取りまでを行い、後続でE1の新規ベンチマークと候補実装を追加した。**pprof は取得・解析とも使用しない。**

E1 の実施結果: [Store tree list 処理除去実験の結果](store-list-elision-experiment-results-20260918.md)。local list の owner/header 再解決を除去した一要因実験で、主評価セルは有意に短縮した。

## 1. 問題と最初の判断

`tsc/before.txt` では、store は pointer より多くの条件で遅い。一方、この結果だけでは、追加命令、依存 load、キャッシュ容量、分岐、計測環境のどれが主因かは決まらない。まず表現全体を変更せず、list の反復処理と named child の参照復元を別々に測る。その後、命令の削減とデータ配置の変更を直交させる。

最初に試す候補は **list owner と list header の解決を一回にまとめること**。次が **同じ親の header/childStart の再取得を減らすこと**。どちらもコードから得た仮説であり、実測した hot path 順位ではない。全面的な SoA 化、NodeRef 圧縮、走査順への再配置は、小さい変更で説明できない差が残る場合に進める。

成功は pointer に必ず勝つことではなく、正しさを保ち、store の費用を再現可能に減らすこと。最終的な採用判断は実入力の stage と end-to-end へ戻す。

## 2. 選択 checkout と artifact の適格性

| 項目 | 確認内容 |
|---|---|
| selected repo | `/Volumes/SanDisk1TB/worktree/ast-traversal-bench-design` |
| 対象 package | `tsc/internal/ast`、計測実装は `tsc/internal/astbench/workload` |
| HEAD | `e6820c555897786acd17a15be84a19855a396629` |
| typescript-go checkout | `tsc` は独立 Git root ではなく上記 repo 内。選択 revision は上記 HEAD |
| tsgolint revision | この checkout について対応付け未確認。推測して埋めない |
| dirty state | `store.go`、`store_factory.go`、`store_identity.go` に既存変更あり。これらだけでも合計 2 insertions / 311 deletions。HEAD のみで実装を同定できない |
| candidate clones | `cursor-ast-store-tests`、`binder-rewrite`、`profile`、`flownode`、`lock-design-inv`、`lock-profile`、`store-pr-7`、`store-pr-7-nodeseq-t10`、`store-pr-7-attach-parent-fix`、`store-redesign`、`store-schema-foreach-child`、`store-nolock-exp`、`nolock97`、`putcol-bce`。今回はユーザー指定 checkout を選び、他 clone の結果を混ぜない |

artifact status は `current` / `stale` / `missing` / `unsupported` を使う。`current` は repo_root・tsgolint_git_rev・typescript_go_git_rev の全一致を確認できたものだけ。さらに dirty patch/source hash の一致を測定比較の条件とする。来歴が不足する既存ファイルは保守的に `stale` とし、不一致確定と来歴不足を理由欄で区別する。stage artifact があっても bench.txt に要求 regex の Benchmark 行がなければ `unsupported` とする。

| artifact set | status | 根拠と使い方 |
|---|---|---|
| `tsc/before.txt` | stale（来歴不足） | M1/darwin/arm64、32 cells × 6 samples は存在。repo/revision/source hash がなく current と認定不可。仮説生成に使う |
| `/Volumes/SanDisk1TB/worktree/ast-traversal-bench-evidence-20260917` | stale（revision 不一致） | identity.json の typescript_go_git_rev は `e9beda12c346a78aca06e332898129b926868b62`、tsgolint_git_rev は null、dirty=true。選択 HEAD と異なる |
| 本書の `artifacts/store-tree-plan-20260918/` | stale（元結果を継承） | 既存 raw の表現別抽出と benchstat。新しい性能測定ではない |
| 選択ソースの A/A・AB/BA・実入力 stage・end-to-end・hardware counters | missing | 今回は未取得。既存 tree の測定値で代用しない |

古い artifact に存在しない stage を数値ゼロとして報告しない。次回は既存 artifact の inspect を先に行い、要求 stage ごとに status を付けてから不足分だけ実行する。

## 3. before.txt の証拠

全 192 行で `B/op=0`、`allocs/op=0`。各表現で論理 node 数と visit 数が一致する。full-tree は `7×subtrees+1` visits、expression は `6×subtrees+1` visits。expression が operator token を訪問しないため、両 visitor の ns/op をそのまま仕事量一定の比較に使わない。

次表は benchstat の中央値表示を ns/op に換算したもの。増分は benchstat 出力を転記した。全セルの不確実性、p 値、allocation 指標は [benchstat.txt](artifacts/store-tree-plan-20260918/benchstat.txt) に保存した。

| visitor | subtrees | pointer ns/op（丸め） | store ns/op（丸め） | store の時間増分 |
|---|---:|---:|---:|---:|
| expression | 32 | 1,333 | 2,004 | +50.36% |
| expression | 64 | 6,478 | 4,440 | 未解決（p=0.065） |
| expression | 128 | 7,454 | 7,690 | 未解決（p=0.699） |
| expression | 256 | 11,480 | 16,810 | +46.38% |
| expression | 512 | 26,410 | 32,300 | +22.31% |
| expression | 1024 | 45,640 | 57,940 | +26.95% |
| expression | 2048 | 85,990 | 111,880 | +30.11% |
| expression | 4096 | 170,700 | 219,700 | +28.70% |
| full-tree | 32 | 1,915 | 2,652 | +38.49% |
| full-tree | 64 | 3,771 | 5,274 | +39.84% |
| full-tree | 128 | 7,537 | 12,798 | +69.81% |
| full-tree | 256 | 14,860 | 21,240 | +42.96% |
| full-tree | 512 | 30,410 | 47,540 | +56.33% |
| full-tree | 1024 | 64,180 | 92,310 | +43.84% |
| full-tree | 2048 | 140,400 | 202,500 | +44.21% |
| full-tree | 4096 | 242,000 | 336,200 | +38.94% |

各行の pointer/store とも **0 B/op、0 allocs/op**。最大サイズは 28,673 logical nodes。最大サイズ expression は 24,577 visits/op、full-tree は 28,673 visits/op。

注意点:

- full-tree/512/store は 42,097〜124,693 ns/op、expression/64/store は 3,800〜8,877 ns/op。外れ値を後から都合よく削除しない。時間順・熱状態・同時負荷の情報がなく原因は未解決。
- 有意差が出ても順序・コア移動・環境差という交絡は消えない。16 比較の探索結果であり、個別 p 値を採用基準にしない。非有意差は同等性の証明ではない。
- 曲線に一貫した cache 境界は立証されていない。4096 でも store が逆転していないが、LLC を超えた証拠や性能上限の証拠にはならない。
- suffix `-8` はこの測定区間の並列度を表さない。現在の `Measure` は GOMAXPROCS=1、LockOSThread、GC off にしている。ただし raw がこの実装で取られたかは未確定。LockOSThread も CPU core 固定を保証しない。
- 全体の geomean は異なる visitor/サイズをまとめた参考値にすぎず、優先順位や採否には使わない。

**hot paths:** CPU 時間の関数別帰属は未取得。コード上の候補は `walker.storeWalk` → `Loc/Flags/storeText`、`childAt`、`forEachChildSchema`、`forEachChildList`、`NodeSeq.All` → `ListAt`。

**allocation drivers:** 測定区間内は raw 上ゼロ。構築区間の nodes/children/lists の拡張、文字列 intern、side table、pointer node 確保は別途調べる候補であり、本結果から量は分からない。0 B/op は保持メモリや GC scan の削減を意味しない。

**診断:** store の訪問当たり固定費が第一仮説。依存 load、list の再解決、dispatch とコード生成の影響が未分離。full-tree は広い経路を測るが、より狭く変更可能な list/child accessor から着手する。**次の行動:** ソース同定と A/A 後、list-only と named-child-only の要因分離。

## 4. 測定契約と pprof を使わない帰属

1. baseline と candidate の source snapshot、dirty patch、binary hash、Go version、GOOS/GOARCH、build flags、実行 command、repo identity を保存する。ユーザーの既存変更を baseline に含め、勝手に取り消さない。
2. 読み取り専用 walk を対象とし、訪問順・属性・nil/optional child・operator・early exit・foreign child/list の完全 trace を区間外で比較する。checksum だけでは順序違いや衝突を検出できない。timed run では visits と checksum を照合する。
3. 1 op は root からの一走査。ns/op、ns/visit、B/op、allocs/op、visits/op を必須とする。計測器・scratch は事前確保。構築、検証、memstats、counter 設定は計測外。区間内 GC/stack growth/allocation を検査する。
4. baseline/candidate は別プロセス、同じ入力・seed・batch・warmup。最初は32/256/4096 subtrees × expression/full-tree。32 は「L1 に入る」と未確認のまま呼ばない。候補が絞れたら元の8サイズへ広げる。
5. pilot は AB/BA 各2ペア＋A/A。確認は独立プロセスの AB/BA 各6ペアを開始点とし、事前時間予算で打ち切る。内部 loop を独立標本に数えない。A/A が候補差と同程度に揺れるなら時間差は未解決とする。
6. pair 順序は固定 seed で balanced randomization。充電・電源モード・温度/thermal pressure（取得可能なら）・OS・背景負荷・時刻を保存。汚染の除外基準は実行前に決め、失敗/再試行も残す。
7. raw old.txt/new.txt を benchstat で比較する。サイズを pool しない。主評価セルを先に決め、holdout を残す。効果量・区間と各ペアの方向を確認する。

帰属は **同じ仕事の介入実験** で行う。list 解決だけ、header 取得だけ、dispatch だけを変えた候補を用意し、単独で測る。各段階の ns/op を引き算して「純粋な関数コスト」とは呼ばない。inlining・配置・cache の相互作用があるためである。

補助証拠は、Go compiler の inlining/escape/BCE 診断、最適化済み binary の objdump、区間を囲む instructions/cycles カウンタ。BCE は `-d=ssa/check_bce/debug=1`、inlining は対象 package の `-m=2` を使用し、使用 toolchain の対応を実行時に確認する。静的命令数は動的 instructions の代用品ではない。

Apple M1 の counters は任意の補助 lane とする。利用可能な KPC 等について、対象 worker thread、イベント定義、測定前後 read の allocation、empty-bracket overhead、既知 loop の反復数への比例、context switch/migration/multiplexing を検証できた場合だけ使う。イベント番号を推測しない。帰属・校正が確認できなければ counter artifact を unsupported とし、wall time と介入で進める。cache refill/TLB がなくてもサイズ曲線から miss 数を捏造しない。CPU sampling、pprof、pprof を前提とする PGO は本計画では使わない。

## 5. 実験一覧と分岐条件

各実験の変更は独立 patch にする。診断専用のアクセス制限を production API 全体へ無条件適用しない。

| ID / 方法 | 問題・仮説 | 最小の介入と対照 | 支持する観測 / 次の判断 | tradeoff・正しさ |
|---|---|---|---|---|
| E0 計測系 | ソース差・環境差が既存の揺れを作る | 同じ binary の A/A と、現 store/pointer の balanced run | A/A の揺れが減れば E1。減らなければ測定時間/環境を整え、差を未解決とする | fixture/visitor hash、source hash を固定 |
| E1 list の不要処理除去 | `ListAt` が要素ごとに owner/list header を再解決 | ArrayLiteral の長い local list だけ。owner/start/len を loop 外で一回取得し、children を走査。現 API loop と比較 | list 長に応じ時間・instructions/edge が減るなら本 visitor に適用。変わらなければ asm で既存 hoist の有無確認 | foreign list の owner は親 store と違い得る。0 ref は externalList fallback、nil と early exit を維持 |
| E2 named child の load/命令 | `childAt` は parent header→children→child header の依存鎖。左右で親を再取得 | list なし Binary/Paren 木。同じ親の header/childStart を一回読み左右 child を復元する診断版 | small から改善なら固定費に整合。大規模のみ改善なら E5 の配置と交差実験 | 再帰/visitor による mutation をまたぐ pointer/slice 保持は危険。read-only 境界を定義 |
| E3 属性アクセス融合 | `Loc`、`Flags`、text 解決が再ロードを増やす | 同じ全属性を読むまま header を一回取得する実験用 helper。別に no-text / header-only を両表現へ対称適用 | 融合版の改善で本候補化。属性を削った版はどの仕事が効くかの診断だけ | read を省いた版を高速化の達成値にしない。副作用と nil semantics を維持 |
| E4 dispatch/コンパイラ | callback/iter.Seq の呼出し・巨大 switch・inline budget が支配 | storage を固定し schema callback と同順序の直接 visitor を比較。次に loop 内 helper の縮小/slow path 分離を一つずつ | calls/spills/BCE/inlining の変化と ns/visit を対応付け。効く型のみ生成器から改善 | 全 switch 複製は text size と保守費増。expression の token skip をfull-treeと混同しない |
| E5 メモリアクセス・配置 | construction 順と visit 順の不一致や load latency が支配 | 同一 shape/payload/ref幅で construction、DFS 配置、固定seed shuffle。node rows と children/list の配置を別々に変える | 同じ命令経路で layout 感度があれば locality 仮説を強める。small/large と E2 を交差 | root から edge を辿る。訪問列配列の直線走査へ置換しない。remap/再配置コストを別計測 |
| E6 header サイズ/レイアウト | 24B row の field集合・stride が取得量に不適合 | 全fieldを保持した field順変更、hot/cold分離、32B padding を別候補にする。hot fields をまとめる AoS と kind/flags/edge index の SoA を小さな試作で比較 | bytes と instructions の双方を測る。小ささで有利か、アドレス計算・load数で不利かを判別 | padding は密度悪化、SoA は複数 stream。実機 cache line は取得値を使う。コメントの64Bを前提にしない |
| E7 表現サイズ | edge/header 帯域が効くならさらに圧縮可能 | E5/E6 に信号がある場合だけ bounded shard の16-bit local ref＋escape、delta ref等を一候補ずつ | large/多ASTで有利かつsmallのdecode費が許容なら検討 | >65535、overflow、foreign refs、shard境界、再配置、decode branches。全面導入は実入力E2E必須 |
| E8 アルゴリズム | 再帰・汎用列挙の定数項が大きい | 同じ訪問順の明示stack、kind別子列挙の生成、named child の一括取得を独立比較 | 深さ/幅による差が説明できれば対象を絞る | 今のwalkは基本O(nodes+edges)。漸近改善を約束しない。stackは事前確保しretained bytesも計測 |
| E9 仕事の除去/再利用 | 実consumerが未使用情報を計算、または同じ木を何度も読む | 実入力で利用属性・再訪回数を非timed診断で調べ、不要read/重複walkだけ除去。必要ならimmutable treeの限定cache | consumer結果一致と実stage改善で判断。構築費を含めbreak-even回数を測る | 本baselineの契約変更は別workload。cache失効、mutation、保持量に強い証拠が必要 |

初期優先順は E0 → E1/E2 → E3/E4。E1/E2 は問題を分ける別ケースとして実装できるが、性能測定は同時実行しない。E5 は固定費が残る場合でも多角的検証として一度実施する。E6/E7 は layout 感度または実測 footprint を根拠に進める。parallel traversal、無条件cache、prefetch は初期候補にしない。前二者は意味・状態が複雑になり、prefetch は有効な先読み距離と利用可能な実装を確認してから小さく試す。

## 6. 最小 benchmark の具体形

E1 は root ArrayLiteral + N 個の Identifier。N=0/1/8/64/1024、同じ text 長・seed、kind/flags/loc/text を読み同じ checksum。本番の NodeSeq と ForEachChildList を別セルにし、list 処理以外の違いを持ち込まない。foreign-owner list、external child、nil element は検証ケースと実分布に応じた別性能ケースにする。

E2 は list を含まない Binary/Paren 木。極端に深い chain のみではなく、浅い平衡木と中程度の chain を使う。Binary の operator を訪問するfull-tree版と省くexpression版を分離する。ref復元→属性read→次edge取得が直列になる形を保持する。事前 node array の streaming benchmark を代用しない。

E3/E4 はE1/E2と既存 repeated subtree の両方で測る。診断用 walker の tracing 分岐・record/checksum も費用を持つため、元 walker を主結果として残し、同値性検証を別関数へ移した lean walker を**両表現へ同時適用した対照**として一度測る。計測器を軽くした効果を store 改善に数えない。

E5/E6 のサイズは元の32〜4096を維持し、必要ならメモリ予算内で上限を広げる。list長と総node数が同時に増える元ケースだけに依存せず、固定list長のsubtree増加ケースも追加する。配列capacity、node/edge/list/textのused bytes、capacity bytes、reachable heap見積り、visitorアクセス領域見積り、peak RSSを別々に記録する。capacity見積りだけでcache residencyを断定しない。

## 7. 実入力への接続と採用基準

合成入力で方向が見えた候補だけ、固定した実TS/TSX入力へ進める。大きい単一ファイル、多数の小ファイル、expression中心、宣言/list中心、transform由来foreign参照を持つ入力を用意し、リポジトリ名だけでなく入力hash・node/kind/list長/深さの分布を保存する。parserで生成したstoreに同じvisitorを適用し、構築を除いたstageと、構築を含む実consumerを別計測する。pointer adapterのない実入力を合成pointerで置き換えない。

最も時間の長い実stageが parse/bind/check 全体でも、変更対象が狭い list/child accessor であることを明記する。stageの反復時間と非timedの呼出し・edge回数で対象の出現を確認し、サンプルCPUの関数帰属で順位を作らない。symbol syntheticを追加しても方向確認に限定し、実workload bottleneckの証明にしない。

受入条件:

- 完全trace、nil/optional/foreign、early exit、構築・変換・寿命に関する対象テストが通る。適用範囲に応じ AST/parser/binder/compiler conformance を実行する。
- 単純な不要処理除去なら、実入力stageで再現する instructions/allocation の低下も有用な証拠。時間差が未解決ならそのまま記す。固定3%閾値は置かない。
- cache、ref圧縮、representation変更、永続状態、lifetimeの複雑化には、構築費・保持メモリ・GC・実用並列度・end-to-endを含むより強い利益を要求する。
- 実証した改善、未解決の差、実証した退行を分ける。B/op=0の維持だけでメモリ退行なしとしない。安定instructionsの削減を比例するlatency改善と呼ばない。
- 一変更ずつ採否を記録してから累積版を測る。各patchの証拠と累積end-to-end回帰確認を両方残す。

## 8. 実行手順と出力物

最初の作業単位は source identity 固定、E0 の A/A、E1/E2 の検証テストと診断benchmarkまで。pilotでどちらにも再現差がなければ、それ以上の手動微調整を続けずE3/E4/E5へ移る。一候補につきpilot一巡・独立確認一巡を初期予算とし、追加runは未解決事項と必要精度を明記して行う。

既存 Go benchmark の狭い再現例（作業ディレクトリは選択repoの `tsc`）。これは balanced campaign の代わりではない:

```sh
go test ./internal/ast -run '^$' \
  -bench '^BenchmarkTraversalSizeScaling$/^expression$/^subtrees-256$/^store$' \
  -benchmem -count=6 > old.txt
# 同じ source snapshot から一要因だけ変更した candidate で同条件を実行し new.txt を保存
benchstat old.txt new.txt > comparison.txt
```

本計画の既存 raw 再解析は次で再現できる（`tsc` 起点）。名前の最終表現部分だけを揃え、sample/metric は変更していない:

```sh
out=internal/ast/docs/artifacts/store-tree-plan-20260918
awk '!/^Benchmark/ {print; next} /\/pointer-8/ {sub(/\/pointer-8/, "/representation-8"); print}' before.txt > "$out/pointer.txt"
awk '!/^Benchmark/ {print; next} /\/store-8/ {sub(/\/store-8/, "/representation-8"); print}' before.txt > "$out/store.txt"
benchstat "$out/pointer.txt" "$out/store.txt" > "$out/benchstat.txt"
```

新規runでは `identity.json`、source/patch/binary hash、case定義、raw old/new、A/A、attempt順序、benchstat、正しさの結果、compiler診断/asm、利用可能なら校正済counterを一つのrun directoryに保存する。parent design の既存 astbench campaign を利用する場合は現CLIのhelpとvalidity判定を確認し、未実装optionを本書から創作しない。

後続のissue/noteには必ず「問題、証拠、再現、仮説、提案修正、受入条件」を記載し、selected repo・candidate clones・artifact status・ns/op・B/op・allocs/op・hot pathの確度・allocation driver・診断・次の行動を添える。

参照実装: `internal/astbench/workload/workload.go`、`internal/ast/traversal_bench_test.go`、`internal/ast/store.go`、`internal/ast/node_sequence.go`。関連設計: [サイズ依存性の測定設計](traversal-m1-size-scaling-design-20260918.md)。今回のスコープは計画作成なので、perf playbook の修正実装・post-fix測定・PR作成は対象外。既存baselineの再解析で出発点を作り、独立した実装読み取りの提案も本書で照合した。
