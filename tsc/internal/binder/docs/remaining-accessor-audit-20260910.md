# 残存ChildRef / KindAt / FlagsAtの削減可能量監査（2026-09-10）

単純なgetter検査省略だけを主な到達手段にする根拠は不足している。
次の設計対象は **構文読取区間でのheader位置の短期借用** に絞る。
Flagsや宣言名の値memo、全node cacheは主比較に入れない。新しい実Binder benchmarkは実行していない。

## 今回追加した証拠

1. 保存caller計数を8入力で再集計し、各accessorのcaller和と総数が一致すること、最新実験のnormal監査の総数とも一致することを確認。
2. 実ast.Storeを使うGo codegen probeをコンパイルし、既知slot/live refで検査を省いた時の機械語を比較。nil child、値一致、Flags更新後のfresh readをテスト。
3. Storeの構文変更入口26個とFactory.ListRefsのbulk-copy入口1個、Flags setter2個を計数し、20入力のBindSourceFile区間を監査。
   root=sf.ParseStore()とother Storeを区別。計数を有効にしたsynthetic writeでroot/other双方を検出する自己試験も成功。
   この試験は機構確認であり性能benchmarkではない。

## 呼出範囲

回数はread命令数ではなく論理accessor入口回数。

| accessor | checker回/bind | dom回/bind | 主な仕事 |
|---|---:|---:|---|
| ChildRef | 558,230 | 196,338 | 親header→childStart→children、slot検査 |
| KindAt | 688,528 | 200,203 | node headerのkind読出し |
| FlagsAt | 530,841 | 164,574 | node headerの最新Flags読出し |

ChildRefの生成walker＋生成helperは396,418 / 187,892回（約71% / 96%）。
単なるwriter監査では、全callerでのshape一致・live refは証明されない。これは直接getterの候補範囲である。
KindAt/FlagsAtにはlistOwner・Store IDのatomic再解決はなく、listの直接resolverと同じ利益は想定できない。

caller上位:

| accessor/caller | checker | dom | 評価 |
|---|---:|---:|---|
| ChildRef: generated walker | 189,849 | 74,321 | 型dispatch済みslot。現在既にinline、CALL除去を再計上しない |
| ChildRef: nameRefGenerated | 112,417 | 75,859 | 既知shape getter候補。名前値memoにはしない |
| ChildRef: expressionRefGenerated | 61,891 | 0 | 既知shape getter候補 |
| KindAt: isIdentifierNameRef | 123,553 | 0 | 読んでいるのは親のkind。現在nodeのkindと同一視しない |
| KindAt: bindChildRef | 121,262 | 52,545 | 取得した子のheaderを初めて読む。不要readとは扱わない |
| KindAt: nameOfDeclarationRef | 50,632 | 67,413 | 宣言名のkind。呼出元の宣言kindとは別 |
| KindAt: at | 115,545 | 18,716 | Handle構築のkind。既存のHandleOfとの適用条件監査が必要 |
| FlagsAt: bindKind | 316,520 | 109,605 | 書込み・再帰を挟む複数siteを含む。全件を重複と数えない |
| FlagsAt: checkContextualIdentifierRef | 123,554 | 36,271 | 先行Flags再利用は命令改善なし。値memoは主対象外 |
| FlagsAt: isOptionalChainRef | 46,422 | 50 | Flagsは可変。保持するなら位置であり値ではない |

全callerはcensus.json。手書きChildRefは161,812 / 8,446回。
同じfieldの近接read計数は再利用可能数ではない。accessor-fieldsのnearは計数対象accessorの距離≤4で、
writer epoch・同一stack frame・保持期間の証拠を持たない。
例: checkerのbindKind Flags→isOptionalChain Flags 44,706回、domのnameOfDeclarationRef Kind→同Kind 34,665回を、
そのまま安全なmemoやStore固有の利益にしない。子kind、親kind、宣言名kindの異なる対象も混同しない。

## 機械語と単価の桁

Go1.26.0、darwin/arm64。実Storeの小さいnoinline wrapper内でgetterをinlineさせたprobe。
Go自動境界検査は維持。非nil/live/有効slot・stack十分の経路をたどった静的命令数（prologue/epilogue込み）:

| probe | 通常 | 検査省略 | 主に消えるもの |
|---|---:|---:|---|
| ChildRef slot=0 | 27 | 23 | childLenのload＋分岐、return周辺のコード生成差 |
| KindAt | 20 | 16 | Store/refのnil確認、return周辺の差 |
| FlagsAt | 21 | 17 | Store/refのnil確認、return周辺の差 |

ChildRefでschemaから正当化できる差は主にchildLenのloadとbranchの2命令。
Kind/Flagsのlive入口でもnodes base/len取得、index変換・24 byte行への計算、bounds check、field loadは残る。
実callerではnil確認が既に消えている場合があり、この4命令差をそのまま動的削減単価にしない。
逆に他slotやcallerでコード生成が変わるため、厳密な上限とも呼ばない。

KindAtとFlagsAtを単に同じwrapperから連続呼出しても、このprobeでは各々のheader解決が残った。
同じnodeという情報だけでcompilerがすべてを共有するとは限らない。
ただし、これをFlagsの先読み・値memoへ置き換える根拠にはしない。

生成ChildRef全件とKindAt/FlagsAt全件が改善可能だと楽観的に仮定すると対象は1,615,787 / 552,669 calls/bind。
各callで同じ数の命令を独立に省ける仮説の感度計算:

| 仮定の削減単価 | checker削減命令 | dom削減命令 | 留意 |
|---|---:|---:|---|
| 2命令/call | 3,231,574 | 1,105,338 | 実測の動的効果ではない |
| 4命令/call | 6,463,148 | 2,210,676 | 実callerでの重複除去済み分を未控除 |
| 8命令/call | 12,926,296 | 4,421,352 | より広いheader解決省略を仮定した参考値 |

4命令シナリオでも直近normal KPCの約3.5% / 2.8%規模。
この感度計算は予測・保証・上限ではなく、wallへの換算もしない。
既存pointer/Storeのwall差を埋めるにはStore wall約34.65% / 28.51%短縮が必要だった。
変更対象時間割合f、対象内短縮rなら単純モデルでf×rがこの値に届く必要があるが、fは未確定。
検査省略をノード型ごとに繰り返す計画では、到達予算を裏付けられない。

## 構文writerとFlagsの動的監査

27の構文入口を監査した20入力で、BindSourceFile中のroot/otherいずれの構文writer計数も0。
Flags setterは全20入力で呼ばれ、checkerのroot SetFlagsAtは4,980回、domは6,391回。
構文・診断・Symbol・Flow・訪問順が保存baselineと一致し、追加writer keyを除いたaccessor計数も一致した。

対象にはappendSlots/appendList、linkChild/linkChildRef/linkList、SetChild/SetListAt/SetParent/SetIdent、
SetListSlot、Restore、Compact、Factory.ListRefs等を含む。mustMutateだけを監視する方式ではない。
完全な一覧はwriter-instrumentation.json、20入力結果はwriter-results.json。

これは既知入力の動的証拠。すべてのtransitive caller、別goroutineからのwrite、将来の追加writer、
lazy JSDoc/tokenを含む全入力を証明しない。SetUintValue/SetObjectValue等のside table writerは未計数。構文由来の値も含むため、AST全構文不変の証明ではなく、nodes/children/listsの借用前提の観測である。
並列writerを防ぐ強制契約もまだない。初期/最終配列長の一致だけで途中の変更を否定しているわけではない。

## 次に絞る設計

**対象Storeの構文読取区間を明示し、既存callerが解決したheader位置を必要区間だけ渡す。**

- Begin/Endの読取契約はnodes/children/list backing arrayの再配置と構造変更を対象にする。
- Flags・Symbol・Flowの意味更新は継続できる。Flagsは元と同じ時点でfresh readする。
- header位置の借用は短命なpointer/小さいview。全node cache、名前値memo、大きいcontextは追加しない。
- 新writerの入口を契約に必ず追加し、検査buildで違反を検出する。別Storeの正当な構築は妨げない。
- まずheaderを一度解決する現実のcaller経路を選び、構築・受渡し・spillを含めた利益を同一consumerで比較する。
  ChildRef単発の検査省略だけを次の高速化本命にはしない。

最大の未確定点は、header借用の保持費を回収する複数read区間と、その時間割合。
今回の監査から「借用すればpointer同等になる」とは結論しない。
次の性能実装は、上記契約の違反監査と対象区間の予算を満たした場合に進める。

## 実測基準・artifact状態

新規benchmarkは実行していないため、今回のprobe/契約候補のns/op・B/op・allocs/op比較はmissing。
以下は既存rawをbenchstatで比較した履歴基準であり、今回の新測定ではない。

| 入力 | 版 | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| checker | pointer | 12,458,733.5 | 7,425,344 | 13,954 |
| checker | Store | 19,064,295 | 12,799,524 | 14,165 |
| dom | pointer | 4,874,793.5 | 5,287,312 | 16,562 |
| dom | Store | 6,819,245 | 7,867,930 | 16,684 |

selected repo: /Volumes/SanDisk1TB/worktree/cursor-ast-store-tests、32598cba146fa4dd7b6162b838630c90d865ab28。
pointer checkout: /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript、8ac035a394c79e693a3a7d74cb170448503ee894。
tsgolint_git_revは両者null。candidate clones: flownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、
nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr-1〜5、store-pr-6-perf、
store-pr-7、store-pr-7-attach-parent-fix、store-pr-7-nodeseq-t10、store-redesign。全pathはworktrees.txt。別介入として不使用。

artifact set: .cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/remaining-accessor-audit/。
今回・参照censusは3 identity条件の一致を確認しcurrent。ただし保存baseline Binderと現在のdirty Binderは区別する。
source-hashes.json、census-source-identity.json、audit-writer/identity.jsonでsource・binary・overlayを管理。
probeは現在のast source、writer監査は保存baseline Binderを使う。production Goは変更していない。
新しいCPU profileはmissing、旧profileの今回候補への帰属はstale。要求regexなしのbench.txtを流用しておらずunsupported stageはない。

広いhot pathはwalk/helpersであり、今回の3getterはその一部。呼出回数をCPU占有率として扱わない。
allocation driverはsymbolIdx/flows列、FlowNode 32→48B、Symbol 96→104B、Handle 8→16Bで、今回のguard省略では残る。
GC改善、parse+bind全体、pointerとの同時採用比較は未実施。

## 再現・受け入れ条件

binder_remaining_accessor_audit.pyは既存census集計とwriter監査を生成する。
probe.go/probe_test.goとoverlay.jsonを使ってgo test -c ./internal/astを行い、probe.testの対象Testを実行、objdumpで比較する。
再実行は新規artifact先を使い、保存結果を上書きしない。今回の処理は計数と機械語確認で、benchstatへ渡せる新規速度rawはない。

次候補の採用には、読取契約の違反0と意味・訪問順一致、構築を含む総費用の改善、精度内の独立wall比較、
8入力非悪化と全phase/GC評価を必要とする。guard省略や近接read回数だけで採用しない。
