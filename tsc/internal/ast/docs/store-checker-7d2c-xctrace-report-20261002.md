# Store checker の Check の差の帰属 (xctrace) 報告

作成日: 2026-10-02。[指示書](store-checker-7d2c-xctrace-instructions-20261002.md) の実行結果。生出力は `_store-checker-7d2c-xctrace/` (commit しない)。

## 判定

| 受入 | 結果 |
| --- | --- |
| 結合の検証 (inst ±3% / cycles ±5%、全 run) | **合格**。6 run とも線内 (inst −0.6〜+1.1%、cycles −0.2〜+3.3%) |
| A/A (Pointer 同士の inst の差 ≤ 1%) | **不合格**。3 run の幅 1.6% (8.551〜8.686 G)。pointer-1 と pointer-3 の差は 0.45%。原因は §2.3 の補完 (pointer-2 は きれいな増分が 86%) と見ている (仮説) |
| inst / cycles の差を分類で 8 割以上説明 | **合格**。分類の合計は差の全部。仕組みで名前を付けられた項目 (§3.2 の 1〜5) は inst 65%・cycles 68% |
| 7d-2c の 2 項目との照合 | adapter 見積 14 ms → 標本 18.0 ms。links の pageMap 見積 24 ms → 標本 21.7 ms |

**主な結論** (確定):
- 差の最大項目は、`ast/store` の祖先 walk 系の関数が **inline されなくなったこと** で、cycles の差の 39% (+147 M cycles/回) を占める。
  - 該当は `FindAncestor`・`GetCombinedNodeFlags` など、Pointer では inline されて呼び出し元に溶けている関数と、その callback。
  - 原因は inline 予算の超過。`FindAncestor` は Pointer の cost 33 に対し Store は 89 で、予算 80 を超える。ループ 1 回は 9 命令が 21 命令になる。
- links の pageMap は 18%、adapter (計測側の偏り) は 15%。
- b719 で見えた `Store.locals` の map (Resolve の全祖先) は、もう標本に出ない。

## 1. 変更・入力

- **開始時の HEAD**: `cd66aaa4d3` (before 2980ba4970 + 指示書の決定事項の commit。コードの差は無い)。
- **差分**: `internal/storeharness/xctrace_test.go` の新規 1 file (`TestXctraceCheck`) だけ。sha256 `eccb3dc5…` (`diff.sha256`)。
  - env `STORE_CHECKER_XCTRACE_SIDE` (pointer | store)・`_K` (既定 10)・`_LOG` で動く。
  - 条件は KPC と同じ: GC オフ、`GOMAXPROCS(1)`、`LockOSThread`、各回の前に `runtime.GC()` を 1 回。program の構築と bind は回の外。
  - 1 回の仕事は `checkPointer` / `checkStore` そのまま。
  - 各回の setup と check の時間を log に書く。xctrace は launch したプロセスの出力を見せないため。
- **binary**: `go test -c` (tag なし)。sha256 `0279a299…` (`binary.sha256`)。Go 1.27.1、M1。
- **fixture**: `TestWriteCompilerProjectFixture` で作り直した (PASS、files 124 / modules 91 / questions 914)。7d-2c の fixture とは sha256 が違う。中身の同一性は class equal の検査に任せ、byte 一致は確かめていない。
- **inline の記録**:

| package | 取得したもの |
| --- | --- |
| `ast`・`ast/store` | `-m=2` |
| `storechecker`・`checker` | `-m=1` (**指示書からの逸脱**) |

  `-m=2` では `go` コマンドが 23 GB、compile が 9 GB ずつを抱えて swap (23.5 GB) を使い切り、30 分進まなかった。`-m=1` では inline できるかどうかは出るが、cost は出ない。§4.1 の cost は `ast/store` の分だけ。

## 2. 記録と結合

### 2.1 テンプレートと counter

- **テンプレート**: user template `cpu-counter`。counting mode は `bottleneck bottlenecks EL0`、1 ms ごとの sample。
- **counter の並び**: `kdebug-counters-with-time-sample` の値は 12 本。名前は export に出ない。
  - [0] = cycles: 1 ms あたり 3.14 M で、P-core のクロックに合う。
  - [1] = inst: KPC の inst と ±1% で合う。
  - [2..] は bottleneck 用の configurable counter で、使っていない。
- **値は core 単位の累積**: thread が core を移ると値が 10^13 桁で飛ぶ。
- **XML の構造** (Instruments 27): `tagged-backtrace` の直下が `frame` で、leaf が先頭。Go の関数名は解決済み。inline frame は出ない (inline された関数は外側の関数に入る)。

### 2.2 結合

- 各 sample の増分は、同じ thread の直前の sample からの差 (`join.py`)。
- 同じ core で 2 ms 以内のものだけを「きれい」とする。
- それ以外 (core の移動・長い間隔) は、同じ leaf 関数のきれいな増分の平均で補う。標本が 3 未満の leaf は全体の平均で補う。
- 区間は stack に `checkPointer` / `checkStore` を含む sample。

| run | 区間の sample | きれい | inst/回 | KPC 比 | cycles/回 | KPC 比 | check の wall (10 回) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| pointer-1 | 8,252 | 97.9% | 8.551 G | −0.44% | 2.589 G | −0.15% | 8.34 s |
| pointer-2 | 8,698 | 86.4% | 8.686 G | +1.13% | 2.679 G | +3.32% | 8.97 s |
| pointer-3 | 8,543 | 89.1% | 8.590 G | +0.01% | 2.632 G | +1.50% | 8.60 s |
| store-1 | 9,874 | 84.2% | 10.156 G | −0.26% | 3.026 G | +1.48% | 9.97 s |
| store-2 | 9,819 | 89.7% | 10.234 G | +0.51% | 3.028 G | +1.54% | 10.22 s |
| store-3 | 9,612 | 89.1% | 10.125 G | −0.56% | 2.977 G | −0.17% | 9.69 s |

KPC は Pointer 8.589 G / 2.593 G、Store 10.182 G / 2.982 G。

- **3 run の平均**: 差は inst +1.562 G / cycles +0.377 G。KPC の差は +1.593 G / +0.389 G。
- **区間の外** (parse・bind・GC): Pointer 2.24 G inst/回、Store 1.51 G。Store は fixture から作るので、構築の仕方が違う。

### 2.3 記録の障害 (記録のため)

1. **binary を作り直した直後の record**: 対象が RSS 6 MB・CPU 0 のまま約 3.5 分止まり、その後に進んだ。trace の先頭 3.5 分は空になる。
2. **store-1 の 1 回目**: "ending recording" のあと 28 分止まった。
   - ダイアログは出ていなかった (ユーザー確認)。動いていたのは `dtsecurity` だけ (CPU 11%)。
   - 止めてやり直した。`record.sh` には、300 s で止めて 1 回だけやり直す番人を入れた。
   - やり直し後の 5 本は通った。

## 3. 分類

### 3.1 指示書 §3 の分類 (self、1 回あたり)

| 分類 | P inst M | S inst M | 差 inst M | 割合 | P cyc M | S cyc M | 差 cyc M | 割合 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| 共通 | 5924 | 6376 | +452 | 29% | 1796 | 1867 | +71 | 19% |
| Store 固有: accessor | 0 | 260 | +260 | 17% | 0 | 75 | +75 | 20% |
| Store 固有: utility | 0 | 341 | +341 | 22% | 0 | 105 | +105 | 28% |
| map | 1566 | 1882 | +317 | 20% | 492 | 571 | +79 | 21% |
| 測定用 adapter | 0 | 193 | +193 | 12% | 0 | 57 | +57 | 15% |
| 書き換えた関数 | 0 | 420 | +420 | 27% | 0 | 129 | +129 | 34% |
| Pointer 固有 | 433 | 0 | −433 | −28% | 138 | 0 | −138 | −37% |
| その他 | 686 | 699 | +13 | 1% | 207 | 205 | −2 | −1% |

この分類は、inline の違いで帰属が両側をまたいで動くので読みにくい:
- 「共通」の差 +452 M には、`compareNodes` の −91 M と `Node.File` の +92 M のような付け替えが入っている。Pointer は `GetSourceFileOfNode` を inline し、Store は `File()` を呼ぶ。
- 「書き換えた関数」の上位は `FindAncestor` に渡す callback (`.func1`) で、Pointer では inline されて呼び出し元に溶けている。

そこで、仕組みで括り直したのが §3.2 です。

### 3.2 仕組みで括った分類 (`mechanism.py`、両側に同じ規則)

| 仕組み | P inst M | S inst M | 差 inst M | 割合 | P cyc M | S cyc M | 差 cyc M | 割合 | 差 ms (3.2 GHz) |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 links の pageMap | 0 | 226 | +226 | 14% | 0 | 69 | +69 | 18% | +21.7 |
| 2 測定用 adapter | 0 | 193 | +193 | 12% | 0 | 57 | +57 | 15% | +18.0 |
| 3 祖先 walk・utility (+ callback) | 71 | 549 | +477 | 31% | 23 | 170 | +147 | 39% | +45.9 |
| 4 node accessor | 320 | 256 | −64 | −4% | 102 | 74 | −28 | −7% | −8.7 |
| 5 他の map | 1566 | 1657 | +91 | 6% | 492 | 501 | +10 | 3% | +3.0 |
| 6 checker・binder・他の ast の self | 5565 | 6116 | +552 | 35% | 1695 | 1804 | +109 | 29% | +34.2 |
| 7 runtime ほか | 1088 | 1175 | +87 | 6% | 322 | 334 | +12 | 3% | +3.7 |

各行に入れたもの:
- **3**: leaf が次のどれかのもの。
  - `FindAncestor`・`FindAncestorOrQuit`・`FindAncestorKind`・`GetCombinedNodeFlags`・`GetRootDeclaration`・`IsGlobalSourceFile`・`GetThisContainer`・`GetAssignmentTarget`・`GetEnclosingBlockScopeContainer`・`GetSymbolId`・`HasQuestionToken`・`GetSourceFileOfNode`・`IsExpressionOfOptionalChainRoot`・`accessKind`
  - これらに渡す callback (`getControlFlowContainer.func1` ほか)
- **4**: leaf が node の method のもの (Store の `Node.*`、Pointer の `(*Node).*`・`(*X).*`)。

読み方:
- **3 の差は上限**。Pointer では同じ walk が inline されて、6 の呼び出し元の self に入っている。3 と 6 を合わせた +256 M cycles (68%) が、「祖先 walk と、その inline の差」の合計の上限になる。
- **4 が負**なのは、Pointer の `Text`・`Expression`・`Initializer` などが inline されずに呼ばれているのに対し、Store の accessor の多くは inline されて呼び出し元の self に入っているため。

### 3.3 共通関数の差の上位 (inst、抜粋。全表は `classify.md`)

| 関数 | P | S | 差 |
|---|---:|---:|---:|
| `checker.(*Checker).compareNodes` | 230.0 | 139.3 | −90.7 (`File()` を呼ぶ側に付け替え) |
| `binder.(*NameResolver).Resolve` | 199.0 | 266.9 | +67.8 |
| `checker.(*Checker).checkTypeRelatedToEx` | 85.2 | 28.1 | −57.1 |
| `slices.BinarySearchFunc[...*checker.Type...]` | 237.5 | 289.8 | +52.3 |
| `checker.(*Checker).checkIdentifier` | 130.5 | 88.5 | −42.0 (`getControlFlowContainer` が inline されなくなった) |
| `checker.(*Checker).getTypeAtFlowNode` | 155.0 | 193.7 | +38.7 |
| `checker.(*Checker).isMatchingReference` | 93.0 | 123.6 | +30.6 |
| `checker.(*Checker).narrowType` | 49.6 | 77.2 | +27.5 |

inclusive では:
- `checkIdentifier`: +953 M inst / +246 M cycles。cycles の差の 65%。
- その中で最大の `getFlowTypeOfReferenceEx`: +596 M / +143 M。
  - 中身は pageMap (+47.7 M cycles) が最大で、`Node.File`・`Node.Text` の呼び出し、`getTypeAtFlowNode`・`isMatchingReference`・`narrowType` の self が続く。
  - 詳細は `callees-checkIdentifier.md` と `subleaf-getFlowTypeOfReferenceEx.md`。

## 4. 切り分け

### 4.1 2 word 渡しの境界と inline

**確定** (`-m` 出力): 次の関数は Pointer で inline できて、Store ではできない。

| 関数 | Pointer の cost | Store の cost |
| --- | ---: | ---: |
| `FindAncestor` | 33 | 89 |
| `FindAncestorOrQuit` | inline 可 | 95 |
| `GetCombinedNodeFlags` | 64 | 245 |
| `GetRootDeclaration` | inline 可 | 106 |
| `IsGlobalSourceFile` | inline 可 | 91 |
| `GetSymbolId` | 53 | 83 |
| `HasQuestionToken` | inline 可 | 86 |
| `GetSourceFileOfNode` ↔ `Node.File` | inline 可 | 111 |

- inline の予算は 80。Store の cost は Pointer 側にはない (`-m=1` なので Pointer の cost は一部だけ)。
- これらの callback (`getControlFlowContainer.func1` 100 M inst、`isExportOrExportExpression.func1` 50 M、`isInPropertyInitializerOrClassStaticBlock.func1` 25 M、`isInAmbientOrTypeNode.func1` 19 M) も、`FindAncestor` が inline されないので間接呼び出しのまま残る。
- **逆向き** (Store だけ inline できる): `nodeLinkStore.Get/Has/TryGet`、`symbolArenaLinkStore` 系、`orderedSet` ほか。checker 側の hot path にあるのは link store の Get。
- **上限**: `store.Node` を受け渡す、inline されない関数の self cycles の合計 = §3.2 の 3 と、Store の 4 (74 M) で **244 M cycles/回**。

**ループ 1 回の命令数** (確定、`go tool objdump`):

| | 1 回の命令数 | 中身 |
| --- | ---: | --- |
| Store の `FindAncestor` | 21 | 2 word の spill 2、`IsNil` が kind まで読む 4 (Pointer は nil 比較 1)、`Store.node` の LDP・CMP・BGE・ADD 4、callback の間接呼び出し |
| Pointer の `FindAncestor` (単体の版) | 9 | — |

foreign の枝と panic はループの外に置かれていて、通常の経路は通らない。

### 4.2 辺の解決と nil 終了の親ループ

**確定** (overlay の計数、`count.txt`): `Parent()` は 24,650,495 回、nil を返したのは 547,441 回で、7d-2c と同値。呼び出し元は 198 関数あり、上位は次のとおり。

| 呼び出し元 | 回数 | nil |
| --- | ---: | ---: |
| `ast/store.FindAncestor` | 5,338,023 | 280,785 |
| `storebinder.(*NameResolver).Resolve` | 2,046,805 | 10,014 |
| `isExportOrExportExpression.func1` | 1,673,567 | 143,196 |
| `(*Checker).narrowType` | 1,614,856 | 0 |
| `ast/store.FindAncestorKind` | 1,169,290 | 97,869 |
| `(*Checker).checkIdentifier` | 1,038,326 | 0 |
| `GetAssignmentTarget` / `IsExpressionOfOptionalChainRoot` | 895,426 / 888,734 | 0 |
| `shouldMarkIdentifierAliasReferenced` | 791,462 | 0 |
| `getTypeAtFlowAssignment` | 747,092 | 0 |
| `FindAncestorOrQuit` / `GetCombinedNodeFlags` / `GetThisContainer` | 723,790 / 714,673 / 708,784 | 3,891 / 0 / 0 |

**見積り** (計数 × 機械語の命令数):
- 1 回の `Parent()` + `IsNil` は、inline された形で Store 9 命令、Pointer 2 命令。差は +7 命令。
- 24.65 M 回 × 7 = **約 173 M inst** (inst の差の 11%)。
- spill を含む非 inline 経路 (+12 命令/回) を全部に当てると、上限は約 296 M。
- cycles への換算はしない。単価の cycles を測っていないため。7d-2c の storeexp では「inst の減りが cycles に出ない」例がある。

**照合**:
- §3.2 の 3 と 6 の差 (+1,029 M inst) のうち、親ループの命令の増分は 17〜29%。
- 残りは inline されない呼び出しの prologue / epilogue と callback の間接呼び出し、関数の中の 2 word の spill と見ている (**仮説**。関数ごとの呼び出し回数を取っていない)。

`Node.File()` の parent walk (合成 Store の node) は 276 回で、無視できる。

### 4.3 V1 の分岐 (合成 Store の私有表)

**確定** (計数):

| 解決 | bound | private |
| --- | ---: | ---: |
| `symbolAt` | 968,575 | 0 |
| `flowAt` | 333,072 | 497 |
| `localsAt` | 2,309,705 | 0 (うち i == 0 は 1,786,599 で、表を引く前に返る) |

- 分岐の合計は 361 万回で、ほぼ常に同じ向き (予測が当たる)。
- 1 回 2〜3 命令 (`s.bound` の load と分岐) として **7〜11 M inst** (差の 1% 未満)。
- 標本には出ない (inline されている)。**V1 の分岐は差の原因ではない。**

### 4.4 links と map

**確定**: map の呼び出し元ごとの差 (1 回あたり、norm 後に突き合わせ)。

| 呼び出し元 | 差 inst M | 差 cycles M |
| --- | ---: | ---: |
| `PagedLinkStore[resolvedSymbol].Get` (pageMap) | +225.7 | **+69.4** |
| `getSymbol` (symbol table) | +18.6 | +4.3 |
| `isReachableFlowNodeWorker` | +13.1 | +3.5 |
| `LinkStore[referenceKinds].Get` | +13.0 | +3.3 |
| `LinkStore[node 鍵, resolvedType 等]` | ほぼ 0 | ほぼ 0 (`NodeKey` 鍵と `*ast.Node` 鍵で同等) |

- **pageMap**: +69.4 M cycles = 21.7 ms (3.2 GHz 換算)。7d-2c の見積り (3.3 M 回 × 7.3 ns = 24 ms) と 1 割で合う。
- **`Store.locals` の map** (b719 で 0.78 G):
  - Resolve の部分木の中の map は `getSymbol` と `referenceKinds` だけで、両側にある。`Store.locals` の map は出ない。
  - Resolve の self は +18.9 M cycles。locals の解決は `localsAt` の配列で、inline されている。
  - **b719 の経路は残っていない** (確定)。

## 5. 7d-2c の確定項目との照合

| 項目 | 7d-2c の見積り | xctrace (標本) | 差 |
| --- | --- | --- | --- |
| 測定用 adapter | 約 14 ms (単体 11.1 ms + 推定) | 57 M cycles = 18.0 ms、193 M inst | +4 ms。`fmt.Sprintf`・map・malloc の cache の冷え方が単体ベンチと違う (仮説) |
| links の pageMap | 約 24 ms | 69.4 M cycles = 21.7 ms | −2 ms |

## 6. 次の改善段の候補

基準は KPC の Check (Pointer 2.593 G、Store 2.982 G、比 1.150)。見積りは xctrace の 1 回あたりの差。

| 候補 | 内容 | 見積り | Check cycles 比 | 受入条件 |
| --- | --- | --- | --- | --- |
| A | `symbolNodeLinks` の鍵を slice で引けるもの (StoreID ごとの page 表 か file index) にする | −226 M inst / −69 M cycles (確定値の全部) | −0.027 | Check KPC の比が 0.02 以上下がる。`PagedLinkStore` 配下の map が標本から消える。等価 (段 1・段 2) が不変 |
| B | `Parent()` / `IsNil` / `Store.node` の inline cost を下げ、§4.1 の 8 関数を予算 80 内に戻す (例: foreign の枝を非 inline の関数に出す、walk の nil 判定を `h == nil` だけにできる形にする) | 上限 −147 M cycles (§3.2 の 3)。callback と呼び出しの費用だけが消えるとして −40〜−100 M cycles (**仮説**) | −0.015〜−0.039 (上限 −0.057) | `-m=2` で 8 関数が `can inline`。Check KPC の比が下がる。Walk / Bind の KPC が線内 |
| C | 測定用 adapter の鍵を struct にする (`fmt.Sprintf` をやめる) | −193 M inst / −57 M cycles (計測の偏り) | −0.022 | adapter の単体費用 < 1 ms。§3.2 の 2 が 5 M cycles 未満 |
| D | `Parent()` 自体の命令数 (+7/回) を減らす (32B header で `×32` を shift に。境界検査は構造上要る) | ≤ −173 M inst (cycles は未測定) | 不明 | Walk KPC で inst が下がり、cycles が上がらない |

**線 1.10 に届く組み合わせ**:
- **A + C**: 2.982 − 0.069 − 0.057 = 2.856 G で、比 **1.101**。線上。
  - C は計測の偏りを除くだけで、checker は速くならない。
  - 「Store の checker の真の比」は C を引いた 1.128。A だけで 1.101 になる。
- **A + B (下限) + C**: 比 1.06〜1.09 で、線の内側。
- **B が上限まで効くとき**: A + B + C で 1.04。
- A と C は確定値の見積りで、B は仮説を含む。B は、inline が戻ったときに呼び出し元の self がどれだけ増えるかを測っていない。

## 7. 指示書の外で判断したこと

1. **`storechecker`・`checker` の inline の記録を `-m=1` にした** (§1)。`-m=2` が 8 GB 機で完了しないため。
2. **core 単位の counter の補完** (§2.2)。きれいな増分が 84〜98% なので、残りを leaf ごとの平均で補った。A/A の 1% を外れたのは、この補完の誤差と見ている。
3. **§3.2 の仕組み別の分類を足した**。§3 の分類のままだと、inline の差で帰属が両側をまたいで動き、差の正体が読めないため。
4. **overlay の計数** (`_store-checker-7d2c-xctrace/overlay/`) は store.go・bound.go への計数の挿入と、計数用の test だけ。計測の binary とは別に build した。
5. **record の番人** (300 s で止めて 1 回やり直す) を入れた。§2.3 の 2 の停止のため。
