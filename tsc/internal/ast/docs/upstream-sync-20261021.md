# upstream 追従の台帳 (2026-10-21)

store-ast (80e3068407、base f29aeb9f82) を upstream/main 5733ab05f5 へ merge で追従する。作業 branch は `store-ast-sync`。

- 計画: 段は 10。merge commit には Pointer 側の解決だけを入れ、Store の追従は後続の commit で行う。
- 承認の単位: upstream の ast PR を Store 側でどう扱うか。各段の開始時に承認を取る。
- 段の状態はこの台帳を正とする。

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
| e8c1ac1603 | `ast.GetSourceFileOfSymbol`・`SourceFile.IsContentMapped` を足す (api 用) | 無し。checker から到達したら翻訳し、Symbol の形は S5 に従う | |
| 81d3f3f5e8 | `IsInExpressionContext` が関数式・クラス式の名前を式から外す | 翻訳する (store/utilities.go) | |

### `--astBackend` の載せ直し (e8fd69fc1e)

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
