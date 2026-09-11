# local owner fast path＋宣言名 memo の組合せ（2026-09-10）

## 問題・仮説・提案修正

個別の探索で、local list owner 関数呼出の削減 A と、構文上の宣言名の一件 memo N は
実 binder の命令数を削減した。単独の wall 改善は採用条件に届かなかったため、
両者の効果を維持できるか4者の対照で検証する。Flags 再利用、ListSlotAt inline、
未コミット PropertyAccess 候補は含めない。採用対象の実入力は測定前に dom と指定。

- A: 同一 Store の list は各 accessor 内で owner を確認し、foreign lookup だけ元の関数を呼ぶ。
- N: 構文上の宣言名と kind を直前一件だけ再利用。missing / assigned-name fallback は memo に入れない。
- AN: 上記2つのみを組み合わせる。binder の判定・診断・走査順を維持する。

## Identity・artifact

- selected repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`
- Store revision: `32598cba146fa4dd7b6162b838630c90d865ab28`
- pointer repo: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`
- pointer revision: `8ac035a394c79e693a3a7d74cb170448503ee894`; TSGolint: `null`
- artifact set: `.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/owner-name-experiment/`
- 両 identity 3項目を再照合し `current`。binary / overlay / source SHA と dirty diff は別途保存。
  AN の binder.go は保存済み N と byte-identical、store.go は保存済み A と byte-identical。
  組合せ以外の変更がないことを `composition-check.json` に記録。
- 候補 checkout は flownode、store-redesign、store-nolock-exp、lock-profile、profile、store-pr-* 等。
  保存済み全パス・revision は `worktrees.txt`。別介入があるため対照に使わない。
- 今回の pointer 再測定、8入力の性能比較、独立再確認、追加 Instruments、parse+bind / GC CPU は `missing`。
  既存 Instruments CPU Profiler が背景証拠。pprof は使用しない。

## 方法

checker / dom の2入力を維持し、今回は **10 bind × 6 rounds** に事前固定。
4 rounds では不足した benchstat の95%区間を得つつ、小規模探索として実施した。
有意になるまで測定を延長していない。

順序は次の6組：
`baseline,A,AN,N` / `A,N,baseline,AN` / `N,AN,A,baseline` /
`AN,baseline,N,A` / `N,AN,A,baseline` / `AN,baseline,N,A`。
各位置への割当は1回または2回で、完全均等ではない。両入力に同じ事前指定順序を適用。

Go 1.26.0、CGO=1、GOMAXPROCS=8、GOMEMLIMIT=off。wall は GOGC=100、
fresh parse と強制 GC を timer 外に置く。KPC は別セッションで GOGC=off、新規 pthread。
4 binary 各2プロセスで KPC 自己検証が成功。初期 baseline / event SHA は保存済み A/A と同じだが、
30 bind の旧 A/A を10 bind の精度保証としては使わない。
KPC と wall は別セッションで、性能測定中に候補のビルドや再標本化集計を重ねていない。

## 正しさ・操作数

代表8入力と追加9ケースで、共通操作数、syntax、diagnostics、Symbol、node-symbol、CFG、visit trace hash が baseline と一致。
AST / binder / compiler 既存単体テストも成功。旧 pointer / Store 間の匿名 class Symbol.Name 差は今回の対象外。
full compiler regression は今回再実行していない。既存失敗群を解消したとは扱わない。

AN の削減回数：

| 入力 | listOwner 呼出 | KindAt | ChildRef |
|---|---:|---:|---:|
| checker | 170,288 | 33,299 | 33,299 |
| dom | 69,924 | 44,119 | 44,119 |

取得回数を全体時間の寄与率に換算せず、以下の実測と分ける。

## KPC の結果

| 入力 | variant | EL0 inst/op | baseline比 | EL0 cycles/op | baseline比 |
|---|---|---:|---:|---:|---:|
| checker | baseline | 185,377,466.5 | — | 87,767,239.5 | — |
| checker | A | 182,838,506.5 | −1.37% | 86,424,405 | −1.53% |
| checker | N | 183,466,787.5 | −1.03% | 87,096,026.5 | −0.76% |
| checker | AN | 181,257,275.5 | **−2.22%** | 86,173,592 | **−1.82%** |
| dom | baseline | 77,700,652 | — | 32,481,268 | — |
| dom | A | 76,673,236 | −1.32% | 32,116,564.5 | −1.12% |
| dom | N | 75,976,514 | −2.22% | 31,612,645.5 | −2.67% |
| dom | AN | 74,859,216 | **−3.66%** | 31,338,915 | **−3.52%** |

AN の baseline 比 inst / cycles は両入力とも benchstat p=0.002。
AN の inst は A 比・N 比でも両入力 p=0.002。
checker の AN vs A の cycles は非有意（p=0.180）。

相互作用は各 round の `AN − A − N + baseline` を baseline で正規化して評価した。
命令数の平均残差は checker +0.20%（bootstrap95%区間 −0.20〜+0.59%）、
dom −0.21%（−0.70〜+0.24%）。いずれもゼロを含み、強い相乗効果・打ち消しは確認していない。
これは独立性の証明ではない。乗法相互作用も保存したが、寄与率を足して全体差を説明したとは扱わない。

## 通常 GC wall・割り当て

| 入力 | variant | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| checker | baseline | 16,292,464.5 | 12,799,537.5 | 14,165.5 |
| checker | A | 16,099,566.5 | 12,799,499.5 | 14,165 |
| checker | N | 16,534,133.5 | 12,799,521 | 14,165 |
| checker | AN | 16,060,623 | 12,799,550 | 14,165 |
| dom | baseline | 5,762,808.5 | 7,868,563 | 16,684 |
| dom | A | 5,675,906 | 7,866,729 | 16,684 |
| dom | N | 5,730,885.5 | 7,866,729.5 | 16,684 |
| dom | AN | 5,571,537.5 | 7,870,422 | 16,684 |

- **dom AN: −3.32%、benchstat p=0.004（n=6）**。
  paired log-ratio 平均 −3.69%、追加 bootstrap95%区間 −4.60〜−2.86%。
  中央値で3%短縮し有意だが、区間全体で3%以上と保証されたわけではない。
- checker AN: −1.42%、p=0.180、非有意。
- A / N 単独の wall は両入力とも benchstat 非有意。checker の単独候補にはばらつきが大きいサンプルがあり、除外していない。
- B/op / allocs の小差をメモリ改善と解釈しない。今回の変更は表現縮小ではない。

## 診断・採用条件・次の行動

広い hot path は walk / helpers。狭い実験で owner 解決と構文名再取得の削減が組合せでも機能し、
事前指定の dom で通常 GC wall 3%以上・有意、inst / cycles 低下を初回探索で観測した。
**次段階へ進める候補を得たが、本体にはまだ採用しない。**

8入力の非悪化区間、独立セッションでの再確認、full regression、parse / bind / parse+bind、
累積 GC CPU / assist / scan / live は未検証。今回の BindHot だけで GC 高速化は判断しない。
既知 allocation driver は symbolIdx / flows 列、symbolRefs、FlowNode 32→48B、Symbol 96→104B、
Declarations Handle 8→16B。これらの追加コストは残る。pointer 同等性能とメモリアクセス・GC 両立も未達。

次は baseline / AN を固定し、測定精度を確認した独立セッションで dom を再確認し、
8入力へ性能比較を広げる。小入力の反復数は保存済み pilot を使用し、原計画の20 roundsを基準にする。
3%以上の退行がないことを区間で評価し、非有意だけを非悪化とは扱わない。
通過後に phase 全体・GC・正しさの採用条件を確認する。

## 再現

- `tools/scripts/tsc/binder_owner_name_experiment.py --out <既存 artifact root>` が組合せの overlay と監査を生成。
- 実行コード `campaign.py`、加法相互作用の解析 `additive-analysis.py` を artifact に保存。
- raw: `wall-6/{baseline,A,N,AN}.txt`, `kpc/kpc-6/{baseline,A,N,AN}.txt`。
- 各 `baseline-vs-*`, `A-vs-AN`, `N-vs-AN` に `benchstat old.txt new.txt`、追加 bootstrap の結果。
- `config.json`, `order.jsonl`, identity に入力・環境・引数・順序・SHA。
- 既存出力を上書きせず、raw の削除・外れ値除去・有意になるまでの追加測定は行わない。
