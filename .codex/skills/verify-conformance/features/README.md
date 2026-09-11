# Conformance verification features

| Feature | Purpose |
| --- | --- |
| [Commit snapshot](snapshot.md) | 指定commitのsuite全体を実行し、結果と生成内容を保存 |
| [Regression comparison](compare.md) | commit間/A-Aのstatus・内容・coverageの変化を検出 |
| [Focused reproduction](focused.md) | 失敗したファイル/設定/検証を絞って再現 |

全体検証を依頼されたとき、focused実行だけで完了としない。終了コードの意味はSKILL.mdを参照。

Gitレビュー用の書き出し・照合は[SKILL.mdのGit snapshot手順](../SKILL.md#gitでレビューするsnapshot)を参照。
