# Check の線 1.10 に向けた A・B・C の実装 指示書

作成日: 2026-10-02。対象は実装を担当するエージェント (新しいセッション)。

[accessor tax 報告](store-checker-7d2c-accessor-tax-report-20261002.md) §3 の見積りでは、A + C + B で Check の cycles 比が 1.148 → 1.073 になる。本段では 3 つを production に入れ、**1 つずつ KPC を取って**、見積りと加算の仮定を確かめる。

| 記号 | 内容 | 根拠 | 見積り (Store の Check) |
| --- | --- | --- | --- |
| A | `symbolNodeLinks` を map でなく slice で引く | xctrace 報告 §4.4 (確定) | −69 M cycles / −226 M inst |
| B | 祖先 walk の 8 関数を inline 予算内に戻す | accessor tax 報告 §2 (overlay で実測) | −69 M cycles / −384 M inst |
| C | 測定用 adapter の鍵を struct にする | 7d-2c 実装報告、xctrace 報告 §6 | −57 M cycles / −193 M inst (計測の偏りを除くだけ) |

## 0. 規則

- **変更してよいもの**:
  - `internal/ast/store/` の手書きの file (`store.go`・`*_manual.go`)
  - `internal/storechecker/*_manual.go`
  - `internal/storeharness/` (adapter と test)
  - docs
- **変更しないもの**:
  - 生成物 (`// Code generated` の file) を手で直さない。生成物を変えたいときは P2 の `*_manual.go` で置き換え、`storetranslate` を走らせて生成物を作り直す。
  - Pointer の package (`ast`・`checker`・`binder`・`core`)、go.mod / go.sum。
- **commit**: A・B・C を別々の commit にする。順番は §1 → §2 → §3。各 commit の後に §4.1 の KPC を取る。
- **一時物**: before の archive・`-m=2` の出力・一時 binary は scratchpad に置く (repo 内に置くと Spotlight の索引で負荷が上がる)。
  - scratchpad は PC の再起動で消える。消えても作り直せるように、overlay や binary を作るスクリプトと sha256 は `_store-checker-7d2c-abc/` に残す。
- **生出力の置き場**: `internal/ast/docs/_store-checker-7d2c-abc/` (commit しない)。
- **sudo**: KPC は root が要る。コマンドを渡し、ユーザーが別のターミナルで `tee` した file を読む。binary を作り直した後の最初の計測では、許可ダイアログに注意するようユーザーに伝える。
- **記録の分け方**: 仮説と確定を分けて書く。見積りには「見積り」と書く。
- **開始時に記録すること**: HEAD と未コミットの差分の hash。

## 1. A: `symbolNodeLinks` を slice で引く

### 1.1 なぜ遅いか (確定)

- `core.PagedLinkStore` (`core/linkstore.go:49`) は、鍵を 256 個ずつの page に分けて引く。page 番号 (= 鍵 >> 8) が 65,536 未満なら slice の添字で、以上なら `map[uint64]` で page を引く。
- 鍵の違い:
  - Pointer の鍵は `GetNodeId` (0 からの連番) なので、常に slice の経路を通る。
  - Store の `nodeLinkStore` の鍵は `node.Key().Uint64()` (上位 32 bit が StoreID)。page 番号が必ず 65,536 を超え、**`Get` のたびに map を引く**。
  - 回数は 1 回の check で約 330 万回、費用は約 22 ms。
- Store 側で `PagedLinkStore` を node の鍵で使うのは `symbolNodeLinks` (`storechecker/checker.go:609`) だけ。
  - ほかの node links は `core.LinkStore` (map) で、Pointer も map なので差は出ない (xctrace 報告 §4.4)。

### 1.2 変更

- `storechecker/links.go` の `nodeLinkStore` (型と `Get`・`Has`・`TryGet`) を、P2 の `storechecker/links_manual.go` で置き換える。
  - Pointer の宣言の hash の行 (`// storetranslate:pointer-hash … links.go:nodeLinkStore` など) を付ける。
  - そのあと `cd tools && go run ./cmd/storetranslate` で `links.go` を作り直す。
- **形**: StoreID ごとの page 表 (2 段の表) にする。

  ```go
  type nodeLinkStore[V any] struct {
  	stores [][]*[256]V // [StoreID][NodeRef>>8]
  }
  ```

  - 鍵は `node.Key()` の `StoreID()` と `ID()`。
  - 表の伸ばし方と page の確保は `PagedLinkStore` と同じにする。`Get` は確保し、`Has` と `TryGet` は確保しない。
  - nil の Node は `NodeKey{}` (StoreID 0、ref 0) になる。今と同じく 1 つの鍵として扱う。
- **形を変えてよい範囲**:
  - `Get`・`Has`・`TryGet` の意味 (同じ鍵なら同じ `*V` を返す、`TryGet` は未確保なら nil) を変えない限り、形は自由。
  - 例: file の番号ごとの offset で連番にする案もある。その場合は、合成 Store (check の途中で増える) の扱いを報告に書く。
- **inline**: `-m=2` を `./internal/storechecker` にかけると 8 GB 機では終わらない。`Get` の inline の可否は、該当 file だけを小さな package に写して確かめるか、objdump で呼び出し元を見る。

### 1.3 受入

| 項目 | 条件 |
| --- | --- |
| 等価 | §4.2 が全部通る |
| KPC | §4.1 で、Store の Check の cycles が A の前より 40 M 以上下がる (見積りの 6 割) |
| retained | §4.3 が線内。Store の数だけ外側の表が伸びるので、取り直す |

## 2. B: 祖先 walk の 8 関数を inline 予算内に戻す

### 2.1 変更

accessor tax 報告 §2.1 の試作 (`_store-checker-7d2c-accessor/proto/gen.py`、overlay の内容の hash `c8c523e131f8`) を、そのまま production に入れる。

| 関数 | 置き場所 | 方法 |
| --- | --- | --- |
| `(*Store).node`・`Node.File` | `ast/store/store.go` (手書き) | 直接直す |
| `FindAncestorOrQuit`・`GetRootDeclaration`・`GetCombinedNodeFlags`・`IsGlobalSourceFile`・`HasQuestionToken` | `ast/store/utilities.go` (生成物) | P2 で `ast/store/utilities_manual.go` に置き、hash の行を付ける |
| 遅い経路の関数 (`getRootDeclarationOfBindingElement`・`getCombinedNodeFlags`・`isGlobalSourceFile`・`newGetSymbolId`) | 同上 | `// storetranslate:store-only` を付ける |
| `GetSymbolId` | `ast/store/utilities_manual.go` (既に P2) | 直接直す |
| `FindAncestor` | — | 変えない (`node` の変更だけで cost 80 に入る) |

- **`Store.node` の foreign 経路**:
  - 通常の build では、`s.foreignNodes[e^foreignBit]` (bounds check で panic) にする。
  - `storeChecks` の build では、今の明示の検査 (`panic("store: invalid node edge")`) を残す。`if storeChecks && e&foreignBit == 0 { panic(...) }` の形にする。
  - 通常の build で cost 29 のままになるかを `-m=2` で確かめる。cost が上がって `FindAncestor` が予算を外れるなら、検査を残さない形に戻し、そう報告する。
  - コメントを今の内容 (「foreign の側を call にしないのは inline 予算のため」) から、xor の理由と `storeChecks` の検査に書き換える。
- **`Store.list` も同じ形にするか**: 本段ではしない。`List.At` の cost (78 → 69) は試作で `node` を変えたことで下がっているので、`-m=2` の値を報告に書くだけにする。

### 2.2 受入

| 項目 | 条件 |
| --- | --- |
| inline | `-m=2` を `./internal/ast/store` だけにかけ、8 関数が `can inline` (cost を記録)。tag なしと `storechecks` の両方で取る |
| 呼び出し元での inline | checker の binary の objdump で、8 関数への `CALL` が 0 本 (試作では before の 325 本 → 0 本、`_store-checker-7d2c-accessor/calls-*.txt`)。`storechecker` の callback (`.func1` など) の `CALL` は、測定用 adapter の 1 本だけ |
| 等価 | §4.2 が全部通る |
| KPC | §4.1 で、Store の Check の cycles が B の前より 40 M 以上下がる。Walk ≤ 1.15、Bind ≤ 1.10 |
| `storetranslate` | 生成物の作り直しで差分が出ない (P2 の hash が合う) |

## 3. C: 測定用 adapter の鍵を struct にする

### 3.1 変更

- **今の作り**: `storeharness/fixture.go` の `nodeKey` は、`fmt.Sprintf("%s|%d|%d|%d", path, pos, end, kind)` で文字列の鍵を作る。`GetEmitSyntaxForUsageLocation`・`GetModeForUsageLocation`・`GetResolvedModuleFromModuleSpecifier` の呼び出しごとに、これが動く。
- **変えること**: `NewFixtureProgram` で、fixture の 3 つの map (`EmitSyntax`・`UsageMode`・`FromSpecifier`) を一度だけ struct の鍵の map に作り直す。
  - 鍵は、例えば `struct{ file *store.File; pos, end int32; kind ast.Kind }`。
  - 呼び出しでは、この struct の鍵で引く。
- **変えないこと**:
  - fixture の JSON の形 (文字列の鍵)。`Recorder` の書き方もこのまま。
  - 記録に無い問い合わせで panic すること。panic のメッセージには、今の文字列の鍵を入れる (失敗の経路でだけ作る)。
- **`Calls`**: `p.Calls[method]++` の map もあるが、回数は 1 run で約 17 万で小さい。測って 1 ms を超えるときだけ配列にする。

### 3.2 受入

| 項目 | 条件 |
| --- | --- |
| adapter の単体費用 | 1 ms/run 未満。測り方は自由。例: 1 回の check の adapter の呼び出しの並びを test の中で記録して再生する。7d-2c の `adaptercost/` は scratchpad ごと消えたので作り直す。test file に入れてよい |
| 同一性 | `TestFixtureIdentityCompilerProject` と、7d-2c §3.3 の同一性の試験が通る |
| KPC | §4.1 で、Store の Check の cycles が C の前より 30 M 以上下がる |

## 4. 計測と試験

### 4.1 Check KPC (各段の後と、before)

**binary の組み方**: 1 回の session で、before と各段の後の binary を交互に回す。

1. 4 つの source を用意する。
   - before = 開始時の HEAD
   - after-A
   - after-A+B
   - after-A+B+C
2. それぞれから `go test -tags kperf -c` で storeharness の binary を作る。
   - before・途中の段の source は `git archive` で scratchpad に出してから build する。
   - Walk / Bind 用の binary (`ast/store`・`storebinder`、比べる相手の Pointer は `ast`・`binder`) は、before と after-A+B (B で変わる) で作る。
3. 全 binary の sha256 を `_store-checker-7d2c-abc/kpc-binary.sha256` に記録する。

**回し方**:
- 元にするスクリプトは `_store-checker-7d2c-accessor/kpc-proto.sh`。
- 1 run の中で、各 binary の `BenchmarkCheckKPCPointer` → `BenchmarkCheckKPCStore` の順に回す。これを 5 run。
- Walk / Bind は 3 run × (`-benchtime 20x -count 5`) で、比は Store の binary の中の `/pointer` と `/store` で取る (7d-0 と同じ)。

**報告する値**:
- 各段の Store の cycles と inst、各段の差 (A・B・C 単独の効果)。
- Check の cycles 比 (同じ binary の Pointer で割る)。
- Pointer の A/A。全 binary の Pointer の幅が 2% 以内であること。
- 見積り (A −69・B −69・C −57 M cycles) と並べ、加算が成り立つかを書く。

**ゲート**: after-A+B+C の Check の cycles 比 ≤ **1.10**。

### 4.2 等価 (各 commit の前)

| 試験 | 範囲 |
| --- | --- |
| 段 1 strict | `TestCheckerCorpus`。compared の件数が before と違うときは、Pointer の非決定の除外 (`excluded (Pointer nondeterministic)`) の件数の違いかを確かめる |
| 段 2 | `TestCheckerConfigurations`。**A+B+C の後に 1 回だけ** (採用の段なので) |
| compiler プロジェクト | `TestCompilerProjectEquivalence` |
| 単体 | `TestModeCorpus`、`go test ./internal/ast/store/... ./internal/store...` (tag なしと `storechecks`) |
| 生成器 | `node tools/scripts/tsc/generate.ts` と `storetranslate` を走らせて、差分が出ない |

手順は 7d-2c の `regress.sh` (`_store-checker-7d2c-impl/`) を元にする。

### 4.3 retained / GC (A+B+C の後に 1 回)

7d-2c 実装指示書 §5 と同じ手順と線 (retained ≤ 0.85、mark CPU ≤ 0.75 × Pointer)。A で links の表の形が変わるので取り直す。

### 4.4 VS Code 全体は取らない

Store checker は tsc に結線されていない。storeharness が測れる仕事は compiler プロジェクトだけで、VS Code 規模の check はこの段では測れない。報告にもそう書く。

## 5. 線を外れたとき

- **after-A+B+C の比が 1.10 を超えたとき**: 次の段の候補を報告に書く。accessor tax 報告 §4 の候補と、片側でだけ inline される関数の洗い出し (storechecker と checker を `-m=1` で比べる)。
- **加算が成り立たないとき** (各段の和と、全部入れた差が 10 M cycles 以上違う): どの段の組み合わせで崩れたかを、各段の binary の差で書く。

## 6. 報告

- **置き場所**: `store-checker-7d2c-abc-implementation-report-20261002.md`
- **書くこと**:
  - 変更 (file と、P2 の hash の行)
  - §1〜§4 の値と受入の結果
  - 見積りとの照合
  - 仮説と確定の区別
  - 指示書の外で判断したこと

## 7. 決めること (着手前にユーザーへ確認)

1. **before**: 未コミットの docs 3 本 (xctrace 報告・accessor tax の指示書と報告) と `xctrace_test.go` を先に commit し、その hash を before にするか。推奨は commit すること。本段は commit を 3 つ積むので、before を commit にしておくと各段の source と binary の対応が追いやすい。
2. **`Store.node` の検査を `storeChecks` で残すか** (§2.1)。推奨は残すこと。通常の build の命令は変わらず、壊れた辺の手がかりを検査の build で保てる。
3. **A の形**: StoreID ごとの page 表 (推奨) か、file の番号ごとの offset で連番にするか。推奨の理由は、合成 Store が check の途中で増えても範囲の割り当てが要らないこと。
