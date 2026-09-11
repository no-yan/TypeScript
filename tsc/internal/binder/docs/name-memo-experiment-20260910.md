# 宣言名解決の再利用実験（2026-09-10）

## 問題・判断

walk/helperに分散するStore解決のうち、宣言名の再解決を切り分けた。直前の構文上の宣言名Ref・kindを再利用すると、実binderのEL0命令数がcheckerで0.90%、domで2.49%減った。**機構の改善は確認したが、通常GC wallで3%以上の有意な改善を示せず、採用しない。** wallの精度不足と効果ゼロは区別する。

## 選択repo・候補・artifact

- Store: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests` / `32598cba146fa4dd7b6162b838630c90d865ab28`。
- pointer: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript` / `8ac035a394c79e693a3a7d74cb170448503ee894`。TSGolintは両者null。
- 候補checkout一覧は既存artifact `20260910-binder-investigation-02/worktrees.txt`。flownode/store-redesign/store-nolock-exp/lock-profile/profile/store-pr-*等は別介入があり対照に使わない。
- 今回artifact root（以下N）: `.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/name-experiment/`。
- baselineは保存済baseline-binder.go。既存の未コミットPropertyAccessExpression候補は外し、作業ツリーの変更を保持した。新しい名前再利用はN/candidate/binder.goのoverlayだけで、本体へ未適用。
- identityは各buildのrepo_root/tsgolint_git_rev/typescript_go_git_revでcurrentを判定。binary/overlay/source/fixture hashは別に記録。microは三者、wall/KPCはStore baseline対candidate。

| stage | status | 扱い |
|---|---|---|
| 既存eval-8ac035a-20260909 | stale | 背景証拠のみ |
| audit-sites、今回audit・micro・wall・KPC | current | raw、identity、benchstat保存 |
| 最初のkpc-candidate build | missing | 空のgofmt入力で準備失敗、測定なし。v2で修正 |
| 通常GC CPU/assist・全phase・別セッション採用確認 | missing | wallの採用条件を満たさず未実施 |

## 呼出箇所別の証拠

計数専用audit-sitesでKindAt/FlagsAt/ChildRef等の直接の呼出箇所を分類。runtime.Callerで論理回数を数えたもので、サンプルCPU時間の帰属には使っていない。CPU profileの根拠は前回取得したInstruments CPU Profilerのみ。pprofは使用しない。

| accessor / caller | checker | dom |
|---|---:|---:|
| ChildRef / nameRefGenerated | 112,417 | 75,859 |
| ChildRef / generated walker | 189,849 | 74,321 |
| KindAt / nameOfDeclarationRef | 50,632 | 67,413 |
| FlagsAt / checkContextualIdentifierRef | 123,554 | 36,271 |

広いhot pathはwalk/helpers。今回の狭い対象は、hasDynamicNameRef・getDeclarationNameRef等で反復される構文上の宣言名解決。Symbol tableのhash/merge/診断を省略する実験ではない。Symbol syntheticも使用していない。

## 仮説・提案修正

nameOfDeclarationRefが正の構文名を解決した直後、その入力Ref・入力kind・名前Ref・名前kindをBinder内の一件の記録へ保持する。同じ入力なら名前スロット解決と名前KindAtを再実行せず、その値を返す。

- 再利用は正の構文名のみ。名前なし・代入先推論等のfallbackを記録しない。
- キーは宣言Refとkind。Binderは単一Storeに属し、pool返却時に全フィールドをゼロ化する。
- bind中に変更されるflagsやsymbolは記録しない。構文名のchild slotとchild kindがbind中に不変であることを利用する。
- 保持するのはscalarのRef/kind。AST Storeのnoscan列や公開APIは変更しない。Binder自体の追加fieldはコストに含める。
- 全nodeのKind cache、list-only hoist、kindごとの一括取得追加とは異なる。今回の新しい呼出回数・ヒット監査を根拠に、一つの名前解決経路を評価する。

## 回数と正しさ

| 入力 | nameOfDeclaration問い合わせ | 再利用hit | 省けたChildRef | 省けたKindAt |
|---|---:|---:|---:|---:|
| Herebyfile.mjs | 1,553 | 1,008 | 1,008 | 1,008 |
| api.ts | 4,230 | 2,727 | 2,727 | 2,727 |
| checker.ts | 51,598 | 33,299 | 33,299 | 33,299 |
| client.ts | 814 | 519 | 519 | 519 |
| dom.generated.d.ts | 69,943 | 44,119 | 44,119 | 44,119 |
| jsxComplexSignatureHasApplicabilityError.tsx | 811 | 517 | 517 | 517 |
| mapCode.ts | 336 | 198 | 198 | 198 |
| proto.generated.ts | 2,160 | 1,470 | 1,470 | 1,470 |

再利用率は約59〜68%。8代表入力で共通操作数、訪問順、構文/CFG/Symbol/node-symbol/診断hash、問い合わせ順が一致。追加9ケース（optional chain、computed/private names、欠損構文、予約語、export、ambient、JS、JSX、名前推論fallback）でも一致。予約語ケースはbinder診断3件で一致。既存のpointer対Storeの匿名class Symbol.Name差異は今回修正していない。

## 三者micro

実binderから記録した問い合わせ順を再生し、parentをkind/pos/endで一意に対応付けた。一意に解決できない入力は失敗させる。名前の結果もkind/pos/endのchecksumで比較し、全8入力でpointer/baseline/candidateが一致。setupとparseはtimer外、各反復でBinderの再利用状態をリセットする。

| 入力 | pointer ns/op | Store baseline ns/op | candidate ns/op | baseline→candidate |
|---|---:|---:|---:|---:|
| checker.ts | 279,250 | 355,188 | 278,031 | -21.72% |
| dom.generated.d.ts | 357,088 | 407,706 | 313,522 | -23.10% |

各200ms×12rounds、三者の実行順を循環・反転。全て0 B/op・0 allocs/op。両入力とも短縮はbenchstatで有意（表示p=0.000）。一回の実bindに相当する問い合わせ列で約77〜94µs削減であり、そのままbinder全体23%改善とは扱わない。

## 実binder wall（通常GC）

Go1.26.0、CGO=1、GOMAXPROCS=8、GOGC=100、GOMEMLIMIT=off。新規parse・強制GCはtimer外。各30bind×20rounds、順序反転。

| 入力 | baseline ns/op | candidate ns/op | 中央値差 | paired log比CI95 | benchstat |
|---|---:|---:|---:|---|---|
| checker.ts | 16,987,046.5 | 16,915,127.0 | -0.42% | [-8.60, +1.80]% | p=0.414, 非有意 |
| dom.generated.d.ts | 5,922,342.0 | 6,009,337.5 | +1.47% | [-0.85, +3.37]% | p=0.862, 非有意 |

| 入力 | baseline B/op | candidate B/op | baseline allocs/op | candidate allocs/op |
|---|---:|---:|---:|---:|
| checker.ts | 12,799,533.0 | 12,799,535.5 | 14165 | 14165 |
| dom.generated.d.ts | 7,867,921.5 | 7,867,950.5 | 16684 | 16684 |

B/op・allocs/opにも有意差なし。壁時計の区間が広く、domでは3%退行も除外できない。外れ値を除外せず保存し、有意になるまでround数を延長していない。後から確認した背景負荷はgit117%、Codex Renderer87.7%、mdworker25.3%で、温度警告履歴なし。ただし事後観測から各計測時点の負荷を断定しない。processは停止していない。

## 実binder KPC（通常GC wallとは別）

GOGC=off、30bind×10rounds、順序反転。kpc-build-v7と同じbaseline binary/event hashのA/A合格を確認し、新規OS threadで測定。両binaryの自己検証を各2processで再実行してPASS。管理者processのfixture/root探索は/tmpへ分離。

| 入力 | baseline EL0 inst/op | candidate EL0 inst/op | 差 | EL0 cycles差 | benchstat |
|---|---:|---:|---:|---:|---|
| checker.ts | 185,534,851.5 | 183,861,591.0 | -0.90% | -0.97% | inst有意、cycles非有意（p=0.123） |
| dom.generated.d.ts | 77,821,463.0 | 75,887,204.5 | -2.49% | -2.67% | inst有意、cycles有意 |

両入力のinstはp=0.000。domのcyclesもp=0.000。checkerのcyclesは中央値が低いだけで、有意な改善とはしない。domの分岐命令は3.37%減、L1D load missは1.29%減。逆アセンブルでhit時にnameRefGenerated/KindAtの解決を迂回する経路を確認。静的命令行数を動的命令数に換算していない。

**「機構は改善したが全体への寄与が限定的」「wallの精度不足」の両方が残る。** KPC下の時間を通常GC wallの代用にして3%合格とはしない。

## 採用条件・次の行動

今回の候補は本体へ適用しない。必要な通常GC wall3%以上の有意短縮を示しておらず、非退行区間・別セッション・全phase/通常GC CPUも未確認。cache hitや命令数削減だけで採用しない。既存allocation driver（symbolIdx/flowIdx列、FlowNode32→48B、Symbol/Handle拡大）は今回未変更。

次は測定環境の精度確認を優先する。再評価するなら独立A/Aで±1.5%を確認し、あらかじめ固定した回数でdomの通常GC wallを比較する。精度確認前に記録対象をflagsや別helperへ広げない。現段階で新しいkind別一括取得やsymbol syntheticへ切り替える根拠もない。

## 再現・成果物

- `python3 tools/scripts/tsc/binder_name_experiment.py --out <元artifact絶対path>` は新しいname-experimentを準備。既存出力への上書きを拒否する。
- N/prepare-micro.py、run-experiment.py、prepare-kpc.py、kpc/run.pyは今回の固定source/pathを使った再現スクリプト。実行先を変える場合は新artifactを指定し、identityを取り直す。
- N/audit-equality.json、edge-equality.json、micro-equality.jsonに正しさ確認。N/wall-20、micro-12、kpc/paired-10にraw・benchstat・区間。
- N/candidate/binder.goがreview用修正、baseline/name-resolver.asm.txtとcandidate/name-resolver.asm.txtが逆アセンブル。
- AST/binder/compiler単体テストはPASS（core-tests.txt）。コンパイラ全回帰はFAILで、既存baselineと同じ11,115件の失敗集合（親testを含む）。新規失敗名・消えた失敗名はともに0。失敗集合の一致を、各失敗ケースの出力の完全一致とは扱わない。compiler-regression.txtとregression-comparison.jsonを参照。
