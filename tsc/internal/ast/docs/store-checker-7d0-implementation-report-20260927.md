# Store checker 基盤 (TODO 7d-0) 実装報告

作成日: 2026-09-27。指示書は [store-checker-7d0-implementation-instructions-20260924.md](store-checker-7d0-implementation-instructions-20260924.md) (以下「指示書」)、設計は [store-checker-design-20260923.md](store-checker-design-20260923.md) (以下「7d 設計」)、[動作設計](store-checker-execution-design-20260924.md)、[Z1](store-node-zero-design-20260926.md)、[レビュー追加契約](store-checker-review-contracts-20260926.md)。生の出力は `_store-checker-7d0-impl/` (ignore 対象、ローカルのみ)。KPC はユーザーが 2026-09-27 に 2 回取った (16:48 の `kpc-*-20260927-1648.txt` と、`ForEachChild` の生成を直した後の 17:15 の `kpc2-*-20260927-1715.txt`、§5)。

## 0. 結論

- **正しさ**: parse / bind の等価は 7c と同じ。bind は corpus 17,415 file のうち 17,085 file で mismatch 0、除外 330 file は理由ごとの file 番号まで 7c と一致、宣言の Store 検査 (旧 `Ref.file`) は 708,382 件で 7c と同数。parse は 17,415 file 一致。これは **変換器で出し直した `store/utilities.go` で** の結果。tag なしと `-tags storechecks` の両方で store / storeparser / storebinder / 変換器のテストが通る。例外は `storeparser` の `FuzzEquivalence/a708303f611b95c8` の 1 件で、**変更前の HEAD (d8f8adf5d6) でも同じく落ちる** 既知の差分 (JSX module 判定)。
- **storage の操作列**: 指示書 §6.2 の 17 項目を `store/storage_test.go` に置いた。容量に依存する 11 項目は容量 1 / 2 / 3 / 4096 の 4 通りで回し、正規化した構造 (kind、辺の `SameNode` 関係、親、`File()`、flow の所有者) が一致する。
- **KPC (同じ binary の Pointer 比、cycles)**: 最初の実装は **Walk 1.188〜1.194 で線 ≤1.15 を外れた**。objdump で原因を確かめ (単一の子の `forEachChild*` 41 本と `visitList` が inline から外れ、訪問ごとの呼び出しに戻っていた)、`ForEachChild` の生成を直した後 (after2) は **Walk 1.054、Parse 0.824、Bind 1.020** で 3 本とも線の内側。before (HEAD) は Walk 1.106 / 1.149、Bind 0.992 / 0.984、Parse 0.824。安全の検査は変えていない。§5。
- **inline**: 指示書 §3.2 の必須対象は全部 inline される (`node()` 38、`Parent()` 45、typed view の子 accessor 52、kind 多相の子 accessor 75、`List.At` 78、`FlowNode()` 41、`Symbol()` 40)。多相の子 accessor は素直な生成では 111 で予算を超えたので、生成の形を「slot の値を求めて `node()` を 1 回」に変えた (§2 の 3)。`ForEachChild` は inline に頼らない形に生成し直した (§2 の 5)。inline されなくなったのは utility 8 本 (§3)。
- **retained / GC** (variant 別プロセス、5 run の中央値): checker.ts 0.565 (線 ≤0.60、7c 0.561)、fixtures 0.602 (≤0.70、7c 0.583)、最後の強制 GC の mark CPU 比 checker.ts 0.13 (≤0.25、見積り 0.13)、fixtures 0.22 (≤0.35)。
- **変換器**: `store/utilities.go` は 166 関数の変換出力 + P2 10 関数。binder.go の試作の hunk 一致率は (a) 85%・(c) 58%・(b)(d) 0%、全体 34% (22/64)。線 (案) の「(a)〜(d) の 9 割」には届かない。(b)(d) は書き込み先と flow の index 化で、7d 設計 §2.1 のとおり変換器は読み側の規則しか持たないため。**判断をお願いしたい** (§6.3)。
- 指示書に書かれていない判断を 14 件した。どれも実装を止めるほどではないと判断して進め、§8 に列挙した。大きいのは 3 件: source の Store に program の token を渡す production の API を作っていない (テスト用の hook だけ)、nil の `Flow` どうしの `==` が一致しない (V1 の形のまま)、RTA の動的な辺を変換対象の中の値の参照に絞った。

## 1. `store` の API の変更 (報告 (a))

### 1.1 足したもの

| API | 節 |
| --- | --- |
| `StoreID`、`NodeKey` (`StoreID()`、`ID()`)、`Store.ID()`、`Node.Key()` | §3.1 |
| `IDSpace`、`NewIDSpace(nFiles)`、`(*IDSpace).Next()` (`idspace.go`) | §3.1 |
| `Node.File()`、`Builder.FinishFile(*File)` | §3.1 |
| foreign の辺 (`foreignBit`、`Store.foreignNodes` / `foreignLists`、`(*Store).list`) | §3.2 |
| `SameNode`、`SameList`、Z1 の `IsNil` (`h == nil \|\| kind == Unknown`)、`Node` の非比較化 (`_ [0]func()` を先頭に) | §3.3、Z1 |
| `Symbol.gid`、`GetSymbolId` (P2、`utilities_manual.go`) | §3.4 |
| `Flow` / `FlowList` (解決層) と `Flags` / `Node` / `Antecedent` / `Antecedents` / `SwitchClause` / `ReduceLabel` / `Next` / `Flow`、`Node.Symbol()` / `FlowNode()`、生成の `Locals()` / `LocalSymbol()` / `EndFlowNode()` / `ReturnFlowNode()` / `FallthroughFlowNode()` (Node と typed view)、`Bound.Store()` | §3.5 |
| `NodeRegion` / `NewNodeRegion`、`Factory` / `NewFactory` / `FactoryHooks`、`ReleaseArenas`、`NewNodeList`、writer 7 本、`CloneNodeList`、定義ごとの `New*` 194 / `Update*` 167 / `Clone*` 193 と `CloneNode` (`factory.go`、`factory_generated.go`) | §3.6 |
| `node_helpers.go`: `FirstNonNil` / `MapNonNil` / `OrElse` / `SameMap` / `SameMapIndex` / `Deduplicate` / `AppendIfUnique` / `Contains` / `Index` | §5.1 |

§3 に無いもの:

| API | 理由 |
| --- | --- |
| `Node.Loc() core.TextRange` | 変換規則 `x.Loc` → `x.Loc()` (§5.1 の表) の受け手 |
| `Node.PropertyNameOrName()` | Pointer の `(*Node).PropertyNameOrName` と同名の accessor。`getModuleInstanceStateForAliasTarget` が規則で訳せる (7c は手書きで展開していた) |
| `Builder.FinishFile(*File)` | §3.1「File に Store を渡すときに Store.file を設定する」の入口。storeparser の 2 か所が `result.Store = p.b.Finish()` の代わりに呼ぶ |
| `Factory.NodeCount()` / `TextCount()`、`FactoryHooks.OnUpdate` / `OnClone`、`NewModifierList` | 動作設計 §1「NodeCount / TextCount 等は従来の契約を保つ」と `ast.NodeFactory` / `NodeFactoryHooks` と同じ形 |
| `Factory.CloneNode(Node)` | `(*ast.Node).Clone(f)` の受け手 (定義ごとの `Clone*` への dispatch、生成) |
| `Contains` / `Index` (Node 版) | 規則 `slices.Contains` / `Index` → `SameNode` で探す版 |
| export_test.go の hook (`SetInitialCaps`、`TextsCap`、`SetRawParent`、`SetNextSymbolId` など) | storage テスト専用。production の API ではない (§8 の 1) |

### 1.2 改名

| 7c | 7d-0 |
| --- | --- |
| `Node.Symbol() SymbolId` / `SetSymbol` | `SymbolId()` / `SetSymbolId` (id 層)。`Symbol()` は `*Symbol` (解決層) |
| `Node.FlowNode() FlowRef` / `SetFlow` | `FlowRef()` / `SetFlowRef`。`FlowNode()` は `Flow` |
| `LocalsSlot` / `SetLocalsSlot` | `LocalsRef` / `SetLocalsRef`。`Locals()` は `SymbolTable` |
| `LocalSymbol() SymbolId` / `SetLocalSymbol` | `LocalSymbolId` / `SetLocalSymbolId`。`LocalSymbol()` は `*Symbol` |
| `EndFlowNode` / `ReturnFlowNode` / `FallthroughFlowNode` (FlowRef) と Set | `EndFlowRef` / `ReturnFlowRef` / `FallthroughFlowRef` と Set。元の名前は `Flow` を返す解決層 |
| `FlowList` (slab の行) | `FlowListEntry`。`FlowList` は解決層の型 (§8 の 4) |
| `NewBound()`、`NewBoundIn(scratch)` | `NewBound(s)`、`NewBoundIn(scratch, s)` (§4 の 5。Bound と Store が互いを指す) |
| `NewBuilder(src, file uint32)`、`Reset` | `NewBuilder(src, id StoreID)`、`Reset(src, id)` |
| `SymbolName(st, symbol)`、`(*Symbol).IsStatic(st)` | `SymbolName(symbol)` (変換の出力)、`IsStatic()` |
| `IsModuleAugmentationExternal(file, node)`、`IsImplicitlyExportedJSDocDeclaration(file, node)` | Pointer と同じ `(node)`。U2 の出し直しで Pointer の形に戻った (§4) |

### 1.3 削ったもの

`Ref`、`Ref.File()` / `Id()`、`Node.FileRef()`、`Node.nilNode()` (Z1 で `Node{}` が nil)、`position.go` (`GetNodeAtPosition` は P2 に移し、`nodeContainsPosition` は変換の出力)、`symbol.go` の `GetSymbolTable` / `GetMembers` / `GetExports` / `SymbolName` (根に入るので変換の出力に移した)。

## 2. generator の出力差分 (報告 (b))

`generate-go-store.ts` +355/−51。生成物は `views_generated.go` (+/− 約 1,900 行)、`foreach_generated.go` (+/− 約 2,000 行、§2 の 5)、`convert_generated.go` (1 行)、`factory_generated.go` (新設 3,866 行)。`shapes_generated.go`、`builder_generated.go`、`storetest/equivalence_generated.go` は不変。

1. typed view 178 本を `type Xxx Node` にした (Z1: 同じ underlying type から導き、比較不能も引き継ぐ)。
2. list slot の読みを `n.s.list(ListRef(...))` にした (foreign の list を正規の `List` に解く)。子の slot は元から `n.s.node(...)` なので `node()` の変更がそのまま効く。
3. **kind 多相の子 accessor** は `var e NodeRef; if slot != 0xFF { e = ... }; return n.s.node(e)` の形にした。素直に 2 経路で `node()` を呼ぶ形 (7c の形) だと foreign 側が 2 回展開されて cost 111 になり inline されない。slot の無い kind は辺 0 (番兵) を読むので意味は同じ。cost 75。
4. 予約 slot の accessor を 2 層にした: id 層 `LocalsRef` / `LocalSymbolId` / `EndFlowRef` / `ReturnFlowRef` / `FallthroughFlowRef` と setter、解決層 `Locals` / `LocalSymbol` / `EndFlowNode` / `ReturnFlowNode` / `FallthroughFlowNode` (Node と typed view)。
5. **`ForEachChild` は inline に頼らない形にした**。slot が 1 つ (子か list) の定義 60 本は switch の case に本体を直接書き、複数の slot を持つ定義の `forEachChild*` は list の loop を展開する (要素は list の owner の Store で解く)。`visitList` は消した。同じ定義の kind は 1 つの case にまとめた。最初の実装は 7c の形 (case ごとに `forEachChild*` を呼び、list は `visitList`) のままで、7c では単一の子の 41 本 (cost 64) と `visitList` (80) が inline されていたが、`node()` の foreign 側で 92 / 110 になり、訪問ごとに呼び出しが 1 段増えて Walk が線を外れた (§5)。`ForEachChild` 自体の機械語は 1,465 → 4,057 行。
6. `factory_generated.go`: 定義ごとの `New` / `Update` / `Clone`。引数は Pointer の `NodeFactory` と同じ (schema の `noFactory` を除く member、schema の順、kind と `NodeFlags` の引数、`bitmask`、kind alias の `NewJSTypeAliasDeclaration` など)。子は `Node`、list は `List`、text は値。Store に slot の無い member (JSDoc の `[]string` 4 定義、`SyntheticExpression.Type`) は引数にせず TODO コメントを出す。`Update` は `SameNode` / `SameList` / 値の比較、`Clone` は浅い (§3.6)。
7. `convert`: `NewBuilder` の第 2 引数を `store.StoreID(fileIndex)` にしただけ。

## 3. inline 判定 (報告 (c))

`go build -gcflags=-m=2 ./internal/ast/store/` (通常 build、Go 1.27.0 darwin/arm64)。before は HEAD。生の出力は `_store-checker-7d0-impl/inline-{before,after}.txt`。

| 関数 | before | after |
| --- | ---: | ---: |
| `(*Store).node` | 10 | 38 |
| `Node.Parent` | 17 | 45 |
| typed view の子 accessor (`QualifiedName.Left` など) | 24 | 52 |
| kind 多相の子 accessor (`Node.Expression` など 31 本) | 55 | 75 |
| `List.At` | 50 | 78 |
| `(*Store).list` / `Block.Statements` | — / 20 前後 | 29 / 43 |
| `Node.IsNil` | 6 | 11 |
| `Node.FlowNode` / `Node.Symbol` | 4 / 4 (id 層) | 41 / 40 (解決層、分岐 1 つ) |
| `Node.Locals` / `Node.LocalSymbol` / `Node.ReturnFlowNode` | — | 76 / 68 / 69 |
| `SameNode` / `Node.Key` | — | 34 / 41 |
| `Node.Ref` | 16 | 16 |
| `(*Builder).header` / `(*Builder).List` | 33 / 41 | 40 / 48 (行数と extra の foreignBit 検査) |
| `Node.File` | — | cannot (111) |

inline されなくなったもの (同名の関数で can → cannot)。最初の実装では単一の子の `forEachChild*` 41 本 (64 → 92) と `visitList` (80 → 110) もここに入っていたが、§2 の 5 で関数ごと無くした (switch の中に書くか loop を展開した) ので、今は 8 本:

| 関数 | before → after | 理由 |
| --- | --- | --- |
| `FindAncestor` / `GetRootDeclaration` / `GetContainingClass` / `IsLiteralLikeElementAccess` / `IsLogicalOrCoalescingAssignmentExpression` / `IsLogicalOrCoalescingBinaryExpression` / `IsObjectLiteralOrClassExpressionMethodOrAccessor` / `IsPrologueDirective` | 50〜79 → 88〜132 | `Parent()` と子 accessor の cost 増。`IsObjectLiteralOrClassExpressionMethodOrAccessor` は 7c の (g) (`parentKind` のまとめ) が逐語に戻ったぶんも乗る |

Builder の ctor は 178 本が cannot のまま (before も同じ)、can 1 本も同じ。Factory の生成 `New` 194 本は全部 cannot (cost 123〜794。Store の選択・辺の符号化・hook を 1 関数に持つため)、手書きの `NewNodeList` 63 / `NewModifierList` 67 は can。

切り分けの材料として、印の検査を XOR (`foreignNodes[e^foreignBit]`: 印の無い値は必ず 2^31 以上の index になり bounds check で落ちる) にした版を一時的に測った: `node()` 29、`Parent()` 36、子 accessor 43、多相 66 だが、`forEachChild*` 83・`visitList` 101 で予算は超えたまま。決め手にならないので採っていない (元の明示的な印の検査のまま)。

objdump (kperf build の `Node.ForEachChild`、`go tool objdump`): before (HEAD) は単一の子の 41 case を inline し visitor の間接呼び出しが 41、最初の実装はそれが 0 で 41 case とも `forEachChild*` の呼び出し、複数の slot の関数は `CALL visitList`。子の辺の解決 (`node()`) の速い側は比較と分岐が 1 回ずつで bounds check と同じ数、増えたのは分岐先の foreign 側 (通らないコード) だけ。after2 は visitor の間接呼び出しが 60 (単一の子 41 + 単一の list 19) で、`visitList` の呼び出しは無い。これ以外の call site の突き合わせ (検証指示 §3 の後半) はしていない。

## 4. storebinder / storeparser の hunk (報告 (d))

`git diff HEAD -- internal/storebinder internal/storeparser`: 48 hunk、+118/−100。

| 分類 | 内容 |
| --- | --- |
| §4 の 1 (StoreID、`Store.file`) | storeparser の `result.Store = p.b.Finish()` 2 か所 → `p.b.FinishFile(result)`。`NewBuilder(sourceText, 0)` は定数 0 なので字面は不変 |
| §4 の 2 (id 層の名前) | `SetFlow` → `SetFlowRef`、`SetSymbol` → `SetSymbolId`、`SetLocalSymbol` → `SetLocalSymbolId`、`LocalsSlot` → `LocalsRef`、`Set{End,Return,Fallthrough}FlowNode` → `…FlowRef`、`symbolOf` と JSON の root の読みは `SymbolId()` |
| §4 の 3 (D2) | `singleDeclarationsArena core.Arena[store.Node]`、`newSingleDeclaration(store.Node)`、`append(…, node)`、`SetValueDeclaration(symbol, node)` と `isFunctionSymbol(symbol)` は Pointer と同じ形に戻った (`Ref` の解決が要らない)、`declareSymbolEx` の `range symbol.Declarations` と `bindClassLikeDeclaration` の `Declarations[0]`、`getInitializerSymbol` の `ValueDeclaration`、`store.SymbolName(symbol)`、pattern 名は `node.Key()` の `StoreID:NodeRef` |
| §4 の 4 (等価テスト) | `bindequiv_test.go` の `ref` → `declaration` (Declarations / ValueDeclaration の Node が file の Store にあるか。`n.File() == m.file` で判定)、id 層の名前。`binder_test.go` の `countRefs` → `countDeclarations`。`seal_checks_test.go` の名前 |
| §4 の 5 (`Bound.store`) | `store.NewBoundIn(scratch, b.s)` |
| §4 の private 名 | `GetSymbolNameForPrivateIdentifier` は `strconv.FormatUint(store.GetSymbolId(containingClassSymbol), 10)` |
| §4 以外 (Z1) | Node が比較不能になったので `==` 5 か所 (`getInferTypeContainer`、`isStatementCondition` 3、`isTopLevelLogicalExpression`) を `store.SameNode`、`core.AppendIfUnique` を `store.AppendIfUnique` (§5.1 の規則と同じ置換) |
| §4 以外 (U2) | `store.IsImplicitlyExportedJSDocDeclaration(b.file, node)` と `store.IsModuleAugmentationExternal(b.file, node)` を `(node)` に。出し直した utility が Pointer のシグネチャになった結果 (`binder/binder.go` と同じ呼び出しに戻った) |
| doc | `storebinder/doc.go` の `store.Ref` などの記述、storeparser `bench_test.go` の dom sub-benchmark (§7) |

private 名の gid 採番: KPC の入力 (checker.ts、dom、fixtures 166 file) には private member の宣言が 0 件で、bind 中の gid の初回採番は 0 回。Bind KPC にこの費用は乗らない。

## 5. 費用 (測ったもの)

**KPC** (ユーザーが別ターミナルで取った。GC オフの Session、`-test.benchtime 20x -test.count 5`、値は 5 run の中央値。before は HEAD d8f8adf5d6 の binary、after は最初の 7d-0、after2 は §2 の 5 の後)。2 回取った。

1 回目 (16:48、load average 2.2〜2.6、before1 → after → before2):

| 指標 | 線 | before1 | after | before2 | 7c (Compact 後) |
| --- | --- | ---: | ---: | ---: | ---: |
| Walk cycles S/P | ≤ 1.15 | 1.132 | **1.194** | 1.136 | 1.137 |
| Parse cycles S/P (checker.ts) | ≤ 1.00 | 0.824 | 0.826 | 0.824 | 0.824 |
| Bind cycles S/P | ≤ 1.10 | 0.984 | 1.029 | 0.988 | 0.984 |

Walk が線を外れたので objdump で原因を確かめ (§3)、§2 の 5 の生成の変更を入れて 2 回目を取った。

2 回目 (17:15、load average 4.1 → 2.8、before1 → after → after2 → before2。`kpc2.sh`):

| 指標 | 線 | before1 | after | **after2** | before2 |
| --- | --- | ---: | ---: | ---: | ---: |
| Walk cycles S/P | ≤ 1.15 | 1.106 | 1.188 | **1.054** | 1.149 |
| Walk inst S/P | 報告 | 1.219 | 1.307 | 1.297 | 1.221 |
| Parse cycles S/P (checker.ts) | ≤ 1.00 | — | 0.825 | **0.824** | — |
| Parse inst S/P (checker.ts) | 報告 | — | 0.825 | 0.825 | — |
| Parse cycles S/P (dom、7d-1 の before) | 報告 | — | 0.866 | 0.865 | — |
| Bind cycles S/P | ≤ 1.10 | 0.992 | 1.027 | **1.020** | 0.984 |
| Bind inst S/P | 報告 | 1.091 | 1.137 | 1.143 | 1.089 |

2 回目は load が高く、before2 の Walk の Store 側は run 間で 4.2% ばらついた (他は 0.2〜0.7%)。1 回目の before (1.132 / 1.136) と合わせて読むと、after2 の Walk は 7c より下がっている。

Store 側の絶対値 (2 回目、before は 2 回の平均):

| | inst before → after → after2 | cycles before → after → after2 | IPC after2 |
| --- | --- | --- | ---: |
| Walk | 26.33M → 28.20M → 27.97M | 8.68M → 9.21M → 8.33M | 3.36 (before 3.03) |
| Bind | 120.77M → 125.59M → 126.27M | 33.01M → 34.74M → 34.08M | 3.70 |
| Parse (checker.ts) | (1 回目 212.8M) → 214.69M → 215.17M | (1 回目 55.3M) → 54.94M → 55.18M | 3.90 |

- **Walk**: after2 は before より inst が +6.3% (1 visit あたり約 +5.5) 多いが、cycles は −4% 前後。訪問ごとの呼び出しが消え (単一の list の 19 定義は 7c でも呼び出しだったのでそれも消えた)、IPC が 3.03 → 3.36 に上がった。残る inst の増分は辺の解決の foreign 側の分岐と、kind をまとめた case の判定。
- **Bind**: after2 は Store 側 cycles が before 比 +3.2%、inst +4.6%。binder は bind の本体で子を役割 accessor と `Refs()` で読み、`ForEachChild` の比重が小さいので §2 の 5 の効果は小さい。増分の候補は `node()` の分岐、binder の header writer に足した番兵の拒否 (§8 の 10)、訳し直した utility (7c の (g) の読みのまとめが逐語に戻った分と、inline を失った 8 本)。命令列では確かめていない。
- **Parse**: 変わらない。+1.8M inst (約 +6 inst/ノード) は Builder の `header` / `List` に足した foreignBit の検査 (比較と分岐 1 回ずつ) が候補で、cycles には出ていない。

Pointer 側は各回 ±2% 以内で、差は Store 側の変更による。B/op は Bind 6.51 → 6.95 MB (+6.8%、D2 の宣言 16B と Symbol 112B)、Parse は不変。

**ns** は load average 3.7〜5.9 で参考値にもならないので載せない。

**retained / GC** (`BenchmarkStoreBindRetainedV1`、sub-benchmark 名で片方の variant だけを走らせる別プロセス、入力 × variant を交互に 5 run、`GODEBUG=gctrace=1`)。retained は 5 run で 0.1% 以内。mark CPU は最後の強制 GC の assist + background + idle。load 3.7 なので GC の時間は参考値。

| 入力 | retained B/node Pointer / Store | 比 (線) | 7c | mark CPU ms Pointer / Store | 比 (線) |
| --- | --- | ---: | ---: | --- | ---: |
| checker.ts | 109.4 / 61.86 | 0.565 (≤0.60) | 0.561 | 20.4 / 2.7 | 0.13 (≤0.25) |
| fixtures | 124.6 / 74.96 | 0.602 (≤0.70) | 0.583 | 84.0 / 18.1 | 0.22 (≤0.35) |
| dom | 128.4 / 95.03 | 0.740 | 0.70 | 8.8 / 3.5 | 0.40 |

Store 側の増分 (7c 比): checker.ts +0.4 B/node、fixtures +2.3 B/node。D2 の見積り (宣言 1 つにつき +8B、Symbol +16B) の範囲。`Symbol` は 112B (ast.Symbol 96B)。

このベンチは両 variant とも text を読んでから計測を始め、比較用の AST は作らない (sub-benchmark の filter で片方しか走らない)。live heap profile での監査はしていない。

## 6. 変換器 (報告 (e)、(f))

### 6.1 規則

`tools/cmd/storetranslate` (tools module、x/tools v0.50.0 の `go/packages` / `ssa` / `callgraph/rta`)。規則は go/types の型と package store の API (同じ `packages.Load` で読む) から決まるものだけ。

| Pointer | Store | 指示書 §5.1 |
| --- | --- | --- |
| `*ast.Node` と alias、data struct の pointer | `store.Node`、typed view | 表 |
| `x == nil` (node、list、flow) | `x.IsNil()` | 表 |
| node どうしの `==` / `!=`、list どうし | `SameNode`、`SameList` | 表 |
| `x.Kind` / `Flags` / `Parent` / `Loc` | method 呼び出し | 表 |
| `x.AsXxx().Field` | typed view の accessor。view に無く `Node` にあれば `Node(v).F()` | 表 |
| `*ast.NodeList`、`.Nodes`、`len` / index / `range` | `List`、`Len` / `At` / index loop (名前でない list は先に変数へ) | 表 |
| `ast.GetSourceFileOfNode(x)`、`ast.GetNodeId(x)`、`map[*ast.Node]T` | `x.File()`、`x.Key()`、`map[NodeKey]T` と index / `delete` / literal の key に `.Key()` | 表 |
| `slices.Contains` / `Index` (node の slice) | `store.Contains` / `Index` | 表 |
| `core.FirstNonNil` / `MapNonNil` / `OrElse` / `SameMap` / `SameMapIndex` / `Deduplicate` / `AppendIfUnique` | 実際の型引数で判定 (FirstNonNil / MapNonNil は結果 U、他は T) し store の Node 版。他の型引数は core のまま | 表 (Z1) |
| key 専用 struct の node field (map の key、`collections.Set` などの要素、入れ子と配列も) | field を `NodeKey`、keyed / positional literal と代入の値を `.Key()`、nil literal を `NodeKey{}`。読み戻し・export・範囲外の使用があれば未対応として報告 | 表 (Z1) |
| `*ast.Symbol` / `SymbolTable` / `*ast.FlowNode` / `*ast.FlowList` | `*Symbol` / `SymbolTable` / `Flow` / `FlowList`。flow の field は method | 表 |
| `x.FlowNodeData().FlowNode` | `x.FlowNode()` | 表 |
| `ast.Xxx(...)` | `store.Xxx(...)` (package store の中では修飾なし) | 表 |
| untyped nil で文脈が node / list / flow | `Node{}` / `List{}` / `Flow{}` | 表の帰結 |
| `x.Pos()` / `End()` (int32) | `int(x.Pos())`。比較で両辺が Pos / End か一方が定数なら変換を落とす | 足した (§8 の 7) |
| `x.Loc.Pos()` / `End()` (node) | `x.Pos()` / `End()` (`(*ast.Node).Pos` の定義そのもの) | 足した |
| `x.AsSourceFile()` | `x.File()` (`*ast.SourceFile` ↔ `*store.File`) | 足した |
| `x.AsNode()` (data) | `Node(x)` | 足した |
| export された ast 関数でシグネチャに node の無いもの (`IsAssignmentOperator` など) | 訳さず `ast.X` のまま (7c と同じ流儀)。unexported のものは store から呼べないので訳す | 足した |

未対応として報告するもの (問題として出し、出力に使わない): node の field への書き込み、list の要素の書き込み・slice・`append`・`[]*Node` を取る引数への list の受け渡し (`core.Some(node.Arguments(), …)` など)、node への type assertion / type switch、node の switch、node の method value、Store に同名の method / field が無いもの、Store の field の型が Pointer と違うもの (`File.ExternalModuleIndicator` は `NodeRef`)、comparable 制約の helper で規則の無いもの (`core.FirstNonZero` など)。

到達可能性 (E3): 根 171 (7c の `store/utilities.go` の 164 関数 (`roots.txt`) と、storebinder / storeparser が `store.X` で呼ぶ ast の関数 7) から、RTA の call graph (全 package の `init` を RTA の根に足す、到達可能 65,638 関数) を ast の package-level 関数 (method を除く) の中だけで辿る (2026-09-28 に構文上の到達計算へ置き換え、§8 の 3)。P2 の関数は Pointer 版の本体を辿らず、手書き版の callee を辿る。規則が呼び出しごと書き換える `GetSourceFileOfNode` / `GetNodeId` と、ast に残す export された kind の述語 (§8 の 5) は辿らない。到達 176 = 変換 166 + P2 10 (§8 の 3 で動的な辺を絞った後)。

テスト (`translate_test.go`): tsc module に overlay で置いた fixture package を訳し、(1) Z1 の helper の規則が Node の型引数でだけ当たり他は core のまま、(2) 2 つの既存の複合 key を写した `nonExistentPropertyKey` (Set の要素、positional literal、keyed literal の nil、field 代入、`Has` / `Add`) と `accessibleChainCacheKey` (map の key、`make`、lookup、keyed literal) が `NodeKey` になる、(3) node key の map、list の loop、nil と比較、を出力の文字列で確かめ、**出力を別の仮想 package として package store に対して型検査する**。未対応の fixture で、`core.FirstNonZero` の Node、読み戻される key struct の field、list を slice として渡す箇所が問題として報告されることを確かめる。P2 の hash を 1 つ壊すと変換が止まることも確かめる。

Node 版 helper の意味 (`store/node_helpers_test.go`): 零値、2 つの Store の番兵、2 つの Store の実ノード、foreign の辺を経た同じ実ノードを混ぜた 9 要素の全部分列で、各 helper を core 版 (`Key()` の列に対して) と比べ、結果、callback の引数の列 (回数と順序)、入力 slice を返すかどうか (再利用)、nil slice の保持が一致する。

### 6.2 `store/utilities.go` の出し直しと P2 (報告 (f))

出力は 166 関数、ソースの file 名・offset 順、コメントつき、gofmt 済み。3 回の実行でバイト一致。7c の手書き版に無かった関数: `IsJSDocTypeAssertion`、`GetLeftmostAccessExpression`、`IsAsExpression` など、Pointer の枝をそのまま訳したことで到達したもの。

P2 (`utilities_manual.go`、各関数の直前に `storetranslate:pointer-hash`):

| 関数 | 理由 |
| --- | --- |
| `GetLocals` | Store の locals は Bound の表で、binder が初回に作る。引数に `*Bound` が要る (7c の (f) 15) |
| `GetNodeAtPosition` | JSDoc の引数が無い (7d-1 まで Store に JSDoc が無い) |
| `GetSymbolId` | I1 の gid。Pointer の `symbol.id` を訳すと Bound の密な番号を読んでしまう |
| `IsLocalsContainer` | `LocalsContainerData() != nil` に相当する Store の API は slot 表 (7c の (f) 17) |
| `IsExternalModule` / `IsExternalOrCommonJSModule` | `File` の field は `NodeRef` と Bound の中 |
| `GetExternalModuleName` | `core.FirstOrNil(node.Arguments())`: list を slice の helper に渡す規則が無い |
| `NodeHasName` / `IsPotentiallyExecutableNode` | `core.Some` に list を渡す |
| `IsAsyncFunction` | `BodyData()` が無い。`Body()` / `AsteriskToken()` の多相 accessor で書く |

7c の (f) / (g) のうち utility に入れたものの扱い:

| 7c | 扱い |
| --- | --- |
| (f) 33 `IsOuterExpression` の JSDoc 枝なし | 逐語で足りた: `IsJSDocTypeAssertion` は Reparsed flag を読むだけで訳せ、Store の parse は reparse しないので枝は取られない |
| (f) 34 `isVariableDeclarationInitializedWithRequireHelper` の `allowAccessedRequire` panic | 逐語で足りた (`GetLeftmostAccessExpression` も訳した。7c の panic は消えた) |
| (f) 35 `GetModuleInstanceState` 系の `map[NodeId]` と ancestors | 規則 (`NodeId` → `NodeKey`、`GetNodeId` → `Key()`、`[]*Node` → `[]Node`) |
| (f) 36 `IsRequireCall` / `IsBindableObjectDefinePropertyCall` の `.AsIdentifier().Text()` | 逐語で足りた (役割の `Text()` のまま。inline されない呼び出しが残る) |
| (g) `IsObjectLiteralOrClassExpressionMethodOrAccessor`、`IsExpressionOfOptionalChainRoot`、`IsModuleAugmentationExternal`、`GetElementOrPropertyAccessName`、`GetAssignmentDeclarationKind`、`IsBindableObjectDefinePropertyCall` / `IsRequireCall`、`getModuleInstanceStateWorker` | 逐語で足りた (規則にしない方針どおり)。`IsPotentiallyExecutableNode` は (g) ではなく `core.Some` のため P2 |
| 7c 実装報告 §4 の 3 件 (`IsModuleAugmentationExternal(file, node)`、`IsImplicitlyExportedJSDocDeclaration(file, node)`、`IsJsonSourceFile(file)`) | 規則 (`AsSourceFile()` → `File()`、`*SourceFile` → `*File`) で Pointer のシグネチャのまま訳せた。storebinder の 2 か所の呼び出しを直した (§4) |

### 6.3 binder.go の試作 (報告 (e))

`internal/binder/binder.go` を丸ごと訳し (`-binder DIR`)、7c の逐語版 (`binder-verbatim.diff` を現行 `binder/binder.go` に当てて復元) と比べた。比較の前の正規化は 7d-0 で改名した API 名の対応表 (`compare.go` の `renameTable`、17 項目、名前だけ) のみ。hunk の分類は 7c 報告が記録していないので、hunk の追加行のキーワードで付け直した ((d) flow → (b) 出力先 → (c) 子の受け直し → (a) の順)。hunk が一致 = hunk の追加行が全部、試作の同じ関数の行にある。

| 分類 | hunk | 一致 | 残りのうち 7c 以後の設計変更 (D2 / Z1 / K3) による差 |
| --- | ---: | ---: | ---: |
| (a) 型の置換 | 13 | 11 (85%) | 0 |
| (b) 出力先の置換 | 14 | 0 (0%) | 11 |
| (c) 子の受け直し | 19 | 11 (58%) | 6 |
| (d) flow の index 化 | 18 | 0 (0%) | 6 |
| 計 | 64 | 22 (34%) | 23 |

関数単位では 171 中 70 (41%) が一致。訳せなかった箇所は 83 件 (`binder-trial-problems.txt`): node の field への書き込み 21、`*SourceFile` の field と method (`AsNode`、`Symbol`、`CommonJSModuleIndicator` など) 27、list を slice として渡す・代入 9、`FlowNodeData` 6、`LocalsContainerData` / `DeclarationData` / `BodyData` などの data 取得 6、flow の合成ノード (`NewFlowSwitchClauseData` など) と診断の作成 4、Binder struct の型 (flow label が Pointer の型) 1 など。

(a)(c) の不一致のうち設計変更でないもの 4 件は、いずれも 7c 報告の (f) (#4 `declarationNameToString`、#11 `getInitializerSymbol` の method 化、#27 SourceFile の field、flow の 0 比較)。(b)(d) は書き込み先 (`SetFlowRef`、`b.bound.*`) と flow の slab で、7d 設計 §2.1 のとおり変換器は (a) と (c) の類の規則しか持たないので、原理的に一致しない。**線 (案)「(a)〜(d) の 9 割」は満たさない。(a)(c) だけなら設計変更と (f) を除いて全部一致している。線の読み方 (書き込み側の (b)(d) を分母に入れるか) と、7d-2 の前に規則を足すか (候補: list を slice として取る core の helper の List 版、`*SourceFile` の field の File 版) の判断をお願いしたい。**

## 7. storage の操作列 (報告 (g))

`store/storage_test.go` (package `store_test`。source は storeparser + storebinder、合成は Factory)。容量 = 1 / 2 / 3 / 4096 の 4 通り (`eachCapacity`) は構造の文字列 (`describe`) を比べる。

| # | テスト | 容量 | 主な assert |
| --- | --- | --- | --- |
| 1 | `TestStorageThisPropertyReference` | 4 通り | `Name()` が源の名前と `SameNode`、源の名前の親が不変、`this.Parent()`、`ref.Parent()`、`FlowNode()` が constructor の return flow で源の Bound が所有、`File()`、容量 1 で this と ref の StoreID が違う |
| 2 | `TestStorageSyntheticElementAccess` | 4 通り | 源の式が子、源の親が不変、括弧で包む枝、flow、`File()` |
| 3 | `TestStorageLateParentAcrossStores` | 4 通り | 容量 1 で StoreID が変わる、古い Store のノードに後から新しい Store の親、保持した Node と accessor の Node が同じ更新を読む、親の連鎖 |
| 4 | `TestStorageUpdateAndClone` | 4 通り | 無変更の `Update` (別の Store の nil を含む) が元を返し hook を呼ばない、変更ありは新しいノード、`Clone` が子と list を共有、親・flow・symbol を写さず flags と range を写す |
| 5 | `TestStorageCloneNodeList` | 4 通り | 新しい block、要素の共有、source の list の clone、storechecks で Clone の先・元・source の clone への `SetListElement` が panic、source の list への書き込みは通常 build でも panic、無関係な list は通る |
| 6 | `TestStorageNilIdentity` | — | `Node{}`・2 つの source の番兵・合成の番兵の全組で `SameNode`、`Key() == NodeKey{}`、`File() == nil`、`make([]Node, n)` と `Symbol.ValueDeclaration` の零値 |
| 7 | `TestStorageCorruptEdges` | — | 通常 build で: 行数の直後・遠くの印の無い値、source の Store の foreign の値、foreign 表の範囲外、foreign 表を持つ Store の小さい印の無い値、が panic |
| 8 | `TestStorageWriterOwner` | 4 通り (ループ) | 通常 build で source のノード・root・別 region・seal 済み・`Node{}`・番兵への writer 5 本と `SetLocals` / `SetListElement` が panic、binder の writer も番兵に書けない |
| 9 | `TestStorageReleaseArenas` | 4 通り | 解除前のノードの読み書き・子の接続、解除後の `New` が別の StoreID |
| 10 | `TestStorageFakeScope` | 4 通り | `Locals()` が同じ map (後で足した key が見える)、親が源の locals、Resolve の順の祖先上り |
| 11 | `TestStorageSizes` | — | `NodeHeader` 32、`Node` 16、`Flow` 16、`FlowNode` 16、`NodeKey` 8、`Symbol` 112 を記録 |
| 12 | `TestStorageFlowGraph` | 4 通り (後半) | Pointer の flow graph と V1 の API だけで辿った graph を比べる (Bound の表を読まない)。branch / loop / condition / switch clause / reduce label / assignment を全部通ったことを assert。2 つの Bound で同じ番号の FlowRef、片方の flow を付けた合成ノードから辿った graph がもう片方に解かれない |
| 13 | `TestStorageImportBoundary` | — | 同じ数値の StoreID の別 program のノードと別 region の合成ノードを `New` / `NewNodeList` / `SetParent` / `SetListElement` / `Update` に渡すと panic、別 program の flow の `SetNodeFlow` が panic、同じ region の別 Factory と同じ program の source は通る |
| 14 | `TestStorageHookReentry` | 4 通り | hook の中の `New` で active が替わっても外側が自分の行を返す、容量 1 で StoreID が違う |
| 15 | `TestStorageColumnGrowth` | 4 通り (大きい容量 × 64) | `extra` の初期 cap 3 と `texts` 0 で、右辺でノードを作ってから `SetListElement`。`extra` の cap が変わったことを assert、保持した `List` が最新の要素を読む、古い `Text()` の値が不変 |
| 16 | `TestSymbolIdLimit` | — | 子プロセスで counter を上限の直前にし、最後の 2 つの id、その後 2 回とも panic して 0 も過去の id も出さない、既存の id が不変 |
| 17 | `TestSymbolIdAndPrivateNames` | — | 反復・16 goroutine の並列の初回呼び出しが同じ gid、別 symbol は別の非零 gid。2 file × 3 class (入れ子、継承、同じ `#x`、static / instance、method) の private 名が `#<gid>@name` で、Members / Exports の raw key から宣言の symbol が引け、12 個が全部別。2 file の class の local SymbolId が同じ (衝突の条件) ことを assert。bind の順を逆にしても同じ |

Z1 §7 の受け入れ条件のために足したもの (`store/zero_test.go`): `Node` の size 16・`s` offset 0・`h` offset 8、view ↔ Node の変換、`KindUnknown` の実ノードを Factory が拒否、比較不能の compile-fail 7 件 (`==`、view の `==`、map の key、Node を含む struct / 配列の key、comparable 型引数、`slices.Index`) を `go build -overlay` で確かめる (エラー文が比較不能であることまで確認)。

## 8. 指示書に書かれていない判断 (報告 (h))

実装は止めずに進めた。1〜3 は後段に影響するので判断をお願いしたい。

1. **source の Store の program の token**: §3.6 は「source の Store は Builder が `space` を設定する」とするが、§4 の 1 の `NewBuilder(src, id StoreID)` には space の引数が無く、storeparser の `ParseSourceFile` は StoreID も取らない。production の API は足さず (storeparser は StoreID 0 と nil の space のまま)、storage テストだけが export_test の `SetSourceIdentity` で与える。program が StoreID と space を渡す入口は 7d-2 (H1 の adapter) で決める必要がある。 **2026-09-28 に解決**: 検証で指示書 §3.6 の未実装 (P1) と判定されたので、`NewBuilder(src, space, id)` / `Reset` が space を受け取り、`storeparser.ParseSourceFileIn(space, id, …)` を足した (`ParseSourceFile` は `(nil, 0)`、program 外の Store)。`SetSourceIdentity` は削除し、storage テストは production の入口で parse する。検証報告 §12。
2. **nil の `Flow` の `==`**: V1 の `Flow{bound, ref}` は非 nil なら正規だが、source から読んだ nil (`Flow{b, 0}`) と `Flow{}` は `==` で一致しない。checker は `map[*ast.FlowNode]bool` を持つので `Flow` は比較可能のままにし、doc comment に「nil は `IsNil` で判定」と書いた。7d-2 の変換規則 (flow どうしの `==` を `a == b \|\| a.IsNil() && b.IsNil()` にするか、解決層で nil を正規化するか) が要る。 **2026-09-28 に B 案で解決**: resolver が nil を全部 `Flow{}` に揃える (各 resolver で 0 を判定、`flowAt` は分岐なしの選択)。`==` と map の key は nil も Pointer と同じ意味になり、変換規則は要らない。検証報告 §12。
3. **RTA の動的な辺**: RTA は program 全体で address を取られた関数を動的呼び出しの callee にするので、`FindAncestor` の `callback(node)` から `newParentInChildrenSetter` (node に書く)、`collectIdentifiersForSourceFile`、`IsDecorator` が入った。変換対象の中で値として名前を参照された関数は別に拾っているので、動的な辺はそれに限った (3 関数を落とした。出力の header に記録)。静的な辺と init を根に入れた RTA の graph はそのまま使う。 **2026-09-28 に解決**: RTA から使っていたのは静的な辺だけ (動的な辺は捨てて値参照で代えていた) なので、RTA と SSA をやめ、関数本体に出てくる ast の package-level 関数の識別子 (呼び出しと値の両方、`TypesInfo.Uses`) を辿る構文上の到達計算に置き換えた。到達集合 176 と `utilities.go` は header の注記以外 byte 一致、binder 試作の比較と problems も一致。取りこぼしは「出力自身が名前を書く / 呼び出し側の訳が名前を書く (無ければコンパイルエラー) / field・変数から読む (Pointer の型のままでコンパイルエラー)」のどれかで、黙った意味の差にならない。検証報告 §12。
4. `FlowList` の名前は解決層に使い、slab の行の型を `FlowListEntry` に改名した (§3.5 の API 名と衝突するため)。
5. export された kind だけの ast 関数は訳さず `ast.X` を呼ぶ (7c の流儀)。
6. `GetSymbolId` と `IDSpace.Next` の counter は Pointer の `Add` ではなく CAS の loop で、上限では counter を動かさずに panic する (周回で 0 や過去の番号を出さないため。§6.2 の 16 の要求を満たす形)。
7. 変換規則を §5.1 の表に 5 つ足した (§6.1 の「足した」)。どれも型と Pointer の定義から決まる。
8. kind 多相の子 accessor の生成の形を変えた (§2 の 3)。
9. Factory: Store に slot の無い member は引数にしない。raw の node list (`SyntaxList.Children`、`JSDocTypeLiteral.JSDocPropertyTags`、Pointer は `[]*Node`) は `List` を取る。
10. Z1 §4 に従い、binder の header writer (`AddFlags` / `ClearFlags` / `SetFlowRef` / `SetSymbolId`) にも通常 build で番兵への書き込みの拒否 (kind の比較 1 回) を入れた。Bind KPC に乗る。
11. `Store.bound` は `NewBound` / `NewBoundIn` の時点で設定する (bind の開始時)。解決層は storechecks build で Seal 前の読みを panic させる (A1)。
12. `List` は比較可能のまま (`SameList` を足した)。Z1 は Node だけを対象にしている。
13. 複合 key の規則は `collections.Set` に加えて `OrderedSet` / `SyncSet` の要素と `OrderedMap` / `SyncMap` の key も起点にする。
14. storebinder の §4 以外の hunk (Z1 の `SameNode` 5 か所と `AppendIfUnique`、U2 の 2 か所)。

## 9. 限界

- KPC の帰属 (§5) は、Walk は objdump で確かめたが、Bind と Parse の増分は inline 判定と差の大きさからの推定で、関数別の命令数では確かめていない。KPC 中の load average は 2.2〜4.1 で線の 1.5 を切っていない。
- retained の live heap profile による監査、inline の call site と objdump の突き合わせはしていない (検証指示 §3、§5)。
- binder.go の試作の hunk 分類はキーワードの自動分類で、7c 報告の数 (a 24、b 14、c 15、d 8、f 3) とは一致しない。
- `storeparser` の `FuzzEquivalence/a708303f611b95c8` は HEAD から落ちている (本変更と無関係)。

## 10. 再現

```sh
cd $ROOT && node tools/scripts/tsc/generate.ts && (cd tools && go run ./cmd/storetranslate) && git status --short
cd $ROOT/tools && go run ./cmd/storetranslate -binder $TSC/internal/ast/docs/_store-checker-7d0-impl/binder-trial
cd $TSC && go build ./... && go vet ./internal/ast/... ./internal/storeparser/... ./internal/storebinder/...
cd $ROOT/tools && go vet ./cmd/storetranslate/... && go test -count=1 ./cmd/storetranslate/
cd $TSC && go test -count=1 ./internal/ast/store/... ./internal/storeparser/... ./internal/storebinder/...
cd $TSC && go test -tags storechecks -count=1 ./internal/ast/store/... ./internal/storeparser/... ./internal/storebinder/...
cd $ROOT/tools && go run ./cmd/storetranslate -hash GetLocals   # P2 の hash の行
```
