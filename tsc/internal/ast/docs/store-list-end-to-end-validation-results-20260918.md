# Store tree list fast path: end-to-end 予備検証

作成日: 2026-09-18。結論: **parser生成Store ASTだけを歩くstageでは -4.88%の改善を実証したが、VS Code Monacoのparse/bind end-to-end wall time差は未解決だった。** warmup後のbalanced runは baseline 594.4 ms、candidate 597.4 ms、benchstat p=0.932、n=12/arm。これは同等性や退行の証明ではない。最大RSSも 299.2 MiB 対 298.9 MiB、p=0.811で未解決だった。pprofは取得・使用していない。

## 1. 対象とartifact status

| 項目 | 値 |
|---|---|
| selected TypeScript-Go repo | `/Volumes/SanDisk1TB/worktree/ast-traversal-bench-design` |
| baseline / candidate | `3b1ea86b74` / `613a54b22f8` |
| TSGolint revision | null。今回の対象にTSGolint checkoutはない |
| workload repo | `/Volumes/SanDisk1TB/ghq/github.com/microsoft/vscode`、`22f76d2e5f` |
| workload | `src/tsconfig.monaco.json`、SHA-256 `c8d3026d…0bf5f8` |
| host | macOS 26.6.2、darwin/arm64、Apple M1、Go 1.25.0 |

TypeScript-Goの候補cloneは `cursor-ast-store-tests`、`binder-rewrite`、`profile`、`flownode`、`lock-design-inv`、`lock-profile`、`store-pr-7`、`store-pr-7-nodeseq-t10`、`store-pr-7-attach-parent-fix`、`store-redesign`、`store-schema-foreach-child`、`store-nolock-exp`、`nolock97`、`putcol-bce`。結果は混ぜていない。

workload checkoutには `pnpm-lock.yaml`、`pnpm-workspace.yaml`、`profile/`、`trace.out/` のuntracked entryがある。tracked diffは空で、両binaryは同じfilesystem snapshotを読んだ。この状態をidentityへ保存した。

| artifact set | status | 根拠 |
|---|---|---|
| `tsc/before.txt` | stale | revisionとdirty source hashがなく不使用 |
| parsed `checker.ts` Store walk | current | clean baseline/candidate、source/binary hash付き。-4.88%、p<0.001 |
| Monaco pilot | current・補助 | 最初のbaselineにcold outlier。除外せずpilotとして保存 |
| Monaco warm balanced | current・主評価 | identityと一致し、明示warmup後の独立process rawを保存 |
| VS Code全体 smoke | current・適格性確認のみ | 1回/armでI/O変動が大きく、効果量の証拠にはしない |
| end-to-end allocation count | missing | CLI processのB/op・allocs/opは収集していない |
| pprof | unsupported | ユーザー指定により実験手段から除外 |

identityは [identity.json](artifacts/store-list-end-to-end-validation-20260918/identity.json)、pilotと主評価のraw・benchstat・runnerは [artifact directory](artifacts/store-list-end-to-end-validation-20260918/) に保存した。

## 2. 測定方法

clean detached worktreeからbaseline/candidateの`tsgo`をビルドした。Monacoは `--noEmit --noCheck --declaration false --checkers 1` とし、parseとbindを含む一方でcheckerを除いた。`GOMAXPROCS=1`、`GOMEMLIMIT=2GiB`を固定した。

pilotで最初のbaselineだけ1.039秒となり、残りは概ね0.56〜0.65秒だった。この標本を事後除外せずpilotへ残し、両binaryを1回ずつ明示warmupしてから新しいblockを開始した。主評価は12個の独立process pairをAB/BA交互に各6組。その後、同じcandidate binaryをfirst/second位置で12 pair測った。Python runnerは高精度のelapsed timeと`wait4`のuser/system time・最大RSS・終了状態・出力hashを記録した。

## 3. 主結果

[warm/benchstat-balanced.txt](artifacts/store-list-end-to-end-validation-20260918/warm/benchstat-balanced.txt):

| 指標 | baseline | candidate | 差の判定 |
|---|---:|---:|---|
| wall time | 594.4 ms | 597.4 ms | unresolved、p=0.932 |
| user time | 509.6 ms | 512.4 ms | unresolved、p=0.932 |
| system time | 82.21 ms | 79.96 ms | unresolved、p=0.977 |
| maximum RSS | 299.2 MiB | 298.9 MiB | unresolved、p=0.811 |

ABだけではwall 597.4 ms対607.8 ms、p=0.485。BAだけでは594.4 ms対587.3 ms、p=0.485で、方向が反転した。12 pairの差も -10.87%〜+13.57%に広がった。同一binaryのA/A対照はfirst 594.1 ms、second 601.1 ms、p=0.178。全48 processはexit 0で、stdout/stderrのhashは全件同一だった。

このためMonaco end-to-endについて「改善」「同等」「退行」のいずれも実証していない。観測変動に対して対象stageの寄与が小さく、wall timeでは解像できないと判断する。最大RSSについても悪化なしを証明したのではなく、差が未解決である。

## 4. VS Code全体のsmoke

`src/tsconfig.json --noEmit --checkers 1` はbaseline/candidateともexit 1で、同一の既知diagnosticを1件出した。stdout SHA-256も一致した。単発wall timeは73.52秒対59.84秒だったが、sandbox内の大きなI/O変動、片順序、1 sample/armのため性能証拠に使わない。このセルを繰り返す費用は変更の小ささに釣り合わないため打ち切った。

## 5. 診断と採否

最も広いstageはMonaco parse/bindとVS Code全体だが、変更可能で直接対応する狭いstageは `ast.Walk → ForEachChild → forEachChildList` である。狭い実Store AST stageでは0 B/op・0 allocs/opを保ったまま -4.88%を実証した。end-to-endではその寄与が測定noiseより小さく、差は未解決だった。

allocation driverは今回のCLI計測では未取得。最大RSSはprocess全体を含み、list fast path固有のallocationを帰属できない。hot pathの関数順位も取得していない。pprofのCPU sample帰属は使わず、一要因介入とstage境界で判断した。

採否は前段と同じく **採用を支持** とする。変更は不要なloadを除く単純なread-only fast pathで、実入力の対象stageに再現可能な改善があり、Monacoで実証した退行はない。end-to-endの統計的改善を受入条件にはしない。より複雑なcache・表現変更なら、この程度の未解決結果では採用しない。

## 6. 次の行動

この候補への追加測定は止め、計画のE2「named childの親header/childStart再取得」を一要因microbenchmarkで調べる。listを含まないBinary/Paren treeを使い、small/large、balanced/chainを分ける。改善が出た場合だけparser生成実AST stageへ接続し、E1との累積end-to-endは複数変更が積み上がった時点で再測定する。
