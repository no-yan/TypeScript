---
name: verify-ast-walk
description: Evaluate TypeScript Go AST tree-walk performance through the astbench CLI, verify pointer/store traversal equivalence, run daily comparisons, and preserve reproducible evidence. Use for AST walk or layout performance verification; distinguish synthetic results from parser/checker end-to-end performance.
---

# Verify AST walk performance

結果は日本語で説明する。目的はGC非介入で同じ木のedgeを辿るコストを評価すること。GC改善の再証明、型チェック最適化、必ずstoreが勝つ結果の取得へ目的を移さない。Go/shellを使い、Pythonは実行しない。

対象はこのskillを含むcheckoutの`tsc/cmd/astbench` CLI。初期作業先は`/Volumes/SanDisk1TB/worktree/ast-traversal-bench-design`。dirtyなAST変更があるためHEADとworking treeを混同しない。[feature map](features/README.md)を入口にする。

## Launch

Goは`tsc/go.mod`に対応する版（作成時1.26）、Git、shasumが必要。比較集計にはbenchstatをPATHに置く。Node/Python、server、port、authは不要。Go buildは未取得toolchain/moduleを取得することがある。これはdry-runではない。

repo rootから次を実行する。OUTは新規の絶対pathで、証拠を残す永続領域を選ぶ。既存ディレクトリを再利用しない。

```sh
CTL="$PWD/.codex/skills/verify-ast-walk/scripts/control-ast-walk"
OUT="/Volumes/SanDisk1TB/worktree/ast-walk-verification-$(date -u +%Y%m%dT%H%M%S)-$$"
"$CTL" launch "$OUT"
```

`ready: source, helper and binary match`とexit 0で準備完了。launchは一度buildするだけで、常駐processはない。各driveは新しいCLI processとして隔離し、toolのPTY/terminal sessionで直列に実行する。buildと測定、別campaign、Instruments/KPCを並行実行しない。固有OUTは証拠の衝突を避けるが、CPU/cache競合を隔離しない。

失敗時も最後に`"$CTL" cleanup "$OUT"`を実行する。ログは残る。build中のsource変更は検出して停止するので、新しいOUTでやり直す。既存のユーザー変更をresetしない。

## Doctor

```sh
"$CTL" doctor "$OUT"
```

読み取り専用でrepo root、HEADとtracked差分・untracked tscファイルの署名、helperとbinary hashを確認する。毎driveの開始時にも自動実行する。失敗したdrive後はdoctorし、状態を確認してから新しいlabelで再実行する。古い証拠を上書きしない。

doctorは性能環境の無汚染、PMU準備、GC非介入や結果の同値性を証明しない。ignoredなbuild入力や外部依存まで網羅した署名ではないため、特殊なbuild設定は別途保存する。実験のsnapshot identityはcampaignのidentity.jsonで確認する。

## Drive

まず既存artifactをinspectし、current/stale/missing/unsupportedを確認する。過去のreportだけでcurrentとしない。最短の動作検証は[equivalence](features/equivalence.md)のverify。性能を評価する依頼は[sampling](features/sampling.md)と[daily comparison](features/daily.md)へ進み、verifyだけで完了としない。

共通形は`"$CTL" drive "$OUT" LABEL SUBCOMMAND ...`。LABELは英数字・`-`・`_`で毎回固有にする。引数列、cwd、GC環境、stdout/stderr、終了コード、時刻を保存する。終了コード0だけで合格とせずJSONのvalid/complete、訪問数、保存ファイルを読む。

現時点の対応: full-tree/expression、wide/deep/mixed、construction配置、pointer/store、store-only fixture、wall daily、inspect/collect/report。deepは8192 node上限。layout_seedはconstructionでは配置を変更しない。KPC/decision、shuffle、再訪visitor、M1 size sweep/曲線生成は未対応。未対応を代替測定でverified扱いしない。最新sourceで対応を確認したうえでmapを更新する。

参照設計: [主設計](../../../tsc/internal/ast/docs/traversal-measurement-design-20260917.md)、[M1サイズ設計](../../../tsc/internal/ast/docs/traversal-m1-size-scaling-design-20260918.md)。256/16384 nodeは暫定点でcache residencyの証明ではない。

## Evidence

`$OUT/drives/LABEL/`に各操作の証拠、`$OUT/campaigns/`に凍結したsource・binary・plan・attemptsを保存する。cleanup後も残す。元artifactを移動するとidentity内の絶対path参照が壊れるため、保持したまま集計する。copy時はhashと再集計可能性を確認する。

必ずselected repo、candidate clones、artifact set/status、ns/op、B/op、allocs/op、hot paths、allocation drivers、診断、次の行動を報告する。不明値はmissing。currentはrepo_rootとtsgolint_git_rev（対象外はnull）とtypescript_go_git_rev一致が必要。inspectの一致だけではdirty source・build/fixture/planの一致を保証しないためmanifestも読む。

同じ訪問列・属性のverifyが前提。合成の計測器込み区間だけゼロallocation・GCを要求する。実入力CLIの確保や通常GCは正常なmetricだが、現在のこのハーネスでは実入力CLI全体を評価していない。

比較はrawを残しbenchstatを使う。単発sampleや4ペアのdailyから採用を決めない。ns/nodeの分母は実訪問数。命令数/µops/cycles/wallを混ぜず、非有意差を同等・退行なしとしない。外部負荷・thermal/swap telemetryやcurrent top 10は未取得なので、dailyの結果は方向確認とする。旧hot path候補はParent/Expression/Name/Text/listOwner。構築・list materializationの確保は計測外で、ゼロB/opから保持メモリ削減を推定しない。

## Cleanup

```sh
"$CTL" cleanup "$OUT"
test -f "$OUT/cleanup.txt"
test -d "$OUT/drives"
```

最後のdriveと失敗後の再検証まで終えてから実行する。helperはforegroundの子PIDを追跡し、INT/TERM時にその子へTERMを送って終了を待つ。名前によるkillは禁止。active.pidが残ったときはcleanupが拒否するので、その記録と起動sessionを照合し、実際にそのdriveが動いているか確認する。生存していれば起動sessionから中断・終了を待ち、死亡確認後のみstale markerを除去する。PID再利用の可能性があるためmarkerのPIDを無条件にkillしない。

cleanupは使い捨てdriver binaryだけを削除する。campaign snapshot/binaryは再集計に必要な証拠として残す。確認したfeatureのstdout/stderr/exitと結果ファイルをcleanup後にもう一度読み、残っていることを確認する。

## Helpers

唯一の同梱helperは実行可能な`scripts/control-ast-walk`。launch/doctor/drive/cleanupは上記の通り。既存`tools/scripts/tsc/astbench.sh`はgo runを使うため測定反復ごとには使わず、事前buildしたCLIを駆動する。

変更後の保守には`$maintain-verification-skill`でこのskillを指定し、featureごとのsource確認とlive coverageを行う。
