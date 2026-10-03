# Store checker の性能 (TODO 7d-2c) 検証指示書

作成日: 2026-10-02。対象は検証を担当するエージェント。

改訂: 2026-10-03。Check の線を通すために入れた 3 つの変更 (46a98e1abd・f71b24045d・ec2196c835) を範囲に入れた。§1・§3・§5・§6・§8・§10 を改訂し、§4 (3 つの変更の監査)・§5.2 (Walk / Parse / Bind)・§5.3 (偽 Program 全体の費用) を足した。

[実装指示書](store-checker-7d2c-implementation-instructions-20261002.md) の測定と、[3 つの変更の実装指示書](store-checker-7d2c-abc-implementation-instructions-20261002.md) の変更を独立に検証する。

- **before**: **9c93098cc3** (7d-2c の開始時)。
- **after**: 検証の開始時の HEAD。production の source は ec2196c835 と同じであること (`git diff ec2196c835 HEAD -- tsc tools ':!tsc/internal/ast/docs'` が空)。

原則:

- 実装報告の数字を写すのではなく、**測定の条件と経路が契約どおりか**を確かめる。
- ゲートの値は、自分で取り直した値で判定する。
- 3 つの変更それぞれの効果の大きさ (3 つの変更の指示書の受入、「−40 M cycles 以上」など) は取り直さない。7d-2c のゲートは after の値だけで決まる。
- 改善を提案しても実装はしない。

## 0. 規則と証拠

- **先に読むもの**:
  - [7d 設計](store-checker-design-20260923.md) §2.7
  - [レビュー契約](store-checker-review-contracts-20260926.md) §3・§4
  - [実装報告](store-checker-7d2c-implementation-report-20261002.md) と、[3 つの変更の実装報告](store-checker-7d2c-abc-implementation-report-20261002.md)、それぞれの生出力 (`_store-checker-7d2c-impl/`・`_store-checker-7d2c-abc/`)
  - 背景として、[xctrace 報告](store-checker-7d2c-xctrace-report-20261002.md) と [accessor tax 報告](store-checker-7d2c-accessor-tax-report-20261002.md)
- 実装報告にあることと、検証で再現したことを区別して書く。
- **source の hash**: sha256 を開始時と終了時に保存する。対象は tsc / tools の追跡・未追跡の `.go`・`.ts`・`.mod`・`.sum`・`.json`。開始時と終了時で一致しなければならない。
- **記録するもの**: HEAD、before との差分、未コミット差分の hash、Go の版、環境変数、コマンド、終了コード。commit しない。
- **置き場**:
  - 一時 binary・overlay・probe・before の archive は scratchpad に置く (repo 内に置くと Spotlight の索引で負荷が上がる)。build は `GOWORK=off`。
  - scratchpad は PC の再起動で消える。binary・overlay・fixture を作るスクリプトと sha256 は結果の置き場に残す。
  - 結果は `internal/ast/docs/_store-checker-7d2c-results/` に置く。
- **KPC**: root が要る。ユーザーに別のターミナルで `sudo … | tee` を依頼し、そのファイルを読む。binary を作り直した後の最初の計測では、許可ダイアログに注意するようユーザーに伝える。

## 1. 範囲

before から after までの変更を、commit ごとに次の範囲と照合する。

| commit | 許す範囲 |
| --- | --- |
| 測定 (2980ba4970) | storeharness・計測用の test file・docs |
| 3 つの変更 (46a98e1abd・f71b24045d・ec2196c835) | 3 つの変更の指示書 §0 のとおり: `internal/ast/store/` の手書きの file (`store.go`・`*_manual.go`)、`internal/storechecker/*_manual.go`、`internal/storeharness/`、docs。生成物 (`storechecker/links.go`・`ast/store/utilities.go`) は `storetranslate` による作り直しの変化だけ |
| ほか (cd66aaa4d3・de23e1f53d・dd324e071a・9a4c31f470 と、この改訂の commit) | docs と `storeharness/xctrace_test.go` だけ |

- **不合格になる変更**: 上の範囲の外の変更。例えば、storeprinter の production、storechecker の `*_manual.go` 以外への手での変更、Pointer の package、生成器、手で直した生成物、go.mod / go.sum。
- **`xctrace_test.go`**: 環境変数で動く test で、ゲートの binary にも入る。計測区間の経路を変えないことを読む (`init`・package 変数の初期化・benchmark から呼ばれる関数が無いこと)。
- **instrumentation**: 計数のコードが、追跡する変更に入っていないこと。ゲートの binary に計数のコードが入っていないこと。ゲートの binary は build の入力 (source の hash と tag) から確かめる。

## 2. 前提の等価

- 実装の §2 (compiler プロジェクトの等価) を自分で再実行し、equal であることを確かめる。
- 印字の要求列の出力一致も再実行する。
- 比較器が感度を持つことを、scratch overlay で確かめる。Store の記録の 1 つと、印字の出力の 1 文字を変えると、どちらも失敗すること。

## 3. 隔離の監査 (契約 §3)

### 3.1 fixture と adapter を読む

- fixture の型を再帰的に読み、`*ast.*`・`compiler.Program`・`ParsedCommandLine` の AST・Pointer の host / cache への参照が無いことを確かめる。
- 測定用 adapter の全 method が、fixture と Store file だけから答えることを確かめる。
- **struct 鍵 (ec2196c835)**:
  - node を鍵にする 3 つの map は、`NewFixtureProgram` が fixture の文字列鍵から struct 鍵 (`nodeKeyOf`) に作り直す。
  - 作り直しの前後で件数が減らないこと (2 つの文字列鍵が 1 つの struct 鍵に潰れていないこと) を、compiler プロジェクトの fixture と段 2 の sample で確かめる。
  - 記録に無い問い合わせで panic し、そのメッセージに文字列の鍵が入ること。

### 3.2 検出感度

scratch overlay で 1 つずつ壊し、検出されることを確かめる。

| # | 故障 | 必須の検出 |
| --- | --- | --- |
| 1 | adapter の 1 つの method を Pointer program への委譲に戻す | 構築経路の監査、または live heap profile の Pointer の型 |
| 2 | 索引に無い syntax 依存の問い合わせに既定値を返す | fixture の完全性の試験 (同一性の試験で差が出る、または panic) |
| 3 | 合成 import を fixture から作らない (nil を返す) | 同一性の試験 |
| 4 | fixture の解決の 1 件を落とす | 同一性の試験、または panic |

### 3.3 同一性の試験を再実行する

測定用 adapter と H1 adapter で同じ記録になることを、自分で確かめる。対象は compiler プロジェクトと段 2 の sample (実装と別の seed)。

### 3.4 live heap profile

Store の測定プロセスの profile を自分で取り、次のことを確かめる。

- `*ast.Node`・`*ast.SourceFile`・`compiler.Program` の割り当て元が無いこと。
- あれば、生成元・数量・寿命を調べる。印字が実際に作る一時ノードかどうかを区別する。

### 3.5 fixture を作り直す

- after から `TestWriteCompilerProjectFixture` で fixture を作り直す。
- sha256 を、3 つの変更の計測が使った fixture (`3f44678b59c91f5285d2e5520e44ecc3a640544174455050891e494f4f7a34c2`、`_store-checker-7d2c-abc/start.txt`) と比べる。
  - 一致すれば、3 つの変更が fixture (Pointer の Program の応答) に影響しないことの証拠になる。
  - 一致しなければ、差分の中身を調べる。
- §5 の KPC と §6 の retained は、作り直した fixture で取る。

## 4. 3 つの変更の監査

### 4.1 手書き版の意味

置き換え前の版と並べて読み、意味が同じことを確かめる。近道をしている箇所は、元の版でその入力に何が起きるかから正しさを示す。

| file | 対象 | 並べる相手 |
| --- | --- | --- |
| `storechecker/links_manual.go` | `nodeLinkStore` の `Get`・`Has`・`TryGet`・`newPage` | 46a98e1abd^ の `links.go` (`core.PagedLinkStore` 経由) |
| `ast/store/utilities_manual.go` | `FindAncestorOrQuit`・`GetRootDeclaration`・`GetCombinedNodeFlags`・`IsGlobalSourceFile`・`HasQuestionToken`・`GetSymbolId` | f71b24045d^ の `utilities.go` と `utilities_manual.go`、Pointer の `ast` の同名の関数 |
| `ast/store/store.go` | `(*Store).node` (xor)、`Node.File` (n 自身から始める) | f71b24045d^ の `store.go` |

確かめること:

- **`nodeLinkStore`**:
  - 同じ鍵に同じ `*V` を返すこと。
  - `Has` と `TryGet` が確保しないこと。
  - nil の Node が `NodeKey{}` の 1 つの鍵になること。
- **`(*Store).node`**: foreignBit の無い壊れた値が、bounds check で panic すること。scratch の試験で、通常の build と `storechecks` の build の両方で 1 回ずつ確かめる。
- **hash の行**:
  - 元の宣言の hash の行が合うこと。`storetranslate` を走らせて止まらず、生成物が変わらないことで確かめる (§9 と兼ねる)。
  - 感度: hash を 1 つ壊すと `storetranslate` が止まること。

### 4.2 StoreID の重なり

- links の鍵は `(StoreID, NodeRef)` で、外側の表の添字が StoreID。2 つの Store が同じ StoreID を持つと、別の Store のノードの links が黙って混ざる。
- check の間に links の鍵になりうる全 Store の StoreID が、program の中で重ならないことを確かめる。対象は次の Store。
  - source
  - 遅延 JSDoc
  - checker の合成
  - 印字の EmitContext
  - `store.NewSyntheticImport`
- 確かめ方は 2 つ。
  - **コード**: 採番の経路を読む。
  - **実測**: overlay で、`nodeLinkStore` に来た鍵の StoreID ごとに Store の同一性を記録する。compiler プロジェクトと段 2 の sample で取る。

### 4.3 inline

3 つの変更の報告 §2.2 の主張を、自分の build で取り直す。対象は次の 8 関数で、以下「8 関数」と呼ぶ。

`Node.File`・`FindAncestor`・`FindAncestorOrQuit`・`GetRootDeclaration`・`GetCombinedNodeFlags`・`IsGlobalSourceFile`・`HasQuestionToken`・`GetSymbolId`

- **`-m=2`**:
  - `./internal/ast/store` だけにかける (`./internal/storechecker` は 8 GB 機では終わらない)。tag なしと `storechecks` の両方で取る。
  - `(*Store).node` と 8 関数の cost を記録する。
  - tag なしで 9 つすべてが `can inline` であること。
- **objdump**:
  - after の storeharness の kperf binary で、8 関数への直接の `CALL` が 0 本であること。
  - storechecker の callback (`.func1` など) への `CALL` は、測定用 adapter の 1 本 (`(*tracedTypeAdapter).Display.func1`) だけであること。
  - 比較のため、3 つの変更の前 (de23e1f53d) の binary の本数も数える。報告では 325 本。

## 5. KPC の再測定

### 5.1 Check

- **binary**: after の source から、自分で kperf の binary を作る。build の入力の hash を記録する。
- **実行**: Pointer → Store の順を 1 run として、5 run をユーザーに依頼する。
- **区間**: 区間の中身が、実装指示書 §4 と §9 の決定 (suggestion を含めない、測定用 adapter) どおりであることを、ベンチのコードで読む。
  - parse・bind・fixture の読み込みが区間の外にあること。
  - Pointer と Store が同じ仕事 (同じ file の列、同じ API) をすること。
- **判定**: cycles の比が ≤ 1.10 か。inst・IPC・node あたりの値と、Pointer の A/A の幅 (≤ 2%) を報告する。
- **照合**: 3 つの変更の報告の値 (cycles 比 1.053) と比べる。差が A/A の幅を超えれば原因を調べる。
- **旧基準値**: `BenchmarkASTCheckKPCV1` も取り直し、program 経由の値として並べる。

### 5.2 Walk / Parse / Bind

- **取り直す理由**:
  - f71b24045d は `(*Store).node` を変えた。walk・parse (Builder の view も `node` を通る)・bind の経路が変わる。
  - 3 つの変更の実装は Walk と Bind だけを取り、Parse を取っていない。
- **ベンチ**:

| 段 | ベンチ | package | 線 |
| --- | --- | --- | --- |
| Walk | `BenchmarkStoreWalkKPCV1` | `ast/store` | ≤ 1.15 |
| Parse | `BenchmarkStoreParseKPCV1` (checker.ts と dom) | `storeparser` | ≤ 1.00 (どちらも) |
| Bind | `BenchmarkStoreBindKPCV1` | `storebinder` | ≤ 1.10 |

- **回し方**: 3 run × `-benchtime 20x -count 5`。比は、同じ binary の中の `/store` ÷ `/pointer` の cycles。
- **集計**: sub-benchmark を 1 つの鍵にまとめない (accessor tax の集計の誤り、3 つの変更の報告 §4)。

### 5.3 偽 Program 全体の費用 (報告のみ、線なし)

- **背景**:
  - 3 つの変更の実装が測ったのは、node を鍵にする 3 method の呼び出し (45,526 回) だけ。
  - adapter の呼び出しは全体で 168,302 回 (30 method) ある。全体の費用は見積りで約 7 ms/run (報告 §2.3)。
  - adapter は直さず、比に残る偏りは 7e の CLI 結線で消す (2026-10-03、ユーザー)。
  - ここでは、ゲートの Store の値に adapter の費用がどれだけ入っているかを実測で示す。
- **測り方**:
  - 1 回の check で、全 method の呼び出し (method と引数) を記録する。
  - それを同じ `FixtureProgram` に再生する。1 op = 1 check 分の呼び出し。
  - overlay か scratch の test で行い、commit しない。
  - 再生は、できれば kperf の Session で測り、cycles で出す。
- **報告する値**:
  - method ごとの回数と単価。
  - 合計 (ms/run と cycles)。
  - §5.1 の Store の Check の cycles に対する割合。
  - 見積り (約 7 ms) との照合。

## 6. retained / GC の再測定

- 別の子プロセスで、Pointer と Store を交互に各 3 run 以上取る。
- 確かめること:
  - 共通の baseline と GC 2 回の手順。
  - root が `KeepAlive` されていること。
  - sync.Pool の条件が揃っていること。
- **判定**: retained の比 ≤ 0.85、mark CPU の比 ≤ 0.75。値は、最後の強制 GC の gctrace の行から自分で読む。
- **内訳**: 内訳の和が全体と合うことを照合する。adapter / fixture の分が入っていることも確かめる。
- **check の間の増分 (報告のみ、線なし)**:
  - parse + bind の後から check の後までの HeapAlloc と scan heap の増分を、Pointer と Store のそれぞれで出す。
  - heap profile の差分 (inuse_space、check の後 − parse + bind の後) で、上位の割り当て元を並べる。
  - 3 つの変更の生出力から読むと、増分は Pointer +103.8 MB・Store +115.4 MB。
- **混在の禁止**:
  - GC オフの KPC の値と混ぜていないこと。
  - 比較用の Pointer を保持した混在ハーネスの値で代用していないこと。
  - 条件を満たす artifact が無ければ `missing` とする。

## 7. 印字の全区間

f71b24045d は印字が通る accessor も変えたので、after で取り直す。

- **区間の範囲**: nodebuilder + storeprinter + 文字列取得が区間の中にあることを、ベンチのコードで読む。
  - 取得済みの文字列を返すだけになっていないこと。
  - 印字の変換・cache の構築を区間の外に置いていないこと。
- **cache の状態**: cache miss と cache hit の両方が、それぞれ正しい状態で測られていること。
  - miss: 新しい checker。
  - hit: 同じ checker で同じ列を再実行。
- **manifest**: 要求列が Pointer と Store で同じであること (manifest の hash と件数)。
- **再測定**: KPC・ns/op・B/op・allocs/op を自分で取り直す。benchstat の結果を保存する。
- **内訳**: 内訳の instrumented run が、全区間の数字に混ざっていないこと。

## 8. 判断材料の計数

- 合成の Store の数と使用率、R4' の確保数と bytes を、自分の overlay で 1 回数え直す。
- 実装の値と一致すること。一致しなければ、計数の位置の違いを調べる。
- `getEffectiveCallArguments` の結果が resolveCall の外へ出る経路があるかを、自分でコードを読んで答える。
- **links の外側の表** (7d-2 設計の残り §126): compiler プロジェクトの check の後について、次を数える。
  - `nodeLinkStore` の外側の表の長さ (StoreID の最大値 + 1)。
  - Store ごとの page 表の長さと、確保された page の数。
  - page の中で使われた要素の割合 (密度)。Pointer の `GetNodeId` は links を持つノードにだけ番号を振るので、比較のため Pointer の `PagedLinkStore` の page の数と密度も並べる。

## 9. 回帰

実装指示書 §8 の回帰を、after で再実行する。

- 段 1 strict、段 2 strict (`TestCheckerConfigurations`)、`TestCompilerProjectEquivalence`、`TestModeCorpus`、両 tag の一式。既知の失敗は `FuzzEquivalence/a708303f611b95c8` だけ。
- build と vet が通ること (両 tag)。
- 生成器 2 つ (`node tools/scripts/tsc/generate.ts` と `storetranslate`) を回しても、生成物が変わらないこと。
- 終了時の source hash が、開始時と一致すること。

## 10. 検証報告と判定

`store-checker-7d2c-verification-report-20261002.md` を作る。

**含めるもの**:
- 全コマンドと終了コード、成果物の場所
- 範囲の照合
- 隔離の監査と感度検査
- 3 つの変更の監査
- 再測定 (Check・Walk / Parse / Bind・retained / GC・印字)
- 偽 Program 全体の費用
- 計数、回帰、source の不変

**判定は分けて書く**:
- **2c の測定の正しさ**: 次の 4 つがそろったか。
  - 契約どおりの条件で測ったか。
  - 隔離が成り立つか。
  - 比較器と監査に感度があるか。
  - 再測定が実装の値と幅の内で一致するか。
- **3 つの変更の正しさ**: 範囲・意味・StoreID・inline の監査を通ったか。
- **各ゲートの合否**: Check cycles・Walk・Parse (checker.ts / dom)・Bind・retained・mark CPU。
- **線の無いもの**: 印字は、退行の有無と大きさだけを書く。偽 Program 全体の費用と check の間の増分は、値だけを書く。

残件には、問題・証拠・再現・仮説・修正案・受入条件を付ける。改善の実装には進まない。
