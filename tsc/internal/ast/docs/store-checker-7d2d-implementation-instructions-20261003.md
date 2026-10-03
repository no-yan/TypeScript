# 7e の前段 (TODO 7d-2d) 実装指示書

作成日: 2026-10-03。対象は実装を担当するエージェント (新しいセッション)。

7d-2c の検証で、7e (`--noEmit` での CLI 結線) の前に片付けると決めたものを 1 つの段にまとめる (2026-10-03、ユーザー)。

| 記号 | 内容 | 根拠 |
| --- | --- | --- |
| A | `typeToString` / `symbolToString` ごとに容量 4,096 の Store (約 420 KB) を確保する退行を直す。印字の Store を 16 行から倍々に伸ばす (案 1) | [7d-2c 実装報告](store-checker-7d2c-implementation-report-20261002.md) §6.3、[検証報告](store-checker-7d2c-verification-report-20261002.md) §7 |
| B | 同一性の試験 (`fixtureIdentity`) に、合成 import の specifier の照合を足す | 検証報告 §3.2・§10 の残件 3 |
| C | fixture の byte を、同じ source から作り直して変わらないようにする | 検証報告 §3.5・残件 4 |
| D | Check の KPC の A/A を 2% 以内に収める手順にする | 検証報告 §5.1・残件 2 |

- before: 開始時の HEAD (621f241fbf 以降)。開始時に HEAD と未コミットの差分の hash を記録する。
- 前提の値 (7d-2c の検証、repo の tree から build):
  - Check の cycles 比 1.0635 / 1.0493。
  - 印字の configurations / build: cycles 比 1.417、B/op 103.0 MiB 対 2.9 MiB。
  - configurations / 診断の生成: B/op 5,540 MiB 対 244 MiB。

## 0. 規則

- **変更してよいもの**:
  - `internal/ast/store/` の手書きの file (`factory.go` など)
  - `internal/storeprinter/region.go`、`internal/storechecker/region.go`、`internal/storechecker/*_manual.go`
  - `internal/storeharness/`
  - docs
- **変更しないもの**:
  - 生成物 (`// Code generated` の file)。`storechecker/nodebuilder.go` の `getNodeBuilder` も生成物で、ここが呼ぶ `Factory.ReleaseArenas` (手書き) の側で直す。
  - Pointer の package、go.mod / go.sum。
- **commit**: A・B・C を別々の commit にする。D は手順なので commit しない (スクリプトは生出力の置き場に残す)。
- **binary の作り方**:
  - 比べる binary は、すべて repo の tree から build する。before と after の違いは commit の切り替えではなく、scratch の overlay (`go test -overlay`) か、同じ path での順の build で入れる。
  - `git archive` で別の path に出した tree から build しない。`repo.TestDataPath()` が compile 時の path で決まり、Pointer 側だけが別の path の file を読む (検証報告 §10 の残件 1、+0.9% inst)。
- **一時物**: binary・overlay の写し・fixture・manifest は scratchpad に置く (repo 内に置くと Spotlight の索引で負荷が上がる)。scratchpad は再起動で消えるので、作り直すスクリプトと sha256 を生出力の置き場に残す。build は `GOWORK=off`。
- **生出力の置き場**: `internal/ast/docs/_store-checker-7d2d/` (commit しない)。
- **sudo**: KPC は root が要る。コマンドを渡し、ユーザーが別のターミナルで `tee` した file を読む。binary を作り直した後の最初の計測では、許可ダイアログに注意するようユーザーに伝える。
- **記録の分け方**: 仮説と確定を分けて書く。見積りには「見積り」と書く。

## 1. A: 印字の Store の確保

### 1.1 仕組み (確定)

- `storechecker.(*Checker).getNodeBuilder` (`nodebuilder.go:135`、生成物) は、`typeToString` / `symbolToString` の 1 回ごとに `EmitContext().Factory.ReleaseArenas()` を呼ぶ。
- Store の `Factory.ReleaseArenas` は `f.active = nil` だけ (`ast/store/factory.go:102`)。次の node で `newStore` が容量 4,096 の Store を作る。
  - header 32 B × 4,097、extra 16 × 4,096 × 4 B、texts 8 × 4,096 B で、約 420 KB をゼロ埋めで確保する。
  - 使うのは数〜数十行。compiler プロジェクトの check の中だけで 99 Store、使用は 1〜53 行。
- Pointer の `NodeFactory.ReleaseArenas` (`ast/ast.go:3183`) は arena を捨てるだけ。次の確保は arena の chunk を 1 要素から 2 倍ずつ 256 まで伸ばす (`core/arena.go:61`)。
- Store の header の配列は動かせない (不変条件 15: 公開した header の配列は動かさない)。Store を伸ばす形は取れない。

### 1.2 変更: 印字の Store を小さく始めて倍々に伸ばす (案 1、2026-10-03、ユーザー)

印字の EmitContext の Factory (`storeprinter/region.go` の `NewNodeFactoryIn`) だけに効かせる。checker の Factory (`storechecker/region.go`、容量 4,096) は変えない。checker の Factory の容量の決め直しは 7e の後 (2026-10-03、ユーザー)。

- **最小容量 16 行・上限 4,096 行**。
  - release の後の最初の Store は 16 行で作る (header 32 B × 17 + extra 16 × 16 × 4 B + texts 8 × 16 B で約 1.7 KB)。
  - Store が満杯になるたびに、次の Store の容量を 2 倍にする (16 → 32 → … → 4,096、以後 4,096)。
  - `ReleaseArenas` は今までどおり active を捨て、次の容量を 16 に戻す。
- **作り方で区別する**:
  - checker の Factory に効かないよう、`NewFactory` とは別の構築関数 (例: `NewGrowingFactory(region, min, max, hooks)`) か、印字の Factory だけが使う選択肢にする。
  - `extraCap`・`textsCap` も、その Store の容量に比例させる (今は `16 × capacity`・`8 × capacity`)。
- **変えないもの**:
  - Store の行を動かさない (不変条件 15)。満杯の判定 (`row` の `len(s.nodes) < cap(s.nodes)`) の形も保つ。
  - 合成の Store の不変条件 (17・18・26)、`Factory` の hook の呼び方。
- **試験**: `ast/store` に、印字の Factory と同じ構築で次を確かめる試験を足す。
  - release の前後と、容量の切り替わりの前後で、ノードが正しい Store に書かれる。
  - 容量が 16・32・… と伸び、release で 16 に戻る。
  - 境目をまたぐ親子の辺 (foreign の辺) が解ける。

**採らなかった案** (2026-10-03、ユーザー):

| 案 | 理由 |
| --- | --- |
| 案 2: 印字の容量を 256 に固定 | 16 行以下の印字 (check の中の印字の大半、中央 8 行) でも 1 回約 26 KB を確保する |
| 案 3: release をまたいで同じ Store に書き続ける | release の意味 (cache に残らないノードを捨てる) が Pointer と変わる。印字の後も active の Store (最大約 420 KB) が checker の寿命の間生きる |
| 案 4: 使い捨ての Store を release で 0 埋めして使い回す | 印字のノードは `NodeBuilderLinks.serializedTypes` の cache (`nodebuilderimpl.go:3212`) に入り、release の後も `DeepCloneNode` (`3189` 行) で読み直される。0 埋めすると cache の木が壊れる。安全にするには cache に入れるときに写す必要があり、cache を書く関数 (生成物) を P2 で手書きにすることになる |

### 1.3 計測 (案 1 と before)

案 1 (after) と before を、repo の tree の上の 2 本の binary で比べる。before は開始時の HEAD、または C・B を入れた後の commit (A の前)。

| 計測 | 対象 | 回し方 |
| --- | --- | --- |
| 印字の ns・B/op・allocs/op | `BenchmarkPrint` | `STORE_CHECKER_PRINT_STRIDE=1000`、`-benchtime 1x -count 5`。benchstat |
| 印字の KPC | `BenchmarkPrintKPC` | 同じ間引き、3 run |
| Check の KPC | `BenchmarkCheckKPCPointer` → `BenchmarkCheckKPCStore` | §4 (D) の手順で、2 本を交互に 5 run |
| retained (check の後) | `TestRetained` | 2 本、各 3 run |
| retained (印字の後) | `TestRetained` に `STORE_CHECKER_RETAINED_WORK=print` | 2 本、各 3 run |
| 出力の一致 | `TestPrintOutputs` (間引きなし) | after |
| 合成の Store の数と使用率 | 7d-2c の計数 (検証の `overlay/count/`) と同じ観点 | after、compiler プロジェクトの check。Store の数と容量ごとの数、foreign の辺の数 |

- 印字の全量の KPC (`STORE_CHECKER_PRINT_STRIDE=1` の Store 側) が完走するかも、before と after で 1 回ずつ試し、結果 (完走・kill、時間) を書く。7d-2c では、GC オフの session で回収されない確保が積もって kill された。
- 報告する値:
  - 印字: 集合と操作ごとに、before と after と Pointer の値、before からの差。
  - Check: cycles・inst・IPC。
- 印字は線を置かない (契約 §4)。許容率を新設せず、絶対の増分で比べる。
- **止まる条件**: 次のどれかが出たら、commit の前に報告してユーザーに相談する。
  - Check の cycles 比が 1.10 を超える。
  - check の後の retained が 0.85、mark CPU が 0.75 を超える。
  - 印字のどれかの操作で、Store の cycles が before より A/A の幅を超えて増える。

## 2. B: 同一性の試験に合成 specifier の照合を足す

### 2.1 問題 (確定)

- `fixtureIdentity` (`storeharness/fixture_test.go`) は、記録の run と再生の run の記録を `compareRecords` で比べるだけ。
- 2b-3 で `comparePrograms` に足した `compareModes` (`configurations_test.go:185`) の、合成 specifier の照合を持たない。
- 検証の故障 3 (`FixtureProgram` が合成 import を作らない) は、合成 import を持つ 139 構成のうち 62 構成でしか検出されなかった。checker が問うた 118 構成でも 56 構成は記録が変わらない。

### 2.2 変更

`fixtureIdentity` で、記録が equal のとき、各 file について H1 の Program と `FixtureProgram` の合成 specifier を照合する。

- 対象の役割は 2 つ: import helpers と JSX runtime。
- 照合は `compareModes` と同じ観点:
  - 有無が同じ。
  - 同じ path と役割で 2 回引くと同じ Node。
  - 形が同じ: StringLiteral、親が ImportDeclaration、その親がその program の file の root、`File()` がその file、text が同じ。
- `FixtureProgram` の側は、`GetJSXRuntimeImportSpecifier` を呼ぶと記録に無い path で panic する (`lookup`)。そのため、`helperImports` / `jsxImports` の map を直接読むなど、記録に無い問い合わせを起こさない形にする。
- 不一致の class は `compareModes` の名前に合わせる (例: `mode: synthetic import`)。

### 2.3 受入

| 項目 | 条件 |
| --- | --- |
| 感度 | 検証の overlay `mut3` (`_store-checker-7d2c-results/overlay/gen_mut.py`) を当てて、`TestFixtureIdentityConfigurations` を検証の sample (`_store-checker-7d2c-results/sample-filter.txt`) に回すと、合成 import を持つ 139 構成すべてで不一致になる |
| 誤検出なし | 変更の後、`TestFixtureIdentityCompilerProject` と `TestFixtureIdentityConfigurations` (段 2 の全 15,205 構成) が通る |

## 3. C: fixture の byte を揃える

### 3.1 問題 (確定)

- `Fixture.Modules` の番号は、記録の run で checker が初めて問うた順 (`Recorder.moduleIndex`)。
- この順が run によって揺れたことがある。accessor tax 報告では、同じ production の source から作り直して sha が変わった。検証の 2 回は一致した。
- その結果、fixture の sha を同一性の印に使えない。

### 3.2 変更

- `Recorder.Fixture()` (書き出しの前) で、`Modules` を値の順 (例: `ResolvedFileName`、次に残りの field) に並べ替える。
- `Resolved`・`FromSpecifier` の添字を付け替える。
- `FixtureProgram` の読み方は変えない。fixture の JSON の形も変えない (field を足さない)。

### 3.3 受入

| 項目 | 条件 |
| --- | --- |
| 決定性 | 同じ source から `TestWriteCompilerProjectFixture` を 5 回走らせ、5 本の sha256 が一致する |
| 意味 | 並べ替えの前の fixture と、module の添字を解いて全 field が同じ (検証の `fixture-semantic-diff.py`) |
| 同一性 | `TestFixtureIdentityCompilerProject` と段 2 の `TestFixtureIdentityConfigurations` が通る |

- C を A の計測より先に commit し、A の計測は並べ替えた後の fixture で取る。fixture の sha256 を生出力に記録する。

## 4. D: Check の KPC の手順 (A/A ≤ 2%)

### 4.1 問題

- 7d-2c の検証で、Pointer の cycles の幅が 3.41% と 2.14% で、契約の 2% を超えた。inst は 0.2% で安定していた。
- KPC 1 では、最初の 2 run で 4 本のベンチがそろって 2.5〜3% 高かった。
- 開始の条件 (1 分 load < 1.5) が緩く、直前の作業の残りが cycles に出たと見ている (仮説)。

### 4.2 手順

- 開始の条件を、1 分 load < 1.0 にする。scratchpad の作業中の印 (検証の `busy.sh` と同じ形) が無いことも条件にする。
- 本番の run の前に、捨てる 1 run (全 binary を 1 回ずつ) を入れる。捨てる run の値も生出力に残す。
- 5 run の各 run で、binary の順を回す (例: run 1 は before・after、run 2 は after・before)。
- 報告: Pointer の cycles の幅 (全 binary の全 run と、binary ごと)。

### 4.3 受入

- Pointer の 5 run の cycles の幅 ≤ 2% (binary ごと)。
- 超えたら、超えた run と load・時刻を書き、もう 1 回取る。2 回とも超えたら、ユーザーに相談する。

## 5. 等価と回帰 (A を入れた後に 1 回)

7d-2c の実装指示書 §8 と同じ回帰を回す。手順は検証の `regress.sh` (`_store-checker-7d2c-results/`) を元にする。

- 段 1 strict、段 2 strict、`TestCompilerProjectEquivalence`、`TestModeCorpus`
- package の試験 (両 tag)。既知の失敗は `FuzzEquivalence/a708303f611b95c8` だけ。
- `TestPrintOutputs` (間引きなし、全 201 target)
- `go build ./...`・`go vet` (両 tag)、生成器 2 つの再実行で生成物が不変

## 6. 報告

- 置き場: `store-checker-7d2d-implementation-report-20261003.md`
- 書くこと:
  - 開始時の状態、変更 (file と内容)、commit
  - A の before / after の値 (§1.3 の全表) と、合成の Store の数と使用率
  - B・C の受入の結果
  - D の手順で取った A/A
  - 回帰の結果
  - 仮説と確定の区別、指示書の外で判断したこと

## 7. 決めたこと (2026-10-03、ユーザー)

1. **A の直し方**: 案 1 (最小 16 行・上限 4,096 行の倍々)。比べる段は設けない。採らなかった案と理由は §1.2。
2. **A の commit**: §1.3 の計測で止まる条件に当たらなければ、そのまま commit する。当たれば報告して止まる。
3. **commit の順**: C → B → A。C は A の計測の fixture を決め、B は A と独立。
