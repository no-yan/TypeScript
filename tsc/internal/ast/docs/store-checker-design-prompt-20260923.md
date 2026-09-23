# TODO 7d (Store checker) 設計セッションのプロンプト

作成日: 2026-09-23。新しいセッションの最初のメッセージとして貼る。対象は設計を対話で決めるエージェント。7c の [store-binder-design-prompt-20260922.md](store-binder-design-prompt-20260922.md) と同じ型。

---

あなたの役割は、Store AST (`tsc/internal/ast/store`) と Store binder (`tsc/internal/storebinder`) の上で checker を動かす TODO 7d の**モジュール設計を私と対話で決める**ことです。実装はしません。成果物は設計文書 1 本と、7c と同じ形の実装指示書・検証指示書です。7d は 7c より一桁大きい (checker は非テスト 60,831 行、binder.go は 2,802 行) ので、**最初の決定は「7d で何を作るか」の切り出し**です。

## 進め方の規則

1. **1 ターンに 1 つの決定。** §4 の順に、問いを 1 つ出し、選択肢を表にして (何を得るか、何を払うか、どの計測で決まるか)、推奨を 1 つ添えて私の判断を待つ。決まったら設計文書に書いてから次へ進む。
2. **推測で埋めない。** 選択肢の費用が分からないときは、コードを読むか、既存の計測値を引くか、「これを測れば決まる」と実験を提案する。数字の無い形容詞 (速い、重い) で比べない。
3. **私に忖度しない。** 私の案に穴があれば反論する。私が既に決めたこと (下の「決定済み」) は再論しない。
4. 決定は `tsc/internal/ast/docs/store-checker-design-<YYYYMMDD>.md` に、既存の設計文書と同じ文体 (決定事項・採らなかった案・不変条件・検証計画・TODO) で書く。「なぜ採らなかったか」を必ず残す。
5. 全部決まったら、`store-binder-implementation-instructions-20260922.md` と `store-binder-verification-instructions-20260922.md` を型にして 7d 用の 2 本を書く。7d を段に割ったなら、最初の段の分だけでよい。
6. 生成コードについての原則 (2026-09-23 に合意): スキーマ (`ast.json`) から導けない知識は generator に入れず手書きにし、generator は表だけを出す。移植先は参照実装 (Pointer) と同じ形で書く。検証は検査対象と独立な知識から作る (同じ generator が出したテストは同じ盲点を持つ。役割の無い kind に黙って既定値を返す accessor は欠落を隠す)。

## 先に読むもの (この順に)

1. `tsc/internal/ast/docs/store-ast-design-20260922.md` — §1 (ゲート 3 は「binder 通過後に決める」)、§2.5 (通貨)、§2.8〜2.9 (不変条件、外部 Store)、§5 (links、synth Store、JSDoc)、§6 TODO 9〜10、§7、§8。
2. `tsc/internal/ast/docs/store-binder-design-20260922.md` — §2 (決定事項)、§4 (不変条件)、§7 の末尾「7c の後に残るもの」と accessor 命名の 2 層化。
3. `tsc/internal/ast/docs/store-binder-verification-report-20260923.md` — §1、§3.2 / §3.4 (JS の除外と mask)、§5、§9。
4. `tsc/internal/ast/docs/accessor-dynamic-frequency-20260921.md` — check の accessor 回数 (1 node あたり Kind 143.8、Parent 49.4、`IsXxx` 72、外付け (Symbol / Locals / GetNodeId) 11.5、`GetSourceFileOfNode` 948 万回)。
5. `tsc/internal/checker/links.go` と `checker.go:670-700` (links の一覧)、`core/linkstore.go` (`PagedLinkStore`)。
6. `tsc/internal/binder/nameresolver.go` (506 行)、`referenceresolver.go` (262 行)。
7. `tsc/internal/parser/reparser.go` (752 行) と `jsdoc.go` (1,355 行)。JSDoc の構築経路。
8. `tsc/internal/compiler/ast_benchmark_test.go` の `BenchmarkASTCheckV1` / `BenchmarkASTCheckKPCV1` (compiler プロジェクト 79 file、SingleThreaded)。

## 決定済み (再論しない)

- **7c は合格** (2026-09-23): corpus 17,085 file で bind の等価 mismatch 0、Bind cycles/node 1.025、retained 0.56。通貨は `store.Node{s,h}`、header 32B に flow / symbol、予約 slot、`Bound` (index 連結の flow slab、scratch + Compact)、`store.Symbol` (`Declarations []Ref`、96B)。
- **JSDoc は storeparser に reparse を足して parse の Store 本体に持つ** (ユーザー決定)。7c の JS 除外 330 file と JS の mask 2 件 (`ThisNodeOrAnySubNodesHasError`、Imports の JSDoc 由来要素) はこれで消えるはずで、7d の検証で確かめる。
- `bindChildren` の kind switch の表化は行わない。GC の線は mark の CPU で引く。
- 生成コードの原則 (上の規則 6)。

## 前提と制約 (事実)

- **checker の規模と AST への触れ方** (非テスト、静的な出現数。静的数は実行回数の順位を誤るので、重みは 4 の動的頻度で付ける):

  | | 出現 |
  | --- | ---: |
  | `*ast.Node` | 1,956 |
  | `ast.IsXxx` | 1,968 |
  | `.Parent` | 1,163 |
  | `.Kind` | 638 |
  | `.Declarations` / `.ValueDeclaration` | 297 / 261 |
  | `JSDoc` | 176 |
  | `ast.GetSourceFileOfNode` | 125 |
  | `.Symbol()` | 103 |
  | ノード構築 (`.NewXxx(`、factory、Clone) | 507 / 70 / 54 (nodebuilder が主) |
  | `GetNodeId(` | 13 |

- **links**: node キー 12 本 (`core.LinkStore[*ast.Node, …]` と `nodeLinkStore` = `PagedLinkStore` を `GetNodeId` で引く)、symbol キー 16 本 (`GetSymbolId`、`symbolArenaLinkStore`)。`NodeId` / `SymbolId` は今は process-wide の atomic 採番。Store の `NodeRef` と `store.SymbolId` は **file ごとに密** で、checker の transient symbol はどの `Bound` にも属さない。
- **ast を import する非テスト package**: ls 53 file、transformers 40、checker 23、execute 19、printer 14、compiler 11、format 10 …。checker の出力 (型、symbol、ノード) を読むのはこれらで、7d の境界をどこに引くかの決め手になる。
- **前回の Store ブランチ (b719) の checker の教訓**:
  - VS Code trace (GOGC=off) で Store 版は +10.5G inst。**両版共通の 2,699 関数は横ばい (−0.3G)** で、差は全部 Store 固有: accessor +7.5G、map +2.7G、書き換えた checker 関数 +1.9G。AST 縮小の locality 利益は mutator では観測されなかった。
  - map 2.7G の内訳: `NameResolver.Resolve` が全祖先で `Locals()` を引き `map[NodeRef]` を miss probe する分が 0.78G (Pointer は型判定で map を触らない)。`PagedLinkStore` の pageMap 落ち 0.77G。16B の `Handle` キーの generic map は `StoreID<<32|NodeRef` の uint64 キーで −7% (checker CPU、monaco 単スレ)。`symbolNodeLinks` の `nodeLinkStore.key()` は毎回 `GetSourceFileOfNode` を呼び最も遅い。
  - accessor self 12.7G の内訳: 多相 accessor 3.69 (Expression / Name / Text / ModifierFlags)、utility 3.43 (`GetSourceFileOfNode` / `FindAncestor`)、Parent 2.14、list 1.19。多相 accessor は動的にほぼ単相 (Text 98% Identifier、Expression 90% PAE+Call)。
  - JSDoc を checker の synth Store で遅延構築した実験は Monaco Bind −20〜29%、Check 誤差内。checker が JSDoc を要求した host は 19.4k 中 3.1k。**7d はこれを採らず parse 時に本体へ入れると決めた** (上)。遅延の利得を捨てる代わりに bind の等価と単純さを取った。
- **基準値** (Pointer、GC オフの Session、`_kpc-baselines/kpc-after.txt`): Check 8.483〜8.487G inst (compiler プロジェクト)。Emit 5.91〜5.95G。cycles は同ファイルから取る。
- **不変条件** (§2.8): `NodeRef` は密で parse 後不変。Store 本体は noscan。bind 完了後 (`Seal`) は Store に書かない。`map[NodeRef]T` を hot path に置かない。

## 決めること (この順に)

各項目で、既知の選択肢と決め手を挙げてある。選択肢を増やしてよいが、決め手の無い選択肢は出さない。

### 4.1 範囲と段の切り方

- **checker 全体の機械的移植を 1 段でやるか、段に割るか。** 候補: (a) 全体を 1 段 (binder と同じ逐語移植、60k 行)、(b) 検証可能な縦の切り口 (例: `checkSourceFile` から診断までを通すのに要る集合) を先に、(c) 基盤だけ先 (links、`Ref` 解決、nameresolver、JSDoc reparse、synth Store) で checker 本体は次段。決め手: 各段で何が等価テストとゲートで検証できるか。「測れない段」を作らない。
- **Pointer と Store の checker を併存させる期間の形**: 7b / 7c と同じく別 package (`storechecker`) に写してテストだけが両方を import するか。LS / transformers / printer は 7d では Pointer のままにするか。
- **nameresolver / referenceresolver の port** を 7d に含めるか (checker と LS が呼ぶ)。

### 4.2 JSDoc reparse の形 (決定済みの方針の中身)

- storeparser に reparser を移植して `@typedef` / `@callback` / `@import` / `@type` / cast / `@implements` 等を Pointer と同じ位置に組む。`NodeFlagsReparsed` で区別。Builder に要る API (構築済みの list への末尾追加、既存 slot の差し替え) と、その Compact / dead node への影響。
- checker が読む **JSDoc comment ノード** (`node.JSDoc(file)` の tag 列、`@deprecated` 等) も本体に持つか。Pointer はこれを parser の JSDoc cache に置く。reparse 由来ノードと comment ノードは別物なので、両方の置き場を決める。
- 7b の parse 等価と 7c の bind 等価から JS の除外と mask を外せることを検証計画に入れる。

### 4.3 checker の通貨と、heap に保存するノード

- checker のローカルと関数境界は `store.Node{s,h}` (7c と同じ) でよいか。7c は単一 Store だったが checker は複数 file をまたぐ。`Node{s,h}` は `s` を持つので file をまたいでも解決は不要。
- **型・signature・links などの heap 構造にノードを保存する形**: `Node{s,h}` (16B、pointer 2 本で scan 対象) / `store.Ref{file, id}` (8B、noscan、読むたびに file 表から `Store` を引く) / 混在。決め手: 保存の回数と読み戻しの回数 (動的頻度で数える)、GC が辿る pointer 数、`Ref` → `Node` の解決費用 (file 表の load + `node()`)。
- `GetSourceFileOfNode` (948 万回) は `Node.s` から 1 load にできる (§8 の候補)。7d で入れるか。

### 4.4 links と id

- node キーの links: `links[file][NodeRef]` の 2 段 (file ごとの密な slab) / uint64 キー (`file<<32|NodeRef`) の `PagedLinkStore` / 予約 slot。決め手: link ごとの密度 (何 % の node が持つか)、checker 数 × node 数の常駐、前回の教訓 (pageMap 落ち、generic map)。
- symbol キーの links: `store.SymbolId` は file ごとに密で、transient symbol はどの `Bound` にも無い。global な symbol id の体系 (file index + id / checker が採番する id / `*Symbol` の pointer キー) を決める。7c 設計 §7 の「`SymbolId` の global 化」。
- `Locals()` の不在 kind の経路: 7c の役割 accessor `LocalsSlot()` は表引き + 比較 1 回で不在を返す。nameresolver の祖先上りでこれが map probe を消すことを確かめる (前回 0.78G)。

### 4.5 Symbol と program 横断

- `store.Symbol` と `ast.Symbol` の関係: checker の transient symbol、`mergeSymbol` による global merge (`Declarations` が複数 file の `Ref` を持つ)、`ValueDeclaration` の解決に要る file 表 (`Ref.file` → `*Store`) の所有者 (program か checker か)。
- 7c 設計 §7 の accessor 命名の 2 層化 (id 層 `SymbolId()` / `FlowRef()` / `LocalsRef()` と、Pointer と同名の解決層 `Symbol()` / `Locals()` / `FlowNode()`、`Store` が `bound` を持つ)。Bind ゲートを通ったので、7d の最初にやるか決める。
- 診断の file: 7b / 7c は nil。program が付ける経路。

### 4.6 checker が作るノード (synth Store)

- nodebuilder (`nodebuilderimpl.go` 3,714 行) と `nodecopy.go` は型を AST に戻す (ノード構築 507 箇所)。これを 7d の範囲に入れるか (型の表示と declaration emit に要る。診断メッセージの型文字列もここを通る)。
- 入れるなら synth Store の形 (§5: 固定長 chunk の連結で動かさない、E8 の Store をまたぐ辺、checker ごとの所有)。

### 4.7 検証の設計

- **等価**: Pointer の checker と Store の checker を何で比べるか。候補: 全診断の (file, pos, len, code, message) 列、`TypeToString` の結果、各識別子の `getSymbolAtLocation`。compiler / conformance の corpus (17,415 file の unit) で回すか、テストの baseline (`testdata/baselines`) と突き合わせるか。**規則 6 のとおり、比較の根拠は Store 実装と独立に作る。**
- **ゲート 3**: Check の cycles で線を決める (`BenchmarkASTCheckKPCV1` と同じ契約、compiler プロジェクト)。Walk / Bind と同じ 1.1 倍か。前回の +10.5G の内訳 (accessor、map、書き換え関数) のどれを 7d の設計が消すかを、線を決める前に見積もる。
- **retained / GC**: check 後の常駐 (links、型、synth Store) を Pointer と比べる。GC の線は mark の CPU (決定済み)。

## 成果物の順

1. 設計文書 (4.1〜4.7 の決定と、採らなかった案)。4.1 で段に割ったら、後の段は「未決」として残してよい。
2. 実装指示書 (最初の段の分)。
3. 検証指示書 (同)。

最初のターンは、§4.1 の範囲と段の切り方について選択肢と推奨を出すところから始めてください。読むものを読み終えるまで問いを出さないでください。
