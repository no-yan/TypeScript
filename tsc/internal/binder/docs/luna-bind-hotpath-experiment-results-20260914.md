# Luna bind hotpath 実験結果 (2026-09-14)

## 結論

B0 / S / L / N / LR の5条件を実装し、固定source、overlay、最適化binary、意味監査、寿命probe、逆アセンブル、後続測定driverを作成した。意味検証はB0基準で全20入力一致した。性能の採否は未判定である。長時間benchmark、PMU/kperf、Instruments記録は実行していない。

## カード0〜10の実施記録

- selected repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`
- selected HEAD: `1be4c8a1e64bda712843fe6d940fed3a44e7be78`
- candidate clones: なし（今回pointer比較なし）
- `typescript_go_git_rev`: `1be4c8a1e64bda712843fe6d940fed3a44e7be78`
- `tsgolint_git_rev`: `null` / 未使用
- Go: `go1.26.0 darwin/arm64`; Node: `v24.3.0`
- 既存変更はGit stashがindex.lock制約で作成できなかったため、tracked diffと未追跡文書をOUTへ保存した。snapshot: [`/private/tmp/bind-hotpath-luna-20260914-175901`](/private/tmp/bind-hotpath-luna-20260914-175901)
- OUTの各`variants/<name>/perf/`が性能用の凍結sourceで、`perf-overlay.json`が対応overlayである。各`perf/source.sha256`は実binary作成前後で一致した。監査用sourceは別の`audit-overlay.json`にのみ使用し、性能binaryには混入させていない。

variant source SHA256（binder.go / bindwalk_generated.go）:

| variant | binder.go | bindwalk_generated.go |
|---|---|---|
| B0 | `9b2dce1b517a45ab43d63432b381dbe50db21e2c04a77d69183de57d2161cbfd` | `20de8415aaa55cf60c715f003005283faf1daaa3e8fc89e87b2541e41ca84707` |
| S | `c3add5fe2462cc756760394260b3b32bda6c7711e143ce71267cc7701525c776` | `20de8415aaa55cf60c715f003005283faf1daaa3e8fc89e87b2541e41ca84707` |
| L | `cdb4143aa77502da69d9e90f58abe32a85bd4daa5c1bdc70ad49db4a9b1a2093` | `20de8415aaa55cf60c715f003005283faf1daaa3e8fc89e87b2541e41ca84707` |
| N | `0f6555f570cd25b8700ba54239becc4b584e6c6a8c7164b15c4ba786733faf4f` | `4c1bb1570b19a5137a049cfc2c311302a4ad5161d8422d0e2d339c1130fd4dac` |
| LR | `1ec50646f8f60fffd62ab34e3a8810b28b3a17a14acf6f868fa21c9b996f6eab` | `4c1bb1570b19a5137a049cfc2c311302a4ad5161d8422d0e2d339c1130fd4dac` |

### 実装差分

- S: `bindIdentifierEffects(ref, knownName)`とcontextual identifier共通coreを追加。B0の呼出し経路・生成walkerは維持。
- L: `bindChildRef`でIdentifierを入口dispatchし、`finishIdentifierBinding`で既存のerror flag/`seenParseError` epilogueを保持。
- N: Lの同じ呼出し経路でQualifiedName.Rightを`bindIdentifierNameChildRef(..., false)`へ接続。
- LR: Nの`false`だけを`true`へ変更。共通coreはdiagnostics、FlagsAt、Ambient/JSDoc確認を常に実行し、その後にknownName判定を行う。生成器はQualifiedName.Rightの明示predicateだけを生成し、他のchild/listは変更していない。
- PrivateIdentifier、This/Super、Ambient/JSDoc、diagnostic順、Flow、子走査順、その他の名前位置は旧経路を維持。

### 意味検証

`TestInvestigationAudit`をB0/S/L/N/LRで同一manifestに対して実行し、diagnostic code/位置/順序、symbol digest、node-symbol digest、syntax digest、正規化visit trace、全node flags、node→Flow対応、Flow graph、到達symbol数を比較した。

| variant | 不一致数 |
|---|---:|
| S | 0 / 20 |
| L | 0 / 20 |
| N | 0 / 20 |
| LR | 0 / 20 |

`TestInvestigationBatchLifetime`も5条件・全fixtureで成功した。distinct/unbound→bound、最大batch 10、Store登録数の復帰、二重cleanupを確認した。監査用hook/testは`binderinvestigation && binderaudit`専用である。

L/N/LRでは追加の`TestInvestigationIdentifierFinishFourStates`を実行し、初期`seenParseError` false/true × `ThisNodeHasError`無/有の4条件でaggregate flagとseenParseErrorを確認した。N/LRでは`TestInvestigationIdentifierNameChildBoundaries`によりnilと非Identifierを確認し、KnownName経路では旧`isIdentifierNameRef`が成立することをassertした。

### 生成・ビルド・逆アセンブル

生成コマンドを2回実行し、`bindwalk_generated.go`の2回目hashは不変だった。dprintのnpm取得はネットワーク不可でskipされたため、無関係な生成物は変更対象外として戻した。

全binaryは同じ最適化条件で `GOMEMLIMIT=2GiB GOMAXPROCS=2` を設定して作成した。`-N -l`、`-B`、strip、PGOは未使用。

| variant | bindKind text | bindChildRef text | Identifier helper | bindIdentifierNameChildRef | walker text |
|---|---:|---:|---|---:|---:|
| B0 | 4128 B | 144 B | なし | なし | 19696 B |
| S | 4112 B | 144 B | 128 B | なし | 19696 B |
| L | 4112 B | 208 B | 128 B | なし | 19696 B |
| N | 4112 B | 208 B | 128 B | 208 B | 19696 B |
| LR | 4112 B | 208 B | 128 B | 208 B | 19696 B |

`go tool objdump`全文は各variantの`perf-objdump.txt`、symbol/text sizeは`perf-symbol-sizes.txt`に保存した。frame prologue、CALL、比較分岐は同ファイルで確認可能である。driverと同じ`perf-binder.test`を対象にしており、これは静的機械語の比較であって動的inst/opやcycles/opを意味しない。

### 測定driver

[`run-bind-investigation.sh`](/private/tmp/bind-hotpath-luna-20260914-175901/run-bind-investigation.sh) を準備した。固定10round、各variant新規process、roundごとの順序rotate/reverse、B0の独立A/A、通常GC/GC無効の別raw出力、`-benchtime=10x`、Benchmark行存在確認を含む。通常GCは`GOGC=100 GOMEMLIMIT=2GiB`、GC無効は`GOGC=100 GOMEMLIMIT=off`を明示する。driverは今回実行していない。benchstat比較も未実行である。

manifestは実在する20 fixtureをSHA256固定し、`manifest.json`に保存した。checker.ts、dom.generated.d.tsを含み、欠落fixtureをskipしない。

## 未測定・未解決事項

以下は未測定（artifact status: `missing`）であり、成功・高速化とは呼ばない。

- `ns/op`: missing
- `B/op`: missing
- `allocs/op`: missing
- `inst/op`: missing（objdumpの静的命令列は代替しない）
- `cycles/op`: missing
- PMU/kperf/Instruments: missing、管理者実行なし
- binder全体通常GC 3%以上、parse+bind非退行: 未判定
- candidate clone/pointer比較: 今回対象外

性能binaryとsource対応は、全5条件について`perf/source.sha256`と`perf-binder.test`のビルド入力を再確認済みである。監査hookを含むsourceは`audit.test`専用であり、性能binaryの再ビルドには使っていない。既存の別revision artifactは今回のHEADと一致しないため、採用根拠にはせず`stale`扱いとする。今回の狭い対象はIdentifier入口とQualifiedName.Right、広いhot pathはwalk/helpersである。割当driverはSymbol/Arena/Flow等であり、今回の実装から割当削減は主張しない。

次のアクションは、安定した環境でprepared driverを実行し、rawを条件別保存してbenchstatでB0/S、S/L、L/N、N/LR、LR/B0を比較すること。C以降、parse変更、PMU計測は開始していない。
