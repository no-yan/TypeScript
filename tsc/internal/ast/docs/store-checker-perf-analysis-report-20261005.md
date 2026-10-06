# Store checker 性能分析 報告

2026-10-05〜06。対象は [指示書](store-checker-perf-analysis-instructions-20261005.md)。証拠は `_store-checker-perf-20261005/`（gitignore 対象）の `s0/`〜`s5/` と `meta/`。



## 0 測定対象と環境

| 項目 | 値 |
| --- | --- |
| HEAD | `daa33e7d88`（Go source は `145da728eb` と同一） |
| source hash | 開始 `meta/source-hash-start.txt`（tsc/・tools/ の追跡・未追跡 66,687 file）、終了 `meta/source-hash-end.txt`（66,698 file）。差は足した 11 file だけで、既存の file の変更は無い（`meta/source-hash-diff.txt`） |
| 回帰 | `go vet`（通常と storechecks）と `go test ./internal/storeclitest`（235 s）、`go test ./tools/cmd/perfclass` が通過（`meta/regression.txt`） |
| 製品コード | 変更なし。足したのは test（`internal/storeclitest/stages_test.go`・`rusage_*_test.go`・`xctrace_test.go`）、tool（`tools/cmd/perfclass`）、overlay（`s3b/overlay/`） |
| 分類の tool | `tools/cmd/perfclass`。規則を 2026-10-05 22:32:32 に固定（`meta/tool-frozen.sha256`）。3b の profile は固定の後に測り直した |
| 機械 | M1 8GB、Go 1.27.1、GOMAXPROCS=1 |

### 0.1 測定環境の障害（記録のため）

- 最初の段 1 の run（21:35〜21:52）は battery 駆動で、idle sleep（battery 時 1 分）が 21:35:36〜21:40:41、21:44:45〜21:47:35 に入った（`s1/sleep-events.txt`）。Pointer A/A の 9・10 組は sleep の直前・直後で、その後 load が 7 まで上がった（`duetexpertd` が CPU 76%）。
- 1 回目の再実行は sleep 中（22:23 の DarkWake）に始まり、lid を閉じた sleep で止まり、私の `go test` とも重なったので、全体を無効にした（`s1/invalid/`、`s1/rerun.log`）。
- ユーザーが AC 電源につないだ後（23:04）、`caffeinate -i` の下で段 1 の C01 を取り直した。7e §9.5 規則 8 の 1 回の再実行にあたる。
- 量（GC 回数、確保量、HeapAlloc、scan）は決定的なので、sleep の影響を受けない。L01 の wall は sleep と paging を含むので使わない。

## 2 段 0：到達限界

### 2.1 heap

`H` は Pointer の checked 地点の HeapAlloc で 4,745.3 MB（7e の 4,745.85 MB と一致）。`F` は段 3b の確保場所の分類から、両 backend で量が 2% 以内で一致し、AST の表現に依らない確保場所を積み上げた（`s3b/scan_floor.py`、`s3b/scan_floor.md`）。

| F | 数え方 | MB | F/H |
| --- | --- | ---: | ---: |
| 厳しい方 | 型に AST の参照が無いもの（symbol の値の links、symbol table の map、Relation、Union・Literal 型、type mapper、source text、symbol id の page）。両 backend の小さい方 | 1,112.6 | 0.234 |
| 緩い方 | 厳しい方 + 両 backend で 2% 以内に一致するが node の field や node の鍵を持つもの（Type の data、node 鍵の LinkStore ほか） | 1,752.6 | 0.369 |

- 固定条件：checker の Type・symbol table・links の値の個数は、両 backend（表現が大きく違う 2 つ）で 2% 以内に一致した。Symbol（Store 112 B / Pointer 96 B）、Signature（136 / 128 B）、node links の page、binder の産物は大きさが表現で変わるので `F` に入れていない。
- 不確実性：profile は MemProfileRate 4096 で、上位の確保場所は数千〜数百万の標本を持つ。profile の合計は HeapAlloc と 0.05% 以内で一致した（Pointer 4,744.1 / 4,745.3 MB、Store 3,999.2 / 3,998.5 MB）。分類の不確実性は「厳しい方」と「緩い方」の幅で表した。
- 判断：保守的な下端（緩い方の 0.369）でも線 0.80 を大きく下回る。**固定条件の下で heap gate に届かないとは言えない。** 7e 実装報告の「AST の削減分は約 0.75GB（16%）なので 0.84 で頭打ち」は、削減済みの量を固定費と読んだもので、残っている量の内訳（§6）からは誤り。

### 2.2 cycles

`C_P` = 2.609 G、`C_S` = 2.728 G（7e の C01 Check KPC、同じ source、`_store-cli-impl-20261004/e/c01-kpc/`）、`g` = 1。必要削減量は 0.119 G cycles。`T_S` は段 3a の Store の cycles を、対象の広さを変えて 3 通りに数えた（`s3a/bins.py`、重複なし）。

| `T_S` の範囲 | Store cycles（1 区間） | 必要削減率 |
| --- | ---: | ---: |
| 狭い：links + 祖先 walk + node accessor | 287.7 M | 41.4% |
| 中間：+ self のうち「移植で変更」「片側だけ」の関数と ast の関数 | 1,372.1 M | 8.7% |
| 広い：memmove・runtime のその他・stdlib を除く全部 | 2,653.3 M | 4.5% |

- 3 通りとも 100% 以下なので、分析範囲を広げる必要は無い。狭い範囲でも 41% で、AST の accessor と walk と links だけで退行を消すには、それらを 4 割削る必要がある。
- 100% 以下は実現可能性の証明ではない（§2.2 の注意）。

### 2.3 mark CPU

7e の強制 GC の mark CPU の比は 0.457 で、線 0.80 を大きく下回る（A/A 不合格のため未確定）。楽観シナリオの推計：強制 mark は scan bytes にほぼ比例するとすると、checked 地点の scan は Store 2,965.8 MB / Pointer 4,446.5 MB = 0.667 で、§6 の候補（Symbol と node links の page、どちらも scan）を入れると Store の scan はさらに約 0.2 GB 減る。mark CPU の比は scan の比より小さく出ている（0.457 < 0.667。Store の scan 対象は Symbol の arena のような大きな配列が多く、1 byte あたりの走査費用が小さいと見ている。仮説）。通常実行中の GC（C01、GOGC=100）は Store 10 回・42.3 ms、Pointer 6 回・121.0 ms で、1 回あたりの GC CPU の比は 0.209。

## 3 段 1：区間 × 費用

runner は `internal/storeclitest/stages_test.go`（`TestNativeStages`）。1 プロセスで Build から Report までを `performPointerCheck`・`performStoreCheck` と同じ順に回し、collector の各 stage の後で値を読む。footprint・instructions・cycles は `proc_pid_rusage(RUSAGE_INFO_V4)`（cgo なしの system call 336、`rusage_darwin_test.go`）。区間の定義は `s1/analyze.py` の先頭。

C01 の取り直し（`s1/c01-off-r2/`・`s1/c01-gc-r2/`、23:11〜23:14、AC 電源、load1 1.6〜3.4）を正とする。最初の組（`s1/c01-off/`・`s1/c01-gc/`）は §0.1 の sleep で A/A が不合格で、記録として残す。

### 3.1 差の表（C01、GOGC=off、mutator）

A/A は backend ごとに 10 組、A/B は交互順で 10 組。区間は組の比の中央値と bootstrap 95%（7e §9.5）。

| 区間 | utime 差 ms | utime 比 | inst 差 M | inst 比 | cycles 差 M | cycles 比 | A/A（utime・inst・cycles） |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| Build | −11.40 | 0.844 [0.839, 0.849] | −125.3 | 0.868 | −33.5 | 0.871 | 合格 |
| Bind | +1.48 | 1.045 [1.037, 1.049] | +64.9 | 1.171 | +6.4 | 1.058 | 合格 |
| Check | **+37.45** [+36.22, +40.26] | **1.046** [1.045, 1.050] | **+691.5** | **1.081** | **+121.9** | **1.047** | 合格 |
| Total | +27.35 | 1.030 | +635.1 | 1.064 | +95.4 | 1.032 | 合格 |

- Syntactic と Report は数十 µs で、分解能以下（A/A の判定外）。
- stime は Check で +1.24 ms（1.061）だが、A/A は不合格（ms 未満の量）。
- Check の inst と cycles（全 thread、GC なし）は 7e の KPC（+0.664 G inst、+0.119 G cycles）と合う。

### 3.2 GC あり（C01、GOGC=100、run 全体）

| 値 | Pointer | Store | 比 [95%] |
| --- | ---: | ---: | ---: |
| GC 回数 | 6 | 10 | 1.667 |
| GC の CPU（runtime の推定） | 121.0 ms | 42.3 ms | 0.349 [0.336, 0.357] |
| 1 回あたり | 20.2 ms | 4.2 ms | 0.209 |
| 確保 bytes | 338.3 MB | 381.9 MB | 1.129 |

区間ごとの GC は比べない（§3.3）。GC ありの区間の wall は位相で動く（Bind の Pointer A/A [0.687, 1.572]）。

### 3.3 構成の表（C01 の Store、GOGC=100）

| 区間 | wall ms | 全体に対する割合 | utime ms | stime ms |
| --- | ---: | ---: | ---: | ---: |
| Build | 76.1 | 7.0% | 68.7 | 7.4 |
| Bind | 37.9 | 3.5% | 35.3 | 2.6 |
| Check | 967.3 | 89.5% | 945.8 | 21.9 |
| 全体 | 1,080.7 | 100% | | |

run 全体の GC の CPU は 42.3 ms（3.9%）。非 CPU（wall − utime − stime）は 0 付近（負は複数 thread の CPU の重なり）。

### 3.4 量（GOGC=100）

| 区間 | L01 GC 回数 P / S | L01 確保 MB P / S | L01 GC CPU s（その区間で終わった GC） P / S |
| --- | --- | --- | --- |
| Build | 9 / 10 | 2,122 / 1,222 | 1.16 / 0.13 |
| Bind | 0 / 0 | 549 / 575 | 0 / 0 |
| Check | 1 / 2 | 3,268 / 3,554 | 2.96 / 1.39 |
| 全体 | 10 / 12 | 5,940 / 5,352 | 3.99 / 1.52 |

L01 の Check の確保は Store が +286 MB（+8.7%）多い。L01 の wall は sleep と paging を含むので使わない（§0.1）。

## 4 段 2：割り付ける量

7e の raw から再計算した（`s2/recompute.py`、`s2/gate-table.md`）。7e 実装報告の表と一致した。

| gate | 現在（Store/Pointer） | 線 | 種類 | 割り付ける量 |
| --- | --- | --- | --- | --- |
| C01 Check KPC cycles | 1.0455（2.728 / 2.609 G） | 置かない | 退行 | +0.119 G cycles |
| L01 checked HeapAlloc | 0.8426（3,998.9 / 4,745.9 MB） | 0.80 | 目標 | 202.2 MB |
| L01 CLI wall | 0.8638（21.26 / 24.99 s） | 0.70 | 目標 | 3.77 s |
| L01 強制 GC の mark CPU | 0.4569 | 0.80 | — | 割り付けない |

### 4.1 送ったセル

| gate | セル | 量 | 送るか | 理由 |
| --- | --- | ---: | --- | --- |
| C01 Check KPC（退行） | Check × mutator | utime +37.45 ms、cycles +121.9 M（差の全体） | 送る（段 3a） | A/A の外、差の 100% |
| | Check × stime | +1.24 ms | 送らない | A/A 不合格、差の 3% |
| | Bind × mutator | cycles +6.4 M | 記録のみ | KPC の区間の外、§3.4 |
| L01 wall（目標、3.77 s） | Check × mutator | Check は構成比 89.5% → 3.37 s。うち mutator は約 83.6%（utime 945.8 ms から Check で終わった GC 0.04 s を引く）→ 3.15 s | 送る（段 3a の Store の内訳） | 20% 以上 |
| | Build | 7.0% → 0.26 s | 送らない | 20% 未満 |
| | Bind | 3.5% → 0.13 s | 送らない | 20% 未満、§3.4 |
| | run 全体の GC | 3.9% → 0.15 s | 送らない | 20% 未満 |
| L01 heap（目標、202 MB） | — | — | 段 3b | §6 |

L01 の sys（7e の比 0.588）と page fault はこの割り付けの外にある（C01 には paging が無い）。

## 5 段 3a：mutator

### 5.1 記録

- driver は `internal/storeclitest/xctrace_test.go`（`TestXctraceNativeCheck`）。`BenchmarkNativeCheckKPC` と同じ区間を `//go:noinline` の `xctraceCheckPointer`・`xctraceCheckStore` に入れ、K=10、GC オフ、GOMAXPROCS 1、thread 固定。
- `cpu-counter` で backend ごとに 3 run（`s3a/record.sh`、`s3a/join.py`）。最初の pointer-1 は許可ダイアログ待ちで 2 回 300 秒の番人に止められ、承認後に取り直した。

| run | inst / 区間 | KPC 比 | cycles / 区間 | KPC 比 | 補完なしの sample |
| --- | ---: | ---: | ---: | ---: | ---: |
| pointer-1 | 8.478 G | −1.1% | 2.591 G | −0.7% | 97.8% |
| pointer-2 | 8.515 G | −0.7% | 2.632 G | +0.9% | 97.0% |
| pointer-3 | 8.494 G | −0.9% | 2.567 G | −1.6% | 98.3% |
| store-1 | 9.231 G | −0.0% | 2.737 G | +0.3% | 93.7% |
| store-2 | 9.155 G | −0.9% | 2.680 G | −1.8% | 98.8% |
| store-3 | 9.159 G | −0.8% | 2.683 G | −1.6% | 98.8% |

全 run で inst ±3%・cycles ±5%（7d-2c の基準）に入る。Pointer どうしの inst の幅は 0.44%。差は inst +685.8 M、cycles +103.5 M（KPC は +664 M、+119 M）。

### 5.2 分類の tool と旧 `mechanism.py` との違い

`perfclass 3a`（規則の hash は `meta/tool-frozen.sha256`）。同じ join の出力に 7d-2c の規則を sample ごとに当てた（`s3a/compare_7d2c.md`）。分類が移った sample はすべて、変えた規則で説明できた。

| 移り | Store inst M | 規則 |
| --- | ---: | --- |
| 他の map → links | 360.1 | links の表に `core.LinkStore` を含めた（旧は `PagedLinkStore` の map だけ） |
| self → links | 166.4 | links の型の method 自身を links にした |
| runtime → self | 459.8 | generic の helper（`slices.BinarySearchFunc` など）を呼び出し元に付けた。`tspath` など tsc の他の package を self に含めた |
| runtime → 他の map | 128.0 | runtime を leaf ではなく stack の連鎖で分けた（map の下の memmove・memclr） |
| runtime → 確保・memmove・その他・stdlib | 370.0・15.6・45.9・100.9 | runtime の分割 |
| self → walk | 123.5 | walk の一覧を見直した（`IsThisInTypeQuery`・`getCombinedNodeFlags`・`IsExpressionNode` ほか） |
| walk → self | 7.5 | walk の callback でない ast の closure（`ForEachReturnStatement.func1`）を外した |
| accessor → self | 6.1 | `Builder`・`Factory`・`OrderedNodeSet` を accessor から外した |

測定用 adapter は 0（7e の結線で消えた）。

### 5.3 仕組み別（1 区間）

| 仕組み | Pointer inst M | Store inst M | 差 inst M | Pointer cyc M | Store cyc M | 差 cyc M | 差 cycles の割合 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 links | 575.3 | 544.0 | −31.3 | 182.8 | 165.6 | −17.2 | −17% |
| 3 祖先 walk・utility | 143.2 | 242.4 | +99.2 | 46.1 | 74.2 | +28.0 | 27% |
| 4 node accessor | 310.6 | 158.7 | −151.9 | 99.6 | 47.9 | −51.7 | −50% |
| 5 他の map | 1,300.4 | 1,387.4 | +87.0 | 402.3 | 411.7 | +9.5 | 9% |
| 6 checker・binder・他の tsc の self | 5,680.4 | 6,312.7 | **+632.4** | 1,719.6 | 1,844.2 | **+124.6** | 120% |
| 7a runtime：確保 | 323.4 | 370.0 | +46.6 | 99.1 | 109.6 | +10.5 | 10% |
| 7b runtime：memmove | 16.2 | 15.6 | −0.6 | 4.9 | 4.5 | −0.4 | 0% |
| 7c runtime：その他 | 51.7 | 49.7 | −2.1 | 13.3 | 12.8 | −0.5 | 0% |
| 8 stdlib ほか | 94.4 | 100.9 | +6.5 | 28.9 | 29.6 | +0.7 | 1% |
| 合計 | 8,495.7 | 9,181.5 | +685.8 | 2,596.6 | 2,700.1 | +103.5 | 100% |

- links は Store の方が安い（7d-2c の A の page 表）。node accessor も Store の方が安い：Store の accessor は呼び出し元に inline されて 6 に入り、Pointer の多相 accessor は呼び出しのまま 4 に残る。4 と 6 は合わせて読む（+480.5 M inst、+72.9 M cycles）。
- MPKI（§5.4）：どの仕組みでも cycles の差は「inst の差 × Pointer の CPI」を下回る（`s3a/bins.md`）。IPC が下がった仕組みは無いので取らない。

### 5.4 関数別（inst の差の 80% まで）

由来は `perfclass` の go/printer の比較。Pointer で 0 の関数は、Pointer では呼び出し元に inline されている。

| 関数 | 由来 | Pointer inst M | Store inst M | 差 | AST 操作 |
| --- | --- | ---: | ---: | ---: | --- |
| `getControlFlowContainer` | 移植で変更 | 0 | 87.0 | +87.0 | 祖先 walk（FindAncestor の callback）。Store だけ inline されない |
| `isExportOrExportExpression` | 移植で変更 | 0 | 62.7 | +62.7 | 祖先 walk。同上 |
| `binder.NameResolver.Resolve` | 移植で変更 | 207.8 | 248.2 | +40.4 | Parent、locals、map |
| `getResolvedSymbol` | 本体同じ | 38.0 | 75.6 | +37.6 | links（Store は page 表の引きが inline され、Pointer は `PagedLinkStore.Get` の呼び出しで links に入る） |
| `isInAmbientOrTypeNode` | 移植で変更 | 0 | 30.6 | +30.6 | 祖先 walk |
| `narrowType` | 移植で変更 | 49.8 | 80.0 | +30.2 | Parent、Kind |
| `isReachableFlowNodeWorker` | 移植で変更 | 59.7 | 89.1 | +29.4 | flow、map |
| `getTypeAtFlowNode` | 移植で変更 | 147.0 | 174.2 | +27.2 | flow、Node の受け渡し |
| `isInPropertyInitializerOrClassStaticBlock` | 移植で変更 | 0 | 26.8 | +26.8 | 祖先 walk |
| `isPartOfImportEqualsModuleReference` | 移植で変更 | 0 | 22.9 | +22.9 | Parent |
| `getPropertyOfTypeEx` | 移植で変更 | 142.5 | 160.5 | +18.1 | map |
| `ast.List.Nodes` | — | 0 | 17.6 | +17.6 | list |
| `isSimpleTypeRelatedTo` | 本体同じ | 283.8 | 299.1 | +15.3 | （AST 操作なし） |
| `isTypeSubsetOfUnion` | 本体同じ | 216.3 | 231.3 | +15.1 | （AST 操作なし） |
| `getTypeAtFlowAssignment` | 移植で変更 | 17.8 | 32.0 | +14.1 | flow、Node |
| `ast.getCombinedNodeFlags` | — | 0 | 14.0 | +14.0 | 祖先 walk（B の遅い経路） |
| `getSymbol` | 本体同じ | 150.4 | 164.2 | +13.8 | map |
| `ast.GetAssignmentTarget` | — | 29.0 | 42.3 | +13.3 | 祖先 walk |
| `getTypeOfParameter` | 移植で変更 | 14.0 | 27.2 | +13.2 | Node |
| `checkSourceElementsList` | 片側だけ | 0 | 12.8 | +12.8 | list |
| `isMatchingReference` | 移植で変更 | 122.0 | 134.6 | +12.6 | Node の受け渡し（7d-2c §1.4） |

負の側の大きいもの：`compareNodes` −69.2（Pointer は `GetSourceFileOfNode` の親ループが inline）、`checkIdentifier` −56.4（Pointer は `getControlFlowContainer` が inline）、`checkTypeRelatedToEx` −43.3、`markPropertyAliasReferenced` −37.9（Pointer は `isExportOrExportExpression` が inline）。上位の正の差の多くは inline の境界が動いただけで、呼び出し元の負の差と対になる。

## 6 段 3b：heap

### 6.1 カテゴリ別（L01、checked 地点）

分類は `perfclass 3b`（`go tool pprof -traces -unit=B -sample_index=inuse_space`）。

| カテゴリ | Pointer MB | Store MB | Store − Pointer | Store の scan MB | Store の scan / bytes |
| --- | ---: | ---: | ---: | ---: | ---: |
| parse/bind の産物 | 2,214.7 | 1,252.9 | −961.9 | 516.3 | 0.41 |
| checker | 1,687.8 | 1,795.8 | **+108.0** | 1,740.0 | 0.97 |
| links | 643.1 | 749.6 | **+106.5** | 743.0 | 0.99 |
| map・その他 | 198.5 | 200.9 | +2.4 | 66.7 | 0.33 |
| 合計 | 4,744.1 | 3,999.2 | −744.9 | 3,066.0 | 0.77 |

- heap profile は先頭の runtime の frame（確保と map の code）を落とす（runtime/pprof）。このため stack から map を判定できず、map の確保はそれを求めた関数に付く。「他の map」の行は作れない。
- 照合：scan bytes の model の合計は `/gc/scan/heap` に対し Pointer +2.3%（4,547.3 / 4,446.5 MB）、Store +3.4%（3,066.0 / 2,965.8 MB）で、10% 以内。model は上位の確保場所の型を手で確かめた表（`s3b/scan_floor.py` の判断表）で、残りは scan とした（上振れの向き）。
- checked 地点の scan/live は Pointer 0.937、Store 0.742。

### 6.4 checker 側の原因（L01）

checker と links はどちらも 40 MB を超えるので行った。Store が Pointer より多い確保場所の上位：

| 確保場所 | Pointer MB | Store MB | 差 | 原因 |
| --- | ---: | ---: | ---: | --- |
| checker の `newSymbol`（Symbol の arena） | 490.2 | 571.0 | +80.7 | Symbol が 96 → 112 B。509 万個 × 16 B |
| links の `nodeLinkStore.newPage` | 0 | 139.5 | +139.5 | symbolNodeLinks の page を StoreID ごとに NodeRef で引く |
| links の `PagedLinkStore.Get` | 93.8 | 59.8 | −34.0 | Pointer はここに symbolNodeLinks の page も入る |
| binder の `newSymbol` | 245.9 | 287.1 | +41.2 | 同じ Symbol の +16 B（parse/bind のカテゴリ） |
| binder の `newSingleDeclaration` | 20.8 | 41.0 | +20.2 | 単一宣言の `[]Node` の要素が 8 → 16 B |
| checker の `Factory.newStore` ほか合成 Store | 0 | 約 12 | +12 | checker の合成 node |
| checker の `newSignature` | 69.0 | 73.3 | +4.3 | `declaration` が 2 word |

**Symbol の field**（overlay で全要素を数えた、`s3b/count-l01-*.json`）：

| field | Pointer | Store | checker の Symbol の非 0 | binder の Symbol の非 0 |
| --- | --- | --- | ---: | ---: |
| `ValueDeclaration` | `*ast.Node` 8 B | `store.Node` 16 B | 89.0% | 82.8% |
| `id` | `atomic.Uint64` 8 B | `SymbolId` 4 B（+4 B の詰め物） | 0.0%（Store） | 100% |
| `gid` | — | `atomic.Uint64` 8 B | 100% | 73.6% |
| `Members`・`Exports`・`ExportSymbol` | 8 B ずつ | 同じ | 0.0% | 12.1%・1.3%・2.5% |

- Store の Symbol は `id`（binder が振る file 内の番号）と `gid`（GetSymbolId の全体の番号）を別に持つ。checker の Symbol では `id` が常に 0、binder の Symbol では `gid` が 26% で 0。
- `Members`・`Exports`・`ExportSymbol` は checker の Symbol では常に 0（両 backend 同じ）。

**node links の page**（overlay、`symbolNodeLinks`）：

| | page | slot | symbol が入った slot | 密度 | page の bytes |
| --- | ---: | ---: | ---: | ---: | ---: |
| C01 Pointer | 1,206 | 308,736 | 301,895 | 0.978 | 2.5 MB |
| C01 Store | 3,607 | 923,392 | 301,895 | 0.327 | 7.4 MB |
| L01 Pointer | 15,160 | 3,880,960 | 3,430,463 | 0.884 | 31.0 MB |
| L01 Store | 59,607（表 8,670） | 15,259,392 | 3,430,463 | 0.225 | 122.1 MB |

- 入った slot の数は両 backend で一致する（同じ仕事）。Pointer は `GetNodeId` を引かれた node にだけ順に番号を振るので密で、Store は Store ごとの NodeRef（全 node の番号）で引くので疎になる。7d-2c 検証の残件 5（3 倍、密度 0.33）は C01 で再現し、L01 では 3.9 倍・0.225 になった。

**arena の末尾**：`core.Arena` は 1 chunk 最大 256 要素で、現在の chunk しか持たない（`internal/core/arena.go`）。checker の arena は 1 つなので末尾の空きは無視できる（Symbol 16 要素、Signature 67 要素）。binder の arena は file ごとなので、file ごとの最後の chunk の空きが積み上がる：Store の Symbol 557,767 要素 = 62.5 MB、単一宣言 9.3 MB。Pointer にも同じ無駄がある（Symbol 53.0 MB）。chunk の中で死んだ要素が保持される分は、この方法では測れない。

## 7 段 4：単価（途中）

### 7.1 回数（overlay、C01 の KPC 区間）

計数の overlay は `s4/count/gen.py`（関数の入口に counter、Store 側は受けた node を記録）、試験は `s4/count/count_test.go.txt`（`TestPerfOpCount`）。結果は `s4/count-c01-{pointer,store}.json`。

| 操作・関数 | Pointer | Store |
| --- | ---: | ---: |
| `Parent` | field の読み（数えない） | 24,066,135 |
| `IsNil` | nil 比較（数えない） | 69,487,109 |
| `File` ↔ `GetSourceFileOfNode` | 9,477,718 | 9,603,495 |
| `symbolNodeLinks.Get` | 3,299,156 | 3,299,156 |
| `getResolvedSymbol` ほか 20 関数 | | 両側で一致（`compareNodes` −344、`compareSymbolsWorker` −565、`getTypeAtFlowNode` −732 だけ違う） |

Pointer の `Parent` と nil 比較は、Store と同じ回数と仮定する（7d-2c と同じ）。

### 7.2 呼ばれる操作（再生のマイクロベンチ）

`internal/storeclitest/replay_test.go`。記録した node（合成 Store 1,182 件・nil 83 件を除く）を、hook の無い binary で check を済ませた program の中から (file, pos, end, kind) で引き直し、同じ順で操作だけを当てる。両 backend とも全件が見つかり、引き直した node の (file 名, pos, kind) の列の hash が記録時と一致した（parent `ba34bc37…`、file `b457303e…`、links `3b604892…`）。

単価は `proc_pid_rusage` の counter（root 不要、`TestReplayOp`、1 プロセスで 5 pass）と KPC（`BenchmarkReplayOpKPC`、`s4/kpc-replay.sh` をユーザーが sudo で実行、各 5 標本）の両方で取った（`s4/kpc-replay.txt`）。

| 操作 | KPC inst/node（中央値 [min, max]） P / S | rusage P / S |
| --- | --- | --- |
| file | 31.73 [31.70, 31.91] / 14.03 [14.00, 14.52] | 31.70 / 14.00 |
| links | 59.38 [59.35, 59.44] / 53.22 [53.17, 53.27] | 59.39 / 53.25 |
| kind | 7.21 [7.18, 8.44] / 10.34 [10.20, 11.12] | 6.00 / 8.00 |
| isnil | 6.84 [6.83, 6.85] / 16.09 [14.21, 19.19] | 6.00 / 12.00〜12.24 |
| parent | 8.20 [8.19, 9.54] / 23.90 [22.07, 25.29] | 7.00 / 18.96 |

- file と links は KPC と rusage が 0.1% 以内で一致した。
- 2,400 万 node を回す kind・isnil・parent は、KPC が rusage より多く、run の間でも揺れる。KPC の run 中は Spotlight（`mdworker_shared`・`mds`）で load が 14〜17 だった。Pointer の安定した値でも rusage より +0.8〜1.2 inst/node 多い。数百 MB の node の配列を回す間の page fault の kernel の命令を KPC が数え、rusage の `ri_instructions` は user の命令だけを数えると見ている（仮説、counter の mode は未確認）。段 3a の xctrace は EL0 で数えるので、単価は rusage の値を使う。

| 操作 | Pointer inst/op | Store inst/op | 差 | loop の差（基準）を引いた差 | cycles/op P / S |
| --- | ---: | ---: | ---: | ---: | --- |
| 基準：`Kind` の読み | 6.00 | 8.00 | +2.00 | 0 | 1.58 / 1.54 |
| `IsNil` | 6.00 | 12.00〜12.24 | +6.0〜6.2 | +4.0〜4.2 | 1.07 / 1.80〜1.95 |
| `Parent` + `IsNil` | 7.00 | 18.96 | +11.96 | +9.96 | 1.59 / 3.61 |
| `File` ↔ `GetSourceFileOfNode` | 31.70 | 14.00 | −17.70 | −19.70 | 8.15 / 2.53 |
| `symbolNodeLinks.Get`（新しい store に） | 59.39 | 53.25 | −6.14 | −8.14 | 10.1 / 10.2 |

- 基準の差 +2 は 16 B の要素の loop の形（objdump：Store は要素の copy と counter の減算が 1 つずつ多い）。実際の呼び出し元では Node は register か field から来るので、操作の単価は基準を引いた値にする。
- objdump では `IsNil` は Store が `CBZ`・`MOVH`・`UBFX`・`CBZW` の 4 命令、Pointer は `CMP` 1 命令で、+3。再生の +4.0〜4.2 は loop の分岐の形の差を含む。
- links の再生は storechecker と checker の `nodeLinkStore` の複製（どちらも非公開のため、`replay_test.go` に同じ code を置いた）。

### 7.3 回数 × 単価（呼ばれる操作だけ）

| 操作 | 単価の差 | 回数 | 予測 inst |
| --- | ---: | ---: | ---: |
| `Parent`（`IsNil` を除く：9.96 − 4.1） | +5.9 | 24.07 M | +142 M |
| `IsNil` | +4.1 | 69.49 M | +285 M |
| `File`（Store 6.00 × 9.60 M、Pointer 25.70 × 9.48 M） | | | −186 M |
| links（Store 45.25 × 3.30 M、Pointer 53.39 × 3.30 M） | | | −27 M |
| 合計 | | | **+214 M** |

段 3a の仕組み 1・3・4・6 の差は +548.4 M で、呼ばれる操作だけでは 39% を説明する。残りは呼び出し元の費用（2 word の受け渡し、inline の境界）と見て、objdump で数える（未完）。

### 7.4 呼び出しあたりの差（段 3a の self ÷ 回数）

| 関数 | 回数 | Pointer / 回 | Store / 回 | 差 / 回 | 差の計 M |
| --- | ---: | ---: | ---: | ---: | ---: |
| `getControlFlowContainer` | 406,749 | 0（inline） | 214.0 | +214.0 | +87.0 |
| `isExportOrExportExpression` | 143,198 | 0（inline） | 438.1 | +438.1 | +62.7 |
| `NameResolver.Resolve` | 245,568 | 846.4 | 1,010.8 | +164.4 | +40.4 |
| `getResolvedSymbol` | 3,015,167 | 12.6 | 25.1 | +12.5 | +37.6 |
| `narrowType` | 863,574 | 57.6 | 92.6 | +35.0 | +30.2 |
| `isReachableFlowNodeWorker` | 181,298 | 329.2 | 491.4 | +162.2 | +29.4 |
| `getTypeAtFlowNode` | 1,264,690 | 116.1 | 137.7 | +21.6 | +27.2 |
| `isTypeSubsetOfUnion`（本体同じ、AST なし） | 141,141 | 1,532.2 | 1,638.9 | +106.7 | +15.1 |
| `isSimpleTypeRelatedTo`（本体同じ、AST なし） | 2,864,084 | 99.1 | 104.4 | +5.3 | +15.3 |
| `checkIdentifier` | 260,324 | 608.3 | 362.1 | −246.2 | −64.1 |
| `compareNodes` | 4,526,280 | 49.1 | 35.4 | −13.8 | −62.3 |
| `markPropertyAliasReferenced` | 93,672 | 550.5 | 155.3 | −395.2 | −37.0 |

- `isTypeSubsetOfUnion` と `isSimpleTypeRelatedTo` は両 binary で命令数も呼び出し先も同じ（80・452 命令）。増分は data の並び（型 ID の順）か、1 ms sample の帰属の揺れ（この規模で ±5 M 程度）と見ている（仮説）。

### 7.5 呼び出し元の費用（objdump、測った binary `s3a/binary.sha256` の `4c1864ec…`）

- Store だけ inline されない関数（`getControlFlowContainer`・`isExportOrExportExpression`・`isInAmbientOrTypeNode`・`isInPropertyInitializerOrClassStaticBlock`・`isPartOfImportEqualsModuleReference`、片側だけの `checkSourceElementsList`）は、呼び出しごとに約 14 命令（stack 検査と frame で 6、2 word の引数の退避 2、epilogue 3、呼び出し側の引数と CALL 3）。Pointer の版は binary に symbol が無く、呼び出し元に inline されている。
- 2 word の受け渡しと inline の境界の移動は、flow の関数で静的な命令数と呼び出しの数が増えている：`narrowType` 240 → 480 命令・CALL 20 → 34、`getTypeAtFlowNode` 396 → 568・27 → 41、`getTypeOfParameter` 48 → 88・5 → 6。経路ごとの動的な数は取っていない。
- `getSymbol`・`isTypeSubsetOfUnion`・`isSimpleTypeRelatedTo` は両 binary で機械語が同じ（64・80・452 命令、呼び出し先も同じ）。§7.4 の呼び出しあたりの差は、その下の runtime（map の探索、`efaceeq`・`memequal`）に付いた分で、AST とは別の data 依存の費用。Swiss map の hash の seed は map ごとの乱数なので、探索の長さは run と backend で変わる。

## 8 段 5：照合（inst）

`s5/reconcile.py`、`s5/reconcile.md`。

| 項目 | 単価の出所 | 回数 | 予測 inst M（低〜高） |
| --- | --- | ---: | ---: |
| Parent（IsNil を除く） | 再生 | 24,066,135 | +140.5 |
| IsNil | 再生（高）／objdump +3（低） | 69,486,594 | +208.5〜+286.3 |
| File ↔ GetSourceFileOfNode | 再生 | 9,603,289 / 9,477,718 | −186.0 |
| symbolNodeLinks.Get | 再生 | 3,299,156 | −26.9 |
| 多相 accessor の辺（`Node.Expression` ほか） | 7d-2c objdump −5〜−10 | 5,255,329 | −52.6〜−26.3 |
| 型付き子 accessor・`Flow.Node` の辺 | 7d-2c objdump +5〜+13 | 6,075,031 | +30.4〜+79.0 |
| `List.At` + `Store.list` | 7d-2c の +20〜+27 M（250 万回）を回数で比例 | 2,449,804 | +19.6〜+26.5 |
| walk の loop・ForEachChild の辺 | Parent の単価 | 1,567,527 | +9.2 |
| Store だけ inline されない 6 関数 | objdump 14 命令/回 | 945,689 | +13.2 |
| 予測の合計 | | | **+156.0〜+315.6** |
| 実測（段 3a の仕組み 1・3・4・6） | | | **+548.4** |
| 残差 | | | **+232.8〜+392.4（42〜72%）** |

- `Store.node` の回数（38,041,799）は呼び出し元の論理的な関数で分けた（`s4/count2-c01-store.json`）：Parent 24,066,135、多相 accessor 5,255,329、型付き子 accessor ほか 6,075,031、List.At 1,077,777、walk の loop 723,790、ForEachChild 843,737。
- 子 accessor の単価は HEAD で測り直していない（7d-2c の objdump）。
- 残差は 20% を超えたので、段 3a に 2 回戻った。
  1. 1 回目：本体が同じ関数の増分。仕組み 6 の中で `isSimpleTypeRelatedTo` +22.7、`isTypeSubsetOfUnion` +15.1、`isRelatedToEx` +10.6、`compareTypeNames` +9.1 M。AST 操作が無く、機械語も同じなので、上の予測の項目では説明できない。§7.5 の data 依存の費用と見ている。
  2. 2 回目：Node を受け渡す flow の関数（`narrowType` +30.2、`isReachableFlowNodeWorker` +29.4、`getTypeAtFlowNode` +27.2、`getTypeAtFlowAssignment` +14.1、`getTypeOfParameter` +13.2、`isMatchingReference` +12.6 M）。静的な命令数と呼び出しが増えている（§7.5）が、経路ごとの動的な数を取っていないので予測には入れていない。`getResolvedSymbol` +37.6 M は links の引きの inline で、再生の links（−8.1/回）より実際の文脈の方が Store に不利（links と合わせた実測 +6.3 M、予測 −26.9 M）。
- **未帰属**：+233〜+392 M inst。候補は (1) flow の関数の 2 word の受け渡しと inline の境界（上の 2 回目、計 +127 M の関数群）、(2) 本体が同じ関数の data 依存の費用（1 回目、計 +57 M）、(3) `getResolvedSymbol` の文脈での links の費用（+33 M）。
- cycles（仕組み別、段 3a の実測）：walk +28.0、self +124.6、accessor −51.7、links −17.2、他の map +9.5、確保 +10.5 M。inst から cycles への換算は安定しないので、cycles の残差の規則は置かない（§8）。再生の cycles/op の差（loop の差を引く前）：Parent + IsNil +2.0、IsNil +0.8、File −5.6、links +0.1。

## 9 修正の候補（gate ごと）

修正は本書の範囲外。「効く費用」と「見積もった効果（割り付ける量に対する割合）」を付ける。

### 9.1 L01 checked HeapAlloc（割り付ける量 202 MB）

| 候補 | 効く費用 | 見積もり | 割合 | 根拠 |
| --- | --- | ---: | ---: | --- |
| Symbol を 96 B に戻す（`ValueDeclaration` を 8 B の鍵に、`id` と `gid` を 1 本に） | 保持 heap、scan の割合 | −122 MB（checker 81 + binder 41） | 61% | §6.4 の field の表 |
| symbolNodeLinks を密に引く（Pointer の `GetNodeId` のように、引かれた node にだけ番号を振る） | 保持 heap、scan | −91 MB（page 122 → 31 MB） | 45% | §6.4 の page の表 |
| binder の arena を program で 1 つにする（file ごとの最後の chunk の空きを無くす） | 保持 heap、scan | −72 MB（Symbol 62.5 + 単一宣言 9.3） | 36% | §6.4 の arena の末尾（Pointer にも 53 MB ある） |
| 単一宣言の `[]Node` の要素を 8 B に | 保持 heap | −20 MB | 10% | binder の `newSingleDeclaration` 41.0 / 20.8 MB |

上の 2 つで 213 MB になり、比は約 0.797（3,998.5 − 213 = 3,785.5 MB、Pointer 4,745.3 MB）。どれも scan される bytes なので、GC の CPU も減らす向き。

### 9.2 C01 Check KPC（割り付ける量 +119 M cycles、inst +664 M）

| 候補 | 効く費用 | 見積もり（inst） | 割合（inst の差に対して） |
| --- | --- | ---: | ---: |
| `IsNil` を 1 比較にする（nil の Node を `h == nil` だけで表す。今は kind の読みと比較が 3 命令多い） | mutator | −209〜−286 M | 31〜43% |
| `Parent` の辺の解決（+5.8 命令/回） | mutator | −141 M まで | 21% |
| Store だけ inline されない callback 付きの walk の関数 | mutator | −13 M（呼び出しの費用だけ） | 2% |
| flow の関数の 2 word の受け渡し（未帰属の候補） | mutator | 未確定（+127 M の関数群） | — |

cycles の効果は inst から換算しない（storeexp と 7d-2c で、減った inst が cycles に出ない例がある）。再生の cycles/op では Parent + IsNil が +2.0、IsNil が +0.8 cycles/回で、回数を掛けると cycles の差の全体に近い大きさになるが、loop の文脈の値なので上限として読む。

### 9.3 L01 CLI wall（割り付ける量 3.77 s）

- 割り付け先は Check の mutator（C01 の構成比で 3.15 s）。段 3a の Store の Check の内訳は self 68%、他の map 15%、links 6%、確保 4%、walk 3%、accessor 2%（cycles）。AST に関わる狭い範囲（links + walk + accessor）は 10.7% で、C01 の退行（4.4%）を全部消しても 3.15 s には遠い。
- 線 0.70 には、checker の CPU（両 backend に共通の仕事）、L01 の GC（Store 1.52 s）、paging と sys（7e の sys の比 0.588。割り付けの外）を合わせて削る必要がある。heap の候補（§9.1）は GC と paging の両方に効く向き。
