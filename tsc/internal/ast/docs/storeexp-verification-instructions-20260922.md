# storeexp 検証指示

作成日: 2026-09-22。対象は検証を担当するエージェント。何を作ったかは [storeexp-implementation-instructions-20260922.md](storeexp-implementation-instructions-20260922.md) (以下「実装指示書」)、何を確かめたいかは [store-ast-design-20260922.md](store-ast-design-20260922.md) §4 (以下「設計文書」)。

目的は、設計文書の layout と accessor の形が Pointer AST に対してどこで勝ち、どこで負けるかを、**命令列で理由を説明できる形で**報告することである。ゲートを通すことは目的ではない。

## 0. 規則

1. **実装を直して数字を良くしない。** accessor の特殊化、`unsafe` の追加、ベンチ側の書き換えをしない。問題を見つけたら、命令列と一緒に報告に書き、修正案は提案に留める。直してよいのは正しさのバグ (テストが落ちる、Pointer と合計が合わない) だけで、直した内容は報告に書く。
2. **測った事実と見積りを分けて書く。** 見積りには式と前提を付ける。
3. 失敗、測れなかったもの、skip したものを省かない。
4. 生の出力は `tsc/internal/ast/docs/_storeexp-results/` に保存する (`_` 始まりなので Go のビルド対象にならない)。
5. コマンドは絶対パスで書く。以下 `TSC=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc`、`PKG=./internal/ast/storeexp/`、`OUT=$TSC/internal/ast/docs/_storeexp-results`。
6. 環境を記録する: `go version`、マシン (Apple M1 を想定)、commit、電源接続、他の重い process が無いこと。

### KPC (inst / cycles) はユーザーが実行する

KPC は root が要り、エージェントの shell からは sudo できない。エージェントは test binary を作り、ユーザーに別ターミナルで次を実行してもらい、保存されたファイルを読む。

```sh
# エージェント
cd $TSC && go test -tags 'storeexp kperf' -c -o $OUT/storeexp.kperf.test $PKG
# ユーザー (別ターミナル)
cd $TSC/internal/ast/storeexp && sudo $OUT/storeexp.kperf.test -test.run '^$' \
  -test.bench 'StoreExp(Walk|Access)KPC' -test.benchtime 20x -test.count 5 | tee $OUT/kpc-$(date +%Y%m%d-%H%M).txt
```

KPC の結果が無い間も、§1〜§3 と ns ベースの §4〜§5 は進められる。判定は inst で行い、ns は参考にする。

## 1. 正しさ (前提)

```sh
cd $TSC && go run internal/ast/storeexp/_gen/main.go && git status --short internal/ast/storeexp   # 生成物に差分が無い
cd $TSC && go test -tags storeexp -count=1 $PKG
cd $TSC && go test -tags 'storeexp storechecks' -count=1 $PKG
cd $TSC && go build ./... && git status --short | grep -v 'internal/ast/storeexp\|internal/ast/docs'   # 出力が無いこと
cd $TSC/internal/ast/storeexp && grep -L 'go:build' *.go                                              # doc.go だけ
```

どれかが満たされなければ、先へ進まず報告する。実装指示書 §1 のガード 5 項目も目で確認し、欠けていれば報告する。

## 2. E2 / E4: inline と bounds check

```sh
cd $TSC && go build -tags storeexp -gcflags='-m=2' $PKG 2>&1 | grep -E 'can inline|cannot inline' > $OUT/inline.txt
cd $TSC && go build -tags storeexp -gcflags='-d=ssa/check_bce/debug=1' $PKG 2> $OUT/bce.txt
```

確認して表にする:

| 対象 | 期待 |
| --- | --- |
| `node`、header のメソッド、`AsXxx`、代表 kind の accessor、多相 accessor、`List` のメソッド、`Text` | 全部 `can inline`。`cannot inline` なら理由 (cost の数字) を写す |
| `node()` の bounds check | 1 |
| slot 読み (`extra[h.data+k]`) | 1 |
| 多相表の読み `nameSlot[kind&511]` (E4) | 0。出ていれば、表の長さと mask の書き方を報告する |
| `List.Refs()` | 2 以下 |
| `Text()` | 2 以下 (slice 式 1 件は分岐 2 つ) |

`ForEachChild` の kind 別関数は inline されなくてよい (Pointer 版も同じ)。

## 3. E2 / E5 / E6: 命令列

```sh
cd $TSC && go test -tags storeexp -c -o $OUT/storeexp.test $PKG
cd $TSC && go tool objdump -s 'storeexp\.asm' $OUT/storeexp.test > $OUT/asm-ops.txt
cd $TSC && go tool objdump -s 'storeexp\.Node\.ForEachChild$' $OUT/storeexp.test > $OUT/asm-foreach-store.txt
cd $TSC && go tool objdump -s 'ast\.\(\*Node\)\.ForEachChild$' $OUT/storeexp.test > $OUT/asm-foreach-pointer.txt
```

1. **単価表。** `asm_test.go` の `asmXxxPointer` / `asmXxxStore` の各組について、hot path (panic 側と関数の prologue / epilogue を除く) の命令数、load 数、条件分岐数、間接分岐数を数える。数え方に迷った箇所は命令列を引用する。budget 文書 §4 の表の「目安」を実数で置き換えられる形にする。
2. **E5。** Store の `ForEachChild` の dispatch がジャンプテーブル (範囲検査 + 表の load + `BR`) か、比較の連鎖 (二分探索) かを判定する。Pointer 版も同じ基準で判定する。比較の連鎖なら深さを数える。
3. **E6。** `Node.Ref()` の命令数と、除算が magic 乗算になっているかを確認する。
4. **E11 の下調べ。** `asmParentChainStore` の 1 段あたりの命令数と、Pointer 版との差を数える。`s.nodes` の base / len の load がループの外に出ているかを見る。

## 4. E1: Walk

```sh
cd $TSC && go test -tags storeexp -run '^$' -bench 'StoreExpWalkV1' -benchtime 2s -count 5 $PKG | tee $OUT/walk-ns.txt
```

KPC は §0 の手順 (`BenchmarkStoreExpWalkKPCV1/{pointer,switch,shape}`)。

- 各条件 5 run の中央値と幅 (max − min) / 中央値を出す。
- **ノイズの確認**: `pointer` の幅が inst で 1.5% を超えたら、測定をやり直すか、その旨を書いて判定を保留する。同じ process 内の `pointer` が既知の基準値 (70.3 inst/visit、IPC 2.65) から 3% 以上ずれていたら、ベンチの形が `BenchmarkASTWalkKPCV1` と違っていないか確認する。
- **判定**: `switch` の inst/visit が同じ run の `pointer` の 1.10 倍以下なら合格。budget 文書の予測は +8〜10% である。予測から外れた場合 (良い側でも) は §3 の命令列で理由を説明する。
- `shape` は参考。`switch` との差を inst/visit と分岐予測ミス (取れるなら) で報告する。
- cycles/visit と IPC も同じ表に載せる。inst が増えて cycles が増えない、またはその逆なら、そう書く。

## 5. E3 / E11: 役割アクセス

```sh
cd $TSC && go test -tags storeexp -run '^$' -bench 'StoreExpAccess$' -benchtime 1s -count 5 $PKG | tee $OUT/access-ns.txt
```

KPC は §0 の手順 (`BenchmarkStoreExpAccessKPC`)。

1回あたりの単価は、固定費を引いて出す:

```
inst/access(op, side) = ( inst/op(op, side) − inst/op(baseline, side) × ops(op) / ops(baseline) ) / ops(op)
```

`baseline` は slice を読むだけのループで、`[]Node` (16B) と `[]*ast.Node` (8B) の差を含む。ops が違う操作 (絞った slice) には比例配分で引く。引いた結果が負か 1 未満になったら、`baseline` の引き方が合っていないので、引く前の値も併記する。

報告する表:

| 操作 | ops | Pointer inst/access | Store inst/access | 差 | §3 の命令数との整合 |
| --- | ---: | ---: | ---: | ---: | --- |
| header (4 field) | | | | | |
| known | | | | | |
| polyPresent | | | | | |
| polyAll | | | | | |
| list (要素あたりも) | | | | | |
| text | | | | | |
| modflags | | | | | |
| parentChain (段あたりも) | | | | | |
| refRoundTrip | | — | | | |

- **判定**: 操作ごとに Store ≤ Pointer なら合格。超えた操作は、§3 の命令列のどの命令が差を作っているかを特定する (bounds check、添字計算、表引き、番兵の比較、`Node` の 2 word 渡し、など)。
- **Check への影響の見積り** (見積りと明記する)。[accessor-dynamic-frequency-20260921.md](accessor-dynamic-frequency-20260921.md) §4 の check の回数を重みにする。
  ```
  Δ inst/node ≈ 165 × Δheader/4      (Kind + Loc + Flags + id の読み。Parent は下で別に数える)
              + 49  × Δparent段
              + 40  × Δknown/読んだ子の数
              + 15  × Δpoly            (polyPresent と polyAll を 4 : 6 で混ぜる。Locals の不在率 60% に合わせる)
              + 1   × Δmodflags
              + 1   × Δlist
              + 4.6 × Δtext
  ```
  Check は約 10,070 inst/node。回数はソース上の評価回数で load 数の上限なので、この見積りも上限である。Pointer 側で消える費用 (`AsXxx` の type assert 23.6 回/node、`GetNodeId` 3.6 回/node) は `known` の差に含まれる分以外は数えない。
- **E11 の判定**: `parentChain` の段あたりの差が +4 命令なら Check の約 +2%。差が +2 命令を超えたら、ref で回して `nodes := s.nodes` を局所変数に持つ書き方 (下) を同じベンチに 1 本足して測り、規約にする価値があるかを報告する。これは §0 規則 1 の例外として追加してよい (実装の変更ではなく、書き方の比較)。
  ```go
  nodes := n.s.nodes
  for ref := n.h.parent; ref != 0; ref = nodes[ref].parent { depth++ }
  ```

## 6. E10: footprint

```sh
cd $TSC && go test -tags storeexp -run 'TestStoreExpFootprint' -v -count=1 $PKG | tee $OUT/footprint.txt
```

B/node を Pointer の 84.6 (checker.ts)、82.4 (dom) と並べる。**この値は下限である。** 実験の Store は Identifier 以外の data word (literal の text、TokenFlags、operator など) と、symbol などの予約 slot を持たない。設計文書の試算 33〜35 B/node との差を書く。

## 7. やらないこと

E7 (C 型との比較) は E3 が不合格のときにユーザーが判断する。E8 (foreign 検査)、E9 (Bind ベンチのばらつき)、Bind / Check / Emit の計測はこの検証の範囲外。

## 8. レポート

`tsc/internal/ast/docs/storeexp-verification-report-<YYYYMMDD>.md` に、既存の文書と同じ文体 (である調、表) で書く。

1. 結論 (3〜6 項目。各項目に合否と数字)
2. 環境と入力
3. 正しさ (§1 の結果、実装指示書からの逸脱、override 表の kind)
4. inline / bounds check (§2)
5. 単価表と命令列の要点 (§3)。差を作っている命令を引用する
6. Walk (§4)
7. 役割アクセスと Check への影響の見積り (§5)
8. footprint (§6)
9. 設計文書への反映案: 予測と外れた点、budget 文書 §4 の表に入れる実数、規約にすべき書き方、取り下げるべき決定があればそれ
10. 限界 (入力 1 つ、microbench は cache が温まっている、Pointer から変換した Store は parser が作る配置と `extra` の並びが違いうる、回数は上限)
11. 再現手順と生データの場所

設計文書の決定に反する結果が出た場合、結論の先頭に書く。
