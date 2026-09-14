# bind 走査連鎖のホットパス分析と削減案 (2026-09-14)

対象: Store 版 `bindKind → getContainerFlagsRef → bindChildrenRef → forEachBindChildGenerated → bindChildRef → bindKind` を、Original (`/Volumes/SanDisk1TB/ghq/github.com/microsoft/typescript`、HEAD をこの日ビルド) の `bind → GetContainerFlags → bindChildren → ForEachChild → visit → bind` と機械語で比べ、ノード種別の頻度で重み付けした。

出典:
- ノード頻度: `fixtures/compiler/checker.ts` (298,054 ノード) と `fixtures/lib/dom.generated.d.ts` (109,605) を Store parser で読み、一時テストで数えた (テストは削除済み、数値は本文)。
- 機械語: `built/local/tsc` (Store、9/14 12:19 ビルド) と Original を `go tool objdump` で読んだ。命令数は「その経路で実行される命令」を聴き取りで数えた静的値で、±10% 程度の誤差がある。
- 既存ノートとの整合は末尾「試行済みとの関係」に書いた。同じ案を再提案しないよう `bind-original-vs-store.md` §8 と `binder-investigation-insights-20260910.md` を照合した。

## 結論

1. **走査の「データ取得」はすでに親でまとめている** (`Access*` が親 header を 1 回読み、子 slot を配列から連続で読む)。無駄は取得ではなく**判断**にある: 子 1 つごとに `bindChildRef → bindN → bindKind` へ降り、そこで 3 回の二分探索 switch (`bindKind`、`getContainerFlagsRef`、`bindChildrenRef`) と `FlagsAt` 2 回を通ってから、ようやく「このノードは何もしない」と分かる。
2. checker.ts の訪問 298k のうち **識別子 41.5%、それ以外のトークン 9.9%** で、この 51% は子を持たず bind 側の仕事も「flow を書く」「識別子の予約語検査」だけ。ところが現状は非トークンと同じ連鎖を通る。識別子 1 訪問の推定命令数は Store ≈ 260、Original ≈ 160。差 100 × 123.5k ≈ 12M で、74M の差の 17% がここ。
3. 非トークン 145k は連鎖の骨格だけで Store ≈ 180 命令、Original ≈ 130。差 50 × 145k ≈ 7M。空の子 slot 110k (33%) と空 list 65k (61%) がそれぞれ call 1 回 (13 / 15 命令) を払う (Original は inline の nil 判定 4 / 3 命令) → +2M。
4. 静的に数えられる連鎖の差は合計 ≈ 22M (74M の 3 割)。残りは宣言・container・symbol の経路 (既存ノートの通り) だが、**連鎖側は Original を下回るところまで削れる**: 識別子とトークンを親で処理し、3 段の switch を表引き 1 回にすると、静的見積りで Store 185M → 約 140M (−25%)、Original 111M に対して 1.67 倍 → 1.25 倍。

## 1. 訪問の内訳 (checker.ts)

| 区分 | 件数 | 割合 | bind 側で必要な仕事 |
| --- | ---: | ---: | --- |
| Identifier | 123,554 | 41.5% | flow を書く、予約語検査 (名前位置なら検査不要) |
| うち名前位置 (`isIdentifierNameRef` が真) | 25,682 | 8.6% | flow のみ |
| うち予約語 map を引く (2〜12 文字・小文字開始) | 48,352 | 16.2% | map lookup が走る |
| その他トークン (演算子・キーワード・リテラル) | 29,506 | 9.9% | **なし** (`this`/`super` 42 件のみ flow) |
| 非トークンノード | 144,994 | 48.6% | 連鎖 + 種別ごとの処理 |
| うち container | 11,250 | 3.8% | bindContainer |
| うち statement (`SetFlow` あり) | 24,245 | 8.1% | |
| 子 slot | 331,159 | | うち **空 109,974 (33%)** |
| list slot | 105,860 | | うち **空 64,600 (61%)**、要素 76,868 |

上位種別: PropertyAccessExpression 8.4% (3 slot、questionDot は 99% 空)、CallExpression 6.2% (questionDot 100% 空、typeArguments list ほぼ空)、BinaryExpression 5.5% (4 slot + 1 list のうち Type slot と modifiers list は常に空)、Block 3.2%、TypeReference 2.9%、VariableDeclaration 2.3% (4 slot 中 2 空)、Parameter 1.9% (5 slot 中 3.1 空)。dom.generated.d.ts は識別子 33%、Parameter 7.7% (5 slot 中 2.8 空)、空 slot 39%。

## 2. 連鎖の機械語 (Store vs Original)

| 段 | Store | Original |
| --- | --- | --- |
| 親→子の呼び出し | `bindChildRef` (36 命令の関数、経路 23): stack check 4、frame 3、`ref==0` 1、`b.store==nil` 1、`nodes` bounds 3、kind load 3、引数 2、CALL、復帰 3 | `visit` は `ForEachChild` に inline: field load、CBNZ、closure fn load、ctx、CALL (R) ≈ 10。nil の子は 4 命令で終わる |
| `bindKind` 入口 | 144B frame、引数 4 本を spill、kind の**二分探索** (`CMPW $227; BGT` … 深さ 6〜8) | 128B frame、同じ二分探索 (Go は疎な switch を jump table にしない) |
| Identifier case | `SetFlow` CALL → 中で `flowID` CALL (2 段、≈40 命令: nil 2、`mustMutate` 3、lastFlow 比較、`putCol` の len/grow 判定・bounds) | 型 assert 5 + write-barrier 付き store ≈ 12 |
| 予約語検査 | `checkContextualIdentifierRef` ≈ 155: `FlagsAt` 11、`isIdentifierNameRef` CALL ≈ 45 (`ParentRef` 12 + `KindAt` 9 + switch、名前位置では `nameRefGenerated` CALL +40)、`TextAt`/`internText` ≈ 30、`GetIdentifierToken` 8、map lookup 39% | ≈ 100: `IsIdentifierName` ≈ 26 (parent は pointer load)、`Node.Text` ≈ 22、以下同じ |
| 末尾 | `FlagsAt` 11 + `kind > LastToken` + seenParseError 保存 3 + `getContainerFlagsRef` CALL (二分探索、≈18) + `bindChildrenRef` CALL + 復元 7 | `Flags` load 3 + 比較 + 保存 + `GetContainerFlags` CALL (≈18) + `bindChildren` CALL + 復元 8 |
| `bindChildrenRef` | ≈ 40: inAssignmentPattern 保存 3、unreachable 比較 4、statement 範囲 4、**3 回目の二分探索**、default → CALL | ≈ 34: 同じ構造 (statement では `FlowNodeData()` の interface CALL が加わる) |
| 子列挙 | `forEachBindChildGenerated` 4,840 命令・**592B frame**、jump table 11、`Access*` inline ≈ 20 (nil 2、bounds 4、kind 検査 3、shape 検査 4、slice 4)、子ごとに 3 + CALL | `Node.ForEachChild` 3,600 命令・368B frame、jump table 8、型 assert 5、子ごとに ≈10 (間接 CALL 込み) |

Store が Original より少ないのは「間接呼び出しが無い」ことだけで、段数 (6 vs 7) の差は `bindChildRef` の 23 命令で相殺されている。`forEachBindChildGenerated` は Go の "big function" (5,000 ノード超) 扱いで、中では cost 20 以下しか inline されない: `AccessIfStatement` は inline されるが `AccessParameter` / `AccessTypeParameterDeclaration` は CALL、`bindChildRef` も CALL のまま。これが 2026-09-10 の parent-argument 実験で `ChildRef` が CALL 化した理由と同じ。

## 3. 経路別の命令数見積り

| 経路 | Store | Original | 差 | 件数 | 差の合計 |
| --- | ---: | ---: | ---: | ---: | ---: |
| Identifier (式位置) | ≈ 265 | ≈ 160 | +105 | 97.9k | 10.3M |
| Identifier (名前位置) | ≈ 240 | ≈ 150 | +90 | 25.7k | 2.3M |
| その他トークン | ≈ 61 | ≈ 35 | +26 | 29.5k | 0.8M |
| 非トークン骨格 (子 1 つ) | ≈ 180 | ≈ 130 | +50 | 145k | 7.2M |
| 空の子 slot | 13 | 4 | +9 | 110k | 1.0M |
| 空 list slot | 15 | 3 | +12 | 64.6k | 0.8M |
| statement の `SetFlow` | 40 | 7 | +33 | 24.2k | 0.8M |
| **合計** | | | | | **≈ 23M (74M の 31%)** |

識別子経路の内訳 (Store、式位置): 親側 3 + `bindChildRef` 23 + `bindKind` 入口 21 + `SetFlow`+`flowID` 40 + `checkContextualIdentifierRef` 155 + 末尾 23。これが「ホットパス中のホットパス」で、1 訪問に **関数呼び出し 6 回** (`bindChildRef`、`bindKind`、`SetFlow`、`flowID`、`checkContextualIdentifierRef`、`isIdentifierNameRef`、名前位置ならさらに `nameRefGenerated`) と条件分岐 ≈ 45 本を使う。

## 4. 何が無駄か

1. **子の種別判断が子側にある。** 親 (`forEachBindChildGenerated`) は slot の役割 (式 / 名前 / トークン / 型) を静的に知っているのに、子ごとに header を読み、`bindKind` で 3 回 switch し、`isIdentifierNameRef` で**親を読み直して**「名前位置か」を判定している。`isIdentifierNameRef` の 45〜85 命令は、親が呼ぶ時点で答えが決まっている。
2. **トークンが非トークンと同じ入口を通る。** 29.5k のトークンに必要なのは `this`/`super` (42 件) の flow だけ。keyword type (`string`、`number` など、dom では数千件) も同じ。
3. **switch 3 回が二分探索。** `bindKind` (61 case)、`getContainerFlagsRef` (28 case)、`bindChildrenRef` (36 case) はどれも疎で jump table にならず、1 回あたり 6〜9 本のデータ依存分岐。3 回で ≈ 25 分岐、命令 ≈ 35。Original も同じ形だが、これは分岐予測ミス (discarded 12%) と fetch 幅不足 (delivery_bandwidth) の直接の源泉。
4. **`SetFlow` が 2 段 CALL。** `flowID` は cost 超過で inline されず、lastFlow 一致でも 14 命令。`putCol` の grow 判定は `PrepareBindTables` で列を確保済みなので bind 中は常に偽。`mustMutate` も bind 中は常に偽。
5. **空 slot と空 list が CALL。** `bindChildRef(0)` 13 命令 × 110k、`bindListRef(0)` 15 命令 × 65k。Original は inline の nil 判定。
6. **`Access*` の検査。** 生成 walker の case は kind を確定して呼ぶのに、accessor が kind と slot 数を再検査する (7 命令、分岐 4)。
7. **parse error 伝播の固定費。** `ThisNodeHasError` の `FlagsAt` 読み + seenParseError 保存/復元 + 末尾の or/TBZ が全ノードで ≈ 20 命令。parse diagnostics が 0 件のファイルでは `ThisNodeHasError` を持つノードは存在しない (`parseErrorAtRange` が diagnostics 追加と `hasParseError=true` を同時に行う唯一の経路) ので、ファイル単位で丸ごと省ける。
8. **`s == nil` / `id == 0` ガード。** すべての accessor に 2 分岐。bind 中は `b.store != nil` が不変。bounds check と合わせて `-B` 実験で Binder −4.65% / Binder+ast −6.72% の上限が測られている。

## 5. 提案 (効果の大きい順)

### A. 親が子の役割で分岐し、識別子・トークンは連鎖に入れない (推定 −13%)

生成 walker の各 slot に schema 由来の役割を持たせ、生成コードを次の形にする。

```go
// 式位置の slot (PropertyAccess.Expression, Binary.Left/Right, Call.Expression, ...)
if c := a.Left; c != 0 {
    k := s.nodes[c].kind           // 今も bindChildRef で読んでいる load
    switch {
    case k == ast.KindIdentifier:  b.bindIdentifierExpr(c)   // flow 直書き + 予約語検査 (親判定なし)
    case k <= ast.KindLastToken:   if k == This || k == Super { b.bindThisSuper(c) }  // それ以外は何もしない
    default:                       b.bindKind(c, k, kind)
    }
}
// 名前位置の slot (PropertyAccess.Name, 宣言の Name, QualifiedName.Right, BindingElement.PropertyName, ImportSpecifier.PropertyName, ...)
if c := a.Name; c != 0 {
    k := s.nodes[c].kind
    switch {
    case k == ast.KindIdentifier:        b.setFlowDirect(c)          // 名前位置は検査不要
    case k == ast.KindPrivateIdentifier: b.checkPrivateIdentifierRef(c)
    default:                             b.bindKind(c, k, kind)      // ComputedPropertyName, StringLiteral, BindingPattern
    }
}
// トークン専用 slot (OperatorToken, QuestionDot, Exclamation, Question, Asterisk, DotDotDot, AwaitModifier, ...)
// → 生成しない (this/super は式 slot にしか現れない)
```

- `bindIdentifierExpr` は「diagnostics 0 件 & Ambient/JSDoc でない → `TextAt` → `GetIdentifierToken`」だけ。`isIdentifierNameRef` (ParentRef、KindAt、switch、`nameRefGenerated`) は消える。名前位置の識別子 25.7k は検査自体が消える。
- `setFlowDirect` は `s.flows[c] = b.currentFlowID` (下記 D)。
- 識別子 1 訪問: 式位置 ≈ 265 → ≈ 90、名前位置 ≈ 240 → ≈ 20、トークン ≈ 61 → ≈ 10。合計 ≈ 24M (−13%)。
- 意味論: `isIdentifierNameRef` の真偽は親の kind と slot で完全に決まる (`isIdentifierNameRef` の case 一覧 = 名前 slot の一覧)。JSX の `ExportSpecifier`/`JsxAttribute`/`Jsx*Element` は「親がそれなら常に真」なので、それらの全 slot を名前扱いにする。`checkContextualIdentifierRef` の `len(b.file.Diagnostics())` 判定と Ambient/JSDoc flag 判定はそのまま残す。
- parse error があるファイルでは、トークン slot も `ThisNodeHasError` 伝播のために読む必要がある → G の `b.hasParseErrors` で分岐し、真のときだけ従来経路 (`bindChildRef`) を使う。
- 既存ノートとの関係: `luna-accessor-d1-parent-design-20260910.md` の D1 (識別子 case に親を渡す) の強い形。D1 は親 ref を渡して子側で判定するが、本案は判定自体を親の生成コードに畳むので `ParentRef`/`KindAt`/`nameRefGenerated` が全部消える。`parent-argument-experiment` が失敗した原因 (引数追加で walker が big caller 化し `ChildRef` が CALL 化) は、本案では引数を増やさず、walker から出る CALL を「1 子 1 回」に減らす方向なので当たらない。`bind-ident-contextual-bench.md` の「`checkContextualIdentifierRef` 全スキップ −12.5%」が上限で、本案はそのうち名前位置分 + 親判定分を診断を壊さず取る。
- 生成器: `tools/scripts/tsc/generate-go-ast.ts` の walker 出力に slot 役割 (式 / 名前 / トークン / 型 / list) を追加する。役割は既存 schema の型名 (`Expression`、`MemberName`/`DeclarationName`/`PropertyName`、`*Token`、`TypeNode`) から機械的に決まる。

### B. 予約語判定を parse 時に 1 bit で持つ (推定 −3.7%、pointer でも可能)

A の後も式位置の識別子 98k に `TextAt` (30) + `GetIdentifierToken` (8) + 39% の map lookup (≈70) ≈ 65 命令が残る。scanner はトークン化の時点で「この識別子はキーワード由来か」を知っている (`createIdentifierWithDiagnostic` で `p.token != KindIdentifier` のとき)。`NodeFlags` の空き bit (29〜31) に `IdentifierFromKeyword` を立て、binder は bit が立つときだけ map を引く。2026-09-10 に `IdentifierMayBeReserved` として −7.7% を測って **pointer 木でも同じことができるという理由で revert** している。Original との差を縮めるのが目的なら対象外、bind の絶対時間 (5 倍計画) が目的なら最も安い 4% なので、判断は目的次第。本文書では「A の後に残る識別子コストの 7 割」として記録する。

### C. 3 回の二分探索 switch を表 1 回 + 密な jump table にする (推定 −2.7%、分岐 −15/ノード)

```go
var kindInfo [ast.KindCount]uint8   // bit0: bindKind に case あり, bit1: bindChildrenRef に flow case あり,
                                    // bit2: statement 範囲, bit3-7: getContainerFlags の結果 index (Block/PropertyDeclaration/accessor は「動的」index)
```

`bindKind` は `info := kindInfo[kind]` を 1 回読み、`info&hasBindCase != 0` のときだけ既存 switch へ入る (切り分けた `bindKindCase` へ CALL)。`getContainerFlagsRef` は `containerFlagsTable[info>>3]` の load に置き換え、動的 3 種 (Block の親判定、PropertyDeclaration の initializer、accessor の object literal 判定) だけ関数を呼ぶ。`bindChildrenRef` も `info&hasFlowCase` で振り分ける。switch へ入る残り 1 割の kind は `case` を 0..N の密な index にして jump table 化する。Original も同じ 3 switch を持つ (parity の観点では両方に効く) が、Guided で見た discarded 12% と delivery_bandwidth の源泉なので、命令数以上に効く可能性がある。判定は kperf の cycles と xctrace の delivery/discarded。

### D. `SetFlow` を直書きにする (推定 −1.5〜2%、既に −1.4% 実測あり)

`b.currentFlow` の代入 57 箇所すべてで `b.currentFlowID = flow.id` も更新し (`NewFlow` が id を stamp 済み)、bind 中の書き込みは `s.flows[ref] = b.currentFlowID` (bounds check のみ)。`mustMutate`、`flowID` の lastFlow 比較、`putCol` の grow 判定が消える。`bind-store-only-ab.md` の `flowIDBind` + 直接 `flows[ref]` が −1.4% で wall flat のため不採用になっているが、A と C で残る命令が減った状態では相対効果が上がる。単独では再提案しない。

### E. 空 slot・空 list を親で落とし、walker 専用の無検査 accessor を使う (推定 −1.7%)

- 生成コードの `if c := a.X; c != 0` (A の形) で `bindChildRef(0)` の CALL 13 命令 × 110k が消える。`bindListRef` も `if l != 0` を生成側に出す (65k × 12)。
- walker の case は kind を確定しているので、`Access*` の kind/shape 検査を省いた `access*Unchecked` を生成して使う (7 命令・分岐 4 × 145k)。`AccessParameter` など CALL になっている accessor はこれで inline 判定に収まる可能性がある (cost を 20 以下に)。
- `syntax-scalars-experiment` (整数一括取得 −1.7%) と `direct-children-experiment` (−1.1%、frame 352B) はこの方向の実験で、frame 膨張が副作用だった。A で walker から出る CALL が減ると spill も減るので、再測定は A の後に行う。

### F. bind 中の `s == nil` / `id == 0` ガードと bounds check を binder 専用 accessor から外す (上限 −4.65〜−6.72%、Store 固有)

`binder-bounds-check-experiment-20260910.md` の `-B` 上限。実装は (1) binder が `nodes := s.nodes` を 1 度取り、`nodes[id]` に対して `id < uint32(len(nodes))` を bind 入口で 1 回だけ保証する unsafe 版 header アクセス、または (2) nil ガードだけ外した契約付き accessor (`store_bind_span.go` の `BindListSpan` と同じ「借用契約」の形)。安全性との取引なので、A/C/E の後に残る分を見て決める。

### G. parse error 0 件のファイルで error 伝播を丸ごと省く (推定 −1.6%、pointer でも可能)

`b.hasParseErrors = len(file.Diagnostics()) != 0` を bind 入口で 1 回決め、偽なら `bindKind` の `ThisNodeHasError` 読み・seenParseError 保存/復元・末尾の `SetFlagsAt` 経路を飛ばす。A のトークン省略もこの flag に依存する。

### 見積りの合計

| 案 | Store 185M からの削減 | parity |
| --- | ---: | --- |
| A 親側の役割分岐 | ≈ −24M (−13%) | Store 側の生成 walker の設計 (Original は汎用 visitor) |
| B parse 時 keyword bit | ≈ −7M (−3.7%) | pointer でも可能 (revert 済み) |
| C 表引き dispatch | ≈ −5M (−2.7%) + 分岐 −15/ノード | 両方に可能 |
| D flow 直書き | ≈ −3M (−1.5%) | Store 固有 |
| E 空 slot / 無検査 accessor | ≈ −3M (−1.7%) | Store 固有 |
| F ガード・bounds | ≈ −5M (−3%、上限 −6.7%) | Store 固有 |
| G parse error 短絡 | ≈ −3M (−1.6%) | 両方に可能 |
| 合計 | ≈ −50M → 135〜145M (Original 111M の 1.2〜1.3 倍) | |

A + D + E + F (Store 固有と設計改善だけ) で ≈ −19%。静的見積りは leaf 単位の実験では過大評価になった実績があるが、A は「関数呼び出し 6 回と分岐 45 本を 1 回と 5 本にする」種類の削減で、既存ノートが差の在処とした「走査の骨格」そのものを消す。

## 6. 検証の順序

1. A を生成器で実装し、`BINDER_KPC_CONFIG` の instructions 群で checker / dom の inst/op と cycles/op (両方が下がることが採用条件、`kperf-bind-measurement.md`)。20 入力の意味 digest 一致。
2. A の上に C、次に D/E/G。各段で inst と cycles を記録し、`accessor-review-measurement-20260911.md` と同じ A/A gate (inst ±1%、cycles ±1.5%)。
3. Instruments Guided (cpu-counter テンプレート) で bind 区間の discarded と delivery_bandwidth が下がることを確認する (C の分岐削減はここに出る)。
4. F は最後。安全性の設計 (`luna-accessor-d2-header-design-20260910.md` の header pointer 契約) と併せて判断する。

## 試行済みとの関係

- 不採用済みで本文書が再提案しないもの: `childKinds` 列、packed `{ref,kind}`、walker 分割、`ListSlotAt` inline、`ListElems`、ancestor stack、親 ref の引数渡し、Flags 再利用、`mustMutate` no-op 単独、`flowIDBind` 単独。
- 本文書の A は D1 (設計待ち) を置き換える。B は revert 済み案の再掲で、採用基準 (pointer parity) の判断を利用者に委ねる。C と G は `bind-original-vs-store.md` §8 の E3 (「頻出 kind の子走査を特殊化」) と同系だが、E3 は walker 側、本案は `bindKind` 側の 3 switch を対象にする点で異なる。
- 数値は静的経路計数であり、`binder-investigation-insights-20260910.md` §5 の教訓 (ソース上の操作数と動的命令数は比例しない) の通り、kperf で確認するまで採用判断に使わない。
