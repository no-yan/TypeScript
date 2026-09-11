# Commit snapshot

開発者が指定したcommitでconformanceを実行し、その時点の正しさの観測を保存する。

## Sub-features

- 指定commitだけを独立したコピーへ展開してbuild。
- suiteの成功・失敗・skip・未終了を記録。
- conformance入力の最上位ディレクトリを機能カテゴリとして、file/configurationケースの成功・失敗・skip・未終了の件数と比率を記録。
- 診断/出力/type/symbol等のactual・referenceを保存。

## How to get to it (user POV)

repo rootから`conformance.py prepare --revision HEAD`と`doctor`を実行する。未コミット変更は対象外。

## Driving it with conformance.py

SKILL.mdのLaunchとDriveに従い、新しいartifact名でprepare → doctor → snapshotを実行する。report.mdとsnapshot.jsonのcommit、case_counts、category_counts、complete、exit_codeを確認する。各カテゴリのstatus合計がそのカテゴリの総ケース数と一致し、全カテゴリの合計がcase_countsと一致することを確認する。`contents/`と`captures.jsonl`があり、baseline-diffsが指定artifact配下に作られることを確認する。cleanup後もsnapshot・内容が読めることが証拠。

## Gotchas

suite失敗を含む完全snapshotでもhelperは0で終了する。TestLocalは元々regressionも含むため、overlayを省いた直接実行はこのfeatureと同じ対象ではない。選択0件や途中停止は完全snapshotにしない。
