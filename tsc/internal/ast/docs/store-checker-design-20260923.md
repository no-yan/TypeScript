# Store checker (TODO 7d) 設計: 決定事項と検証計画

作成日: 2026-09-23。設計ドキュメントのみ。コードは変更しない。

2026-09-24 追記: JSDoc lazy parsing と合成ノードのストアについて、制約を分解した[再設計案](store-checker-regions-design-20260924.md)と、Checker の API・呼び出し順・寿命を具体化した[動作設計](store-checker-execution-design-20260924.md)を作成した。現在の検討案と旧決定の置換範囲はそちらを参照。この文書の X2'、深い複製、C4' 等の記述は決定履歴として残しており、再設計案と同時に満たす実装条件ではない。再設計は未実装・性能未検証。

2026-09-24 の見直し (ユーザー): 再設計案と動作設計を踏まえて次を決めた。段は 7d-0 / 7d-1 / 7d-2 の 3 段 (S2)。合成ノードから source への辺は foreign ref (FR1)、synth Store は満杯なら足す (R、C4' を置き換え)、遅延 JSDoc は Pointer と同じく file ごとの共有 cache + RWMutex で root の親は host への foreign ref (J-d、X2' を置き換え)、複製は浅い共有で list も foreign、`NodeList.Clone` は要素の辺を写す (K3、深い複製と再設計案の list の alias を採らない)、`store/utilities.go` は変換で出し直す (U2)、解決層は所有者を運ぶ `Flow` (V1)、StoreID は source 以外を program の 1 つの counter で振り `Ref` → `NodeKey` (Y3)、ノードの比較は `SameNode` (E1)。再設計案のうち checker ごとの JSDoc reader (J-a)、私有 StoreID の範囲分け、list の alias は採らなかった。各節の決定を参照。

Store AST ([store-ast-design-20260922.md](store-ast-design-20260922.md)、以下「設計文書」) と Store binder ([store-binder-design-20260922.md](store-binder-design-20260922.md)、以下「7c 設計」) の上で checker を動かす TODO 7d (設計文書 §6 の 9) の設計。決定の順序と問いは `store-checker-design-prompt-20260923.md`。

数字の出所: 断りが無ければ 2026-09-23 に `internal/checker` の非テスト 25 file (60,831 行) を grep で数えた静的な出現数。動的な重みは [accessor-dynamic-frequency-20260921.md](accessor-dynamic-frequency-20260921.md)。

## 1. 目標とゲート

設計文書 §1 のゲート 3 (Check)。線は §2.7 (未決) で決める。

## 2. 決定事項

### 2.1 範囲と段の切り方

**7d を 3 段に割る: 7d-0 基盤 (store の型と API) → 7d-1 JSDoc → 7d-2 checker の移植 (nameresolver を含む)。** (2026-09-24、ユーザー。2026-09-23 の 2 段 (7d-1 / 7d-2) を置き換え) 7d-1 の中では eager と reparse (source の Store に閉じ、B1 の Builder API だけに依存) を遅延 JSDoc の公開 (7d-0 の Region に依存) より先に行う。並行させるかは実装を委譲するときに決める。

| 段 | 中身 | 検証できること |
| --- | --- | --- |
| 7d-0 | 辺の型 (`NodeEdge` / `ListEdge`) と foreign の解決 (FR1、K3、安全条件つき) と generator への反映。`Ref` → `NodeKey`、StoreID の採番 (Y3)、`Store` → `*File` と `File()` (F1 の見直し)。accessor の 2 層化と所有者を運ぶ解決層 (A1、V1)。`store.Symbol` の `Declarations []Node` / `ValueDeclaration Node` (D2) と `gid` (I1)。合成ノードの Factory / NodeRegion (R) と writer (`SetParent` など)。変換器の binder.go 試作と、ast の utility の変換で `store/utilities.go` を出し直すこと (U2)。storebinder と bind の等価テストの手直し | 7b / 7c の parse / bind の等価が mismatch 0 のまま (出し直した utility で)。Walk / Parse / Bind の KPC を 7d-0 の前後で同じセッションで取り、既存の線 (Walk ≤ 1.15、Parse ≤ 1.00、Bind ≤ 1.10) を満たす (**FR1 と V1 の hot path の費用はここで初めて実測になる**)。retained / GC (D2 の見積り: mark CPU の比 0.10 → 0.13 前後)。storage の操作列のテスト ([動作設計](store-checker-execution-design-20260924.md) §9 の操作を checker 無しの手組みのノードで、容量 1〜3 で境界を強制)。変換器の自動化率 (binder.go の試作) |
| 7d-1 | reparser.go (752 行) と jsdoc.go (1,355 行) を storeparser に手作業で逐語移植 (T1)。eager と reparse を本体に入れる (B1 の Builder API) → 遅延 JSDoc を file ごとの共有 cache に lock の下で公開 (J-d、root の親は host への foreign ref) | 7b の parse 等価から JS の除外規則 (`SkipReparsed`、JSDoc cast の透過、`MaskFlags` の 4 bit) を外して mismatch 0。7c の bind 等価から除外 330 file と JS の緩め 2 件を外して mismatch 0。Parse KPC に dom を足す。遅延 JSDoc: 同じ host を 2 回引いて parse が 1 回、空の結果、root → host → `File()` |
| 7d-2 | checker の変換 (nameresolver を含む。LS / emit 向け API は根から到達する関数だけ、E3)。nodebuilder の出力は合成の Store (NB1) と Store → Pointer の変換器 | 全診断の等価 (位置、code、message、related information、suggestion)、Check KPC |

2026-09-23 の 2 段の表 (記録用):

| 段 | 中身 | 検証できること |
| --- | --- | --- |
| 7d-1 | reparser.go (752 行) と jsdoc.go (1,355 行) を storeparser に移植。JSDoc 由来ノードを parse の Store 本体に持つ (決定済み)。TS の JSDoc comment ノードの置き場は §2.2 で決める | 7b の parse 等価から JS の除外規則 (`SkipReparsed`、JSDoc cast の透過、`MaskFlags` の 4 bit) を外して mismatch 0。7c の bind 等価から除外 330 file と JS の緩め 2 件 (`ThisNodeOrAnySubNodesHasError`、Imports の JSDoc 由来要素) を外して mismatch 0。Parse KPC |
| 7d-2 | checker の移植。nameresolver を含む。LS / emit 向け API (services 1,151、exports 400、emitresolver 1,323 行) は根から到達する関数だけを残す (下の「LS / emit 向け API の外し方」) | 全診断の等価 (位置、code、message)、Check KPC |

根拠:

- **checker の「縦の切り口」は package 全体と同じになる。** Go の package は全体がコンパイルできないとテストできず、`checkSourceFile` から到達する関数は型解決の相互再帰で checker.go のほぼ全部に及ぶ。切り出せる境界は出力の言語が変わる 2 か所だけ: nodebuilder 群 7.0k 行 (`nodebuilderimpl`、`nodecopy`、`pseudotypenodebuilder`、`nodebuilder_hover`、`nodebuilder`、`nodebuilderscopes`、`symboltracker`、`printer.go`、型 → AST) と、LS / emit 向け API 2.9k 行。本体は約 51k 行。
- **診断の message は Pointer の printer を通る** (`typeToStringEx` checker/printer.go:59 → nodebuilder → `printer.NewPrinter`)。printer を 7d で移植しない限り、7d-2 で message を比べるには nodebuilder の出力が Pointer ノードである必要がある。詳細は §2.6。
- **7d-1 は checker と独立に閉じ、7b / 7c と同じ規模と検証の形を持つ。** 7d-2 が JSDoc を読む前提 (置き場と API) を先に固める。

7d-2 の大きさ (7c の約 20 倍) は段を割っても縮まない。代わりに 7d-2 の中の検証の順を決める (§2.7): nameresolver の単体等価 → build → 全診断の等価 → Check ゲート。ゲートの線を決める前に、前回の +10.5G の 3 項目 (accessor 7.5G、map 2.7G、書き換えた関数 1.9G) をどの決定が消すかを見積もる。

**基盤 (links、id、synth Store) の設計の手戻りは、段を割っても減らない。** 基盤を先に実装しても使い手が無く事後に検証できることは少ないので、誤りが見つかるのはどの案でも checker が動いたときである。手戻りを減らす手段は 2 つ: (1) 基盤の選択肢を Pointer 上の計測 (links の密度、`Ref` の保存と読み戻しの回数、`Resolve` の呼び出し分布) で設計時に決める (§2.3〜2.6)、(2) 移植を機械的な変換にして、基盤の変更を「変換規則を直して再生成」で済むようにする (§2.1 の残りの問い)。

#### 採らなかった案

| 案 | 理由 |
| --- | --- |
| (a) 1 段 (JSDoc、nameresolver、checker 60.8k 行、nodebuilder の synth Store 化) | 等価もゲートも最後に一括で出て、落ちたときの帰属が 60k 行にまたがる (前回の +10.5G の内訳は事後の trace 頼みだった)。printer が Pointer なので型文字列の出口で Store → Pointer の変換がどのみち要る |
| (c) 3 段 (JSDoc / 基盤 = nameresolver・links・id・synth Store / checker 本体) | 基盤の段が測れない。links・id・synth Store には使い手が無く、nameresolver は等価は取れるが性能は合成 driver でしか測れない (7c で nameresolver を範囲外にした理由と同じ)。手戻りの危険は checker が動くまで見えない点で (b) と同じ |
| (2026-09-24 追記) 2 段のまま基盤を 7d-1 / 7d-2 の中で作る | FR1 は全 Store の `node()` を変えるので、その費用が Check ゲートまで分からない。上の (c) を退けた理由 (基盤の段は測れない) は、今の基盤 (FR1、D2、A1) については成り立たない: Walk / Parse / Bind の KPC、parse / bind の等価、storage の操作列のテストで測れる。7d-0 の代償は Factory の API を使い手 (checker) より前に決めること。足りなければ 7d-2 で直す (変換規則の側で吸収できる) |
| (b') 7d-2 を本体 51k (nodebuilder は stub) と nodebuilder 7k に割る | ゲートの前倒しは 7k 行分 (11%) だけで、本体の段では message を比べられず、stub が型文字列を返す経路を隠す |

#### 移植の方法

**go/types を使う変換ツールで移植する。** (2026-09-23、ユーザー) ツールは (a) 型の置換と (c) 子の受け直しの類の規則だけを持つ。規則にならない書き換え ((f) 類) は手書きの差分として別に持ち、storechecker は毎回「変換 → 手書き差分の適用」で再生成する。基盤の設計変更と上流の checker への追従を再生成で済ませることが、段を 2 つに割って進む (§2.1) 前提になる。

- 規則は store の API と go/types の型情報から導けるものに限る (生成コードの原則: スキーマから導けない知識は generator に入れず手書き)。
- (g) 読みのまとめはツールに入れない。「間にノードへの書き込みが無い」の判定は意味解析で、規則にならない。入れるかどうかと仕組みは「手書き差分の持ち方」(下) のとおり未決で、入れない公算が高い。
- **採否は binder.go での試作で確定する。** binder.go に変換をかけ、7c の手書き逐語版 (`_store-binder-results/binder-verbatim.diff` の出力側) と比べる。手書き版は独立に作られた正解なので、一致した hunk の割合が自動化率になる。線は「(a)〜(d) 相当の hunk の 9 割以上が一致」(案)。

checker で規則が当たる量 (静的): `.AsXxx()` 1,453 (うち `.AsXxx().Field` 441)、`== nil` 1,016 (ノード以外を含む)、`[]*ast.Node` 170、`.Nodes` 100、`.Data()` 系 35。規則にならないもの: `map[*ast.Node]` 12、`Set[*ast.Node]` 6、`map[ast.NodeId]` 6 (§2.4)、`c.factory` 25 (§2.6)。7c では 64 hunk 中 61 が (a)〜(d) で、(f) は 3 hunk + 他の hunk の中の 40 箇所、(g) は別に 41 hunk だった。

| 採らなかった案 | 理由 |
| --- | --- |
| 手作業の逐語移植 (7c と同じ) | 58k 行を手で書き、基盤の設計変更のたびに手で直す。上流に追従できない。手戻りの費用が段の割り方の前提 (§2.1) を崩す |
| 変換ツールを 1 回だけ使い、以後は手で保守 | 基盤の変更が手作業に戻り、上の案の欠点をそのまま持つ |

#### 変換の出力の置き場 (併存の形)

**Pointer の package 配置を写し、生成物はコミットする。** (2026-09-23、ユーザー) `ast` → `store`、`binder` → `storebinder`、`evaluator` → `storeevaluator`、`checker` → `storechecker`。生成の再現 (再生成して `git status` が不変) を検証の項目にする (7a〜7c の generator と同じ)。production は store 系 package を import せず、テストだけが両方を import する。

- checker が呼ぶ `ast.Xxx(` は 382 種で、`store` にあるのは 130 種。残る 252 種は `ast/utilities.go` (4,631 行) の一部で、変換して `store` に出す。
- checker が import する package のうちノードを読むもの: `binder` (`NameResolver`、`SetValueDeclaration`、`GetContainerFlags`、`GetSymbolNameForPrivateIdentifier`、`FindUseStrictPrologue`。`ReferenceResolver` は emitresolver だけ)、`evaluator` (168 行)、`pseudochecker` (1,126 行) と `modulespecifiers` (2,286 行) は nodebuilder だけ、`astnav.GetTouchingPropertyName` (relater.go 1 箇所)、`parser.ParseIsolatedEntityName` (jsx.go:1436、文字列から Pointer ノードを作る)。`pseudochecker` / `modulespecifiers` を変換に含めるかは §2.6 で決める。
- checker を import する ls 30 file、api、compiler、execute/incremental、transformers/declarations は 7d の範囲外。
- **`store/utilities.go` (読み側、7c で手書きした 165 関数) は変換で出し直す。storebinder (書き込み側) は手書きのまま (U2)。** (2026-09-24、ユーザー。下の 2026-09-23 の決定を置き換え) 読み側の utility は checker と同じ契約 (K3 の list の解決、FR1 の foreign、見直し中の `NodeKey`、A1 の名前) に従う必要があり、P2 の hash は上流の Pointer の変更しか検出しないので、手書きのままだと契約の変更を直し忘れても何も落ちない。出し直しは 7d-0 の作業で、7c の parse / bind の等価テストが出し直した utility で通ることを確かめる。7c の (f) / (g) のうち utility に入れたもの (実装報告 §2 の #33〜#36、§3 の (g) 8 関数、§4.2 の 3 件) は、規則にするか P2 の差し替えにするかを 1 つずつ決める。7c の手書き版は変換器の試験台として git の履歴に残る。storebinder は変換規則 (読み側の型の置換) の対象外で、binder を変換しない理由 (試験台として正解を残す) も変わらない。
- (2026-09-23 の決定の記録) **7c の手書き部分 (`storebinder`、`store/utilities.go` の 165 関数) は変換で出し直さない。** (2026-09-23、ユーザー。当初案の「出し直す」を撤回) binder.go は変換器の試験台 (7c の手書き逐語版を正解として比べる) にだけ使う。したがって基盤の変更 (Symbol の global 化、accessor の 2 層化など) が storebinder に及ぶときは手で直す。`store/utilities.go` の手書き関数は変換の出力と同名になるので、P2 (下) の差し替えとして扱い、元の Pointer 関数の hash を記録する。

| 採らなかった案 | 理由 |
| --- | --- |
| `storechecker` 1 package に依存のノード読みコードも全部まとめる | Pointer との対応が崩れ、diff が package をまたぐ。7c の `store` の utilities と重複する |
| 生成物をコミットしない (テスト時・`go generate` 時に作る) | 生成物をレビューで読めない。grep・`-tags storechecks`・objdump が「今の生成物」に対する作業になり、再現性の確認がかえって難しい |

#### 手書き差分の持ち方

**関数単位の差し替え (P2)。** (2026-09-23、ユーザー) 手書き file に同名の関数があれば変換はその関数を出さない。手書きの関数には、書いたときの Pointer 版の関数本体の hash を記録し、上流で変わったら変換を止める。

- **(f) はできるかぎり変換規則に入れる。** 呼び出し先の付け替え、引数の追加、list のループの形は規則にする。7c の (f) 28 項目のうち少なくとも 7 項目 (#2 `GetNodeId` → `FileRef`、#4〜#6・#9 の Store 版 helper への付け替え、#15 `GetLocals(bound, …)`、#24 `Refs()` のループ) がこの形。手書きの関数は制御構造を変えるものだけに絞り、基盤の変更で手で直す範囲を最小にする。
- **(g) の仕組みは今は決めない。入れない公算が高い。** 性能のための局所的な書き換えを入れるたびに変換の仕組みを保守する費用が高い。性能の手段は、全関数に一律に効く store の API と変換規則の側 (通貨、links、`Locals` の不在判定、`IsXxx` の表引きなど) に置く。Check ゲートは (g) 無しの変換出力で判定する前提になる。
- 7c では (g) が 30 / 170 関数 (最大の `bind` を含む) に触れた。7c の Bind ゲートに (g) がどれだけ効いたかは測っていない (逐語版と fold 版の KPC を並べていない)。

| 採らなかった案 | 理由 |
| --- | --- |
| P1 patch file | 行単位なので、変換規則 (基盤の設計) を変えると文脈行が一斉に変わり当たらなくなる。手戻りを安くするという変換の目的とぶつかる |
| P3 構造化した書き換え指示 (関数名 + 式のパターン → 置換式) | 小さな言語を作ることになり、制御構造を変える (f) は表せず結局 P2 も要る |

#### LS / emit 向け API の外し方

**変換器は根の集合から call graph で到達する関数だけを出す (E3)。** (2026-09-23、ユーザー: 実現可能なら。試作で確認済み) 根は等価テストとベンチが呼ぶ `Checker` の公開 method で、§2.7 で確定する。解析は RTA (`x/tools/go/callgraph/rta`) に全 package の `init` を根として足したもの。変換しない package (printer、parser、transformers、compiler、ls) の先は辿らない。 (2026-09-28 追記: 7d-0 の ast の utilities の到達計算は RTA をやめ、本体に出てくる ast の package-level 関数の識別子 (呼び出しと値) を辿る構文上の計算にした。RTA から使っていたのが静的な辺だけだったため。出力は一致。checker の到達計算 (method、interface 呼び出しを含む) で RTA が要るかは 7d-2 で決める。)

file 単位では外せない: services / exports / emitresolver の関数を本体と nodebuilder が呼ぶ (`IsArgumentsSymbol` ← checker.go、`SkipAlias` ← utilities.go、`IsDistributedTypeParameter` ← relater.go、`isConstEnumOrConstEnumOnlyModule` ← checker.go、`isOptionalParameter` ← utilities / nodebuilder、`IsSymbolAccessible` ← symbolaccessibility など。名前の grep なので同名 method の誤検出を含む)。

試作 (2026-09-23、scratchpad `reach/`、`ssa` + CHA / RTA、根 = `NewChecker`・`GetDiagnostics`・`GetGlobalDiagnostics`・`GetSymbolAtLocation`・`GetTypeAtLocation`・`TypeToString`、実行 3.8 s・RSS 0.96 GB)。checker package の関数 (method を含む) の到達数:

| file | CHA | RTA |
| --- | ---: | ---: |
| services.go | 5 / 64 | 4 / 64 |
| exports.go | 6 / 92 | 2 / 92 |
| emitresolver.go | 20 / 64 | 20 / 64 |
| tracer.go | 6 / 38 | 6 / 38 |
| types.go | 58 / 112 | 58 / 112 |
| checker.go | 1,490 / 1,508 | 1,487 / 1,508 |
| checker package 計 | 2,513 / 2,879 | 2,500 / 2,879 |

- **checker package の刈り込みは効く。** LS / emit 向け API が落ち、本体はほぼ全部残る。
- **ast の utilities はほとんど刈れない。** `ast/utilities.go` の free 関数は CHA 383 / 416、RTA 316 / 416。「ast から store に何を変換するか」は同じ仕組みで決まるが、量はほぼ全部になる。
- CHA は「全関数が address-taken」と仮定するので、`func(*ast.Node) bool` 型の callback (`FindAncestor`、visitor) から同じ signature の関数 (`IsXxx` のほぼ全部と binder の関数、binder 178 / 195) に辺が張られ、粗い。RTA は address を取る箇所が到達したときだけ辺を張るので細かいが、`init` で登録される関数値 (`ast.SetParseJSDocForNode` など) は `init` を根に入れないと落ちる。
- **刈り込みの誤りは黙って通らない。** 出力したコードから名前で参照される関数は全部静的な参照なので、刈りすぎればコンパイルが落ちる。関数値として渡される関数も、渡す箇所が出力されていれば名前で参照される。解析が粗くて残りすぎる害は、コード量が増えることだけ。

| 採らなかった案 | 理由 |
| --- | --- |
| E1 file 単位で外し、呼ばれる関数を stub か手書きで補う | 上の約 15 関数を手で追い続ける。上流で呼び出しが増えるたびに手で直す |
| E2 全部変換する | 使い手の無い約 2.9k 行が検証されずに入る。emitresolver は `ReferenceResolver` (262 行) と emit 側の型を引き込み、(f) の手書きも増える |

#### Check のハーネス

**Pointer の program から file 集合・options・module 解決を借り、同じ text から Store を parse + bind して、変換後の `storechecker.Program` を adapter で実装する (H1)。** (2026-09-23、ユーザー) Check KPC の計測区間は「checker を 1 つ作り (`SingleThreaded`)、program の順に file ごとに `GetDiagnostics` を呼ぶ」だけ。retained / GC には Pointer program を保持するこのハーネスを使わず、値だけの fixture と Store 専用 adapter を使う別プロセスを用意する（[レビュー契約 §3](store-checker-review-contracts-20260926.md)）。**Pointer 側も同じ driver で checker を直接呼んで測り直し**、それを Check ゲートの基準値にする。等価は program の後処理 (comment directive による除外、ソート) の前の checker の生出力で比べる。

- 旧基準値 (`BenchmarkASTCheckKPCV1`、8.48G inst) は `program.GetSemanticDiagnostics` で、checker pool・収集・`@ts-ignore` の除外・ソートを含む (compiler/program.go:777)。新しい driver の Pointer 値と並べ、差を program 側の費用として報告する。
- `checker.Program` (checker.go:554) でノードを返すのは `GetJSXRuntimeImportSpecifier` と `GetImportHelpersImportSpecifier` の 2 つだけで、Store 版を adapter に手書きする。他は file 名と module 解決。

| 採らなかった案 | 理由 |
| --- | --- |
| H2 compiler.Program を変換する | module 解決や include の処理まで変換対象になり、Store 化で得るものが無い部分に手間を使う |
| H3 Pointer は既存の 8.48G、Store だけ H1 | 区間の中身が違い (program の pool・除外・ソートが片側だけ)、比が checker の差を表さない |

H1 のメモリ計測用 adapter は7d-2の実装対象であり、Pointer AST を外す条件まで含めて検証する。

### 2.2 JSDoc reparse の形

前提: Pointer は TS の JSDoc を初回アクセスで遅延 parse し (`withJSDoc` parser/jsdoc.go:56、`resolveJSDoc` ast/ast.go:2745、`map[*Node][]*Node` + mutex、bind 後の書き込み)、`@see` / `@link` を含む comment だけ eager に parse する。eager + reparse は JS だけ。reparse が作るノード (`@typedef` / `@callback` の `JSTypeAliasDeclaration`、`@import` の `JSImportDeclaration`、cast、型注釈の複製) は parse の Store 本体に入れる (決定済み)。

#### comment ノードの置き場

**JS の JSDoc と、TS で `@see` / `@link` を含む comment は parse 時に本体 Store に入れる。TS の残りは Pointer と同じく file ごとの共有 cache に遅延で parse し、file ごとの RWMutex で守る (J-d)。** (2026-09-24、ユーザー。下の 2026-09-23 の決定「遅延分は checker の synth Store、`@deprecated` のときだけ重複」を置き換え。並列化の工夫は lock の待ちを測ってから)

- API は Pointer と同じ `node.JSDoc(file)` (file が nil なら `n.File()`)。本体の eager 索引を引き、無ければ file の遅延 cache を引き、無ければ lock の中で parse する。JSDoc を読む free 関数に reader を渡す必要が無く、変換は恒等写像。
- 遅延 parse は非公開の Builder で組み (speculation の巻き戻しも Builder の中)、`Finish` の後で StoreID を振り、root の親を host への foreign ref (FR1、§2.6) にして seal してから cache に載せる。公開した Store には以後書かない。空の結果も「完成済み」として記録し、同じ host を繰り返し parse しない (Pointer は map の `ok` を見る)。
- cache の key は host の file 内の `NodeRef` (source Store の番号で密かつ一意)。**host はその file の source Store のノードに限り、それ以外は通常 build で拒否する** (不変条件 27。Pointer の `JSDoc(file)` は clone など file の外のノードも受け、clone は `HasJSDoc` を含む flags を引き継ぐので、合成の host が来うる。今の checker でこの経路に合成の host が届くかは未確認)。3,618 回/check しか引かれず hot path ではないので map でよい (不変条件 2.8.6 は hot path の話)。
- 書き込み先は file の JSDoc cache で、Store 本体ではない。不変条件 2.8.5 には触れない (Pointer も同じ書き込みをする)。
- **遅延 JSDoc の StoreID は program の名前空間で parse のたびに atomic な counter で振る。** 合成の Store (R) も同じ counter で振る (Y3、§2.4)。番号は並列 check の順で変わるが、links の key にしか使わず出力に出ない (Pointer の `NodeId` と同じ性質)。
- 重複 parse は無い (file ごとに 1 回)。2026-09-23 に決めた「`@deprecated` のときだけ複数の checker が同じノードを parse する」というコードのコメントは不要になった。
- 検証: 7d-2 で lock の待ち時間を測る。並列 check の妨げになったときだけ lock の無い形 (host ごとの atomic な slot など) を検討する。

以下は 2026-09-23 の決定の記録。

**(2026-09-23) JS の JSDoc は parse 時に本体 Store に入れる。TS は `@see` / `@link` を含む comment だけ parse 時に本体に入れ、残りは checker が遅延で parse して自分の synth Store に置く。`@deprecated` は eager にしない。** 本体に入れるものは `withJSDoc` の時点で `nodes` の末尾に足す (Compact 前)。host → JSDoc の索引を本体と checker の synth Store がそれぞれ持ち、`host.JSDoc()` は本体を引いて無ければ checker の側を引く (無ければそこで parse する)。

根拠 (静的な追跡、2026-09-23。(A) の到達範囲は 2026-09-27 に訂正): TS の check で遅延 parse が起きる経路は 2 つだけ。(A) `checkSignatureDeclaration` → `checkUnmatchedJSDocParameters` → `getAllJSDocTags` (checker.go:2787、jsdoc.go:9) は関数から `GetNextJSDocCommentLocation` で祖先の文・宣言まで上がって読み、tag の門前払いが無い (`HasJSDoc` だけで parse する)。`checkSignatureDeclaration` の呼び出し元 5 つのうち 4 つは `checkSourceElement` 配下で検査中の file に閉じるが、`contextuallyCheckFunctionExpressionOrObjectLiteralMethod` (checker.go:10392) は式の型計算の側にあり、別 file に届く。型注釈の無い宣言 (`/** doc */ export const f = (x: number) => x`、object literal の method、class field の arrow、`export default` の arrow、推論される戻り値の `return` の arrow など) の型を `getTypeOfSymbol` が求めると、参照した側の checker が別 file の初期化子を `checkExpression` し、その file の JSDoc を parse する (2026-09-27、Pointer で fileA だけ検査して fileB の cache が埋まることを確認)。担当 checker も自分の検査で同じ host を parse するので、別 checker がこの経路で parse した分は全部重複になる。当初の「検査中の file の関数宣言を読むので重複しない」は、呼び出し元を 1 段しか辿らず、`check*Declaration` を文の検査の側だと名前から決めた誤り。(B) `IsDeprecatedDeclaration` / `addDeprecatedSuggestionWorker` → `GetJSDocDeprecatedTag` は参照された宣言 (別 file、lib の global、別 file の interface の member を含む) を読み、checker 間で重複しうる。(B) は parse 時の文字列走査で付く `PossiblyContainsDeprecatedTag` (parser/jsdoc.go:64) で門前払いされる。重複は (A) の別 file 経路と (B) を合わせて稀である (前回の Monaco で 4 checker の重複は 3,167 件中 30。経路別には分けていないので、30 は両経路の合計で (B) だけの値ではない)。checker と、checker が呼ぶ ast の utility で `.JSDoc(` を呼ぶのはこの 2 経路と `GetNodeAtPosition` (checker.go:32046、JSDoc を含めない呼び出し) だけ。nodebuilder と LS の経路はこの追跡の範囲外。

- speculation の巻き戻しは `nodes` / `extra` の truncate で JSDoc ノードも消える。host → JSDoc の索引も同じ位置まで巻き戻す。
- 本体に入れた JSDoc ノードは host より後の id を持つ。post-order の「子の id < 親の id」は JS で成り立たなくなるが、依存しているのは `Store.Root()` の「root が最後」だけで、root より前に足すので保たれる (§2.2 の Builder API の問いで不変条件の弱め方を書く)。
- `@deprecated` を eager にする案 (dom で JSDoc 6,153 個中 413 個、全部 eager の差に比例させて約 +0.5 ms の見積り) は採らない (ユーザー)。(B) の重複は消せるが、(A) の別 file 経路の重複は残る (2026-09-27 訂正、当初は「重複を構造的に消せる」と書いていた)。重複は稀で、eager の範囲を Pointer から広げない。

計測 (2026-09-23、Pointer、overlay で TS も eager にした版と比較。ns は load 5.9 で参考値、`scratchpad/jsdoc/`):

| | 遅延 (現行) | 全部 eager |
| --- | ---: | ---: |
| checker.ts parse | 23.7〜25.0 ms、26.14 MB | 23.8〜29.4 ms、26.34 MB (JSDoc が少なく誤差内) |
| dom.generated.d.ts parse | 10.5〜13.8 ms、9.90 MB | 17.4〜34.3 ms、13.23 MB (最小値比 1.66 倍、B/op +34%) |

- compiler プロジェクト (TS 124 file、lib 込み) で `HasJSDoc` の host は 5,299、check 中に parse されたのは 2,845 (54%)、`resolveJSDoc` の呼び出しは 3,618 回。前回の Monaco では 19.4k 中 3.1k (16%)。
- host → JSDoc の索引は 3,618 回しか引かれず (`GetNodeId` 333 万回の 3 桁下)、hot path ではない。
- **Parse の計測に dom を足す。** checker.ts には JSDoc がほとんど無く、TS の JSDoc の費用が今の Parse ゲートに出ない。

| 採らなかった案 | 理由 |
| --- | --- |
| J1 TS も parse 時に作り本体に持つ | JSDoc の多い入力の parse が 1.66 倍・B/op +34% (dom)。lib.dom.d.ts は全 program に入り、host の 46〜84% は読まれない |
| J2 file ごとの副 Store を checker 間で共有し、JS は parse 時・TS は遅延で lock の下に満たす (Pointer の `resolveJSDoc` と同じ形。2026-09-23 に一度決め、同日撤回) | (J-d の前身。J-d は JS を本体に置く点が違う) lock が要り、JS の JSDoc まで Store をまたぐ辺を持つ。重複 parse を避けられる利点は、重複が (A) の別 file 経路と `@deprecated` の経路に限られ稀なので小さい |
| checker ごとの遅延 cache (2026-09-23 の決定、および [再設計案](store-checker-regions-design-20260924.md) §5 の J-a: host ごとの完成済み Store を checker の reader が持つ) | JSDoc を読む free 関数 (`GetJSDocDeprecatedTag` など 4 関数) に reader を引数で渡す P2 差し替えか変換規則が要る。checker 間で (A) の別 file 経路と `@deprecated` の経路の重複 parse が起きる。cache の key を checker の中の名前空間で管理する。得るのは lock が無いことだけで、Pointer も同じ lock を払っている |
| `@deprecated` を含む comment も eager にする | 上のとおり |

#### 遅延 JSDoc の root から上がる辺

**遅延 JSDoc の root の親は host への foreign ref (FR1、§2.6)。通常の `Parent()` が host を返し、`GetJSDocHost` もそのまま動く。`File()` は遅延 JSDoc の Store が持つ `*File` で答える (§2.3、親は辿らない)。** (2026-09-24、ユーザー: FR1 と J-d による。下の X2' を置き換え) `JSDocHost()` という別の API、`storeChecks` の panic の罠、(checker, file) ごとの Store は要らない。

| 採らなかった案 | 理由 |
| --- | --- |
| X2' (下、2026-09-23 に決め 2026-09-24 に置き換え) | Store をまたげない前提の回避策で、root の親を nil にする特別扱い、それを見張る罠、別の API、`File()` のためだけの (checker, file) 分割を抱えていた。FR1 の下では全部要らない |

以下は 2026-09-23 の決定の記録。

**`Parent()` は変えない。遅延で parse した JSDoc の root の親は nil にし、host へは `JSDocHost()` で上がる。遅延 JSDoc の Store は (checker, file) ごとに 1 つ持ち、file は Store から引く。`storeChecks` build では遅延 JSDoc の root で `Parent()` を呼ぶと panic する (X2')。** (2026-09-23、ユーザー。同日の X1 を置き換え)

- Store をまたぐのは checker の synth Store に置く TS の遅延分だけ。JS と `@see` / `@link` は本体にあり、親は同じ Store の中にある。本体から遅延 JSDoc の Store への辺は無い (reparse の複製と元の対応 `GetReparsedNodeForNode` は位置による探索、host → JSDoc は索引)。
- 遅延分の読み手は 2 経路 (§2.2 の (A) `checkUnmatchedJSDocParameters`、(B) `GetJSDocDeprecatedTag`) で、どちらも tag を読むだけ。root を越えて上がるのは診断の `NewDiagnosticForNode` → `GetSourceFileOfNode` で file を知るためだけで、名前解決は無い。`GetSourceFileOfNode` は `Node.s` の file で答える (設計文書 §8) ので、Store 1 つを 1 file に対応させる。
- nodebuilder や LS で遅延 JSDoc から上へ辿る経路が後で見つかったら、`storeChecks` の panic で落ち、その箇所を手書きで `JSDocHost()` に直す。
- Store の数は「checker 数 × JSDoc を読んだ file 数」になる (各 Store は小さい)。

| 採らなかった案 | 理由 |
| --- | --- |
| X1 `Parent()` に分岐を 1 つ入れ、印のある `parent` で外の Store の host へ出る (2026-09-23 に J2 の前提で一度決め、置き場の変更で撤回) | 全ての `Parent()` (check で 49 回/node) に比較と分岐、Check の約 +1% の見積りを、root を越えて file を知るだけの 2 経路のために払う。根拠だった `@see` / `@link` の名前解決は本体に入りまたがなくなった |
| X2 (panic の罠なし) | 辿りを切る見落としが黙って結果を変え、等価テストでしか分からない |
| X3 遅延 parse を本体 Store に追記 | bind 後に本体へ書く (不変条件 2.8.5 違反)。複数 checker の同時追記と、再確保で古い `h` が古い内容を黙って読む失敗 (設計文書 §5) |

#### reparse の Builder API

**Pointer と同じ形の Builder API を足す (B1)。** (2026-09-23、ユーザー)

- 役割名の slot setter (生成、Compact 前だけ有効): 空の optional slot を埋める (`@type` → `decl.Type`、`@return` → `fun.Type`、`@param` → `param.Type`、`FullSignature`) と、子を cast で包み直す (`SetExpression` / `SetInitializer` / `bin.Right = makeNewCast(old)`) に使う。
- list の作り直し: 新しい block を作って slot を付け替え、古い block は dead になる (modifiers への追加、`@this` の引数、`@implements`)。7b の top-level await の再構築と同じ流儀。dead の量は `TestDeadNodes` で数える (JS だけ)。
- `finishMutatedNode` の Store 版: 生成 `ForEachChild` で直下の子の `parent` を書き直す。
- host → JSDoc の索引は Builder の `Mark` / `Truncate` に連動して巻き戻す。

**不変条件の弱め方** (設計文書 §2.7 の「構築は post-order」): JSDoc ノード (本体に足すもの) と JS の reparse が作るノード (cast、複製) は host より後の id を持ち、「子の id < 親の id」は成り立たない。依存してよいのは「root が最後のノード」(`Store.Root()`、store.go:77) だけで、JSDoc も reparse も root を組む前に終わるので保たれる。設計文書 §8 の「線形走査の活用」(親の設定・subtree facts の伝播を配列の 1 pass で) は post-order を前提にできない。

| 採らなかった案 | 理由 |
| --- | --- |
| B2 host を組む前に JSDoc を parse し、constructor に最終形の子を渡す | `withJSDoc` の呼び手 56 箇所を組み直し、Pointer と同じ形で書けない |
| B3 list の block に予備の容量 | 反例は JS の reparse だけなのに全 list が払う |

#### 7d-1 の移植の方法

**7b と同じ手作業の逐語移植 (T1)。** (2026-09-23、ユーザー) diff を記録し hunk を分類する。7b で (e) として削除した JSDoc / reparse の 12 関数と 11 field を戻す作業に当たる。store 系は「手書き: storeparser / storebinder」「変換: storechecker / store の utilities / storeevaluator」に分かれる。7d-1 は変換ツールの binder.go 試作と並行に進められる (試作は 7d-2 の前提で、7d-1 は依存しない)。storeparser には checker が遅延 JSDoc に使う入口 ((file, host) を受け、新しい Builder に JSDoc を組んで返す) を置く。

| 採らなかった案 | 理由 |
| --- | --- |
| T2 変換ツールに組み立て側の規則 (`factory.NewXxx` → Builder、id 書き) を足す | 7d-1 が変換ツールの完成を待つ。組み立て側の規則は checker に使い道が無く (checker が作るノードは 16 箇所、§2.6)、2.1k 行のために規則の集合をもう 1 つ持つ |

### 2.3 checker の通貨と heap に保存するノード

#### heap に保存するノードの形

**持ち主の型の package で決める (N1')。checker package の struct の field は `Node{s,h}`、store package の共有の型のうち `Bound` は 7c のとおり `NodeRef`。`store.Symbol` の `Declarations` / `ValueDeclaration` は `Node` (D2、§2.5)。** (2026-09-23、ユーザー。2026-09-24 に D2 に合わせて書き直し。当初は `Symbol.Declarations` / `ValueDeclaration` も `Ref` としていた) ローカル変数と引数は `Node{s,h}` (7c と同じ)。map の key は §2.4 で別に決める。変換規則は「field の持ち主の型が checker package なら `Node`」の 1 本で、型名の一覧を持たない。

設計文書 §2.5 の「ヒープに保存するのは `Ref`」は「`Bound` の中は `NodeRef`、`store.Symbol` と checker の持ち物には `Node`、map の key は `NodeKey`」に書き直す。§2.5 の根拠 (flows で効いたのは数十万本の pointer、§2.9 の synth Store の参照を共有構造に漏らさない) は、checker ごとの持ち物で保存数が約 7 万本の checker の struct には当たらない。`store.go:52` の `Node` の doc comment ("Store a Ref on the heap") も合わせて直す。

計測 (2026-09-23、compiler プロジェクト 936,205 node、単スレッド 1 回の check、`_fieldcount/`): checker package の struct field のうち型がノードを含むもの (`*ast.Node`、その slice / map、`NodeList`、型引数) の読みと書き。

| 分類 | 読み | 書き | 主なもの |
| --- | ---: | ---: | --- |
| 作業中だけ生きる | 6,410,967 | 3,603,562 | `FlowState.reference` 読み 3,379,945、`Checker.currentNode` 書き 1,537,716 / 読み 768,858、`Checker.lastGetCombinedNodeFlagsNode` 読み 1,262,952、`Relater.errorNode` 書き 270,598、`CallState.node` / `args` / `typeArguments` |
| 長く生きる | 1,153,515 | 67,440 | `Signature.declaration` 読み 572,354 / 書き 41,938、`TypeReference.node` 読み 277,546、`AliasSymbolLinks.typeOnlyDeclaration` 読み 235,326、`SourceFileLinks.identifierCheckNodes` 19,915 |
| map / links (§2.4) | 964,933 | 77,624 | `signatureLinks` 311,913、`typeNodeLinks` 256,128、`skipDirectInferenceNodes` 164,818、`flowTypeCache` 71,391 |

- 全部 `Ref` (N2) の費用: 解決 1 回 約 10 命令として、作業中 64M 命令 (Check 8.48G の +0.76%)、長く生きる分 12M 命令 (+0.14%)。
- 全部 `Node` の費用: 長く生きる保存は書き 6.7 万で、check の終わりまで残る pointer は Pointer 版より約 +7 万本、約 +0.5 MB。Store の AST が消した pointer (100 万本の桁) の 2 桁下。
- どの案も 1% を切るので、決め手は変換規則の単純さ。
- 計数の限界: `range` のループは 1 回と数える (`CallState.args` などの要素ごとの読みは過小)。method 経由の書き (`OrderedSet.Add` など) と `&x.f` 経由の書きは数えない。

| 採らなかった案 | 理由 |
| --- | --- |
| N2 struct field は全部 `Ref` | `FlowState.reference` (338 万回) など作業中の struct の hot な読みが解決を払い、Check の約 +0.9%。GC の得は約 7 万本 |
| N3 長く生きる型は `Ref`、作業中の struct は `Node` | 「どの型が長く生きるか」は型情報から導けず、変換器の設定に型名の一覧を手で持つ。得るのは約 7 万本の pointer と +0.14% の差だけ |

#### ノードの同一性の比較と nil

**変換規則で、両辺がノードの `==` / `!=` を `store.SameNode(a, b)` (`a.h == b.h || (a.IsNil() && b.IsNil())`) にする。ノードの slice の `slices.Contains` / `Index` は同じ比較で探す版に、map の key は `Key()` (nil は 0 に正規化) にする (E1)。** (2026-09-24、ユーザー) `== nil` は 7c と同じく `IsNil()`。

2026-09-26 の Z1 による補足: Node を非比較型にし、generic helper 内部の比較の変換漏れもビルドで検出する。`FirstNonNil` / `MapNonNil` / `OrElse` の Node に対する零値比較は IsNil を使う版へ、`SameMap` / `SameMapIndex` / `Deduplicate` / `AppendIfUnique` の Node 比較は SameNode を使う版へ付け替える。実際の型引数で選び、Node 以外は維持する。また key 専用 struct の Node field は NodeKey にする。既存の `NonExistentPropertyKey.propNode` と `accessibleChainCacheKey.location` が対象で、全構築・代入を Key() に変換し、Node として読み戻す用途がないことを確認する。使用箇所の検査と評価・slice 再利用契約は [実装指示書 §5.1](store-checker-7d0-implementation-instructions-20260924.md) に定める。

- nil は各 Store の番兵行 `nodes[0]` なので、別の Store の nil どうしを `==` で比べると false になる (Pointer は true)。7c は Store が 1 つだったので表に出なかった。nil でないノードは FR1 の foreign ref が正規の `Node{s,h}` を返すので、`h` の比較で足りる。
- 静的な出現数 (go/types、非テスト): checker は `node == nil` 1,276 / `node == node` 182、ast (U2 で変換する utility) 194 / 358、binder 95 / 24。ノードの slice の `slices.Contains` / `Index` は checker に最大 60 (ノード以外を含む)。
- 規則の対象は型で機械的に決まる (両辺が `*ast.Node`)。

| 採らなかった案 | 理由 |
| --- | --- |
| E2 `node(0)` が全体で共有の nil を返す | `node()` に `e == 0` の分岐が入り全ての辺の解決が払う。list の slot の 60% が nil (設計文書 §2.3) |
| E3 `==` のまま | 別の Store の nil どうしが false になり、黙って結果が変わる (無変更の `Update` が新しいノードを作る、など) |

#### `GetSourceFileOfNode`

**(2026-09-24 の見直し、ユーザー: Y3)** `File()` は Store の種類で分ける。source の Store と遅延 JSDoc の Store (J-d) は Store が持つ `*File` を 1〜2 load で返す (遅延 JSDoc の Store は中のノードが全部 host の file に属するので、公開時に `*File` を入れる)。合成の Store (R) は `Parent()` を辿り、source か遅延 JSDoc の Store に着いたらその file を、nil に着いたら nil を返す (Pointer の `GetSourceFileOfNode` と同じ答え)。合成ノードの file を作成時に処理中だった file や `Original` から推測しない。以下の F1 の本文のうち「synth Store のノードの `File()` は §2.6 で決める」はこれで閉じる。

**`Store` に `*File` への逆向きの pointer を持たせ、`n.File()` を 1〜2 load にする (F1)。** (2026-09-23、ユーザー) 変換規則は `ast.GetSourceFileOfNode(x)` → `x.File()`。parse の Store と遅延 JSDoc の Store (J-d、§2.2) は 1 Store の中のノードが全部 1 file に属するので意味は Pointer と同じ。synth Store のノードの `File()` は親を辿る (上の 2026-09-24 の見直しで決着。当初は「§2.6 で決める」としていた): checker が作る `this.x` の参照は `reference.Parent = constructor` (本物のノード) を持ち、Pointer では親を辿って file に届く (checker.go:5052〜5074、flow.go:2469〜2500)。

- check の呼び出しは 948 万回 (accessor 動的頻度 §5.1、82% は EnumMember で入力固有)。逐語に親を辿ると段ごとに `Parent()` 11 命令 (Pointer 3)。
- `Store` の pointer は file ごとに 1 本で GC の量は無視できる。File ↔ Store の循環は Go の GC で問題ない。

| 採らなかった案 | 理由 |
| --- | --- |
| F2 逐語に親を辿る | 948 万回 × 段数 × 8 命令の差。入力依存だがこの入力で Check の数 % の桁 |
| F3 checker の file 表で `c.files[n.s.file]` | checker を持たない free 関数 (ast の utility、scanner の helper) で使えず、規則が呼び出し元で分かれる |

### 2.4 links と id

#### node をキーにする links

**(2026-09-24 の見直し、ユーザー: Y3)** Store の番号を file の番号と分ける。`Ref{file, id}` → `NodeKey{StoreID, NodeRef}`、`FileRef()` → `Key()` に改名する (`Ref.File()` を診断の file と取り違える余地を消す)。StoreID は source の Store が 0..nFiles−1 (program が作るときに決める)、**それ以外 (遅延 JSDoc の Store、合成の Store) は全部 program の 1 つの atomic な counter で nFiles から振る**。番号は program の中で一意で、別の checker の `NodeKey` と衝突しない。以下の本文の「file index」は StoreID と読み替え、paged の links は `links[StoreID].pages[ref >> 8][ref & 255]`。外側の枠は checker ごとに最大の StoreID までの長さ (1 本あたり数千 × 8B) で、page は初回使用時に確保する。新しい Store を作るたびに atomic な加算が 1 回要るが、Store を作るのは合成の Store の満杯と遅延 JSDoc の parse (この入力で 2,845 回) だけ。

| 採らなかった案 | 理由 |
| --- | --- |
| Y1 範囲を分ける (私有の Store は checker ごとに最上位 bit を立てた範囲) | 節約するのは Store を作るときの atomic 1 回だけ。links の外側の枠が bit で 2 つの表に割れ、checker 間で `NodeKey` が衝突しうるのを見張る規則が要る |

**Pointer と同じ分け方のまま key だけ替える (L1)。** (2026-09-23、ユーザー) map の 9 本 (`core.LinkStore[*ast.Node, V]`) は `core.LinkStore[store.NodeKey, V]`。paged の `symbolNodeLinks` (Pointer は `GetNodeId` で引く `PagedLinkStore`) は StoreID ごとの 2 段にする (2026-09-24 に Y3 で file index → StoreID):

```
symbolNodeLinks.Get(n) = links[StoreID].pages[ref >> 8][ref & 255]
```

- 1 つの checker の links には複数の file のノードが key として入る (参照した宣言の型の解決で lib.d.ts の型ノードに `typeNodeLinks`、宣言に `signatureLinks` など)。checker 自身の Store (synth Store、遅延 JSDoc の Store) のノードも入る。
- **paged の key を 1 本の整数に詰めない。** 前回 (GlobalRef) は `uint64(file)<<32 | NodeRef` を key にしたので page 番号 (`key >> 8`) が file 1 で 2^24 を超え、`maxPageCount` (65,536) を必ず超えて全部が pageMap (map) に落ちた。file と NodeRef をそれぞれ slice の index にすれば map を通らない。map の 9 本は key の詰め方を問わない。
- **source 以外の Store (合成の Store、遅延 JSDoc の Store) には本体と別の StoreID を program の counter で振る** (Y3、上)。遅延 JSDoc の Store に本体と同じ番号を振ると NodeRef が重なって links が衝突する。合成の Store の `NodeKey` は checker の持ち物にしか入らず、共有構造に漏れない (設計文書 §2.9)。(2026-09-23 の当初案は「checker の中で固有の file index を振る: synth Store は `nFiles`、遅延 JSDoc の Store はその後ろ」で、Y3 で置き換えた)
- 検証: `LinkStore[store.NodeKey, V]` の lookup が 64bit の fast path (`mapaccess1_fast64` 相当) に乗ることを objdump で確かめる。乗らなければ key を `uint64` にする (map の側は詰めてよい)。
- Store は seal 後に書かない (不変条件 2.8.5) ので、links を予約 slot に置く案は成り立たない。links は checker ごとの持ち物。

計測 (2026-09-23、compiler プロジェクト、check 後の単一 checker、NodeCount 959,119、`_fieldcount/links-20260923.tsv`):

| link | 形 (Pointer) | 持つノード | 密度 | Get (+ Has) | 値 |
| --- | --- | ---: | ---: | ---: | ---: |
| `symbolNodeLinks` | paged (`GetNodeId`) | ≤ 約 31 万 (page 1,206 × 256) | ≤ 32% | **3,299,149** | 8B |
| `typeNodeLinks` | map | 86,646 | 9.0% | 239,097 + 17,031 | 32B |
| `signatureLinks` | map | 67,582 | 7.0% | 311,913 | 24B |
| `assertionLinks` | map | 6,214 | 0.65% | 13,480 | 8B |
| `nodeLinks` | map | 5,339 | 0.56% | 54,176 | 8B |
| `enumMember` / `arrayLiteral` / `switchStatement` / `computedName` | map | 2,074 / 1,206 / 349 / 346 | < 0.3% | 計 24,632 | 24〜56B |
| `jsxElementLinks` | map | 0 | 0 | 0 | 40B |

node キーの呼び出しは計 約 406 万回で、81% が `symbolNodeLinks`。Pointer ではこれが `GetNodeId` の atomic な遅延採番を払う。この入力で pageMap に落ちた呼び出しは 0 (前回の 0.77G はプロセスの id が 1,677 万を超える VS Code 規模)。

| 採らなかった案 | 理由 |
| --- | --- |
| L2 全部 file ごとの密な 2 段 (`links[file][NodeRef]`) | 値を全部並べると 248B × 959k ≈ 238 MB/checker。密度 1% 未満の link が大半 |
| L3 全部 file ごとの paged | 疎な link で page を無駄に取る。上限 (page 3,746 × 256 × 値) は `typeNodeLinks` 31 MB、`signatureLinks` 23 MB (map なら数 MB)。実分布は測っていない |
| 予約 slot | Store は seal 後に書かない |

#### symbol の id

**Pointer と同じ遅延採番を別の field で持つ (I1)。** (2026-09-23、ユーザー) **(2026-09-24 訂正: `gid` は `atomic.Uint64`。Pointer の `ast.SymbolId` は uint64 (ast/ids.go:5) で、32bit に縮めると周回で未採番の 0 と過去の id を再発行し、型の cache の key で別の symbol を混同する。当初の「32bit を padding に入れて 96B のまま」は誤り。`Symbol` は D2 と合わせて 112B)** `store.Symbol` の `id uint32` の後ろに `gid` を置き、`GetSymbolId` はプロセス全体の atomic 採番のまま。7c の file 内の密な `id` は Bound の参照用だけに残す。private 名 (`#<id>@`) は bind と check の両方で `store.GetSymbolId` の gid を使う（2026-09-26 訂正）。symbol キーの map の 14 本は Pointer と同じく `*Symbol` を key にする。

checker の `GetSymbolId` 19 箇所の用途: `valueSymbolLinks` (paged、Get 1,730,805 回、page 829)、型の cache の hash key (`keyBuilder.writeSymbol`)・`EnumRelationKey`・`symbolTableID`・nodebuilder の map、`compareSymbols` の最後の比較 (utilities.go:389、宣言が無く名前も同じ symbol の順序)、unique symbol の型の名前 `__@name@<id>` (checker.go:23346)。I1 でも Pointer と Store の番号・採番順が一致する保証はない（2026-09-26 訂正）。counter 初期値、bind や事前処理による採番、要求順が独立だからである。等価は symbol の一対一対応で検証し、内部名の ID は対応する symbol として照合する。任意の ID fallback の符号一致は契約にせず、各側の比較の性質と、同じ実操作における候補選択・出力順序を別途検証する。公開結果の差を並べ替えや数字の一括除去で隠さない。生の診断・印字文字列は完全一致を維持する。具体的な規則と段別のテストは [レビュー契約 §1〜2](store-checker-review-contracts-20260926.md)。

将来の最適化の候補 (ユーザー): `(file index, file 内の id)` にする。
- file index は program が作るときに密に振る (`Store.file`) ので atomic は要らない。transient symbol (checker が作る) は checker 内の枠で番号を振り、checker ごとの持ち物なのでこれも atomic は要らない。つまり採番の atomic は全部消える。
- 今の遅延採番の競合は「id を初めて求められた symbol の数」に比例し、呼び出しの数には比例しない。この入力では採番は最大約 21 万 (page 829 × 256)、呼び出し 173 万の大半は `Load` だけ。共有の counter と symbol の cache line への書き込みは symbol ごとに 1 回で、並列の律速になっている根拠は無い (測っていない)。移す利得は、bind 後の共有 symbol への書き込みが消えて symbol が完全に読み取り専用になることの方。
- 移すときの費用は等価の側: `compareSymbols` の最後の比較の順序と `__@name@<id>` が Pointer とずれる。ただし Pointer の遅延採番は並列 check では要求の順が goroutine の競争で決まり、この順序はもともと非決定的なので、`(file, id)` はむしろ決定的になる。

| 採らなかった案 | 理由 |
| --- | --- |
| I2 今すぐ `(file, id)` にする | 等価テストに順序と名前の正規化が要る。消せるのは atomic の load と symbol ごとに 1 回の採番で、Pointer も払っている費用。上の将来の候補として残す |
| I3 id を捨てて `*Symbol` を key に | `valueSymbolLinks` の 173 万回が map を引く。hash key (`keyBuilder`) に整数が要るので id は結局要る |

#### locals の表

**map のまま (7c の `Bound.locals []SymbolTable`、`SymbolTable = map[string]*Symbol`) (K1)。** (2026-09-23、ユーザー) 7c 設計 §2.3 の保留「map をやめるかは 7d で `Resolve` の実測で決める」をここで閉じる。

- `Locals()` は check で 230 万回、60% が locals を持てない kind (名前解決の祖先上り)。7c の `LocalsSlot()` は kind の表と比較 1 回で不在を返し、map を引かない。前回の 0.78G は `map[NodeRef]` を毎段 miss probe した分で、この形では消えている。**検証: `Resolve` の祖先上りで不在の kind が map を引かないことを命令列と Check の内訳で確かめる。**
- container での名前の探索は Pointer も `map[string]*Symbol` で、費用は同じ。

| 採らなかった案 | 理由 |
| --- | --- |
| K2 locals を map 以外に (小さい表の線形探索、intern した名前 id) | Pointer と形が変わり変換規則か手書きが要る。Pointer 比の損が無いので 7d で入れる理由が無い。設計文書 §8 の「識別子の名前ハッシュを 1 回に」の候補として残す |

### 2.5 Symbol と program 横断

#### `store.Symbol` の宣言の持ち方

**`store.Symbol` の `Declarations` を `[]Node`、`ValueDeclaration` を `Node` にする (D2)。7c 設計の不変条件 9 (「`Bound` と `Symbol` に保存するノードの手掛かりは `Ref` だけ」) を覆す。** (2026-09-23、ユーザー: 遅ければ後で直す) N1' (§2.3) で checker の struct は `Node` にしたので、checker で `Ref` を `Node` に戻す必要があったのは実質この 2 field だけだった。D2 でその経路が checker から消え、free 関数 (ast の utility、checker/utilities.go) に Store の表を届ける仕組みも要らなくなる。`Ref` は map の key (§2.4) と `FileRef()` に残る (2026-09-24 に Y3 で `NodeKey` と `Key()` に改名)。

- 7d で storebinder (手書き、§2.1) の `Declarations` / `ValueDeclaration` の書き込みと、bind の等価テスト (`Ref.file` の検査など) を手で直す。`Declarations []Node` は 1 要素 16B (`singleDeclarationsArena` も `core.Arena[Node]`)。`store.Symbol` は 96B → `ValueDeclaration` が +8B、`gid` の 64bit (I1 の訂正) が +8B で 112B。大きさは計測値として報告し、固定の条件にしない。
- **synth Store のノードを共有の symbol (bound symbol) に入れない** (設計文書 §2.9) は、`Ref` でも `Node` でも同じく守る。checker の transient symbol は checker の持ち物なので入れてよい (jsx.go:846 の `childrenPropSymbol.ValueDeclaration` が synth ノード)。
- **検証**: 7c の retained / GC ベンチ (`BenchmarkStoreBindRetainedV1`) を D2 で取り直し、7c の値 (checker.ts retained 0.561、mark CPU 0.10) と並べる。見積り: 宣言は compiler プロジェクトで約 6 万〜26 万個 (7c 実測の checker.ts 0.066/node から corpus の 0.27/node の範囲)、pointer +13 万〜52 万本、+0.5〜2 MB。checker.ts で mark CPU の比が 0.10 から 0.13 前後 (未測定)。

計測 (2026-09-23、compiler プロジェクト、check 1 回、`_fieldcount/declcount-*.tsv`): `ast.Symbol.Declarations` / `ValueDeclaration` の読みを使い方で分類。

| 使い方 | 回数 | 解決 |
| --- | ---: | --- |
| `len(Declarations)` | 10,042,298 | 不要 |
| `Declarations[i]` | 9,085,560 (うち `compareSymbolsWorker` 9,054,870。union のソートで enum member を比べる入力固有の偏りで、`GetSourceFileOfNode` 948 万回と同じ経路) | 1 |
| `ValueDeclaration` を変数に | 2,240,555 | ほぼ 1 |
| `ValueDeclaration == nil` | 2,011,448 | 不要 |
| `range Declarations` の要素 | 986,635 (うち `isDeclarationContainedBy` 925,582) | 要素ごと |
| `Every` / `Some` / `Find` / `FindLast` の引数 | 1,114,165 呼び出し | 要素ごと |
| `ValueDeclaration` を関数の引数に | 約 1.2M | 1 |

`Ref` のままなら解決は約 1,500 万回 (偏りを除くと約 650 万回)、1 回約 10 命令で Check +1.8% (+0.8%)。

| 採らなかった案 | 理由 |
| --- | --- |
| D1 7c のまま `[]Ref` / `Ref`、checker の表で解決 | Check +0.8〜1.8% に加え、free 関数に Store の表を届ける仕組み (変換規則で引数を足すか表の置き場。中央の registry は設計文書 §3 で退けた) がこの 2 field のためだけに要る |

#### accessor の命名の 2 層化

**7d-2 の変換より前に 2 層にする (A1)。** (2026-09-23、ユーザー) 7c 設計 §7 の保留をここで閉じる。id 層は型付き id の名前 (`SymbolId()` / `FlowRef()` / `LocalsRef()`)、解決層は Pointer と同名 (`Symbol() *Symbol` / `Locals() SymbolTable` / `FlowNode() *FlowNode`) にし、`Store` が `bound *Bound` を持つ。7d-1 と同じ時期に store 側の作業として行う (7d-1 も storebinder の等価テストを触る)。

- 今の API は 1 つの名前に 2 層が混ざる: `n.Symbol()` が `SymbolId` を返す (Pointer は `*Symbol`)、`n.FlowNode()` が `FlowRef`、`n.LocalsSlot()` が `(uint32, bool)`、実体は `Bound.SymbolOf` / `Flow` / `Locals(i)` と `store.GetLocals(bound, n)` (store.go:89-90、bound.go:119-123、views_generated.go:6050)。storebinder の呼び出しは 13 箇所。
- 2 層にすれば checker の `n.Symbol()` / `n.Locals()` の変換は恒等写像になる。今の名前のままだと変換規則が名前の対応表を持つことになり、規則 6 の「導けない知識」を変換器に持ち込む。
- 解決層は `n.s.bound` の load が 1 回増える。Bind ゲートは Compact 後 0.984 で余裕がある。解決層は `Seal` の後だけ使える (bind 中は slab が動く)。`storeChecks` build で検査する。

| 採らなかった案 | 理由 |
| --- | --- |
| A2 今の名前のまま変換規則で付け替える | 名前が同じで型が違う API が残り、変換規則が名前の対応表を持つ |

#### 解決層の返す値 (A1 の見直し)

**解決層は所有者を運ぶ値を返す (V1)。** (2026-09-24、ユーザー。上の A1 の「解決層 `FlowNode() *FlowNode`」を置き換え。名前を Pointer と同じにする点は保つ) `FlowNode()` は `Flow{bound, ref}` を返し、`Flow` の `Node()` / `Antecedent()` / FlowData は運んできた Bound で解く。header の 32bit (`flow` / `symbol` / locals の slot) は、source の Store では `s.bound` の番号、合成の Store ではその Store の私有表 (`flowImports []Flow`、`symbols []*Symbol`、locals の map の表) の番号。変換規則は Pointer の `*ast.FlowNode` → `store.Flow` の 1 本。形は [動作設計](store-checker-execution-design-20260924.md) §1。

- 合成ノードが source の Bound の flow や checker の symbol / locals を持つ箇所: `getSyntheticElementAccess` の `result.FlowNode = parentAccess の flow` (checker.go:18203、この入力で約 470 回)、`this.x` の参照の `constructor.ReturnFlowNode`、jsx.go:848 の transient symbol、`fakeScope` の locals (nodebuilderscopes.go:145、後で書き換わる map なので同一性を保つ)。
- 7c の `FlowNode.Node` は `NodeRef` で、解く Store は flow が属する Bound から決まる。裸の `*FlowNode` では解く先が分からない。
- 費用: `FlowNode()` / `Symbol()` / `Locals()` に source か合成かの分岐 (`s.bound != nil`) が 1 つ入る。check の読みは `FlowNodeData()` 32 万、`Symbol()` 90 万、`Locals()` 230 万回 (60% は kind の表で門前払い、accessor 動的頻度 §5.5)。7d-0 の Bind KPC と 7d-2 の Check KPC で測る。flow を辿るループ (flow.go) は `Flow` (2 word) を持ち回る。
- 私有表は scan される側表に置き、noscan の header / extra に pointer を入れない。binder を通らない遅延 JSDoc の Store は表が空で、未設定の slot 0 は nil を返す。

| 採らなかった案 | 理由 |
| --- | --- |
| V2 `FlowNode.Node` を `Node{s,h}` にする | 7c で noscan にした flow の slab に pointer が入り scan の対象になる。16B → 24B。7c の決定を覆す |
| V3 合成ノードに付ける箇所だけ手書きの側表 | flow の読み手が毎回側表を見ることになり、手書きが flow の中心に入る |

#### 診断の file

**`ast/diagnostic.go` も変換して `store.Diagnostic` (file は `*store.File`) を出す (G1)。** (2026-09-23、ユーザー) checker の診断はこの型で持ち、等価テストは file を path で対応させて比べる。collection のソートと file ごとの分類 (diagnostic.go:262〜372) が Pointer と同じ意味で動くので、順序まで比べられる。

- storeparser / storebinder の診断 (`ast.Diagnostic`、file = nil の 15 箇所) は 7d では触らない。checker の `GetDiagnostics` が返すのは checker が作った診断だけで、parse / bind の診断と合わせるのは program の仕事。7b / 7c のテストは (pos, len, code, message) の比較のまま。
- program・LS・出力側が Store の診断を読む日には `ast.Diagnostic` への変換か program 側の対応が要る (7d の範囲外)。
- `ast` package の差分 0 の規則により、`ast.Diagnostic` の file の型を変える案は取れない。

| 採らなかった案 | 理由 |
| --- | --- |
| G2 `ast.Diagnostic` を file = nil で使い file を別に持つ | collection のソートと file の分類が nil で壊れ、順序を比べられない。file ごとの取り出しに別の仕組みが要る |

### 2.6 checker が作るノード

#### nodebuilder の出力

**出力も Store にする (NB1)。** (2026-09-23、ユーザー) nodebuilder は checker の synth Store に組み、print の直前に Store → Pointer の変換器で Pointer の木にして printer に渡す。 **(2026-09-29 に NB1' で置き換え、ユーザー: Store → Pointer の変換器を作らず、printer を変換して `storeprinter` で Store のまま印字する。[7d-2 設計の残り](store-checker-7d2-design-remainder-20260929.md) §2.4。以下の変換器と EmitContext の記述は記録として残す)**変換規則は checker package 全体で一律 (`*ast.Node` → `store.Node`)。

- 型から「この `*ast.Node` は出力か source か」が分からない: nodebuilder は入力 (source) と出力 (factory) を同じ `*ast.Node` の変数で混ぜる (`setTextRange(range_, location)` nodebuilderimpl.go:1436 は `range_` が出力で `location` が source、`b.e.SetOriginal(output, source)`、`reuseNode` は visitor で変わらなかった source ノードをそのまま返す)。出力だけ Pointer に残す変換は規則で書けない。
- (2026-09-24 に K3 で撤回、下の「複製の規則」。記録として残す) **Store をまたぐ複製は深い複製にする。** Pointer の `node.Clone(factory)` (nodecopy.go:663、815、874、nodebuilderimpl.go:1440) は浅い複製で子を共有するが、synth Store の slot に source Store の ref は入れられない。変わるのは複製した部分木の中のノードの同一性だけで、出力のテキストは同じ。同一性を key にしているのは `idToSymbol` (複製の root だけ、nodebuilderimpl.go:1441) と `Original` の連鎖で、等価テストは message のテキストで比べる。
- `EmitContext` (printer、Pointer の型) のうち nodebuilder が使う部分 (`AddEmitFlags` 16、`SetEmitFlags` 9、`MostOriginal` / `SetOriginal` / `Original`、合成 comment など計 45 箇所ほど) の Store 版が要る。到達可能性の解析 (§2.1) で printer/emitcontext.go (1,067 行) から必要な分だけ変換する。
- Store → Pointer の変換器は schema から生成する (7a の Pointer → Store 変換器の逆向き)。Pointer の factory は子の `Parent` を書かない (ast_generated.go:4163) ので、変換器も書かない。
- 2026-09-26 補足: 比較用 Pointer source への対応表を変換器の必須依存にしない。計測用 Store プロセスでは必要な source / Original / text / range も Store から変換する。その一時 Pointer AST は実機能の費用として数える。
- 印字の性能は nodebuilder + adapter + printer + 文字列取得の全区間で別途評価する。実入力の型・symbol 要求列と診断あり corpus、cold / warm cache を使い、KPC inst / cycles、通常 GC の ns/op・B/op・allocs/op、retained を報告する。診断0の Check 合格で代替しない。内訳と採否条件は [レビュー契約 §4](store-checker-review-contracts-20260926.md)。
- `pseudochecker` / `modulespecifiers` (nodebuilder だけが使う、§2.1) は入力が source ノードなので変換の対象に含める。

| 採らなかった案 | 理由 |
| --- | --- |
| NB2 出力は Pointer のまま nodebuilder 群 7k 行を手書き | 7k 行が変換の外に出て、基盤の変更のたびに手で直す。§2.1 の方針の外 |
| NB3 7d-2 から nodebuilder を外して stub | message を比べられない (§2.1 の (b') と同じ) |

#### 複製の規則

**浅い共有に戻し、list の slot にも foreign の経路 (`ListEdge`) を持たせる。`NodeList.Clone` は要素の辺を写した新しい block にし、Clone した list への要素の書き込みは `storeChecks` で拒否する (K3)。** (2026-09-24、ユーザー。上の「Store をまたぐ複製は深い複製にする」を撤回) **実装コメント必須** (ユーザー): `NodeList.Clone` の実装と `storeChecks` の拒否箇所に、「Pointer の `NodeList.Clone` は要素の slice を共有するが、ここでは写す。Clone した list の要素を書き換えて元の list に見えることに頼るコードは到達範囲に無い (2026-09-24 の調査、nodebuilderimpl.go:3104〜3115 の書き換えは nodebuilder が自分で作った list)。頼るコードが増えたらこの検査で落ちる」の趣旨を書く。

**(2026-09-24 のレビューで判明した穴、2026-09-26 に決定: Clone の元と先の両方への書き込みを拒否する)** 「Clone した list への書き込みを拒否すれば、共有に頼るコードが増えたとき検査で落ちる」は成り立たなかった: `a=[x]; b=Clone(a); SetListElement(a, 0, y)` では Pointer の `b[0]` は `y`、K3 の Store は `x` で、書き込み先は Clone の**元**なので検査を通る。**`storeChecks` build は Clone の元と先の両方を region の側の集合に記録し、どちらへの `SetListElement` も panic させる。** (ユーザー) 到達範囲に Clone の元や先を書き換えるコードは無い (上の調査) ので、両方の拒否は今の到達範囲では何も落とさず、読みの費用も増えない。K2 (共有を保つ) は採らない理由 (全ての list の読みに alias の判定) が変わらない。

- Pointer の `node.Clone(factory)` は新しい root を作り子のノードと list を共有する (nodecopy.go:663、815、874、nodebuilderimpl.go:1440)。`Update*` は子と list の同一性を比べ、無変更なら元のノードを返す (ast_generated.go:4174)。ノードの共有は FR1、list の共有は `ListEdge` で表し、同一性は Pointer と同じになる。明示的な `DeepCloneNode` (nodebuilderimpl.go:3043 など) はそのまま。
- `NodeList.Clone` (ast/ast.go:147) の使い手は nodecopy.go:884 の 1 箇所で位置を変えるだけ。到達範囲で list の要素をその場で書き換えるのは nodebuilderimpl.go:3104〜3115 の `tupleConstituentNodes` だけで、nodebuilder が直前に自分で作った list。
- list の読みは `ListEdge` を解決してから、その list の owner の Store で要素を読む (再設計案 §3)。walker と手書きの utility もこの規則に従う。

| 採らなかった案 | 理由 |
| --- | --- |
| K1 深い複製のまま | 複製した部分木の中で同一性が変わり、子が source でなくなって名前解決や flow が source を辿れない場合が出る。FR1 で解いた問題が複製の経路で戻る |
| K2 `NodeList.Clone` に alias (list の長さの word の最上位 bit と `listElements` 表で格納先を共有、再設計案 §3) | 到達範囲に使い手の無い性質のために、全ての list の読みに alias の判定の費用をかける |

#### synth Store の前提

計測 (2026-09-23、compiler プロジェクト、check 中に `NodeFactory.newNode` が作ったノード): 計 13,724 (parse の 959k の 1.4%)。うち遅延 JSDoc (§2.2 の Store に置く) が約 8,000 (`JSDoc` 2,845、`JSDocText` 2,393、`JSDocUnknownTag` 1,773、`JSDocParameterTag` 945 …)、残りは `Identifier` 4,041、`StringLiteral` 476、`ElementAccessExpression` 472、`TypeReference` 101 など。診断の無い入力なので nodebuilder はほぼ動いていない。診断の多い入力では増える。

#### synth Store の物理的な形

**満杯なら次の Store を足す (R)。** (2026-09-24、ユーザー。下の C4' を置き換え) 形は [再設計案](store-checker-regions-design-20260924.md) §2・§4 と [動作設計](store-checker-execution-design-20260924.md) §1・§8 の Factory / NodeRegion: 公開した header は動かさず、番号も再利用しない。Store の全件表を持たず、参照 (Node、cache、foreign 表、Factory の割り当て先) が無くなった Store は GC に任せる。nodebuilder の `ReleaseArenas` は割り当て先を外すだけで、古い Store を消したり番号を使い直したりしない。

- C2 を退けた理由 (境界で子を深く複製し同一性が割れる) は FR1 で消えた: 境界をまたぐ辺も foreign ref で表せる。作った後の書き換え (古い Store のノードに新しい Store の親や子を書く) は古い Store の foreign 表に項目を足して表す。foreign 表は `Node` の値でアドレスを外に渡さないので伸ばしてよい。
- 正しさが容量の定数に依存しない。**7d-0 の storage テストで容量を 1〜3 にして境界を強制し、構造・共有・診断が変わらないことを確かめる。** 容量は性能の変数で、7d-2 で Store の数と使用率を数えて決める。
- 払うもの: Store ごとの StoreID の採番、links の page の枠 (§2.4 の file index を StoreID と読み替える)、境界をまたぐ辺の foreign 表の 1 段、合成ノードの `File()` は親を辿る (§2.3 F1 の見直し)。

| 採らなかった案 | 理由 |
| --- | --- |
| C4' (下、2026-09-23 に決め 2026-09-24 に置き換え) | 正しさが「容量が足りること」に依存し、容量を corpus の最大から決めても想定外の入力で落ちる。確保した容量が使わなくても heap に数えられる。checker の寿命まで全部を保持する。FR1 が入れば払う理由が無い |

以下は 2026-09-23 の決定の記録。

**checker ごとに 1 つの synth Store を、固定の容量で確保して伸ばさない。7d では解放もしない (C4')。** (2026-09-23、ユーザー) `nodes` / `extra` / `texts` のどれも再確保しない (checker は synth ノードを `Node{s,h}` で持ったまま次を足すので、再確保すると古い `h` が古い内容を黙って読む、設計文書 §5)。容量は 7d-2 で corpus 全体の checker あたりの最大数を測って決め、超えたら panic する。

- **file の切り替えで解放しない。** checker の寿命まで生きる合成ノードがある: `newCallSignature` の `FunctionTypeNode` は `Signature.declaration` として型の cache に入る (checker.go:30918)、jsx の children の `PropertySignature` は `childrenPropSymbol.ValueDeclaration` として links の型に入る (jsx.go:846)、`_jsxFactoryEntity` (jsx.go:1374)。links と cache の key が `(file index, NodeRef)` なので、番号を使い直すと古い link が当たる。
- 容量の目安: extra は 7c の実測 (checker.ts header 9.54 MB + extra 2.79 MB) から約 9.4 B/node、10 万ノードで約 4.1 MB/checker。この入力の合成ノード (遅延 JSDoc を除く) は約 5,700。
- 確保した容量は使っていなくても Go の heap に数えられ、GC の目標値と B/op の比較を歪める (noscan なので mark の費用は無い)。10 MB × checker 8 = 80 MB より、実測に基づいて 4 MB 前後から始める。
- 合成の識別子の名前は `texts` に追記する。`Text()` が `texts` の部分文字列を返すので、`texts` も固定の容量で確保する。

| 採らなかった案 | 理由 |
| --- | --- |
| C2 容量固定の小さな Store をつなげ、境界で子を深く複製 | 境界で同一性が割れ、割れる場所が容量の定数に依存する。起きても黙って結果が変わる。仕組み (境界の検出、深い複製、Store の列、Store ごとの file index) が多い |
| C1 synth Store の中を chunk に分けて `node()` で引く | `node()` が全 Store で 1 段深くなるか分岐が入り、hot path の全部が払う |
| file の切り替えで解放・再利用 (ユーザー案の一部) | 上のとおり checker の寿命まで生きる合成ノードがあり、links の key も衝突する |

既知の前提: checker 本体が作って型検査に戻す合成ノードは checker.go 8 (`this.x` 参照 5052〜5070、element access 18189〜18196、`NewSyntheticExpression` 30515、`NewFunctionTypeNode` 30918)、flow.go 6 (2469〜2496、`this.x` 参照)、jsx.go 2 (846、1383) の計 16 箇所。nodebuilder 群の構築 (`nodecopy` 27、`nodebuilderimpl` 17、`nodebuilder_hover` 4) は型を AST に戻す出口側。

#### 合成ノードから source への辺 (foreign ref)

**辺の値が Store の行数以上なら、最上位 bit を確かめて Store の foreign 表から正規の `Node` を返す (FR1)。** (2026-09-24、ユーザー) 合成ノードの子・親は source (または別の Store) のノードを複製せずに指し、同一性と意味は Pointer と同じになる。安全条件 ([再設計案](store-checker-regions-design-20260924.md) §3 の形): 印の無い範囲外の値は通常 build でも panic し、foreign 表の index にも bounds check を残す。parse の Store は foreign 表を持たず、辺はローカルのまま。

```go
func (s *Store) node(e NodeRef) Node {       // 型名は再設計案では NodeEdge
    if uint32(e) < uint32(len(s.nodes)) {
        return Node{s, &s.nodes[uint32(e)]}
    }
    if uint32(e)&foreignBit == 0 {
        panic("invalid local node edge")
    }
    return s.foreignNodes[uint32(e)&^foreignBit]
}
```

Pointer で合成ノードが source を指す箇所 (checker の `.Parent = ` を全部見た、2026-09-23):

| 箇所 | 親 → source | 子 → source | 読み手 |
| --- | --- | --- | --- |
| `getSyntheticElementAccess` (checker.go:18184〜、この入力で ElementAccess / StringLiteral 各 470 前後) | `result.Parent = node` | `lhsExpr = parentAccess` (source の式をそのまま子に) | flow の `isMatchingReference` が子を構造で比べ、子の識別子の resolved symbol と flow node を引く |
| `this.x` の参照 (checker.go:5052〜、flow.go:2469〜) | `= constructor` / `staticBlock` | name = `propName` | flow の照合、flow.go:111 の親の kind |
| `fakeScope` (nodebuilderscopes.go:146) | `= enclosingDeclaration` | — | 名前解決が fakeScope の locals から source の祖先へ上がる |
| `createSyntheticExpression` (checker.go:30514) | `= parent` | `tupleNameSource` | 診断の位置と file |
| jsx.go:846 | `= attributeParent` | — | 宣言として読まれる |

計測 (2026-09-23、scratchpad `foreign/`、上の形から bit 検査を除いた版。objdump と ns、5 run の最小): 親連鎖 1 段 9 → 11 命令 (+2: レジスタのコピーと遅い側を挟む `JMP`)、子の kind 読みも +2。親連鎖 64 段 95.2 → 93.7 ns、子の kind 読み 2.14 → 2.13 ns。inline の費用は `node()` 25、`Parent()` 32、子 40 (予算 80 の内側。遅い側を関数呼び出しにした版は `Parent()` が 86 で inline されなかった)。**この比較は採用の根拠にしない** (再設計案 §7): bit 検査の分岐、register pressure、list の解決、GC は実コードと Check の KPC で確かめる。

**この決定で見直した項目** (再設計案が置換範囲を §9 に挙げている。2026-09-24 に全部決定した):

- 深い複製 (§2.6「nodebuilder の出力」) → 浅い共有 (K3、§2.6「複製の規則」)。
- X2' (§2.2) → JSDoc の root の親は host への foreign ref (J-d、§2.2「遅延 JSDoc の root から上がる辺」)。
- C4' (§2.6) → 満杯なら次の Store を足す (R、§2.6「synth Store の物理的な形」)。
- F1 (§2.3) → 合成ノードの `File()` は親を辿る (§2.3 の 2026-09-24 の見直し)。
- list → list の slot にも foreign の経路 (`ListEdge`)、`NodeList.Clone` は要素の辺を写す (K3)。
- 合成ノード自身の意味情報 (`getSyntheticElementAccess` の `result.FlowNode`、jsx の合成ノードの `Symbol`) → 所有者を運ぶ `Flow{bound, ref}` と合成の Store の私有表 (V1、§2.5「解決層の返す値」)。

2026-09-23 の説明で「合成ノードは作った後で書き換えない」としたのは誤り: 作った後で `Parent`・位置・flow・locals・list の要素を更新する (`literal.Parent = result` など)。古い Store のノードに新しい Store の子や親を書く操作が要る。

| 採らなかった案 | 理由 |
| --- | --- |
| FR2 またがない (合成の root の親は nil、子は深く複製) | `getSyntheticElementAccess` と `fakeScope` の名前解決、flow の照合が黙って変わる |
| FR3 上の 5 種の関数を手書きで書き換え、合成ノードを作らない | flow と nodebuilder の中心に近い関数を手で保守する。`reference` は flow 全体で `*ast.Node` として使われる |

### 2.7 検証の設計

#### checker の等価

**testrunner の compiler / conformance の全テスト (compiler 6,832 本、conformance 5,899 本) の全構成を、H1 のハーネスで Pointer と Store の両方で回して突き合わせる (Q1)。** (2026-09-24、ユーザー) 比べるもの:

- (a) checker の生の診断: file、位置、長さ、code、category、message の連鎖、related information、suggestion (`GetSuggestionDiagnostics`)。program の後処理 (comment directive の除外、ソート) の前。
- (b) 型と symbol の walk: `tsbaseline/type_symbol_baseline.go` の walker と同じく、全ての式・識別子・宣言名で `GetTypeAtLocation` の型の文字列と `GetSymbolAtLocation` の symbol。**Store 用の walker は変換で作らず、Pointer 版を読んで同じ規則で手書きする** (変換で作ると比較の相手と同じ規則を通った同じ盲点の比較器になる、規則 6)。

根拠: 診断の無いコードでは診断の比較は何も検出しない (ゲートの入力の compiler プロジェクトも診断 0)。(b) が型や symbol の解決の違いと nodebuilder / printer の経路を広く通す。比較の相手の Pointer の checker は変換器とも Store とも独立した実装。除外は 7b / 7c と同じく理由ごとの機械的な条件で決め、テストが log する。

**E3 (到達可能性) の根**: `NewChecker`、`GetDiagnostics`、`GetSuggestionDiagnostics`、`GetGlobalDiagnostics`、`GetTypeAtLocation`、`GetSymbolAtLocation`、`TypeToString`、`NewNodeBuilder` (walker の型の文字列)、`IsTypeAny`。

| 採らなかった案 | 理由 |
| --- | --- |
| Q2 Store の出力をコミット済みの reference の baseline と比べる | baseline は program の診断や整形を含み、Pointer にも既知の差 (`.diff`) があるので、Store 固有の差だけを取り出しにくい |
| Q3 診断だけを比べる | 診断の無いコードでは何も比べず、型の違いが素通りする |

#### Check ゲート

**cycles ≤ 1.10 × Pointer。inst と IPC は報告。** (2026-09-24、ユーザー) Bind と同じ線。測り方は既存の KPC の契約と同じ (GC オフの Session、`SingleThreaded`、compiler プロジェクト)。基準は H1 の driver (checker を直接呼ぶ) で Pointer と Store を同じ binary で 5 run ずつ取る。旧基準値 `BenchmarkASTCheckKPCV1` (program 経由) は 2.60G cycles / 8.48G inst / IPC 3.26、936,205 node あたり 2,780 cycles / 9,063 inst で、新しい driver の値と並べて program 側の費用として報告する。

線を決める前の見積り (1 node あたりの inst。回数は accessor 動的頻度、単価は storeexp と FR1 の objdump):

| 項目 | 回数/node | 1 回の差 | 増減 |
| --- | ---: | ---: | ---: |
| 子の辺の解決 (typed field 17.1 + 多相 14.9) | 32 | +10 (32B の子の kind 読み 10 対 Pointer 2、FR1 +2) | +320 |
| 親の辺 (`Parent`) | 49.4 | +8 (32B で 9 対 3、FR1 +2) | +395 |
| nil 判定を終了条件にする親ループ（Z1） | 反復数未計測。Parent 全体の49.4と同一ではない | Pointer 比 +1〜2 命令/反復（kind load と、残る場合の h 判定） | 未計測。下の既存合計には未算入 |
| V1 の分岐 | 約 3.8 | +2 | +8 |
| `GetNodeId` が消える | 3.6 | −5 前後 | −18 |
| `GetSourceFileOfNode` が 1〜2 load に (入力固有の偏り) | 10.1 | −9 前後 | −90 |
| `AsXxx()` の型 assert が消える | 23.6 | −3 前後 | −70 |
| 多相 accessor が interface 呼び出しから表引きに | 14.9 | −5 前後 | −75 |
| `Node{s,h}` の 2 word 渡し | 未計測 | +2 / 境界 | ? |

既存の合計は 2 word 渡しと Z1 の親ループ終了判定を除いて約 +470 inst/node (+5%)、2 word 渡しを Walk の実績 (+8 inst/visit) から類推した旧見積りは inst +5〜10%。Z1 を含めた合計は未確定である。親ループの頻度を計数し、最終 objdump で kind load の共有と既存の辺解決見積りとの二重計上を除いて更新する。cycles はこれまで inst より低く出ている (Walk inst 1.22 / cycles 1.14、Bind 1.09 / 0.98、Parse 0.82 / 0.82。増えるのが依存の連鎖の外の整数命令だから)。前回の +10.5G の内訳に対して: map 2.7G は L1 (8B key、StoreID ごとの page) と K1 (kind の表での門前払い) で消える見込み、書き換えた関数 1.9G は逐語の変換 (手書きは制御構造を変えるものだけ) でほぼ消える見込み、accessor 7.5G は 32B header・表引きの多相 accessor・`Node{s,h}` で縮むが辺の解決の +8〜10 inst は残る。

線を超えたときの切り分けの順: (1) 2 word 渡しの境界の数と inline の変化、(2) 辺の解決 (`node()` の命令列) と nil 終了判定のある親ループ、(3) V1 の分岐、(4) links と map。親探索が寄与する場合は、[Z1 §6 の ancestors helper](store-node-zero-design-20260926.md) のように入口で一度だけ IsNil を検査し、ループ内は parent edge == 0 で終了する方式を候補にする。非零 edge が実ノードだけを指す不変条件と現在の Node の Store での解決を保ち、callback / 探索の意味を変えずに検証する。未測定の削減を見積りから先に差し引かない。

Z1 の ns 比較は通常 GC と高負荷環境の探索記録であり、採用根拠にしない。7d-0 は GC オフの KPC inst / cycles と生成 accessor / ctor の inline の before / after、後段の Check は同一 binary の Pointer 比で評価する。旧 Store 比だけでは Check ゲートを満たしたことにならない。

| 採らなかった案 | 理由 |
| --- | --- |
| ≤ 1.00 | 辺ごとの +8〜10 inst を cycles で完全に吸収する必要があり、見積りから厳しい |
| ≤ 1.15 (Walk と同じ) | Walk は辺の解決だけを測るので緩めにした。Check は辺の解決以外の仕事が大半で、同じ緩さにする理由が無い |

#### check の後の retained と GC

**retained ≤ 0.85 × Pointer、GC の mark CPU ≤ 0.75 × Pointer (check の後、compiler プロジェクト)。内訳 (AST、Bound、合成の Store、遅延 JSDoc の Store、foreign 表、links) を報告する。** (2026-09-24、ユーザー) 測り方は 7c の retained ベンチ (GC 2 回、gctrace の最後の強制 GC の行) を check まで伸ばす。 ただし Store 側に比較用 Pointer AST / program を保持しない隔離プロセスを使う。共通 fixture baseline、root の寿命、profile と構築経路の監査を [レビュー契約 §3](store-checker-review-contracts-20260926.md) に従って確認する。Pointer 分を差し引くだけの混在計測はこのゲートに使わない。見積りの前提 (parse + bind の比がこの入力でも 0.56 前後) は 7d-0 の retained の測り直しで先に確かめ、外れたら線を直す。

Pointer の実測 (2026-09-24、compiler プロジェクト、GC 2 回の後の `HeapAlloc`、3 run 同値、scratchpad `retained/`): parse +101.6 MB、bind +26.6 MB、check +106.4 MB、計 234.7 MB。強制 GC の wall は bind の後 10.0〜10.2 ms、check の後 23.4〜24.2 ms。

見積り: parse + bind の 128.2 MB が 7c の比 (0.56〜0.58) なら約 73 MB (parse の分には source の text や program の構造など両側共通の分も入るので、実際の比はこれより 1 に近くなりうる)。checker の状態 (+106.4 MB) は Store でもほぼ同じで、D2 (+0.5〜2 MB)、合成の Store、遅延 JSDoc の Store、foreign 表が少し乗る。計 約 181 MB、比 約 0.77。GC は AST の scan がほぼ消える (7c の mark CPU の比 0.10) ので check の後で 約 24 − 10 × 0.9 ≈ 15 ms、比 0.6 前後 (wall からの類推、mark CPU は gctrace で測る)。

| 採らなかった案 | 理由 |
| --- | --- |
| 報告だけ | 回帰を見逃す |
| retained ≤ 1.00、mark CPU ≤ 1.00 | AST の縮小が checker の状態に埋もれて消えても通ってしまう |

## 3. 不変条件

設計文書 §2.8 の 1〜6 と 7c 設計 §4 の 7〜14 を引き継ぎ、次のとおり改める・足す。

改めるもの:

- **2.8.3** (parse の Store の辺はすべてローカル、accessor に foreign 検査を入れない) → source の Store の辺はローカルのまま。辺の解決 (`node()`、list の解決) には foreign の経路がある (FR1、K3)。foreign の辺を持つのは合成の Store と遅延 JSDoc の Store だけ。
- **7c の 7** (header の `flow` / `symbol` は bind だけが書く) → source の Store について。合成の Store の header の 32bit はその Store の私有表の番号 (V1)。
- **7c の 8** (bind 完了後は Store に書かない) → source の Store と、公開した遅延 JSDoc の Store は seal 後に書かない。合成の Store は、持ち主の checker の Factory の writer (`SetParent` など) からだけ書ける。
- **7c の 9** (`Bound` と `Symbol` に保存するのは `Ref` だけ) → `store.Symbol` の `Declarations` / `ValueDeclaration` は `Node` (D2)。`Bound` の中の `NodeRef` (flow の `Node`、`containers` など) は、その Bound の source Store の番号のまま。
- **7c の 13 の `FileRef()`** → `Key()` (Y3)。

足すもの:

15. 公開した header の配列は動かさない。StoreID は使い直さない。Store の全件表 (registry) を持たない (R)。
16. 辺の値が Store の行数以上なら、最上位 bit を確かめて foreign 表を引く。印の無い範囲外の値は通常 build でも panic する。foreign 表には正規の `Node` / `List` を入れ、foreign 表の項目どうしの連鎖を作らない (FR1)。
17. checker の私有のノードを、共有の source Store、共有の symbol、file の遅延 JSDoc cache に書き込まない。
18. 合成の Store の Factory の `New` は子の `Parent` を書かない (Pointer の factory と同じ)。親は `SetParent` で明示的に書く。source の Builder は今までどおり constructor が子の `parent` を書く。
19. `NodeList.Clone` は要素の辺を写した新しい block を作る。Clone の元と先の両方への要素の書き込みを `storeChecks` で拒否する (K3、実装コメント必須)。
20. 構築の順は post-order だが、JSDoc ノードと JS の reparse が作るノードは例外で、依存してよいのは「root が最後のノード」(`Store.Root()`) だけ (B1)。
21. flow / symbol / locals の解決は所有者を運ぶ (`Flow{bound, ref}`)。`FlowNode.Node` は、その Flow の Bound の source Store で解く (V1)。
22. StoreID は、source の Store が 0..nFiles−1、それ以外 (遅延 JSDoc、合成) は program の 1 つの atomic な counter で振る (Y3)。
23. ノードどうしの比較は `SameNode`、map の key は `Key()` (nil は 0 に正規化)。`==` で `Node` を比べない (E1)。
24. 遅延 JSDoc は file ごとの共有 cache に RWMutex の下で parse し、完成して seal した Store だけを公開する。空の結果も完成済みとして記録する (J-d)。
25. `File()` は、source の Store と遅延 JSDoc の Store では Store が持つ `*File`、合成の Store では親を辿る。作成時に処理中だった file や `Original` から推測しない (F1 の見直し)。nil のノード (どの Store の番兵も) には nil を返す (Pointer の `GetSourceFileOfNode(nil)` と同じ。nil の判定を先に行う)。
26. **別の Store のノードを取り込む全ての入口** (Factory の `New` / `Update` / `Clone` の引数、`SetParent` の親、`SetListElement` の要素、`SetNodeFlow` の Flow) で、取り込むノードが同じ program (`Store.space`) の source か遅延 JSDoc の Store、または同じ region の合成の Store であることを通常 build で確かめる。StoreID は program の中でしか一意でないので、別の program のノードを取り込むと `NodeKey` が衝突する (2026-09-24 のレビューで追加)。
27. file の遅延 JSDoc cache (J-d) の host は、その file の source Store のノードに限る。それ以外の host (合成ノードなど) は通常 build で拒否する。key の `NodeRef` はその Store の中でしか一意でなく、合成の host を共有 cache に入れると私有のノードが共有側に流れる (不変条件 17)。合成の host で JSDoc を引く経路が見つかったら、共有 cache とは別の寿命の私有の経路を設計する (2026-09-24 のレビューで追加)。

## 4. TODO

2026-09-26 の4点レビュー（private 名、symbol 番号、Pointer AST を外すメモリ計測、印字変換の性能）は [追加契約](store-checker-review-contracts-20260926.md) に反映した。文書対応済みであり、実装・検証完了ではない。7d-0 / 7d-2 の完了条件は同文書 §5。

1. ~~7d-0 の実装指示書と検証指示書~~ 済 (2026-09-24): [実装指示書](store-checker-7d0-implementation-instructions-20260924.md)、[検証指示書](store-checker-7d0-verification-instructions-20260924.md)。
2. ~~7d-0 の before の計測~~ 済 (2026-09-27)。
3. ~~7d-0 の実装と検証~~ 済 (2026-09-28、[検証報告](store-checker-7d0-verification-report-20260927.md) §12 の修正後に合格)。compiler プロジェクトの parse + bind の retained 比は 0.567 で §2.7 の線の前提どおり。
4. ~~7d-1 の実装指示書と検証指示書~~ 済 (2026-09-28): [実装指示書](store-checker-7d1-implementation-instructions-20260928.md)、[検証指示書](store-checker-7d1-verification-instructions-20260928.md)。7d-1 の実装と検証も済 (2026-09-29、2341effa51。[実装報告](store-checker-7d1-implementation-report-20260929.md) §10 の巻き戻された reparse の修正を含む)。
5. ~~7d-2 の設計の残り~~ 済 (2026-09-29): [7d-2 設計の残り](store-checker-7d2-design-remainder-20260929.md)。checker の変換規則 R1〜R17、到達計算、P2、Store → Pointer の変換器と EmitContext、容量、(g)。7d-2 は 2a 変換 / 2b 等価 / 2c 性能の 3 小段に分け、指示書を小段ごとに書く。
6. ~~7d-2a の実装指示書と検証指示書~~ 済 (2026-09-29): [実装指示書](store-checker-7d2a-implementation-instructions-20260929.md)、[検証指示書](store-checker-7d2a-verification-instructions-20260929.md)。7d-2a の実装・検証・修正も済 (2026-09-30、[実装報告](store-checker-7d2a-implementation-report-20260930.md)、[修正報告](store-checker-7d2a-fix-report-20260930.md))。
7. 7d-2b の指示書。2b は 2 段に分ける (2026-09-30、ユーザー): 2b-1 はハーネスと 2b の最初の P2 を作って全テストを回し、不一致を原因ごとに分類して報告する。2b-2 はその分類を見て直し方を決めてから書く。

零値の扱い (2026-09-26 に設計案を追加。2026-09-28 に 7d-0 で実装済み。以下は設計時の記録):

- **[零値の設計 Z1](store-node-zero-design-20260926.md) を推奨する。** `Node{}` と各 Store の番兵を同じ nil として扱い、`IsNil` / `SameNode` / `Key` / `File` と nil を受ける境界だけを対応させる。通常の header 読みと `node()` は変えない。Node は先頭のゼロサイズ field で非比較型にし、view は同じ underlying type から導く。writer は番兵への書き込みを拒否する。命令列の差・時間測定の限界・KPC と変換規則の受け入れ条件は Z1 §6〜7。性能の採用判断は未了。現行コードの `IsNil()` は引き続き零値で panic する。

未決 (7d-1 / 7d-2 で扱う):

- ~~`ModifierFlags` の bit 16 以降~~ 閉じた (2026-09-29、[7d-2 設計の残り](store-checker-7d2-design-remainder-20260929.md) §2.1): Go の Pointer は bit 16 以降をどこでも立てないので、header の 16 bit で Pointer と同じ値になる。
- 子を返す accessor の返り値 (解決済み `Node` か `NodeRef` か) を決める根拠の「nil 判定だけ・転送だけの呼び出しの頻度」(設計文書 §2.5 の再検討 1) は数えていない。7d-2 の Check ゲートを越えたら不要。
- source と一致しない text を示す flag の置き場 (設計文書 §7)。
