# Luna 向け Binder 基盤移行指示書

2026-09-15。対象モデルは `gpt-5.6-luna`、reasoning effort は `high`。この文書は実装指示であり、移行済みの報告ではない。

## 1. 依頼と完成範囲

[変換設計](upstream-to-store-translation.md)に従い、upstream Binder のシグネチャ・データ構造・保存先・生成 walker・API 境界を Store 版へ移行する。[全シグネチャ台帳](luna-foundation-signatures-20260915.md)の167関数を漏れなく扱う。英語で考え、日本語で報告する。

「基盤」には、型変更に伴って必要になる body のアクセス変換と caller の更新を含める。宣言だけを NodeRef にして、body が `node.Kind` / `AsX` を使う状態を完成とはしない。関数の大半に変更が及ぶが、宣言・診断・Flow のアルゴリズム、分岐条件、保存復帰、訪問順は固定 upstream のままにする。

次の二つを区別して報告する。

- 構造移行済み。型・状態・生成器・caller の契約が揃い、通常ビルドと package test が通る。
- 互換性検証済み。全 AST の保存対応を含む意味監査と conformance 比較で、変更と未確認範囲を説明できる。

前者だけで正確性の検証が完了したとしない。原則として両方を終える。環境障害や未解決差分が残る場合は、実際に完了した範囲と阻害条件を残し、「基盤を全て移行済み」と報告しない。

### 今回行わないこと

- `bind(node, kind)` の性能実験。これは設計の「未確認事項と次の実装」に残っている候補である。今回の基本入口は `bind(node ast.NodeRef) bool`。
- `FlowNode.Data *Node` の `*FlowData` 化。削除計画 Step 1 は互換基準版の後の別変更。
- Symbol.Declarations の NodeRef 化、GlobalRef 化、公開 Binder API の再設計。
- 新しい `bindNode`、`bindRef → bindN → bindKind`、Ref query 層、取得済み情報を運ぶ巨大 context。
- 最適化・性能測定、無関係な resolver 差分の取り込み、期待 baseline の承認。

## 2. 起点を固定する

| 対象 | 固定値 |
| --- | --- |
| 作業 repo | `/Volumes/SanDisk1TB/worktree/binder-rewrite` |
| 意味の正 | `879f9867ac455404e75759dd1739281cf6aa7f85` |
| 周辺 Store API の基準 | `85506e8b7d51f2b05f84f9ba1a8c3309b2f30ce4` |
| 貼り付け済み binder blob | `e20bbaea3ce0bdc87bbdff916c0155ae9430487d` |

着手前に status、HEAD、binder blob、設計文書と台帳の内容を確認する。作業開始時点で `binder.go` は変更済み、設計文書は未追跡である。ユーザーの変更なので HEAD の旧 Store Binder で上書きしない。比較元は `git show <固定commit>:tsc/internal/binder/binder.go` から別の場所へ保存する。起点が進んでいたら差分を読み、済んだ変更を巻き戻さず台帳を更新する。

現在の貼り付け状態は pointer Binder と Store API が混在しており、ビルド可能な baseline ではない。開始時の型エラーは未移行の証拠として保存する。意味の対照には固定 upstream の独立 snapshot、既存挙動の補助対照には固定 Store HEAD を使う。HEAD の旧 Binder の内部構造を移植元にしない。

作業用ログ、source hash、段階別差分、テスト結果は別の artifact ディレクトリに保存する。実装前に固定 upstream 用の観測方法と入力を準備し、構造を変更した後で正解を候補から作らない。

## 3. データ構造・ライフサイクル

| 対象 | 変更後の契約 |
| --- | --- |
| `Binder.file` | `*ast.SourceFile` のまま |
| `Binder.store` | `*ast.Store` を追加。`store, root := file.ParseTreeRef()` の組から設定 |
| `container` / `thisContainer` / `blockScopeContainer` / `lastContainer` | すべて `ast.NodeRef`。対応する kind field は追加しない |
| `ExpandoAssignmentInfo` の node / container / blockScopeContainer | すべて `ast.NodeRef`。同じ Store 内の遅延参照 |
| `singleDeclarationsArena` | `core.Arena[ast.Handle]` |
| `symbolArena` / `flowListArena` | 現行の Symbol / FlowList arena を維持 |
| `flowNodeArena` | Binder から削除。全 FlowNode は `b.store.NewFlow(flags)` で作る |
| `currentFlow`、Flow target、`ActiveLabel` | `*ast.FlowNode` / `*ast.FlowLabel` を維持。Flow の ref 化はしない |
| `bindFunc` | 生成 walker が直接 `b.bind(child)` を呼ぶため削除。`ForEachChild(b.bindFunc)` と pool の closure 保存も除く |
| その他の flags / count / set / label state | 型と意味を維持。NodeRef の保存復帰は元の位置で行う |

`BindSourceFile(file *ast.SourceFile)` の公開入口と `IsBound` / `BindOnce` の動作を維持する。内部で file / Store を設定し、Store HEAD の `ast.RegisterFile(file)` による登録を維持する。Store の設定前に unreachable Flow を生成しない。root を bind してから deferred expando、最後に SymbolCount を更新する。

`PrepareBindTables()` は登録や意味上の必須初期化ではなく、Symbol / Flow 列の事前確保である。今回の基準版では周辺 Store HEAD の確保条件を維持する選択として、登録後・最初の Flow 生成前に呼ぶ。設計 §5 に従って採用した事前確保条件として記録し、後続の性能比較の途中で出し入れしない。

pool の New は Binder を作るだけにし、put 時は `*b = Binder{}` で状態をクリアする。既に生成した Symbol / Flow の参照先を reset の一環で消さない。Binder が Store を独自に破棄・unregister する処理は追加しない。root は入口の局所変数でよく、必要箇所で file の既存 ParseTreeRef から得る。新しい root/kind cache は不要。

## 4. シグネチャと境界を同時に移す

台帳の目標署名を正とする。以下が一括置換の例外である。

### 通常の内部参照

Binder method の構文 node 引数・戻り値は NodeRef。`getInferTypeContainer` の戻り値、`doWithConditionalBranches` の callback に含まれる `value` も対象にする。inline callback が `ast.FindAncestor` 等の既存 Handle API へ渡る場合は、その callback は `func(ast.Handle) bool` にする。すべての callback を同じ型に置換しない。

`bindEach` / `bindNodeList` / `bindModifiers` / `bindEachStatementFunctionsFirst` は `ast.ListRef` を受ける。ListLen / ListElem で走査し、元の二回走査を維持する。`clause.Statements.Nodes`、`node.Arguments()`、`call.Arguments.Nodes` 等の caller は所有元の ListRef に変換する。ref slice や Handle slice を中間生成しない。既存の bindNodeList / bindModifiers の役割は維持してよいが、同じ形の移行用 wrapper を追加しない。

### Handle を残す場所

```go
func GetContainerFlags(node ast.Handle) ContainerFlags
func SetValueDeclaration(symbol *ast.Symbol, node ast.Handle)
func FindUseStrictPrologue(sourceFile *ast.SourceFile, statements []ast.Handle) ast.Handle
func (b *Binder) newSingleDeclaration(declaration ast.Handle) []ast.Handle
```

既存 free query は台帳のとおり Handle に適応する。特に `isNarrowableReference`、`hasNarrowableArgument` などは foreign child を解決するため、内部再帰も Handle のまま。`isNarrowingBinaryExpression(*ast.BinaryExpression)` は `isNarrowingBinaryExpression(ast.Handle)` にし、BinaryExpression の型付き Handle getter を使う。private だからという理由で NodeRef に落とさない。

`isFunctionSymbol` は引数が Symbol のままでも、ValueDeclaration が Handle に変わっているため body の適応が必要。Handle の空値は `ast.Handle{}` / `IsNil()`、NodeRef の空値は `ast.NoNodeRef`。同じ整数 ref でも別 Store なら同じ node ではない。

内部から既存 Handle query を呼ぶ位置で `b.store.At(ref)` を作る。設計 §5 の105種の分類に従い、kind / Flags / Loc の局所判定は直接変換する。Handle を返す query の結果を NodeRef の状態や戻り値へ入れる場合、空値を処理し、同じ Store であることを確認してから Ref を取る。変換だけの共通中継は作らない。

### Flow の生成・保存

```go
func (b *Binder) newFlowNode(flags ast.FlowFlags) *ast.FlowNode
func (b *Binder) newFlowNodeEx(flags ast.FlowFlags, node ast.NodeRef, antecedent *ast.FlowNode) *ast.FlowNode
func (b *Binder) newFlowData(flags ast.FlowFlags, data *ast.Node, antecedent *ast.FlowNode) *ast.FlowNode
```

newFlowNode は Store.NewFlow を使う。newFlowNodeEx は通常 AST を `FlowNode.Node = b.store.At(node)` に保存し、newFlowData は `FlowNode.Data = data` に保存する。両者の Antecedent 設定は原文どおりにする。reduce label と switch clause の constructor は newFlowData を使い、switchStatement の引数だけ Handle に上げる。`flowStart.Node = node` のような constructor 外の代入も検索して適応する。許す `*ast.Node` は合成 payload 境界であり、通常 AST の抜け道ではない。

原文の `setFlowNode` の3 callerは元の guard 内で Store.SetFlow に置換し、free setter は削除する。文の17 kind 範囲の SetFlow も直接代入する。unreachable の任意 node では既存 Flow が非 nil のときだけ nil を書く。IsPotentiallyExecutableNode の Flags 更新条件は維持する。

`setReturnFlowNode` は、唯一の caller が Constructor / ClassStaticBlockDeclaration に絞る guard を保ったまま Store.SetReturnFlow に置換して削除する。新しい caller が増えていた場合は先に静的な対象 kind を再確認する。EndFlow は関数 node に付くので body ref に書かない。FallthroughFlow は case の Handle setter を使う。Flow / EndFlow / ReturnFlow / FallthroughFlow を混同しない。

### Symbol・Locals・Flags

- addDeclarationToSymbol は Store.SetSymbol と Handle の宣言列を更新する。既存の宣言順、AppendIfUnique、ValueDeclaration の選択規則を維持する。
- SourceFile の Symbol 代入では parse root と file.Symbol を同時に更新する。JSON の一時 property 宣言後は、一つの originalSymbol を両方へ復元する。旧 Store HEAD の field-only 復元をコピーしない。
- `declareModuleMember` の判定は、到達する container が ModuleDeclaration / SourceFile であることを確認した上で既存 IsLocalsContainerKind を使う。固定 upstream の IsLocalsContainer 自体は LocalsContainerData の有無であり、全 kind 上で同一の集合ではない。`lookupName` は Store.Locals の値を読む。class / type / signature を後者から除外しない。
- 新規 `getLocals(ref) ast.SymbolTable` は nil ref なら nil、既存 map があればそれを返し、なければ作って SetLocals。読み取りだけの lookupName / 監査では呼ばない。
- NextContainer の writer と保存先を元どおりにする。Flags は元の更新位置で現在値を読み、同じ bit 演算で SetFlagsAt する。再帰前に読んだ Flags を再帰後の更新に流用しない。

## 5. 生成器と child アクセス

手書き Binder に slot 番号は書かない。型付き AccessX の名前付き field を同じ関数内で使う。原文の処理順を field 配列順へ変えない。scalar は既存の型付き getter を使用し、AccessX に存在しない field を作らない。

変更する生成元は `tools/scripts/tsc/generate-go-ast.ts`。関係する出力文字列をすべて検索して更新する。

| 現在の生成 caller | 目標 |
| --- | --- |
| `b.bindChildRef(child, kind)` | `b.bind(child)`。現在の kind は親の kind である |
| `b.bindListRef(list, kind)` | `b.bindEach(list)` |
| `b.bindEachStatementFunctionsFirstRef(list, kind)` | `b.bindEachStatementFunctionsFirst(list)` |
| `b.bindChildrenOf(ref, kind)` の fallback | 削除。child なしの kind は明示的に処理し、未対応の child-bearing kind は生成時に検出 |
| 既存18個の生成 getter 系関数 | 使用するものの `(ref, kind)` を維持。未使用の Binder 専用 helper は生成元とともに削除 |

`bindEachChild(ref)` は `forEachBindChildGenerated(ref, b.store.KindAt(ref))` で既存生成 dispatch を呼ぶ。生成側が子を bind する際は子の ref だけを渡す。別途 parentKind 引数は追加しない。生成 dispatch の kind 例外を意味処理の method へ広げない。

生成 walker の意味監査は必須。JSDocParameterOrPropertyTag の IsNameFirst に応じた順序を固定 upstream の ForEachChild と照合する。現在の generateBinderWalk は schema 順のままなので、呼出名だけを変更して完成としない。SourceFile / Block / ModuleBlock の functions-first と SourceFile EOF の位置、Parameter / BindingElement の initializer と name の順序も確認する。生成 walker は汎用 child 訪問だけを担当し、専用 Flow 処理を吸収しない。

parser は externalChild / externalList を作らない。debug / ast 内テストで両 map が空という入口条件を確認し、その下で child 0 は参照なしとして扱う。実在する missing node は非0。foreign を含む任意の合成木を主走査で bind できる機能は追加しない。既存の foreign narrowing query のテストは維持する。

正規入口は repo root の次のコマンドである。

```sh
node --experimental-strip-types --no-warnings tools/scripts/tsc/generate.ts
```

Node・依存関係・formatter を揃え、独立した candidate コピーで全生成物の再現性を確認する。encoder / Go AST / TS AST の追加・削除も比較する。生成元を変えず生成物だけを直さない。調査タグの付いた生成 walker 呼出やベンチマーク補助コードも caller 検索に含め、不要な実験自体は起動しない。

## 6. 実装の進め方

1. 固定 snapshot と全167関数の台帳を用意し、各行に着手・完了・未解決の状態を記録する。body 内の型付き locals と callback の境界も併記する。
2. 基準 upstream に対する入力と観測方法を固定する。監査は設計 §6 の全 AST 対応を記録できる形へ拡張する。候補の出力を期待値にしない。
3. 型・state・pool・入口・Flow arena・Symbol arena を移す。次に内部シグネチャと公開 Handle 境界を移し、対応 caller と body の構文アクセスを更新する。
4. 生成 walker と ListRef 走査を同じ内部契約へ揃える。意味処理は upstream の関数単位で追い、diagnostic・分岐・訪問順の差がアクセス変換以外にないことを記録する。
5. 全 caller と型エラーを解消する。完成していない関数を panic / TODO / 空 return に置換したり、build tag で除外したり、type alias で pointer AST を復活させたりしない。
6. 下記の構造検査・package test・意味比較を実行し、失敗の原因を解決する。互換基準版が完成するまで性能候補と FlowData Step 1 は適用しない。

型移行中の一時的なコンパイルエラーはログに分類してよい。最終成果物には残さない。無関係な checker / ast の型を旧 pointer Binder に合わせて戻してエラーを消さない。

## 7. 受入条件

### 構造とビルド

- 台帳167関数に未分類・未移行がない。削除2関数・追加2関数・生成 helper の増減を説明できる。名前が同じでも caller / body が旧表現なら未完了。
- 通常の AST state / 内部引数 / 遅延参照が NodeRef、構文 list が ListRef。private query / 公開 API / Symbol 保存の Handle 例外は台帳と一致する。
- Binder 内の pointer AST は合成 Flow payload 境界に限定される。NodeList / ModifierList / `[]*ast.Node`、bindFunc、flowNodeArena、親 kind 中継、旧 generated caller が残っていない。
- 全生成出力が正規入口で再現する。実際の git 差分と未追跡ファイルも含めて確認する。
- `tsc` ディレクトリで `go test ./internal/ast ./internal/binder ./internal/checker ./internal/compiler` を実行する。現存テストを無効化・弱体化しない。

### 意味の証拠

設計 §6 の監査を実行する。既存 `binder_audit_test.go.tmpl` の署名変更だけでは足りない。全 AST の構造パスを用い、Flow / EndFlow / ReturnFlow / FallthroughFlow、Flags、NextContainer、Symbol / LocalSymbol / Locals、file.Symbol を記録する。Flow の辺と AST 側の attachment は同じ正規化 ID を使い、nil も比較する。監査で読むために Locals を作成しない。

最低限、以下を観測できる入力を含める。

- class の型パラメータ、FunctionType / ConstructorType / signature の Locals 検索。
- JSON の module / property Symbol と root / file の復元、通常の module / CommonJS export。
- unreachable 前後の Flow 消去と Flags、関数ごとの EndFlow、constructor / static block の ReturnFlow、switch の FallthroughFlow。
- 二回 bind と pool の別ファイル再利用、foreign child を含む narrowing query。
- functions-first、Parameter / BindingElement、JSDoc の両順序、optional chain / IIFE / loop / try / switch、deferred expando。

EndFlow の保存先 ref の取り違え、各 attachment の欠落、Flags / NextContainer / file.Symbol の変異を監査が検出する負例も確認する。`TestBindStoreSideMaps` が通るだけでは保存先の正しさを証明しない。

conformance は [verify-conformance](../../../../.codex/skills/verify-conformance/SKILL.md) の prepare / snapshot / compare を使い、固定 upstream、candidate、補助対照の Store HEAD を区別する。helper は commit 固定なので、dirty candidate を HEAD の検証で代用しない。必要なら独立コピーに candidate の変更を取り込んで検証用 commit を作り、その source と作業ツリーの変更ファイルの hash が一致することを確認する。ユーザーのブランチへの自動 commit / push は行わない。

完全な raw 比較、成功出力の内容変更、skip、比較不能も保存する。既知の JSON root 復元差は固定 upstream の契約に照らして扱う。差分をなくすために旧 Store HEAD の挙動や期待 baseline を正解へ書き換えない。

## 8. 判断が必要になった場合

通常の caller 修正・局所アクセス変換・型エラー解消は自分で進める。次の場合は無理に共通化せず、対応する upstream の式、Store API、consumer、候補の差分、未確定の判断を具体的に残す。

- foreign Handle を同一 Store の内部 state に戻す必要があるが、同一 Store の前提を証明できない。
- upstream の条件に対応する Store API がなく、API の意味変更を要する。
- 意味監査が不一致で、型変換の修正だけでは説明できない。
- 複数の箇所で同じ新規中継や cache が必要に見え、今回の設計境界を越えそうになる。

これらを未検証の例外で押し通さない。完了した独立部分と検証結果は残す。モデル名を正確性の根拠にせず、source の対応と観測結果を判断材料にする。

## 9. 完了報告

変更ファイル、型と署名の完了数、残した Handle / pointer payload 境界、生成物の再現結果、package test、意味監査、conformance の比較を報告する。未実行・環境障害・比較不能は別に示す。ベンチマークは今回の受入条件に含めず、速度改善は主張しない。

この指示書作成時点では、実装・生成器実行・テスト・ベンチマークは行っていない。固定 source と周辺 API の静的照合に基づく指示である。

## 実装時に確定した注意点

- ModuleDeclaration.Attributes は現行 Store schema / parser に存在しない。attributes 専用の名前生成と診断は今回の対象木には到達せず、構文対応は parser / checker を含む別変更にする。通常の module / pattern 処理は維持する。
- ClassStaticBlockDeclaration の Body child は upstream BodyBase の能力を意味しない。bindContainer の EndFlow / HasImplicitReturn から static block を除外し、ReturnFlow は保存する。
- scanner.DeclarationNameToString は元ソースの綴りと missing を扱うため TextAt と置換しない。キーワード分類など原文の node.Text() は TextAt のままにし、診断表示名と分ける。
- 意味監査の実行入口は `tools/scripts/tsc/binder_semantic_audit.py`。観測用 overlay を使い、production code に監査 API を追加しない。run / compare / selftest のコマンドと raw artifact を保持する。
