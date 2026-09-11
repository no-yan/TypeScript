# 生成walkerのchildren直読実験（2026-09-10）

生成walkerの291箇所を、caseごとにchildrenの位置を一度解決する固定長配列の直接loadへ変更した。
**実Binder全体のEL0命令はchecker −1.12%、dom −0.50%。通常GCの時間短縮は確認できない。**
両入力の命令数A/Aは合格、wall A/Aは不合格。これは一つの具体的な実装の結果であり、「最適実装の上限」ではない。
walker単独の実行時間や改善率は測っておらず、全体の値をwalker内の改善率として扱わない。

## 問題・仮説・対象

既存のChildRefは既にwalker内にinlineされている。CALL除去ではなく、子ごとのheader、childStart、配列baseの再読出しとslot検査をまとめる実験。
保存caller監査の実行回数はchecker 189,849回、dom 74,321回。全ChildRef 558,230 / 196,338回の一部である。
最大のhot pathは依然walk/helpers全体であり、このchild経路より広い。
生成helper、KindAt、FlagsAt、list経路、setter、2pass等の処理ロジックは変更しない。

caseが知るshapeを使い、たとえば2子のcaseを次の形にする:

```go
children := (*[2]ast.NodeRef)(s.InvestigationChildren()[s.InvestigationChildStart(ref):])
b.bindChildRef(children[0], kind)
b.bindChildRef(children[1], kind)
```

2つの小さいStore helperは、それぞれ既存children sliceとnodes[ref].childStartを返すだけ。
両方とも機械語でinlineされ、子の値は元の訪問時点で読む。再帰前の子値の先読み・Flags値cacheはない。
配列配置・整数配列のnoscan性は元のまま。構文配列が借用中に再配置されないこと、live refとschema通りのslotが前提。
Goの配列境界検査は有効だが、元のchildLenによるslot検査を各readで実行しない。-Bは使用していない。
前段の27構文writer監査では20入力のBind中の対象writerは0だったが、全入力・並列writerの禁止契約は未実装。
実験overlayのみであり、公開アクセサとして採用したものではない。

## 対照・測定結果

checker.ts / dom.generated.d.ts、それぞれ10 bind × 6 round。normal/directを交互・逆順で実行。
同一normal binaryのA/Aも各6 round。通常GC wallとGOGC=offのKPCを順番に実行した。
Go1.26.0、darwin/arm64、Apple M1、GOMAXPROCS=8。比較は保存rawにbenchstatを適用。
以下は中央値。KPC benchmarkのns/opは測定器の費用を含むため、通常wallと混ぜない。

| 入力 | 指標 | normal | direct | 差・判定 |
|---|---|---:|---:|---|
| checker | ns/op（通常GC） | 17,945,548 | 17,604,314.5 | −1.90%、p=.485、A/A不合格 |
| dom | ns/op（通常GC） | 6,352,891.5 | 6,361,264.5 | +0.13%、p=.699、A/A不合格 |
| checker | B/op（通常GC） | 12,799,473 | 12,799,486 | 非有意 |
| dom | B/op（通常GC） | 7,870,409 | 7,868,550 | 非有意 |
| checker | allocs/op | 14,165 | 14,165 | 同じ中央値 |
| dom | allocs/op | 16,684 | 16,684 | 同じ中央値 |
| checker | EL0 inst/op | 185,314,259 | 183,235,342 | **−1.12%、p=.002** |
| dom | EL0 inst/op | 77,729,397.5 | 77,337,019.5 | **−0.50%、p=.002** |
| checker | EL0 cycles/op | 86,692,891.5 | 86,280,150.5 | −0.48%、p=.026だがA/A不合格 |
| dom | EL0 cycles/op | 32,373,118 | 32,290,817.5 | −0.25%、p=.180 |

A/Aのpaired log-ratio bootstrap区間（wall/cyclesは±1.5%、命令は±1%内が条件）:

| 指標 | checker | dom |
|---|---|---|
| 通常wall | [−18.07%, −0.25%] 不合格 | [−8.38%, +4.71%] 不合格 |
| EL0命令 | [−0.57%, +0.32%] 合格 | [−0.41%, +0.17%] 合格 |
| EL0 cycles | [−2.82%, +0.29%] 不合格 | [−0.25%, +0.46%] 合格 |

固定回数の探索比較を実行し、精度不足の指標を採用根拠にしない。追加roundは行っていない。
L1D missはchecker +2.23%、dom +2.15%（各p=.009）、checker branch missは+2.03%（p=.004）。
これらをフレーム拡大の因果証明とはしないが、命令減少がそのまま時間短縮にならないことと整合する。

## 機械語・保持費

- normalのast.Store.ChildRef CALLは0。directの位置/配列helper CALLも0。bindChildRefという訪問用BinderメソッドのCALLとは区別した。
- walker全体の静的命令数（cold path込み）は10,292→5,500。実行命令数とは別物であり、約47%の時間短縮とは解釈しない。
- panicBoundsの静的CALL siteは582→441。case入口のslice/固定長変換等の検査は残る。
- スタックフレームは48→352 B。保持するchildren pointerのspillが実際に存在する。
- QualifiedNameの第2子は、再帰から戻った後に保持pointerをreloadし、4 byte offsetから直接loadする。headerの再解決はない。

この2条件は構築・保持・検査・compiler判断を含む実装全体の比較。保持費だけを独立推定したものではない。
最初の借用helper版ではCALLが147箇所に残ったため、要求されたinline対照として採用せず、最終版ではhelperを分割した。
最終版もフレーム拡大があるので、最適性の証明・理論的上限とは呼ばない。

## 正しさ・identity・artifact

normal/directそれぞれ20入力の構文、診断、Symbol、Flow、node symbol、counts、訪問traceが保存baselineと一致。
ast/binder/compilerのテスト成功。8代表入力のaccessor差はChildRefとwalker.ChildRefだけで、削減回数は予定どおり。
checker 189,849、dom 74,321回のChildRefがなくなり、他accessorの回数は同じ。

選択repo:

- Store: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、`32598cba146fa4dd7b6162b838630c90d865ab28`
- pointer: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、`8ac035a394c79e693a3a7d74cb170448503ee894`
- tsgolint_git_revは双方null。今回pointerは再測定していない。

候補cloneはflownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜5、store-pr-6-perf、store-pr-7、store-pr-7-attach-parent-fix、store-pr-7-nodeseq-t10、store-redesign。別介入を混ぜないため不使用。全件はworktrees.txt。

artifact root:
`.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/`

- `remaining-accessor-audit`、`direct-list-span-experiment-02`: 測定前に確認した既存artifact。選択identity上current。今回の直読版の測定ではない。
- `direct-children-experiment`: 初期変換の対象が他の生成helperまで及んだことをassertで検出し中止。比較stageはmissing。
- `direct-children-experiment-02`: 借用helper CALLが残る初期実装。wallのみ別runtimeに保存、最終比較に使用しない。KPCはmissing。
- **`direct-children-experiment-03`: 最終比較。repo_root / tsgolint_git_rev / typescript_go_git_rev一致でcurrent。**
  wall-aa / wall-pairs / kpc-aa / kpc-pairsの96 sample、raw、benchstat、summary、precision、codegen、audit、build identityを保存。
  requested Benchmark行は各条件・入力6行ありunsupportedではない。
- dirtyな本体Binderと実験baselineは別。currentはdirty source同一性を意味しない。overlay全sourceとbinary hashを再検証し、complete.jsonに本体Binder hashを記録した。
- 別revision/rootの旧結果はstaleとして速度比較に使わない。最終artifactの再配置していない同一source snapshotを比較する。

## 再現・次の判断

`tools/scripts/tsc/binder_direct_children_experiment.py`がoverlayの作成・監査・ビルド・固定順測定を定義する。
prepare、runtime、wall、kpcの順。既存artifactを上書きしないため、再実行する場合は新しいartifact/runtimeディレクトリにする。
KPCは既存の管理者権限測定手順を使う。詳細なbuild command・環境・fixture hashは各identityとconfigに保存済み。

残るallocation driverはsymbolIdx/flows列、symbolRefs、FlowNode 32→48 B、Symbol 96→104 B、Handle 8→16 B。
この実験では配列表現・割当構造を変えていない。B/opに再現性ある改善はなく、GC高速化は未測定。
保存pointer対照に追い付くためのwall約35% / 29%短縮に対し、本案のみで到達できる証拠はない。

次の実装候補は、同じ直読経路でcase間の保持変数を共有する等、352 Bフレームを縮めるもの。
まず機械語で保持位置・frameが改善したことを確認し、その差がなければ新たな速度測定は行わない。
より広いheader借用を設計するなら、実callerで複数回読む範囲と構文有効期間の契約を先に確定する。
受入条件は20入力一致、構文writer契約、CALLと保持費の機械語確認、精度を満たす実Binder wall/命令改善。
本体採用・pointer同等の主張には、parse+bindとGCへの影響も別途必要。
