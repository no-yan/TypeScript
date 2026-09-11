---
name: verify-conformance
description: Run TypeScript Go conformance tests at a pinned commit, preserve diagnostic/output/type/symbol snapshots, and compare commits for regressions without accepting baselines. Use for conformance verification or commit-by-commit regression checks.
---

# Verify TypeScript Go conformance

日本語で結果を説明する。このskillはconformance suiteの検証用。CLI全体の性能測定・encoder・regression suite・checkerの修正は自動的に開始しない。

`tsc/internal/testrunner/compiler_runner_test.go`の`TestLocal`はregressionとconformanceを混合する。helperはGit commitをarchiveした独立コピーとGo overlayを使い、conformanceだけを選ぶ。診断、出力、source map、型、シンボルなど既存の検証内容は維持する。ユーザーのcheckout、参照baseline、ブランチは変更しない。

入口一覧は[features/README.md](features/README.md)。スナップショットと比較の意味は[references/snapshot-contract.md](references/snapshot-contract.md)。

## Launch

repo rootから実行する。Python 3.12以上（tarの安全な展開filter）、Git、リポジトリ指定のGo toolchainが必要。Node.jsは不要。

```sh
SKILL=.codex/skills/verify-conformance
$SKILL/scripts/conformance.py prepare --repo . --revision HEAD --out "$SKILL/artifacts/head-build"
```

`prepared`、commit、入力件数が表示され終了コード0ならbuild済み。`artifacts/head-build/identity.json`の`ready: true`とbuild.logでも確認できる。既存ディレクトリには上書きしない。次の実行には新しい名前を使う。

対象はcommit済み内容だけ。dirtyな変更は含まれず、identityに除外したtracked変更が記録される。未コミットコードを検証したい依頼に、HEADの結果で回答しない。このhelperはdirty snapshotに未対応と説明する。

Goのbuildが失敗したらbuild.logを確認し、実行成功を装わない。prepare失敗時はそのruntimeを自動削除し、ログ・identityは保持する。conformance失敗は既知の起点にも存在するため、build不能とsuiteの失敗を区別する。

## Doctor

```sh
$SKILL/scripts/conformance.py doctor --prepared "$SKILL/artifacts/head-build"
```

読み取り専用。binary、helperのSHA、入力・参照baseline・ハーネスがprepare時と一致するか検査する。`ready: true`が条件。古いcommitでも意図して指定した対照なら利用できる。doctor成功はconformance成功を意味しない。

## Drive

初回は同じ準備済み版で2回取得し、比較の安定性を確認する。

```sh
$SKILL/scripts/conformance.py snapshot --prepared "$SKILL/artifacts/head-build" --out "$SKILL/artifacts/head-a"
$SKILL/scripts/conformance.py snapshot --prepared "$SKILL/artifacts/head-build" --out "$SKILL/artifacts/head-b"
$SKILL/scripts/conformance.py compare --old "$SKILL/artifacts/head-a" --new "$SKILL/artifacts/head-b" --out "$SKILL/artifacts/head-aa"
```

commit間比較では同じ手順で比較元を準備する。直前commitと比較する例:

```sh
$SKILL/scripts/conformance.py prepare --repo . --revision HEAD^ --out "$SKILL/artifacts/parent-build"
$SKILL/scripts/conformance.py snapshot --prepared "$SKILL/artifacts/parent-build" --out "$SKILL/artifacts/parent"
$SKILL/scripts/conformance.py compare --old "$SKILL/artifacts/parent" --new "$SKILL/artifacts/head-a" --out "$SKILL/artifacts/parent-to-head"
```

マージcommitの`HEAD^`が意図した基点か、先にgit logで確認する。Store直前版とpointer版は異なる目的の対照。ユーザーの指定した基点を優先し、無関係なcloneを勝手に選ばない。

絞り込みはGoのサブテストregexで指定する（既定はsuite全体）。例:

```sh
$SKILL/scripts/conformance.py snapshot --prepared "$SKILL/artifacts/head-build" --out "$SKILL/artifacts/focused" --filter '^TestLocal$/^indexSignatureTypeInference\.ts$'
```

snapshotコマンドは、失敗を含んでも**完全なsnapshotの取得**に成功すれば0を返す。suite自体の終了コードと成功・失敗・skipはsnapshot.json/report.mdに保存する。panicなどによる未終了・対象0件・実行異常は2。0をテスト全成功と読み替えない。

compareの終了コード: 0=新規退行を観測せず、1=pass→fail、2=比較不能、3=内容/skip等の変更で要確認。既存失敗が残っていても0になり得る。「全テスト成功」とは別。

## Gitでレビューするsnapshot

Git管理する結果は`snapshots/*.snap`。JSONは`artifacts/`内の機械比較用データであり、コミットしない。完全なcaptureを取得した後に、以下で追跡対象を更新する。

```sh
python3 "$SKILL/scripts/export_snapshots.py" --source "$SKILL/artifacts/head-a" --out "$SKILL/snapshots"
git diff -- .codex/skills/verify-conformance/snapshots
```

初回は未追跡ファイルなので、`git add -N .codex/skills/verify-conformance/snapshots`で通常のdiffに表示できる。内容確認後にsnapshotを変更と一緒にstageする。自動commit/pushはしない。

保存済みsnapshotとの読み取り専用照合:

```sh
python3 "$SKILL/scripts/export_snapshots.py" --source "$SKILL/artifacts/head-a" --out "$SKILL/snapshots" --check
```

終了コードは0=一致、1=差分あり、2=不完全capture・内容欠損等の異常。`--check`は一切書き換えない。異なるcommitならsummaryのcommit行も変わる。差分ありは自動的に新規回帰という意味ではない。意味判定には既存の`compare`を使う。

summaryは検証したTypeScript sourceのcommit SHA・入力契約・全体件数に加え、conformance入力の最上位ディレクトリを機能カテゴリとしてpass/fail/skip/unfinishedの件数と比率を記録する。比率の分母は各カテゴリで観測したfile/configurationケース総数。stage別snapshotは全ケースのstatus、成功を含む出力fingerprint、失敗/skipの理由、期待値とactualのunified diffを含む。時刻・実行秒数・一時ディレクトリは含めない。出力fingerprintは成功した出力の変化も見逃さないために残す。実際の出力全文はartifactに保存する。

ファイル名末尾の0〜fはテスト名のSHA256先頭1桁による固定区分。ケース追加で他の区分が移動せず、巨大なstageもGitHubでレビューしやすいサイズに分割できる。snapshot更新は既存失敗の記録であり、TypeScriptの期待baselineの承認ではない。絞り込み結果でsuite全体のsnapshotを置き換えない（focused用に別の出力先を指定する）。

## Evidence

各snapshotは`test.jsonl`、`snapshot.json`、`captures.jsonl`、`contents/<SHA256>.txt`、baseline-diffs、実行条件を保持する。成功した検証もactualとreferenceの内容を保存するので、同じfail件数のまま出力が変わるケースも検出できる。

既存の実compiler runnerを通じて入力をparse/bind/check/emitし、生成結果を捕捉する。内部setterで結果を作らない。overlayはsuite選択、出力先の隔離、観測の追加だけ。元の比較・assertionを省かない。各prepareにoverlayを保存するので変更箇所をレビューできる。

報告はファイル×設定の成功/失敗/skip/未終了、pass→fail、fail→pass、内容変更、skip変化、追加/消失ケースを分ける。親子テスト件数を独立した失敗入力として加算しない。比較不能な項目と未確認範囲を明記する。

A/Aで内容が変わる場合、非決定性と新規退行をまだ区別できない。raw/内容差分から原因を確認する。差分を消すために正規化を追加したり、都合のよいrunだけを選んだりしない。

以前の件数だけのJSONや手元のbaseline-diffsだけでは、成功した出力まで含む本snapshotと同等ではない。旧結果は背景資料として保持し、比較の起点はこのhelperで取得し直す。

artifactのrepo/ref/hashなど識別情報は変更しない（prepareのready/cleanup状態だけは更新する）。repo_root、tsgolint_git_rev、typescript_go_git_revが選択checkoutと一致する場合だけ`current`。意図的な旧commitは`stale`でも比較元として使える。未取得は`missing`。Go Benchmark行を使わないこのsuiteで`unsupported`を失敗の別名にしない。

## Cleanup

```sh
$SKILL/scripts/conformance.py cleanup --prepared "$SKILL/artifacts/head-build"
$SKILL/scripts/conformance.py cleanup --prepared "$SKILL/artifacts/parent-build"
```

準備先の`.runtime`だけを削除する。snapshot、内容、比較、identity、overlay、buildログは残る。同じprepareの実行はlockで排他し、実行中のcleanupを拒否する。中断はhelperにCtrl-Cを送り、そのhelperが起動したprocess groupだけを停止する。プロセス名によるkillはしない。再実行には新しいprepareが必要。

失敗した試行にもcleanupを実行し、最後にsnapshot.jsonとreport.mdが残っていることを確認する。過去artifactの削除、baseline承認、GitHub投稿、commit/pushはこのskillの実行に含めない。

## Helpers

実装: `scripts/conformance.py`（各subcommandの`--help`あり）。比較判定の自己テスト:

```sh
python3 "$SKILL/scripts/test_conformance.py"
```

ハーネスのソースが変わってoverlay anchorが一致しなければ停止する。変更を読んでoverlayを更新し、自己テストと1つの実featureを再検証する。feature mapの保守には`$maintain-verification-skill`を利用する。
