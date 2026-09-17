# Daily comparison

凍結した二つの版を、順序を均等化・ランダム化した独立processで比較し、A/A controlと失敗attemptも保存する。

## 確認項目

- prepare: refs/source、harness overlay、binary、planを凍結。
- run: dailyの4ペアとA/Aを実行。
- resume: 完了済みrunの再実行が重複標本を作らない。

## 対象コマンド

`prepare` → `run --lane daily`。既存のnative-pointer-store planを用いる。

## 実行手順

実行前に確認する条件: launch済み。既存artifactをinspect済み。他のbuild・測定を止め、必要な空きメモリを確認する。

```sh
"$CTL" drive "$OUT" prepare prepare --repo "$PWD" --before HEAD --after HEAD --plan "$PWD/tsc/internal/astbench/examples/native-pointer-store.json" --out "$OUT/campaigns" --run-id native
"$CTL" drive "$OUT" daily run --run "$OUT/campaigns/native" --lane daily
"$CTL" drive "$OUT" daily-resume run --run "$OUT/campaigns/native" --lane daily
```

prepare後にidentity.json、plan.json、snapshot binaryとbuildログを読む。run前後のattempt一覧を保存し、完了済みresumeで件数が増えないことを確認。collectでcomplete/validを確認するまで完了判定しない。

## 注意点

上記は同じcommitのnative legacy pointer対storeで、二つのproduction parser比較ではない。HEADはdirtyなAST変更を含まない。layout変更前後を測る場合は目的に合うbefore refと`--after-working-tree`、同一表現のplanを明示し、結果を混ぜない。共通harness overlayもidentityで確認する。失敗時は新しいdrive labelを使い、凍結planや過去attemptを編集しない。SIGINT時の中断記録を保ち、doctor後に同じrunをresumeする。
