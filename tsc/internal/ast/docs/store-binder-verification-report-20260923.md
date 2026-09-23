# Store binder (TODO 7c) 検証報告

作成日: 2026-09-23。指示書は [store-binder-verification-instructions-20260922.md](store-binder-verification-instructions-20260922.md) (以下「検証指示」)、対象は [store-binder-implementation-report-20260923.md](store-binder-implementation-report-20260923.md) (以下「実装報告」) の実装 (commit 2dfabb31e6 + 未コミットの 7c 差分)。設計は [store-binder-design-20260922.md](store-binder-design-20260922.md) (「7c 設計」)。生の出力は `_store-binder-results/` (§10)。

## 1. 結論

**合格。** 正しさは corpus 17,085 file で mismatch 0、Bind ゲート (cycles/node ≤ 1.10 × Pointer) は **1.025**、32B header の Walk / Parse は 24B と同等以上、retained と GC (CPU) は提案線の内側。次は 7d (checker の設計) に進める。ユーザーの決定 (2026-09-23): **JS の除外 330 file は受け入れ、7d で storeparser に JSDoc reparse を足して解消する** (§3.2 / §3.4 の緩めも同じ原因で、同時に消える)。`bindChildren` の kind switch の表化は 7c では行わない (§5)。GC の線は mark の CPU で引く。slab の成長は `Bound` の scratch + Compact (e9a5942778) で決着。

**計測の時点 (2026-09-23 追記)**: §6 と §7 の数字は e9a5942778 (Bound の scratch + Compact) の**前**のバイナリで取った。Compact は bind の末尾に slab を exact に copy するので Bind の cycles がわずかに動きうる。Compact 後の Bind KPC (HEAD 225f56d491、`kpc-bind-compact-20260923-1904.txt`、5 run 平均):

| | pointer | store | 比 | Compact 前の比 |
| --- | ---: | ---: | ---: | ---: |
| cycles/node | 112.84 | 111.06 | **0.984** | 1.025 |
| inst/node | 372.0 | 405.5 | 1.090 | 1.129 |
| IPC | 3.30 | 3.65 | | |
| B/op | 7.42 MB | 6.51 MB | 0.88 | 1.92 |
| allocs/op | 13,952 | 12,646 | 0.91 | 0.91 |

Pointer は Compact 前と同じ (cycles 112.5 → 112.8、inst の run 幅 0.46%)。Store は cycles −3.7%、inst −13 inst/node で、append 成長の `growslice` / copy が消えた分。KPC の Session は GC オフで pool の scratch が温存される契約なので、GC ありで pool が消える経路 (Compact のコミットで B/op +14%) はこの数字に出ない。**Bind ゲートは Compact 後も合格 (0.984)。**Compact のコミットの計測では B/op は GOGC=off で 13.56 → 7.18 MiB (Pointer 7.08)、GC ありでは pool が消えて +14%、retained は −1.6%。

| 指標 | 線 | 実測 (store / pointer) | 判定 |
| --- | --- | ---: | --- |
| Walk cycles/visit (32B) | ≤ 1.15、24B の同時測定と並べる | **1.137** (29.28 / 25.75)。同セッションの 24B は 1.167 | 合格。24B より良い |
| Parse cycles/op (32B) | ≤ 1.00 | **0.824** (55.93M / 67.85M)。24B の before は 0.822 | 合格。不変 |
| **Bind cycles/node** | **≤ 1.10** | **1.025** (115.3 / 112.5)。Compact 後 **0.984** (111.1 / 112.8) | **合格** |
| Bind inst/node、IPC | 報告 | 418.8 / 370.8 = 1.129、IPC 3.63 / 3.30 | inst +12.9% を IPC が吸収 |
| Bind B/op、allocs/op | 報告 | 14.22 MB / 7.42 MB (1.92×)、12,754 / 13,952 (Compact 前) | slab の append 成長。Compact 後は GOGC=off で Pointer 並み (上の追記) |
| retained B/node (parse+bind) | checker.ts ≤ 0.60、fixtures ≤ 0.70 (提案線) | checker.ts **0.561** (61.4 / 109.4)、fixtures **0.583** (72.6 / 124.6)、dom 0.696 (89.4 / 128.4) | 提案線に対して合格 |
| 保持中の GC 1 回、store / pointer | **mark の CPU で引く (ユーザー決定)**。checker.ts ≤ 0.25、fixtures ≤ 0.35 | CPU: checker.ts **0.10** (1.9 / 19.2 ms)、fixtures **0.16** (13 / 82 ms)。wall (`gc-ms`) は checker.ts 0.263、fixtures 0.231、dom 0.654 | 合格 |

Pointer の inst の幅は Bind 5 run で 0.8% (110.30〜111.16M)、線の 1.5% 以内なので測り直しは不要。

## 2. 環境と入力

- MacBook Air (M1、MacBookAir10,1)、go1.27.1 darwin/arm64、HEAD 2dfabb31e6 + 7c の未コミット差分 (`git status` は §10)。
- KPC 4 本 (`kpc-walk-24b` / `kpc-walk` / `kpc-parse` / `kpc-bind`、00:46) はユーザーが検証指示 §4 のとおり取ったもの。バイナリの mtime (00:46) が最終ソース (binder.go / utilities.go 00:30、doc.go 00:33、generator 00:00) より新しいことを確認した。そのときの load average は記録が無い。
- ns / retained / gctrace は本セッションで取った (00:53〜01:00、load average 1.8〜4.2。ns は参考値で、判定は KPC)。
- source の sha256 (35 file: `storebinder/*.go`、`storeparser/*.go`、`ast/store/**/*.go`、`generate-go-store.ts`) は計測前後で不変 (`sha256-before.txt`)。実装は直していない。
- 入力: checker.ts (Store 298,884 node、Pointer 302,587)、dom.generated.d.ts、corpus 17,415 file (fixtures + compiler / conformance の unit)。

## 3. 正しさ (検証指示 §1)

### 3.1 ガード

全部通った (`sec1-guards.txt`): generator は再現 (`git status` 不変、sha 不変)、`go build ./...`、`go vet` 3 package、`git diff --stat 326bc57b8a -- internal/binder internal/parser internal/scanner internal/ast ':!internal/ast/store' ':!internal/ast/docs'` は空、production から store 系 package の import なし。`doc.go` は package comment に 7c 設計へのパス・機械的移植・出力の置き場・範囲外・移植規則を持つ。

テスト: tag なし / `-tags storechecks` の両方で store / storeparser / storebinder が通り、`internal/ast` / `parser` / `binder` も通る (`test-notag.txt`、`test-storechecks.txt`、`test-pointer-pkgs.txt`)。`TestSealPanics` は AddFlags / ClearFlags / SetFlow / SetSymbol / SetLocalsSlot / typed setter の 6 本。

### 3.2 corpus と除外

| 項目 | 値 |
| --- | ---: |
| file 数 | 17,415 (7b と同じ)、Store 2,584,467 node |
| 比較した file | 17,085 (TS 15,959 + JS 1,126) |
| 除外 (JS) | 330 = 設計 §2.1 の規則 293 ∪ `@import` の specifier 57 ∪ expando / require の Reparsed 型注釈 11 ∪ JSDoc cast 48 |
| parse mismatch | 0 |
| `Declarations` / `ValueDeclaration` の `Ref.file` 検査 | 708,382 Ref、不一致 0 |

7b の await 8 file: #713、#714、#3037、#7521、#12580、#12584 (TS) は除外されず比較され一致。**#5255 と #8555 は JS** で、JSDoc 由来の理由 (#5255: reparsed symbol + JSDoc cast、#8555: reparsed symbol) で除外されている。await が理由ではなく、設計 §2.1 の 293 の規則でも除外される file。

除外の内訳は `TestCorpus` が理由ごとに file 番号を log する (`test-notag.txt`)。設計の 293 に足した 37 file を含む 330 の除外は受け入れる (ユーザー決定)。原因は Store parser が JSDoc 由来のノードを持たないことで、7d で storeparser に JSDoc reparse を足して除外を無くす。

### 3.3 parse の等価 (7b)

32B + reparse 有効 + indicator ありで `storeparser.TestCorpus` は mismatch 0 (17,415 file、2,576,310 node 訪問、dead 0.87%)。診断の差は 7b と同じ JS 10 file の code 1003 (Store 側が subset) だけ。await 8 file の除外は外れている。

### 3.4 bind の等価: driver が比べるもの

`bindequiv_test.go` を実装指示書 §6.1 と照合した。全項目がある: flags (`MaskFlags`)、symbol (Name 正規化 / Flags / CheckFlags / Declarations の (kind,pos,end) 列 / ValueDeclaration / Parent / ExportSymbol / Members / Exports、対の memo は両向き)、LocalSymbol、Locals (不在 kind の一致 + key 集合 + 値)、flow (node / EndFlowNode / ReturnFlowNode / FallthroughFlowNode から Flags / Node / SwitchClause の (switch, start, end) / ReduceLabel の target と antecedents / Antecedent / Antecedents の長さと順序、memo 両向き)、file (SymbolCount / Symbol / GlobalExports / PatternAmbientModules / bind diagnostics の (pos,len,code,message) / CommonJSModuleIndicator / ExternalModuleIndicator / Imports / ModuleAugmentations / AmbientModuleNames / UsesUriStyleNodeCoreModules)。指示書に無い `NextContainer` 連鎖 (`Containers`) と `Ref.file` の検査も入っている。

名前の正規化は private 名 `#<id>@` と `pattern@<id>` の 2 種だけ。**ただし id の正規化以外に緩めが 2 つあり、実装報告に書かれていない**。一時テスト (`zz-verify.txt`) で件数を取った:

| 緩め | 対象 | 影響 |
| --- | --- | --- |
| JS file の `MaskFlags` に **`ThisNodeOrAnySubNodesHasError`** (bind が書く flag) が入っている (7b の mask + これ)。理由はコメントのとおり、Pointer の JSDoc reparse が Store に無いノードに `ThisNodeHasError` を立て、binder が祖先に伝播するため | JS 1,126 file | mask を外すと **3 file** (#5083、#13393、#13522) で Flags が不一致。7b 由来の `PossiblyContainsDynamicImport` も外すとさらに 15 file |
| `Imports` / `ModuleAugmentations` の比較で Pointer 側の JSDoc 由来ノード (`JSDoc|Reparsed`) を落とす。`ExternalModuleIndicator` が JSDoc 由来なら比較しない | JS | Imports の要素 **31 個**を落とした。ModuleAugmentations 0、indicator の読み飛ばし 0 file |

TS 15,959 file は mask なし (`Options{}`) で厳密。どちらの緩めも「Store に無いノードの影響」に限定され機械的。`ThisNodeOrAnySubNodesHasError` の 3 file は Pointer parser が JSDoc から作った空の Identifier (`@type {@import("a").Type}`、`@type {?}`、名前なし `@implements`) に `ThisNodeHasError` が立ち binder が祖先へ伝播したもので、binder ではなく Store parser に JSDoc ノードが無いことが原因。7d の JSDoc reparse で mask ごと消える。実装報告 §7 には追記が要る。

### 3.5 flow slab の個数

`TestFlowSlabs` の値は実装報告 §8 と一致: checker.ts flows 79,991 / flowLists 49,789 / flowData 879 (番兵込み) に対し 7c 設計 §2.1 の到達可能数 45,694 / 15,402 / 878、dom 4,846 / 1 / 1 に対し 4,841 / 0 / 0。差は到達不能な label と、`finishFlowLabel` が 1 antecedent に畳んだ label / list (Pointer も同数を arena に作るが到達可能数には数えない)。

### 3.6 `store` に足した API と sizeof

実装指示書 §3 の一覧と現物 (`store.go` の diff、`bound.go`、`symbol.go`、`file.go`、`position.go`) を照合した。一覧のものは全部あり、増えたものは実装報告 §4 の 7 件 (`NewBound`、`FlowCounts` / `Footprint`、`IsStatic(st)` / `SymbolName(st, …)`、`GetLocals(bound, container)`、`IsModuleAugmentationExternal(file, …)` 等の file 引数 3 本、非公開 `nilNode`、`kperf.Session.Totals`) で、報告と一致。

- `unsafe` は `Node.Ref()` (store.go:111-112) と `List.Refs()` (store.go:174) だけ。
- `Builder.SetFlags` / `SetLoc` は無い。
- 一時テスト: `unsafe.Sizeof(NodeHeader{}) == 32`、`Symbol{} == 96`、`FlowNode{} == 16` (FlowList 8、FlowData 12、Node 16、Store 88)。
- `sealed` は `checkUnsealed` (cost 0、`{}` に inline) 経由でしか読まれず、`storeChecks` 無しの build では読まれない。

## 4. 移植の機械性 (検証指示 §2)

### 4.1 関数の名前と順序

`binder.go`: 名前と順序は Pointer と同じ。増えたのは `nilNode`、`symbolOf`、`newFlowData` の 3 つ、`setFlowNodeReferenced` と `getInitializerSymbol` は method 化 (位置は同じ)。`references.go`: Pointer の 2 関数の後に `ast` から移した 10 関数。`symbol.go`: `Id`、`GetSymbolTable`、`GetMembers`、`GetExports`、`GetLocals` の 5 つが増え、`InternalSymbolName*` 定数は移していない。

### 4.2 hunk の分類

`binder-verbatim.diff` (64 hunk) を全部読んだ。分類は実装報告 §1 の表と矛盾しない: (a) 型、(b) 出力先、(c) 子の受け直し、(d) flow の index 化が大部分で、(e) は 0 (`KindJSTypeAliasDeclaration`、`IsImplicitlyExportedJSDocDeclaration`、`NodeFlagsJSDoc` の分岐は残っている)。Pointer にも効く書き換え (`checkContextualIdentifier` の条件順など) は入っていない。

(f) は実装報告 §2 の 40 項目に header の読み回数が Pointer / Store で並べて書かれている。現物と照合して差し戻すものは無い。表に**無い**小さな書き換えが 3 つあった (いずれも header の読みは同数):

| 箇所 | 内容 | Pointer | Store |
| --- | --- | --- | --- |
| `store/utilities.go` `getModuleInstanceStateForAliasTarget` | `PropertyNameOrName()` → `PropertyName()` が nil なら `Name()` (Store に役割 accessor が無い) | kind 1 + field | kind 表 2 + extra ≤2 |
| `store/utilities.go` `GetExternalModuleName` (CallExpression) | `core.FirstOrNil(Arguments())` → `Len() > 0 && At(0)` | 0 | list 読み 1 |
| `store/utilities.go` `IsForInOrOfStatement` | `node != nil` の検査を外した (番兵の kind は 0 で false になる) | 0〜1 | kind 1 |

### 4.3 (g) 読みのまとめ

`binder-fold.diff` (41 hunk) を 1 箇所ずつ見た。まとめた変数は kind / Parent / 子 (left、right、type、operatorToken、expression、statement、label、initializer、catchClause、finallyBlock、caseBlock、exportClause、variableDeclaration、name) / operator / nameText だけで、`Flags()`、`Symbol()`、`FlowNode()`、`LocalsSlot()`、`file` / `Bound` の field は一つも無い。`setContinueTarget` と `isTopLevelLogicalExpression` のループの書き換えは元と同じ順で親を辿る。`bindLabeledStatement` は folded した `label` に `AddFlags` するが、`Node{s,h}` の h は bind 中に動かないので正しい。

### 4.4 `store/utilities.go` と `internal/ast/utilities.go`

関数ごとに並べて diff した (`utilities-funcdiff.txt`、165 関数: ast にあるもの 115、`Kind() ==` の 1 行述語 47 と `IsLocalsContainer` 等)。順序は ast と同じ (逆転 1 は `ast.go` から来た `IsLocalsContainer` を末尾に置いたため)。分岐は同じで、違いは実装報告 §2 の #33 (`IsOuterExpression` の JSDoc 枝)、#34 (`allowAccessedRequire` の panic)、#35 (`map[NodeRef]`)、#36 (`AsIdentifier().Text()`)、§3 の (g) 8 関数、§4.2 の 3 件だけ。`references.go` の 12 関数も同様 (`walkTreeForJSXTags` の `SubtreeFacts` 枝刈りが無いだけ)。`GetNodeAtPosition` は ast 版から JSDoc の枝を除いたもので、`nodeContainsPosition` は同じ。

## 5. API と命令列 (検証指示 §3)

| 対象 | 期待 | 結果 |
| --- | --- | --- |
| `Node.Ref()` | 乗算なし、`LSR $5` | storebinder の全関数で `LSR $5` が 16 箇所。`MUL` / `UMULH` は 19 箇所あるが全部 `List.Refs()` の `unsafe.Slice` の長さ検査 (store.go:174) で、`Ref()` には無い |
| accessor の inline | can inline、`sealed` 読みなし | `FlowNode` / `Symbol` / `SetFlow` / `SetSymbol` / `AddFlags` / `ClearFlags` / `FileRef` / `Ref` / 役割 accessor 5 本 / 役割 setter 5 本 / `Store.Node` / `Bound` の読み書き 11 本は全部 can inline。`GetLocals` だけ cost 181 で cannot (実装報告どおり) |
| `bind` | `node()` の形は子と親 1 段以外に無い | `ADD R<<5` (node()) は **2 箇所**、どちらも親 1 段: **`IsObjectLiteralMethod` の `Parent()`** (MethodDeclaration / MethodSignature の case、binder.go:704 に inline) と **`IsPartOfTypeQuery` の `Parent()` ループ** (QualifiedName の case、binder.go:649)。検証指示が挙げる `IsIdentifierName` は `checkContextualIdentifier` の中で、それは `bind` から CALL される別関数なので `bind` 本体には出ない。`GetContainerFlags` も CALL |
| `bindChildren` | 同上、kind switch がジャンプテーブル | node() は `EndOfFileToken` と `bindEachChild` の子走査だけ。**switch はジャンプテーブルではなく `CMPW` の二分探索連鎖 (43 本)**。Pointer 版の `bindChildren` も同じ形 (42 本、`asm-bind-children-pointer.txt`) なので両者に差は無いが、検証指示の期待は両側とも満たさない。kind → 分岐番号の `[256]uint8` 表で解決できるが、**7c では実施しない (ユーザー決定、2026-09-23)** |
| `bindContainer` | save / restore が 2 word | save は `LDP` + `MOVD` ×2、restore は `MOVD` ×2 + `STP` (Binder は heap にあり `Node` が pointer を持つので write barrier 付き。Pointer の `*ast.Node` と同じ) |
| `addAntecedent` | ループが base + index、growslice は append だけ | ループは `LSL $3` (FlowList 8B)、`growslice` は `NewFlowList` (bound.go:89) の 1 箇所 |
| `nameSlot` 型の表引き | check_bce の報告に無い | 役割表 5 本 + `nameSlot` の 53 行に bounds check の報告なし |
| 7a の accessor の inline 判定 | 7a / 7b と同じ、cannot が増えていない | `Node.*` accessor の cost は 7b の `_store-parser-results/inline-store.txt` と全て同じ。`Builder` の constructor 40 本と `NewBuilder` は 7b の一覧では can、今は cannot (`NewReturnStatement` は cost 81、予算 80) だが、**原因は 32B ではなく S1 (205b6bb15b、Builder に Store を埋め込み `b.nodes` → `b.s.nodes`)**。7b の一覧は S1 の前に取ったもので、`b.s.` の ODOT 1 段が constructor 内 3〜7 箇所で効き 1-child 形が 74 → 81 になった。24B の HEAD でも同じで、header に列を足しても複合リテラルに書かない限り cost は変わらない (2026-09-23 訂正。初版は「header の零 field が増えた分」としていた)。Parse の inst は 24B と同じ (214.32M vs 214.43M) |

## 6. bind の費用 (検証指示 §4)

同じ binary の pointer に対する比 (5 run 平均、`kpc-*-20260923-0046.txt`):

| ベンチ | pointer | store (32B) | 比 (cycles / inst) | 24B (同セッション / before) |
| --- | ---: | ---: | ---: | ---: |
| Walk | 7.675M cycles (25.75/visit)、21.58M inst、IPC 2.81 | 8.727M (29.28/visit)、26.30M (88.2/visit)、IPC 3.01 | **1.137** / 1.219 | 1.167 / 1.260 (同セッション)、1.157 / 1.260 (before) |
| Parse | 67.85M cycles、261.67M inst、IPC 3.86 | 55.93M、214.32M、IPC 3.83 | **0.824** / 0.819 | 0.822 / 0.825 |
| Bind | 33.54M cycles (112.5/node)、110.53M inst (370.8)、IPC 3.30、7.42 MB、13,952 allocs | 34.38M (115.3/node)、124.81M (418.8)、IPC 3.63、14.22 MB、12,754 allocs | **1.025** / 1.129 | |

- **Walk**: 32B は 24B より cycles −1.9%、inst −3.2% (27.18M → 26.30M、≈3 inst/visit)。`node()` の index 計算が 24B の 2 命令 (×24) から 32B の shift 1 本になった分で、7a の 1.137 に戻った。7a と before の食い違い (1.137 vs 1.157) は 24B 側の再測定でも 1.167 なので、24B 自体が 7a より遅く測れており原因は未分離のまま。
- **Parse**: cycles / inst は不変。B/op は 12.72 MB → 15.97 MB (+3.25 MB)。`Finish` の copy が 8 B/node で 2.39 MB、残り 0.86 MB は bound slot (40 kind に 1 word) の `extra`。ns ベンチの B/op (12.5〜13.0 MB) は KPC ベンチ (15.97) と pool の水準が違うので、KPC 同士 (7b 12.72) で比べる。
- **Bind**: cycles 1.025 で合格。inst は +12.9% (+48 inst/node) だが IPC が 3.30 → 3.63 に上がり cycles に出ない。inst の内訳は取っていない (ゲートを通ったので検証指示 §4 の切り分け (1)〜(5) は不要と判断)。§5 のとおり `node()` は辺ごと + 親 2 段で、`Ref()` の動的回数は測っていない。
- ns (参考、load 2.3): Bind checker.ts 11.28 / 12.03 ms = 0.94、dom 3.97 / 4.61 = 0.86。Walk 2.73 / 2.42 = 1.13、Parse 18.05 / 24.41 = 0.74。

## 7. retained heap と GC (検証指示 §5)

`BenchmarkStoreBindRetainedV1` 5 run (`retained.txt`、値は run 間で同一):

| 入力 | Pointer B/node (MB) | Store B/node (MB) | 比 (B/node / MB) | GC ms Pointer / Store | 比 |
| --- | ---: | ---: | ---: | ---: | ---: |
| fixtures (166 file) | 124.6 (136.8) | 72.6 (78.1) | 0.583 / 0.571 | 12.17 / 2.82 | 0.231 |
| checker.ts | 109.4 (33.1) | 61.4 (18.3) | 0.561 / 0.554 | 3.017 / 0.794 | 0.263 |
| dom | 128.4 (14.6) | 89.4 (9.9) | 0.696 / 0.677 | 1.395 / 0.912 | 0.654 |

B/node の分母は各側の NodeCount (Pointer は JSDoc ノード込み、Store は dead 込み) なので MB の比も並べた。実装報告 §8 の 1 run の値と一致。

gctrace (テストバイナリ直接実行、最後の forced GC):

| 入力 | 側 | live | GC clock | mark cpu (assist / background / idle) |
| --- | --- | ---: | ---: | --- |
| checker.ts | Pointer | 35 MB | 2.7 ms | 0 / 5.2 / 14 ms |
| checker.ts | Store | 21 MB | 0.75 ms | 0 / 0.93 / 0.95 ms |
| fixtures | Pointer | 143 MB | 10 ms | 0 / 20 / 62 ms |
| fixtures | Store | 87 MB | 2.3 ms | 0 / 4.1 / 8.9 ms |

GC の wall (`gc-ms`) は 8 P の並列 mark の時間で、checker.ts の 0.263 は提案線 0.25 をわずかに超えるが、mark の CPU では 1.9 / 19.2 ms = **0.10** (fixtures 13 / 82 = 0.16)。見積り (0.14〜0.16) に合うのは CPU の方。線は CPU で引く (ユーザー決定) ので合格。

Footprint との照合 (checker.ts、Store 側): Store 12.34 MB (headers 9.54 + extra 2.79 + texts 0.01) + `Bound` の slab 1.93 MB + symbol 18,444 × 96 = 1.77 MB + SymbolTable 5,917 個 / 17,305 entry ≈ 0.90 MB + `Declarations` の cap 0.15 MB = 17.09 MB。retained 18.34 MB との差 1.25 MB (6.8%) は size class、map の内部、Arena chunk の余り、`File`。B/node で書くと 41.3 (Store) + 6.4 (Bound) + 5.9 (symbol) + 3.0 (表) + 0.5 (宣言) + 4.2 (残差) = 61.4 で、7c 設計 §5 の見積り 52.8 + T (10〜15) の内側。dom は 8.78 MB に対し retained 9.87 MB (差 11%)。

## 8. `File` の field と parser 側の出力 (検証指示 §6)

corpus 17,085 file で `ExternalModuleIndicator`、`Imports`、`ModuleAugmentations`、`AmbientModuleNames`、`UsesUriStyleNodeCoreModules`、`CommonJSModuleIndicator` の不一致 0 (§3.4 の driver、JSDoc 由来の Imports 要素 31 個を除く)。カバレッジ: Imports あり 2,542 file、ModuleAugmentations あり 213、AmbientModuleNames あり 244、indicator あり 5,572、UsesUriStyleNodeCoreModules が unknown 以外 41、CommonJS 261。

`walkTreeForJSXTags` の枝刈り無しの費用: TSX / JSX で比較した 486 file のうち import / export の無い (indicator が nil の) file は **214**。ただし corpus の parse option は `SourceFileParseOptions{FileName, Path}` だけで `ExternalModuleIndicatorOptions.JSX` が false なので、この走査は corpus では 1 度も動いていない。jsx: react-jsx 相当の option で初めて 214 file が全走査になる。

## 9. 設計文書への反映案

7c 設計:
- §2.1 の除外規則は実装の 4 理由 (330 file) に広げる (決定)。7d で storeparser に JSDoc reparse を足し (JSDoc ノードを parse の Store 本体に持つ形)、除外と JS の mask を無くす。`@import` の specifier、`@type` の expando / require、JSDoc cast、`@implements` がその対象。
- §2.2 の「`Ref()` は 0.25〜0.3 回/node」は未計測。ゲートを通ったので実測は不要だが、命令列上 `Ref()` の site は 16。
- §2.3 の flow slab: append 成長で B/op 1.92× だった (時間は不変)。`Bound` の scratch を pool の Binder が持ち、bind の末尾に exact copy する形 (e9a5942778) に決めた。GC ありでは pool が 2 GC で消えるので効果は GOGC に依存する。
- §5 の見積り: retained 0.54 → 実測 0.56 (checker.ts)、0.63 → 0.70 (dom)。GC は wall で 0.26 / 0.65、CPU で 0.10。

store-ast-design:
- 32B header の実測: Walk 1.137 (24B 1.167)、Parse 0.824 (不変)、Bind 1.025。`node()` の index が shift 1 本になり Walk の inst が −3.2%。
- `Builder` の constructor の inline 喪失は S1 が原因で、32B とは無関係 (§5)。generator の `data :=` ローカルをやめると 41 本が戻る (cost 76) が、7c の範囲外。
- (f) の header 読み回数の表 (実装報告 §2) は現物と合う。`bind` に inline された親 1 段の `node()` は `IsObjectLiteralMethod` (MethodDeclaration の経路) と `IsPartOfTypeQuery` (QualifiedName の経路) の 2 つで、Identifier の経路 (`IsIdentifierName`) は `checkContextualIdentifier` の CALL の先。
- `bindChildren` の kind switch は Pointer / Store とも二分探索の比較連鎖で、ジャンプテーブルではない。`[256]uint8` の表で解決できるが 7c では行わない (ユーザー決定)。

## 10. 限界

- KPC 4 本はユーザーが本セッション前に取った。バイナリとソースの整合は mtime でしか確認していない (sha は取っていない)。load average の記録なし。
- ns / retained / gctrace は load average 1.8〜4.2 で取った。判定は KPC と retained の bytes (run 間で同一) で、ns と GC の wall は参考値。
- Bind の inst +12.9% の内訳は取っていない。`Ref()` の動的回数も未計測。
- 検証指示 §4 の before の Walk (7a 1.137 vs before 1.157) が再現しない理由は未分離のまま。
- JS の等価は §3.4 の mask と Imports の読み飛ばしに依存する。TS は厳密。
- `walkTreeForJSXTags` は corpus で動いていない (§8)。
- hunk の分類の個数 (実装報告 §1 の表) は独立に数え直していない。64 hunk 全部を読み、分類の内容と (f) / (g) の一覧に矛盾が無いことを確認した。
- retained の Footprint 照合の map の大きさは概算 (24 B/entry ÷ 0.8 + 64 B/表)。

## 11. 再現手順と生データ

`OUT = tsc/internal/ast/docs/_store-binder-results/`:

| file | 内容 |
| --- | --- |
| `env-verify-20260923.txt`、`sha256-before.txt` | 環境、source の sha256 (終了時に diff で不変を確認) |
| `sec1-guards.txt`、`test-notag.txt`、`test-storechecks.txt`、`test-pointer-pkgs.txt` | §3 のガードとテスト (corpus の除外 file 番号は `test-notag.txt`) |
| `sec2-func-order.txt`、`binder.diff`、`binder-verbatim.diff`、`binder-fold.diff`、`references.diff`、`symbol.diff`、`utilities-funcdiff.txt` | §4 (utilities の関数単位 diff は scratchpad の `funcdiff.py` で生成: ast と store の `func` を名前で対応させて `difflib`) |
| `inline-store.txt`、`inline-binder.txt`、`bce-store.txt`、`asm-*.txt`、`asm-storebinder-all.txt`、`asm-bind-children-pointer.txt`、`sec3-asm-summary.txt`、`sec3-asm-detail.txt` | §5 |
| `kpc-walk-24b-20260923-0046.txt`、`kpc-walk-…`、`kpc-parse-…`、`kpc-bind-…`、`*.kperf.test` | §6 (ユーザー実行) |
| `bind-ns.txt`、`walk-ns.txt`、`parse-ns.txt` | §6 の ns |
| `retained.txt`、`gctrace-{store,pointer,checker-store,checker-pointer}.txt` (+ `.stdout.txt`) | §7 |
| `zz-verify.txt` | §3.2 / §3.4 / §7 / §8 の一時テスト (`storebinder/zz_verify_test.go`、実行後に削除) の出力 |

コマンドは検証指示 §1〜§5 のとおり。一時テストは `bindequiv_test.go` の `reparsedEffects` / `excluded` / `BindEquivalent` と `binder_test.go` の `corpus` を呼び、mask を変えた `BindEquivalent` で Flags の不一致 file を数えた。
