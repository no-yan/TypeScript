# Store checker JSDoc (TODO 7d-1) 検証指示

作成日: 2026-09-28。対象は検証を担当するエージェント。何を作ったかは [store-checker-7d1-implementation-instructions-20260928.md](store-checker-7d1-implementation-instructions-20260928.md) (以下「実装指示書」)。設計は [store-checker-design-20260923.md](store-checker-design-20260923.md) (以下「7d 設計」) の §2.1・§2.2・§3 の不変条件 20・22・24〜27。

目的は 4 つ。**(1) JS を含めて parse / bind の等価が除外と緩め無しで mismatch 0**、**(2) JSDoc の木が Pointer と同じで、遅延 JSDoc の契約 (1 回だけ parse、空の記録、host への辺、seal、host の検査、並列) が成り立つ**、**(3) 移植が逐語で、Builder に足した API が実装指示書 §3.1 の範囲に収まる**、**(4) Parse の費用 (checker.ts と dom) と、Walk / Bind / retained が 7d-0 から退行していないこと**。

## 0. 規則

7d-0 の検証指示 §0 と同じ。実装を直して数字を良くしない。直してよいのは正しさのバグだけで、直したら報告に書く。生の出力は `tsc/internal/ast/docs/_store-checker-7d1-results/` (ignore 対象) に保存する。コマンドは絶対パス (`ROOT=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、`TSC=$ROOT/tsc`、`OUT=$TSC/internal/ast/docs/_store-checker-7d1-results`)。環境 (go version、commit、load average) を記録する。

**計測前に source の sha256 を取り、終了時に不変を確認する** (`internal/ast/store/**/*.go`、`internal/storeparser/*.go`、`internal/storebinder/*.go`、`generate-go-store.ts`、`tools/cmd/storetranslate/*.go`)。

before は 7d-1 の直前の commit **3bb0252405** (7d-0 と 2026-09-28 の修正を含む)。before の source は `git archive 3bb0252405` を別ディレクトリに展開して作り、binary と source の sha256 を記録する。

既知の失敗は `FuzzEquivalence/a708303f611b95c8` (`ExternalModuleIndicator differs`、SubtreeFacts の枝刈りが未実装) の 1 件だけ。**この seed が同じ理由で落ちることを確かめ、それ以外の失敗は 1 件でも不合格とする。**

KPC は root が要るのでユーザーに依頼する (§4)。ns / allocs / retained / gctrace は自分で取る。

## 1. 正しさ

```sh
cd $ROOT && node tools/scripts/tsc/generate.ts && (cd tools && go run ./cmd/storetranslate) && git status --short
cd $TSC && go build ./... && go vet ./internal/ast/... ./internal/storeparser/... ./internal/storebinder/...
cd $ROOT/tools && go vet ./cmd/storetranslate/... && go test -count=1 ./cmd/storetranslate/
cd $TSC && git diff --stat 3bb0252405 -- internal/ast internal/binder internal/parser internal/checker internal/scanner go.mod ':!internal/ast/store' ':!internal/ast/docs'   # 空
cd $TSC && git diff --stat 3bb0252405 -- internal/storebinder/binder.go   # 空
cd $TSC && go test -count=1 -v ./internal/ast/store/... ./internal/storeparser/... ./internal/storebinder/... > $OUT/test-notag.txt 2>&1
cd $TSC && go test -tags storechecks -count=1 -v ./internal/ast/store/... ./internal/storeparser/... ./internal/storebinder/... > $OUT/test-storechecks.txt 2>&1
cd $TSC && go test -race -count=1 -run '<実装指示書 §6.2 の 2 の並列テスト>' ./internal/ast/store/... ./internal/storeparser/... > $OUT/test-race.txt 2>&1
cd $TSC && go test -count=1 ./internal/ast/ ./internal/parser/ ./internal/binder/
```

確認すること:

1. 実装指示書 §1 のガード。生成物が 2 つの生成器で再現すること。storebinder の本体が変わっていないこと。
2. **除外と緩めが全部外れていること**: 実装指示書 §6.1 の一覧を 1 つずつ、テストのコードを読んで確かめる。`storetest.Options` の `SkipReparsed` / `MaskFlags`、`isJSDocCast`、`fromJSDoc`、`eagerJSDoc` の skip、`subsequence`、330 file の除外 (`reparsedEffects` / `excluded`) が消えたか、使われていないこと。**一覧に無い緩め** (JS や JSDoc で分岐する比較、`Reparsed` / `NodeFlagsJSDoc` を見て飛ばす処理) を `rg 'Reparsed|NodeFlagsJSDoc|isJavaScript|ScriptKindJS|jsdoc' internal/storeparser/*_test.go internal/storebinder/*_test.go internal/ast/store/storetest/` で探し、残っていれば理由を読む。
3. **parse の等価**: corpus 17,415 file で、JS を含めて除外 0・mismatch 0。JSDoc の木の等価 (実装指示書 §6.2 の 1) の件数 (比べた host の数、eager と遅延の内訳) を記録する。7d-0 の log (`_store-checker-7d0-results/verify-test-notag-v.txt`) と比べて、比べた file の数が 17,085 から 17,415 に増えていること。
4. **bind の等価**: 除外 0・mismatch 0。宣言の Store の検査の件数を 7d-0 (708,382) と比べ、増えた分が 330 file と JSDoc 由来の宣言で説明できること。
5. **JSDoc の木の等価のテストが実際に比べていること**: テストのコードを読み、(a) TS の遅延と JS / `@see` / `@link` の eager の両方の経路を通る、(b) 各 JSDoc の preorder で kind・flags・pos・end・text を比べる、(c) `Parent()` が host、(d) `File.ReparsedClones` と `JSDocDiagnostics` を比べる、の 4 つがあること。**`JSDocText` の text (実装指示書 §2 の 1) と `JSDocParameterOrPropertyTag` の子の順 (§2 の 2) が比較に入っている入力が corpus にあること**を、該当 kind の出現数で確かめる (`IsNameFirst` が true と false の両方)。
6. **遅延 JSDoc の契約** (実装指示書 §6.2 の 2): 6 項目が全部あること。parse の回数の数え方が本物の parse を数えていること (cache の hit を数えていないこと)。空の結果のテストの入力が実際に空になること。**host の検査が tag なしの build で panic すること** (合成の host、別 file の host の両方。eager の file (JS) でも)。並列テストが同じ file の同じ host を同時に引いていて、`-race` で走っていること。
7. **不変条件の検査**: storeChecks build で「root が最後のノード」の検査 (実装指示書 §3.1) と、`Truncate` の後に `jsdocInfos` / `reparsedClones` に消えたノードが残らない検査 (§4.2) があること。`FinishLazyJSDoc` の Store が seal 済みで、以後の書き込み (Builder・Factory・binder の writer) が拒否されること。
8. `unsafe` が 7d-0 の 2 箇所から増えていないこと。

## 2. 移植の機械性

```sh
cd $TSC && git diff 3bb0252405 -- internal/storeparser ':!*_test.go' > $OUT/storeparser.diff
cd $TSC && diff <(sed 's/^/ /' internal/parser/jsdoc.go) <(sed 's/^/ /' internal/storeparser/jsdoc.go) > $OUT/jsdoc-vs-pointer.diff
cd $TSC && diff <(sed 's/^/ /' internal/parser/reparser.go) <(sed 's/^/ /' internal/storeparser/reparser.go) > $OUT/reparser-vs-pointer.diff
```

- jsdoc.go (54 関数) と reparser.go (21 関数) の関数の集合と順序が Pointer と同じであること。関数単位で並べた diff (7d-0 検証の `funcdiff.py` と同じ手順) を全部読み、違いが実装指示書 §4.1 の置き換え (Builder の constructor、`List`、setter、`DeepCloneReparse`、`ReparentChildren`、`NodeRef` での保持) だけであること。分岐・条件・順序の違いがあれば 1 件ずつ理由を確かめる。
- `parser.go` の hunk: 実装指示書 §4.1 の一覧 (field、`ParserState`、`parseListIndex` の `reparseList`、`finishSourceFile`、56 箇所の `withJSDoc`) 以外が無いこと。`withJSDoc` が 56 箇所で、`jsdocFlags` が消えていること。
- **speculation** (実装指示書 §4.2): `mark` / `rewind` が戻すものが Pointer と同じ (増えも減りもしない) こと。
- **遅延 parse の入口**: `parseJSDocForNode` の移植が Pointer と同じく、TS の `contextFlags` を None にし、diagnostics を捨て、`NodeCount` を更新しないこと。
- 変換器の再実行で増えた関数と P2 を、実装報告の一覧と照合する。P2 は 1 件ごとに hash の注記があること。

## 3. API と命令列

```sh
cd $TSC && go build -gcflags='-m=2' ./internal/ast/store/ > $OUT/inline-store-full.txt 2>&1
cd $TSC && go build -gcflags='github.com/microsoft/TypeScript/tsc/internal/...=-m=2' ./internal/storeparser/ > $OUT/inline-storeparser-full.txt 2>&1
```

before (3bb0252405 の展開) でも同じものを取り、関数名で突き合わせる。

| 対象 | 期待 |
| --- | --- |
| Builder の生成 constructor | can / cannot の件数と cost が before から変わらないこと (7d-1 は constructor を変えないはず)。変わったら理由 |
| 足した setter、`SetFlags` / `SetRange`、`ReparentChildren`、`DeepCloneReparse` | 判定と cost を記録する (reparse は JS だけなので線は置かない) |
| `Node.JSDoc` / `EagerJSDoc` | 判定と cost を記録する。hot path ではない (7d 設計 §2.2: 3,618 回/check) |
| storeparser の `withJSDoc` の呼び出し | TS の速い側 (`HasJSDoc` の flag を立てて `@see` / `@link` が無ければ return) が、7d-0 の `jsdocFlags` と同じ程度の命令数であること。objdump で確かめる |
| 7d-0 の accessor の inline 判定 | cannot が増えていないこと |

## 4. KPC: before / after

ユーザーが別ターミナルで (load average < 1.5 を待ってから)、before と after の binary を同じセッションで before1 → after → before2 の順に走らせる。7d-0 の検証で使った `verification-20260927/kpc-20260928/run-kpc.sh` と同じく、source と binary の sha256 を実行前後で照合するスクリプトを `$OUT/run-kpc.sh` に用意し、ユーザーに渡す。

```sh
# before: 3bb0252405 の git archive を $BEFORE に展開して
cd $BEFORE/tsc && go test -tags kperf -c -o $OUT/store.before.kperf.test ./internal/ast/store/ && go test -tags kperf -c -o $OUT/storeparser.before.kperf.test ./internal/storeparser/ && go test -tags kperf -c -o $OUT/storebinder.before.kperf.test ./internal/storebinder/
# after
cd $TSC && go test -tags kperf -c -o $OUT/store.after.kperf.test ./internal/ast/store/ && go test -tags kperf -c -o $OUT/storeparser.after.kperf.test ./internal/storeparser/ && go test -tags kperf -c -o $OUT/storebinder.after.kperf.test ./internal/storebinder/
# 各段 (Walk: ast/store の StoreWalkKPCV1、Parse: storeparser の StoreParseKPCV1、Bind: storebinder の StoreBindKPCV1) を
#   sudo <binary> -test.run '^$' -test.bench '<名前>' -test.benchtime 20x -test.count 5
# で before1 → after → before2
```

判定は**同じ binary の pointer に対する比**で行い、before の比と並べる:

| 指標 | 線 | 7d-0 の値 (2026-09-28、load 1.8〜2.1) |
| --- | --- | --- |
| Walk cycles/visit、store / pointer | ≤ 1.15 | 1.093 |
| Parse cycles/op、store / pointer (checker.ts) | ≤ 1.00 | 0.822 |
| **Parse cycles/op、store / pointer (dom)** | **≤ 1.00 (案。ユーザーの確認待ち)** | 0.870 |
| Bind cycles/node、store / pointer | ≤ 1.10 | 1.014 |
| 4 本の inst と IPC、before からの差 | 報告 | |

- Walk と Bind (checker.ts は TS) は 7d-1 で変わらないはず。before から有意に動いたら理由を探す (Builder の変更や生成の `ForEachChild` の変化)。
- Parse の差は `withJSDoc` の TS の速い側と、`@see` / `@link` の eager parse。dom の比が上がったら、dom の `@see` / `@link` を含む comment の数と、`withJSDoc` の速い側の命令数で切り分ける。
- **JS の parse と reparse の費用は KPC の入力に無い** (checker.ts と dom は TS)。corpus か fixtures の JS の file で ns / B/op / allocs を Pointer と比べ、参考として報告する (線は置かない)。

自分で取るもの (参考、load average を記録):

```sh
cd $TSC && go test -run '^$' -bench 'StoreWalkV1|StoreParseV1|StoreBindV1' -benchtime 2s -count 5 ./internal/ast/store/ ./internal/storeparser/ ./internal/storebinder/ | tee $OUT/ns.txt
```

**遅延 JSDoc のベンチ** (実装指示書 §7): dom の全 host の `JSDoc(file)` を Pointer と Store で ns / B/op / allocs (と KPC 版があれば cycles / inst) を比べる。線は置かず、7d-2 の Check に含まれる費用の前見積りとして報告する。

## 5. retained heap と GC

7d-0 の検証指示 §5 と同じ手順 (variant ごとに別の新しいプロセス、GC 2 回の後の値、交互に各 5 回) で、checker.ts / fixtures / dom の parse + bind の retained と mark CPU を取る。

| 指標 | 線 | 7d-0 の値 |
| --- | --- | --- |
| retained B/node、store / pointer | checker.ts ≤ 0.60、fixtures ≤ 0.70 | 0.565 / 0.602 |
| GC の mark CPU、store / pointer | checker.ts ≤ 0.25、fixtures ≤ 0.35 | 0.136 / 0.207 |

- TS の JSDoc は遅延なので、parse + bind の retained には eager 分 (`@see` / `@link`) と `File` の cache の map しか乗らないはず。fixtures に JS があれば、JS の JSDoc と reparse のノードが乗る。fixtures の JS の file の数と、7d-0 からの retained の増分を報告する。
- **遅延 JSDoc の Store の retained**: dom の全 host を引いた後の、遅延 JSDoc の Store の数と B/host を Pointer (`jsdocCache` の map と JSDoc ノード) と比べて報告する (線は置かない)。

## 6. レポート

`tsc/internal/ast/docs/store-checker-7d1-verification-report-<YYYYMMDD>.md` に、既存の文書と同じ文体で:

1. 結論 (合否と数字。§4 / §5 の表を埋めたもの)
2. 環境と入力
3. 正しさ (§1。外した除外と緩めの確認、parse / bind の等価の範囲、JSDoc の木の等価の件数、遅延 JSDoc の契約、既知の fuzz seed の確認)
4. 移植の機械性 (§2。jsdoc.go / reparser.go の関数単位の差分、parser.go の hunk、speculation、変換器の再実行)
5. API と命令列 (§3)
6. 費用 (§4。before / after の 4 本、JS の参考値、遅延 JSDoc のベンチ)
7. retained heap と GC (§5。遅延 JSDoc の Store を含む)
8. 7d 設計への反映案 (実装指示書 §9 の決めたことの妥当性、dom の線、遅延 JSDoc の費用の見積り、7d-2 に回すもの)
9. 限界
10. 再現手順と生データの場所

合格なら次は 7d-2 の設計の残り (7d 設計 §4 の TODO 5)。不合格でも §4 / §5 の数字は合否に関わらずレポートに残す。
