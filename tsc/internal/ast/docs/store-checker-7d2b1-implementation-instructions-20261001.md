# Store checker 等価 段 1 (TODO 7d-2b-1) 実装指示書

作成日: 2026-10-01。対象は実装を担当するエージェント。

## 前提の文書

| 呼び名 | 文書 |
| --- | --- |
| 残りの設計 | [store-checker-7d2-design-remainder-20260929.md](store-checker-7d2-design-remainder-20260929.md) |
| 7d 設計 | [store-checker-design-20260923.md](store-checker-design-20260923.md) (H1・Q1 は §2.1・§2.7) |
| レビュー契約 | [store-checker-review-contracts-20260926.md](store-checker-review-contracts-20260926.md) |
| 2a 実装報告 | [store-checker-7d2a-implementation-report-20260930.md](store-checker-7d2a-implementation-report-20260930.md) (§8.1 が本書の §3) |
| 検証指示書 | [store-checker-7d2b1-verification-instructions-20261001.md](store-checker-7d2b1-verification-instructions-20261001.md) |

- before は **5c0e86b580** (7d-2a)。
- 設計判断は上の文書と本書で済ませてある。書かれていない判断が必要になったら、実装を進めずに報告する。
- commit はしない。

## 0. 作るものの性格

**2b は初めて checker を動かす段で、2 つの小段に分ける (2026-09-30、ユーザー)。**

- **2b-1 (本書)**: 次の 4 つを作り、corpus を回して不一致を原因ごとに分類し、報告する。
  1. ハーネス
  2. 2b の最初の P2 (`checkIfExpressionRefinesParameter`)
  3. walker と比較器
  4. symbol の対応
- **2b-2**: 2b-1 の分類を見て、直し方を決めてから書く。

**入力も 2 段に分ける (2026-09-30、ユーザー)。**

| 段 | 入力 | program |
| --- | --- | --- |
| **段 1 (本書)** | 2a と同じ corpus (fixtures と tests/cases の unit、17,415 file) | 1 file ずつ、本書 §5.1 の option で作る |
| 段 2 (後の指示書) | testrunner の compiler / conformance の全構成 (約 13.4k) | multi-file、@option、symlink を含む |

段 2 を後に回す理由は 2 つある。

1. **testrunner は Pointer の `compiler.Program` 前提で作られている。**
   - 列挙と構成の展開は unexport。
   - `harnessutil.CompileFiles` は program を 2 つ作り、Pointer の checker で診断を集めて emit まで走らせる。
   - program の中の checker pool に、Store の checker を入れる口が無い。
2. **差分が大きすぎる。** 初めて動かす checker を、multi-file・module 解決・JSX の合成 import・全構成に一度にかけると、不一致の原因を切り分けられない。テストが変更の正しさの指標として働かない。

**2b-1 では、見つけた不一致を直さない。** 直してよいのは次の 4 つだけ。

- §3 の P2
- §4 の根の追加
- ハーネス自身のバグ
- 変換器の §3.2 の拡張

変換の規則・ほかの P2・store の API の誤りは、自明に見えても直さず、分類して報告する (§7)。これが 2b-1 と 2b-2 の分け方そのもの。

**作らないもの**

- 段 2 の testrunner への接続。
- 合成の import specifier (`GetJSXRuntimeImportSpecifier`・`GetImportHelpersImportSpecifier`、§5.2)。
- 文字列に埋め込まれた ID の token (`#<id>@`・`__@name@<id>`) を、対応済みの symbol と照合する検査 (レビュー契約 §2 の 3)。
- `compareSymbols` の fallback の性質の検査と、採番順の trace (同 §2 の 4)。これらは 2b-2 に回す (§11 の 6)。
- 性能の計測 (2c)。

## 1. ガード

1. **変更してよいもの**
   - `tools/cmd/storetranslate/` (§3.2、§4)。
   - 再生成した生成物。
   - `tsc/internal/storechecker/` の P2 (`*_manual.go`)。§3 の 3 つの関数と、`checkIfExpressionRefinesParameter` の今の panic の置き換えだけ。
   - 新設の `tsc/internal/storeharness/` (§2)。
   - `tsc/internal/ast/docs/`。
2. **変更しないもの**
   - `tsc/internal/ast/store/`・`storeparser/`・`storebinder/` の production のコード。変換器の出力が変わる場合を除く。変わったら報告に書く。
   - Pointer の package (`ast` (store 以外)、`checker`、`printer`、`binder`、`parser`、`compiler`、`testrunner`、`testutil` など)。
   - `tsc/go.mod`、`tools/go.mod`。
   - production (store 系でも storeharness でもない package) から、store 系の package と `storeharness` を import しない。
3. 既存のテストは除外と緩めを増やさずに通す。既知の失敗は `FuzzEquivalence/a708303f611b95c8` の 1 件だけ。
4. 生成物は `node tools/scripts/tsc/generate.ts` と `(cd tools && go run ./cmd/storetranslate)` で再現すること。
5. `unsafe` を増やさない。
6. 一時ファイル・binary・大きな出力は scratchpad に置く。build は `GOWORK=off`。

## 2. ファイル

```
tools/cmd/storetranslate/        §3.2 hash の複数行と store-only、§4 根の追加
tsc/internal/storechecker/       §3 P2 (flow_manual.go か checker_manual.go)
tsc/internal/storeharness/       新設
  program.go                     §5.1 Pointer program と Store の file の組み立て
  adapter.go                     §5.2 storechecker.Program の実装
  mode.go                        §5.2 の C: module の mode の 3 method (fileloader の手書きの写し)
  driver.go                      §6.1 両側を同じ順で呼ぶ driver
  walker.go                      §6.3 type / symbol の walker (Store 用、手書き)
  compare.go                     §6.2・§6.4 比較器と分類
  *_test.go                      §8
  testdata/private/              §6.5 の fixture
```

## 3. `checkIfExpressionRefinesParameter` の P2 (2a 実装報告 §8.1)

### 3.1 P2 の中身

2a 実装報告 §8.1 の決定どおりにする。**合成の flow を作らず、一番上の条件を直接評価する。**

根拠 (2a 実装報告 §8.1 の要約):
- Pointer の checker が flow node を作るのは、この関数の 3 行 (checker.go:20923・20925・20932) だけ。
- 合成の flow は起点にしか現れない。
- `getTypeAtFlowNode` は、Condition の枝で `getTypeAtFlowCondition` を呼んで返すだけ。そこで flow から読むのは `Flags`・`Node` (= `expr`)・`Antecedent` だけ。
- `Shared` が立たないので `sharedFlows` にも入らない。
- antecedent が nil のときの Start は `f.initialType` を返す。

よって置き換えは厳密に等価。P2 は 3 つ。

1. **`checkIfExpressionRefinesParameter`** (置き換え)
   - Pointer と同じく antecedent を求める。
   - 下の 2 を、`assumeTrue` を真と偽にして 2 回呼ぶ。
   - 返り値の判定は Pointer のまま。
2. **`getFlowTypeOfReferenceUnderCondition`** (store-only)
   - `getFlowTypeOfReferenceEx` の写し。
   - 違いは、`getTypeAtFlowNode(f, flowNode)` の呼び出しを、下の 3 の呼び出しに差し替えることだけ。
3. **`getTypeAtSyntheticCondition`** (store-only)
   - `getTypeAtFlowNode` の depth の増減を写し、その中で `getTypeAtFlowCondition` の本体を写す。
   - flow の `Flags`・`Node`・`Antecedent` の読みを、引数の `assumeTrue`・`expr`・antecedent に置き換える。
   - antecedent が nil なら `FlowType{t: f.initialType}` を返す。

写した部分は、Pointer の文の順序・early return・`f` の状態の読み書きを変えない。

### 3.2 変換器の拡張

今の変換器は、P2 の宣言ごとに hash の行を 1 つだけ読む。そして P2 の名前が、hash の指す宣言の名前 (か `<名前>List`) と一致することを要求する (manual.go:103-126)。これを次のように拡張する。

- **P2 の宣言は hash の行を 1 つ以上持つ。** 全部の行の hash を、Pointer の宣言と照合する。どれか 1 つが変われば、今と同じ `the Pointer declaration … changed` で止める。
- **置き換えの P2 (印が無いもの)**: hash の行のうち、ちょうど 1 つが、P2 と同じ名前 (か `<名前>List`) の宣言を指すこと。
- **store-only の P2**: doc comment に `// storetranslate:store-only` の行を持つ。
  - 名前が Pointer の元の package に無いこと。あれば「Pointer の宣言を名指している、置き換えにせよ」で止める。
  - hash の行は、写した宣言を指す。
- 印の無い P2 の名前が Pointer に無いときは、今と同じく止める。打ち間違いが黙って補助として通るのを防ぐため。
- 到達計算は、store-only の P2 の本体の参照も、置き換えの P2 と同じく辿る。
- **テスト** (`translate_test.go`) に次の 4 つを足す。
  1. 2 行目の hash だけが変わったら止まる。
  2. 印の無い P2 の名前が Pointer に無いと止まる。
  3. 印の付いた P2 の名前が Pointer にあると止まる。
  4. store-only の本体の参照が到達計算で辿られる。

§3.1 の hash の持ち方:

| P2 | 持つ hash |
| --- | --- |
| `checkIfExpressionRefinesParameter` | 自身 |
| `getFlowTypeOfReferenceUnderCondition` | `getFlowTypeOfReferenceEx` |
| `getTypeAtSyntheticCondition` | `getTypeAtFlowNode` と `getTypeAtFlowCondition` |

## 4. 根の追加

walker (§6.3) が呼ぶ `SymbolToStringEx` が storechecker に無い。到達計算の根 (7d 設計 §2.7 の E3 の根) に `SymbolToStringEx` を足して再生成する。

- 同じ理由で足りない export がほかにあれば、それも根に足す。足したものは報告に列挙する。
- 小文字の関数を呼ぶために、ハーネスを storechecker の内部テストに置くことはしない。

## 5. `storeharness`

### 5.1 program の組み立て

入口は `NewPrograms(files map[string]string, options *core.CompilerOptions) (*compiler.Program, *Program)` の形にする。段 2 で multi-file に使えるよう、file を複数取れるようにする。

**Pointer の program**

- VFS: `vfstest.FromMap(files, true)` を `bundled.WrapFS` で包む。
- host: `compiler.NewCompilerHost("/", fs, bundled.LibPath(), …)`。
- config: `tsoptions.ParsedCommandLine` に options と root (files の全部) を入れる。
- `compiler.NewProgram(ProgramOptions{Config, Host, SingleThreaded: core.TSTrue})`。
- **Pointer の lib の SourceFile を program の間で使い回す。** harnessutil の `cachedCompilerHost` (harnessutil.go:486-528) と同じ形を storeharness に写す。harnessutil は変えない。

**Store の file**

- `space := store.NewIDSpace(len(pointer.SourceFiles()))` を作る。
- Pointer の `SourceFiles()` の順に、`storeparser.ParseSourceFileIn(space, store.StoreID(i), pf.ParseOptions(), pf.Text(), pf.ScriptKind)` で parse する。
- `ParseSourceFile` (space が nil) は使わない。
- **Store の lib は program ごとに parse + bind し直す。** Store は自分の IDSpace を持ち、Factory が別の program のノードを拒否するので (idspace.go、factory.go:53-60)、program の間では使い回せない。

**段 1 の option**

- 既定の `core.CompilerOptions{}` に、`AllowJs: TSTrue`・`CheckJs: TSTrue` だけを足す。
- 理由: corpus の JS の unit を program に入れ、JS の checker の経路 (JSDoc の reparse を含む) を通すため。
- ほかは足さない (`NoErrorTruncation` も入れない)。

### 5.2 adapter (`storechecker.Program` の実装)

面は checker_manual.go:680-705 の `Program` と、`storemodulespecifiers.ModuleSpecifierGenerationHost` (types.go:37-58)。7d 設計 §2.1 の「ノードを返すのは 2 method だけ」は不正確で、ノードに触れる method は 5 つある (§11 の 7)。

**A. そのまま委譲するもの**

- 対象: options・module 解決・path を取る method の全部 (`Options`・`FileExists`・`GetResolvedModule`・`GetSourceFileMetaData` など)。
- `*store.File` は `ast.HasFileName` を満たすので、HasFileName を取る method にはそのまま渡す。
- 委譲先の Pointer の実装が `FileName()` と `Path()` 以外を読むものがあれば、報告する。

**B. file を path で写すもの**

- `SourceFiles` (Pointer と同じ順)
- `GetSourceFile`
- `GetSourceFileForResolvedModule` (Pointer の実装の redirect の引き直しを含めて、結果を写す)
- `SourceFileMayBeEmitted` (Store の file を Pointer の file に戻して委譲する)

**C. 手書きするもの** (mode.go)

- 対象の 3 method と、写す元の Pointer の実装:

  | method | 写す元 |
  | --- | --- |
  | `GetEmitSyntaxForUsageLocation` | fileloader.go:1056-1085 |
  | `GetModeForUsageLocation` | fileloader.go:1018-1050 |
  | `GetResolvedModuleFromModuleSpecifier` | program.go:615 |
- **Pointer の実装を読んで、同じ判定の順に Store の述語で書く。** 変換器は使わない。
- file ごとの options は `module.GetCompilerOptionsWithRedirect(p.Options(), p.GetRedirectForResolution(file))` で得る。
- JSDocImportTag には `IsXxx` が無いので、`Kind()` で判定する。

**合成の import specifier の 2 つ**

- `GetJSXRuntimeImportSpecifier`・`GetImportHelpersImportSpecifier` の 2 つ。
- Pointer の program が nil を返すとき、`store.Node{}` を返す。
- Pointer が nil でないものを返したら、`panic("storeharness: synthetic import specifiers are stage 2")` にする。
- 段 1 の option では作られない見込み。条件は `jsx` が react-jsx 系か、`importHelpers` が立っていること (fileloader.go:831-848)。
- panic が出たら、その件数を報告する。

**そのほか**

- `BindSourceFiles` は、全部の Store の file を `storebinder.BindSourceFile` で bind する (冪等)。
- `IDSpace` は、§5.1 の space を返す。

## 6. 比較

### 6.1 driver

1 つの program について、Pointer と Store の両側で次を同じ順に呼ぶ。Pointer を最後まで走らせてから Store を走らせ、記録を比べる。

1. `NewChecker(program, nil)`。
2. root の file (lib 以外) の各 file について、program の順に `GetDiagnostics(ctx, f)`、次に `GetSuggestionDiagnostics(ctx, f)`。
3. `GetGlobalDiagnostics()`。
4. root の file について、§6.3 の walker。

**両側とも checker は 1 つで、program を通さない** (H1、7d 設計 §2.1)。program の後処理 (comment directive の除外、ソート) の前の生の出力を比べる。

**panic は側ごとに recover する。**

- Pointer が panic したら、その file は除外する (理由「Pointer panics」、§7.1)。
- Store が panic したら、その file の結果は panic の class になる (§7.2)。
- panic の後、その checker で続けない (checker の状態が途中のまま)。

### 6.2 (a) 診断

- 3 つの列 (file ごとの diagnostics・suggestion、global) を、**並べ替えずに** 先頭から比べる。
- 比べるもの: 件数と、各診断の次の field。
  - file の path (nil を含む)
  - `Pos`・`End`
  - `Code`・`Category`
  - `MessageKey`・`MessageArgs`・`MessageText`
  - `Source`
  - `ReportsUnnecessary`・`ReportsDeprecated`・`SkippedOnNoEmit`
  - `RepopulateInfo` の各 field
  - `MessageChain`・`RelatedInformation` の各要素。同じ field を再帰して比べる。
- 最初に食い違った field の名前を、分類の鍵に使う (§7.2)。

### 6.3 (b) type / symbol の walker

**Store 用の walker は、Pointer の `internal/testutil/tsbaseline/type_symbol_baseline.go` を読んで、同じ規則で手書きする。** 変換器では作らない (7d 設計 §2.7、規則 6)。Pointer 側は、`tsbaseline` の該当の関数を storeharness に写す。同じ AST なので、写しでよい。Pointer の walker を export に変えることはしない (§1 の 2)。写しが baseline と同じ規則であることは §8 の 4 で確かめる。規則は次の 4 つ。

1. **訪問**: `forEachASTNode` (319-344)。
   - `ForEachChild` の preorder で辿る。
   - Reparsed の扱いと、As / Satisfies の例外は、Pointer と同じにする。
2. **対象**: `visitNode` (304-317)。
   - `IsExpressionNode`・Identifier・`IsDeclarationName`・heritage の QualifiedName の条件。
3. **型**: 356-414。
   - 除外の条件を Pointer と同じにする。
   - extends の `ExpressionWithTypeArguments` の親を先に試す。
   - any の intrinsic の短絡と、その例外の一覧 (459-491) を写す。
   - `NewNodeBuilder(…).TypeToTypeNode(t, node.Parent, flags, InternalFlagsAllowUnresolvedNames, nil)` の flags と、`InTypeAlias` の作り直しを写す。
   - `RemoveComments` の printer で印字する。
   - `hadErrorBaseline` は、各側が自分の診断の件数から決める。件数が違えば、(a) で既に不一致になる。
4. **symbol**: 417-456。
   - `SymbolToStringEx(sym, node.Parent, SymbolFlagsNone, AllowAnyNodeKind)` を `EscapeAllInternalSymbolNames` に通す。
   - 宣言の最大 5 件の `Decl(<basename>, <line>, <char>)` を付ける (lib は `--, --`)。

記録するのは、`(node の pos, 表示テキスト, 型の文字列 | symbol の文字列)` の列。両側の列を先頭から比べる。

**Store に無い述語は、storeharness に Pointer を読んで手書きする。** 例は `GetMeaningFromDeclaration`・`IsLabelName`・`GetSourceTextOfNodeFromSourceFile`。書いた関数を報告に列挙する。

### 6.4 (c) symbol の対応 (レビュー契約 §2 の 1・2)

walker の symbol の記録から、Pointer と Store の symbol の一対一の対応を作る。

- **対にする単位**: 同じ位置の walker の記録にある (Pointer の symbol, Store の symbol)。
- **表**: 両方向に持つ (`P→S`、`S→P`)。1 つの symbol が 2 つの相手と対になったら、不一致「symbol correspondence」。
- **宣言のある symbol**: 宣言の列が対応すること。各宣言の (file の path, `Pos`, `End`, `Kind`) を比べ、順序も比べる。
- **宣言の無い symbol**: 上の一貫性だけを検査する。owner と member の slot からの構成 (契約 §2 の 2 の前半) は 2b-2 で足す。
- **比べない**: `GetSymbolId` の値。
- **採番しない**: 観測のために `GetSymbolId` を呼ばない (契約 §2 の末尾)。

### 6.5 (d) private の lookup (レビュー契約 §1)

`testdata/private/` に fixture を置き、§6.1 の driver で比べる。**fixture は 1 件でも不一致なら失敗にする** (corpus と違い、報告だけにしない)。fixture は次の 6 つ。

1. 別の file に、ローカルの SymbolId が同じになる class を置く (2 file の program)。
2. 同じ file の入れ子の class。
3. 同じ `#x` を持つ 2 つの class。
4. static と instance の private。
5. 基底の class の private と、派生の class の同名の private。
6. 別の class の private への不正なアクセス (診断が出る)。

## 7. 実行と分類

### 7.1 corpus の実行

- **テスト**: `TestCheckerCorpus` (storeharness)。
  - corpus は 2a と同じ集め方。`storebinder/binder_test.go:101-172` の `corpus` を storeharness の `_test` に写す。ほかの 4 つの写しには触れない。
  - `-short` では走らせない (他の corpus テストと同じ)。
- **除外**: 次の 2 つの理由だけ。理由ごとに件数と先頭の file をテストが log する。
  1. root が Pointer の program の `SourceFiles()` に無い。
  2. Pointer が panic する。
- **並列**:
  - worker の goroutine が file を順に取る。
  - Pointer の lib の cache は共有する。Store は共有しない。
  - worker の数は下の sample で決める。この機械は 8GB なので、既定は 4。
- **sample**:
  - まず、corpus を固定の間隔で間引いた 500 file (`STORE_CHECKER_CORPUS_SAMPLE=500`) で、wall・最大 RSS・file あたりの時間を測る。
  - 全体の見込みを出す。
  - **見込みが 60 分を超えるか、RSS が 6GB を超えるなら、全体を回さずに報告する。**
- **報告の mode**:
  - corpus の不一致ではテストを失敗にしない。分類して log する。
  - テストが失敗するのは、次のどれかのときだけ: ハーネスのエラー、§6.5 の fixture の不一致、§8 の試験の失敗。
  - 不一致で失敗にする strict な mode は 2b-2 で入れる。
- **詳細の出力**: `STORE_CHECKER_CORPUS_OUT=<path>` のとき、1 file 1 行の JSONL を書く。field は file・class・最初の不一致の詳細 (両側の値)。

### 7.2 分類の鍵

file ごとに、最初の不一致を 1 つの class にする。表示は鍵の文字列で、機械的に決まること。

| class | 鍵 |
| --- | --- |
| Store の panic | `store panic: <message の先頭 60 字> @ <最初の internal/store* の frame の関数名>` |
| 合成の import | `synthetic import (stage 2)` |
| 診断 | `diag <列>: count` か `diag <列>: <field>`。列は `file`・`suggestion`・`global` |
| walker | `walk: node set`・`walk: type`・`walk: symbol` |
| symbol の対応 | `symbol correspondence` |
| ID の token | `internal id token` |
| 一致 | `equal` |

- `internal id token` は、walk の型・symbol の文字列と、診断の message が、`#…@` と `__@…@` の中の数字の並びを除けば一致する場合。**合格扱いにはしない。** 数えるだけ (§0、2b-2 で照合する)。
- `store panic: storechecker: checkIfExpressionRefinesParameter` の class は、§3 の後は 0 件のはず。

## 8. テスト (新設)

1. **mode の単体等価** (strict)
   - corpus の全 file の program について、module specifier の位置の全ての `StringLiteralLike` を集める。対象は import / export / import-equals の external reference・`require`・`import()`・import type・JSDoc import。
   - 各位置で、Pointer の program の `GetModeForUsageLocation`・`GetEmitSyntaxForUsageLocation`・`GetResolvedModuleFromModuleSpecifier` と、adapter の同名の method を比べる。
   - ノードは `storetest.Pairs` で対にする。
   - 位置の数と、mode ごとの内訳を log する。
2. **§6.5 の fixture。**
3. **変換器のテスト** (§3.2)。
4. **Pointer 側の walker の検定** (strict)
   - storeharness の Pointer 側の walker も手書きなので、`tsbaseline` と同じ規則であることを、コミット済みの reference baseline で確かめる。
   - 対象: `tests/cases/compiler` のうち、`@` の option が無く、unit が 1 つで、reference の `.types` と `.symbols` がある test の全部。
   - option: testrunner の既定 (`harnessutil.go:81-113` の `NewLine`・`SkipDefaultLibCheck`・`NoErrorTruncation`) にそろえる。§5.1 の段 1 の option は使わない。
   - 手順: file を `/.src/<名前>` に置いて program を作り、§6.1 の順で診断を取ってから walker を回す。
   - 比べるもの:
     - `.types` の `>` で始まる行を `>表示テキスト : 型` として取り出し、walker の型の記録の列と比べる。
     - `.symbols` の行も同じく、symbol の記録の列と比べる。
     - 並びと件数も比べる。
   - 不一致の原因が baseline の側の条件 (emit の後に walk する、など) なら、検定を緩めずに止めて報告する。
   - これで、比較の相手 (Pointer 側) が baseline の walker と同じ規則であることを保証する。

## 9. 完了条件

1. 生成物が再現する。build と、store 系・storeharness の `go vet` が通る。
2. 既存のテストが両方の tag で通る。失敗は既知の a708 の 1 件だけ。
3. 変換器のテストが通る。
4. §8 の 1・2・4 が不一致 0。
5. `TestCheckerCorpus` が、ハーネスのエラー無しで全体を回り終える。§7.1 の sample の線を超えた場合は、sample だけでよい。
6. `checkIfExpressionRefinesParameter` の panic の class が 0 件。

## 10. 報告

`store-checker-7d2b1-implementation-report-<日付>.md` に次を書く。

1. 結論と完了条件ごとの結果。
2. **分類の表**: class ごとの件数、上位 20 class の例 3 件ずつ (file と両側の値)、`equal` の件数と割合。
3. 上位 10 class の原因の読み。
   - コードを読んで、疑わしい元を 1 つ挙げる: 変換の規則 (R 番号)・P2・adapter・store の API・ハーネス。
   - 直し方の案は書いてよい。直さない。
4. sample の時間と RSS、全体の時間、worker の数。
5. §4 で足した根と、§6.3 で手書きした述語の一覧。
6. §5.2 の A で、`FileName()`・`Path()` 以外を読む委譲先があったか。
7. 合成の import の panic の件数。
8. 本書に無い判断の一覧。

## 11. 本書で決めたこと (設計文書に無いもの)

1. **2b の分け方と入力の分け方** (2026-09-30、ユーザー): 2b-1 / 2b-2、段 1 / 段 2 (§0)。
2. **段 1 の option**: `AllowJs`・`CheckJs` だけを足す (§5.1)。
3. **Store の lib は program ごとに作り直す** (§5.1)。IDSpace による制約のため。費用は sample で測る。
4. **2b-1 は報告の mode** (§7.1)。strict は 2b-2。
5. **P2 の store-only の印** (§3.2)。hash の行を複数持てるようにする。印を要求するのは、打ち間違いを黙って通さないため。
6. **2b-2 に回すもの**: ID の token の照合、`compareSymbols` の fallback の性質、採番順の trace (レビュー契約 §2 の 3・4)。
   - 2026-09-30 にユーザーへ示した案では、fallback の性質を 2b-1 に入れるとしていた。
   - fallback の比較関数は checker の非公開の関数で、試験を足すと Pointer の checker の package に test file が要る。これは §1 の 2 のガードに当たるので、ID の扱いと一緒に 2b-2 で設計する。
7. **H1 の adapter の面の訂正**: ノードに触れる method は 5 つ (§5.2 の C と、合成の import の 2 つ)。7d 設計 §2.1 の「2 つだけ」は不正確。
8. **合成の import specifier は段 2** (§5.2)。段 1 の option では作られない見込みで、出たら panic して数える。
9. **ハーネスの置き場所**: 新設の `internal/storeharness`。2c の KPC driver でも使うため、test file ではなく package にする。
