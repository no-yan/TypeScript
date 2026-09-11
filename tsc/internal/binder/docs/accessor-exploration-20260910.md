# アクセサ内部と呼出側の切り分け：小規模探索（2026-09-10）

## 問題と方針

Store binder の追加命令が、アクセサ内部の解決、呼出側の重複取得・値の受け渡し、
それ以外の処理のどこにあるかを切り分ける。ユーザーの指示により、探索は
**checker / dom の2入力、10 bind × 4 rounds**に縮小した。有意になるまで延長しない。
今回は内部解決の対照 A のみを測定し、呼出側 B と A+B は未測定。

## Identity と artifact

- selected repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`
- Store revision: `32598cba146fa4dd7b6162b838630c90d865ab28`
- pointer repo: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`
- pointer revision: `8ac035a394c79e693a3a7d74cb170448503ee894`
- TSGolint revision: `null`。両 HEAD を今回も照合。
- artifact: `.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/accessor-exploration/`
- 候補 checkout: flownode、store-redesign、store-nolock-exp、lock-profile、profile、store-pr-* 等。
  保存済み全パス・revision は artifact の `worktrees.txt`。別介入があるため対照に使わない。
- 既存 wall / audit / Instruments / KPC v7 と今回の artifact は identity 3項目上 `current`。
  variant の同一性は別途 binary / overlay / source SHA を保存。既存 PropertyAccess 候補は
  baseline overlay で外し、作業ツリーの変更を保持。
- 今回の B、A+B、GC CPU / parse+bind は `missing`。owner-split の性能測定も意図的に `missing`。
  `unsupported` な benchmark を数値比較に混ぜていない。

## 証拠1：同じ node への再アクセス

計数専用 binary で KindAt / FlagsAt / ParentRef / TextAt / ChildRef / ListSlotAt の
入口を一つの系列とし、Store と node の組に前回アクセスしてからの距離を記録。
listOwner / ID の入れ子はこの系列に含めない。これは主要6アクセサの入口系列であり、
全メモリロードの trace ではない。インライン化後に残るロード数とも異なる。

| 入力 | 初回 | 距離1 | 距離2–4 | 距離5–16 | 距離17–64 | 距離65以上 |
|---|---:|---:|---:|---:|---:|---:|
| checker | 298,054 | 1,031,997 | 621,117 | 106,679 | 79,475 | 43,390 |
| dom | 109,605 | 335,846 | 172,634 | 44,422 | 13,865 | 9,855 |

距離1–4は全入口の約76% / 74%。異なるフィールドや child slot の取得も含むため、
この割合を不要アクセス率や削減可能命令率とは扱わない。Flags 等の可変値を
無条件にキャッシュできることも意味しない。操作数と意味の hash は既存 audit と一致。

## 証拠2：通常経路の関数境界

コンパイラの `-m=2` では KindAt / FlagsAt / ChildRef は inline 可能。
listOwner (cost 124)、ListLen (88)、ListElem (112)、ListSlotAt (93) は budget 80 を超える。
これは関数全体の判定で、全呼出箇所での実際のインライン化を保証するものではない。

最初に listOwner の外部 Store 検索を別関数に分離したが cost 95 で依然 inline 不可。
狙った通常経路の CALL が消えなかったため、この候補の性能反復測定は省いた。

## 仮説・対照 A・提案修正

同じ Store の list でも、内部 owner 解決関数の呼び出しと復帰が毎回発生している。
ListElem / ListLen / ListLoc / ListAt / ListRefAt / ExternalListAt の6アクセサ内で
local owner の判定を直接実行し、foreign owner のときだけ元の listOwner を呼ぶ。
欠損値、範囲チェック、外部 Store の解決は保持。binder の呼出箇所は変更しない。

audit では上位アクセサ数・ID 読み取り数は変わらず、listOwner 呼出数だけが
checker 170,288→0、dom 69,924→0。両入力で foreign path は通っていない。
逆アセンブルでも local path が CALL を迂回し、atomic ID 読み取りと比較は残る。
この介入は owner 検証そのものの除去ではなく、通常経路の呼出境界の除去である。
ソースは `fast-owner/store.go` の overlay に限定し、本体には採用していない。

## 実 binder 結果

KPC は GOGC=off、Go 1.26.0、CGO=1、GOMAXPROCS=8、GOMEMLIMIT=off。
初期化後の新規 pthread を使用し、baseline / candidate 各2プロセスの自己検証を通過。
保存済み A/A と baseline binary・イベント SHA は同一。ただし A/A は30 bindなので、
今回の10 bind の精度を再保証するものではない。CPU profile の追加取得はしていない。
帰属の根拠は既存 Instruments CPU Profiler。pprof は使用しない。

| 入力 | baseline EL0 inst/op | A EL0 inst/op | 差 | cycles差 |
|---|---:|---:|---:|---:|
| checker | 185,365,349.5 | 182,558,287.5 | −1.51% | −1.98% |
| dom | 77,778,645.5 | 76,935,540.0 | −1.08% | −0.26% |

命令数は両入力で benchstat p=0.029、cycles は両方非有意。
**n=4 では benchstat の95%信頼区間に必要な6サンプルを満たさない**。
追加 bootstrap の区間も少数標本の探索指標に限定し、採用の根拠にはしない。

通常 GC の独立した wall 探索（同じ10 bind × 4 rounds）：

| 入力 | baseline ns/op | A ns/op | baseline B/op | A B/op | allocs/op 両版 |
|---|---:|---:|---:|---:|---:|
| checker | 17,855,239.5 | 19,813,294 | 12,799,537 | 12,799,524.5 | 14,165 |
| dom | 6,363,825 | 5,726,956 | 7,870,358 | 7,863,011 | 16,684 |

wall の中央値差は +10.97% / −10.01%、どちらも非有意。
この小規模 wall は安定した速度改善・非悪化を判定できない。命令数と異なる符号を
採用判断に読み替えず、外れ値削除や有意になるまでの延長もしない。

## 正しさと限界

- 2入力の syntax / 診断 / Symbol / node-symbol / CFG / visit trace と共通操作数が一致。
- candidate overlay で AST / binder / compiler 既存単体テスト PASS。
- 今回は full compiler regression、8入力拡張、外部 list を含む専用追加監査は未実施。
- 既存 pointer / Store 間の匿名 class Symbol.Name 差は今回も修正対象外。
- 割り当ての既知候補は symbolIdx / flows 列、symbolRefs、FlowNode 32→48B、
  Symbol 96→104B、Declarations Handle 8→16B。今回の介入はそれらを縮小しない。

## 診断・採用条件・次の行動

広い hot path は walk / helpers。今回の狭い実験で、分散した list アクセサ内部の
呼び出しコストが追加命令の一部であることを確認した。削減は約281万 / 84万命令。
過去の pointer 比の追加約7,436万 / 2,862万命令と比べても小さい
（別セッションなので厳密な寄与率の推定ではない）。残差を「アクセス以外」とは断定できない。
header / slot / text の他の解決経路と呼出側 B は未介入だからである。

次は反復数を増やすより先に、B の対象を同じ node の header 再取得箇所に絞る。
既存 call-site census と今回の再アクセス距離を合わせ、同じフィールドの再取得と
異なるフィールドを読むための header 再解決を区別する。可変 Flags の更新境界を保つ。
A / B / A+B を小規模比較し、相互作用を測ってから、有望な組合せを8入力・十分な反復に広げる。
list-only hoist、Kind cache の既存案をこの集計だけで再採用しない。

採用条件は変更なし：通常 GC binder wall 3%以上、有意、inst / cycles 低下、独立セッション、
代表入力の非悪化区間、意味検証、parse+bind / live / scan / GC CPU の確認。
今回の A は探索用 overlay のままとする。

## 再現

- `tools/scripts/tsc/binder_accessor_experiment.py --out <既存 artifact の絶対パス>`
  は fast-owner の wall / KPC / audit overlay と単体テストを生成する。既存出力は上書きしない。
- census と初期分離候補の生成は artifact `prepare.py`、実行は `campaign.py`。
  実行時の絶対パス・引数・順序は各 `order.jsonl`、環境と binary SHA は `config.json`。
- raw は `wall-4/{baseline,candidate}.txt` と `kpc/kpc-4/{baseline,candidate}.txt`。
  両組とも `benchstat baseline.txt candidate.txt` を実行済み。
- KPC 初回起動は一時コピーの実行属性不足で失敗。エラーを保存し、実行属性のみ修正して
  測定開始前に再実行。失敗起動を測定サンプルに含めていない。
