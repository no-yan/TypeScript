# 85506e8 と 80d8b41 の性能比較結果

2026-09-15。ユーザーの指示で追加の改善計測を終了し、完了した比較・診断をまとめた。production codeの変更・採用・commitは行っていない。

## 結論

最新コミットは起点よりBinderの命令数がcheckerで20.97%、domで20.80%多い。cyclesも16.12% / 14.09%増加した。両指標とも前後のA/A精度条件を満たし、主比較・確認用ともbenchstat p=.002。通常wallの時間は増加方向だが、精度条件を満たさず確定倍率は保留する。

訪問node数とFlow生成数は同じ一方、kindの再取得とHandle経由のアクセスが増え、既存のlist span経路が失われている。これは実行量増加を説明する具体的な候補である。各経路へ21%全体を因果配賦する実験は未完了。

## 比較対象と証拠の状態

- 選択repoは `/Volumes/SanDisk1TB/worktree/binder-rewrite`。
- Aは `85506e8b7d51f2b05f84f9ba1a8c3309b2f30ce4`、Bは `80d8b41ccfb046a551b721cb04f909205d66513a`。中間Mは `6f6f3ae597fdd73bb3a28b172fe8b647a2694bb8`。
- 候補checkoutはcursor-ast-store-tests、pointer main、flownode、lock系、store-pr系など。比較には選択repoから固定したA/Bの独立git archive snapshotだけを使った。Mと候補は検証済みの限定overlay。
- `tsgolint_git_rev=null`。Go 1.26.0、Apple M1、8GB、macOS 26.6.2、GOMAXPROCS=8、GOGC=100、GOMEMLIMIT=off。
- Artifact setは `/Volumes/SanDisk1TB/Library/Caches/binder-85506e8-80d8b41-20260915-run1`。

| Artifact / stage | status | 完了・制限 |
| --- | --- | --- |
| 主A/B wall・KPC | `current` | 144/144行。identity・バイナリ・入力・sourceを終了時に再照合 |
| A/B Instruments・操作数・意味監査 | `current` | 4 traceと2入力の操作数、11入力の意味監査 |
| A/M・M/B・9入力拡張のwall | `current` | wallは完了。各run全体はKPC未取得のためcomplete=false |
| 上記followupのKPC | `missing` | ユーザー指示で追加実行せず |
| 改善候補のKPC、parse+bind、retained live/scan、CLI、追加conformance | `missing` | 改善の検証・採用判断は未完了 |
| 旧foundation・narrowable artifact set | `stale` | 今回の直接比較へ数値を混ぜていない |

今回実行したstageには要求Benchmark行があり、`unsupported` はない。旧foundationの保存stageにbench.txtが欠ける点は旧artifactの制限としてprotocolに記録した。statusと精度の合否を別に扱う。

## 主比較の性能値

1 opはparse済みの独立ASTを1回bindする処理。10 ASTのbatchをfresh processで測定した。通常GC wall、GC無効wall、GC無効KPCを別stageにし、各6固定round、前後a/bの4ラベルを回転・反転した。a/aとb/bを合算せずbenchstatで比較した。

| 入力 | 指標 | A | B | 主比較差 | 確認用差 |
| --- | --- | ---: | ---: | ---: | ---: |
| checker.ts | ns/op | 16,088,731.0 | 18,266,012.5 | +13.53% | +13.43% |
| checker.ts | B/op | 12,798,224.0 | 12,777,532.5 | -0.16% | -0.16% |
| checker.ts | allocs/op | 14,163.0 | 14,165.0 | +0.01% | +0.01% |
| checker.ts | el0-inst/op | 165,461,457.5 | 200,163,250.0 | +20.97% | +20.46% |
| checker.ts | el0-cycles/op | 78,310,183.0 | 90,931,889.5 | +16.12% | +16.01% |
| dom.generated.d.ts | ns/op | 6,059,273.0 | 6,880,706.0 | +13.56% | +10.75% |
| dom.generated.d.ts | B/op | 7,869,560.5 | 7,867,973.5 | -0.02% | +0.04% |
| dom.generated.d.ts | allocs/op | 16,682.5 | 16,683.0 | +0.00% | +0.00% |
| dom.generated.d.ts | el0-inst/op | 68,632,573.5 | 82,910,635.0 | +20.80% | +20.94% |
| dom.generated.d.ts | el0-cycles/op | 29,428,199.5 | 33,574,075.0 | +14.09% | +14.61% |

通常wallの前後両版A/A条件は両入力とも未達。ns/opの約13.5%増は観測値であり、精密な時間回帰率として採用しない。GC無効wallも同様に未達。

KPCの命令数A/Aは95% CI全体が±1%以内、cyclesは±1.5%以内という条件を前後両版・両入力で満たした。bootstrapは同roundの対数比、20,000回、seed固定。KPC内のns/opはcounter readを含むため通常wallと混合しない。EL0+EL1固定counterと、表のEL0可変counterを分離した。

## 原因について確認できたこと

### 同じnode数でも参照の仕方が変わっている

| 操作 / Bind | checker A → B | dom A → B |
| --- | ---: | ---: |
| BindEntries | 298,054 → 298,054 | 109,605 → 109,605 |
| KindAt | 554,467 → 1,173,328 | 156,084 → 426,319 |
| At | 0 → 708,951 | 0 → 269,003 |
| ListLen | 40,101 → 113,697 | 29,088 → 60,375 |
| ListElem | 2,812 → 100,308 | 0 → 44,181 |
| TryBindListSpan | 71,202 → 0 | 51,044 → 0 |
| BindListSpanElem | 100,570 → 0 | 47,961 → 0 |
| NewFlow | 79,989 → 79,989 | 4,844 → 4,844 |
| SetFlow | 172,916 → 172,843 | 37,215 → 37,215 |

- Aは取得済みkindをbindKind等へ渡す。Bはbind・bindChildren・bindEachChild等で再取得する。
- Aの129個の生成walkerのlist呼出箇所はspan対応のbindListRefを使う。BのbindEachは各要素でlist owner/headerを解決するListElemを使う。functions-firstの2passでもspanが失われている。
- BはStore.At経由でHandleを作る呼出しが増えた。At=0のAにもHandleOfや直接getterがあるため、この表を全Handle生成数とは扱わない。
- 計数は診断専用binaryでroot bind直前から行った。parseとunreachableFlow sentinel生成を除外する。ListRefAt、生成accessor、Handleメソッド、直接field readは対象外。計数binaryの時間は性能値に使わない。

### CPUのhot pathは広いwalk/helper境界にある

最新Instrumentsでも、広いhot pathはbind・子走査・helper群。Bのexclusive leafではcheckerのbindが13.73%、bindChildrenが6.05%、GetContainerFlagsが2.34%。domではbindが10.55%、bindChildrenが3.64%、declareSymbolExが2.97%。map・文字列hash・Handleアクセスにも分散している。狭いlist解決は改善しやすい一部であり、21%全体の説明ではない。

4条件をCPU Profilerで各20秒採取し、Runningかつ対象PIDのstackからBinderを抽出した。parse・GCを分離し、未解決参照/stackは0件。構成比はexclusiveとinclusiveを分け、inclusiveを加算しない。各1 traceの構成比を速度差や削減可能量へ換算しない。

### 割り当て増はほぼ解消している

A/Mのwall比較ではcheckerが14,163→24,065 allocs/op、M/Bでは24,065→14,165。直接A/BではB/opが約0.16%減り、allocs/opは2回多いだけ。旧9,900回の一時slice生成はBで解消されているため、残る約21%の命令増をその割り当て増だけで説明できない。Flow/列/Symbol/Localsは従来のallocation driverだが、今回その全stackを再採取して増分を帰属したわけではない。

## 入力拡張と中間コミットの比較

追加wallは完了したが、KPCは追加実行していない。A/Mは通常wallでchecker +16.27%、dom +17.31%、M/Bは+1.07% / +0.84%。M/Bの確認用checkerは−1.50%と符号も異なる。各比較のwall精度が未達なので時間の因果倍率として使わない。

9入力拡張の通常wall主比較は以下。すべての入力で前後両版の精度条件を同時には満たさなかったため、増加方向の観測として扱う。

| 入力 | 通常wall主比較 | 確認用 |
| --- | ---: | ---: |
| checker.ts | +9.54% | +15.39% |
| dom.generated.d.ts | +10.53% | +16.60% |
| Herebyfile.mjs | +6.06% | +8.17% |
| async-api.ts | +14.63% | +22.42% |
| proto.generated.ts | +16.28% | +14.89% |
| parserharness.ts | +9.04% | +12.70% |
| controlFlowOptionalChain.ts | +13.69% | +11.13% |
| jsxComplexSignatureHasApplicabilityError.tsx | +7.99% | +8.66% |
| lib.es5.d.ts | +9.01% | +4.59% |

Herebyfile.mjsは実JavaScript/JSDoc入力。controlFlowOptionalChainとTSX fixtureは構文カバレッジの対照であり、実プロジェクト全体の代表値とはしない。すべての入力のpath・hashを両snapshotで照合した。

## 正確性と改善候補の状態

- Bのast/binder/checker/compiler package testはPASS。A/B/M/候補の寿命・KPCBatchテストもPASS。主A/Bの実カウンタ自己検証は各版2processでPASS。
- A/Bの11入力意味監査に15 fieldの差がある。JSON Symbol同期9件、static block/case Flow所有4件、匿名関数Symbol名2件で、既存の基盤移行記録にある意図したupstream整合修正と一致する。未説明差として隠していない。
- 候補はBのbindEachだけに既存TryBindListSpanを戻す6行のoverlay。fallbackと訪問順を維持し、kindやfunctions-firstを変更していない。B対候補の11入力の意味監査は差0。
- 停止指示時点では候補の通常wallは終了していた。主比較−2.39% / −2.81%、確認用−1.83% / −2.18%だが、A/A未達。候補KPCは未実行。改善の確定・採用判断はしていない。候補の追加検証を止めた。
- 全conformance、parse+bind、保持時メモリ/scan、プロジェクトCLIの追加検証は未実施。既存conformance記録を今回の新規検証として数えない。

## 診断と次の行動

命令数とcyclesの回帰は確定。主因候補は基盤移行で増えた再取得・Handle/helper境界と失われたlist span経路である。割り当て増の解消だけでは実行量の回帰は解消していない。意図した意味修正を維持しながら、取得済みkindとlocal list情報の再利用を個別に評価するのが次の方針。

ユーザーの指示により、ここで調査を終了する。再開する場合は、保存済み最小span候補の命令数を測り、その寄与を確定してからkind伝搬の独立対照へ進む。現在の結果から各候補の改善率や全回帰の解消を約束しない。

## 再現・保存物

主比較の再現は固定snapshotからbinを復元し、manifestの絶対pathを同じruntimeへ復元する。新しい実行には別run directoryと新しいprotocol/identityを用い、旧rawへ追記しない。`drivers/runner.py`と`drivers/collect.py`、fixture、event設定、全raw、bin、source archive、source hash一覧を保存した。

- `identity.json`、`final-verification.json`、`protocol.md`、`environment-before.json`
- `analysis/summary.json`、`analysis/aa-gates.json`、`analysis/*/*.benchstat.txt`、`raw/`、`order.jsonl`
- `instruments/` と `diagnostic-drivers/`
- `operation-counts/`、`semantic-audit/`、`span-candidate/`
- `followups/`。各runのwallは完了、KPC欠測による全体complete=falseを保持した。

synthetic symbol benchmarkは実行しておらず、実ワークロードのボトルネック証明には使っていない。
