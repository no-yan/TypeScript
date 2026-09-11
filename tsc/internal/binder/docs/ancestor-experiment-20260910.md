# ancestorスタックの3者対照（2026-09-10）

## 問題・診断

現在のnodeの親ref/kindをStoreから繰り返し取得する代わりに、走査中の祖先を保持できるかを検証。
**今回の全nodeスタック＋識別子判定一経路の構成は不採用。再解決除去の利益より維持コストが大きい。**
checkerでは維持だけの対照より命令0.90%減だが、baselineより1.91%増。
利用箇所のないdomはbaselineより2.52%増。命令の差はそれぞれ有意。
全祖先スタックがすべての用途で不適という結論ではない。多数の祖先検索に共有する場合や、
再帰走査そのものを置換する場合の収支は今回測っていない。

## 選択repo・artifact状態

選択Store repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests` @
`32598cba146fa4dd7b6162b838630c90d865ab28`。
pointer候補: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript` @
`8ac035a394c79e693a3a7d74cb170448503ee894`。TSGolint revisionはnull。
repo_root / tsgolint_git_rev / typescript_go_git_rev一致を確認しcurrent。
全候補clones（flownode、store-redesign、store-nolock-exp、lock-profile、profile、store-pr-*等）は
worktrees.txt。別介入のあるcloneを対照には混ぜない。

artifact set: `.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/ancestor-experiment/`。
identity-check.json、各build identity、dirty.patch、未追跡Go hash、overlay/source/binary hash、
fixture hash/manifest、protocol、campaign、raw、benchstat、assemblyを保存。
currentというrevision条件とdirty variantの同一性は分ける。
ユーザーの本体変更を保持し、保存baseline overlayで既存PropertyAccess候補を除外した。
今回の本体介入はoverlayのみ。新しいpointer対照・Instruments profile・GC/phase評価はmissing。
過去のstale profileを今回の因果根拠にしない。pprofは使用しない。

## 仮説・提案修正・実験条件

Storeの永続状態ではなくBinderに `[]bindNode{ref,kind}` を追加。
bindKindの入口でappendし、唯一の正常return直前で元の長さへ戻す。
既存の再帰走査は維持する。スタックは初回利用から必要に応じて拡張し、
既存putBinderのリセットで解放対象になる。固定容量の事前確保や保持poolは混ぜない。

3者は以下。2入力、10 bind × 6 rounds、6通りの順序を各1回、探索的対照。

- baseline: 元のStore binder。
- control: 全nodeで祖先スタックを維持し、取得は従来のまま。
- candidate: 同じスタックを維持し、isIdentifierNameRefだけで親ref/kindを再利用。

candidateはquery refとstack最上段の現在refが一致し、depth>=2のときだけ直前frameを利用する。
それ以外は元のParentRef/KindAtへfallbackする。親のname/child判定・keyword・診断条件は維持。
追加fieldはBinder先頭に配置されるため、baseline対controlはpush/popだけでなくBinderの配置変更も含む。
control対candidateは同じfield配置と同じ維持処理であり、取得側の置換を切り分ける。

Go1.26.0、CGO=1、GOMAXPROCS=8、GOMEMLIMIT=off。
通常wallはGOGC=100、KPCはGOGC=off、別セッション。parse/強制GCはtimer外。
KPCは各binaryで新規process自己検証2回、v7 baseline/event hash一致を確認。
fixed EL0+EL1は別指標。通常GC wallとは混ぜない。過去30 bind A/Aを今回10 bindの保証にしない。
全rawを保存しbenchstat。補助集計はround対応mean-log-ratio bootstrap20,000回。
結果を見てround数を延長せず、外れ値も除外しない。

## 親列・正しさの監査

最初にprobe binaryで全bindKind入口の直前frameと実際のParentRef/KindAtを照合した。
8実入力＋既存9境界入力で不一致0。さらに180段のif、遅延expando処理を含むJS、
分割代入と入れ子関数の3入力でも不一致0。
全20入力の構文・診断・Symbol・CFG・訪問順等がbaseline/control/candidateで一致。
AST/binder/compiler単体テスト成功。stackなし・別node問い合わせのfallbackテストも成功。
コンパイラ全回帰試験と任意の全構文に対する不変条件の証明は未完了。
既存pointer対Storeの匿名class Symbol.Name差を解消したという意味ではない。

| 操作数 | checker | dom |
| --- | ---: | ---: |
| bindKind / push回数 | 298,054 | 109,605 |
| isIdentifierNameRef query | 123,553 | 0 |
| stackで取得可能なquery | 123,553 | 0 |
| candidateのParentRef削減 | 123,553 | 0 |
| candidateのKindAt削減 | 123,553 | 0 |

probeによる確認のためのParentRef/KindAtは別binaryで実行し、timed binaryには入れない。
上表のParentRef/KindAtは同じ親解決の要素なので、独立した全体寄与として足さない。
domではambientの条件により対象queryが実行されず、維持コストの負の対照になる。
遅延処理で過去の祖先がスタックに残ることを期待しない。任意nodeに適用する汎用親APIにはしない。

## 実命令数・cycles

GOGC=off、EL0、中央値。pはbenchstat、n=6。

| 入力 | baseline inst/op | control inst/op | candidate inst/op | control対baseline | candidate対control | candidate対baseline |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| checker | 185,309,372 | 190,556,144.5 | 188,847,895.5 | +2.83%, p=.002 | −0.90%, p=.002 | +1.91%, p=.002 |
| dom | 77,725,097 | 79,404,323 | 79,686,823.5 | +2.16%, p=.002 | +0.36%, p=.093 | +2.52%, p=.002 |

checkerの同一セッション中央値差では、維持等の追加約5.25M instに対し、取得置換の削減は約1.71M。
粗い収支として約3割しか取り戻せていない。これは各命令の厳密な帰属ではなく、3者の全体差による比較。
domのcontrol/candidate差は非有意で、実行されない再利用分岐に効果を帰属しない。

| 入力 | baseline cycles/op | control cycles/op | candidate cycles/op | candidate対baseline / p |
| --- | ---: | ---: | ---: | ---: |
| checker | 86,222,437.5 | 87,956,281.5 | 87,086,969 | +1.00% / .093 |
| dom | 32,203,072.5 | 32,877,748.5 | 33,053,875.5 | +2.64% / .065 |

control対baseline cyclesはchecker+2.01% (p=.009)、dom+2.10% (p=.065)。
candidate対controlは−0.99% (p=.132) / +0.54% (p=.180)。cycles改善は未確認。

## 通常GCの時間・割り当て

| 入力・variant | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| checker baseline | 16,411,347.5 | 12,799,537 | 14,165 |
| checker control | 16,918,329 | 12,800,572.5 | 14,172 |
| checker candidate | 16,387,314.5 | 12,800,558 | 14,172 |
| dom baseline | 5,725,348 | 7,866,640 | 16,684 |
| dom control | 5,816,890 | 7,868,817 | 16,689 |
| dom candidate | 5,803,993.5 | 7,870,692.5 | 16,689 |

candidate対baseline wallはchecker−0.15% (p=.699)、dom+1.37% (p=.394)、非有意。
control対baselineは+3.09% (p=.132) / +1.60% (p=.394)。
candidate対controlは−3.14% (p=.132) / −0.22% (p=.818)。対照内の3%中央値差を採用条件達成としない。
candidate対baseline paired95%区間はchecker [−0.62%, +0.70%]、dom [−1.00%, +1.89%]。
controlのcheckerには大きなばらつきがあり、bootstrapの一部区間が0を跨がなくても
事前指定benchstat判定から都合のよい指標へ切り替えない。

allocs中央値はchecker+7、dom+5。スタック拡張が新たに加わる。
B/opの差全体をstack容量に帰属しない。他の割り当て量も変動する。
要素は整数のみの8 byteでnoscanだが、slice headerにはpointerがありBinder自体はscan対象。
今回の構成は既存GC対象を減らしておらず、GC高速化の根拠はない。

## 生成コード・hot path・allocation driver

bindKindに容量確認・要素書込み・length更新・拡張slow pathが追加された。
ParentRef/KindAtは元からinlineされており、getter CALLを消したという説明はしない。
candidateの局所経路は親header列の取得をstack値に置き換え、fallbackには元の取得を残す。
bindKindのframe144 B、isIdentifierNameRef32 Bは3者で不変。
静的コード量は実行命令数の代用にしない。

広いhot pathは既存Instrumentsのwalk/helpers。今回の狭い消費箇所はisIdentifierNameRef。
全walkへ維持処理を入れるため、消費箇所の改善だけでは採用判断できない。
既存allocation driverはsymbolIdx/flows列、symbolRefs、FlowNode32→48 B、Symbol/DeclarationsのHandle拡大。
これらは今回縮めていない。symbol syntheticは使用せず実入力を測った。

## 再現・採用条件・次の行動

probeはartifact内probe.py。候補生成は `tools/scripts/tsc/binder_ancestor_experiment.py`。
再現時は保存overlay/binaryとcampaign.pyの環境・fixture・順序を使い、新しい出力先に保存する。
追加3入力はextra.py、fallback/assemblyはverify.py。KPCは管理者権限の独立セッション。
wall-6、kpc/kpc-6内の3pairで `benchstat <old>.txt <new>.txt`。既存rawは上書きしない。

本候補は保留し、本体不採用。全nodeスタックの使用先を証拠なしに増やさない。
今回必要だったのは直近の親だけであり、次に試すなら既存parentKindにparentRefを添える等、
祖先sliceを持たず呼び出し側の既知情報を渡す最小構成が優先。
深い祖先検索にstackを使う案は、残存query数と削減余地から維持費を回収できる見込みを先に出す。
「再解決を省く」は支持されるが、「全nodeで汎用状態を維持すれば得」は今回反証された構成がある。

採用には事前対象wall有意3%以上、inst/cycles低下、独立確認、8入力非悪化区間、全回帰、
parse/bind/parse+bind、通常GC CPU/assist/scan/liveを要する。pointer同等とGC高速化は別判定。
owner＋nameの独立確認と解決済み名前core化は未完了のまま保持する。
