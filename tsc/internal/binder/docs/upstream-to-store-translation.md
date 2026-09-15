# upstream Binder から Store 版への変換規約

2026-09-15。Binder 再実装のための設計文書。本文の変換後コードは実装予定の形であり、実装済みの報告ではない。

## 1. 基準と到達する形

upstream の Binder を出発点に、構文の保持・アクセス方法だけを Store に合わせる。元の関数名、分岐、診断、Symbol の宣言・統合、Flow の構築順を追える形にする。旧 Store Binder の移行途中の関数構成を復元する作業にはしない。

| 対象 | 固定した基準 | 用途 |
| --- | --- | --- |
| 選択 repo | `/Volumes/SanDisk1TB/worktree/binder-rewrite` | 文書と再実装の作業先 |
| upstream | `879f9867ac455404e75759dd1739281cf6aa7f85` | Binder の意味・制御構造の正 |
| Store 側 HEAD | `85506e8b7d51f2b05f84f9ba1a8c3309b2f30ce4` | 周辺 AST API・消費者の契約 |
| 貼り付け済み binder.go | upstream と完全一致、2,802 行 | 再実装の起点 |
| upstream binder blob | `e20bbaea3ce0bdc87bbdff916c0155ae9430487d` | 原文の識別 |

貼り付け済みファイルの SHA256 は `00b8528e353033a08a28f78f0c66f7c56d27d4634fd586b096e3330f6ef43883`。HEAD の旧 Store Binder は 3,697 行。原文の確認には、動く可能性のある `upstream` 名だけでなく上記 commit を使う。

```sh
git show 879f9867ac455404e75759dd1739281cf6aa7f85:tsc/internal/binder/binder.go
git show 85506e8b7d51f2b05f84f9ba1a8c3309b2f30ce4:tsc/internal/binder/binder.go
```

現在の作業ツリーは、原文の `*ast.Node` Binder と移行後の Store API・生成 walker が混在する意図的な作業開始状態である。この状態をコンパイル可能な Store 基準版とは呼ばない。

合意した範囲は次のとおり。

- 通常の同一 Store 内の Binder 処理は `NodeRef` に統一する。Binder が Store を保持する。
- 一括取得は各関数の中で行う。既存生成 getter/dispatch の直近の kind 引数は基本形に含め、それ以外の関数をまたぐ取得済み情報の追加受け渡しは性能例外として別に評価する。
- Symbol・Flow・Checker・既存共通 API の表現は維持する。Handle や合成 Node が必要な境界を明示する。
- この文書の成果物は変換規約。Binder のコード変更、測定、周辺 resolver の独立した upstream 差分の取り込みは次段階とする。

過去の移行カードは実験の履歴として参照する。再実装の基本形と矛盾する `bindNode`、親 kind の中継、Resolved 関数群の維持指示は、今回の再実装へ引き継がない。

## 2. 型とアクセスの逐語変換

API の実体は [Store](../../ast/store.go)、[生成 Accessor](../../ast/store_accessors_generated.go)、[共通 query](../../ast/store_query_manual.go)。境界の型は [Symbol](../../ast/symbol.go)、[Flow](../../ast/flow.go)、[SourceFile と pointer AST](../../ast/ast.go) を参照する。

### 通常処理の表現

内部の基本入口は `func (b *Binder) bind(node ast.NodeRef) bool`。原文の node 引数・container 状態・遅延処理の同一 Store 内参照を `NodeRef` にする。`kind` は必要な関数内で取得し、意味処理の各引数・Binder field と恒常的に対にして保持しない。例外として、既存の生成 getter/dispatch の `(ref ast.NodeRef, kind ast.Kind)` は維持し、呼出元でその ref について取得した kind を直接渡す。別 node の kind、親 kind の中継、取得済み kind を意味処理の関数間で運ぶ規約には広げない。`bindRef → bindN → bindKind` の中継や独自の `bindNode` 型を追加しない。

`bool` は原文の `bind(*ast.Node) bool` から維持する戻り値である。`doWithConditionalBranches` の callback も node 引数だけを NodeRef に変換し、戻り値・呼び出し順を保つ。

| 原文 | 基本の変換 | 注意点 |
| --- | --- | --- |
| `*ast.Node`、具体的な構文ノード型 | 通常の内部処理は `ast.NodeRef` | 型別の子は関数内で `AccessX` により取得 |
| `node == nil` | `node == ast.NoNodeRef` | 0 は参照なし。存在する missing node は非0 |
| node 同士の同一性比較 | 同一 Store 内では ref 比較 | Store をまたぐ比較は Handle のまま |
| `file.AsNode()` | `file.ParseTreeRef()` の root | Store/root を一組で取得。SourceFile wrapper を構文木として辿らない |
| `node.Kind` | `b.store.KindAt(node)` | ローカルで必要な回数だけ取得 |
| `node.Flags`、その更新 | `FlagsAt`、`SetFlagsAt` | 更新直前に現在値を読み、原文の bit 操作を行う |
| `node.Loc`、`Pos()`、`End()` | `LocAt(node)` と TextRange のメソッド | 診断 span の算出規則は変えない |
| identifier / literal の `node.Text()` | `TextAt(node)` | MetaProperty / JsxNamespacedName 等の合成 text は `Handle.Text()` の kind 別処理を維持 |
| `node.Parent` | `ParentRef(node)` | 同一 Store の親に限る。境界では `Handle.Parent()` |
| `node.AsX()` から複数 child を読む | `AccessX(node)` の名前付きフィールド | 取得は構文 snapshot。bind は原文順 |
| 単項式の `Operator` 等の scalar | 既存の型付き scalar getter を境界で呼ぶ | Accessor は scalar を含まない。child の OperatorToken と混同しない |
| `node.Name()` 等の多相 getter | 対応する既存の生成 ref getter | kind dispatch と欠損の振る舞いを照合。未対応の共通 query は Handle API 境界で呼ぶ |
| NodeList・ModifierList | `ast.ListRef` | `ListLen` と `ListElem` のループは同一 Store 内に限定 |
| `[]*ast.Node` の構文列 | 所有元の ListRef を使う | 既存 API が返す `[]Handle` / NodeSeq は境界として扱い、機械的に ref slice を作らない |

slot 番号や位置依存の tuple は手書き Binder に持ち込まない。単一フィールドを読む経路では単項 getter を使い、必要のない全 child/list を読む改修にしない。既存の ref getter 群は [生成 walker](../bindwalk_generated.go) の `nameRefGenerated`、`initializerRefGenerated` 等にある。18 個の getter 系関数は `(ref, kind)` の既存署名を採用し、ref-only に変更して内部で `KindAt` を再読する設計は採らない。生成 walker の dispatch も同じ局所例外とする。これらを使う場合も、本文の通常入口と名前を重複させる移行 wrapper は作らない。

### 呼出し側から決める署名

次は利用例の断片であり、原文の診断・分岐全体を省略している。生成 leaf へ渡す kind は対象 ref 自身のものに限る。

```go
// bind 内: 取得済み kind は直近の生成 getter で使う。
kind := b.store.KindAt(node)
nameRef := b.nameRefGenerated(node, kind)
b.bind(nameRef) // 再帰先へ kind は運ばない。

// lookupName 内: map の有無は kind 能力から推測しない。
if local := b.store.Locals(container)[name]; local != nil {
    return core.OrElse(local.ExportSymbol, local)
}

// JSON 分岐内: external module Symbol を作った後に行う。
originalSymbol := b.file.Symbol
b.declareSymbol(ast.GetSymbolTable(&originalSymbol.Exports), originalSymbol,
    root, ast.SymbolFlagsProperty, ast.SymbolFlagsAll)
b.store.SetSymbol(root, originalSymbol)
b.file.Symbol = originalSymbol
```

利用例から導く内部形は次のとおり。既存 field の残りと body は省略した設計スケッチで、新しい公開 constructor / `Bind` method は追加しない。

```go
type Binder struct {
    file  *ast.SourceFile
    store *ast.Store
    container, thisContainer, blockScopeContainer, lastContainer ast.NodeRef
    // その他の状態・arena・pool は現行契約に適応する。
}
func (b *Binder) bind(node ast.NodeRef) bool { panic("not implemented") }
func (b *Binder) bindChildren(node ast.NodeRef) { panic("not implemented") }
func (b *Binder) lookupName(name string, container ast.NodeRef) *ast.Symbol {
    panic("not implemented")
}
func (b *Binder) getLocals(node ast.NodeRef) ast.SymbolTable { panic("not implemented") }
// 既存生成 leaf の署名を維持する。
func (b *Binder) nameRefGenerated(ref ast.NodeRef, kind ast.Kind) ast.NodeRef {
    panic("not implemented")
}
```

モジュールの責任は、`binder.go` が宣言・Flow・SourceFile 同期、`generate-go-ast.ts` と `bindwalk_generated.go` が構文 slot と訪問順、`ast.Store` が保存と所属、既存 ast query が Handle による探索、監査 driver が比較用の正規化を持つ形とする。監査の構造パスや ID を本番 Binder の引数・field には入れない。公開 `BindSourceFile(file)` は Store/root の取得、BindOnce、pool の順序を隠し、呼出側へ新しいライフサイクル操作を要求しない。

### Bind の書き込み先

| 原文の情報 | Store 版の保存先・操作 |
| --- | --- |
| SourceFile の declaration Symbol | parse root への `SetSymbol(root, symbol)` と同時に `file.Symbol = symbol`。JSON の一時宣言後の復元も両方に書く |
| declaration Symbol / LocalSymbol | `Symbol` / `LocalSymbol`、`SetSymbol` / `SetLocalSymbol` |
| node の Flow | `Flow` / `SetFlow` |
| function の EndFlow / ReturnFlow | `EndFlow` / `SetEndFlow`、`ReturnFlow` / `SetReturnFlow` |
| case の FallthroughFlow | 既存の `Handle.FallthroughFlowNode` / `SetFallthroughFlowNode` 境界 |
| container Locals | `Locals` / `SetLocals`。原文と同じ条件で遅延作成 |
| container chain | `NextContainer` / `SetNextContainer` |
| Symbol.Members / Exports | 既存の Symbol の map と `ast.GetSymbolTable` |
| 診断、SymbolCount、module 指標 | SourceFile の既存 API・field と parse root の Symbol |

`ast.GetSymbolTable(&symbol.Exports)` 等の map 作成・差し替えのタイミング、default export の分岐、LocalSymbol と ExportSymbol の関係を維持する。再帰前に取得した map・Symbol・Flags の値を、更新をまたいで再利用しない。

### Flow と Locals は使用箇所ごとに変換する

`FlowNodeData() != nil` 自体は field を持つ能力の判定だが、固定 upstream の全使用箇所では実行時の能力 predicate は不要である。構文上の呼出は 3 箇所（1653、1665、2512）で、2512 の `setFlowNode` の 3 呼出元を展開すると次の 5 経路になる。`hasFlowNodeData(kind)` の生成も、schema 全体の能力表の追加も行わない。

| upstream binder.go の経路 | 対象と根拠 | Store 変換 |
| --- | --- | --- |
| 620 → `setFlowNode` | PropertyAccessExpression / ElementAccessExpression は FlowNodeBase を持つ | 元の currentFlow / narrowing 条件の内側で `SetFlow(node, b.currentFlow)` |
| 918 → `setFlowNode` | FunctionExpression / ArrowFunction は FlowNodeBase を持つ | `SetFlow(node, b.currentFlow)` |
| 984 → `setFlowNode` | 元の条件を通る MethodDeclaration / GetAccessor / SetAccessor は FlowNodeBase を持つ | 元の currentFlow / method 条件の内側で `SetFlow(node, b.currentFlow)` |
| 1665、文の範囲 | VariableStatement から DebuggerStatement の 17 kind はすべて StatementBase → FlowNodeBase | 元の kind 範囲の内側で `SetFlow(node, b.currentFlow)` |
| 1653、unreachable | 任意 kind。未設定と nil 設定は `Store.Flow` から区別不能 | `if b.store.Flow(node) != nil { b.store.SetFlow(node, nil) }` |

17 kind は Variable、Expression、If、Do、While、For、ForIn、ForOf、Continue、Break、Return、With、Switch、Labeled、Throw、Try、Debugger の各 Statement。Block / EmptyStatement はこの範囲外である。根拠は固定 source の `ast.go` / `ast_generated.go`、[schema](../../../../tools/scripts/tsc/ast.json) の基底、[kind の範囲](../../ast/kind_generated.go) を照合する。将来呼出元や kind 範囲が変わる際にこの対応を再確認する。

unreachable で無条件に `SetFlow(node, nil)` しても読み取り値は同じであり、旧 Store HEAD もそうしている。ただし `putCol` は未確保の列を伸ばすため、基本形では既に非 nil の値があるときだけ消去する。この値判定を reachable な Flow 代入へ流用しない。`IsPotentiallyExecutableNode` による Unreachable Flags の設定、子訪問、状態復帰は原文どおり残す。

| Locals の使用箇所 | 問い | Store 変換 |
| --- | --- | --- |
| 403、`declareModuleMember` | upstream の `IsLocalsContainer`（LocalsContainerData の有無）か | `ast.IsLocalsContainerKind(b.store.KindAt(container))`。ここへ渡る container は ModuleDeclaration / SourceFile だけで両判定が一致する。未作成 Locals と区別する |
| 1295、`lookupName` | その container の Locals に名前があるか | `locals := b.store.Locals(container)` を読み、`locals[name]` が非 nil なら `core.OrElse(local.ExportSymbol, local)`。次に Symbol.Exports を検索 |

`IsLocalsContainerKind` は LocalsContainerBase を埋め込む全 kind の能力判定ではない。ClassDeclaration、ClassExpression、FunctionType、ConstructorType、CallSignature、ConstructSignature、IndexSignature、MethodSignature は Locals を持つが同 helper の集合に含まれない。lookupName の読み取りは、能力がない場合と map が nil の場合のどちらも検索結果がないため値判定で等価になる。固定 upstream の `IsLocalsContainer` 自体は `LocalsContainerData() != nil` であり、狭い kind 集合ではない。403 の変換は到達可能な container が ModuleDeclaration / SourceFile であることに依存する。caller を増やす際はこの制約を再監査する。lookupName へ流用しない。class の型パラメータを含む名前検索を受入例にする。

### SourceFile.Symbol の同期と復元

upstream では `file.AsNode()` の declaration Symbol と `file.Symbol` は同じ field である。Store では parse root の側テーブルと wrapper の field が別なので、`addDeclarationToSymbol` の SourceFile 分岐で両方に同じ Symbol pointer を代入する。同期責任は Binder に置き、SourceFile を知らない汎用 `Store.SetSymbol` の副作用にはしない。参照側は [checker 初期化](../../checker/checker.go)（1269行） と [emit resolver](../../checker/emitresolver.go)（350行） が field を、通常の declaration query が node 側を読む。

JSON の `bindSourceFileIfExternalModule`（upstream 766–768）は module Symbol を保存し、同じ root に property Symbol を一時宣言してから元へ戻す。復元時も `SetSymbol(root, originalSymbol)` と `file.Symbol = originalSymbol` を対にする。旧 Store HEAD はこの末尾で field だけを戻しており、root が一時 property Symbol のままになる。この差は補助対照との既知の差として記録し、upstream に合わせて両方を復元する。通常宣言後と JSON 復元後に `store.Symbol(root) == file.Symbol` を確認する。一時 property Symbol 自体は Exports の要素として残る。

### 残す境界

| 境界 | 維持する契約 |
| --- | --- |
| Bind の公開入口 | `BindSourceFile(file *ast.SourceFile)` と BindOnce / pool のライフサイクル |
| Checker が使う API | `GetContainerFlags(node ast.Handle)`、`SetValueDeclaration(symbol *ast.Symbol, node ast.Handle)` |
| strict prologue の公開 API | `FindUseStrictPrologue(sourceFile *ast.SourceFile, statements []ast.Handle) ast.Handle` |
| Symbol の AST 参照 | `Declarations []ast.Handle`、`ValueDeclaration ast.Handle` |
| Flow の通常 AST 参照 | `FlowNode.Node ast.Handle` |
| Flow の合成データ | 最初の互換基準版では `FlowNode.Data *ast.Node`。switch/reduce の専用データ。下記の順序で `*ast.FlowData` に移行 |
| 共通 query、scanner の診断 API | 要求された範囲で `b.store.At(node)` を渡す |
| foreign 対応の query | 所属 Store を持つ Handle で辿る。既存の narrowing helper テストを維持 |

公開署名は貼り付け済み原文ではなく、上記 HEAD の Store 側の契約である。Handle の空値は `ast.Handle{}`、判定は `IsNil()` とする。公開入口と内部処理で意味処理を二重に実装しない。Handle を作るためだけに新しい抽象型・payload 中継関数を追加せず、API を呼ぶ場所で変換する。

Flow は `Store.NewFlow` から生成し、通常 AST は Node に、`NewFlowSwitchClauseData` / `NewFlowReduceLabelData` の結果は Data に格納する。原文の `newFlowNodeEx` が両方を受ける構造は、現行 Flow 型に合わせて通常参照と合成データの生成処理に分ける。これは性能例外ではなく表現上必要な適応である。Symbol の一件宣言用 arena も既存契約に合わせ `core.Arena[ast.Handle]` とする。

### pointer Node 削除計画との順序

[削除計画 Step 1](../../ast/docs/pointer-node-removal-plan.md) は `FlowNode.Data *Node` を `*FlowData` に置き換えるため、上記の現行境界と恒久的には両立しない。順序を **現行 Flow 型で Binder の互換基準版を確立 → Step 1 を AST / Binder / Checker 同時に適用 → 意味検証を再実行 → 性能候補を比較** と固定する。今回の文書修正で Step 1 本体は実装しない。

Step 1 では Binder の payload 生成経路（`newFlowData` と switch / reduce の生成箇所）、`NewFlowSwitchClauseData` / `NewFlowReduceLabelData` の戻り値、Checker の payload 読み取りと reduceLabels の型をまとめて変更する。Flow の通常 `Node ast.Handle`、公開 Binder API は維持する。Binder を二度適応する費用を受け入れ、互換性の差と payload 表現の差を個別に確認する。§5 の候補と対照は同じ payload 型を使う。Step 1 前後の数値を getter 改善の before/after として比較しない。

Store のコメントが推奨する pointer 削減を理由に、Symbol の宣言列まで NodeRef / GlobalRef へ変更しない。GlobalRef は Store を含むプロセス内識別子であり、今回の Binder 内部の通貨にはしない。

## 3. 親での一括取得と寿命

ここでの「親」は子スロットを所有するノードを指す。全訪問で AST の親情報を引数に載せる設計とは別である。

生成済み `AccessX` は、親 header の kind/shape を検査し、子スロットをまとめて読み、list slot を解決する。返却値は NodeRef/ListRef の構造体。フィールドを読むたびに Store へ問い合わせる view ではない。子の kind・Flags・Symbol・Flow・Store pointer や配列 slice は保持しない。

### 原文と変換後の例

原文の Parameter は、保存されている field の並びとは異なる順序で bind する。

```go
func (b *Binder) bindParameterFlow(node *ast.Node) {
    param := node.AsParameterDeclaration()
    b.bindModifiers(param.Modifiers())
    b.bind(param.DotDotDotToken)
    b.bind(param.QuestionToken)
    b.bind(param.Type)
    b.bindInitializer(param.Initializer)
    b.bind(param.Name())
}
```

基本変換では同じ関数内で一括取得し、その後の処理を対応させる。次の例の `bindModifiers` の引数は ListRef、`bind` / `bindInitializer` の引数は NodeRef に変換済みであることを前提とする。

```go
func (b *Binder) bindParameterFlow(node ast.NodeRef) {
    param := b.store.AccessParameter(node)
    b.bindModifiers(param.Modifiers)
    b.bind(param.Rest)
    b.bind(param.Question)
    b.bind(param.Type)
    b.bindInitializer(param.Initializer)
    b.bind(param.Name)
}
```

同様に `bindIfStatement` の `node.AsIfStatement()` は `b.store.AccessIfStatement(node)`、`stmt.Expression` は `stmt.Condition` に対応する。then/else/post の label 作成、条件の bind、各 branch の currentFlow 設定・合流の順序はそのまま残す。

| データ | 取得と再利用の範囲 |
| --- | --- |
| Accessor の child/list ref | 原文が必要とする分岐で取得し、構文が変わらない同じ関数の処理中に再利用 |
| kind | 必要な場所で取得。同じ node の局所判定と直近の既存生成 getter/dispatch 引数で利用 |
| Flags・Symbol・Flow・Locals | 元の読み取り位置で現在値を取得。構文 snapshot に混ぜない |
| list span | 基本形には入れず、§5 の候補。start/length が変わらない走査範囲だけ |

整数 snapshot は配列の再配置を理由に dangling pointer にはならないが、構文の書き換えを反映しない。Compact / Restore による参照の無効化も許容しない。構文 writer を呼ぶ経路では取得位置を writer の後に置くか、変更後に取り直す。別 goroutine の writer と同時に読めるという保証も追加しない。

未取得と「取得済みだが名前なし」を同じ 0 で表さない。基本形では原文の必要分岐で取得する。性能例外として遅延 context を作る場合にのみ、取得済み状態を別に表す。

## 4. 走査と同一 Store の条件

### 生成部分と意味処理の分担

`bind` の宣言・strict-mode 検査・parse error 伝播、`bindContainer` の保存復帰、`bindChildren` の Flow 分岐は原文を移植する。生成 walker は汎用 `bindEachChild` の子訪問だけを担当する。

[生成器](../../../../tools/scripts/tsc/generate-go-ast.ts) を変更して、生成 walker も `bind(node)` と ListRef を扱う関数に揃える。parentKind の中継や旧関数を呼ぶ生成物を残さない。単項 ref getter は必要なものを使い、呼ばれなくなった Binder 専用 helper は生成元と生成物から一緒に除く。

訪問順の基準は upstream の `ForEachChild`。schema 順で表現できる kind はそれを生成する。手書き visitor のある kind は同じ条件・順序を別途実装する。特に JSDocParameterOrPropertyTag は `IsNameFirst` により Name と TypeExpression の順序が変わる。現在の Store `forEachChildSchema` はこれを処理するが、現在の `generateBinderWalk` は同じ対応を持たない。生成されているという理由だけで正しいと扱わない。

旧 `bindChildrenOf` の物理レイアウト順、つまり named child をすべて訪問してから list を訪問する fallback は引き継がない。schema / 手書き visitor の対応が欠けた場合は生成時に検出する。

| 経路 | 維持する順序・条件 |
| --- | --- |
| SourceFile / Block / ModuleBlock | function declaration を先に処理する二回走査。SourceFile の EOF 訪問位置も維持 |
| Parameter | modifiers → rest → question → type → initializer → name |
| BindingElement | rest → propertyName → initializer → name |
| For / ForIn / ForOf | 原文の実行順。field 配置順にはしない |
| Binary / destructuring / optional chain / IIFE | 専用分岐・短絡・argument 先行の条件・Flow target の保存復帰を維持 |
| unreachable / parse error | 元の対象 kind、Flow 消去、Flags 設定、伝播を維持 |
| deferred expando | 通常走査の終了後に、保存した container 文脈で処理 |

### foreign の扱い

NodeRef は所属 Store なしには解釈できない。通常の parser が作る同一 Store の parse tree を主走査の前提にする。別 Store の任意の合成木を BindSourceFile で bind できる機能は今回追加しない。

- parser は `externalChild` / `externalList` を書かない。debug / テスト時に主走査の入口で両 map が空であること、root と list owner が当該 Store であることを確認する。この前提の下では `AccessX` / `ChildRef` / list 要素の 0 は「子がない」と扱い、bind は訪問しない。実在する missing node は非0で訪問する。
- 両 map は ast package の private field なので、検査は ast 内のテストまたは debug 用境界検査として実装する（新規の検証処理であり、既存公開 API ではない）。不成立なら主走査を開始せず検査を失敗させる。foreign を含む木を黙って部分 bind しない。通常の parser 経路に per-node の検査や fallback を追加しない。
- ListRef は owner を含む。`ListElem` / `ListRefAt` は list owner を解決したうえで、その Store に相対的な NodeRef を返す。戻り値が `b.store` 所属とは限らない。
- 外部 list 要素は整数スロットでは0になる。`ListAt` は owner と external element を解決した Handle を返す。foreign を扱う query では `Handle.Child` / `Parent` / `Store.ListAt` 等の既存 API を使う。
- Handle から主走査へ入る境界は同一 Store であることを確認する。異なる owner を捨てて主走査へ流さない。内部の全関数に同じ検査を散らさない。
- `TryBindListSpan` の成功は list header が local であることを示す。要素に foreign 参照がないことまで保証するものではない。

## 5. 性能例外の追加と採否

### 基本変換との差分を分離する

| 分類 | 例 | 扱い |
| --- | --- | --- |
| 必須の表現適応 | NodeRef、Store side table、Store.NewFlow、Flow の Node/Data 分離 | 基本変換 |
| 合意した局所一括取得 | `AsParameterDeclaration` → `AccessParameter` | 基本変換。高速化の実証とは別 |
| Store の追加取得費用を減らす変更 | 名前 ref の引数渡し、child-only projection、list span | 性能候補として独立比較 |
| 共通アルゴリズムの変更 | 二回走査の統合、診断・narrowing の memo、役割による訪問省略 | 今回の逐語変換に混ぜず別設計 |

`AccessBinaryExpressionChildren` / `AccessCallExpressionChildren` は既存 API だが、再実装で full accessor の代わりに選ぶ場合は候補として差分を記録する。過去に存在した最適化を無条件に再採用しない。`PrepareBindTables` の事前確保を引き継ぐ場合も性能条件として記録し、候補比較の途中で出し入れしない。

### 共通 helper の分類と基準版の既知の偏り

固定 upstream Binder の `ast.X(...)` 呼出名を重複排除すると105種ある（型の定数参照・メソッド呼出は除く）。以下はその全分類であり、呼出回数や実行時比率ではない。非生成 query の本体は [utilities.go](../../ast/utilities.go)、生成 kind 判定は [ast_generated.go](../../ast/ast_generated.go) に照合した。`*Ref` 中継を廃止することと、kind / Flags だけの問いにも Handle を必須にすることは別である。

| 分類 | 対象 helper | 基本変換 |
| --- | --- | --- |
| 単一 kind の生成判定（28） | IsIdentifier、IsPrivateIdentifier、IsComputedPropertyName、IsVariableStatement、IsVariableDeclaration、IsFunctionDeclaration、IsClassDeclaration、IsTypeAliasDeclaration、IsJSTypeAliasDeclaration、IsModuleBlock、IsExportAssignment、IsNamespaceExport、IsExportSpecifier、IsClassStaticBlockDeclaration、IsStringLiteral、IsNumericLiteral、IsBinaryExpression、IsPrefixUnaryExpression、IsFunctionExpression、IsPropertyAccessExpression、IsCallExpression、IsParenthesizedExpression、IsTypeOfExpression、IsConditionalTypeNode、IsJsxNamespacedName、IsSourceFile、IsModuleDeclaration、IsExportDeclaration | 同じ kind 比較を呼出側で行う。0 は KindUnknown として元の結果を維持 |
| 複数 kind の局所判定（10） | IsAccessExpression、IsBindingPattern、IsBooleanLiteral、IsDeclarationStatement、IsForInOrOfStatement、IsFunctionLike、IsLocalsContainer、IsPropertyNameLiteral、IsStringLiteralLike、IsStringOrNumericLiteralLike | 対応する kind 集合の比較。公開 Kind helper がある FunctionLike / Locals はそれを利用。IsDeclarationStatement の private kind helper は固定集合を呼出箇所に展開し、新しい公開 predicate は作らない。元の nil guard を維持 |
| Flags / kind（2） | IsInJSFile、IsOptionalChain | `FlagsAt` と kind の局所判定。可変 Flags はその場で読む |
| Loc / kind / nil（2） | NodeIsMissing、NodeIsPresent | `ref == 0`、Loc の Pos/End、EOF kind の条件を同じ式にする。missing と参照なしは区別 |
| text（2） | IsPushOrUnshiftIdentifier、ModuleExportNameIsDefault | 固定呼出元は Identifier / export name literal なので `TextAt` で同じ文字列比較。一般の合成 text query には拡張しない |
| Handle 境界（45） | 下表 | 既存共通アルゴリズムを `b.store.At(ref)` 経由で呼ぶ |
| Locals の遅延作成（1） | GetLocals | 原文の必要位置で既存 `getLocals(ref) ast.SymbolTable` に適応。nil ref は nil、未作成なら map を作って SetLocals。lookupName の読み取りでは使わない |
| Node identity（1） | GetNodeId | `b.store.At(ref).NodeId()`。ref の整数値を NodeId として流用しない |
| Node 引数を取らないもの（14） | GetExports、GetMembers、GetSymbolId、GetSymbolTable、IsAssignmentOperator、IsLogicalOrCoalescingAssignmentOperator、IsLogicalOrCoalescingBinaryOperator、IsExternalModule、IsExternalOrCommonJSModule、IsJsonSourceFile、NewDiagnostic、NewFlowReduceLabelData、NewFlowSwitchClauseData、SymbolName | 既存 Symbol / Kind / SourceFile API を維持。Flow constructor の通常 AST 引数は Handle、payload 型は §2 の順序で適応 |

Handle 境界に残す一覧を以下で固定する。「Handle が必要」は既存 API を維持する本基準版での選択であり、NodeRef で実装不可能という意味ではない。単純な child / parent 判定もあるが、ここを Ref 化する変更は別候補として記録する。

| 共通処理 | helper |
| --- | --- |
| 親・祖先・文脈 | FindAncestor、GetContainingClass、GetImmediatelyInvokedFunctionExpression、IsExpressionOfOptionalChainRoot、IsIdentifierName、IsImplicitlyExportedJSDocDeclaration、IsInTopLevelContext、IsModuleAugmentationExternal、IsObjectLiteralMethod、IsObjectLiteralOrClassExpressionMethodOrAccessor、IsOutermostOptionalChain、IsPartOfParameterDeclaration、IsPartOfTypeQuery |
| 修飾子・宣言 | GetCombinedModifierFlags、GetModuleInstanceState、GetNameOfDeclaration、HasDynamicName、HasSyntacticModifier、IsAmbientModule、IsAsyncFunction、IsAutoAccessorPropertyDeclaration、IsBlockOrCatchScoped、IsEnumConst、IsGlobalScopeAugmentation、IsParameterPropertyDeclaration、IsStatic |
| 式・child・list | ExpressionIsAlias、GetAssignmentDeclarationKind、GetElementOrPropertyAccessName、IsAssignmentTarget、IsDestructuringAssignment、IsDottedName、IsEntityNameExpression、IsExpandoInitializer、IsLeftHandSideExpression、IsLogicalExpression、IsLogicalOrCoalescingAssignmentExpression、IsNullishCoalesce、IsOptionalChainRoot、IsPotentiallyExecutableNode、IsPrologueDirective、IsRequireCall、IsSignedNumericLiteral、IsVariableDeclarationInitializedToRequire、SkipParentheses |

`HasSyntacticModifier` は `NodeFlags` の bit 読み取りではない。現行の `Handle.ModifierFlags()` は modifier list を走査するので、FlagsAt へ置換できない。Handle を返す query の結果を主走査に戻す場合は §4 の同一 Store 境界を適用する。公開 Handle query / narrowing helper 自体は NodeRef に変更しない。

**既知の偏り:** 旧 Store HEAD の Ref helper 群に比べ、本基準版は残した45種等で Handle 構築・header 再読・共通 query 内の再取得が増える可能性がある。局所 inline を基本形へ含めても、この差は消えない。`At` は値を作る操作なので、それだけで heap allocation が起きるとは主張しない。上述の分類と各呼出位置・変換箇所を基準版に固定し、追加される呼出数、inline / escape / stack、実測の寄与を記録する。

§5 の child-only projection / list span / 追加引数候補は **helper 境界が同じ対照** と比較する。候補が helper の Ref 化や既存 `HandleOf(store, ref, kind)` による header 再読削減も含むなら、変更を分けた対照（基本版、helper 変更のみ、構文取得変更のみ、両方）を保存して benchstat で比較する。同じ境界の対照が作れない場合、総合的な改善として報告し、projection 等への単独の帰属や HEAD に対する優位性は未判定にする。基準版へ故意に余分な `At` を追加して改善幅を作らない。

### 候補を作る手順

1. 同じ親・名前・list をどの関数が再取得するか列挙する。最初の取得元、後の利用先、writer の介在、有効期間、不要な先読みの有無を示す。
2. 一つの経路で、必要な構文参照だけを既存の関数へ渡す。長い宣言・診断本体を複製せず、単なる Resolved 中継を増やさない。
3. 基本変換版と候補の意味・訪問順を照合する。意味監査用と性能用の binary を分ける。
4. 通常ビルドのコードで再取得の除去を確認し、inline、CALL、引数、frame、spill、escape、コード量の変化も確認する。source 上の getter 数や静的 CALL 数を動的命令数と呼ばない。
5. 同じ入力・Go・GC・保持条件の raw を保存し、`benchstat old.txt new.txt` で比較する。基準は局所 Accessor までを含む逐語 Store 版。pointer 版との比較は別列にする。

局所の機構確認と統合候補の採用判定を分ける。各小変更で全体3%を要求しないが、機構確認だけで本体採用とも呼ばない。統合候補は既存の [調査手順](binder-investigation-plan-20260910.md) の A/A、通常GCでの Binder 全体3%以上の改善、代表入力への展開、parse+bind と GC の確認を満たすこと。測定不能なら追加回数で押し切らず未判定として候補を保持する。

全祖先 stack、巨大な context、可変 Flags の cache、unsafe、`-B`、診断結果の memo は基本形に入れない。getter 減少を引数・stack の費用が相殺する場合、不要な分岐へ取得を前倒しする場合、無効化規則が複雑になる場合は候補を縮小または不採用とする。

### 証拠と記録の形式

測定より先に既存 artifact を調べる。記録には選択 repo、候補 clone、artifact set、`ns/op`、`B/op`、`allocs/op`、hot paths、allocation drivers、診断、次の行動を含める。

| status | 判定 |
| --- | --- |
| `current` | `repo_root`、`tsgolint_git_rev`、`typescript_go_git_rev` がすべて選択 checkout に一致 |
| `stale` | 保存済み artifact の上記 identity が不一致。意図的な旧版対照としての利用は可能 |
| `missing` | 必要な artifact・測定・指標が未取得または存在しない |
| `unsupported` | stage artifact はあるが、`bench.txt` に要求 regex の `Benchmark...` 行がない |

identity の一致だけで dirty source の一致を保証しない。patch、対象 source、生成元、binary、入力の hash も保存・照合する。variant の内容を固定できたことと、選択 checkout に対する `current` は区別する。

ベンチマークは raw を残して benchstat を使う。比較できない指標には理由を書く。symbol synthetic は方向確認に限る。`declareSymbolEx` / `GetSymbolTable` が支配的で `declareModuleMember` が見える場合は、export の多い一意宣言を使う最小 microbenchmark を先に選ぶ。広い hot path が walker/helpers なら、狭い symbol 経路だけが全体の律速とは説明しない。

性能例外の記録は「問題、証拠、再現、仮説、修正案、受入条件」の順とし、原文の対応関数、基本変換との差分、候補を戻す条件を含める。

### 文書作成時点の証拠

以下の artifact の記録は改訂前文書の調査結果を保持したもの。今回のレビュー反映では source / 生成器 / 消費者の静的照合を行い、artifact の再測定や新たな性能評価は行っていない。

- 選択 repo/revision は §1。`tsgolint_git_rev` は `null`。TSGolint は今回使用していない。
- 主な比較候補は `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests` と `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`。同じ repository の他 worktree も `git worktree list` で確認したが、新規 build / 測定には使用していない。
- 確認した artifact set は、前者の `.cursor/skills/verify-tsc/artifacts/20260911-binder-local-accessors`、`20260911-accessor-review-before`、`20260911-accessor-review-after`、および `/private/tmp/bind-hotpath-luna-20260914-225839`。選択 repo/revision に対してすべて `stale`。履歴に書かれた当時の `current` を今回の状態へ読み替えない。
- 現在の貼り付け状態の `ns/op`、`B/op`、`allocs/op`、hot path profile は `missing`。新しい基本変換版もまだなく、現在版との benchstat 比較はできない。新規ベンチマークは実行していない。
- 過去の広い hot path は walker/helpers、狭い改善対象は child/list/header の再解決。割当要因には Symbol/Flow の arena、symbol/flow の列、宣言の Handle 列がある。これは旧版の分析であり、現在版の帰属とは主張しない。
- 診断は、再取得を減らす設計と、その情報を保持・受け渡す費用を分けて評価する必要があること。次の行動は逐語 Store 版の意味を固定し、それを対照として必要な性能候補だけを測ること。

背景資料は [解決済み情報の設計](resolved-access-design-20260910.md)、[局所化の実装記録](local-accessor-cleanup-20260911.md)、[局所化の前後測定](local-accessor-measurement-20260911.md)、[hotpath の測定](luna-bind-hotpath-measurement-results-20260914.md)。過去の before/after の評価は保存された benchstat とその測定条件に従い、数値の目視比較では更新しない。

## 6. 実装順と検証

### 再実装の順序

1. §1 の原文と Store 側の source・入力を固定する。旧 Binder を作業ファイルへ戻さず、比較には独立した snapshot を使う。
2. 入口・NodeRef 状態・Store 出力・公開 API 境界を適応する。`BindOnce` と pool の reset、root の登録・既存 Store ライフサイクルを維持する。
3. 原文の宣言処理、container、Flow 処理を局所 Accessor で移す。手書きの意味処理と生成 walker の呼び出しを同じ内部署名へ揃える。
4. §2 の Flow 5 経路、Locals の2種類の判定、root / file Symbol の同期と復元、手書き visitor を含む走査を検証する。未使用の旧 Binder 専用中継を生成元とともに除く。
5. 下記の意味検証を通した版を互換基準版として固定する。削除計画 Step 1 を独立適用して同じ検証を通し、helper 分類・payload 型・生成物を固定した性能基準版に対して §5 の候補を一つずつ比較する。

### 意味検証

| 対象 | 確認する内容 |
| --- | --- |
| Store 境界 | 空 ref と missing node、kind/shape 拒否、同じ整数 ref を持つ別 Store、foreign child/list/element、Handle 同一性 |
| Accessor | full/projection の共通 field 一致、構文 snapshot の寿命、可変状態の更新後の再読 |
| Flow の代入 | §2 の静的に絞れる4経路と unreachable の消去。未設定列の不要な伸長がないこと、既存 Flow が消えること |
| Locals | export 分岐の kind 集合、class / type / signature の lookupName、未設定・空・値ありの map |
| Symbol | merge/conflict、default/computed export、LocalSymbol/ExportSymbol、宣言順と ValueDeclaration、root / file.Symbol の同一性と JSON 復元 |
| AST の意味情報 | 全 AST node の Flow / EndFlow / ReturnFlow / FallthroughFlow、Flags、NextContainer の対応と nil |
| Flow・訪問順 | functions-first、initializer-before-name、optional/short-circuit、IIFE、loop/try/switch、unreachable |
| 診断・構文 | missing、strict mode、parse error 伝播、JS/CommonJS、JSX、JSDoc の両訪問順、遅延 expando |
| ライフサイクル | 二度目の BindSourceFile、pool 再利用、Store 所属、Flow arena と合成 payload |

実装後のチェックは別々に実行・記録する。

1. `tsc` で `go test ./internal/ast ./internal/binder ./internal/checker ./internal/compiler` を実行する。現存の side map 存在確認テストだけでは、Symbol/Flow の内容や訪問順の等価性は証明できない。
2. candidate の独立したコピーで、生成物を保存してから repo root で `node --experimental-strip-types --no-warnings tools/scripts/tsc/generate.ts` を実行する。使用する Node・依存関係・formatter を揃え、`Herebyfile.mjs` の `generate:ast` と同じ正規入口から encoder / Go AST / TS AST をすべて生成し、再生成前後の全出力ファイル（追加・削除を含む）を比較する。Go generator 単独の実行ではこの検証を代用できない。生成物への手修正で差を消さない。
3. 下記 conformance 手順の `prepare` / `snapshot` / `compare` で、固定した比較元と candidate を別々に検証する。通常の package test の成功を conformance の成功と読み替えない。

意味の正は固定した upstream。upstream と Store 版の比較では、入力・parse option・構文形状を先に照合する。diagnostic の内容と順序、Symbol の関係、Flow の辺と各 AST node への保存対応、node Flags、NextContainer、訪問列を比較し、pointer アドレスや Store の数値 ID の一致は求めない。AST 形状が異なる入力は先に差を分類し、Binder の不一致と決めつけない。旧 Store HEAD との比較は既存挙動の変化を検出する補助対照であり、upstream に反する結果を正解として保存しない。

検証用には [意味監査 driver](../../../../tools/scripts/tsc/binder_semantic_audit.py)、[監査テンプレート](../../../../tools/scripts/tsc/binder_audit_test.go.tmpl)、[conformance 手順](../../../../.codex/skills/verify-conformance/SKILL.md) がある。driver の `run` は pointer / Store 用の観測 overlay を生成し、通常 Binder を実入力で動かして全 AST の保存対応を記録する。`compare` は入力・観測コードの hash と取得完了を検査してから raw を比較し、`selftest` と Store 実行時の変異検査で取りこぼしを確認する。production に監査 API は追加しない。foreign map の検査も観測側から長さを読むだけにする。conformance は固定 commit の実体を検証するため、dirty candidate を HEAD の検証で代用しない。今回の実装では作業木と Go source hash が一致する独立コピーに検証専用 commit を作り、ユーザーのブランチには commit しない。

原文と candidate の完全な比較結果を保存し、成功ケースの内容変更、skip、比較不能も報告する。差分を消すために baseline を自動承認しない。

### 意味監査の記録契約

訪問列とは独立に、bind 前に原文の child 順から作る **全構文ノードの census** を用意する。AST key は source file の識別子と child edge / list index の構造パスとし、kind / span を照合情報に添える。kind:pos:end だけでは同じ span の missing / 合成 node が衝突するため key にしない。bind 中に生じる追加構文があれば作成元と順序を記録し、対応づけられない node を silently skip しない。

各 key について bind 前後の `Flags` と、bind 後の `Symbol` / `LocalSymbol` / `Locals`、`Flow` / `EndFlow` / `ReturnFlow` / `FallthroughFlow`、`NextContainer` を保存する。Flow は生成順の正規化 ID（nil は0）で **既存の Flow グラフの記録と同じ ID 空間** を使う。片側だけで独立に付け直して一致させない。未対応の型では upstream の field 不在を nil と正規化し、Store 側の誤った非 nil 値も検出する。NextContainer は AST key、SourceFile.Symbol は node Symbol と同じ Symbol ID 空間へ写し、root との同一性も保存する。監査用の読み取りは Locals 等を遅延作成する getter を呼ばず、副作用なしに行う。

Flow census 外の pointer や構文 census 外の参照は、共有 singleton など明示的に正規化したもの以外を監査失敗にする。比較する行の完全な raw と digest の両方を保存し、不一致の AST key・field・期待値・実値を表示する。driver の取得完了と比較結果を別々に記録し、テンプレートの存在だけで検証済みとは扱わない。

受入条件には監査自体の負例を含める。`SetEndFlow` の保存先を別の関数 ref に変える、各 Flow 対応を消す、Flags の bit を落とす、NextContainer の辺を変える、file.Symbol の同期または JSON root 復元を省く、という意図的な変異をそれぞれ検出する。正常な Flow グラフと訪問列が同じでも保存対応の比較は失敗しなければならない。Checker の `functionHasImplicitReturn`（`checker.go:17301`）を含む実例と package test / conformance は別の検証として実施する。

### 文書の完成条件

- 原文の式から Store API、能力判定、境界処理への対応を判断できる。
- 実在する API、必要な生成追加、性能候補を区別できる。
- 一括取得の例で、保存 field 順と bind 順の違いを説明できる。
- 性能例外の対照・証拠・採否が §5 で決まり、旧版の数値を現在版の性能として扱わない。
- コード例・リンク・公開署名を固定した source と照合する。文書だけの変更にコンパイルやベンチマークの成功を付記しない。

## 7. 設計判断の記録

### 問題・利用像・構造

安全側に見える一律の能力判定が、不要な生成物と upstream にない検索条件を導入していた。必要なのは使用箇所の意味を保存する変換である。利用像・署名・モジュール境界は §2「呼出し側から決める署名」、保存の対応は同節の表を設計契約とする。

公開面は既存 `BindSourceFile(file)` のまま、Store/root の取得とライフサイクルを内部に隠す（boundary-discipline）。構文配置の知識は生成器に置き、意味条件は対応する Binder 関数に置く。新しい transport 型や Ref query module を増やさず、共通の探索は既存 ast API に閉じる（subtract-before-you-add）。SourceFile の二つの Symbol 保存先は消費者互換性のために必要であり、同期と復元を Binder の既存書き込み地点に集める。監査の正規化はテスト側に閉じる。

### 統合判断（Synthesis decision）

4候補を独立に作り、中央集約の Ref query 層と局所変換の構造差、FlowData の先行適用と後続適用の順序差を比較した。主担当と独立 judge の両方が、訂正後の候補2（局所 NodeRef + 既存生成 leaf）を基礎に選んだ。比較軸は upstream との一致、公開面の小ささと隠せる責任、性能対照の公正さ、AST 出力の観測範囲、検証の実行可能性である。脱落による未提出はない。

| 候補 | 採用した点 | 採らなかった点 |
| --- | --- | --- |
| 1：局所変換 | parse-only 境界で foreign がないことを確認する正の規則、FlowData を別段階にする順序 | `bindChildren(ref, kind)`。意味処理への kind 中継になるため ref-only へ訂正 |
| 2：局所変換と中央 Ref query 層の比較 | 基礎案。既存生成 leaf の署名、query の責任分担、helper 境界を固定した比較 | 原稿の逆順 ParseTreeRef、Locals 集合拡張、無条件の Unreachable flag は source 照合で訂正後に採用 |
| 3：FlowData 先行の協調変更 | 先行適用も可能な代替順序として評価 | `FlowData interface` と新規 constructor は既存公開契約・削除計画の具体的な `*FlowData` に反するため棄却 |
| 4：監査を中心にした境界 | 全 AST の構造パス、nil を含む保存対応、監査の負例 | 新しい `NewBinder` / `Bind` 公開面、unreachable を17文に限る注記は棄却 |

生成済み処理を転送するだけの浅い wrapper、共有 query 意味の二重実装、実行段階ごとに公開操作を分ける設計、kind を恒常的に引き回す中継を red flag として除外した。統合後は §2 の署名を正とし、候補の未訂正スケッチを実装契約にしない。

### 受け入れるトレードオフ

- 既存の Handle query の費用を一部受け入れ、複数の表現で共通アルゴリズムを維持する負担を避ける。性能の偏りは §5 の対照条件で扱う。
- 18個の生成 getter の kind 引数を局所例外として受け入れ、既知 kind を直後に再取得する API 変更を避ける。型だけでは ref/kind の整合を保証できないため、生成コードと呼出元の source 照合で保証する。
- Binder の payload 境界を二度適応する費用を受け入れ、upstream 互換性と FlowData 表現変更の差を分離する。

### 代替構造

**中央 Ref query 層:** `RefQueries{store}.Name(ref)` 等へ構文・共通 query を集約すれば、caller から kind dispatch を隠せる。一方、既存 Handle query への転送だけなら浅く、独立実装なら祖先・foreign・modifier の意味が二重化する。caller は Store と新 query surface の両方を覚える必要がある。現時点では局所変換より責任を深く隠せず、追加表面が大きいので採らない。複数 consumer と実測上の必要が揃えば別設計として再評価する。

**FlowData 先行:** 具体的な `*FlowData` と既存公開入口を維持すれば成立し得るが、貼り付け済み Binder は未適応なので、独立した Store HEAD checkout で先行変更を検証して取り込む必要がある。本計画では現行周辺契約を固定して意味を比較できる順序を優先し、§2 の baseline-first を採る。

### 未確認事項と次の実装

**`bind(node ast.NodeRef, kind ast.Kind)` の検討:** 基本形は §2 の `bind(node ast.NodeRef)` とするが、kind 引数の追加は今後の性能候補として残す。functions-first の走査など、呼出元が同じ node の kind を既に取得している経路では、bind 内の再読を省ける。一方、親の Accessor が子の NodeRef だけを返す経路では、`KindAt(child)` を呼出元へ移すだけでは取得回数は減らない。後続の Accessor / Handle query 内の再読も、bind の署名変更だけでは除去されない。

候補では node 自身の正しい kind を渡し、bind の処理本体を一つに保つ。kind を渡すためだけの中継関数や Binder field は増やさない。§2 の基本形の規約は、この候補の比較を禁止するものではない。実装後に再読が実際に減る経路を確認し、同じ helper 境界・payload 型・入力条件の二版で意味監査と、追加引数によるレジスタ・stack / spill への影響を確認する。raw benchmark を保存し、§5 に従って `benchstat old.txt new.txt` で採否を決める。速度改善は現時点では未測定である。

残した Handle 境界の費用はどの程度か、foreign 不変条件は全 parser fixture で成立するか、拡張した監査が意図的な保存先の取り違えを検出できるかは、実装後の測定・テストで解決する。設計上の追加承認待ちはない。基盤移行と監査 driver を実装し、後続の性能検討はその検証結果を起点とする。

設計時には schema の基底・kind 範囲、105種の helper 呼出名、Store の保存 API、Checker の読取り、正規生成入口を静的照合した。その後の基盤実装・実行結果は [移行結果](foundation-migration-results-20260915.md) に記録する。性能測定は今回の範囲に含めず、速度改善は未確認のままとする。

## 実装で確認した周辺 AST 契約差

固定 upstream の ModuleDeclaration.Attributes は、現在の Store schema、Factory、parser には存在しない。今回の表現移行は現行 parser が生成する木を対象とし、getDeclarationName の attributes 付き pattern 固有名と bindModuleDeclaration の attributes 専用診断分岐を含めない。通常の module 名、pattern 検査、Symbol 登録は維持する。これは getter で nil を偽装する変換ではなく、表現可能な構文の制約である。ambient module import attributes の導入は schema・parser・checker を合わせた別変更が必要であり、固定 upstream の全構文同等性は今回の達成条件にしない。監査では入力木の差も別途記録する。

### Body child と BodyBase の違い

ClassStaticBlockDeclaration は構造上 Body child を持つが、固定 upstream の BodyBase を埋め込まない。`bodyRefGenerated` が非0でも `BodyData() != nil` と同値ではない。bindContainer の EndFlow / HasImplicitReturn 保存では static block を除外する。ReturnFlow は元の Constructor / static block guard のまま保存する。監査の flow fixture でこの差を検出し、単体テストにも負の条件を置く。
