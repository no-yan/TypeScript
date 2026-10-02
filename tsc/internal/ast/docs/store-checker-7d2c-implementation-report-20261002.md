# Store checker の性能 (TODO 7d-2c) 実装報告

作成日: 2026-10-02。[実装指示書](store-checker-7d2c-implementation-instructions-20261002.md) の §10 に従う。生出力は `_store-checker-7d2c-impl/` (commit しない)。

## 判定

- **2c の測定: そろった (縮小と未取得あり)。**
  - §2・§3・§4・§5・§7・§8 は指示書どおりに取った。
  - §6 の KPC と ns は、要求を 1/1000 に間引いた列で取った (§6.1)。全量の KPC は完走しない。
  - §7.3 の「2 word 渡しの境界」の動的回数は取っていない (§7.3)。
- **各ゲート:**

| ゲート | 線 | 値 | 判定 |
| --- | --- | --- | --- |
| Check cycles (KPC、5 run の median) | ≤ 1.10 | **1.150** (inst 1.186) | **不合格 (性能未達)** |
| retained (check の後、baseline からの増分) | ≤ 0.85 | 0.834 (絶対量 0.842) | 合格 (余裕 0.016) |
| mark CPU (check の後、最後の強制 GC) | ≤ 0.75 | 0.652 | 合格 |

- 印字は線を置かない。ただし再現する退行がある (§6)。
  - Store の `typeToString` / `symbolToString` は、呼ぶたびに容量 4,096 の Store (約 420 KB) を確保する。
  - 診断の多い構成では、確保量が Pointer の 22 倍。
  - 指示書のとおり、Check が合格でも未解決として残す。

## 1. 変更

| file | 内容 |
| --- | --- |
| `storeharness/fixture.go` (新規) | 値だけの `Fixture`、H1 Program を包む `Recorder`、測定用 adapter `FixtureProgram` (§3)。記録に無い問い合わせは panic |
| `storeharness/driver.go` | `runStore` から `runStoreOn(r, storechecker.Program, diag, walk)` を切り出した (driver の仕事は不変) |
| `walker.go`・`pointerwalker.go` | `writeTypeOrSymbol` を `typeOf` (型の解決) と `storePrintType` / `pointerPrintType` (印字) に、walk の選別を `storeWalked` / `pointerWalked` に分けた。出力は不変 (§8 の回帰で確認) |
| `project_test.go` (新規) | compiler プロジェクトの program と targets、§2 の `TestCompilerProjectEquivalence` |
| `fixture_test.go` (新規) | §3.3 の同一性: `TestFixtureIdentityCompilerProject`、`TestFixtureIdentityConfigurations` |
| `measure_test.go` (新規) | fixture の書き出し、Check KPC 3 本 (`BenchmarkCheckKPC{Pointer,Store,Program}`) |
| `retained_test.go` (新規) | §5 と印字の後の retained / GC。子プロセスで、`STORE_CHECKER_RETAINED_WORK=check|print` |
| `print_test.go` (新規) | 印字の manifest、全区間ベンチ (`BenchmarkPrint`、`BenchmarkPrintKPC`)、出力一致 `TestPrintOutputs` |

- production・生成器・生成物・Pointer の package・go.mod は変えていない。
- 計数の instrumentation は scratch の overlay (`count/`、`linkbench/`、`adaptercost/`) だけで行った。追跡する変更とゲートの binary には入っていない。

**before と開始時の状態**
- before は 9c93098cc3 で、開始時の HEAD も同じ。
- 開始時の差分は 3 つで、どれもユーザーのもの:
  - `store-checker-design-20260923.md` の変更
  - 指示書 2 本 (untracked)

## 2. 前提の等価 (§2)

`TestCompilerProjectEquivalence` は **equal**。

| 項目 | 値 |
| --- | --- |
| 診断対象 / walk 対象 | 124 / 79 file |
| 診断 | 0 |
| suggestion | 274 |
| walk の記録 | 型 596,674・symbol 596,674 (distinct 81,270) |
| CompareSymbols の対 | 272,046 |
| mode の位置 | 184 |
| 合成 import | 0 |

## 3. 測定用 program (§3)

### 3.1 fixture

H1 Program を `Recorder` で包み、driver の全仕事 (診断・suggestion・walk) で問われた (引数の値 → 答え) を記録する。

| 種類 | 鍵 / 中身 |
| --- | --- |
| file | path |
| node | (path, pos, end, kind) |
| `*ResolvedModule` | 同一性を保つ表 |
| file の text | bytes。JSON の string は不正な UTF-8 を U+FFFD に置き換えるため。段 2 の `regexInvalidUtf8WithUnicodeFlag.ts` で発見し、修正した |

間接保持を値に落とした 3 つ:

- **symlink cache**: 4 つの map を値に。
- **package.json の項目**: file の text。測定側で `packagejson.Parse` を当て直す。記録時に `Fields` の DeepEqual で、当て直した結果が program のものと一致することを確かめる。
- **`ResolutionDiagnostics`**: Store のどの package も読まない。落として数を記録する (compiler プロジェクトでは 0)。

`ParsedCommandLine` と project reference は nil のときだけ記録し、nil 以外が来たら記録で panic する。compiler プロジェクトと段 2 の全量で、panic は 0。

compiler プロジェクトの fixture の大きさ:

| 項目 | 値 |
| --- | --- |
| file | 124 |
| module | 91 |
| 問い合わせ | 914 件 |
| JSON | 11.2 MB |

### 3.3 同一性

| 対象 | 結果 |
| --- | --- |
| compiler プロジェクト | equal |
| 段 2 の全 15,205 構成 | equal 13,446、比較しない 1,759 (runner の skip・却下・Pointer の panic)、不一致 0 |

段 2 の equal 13,446 は 2b-3 の strict の equal 数と同じ。

### 3.4 構築経路の監査

**読み:**
- `FixtureProgram` の構築は storeparser・storebinder・`store.NewSyntheticImport`・`packagejson.Parse`・`symlinks` だけ。
- storechecker・storeprinter は `tsoptions` を通して Pointer の parser を link するが、呼ぶのは `GetSupportedExtensions` (options だけを読む純関数) と型だけ。

**profile:**
- Store の子プロセスの heap profile (check・print、retained・gc、計 36 本) で、次の名前を含む stack は 0 件 (`audit-store-profiles.txt`)。
  - `tsc/internal/(parser|compiler|binder|checker|printer).`
  - `ast.(*NodeFactory)`
  - `cachedLib` / `newPointerProgram` / `newProgram`
  - `ast.NewSourceFile`
  - `tsoptions.GetParsed*`
- 同じ検査を Pointer の profile に当てると 416 件当たる。

## 4. Check KPC (§4)

binary は `storeharness.kperf.test` (sha256 `ae08055f…`)。Pointer → Store → Program の順を交互に 5 run。GC オフ、`GOMAXPROCS(1)`、`SingleThreaded`。node 数は 939,772。

| | cycles (median) | inst (median) | IPC | cycles/node | inst/node |
| --- | ---: | ---: | ---: | ---: | ---: |
| Pointer (H1 driver) | 2.593 G | 8.589 G | 3.31 | 2,759 | 9,140 |
| Store (測定用 adapter) | 2.982 G | 10.182 G | 3.41 | 3,174 | 10,837 |
| Program (V1 相当) | 2.568 G | 8.516 G | 3.29 | 2,733 | 9,063 |

- **比 (Store / Pointer)**:
  - cycles: median 1.150、mean 1.153、min / min 1.148
  - inst: 1.186
- **A/A (Pointer の 5 run の幅)**: cycles 1.93%、inst 0.34%。Store は cycles 3.08%、inst 0.06%。
- **program 側の費用**: Program / Pointer は cycles 0.994、inst 0.992。旧基準値 V1 (2.60G / 8.48G) とも合う。
- **adapter の method の呼び出し回数** (1 run):

| method | 回数 |
| --- | ---: |
| `GetEmitModuleFormatOfFile` | 77,070 |
| `GetModeForUsageLocation` | 22,810 |
| `GetResolvedModule` | 22,810 |
| `GetSourceFileForResolvedModule` | 22,810 |
| `GetEmitSyntaxForUsageLocation` | 22,716 |
| `GetImpliedNodeFormatForEmit` | 86 |

### 4.1 線を外れたものの切り分け

差は inst +1,696/node・cycles +415/node。設計 §2.7 の見積り (+470 inst/node) の 3.6 倍。

**確定 (単体の実測 × 回数):**

1. **測定用 adapter の費用 (計測側の偏り)**
   - 内容: 上の呼び出しを fixture の map で答える費用。node の鍵は `fmt.Sprintf` で作る。
   - 測り方: 単体ベンチ (`adaptercost/`) で、`GetResolvedModule` を除く呼び出しが 11.1 ms/run。除いた分を含めて約 14 ms と見積もる。
   - 割合: Store の区間 (約 930 ms) の 1.5%。
   - 全部差し引いても cycles の比は約 1.13 で、判定は変わらない。
   - Pointer 側の同じ問い合わせ (本物の program の method) の費用はこの見積りでは差し引いていないので、真の偏りはもっと小さい。

2. **links の map (切り分けの (4))**
   - 仕組み:
     - Store の `symbolNodeLinks` は鍵が `StoreID<<32 | ref` なので、`core.PagedLinkStore` の page 番号が `maxPageCount` (65,536) を超え、全部 `pageMap` (map) を引く。
     - Pointer は node id なので `pageList` (slice) を引く。
   - 規模:
     - check の後の `pageMap` は 3,620 page、`pageList` は 41。
     - check 1 回の参照は 3,304,029 回 (`count7d2c.txt`)。
   - 単価 (`linkbench/`、同じ page 数): map 10.3 ns、slice 2.9 ns、差 7.3 ns。
   - 見積り: 約 24 ms で、cycles の差 (約 120 ms) の 2 割前後。

**仮説 (未確定):**
- 残り約 80 ms は帰属していない。切り分けの (1) 2 word 渡しの境界と inline、(2) 辺の解決と nil 終了の親ループ、(3) V1 の分岐のどれかと見ている。
- 親ループの規模: `Parent()` は 24,650,495 回 (26.2/node)、nil を返したのは 547,441 回。
- 関数別の inst は取っていない。darwin の pprof は inline した leaf を過小帰属するので、xctrace か overlay の計数が要る。

**次の改善段の材料 (候補・見積り・受入条件):**

| 候補 | 見積り | 受入条件 |
| --- | --- | --- |
| A. `symbolNodeLinks` の鍵を (StoreID ごとの page 表 または file index) にして slice で引く | −24 ms 前後 (cycles 比 −0.03) | Check KPC の比が下がり、等価 (段 1・段 2) が不変 |
| B. 関数別 inst の取得 (xctrace の PMC か、Store 固有の経路の overlay 計数) | 残り 80 ms の帰属 | 帰属した項目ごとに単価 × 回数で差の 8 割以上を説明できる |
| C. 測定用 adapter の鍵を struct にする (`fmt.Sprintf` をやめる) | 計測の偏り −1.5% | adapter の単体費用が 1 ms 未満 |

C は storeharness の中で閉じる。B の結果を待たずに入れられる。

## 5. retained / GC (§5)

子プロセスを Pointer と Store で交互に、各 3 run。retained の値は 3 run で 0.1 MB 以内の差。

| 段 | Pointer HeapAlloc | Store HeapAlloc | 増分の比 | scan heap P / S | mark CPU P / S (ms) | mark CPU の比 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| baseline (fixture だけ) | 11.43 MB | 11.43 MB | | 1.4 / 1.4 MB | 0.5 / 0.5 | |
| parse + bind の後 | 133.75 | 84.49 | **0.597** | 113.4 / 21.4 MB | 70 / 13.1 | 0.187 |
| check の後 | 237.48 | 199.87 | **0.834** | 211.0 / 126.2 MB | 161 / 105 | **0.652** |

- **mark CPU** は gctrace の `assist + background + idle` の和。
  - idle を除いた background だけでは、check の後が 40 / 28 ms で比 0.70。
  - idle の mark は空いている P の数で増えるので、両方を並べた。
- **GC 回数** (GC を有効にした実行の全体): Pointer 11〜12、Store 13〜14。強制 GC 6 回を含む。
- **旧値 (stale)**: Pointer の parse +101.6 / bind +26.6 / check +106.4 MB (計 234.7)。今回は parse + bind +122.3・check +103.7 で、判定には今回の値を使う。7d-0 の parse + bind の比 0.567 に対して、今回は 0.597。

**内訳** (check の後、heap profile の標本からの推定で HeapAlloc とは一致しない、`retained-breakdown.txt`):

| 分類 | Pointer | Store |
| --- | ---: | ---: |
| AST (parse) | 139.1 (program 全体) | 47.9 |
| Bound | (program に含む) | 25.2 |
| links | 18.1 | 24.8 |
| 合成の Store | — | 3.2 |
| 遅延 JSDoc の Store | — | 1.0 |
| foreign 表 | — | 0 (名前の出る確保元なし) |
| checker のその他 | 85.6 | 90.3 |
| adapter / fixture | 10.7 | 8.7 |

## 6. 印字の全区間 (§6)

### 6.1 manifest と縮小

**manifest** (`TestWritePrintManifest`):
- 対象: compiler プロジェクトと 200 構成。
- 構成の選び方: 2b-3 の strict (`_store-checker-7d2b3-results/strict.jsonl`) で equal のものを、Pointer の診断 + global の多い順、同数は index 順。
- 要求: 1,159,486 件 (うち compiler 998,985)。各要求は (walk 対象・preorder の序数・pos・kind・操作 type / symbol・hash)。
- 診断の message: 18,230 件。

**出力の一致** (`TestPrintOutputs`): 全 target の Store の文字列 (miss と hit) と message が manifest (Pointer) と一致した。

**全量の KPC が完走しなかったこと:**
- 1 run 目の Pointer miss は 68 s・741 G inst。その次の Store の sub-benchmark は kill された (`kpc-print-full-killed.txt`)。
- 原因は下の 6.3 の確保。GC オフの session で、約 60 万件の symbol 要求 × 約 420 KB を、回収されないまま書き込んでいく。kill の直接の原因 (memory) の確認はしていない。
- 通常の GC の下では、全量も retained の実行で完走している。

**縮小:** KPC と ns は `STORE_CHECKER_PRINT_STRIDE=1000` (各 target の要求を先頭から 1,000 件おき) で取った。

**cache の状態:**
- miss: 新しい checker で印字する。区間の外で診断対象を check し、要求の型と symbol を解決しておく。
- hit: 同じ checker で、同じ列をもう一度印字する。

**操作の意味:**
- build: nodebuilder だけを通す。miss − build を printer と文字列の費用と読む。
- 診断の生成: 区間に check を含む。印字だけの費用ではない。

### 6.2 値

**KPC** (GC オフ、3 run の median、比は Store / Pointer、`kpc-print-summary.txt`):

| 集合 / 操作 | cycles P | cycles 比 | inst 比 | 幅 P / S |
| --- | ---: | ---: | ---: | --- |
| compiler / miss | 323.8 M | 1.105 | 1.075 | 2.6 / 1.4% |
| compiler / hit | 120.6 M | 1.151 | 1.114 | 0.8 / 2.9% |
| compiler / build | 318.2 M | 1.093 | 1.090 | 3.8 / 1.7% |
| compiler / 診断の生成 | 2,601 M | 1.124 | 1.180 | 18.3 / 18.6% |
| configurations / miss | 27.3 M | 1.319 | 1.684 | 10.2 / 8.8% |
| configurations / hit | 18.1 M | 1.044 | 1.266 | 16.4 / 7.0% |
| configurations / build | 22.5 M | 1.413 | 1.810 | 8.3 / 6.2% |
| configurations / 診断の生成 | 4,291 M | 1.145 | 1.193 | 8.0 / 6.3% |

**ns** (通常の GC、5 run、benchstat、`print-benchstat.txt`):

| 集合 / 操作 | sec/op | B/op (Store / Pointer) | allocs/op |
| --- | --- | --- | --- |
| compiler / miss | −28.5% | 233 / 697 MiB | +44.5% |
| compiler / hit | −34.7% | 215 / 688 MiB | +4.9% |
| compiler / build | ~ (p = 0.056) | 230 / 27 MiB | +44.6% |
| compiler / 診断の生成 | +10.5% | 262 / 200 MiB | +22.1% |
| configurations / miss | +116% | 103 / 5.9 MiB | +13.9% |
| configurations / hit | +45% | 18 / 3.9 MiB | +5.7% |
| configurations / build | +118% | 103 / 2.9 MiB | +10.0% |
| configurations / 診断の生成 | +14.9% | 5,541 / 244 MiB | +11.6% |

- compiler の miss / hit は、ns では Store が速く、KPC では遅い。Pointer は要求ごとに writer を file の長さまで Grow する (本番の `typeToString` も同じ) ので、通常の GC の下では GC の費用が Pointer に乗ると読む (仮説)。
- **内訳**: build を miss の内訳として読むと、compiler では nodebuilder が 9 割以上。configurations では build と miss がほぼ同じで、printer の分は小さい。

**印字の後の retained / GC** (別実行、compiler の全量、3 run):

| | Pointer | Store |
| --- | ---: | ---: |
| bound の後 | 204.7 MB | 155.5 MB |
| 印字の後 | 4,279 MB | 1,025.5 MB |
| mark CPU | 13.3〜15.1 s | 0.31〜0.40 s |

- Pointer の 4.1 GB の大半は、nodebuilder の node の arena の塊 (`slices.Grow` の node 構造体)。
- 「checker の cache が保持する少数の node が、arena の塊ごと生かしている」は仮説。profile は確保元しか示さない。

### 6.3 再現する退行 (未解決として残す)

**事実:**
- Store checker の `getNodeBuilder` の release (`storechecker/nodebuilder.go:135`) は、`typeToString` / `symbolToString` の 1 回ごとに `Factory.ReleaseArenas` を呼ぶ。
- そのため次の呼び出しは、容量 4,096 の新しい Store から始まる (`store.(*Factory).newStore`)。
- 1 Store は header 32 B × 4,097、extra 16 × 4,096 × 4 B、texts 8 × 4,096 B で、約 420 KB をゼロ埋めで確保する。使うのは数〜数十 node。

**根拠:**
- compiler / build の区間の B/op 240 MB は、alloc profile の `newStore` 234 MB とほぼ一致する。
- check の中でも同じ経路で、使用率 1〜53 / 4,096 の Store を 99 個作る (§7.1)。

**影響:**
- configurations / 診断の生成で、確保量が Pointer の 22.7 倍。
- 全量の印字 KPC が完走しない。

**修正の候補** (本段では行わない):
- Store の容量を使い方に合わせる (小さく始めて伸ばす、または node builder 用の容量を別に持つ)。
- `ReleaseArenas` の後も、空の active Store を再利用する。

## 7. 判断材料の計数 (§7、overlay、compiler プロジェクトの check)

### 7.1 合成の Store

- **checker の Factory**: 1 Store、使用率 1,023 / 4,096。
- **node builder の EmitContext の Factory**: 99 Store、各 1〜53 / 4,096。
  - check の後の heap には残らない (§5 の profile)。確保だけで約 42 MB。
- **links の外側の枠**: `symbolNodeLinks` の `pageMap` 3,620、`pageList` 41。ほかの node の links は `core.LinkStore` (map) で、Pointer と同じ形。
- 容量 4,096 の決め直しは、checker の Factory 単独なら 1,024 で足りる。ただし 6.3 の EmitContext の分は容量だけでなく確保の形 (`ReleaseArenas` の後の新規確保) の問題。

### 7.2 R4' のコピー (`Nodes()`)

| 呼び出し元 | 呼び出し | 確保 | 要素 | bytes |
| --- | ---: | ---: | ---: | ---: |
| `checker.go:29000` (`getEffectiveCallArguments` の既定) | 54,251 | 49,906 | 92,602 | 1,481,632 |
| `jsdoc.go:102` | 2,890 | 2,890 | 3,982 | 63,712 |
| `grammarchecks_manual.go:147` | 1,571 | 1,475 | 9,894 | 158,304 |
| `utilities.go:1057` | 1,562 | 1,441 | 6,459 | 103,344 |
| `ast/store/utilities.go:2110` | 1,428 | 1,428 | 1,938 | 31,008 |
| `checker.go:8277` (`resolveCall` の型引数) | 550 | 550 | 724 | 11,584 |
| `storeprinter/printer.go:4464` | 167 | 167 | 307 | 4,912 |
| `checker.go:28971` (spread) | 137 | 137 | 346 | 5,536 |
| `checker_manual.go:413` | 82 | 82 | 90 | 1,440 |
| `utilities.go:1059` | 7 | 7 | 358 | 5,728 |
| `jsx.go:810/814`・`flow.go:1210` | 0 | | | |
| 計 | 62,645 | 58,083 | 116,700 | 1,867,200 |

- **call expression あたり**:
  - call 系の node (call・new・tagged template・decorator・JSX 開始) は 53,068 個。
  - `resolveCall` 53,123 回、`getEffectiveCallArguments` 54,396 回。
  - call あたりのコピーは約 1.02 回・28 B。
- **結果が `resolveCall` の外へ出る経路 (コードの読み)**:
  - 呼び出し元は 4 箇所: `resolveCall` の `CallState.args`、`getContextuallyTypedParameterType`、`getContextualTypeForArgument`、`utilities.go:296`。
  - どれも slice を局所変数か `CallState` (stack 上の値、`chooseOverload` などへ pointer で渡すが保存しない) に置くだけ。
  - struct の field・links・cache への代入は無い。要素の Node は値として保存されうるが、slice 自体は出ない。
  - したがって、案 B / E (コピーをやめる) の安全性の障害は見当たらない。
  - 費用は check あたり 1.87 MB・5.8 万確保で、Check の inst に対しては小さい (未測定)。

### 7.3 Store 固有の経路の頻度

- `Parent()`: 24,650,495 回 (26.2/node)、nil 終了 547,441 回。nil 終了の親ループの反復数の上限として読む。
- 2 word 渡しの境界の動的回数: 取っていない。§4.1 の B (関数別 inst) で取る。

## 8. 試験と回帰

**テストとして残したもの:**
- §2: `TestCompilerProjectEquivalence`
- §3.3: `TestFixtureIdentity*`
- §6: `TestPrintOutputs`。fixture と manifest を env で受けるので、その env の無い通常の `go test` では skip する。

**回帰** (`regress.sh`、`regress.log`、HEAD 9c93098cc3、差分 hash `f4d6fd97933c`):

| 項目 | 結果 |
| --- | --- |
| `go build ./...` | 合格 |
| `go vet` (通常 / `storechecks`) | 合格 |
| 生成器 2 つ | 前後で `git status` と diff の sha1 が不変 |
| 段 1 strict `TestCheckerCorpus` | 比較 17,413 / equal 17,413 |
| 段 2 strict `TestCheckerConfigurations` | 合格、mismatch 0 |
| `TestModeCorpus` | 17,415 file、4,996 位置、不一致 0 |
| package テスト (通常 / `storechecks`) | 既知の `FuzzEquivalence/a708303f611b95c8` (storeparser) だけが失敗。ほかは全部合格。storeharness には本段の試験も入る |

段 1 の比較数は、2b-3 の 17,405 / 17,406 から 17,413 に変わった。原因は調べていない。

## 9. 指示書の外で判断したこと

1. fixture は「問い合わせを記録して再生する」形にした。全 method の全答えを前もって列挙はしない。記録に無い問い合わせは panic する。
2. `ResolutionDiagnostics` を落とした。Store のどの package も読まない。
3. symlink cache と package.json の項目は、値に落として測定側で組み直した。
4. 印字の要求列の構成の選び方に、uncommitted の 2b-3 の出力 (`strict.jsonl`) を使った。
5. 印字の KPC と ns は 1/1000 の間引きで取った (ユーザーと合意、2026-10-02)。
6. symbol の操作の文字列は `SymbolToStringEx` だけ。walker の `Decl(...)` の部分は印字ではないので除いた。
