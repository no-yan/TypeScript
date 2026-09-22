# Store parser (TODO 7b) 検証指示

作成日: 2026-09-22。対象は検証を担当するエージェント。何を作ったかは [store-parser-implementation-instructions-20260922.md](store-parser-implementation-instructions-20260922.md) (以下「実装指示書」)。設計は [store-ast-design-20260922.md](store-ast-design-20260922.md) §2.7。

目的は 2 つ。**(1) 移植が Pointer parser と同じ木と同じ診断を作ること**、**(2) parse の費用と、parse 結果の GC / memory を Pointer と比べること**。(2) は Store 設計の賭け (noscan、footprint、割り当て数) を初めて実測する段階で、Walk (7a) が命令数で負けたままここに来ていることを前提に読む。

## 0. 規則

7a の検証指示 §0 と同じ。実装を直して数字を良くしない。直してよいのは正しさのバグだけで、直したら報告に書く。生の出力は `tsc/internal/ast/docs/_store-parser-results/` に保存する。コマンドは絶対パス (`TSC=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc`、`PKG=./internal/storeparser/`、`OUT=$TSC/internal/ast/docs/_store-parser-results`)。環境 (go version、commit、load average) を記録する。

**計測前に source の sha256 を取り、終了時に不変を確認する** (`internal/storeparser/*.go`、`internal/ast/store/**/*.go`、`generate-go-store.ts`)。

KPC は root が要るのでユーザーに依頼する (§4)。ns / allocs / retained は自分で取る。

## 1. 正しさ

```sh
cd $TSC/.. && node tools/scripts/tsc/generate.ts && git status --short
cd $TSC && go build ./... && go vet ./internal/ast/... ./internal/storeparser/... && git status --short
cd $TSC && git diff --stat HEAD -- internal/parser internal/scanner internal/ast ':!internal/ast/store' ':!internal/ast/docs'   # 空
cd $TSC && go test -count=1 ./internal/ast/store/... ./internal/storeparser/...
cd $TSC && go test -tags storechecks -count=1 ./internal/ast/store/... ./internal/storeparser/...
cd $TSC && grep -rl '"github.com/microsoft/TypeScript/tsc/internal/\(ast/store\|storeparser\)' --include='*.go' . | grep -v 'internal/ast/store/\|internal/storeparser/'   # 空
cd $TSC && go test -count=1 ./internal/ast/ ./internal/parser/
```

確認すること:

1. 実装指示書 §1 のガード (package、import、`internal/parser` / `scanner` / `ast` の不変、`doc.go`)。
2. corpus テストが `-short` 無しで走り、file 数と node 数が 7a (17,415 file / 2,584,467 node) と同じ入力であること。skip されたディレクトリが無いこと。
3. **診断の差分 (JS 系)**: テストが `t.Log` する差分を全部読み、1 件ずつ JSDoc / reparse 由来であることを message から確認する。由来を説明できないものがあれば移植のバグとして扱う。
4. **dead node**: fixture ごとの `NodeCount − 訪問数` と、corpus 全体の合計、Pointer の `NodeCount` との比。speculation の truncate テストが通ること。
5. **`View` の規則**: 実装報告の一覧を、自分でも `grep -n 'View(' $TSC/internal/storeparser/*.go` で取り直して照合する。1 行ずつ、戻り値が次の parse 呼び出しまでに使い切られていることを見る。
6. **`store` に足した API**: 実装指示書 §3 の一覧と現物を照合し、増えたものを列挙する。`unsafe` が `Ref()` と `Refs()` 以外に無いこと (`grep -n unsafe internal/ast/store/*.go`)。

## 2. 移植の機械性

```sh
cd $TSC && diff -u internal/parser/parser.go internal/storeparser/parser.go > $OUT/parser.diff; wc -l $OUT/parser.diff
```

- 関数の名前と順序が同じであること (`grep -n '^func ' … | sed 's/(.*//'` を両方で取って `diff`)。
- diff の hunk を全部読む (長いが、これが移植の正しさの主な根拠である。等価テストは木の形しか見ない)。次の分類で数えて報告する: (a) 型の置換だけ、(b) constructor の折り畳み (`finishNode` → `NewXxx(flags, pos, end, …)`)、(c) 子をローカルに受け直した (実装指示書 §4.2 の例外)、(d) 書き換え箇所の置換 (§4.3 の 9 つ)、(e) JSDoc / reparse の削除、(f) **それ以外**。(f) は 1 つずつ理由を確認し、報告に列挙する。
- 実装指示書 §4.3 の表と現物を照合し、`SetParent` 相当が無いことを確認する。
- `finishNodeWithEnd` の 3 箇所 (JSX、`parseTupleElementType`) で `end` が正しく渡っていること。

## 3. Builder と constructor の形

```sh
cd $TSC && go build -gcflags='-m=2' ./internal/ast/store/ 2>&1 | grep -E 'can inline|cannot inline' > $OUT/inline-store.txt
cd $TSC && go build -gcflags='-m=2' $PKG 2>&1 | grep -E 'can inline|cannot inline' > $OUT/inline-parser.txt
cd $TSC && go test -c -o $OUT/storeparser.test $PKG
cd $TSC && go tool objdump -s 'store\.\(\*Builder\)\.NewIdentifier$' $OUT/storeparser.test > $OUT/asm-new-identifier.txt
cd $TSC && go tool objdump -s 'store\.\(\*Builder\)\.NewCallExpression$' $OUT/storeparser.test > $OUT/asm-new-call.txt
cd $TSC && go tool objdump -s 'store\.\(\*Builder\)\.List$' $OUT/storeparser.test > $OUT/asm-list.txt
cd $TSC && go tool objdump -s 'store\.\(\*Builder\)\.View$' $OUT/storeparser.test > $OUT/asm-view.txt
```

| 対象 | 期待 |
| --- | --- |
| `View`、`AddFlags`、`SetLoc`、`Mark`、`Truncate`、`header`、`adopt`、`adoptList` | can inline |
| 7a の accessor の inline 判定 | 7a と同じ (`Node.Text` だけ cannot)。`store.go` の変更で変わっていないこと |
| `NewIdentifier` | `identifierData` の compare が `memequal` の call になっていること (source の部分文字列なら pointer 一致で終わる)。`texts` 側は escape のときだけ |
| `NewCallExpression` | `extra` への append 1 回、header の append 1 回、`adopt` × 2、`adoptList` × 2。`growslice` の call は append の数だけ |
| `List` | `extra` への append が要素ループ 1 本。`make` 無し |
| `Finish` | `nodes` / `extra` / `texts` の copy 3 回だけ (`Reset` 後の parse で B/op がこれに近いことを §4 で確認) |

## 4. parse の費用 (ns、allocs、KPC)

```sh
cd $TSC && go test -run '^$' -bench 'StoreParseV1' -benchtime 2s -count 5 $PKG | tee $OUT/parse-ns.txt
cd $TSC && go test -tags kperf -c -o $OUT/storeparser.kperf.test $PKG
# ユーザー (別ターミナル、load average < 1.5 を待ってから)
cd $TSC/internal/storeparser && sudo $OUT/storeparser.kperf.test -test.run '^$' -test.bench 'StoreParseKPCV1' -test.benchtime 20x -test.count 5 | tee $OUT/kpc-$(date +%Y%m%d-%H%M).txt
```

基準値 (Pointer、checker.ts、GC オフの Session、`internal/ast/docs/_kpc-baselines/kpc-after.txt`。3 run):

| | Pointer | per node (298,054) |
| --- | ---: | ---: |
| inst/op | 262.5M | 881 |
| cycles/op | 69.3M | 233 |
| IPC | 3.77 | |
| B/op | 26.14 MB | 87.7 |
| allocs/op | 11,935 | 0.040 |

今回は同じ binary で `pointer` と `store` を続けて取るので、この表は照合用 (pointer が ±3% で再現すること)。判定は今回の `pointer` に対する比で行う。

判定 (**閾値は提案。ユーザーが決める**):

| 指標 | 提案する合格線 | 根拠 |
| --- | --- | --- |
| cycles/op (KPC) | store / pointer ≤ **1.00** | 走査 (scan) は同じ scanner で、構築は arena の alloc → slice の append に変わるだけ。parent 書きと text の compare が増え、`newNode` / arena / `NodeList` alloc / `overrideParentInImmediateChildren` の `ForEachChild` が消える。速くならない理由が命令列で説明できなければ移植の問題 |
| inst/op (KPC) | 報告のみ。増えたら命令列で内訳 | |
| B/op | store / pointer ≤ **0.5** | 理論値は `Finish` の copy = header 7.15 MB + extra 2.67 MB + texts 11 KB ≈ 9.8 MB (0.38)。scratch は 2 回目以降 alloc しない |
| allocs/op | ≤ **200** | `Finish` 3 + `SourceFile` + diagnostics + pragmas + scanner 内部。Pointer の 11,935 は arena の chunk と `NodeList`、`nodeSliceArena` の clone |
| ns/op (GC あり) | 報告のみ (cycles と allocs で説明する) | |

pointer の inst の幅が 1.5% を超えたら測り直す (7a と同じ)。fixture の TS で Pointer が `@see` / `@link` により eager JSDoc parse をしている数を数え (`grep -c '@see\|@link' testdata/fixtures/compiler/checker.ts` と、Pointer parse 後の `JSDocCache` の長さ)、Pointer 側の余分な仕事として報告に書く。

## 5. retained heap と GC (設計の賭けの実測)

```sh
cd $TSC && go test -run '^$' -bench 'StoreParseRetainedV1' -benchtime 1x -count 5 $PKG | tee $OUT/retained.txt
cd $TSC && GODEBUG=gctrace=1 go test -run '^$' -bench 'StoreParseRetainedV1/store' -benchtime 1x -count 1 $PKG 2> $OUT/gctrace-store.txt
cd $TSC && GODEBUG=gctrace=1 go test -run '^$' -bench 'StoreParseRetainedV1/pointer' -benchtime 1x -count 1 $PKG 2> $OUT/gctrace-pointer.txt
```

| 指標 | 提案する合格線 | 根拠 |
| --- | --- | --- |
| retained B/node | store ≤ **40** (Pointer は約 85〜88 の見込み。7a の footprint 32.95 / 34.18 + `SourceFile` / diagnostics / src) | 設計文書 §1 の footprint 39〜41% |
| 保持した状態の `runtime.GC()` 1 回 | store / pointer ≤ **0.25** | `nodes` と `extra` は noscan。残る scan 対象は `SourceFile`、diagnostics、src の string header。0.25 を超えるなら何が scan されているかを `gctrace` の mark 量と heap profile (`-memprofile`) で説明する |
| gctrace の `MB live` | 報告 | |

retained のベンチは `-count 5` の幅が大きければ、`runtime.GC()` を 3 回呼んで最小を取る形に読み替える (ベンチ側は変えない。報告に書く)。

## 6. 診断と SourceFile の field

`store.SourceFile` の field を Pointer と突き合わせる (テストがあれば読む、無ければ一時テストで): `IsDeclarationFile`、`ScriptKind`、`LanguageVariant`、`IdentifierCount`、`CommentDirectives`、`Pragmas`、`ReferencedFiles` / `TypeReferenceDirectives` / `LibReferenceDirectives`、`CheckJsDirective`、root の `Flags` (`sourceFlags` を含む)。corpus 全体で一致件数と不一致の label を報告する。`IdentifierCount` は Pointer と同じ数え方 (speculation 分を含む) になるはずで、違えば理由を書く。

## 7. レポート

`tsc/internal/ast/docs/store-parser-verification-report-<YYYYMMDD>.md` に、既存の文書と同じ文体で:

1. 結論 (合否と数字。§4 / §5 の表を埋めたもの。閾値が未決なら「提案線に対して」と書く)
2. 環境と入力
3. 正しさ (§1。等価の範囲: file 数、node 数、JS の除外規則、診断の差分の内訳、dead node)
4. 移植の機械性 (§2。diff の分類と (f) の一覧)
5. Builder と constructor (§3)
6. parse の費用 (§4。inst の内訳が要るなら命令列で)
7. retained heap と GC (§5)
8. 設計文書への反映案 (あれば。特に §2.7 と §5 の「線形走査の活用」「死にノードの切り詰め」の実測値)
9. 限界
10. 再現手順と生データの場所

合格なら次は TODO 7c (binder)。不合格でも、§5 の数字 (retained と GC) は設計の賭けの最初の実測なので、合否に関わらずレポートに残す。
