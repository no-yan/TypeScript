# Isolated walk sample

構築済みASTのrootからedgeを辿り、型チェックを除いた走査の時間とallocation/GCを観測する。

## 確認項目

- pointer-sample / store-sample: 同じcaseの単発測定。

## 対象コマンド

astbench CLIの`sample --config`。

## 実行手順

実行前に確認する条件: launchとequivalenceが成功。同じ入力・visitorを使う。

```sh
"$CTL" drive "$OUT" sample-pointer sample --config '{"case":"expression","shape":"mixed","nodes":256,"seed":1,"layout_seed":1,"layout":"construction","representation":"pointer","batch":256}'
"$CTL" drive "$OUT" sample-store sample --config '{"case":"expression","shape":"mixed","nodes":256,"seed":1,"layout_seed":1,"layout":"construction","representation":"store","batch":256}'
```

各exit=0、valid=true、ns_per_op>0、visits>0、bytes_per_op/allocs_per_op/allocated_bytes/allocations/gc_cycles=0を確認。visits/checksumが両版で同じことを読む。失敗値も保存する。

## 注意点

2回の数値を目視比較して高速化と結論しない。統計比較はdailyへ。AST構築と保持メモリはsampleのB/opに含まれない。総node数とexpression visitorのvisitsは異なり得る。
