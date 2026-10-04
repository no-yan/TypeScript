# Store CLI 7e 検証指示書

作成日 2026-10-04。対象は実装担当者とは別の検証担当者。

[実装計画指示書](store-cli-implementation-instructions-20261004.md) の A から E を、実装報告、source、実 CLI、独立した測定で確認する。根拠は [CLI 設計書](store-cli-design-20261004.md)。大規模入力の性能基準は同日のユーザー指定を優先し、Store/Pointer 比で時間0.70、保持メモリ0.80、mark CPU 0.80以下とする。

## 0 検証対象を記録する

- [ ] 実装計画、実装報告、全差分、`_store-cli-impl-20261004/` を読む。報告の数値を写して受入に使わない。
- [ ] 選択 repo を `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript` に固定する。文書作成時の HEAD は `a16540cd8e828b53ad269130889bcf680debd2e4`。検証時は実装後の実際の HEAD と dirty diff を記録する。
- [ ] 結線前 Pointer の source snapshot と binary を実装担当者から受け取る。結線後の同一 binary 内 Pointer/Store 比較と、結線前後の Pointer 比較を分ける。
- [ ] 検証結果を `tsc/internal/ast/docs/_store-cli-results-20261004/` に保存する。source を直さず、追加の計数・故障注入は scratch overlay に置く。
- [ ] 開始・終了時に `tsc/` と `tools/` の tracked/untracked source、設定、module file の hash を保存する。結果ディレクトリと生成した binary は除く。検証による恒久差分がないことを最後に確認する。
- [ ] すべてのコマンドに cwd、環境、開始終了時刻、exit status、stdout/stderr を残す。`tee` を使う場合は pipeline の元コマンドの終了値も保存する。

近隣の `TypeScript-checker-learning` は異なる checkout である。binary の build path と `repo.TestDataPath()` が読む fixture path を選択 repo に揃える。path を変えた `git archive` の build を無条件に比較しない。

本書の新 test/benchmark 名は実装契約であり、文書作成時には存在しない。実装後に `go test -list` で存在を確認する。空の regex 結果、`[no tests to run]`、skip、終了コード0だけでは合格にしない。

## 1 既存 artifact を先に分類する

再測定前に metadata、raw、測定区間を読む。状態は次の4値だけを使う。

| 状態 | 判定 |
| --- | --- |
| `current` | `repo_root` と、binary にコードを提供する実在 repository 全ての git rev が選択 checkout と一致する。さらに dirty diff、overlay、binary、入力、環境の一致を確認して比較可能性を判定する |
| `stale` | artifact はあるが root/rev が一致しない、または出所を現在の tree に結び付けられない |
| `missing` | 要求する測定の一次データがない |
| `unsupported` | artifact はあるが、その `bench.txt` に要求 regex の `Benchmark...` 行がない |

tag不足や権限不足で KPC を取得できない場合は実行上の理由を別欄に書く。artifact 状態と「入力が初版非対応」を同じ field に入れない。古い `kpc.txt` のような形式では元ファイルを保持し、抽出元と regex を記録した `bench.txt` を作ってから stage を判定する。

文書作成時の確認結果は次のとおり。

| artifact set | 状態 | 利用範囲 |
| --- | --- | --- |
| `_store-cli-design-20261004/` の再集計と `_store-checker-7d2d/kpc.txt` | `stale` | 旧 tree `9a0f025bc4` と A の差分に対する GC off の Check 区間 |
| `_store-checker-7d2d-results/` の Check KPC | `missing` | 省略理由の記録はあるが新しい一次計測はない |
| native CLI end-to-end、NativeCheckKPC、native Program 経由計測 | `missing` | 新実装後に取得する |

既存 benchstat の参考値は以下である。再ベンチは行っていない。

| 旧 Check | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Pointer | 816,251,082 | 209,437,896 | 1,433,498 |
| Store | 860,867,834 | 225,120,824 | 1,569,732 |

保存済み benchstat は順に +5.47%、+7.49%、+9.50%、各 n=5、p=0.008。95%区間の標本不足警告がある。native の合否や通常 GC の end-to-end 比には使わない。

旧 hot paths は node/parent access、symbol/flow、links、harness 問い合わせ。旧 allocation drivers は type arena、`newObjectType`、union/intersection の property、`nodeLinkStore.newPage`、合成/印字 Store。後続修正済みの費用を現在の問題として扱わない。現時点の診断は native 実入力データの欠如であり、次の action は接続と独立等価試験である。

## 2 生成境界と本番経路を監査する

- [ ] `storetranslate` の root 一覧と到達宣言一覧を保存する。`ProgramLike` 経由で不要な Emit、GetDeclarationDiagnostics、incremental、LS、transformer へ広がっていないことを確認する。
- [ ] generated file の直接編集がなく、手書き replacement の pointer-hash が元宣言と一致することを確認する。`STORETRANSLATE_FORCE` が未設定であることも確認する。
- [ ] 同じ tree で生成器を2回実行し、全生成出力の hash が変わらないことを確認する。dirty tree では `git diff` が空という条件を使わず、実行前後を比較する。
- [ ] scratch 上の元 compiler 宣言を変更し、新しい compiler 用 manual 境界の hash 検査が失敗することを確認する。既存 `TestManualHash` の成功だけで新境界を証明しない。
- [ ] `performStoreCheck` から Store Host、parser、binder、checker、reporter まで呼出関係を読む。Store 本番経路から storeharness、FixtureProgram、Pointer Program を呼ばないことを確認する。
- [ ] 計数用 build で入力 path と役割を識別する。Store CLI の対象 TS/JS/JSON/lib に対する Pointer parse/bind/check を各0回と確認する。tsconfig JSON の Pointer parse は別集計にする。
- [ ] 未指定/pointer 実行では Store Program、IDSpace、checker の構築を各0回と確認する。同じ binary にリンクされた package 初期化は §9 の Pointer 前後比較で評価する。

同じ binary は Pointer の package を含むので、`go list -deps` に Pointer package があること自体を違反にしない。静的な呼出監査と path 別計数を組み合わせる。profile に frame がないことだけを0回の証拠にしない。計数用 build を性能測定に流用しない。

## 3 ID と所有権を試験する

新しい `TestNativeIDSpace` 相当の試験で次の操作を実行する。通常 tag と `storechecks` の両方で確認する。

1. Program P を `NewIDSpace(0)` で作る。
2. source A、lazy JSDoc A、合成 import A、source B、lazy JSDoc B、合成 import B、source C をこの順に増やす。
3. 各段階で StoreID、NodeKey、foreign 辺、parent/file、links の値を保存する。
4. 最後に全ての値を再読込し、非衝突、不変、同じノードへの解決を確認する。
5. eager JSDoc を持つ source でも試す。別 Store を作る場合だけ新 ID を消費することを確認する。
6. 同じ source path を再取得し、同じ file と Node を返すことを確認する。
7. Program Q を別 IDSpace で作り、P の node を Q に渡すと既存の拒否契約で失敗することを確認する。
8. 固定予約 `NewIDSpace(nFiles)` の既存 fixture、空 fixture、ID 上限到達後の再呼出を試す。再発行や wraparound を許さない。

- [ ] SourceFiles の index と StoreID が異なる例でも graph と links が動くことを確認する。
- [ ] checker/printer の合成 Store を作った後も source の node が変わらないことを確認する。
- [ ] 診断と serializedTypes が保持する合成 node を、reporting 前に release、ゼロ埋め、再利用しないことを確認する。
- [ ] checker が1個であり、Global/Semantic 間で共有されることを計数する。statistics 起因の初期化は別イベントとして記録する。

## 4 native graph を独立比較する

共通入力は raw bytes、symlink、cwd、case sensitivity、roots、argv、config/options とする。この値から fresh FS、Host、Program を各側で別々に作る。Pointer の SourceFiles、解決結果、metadata、診断対象配列を Store に渡したら検証不成立とする。

| 必須 fixture 群 | 比較する観測値 |
| --- | --- |
| root/default lib/types、path/type/lib reference | roots、SourceFiles の順序、default lib 判定、欠落理由、include reasons |
| NodeNext と Node16 の import/require、bundler | 解決 mode、resolved file、package identity、拡張子、module metadata |
| paths/baseUrl、exports/imports、package dedup | 同じ specifier が同じ実ファイルへ解決すること。期待する path を固定 |
| 循環 import、symlink、casing | 無限探索なし、重複 file の扱い、canonical path、診断 |
| TSX/JSX runtime、importHelpers | 合成 specifier の有無、順序、text、kind、親、File、2回引いた同一性 |
| JS/JSDoc と JSON | JS 探索制限、JSDoc import、JSON source の Store parse、同じ graph |
| root/import/type/lib の欠落 | 解決失敗の診断と位置、Program の短絡、source 順序 |

- [ ] SourceFiles を比較前に path sort しない。元順序のまま比較する。
- [ ] 合成 import は Statements に含まれず、親が ImportDeclaration、その親が所属 source の root であることを確認する。
- [ ] C01 と L01 の独立 graph、roots、設定、target path と順序を保存する。diagnostics 以外の問い合わせに Pointer 委譲が残っていないことを調べる。

## 5 collector と診断表示を照合する

次の呼出順を mock callback のイベント列と実 Program の両方で確認する。診断の基準数は最初の config 診断数とする。

| 入力状態 | 期待する stage |
| --- | --- |
| syntax 診断あり | Config、Syntactic。Program/Bind/Global/Semantic を省く |
| syntax なし、program または最初の global 診断あり | Config、Syntactic、Program、Bind、Global。Semantic と2回目の Global を省く |
| syntax/program/global の追加診断なし | Config、Syntactic、Program、Bind、Global、Semantic、Global |
| config 診断だけあり | config 件数を基準に後続判定する。診断総数が0であることを条件にしない |

- [ ] statistics 無効の syntax error ケースでは、イベント列だけでなく実際の binder 呼出も0回であることを確認する。graph 構築直後の無条件 bind を見逃さない。
- [ ] config、parser、binder、checker 由来を混在させ、path/span/code/category/source、message key/args/text、chain、related information、SkippedOnNoEmit、不要・非推奨情報を比較する。
- [ ] sort 順と related information だけが異なる重複の統合を確認する。locale を変えても raw identity による整列規則を維持する。
- [ ] skipLibCheck、skipDefaultLibCheck、checkJs/ts-check、plain JS、JSDoc、ts-ignore、未使用/使用済み ts-expect-error を個別に通す。bind 診断の directive 処理も確認する。
- [ ] empty roots、欠落 global、syntax error、semantic error、check 後に増える global 診断を含める。panic を空診断に変換したら不合格にする。
- [ ] plain/pretty と quiet の全組合せで、診断本文、listFiles、summary、statistics の出力順を比較する。数値として変動する時間/メモリ欄だけを別比較にし、診断全文を広く正規化しない。
- [ ] statistics は要求時だけ呼び、2回の GC、memory snapshot、ProgramStatistics の順を確認する。構文エラー後の統計が checker 初期化を起こす場合も両側で一致させる。
- [ ] `Memory allocs` は累積 Mallocs、`Memory used` は snapshot の Alloc として照合する。区間の allocs/op、retained と読み替えない。
- [ ] 通常 noEmit の成功は0、コンパイル診断ありは2と確認する。`EmitSkipped` に基づく1、引数/config/非対応の EarlyExit はそれぞれの契約で確認する。

## 6 CLI 境界と副作用を試験する

実 binary と fresh temporary directory を使う。終了値、stdout/stderr、FS の書込み一覧を保存する。既存 config や外部 checkout を `--init` で上書きしない。

| 試験群 | 必須ケースと合格条件 |
| --- | --- |
| backend 選択 | 未指定、pointer、store。argv と response file の両方。未指定/pointer の既存挙動が同じ |
| 引数/config エラー | 不正値、値欠落、tsconfig 内 astBackend、extends 先の不正指定。既存 parser の診断と終了規則に従う |
| config と roots | `-p`、直接 root 指定、extends、include/exclude、空 references。両側の実効設定と graph が同じ |
| help/version/init/showConfig | 簡易 help は非表示、all には列挙値・既定値・制限。init は3 backend 指定で byte 一致して再読込可能。showConfig に astBackend が混入しない |
| Store 必須 option | noEmit なし、singleThreaded なし、矛盾する checkers。具体的な拒否理由があり source parse/check/write は0 |
| Store 非対応 option | declaration、emitDeclarationOnly、declarationMap、isolatedDeclarations、composite、incremental、watch、非空 references、content mapper、noCheck、listFilesOnly、explainFiles、generateTrace を1つずつ有効にする |
| 先行入口 | build、lsp、api の先行分岐と、Store 指定を含む response file。無視して Pointer 実行せず、理由を示して終了する |
| 表示 | listFiles、locale、plain/pretty、quiet、diagnostics、extendedDiagnostics。順序、summary、終了値が契約どおり |
| 書込み | check 成功・診断あり・拒否の各ケースで JS/d.ts/buildinfo が0。init の config と明示 profile だけは別許可として記録 |

対応範囲の検証は merged config に対して行う。拒否 fixture では parse、bind、check、write の計数が0であることを確認する。config JSON 読込みと source parse は区別する。正常な help/init/showConfig に check の制限を誤適用しない。

## 7 全母集団の等価性を確認する

母集団の正本は `tsc/testdata/tests/cases/compiler` と `conformance` の全 `.ts/.tsx`、その option variations である。`storeharness/configprep_test.go` の列挙・入力分解を再利用し、case/variation ID を保つ。

- [ ] `prepareCase` が補う SkipDefaultLibCheck、NoErrorTruncation、NewLine 等を実効 options に記録する。
- [ ] native check に共通の noEmit/singleThreaded を追加した派生 ID と設定差分を残す。declaration/composite 等を黙って false にして元 case の合格にしない。
- [ ] 各側は自分の graph から対象を選び、最終診断と graph と CLI 相当の結果を比較する。既存 harness の checker 生出力比較は別の回帰として維持する。
- [ ] filter/sample/shard、legacy skip、native 非対応、入力エラー、panic を manifest の独立 field に残す。両側の同じ panic も equal に数えない。
- [ ] 旧 `SkipUnsupportedCompilerOptions` が除外する baseUrl 等を、§4と§6の専用 fixture で補う。旧 equal 13,446 は今回の成功件数として引用しない。
- [ ] 最終実行では全対応構成を実行する。対応数、実行数、合格数が一致し、全列挙入力に状態があることを機械検査する。差分を除外規則の追加で消さない。

manifest は最低限、suite、caseId、variation、派生元、入力/tree/lib hash、symlink、cwd、case sensitivity、roots/argv、config と実効 options、全列挙数、入力エラー、非選択理由、legacy 結果、native 対応理由、各側の graph、最終診断、exit、source/checked 数、短絡理由、write、stdout/stderr、最初の差分位置を持つ。

新設 `internal/storeclitest` の受入用の予定コマンドは次の形にする。各 test が存在することと、JSONL に実行件数があることを先に確認する。既存の環境変数名を無断で新 runner の契約に流用せず、出力先等の実装済み interface を報告書に明記する。

```sh
cd /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc
GOWORK=off go test ./internal/storeclitest -list 'TestNative'
GOWORK=off go test ./internal/storeclitest -run '^(TestNativeCLIEquivalence|TestNativeCLIRejectsUnsupported|TestNativeComparatorSensitivity)$' -count=1
GOWORK=off go test ./internal/storeclitest -run '^TestNativeCheckerConfigurations$' -count=1 -timeout=120m
GOWORK=off go test -tags=storechecks ./internal/storeclitest -run '^TestNative' -count=1 -timeout=120m
```

## 8 比較器が故障を検出できるか確かめる

次の故障を1つずつ scratch overlay または比較器の入力変形で入れる。故障前は合格し、故障後は該当する観測差で失敗することを要求する。compile error や別原因の panic だけではその観測の感度を証明しない。

| 故障 | 検出すべき観測 |
| --- | --- |
| source B に source A の ID を再利用 | 非衝突・NodeKey・links の試験 |
| SourceFiles の2要素を交換 | graph の順序比較 |
| module の解決先または mode を変更 | resolved graph と期待 path |
| JSX/helper の合成 import を欠落させる | 有無と同一性。checker が問わなくても検出する |
| related information を1件落とす | 最終診断の field 比較と pretty 出力 |
| directive 処理を省く | 期待診断と unused expect-error |
| semantic を省いて空診断を返す | stage 計数、checked file 数、期待診断 |
| noEmit の診断ありを exit 1 にする | 実 CLI の終了値 |
| 拒否前に source parse、または buildinfo write を行う | path/役割別計数と FS write 記録 |
| Store から Pointer parse または Program を呼ぶ | 本番経路混入の計数試験 |

故障が検出されなければ、対応する検証を未完了とする。除外の追加ではなく比較器の不足として報告する。overlay を外して元の試験が再び通ることを確認する。

## 9 性能を独立に測定する

### 9.1 入力と環境を揃える

- [ ] 実装計画 §1.2 の C01、L01、D01、E01 をそのまま使う。L01 の rev、config、pnpm の実依存、electron typing と入力 closure を再照合する。
- [ ] 入力の graph、最終診断、対象 file と順序、source/checked 数が一致した後で timing を取る。semantic 診断ありの正常な check は有効な仕事として扱う。syntax/global error による短絡は Check の0 nsサンプルにしない。
- [ ] GOMAXPROCS=1、singleThreaded、checker 1個、通常 GC の GOGC=100、GOMEMLIMIT未設定、同じ Go version、build flags、lib、locale、cwd、FS cache の寿命を記録する。環境を変えた結果は別セットにする。
- [ ] OS file cache は warm 条件に固定する。cold 性能を主張しない。backend ごとに warm-up を2回行い、結果を保存して集計から除く。
- [ ] build、test、profile、indexing 等の負荷を記録し、測定中に自分の並列作業を動かさない。KPC と xctrace を同時に動かさない。

通常 CLI は同じ binary を両側で使う。標準 build の `npx hereby tsgo` は `noembed` と生成 lib を用いる。scratch に binary を移すときは同じ lib を配置し、default lib path と hash を検証する。KPC は `CGO_ENABLED=1 -tags=kperf` の専用 binary とし、その中の両 backend を同条件で比べる。CLI と KPC の別 build の数値を直接比にしない。

### 9.2 測定を分ける

| 測定名 | 区間と初期状態 | 必須指標 |
| --- | --- | --- |
| CLI end-to-end | 別子プロセスの起動から診断表示・終了まで。通常 GC、statistics/profile/計数なし | wall、CPU、peak RSS、stdout/stderr/exit |
| NativeCommandLine | 同じ execute.CommandLine。反復ごとに fresh System/FS cache/Program、同条件 writer | ns/op、B/op、allocs/op。起動費用を含まないと明記 |
| NativeProgram Build | config 確定後から graph 確定まで | ns/op、B/op、allocs/op。I/O/parse/解決/include を含む |
| NativeProgram Bind/Check | graph 構築後、bind と checker 作成・Program global/semantic を区分 | ns/op、B/op、allocs/op、stage 実行情報 |
| Diagnostic/report | directive/filter、sort/dedup、writer の帰属を明示 | 時間、割り当て。E01で診断経路を確認 |
| NativeCheckKPC | §9.3 の生 checker 区間 | cycles/op、inst/op、IPC、ns/op、B/op、allocs/op |
| Retained/GC | parse/bound/checked の同じ lifetime を別プロセスで保持 | HeapAlloc、baseline差、scan bytes、GC回数、mark CPU |

CLI wall の子プロセス runner の親側 B/op は記録しても compiler の欄に載せない。CLI の割り当て欄は「直接取得なし、NativeCommandLine を参照」とする。`--diagnostics` の2回GC、allocs profile、KPC のGC offを通常 timing に混ぜない。

Check の前に Program/options/global の問い合わせが checker 初期化を起こしていないかを計数用 build で確認する。初期化・skip/directive/filter がどこに計上されたかを表にし、未計上の仕事を残さない。旧表示の Parse time は Program 構築であり、純粋な parser 時間とは呼ばない。

### 9.3 NativeCheckKPC の境界を確認する

C01 の `BenchmarkNativeCheckKPC` は必須である。旧 Check と同じ仕事として、SkipDefaultLibCheck による default lib 除外を共通にする。CLI の追加 skip 条件を適用した別区間とは名前を分ける。

1. 独立等価試験で両 native Program の graph と対象 path/順序を照合する。
2. 各子プロセスでは選択した backend だけを構築する。Store 子プロセスに Pointer Program、H1、Fixture、録音済み解決を保持しない。
3. config、構築、parse、I/O、解決、全 source bind、対象選択は計測外にする。setup で global/semantic を問い合わせない。
4. timer と counter 内では `NewChecker` を1回、対象順の `GetDiagnostics`、最後の `GetGlobalDiagnostics` だけを行う。NewChecker 内の bind 済み判定も含める。
5. 診断の消費、期待結果照合、sort/dedup、表示、cleanup は区間外にする。
6. 各標本は fresh Program/fresh checker、`-benchtime=1x` 相当の独立プロセスにする。GC off の長い反復で確保を積み上げない。

既存 `internal/testutil/kperf/session.go` の counter 開閉、OS thread 固定、GC/GOMAXPROCS 制御を確認し、counter の前後と timer の前後を記録する。KPC は darwin/arm64、cgo、kperf tag と必要な権限を要求する。実行できない環境では別の測定可能な環境へ引き継ぐ。CLI timing で代替完了にしない。

予定 benchmark 名を `BenchmarkNativeCheckKPC/C01/backend=pointer` と `.../backend=store` に揃えた場合のコマンド例を示す。sudo は実環境の承認・実行手順に従い、対話入力を必要とする場合はこの具体的なコマンドを渡す。

```sh
cd /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc
GOWORK=off CGO_ENABLED=1 go test -tags=kperf -c -o /private/tmp/store-cli-native.kperf.test ./internal/storeclitest
sudo env GOMAXPROCS=1 /private/tmp/store-cli-native.kperf.test -test.run '^$' -test.bench '^BenchmarkNativeCheckKPC$/^C01$/^backend=pointer$' -test.benchtime=1x -test.count=1
sudo env GOMAXPROCS=1 /private/tmp/store-cli-native.kperf.test -test.run '^$' -test.bench '^BenchmarkNativeCheckKPC$/^C01$/^backend=store$' -test.benchtime=1x -test.count=1
```

入力 manifest の渡し方は実装済み runner の interface で固定し、各コマンドに記録する。上の単発実行だけでは受入を終えず、§9.5の順序・標本を runner で実行する。

### 9.4 retained と GC の定義を揃える

- [ ] backend と観測地点ごとに新しいプロセスを使い、Program 作成前と対象地点を観測する。parse の観測用 GC を済ませた Program を bound/checked 測定へ流用しない。config、診断、Program、checker 等の保持対象を一覧にし、backend 間で揃える。
- [ ] 各地点で GC を2回行い、2回目後の HeapAlloc を保存する。L01 の retained gate は checked 地点の総 HeapAlloc とする。Program 作成前の baseline を引いた差も別欄に保存する。
- [ ] 1回目と2回目の GC の間、2回目直後に runtime の mark CPU counters を読む。`/cpu/classes/gc/mark/assist:cpu-seconds`、`/cpu/classes/gc/mark/dedicated:cpu-seconds`、`/cpu/classes/gc/mark/idle:cpu-seconds` の合計差を、2回目強制 GC の mark CPU とする。metric の受け皿は事前確保し、GC回数と gctrace の該当 forced GC を照合する。これらは runtime の推定CPU値であり、OSのCPU時間と直接比較しない。
- [ ] 2回目 GC の終了後まで Program、checker、診断を `runtime.KeepAlive` 相当で保持する。途中 release、GC前の nil 化、片側だけの cache 保持を禁止する。
- [ ] gate 用強制 GC と別に、通常 GC の check 全体の mark CPU、GC回数、scan bytes、peak を取得する。profile の alloc_space/allocs を retained として使わない。
- [ ] Pointer の分母が0、測定分解能未満、未実行なら比を出さず未確定とする。都合のよい微小値を加算しない。L01 の gate に必要な値が取れなければ性能受入未完了にする。

### 9.5 順序と統計処理を固定する

次を事前に記録し、結果を見て選び直さない。

1. 同一 binary・同一 backend の A/A を backend ごとに10組取得する。通常 CLI、KPC、retained/GC を別々に校正する。L01 の gate 指標と Pointer 前後比較には同条件の A/A を必須にする。
2. A/B は10組で品質確認し、判定用には必ずさらに20組を取得する。合計30組の対応標本で最終判定する。最初の10組だけで有利な結果を採用しない。
3. 各組で Pointer/Store を別プロセスにし、奇数組は Pointer→Store、偶数組は Store→Pointer とする。Pointer 結線前後も before→after、after→before の順を交互にする。
4. warm-up は別保存する。クラッシュ、入力不一致、外部負荷で無効にする場合は事前の条件と時刻を残し、片側だけを捨てず組全体を無効とする。
5. raw benchmark 名と metric を保持し、backend の名前部分だけを正規化したファイルを作る。同じ仕事・区間ごとに `benchstat old.txt new.txt` を実行する。before/after も同じ方法にする。
6. gate の比は各組の比の中央値で推定する。組単位の bootstrap を20,000回、固定 seed `20261004` で行い、2.5/97.5 percentile を95%区間とする。resampling は対を崩さない。実装と version、raw pair ID を保存し、benchstat の差の検定とこの比の区間を別欄にする。
7. A/A の95%区間が `[0.98, 1.02]` に収まり、相対全幅 `(最大値−最小値)/中央値` がKPC cyclesでは2%以下であることを要求する。通常 CLI と retained/GC は区間条件を使い、外れ値を後から消して条件を満たさない。
8. A/A 不合格または gate 区間が線をまたぐ場合、環境を整えて同じ一式を1回だけ再実行できる。初回を破棄せず、再実行理由と両結果を併記する。有利な方だけを選ばない。2回目でも安定しない、または判定が食い違う場合は未確定とする。

本書の2%は測定品質の条件であり、最適化の最低改善率ではない。標本間の長期 drift や自己相関を記録し、bootstrap の区間がそれを解消するとはみなさない。測定条件が変わったセット同士を混ぜない。KPC の ns/op と通常 CLI の wall time も混ぜない。

### 9.6 合否と帰属を報告する

L01 の wall time、checked HeapAlloc、checked の強制 GC mark CPU の3比に、それぞれ上限0.70、0.80、0.80を適用する。

| 結果 | 判定 |
| --- | --- |
| 95%区間上端が上限以下 | その指標は合格 |
| 95%区間下端が上限を超える | 基準未達。Pointer より改善していても目標達成とは呼ばない |
| 区間が上限をまたぐ、A/A不合格、必要データ不足 | 未確定または未取得。合格に数えない |

「改善を確認」「差は未確定」「退行を確認」は、比1に対する別の評価として併記する。例えば wall 比0.85なら改善でも0.70の目標は未達である。有意差なしを同等の証明にしない。

Pointer 結線前後は、A/A の各組比について `|ratio−1|` の最大値をノイズ幅 δ として A/B 前に固定する。after/before 比の95%区間下端が `1+δ` を超えれば退行として修正対象にする。区間上端が `1+δ` 以下ならその測定条件で許容幅内と記録する。区間がまたぐ場合は未確定とし、§9.5の再実行後も残れば全体の受入を保留する。δ を Store の利益に合わせて拡大しない。

- [ ] CPU、alloc_space、alloc_objects、必要な heap profile を通常 timing と別実行で取得する。双方の hot paths と allocation drivers を記録し、native 経路の実費に帰属させる。
- [ ] Check のような広い stage が最重であっても、修正可能な accessor、解決、確保の狭い経路を分けて報告する。最小の追加 microbenchmark は疑う1経路だけを隔離する。
- [ ] 合成 benchmark は方向確認に限定する。実入力での費用削減やL01の完了を代用しない。
- [ ] 数値比較できない組合せは理由を示し、ratio を出さない。3%の一律採用線を追加しない。

### 9.7 artifact を保存する

測定セットごとに次の構成を保存する。名前は workload と区間が衝突しないようにする。

```text
metadata.json
input-manifest.json
source-snapshot.json
commands.jsonl
pointer/bench.original.txt
pointer/bench.txt
store/bench.original.txt
store/bench.txt
benchstat.txt
aa/pointer/bench.txt
aa/store/bench.txt
pairs.jsonl
ratio-intervals.json
cli/stdout/
cli/stderr/
cli/exits.jsonl
profiles/
retained/
```

metadata は `repo_root`、binary にコードを提供する実在 repository の root/rev、dirty diff/overlay hash、binary hash、Go/tool version、build tags、環境、backend、全コマンド、測定境界、warm-up、順序、cache 条件、source/checked 数を持つ。入力 repository の rev/hash と依存 lockfile は binary の source repository と別項目にする。

## 10 回帰を実行する

既存試験は次の入口を使う。新 package の試験も追加する。通常 tag と `storechecks` の両方で行い、`-short` は使わない。

```sh
cd /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc
GOWORK=off go test ./internal/tsoptions ./internal/execute/... -count=1
GOWORK=off go test -tags=storechecks ./internal/tsoptions ./internal/execute/... -count=1
GOWORK=off go test ./internal/ast/store ./internal/storeparser ./internal/storebinder ./internal/storechecker ./internal/storeharness -count=1 -timeout=120m
GOWORK=off go test -tags=storechecks ./internal/ast/store ./internal/storeparser ./internal/storebinder ./internal/storechecker ./internal/storeharness -count=1 -timeout=120m
GOWORK=off go build ./...
GOWORK=off go build -tags=storechecks ./...
GOWORK=off go vet ./internal/ast/... ./internal/store... ./internal/compiler/... ./internal/execute/... ./internal/tsoptions/...
GOWORK=off go vet -tags=storechecks ./internal/ast/... ./internal/store... ./internal/compiler/... ./internal/execute/... ./internal/tsoptions/...
```

旧 corpus runner は FILTER、SAMPLE、SHARDS、SHARD を解除して全量を回す。`STORE_CHECKER_CONFIG_REPORT=1` は mismatch で失敗しない報告モードなので使わない。

```sh
env -u STORE_CHECKER_CONFIG_REPORT -u STORE_CHECKER_CONFIG_FILTER -u STORE_CHECKER_CONFIG_SAMPLE -u STORE_CHECKER_CONFIG_SHARDS -u STORE_CHECKER_CONFIG_SHARD GOWORK=off STORE_CHECKER_CONFIG_WORKERS=1 STORE_CHECKER_CONFIG_OUT=/private/tmp/store-cli-legacy-configurations.jsonl go test ./internal/storeharness -run '^TestCheckerConfigurations$' -count=1 -timeout=120m
```

同じ全量試験を無理由に繰り返さない。package 全体の試験でこの runner が全量通過していれば、その raw と manifest を使う。失敗、変更、未解決の懸念がある範囲だけを追加実行する。

`TestCompilerProjectEquivalence`、`TestFixtureIdentityCompilerProject`、`TestFixtureIdentityConfigurations`、`TestModeCorpus`、`TestPrintOutputs` の実行結果も確認する。過去の既知失敗は同一入力で before/after を再現して帰属を残す。既知というラベルだけで今回の退行を除外しない。

生成器は実装計画 §7 のコマンドで確認する。新しい compiler manual の故障注入も含め、生成の成功、2回目の不変、元宣言変更の検出を別々に記録する。

## 11 報告と完了を判定する

`store-cli-verification-report-20261004.md` に以下を残す。

- [ ] 選択 repo、before/after、dirty diff と source hash、artifact set と4状態。
- [ ] AからEの受入表。各項目のコマンド、実行数、証拠path、不一致の再現。
- [ ] 母集団総数、legacy skip、native非対応、非選択、入力エラー、panic、対応・実行・合格数。
- [ ] CLI の診断全文と exit、graph、書込み、混入計数、故障注入の検出結果。
- [ ] C01/L01/D01/E01の各測定区間の ns/op、B/op、allocs/op、CLI wall/CPU/peak、native KPC、retained/GC、raw/benchstat/A/A/区間。
- [ ] hot paths、allocation drivers、広いstageと狭い修正対象、診断と次のaction。未取得指標は空欄にせず理由を書く。
- [ ] Pointer結線前後の回帰と、検証後にsourceが不変である証拠。

判定は次の4行を独立に書く。

1. 機能は合格、未完了、または不合格。
2. 性能の必須測定は完了、または未取得あり。
3. L01の3目標とPointer回帰は合格、未確定、または基準未達/退行。
4. 7e全体は上記を満たした場合だけ完了。Storeの既定採用は別判断。

機能だけが通過した場合は「機能完了、性能評価未完了」と報告する。性能が測れたが目標未達なら「機能完了、性能基準未達」とする。未確定を合格に繰り上げない。残件には問題、証拠、再現、仮説、修正案、受入条件を必ず含める。検証担当者は製品コードの修正へ進まない。
