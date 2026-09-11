# list要素配列分離 × 整数Spanの実験（2026-09-10）

## 問題・仮説

list要素とnamed childが同じchildren配列を使うことが多段アクセスの原因かを、実Binderで検証する。
単純な要素配列分離は依存load段数を変えないが、cache配置やworking setへの効果は静的監査だけでは判断できない。
そこで配置と再解決削減を分けた2×2対照とする。

## 実装・条件

- normal: 保存baseline、元のwalker。
- split: list要素だけをlistElements []NodeRefへ移す。named childとlist index slotはchildrenに残す。
- span: 現行配置のままbindListRefで既存の整数Spanを使う。
- split_span: 分離配置で同じSpanを使う。

全条件でGo自動境界検査とsetter検査を維持。2pass、訪問順、Binderロジック、schema生成walkerは共通。
Spanはowner/start/lenをloop前に解決し、整数startから要素を読む。foreign listは既存fallback。
新規の直接descriptor配置やslice借用は実装しない。

分離版はStoreの要素アクセス、appendList、linkListの親付与、Factory.ListRefsのbulk copy、
Checkpoint/Restore、Compact、StoreScratchの再利用に対応。
初期NodeRef capacityの合計は従来と同じで、childrenとlistElementsに半分ずつ配分する。
この比率の最適化は探索しない。Compactは双方をexact-sizeにする。nodeHeaderは24 byteを維持。
Storeにslice headerが1本増えるが、両backing arrayは整数だけでnoscan。
配列数やcapacity成長の費用はparse側へ影響するため、Binder-only測定で全体高速化を主張しない。

## 再現とartifact

script: tools/scripts/tsc/binder_list_layout_experiment.py。
artifact: .cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/list-layout-experiment/。
prepareでoverlay buildと意味監査、runtimeでfixture・binary固定、wallとkpcを順次実行する。
既存結果を上書きしないため再実行は新しい出力先を指定するようscriptのDを変更する。
Go 1.26.0、Apple M1、GOMAXPROCS=8。checker/dom、10 bind × 6 rounds、順逆・回転順。
通常GC wallはGOGC=100、KPCはGOGC=off。parseは両方で計測外。
A/A ±1%（命令）、±1.5%（wall/cycles）を判定し、失敗指標も固定の探索比較を保存するが採用根拠にしない。
追加roundは行わない。全5 pairをbenchstatで比較する。

## 検証

4条件×20入力で構文・診断・Symbol・Flow・訪問順が一致。
normalとsplitは全アクセサ計数一致、spanとsplit_spanも一致。
候補3条件はAST/Binder/Compilerテスト成功（spanの既存テストはGo cache利用）。
splitとsplit_spanは追加の分離・Restore・Compact・scratch非alias試験を通過。
Spanのnil/empty/qualified/unqualified/foreign、範囲外panic、配列拡張、要素更新の追加試験も両配置で通過（-count=1）。
全4 KPC binaryで自己検証を2回実施。全回帰試験や全writerの契約証明ではない。

機械語は各条件の*.asm.txtに保存。normal/splitのbindListRefはListLenをloop前、ListElemをloop内でCALLし、
ListElem内部のlists[index].start→要素の依存が残る。span/split_spanのlocal経路ではheader解決がloop外へ移り、
要素loadは保持したstartとStoreの現在の配列baseから行う。foreign fallbackには元のCALLが残る。

Spanが置き換えるListLenはchecker 32,874回 / dom 18,172回、ListElemは50,646回 / 35,424回。
listOwnerは83,520回 / 53,596回、ID確認は50,646回 / 35,424回減る。これらは重複する計数なので加算しない。
配置分離自体によるcall削減はない。nodeHeader→list index slotの段は今回のSpanでも最初の解決時に残る。

## 精度

A/A paired mean log ratio bootstrap 95%区間（20,000回、seed=20260910）:

| 指標 | checker | dom | 判定 |
|---|---:|---:|---|
| EL0命令 | −0.37〜+0.19% | −0.09〜+0.25% | 両入力±1%内 |
| EL0 cycles | −1.96〜+1.75% | −0.45〜+0.49% | checkerが±1.5%外 |
| 通常GC wall | −1.44〜−0.29% | −0.94〜+1.86% | checker通過、dom不合格 |

checker wallは許容精度内だが、同一binaryのA/Aに約−0.88%の中心ずれがある。
約1%の配置効果を強い採用証拠とはしない。dom wallは探索値として扱う。
事前の固定protocolに従って全比較を保存し、追加roundや除外はしていない。

## 結果

同一campaignの中央値比、benchstat各n=6、名目p値（探索的、多重比較補正なし）。

| normalからの変化 | 命令 checker | 命令 dom | 通常GC時間 checker | 通常GC時間 dom |
|---|---:|---:|---:|---:|
| split | −0.43% (p=.180) | −0.16% (p=.818) | −0.97% (p=.026) | −0.75% (p=.132) |
| span | −1.74% (p=.002) | −2.87% (p=.002) | −1.61% (p=.004) | −2.10% (p=.180) |
| split_span | −1.88% (p=.002) | −2.61% (p=.002) | −1.69% (p=.026) | −1.23% (p=.065) |

Spanに分離を追加したspan→split_spanでは、命令−0.14% / +0.26%（p=.180 / .394）、
wall−0.09% / +0.89%（p=.818 / .394）。追加改善を確認できない。
分離配置にSpanを加えるsplit→split_spanの命令は−1.45% / −2.46%。全5pairのbenchstatと区間はartifact参照。
cyclesは全5pair・両入力で非有意。KPCのns/opを通常GC wallへ流用しない。

実測中央値（時間は通常GC、命令は別KPC campaign）:

| 入力 | 条件 | ns/op | B/op | allocs/op | EL0命令/op |
|---|---|---:|---:|---:|---:|

| checker.ts | normal | 16,416,440.0 | 12,799,444.0 | 14,165.0 | 185,795,698.0 |
| checker.ts | split | 16,256,521.0 | 12,799,444.0 | 14,165.0 | 184,998,098.5 |
| checker.ts | span | 16,152,762.5 | 12,799,470.0 | 14,165.0 | 182,555,778.0 |
| checker.ts | split_span | 16,138,392.0 | 12,799,444.0 | 14,165.0 | 182,309,516.0 |
| dom.generated.d.ts | normal | 5,797,642.0 | 7,866,703.5 | 16,684.0 | 77,813,192.0 |
| dom.generated.d.ts | split | 5,754,385.5 | 7,868,562.5 | 16,684.0 | 77,687,364.5 |
| dom.generated.d.ts | span | 5,676,119.0 | 7,870,332.5 | 16,684.0 | 75,579,826.5 |
| dom.generated.d.ts | split_span | 5,726,604.5 | 7,866,729.0 | 16,684.0 | 75,778,553.0 |

BinderのB/op・allocs/opは実質同じで割当改善を確認できない。
parseとCompactはtimer外なので、分離で増える配列object、capacity成長、配置準備の費用はこのB/opには含まれない。
初期容量配分を変えた効果も込みの簡易配置対照であり、cache局所性だけを厳密分離した測定ではない。

## 診断・次の行動

単純な要素配列分離は依存loadを除去せず、Binder全体の有意な命令削減も確認できなかった。
checker wallに小さい改善はあるが、A/Aのずれと同程度で、Spanに加える追加価値も確認できない。
分離が常に無効と証明したわけではないが、今回の実装を本体へ採用しない。

Spanの命令削減は共有・分離の両方で再現した。先行実験とも方向が整合する。
今回の結果は「同じmetadataの反復解決を減らす」設計を支持する。
ただしwall ≥3%の既存採用条件には未達。pointer同等やGC高速化の主張には足りない。
次は単純分離の調整を広げず、構文読取契約とslotから直接得るSpan/getterへ進む。
直接descriptor配置を選ぶ場合は、共有list identity・foreign・writerの契約を先に満たす。

広いhot pathは既存調査のwalk/helpersであり、今回のSpan対象はbindListRefだけ。
割当増の主因のsymbolIdx/flows列、FlowNode 32→48B、Symbol 96→104B、Handle 8→16Bは残る。
新規profileは取得せず、旧profileで今回のcache効果を断定しない。

## 選択repoとartifact状態

selected repo: /Volumes/SanDisk1TB/worktree/cursor-ast-store-tests、32598cba146fa4dd7b6162b838630c90d865ab28。
pointer checkout: /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript、8ac035a394c79e693a3a7d74cb170448503ee894。
tsgolint_git_revは両者null。candidate clonesはflownode、land-44-45、lock-design-inv、lock-profile、
merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜5、
store-pr-6-perf、store-pr-7、store-pr-7-attach-parent-fix、store-pr-7-nodeseq-t10、store-redesign。
全pathはworktrees.txt。別介入を混ぜないため不使用。

今回のartifactはrepo_root/tsgolint_git_rev/typescript_go_git_revが選択checkoutと一致しcurrent。
HEAD判定とsource一致を区別し、dirty.patch、go-sources.json、各overlay/source/binary SHAを保存。
4条件とも保存baseline Binderを使い、ユーザーのdirty本体Binderを変更していない。
旧list-span実験はidentity上currentの別保存条件、旧profileの今回候補への帰属はstale。
parse+bindの総時間・総B/op、GC専用測定、pointerとの今回同時比較、直接descriptor実装はmissing。
今回の全rawに要求Benchmark行があり、unsupported stageはない。

本番採用には所有権・意味・訪問順の一致、精度内の独立wall再確認、8入力非悪化、
parse+bindとGCの全体評価が必要。今回のsource/overlayと結果は保存し、本体未採用で終了する。
