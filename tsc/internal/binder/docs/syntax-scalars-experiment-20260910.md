# 子参照の整数一括取得：If＋生成walker（2026-09-10）

**ChildRefの連続呼出しをやめ、Store内で開始位置を一度だけ解決して整数参照を返す実装を追加した。**
Ifに加え、頻度の高い生成walkerの147 case・291箇所へ展開し、生成元にも反映。
Binder全体のEL0命令はchecker −1.69%、dom −1.76%（各p=.002）。
domのEL0 cyclesは−2.04%（p=.041、A/A合格）。通常GC wallは両入力とも非有意。
変更はworktreeに残している。pointer同等・GC高速化や全面採用の条件を満たしたという結論ではない。

## 問題と実装

単に3つのChildRefを並べたIf版では、compilerはnodes/base/childStartの解決を共有しなかった。
そこでIfStatementRefsと、schemaの子数から生成するSyntaxChildren1〜6を追加した。
いずれもChildRefを内部で呼ばず、次のように読む。

```go
n := &s.nodes[ref]
// nil/ref=0と期待するchildLenの検査を行う。
start := int(n.childStart)
children := s.children[start : start+3]
return children[0], children[1], children[2]
```

Binder側は返されたNodeRefのみ保持する。Ifの条件bindとthen/elseのFlow接続、walkerの訪問順、list訪問位置は元のまま。
Flags・Symbol・Flowの読出し時点を変えず、構文child参照だけを先取りする。
生成walkerは整数作業変数child0〜child5をcase間で共有し、再帰をまたぐslice/pointer保持を避けた。

対象ファイル:

- `tsc/internal/ast/store_if_syntax.go`、境界・欠損・growth後snapshotのテスト
- `tsc/internal/ast/store_syntax_children_generated.go`
- `tsc/internal/binder/binder.go` のbindIfStatementRef
- `tsc/internal/binder/bindwalk_generated.go`
- `tools/scripts/tsc/generate-go-ast.ts`（両生成物の再生成一致を確認）

前提はBind中の対象child参照が変わらないこと。返却後のStore再配置自体は整数参照に影響しないが、構文編集はsnapshotへ反映されない。
以前の27構文writer・20入力監査は根拠の一部で、全入力・並列writerに対する不変契約の強制は未実装。
元の整数配列のnoscan性とレイアウトは維持する。-B、setter検査削減、2pass変更、全node cacheは入れていない。

## 対象範囲と機械語

現在のworktreeには既存PropertyAccessExpressionRefsの変更があり、これを全条件に揃えた。
従って以前の保存baselineよりwalkerのChildRef実行回数が少ない。

| 削減されたChildRef | checker / bind | dom / bind |
|---|---:|---:|
| If一括取得 | 19,086（If 6,362回） | 0 |
| 生成walker追加 | 116,031 | 74,171 |
| 合計 | 135,117 | 74,171 |

残存ChildRefはchecker 349,295、dom 122,017。hot path全体はwalk/helpersで、この一括取得の対象より広い。
Ifが実行されないdomでも効果が出る範囲に広げた。Ifだけのdom差はIfの処理削減に帰属させない。

- 生成walker内のSyntaxChildren helper CALLは0。実際にinlineされ、各caseでheader/startを一度だけ解決する。
- 元版のChildRefもinlineだったので、ChildRef CALL除去として効果を数えない。
- walkerのframeは元版48 B→今回80 B。先行pointer借用版の352 Bより小さいが、別sourceの過去値との速度対照ではない。
- IfStatementRefsもIf caller内にinline。子値は再帰前に整数として取得される。
- 引数・返り値・spill・検査を含む実装全体の比較であり、保持費だけの独立分離や最適性の証明ではない。

## 測定条件

normal（If変更前）、if_batch（Ifのみ一括取得）、all_batch（If＋生成walker）の3条件。
その他のdirtyなPropertyAccess変更を揃えた。保存旧Binderをそのまま新beforeとはしていない。
checker.ts / dom.generated.d.ts、10 bind × 6 round、順序と逆順を組み合わせた固定比較。
各modeで同一normal binaryのA/Aも6 round実施。通常GC wallとGOGC=off KPCは逐次実行。
Go1.26.0、Apple M1、darwin/arm64、GOMAXPROCS=8。各KPC binaryの自己検証2回成功。
rawを保存しbenchstatで全pairを比較。精度不足でも事前規定の固定探索比較を完了し、追加roundは行わない。

### normal → all_batch（Binder全体の中央値）

| 入力 | 指標 | normal | all_batch | 差・判定 |
|---|---|---:|---:|---|
| checker | ns/op 通常GC | 16,594,712.5 | 16,362,465 | −1.40%、p=.937、A/A不合格 |
| dom | ns/op 通常GC | 6,010,392 | 5,896,931 | −1.89%、p=.310、A/A合格だが非有意 |
| checker | B/op 通常GC | 12,799,460 | 12,799,473 | 実質不変 |
| dom | B/op 通常GC | 7,866,703.5 | 7,866,729 | 実質不変 |
| checker | allocs/op | 14,165 | 14,165 | 同じ中央値 |
| dom | allocs/op | 16,684 | 16,684 | 同じ中央値 |
| checker | EL0 inst/op | 184,122,038 | 181,002,459 | **−1.69%、p=.002** |
| dom | EL0 inst/op | 77,850,265 | 76,477,815 | **−1.76%、p=.002** |
| checker | EL0 cycles/op | 88,342,061 | 85,829,934 | −2.84%、p=.009だがA/A不合格 |
| dom | EL0 cycles/op | 32,937,029 | 32,263,788.5 | **−2.04%、p=.041、A/A合格** |

KPCのns/opは計測器の費用を含むため、通常GC時間の代わりに使わない。
L1D missはchecker −0.22%前後、dom +0.24%前後、いずれも非有意。先行pointer借用での約+2%増は今回確認されない。

### Ifだけとwalker追加の分離

| 比較 | checker命令 | dom命令 | cycles |
|---|---:|---:|---|
| normal→if_batch | −0.32%、p=.041 | −0.25%、p=.180 | checker精度不足、dom非有意 |
| if_batch→all_batch | **−1.38%、p=.002** | **−1.51%、p=.002** | dom −1.68%、p=.041、checker非有意・精度不足 |

Ifだけのchecker差は小さく、A/A命令中心ずれ+0.40%と同程度の桁であり、単独効果量を精密に断定しない。
walker追加の命令減少が両入力で確認できる。通常wallは追加比較でも両入力非有意。

A/A paired log-ratio bootstrap区間:

| 指標（許容幅） | checker | dom |
|---|---|---|
| wall（±1.5%） | [−0.73%, +17.72%] 不合格 | [−1.23%, +0.40%] 合格 |
| EL0命令（±1%） | [+0.21%, +0.60%] 合格 | [−0.44%, +0.34%] 合格 |
| EL0 cycles（±1.5%） | [−2.47%, +3.07%] 不合格 | [−1.35%, +0.74%] 合格 |

## 検証・artifact identity

AST/Binder/Compilerテスト成功。3条件すべて20入力で構文、診断、Symbol、Flow、node symbol、counts、訪問traceが保存baselineと一致。
全20入力で予定したChildRef削減と、その他accessor回数の一致を確認。一括取得回数×arityの和がwalker ChildRef削減回数と一致。
新規generated outputの再生成一致、git diff --checkも確認した。

選択repoは `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、typescript_go_git_rev `32598cba146fa4dd7b6162b838630c90d865ab28`。
pointer候補は `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、`8ac035a394c79e693a3a7d74cb170448503ee894`。双方tsgolint_git_rev=null。pointerは今回未測定。
候補cloneはflownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜5、store-pr-6-perf、store-pr-7、store-pr-7-attach-parent-fix、store-pr-7-nodeseq-t10、store-redesign。別介入のため不使用。全件はworktrees.txt。

artifact root: `.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/`

- `remaining-accessor-audit` / `direct-children-experiment-03`: 測定前に確認した既存artifact。identityはcurrentだが今回のbefore sourceとは異なる。
- `if-refs-experiment`: 3 ChildRef先取りの旧候補。wall/KPC保存済み。current identityだが最終実装ではない。
- `if-refs-batch-experiment`: If一括取得のみの3条件build/20入力監査。ユーザーの範囲拡大に従い速度測定せず、performanceはmissing。
- **`syntax-scalars-experiment`: 最終3条件比較、current。** repo_root、tsgolint_git_rev、typescript_go_git_revの一致を検証。
  120 sample、全raw、benchstat、paired summary、precision、binary/source hashes、protocol、監査と機械語を保存。
  全条件に対象Benchmark行が各入力6行あり、unsupportedではない。
- 別root/revisionの旧結果はstaleとして比較から除外。currentというidentityとdirty sourceの一致は別々に検証する。
  最終測定の全Go source snapshotと各overlay/binary hashの一致を完了時に確認した。

## 再現・診断・次の行動

`tools/scripts/tsc/binder_syntax_scalars_experiment.py` のprepare→runtime→wall→kpcで実施。
新規実行は別artifact/runtime名を使い、既存rawを上書きしない。詳細なbuild commandとenvironmentはidentity/configに保存。

開始位置を共有するにはAPI内部で明示的に一括取得する必要があり、単なるChildRefの先取りではcompiler任せの共有にならなかった。
整数返却により、共有位置の再解決削減を、前回より小さいframeで実現できた。
ただしこの範囲の実Binder命令減少は約1.7%。必要なwall約35% / 29%短縮を裏付ける規模ではない。

割当driverのsymbolIdx/flows列、symbolRefs、FlowNode 32→48 B、Symbol 96→104 B、Handle 8→16 Bは残る。
GC時間・parse+bind・live/scanはmissing。今回のB/op/allocs/opは改善していない。
次は精度を満たす独立wall対照と構文不変契約を確認する。さらに広げる場合は残る高頻度callerで同一headerの複数readがある範囲を先に数える。
受入条件は意味/訪問順一致、構文snapshot前提、機械語での一回解決、実Binder wallの再現性ある改善、parse+bind/GCへの悪影響がないこと。
