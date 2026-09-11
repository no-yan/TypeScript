# IfStatement 親構文一括取得の実 binder A/B（2026-09-09）

**IfStatement 一箇所への適用では、BindHot の有意な改善を確認できなかった。** micro の約13%短縮を binder 全体へ外挿できない。以下の実験結果を記録する。

| 入力 | variant | ns/op（中央値） | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| checker.ts | Store baseline | 16,697,058 | 12,799,476 | 14,165 |
| checker.ts | Store candidate | 16,968,749 | 12,799,476 | 14,165 |
| checker.ts | pointer | 11,666,151 | 7,425,296 | 13,954 |
| dom.generated.d.ts | Store baseline | 6,611,860 | 7,867,939 | 16,684 |
| dom.generated.d.ts | Store candidate | 6,839,345 | 7,865,516 | 16,684 |
| dom.generated.d.ts | pointer | 4,549,254 | 5,287,312 | 16,562 |

Store baseline → candidate は時間に有意差なし（checker p=0.925、dom p=0.620、n=20）。中央値が増えていても有意な退行と判定しない。candidate は pointer に対して checker +45.45%、dom +50.34%（どちらも p=0.000、n=20）で、同等性能の目標は未達。

dom の B/op には −0.03%（p=0.018）が出たが、対象 if 訪問はゼロで allocs/op は不変。この微小差を今回の一括取得のメモリ効果として採用しない。checker の B/op / allocs/op は不変。

KPC（fixed EL0+EL1、n=6）：

| 入力 | 指標 | baseline | candidate | benchstat |
| --- | --- | ---: | ---: | --- |
| checker.ts | inst/op | 188,381,305 | 188,083,370 | 有意差なし、p=0.240 |
| checker.ts | cycles/op | 89,231,199 | 89,398,607 | 有意差なし、p=0.818 |
| dom.generated.d.ts | inst/op | 78,676,181 | 78,798,368 | 有意差なし、p=0.589 |
| dom.generated.d.ts | cycles/op | 33,362,014 | 33,350,907 | 有意差なし、p=0.589 |

checker の命令数点推定はわずかに減ったが、減少が確認できたとは扱わない。KPC の ns/op と通常の BindHot は、GC設定・root実行・OSスレッド固定・計測処理が異なるため直接比較しない。sanity の同一baseline 2回の命令数差は checker 0.097%、dom 0.470% で1%未満。

Parse+Bind（n=8）：

| 入力 | baseline ms/op | candidate ms/op | p |
| --- | ---: | ---: | ---: |
| checker.ts | 40.78 | 41.01 | 0.798 |
| dom.generated.d.ts | 17.14 | 17.18 | 0.721 |

| 入力 | 指標 | baseline | candidate | p |
| --- | --- | ---: | ---: | ---: |
| checker.ts | live heap MiB | 22.95 | 22.95 | 0.967 |
| checker.ts | scan heap MiB | 8.960 | 8.960 | 0.967 |
| dom.generated.d.ts | live heap MiB | 11.87 | 11.88 | 0.738 |
| dom.generated.d.ts | scan heap MiB | 5.342 | 5.348 | 0.716 |

時間・live heap・scan heap に有意差なし。非悪化を厳密な同等性検定で証明したわけではない。Parse+Bind の B/op は baseline/candidate とも変動が大きく、checker 42.29→40.19 MiB/op（p=0.959）、dom 28.24→26.65 MiB/op（p=0.110）で有意差なし。allocs/op は checker 約15.20k→15.20k（p=0.929）、dom 約18.59k→18.58k（p=0.096）。B/op の点推定の減少をこの候補の効果として主張しない。正確な中央値は parsebind-medians.json に保存した。

参考として pointer → candidate では、Parse+Bind 時間は checker +9.38%、dom +14.17%、live heap は checker −27.30%、dom −14.50%、scan heap は checker −71.42%、dom −60.89%（すべて p=0.000、n=8）。この1ファイル parse+bind 後の scannable heap 削減は Store 表現の効果に整合するが、今回の IfStatement 変更の追加効果ではない。checker を実際に動かす workload 全体の GC CPU 改善や累積 scan work の削減率にも読み替えない。

## 問題・仮説・対象

前回の `parent-syntax-micro-20260909.md` では、3つの子 index を scalar の戻り値で取得する micro が約13%短縮した。この結果を実 binder の改善と混同せず、`bindIfStatementRef` 一箇所への適用で検証した。

選択 repo は `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、revision は `32598cba146fa4dd7b6162b838630c90d865ab28`。pointer 比較 repo は `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、`8ac035a394c79e693a3a7d74cb170448503ee894`。他の candidate clones（flownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜5、store-pr-6-perf、store-pr-7、store-pr-7-attach-parent-fix、store-pr-7-nodeseq-t10、store-redesign）は選ばず、全パスと SHA を identity に保存した。

最も広い既存 hot path は walk + helpers で、今回の対象はその中の狭い named-child 解決。`declareSymbolEx` / `GetSymbolTable` を主因とする調査ではなく、symbol synthetic は使っていない。

## 変更案

Store の `IfStatementRefs` が header と3 slot の範囲を一度解決し、expression / then / else の NodeRef を個別の戻り値で返す。子の取得は分岐ラベル3つの生成後、bindConditionN より前にまとめた。CFG 操作と実際の子の bind 順序は維持した。従来の expressionRefGenerated の KindIfStatement 分岐は ChildRef(ref, 0) なので、その処理も同じ slot の取得に置き換わる。

API は型が IfStatement と判明している same-Store の node に限定する。欠損・foreign child のゼロ表現は ChildRef と同じで、foreign Handle 解決は追加しない。戻り値は整数3個で backing array を借用しない。配列成長をまたいで保持できるが、取得後の構文編集は反映されない。今回の4入力では bind 前後の全 child/list slot と親参照が一致した。

本番計測バイナリに訪問順の記録やテスト用の分岐スイッチは入れていない。baseline は保存した元の binder.go を Go overlay で戻して構築し、candidate と同じ追加ベンチを使用。pointer もベンチのみ overlay し、元の checkout は変更しない。

## 正しさ

AST と binder パッケージの全テストが通過。API の欠損 else、3 slot の値、Store 成長、nil/zero 親、不正な slot schema のテストも通過。

別の非計測バイナリで bindKind 入口を記録し、次を変更前後で比較した。

| 入力 | bindKind 訪問 | IfStatement 訪問 | 到達 symbol | 到達 FlowNode | bind diagnostics |
| --- | ---: | ---: | ---: | ---: | ---: |
| checker.ts | 298,054 | 6,362 | 18,444 | 45,694 | 0 |
| dom.generated.d.ts | 109,605 | 0 | 25,112 | 4,841 | 0 |
| branches.ts | 126 | 12 | 10 | 49 | 2 |
| missing.ts | 25 | 2 | 6 | 6 | 2 |

全入力で構文 slot、bind 訪問順、node の flags / side tables、symbol graph、CFG graph、bind 診断の SHA256 が完全一致。symbol table の key を sort し、pointer address の代わりに安定した traversal ID を使った。CFG の antecedent 順序、switch/reduce の synthetic payload も含む。branches.ts は入れ子、else-if、ループ内分岐、try/finally、switch、短絡条件、重複宣言を含み、missing.ts は不完全な構文を含む。これは4入力での一致で、コンパイラ全 conformance suite を通したという主張ではない。

`dom.generated.d.ts` は対象関数を通らないため、性能の negative control でもある。この入力で観測された速度差を3 slot 一括取得そのものの効果と解釈しない。

## 環境・計測方法

Apple M1 / Go 1.26.0 / darwin arm64 / CGO_ENABLED=1。既存の Go 1.26.6 データとの before/after は行わない。baseline/candidate はともに kperf tag、pointer は通常ビルド。pointer 比較は表現全体の参考比較で、最も厳密な介入比較は同じ条件の Store baseline/candidate。

checker.ts の SHA256 は `4fb2f7e7d898a1729a24b9ad2507b697b747bc8c1315f27cd7e72861115a83e9`、dom.generated.d.ts は `056acdb4168b9fde08093fa6917ff5e26cfe067c003c0a4c55d05a8a94d54b1c`。Store と pointer の fixture hash は一致。

- BindHot: GOGC=100、GOMAXPROCS=8、GOMEMLIMIT=off、30x × 20 rounds。3 variant の順番をローテーション。parse と強制 GC は timer 外。
- Parse+Bind: 同じ環境、20x × 8 rounds。3 variant をローテーション。1 op に parse と bind を含む。各 op の直前の強制 GC は timer 外。
- live-B / scan-heap-B: Parse+Bind の時間計測後、入力を1つ保持して GC を2回実行し、入力なしの状態との差を計算。runtime metrics `/gc/heap/live:bytes` と `/gc/scan/heap:bytes`。後者は scannable heap の指標で、累積 scan work や GC CPU 時間ではない。

KPC は GOGC=off、GOMAXPROCS=8、30x × 6 rounds、baseline/candidate を交互に実行。sanity として baseline を先に2回測定。管理者認証後に内蔵ディスク上の保存済みバイナリを実行し、KPERF_TESTDATA で同一内容のコピーを指定。fixed counter は EL0+EL1 を含むため、user 命令だけの値とは扱わない。

ビルド・正しさの検証を終えてから時間計測を開始し、各計測 campaign は同時実行しない。raw を保存し、比較は benchstat old.txt new.txt。baseline 自体の時間帯による変動があるため、異なる campaign の中央値を手で差し引いて効果量を作らない。

## Artifact と状態

保存先: `.cursor/skills/verify-tsc/artifacts/20260909-parent-syntax-bind/`。

| artifact / stage | 状態 | 制約 |
| --- | --- | --- |
| 今回の BindHot（3 variant） | `current` | 20 rounds、同一 fixture hash、各 identity 保存 |
| 今回の BindKPC（Store A/B） | `current` | 6 rounds、root、fixed EL0+EL1 |
| 今回の Parse+Bind と heap/scan 指標 | `current` | 8 rounds、scannable heap と累積 scan work を区別 |
| 今回の正しさ比較 | `current` | 4入力、計測とは別の instrumented binary |
| EL0 限定 benchmark stage | `unsupported` | この artifact の bench.txt に EL0 用 Benchmark 行なし |
| 候補の EL0 load/store・branch・cache miss 計測 | `missing` | fixed の命令数から内訳を推定しない |
| 候補の累積 GC CPU / scan work | `missing` | live/scan-heap 指標による代替証明をしない |

`current` は repo_root / tsgolint_git_rev / typescript_go_git_rev の一致を条件とする。TSGolint は非関与のため null。baseline/candidate の差は記録済み overlay / patch / API ソースで特定し、identity に binary SHA256、Go/CGO、fixture hash を記録。既存 micro は初期根拠、既存 eval-8ac035a / Instruments の性能データは `stale` として扱う。

## 診断・allocation driver・採用条件

実 binder で有意な時間短縮がなく、inst/op / cycles/op の低下も確認できないため、**この一箇所の変更は不採用**。介入の binder 差分と API・テストを artifact に保存し、本番 binder と Store API は変更前に戻した。既存のユーザー差分は維持した。前回の microbenchmark と今回の報告は残す。

逆アセンブルでは bindIfStatementRef の expressionRefGenerated CALL は1→0、bounds panic の静的 call site は7→5。関数の命令行は264→228で、局所的なコード削減は実現した。ただし行数は実行時 inst/op ではなく、panic/stack-growth などの経路も含む。6,362回の対象訪問があることだけで、全体の約45%のpointer比退行を説明できるとはいえない。

診断は「親の重複解決は存在するが、IfStatement 一箇所の一括取得は実 workload の有効な改善策として支持されない」。有意差なしは効果ゼロの証明ではなく、一括取得設計全般の棄却でもない。

次のアクションは、識別子判定の親 header / name slot のように訪問頻度の高い経路で、同一headerを取り直す箇所とその頻度を実 workload から定量化すること。その上で同じ判定を維持した一括解決の最小実験を選ぶ。今回の非有意な結果を根拠に一括取得を全kindへ広げない。

既存の大きな allocation driver は symbolIdx / flows の列、symbolRefs、FlowNode の32→48 B化、Symbol の Handle / Declarations の拡大。今回の変更はこれらを変えず、AST header や GC 対象グラフのレイアウトも変えない。局所的に整数だけを返せても、AST と binder グラフ全体の noscan 化を実現したことにはならない。

採用条件は、BindHot の有意な改善、inst/op と cycles/op の低下、parse+bind と live heap / scan 指標の非悪化、診断・symbol・CFG・走査順の一致。未改変 pointer と同等以上という最終目標は、部分改善とは別に判定する。

## 再現

計測に使った生データ、`run_wall.py`、`run_parsebind.py`、`kpc-driver.sh`、overlay、非計測の `audit_test.go` と出力を artifact に保存した。スクリプトは既存 raw を混ぜないよう、新規出力先で実行する。baseline/candidate の `*-bind-if.asm` はそれぞれの計測バイナリから抽出した。`candidate.patch` と `store_if_syntax.go` で介入を再構成できる。

本番変更を戻した後の再現には、`candidate-perf-replay-overlay.json` と `candidate-audit-replay-overlay.json` を使える。これらは保存済み candidate-binder.go と Store API を仮想的に追加し、現 checkout を編集せずに候補を再構築する。元の build 時の overlay も保存してある。
