# Binder 呼び出し再設計の実装・検証指示書

2026-09-15。[設計書](binder-call-architecture-20260915.md)の主案を、独立して採否判断できる変更単位で実装する。
各最適化の採用には、正確性、マイクロベンチでの削減、Binder 全体で明確な回帰がないことを必要とする。
KPC は通常の採用条件にしない。原則として累積候補の最終確認で一度行い、途中では原因を解決する必要があるときだけ使う。
本書の作成時点では実装・新規ベンチマークを開始していない。

## How to read this

チェックは証拠を保存してから付ける。実装しただけ、ビルドできただけ、benchmark が動いただけでは採用しない。
本文を手順書として実行し、設計理由は設計書を読む。担当者は英語で考え、日本語で報告する。

この依頼では次の優先順位を使う。

1. 最新のユーザー指示。本書の micro＋Binder gate と低頻度 KPC を適用する。
2. リポジトリの意味・所有権・寿命の契約と最新の性能採用方針。
3. [設計書](binder-call-architecture-20260915.md)の API と変更境界。
4. [旧改善計画](binder-improvement-plan-20260915.md)と過去の計測記録。

旧文書の「小差分ごとの KPC」「3%以上の改善」「毎差分で全 conformance と全メモリ検証」を機械的に適用しない。全体時間の改善率に固定の最小値を設けない。実行には pstack-mode の `playbooks/perf-issue.md` を使い、本書の検証頻度と採用条件を優先する。

今回はローカル compiler の実装指示書である。PR 作成、push、merge、定期実行、Goal の作成は本書だけでは開始しない。UI の10並列実行・スクリーンショットは対象外とし、実入力による compiler 実行ログを保存する。計測は一人が直列で担当する。

## Program checklist

### Arm the program

- [ ] 実装開始を依頼されたら、本書と Binder の `AGENTS.md`、設計書、既存 artifact を読む。指示書の作成だけを依頼された段階では実装しない。
- [ ] `repo_root`、基準 SHA、候補 SHA または dirty 差分、対象カード、モデル、出力先を `run-plan.json` に記録する。
- [ ] 最初の基準を `80d8b41ccfb046a551b721cb04f909205d66513a` と照合する。現在の source が異なる場合は新しい基準を記録し、旧結果を現在の性能値として使わない。
- [ ] 既存のユーザー差分を記録する。対象外の文書・コードを戻さない。

### Spawn owners

担当モデルは次の割当を使う。モデル名は本セッションの利用可能モデルと `~/.codex/pstack-models.toml` を確認した。新しい実行環境では開始時に利用可能性を再確認する。

| 役割 | モデル | reasoning effort | 責任 |
| --- | --- | --- | --- |
| 進行管理・指示の分解 | `gpt-5.6-terra` | `medium` | 依存関係、source identity、検証の抜けを管理する |
| 通常実装 | `gpt-5.6-luna` | `high` | C01〜C09、C12のコード・境界テストを実装する |
| 計測器の整備 | `gpt-5.6-luna` | `high` | C00の runner、micro、比較器の正確性を担う |
| 計測実行・一次集計 | `gpt-5.6-luna` | `medium` | 固定 plan を直列実行し、raw と benchstat を保存する |
| 独立したコード・計測レビュー | `gpt-5.6-terra` | `high` | 作成者と別 agent で source、意味、測定範囲、採否を確認する |
| 宣言 query・Flow pair の実装 | `gpt-5.6-terra` | `high` | C10、C11の意味の共通化、Flow順序、alias を扱う |
| 所有権の再設計・難しい不一致の裁定 | `gpt-6-astra` | `low` | local/foreignの証明、Flow lifetime、レビュー対立を解決する |
| 最終累積候補のレビュー | `gpt-6-astra` | `low` | 全体結果と残る不確実性を見て最終判定する |

Luna の実装指定は設定の feature/refactoring と perf-issue、Astra の low は hardest tasks に合わせた。Terra/high は設計レビューと意味の複雑なカードに使う本指示書の指定である。グローバル設定ファイルは変更しない。

- [ ] 通常は実装1 agent と独立レビュー1 agent を使う。計測 agent は同じホストの build・test・profile が終了してから動かす。
- [ ] 同じ `binder.go` を複数 agent が同時編集しない。別ファイルの調査・レビューだけ並行できる。
- [ ] モデルが利用不可なら無断で低い構成へ置き換えない。利用可能な同等以上の構成を選び、理由と変更を記録する。
- [ ] Luna が同じ意味の問題で2回修正しても解決できない場合、または owner・保存先・訪問順を変更する必要が出た場合は Terra/high に引き継ぐ。
- [ ] Terra のレビューが割れる場合や、新しい cache / lifetime / syntax image が必要になる場合は Astra/low に設計判断を依頼する。測定のばらつきはモデルを増やして解決しない。

### PR mechanics

- [ ] 本書の C 番号を変更単位として使う。PR にする場合も一つの性能仮説を一つの差分にする。
- [ ] before は直前の採用済み累積 source、after はその source＋対象カードだけにする。棄却・保留候補の上に次を積まない。
- [ ] test-only の baseline adapter が必要なら、production の old/new と共通 micro 本体を区別して保存する。
- [ ] commit / push / merge は実装依頼時の権限範囲に従う。性能のために意味を変えたり、期待 baseline を承認したりしない。

### Verdict and merge

各最適化カードは、次の三条件を満たしたときだけ `ADOPT` とする。

- [ ] 対象の正確性試験と実入力の意味比較が成功している。
- [ ] 指定した micro の主指標で、再現する削減を確認している。
- [ ] Binder bench の必須入力すべてで明確な regression がなく、測定の不確実性が本書の範囲に収まる。

`HOLD` は判定材料不足、`REJECT` は意味回帰・性能回帰・再現しない利得、`ADOPT` は小差分としての採用を表す。artifact の鮮度を示す `current / stale / missing / unsupported` とは別の欄に記録する。

### Boot recipe

- [ ] Go module は repo root の `tsc/` であることを確認する。両版で同じ Go、build tag、compiler flags を使う。
- [ ] 通常GCの `BenchmarkBindInvestigation` を Binder 全体の比較に使う。実測区間には parse を含めない。
- [ ] 最初のC00で固定 manifest、baseline/after binary、runner、collector、host全体で共通の計測 lock を用意する。
- [ ] 計測中は他 agent の build、test、micro、Binder bench、KPC、Instruments を並走させない。別 run directory の lock だけで十分と考えない。

## 測定と採否の規則を固定する

### Micro と Binder で同じ比較単位を使う

- micro の before/after は同じ入力・同じ意味操作・同じ logical operation 数にする。関数シグネチャだけを揃えて実際の仕事が違う比較を作らない。
- micro の主指標はカード着手前に `ns/op` または `B/op`・`allocs/op` と指定する。結果を見て有利な指標へ切り替えない。
- read-only helper は実入力から抽出した node corpus または同じ構文の最小 fixture を使う。結果は checksum 等で消費し、DCE・定数畳み込みで測定対象が消えていないか確認する。
- 従来経路は before の production code、新経路は after の production code を呼ぶ。micro 内に有利な模倣実装を書かない。新規 API の baseline adapter は元の call sequence を再現するだけにする。
- mutable な Binder / Symbol / Flow を毎回同じ状態で呼ばない。distinct な未bind AST または新しい状態を batch 単位で準備する。準備・明示GC・解放を計測外に置き、実際の操作が必要とする allocation は計測内に残す。
- 1 op が corpus 一巡なら、その件数を記録する。1 helper call の ns/op と混同しない。状態をリセットするために対象の allocation を計測外へ移さない。
- `bindEach` / functions-first / kind 伝搬の micro は実際の bind 呼出し経路を含める。単独の KindAt や list 取得だけの短縮では合格にしない。

### 固定する初期プロトコル

| 項目 | 指示 |
| --- | --- |
| 通常の Binder 入力 | `checker.ts` と `dom.generated.d.ts`。各カードの追加入力も下表に従う |
| 通常GC | `GOGC=100`、`GOMEMLIMIT=off`、`BINDER_BENCH_GC_OFF=0` |
| 並列度 | `GOMAXPROCS=8` を初期値とし、両版で同じ値を固定 |
| Binder iteration | `-test.benchtime=10x`。1opはdistinct/unbound ASTを1回bindする |
| read-only micro | 初期値 `-test.benchtime=300ms`。結果取得前に所要時間を見て固定する |
| mutable micro | 初期値は10個の独立状態を使う `10x`。1opを固定した小さなcorpusにして時計の誤差を避ける |
| round | 8 round。各 round に `before_a / after_a / before_b / after_b` を1回ずつ |
| 順序 | 4通りのrotationを一巡し、後半4 roundは各順序を反転。順序を先にファイルへ保存 |
| process | sampleごと・対象benchmarkごとにfresh process。`-test.count=1` |
| 主比較 | `before_a` 対 `after_a` の8標本 |
| 確認比較 | `before_b` 対 `after_b` の8標本。主比較と合算しない |
| 自己比較 | 同じbinaryの a 対 b を両版で比較。通常run内の標本を使い、別KPCを要しない |

初期値を変える場合は性能結果を見る前に理由を記録する。同じrunに結果がよくなるまで標本を追加しない。固定runが不十分なら `HOLD` とし、原因と変更した計画を明記して新しいrunを一度行う。繰り返し調整を続けず、なお不明なら当該カードを保留する。

### Micro の削減を判定する

時間を主指標にした場合は、主比較と確認比較の両方で after の `ns/op` が低く、benchstat の `p < 0.05` を満たすことを要求する。最低改善率は設けない。入力別の結果を残し、最もよい subcase だけを選ばない。

allocation を主指標にした場合は、両比較で `B/op` または `allocs/op` の減少を確認する。値が変動する場合は benchstat の有意差も確認する。すべての標本で同じ値を取る決定的な差は、raw と操作数から削減を説明する。関連する時間や他のメモリ指標に明確な悪化があれば保留する。

各カードの「ns/op削減」は、主に再読・再分類を減らす候補に対する初期の主指標を示す。着手前にallocation削減の具体的な機構を特定した場合は、理由とともに主指標を `B/op` または `allocs/op` に固定し、上記のallocation gateを適用できる。時間が非有意だった後に指標を切り替えることは認めない。

純粋な operation counter、静的な命令数、At/KindAt の呼出回数だけでは、この micro gate を通さない。機構を確認する補助証拠として使う。micro で時間も allocation も削減を示せなければ、KPCだけで通常gateを置き換えず `HOLD` または `REJECT` とする。

### Binder の明確な regression を判定する

1. `ns/op`、`B/op`、`allocs/op` を必須入力ごとに benchstat で比較する。geomean だけで入力別の悪化を隠さない。
2. 主比較と確認比較の両方で同じ指標が有意に悪化したら `REJECT` とする。固定の「これ以下なら悪化を無視する」割合は設けない。
3. 片側だけ有意に悪化、符号の不一致、同じ方向のallocation増などがあれば `HOLD` とする。確認側の非有意を理由に、有意な悪化を打ち消さない。事前固定した新runで再確認し、再現したら棄却する。
4. 有意な悪化がなくても、比較区間が広く大きな回帰を含む場合は `HOLD` とする。下記の測定品質条件を確認する。
5. micro gateと正確性を満たし、Binder全入力で上記の懸念がなく測定品質条件を満たす場合、Binder時間の改善が非有意でも `ADOPT` としてよい。

測定品質の初期値は、通常GC wall の両版 A/A の95%区間が ±2% 内に収まり、候補対beforeの相対差の95%区間上端が +2% 以下であることとする。round対応の log ratio を保存済みcollectorと同じ方法で計算し、seed・実装・区間を保存する。主比較と確認比較を別々に確認する。

この2%はユーザーが指定した許容回帰率ではなく、本指示書が置く初期の検出精度条件である。有意な1%悪化を許可する条件でも、2%未満の回帰が存在しない証明でもない。変更の危険度に応じて事前に厳しくできる。精度不足のrunを通すために、結果を見て緩めない。新しい表現や寿命を追加する候補は、この通常gateだけで採用しない。

### KPC を使う場面を限定する

- 通常カードでは実行しない。未実行は `missing`、`not_required_for_card=true` と記録し、それだけで `HOLD` にしない。
- 原則は最終累積候補対元のBを1キャンペーン測る。すでに同じsource/条件の有効な結果があれば再利用する。
- 途中で使うのは、micro利得とBinder悪化の原因が通常の機械語確認で分からない場合、大きな呼出規約変更が重なった場合、またはユーザーが依頼した場合。
- KPCを実行するときは[専用手順](kpc-measurement-workflow.md)の実counter自己検証、固定round、寿命、イベント確認を省略しない。通常wallとKPC中のns/opを混ぜない。
- 最終KPCが使えなくても、主案の小さな変更は通常gateと累積検証で判断できる。理由を明記し、最終性能の命令数・cyclesは未確認と報告する。

## C00 計測の基準と小さな runner を用意する

**依存。** なし。担当は Luna/high、レビューは Terra/high。

- [ ] `tsc/internal/binder/bind_bench_test.go` の `BenchmarkBindInvestigation` と `bind_bench_lifetime_test.go` を再利用する。1回bind済みASTへの再呼出しを測らない。
- [ ] `tools/scripts/tsc/binder_semantic_audit.py` と固定fixtureを確認する。差分比較器の自己検証を行う。
- [ ] repo内に通常wallとmicroの小さなrunnerを置く。提案名は `tools/scripts/tsc/binder_call_bench.py`。これは現時点で未実装である。
- [ ] runnerにrun専用source/binary、共通ホストlock、固定順序、fresh process、raw保存、benchstat、区間算出、欠測検出を実装する。KPC自動化や汎用benchmark基盤へ広げない。
- [ ] 既存保存driverを移植元として確認し、埋め込まれた日付・path・6round・PMU実行をそのまま新runへ流用しない。
- [ ] baselineとcandidateを同じproduction sourceにしたA/Aでrunnerを検証する。fixture欠落、regex不一致、失敗exit、重複sample、binary差替えは明示失敗にする。
- [ ] microの新規関数名は `BenchmarkBinderCall*`、AST queryは `BenchmarkStoreQuery*` を基本にし、同じregexが両版に存在することを確認する。
- [ ] C00は計測基盤なので「microで改善」という最適化gateの対象外と記録する。runnerの正確性と同一仕事の比較可能性を完了条件にする。

## C01 bindEach に local list span を戻す

**依存。** C00。担当は Luna/high。

- [ ] `binder.go` の `bindEach` だけに既存 `TryBindListSpan` / `BindListSpanElem` を接続する。保存済み候補はP0aの6行overlayである。
- [ ] 主走査のlocal listとlocal要素の契約を確認する。foreign list ownerのrefを `b.store` に渡すfallbackを追加しない。
- [ ] micro `BenchmarkBinderCallBindEach` を作る。空、1要素、複数要素、実入力から採った長さ分布を含め、parse済み未bind ASTの実際のbindを測る。
- [ ] 主指標をns/opとし、主ケースを実入力由来の混合corpusにする。空listは回帰対照にする。
- [ ] span境界試験、訪問順、11入力意味監査と共通Binder gateを通す。

## C02 functions-first の2passで span を共有する

**依存。** C01採用時はそのhead。C01棄却時は理由を確認し、独立候補として再計画する。担当は Luna/high。

- [ ] `bindEachStatementFunctionsFirst` だけを変更する。関数先行、2pass、要素値の再読、EOFの位置を維持する。
- [ ] micro `BenchmarkBinderCallFunctionsFirst` に関数なし、関数のみ、混在、少数と長いlistを入れる。主ケースは実入力由来の混合corpusとする。
- [ ] partition配列やkind cacheは追加しない。ns/op削減、先行順とFlow結果、共通Binder gateを確認する。

## C03 現在 node の kind を再dispatchで共有する

**依存。** C00と直前採用head。担当は Luna/high、設計差分をTerra/highが先に読む。

- [ ] `bind` が持つkindを `bindChildren`、`bindContainer`、`bindEachChild`、既存generated walkerへ渡す。通常の `bind(NodeRef) bool` 入口は残す。
- [ ] `generate-go-ast.ts` と `bindwalk_generated.go` の接続を同時に更新する。kind専用nodeで不要な引数を増やさない。
- [ ] micro `BenchmarkBinderCallDispatch` でleaf中心、container中心、混合構文の実際のbind経路を測る。単独KindAtのベンチだけで済ませない。
- [ ] ns/op削減、通常ビルドのCALL・frame・spill・inline差、生成物一致、11入力意味監査、共通Binder gateを確認する。
- [ ] 全helperへのkind伝搬や新しいVisit型が必要になったら、このカードを拡張せず設計判断へ戻す。

## C04 Container 分類の入力を絞る

**依存。** C03が採用された場合はそのhead。採用されない場合は局所取得したkindから開始する。担当は Luna/high。

- [ ] C04aで `GetContainerFlags` に渡すHandleを既知kindのHandleOfで作る限定差分を評価する。
- [ ] C04aを基準にC04bとしてkind-onlyの共有ruleと必要時だけのparent/initializer probeを比較する。rule方式を無条件採用しない。
- [ ] micro `BenchmarkBinderCallContainerFlags` は単独classifierに加え、呼出側から既知kindを渡す経路を含める。kind-only、Block、method/accessor、initializer有無を用意する。
- [ ] public Handle入口とlocal入口の全結果を比較し、foreign parent/childも試験する。`checker/utilities.go` の呼出しを壊さない。
- [ ] C04a/C04bを個別にns/opと共通Binder gateで判定する。共有ruleの追加費が勝てなければC04aまでで終える。

## C05 識別子の予約語判定を先に行う

**依存。** C00と直前採用head。担当は Luna/high。

- [ ] `checkContextualIdentifier` のdiagnostics、Ambient、JSDoc gateを保ち、keyword判定をIdentifierName判定より前へ移す。
- [ ] micro `BenchmarkBinderCallContextualIdentifier` に通常identifier、keyword状の名前、property/import/export/JSX名を入れる。実入力の構成比を記録した混合corpusを主ケースにする。
- [ ] 通常identifierの単独勝利だけで採用しない。追加の文字列判定が発生するproperty名群も報告する。
- [ ] escape、strict、await/yield、既存診断の抑制と診断spanを比較する。追加Binder入力にTSXと `Herebyfile.mjs` を入れる。

## C06 IdentifierName を Store＋NodeRef の共通queryにする

**依存。** C05の採否が確定したhead。担当は Luna/high、owner境界をTerra/highがレビューする。

- [ ] `ast.IsIdentifierName(Handle)` を `Store.IsIdentifierName(NodeRef)` の共通実装へ接続する。node自身のkindを要求しない。
- [ ] parent/選択childがexternalの場合はprivateなowner-aware処理へ進む。公開関数へ戻る循環fallbackを作らない。
- [ ] micro `BenchmarkStoreQueryIdentifierName` にlocal親、external親、external子、ref数値が同じ別Store、名前スロットの全分類を入れる。
- [ ] 主ケースはlocalの実入力由来corpusとし、foreignケースは正確性と費用の対照にする。全親kindの結果一致と共通Binder gateを確認する。
- [ ] C05で消えたquery回数を再び削減利益へ数えない。前後はC05採否後の同じ基準を使う。

## C07 modifier query を ListRef 起点にする

**依存。** C00と直前採用head。担当は Luna/high。

- [ ] `Store.ModifierFlagsOf(ListRef)` を唯一の集計実装にする。Handle入口と取得済み `ParameterAccessor.Modifiers` のcallerを接続する。
- [ ] owner/headerを一度解決し、local要素のkindを読む。foreign listとforeign要素も同じModifierToFlag規則で処理する。
- [ ] micro `BenchmarkStoreQueryModifierFlags` に空、decoratorのみ、1修飾子、複数修飾子、foreign列・要素を入れる。listをすでに持つcallerとHandle callerの両方を測る。
- [ ] ns/op、allocation不増、modifier真理値、decoratorの訪問、共通Binder gateを確認する。新しい永続cache・flags列は追加しない。

## C08 constructor だけで parameter-property の修飾子を調べる

**依存。** C07採用headを優先する。C07保留でも既存queryを使った独立差分として実施できる。担当は Luna/high。

- [ ] `bindParameter` でconstructor親を先に判定する。parameter Symbolとclass property Symbolの宣言順を保つ。
- [ ] micro `BenchmarkBinderCallParameterProperty` に通常関数、method、constructor、readonly/accessibility/override、分割代入を含める。
- [ ] 主ケースを実入力由来のparameter混合corpusにする。constructorだけのmicroにも悪化がないか確認する。
- [ ] 真理値・Symbol・診断順、ns/op削減、共通Binder gateを確認する。

## C09 container Symbol を分岐内で一回取得する

**依存。** C00と直前採用head。担当は Luna/high。

- [ ] `declareSymbolAndAddToSymbolTable` の二重取得がある分岐だけを変更する。GetMembers/GetExportsの遅延作成と宣言順を保つ。
- [ ] micro `BenchmarkBinderCallContainerSymbol` で該当enum/type/object/interfaceの宣言操作を測る。毎回freshなtable状態を使い、重複宣言の別経路を測らない。
- [ ] 最小の複合呼出しでns/op削減が示せなければ、取得回数だけを根拠に採らない。
- [ ] `declareSymbolEx/GetSymbolTable` が支配し `declareModuleMember` が見える場合に限り、export-heavy unique declarationを追加の最小microに選ぶ。symbol syntheticは実入力のボトルネック証明にしない。
- [ ] 共通Binder gateと宣言結果・重複診断を確認する。

## C10 宣言の root・所属 query を共有する

**依存。** 先行カードの採否確定後。担当は Terra/high、独立レビューも別のTerra/high。

- [ ] `bindVariableDeclarationOrBindingElement` のrequire、block/catch、parameter所属で共有できる読取を確認する。AST側の既存述語と意味を二重実装しない。
- [ ] micro `BenchmarkBinderCallDeclarationContext` に直接宣言、入れ子BindingElement、catch、parameter、require分割代入、var/let/constを入れる。
- [ ] 診断を先に出す順序、Symbol flags/excludes、root探索を保持し、ns/opと共通Binder gateを確認する。追加Binder入力にJS/JSDocを入れる。
- [ ] 共有規則を定義できず特例が増える場合は保留する。新しい祖先stackやcacheを足して解決しない。

## C11 条件式の両側接続を一つの API にする

**依存。** 先行カードの採否確定後。担当は Terra/high、最初の形と最終差分をAstra/lowがレビューする。

- [ ] `createFlowCondition` の5組のtrue/false呼出しを `addConditionAntecedents` へ移す。式を一度分類し、trueのcreate→add、falseのcreate→addを維持する。
- [ ] AST mutationや再帰bindが分類と接続の間にないことを、calleeまで追って確認する。Flow flagsのsnapshotを上書きしない。
- [ ] micro `BenchmarkBinderCallConditionPair` をbeforeでは現行2回のcreate/add、afterでは新APIに接続する。二つのFlowを先に作るadapterは不正とする。
- [ ] missing、literal、non-narrowing、narrowing、optional/nullish、論理代入、同一targetを用意する。主ケースは実入力の条件式corpusとする。
- [ ] freshなFlow状態でns/opとallocationを測る。micro中にFlow arenaを際限なく成長させない。Flow生成は測定内に残す。
- [ ] Flow ID、Referenced/Shared、FlowList順、antecedentとtargetのalias、診断と意味digestを確認する。追加Binder入力に `controlFlowOptionalChain.ts` とJSを入れる。
- [ ] 通常のgateが明確ならKPCを挟まず進める。Flow表現の変更はこのカードに含めない。

## C12 Flow に保存する Handle を直接受け取る

**依存。** C11採用headを優先する。単独で実施する場合はその基準を明記する。担当は Luna/high、Terra/highが全callerをレビューする。

- [ ] `newFlowNodeEx` の引数を保存対象のHandleに変更し、Known/Refの兄弟APIを増やさない。全assignment/call/start等のcallerを適応する。
- [ ] micro `BenchmarkBinderCallFlowNode` にHandle既取得、ref＋kind既取得、refのみのcallerを含める。未知kindのAtを測定外へ移さない。
- [ ] 実入力に対応する混合callerを主ケースにする。FlowのNode/Data、owner保持、ns/op削減、共通Binder gateを確認する。
- [ ] 恒常的なFlowNodeRef化やCheckerのowner伝搬は別設計とする。

## 累積候補を検証する

### 中間checkpoint

- [ ] C01〜C04の採否確定時に、最初のB対累積headで通常Binderの2入力を再比較する。個別利得の単純加算をしない。
- [ ] C05〜C10の採否確定時に、保存済み9入力manifestで累積Binder benchを行う。これはKPC実行の自動トリガーにしない。
- [ ] 共通queryの意味を変更したカードではfocused conformanceを実行する。小さなspan/local変数のカードごとに全suiteを繰り返さない。

### 最終checkpoint

- [ ] 元のBと最終採用headを固定し、通常Binderの9入力で同じ回帰判定を行う。
- [ ] parse+bindと代表projectの実compiler CLIを両版で実行する。入力・tsconfig・診断・exit code・出力を保存し、関連する全体費用の悪化がないか確認する。
- [ ] 全conformanceを固定commitで比較する。対象0件、未終了、pass→fail、出力内容変更、skip変化を区別する。snapshot取得コマンドのexit 0を全テスト成功と読み替えない。
- [ ] allocation増、新しい保持状態、逃避、Flow/arena/寿命の変更がある場合はretained live/scanとGCを評価する。主案で保持表現が不変なら、その静的確認とB/op・allocs/opを記録し、各小差分でGC probeを必須にしない。
- [ ] 原則一度の累積KPCを実施する。利用不可なら `missing` と理由を残す。通常gateが成立した小変更をKPC欠測だけで棄却しない。
- [ ] Astra/lowの最終レビューを受け、採用済みカード、棄却・保留カード、未確認効果を分けて報告する。

## Close the program

- [ ] 全カードにbefore/after identity、担当モデル、検証artifact、`ADOPT / HOLD / REJECT / NOT_STARTED` を記録する。
- [ ] 採用した全最適化についてmicroの削減とBinder回帰確認を示す。未測定カードを完了扱いにしない。
- [ ] 実装・unit・実入力・perfの証拠を対応付ける。新しい版の検証に古いSHAの結果を付けない。
- [ ] 最終報告で選択repo、候補clone、artifact set/status、ns/op、B/op、allocs/op、hot path、allocation driver、診断、次の行動を示す。
- [ ] 依頼範囲の成果物を引き渡す。保留カードは理由と再開条件を残し、性能改善を約束しない。

## Appendix A. Prototype evidence と開始時の根拠

本書作成では新しいprototypeを実装していない。実ソース、既存の設計比較、benchmarkの寿命処理、生成元、監査ツールを照合した。候補の効果を未測定のまま確定せず、C00と各カードで検証する。

- 選択repoは `/Volumes/SanDisk1TB/worktree/binder-rewrite`、作成時HEADは `80d8b41ccfb046a551b721cb04f909205d66513a`。
- 候補checkoutはcursor-ast-store-tests、pointer main、flownode、lock系、store-pr系。全一覧は下記artifact setの `worktrees.txt`。今回別cloneのsourceは使っていない。
- artifact setは `/Volumes/SanDisk1TB/Library/Caches/binder-85506e8-80d8b41-20260915-run1`。identityのrepoとtypescript-go revisionが一致し、tsgolint revisionはnull。主A/Bは現選択checkoutに対して `current` であり、新カードの効果の証明ではない。
- 新カードのmicro・Binder結果は `missing`。別revisionの過去artifactは `stale`。stageはあるが要求regexのBenchmark行がなければ `unsupported`。KPCを実行しなかったことや権限がないことを、Benchmark行不在の `unsupported` と混同しない。
- 広いhot pathはbind/walk/helper全体。listは狭く取り組みやすい部分である。allocation driverはFlow、列、Symbol、Localsが候補で、現在の回帰を全allocation stackへ帰属したわけではない。

基準の保存済み測定値は次のとおり。Aは85506e8、Bは80d8b41。通常wallはA/A精度未達である。

| 入力 | ns/op A → B | B/op A → B | allocs/op A → B |
| --- | ---: | ---: | ---: |
| checker.ts | 16,088,731.0 → 18,266,012.5 | 12,798,224.0 → 12,777,532.5 | 14,163.0 → 14,165.0 |
| dom.generated.d.ts | 6,059,273.0 → 6,880,706.0 | 7,869,560.5 → 7,867,973.5 | 16,682.5 → 16,683.0 |

診断は、同じnode訪問数に対して再解決が増えたこと。次の行動はC00で通常のmicroとBinder比較を再現可能にし、C01を独立に評価すること。過去のKPC命令増を新しいmicro削減として使わない。

## Appendix B. 実行コマンドを組み立てる

以下はC00と各カードの実装後に実行する手順。新しいmicro名は本書の仕様であり、現時点のrepoには存在しない。C00の提案runnerについても、実装前に架空のsubcommandを実行しない。

作業用変数は、そのrunで固定した絶対pathを設定する。

- `BINDER_BEFORE` と `BINDER_AFTER` は両版のsource snapshotのrepo root。
- `BINDER_BEFORE_SHA` と `BINDER_AFTER_SHA` はconformance対象sourceに一致する完全なcommit SHA。dirty差分を含むときは、その差分を固定するまでHEADを代入しない。
- `BINDER_RUN` は新規run directory。`bin/`、`fixtures/`、`raw/`、`analysis/` を作る。
- `BINDER_MANIFEST` はそのrunにコピーしてhash検証したfixture manifest。
- `BINDER_BENCHSTAT` はversionとhashを記録したbenchstat実行ファイル。
- `BINDER_PROJECT_CONFIG` はC00で選ぶ実在tsconfigの絶対path。project source・依存関係・設定のhashを `run-plan.json` に保存し、before/afterで同一入力を使う。未設定ならCLI検証を開始しない。

### 単体試験と寿命検証

各snapshotのrepo rootから実行する。追加したfocused testのregexと対象件数も保存する。

```sh
go -C tsc test ./internal/ast ./internal/binder ./internal/checker ./internal/compiler
BINDER_INVESTIGATION_MANIFEST="$BINDER_MANIFEST" go -C tsc test -tags=binderinvestigation ./internal/binder -run '^TestInvestigationBatchLifetime$' -count=1
python3 tools/scripts/tsc/binder_semantic_audit.py selftest
```

package testは変更に応じて選び、同じsourceで成功済みの重い試験を理由なく繰り返さない。新しい差分や失敗がある場合は関連分を再実行する。生成物を変更した場合はrepo指定Node環境で次を実行し、再生成による未反映差がないことを確認する。

```sh
node --experimental-strip-types --no-warnings ./tools/scripts/tsc/generate.ts
```

### 実入力の意味を比較する

repo rootの既存スクリプトを使う。fixture選択とハーネスを同じにする。

```sh
python3 tools/scripts/tsc/binder_semantic_audit.py run --repo "$BINDER_BEFORE" --representation store --out "$BINDER_RUN/semantic-before"
python3 tools/scripts/tsc/binder_semantic_audit.py run --repo "$BINDER_AFTER" --representation store --out "$BINDER_RUN/semantic-after"
python3 tools/scripts/tsc/binder_semantic_audit.py compare --old "$BINDER_RUN/semantic-before" --new "$BINDER_RUN/semantic-after" --out "$BINDER_RUN/semantic-compare"
```

監査scriptがsource snapshotのAPIへ適合しない場合は、adapterとその理由を保存する。比較を弱める正規化を追加しない。対象fixtureが0件なら失敗とする。

### Binder bench のbinaryを先にビルドする

```sh
go -C "$BINDER_BEFORE/tsc" test -c -tags=binderinvestigation -o "$BINDER_RUN/bin/before-binder.test" ./internal/binder
go -C "$BINDER_AFTER/tsc" test -c -tags=binderinvestigation -o "$BINDER_RUN/bin/after-binder.test" ./internal/binder
```

次はbeforeの1 sample。C00のrunnerは各版の `tsc/` を作業ディレクトリにし、同じ引数を固定順序で実行する。fixture名の `/` 区切りを含むGo benchmark regexに注意する。

```sh
env GOMAXPROCS=8 GOGC=100 GOMEMLIMIT=off \
  BINDER_INVESTIGATION_MANIFEST="$BINDER_MANIFEST" BINDER_BENCH_GC_OFF=0 \
  "$BINDER_RUN/bin/before-binder.test" \
  -test.run='^$' \
  -test.bench='^BenchmarkBindInvestigation$/^checker\.ts$' \
  -test.benchtime=10x -test.count=1 -test.benchmem
```

DOMなども1入力ずつfresh processで測る。test fixtureのrepo-root探索やoverlayが必要なら両版同じ方法で設定し、そのhashを記録する。対象がskipした出力を成功と扱わない。

AST queryのmicroは `./internal/ast`、Binderのmicroは `./internal/binder` のtest binaryを作る。read-onlyは固定した時間、mutableは固定したbatch反復数を指定する。`-test.bench='.'` で無関係な旧benchmarkをまとめて実行しない。

### raw から比較する

stageごとにsampleログを保存し、順序情報から次の4ファイルを生成する。

```text
raw/<stage>/before_a/bench.txt
raw/<stage>/after_a/bench.txt
raw/<stage>/before_b/bench.txt
raw/<stage>/after_b/bench.txt
```

```sh
"$BINDER_BENCHSTAT" "$BINDER_RUN/raw/binder/before_a/bench.txt" "$BINDER_RUN/raw/binder/after_a/bench.txt" > "$BINDER_RUN/analysis/binder-primary.txt"
"$BINDER_BENCHSTAT" "$BINDER_RUN/raw/binder/before_b/bench.txt" "$BINDER_RUN/raw/binder/after_b/bench.txt" > "$BINDER_RUN/analysis/binder-confirmation.txt"
```

microと両版A/Aも同じ形式で保存する。benchstatのp値はpaired testと呼ばない。round対応の区間計算は別の結果として保存し、両方を判定に使う。

### conformance と実compilerを確認する

実行前に `.codex/skills/verify-conformance/SKILL.md` を読み、prepare / snapshot / compareを使う。このhelperはcommit済みsourceだけを検証する。dirty変更をHEADの結果で検証済みにしない。候補を隔離したローカルcommitへ固定するか、正確なsnapshotに対応する検証手順を用意する。

```sh
python3 .codex/skills/verify-conformance/scripts/conformance.py prepare --repo "$BINDER_AFTER" --revision "$BINDER_AFTER_SHA" --out "$BINDER_RUN/conformance-after-build"
python3 .codex/skills/verify-conformance/scripts/conformance.py snapshot --prepared "$BINDER_RUN/conformance-after-build" --out "$BINDER_RUN/conformance-after"
```

beforeも同様に固定し、compareで内容まで確認する。初回のA/Aで非決定性を確認し、既知失敗と新規回帰を区別する。prepareが同じならfocusedとfullで同じbinaryを使えるが、対象範囲を取り違えない。

CLIは `tsc/cmd/tsc` をビルドする。代表projectとtsconfigはC00で実在pathを選んで固定する。以下の `BINDER_PROJECT_CONFIG` はその実在tsconfigを指す。

```sh
go -C "$BINDER_AFTER/tsc" build -o "$BINDER_RUN/bin/after-tsc" ./cmd/tsc
"$BINDER_RUN/bin/after-tsc" --project "$BINDER_PROJECT_CONFIG" --noEmit --extendedDiagnostics
```

同じprojectをbeforeでも実行する。起動引数、依存関係、source、exit code、診断を保存する。parse+bindの比較も別stageに保存し、bind-onlyの結果と混ぜない。現行 `BenchmarkParseBindColumnPresize` を使う場合も、固定入力とStore解放・メモリ寿命を監査してから採否用に使う。

## Appendix C. 成果物と依頼文を残す

各カードの結果は次の構成で保存する。baselineの結果を書き換えない。

```text
<run>/
  run-plan.json
  identity.json
  source-diff.patch
  input-manifest.json
  order.jsonl
  models.json
  raw/
  analysis/
  correctness/
  codegen/
  report.md
```

`report.md`には問題、証拠、再現、仮説、実装内容、採否条件、結果、未確認点、次の行動を記載する。実装したAPI数や削除行数を性能結果の代用にしない。KPCは実行有無と必要性を別々に書く。

実装agentには次を渡す。C番号、before、出力先だけを具体値へ置き換える。

> `binder-call-implementation-instructions-20260915.md` の指定カードを実装してください。beforeからそのカードだけを変更し、境界テストとproduction経路を呼ぶmicroを追加してください。意味・所有権・更新順を保ち、指定モデルを使ってください。独立レビュー後にmicroと通常GCのBinder benchを固定planで実行してください。microの削減とBinderの回帰確認を採用条件とし、KPCはカードごとには実行しないでください。raw、benchstat、区間、source identity、モデル、未確認点を保存してください。性能gateが成立しなければ保留か棄却とし、別の未評価変更を積み上げないでください。

独立reviewerには次を渡す。

> 指定カードのdiffと検証成果物を読み、productionとmicroの仕事が一致するか、DCE・bound AST再利用・寿命・foreign owner・Flow順序の問題がないか確認してください。microの削減、Binder全入力の結果、A/Aと区間、失敗・欠測を確認し、`ADOPT / HOLD / REJECT` の勧告と根拠を返してください。実装者の要約だけで判定せず、rawと該当sourceを確認してください。

## Appendix D. 参照と手順の適用範囲

- [設計書](binder-call-architecture-20260915.md)
- [元の改善計画](binder-improvement-plan-20260915.md)
- [調査の記録と順序](binder-investigation-plan-20260910.md)
- [保存済みのA/B結果](commit-performance-results-20260915.md)
- [KPCを実行する場合の手順](kpc-measurement-workflow.md)
- [Binder wall benchmark](../bind_bench_test.go)
- [batch寿命試験](../bind_bench_lifetime_test.go)

pstack-modeのmulti-phase-planから、変更単位・担当者・観測結果・unit/実入力/perf・証拠保存の構造を適用した。今回は実装前prototype、cloud UI lane、PR lifecycle、定期tickを開始しない。ユーザーの文書作成という範囲と、通常benchを直列実行する条件を優先する。モデルは現在のローカル設定を基に指定し、使えないSparkを手順へ記載しない。

付属の `check-plan.mjs` は実行したが、英語の固定見出し・PR lifecycle・Goal・定期tickを要求するため、本書の形式検査は通過していない。これらは今回の依頼範囲に合わせて適用しない。コマンドの実在性、変更カード、採否条件、参照先は別途確認する。

本書の作成時点では全カード `NOT_STARTED`、新規性能artifactは `missing`。次の実行依頼でC00から開始する。
