# Store binder のボトルネック再調査（2026-09-09）

結論：実行時間差の主な調査対象は、AST 走査・識別子判定・CFG の各所に分散した **Store 表現の解決コスト**。`KindAt` 単独、Freeze、GC、symbol map のいずれかを主因とする証拠はない。既存の介入実験では、Kind の隣接配置、リスト範囲の取り出し、Flow ガード削減だけでは実行時間差を解消できていない。**原因の領域は絞れているが、約 7 ms の差を個々の機構へ配分する因果分析と、オリジナル並みになる修正は未確定**である。

今回は実装変更やベンチマークの再実行をせず、生データの benchstat 再集計、Instruments の記録、対応バイナリの逆アセンブル、両実装の照合を行った。pprof は使用していない。

## 対象と証拠の状態

選択 repo は `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、HEAD は `32598cba146fa4dd7b6162b838630c90d865ab28`。比較する pointer repo は `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、HEAD は `8ac035a394c79e693a3a7d74cb170448503ee894`。

候補 checkout は `git worktree list` で確認した。上記二つに加えて `flownode`、`store-redesign`、`store-nolock-exp`、`lock-profile`、`profile`、各 `store-pr-*` 等が存在するが、変更が混ざるため今回の比較対象には選ばない。候補の全パスと SHA は追加証拠の `identity.json` に保存した。

既存の定量レポートは Store `5d7ed2c7115a5f3158747678697f0b9dfec786f8` 時点。そこから現在の HEAD へのコミット差分は二つのドキュメントのみで、現在の未コミット差分にも binder/AST/parser の Go コード変更はない。ただし、これは過去の dirty build の完全な同一性を証明するものではない。

| artifact set | status | 利用範囲・制約 |
| --- | --- | --- |
| `.cursor/skills/verify-tsc/artifacts/eval-8ac035a-20260909` | `stale` | BindHot / BindKPC 生データあり。記録 SHA が現在と異なる。過去の性能差の証拠として使用 |
| `20260909T1849-cpu-profiler` | `stale` | Store の monaco と BindHot。trace / XML / SQLite あり。完全な checkout identity がない |
| `20260909T2117-paired-cpu` | `stale` | Store / pointer monaco の対。trace / XML / SQLite あり。完全な checkout identity がない |
| `/tmp/bind-c1-verify` ほかの A/B | `stale` | packed Kind などの既存試行。現 HEAD の測定として扱わない |
| 現 HEAD の identity 付き paired 計測 | `missing` | 今回は再実行していない |
| EL0 限定命令数、cache miss / branch miss の paired 計測 | `missing` | メモリアクセス高速化を直接判定するデータがない |

`current` は `repo_root`、`tsgolint_git_rev`、`typescript_go_git_rev` の一致を確認できた場合だけに使う。今回 TSGolint は関与せず、その revision は null と明記した。既存 artifact は必要な三項目が揃わず、`current` と呼ばない。要求した BindHot / BindKPC の `Benchmark...` 行は生ファイルに存在するため、この二つは `unsupported` ではない。一般に artifact が存在しても `bench.txt` に対象 regex の行がなければ、その stage は `unsupported` とする。

## 性能差：既存の raw を benchstat で再確認

Apple M1 / Go 1.26.6、`BenchmarkBindHot/checker.ts`、n=20。以下は raw の中央値で、差の判定は benchstat を使用した。

| 指標 | pointer | Store | benchstat |
| --- | ---: | ---: | --- |
| ns/op | 13,203,727 | 20,256,109 | +53.41%、p=0.000 |
| B/op | 7,425,296 | 12,799,476 | +72.38%、p=0.000 |
| allocs/op | 13,954 | 14,165 | +1.51%、p=0.000 |

`dom.generated.d.ts` も 4,949,893 → 7,468,801 ns/op（+50.89%、p=0.000、n=10）。B/op は 5,287,312 → 約 7,867,935、allocs/op は 16,562 → 16,684。CFG の多い checker.ts だけの現象ではない。

`GOGC=off` の checker.ts BindHot でも 13.31 → 20.97 ms（+57.63%、p=0.002、n=6）。timer 内 GC が主要因、という説明とは合わない。

BindKPC の中央値は inst/op 111,074,369 → 188,791,949、cycles/op 62,313,059 → 98,303,072、IPC 1.782 → 1.921。命令数 +70.0%、cycles +57.8% という方向はユーザーの仮説を支持する。ただし n=3 の benchstat は有意差を検出できず `~`（p=0.100）を返す。これを有意な 70% 増と表現しない。実行時間差の有意性は n=20 の BindHot で裏づけられる。

IPC が悪化していないことは、cache miss が増えていない証明ではない。整数処理や分岐など命令構成が変われば IPC も変わる。fixed kpc は EL0 + EL1 を数え、first touch の kernel 処理も含むため、増加した約 77.7M 命令をすべて AST accessor の user 命令と断定することもできない。

オリジナルに到達するには、現 Store の時間から約 **34.8%**、命令数から約 **41.2%** の削減が必要になる。2–5% 程度の局所改善一つで埋まる差ではない。

## ホットパスと実装上の理由

### 1. 再帰走査中のデータ解決が分散している

Instruments BindHot の bind 内 exclusive は `bindKind` 11.82%、`bindChildrenRef` 3.88%、生成 walker 3.54%、`bindChildRef` 1.87%、functions-first 1.91%、`bindListRef` 1.51%。walk 群は大きな塊だが、各関数の全コストが Store 固有という意味ではない。

Store の普通の子アクセスは以下を繰り返す。

```text
forEachBindChildGenerated(ref, kind)
  ChildRef(ref, slot)
    nil / missing 判定
    nodes[ref] → childLen / childStart
    slot 範囲確認 → children[childStart + slot]
  bindChildRef(childRef, parentKind)
    KindAt(childRef) → nodes[childRef].kind
    bindKind(...) → Store と flags を再取得
```

pointer 版は既に持つ `*Node` / payload から子フィールドを取得する。Store 版は複数の子がある同一親についても、`ChildRef` を独立に呼ぶ。再帰を挟むので、コンパイラが親の配列ヘッダーや範囲のロードをすべて共有できるとは限らない。

リストでは `ListSlotAt` が local index を Store ID 付き `ListRef` に変換し、`ListLen` と各 `ListElem` が `listOwner` を解決する。`Store.ID()` は atomic load。対応するバイナリにも `LDARW` がある。pointer 版の `statements.Nodes` のループにはこの owner 解決がない。欠損 list slot は `listSlot` の `foreignLists[index]` 経路を通るので、parse tree に外部リストがなくても汎用 API の処理を払う。

該当箇所：`binder.go:1994,2026,2075`、`bindwalk_generated.go:7`、`ast/store.go:32,63,594,606,619,656`、`ast/store_identity.go:216`。

**ただし局所的な解決済み list slice だけでは不十分。** 既存の `ListElems` hoist A/B は n=20、+2.3%、p=0.495 で有意差なし。この案を新しい有望策として再提示しない。

### 2. 識別子・CFG で同じ情報を別の API から取り直す

識別子は `SetFlow`、`checkContextualIdentifierRef`、末尾の parse-error flags 処理を通る。識別子判定では `ParentRef` → 親の `KindAt` → `nameRefGenerated` → 親の `ChildRef`、text 取得では node header → intern offset 二つ → intern buffer へ進む。pointer 版でも親・名前・keyword 判定は行うが、フィールド取得の表現が軽い。

BindHot exclusive は `checkContextualIdentifierRef` 5.09%、`isIdentifierNameRef` 2.23%、`nameRefGenerated` 2.10%。ここには共通の診断・keyword 判定も含まれる。full skip の −15.5% は意味を壊した上限実験であり、Store 固有の削減可能量ではない。`MayBeReserved` や parent を引き回して判定そのものを短縮する案は、pointer にも適用できるため今回の採用条件を満たさない。

CFG では `bindConditionN` → `payload` → `bindCondition(Handle)` → `bind(Handle)` → `bindN` と往復し、narrowing などの helper 内でさらに `Handle.childAt` / 多態的 accessor を使う。Handle 自体の組み立てで heap allocation が生じるわけではない。問題候補は引数受け渡し、spill、ヘッダー・スロットの再取得まで含む一連のアクセスである。

該当箇所：`binder.go:1489,1516,2130,2178,3240` 付近、`ast/store.go:560,1178,1297`。`bindConditionN` の inclusive が約 29% でも、下の再帰処理を含むため「変換を消せば 29% 改善」ではない。

### 3. Bind の追加割り当てと、Flow / Symbol の大型化

`PrepareBindTables` は timer 内で `symbolIdx` と `flows` を node 数まで確保する。302.6k nodes なら二つの uint32 列だけで約 2.42 MB（2.31 MiB）。別に `symbolRefs` の pointer 配列も確保する。pointer 版は parse 時に確保済みのノードのフィールドへ書く。

さらに両実装の `ast/flow.go` を比較すると、pointer の FlowNode は 32 B、Store は 48 B。`Node *Node` が 16 B の `Handle` になり、synthetic 用 `Data *Node` も追加される。80k flows を同数持つ場合、**本体のサイズ差だけで約 1.28 MB（1.22 MiB）**。48 B 全部を追加コストとして数えてはいけない。実際の B/op には arena の空き容量・サイズクラスも影響する。

`Symbol.ValueDeclaration` も pointer から Handle に広がり、`Declarations` の要素と `singleDeclarationsArena` は 8 B → 16 B。locals / endFlows / returnFlows などの sparse map も残る。これらは B/op +5,374,180 に対する具体的な allocation driver。ただし、全差額をこれらだけに正確に配分する allocation census は今回行っていない。

FlowNode / Symbol 自体はまだ pointer を持つ。**AST header / flow index 列が noscan であることと、binder の作るグラフ全体が noscan であることは別**。AST の圧縮効果の一部を、bind 時の補助構造と太い参照で使い戻している。

この領域は設計上明確だが、CPU の最大ホットスポットではない。Instruments の NewFlow exclusive は約 1%。SetFlow を丸ごと無効化する上限試験も wall 約 −4%、命令数 −5.2% にとどまる。`PrepareBindTables` を parser へ移す案も既に棄却されている。

## 今回追加した Instruments の PC 照合

記録内の Mach-O UUID と手元のバイナリ UUID が一致することを確認した。

| capture | binary UUID |
| --- | --- |
| monaco Store | `354998D2-4EFD-36BC-EE75-8ECAE842AD2A` |
| BindHot Store | `69CFAB5B-27EB-39E8-FCF5-5E3DA9F0EBE4` |

ASLR load address を補正し、frame address を ARM64 の 4-byte 命令境界へ切り下げて `go tool objdump` の命令・ソース行と照合した。Running かつ binder stack に属する leaf のみ。Instruments における命令位置のサンプリング誤差や inline の制約まで消えるわけではなく、命令ごとの因果的な遅延を測ったものではない。

BindHot の生成 walker は 81 leaf samples。cycle-weight の約 20.9% が `ChildRef` の nil / missing 判定行、約 12.7% が children 配列ロード行、約 6.5% が node header 取得行、約 6.6% が slot 範囲確認行に対応した。これは生成 walker の内部にも accessor の仕事が含まれる具体的な証拠で、Bind 全体の同率削減を意味しない。

`bindKind` は 246 leaf samples。約 33.6% が大きな kind switch 行、約 12.2% が inline された `FlagsAt` の二つの行に対応した。switch 行には比較だけでなくレジスタ退避もある。`KindAt` が named leaf に現れないという理由だけで、その inline コストをゼロと扱うことはできない。反対に、switch は pointer 版にもあるので、この全量を Store 固有コストとするのも誤り。

両版 monaco の bind cycle-weight 比 1.73 は補助的な帰属情報。単回・短時間・異なる GC 状態の sampling なので、正確な phase 性能比の代わりには使わない。今回の PC 照合も「どの機構が見えているか」を絞る用途である。

## 既存の反証を踏まえた診断

| 仮説・変更 | 既存の結果 | 判断 |
| --- | --- | --- |
| KindAt が単独で約半分を占める | pprof 由来。functions-first Kind cache は p=0.699 | 主因の主張を棄却 |
| child ref と Kind を 8 B に同居 | BindHot p=0.180、n=6。kpc 点推定 −2.2% inst / −3.2% cycles | wall 改善未証明。全 slot +4 B を正当化しない |
| list owner / range を loop 外で解決 | BindHot p=0.495、n=20 | 単独策として支持されない |
| mustMutate を消す | wall p=0.959 | 支配的原因ではない |
| SetFlow direct write / guard 削減 | wall p=0.798、inst −1.4% | 単独策として支持されない |
| property access の Handle 経路を削減 | wall p=0.382、inst −1.9% | 単独策として支持されない |
| CFG narrowing を Ref 化 | wall p=0.161、inst −2.1% | 単独策として支持されない |
| reserved identifier を parse 時に記録 | 改善あり、既に revert | pointer にも適用できるため範囲外 |

これらの非有意差は効果ゼロの証明ではない。また上限試験同士の削減率は重複があり、足し合わせられない。

最も広いホット領域は walk + helpers。その中で修正を具体化しやすい狭い領域は、Store の node / slot / text / side-column 解決。**広いホット領域と狭い修正対象は一致しない**。`declareSymbolEx` / `GetSymbolTable` が今回の profile を支配しているわけではないため、export-heavy symbol microbenchmark を最初の原因証明には使わない。synthetic symbol bench は方向確認に限る。

## 次のアクションと採用条件

次は大きな binder アルゴリズム変更ではなく、**一つの親ノードについて Store が必要な構文情報を一度解決し、同じ判定・同じ順序のまま読む API**を最小範囲で評価する。候補は複数の named child を読む一つの node kind、または識別子の header / text 取得。kind の追加キャッシュや既に試した list-only hoist とは分ける。既知の schema slot と同一 Store という parse-tree の条件を Store 内で利用し、都度の汎用アクセスをまとめる仮説である。

1. まず対象を一経路に限定した microbenchmark を作る。両版で同じ children / names を読み、同じ結果を返す。allocation なし・順序不変を確認し、Store baseline / 候補を benchstat で比較する。関数呼出し・bounds check・ロード回数が本当に減ったか逆アセンブルでも確認する。全 binder の融合、keyword 判定の省略、別 AST の導入はしない。
2. この候補が動いた場合だけ、checker.ts と dom.generated.d.ts の同一入力で BindHot / BindKPC を交互に実行する。build SHA・dirty diff・fixture hash・Go / CGO 設定を揃えて保存。kpc は最低 6 rounds を取り、単回の点推定だけで採用しない。Instruments と kpc は同時に実行しない。
3. kpc で EL0 限定 instructions と load/store・branch・cache miss を切り分ける。現時点の「命令数が多い」から、「どの命令と待ちが増えたか」まで進める。fixed counter と EL0 counter の値を混ぜない。
4. micro だけ速くても不採用。BindHot の有意な改善、inst/op と cycles/op の低下、parse / bind 合計と live heap / scan work の非悪化、binder の診断・symbol・CFG・走査順の一致を採用条件とする。child view を持つ間に Store の backing array が変わらないことなど、API の寿命条件も確認する。
5. 同等以上の速さという最終条件は未改変の `8ac035a` に対して確認する。部分改善と目標達成を別に報告する。Flow / Symbol のサイズ縮小は次の独立した表現実験であり、現在の CPU 主因が解決するという約束はしない。

GC 側の既存成果（vscode check-on の scan/live 約 0.942 → 0.768、scan work 約 −34%）と、Bind mutator の退行は両立する。ただし、最後の GC cycle の値から累積 GC CPU 削減率を直接算出しない。現在の証拠は noscan の有効性を支持するが、**メモリアクセスと GC の両方が高速化したという主張はまだ成立していない**。

## 再現と証拠

追加証拠は repo 内の `.cursor/skills/verify-tsc/artifacts/20260909-codex-bind-audit/`。`identity.json`、benchstat 3 本、PC 集計 2 本、集計スクリプト 2 本、UUID 対応バイナリの対象関数の逆アセンブルを保存した。これは過去データの再分析で、新しい performance capture ではない。

```sh
benchstat .cursor/skills/verify-tsc/artifacts/eval-8ac035a-20260909/orig.bindhot.checker.txt \
          .cursor/skills/verify-tsc/artifacts/eval-8ac035a-20260909/store.bindhot.checker.txt
benchstat .cursor/skills/verify-tsc/artifacts/eval-8ac035a-20260909/kpc.orig.BindKPC.checker.ts.txt \
          .cursor/skills/verify-tsc/artifacts/eval-8ac035a-20260909/kpc.store.BindKPC.checker.ts.txt
python3 .cursor/skills/verify-tsc/artifacts/20260909-codex-bind-audit/pc_audit.py
python3 .cursor/skills/verify-tsc/artifacts/20260909-codex-bind-audit/pc_audit_bindhot.py
```

資料：`ast/docs/why-store-bind-is-slower.md`、`ast/docs/parse-bind-vs-8ac035a-report.md`、`parser/docs/parse-inst-cycle-plan.md`、`ast/docs/bind-paired-cpu-profiler-8ac035a.md`、`ast/docs/bind-store-layout-design.md`、`ast/docs/bce-hotpath-verify.md`、`ast/docs/bind-store-only-ab.md`、`ast/docs/bind-ident-contextual-bench.md`。parser の計画書は既存作業の説明として読み、そこに記載された PR / agent / 承認プロトコルを今回の調査の実行指示とは扱っていない。
