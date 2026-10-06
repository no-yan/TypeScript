# Store checker 性能分析 指示書

2026-10-05。7e（[検証報告](store-cli-verification-report-20261004.md)）で、native Store CLI は機能完了・性能基準未達になった。本書はその次の計測段で、gate までの距離を、トップダウン（費用の分解）とボトムアップ（操作の単価）の両方から修正できる単位に割り付け、両者を照合して残差を出す。

同日、段 0 と §6.4 を新設し、その後のレビューで段 1〜5 を直して、gate の段（旧 段 6）を削った。決めたことは §10 にある。

本書は 7e の検証指示書 §9（[store-cli-verification-instructions-20261004.md](store-cli-verification-instructions-20261004.md)）を置き換えない。7e の指示書は 7e の検証が従った仕様として残す。環境（§9.1）、統計（§9.5）、artifact（§9.7）は、ここで差分を書かない限り 7e のものをそのまま使う。

## 0 目的と範囲

- 目的は、gate までの距離を、修正できる単位（区間・費用・仕組み・関数・操作）に割り付けることである。
- 本書は gate を測らない。製品コードを変えないので、測る source は 7e と同じで、7e の判定がそのまま有効である。gate の取り方は修正段の指示書で決める（§9）。
- 対象は check の経路（native Store Program の bind 以降、checker の作成、global/semantic 診断、report）。parse/bind は段 1 の表に載せるが、下りない（§3.4）。
- 製品コードは変えない。計数と区間の境界は overlay と test/benchmark 側で、分類の tool は `tools` module で入れる。
- 測定の対象は HEAD `daa33e7d88`（Go source は `145da728eb` と同一）。始める前に source hash を記録し、終わりに照合する（7e 報告 §0 と同じ）。

## 1 構成

| 段 | 問い | 入力 | 主な指標 | 次へ渡すもの |
| --- | --- | --- | --- | --- |
| 0 | gate の線に届くには何を削る必要があるか | C01・L01（heap は Pointer、cycles は両 backend） | heap 比の条件付き下限、対象処理の必要 cycles 削減率 | 固定条件と、分析範囲を広げる必要性 |
| 1 | どの区間のどの費用か | C01（時間・GC）、L01（heap・scan・GC 回数） | 区間 × 費用 の差と Store の構成 | 差の表、構成の表 |
| 2 | どこに下りるか | 段 1 の表 | 割り付ける量と規則（§4） | 対象の一覧 |
| 3a | mutator のどの仕組み・関数か | C01（GC なし） | 仕組み別・関数別の inst・cycles | 操作と関数の候補 |
| 3b | heap の何が scan され、何を確保しているか | C01・L01 | カテゴリ別 scan/noscan、確保場所別 alloc | 削れる heap の候補 |
| 4 | 1 回あたりいくら違うか | 実入力の node 列、測定 binary の機械語 | 単価の inst（cycles は記録） | 単価表 |
| 5 | 単価 × 回数で段 3a の差を説明できるか | 段 3a・段 4、回数 | inst の残差 | 未帰属の大きさ |

反復は C01 で回す。L01 は 8GB 機で paging が入り、7e で時間の A/A が不合格だったので、段 0 の heap、段 1 の heap・scan・GC 回数、段 3b にだけ使う。これらは決定的なので backend ごとに 1〜3 run でよい（7e の heap の A/A は [1.0000, 1.0001]）。D01 と E01 は使わない。

## 2 段 0：到達限界と必要削減率の見積もり

### 2.1 heap：必ず残る live bytes

Pointer の checked 地点の総 HeapAlloc を `H` とする。7e §9.4 と同じ保持条件で、AST の表現を変更しても必ず残る live bytes を `F` として積み上げ、`heap 比 ≥ F / H` を条件付きの下限とする。段 3b の分類を先に行い、型・個数・確保サイズ・保持理由と、それらが変わらないとする根拠を記録する。

- checker の型・symbol・cache も、AST の変更で個数やサイズ、寿命が変わる可能性があれば `F` に入れない。未分類の量も入れない。残りの `H − F` はすべて削減可能と仮定する。
- heap profile の標本誤差と分類の不確実性を記録する。点推定だけで到達不能とせず、不確実性を考慮した `F / H` の保守的な下端でも線を超える場合に限り、「固定条件の下では届かない」とする。下端を評価できない場合は参考推計とする。
- 7e 実装報告の「AST の削減分は約 0.75GB（16%）」（削減済みの量で、残っている AST の量ではない）だけから、残り 84% を固定費とはしない。段 0 では、残る側の量と固定条件を確認する。

### 2.2 cycles：対象処理に必要な削減率

同じ入力・Check 区間・GC 条件で測った Pointer の cycles を `C_P`、Store を `C_S`、Store の改善対象処理に帰属した cycles を `T_S`、gate の比を `g` とし、次を出す。

`必要削減率 = max(0, C_S − g × C_P) / T_S`

- 改善対象には AST accessor 本体、inline 先、関連する確保、影響を否定できない呼び出し元と未帰属分を広く含める。対象の一覧と帰属規則を保存し、同じ cycles を重複計上しない。関数全体を含めた値は AST アクセスだけの費用とは呼ばない。
- 100% を超える場合は「対象外の費用が変わらないなら、この対象処理の削減だけでは届かない」と判断し、分析範囲を広げる。100% 以下でも実現可能性の証明にはしない。AST の縮小で対象外の cache miss 等も減り得るので、この値から AST 最適化全体の限界を断定しない。
- `T_S` が0または測定分解能未満なら率は出さず、必要削減 cycles と未確定の理由を書く。線を既に満たす場合は必要削減率0とし、退行の帰属分析は続ける。
- 段 0 では条件の一致を確認できた既存計測を暫定値として使い、足りない値は未取得とする。段 3a の計測後に更新する。C01 は線を置かない（§10）ので `g = 1` とし、差の全体を削る量とする。L01 は cycles の取得方法と線が未決定なので、必要削減率を出さない。C01 の値を L01 に転用しない。
- `_opcount` の回数 × 単価は段 4・5 で照合に使う。7d-2c accessor tax 報告の objdump 由来の単価は命令数なので、cycles の代替にはしない。

### 2.3 mark CPU・wall と判断

mark CPU は scan 量・オブジェクト構造・単位量あたりの走査費用を明記した楽観シナリオとして推計する。checked の強制 GC 1 回と通常実行中の GC 全体を分け、heap 比から mark CPU 比の下限を導かない。wall は paging を含む実測で評価し、heap や cycles の割合から下限を転用しない。

- [ ] 条件付きの heap 下限が線を超える場合も、線は下げず、段 1 以降は止めずに進める（§10 の決定 1）。固定条件と不足量を報告し、§6.4 で checker 側の原因を分析する。
- [ ] 段 0 は分析範囲を決めるための見積もりであり、gate の合否には使わない（本書は gate を測らない。§9）。

## 3 段 1：区間 × 費用

### 3.1 区間

| 区間 | 境界 |
| --- | --- |
| Build | config 確定後から graph 確定まで（7e §9.2 の NativeProgram Build と同じ） |
| Syntactic | collector の Config と Syntactic の stage |
| Bind | Bind の stage（全 source の bind） |
| Check | Program・Global・Semantic・Global の stage。checker の作成と、Semantic の中の directive と filter を含む（`BenchmarkNativeProgram` の check と同じ） |
| Report | SortAndDeduplicate と writer |

`BenchmarkNativeProgram`（`internal/storeclitest/bench_test.go`）は区間ごとに別の反復で測る。段 1 では 1 プロセスが Build から Report までを順に実行し、各境界で §3.2 の値を読む runner を test 側に足す。

### 3.2 読む値

| 費用 | 値 | 出所 |
| --- | --- | --- |
| wall | 単調時計 | `time.Now` |
| CPU | utime、stime | `getrusage(RUSAGE_SELF)` |
| 非 CPU | wall − (utime + stime)、minor/major fault、voluntary/involuntary context switch | `getrusage(RUSAGE_SELF)` |
| GC の量 | `/gc/cycles/total:gc-cycles`、`/gc/heap/allocs:bytes` | runtime/metrics |
| GC の CPU | `/cpu/classes/gc/mark/assist`、`/mark/dedicated`、`/mark/idle`、`/gc/pause`（各 `:cpu-seconds`） | runtime/metrics |
| メモリ | phys footprint と peak | `proc_pid_rusage` の `ri_phys_footprint` と `ri_lifetime_max_phys_footprint`（圧縮メモリを含む。RSS は使わない） |

- runtime/metrics の `/cpu/classes/*` は、GC の mark termination でしか更新されない（Go 1.27.1 の `runtime/metrics.go` の `cpuStatsAggregate.compute` と、`mgc.go` の `accumulate`）。境界で読んだ値は直前の GC が終わった時点のもので、GOGC=off では動かない。
  - mutator の CPU に `/cpu/classes/user` を使わない。この値は GOMAXPROCS × wall から GC と idle を引いたもので、page fault の待ちも含み、非 CPU と重なる。
  - GC の CPU は、その GC が終わった区間に入る。表にこの定義を書く。
- `/gc/heap/live` は直前の GC の mark 量、`/gc/scan/heap` は GC の外では以後の確保も含むので、境界で並べても同じ時点の量にならない。境界の live と scan は §6.2 の run で取る。
- 境界での読み取りは区間あたり 1 回とし、受け皿は事前に確保する。

### 3.3 run と読み方

| run | 入力 | GC | 標本 | 読むもの |
| --- | --- | --- | --- | --- |
| 時間（GC なし） | C01 | GOGC=off | 7e §9.5 の A/A を backend ごとに 10 組、A/B を交互順で 10 組 | 区間ごとの utime（mutator） |
| 時間（GC あり） | C01 | GOGC=100 | 同上 | run 全体の GC の CPU と回数、区間ごとの wall と非 CPU |
| 量 | L01・C01 | GOGC=100 | backend ごとに 1〜3 run | 区間ごとの GC 回数と alloc bytes |

- 条件は 7e §9.1（GOMAXPROCS=1、singleThreaded、checker 1 個、GOMEMLIMIT 未設定、warm cache）。Store の CLI は singleThreaded と `--checkers 1` が必須なので、並列度の軸は範囲外とする。
- C01 では区間ごとの GC を比べない。C01 の区間は短く（bind は約 35 ms）、区間に入る GC は 0 回か 1 回で、どちらになるかは前の区間が残した heap goal で決まる。区間の比較は GC なしの mutator で行い、GC は run 全体（回数と 1 回あたりの CPU）で比べる。
- L01 の GOGC=off は 8GB に収まらない見込みなので取らない。

### 3.4 parse/bind の扱い

7e の C01 benchstat の bind（sec/op +17.5%、B/op +33.1%）は、本書の作成時に次のとおり確かめた（`_store-checker-perf-20261005/pre-bind-gc/`）。

- 時間：GOGC=off では Pointer 33〜36 ms、Store 33〜37 ms で差がない。GC ありでは bind の窓に GC が入った標本が遅くなり、どちらの backend に入るかは run の並びで変わる。同じ HEAD で測り直すと、Pointer 67〜103 ms、Store 34〜37 ms と 7e とは逆になった。
- B/op：testing の `runN` が標本ごとに `runtime.GC()` を呼び、build の GC と合わせて sync.Pool が空になる。そのため Store の bind は毎回 Bound の scratch（flow、flow list、symbol など）を append で作り直し、約 10 MB 多く確保する。pool が残る GOGC=off の 2 標本目以降は、Store 27.5 MB、Pointer 28.3 MB。CLI は 1 プロセスで 1 回だけ bind するので、この確保は本番でも 1 回ずつ起きるが、保持はされない。

parse/bind は段 1 の表に載せ、段 3 には下りない。scratch の作り直しは、§12 の候補に確保の項目として載せる。

### 3.5 出力

区間 × 費用の表を 2 つ作る。

- 差の表：Store − Pointer の差と比。各セルに A/A の幅を付ける。
- 構成の表：Store の各セルの量と、全体に対する割合。目標型の割り付けに使う。

## 4 段 2：下りる対象の選択

### 4.1 割り付ける量

gate ごとに、何をどれだけ、どこへ割り付けるかを決める。削る量は §2.2 の分子（`C_S − g × C_P`）と同じ考え方で、heap と wall にも当てる。値は 7e 実装報告の中央値で、始める前に 7e の raw（`_store-cli-impl-20261004/e/analysis/`）から再計算し、表と一致することを確かめる。

| gate | 現在（Store/Pointer） | 線 | 種類 | 割り付ける量 | 割り付け先 |
| --- | --- | --- | --- | --- | --- |
| C01 Check KPC cycles | 1.0455 | 置かない | 退行 | 差の全体 | 差の表（段 1）→ 段 3a |
| L01 checked HeapAlloc | 0.8426（3,999 / 4,746 MB） | 0.80 | 目標 | 202 MB（3,999 − 0.80 × 4,746） | Store の heap の内訳（段 3b） |
| L01 CLI wall | 0.864（21.26 / 24.99 s） | 0.70 | 目標 | 3.77 s（21.26 − 0.70 × 24.99） | C01 の Store の構成比（段 1） |
| L01 強制 GC の mark CPU | 0.457 | 0.80 | — | 割り付けない | — |

- **退行**は Store が Pointer より悪い gate で、差を割り付ける。**目標**は Store がすでに Pointer より良く、さらに削る gate で、Store 自身の費用の内訳に割り付ける。目標では、両 backend で同じ量のセル（checker の CPU、型と symbol の heap）も候補に入る。
- mark CPU は点推定が線を大きく下回り、未確定の理由は A/A 不合格だけなので、測定の安定化の問題として §9 に送る。
- L01 の wall は、C01 の段 1 の Store の構成比（区間 × 費用）で 3.77 s を割る。C01 には paging が無いので、L01 の sys（7e の比 0.588）と fault の分はこの割り付けの外にある。報告にそう書く。

### 4.2 選ぶ規則

- 退行（C01 Check KPC）：次の両方を満たすセルを送る。
  1. 差の 95% 区間が A/A の幅の外にある。
  2. 差が、割り付ける量（差の全体）の 20% 以上である。
- 目標（L01 wall）：C01 の構成の表で、全体に対する割合が 20% 以上（3.77 s の 0.75 s 以上にあたる）のセルを送る。差が負や 0 のセルも送る。時間のセルは規則 1 の A/A も満たすこと。
- 目標（L01 heap）：段 3b に送る。カテゴリの選び方は §6.3。
- 送らなかったセルも、量と理由を表に残す。parse/bind のセルは §3.4 のとおり記録だけにする。

## 5 段 3a：mutator

### 5.1 分類

主の分類は、7d-2c xctrace 報告 §3.2 の仕組み別（`mechanism.py`）とする。由来別（同名・移植・片側だけ）は、inline の違いで帰属が両側をまたいで動くので（同報告 §3.1）、補助の列にする。

仕組みを HEAD に合わせて直す。

- 測定用 adapter の行を削る。7e の結線で消えたはずなので、消えていることを確かめる。
- links は pageMap から StoreID ごとの page 表に変わった（7d-2c の A）。
- 祖先 walk・utility の一覧を、7d-2c の B で inline を戻した関数を含めて見直す。
- runtime を、確保（mallocgc 系）、map、memmove、その他に分ける。HEAD の KPC は B/op +6.8%・allocs +6.4% なので、runtime の差は確保の差として読む。

### 5.2 分類の tool

分類は Go で書いた tool が行う。Python は xctrace の XML の結合（`join.py`）だけを受け持つ。

- 置き場所は `tools/cmd/` の下（`tools` module。製品コードではない）。名前は実装者が決める。
- 入力と出力：

| mode | 入力 | 出力 |
| --- | --- | --- |
| 3a | `join.py` が書く sample の列（stack、inst、cycles、区間の印）。pickle をやめて JSONL にする | sample ごとの仕組みと由来、仕組み別・関数別の集計 |
| 3b | `go tool pprof -traces` の出力（`inuse_space`、`alloc_space`） | 確保場所ごとのカテゴリ、カテゴリ別の集計 |

- pprof の proto を直接読む `github.com/google/pprof` は `tools` module の依存に無い。足す場合はユーザーに確認する。
- 規則：
  - 関数名から package・receiver・関数を取り出す。closure（`.funcN`）と generic の instance は、親の関数として扱う。
  - 透過する関数：runtime の map と確保、generic の helper（`slices.Grow`、`core.(*Arena).New` など）。leaf がこれらなら、stack を上って最初の透過しない関数で決める。map は、呼び出し元が links の表なら links、それ以外は「他の map」とする（7d-2c の `mechanism.py` と同じ）。3b のカテゴリも同じ規則で決める。
  - 由来の列：checker と storechecker（binder と storebinder、compiler と storecompiler も同じ）で同名の関数の本体を、signature を除いて go/printer で出し、package 名をそろえて比べる。同じなら「同名・本体同じ」、違えば「移植で変更」、片側にしか無ければ「片側だけ」とする。
  - source から導けない一覧（祖先 walk・utility の関数、accessor の receiver の型、links の型）は、tool の中に Go の表として持つ。
- 試験：
  - 規則ごとに、手で作った stack を入れる table test。
  - 初回は、同じ join の出力に旧 `mechanism.py` も当て、sample ごとの分類の違いが、§5.1 で変えた規則（adapter、page 表、walk の一覧、runtime の分割）だけで説明できることを確かめ、artifact に残す。
- 規則（tool の source の hash）は測定の前に固定し、測定の後で変えない。最後の受け皿（checker・binder・他の ast の self、runtime ほか）に入った関数の上位は、一覧にして報告する。

### 5.3 記録

- 入力は C01 の GC なし。KPC（`BenchmarkNativeCheckKPC`）で区間全体の inst・cycles・IPC を、xctrace の `cpu-counter` テンプレートで関数別の値を取る。
- xctrace の driver は native の経路で書き直す。7d-2c の `storeharness/xctrace_test.go`（`TestXctraceCheck`）は fixture の偽 Program で `checkPointer`/`checkStore` を回し、`join.py` はその名前で区間を決める。storeclitest に `BenchmarkNativeCheckKPC` と同じ区間を回す driver を足し、区間の印に inline されない関数を置き、`join.py` の判定をその名前に変える。
- 記録と結合は 7d-2c xctrace 報告 §2 の手順（`record.sh`、`join.py`、core の移動の補正）を使う。
- pprof は関数別の帰属に使わない（darwin で inline された leaf を約 10 倍過小に帰属する）。
- inst は反復の主指標、cycles は段 3a の終わりに確認する。inst が減って cycles が減らない例（storeexp、7d-2c の試作は減った inst の 0.18）があるので、cycles の確認を省かない。

### 5.4 MPKI（条件付き）

仕組みの cycles の差が、inst の差を Pointer の CPI で換算した値を上回る（IPC が下がっている）ときだけ、その仕組みについて MPKI を取る。7e の KPC では Store の IPC が +3%（cycles 1.0455 < inst 1.0774）なので、区間全体では条件を満たさない。

- counter は L1D load miss、L1D store miss、DTLB miss、branch mispredict を、1000 命令あたりで出す。kperf の harness は cycles と inst しか読まないので、Instruments の template を使う。template はユーザーが GUI で作る（bplist の書き換えでは作れなかった）。選んだ counter 名を記録する。
- M1 で取れない counter は未取得と書き、別の counter で代えない。
- 関数別の帰属が要るときは、2026-09-24 の L1D sampling（`l1d-miss-sampling` template、`MetricTableForThread` を区間の窓で合計、leaf のみ）を使う。当時の集計 script は scratchpad にあって消えているので、書き直して artifact に残す。

### 5.5 出力

仕組み別の inst・cycles の差と、関数別（上位で差の 80% まで）の差。各関数に、仕組みと、呼んでいる AST 操作（Kind、Parent、祖先 walk、links、text など）を付け、段 4 の候補にする。

## 6 段 3b：heap

### 6.1 カテゴリ別

checked 地点（7e §9.4 と同じ lifetime、GC 2 回の後）で、live heap を次のカテゴリに分ける。

| カテゴリ | 例 |
| --- | --- |
| parse/bind の産物 | Store の slab と bind の列、Pointer の node 型・list・text・flow・symbol・locals |
| checker | Type、Symbol、Signature、instantiation の cache |
| links | node links と symbol links の表 |
| map・その他 | path、string、loader、source text、診断 |

- Store は 32B header に flow と symbol の slot を持ち、Pointer の node は `FlowNodeBase` を埋め込むので、確保場所では AST と bind を分けられない。1 つのカテゴリにする。
- 量は、checked 地点で取った heap profile の `inuse_space` を、§5.2 の tool でカテゴリに振り分けて出す。
- scan と noscan は、確保場所ごとに確保している型から決める。scan bytes は、object 1 つなら ptrdata、n 要素の配列なら (n−1)·size + ptrdata で数える（`/gc/scan/heap` は noscan の object と、object の末尾の noscan 部分を除く）。上位で inuse の 80% を占める確保場所は、型を手で確認して表にする。
- 照合：カテゴリの scan bytes の合計を `/gc/scan/heap:bytes` と比べる。10% を超えてずれたら、規則か型の割り当てが誤っているので、段 3b の結論にしない。
- profile を取る前に GC を 2 回行い、sync.Pool の victim cache を抜く。
- links は、page の枚数と密度（使われている slot の割合）も C01・L01 で数える。7d-2c 検証の残件 5（Store の page は Pointer の 3 倍、密度 0.33、C01 で +5 MB）は「7e の end-to-end の retained を見てから判断」とされ、まだ測っていない。

### 6.2 区間別の alloc と、境界の live・scan

- 区間（§3.1）ごとに、`allocs` profile の `alloc_space` と `alloc_objects` の差分を取る。区間の境界で `runtime.GC()` を呼んでから profile を書く（allocs profile は最後の GC 時点で止まる）。
- 同じ境界で `/gc/heap/live:bytes` と `/gc/scan/heap:bytes` を読む。GC の直後なので、2 つは同じ時点の量になる。
- この run は境界の GC で時間が変わるので、段 1 の時間の run とは分ける。

### 6.3 読み方

GC の CPU は一次近似で `alloc bytes × (scan bytes / live bytes) ÷ (GOGC/100)` に比例する。案は次の 3 つに分けて書く。

- scan を減らす案（noscan 化、scannable な object を減らす）：GC の CPU に直接効く。
- alloc bytes を減らす案：GC の回数が減る。
- noscan の live だけを減らす案：保持 heap の gate には効くが、scan/live が上がるので GC の CPU は増える。

L01 heap については、Store のカテゴリごとの量を 202 MB と並べた表を作り、40 MB（202 MB の 20%）以上のカテゴリについて、削れる見込みと根拠を書く。

### 6.4 checker 側の原因

checker と links のカテゴリが 40 MB（§4.1 の 202 MB の 20%）以上あるときは、段 0 の結果によらず、その heap を分析する。必要な削減は Store heap の 5% なので、最も安く削れる分が checker 側にあることもある。§2.1 の固定条件の下で heap gate に届かないと判断したときは、ここが主な分析になる。改善は本書の範囲外で、ここでは原因を特定するところまでを行う。

1 段目は、上位の型すべてについて機械的に出す。

- checker と links のカテゴリについて、上位の型（Symbol、Type の各種、Signature、links の値型、map）ごとに、個数 × `unsafe.Sizeof`、失う bytes、pointer を含む word の数を表にする。heap profile と型の情報から出し、arena の len と cap だけを overlay で読む。
- 失う bytes は、確保のしかたで数え方を変える。
  - object ごとに確保する型は、size class への切り上げで失う bytes を数える。
  - `core.Arena` から確保する型（Symbol、Signature、IndexInfo、links の値。checker と storechecker の両方）は、size class への切り上げが起きない。chunk の末尾の使われていない容量（cap − len）を数える。
  - arena では、生きている要素が 1 つでもあると chunk 全体が残り、同じ chunk の死んだ要素も保持される。この量はこの表では測れないので、測れていないと報告に書く。
- 同じ表を Pointer と Store の両方で作る。両者で同じ量になる（7e 実装報告の診断）ことを確かめ、違えば Store の移植で増えた分として別に書く。

2 段目は、見込みのある型だけをアドホックに深掘りする。

- 1 段目で量が 40 MB 以上の型だけを候補にする。何を見るかは型ごとに決めてよい。例：使われていない field の割合（Symbol の field のうち、L01 で一度も非 0 にならないもの）を overlay で数える。
- 結論は「どの型の、どの field か、size class か、個数か」の形で書き、見積もった削減量を付ける。

## 7 段 4：単価

段 3a の出力で差に効いている操作と関数を、費用がどこに載っているかで 2 種類に分けて測る。

| 種類 | 例 | 単価 | 回数 |
| --- | --- | --- | --- |
| 呼ばれる操作 | Parent、IsNil、File、links の参照、祖先 walk | 実入力の node 列を再生するマイクロベンチ | overlay で数えた呼び出し回数 |
| 呼び出し元の費用 | 2 word の Node の受け渡し（入口と call をまたぐ退避、読み直し、引数の移動）、inline の境界の移動 | 段 3a の上位関数を objdump で読み、Pointer に対して増えた命令を数える | overlay で数えた関数の呼び出し回数（経路で命令が分かれる関数は経路ごと） |

- 呼び出し元の費用はマイクロベンチでは測れない。増える命令は呼び出し元の register の使い方で決まり、1 つの操作を回すループには call が無いので退避が起きない。実際の呼び出し元を再生するには checker の状態が要る。7d-2c accessor 税報告 §1.4 では、`isMatchingReference` の +11 命令/回のうち約 10 がこの費用だった。
- objdump は、段 3a で測った binary そのものを読む。register の割り当ては build ごとに変わる。
- 呼ばれる操作のマイクロベンチ：
  - 1 本は 1 つの操作だけを隔離する。Store と Pointer の両方で、同じ node の同じ順序に同じ操作を当てる。
  - 入力は実入力から取る。C01 の check 中にその操作が受けた node を overlay で順に記録し、各 backend で再生する。両 backend の列が同じ node を指すことは、(file, pos, kind) の列の hash で確かめる。合成木は使わない（合成木は cycles を半分に過小評価した前例がある）。
  - 指標は KPC の inst/op と cycles/op。単価の A/A を取り、幅を記録する。1 つの操作だけを回すと cache を独占するので、cycles/op は実際の文脈より小さく出る。段 5 の照合には inst を使う。
- 7d-2c で確定した 3 つ（祖先 walk、links、adapter）は、HEAD で残っている分を確かめる。

## 8 段 5：照合

照合は inst で行う。

1. 回数：段 4 の回数を、C01 の check で両 backend について数える。`_opcount` は Pointer 用なので、Store 側の計数は overlay を足す。呼ばれる操作の回数は両 backend で同じはずなので、違ったら段 4 の前提（同じ仕事）が崩れていると判断し、理由を調べる。
2. 予測：Σ（呼ばれる操作の単価の差 × 回数）+ Σ（呼び出し元で増えた命令 × 呼び出し回数）。
3. 実測：段 3a の inst の差（GC なし）。範囲は、予測に入れた操作と関数が属する仕組みの合計とし、runtime の確保の差は含めない。
4. 残差 = 実測 − 予測。

- 残差が実測の差の 20% 以下なら、段 4 の一覧で差を説明できたとする。
- 20% を超えたら段 3a に戻り、残差を説明する関数を探す。戻るのは 2 回までとし、それでも残る分は「未帰属」として大きさと候補を報告する。
- 予測が実測を超える場合も同じ扱いにする。単価の入力か、objdump で数えた経路が、実際の文脈を再現していない。
- cycles は、仕組みごとに段 3a の実測を並べて報告する。inst から cycles への換算は安定しないので、cycles の残差の規則は置かない。

## 9 gate の扱い（修正段へ引き継ぐもの）

本書では gate を測らない（§0）。次の 3 つは修正段の指示書で決める。

1. 時間の gate を cycles を主にするか。KPC の cycles には paging の待ちと sys が入らないので、Store が稼いでいる分（L01 の sys の比 0.588）が落ち、Store に厳しい向きの変更になる。7e §9.6 の gate（L01 wall ≤ 0.70）を変えることになるので、線も合わせて決める。
2. L01 の cycles の取り方。KPC の session は GC をオフにするので、L01 では 8GB に収まらない。案は (a) GC ありで counter だけを使う session（A/A を新しく取る）、(b) xctrace の `cpu-counter` で L01 の check 区間を記録、(c) L01 は wall のみ。
3. L01 の mark CPU の A/A を安定させる方法。

## 10 決めたこと

2026-10-05、ユーザー。判断待ちは無い。

| 項目 | 決定 |
| --- | --- |
| 1 段 0 で届かない gate | 線は下げず、届くようにすべき課題として残す。段 0 では固定条件の下での到達限界を記録する。heap の原因は §6.4 で分析し、checker 側の改善を同時に行うかは §6.4 の結果を見て別に決める |
| 2 Check KPC の線 | 置かない。§2.2 は `g = 1`、割り付ける量は差の全体 |
| 3 段 2・段 5 の閾値 | 20%。絶対量の下限は置かない |
| 4 割り付け | gate の種類で分ける。退行は差、目標は Store の内訳。mark CPU は割り付けない |
| 5 L01 の wall | C01 の Store の構成比で割る |
| 6 段 3a の分類 | 仕組み別を主、由来別を補助にする。分類は本書で作る Go の tool で行う（§5.2） |
| 7 段 4 | 呼ばれる操作と呼び出し元の費用の 2 種類に分ける（§7） |
| 8 旧 段 6（gate） | 削る。gate の取り方は §9 で修正段へ送る |
| 9 MPKI | IPC が下がった仕組みだけ取る |
| 10 L01 の段 1 | heap・scan・GC 回数だけにする |
| 11 §6.4 | checker と links が 40 MB 以上なら段 0 の結果によらず行う。上位の型すべての機械的な表と、40 MB 以上の型だけのアドホックな深掘りの 2 段にする。arena の型は chunk の未使用の容量で数える |

## 11 artifact と運用

- 結果は `tsc/internal/ast/docs/_store-checker-perf-20261005/`（gitignore 対象）に、段ごとのディレクトリ（`s0/`〜`s5/`）で置く。各ディレクトリに 7e §9.7 の metadata と、分類の tool の source hash・境界の定義を入れる。本書の作成時の bind の確認は `pre-bind-gc/` にある。
- 一時 binary、archive、`-m=1` の出力は scratchpad に置く（`-m=2` は 8GB 機で storechecker が完了しない）。scratchpad は再起動で消えるので、生成 script と hash は artifact 側に残す。
- KPC の binary は、execute が引き込む FSEvents のために `CGO_LDFLAGS="-framework CoreFoundation -framework CoreServices"` が要る。KPC は sudo が要るので、コマンドを渡してユーザーの別ターミナルで実行してもらい、結果のファイルを読む。
- 新しく build した binary の初回の xctrace は、許可ダイアログ待ちで止まることがある。止まったらユーザーに承認を頼む。
- 2 時間を超える run は nohup で回し、他アプリのメモリ圧迫と load を記録する。KPC と xctrace を同時に動かさない。
- 回帰の試験は、production code を変えないので不要。overlay・test・tool を足した package の試験だけを回す。

## 12 報告

報告は `store-checker-perf-analysis-report-20261005.md` に、段ごとに次を書く。

| 段 | 書くこと |
| --- | --- |
| 0 | heap の `H`・`F`・比の下限と固定条件・不確実性、cycles の `C_P`・`C_S`・`T_S`・必要削減率、mark CPU の推計条件、未取得項目 |
| 1 | 差の表と構成の表（A/A 付き）、parse/bind の記録 |
| 2 | §4.1 の表を 7e の raw から再計算した値、送ったセルと送らなかったセル、その理由 |
| 3a | 分類の tool の hash と旧 `mechanism.py` との違い、仕組み別の差、関数別の上位、取った場合は MPKI |
| 3b | カテゴリ別の量と scan/noscan、照合の結果、links の page、区間別の確保場所の上位、202 MB との対照、§6.4 を行った場合はその表 |
| 4 | 単価表（2 種類、A/A 付き） |
| 5 | 予測・実測・残差（inst）、仕組みごとの cycles、戻った回数、未帰属の大きさ |

最後に、修正の候補を gate ごとに、「効く費用（mutator / GC の回数 / scan の割合 / 保持 heap）」と「見積もった効果（割り付ける量に対する割合）」付きで並べる。修正そのものは本書の範囲外とする。
