# Binder用アクセサ設計 v2：構文読取契約・直接getter・必要な区間だけのSlot/Span

2026-09-10。[レビュー](binder-slot-accessor-review-20260910.md)の5点を反映。既存artifactのcall-site集計と構文writerの静的監査を追加した。本アクセサ候補の実装・性能測定・writer guardの動的検証は未実施。先行する[walker codegen対照](walker-codegen-experiment-20260910.md)は終了したが、分割の高速化は未証明で本体不採用。

**基本形は、既存b.storeに結び付く構文読取区間と、schemaから生成する直接getterである。位置を複数回使う区間だけSlot/Spanを比較し、安定した配列への通常のGo pointer/slice借用も候補に含める。** 型付きviewの採用は前提にしない。永続AST、Binderの判定、診断、Symbol/CFG処理、functions-firstの2passを維持し、名前・Flags等の値のmemoを主比較へ持ち込まない。

これはpointer同等を目指す設計であり、同等以上を実証した設計ではない。現在の証拠では、アクセサ変更だけで目標へ届くとの保証はできない。既存ノートの数％の改善を足し合わせても、その保証にはならない。

## 1. 問題・証拠・必要な削減量

選択repoは `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、Store revisionは `32598cba146fa4dd7b6162b838630c90d865ab28`。pointer対照は `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、revisionは `8ac035a394c79e693a3a7d74cb170448503ee894`。TSGolint revisionは両者とも `null`。今回、両HEADと保存identityの3項目を照合した。

候補cloneは、同じworktree親ディレクトリの `flownode`、`land-44-45`、`lock-design-inv`、`lock-profile`、`merged-symbols-guard`、`nolock97`、`opt-merge-symbol`、`profile`、`putcol-bce`、`store-nolock-exp`、`store-pr-1`〜`store-pr-5`、`store-pr-6-perf`、`store-pr-7`、`store-pr-7-attach-parent-fix`、`store-pr-7-nodeseq-t10`、`store-redesign`。今回のworktree一覧も保存済み一覧と一致した。別介入を含むため対照にしない。全パス・revisionは [worktrees.txt](../../../../.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/worktrees.txt) を参照する。

以下、artifact setを `A = .cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/`、`K = .cursor/skills/verify-tsc/artifacts/20260910-kpc-reliability/campaign-v7/` とする。いずれもrepoルートからのパス。

| artifact / stage | status | 今回の扱い |
| --- | --- | --- |
| Aのstore/pointer identity、paired-8-20 | `current` | identity照合、対象Benchmark行、保存benchstatを確認 |
| A/kpc-build-v7、K/paired-instructions、K/paired-memory | `current` | 両build identityとrawの対象Benchmark行を確認 |
| A/list-span-experiment、A/parent-argument-experiment | `current` | identity照合。個別ノートに記録された機構と制約を参照 |
| 旧eval・旧Instruments | `stale` | この設計の効果の根拠には使わない |
| 初期KPC v5のBenchmark stage | `unsupported` | 既存KPCノートで対象行なしと記録。性能比較から除外 |
| A/audit-sites、今回の[集計inventory](artifacts/slot-accessor-design-v2/inventory.json)の入力 | `current` | identity照合。8入力の各accessorについてcall-site合計と総数の一致を確認 |
| 本設計candidateのwall / KPC / 全phase / 通常GC | `missing` | 実装前のため数値比較できない |

`current`は `repo_root`、`tsgolint_git_rev`、`typescript_go_git_rev` の一致だけを表す。dirty source、overlay、binary、fixtureの同一性は別に扱う。既存PropertyAccess変更を含む現在のdirty treeと、保存baselineの測定値を同一視しない。今回もStoreのbinder SHAは不一致、pointerのbinder SHAは一致した。現在のsource SHAと入力artifact SHAはinventoryへ保存した。

通常GCの既存比較は次のとおり。`A/paired-8-20/{pointer,store}.txt` に対する保存 `benchstat.txt` を使用した。この表は新しいwalker比較や本アクセサcandidateの測定値ではない。

| 入力 | 版 | ns/op | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| checker.ts | pointer | 12,458,733.5 | 7,425,344 | 13,954 |
| checker.ts | Store | 19,064,295 | 12,799,524 | 14,165 |
| dom.generated.d.ts | pointer | 4,874,793.5 | 5,287,312 | 16,562 |
| dom.generated.d.ts | Store | 6,819,245 | 7,867,930 | 16,684 |

時間差はbenchstatで各p<.001、n=20。pointerの時間に届くには、このStore baselineからchecker **34.65%**、dom **28.51%**の短縮が必要。これは既存時間比から求めた目標値であり、candidateの予測ではない。別条件の1.7倍を出発点にする場合は41.18%が必要になる。

広いhot pathは既存Instrumentsの **walk / helpers**。狭く操作できる場所はその内部のheader、child/list、名前のスロット解決であり、最も広いhot path全体とは異なる。生成walker内のChildRefはcheckerで全558,230回中189,849回、約34%に過ぎない。そこだけ直して全アクセサを改善したとは扱わない。symbol syntheticは実workloadの主因の証拠にせず、本設計の最初のmicroにも使わない。

別セッションのKPCではcheckerのEL0命令が110.94M→185.30M、domが49.14M→77.76M。L1D load missは減ってもload/storeと命令数が増えている。診断は「局所性の利益を、表現を解く仕事が食っている可能性が高い」。どの機構が時間差の何割かは未確定で、追加命令をすべて本アクセサで除けるとは置かない。

根拠: [insights](binder-investigation-insights-20260910.md)、[入力別結果・割当帰属](binder-investigation-results-20260910.md)、[KPCとcall-site監査](kpc-binder-results-20260910.md)。

## 2. 比較の公平性をAPIの制約にする

既存の[解決済み情報を渡す設計](resolved-access-design-20260910.md)より、今回の主比較は範囲を狭める。「意味が同じ」だけでは十分ではなく、pointerにも適用できる共通処理の削減を混ぜない。

| 変更 | 主比較での扱い | 理由 |
| --- | --- | --- |
| `NodeRef → header → childStart` の結果を保持 | 対象 | Storeの物理アドレス計算を償却する |
| local list slotの32bit値から直接spanを取得 | 対象 | StoreIDの付与・再検証という表現変換を省く |
| list headerのstart/lengthを一走査中保持 | 対象 | pointer版の既存slice相当の入口を作る |
| name ref/kind/textのmemo、名前判定結果の共有 | 主比較から除外 | pointerでも同じ読み取り・計算を減らせる |
| 親情報の引数追加、Flags再利用 | 主比較から除外 | 共通情報フローの改善になり得るうえ、既存実験で費用が問題 |
| functions-firstを1pass化、事前分類、訪問省略 | 対象外 | 共通の処理量・走査アルゴリズムを変える |
| Symbol merge、map、CFG、文字列処理の改善 | 対象外 | アクセサの表現コストから分離できなくなる |
| FlowNode / Symbol / Handleの縮小 | 別の表現実験 | 正当な候補だが、アクセサ効果とは別に評価する |

主比較では同じ論理field read、判定、Symbol操作、Flow生成、訪問順を維持する。監査では「Nameを要求した回数」と「そのためのheader解決回数」を別々に数える。減らすのは後者。compilerが結果的に重複ロードを除く可能性はあるが、ソースで共通の名前判定をmemoへ置き換えない。

汎用helperのdispatch削減などpointerにも同等の改善を移植できる変更が必要になった場合は、その変更を主候補から外すか、P0/PcommonとSaddr/Saddr+commonの同一セッション対照を追加する。Storeの最終candidateと未測定の「最適化pointer」を比較したことにしない。過去のowner＋nameのdom −3.32%は機構の背景であり、本設計やStore固有の優位性の効果量ではない。

## 3. 目標へ結び付ける対象範囲と削減予算

### 保存call-siteデータで確定できた範囲

入力は `A/audit-sites/results/*.json`。そのidentityは選択checkoutに一致するが、計数専用overlayのデータである。新しい動的計数ではない。[inventory.json](artifacts/slot-accessor-design-v2/inventory.json)に8入力の総数・全caller別計数・入力hash・今回のsource hashを保存した。以下のread数はアクセサ入口回数であり、冗長な機械語load数ではない。

| 経路・対象caller | checker回/bind | dom回/bind | 最初に省く仕事／再利用区間 | 残る仕事と追加費用 |
| --- | ---: | ---: | --- | --- |
| 生成walkerのChildRef | 189,849 | 74,321 | schema既知のchildLen等の再検証。複数fieldを読むnodeでは一handler内のbase候補 | child ref取得、子headerのkind、再帰。base保持は別対照 |
| その他の生成helperのChildRef | 206,569 | 113,571 | nameRefGenerated、expressionRefGenerated、initializerRefGenerated等のcase内で直接read | 同じkind dispatchと論理query、子のkind/text取得を維持 |
| 手書きhelperのChildRef | 161,812 | 8,446 | binaryOperatorKindAt、bindBinaryExpressionFlowRef等。既知shapeを証明したcallerだけ対象 | generic/nullable入口の検証。範囲全部がtrustedとは未確認 |
| bindListRefのListElem | 50,646 | 35,424 | 一走査のowner/start/length | 要素とkindのread、bind。span構築・保持 |
| functions-firstのListElem | 49,632 | 5,838 | 同じlistの2passをまたぐowner/start/length | 2回の全要素read・kind判定をそのまま維持 |
| その他のListElem | 3,900 | 6,699 | case/switch、modifierFlagsRef、hasExportDeclarationsRef等。各走査に限定 | 各loopのearly exit・意味処理を維持。初回対象外 |
| ListSlotAt全体 | 110,158 | 87,747 | 既知slotの直接readと、需要地点でlocal listを直接開く | slot値のread。0時のmissing/foreign分岐 |
| listRef内部のID取得 | 54,461 | 21,960 | 上記直接入口と接続できた場合だけ資格付与を省く | 全件が接続できるとは未確認。ListSlotAtと別の独立利益として足さない |
| listOwner全体 | 170,288 | 69,924 | 要素ごとの再解決を走査入口へ移す | 入口のowner確認またはlocal格納規約の証明。ListElem/ListLenと重複する計数 |

ChildRefの3行は排他的で、合計558,230 / 196,338。walker＋生成helperは396,418 / 187,892で、全ChildRefの約71% / 96%。これは直接getterを検討できる**候補範囲の上限**であり、全入口のnonzero/shapeを証明した採用範囲ではない。

ListElemの上位2loopは100,278 / 41,262、全体の96.26% / 86.03%。同じ2loopのListLenはそれぞれ、bindListRefが32,874 / 18,172、functions-firstが9,688 / 3。このListLen数を、0を含む他callerのListLen全体へ一般化しない。

優先範囲は「生成walkerだけ」から、**schema既知の生成helper群と、上位2つのlist loopに共通するprimitive**へ修正する。名前helperで名前の値をmemoすることは含めない。初期のprimitive確認と、この広い範囲への横展開判断を分ける。

### どれだけ削れるかと、何がまだ分からないか

| 残存経路 | checker回/bind | dom回/bind | 本案で自動的には消えない費用 |
| --- | ---: | ---: | --- |
| KindAt | 688,528 | 200,203 | 子ref→header、必要なkind read |
| FlagsAt | 530,841 | 164,574 | 最新の可変Flags read。値のmemoはしない |
| ParentRef | 160,977 | 13,113 | parent readと、続く親nodeの解決 |
| TextAt | 131,978 | 24,252 | intern id→offset→text、文字列処理 |
| Symbol/Flowとその他Handle経路 | この計数からは未分離 | この計数からは未分離 | dense index→pointer/arena、payload拡大、semantic allocation、一般helperのdispatch |

KindAt等の一部はChildRef後に呼ばれるため、これらを排他的なCPU帰属として合算しない。viewがheaderの再取得を省けても、必要なkind/Flags readまでなくなるとは数えない。

計画に使う式は、排他的な操作クラスjについて、`予想Δinst = Σ(対象回数j × 実測した1回あたり削減j) − open/保持/dispatchの追加命令`。親子に入れ子のListSlotAt/listRef/IDやListElem/listOwnerを複数のjへ重ねない。現時点では1回あたり削減と保持費が `missing` なので、総削減命令やwallを数値予測しない。

後続の[-B診断対照](binder-bounds-check-experiment-20260910.md)では、元walkerの命令がBinderのみ-Bで−4.65% / −3.57%、Binder＋astで−6.72% / −4.81%となった。ただし広いpackageの自動境界チェックと周辺codegenへの介入で、単一accessorや上表の操作クラスjへ全額配分できない。手書きslot検査は残る。これは安全なBCE・検査償却を検討する根拠であり、直接getterの削減単価・厳密な上限・wall予測としては使わない。

桁の点検として、既存KPCの追加命令はchecker 74,362,789、dom 28,619,572。全追加命令を取り戻すと仮定するとwalker ChildRefだけで約392 / 385命令/read、全ChildRefでも約133 / 146命令/readを除く必要がある。仮に全ChildRefで10命令/readを除いても5.58M / 1.96Mに留まる。10は実測値でも上限でもない。これらは**命令数の感度計算**で、wallの限界や必要命令削減の証明ではない。

wallについては、変更可能部分の時間割合fとその短縮割合rに対し、単純モデルでは `f × r ≥ 0.3465 / 0.2851` が必要。例えばf=50%ならその部分を69.3% / 57.0%短縮する必要がある。fも未測定で、Instrumentsのexclusive比率へ別セッションのwallを掛けて埋めない。実callerを使う機構対照と、処理量を揃えたCPU帰属から予算を更新する。

**現時点の判断は、型付きviewだけで約35% / 29%を回収できる根拠は不足している、である。** 直接getter＋list primitiveの広い候補範囲を確定したが、その範囲だけで十分という判断もまだ出さない。次の規則を横展開のgateとする。

1. 最小経路で生成コードと動的命令の正味の利益を確認し、上表の実際に同じ構造を持つcallerだけに適用範囲を限定する。
2. その範囲の操作数と測定した削減単価から、重複なしの予算・区間・残差を記録する。microからの外挿は仮説として表示し、統合実binderで確認する。
3. 予算が不足するなら、新しいnode型を順番に試さない。残差の帰属に応じて、child→headerの二段参照、text、Symbol/Flow/Handle表現のどれを次に調べるかを決める。
4. 子側依存loadが残るならchild record/参照表現、semantic allocationが残るならFlowNodeの参照やdeclaration Handle等の表現縮小を**別介入**として検討する。過去のpacked record等のstale結果だけで再採用しない。1pass化や共通の名前memoへ逃げない。

3%の実binder改善は局所候補の採用条件として維持するが、それだけでpointer同等計画の横展開を正当化しない。

## 4. 構文の安定性と意味情報の可変性を分ける契約

### scopeとowner

新しい契約はStore全体のFreezeではなく、**parse完了後、同じb.storeの構文を読む同期区間**。提案scopeは `bindSourceFile` のRegisterFile・PrepareBindTables後から、`bindRef`と`bindDeferredExpandoAssignments`を終えるまで。終了時に借用を失効させ、Binderをpoolへ返す前に必ず閉じる。開始・終了はO(1)とし、全ASTを再走査するpassは追加しない。

このscopeでnodes/lists/childrenの配置と構文fieldを固定する一方、既存のFlags・Symbol・Flow等の更新は許す。`*nodeHeader`が指す同じrowのflagsは更新され得る。header全体を値コピーしてFlagsをsnapshot化しない。

`b.store`を再度ラップして全nodeにReaderを配る必要はない。source/rootの対応はParseTreeRefから一度得て、owner切替はgeneric境界で扱う。NodeRef/slotだけでownerは証明できないので、同じscopeのconsumerへしか渡さない。検証buildではStore ownerとgenerationを記録し、wrong Store、終了後、Restore後の再使用を検出する。releaseの各view/getterへowner/epochを追加する設計にはしない。

現在のBindOnceは同じfileの二重bindを防ぐが、全構文writerを排除するlockではない。`LockParseStoreWriter`も存在するが、今回の検索ではBinderから使われていない。従って「既存leaseが構文の不変性を保証済み」とは置かない。Storeの単一writer契約と、scope中に同じStoreの構文を書かない契約を、以下のwriter guardとcall graph監査で確立する。同じStoreへの並行構文変更を新たに許容しない。

### writerの初期静的監査

ソースは [store.go](../../ast/store.go)、[store_factory.go](../../ast/store_factory.go)、[ast.go](../../ast/ast.go)、[parser/jsdoc.go](../../parser/jsdoc.go)。表は今回確認した主要writer系統とscope中の規則。全推移的call graphの検証完了を表すものではない。

| writer系統 | scope中の扱い | 根拠・guardを置く場所 |
| --- | --- | --- |
| Alloc/AllocSlots→appendSlots、AllocList→appendList | 同じparse Storeでは禁止 | nodes/lists/childrenの追加・再配置。内部appendはmustMutateを通らない |
| FinishParse、setIdent | 禁止 | pos/end/flagsの複合書込、childStartのintern id更新。parser専用入口にもguardが必要 |
| linkChild/linkChildRef/linkList | 禁止 | childrenと子parentを直接更新。mustMutateだけでは捕捉できない |
| SetChild/SetList/SetListSlot/setListSlot、SetListAt | 禁止 | 既存slotの内容変更と親付与。lenが同じでも違反 |
| SetParent、SetExternalChild/SetExternalListAt、attachSameStore/attachList/SetParentsInChildren | 禁止 | local/foreign構文edgeと保持mapの変更 |
| SetLoc、setListLoc、SetTokenFlags | 禁止 | 構文位置・token情報の変更 |
| Intern/intern、SetIdent、SetStringValue | 禁止 | intern配列の追加・既存nodeの文字列情報変更 |
| SetUintValue/SetObjectValue | 構文slotは禁止、意味slotは明示分類 | operator等の構文scalarとFallthroughFlow等の意味情報を同じ許可にしない |
| Restore | 禁止 | 切詰め・領域再使用・side table削除。途中の違反も捕捉 |
| Compact、scratch再使用 | 禁止 | 長さを保って配列を置換し、旧配列をscratchへ返す。開始/終了のlen比較では検出不能 |
| Factory.ListRefsのcopy、生成Factory/CopySubtree/Flatten/visitor更新 | 禁止された下位writerへ到達させる | ListRefsはchildrenへ直接copyも行う。bulk writeを監査対象に含める |
| SetParseStore/SetParseRoot、Storeの再登録・owner差替え | 対象file/Storeの差替えを禁止 | b.storeと公開rootの乖離やborrowの別owner流用を防ぐ |
| Seal/Freeze | scopeの前後で実行 | Freezeは意味更新まで禁止するので前倒ししない |
| SetFlagsAt/SetFlags | 許可 | 現在のBinderが更新する可変bit。更新を省略・先読みしない |
| PrepareBindTables、NewFlow、SetSymbol/SetLocalSymbol/SetFlow/SetEndFlow/SetReturnFlow/SetLocals/SetNextContainer | 許可 | semantic列・map・arenaだけを更新し、構文配列を再配置しないことを検証 |
| Symbol field、diagnostic、SourceFileのbind metadata更新 | 許可 | 構文fieldへ副作用を持つものは別分類 |

混在するSetObjectValueは(kind, slot)のschema分類で扱う。`SetFallthroughFlowNode`は意味情報だが、同じSetObjectValueのJSDoc text等は構文情報。名前だけで「setterはすべて禁止」「mapはすべて許可」としない。

今回Binder本体で直接確認した更新は上表の意味情報系統で、構文append/slot変更/Restore/Compactは見つからなかった。parserは返却前にCompactとSealを実行する。遅延JSDocはJSDocCache.storeまたはwarmSharedJSDocの新しいside Store、GetOrCreateTokenも専用tokenFactory Storeへ構築することをソースで確認した。hostへの親edgeはside Store側へ書く。**任意のJSDocCacheにparse Storeが渡されないことや、全helperの副作用まで証明したわけではない。** GetNameOfDeclaration、module/assignment helpers、scanner診断、callback、生成アクセサ、Factory hookを含む推移的監査が残る。

### 契約の実装・検証方法

最初は計数専用buildでStoreごとの構文読取depth/generationを持ち、上表の**変更の直前**にwriter guardを置く。ノード別cacheは不要。append、copy、slice差替え、map更新を含み、入口/出口のlen比較だけに依存しない。違反時はStore・writer・caller・操作種別を記録して失敗させ、実行後に見えなくなった変更も無視しない。unknown writerは既定で未分類として扱う。

動的監査は代表8入力・既存境界入力・遅延JSDoc・診断token・deferred bindを通し、同じparse Storeの構文writerが0件、意味更新は従来どおりであることを確認する。意図的なSetChild、append、Restore→再append、Compact、wrong owner等でguard自体が発火することも確認する。監査コードが呼ばれなかっただけの0件を不変性の証拠にしない。生のslice取得APIを増やす場合は書込可能なaliasの流出も監査する。

release候補では構文writer側へ契約違反チェックをまとめる方式を比較する。意味setterの既存mustMutateは維持し、getterにscope判定を入れない。parser用内部writerにもチェックを置くなら、そのparse時間増加を含めて測る。チェックを検証buildだけに残す案は、producer/call graphの証明とAPI閉域性を別途必要とし、debug成功だけでreleaseの安全性が保証されたとはしない。

同じparse Storeへの必須構文変更が見つかった場合は、全bindをborrow可能とはしない。全borrowが閉じる境界までscopeを狭め、その経路は既存genericまたは整数方式に戻す。**生きたpointer/sliceがあるままwriterを通し、後からfallbackする設計は禁止**。整数方式もslotの切詰め・再使用には無条件には安全でない。

## 5. 既知shapeの直接getterを最初の比較対象にする

既存Handle.childAtと同様に、生成producer/dispatchからshapeが分かる入口では、childLenを再確認せず読む。次はrelease primitiveの疑似コードで、未実装。

```go
// 契約: sはこのbindのStore。ref != 0で生存し、Parameterのshape。
// shape/nonzeroの証明はproducer/dispatch、検証buildで照合する。
func (s *Store) BindParameterName(ref NodeRef) NodeRef {
    return s.children[s.nodes[ref].childStart + slotParameterDeclarationName]
}
```

nil/ref=0を許すgeneric helperは、その境界で必要なguardを残す。既知kindであってもref=0を排除できなければ直接primitiveを使わない。生成名helperの各caseは直接getterへ下ろせるが、既存のkind switch、Name要求回数、fallbackは維持する。nonzero/shapeのdebug照合に成功しただけで、公開AllocSlotsで任意shapeを作るcallerまでtrustedにしない。

これは `TryBindParameterSlots`でkind/childLen/listLenを再確認してviewを作るv1の必須入口を置き換える。Goの配列境界検査は維持し、任意slotを受け取る公開ChildRefの検証は残す。missing/externalの0表現も変えない。単発getterにも改善余地を認め、base保持を前提にしない。

schema既知により省く手書き検査と、Goが挿入する配列境界検査は別々に機械語で確認する。前者だけを省いても、上記-Bの削減率を得られるとは置かない。通常buildでコンパイラが検査不要と証明できる区間を作れるかをD/I/Bで比較し、-B自体は候補へ持ち込まない。

generatorは [generate-go-ast.ts](../../../../tools/scripts/tsc/generate-go-ast.ts)。新しいgetter、既存bindwalkのcase、手書きの既知shape入口を同じschema定数へ結び付ける。生成物だけ手修正せず、意味ロジックは生成しない。

## 6. Slot/SpanとGo借用を必要な区間だけ比較する

### 同じfield需要に対する二つの保持方式

| 方式 | 保持値の例 | 使える条件 | 主な収支 |
| --- | --- | --- | --- |
| D: 直接getter | 既存ref/kindのみ | trustedなfield read | viewなし。header/baseの再取得は残る |
| I: 整数Slot/Span | base、またはstart/length | 同じowner、slot identity/範囲が安定 | 現在のs.childrenを読む。配列appendだけなら再配置にも対応可能。引数/保持/添字計算 |
| B: 通常Goのborrow | `*nodeHeader`、範囲を絞った`[]NodeRef` | §4の構文配列安定区間 | header再アドレス計算やStoreからのslice再取得を省ける可能性。pointer root、frame/spill、escape |

IもBもopenを最初の既存需要地点に置き、未使用fieldの値を先読みしない。子ref、kind、名前text、Flagsをcacheしない。getterは元のタイミングで最新値を読む。Bのheader pointerは同じrowへのFlags更新を観測できるが、headerの値コピーはその代替ではない。sliceは書込禁止の内部借用とし、appendや保持scope外への返却を認めない。Goの型だけではread-only/lifetimeを保証しないため、内部契約とguardで補う。

短命のpointer/sliceをstackで保持しても、参照先の整数配列の要素領域はnoscanのまま。ローカルGo 1.26.0の `runtime/mgcmark.go` のnoscan分岐でも参照先内部の走査は省かれる。ただしstackのpointer走査、参照先の生存期間、escapeして生じるheap allocationは別費用であり、実際に確認する。noscanを理由にBを棄却しない。unsafe/uintptr、全ASTのpointer mirror、O(nodes) cacheは不要。

### 最初に同じ経路で比較する範囲

codegen対照後の最初のnamed-child機構確認は `bindParameterFlowRef` の5つのchild需要を固定する。source上はdotDotDot、questionToken、type、initializer、nameの順で、initializerや他childのbindをまたいで位置がliveになる。modifier listの需要も元の位置に残す。

保存したslot0のcall-site数からこのhandlerはchecker 5,788 / dom 8,446回。5つのchild需要は **28,940 / 42,230 read/bind**。Dは5回の必要な値readとheader/base解決を行う。I/Bでnamed-child位置だけ保持する場合、最大4×handler回数、**23,152 / 33,784回の追加header/base解決を省ける形**になる。これはソース上の解決回数であり、DでもcompilerがCSEできた分は機械語の削減にならない。

modifier listの直接span化はこのnamed-child対照に混ぜない。I/Bへの接続で既存name/type helperのkind dispatchまで除くなら、Dにも同じdispatch変更を施す別対照を作る。最小primitive比較へ共通意味処理の特殊化を混入させない。

このhandlerだけでpointer差を回収する計画ではない。成功後の対象候補は§3の生成helper群だが、同じ再利用頻度・live rangeとは限らない。直接getterに対する位置保持の追加利益がなければ、Dを選びviewを増やさない。名前slotのoptional contextやnode型ごとの新しいviewは必要性が確認できるまで生成しない。

### list spanはnode viewと独立させる

node型別viewなしで次の二入口を用意し、同じlocal span primitiveへ収束させる。

```go
// schema既知のnode/list slotから直接開く。
OpenBindStatements(ref NodeRef, knownKind Kind) (localSpan, qualifiedFallback)
// 既にqualified ListRefを持つcaller向け。
OpenBindList(list ListRef) (localSpan, qualifiedFallback)
```

これはAPIの概念表記で、大きな複合戻り値を必須にはしない。I/Bそれぞれで小さい結果型とrare fallbackを比較する。前者はnode headerからslotを読み、非0のlocal list indexをそのまま `s.lists[index]` へ解く。既知schemaのchild数を使えればlistBaseのchildLen読出も不要。StoreID付与→直後のowner照合を経由しない。

slotが0ならforeignListsを確認し、missing/foreignを区別する。後者はownerを入口で一度確認する。foreign要素のNodeRefをb.storeのKindAtで解いてはいけない。qualified/Handleの既存fallbackを保ち、現行binderのsame-Store制約をこの最適化で解消したとは主張しない。

最初はbindListRef、その後にfunctions-firstの上位2loopへ同じprimitiveを検討する。functions-firstは同じlistを**2回全走査**し、各回で要素とkindを読み直す。spanの範囲だけを保持し、partition・1pass化・関数一覧作成をしない。過去のspanのwall非有意や特定slice実装の悪化は参照するが、新しいD/I/B全体の採否に読み替えない。

## 7. 対照の費用とcompiler最適化を監査する

既定のwalker codegen対照を先に実施した。親情報、直接getter、scope、viewを混ぜず分割とCALL化を比較し、[再計測02](walker-codegen-recheck-02-20260910.md)でKPC A/Aと3者比較まで完了した。分割は命令数の有意減なし、cycles +1.33% / +2.18%。CALL化は同じ分割比で命令数+1.00% / +1.07%。分割は採用せず、構成Cは元のwalkerに固定する。wall利益は未証明で、inline拡大そのものを成功条件にしない。

同じC・caller・構文読取契約で、次を比較する。各候補はsemantic workとfield需要を共通にする。

| variant | 内容 | 比較の目的 |
| --- | --- | --- |
| C | codegen条件を固定した従来アクセサ | 基準 |
| H | 構文読取契約だけ、従来アクセサ | begin/end、writer guard等の総費用 |
| D | H＋schema既知の直接getter、base保持なし | 位置保持なしで省けるStore固有コスト |
| I | Dと同じ経路で整数位置を必要期間保持 | Dに対する位置保持の正味の利益 |
| B | Dと同じ経路で安定配列へのGo借用 | I/Dに対する再アドレス計算と保持費の収支 |
| M（成立すれば） | candidateと同じ値を構築・保持するが再解決も行う | 補助的な因果分解。採否の必須対照ではない |

Mはソースにunused viewを書くだけでは成立しない。各control/candidateについて、コンストラクタの実在、base/pointerの生成、再帰をまたぐlive range、引数、stack保存/再load、frame、inline/CALL、escape、コード量を機械語・compiler診断で確認する。Mとcandidateで同じ保持が残っていることを、単にframeサイズが同じという理由で認定しない。

compilerがMの値を消した場合はその対照を棄却する。人工的なnoinline sink、global escape、volatile相当の読取、意味のないbranchで保持を強制すると、追加費用自体が介入になる。同じ費用を両者へ入れてもlive range/配置が一致するとは限らない。対称な対照を作れなければ、**I/B対Dを総費用差として報告し、保持費と再解決削減の厳密な分解とは呼ばない**。

Go 1.26.0では既存巨大walkerでChildRef cost36がbig caller上限20を超えた例がある。各consumerで実際のinline結果を確認する。小さい直接primitiveにしても、コンパイラが既にguard/loadを除いていれば改善はない。静的CALL数・命令行数・source上のgetter削減を動的命令へ置き換えない。

## 8. 再現・実装順序・正しさ

v2初稿で追加した作業は保存call-siteの集計と静的source監査、設計修正であり、inventoryは性能測定ではない。その後のwalker測定は[別ノート](walker-codegen-experiment-20260910.md)に保存した。inventoryは各accessorについて `site:<accessor>@...` の和が総数に一致することを8入力で検証して作成した。generated helper群はcaller名がGeneratedで終わる行、walkerはinvestigationWalkerChildRef、残りを手書き群とする。ListSlotAt/listOwner等を重複加算しない。

| 順序 | 次に完了する仕事 | gate |
| --- | --- | --- |
| 読取監査 | §3の既存計数を現在sourceへ対応付け、§4のwriter guard/call graph分類を準備 | 候補callerのowner/nonzero/shape、slot identityと契約scopeを確定。今回は静的初期監査まで |
| 1 | 既定のwalker codegen対照（動的対照完了） | 意味・論理read一致。再計測02でKPC比較完了、分割はcycles増で不採用。元のwalkerをCとして保持。今回のwall比較はmissing |
| 2 | Hのwriter監査、同じParameter経路のD/I/B | 構造安定性を確認してからBを有効化。Dと比べて保持の利益があるか判定 |
| 3 | node直接入口/既存ListRef入口、I/B spanを独立比較 | bindListRefで確認し、functions-firstへ進む前に予算を更新 |
| 4 | §3の範囲・残差gateを通った機構だけ統合 | C/H/Dとの総費用比較。局所効果の加算で統合結果を予測しない |
| 5 | S0 / 統合candidate / P0を同一セッション、独立再現、8入力、全phase/GC/回帰 | pointer同等・優位性と、GCの主張を個別判定 |

初期microを作るなら上記の実call-site列を最小単位とし、open/保持もtimerに含める。checksum、compilerのloop hoist/DCE、実再帰をまたぐ保持を確認し、microだけでは実binderの主因・優位性の証拠にしない。symbol syntheticは使わない。symbolが実workloadで支配的と分かった場合に初めて別途検討する。

探索規模は既存方針のchecker/dom、10 bind×6 roundsを事前固定し、codegen・意味監査・KPC自己検証を先に行う。wallとKPCは独立セッション。小規模の精度不足は保留として、有意になるまで延長しない。採用確認ではその測定条件のA/Aと事前指定の独立セッションを使う。

候補は保存baselineと独立overlayで比較し、既存PropertyAccess等のdirty変更を混ぜない。source/overlay/generator/fixture/binary hashを保存する。Go 1.26.0、CGO=1、GOMAXPROCS=8、GOMEMLIMIT=off、wall GOGC=100、KPC GOGC=offを引き継ぐ。rawを保存し `benchstat old.txt new.txt` で比較する。既存baselineの再集計はrepoルートで以下。

```sh
benchstat .cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/paired-8-20/pointer.txt .cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/paired-8-20/store.txt
```

本設計の候補harness/生成コマンドはまだ存在しない。既存 `binder_investigation.py` / `binder_list_span_experiment.py` は測定条件・保存方法の参考とし、新しいAPIを実装したものと扱わない。

正しさは代表8入力＋既存境界入力のsyntax、diagnostics、Symbol、node-symbol、CFG、各passの訪問順、論理field要求数、意味writerを比較する。別の計数系列で物理解決数を測る。AST/binder/compiler単体と全回帰、writer guardの故障注入、borrowのwrong owner/expiry、最新Flagsの可視性も確認する。scopeで禁止した構文slot更新の可視性は、borrow中はguardによる拒否を期待し、従来generic/整数経路では元の契約に従って確認する。

既存の匿名classのSymbol.Name差とbaseline回帰失敗は別の正しさ課題。Store baselineとの一致だけでpointerとの完全一致にはならない。修正を揃えた再比較まで、全体の意味同等性を主張しない。

## 9. メモリ・GC、採用条件、停止条件

永久表現のnoscan性は維持する。短命のpointer/sliceを使えることと、GCが速くなることは別である。既存allocation driverは次のまま。

| driver | 既存の差・帰属 | アクセサ案 |
| --- | --- | --- |
| node-wide symbolIdx / flows | checkerで各capacity 1,210,352 B | 維持 |
| symbolRefs | checkerでcapacity 302,584 B、pointer保持 | 維持 |
| FlowNode | 32→48 B。追加16 B×79,990個=1,279,840 B | 維持 |
| Symbol | 96→104 B | 維持 |
| declaration Handle | 8→16 B | 維持 |

容量余剰やallocator等の残差もあり、B/op差を表へ全額帰属しない。pointer/slice borrowのescape、stack root、保持時間、余分なstack growthも追加評価する。

局所採用は既定の実binder wall≥3%短縮・benchstat有意・inst/cycles低下・独立再現・8入力3%非悪化の区間確認・意味/全phase/GC gate。ただしviewはさらにDに対する正味の利益を要する。Dが同等以上ならDを選ぶ。局所採用と§3の到達予算gateは別である。

pointer同程度の判定は、事前に決めた許容帯（例えば±3%）へcandidate/pointerの区間全体が入ること。「同等以上／高速」は、対象入力を明示し、その区間上端が1.00以下／未満であることとbenchstatを確認する。非有意を同等性にせず、全入力への主張では各入力と多重比較を考慮した事前の判定法を固定する。

メモリアクセスとGCの両立は、同じ仕事量・保持条件でのmemory counterと、通常GCの累積CPU・assist・scan・live・総時間、parse/bind/parse+bindで判断する。writer guardのparser費用をtimer外へ隠さず、構文区間の確立を前phaseへのコスト移転にしない。BindInvestigationはparse/強制GCをtimer外に置くため、これだけでは通常GC改善を判定できない。CPU帰属はInstruments、pprofは使わない。

止める条件は、構文scopeを確立できない、DよりI/Bの総費用が高い、制御実装がDCEされて因果分解が成立しない、または到達予算が不足する場合。因果分解が不成立でも総費用比較は可能だが、結果の呼び方を変える。予算不足なら残存費用に対応する表現実験へ切り替え、1pass化や共通名前memoを同じ実験の成功にしない。

## 10. レビューへの対応

| 指摘 | v2での変更 | 未完了の検証 |
| --- | --- | --- |
| P1: 到達予算なし | §3の排他的call-site集計、直接getterの広い候補範囲、上位2list loop、残存費用、予算不足時の次の表現候補 | 動的削減単価、絶対CPU帰属、統合wall |
| P1: 有効期間がcaller任せ | §4の共通scope、意味/構文writer分類、low-level/bulk/Compact/Restoreのguard | 推移的call graph、writer故障注入と実入力監査 |
| P2: 直接getter対照なし | §5と§7でDを必須対照にし、Try/viewを基本形から外した。listは独立 | D/I/Bのcodegenと実binder対照 |
| P2: pointer/sliceの早期排除 | §6で安定配列のGo借用Bを追加。noscan、root、escapeを区別 | scope証明、借用の最新値・escape/GC費用 |
| P2: 三者対照のDCE | §7でlive range/spill確認を必須化。不成立なら厳密な保持費分解を棄却 | control/candidateの機械語照合 |

先行するwalker codegen対照では20入力の意味・訪問・アクセサ呼出数が一致し、inline/CALLの介入も機械語で確認した。初回と[独立A/A再確認01](walker-codegen-recheck-20260910.md)ではKPC精度が不足したが、[再計測02](walker-codegen-recheck-02-20260910.md)でKPCの動的対照を完了した。分割は命令数の有意減がなくcyclesが増え、本体不採用。同じ分割でのCALL化には約1%の命令増があった。元のwalkerは既にinlineしているので、CALL除去を現在のbaselineからの削減予算に加えない。分割一般の失敗とは断定せず、今回の構成Cは変更しない。

直近の次の行動は、上記writer監査とwriter guardの動的検証、候補callerのowner/shape契約確認である。その後は**元のwalkerを共通にして**同じ経路のD/I/Bを比較し、分割を前提にしない。測定条件を整え、次の独立protocolでは命令数とcyclesの可測性を別々に判定する。owner＋nameの独立確認は既存研究の未完了項目として保持するが、今回の主候補には組み込まない。


### setter検査削減の追加診断（2026-09-10）

[setter対照](binder-setter-check-experiment-20260910.md)を元のwalkerの4条件で完了。
Binder＋astの-Bにsetter phase/live/local所有権検査削減を加えると、EL0命令は追加−0.59% / −0.39%、
通常版比の合計−7.29% / −5.07%。通常GC wallは合計−3.03% / −1.30%の中央値だが非有意かつA/A不合格。
4条件の20入力監査と追加local所有権assert監査は一致。実験用overlayのみで本体不採用。
この小さい追加効果を主な到達手段とはせず、構文writer監査・直接getterの既定順序へ戻る。


### list要素配列分離の実Binder対照（2026-09-10）

[配置×Spanの4条件実験](list-layout-experiment-20260910.md)を完了。
分離だけの命令差−0.43% / −0.16%は非有意。共有配置Spanの命令−1.74% / −2.87%は有意。
Spanへ分離を追加しても命令・wallの追加改善なし。20入力監査、core test、scratch/Restore/CompactとSpan境界試験成功。
checker wallは分離−0.97%、Span−1.61%だが、前者はA/Aの中心ずれ約−0.88%と同程度。dom wallは精度不足。
分離は本体不採用。parse+bind費用は未測定。単純分離を広げず、構文読取契約と直接getter/Spanへ戻る。


### 生成walkerの直接list-slot Span対照（2026-09-10）

[4条件の実装対照](direct-list-span-experiment-20260910.md)を完了。
129箇所を同じhelperに接続するcontrol/directで、resolverのみ変更。20入力監査・core/境界試験成功。
直接版はcontrol比命令−1.13% / −1.62%だが、helper化自体が+0.59% / +1.21%。
既存Span比の純追加は−0.55%（p=.093）/ −0.43%（p=.041）。wall/cycles追加改善は未確認、wall A/A不合格。
本体不採用。list経路の細分化を続けず、残存ChildRef/KindAt/FlagsAtとwriter契約の削減可能量監査へ戻る。


### 残存3getterと構文writerの監査（2026-09-10）

[残存アクセサ監査](remaining-accessor-audit-20260910.md)を完了。8入力のcaller総数を再検証し、実Store codegen probeで単純guard省略の桁を確認。
KindAt/FlagsAtにowner再解決はなく、ChildRefの生成範囲は約71% / 96%。guard省略だけでは必要wall短縮の予算を裏付けられない。
27構文入口の20入力Bind監査で構文writer計数0、Flags更新はchecker4,980 / dom6,391回。意味・訪問順・既存accessor計数一致。
次の設計対象は構文読取契約とheader位置の短期借用。Flagsはfresh readを維持し、値memoにしない。
全writer/並列writer契約と保持費の回収は未証明。新規速度benchmarkは未実行、本体未変更。


### 整数child一括取得：If＋高頻度walker（2026-09-10）

[整数一括取得](syntax-scalars-experiment-20260910.md)をworktreeと生成元へ実装。ChildRef連続呼出しでは共有されなかったheader/startをStore内で一回解決し、NodeRefを返す。
Ifに加え147 case/291 siteへ展開。今回の共通PropertyAccess変更込みでChildRef削減135,117 / 74,171回。helper CALL0、walker frame 48→80 B。
3条件20入力一致、AST/Binder/Compilerテスト成功。実Binder命令−1.69% / −1.76%（各p=.002、A/A合格）。Ifだけからwalker追加分−1.38% / −1.51%。
dom cycles−2.04%（p=.041、A/A合格）。通常wall−1.40% / −1.89%は非有意、checker wall/cycles精度不足。
実装はworktreeに保持。全面採用・pointer同等・GC高速化は未証明。次は独立wall精度確認と構文snapshot契約、parse+bind/GC検証。


### API命名・生成方針の改訂（2026-09-10）

ユーザーの命名方針を反映し、今後のcallerは`AccessParameter(ref)`→`ParameterAccessor`のようなnode別生成APIを使う。
数付きSyntaxChildrenNの新規横展開は行わない。返却は名前付きNodeRef/ListRef fieldを持つ値snapshotで、内部でkind/schemaを検査しheader/startを一度解決する。
Walkは訪問処理の名前として区別する。既存arity APIは移行中の互換用に保持する。
[改訂実装指示書](luna-accessor-implementation-plan-20260910.md)へG0（生成）、G1（完了済みP1の移行）、G2（walker等の移行）を追加。
P0/P1の完了履歴とartifactは保持する。struct返却・未使用list解決・inline/spillを新たに検証し、旧結果を新APIの性能根拠にしない。今回の変更は計画文書のみ。
