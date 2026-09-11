# アクセサの取得フィールド・呼出側 B の探索（2026-09-10）

## 問題

同じ node へのアクセスが多いことは、同じ情報の不要な再取得が多いことと同義ではない。
アクセサ内部の対照 A（local list の owner 関数呼出を除く）に加え、呼出側 B を分離し、
再取得削減が実 binder の総命令数を減らすか確認する。

## Identity / artifacts

- selected repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`
- Store: `32598cba146fa4dd7b6162b838630c90d865ab28`
- pointer repo: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`
- pointer: `8ac035a394c79e693a3a7d74cb170448503ee894`; TSGolint: `null`
- artifact set: `.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/accessor-fields/`
- 両 checkout の identity 3項目を再照合し `current`。各 binary / overlay / source SHA は
  variant 別 identity、測定時 binary SHA は config に保存。dirty diff と status も保存。
- 候補 checkout の flownode、store-redesign、store-nolock-exp、lock-profile、profile、
  store-pr-* 等の全パス・revision は保存済み `worktrees.txt`。対照には使わない。
- 新規の Instruments / load-store counter / GC CPU / full regression stage は `missing`。
  既存 Instruments CPU Profiler を背景証拠として使い、pprof は使用しない。
- 本体の未コミット PropertyAccess 候補は保存済み baseline overlay で除外。
  変更は測定 overlay 内だけ。今回は pointer binary の再測定はしない。

## 証拠と仮説の選択

主要6アクセサについて、同じ Store / node への直前のアクセスと、現在のアクセスを分類。
ChildRef / ListSlotAt は slot も一致した場合だけ same-field とする。
呼出関数と行番号は計数専用 binary の runtime.Caller で取得する。CPU 帰属や時間の測定ではない。

| 入力 | 近接・同じfield | 近接・異なるfield | 遠隔・同じfield | 遠隔・異なるfield |
|---|---:|---:|---:|---:|
| checker | 231,252 | 1,421,862 | 79,354 | 150,190 |
| dom | 85,180 | 423,300 | 25,093 | 43,049 |

近接はこの6アクセサの入口系列で距離4以内。初回アクセスは表に含めない。
これらは取得先の分類で、同じ値であること・冗長な機械語ロードであることの証明ではない。
Flags の書き換えを自動追跡していないため、再利用候補の区間は別途ソースを確認する。

目立つ経路：

- checker: contextual identifier の Flags → Parent → Text → bindKind の Flags。
  Parent → Text → Flags は各97,872回。親判定で終わる経路もあり、削減候補の Flags は123,554回。
- dom: contextual identifier の Flags → bindKind の Flags が36,271回。
- dom の nameOfDeclarationRef 間の Kind 再取得も多いが、以前の name memo と重なるため再実験しない。
- walker の ChildRef → ChildRef の約50,585 / 25,966回は異なる slot。不要な同一 child 取得とは呼ばない。

## 提案修正 B と意味の確認

識別子だけ bindKind で Flags を一度読み、checkContextualIdentifierFlagsRef に渡し、
直後の parse-error bit 判定に同じ値を使う。他の kind は従来のタイミングで Flags を読む。
この区間の contextual 判定・診断生成は node Flags を変更しない。子の再帰走査をまたいで
可変 Flags をキャッシュしない。keyword、診断条件、走査順を維持する。

- B: FlagsAt が checker 123,554回、dom 36,271回減少。他の計数は同じ。
- A+B: 上記に加え listOwner が170,288回 / 69,924回減少。
- 2入力の共通操作数、syntax、診断、Symbol、node-symbol、CFG、visit trace hash が一致。
- A+B は追加9ケース（optional、reserved、missing/parse error、JS、JSX、computed/private、
  exports、ambient、fallback）でも既存 baseline と共通操作数・意味の hash が一致。
- A+B overlay の AST / binder / compiler 既存単体テスト PASS。
- 既存 pointer / Store の匿名 class Symbol.Name 差は今回の対象外。full regression は再実行しない。

## 小規模の対照実験

各入力・各 variant **10 bind × 4 rounds**。順序は事前固定：
`baseline,A,AB,B` / `A,B,baseline,AB` / `B,AB,A,baseline` / `AB,baseline,B,A`。
全 variant が各位置に一度ずつ来る。前回の値との差し引きではなく、今回の4者を直接比較。

Go 1.26.0、CGO=1、GOMAXPROCS=8、GOMEMLIMIT=off。KPC は GOGC=off、新規 pthread。
4 binary 各2プロセスの KPC 自己検証が成功。保存済み A/A と baseline / event SHA は同じだが、
その A/A は30 bindであり、今回の10 bind の精度を保証しない。
wall は通常 GC、fresh parse と強制 GC を timer 外に置く既存 BindHot。
KPC と wall は同時実行しない。

### EL0 instructions / cycles

| 入力 | variant | inst/op | baseline比 | cycles/op | baseline比 |
|---|---|---:|---:|---:|---:|
| checker | baseline | 185,175,774 | — | 87,304,890.5 | — |
| checker | A | 182,829,563 | −1.27% | 86,165,754 | −1.30% |
| checker | B | 185,707,613 | +0.29% | 87,490,190 | +0.21% |
| checker | A+B | 182,888,880 | −1.23% | 86,535,250.5 | −0.88% |
| dom | baseline | 77,644,281.5 | — | 32,453,427.5 | — |
| dom | A | 76,635,763 | −1.30% | 32,054,041 | −1.23% |
| dom | B | 77,726,267.5 | +0.11% | 32,583,353.5 | +0.40% |
| dom | A+B | 76,860,939 | −1.01% | 32,589,038 | +0.42% |

benchstat では A の inst / cycles は両入力 p=0.029。B はどちらも非有意。
A+B の inst は両入力 p=0.029、cycles は非有意。
**n=4 は benchstat の95%区間に必要な6サンプルに不足するため、採用判断には使わない。**
B の増加を統計的に確定したとは扱わないが、減少を示す結果ではない。
A+B が A を明確に上回る証拠はない。

各 round の `AB × baseline / (A × B)` も保存した。命令数の相互作用の追加 bootstrap 区間は
両入力でゼロ効果を含み、小標本での方向確認に限定する。A と B の寄与率を足さない。

### 通常 GC wall / allocation

| 入力 | variant | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| checker | baseline | 16,745,485 | 12,799,511.5 | 14,165 |
| checker | A | 16,674,148 | 12,799,524 | 14,165 |
| checker | B | 16,441,637.5 | 12,799,511.5 | 14,165 |
| checker | A+B | 16,223,958 | 12,799,498.5 | 14,165 |
| dom | baseline | 5,760,739.5 | 7,866,716.5 | 16,684 |
| dom | A | 5,704,690 | 7,870,409 | 16,684 |
| dom | B | 5,757,943.5 | 7,870,409 | 16,684 |
| dom | A+B | 5,688,510 | 7,870,396 | 16,684 |

wall は全 baseline 比較で非有意。A+B checker の中央値 −3.11%も採用条件の達成ではない。
割り当て回数は全 variant で同じ。B/op の小差を表現縮小とは解釈しない。
既知 allocation driver は symbolIdx / flows 列、symbolRefs、FlowNode 32→48B、
Symbol 96→104B、Declarations Handle 8→16B。今回はどれも変更しない。

## 診断・採用条件・次の行動

広い hot path は walk / helpers。狭い対照 A では内部の関数境界を減らし、命令削減を今回も観測。
B はアクセス回数を減らしても命令削減にならない。生成コードでは bindKind の呼び出し前に
Flags を `92(RSP)` に保存し、呼び出し後に再ロードする命令が追加されている。
kind による再取得の分岐や他のコード生成変化もある。これらは相殺の候補だが、
静的命令行数から動的な寄与を割り当てることはしない。B の全残差を spill だけに帰属しない。

今回の B / A+B は採用しない。これで「Flags の再利用は常に無効」とは結論しない。
値の受け渡し方法を変える候補は別の実験である。
次の優先対象は A と同じアクセサ内部の通常経路・関数境界、特に inline budget を超えていた
ListSlotAt。単純に B の測定回数を増やさず、通常経路の CALL を実際に減らせる介入を先に確認する。
有望な内部経路をまとめる段階で入力数・反復数を増やす。

採用条件は通常 GC binder wall 3%以上・有意、inst/cycles 低下、独立再測定、8入力の非悪化区間、
意味検証と parse+bind / live / scan / GC CPU の確認。今回の結果はこれらを満たさない。

## 再現

- 計数: `tools/scripts/tsc/binder_accessor_fields.py --out <既存 artifact root>`。
- B / A+B の生成と監査: `tools/scripts/tsc/binder_flags_experiment.py --out <同 root>`。
  出力ディレクトリが既存の場合は上書きせず停止する。
- 今回実行したソースは artifact の `prepare.py`, `prepare-flags.py`, `campaign.py`。
- raw は `wall-4/{baseline,A,B,AB}.txt` と `kpc/kpc-4/{baseline,A,B,AB}.txt`。
- 各組について `benchstat old.txt new.txt` を実行済み。`baseline-vs-*`, `A-vs-AB`, `B-vs-AB`
  に比較結果、`interaction.json` に相互作用、`order.jsonl` に引数・順序を保存。
