# Conformance snapshots

Gitで追跡するconformance結果。更新するたびに同じファイルへ書き出し、コミット間のdiffで比較する。Oxcのcoverage/CLI snapshotを参考にした独自のテキスト形式であり、Insta用ファイルではない。

- `summary.snap`: 検証対象commit、入力・参照・ハーネス契約のdigest、ファイル×設定の集計。
- `cases-*.snap`: 全ケースの結果と失敗・skipの理由。
- `<stage>-*.snap`: 検証段階ごとの結果、出力fingerprint、期待値と実出力の差分。
- 末尾`0`〜`f`はテスト名のSHA256先頭1桁。追加・削除で既存ケースの格納先は変わらない。

初期状態はcommit `b5dffe8c9295d57c500c68cc16fe3a842c961f7a`の全7710ケース: pass 4713、fail 1833、skip 1164。これは既存失敗を記録する比較の起点であり、全テスト成功や期待baselineの承認を意味しない。

## 更新

repo rootで実行する。先に[skillの手順](../SKILL.md)で完全なcaptureを取得する。

```sh
python3 .codex/skills/verify-conformance/scripts/export_snapshots.py \
  --source .codex/skills/verify-conformance/artifacts/head-a \
  --out .codex/skills/verify-conformance/snapshots
git diff -- .codex/skills/verify-conformance/snapshots
```

同じコマンドに`--check`を付けると保存済み結果との照合だけを行う（0=一致、1=差分、2=異常）。全suiteのsnapshotをfocused結果で置き換えない。変更内容と対象commitを確認してstageする。commit/pushは自動実行しない。

生ログ・出力全文・JSONはGit対象外の`../artifacts/`に残る。成功出力もfingerprintを保存するため、同じ成功/失敗件数でも出力変化を確認できる。成功出力の全文を調べる場合はartifactの`contents/<fingerprint>.txt`を参照する。生ログの保存先や日時はsnapshotには含めない。
