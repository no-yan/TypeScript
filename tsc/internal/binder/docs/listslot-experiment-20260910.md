# ListSlotAt のインライン化と関数境界の探索（2026-09-10）

## 問題・既存証拠・仮説

広い hot path は walk / helpers。前段の local list owner 呼出削減 A は命令数を約1.3%減らしたが、
呼出側の Flags 再利用 B は再取得回数を減らしても総命令数を減らさなかった。
次の狭い対象として ListSlotAt を選択した。既存 audit の入口回数は checker 110,158、dom 87,747。
既存 compiler 診断では inline cost 93 > budget 80。通常経路に関数呼出が残っていた。

仮説：既に判定済みの receiver / local index のゼロ判定をソース上で重ねなければ、
同じ accessor の結果・呼出回数を維持したまま inline 可能になり、命令を削減できる。

## Identity・artifact

- selected repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`
- Store revision: `32598cba146fa4dd7b6162b838630c90d865ab28`
- pointer repo: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`
- pointer revision: `8ac035a394c79e693a3a7d74cb170448503ee894`; TSGolint revision: `null`
- 両 HEAD と repo_root / tsgolint_git_rev / typescript_go_git_rev を照合し `current`。
  dirty variant は別途 overlay、source、binary SHA で管理。dirty patch、status、未追跡 Go SHA を保存。
- artifact set: `.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/listslot-experiment/`
- 候補 checkout: flownode、store-redesign、store-nolock-exp、lock-profile、profile、store-pr-* 等。
  全パス・revision の保存済み一覧は `worktrees.txt`。これらを対照には使用しない。
- 今回の pointer 再測定、追加 Instruments、I-cache counter、GC CPU、parse+bind、full regression は `missing`。
  既存 Instruments CPU Profiler を背景証拠とし、pprof は使わない。
- 現在の未コミット PropertyAccess 候補は保存済み baseline overlay で除外。本体の変更は保持。

## 提案修正と生成コード

ListSlotAt の `s.listSlot(...)` を、その場で local index を読む処理に置換。
local != 0 では `s.id.Load()` を使い ListRef を組み立てる。receiver は直前に検証済み。
nil、missing node、slot 範囲、foreignLists fallback、atomic ID 読み取りを維持する。
公開 API、binder の呼出箇所、走査アルゴリズムを変えない。

inline cost は75となり、実際の caller コードから ListSlotAt の CALL が消えた。

| 関数 | baseline の表示コード量 | candidate | ListSlotAt CALL の静的箇所数 |
|---|---:|---:|---:|
| forEachBindChildGenerated | 41,168 B | 61,296 B | 129 → 0 |
| modifiersRefGenerated | 1,104 B | 6,192 B | 31 → 0 |

コード量は保存した objdump の表示語数×4。静的箇所数・inline cost を動的命令数と同一視しない。
展開によるコード量増加は確認できるが、これを I-cache miss の増加と読み替えない。

## 意味の検証

- checker / dom の共通操作数、syntax、diagnostics、Symbol、node-symbol、CFG、visit trace hash が一致。
- ListSlotAt の入口回数は同じ。ID メソッド呼出44,479 / 21,958回が直接 atomic load に置き換わり、
  同数の direct_id_load を計数。ID 読み取り自体は削減しない。
- 追加9ケース（optional、reserved、missing/parse error、JS/JSX、computed/private、exports、ambient、fallback）の監査も一致。
- 旧実装を oracle として nil、missing、範囲外 panic、local / foreign / empty list、ID=0 / 1 / uint32最大を
  90組で比較。baseline / candidate とも成功。
- 両 overlay で AST / binder / compiler の既存単体テスト成功。
- 既存 pointer / Store 間の匿名 class Symbol.Name 差を今回修正・解消したとは扱わない。

## 初回の小規模実験

checker / dom、10 bind × 4 rounds、交互順序。Go 1.26.0、CGO=1、GOMAXPROCS=8、GOMEMLIMIT=off。
KPC は GOGC=off、新規 pthread。両 binary 各2プロセスで KPC 自己検証成功。
保存済み30 bind A/A と初回 baseline / event SHA は同じだが、10 bind の精度を保証しない。

| 入力 | baseline EL0 inst/op | candidate | 差 | baseline cycles/op | candidate | 差 |
|---|---:|---:|---:|---:|---:|---:|
| checker | 185,168,005 | 186,121,306 | +0.51% | 87,917,846 | 88,987,610.5 | +1.22% |
| dom | 77,760,209.5 | 77,854,921.5 | +0.12% | 32,546,144 | 32,661,582 | +0.35% |

benchstat では上記はすべて非有意（checker inst p=0.057）。CALL の消失から期待した総命令削減は観測しなかった。

通常 GC の独立 wall 探索も10 bind × 4 rounds。parse と強制 GC は timer 外。

| 入力 | baseline ns/op | candidate ns/op | baseline B/op | candidate B/op | allocs/op 両版 |
|---|---:|---:|---:|---:|---:|
| checker | 19,626,273 | 19,396,981.5 | 12,799,524 | 12,799,537 | 14,165 |
| dom | 6,783,779 | 6,878,427.5 | 7,870,371 | 7,870,396.5 | 16,684 |

wall / B/op / allocs は非有意。中央値の差を同等性・非悪化の証明には使わない。

## 追加の最小対照：同じ関数本体＋noinline

予想した命令削減がなかったため、関数本体を固定して `//go:noinline` だけを追加した。
gofmt が加えるコメント区切り以外は同一であることを照合。
インライン候補 vs noinline 候補の新しい paired session を、事前に10 bind × 4 roundsに固定。
元の測定の延長ではない。この対照の baseline はインライン候補であり、過去の A/A baseline と異なる。
KPC 自己検証はこの両 binary でも各2プロセス成功。新しい A/A は未実施。

| 入力 | inline EL0 inst/op | noinline | inline比 | inline cycles/op | noinline | inline比 |
|---|---:|---:|---:|---:|---:|---:|
| checker | 186,192,327.5 | 184,981,958.5 | −0.65% | 87,897,731.5 | 87,779,501 | −0.13% |
| dom | 77,565,268.5 | 77,682,126 | +0.15% | 32,410,516 | 33,016,634.5 | +1.87% |

checker の inst / branch は p=0.029。dom の inst と両入力の cycles は非有意。
**全比較 n=4 で、benchstat の95%区間に必要な6サンプルに不足する。採用の証拠にはしない。**
別セッションの中央値をつなぎ、noinline vs 元 baseline の効果を算出してはいけない。
noinline の通常 GC wall は `missing`。

## 診断・採用条件・次の行動

今回の ListSlotAt インライン候補は採用しない。checker の追加対照は、アクセサの通常経路の
関数呼出を消しても、呼出側への展開によって総命令が増える場合があることを示す探索結果。
コード量は増えているが、どの分岐・レジスタ処理が何命令を説明するかは未確定。
I-cache が原因とも、dom でも同じ機構が支配するとも断定しない。

既知の allocation driver は symbolIdx / flows 列、symbolRefs、FlowNode 32→48B、Symbol 96→104B、
Declarations Handle 8→16B。今回は変更せず、GC 改善も判定していない。

次は inline 可能な accessor を網羅的に増やす方針を止める。
内部の local owner 呼出削減 A と宣言名 memo のように実 binder の命令削減が確認できた介入を
候補として残し、組合せの相互作用を小規模に確認する。3%に届きそうな対象を選べた段階で、
代表入力と反復数を増やす。残りの退行を accessor 以外の処理と断定することも避ける。

採用条件は変更なし：通常 GC binder wall 3%以上・有意、inst/cycles低下、独立確認、8入力の非悪化区間、
正しさ、parse+bind / live / scan / GC CPU。元の pointer 同等性能・noscanとの両立は未達。

## 再現

- 初期生成: `tools/scripts/tsc/binder_listslot_experiment.py --out <既存 artifact root>`。
- audit / 境界テスト: artifact `verify.py`。追加対照: `prepare-noinline.py`。
- 実行コード: `campaign.py`, `noinline-campaign.py`。出力が既存なら上書きせず停止。
- raw: `kpc/kpc-4/{baseline,candidate}.txt`, `wall-4/{baseline,candidate}.txt`,
  `noinline-kpc/kpc-4/{baseline,candidate}.txt`。最後の baseline は **inline候補**。
- 全組で `benchstat old.txt new.txt` を実行。SHA・環境・引数・順序は identity/config/order.jsonl。
- 新規のプロファイル取得は行わず、生成コードは asm / codegen.json に保存。
