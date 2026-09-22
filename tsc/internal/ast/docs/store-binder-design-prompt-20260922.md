# TODO 7c (Store binder) 設計セッションのプロンプト

作成日: 2026-09-22。新しいセッションの最初のメッセージとして貼る。対象は設計を対話で決めるエージェント。

---

あなたの役割は、Store AST (`tsc/internal/ast/store`) の上で binder を動かす TODO 7c の**モジュール設計を私と対話で決める**ことです。実装はしません。成果物は設計文書 1 本と、7b と同じ形の実装指示書・検証指示書です。

## 進め方の規則

1. **1 ターンに 1 つの決定。** §4 の順に、問いを 1 つ出し、選択肢を表にして (何を得るか、何を払うか、どの計測で決まるか)、推奨を 1 つ添えて私の判断を待つ。決まったら設計文書に書いてから次へ進む。
2. **推測で埋めない。** 選択肢の費用が分からないときは、コードを読むか、既存の計測値を引くか、「これを測れば決まる」と実験を提案する。数字の無い形容詞 (速い、重い) で比べない。
3. **私に忖度しない。** 私の案に穴があれば反論する。私が既に決めたことは再論しない。
4. 決定は `tsc/internal/ast/docs/store-binder-design-<YYYYMMDD>.md` に、既存の設計文書と同じ文体 (決定事項・採らなかった案・不変条件・検証計画・TODO) で書く。「なぜ採らなかったか」を必ず残す。
5. 全部決まったら、`store-parser-implementation-instructions-20260922.md` と `store-parser-verification-instructions-20260922.md` を型にして 7c 用の 2 本を書く。

## 先に読むもの (この順に)

1. `tsc/internal/ast/docs/store-ast-design-20260922.md` — §1 (ゲート)、§2.5 (通貨)、§2.7〜2.9 (構築、不変条件、synth Store)、§5 (symbol / locals / links / flow の移行形)、§6 TODO 8、§8。
2. `tsc/internal/ast/docs/store-parser-verification-report-20260922.md` — §1、§3.4 (top-level await の除外)、§8、§9、末尾の「7c の前提」。
3. `tsc/internal/storeparser/doc.go` — 移植の規則。7c もこの package と同じ形で始めるかを §4 で決める。
4. `tsc/internal/binder/binder.go` (3,674 行のうち binder.go が本体)。読む観点は「ノードと file に何を書くか」: `Symbol` (container 27 箇所、node 7)、`Parent` 読み 28、`Flags` 書き 22 (ContainsThis、ExportContext、HasAsyncFunctions、HasExplicitReturn、HasImplicitReturn、ThisNodeOrAnySubNodesHasError、Unreachable)、`LocalsContainerData` / `DeclarationData` / `FlowNodeData` / `BodyData`、file の `Symbol` / `SymbolCount` / `PatternAmbientModules` / `GlobalExports` / `bindDiagnostics` / `IsBound` / `ExternalModuleIndicator` / `CommonJSModuleIndicator`。ノードをキーにする map は `GetNodeId(attributes)` の 1 箇所だけ。
5. `tsc/internal/ast/symbol.go` の `Symbol` (Declarations `[]*Node`、ValueDeclaration `*Node`、Members / Exports の `SymbolTable = map[string]*Symbol`)、`tsc/internal/ast/flow.go` の `FlowNode` / `FlowList`。
6. `tsc/internal/parser/references.go` の `collectExternalModuleReferences` と `tsc/internal/ast/parseoptions.go` の `SetExternalModuleIndicator`。7b で移植していない。

## 前提と制約

- **ゲート** (設計文書 §1 の 2): `BenchmarkASTBindKPCV1` と同じ契約で checker.ts、**cycles/node** で Pointer の 1.1 倍以内。Pointer は 33.8M cycles / 298,054 node ≈ 113.5 cycles/node (GC オフの Session、`_kpc-baselines/kpc-after.txt`)、線は約 125。inst では判定しない。
- **不変条件** (§2.8): `NodeRef` は密で parse 後不変。`extra` は `[]uint32` のまま (Store 本体は noscan)。bind 完了後は Store に書かない。`map[NodeRef]T` を hot path に置かない。
- **書いてよい場所**: bind 中の `h.flags |= …` は許可 (§2.7)。ただし Store には今 Builder 以外の書き込み API が無い (`Builder.AddFlags` のみ)。
- **前回の Store branch (b719) の教訓**。同じ穴に落ちないための事実:
  - binder の逐語訳 (binder-rewrite) は Monaco で Pointer 比 inst +21%。主因は accessor ごとの税 (NodeRef を渡して毎回 `Kind()` / `Parent()` を引き直す形)。header を 1 回読んで役割分岐する「通貨の形」で −13% の見積もり。7a の Walk は cycles 1.137 倍で合格したが inst は 1.279 倍。
  - bind 1.5 倍の内訳 (xctrace): 命令数 1.79 倍で、メモリ待ちではない。4 割は走査機構 (kind の 2 段 switch、`childAt`、`listOwner`)、side map +54M inst、`SetFlow` / `flowID` +57M。生成 walker に替えても命令数は減らなかった。
  - locals を `map[NodeRef]` にすると名前解決 (`Resolve`) 配下で 0.78G inst。`Locals()` 呼び出しの 60% は locals を持たない kind に対するもので、kind の bitset で先に弾ける。
  - `getLocals` は Monaco で 65.5k エントリの map。symbolNodeLinks は 2 段配列 (index 列 → slab) が候補。
  - bind 後に GC が辿る heap の 28% は flow 構造 (`FlowNode` 4 word + `FlowList`)。packed 化の計画はあるが未実装で、7c の範囲に入れるかは §4 で決める。
  - `checkContextualIdentifier` のように、Pointer 順を Store に持ち込むと遅い箇所と、Store 順を pointer に持ち込むと遅い箇所の両方があった。移植の順序を機械的に決めず、header を読む回数で決める。
- **7b の未完了で 7c が引き継ぐもの**: `ExternalModuleIndicator` / `CommonJSModuleIndicator` と `collectExternalModuleReferences` の移植 (binder は `IsExternalOrCommonJSModule` でスコープを決めるので必須)、それに伴う `reparseTopLevelAwait` の有効化と除外 8 file の再検証、JS の JSDoc reparse ノード (`@typedef` / `@template` 等、binder が bind する) の不在、診断の file が nil。

## 決めること (この順に)

各項目で、既知の選択肢と決め手を挙げてある。選択肢を増やしてよいが、決め手の無い選択肢は出さない。

### 4.1 範囲

- binder.go だけか、nameresolver.go / referenceresolver.go も含むか。後者は checker と LS が使うので、7c で port する理由があるなら「誰が 7c の段階で呼ぶか」を言えること。
- JS file を 7c の等価テストに入れるか。入れるなら JSDoc reparse ノードの置き場 (parse の Store に入れる / synth Store で遅延 / 7c は TS のみ) を先に決める。前回は synth Store で Monaco Bind −20〜29% だったが、checker が読む JSDoc host は 19.4k 中 3.1k だった。
- flow の packed 化を 7c に含めるか、Pointer の `FlowNode` arena をそのまま使い `node → flow` の対応だけ Store 側に持つか。

### 4.2 移植の規則 (通貨)

7b は「Pointer parser の逐語移植、型の置換だけ」で cycles 0.996 だった。binder で同じ規則を使うと +21% の前例がある。決めること:

- binder の local 変数 (`container`、`blockScopeContainer`、`thisContainer`、`currentFlow` の `Node`) の型: `NodeRef` (4B、読むたび `s.node(id)`) か `store.Node{s,h}` (16B、header は手元) か。設計文書 §2.5 の再検討と 7a の E3 単価表を根拠に、bind の hot loop (`bind` → `bindWorker` → kind switch → `bindChildren`) で header を何回読むかを数えて決める。
- 走査: Pointer の `ForEachChild` 相当を Store の生成 `ForEachChild` で行うか、§8 の「線形走査の活用」(post-order の `nodes` を 1 pass) で置き換えられる部分 (parent 設定はもう不要、`ContainsThis` 等の subtree fact の伝播、container の決定) があるか。前回、生成 walker は命令数を減らさなかったので、線形 pass にする根拠は「木を辿らない」ことでなければならない。
- 移植の記録: 7b と同じく diff を根拠にするなら、逐語でない書き換え箇所を分類 (f) として列挙する規則を保つ。

### 4.3 bind の出力をどこに置くか (モジュール境界)

これが 7c の主題。Pointer は node 内 (`DeclarationData.Symbol`、`LocalsContainerData.Locals`、`FlowNodeData.FlowNode`) と `SourceFile` の field に書く。Store では:

| 出力 | 選択肢 | 決め手 |
| --- | --- | --- |
| node → Symbol (Symbol / LocalSymbol) | (a) `[]*Symbol` 密な列 (8B/node、scan 対象、§5 の移行形) / (b) 宣言 kind だけの疎な列 + index 列 (4B/node) / (c) 予約 slot に `SymbolId` (Store は noscan のまま、`symbols []Symbol` slab) | 宣言 kind の割合 (7b の kind ヒストグラムから数える)、checker が `Symbol()` を呼ぶ回数 (accessor 動的頻度の実測 `_opcount` がある)、GC が辿る pointer 数 |
| Locals | container kind の予約 slot → `locals []SymbolTable`、不在 kind は bitset で弾く (§5 で決定済み)。決めるのは slot の持ち方 (a〜c と同じ) と `SymbolTable` を map のままにするか | `Locals()` 230 万回中 60% が不在 kind |
| node → FlowNode | symbol と同じ扱い (§5)。4.1 で packed 化を外すなら `[]*FlowNode` の列 | flow を持つ kind の割合 |
| Flags の書き込み 22 箇所 | Store に `AddFlags(id, flags)` を足す (bind 中のみ許可を型か doc で示す) / Builder を bind まで生かす | 不変条件 5 (bind 後は書かない) をどう守らせるか |
| file の field (Symbol、SymbolCount、PatternAmbientModules、GlobalExports、bindDiagnostics、IsBound) | `store.File` に足す / 別の `Bound` 構造体を File が持つ / binder package の型 | 「誰が読むか」で決める。checker と LS が読む field は File、binder 内でしか使わないものは binder |
| これらの列の所有者 | `store` package (AST と同じ場所、Store は noscan のまま別 struct) / `storebinder` package / `store.File` の field | store package は AST の形だけを知る、という境界を保つか。symbol の型 (`ast.Symbol` か新型か) が決まらないと決まらない |

### 4.4 Symbol の型

`ast.Symbol` は `Declarations []*Node` と `ValueDeclaration *Node` を持ち、checker と LS の 72 file が読む。Store では宣言は `store.Ref` (file index + id、8B、noscan) になる。選択肢: (a) `ast.Symbol` に Store 用 field を足す (移行期は両方持つ)、(b) `store.Symbol` を新設して binder は新型だけを書く (checker 着手まで両立)、(c) ジェネリック。決め手: 7c の等価テストで何を比べるか (4.6)、7d (checker) が読む形、Symbol 1 個の大きさ (今 10 word)。`SymbolTable` (`map[string]*Symbol`) と `id atomic.Uint64` の扱いも含める。

### 4.5 7b からの引き継ぎ

- `ExternalModuleIndicator` の計算をどこでやるか: parser の末尾 (Pointer と同じ) / bind の先頭 / 線形 pass。`collectExternalModuleReferences` は木を歩くので、`nodes` の 1 pass で kind を見るだけで済むか確かめる。
- `reparseTopLevelAwait` の有効化と 8 file の再検証を 7c の検証指示に入れる。
- 診断の file: `store.File` の diagnostics に file を付ける経路 (program 側か File 側か)。
- 7b レビューで挙がった `SetFlags` / `SetLoc` の未使用、`reparseTopLevelAwait` の未検証を 7c でどう扱うか (先に片付けるか、7c の実装指示に含めるか)。

### 4.6 検証の設計

- **等価テスト**: Pointer の bind 結果と Store の bind 結果を何で比べるか。候補: 各ノードの Symbol の (Name、Flags、Declarations の位置列、Parent Symbol の Name)、container の Locals の key 集合と各 Symbol、flow graph の形 (node ごとの FlowNode の Flags と antecedent の node 位置)、file の SymbolCount / GlobalExports / bindDiagnostics。7b の `storetest.Equivalent` に足す形か、別 driver か。corpus (17,415 file) で回す。
- **KPC**: `BenchmarkASTBindKPCV1` と同じ形 (parse は区間外、bind だけ Measure)。Store 側は 7b の parser で作った Store を bind する。`kperf.Session.Measure` は GC を 1 回しか呼ばず pool の scratch を温存する契約 (session.go のコメント) なので、binder の pool も同じ挙動になる。
- **retained / GC**: 7b の `BenchmarkStoreParseRetainedV1` を bind 後まで伸ばす (GC 2 回で HeapAlloc を読む)。設計の賭け (Store は noscan) が symbol / flow の列でどれだけ崩れるかを数字で出す。
- **合格線**: cycles/node ≤ 1.1 倍 (決定済み)。retained と GC の線は提案して私が決める。

## 成果物の順

1. 設計文書 (4.1〜4.6 の決定と、採らなかった案)。
2. 実装指示書 (7b の型: ガード、規則、書き換え箇所の表、`store` に足す API の一覧、テストの契約)。
3. 検証指示書 (7b の型: 正しさ、移植の機械性、命令列、KPC、retained / GC、レポートの章立て)。

最初のターンは、§4.1 の範囲について選択肢と推奨を出すところから始めてください。読むものを読み終えるまで問いを出さないでください。
