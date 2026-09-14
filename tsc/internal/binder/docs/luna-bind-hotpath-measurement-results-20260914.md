# Luna bind hotpath 5条件実測結果 (2026-09-14)

## 結論

固定した B0 / S / L / N / LR を、指定driver・固定10round・各条件新規processで実測した。通常GCの測定suite幾何平均は LR/B0 が **+2.58%**、GC無効は **+2.38%**（いずれも退行方向）だった。A/Aのばらつきは3%改善を判定できる水準ではない。従って、wall結果だけからbinder最適化の採用や3%改善を宣言しない。

通常GCの内部差分は S/B0 `+4.72%`、L/S `-7.21%`、N/L `-1.83%`、LR/N `+7.54%` で、段階ごとの効果も安定しない。GC無効では S/B0 `-2.78%`、L/S `+3.84%`、N/L `-0.23%`、LR/N `+1.65%` となり、GC条件で方向が変わる。

## 問題と仮説

問題は、Identifier入口のdispatch短縮と、QualifiedName.Rightの親再判定省略がbinder wall costを実際に下げるか未判定だったこと。仮説は、LがIdentifier入口の共通経路を短縮し、LRが既知の名前位置で `isIdentifierNameRef` の再判定を省くことでCPU費用を下げる、というもの。

広いhot pathは walker/helpers、今回の狭い対象はIdentifier入口とQualifiedName.Rightである。割当driverはSymbol/Arena/Flow等であり、今回の候補による割当削減は主張しない。

## selected repo とartifact status

- selected repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`
- selected `repo_root`: 同上
- selected `typescript_go_git_rev`: `fee59d5c84c167172f82ddc190182f4873ca1ed1`
- selected `tsgolint_git_rev`: `null`（未使用）
- 既存artifact: [`/private/tmp/bind-hotpath-luna-20260914-175901`](/private/tmp/bind-hotpath-luna-20260914-175901) — **stale**。記録されたsource revision `1be4c8a1e64bda712843fe6d940fed3a44e7be78` がselected repoと異なるため、厳密な `current` ではない。
- 今回artifact: [`/private/tmp/bind-hotpath-luna-20260914-225839`](/private/tmp/bind-hotpath-luna-20260914-225839) — 固定候補のsource/binary/fixture hashは検証済み。ただし上記repo revision規則では provenance status は **stale** とする。variant hashを実物照合せずcurrentとは呼んでいない。
- 既存wall raw: `driver-results/`なし、requested Benchmark行なし — **missing**。
- `inst/op`, `cycles/op`, parse+bind、PMU/kperf/Instruments — **missing**。objdumpの静的命令列を動的指標の代用にしていない。
- candidate clones: なし（pointer比較は今回対象外）。作業ツリーは既存変更を含むdirty状態で、測定中にrestore/commit/pushしていない。状態記録は [`worktree-status.txt`](/private/tmp/bind-hotpath-luna-20260914-225839/worktree-status.txt)。

Nの既存結果文書のbinder.go SHAは、測定前に `1ec506...` から実物の `0f6555f570cd25b8700ba54239becc4b584e6c6a8c7164b15c4ba786733faf4f` へ修正した。LRの実物は `1ec506...` である。[修正済み既存結果文書](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/internal/binder/docs/luna-bind-hotpath-experiment-results-20260914.md)

## 5条件の凍結と事前検証

B0/S/Lは同一の旧QualifiedName walker、N/LRは同一の名前helper経路だった。N→LRのbinder.go差分は `bindIdentifierEffects(ref, false)` から `bindIdentifierEffects(ref, true)` への1箇所だけである。検証差分は [`n-lr-binder.diff`](/private/tmp/bind-hotpath-luna-20260914-225839/n-lr-binder.diff)、全事前検証は [`preflight.txt`](/private/tmp/bind-hotpath-luna-20260914-225839/preflight.txt) に保存した。

| variant | binder.go SHA256 | bindwalk_generated.go SHA256 | perf-binder.test SHA256 |
|---|---|---|---|
| B0 | `9b2dce1b517a45ab43d63432b381dbe50db21e2c04a77d69183de57d2161cbfd` | `20de8415aaa55cf60c715f003005283faf1daaa3e8fc89e87b2541e41ca84707` | `7c2fb956c084b2a430c298fb40a43b0edc251de37125cb5ab86db4854fd72ae3` |
| S | `c3add5fe2462cc756760394260b3b32bda6c7711e143ce71267cc7701525c776` | `20de8415aaa55cf60c715f003005283faf1daaa3e8fc89e87b2541e41ca84707` | `36a31a950f09850cf2d98cc673bb5d34afd588d81ffe71bc764aebd6295422a0` |
| L | `cdb4143aa77502da69d9e90f58abe32a85bd4daa5c1bdc70ad49db4a9b1a2093` | `20de8415aaa55cf60c715f003005283faf1daaa3e8fc89e87b2541e41ca84707` | `3bd9dc1d03619a0c88f44545bfc6716b2c764d05751adcd656a6e9f61deace67` |
| N | `0f6555f570cd25b8700ba54239becc4b584e6c6a8c7164b15c4ba786733faf4f` | `4c1bb1570b19a5137a049cfc2c311302a4ad5161d8422d0e2d339c1130fd4dac` | `140e753bf4190f68aef11dd000301637b71a1dc81bb0d02e5fbae20920da4a45` |
| LR | `1ec50646f8f60fffd62ab34e3a8810b28b3a17a14acf6f868fa21c9b996f6eab` | `4c1bb1570b19a5137a049cfc2c311302a4ad5161d8422d0e2d339c1130fd4dac` | `bdb1bd79e9b7a6ffeab3d0a7719ef77c2d7daf50d56dbe4b6d686b3598d70b84` |

全20 fixtureの存在とmanifest SHAを照合した。特に `checker.ts` は `4fb2f7e7d898a1729a24b9ad2507b697b747bc8c1315f27cd7e72861115a83e9`、`dom.generated.d.ts` は `056acdb4168b9fde08093fa6917ff5e26cfe067c003c0a4c55d05a8a94d54b1c` で一致した。性能sourceには監査hookがなく、性能binaryにも `binderaudit`、`TestInvestigationAudit`、`InvestigationCounts` 等のsymbolはなかった。

binary metadataは5条件で共通して Go `go1.26.0`、darwin/arm64、`CGO_ENABLED=1`、`-tags=binderinvestigation`、gc compiler、通常のexe buildだった。`-N -l`、`-B`、strip、PGOは使用していない。Nodeは `v24.3.0`。測定環境は全て `GOMAXPROCS=2`、通常GC `GOGC=100 GOMEMLIMIT=2GiB BINDER_BENCH_GC_OFF=0`、GC無効 `GOGC=100 GOMEMLIMIT=off BINDER_BENCH_GC_OFF=1`。

## 再現手順と実行結果

driverは [`run-bind-investigation.sh`](/private/tmp/bind-hotpath-luna-20260914-225839/run-bind-investigation.sh)。既定の順序、固定10round、各条件の新規process、各fixture `-test.benchtime=10x`、各roundの独立B0 A/Aを維持した。参照binaryは `perf-binder.test` のみで、driverは今回1回だけ実行した。

```sh
cd /private/tmp/bind-hotpath-luna-20260914-225839
sh ./run-bind-investigation.sh
```

実行は 2026-09-14 23:05:36〜23:10:09 JST、exit 0。rawは [`driver-results/`](/private/tmp/bind-hotpath-luna-20260914-225839/driver-results) に保存し、110 process結果・2,200 benchmark行・各raw 20 fixture・全raw `PASS` を確認した。[raw検証](/private/tmp/bind-hotpath-luna-20260914-225839/raw-validation.txt)

集約はvariant/GC条件ごとに10 process結果を連結した。process内の10bindを独立sampleにはしていない。[集約manifest](/private/tmp/bind-hotpath-luna-20260914-225839/aggregation-manifest.txt)

## benchstat結果

以下はfixture幾何平均の `ns/op`（表示はµs）と、candidate/baseの変化率。B/opとallocs/opの変化率は全比較で丸め上ほぼ0%で、実質的な割当削減は観測していない。各fixtureのbase値、candidate値、CI、変化率、benchstatのp値は [`benchstat-fixture-summary.tsv`](/private/tmp/bind-hotpath-luna-20260914-225839/benchstat-fixture-summary.tsv) に保存した。

| GC | 比較 | ns/op base→candidate | 変化 | B/op変化 | allocs/op変化 |
|---|---|---:|---:|---:|---:|
| 通常 | S/B0 | 110.1→115.3µs | +4.72% | +0.00% | +0.00% |
| 通常 | L/S | 115.3→107.0µs | -7.21% | -0.00% | -0.00% |
| 通常 | N/L | 107.0→105.1µs | -1.83% | +0.00% | +0.00% |
| 通常 | LR/N | 105.1→113.0µs | +7.54% | -0.00% | +0.00% |
| 通常 | LR/B0 | 110.1→113.0µs | +2.58% | -0.00% | +0.00% |
| GC無効 | S/B0 | 107.1→104.1µs | -2.78% | +0.00% | +0.00% |
| GC無効 | L/S | 104.1→108.1µs | +3.84% | -0.00% | -0.00% |
| GC無効 | N/L | 108.1→107.9µs | -0.23% | +0.00% | +0.00% |
| GC無効 | LR/N | 107.9→109.7µs | +1.65% | +0.00% | +0.00% |
| GC無効 | LR/B0 | 107.1→109.7µs | +2.38% | +0.00% | +0.00% |

benchstatの全出力（通常GC）: [S/B0](/private/tmp/bind-hotpath-luna-20260914-225839/benchstat/normal/S-over-B0.txt)、[L/S](/private/tmp/bind-hotpath-luna-20260914-225839/benchstat/normal/L-over-S.txt)、[N/L](/private/tmp/bind-hotpath-luna-20260914-225839/benchstat/normal/N-over-L.txt)、[LR/N](/private/tmp/bind-hotpath-luna-20260914-225839/benchstat/normal/LR-over-N.txt)、[LR/B0](/private/tmp/bind-hotpath-luna-20260914-225839/benchstat/normal/LR-over-B0.txt)。

benchstatの全出力（GC無効）: [S/B0](/private/tmp/bind-hotpath-luna-20260914-225839/benchstat/gc-off/S-over-B0.txt)、[L/S](/private/tmp/bind-hotpath-luna-20260914-225839/benchstat/gc-off/L-over-S.txt)、[N/L](/private/tmp/bind-hotpath-luna-20260914-225839/benchstat/gc-off/N-over-L.txt)、[LR/N](/private/tmp/bind-hotpath-luna-20260914-225839/benchstat/gc-off/LR-over-N.txt)、[LR/B0](/private/tmp/bind-hotpath-luna-20260914-225839/benchstat/gc-off/LR-over-B0.txt)。machine-readableな全比較値は [`benchstat-fixture-summary.tsv`](/private/tmp/bind-hotpath-luna-20260914-225839/benchstat-fixture-summary.tsv)、幾何平均は [`geomean-summary.tsv`](/private/tmp/bind-hotpath-luna-20260914-225839/geomean-summary.tsv)。

### checker.ts / dom.generated.d.ts と入力間の分岐

直接比較LR/B0では、通常GCの `checker.ts` は `-0.62%`、`dom.generated.d.ts` は `+0.87%`。GC無効ではそれぞれ `+1.57%`、`+0.25%` だった。いずれもwallのbenchstat判定は `~`（n=10）で、同等性の証明ではない。

通常GCのLR/B0は20入力中4入力が見かけ上改善（`checker.ts`, `mapCode.ts`, `api.ts`, `proto.generated.ts`）、16入力が退行方向。GC無効は7入力が改善、13入力が退行方向で、方向は入力とGC条件に依存する。特に `jsxComplexSignatureHasApplicabilityError.tsx` は通常GC `+9.94%`、GC無効 `+20.43%`、`exports.ts` は通常GC `+10.63%`に対してGC無効 `-6.23%`だった。全20入力の詳細値は上記TSVに隠さず保存した。

通常GCのLR/Nでは `jsx.tsx` に `+28.38% (p=0.045, n=10)` が出た。ただしこれは20入力×5比較×2 GC条件の未補正な多重比較の一つであり、測定状態の変化も含む探索的な差である。この単発結果から入力依存の退行は未確認とし、再現性検証なしには断定しない。

また、通常GCの `checker.ts` は全variantでround 1〜2の約16〜18msから、round 5〜10の約52〜61msへ移った。round 3は遷移的で、round 4はB0 88.3ms、S 96.0ms、L 60.2ms、N 39.3ms、LR 467.2ms（LRが突出）だった。全variantに共通する水準変化とLRの単発外れ値は、単なるvariant差ではなく、CPU周波数/熱・scheduler/OS負荷など測定状態が途中で変化した可能性を示すが、現証拠では原因未確定である。時系列は [`checker-timeseries-normal.tsv`](/private/tmp/bind-hotpath-luna-20260914-225839/checker-timeseries-normal.tsv) に保存した。

## A/A精度評価

通常GCの独立B0 A/Aは各10 process。benchstatのA/A幾何平均は `110.1→109.4µs`, `-0.63%`、B/opは `-0.00%`、allocs/opは `+0.00%`だった。[A/A benchstat](/private/tmp/bind-hotpath-luna-20260914-225839/benchstat/aa/B0-AA-over-B0.txt)

しかし、A/Aの各fixture wall CI `±59〜74%` は各群の中央値のCIであり、A/A比のCIではない。各roundの20fixture-geomean比は `-7.84%〜+13.12%` に散った。round対応したwhole-suiteの `log(AA/B0)` から算出した近似95%区間は、幾何平均比 `+0.93%`、`[-3.95%, +6.06%]` だった（n=10）。これは各群CIとは別の、round対応A/A比の区間であるが、時系列の非定常性を含むため記述的な近似に留まる。[A/A round値](/private/tmp/bind-hotpath-luna-20260914-225839/aa-whole-binder.tsv) [A/A比CI](/private/tmp/bind-hotpath-luna-20260914-225839/aa-whole-binder-ci.txt) [A/A fixture精度](/private/tmp/bind-hotpath-luna-20260914-225839/aa-precision.tsv)

よって、観測A/A差 `-0.63%` や近似区間を同等性の証明とは扱わない。区間は0を跨ぎ、3%改善を検出できる幅でもない。今回の固定10roundでは、目標のbinder全体3%改善を判定する精度を確認できておらず、最終結論は留保する。

## 診断、提案、受入基準、次のアクション

診断は「候補経路差はhashで固定され、wall測定も完走したが、通常GC中に全variant共通の水準変化とround 4 LRの467ms外れ値があり、測定状態の安定性が未確認。GC条件間・入力間の方向も分かれ、A/A比の推定精度は3%目標に届かず、binder全体3%改善を裏付ける証拠はない」。原因はCPU周波数/熱、scheduler/OS負荷、その他の環境要因を含め未確定である。B/opとallocs/opが変わらないため、wall差を割当削減の効果とは解釈しない。parseはtimer外なのでparse+bind非退行もまだ言えない。

提案修正は今回追加しない。次の受入基準は、(1) selected repoとcandidate source/binaryのrevision・variant hashを同一カードで固定、(2) 意味一致、(3) 通常GCの実workloadでbinder全体3%以上をA/A精度内で確認、(4) parse+bind非退行、(5) `inst/op` と `cycles/op` を別途測定、(6) `checker.ts` と `dom.generated.d.ts` を含む入力間の退行を説明できること。wall改善だけでは採用しない。

次のアクションは、同じdriverでの再測定より先に、(1) checker.tsのround時系列とround 4 LR外れ値の原因調査、(2) CPU周波数/熱状態、scheduler/OS負荷、バックグラウンド負荷など測定環境の安定性確認、(3) 安定性を確認できる再現条件と停止基準の確立、を行うこと。その後、必要なら候補sourceをselected repo revisionへ再構築してstaleを解消し、同じ固定driverで再測定する。parse+bindとPMU/kperfはさらに別段階で実施する。今回、PMU、Instruments、新しい最適化、commit、pushは行っていない。
