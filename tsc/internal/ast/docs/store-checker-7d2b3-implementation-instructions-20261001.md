# Store checker 全構成への接続と分類 (TODO 7d-2b-3) 作業指示書

作成日: 2026-10-01。対象は実装を担当するエージェント。2026-10-01 改訂: §3.3 の診断対象から、`SkipDefaultLibCheck` のときの default lib を除いた (ユーザー)。

## 0. 目的と到達点

compiler / conformance のテストを **仮想ファイル単位ではなく、テスト × 構成の program 単位**で Store checker の比較へ接続する。全構成の一覧を作り、接続できた構成を比較し、差と未対応部分を分類する。

入力の拡大と、新しく見つかった意味の差の修正を同じ段で行わない (2026-10-01、ユーザー)。本段ではハーネスを作り、ハーネスの誤りは直す。Store の API・checker・変換規則の新しい修正は、分類を見て次に決める。**本段の完了は 7d-2b 全体の完了ではない。** 全構成の等価という最終条件を、比較できた構成だけの等価へ変更しない。

| 前提 | 文書 |
| --- | --- |
| H1・Q1、所有権の不変条件 | [7d 設計](store-checker-design-20260923.md) §2.1・§2.7・§3 |
| 2b 全体の完了条件、NB1′ | [残りの設計](store-checker-7d2-design-remainder-20260929.md) §4 |
| private 名・symbol・ID | [レビュー契約](store-checker-review-contracts-20260926.md) §2 |
| 段 1 の実装 | [2b-2 実装報告](store-checker-7d2b2-implementation-report-20261001.md) |
| 段 1 の検証 | [2b-2 検証報告](store-checker-7d2b2-verification-report-20261001.md) |
| 本段の検証 | [検証指示書](store-checker-7d2b3-verification-instructions-20261001.md) |

before は **c5969e29e53**。開始時の HEAD と差分も記録する。段 1 は strict で 17,415 入力中 17,405 equal、合成 import 8、Pointer 非決定 1、Pointer panic 1、root 不在 0。これは過去の測定値であり、段 2 の期待件数ではない。

本書が指定するハーネスの構成は今回の作業方針。合成 import の所有権など、下記の未決事項をユーザーが決定済みであるかのように扱わない。commit・性能計測・2c は行わない。

## 1. 変更範囲

変更してよいのは `tsc/internal/storeharness/` と `tsc/internal/ast/docs/`。既存の比較契約を保った driver の入口の追加・共通化、構成の準備、テスト、報告を含む。

Pointer の package (testrunner・testutil の test file を含む)、store の production API、storechecker の P2、生成器・変換器・生成物、go.mod / go.sum は変更しない。production package から Store 系への import を追加しない。既存の段 1 テストの入力・strict の既定・比較項目・除外条件を緩めない。

独立照合のための testrunner 内部への probe は scratch の複製または Go overlay だけで行い、追跡する変更に含めない。後述の未対応を隠すために共有 API を書き換えない。ハーネス内でも未決の所有権変更はしない。

## 2. 入力一覧を先に確定する

参照元は `tsc/internal/testrunner/compiler_runner.go`、`test_case_parser.go`、`tsc/internal/testutil/harnessutil/harnessutil.go`。行番号ではなく、開始時の実装を読む。

1. `NewCompilerBaselineRunner` / `EnumerateTestFiles` と同じ compiler・conformance の全テストを列挙する。`RunTests` は baseline の削除を伴うため、列挙には使わない。
2. `extractCompilerSettings` と `compilerVaryBy` の意味を再現し、公開の `GetFileBasedTestConfigurations` を利用する。構成が空なら nil 構成を 1 回作る。boolean / enum、カンマ、`*`、除外、重複除去、組合せ数上限を元の挙動どおり扱う。
3. runner の `skippedTests` と `SkipUnsupportedCompilerOptions` に相当する適用結果も一覧に残す。runner 由来の skip、option の fatal、準備失敗を区別する。元の reason と根拠を記録する。全構成数を先に数え、skip したものを分母から消さない。
4. `suite + 相対 test path + 正規化した構成 key/value` を基に一意な case ID を作る。同一 key が残る場合は決定的な出現番号を加える。表示用の configurationName だけを key にしない。全一覧に安定した index を振ってから filter / sample / shard を適用する。
5. manifest を保存する。case ID、index、元ファイル、構成、入力内容の hash、処理対象かとその理由を持たせる。順序が意味を持つ roots・units・配列 option は canonical 化で並べ替えない。

非公開の準備処理を storeharness に写す場合は、元の関数と revision を comment に残す。写し同士の比較では検証にならない。scratch の testrunner 内部 probe で、元の設定抽出・varyBy・構成展開の結果と **全件**一致を確かめる。構成列挙に至れない入力は input-error 行として残し、構成 case の分母とは別に「全入力 file = 展開済み file + 列挙不能 file」で照合する。元 runner 自身の拒否も独立に再現する。列挙不能があれば「全構成を列挙済み」とは書かず、元 runner の拒否まで確認した全入力の処理完了と区別する。

## 3. 構成から新しい program を作る

現行の `NewPrograms(map, options)` は map の全 file を root にする段 1 用。段 2 の入口では、files / roots / options / currentDirectory / symlinks / config 等を保持する descriptor と、毎回新しい Pointer program を作る factory を用意する。名前は任意。段 1 の入口は互換のまま残す。

### 3.1 runner の入力準備との一致

`makeUnitsFromTest`、`newCompilerTest`、`CompileFiles` / `CompileFilesEx` の準備部分を確認する。

- BOM、unit 順序、複数の仮想ファイル、config 用 VFS、最初の tsconfig の選択と unit からの取り出し。
- config の FileNames とその他の file の分離。config が無い場合の、最後の unit の require / triple-slash reference、`noimplicitreferences` による root 選択。
- config options の clone → runner の既定値 → 構成ごとの設定、の適用順。改行、SkipDefaultLibCheck、NoErrorTruncation も含める。`AllowJs` / `CheckJs` を一律に上書きしない。
- currentDirectory、絶対パス化、case sensitivity、symlinks、通常の lib と harness の libfiles / includeLibDir、JSON / tsbuildinfo の root 除外、パスを持つ option の正規化。
- config file の存在、module 解決、SourceFiles の順序・text・script kind・parse options。

project references は現 runner の準備で転送されない経路がある。config に書かれた値と実際に program へ渡された値を区別し、本段で runner の意味を拡張しない。content mapper / incremental 等も、実際に使う経路を確認する。

### 3.2 診断・Emit 前の状態を使う

公開の `harnessutil.CompileFiles` は診断と Emit を実行してから program を返す。これを H1 の初期 program として流用しない。`CompilationResult.Diagnostics` も raw checker 診断の代わりにしない。

storeharness に必要な構築部分を切り出して写し、診断・type walk・Emit を先に実行しない新しい Pointer program を作る。`newProgram(pointer)` による Store の独立 reparse を再利用する。libs を含む全 SourceFiles を program 順に取り込み、IDSpace を共有する。Pointer AST の Store への変換は禁止。

content mapper の host / project は比較と再試行が終わるまで有効でなければならない。`CompileFilesEx` の戻り時の `Close` をそのままコピーしない。既存契約の範囲で新規構築を再現できない経路は、具体的な条件・件数・不足する API を `adapter-unsupported` として記録する。既知で実装できる通常の multi-file / config / symlink 入力を未対応にして完了しない。

同じ descriptor からの fresh build を初回比較と Pointer の再試行に使う。段 1 の map/options 入口に戻す、options の可変状態や checker/cache を再利用する、といった近道は禁止。

### 3.3 診断対象と walker 対象

段 2 の raw 診断・suggestion の対象 (診断対象) は、program の SourceFiles (program 順) から、実効 options の `SkipDefaultLibCheck` が真のときの default lib を除いた全部とする (2026-10-01 改訂、ユーザー)。

- root と依存 file は全部含める。
- 除くのは `compiler.Program.SkipTypeChecking` の条件のうち default lib のものだけ。noCheck の program の file、`skipLibCheck` の .d.ts、checkJs でない JS、project reference の source は除かない。
- `SkipDefaultLibCheck` が偽の構成では、default lib も対象にする。
- 理由: checker の `GetDiagnostics` は、渡された file を options に関係なく検査する。default lib を飛ばすのは program 側。改訂前の版は default lib を含めていて、各 case で両側が default lib を全部検査した。300 case の sample で 1 case の median が 518〜559 ms だった (外すと 34〜45 ms、段 1 並み。`_store-checker-7d2b3-impl/libcost/`)。
- 改訂前の版の予備の全量では、比べた 13,328 構成が default lib を含めて全部 equal だった。default lib の等価を構成ごとに確かめる別のテストは本段では作らない (要否は未決)。

checker の各 file の診断・suggestion → global → type walk → symbol walk → CompareSymbols の順を保つ。root だけでは、import された file の診断を漏らす。compiler の後処理済み診断の一致を検証するものではない。段 1 の対象は変えない。type / symbol walker は runner の `toBeCompiled + otherFiles` のうち program 内に存在する file を対象とし、宣言ファイルも含める。root・診断対象・walker 対象は別々に記録する。hadErrors は段 2 の診断対象全体から算出する。重複・順序の扱いを runner と照合し、最初の root だけで終わらせない。

Q1 のため type / symbol / suggestion の比較を行う。baseline 出力を省く `NoTypesAndSymbols` / `CaptureSuggestions` 等を、Q1 の比較省略条件にはしない。harness option 自体は descriptor に残す。comment directive 前の hadErrors 判定を使い、段 1 baseline テスト専用の override を流用しない。

## 4. 合成 import は未決の境界を調べる

Pointer の `createSyntheticImport` は literal → import declaration → SourceFile の親鎖を program に保持する。現在の Store Factory は checker 私有 region のため、Program 専用 region を作るだけでは他 region の import / clone が拒否される。**この所有権を本段で暗黙に変えない。**

既存の合成 import 除外を残し、段 2 では `adapter-unsupported: synthetic-import` として明示する。Pointer の API で実際の発生を確認し、JSX / helper、file、module text、resolution mode と必要な問い合わせ経路を記録する。新しい無関係な Store panic を合成 import 扱いにしない。case 全体を比較できなければ、その case を equal に数えない。

別途、次の選択肢の制約を調べ、推奨と未確認事項を報告する: program 専用 region、checker ごとの生成、共有の immutable synth の所有者区分。到達経路の小さな scratch probe はよいが、所有権規則を変えた実装は採用しない。

次の修正指示に必要な受入条件も整理する: 両合成ノードの形・flags・range・親・File、path/役割ごとの安定した同一性、NodeKey の program 内一意性、source の不変性、region 間 import 拒否、容量 1、runtime/dev runtime/helpers、module の存在/不在、Node16/NodeNext、段 1 の 8 件。helper は別 fixture で確認する。除外が残る間は段 2 の等価完了を宣言しない。

## 5. 比較・集計と全構成実行

既存の raw diagnostics、独立 walker、private 名・symbol の双方向対応、ID token 検出、CompareSymbols の性質検査を共通化して使う。比較 field の削減、診断の sort、ID の正規表現置換、Store → Pointer printer 変換を追加しない。

新しいテスト名は `TestCheckerConfigurations` とする。段 1 の環境変数と混ぜず、`STORE_CHECKER_CONFIG_*` を用意する (`REPORT`、`OUT`、`TRACE`、`FILTER`、`SAMPLE`、`WORKERS`、`SHARDS`、`SHARD`)。`FILTER` は case ID を指定可能にする。shard は全 manifest の index modulo SHARDS で選び、SHARD は 0 始まり。不正な値は失敗させる。既定は strict。`REPORT=1` は分類収集用で、equal 以外を記録して最後まで進む。manifest 欠落・重複・出力破損・未完了・ハーネスの自己検査失敗は report でも失敗する。

各 case は次の terminal status を 1 つ持つ。細かい比較 class は別 field に保存する。

| status | 意味 |
| --- | --- |
| equal | 必須の比較を全て実施して一致 |
| mismatch | 比較差、Store panic、token、性質違反等 |
| runner-skipped | 元 runner の規則による skip |
| runner-rejected | 構成は列挙できるが、元 runner 自身が option 等を fatal とする。独立再現が必要 |
| adapter-unsupported | 合成 import 等、接続契約の未対応。対象・根拠が必要 |
| pointer-excluded | 独立再現した Pointer panic / 非決定 |
| harness-error | 準備・root 対応・比較基盤の不備。要修正 |

strict では mismatch / adapter-unsupported / harness-error は失敗。runner-rejected も strict では失敗する。runner-skipped / pointer-excluded は根拠付きで記録し、2b 全体の除外妥当性を別途レビューする。段 1 の除外規則は変更しない。予期しない root 不在は単なる除外にしない。正当に root が空の config は元 runner の挙動を再現し、実行した比較と空集合の事実を明記する。

Pointer 非決定の判定は既存の最大 10 回の fresh 再試行と同じ comparator を使い、対象 case ID と入力 hash を全試行に残す。token / 性質違反 / Store panic を非決定で飲み込まない。Pointer 自身の変動と、Store 側の差が重なっていないかを報告する。

JSONL には status/class、reason、最初の差、診断/ノード/symbol/token/CompareSymbols の件数、再試行情報を残す。manifest と一対一で照合する。full run は filter / sample / shard の取り残し 0 を確認し、構成の全件数が status 別件数の和になること。列挙不能 file の件数・理由も別表で併記する。中断・timeout は完了でも未対応でもない。

## 6. 必要な試験と実行

1. 独立した runner probe との全件 manifest 一致。fixture では入力準備と fresh program の実効 options / roots / SourceFiles / 解決結果まで一致。
2. multi-file、非 root からの import と依存 file 内だけの診断、複数 root の後方だけにある診断、config と directive の優先順位、構成の組合せ、case sensitivity / symlink、JSON、lib、JS、NodeNext、空 roots の fixture。content mapper 等の未対応境界にも reason の誤分類を防ぐ試験を置く。
3. 同じ descriptor から複数回構築して可変状態を共有しないこと。再試行でも構成・roots・files が一致すること。
4. mode / emit syntax / resolved module の既存照合を段 2 の比較可能な構成にも適用する。合成 import は未対応と明記する。
5. 全構成を report で 1 回実行し、差を分類する。各原因の代表例を case ID 指定で再現する。新しい production の不一致は直さない。
6. 段 1 strict、mode、accessor、Pointer walker baseline、untagged / storechecks の Store 系 suite を回帰確認する。既知の失敗は `FuzzEquivalence/a708303f611b95c8` だけ。新しい fixture は両 tag で通す。

実行場所は `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc`、build/test は `GOWORK=off`。生出力は `internal/ast/docs/_store-checker-7d2b3-impl/` に置き、コマンド・終了コード・HEAD・差分 hash・入力 hash・環境変数を保存する。大きな出力は commit 対象にしない。テスト名・変数・実行例を実装報告に揃える。

## 7. 完了と報告

`store-checker-7d2b3-implementation-report-20261001.md` を作る。

- 変更、準備処理の写し元と独立照合、対象一覧・実行数・全 status/class の件数。
- 全量 / 部分実行の区別、比較対象 field、token と CompareSymbols の件数、段 1 回帰結果。
- 各原因に、問題・証拠・case ID と再現コマンド・仮説・修正案・受入条件。仮説を確定原因と書かない。混在原因・未分類も残す。
- 合成 import の所有権案、その他の接続未対応と件数、設計判断が必要な点。
- 本段の接続・分類が完了したかと、全構成等価が成立したかを別々に判定する。

通常の全構成接続と全件の処理結果が揃い、ハーネスの誤りが無く、未対応と不一致が再現可能で、段 1 が維持されれば本段の完了。未分類の大量残留、通常入力を未対応へ逃がす実装、未実行部分がある場合は未完了。architecture の判断が必要なら該当箇所を未決として残し、依存しない列挙・比較・分類は進める。
