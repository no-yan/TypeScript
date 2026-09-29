# Store checker JSDoc (TODO 7d-1) 検証報告

作成日: 2026-09-29。検証指示は [store-checker-7d1-verification-instructions-20260928.md](store-checker-7d1-verification-instructions-20260928.md) (以下「検証指示」)、実装指示は [store-checker-7d1-implementation-instructions-20260928.md](store-checker-7d1-implementation-instructions-20260928.md) (以下「実装指示書」)、実装報告は [store-checker-7d1-implementation-report-20260929.md](store-checker-7d1-implementation-report-20260929.md) (以下「実装報告」)、設計は [store-checker-design-20260923.md](store-checker-design-20260923.md) (以下「7d 設計」)。生の出力は `_store-checker-7d1-results/` (ignore 対象、ローカルのみ)。

## 1. 結論

**正しさは不合格 (1 件)、性能は KPC 4 本と retained / GC が全部線の内側で合格。** 実装は直していない。

不合格の 1 件: **JS で、巻き戻される speculation の中で `@typedef` / `@callback` / `@import` が reparse されると、Store parser が通常 build で panic する** (§3.5)。`x = (/** @typedef {number} T */ a, b);` が最小の入力。Pointer は同じ入力で JSTypeAliasDeclaration を statement として残す。corpus 17,415 file と fuzz の seed に無い形なので等価テストは全部通っていた。原因は実装指示書 §4.2 の「speculation で戻すものは Pointer と同じ」が Store では成り立たないことで (Pointer は捨てた speculation のノードを heap で生かし、Store は `Truncate` で行を再利用する)、直し方に設計判断が要るので §0 の「正しさのバグは直してよい」の範囲を超えると判断した。案は §8。

| 指標 | 線 | 結果 (after) | before (3bb0252405、同じセッション) | 7d-0 の値 |
| --- | --- | --- | --- | --- |
| parse の等価 | JS を含めて除外 0・mismatch 0 | **17,415 file (JS 1,456) mismatch 0**、JSDoc の木 54,459 ノード mismatch 0、診断 17,415 file 一致 | — | 17,085 file (JS は緩めあり) |
| bind の等価 | 除外 0・mismatch 0 | **17,415 file mismatch 0**、宣言の Store 検査 717,627 件 | — | 17,085 file、708,382 件 |
| 巻き戻された reparse | (検証指示に無い) | **panic (§3.5)** | — | — |
| Walk cycles/visit S/P | ≤ 1.15 | **1.081** | 1.074 / 1.048 | 1.093 |
| Parse cycles/op S/P (checker.ts) | ≤ 1.00 | **0.826** | 0.826 / 0.823 | 0.822 |
| Parse cycles/op S/P (dom) | ≤ 1.00 (案) | **0.824** | 0.866 / 0.868 | 0.870 |
| Bind cycles/node S/P | ≤ 1.10 | **1.018** | 1.014 / 1.014 | 1.014 |
| retained B/node S/P | checker.ts ≤ 0.60、fixtures ≤ 0.70 | **0.565 / 0.601** (dom 0.740) | 0.565 / 0.602 (dom 0.740) | 0.565 / 0.602 |
| GC mark CPU S/P | checker.ts ≤ 0.25、fixtures ≤ 0.35 | **0.163 / 0.224** (dom 0.429) | 0.134 / 0.218 (dom 0.412) | 0.136 / 0.207 |

- KPC の Store 側の before → after: Walk は inst 同じ・cycles +0.5%、Parse checker.ts は inst +0.84%・cycles −0.6%、Parse dom は inst +1.6%・**cycles −5.2%**、Bind は inst +0.1%・cycles −0.4% (§6)。
- 遅延 JSDoc (dom の全 host 6,153 comment) は Pointer 比 ns 1.06、B/op 1.55、allocs 1.14、KPC cycles 1.092 / inst 1.043、全部引いた後の retained 1.16 (§6・§7)。実装報告の値と一致。
- 既知の fuzz seed `FuzzEquivalence/a708303f611b95c8` は tag なし / storechecks とも `ExternalModuleIndicator differs` の 1 件だけで落ち、ほかの失敗は無い。

## 2. 環境と入力

- `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、HEAD `3bb0252405` + 未コミットの 7d-1 (git status 36 項目、`git-status-start.txt`)。before は `git archive 3bb0252405 tsc tools` を `_store-checker-7d1-results/before-src/` に展開し、`git show` の blob と byte 一致を確かめた。repo の `go.work` があるので before / after とも `GOWORK=off` で build した。
- go1.27.1 darwin/arm64、Apple M1 8 GB (`env.txt`)。
- source の sha256 (57 file、`source-sha256-start.txt` / `-end.txt`) と git status は開始時と終了時で一致。一時テスト 5 本 (§3.3・§3.5・§6・§7 で使ったもの、写しは `zz_verify_*.go.txt`) は実行後に削除した。
- load average: テスト 2.5〜3.5。retained と ns の前に、before の tree の展開 (405 MB) と `-m=2` の出力を Spotlight が索引して **5〜7** に上がり、2.5 を切るまで待った (検証者の手順の失敗。scratchpad に置けば避けられた)。retained 2.1〜2.3、ns 1.8〜4.5 (Parse の after / before2 で 4.5)、**KPC 1.32〜2.04** (bind before2 だけ 1.5 を超えた)。
- KPC の binary と source の sha256 は `run-kpc.sh` が実行前後で照合し、両方 OK (`kpc-*-sha256.txt`)。

## 3. 正しさ (検証指示 §1)

### 3.1 ガードと再現

| 検査 | 結果 |
| --- | --- |
| `node tools/scripts/tsc/generate.ts` + `(cd tools && go run ./cmd/storetranslate)` | exit 0、git status と source の sha256 が不変 (177 関数 + P2 11 + 根 183) |
| `go build ./...`、`go vet` (tsc の 3 package、tools の変換器)、`go test ./cmd/storetranslate/` | 全部通る |
| `git diff --stat 3bb0252405 -- internal/ast internal/binder internal/parser internal/checker internal/scanner go.mod` (store と docs を除く) | 空 |
| `git diff --stat 3bb0252405 -- internal/storebinder/binder.go` | 空 |
| `go test -v` tag なし / `-tags storechecks` (store、storeparser、storebinder) | store と storebinder は PASS。storeparser は既知の seed 1 件だけ FAIL (`parser_test.go:361: ExternalModuleIndicator differs`) |
| `go test -race -run 'LazyJSDoc\|JSDocHostCheck'` | PASS |
| `go test ./internal/ast/ ./internal/parser/ ./internal/binder/` | PASS |
| `unsafe` | `store.go` の 2 箇所 (`Ref` と `refsOf`) のまま |

### 3.2 外した除外と緩め

実装指示書 §6.1 の一覧を 1 つずつテストのコードで確かめた。`storetest.Options` は型ごと消え (`SkipReparsed`、`MaskFlags`、`isJSDocCast` の透過、reparse だけの list を nil とみなす緩め)、`Equivalent` / `Pairs` は option を取らない。parser_test.go の `options`、`eagerJSDoc` の `IdentifierCount` の skip、`fromJSDoc`、`ExternalModuleIndicator` の skip、`rootFlagDiffs`、`subsequence` / `missingFrom`、binder_test.go / bindequiv_test.go の `options`、`fromJSDoc` / `nodes` の除去、`reparsedEffects` / `isTypeOfExpandoOrRequireDeclaration` / `excluded` も消えている。`FuzzEquivalence` も `TestCorpus` と同じ規則になった。

一覧に無い緩めは `rg 'Reparsed|NodeFlagsJSDoc|isJavaScript|ScriptKindJS|jsdoc|skip|mask|exclud|&\^|continue'` で探して見つからなかった。残る `continue` は `EquivalentJSDoc` の HasJSDoc の無いノードと、個数の不一致と walk の不一致を数えた後のもので、緩めではない。`ModifierFlags` の `&0xFFFF` は 7d-0 からあり JSDoc と無関係 (bit 16 以降は 7d-2)。

### 3.3 parse と bind の等価

- parse: `17415 files (1456 JS), 2584467 nodes visited, 54459 JSDoc nodes compared (18750 eager, in the file's Store)`、`diagnostics: 17415 files same, 0 differ`、field の不一致 0。7d-0 の log (17,085 file 比較、JS 10 file は診断を部分列で許していた) から、比べた file が 17,415 に増えた。
- JSDoc の木 (一時テストで数え直した、`jsdoc-counts.txt`): host は eager 1,990 (JS 1,917 のうち comment を持つ 1,916、TS の `@see` / `@link` 74)、遅延 11,162、空 3。実装報告の「eager 1,991」は JS の host を全部数えた値で、空の 1 host を含む。
- bind: `17415 files: 17415 compared, 0 with a parse mismatch; 717627 declaration Stores checked`。増分 +9,245 を一時テストで分けた (`decl-split.txt`): **7d-0 が除外した 330 file (7d-0 の log の 5 行の和集合がちょうど 330) が 9,245 件、残り 17,085 file が 708,382 件で 7d-0 と完全一致**。330 file の 9,245 件のうち reparse か JSDoc の下にある宣言は 2,462 件、残り 17,085 file では 0 件。増分は全部 330 file の分で、その中に JSDoc 由来の宣言が含まれる。

### 3.4 JSDoc の木の等価のテストと遅延 JSDoc の契約

`storetest.EquivalentJSDoc` を読んだ:

- (a) 経路: 両方で `host.JSDoc(file)` を引くので、TS は遅延、JS と TS の `@see` / `@link` は eager の cache を通る。
- (b) 各 comment を `pairs` で preorder に並べ、`Equivalent` と同じ `pair` (kind・pos・end・flags・`Parent`・`ModifierFlags`・member 全部・役割) で比べる。text は生成の比較で `JSDocText.Text` と `JSDocLink*.Text` が Pointer の `Text()` (`strings.Join`) と比べられる (member 466 → 470、役割 180 → 184)。
- (c) root の親は「host かどうか」を Pointer と照合し、さらに `pair` の `Parent` で kind と範囲を比べる。`File()` が host の file。
- (d) `ReparsedClones` (位置と kind。end は比べない) と `JSDocDiagnostics` は `fieldMismatches`。

corpus に入っている入力: `JSDocText` 10,897 (text が空でないもの 10,871)、link 114、param / property tag は `IsNameFirst` 真 1,071・偽 1,062。偽は 1,062 件全部が type と name を両方持つので、子の順の切り替えが preorder の比較に効く。

遅延 JSDoc の契約 (`store/jsdoc_test.go`) は実装指示書 §6.2 の 6 項目が全部ある:

- parse の回数は、登録された parser を包んで数える。包んだ関数は `resolveJSDoc` の write lock の中の miss でしか呼ばれないので、cache の hit は数えない。
- 空の結果の 2 入力は、テスト自身が `JSDoc == nil` を assert し、2 回目に parse しないこと、StoreID を消費しないことも見ている。
- 合成の host と別 file の host は TS の file と JS の file の両方で、tag なしの build でも panic する。
- 並列: 16 goroutine が 64 host を別々の順で引く。**同じ host の同時アクセスが実際に起きているか**を `-race -coverprofile` で 5 回確かめた (`cover-concurrent-*.out`)。write lock の後の二重検査の hit は 36〜47 回、parse は毎回 64 回ちょうど。
- seal: Factory の writer は全 build で、binder の writer は storeChecks で panic する。Builder は `Finish` で複製するので公開後の Store に届かない。

不変条件の検査: `FinishFile` の storeChecks の「root が最後」(`Finish` ではなく `FinishFile` に置いた理由は実装報告 §7 の 5 で妥当) と、`rewind` の後の `checkNoTruncatedRefs` (`jsdocInfos`、`reparsedClones`、`reparseList`) がある。

### 3.5 巻き戻された reparse の panic (不合格)

`checkNoTruncatedRefs` が `reparseList` を見ていることから、Pointer の `parseDelimitedList` は `reparseList` を区切らない (`parseListIndex` だけが区切る) ので、区切りの list の中の reparse が speculation ごと巻き戻されると残るのではないかと考え、一時テスト (`zz_verify_reparse_test.go.txt`、`rewound-reparse.txt`) で確かめた:

| 入力 (`/a.js`) | Pointer | Store tag なし | Store storechecks |
| --- | --- | --- | --- |
| `x = (/** @typedef {number} T */ a, b);` | JSTypeAliasDeclaration [9,28) + ExpressionStatement | **panic: `store: invalid node edge`** (`parseListIndex` の `View`) | panic: `checkNoTruncatedRefs` |
| `x = (/** @typedef {number} T */ a);` | 同様 | panic | panic |
| `x = (/** @import { A } from 'a' */ a, b);` | JSImportDeclaration + ExpressionStatement | panic | panic |
| `x = (/** @callback F @returns {string} */ a, b);` | 同様 | panic | panic |
| `f(/** @typedef {number} T */ a, b);` | 一致 | 一致 | 一致 |
| `x = (/** @typedef {number} T */ a, b) => a;` | 一致 | 一致 | 一致 |

`(` の後が arrow function かどうか決まらないと `tryParse` で引数として parse し、引数の JSDoc の `@typedef` を reparse して `reparseList` に積む。arrow でないと分かると rewind するが、Pointer は `reparseList` を戻さないので、typedef は外側の `parseListIndex` で statement に入る (T は宣言される)。Store は `Truncate` がその行を消すので、`reparseList` は範囲外 (panic) か、再利用された別のノードを指す。後者は panic せずに別のノードを statement に入れうる。

JS の editor 入力で起こりうる形で、parse の panic は program 全体を落とすので、頻度は低くても不合格とした。storeChecks の検査は corpus で一度も発火していない。

## 4. 移植の機械性 (検証指示 §2)

- jsdoc.go: Pointer 54 関数、Store 55 (足したのは `rawList`)。reparser.go: 21 → 23 (`isEmptyList`、`functionLikeHost`)。共通の関数は順序も同じ (`jsdoc-funcdiff.txt`、`reparser-funcdiff.txt`、7d-0 の `funcdiff.py`)。
- 差分 (jsdoc 922 行、reparser 1,018 行) を全部読んだ。違いは型の置換と `View`、constructor の折り畳み、`List`、setter / `SetFlags` / `SetRange` / `ReparentChildren` / `DeepCloneReparse(Modifiers)` / `SetParent`、`NodeRef` での保持、append をまたぐ list の `slices.Clone`、text の `strings.Join` だけ。分岐・条件・順序の違いは実装報告 §1.1 の表のとおりで、1 件ずつ確かめた:
  - `parseTypedefTag` の `JSDocTypeLiteral` を使うときだけ作る: Pointer では捨てた literal の property tag は dead で親が nil。Store でも dead で比較に出ない。等価で確認。
  - `@template` の `isEmptyList`: `core.Arena.Clone` は長さ 0 で nil を返すので、parse した list の「空 = nil」は正しい。class の `TypeParameters == nil` は `IsNil()` のまま。
  - `parseJSDocForNode` が parent を `NoNodeRef`、parse option を `FileName` / `Path` だけで渡す: `parseJSDocComment` の `parent` 引数は Pointer でも使われていない。`ExternalModuleIndicatorOptions` を読むのは file の終わりだけ。
- parser.go: 69 hunk。`withJSDoc` の呼び出しは 56 箇所 (Pointer と同数)、`jsdocFlags` は消えた。そのほかは実装指示書 §4.1 の一覧 (field、`JSDocInfo`、`ParserState`、`parseListIndex` の差し込み、`parseSourceFileWorker` の末尾、`parseToplevelStatement`、`finishSourceFile`、`createJSDocCache`) と `checkNoTruncatedRefs` だけ。
- speculation: `ParserState` / `rewind` が戻すものは Pointer と同じ (`jsdocInfos` と `reparsedClones` の長さ。`jsdocDiagnostics`、`reparseList`、`hasDeprecatedTag` は戻さない)。7b からの `Truncate` と `missingLists` だけが違う。**この「同じ」が §3.5 の原因。**
- 遅延 parse の入口: `initializeState` は script kind から context flags を決めるので、TS は None で host の flags を写さない。diagnostics は parser を pool に返すときに捨てる。`File` を持たないので `NodeCount` を更新しない。
- 変換器: 166 → 177 関数、P2 10 → 11。増えた 11 関数と P2 の 2 件 (`SetParseJSDocForNode` は hash `e231d5a508aa9186` の注記付きで新規、`GetNodeAtPosition` は `f2ca68b56aa87bec` のまま `includeJSDoc` を足した) は実装報告 §4 と一致。`GetNodeAtPosition` は Pointer (utilities.go) と同じ形。

## 5. API と命令列 (検証指示 §3)

`inline-store-{before-,}full.txt` を関数名で突き合わせた (`inline-store-diff.txt`、`inline-store-cost.txt`)。

| 対象 | 結果 |
| --- | --- |
| Builder の生成 constructor | 判定は全部 before と同じ (inline されるのは 1 本のまま)。cost が変わったのは text slot が増えた `NewJSDocLink` / `LinkCode` / `LinkPlain` (88 → 158、どちらも cannot) だけ。`NewJSDocText` は 136 で新規 (cannot) |
| 足した setter | `SetFlags` 8、`SetRange` 15、`SetParent` 16 は can。child の setter 117、list の setter 134、`SetModifiers` 198、`SetObjectAssignmentInitializer` 115、`SetPhaseModifier` 100、`ReparentChildren` 119 は cannot。`DeepCloneReparse` 80 / `DeepCloneReparseModifiers` 61 は can (本体 `deepCloneReparse` は cannot) |
| `Node.JSDoc` / `EagerJSDoc` | cannot (179 / 308)。`checkHost` は can (9) |
| 7d-0 の accessor | can → cannot は 0 件。can は 1,308 → 1,336 (+28 は全部新しい関数) |
| `withJSDoc` の速い側 (objdump、`asm-withjsdoc-after.txt`、`asm-parseVariableStatement-{before,after}.txt`) | 7d-0: inline された `jsdocFlags` と flags 0 の `AddFlags` (bounds check と RMW を無条件に実行) で約 12 命令。7d-1: 呼び出し側 4 + callee 14 (stack check 4、frame 3、`TBZ`、nil を返す 3、epilogue 3) で約 18 命令。**JSDoc の無いノードで +6 命令前後。** TS の JSDoc ありで `@see` / `@link` 無しは callee で約 40 命令 (7d-0 は約 14)。呼び出し元の関数は小さくなった (`parseVariableStatement` 68 → 48 命令) |

## 6. 費用 (検証指示 §4)

KPC はユーザーが `run-kpc.sh` で取った (`kpc-*-20260929-011737.txt`、集計は `kpc-summary.txt`)。値は 5 run の中央値。S/P は同じ binary の pointer との比。

| 段 | 指標 | before1 | after | before2 |
| --- | --- | ---: | ---: | ---: |
| Walk | cycles S/P (線 ≤ 1.15) | 1.074 | **1.081** | 1.048 |
| | Store cycles / inst / IPC | 8.335M / 27.97M / 3.35 | 8.373M / 27.97M / 3.34 | 8.330M / 27.98M / 3.36 |
| Parse checker.ts | cycles S/P (線 ≤ 1.00) | 0.826 | **0.826** | 0.823 |
| | Store cycles / inst / IPC | 55.33M / 214.9M / 3.88 | 54.86M / 216.7M / 3.95 | 55.01M / 214.7M / 3.90 |
| Parse dom | cycles S/P (線 ≤ 1.00、案) | 0.866 | **0.824** | 0.868 |
| | Store cycles / inst / IPC | 26.20M / 110.0M / 4.20 | 24.84M / 111.8M / 4.50 | 26.21M / 110.0M / 4.20 |
| Bind | cycles S/P (線 ≤ 1.10) | 1.014 | **1.018** | 1.014 |
| | Store cycles / inst / IPC | 33.92M / 126.1M / 3.72 | 33.78M / 126.2M / 3.73 | 33.91M / 126.1M / 3.72 |

- Walk と Bind は inst が before と同じ (±0.1%) で、7d-1 で変わらないという予想どおり。Walk の S/P の揺れは Pointer 側の spread (3〜5%) による。
- Parse の Store の inst は checker.ts +1.8M (+0.84%)、dom +1.8M (+1.6%)。§5 の `withJSDoc` の呼び出し費用からの見積り (checker.ts 0.33M、dom 0.31M) の 5〜6 倍で、見積りは足りない。残りの候補は、`mark` / `rewind` が 2 field ずつ多く写すこと (speculation の回数ぶん)、checker.ts の eager の `@see` / `@link` の parse、`createJSDocCache`。関数別の命令数は取っていないので帰属は未確定。
- それでも cycles は増えていない (checker.ts −0.6%、dom **−5.2%**、IPC 4.20 → 4.50)。dom の cycles 減は ns でも出ている (Store の Parse after/before 0.956)。仮説は、56 箇所の inline をやめて呼び出し元が小さくなり、hot な parse 関数の命令の取り込みが減ったこと。dom は interface member (`withJSDoc` を持つ) が大半。未検証で、線の判定には使わない。
- 検証指示の「dom の比が上がったら切り分ける」は当たらない (下がった)。
- ns (参考、before → after → before → after の交互、`ns-summary.txt`): Store の Walk 0.999 / 0.997、Parse 0.997 / 0.956、Bind 0.995 / 1.009 (checker.ts / dom の after / before)。

JS の parse (参考、corpus の JS / JSX 1,456 file を毎回全部 parse、一時ベンチ、`ns-parse-js.txt`): Pointer 27.9 ms / 25.3 MB / 83,500 allocs、Store 20.0 ms / 8.06 MB / 34,673 allocs (**0.72 / 0.32 / 0.42 倍**)。JSDoc の parse と reparse が入っても Store は Pointer より速く、確保も少ない。

遅延 JSDoc (dom、全 host 6,151、comment 6,153、`ns-lazyjsdoc.txt`、`kpc-after-lazyjsdoc-*.txt`):

| | Pointer | Store | S/P |
| --- | ---: | ---: | ---: |
| ns/op (中央値) | 9.32 ms | 9.89 ms | 1.06 |
| B/op | 6.37 MB | 9.89 MB | 1.55 |
| allocs/op | 77,715 | 88,495 | 1.14 |
| KPC cycles / inst / IPC | 26.5M / 139.4M / 5.26 | 29.0M / 145.3M / 5.02 | 1.092 / 1.043 |

実装報告の値 (1.06 / 1.55 / 1.14) と一致。B/op の内訳 (連結 text の 3 重の複製 約 4.5 MB/op) は検証していない。

## 7. retained heap と GC (検証指示 §5)

7d-0 と同じ手順 (`BenchmarkStoreBindRetainedV1` を入力 × variant で 1 つに絞り、run ごとに別プロセス、`GODEBUG=gctrace=1`、GC 2 回後の値、mark CPU は最後の forced GC の assist + background + idle)。before と after の binary を同じセッションで交互に 5 run (`retained-{before,after}/`、`retained-{before,after}-summary.txt`、load 2.1〜2.3)。

| 入力 | build | retained B/node P / S | S/P (線) | retained MB P / S | mark CPU ms P / S (中央値、min..max) | S/P (線) |
| --- | --- | --- | ---: | --- | --- | ---: |
| checker.ts | before | 109.40 / 61.86 | 0.565 | 33.10 / 18.49 | 20.2 / 2.7 (2.5..2.8) | 0.134 |
| | after | 109.40 / 61.84 | **0.565** (≤ 0.60) | 33.10 / 18.49 | 19.0 / 3.1 (2.44..3.3) | **0.163** (≤ 0.25) |
| fixtures | before | 124.60 / 74.96 | 0.602 | 136.80 / 80.63 | 82.0 / 17.9 | 0.218 |
| | after | 124.60 / 74.94 | **0.601** (≤ 0.70) | 136.80 / 80.68 | 80.0 / 17.9 | **0.224** (≤ 0.35) |
| dom | before | 128.40 / 94.98 | 0.740 | 14.58 / 10.49 | 8.5 / 3.5 | 0.412 |
| | after | 128.40 / 94.99 | 0.740 | 14.58 / 10.49 | 8.4 / 3.6 | 0.429 |

- retained の bytes は run 間 0.1% 以内。7d-1 の増分は fixtures の Store で +0.05 MB (NodeCount +864 = JS の eager JSDoc と reparse のノード)。checker.ts と dom は TS で eager 分がほぼ無く、変わらない。
- checker.ts の mark CPU の S/P が 0.134 → 0.163 になったのは Store の中央値が 2.7 → 3.1 ms に動いたためだが、after の min は 2.44 ms で before の範囲と重なり、有意な差とは言えない。線の内側。
- 遅延 JSDoc の Store (dom、一時テスト): 全 host 6,151 を引いた後、Store は host ごとに 1 つで 6,151 個 (空は 0)、中身は 13,404 ノード、列の footprint 2.32 MB (1 Store 376 B)。retained (一時ベンチ、`lazy-retained/`、variant 別プロセス 5 run、run 間ほぼ一定) は **Pointer 3.56 MB (578 B/comment)、Store 4.12 MB (669 B/comment)、1.16 倍**。Store 1 つに sentinel 行と `Store` の header が付くことが、comment ごとの固定費になっている。

## 8. 7d 設計への反映案

1. **§3.5 の直し方 (判断が要る)。** Pointer と同じ結果 (捨てた speculation の typedef が statement に残る) を出すには、そのノードを rewind の後も生かす必要がある。案:
   - **(c) 推奨**: `ParserState` に `reparseList` の長さを記録し、rewind の間に `reparseList` が伸びていたら、その rewind では `Truncate` しない (speculation のノードは dead として残す。top-level await と同じ流儀)。変更は `rewind` の 1 条件で、起きるのはこの形の入力だけ。dead node が増えるのは JS のこの入力に限る。
   - (a) `reparseList` も巻き戻す: 簡単だが Pointer と結果が変わる (T が宣言されなくなる) ので等価が壊れる。
   - (b) 残すべき部分木だけ Truncate の後に移し替える: 参照の付け替えが要り、複雑。
   
   どれにしても、この形を `TestDeadNodes` か parse の等価の入力に固定し、fuzz の seed に JSDoc を含む JS の入力を足すことを勧める (今の fuzz は corpus を seed にしているが、この形を見つけていない)。
2. 実装指示書 §9 で決めたことは妥当: text の連結 (比較に 10,897 件)、`IsNameFirst` の手書き case (偽 1,062 件が順に効く)、setter の範囲 (14 個とも使われる)、`File` の cache と通常 build の host 検査、`FinishLazyJSDoc`、program 外の Store。**「speculation で戻すものは Pointer と同じ」だけは Store の `Truncate` と両立しない** (§3.5)。
3. dom の Parse の線 (≤ 1.00、案): after は 0.824 で、before (0.866) より余裕がある。線として置いてよい。
4. 遅延 JSDoc の費用: dom の全 host で Pointer 比 cycles 1.09、B/op 1.55、retained 1.16。7d 設計 §2.2 の 2,845 host / check で見ると、差は 0.3 ms・1.6 MB 程度で、7d-2 の Check の線に効く大きさではない。B/op を減らすなら、実装報告 §6 の案 (text を source の範囲で持つ、`Finish` で `textBuf` を複製しない) が先。7d-2 に回す。
5. 7d-2 に回すもの: Parse の inst 増 (+0.8〜1.6%) の関数別の帰属 (cycles は増えていないので優先度は低い)、遅延 JSDoc の lock の待ち、合成の host が `JSDoc` に届く経路 (今は panic)。

## 9. 限界

- §3.5 は検証者が当たりを付けた形で見つけたもので、同じ種類の形 (巻き戻される speculation の中で `reparseList` に積まれるもの) を網羅していない。fuzz も回していない。
- KPC の load は 1.32〜2.04 で、bind before2 は線の 1.5 を超えた。before1 / before2 の Store の値は 0.1% 以内で一致している。
- Parse の inst 増の帰属と dom の cycles 減の理由は仮説のまま。
- ns の Parse の after / before2 は load 4.5 の下で取った。
- 遅延 JSDoc の B/op の内訳は実装報告の値を検証していない。

## 10. 再現手順と生データ

```sh
ROOT=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript; TSC=$ROOT/tsc; OUT=$TSC/internal/ast/docs/_store-checker-7d1-results
# §3: generate.txt、build-vet.txt、storetranslate.txt、guard.txt、test-{notag,storechecks,race,pointer}.txt、test-log.txt
#     decl-split.txt (+ excluded-7d0.txt)、jsdoc-counts.txt、cover-concurrent-*.out、rewound-reparse.txt、zz_verify_*.go.txt (一時テストの写し)
# §4: storeparser.diff、{jsdoc,reparser}-vs-pointer.diff、{jsdoc,reparser}-funcdiff.txt (verify-scripts/funcdiff.py)
# §5: inline-{store,storeparser}{-before,}-full.txt、inline-store-diff.txt (inlinediff.py)、inline-store-cost.txt (inlinecost.py)、asm-*.txt
# §6: KPC はユーザーが別ターミナルで (sha256 を前後で照合、load < 1.5 を最大 30 分待つ):
bash $OUT/run-kpc.sh
#     kpc-*-20260929-011737.txt → kpc-summary.txt (verify-scripts/kpc-summary.py)
#     ns-{store,storeparser,storebinder}-{before,after,before2,after2}.txt → ns-summary.txt、ns-lazyjsdoc.txt、ns-parse-js.txt
# §7: retained-{before,after}/ → retained-{before,after}-summary.txt (retained-summary.py)、lazy-retained/、lazy-jsdoc-stores.txt
# 計測の順序: run-measure.sh (run-measure.log)。before の source: before-src/ (git archive 3bb0252405)
```
