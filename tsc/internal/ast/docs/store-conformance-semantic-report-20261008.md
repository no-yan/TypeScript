# Store の conformance semantic baseline 比較 報告

2026-10-08。[指示書](store-conformance-semantic-instructions-20261008.md)。before は `store-ast` の `db250179a6`、ハーネスの commit は `27fe0faf29`。生出力は `_store-conformance-semantic-20261008/`（commit しない）。

## 主張

**未達。**

- runner が回す全構成は 13,446 ある。そのうち Store が受け付ける構成は 13,430 で、その semantic baseline は 39,086 件である。
- 内訳:
  - emit に依存しない範囲で reference と一致: 38,983 件
  - emit に依存する範囲で、emit を除いた Pointer と一致: 58 件
  - `store-mismatch`: 45 件
- `store-mismatch` の 45 件は全部 errors で、全部が出力 path 検査なし（TS5055・TS5056）である。
  - 45 件とも、P から TS5055・TS5056 の行を除くと、S と byte で一致する。
  - これは §4 で予告した代償（S だけ `SuppressOutputPathCheck` を立てる）にあたる。
- 除外は理由ごとに次のとおり。
  - content mapper: 15 件
  - captureSuggestions: 1 件
  - project references: 0 件

types・symbols・module resolution の `store-mismatch` は 0 件である。

## suite × 種類 × 判定

run3（commit `27fe0faf29`）の manifest。run1・run2・run3 の 3 本は、時間の field を除いて同一である（§6.3）。

| suite | 種類 | equal | equal-pointer-noemit | store-mismatch | 計 |
| --- | --- | ---: | ---: | ---: | ---: |
| compiler | errors | 6,760 | 45 | 36 | 6,841 |
| compiler | types | 6,673 | 1 | 0 | 6,674 |
| compiler | symbols | 6,674 | 0 | 0 | 6,674 |
| compiler | module resolution | 76 | 0 | 0 | 76 |
| conformance | errors | 6,568 | 12 | 9 | 6,589 |
| conformance | types | 6,077 | 0 | 0 | 6,077 |
| conformance | symbols | 6,077 | 0 | 0 | 6,077 |
| conformance | module resolution | 78 | 0 | 0 | 78 |
| 計 | | 38,983 | 58 | 45 | 39,086 |

- `equal-pointer-noemit` の 58 件は、証拠 README（`_conformance-noemit-20261008/`）の 59 件から次の 1 件を除いたものである。
  - 除いた 1 件は `isolatedDeclarationsAllowJs.ts` の errors。P ≠ R（declaration 診断 TS9010 が無い）で、そのうえ S が P と TS5055 の分だけ違うので、`store-mismatch` に入る。
  - 58 件の内訳は errors 57（declaration 診断の差 54、check 順 3 で code は同じ）と types 1。declaration 診断の差は、消えた code が全部 `_conformance-noemit-20261008/declcodes.txt` にあることで判定した。
  - types の 1 件は `declarationEmitObjectAssignedDefaultExport` の `any` → `error`。R の errors が TS2883 だけなので、P の errors は NoContent になる。そのため hadErrorBaseline が false に変わる。S も同じ text を作った。
  - 証拠 README では「declaration 診断の差 55」だった。上の 1 件がこの 55 に含まれるので、ここでは 54 になる。
- `S` に `SuppressOutputPathCheck` を立てた構成は 13,430 のうち 12,270 である（残り 1,160 は `noEmit` が true）。
- Store の panic は 0 件。S の `GetErrorBaseline` の失敗、pre/post の件数差、code -1 も 0 件。

## 母集団

| 状態 | 構成 |
| --- | ---: |
| compared | 13,430 |
| store-unsupported | 16 |
| runner-skipped（`SkipUnsupportedCompilerOptions`） | 1,720 |
| runner-skipped（`skippedTests`） | 39 |
| runner-rejected | 0 |
| harness-error | 0 |
| 計（列挙した構成） | 15,205 |
| input-error（列挙できない file） | 7 |

列挙した test file は 12,730。

### store-unsupported の一覧

| 理由 | 構成 |
| --- | --- |
| content mapper（15） | `contentMapperAliasDiagnostic`、`contentMapperDeclarationEmit`、`contentMapperDeclarationEmitFailure`、`contentMapperDiagnosticDirectives`、`contentMapperDiagnostics`、`contentMapperInvalidDiagnosticDirectives`、`contentMapperInvalidExtension`、`contentMapperPerFileExtension`、`contentMapperSupplementalDiagnostics`、`contentMapperSupplementalFileCollision`、`contentMapperSupplementalGlobals`、`contentMapperSupplementalModule`、`contentMapperSymlink`、`contentMapperSynthesizedUnusedDiagnostics`、`contentMapperTransform`（いずれも compiler） |
| captureSuggestions（1） | compiler/`overshifts.ts` |
| project references（0） | — |

- project references は、Program に渡す config（`CommandLine`）で判定した。`CheckModeUnsupported` と同じく、loader が読む値である。
  - runner の `CompileFilesEx` も ParsedCommandLine に references を渡さないので、この値は常に空である。
  - tsconfig に `references` を書いた test も無い。

## store-mismatch の分類

| 種類 | 分類 | 件数 | 確定か |
| --- | --- | ---: | --- |
| errors | 出力 path 検査なし。P から TS5055・TS5056 の行（先頭の行とその続きの行、`!!!` の行）を除くと S と byte で一致する | 45 | 確定 |

- S の形での内訳:
  - S が NoContent: 13 件。P の診断が TS5055・TS5056 だけの構成。
  - S にほかの診断が残る: 32 件
- 消えた診断（P にあって S に無いもの）の内訳:

  | 消えた診断 | 件数 |
  | --- | ---: |
  | TS5055 × 1 | 38 |
  | TS5055 × 2 | 1 |
  | TS5056 × 1 | 1 |
  | TS5056 × 2 | 2（`bundlerImportTsExtensions` の `noEmit=false` の 2 構成） |
  | TS5055 × 1 と TS5056 × 1 | 3 |
- 45 件の集合は、compared の構成の reference errors.txt のうち TS5055・TS5056 を含む 45 file と、両方向で一致する。全 reference でも、TS5055・TS5056 を含む errors.txt は同じ 45 file である。
- 原因は確定している。S は `SuppressOutputPathCheck` を立てているので、`verifyCompilerOptions` の出力 path 検査を通らない（`storecompiler/options_manual.go:480`、§4）。
- 一覧は `_store-conformance-semantic-20261008/classify-run3-list.tsv`、分類の手順は `classify.py`。

## 照合

### §6.1 母集団

同じ source で `go test ./internal/testrunner -run '^TestLocal$' -json` を流し（63 秒、全 pass）、`population.py` で照合した。

- level 2:
  - TestLocal の pass は 13,446、skip は 1,720。
  - pass の集合は、compared（13,430）と store-unsupported（16）の和と、両方向で一致する。
  - 2 つの重なりは 0 で、test 名の重複も 0。
- level 3（compared の構成に属するもの）:
  - runner の `(構成名, 種類)` の集合は 39,086 件で、全部 pass。
  - 種類ごとに error 13,430、type 12,751、symbol 12,751、module_resolution 154。
  - manifest の適用の集合と、両方向で一致する。

### §6.2 P の照合

現在の `harnessutil.go` に、証拠 README の 3 箇所の編集を当てた（`make-overlay.py`）。差分は保存してある overlay と同じである。その overlay で `-run 'TestLocal/./^(error|type|symbol|module_resolution)$'` を流し（62 秒）、`pcheck.py` で照合した。

- runner の local の file は 59 件。内訳は `.delete` 39、errors.txt 19、types 1。
  - 全部が compared の構成の baseline だった。store-unsupported の構成の file は 0 件。
- ハーネスの `{(c, k) : P ≠ R}` も 59 件で、集合は両方向で一致する。
- 59 件とも、ハーネスの P の text は local の file と byte で一致する。`.delete` の 39 件は、P が NoContent で R がある。
- module resolution の P ≠ R は 0 件。
- Pointer の panic と、P の `GetErrorBaseline` の失敗は、どちらも 0 件（harness-error 0）。

### §6.3 決定性

全量を 3 回流した（run1・run2 は texts の解放だけが違う直前の版、run3 は commit `27fe0faf29`）。

- manifest の 15,213 行は、`elapsedMs` を除いて 3 本とも同一である。
- 書き出した `.P`・`.S`（206 file）も 3 本とも同一である。
- module resolution の揺れは無い。

### §6.4 回帰

- `TestLocal` 全体は pass した。local の file（`.delete` を含む）は 0 件。
- storeharness の、configprep と walker に触れる試験:
  - `TestPointerWalkerBaselines`（4,671 本、差 0）
  - `TestConfigurationFixtures`・`TestConfigurationRebuild`・`TestConfigurationClasses`・`TestConfigurationContentMappers`
  - いずれも pass した。段 2 の全量は回していない。
- storecompiler の package 試験は pass した。
- これらは commit の直前の版で流した。その後に変えたのは `conformance_test.go` の texts の解放だけで、上の試験はこの file を使わない。

## 所要時間と peak footprint

test binary（`go test -c`）を `/usr/bin/time -l` で直接走らせた。worker 4、singleThreaded（`TestProgramIsSingleThreaded` が true）。M1 8 GB、AC。

| run | wall | user | peak footprint | 最大 RSS |
| --- | ---: | ---: | ---: | ---: |
| run1 | 315 s | 1,380 s | 2,216 MB | 1,695 MB |
| run2 | 346 s | 1,570 s | 2,160 MB | 1,889 MB |
| run3 | 364 s | 1,610 s | 2,193 MB | 1,570 MB |

- 時間は参考値である。各 run の開始時に外部の負荷があった。
  - mediaanalysisd が CPU 42〜119%。
  - run1 では XprotectService・BackgroundShortcutRunner・Chrome も重なった。
  - swap の使用は 3.7〜4.2 GB。
- 構成ごとの所要時間は中央値 71 ms。飛び抜けて長いのは次の 2 構成で、1 本目だけで wall は 109 s を下回れない。
  - `intersectionConstructorReductionCrash.ts`: 109〜112 s
  - `crossProductUnionIntersectionRepeatedPropDeclarationsNoOOM1.ts`: 60〜75 s
- runner 側の参考値: overlay 付き TestLocal（error・type・symbol・module_resolution）は 62 秒、TestLocal 全体は 63 秒。

## commit と source hash

- before: `db250179a6`
- ハーネス: `27fe0faf29`
  - `storeharness/conformance_test.go`・`semanticbaseline_test.go`（新規）
  - `configprep.go`（構成名）
  - `storecompiler/storediagnostics.go`（`CompareWrappedDiagnostics`）
- source hash は `_store-checker-perf-20261005/meta/hash-tree.sh` の方法で取った。
  - 開始時: `meta/source-hash-start.txt`
  - run3 時: `meta/source-hash-run3.txt`（一覧の sha256 は `aaa5b41587e81f2d…`）
  - 2 つの差はハーネスの 4 file だけである。testdata（fixture・reference・lib）は変わっていない。

## 指示書から外れた判断

1. **行番号**: `walker.go` の記録に行番号を足さなかった。
   - 代わりに、記録が持つ `pos`（`node.Pos()`）から、runner の `writeTypeOrSymbol` と同じ規則で行を計算した（`SkipTrivia` の後の ECMA 行。`semanticbaseline_test.go` の `baselineResults`）。
   - 規則は同じで、既存の walker とその試験に変更が及ばない。§6.2 の byte 一致で確かめた。
2. **§6.2 の照合範囲**: ハーネスは store-unsupported の構成で P を作らないので、照合は compared の構成に限った。
   - overlay の runner は、store-unsupported の構成の file を 1 件も出さなかった。結果として、全体でも一致している。
3. **P の host の parse cache**: runner の `cachedCompilerHost` は、全 test で共通の global cache を持つ。ハーネスは構成ごとの cache にした。
   - pre と post で共有するので、runner と同じく post は pre の SourceFile を再利用する。
   - bundled lib は storeharness の global cache（`cachedLib`）を使う。
   - 全 test 共通にしなかったのは、memory を溜めないためである。§6.2 の byte 一致で確かめた。
4. **S の host**: pre と post で 1 つの host を共有し、その host に `TracerForBaselining` の `Trace` を渡した（P と runner と同じ形）。
   - pre は TraceResolution が false なので trace を書かない。trace は post の分だけになる。
5. **S の pre/post の件数差**: errors を `store-mismatch`（pre-post-count）にした。
   - types・symbols の hadErrorBaseline は、runner と同じく「診断あり」とした。runner は件数差の診断を足すためである。
   - 該当は 0 件。
6. **S の code -1**: 指示書は P の扱いだけを決めている。S では `store-mismatch`（code -1）とした。該当は 0 件。
7. **全量の回数**: §6.3 の 2 回（run1・run2）の後に、commit したコードで run3 を足した。
   - run1・run2 の後、`STORE_CONFORMANCE_OUT` が無いときに texts が解放されない点を直した。この修正では出力は変わらない（部分集合 102 構成で manifest と text が一致）。
   - 本書の数は run3 のものである。
8. **`StoreDiff` の比較相手**: S の比較相手は常に P の text にした。P = R のときは R と同じなので、§1 の「R where P = R, P otherwise」と同じになる。

## 判断待ち（§7）

該当なし。

- §6.1・§6.2 は一致した。
- §2 にない store-unsupported は要らなかった。
- 全量は panic もメモリ不足もなく回った。
- module resolution は 3 回とも同一だった。

## 次の段への材料

- `store-mismatch` は、出力 path の検査が無いことだけで生じている。Store の checker・binder・parser・loader の差は、この比較の範囲（errors・types・symbols・module resolution、13,430 構成）では出ていない。
- 主張を「一致」に変えるには、2 つの道がある。どちらにするかは判断が要る。
  - Store の `verifyCompilerOptions` に出力 path の検査を足す（`outputpaths.ForEachEmittedFile` が Pointer の file を取るので、写しが要る）。
  - 判定で TS5055・TS5056 を除外の理由として扱う。
