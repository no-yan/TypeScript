# Store binder (TODO 7c) 実装指示書

作成日: 2026-09-22。対象は実装を担当するエージェント。設計の根拠は [store-binder-design-20260922.md](store-binder-design-20260922.md) (以下「7c 設計」) と [store-ast-design-20260922.md](store-ast-design-20260922.md) (以下「設計文書」)。前段は `tsc/internal/storeparser/` (TODO 7b、[store-parser-verification-report-20260922.md](store-parser-verification-report-20260922.md))。検証の手順と判定は [store-binder-verification-instructions-20260922.md](store-binder-verification-instructions-20260922.md)。

設計判断はこの文書と 7c 設計で済ませてある。書かれていない判断が必要になったら、実装を進めずに報告する。

## 0. 作るものの性格

現行の binder (`tsc/internal/binder/binder.go`、2,802 行) を **機械的に移植**して、7b の `store.File` を bind する binder を作る。目的は 3 つ:

1. bind の出力 (symbol、locals、flow、flags、file の field) を 7c 設計 §2.3 の置き場 (32B header + 予約 slot + `Bound`) に書けることを示し、Pointer と同じ結果になることを corpus で確かめる。
2. **Bind ゲート** (設計文書 §1 の 2): checker.ts の cycles/node が Pointer の 1.1 倍以内。前回の Store は逐語訳で +21% だった (7c 設計 §1)。
3. bind 後の retained heap と GC を Pointer と比べ、「Store は noscan」の賭けが `Bound` でどれだけ崩れるかを数字にする。

| | Pointer binder | 7c |
| --- | --- | --- |
| 入力 | `*ast.SourceFile` | `*store.File` (7b + §4 の field) |
| 訪問ノードの通貨 | `*ast.Node` | `store.Node{s,h}` (7c 設計 §2.2) |
| node → symbol / flow | `DeclarationData().Symbol`、`FlowNodeData().FlowNode` | `NodeHeader.symbol` / `.flow` (32B header、§3.1) |
| Locals、LocalSymbol、EndFlow、ReturnFlow、Fallthrough | node の field | kind ごとの予約 slot (`extra`、generator が出す。§3.3) |
| FlowNode / FlowList | pointer 連結、arena | index 連結の slab (`Bound.flows` / `flowLists`、§3.2) |
| Symbol | `ast.Symbol` (`Declarations []*Node`) | `store.Symbol` (`Declarations []Ref`、§3.2) |
| file の field | `ast.SourceFile` | `store.File.Bound` (§3.4) |
| 走査 | `ForEachChild` | 生成 `ForEachChild`、同じ訪問順 |

作らないもの (7d 以降): nameresolver / referenceresolver、JSDoc reparse ノードの bind (JS の `@typedef` 等)、診断への file の付与、checker との結合、program への組み込み。

## 1. ガード

1. package は `tsc/internal/storebinder`、package 名 `storebinder`。import してよいのは `internal/ast` (Kind、flags、`SymbolFlags`、`Diagnostic` 等の値型と kind だけを見る述語)、`internal/ast/store`、`internal/scanner`、`internal/core`、`internal/collections`、`internal/debug`、`internal/diagnostics`、`internal/tspath`。**`internal/binder` と `internal/parser` は import しない** (テストからは比較のために import してよい)。
2. **`internal/binder`、`internal/parser`、`internal/scanner`、`internal/ast` (store 以外) を変更しない。** scanner や ast に足したい API が出たら、足さずに報告する。
3. `internal/ast/store` に足してよいのは §3 に列挙したものだけ。7a の accessor、`ForEachChild`、typed view の形は変えない。generator の変更は §3.3 に列挙した出力だけ。
4. `internal/storeparser` の変更は §4 に列挙したものだけ (references の移植、indicator、reparse の起動、`File` の field)。
5. 移植は**機械的**に行う。関数名、関数の順序、コメント、ローカル変数名を Pointer binder と同じに保つ (検証で `diff` を取る)。挙動を「改善」しない。Pointer binder のバグに気づいたら直さず報告する。逐語でない書き換えは §5.6 の分類 (f) として、**Pointer 版と Store 版の header の読み回数を並べて**列挙する。Pointer にも効く書き換えは入れず、別提案として報告する。
6. 逐語移植が通った後に §5.7 の (g) のまとめ pass を 1 回だけ行い、その diff を別に取る。
7. `doc.go` の package comment (英語) に: 7c 設計へのパス、`internal/binder` の機械的な移植であること、出力の置き場 (§3)、7c の範囲外 (JSDoc reparse ノード、nameresolver、診断の file)、移植規則 (§5) の要点。

## 2. ファイル

```
tsc/internal/ast/store/
  store.go                 §3.1 header 32B、Node の setter、Seal、Ref 周り
  bound.go                 §3.2 Bound、FlowRef / FlowNode / FlowList / FlowData
  symbol.go                §3.2 Symbol、SymbolId、SymbolTable、PatternAmbientModule、GetExports 等 (internal/ast/symbol.go の移植)
  file.go                  §3.4 File の field
  position.go              §3.5 GetNodeAtPosition
  utilities.go             §5.5 binder が呼ぶ ast の述語の Store 版 (手書き)
  storetest/equivalence.go §3.6 Pairs
  *_generated.go           §3.3 (generator の出力)
tsc/internal/storeparser/
  references.go            §4 internal/parser/references.go の移植 + indicator (internal/ast/parseoptions.go の該当部)
  parser.go                §4 reparse の起動、finishSourceFile の field
tsc/internal/storebinder/
  doc.go
  binder.go                internal/binder/binder.go の移植
  binder_test.go           §6
  bench_test.go            §7
tools/scripts/tsc/generate-go-store.ts   §3.3
```

## 3. `store` package への追加

### 3.1 header 32B と書き込み API (7c 設計 §2.3)

```go
type NodeHeader struct {            // 32B、noscan
    kind          ast.Kind
    modifierFlags uint16
    flags         ast.NodeFlags
    pos, end      int32
    parent        NodeRef
    data          uint32
    flow          FlowRef             // bind が書く。0 = nil
    symbol        SymbolId            // bind が書く。0 = nil
}

func (n Node) FlowNode() FlowRef            // h.flow
func (n Node) Symbol() SymbolId             // h.symbol
func (n Node) AddFlags(f ast.NodeFlags)     // h.flags |= f
func (n Node) ClearFlags(f ast.NodeFlags)   // h.flags &^= f
func (n Node) SetFlow(f FlowRef)
func (n Node) SetSymbol(id SymbolId)
func (n Node) FileRef() Ref                 // Ref{s.file, n.Ref()}。Declarations 等、heap に保存する形
func (r Ref) File() uint32
func (r Ref) Id() NodeRef
func (s *Store) Node(ref NodeRef) Node      // s.node の公開。list の要素と Ref の解決に binder が使う
func (s *Store) Seal()                      // 以後の書き込みは storeChecks build で panic
```

- `node()` の `&s.nodes[ref]`、`Ref()` の `unsafe.Sizeof(NodeHeader{})` はそのまま (32 で割る形になる。乗算が shift になることは検証が見る)。
- setter はすべて `if storeChecks && s.sealed { panic }` を先頭に持つ。`storeChecks` が false の build では `sealed` を読まない。`sealed` は `Store` の bool field (noscan のまま)。
- `Builder.SetFlags` / `SetLoc` は削る (7b で未使用)。
- `Finish` は `flow` / `symbol` を 0 のまま copy する。`convert` (Pointer → Store) も 0。

### 3.2 `Bound`、flow、Symbol (7c 設計 §2.1、§2.3、§2.4)

```go
// bound.go
type FlowRef uint32                          // Bound.flows の index。0 = nil
type FlowNode struct {                       // 16B、noscan
    Flags       ast.FlowFlags
    Node        NodeRef                      // 対応する AST ノード。SwitchClause / ReduceLabel では Bound.flowData の index
    Antecedent  FlowRef                      // Label 以外
    Antecedents FlowListRef                  // Label の antecedent 列
}
type FlowListRef uint32                      // Bound.flowLists の index。0 = nil
type FlowList struct{ Flow FlowRef; Next FlowListRef }   // 8B
type FlowData struct{ A, B, C uint32 }       // SwitchClause: (switchStatement, clauseStart, clauseEnd)。ReduceLabel: (target FlowRef, antecedents FlowListRef, 0)

type Bound struct {
    SymbolCount          int
    PatternAmbientModules []PatternAmbientModule
    GlobalExports        SymbolTable
    CommonJSModuleIndicator NodeRef
    diagnostics          []*ast.Diagnostic   // bind diagnostics。file は nil
    symbols              []*Symbol           // SymbolId → *Symbol。[0] は nil
    flows                []FlowNode          // [0] は番兵
    flowLists            []FlowList          // [0] は番兵
    flowData             []FlowData          // [0] は番兵
    locals               []SymbolTable       // 予約 slot の index → table。[0] は nil
    containers           []NodeRef           // NextContainer の連鎖を宣言順の列に
}
// 読み
func (b *Bound) Flow(r FlowRef) *FlowNode           // &b.flows[r]。bind 中は append で動くので保持しない
func (b *Bound) FlowList(r FlowListRef) *FlowList
func (b *Bound) FlowData(i uint32) *FlowData
func (b *Bound) SymbolOf(id SymbolId) *Symbol       // b.symbols[id]
func (b *Bound) Locals(i uint32) SymbolTable
func (b *Bound) Containers() []NodeRef
func (b *Bound) BindDiagnostics() []*ast.Diagnostic
// 書き (storebinder が呼ぶ。field は非公開のまま)
func (b *Bound) NewFlow(flags ast.FlowFlags, node NodeRef, antecedent FlowRef) FlowRef
func (b *Bound) NewFlowList(flow FlowRef, next FlowListRef) FlowListRef
func (b *Bound) NewFlowData(a, bb, c uint32) uint32
func (b *Bound) AddSymbol(s *Symbol) SymbolId       // s.id を書いて返す
func (b *Bound) NewLocals() uint32                  // make(SymbolTable) を append、index を返す
func (b *Bound) AddContainer(ref NodeRef)
func (b *Bound) AddDiagnostic(d *ast.Diagnostic)
```

file の symbol は `Bound` に置かない。Pointer の `file.Symbol` は root ノードの `DeclarationBase.Symbol` そのもの (`SourceFile` が `DeclarationBase` を埋め込む) なので、Store では root header の `symbol` であり、`File.Symbol()` がそれを読む (§3.4)。

```go
// symbol.go: internal/ast/symbol.go を移植し、2 field の型を変える
type SymbolId uint32
type Symbol struct {
    Flags            ast.SymbolFlags
    CheckFlags       ast.CheckFlags
    Name             string
    Declarations     []Ref                  // []*Node → []Ref
    ValueDeclaration Ref                    // *Node → Ref (zero = nil)
    Members          SymbolTable
    Exports          SymbolTable
    id               SymbolId               // atomic.Uint64 の遅延採番 → 生成時に slab の index
    Parent           *Symbol
    ExportSymbol     *Symbol
}
type SymbolTable map[string]*Symbol
type PatternAmbientModule struct{ Pattern core.Pattern; Symbol *Symbol }
func (s *Symbol) Id() SymbolId
func GetSymbolTable(data *SymbolTable) SymbolTable   // ast と同名
func GetExports(symbol *Symbol) SymbolTable
func GetMembers(symbol *Symbol) SymbolTable
func GetLocals(...)                                   // §5.2 (予約 slot 経由)
```

- `Symbol` の大きさは `ast.Symbol` と同じ 96B であること (検証が `unsafe.Sizeof` で見る)。
- `IsExternalModule`、`IsStatic` (`ValueDeclaration` の `ModifierFlags` を Store で読む)、`CombinedLocalAndExportSymbolFlags`、`SymbolName`、`Escape*` は同名で移植する。`IsStatic` / `SymbolName` は node を読むので `(*Store)` か `Node` を引数に取る形にし、(f) に入れる。
- `InternalSymbolName*` 定数は `ast` のものをそのまま使う (移植しない)。

### 3.3 generator: 予約 slot と役割 accessor

`generate-go-store.ts` の `classify` に slot class `bound` (1 word、uint32) を足す。対象は名前で決める:

| field (ast.json) | 定義 | class |
| --- | --- | --- |
| `ExportableBase.LocalSymbol` (`*Symbol`) | 13 定義 | `bound` (SymbolId) |
| `LocalsContainerBase.Locals` (`SymbolTable`) | 直接 13 + `FunctionLikeBase` 経由 | `bound` (`Bound.locals` の index) |
| `BodyBase.EndFlowNode` (`*FlowNode`) | 2 定義 + 合成 | `bound` (FlowRef) |
| `ReturnFlowNode` (`*FlowNode`) | FunctionDeclaration、ConstructorDeclaration、ClassStaticBlockDeclaration、FunctionExpression | `bound` (FlowRef) |
| `CaseOrDefaultClause.FallthroughFlowNode` (`*FlowNode`) | 1 定義 | `bound` (FlowRef) |
| `DeclarationBase.Symbol`、`FlowNodeBase.FlowNode` | | **skip** (header にある) |
| `LocalsContainerBase.NextContainer` | | **skip** (`Bound.containers`) |
| `CompositeBase.facts` | | skip のまま |

- `bound` slot は宣言順の末尾に置く (子と list の並びと `ForEachChild` の契約を変えない。`listMask` の bit は立てない)。constructor は 0 で埋める (引数に取らない)。
- 生成する API: 定義ごとの getter (`func (v FunctionDeclaration) LocalsSlot() uint32` 等) と setter (`SetLocalsSlot(uint32)`。§3.1 と同じ `sealed` 検査)、役割 accessor `func (n Node) LocalsSlot() (uint32, bool)` / `LocalSymbol() SymbolId` / `EndFlowNode() FlowRef` / `ReturnFlowNode() FlowRef` / `FallthroughFlowNode() FlowRef` を `nameSlot` と同じ `[512]uint8` の表 (番兵 0xFF で不在) で。**不在 kind の経路は表引き + 比較 1 回で戻る** (設計文書 §2.6)。名前の使い分け: `LocalsSlot` (typed view と役割 accessor、`extra` の値 = `Bound.locals` の index)、`Bound.Locals(i)` (index → `SymbolTable`)。`Locals()` という名前は Store 側に作らない。
- `storetest/equivalence_generated.go` の member 比較から `bound` slot は除く (parse 直後は 0 で、Pointer 側は nil。bind の等価は §6 の driver が見る)。
- `convert` は `bound` slot を 0 で作る。
- 生成物の差分は `shapes_generated.go` (words の増加と役割表 5 本)、`views_generated.go` (getter / setter / 役割 accessor)、`builder_generated.go` (0 埋め) に限られること。

### 3.4 `store.File` の field

7b の `File` に足す:

```go
// parser が書く (§4)
ExternalModuleIndicator     NodeRef
Imports                     []NodeRef            // module specifier の literal
ModuleAugmentations         []NodeRef
AmbientModuleNames          []string
UsesUriStyleNodeCoreModules core.Tristate
// binder が書く
Bound                       *Bound               // bind 開始時に付く
bindOnce                    sync.Once
isBound                     atomic.Bool          // bind 完了後に BindOnce が立てる (Pointer と同じ)
func (f *File) IsBound() bool                    // isBound.Load()
func (f *File) BindOnce(fn func())               // ast.SourceFile.BindOnce と同じ形: fn() の後に isBound を立てる
func (f *File) Symbol() *Symbol                  // Bound.SymbolOf(Root().Symbol())。root header が Pointer の file.Symbol
func (f *File) IsExternalModule() bool           // ExternalModuleIndicator != 0
func (f *File) IsExternalOrCommonJSModule() bool // 上 || Bound.CommonJSModuleIndicator != 0。binder は bind 中に CommonJS 側を書くので、Bound を先に付けてから bind する
```

### 3.5 `GetNodeAtPosition`

`ast.GetNodeAtPosition(file, pos, includeJSDoc)` の Store 版を `position.go` に手書きする。root から `ForEachChild` で pos / end の範囲に入る子へ降りる。JSDoc は無いので第 3 引数は無し。`collectExternalModuleReferences` (§4) が使う。

### 3.6 `storetest.Pairs`

```go
type Pair struct{ P *ast.Node; N store.Node }
func Pairs(file *ast.SourceFile, s *store.Store, opts Options) ([]Pair, Mismatches)
```

`Equivalent` の pre-order の対応付け (`preorderPointer` + `Preorder`、`SkipReparsed` / JSDoc cast の透過を含む) をそのまま返す。訪問数が違えば `Mismatches` に入れて対は返さない。`Equivalent` はこれを使う形に書き換えてよい (出力は変えない)。

## 4. `storeparser` の変更 (7c 設計 §2.5)

1. `internal/parser/references.go` を `storeparser/references.go` に移植する: `collectExternalModuleReferences` / `collectModuleReferences` は root の statements を `Refs()` で回し、動的 import は `ast.ForEachDynamicImportOrRequireCall` の text 走査部分 (`findImportOrRequire`) をそのまま使い、`GetNodeAtPosition` (§3.5) で降りる。書く先は `File` の 4 field (§3.4)。
2. `internal/ast/parseoptions.go` の `SetExternalModuleIndicator` / `getExternalModuleIndicator` / `isFileProbablyExternalModule` / `isAnExternalModuleIndicatorNode` / `getImportMetaIfNecessary` / `findChildNode` / `isFileModuleFromUsingJSXTag` / `walkTreeForJSXTags` を同じ file に移植する。`Builder.View` を通して読む (Compact 前)。`walkTreeForJSXTags` の `SubtreeFacts` の枝刈りは無いので全走査になる。コメントに書く。
3. `parseSourceFileWorker`: Pointer と同じ順に戻す。`finishSourceFile` (indicator を含む) → `!IsDeclarationFile && indicator != 0 && len(possibleAwaitSpans) > 0` なら `reparseTopLevelAwait` → 変わったら `finishSourceFile` をもう一度 → `Finish` → `collectExternalModuleReferences`。7b の `TODO(7c)` コメントを消す。
4. `finishSourceFile` に `ExternalModuleIndicator` を書く。`store.File` の 4 field は references が書く。
5. `doc.go` の「Not ported」から references と indicator を外す。

## 5. 移植規則

`internal/binder/binder.go` を `storebinder/binder.go` に copy し、次の置換を上から順に当てる。**規則に無い書き換えはしない。**

### 5.1 型

| Pointer | Store |
| --- | --- |
| `*ast.Node` (訪問ノード、引数、戻り値、`container` / `thisContainer` / `blockScopeContainer` / `lastContainer`、`ExpandoAssignmentInfo` の 3 field) | `store.Node` |
| `node == nil` / `!= nil` | `node.IsNil()` / `!node.IsNil()` (`nodes[0]` が番兵。accessor は nil を番兵 Node で返す) |
| `*ast.SourceFile` (`b.file`) | `*store.File`。`b.s *store.Store`、`b.bound *store.Bound` も持つ |
| `*ast.FlowNode`、`*ast.FlowLabel` (`currentFlow`、`current*Target`、`preSwitchCaseFlow`、`unreachableFlow`、`ActiveLabel` の 2 field、戻り値) | `store.FlowRef` |
| `*ast.FlowList` | `store.FlowListRef` |
| `flow == nil` / `flow == b.unreachableFlow` | `flow == 0` / `flow == b.unreachableFlow` (index 比較) |
| `*ast.Symbol`、`ast.SymbolTable` | `*store.Symbol`、`store.SymbolTable` |
| `[]*ast.Node` (Declarations) | `[]store.Ref` |
| `ast.NewFlowSwitchClauseData` / `NewFlowReduceLabelData` | `b.newFlowData(a, b, c)` が `Bound.flowData` に append して index を返し、それを `FlowNode.Node` に入れる |
| `core.Arena[ast.Symbol]` 等 | `symbolArena core.Arena[store.Symbol]`、`singleDeclarationsArena core.Arena[store.Ref]`。flow の arena は `Bound.flows` / `flowLists` への append |
| `ast.GetNodeId(attributes)` (pattern ambient module の symbol 名) | `attributes.FileRef()` を `file:id` の 10 進で。`NodeRef` だけだと別 file の同名 pattern が 7d の globals merge で衝突する (7c 設計 §4 の 13)。等価テストは `pattern@<…>` を正規化する (§6.1) |

### 5.2 出力先 (分類 (b))

| Pointer | Store |
| --- | --- |
| `node.DeclarationData().Symbol = s` (`addDeclarationToSymbol`) | `node.SetSymbol(s.Id())` |
| `node.Symbol()` | `b.bound.SymbolOf(node.Symbol())` (binder 内のヘルパ `b.symbolOf(node)`) |
| `node.ExportableData().LocalSymbol = local` | `node.SetLocalSymbol(local.Id())` (役割 setter) |
| `ast.GetLocals(container)` | `b.getLocals(container)`: `container.LocalsSlot()` で slot を読み、0 なら `b.bound.NewLocals()` の index を `SetLocalsSlot` で書き、`b.bound.Locals(i)` を返す |
| `container.LocalsContainerData() != nil` (`lookupName`、`IsLocalsContainer`) | `LocalsSlot()` の ok |
| `node.AsIdentifier().FlowNode = f`、`setFlowNode`、`flowNodeData.FlowNode = f` | `node.SetFlow(f)`。`FlowNodeData() != nil` の判定は不要 (全 kind に slot がある)。成り立つ根拠は 4 呼び出し箇所が kind でガードされていること (statement は `StatementBase` が `FlowNodeBase` を埋め込む、他は kind の分岐の中)。ガード無しの書き込みを足さない |
| `bodyData.EndFlowNode = f`、`setReturnFlowNode`、`clause.FallthroughFlowNode = f` | 役割 setter `SetEndFlowNode` / `SetReturnFlowNode` / `SetFallthroughFlowNode` (kind switch は setter の中の表引きに畳まれる) |
| `node.Flags |= …`、`&^=` (binder.go で `node.Flags` に書く 12 行。`symbol.Flags` / flow の `Flags` / `emitFlags` は別) | `node.AddFlags(…)`、`node.ClearFlags(…)` |
| `b.lastContainer.LocalsContainerData().NextContainer = next` | `b.bound.containers = append(…, next.Ref())` |
| `b.file.Symbol` (読み / 一時上書き / 復元。`bindSourceFileIfExternalModule` の JSON 経路) | root の header: `root.Symbol()` / `root.SetSymbol(…)`。`bindSourceFileAsExternalModule` は `addDeclarationToSymbol` 経由で同じ header に書くので、上書きと復元が同じ場所になる |
| `SymbolCount`、`PatternAmbientModules`、`GlobalExports`、`SetBindDiagnostics` (`addDiagnostic`)、`CommonJSModuleIndicator` | `b.bound.*` (`AddDiagnostic`) |
| `b.file.ExternalModuleIndicator`、`IsDeclarationFile`、`Diagnostics()`、`FileName()`、`Text()` | `b.file.*` |
| `ast.IsExternalModule(file)`、`IsExternalOrCommonJSModule(file)` | `b.file.IsExternalModule()` 等 (§3.4) |
| `symbol.Declarations = append(…, node)` 等 | `node.FileRef()` |
| `SetValueDeclaration` の `valueDeclaration.Kind` | `b.s.Node(ref.Id()).Kind()` (7c は単一 file。`Ref.File()` は検証で `s.file` と一致することを見る) |

### 5.3 flow (分類 (d))

- `newFlowNode(flags)` は `b.bound.NewFlow(flags, 0, 0)`、`newFlowNodeEx` は `NewFlow(flags, node.Ref(), antecedent)`、`newFlowList` は `NewFlowList`。`unreachableFlow` は Pointer と同じく最初に作る (index 1)。
- flow の field を読む / 書く箇所 (`antecedent.Flags`、`label.Antecedents`、`list.Flow` / `list.Next`、`flowStart.Node = node`) は `b.bound.Flow(r).Field` に置き換える。**`*FlowNode` をローカル変数に保持しない** (append で動く)。`addAntecedent` のループは index で回す。
- `combineFlowLists` の再帰はそのまま (index を返す)。
- `createFlowSwitchClause` / `createReduceLabel` は `b.bound.NewFlowData(…)` の index を `Node` に入れる。

### 5.4 訪問

- `bind(node store.Node) bool`、`bindFunc store.Visitor`。`node.ForEachChild(b.bindFunc)`。
- 子を typed view で受ける (`stmt := node.AsWhileStatement(); b.bind(stmt.Expression())`)。Pointer の `stmt.Expression` (field) は `stmt.Expression()` (accessor、解決済み `Node`) に。nil の子は番兵 Node で来て `bind` の先頭の `IsNil()` で戻る。
- list は `Refs()` を回して `b.bind(b.s.Node(ref))` (`bindEach`。`Store.Node` は §3.1)。
- 訪問順を変えない: `bindEachStatementFunctionsFirst`、`bindDestructuringAssignmentFlow` の左右の順、`bindCallExpressionFlow` の IIFE の順など、手書きの順はそのまま。

### 5.5 `ast` の述語と helper

binder.go が `*ast.Node` に対して呼ぶ `ast` の関数のうち、**kind だけを見るもの** (`IsAssignmentOperator`、`IsLogicalOrCoalescingBinaryOperator`、`ModifierToFlag` 等、引数が `Kind`) はそのまま使う。**ノードの中身を見るもの**は `store/utilities.go` に同名で手書きする (`internal/ast/utilities.go` の該当関数の移植。順序も同じ)。binder が呼ぶ集合だけを移植し、一覧を報告に付ける。目安 80 前後: `HasSyntacticModifier`、`GetCombinedModifierFlags`、`GetNameOfDeclaration`、`IsAmbientModule`、`IsGlobalScopeAugmentation`、`IsModuleAugmentationExternal`、`GetModuleInstanceState`、`GetContainingClass`、`IsPrivateIdentifier`、`IsPropertyNameLiteral`、`IsComputedPropertyName`、`IsStringOrNumericLiteralLike`、`IsSignedNumericLiteral`、`HasDynamicName`、`IsStatic`、`IsParameterPropertyDeclaration`、`IsBindingPattern`、`IsBlockOrCatchScoped`、`IsPartOfParameterDeclaration`、`IsVariableDeclarationInitializedToRequire`、`IsEnumConst`、`IsAsyncFunction`、`IsFunctionLike`、`IsObjectLiteralMethod`、`IsObjectLiteralOrClassExpressionMethodOrAccessor`、`IsAutoAccessorPropertyDeclaration`、`GetImmediatelyInvokedFunctionExpression`、`IsPotentiallyExecutableNode`、`IsIdentifierName`、`IsInTopLevelContext`、`IsPrologueDirective`、`IsExpressionOfOptionalChainRoot`、`IsNullishCoalesce`、`IsOptionalChain`、`IsOptionalChainRoot`、`IsOutermostOptionalChain`、`IsLogicalExpression`、`SkipParentheses`、`IsDestructuringAssignment`、`IsAssignmentTarget`、`IsEntityNameExpression`、`IsDottedName`、`IsPushOrUnshiftIdentifier`、`IsRequireCall`、`ExpressionIsAlias`、`IsExpandoInitializer`、`GetElementOrPropertyAccessName`、`GetAssignmentDeclarationKind`、`IsImplicitlyExportedJSDocDeclaration`、`IsInJSFile` (root の flags)、`IsPartOfTypeQuery`、`FindAncestor`、`ModuleExportNameIsDefault`、`IsJsxNamespacedName`、`NodeIsMissing` / `NodeIsPresent`、`IsLeftHandSideExpression`、`IsDeclarationStatement`、`IsVariableStatement`、`IsForInOrOfStatement`、`IsBooleanLiteral`、`IsTypeOfExpression`、`IsStringLiteralLike`、`IsAccessExpression`、`IsExternalModuleReference`、`IsJsonSourceFile` と、`IsXxx` の単純 kind 判定 (`IsIdentifier` 等。`Kind() == …` を inline する 1 行)。

- `scanner.GetRangeOfTokenAtPosition(file, pos)`、`GetErrorRangeForNode(file, node)`、`GetSourceTextOfNodeFromSourceFile(file, node, includeTrivia)`、`DeclarationNameToString(node)` は text と node しか読まない。`storebinder/utilities.go` に Store 版を書く (分類 (f))。`ast.NewDiagnostic(nil, loc, msg, args...)` で file は nil。
- `core.Some(statements, …)` 等の slice ヘルパは `Refs()` のループに。

### 5.6 分類 (f) の規則

逐語でない書き換えは表に列挙する: 箇所、内容、**Pointer 版と Store 版の header の読み回数**。既知の候補: `getDeclarationName` の `GetNodeId` (5.1)、`IsStatic` / `SymbolName` の引数 (§3.2)、scanner ヘルパ (5.5)、`bindParameter` の `slices.Index(node.Parent.Parameters(), node)` (`Refs()` 上で `Ref()` を探す)、`getThisClassAndSymbolTable` の `thisContainer.Parent.Symbol()`。`checkContextualIdentifier` の条件順は**変えない** (7c 設計 §2.2)。

### 5.7 分類 (g): 読みのまとめ pass

逐語移植が §6 のテストを通ってから 1 回だけ。関数ごとに、同じノードに対する同じ accessor (`Kind()`、`Parent()`、`Name()`、slot 読み、`Text()`) の繰り返しを、間にその値を変える書き込みが無ければローカル変数に受ける。対象は **parse 後不変のもの** (kind、pos、end、parent、子、text) だけ。`Flags()`、`Symbol()`、`FlowNode()`、`LocalsSlot()`、`file` / `Bound` の field は bind 中に変わるのでその場で読む。この pass の diff (`binder.go` の逐語版 → まとめ版) を別に保存し、箇所を列挙する。

### 5.8 JSDoc と JS

- `KindJSTypeAliasDeclaration`、`IsImplicitlyExportedJSDocDeclaration`、`NodeFlagsJSDoc` の分岐は**残す** (kind と flag の判定だけで、Store にそのノードが無ければ通らない)。削除は (e) に入れない (7b と違い、binder には JSDoc 専用の関数が無い)。
- JS の CommonJS / expando / `this.x` の経路はそのまま移植する (7c 設計 §2.1)。

## 6. テスト (`storebinder/binder_test.go`)

1. **等価 (fixture)**: `fixtures.ASTBenchFixtures` と小さな source を Pointer (`parser.ParseSourceFile` + `binder.BindSourceFile`) と Store (`storeparser.ParseSourceFile` + `storebinder.BindSourceFile`) で bind し、`storetest.Pairs` の対ごとに次を比べる driver (`bindequiv.go`、test package 内):
   - bind 後の flags (`MaskFlags` を適用)。
   - symbol: `Name` (id を含む 2 種を正規化する: private 名の `#<id>@` → `#@`、pattern ambient module の `pattern@<id>` → `pattern@`。それ以外の緩めはしない)、`Flags`、`CheckFlags`、`Declarations` の (kind, pos, end) 列、`ValueDeclaration` の (kind, pos, end)、`Parent` / `ExportSymbol` の再帰、`Members` / `Exports` の key 集合と値の再帰。(Pointer, Store) の対を memo にして循環を止め、同じ Pointer symbol が別の Store symbol と対になったら mismatch。
   - `LocalSymbol`、`Locals` (不在 kind は両方 nil、key 集合、値の再帰)。
   - flow: node の flow、`EndFlowNode`、`ReturnFlowNode`、`FallthroughFlowNode` から並行に辿る: `Flags`、`Node` の (kind, pos) (SwitchClause は (switch の pos, start, end)、ReduceLabel は target と antecedents を再帰)、`Antecedent` の再帰、`Antecedents` の長さと順序。対を memo。
   - file: `SymbolCount`、`Symbol`、`GlobalExports`、`PatternAmbientModules` (pattern の文字列と symbol)、bind diagnostics の (pos, len, code, message) 列、`CommonJSModuleIndicator` / `ExternalModuleIndicator` の (kind, pos)、`Imports` / `ModuleAugmentations` の (kind, pos) 列、`AmbientModuleNames`、`UsesUriStyleNodeCoreModules`。
   mismatch は kind と label で集計して 1 回で全部出す。
2. **等価 (corpus)**: 7b の corpus (`-short` で fixtures だけ) を同じ driver で。除外は「JS 系で `NodeFlagsReparsed` かつ `Symbol != nil` のノードを持つ file」だけ (番号と名前を log)。7b の await 8 file は除外しない。parse の等価 (7b の `storetest.Equivalent`) が先に通ることを前提にし、通らなければ bind を比べない。
3. **Seal**: `-tags storechecks` で、bind 後に `AddFlags` / `SetFlow` / 予約 slot の setter を呼ぶと panic する。
4. **flow slab**: fixture ごとに `len(flows)`、`len(flowLists)`、`len(flowData)` を `t.Log` し、7c 設計 §2.1 の到達可能数 (checker.ts 45,694 / 15,402 / 878) 以上であること (到達不能な label の分だけ多い)。
5. **`Ref.file`**: corpus 全体で `Declarations` / `ValueDeclaration` の `file` が `Store.file` と一致する。
6. 7b の `parser_test.go`: await 8 file の除外を外し、`TestDeadNodes` に top-level await の reparse を通る source を 1 つ足す。

全テストを tag なしと `-tags storechecks` で通す。7a / 7b のテストも両 tag で通ること。

## 7. ベンチ (`storebinder/bench_test.go`)

`internal/binder/ast_benchmark_test.go` の形を移す。

- `BenchmarkStoreBindV1/<fixture>/{pointer,store}`: parse は `StopTimer` の外 (毎回 parse し直す。Pointer は `BindOnce` が 2 回目を弾くため)。ns/op、B/op、allocs/op。
- `BenchmarkStoreBindKPCV1/{pointer,store}`: checker.ts、`kperf.Session`、`Measure` の中は bind だけ。両方とも分母 298,054 で cycles/node と inst/node を `ReportMetric`。
- `BenchmarkStoreBindRetainedV1/{fixtures,checker.ts,dom}/{pointer,store}`: 7b の `BenchmarkStoreParseRetainedV1` を bind まで伸ばす (parse + bind の結果を保持、GC 2 回、`HeapAlloc` 増分 / node、その後の GC 1 回の ms)。入力は 7b と同じ fixtures 166 file に加えて、checker.ts 単独と dom 単独 (比が symbol の密度で動くため。7c 設計 §5)。

## 8. 完了条件

1. `node tools/scripts/tsc/generate.ts` で生成物が再現し、`git status` の変更が `generate-go-store.ts`、`internal/ast/store/` (§3)、`internal/storeparser/` (§4)、`internal/storebinder/` だけ。`internal/binder`、`internal/parser`、`internal/scanner`、`internal/ast` (store 以外) に差分なし。
2. `go build ./... && go vet ./internal/ast/... ./internal/storeparser/... ./internal/storebinder/...` が通る。production から `storebinder` / `storeparser` / `store` の import なし。
3. §6 の全テストが両 tag で通る。7a / 7b のテストも通る。
4. §7 の 3 ベンチが完走。
5. 報告に: (a) 分類 (a)〜(g) の hunk 数と、(f) の一覧 (header の読み回数付き)、(g) の一覧、(b) `store` に足した API の一覧と §3 に無いものの理由、(c) generator の出力差分の要約 (slot を持つ kind の数、役割表 5 本の在の kind 数)、(d) `store/utilities.go` に移植した関数の一覧、(e) JS の除外 file 数と診断の差分の内訳、(f) flow slab の個数 (fixture ごと)、(g) 移植しなかった関数があればその一覧と理由、(h) Pointer binder のバグに気づいたらその箇所 (直さない)。
