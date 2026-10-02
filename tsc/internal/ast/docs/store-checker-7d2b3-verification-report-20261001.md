# Store checker 全構成への接続と分類 (TODO 7d-2b-3) 検証報告

作成日: 2026-10-02。[検証指示書](store-checker-7d2b3-verification-instructions-20261001.md) (§8 追補を含む) に従い、[実装報告](store-checker-7d2b3-implementation-report-20261001.md) を独立に検証した。

- HEAD = before = **c5969e29e53**。commit していない。
- 生の出力: `_store-checker-7d2b3-results/` (ローカルのみ)。
- probe・感度検査の overlay と一時 file は scratchpad に置き、写しを `_store-checker-7d2b3-results/probe/` と `mutations/` に保存した。

## 1. 判定

- **7d-2b-3 の接続・分類: 合格。**
  - 独立 probe の照合で全件一致した。
  - 感度検査は 12 項目 + §8.3 の 4 項目 (+ 変種 6) が全部検出した。
  - ハーネスの欠陥は見つからず、ハーネスは修正していない。
  - 段 1 は維持されていた。
- **7d-2b 全体の等価: 本段の合格からは推論しない。**
  - 比べた全構成は一致し、段 1・段 2 とも strict が通る。
  - 未決のまま残るもの:
    - 除外 (runner-skipped 1,759 case、runner が読まない 7 file、段 1 の Pointer panic 1 file・非決定 1 file) の妥当性
    - default lib の等価を確かめる別テストの要否

## 2. 記録

| 項目 | 値 / 場所 |
| --- | --- |
| Go | go1.27.1 darwin/arm64、`GOWORK=off`。`STORE_*` 変数は未設定で開始 (`env-start.txt`) |
| 未コミット差分の hash | `uncommitted-diff.sha256` |
| source hash | 開始 `source-hash-start.txt` (18,117 file)、終了 `source-hash-end.txt`。**完全一致** |
| 生成器 | `generate.ts` exit 0、`storetranslate` exit 0。`git status` と diff の sha1 は前後で不変 (`regen-*.txt`) |
| build / vet | exit 0 / exit 0 |

## 3. 範囲 (§8.1)

before からの差分は次のものだけだった。

- storeharness と docs。
- `ast/store/syntheticimport.go`・`syntheticimport_test.go` (新設)。
- `factory.go` の `checkImport` の comment (+4/−2 行、コードは不変)。

Pointer の package、ほかの store の production、P2、生成器・生成物、go.mod / go.sum には差分が無い。

## 4. 構築経路の読み (§1)

- **写しと元の照合**: `prepareCase` / `build` を、元の次の関数と行ごとに照合した。
  - `makeUnitsFromTest`、`newCompilerTest`、`CompileFiles`、`CompileFilesEx`、`createProgram`
  - 照合した点: 適用順 (clone → 既定値 → directive)、root の選択、`baseurl` の書き戻し (写しは map を複製する)、パスの正規化、libFiles、content mapper
- **元との違い**: 実装報告 §3.3 のとおり。
  - preProgram を作らない
  - tracer を付けない
  - incremental で包まない
  - lib 以外の file は program ごとに parse し直す
  - どれも checker に渡る `compiler.Program` を変えない (§5 の probe で裏付けた)。
- **比較の入力**: `CompileFiles` の診断・Emit 済み program は使っていない。Store は Pointer の text から reparse する。
  - 次のものは導入されていない: Pointer AST の変換、比較前の診断の並べ替え、ID の置換、Pointer printer への変換。
- **再実行**: descriptor の `build` を使い、段 1 の入口には戻らない。options は build ごとに clone する。
- **content mapper**: 初回の program は `compareCase` が返るまで生かし、再実行の program はその都度閉じる。
- **project references**: runner と同じく転送しない。corpus の全 12,730 file に `"references"` は 0 件で、実装報告の主張どおり。
- **対象**: hadErrors は診断対象から計算している。walker 対象は runner の `verifyTypesAndSymbols` と同じ。
- **気づいた差 (status への影響なし)**:
  - runner は program を作ってから `SkipUnsupportedCompilerOptions` を呼ぶ。ハーネスは skip を先に判定する。
  - 影響があり得るのは、skip される構成で Pointer の構築が panic する場合だけ。§5 の probe では、skip 1,720 case の status が全部一致した。

## 5. 入力一覧と program の独立照合 (§2・§3)

### 5.1 probe の作り

**runner 側** (testrunner package、overlay):

- 元の `NewCompilerBaselineRunner(...).EnumerateTestFiles`・`skippedTests`・`getCompilerFileBasedTest`・`makeUnitsFromTest`・`newCompilerTest` を、構成ごとの subtest で呼ぶ。
- harnessutil の写しの `CompileFilesEx` に hook を 1 つ足した (`probe/harnessutil-overlay.diff`)。
  - hook は runner の host と config で `createProgram` した直後、**診断と Emit の前**に snapshot を取って抜ける。
- `SkipUnsupportedCompilerOptions` は、runner と同じく program を作った後に呼ぶ。
- Fatal / Skip の message は `go test -json` から取った。

**harness 側**: `enumerateConfigurations`・`prepareCase`・`build` の結果から、同じ snapshot 関数 (同じ text) で記録を作る。

**snapshot に入れたもの**:

- 実効 options の JSON、harness options
- roots、config file、references の数、currentDirectory、大文字小文字の区別
- SourceFiles の順と各属性: name、path、text の hash、script kind、variant、parse options、宣言か、default lib か、mapper、既定の mode、emit format
- 合成 specifier
- module と type reference の解決の全部
- toBeCompiled / otherFiles (名前と内容の hash)、walker 対象

**照合の key**: suite + 相対 path + 構成の JSON。表示名は key にしない。

### 5.2 結果

| 対象 | 結果 |
| --- | --- |
| 全量 (`probe-full-compare.txt`) | 15,166 case が双方にあり、片側だけの case は 0。status の対は built 13,446・skip 1,720 で全部一致。snapshot の差は全 field で 0。`skippedTests` と列挙できない file (42 file) も一致 |
| manifest と runner (`manifest-vs-runner.txt`) | case の集合と入力 hash が一致。ID は一意、index は 0 から連番。全 12,730 file |
| 実装の fixture (`probe-configurations-compare.txt`) | 25 case が一致 (built 22・fatal 2・skip 1)。列挙できない 2 file は runner と同じ理由 |
| 自作の fixture (`probe/fx/`、`probe-fx-compare.txt`) | `*` と `-true`、大文字小文字違いの重複 (`es2015, ES2015`)、`!` による除外、空の値 (`true,`)、組合せ 8、enum 値の重複、構成なし、除外で空になる例 (input-error)。全部一致 |

- runner の skip の message とハーネスの理由 (呼び出し位置) は全件対応した。

  | 理由 | case |
  | --- | ---: |
  | ES5 | 1,260 |
  | System / UMD | 266 |
  | moduleResolution | 85 |
  | alwaysStrict | 52 |
  | baseUrl | 33 |
  | esModuleInterop | 24 |

- corpus の特徴:
  - otherFiles を持つ case 645、tsconfig 157、大文字小文字を区別しない 6、content mapper 15、解決を持つ case 2,529。
  - SourceFiles が空の program と project references を持つ program は 0。
- 再列挙の安定性: 自分の run の manifest は実装の run4 の manifest と行単位で同一だった。

**probe の自分の誤り (修正済み)**: 初回は 2 case (`declarationEmitPrefersPathKindBasedOnBundling.ts`) が key で食い違った。runner が構成の map に `baseurl` を書き戻した後の値を、probe が key にしていたため。map を写してから呼ぶように直した後は 0 差。

## 6. 検出感度 (§4・§8.3)

- 方法: 写した file の 1 か所を壊して overlay にし、1 つずつ試した。
- 無改変の対照 (`00-control`) は全部 exit 0 だった。
- 各項目の差分・コマンド・出力は `mutations/<名前>/`、一覧は `mutations/summary.txt`。

| # | 壊し方 | 検出したもの |
| --- | --- | --- |
| 1 | 構成を 1 つ落とす | fixture の ID 一覧、独立 probe の照合 (片側だけの case) |
| 2 | 構成の部分と `#n` を消して ID を同じにする | `checkManifest` の「not unique」(fixture 試験、段 2 の report とも) |
| 3 | otherFiles も root にする | fixture (dependencyOnly)、probe の roots |
| 4 | tsconfig を directive より優先する | fixture (strict と TS7006)、probe の options |
| 5a / 5b | symlink を落とす / 依存 file を落とす | fixture、probe の SourceFiles・解決 |
| 6a / 6b | 2 番目以降の root の診断を落とす / 依存 file の診断を落とす | fixture (laterRoot / dependencyOnly) |
| 7 | walker から otherFiles を落とす | fixture の walker 対象と記録 |
| 8a / 8b | 再実行の root / option を段 1 の値にする | `TestConfigurationRebuild` の rerun の要約 |
| 9 | Store の診断の message を変える | strict が失敗。report では mismatch `diag file: MessageText`、再実行 10 回で非決定には分類されない |
| 10a / 10b | JSONL の行を落とす / case を走らせない | report でも失敗 (読み戻しの照合 / 「no status」) |
| 11 | Store の panic を adapter-unsupported にする | `TestConfigurationClasses` |
| 12a / 12b | Store の診断に `__@x@1` / 生 byte `0xFE@x@1` を入れる | strict が失敗。report では `internal id token present` (4 件) で、差の行 `Store file of target 1 #0 MessageText` が残る |
| §8.3 (a) | JSX / helpers の specifier を nil で返す | fixture。helpers は段 2 の strict でも検出 |
| (b) | text を変える | fixture、段 2 の strict |
| (c) | decl の親を root でないノードにする | fixture、`TestNewSyntheticImport` |
| (d) | 毎回新しいノードを返す | fixture (`SameNode`) |

**観察 (欠陥とはしない)**: (a) の JSX・(c)・(d) では、corpus の代表 (`jsxImportSourceNonPragmaComment.tsx`) の段 2 比較は equal のままだった。

- `compareModes` は adapter の map を読み、checker に返った値は見ないため。
- 今の checker の到達では、これらの違いが記録に出ないため。

§8.3 の要件は「fixture または段 2」なので満たしている。

**自分のスクリプトの誤り (修正済み)**: 同じ file への 2 つ目の編集が 1 つ目を消していた。このため 02・04 が当初は誤って「通過」や compile 失敗に見えた。直した後に全項目を回し直し、compile の失敗を検出と数えないようにした。

## 7. 全構成の再実行と分類 (§5・§8.4)

| 実行 | 結果 |
| --- | --- |
| report (`configurations.*`) | 15,205 case = equal 13,446 + runner-skipped 1,759。ほかの status は 0。選ばれない case 0、input-error 7 (全部 `skippedTests`)。wall 3m34s、exit 0 |
| strict (`strict.*`) | PASS、exit 0 |

**実装の run4 との照合** (`configurations-vs-run4.txt`):

- ID・index・入力 hash の差 0。
- 全 field (件数・CompareSymbols の組・mode の位置・`syntheticAsked`) の差 0。
- run3 の adapter-unsupported 118 case は、`syntheticAsked > 0` の 118 case と同じ集合で、全部 equal。
- それ以外の case で、run3 と status か class が違うものは 0。

**§5 の各項目**:

1. 合計は status 別の和と一致した。harness-error は 0。
2. equal には walker 対象が空の 2 case が入る (JS の root が program に入らない例。比較は global の診断で行う。実装報告 §6.4 (C))。
3. runner-skipped の理由は §5.2 のとおり、元 runner で独立に確認した。
4. 合成 import の case は 139。
   - JSX のみ 62、helpers のみ 77、両方 0。
   - checker が受け取ったのは 118 (JSX 62・helpers 56)。受け取らなかった 21 (helpers) も equal。
   - 段 1 の 8 file は、§8 の段 1 の結果で equal になった。
5. pointer-excluded は段 2 では 0 なので、20 回の単独実行の対象が無い。段 1 の 2 件は §8 を参照。
6. mismatch は 0。
7. strict は equal の代表で成功し、mismatch (#9) と token (#12) の代表で失敗した。

**store の API (§8.2)**: コードを読んで確かめた。

- `space.Next()` の StoreID、owner nil、file なし、seal。
- 範囲 (-1, -1)、flags 0、TokenFlags なし。
- 親は foreign 辺で root へ張る。`File()` は親を辿って file を返す。
- NodeKey は IDSpace の連番なので、source・遅延 JSDoc・checker の合成と重ならない。
- region の書き込み・取り込み・別 program の拒否は、`TestNewSyntheticImport` が両 tag で PASS した。

## 8. 回帰 (§6)

| 一式 | 結果 |
| --- | --- |
| tag なし (`test-notag.txt`) | 失敗は既知の `FuzzEquivalence/a708303f611b95c8` だけ |
| storechecks (`test-storechecks.txt`) | 同上 |

- 実行を log で確認したもの:
  - `TestCheckerCorpus` (strict)、`TestModeCorpus`、`TestPolymorphicAccessors`、`TestPointerWalkerBaselines`
  - `TestConfiguration*` 4 本、`TestNewSyntheticImport`
- 段 1 の件数:

  | 項目 | tag なし | storechecks |
  | --- | --- | --- |
  | 比較 / equal | 17,414 / 17,414 | 17,413 / 17,413 |
  | Pointer panic | 1 | 1 |
  | 非決定 | 0 | 1 (`jsxNamespaceImplicitImportJSXNamespace.tsx`) |
  | 合成 import の除外 | 0 | 0 |
  | token | 0 | 0 |
  | CompareSymbols の組 (fallback) | 554,182 (406) | 554,182 (406) |

  - 実装報告 §12.4 と同じで、非決定の揺れも同じ形だった。
- mode は 4,996 位置で不一致 0。walker baseline は 4,671 本で差 0。

## 9. 残件 (次の判断へ)

| 残件 | 内容 |
| --- | --- |
| 除外の妥当性 | **決定 (2026-10-02、ユーザー)**: `SkipUnsupportedCompilerOptions` の skip 1,720 case は除外してよい。<br>残る影響: 約 259 test file は、どの構成でも比べていない。storechecker の target 分岐 (約 25 か所) と module kind 分岐 (8 か所) の、これらの option での実行も未検証のまま。<br>`skippedTests` の 39 case と列挙できない 7 file、段 1 の Pointer 非決定 1 は、検証者の評価で除外が妥当 (作れない program、Pointer の揺れ)。<br>段 1 の Pointer panic 1 は、段 2 では baseUrl で skip されるので、上の決定によって除外のまま |
| default lib の等価 | **決定 (2026-10-02、ユーザー)**: 確かめない。別テストは作らない |
| (任意) 段 2 の合成 specifier の比較 | 問題: §6 の観察のとおり、corpus の比較は adapter が返す値を見ない。修正案: `compareModes` を、map でなく adapter の `GetJSXRuntimeImportSpecifier` / `GetImportHelpersImportSpecifier` が返す値と比べる。受入条件: §8.3 の (a) JSX・(d) が段 2 の比較でも失敗すること。今は fixture が検出しているので、要否はユーザーの判断。**決定 (2026-10-02、ユーザー)**: 塞ぐ |

## 10. 追補: 合成 specifier の比較の修正 (2026-10-02、ユーザーの決定による)

§6 の観察で見つけた弱点を、ハーネスの `compareModes` (`configurations_test.go`) で直した。修正前の証拠は `mutations/before-fix/` にある。

**変更**

- Store 側の合成 specifier を、adapter の map からではなく、checker と同じ入口 (`GetImportHelpersImportSpecifier` / `GetJSXRuntimeImportSpecifier`) から取る。
- 次の 3 つを確かめる。
  - 2 回目の問い合わせで同じノードが返ること (class `mode: synthetic import identity`)。
  - Pointer と同じ形であること: literal → ImportDeclaration → file の root、`File()`、text (class `mode: synthetic import shape`)。
  - Pointer にある所に Store もあること (class `mode: synthetic import`)。
- `syntheticAsked` は、`compareModes` より前に記録に写すので、記録は変わらない。
- `gofmt` が、既存の `caseLine` の field の整列も直した (意味は変わらない)。

**受入**

- 感度検査: (a) JSX・(a) helpers・(b)・(c)・(d) が、全部 corpus の代表に対する段 2 の strict でも失敗するようになった。fixture 試験でも引き続き失敗する。
- 無改変の対照: 全部通った。
- 段 2 全量の strict: PASS (`fix-specifier/strict.*`)。equal 13,446、mode を比べた位置 8,213 で、修正前と同じ。
- `TestConfiguration*` と `TestModeCorpus`: tag なし・storechecks とも PASS。
