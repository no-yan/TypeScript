# Store checker の性能 (TODO 7d-2c) 実装指示書

作成日: 2026-10-02。対象は計測を担当するエージェント。

7d-2 の最後の小段。Store checker の Check の費用、check の後の retained と GC、印字の全区間を、同じ入力の Pointer と比べて測る。あわせて、容量と R4' の判断材料を数える。**本段は測定と報告で、性能の改善は行わない。** 線を外れたら「性能未達」として数字と切り分けを残し、改善の方針は報告を見て決める。

| 前提 | 文書 |
| --- | --- |
| 線 (Check cycles ≤ 1.10、retained ≤ 0.85、mark CPU ≤ 0.75)、見積り、切り分けの順 | [7d 設計](store-checker-design-20260923.md) §2.7 |
| 2c の範囲、容量 (§2.5)、R4' と案 B / E (§3 の 5)、NB1' | [7d-2 設計の残り](store-checker-7d2-design-remainder-20260929.md) §2.4・§3・§4 |
| 隔離プロセスでの retained / GC、印字の全区間 | [レビュー契約](store-checker-review-contracts-20260926.md) §3・§4 |
| KPC の契約 (GC オフの Session、`SingleThreaded`) | `internal/testutil/kperf/session.go` |
| H1 の driver と adapter | `internal/storeharness/` (2b、9c93098cc3) |

before は **9c93098cc3**。開始時の HEAD と差分を記録する。

## 0. 規則

- **2b の等価が成り立つ入力だけで数字を採る** (7d-2 設計の残り §4)。compiler プロジェクトは 2b の corpus に入っていないので、§2 で先に等価を確かめる。
- 印字は NB1' (printer を `storeprinter` に変換) なので、Store → Pointer の変換器は無い。
  - 契約 §4 の「nodebuilder + 変換 + printer + 文字列」は、ここでは **nodebuilder + storeprinter + 文字列取得** の全区間になる。
  - Pointer 側は nodebuilder + printer + 文字列取得。
- 変更してよいもの:
  - storeharness (driver・adapter・ベンチ・fixture)
  - 計測用の新しい test file
  - docs
- 変更しないもの: storechecker・storeprinter・store の production、生成器と生成物、Pointer の package、go.mod / go.sum。
- 計数のための instrumentation は scratch の overlay だけで行い、追跡する変更に含めない。全区間のゲートの binary に混ぜない。
- KPC は root が要る。実行はユーザーに依頼する (§6)。
  - `sudo` は TTY の無い Bash ツールでは動かない。コマンドを渡し、ユーザーが別のターミナルで `tee` した結果を読む。
  - ns / allocs / retained / gctrace は自分で取る。
- 一時 binary・archive・probe は scratchpad に置く。repo の中に置くと Spotlight の索引で遅くなる。生出力は `internal/ast/docs/_store-checker-7d2c-impl/` に置き、コマンド・終了コード・HEAD・差分 hash・Go の版・環境変数を保存する。commit しない。

## 1. 入力

| 入力 | 用途 |
| --- | --- |
| compiler プロジェクト (`testdata/fixtures/compiler/tsconfig.json`、既存の KPC ベンチと同じ読み方) | Check KPC、retained / GC、印字 (walk の要求列) |
| compiler / conformance から選ぶ診断ありの corpus | 印字 (診断の message の印字を含む)。2b の段 2 で equal の構成から、診断の多い順に選ぶ。件数と選び方を manifest に固定する |
| 小さい例 (大きい union / intersection / conditional、alias / unique symbol、再利用部分木・Original / comment) | 印字の経路の切り分けだけ。主たる証拠にしない |

## 2. 前提の等価 (compiler プロジェクト)

2b の H1 の比較 (`comparePrograms`) を compiler プロジェクトの program に当てる。対象は次のとおり。

- 診断対象: 段 2 と同じ規則 (`SkipDefaultLibCheck` のときの default lib を除く SourceFiles)。
- walker 対象: root の全 file。

比べるもの (2b と同じ):

- 生の診断・suggestion・global
- 型と symbol の walk
- symbol の対応、token、CompareSymbols の性質、mode

**equal でなければ、ここで止めて報告する** (数字を採らない)。結果は件数とともに報告に残す。

## 3. 測定用の program (隔離、契約 §3)

retained / GC と KPC の Store 側は、**Pointer program を持たない**測定用 adapter で回す。

### 3.1 準備プロセス

Pointer program から、値だけの fixture を書き出す。

- 中身: file の順・name・path・text、parse options、options、module / type reference の解決、file metadata、package map、emit format / syntax の索引、合成 import の (file, 役割, text)、project reference の情報、default lib の印、その他 `storechecker.Program` と `Host` の全 method が返すもの。
- `ParsedCommandLine` の config AST などの間接保持も値に落とす。
- syntax に依存する問い合わせ (`GetEmitSyntaxForUsageLocation` など) は、file・位置・kind・mode の値で索引を作る。測定中に索引に無い問い合わせが来たら panic する (fallback 禁止)。

### 3.2 測定用 adapter

- fixture と Store file だけで `storechecker.Program` を実装する。合成 import は fixture の値から `store.NewSyntheticImport` で作る。
- Pointer の parser / program / SourceFile を、closure・interface・host・cache のどこにも持たない。

### 3.3 同一性の試験

測定用 adapter の上の Store checker が、2b の H1 adapter の上の Store checker と同じ記録を出すことを、次の 2 つで確かめる。

- compiler プロジェクト。
- 段 2 の構成の sample。全構成を回せるなら全量。

`comparePrograms` の比較器をそのまま使う。fixture の完全性も、この試験で確かめる。

### 3.4 構築経路の監査

Store の測定プロセスで次のものが作られないことを、構築経路の読みと live heap profile の両方で確かめる。

- `*ast.SourceFile`、`*ast.Node`
- `compiler.Program`
- Pointer の lib cache (`libCache`)

profile に何も出ないことだけを、参照経路が無いことの証明とは扱わない。

## 4. Check KPC (ゲート: cycles ≤ 1.10 × Pointer)

`kperf.Session` の契約 (GC オフ、`GOMAXPROCS(1)`、`SingleThreaded`) で、Pointer と Store を **同じ binary** で各 5 run 取る。

- **区間の中**: `NewChecker` と、診断対象の各 file の `GetDiagnostics`、`GetGlobalDiagnostics`。
- **区間の外**: parse・bind・program の構築、fixture の読み込み。
- **Store 側の adapter**: §3 の測定用 adapter を使う (§9 の 1、決定済み)。
- **報告すること**:
  - inst・cycles・IPC と、node あたりの値。
  - 旧基準値 `BenchmarkASTCheckKPCV1` (program 経由、2.60G cycles / 8.48G inst) を同じ binary 条件で取り直し、program 側の費用として並べる。
  - A/A のばらつき (Pointer 同士の run 間差)。KPC の inst は run の間で ±0.5% 動くことがあるので、Pointer の値を差し引いて読む。
- **線を超えたとき**: 7d 設計 §2.7 の順で切り分ける。
  1. 2 word 渡しの境界の数と inline の変化
  2. 辺の解決と nil 終了判定のある親ループ
  3. V1 の分岐
  4. links と map

  関数別の inst は overlay の計数か xctrace で取る。darwin の pprof は inline した leaf を過小帰属するので、pprof の % を帰属の根拠にしない。

## 5. retained / GC (ゲート: retained ≤ 0.85、mark CPU ≤ 0.75 × Pointer)

契約 §3 の手順で取る。

1. Pointer と Store を **別の子プロセス**で交互に走らせ、各 3 run 以上取る。各プロセスで、fixture だけを読んだ共通の baseline を取る。
2. 段は 2 つ: parse + bind の後と、check (§4 と同じ仕事) の後。同じ意味の root を生かし、GC を 2 回行って `HeapAlloc` と live heap profile を保存する。root は計測の終わりまで `KeepAlive` する。sync.Pool の victim cache に注意する (GC 2 回で揃える)。
3. 報告する値:
   - retained: baseline からの増分と絶対量の両方。
   - GC の測定は GC を有効にした別の実行で行い、最後の強制 GC の gctrace から mark CPU・scan bytes・GC 回数を取る。KPC (GC オフ) と混ぜない。
   - 内訳: AST、Bound、合成の Store、遅延 JSDoc の Store、foreign 表、links、adapter / fixture。adapter / fixture の分も隠さず含める。
4. 旧値 (Pointer の parse +101.6 / bind +26.6 / check +106.4 MB、7d-0 の parse + bind の比 0.567) は、checkout が違えば `stale` として並べるだけにし、判定には今回の Pointer の値を使う。

## 6. 印字の全区間 (契約 §4。線は置かない)

- **要求列**: §1 の入力から manifest に固定して、Pointer と Store で同じ列を使う。manifest の各要求に次のものを持たせる。
  - file、位置、操作 (`TypeToString` / `SymbolToString` / 診断の生成)、flags、期待出力の hash
- **主計測は全区間**:
  - Store: nodebuilder + storeprinter + 文字列取得
  - Pointer: nodebuilder + printer + 文字列取得
  - 取得済みの文字列を返すだけの試験にしない。
- **cache の状態を分ける**: 初回 (新しい checker、cache miss) と、同じ checker で同じ列を再実行した場合 (cache hit) を分けて報告する。
- **取る値**:
  - KPC (GC オフ): inst / cycles / IPC
  - 通常の GC: ns/op・B/op・allocs/op (raw と benchstat)
  - 別実行: 印字の後の retained / GC
- **内訳**: nodebuilder / printer の内訳は、全区間と別の instrumented run で取る。
- **出力の一致**: 出力の文字列は完全一致が条件。一致しなければ数字を採らない。
- **退行の扱い**: 再現する退行があれば、Check が合格でも未解決として残す。許容率を新設しない。

## 7. 判断材料の計数 (instrumented run、ゲートと別)

compiler プロジェクトの check (§4 の仕事) で数える。

1. **合成の Store**: checker あたりの Store の数、各 Store の使用率、links の外側の枠の長さ。容量 4,096 (`storechecker/region.go`) の決め直しの材料。
2. **R4' のコピー**:
   - `Nodes()` の呼び出し数・確保数・bytes (11 箇所それぞれと、call expression あたり)。
   - `getEffectiveCallArguments` の結果が resolveCall の外へ出る経路の有無 (コードを読んで答える)。
   - 案 B / E の判断材料。
3. **Store 固有の経路の頻度**: 2 word 渡しの境界、nil 終了の親ループの反復数。線を超えたときの切り分けに使う。

## 8. 試験と回帰

- §2 の等価、§3.3 の同一性の試験、§6 の出力一致は、テストとして残す。両 tag (通常と `storechecks`) で通す。
- 回帰: 次の 4 つ。既知の失敗は `FuzzEquivalence/a708303f611b95c8` だけ。
  - 段 1 strict `TestCheckerCorpus`
  - 段 2 strict `TestCheckerConfigurations`
  - `TestModeCorpus`
  - `go test -count=1 -timeout 60m -skip '^TestCheckerConfigurations$' ./internal/ast/store/... ./internal/store...` (両 tag)
- `go build ./...`、`go vet ./internal/ast/... ./internal/store...`、生成器 2 つを再実行して不変。

## 9. 決定事項 (2026-10-02、ユーザー: 3 つとも推奨どおり)

1. **Check KPC の Store 側の adapter**: §3 の測定用 adapter (隔離と同じもの) を使う。
   - H1 の adapter は Pointer program へ委譲する method と byPath の map を持ち、production の Store program と費用が違う。
   - 測定用 adapter の method の費用も production とは違うので、method ごとの呼び出し回数を併記する。
2. **KPC の区間の仕事**: `NewChecker` + 各 file の `GetDiagnostics` + `GetGlobalDiagnostics` とする。suggestion は含めない (compiler の `GetSemanticDiagnostics` に合わせる)。
3. **印字の corpus の大きさ**: 診断ありの構成から 200 構成と、compiler プロジェクトの walk の全要求。

## 10. 報告

`store-checker-7d2c-implementation-report-20261002.md` に次のものを書く。

- 変更 (file と内容)、before と開始時の差分。
- §2 の等価、§3.3 の同一性と §3.4 の監査。
- 3 つのゲートの値と判定 (raw・benchstat・A/A)。
- 印字の全区間 (cache miss / hit、内訳、出力一致)。
- §7 の計数。
- 線を外れたものの切り分け。仮説と確定を分けて書く。
- 回帰の結果。
- 判定は 2 つに分けて書く。
  - **2c の測定**: 全項目がそろったか。
  - **各ゲートの合否**: 線を外れたら、次の改善段の材料 (候補と見積りと受入条件)。
