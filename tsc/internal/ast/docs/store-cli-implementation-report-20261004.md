# Store CLI 7e 実装報告

2026-10-04〜05。対象は [実装計画指示書](store-cli-implementation-instructions-20261004.md)。証拠は `_store-cli-impl-20261004/`（ローカル、gitignore 対象）。

## 判定

| 項目 | 判定 |
| --- | --- |
| 機能 | 完了。A〜E の実装と試験は通過した。全母集団の対応 13,170 構成で Pointer と Store が一致し、不一致 0・panic 0 |
| 性能の必須測定 | C01 の native Check KPC は取得済み（2026-10-05、ユーザーが sudo で実行、A/A 合格）。E01 は未測定 |
| L01 の 3 目標 | 保持メモリは基準未達（A/A 合格）。wall は区間が上限を大きく超えるが A/A 不合格のため、規則上は未確定。mark CPU は区間が上限を大きく下回るが A/A 不合格のため、規則上は未確定。§9.5 の再実行は行わない（2026-10-05、ユーザー決定）ので、この判定で確定 |
| Pointer の結線前後 | 許容幅内。ただし δ=0.149 と大きく、検出力は弱い |
| 7e 全体 | 未完了（機能完了、性能基準未達。L01 の保持 heap が 0.80 を超え、wall と mark CPU は A/A 不合格で未確定） |

## commit

| commit | 単位 |
| --- | --- |
| `1ce32dab36` | 設計文書・指示書 3 本と、動的採番の契約（Y3・不変条件22・API コメント）。先行していた未コミット変更 |
| `b4d8e76349` | A: 生成境界と動的採番 |
| `85589261f1` | B: native loader と graph の比較 |
| `29b5e6a27d` | C・D: check と診断、CLI の結線（試験を共有するため 1 commit） |
| `145da728eb` | E: 全母集団の等価試験、benchmark、retained 計測 |

`store-checker-7d2d-verification-report-20261003.md` は作業開始時点から未コミットのまま残っている。今回の作業範囲外なので触れていない。

## A 生成境界と動的採番

- `tools/cmd/storetranslate`: `compilerRoots` を追加。root は `compiler.NewProgram` と、観測・診断・統計用のメソッドだけで、`ProgramLike` は root にしない。生成器の規則を 3 点直した。(1) P2・手書きの本体で selector 名から辿るメソッドを、受け手の型が到達済みのものに限る（modeFull は従来どおり）。(2) modeChanged の P2 本体が名指す非公開宣言を複製する。(3) `pkgSpec.shared` で `module.Resolver` と `NewResolver` を共有する。既存の 58 生成物は (1)〜(3) の前後で byte 一致した。
- `storecompiler`: compiler の loader（fileloader・filesparser・fileInclude・includeprocessor・processingDiagnostic・projectreferencefilemapper）と、Program のメソッド 40 個余りを生成した。手書きの P2 は ProgramOptions、Host、Program 型、content mapper・parse cache・project reference の切断、合成 import、GetSymlinkCache、sourceFileMayBeEmitted、診断群、verifyCompilerOptions。
- 動的採番: Program が `NewIDSpace(0)` を持ち、Host が parse 直前に `Next()` を 1 回呼ぶ。`TestNativeIDSpace` と `TestNativeIDSpaceProgram` を通常 tag と storechecks で実行した。故障注入（ID 0 の再利用）は検出された。
- Pointer の parse・bind・checker が 0 回であることを、overlay の計数で確認した（`a/count-pointer-work.out`）。config JSON の parse 1 回は別に集計した。
- 証拠: `a/README.md`。到達一覧、生成の再現、hash の検出、計数。

## B native loader

- 比較器 `storeclitest/graph.go` は、file の順、lib、metadata、合成 specifier、全解決、redirect、include reason、missing を照合する。fixture 10 件（循環、NodeNext、bundler+paths、JSON+JS、types/ref、JSX+helpers、symlink+dedup、casing、欠落、空 roots）で一致した。
- C01（124 file）、L01（9,788 file、233,747 行）、D01（249 file）の graph は byte 一致した。file の順は、結線前 CLI の `--listFiles` とも一致した。
- `storeharness/mode.go` の手書き判定を削除し、生成コードを正本にした（TestModeCorpus PASS）。
- 証拠: `b/README.md`。

## C・D check と診断、CLI

- `execute/tsc/check.go`: `CheckStages[D]` と `CollectCheckDiagnostics`（GetDiagnosticsOfAnyProgram の check 到達部分）、`CompilationReport` と `ReportCompilation`、`CheckEmit` を追加した。
- 診断: Store の parser・binder・config・resolver の診断は、`store.DiagnosticFromAST` で Store 診断に変換する。整列は各 backend の既存規則または生成規則を使い、新しい比較規則は書いていない。`ast.Diagnostic` に `Message()` accessor を 1 つ追加した。
- CLI: `--astBackend pointer|store` を追加した。未指定は旧経路のまま。pointer は check mode の範囲内なら共通の collector を通り、範囲外なら旧経路。store は範囲外を理由付きの `ExitStatusNotImplemented` にする（source を読む前）。build は既存の parser が TS5094 で拒否する。lsp と api は exit 5。init と showConfig からは除外した。help の並べ替えは安定 sort にした（同名の help 2 件の既存の不安定さを、option の追加が表に出したため）。help-all の baseline は 3 行追加のみ。
- 試験: TestNativeCLIEquivalence（20 fixture × 7 表示 flag × 3 backend）、TestNativeCLIRejectsUnsupported（15 条件、source の読込 0、書込み 0）、TestNativeCLIEntry、TestCollectCheckDiagnostics、TestSortAndDeduplicateDiagnostics、lsp/api。directive 処理を省く故障は検出された。
- 実 binary: L01 の診断全文と `--listFiles`（9,834 行）が 3 backend で一致した。結線後の未指定の出力は、結線前と一致した。
- 証拠: `cd/README.md`、`cd/regression.out`（両 tag の build・vet・test がすべて PASS）。

## E 等価と性能

### 全母集団

`TestNativeCheckerConfigurations`（`e/corpus/native-configurations.jsonl`）。12,730 file、15,205 構成、入力エラー 7。

| 状態 | 件数 |
| --- | ---: |
| equal | 13,170（exit 2 が 8,070、exit 0 が 5,100、書込み 0） |
| native-unsupported | 1,996（declaration 1,965、emitDeclarationOnly 163、isolatedDeclarations 27、contentMappers 15、declarationMap 15、composite 9、incremental 7、noCheck 3、listFilesOnly 2。重複あり） |
| legacy-skip | 39 |
| input-error | 7 |
| different・panic | 0 |

派生設定として noEmit=true、singleThreaded=true、listFiles=true を加え、元の値を `derived` に記録した。declaration などを黙って無効化して合格にはしていない。旧 runner の baseUrl などの除外は適用しておらず、全件を実行した。

### 測定条件

- binary は HEAD `145da728eb` を clean で build した（`e/binaries.sha256`）。noembed で、lib は結線前と同じ hash。
- GOMAXPROCS=1、`--singleThreaded`、GOGC と GOMEMLIMIT は未設定。M1 8GB、Go 1.27.1、warm cache。
- 順序は奇数組 pointer→store、偶数組 store→pointer。warm-up は backend ごとに 2 回。A/A は 10 組、A/B は 30 組。
- 区間は各組の比の中央値で、組単位の bootstrap 20,000 回、seed 20261004（`e/ratio_interval.py`）。
- load の待機条件は、ユーザーとの相談で 1.5×3 回から 2.5×1 回に変えた（load が 2〜3.6 で推移し続けたため）。各 run の load1 を記録した（A/B の中央値は 2.3〜2.4）。測定は 2026-10-04 22:19 から 10-05 00:46 まで（`e/queue.log`）。

### L01（ユーザー指定の gate）

| 指標 | A/A Pointer | A/A Store | A/B（Store/Pointer） | 上限 | 判定 |
| --- | --- | --- | --- | --- | --- |
| CLI wall | [0.910, 1.076] | [0.968, 1.033] | 0.864 [0.844, 0.879] | 0.70 | A/A 不合格のため未確定。区間の下端は上限を超える（参考: 基準未達相当）。比 1 に対しては改善 |
| checked の保持 heap | [1.0000, 1.0001] | [1.0000, 1.0001] | 0.8426 [0.8426, 0.8426] | 0.80 | 基準未達 |
| 強制 GC の mark CPU | [0.931, 1.074] | [0.941, 1.002] | 0.457 [0.438, 0.479] | 0.80 | A/A 不合格のため未確定。区間は上限を大きく下回る |

補足値（A/B、中央値）:

- wall: Pointer 24.99 秒、Store 21.26 秒。user の比 0.915、sys の比 0.588。
- peak footprint の比 0.829、maxRSS の比 1.00（8GB 機の物理メモリで頭打ち）。
- 通常 GC: mark CPU の比 0.624、GC 回数の比 1.13、scan heap の比 0.678。
- heap: Pointer 4,746 MB、Store 3,999 MB。強制 mark: 6.84 秒、3.10 秒。

A/A 不合格の原因は、8GB 機で約 4〜6GB を使う L01 のページング（sys）と、mark CPU の揺れにある。sys の A/A 相対幅は 0.65〜0.99。§9.5 の 1 回の再実行は、まだ使っていない。

### 結線前後の Pointer（L01、flag 未指定）

A/A（after）の wall の最大偏差 δ は 0.149。after/before は 0.9725 [0.945, 1.019] で、上端が 1+δ 以下なので許容幅内。footprint は 0.998 [0.993, 1.001]。

### C01・D01

| 測定 | 結果 |
| --- | --- |
| C01 CLI wall（A/B 10 組） | 0.911 [0.895, 0.945]。A/A Pointer [1.004, 1.033] |
| C01 footprint | 0.849 |
| D01 | 1 回 0.05 秒で、`time` の分解能以下。比は出さない |
| C01 benchstat（`e/c01-bench/`、in-process、n=10） | CommandLine −11.1% sec/op・−7.9% B/op・+2.0% allocs。Program の build −46.6%・allocs −69.5%、bind +17.5%・B/op +33.1%、check −7.9%・B/op +6.6%・allocs +6.4% |
| C01 native Check KPC（`e/c01-kpc/`、-benchtime=1x の独立プロセス、A/A 10 組×2・A/B 30 組） | A/A の cycles は Pointer [0.998, 1.001]・幅 0.58%、Store [0.998, 1.002]・幅 0.77% で合格。A/B（Store/Pointer）: cycles 1.0455 [1.0441, 1.0467]、inst 1.0774 [1.0770, 1.0779]、IPC 1.030、ns 1.0455、B/op 1.068、allocs 1.064。採用線は置かない。旧 harness 経由の Check（7d-2d）の cycles 1.0525 から、adapter が無くなった分だけ小さい。load1 の中央値は 3.61 |

### hot paths と allocation drivers（L01、`e/profiles/`）

- CPU: 両側とも最上位は GC の scan（tryDeferToSpanScan、typePointersOfUnchecked、scanObject）。Store で scanObject の累積は 2.21 秒から 1.56 秒に減った。checker の関数（instantiateSymbol、newSymbol、map access）はほぼ同等。
- alloc_space（allocs profile は最後の GC 時点で途切れる。memory の注意事項どおり）: Pointer は AST の slab（`slices.Grow` の node 型群）が上位。Store は `store.(*Builder).Finish`（42%）、binder の `declareSymbolEx`、loader の path 文字列。
- 診断: L01 の保持 heap は checker の型・symbol が大半を占め、両 backend で同じ量になる。AST の削減分は約 0.75GB（16%）しかないため、比は 0.84 で頭打ちになる。0.80 に届かせるには、AST 以外（checker 側）の削減が要る。wall は user 0.915 と sys 0.59 で、0.70 には GC とページング以外にも checker の CPU 削減が要る。

## 指示書から外れた判断と未完了

- C と D は 1 commit にした（試験を共有するため）。
- `getAdditionalJSSyntacticDiagnostics` は、Store に SubtreeFacts が無いため全走査にした。全母集団で差は出なかった。
- `verifyCompilerOptions` の emit path の検査は、noEmit 必須なので到達時に panic する。
- `--build --astBackend store` は、専用の拒否ではなく既存の TS5094（exit 1）にした（build の help に載せないため）。
- KPC は、ユーザーが load 3〜4 の状態で実行した。以前決めた load<1.5 の条件は満たしていないが、A/A は合格している。
- KPC の binary は、execute が引き込む FSEvents のために `CGO_LDFLAGS="-framework CoreFoundation -framework CoreServices"` が要る。
- 未完了: E01 の測定、比較器の感度試験の網羅（一部の故障注入のみ）。§9.5 の再実行は行わない（ユーザー決定）。

## 次の action

1. 保持メモリの 0.80 は、AST の変更だけでは届かない。checker の型と symbol の表現の見直しが要るので、別の設計判断として扱う。
