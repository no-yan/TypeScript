# isExportOrExportExpression の親再取得除去: 効果検証

2026-09-17。[vscode-store-pointer-b719-20260916.md](vscode-store-pointer-b719-20260916.md) の「次の検証」1 (親取得の局所重複除去) を実施した記録。対象 HEAD は `e22804520d` (branch `binder-rewrite`)、変更は `internal/checker/checker.go` の `isExportOrExportExpression` 1 関数のみ。

## 結論

- **移植逸脱の復元として採用する。** Pointer 版は `parent := n.Parent` が field 読み 1 回だが、Store 版は同じ字面が method value になり、祖先 1 段ごとに `Handle.Parent()` を 1〜4 回余分に呼んでいた。修正後は Pointer 版と同じく 1 回。診断出力・テストは不変。
- **効果は実測で Monaco 1 run あたり約 6.6 ms、Check 時間の約 0.3%。** 決定的カウントで Parent 呼び出し −1,733,810 回/run (全 Parent 呼び出しの 3.7%)、プロセス内タイマーで関数の総時間 24.3 → 17.7 ms/run、マイクロベンチで −35% (8.7 → 5.6 ns/祖先訪問)。3 経路の値は互いに整合する。
- **end-to-end の wall は解像できないので主張しない。** 見込み 0.3% は run 間ノイズより小さい。採否根拠は上の決定的な仕事量削減と、元 doc の採用基準 (移植で逸れた箇所の復元、`binder/docs/caller-known-refetch-inventory-20260916.md`) である。
- **副次的な発見: darwin の Go pprof はこの関数を約 10 倍過小に帰属した。** 1000 Hz でも inclusive は 2.2 ms/run (タイマー実測 21 ms/run)、`Handle.Parent` の flat は 46.5M 呼び出しに対し 8.6 ms/run (0.19 ns/回) で物理的にあり得ない。inline される小さな leaf の占有率を pprof で測るのは避け、カウント + タイマー overlay を使う。

## 変更

```go
// before (Store 版、移植時の機械変換)
parent := n.Parent          // method value。parent != nil は常に真
if parent != nil {
    if ast.IsAnyExportAssignment(parent()) { ... parent().Expression() ... }
    if ast.IsExportSpecifier(parent())     { ... parent().ExportSpecifierName() ... parent().PropertyName() ... }
}
// after (Pointer 版と同形)
parent := n.Parent()
if !parent.IsNil() { ... parent ... }
```

`Handle.Parent()` は parent ref → `handleOf` → 親 header の kind 読みを伴う (`store.go:1246`)。旧コードは祖先 1 段につき FindAncestor の 1 回に加えて closure 内で通常 2 回 (export 位置なら 3〜4 回) 呼んでいた。nil Handle に対する `IsAnyExportAssignment` は false なので、`IsNil` ガードを入れても結果は同じ。

到達条件は `!isolatedModules && preserveConstEnums` (呼び出し元 `markPropertyAliasReferenced` / `markAliasReferenced`)。Monaco と VS Code full の tsconfig は両方これを満たす。callback は export 位置以外で false を返すため、alias 参照 1 件ごとに根まで登る。

## 計測 1: 決定的カウント (Monaco、実ワークロード)

`go build -overlay` で `Handle.Parent` と `isExportOrExportExpression` にカウンタを足したバイナリ (生成スクリプト: [overlay/gen_counts.py](artifacts/export-expression-parent-refetch-20260917/overlay/gen_counts.py))。`--project src/tsconfig.monaco.json --noEmit --singleThreaded`、各 1 回。

| 項目 | before | after | 差 |
| --- | ---: | ---: | ---: |
| `isExportOrExportExpression` 呼び出し | 163,361 | 163,361 | 0 |
| 祖先訪問 (callback 呼び出し) | 1,733,807 | 1,733,807 | 0 |
| `Handle.Parent` 総呼び出し | 46,472,551 | 44,738,741 | −1,733,810 (−3.73%) |

呼び出し数と訪問数が一致するので走査経路は不変。除去数 = 訪問数 + 3 (export 位置で 3〜4 回呼んでいた分)。平均深さ 10.6 段/呼び出し。raw: [counts/](artifacts/export-expression-parent-refetch-20260917/counts/)。

## 計測 2: プロセス内タイマー (Monaco、実ワークロード)

同じ overlay 手法で関数の入口・出口に `time.Now()` を置き、総和を出力 ([overlay/gen_time.py](artifacts/export-expression-parent-refetch-20260917/overlay/gen_time.py))。`null_pair` は `time.Now()` 2 回の空区間で、計測自体の費用の目安。before/after は交互に 3 run。

| run | before (ms) | after (ms) | null pair (ms) |
| --- | ---: | ---: | ---: |
| 1 | 24.35 | 17.85 | 6.8 / 6.5 |
| 2 | 22.74 | 17.70 | 6.3 / 6.8 |
| 3 | 24.65 | 16.79 | 6.5 / 6.5 |

計測費用は before/after で同一 (呼び出し数が同じ) なので差分に影響しない。**Δ ≈ 6.6 ms/run (5.5〜7.9)**。同条件の `--extendedDiagnostics` は Check 2.107 s、Total 3.35〜3.47 s (各 1 run) なので、Check の約 0.31%、Total の約 0.19%。

## 計測 3: マイクロベンチ (関数単体)

`internal/checker/export_expression_bench_test.go`: checker.ts fixture を parse + bind し、全識別子 (123,554 個、祖先訪問 1,578,597 回 = 平均 12.8 段) に対して呼ぶ。before は `-overlay` で HEAD の checker.go を使い、交互に `-count=10`。

```
                             │ before.txt   │             after.txt               │
IsExportOrExportExpression-8   13.710m ± 7%   8.859m ± 1%  -35.39% (p=0.000 n=10)
```

祖先訪問 1 回あたり 8.68 → 5.61 ns (−3.07 ns)。allocs 0。この単価を Monaco の訪問数に掛けると 1.73M × 3.07 ns = 5.3 ms/run で、タイマー実測 6.6 ms と整合する (実ワークロードの方がキャッシュが冷たい分だけ大きい)。raw: [bench/](artifacts/export-expression-parent-refetch-20260917/bench/)。

## 計測 4: pprof (参考、解像できず)

Monaco `--singleThreaded`、before/after 交互 5 run 併合、`-ignore=runtime.madvise`。

- 100 Hz + `GOGC=off`: madvise が全サンプルの 59%、`isExportOrExportExpression` は 1 サンプルも出ない。この機械 (8 GB) では `GOGC=off` が madvise を膨らませる。
- 1000 Hz (`runtime.SetCPUProfileRate(1000)` を overlay で挿入)、GOGC 既定: `isExportOrExportExpression` cum 11 → 5 ms (5 run 合計)、`FindAncestor` cum 45 → 35 ms、`Handle.Parent` flat 43 → 36 ms。方向は一致するが、絶対値はタイマーの約 1/10。`Handle.Parent` flat 8.6 ms/run ÷ 46.5M 回 = 0.19 ns/回は不可能な値で、inline された leaf への帰属が失われている。

raw: [pprof/](artifacts/export-expression-parent-refetch-20260917/pprof/)。この関数の占有率の根拠には使わない。

## 正確性

- `go test ./internal/checker/ ./internal/ast/` 通過。
- Monaco の診断出力 (stdout) と exit code は before/after で一致 (カウント版・通常版とも)。
- VS Code full (`src/tsconfig.json --noEmit --declaration false --singleThreaded`、カウント版で各 1 run): 診断出力 (TS2724 が 1 件) と exit code 2 が一致。

## VS Code full のカウント

| 項目 | before | after | 差 |
| --- | ---: | ---: | ---: |
| `isExportOrExportExpression` 呼び出し | 1,548,960 | 1,548,960 | 0 |
| 祖先訪問 | 19,790,647 | 19,790,647 | 0 |
| `Handle.Parent` 総呼び出し | 344,980,636 | 325,189,932 | −19,790,704 (−5.74%) |

平均深さ 12.8 段。マイクロベンチの単価 3.07 ns と Monaco のタイマー/マイクロベンチ比 1.25 を掛けると約 61〜76 ms/run の見込みで、元 trace の checker thread 時間 (約 35 s) に対して 0.2% 程度。wall (`real` 34.2 s → 25.8 s) は 8 GB 機の paging (`sys` 11.7 → 7.7 s) と実行順の影響で、この変更には帰属しない。

## 元 doc の見積との対応

元 doc は VS Code full の trace で `Parent-fm` 経由の Parent self を全 cycles の 0.271% と読み、「Parent 全体 2.641% を改善幅にしない」と注意していた。今回の Monaco 実測 (Check の 0.31%) はその桁に収まる。次項 (symbolNodeLinks の索引、accessor 税) の方が大きいことは変わらない。今回確立した「overlay カウント + プロセス内タイマー」は、pprof が信用できない小さな経路の before/after に再利用できる。

## 再現

```sh
S=<scratchpad>
# before の checker.go を固定
git show HEAD:tsc/internal/checker/checker.go > $S/checker.before.go
# カウント / タイマー overlay を生成してビルド (noembed は lib*.d.ts 同居)
python3 artifacts/export-expression-parent-refetch-20260917/overlay/gen_counts.py $S/checker.before.go $S/before
CGO_ENABLED=0 go build -trimpath -tags=noembed -overlay $S/before/overlay.json -o $S/bin/before/tsc ./cmd/tsc
cp built/local/lib*.d.ts $S/bin/before/
TSGO_EXPORT_EXPR_COUNTS=1 $S/bin/before/tsc --project <vscode>/src/tsconfig.monaco.json --noEmit --singleThreaded
# マイクロベンチ (before は overlay で HEAD の checker.go)
go test ./internal/checker/ -run '^$' -bench IsExportOrExportExpression -count=10 -benchmem
```
