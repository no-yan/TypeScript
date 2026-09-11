# Snapshot contract v1

## Saved evidence

prepareは指定commitのarchive、overlay、binaryを作る。identityにはrepo_root、typescript_go_git_rev、tsgolint_git_rev=null、tree、helper SHA、Go環境、入力/期待値/ハーネスのdigestを保存する。archiveだけを使うためgit refsやユーザーのbaseline-diffsは書き換えない。runtimeはcleanup対象、その他は証拠。

snapshot.jsonはGo JSONイベントの正確なテスト名をkeyとする。ファイル名は既存runnerが一意性検査する。各子検証にはstatus、失敗/skipログの正規化hash、capture一覧（baseline相対path、actual SHA、reference SHA）を保持する。実内容はcontentsに保存する。

成功したbaseline.Runも捕捉する。内容が不要な場合の既存`NoContent`も記録する。overlayはbaseline承認や期待値の更新をしない。baseline.Runに到達する前のpanicにはcaptureがなく、結果ログが証拠になる。

actual/referenceはその実行の隔離repo絶対pathだけを`<REPO>`へ置換する。改行・型表示・診断番号などは維持する。失敗メッセージではGoの進行行、volatileなstackフレーム、テストソースの行番号を正規化する。assertion内のhex値を無差別に削らない。rawログは無変更で保存する。stackだけの差はフィンガープリントで潰れる場合があるため、原因分析ではrawも読む。

## Comparison

commit hashやrepo pathが違うこと自体は比較を無効にしない。同じ入力/期待値/ハーネス、helper protocol、Go環境、filterが必要。v1は安全側に全tests入力・全reference・testrunner/testutil/repoをfingerprintするため、conformance外のテスト資産変更でも比較不能になる場合がある。その場合も差分一覧は出るが、無条件の「退行なし」とせず変更範囲を確認する。参照baseline変更やcoverageの増減はcomparison.jsonに結果を出しつつverdictをincomparableにする。比較不能な結果から「退行なし」を主張しない。

優先順位: incomparable → regression（pass→fail）→ review_required（内容/skip等変化）→ no_regression_observed。異なる子検証の件数は重複し得るため、入力数と混ぜない。失敗が改善したときのcontent_changedもレビュー対象に出る。

## Limits

snapshotのcompleteは登録済みケースの終了とGoイベントの整合を表す。ハーネスが将来ケースを登録しなくなる変更は、harness digest/coverageの不一致として検出する。既存skipは尊重し、skipを実行成功としない。

これは性能benchmarkではない。elapsedは運用時間であり、overlayの内容保存費用を含むため製品性能を比較しない。公開・commit・baseline承認は自動化しない。
