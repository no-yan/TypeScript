# Check の線 1.10 に向けた A・B・C の実装 報告

作成日: 2026-10-03。指示書は [store-checker-7d2c-abc-implementation-instructions-20261002.md](store-checker-7d2c-abc-implementation-instructions-20261002.md)。生出力は `_store-checker-7d2c-abc/` (commit しない)。

## 0. 結論

| 項目 | 線 | 値 | 結果 |
| --- | --- | --- | --- |
| Check cycles 比 (A+B+C) | ≤ 1.10 | **1.053** (before 1.142) | 合格 |
| A の効果 | −40 M cycles 以上 | −112.2 M | 合格 |
| B の効果 | −40 M cycles 以上 | −84.9 M | 合格 |
| C の効果 | −30 M cycles 以上 | −31.1 M | 合格 (余裕 1.1 M) |
| Walk / Bind (B の後) | ≤ 1.15 / ≤ 1.10 | 1.087 / 1.005 | 合格 |
| retained (check の後) | ≤ 0.85 | 0.834 | 合格 |
| mark CPU (check の後) | ≤ 0.75 | 0.641 | 合格 |
| adapter の単体費用 | < 1 ms/run | node 鍵の 3 method で 0.66 ms。adapter 全体は未測定で、見積り約 7 ms (§2.3) | 3 method のみ合格。全体は見積りで未達 |
| 等価 (§4.2) | 全部 | 既知の `FuzzEquivalence/a708303f611b95c8` のみ失敗 | 合格 |

> **検証での注記 (2026-10-03)**: §4 の Check の比は、Pointer と Store が同じ仕事をしていない条件で取られていた。`build-kpc.sh` は各段の source を `git archive` で scratchpad に出して build するので、`repo.TestDataPath()` (compile 時の source の path から決まる) が長い path を指し、Pointer 側だけがその下の compiler プロジェクトを読む。Store 側は repo の path で記録した fixture を使う。同じセッションでこの段の C の binary と repo から build した binary を交互に取ると、Pointer の inst は +0.92%、Store は +0.06% 違った。このため本報告の比 (A+B+C で 1.053) は約 1% 低く出ている。repo から build した binary では 1.0635 / 1.0493 で、線 1.10 の判定は変わらない。各段の差 (Store 側の値) は影響を受けない。詳細は [検証報告](store-checker-7d2c-verification-report-20261002.md) §5.1 と §10 の残件 1。

## 1. 開始時の状態と commit

- 開始時の HEAD は `cd66aaa4d3`。未コミットだったのは、docs 3 本と `xctrace_test.go` (§7 の 1)。
- ユーザーの決定により、これらを先に commit した。

| 段 | commit |
| --- | --- |
| before | `de23e1f53d` (docs 3 本と `xctrace_test.go`) |
| A | `46a98e1abd` |
| A+B | `f71b24045d` |
| A+B+C | `ec2196c835` |

本指示書と本報告は未コミット。

## 2. 変更

### 2.1 A: `symbolNodeLinks` を StoreID ごとの page 表で引く

- **置き場所**: `storechecker/links_manual.go` (新規、P2)。`links.go` は `storetranslate` で作り直し、`nodeLinkStore` の 4 宣言が消えただけ。
- **hash の行**:
  - `23820a26eafb977a links.go:nodeLinkStore`
  - `d5587bfdc4247b0b links.go:nodeLinkStore.Get` (`newPage` も同じ行で、`store-only`)
  - `0a6c3cc96a69d318 links.go:nodeLinkStore.Has`
  - `84bb3b500a6da07a links.go:nodeLinkStore.TryGet`
- **形**:
  - `stores [][]*[256]V` で、添字は `[StoreID][NodeRef>>8]`。
  - StoreID は `IDSpace` が 0 から密に振るので、合成 Store が check の途中で増えても、外側の表が伸びるだけで済む。
  - 伸ばし方は `PagedLinkStore` と同じ (`slices.Grow`)。`Get` は確保し、`Has` と `TryGet` は確保しない。
  - nil の Node は `NodeKey{}` (StoreID 0・ref 0) で、1 つの鍵として扱う。
- **inline (§1.2)**:
  - 該当 file を小さな package に写して `-m=2` をかけた。`TryGet` は cost 113 で予算 80 を外れる。
  - そのため `Get` は、`TryGet` を呼ばずに引く処理を本体に持つ。速い経路で call が 1 段減る。
  - `Get` 自体も inline されない (objdump で呼び出し元に CALL が 30 本)。before の `Get` も `PagedLinkStore.Get` への call だったので、call の数は増えていない。

### 2.2 B: 祖先 walk の 8 関数を inline 予算内に戻す

試作 (`_store-checker-7d2c-accessor/proto/gen.py`) と同じ内容を入れた。試作との違いは次の 2 点だけ。
- コメント。
- `node` の `storeChecks` の検査。

**`store.go` (手書き)**
- `node`: foreign 側は `s.foreignNodes[e^foreignBit]` にした。`storeChecks` の build だけ `if storeChecks && e&foreignBit == 0 { panic("store: invalid node edge") }` を残す (§7 の 2、ユーザー決定)。コメントは、xor の理由と検査に書き換えた。
- `File`: `parentFile` と同じ親ループを n 自身から始める形にした。`parentFile` は削除。

**`utilities_manual.go` (P2)**

| 関数 | hash の行 | 遅い経路 (`store-only`、`//go:noinline`) |
| --- | --- | --- |
| `FindAncestorOrQuit` | `2978672a03f3135f` | — |
| `GetRootDeclaration` | `dca65bd012276201` | `getRootDeclarationOfBindingElement` |
| `GetCombinedNodeFlags` | `83da3d18b6ace2cc` | `getCombinedNodeFlags` |
| `IsGlobalSourceFile` | `f831f76b089fef80` | `isGlobalSourceFile` |
| `HasQuestionToken` | `8df070f5dc4fd89b` | — |
| `GetSymbolId` (既に P2、直接直した) | `2d51f4a544629e45` | `newGetSymbolId` |

`utilities.go` は `storetranslate` で作り直した。5 関数が消えただけ。

**`-m=2` (`./internal/ast/store` だけ、`_store-checker-7d2c-abc/B/m2-*.txt`)**

| 関数 | tag なし | `storechecks` |
| --- | ---: | ---: |
| `(*Store).node` | 29 | 38 |
| `Node.File` | 70 | 79 |
| `FindAncestor` | 80 | **89 (inline 不可)** |
| `FindAncestorOrQuit` | 79 | **88 (inline 不可)** |
| `GetRootDeclaration` | 72 | 72 |
| `GetCombinedNodeFlags` | 79 | 79 |
| `IsGlobalSourceFile` | 70 | 70 |
| `HasQuestionToken` | 74 | 74 |
| `GetSymbolId` | 79 | 79 |
| `List.At` (参考) | 69 | 78 |

- 通常の build では、`node` が cost 29 のままで、全部 inline できる。検査を残す形を採った (§2.1 の条件を満たす)。
- `storechecks` の build では、検査の分 (+9) で `FindAncestor` と `FindAncestorOrQuit` が予算を外れる。これは検査の build の速さだけの問題なので、そのまま残した。

**呼び出し元での inline** (storeharness の kperf binary の objdump、`B/calls-B.txt`)
- 8 関数への `CALL` は 0 本。before は 325 本。
- storechecker の callback への `CALL` は、`(*tracedTypeAdapter).Display.func1` の 1 本だけ。

### 2.3 C: 測定用 adapter の鍵を struct にする

**`storeharness/fixture.go`**
- `NewFixtureProgram` で、`EmitSyntax`・`UsageMode`・`FromSpecifier` を一度だけ作り直す。
  - 鍵は `nodeKeyOf{file *store.File; pos, end int32; kind ast.Kind}`。
  - 引くときの file は、`ast.HasFileName` が `*store.File` ならそれを使い、違えば `byPath` で引く。
- 次のものは変えていない。
  - fixture の JSON の形。
  - `Recorder`。
  - 記録に無い問い合わせで panic すること。panic のメッセージは今の文字列の鍵で、失敗の経路でだけ作る。
- `Calls` は `[numCalls]int` にし、30 個の method を定数で名付けた。報告用に `CallCounts()` (名前の map) を置いた。`measure_test.go` はそれを使う。

**adapter の単体費用** (`adaptercost_test.go` の `BenchmarkAdapterCost`、`adapter-cost.txt`)
- 測り方: 1 回の check の node 鍵の呼び出し (45,526 回) を記録して再生する。adapter の呼び出しは全体で 168,302 回。

| 計測 | 変更前 | 変更後 |
| --- | ---: | ---: |
| node 鍵の 3 method (変更後は `Calls` 込み、変更前の値は `Calls` を含まない) | 8.69 ms (文字列鍵) | 1.50 ms (path を鍵) → **0.66 ms** (`*store.File` を鍵) |
| 鍵で引くだけ | — | 1.10 ms (path) → 0.57 ms (`*store.File`) |
| `Calls` の計数 (全呼び出し) | 1.41 ms (map) | 0.36 ms (配列、ループ自体の費用も含む) |

- path の文字列を鍵にした最初の形は 1.50 ms で、線を超えた。`Calls` の計数も 1.41 ms で、§3.1 の「1 ms を超えるときだけ配列」に当たった。
- そこで、鍵を `*store.File` に、`Calls` を配列にした。
- **測っていない範囲**: 上の値は node 鍵の 3 method だけで、adapter 全体ではない。文字列鍵のまま残した method の費用は、上の単価からの見積りで約 7 ms/run (約 22 M cycles)。
  - `GetResolvedModule` (`fmt.Sprintf`): 22,810 回 × 約 190 ns (文字列鍵の 8.62 ms ÷ 45,526 回) で約 4.3 ms
  - `GetEmitModuleFormatOfFile` (path の鍵): 77,070 回 × 約 24 ns (path 鍵の 1.10 ms ÷ 45,526 回) で約 1.9 ms
  - `GetSourceFileForResolvedModule`: 22,810 回 × 約 24 ns で約 0.5 ms
  - したがって、§3.2 の線 (adapter の単体費用 < 1 ms/run) を満たしたのは 3 method だけで、全体は見積りで未達。直さない理由は §8。

**同一性**: `TestFixtureIdentityCompilerProject` と `TestFixtureIdentityConfigurations` は、regress の packages の step に入っている。両 tag で合格。

## 3. 等価 (§4.2)

- **手順**: `_store-checker-7d2c-abc/regress.sh` (7d-2c の `regress.sh` が元) で、各段の commit の前に回した。出力は `A/`・`B/`・`C/` に置いてある。
- **元の手順との違い**: corpus・mode・project を独立の step で回すので、packages の step ではこれらを skip した (二重実行を避けるため)。

| 試験 | A | A+B | A+B+C |
| --- | --- | --- | --- |
| 生成器 (`generate.ts`・`storetranslate`) | 差分なし | 差分なし | 差分なし |
| 段 1 strict | compared 17,414 / equal 17,414 | 17,413 / 17,413 | 合格 |
| `TestCompilerProjectEquivalence` | 合格 | 合格 | 合格 |
| `TestModeCorpus` | 合格 | 合格 | 合格 |
| 段 2 `TestCheckerConfigurations` | — | — | 合格 |
| packages (tag なし / `storechecks`) | a708 のみ失敗 | a708 のみ失敗 | a708 のみ失敗 |

段 1 の compared の差 (17,414 と 17,413) は、Pointer の非決定の除外の件数が違うためで、0 件と 1 件 (`jsxNamespaceImplicitImportJSXNamespace.tsx#/node_modules/preact/jsx-runtime/index.d.ts`)。

## 4. Check KPC (§4.1)

**binary**
- `build-kpc.sh` で、各 commit を `git archive` で scratchpad に出して build した。sha256 は `kpc-binary.sha256`。
- fixture は 7d-2c の accessor tax のものを再利用した。sha256 は `3f44678b…`、`start.txt` に記録。fixture は Pointer の Program の応答なので、A・B・C の影響を受けない。

**回し方**: `kpc.sh` で、Check を 5 run × 4 binary × (Pointer → Store) 回した。生出力は `kpc.txt`、集計は `kpc-sum.py` → `kpc-summary.txt`。

| 段 | Pointer cycles | Store cycles | Store inst | cycles 比 | inst 比 | 段の差 (cycles / inst) | 見積り (cycles / inst) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| before | 2622.1 M | 2994.2 M | 10190.4 M | 1.1419 | 1.1744 | | |
| A | 2616.7 | 2882.0 | 9789.1 | 1.1014 | 1.1284 | −112.2 / −401.2 | −69 / −226 |
| A+B | 2626.7 | 2797.2 | 9388.3 | 1.0649 | 1.0824 | −84.9 / −400.8 | −69 / −384 |
| A+B+C | 2626.4 | 2766.1 | 9240.4 | **1.0532** | 1.0655 | −31.1 / −147.8 | −57 / −193 |

- **Pointer の A/A**: 4 binary の中央値の幅は 0.38%、20 run 全体の幅は 1.63% (線 2%)。
- **Walk / Bind** (3 run × `-count 5`、同じ binary の `/store` ÷ `/pointer`)

| | before | A+B |
| --- | --- | --- |
| Walk | 1.0915 (inst 1.296) | 1.0869 (inst 1.296) |
| Bind | 1.0090 | 1.0045 |

  B は Walk と Bind の命令をほぼ変えていない。
  - accessor tax 報告の Walk の inst 比 1.15 とは比べられない。あちらの `kpc-sum.py` は、`/pointer` と `/store` の副 benchmark を 1 つの鍵にまとめていた (範囲 21.5..28.0 M が 2 峰)。

### 4.1 見積りとの照合

- **全体**: 段の差の和は −228.1 M cycles。見積りの和は −195 M で、実測が 33 M 大きい。
  - 各段は順に積んだ binary の差なので、「各段の和 = 全部入れた差」は作りから成り立つ。
  - ここで比べているのは、各段の単独の見積りと、積んだ順での実測。
- **A**: 見積りより大きい。cycles −112 対 −69、inst −401 対 −226。
  - 仮説: 見積り (xctrace 報告 §4.4) は `pageMap` の map を引く費用だけを数えていた。旧 `Get` の page 番号の比較・`PagedLinkStore.Get` への call・map の hash 計算に加え、`Uint64()` の組み立ても消えた分が上乗せされている。未検証 (関数別の帰属は取っていない)。
- **B**: cycles −85 対 −69、inst −401 対 −384。
  - inst は試作の overlay での実測 (−384) とほぼ一致する。cycles は A の後に積んだ値で、試作 (A なし) より 16 M 大きい。
  - 仮説: A で `nodeLinkStore` の map の待ちが消え、walk の命令が cycles の上で前に出た。未検証。
- **C**: cycles −31 対 −57。
  - adapter の node 鍵の 3 method は、単体で 8.69 → 0.66 ms (約 −26 M cycles 相当、3.2 GHz 換算の見積り)。実測の −31 M と同じ桁。
  - 見積りの −57 M (xctrace 報告 §6) は、adapter 全体の帰属から出した値。C は node 鍵の 3 method と `Calls` だけを直した。
  - 仮説: 差は、文字列鍵のまま残した method の分。とくに `GetResolvedModule` は、呼び出しごとに `fmt.Sprintf` で鍵を作る (1 run で 22,810 回)。
- **確定していること**:
  - 3 段の効果と、線の合否。
  - B で 8 関数の CALL が 0 本になったこと (objdump)。
  - C の adapter の node 鍵の費用 (単体)。

## 5. retained / GC (§4.3、A+B+C の後)

- 手順は 7d-2c 実装指示書 §5 と同じ (`TestRetained`、子プロセスを Pointer と Store で交互に、各 3 run)。生出力は `retained-check.txt` と `retained-check/`。
- 3 run の差は 0.2 MB 以内。下表は 3 run の代表値。

| 段 | Pointer HeapAlloc | Store HeapAlloc | 増分の比 | scan heap P / S | mark CPU P / S (ms) | mark CPU の比 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| baseline | 11.43 MB | 11.43 MB | | 1.4 / 1.4 MB | 0.5 / 0.45 | |
| parse + bind の後 | 133.75 | 84.50 | 0.599 | 113.4 / 21.4 | 70 / 13.5 | 0.19 |
| check の後 | 237.5 | 199.9 | **0.834** (絶対量 0.842) | 211.0 / 126.2 | 167 / 107 | **0.641** |

- 7d-2c の値 (0.834・0.652) とほぼ同じ。
- A で links の表の形が変わったが、retained には差として見えない。page の中身は同じで、外側の表が増えるだけ (Store の数 × 24 B + page 表)。

## 6. VS Code 全体 (§4.4)

取っていない。Store checker は tsc に結線されておらず、storeharness が測れる仕事は compiler プロジェクトだけ。

## 7. 指示書の外で判断したこと

1. **A の `Get`**: `TryGet` を呼ばずに、引く処理を自前で持たせた (cost 113 で inline 不可のため、call を 1 段減らす)。`Has` は `TryGet` を呼ぶ。
2. **B の `storechecks` の build**: `FindAncestor`・`FindAncestorOrQuit` が予算を外れる (89・88)。指示書の条件は通常の build なので、検査を残す形を採った。
3. **C の鍵**: path の文字列ではなく `*store.File` にした。path の形は 1.50 ms で線を超えたため。
   - file が `*store.File` でないときは `byPath` で引く。fixture の鍵の path が program に無ければ、`NewFixtureProgram` で panic する。
4. **C の `Calls`**: 配列にした (map では 1.41 ms で §3.1 の条件に当たった)。名前の map は `CallCounts()` で作る。
5. **regress の packages の step**: 独立の step で回す corpus・mode・project・configurations を skip した。
6. **Walk / Bind**: Pointer だけの binary (`ast`・`binder`) は作らず、Store 側の binary の中の `/pointer` と `/store` で比を取った (§4.1 の指定どおり)。
7. **fixture**: 作り直さず、7d-2c accessor tax の fixture を sha で照合して再利用した。
8. **`BenchmarkAdapterCost` の初版の誤り**: 副 benchmark の再生が `program.Calls` を増やし続けた後で回数を読み、`calls` の値が膨らんだ。check の直後に写しを取る形に直して測り直した (上の値は直した後のもの)。

## 8. 次の候補 (線は通ったので任意)

- **adapter の残りの文字列鍵**: `GetResolvedModule` (`fmt.Sprintf`、22,810 回/run)、`GetEmitModuleFormatOfFile` (77,070 回、path の鍵)、`GetSourceFileForResolvedModule` (22,810 回)。直さない。
  - Pointer 側は本物の `compiler.Program` を通るので、比には fixture の引き費用と本物の Program の計算費用の差が混ざる。差の向きは未測定で分からない。
  - CLI に結線すれば両側とも本物の Program を通るので、この差はそこで消える。adapter に手を入れても結線で捨てることになる。
  - Store 側の段の差 (各変更の効果) は adapter の費用が共通なので影響を受けない。影響を受けるのは Pointer との比の絶対値だけ。
- **accessor tax 報告 §4 の候補**と、片側でだけ inline される関数の洗い出し (`-m=1` の比較)。
