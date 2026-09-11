# Store setter検査削減の診断実験（2026-09-10）

`-B`にsetter検査削減を加えた追加効果は、Binder全体のEL0命令数でchecker **−0.59%**、dom **−0.39%**。
通常版からの合計は **−7.29% / −5.07%**。通常GC wallの中央値は合計 **−3.03% / −1.30%** だが、
A/A精度不足かつ合計比較も非有意であり、wall高速化は未確認。本体には採用しない。

## 問題と仮説

ユーザー指定の追加診断。前回のGo自動境界検査除去に加え、Store setterの手書き検査、
Handle生存検査、同一Storeの再確認が残存命令をどれだけ占めるか測る。
2pass、訪問順、名前・Flags等の値取得とBinderの処理ロジックは変えない。

## 対象とartifact status

- selected repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、`32598cba146fa4dd7b6162b838630c90d865ab28`。
- pointer対照checkout: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、`8ac035a394c79e693a3a7d74cb170448503ee894`。今回は再計測しない。
- 両者のtsgolint_git_revはnull。`identity-verified.json`でHEADとの一致を検証。
- candidate clones: flownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜5、store-pr-6-perf、store-pr-7、store-pr-7-attach-parent-fix、store-pr-7-nodeseq-t10、store-redesign。別介入を混ぜないため不使用。全pathは`worktrees.txt`。
- artifact set: `.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/setter-check-experiment/`。
  今回の4条件は3つのidentity条件が一致するため **current**。全条件を新規buildした。
  dirtyな本体Binderではなく、従来の保存baseline Binderをoverlayで共通使用。
  HEADだけでsource同一性を主張せず、`dirty.patch`、`source-hashes.json`、各buildのoverlay/source/binary SHAを保存した。
- 前回bounds-check-experimentはidentity上currentの別条件。旧Instruments profileは今回のcandidate帰属には **stale**。
  本番用の書込契約、pointerとの今回の同時比較、GC専用測定は **missing**。
  今回の要求regexについて **unsupported** stageはない。全rawに要求Benchmark行がある。

## 介入の範囲

4条件とも元のwalker。`normal`、`bounds`、`setter`、`combined`。
`bounds`はBinderとastの両packageに`-B`を指定する前回の最大除去条件で、runtime/stdlibには適用しない。
文字どおりの`-gcflags=-B`だけではBinder packageのみが対象になる点は前回と同じ。

setter介入は次のとおり。

- StoreのSetFlagsAt / SetSymbol / SetLocalSymbol / SetFlow / SetEndFlow / SetReturnFlow / SetLocals / SetNextContainerから`mustMutate`を除去。
- HandleのSetFlags / SetUintValue / SetObjectValue / SetSymbol / SetLocalSymbol / SetFlowNode / SetEndFlowNode / SetReturnFlowNode / SetLocals / SetNextContainerについて、存在する`mustLive`と`mustMutate`呼出を除去。
- `mustMutate`はFreeze後のwrite禁止検査。`mustLive`はnil、ID=0、IDとnodes長の検査。Goが挿入する境界検査とは別物。
- SetFlowは、非zero IDのFlowが当該Storeのarenaに属する前提でIDを直接使用する専用helperへ変更。
  元の`flowID`は全callerから消さず、nilは0、ID=0の外部・sentinel Flowは従来のfallbackを維持。
  local経路では既存lastFlow fast pathの判定も省かれ、lastFlow/lastFlowIDは更新する。
  このため結果は検査命令だけの厳密な分離ではなく、local ID変換経路のコード生成全体の効果である。
- Handle.SetNextContainerの`refInStore`を直接IDへ置換。元のBinder本体は既にStore.SetNextContainerへNodeRefを渡しており、このwrapper変更が主要効果とは想定しない。
- `putCol`の列拡張、nilの意味、map更新、symbol/flow格納方式、構造setter、NewFlow等のallocatorは維持。
  scalar/object slotの負値検査も維持。全Store検査を取り払う実験ではない。

検査付き監査版では非zero Flow IDのarena所有権とNextContainerの同一Storeをassertした。
通常のphase/live検査も残して20入力が通過。4測定条件も各20入力で構文、診断、Symbol、Flow、
訪問順および既存アクセサ計数が一致した。計数は既存getter中心で、setter別の動的回数は今回追加していない。
今回の監査は既知入力の前提確認であり、一般のStore APIの検査削除を正当化しない。

## 測定条件と精度

Apple M1、darwin/arm64、Go 1.26.0、GOMAXPROCS=8、CGO_ENABLED=1。
checker.ts / dom.generated.d.ts、10 bind × 6 rounds、4条件を順逆・回転順で交替。
parseは計測区間外。通常GC wallはGOGC=100、命令は既存KPC harnessのGOGC=off・fresh thread。
wallとKPCは順次実行し、buildは先に完了。各KPC binaryの自己検証を2回実施。
A/A後も固定の探索比較を実行することを事前protocolに明記し、精度失敗を隠さず報告する。
追加roundは実施しない。全5pairをrawからbenchstatで比較した。

A/Aのpaired mean log ratio bootstrap 95%区間（20,000回、seed=20260910）:

| 指標 | checker | dom | gate |
|---|---:|---:|---|
| EL0命令 | −0.59〜+0.23% | −0.65〜+0.11% | 両入力±1%内、通過 |
| EL0 cycles | −1.29〜+1.83% | −0.40〜+0.84% | checkerが±1.5%外 |
| 通常GC wall | −3.11〜+0.95% | +0.31〜+2.46% | 両入力±1.5%外 |

## 結果

以下の割合は同一campaign内の中央値比。p値はbenchstat（各n=6、探索的・多重比較補正なし）。

| 比較 | 命令 checker | 命令 dom | 通常GC時間 checker | 通常GC時間 dom |
|---|---:|---:|---:|---:|
| normal → bounds | −6.73% | −4.69% | −2.33% (p=.002) | −0.70% (p=.394) |
| normal → setter | −0.74% (p=.002) | −0.36% (p=.180) | −0.85% (p=.180) | +2.22% (p=.310) |
| bounds → combined（追加効果） | −0.59% (p=.002) | −0.39% (p=.004) | −0.71% (p=.394) | −0.60% (p=1.000) |
| normal → combined（合計） | −7.29% (p=.002) | −5.07% (p=.002) | −3.03% (p=.065) | −1.30% (p=.180) |

通常GC時間は精度gate不合格なので、bounds単独checkerの名目p=.002も採用根拠にしない。
combinedのchecker時間は一部sampleの揺れが大きい（benchstat範囲±35%）。追加効果・合計ともwall非有意。
KPCのns/opを通常GC wallの代替にしない。命令差と時間差は一致しない。

実測中央値（時間はns/op）:

| 入力 | 条件 | 通常GC ns/op | B/op | allocs/op | EL0命令/op |
|---|---|---:|---:|---:|---:|
| checker.ts | normal | 16,462,687.5 | 12,799,444.0 | 14,165.0 | 185,661,333.5 |
| checker.ts | bounds | 16,078,456.5 | 12,799,444.5 | 14,165.0 | 173,159,667.5 |
| checker.ts | setter | 16,322,642.0 | 12,799,407.0 | 14,165.0 | 184,295,264.5 |
| checker.ts | combined | 15,964,683.0 | 12,799,444.0 | 14,165.0 | 172,133,653.0 |
| dom.generated.d.ts | normal | 5,795,583.5 | 7,868,562.5 | 16,684.0 | 77,813,308.5 |
| dom.generated.d.ts | bounds | 5,754,833.5 | 7,866,729.5 | 16,684.0 | 74,162,231.5 |
| dom.generated.d.ts | setter | 5,924,312.0 | 7,870,383.5 | 16,684.0 | 77,535,627.5 |
| dom.generated.d.ts | combined | 5,720,288.0 | 7,868,550.0 | 16,684.0 | 73,871,653.0 |

B/op、allocs/opの改善は確認できない。全rawのKPC ns/op、fixed counter等も保存しており、
`kpc-pairs/summary.json`と各benchstatに全指標がある。

機械語は`*.asm.txt`に保存。選択したsetter/flow関数の範囲ではnormalのpanicBounds 5箇所がboundsで0、
手書きgopanic 2箇所がsetterで0、combinedで双方0。inline先を含む全setterの網羅集計ではない。
static命令行数はhelper追加とinlineで関数集合が変わるため、動的命令削減量とは扱わない。

## 診断・提案・受け入れ条件

今回のsetter介入の追加効果は1%未満。Go自動境界検査の影響は再現したが、このsetter検査削減を加えても、
pointer同等に必要だった約29〜35%のwall短縮を説明できない。命令削減率をwallの上限へ直接換算もしない。
広いhot pathは既存調査のwalk/helpersであり、setterはその一部。
旧profileだけで今回の候補のhotspot帰属は断定しない。

割当増の主因であるnode-wide symbolIdx/flows列、FlowNode 32→48B、Symbol 96→104B、Handle 8→16Bは残る。
この介入ではnoscan性を含むデータ型を変更しておらず、GC高速化の証明にはならない。

次は予定していた構文writerの推移的監査と読取契約、schema既知の直接getterへ戻る。
setter専用のlocal契約は補助候補とし、一般APIの検査を無条件に除去しない。
本番採用には全callerでphase/live/ownershipを保証し、foreign Flow・cross Storeの正当な機能を保持すること、
意味・訪問順の一致、精度gateを満たす独立通常GC wall比較で短縮を確認することが必要。
pointer優位性を論じる段階では、同じ処理ロジックと安全性条件でpointerも同時測定する。

## 再現

`tools/scripts/tsc/binder_setter_experiment.py prepare`で新規artifact先へbuild・監査、
`runtime <新規temp先>`でfixture・binaryを固定し、`wall <temp先>`、`kpc <temp先>`を順次実行する。
既存artifactを上書きしないため、再実行ではscriptの出力先Dを新しい名前にする。
KPCはmacOS管理者認証が必要。rawは各`*-pairs/{normal,bounds,setter,combined}.txt`、
比較は`*.benchstat.txt`。実行script・依存script・protocol・overlay・build log・SHAもartifactに保存した。
