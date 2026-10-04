# Store CLI 7e 実装計画指示書

作成日 2026-10-04。対象は実装担当者。根拠は [CLI 設計書](store-cli-design-20261004.md) と、同日のユーザー指定による大規模入力の性能基準である。

同じ CLI binary に `--astBackend pointer|store` を追加し、Store の実ファイル探索から診断表示までを接続する。既定値は Pointer のままにする。P0 で入力と基準を固定し、A、B、C、D、E の順に実装する。各単位は変更、試験、証拠を一組にする。

## 0 本書を使う

- [ ] [検証指示書](store-cli-verification-instructions-20261004.md) を先に読む。単体試験だけで完了にせず、実ファイルを通した動作と性能の証拠も揃える。
- [ ] 本書のチェック欄は、その欄が指定するログ、差分、manifest が存在するときだけ完了にする。未実行を合格にしない。
- [ ] 文書作成時の選択 repo `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、HEAD `a16540cd8e828b53ad269130889bcf680debd2e4` と、実装開始時の tree を照合する。
- [ ] 既存の未コミット変更を保存する。設計文書3本と `ast/store/idspace.go`、`ast/store/store.go`、`storeparser/parser.go` には dynamic IDSpace 契約の先行変更がある。今回確認した Go の差分はコメント変更であり、native loader の実装完了を意味しない。
- [ ] 以下のコードパスは特記しない限り repo の `tsc/` からの相対パスとして読む。`tools/` は repo root 配下を指す。追加予定の symbol、test、benchmark は既存 API と区別する。

今回の成果物は指示書である。実装、benchmark の再実行、commit、PR 作成は行っていない。実装開始後は A から E を個別の変更単位として記録する。PR を使う場合もこの依存順にする。merge は本書で委任しない。

## 1 P0 で測定対象と判定を固定する

### 1.1 開始状態を保存する

- [ ] 実装用の証拠を `internal/ast/docs/_store-cli-impl-20261004/` に保存する。binary、大きな入力展開、overlay は scratch に置き、再作成手順と SHA-256 を証拠側に残す。
- [ ] HEAD、`git status --short`、追跡差分、未追跡の入力ソース、Go version、OS、CPU、実行環境を保存する。HEAD だけで dirty tree を識別しない。
- [ ] 既存 artifact を [検証指示書 §1](store-cli-verification-instructions-20261004.md#1-既存-artifact-を先に分類する) に従って分類する。旧 Check 計測から native CLI の性能を推定して合格にしない。
- [ ] 結線前の Pointer binary を同じ checkout path から作り、binary と lib を保存する。既存未コミット変更を消して baseline を作らない。後で同じ path に適用した overlay 等で再現できる source snapshot を保存する。
- [ ] 既存 CLI の plain、pretty、quiet、listFiles、statistics、syntax/global/semantic error の出力を保存する。C と D の characterization test の期待値にする。

### 1.2 入力を固定する

性能入力を次の4群に固定する。L01 だけをユーザー指定の大規模性能 gate の対象とする。C01、D01、E01 の結果も必須だが、L01 を代替しない。

| ID | 入力と設定 | 実装開始時に残す証拠 |
| --- | --- | --- |
| C01 | 選択 repo の `testdata/fixtures/compiler/tsconfig.json` を継承する semantic-check config | 元 config、派生 config、roots、全実効 options、解決後 file 集合 |
| L01 | `/Volumes/SanDisk1TB/ghq/github.com/microsoft/vscode` の rev `22f76d2e5fbf22b786986e2f7debf7446af7b4ad`、`src/tsconfig.json` を継承する semantic-check config | VS Code 入力 rev、設定 hash、実際の依存物、electron typing、全 graph。大規模 gate の正本 |
| D01 | L01 と同じ入力 checkout の `src/tsconfig.vscode-proposed-dts.json` を継承し `skipLibCheck=false`、`skipDefaultLibCheck=false` とする | 実在する宣言入力を check した file 数と診断。宣言生成は行わない |
| E01 | `testdata/tests/cases/compiler/unusedLocalsAndParametersTypeAliases2.ts`、`testdata/tests/cases/conformance/jsdoc/checkJsdocTypeTag4.ts`、`testdata/tests/cases/conformance/jsx/checkJsxChildrenProperty2.tsx`、`testdata/tests/cases/compiler/checkJsxNotSetError.ts`。syntax 短絡は `testdata/tests/cases/conformance/jsx/jsxParsingError1.tsx` を別 ID にする | `prepareCase` 相当で展開した raw file bytes、元 variation と派生 ID、TS/JS/TSX/JSX の診断、check 実行有無 |

C01、L01、D01 の派生 config は、元 config と同じディレクトリに置くか、同じ virtual path の FS overlay で供給する。別ディレクトリへのコピーで相対パスの意味を変えない。外部 checkout へ書けない場合は入力値を保持した scratch FS を使い、両 backend の path と cwd を揃える。

派生 config 名は、C01 が `testdata/fixtures/compiler/tsconfig.store-cli-c01.json`、L01 が VS Code の `src/tsconfig.store-cli-l01.json`、D01 が同じ `src/tsconfig.store-cli-d01.json` とする。実 CLI では同じ cwd から同じ `-p`、`--noEmit --singleThreaded --pretty false` を指定し、`--astBackend pointer` と `--astBackend store` だけを切り替える。

- [ ] C01 は `noEmit=true`、`singleThreaded=true` とし、継承する `composite`、`declaration`、`declarationMap`、`emitDeclarationOnly`、`isolatedDeclarations`、`incremental` を明示的に false にする。それ以外の roots、include、module、target、strict、lib、skipLibCheck を維持する。
- [ ] L01 と D01 は `noEmit=true`、`singleThreaded=true` を共通設定に追加する。測定時は `pretty=false`、quiet は無効、statistics/profile は無効にする。D01 の skip 設定以外を都合よく変更しない。
- [ ] E01 の多重 file 入力は `@filename` を分解する。元の `.ts` テスト記述をそのまま CLI の1ソースとして渡さない。React の補助 `.lib` を含めて入力を記録する。
- [ ] L01 の `src/tsconfig.json` と `src/tsconfig.base.json` には、文書作成時の静的確認では project references、composite、declaration、incremental の有効指定がない。ただし native loader での完走は未検証である。最終実効設定と依存物を開始時に監査する。
- [ ] L01 の依存物は pnpm の実インストールを使う。`package-lock.json` だけから同一性を主張しない。付録Aの hash を再確認し、`node_modules` の symlink と実際に読む source、package.json、設定、lib の hash を manifest に固定する。
- [ ] P0 で入力 snapshot を固定する。B の完了時に各側で独立に探索した graph を照合し、実際に読んだ入力の和集合を manifest に追記する。片側でしか読まなかったファイルや resolver の負の探索も差として調べる。Pointer の解決結果を Store に与えない。
- [ ] L01 が初版の対応範囲で成立しない場合は、欠ける機能と再現を報告する。references の削除、roots の削減、依存診断の隠蔽で同じ L01 の合格を作らない。

入力の rev と config は本書で選択済みである。P0 の入力 snapshot に、BからDで解決後 manifest、診断件数、checked file 数を加え、Eの性能実測前に確定する。これらの実行結果は文書作成時点では未取得である。設定や依存物が変わったら別の入力版にする。

### 1.3 性能の判定を固定する

2026-10-04 のユーザー指定を、設計書 §8.3 の候補A/Bに優先する。比は同一仕事の Store 値を Pointer 値で割る。

| 対象 | 完了に必要な条件 |
| --- | --- |
| L01 の通常 GC CLI end-to-end | wall time 比の95%区間上端が **0.70 以下** |
| L01 の checked 後保持メモリ | 同じ lifetime と強制 GC 後の retained bytes 比の95%区間上端が **0.80 以下** |
| L01 の checked 後 mark CPU | 同じ条件の強制 GC の mark CPU 比の95%区間上端が **0.80 以下** |
| C01 の native Check KPC | cycles/op、inst/op、IPC、ns/op、B/op、allocs/op と A/A、A/B を必ず取得する。旧 cycles 比 1.10 は新経路の合格線として継承しない |
| Pointer の結線前後 | 同じ入力で挙動と費用を比較する。A/A の幅を超える再現した退行を残したまま全体を合格にしない。Store の利益で相殺しない |

mark CPU の gate は checked の保持状態に対する強制 GC とする。通常 GC の mark CPU、GC 回数、peak RSS、scan bytes、parse/bound 地点の retained も別結果として報告する。強制 GC の値で通常 GC の wall time を置き換えない。

native Check KPC、C01、D01、E01 には新たな数値の採用線を置かない。測定と退行の帰属を必須にし、L01 の3条件を7eの性能受入に使う。ここは本指示書での評価方法の選択であり、旧候補値をユーザーの承認値として扱わない。Pointer に未解決の退行があれば全体の受入を保留する。

標本数、区間推定、A/A、再測定上限、ゼロ値の扱いは [検証指示書 §9](store-cli-verification-instructions-20261004.md#9-性能を独立に測定する) に固定する。区間が基準をまたぐ場合は未確定とし、追加測定後も未確定なら性能受入を完了にしない。数値基準は結果を見て変更しない。

## 2 A で生成境界と動的採番を実装する

依存は P0。編集範囲は `tools/cmd/storetranslate/`、`internal/ast/store/`、`internal/storeparser/` の必要箇所、新設 `internal/storecompiler/`、関連する設計・試験である。

- [ ] `storetranslate/pkgs.go`、`reach.go`、`run.go` を読み、compiler の狭い root 宣言と生成方式を追加する。`compiler.ProgramLike` 全体を root にしない。`modeFull` による全 type の出力を避け、到達 interface の全 method 展開も調べる。
- [ ] 最小 root と import 1 本を対象に、生成対象の一覧と対象外への到達を出す。config の Pointer JSON AST と対象ソースの Store AST が混在する箇所を記録する。特に `includeprocessor.go` の config 診断境界を分ける。
- [ ] `Program`、Store Host、source 作成、単一 checker の所有、診断取り込みを手書きの境界にする。探索規則と AST を読む規則は型に基づいて変換する。大規模 generic 化へ広げない。
- [ ] `*_manual.go` の replacement には元宣言の `storetranslate:pointer-hash` を付ける。複数宣言を移植したら全依存を記録する。独自 helper は既存 `store-only` 規約に従う。生成ファイルを手で直さない。
- [ ] Program は `NewIDSpace(0)` を所有する。Host が source を初めて parse する直前に `Next()` を1回だけ呼ぶ。再取得では同じ file を返し、探索 index を StoreID に変換しない。
- [ ] `SourceFiles` の順序を ID から独立に保持する。`SourceFiles()[StoreID]`、`StoreID < sourceCount`、source ID の連続性に依存したコードを検索し、実際の前提を監査する。links の `[StoreID][page]` は種類判定と区別し、拡張時の同一性を確かめる。
- [ ] 固定入力の `NewIDSpace(nFiles)` と予約済み source ID を維持する。動的 Program と固定予約方式を途中で混ぜない。ID の再発行、再採番、Program をまたぐ AST cache を導入しない。
- [ ] `source A → lazy JSDoc A → synthetic A → source B → lazy JSDoc B → synthetic B → source C` を構築する試験を追加する。各時点の ID、NodeKey、foreign 辺、links を最後まで保持して照合する。eager JSDoc、別 Program 拒否、ID exhaustion も確認する。
- [ ] Y3、不変条件22、実行・region設計、API コメントを照合する。先行変更済みの記述を古い固定採番へ戻さない。

A の完了証拠は、import 1 本の Store native graph、Pointer source parse 0 の計数、通常 tag と `storechecks` の動的・固定採番試験、生成再現性、元宣言変更の hash 検出である。変換が未対応の構文で停止したら最小再現と必要な変換規則を残し、その範囲だけを直す。

## 3 B で Store native loader を完成させる

依存は A。主対象は `compiler/{fileloader,filesparser,fileInclude,includeprocessor,processingDiagnostic}.go` から生成・移植する check 到達部分と Store Host である。

- [ ] FS、cwd、default lib、config cache、trace を受ける Store Host を作る。Pointer を返す `compiler.CompilerHost.GetSourceFile` の埋め込みで代用しない。
- [ ] root、default lib、types、imports、module augmentations、path/type/lib reference を読み込む。TS、TSX、JS、JSX、宣言ファイル、プログラムに入る JSON を Store parser で読む。
- [ ] `module.Resolver` と AST 非依存型を共有し、mode、NodeNext/Node16、bundler、exports/imports、paths/baseUrl、package dedup、symlink、casing、JS の探索制限を移植する。
- [ ] fileloader の結果順を維持する。子 task の収集順と lib の優先度を調べ、path sort や ID 順に置き換えない。roots、SourceFiles、metadata、resolved modules/types、include reasons を観測できるようにする。
- [ ] 欠落 root/import/type/lib、casing、config の include 診断を保持する。nil や空結果で未実装の問い合わせを成功させない。
- [ ] `storeharness/mode.go` のノード依存部分を本番側へ移し、harness が正本を呼ぶようにする。Pointer metadata に委譲する adapter 部分を本番へ持ち込まない。
- [ ] JSX runtime と import helpers は `store.NewSyntheticImport` を使う。同じ file と役割で同じ Node を返す。条件、順序、text、parent、File を Pointer と揃え、Statements へ追加しない。
- [ ] 入力値から両側を独立構築する graph 比較 runner を追加する。Pointer の SourceFiles、解決結果、metadata、対象 file 配列は共有しない。

B の完了証拠は、循環、NodeNext、bundler、JSON、types、symlink、欠落を含む専用 fixture の graph 一致と、対象ソース/lib の Pointer parse/bind/check 0 の計数である。config JSON の Pointer parse は別集計にする。C01 と L01 の graph と対応範囲をこの段階で照合する。

## 4 C で check と診断の契約を揃える

依存は B。編集範囲は Store Program、診断 view、新しい共通 collector/reporter、既存 `compiler/program.go` と `execute/tsc/` の必要最小限の抽出である。

- [ ] 変更前の `compiler.GetDiagnosticsOfAnyProgram` を characterization test で固定する。config 診断の基準数、syntax 短絡、program 診断後の bind/global、semantic 後の global 再取得を観測する。
- [ ] graph 確定後、collector の Bind 段階で bind し、単一 checker を Program が所有する。構築直後に無条件で bind して syntax 短絡を破らない。KPC setup の明示 bind と statistics 起因の初期化は別の規則として扱う。Global と Semantic の問い合わせで同じ checker を使い、ファイルごとの再生成をしない。
- [ ] skipLibCheck、skipDefaultLibCheck、checkJs/ts-check、plain JS、JSDoc、directives、unused expect-error、SkippedOnNoEmit、include 診断を Program の集計へ移植する。bind 診断を directive 処理より前に独立表示しない。
- [ ] 元の `ast.Diagnostic` または `store.Diagnostic` と file override を保持する `DiagnosticView` を追加する。message identity/args、chain、related information、source、category、不要・非推奨情報、repopulate 情報を保持する。
- [ ] `ast.CompareDiagnostics` と `compiler.SortAndDeduplicateDiagnostics` の規則を共通 helper または変換器で管理する。文字列にしてから sort しない。related information の統合と config/source 混在を試験する。
- [ ] `CheckStages` と `CollectCheckDiagnostics` を追加する。callback は値を返すだけにする。syntax があれば Program/Bind/Global/Semantic を省く。syntax がなければ Program と Bind と Global を行い、累積診断数が config 基準数と同じ場合だけ Semantic と2回目の Global を呼ぶ。
- [ ] `CompilationReport`、`ProgramStatistics`、`ReportCompilation` を追加する。診断表示、listFiles、summary、total time、statistics、終了コードをこの順に一か所で扱う。AST と Program を reporter に渡さない。
- [ ] quiet は個別診断を抑止する既存挙動を維持する。pretty による summary、listFiles、statistics をまとめて捨てない。
- [ ] statistics 指定時だけ GC を2回行い、memory snapshot の後で `Statistics()` を呼ぶ。Program と checker は reporting と GC が終わるまで保持する。統計が起こす checker 初期化を semantic 実行と区別する。
- [ ] 通常の noEmit は `EmitSkipped=false` を維持する。診断なしは0、診断ありは2を返す。`EarlyExit` は既存の終了値を維持し、余分な summary/statistics を出さない。
- [ ] 同じ対応範囲の Pointer one-shot check も共通 collector/reporter に接続する。emit、declaration、incremental、content mapper の経路を限定 collector へ押し込まない。既存 `EmitInput`、`ProgramLike`、`OnProgram` を Store に偽装実装させない。

C の完了証拠は、最終診断と表示・終了値の一致、collector の呼出順、checker の寿命、混在診断の整列・統合、通常 tag と `storechecks` の通過である。checker の生診断比較だけで完了にしない。

## 5 D で CLI を接続する

依存は C。対象は `tsoptions`、`execute/tsc.go`、`execute/tsc/{init,statistics,emit}.go`、`cmd/tsc/main.go` の先行分岐、CLI 試験である。

- [ ] `astBackend` を CLI 専用の列挙値として既存 parser に登録する。既定値は pointer、値は pointer/store。通常 argv と response file の同じ展開を使い、main に文字列抜き取りパーサーを追加しない。
- [ ] 不正値、値欠落、tsconfig 内指定を既存の引数/config 診断規則で拒否する。簡易 help では非表示、`--all` では値、既定値、実験用表示、初版の制限を表示する。
- [ ] `generateTSConfig` と `showconfig.go` の出力から astBackend を明示的に除外する。他の CLI-only option の整理を混ぜない。
- [ ] 引数エラー、init、version、help/all、config 読込み、showConfig の順序を維持し、その後に Store check の対応範囲を検証する。init/version/help/showConfig に noEmit/singleThreaded を要求しない。
- [ ] 最終的な merged options に `noEmit=true` と明示的な `singleThreaded=true` を要求する。checker は1個とし、矛盾する checkers 指定を拒否する。
- [ ] declaration、emitDeclarationOnly、declarationMap、isolatedDeclarations、composite、incremental、watch、build、非空 project references、content mapper、noCheck、listFilesOnly、explainFiles、generateTrace を有効時に拒否する。空 references は許可する。
- [ ] 拒否は source の parse、bind、check、書込みより前に行う。具体的な理由と `ExitStatusNotImplemented` を返し、Pointer fallback を実装しない。不正引数/config の既存終了値とは分ける。
- [ ] `CommandLine` 冒頭の build 分岐と main の lsp/api 分岐にも Store 指定の扱いを設ける。response file を含め、Store 指定を黙って無視しない。共通 parser の処理を利用する。
- [ ] `performStoreCheck` から Store Program を構築し、C の collector/reporter に渡す。Pointer 未指定/明示の経路では Store Program、IDSpace、checker を作らない。
- [ ] listFiles、locale、plain/pretty、quiet、diagnostics、extendedDiagnostics、pprofDir を接続する。check では JS、d.ts、buildinfo を一切書かない。init の config と明示された profile 出力は別の許可された出力として試験する。

D の完了証拠は、同一 binary で backend だけを変えた2本の CLI 実行、未指定と pointer の回帰、全拒否条件の副作用0、init の byte 一致、showConfig の非混入である。flag が表示された時点を完成にしない。

## 6 E で等価試験と性能測定を完成させる

依存は D。主対象は新設 native 比較 runner、benchmark、測定 runner、artifact manifest、報告書である。本番 package から `storeharness` を import しない。

- [ ] `storeharness/configprep_test.go` の列挙・入力分解を再利用する。独立比較用の試験 package は `internal/storeclitest` とする案を採る。各側に fresh FS、Host、Program を渡す。
- [ ] `testdata/tests/cases/compiler` と `testdata/tests/cases/conformance` の全 `.ts/.tsx` と option variations を母集団にする。native 用の noEmit/singleThreaded 追加は派生 ID として記録する。legacy skip と native 非対応を分け、全対応構成を最終受入で実行する。
- [ ] 既存 runner が除外する baseUrl 等を raw tsconfig/argv の CLI 専用 fixture で補う。単なる両側一致に加え、期待する file/path/diagnostic を固定する。
- [ ] `BenchmarkNativeCheckKPC` を追加する。setup は各 backend の native Program の構築、bind、対象選択まで。計測は NewChecker 1回、同順の GetDiagnostics、最後の GetGlobalDiagnostics に限定する。H1、Fixture、記録済み解決を使わない。
- [ ] `BenchmarkNativeCommandLine` を追加し、同じ `execute.CommandLine` に反復ごとに新しい System、FS cache、Program と同条件の writer を渡す。ns/op、B/op、allocs/op を報告する。
- [ ] `BenchmarkNativeProgram` に構築、Bind、Program 経由 Check の区間を設ける。setup で Global/Semantic を呼んで checker 初期化を逃がさない。後処理と report の帰属を区間一覧に残す。
- [ ] 実 CLI の子プロセス runner を追加する。wall、CPU、peak RSS、stdout/stderr、exit、check 実行状態を保存する。親 runner の B/op を compiler の値として報告しない。
- [ ] retained/GC の専用 runner は1プロセスで片側だけを保持する。parse、bound、checked の各地点で同じ保持対象と GC 条件を使い、runtime.KeepAlive 相当で寿命を保証する。
- [ ] 各 runner は regex が0件、入力が非対応、check 未実行、panic、入力不一致を成功サンプルから除外し、その理由を保存する。除外したまま必要な測定を完了扱いにしない。
- [ ] [検証指示書 §9](store-cli-verification-instructions-20261004.md#9-性能を独立に測定する) の A/A、A/B、benchstat、区間推定、profile、Pointer 結線前後を実施する。機能検証・build・profile と timing を同時実行しない。

E の完了証拠は、全母集団 manifest と全対応構成の合格、必須測定の raw と benchstat、L01 の3条件、Pointer 回帰評価、hot paths と allocation drivers の帰属である。KPC が実行できなければ性能評価未完了とする。

## 7 生成と回帰を確認して引き渡す

以下は既存のコマンドである。module ごとに実行し、測定 build では `GOWORK=off` を固定する。新 package と新 test のコマンドは検証指示書の予定名に従って追加する。Go 1.27 と既存依存物を用い、検証のために go.mod/go.sum を更新しない。

```sh
cd /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript
node tools/scripts/tsc/generate.ts

cd /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tools
GOWORK=off go test ./cmd/storetranslate
GOWORK=off go run ./cmd/storetranslate

cd /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc
GOWORK=off go test ./internal/ast/store ./internal/storeparser ./internal/storebinder ./internal/storechecker
GOWORK=off go test -tags=storechecks ./internal/ast/store ./internal/storeparser ./internal/storebinder ./internal/storechecker
GOWORK=off go test ./internal/tsoptions ./internal/execute/...
GOWORK=off go test -tags=storechecks ./internal/tsoptions ./internal/execute/...
GOWORK=off go build ./...
GOWORK=off go build -tags=storechecks ./...
GOWORK=off go vet ./internal/ast/... ./internal/store... ./internal/compiler/... ./internal/execute/... ./internal/tsoptions/...
GOWORK=off go vet -tags=storechecks ./internal/ast/... ./internal/store... ./internal/compiler/... ./internal/execute/... ./internal/tsoptions/...
```

- [ ] AST generator と storetranslate を再実行し、生成物の開始前後 hash が一致することを確認する。`STORETRANSLATE_FORCE` は設定しない。
- [ ] 既存の checker 等価、fixture identity、mode、print の回帰も実行する。既知失敗という過去の記述だけでは除外せず、同じ入力の before/after を記録する。
- [ ] `store-cli-implementation-report-20261004.md` に、AからEの変更、実行したコマンド、証拠パス、未完了を記録する。測定結果は選択 repo、artifact set、ns/op、B/op、allocs/op、hot paths、allocation drivers、診断、次の action を含める。
- [ ] 機能完了、性能合格・未確定・基準未達、既定採用の判断を分ける。L01 の3条件を満たすまで7e全体を完了としない。既定採用は本書の範囲外である。
- [ ] L01 の目標が未達なら、区間と実入力から原因を特定する。loader 接続に無関係な微小最適化を混ぜず、問題・証拠・再現・仮説・修正案・受入条件を持つ追補にする。

## 付録A 入力と既存証拠の確認記録

文書作成時は読み取りのみで確認した。native compiler の変換 prototype、Store graph、CLI 等価、性能試験は未実行である。A の最小構築は実装開始後の最初の技術検証とする。

VS Code の追跡ファイルには変更がなく、未追跡の pnpm 設定、profile、trace が存在した。次は開始時の照合用 SHA-256 であり、実際の解決入力全体の hash を代替しない。

| VS Code root からの path | SHA-256 |
| --- | --- |
| `src/tsconfig.json` | `6c35a05a6798601b37cd28c535f4e7258ffdf34e271fbf6a4411d328f1db2332` |
| `src/tsconfig.base.json` | `74359c514fb10624b9871279dd094fdd8034d4f3e37b154b5b98726db361c8c4` |
| `package.json` | `331bc70f3ada63a5ed5b97837edcc6d87511cc411cd206c057335bbfa4b42cdd` |
| `package-lock.json` | `2199e0769a0d07fbcae810dd95f66559520bce2f3e1321776894922b6aa810ea` |
| `pnpm-lock.yaml` と `node_modules/.pnpm/lock.yaml` | `ba768c49beac6f06168190dd6545fff5fbc073888c617f11b6fad6611f539391` |
| `pnpm-workspace.yaml` | `5f035fe8ddaff11ac4a57131f98007a0900e09e598f686560937135a5a9ce1d2` |
| `node_modules/.modules.yaml` | `831ab977b01fd27b7ec211ab058e1e883178694a2986ddb1797ba00449f1e296` |
| `.build/typings/electron.d.ts` | `24bd380cc4aec824e8c7e8b18bdfcb8a4078d522fc607645b38774dac094da41` |

## 付録B 境界と参照先

設計書で不採用の harness 経由 CLI、checker だけの切替、Program 全体の generic 化、別 binary は再導入しない。主なリスクは compiler 宣言の到達範囲、config JSON と source AST の混在、動的 ID の添字前提、L01 の依存物と初版対応範囲、同一 binary に Store をリンクする Pointer 費用である。A、B、E の証拠で順に確認する。

- [CLI 設計書](store-cli-design-20261004.md)
- [検証指示書](store-cli-verification-instructions-20261004.md)
- [checker 親設計](store-checker-design-20260923.md)
- [実行設計](store-checker-execution-design-20260924.md)
- [region 設計](store-checker-regions-design-20260924.md)
- [前段の実装報告](store-checker-7d2d-implementation-report-20261003.md)
- [既存 Check の測定境界](../../storeharness/measure_test.go)

作成時は pstack の multi-phase-plan の分割・証拠・独立調査を適用し、既存の日本語指示書形式に合わせた。PR 自動運転、goal、定期監視、10台のUI試験、画面録画は今回の文書作成には適用しない。実 CLI は検証指示書の stdout/stderr/exit とファイル副作用で確認する。将来PRで実装を分割する場合の実行 playbook は `playbooks/autopilot-stack.md` とし、起動や merge の許可を本書から推定しない。

付属の `check-plan.mjs` は英語のPR自動運転テンプレート専用であり、本書への実行結果は `0 PR sections, 4 problems` だった。4件は専用の3見出しとPR節の欠如で、本書形式への適用外として扱った。参照パス、コードブロック、受入条件の対応、2文書の整合は別に確認する。
