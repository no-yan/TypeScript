# 85506e8 と最新コミットの性能を比較する計画

2026-09-15。成果物は計測・原因分析・改善候補の検証計画。ユーザーの開始指示を受け、固定snapshotと測定環境の準備を開始した。実行結果は別レポートに保存する。

実行後、ユーザーの停止指示で追加の改善計測を終了した。[結果と未実施範囲](commit-performance-results-20260915.md)を参照。主A/Bのwall・KPCは完了し、下記の未実施項目を完了扱いにはしていない。
主比較は同じStore実装系列のBinderとし、回帰が確認された場合は原因を切り分ける。
最適化候補の実装・採用とPR作成は、この計画の作成には含めない。

## 比較対象を固定する

- [x] 選択repoを `/Volumes/SanDisk1TB/worktree/binder-rewrite` と確認した。調査開始時の作業木はclean。
- [x] Aを `85506e8b7d51f2b05f84f9ba1a8c3309b2f30ce4` とする。
- [x] Bを調査開始時のローカルHEAD `80d8b41ccfb046a551b721cb04f909205d66513a` とする。「最新」はこのブランチのHEADを指す。リモートmainへの置換や、実行中のHEAD更新はしない。
- [x] 中間Mは `6f6f3ae597fdd73bb3a28b172fe8b647a2694bb8`。A→Mが基盤移行、M→Bが引数列の直接走査への変更。
- [x] 計測開始時にA/B/Mの存在とA→M→Bの親子関係を再確認した。HEADはBのまま。未追跡の本計画書だけがあり、実装のdirty変更はない。
- [ ] A/Bそれぞれの独立したsource snapshotを作る。fixture、Go/CGO、build tag、GC条件、benchmark sourceを一致させ、ソース・バイナリ・driverのSHA256を保存する。
- [ ] `identity.json` に選択repo_root、`tsgolint_git_rev=null`、`typescript_go_git_rev=B`、before=A、after=B、build用rootとsource hashを分けて保存する。TSGolintは測らない。

候補checkoutは `git worktree list --porcelain` で確認した。Aと同じHEADの `cursor-ast-store-tests`、pointer系main `8ac035a394`、flownode、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜7系、store-redesign、store-schema-foreach-child等が存在する。主比較は上記選択repoから作る固定snapshotだけを使う。実行時の全候補一覧は `worktrees.txt` に保存する。

## 既存artifactを先に判定する

以下のidentityと保存済みbenchstatを確認した。旧レポートの当時の `current` 表記を、今回の判定へ引き継がない。

| Artifact set | 今回のstatus | 用途 |
| --- | --- | --- |
| `/Volumes/SanDisk1TB/Library/Caches/binder-foundation-20260915-164851/performance` | `stale` | revision欄はA、afterは移行時のdirty snapshot。仮説とdriverの参照 |
| `/Volumes/SanDisk1TB/Library/Caches/binder-narrowable-20260915` | `stale` | revision欄はM、afterは1行overlay。仮説とdriverの参照 |
| A対Bを直接比較する新規wall/KPC | `missing` | 本計画で取得する |
| A/Bの関数別Instruments trace、parse+bind、live/scan | `missing` | 必要な段階で取得する |
| 以前のwalk/helper CPU帰属 | `stale` | 最新のCPU構成比として使わない |

- [ ] 再利用時はrepo_rootと両revision欄が選択checkoutに一致し、source・条件も一致するものだけを `current` とする。
- [ ] artifactが存在しても、要求regexに一致する `Benchmark...` 行がstageの `bench.txt` にない場合は、そのstageを `unsupported` とする。未実行でartifactがない場合の `missing` と区別する。
- [ ] `complete` と精度の合否はstatusとは別に保存する。途中失敗を比較完了と扱わない。

### 過去の数値を仮説の根拠として保持する

wall列は通常GC wallの主比較中央値、EL0命令数列はKPC GC無効stageの主比較のbenchstat差。時間のA/A精度未達を含むため、今回の性能値として引用しない。

| 旧比較・入力 | ns/op 前→後 | B/op 前→後 | allocs/op 前→後 | EL0命令数のbenchstat差 |
| --- | ---: | ---: | ---: | ---: |
| 基盤移行・checker.ts | 55,624,768.5 → 68,874,871 | 12,798,208 → 12,994,156 | 14,163 → 24,065 | +23.31% |
| 基盤移行・dom | 20,312,250 → 24,569,241.5 | 7,867,448.5 → 7,867,441.5 | 16,682 → 16,682 | +20.90% |
| 引数列修正・checker.ts | 18,121,606.5 → 17,757,948 | 12,994,156 → 12,777,516 | 24,065 → 14,165 | −1.78% |
| 引数列修正・dom | 6,674,877 → 6,633,958.5 | 7,867,441.5 → 7,867,441.5 | 16,682 → 16,682 | −0.10%、非有意 |

旧比較のwall絶対値はセッション間で大きく異なる。率の加減算・乗算や、別セッションのrawを合成したbenchstatでA対Bを推定しない。直接比較は現在 `missing`。
旧allocation診断は `hasNarrowableArgument → Handle.Arguments → NodeSeq.Slice` に9,900 objects/Bindを帰属した。Bには直接走査の修正があるため、同じ修正を再提案しない。残るFlow chunk、列拡張、Symbol/Localsも候補だが、現在の増分の原因とは未確定。

## 1. ハーネスと測定環境を検証する

- [ ] [KPC手順](kpc-measurement-workflow.md)から既存driverを新規runへコピーしてpathを更新する。共通 `binder_measurement.py` は未実装であり、実行可能な入口として扱わない。
- [ ] 新規保存先を `/Volumes/SanDisk1TB/Library/Caches/binder-85506e8-80d8b41-<run-id>/`、runtimeを `/private/tmp/binder-85506e8-80d8b41-<run-id>/` として、実際の絶対pathをmanifestに固定する。既存rawへ追記しない。
- [ ] 両版の `BatchLifetime` / `KPCBatch` テストを通す。10個の独立した未bind ASTを準備し、parseと明示GCを区間外へ置く。N=1校正とbatch終了でStore登録数が開始値に戻ることを確認する。
- [ ] `GOMAXPROCS=8`、`GOGC=100`、`GOMEMLIMIT=off` を固定する。CPU、OS、Go、CGO、benchstat、電源条件、温度・メモリ圧迫と測定中の競合負荷を記録する。
- [ ] wall・KPC・Instruments・buildを直列実行する。並行agentは解析だけを担当し、同じhostで計測やbuildを起動しない。
- [ ] 現hostのevent DBと保存設定を照合する。KPCは両版各2 fresh processで自己検証し、設定後のfresh pthreadで計数する。read失敗・逆行・readback不一致は失敗として残す。

この段階の証拠は `identity.json`、`protocol.md`、source/binary hashes、`selftest/`、環境ログ。準備のテストを性能標本へ混ぜない。

## 2. A対Bを同一セッションで直接測る

- [ ] 初期入力を同じ内容の `checker.ts` と `dom.generated.d.ts` に固定する。pathとhashをmanifestへ保存する。
- [ ] 通常GC wall、batchだけGC無効のwall、batchだけGC無効のKPCを別stageで測る。各stage・各入力で6固定round、各roundはA_a/B_a/A_b/B_bの4 processとする。
- [ ] 実行順は最初の4roundで4通りに回転し、残り2roundは元の順序と逆順にする。1 sampleはfresh processで10 bind。2入力×3stage×6round×4版区分の144 benchmark行を期待する。
- [ ] A_a対B_aを主比較、A_b対B_bを確認用にする。A_a対A_b、B_a対B_bを同一版のA/Aとし、主・確認用を12標本に合算しない。
- [ ] rawを保存し、各stageで `benchstat old.txt new.txt` を実行する。名前、単位、10 iteration、標本数、round対応、欠落・重複・失敗を先に検査する。
- [ ] 同roundの対数比を20,000回paired bootstrapし、seedを固定する。A/Aの95% CI全体が命令数±1%、wall/cycles±1.5%に収まるかを個別に判定する。
- [ ] 指標別に判定する。命令数は精度合格、主・確認用の両方で1%以上増加かつbenchstat p<.05なら回帰として原因分析へ進む。wallは精度合格、両比較で3%以上増加かつp<.05を時間回帰の基準とする。これ未満の差も数値とCIを残す。
- [ ] 精度未達なら「未確定」とする。wall精度未達を理由に、有効な命令数の調査を止めない。固定round終了後に標本を追加せず、負荷条件の見直しは新規protocolに分ける。

時間は通常wallのns/op、割り当てはB/op・allocs/opを報告する。KPCはEL0 inst/op・cycles/opを主指標とし、分岐数・分岐ミス・L1D load missを補助指標とする。EL0+EL1固定counter、KPC下ns/op、別threadの仕事は混同しない。非有意差を同等性の証明としない。

## 3. 回帰があれば入力とコミットを切り分ける

- [ ] A/M、M/Bをそれぞれ独立した新規runで比較し、基盤移行と引数列修正を分離する。各pairに第2節と同じ6-round・4ラベル・主/確認/A/Aの設計を適用する。中間コミットは1個だけなので、広い履歴のbisectから始めない。
- [ ] 性能入力を6〜10個へ広げる。宣言、制御フロー、call/property、import/export、JavaScript/JSDoc、TSXを含む実ソースを既存fixtureから選び、測定前にpath・hash・規模・機能をmanifestへ固定する。小さい意味監査用snippetを実負荷の代わりにしない。
- [ ] 同じ入力でnode訪問数、child/listアクセス数、KindAt/FlagsAt/ParentRef、Handle化、FlowNode生成数を別の診断binaryで数える。inst/nodeと呼出回数の差で候補を絞る。
- [ ] A/BのInstruments CPU Profiler traceをchecker/domで取得する。parse準備をBindのCPU帰属に混ぜず、同じ採取区間と関数・inline情報を比較する。pprofは使わない。
- [ ] 割り当てに差が残る場合だけ、別実行でallocation stack差分を取る。同一PC stackのサイズ別bucketを合算する。診断自身の割り当てと、rate=1のbytesを通常B/opから分離する。
- [ ] 診断と訪問順・Symbol・Flow・AST保存先の監査差を先に分類する。基盤移行の意図した意味修正による仕事量と、同じ仕事の余分な実行を区別する。

この段階の完了条件は、入力ごとの回帰表、現在のhot path、allocation driver、優先仮説1件、その仮説を反証できる最小対照の記録。

## 4. 根拠がある改善候補を一つずつ検証する

| 観測する根拠 | 改善候補 | 反証する対照・注意点 |
| --- | --- | --- |
| 多入力でKindAt等の再取得とinst/nodeが増える | `bind(ref, kind)` と取得済みkindの局所伝搬 | baseline、引数だけ渡して再取得、渡して再利用。inline・CALL・spillを機械語でも確認 |
| Handle/queryとchild/list解決が支配する | Store ownerやspanを短い有効範囲で一度解決する | 共通helperへの接続費と再利用の効果を分離。所有権とwriter境界を維持 |
| 特定のslice生成がalloc増分を説明する | 読み取り専用列の直接走査 | 該当call-siteだけ変更し、早期終了・空列・順序を監査。Bの既存修正は維持 |
| `declareSymbolEx` / `GetSymbolTable` が支配し `declareModuleMember` が見える | Symbol表アクセスやexport宣言経路の再取得削減 | まずexport-heavy unique declarationの最小micro。symbol syntheticは方向確認だけ |
| Flow/列のbytesやscanが主な差を説明する | 容量予約・表現縮小 | bind以外へ費用を移していないかparse+bindとlive/scanで確認 |

優先仮説は広いwalk/helper境界の重複取得。A→Bの差分では `bindNode`、container kindの保持がなくなり、Bの `bindEachChild` などは `KindAt` を呼ぶ。ただし静的差分やKPC総量だけでは因果を確定できない。旧profileでも広いwalk/helpersがhotで、既知のslice経路より範囲が広い。現在のprofileが別の優先箇所を示せば、その証拠に従う。

- [ ] 最小microで機構を確認し、同じ候補を実BinderのA/B形式で再測定する。microの改善率を実入力へ外挿しない。
- [ ] 候補1件の検証が完了するまで次の変更を重ねない。安全検査を無条件で外す変更や、以前失敗したwalker分割の全面展開から始めない。
- [ ] ast/binder/checker/compiler test、意味監査、固定commitのconformance比較を行う。conformanceは `verify-conformance` skillに従い、診断・出力・type・symbol snapshotを保存し、baselineを更新しない。
- [ ] 有望な候補だけparse+bind、保持時メモリ・GC scan、代表プロジェクトのCLIで確認する。Binder単体改善をプロジェクト全体の高速化と呼ばない。
- [ ] 採用推奨は、意味の未説明差がなく、独立確認でも実Binderの命令数1%以上または通常wall3%以上の改善が精度基準と有意差を満たす場合にする。他方の指標が未確定なら、その限界を明記する。全体性能・メモリの回帰があれば採用を保留する。

## 結果を保存して完了する

- [ ] レポートに問題、証拠、再現手順、仮説、提案する修正、受け入れ条件を書く。
- [ ] 選択repo、候補clone、artifact setとstatus、各入力のns/op・B/op・allocs/op・KPC指標、benchstat、A/A判定、hot path、allocation driver、診断、次の行動を含める。
- [ ] raw、実行順序、selftest、source/overlay/binary/fixture/event DB hashes、trace、診断ログ、collector、正確性検証結果を保存する。
- [ ] 原因が不明なら、不明な範囲と次の最小実験を明記する。回帰が確認されなければ、その測定範囲と精度を示して終了する。
- [ ] 調査stageを実際に完了した時だけ、[既存の調査順序](binder-investigation-plan-20260910.md)へ進捗を追記する。

## 実行時に使うコマンドを確認する

以下は準備後のコマンド形。`BINDER_SNAPSHOT` を各版の固定repo、`BINDER_RUN` を新規runtime、`BINDER_VARIANT` を `before` または `after` に設定する。必要なrepo-root探索用overlayは両版に共通適用し、下記buildにも同じ `-overlay` を追加して記録する。fixture・設定と各版のsourceを用意する前には実行しない。

```sh
cd "${BINDER_SNAPSHOT:?}/tsc"
CGO_ENABLED=1 go test -c -tags=binderinvestigation \
  -o "${BINDER_RUN:?}/bin/${BINDER_VARIANT:?}-wall.test" ./internal/binder
CGO_ENABLED=1 go test -c -tags=binderinvestigation,kperf \
  -o "$BINDER_RUN/bin/$BINDER_VARIANT-pmu.test" ./internal/binder

BINDER_INVESTIGATION_MANIFEST="$BINDER_RUN/manifest.json" \
BINDER_INVESTIGATION_REPO_ROOT="$BINDER_RUN" \
  "$BINDER_RUN/bin/$BINDER_VARIANT-wall.test" \
  -test.run='^TestInvestigationBatchLifetime$' -test.v

BINDER_INVESTIGATION_REPO_ROOT="$BINDER_RUN" \
  "$BINDER_RUN/bin/$BINDER_VARIANT-pmu.test" \
  -test.run='^TestInvestigationKPCBatch$' -test.v

GOMAXPROCS=8 GOGC=100 GOMEMLIMIT=off BINDER_BENCH_GC_OFF=0 \
BINDER_INVESTIGATION_MANIFEST="$BINDER_RUN/manifest.json" \
BINDER_INVESTIGATION_REPO_ROOT="$BINDER_RUN" \
  "$BINDER_RUN/bin/$BINDER_VARIANT-wall.test" \
  -test.run='^$' -test.bench='^BenchmarkBindInvestigation$' \
  -test.benchtime=10x -test.count=1 -test.benchmem
```

最後の例は1 sampleである。固定driverが前述の順序で起動しstdout/stderr・exit codeを保存する。GC無効wallでは `BINDER_BENCH_GC_OFF=1` にする。KPCは管理者認証経路で `BINDER_KPC_CONFIG="$BINDER_RUN/instructions.json"` を追加し、自己検証の `-test.run='^TestInvestigationKPC$'` を先に実行する。本計測はpmu binary、`BINDER_BENCH_GC_OFF=1`、`-test.bench='^BenchmarkBindInvestigationKPC$'` を使う。自己検証結果と測定rawは分離する。

fixtureごとにfresh processへ分ける場合は、上記bench regexへエスケープ済み入力名を追加する。主・確認・A/Aの各pairを別ファイルへ抽出し、保存済みbenchstat binaryで `benchstat old.txt new.txt` を実行する。

## 手順の適用範囲と参照

pstackのinvestigationとperf-issueの測定原則を適用する。今回は計画作成なので、新規baseline trace、最適化実装、post-fix trace、PRは実行段階の作業として残す。複数PRの実装・マージ計画ではないため、multi-phase-planのPRテンプレート、UIの10 lane、PR用check-plan検査は適用しない。未実装APIの試作も不要で、既存ハーネスとdriverを確認した。

throughput checkpoint: n/a, read-only investigation

- [基盤移行の既存測定](foundation-migration-performance-20260915.md)
- [引数列修正の既存測定](narrowable-argument-performance-20260915.md)
- [寿命・GC・KPCの実行契約](kpc-measurement-workflow.md)
- [既存調査の順序と判断基準](binder-investigation-plan-20260910.md)
- ハーネスは `../bind_bench_test.go`、`../bind_investigation_kperf_test.go`。
- 意味監査は `tools/scripts/tsc/binder_semantic_audit.py` と `binder_audit_fixtures.json`。
