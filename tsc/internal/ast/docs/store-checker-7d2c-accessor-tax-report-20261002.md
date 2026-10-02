# accessor の税と祖先 walk の inline の計測 報告

作成日: 2026-10-02。[指示書](store-checker-7d2c-accessor-tax-instructions-20261002.md) の実行結果。生出力・スクリプトは `_store-checker-7d2c-accessor/` (commit しない)。

## xctrace 報告の訂正

[xctrace 報告](store-checker-7d2c-xctrace-report-20261002.md) の検算で分かったこと。xctrace 報告そのものは書き換えていない。

1. **「A だけで比 1.101」は C を差し引いた基準の値**。測ったままのゲートでは、A 単独で 1.123、A + C で 1.1014 で、どちらも線の外。本段の KPC の基準 (§3) で計算し直すと A + C は 1.0996 で、線のちょうど上に乗る。
2. **受入の「8 割以上を説明」は自明に通る**。分類の合計が差の全部だから。
   - adapter を除いた checker 自身の差は約 320 M cycles。
   - そのうち名前の付いた仕組み (1・3・4・5) は約 198 M (62%) で、仕組み 3 は上限の値。
3. **A/A を外した pointer-2 の事情**。E-core の sample が 1.6% (ほかの run は 0〜0.2%) で、補完 14% が重なった run だった。
4. **両側の仕事量と flow の回数は一致する** (指示書の背景の表)。
   - 例外は `getFlowTypeOfReferenceEx` だけで、Store が 494 回少ない (250,336 / 249,842)。
   - 費用には効かない。理由は未確認として残す。

## 判定

| 受入 | 結果 |
| --- | --- |
| §1 回数と単価が両側でそろい、仕組み 6 と照合 | 回数・単価・照合はそろった。ただし **説明できたのは仕組み 6 の 16〜19% で、6 割に届かない**。届かない理由は §1.4 で、objdump で 2 関数を読んだ |
| §2 等価 | **合格**。段 1 strict 17,414 件すべて一致、compiler プロジェクト一致、`ast/store/...` は tag なし・`storechecks` の両方で PASS |
| §2 KPC 5 run × 2 binary、A/A (Pointer) の cycles の幅 ≤ 2% | **合格**。Pointer 10 run の幅は 1.35%、中央値どうしの差は +0.34% |
| Walk / Bind の線 | **線内**。Walk 1.088 (≤ 1.15)、Bind 1.001 (≤ 1.10) |

**主な結論**:
- **(a) 祖先 walk の inline を戻す試作の効果** (確定、KPC)
  - Store の Check は −69.2 M cycles / −384 M inst で、cycles 比は 1.148 → 1.118 (−0.027)。
  - 仕組み 3 の上限 147 M に対して、実際に出たのは 47%。
  - 消えた命令の多くは cycles に出なかった。命令の減りに対して cycles の減りは 0.18 の割合だった。
- **(b) accessor の税** (見積り)
  - 回数 × 単価で、全仕組みを合わせて +340〜+374 M inst。
  - 仕組み 6 の +552 M のうち、accessor で説明できるのは +87〜+107 M だけ。
  - 仕組み 6 の残りの主因は、accessor ではなく **2 word の Node の受け渡し** (退避・再読み込み・引数の移動) と inline の境界の移動と見ている (仮説。2 関数の objdump で確認、全体の数は取っていない)。
- **線 1.10 に届くか**: A + C + (a) で 1.073 (加算を仮定)。(a) だけでは 1.122 で届かない。

## 0. 変更・入力

- **HEAD**: `cd66aaa4d3` (before。§6 の 1 はユーザー決定で commit せず HEAD を before にした)。
- **未コミット**: `xctrace_test.go`・xctrace 報告・本指示書の 3 file。sha256 は `untracked.sha256`。コードに効くのは `xctrace_test.go` だけで、test file。
- **production の source**: 変更なし。試作は overlay だけ。
- **環境**: Go 1.27.1、M1 (8 GB)。
- **fixture**: PC の再起動で scratchpad が消えたので、KPC の前に `TestWriteCompilerProjectFixture` で作り直した。
  - files 124 / modules 91 / questions 914。sha256 は `3f44678b…` (`fixture-write.txt`)。
  - 計数 (§1) と等価 (§2.2) は、xctrace のときの fixture (再起動で消えた) で取った。
- **KPC binary**: 再起動後に作り直したものが、再起動前のものと 8 本とも byte 一致した (`kpc-binary.sha256`)。

## 1. (b) accessor の税

### 1.1 回数 (確定、計数専用の binary)

- **計数の仕組み**: overlay (`count/gen.py`) で、各 accessor の入口に `runtime.Callers` で呼び出し元の PC を記録する hook を入れた。
- **仕事**: compiler プロジェクトの `checkStore` / `checkPointer` を 1 回ずつ。
- **生出力**:
  - `count.tsv` (v2): 論理的な呼び出し元と、計数 binary での物理的な関数。
  - `count-v1.tsv`: `Store.node` の呼び出し元の 2 段 (accessor とその呼び出し元)。

| accessor | Store | Pointer |
| --- | ---: | ---: |
| `Kind` | 96,943,351 | field の読み (数えられない) |
| `IsNil` | 72,282,102 | nil 比較 (数えられない) |
| `Parent` | 24,650,495 (xctrace の計数と同値) | field の読み |
| `Store.node` (辺の解決全体) | 37,988,875 | — |
| `Store.list` / `List.At` | 1,406,616 / 1,095,544 | slice の読み |
| `File` ↔ `GetSourceFileOfNode` | 9,625,453 | 9,498,821 (親ループ 31,457,441 回、1 呼び出しあたり 3.31 段) |
| `Expression` / `Name` / `Initializer` / `Type` | 1,754,322 / 1,357,587 / 989,960 / 479,447 | 1,754,322 / 1,357,587 / 989,960 / 479,420 |
| `Text` | 4,380,148 | 4,380,148 |

- 多相 accessor と `Text` は、両側の回数が完全に一致した (`Type` だけ 27 回差)。
  - これが「仕事量が同じ」という前提の裏付けになる。
  - Pointer の `Kind`・`Parent`・nil 比較は、Store と同じ回数と**仮定**した (指示書 §1.1 のとおり)。
- `Store.node` 37.99 M の内訳:
  - `Parent` 24.65 M
  - 多相 accessor 4.58 M
  - `List.At` 1.10 M
  - 残り 7.66 M は、型付きの子 accessor (`BinaryExpression.Left` など) と、それ以外の解決。

### 1.2 単価 (objdump、inline された形)

before の binary を objdump で読んだ。対象は `isMatchingReference`・`IsThisInTypeQuery`・`getControlFlowContainer` と、accessor の単体。

| accessor | Store (呼び出し元の self に入る命令) | Pointer | 差 |
| --- | --- | --- | --- |
| `Kind` | `MOVH` 1 | `MOVH` 1 | 0 |
| `IsNil` | `CBZ`・`MOVH`・(`UBFX`)・`CBZW` で 3〜4 | `CBZ` 1 | +2〜+3 |
| `Parent` | `MOVWU`・`MOVD`・`LDP`・`CMP`・`BGE`・`ADD` (・`JMP`) で 6〜7 | `MOVD` 1 | +5〜+6 |
| 型付きの子 accessor | slot の読み 6 (`LDP`・`MOVWU`・`ADD`・`CMP`・`BLS`・`MOVWU`) + `node` 5〜6、退避の読み直しを入れて 11〜14 | type assertion 5 + load 1、または load 1 | +5〜+13 |
| 多相 accessor | inline 19〜21 (表 7 + slot 7 + node 6) | 呼び出し (呼び出し元の分は 2〜3。本体 22〜28 は仕組み 4) | 呼び出し元では +17。本体を入れると −5〜−10 |
| `List.At` | 12〜16 (見積り) | 3〜4 | +9〜+12 |
| `Text` (識別子) | 呼び出し。本体 29 | 呼び出し。本体 25〜30 | 呼び出し元では +1 (2 word の引数) |
| `File` | 呼び出し (本体 19〜22、仕組み 4) | `GetSourceFileOfNode` が inline されて親ループ (1 段 6 命令 × 3.31 + 5) | 呼び出し元では Store が −18〜−22 |

- `Parent` + `IsNil` で Store 9〜11・Pointer 2 になり、xctrace 報告の 9 / 2 と合う。
- 呼び出し元によって幅が出るのは、主に退避の読み直し (spill の reload) があるかどうか。

### 1.3 照合 (見積り)

#### 仕組みの割り当ての規則

- **計数 binary から物理的な関数を取らない**: hook で inline の判定が変わるため。
- **代わりの決め方**: 論理的な呼び出し元の名前と、before の `-m=2` の inline 判定から決めた (`tax.py`)。
  - 根拠の抜粋は `m2-*-excerpt.txt`。全文は 154 MB あり、Spotlight の負荷になるので scratchpad に置いた。
- **Store 側**:
  - 呼び出し元が祖先 walk の関数か callback なら仕組み 3。
  - 呼び出し元が inline されない store の method なら仕組み 4。
  - それ以外は仕組み 6。
- **Pointer 側**:
  - 祖先 walk の関数が inline されているので、そこでの読みは仕組み 6。
  - `GetThisContainer`・`GetAssignmentTarget`・`IsExpressionOfOptionalChainRoot`・`accessKind` は Pointer でも inline されないので、仕組み 3。

#### 仕組み別の照合

| 仕組み | 見積りの差 (低〜高) | xctrace の差 | 説明できた割合 |
| --- | ---: | ---: | ---: |
| 3 祖先 walk | +127〜+153 M | +477 M | 27〜32% |
| 4 node accessor | +112〜+126 M | −64 M | (符号が合わない。§1.4) |
| 6 checker・binder・他の ast の self | **+87〜+107 M** | **+552 M** | **16〜19%** |
| 合計 | +340〜+374 M | (差全体 +1,562 M) | 22〜24% |

#### accessor ごとの内訳 (M inst、低〜高)

| accessor | 回数 | 仕組み 3 | 仕組み 4 | 仕組み 6 | 合計 |
| --- | ---: | ---: | ---: | ---: | ---: |
| `IsNil` | 72.28 M | +39〜+52 | +15〜+25 | +90〜+140 | +145〜+217 |
| `Parent` | 24.65 M | +76〜+89 | 0 | +48〜+59 | +123〜+148 |
| 子 accessor | 7.66 M | +3〜+4 | +7〜+9 | +51〜+64 | +61〜+77 |
| `Store.list` + `List.At` | 2.50 M | 0 | +0〜+2 | +19〜+26 | +20〜+27 |
| 多相 accessor | 4.58 M | +1 | −100〜−127 | +76〜+81 | −23〜−46 |
| `Text` | 4.38 M | 0 | −4〜+18 | +4 | 0〜+22 |
| `File` | 9.63 M | 0 | +183〜+212 | −211〜−243 | −28〜−31 |
| `Kind` | 96.94 M | +7 | 0 | −7 | 0 |

- **大きいのは `IsNil` と `Parent`**。2 つで +268〜+365 M、税のほぼ全部。
- **多相 accessor と `File` は、Store のほうが安いか同じ**。
  - Store は表引きの inline、`File` は O(1)。
  - Pointer は呼び出し、`GetSourceFileOfNode` は親ループ。
  - ただし帰属が仕組み 4 と 6 の間で動くので、仕組みごとの差は大きく出る。

#### 上位 20 関数 (仕組み 6、xctrace の self の差と見積り)

全表は `mech6-join.md`、関数ごとの xctrace の値は `mech6-functions.tsv`。

| 関数 | xctrace の差 | 見積り |
| --- | ---: | ---: |
| `compareNodes` | −90.7 | −180〜−201 (`GetSourceFileOfNode` の inline 分) |
| `NameResolver.Resolve` | +67.8 | +17.5〜+22.1 |
| `checkTypeRelatedToEx` | −57.1 | +0.5〜+0.8 |
| `checkIdentifier` | −42.0 | +9.2〜+11.3 |
| `getTypeAtFlowNode` | +38.7 | +1.9〜+2.1 |
| `isMatchingReference` | +30.6 | +0.8〜+0.9 |
| `isPartOfImportEqualsModuleReference` | +30.2 (Pointer は 0) | +0.2〜+0.3 |
| `narrowType` | +27.5 | +20.8〜+21.1 |
| `IsThisInTypeQuery` | +23.8 | 0 |

- 仕組み 6 の関数ごとの差は、正の差の合計 +1,334 M と負の差の合計 −783 M が相殺して +552 M になっている。
- 見積りが合うのは `narrowType`・`NameResolver.Resolve` の一部くらい。

### 1.4 説明できなかった分 (objdump)

**`isMatchingReference`** (+30.6 M、198 万回、1 回あたり約 +15 命令)

- accessor の呼び出しは少ない。`Kind` 396 万回、`Expression` 4.8 万回。
- 最も多い経路は、識別子同士で `getResolvedSymbol` を 2 回呼ぶ経路。命令数は Store 約 59、Pointer 約 48 で、差は +11。
- 増えた命令:

| 増えた命令 | Store | Pointer | 差 |
| --- | ---: | ---: | ---: |
| 入口での引数の退避 | 4 store (source・target が 2 word ずつ) | 2 | +2 |
| 分岐前の退避 | 5 | 1 | +4 |
| `getResolvedSymbol` 2 回と `IsThisInTypeQuery` の前の再読み込み・引数の移動 | 1 回ごとに 1 多い | — | +3 |

- 合わせて約 +9〜+10 で、これは **2 word の Node の受け渡しの命令**。accessor の命令ではない。
- frame も 192 B と 144 B で違う。

**`IsThisInTypeQuery`** (+23.8 M)

- ほとんどの呼び出しは「`this` ではない」で早く返る。
- その経路の増分は、入口の退避 +2 と `Text` 呼び出しの引数 +1 だけで、親をたどる経路は少数。

**まとめ** (仮説)

- 仕組み 6 の残り (+450 M 前後) の主因は次の 2 つと見ている。
  - 2 word の Node の受け渡しの退避・再読み込み・移動。
  - inline の境界の移動 (`checkTypeRelatedToEx` −57 や `isPartOfImportEqualsModuleReference` +30 のように、どちらかの側だけで inline される関数)。
- 関数ごとに objdump したのは 2 本だけなので、全体の数は取っていない。
- 仕組み 4 の符号が合わない (見積り +112〜+126、xctrace −64) のも、境界の移動によると見ている。
  - 見積りでは Store の `File` の本体 (+183〜+212) を仕組み 4 に置いた。
  - xctrace では、Pointer の多相 accessor の本体 (Text 65.7・Expression 43.1 など) が仕組み 4 の Pointer 側に入っている。

## 2. (a) 祖先 walk の inline を戻す試作

### 2.1 試作 (overlay: `proto/gen.py`、`proto/overlay.json`)

差し替えたのは `ast/store` の `store.go`・`utilities.go`・`utilities_manual.go`。内容の hash は `c8c523e131f8` (`equiv.log`)。

| 関数 | 形 | cost (before → 試作) |
| --- | --- | ---: |
| `(*Store).node` | foreign 経路の `if e&foreignBit == 0 { panic }` をやめ、`s.foreignNodes[e^foreignBit]` にした。`foreignBit` が無い値は添字が 1<<31 以上になり、bounds check で panic する (条件は同じで、**メッセージが変わる**) | 38 → 29 (`Parent` 45 → 36) |
| `FindAncestor` | 変更なし (`node` の変更だけで入った) | 89 → 80 |
| `FindAncestorOrQuit` | `IsNil` と `Parent` を手で展開した | 95 → 79 |
| `GetRootDeclaration` | `BindingElement` のときだけ、非 inline のループ関数を呼ぶ | 106 → 72 |
| `GetCombinedNodeFlags` | `BindingElement`・`VariableDeclaration`・`VariableDeclarationList` 以外は自分の flags を返す。この 3 つは非 inline の元の本体を呼ぶ | 245 → 79 |
| `IsGlobalSourceFile` | `Kind() == SourceFile` のときだけ、非 inline の関数で `File()` を見る | 91 → 70 |
| `GetSymbolId` | id があればそれを返し、無ければ非 inline の関数で採番する | 83 → 79 |
| `HasQuestionToken` | `t.h != nil && t.h.kind == QuestionToken` にした (QuestionToken は自分の Store の node か `Node{}` を返し、番兵は kind が Unknown) | 86 → 74 |
| `Node.File` | `parentFile` と同じ親ループを n 自身から始める形にした (元の意味と同じ) | 111 → 70 |

- **意味の保存**: 戻り値・callback を呼ぶ順と回数・panic の条件は変えていない。変わるのは、壊れた辺での panic のメッセージだけ。
- **形を決めるまでに比べた候補** (-m=2 の cost):

| 関数 | 候補 | cost |
| --- | --- | ---: |
| `FindAncestor` | 辺の値で終了を判定する形 (Z1 §6 の形) | 88 |
| `FindAncestor` | `IsNil` を手で展開した形 | 76 |
| `FindAncestorOrQuit` | 元の形 | 86 |
| `FindAncestorOrQuit` | `break` の形 | 86 |
| `FindAncestorOrQuit` | kind 判定だけ展開した形 | 82 |
| `File` | 遅い経路を非 inline に出す形 | 85〜89 |

  - 辺の値で終了を判定する形は、Z1 §6 の想定とちがって cost が下がらなかった。
- **呼び出し元で inline されたか**: 試作の binary の objdump で確かめた (`calls-before.txt`・`calls-proto.txt`)。
  - before では、8 関数への `CALL` が `File` 181・`FindAncestor` 52・`GetRootDeclaration` 25・`GetSymbolId` 22・`HasQuestionToken` 18・`GetCombinedNodeFlags` 11・`IsGlobalSourceFile` 8・`FindAncestorOrQuit` 8 本あった。
  - 試作では 0 本。
  - checker の callback (`.func1`) の `CALL` も消えた。残ったのは `tracedTypeAdapter.Display.func1` (測定用 adapter) の 1 本だけ。
  - `getControlFlowContainer` には、`FindAncestor` のループと callback の本体が展開されている。

### 2.2 等価

`equiv.log`・`equiv-*.txt`。どれも同じ試作の overlay で走らせた。

| 項目 | 結果 |
| --- | --- |
| 段 1 strict `TestCheckerCorpus` | compared 17,414、equal 17,414、exit 0 |
| `TestCompilerProjectEquivalence` | PASS |
| `go test ./internal/ast/store/...` (tag なし / `storechecks`) | 両方 PASS |

- **compared の件数の差**: before (overlay なし) で同じ段 1 を回すと compared 17,413 だった (`corpus-before.txt`)。
  - 差の 1 件は試作によるものではない。
  - before の run で、Pointer の非決定 (`jsxNamespaceImplicitImportJSXNamespace.tsx`) が出て除外されただけ。

### 2.3 計測 (KPC)

`kpc-proto.sh`・`kpc-proto.txt`・`kpc-sum.py`・`kpc-summary.txt`。

- **実行**: ユーザーが別のターミナルで sudo で走らせた。開始時の load は 1.40。
- **順番**: Check は 1 run の中で before-Pointer → before-Store → 試作-Pointer → 試作-Store の順に回し、5 run。

**Check** (中央値、5 run)

| | before | 試作 | 差 |
| --- | ---: | ---: | ---: |
| Pointer cycles | 2,588.5 M | 2,597.3 M | +0.34% (A/A) |
| Pointer inst | 8,589.3 M | 8,586.8 M | −0.03% |
| Store cycles | 2,972.6 M | 2,903.4 M | **−69.2 M** |
| Store inst | 10,181.6 M | 9,797.5 M | **−384.1 M** |
| cycles 比 (同じ binary の Pointer) | 1.1484 | **1.1178** | −0.031 |
| cycles 比 (before の Pointer で割る) | 1.1484 | 1.1216 | −0.027 |
| inst 比 | 1.1854 | 1.1410 | |

- before の比 1.148 は、7d-2c の 1.150 と合う。
- Store の run ごとの値の幅は、before 0.74%・試作 0.71%。

**Walk / Bind** (7d-0 と同じく、Store の binary の中の pointer / store の比。中央値、3 run × 5)

| | before | 試作 | 線 |
| --- | ---: | ---: | --- |
| Walk cycles 比 (inst 比) | 1.0835 (1.297) | 1.0880 (1.297) | ≤ 1.15、内 |
| Bind cycles 比 (inst 比) | 1.0114 (1.142) | 1.0012 (1.138) | ≤ 1.10、内 |

- 別の binary で測った Pointer (`BenchmarkASTWalkKPCV1`・`BenchmarkASTBindKPCV1`) は、Walk 7.61 M・Bind 33.51 M cycles で、Store の binary の中の pointer と 1% 以内で合う。
- Walk は inst が同じなので、cycles の差 (+0.4%) はノイズの範囲。

### 2.4 (a) と (b) の重なり

試作で accessor の命令が変わったかを、objdump で比べた。

| 箇所 | 結果 |
| --- | --- |
| `Store.node` の通常経路 | 試作も before も `MOVW`・`LDP`・`CMP`・`BGE`・`ADD` で同じ |
| foreign 経路 | 7 命令ずつで同数 (`TBZ`・`AND`・`BLS` が `EOR`・`MOVW`・`BCS` に替わった) |
| `FindAncestorOrQuit` の展開 | 元の `IsNil`・`Parent` と同じ命令 |
| `HasQuestionToken` | `IsNil` の kind 判定 1〜2 命令が減る。呼び出しは少ない |

- **(b) の分**: ほぼ 0 (見積り)。
- **(a) の分**: KPC の差のほぼ全部 (−384 M inst / −69 M cycles)。中身は、呼び出し・prologue / epilogue・callback の間接呼び出し・`File` の本体を除いたこと。

## 3. 結論の表 (Check の cycles 比、測ったままの基準)

基準は本段の before の KPC (Pointer 2,588.5 M、Store 2,972.6 M、比 1.148)。

| 組み合わせ | 種類 | Store cycles | 比 | 線 1.10 |
| --- | --- | ---: | ---: | --- |
| なし | 実測 | 2,972.6 M | 1.148 | 外 |
| A (pageMap −69.4 M) | xctrace の確定値の見積り | 2,903.2 M | 1.122 | 外 |
| (a) 試作 (−69.2 M) | **実測** | 2,903.4 M | 1.122 | 外 |
| A + C (C は adapter −57 M) | 見積り | 2,846.2 M | 1.0996 | 線上 |
| A + (a) | 見積り + 実測、加算を仮定 | 2,834.0 M | 1.095 | 内 |
| A + C + (a) | 見積り + 実測、加算を仮定 | 2,777.0 M | **1.073** | 内 |
| (b) accessor の税 | 見積り | +340〜+374 M inst。cycles は**換算しない** (§1.3 の規則) | — | — |

- **(b) を cycles にした場合の参考** (仮定): 本段の (a) で inst の減りが cycles に出た割合 0.18 を当てはめると、61〜67 M cycles (比で 0.024〜0.026) になる。
  - ただし (b) は消せる量ではなく税。`IsNil`・`Parent` の命令を消す変更 (D など) でも、取れるのはその一部。
- **A と (a) の加算**: 触る場所が違う (pageMap と祖先 walk) ので加算できると見ているが、仮定で確かめていない。

## 4. 次の改善段の候補

| 候補 | 内容 | 根拠 | 受入条件 (案) |
| --- | --- | --- | --- |
| B (本段の試作を production へ) | §2.1 の 8 関数と `Store.node` の形 | 実測 −69 M cycles (比 −0.027〜−0.031)、Walk / Bind は線内 | `-m=2` で 8 関数が `can inline`。checker 側で 8 関数と callback への `CALL` が 0。Check KPC の比が 0.02 以上下がる。段 1・段 2 の等価が不変。Walk ≤ 1.15、Bind ≤ 1.10 |
| A | `symbolNodeLinks` を slice で引く | xctrace の確定値 −69 M cycles | xctrace 報告 §6 のまま |
| C | adapter の鍵を struct にする | −57 M cycles (計測の偏り) | xctrace 報告 §6 のまま |
| E (新、要測定) | 2 word の Node の受け渡しの費用を数える。例: 仕組み 6 の上位関数で、引数・frame の退避の命令を objdump で数える | §1.4 で、仕組み 6 の残りの主因と見ている (仮説) | まず計測の段とする。7c で 1 word は「境界ごとに +2 命令」で不採用にしたので、その見積りと照合する |
| D | `Parent()` / `IsNil` の命令を減らす | (b) の税の 7〜8 割が `IsNil` と `Parent` (+268〜+365 M inst、見積り) | Walk KPC で inst が下がり、cycles が上がらない。Check の inst の差が見積りの範囲で減る |

## 5. 決めること

1. **B を採用するか**。採用する場合、`Store.node` の foreign 経路の panic メッセージが `store: invalid node edge` から runtime の index out of range に変わる。
   - 条件は同じだが、壊れた辺を見つけたときの手がかりが減る。
   - 代わりに、`storeChecks` の build でだけ明示の検査を残す案がある。
2. **E を次の段にするか**。仕組み 6 の残り +450 M 前後は、本段の範囲 (accessor と祖先 walk) の外。

## 6. 指示書の外で判断したこと

1. **計数を 2 回取った** (v1・v2)。
   - v1 は呼び出し元を 2 段で取った。
   - v2 では、計数 binary の物理的な関数と、Pointer の親ループの反復数を足した。
   - 物理的な帰属は計数 binary では正しく取れない (hook で inline が変わる) ので、仕組みの割り当ては論理名と before の `-m=2` で決めた (§1.3)。
2. **仕組み 6 の関数ごとの差を、xctrace の pickle から出し直した** (`mech6.py`)。classify.md は inst と cycles の表が混ざっていて、関数名で引けないため。合計 +551.8 M で、xctrace 報告と一致する。
3. **`-m=2` の全文を scratchpad に移した** (ユーザー指示。Spotlight の負荷)。repo 側には抜粋だけを置いた。
4. **再起動で scratchpad が消えたあと、作り直して計測した** (§0)。
   - overlay は hash で同一、binary は byte 一致を確かめた。
   - fixture は作り直したので sha が変わった。
5. **Walk / Bind の比は、Store の binary の中の pointer / store で取った** (7d-0 と同じ)。別の binary の Pointer は照合だけに使った。
