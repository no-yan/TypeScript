# Store checker 変換 (TODO 7d-2a) 検証指示

作成日: 2026-09-29。対象は検証を担当するエージェント。何を作ったかは [store-checker-7d2a-implementation-instructions-20260929.md](store-checker-7d2a-implementation-instructions-20260929.md) (以下「実装指示書」)。設計は [store-checker-7d2-design-remainder-20260929.md](store-checker-7d2-design-remainder-20260929.md) (以下「残りの設計」) と [store-checker-design-20260923.md](store-checker-design-20260923.md) (以下「7d 設計」)。

目的は 4 つ。**(1) 生成物が変換器だけで再現し、storechecker と storeprinter などが build できる**、**(2) 変換が機械的で、P2 (手書き) が理由のある最小限に収まる**、**(3) checker 無しで閉じる 2 つの単体等価 (nameresolver、printer) が不一致 0 で、その比較が本当に独立に比べている**、**(4) 既存の parse / bind が変わらない**。2a は性能の線を持たない (Check の性能は 2c)。

## 0. 規則

7d-1 の検証指示 §0 と同じ。実装を直して結果を良くしない。直してよいのは正しさのバグだけで、直したら報告に書く。コマンドは絶対パス (`ROOT=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、`TSC=$ROOT/tsc`、`OUT=$TSC/internal/ast/docs/_store-checker-7d2a-results`)。

- 生の出力 (text) は `$OUT` (ignore 対象) に保存する。**before の展開、一時 binary、`-gcflags=-m=2` の大きな出力は scratchpad に置く** (repo の中に置くと Spotlight の索引で load が上がる)。build は `GOWORK=off`。
- 環境 (go version、commit、load average) を記録する。検証の最初と最後に source の sha256 を取り、不変を確かめる (`internal/ast/store/**/*.go`、`internal/store*/**/*.go`、`tools/cmd/storetranslate/*.go`、`tools/scripts/tsc/generate-go-store.ts`)。
- before は 7d-2a の直前の commit (実装の報告に書かれたもの。無ければ **2341effa51**)。
- 既知の失敗は `FuzzEquivalence/a708303f611b95c8` (`ExternalModuleIndicator differs`) の 1 件だけ。同じ理由で落ちることを確かめ、それ以外の失敗は 1 件でも不合格とする。

## 1. 正しさと再現

```sh
cd $ROOT && node tools/scripts/tsc/generate.ts && (cd tools && go run ./cmd/storetranslate) && git status --short
cd $TSC && go build ./... && go vet ./internal/ast/... ./internal/storeparser/... ./internal/storebinder/... ./internal/storechecker/... ./internal/storeprinter/... ./internal/storeevaluator/... ./internal/storescanner/... ./internal/storenodebuilder/... ./internal/storepseudochecker/... ./internal/storemodulespecifiers/...
cd $ROOT/tools && go vet ./cmd/storetranslate/... && go test -count=1 ./cmd/storetranslate/
cd $TSC && git diff --stat <before> -- internal/ast internal/checker internal/printer internal/binder internal/parser internal/scanner internal/evaluator internal/nodebuilder internal/pseudochecker internal/modulespecifiers go.mod ':!internal/ast/store' ':!internal/ast/docs'   # 空
cd $TSC && git diff <before> -- internal/storebinder/binder.go internal/storeparser ':!*_test.go' > $OUT/file-migration.diff   # §4.2 の File の field の移行の置き換えと ParseIsolatedEntityName だけ
cd $TSC && go test -count=1 -timeout 30m -v ./internal/ast/store/... ./internal/store... > $OUT/test-notag.txt 2>&1
cd $TSC && go test -tags storechecks -count=1 -timeout 30m -v ./internal/ast/store/... ./internal/store... > $OUT/test-storechecks.txt 2>&1
```

確かめること:

1. 実装指示書 §1 のガード。`file-migration.diff` の hunk が全部、`File` の `FileName`・`Path`・`Text`・`Imports` の読み書きを移行後の method / setter に置き換えたもの (と storeparser の `ParseIsolatedEntityName`) であり、それ以外の変更が無いこと。移行後の `*store.File` が `ast.HasFileName` を満たすこと (`var _ ast.HasFileName = (*store.File)(nil)` がコンパイルできる)。生成物が 2 つの生成器で byte 一致で再現すること (再生成の後に `git status` 不変)。**生成物に手で直した跡が無いこと**: 生成物の file は全部、先頭の `Code generated` の注記があり、再生成で変わらない。手書きは `*_manual.go` と実装指示書 §4 の file だけ。
2. production から store 系の package の import が無いこと (`go list -deps` で、store 系以外の package の依存に store 系が現れない)。
3. parse の等価 17,415 file と bind の等価 17,415 file が mismatch 0、宣言の Store の検査 717,627 件が変わらないこと (7d-1 の値)。corpus の件数と dead node の数 (7d-1 は 22,793) も記録して比べる。
4. `unsafe` が 7d-1 から増えていないこと。

## 2. 変換の機械性

### 2.1 規則

- 実装報告 (a) の規則の一覧を、変換器のコード (`tools/cmd/storetranslate/`) と突き合わせる。各規則が **go/types の型と store の API だけで決まる**こと。変換器のコードに、関数名・変数名・型名の表 (package の対応表と、Pointer と同名の API への恒等の対応を除く) が無いことを `rg` と目視で確かめる。あれば「導けない知識」を変換器に持ち込んだ箇所として報告する。
- **R8 (書き込み)**: 持ち主の Factory の探し方が「receiver か引数の struct の `NodeFactory` 型の field」で、変数名 (`c`、`b`、`factory`、`f`) を見ていないこと。27 箇所 (残りの設計 §2.1 の表) が全部 writer の呼び出しになり、`f` が期待どおり (`c.factory` / `b.f`) であることを出力で確かめる。実装報告 (h) の書き込み先の分類を、各箇所のノードの出どころを読んで照合する (source のノードへの書き込みがあれば、2b で panic する箇所として報告に残す。2a は checker を動かさないので実行では分からない)。nodecopy.go:886 の `NodeList` の `Loc` が `SetListRange` になっていること。
- **R4' (`Nodes()` のコピー)**: 実装報告 (b) の一覧が 11 箇所 (実装指示書 §3.3 の R4' の行) と一致し、生成物の `Nodes()` の呼び出しがその一覧の外に無いこと (`rg '\.Nodes\(\)' internal/storechecker internal/storeprinter` で数える)。`getEffectiveCallArguments` が P2 でなく変換の出力であること。
- 規則ごとに変換テスト (実装指示書 §6.4) があること。R8 の見つからない例と 2 つある例が problem になるテストがあること。
- (g) (読みのまとめ) の規則が入っていないこと。

### 2.2 到達計算

- 実装報告の到達の表を、7d 設計 §2.1 の試作の表 (CHA / RTA) と並べる。checker.go はほぼ全部、services.go・exports.go・emitresolver.go は大半が落ちる見込み。storeprinter は `Write` が emit と共通の入口なので printer.go の大半が残る見込み (残りの設計 §2.4)。大きく違えば理由を確かめる。
- **刈りすぎの点検**: 刈った関数のうち 20 個を無作為に選び、根から到達しないことを確かめる。直接の呼び出し元を grep して、それぞれが (a) 刈られた関数か、(b) 変換対象外の package か、(c) 到達したが関数値・interface 経由でなく名指していない、のどれかであること。(a) は同じ点検を再帰的に根に向かって続け、途中で到達した関数が名指していれば刈りすぎ。1 段の grep で終えない。interface の実装 (`SymbolTracker` の実装型、`Program` の実装など) が刈られていないこと。
- **刈り残しの点検**: storechecker に出た関数のうち、services.go / exports.go / emitresolver.go のものを全部並べ、根からの経路を 1 つずつ示せること (実装報告か変換器の出力に経路があれば照合、無ければ自分で辿る)。

### 2.3 P2

```sh
cd $TSC && ls internal/ast/store/*_manual.go internal/store*/*_manual.go
```

- P2 の各関数に hash の注記があり、`(cd tools && go run ./cmd/storetranslate)` が hash を照合していること (1 つの注記の hash を書き換えて変換器が止まることを確かめ、元に戻す。source の sha256 で戻ったことを確認する)。
- **P2 の各関数を Pointer の関数と並べて diff を読む。** 違いが実装報告 (b) の理由 (実装指示書 §3.4 の表と、足したものは 1 件ずつの理由) だけであること。理由の外の違い (条件・順序・分岐の違い) があれば 1 件ずつ報告する。**List 版** (実装指示書 §3.4) は元の Pointer の関数と、引数の型と List の規則の置き換えだけが違うこと。list を渡す呼び出し元が全部 List 版を、slice を渡す呼び出し元が全部元の関数を呼んでいること (生成物を grep する)。特に `NewChecker` の P2 は、変換器の出力と region / Factory の 2 行だけが違うこと (変換器にその関数を一時的に出させて diff を取る。出させる方法が無ければ Pointer の関数との diff を読む)。
- P2 の数と行数を、実装報告 (b) の値と照合する。

### 2.4 生成物の読み

storechecker の到達した関数から 20 個を無作為に選び (checker.go から 10、flow.go・relater.go・nodebuilderimpl.go・jsx.go から計 10)、Pointer の関数と並べて読む。違いが規則の置き換え (型、`IsNil`、`SameNode`、accessor、`Key()`、`List` のループ、writer など) だけで、制御構造と評価順が同じであること。**nil の比較を含む式、ノードの `==`、list の range、map の key、書き込み**が 1 つ以上ずつ入るように選ぶ。

## 3. 単体等価

### 3.1 nameresolver (実装指示書 §6.2)

1. テストを走らせ、実装報告 (e) の数 (file 数、呼び出し数、nil 以外の結果の数、不一致 0) が再現すること。
2. **比較が本当に比べていること**:
   - callback が両側で同じ意味であること (コードを読む)。Store 側だけに正規化や skip が無いこと。
   - 対応に使う symbol の対 (`bindComparer` の `symbols`) が、Resolve の結果と独立に作られていること (Resolve の結果から対を作っていないこと)。
   - nil 以外の結果が十分にあること (呼び出しの大半が nil なら、何も比べていない)。meaning ごとの nil 以外の数を記録する。
   - **わざと壊して落ちること**: storebinder の生成物の `nameresolver.go` の中の 1 箇所 (例: locals を見る条件の 1 つ) を一時的に変えて、テストが不一致を出すことを確かめ、元に戻す (sha256 で確認)。
3. 入れ子の関数・class・namespace・enum・`with`・catch 節・型の parameter・`arguments`・JS の `require` を含む file が corpus にあり、その識別子が比べられていること (該当する位置での呼び出し数を数える)。JSDoc の中の識別子 (JS の `@param {T}`・`@type`、TS の遅延 JSDoc) が除外されずに比べられ、Pointer の側で `lastLocation` が `KindJSDoc` になる呼び出し (nameresolver.go:64-74) が 1 回以上あること (数える)。
   - 注 (2026-09-30 ユーザー決定で要件から外した): `KindJSDocParameterTag`・`KindJSDocReturnTag` の 2 分岐は Go 版では到達しない。分岐に入るには lastLocation が function-like な location の直接の子で、かつその location の locals で名前が見つかる必要がある。これらの tag を直接の子に持つ function-like な kind は `JSDocSignature` だけ (`Parameters` と `Type`) で、binder の `GetContainerFlags` (binder/binder.go:2580) は `JSDocSignature` に `ContainerFlagsNone` を返すので、`b.container` にも `b.blockScopeContainer` にもならず、locals は常に nil。corpus で 0 回 (両検証報告)。Pointer の nameresolver.go:313-316 の comment もこの経路を no-op としている。Store 側の 2 項は Pointer からの機械的な変換のまま残す。
4. **callback の列の比較** (実装指示書 §6.2): 戻り値だけでなく callback の呼び出しの列を比べていること。fixture (空でない Globals、`excludeGlobals`、Alias、`isUse`、非 nil の message、`OnPropertyWithInvalidInitializer`、scope の変化の cache) が全部あり、各 fixture で Pointer の列にその callback が現れること。**わざと壊して落ちること**を 2 つ確かめる: 生成物の nameresolver.go から globals の検索 (Pointer の nameresolver.go:327 に当たる行) を消す、`SymbolReferenced` の呼び出し (322 に当たる行) を消す。どちらでもテストが落ち、元に戻す (sha256 で確認)。

### 3.2 printer (実装指示書 §6.3)

1. テストを走らせ、実装報告 (e) の数 (file 数、5 つの設定 × source file あり / なしの不一致 0、両方が panic した数) が再現すること。
2. **比較が本当に比べていること**:
   - 比べる相手が Pointer の parser と Pointer の printer の出力であること (Store の printer の出力どうしを比べていないこと)。
   - printer の設定が checker/printer.go の 5 つの関数と同じであること (コードを並べて確かめる)。`createPrinterWithRemoveCommentsNeverAsciiEscape` に checker の呼び出し元が無いことは既知で、不合格の理由にしない。
   - 出力が空や数文字でないこと (印字の種類ごとの出力の長さの合計を記録する)。
   - **source file が nil の経路**: 実装指示書 §6.3 の fixture (source file の引数だけを変えると Pointer の出力が変わる入力) を読み、元のノードの入力で Pointer の file あり / nil の出力が本当に違うこと (違わなければ nil の経路を試していない)、合成の複製の入力で両方が `a` であること (複製には出力の差を要求しない)、Store がそれぞれに一致することを確かめる。**coverage や「root だけの印字との差」で nil の経路を判定しない**: `canUseOriginalText` などの false は source file があっても別の理由で起きる (レビューの例: parser.numericSeparators.decimal.ts の `1_000_000_000`)。Go の coverage は複合条件のどの項が効いたかを区別しない。fixture が printer の nil に固有の分岐 (Pointer の printer.go:241-248 の `canUseSourceFile` の `currentSourceFile != nil` の項など) を 1 つ以上ずつ突いているかを、printer のコードを読んで確かめ、突いていない分岐があれば挙げる。
   - corpus の印字で Pointer の出力が file あり / nil で違った数 (実装報告) を記録する。
   - **EmitContext の情報の試験** (実装指示書 §6.3): emit flags・textSource・original・合成の comment の各項目で、Pointer の出力が設定のあり / なしで違う入力が使われていること (違わない項目は、実装報告がその理由を書いていること)。印字する EmitContext の Factory で作ったノードであること (空の hooks の Factory で作っていないこと)。
   - **わざと壊して落ちること**: storeprinter の生成物の中の 1 箇所 (例: `canUseSourceFile` の条件、残りの設計 §2.4 の (1)) を一時的に変えて不一致が出ることを確かめ、元に戻す。
3. 両方が panic する file があれば、その file と panic の内容を記録する (Pointer の printer の既知の制約か)。実装報告の両方 panic の数が cell (設定 × file あり / nil × 種類) ごとにあり、どの cell も大半が両方 panic でないことを確かめる。

### 3.3 変換の境界

- `storeparser.ParseIsolatedEntityName` (実装指示書 §4.4): Pointer と同じ入力 (`React.createElement`、`h`、`a.b.c`、不正な入力 `a.`、`1`、空) で、結果の木の kind と text が一致し、位置が全部 (-1, -1)、nil になる入力も一致すること。Store が seal 済みで、書き込みが拒否されること。StoreID が program の counter から来ていること。
- region と Factory (実装指示書 §5.1): `NewChecker` の P2 が region を `program.IDSpace()` から 1 つ作り (先頭の file に頼らない)、`getNodeBuilderEx` の EmitContext が同じ region に作られることをコードで確かめる。region の無い `storeprinter.NewEmitContext` が panic すること。**file が 0 件の Program の回帰試験** (実装指示書 §6.5) があり、Pointer と Store の `NewChecker` がどちらも正常に終わること。
- parse の診断の境界 (実装指示書 R11): checker.go:12733 に当たる生成物の callback の引数が `*ast.Diagnostic` のままで、その変換テストがあること。

## 4. 性能

2a は性能の線を持たない。確かめるのは、既存の hot path が変わっていないことだけ。

1. `git diff <before> -- internal/ast/store internal/storeparser internal/storebinder ':!*_test.go'` を読み、**既存の関数の本体の変更**を一覧にする。除外してよいのは、before に無かった関数 (足した関数) と、実装指示書 §4.3 の TODO の comment の書き換えだけ。**生成物 (`store/utilities.go`、`*_generated.go`) も除外しない**: 変換器の規則が変わると、before から在る関数の出力も変わりうる (storebinder は `HasSyntacticModifier` などを `utilities.go` から呼ぶ)。生成物は before と after の関数を名前で対にして本体を比べ (`_store-checker-7d1-results/verify-scripts/funcdiff.py` と同じ手順。method は receiver の型を名前に含めて対にする)、本体が変わった関数を全部挙げる。
2. 既存の関数の本体の変更が 1 つも無ければ、Walk / Parse / Bind の KPC は取らない (報告にそう書き、1 の比較の出力を添える)。**実装指示書 §4.2 の `File` の field の移行は storeparser と storebinder の関数の本体を変えるので、少なくとも Parse と Bind の KPC は取ることになる** (field の読みを inline の method にしただけなら命令列は同じ見込みだが、見込みで省かない)。
3. 変更があれば、変更した関数が Walk / Parse / Bind のベンチの経路にあるかを示し、経路にあれば KPC をユーザーに依頼する (7d-1 の検証指示 §4 と同じ手順、before1 → after → before2。線は Walk ≤ 1.15、Parse ≤ 1.00 (checker.ts と dom)、Bind ≤ 1.10)。binary と実行スクリプトは scratchpad に用意し、スクリプトは `sudo -v` の後で load < 1.5 を待つ。
4. inline の判定: `go build -gcflags=-m=2 ./internal/ast/store/` の出力で、`node()`、`Parent()`、生成の子 accessor、`List.At`、`FlowNode()`、`Symbol()` の判定と cost が before と同じであること (出力は scratchpad に置く)。

## 5. レポート

`store-checker-7d2a-verification-report-<日付>.md` に:

1. 結論 (合格 / 不合格と理由) と、線ごとの表。
2. §1 の再現・build・テストの結果。
3. §2 の規則・到達・P2・生成物の読みの結果。P2 の理由の外の違いと、刈りすぎ / 刈り残しの一覧。
4. §3 の単体等価の数と、わざと壊して落ちたことの記録。
5. §4 の diff の一覧と、KPC を取ったか (取ったなら結果)。
6. 判断を仰ぐこと。
7. 再現の手順。
