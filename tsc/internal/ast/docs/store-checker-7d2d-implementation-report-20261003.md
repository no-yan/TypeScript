# 7e の前段 (TODO 7d-2d) 実装報告

作成日: 2026-10-03。[実装指示書](store-checker-7d2d-implementation-instructions-20261003.md) §6 に従う。生出力は `_store-checker-7d2d/` (commit しない)。

## 判定

| 記号 | 内容 | 結果 |
| --- | --- | --- |
| C | fixture の byte を揃える | **合格**。5 回書いて sha256 が一致、意味は不変、同一性の試験は通る。`37551fe764` |
| B | 同一性の試験に合成 specifier の照合 | **合格**。故障 3 で 139 構成すべて不一致、誤検出 0。`9a0f025bc4` |
| A | 印字の Store を 16 行から倍々 | **止まる条件に当たらない**。Check cycles 比 1.0545 → 1.0525、retained 0.820・mark CPU 0.622、印字の Store の cycles は 8 操作すべて減少。`927e6c7497` |
| D | Check の KPC の A/A ≤ 2% | **合格**。Pointer の cycles の幅 before 0.78%・after 0.35% (2 本合わせて 1.03%)。開始条件はユーザーの決定で load < 1.5 |

## 0. 開始時の状態

- HEAD `f2bbf0bde7` (指示書の commit)。未コミットの差分は無し (`git diff HEAD` の sha256 `e3b0c442…`、`start.txt`)。
- 開始時の HEAD で `TestWriteCompilerProjectFixture` を回した fixture は `42e2cf35…` で、7d-2c 検証の fixture と一致した。
- 作業中、`mediaanalysisd` (macOS の写真解析) が CPU を 100〜180% 使い、load は 4〜48 だった。計測の値に関わるのは KPC だけで、KPC は §4 の開始条件で待たせた。

## 1. 変更と commit

commit の順は指示書 §7 の 3 のとおり C → B → A。

| commit | file | 内容 |
| --- | --- | --- |
| `37551fe764` (C) | `storeharness/fixture.go` | `Recorder.Fixture()` が `sortModules` を呼ぶ。`Modules` を値の順に並べ、`Resolved`・`FromSpecifier`・`r.modules` の添字を付け替える |
| `9a0f025bc4` (B) | `storeharness/fixture_test.go` | `fixtureIdentity` が、記録が equal のとき `compareSyntheticImports` を呼ぶ |
| `927e6c7497` (A) | `ast/store/factory.go` | `NewGrowingFactory(region, min, max, hooks)` を足す。`NewFactory` は min = max の場合 |
| | `storeprinter/region.go` | 印字の Factory を `NewGrowingFactory(region, 16, 4096, …)` にする |
| | `ast/store/storage_test.go`・`export_test.go` | 試験 18 (`TestStorageGrowingFactory`) と、Store の行の容量を読む `NodeCapacity` |

### 1.1 C: `sortModules` (`fixture.go:326`)

- 並べる鍵は module の JSON (`json.Marshal(fixtureModule)`)。field の順が struct の順なので、`ResolvedFileName` が先に効く。field を手で並べないので、field が増えても比べ漏れない。
- 同じ値の module が別の pointer で 2 つあることがある (compiler プロジェクトで 12 件)。その順は、その module を指す鍵のうち最小のもの (`"Resolved " + 鍵` か `"FromSpecifier " + 鍵`) で決める。
- `r.modules` (pointer → 添字) も付け替えるので、`Fixture()` の後に記録が続いても添字は正しい。
- JSON の形と `FixtureProgram` の読み方は変えていない。

### 1.2 B: `compareSyntheticImports` (`fixture_test.go:83`)

- 対象は H1 の Program の全 file の 2 役割 (import helpers・JSX runtime)。`compareModes` と同じ 4 点を見る。
  - 有無が同じ。class `mode: synthetic import`。
  - 同じ path と役割でもう一度引くと同じ Node (両側とも)。class `mode: synthetic import identity`。
  - 形: StringLiteral、親が ImportDeclaration、その親がその program の file の root、`File()` がその file。両側とも自分の program の file で確かめる。class `mode: synthetic import shape`。
  - text が同じ (同じ class)。
- 引き方:
  - H1 は method で引く (`GetImportHelpersImportSpecifier`・`GetJSXRuntimeImportSpecifier`)。
  - FixtureProgram の helpers は method で引く (記録を引かないので panic しない)。
  - FixtureProgram の JSX は、記録 (`JSXReference`) にある path なら method、無い path なら `jsxImports` の map を直接読む。
- `Detail` は、H1 の側を `Pointer`、FixtureProgram の側を `Store` に入れる (`compareRecords(recorded, replayed)` と同じ並び)。

### 1.3 A: `NewGrowingFactory` (`factory.go:101`)

- `Factory` に `minCapacity`・`maxCapacity` を足した。`capacity` は「次の Store の行数」。
  - `setCapacity(rows)` が `capacity`・`extraCap = 16 × rows`・`textsCap = 8 × rows` を一度に決める。
  - `row()` (`factory.go:213`) は、active の Store が満杯のときだけ (今までどおり `len(s.nodes) < cap(s.nodes)` の判定の外で)、`capacity < maxCapacity` なら 2 倍にしてから新しい Store を作る。fast path は変わらない。
  - `ReleaseArenas()` (`factory.go:121`) は active を捨て、`capacity != minCapacity` のときだけ `minCapacity` に戻す。
- `NewFactory(r, capacity, hooks)` は `NewGrowingFactory(r, capacity, capacity, hooks)`。min = max なので伸びず、release で戻す値も同じ。
  - checker の Factory (`storechecker/region.go`、4,096) の動きは変わらない (§4.2 の計数で 1 Store・1,023 行・foreign 1,051 が before と同じ)。
  - `SetInitialCaps` (試験だけが使う) で決めた列の容量は、固定の Factory では release でも上書きされない。
- 変えていないもの: Store の行を動かさない (不変条件 15)、合成の Store の不変条件 (17・18・26)、hook の呼び方。

### 1.4 A の試験 (`storage_test.go:1083`、試験 18)

印字の Factory と同じ構築 (`NewGrowingFactory(region, 16, 4096, {})`) で、次を確かめる。両 tag で通る。

- 容量: 16・32・64・…・4,096・4,096 と伸び、それぞれの Store の node・extra・texts の容量が 16 × / 8 × に比例する。満杯でない 4,096 の Store から release すると、次は 16 に戻る。
- ノードの置き場所: 各 Store の node は、その Store だけに入る。release の後の node は新しい Store に入り、release の前の node (最初の Store と、満杯でない 4,096 の Store) は読めて書ける。
- foreign の辺: 満杯の 16 の Store の最後の node を子に持つ親は 32 の Store に入り、子の辺が foreign entry として解ける。`SetParent` で古い Store に書いた親の辺も解ける。release の前の node を子に持つ release の後の node も解ける。最後に、作った全 node の text が読める。

試験の感度 (`a-test-sensitivity.txt`、overlay で factory.go に故障を入れる):

| 故障 | 結果 |
| --- | --- |
| release で容量を戻さない | 失敗 (release の後の Store が 4,096) |
| 満杯で 2 倍にしない | 失敗 (2 つ目の Store が 16) |
| 列の容量を比例させない | 失敗 (extra の容量が 16 × 16 でない) |

## 2. C の受入 (§3.3)

| 項目 | 結果 | file |
| --- | --- | --- |
| 決定性 | C の source の 1 binary (`4a4d6e7a…`) で `TestWriteCompilerProjectFixture` を 5 回 (別プロセス)。5 本とも sha256 `36565f8e…` | `c-write5.txt` |
| 意味 | 開始時の fixture (`42e2cf35…`) と、module の添字を値に解いて全 field が同じ (検証の `fixture-semantic-diff.py`) | `c-write5.txt` 末尾 |
| 同一性 | compiler プロジェクト: 5 回の書き出しがどれも `fixtureIdentity` equal (書き出しの試験は equal でないと失敗する)、`TestFixtureIdentityCompilerProject` も equal。段 2: `TestFixtureIdentityConfigurations` 15,205 構成で equal 13,446・not-run 1,759・不一致 0 | `b-identity-*.txt` |

- 5 回の一致だけでは「問う順がたまたま揺れなかった」と区別できない。そこで、開始時の fixture (問うた順) に同じ規則を Python で当てると C の出力と一致すること、さらに添字をランダムに付け替えた 20 通りからも一致することを確かめた (`c-resort.py`・`c-resort.txt`)。並びは値と鍵だけで決まる (確定)。
- 段 2 の同一性は C+B の binary (`658d1383…`) で取った。B は不一致の条件を足すだけなので、C だけの tree でも通る。
- 開始時の fixture の並びと C の並びは違う (`before order == C order: False`)。

## 3. B の受入 (§2.3)

| 項目 | 結果 | file |
| --- | --- | --- |
| 感度 | 検証の overlay `mut3` を当てて、検証の sample 1,130 構成に回した。不一致 139・equal 991。不一致の 139 構成は、2b-3 の strict で合成 import を持つ 139 構成と一致した。内訳: Store の panic (記録の差、従来どおり) 62、`mode: synthetic import` (新しい照合) 77 | `b-mut3-sample.txt`・`b-mut3-check.txt` |
| 誤検出なし | `TestFixtureIdentityCompilerProject` equal。`TestFixtureIdentityConfigurations` 全 15,205 構成で不一致 0 (h1-panics・harness-error も 0) | `b-identity-compiler.txt`・`b-identity-configurations.txt` |

- overlay は検証の `gen.py` を、C の後の tree で作り直した (`mut3` の hash `58f48d27…`、当てる file は `fixture.go` の 1 つ)。
- commit した差分 (`f2bbf0bde7..9a0f025bc4`) は、受入の binary を作ったときの差分と sha256 が一致する (`4d1f5705…`)。C の commit の差分も 5 回書きの binary の差分と一致する (`be8ecbf8…`、`c-commit-diff.sha256`)。

## 4. A の計測 (§1.3)

### 4.1 binary

- before は B の commit (`9a0f025bc4`)、after は A を入れた tree。どちらも repo の tree から build した (`build.sh`)。before は、A の 2 file (`factory.go`・`region.go`) を HEAD の版にする overlay で作った (`overlay/gen.py`)。
- before の通常の binary の sha256 は `658d1383…` で、B の受入で overlay なしに C+B の tree から作った binary と byte 一致した。overlay の before は B の commit そのもの (確定)。
- KPC の binary: before `384e346e…`、after `66ed3b7d…` (`kpc-binary.sha256`)。fixture `36565f8e…` (C の書き出し)、印字の manifest `67273879…` (7d-2c と同じ sha256)、構成の fixture 200 本は C で書き直した (`kpc-inputs.sha256`・`print-fixtures.sha256`)。

### 4.2 合成の Store の数と使用率 (compiler プロジェクトの check、`checkStore` 1 回)

計数の overlay (`overlay/store_zz_count.go` ほか) で、`newStore` ごとに Factory と Store を記録し、check の後に数えた (`m-count-*.txt`)。

| | before | after |
| --- | --- | --- |
| checker の Factory | 1 Store、4,096 行、使用 1,023、foreign node 1,051 | 同じ |
| node builder の EmitContext の Factory (1 つ) | 99 Store、全部 4,096 行。使用 最小 1・中央 8・最大 53、計 953。foreign 0 | 112 Store: 16 行 99・32 行 11・64 行 2。使用 最小 1・中央 8・最大 32、計 953。foreign node 35・list 2 (13 Store) |
| 印字の Store の列の bytes (check の後の容量) | 42,175,584 (約 40.2 MiB) | 218,992 (約 214 KiB) |

- 使用率: after の 16 行の Store は 0.476、32 行は 0.537、64 行は 0.078 (before は 0.002)。
- before の印字の Store は、どれも 1 回の印字で使い終わっていた (最大 53 行、foreign 0)。after では、満杯の Store の後にできる 32 行の Store が 11 個、64 行が 2 個あり、foreign の辺 (node 35・list 2、計 37 本) を持つ Store が 13 個ある。辺の数は使う行数 (953) の 4%。
- 列の bytes は、header 32 B × 容量 + extra 4 B × 容量 + texts の容量 + foreign 表 16 B × 容量で数えた。

### 4.3 印字の ns・B/op・allocs/op (`BenchmarkPrint`、1/1000、5 run、benchstat)

before と after を run ごとに交互に回した (`measure.sh`)。benchstat は `print-benchstat.txt`。

| 集合 / 操作 | Store sec/op (before → after) | Store B/op (before → after) | Pointer B/op | Store allocs/op |
| --- | --- | --- | --- | --- |
| compiler / miss | 127.8 → 109.0 ms (−14.8%) | 233.4 → 46.6 MiB (−80.0%) | 696.8 MiB | +0.02% |
| compiler / hit | 56.5 → 39.7 ms (−29.7%) | 214.7 → 27.9 MiB (−87.0%) | 687.5 MiB | ~ |
| compiler / build | 129.4 → 104.8 ms (−19.1%) | 229.6 → 42.8 MiB (−81.4%) | 27.3 MiB | +0.02% |
| compiler / 診断の生成 | 875.8 → 870.2 ms (~) | 255.6 → 214.8 MiB (−16.0%) | 199.8 MiB | +0.01% |
| configurations / miss | 8.08 → 5.34 ms (−34.0%) | 103.2 → 3.6 MiB (−96.5%) | 5.8 MiB | +1.68% |
| configurations / hit | 2.31 → 1.57 ms (−32.2%) | 18.4 → 1.4 MiB (−92.5%) | 3.8 MiB | +1.84% |
| configurations / build | 6.74 → 3.77 ms (−44.0%) | 103.0 → 3.4 MiB (−96.7%) | 2.9 MiB | +1.98% |
| configurations / 診断の生成 | 1.540 → 1.353 s (−12.2%) | 5,509 → 206.8 MiB (−96.3%) | 177.2 MiB | +0.13% |

- Pointer 側は before と after で有意差なし (同じ code)。
- allocs/op が増えるのは、Store の数が増えるため (1 Store あたりの確保は Store・3 列・私有の表など数個)。
- after でも、build の B/op は Store が Pointer より多い (compiler 42.8 対 27.3 MiB、configurations 3.4 対 2.9 MiB)。compiler の 1 要求あたり約 16 KB の差。最小 16 行の Store (約 1.7 KB) と私有の表、伸びた要求の 32・64 行の Store の分と見ている (仮説、内訳は取っていない)。
- ns は load 2〜3 の下で取った (`measure.log`)。比べるのは before と after の交互の run で、絶対値は参考。

### 4.4 印字の KPC (`BenchmarkPrintKPC`、1/1000、3 run)

`kpc.sh` の後半 (`kpc.txt`、集計は `kpc-sum.py` → `kpc-summary.txt`)。3 run、run ごとに binary の順を入れ替えた。Pointer の値は 2 本の binary の 6 標本の median。

| 集合 / 操作 | Pointer | Store before | Store after | after − before | after / P | before / P | 幅 P / before / after |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| compiler / miss | 323.2 M | 352.2 M | 343.6 M | −8.6 M (−2.4%) | 1.063 | 1.090 | 2.2 / 0.8 / 0.6% |
| compiler / hit | 121.4 M | 135.7 M | 125.3 M | −10.4 M (−7.7%) | 1.032 | 1.118 | 1.1 / 0.2 / 1.4% |
| compiler / build | 309.1 M | 340.4 M | 331.5 M | −8.9 M (−2.6%) | 1.072 | 1.101 | 2.8 / 2.8 / 1.2% |
| compiler / 診断の生成 | 2,569.5 M | 2,679.9 M | 2,673.4 M | −6.5 M (−0.2%) | 1.040 | 1.043 | 4.1 / 0.4 / 0.2% |
| configurations / miss | 25.1 M | 33.9 M | 25.4 M | −8.5 M (−25.1%) | 1.014 | 1.353 | 2.1 / 1.9 / 1.7% |
| configurations / hit | 17.2 M | 18.3 M | 17.2 M | −1.2 M (−6.3%) | 1.001 | 1.068 | 1.8 / 1.7 / 3.2% |
| configurations / build | 21.3 M | 30.1 M | 21.5 M | −8.6 M (−28.6%) | 1.012 | 1.417 | 2.1 / 2.6 / 1.3% |
| configurations / 診断の生成 | 4,269.3 M | 4,751.4 M | 4,287.6 M | −463.9 M (−9.8%) | 1.004 | 1.113 | 0.5 / 0.2 / 0.3% |

(cycles。inst も全操作で減った: compiler の 4 操作で −15〜−26 M、configurations / 診断の生成で −1,945 M。)

- 7d-2c の印字の退行 (configurations / build 1.417) は 1.012 になった。configurations の 4 操作は Pointer とほぼ同じ (1.001〜1.014)。
- compiler の miss・build は after でも Pointer より 6〜7% 多い。印字の Store の確保ではない残り (accessor や adapter など 7d-2c で見た仕組み) と見ている (仮説)。

### 4.5 Check の KPC (§4 の D の手順、5 run)

`kpc.sh` の前半 (`kpc.txt`・`kpc-summary.txt`)。13:39:46 開始 (1 分 load 1.39)、Check の 24 回は 13:40:13 まで。

| | cycles (median) | inst (median) | IPC | cycles の幅 | inst の幅 |
| --- | ---: | ---: | ---: | ---: | ---: |
| before Pointer | 2,607.3 M | 8,595.0 M | 3.296 | 0.78% | 0.21% |
| before Store | 2,749.6 M | 9,233.9 M | 3.358 | 0.30% | 0.14% |
| after Pointer | 2,598.2 M | 8,589.2 M | 3.306 | 0.35% | 0.11% |
| after Store | 2,734.7 M | 9,193.3 M | 3.362 | 0.29% | 0.10% |

| 比 | cycles | inst |
| --- | ---: | ---: |
| Store / Pointer (before) | 1.0545 | 1.0743 |
| Store / Pointer (after) | **1.0525** | 1.0703 |
| after / before (Store) | 0.9946 (−14.9 M) | 0.9956 (−40.6 M) |
| after / before (Pointer、同じ code) | 0.9965 (−9.2 M) | 0.9993 (−5.7 M) |

- 2 本の binary の Pointer の inst の差は −0.07% (±0.3% の内側)。path の非対称 (7d-2c の残件 1) は無い。
- Store の inst は 40.6 M 減った。Pointer の揺れ (−5.7 M) を引いても約 35 M。before の check の中の 99 回の 420 KB の Store のゼロ埋め (約 40 MiB) が無くなった分と見ている (仮説、関数別には取っていない)。
- 7d-2c の検証 (KPC 1) の after の値 (Pointer 2,602.1 M・Store 2,767.2 M、比 1.0635) と比べ、今回の before は Store が 0.6% 低い (2,749.6 M)。C の fixture の並べ替え (module の表の順) と、開始の条件の違いのどちらが効いたかは分けていない。

### 4.6 retained と GC (`TestRetained`、各 binary 3 run)

集計は検証の `retained-sum.py` を、最後の段を引数で選べるよう写したもの (`retained-sum.py`・`retained-summary.txt`)。

**check の後**

| | before | after | 線 |
| --- | ---: | ---: | --- |
| retained 増分比 (Store / Pointer) | 0.833 | **0.820** | 0.85 |
| 絶対値の比 | 0.841 | 0.829 | |
| Store の check の増分 (bound から) | +115.35 MB | +112.43 MB | |
| mark CPU 比 (最後の forced の gctrace) | 0.640 | **0.622** | 0.75 |
| Store の mark CPU | 103 ms | 115 ms | |
| Pointer の mark CPU | 161 ms | 185 ms | |

- Store の retained は 2.9 MB 減った。before で check の後まで残っていた印字の Store (4,096 行) の分と見ている (仮説)。
- mark CPU は両側とも after の run で高い (Pointer は同じ code で 161 → 185 ms)。after の retained の run は load 6〜7 の下だった (`measure.log` 11:49)。比は両側の同じ run で取るので、この揺れは比に半ば打ち消される。

**印字の後** (`STORE_CHECKER_RETAINED_WORK=print`、compiler の全要求を 1 回)

| | before | after |
| --- | ---: | ---: |
| retained 増分 Pointer / Store | 4,196.4 / 943.2 MB | 4,196.4 / 940.6 MB |
| 増分比 | 0.225 | 0.224 |
| mark CPU Pointer / Store | 11,970 / 389 ms | 12,790 / 308 ms |
| mark CPU 比 | 0.032 | 0.024 |

- 印字の後の Store の retained は 2.5 MB しか減らない。印字の Store の大半は release の後に回収されていて、残る Store は少ない (before でも)。
- この実行は GC あり (通常の GC) なので、全量の印字でも両方とも完走した (1 binary あたり約 17 分)。

### 4.7 出力の一致

`TestPrintOutputs` (間引きなし、201 target) は after で合格 (`m-print-outputs.txt`)。

### 4.8 印字の全量の KPC の完走 (Store 側、`STORE_CHECKER_PRINT_STRIDE=1`)

`kpc-full.sh` (`kpc-full.txt`)。after から始めた。

- after: compiler / miss は完走した (60.0 s、172.4 G cycles、746.8 G inst、**B/op 31.3 GB**、2.53 億 allocs)。次の compiler / hit の途中で、プロセスの footprint が 28 GB (うち圧縮 27 GB)、swap が 13.3 GB 中 12.6 GB に達し、ユーザーが止めた (13:58:31 の起動から 8 分 21 秒の時点ではまだ動いていて、その後に `sudo kill`)。
- before: 走らせていない。

**完走しない理由** (確定と仮説を分ける):
- 確定: KPC の session は GC を止める (`kperf.Start` の `debug.SetGCPercent(-1)`)。全量の 1 操作の確保は、after でも 31.3 GB ある (1 要求あたり約 31 KB)。8 GB 機に積もりきらない。
- 確定: 同じ全量の印字を通常の GC で回した `TestRetained` (§4.6) では、2 回 GC した後の live heap が Store 1,023 MB で、3 run とも ±0.2 MB に揃う。確保の大半は回収できるごみで、保持 (リーク) ではない。
- 見積り: 1 要求あたり約 31 KB のうち、印字の Store の分 (最小 16 行で約 1.7 KB) は 1 割に満たない。残りは node builder と printer の確保 (Pointer の compiler / build も 1/1000 で 27.3 MiB、1 要求あたり約 28 KB)。全量の Store 側は、A の有無によらず GC オフでは完走しない。before は 1 要求ごとにさらに 420 KB が加わる。
- したがって、全量の印字 KPC の完走は A の判定材料にならない。before は結果が明らかなので省いた (§8 の 9、こちらの判断)。

**スクリプトの不備** (2 つとも確定):
- 見張りは `ps -o rss` で 5 GB を見ていたが、macOS の RSS は圧縮メモリを含まない。footprint 28 GB のとき RSS は 674 MB で、見張りは働かなかった。footprint (`top -stats mem`) を見るべきだった。
- binary を見張りのために `&` で起動したので、非対話の shell の背景のプロセスとして SIGINT を無視した。ターミナルの Ctrl-C ではスクリプトだけが止まり、binary は `sudo kill` で止めた。

### 4.9 止まる条件 (§1.3)

| 条件 | 線 | after | 判定 |
| --- | --- | --- | --- |
| Check の cycles 比 | ≤ 1.10 | 1.0525 | 当たらない |
| check の後の retained 増分比 | ≤ 0.85 | 0.820 | 当たらない |
| check の後の mark CPU 比 | ≤ 0.75 | 0.622 | 当たらない |
| 印字の Store の cycles が before より A/A の幅を超えて増える | 幅 (before の Store・Pointer の幅) | 8 操作すべて減少 | 当たらない |

指示書 §7 の 2 に従い、A を commit した (`927e6c7497`)。

## 5. D: Check の KPC の手順

`kpc.sh` は §4.2 の手順で書いた。

- **開始の条件**: 指示書の「1 分 load < 1.0」は、この機械では届かなかった (12:05〜12:42 の 37 分の待ちで最小 2.08。`mediaanalysisd` が CPU を 90〜180% 使っていた)。ユーザーの決定 (2026-10-03) で **load < 1.5** に緩めた。作業中の印 (`busy.flag`) が無いことは条件のまま。13:39:46 に load 1.39 で始まった。
- **捨てる run**: before・after の Pointer・Store を 1 回ずつ。値は `kpc-summary.txt` の先頭に残した。

| | 捨てる run | 本番 5 run の median | 差 |
| --- | ---: | ---: | ---: |
| before Pointer | 2,632.3 M | 2,607.3 M | +1.0% |
| before Store | 2,818.0 M | 2,749.6 M | +2.5% |
| after Pointer | 2,631.5 M | 2,598.2 M | +1.3% |
| after Store | 2,737.1 M | 2,734.7 M | +0.1% |

- **順の入れ替え**: 奇数の run は before → after、偶数の run は after → before。
- **結果**: Pointer の cycles の幅は before 0.78%、after 0.35%、2 本の 10 標本で 1.03%。**受入 (binary ごとに ≤ 2%) に合格**、取り直しは無い。
- 捨てる run の 4 本のうち 3 本が本番より 1〜2.5% 高い。7d-2c の KPC 1 で run 1・2 がそろって 2.5〜3% 高かったのと同じ向きで、捨てる run がそれを吸った (1 回だけの観察)。

## 6. 等価と回帰 (§5)

A を入れた tree (HEAD `9a0f025bc4` + A、差分 `c858deb43bff`) で `regress.sh` を回した (`regress.log`・`regress-*.txt`)。commit した A の差分は、計測と回帰の tree の差分と同じ (`c858deb43bff`)。

| 段 | 結果 |
| --- | --- |
| `go build ./...`・`go vet ./internal/ast/... ./internal/store...` (両 tag) | 通過 |
| 生成器 2 つの再実行 (`generate.ts`・`storetranslate`) | 生成物は不変 (status・diff の hash が前後で同じ) |
| 段 1 strict `TestCheckerCorpus` | PASS (234 s) |
| 段 2 strict `TestCheckerConfigurations` | PASS (174 s) |
| `TestCompilerProjectEquivalence` | PASS (235 s) |
| `TestModeCorpus` | PASS (82 s) |
| package の試験 (通常の tag、`./internal/ast/store/... ./internal/store...`) | 既知の `FuzzEquivalence/a708303f611b95c8` 以外は ok (`ast/store`・`storebinder`・`storechecker`・`storeharness`・`storeprinter`) |
| package の試験 (`storechecks`) | `ast/store`・`storechecker`・`storeprinter` ok (`regress-packages-storechecks-3.txt`)。下の範囲の変更を参照 |
| `TestPrintOutputs` (間引きなし、201 target) | 合格 (§4.7、`m-print-outputs.txt`) |

**範囲の変更** (2026-10-03、ユーザーの決定): `storechecks` の package の試験は、全 package ではなく、A が触った・直接受ける 3 package (`ast/store`・`storechecker`・`storeprinter`) にした。

- `storechecks` の全 package の段は、`ast/store`・`storebinder`・`storechecker` が ok になった後、`storeharness` (前回 947 s) の途中で止めた。
- 理由: `storeharness` の時間の大半は同一性の試験 2 本 (B の受入で回したもの) で、検証 (検証指示書 §5) でも全部の回帰をもう一度回すため。
- `storechecks` の `storeharness`・`storeparser` は、この段では回していない (`storebinder` は止める前に ok)。

## 7. 仮説と確定

**確定**

- C: 並びは値と鍵だけで決まり、5 回の書き出しが byte 一致する。開始時の fixture と意味は同じ。
- B: 故障 3 を合成 import を持つ全 139 構成で検出し、全量で誤検出は無い。
- A: 印字の Store は 16 行から倍々になり、check の中の印字の Store の列は 40.2 MiB から 214 KiB になった。checker の Factory は before と同じ (1 Store、1,023 行)。
- A: Check の比は 1.0545 → 1.0525、Store の inst は −40.6 M。印字の Store の cycles は全 8 操作で減り、configurations の 4 操作は Pointer とほぼ同じになった。
- 全量の印字 KPC が完走しないのは、GC オフの session で 1 操作 31 GB の確保が積もるため。通常の GC の下の live heap は 1 GB で一定で、リークではない。
- D: load < 1.5・捨てる run・順の入れ替えで、Pointer の幅は 2% の内側だった。

**仮説** (確かめていない)

- Store の inst の −35 M (Pointer の揺れを引いた分) は、420 KB の Store のゼロ埋めが無くなった分。
- after でも build の B/op が Pointer より多い分 (compiler で 1 要求あたり約 16 KB) は、最小 16 行の Store・私有の表・伸びた要求の 32・64 行の Store。
- compiler の miss・build の cycles 比 (1.06〜1.07) の残りは、印字の Store の確保ではなく 7d-2c で見た accessor などの仕組み。
- 捨てる run が本番より高いのは、計測の開始直後の環境 (cache・周波数・直前の作業の残り) による。

## 8. 指示書の外で判断したこと

1. **C の同順位**: 値の同じ module が別の pointer で 12 件あった。指示書の「値の順」だけでは順が決まらないので、その module を指す最小の鍵で決めた。
2. **C の値の比べ方**: field を手で並べず、module の JSON を鍵にした (field の追加で比べ漏れない)。
3. **C の受入の段 2**: C だけの binary ではなく C+B の binary で回した (B は不一致の条件を足すだけ)。compiler プロジェクトの同一性は C だけの binary の 5 回の書き出しで確かめた。
4. **B の JSX の引き方**: 記録にある path は method で、無い path は map で読む。method で引けるところは method にして、「2 回引いて同じ Node」の検査が adapter の method を通るようにした。
5. **A の `NewFactory`**: 別の構築関数を足し、`NewFactory` をその min = max の場合にした (固定の Factory と伸びる Factory で `row` と `ReleaseArenas` を 1 つにする)。release で容量を戻すのは `capacity != minCapacity` のときだけにして、固定の Factory の `SetInitialCaps` を保った。
6. **A の計数**: 指示書の「7d-2c の計数と同じ観点」を、自前の小さな overlay (`overlay/`) で取った。7d-2c の `count` overlay は links の記録なども含み、`factory.go` の差し込み位置だけが共通だった。
7. **全量の印字 KPC の見張り**: RSS 5 GB で止める watchdog を `kpc-full.sh` に入れた (8 GB 機がページングで止まるのを避けるため)。RSS が圧縮メモリを含まないので働かず、ユーザーが止めた (§4.8)。
8. **fixture の symlink の集合の順** (触っていない): `fixtureSymlinks` の `DirectoriesByRealpath`・`FilesByRealpath` の値は `SyncSet` の range の順で、run ごとに揺れうる。compiler プロジェクトの symlink 表は空なので C の受入には効かない。構成の fixture は byte を比べないので、直していない。
9. **全量の印字 KPC の before を省いた**: after が GC オフの確保で 1 操作 31 GB に達したので、1 要求ごとに 420 KB が加わる before も完走しないのは明らか。機械を swap で止める危険のほうが大きいので回していない。
