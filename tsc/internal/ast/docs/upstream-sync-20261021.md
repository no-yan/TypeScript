# upstream 追従の台帳 (2026-10-21)

store-ast (80e3068407、base f29aeb9f82) を upstream/main 5733ab05f5 へ merge で追従する。作業 branch は `store-ast-sync`。

- 計画: 段は 10。merge commit には Pointer 側の解決だけを入れ、Store の追従は後続の commit で行う。
- 承認の単位: upstream の ast PR を Store 側でどう扱うか。各段の開始時に承認を取る。
- 段の状態はこの台帳を正とする。
- storetranslate の設定 (共有の一覧・root) を upstream の改名や新しい呼び出しに合わせるときは、進めてから段の報告に書く。変換の規則そのものを変えるときは、先に承認を取る (2026-10-21 ユーザー決定)。

古くなる P2 と Mirror・Ref の一覧は、scratchpad の `p2stale` で作った。storetranslate の `manual.go` と同じ切り出しで hash を取るもので、store-ast では 124 個の hash を全部再現する。

- `go run . <repo> <rev>`: その rev で古くなる P2 を出す。
- `go run . diff <repo> <from> <to>`: 変わった Pointer 宣言を出し、次の 2 つに突き合わせる。
  - Mirror: 手書きファイルまたは P2 にある同名の宣言。
  - Ref: P2 の本体が名前で参照している宣言。

## S1a: f29aeb9f82..7a418fec4b8 (#1〜#62)

### ast・parser・scanner・binder の PR と Store 側の扱い

| PR | Pointer の変更 | Store 側の扱い | 承認 |
|---|---|---|---|
| 1ac2d5aed0 incremental codegen | generate.ts が `force` を取る。各 generator が `GeneratedFile(...).isCurrent(force)` と `markCurrent` を使う | generate.ts は upstream を取り、`generateGoStore(force)` を足す。generate-go-store.ts を `GeneratedFile` 化する (入力は自身・schema.ts・ast.json) | 2026-10-21 承認 |
| 87f2e8ca9b AST helpers | ast.json の `DeclarationName` union に PropertyAccessExpression が入る | 無し (generator は union を読まない) | 2026-10-21 承認 |
| b7fb5e47a3 diamond | ast.json で TypeElementBase・ClassElementBase・ObjectLiteralElementBase が DeclarationBase を extends する | Store AST を再生成する。DeclarationBase を新しく得る kind に Symbol の bound slot が増える。再生成の diff でその kind を確かめる | 2026-10-21 承認 |
| 0681ef7fa3 JSX recovery | `parseJsxAttributeValue` が `mustBeUnary=true` を渡す | storeparser の同じ関数に引数を 1 つ手で移植する | 2026-10-21 承認 |
| f34fb210f7 | `IsTypeDeclaration` の修正 | 翻訳する (store/utilities.go) | 2026-10-21 承認 |

### 診断 code (2026-10-21 承認)

`--astBackend` の 4 メッセージは、55e92ac9b1 で `diagnosticMessages.json` に移す。code は次のとおり。

- 18990: option の説明 (Message)
- 18991〜18993: error

upstream は 18100 から順に足している (upstream/main で 18124)。ファイルの末尾は避けて入れる。

### 古くなる P2 (5)

- checker: `Checker.checkKindsOfPropertyMemberOverrides` (checker_manual.go)
- compiler:
  - `Program.getBindAndCheckDiagnosticsWithChecker` (diagnostics_manual.go)
  - `Program.verifyCompilerOptions` (options_manual.go)
  - `ProgramOptions` (loader_manual.go)
  - `filesParser.getProcessedFiles` (loader_manual.go)

### Mirror (手書きの移植)

- storeparser `Parser.parseJsxAttributeValue`

### Ref (P2 が参照する、変わった宣言。手順 6 で読む)

- checker: `Checker` (struct)、`getReducedType`、`getTemplateLiteralType`、`PropertiesTypesKey`
- compiler:
  - `Program.getDiagnosticsWithPrecedingDirectives`
  - `fileLoader` (struct)、`fileLoader.resolveImportsAndModuleAugmentations`
  - `getCheckerAssociationsInOrder`、`processAllProgramFiles`、`processedFiles`
- printer: `Printer.emitExpression`

### storeharness が写したファイル

compiler_runner.go・harnessutil.go・test_case_parser.go・type_symbol_baseline.go は、この範囲で変わらない。

### テキスト衝突 (merge commit で解決する)

- generate.ts
- 診断: extraDiagnosticMessages.json は 55e92ac9b1 で削除された。diagnostics_generated.go は 52843676b4 で変わった。
- generateTrace baseline 2 本 (75e50a46dd で es2026 になった)

### 結果 (2026-10-21、合格)

commit:

- 20264a79b4e merge
- 65ded5b2b6e Store AST の再生成 (generate-go-store.ts の GeneratedFile 化)
- bb0ea4ca8c6 storeparser の JSX 修正
- 98c9b4d26b2 再翻訳と P2 5 件
- c70df16fd46 generateTrace baseline

台帳と予測の違い・計画外の判断:

- b7fb5e47a3 で増えたのは bound slot ではなかった。`HasDeclarationData` の kind 一覧に JsxSpreadAttribute・NotEmittedTypeElement が入っただけで、shapes は変わらない。
- 診断 code の決定は計画では S1b だったが、55e92ac9b1 が S1a にあるので S1a で決めた。
- storetranslate の設定を 2 か所変えた。どちらも upstream への追従で、新しい規則ではないと判断した。
  - 共有の一覧に `DefaultResolver` を足した。6492ef896e8 で resolver の struct が `module.DefaultResolver` に改名され、`module.Resolver` は interface になったため。
  - root に `checker.Checker.WasCanceled` を足した。P2 の `getBindAndCheckDiagnosticsWithChecker` だけが呼ぶため。
- Ref 10 件を読んだ。どれも呼び出しか型としての使用で、本体を写したものは無い。
- lint: upstream の新規則 (`casebody` の「break だけの case」・「top level の break」) に当たるものは無い。store のコードに残る指摘 242 件は store-ast からある分で、旧 `emptycase` と同じ「空の case」5 件もその中に含まれる。

検証 (scratchpad/S1a/stage-end.log):

- build・vet は両 tag で通った。
- storetranslate の試験 (TestManualHash を含む) が通った。
- 生成器 3 つを 2 回目に回しても差分は無い。
- store package の試験は両 tag で通った。既知の fuzz a708303f は skip した。
- storeparser・storebinder の `TestEquivalence`・`TestCorpus` が通った。
- Pointer 側の tsoptions・diagnostics・tsctests が通った。
- storeharness:
  - `TestCompilerProjectEquivalence`・`TestModeCorpus` が通った。
  - 段 1 strict: 17,447 件。equal 17,445、既知の除外 2 (Pointer 非決定 1・Pointer panic 1。どちらも 7d-2b から同じ)。
  - 段 2: 15,231 件。equal 13,472、runner-skipped 1,759。

## S1b: 7a418fec4b8..e8fd69fc1e (#63〜#89)

### ast・parser・scanner・binder の PR と Store 側の扱い

| PR | Pointer の変更 | Store 側の扱い | 承認 |
|---|---|---|---|
| e8c1ac1603 | `ast.GetSourceFileOfSymbol`・`SourceFile.IsContentMapped` を足す (api 用) | 無し。checker から到達したら翻訳し、Symbol の形は S5 に従う | 2026-10-21 承認 |
| 81d3f3f5e8 | `IsInExpressionContext` が関数式・クラス式の名前を式から外す | 翻訳する (store/utilities.go) | 2026-10-21 承認 |

### `--astBackend` の載せ直し (e8fd69fc1e、2026-10-21 承認)

upstream は option の宣言を `tools/scripts/tsc/options.ts` から生成するようになった。`declscompiler.go` は削除され、`compileroptions.go`・`enummaps.go`・`parsinghelpers.go` の中身は生成物に移った。こちらの手書きの 5 か所を、`options.ts` の 1 エントリと enum・enumMap に置き換える。

- `options-model.ts`: `CompilerOptionType` に `"ASTBackend"` を足す。
- `options.ts`:
  - compilerOptions: `{ name: "astBackend", goName: "ASTBackend", type: "ASTBackend", internal: true, declaration: { group: "optionsForCompiler", isCommandLineOnly: true, category: Command-line Options, description: 18990, defaultValueDescription: "pointer" } }`
  - enums: `ASTBackend` (None=0・Pointer=1・Store=2)。API には出さない (`api` を付けない)。
  - enumMaps: `astBackend` → `astBackendOptionMap` (pointer・store)
  - declarationOrder.optionsForCompiler に `"astBackend"`。help は category の中で名前順なので、位置は出力に影響しない。
- `showconfig.go` の手書きの除外は、生成器が Command-line Options を除くので不要になる。

### 古くなる P2 (12)

- compiler (program.go): `NewProgram`、`Program`、`Program.GetSymlinkCache`、`Program.initCheckerPool`、`Program.verifyCompilerOptions`、`ProgramOptions`
- compiler (fileloader.go・filesparser.go): `fileLoader.addProjectReferenceTasks`、`fileLoader.parseSourceFile`、`filesParser.getProcessedFiles`、`parseTask.load`
- compiler (includeprocessor.go): `includeProcessor`、`includeProcessor.getDiagnostics`

ほぼ a5c43c4d54 (Program の options の寿命) による。

### Ref (P2 が参照する、変わった宣言。手順 6 で読む)

compiler の 41 宣言。P2 の本体から消えた宣言を呼んでいるのが 3 つある。

- `includeProcessor.addProcessingDiagnostic` (options_manual.go)
- `includeProcessor.addProcessingDiagnosticsForFileCasing` (loader_manual.go)
- `ProgramOptions.canUseProjectReferenceSource` (loader_manual.go)

これらは `fileIncludeData`・`ProgramConfig` に移った。

### storeharness が写したファイル

- compiler_runner.go: `compilerVaryBy` が消え、options.ts から `testrunner/options_generated.go` に生成されるようになった (非公開)。
  - storeharness `configprep.go` の写しを、生成された集合に合わせて書き直す。
- harnessutil.go:
  - `GetFileBasedTestConfigurations` が `*collections.Set[string]` を取る。
  - `SetOptionsFromTestConfig` が parse error で止まらなくなった。
  - 宣言の診断を集める位置が変わった。

### テキスト衝突 (merge commit で解決する)

- program.go: `p.opts.Tracing` を `p.hosts.Tracing` にする (a5c43c4d54)。
- compileroptions.go・tsoptions 4 ファイル (うち declscompiler.go は upstream で削除) は upstream を取り、上の載せ直しをする。

### 結果 (合格)

commit:

- 17467c1639b merge
- 603a7829274 再翻訳と P2 12 件
- b7a48d0db74 storeharness の compilerVaryBy
- 4e1b2e1fab7 storeclitest の graph

計画外の判断:

- upstream の試験 2 本 (`TestShowConfigEnumSerialization`・`options.test.ts` の enum の大小文字) は、「enum の option はすべて設定ファイルの option」と仮定していた。`--astBackend` は最初の command line 専用の enum option なので、2 本とも command line 専用の option を飛ばすようにした。
  - 生成器を変えて全部の enum に serializer を出す案も試した。生成器と生成物を upstream と同じに保つため、試験の側で揃えた。
- `ProgramOptions` の分割 (a5c43c4d54) では、`idSpace` を置くために `ProgramConfig` を P2 にした。`fileLoader.opts` と `Program.opts` の両方から読めるためである。
  - `ProgramFactories` も P2 にして、`CreateCheckerPool` を外した。
  - `ProgramHosts` は翻訳に任せた。
- storeharness の `compilerVaryBy` は写しをやめた。`testrunner/options_generated.go` を実行時に go/parser で読む。
  - upstream は、この一覧を options.ts のメタデータから生成するようになった。実行時に計算し直す材料は無い。
  - 写すと、検知できない写し (計画の穴 d) が 1 つ増える。
  - 新しい一覧には `all`・`diagnostics`・`listfiles` などが入り、旧版と中身が違う。
- storeclitest の `graph.go` は `FileIncludeReason.data` を reflect で読んでいた。a5c43c4d54 で `data` が型ごとの field に分かれたので、struct の全 field を出すようにした。
  - 段の終わりの検証で `TestNativeGraph` の panic として見つかった。
  - 検証スクリプトのラッパーが終了コードを返していなかったので、それも直した。
- `tools/scripts/gen/cache.test.mts` の 1 本は macOS の `/var` → `/private/var` で落ちる。環境依存で、こちらの変更とは関係ない。

検証 (2026-10-21、合格、scratchpad/S1b/stage-end.log): S1a と同じ項目に加え、`options.test.ts` (24 本) が通った。

- 段 1 strict: 17,475 件。equal 17,473、既知の除外 2。
- 段 2: 15,249 件。equal 13,490、runner-skipped 1,759。

## S2: e8fd69fc1e..2f9fd09a10 (#90〜#96)

### ast・parser・scanner・binder の PR と Store 側の扱い

| PR | Pointer の変更 | Store 側の扱い | 承認 |
|---|---|---|---|
| 2f9fd09a10 source phase imports | `KindSourceKeyword` を DeferKeyword の後に足す (LastKeyword・LastToken・LastContextualKeyword を兼ね、以降の Kind の値が 1 ずれる)。ImportClause の phase に `source` を許す。ast に `IsImportPhaseMetaProperty`・`IsSourcePhaseImport(Call)` など。scanner の keyword 表に `source`。checker・printer・grammarchecks も変わる | (1) Store AST を再生成する (表は Kind の記号名で引くので、値のずれは安全)。(2) storeparser の 6 宣言を手で移植し、新しい helper 3 つ (`currentImportPhaseModifier`・`shouldParseImportPhaseModifier`・`parseImportMetaPropertyName`) を足す。`tokenAfterImportedIdentifierDefinitelyProducesImportDeclaration` は `...AllowsImportEqualsDeclaration` への改名に従う。(3) storescanner・store/utilities.go・checker・printer は翻訳する。(4) `CallExpression.computeSubtreeFacts` の変更は対象外 (Store に SubtreeFacts は無く、読むのは emit の transformer だけ) | 2026-10-21 承認 |

### 古くなる P2 (1)

- checker: `NewChecker` (checker_manual.go)

### Mirror (手書きの移植)

storeparser の `isStartOfStatement`、`parseImportDeclarationOrImportEqualsDeclaration`、`parseLeftHandSideExpressionOrHigher`、`scanStartOfDeclaration`、`tryParseImportClause`、`tokenAfterImportedIdentifierDefinitelyProducesImportDeclaration` (改名)。新規は `currentImportPhaseModifier`・`shouldParseImportPhaseModifier`・`parseImportMetaPropertyName`。

### Ref

- checker の `Checker` (struct)、compiler の `fileLoader.resolveImportsAndModuleAugmentations`: 呼び出しだけ (手順 6 で確認)。
- ast の `_` は名前の誤一致。

### storeharness が写したファイル・lint

変化なし。

### テキスト衝突

診断の生成物 2 つ (`diagnosticMessages.generated.json`・`diagnostics_generated.go`)。upstream 側を取って再生成する。

### 結果 (合格)

commit:

- 3d62114322f merge
- a0f56d7ba67 Store AST の再生成 (token の case に SourceKeyword)
- 6c536e2b737 storeparser の移植
- 3ea84492ac7 再翻訳 (`NewChecker` の P2、`roots.txt` に `IsImportPhaseMetaProperty`)
- 8361265c68f storeharness の adapter

計画外の判断・見つかったこと:

- storeharness の測定用 adapter (`mode.go`) の `GetResolvedModuleFromModuleSpecifier` は、Pointer の写しだった。2f9fd09a10 で入った「source phase import は nil」の判定が欠けていた。
  - 段の終わりの `TestModeCorpus` が、importSource7・19・20 の不一致 6 件として見つけた。
  - これは計画の穴 (d) (harness の写し) に当たる。
  - これを受けて、p2stale の Mirror の突き合わせに storeharness・storeclitest の手書きファイルを加えた。S1a・S1b の範囲を当て直すと、新しく出たのは adapter の委譲 3 つ (`BindSourceFiles`・`GetNearestAncestorDirectoryWithPackageJson`・`GetPackageJsonInfo`) だけで、写した本体は無かった。
- `roots.txt` に `IsImportPhaseMetaProperty` を足した。storeparser が呼ぶ ast 関数は root に載せる、という既存の取り決めに従った。
- `CallExpression.computeSubtreeFacts` の変更は、承認どおり対象外にした。

検証 (2026-10-21、合格、scratchpad/S2/stage-end.log):

- 段 1 strict: 17,569 件。equal 17,567、既知の除外 2。Pointer の非決定の 1 件は、今回は `MessageArgs` の差として出た。ファイルは同じ。
- 段 2: 15,296 件。equal 13,537、runner-skipped 1,759。
- upstream が足した `importSource*` の試験は、corpus と段 2 に入っている。

## S3: 2f9fd09a10..ed4807212c (#97〜#103)

### ast・parser・scanner・binder の PR と Store 側の扱い

| PR | Pointer の変更 | Store 側の扱い | 承認 |
|---|---|---|---|
| ed4807212c strongly typed file paths | path を `tspath.RootedFilePath`・`tspath.PathKey` などの型で区別する。`ast.HasFileName` は `FileName() RootedFilePath`・`PathKey() PathKey` になり、`SourceFile.Path()` は `PathKey()` に改名する。`SourceFileParseOptions` は `FileName`・`PathKey` を持つ。新しい lint `typedpaths` が型の変換を検査する (483 ファイル) | (1) 手書きの `store/file.go` の `File` を新しい `HasFileName` に合わせ、`Path()` を `PathKey()` に改名する。Pointer の `SourceFile` と同じ形にする。(2) storeparser (`initializeState`・`parseSourceFileWorker`)・storebinder (`bindSourceFileAsExternalModule`)・storecompiler の手書きファイル・storeharness・storeclitest を、コンパイルが通るよう直す。(3) P2 19 件を移植し直し、消えた `compilerHost.GetCurrentDirectory` の P2 を削除する。(4) 翻訳される側は storetranslate に任せる。(5) 手書きの store のコードにも `typedpaths` をかけ、upstream と同じ規則に従わせる | 2026-10-21 承認 |

### 古くなる P2 (19)、消える宣言 (1)

- checker: `Program` (interface)
- compiler:
  - `CompilerHost`、`compilerHost`、`NewCompilerHost`、`NewCachedFSCompilerHost`、`compilerHost.DefaultLibraryPath`、`compilerHost.GetResolvedProjectReference`
  - `Program`、`ProgramConfig`、`NewProgram`、`Program.GetSymlinkCache`、`Program.verifyCompilerOptions`
  - `fileLoader.addProjectReferenceTasks`、`fileLoader.parseSourceFile`、`filesParser.getProcessedFiles`、`parseTask.load`
  - `includeProcessor`、`FileIncludeReason.toRelatedInfo`、`sourceFileMayBeEmitted`
- 消える: `compilerHost.GetCurrentDirectory` (CompilerHost interface から外れた)

### Mirror (手書きの移植)

- storeparser: `initializeState`、`parseSourceFileWorker`
- storebinder: `bindSourceFileAsExternalModule`
- ast/store の手書き (名前が違うので自動では拾えない): `store/file.go` の `File` (Pointer の `SourceFile.FileName`・`PathKey`・`OriginalFileName`・`VirtualFileName`・`hasFileNameImpl`)
- storeharness の adapter (`adapter.go`・`mode.go`): checker の `Program` interface を実装する 27 の method。大半は Pointer への委譲。`GetModeForUsageLocation`・`GetEmitSyntaxForUsageLocation` は storecompiler の関数を呼ぶ。

### Ref

compiler など 68 件。手順 6 で、写した本体があるかを確かめる。

### storeharness が写したファイル

- `harnessutil.go` (+/- 317)
- `compiler_runner.go` (41)
- `test_case_parser.go` (22)
- `type_symbol_baseline.go` (6)。これは storeharness `pointerwalker.go` の写し元。

それぞれの差分を、写しと突き合わせる。

### テキスト衝突

- program.go: こちらの `bindSourceFile` の `file.Path()` を `file.PathKey()` にする。
- `core/options_generated.go`: 生成物。upstream を取って再生成する。

### 結果 (合格)

commit:

- 39ec4fd967f merge。こちらの AST phase benchmark と fixture の型付き path、API の TS 型 (`proto.generated.ts`) の再生成を含む。
- b6a6d69f096 store.File・storeparser・storebinder
- dfa37beeb6a 再翻訳と P2 19 件 (+ 削除 1)、`storeprogram.go`・`execute/check.go`
- 3704e3e7ed9 storeharness・storeclitest・各 package の試験
- ace9147622b `typedpaths` の指摘 1 件
- 083e8d86a03 harness の Pointer program の base directory

見つかったこと・計画外の判断:

- 段の終わりの検証の 1 回目は「PASS」だったが、段 1 の 17,581 件がすべて「Pointer panics」で除外されていて、何も比べていなかった。
  - 原因: harness が `ParsedCommandLine` を literal で作っていて base directory が無く、ed4807212c の resolver が panic した。`tsoptions.NewParsedCommandLine` に直した。
  - harness は除外を失敗にしないので、検証スクリプトの最後に確認を足した。除外は既知の 2 件まで、段 1 の equal は 17,000 以上、段 2 の equal は 13,000 以上。
  - S1a〜S2 の数値は、equal の件数から実際に比べていたことが分かる。
- `gen-proto` の変更で、API の TS 型 `RawCompilerOptions` に `astBackend` が出るようになった。upstream の他の internal option (`checkers`・`quiet` など) と同じ扱いなので受け入れ、merge commit を amend して入れた (push 前の直前の commit)。
- こちらが Pointer 側に足した試験 (AST phase benchmark・kperf) は、merge commit の確認でビルドしていなかった。段の終わりの検証に、これらの package の vet を足した。
- harness の fixture (Pointer の program の答えを記録して再生するもの) は repo に記録されていない。なので schema を新しい型 (`CaseSensitivity`・`BaseDirectory` など) に変えた。
- `configprep.go` の写し元の revision を ed4807212c に更新した。upstream は option を絶対 path にする処理を harness から消したので、写しからも消した。
- 内部ディスクが満杯になった (Go のビルドキャッシュ 30GB)。ユーザー決定で、Go の GOCACHE・GOTMPDIR を SanDisk に向けた。

検証 (2026-10-21、合格、2 回目):

- 段 1 strict: 17,581 件。equal 17,579、既知の除外 2。
- 段 2: 15,299 件。equal 13,540、runner-skipped 1,759。
- mode: 17,581 ファイル・5,120 か所で、不一致 0。Pointer の panic 1 は既知の 1 件。
- project: 272,046 の対。Store と Pointer の件数は一致した。
- 検証スクリプトに確認を足した。
  - 段 1・段 2: 除外の件数と equal の下限。
  - mode: Pointer の panic の件数と位置の下限。

## S4: ed4807212c..c0ca4744c91 (#104〜#130)

### ast・parser・scanner・binder の PR と Store 側の扱い

| PR | Pointer の変更 | Store 側の扱い | 承認 |
|---|---|---|---|
| afd02f1486 | `GetTypeAnnotationNode` が JSDocParameterTag を扱う | 翻訳する (store/utilities.go) | |
| bea2e849e9 | `tryParseImportClause` が `defer` にも名前空間の束縛を要求する | storeparser の同じ関数に手で移植する | |

### 古くなる P2

0 件。

### その他

- 253bcd86231 で `printer.EmitContext.Reset` と `GetEmitContext` が消え、emit context は呼び出しごとに `NewEmitContext()` で作るようになった。
  - storetranslate の root の `printer.EmitContext.Reset` を外す (upstream の削除への追従)。
  - storeharness の `pointerwalker.go` (type_symbol_baseline.go の写し) と `walker.go` を、同じ形に合わせる。
- storeharness が写したファイル: compiler_runner.go に `-run` の filter が入った。configprep は列挙に使わないので対象外。

### テキスト衝突

診断の生成物 2 つ。upstream を取って再生成する。
