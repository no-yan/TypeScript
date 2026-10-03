# Store checker の性能 (TODO 7d-2c) 検証報告

作成日: 2026-10-03。[検証指示書](store-checker-7d2c-verification-instructions-20261002.md) (2026-10-03 改訂、67f21f0049) の §10 に従う。生出力は `_store-checker-7d2c-results/` (commit しない)。

- before: 9c93098cc3。after: 67f21f0049 (開始時の HEAD、未コミットの差分なし)。
- `git diff ec2196c835 HEAD -- tsc tools ':!tsc/internal/ast/docs'` は空。production の source は ec2196c835 と同じ。
- 実装報告にあることと、この検証で再現したことを分けて書く。「再現」と書いた値は、すべて自分で取り直した値。

## 判定

### 2c の測定の正しさ: そろった (条件付き)

| 観点 | 結果 |
| --- | --- |
| 契約どおりの条件 | 満たす。KPC は GC オフの Session・同じ binary・区間の中身が実装指示書 §4 / §9 どおり (§5.1)。retained は別の子プロセス・共通の baseline・GC 2 回 (§6)。印字は全区間 (§7) |
| 隔離 | 成り立つ。Store の子プロセスの profile 18 本で Pointer の frame は 0 件。型を再帰的にたどると、Pointer の型への経路は file が nil の `*ast.Diagnostic` の 1 本だけで、実行時にも全件 nil (§3) |
| 比較器と監査の感度 | ある。比較器 2 つ (記録・印字) と故障 4 種を、すべて必須の方法で検出した (§2・§3.2) |
| 再測定と実装の値の一致 | Store の値は一致した (Check の cycles 2,767 対 2,766 M)。Check の比は 1.0635 (今回) 対 1.053 (3 つの変更の報告) で、差の大半は 3 つの変更の計測の偏りだと確かめた (§5.1 の残件 1) |

条件:

- Pointer の A/A は、cycles の幅が 2 回の KPC で 3.41% と 2.14% で、線の 2% を超えた (§5.1)。
  - inst の幅は 0.2% で安定している。
  - run ごとの比は全 run で 1.10 を下回る。ゲートの合否は変わらない。
- 同一性の試験は、合成 import の specifier の答えそのものには鈍い。故障 3 は検出したが、合成 import を問う 118 構成のうち 56 構成では、nil を返しても記録が変わらない (§3.2、残件 3)。

### 3 つの変更の正しさ: 通った

| 監査 | 結果 |
| --- | --- |
| 範囲 | 3 commit とも、変更は許された file だけ。生成物の変化は `storetranslate` の作り直しの分だけ (§1・§4.1) |
| 意味 | 置き換えた宣言 (`nodeLinkStore` の型・`Get`・`Has`・`TryGet` と、utilities の 6 関数) と `node`・`File` は、元の版と同じ意味 (§4.1)。`nodeLinkStore` は旧 `PagedLinkStore` との差分試験 (20 万操作) で一致 |
| StoreID | 採番は 1 つの atomic な counter で、重ならない (コードの読み)。実測でも重なりは 0。ただし実測で links に来た Store は source と region だけ (§4.2) |
| inline | `-m=2` の cost は報告と全部一致。objdump の `CALL` は 325 → 0 本 (§4.3) |

### 各ゲート

| ゲート | 線 | 値 (再現) | 判定 |
| --- | --- | --- | --- |
| Check cycles | ≤ 1.10 | 1.0635 (KPC 1、5 run の median)、1.0493 (KPC 2、5 run) | 合格 |
| Walk | ≤ 1.15 | 1.0589 | 合格 |
| Parse checker.ts / dom | ≤ 1.00 | 0.8235 / 0.8274 | 合格 |
| Bind | ≤ 1.10 | 1.0047 | 合格 |
| retained (check の後) | ≤ 0.85 | 0.833 (絶対量 0.841) | 合格 (余裕 0.017) |
| mark CPU (check の後) | ≤ 0.75 | 0.650 | 合格 |

### 線の無いもの

- **印字**: 退行がある。大きさは 3 つの変更の前とほぼ同じ (§7)。
  - configurations の build は cycles 1.417 倍・B/op 35 倍。診断の生成は B/op 22.7 倍。
  - compiler の診断の生成は、cycles 比が 1.124 から 1.036 に縮んだ。
- **偽 Program 全体の費用**: 1 回の check で 17.4 M cycles・96.2 M inst (約 5.4 ms)。Store の Check の 0.63% (cycles)。見積り約 7 ms の 8 割 (§5.3)。
- **check の間の増分**: HeapAlloc は Pointer +103.8 MB・Store +115.4 MB。scan heap は +97.6 MB・+104.8 MB (§6)。

### 決定 (2026-10-03、ユーザー)

- **7d-2c は完了**とする。
- 残件の扱いは推奨どおり。
  - 残件 1: 3 つの変更の報告に注記を入れた。
  - 残件 2 (A/A)・残件 4 (fixture の byte): 次に KPC や fixture を使う段で直す。
  - 残件 3 (同一性の試験と合成 specifier): 7e の前に入れる。
  - 残件 5 (links の page の密度): 7e の end-to-end の retained を見てから判断する。
- 本報告を commit する。

## 0. 開始時の状態と記録

| 項目 | 値 | file |
| --- | --- | --- |
| HEAD | 67f21f00492a | `head.txt` |
| 未コミットの差分 | なし (sha256 は空の hash) | `status-start.txt`・`uncommitted-diff.sha256` |
| before との差分 | 27 file (docs 11、production 5、storeharness 11) | `diff-stat-before.txt` |
| ec2196c835 との差分 (docs を除く) | 空 | `diff-ec2196-head.txt` |
| source hash (開始時) | 18,126 file | `source-hash-start.txt` (`hash-source.sh`) |
| 環境 | go1.27.1 darwin/arm64、Apple M1、8 GB、node v24.3.0 | `env-start.txt` |

- source hash の対象は、tsc / tools の追跡・未追跡の `.go`・`.ts`・`.mod`・`.sum`・`.json`。結果の置き場 (`internal/ast/docs/_*`) は、検証の間に変わるので除いた。
- 一時物 (binary・fixture・manifest・overlay の写し・archive) は scratchpad に置いた。作り直すスクリプトと sha256 は結果の置き場に残した。
  - overlay は `overlay/gen.py` (と `gen_mut.py`) で作り直せる。各 overlay の内容の hash は `overlay/gen.txt`。
- build は `GOWORK=off`。

## 1. 範囲

commit ごとの変更 file (`range-by-commit.txt`):

| commit | 変更 | 許す範囲と照合 |
| --- | --- | --- |
| 2980ba4970 (測定) | docs 5、storeharness 9 (`driver.go`・`fixture.go`・`walker.go`・`pointerwalker.go` と test 5) | 範囲内 |
| cd66aaa4d3 | docs 1 | 範囲内 |
| de23e1f53d | docs 3、`storeharness/xctrace_test.go` | 範囲内 |
| 46a98e1abd (A) | `storechecker/links.go` (生成物)、`storechecker/links_manual.go` | 範囲内 |
| f71b24045d (B) | `ast/store/store.go`、`ast/store/utilities.go` (生成物)、`ast/store/utilities_manual.go` | 範囲内 |
| ec2196c835 (C) | `storeharness/adaptercost_test.go`・`fixture.go`・`measure_test.go` | 範囲内 |
| dd324e071a・9a4c31f470・67f21f0049 | docs だけ | 範囲内 |

- **生成物**: `links.go` は `nodeLinkStore` の 4 宣言が消えただけ。`utilities.go` は 5 関数が消え、到達数の comment が 451 → 446 になっただけ。
  - どちらも、HEAD の写しで `storetranslate` を走らせると生成物が変わらない (§4.1)。回帰の生成器の step でも変わらない (§9)。
- **`xctrace_test.go`**: `TestXctraceCheck` だけで、env (`STORE_CHECKER_XCTRACE_SIDE`) が無ければ skip する。
  - `init` も package 変数も無い。
  - ベンチから呼ばれる関数を定義していない (`checkPointer`・`checkStore` などは他の file のものを呼ぶだけ)。
- **instrumentation**:
  - 追跡する変更のうち、test 以外の file にある計数のコードは `FixtureProgram.Calls` (`fixture.go`) だけ。
  - これは実装指示書 §9 の 1 (method ごとの呼び出し回数を併記する) のための adapter の一部で、ゲートの binary の Store 側の区間にも入る。その費用は §5.3 の再生に含まれる (C の報告の単体値は 0.36 ms)。
  - ゲートの binary は、clean な repo の tree (status が空であることをスクリプトで確かめた) から、overlay なしで build した。`go version -m` の build 設定は `-tags=kperf` と CGO だけ (`build-kpc.txt`)。

## 2. 前提の等価

| 試験 | 結果 | file |
| --- | --- | --- |
| `TestCompilerProjectEquivalence` (§9 の回帰で実行) | equal。診断対象 124・walk 対象 79、診断 0・suggestion 274、型と symbol の記録は各 596,674 (distinct 81,270)、CompareSymbols の対 272,046、合成 import 0、mode の位置 184。実装の値と同じ | `regress-project.txt` |
| 印字の出力一致 `TestPrintOutputs` (全 201 target、間引きなし) | 合格。miss・hit の文字列と診断の message がすべて manifest と一致 | `print-outputs.txt` |

比較器の感度 (scratch overlay、`overlay/gen_mut.py`):

| 壊し方 | 結果 | file |
| --- | --- | --- |
| Store の型の walk の記録の最初の 1 件の値に `x` を足す (`cmprecord`) | `TestCompilerProjectEquivalence` が失敗。`class "walk: type"`、`versionMajorMinor` で `"6.0"` 対 `"6.0"x`。Pointer の再実行 10 回でも Pointer は変わらず、非決定の除外にならない | `sens/cmprecord-project.txt` |
| Store の印字の文字列の 1 つの最後の 1 文字を変える (`cmpprint`) | `TestPrintOutputs` が失敗。`"... string \| undefinee"`、manifest と違う | `sens/cmpprint-outputs.txt` |

- 記録の壊し方の試験は 1,874 秒かかった。不一致が出ると、compiler プロジェクトの Pointer 側を `pointerReruns` (10) 回作り直して走らせるため (`driver.go:355`)。

## 3. 隔離の監査 (契約 §3)

### 3.1 fixture と adapter

**型を再帰的に読む** (`TestVerifyFixtureTypes`、overlay の test、`audit-types-final.txt`)

- `Fixture` と `FixtureProgram` の型を、field・pointer・slice・map をたどって全部読んだ。
- 届いた名前付きの型の package は、ast・ast/store・collections・core・diagnostics・internal/sync・module・packagejson・storeharness・strings・symlinks・sync・tspath ほか。compiler・checker・parser・binder・printer・vfs・tsoptions の `ParsedCommandLine` には届かない。
- Pointer の AST への経路は 1 本だけだった: `Store.bound.diagnostics` (と `File.diagnostics`) の `*ast.Diagnostic`。その `file` field が `*ast.SourceFile`。
  - binder と parser は `file` を nil で作る (`storebinder/binder.go:2823`・`2831`、`bound.go:52` の comment)。
  - 実行時にも確かめた。compiler プロジェクトは diagnostic 0 件。段 2 の sample (§3.3 と同じ 1,130 構成) の fixture program は parse / bind の diagnostic 289 件 (message chain と related を含む) で、file を持つものは 0 件。
  - この 1 経路を許可リストにして、ほかに Pointer の型が出たら失敗する形にした。通常の build で合格し、故障 1 で失敗する (§3.2)。
- interface・func・unsafe.Pointer は、上の `*ast.SourceFile` の先 (`nodeData` など) と、symlink cache の `sync.Map` の内部だけ。後者の値は `KnownDirectoryLink` (値の struct) と文字列。

**adapter の全 method** (`fixture.go` を読んだ)

- 30 method はどれも、`p.fixture` の map、fixture の text から parse した `p.files`、fixture の値から作った `p.modules`・`p.symlinks`・`p.packageJsons` (fixture の text を `packagejson.Parse`)、`store.NewSyntheticImport` で作った合成 import から答える。
- Pointer の program・parser・SourceFile・host・cache を持つ field は無い。

**struct 鍵** (`TestVerifyNodeKeysCompiler`・`TestVerifySampleConfigurations`)

| 対象 | 文字列鍵 (EmitSyntax / UsageMode / FromSpecifier) | struct 鍵 |
| --- | --- | --- |
| compiler プロジェクトの fixture | 89 / 164 / 86 | 89 / 164 / 86 |
| 段 2 の sample 1,130 構成の合計 | 413 / 560 / 95 | 413 / 560 / 95 |

- 作り直しで件数は減らない (2 つの文字列鍵が 1 つの struct 鍵に潰れていない)。
- 記録に無い問い合わせ (file の root を usage にする) は 3 method とも panic し、メッセージに文字列の鍵が入る。例: `fixture: GetEmitSyntaxForUsageLocation(bundled:///libs/lib.es5.d.ts|0|218947|307) was not recorded`。

### 3.2 検出感度

各故障を 1 つずつ overlay で入れた (`overlay/gen_mut.py`)。

| # | 故障 | 検出 | file |
| --- | --- | --- | --- |
| 1 | `GetEmitModuleFormatOfFile` を Pointer program (`compiler.NewProgram`) への委譲に戻す | **型の監査**: `FixtureProgram.mutPointer: compiler.Program` ほか 165 件で失敗。**live heap profile**: Store の子プロセスの check の後の profile 2 本で、Pointer の frame を含む stack が 158 件と 188 件 (通常の build では 0 件)。同一性の試験は合格のまま (委譲しても答えが同じなので想定どおり) | `sens/mut1-types.txt`・`sens/mut1-audit.txt`・`sens/mut1-identity-compiler.txt` |
| 2 | 索引に無い syntax 依存の問い合わせに既定値を返し、索引を空にする | compiler プロジェクト: Store が panic (mode 0 の `GetResolvedModule` が記録に無い)。sample: 1,130 構成のうち 218 構成で不一致 (panic 188、診断の差 30) | `sens/mut2-identity-*.txt` |
| 3 | 合成 import を fixture から作らない (nil) | sample: 62 構成で不一致 (どれも Store の panic) | `sens/mut3-identity-sample.txt` |
| 4 | fixture の解決 (`Resolved`) の 1 件を落とす | compiler プロジェクト: Store が panic (`GetResolvedModule ... was not recorded`) | `sens/mut4-identity-compiler.txt` |

故障 3 の検出の内訳:

- sample のうち合成 import を持つ構成は 139。そのうち checker が合成 import を問うた構成 (2b-3 の `syntheticAsked > 0`) は 118。
- 検出した 62 構成は、すべて問うた構成。問うたのに記録が変わらない構成が 56 ある (例: `compiler/importHelpersWithImportStarAs.ts(esmoduleinterop=true,module=commonjs)`)。
- `fixtureIdentity` は `compareRecords` だけで比べ、2b-3 で塞いだ `compareModes` (返された specifier のノードの照合) を持たない。そのため、合成 specifier の答えの誤りは、記録に出る場合しか検出しない (残件 3)。

### 3.3 同一性の試験を再実行する

| 対象 | 結果 | file |
| --- | --- | --- |
| compiler プロジェクト (`TestFixtureIdentityCompilerProject`) | equal。files 124、modules 91、問い合わせ 914 | `identity-compiler.txt` |
| 段 2 の sample (`TestFixtureIdentityConfigurations`) | 1,130 構成すべて equal | `identity-sample.txt` |

- sample の選び方 (`sample.py`):
  - 2b-3 の strict で equal の 13,446 構成から、seed 20261003 で無作為に 1,000。
  - 合成 import を持つ 139 構成を全部加えた (重なりを除いて 1,130)。
  - 実装は全量 (15,205) を回したので、それとは別の選び方になる。
- id の一覧は `sample-ids.txt`。`STORE_CHECKER_CONFIG_FILTER` で渡した。

### 3.4 live heap profile

§6 の retained の実行 (HEAD の binary、Pointer と Store を交互に各 3 run、retained と GC の 2 種) の heap profile 36 本を、`audit-profiles.py` で調べた (`audit-profiles.txt`)。

- 数えたもの: stack に Pointer の frame を含む標本の数 (alloc_objects)。
- Pointer の frame の型: `tsc/internal/(parser|compiler|binder|checker|printer).`、`ast.(*NodeFactory)`、`ast.NewSourceFile`、`tsoptions.GetParsed*`、`cachedLib`、H1 の `newProgram`。

| 子プロセス | profile | Pointer の frame を含む stack |
| --- | ---: | ---: |
| Store (baseline・bound・checked × retained・GC × 3 run) | 18 | **0** |
| Pointer (同上) | 18 | 5,066 |

- `*ast.Node`・`*ast.SourceFile`・`compiler.Program` の割り当て元は、Store の子プロセスに無い。
- 印字が作る一時の Pointer ノードは、NB1' (storeprinter) なので、そもそも存在しない。
- 故障 1 を入れると、同じ監査で 158 件と 188 件が出る (§3.2)。profile に出ないことだけを証明とは扱わず、§3.1 の型の監査と構築経路の読みを合わせた。

### 3.5 fixture を作り直す

- after から `TestWriteCompilerProjectFixture` で作り直した。sha256 は `42e2cf35…` で、3 つの変更の計測の fixture (`3f44678b…`) と**一致しない** (`fixture-write.txt`)。
- **差分の中身**:
  - byte の差は `Modules` 表の 74〜77 番の並び (`ts.performance` / `performance.ts` と `ts.moduleSpecifiers` / `moduleSpecifiers.ts` の 2 対の入れ替わり) と、それを指す `Resolved` の 4 件の添字だけ。
  - module の添字を値に解くと、全 field が同一 (`fixture-semantic-diff.py`・`fixture-semantic-diff.txt`)。
  - したがって、3 つの変更は Pointer の Program の応答に影響していない。
- **並びが違う理由 (仮説)**:
  - 表の番号は、記録の run で checker が初めて問うた順に振られる (`Recorder.moduleIndex`)。
  - 同じ after から 2 回作り直すと byte まで一致した (`fixture-write-2.txt`)。
  - 一方、accessor tax 報告 (333 行) には、production が同じ source のまま作り直して sha が変わったとある。
  - 問う順に run ごとの揺れがあり、2 回の一致は偶然 (2 通りの入れ替わりなら半々) と見ている。確定はしていない (残件 4)。
- §5 の KPC と §6 の retained は、作り直した fixture (`42e2cf35…`) で取った。

## 4. 3 つの変更の監査

### 4.1 手書き版の意味

置き換え前の版 (46a98e1abd^ の `links.go` と `core.PagedLinkStore`、f71b24045d^ の `utilities.go` と `store.go`、Pointer の `ast/utilities.go`) と並べて読んだ。

**`nodeLinkStore` (`storechecker/links_manual.go`)**

- 旧版の鍵は `NodeKey.Uint64()` = `StoreID<<32 | ref`。`PagedLinkStore` は page 番号 `key>>8` = `StoreID<<24 | ref>>8` と、要素 `ref & 255` で引く。
- 新版は `stores[StoreID][ref>>8][ref&255]`。(StoreID, ref) と旧鍵は 1 対 1 で、page の分け方 ((StoreID, ref>>8) ごと) も同じ。同じ鍵に同じ `*V`、`TryGet` の nil も旧版と同じ条件になる。
- `Get` だけが `newPage` で確保する。`TryGet` と `Has` (`TryGet` を呼ぶ) には確保の経路が無い。
- nil の Node は `Key()` が `NodeKey{}` を返すので、Store 0 の ref 0 (sentinel) の 1 つの鍵になる。旧版の鍵 0 と同じ。
- **差分試験** (`TestVerifyNodeLinkStore`、overlay、`links-diff.txt`): 合格。
  - 対象: source の Store 3 つ (300〜3,000 文) と合成 import の Store の 22,508 ノード、nil 3 種 (零値と 2 つの Store の sentinel)。
  - 新旧に同じ無作為の操作を 20 万回 (`Get` 66,768、`TryGet` 66,554、`Has`)。
  - 確かめたこと: 鍵ごとの `*V` が一定、異なる鍵は異なる `*V`、`TryGet` の nil と値と `Has` が旧版と一致、nil 3 種は 1 つの鍵、`Has`・`TryGet` の `AllocsPerRun` は 0 で表を伸ばさない。

**`ast/store/utilities_manual.go`**

| 関数 | 元の版との関係 |
| --- | --- |
| `FindAncestorOrQuit` | `node.h != nil && node.h.kind != KindUnknown` は `IsNil()` の否定、`node.s.node(node.h.parent)` は `Parent()` の本体。同じ |
| `GetRootDeclaration` | BindingElement のときだけ遅い経路 (元のループそのもの) を呼ぶ。そうでなければ元のループは 1 回も回らず node を返すので同じ |
| `GetCombinedNodeFlags` | BindingElement・VariableDeclaration・VariableDeclarationList のときは元の本体。ほかの kind では、元の版は `GetRootDeclaration` が node を返し、親へ上がる分岐はどれも成り立たない。例外は VariableStatement で、自分の flags を自分に OR するだけ。結果は `node.Flags()` で同じ。nil の扱い (h が nil なら `Kind()` で panic、sentinel は flags を返す) も同じ |
| `IsGlobalSourceFile` | `&&` の右辺を noinline の関数にしただけ。評価順と短絡は同じ |
| `HasQuestionToken` | 元は `IsQuestionToken(t)` = `!t.IsNil() && t.Kind() == QuestionToken`。新は `t.h != nil && t.h.kind == QuestionToken`。kind が QuestionToken なら kind ≠ Unknown なので `IsNil()` は偽。どの Node でも同値 |
| `GetSymbolId` | 速い経路は `gid` が 0 でなければ返す。元の版も同じ値を返す。0 のときは元の本体 (再 Load と CAS) をそのまま呼ぶので、並行の採番も同じ |

Pointer の `ast` の同名の関数 (`ast/utilities.go:967`・`1173`・`1196`・`2526`・`3756`) とも、nil を `IsNil()` に読み替えれば同じ形。

**`store.go`**

- `(*Store).node`: 範囲外の値は `s.foreignNodes[e^foreignBit]` で引く。
  - foreignBit のある値では `e^foreignBit` と `e&^foreignBit` は等しい。
  - foreignBit の無い壊れた値では添字が 2^31 以上になり、foreignNodes の長さ (2^31 未満) を必ず超えて bounds check で panic する。
  - `storechecks` の build では、その前に明示の検査で panic する。
- **試験** (`TestVerifyNodeCorruptEdge`、overlay、`nodepanic-none.txt`・`nodepanic-storechecks.txt`): 両 build で合格。
  - 壊れた辺 4 種 (4・5・1000・2^31−1) を、`node`・`Parent`・inline された `FindAncestor`・`File` の 4 経路で引いた。
  - 通常の build は `runtime error: index out of range [2147483652] with length 1` など、`storechecks` は `store: invalid node edge` で、全部 panic した。
  - 正しい foreign の辺は、今までどおり foreign 表の Node に解ける。
- `Node.File`: 元は「nil なら nil、自分の Store の file があれば返す、なければ親を辿る (`parentFile`)」。新は「nil でない間、Store の file があれば返し、なければ親へ」。同じ。

**hash の行**

| 試験 | 結果 | file |
| --- | --- | --- |
| HEAD の写し (scratchpad の `git archive`) で `storetranslate` | exit 0。生成物 (`storechecker/*.go`・`ast/store/*.go`) の hash は前後で同じ | `hash-unchanged.txt`・`batch3.log` |
| `links_manual.go` の `nodeLinkStore` の hash を 1 文字変える | exit 1: `the Pointer declaration nodeLinkStore changed (hash 23820a26eafb977a, recorded 23820a26eafb9770)` | `hash-broken-links_manual.txt` |
| `utilities_manual.go` の `GetCombinedNodeFlags` の hash (同じ hash の 2 行) を変える | exit 1: 2 宣言とも名指しで止まる | `hash-broken-utilities_manual.txt` |

repo の tree の生成器の再実行は §9。

### 4.2 StoreID の重なり

**コード**: 採番の経路は次のとおり。

| Store | 採番 |
| --- | --- |
| source | `NewFixtureProgram` (と H1 の `newProgram`) が file の順に `StoreID(i)`、0..nFiles−1 (`fixture.go:687`)。`NewIDSpace(nFiles)` は counter を nFiles から始める |
| 遅延 JSDoc | `FinishLazyJSDoc` が host の Store の `space.Next()` (`ast/store/jsdoc.go:124`) |
| checker の合成・印字の EmitContext | `Factory.newStore` が region の `space.Next()` (`factory.go:168`)。region は `NewNodeRegion(program.IDSpace())`、EmitContext は `NewEmitContextIn(c.factory.Region())` で同じ space。space の無い region は作れない (`NewNodeRegion` が panic) |
| `store.NewSyntheticImport`・`ParseIsolatedEntityName` | file の Store の `space.Next()` (`syntheticimport.go:23`・`storeparser/isolated.go:20`)。space が無ければ panic |

- source 以外は、すべて program の 1 つの IDSpace の atomic な CAS で採番する。範囲の端では counter を進めずに panic するので、再発行も無い。
- 例外は「program の外の host の遅延 JSDoc は StoreID 0」(`FinishLazyJSDoc` の comment)。program の source の Store は space を持つので、program の中では起きない。

**実測** (count overlay で `nodeLinkStore` の `Get`・`TryGet` に来た鍵の StoreID ごとに、`*Store` の同一性と IDSpace を記録):

| 入力 | nodeLinkStore | 来た Store (種類) | 重なり |
| --- | ---: | --- | ---: |
| compiler プロジェクト、KPC の check | 1 | source 109、region 1 | 0 |
| 同、check + driver の全仕事 (walk・印字を含む) | 2 | source 218、region 2 (延べ) | 0 |
| 段 2 の sample 1,130 構成 (記録と再生の両方の checker) | 1,844 | source 5,530 (延べ) | 0 |

- 別の IDSpace の Store も来なかった。nil の鍵は 0 回。
- **限界**: 実測で links の鍵に来たのは source と region の Store だけ。遅延 JSDoc・program の合成 (合成 import など) の Store は、この入力では `symbolNodeLinks` の鍵にならなかった。
  - region の Store は、checker の合成と EmitContext のどちらかを区別していない (同じ region の token を持つ)。
  - これらの重なりの無さは、上のコードの読みだけによる。

### 4.3 inline

**`-m=2`** (`./internal/ast/store` だけ、`m2-none.txt`・`m2-storechecks.txt`)

| 関数 | tag なし | `storechecks` | 3 つの変更の報告 |
| --- | ---: | ---: | --- |
| `(*Store).node` | 29 | 38 | 29 / 38 |
| `Node.File` | 70 | 79 | 70 / 79 |
| `FindAncestor` | 80 | 89 (inline 不可) | 80 / 89 |
| `FindAncestorOrQuit` | 79 | 88 (inline 不可) | 79 / 88 |
| `GetRootDeclaration` | 72 | 72 | 72 / 72 |
| `GetCombinedNodeFlags` | 79 | 79 | 79 / 79 |
| `IsGlobalSourceFile` | 70 | 70 | 70 / 70 |
| `HasQuestionToken` | 74 | 74 | 74 / 74 |
| `GetSymbolId` | 79 | 79 | 79 / 79 |
| `List.At` (参考) | 69 | 78 | 69 / 78 |

tag なしで 9 つすべてが `can inline`。

**objdump** (`go tool objdump -s 'internal/(store|ast/store)'`、`calls-after.txt`・`calls-de23.txt`)

| binary | 8 関数への直接の `CALL` | storechecker の callback への `CALL` |
| --- | ---: | --- |
| after (ゲートの kperf binary、`155ddf37…`) | **0** | `(*tracedTypeAdapter).Display.func1` の 1 本だけ |
| de23e1f53d (3 つの変更の前、archive から build、`9043c2e6…`) | 325 (`Node.File` 181・`FindAncestor` 52・`GetRootDeclaration` 25・`GetSymbolId` 22・`HasQuestionToken` 18・`GetCombinedNodeFlags` 11・`IsGlobalSourceFile` 8・`FindAncestorOrQuit` 8) | 同じ 1 本 |

報告の 325 → 0 を再現した。

## 5. KPC の再測定

ユーザーが別のターミナルで `sudo bash kpc.sh | tee kpc.txt` (KPC 1) と `kpc2.sh | tee kpc2.txt` (KPC 2) を実行した。

- 両方とも、こちらの作業の切れ目 (`busy.flag`) で、かつ load < 1.5 になるまで待ってから始まる。開始時の load は KPC 1 が 1.29、KPC 2 が 1.49。
- binary の sha256 は `kpc-binary.sha256` (= `build-kpc.txt`) と `kpc2-binary.sha256`。
- fixture の sha256 は `42e2cf35…`、manifest の sha256 は `67273879…`。
- 全実行の終了コードは 0 (KPC 1 は 29 回、KPC 2 は 26 回)。

### 5.1 Check

**区間** (`measure_test.go` を読んだ)

- 区間の中: `NewChecker`、診断対象の各 file の `GetDiagnostics`、`GetGlobalDiagnostics`。suggestion は含めない (§9 の 2)。
- 区間の外: parse・bind・fixture の読み込みと `NewFixtureProgram`。どちらの側も `b.StopTimer()` の中で、`session.Measure` の前に済ませる。
  - `NewChecker` が中で呼ぶ `BindSourceFiles` は、両側とも bind 済みの file を飛ばす (`storebinder.BindSourceFile`・`compiler.(*Program).BindSourceFiles` の `IsBound` 判定)。
- 同じ仕事か:
  - file の列: Pointer は `projectTargets` (program の順、`SkipDefaultLibCheck` なら default lib を除く)、Store は `fixtureDiagFiles` (fixture の file の順に同じ規則)。fixture の file の順は Pointer program の順をそのまま記録したもの。compiler プロジェクトは 124 file のすべてが対象。
  - API は同じ 3 つ。
- Store 側の Program は測定用 adapter (§9 の 1)。

**値** (KPC 1、`kpc-summary.txt`、5 run の median)

| | cycles | inst | IPC | cycles/node | inst/node |
| --- | ---: | ---: | ---: | ---: | ---: |
| Pointer (H1 driver) | 2,602.1 M | 8,596.1 M | 3.304 | 2,769 | 9,148 |
| Store (測定用 adapter) | 2,767.2 M | 9,238.5 M | 3.339 | 2,945 | 9,832 |
| 比 Store / Pointer | **1.0635** | 1.0747 | | | |
| Program (`BenchmarkCheckKPCProgram`) | 2,571.6 M | 8,518.1 M | 3.312 | | |
| V1 (`BenchmarkASTCheckKPCV1`、compiler の binary) | 2,606.0 M | 8,576.4 M | 3.291 | | |

- node 数は 939,7xx (per node の値からの逆算。実装は 939,772)。
- run ごとの比 (同じ run の Pointer と Store): 1.0523・1.0463・1.0635・1.0557・1.0595。min/min は 1.0589。
- **A/A**: Pointer は cycles の幅 3.41%・inst 0.19%、Store は cycles 2.76%・inst 0.31%。
  - cycles の幅は線 2% を超えた。run 1・2 では、Pointer・Store・Program・V1 の 4 本がそろって run 3〜5 より 2.5〜3% 高い。
  - 共通の変化なので、開始直後の環境 (load 1.29 から下がりきっていない) と見ている。run 3〜5 だけなら Pointer の幅は 0.33%。
- **program 側の費用**: Program / Pointer は cycles 0.988・inst 0.991。V1 / Pointer は 1.0015・0.998。V1 は旧基準値 (2.60 G / 8.48 G) と cycles で一致し、inst は 1.1% 多い。

**KPC 2 の再測定** (`kpc2-path-summary.txt`、5 run): after の比は cycles 1.0493・inst 1.0740。Pointer の cycles の幅は 2.14%、Store は 1.34%。

**照合** (3 つの変更の報告の 1.053 と)

- Store の値は一致した: 2,767.2 対 2,766.1 M cycles、9,238.5 対 9,240.4 M inst。
- 比の差 (+0.010) は Pointer 側から来る。Pointer の inst は、今回 8,596 M・実装 (2980ba4970) 8,589 M に対して、3 つの変更の 4 本の binary は 8,672〜8,677 M (+0.9%)。
- **原因を確かめた (残件 1)**:
  - 3 つの変更の binary は `git archive` で scratchpad に出した tree から build されている。
  - `repo.TestDataPath()` は compile 時の source の path (`runtime.Caller`) から決まるので、Pointer 側は scratchpad の長い path の下の compiler プロジェクトを読む。一方、Store 側は repo の path で記録した fixture を使う。
  - KPC 2 で、同じ fixture・同じセッションの中で、after (repo から build) と 3 つの変更の C の binary (archive から build、`38cf2146…`) を交互に 5 run 取った。
  - 結果: Pointer の inst は C が +78.9 M (+0.92%)、Store は +6.0 M (+0.06%)。
  - inst の比は C で 1.0649、after で 1.0740。3 つの変更の計測の比は、path の非対称で 1% ほど低く出ていた。
  - cycles は KPC 2 の揺れ (C の Pointer の幅 4.7%) に埋もれるが、向きは同じ (C の Pointer の median が 0.7% 高い)。
- ゲートの判定は変わらない。KPC 1・2 のどの run でも、after の比は 1.10 を下回る (最大 1.0635)。

### 5.2 Walk / Parse / Bind

同じ binary の中の `/store` ÷ `/pointer` の cycles の median。3 run × `-benchtime 20x -count 5` (各 15 標本)。sub-benchmark ごとに別の鍵で集計した (`kpc-sum.py`)。

| 段 | 比 (cycles) | inst の比 | 幅 P / S | 線 | 判定 | 照合 |
| --- | ---: | ---: | --- | --- | --- | --- |
| Walk | 1.0589 | 1.2966 | 5.60% / 0.82% | ≤ 1.15 | 合格 | 3 つの変更 1.0869 (inst 1.2958) |
| Parse checker.ts | 0.8235 | 0.8323 | 1.16% / 1.32% | ≤ 1.00 | 合格 | 7d-2a 検証 0.832 |
| Parse dom | 0.8274 | 0.9045 | 0.70% / 0.39% | ≤ 1.00 | 合格 | 7d-2a 検証 0.825 |
| Bind | 1.0047 | 1.1393 | 1.47% / 1.47% | ≤ 1.10 | 合格 | 3 つの変更 1.0045 |

- Walk の比の差 (1.059 対 1.087) は、Pointer の cycles の幅 (5.6%) の内。inst の比は 3 つの変更と同じ。
- Parse は 3 つの変更で取っていなかった。f71b24045d が `node` を変えた後も、7d-2a の値と同じ水準。

### 5.3 偽 Program 全体の費用 (報告のみ)

**測り方** (`overlay/replay/zz_replay_test.go`)

- `FixtureProgram` を包む記録器で、KPC の Store の check と同じ仕事 (`checkStore`) を 1 回走らせ、`storechecker.Program` の全 method の呼び出しを (method, 引数) で記録した。
- 同じ `FixtureProgram` に再生する。1 op = 1 回の check 分の呼び出し。全体と method ごとに測った。
- KPC は KPC 2 の中で 3 run × `-benchtime 20x -count 5` (`kpc2-replay-summary.txt`)。ns は通常の GC で `-count 5` (`replay-ns.txt`)。

**値**

| method | 回数 | cycles | inst | cycles / 回 | ns (median) |
| --- | ---: | ---: | ---: | ---: | ---: |
| 全体 | 168,306 | **17.42 M** | **96.20 M** | 103.5 | 5.81 ms |
| `GetResolvedModule` | 22,810 | 10.23 M | 55.05 M | 448.6 | 3.87 ms |
| `GetEmitModuleFormatOfFile` | 77,070 | 3.53 M | 22.63 M | 45.8 | 1.26 ms |
| `GetModeForUsageLocation` | 22,810 | 1.20 M | 6.32 M | 52.6 | 0.41 ms |
| `GetEmitSyntaxForUsageLocation` | 22,716 | 1.14 M | 6.27 M | 50.1 | 0.41 ms |
| `GetSourceFileForResolvedModule` | 22,810 | 0.91 M | 5.90 M | 39.8 | 0.32 ms |
| `GetImpliedNodeFormatForEmit` | 86 | 0.02 M | 0.04 M | | 0.002 ms |
| `SourceFiles`・`Options`・`IDSpace`・`BindSourceFiles` | 各 1 | — | — | | — |

- 回数: Calls が数える 30 method の和は 168,302 回で、C の報告と同じ。表の 168,306 回には、Calls が数えない 4 method の各 1 回を足してある。
- 1 回だけの 4 method の値 (約 12,500 inst・8,500 cycles) は、kperf の `Read` 2 回の費用 (Session は差し引かない) で、method の費用ではない。
- **割合**: KPC 1 の Store の Check に対して、cycles 0.63%・inst 1.04%。
- **見積りとの照合**: 3 つの変更の報告の約 7 ms (約 22 M cycles) に対し、KPC で 17.4 M cycles (3.2 GHz で約 5.4 ms)、ns で 5.8 ms。見積りの 8 割で、大半 (59%) は `GetResolvedModule` の `fmt.Sprintf` の鍵。
- **限界**: 再生は呼び出しを小さなループで続けて行うので、check の途中に散らばった実際の呼び出しより cache が温まっている。この値は下限寄りと見ている (未検証)。
- **比への影響**: adapter の費用を Store から全部引くと、KPC 1 の比は 1.0635 → 1.0568。Pointer 側の本物の `compiler.Program` の同じ method の費用は測っていないので、真の偏りはこの間にある (7e の CLI 結線で消える、2026-10-03 のユーザーの決定どおり)。

## 6. retained / GC の再測定

**手順** (`retained_test.go` を読み、実行した。`retained-check.txt`・`retained-check/`・`retained-summary.txt`)

- Pointer → Store の順に、別の子プロセスで各 3 run。各側に retained の子 (HeapAlloc と profile) と GC の子 (`GODEBUG=gctrace=1`) がある。計 12 プロセス。
- 確かめたこと:
  - **共通の baseline**: どちらの子も、まず fixture だけを読んで (`readFixture`) baseline を取る。
  - **GC 2 回**: 各段 (baseline・bound・checked) で `runtime.GC()` を 2 回呼んでから `HeapAlloc` と profile を取る。sync.Pool の victim cache は 2 回目で空になる。
  - **KeepAlive**: program・checker・fixture を、子の終わりまで `runtime.KeepAlive` する。
  - **sync.Pool の条件**: 両側とも各段で GC 2 回なので揃っている。
  - **混在の禁止**: GC の子は通常の GC (GOGC 既定) で、KPC (GC オフ) の値は使っていない。Store の子は Pointer program を作らない (§3.4 の profile で 0 件)。
- mark CPU は、各段の 2 回目の強制 GC の gctrace の行から、assist + background + idle を自分で読んだ (`retained-sum.py`)。

| 段 | Pointer HeapAlloc | Store HeapAlloc | 増分の比 | 絶対量の比 | scan heap P / S | mark CPU P / S (ms) | mark CPU の比 |
| --- | ---: | ---: | ---: | ---: | --- | --- | ---: |
| baseline (fixture だけ) | 11.43 MB | 11.43 MB | | | 1.4 / 1.4 MB | 0 / 0 | |
| parse + bind の後 | 133.75 | 84.50 | 0.597 | 0.632 | 113.4 / 21.4 MB | 69 / 13.3 | 0.193 |
| check の後 | 237.55 | 199.88 | **0.833** | 0.841 | 211.0 / 126.2 MB | 160 / 104 | **0.650** |

- 3 run の差は HeapAlloc で 0.1 MB 以内。mark CPU は Pointer 160〜166・Store 103〜107 ms。
- background だけの比は 0.634。
- check の後の最後の強制 GC の行 (run 0): Pointer `0.26+0/41/125+0.028 ms cpu, 237->237->237 MB`、Store `0.26+0/26/77+0.056 ms cpu, 199->199->199 MB`。
- 3 つの変更の報告 (0.834・0.641) と幅の内で一致する。

**内訳** (`breakdown.py`、inuse_space の stack を規則で分類、run 0、`retained-breakdown.txt`)

| 分類 | Pointer (check の後) | Store (check の後) |
| --- | ---: | ---: |
| AST と bind (Pointer は program 全体) | 105.4 | 40.2 (parse) + 20.9 (Bound) |
| checker のその他 | 69.3 | 83.5 |
| links | 34.0 | 33.8 |
| source の text | 8.5 | 10.4 |
| adapter / fixture | 8.6 | 10.2 |
| 遅延 JSDoc の Store | — | 3.5 |
| 合成の Store | — | 2.5 |
| その他 | 3.0 | 2.0 |
| **合計 (profile の推定)** | 228.8 | 207.1 |
| HeapAlloc | 237.5 | 199.9 |

- **和と全体の照合**: 分類の和は profile の合計そのもので、HeapAlloc とは Pointer −3.7%・Store +3.6% 違う。heap profile は標本 (平均 512 KB ごと) からの推定なので、この幅は標本の誤差と読む。
- **adapter / fixture の分**: 両側に入っている (fixture の JSON の読み込み 8.6 MB。Store は `NewFixtureProgram` の表なども加わり 10.2 MB)。
- **source の text**: Store は `NewFixtureProgram` が fixture の bytes を string にした分、Pointer は compiler host が file を読んだ分 (`os.readFileContents`)。両側とも heap にある。
- **分類の規則**: 実装の内訳の規則は残っていない。実装の表 (links 24.8 対 18.1 MB など) とは分け方が違うので、数字を直接は比べない。
- foreign 表は、名前の出る割り当て元が無い。

**check の間の増分** (報告のみ)

| | HeapAlloc | scan heap |
| --- | ---: | ---: |
| Pointer (check の後 − parse + bind の後) | +103.8 MB | +97.6 MB |
| Store | +115.4 MB | +104.8 MB |

- 3 つの変更の生出力から読んだ値 (+103.8・+115.4 MB) と一致する。
- **profile の差分** (inuse_space、check の後 − parse + bind の後、run 0、`retained-check/*-0.check-delta.top.txt`):
  - Pointer: 主な割り当て元は `slices.Grow` (型の arena の 2 種、14.7・11.8 MB)、`newObjectType` 12.0 MB、`getUnionOrIntersectionProperty` 5.5 MB、`Relation.set` 5.1 MB、`core.LinkStore` 4.6 MB。
  - Store: 主な割り当て元は `slices.Grow` 16.0・8.6 MB、`newObjectType` 13.0 MB、**`nodeLinkStore.newPage` 7.5 MB**、`getUnionOrIntersectionProperty` 5.5 MB、`instantiateSymbolTable` 4.5 MB。
  - `nodeLinkStore` の page は §8 の 3,661 枚 × 2 KB (`SymbolNodeLinks` は 8 B × 256) = 7.3 MB と合う。Pointer の同じ表は 1,213 枚 × 2 KB = 2.4 MB で、差の約 5 MB は page の密度から来る (§8)。

## 7. 印字の全区間

**区間** (`print_test.go`・`walker.go`・`pointerwalker.go` を読んだ)

- miss・hit の区間 `print` は、型の要求ごとに nodebuilder (`TypeToTypeNode`) + storeprinter (`NewPrinter`・`Write`) + 文字列取得 (`strings.Clone`) を行う。symbol の要求は `SymbolToStringEx`。Pointer 側も同じ組み立て。
  - 取得済みの文字列を返すだけにはなっていない。
  - 印字の変換 (NB1' なので Store → Pointer の変換器は無い) や cache の構築を区間の外に置いていない。
  - 小さな非対称が 1 つある。Store の EmitContext は checker ごとに区間の外で 1 つ作る (`newStoreWalker` の `NewEmitContextIn`)。Pointer は区間の中で pool から取る (`printer.GetEmitContext`)。どちらも要求ごとに `Reset` して使い回すので、違いは最初の 1 回の構築だけ。
- **cache の状態**:
  - miss: target ごとに新しい checker を作り、区間の外で診断対象を check して要求の型と symbol を解いてから、1 回印字する。
  - hit: 同じ checker で、同じ列をもう一度印字する。
- **内訳の別の実行**: build (nodebuilder だけ) は別の sub-benchmark の別の区間で、miss の区間に計数のコードは入っていない。
- **manifest** (`print-manifest.txt`): 201 target、要求 1,159,486 件 (compiler 998,985)、message 18,230 件。実装の件数と一致した。
  - sha256: manifest.json `67273879…`、200 構成の fixture の連結 `886c7986…`。
  - Pointer と Store は同じ file を読むので、要求列は両側で同じ。

**KPC** (KPC 2、`STORE_CHECKER_PRINT_STRIDE=1000`、3 run の median、`kpc2-print-summary.txt`)

| 集合 / 操作 | cycles P | cycles 比 | inst 比 | 幅 P / S | 実装 (2980ba4970) の cycles 比 |
| --- | ---: | ---: | ---: | --- | ---: |
| compiler / miss | 323.5 M | 1.081 | 1.069 | 1.4 / 1.3% | 1.105 |
| compiler / hit | 120.9 M | 1.116 | 1.099 | 1.6 / 0.6% | 1.151 |
| compiler / build | 312.4 M | 1.094 | 1.084 | 1.4 / 1.3% | 1.093 |
| compiler / 診断の生成 | 2,580.8 M | 1.036 | 1.070 | 0.6 / 0.9% | 1.124 |
| configurations / miss | 26.8 M | 1.263 | 1.668 | 17.1 / 1.6% | 1.319 |
| configurations / hit | 17.4 M | 1.082 | 1.284 | 1.7 / 2.6% | 1.044 |
| configurations / build | 21.8 M | 1.417 | 1.776 | 1.0 / 3.0% | 1.413 |
| configurations / 診断の生成 | 4,283.8 M | 1.119 | 1.173 | 1.0 / 1.3% | 1.145 |

**ns・B/op・allocs/op** (通常の GC、5 run、benchstat、`print-bench.txt`・`print-benchstat.txt`)

| 集合 / 操作 | sec/op (S 対 P) | B/op (Store / Pointer) | allocs/op |
| --- | --- | --- | --- |
| compiler / miss | −23.8% | 233.4 / 696.8 MiB | +44.3% |
| compiler / hit | −33.3% | 214.7 / 687.5 MiB | +4.7% |
| compiler / build | +26.4% | 229.6 / 27.3 MiB | +44.4% |
| compiler / 診断の生成 | +2.7% | 256.0 / 200.4 MiB | +9.5% |
| configurations / miss | +104.2% | 103.2 / 5.9 MiB | +13.9% |
| configurations / hit | +42.5% | 18.4 / 3.9 MiB | +5.6% |
| configurations / build | +121.0% | 103.0 / 2.9 MiB | +9.7% |
| configurations / 診断の生成 | +14.2% | 5,540 / 244 MiB | +9.7% |

- **退行**: 実装報告 §6.3 の `typeToString` ごとの容量 4,096 の Store (約 420 KB) の確保が残っている。B/op は実装の値とほぼ同じで、configurations の build の cycles 比 (1.417) も同じ。
- **縮んだもの**: compiler の診断の生成 (check を含む) は 1.124 → 1.036、miss は 1.105 → 1.081、hit は 1.151 → 1.116。A と B が check と印字の両方の経路の費用を下げたためと読む (仮説。印字の経路の関数別の帰属は取っていない)。
- 2026-10-03 のユーザーの決定どおり、この退行は 7e の前に直す。本段では直さない。

## 8. 判断材料の計数

自分の overlay (`overlay/count/`) で、KPC と同じ check (compiler プロジェクト、`checkStore`) を 1 回数えた (`count-compiler.txt`)。

**合成の Store**

| | Store の数 | 使用行 / 容量 | 実装 |
| --- | ---: | --- | --- |
| checker の Factory | 1 | 1,023 / 4,096 | 1 Store、1,023 / 4,096 |
| node builder の EmitContext の Factory | 99 (Factory 1 つ) | 最小 1・中央 8・最大 53、計 953 | 99 Store、各 1〜53 |

**R4' のコピー** (`List.Nodes()`)

| 呼び出し元 | 呼び出し | 確保 | 要素 | bytes |
| --- | ---: | ---: | ---: | ---: |
| `storechecker/checker.go:28998` (`getEffectiveCallArguments`) | 54,251 | 49,906 | 92,602 | 1,481,632 |
| `checker.go:8276` (`resolveCall` の型引数) | 53,117 (nil の list を含む) | 550 | 724 | 11,584 |
| `jsdoc.go:102` | 2,890 | 2,890 | 3,982 | 63,712 |
| `grammarchecks_manual.go:147` | 1,571 | 1,475 | 9,894 | 158,304 |
| `utilities.go:1057` | 1,562 | 1,441 | 6,459 | 103,344 |
| `ast/store/utilities.go:2074` | 1,428 | 1,428 | 1,938 | 31,008 |
| `checker_manual.go:413` | 227 (nil の list を含む) | 82 | 90 | 1,440 |
| `storeprinter/printer.go:4464` | 167 | 167 | 307 | 4,912 |
| `checker.go:28969` (spread) | 137 | 137 | 346 | 5,536 |
| `utilities.go:1059` | 7 | 7 | 358 | 5,728 |
| 計 | 115,357 | **58,083** | **116,700** | **1,867,200** |

- 確保数・要素数・bytes は、全 site で実装の値と一致した。
- 呼び出し数が 2 site で多いのは数え方の違い。こちらは nil の list (`IsNil()` で早く返る呼び出し) も数え、実装は数えていない。それを除けば実装の 62,645 と合う。
- 行番号の違い (実装 29000 / 8277 / 2110) は、計数の overlay の差し込みと版の違い。

**`getEffectiveCallArguments` の結果が resolveCall の外へ出る経路** (コードを読んだ答え): 無い。

- 呼び出し元は 4 箇所。
  - `resolveCall` (`CallState.args`、`CallState` は stack 上の値)
  - `getContextuallyTypedParameterType` (`checker.go:28364`、局所変数 → `getSpreadArgumentType`)
  - `getContextualTypeForArgument` (`28661`、`store.Index` に渡すだけ)
  - `utilities.go:296` (`len` だけ)
- `args []store.Node` を受ける 9 関数 (`hasCorrectArity`・`isSignatureApplicable`・`inferTypeArguments`・`getCandidateForOverloadFailure`・`getArgumentArityError` など) に、slice を struct の field・links・map・closure に入れる行は無い。要素の Node を値として保存することはある。
- 実装の答えと一致する。

**links の外側の表** (compiler プロジェクトの check の後)

| | Store (`nodeLinkStore`) | Pointer (`PagedLinkStore`) |
| --- | --- | --- |
| 外側の表の長さ | 1,238 (StoreID の最大 + 1) | — |
| page 表を持つ Store | 110 (source 109・region 1) | — |
| page 表の長さの和 | 3,722 (source 3,718・region 4) | 1,213 (`pageList`) |
| 確保された page | **3,661** (source 3,657・region 4) | **1,213** (`pageMap` 0) |
| 使われた要素 (`Get` の鍵) | 308,575 | 308,575 |
| 密度 | **0.329** (source 0.330・region 0.051) | **0.994** |

- 使われた要素の数は両側で同じで、page の数は Store が 3.0 倍。
  - Pointer の `GetNodeId` は links を持つノードにだけ番号を振るので、page が詰まる。
  - Store の鍵は NodeRef (全ノードの通し番号) なので、page の中が疎になる。
- `SymbolNodeLinks` は 8 B なので page は 2 KB。差は 2,448 page × 2 KB ≈ 5 MB で、§6 の `newPage` 7.5 MB と合う。
- 外側の表の長さ 1,238 は、check の途中で遅延 JSDoc などに StoreID が振られるため (外側の表の 1 要素は 24 B、計 30 KB)。

## 9. 回帰

`regress.sh` (`regress.log`・`regress-*.txt`)。HEAD 67f21f0049、未コミットの差分なし (diff の hash は空の hash)。

| step | 結果 |
| --- | --- |
| `go build ./...` (tag なし / `storechecks`) | 合格 / 合格 |
| `go vet ./internal/ast/... ./internal/store...` (tag なし / `storechecks`) | 合格 / 合格 |
| 生成器 (`node tools/scripts/tsc/generate.ts`、`storetranslate`) | 前後で `git status` と diff の hash が同じ (どちらも空) |
| 段 1 strict `TestCheckerCorpus` | 合格。比較 17,413 / equal 17,413。除外は Pointer の panic 1・Pointer の非決定 1 (`jsxNamespaceImplicitImportJSXNamespace.tsx`) で、3 つの変更の報告と同じ |
| `TestCompilerProjectEquivalence` | 合格 (§2) |
| `TestModeCorpus` | 合格。17,415 file、4,996 位置、不一致 0 |
| 段 2 strict `TestCheckerConfigurations` | 合格。15,205 構成、equal 13,446、runner-skipped 1,759、mismatch 0。合成 import を持つ 139 構成のうち Store の checker が問うたのは 118 |
| package の試験 (tag なし / `storechecks`、上の 4 つを除く) | どちらも既知の `FuzzEquivalence/a708303f611b95c8` (storeparser) だけが失敗。ほかの package は合格 |

**source の不変**: 終了時の source hash (`source-hash-end.txt`) は開始時と byte まで一致した (18,126 file)。終了時の `git status` の untracked は、この報告だけ。

## 10. 残件

### 残件 1: 3 つの変更の Check KPC は、Pointer 側だけが長い path で動いていた

- **問題**: 3 つの変更の Check の比 (1.053) は、Pointer の仕事が Store より約 0.9% (inst) 多い条件で取られていた。比は 1% ほど低く出ている。
- **証拠**: §5.1。
  - 3 つの変更の 4 本の binary の Pointer の inst は 8,672〜8,677 M、repo から build した binary では 8,589 M (実装) と 8,596 M (今回)。
  - 同じセッションの交互の 5 run で、C の binary の Pointer は after より +78.9 M inst、Store は +6.0 M。
- **再現**: `kpc2.sh` の path の段 (`## path run`)。
- **仮説 (仕組み)**: `repo.TestDataPath()` が compile 時の source の path から決まり、archive を展開した scratchpad の長い path の下の file を Pointer が読む。check の中の path 文字列の処理 (hash・比較) が長さに比例して増える。Store 側は repo の path で記録した fixture を使うので変わらない。
  - 仕組みのうち「どの関数の inst が増えたか」は取っていない。
- **修正案**: 比べる binary を、Pointer と Store が同じ path の file を読む条件で build する。
  - 例 1: 全 binary を repo の tree から順に build する (commit ごとに checkout せず worktree を同じ深さの path に置く)。
  - 例 2: fixture をその binary 自身で記録し直す。
- **受入条件**: 同じ source の Pointer の inst が、binary の間で ±0.3% に収まる。
- 7d-2c のゲートの判定は変わらない (今回の値 1.0635 でも合格)。

### 残件 2: Check の Pointer の A/A が 2% を超える

- **問題**: cycles の幅が KPC 1 で 3.41%、KPC 2 で 2.14% (after) と 4.71% (C)。指示書の線 2% を超える。
- **証拠**: §5.1。inst の幅は 0.2% 前後で安定している。KPC 1 では、run 1・2 で 4 本のベンチがそろって高い。
- **仮説**: 開始時の load の閾値 1.5 では、直前の作業の残りが cycles に出る。1 回の区間が約 0.85 s と短く、周波数や他プロセスの影響を受けやすい。
- **修正案**: 開始の閾値を下げる (load < 1.0)、最初の 1 run を捨てる、run を増やして min も併記する。
- **受入条件**: Pointer の 5 run の cycles の幅 ≤ 2%。
- ゲートへの影響は無い。run ごとの比は全 run で 1.10 を下回る。

### 残件 3: 同一性の試験は、合成 import の specifier の答えに鈍い

- **問題**: `fixtureIdentity` は記録 (`compareRecords`) だけで比べる。2b-3 で `comparePrograms` に足した `compareModes` (返された合成 specifier のノードの照合) を持たない。
- **証拠**: 故障 3 (合成 import を nil にする) は、合成 import を問う 118 構成のうち 62 構成でしか検出されない (§3.2)。
- **再現**: `overlay/gen_mut.py` の `mut3` で `TestFixtureIdentityConfigurations` を sample に回す。
- **修正案**: `fixtureIdentity` に、記録の run と再生の run で返された合成 specifier を比べる段を足す。比べるのは、text・親の kind・`File()`・同じ file と役割で同じノードを返すか。2b-3 の `compareModes` と同じ観点。
- **受入条件**: `mut3` が、問うた 118 構成のすべてで検出される。
- 7d-2c の測定への影響は無い。compiler プロジェクトには合成 import が無い (fixture の `HelperImport`・`JSXImport` は 0 件)。

### 残件 4: fixture の byte が、作り直しで変わることがある

- **問題**: fixture の sha が記録の run の順に依存する。`Modules` の表の番号を、初めて問われた順に振るため。
- **証拠**: §3.5。今回の 2 回は一致したが、accessor tax 報告では同じ production の source から作り直して sha が変わった。
- **影響**: 意味は同じなので、測定には影響しない。sha を fixture の同一性の印に使えない (今回の §3.5 の照合も、意味の比較で代えた)。
- **修正案**: 書き出しの前に `Modules` を値の順に並べ替え、`Resolved`・`FromSpecifier` の添字を付け替える。
- **受入条件**: 同じ source から 5 回作り直して byte まで一致する。

### 残件 5: `symbolNodeLinks` の page の密度 (報告のみ、判断材料)

- §8 のとおり、Store の page は Pointer の 3 倍で、密度は 0.33。check の後に約 5 MB 多く保持する。
- 鍵を NodeRef (全ノードの通し番号) にしたことの帰結。7d-2c の retained の線 (0.833) の余裕 0.017 は約 3.8 MB にあたり、この差より小さい。
- 7e で end-to-end の retained を取るときの候補とする。Store ごとに links を持つノードだけを密に番号付けするなどが考えられるが、本段では提案しない。

### 既知の扱い (再掲)

- 印字の退行 (§7) は 7e の前に直す (2026-10-03、ユーザー)。
- 測定用 adapter の費用 (§5.3) は直さず、7e の CLI 結線で消す (2026-10-03、ユーザー)。

## 付録: コマンドと成果物

| 段 | スクリプト / 試験 | 出力 | 終了コード |
| --- | --- | --- | --- |
| 開始時の記録 | `hash-source.sh` ほか | `source-hash-start.txt`・`head.txt`・`env-start.txt`・`diff-*.txt` | 0 |
| fixture | `storeharness.test -test.run TestWriteCompilerProjectFixture` (2 回) | `fixture-write.txt`・`fixture-write-2.txt` | 0 / 0 |
| KPC の binary | `build-kpc.sh` | `build-kpc.txt` (sha256 と build 設定) | 0 |
| batch 1 (count・`node` の panic・`-m=2`) | `batch1.sh` | `batch1.log`・`count-compiler.txt`・`nodepanic-*.txt`・`m2-*.txt` | count は型の監査の許可リスト前で 1 (`audit-types-final.txt` で許可リスト後に 0)、ほか 0 |
| batch 2 / 2b (同一性・sample の照合・感度) | `batch2.sh`・`batch2b.sh` | `batch2.log`・`batch2b.log`・`identity-*.txt`・`count-sample.txt`・`sens/` | 同一性・照合は 0、感度の試験は想定どおり 1 (mut1 の同一性と retained は 0) |
| batch 3 (objdump・hash・links・fixture・再生 ns) | `batch3.sh` | `batch3.log`・`calls-*.txt`・`hash-*.txt`・`links-diff.txt`・`replay-ns.txt` | 0 (hash を壊した 2 回は想定どおり 1) |
| batch 4 (manifest・出力一致・印字の比較器) | `batch4.sh` | `batch4.log`・`print-manifest.txt`・`print-outputs.txt`・`sens/cmpprint-outputs.txt` | 0 (比較器の検査は想定どおり 1) |
| batch 5 (印字 ns・retained・profile の監査) | `batch5.sh` | `batch5.log`・`print-bench.txt`・`print-benchstat.txt`・`retained-check/`・`audit-profiles.txt` | 0 |
| KPC 1 | `kpc.sh` (ユーザー、sudo) | `kpc.txt`・`kpc-summary.txt` (`kpc-sum.py`) | 全 29 回 0 |
| KPC 2 | `kpc2.sh` (ユーザー、sudo) | `kpc2.txt`・`kpc2-*-summary.txt` (`kpc2-*.py`) | 全 26 回 0 |
| 回帰 | `regress.sh` | `regress.log`・`regress-*.txt` | package の 2 step だけ 1 (既知の a708 のみ)、ほか 0 |
| 終了時 | `hash-source.sh` | `source-hash-end.txt` | 0 (開始時と一致) |

- 重い仕事の順序は `busy.sh`・`queue.sh`・`queue2.sh` で決め、KPC の間は止めた。
- 比較器の検査が長いので、batch 2 を途中で区切って KPC 1 に枠を譲り、残りを batch2b にした。
