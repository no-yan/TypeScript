# Store checker 全構成への接続と分類 (TODO 7d-2b-3) 実装報告

作成日: 2026-10-01 (2026-10-02 に完了)。

- 指示書: [store-checker-7d2b3-implementation-instructions-20261001.md](store-checker-7d2b3-implementation-instructions-20261001.md) (以下「指示書」。§3.3 は 2026-10-01 に改訂、§6.6)。
- before: **c5969e29e53**。開始時の HEAD も同じで、開始時の差分は 2 つだけ (`_store-checker-7d2b3-impl/start.txt`)。
  - docs の 1 file (`store-checker-design-20260923.md` の 1 行)
  - 未追跡の指示書 2 本
- commit はしていない。
- 生の出力: `_store-checker-7d2b3-impl/` (ignore 対象、ローカルのみ)。

## 1. 結論

**7d-2b-3 の接続・分類: 完了。**

- compiler / conformance の全 12,730 file を処理した。内訳は、展開した 12,723 file (15,205 case) と、構成を列挙できない 7 file。
  - 列挙できない 7 file は、どれも runner が読む前に飛ばす `skippedTests` の file で、runner 自身も列挙しない。
- 15,205 case は全部、status を 1 つずつ持って終わった (正式の全量実行 run3、4 分 39 秒)。filter / sample / shard で取り残した case は 0。

  | status | case |
  | --- | ---: |
  | equal | **13,328** |
  | runner-skipped | **1,759** |
  | adapter-unsupported (全部 合成 import) | **118** |
  | mismatch・runner-rejected・pointer-excluded・harness-error | **0** |

- ハーネスの準備は、scratch の runner probe と全件で照合した (§3.4)。status、実効 options、roots、SourceFiles、解決結果、walker 対象が一致した。
- 通常の multi-file・config・symlink・content mapper・JSON・lib・JS・NodeNext の入力は全部接続した。未対応にしたのは合成 import だけ。
- 段 1 は維持した (§8)。

**7d-2b 全体の全構成等価: 比べた全構成で一致。除外の妥当性の判断が残る** (§12 の追補で更新)。

- 本段の当初の結果 (§1〜§11): 合成 import の 118 case (73 test file) を比べられず、strict はこの 118 件で失敗した。
- その後、所有権は (c)「共有の immutable な合成」に決まり (2026-10-02、ユーザー)、同じ作業ツリーで実装した (§12)。
  - 段 2 は equal 13,446・runner-skipped 1,759・ほかは 0 で、strict が通る。
  - 段 1 の合成 import の除外も無くなった。
- 残るのは除外の妥当性の判断。
  - runner-skipped の 1,759 case (target ES5 の 1,260 など、runner の規則) と、runner が読まない 7 file。
  - 段 1 の Pointer panic 1 file と、非決定 1 file。

本段で直したこと:

- **ハーネスの誤り 2 つ** (§6.1)。manifest の field の欠落と、strict が `skippedTests` の列挙できない file を失敗に数える規則。
- **ユーザーの決定による診断対象の変更** (§6.6)。`SkipDefaultLibCheck` のときの default lib を外し、指示書を 2 本とも改訂した。

## 2. 変更

変えたのは `tsc/internal/storeharness/` と `tsc/internal/ast/docs/` だけ。Pointer の package、store の production API、P2、生成器と生成物、go.mod / go.sum には触れていない (§8.3)。

### 2.1 既存の file

| file | 変更 |
| --- | --- |
| `driver.go` | `runPointer` / `runStore` の対象を **診断対象** と **walker 対象** に分けた。段 1 の `Compare` は root を両方に渡すので、段 1 の記録は変わらない。<br>比較の本体を `comparePrograms(pointer, targets, rebuild, options)` に切り出した。非決定の判定の再実行は `rebuild` (新しい program を作る関数) で行う。段 1 は `newPointerProgram(files, options)` (今までと同じ)、段 2 は descriptor の `build` を渡す。<br>`Result` に次の field を足した: `Counts` (各側の診断・suggestion・global・型と symbol の記録・異なる symbol の数・walk 対象ごとの記録数)、`RerunStore` (非決定のとき、再実行した Pointer の記録と Store の記録の比較)、`Synthetic`、`Extra` (段 2 の mode の位置の数) |
| `adapter.go` | 合成 import の panic の値を `syntheticImport` (呼ばれた method・file・module の文字列・Pointer が与える mode) にした。除外の判定は、panic の値がこの型かどうかで行う (文字列の一致はやめた) |
| `program.go` | bundled lib の cache を `cachedLib` / `isBundledLib` に切り出し、段 2 の host と共有した (挙動は同じ) |
| `compare.go`・`tokens.go` (と `tokens_test.go`) | 位置の表示を `of root %d` から `of target %d` に変えた。class の文字列は変えていない |
| `mode_test.go` | 1 か所の specifier の比較を `compareSpecifier` に、対の作り方を `pairNodes` (`*testing.T` を取らない) に切り出した。`TestModeCorpus` の比較と数え方は同じ |
| `baseline_test.go`・`private_test.go` | `runPointer` の引数 (root を両方の対象に渡す) |

### 2.2 新しい file

| file | 内容 |
| --- | --- |
| `configprep_test.go` | 列挙 (`enumerateConfigurations`)、case ID、`callWithT`、descriptor (`prepareCase`)、factory (`build`)、対象 (`targets`)、host (`configHost`) |
| `configurations_test.go` | `TestCheckerConfigurations`、`compareCase`、status、選択 (FILTER / SAMPLE / SHARDS)、manifest と JSONL (書いて読み戻す)、集計と strict |
| `configfixtures_test.go` | `TestConfigurationFixtures`、`TestConfigurationRebuild`、`TestConfigurationClasses`、`TestConfigurationContentMappers` |
| `testdata/configurations/*` | runner の形式の fixture 20 本 (21 case と、列挙できない 2 本) |

## 3. 入力の一覧と準備の写し

### 3.1 写し元

公開の関数はそのまま呼ぶ。

- `harnessutil.EnumerateFiles`、`GetFileBasedTestConfigurations`、`SetOptionsFromTestConfig`、`SkipUnsupportedCompilerOptions`
- `testrunner.ParseTestFilesAndSymlinks`
- `tsoptions.ParseJsonSourceFileConfigFileContent`、`tsoptionstest.NewVFSParseConfigHostWithSymlinks`
- `harnessutil.GetConfigNameFromFileName`、`NewOutputRecorderFS`
- `contentmapper.NewHost`、`contentmappertest.NewSpawner`

非公開の部分は写し、各写しの comment に元の関数を書いた。revision は `compiler_runner.go` と `harnessutil.go` が 0e32aa196a、`test_case_parser.go` が 5f647a841a (`configprep_test.go` の冒頭に記載)。

| 写し | 元 |
| --- | --- |
| `compilerTestFile`、`runnerSkippedTests`、`compilerVaryBy` | `compiler_runner.go` の `compilerBaselineRegex`、`skippedTests`、`getCompilerVaryByMap` |
| `extractCompilerSettings`、`testOptionRegex` | `test_case_parser.go` |
| `prepareCase` | `makeUnitsFromTest` (全部)、`newCompilerTest` (`CompileFiles` を呼ぶ前まで)、`CompileFiles` (全部)、`CompileFilesEx` (file system を作る前まで) |
| `build` | `CompileFilesEx` の残りと `compileFilesWithHost` の `createProgram` |
| `configHost` | `cachedCompilerHost.GetSourceFile` (script kind の決め方と、不明なら panic) |
| `testLibFolderMap` | `harnessutil.go` の同名 |

### 3.2 `*testing.T` を取る公開関数

`GetFileBasedTestConfigurations`、`SetOptionsFromTestConfig`、`SkipUnsupportedCompilerOptions` は `*testing.T` を取り、Fatal / Skip で test を終える。

- runner では、`GetFileBasedTestConfigurations` の Fatal は `RunTests` の test 全体を止める。ほかの 2 つは構成ごとの subtest を止める。
- `callWithT` は、それを自分の goroutine で、新しい `testing.T` (zero 値) を渡して呼ぶ。Fatal・Skip・panic のどれで終わったかと、Fatal / Skip を呼んだ関数・file・行を、stack から読む。
- Go 1.27 の testing は、出力先の無い T の log を捨てる (`outputWriter.write` の nil 分岐)。そのため **message は取れない**。理由の欄には呼び出し位置を書き、正確な message は §3.4 の probe の出力と突き合わせた (§6.3 の表)。

### 3.3 descriptor と factory

**`prepareCase`** は、runner の準備を値にした descriptor を作る。

- 持つもの: `currentDirectory`、`toBeCompiled`、`otherFiles`、tsconfig の unit と parse 結果、symlinks、実効 options、harness options、root 名 (`programFileNames`)、`includeLibDir`、content mapper。
- runner は構成の map を自分のものとして、そこに `baseurl` を書き戻す。ここでは map を写してから使う。

**`build`** は、毎回新しく次のものを作る。

- file system (`vfstest` → `bundled.WrapFS` → `OutputRecorderFS`)
- options の clone
- content mapper の host と project
- host、`compiler.NewProgram`

返す関数は content mapper の project と host を閉じる。初回の program は比較と全ての再実行が終わるまで生かし、再実行の program はその実行の後で閉じる。

harnessutil と違う点と、それが program を変えない理由:

- **incremental**: runner の `createProgram` は、options が incremental なら `incremental.NewProgram` で包む (corpus に 7 case)。包むと program の診断と emit に build info が加わるが、checker は中の `compiler.Program` に作る。H1 の checker も同じ `compiler.Program` に作るので、包まない。
- **trace**: host は trace しない。runner の tracer は module 解決の baseline のための記録だけ。
- **SingleThreaded**: true。`testutil.TestProgramIsSingleThreaded` も race detector の外では true。
- **parse の共有**: file は program ごとに parse し直す。bundled lib だけは段 1 と同じく共有する。runner の `sourceFileCache` は全 file を共有するが、key は parse 条件と text なので、作られる木は同じ。
- **project references**: runner の `CompileFilesEx` は、tsconfig の references を program の config に渡さない。ここでも渡さない。program の config の references は両方とも全 case で 0 (probe)。corpus に references を書いた tsconfig は無い。

**`targets`** は対象を返す。

- **診断対象**: program の SourceFiles を program の順に並べ、`SkipDefaultLibCheck` が真のときの default lib だけを除いたもの (§6.6、改訂後の指示書 §3.3)。
  - root と依存 file は全部含む。noCheck の program の file、`skipLibCheck` の .d.ts、checkJs でない JS も除かない。
- **walker 対象**: `toBeCompiled` + `otherFiles` のうち program にある file。runner の `verifyTypesAndSymbols` と同じ順で、重複も runner と同じく残す。

### 3.4 独立照合 (scratch の probe)

probe は 2 つで、どちらも `-overlay` で足した。追跡する file は変えていない。材料は `_store-checker-7d2b3-impl/probe/` で、overlay の json はこの dir の file を指す。

1. **runner 側** (`zz_probe_test.go`、testrunner package)
   - 元の `NewCompilerBaselineRunner`・`EnumerateTestFiles`・`getCompilerFileBasedTest`・`makeUnitsFromTest`・`newCompilerTest`・`SkipUnsupportedCompilerOptions` を、file ごと・構成ごとの subtest で呼ぶ。
   - harnessutil の overlay は、`compileFilesWithHost` に hook を 1 つ足すだけ (`harnessutil-overlay.diff`)。hook は `createProgram` の直後、**診断と Emit の前**に snapshot を取り、そこで返る。
   - Fatal / Skip の message は `go test -json` の出力から取る。
2. **harness 側** (`zz_harness_probe_test.go`、storeharness package)
   - `enumerateConfigurations`・`prepareCase`・`build` の結果から、同じ形の snapshot を取る。
   - snapshot の形 (`snapshot.go.tmpl`) は両方で同じ text を使う。

snapshot に入れたもの:

- 実効 options (JSON)、harness options
- program の config の root と config file、project references の数
- currentDirectory、大文字小文字の区別、content mapper の project の有無
- SourceFiles の順・名前・path・text の hash・script kind・language variant・parse options・宣言か・default lib か・content mapper・metadata
- module と type reference の解決の全部
- `toBeCompiled`・`otherFiles`・walker 対象

**結果**:

| 対象 | 一致したもの |
| --- | --- |
| 全量 (`compare-full.txt`) | 15,205 case と列挙できない 7 file が全部一致 (status、上の snapshot の全 field)。status の内訳は比較 13,446・skip 1,720・`skippedTests` 39。Fatal / Skip の呼び出し位置と message の対応は §6.3 |
| fixture (`compare-fixtures.txt`) | 21 case と列挙できない 2 file が全部一致。runner-rejected 2 (`unsupported module kind AMD`、`Unknown compiler option 'notacompileroption'.`) と skip 1 を含む |

- 比較の検出力 (`compare-sensitivity.txt`): harness 側の出力を 4 通りに壊すと (otherFiles を root にする、walk 対象を 1 つ落とす、option を反転する、case を 1 つ落とす)、どれも検出した。
- corpus の特徴: content mapper の project を持つ case 15、content mapper の file 25、incremental 7、大文字小文字を区別しない 6、tsconfig を持つ 157。SourceFiles が空の program は 0 (fixture `emptyRoots.ts` だけ)。

## 4. 全構成への接続 (`TestCheckerConfigurations`)

### 4.1 case と ID

- **列挙**: runner と同じ順 (compiler、conformance。各 suite は `EnumerateFiles` の順)。`skippedTests` の file も列挙し、その case は runner-skipped にする。
- **構成**: `GetFileBasedTestConfigurations` の結果が空なら、構成なしの 1 case にする。
- **ID**: `suite/相対 path` に、その file の構成が変える option (構成の間で値が 2 つ以上あるもの) の `key=value` を key の順に並べて括弧で付ける。値は小文字。
  - 例: `compiler/foo.ts(module=esnext,target=es2015)`
  - 構成の順は map の反復順で決まらないので、file の中は ID の順に並べる。
  - 同じ ID が残れば、構成全体の JSON の順に `#2`、`#3` を付ける (corpus では 0 件)。
- **index**: 全一覧の位置。filter・sample・shard は index を変えない。
- **自己検査** (report でも失敗する):
  - ID が一意で、`;` を含まない。
  - index が 0 から順に並ぶ。
  - 全 file = 展開した file + 列挙できない file。

### 4.2 status

各 case は status を 1 つだけ持つ。比較の class (段 1 と同じ文字列) は別の field に入れる。

| status | 判定 | strict |
| --- | --- | --- |
| equal | 診断・suggestion・global・型 walk・symbol walk・symbol の対応・token・CompareSymbols の性質・mode を全部比べて一致 | 成功 |
| mismatch | 記録の差、Store の panic、token、性質の違反、mode の差 | 失敗 |
| runner-skipped | `skippedTests`、または `SkipUnsupportedCompilerOptions` の t.Skip | 成功 |
| runner-rejected | `SetOptionsFromTestConfig` か `failOnUnsupportedCompilerOptions` の t.Fatal | 失敗 |
| adapter-unsupported | 合成 import。adapter の panic の値で判定し、Pointer の program に同じ specifier があることを確かめる (無ければ harness-error) | 失敗 |
| pointer-excluded | Pointer の program の構築か run の panic、または 10 回の再実行での Pointer の非決定 | 成功 (根拠を記録) |
| harness-error | 準備の panic、ハーネスの panic、知らない除外 | 失敗 (report でも失敗) |

列挙できない file は case にならず、manifest に input-error の行として残る。strict で失敗にするのは、runner が `skippedTests` で読む前に飛ばす file を除いたもの。

### 4.3 環境変数と出力

段 1 の変数とは分けた。

| 変数 | 意味 |
| --- | --- |
| `STORE_CHECKER_CONFIG_REPORT=1` | report。status を全部記録して最後まで進む。失敗するのは、ハーネスの自己検査、harness-error、出力の読み戻しのときだけ |
| `STORE_CHECKER_CONFIG_OUT=path` | case ごとの JSONL。manifest は `path` の `.jsonl` を `.manifest.jsonl` に替えた所。どちらも書いた後に読み戻して照合する |
| `STORE_CHECKER_CONFIG_TRACE=path` | case の開始 (index・ID・入力 hash) と終了 (status)、Pointer の再実行ごとの行 (index・ID・入力 hash・回数) |
| `STORE_CHECKER_CONFIG_FILTER=s` | ID が `s` を含む case。`=ID` は完全一致 (無い ID なら失敗)。`;` で複数 |
| `STORE_CHECKER_CONFIG_SAMPLE=n` | 一定間隔の n case |
| `STORE_CHECKER_CONFIG_WORKERS=n` | worker の数 (既定 4) |
| `STORE_CHECKER_CONFIG_SHARDS=n` / `_SHARD=i` | index mod n == i の case。両方が要り、範囲外は失敗 |

不正な値はどれも失敗することを確かめた (`repro/invalid-env.txt`)。filter と shard の組み合わせ (`repro/shard.*`) では、index を採番し直さず、選ばれない理由を manifest に記録した。

**manifest** は、1 行が 1 case か、列挙できない 1 file。

- kind、index、ID、suite、file、入力 (test file の text) の sha256、構成、`skippedTests` か
- 列挙できない理由、選ばれたか、選ばれない理由 (`filter` / `sample` / `shard`)

**JSONL** は、選ばれた case ごとに 1 行。

- index、ID、入力 hash、status、class、reason、最初の差 (`detail`)
- root の数と program に無い root、診断対象と walker 対象の数
- 各側の件数 (診断・suggestion・global・型の記録・symbol の記録・異なる symbol・walk 対象ごとの記録)
- token、CompareSymbols の組と fallback、mode の位置の数
- Pointer の program の合成 specifier の数 (JSX・helpers)、未対応の合成 import の内容
- 再実行の回数と最初の差、再実行と Store の比較、時間

## 5. 試験 (指示書 §6 の 1〜4)

期待値は runner を読んで書いた。ハーネスの写しから導いた値は使っていない。全部が両方の tag の一式で通る (§8)。

### 5.1 `TestConfigurationFixtures` (`testdata/configurations/`、20 本)

| fixture | 確かめること |
| --- | --- |
| 列挙 | 21 の case ID が index の順に並ぶ (`variations.ts` は 4 構成)。列挙できない 2 本の理由: `emptyVariation.ts` は `splitOptionValues` の panic、`tooManyVariations.ts` は 25 を超える構成数での `GetFileBasedTestConfigurations` の t.Fatal |
| `dependencyOnly.ts` | `@noImplicitReferences` のとき、root は最後の unit だけで、`dep.ts` は otherFiles。walker 対象は `[main.ts, dep.ts]`。**依存 file にだけある診断** (TS2322) を記録すること。各 walk 対象に両側の記録があり、`fromDep` の symbol の記録があること |
| `laterRoot.ts` | root が 3 つで、**最後の root にだけある診断** (TS2322) を記録すること。3 file とも walk すること。既定 (`skipDefaultLibCheck`) では、診断対象が root の 3 file だけで default lib を含まないこと |
| `configPriority.ts` | tsconfig の options を clone → runner の既定値 → directive の順に適用すること。strict は directive が tsconfig に勝ち、newLine は tsconfig が既定値に勝ち、allowJs は tsconfig、checkJs は directive。tsconfig の files が toBeCompiled、残りが otherFiles。program に無い otherFiles は walk しない。TS7006 と JS の TS2322 |
| `variations.ts` | 4 構成それぞれの実効 options (strict・target・module) |
| `caseInsensitive.ts` | 大文字小文字を区別しない file system と、その解決 (`./LIB`) |
| `symlinked.ts` | `@link` と `@symlink` の両方、解決先の実 path、symlink 越しの型 (TS2322) |
| `jsonImport.ts` | JSON の unit は toBeCompiled だが root ではなく、import で program に入って walk すること |
| `libFiles.ts` | `@libFiles` の `/.lib/react.d.ts` が root で、診断対象であること (default lib ではない)。`@lib: es2015` の lib の選択 (dom が無い) |
| `defaultLibChecked.ts` | `@skipDefaultLibCheck: false` では、default lib も診断対象であること |
| `uncheckedTargets.ts` | noCheck、`skipLibCheck` の .d.ts、checkJs でない JS が、どれも診断対象に残ること (raw の checker は noCheck でも TS2322 を出す) |
| `jsCheck.ts` | `checkJs` の診断 (TS2345) |
| `nodeNext.ts` | `module: nodenext` と `package.json` の `type: module`。mode を比べた specifier の位置が 3 (import 2 と import call 1)。file の既定の mode |
| `emptyRoots.ts` | `"files": []`。root・toBeCompiled・walk 対象・診断対象が全部空で、program の SourceFiles も空 (root の無い program は lib も読まない)。比較は global の診断と空の対象で行い、equal になること |
| `rejectedAmd.ts`・`rejectedUnknownOption.ts`・`skippedTarget.ts` | runner-rejected (`failOnUnsupportedCompilerOptions`、`SetOptionsFromTestConfig`) と runner-skipped (`SkipUnsupportedCompilerOptions`) |
| `jsxRuntime.tsx`・`importHelpers.ts` | adapter-unsupported (synthetic-import)。method・file・module の文字列と、Pointer の program の合成 specifier の数 (JSX 1、helpers 1) |

### 5.2 `TestConfigurationRebuild` (§6 の 3)

- **再構築**: 同じ descriptor から 3 回作った program が、options・host・file system・lib 以外の SourceFile を共有しないこと。program の要約が 3 回とも同じこと。
  - 要約の中身は、実効 options の JSON、root、SourceFiles (名前・text の hash・script kind・parse options)、診断対象、walker 対象。
  - 作った後も、descriptor の options が変わらないこと。
- **再実行**: Store の記録を 1 つ変えて、記録の差を起こす (`afterRuns`)。次のことを確かめる。
  - Pointer の再実行が 10 回走り、各回の program が descriptor から作り直されて、初回と同じ要約になること。
  - class は `walk: type` の mismatch で、非決定に分類されないこと。

### 5.3 `TestConfigurationClasses`

`afterRuns` で次の 4 つを起こし、それぞれの分類を確かめる。

| 起こすこと | 期待する分類 |
| --- | --- |
| 合成 import でない Store の panic | mismatch (`store panic: …`)。未対応にしない |
| Pointer の program に無い合成 import | harness-error |
| walk の値の `__@x@1` | `internal id token present` |
| 診断の生 byte `0xFE@x@1` | 同上 |

### 5.4 `TestConfigurationContentMappers`

compiler suite の content mapper の test (15 file) が、mapper の project を持つ program で作られ、equal か mismatch になること (未対応や harness-error にならないこと)。全量でも 15 file とも equal だった。

### 5.5 mode・emit syntax・resolved module (§6 の 4)

`TestModeCorpus` と同じ比較 (`compareSpecifier`) を、段 2 の比較できる各 case の `compareModes` で行う。

- 対象: default lib を除く全 SourceFile の、全ての module specifier の位置。
- 記録が一致した case にだけ行う。違えば class `mode: <method>` の mismatch にする。
- 全量では 1,951 case の 7,689 位置を比べ、差は 0。
- 合成 import の specifier は木に無いので比べていない (adapter が未対応)。

## 6. 全構成の実行と分類 (指示書 §5、§6 の 5)

### 6.1 実行

| run | 診断対象 | binary | 結果 | 扱い |
| --- | --- | --- | --- | --- |
| run1 | default lib を含む | manifest の修正前 | 15,205 case、wall 52m20s、max RSS 1.45 GB、exit 0 | 予備 (`run1/`) |
| run2 | default lib を含む | manifest の修正後 | 10,115 case で停止 | ユーザーの決定 (§6.6) で止めた。結果ではない (`run2-stopped/`) |
| **run3** | default lib を除く (改訂後) | 最終 | 15,205 case、wall **4m39s**、max RSS 1.75 GB、exit 0 | **正式** |
| strict | 同上 | 同上 | exit 1、`118 cases not accepted` (全部 adapter-unsupported) | strict の確認 |

**run1 の後で直したハーネスの誤り:**

1. **manifest の field の欠落**
   - manifest の行は `*configCase` と `*inputError` を埋め込んでいた。encoding/json は同じ深さの同名 field (suite・file・contentHash・skippedTests) を両方とも捨てるので、run1 の manifest の行にはこれらが無かった。
   - 明示の struct に直し、書いた後に読み戻して照合するようにした (`writeManifest`)。case ごとの比較には影響しない。
2. **strict の規則**
   - 列挙できない 7 file を strict が失敗に数えていた。runner が `skippedTests` で読む前に飛ばす file は除いた (7 本ともそれに当たる)。

**run の間の一致:**

- status と class は、run1・run3・strict の 15,205 case で全部一致した (`run1-vs-run3.txt`、`compare_runs.py`)。
- run1 と run3 の件数の差は、default lib を外したことで説明できる (§6.6)。

**run3 の記録:**

- 環境と記録は `run3.cmd.txt`。
  - `GOWORK=off`。`STORE_CHECKER_CONFIG_REPORT=1`、`_OUT`、`_TRACE` だけを設定し、FILTER / SAMPLE / SHARDS / SHARD と段 1 の変数は未設定。
  - HEAD、tracked の差分の sha256、未追跡の harness file の sha256、開始と終了の時刻、終了コードも記録した。
- 出力は `run3.jsonl`、`run3.manifest.jsonl`、`run3.trace`、`run3.txt`。分析は `run3-analysis.txt` (`analyze_run.py`)。

### 6.2 manifest と JSONL の照合 (run3、`run3-analysis.txt`)

- manifest: case 15,205、input-error 7、選択 15,205、選ばれない case 0。
- manifest と runner probe: case の集合 (suite・file・構成) の差 0、入力 hash の差 0、input-error の集合が一致。ID は一意で、index は 0 から順。
- JSONL: 15,205 行で、manifest の選択と同じ順・同じ ID。status の和は 15,205。
- status と runner probe: 比較 13,446・`skippedTests` 39・skip 1,720 が全部一致。相違 0。

### 6.3 status と原因

| status | case | 原因 |
| --- | ---: | --- |
| equal | 13,328 | |
| runner-skipped | 1,759 | runner の規則 (下の表) |
| adapter-unsupported | 118 | 合成 import (§6.4 の A) |
| mismatch・runner-rejected・pointer-excluded・harness-error | 0 | |
| 計 | 15,205 | |

runner-skipped の理由:

| 理由 (harnessutil.go の行) | runner の message (probe) | case |
| --- | --- | ---: |
| 1258 | `unsupported target ES5` | 1,260 |
| 1241 | `unsupported module kind System` / `UMD` | 266 |
| 1245 | `unsupported module resolution kind` (node10 / classic) | 85 |
| 1261 | `alwaysStrict=false is unsupported` | 52 |
| `skippedTests` (compiler_runner.go) | runner は読まない | 39 |
| 1254 | `unsupported baseUrl …` | 33 |
| 1248 | `esModuleInterop=false is unsupported` | 24 |

列挙できない file (manifest の input-error、7 本):

- `moduleNoneDynamicImport.ts`、`moduleNoneErrors.ts`、`noErrorUsingImportExportModuleAugmentationInDeclarationFile1〜3.ts`、`requireOfJsonFileWithModuleEmitNone.ts`、`requireOfJsonFileWithModuleNodeResolutionEmitNone.ts`。
- どれも `@module: none` (今の option に無い値) で、`getValueOfOptionString` (harnessutil.go:1153) が t.Fatal する。runner probe の message は `Unknown value 'none' for option 'module'`。
- どれも `skippedTests` に載っていて、runner は読まずに飛ばす。

### 6.4 原因ごとの記録

**(A) 合成 import: adapter-unsupported 118 case (73 file)**

- **問題**: Store checker が合成 import の specifier を問い合わせると、adapter が返せる Store のノードが無い。
- **証拠**:
  - 118 case の全部で、adapter の panic の値の method・file・module の文字列が、Pointer の program の specifier と一致した (`hasSyntheticSpecifier`。偽なら harness-error にする)。
  - 内訳:

    | method | module | case |
    | --- | --- | ---: |
    | `GetImportHelpersImportSpecifier` | `tslib` | 56 (mode CommonJS 45 / ESNext 11) |
    | `GetJSXRuntimeImportSpecifier` | 下の 7 種 | 62 (mode CommonJS 45 / ESNext 17) |

    - JSX の module と case 数: `react/jsx-runtime` 29、`react/jsx-dev-runtime` 13、`preact/jsx-runtime` 8、`@emotion/react/jsx-runtime` 4、`preact/jsx-dev-runtime` 4、`/jsx/jsx-runtime` 2、`/jsx/jsx-dev-runtime` 2。
    - file の数: JSX 36、helpers 37。
  - Pointer の program に合成 specifier を持つ case は 139。そのうち 21 は Store checker が一度も問い合わせず、equal になった。21 件とも helpers で、例は `importHelpersWithImportStarAs.ts(esmoduleinterop=true,module=es2015)`。
  - 段 1 の除外との対応 (全部 JSX の case):
    - 段 1 で合成 import として除外した 8 file (5 test) は、段 2 では 10 case。
    - 段 1 で Pointer が非決定だった file の test (`jsxNamespaceImplicitImportJSXNamespace.tsx`) は 1 case。
- **再現** (どちらも strict で exit 1。ログは `repro/strict4.*`、`repro/strict5.*`):
  - JSX: `STORE_CHECKER_CONFIG_FILTER='=compiler/jsxImportSourceNonPragmaComment.tsx'`
  - helpers: `STORE_CHECKER_CONFIG_FILTER='=compiler/importHelpersBundler.ts'`
- **原因**: 確定。adapter が合成 import を未対応にしていて、その所有権が未決であるため。
- **修正案**: §7。案 (a) の scratch probe で 118 case が全部 equal になった。
- **受入条件**: §7.4。

**(B) runner-skipped 1,759 case**

- **問題**: runner の規則で飛ばされ、比べていない構成。
- **証拠**: §6.3 の表。runner probe の status と message に全件一致した。
- **再現** (どちらも strict で exit 0。ログは `repro/strict2.*`、`repro/strict3.*`):
  - `FILTER='=compiler/abstractPropertyBasics.ts(target=es5)'`
  - `FILTER='=compiler/APILibCheck.ts'`
- **原因**: runner の既定の規則による。仮説は無い。
- **修正案**: 本段では無い。2b 全体の除外の妥当性レビューの対象にする。最大は ES5 の 1,260 case。
- **受入条件**: 2b の完了判定で、除外の扱いを明記して決めること。

**(C) 差の無い構造上の特記**

- **program に無い root がある case: 21 件**。
  - 例: allowJs の無い `.js`、`.js.map`、拡張子の無い `/project/e`・`.txt`、`node_modules` の `.js`。
  - runner も同じ root を渡し、program が入れない (program の診断になる)。比較は通常どおり行い、20 件は equal、1 件 (`privateIdentifierPropertyAccessDestructuringAssignmentES6.ts`) は合成 import。
- **walker 対象も診断対象も空の equal: 2 件** (`checkJsFiles6.ts`、`jsFileCompilationWithoutJsExtensions.ts`)。
  - JS の root が program に入らず、残る SourceFiles は default lib だけ。比較は global の診断と空の対象 (`repro/strict6.*` は exit 0)。
- **SourceFiles そのものが空の program**: corpus には無い。fixture `emptyRoots.ts` で確かめた。

mismatch・未分類・混在した原因は無い。

### 6.5 件数 (run3、equal の 13,328 case の合計。両側で同じ)

| 項目 | 件数 |
| --- | ---: |
| 診断対象 / walker 対象 | 18,455 / 18,074 file |
| 生の診断 (6,686 case にある) | 38,568 |
| suggestion | 32,164 |
| global | 87 |
| 型の記録 / symbol の記録 | 740,025 / 740,109 |
| 異なる symbol | 216,148 |

- 内部 ID の token: 0。
- CompareSymbols の性質の検査: 各側 298,919 組 (ID の fallback に届く 407 組)、違反 0。
- mode を比べた位置: 7,689 (1,951 case)。
- Pointer の非決定: 再実行は 0 回 (run1 でも 0 回)。
- 1 case の時間: median 54 ms、p99 294 ms。最大は `intersectionConstructorReductionCrash.ts` の 74.8 s。

### 6.6 診断対象から default lib を外した (ユーザーの決定、2026-10-01)

**経緯**

- 改訂前の指示書 §3.3 は、診断対象を「libs を含む全 SourceFiles」にしていた。run1 はこの形で 52 分かかった (段 1 の 10 倍以上)。
- checker の `GetDiagnostics` は、渡された file を `SkipDefaultLibCheck` に関係なく検査する。default lib を飛ばすのは program の `SkipTypeChecking` (`compiler/program.go:823`) で、raw の checker を直接呼ぶ H1 の driver はそこを通らない。
- 小さな fixture 4 case の CPU profile では、`compareCase` 1.84 s のうち `checkInterfaceDeclaration` (lib の宣言の検査) が Pointer 0.81 s・Store 0.86 s だった。

**決定 (ユーザー)**

- 診断対象から外すのは、`SkipDefaultLibCheck` が真のときの default lib だけにした。`SkipTypeChecking` のほかの条件 (noCheck、`skipLibCheck` の .d.ts、checkJs でない JS、project reference の source) は外さない。
- 指示書と検証指示書を改訂した (作業 §3.3、検証 §3。どちらも冒頭に改訂を記載)。

**実測** (`libcost/`、同じ 300 case の sample、worker 4、各 2 回):

| 版 | 1 case の median | CPU (user) | wall / case |
| --- | ---: | ---: | ---: |
| default lib を含む | 518〜559 ms | 162〜172 s | 117〜124 ms |
| default lib を除く | 34〜45 ms | 15〜18 s | 9〜13 ms |

- 段 1 は 1 file の median が 36〜43 ms、wall 14 ms だった。
- 300 case の status と class は全部同じだった。生の診断の件数も同じ (697)。違うのは default lib の suggestion (21,721 → 634) だけ。

**外したことで比べなくなったもの** (run1 との差、`run1-vs-run3.txt`):

- default lib に出る生の診断 83 件 (17 case)。user code の augmentation や lib の選択で lib の宣言に出るもので、例は `modularizeLibrary_ErrorFromUsingES6ArrayWithOnlyES6ArrayLib.ts(target=es2015)` の 27 件。
- default lib の suggestion 約 104 万件。
- run1 では、どちらも両側で一致していた。
- 異なる symbol の数や CompareSymbols の組の数が run1 と違う equal の case が 8 件ある。lib を先に検査したかで、walk が返す symbol の個体の数が変わる。walk の記録の件数は全 case で同じで、どちらの run でも両側は一致している。

**未決**: default lib の等価を構成ごとに確かめる別のテストは作っていない。作るなら、key を (lib の集合, 効く option) にすると、上の 17 case のような user code 由来の lib 側の診断は落ちる。

## 7. 合成 import (指示書 §4)

### 7.1 コード上の事実 (確定)

- **Pointer の生成** (`compiler/fileloader.go:926-933` `createSyntheticImport`)
  - program の `fileLoader.factory` (1 つで、`factoryMu` で直列に使う) で、`StringLiteral` と `ImportDeclaration` を作る。
  - `literal.Parent = decl`、`decl.Parent = file`。decl は `file.Statements` に入らない。range は (-1, -1) (`ast.newNode` の `UndefinedTextRange`)、flags は 0。
  - 作る条件は `resolveImportsAndModuleAugmentations` (同 816-848) にある。
    - helpers: `importHelpers` が真で、file が JS か、宣言でなく (isolatedModules か外部 module)。
    - JSX: file が JS か TSX で、`GetJSXRuntimeImport` が空でない。
  - file と役割ごとに 1 つ作る。program の map (`jsxRuntimeImportSpecifiers` / `importHelpersImportSpecifiers`) が持ち、module 解決の `moduleNames` と include reason (`synthetic`) にも使う。
- **checker の到達**
  - Pointer: `checker/checker.go:29071` `resolveHelpersModule`、`checker/jsx.go:1449` → `getJSXRuntimeImportSpecifier`。
  - Store: `storechecker/checker.go:27639`、`storechecker/jsx.go:1416`。
  - specifier は `resolveExternalModule` (`storechecker/checker.go:14477`) の `location` に渡るだけ。そこで読むのは次のものだけで、書き込みは無く、checker の Store からこのノードへの辺も作らない (読んで確かめ、§7.3 の probe で実行時にも確かめた)。
    - `File()`、`IsStringLiteralLike`、`Flags()` の Ambient
    - `Parent()` の連鎖 (`FindAncestor` の import call / import / export / import= / `IsEmittableImport` / `IsResolutionModeOverrideHost`)
    - adapter の `GetModeForUsageLocation` (`Parent()` と `Attributes()`)
  - 診断は `errorNode` (JSX の最初の tag、helper を使う所) に付き、specifier には付かない。
- **Store の所有権の規則** (`ast/store/factory.go`)
  - `checkImport` (55-61): region r が Store s へ辺を張れるのは、`s.owner == r.token` (同じ region) か、`s.owner == nil && s.space == r.space` (同じ program の source / 遅延 JSDoc) のときだけ。
  - `checkWritableStore` (372-): 書けるのは、自分の region の seal されていない Store だけ。
  - `NodeRegion` の doc は「1 つの checker の合成 Store の集合」。
  - `Node.File()` (`store.go:182`) は、file を持たない Store では親を辿る。
- **既存の前例: 遅延 JSDoc の Store** (`ast/store/jsdoc.go:114` `FinishLazyJSDoc`)
  - Builder で作り、program の `IDSpace` の次の StoreID、host の file、host への foreign の親辺を持たせて seal する。
  - owner は nil なので、どの checker の region からも辺を張れる (`checkImport` の 2 番目の条件)。

### 7.2 案の比較

| 案 | 作り方 | 制約・得失 |
| --- | --- | --- |
| (a) program 専用 region | adapter (将来は Store の program) が `NewNodeRegion(space)` と `NewFactory` を持ち、file と役割ごとに作って cache する | store の API の変更は要らない (今ある public API だけで書ける。§7.3)。<br>checker は読めて、書けない (owner が違う)。<br>checker の region からの辺 (`New` の子、`SetParent` の親、clone の子) は `checkImport` で panic する。今の到達には無く、将来の到達で生じても黙らずに落ちる。<br>「1 つの checker の region」という NodeRegion の定義に、program の region を足すことになる (所有者区分の追加)。<br>Store は seal されないので、作り終えたら Factory を捨てるか `Seal` する規律が要る |
| (b) checker ごとの生成 | storechecker の 2 か所 (`getJSXRuntimeImportSpecifier`、`resolveHelpersModule`) を P2 にし、checker 自身の Factory で作って `sourceFileLinks` に cache する。`storechecker.Program` は module の文字列だけを返す | 同一性が program 単位 (Pointer) から checker 単位になり、checker の外 (fileloader の解決・include reason) と同じノードを共有しない。<br>checker 自身が書ける Store に入るので、不変性の検査が無い。<br>production の interface (`checker_manual.go`) を変え、P2 が 2 つ増える。<br>H1 は checker 1 つなので、等価の比較には差が出ない見込み (未確認) |
| (c) 共有の immutable な合成 (owner nil) | 遅延 JSDoc と同じ区分。program の `IDSpace` で Builder が作り、file を持たせ、decl から file の root へ foreign の親辺を張り、seal する | どの checker の region からも辺を張れて、どこからも書けない (owner nil と seal)。<br>`File()` は Store の file を直接返し、Pointer の親の連鎖と同じ file になる。<br>store に production の API が要る (`FinishLazyJSDoc` に並ぶ関数)。不変条件 26 の「source または遅延 JSDoc」に 3 つ目の区分を足す文書の改訂も要る。<br>レビュー契約 §3 の測定用 adapter (Pointer なしで specifier を Store のノードで再構成する) にもそのまま使える |

**推奨: (c)。** Pointer の共有 (program が持つ不変の 1 ノードを、どの checker も読む) に最も近い。書き込みの拒否を保ったまま、将来の到達で辺が生じても通る。

**決定 (2026-10-02、ユーザー): (c) を採る。** 実装は本段の範囲外 (store の production API を変える) なので、次の段の指示書で行う。

- (a) は、store を変えずに今の到達を満たす最小の案。store の API の追加を先送りするなら代わりになる。
- (b) は interface と P2 を増やし、不変性と同一性の共有を失うので推奨しない。

未確認の事項:

- (c) で file を持つが source でない Store を、source / 遅延 JSDoc とみなす他のコードが無いか。`JSDoc()` は flag の検査で先に抜ける (`jsdoc.go:21-24`) が、`File` の NodeRef を key にする cache (`jsdocCache`) などを全部は調べていない。
- 到達の外 (LS、emit、宣言の emit が checker を使う経路) で、specifier への辺や書き込みが生じるか。
- Pointer を持たない Store の program (2c の測定プロセスや将来の Store の fileloader) が、file ごとの JSX の import source をどう決めるか。今は Pointer の program に頼っている。決めるには pragma を読む `GetJSXImplicitImportBase` が要る。

### 7.3 到達経路の scratch probe (採用しない)

案 (a) を、storeharness の `adapter.go` の scratch overlay で書いた (`synthetic-probe/`、差分は `adapter-overlay.diff`)。

- program ごとに region と Factory を持つ。
- `(path, 役割)` ごとに `NewStringLiteral` と `NewImportDeclaration` で作り、`SetParent(literal, decl)` と `SetParent(decl, file.Root())` を張って返す。

| 対象 | 合成 Store の容量 | 結果 |
| --- | --- | --- |
| run1 の adapter-unsupported 118 case (`au-ids.txt`) | 4096 | 118 equal、合成 import 138 個 (`cap4096.txt`) |
| 同上 | 1 | 118 equal (`cap1.txt`) |
| 同上 | 1、`-tags storechecks` | 118 equal (`cap1-storechecks.txt`) |
| 段 1 の 8 file を含む corpus 14 file (`TestCheckerCorpus` の filter 5 つ) | 1 | 14 equal、合成 import の除外 0 (`stage1-corpus-cap1.txt`) |
| fixture `jsxRuntime.tsx`・`importHelpers.ts` | 4096 / 1 | equal |

- 容量 1 では、全ての辺が Store の境界をまたぐ。
- どの実行でも、`checkImport` と書き込み検査の panic は 0。
- 今の到達では、合成ノードは読まれるだけだと実行時にも確かめた。

### 7.4 次の修正指示の受入条件 (案)

1. **形・flags・range・親**が Pointer と同じ。
   - ノードは StringLiteral (text は module 名、TokenFlags なし) と ImportDeclaration (modifiers・import clause・attributes なし)。flags 0、range (-1, -1)。
   - 親は literal → decl → file の root で、`File()` は対象の file。
2. 同じ (path, 役割) には同じノード (`SameNode`) を返す。JSX と helpers は別のノード。Pointer に無い file では nil を返す。
3. NodeKey が program 内で一意 (source・遅延 JSDoc・checker の合成と衝突しない)。
4. source の Store を変えない (作る前後で、source の Store の bytes と foreign の表が同じ)。
5. 所有権が選んだ案の規則どおり: ほかの region からの書き込みは panic。辺は (a) なら panic、(c) なら通る。これを storechecks と通常の build の両方で試験する。
6. 合成の Store の容量 1 でも等価。
7. 次の各場合で等価。module が解決できる場合とできない場合 (診断が `errorNode` に出る) の両方で試す。
   - JSX runtime (`react-jsx`)
   - dev runtime (`react-jsxdev`)
   - `jsxImportSource` の pragma
   - helpers (`importHelpers`)
8. Node16 / NodeNext で、合成 specifier の mode (`GetModeForUsageLocation`) と解決が Pointer と同じ。
9. 段 1 の 8 file と、段 2 の adapter-unsupported の全 118 case が、除外なしで equal。helpers は fixture (`importHelpers.ts`) とコーパスの case の両方で試す。
10. 除外 `synthetic import` と status `adapter-unsupported: synthetic-import` を消しても、段 1 strict と段 2 strict が通る。

## 8. 段 1 の回帰と生成物

### 8.1 一式 (両 tag)

| 一式 | 結果 | 時間 | log |
| --- | --- | --- | --- |
| `go test -count=1 -timeout 60m -skip '^TestCheckerConfigurations$' -v ./internal/ast/store/... ./internal/store...` (tag なし) | 失敗は既知の `FuzzEquivalence/a708303f611b95c8` (`ExternalModuleIndicator differs from the Pointer file`) だけ | 15m35s | `test-notag.txt` |
| 同 (`-tags storechecks`) | 同上 | 13m51s | `test-storechecks.txt` |

- 両方の一式で、段 1 と新しい試験が実行されて PASS した (log で確認)。
  - 段 1 の試験: `TestCheckerCorpus` (strict)、`TestModeCorpus`、`TestPolymorphicAccessors`、`TestPointerWalkerBaselines`、`TestPrivateFixtures`、`TestInternalIDTokenRe`、`TestIDTokens`、collection と `TestTypeRoleSlot`。
  - 新しい試験: `TestConfigurationFixtures`、`TestConfigurationRebuild`、`TestConfigurationClasses`、`TestConfigurationContentMappers`。
- `TestCheckerConfigurations` は `-skip` で外れ、実行されていない。全量は §6.1 で別に回した (report と strict)。

### 8.2 段 1 の件数と 2b-2 の照合

| 項目 | 2b-2 | 本段 (tag なし) | 本段 (storechecks) |
| --- | --- | --- | --- |
| `TestCheckerCorpus` 比較 / equal | 17,405 / 17,405 | 17,405 / 17,405 | 17,406 / 17,406 |
| 除外 (root 無し / Pointer panic / 合成 import / Pointer 非決定) | 0 / 1 / 8 / 1 | 0 / 1 / 8 / 1 | 0 / 1 / 8 / **0** |
| token | 0 | 0 | 0 |
| CompareSymbols の組 (fallback) | 554,158 (406) | 554,158 (406) | 554,158 (406) |
| `TestModeCorpus` | 不一致 0 | 4,996 位置、不一致 0 | 同左 |
| `TestPolymorphicAccessors` | 47 method、各側 124,033,376 回、不一致 0 | 同左 | 同左 |
| `TestPointerWalkerBaselines` | 4,671 本、不一致 0 | 4,671 本、不一致 0 | 同左 |

- 除外の file は 2b-2 と同じ。
  - Pointer panic: `pathMappingBasedModuleResolution_rootImport_aliasWithRoot_differentRootTypes.ts#/root/a.ts`
  - 合成 import: 8 file
  - 非決定: `jsxNamespaceImplicitImportJSXNamespace.tsx#/node_modules/preact/jsx-runtime/index.d.ts`
- **storechecks の差**: この回は、非決定の file の Pointer の最初の run が Store と同じ記録で、equal になった (比較が 1 増えた)。
  - 2b-2 の検証報告によれば、この file の Pointer は 20 回で 9 種類の記録を出し、Store の記録はその 1 つと一致する。今回の equal はその範囲の中にある。
  - 段 1 の比較器と除外の規則は変えていない。

### 8.3 生成物と source の範囲

- 生成器 2 つ (`node tools/scripts/tsc/generate.ts`、`(cd tools && GOWORK=off go run ./cmd/storetranslate)`) を回した。
  - 前後で `git status --porcelain` の sha1 (551ac90b…) と `git diff` の sha1 (c398a7cb…) が変わらない (`regen.txt`)。
- `GOWORK=off go build ./...` と `go vet ./internal/ast/... ./internal/store...` は通る。
- 変更の範囲 (before からの差分):
  - tracked: `tsc/internal/storeharness/` の 9 file と、docs の `store-checker-design-20260923.md` (開始時からの 1 行と、TODO 7 への 2b-3 の状態の追記)。
  - 未追跡: `storeharness` の 3 file と `testdata/configurations/`、docs の指示書 2 本 (ユーザーの決定で §3.3 / §3 を改訂) と本報告。
  - Pointer の package・store の production・P2・生成物・go.mod / go.sum には差分が無い。

## 9. 指示書に無い判断

1. **`*testing.T` を取る公開関数を、zero 値の T で隔離して呼んだ** (§3.2)。message は取れないので呼び出し位置を理由にし、正確な message は probe で照合した。
2. **runner-rejected は t.Fatal だけに限った**。準備の中の panic は harness-error にした。
   - runner 自身の準備の panic (例: 最初の `@Filename` の前の内容) も runner を落とす。ただ写しの誤りと区別できないので、要調査として失敗させる。
   - corpus には 0 件。
3. **`skippedTests` の file も列挙した**。
   - 構成は runner-skipped として分母に残した。列挙できない 7 本は input-error で、strict の失敗には数えない。
   - runner は読む前に飛ばすので、runner の挙動との差は無い。
4. **case ID の値は小文字**にし、file の中は ID の順に並べた。構成の順は map の反復順で、実行ごとに変わるため。
5. **FILTER の書式**は次の 3 つ。どれも指示書に無い。
   - 部分一致。
   - `=ID` で完全一致 (無い ID なら失敗)。
   - `;` で複数。
6. **manifest の置き場**は `OUT` から決めた。変数は増やしていない。
7. **mode の比較は、記録が一致した case にだけ行う**。違えば class `mode: …` の mismatch。
8. **harness-error は report でも失敗にした**。段 1 のハーネスの panic と同じ扱い。
9. **非決定の除外のときは、再実行した Pointer の記録と Store の記録も比べる**。結果は `RerunStore` (equal なら、Store は Pointer の変動の中にある)。
10. **段 2 の Pointer の program も bundled lib を共有する** (段 1 の `libCachingHost` と同じ cache)。
    - 再実行のたびに作り直すのは、lib 以外の file と、options・host・file system。
    - runner も lib を共有する。
11. **位置の表示を `of root %d` から `of target %d` に変えた**。段 1 の class の文字列は変えていない。
12. **`TestCheckerConfigurations` は `-short` でだけ skip する**。
    - `go test ./internal/storeharness` を `-skip` なしで回すと、全量 (約 5 分) を strict で回して、合成 import の 118 case で失敗する。
    - 回帰には `-skip '^TestCheckerConfigurations$'` を使う (検証指示書 §6 と同じ)。
13. **run2 を止めた** (ユーザーの決定)。途中の出力は `run2-stopped/` に置き、結果には使っていない。

## 10. 再現の手順

```sh
ROOT=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript
TSC=$ROOT/tsc
OUT=$TSC/internal/ast/docs/_store-checker-7d2b3-impl
SCR=<scratchpad>
cd $TSC && GOWORK=off go build ./... && GOWORK=off go vet ./internal/ast/... ./internal/store...
cd $TSC && GOWORK=off go test -c -o $SCR/harness.test ./internal/storeharness/
# 全構成 (report と strict)
cd $TSC/internal/storeharness && STORE_CHECKER_CONFIG_REPORT=1 STORE_CHECKER_CONFIG_OUT=$OUT/run3.jsonl STORE_CHECKER_CONFIG_TRACE=$OUT/run3.trace $SCR/harness.test -test.run '^TestCheckerConfigurations$' -test.v -test.timeout 60m
cd $TSC/internal/storeharness && STORE_CHECKER_CONFIG_OUT=$OUT/strict.jsonl $SCR/harness.test -test.run '^TestCheckerConfigurations$' -test.v -test.timeout 60m
# 代表 (strict)
STORE_CHECKER_CONFIG_FILTER='=compiler/jsxImportSourceNonPragmaComment.tsx' $SCR/harness.test -test.run '^TestCheckerConfigurations$' -test.v
# fixture と分類
cd $TSC && GOWORK=off go test -count=1 -v -run 'TestConfiguration' ./internal/storeharness/
# runner probe と harness probe (scratch の overlay、追跡外)
cd $TSC && GOWORK=off PROBE_OUT=$SCR/probe.jsonl go test -overlay $OUT/probe/overlay.json -count=1 -run '^TestStoreProbe$' -json ./internal/testrunner > $SCR/probe.json
python3 $OUT/probe/parse_probe.py $SCR/probe.jsonl $SCR/probe.json $SCR/probe-parsed.json
cd $TSC && GOWORK=off PROBE_OUT=$SCR/harness-probe.jsonl go test -overlay $OUT/probe/overlay-harness.json -count=1 -run '^TestHarnessProbe$' ./internal/storeharness
python3 $OUT/probe/compare_probe.py $SCR/probe-parsed.json $SCR/harness-probe.jsonl
python3 $OUT/analyze_run.py $OUT/run3.jsonl $OUT/run3.manifest.jsonl $SCR/probe-parsed.json
# fixture の probe は、両方に PROBE_DIR=$TSC/internal/storeharness/testdata/configurations を付ける
# 合成 import の案 (a): PROBE_SYNTH_CAP=1 PROBE_IDS="$(cat $OUT/synthetic-probe/au-ids.txt)" go test -overlay $OUT/synthetic-probe/overlay.json -run '^TestSynthProbe$' -v ./internal/storeharness
```

コマンド・環境・HEAD・差分の hash・終了コードは、`run.sh` が各実行の `*.cmd.txt` に書いた。

## 11. 判定

- **7d-2b-3 の接続・分類: 完了。**
  - 通常の入力を接続した。全構成を取りこぼしなく処理した (15,205 case + 7 input-error、選ばれない case 0)。
  - 準備を runner probe と全件で照合した。ハーネスの誤り 2 件を直した。
  - 差と未対応を再現できる形で分類した (原因は合成 import と runner の skip の 2 つで、未分類は 0)。
  - 段 1 は維持した (§8)。
- **7d-2b 全体の全構成等価** (§12.5 で更新):
  - 合成 import は (c) で実装し、比べた全構成が一致して、段 1・段 2 の strict が通る。
  - runner-skipped の 1,759 case などを除外とすることの妥当性は、2b 全体として未判断。

## 12. 追補: 合成 import の実装 (案 (c)、2026-10-02)

ユーザーの決定 (2026-10-02) で、§7 の (c) を同じ作業ツリーで実装した。

### 12.1 範囲の変更

本段の指示書 §1 は store の production API を変更範囲に入れていない。この追補はユーザーの指示で範囲を広げたもので、変えたのは次のとおり。

- `ast/store/syntheticimport.go` (新設)
- `ast/store/syntheticimport_test.go` (新設)
- `ast/store/factory.go` (`checkImport` の comment だけ)
- storeharness
- docs (design doc の不変条件 22・25・26 と TODO 7、本報告)

Pointer の package・P2・生成器と生成物・go.mod / go.sum は変えていない。

### 12.2 store

`NewSyntheticImport(file *File, text string) Node` は Pointer の `fileloader.createSyntheticImport` に当たり、specifier の StringLiteral を返す。

- **作り**:
  - Builder で StringLiteral (text は module 名、TokenFlags なし) と、それだけを持つ ImportDeclaration を作る。範囲は (-1, -1)、flags は 0。
  - decl の親は file の root への foreign 辺にする。遅延 JSDoc の root と同じ張り方 (`FinishLazyJSDoc`) で、root の Statements には入らない。
- **Store の区分**:
  - StoreID は program の IDSpace の次の番号。owner も file も持たず、作った時に seal する。
  - これは R16 の `FinishSynthetic` (`ParseIsolatedEntityName`) の Store と同じ区分。新しい区分は増やさず、design doc の不変条件 26 に「program の不変な合成の Store」として 2 つを並べて書いた。`checkImport` のコードは変わらない (owner nil かつ同じ program の Store は、もともと受け入れる)。
- **同一性**: file と役割ごとに 1 つ作り、持つのは呼び出し側 (program)。Pointer の program が map で持つのと同じ。関数は呼ぶたびに新しい組を作る。
- **試験** (`TestNewSyntheticImport`、両 tag で PASS):
  - 形・flags・range、親 (literal → decl → root)、`File()`、root の Statements に無いこと。
  - file のノードが変わらないこと。
  - NodeKey が source・2 回目の呼び出し・別 file のものと重ならないこと。
  - 同じ program の region の Factory の writer (`SetParent`・`SetRange`・`AddFlags`) は panic し、子 (`NewParenthesizedExpression`) と親 (`SetParent` の親) としての取り込みは通ること。
  - 別の program の region からの取り込みは panic すること。

### 12.3 storeharness

- **adapter**:
  - `newProgram` は Store の file を parse した後、Pointer の program が合成 import を持つ file ごとに、`NewSyntheticImport` で作る。順は program の file の順で、file の中では helpers → JSX (fileloader と同じ)。
  - `GetJSXRuntimeImportSpecifier` / `GetImportHelpersImportSpecifier` はそれを返し、渡した回数を数える (`SyntheticAsked`)。
  - 未対応の panic (`syntheticImport`) を撤去した。
- **driver と段 1・段 2**:
  - 除外「synthetic import (stage 2)」と、段 2 の adapter-unsupported の分類 (`classSyntheticImport`・`hasSyntheticSpecifier`) を撤去した。
  - status の adapter-unsupported は schema に残したが、出す所は無い。
  - JSONL に `syntheticAsked` を足した (`syntheticImport` の field は外した)。
- **mode の比較 (`compareModes`)**: 各 file の合成 specifier を、Pointer の specifier と対にして加えた。片側だけにあれば `mode: synthetic import` の mismatch にする。
- **fixture** (24 本・25 case):
  - `jsxDevRuntime.tsx` (react-jsxdev)、`jsxImportSourcePragma.tsx` (`@jsxImportSource` の pragma)、`jsxRuntimeMissing.tsx` (runtime が解決できず、診断は tag に出る)、`importHelpersNodeNext.ts` (`.mts` と `.cts`) を足した。
  - `jsxRuntime.tsx`・`importHelpers.ts` と合わせて 6 本で、次を確かめる。
    - equal であること、checker が合成 specifier を受け取ったこと (`SyntheticAsked > 0`)、合成 specifier の数と mode の比較の位置の数。
    - 返したノードの text・親・`File()`、2 回目も同じノード (`SameNode`) であること、役割の違う側は nil であること。
    - 合成 import の無い program では何も作らないこと。
  - `TestConfigurationClasses` の「Pointer に無い合成 import」の case は撤去した。

### 12.4 結果

| 実行 | 結果 |
| --- | --- |
| 段 2 report (`run4`) | 15,205 case = equal **13,446**、runner-skipped 1,759、mismatch・runner-rejected・adapter-unsupported・pointer-excluded・harness-error **0**。wall 3m45s。exit 0 |
| 段 2 strict (`run4-strict`) | **PASS** (exit 0)。全 case の status と class は report と同じ |

照合 (`run3-vs-run4.txt`):

- run3 の adapter-unsupported 118 case と、run4 で Store checker が合成 specifier を受け取った case の集合が一致した。118 case とも equal。
- それ以外の case で status か class が変わったものは 0。
- Pointer の program に合成 specifier を持つ case は 139 で、そのうち 21 は checker が問い合わせない (§6.4)。この 139 case の mode の比較の位置は、run3 の 17 から run4 の 541 に増えた。合成 specifier と、比べられるようになった 118 case の木の位置の分。全体では 8,213 位置。
- 件数 (equal の合計、両側で同じ): 生の診断 38,684、suggestion 32,739、global 87、型の記録 743,222、symbol の記録 743,306、異なる symbol 217,732。
- CompareSymbols は各側 300,893 組 (fallback 407)、違反 0。token 0、Pointer の再実行 0。

両 tag の一式 (`-skip '^TestCheckerConfigurations$'`) の結果は次のとおり。

| 一式 | 結果 | log |
| --- | --- | --- |
| tag なし | 失敗は既知の `FuzzEquivalence/a708303f611b95c8` だけ | `test-notag-synth.txt` |
| storechecks | 同上 | `test-storechecks-synth.txt` |

- 新しい `TestNewSyntheticImport` と `TestConfiguration*` は PASS。
- 段 1 の `TestCheckerCorpus` (strict) は、合成 import の除外が無くなった。

  | | tag なし | storechecks |
  | --- | --- | --- |
  | 比較 / equal | 17,414 / 17,414 | 17,413 / 17,413 |
  | 除外 (Pointer panic / 非決定) | 1 / 0 | 1 / 1 |

  - 非決定の file は、tag なしの回は equal、storechecks の回は除外になった (§8.2 と同じ揺れ)。
  - 前は除外していた合成 import の 8 file は、両方の回で equal。
  - CompareSymbols は各側 554,182 組 (fallback 406)、token 0。
- `TestModeCorpus`・`TestPolymorphicAccessors`・`TestPointerWalkerBaselines` の件数は §8.2 と同じで、不一致 0。
- 一式の開始の 37 秒後に `factory.go` の comment を直した。comment だけの変更だが、記録をそろえるため、`./internal/ast/store/...` を両 tag で回し直して PASS (`test-store-*-final.txt`)。
- 生成器 2 つを回しても、`git status` と diff の sha1 は変わらない (`regen-synth.txt`)。

### 12.5 判定の更新

- **7d-2b の全構成等価**: 比べた全構成 (段 2 の 13,446 case、段 1 の比較できる全 file) で一致し、strict が段 1・段 2 とも通る。
- 残る除外は次の 3 つで、その妥当性が 2b の完了の判断として残る。
  - runner の規則による runner-skipped 1,759 case と、runner が読まない 7 file
  - 段 1 の Pointer panic 1 file
  - 段 1 の Pointer 非決定 1 file (回によって equal になる、§8.2)
- default lib の等価を構成ごとに確かめる別のテストの要否 (§6.6) も未決のまま。
