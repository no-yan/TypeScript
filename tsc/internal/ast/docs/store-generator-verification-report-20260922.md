# Store generator (TODO 7a) 検証レポート

作成日: 2026-09-22。手順は [store-generator-verification-instructions-20260922.md](store-generator-verification-instructions-20260922.md) (以下「検証指示」)、対象は `tsc/internal/ast/store/` と `tools/scripts/tsc/generate-go-store.ts`。比較対象は [storeexp-verification-report-20260922.md](storeexp-verification-report-20260922.md) (以下「前回」) の §12 (nil ガード後) の数字。

source は計測前に sha256 で固定し (`_store-generator-results/sha256-before.txt`)、終了時に不変を確認した。一時的に package に置いたテスト 2 本 (§10) は削除済みで、写しを `.txt` で残した。

KPC はユーザーが別ターミナルで 2 回実行し、1 回目 (load average 4.3) は無効、2 回目 (`kpc-20260922-1327.txt`) を判定に使った (§6.1)。

## 1. 結論

1. **正しさは全項目合格。** generator の再実行で差分なし、両 tag のテストが corpus 込みで通る (17,415 file / 2,584,467 node)。production から `store` は import されていない。等価テストは 178 定義 × 全 member (466 member、kind 展開で 487) と 7 役割 × 180 kind。
2. **役割表の逆方向検査で 2 kind の差を見つけた (§3.4)。** Pointer の `Text()` は `MetaProperty` と `JsxNamespacedName` で文字列を**合成**して返すが、Store の `Text()` は text member が無いので `""` を返す。生成テストは「Store の表にある kind だけ」を比べるので検出できない (前回 §3.3 の限界そのもの)。バグではなく仕様の差で、設計文書 §7 の未決事項に足す (§8)。
3. **inline は 1 つ不合格: `Node.Text` (cost 157)。** 実装報告のとおりで、役割 accessor の `Text()` を呼ぶ側は call 1 回 (prologue / epilogue 込みで約 11 命令) を払う。それ以外は期待どおり: typed view、member accessor、`AsXxx`、他の役割 accessor は全部 can inline、`visitList` は cost 80 ちょうど、`node()` の bounds check 1、slot 読み 1、役割表 0、`Refs()` 2。
4. **命令列は前回の予測どおり変わった。** 子の読みから `MOVW` (uint32 切り詰め) が消えて 11 → 10 命令。役割 accessor `Name` は 22 → 21。dispatch はジャンプテーブル、list slot の直後に `CBZW`、`visitList` は inline (`CALL` 0)。
5. **Walk (E1) のゲート 1 は合格。** KPC (checker.ts、20x × 5 run の中央値) で pointer 72.39 inst / 25.83 cycles、store **91.19 inst / 29.04 cycles** per visit。cycles 比 **1.124** で合格線 1.15 (≤ 29.5) の内側。inst は前回 92.53 → 91.19 (−1.34) で、期待の ±1 を 0.34 超えるが、§5.1 の `MOVW` 消滅 (k > 0 の slot 読み 0.996 / visit) に加えて k = 0 の slot 読み (0.470 / visit) でも `MOVD` の reg copy が 1 つ消えており、静的な合計 −1.47 で説明できる (§6.1)。ns/visit は checker.ts 1.129 倍、dom 1.044 倍 (参考値)。
6. **footprint は 32.95 / 34.18 B/node** (前回 32.62 / 33.71)。増分 +0.34 / +0.47 B/node は data word の実数 (checker.ts 25,024 word、dom 12,910 word) と 1 byte の誤差なく一致し、内訳は kind 別に出した (§7)。設計文書の試算 33〜35 の範囲内 (checker.ts は 0.05 下)。

## 2. 環境と入力

- commit `07d4703c7a` (branch `store-ast`)、未コミットの変更は `generate.ts`、`generate-go-store.ts`、`internal/ast/store/` だけ。
- go1.27.1 darwin/arm64、macOS 26.6.2、MacBook Air (M1、8 GB)。
- 負荷: load average 2.6〜3.5 (計測中)、1.9〜2.4 (終了時)。前回と同程度に高い。
- 入力: `fixtures.ASTBenchFixtures` (checker.ts 298,054 node、dom.generated.d.ts 109,605 node)、corpus は `testdata/fixtures` + `tests/cases/{compiler,conformance}` (17,415 file)。

## 3. 正しさ

### 3.1 検証指示 §1

| 項目 | 結果 |
| --- | --- |
| `node tools/scripts/tsc/generate.ts` の再実行 | 差分なし (全 15 file の sha256 一致)。`ast_generated.go` / `kind_generated.go` にも差分なし |
| `go build ./...`、`go vet ./internal/ast/...` | OK |
| `git status` | `generate.ts` (M)、`generate-go-store.ts`、`internal/ast/store/` だけ (+ この検証の `_store-generator-results/`) |
| `go test ./internal/ast/store/...` (tag なし) | PASS。`TestCorpus` 17,415 file / 2,584,467 node、3.6 s、`-short` なし、skip されたディレクトリなし |
| `-tags storechecks` | PASS (同じ件数) |
| production からの import | なし (`grep` の出力 0) |
| `go test ./internal/ast/ ./internal/parser/` | PASS |
| `hereby generate:ast` | `Herebyfile.mjs` の `generate:ast` は `generate.ts` を呼ぶだけなので、上と同じ経路 |

実装指示書 §1 のガード: package 名 `store`、`internal/ast` → `store` の import なし、`ast.json` / `schema.ts` / `generate-go-ast.ts` / `internal/ast` に変更なし、`storeexp` は残っている、`doc.go` は 5 項目あり、生成物 6 file の 1 行目は指定の形。`go:build` は `checks_{on,off}.go` だけ。

### 3.2 等価テストの範囲

| | 数 |
| --- | ---: |
| typed view を持つ定義 | 178 (payload を持つ 176 + Identifier / PrivateIdentifier) |
| view が覆う kind | 184 (shapes 表 182 + Identifier / PrivateIdentifier) |
| 比較する member (定義単位) | 466 (`comparedMembers`) |
| 同、kind に展開 | 487 |
| 役割の比較 (kind × 役割) | 180 (`comparedRoles`。Name / Expression / Type / Initializer / Body / Text / Modifiers) |
| 役割表 | 43 表 (`leftSlot` 〜 `classNameSlot`) |

`TestEquivalence` のログ「comparing 466 members and 180 roles per kind」の "per kind" は誤解を招く。総数である。

比較の中身 (`store_test.go` の `equivalent` + 生成の `compareMembers` / `compareRoles`) は実装指示書 §7 のとおり: header (kind / pos / end / flags / `ModifierFlags & 0xFFFF`)、親、`Ref()` 往復、定義ごとの全 member、役割 accessor (Store の表が `0xFF` でない kind、Pointer の panic は `guard` で検出)。

### 3.3 実装指示書からの逸脱の評価 (実装報告の 1〜8)

| 報告 | 現物 | 評価 |
| --- | --- | --- |
| 1. `SyntheticExpression.Type` (primitive `any`) は slot なし + TODO | `views_generated.go:3559`。generator は `any` を明示的に扱い、それ以外の未知型は `problems` に積んで失敗する | 妥当。checker が作るノードで corpus には現れない。§4.1 の表に無い型は `any` だけ |
| 2. `Attributes` を役割表から除外 | `shapes_generated.go:200` に注記。ImportAttributes では list、他 7 定義では child | 妥当。混在は表で表せない。`(*Node).Attributes()` 相当が要るなら kind 分岐になる (7b 以降の論点) |
| 3. slot 0 の定義に typed view なし | `EmptyStatement` 等に `AsXxx` が無い | 7a では無害。移植時に `AsKeywordTypeNode()` 等の呼び出しが出たら足す |
| 4. override 表に Body ← ClassStaticBlockDeclaration、Type ← JSDocVariadicType、Text ← JsxText | ast.go / ast_generated.go で確認: `ClassStaticBlockDeclaration` は `BodyBase` を埋め込まず `Body` field だけなので `(*Node).Body()` は nil、`Type()` に `JSDocVariadicType` の case なし、`Text()` に `JsxText` の case なし (panic) | 妥当。前回の `Expression` 3 kind と合わせて 4 表 7 kind |
| 5. `Node.Text` が inline されない (cost 157) | §4 で確認。`identifierText` 72 + `text` 42 + 表引きと分岐 | 期待 (§2 の表「views の全関数 can inline」) に反する。影響は §4 に書いた |
| 6. 多 kind は `allKinds()` の数、alias の constructor は kind を先頭に | `NewTypeAliasDeclaration(kind, …)`、`NewImportDeclaration(kind, …)` を含む 6 constructor が kind を取る。`JSTypeAliasDeclaration` / `JSImportDeclaration` は shapes 表にある | 実装指示書 §4.3 のとおり |
| 7. `package store_test` + `export_test.go` | `NodeAt` / `Footprint` / `NodeCount` / `IdentifierTextInTexts` / `Shape` の 5 つを export | 妥当。生成テストは `store_test` に置くしかない |
| 8. accessor の形は storeexp と同じ | `Name()` の diff は `n.h.data+uint32(slot)` → `int(n.h.data)+int(slot)` だけ。member accessor も同じ 1 行 | 指示どおり |

その他、実装指示書との照合: `SourceFile` と `JSDocParameterOrPropertyTag` の member 順 override と TODO あり、JSDocCommentBase の `text []string` は TODO (3 定義)、`textInTexts` の仮決めはコメントに明記、`unsafe` は `Ref()` と `Refs()` の 2 箇所、`NewToken` / `List` は手書き、`modifierFlags` は `ModifierToFlag` の OR、constructor は child と list 要素の parent を書く、Kind < 512 と slot ≤ 16 は generator が検査、`NodeHeader` 24 B はテストで固定。

### 3.4 役割表の逆方向 (追加の検査)

生成テストは「Store の表にある kind」だけを比べる。逆 (Pointer が値を返すのに Store は `0xFF`) を、一時テスト (`zz_reverse_test.go.txt`) で corpus 全体に対して 7 役割で検査した。Pointer が panic する kind / 役割 (665 組) は skip。

結果: Name / Expression / Type / Initializer / Body / Modifiers は差なし。**Text で 2 kind**:

| kind | Pointer の `Text()` | Store | 件数 |
| --- | --- | --- | ---: |
| `MetaProperty` | `Name().Text()` (合成) | `""` | 90 |
| `JsxNamespacedName` | `Namespace.Text() + ":" + name.Text()` (合成) | `""` | 124 |

どちらも text member を持たないので、役割表からは導けない。checker で役割版の `Text()` を `MetaProperty` に使うのは `checker.go:32131` の 1 箇所 (`node.Parent.Text() == "defer"`)、`JsxNamespacedName` には無い (他は `Name().Text()` / `Namespace.Text()` で Identifier を読む)。checker 以外は数えていない。Store 化のとき呼び側で `Name().Text()` に書き換えるか、`Text()` に kind 分岐を足すか (inline 予算をさらに超える) は 7b 以降で決める。設計文書 §7 に足す (§8)。

## 4. inline / bounds check

`_store-generator-results/inline.txt`、`bce.txt`。

| 対象 | 期待 | 結果 |
| --- | --- | --- |
| `views_generated.go` の全関数 | 全部 can inline | **686 / 687 が can inline。`Node.Text` だけ cannot (cost 157)** |
| 同、cost の分布 | | `AsXxx` 2、child / list / bool / kind の member accessor 15〜23、text member 39〜47、役割 accessor: child 55、list 39、`RawText` 70、`Identifier.Text` / `PrivateIdentifier.Text` 75 |
| `store.go`: `node` / header / `List` / `Ref` / `text` / `identifierText` | 全部 can inline | `node` 10、`Kind` 〜 `ModifierFlags` 4〜17、`List.IsNil` 〜 `At` 5〜50、`Ref` 16、`text` 42、`identifierText` 72。全部 can inline |
| `visitList` | can inline、cost ≤ 80 | **80 ちょうど** (前回と同じ。1 でも増えると外れる) |
| `builder_generated.go` の `NewXxx` | inline されなくてよい | 多数が cannot (89〜122)。`identifierData` 116、`modifierFlags` 102 も cannot。構築側なので問題ない |
| `assertKinds` | | cannot (83)。`storechecks` tag のときだけ `AsXxx` から呼ばれる |
| `node()` の bounds check | 1 | 1 (`store.go:48`) |
| slot 読み | 1、`MOVW` なし | 1 (`views_generated.go` の各 accessor 行)。child accessor は slot 1 + inline された `node()` 1 の 2 つが同じ行に出る。`MOVW` は §5 で消えたことを確認 |
| 役割表の読み | 0 | 0 (`Name` の `nameSlot[kind&511]` 行に bce なし)。不在側の `n.s.node(0)` に 1 (前回と同じ) |
| `List.Refs()` | ≤ 2 | 2 (`IsSliceInBounds` + `IsInBounds`) |
| `Text` の text 側 | | `text()` の 2 word 読みに 2、`src` / `texts` の slice に各 1 |

**`Node.Text` の影響。** 呼ぶ側は `CALL` + prologue 8 (stack check 3、frame 3、引数 spill 2) + epilogue 3 を払い、kind 既知でも分岐を畳めない。前回 §7.1b の text 14.06 inst (Identifier 専用の inline 版) は役割 accessor では再現しない。`Identifier.Text` (cost 75、inline) の src 経路は前回と同じ 12 命令なので、kind 既知の hot な呼び出し側が `AsIdentifier().Text()` を使う前提なら影響は限定的である。前回 §7.2 の見積り (Check で 4.6 text 読み / node) をそのまま使うと、役割版を使った場合の上限は +50 inst/node ≈ Check の +0.5%。直すなら「Identifier を src から読む経路だけ inline、残りを noinline の slow path に」だが、kind 判定 4〜6 + src 経路 12 + call 引数で 80 に収まるかは試していない。**ユーザーの決定 (2026-09-22、検証後): 当座は無視する。** generator に、inline されないこと、hot path の Identifier 経路が繰り返し呼ばれること、Store の結合後に計測して hot / slow の分割を検討することを `Text()` の関数内 TODO コメントとして出すようにした (`generate-go-store.ts`、`views_generated.go:5363`)。この変更は検証の後で、変えたのは `generate-go-store.ts` と `views_generated.go` のコメントだけ (`sha256-after-text-comment.txt`)。再生成で再現し、build / vet / `-short` テストは通る。命令列は変わらない。

## 5. 命令列

`_store-generator-results/asm-*.txt`。役割 accessor と `Parent` / `Ref` / `Refs` は単体 symbol が linker に落とされるので、一時テストの noinline ラッパ (`zz_verify_test.go.txt`) 経由で読んだ (`asm-wrap-*.txt`)。prologue / epilogue、panic 側、ラッパの引数 spill は除く。

### 5.1 子の読み (`BinaryExpression.OperatorToken`、slot 3)

```
ADD $3, R1, R1          ; data + slot
CMP R1, R3; BLS         ; extra の bounds check
MOVWU (R2)(R1<<2), R1   ; slot
MOVD R1, R4
CMP R3, R4; BCS         ; nodes の bounds check
UBFIZ $3, R1, $32, R1   ; ×8
ADD R1<<1, R1, R1       ; ×24
MOVH (R2)(R1), R0       ; kind
```

**10 命令** (前回 11)。`MOVW R7, R7` (切り詰め) が消えた。それ以外は同じ。`CallExpression.Expression` (slot 0) は `ADD` も無く、`Node` を返すので最後が `ADD base` になる (`asm-call-expression.txt`)。

### 5.2 `ForEachChild` の dispatch

```
MOVH (R1), R3; SUB $167, R3, R3; CMP $182, R3; BHI; ADRP; ADD $928; MOVD (R4)(R3<<3), R27; JMP (R27)
```

前回と同じジャンプテーブル (`SUB $167` / `CMP $182` も同じ)。kind 別関数は 167 で、126 が `CALL` (cannot inline、cost 89〜317)、41 が switch に inline されている (前回 132 call)。

### 5.3 list の走査 (`forEachChildBlock`)

```
MOVWU 20(R1), R3        ; data
LDP 24(R0), (R4, R5)    ; extra
CMP R5, R3; BCS
MOVWU (R4)(R3<<2), R3   ; slot
CBZW R3, 32(PC)         ; nil ガード  ← 前回 §12 の形
MOVD R3, R6; CMP R5, R6; BCS ...  ; Refs() (inline)
```

`visitList` は inline されている (`CALL .*visitList` は 4 file とも 0)。`Refs()` の本体は前回と同じ約 25 命令 (`MOVW` 2 つは `l.at+3` の uint32 演算で、指示の対象外)。ループ本体は `nodes` の `LDP` と `MOVD 48(RSP)` 等の reload を含み、前回と同じ。

### 5.4 役割 accessor (`Name`、在側)

```
MOVH (R1), R2; AND $511; ADRP; ADD; MOVBU (R3)(R2)   ; 表引き 5
MOVD R2, R3; CMPW $255, R3; BNE                       ; 0xFF 比較 3
LDP 24(R0); MOVWU 20(R1); ADD; CMP; BLS; MOVWU        ; slot 6
LDP (R0); MOVD; CMP; BCS; UBFIZ; ADD; ADD             ; node() 7
```

**21 命令** (前回 22: 表引き 5、比較 1、slot 7、`node()` 8 の数え方で、差は slot の `MOVW`)。不在側は 表引き 5 + 比較 3 + `LDP` / `CBNZ` (nodes の空検査) 2。`Modifiers` (list) は在側 表引き 5 + 比較 3 + slot 6 = 14。

### 5.5 `Text`

Identifier の経路 (kind 判定 → `identifierText` → src):

```
MOVH (R1), R2; UBFX $0, R2, $16, R3; CMPW $79, R3; BEQ  [; CMPW $80; BNE]   ; kind 4〜6
MOVWU 20(R1), R2; TBZ $31, R2                                              ; data と flag
LDP 48(R0); MOVW 12(R1); CMP; BHI; CMP; BHI; SUB; AND; SUB; ADD             ; src[data:end] 10
```

16〜18 命令、条件分岐 5〜6 (kind 2〜3、flag 1、slice 2)。`UBFX` は `int16` Kind の zero-extend (前回 §9.5)。

data word の経路 (literal):

```
kind 6; AND $511; ADRP; ADD; MOVBU; MOVD; CMPW $255; BEQ          ; 表引き 4 + 比較 3
MOVWU data; ADD slot; LDP; CMP; BLS; ADD $1; MOVWU off; CMP; BLS; MOVWU len   ; 2 word 10
TBZ $31, off                                                       ; src / texts
LDP; ADD; MOVW; CMP; BHI; CMP; BHI; SUB; AND; SUB; ADD             ; slice 11
```

約 35 命令、条件分岐 8。これに call の prologue / epilogue 11 が乗る。typed の `StringLiteral.Text` (inline) は kind と表引きが無く、`asm-wrap-asmStringText.txt` で 24 命令 (data 1、2 word 10、flag 1、slice 11、+ `LDP`)。

### 5.6 その他

- `Parent`: `MOVWU 16(R1); LDP; MOVD; CMP; BCS; UBFIZ; ADD; ADD` = 8。parent 連鎖 1 段 (ラッパ内のループ) は 12 で、`LDP` がループ内に残った以外は前回の 11 と同じ形。
- `Ref`: `MOVD (R0); SUB; MOVD+MOVK; UMULH; LSR` = 6 (前回と同じ magic 乗算)。
- bool member (`Block.MultiLine`): `LDP; MOVWU; ADD $1; CMP; BLS; MOVWU; CMPW $0; CSET` = 8。
- walker: Store 側に nil 検査なし、Pointer 側に `CBZ` 1 (前回と同じ非対称)。

## 6. Walk (E1)

### 6.1 KPC

**未取得。** §10 のコマンドをユーザーが実行した後、`summarize-kpc.py` で中央値を出して次の表を埋める。合格線は cycles/visit ≤ 29.5、inst は 92.53 ± 1。

| 条件 | 前回 (nil ガード後) | 今回 | 比 |
| --- | ---: | ---: | ---: |
| pointer inst/visit | 72.37 | | |
| store inst/visit | 92.53 | | |
| pointer cycles/visit | 25.67 | | |
| store cycles/visit | 29.20 | | |

§5.1 の `MOVW` 消滅から、store の inst/visit は child slot 1.111 / visit × 1 で約 −1.1 の見込み (91.4 前後)。

**2 回目 (`kpc-20260922-1327.txt`、判定に使用)。** load average 3.0。pointer の inst の幅 0.20% (基準 1.5% 内)、cycles の幅 2.4%。

| 条件 | inst/visit (幅) | cycles/visit (幅) | IPC | 前回 inst | 前回 cycles |
| --- | ---: | ---: | ---: | ---: | ---: |
| pointer | 72.39 (0.20%) | 25.83 (2.4%) | 2.80 | 72.37 | 25.67 |
| store | **91.19** (0.05%) | **29.04** (0.4%) | 3.14 | 92.53 | 29.20 |
| store / pointer | 1.260 | **1.124** | | 1.279 | 1.137 |

- **判定: 合格** (29.04 ≤ 29.5)。前回 (29.20、1.137) から cycles −0.16、比で −0.013。
- **inst は −1.34 / visit** で、期待 (±1) を 0.34 超える。内訳は静的に説明できる。`forEachChildBinaryExpression` を storeexp と diff すると (`asm-foreach-binary-storeexp.txt` と `asm-foreach-binary.txt`)、消えたのは (a) k > 0 の slot 読みの `MOVW Rn, Rn` (切り詰め) と (b) k = 0 の slot 読みの `MOVD R3, R6` (uint32 の index を 64 bit の比較用に copy する reg copy) で、どちらも `int(h.data)+k` にした効果である。checker.ts の実数 (`foreach_generated.go` の slot index × kind ヒストグラム) は k = 0 が 0.470 / visit、k > 0 が 0.996 / visit で、合計 **−1.47** の見込みに対して実測 −1.34。kind 別関数の静的な命令数も CallExpression 236 → 232、BinaryExpression 212 → 204、PropertyAccess 104 → 100 (Block 96 → 96) と減っている。call される kind 別関数の集合は storeexp と同じ 126 で、inline の境界は動いていない。
- cycles は −0.16 で、消えた命令が依存の無い整数命令だったことと合う (前回 §12 と同じ傾向)。IPC 3.17 → 3.14。

**1 回目 (`kpc-20260922-1322.txt`) は無効。** 実行中の load average は 4.3 で、pointer の inst の幅が 2.08% (基準 1.5%)、cycles の幅が 44% (26.5〜40.5) だった。検証指示 §4 の規定により測り直す。参考値 (20x × 5 run の中央値、括弧は幅):

| 条件 | inst/visit | cycles/visit | IPC |
| --- | ---: | ---: | ---: |
| pointer | 73.75 (2.08%) | 31.94 (43.9%) | 2.31 |
| store | 91.73 (1.16%) | 31.01 (5.1%) | 2.96 |

store の inst の中央値 91.73 (最小 91.33) は見込み 91.4 と合い、前回 92.53 から −0.80 で ±1 の枠内にある。ただし store 側も幅 1.16% (前回 0.06%) で、負荷が命令数にまで乗っている (pointer の最小 run 72.46 だけが前回 72.37 と一致する)。cycles は pointer 側が前回 25.67 → 31.94 に膨らんでいて比較にならない。

### 6.2 ns/visit (参考、`-benchtime 2s -count 5` の中央値、括弧は幅 = (最大 − 最小) / 中央値)

| fixture | pointer | store | store / pointer | 前回 (nil ガード後) |
| --- | ---: | ---: | ---: | ---: |
| checker.ts | 8.279 (6.4%) | 9.347 (22%) | **1.129** | 7.941 / 9.182 = 1.156 |
| dom.generated.d.ts | 5.480 (5.8%) | 5.719 (3.4%) | **1.044** | 5.248 / 5.549 = 1.057 |

store の checker.ts に 11.29 ns の外れ値が 1 run ある (負荷)。中央値は前回と同じ水準で、`MOVW` 1 命令の差は ns には出ない。

## 7. footprint (E10)

| fixture | node | header | extra | texts | B/node | 前回 | 差 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| checker.ts | 298,054 | 7,153,320 | 2,668,172 | 11,265 | **32.95** | 32.62 | +0.34 |
| dom.generated.d.ts | 109,605 | 2,630,544 | 1,115,688 | 24,269 | **34.18** | 33.71 | +0.47 |

B/node は前回と同じ式 (header + extra) / node で、`texts` を含めると +0.04 / +0.22。

`extra` の増分は checker.ts +100,096 B = 25,024 word、dom +51,640 B = 12,910 word。kind のヒストグラム (`histogram.txt`) × 定義ごとの data word 数 (`views_generated.go` の accessor 型から数えた) の合計が **どちらも 1 word の誤差なく一致**する (`footprint-breakdown.txt`)。内訳 (word):

| kind | data member | checker.ts | dom |
| --- | --- | ---: | ---: |
| Block | MultiLine 1 | 9,685 | 0 |
| NumericLiteral | Text 2 + TokenFlags 1 | 5,355 | 6,096 |
| StringLiteral | Text 2 + TokenFlags 1 | 3,042 | 5,541 |
| PrefixUnaryExpression | Operator 1 | 3,578 | 2 |
| ImportSpecifier | IsTypeOnly 1 | 1,148 | 0 |
| HeritageClause | Token 1 | 4 | 674 |
| ArrayLiteralExpression / ObjectLiteralExpression | MultiLine 1 | 707 | 0 |
| TemplateHead / Middle / Tail | Text 2 + RawText 2 + TemplateFlags 1 | 1,035 | 100 |
| TypeOperator | Operator 1 | 248 | 495 |
| その他 (PostfixUnary、RegExp、ImportClause、ModuleDeclaration、NoSubstitutionTemplate) | | 222 | 2 |
| 合計 | | **25,024** | **12,910** |

checker.ts の増分の 39% は `Block.MultiLine` で、bool 1 つに 4 B を払っている。設計文書の試算 33〜35 に対して checker.ts は 32.95 (0.05 下)、dom は 34.18 (範囲内)。

## 8. 設計文書への反映案

直してはいない。

1. **§7 の未決事項に「`Text()` の合成 kind」を足す。** Pointer の `Text()` は `MetaProperty` と `JsxNamespacedName` で文字列を合成する。Store の役割 `Text` は member 由来なので返せない。checker の 1 箇所は呼び側で書き換えるのが自然 (§3.4)。
2. **`Text` の inline 方針。** 役割 accessor `Text` は cost 157 で inline されない。当座は無視し (§4)、Store の結合後に計測して決める。設計文書 §2.5 の accessor 予算には「`Text` は例外、`Identifier.Text` が hot path の正」と書いておく。
3. **§2.4 の text の試算を実測に置き換える。** data word は checker.ts +0.34 B/node、dom +0.47 B/node で、bool 1 つ (`Block.MultiLine`) が最大項。bool / TokenFlags / operator を header の穴や 1 word に詰める案は、footprint では最大 −0.13 B/node (checker.ts の MultiLine 分) で、設計文書 §2.2 の「一様配置」を崩す価値は無い。
4. **`Attributes` は役割にならない** (child と list の混在)。checker の `Attributes()` 相当が必要になったら kind 分岐で書く。
5. **kind 別関数 167 のうち 126 が call。** 前回 132。cost 89〜317 で、`ForEachChild` の switch に inline されないのは前回と同じ。Walk の 2 word 渡し +8 (前回 §6.2) はここに由来し、generator では変わらない。

## 9. 限界

- KPC の 2 回目も load average 3.0 で取った。pointer の inst の幅 0.20% は基準内だが、cycles の幅 2.4% は前回 (3.8%) と同程度で、cycles 比 1.124 の有効桁は 2 桁と見るべきである。ns/visit は幅が 22% の条件があり、判定に使えない。
- 命令列の一部 (役割 accessor、`Parent`、`Ref`) は noinline ラッパ経由で読んだ。ラッパの引数 spill と、ループの `LDP` の配置はラッパの形に依存する (parent 連鎖の 11 と 12 の差)。
- 逆方向の役割検査 (§3.4) は 7 役割だけで、`Statements` / `Members` / `Parameters` 等の 36 表は forward の等価テストにも入っていない (実装指示書 §7 の仕様どおり)。
- corpus は parser が受理するものに限る。checker / emitter が作る合成ノード (`SyntheticExpression`、`PartiallyEmittedExpression` 等) の constructor は等価テストで実行されていない。
- 等価テストは JSDoc を辿らない (`Convert` の仕様)。JSDoc 系 kind の member 比較は corpus に JSDoc ノードが子として現れないので実質空である。
- footprint の内訳は kind × 定義の data word 数で、list block と escape 付き識別子の word は前回と同じ (checker.ts / dom とも identifier の escape は 0)。

## 10. 再現手順と生データ

生データは `tsc/internal/ast/docs/_store-generator-results/`: `env.txt`、`load.txt`、`sha256-before.txt`、`tests.txt`、`inline.txt`、`bce.txt`、`asm-*.txt` (単体 symbol)、`asm-wrap-*.txt` (ラッパ経由)、`walk-ns.txt`、`histogram.txt`、`footprint-breakdown.txt`、`reverse-roles.txt`、`zz_verify_test.go.txt` と `zz_reverse_test.go.txt` (一時テストの写し。`store.verify.test` は前者を含む binary)、`store.test`、`store.kperf.test` (固定した source から作った binary)、`summarize-kpc.py` (前回の写し。`BenchmarkStoreExp` の prefix を `BenchmarkStore` に読み替える)。

コマンドは検証指示 §1〜§5 のとおり。追加したのは一時テスト 2 本 (削除済み) だけで、実装は変更していない。

KPC (ユーザーが別ターミナルで実行):

```sh
cd /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/ast/store && \
sudo /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/ast/docs/_store-generator-results/store.kperf.test \
  -test.run '^$' -test.bench 'StoreWalkKPCV1' -test.benchtime 20x -test.count 5 \
  | tee /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/ast/docs/_store-generator-results/kpc-$(date +%Y%m%d-%H%M).txt
```
