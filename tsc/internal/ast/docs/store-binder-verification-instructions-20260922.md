# Store binder (TODO 7c) 検証指示

作成日: 2026-09-22。対象は検証を担当するエージェント。何を作ったかは [store-binder-implementation-instructions-20260922.md](store-binder-implementation-instructions-20260922.md) (以下「実装指示書」)。設計は [store-binder-design-20260922.md](store-binder-design-20260922.md) (以下「7c 設計」) と [store-ast-design-20260922.md](store-ast-design-20260922.md) §1 のゲート 2。

目的は 3 つ。**(1) 移植が Pointer binder と同じ symbol / locals / flow / flags / 診断を作ること**、**(2) Bind ゲート (cycles/node ≤ 1.1 × Pointer) の合否**、**(3) header を 32B にした影響を Walk (7a) と Parse (7b) で取り直し、bind 後の retained / GC を Pointer と比べること**。

## 0. 規則

7b の検証指示 §0 と同じ。実装を直して数字を良くしない。直してよいのは正しさのバグだけで、直したら報告に書く。生の出力は `tsc/internal/ast/docs/_store-binder-results/` に保存する。コマンドは絶対パス (`TSC=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc`、`OUT=$TSC/internal/ast/docs/_store-binder-results`)。環境 (go version、commit、load average) を記録する。

**計測前に source の sha256 を取り、終了時に不変を確認する** (`internal/storebinder/*.go`、`internal/storeparser/*.go`、`internal/ast/store/**/*.go`、`generate-go-store.ts`)。

KPC は root が要るのでユーザーに依頼する (§4)。ns / allocs / retained は自分で取る。

## 1. 正しさ

```sh
cd $TSC/.. && node tools/scripts/tsc/generate.ts && git status --short
cd $TSC && go build ./... && go vet ./internal/ast/... ./internal/storeparser/... ./internal/storebinder/... && git status --short
cd $TSC && git diff --stat 326bc57b8a -- internal/binder internal/parser internal/scanner internal/ast ':!internal/ast/store' ':!internal/ast/docs'   # 空 (基準は 7c 設計の commit。実装が commit していても効く)
cd $TSC && go test -count=1 ./internal/ast/store/... ./internal/storeparser/... ./internal/storebinder/...
cd $TSC && go test -tags storechecks -count=1 ./internal/ast/store/... ./internal/storeparser/... ./internal/storebinder/...
cd $TSC && grep -rl '"github.com/microsoft/TypeScript/tsc/internal/\(ast/store\|storeparser\|storebinder\)' --include='*.go' . | grep -v 'internal/ast/store/\|internal/storeparser/\|internal/storebinder/'   # 空
cd $TSC && go test -count=1 ./internal/ast/ ./internal/parser/ ./internal/binder/
```

確認すること:

1. 実装指示書 §1 のガード (package、import、`internal/binder` / `parser` / `scanner` / `ast` の不変、`doc.go`)。
2. corpus テストが `-short` 無しで走り、file 数が 7b (17,415) と同じ入力であること。**除外の内訳**: JS の「Reparsed かつ symbol あり」の file 数 (7c 設計は 293) と番号。7b の await 8 file (#713、#714、#3037、#5255、#7521、#8555、#12580、#12584) が除外されず一致していること。
3. **parse の等価 (7b) が 32B + reparse 有効 + indicator ありで通る**こと。7b の `TestCorpus` の mismatch 0、診断の差分が 7b と同じ 10 file (JSDoc 由来の code 1003) だけであること。
4. **bind の等価**: driver が比べる項目が実装指示書 §6.1 の全部であることを driver のコードで確認する (項目が抜けていれば mismatch 0 は意味が無い)。id を含む名前の正規化 (private 名の `#<id>@`、pattern ambient module の `pattern@<id>`) 以外の緩めが無いこと。
5. **flow slab の個数**: fixture ごとの `len(flows)` / `len(flowLists)` / `len(flowData)` と、7c 設計 §2.1 の到達可能数との差 (到達不能な label の分)。
6. **`store` に足した API**: 実装指示書 §3 の一覧と現物を照合し、増えたものを列挙する。`unsafe` が `Ref()` と `Refs()` 以外に無いこと。`Builder.SetFlags` / `SetLoc` が消えていること。`unsafe.Sizeof(NodeHeader{}) == 32`、`unsafe.Sizeof(store.Symbol{}) == 96`、`unsafe.Sizeof(store.FlowNode{}) == 16` を一時テストで確認する。
7. **Seal**: `-tags storechecks` で bind 後の書き込みが panic するテストが通ること。

## 2. 移植の機械性

```sh
cd $TSC && diff -u internal/binder/binder.go internal/storebinder/binder.go > $OUT/binder.diff; wc -l $OUT/binder.diff
cd $TSC && diff -u internal/parser/references.go internal/storeparser/references.go > $OUT/references.diff
cd $TSC && diff -u internal/ast/symbol.go internal/ast/store/symbol.go > $OUT/symbol.diff
```

- 関数の名前と順序が同じであること (`grep -n '^func ' … | sed 's/(.*//'` を両方で取って `diff`)。
- diff の hunk を全部読む。分類 (a) 型の置換、(b) 出力先の置換、(c) 子の受け直し、(d) flow の index 化、(e) JSDoc 系の削除 (7c では 0 のはず)、(f) それ以外、(g) 読みのまとめ、で数えて報告する。(f) は実装報告の一覧と照合し、**header の読み回数が Pointer 版と並べて書かれていない項目は差し戻す**。Pointer にも効く書き換え (例: `checkContextualIdentifier` の条件順) が入っていないこと。
- (g) の diff (逐語版 → まとめ版) を実装が保存しているので、まとめた変数が bind 中に変わるもの (`Flags()`、`Symbol()`、`FlowNode()`、`LocalsSlot()`、`file` / `Bound` の field) を含まないことを 1 箇所ずつ見る。
- `store/utilities.go` の各関数が `internal/ast/utilities.go` の同名関数と同じ順序・同じ分岐であること (一時的に並べて `diff`)。

## 3. API と命令列

```sh
cd $TSC && go build -gcflags='-m=2' ./internal/ast/store/ 2>&1 | grep -E 'can inline|cannot inline' > $OUT/inline-store.txt
cd $TSC && go build -gcflags='-m=2' ./internal/storebinder/ 2>&1 | grep -E 'can inline|cannot inline' > $OUT/inline-binder.txt
cd $TSC && go build -gcflags='-d=ssa/check_bce/debug=1' ./internal/ast/store/ 2>&1 | grep -i 'generated' > $OUT/bce-store.txt
cd $TSC && go test -c -o $OUT/storebinder.test ./internal/storebinder/
cd $TSC && go tool objdump -s 'store\.Node\.Ref$' $OUT/storebinder.test > $OUT/asm-ref.txt
cd $TSC && go tool objdump -s 'storebinder\.\(\*Binder\)\.bind$' $OUT/storebinder.test > $OUT/asm-bind.txt
cd $TSC && go tool objdump -s 'storebinder\.\(\*Binder\)\.bindChildren$' $OUT/storebinder.test > $OUT/asm-bind-children.txt
cd $TSC && go tool objdump -s 'storebinder\.\(\*Binder\)\.bindContainer$' $OUT/storebinder.test > $OUT/asm-bind-container.txt
cd $TSC && go tool objdump -s 'storebinder\.\(\*Binder\)\.addAntecedent$' $OUT/storebinder.test > $OUT/asm-add-antecedent.txt
```

| 対象 | 期待 |
| --- | --- |
| `Node.Ref()` | 乗算 (`MUL` / `UMULH`) が無く、shift (`LSR #5`) になっている |
| `Node.FlowNode` / `Symbol` / `SetFlow` / `SetSymbol` / `AddFlags` / `ClearFlags`、生成 setter、`LocalsSlot` 等の役割 accessor、`Store.Node` | can inline。`storeChecks` 無しの build で `sealed` の読みが無い |
| `bind` | 自分の header を読む `MOVH` / `MOVWU` はあるが、`nodes` の base + index の計算 (`node()` の形) は子を辿る箇所と親 1 段 (`IsIdentifierName`、`GetContainerFlags` の Block) 以外に無い |
| `bindChildren` | 同上。kind の switch がジャンプテーブル (間接分岐 1 回) |
| `bindContainer` | `Node` の save / restore が 2 word の store / load |
| `addAntecedent` | ループが `flowLists` の base + index。`growslice` は append の箇所だけ |
| `nameSlot` 型の表引き (`localsSlot` 等) | check_bce の報告に無い (`&511`) |
| 7a の accessor の inline 判定 | 7a / 7b と同じ。32B 化で `cannot` が増えていないこと |

## 4. KPC: 32B の before / after と Bind ゲート

**before** は 7c の変更前の commit (7c 設計の commit) で取る。ユーザーが別ターミナルで (load average < 1.5 を待ってから):

```sh
# before (7c 設計の commit を checkout した状態)
cd $TSC && go test -tags kperf -c -o /tmp/store24.test ./internal/ast/store/ && go test -tags kperf -c -o /tmp/parse24.test ./internal/storeparser/ && go test -tags kperf -c -o /tmp/bind24.test ./internal/binder/
cd $TSC/internal/ast/store && sudo /tmp/store24.test -test.run '^$' -test.bench 'StoreWalkKPCV1' -test.benchtime 20x -test.count 5 | tee $TSC/internal/ast/docs/_kpc-baselines/store-24b-before-$(date +%Y%m%d).txt
cd $TSC/internal/storeparser && sudo /tmp/parse24.test -test.run '^$' -test.bench 'StoreParseKPCV1' -test.benchtime 20x -test.count 5 | tee -a $TSC/internal/ast/docs/_kpc-baselines/store-24b-before-$(date +%Y%m%d).txt
cd $TSC/internal/binder && sudo /tmp/bind24.test -test.run '^$' -test.bench 'ASTBindKPCV1' -test.benchtime 20x -test.count 5 | tee -a $TSC/internal/ast/docs/_kpc-baselines/store-24b-before-$(date +%Y%m%d).txt
# after (7c の binary)
cd $TSC && go test -tags kperf -c -o $OUT/store.kperf.test ./internal/ast/store/ && go test -tags kperf -c -o $OUT/storeparser.kperf.test ./internal/storeparser/ && go test -tags kperf -c -o $OUT/storebinder.kperf.test ./internal/storebinder/
cd $TSC/internal/ast/store && sudo $OUT/store.kperf.test -test.run '^$' -test.bench 'StoreWalkKPCV1' -test.benchtime 20x -test.count 5 | tee $OUT/kpc-walk-$(date +%Y%m%d-%H%M).txt
cd $TSC/internal/storeparser && sudo $OUT/storeparser.kperf.test -test.run '^$' -test.bench 'StoreParseKPCV1' -test.benchtime 20x -test.count 5 | tee $OUT/kpc-parse-$(date +%Y%m%d-%H%M).txt
cd $TSC/internal/storebinder && sudo $OUT/storebinder.kperf.test -test.run '^$' -test.bench 'StoreBindKPCV1' -test.benchtime 20x -test.count 5 | tee $OUT/kpc-bind-$(date +%Y%m%d-%H%M).txt
```

自分で取るもの:

```sh
cd $TSC && go test -run '^$' -bench 'StoreBindV1' -benchtime 2s -count 5 ./internal/storebinder/ | tee $OUT/bind-ns.txt
cd $TSC && go test -run '^$' -bench 'StoreWalkV1' -benchtime 2s -count 5 ./internal/ast/store/ | tee $OUT/walk-ns.txt
cd $TSC && go test -run '^$' -bench 'StoreParseV1' -benchtime 2s -count 5 ./internal/storeparser/ | tee $OUT/parse-ns.txt
```

基準値 (Pointer、checker.ts、GC オフの Session):

| ベンチ | Pointer | per node / visit | 出所 |
| --- | ---: | ---: | --- |
| Walk cycles | 25.67 cycles/visit (72.4 inst) | | `_kpc-baselines/kpc-after.txt`、7a |
| Parse cycles | 67.35M (S1 の run) | 226 | 7b 報告 §11.2 |
| Bind cycles | 33.66〜34.14M (3 run)、inst 110.3〜110.8M | 113.5 cycles、371 inst (÷ 298,054) | `_kpc-baselines/kpc-after.txt` |

判定は**同じ binary の pointer に対する比**で行う (基準値は再現の照合用。pointer の inst の幅が 1.5% を超えたら測り直す)。

| 指標 | 線 | 状態 |
| --- | --- | --- |
| Walk cycles/visit、store / pointer | ≤ 1.15 (ゲート 1)。24B の before (1.137) と並べ、悪化していれば `node()` の命令列で理由 | 決定済み |
| Parse cycles/op、store / pointer | ≤ 1.00 (7b の線)。24B の before (S1 後 0.813) と並べる。`Finish` の copy が 8B/node 増える分は B/op に出る | 決定済み (7b) |
| **Bind cycles/node、store / pointer** | **≤ 1.10** | **決定済み (ゲート 2)** |
| Bind inst/node、IPC | 報告。増えた分は §3 の命令列と (f) の表で内訳 | |
| Bind B/op、allocs/op | 報告 (Pointer は 7.42 MB / 13,952。`SymbolTable` の map が主) | |

Bind が 1.10 を超えたら、7c 設計 §2.2 の順に理由を探す: (1) `node()` が辺 1 本に 1 回になっているか (§3)、(2) `Ref()` の回数 (overlay の計数で 0.25〜0.3 回/node か。flow 生成の `Node`、宣言の `FileRef`、`containers`、pattern 名だけのはず)、(3) `IsIdentifierName` の Parent + `Name()` (0.41 × 20 inst)、(4) 予約 slot の setter が表引きになっていないか、(5) 1 word 通貨 (`*NodeHeader`) の試作。順に切り分けて、どれで説明できるかを報告に書く。

## 5. retained heap と GC

```sh
cd $TSC && go test -run '^$' -bench 'StoreBindRetainedV1' -benchtime 1x -count 5 ./internal/storebinder/ | tee $OUT/retained.txt
cd $TSC && go test -c -o $OUT/storebinder.test ./internal/storebinder/
cd $TSC/internal/storebinder && GODEBUG=gctrace=1 $OUT/storebinder.test -test.run '^$' -test.bench 'StoreBindRetainedV1/fixtures/store' -test.benchtime 1x -test.count 1 2> $OUT/gctrace-store.txt
cd $TSC/internal/storebinder && GODEBUG=gctrace=1 $OUT/storebinder.test -test.run '^$' -test.bench 'StoreBindRetainedV1/fixtures/pointer' -test.benchtime 1x -test.count 1 2> $OUT/gctrace-pointer.txt
```

gctrace はテストバイナリを直接実行する (`go test` 経由だと go コマンド自身の GC が混じる。7b 報告 §10)。retained は GC 2 回の後の値 (7b 報告 §7.2)。

見積り (B/node)。比は symbol の密度で動く (symbol 96B と `SymbolTable` は両側に同量乗る) ので、入力ごとに見る。T ≈ 10〜15 は map 等の共通分 (Pointer の bind B/op 24.9 B/node から flow 5.7 と symbol 5.9 を引いた残り):

| 入力 | Store | Pointer | 比 |
| --- | ---: | ---: | ---: |
| checker.ts (symbol 6.2%) | 35.1 (7b) + 8 (header) + 0.4 (slot) + 2.9 (flow) + 5.9 (symbol) + 0.5 (表) + T = 52.8 + T | 94 + 5.7 + 5.9 + T = 105.6 + T | 0.54 |
| dom (23.5%) | 69 + T | 118 + T | 0.63 |

GC ms: Store 側で scan されるのは symbol + map + `symbols` 表 (S ≈ 16〜19 B/node)、Pointer は全部 (≈ 100 + S)。比 ≈ S / (100 + S) = 0.14〜0.16 (checker.ts)、dom 密度で約 0.25。map は string key + pointer 値で bytes 比より重い。

| 指標 | 線 | 状態 |
| --- | --- | --- |
| retained B/node、store / pointer (parse + bind) | checker.ts ≤ 0.60 (見積り 0.54)、fixtures 166 file ≤ 0.70 (dom 系が多い)、dom は報告 | **提案。ユーザーが決める** |
| 保持中の `runtime.GC()` 1 回の ms、store / pointer | checker.ts ≤ 0.25 (見積り 0.14〜0.16)、fixtures ≤ 0.35 | **提案。ユーザーが決める** |
| gctrace の `MB live` と mark cpu | 報告 | |
| `Footprint()` + `Bound` の各 slab の bytes の合計 | retained との差を説明する (差は `File`、diagnostics、map の overhead、size class) | |

## 6. `File` の field と parser 側の出力

7b の検証 §6 の突き合わせに、7c で足した field を加える: `ExternalModuleIndicator` (kind, pos)、`Imports` / `ModuleAugmentations` の (kind, pos) 列、`AmbientModuleNames`、`UsesUriStyleNodeCoreModules`。corpus 全体で一致件数と不一致の label を報告する。`walkTreeForJSXTags` の枝刈りが無い分の費用は、TSX で import / export の無い file がいくつあるかを数えて報告する (parse の KPC は checker.ts なので出ない)。

## 7. レポート

`tsc/internal/ast/docs/store-binder-verification-report-<YYYYMMDD>.md` に、既存の文書と同じ文体で:

1. 結論 (合否と数字。§4 / §5 の表を埋めたもの。線が未決のものは「提案線に対して」と書く)
2. 環境と入力
3. 正しさ (§1。等価の範囲: file 数、除外の内訳、driver が比べた項目、flow slab の個数)
4. 移植の機械性 (§2。分類 (a)〜(g) と (f) / (g) の一覧)
5. `store` に足した API と generator の出力差分 (§3)
6. bind の費用 (§4。before / after の 3 ベンチと Bind ゲート。inst の内訳が要るなら命令列で)
7. retained heap と GC (§5)
8. 設計文書への反映案 (7c 設計と store-ast-design の両方。特に 32B の Walk / Parse の実測、`Ref()` の回数、(f) で分かった header の読み回数)
9. 限界
10. 再現手順と生データの場所

合格なら次は 7d (checker の設計)。不合格でも §5 の数字は設計の賭けの実測なので、合否に関わらずレポートに残す。
