# Artifact inspection and analysis

利用者が既存証拠の対応範囲を確認し、測定processを起動せずに生データから比較結果を再生成する。

## Sub-features

- inspect: identityと要求benchmarkの有無。
- collect: 完了・同値性・標本の検査。
- collect-out: 別ディレクトリへのbenchstat等の再生成。
- report: 指標と限界の人向け表示。

## How to get to it (user POV)

CLIのinspect/collect/report。collect --outは派生ファイルを作るがrawを書き換えない。

## Driving it with control-ast-walk

Preconditions: launch済み。inspectは計測前に既存artifact rootへ行い、以下ではOUT内のcampaignを対象とする。collect以降はdaily featureのnative campaignが必要。benchstatをPATHに置く。

```sh
"$CTL" drive "$OUT" inspect inspect --repo "$PWD" --artifacts "$OUT/campaigns" --bench BenchmarkTraversal
"$CTL" drive "$OUT" collect collect --run "$OUT/campaigns/native"
"$CTL" drive "$OUT" reanalysis collect --run "$OUT/campaigns/native" --out "$OUT/reanalysis"
"$CTL" drive "$OUT" report report --run "$OUT/campaigns/native"
```

inspectのstatusを読む。collectのcomplete/valid=true、reanalysisのbefore.txt/after.txt/benchstat.txt/paired.json、report stdoutを確認。rawファイルのhashを再集計前後で比較し、rawを変更していないことを検証する。collectorのexit 0を全標本のvalidと同一視しない。

## Gotchas

missingをunsupportedと呼ばない。repo/revision一致だけでdirty sourceまで一致したと断定しない。4ペアではbenchstatの95%中央値CIが有限にならないことがある。benchstat不在時のraw生成成功を比較成功としない。保存済みbinaryもcollectorが照合するためcleanupでcampaignを削除しない。
