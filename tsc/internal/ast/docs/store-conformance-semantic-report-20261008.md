# Store の conformance semantic baseline 比較 報告

2026-10-08。[指示書](store-conformance-semantic-instructions-20261008.md)。before は `store-ast` の `db250179a6`。ハーネスの commit は 2 つある。

- `27fe0faf29`: ハーネス本体
- 本書と同じ commit: 判定の除外（出力 path 検査）

生出力は `_store-conformance-semantic-20261008/`（commit しない）。

## 主張

**一致する。**

- runner が回す全構成は 13,446 ある。そのうち Store が受け付ける構成は 13,430 で、その semantic baseline は 39,086 件である。
- 内訳:
  - emit に依存しない範囲で reference と一致: 38,983 件
  - emit に依存する範囲で、emit を除いた Pointer と一致: 58 件
  - 出力 path 検査の診断を除外して、Pointer と一致: 45 件
  - `store-mismatch`: 0 件
- 除外は理由ごとに次のとおり。
  - content mapper: 15 構成
  - captureSuggestions: 1 構成
  - project references: 0 構成
  - 出力 path 検査の診断（TS5055・TS5056）: 45 baseline（全部 errors）

経緯:

- 実装の判定（run1〜run3）では、この 45 件は `store-mismatch` で、主張は未達だった。
- 45 件とも、差は TS5055・TS5056 だけだった。原因は、S だけ `SuppressOutputPathCheck` を立てていることである（§4）。
- そこで 2026-10-08、ユーザーの決定で、この 2 つの code を判定から除外した（指示書 §1 の `equal-without-output-path-check`）。本書の数は除外の後の run4 のものである。

## suite × 種類 × 判定

run4 の manifest。

| suite | 種類 | equal | equal-pointer-noemit | equal-without-output-path-check | store-mismatch | 計 |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| compiler | errors | 6,760 | 45 | 36 | 0 | 6,841 |
| compiler | types | 6,673 | 1 | — | 0 | 6,674 |
| compiler | symbols | 6,674 | 0 | — | 0 | 6,674 |
| compiler | module resolution | 76 | 0 | — | 0 | 76 |
| conformance | errors | 6,568 | 12 | 9 | 0 | 6,589 |
| conformance | types | 6,077 | 0 | — | 0 | 6,077 |
| conformance | symbols | 6,077 | 0 | — | 0 | 6,077 |
| conformance | module resolution | 78 | 0 | — | 0 | 78 |
| 計 | | 38,983 | 58 | 45 | 0 | 39,086 |

- `equal-pointer-noemit` の 58 件は、証拠 README（`_conformance-noemit-20261008/`）の 59 件から次の 1 件を除いたものである。
  - 除いた 1 件は `isolatedDeclarationsAllowJs.ts` の errors。P ≠ R（declaration 診断 TS9010 が無い）で、そのうえ P に TS5055 がある。S は P' と一致するので、`equal-without-output-path-check` に入る。
  - 58 件の内訳は errors 57（declaration 診断の差 54、check 順 3 で code は同じ）と types 1。
    - declaration 診断の差は、消えた code が全部 `_conformance-noemit-20261008/declcodes.txt` にあることで判定した。
    - 証拠 README では「declaration 診断の差 55」だった。上の 1 件がこの 55 に含まれるので、ここでは 54 になる。
  - types の 1 件は `declarationEmitObjectAssignedDefaultExport` の `any` → `error`。
    - R の errors が TS2883 だけなので、P の errors は NoContent になる。そのため hadErrorBaseline が false に変わる。
    - S も同じ text を作った。
- `S` に `SuppressOutputPathCheck` を立てた構成は、13,430 のうち 12,270 である（残り 1,160 は `noEmit` が true）。
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

## 出力 path 検査の除外（equal-without-output-path-check）

### 判定の定義

- P' は、P の診断から TS5055・TS5056 を抜き、`GetErrorBaseline` で作り直した errors の text である。
- errors で、S が P と違い、P' ≠ P かつ S = P' のとき、この判定になる。
- S = P なら従来の判定（`equal`・`equal-pointer-noemit`）が先に決まる。
- P' は、Pointer を `SuppressOutputPathCheck` 付きで作り直さず、診断の code で抜いて作る。
  - Pointer で `SuppressOutputPathCheck` を読むのは、出力 path の検査（`compiler/program.go:1311`）と `verifyProjectReferences`（`:1366`）だけである。
  - project references は store-unsupported なので、compared の構成では作り直した場合と同じ text になる。
- 抜くのは Store が出さない 2 つの code だけである。Store がほかの診断を落とせば、`store-mismatch` に残る。
- types・symbols・module resolution には P' を使わない。

### 45 件の中身（確定）

- 2 つの code の意味:
  - TS5055（Cannot write file '{0}' because it would overwrite input file）: 出力 path が入力 file と同じになる。
  - TS5056（… would be overwritten by multiple input files）: 2 つ以上の入力が同じ出力 path に書き出される。
  - どちらも Pointer の `verifyCompilerOptions` が Program を作る時に出す。emit の時ではないので、emit を呼ばない P にも出る。
- 45 件とも、S は P' と byte で一致する（manifest の `s` と `pWithoutOutputPathCheck` の sha が同じ）。
  - run3 の text でも、P から TS5055・TS5056 の行（先頭の行とその続きの行、`!!!` の行）を除くと S と byte で一致することを、別に確かめた（`classify.py`）。
- S の形での内訳:
  - S が NoContent: 13 件。P の診断が TS5055・TS5056 だけの構成。
  - S にほかの診断が残る: 32 件
  - P と S で hadErrorBaseline が違うのは NoContent の 13 構成である。その types・symbols は全部 `equal` だった。
- 抜いた診断の内訳:

  | 抜いた診断 | 件数 |
  | --- | ---: |
  | TS5055 × 1 | 38 |
  | TS5055 × 2 | 1 |
  | TS5056 × 1 | 1 |
  | TS5056 × 2 | 2（`bundlerImportTsExtensions` の `noEmit=false` の 2 構成） |
  | TS5055 × 1 と TS5056 × 1 | 3 |

- 主な構成:
  - `allowJs` で `.js` を入力にし、`outDir` が無いもの（例: `jsFileCompilationEnumSyntax.ts` の `a.js` → `a.js`）
  - 入力の `.d.ts` と declaration の出力が重なるもの（`declarationFileOverwriteError.ts`）
  - `b.ts` と `b.js`、`c.ts` と `c.tsx` が同じ `out/*.js` に出るもの（`bundlerImportTsExtensions`）
- 45 件の集合は、compared の構成の reference errors.txt のうち TS5055・TS5056 を含む 45 file と、両方向で一致する。全 reference でも、TS5055・TS5056 を含む errors.txt は同じ 45 file である。
- `pWithoutOutputPathCheck` を持つ baseline は、この 45 件だけである。
- 一覧は `_store-conformance-semantic-20261008/classify-run3-list.tsv`（除外の前の run3 の分類）にある。

## 照合

### §6.1 母集団

同じ source で `go test ./internal/testrunner -run '^TestLocal$' -json` を流し（63 秒、全 pass）、`population.py` で照合した。run3・run4 の manifest とも一致する。

- level 2:
  - TestLocal の pass は 13,446、skip は 1,720。
  - pass の集合は、compared（13,430）と store-unsupported（16）の和と、両方向で一致する。
  - 2 つの重なりは 0 で、test 名の重複も 0。
- level 3（compared の構成に属するもの）:
  - runner の `(構成名, 種類)` の集合は 39,086 件で、全部 pass。
  - 種類ごとに error 13,430、type 12,751、symbol 12,751、module_resolution 154。
  - manifest の適用の集合と、両方向で一致する。

### §6.2 P の照合

現在の `harnessutil.go` に、証拠 README の 3 箇所の編集を当てた（`make-overlay.py`）。差分は保存してある overlay と同じである。その overlay で `-run 'TestLocal/./^(error|type|symbol|module_resolution)$'` を流し（62 秒）、`pcheck.py` で照合した。run3・run4 の manifest とも一致する。

- runner の local の file は 59 件。内訳は `.delete` 39、errors.txt 19、types 1。
  - 全部が compared の構成の baseline だった。store-unsupported の構成の file は 0 件。
- ハーネスの `{(c, k) : P ≠ R}` も 59 件で、集合は両方向で一致する。
- 59 件とも、ハーネスの P の text は local の file と byte で一致する。`.delete` の 39 件は、P が NoContent で R がある。
- module resolution の P ≠ R は 0 件。
- Pointer の panic と、P の `GetErrorBaseline` の失敗は、どちらも 0 件（harness-error 0）。P' の作り直しの失敗も 0 件。

### §6.3 決定性

全量を 4 回流した。

| run | ハーネス |
| --- | --- |
| run1・run2 | `27fe0faf29` の直前の版（texts の解放だけが違う） |
| run3 | `27fe0faf29` |
| run4 | 除外を足した版 |

- run1〜run3: manifest の 15,213 行は、`elapsedMs` を除いて 3 本とも同一である。書き出した `.P`・`.S`（206 file）も同一である。
- run4 と run3（`exclusion-diff.py`）:
  - 違うのは、`pWithoutOutputPathCheck` を持つ 45 件の errors だけである。
  - その 45 件では、判定が `store-mismatch` → `equal-without-output-path-check` に移り、`storeDiff` が消えた。ほかの field は同じである。
  - ほかの baseline と record はすべて同一で、書き出した text も同一である。
  - P' は決定的な P から決まるので、run4 の 2 回目は流していない。
- module resolution の揺れは無い。

### §6.4 回帰

- `TestLocal` 全体は pass した。local の file（`.delete` を含む）は 0 件。
- storeharness の、configprep と walker に触れる試験:
  - `TestPointerWalkerBaselines`（4,671 本、差 0）
  - `TestConfigurationFixtures`・`TestConfigurationRebuild`・`TestConfigurationClasses`・`TestConfigurationContentMappers`
  - いずれも pass した。段 2 の全量は回していない。
- storecompiler の package 試験は pass した。
- これらは `27fe0faf29` の直前の版で流した。その後に変えたのは `conformance_test.go` だけで、上の試験はこの file を使わない。

## 所要時間と peak footprint

test binary（`go test -c`）を `/usr/bin/time -l` で直接走らせた。worker 4、singleThreaded（`TestProgramIsSingleThreaded` が true）。M1 8 GB、AC。

| run | wall | user | peak footprint |
| --- | ---: | ---: | ---: |
| run1 | 315 s | 1,380 s | 2,216 MB |
| run2 | 346 s | 1,570 s | 2,160 MB |
| run3 | 364 s | 1,610 s | 2,193 MB |
| run4 | 328 s | 1,438 s | 2,219 MB |

- 時間は参考値である。各 run の開始時に外部の負荷があった。
  - run1〜run3: mediaanalysisd が CPU 42〜119%。run1 では XprotectService・BackgroundShortcutRunner・Chrome も重なった。
  - run4: Chrome 59%。
  - swap の使用は 3.3〜4.2 GB。
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
- 除外: 本書と同じ commit。`conformance_test.go` だけを変えた。
- source hash は `_store-checker-perf-20261005/meta/hash-tree.sh` の方法で取った。
  - 開始時: `meta/source-hash-start.txt`
  - run3 時: `meta/source-hash-run3.txt`（一覧の sha256 は `aaa5b41587e81f2d…`）
  - 2 つの差はハーネスの 4 file だけである。testdata（fixture・reference・lib）は変わっていない。
  - run4 は、run3 の tree から `conformance_test.go` だけを変えたもので流した。

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
7. **全量の回数**: §6.3 の 2 回（run1・run2）の後に、commit したコードで run3 を、除外の後に run4 を足した。
   - run1・run2 の後、`STORE_CONFORMANCE_OUT` が無いときに texts が解放されない点を直した。この修正では出力は変わらない（部分集合 102 構成で manifest と text が一致）。
8. **`StoreDiff` の比較相手**: S の比較相手は常に P の text にした。P = R のときは R と同じなので、§1 の「R where P = R, P otherwise」と同じになる。

除外の判定（§1 の `equal-without-output-path-check`）は、指示書から外れた判断ではない。実装の後にユーザーが決め、指示書（実装・検証とも）に書き足した。

## 判断待ち（§7）

該当なし。

- §6.1・§6.2 は一致した。
- §2 にない store-unsupported は要らなかった。
- 全量は panic もメモリ不足もなく回った。
- module resolution は 4 回とも同一だった。

## 次の段への材料

- この比較の範囲（errors・types・symbols・module resolution、13,430 構成）では、Store の checker・binder・parser・loader の差は出ていない。
- 除外の 45 件は、emit を前提にした検査を emit しない Store に求めたことで出たものである。
  - Store の CLI は `--noEmit` を必須にしている。`noEmit` の下では Pointer もこの検査をしないので、実際に使う範囲では差は出ない。
  - Store に emitter を入れる段では、この除外を外し、出力 path の検査を写す。
