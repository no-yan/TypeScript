# Luna向け：BinderのAST再解決削減・実装指示書

この文書は実装計画。P0/P1は旧arity APIで検証済み。今回、名前付きの生成アクセサへ方針を改訂した。
完了済み作業とartifactは保持し、新規G0〜G2でAPIを移行する。最新状態は作業記録を参照。
対象一覧は[残存再解決15分類](repeated-access-inventory-20260910.md)。まずこの指示書の指定カードだけを実行する。

## 目的と作業方法

Storeのindexから同じheader、childStart、list owner等を繰り返し解決する費用を除く。
callerが持つ構文参照・kindを必要なworkerへ渡し、意味処理・診断・訪問順は保つ。
小変更ごとに全体wallの有意差を求めない。局所は正しさと機械語、数カードがまとまった時点で実Binder benchmarkを行う。

Lunaへの依頼は原則1カードずつ。同じBinderを複数agentで同時編集しない。
カード完了後、変更・検証・残る重複を報告する。次カードが依頼に含まれていれば続行する。
不明なschemaや予期しない差分があれば、そのカードの不確実な変更を広げず、独立部分を完成させて具体的な相違を報告する。
仕様の変更が必要な場合を除き、通常のファイル編集・テストに追加承認を求めない。

## 初期状態

- 作業repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`
- Go module: 上記repoの`tsc`。Go testはこのディレクトリで実行する。
- 現在のHEAD: `32598cba146fa4dd7b6162b838630c90d865ab28`。dirty変更も実装の一部。
- IfStatementRefs、SyntaxChildren1〜6、生成walker、PropertyAccessExpressionRefs、P1の一括取得は実装済み。既存成果を保持し、G1/G2で名前付きAPIへ順次移行する。
- P0の`tools/scripts/tsc/luna_accessor_audit.py`は作成済み。G0で新生成ファイルのsnapshot対象を追加し、現在版で検証する。P0を作り直さない。
- `git checkout --`、`git reset --hard`、既存dirty変更の全置換は禁止。各カードは現在のファイルへの限定patchにする。
- ソース位置は行番号ではなく関数名を検索して特定する。
- 既存の`.cursor/.../artifacts`は追記専用。古いrawを上書きしない。

## 共通の実装規則

1. NodeRefは整数の子参照、bindNodeはrefとkind。`b.bindN`の引数は`(bindNode, parentKind)`であり、`(NodeRef, parentRef)`ではない。
2. 子のkindを既に持つなら`bindNode{ref: r, kind: k}`を渡す。持たなければ必要な時点で`b.at(r)`を一度呼ぶ。
3. 同じ親の複数child取得は生成`Access<Node>(ref)`を使い、`<Node>Accessor`の名前付きフィールドを読む。新しいcallerで`SyntaxChildrenN`や位置依存tupleを使わない。
4. node種別を生成メソッド名に含め、内部で実際のkindとschema形状を検査する。同じchild数の別kindを通さない。slot番号の知識は生成元へ閉じ込める。
5. 1つのchildしか使わない経路で他の全childまで無条件取得する改修はしない。複数readのある経路に取得を置く。
6. childの取得順とbind順は別。参照は先取りできても、bind/Flow/診断の呼出し順は変えない。
7. Flags・Symbol・Flow・Localsの値を再帰前にcacheしない。構文snapshotにこれらを入れない。
8. Handle.childAtはforeign childを解決できるが、ChildRefはforeign/missingを0で表す。Handle経路を機械的にNodeRef経路へ変換しない。
9. node-wide cache、大きなBindContext、全祖先stack、unsafe、-B、noinline指定、2pass→1pass、診断やnarrowingの結果memoを追加しない。
10. コード生成物を変える場合は`tools/scripts/tsc/generate-go-ast.ts`も変更し、再生成結果の一致を確認する。既存の名前付きアクセサで足りるなら生成元変更は不要。
11. 同じ値が0でも「解決済みで欠損」と「未解決」は別。遅延解決に0だけをsentinelとして使わない。
12. 未使用値の取得を大量に残さない。使うフィールドを明示し、struct返却で不要なfieldのload・list解決・spillが増えていないか確認する。

構文snapshotの前提はBind中に対象child/parent/kindが変わらないこと。既存20入力のwriter監査は動的根拠であり、全入力の保証ではない。
新しい経路で構文writerを呼ぶ場合は先取りしない。整数参照はStore配列の再配置後も有効だが、後から構文を書き換えた値は反映しない。
配列pointerの借用は後述の別カード。通常カードでは整数を保持する。

## schemaとbind順：生成実装の確認表

この表のslot/Nは生成側の確認用であり、callerのAPI指定ではない。実装前に`store_schema_generated.go`の対応constと現行関数を照合する。相違があればコードを優先し、表を更新してから進む。

| node | child slot 0から順 | N | 保つべき順序・注意 |
|---|---|---:|---|
| Parameter | rest, name, question, type, initializer | 5 | modifiers list → rest → question → type → bindInitializer(initializer) → name |
| BindingElement | rest, propertyName, name, initializer | 4 | rest → propertyName → bindInitializer(initializer) → name |
| BinaryExpression | left, type, operator, right | 4 | 通常left→type→operator→right。logical/destructuringは元の分岐順。modifiers list 0があるが専用Flow処理に新たにbindを追加しない |
| CallExpression | expression, questionDot | 2 | list 0=typeArguments、1=arguments。通常expression→questionDot→typeArguments→arguments、IIFEは元のarguments先行順 |
| VariableDeclaration | name, exclamation, type, initializer | 4 | この順にbind後、元の初期化Flow処理 |
| ConditionalExpression | condition, question, whenTrue, colon, whenFalse | 5 | 条件bind→true flowでquestion/whenTrue→false flowでcolon/whenFalse |
| Prefix/Postfix/Delete | operand/expression | 1 | operandを一度bind。演算子は別のscalar値。後処理のみ参照を再利用 |
| While | condition, body | 2 | 元のlabel・condition・bodyの順 |
| Do | body, condition | 2 | Whileと逆。元の順序を保持 |
| For | initializer, condition, incrementor, body | 4 | initializer→condition→body→incrementor。unreachable分岐も元どおり |
| ForIn/ForOf | await, initializer, expression, body | 4 | expression先行。awaitは元どおりForOfの対象分岐だけbind |
| Try | tryBlock, catchClause, finallyBlock | 3 | finallyのFlow分岐を変更しない |
| Switch | expression, caseBlock | 2 | caseBlockをbind後、default検出に同じcaseBlockを使用 |

## カード一覧・依存関係

| ID | 作業 | 一覧の対象 | 依存 |
|---|---|---|---|
| P0 | 現行snapshotを使う検証基盤 | 全件 | なし |
| G0 | 名前付きAccessorの生成仕様・生成器 | 全件 | P0（完了済み） |
| G1 | P1の2関数を名前付きAPIへ移行 | 4 | G0、P1（旧APIで完了済み） |
| G2 | 既存walker/If/PropertyAccessの移行 | 対応済み範囲 | G1 |
| P1 | Parameter/BindingElementの一括取得（履歴） | 4 | P0、名前付き移行はG1 |
| P2 | skipParenthesesとCallの参照受渡し | 2,3 | G2 |
| P3 | BinaryのFlow経路をまとめる | 1 | G2 |
| P4 | Variable/単項、Conditional/loop/try | 6,7 | G2 |
| P5 | Switch/Caseの再取得除去 | 8 | G2 |
| V1 | G0〜G2＋P1〜P5のまとめ検証 | — | G2、P1〜P5 |
| P6a/b/c | 宣言名の共通coreとworker受渡し | 5 | V1 |
| P7a/b | list descriptorとconsumer | 9,10 | V1 |
| P8 | Handle narrowing helperの局所共有 | 11 | V1 |
| V2 | 共通helper改修のまとめ検証 | — | P6〜P8 |
| P9 | modifier/root/Optionalの構文情報 | 13,15の構文 | V2 |
| D1 | 親情報の受渡し範囲を確定 | 12 | V2、設計確認 |
| D2 | fresh read用header位置の契約 | 14,15の意味情報 | V2、設計確認 |
| V3 | 最終統合・残存表 | 全件 | 実装対象カード完了 |

D1/D2は調査成果物を作るカードであり、Lunaが独断でBinder全体の引数やStore lifetimeを変更するカードではない。
一覧の全項目を追跡するが、設計が未確定な箇所を「実装済み」にしない。

## P0：現在のコードを検証できる状態にする（完了済みの仕様）

実装済みdriver: `tools/scripts/tsc/luna_accessor_audit.py`。以下はP0の仕様記録。完了済みdriverを再利用し、必要な対象ファイル追加だけを行う。

手順:

1. `git status`、HEAD、全dirty patch、Go sourceのSHA256、生成元SHA256を保存。現在版を累積比較のbaselineとして凍結する。
2. 新しいdriverはbaseline snapshotとcandidate snapshotの入力を明示的に受ける。sourceを旧baselineへ自動で戻さない。
3. `binder_syntax_scalars_experiment.py`の`before`生成、関数2つだけの差替え、固定CELLSは今回の連続改修に不適合。そのprepareをそのまま呼ばない。
4. 性能buildは各snapshotの全変更ファイルを反映する。監査buildも同じsnapshotに計数hookだけを注入する。
5. 監査hookは最新audit sourceと非監査sourceの差分を参考に、現在の関数へ適用。旧binder.go全体をコピーしない。anchorが一致しなければfailする。
6. 訪問traceはbind入口でnodeごとに一度。newFlowNodeのFlow生成順、symbol生成数、condition回数を記録するhookを維持する。
7. 既存8+9+3入力のmanifestを使い、まず変更前snapshotの20入力を実行。保存syntax-scalars監査の意味digestと一致することを確認する。
8. candidate source→監査sourceの差分がinstrumentationだけであることを保存。buildしたbinary、overlayの参照先、source SHAを照合する。

旧artifact root:
`.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/`

参照するもの:
- `syntax-scalars-experiment/{wall,kpc,audit}-all_batch/identity.json`、overlay、raw
- `syntax-scalars-experiment/audit-all_batch-results`、edge/extraの同名結果
- `manifest.json`、`name-experiment/edge-manifest.json`、`ancestor-experiment/extra-manifest.json`
- `binder_bounds_experiment.py`のaudit比較項目と`binder_setter_experiment.py`の固定順runner
- `binder_investigation.py`のhook生成は参考。ただし旧baseline読込と既存関数本体置換をそのまま流用しない

完了条件: baseline単独20入力一致、候補snapshotがbuildへ確実に入ること、空のBenchmark出力を成功扱いしないこと。
この基盤は以後全カードで共用する。個別カードごとに実験driverを増殖させない。

## G0：名前付きの生成アクセサを追加する

対象: `tools/scripts/tsc/generate-go-ast.ts`、新生成物`tsc/internal/ast/store_accessors_generated.go`、AST境界テスト、P0 driverのsnapshot対象。
このカードではBinder callerを変更しない。schemaから生成する仕組みを作り、まずParameterとBindingElementで検証する。

命名を固定する:

| メソッド | 返却型 | フィールド例 |
|---|---|---|
| `AccessParameter(ref)` | `ParameterAccessor` | Rest, Name, Question, Type, Initializer, Modifiers |
| `AccessBindingElement(ref)` | `BindingElementAccessor` | Rest, PropertyName, Name, Initializer |
| `AccessBinaryExpression(ref)` | `BinaryExpressionAccessor` | Left, Type, Operator, Right, Modifiers |
| `AccessCallExpression(ref)` | `CallExpressionAccessor` | Expression, QuestionDot, TypeArguments, Arguments |
| `AccessVariableDeclaration(ref)` | `VariableDeclarationAccessor` | Name, Exclamation, Type, Initializer |
| `AccessIfStatement(ref)` | `IfStatementAccessor` | Condition, ThenStatement, ElseStatement |
| `AccessPropertyAccessExpression(ref)` | `PropertyAccessExpressionAccessor` | Expression, QuestionDot, Name |

`Info`、`ChildrenOf...`、`Walk...`はこのAPIの名前に使わない。Walkは訪問を実行する関数に予約する。
Goでは別packageから読むのでフィールドはexportする。返却型は値structで、pointer/slice/Store/Flags/Symbol/Flowを含めない。
NodeRef/ListRefのsnapshotであり、フィールド参照時にStoreへアクセスしない。取得後の構文編集は反映しないとdoc commentへ明記する。

```go
type ParameterAccessor struct {
    Rest        NodeRef
    Name        NodeRef
    Question    NodeRef
    Type        NodeRef
    Initializer NodeRef
    Modifiers   ListRef
}
// func (s *Store) AccessParameter(ref NodeRef) ParameterAccessor
```

生成ルール:

1. 構造情報は既存schemaとstoreLayoutから取得する。slot数字を別の手書き表へ複製しない。
2. 名前だけのoverrideを生成元の一箇所に置く。ParameterDeclaration→Parameter、DotDotDotToken→Restなど、上の表へ一致させる。
3. それ以外のnodeはschemaのnode名＋Accessor、フィールドはschema member名を基本にする。alias kindを共有するnodeは許可kind集合をschemaから生成する。名前衝突は生成時にerror。
4. nil Store/ref=0ならzero struct。非zeroではnodes[ref]を一度解決し、実際のkindを検査する。
5. ParameterにはKindParameterだけを許す。同じ5-childの別kindはpanic。childLen/listLenも期待schemaと一致を検査する。kind aliasはschemaにあるものだけ許可する。
6. 同じheaderからchildStartを一度読み、名前付きfieldへ直接詰める。内部からChildRefやSyntaxChildrenNを呼んで再解決しない。
7. listも同じheaderのchildStart+childLenからslotを特定する。localは既存listRefのqualified identity、0 slotは既存foreignLists fallbackを使う。既存list helperを使う場合も親headerを再取得しない。
8. list要素は展開しない。Modifiers/Arguments等はListRefのみ返す。owner/startのloop外解決は引き続きP7。
9. 全フィールドを返すため、未使用listでもID atomic等が走る可能性がある。G2の機械語で必ず費用を確認する。inlineされたから消えたと推測しない。
10. 単発read callerを無理に全部Accessへ変えない。新しいpartial/flags指定APIを独断で追加しない。

試験: 正常な全field、missing child、nil/ref=0、同じchild数の別kind、alias、foreign childの0表現、foreign list identity、取得後Store growth、取得済み値と後の構文変更の独立性。
P0 driverの固定TARGET_GOに新生成物と新規テスト/関連sourceを含める。baselineに新ファイルがない状態もsnapshot/overlayで明示的に扱い、現在checkoutから漏れて混ざらないようにする。
完了条件: 再生成一致、ASTテスト、誤kind検出。公開APIは名前付きstructであり、slot番号やtuple順序をcallerに要求しない。

## G1：実装済みP1を名前付きAPIへ移行する

対象: `bindParameterFlowRef`、`bindBindingElementFlowRef`のみ。旧P1の実装・監査結果は履歴として保存する。

Parameter:

```go
a := s.AccessParameter(ref)
b.bindListRef(a.Modifiers, kind)
b.bindN(b.at(a.Rest), kind)
b.bindN(b.at(a.Question), kind)
b.bindN(b.at(a.Type), kind)
b.bindInitializerN(b.at(a.Initializer), kind)
b.bindN(b.at(a.Name), kind)
```

BindingElement:

```go
a := s.AccessBindingElement(ref)
b.bindN(b.at(a.Rest), kind)
b.bindN(b.at(a.PropertyName), kind)
b.bindInitializerN(b.at(a.Initializer), kind)
b.bindN(b.at(a.Name), kind)
```

Modifiersの取得はアクセサに含めるが、最初にbindする順序は維持する。P6の宣言name受渡しはまだ行わない。
完了条件: 2関数にarity API・位置依存tupleがない。20入力一致。旧P1と新G1のstruct返却/inline/frame/spillを比較。
旧P1のframe（Parameter 96 B、BindingElement 80 B）より増えた場合、数字を記録し、理由を機械語で調べる。

## G2：既存の生成walker・If・PropertyAccessを移行する

1. G0 generatorでwalker対象nodeのAccessorを生成する。各caseは`a := s.Access<Node>(ref)`から名前付きfieldで元の訪問を行う。
2. listもAccessor fieldを使用する。既存の訪問順はschema visitor順、専用workerでは現行コードの順に従う。
3. aliasを共有するcaseは同じschema型のAccessorを使う。child数だけで同じAPIを使い回さない。
4. IfStatementRefs/PropertyAccessExpressionRefsの利用箇所も名前付きAPIへ移行する。
5. SyntaxChildrenN等の既存APIは互換のため当面残す。全repoを検索し未移行callerがないことを確認するまで削除しない。名前付きアクセサの内部実装としてarity APIを再利用しない。
6. frame・未使用fieldのload・未使用listのqualified化・atomic・CALLを旧scalar版と比較する。struct丸ごと保持による悪化があれば、生成側の寿命・配置・コード形状を改善する。公開APIを数付きtupleへ戻さない。
7. 改善不能な場合は影響caseと機械語を具体的に報告し、その移行を保留する。意味一致だけを理由に明確な退行を無視しない。

完了条件: 20入力一致、generator一致、移行済みcallerは名前付きAPIのみ。旧API残存と保留caseを明記。
次にP2以降へ進む。全体時間の有意差をこのカード単独の進行条件にはしない。

## P1：Parameter / BindingElementの一括取得（完了済み履歴）

旧SyntaxChildren5/4によるP1は検証済み。P1を再実装せず、名前付きAPIへの変更はG1として記録する。
旧P1の20入力監査・機械語結果は新G1の検証結果として流用しない。

## P2：skipParentheses → Call

対象: skipParenthesesRef、maybeBindExpressionFlowIfCallN、bindCallExpressionFlowRef。

1. `skipParenthesesN(n bindNode) bindNode`を追加する。`n.ref != 0 && n.kind == KindParenthesizedExpression`の間、child 0を取得して一度だけ`b.at`し、最終bindNodeを返す。
2. refしか持たない既存caller用wrapperは`return b.skipParenthesesN(b.at(ref)).ref`とする。全callerを強制変更しない。
3. maybeBindExpressionFlowIfCallNは返されたbindNodeを使い、直後のKindAtを消す。
4. Callの非optional branchでは`a := s.AccessCallExpression(ref)`、calleeを`b.at(a.Expression)`で一度取得。QuestionDot/TypeArguments/Argumentsもaのfieldを使用する。
5. 括弧除去後のcalleeは別変数にする。元calleeと同一と仮定しない。
6. 通常Callのwalker呼出しを、このnodeの元の4訪問（callee、questionDot、typeArguments list、arguments list）と同じ順の明示呼出しへ置換。
7. IIFE branchは元のtypeArguments→arguments→calleeの順を保つ。元にないquestionDot訪問を追加しない。
8. super判定と後半のpush/unshift判定で元calleeのbindNodeを再利用する。
9. optional branchはまず既存処理を保持。そのbranchの後処理に必要なcalleeは従来どおり取得する。未初期化のcalleeを使わない。

試験: 括弧0/1/複数、IIFE、super、通常call、push/unshift、optional call、欠損子。
完了条件: 非optional経路で同じcallee slotを取り直さず、最終skip kindを再取得しない。

## P3：BinaryのFlow領域内で参照を受け渡す

対象: bindChildrenRefのBinary case、isDestructuringAssignmentRef、bindBinaryExpressionFlowRef、bindLogicalLikeExpressionRef、bindDestructuringAssignmentFlowRef、bindBinaryExpressionKind。

1. 生成`ast.BinaryExpressionAccessor`を使用する。独自の位置依存tupleやbinarySyntaxRefsを重ねて定義しない。
2. `bindChildrenRef`のBinary case内で`a := s.AccessBinaryExpression(id)`とoperatorKindを一度取得する。OperatorはtokenのNodeRefでありkindではない。
3. destructuring判定のResolved版を作り、operatorKindとleftの既知情報で判定する。Flowの3 workerもResolved版を作り、同じAccessor/operatorKindを引数で受ける。
4. 既存Ref版が他callerに必要なら、一度resolveしてResolved版を呼ぶwrapperとして残す。worker本体のコピーを2つ残さない。
5. 通常/論理/論理代入/destructuringの元のbind順、inAssignmentPattern保存復元、Flow作成順を維持する。
6. `bindBinaryExpressionKind`の内部も必要なleft/operatorを一回ずつ解決する。ただしこの前段とbindChildrenRefの後段を跨ぐ全面共有は、このカードでは行わない。

理由: bindKind全体のframeや共通dispatchへBinary用引数を追加すると全nodeへ費用が波及する。まずFlow領域内の複数解決を確実に消す。
残る「bindKind前段→子走査」の2回解決は残存表へ明記。無断で大きなcontext、callback、bindKind末尾の複製を作らない。

試験: &&/||/??、各論理代入、comma、通常代入、object/array destructuring、入れ子、unreachable、JS assignment declaration。
完了条件: Flow領域のoperator再取得が消え、同じslotをworker間で読み直さない。前段を跨ぐ未対応分を明示。

## P4：単純な専用走査をまとめる

P4a: VariableDeclaration、Prefix/Postfix/Delete。
- Variableは`a := s.AccessVariableDeclaration(ref)`で取得し、a.Name→a.Exclamation→a.Type→a.Initializerを元どおりbindする。
- 初期化有無の判定に取得済みinitializerを使う。nameを受け取る`bindInitializedVariableFlowResolved`を作り、最上段の再取得を消す。
- binding patternの再帰では別childなので、そのchildのnameを新たに取得してよい。全再帰で一つのnameを使い回さない。
- 単項/DeleteはAccessPrefixUnaryExpression/AccessPostfixUnaryExpression/AccessDeleteExpressionのOperand/Expression fieldを取得し、元のtarget反転・訪問・後処理で使う。演算子scalarの取得時点は動かさない。

P4b: Conditional、While、Do、For、ForIn/Of、Try。
- 各nodeのAccess<Node>を一度呼び、元のgetter式を意味が対応する名前付きfieldへ置換する。schema表のNをAPI名に使わない。
- 最初のpatchではif/loop/return/Flow操作を移動しない。参照取得文の追加とgetter式の置換だけにする。
- unreachable、finally、await、continue/break/returnのbranchを削らない。

完了条件: 対象nodeのslot再解決がなく、20入力のcounts/trace/Flowが一致。diffが大きければP4a/P4bを別turnにする。

## P5：Switch / CaseBlock

1. `a := s.AccessSwitchStatement(ref)`のExpression/CaseBlockを使用。bind後のdefault検出にもa.CaseBlockを使う。
2. clausesのListLenをloop外へ。list owner解決の全面改修はP7。
3. CaseBlockのswitchExpressionをbindNodeで保持し、KindAtをOR両辺で呼ばない。
4. inner loopを抜けた現在clauseのref/kindを後段へ渡す。iが増えた時だけ次のclauseを取得する。
5. 既知kindを`bindRef`へ渡して再取得させず、`bindKind`またはbindNodeへ接続する。

試験: 空switch、defaultのみ、空clauseの連続、fallthrough、最後が空、true switch、break/returnを含むcase。
完了条件: Flow clauseStart/iの範囲が不変。同じiのListElem、同じexpressionのKindAtが重複しない。

## P6：宣言名の共通core（3カードに分ける）

P6a：共通coreだけ。
- `nameOfDeclarationRef`の結果を表すbindNodeを受け取る`getDeclarationNameResolved`、`hasDynamicNameResolved`を作る。
- 既存Ref wrapperは従来と同じresolverを一回呼んでcoreへ渡す。fallbackを再実装しない。
- nameRef=0は正当な「解決済み欠損」。Ref=0だから再resolveする実装は禁止。
- computed、private、ambient module、default/exportの分岐は同じ一つのcoreへ移す。文字列のmemoはしない。

P6b：Parameter/Variable/Propertyの受渡し。
- 既に取得したnameをdeclare経路のResolved版へ渡す。
- declareSymbolAndAddToSymbolTable→SourceFile/Module/Class→declareSymbolの経路に、必要なところだけname引数を通す。
- 解決済みnameを使う経路と従来wrapperが同じcoreを共有する。診断アルゴリズムを複製しない。

P6c：exportとFunction/Class。
- local/exportの2回declareは残す。両者へ同じ構文nameを渡す。
- function名のstrict checkと宣言worker、class名の後処理で同じresolved nameを渡す。
- Symbol取得・宣言追加・競合診断の順序を変えない。isComputedName等の既存引数の意味を変えない。

試験: export重複、merge、overload、default、computed/private name、parameter property、binding pattern、JS module.exports/require、匿名宣言。
完了条件: resolverを再呼出ししないResolved経路と、旧意味のfallbackが共存。文字列・Symbol cacheなし。20入力完全一致。

## P7：listの再解決をloop外へ出す

P7a：APIと境界試験。
- 既存`list-span-experiment`の整数Spanを参考に、`BindListSpan{start,length uint32}`とlocal-onlyの`TryBindListSpan`、`BindListSpanElem`を実装する。
- nil/zero/qualified local/unqualified local/foreignを区別。foreignはfalseを返し、callerは元のListLen/ListElem経路へ戻る。
- local解決時にownerを確認するのは一回。要素取得時はs.children[span.start+i]を読み、owner/list headerを再取得しない。
- 範囲checkを維持。返却後に要素値が更新された時は新しい値を読む。start/lengthが変わる構文編集の間は使わない。
- backing arrayをpointerで保持せずStoreから読むのでappend再配置に対応する。Compact/Restoreで位置が変わる区間を許可したと主張しない。

P7b：consumerへ適用。
- bindListRef→modifierFlagsRef→hasExportDeclarationsRef→listIndexRef→bindInitializedVariableFlowRef→eachList→functions-firstの順。
- local Span branchと従来foreign fallbackを用意し、foreign要素を別StoreのKindAtで新たに解釈しない。既存foreign動作の修正は別件にする。
- functions-firstは同じSpanで2回loopする。1pass化、全要素kindのcacheはしない。
- listIndexの探索算法・modifierFlagsのOR計算はそのまま。
- ListSlotAt→qualified ListRefの往復省略は後続カード。P7a/bと同時に配置・list identityを変更しない。

試験: 0/1/複数要素、nil element、foreign list、qualified/unqualified、範囲外panic、append後read、既存要素更新。
完了条件: localのListElem相当経路にlistOwnerがない。全consumerで元の訪問順。既存Spanの過去の速度値は今回の実測として使わない。

## P8：Handle narrowing helper

対象: isNarrowableReference、hasNarrowableArgument、isNarrowableOperandと直近callee。
- 同じOperator/ArgumentExpression/Expressionを短絡条件の後半で再取得する箇所を、必要になったbranchのローカルHandleへ置換。
- getterを短絡条件の前へ無条件に引き上げ、以前読まなかったinvalid/foreign slotを読むようにしない。
- Handleを保持してforeign fallbackを維持。NodeRefへ全変換しない。
- helper間の受渡しは取得済みchildだけ。narrowing判定のboolや再帰結果をcacheしない。

完了条件: 対象helperの意味分岐が同じで、同一childを再取得しない。parserで作れないforeignは小さいStore境界試験を追加する。

## P9：modifier / root / Optionalの構文情報

1. 同一宣言処理内のrootDeclarationと親chainを必要なworkerへ渡す。別宣言のrootを使い回さない。
2. modifier list descriptorはP7を使う。modifierFlagsの計算結果cacheは行わない。
3. OptionalChainではexpression/questionDot/parent bindNodeだけを必要なworkerへ渡す。Flags判定は元の時点で行う。
4. isOutermostのparent optional判定とroot判定の間で取得済みの構文childを共有する。Flagsを先読みしない。

完了条件: helper境界で失っていた構文情報だけが増えた引数になり、mutable意味情報がsnapshotに混入しない。

## D1 / D2：Lunaが独断で横断設計しない領域

D1（親情報）:
- 既存parent-argument実験を読み、isIdentifierNameRefの123,553回という旧頻度だけで全訪問へ引数を増やさない。
- 取得済み親ref/kind/slotを持つcaller、保持期間、変更シグネチャ数、escape/frame見込みを表にする。
- 限定callerで渡せる具体的なpatch案を作る。全祖先stackは作らない。実装範囲をレビューしてから適用する。

D2（header位置とmutable意味情報）:
- Flags/Symbol/Localsの各read間にあるwriterを一覧にし、値の共有が禁止される箇所を特定。
- nodes配列の再配置・Compact/Restore・別goroutineのwriterをどう排除するか、契約と検査入口を設計する。
- 契約なしにheader pointer/sliceをBind全体へ保持する実装をしない。
- 作業成果は契約案とcandidateの最小API。コード変更は契約・保持期間をレビューしてから行う。

この2カードの設計待ちはP1〜P9を止める理由ではない。追跡表には「設計待ち」と理由を書く。

## 共通検証とV1/V2/V3

各実装カード:

1. patch前後の関数とgetterの対応表を保存。取得回数とbind回数を混同しない。
2. 変更APIに必要な境界試験のみ追加。実装をそのままなぞる大量のmicrobenchmarkは作らない。
3. `tsc`で`go test ./internal/ast ./internal/binder ./internal/compiler`を実行。generator変更時は再生成一致も確認。
4. P0 driverで20入力の構文・diagnostics・Symbol・Flow・node symbols・counts・visit traceを比較。
5. accessor計数は減ってよい。意味digestの差を許容リストへ追加して通さない。
6. 対象関数のobjdumpを保存し、header/start/ownerの解決箇所、CALL、frame、spillを比較。
7. helper CALLが残る場合は名前やdirectiveで強制せず、実装の大きさ/inline判定を調べる。解決が一回でもCALLが残ることと、inlineで一回解決できたことを区別して報告。
8. 関係ない差分がないことを確認。既存dirty差分を自分の変更として報告しない。

V1/V2の比較:
- P0 baselineと累積candidateを比較し、必要なら直前checkpointとの差も取る。
- checker/dom各10 bind×6 round、順序反転。同じnormal binaryのA/Aも実施。raw→benchstatを保存。
- wall/GOGC=100とKPC/GOGC=offを逐次実行。build/test/他benchmarkを同時実行しない。pprofは使わない。
- A/A区間の許容はwall/cycles±1.5%、EL0命令±1%。非有意や精度不足を勝利とも同等性証明とも扱わない。roundを勝手に増やさない。
- 小さな局所変更にwall有意差がないだけで作業を中断しない。意味一致と再解決削減が確認できれば記録して次へ進む。
- 再現性ある全体退行、解決回数増、過大なframe/escapeがあれば、原因カードを分離して修正。古いbaseline全体へ巻き戻さない。

V3:
- 実装済み/残存/設計待ちを15分類へ対応付ける。Binaryの前段跨ぎ等の残存を明記。
- 合意済み範囲の最終性能・正しさを報告。parse+bindとGC/live/scanが未測定ならmissingとし、全面採用・pointer同等の結論を出さない。

報告必須: 選択repo、候補clone、artifact set/status、ns/op、B/op、allocs/op、hot path、allocation driver、診断、次の作業。
currentはrepo_root/tsgolint_git_rev/typescript_go_git_revが一致した時だけ。dirty source SHAは別途照合。
異なるidentityはstale、新規未測定はmissing、bench.txtに対象Benchmark行がなければunsupported。

## 作業記録のテンプレート

カードごとに[作業記録](luna-accessor-implementation-progress-20260910.md)の同じ行を更新する。

| ID | 状態 | 変更関数 | 消した再解決 | 残存 | 意味監査 | 機械語/フレーム | artifact |
|---|---|---|---|---|---|---|---|
| G0 | 未着手 | — | — | — | — | — | — |

状態は未着手/作業中/実装済み/検証済み/設計待ち。意味監査未実行を検証済みと書かない。
benchmark未実行のカードは性能欄に「未測定」。過去の結果を自分の変更の結果にしない。

## Lunaへ渡す次の指示

P0/P1は完了済み。次はG0→G1でAPIの生成と2関数の移行を行う。以下をそのまま依頼できる。

> `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/internal/binder/docs/luna-accessor-implementation-plan-20260910.md`の改訂版を読み、G0とG1を順番に実装してください。P0/P1の成果とdirty変更は保持し、作り直さないでください。SyntaxChildrenNの新規利用をやめ、schemaからAccessParameter/ParameterAccessor、AccessBindingElement/BindingElementAccessorを生成してください。名前付きfieldには必要なListRefも含め、内部でkindを検査しheader/startを一度解決してください。P0 driverのsnapshot対象に新生成物を追加し、G1の2関数を文書のbind順どおり移行してください。再生成一致・関連テスト・20入力監査・inline/frame/spillを確認し、作業記録を更新してください。今回はG2以降や時間benchmarkへ広げないでください。

後続はG2、その後P2以降をIDで指定する。複雑なP6/P7はa/b/cに分ける。
この計画改訂ではtaskの作成・送信を行わない。

## 参照基準値（今回の計画作成では測定なし）

旧arity APIの参照値（P1以降の現行sourceの測定値ではない）: syntax-scalars all_batchのcheckerは checker 16,362,465 ns/op、12,799,473 B/op、14,165 allocs/op。
dom 5,896,931 ns/op、7,866,729 B/op、16,684 allocs/op。保存benchstatで命令−1.69%/−1.76%、通常wall非有意。
主なhot pathはwalk/helpers。allocation driverはsymbolIdx/flows、symbolRefs、FlowNode/Symbol/Handle拡大。今回のaccessor作業だけでGC改善は保証しない。

Storeは上記repo/32598cba、pointerは`/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`/8ac035a。双方tsgolint_git_rev=null。
候補cloneはrepeated-access-inventoryの末尾およびsyntax-scalars-experiment/worktrees.txtの全件。別介入として未使用。
旧計画時の証拠は`repeated-access-inventory/inventory-evidence.json`。以後P1が実装されたため、そのsource一致を現行版へ持ち越さない。
P0/P1の監査artifactは作業記録に保存済み。G0/G1開始時にidentityと現在source SHAを再照合する。
新計画のbenchmarkはmissing。今回コード実装・モデルへの委譲・benchmark実行は行っていない。

## 2026-09-11 レビューを受けた補足仕様

共通規則12の実装として、listを使わないBinary専用処理とCall構文判定には
`AccessBinaryExpressionChildren` / `BinaryExpressionChildrenAccessor` と
`AccessCallExpressionChildren` / `CallExpressionChildrenAccessor`を生成する。
名前付きchild fieldsだけを返す。kindと**元schema全体**のchildLen/listLen検査は残す。
listのfield・取得処理を生成しないことで、compilerによる未使用field除去に依存しない。
全フィールドを訪問する生成walkerは従来のAccess*を使い、listのbind順は変えない。

OptionalChain後段にはexpression/questionDotだけでなく、後段で読むname/argument/listの
整数構文参照を渡す。Flagsは含めない。親や別の判定経路の再取得まで除去したと報告しない。
FunctionExpressionでは既に取得したnameRef/nameKindをstrict-mode検査へ渡す。

objdumpは任意のsymbol regexと必須関数名を受け取り、通常ビルドと監査ビルドを明示的に区別する。
空出力・必須関数欠落・binary hash不一致を成功と扱わない。frame/spill/CALLはビルド種別ごとに報告する。
V3は意味監査だけで「完了」にせず、wall/KPC・parse+bind・GCの未測定をmissingとして残す。
