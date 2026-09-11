# 頻出 PropertyAccessExpression の親構文一括取得（2026-09-09〜10）

PropertyAccessExpression の一括取得を実装した。局所ベンチは10〜16%短縮したが、実 binder の有意な改善は確認できなかった。候補実装を残し、性能改善としての採用は保留する。

## 問題・選択根拠

Pointer AST に対する Store binder の退行について、親の header / child slot を個別に解決するコストを検証する。既存の audit と IfStatement A/B を先に読み、生データ・identity・実装を確認した。IfStatement の非有意な結果を一括取得設計全体の反証とは扱わない。

選択 repo は `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、`typescript_go_git_rev=32598cba146fa4dd7b6162b838630c90d865ab28`、`tsgolint_git_rev=null`。候補 clones は pointer `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript` (`8ac035a394c79e693a3a7d74cb170448503ee894`)、flownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-*、store-redesign。全パスと SHA は artifact の `worktrees.txt` に保存した。他 checkout は変更していない。

計測とは別のバイナリで `bindKind` の訪問を集計した（静的なソース中の出現数ではない）。

| kind | checker.ts | dom.generated.d.ts |
| --- | ---: | ---: |
| Identifier | 123,554 | 36,271 |
| PropertyAccessExpression | 24,937 | 50 |
| CallExpression | 18,466 | 0 |
| BinaryExpression | 16,527 | 0 |
| TypeReference | 8,512 | 13,486 |
| Parameter | 5,788 | 8,446 |
| PropertySignature | 99 | 8,313 |
| IfStatement | 6,362 | 0 |

**PropertyAccessExpression を選択した。** checker.ts の Identifier 以外で最多であり、3つの named child を持ち、IfStatement より約3.9倍多い。TypeReference は named child 1つと list の組み合わせであり、今回の複数 named child 解決の最小実験には選ばない。Identifier 自体にはこの形式の複数 named child がない。

識別子判定 `isIdentifierNameRef` の親 kind も集計し、checker.ts の PropertyAccessExpression は46,304回で最多だった。これは今回変更した経路の実行回数とは異なる。親 header / name 判定への適用は別の介入として残し、今回と混ぜない。dom は ambient による早期 return があり、この識別子判定の呼び出しがない。

最も広い既存 hot path は walk + helpers。今回の対象はその内部の、通常 property access の named-child 解決という狭い経路であり、両者は一致しない。symbol synthetic は使っていない。

## 仮説・実装

`Store.PropertyAccessExpressionRefs` が header と3 slot の範囲を一度解決し、expression / question-dot token / name を scalar の NodeRef で返す。`bindAccessExpressionFlowRef` の非 optional な PropertyAccessExpression に適用した。従来の generated walker と同じ順で3つの `bindChildRef` を呼ぶ。optional chain と element access は既存の経路を使う。

対象は checker.ts 24,606回、dom 50回。dom は対象がゼロの厳密な negative control ではないが、対象密度が非常に低い入力である。

same-Store で kind が既知の呼び出しに限定する。欠損・foreign child は `ChildRef` と同じゼロ参照で、foreign Handle 解決を追加しない。kind 判定は呼び出し元の責任、3 slot の schema 条件は API が確認する。返すのは整数のみで backing array を借用せず、Store の成長をまたいで保持できる。一方、取得後の構文編集は反映しない。AST header、GC scan 対象、Flow / Symbol の表現は変更しない。

これは性能実験用の候補実装としてワークツリーに残す。性能改善としての採用判断と、候補を実装・保存したことを区別する。

## 正しさ

AST / binder パッケージの全テストが通過。API の child 値・順序・欠損 token・nil/zero 親・Store 成長・不正 schema を検証した。

別の instrumented binary で checker.ts、dom.generated.d.ts、branches.ts、missing.ts、properties.ts、properties-missing.ts の6入力を変更前後で比較した。全入力の構文 slot / parent、走査順、node side table、symbol graph、CFG graph、診断のハッシュが一致した。追加の property 入力は nested access、optional chain、element access、reserved property name、代入、increment、delete、private identifier、不完全構文を含む。全 conformance suite の検証ではない。

## 計測方法・証拠

Artifact set: `.cursor/skills/verify-tsc/artifacts/20260909-parent-frequent/`。

Go 1.26.0 / darwin arm64 / Apple M1 / CGO_ENABLED=1。GOGC=100、GOMAXPROCS=8、GOMEMLIMIT=off。baseline は変更前の binder.go を Go overlay で復元し、candidate と同じ test code / Store API を使用。計測バイナリに audit hook や variant switch は入れない（micro の reader 選択だけは timer 外）。ビルド・テストを終えてから、micro、BindHot の順で測定した。

Micro は64親／65,536親の2サイズで、一回に同じ3 slot を同じ順序で読み、同じ checksum を計算する。半数は question-dot token が欠損し、半数は存在する。これは accessor の実験であり、実 binder の optional chain 処理をモデル化していない。200ms × 12 rounds、順番を交互に反転。

BindHot は同一 fixture の parse と強制 GC を timer 外に置き、30x × 20 rounds、baseline/candidate の順番を交互に反転。生出力を保存し、判定には `benchstat old.txt new.txt` を使う。過去の campaign との手動比較はしない。pointer の新規測定はしていないため、今回の候補と pointer の直接比較はできない。

## 結果

以下は raw の中央値。差の判定は benchstat による。

| micro | baseline ns/op | candidate ns/op | benchstat | B/op / allocs/op（両版） |
| --- | ---: | ---: | --- | --- |
| small | 10.215 | 9.150 | −10.43%, p=0.002, n=12 | 0 / 0 |
| large | 11.420 | 9.615 | −15.81%, p=0.001, n=12 | 0 / 0 |

| BindHot | variant | ns/op | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| checker.ts | baseline | 21,063,546 | 12,799,472 | 14,165 |
| checker.ts | candidate | 20,807,091 | 12,799,480.5 | 14,165 |
| dom.generated.d.ts | baseline | 10,479,847.5 | 7,867,938.5 | 16,684 |
| dom.generated.d.ts | candidate | 10,590,570 | 7,866,733.5 | 16,684 |

**BindHot は有意差なし**（checker p=0.529、dom p=0.989、各 n=20）。B/op も checker p=0.406、dom p=0.239、allocs/op は両入力 p=1.000 で有意差なし。中央値に .5 が出るのは20標本の中央2点を平均したため。

測定時間のばらつきは大きい（benchstat の区間表示は checker ±18〜19%、dom ±31〜34%）。小さな改善・退行を排除できる精度ではなく、非有意差を効果ゼロや厳密な同等性の証明としない。過去の時間中央値との差から今回の効果量を算出しない。

逆アセンブルでは micro の命令行が124→68、bounds panic の静的 call site が6→2。scalar API は inline 展開され、各 ChildRef で繰り返していた header / slot 解決が一つにまとまっている。実 binder でも対象経路は generated walker を経由せず3つの bindChildRef を呼ぶ。一方 `bindAccessExpressionFlowRef` 単体は分岐と処理の取り込みにより48→96命令行へ増える。元の処理は別関数内にあるため、この関数サイズだけで命令数の増減を評価しない。静的命令行数は inst/op ではない。

## Artifact 状態

`current` は repo_root / tsgolint_git_rev / typescript_go_git_rev の一致が条件。baseline と candidate の dirty 差は保存した binder ソース、API ソース、patch、overlay で識別し、各 identity に binary hash、Go/CGO、fixture hash を保存した。候補の AST test の import 整理・変数名変更は計測コードの振る舞いを変えない。

| artifact / stage | 状態 | 範囲 |
| --- | --- | --- |
| 今回の micro / BindHot | `current` | identity が選択 checkout と一致。raw と benchstat あり |
| 今回の frequency / correctness audit | `current` | 同一 checkout、6入力、別 instrumented binary |
| 既存 parent-syntax-bind | `current` | identity の3項目は同じ。保存された IfStatement 介入の結果としてのみ使用 |
| 既存 eval-8ac035a / identity 不足の Instruments | `stale` | 以前の広い hot path の背景情報。今回の候補の性能には使わない |
| 今回の artifact の BindKPC / EL0 stage | `unsupported` | bench.txt に対象 Benchmark 行がない |
| 今回の inst/op / cycles/op / cache miss capture | `missing` | micro の静的命令数で代替しない |
| 今回の Parse+Bind / live heap / scan / 累積 GC CPU | `missing` | binder の B/op で代替しない |
| 今回の candidate と pointer の paired 比較 | `missing` | pointer 同等性能の到達判断はできない |

BindHot の採用根拠が得られなかったため、今回の初期検証では KPC / Parse+Bind / GC 測定へ進めていない。既存の IfStatement KPC を今回の変更の証拠として流用しない。

## 診断・次のアクション・採用条件

親構文をまとめることで局所的な accessor コストは減る。しかし IfStatement より頻出する PropertyAccessExpression の通常走査に適用しても、実 binder の改善は確認できなかった。今回の候補は**実装済み・性能改善として未採用**であり、ワークツリーに実験候補として残す。これだけで pointer 比退行や noscan とアクセス高速化の両立を解決したとはいえない。

大きな allocation driver は既存の symbolIdx / flows 列、symbolRefs、48 B の FlowNode、Handle を含む Symbol / Declarations。今回の変更はこれらを変えない。整数を返す API の allocation がゼロでも、binder グラフ全体の noscan 化や GC CPU の改善を証明しない。

次は今回46,304回と分かった識別子判定の PropertyAccessExpression 親について、親 header と name slot の同時取得を独立に検証する。keyword 判定や診断条件を省略せず、現候補を overlay で戻した状態を baseline にして、介入を混ぜない。今回の結果から全 kind に一括取得を広げる根拠はない。

採用条件は実 BindHot の有意な短縮、inst/op / cycles/op の低下、parse+bind と live / scan 指標の非悪化、診断・symbol・CFG・走査順の一致。pointer と同等以上、メモリアクセスと GC の両方の高速化はそれぞれ別途確認する。

## 再現

artifact 内の `baseline-overlay.json` で変更前 binder を復元してビルドできる。`baseline-binder.go` / `candidate-binder.go`、`store_property_syntax.go`、`store_property_syntax_test.go` に介入を保存した。`run_micro.py` は12 rounds、`run_wall.py` は20 roundsの交互計測。既存 raw への混入を防ぐため、出力ファイルが存在すると失敗する。再測定は新しい artifact ディレクトリで、同じ名前の新規ビルドバイナリとスクリプトを配置し identity も新規保存する。

```sh
benchstat .cursor/skills/verify-tsc/artifacts/20260909-parent-frequent/micro-baseline.txt \
          .cursor/skills/verify-tsc/artifacts/20260909-parent-frequent/micro-scalars.txt
benchstat .cursor/skills/verify-tsc/artifacts/20260909-parent-frequent/wall-baseline.txt \
          .cursor/skills/verify-tsc/artifacts/20260909-parent-frequent/wall-candidate.txt
cd tsc
go test ./internal/ast ./internal/binder
```

計測は9月9日から10日にまたがった。artifact とファイル名は開始日のまま保存している。
