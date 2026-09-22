# Store generator (TODO 7a) 検証指示

作成日: 2026-09-22。対象は検証を担当するエージェント。何を作ったかは [store-generator-implementation-instructions-20260922.md](store-generator-implementation-instructions-20260922.md) (以下「実装指示書」)。比較対象は storeexp の検証結果 [storeexp-verification-report-20260922.md](storeexp-verification-report-20260922.md) (以下「前回レポート」)。

目的は、**生成された Store が手書きの storeexp と同じ命令列・同じ数字になること** (設計文書 §6 の TODO 6「generator の出力に対して E1〜E6 をもう一度」) を確かめることである。新しい設計判断はない。前回と数字が違えば、その理由を命令列で説明する。

## 0. 規則

前回の検証指示 §0 と同じ。実装を直して数字を良くしない。直してよいのは正しさのバグだけ。生の出力は `tsc/internal/ast/docs/_store-generator-results/` に保存する。コマンドは絶対パス (`TSC=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc`、`PKG=./internal/ast/store/`、`OUT=$TSC/internal/ast/docs/_store-generator-results`)。環境 (go version、commit、負荷) を記録する。

**計測前に source の sha256 を取り、終了時に不変を確認する** (前回、実装が並行して書き換わった)。

KPC は root が要るのでユーザーに依頼する:

```sh
cd $TSC && go test -tags kperf -c -o $OUT/store.kperf.test $PKG
# ユーザー (別ターミナル)
cd $TSC/internal/ast/store && sudo $OUT/store.kperf.test -test.run '^$' -test.bench 'StoreWalkKPCV1' -test.benchtime 20x -test.count 5 | tee $OUT/kpc-$(date +%Y%m%d-%H%M).txt
```

## 1. 正しさ

```sh
cd $TSC/.. && node tools/scripts/tsc/generate.ts && git status --short          # 差分なし。ast_generated.go にも差分なし
cd $TSC && go build ./... && go vet ./internal/ast/... && git status --short    # 変更が generate-go-store.ts、generate.ts、internal/ast/store/ だけ
cd $TSC && go test -count=1 ./internal/ast/store/...
cd $TSC && go test -tags storechecks -count=1 ./internal/ast/store/...
cd $TSC && grep -rl '"github.com/microsoft/TypeScript/tsc/internal/ast/store' --include='*.go' . | grep -v 'internal/ast/store/'   # 出力が無いこと (production から import されていない)
cd $TSC && go test -count=1 ./internal/ast/ ./internal/parser/                  # 既存のテストが壊れていない
```

実装指示書 §1 のガードを目で確認する。corpus テストが `-short` 無しで走ったこと、skip されたディレクトリが無いことを確認する。等価テストが比較している member の数 (kind × member) を数えて報告に書く (生成テストの中身を読む)。

## 2. inline と bounds check

```sh
cd $TSC && go build -gcflags='-m=2' $PKG 2>&1 | grep -E 'can inline|cannot inline' > $OUT/inline.txt
cd $TSC && go build -gcflags='-d=ssa/check_bce/debug=1' $PKG 2> $OUT/bce.txt
```

| 対象 | 期待 |
| --- | --- |
| `views_generated.go` の全関数 (typed view、member accessor、`AsXxx`、役割 accessor) | **全部 `can inline`**。`cannot inline` が 1 つでもあれば関数名と cost を列挙する |
| `store.go` の `node`、header メソッド、`List` のメソッド、`Text`、`Ref` | 全部 `can inline` |
| `visitList` | `can inline`、cost ≤ 80 |
| `builder_generated.go` の `NewXxx` | inline されなくてよい |
| `node()` の bounds check | 1 |
| slot 読み | 1、かつ `MOVW` の切り詰めが無いこと (§3 で確認) |
| 役割表の読み | 0 |
| `List.Refs()` | 2 以下 |

## 3. 命令列

```sh
cd $TSC && go test -c -o $OUT/store.test $PKG
cd $TSC && go tool objdump -s 'store\.CallExpression\.Expression$' $OUT/store.test > $OUT/asm-call-expression.txt
cd $TSC && go tool objdump -s 'store\.Node\.Name$' $OUT/store.test > $OUT/asm-name.txt
cd $TSC && go tool objdump -s 'store\.Node\.ForEachChild$' $OUT/store.test > $OUT/asm-foreach.txt
cd $TSC && go tool objdump -s 'store\.forEachChildBlock$' $OUT/store.test > $OUT/asm-foreach-block.txt
cd $TSC && go tool objdump -s 'store\.Node\.Text$' $OUT/store.test > $OUT/asm-text.txt
```

inline される関数は単体の symbol が出ないことがある。その場合は `bench_test.go` の walker か、テストから呼ばれている場所を objdump し、inline 後の命令列を読む。

1. **子の読み** (`CallExpression.Expression` 相当): 前回レポート §5 の 11 命令から `MOVW` が消えて 10 になっていること。
2. **`ForEachChild` の dispatch**: ジャンプテーブル (`SUB`、`CMP`、`BHI`、`ADRP`、`ADD`、`MOVD (Rn)(Rm<<3)`、`JMP (R27)`)。
3. **list の走査** (`forEachChildBlock`): slot 読みの直後に `CBZW` (nil ガード) があり、`visitList` が inline されていること (`CALL .*visitList` が 0)。
4. **役割 accessor** (`Name`): 表引き 5、`0xFF` の比較 1、slot 7、`node()` 8 の形。
5. **`Text`**: Identifier の経路と data word の経路の両方を読み、分岐の数を数える。

## 4. Walk (E1、ゲート 1 の再確認)

```sh
cd $TSC && go test -run '^$' -bench 'StoreWalkV1' -benchtime 2s -count 5 $PKG | tee $OUT/walk-ns.txt
```

KPC は §0。判定は **cycles/visit** (2026-09-22 の決定、設計文書 §1):

| 条件 | 前回 (storeexp、nil ガード後) | 期待 |
| --- | ---: | --- |
| pointer inst/visit | 72.37 | ±0.5% |
| store inst/visit | 92.53 | ±1 命令。外れたら §3 の命令列で理由を説明 |
| pointer cycles/visit | 25.67 | |
| store cycles/visit | 29.20 | **≤ 29.5 (1.15 倍) で合格** |

pointer の inst の幅が 1.5% を超えたら測り直す。

## 5. footprint (E10)

```sh
cd $TSC && go test -run 'TestStoreFootprint' -v -count=1 $PKG | tee $OUT/footprint.txt
```

前回は 32.62 (checker.ts) / 33.71 (dom) B/node で、data word を持たない下限だった。今回は literal の text (2 word)、bool、TokenFlags、operator が入る。増分を `extra` の word 数から kind 別に説明する (どの kind の何 word か。生成 shape から数えられる)。設計文書の試算 33〜35 B/node と比べる。

## 6. レポート

`tsc/internal/ast/docs/store-generator-verification-report-<YYYYMMDD>.md` に、既存の文書と同じ文体で:

1. 結論 (合否と数字。前回との差)
2. 環境と入力
3. 正しさ (§1。等価テストの範囲: kind 数、member 数、corpus のファイル数。実装指示書からの逸脱、override 表)
4. inline / bounds check (§2)
5. 命令列 (§3。前回との差があれば引用)
6. Walk (§4)
7. footprint (§5)
8. 設計文書への反映案 (あれば)
9. 限界
10. 再現手順と生データの場所

合格なら、次はユーザーが `storeexp` を削除し、TODO 7b (parser) に進む。
