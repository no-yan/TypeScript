# storeexp 検証レポート

作成日: 2026-09-22。手順は [storeexp-verification-instructions-20260922.md](storeexp-verification-instructions-20260922.md)、対象は `tsc/internal/ast/storeexp/`。

KPC (inst / cycles) は `_storeexp-results/kpc-20260922-0310.txt` (ユーザーが実行、20x × 5 run)。source は sha256 で固定したものと同一であることを、KPC の後にも確認した。inst の幅は全条件で ≤ 0.14% で、判定に使える。

## 1. 結論

設計文書の決定に反する結果が 3 つある (1〜3)。

1. **Walk (E1) は不合格。** checker.ts で pointer 72.38、switch 98.15 inst/visit、**1.356 倍** (合格線 1.10、予測 +8〜10%)。cycles は 26.24 → 30.07 で 1.146 倍、IPC 2.76 → 3.26。増分 +25.8 inst/visit は命令列と slot の実数でほぼ説明できる (§6): list slot の準備 +9.6 (うち 5.9 は **nil list をガードなしで読む**ことによる)、child slot の読みと `node()` +8.5、`Node` の 2 word 渡し +8、要素 +2.1、type assert と nil 検査の消滅 −3.4。直せる分 (nil ガード、`unsafe.Slice`、切り詰め) を全部直しても約 +17〜18 (1.24 倍) が残る見積りで、1.10 には届かない。
2. **役割アクセス (E3) は inst で 4 操作が不合格。** known +7.6 inst/子 (8.77 → 16.34)、polyPresent +11.0 (10.26 → 21.27)、list +33.9/list (19.55 → 53.42)、parentChain +8.7/段。合格は header (5.00 → 4.00)、polyAll (12.87 → 10.71)、text (35.06 → 14.06)、modflags (14.89 → ≈1)。静的な数え上げ (§5) と実測は ±1 命令で一致した。
3. **E11 は閾値の 2 倍。** parentChain は段あたり 3.26 → 11.95 inst (+8.7)、ref で回す書き方でも 9.27 (+6.0)。cycles は 3.72 → 7.99 / 7.08。Check への影響は上限 +4.2% (ref 版 +2.9%)。律速は依存連鎖 (load → ×24 → 加算 → load) で、書き方の規約で戻るのは cycles の 11% だけである。
4. **Check への影響の見積りは inst で +6.3% (上限、ref 版で +5.0%)。** 内訳は parent +426、known +303 が支配的 (§7.2)。
5. **ただし cycles は逆を向く。** 不合格の 4 操作のうち known (−20%)、polyPresent (−59%)、list (−18%) は cycles では Store が勝つ。同じ重みで cycles を足すと符号が負になる (§7.3)。**「操作ごとに inst で Store ≤ Pointer」という判定基準だけでは、この設計の採否を決められない。** 一方で、この microbench の cycles は Store に有利に偏る (全ノードを 1 回ずつ順に読む)。parentChain と Walk は inst でも cycles でも負ける。
6. **E2 / E4 / E5 / E6 は期待どおり。** accessor は全部 inline、`node()` と slot 読みの bounds check は各 1、多相表の読みは 0、dispatch は両方ジャンプテーブル、`Ref()` は magic 乗算で実測 4.00 inst。
7. **footprint (E10) は 32.62 / 33.71 B/node。** Pointer の 39% / 41%。下限である (§8)。

正しさは全項目合格。ただし検証中に実装が書き換わったので、ハッシュを固定してやり直した (§3.2)。

## 2. 環境と入力

| | |
| --- | --- |
| マシン | Apple M1、AC 電源 |
| Go | go1.27.1 darwin/arm64 |
| commit | 5647a91eb8 (storeexp は未コミット。`_storeexp-results/snapshot-before.sha` の sha256 で固定) |
| 入力 | `fixtures.ASTBenchFixtures` (checker.ts 298,054 node、dom.generated.d.ts 109,605 node) |
| 負荷 | **静かではない。** load average は計測前 3.98、後 2.92 (`XprotectService`、`mds`、他の claude process 3 本)。ns の幅が 5〜13% の操作があるのはこのためで、ns は参考値に留まる |

## 3. 正しさ

### 3.1 検証指示 §1

| 項目 | 結果 |
| --- | --- |
| generator の再実行で差分なし | OK (sha256 一致) |
| `-tags storeexp` | pass |
| `-tags 'storeexp storechecks'` | pass |
| tag なし `go build ./...`、`go vet ./internal/ast/...` | OK。`storeexp/` と `docs/` の外の変更は、作業前からある `kpc-before.txt` / `kpc-after.txt` だけ |
| `grep -L 'go:build' *.go` | `doc.go` だけ |
| ガード 5 項目 | 全部あり (package 名、build tag、`_gen` は `ignore`、`doc.go` の 5 項目、生成物の 1 行目、外部の変更なし) |

fingerprint は `ForEachChild` と `ForEachChildShape` の両方で baseline と一致した。

### 3.2 検証中に実装が変わった

実装 agent の報告を受けた後、02:50〜02:51 に `walk.go`、`store_test.go`、`bench_test.go`、`asm_test.go`、`_gen/main.go` と生成物が書き換わった (簡素化の一巡と思われる)。最初の計測は新旧が混ざったので捨て、02:57 に全ファイルの sha256 を取り、§1〜§6 を全部やり直し、終了時に不変を確認した。この文書の数字はすべて固定後のものである。

報告と現物の食い違い:

- 報告の 3「op 関数は asm ラッパ用に残置」は古い。現物は `opKnown*` / `opList*` / `opModFlags*` を削除し、本体を `asm_test.go` のラッパと `bench_test.go` の `sum*` ループの 2 箇所に書いている。両者が同じ本体であることは目で確認した (機械的な保証は無い)。
- override 表の `Expression` には、報告に無い `SyntheticReferenceExpression` も入っている。

### 3.3 実装指示書からの逸脱の評価

| 逸脱 | 評価 |
| --- | --- |
| `extra[0:3]` を 0 にした | 妥当。nil list を分岐なしで読める。`extra[0] == 0` は保たれる |
| JSDocParameterOrPropertyTag は宣言順で固定 | 実験では観測されない。**本採用時の論点として残る**: 固定 shape は `IsNameFirst` の順序入れ替えを表せない |
| known / list / modflags をループに書き下した | 妥当。inline 予算超過は再確認した (`sumKnownStore` cost 234、`sumListStore` 88、`sumModFlagsPointer` 90)。片側だけ call を払う形を避けている |
| `Store.Root()` | 妥当 |
| Store 側 walker に nil 検査なし | Pointer 側だけ `CBZ` 1 命令を払う。Walk の比較で Store に 1 inst/visit 有利に働く。小さいが、判定線 (+10% ≈ 7 命令) に対しては無視できない大きさである |
| 多相表は `init` で埋める | 読み側の命令列は静的表と同じ (`ADRP` + `ADD` + `MOVBU`)。確認した |
| KPC の probe | `kperf.Start` / `Stop` は対称 (`LockOSThread`、`GOMAXPROCS`、`SetGCPercent` を戻す) で、害は無い |

等価テストの限界: 多相 accessor の検査は「Store の表にある kind」だけで、Pointer の `Expression()` が値を返すのに Store の表に無い kind は検出しない (実装指示書 §7 の仕様どおり)。ベンチが使うのは `Name()` だけで、`Name()` は全ノードで双方向に比較しているので、今回の計測には影響しない。

## 4. inline / bounds check

| 対象 | 期待 | 結果 |
| --- | --- | --- |
| `node` (cost 10)、header のメソッド (4〜17)、`AsXxx` (2)、代表 kind の accessor (15 / 23)、多相 accessor (54)、`List` のメソッド (5〜50)、`Text` (54) | can inline | 全部 can inline |
| `node()` の bounds check | 1 | 1 |
| slot 読み | 1 | 1 |
| 多相表の読み `nameSlot[kind&511]` (E4) | 0 | 0。ただし不在側の `n.s.node(0)` に `len(nodes) > 0` の検査が 1 つ出る (`views.go:224`) |
| `List.Refs()` | ≤ 2 | 2 (`IsSliceInBounds` + `IsInBounds`)。ただし §5 のとおり、検査の件数より slice 式と `unsafe.Slice` が出す命令数が問題 |
| `Text()` | ≤ 2 | 1 (`IsSliceInBounds`、分岐 2)。`texts` 側は別に 3 |

`cannot inline` は `Builder.Node` (210)、`Builder.Identifier` (103)、`ForEachChildShape` (178)、`ForEachChild` (11324) で、どれも期待の範囲。`visitList` は cost 80 でちょうど予算内に収まっている。1 でも増えると inline されなくなり、Walk の数字が段差で変わる。

## 5. 単価表と命令列

hot path の命令数。prologue / epilogue、panic 側、noinline ラッパの引数 spill (`MOVD R0, 8(RSP)` など。inline されれば消える) は除いた。

| 操作 | Pointer 命令 (load / 条件分岐 / 間接分岐) | Store 命令 (load / 条件分岐 / 間接分岐) | 差を作る命令 |
| --- | --- | --- | --- |
| header (4 field) | 6 (3 / 0 / 0) | 6 (3 / 0 / 0) | なし |
| known: Call (子 1) | 11 (4 / 2 / 0) | 16 (6 / 3 / 0) | 下記 |
| known: Binary (子 3) | 18 (7 / 3 / 0) | 44 (10 / 8 / 0) | 下記 |
| known: PropertyAccess (子 2) | 16 (6 / 3 / 0) | 34 (8 / 6 / 0) | 下記 |
| polyPresent | 4 + callee (2 + `RET`) (3 / 0 / **1**) | 22 (7 / 3 / 0) | 表引き 5、slot 7、`node()` 8 |
| polyAll の不在側 | 5 + callee (2 / 1 / **1**) | 11 (4 / 2 / 0) | 表引き 5、`node(0)` の検査 2 |
| list: 準備 | 10 (type assert 5 を含む) | 約 30 | slice 式 14、`unsafe.Slice` の検査 7 |
| list: 要素あたり | 6 | 12 | `nodes` の `LDP` がループ内、bounds check 3、×24 が 2 |
| text (src 側) | `CALL (*Node).Text` + 4 | 12 (3 / 3 / 0) | Pointer は inline されない |
| modflags | 5 + callee (3 / 1 / **1**) | 1 (1 / 0 / 0) | — |
| parentChain: 段あたり | 3 (1 / 1 / 0) | 11 (2 / 2 / 0) | 下記 |
| parentChainRef: 段あたり | — | 9 (1 / 2 / 0) | 下記 |
| `Ref()` (E6) | — | 6 (1 / 0 / 0) | `SUB`、`MOVD`+`MOVK`、`UMULH`、`UBFX`。除算なし |

### 子 1 つの読み (known)

2 つ目以降の子は Pointer が `MOVD 72(R3), R1; MOVH (R1), R1` の 2 命令、Store は次の 11 命令である。

```
ADD $3, R1, R7        ; data + slot
MOVW R7, R7           ; uint32 の切り詰め
CMP R3, R7; BCS       ; extra の bounds check
MOVWU (R2)(R7<<2), R7 ; slot
MOVD R7, R8
CMP R6, R8; BCS       ; nodes の bounds check
UBFIZ $3, R7, $32, R7 ; ×8
ADD R7<<1, R7, R7     ; ×24
MOVH (R5)(R7), R7     ; kind
```

内訳は bounds check 4、×24 が 2、切り詰め 1、reg copy 1、load 2、添字 1。Pointer 側で対応するのは type assert の 5 命令 (view 1 つにつき 1 回) だけで、子を 2 つ以上読むと Store が必ず多い。checker.ts の構成 (Call 18,466、Binary 16,527、PropertyAccess 24,937) で重み付けすると Pointer 7.63、Store 15.86 inst/子。

### parentChain (E11)

```
Pointer:  MOVD 24(R1), R1;  ADD $1, R2;  CBNZ R1
Store:    UBFIZ; ADD; ADD (×24 + base);  ADD $1;  MOVH kind; UBFX; CBZW;  MOVWU parent;  MOVD; CMP; BCC
ref 版:   UBFIZ; ADD; ADD;  MOVWU parent;  ADD $1;  CBZW;  MOVW; CMP; BCC
```

`s.nodes` の base / len の `LDP` は両方ともループの外に出ている。Store 版の `UBFX $0, R5, $16` は `int16` の Kind を 0 と比べるための冗長な zero-extend である。ref 版は kind の load と `UBFX` が消えて 9 命令。どちらも bounds check (3) と ×24 (2) が残る。

### E5

Store も Pointer も同じ形のジャンプテーブルである: `SUB $167` → `CMP $182` → `BHI` → `ADRP` + `ADD` → `MOVD (R4)(R3<<3), R27` → `JMP (R27)`。比較の連鎖ではない。Store は 132 kind が kind 別関数の call、残りは switch に inline されている。Pointer も同様に混在する (call 83)。

## 6. Walk (E1)

### 6.1 KPC (checker.ts、20x × 5 run の中央値、括弧は幅)

| 条件 | inst/visit | cycles/visit | IPC | inst 比 | cycles 比 |
| --- | ---: | ---: | ---: | ---: | ---: |
| pointer | 72.38 (0.10%) | 26.24 (1.5%) | 2.76 | 1 | 1 |
| switch | 98.15 (0.08%) | 30.07 (1.1%) | 3.26 | **1.356** | 1.146 |
| shape | 123.83 (0.01%) | 33.38 (0.2%) | 3.71 | 1.711 | 1.272 |

- **ノイズ**: pointer の inst の幅 0.10% で基準 (1.5%) 内。
- **基準値との照合**: 検証指示の 70.3 inst/visit からは +3.0% ずれるが、これは指示の値が古い。Session を GC オフにした後の `BenchmarkASTWalkKPCV1` (`internal/ast/docs/_kpc-baselines/kpc-after.txt`) は 21.55M inst/op = 72.3 inst/visit、IPC 2.82 で、今回の 72.38 / 2.76 と一致する。ベンチの形は同じである。
- **判定: 不合格 (1.356 > 1.10)。** 予測 +8〜10% からも大きく外れた。
- inst は +35.6% だが cycles は +14.6% で、budget 文書の「cycles の増分は inst の半分以下」は当たっている。
- `shape` は `switch` に対して inst +26%、cycles +11%。取り下げてよい。分岐予測ミスの counter は kperf の Session に無く、取れていない。

### 6.2 +25.8 inst/visit の説明

slot の実数 (一時的なテストで数えた。`_storeexp-results/slotcount.txt`。checker.ts、298,054 visit):

| | 個数 | visit あたり |
| --- | ---: | ---: |
| slot を持つノード | 144,994 | 0.486 |
| child slot (うち nil) | 331,161 (109,976) | 1.111 (0.369) |
| list slot (うち nil / 空) | 105,860 (63,151 / 1,449) | 0.355 (0.212 / 0.005) |
| list の要素 | 76,868 | 0.258 |

| 項目 | 単価の差 (§5 の命令列) | × 回数/visit | inst/visit |
| --- | --- | ---: | ---: |
| list slot の準備 | slot 読み 5 + `Refs()` 25 対 Pointer 2〜3 (nil 検査 + `LDP`)。約 +27 | 0.355 | +9.6 |
| child slot の読み | bounds check 付きの slot 読み 4〜5 対 `MOVD` + `CBNZ`。約 +3 | 1.111 | +3.3 |
| 非 nil の子の `node()` | `LDP`、`MOVD`、`CMP`、`BCS`、`UBFIZ`、`ADD`、`ADD`。+7 | 0.742 | +5.2 |
| `Node` の 2 word 渡し | closure、`walk`、`ForEachChild` の各段で spill 2 + mov 1 | 1 | +8 |
| list の要素 | 約 21 対 約 13 | 0.258 | +2.1 |
| type assert の消滅 | −5 | 0.486 | −2.4 |
| walker の nil 検査 (Pointer だけ) | −1 | 1 | −1 |
| 合計 | | | **+24.8** (実測 +25.8) |

**最大の単一要因は nil list である。** 実装は `extra[0:3]` を 0 にして nil list をガードなしで読めるようにした (逸脱 1)。分岐は消えたが、list slot の 60% を占める nil list のたびに `Refs()` の 25 命令を払っている。Pointer は `CBZ` 1 つで抜ける。`at != 0` のガードを入れれば約 −5.9 inst/visit。これは「分岐を消すための番兵」が inst では損になる例で、設計文書の番兵の方針 (nil ノード、nil list) を list については見直す根拠になる。

直せる分の見積り: nil ガード −5.9、`unsafe.Slice` の検査 7 × 0.143 (非 nil list) = −1.0、切り詰め `MOVW` −1 前後。全部入れて約 +17〜18 inst/visit (1.24 倍)。`node()` と 2 word 渡しの +13 は `Node{s, h}` を通貨にする限り残る。

### 6.3 ns/visit (参考、5 run の中央値、括弧は幅)

| fixture | pointer | switch | shape | switch / pointer | shape / switch |
| --- | ---: | ---: | ---: | ---: | ---: |
| checker.ts | 7.971 (4.3%) | 9.277 (0.4%) | 10.710 (0.5%) | 1.164 | 1.154 |
| dom.generated.d.ts | 5.244 (0.5%) | 5.923 (0.6%) | 7.788 (0.1%) | 1.129 | 1.315 |

ns の比 (1.164) は KPC の cycles の比 (1.146) と合う。dom は KPC のベンチに無い (実装指示書どおり checker.ts のみ)。

## 7. 役割アクセス (E3 / E11)

### 7.1 ns/access (固定費を引く前、5 run の中央値)

| 操作 | ops | Pointer | Store | Store / Pointer | 幅 (P / S) |
| --- | ---: | ---: | ---: | ---: | --- |
| baseline | 298,054 | 0.357 | 0.447 | 1.25 | 4.3% / 0.5% |
| header | 298,054 | 1.966 | 0.856 | 0.44 | 4.1% / 9.5% |
| known (子 117,921) | 59,930 | 7.050 | 5.903 | 0.84 | 3.7% / 5.4% |
| polyPresent | 42,274 | 7.083 | 3.161 | 0.45 | 11.9% / 11.3% |
| polyAll | 298,054 | 5.508 | 2.326 | 0.42 | 2.1% / 12.9% |
| list (要素 30,422) | 18,466 | 7.235 | 9.112 | 1.26 | 8.9% / 5.2% |
| text | 123,554 | 2.902 | 1.514 | 0.52 | 6.1% / 1.9% |
| modflags | 298,054 | 5.866 | 0.624 | 0.11 | 1.0% / 1.0% |
| parentChain (1,455,043 段) | 123,554 | 14.220 | 29.940 | 2.11 | 0.4% / 0.7% |
| parentChainRef | 123,554 | — | 26.550 | 1.87 | — / 0.1% |
| refRoundTrip | 298,054 | — | 0.606 | — | — / 0.5% |

parentChain は段あたり Pointer 1.21 ns、Store 2.42 ns、ref 版 2.15 ns (baseline を引いた値)。Pointer の 1.21 ns は 3.2 GHz で約 3.9 cycle、つまり load 1 段の latency そのもので、Store の 7.7 cycle は load + ×24 (2) + base 加算 (1) の連鎖と合う。**命令数ではなく依存連鎖の長さが律速**なので、ref 版で命令を 2 つ減らしても 11% しか戻らない。

### 7.1b KPC: inst/access と cycles/access (baseline を引いた値)

baseline は pointer 7.04 inst・1.16 cycles、store 9.04 inst・1.47 cycles (要素あたり)。引く前の値は `_storeexp-results/kpc-summary.txt`。inst の幅は全条件 ≤ 0.14%。

| 操作 | ops | Pointer inst | Store inst | 差 | 判定 | Pointer cycles | Store cycles | §5 の静的値との整合 |
| --- | ---: | ---: | ---: | ---: | --- | ---: | ---: | --- |
| header (4 field) | 298,054 | 5.00 | 4.00 | −1.00 | 合格 | 4.85 | 1.06 | 6 / 6。ループ内で baseline の比較が消える分だけ少ない |
| known (子あたり) | 117,921 | 8.77 | 16.34 | +7.57 | **不合格** | 11.42 | 9.09 | 7.63 / 15.86。sum の `ADD` と branch で +0.5〜1 |
| known (op あたり) | 59,930 | 17.26 | 32.15 | +14.89 | | 22.47 | 17.89 | |
| polyPresent | 42,274 | 10.26 | 21.27 | +11.01 | **不合格** | 29.92 | 12.40 | Store 22。Pointer の callee は約 6 |
| polyAll | 298,054 | 12.87 | 10.71 | −2.16 | 合格 | 16.23 | 5.81 | |
| polyAll の不在側 (算出) | 255,780 | 13.30 | 8.96 | −4.34 | | 13.97 | 4.72 | Store 11。Pointer の既定 `Name()` は見積りより重い |
| list (list あたり) | 18,466 | 19.55 | 53.42 | +33.87 | **不合格** | 44.29 | 36.31 | 10 + 6×1.65 = 19.9 / 30 + 12×1.65 = 49.8 |
| list (要素あたり、参考) | 30,422 | 11.86 | 32.43 | | | 26.88 | 22.04 | 準備を含む平均 |
| text | 123,554 | 35.06 | 14.06 | −21.00 | 合格 | 8.45 | 3.98 | Store 12 |
| modflags | 298,054 | 14.89 | 0.00 | −14.89 | 合格 | 17.54 | 0.52 | Store 1。baseline の比較 1 命令と相殺して 0 に見える (引く前 21.93 / 9.04) |
| parentChain (段あたり) | 1,455,043 | 3.26 | 11.95 | +8.69 | **不合格** | 3.72 | 7.99 | 3 / 11 |
| parentChainRef (段あたり) | 1,455,043 | — | 9.27 | +6.01 | **不合格** | — | 7.08 | 9 |
| refRoundTrip | 298,054 | — | 4.00 | | | — | 0.51 | 6 (ラッパの `RET` などを含む数え方の差) |

不在側は `(polyAll × 298,054 − polyPresent × 42,274) / 255,780` で出した。cycles の幅は Pointer 側で大きい操作がある (list 14.3%、header 9.4%、modflags 6.8%)。Pointer の cycles は cache miss と間接分岐に支配されていて、run ごとに揺れる。

差を作っている命令は §5 のとおりで、実測がそれを裏付けた: known と polyPresent は bounds check 2 組 + ×24 + 切り詰め、list は `Refs()` の slice 式と `unsafe.Slice`、parentChain は ×24 と bounds check。

### 7.2 Check への影響の見積り (見積り。Δ は 7.1b の実測、回数は上限)

```
Δ inst/node ≈ 165 × (−1.00)/4                  =  −41
            + 49  × (+8.69)                    = +426   (ref 版 +6.01 なら +294)
            + 40  × (+7.57)                    = +303
            + 15  × (0.4×(+11.01) + 0.6×(−4.34)) =  +27
            + 1   × (−14.89)                   =  −15
            + 1   × (+33.87)                   =  +34
            + 4.6 × (−21.00)                   =  −97
            ≈ +637 inst/node ≈ Check 10,070 の +6.3%   (ref 版で +505、+5.0%)
```

前提: 回数はソース上の評価回数で load 数の上限。Pointer 側で消える type assert と `GetNodeId` は `known` に含まれる分しか数えていない。`text` の −97 は Store の `Text()` が Identifier 専用であることに由来し、本物では縮む (§10)。これを除くと +7.3%。支配項は parent (+426) と known (+303) の 2 つで、どちらも「×24 + bounds check 付きの `node()`」を 1 回ごとに払う構造から来る。

**E11 の判定**: 段あたり +8.69 は閾値 (+2) の 4 倍、Check の上限 +4.2%。ref 版は +6.01 で +2.9%。規約にする価値は inst では 3 割減と小さくないが、cycles では 7.99 → 7.08 (11%) で、依存連鎖が律速のため効きが悪い。

### 7.3 判定基準への異議

検証指示は「操作ごとに inst で Store ≤ Pointer」で合否を決める。この基準は次の理由で、known / poly / header について結論を誤らせうる。

- Pointer 側の費用の大半は inst に出ない。`header` は両者 6 命令で同数なのに ns は 2.3 倍違う (84.6 B/node の pointer chase 対 24 B の密な列)。`poly` と `modflags` は interface の間接 call (200 kind に散る) の予測ミスである。
- 逆に、この microbench は Store に有利でもある。全ノードを 1 回ずつ順に読むので footprint の差が最大に出る。Check は同じノードを 144 回読む (`Kind`) ので、cache に載った後の差は命令列の差に近づく。
- KPC はこれを裏付けた。§7.2 と同じ重みで cycles の差を足すと (参考値。Check の cycles/node が手元に無いので % にはしていない):
  ```
  Δ cycles/node ≈ 165×(1.06−4.85)/4 + 49×(7.99−3.72) + 40×(9.09−11.42) + 15×(0.4×(12.40−29.92) + 0.6×(4.72−13.97))
                  + (0.52−17.54) + (36.31−44.29) + 4.6×(3.98−8.45)
                ≈ −156 + 209 − 93 − 188 − 17 − 8 − 21 ≈ −274
  ```
  inst は +637、cycles は −274 で、**符号が逆**である。cycles 側の最大項は header (−156) と poly (−188) で、どちらも「全ノードを 1 回ずつ順に読む」形が Pointer の cache miss と間接分岐ミスを最大化した結果なので、Check でこの大きさが出るとは考えにくい。
- したがって、真の Check への影響は inst の +6.3% と cycles の負の値の間のどこかで、**この実験の形ではどちらに近いか決められない**。確実に言えるのは、parent 連鎖と Walk は両方の指標で負けること、known / poly / list の inst 増は bounds check と ×24 という予測しやすい整数命令で cycles には出にくいこと、の 2 つである。決めるには、実 checker の 1 関数 (たとえば `checkContextualIdentifier` 規模) を両方の API で書いて KPC で測ることが要る。kperf の Session に分岐予測ミスと L1D miss の counter を足せば、microbench の cycles の偏りも分離できる。

## 8. footprint (E10)

| fixture | node | header | extra | texts | B/node | Pointer | 比 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| checker.ts | 298,054 | 7,153,320 | 2,568,076 | 0 | 32.62 | 84.6 | 0.386 |
| dom.generated.d.ts | 109,605 | 2,630,544 | 1,064,048 | 0 | 33.71 | 82.4 | 0.409 |

設計文書の試算 33〜35 B/node に対して −0.4 / 範囲内。**下限である**: Identifier 以外の data word (literal の text、TokenFlags、operator)、symbol などの予約 slot を持たない。`extra` は 8.6 / 9.7 B/node なので、これらが node あたり 1 word 増えるごとに +4 B/node (+12%) になる。

## 9. 設計文書への反映案

直してはいない。提案だけである。

0. **Walk の予測 (+8〜10%) は外れた (+35.6%)。** budget 文書の余裕の見積りは `Header` の 6 命令だけを純増と見たが、実際は子ごとの `node()`、bounds check 付きの slot 読み、2 word 渡し、list の準備が全部純増だった。「typed view と type assert は相殺する」は正しかったが、それは全体の 1 割でしかない。**nil list の番兵を list については取り下げ、`at != 0` のガードに戻す** (−5.9 inst/visit)。
1. **budget 文書 §4 の表を実数に置き換える** (KPC 実測、括弧は静的値)。子の読み (解決済み `Node`) = 7.6 増 / 子 (11、初回 12〜13)、`node()` = 7〜8、slot 読み = 5、`List` = 53.4 / list (準備 約 30。予算 ≤ 18 を超過、要素あたり 12)、`Name` 在 = 21.3 (22。予算 20〜30 内)、不在 = 9.0、`Text` = 14.1 (12。予算 ≤ 20 内)、`Parent()` 1 段 = 12.0 (ref 版 9.3)、`Ref()` = 4.0、header 4 field = 4.0、`ModifierFlags` = 1。
2. **「子を返す accessor は解決済みの `Node` を返す」の再検討。** 子の kind を見るだけの読みで `node()` の 7 命令を毎回払う。budget 文書の元の形 (`NodeRef` を返し、`Header` は訪問 1 回につき 1) のほうが命令数は少ない。accessor-dynamic-frequency の「通貨の形 ≫ payload 配置」と合わせて、どちらを通貨にするかを inst と cycles の両方で決め直す価値がある。
3. **`List.Refs()` から `unsafe.Slice` を外す。** `extra []uint32` の部分 slice を返して要素ごとに `NodeRef(w)` に変換すれば、`UMULH` / `CBNZ` / `NEG` / `CMP` / `BCC` の 7 命令が消え、`unsafe` も要らない。block を `{start, end}` にする案 (budget 文書) も切り詰め 2 命令を消す。
4. **slot の添字を `uint32` で足さない。** `h.data+k` の `MOVW` (切り詰め) が子 1 つにつき 1 命令出ている。`int(h.data)+k` で消える。
5. **`IsNil()` の `UBFX`。** `int16` の Kind を 0 と比べるときの冗長な拡張。Kind を `uint16` にすれば消えるが、`ast.Kind` の型なので Store だけでは決められない。
6. **E11 の規約。** ref で回す書き方は inst −22% (11.95 → 9.27)、cycles −11%。規約にして損は無いが、問題は解決しない。parent 連鎖の費用は 24 B 行の ×24 と bounds check に由来し、書き方では消えない。親を多段で辿る checker の経路 (`findAncestor` 系) は、Store 化で 2 倍遅くなる前提で設計する必要がある。
7. **`ForEachChildShape` は取り下げてよい。** switch より inst +26%、cycles +11%、ns +15〜31%。
8. **JSDocParameterOrPropertyTag の `IsNameFirst`** を設計文書 §7 の未決事項に足す。

## 10. 限界

- KPC の counter は inst と cycles だけで、分岐予測ミスと cache miss は取れていない。§7.3 の「Pointer の cycles は miss に支配される」は IPC (polyPresent 0.56、list 0.58、known 1.03) からの推定である。
- §6.2 の内訳は命令列の単価 × slot の実数による説明で、合計が実測と 1 命令差で合うことしか確かめていない。項目ごとの実測ではない。
- 入力は実質 1 つ (Access は checker.ts のみ)。kind の構成が違えば known の重み付けは変わる。
- microbench は全ノードを 1 回ずつ順に読む。cache と分岐予測器の状態は Check と違う (§7.3)。
- Pointer から変換した Store は post-order で、parser が作る配置や `extra` の並びと違いうる。
- `text` の比較は Store に有利である。Store の `Text()` は Identifier 専用で、本物は kind の分岐を持つ。Pointer 側は全 kind 対応の `(*Node).Text()` を call している。
- Check の回数は上限で、Pointer 側で消える費用を引いていない。
- 計測中の負荷が高い (load average 3〜4)。
- `asm*` ラッパと `sum*` ループの本体の一致は目視である。

## 11. 再現手順と生データ

生データは `tsc/internal/ast/docs/_storeexp-results/`: `env.txt`、`load.txt`、`snapshot-before.sha`、`inline.txt`、`inline-bench.txt`、`bce.txt`、`bce-handwritten.txt`、`asm-ops.txt` (`-compact` は address を落とした版)、`asm-foreach-{store,pointer,shape}.txt`、`asm-foreach-store-block.txt`、`asm-walkers.txt`、`asm-sum-loops.txt`、`asm-parentchainref.txt`、`walk-ns.txt`、`access-ns.txt`、`footprint.txt`、`summarize.py`。

コマンドは検証指示 §1〜§6 のとおり。検証指示 §5 の例外規定に従い、`bench_test.go` に `opParentChainRefStore` / `sumParentChainRefStore` / `parentChainRef` を、`asm_test.go` に `asmParentChainRefStore` を足した。それ以外の実装は変更していない。

`kpc-20260922-0310.txt` (KPC の生データ)、`kpc-summary.txt` と `summarize-kpc.py` (中央値と baseline の差し引き)、`slotcount.txt` と `slotcount_test.go.txt` (§6.2 の slot の実数。一時的に package に置いて実行し、削除した)。

KPC (ユーザーが別ターミナルで実行):

```sh
cd /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/ast/storeexp && \
sudo /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/ast/docs/_storeexp-results/storeexp.kperf.test \
  -test.run '^$' -test.bench 'StoreExp(Walk|Access)KPC' -test.benchtime 20x -test.count 5 \
  | tee /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript/tsc/internal/ast/docs/_storeexp-results/kpc-$(date +%Y%m%d-%H%M).txt
```

binary は固定したハッシュの source から作ってある。

## 12. 追記: nil list ガードの導入 (2026-09-22、レポート後)

ユーザーの指示で §9 の 3 → 2 → 1 を実施した。1 は実装の変更を伴うのでここに記す。

- budget 文書 §4 を実測に置き換え、設計文書 §1 の基準値を 72.4 に直した。
- 設計文書 §2.5 に「解決済み `Node`」の再検討を書いた。`node()` の形 5 種の静的命令数と実 Store 上の parent 連鎖 ns (`_storeexp-results/resolve-variants-ns.txt`、`resolve_variants_test.go.txt`)。要点: 命令数は bounds check、cycles は ×24 が効く。32B + 無検査で parent 1 段 2.64 → 1.99 ns、Pointer 1.12。
- **設計文書 §2.3 の nil list 番兵を取り下げ、storeexp に `at != 0` ガードを入れた。** 最初は `visitList` の中に入れたが、inline cost が 80 → 86 になり inline されなくなった (list slot ごとに call)。generator を直して呼び出し側 (`if at := s.extra[data+k]; at != 0 && visitList(s, at, v)`) に置き、`visitList` は cost 80 のまま inline される。`ForEachChildShape` も同じ形。生成物は再生成で再現し、テストは両 tag で通る。source の sha256 は `snapshot-nilguard.sha`。

ns/visit (`walk-ns-nilguard.txt`。負荷 load average 3.6〜4.4 で幅が大きい。参考値):

| fixture | pointer | switch (前 → 後) | shape (前 → 後) |
| --- | ---: | ---: | ---: |
| checker.ts | 7.941 (11.6%) | 9.277 → 9.182 (−1.0%) | 10.710 → 10.400 (−2.9%) |
| dom.generated.d.ts | 5.248 (0.6%) | 5.923 → 5.549 (−6.3%) | 7.788 → 7.172 (−7.9%) |

KPC (`kpc-nilguard-20260922-1021.txt`、20x × 5 run の中央値、括弧は幅。source は `snapshot-nilguard.sha` と同一):

| 条件 | inst/visit (前 → 後) | cycles/visit (前 → 後) | IPC | inst 比 | cycles 比 |
| --- | ---: | ---: | ---: | ---: | ---: |
| pointer | 72.38 → 72.37 (0.11%) | 26.24 → 25.67 (3.8%) | 2.82 | 1 | 1 |
| switch | 98.15 → **92.53** (0.06%) | 30.07 → 29.20 (0.4%) | 3.17 | **1.279** | 1.137 |
| shape | 123.83 → 117.92 (0.03%) | 33.38 → 32.81 (1.0%) | 3.59 | 1.629 | 1.278 |

- inst は −5.62/visit で、見積り −5.9 (list slot の nil 率 60% × `Refs()` 25 命令 − ガードの `CBZW` 1) と 0.3 の差で合った。§6.2 の内訳の list の行が実測で裏付けられた。
- cycles は −0.87/visit (−2.9%)。無条件に切り出していた 25 命令は依存の無い整数命令で、IPC 3.26 → 3.17 と、消えたのは並列に実行されていた分だった。cycles 比は 1.146 → 1.137 で、ほとんど動かない。
- 判定は依然 **不合格** (1.279 > 1.10)。残りの内訳は §6.2 のとおりで、`node()` と 2 word 渡しの +13 が主である。

