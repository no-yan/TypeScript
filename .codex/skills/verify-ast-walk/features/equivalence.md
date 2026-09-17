# Traversal equivalence

性能比較の前に、native pointerとstoreが同じ訪問順・属性値で木を辿ることを利用者が確認する。

## Sub-features

- verify-trace: 完全なtraceで表現間の一致を検査。

## How to get to it (user POV)

astbench CLIの`verify --config`。JSONファイルまたはinline JSONを渡す。

## Driving it with control-ast-walk

Preconditions: SKILL.mdのlaunch済み、CTL/OUT設定済み。

```sh
"$CTL" drive "$OUT" equivalence verify --config '{"case":"expression","shape":"mixed","nodes":256,"seed":1,"layout_seed":1,"layout":"construction","representation":"store","batch":1}'
```

exit.txt=0、stdout JSONのvalid=true、entries>0、store_trace/pointer_traceが一致することを読む。これは軽いchecksumだけの比較ではない。両traceを証拠として保持する。

## Gotchas

fixtureはstore-onlyで、このpointer同値性検証の代わりにしない。verifyは時間測定ではない。合格してもASTレイアウトの速さ・top 10代表性を証明しない。
