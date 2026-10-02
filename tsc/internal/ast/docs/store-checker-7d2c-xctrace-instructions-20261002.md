# Store checker の Check の差の帰属 (xctrace) 指示書

作成日: 2026-10-02。対象は計測を担当するエージェント。

[7d-2c 実装報告](store-checker-7d2c-implementation-report-20261002.md) §4.1 の候補 B。**本段は測定と帰属で、性能の改善は行わない。**

Check KPC の差 (cycles 約 120 ms、inst 約 1.59 G) をどの関数の何に帰属させられるかを、xctrace の PMC 標本で関数別に分ける。結果は次の改善段 (候補 A の links の鍵、ほか) の優先度を決める材料にする。

| 前提 | 文書 |
| --- | --- |
| 差の値、確定した 2 項目 (adapter 約 14 ms、links の map 約 24 ms)、`Parent()` の回数 | 7d-2c 実装報告 §4・§4.1・§7.3 |
| 切り分けの順 (1) 2 word 渡しの境界と inline (2) 辺の解決と nil 終了の親ループ (3) V1 の分岐 (4) links と map | [7d 設計](store-checker-design-20260923.md) §2.7 |
| 測定用 adapter と fixture | `internal/storeharness/fixture.go`、7d-2c 実装報告 §3 |
| 前回の帰属 (b719): 共通関数は横ばい、差は accessor・map・書き換えた関数 | memory の vscode-trace-common-vs-store-only-split に基づく。下の §5 の分類はこれに合わせる |

before は 7d-2c の作業ツリー (HEAD 9c93098cc3 + 未コミットの 7d-2c)。開始時の HEAD と差分の hash を記録する。

## 0. 規則

- **変更してよいもの**: storeharness (xctrace 用の driver を test file に足す)、計測用の新しい test file、docs、scratch の overlay とスクリプト。
- **変更しないもの**: storechecker・storeprinter・store の production、生成器と生成物、Pointer の package、go.mod / go.sum。
- **帰属の根拠**:
  - 帰属の根拠は xctrace の PMC (inst と cycles) と、overlay の計数 × 単体の単価だけ。
  - darwin の pprof の % は根拠にしない (inline した leaf を 10 倍前後過小に帰属する)。
- **仮説と確定を分ける**:
  - 確定: 標本か計数 × 単価で数字が出たもの。
  - 仮説: 読みや類推だけのもの。
- **置き場所**:
  - 一時 binary・trace は scratchpad に置く。trace は数 GB になりうる。repo の中に置くと Spotlight の索引で遅くなる。
  - 生出力 (export した XML の要約、結合表、スクリプト、コマンド、終了コード、binary の sha256) は `internal/ast/docs/_store-checker-7d2c-xctrace/` に置き、commit しない。
- **binary を作り直した直後**:
  - 最初の `xctrace record` は、画面の許可ダイアログ待ちで止まることがある。
  - record を始める前に「画面に許可ダイアログが出たら承認してください」とユーザーに伝える。
  - 対象プロセスが RSS 百数十 KB・CPU 0 のまま進まなければ、ダイアログ待ちを疑う。kill と再試行を繰り返さない。
- **root / sudo が要る操作**:
  - Bash ツールは TTY が無く `sudo` が動かない。
  - コマンドを渡してユーザーに別のターミナルで実行してもらい、`tee` した結果を読む。
  - 出力の file が空なら、貼り付けを頼む。

## 1. 計測の対象

### 1.1 driver

storeharness に xctrace 用の test (例: `TestXctraceCheck`) を足す。env で side を選び、1 プロセスで §4 と同じ仕事を K 回繰り返す。

- **入力**: compiler プロジェクト。
  - Store 側は 7d-2c の fixture (`STORE_CHECKER_FIXTURE`) を `FixtureProgram` で読む。
  - Pointer 側は `newCompilerProjectProgram`。
- **条件は KPC と同じ**:
  - `debug.SetGCPercent(-1)`、`runtime.GOMAXPROCS(1)`、`runtime.LockOSThread()`、`SingleThreaded`。
  - 各回の前に `runtime.GC()` で heap を戻す (Session.Measure と同じく 1 回)。
- **1 回の仕事**: `checkPointer` / `checkStore` (measure_test.go) をそのまま呼ぶ。
  - `NewChecker` + 診断対象の各 file の `GetDiagnostics` + `GetGlobalDiagnostics`。
  - program の構築と bind は回の外 (毎回作り直す。KPC と同じ)。
- **区間の切り出し**:
  - stack に `storeharness.checkStore` か `storeharness.checkPointer` を含む標本だけを数える。
  - 時刻の窓では切らない。
  - 区間の外の標本 (parse・bind・GC) は別の行として報告する。
- **K の決め方**: 1 side の 1 プロセスで、区間の合計が 10 s 前後になる回数 (目安 K = 10)。
  - export に時間がかかる (23 s の trace で 10 分超の例がある) ので、長くしない。

### 1.2 binary

- `go test -c` で、通常の build (tag なし) の binary を 1 つ作る。Pointer も Store もこの同じ binary で回す。
- sha256 を記録する。
- inline の判断を記録するため、同じ source で `-gcflags='-m=2'` の出力を `./internal/storechecker`・`./internal/ast/store`・`./internal/checker`・`./internal/ast` について scratchpad に保存する。§4 の (1) で使う。

## 2. 記録

### 2.1 cpu-counter (主)

```
xcrun xctrace record --template 'CPU Counters' --output <scratch>/<side>-<n>.trace \
  --env STORE_CHECKER_FIXTURE=... --env STORE_CHECKER_XCTRACE_SIDE=<side> \
  --launch -- <binary> -test.run '^TestXctraceCheck$' -test.count 1 -test.timeout 30m
```

- テンプレートは inst と cycles を PMC で取るもの。既存の手順は `cpu-counter` を使い、`kdebug-counters-with-time-sample` (core の累積 PMC、[0] = cycles、[1] = inst) を `time-profile` と sample 時刻で結合した。
- テンプレート名と、counter の並びを最初の trace で確かめて記録する。並びを思い込まない。
- **順番**: Pointer → Store を交互に、各 side 3 run。
- **A/A**: Pointer 同士の run 間の差を報告する。

### 2.2 export

```
xcrun xctrace export --input <trace> --toc
xcrun xctrace export --input <trace> --xpath '/trace-toc/run[1]/data/table[@schema="time-profile"]'
xcrun xctrace export --input <trace> --xpath '/trace-toc/run[1]/data/table[@schema="kdebug-counters-with-time-sample"]'
```

- Instruments 27 の XML では、`tagged-backtrace` の直下が `frame` になる。旧版を前提にした解析は、全 sample を未解決にする。
- 最初に 1 sample を手で読み、構造を確かめてから解析を書く。

### 2.3 結合

- 各 time-profile sample に、同じ thread の直前の sample からの PMC の増分 (inst・cycles) を割り当てる。
- leaf の関数を self、stack の全関数を inclusive として集計する。
- inline された関数は、Instruments が inline frame を出すかを最初に確かめる。出ないときは、外側の関数に帰属したまま扱い、§4 の (2) で計数 × 単価と照合する。

**結合の検証** (どちらかが外れたら結合を疑い、数字を採らない):
- 区間の標本の inst の合計 ÷ K が、Check KPC の inst/op (Pointer 8.59 G、Store 10.18 G) と ±3% で合う。
- 同じく cycles が、KPC の cycles/op (2.59 G / 2.98 G) と ±5% で合う。

## 3. 集計の単位

関数名から package の前置きを外して、Pointer と Store で突き合わせる:
- `checker.` ↔ `storechecker.`
- `ast.` ↔ `ast/store.`
- `printer.` ↔ `storeprinter.`

| 分類 | 中身 |
| --- | --- |
| 共通 | 両側に同じ名前の関数があるもの。差を関数ごとに出す |
| Store 固有: accessor | `ast/store` の `Node` の method (Kind、Parent、Expression、Name、Text、多相 accessor)、`Store.node`、list の解決 |
| Store 固有: utility | `ast/store` の関数 (GetSourceFileOfNode、FindAncestor、Is* の述語など) |
| map | `runtime.mapaccess*`・`mapassign*`・hash。直接の呼び出し元 (`PagedLinkStore.Get` の pageMap、`LinkStore`、`Store.locals` など) ごとに分ける |
| 測定用 adapter | `storeharness.(*FixtureProgram)`・`lookup`・`fmt.Sprintf` (adapter の下) |
| 書き換えた関数 | storechecker にだけあって checker に無いもの (手書き・`_manual.go`)。名前で突き合わない残り |
| Pointer 固有 | checker・ast にだけあるもの (interface 呼び出し、`GetNodeId` など)。差から引く側 |
| その他 | runtime (GC 以外)、malloc、memclr など |

**報告の表**:
- 分類ごと: 両側の self inst / cycles、差、差に占める割合。
- 共通関数: 差の上位 30 (inst と cycles の両方)。
- Store 固有: 上位 30。

## 4. 切り分け (7d 設計 §2.7 の順)

1. **2 word 渡しの境界と inline**
   - `-m=2` の出力で、差の上位の関数 (共通) が Pointer で inline され、Store で inline されないものを列挙する。逆向きも。
   - `store.Node` を引数か戻り値に持つ、inline されない関数の呼び出しの self cycles を合計して、上限として報告する。
2. **辺の解決と nil 終了の親ループ**
   - `Parent()`・`Store.node` が inline されて標本に出ない場合は、overlay の計数 (7d-2c の `count/` を流用) と単体の単価で見積もり、標本の外側の関数の差と照合する。
   - 親ループは 7d-2c で `Parent()` 24,650,495 回 / nil 547,441 回。呼び出し元の関数ごとの回数を overlay で取る。
3. **V1 の分岐**: 合成の Store の私有表を引く分岐 (header の flow / symbol / locals) の self を、Store 固有: accessor の中から分けて出す。
4. **links と map**
   - map の呼び出し元ごとの self を出す。
   - 7d-2c の見積り (`symbolNodeLinks` の pageMap 約 24 ms) と照合する。
   - 前回 (b719) は、`Store.locals` の map (NameResolver.Resolve が全祖先で引く) が 0.78 G あった。今回の Store で同じ経路が残っているかを確かめる。

## 5. 受入

**測定:**
- §2.3 の結合の検証が両側の全 run で通る。
- A/A (Pointer 同士) の inst の差が 1% 以内。

**帰属:**
- inst の差 (約 1.59 G) と cycles の差 (約 0.39 G) のそれぞれについて、分類の合計で 8 割以上を説明する。
- 届かなければ、届かない理由 (inline の帰属・標本の粒度など) を書いて未帰属として残す。

**照合:** 7d-2c の 2 つの確定項目と、xctrace の該当分類の値の差を報告する。
- adapter 約 14 ms
- links の map 約 24 ms

## 6. 報告

`store-checker-7d2c-xctrace-report-20261002.md` に次のものを書く。

- 変更 (file と内容)、before と開始時の差分、binary の sha256、テンプレートと counter の並び。
- 結合の検証の値 (標本の合計と KPC の値)、A/A。
- §3 の表、§4 の 4 項目。仮説と確定を分けて書く。
- 次の改善段の候補。各候補に、見積り (inst / cycles と、Check の cycles の比で何ポイント下がるか) と受入条件を付ける。
  - 線 1.10 に届く組み合わせがあるかも書く。

## 7. 決めること (着手前にユーザーへ確認)

1. **before の確定**: 7d-2c を先に commit して before をその hash にするか、未コミットの作業ツリーのまま測るか。推奨は先に commit すること。差分の hash で再現できるが、binary と source の対応が追いやすくなる。
2. **K と run 数**: 推奨は K = 10、各 side 3 run。export の時間が合わなければ K を下げ、結合の検証の誤差 (±3% / ±5%) を守れる範囲にする。
3. **inline frame が出ない場合の扱い**: 推奨は overlay の計数 × 単価で補い、標本では外側の関数に残す。`-gcflags=-l` で inline を止めた binary は、コードの形が変わるので使わない。
