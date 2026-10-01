# Store checker 7d-2b-2 検証報告

2026-10-01。**合格。** 検証指示書 §1〜§7 を完了した。strict 2回とreport modeの全量結果が一致し、11種類の意図的な差をすべて検出した。両タグの既存テストの失敗は、許容された既知の `FuzzEquivalence/a708303f611b95c8` だけ。検証の前後でソース18,092ファイルのSHA-256が完全一致した。

実装・ハーネスを直して結果を良くする変更は行っていない。

## 対象と証拠

対象 repo は `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`。HEAD / before は `830b69fbf139cfc5c64425611017782bd2e69512`。実装は未コミットの作業ツリー。承認済みの `Type` / `TypeExpression` の追加修正を含む。実装の修正・commit は行っていない。

新規 artifact set は [_store-checker-7d2b2-results/](./_store-checker-7d2b2-results/)。`provenance.json` と `source-before.json` が対象を記録する。今回の生成・試験はこの対象に対する `current` の結果。既存 `_store-checker-7d2b2-impl/` は先に読んで期待値と感度の比較材料に使ったが、ソースの完全な provenance が無いため今回の合格証拠には置き換えない。

## 1. 再現とガード

- tsc / tools の追跡・未追跡 `.go/.ts/.mod/.sum/.json` 18,092 ファイルの SHA-256 を開始時に取得。
- 両生成器は成功。再生成後 SHA-256 と git status は不変 (`regen-identity.txt`)。
- `GOWORK=off` の build、ast/store系/storeharness の vet、変換器 vet/test は成功 (`runs.jsonl`)。
- diff のコード監査は [audit/findings.md](./_store-checker-7d2b2-results/audit/findings.md)。生成物の変更は `views_generated.go` の多相3 accessor削除、`storechecker/checker.go` のP2移動、`exports.go` のCompareSymbols追加の3ファイルだけ。storeparser/storebinder、Pointer、他P2、変換規則、go.mod は不変。
- タグなし / storechecks とも終了。両方のexit 1は既知の `FuzzEquivalence/a708303f611b95c8` のみで、messageも `ExternalModuleIndicator differs from the Pointer file` と一致。失敗の親 `FuzzEquivalence` は同じ1件をまとめたもの。
- 終了時の18,092ファイルも開始時と完全一致。追加・削除・変更はいずれも0、HEADも同じ (`source-after.json`、`verification-summary.json`)。検証プロセスの終了をプロセス一覧で確認した。

| 試験 | タグなし | storechecks |
|---|---:|---:|
| コマンド全体 | 1105.51秒、既知1件のみ | 1028.07秒、既知1件のみ |
| TestPolymorphicAccessors | PASS 163.68秒 | PASS 125.29秒 |
| TestPointerWalkerBaselines | PASS 80.47秒 | PASS 71.28秒 |
| TestCheckerCorpus | PASS 669.69秒 | PASS 591.54秒 |
| TestModeCorpus | PASS 172.73秒 | PASS 193.59秒 |
| TestPrivateFixtures | PASS 0.41秒 | PASS 0.41秒 |

collection・nil receiver・TypeRoleSlot・tokenの新規試験を含む ast/store / storeharness、storebinder / storechecker / storeprinterは両タグで合格。`test-notag.txt` / `test-storechecks.txt` が全出力。

## 2. 修正の意味と独立走査

QuestionToken の4 kindと PostfixToken fallback、OrderedNodeSet の反復ごとの長さ/要素読み出し、P2 の文順序/early return を Pointer と照合した。Type / TypeExpression の追加修正も承認内容と一致する。

P2 hash は変換器を使わず宣言bytesの SHA-256 を計算し、先頭16字 `7215403e5af99e9c` が一致。scratch複製のPointerにcomment一行を入れると変換器はhash不一致でexit 1となった。

go/types で指定8 packageを独立走査し interface比較12か所、interface key map5か所を確認。今回P2以外に `store.Node` が入り比較不能panicを起こす箇所は見つからない。各候補の根拠と再現コードは [audit/findings.md](./_store-checker-7d2b2-results/audit/findings.md)。

## 3. 意図的な差の検出

**11種類すべて検出。** 原本を変更せず、scratch の `go test -c -overlay` で1種類ずつ差を入れた。JSONL出力時だけ class に `[mutN]` を付け、判定自体には影響させていない。corpusはworker2。詳細・差分・生ログは [mutations/findings.md](./_store-checker-7d2b2-results/mutations/findings.md)。

| # | 差 | 検出した試験・class・件数 | 最初のfile / index |
|---|---|---|---|
| 1 | QuestionToken fallback削除 | accessor 6kind /4,540件、strict `walk: type` 1件 | accessor: builder.ts、strict: builderPublic.ts /5 |
| 2 | NameがVariableDeclarationでnil | accessor: Name・PropertyNameOrName 各64,881件 | builder.ts |
| 3 | 同じkindでpanic | accessor: 同各64,881件 | builder.ts |
| 4 | Valuesを旧rangeへ戻す | collection 1,877/3,000列、strict 3,000file sampleで43件。suggestion count22/file count21 | expressionToTypeNode.ts /17、checkJsxNotSetError.ts /981 |
| 5 | P2をinterface ==へ戻す | strict `store panic: ... uncomparable type store.Node` 1件 | circularOptionalityRemoval.ts /1022 |
| 6 | 診断引数に`__@x@1` | strict `internal id token present` 1件、token1 | builderPublic.ts /5 |
| 7 | 同じ引数に生byte0xFEのtoken | 同上。JSON化より前の生byteで検出 | builderPublic.ts /5 |
| 8 | 無宣言・同名の異なるsymbolの比較を両順1 | 全17,415fileのstrictで `compareSymbols: antisymmetry (Store)` 43件 | factory/nodeFactory.ts /23 |
| 9 | 決定的fileのStore記録を変更 | strict `walk: node set` 1件、Pointer10再実行でも除外なし | 2dArrays.ts /166 |
| 10 | Pointer再実行を0回へ | 1・2回目equal、3回目にstrict `diag file: count` 1件。上限5回以内で検出 | jsx-runtime/index.d.ts /5591 |
| 11 | Pointer walkerが識別子を飛ばす | baseline 4,613/4,671本で不一致 | 2dArrays.ts |

#9の対象は2b-1の2回のJSONLともequalで、独立Pointer10回と現行版の一致も確認。#8は指定条件で検出できたため、代替の同一性変異は実行・結果採用していない。#10はPointer14診断/Store13診断の差が除外されずstrictで失敗した。全感度実行の終了コードとプロセス終了を確認した。

## 4. strict corpus と除外

strict 2回とも PASS。17,415 file中、比較17,405 / equal17,405、除外10 (synthetic8・Pointer非決定1・Pointer panic1・root無し0)。内部ID0、CompareSymbols各側554,158組 / fallback406組、違反0。両回の除外理由・index・file集合が完全一致した。report mode も同じ結果でPASS。3回すべての除外集合が完全一致 (`corpus-summary.json`)。

独立除外検証は完了。合成import8 fileは全件で Pointer APIが非nil、2b-1と同一集合。Pointer単独20回で非決定fileは9種類の記録になり、Storeの記録はその1つと完全一致。Pointer panicはStore未実行のprogram構築で再現した。詳細は [exclusions/findings.md](./_store-checker-7d2b2-results/exclusions/findings.md)。

CompareSymbols の検査は両側とも全出力記録後に呼ぶ。token/pair集計はsynthetic8件とPointer panic1件の早期除外を含まない。

## 5. 多相 accessor

通常全量照合 PASS。17,415 file、2,639,008ノード対、うちJSDoc54,459対。47 method、各側124,033,376回の呼出、不一致0。Pointer panic/Store零は73,479,053回、両方panicは5,268,412回。

reflect独立列挙は共通50からbind結果3つを除き47で、名前も一致。Pointer panic/Store零の上位10組はすべてIdentifierで、Pointerの該当コードと独立呼出によりpanicを確認した。修正前QuestionTokenの4,540不一致もmutationで再現した。

## 6. Pointer walker baseline

通常全量で4,671本、不一致0、option対象外28本を再現。2b-1の広げたmodeと本数は同じ。hadErrorsの上書きはこの検定だけで、driverの既定はcomment directive適用前の診断のまま。選択条件にcontent mapper使用testは入らない。実行ログは `baselines.txt`。前段の不一致2本 `elidedEmbeddedStatementsReplacedWithSemicolon.ts` / `jsxLibraryManagedAttributesUnusedGeneric.tsx` についても、選択条件に入ることと個別照合の一致を再確認した (`audit/baseline-two.txt`)。

## 7. 時間と限界

worker4でaccessor試験108.68秒、プロセスwall111.90秒、baseline73.66秒。他の検証が並行しているため実装報告との速度比較はしない。worker4の計測は次のとおり。時間は単発測定の再現記録で、性能比較ではない。

| 実行 | test秒 | wall秒 | 最大RSS bytes | peak footprint bytes | 1分load 開始/終了 |
|---|---:|---:|---:|---:|---|
| strict1 | 803.27 | 803.36 | 1,553,121,280 | 2,651,970,880 | 24.10 /35.30 |
| strict2 | 1056.26 | 1056.35 | 1,446,248,448 | 2,765,675,072 | 35.30 /24.05 |
| report | 766.33 | 766.41 | 1,683,439,616 | 2,634,786,112 | 24.05 /27.73 |

これは正しさの検証であり性能調査ではない。ns/op、B/op、allocs/op とhot path / allocation driverは未計測。性能の改善・同等性・退行を結論しない。

collection補助試験は3試験×20回PASS。固定seedの有限な操作列であり、NodeSetの停止位置・変更済み要素の出力回数・複数IDSpace/nil要素等の未網羅範囲がある。詳細は exclusions/findings.md。今回production/ハーネスは変更していない。

## 8. 再現

環境は Go 1.27.1 darwin/arm64、Node v24.3.0。全Go実行は `GOWORK=off`。一時binaryと複製は `/private/tmp/store-checker-7d2b2-verify/`、生ログは対象repoの `tsc/internal/ast/docs/_store-checker-7d2b2-results/` に保存した。

```sh
mkdir -p /private/tmp/store-checker-7d2b2-verify
cd /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript
node /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tools/scripts/tsc/generate.ts
cd /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tools
GOWORK=off go run ./cmd/storetranslate
python3 /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/ast/docs/_store-checker-7d2b2-results/reproduce.py
python3 /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/ast/docs/_store-checker-7d2b2-results/summarize.py
python3 /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/ast/docs/_store-checker-7d2b2-results/finalize.py
```

`reproduce.py` はbuild / vet / translator test、accessor、baseline、strict2回、report1回、両タグ全テストを順次実行する。上の再実行は同名ログを更新するため、今回のログを残す場合は先に退避する。実行時の全コマンド・作業ディレクトリ・終了コードは `runs.jsonl`。mutation overlayと独立probeは各担当のfindingsの再現節と保存source / patchを参照する。

## 9. 判断事項と次の段

追加の設計判断を仰ぐ問題は見つからなかった。段1のstrict検証を完了と判断する。次は既定の計画の7d-2b-3、段2の全構成検証。合成import8件はその範囲のままで、今回の段1合格によって段2や性能2cの合格を主張しない。

補足する限界は§7のcollection試験の有限なcoverageと、非決定fileの20回の独立再現が全可能出力を網羅する証明ではないこと。いずれも今回指定された合格条件を妨げる失敗は観測しなかった。
