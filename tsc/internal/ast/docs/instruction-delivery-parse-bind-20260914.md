# parse + bind の Instruction Delivery が 25% になる理由 (Instruments Guided、2026-09-14)

対象トレース: `~/Documents/1be4c8a1e64bda712843fe6d940fed3a44e7be78.trace`
(Instruments 26.6、M1 MacBook Air 4P+4E、macOS 26.6.2、`built/local/tsc` を VS Code `src/tsconfig.json` に対して起動)。

| run | 名前 | テンプレート / モード | 引数 | 長さ |
| --- | --- | --- | --- | --- |
| 1 | Guided(--noCheck) | cpu-counter / CPU Bottlenecks (Guided, EL0) | `--noEmit --declaration false --extendedDiagnostics --noCheck` | 5.43 s |
| 2 | Guided(--check) | 同上 | `--noEmit --declaration false` | 23.2 s |
| 3 | Instruction Delivery Latency(noCheck) | cpu-counter / Instruction Delivery Bottlenecks (Guided, EL0) | `--noEmit --declaration false --noCheck` | 2.77 s |

## 結論

- parse は delivery 23.6%、bind は 24.8% (P-core だけなら 24.9% / 27.1%)。useful を除くと最大で、processing (13〜14%) と discarded (12〜16%) を上回る。GC は逆に delivery 11%、processing 34%。--check の run 2 でも parse 23.8% / bind 24.9% と再現し、**checker は 27.8%** (processing 27.7%) でさらに高い。delivery 25% は parse / bind 固有ではなく、tsc の木を歩くコード全般の性質で、例外は GC の走査ループだけ。
- M1 (t8103) の Guided 定義では **delivery は残差** (`1 − useful − processing − discarded`) で、直接数えているのは `delivery_latency = MAP_DISPATCH_BUBBLE / cycles` だけ。run 3 で分解すると parse は latency 11.0% + bandwidth 11.6%、bind は 12.4% + 13.1% で、**半々**。P-core では bandwidth 側 (bind 17.1%)、E-core では latency 側 (bind 14.4%) が大きい。
- bandwidth 側の原因は「taken branch の間の直列命令が短い」こと。hot 関数の静的分岐密度は parse / bind とも **3.5 命令に 1 分岐** (条件分岐 14%、call 7.5〜7.9%、無条件 jmp 5〜7%)。8-wide の map unit に対して 1 fetch ブロックが数命令しかなく、毎サイクル満たせない。
- latency 側の原因は命令フットプリント。parse の self サンプルは 295 関数 174 KB (90% を 121 関数 87 KB)、bind は 233 関数 159 KB (90% を 103 関数 104 KB)、スタック上の全関数では 305 KB / 226 KB。P-core L1I 192 KB、E-core 128 KB に対して収まらず、E-core で latency が高いことと整合する。GC は 90% を 11 関数 5 KB で回すので latency 3.3%。
- M1 では L1I miss / iTLB miss の内訳 (`delivery_latency_icache` / `_itlb`) は取れない (M3 以降の `MAP_DISPATCH_BUBBLE_IC/ITLB` が必要)。分岐予測ミス後の再フェッチも `delivery_latency` に混ざる。名前付きカウンタで確定するには kperf ハーネスに configurable event を載せる (後述)。
- 参考: 2026-09-08 の bind マイクロベンチ (checker.ts、P-core 単スレ) では Original が useful 0.43 / delivery 0.38、Port が 0.53 / 0.24。delivery 25% は Store 版固有ではなく、木を歩く front-end としては Original より低い。

## 集計方法

`xctrace export --xpath '/trace-toc/run[@number=N]/data/table[@schema="..."]'` で次を取り出した。

- `MetricTableForThread`: スレッドごとの区間 (start, duration) に `cycle` と各カテゴリの割合 (time-weighted-average)。区間の終端 = time-profile のサンプル時刻 (1 ms tick で分割される)。core 列は Performance / Efficiency。
- `time-profile`: 1 ms サンプルのバックトレース。
- `RemarksByThread`: 閾値超過の remark (delivery 0.2、discarded 0.1、processing 0.4、delivery_latency 0.25、delivery_bandwidth 0.15)。

区間を `(tid, 区間終端)` で time-profile に結合し (終端から 1.1 ms 以内に同スレッドのサンプルがあるもの、cycle の 77%)、バックトレースで phase を決めた: `runtime.gcBgMarkWorker|gcDrain|gcAssistAlloc|bgsweep` → gc、`internal/checker.` → check、`internal/binder.` → bind、`internal/parser.|scanner.` → parse、`internal/compiler|module|tsoptions|vfs|execute` → program、`runtime.asmcgocall` 末尾 → io/syscall、`runtime.morestack` 末尾 → morestack。割合は cycle で重み付けした平均。スクリプトは scratchpad (`mt.py` / `tp.py` / `join.py`)。

`kdebug-counters-with-time-sample` の生 PMC (12 本) も出したが、同一コアの隣接サンプル差分には他プロセスの区間が混ざる (metric の cycle 合計 17.1G に対し差分合計 36.3G) ので使わなかった。

## run 1: CPU Bottlenecks (--noCheck) の phase 別内訳

cycle 重み付き。全体 17.1 G cycles、うち unmatched 23%。

| phase | cycles | share | useful | delivery | processing | discarded |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 全体 | 17.13 G | 100% | 44.9 | 20.6 | 21.3 | 13.2 |
| parse | 3.19 G | 18.6% | 46.6 | **23.6** | 13.9 | 15.8 |
| ├ P-core | 2.18 G | | 44.7 | **24.9** | 12.9 | 17.4 |
| └ E-core | 1.01 G | | 50.7 | 20.9 | 16.0 | 12.4 |
| bind | 2.67 G | 15.6% | 49.5 | **24.8** | 13.3 | 12.4 |
| ├ P-core | 1.66 G | | 47.6 | **27.1** | 11.1 | 14.1 |
| └ E-core | 1.01 G | | 52.6 | 21.1 | 16.8 | 9.4 |
| gc | 3.61 G | 21.1% | 43.4 | 11.3 | 33.9 | 11.4 |
| program (module 解決・path・line map) | 2.07 G | 12.1% | 47.0 | 20.4 | 21.4 | 11.2 |
| io/syscall | 0.89 G | 5.2% | 39.9 | 24.2 | 21.3 | 14.6 |
| morestack | 0.48 G | 2.8% | 46.6 | 23.6 | 16.8 | 13.0 |
| check (noCheck でも残る少量) | 0.04 G | 0.3% | 23.3 | 9.0 | 58.5 | 9.2 |
| unmatched | 3.99 G | 23.3% | 42.0 | 22.9 | 21.0 | 14.1 |

100 ms 刻みでは 3.4〜4.2 s (parse、16〜37 スレッド) が delivery 21〜24%、4.3〜4.7 s (bind、9 スレッド) が 21〜27%、4.8〜5.1 s (GC 主体) が 11〜13% / processing 30〜34%。

関数別に見ると delivery% は parse 内で 22.8〜25.4%、bind 内で 23.7〜26.7% と**ほぼ一様**だった (Scan 23.5、scanIdentifier 23.5、mapaccess2_faststr 23.7、bindKind 24.9、bindEachStatementFunctionsFirstRef 25.6、bindContainer 25.3 …)。割合は 1 ms 区間の平均でサンプルの top frame は瞬間値なので関数帰属は原理的に無理だが、少なくとも特定関数のホットスポットではなく phase 全体のコード形状の問題である。

## Guided 指標の定義 (RecountDT.framework/Resources/Analysis/bottleneck.json、platform t8103)

```
slot              = CORE_ACTIVE_CYCLE * MAP_BW          (MAP_BW: P=8, E=4)
useful            = RETIRE_UOP / slot
processing        = MAP_STALL / CORE_ACTIVE_CYCLE
discarded         = max(0, MAP_REWIND*MAP_BW + MAP_INT_UOP + MAP_LDST_UOP + MAP_SIMD_UOP - RETIRE_UOP) / slot
delivery_latency  = MAP_DISPATCH_BUBBLE / CORE_ACTIVE_CYCLE
correction        = max(0, delivery_latency + discarded + processing + useful - 1)
delivery          = 1 - useful - max(0, processing + discarded - correction)
delivery_bandwidth= delivery - delivery_latency
```

- `MAP_DISPATCH_BUBBLE` = 「Map Unit に処理する uop が無く、かつ stall でもないサイクル」(kpep a14)。front-end から 1 uop も届かなかったサイクルで、L1I miss、iTLB miss、分岐予測ミス後の再フェッチ、`FETCH_RESTART` がここに入る。
- `delivery_bandwidth` は「uop は届いたが MAP_BW 未満」のサイクル分。Apple の説明は "There are too few sequential instructions between taken branches"。taken branch で fetch ブロックが切れるため、分岐塊が短いほど毎サイクルの供給幅が落ちる。
- M1/M2 (t8103/t8112 系) には `delivery_latency_icache` / `_itlb` / `delivery_bandwidth_taken_br` の式が無い。これらは t8120 以降 (M3/A17) の `MAP_DISPATCH_BUBBLE_IC` / `_ITLB` を使う。run 3 の表に `delivery_latency` と `delivery_bandwidth` しか無いのはそのため。

## run 3: Instruction Delivery Bottlenecks (--noCheck) の phase 別内訳

全体 15.8 G cycles、unmatched 25%。

| phase | cycles | latency (MAP_DISPATCH_BUBBLE) | bandwidth (残差) | 合計 |
| --- | ---: | ---: | ---: | ---: |
| 全体 | 15.80 G | 9.9 | 11.2 | 21.1 |
| parse | 3.38 G | 11.0 | 11.6 | 22.6 |
| ├ P-core | 2.11 G | 10.5 | 13.8 | 24.3 |
| └ E-core | 1.27 G | 11.9 | 7.9 | 19.8 |
| bind | 2.57 G | 12.4 | 13.1 | 25.5 |
| ├ P-core | 1.51 G | 11.0 | **17.1** | 28.1 |
| └ E-core | 1.07 G | **14.4** | 7.3 | 21.7 |
| gc | 2.21 G | 3.3 | 7.7 | 11.0 |
| program | 2.01 G | 8.2 | 11.6 | 19.8 |

run 1 と run 3 は別プロセスだが phase 別の delivery 合計 (23.6 vs 22.6、24.8 vs 25.5) は一致しており、分解は信用できる。

## なぜ parse / bind で高いか

### 1. bandwidth: taken branch の間隔が短い

run 1 の self サンプル上位関数を `go tool objdump` で数えた静的分岐密度 (Go 構文、CALL/RET/JMP/条件分岐の合計)。

| phase | 対象 | 静的命令数 | 命令/分岐 | 条件分岐 | JMP | CALL | RET |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| parse | Scan, scanIdentifier, rewind, parseDelimitedList, parseAssignmentExpressionOrHigherWorker, parseMemberExpressionRest, parseCallExpressionRest, appendSlots, linkList | 3308 | **3.5** | 13.5% | 6.7% | 7.5% | 0.8% |
| bind | bindKind, bindEachStatementFunctionsFirstRef, bindListRef, checkContextualIdentifierRef, bindContainer, bindRef, bindChildrenRef, bindChildRef, nameRefGenerated, ListSlotAt | 5028 | **3.5** | 14.4% | 5.0% | 7.9% | 1.4% |
| gc | scanobject 系 15 関数 | 2164 | 4.7 | 9.1% | 4.0% | 6.2% | 1.8% |

代表例: `Scan` は 1896 命令に分岐 600 (3.2 命令/分岐、CALL 129)、`bindKind` は 1032 命令に分岐 278 (3.7)、`bindChildrenRef` は 2.5、`nameRefGenerated` は 2.8 (RET 42 本)。命令ごとに taken branch になる確率が高く、fetch ブロックが数命令で切れるので 8-wide の map を満たせない。E-core (4-wide) では同じ塊でも幅を満たしやすく、bandwidth が 7〜8% に下がる (逆に latency が上がる) のもこれで説明がつく。

構造的には、ノードごとに `bindKind → bindChildrenRef → forEachBindChildGenerated → bindChildRef → bindKind` の段を降りる呼び出し連鎖と、`bindKind` / `bindContainer` / `Scan` の大きな switch (jump table 経由の間接分岐) が、直列命令列を細切れにしている。これは 2026-09-09 の生成 walker 実験で「関数間を移動しただけで命令数は減らない」と出た構造そのものである。

### 2. latency: 命令フットプリントが L1I を超える

run 1 の time-profile から phase ごとに出現した関数と `go tool nm -size` の合計。

| phase | self 関数数 (合計サイズ) | self 90% までの関数数 (サイズ) | スタック上の全関数 (サイズ) |
| --- | ---: | ---: | ---: |
| parse | 295 (174 KB) | 121 (87 KB) | 531 (305 KB) |
| bind | 233 (159 KB) | 103 (104 KB) | 343 (226 KB) |
| gc | 53 (30 KB) | 11 (5 KB) | 257 (184 KB) |
| program | 207 (132 KB) | 96 (67 KB) | 491 (313 KB) |

M1 の L1I は P-core 192 KB、E-core 128 KB (`hw.perflevel0/1.l1icachesize`)。parse / bind の作業集合は self だけで L1I と同程度、スタック上の全関数を含めると超える。しかも parser 242 KB、scanner 71 KB、binder 156 KB、ast 595 KB (`Store` アクセサ) がパッケージ単位で離れた場所にリンクされ、ノードごとにこれらを往復するので、ホット関数の局所性が悪い。E-core で latency が 12〜14% に上がるのは L1I が小さいためと読める。GC は走査ループ 11 関数 5 KB に集中するので latency 3.3% で済む。

さらに `runtime.morestack` (2.8%) と `mapaccess2_faststr` / `memHashAES` (parse 10%) のように、ランタイムへ跳ぶ経路が parse / bind の命令列に頻繁に挟まる。

### 3. discarded との相互作用

parse の discarded は 15.8% (P-core 17.4%) で bind (12.4%) より高い。scanner の文字種分岐と `lookAhead` / `rewind` の予測ミスが多いと考えられ、ミス後の再フェッチ bubble は M1 の定義では `delivery_latency` に含まれる。parse の latency 11% の一部は分岐予測ミスの副産物で、L1I miss だけではない。

### 4. processing が低いことの裏返し、ただし checker も同じ delivery を持つ

parse / bind は data 側の待ちが少ない (processing 12〜14%、2026-09-08 の bind マイクロベンチでも data 待ち 1 割未満)。命令窓が詰まらないぶん、front-end が uop を供給できない時間が「useful 以外で最大」に見える。

ただし run 2 (--check、120 G cycles) を同じ方法で分けると、checker は delivery 27.8% で parse / bind より高く、そこに processing 27.7% (map 検索と Store 間接参照のメモリ待ち) が加わって useful は 32.9% まで落ちる。つまり delivery 25% 前後は tsc の AST を歩くコード (parser / binder / checker) に共通の値で、parse / bind で目立つのは processing と discarded が小さいからにすぎない。GC (delivery 10.5%、processing 43.6%) だけが例外で、これは走査ループが小さく分岐塊が長いため。

| run 2 phase | cycles | share | useful | delivery | processing | discarded |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 全体 | 119.9 G | 100% | 33.8 | 25.0 | 29.4 | 11.8 |
| check | 75.7 G | 63.1% | 32.9 | **27.8** | 27.7 | 11.7 |
| ├ P-core | 50.0 G | | 32.0 | 29.0 | 25.6 | 13.5 |
| └ E-core | 25.6 G | | 34.6 | 25.4 | 31.8 | 8.2 |
| gc | 10.8 G | 9.0% | 34.3 | 10.5 | 43.6 | 11.6 |
| parse | 2.9 G | 2.4% | 47.3 | 23.8 | 12.6 | 16.3 |
| bind | 2.7 G | 2.2% | 51.6 | 24.9 | 11.6 | 11.9 |
| program | 1.6 G | 1.3% | 41.4 | 21.3 | 24.0 | 13.3 |

checker の self 上位は `mapaccess2_fast64` 10.2%、`mapaccess2` 4.5%、`Handle.Parent` 2.4%、`Handle.childAt` 1.8% で、どれも delivery 27〜30%。run 1 に混じっていた check 0.04 G (processing 58%) は起動直後の少量サンプルで、代表値ではない。

## 確定に必要な追加計測

M1 の Guided では L1I miss / iTLB miss / taken branch を分離できない。`tsc/internal/testutil/kperf` (`kperf-bind-measurement.md`) の configurable counter に次を載せて bind 1 回分を数えるのが最短:

- `MAP_DISPATCH_BUBBLE` (latency の分子)、`CORE_ACTIVE_CYCLE`
- `L1I_CACHE_MISS_DEMAND`、`L1I_TLB_MISS_DEMAND`、`FETCH_RESTART` (latency の内訳)
- `INST_BRANCH_TAKEN`、`INST_BRANCH` (mask 224: PMC5〜7)、`INST_ALL` (mask 128: PMC7) — 命令あたりの taken branch 率 (bandwidth の裏付け)

root が要るので対話セッションで `osascript` 経由の手順を使う。xctrace の手動テンプレートを `instruction-delivery.tracetemplate` の JSON (`configurationType` / `allEventsAndFormulas`) を書き換えて作る試みは、`{"manual":{}}` + `allEventsAndFormulas=[{isEvent,mnemonic,alias,...}]` で "instrument run data is missing"、`{"manual":{"events":[...]}}` で "No counting mode selected" になり、正しいキー構造が分からず断念した。

## 対策の方向 (未検証)

- 呼び出し段数と switch 段数を減らして taken branch の間隔を伸ばす (bandwidth)。生成 walker のように「関数を移す」だけでは効かない。ノードあたりの call/ret を減らす変更だけが対象。
- ホット関数を同じ領域に集める (latency)。Go には関数配置を指定する手段が無いが、PGO (`go build -pgo`) はホット呼び出しのインライン化で call/ret と関数間ジャンプを減らせる。効果は kperf の `MAP_DISPATCH_BUBBLE` と `INST_BRANCH_TAKEN` で判定する。
- 参考値として Original の bind は delivery 0.38 だったので、delivery の絶対値だけを下げても Original との 1.5 倍差 (命令数 1.79 倍が原因) は縮まらない。優先度は命令数削減の方が高い。
