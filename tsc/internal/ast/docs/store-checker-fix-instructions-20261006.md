# Store checker 修正段 指示書（L01 heap 0.80 と Check の IsNil）

2026-10-06。対象は実装を担当するエージェント（新しいセッション）。根拠は [性能分析の報告](store-checker-perf-analysis-report-20261005.md)（以下「分析」）で、数値は特に断らない限りそこからとる。証拠は `_store-checker-perf-20261005/`。

7e は「機能完了、性能基準未達」で、L01 の checked HeapAlloc の比は 0.8426（線 0.80、割り付ける量 202 MB）だった。分析で、差は AST の削減の頭打ちではなく、Store の表現が checker・links・binder に持ち込んだ余分（Symbol の +16 B、疎な node links の page、file ごとの arena の末尾）だと分かった。本段はそれを戻して heap の gate を通し、あわせて Check の退行の最大の項目（`IsNil`）を直す。

| 記号 | 内容 | 根拠（分析） | 見積り |
| --- | --- | --- | --- |
| M1 | binder の arena を file の間で使い回す | §6.4 arena の末尾 | L01 heap −72 MB |
| M2 | `store.Symbol` の `id` と `gid` を 1 語にする（112 → 104 B） | §6.4 Symbol の field | L01 heap −57 MB |
| M3 | `symbolNodeLinks` を、引かれる kind の node だけの密な表にする | §6.4 node links、本書 §3.1 の追加計測 | L01 heap −64 MB |
| M4 | `Symbol.ValueDeclaration` を 8 B にする（104 → 96 B）。**条件付き**（§10 の 1） | §6.4 Symbol の field | L01 heap −57 MB |
| C1 | `IsNil` を 1 比較にする（nil の Node を 1 通りにする） | §7・§8・§9.2 | C01 Check −132〜−170 M inst（見積り） |

M1〜M3 の合計は −193 MB で、比は約 0.802（3,998.5 − 193 = 3,805.5 MB、Pointer 4,745.3 MB）。線の外側に 9 MB 残る見積りなので、M4 を条件付きで用意する。M1〜M4 で −250 MB、比は約 0.790。どれも scan される bytes なので、GC の CPU も減る向き。

## 0. 規則

- **変更してよいもの**：
  - `internal/ast/store/` の手書きの file（`store.go`・`symbol.go`・`bound.go`・`*_manual.go`）
  - `internal/storebinder/binder.go`（手書き）
  - `internal/storechecker/*_manual.go`（P2）
  - M4 を行う場合だけ、`tools/cmd/storetranslate` の規則
  - test・benchmark・docs
- **変更しないもの**：
  - 生成物（`// Code generated` の file）を手で直さない。変えたいときは P2 か生成器の規則で直し、`storetranslate` で作り直す。
  - Pointer の package（`ast`・`checker`・`binder`・`core`・`compiler`）。Pointer にも同じ無駄（binder の arena の末尾 53 MB）があるが、本段では直さない（gate は Pointer に対する比なので、Pointer を変えると基準が動く）。
  - go.mod / go.sum。
- **commit**：M1・M2・M3・（M4）・C1 を別々の commit にする。順番は §1 → §2 → §3 → §4 → §5。各 commit の後に §6 の計測を取る。
- **環境**（分析 §0.1 で起きたこと）：
  - 測定の queue は `nohup caffeinate -i bash …` で起動する。battery では idle sleep が 1 分で、run が途中で凍結する。時間を測る前に `pmset -g batt` を見て、battery なら AC 電源と lid を開けたままにすることをユーザーに頼む。
  - 外れ値が出たら、先に `pmset -g log | grep -E " (Sleep|Wake|DarkWake) "` と load（Spotlight の `mdworker_shared`・`mds`）を見る。
  - 大きな一時物（trace の text、archive、`-m=2` の出力、一時 binary）は scratchpad に置く。repo 内や数 GB の text は Spotlight の索引で load を上げる。
  - KPC と xctrace を同時に動かさない。測定中は自分の build や test を走らせない。
- **sudo**：KPC は root が要る。スクリプトを用意してユーザーに別ターミナルで `sudo` で実行してもらい、結果の file を読む。binary を作り直した後の最初の xctrace は、許可ダイアログの承認をユーザーに頼む。
- **生出力の置き場**：`internal/ast/docs/_store-checker-fix-20261006/`（commit しない）。段ごとに `M1/`〜`C1/` と `before/`。
- **記録**：仮説と確定を分ける。見積りには「見積り」と書く。開始時に HEAD と未コミットの差分の hash、終わりに source hash（分析の `meta/hash-tree.sh` と同じ方法）を記録する。

## 1. M1：binder の arena を file の間で使い回す

### 1.1 なぜ余るか（確定）

- `storebinder.Binder` は `symbolArena core.Arena[store.Symbol]` と `singleDeclarationsArena core.Arena[store.Node]` を持つ（`binder.go:80-81`）。Binder は `sync.Pool` で使い回されるが、`putBinder`（`binder.go:117`）が `*b = Binder{bindFunc, scratch}` で arena を 0 に戻す。
- `core.Arena` は chunk を 1, 2, 4, …, 256 要素と伸ばし、今の chunk だけを持つ（`core/arena.go`）。file ごとに arena が新しくなるので、各 file の最後の chunk の空きが残り、chunk の中の symbol が生きている限り解放されない。
- L01（分析 `s3b/count-l01-store.json`）：binder の Symbol の arena は cap 2,556,427 要素・使用 1,998,660 で、空き 557,767 要素 = 62.5 MB。単一宣言の arena の空き 581,234 要素 = 9.3 MB。

### 1.2 変更

- `putBinder` で、`scratch` と同じく 2 つの arena を残す。次の file の bind は前の file の chunk の続きから詰める。
- 並行：pool の Binder は一度に 1 つの goroutine しか使わないので、arena は共有されない。並列の bind では worker ごとに arena が分かれ、空きは worker ごとに 1 chunk まで。
- 寿命：1 つの chunk に複数の file の symbol が入る。file を捨てる program（LS の増分）では、捨てた file の symbol が同じ chunk の生きている symbol に保持される。今の storebinder の利用者は Store の CLI（1 プロセス 1 program）だけなので、この影響は無いことを comment と報告に書く。
- pool が GC で空になると（victim cache を 2 回の GC で抜ける）、新しい Binder は新しい arena から始まる。空きは 1 chunk 分（最大 255 要素）増えるだけ。

### 1.3 受入

| 項目 | 条件 |
| --- | --- |
| 等価 | §6.3 が全部通る |
| arena の末尾 | 分析の overlay（`_store-checker-perf-20261005/s3b/overlay/`、`TestPerfCount`）で、L01 の binder の Symbol の空きが 1,000 要素未満 |
| heap | §6.1 で Store の L01 checked HeapAlloc が 55 MB 以上減る |
| Bind | §6.2 の Bind KPC が線内（Store / Pointer の cycles 比 ≤ 1.10） |

## 2. M2：`id` と `gid` を 1 語にする

### 2.1 なぜ 16 B になるか（確定）

- `store.Symbol`（`symbol.go:25`）は `id SymbolId`（4 B + 詰め物 4 B）と `gid atomic.Uint64`（8 B）を持つ。Pointer の `ast.Symbol` は `id atomic.Uint64` 1 つ。
- `id` は binder が `Bound.AddSymbol`（`bound.go:161`）で振る file 内の番号で、読むのは binder の `SetSymbolId`・`SetLocalSymbolId`（`binder.go:450`・`2630`）だけ。checker の Symbol（L01 で 509 万個）では常に 0。
- `gid` は `GetSymbolId`（`utilities_manual.go:85`）が初回に振る全体の連番。binder の Symbol では 26% が 0。

### 2.2 変更

- 2 つを 1 つの `atomic.Uint64` にまとめる。推奨の割り当て：
  - 下位 40 bit：`gid`（全体の連番、初回に CAS で振る）。上限は約 1.1 × 10¹²。`newSymbolId` の「巻き戻らない」契約は、40 bit の上限で panic する形で保つ。
  - 上位 24 bit：file 内の `SymbolId`（`AddSymbol` が振る）。上限 16,777,215 を超えたら、どの build でも panic する。
  - `AddSymbol` は symbol が他から見える前に上位を書く。`GetSymbolId` の CAS は上位を保つ。
- `Id()` と `GetSymbolId` の API は変えない。`unsafe.Sizeof(store.Symbol{})` が 104 であることを test に書く。
- **inline**：`GetSymbolId` は 7d-2c B で inline の予算（80）に戻した（cost 79）。mask で予算を超える場合は、遅い経路を `newGetSymbolId` に寄せるなどの形で予算内に戻す。`-m=2` は `./internal/ast/store` だけなら 8 GB 機でも終わる。呼び出し元での inline は objdump で確かめる（7d-2c abc の `calls-B.txt` と同じ方法）。
- 別の割り当て（binder が file 内の番号を別の表に持つ、など）でもよい。条件は Symbol が 104 B、`GetSymbolId` が密で安定、Bind が線内。

### 2.3 受入

| 項目 | 条件 |
| --- | --- |
| 等価 | §6.3 |
| 大きさ | `unsafe.Sizeof(store.Symbol{}) == 104` の test |
| heap | §6.1 で 45 MB 以上減る（checker 509 万 + binder（M1 の後）約 200 万 × 8 B の見積り 57 MB の 8 割） |
| Check | §6.2 の Check KPC の cycles が M1 の後より 1% 以上は増えない（`GetSymbolId` の mask の分） |
| Bind | Bind KPC が線内 |

## 3. M3：`symbolNodeLinks` を密な表にする

### 3.1 なぜ疎か（確定。分析 §6.4 と、本書のための追加計測）

- Store の `nodeLinkStore`（`storechecker/links_manual.go`）は StoreID ごとに NodeRef で page（256 slot × 8 B）を引く。L01 で 59,607 page・122 MB、symbol が入った slot は 3,430,463（密度 0.225）。Pointer は `GetNodeId` で引かれた node にだけ番号を振るので 15,160 page・31 MB（0.884）。
- page を小さくしても密にならない（`_store-checker-perf-20261005/s3b/pagesizes-l01-store.json`）：

| page の slot 数 | page 数 | bytes | 密度 |
| ---: | ---: | ---: | ---: |
| 8 | 1,443,376 | 92.4 MB | 0.297 |
| 32 | 416,596 | 106.6 MB | 0.257 |
| 256（今） | 59,462 | 121.8 MB | 0.225 |

  symbol の入る node が NodeRef の上で一様に散らばっているため。
- symbol が入る node の kind（L01）：

| kind | symbol が入った数 | 割合 | その kind の node 数 | その kind のうちの割合 |
| --- | ---: | ---: | ---: | ---: |
| Identifier | 1,795,801 | 52.3% | 5,105,405 | 0.35 |
| PropertyAccessExpression | 1,322,392 | 38.5% | 1,329,298 | 0.99 |
| TypeReference | 305,513 | 8.9% | 383,854 | 0.80 |
| その他（PropertyAssignment ほか） | 約 6,800 | 0.2% | | |

### 3.2 変更（推奨の形）

- 表の slot を、Identifier・PropertyAccessExpression・TypeReference の node にだけ持たせる。L01 で 6,818,557 slot × 8 B = 54.5 MB。
- 添字は、Store ごとの「この 3 kind か」の bitmap（1 node 1 bit）と 64 bit ごとの累積数（uint32）による rank：`dir[ref>>6] + popcount(bits[ref>>6] & (1<<(ref&63) - 1))`。L01 で bitmap 約 2 MB、累積数 約 1 MB。bitmap は、その Store で最初に `Get` されたときに kind の列から作る（source Store は bind の後は node が増えない）。
- 3 kind 以外の node と、check の途中で node が増える Store（Factory の合成 Store）は、`core.LinkStore[store.NodeKey, SymbolNodeLinks]`（map）に回す。L01 で約 6,800 件。lazy JSDoc の Store は、作った後に node が増えないことを確かめてから表に入れる。確かめられなければ map に回す。
- `Get`・`Has`・`TryGet` の意味（同じ鍵なら同じ `*V`、`TryGet` は未確保なら nil）を変えない。nil の Node は今と同じく 1 つの鍵。
- 試して捨てた形（報告に書く必要はない）：page を小さくする（上の表）、引かれた node に遅延で番号を振る（番号の置き場に 1 node 4 B の列が要り、L01 で 27 MB + 密な page 31 MB で得にならない）、hash map（7d-2c の pageMap の費用）。
- 形を変えてよい範囲：上の意味を保ち、L01 の表の bytes が 60 MB 以下で、`Get` の単価（§3.3）が条件内なら自由。

### 3.3 受入

| 項目 | 条件 |
| --- | --- |
| 等価 | §6.3 |
| 表の大きさ | overlay の計数で、L01 の symbolNodeLinks の表（slot・bitmap・累積数・map）の合計が 60 MB 以下 |
| heap | §6.1 で 55 MB 以上減る |
| `Get` の単価 | 分析の再生（`internal/storeclitest/replay_test.go` の `links`、rusage の counter）で、Store の inst/op が今の 53.25 から +10 以内。再生の `replayStoreLinkStore` は新しい形の複製に差し替える |
| Check | Check KPC の cycles が M2 の後より 1% 以上は増えない |

## 4. M4：`ValueDeclaration` を 8 B にする（条件付き）

**§10 の 1 でユーザーが入れると決めた場合だけ行う。** 推奨は「M1〜M3 の後の L01 の heap 比が 0.795 を超えたら行う」。

- 今：`ValueDeclaration store.Node`（16 B、2 word）。非 nil は checker の Symbol で 89.0%、binder で 82.8%。
- 形：`store.NodeKey`（StoreID と NodeRef、8 B）を持ち、program の Store の表（StoreID → `*Store`、`IDSpace` が振る）で Node に戻す。field を method（`ValueDeclaration()`・`SetValueDeclaration(n)`）にし、`storetranslate` に Pointer の field の読み書きを method に写す規則を足す。storechecker の参照は 237 か所（大半が生成物）。
- 受入：等価、`unsafe.Sizeof(store.Symbol{}) == 96`、heap で 45 MB 以上減る、Check KPC の cycles が 1% 以上は増えない（読むたびに表を引く分）、生成器を 2 回走らせて差分が出ない。

## 5. C1：`IsNil` を 1 比較にする

### 5.1 なぜ高いか（確定）

- nil の Node は 2 通りある：zero の `Node{}`（`h == nil`）と、各 Store の番兵の行 `nodes[0]`（kind Unknown）。`(*Store).node(0)` は番兵を返すので、`IsNil` は `n.h == nil || n.h.kind == KindUnknown` と 2 段で判定する（`store.go:162`）。
- 機械語では Store が `CBZ`・`MOVH`・`UBFX`・`CBZW` の 4 命令、Pointer は `CMP` 1 命令（分析 §7.2）。C01 の Check で 69,486,594 回呼ばれ、再生の単価の差は +4.1 inst/op。予測では inst の差の 31〜43% を占める（分析 §8・§9.2）。

### 5.2 変更

- `node(e)` が `e == 0` のとき `Node{}` を返すようにし、nil を zero の Node だけにする。`IsNil` は `n.h == nil`、`SameNode` は `a.h == b.h` にできる。番兵の行は配列の 0 番として残すが、外へは出さない。
- `Node{s, &s.nodes[0]}` を作る経路が `node` のほかに無いことを grep で確かめる。
- nil の Node の `Kind`・`Parent` などの読みは、今の契約（`store.go:88` の comment）でも範囲外だが、番兵が Unknown を返すことに頼っている手書きの code があると panic に変わる。§6.3 の等価と `storechecks` の build で見つける。見つかったら、Pointer の同じ箇所（nil を先に比べる形）に合わせて直す。
- **inline**：`node` に分岐が増える。7d-2c B で `FindAncestor`（cost 80）・`FindAncestorOrQuit`（79）ほか 8 関数を予算内に戻した。`IsNil` が安くなる分と相殺されるかを `-m=2`（`./internal/ast/store`）で確かめ、外れたら分岐の無い形（`CSEL` になる書き方）などで戻す。呼び出し元での inline は objdump で確かめる。
- 見積り：`IsNil` −3 × 69.5 M = −208 M inst、`node` +1〜2 × 38.0 M = +38〜+76 M inst、差し引き −132〜−170 M inst。cycles は inst から換算しない。

### 5.3 受入

| 項目 | 条件 |
| --- | --- |
| 等価 | §6.3。`storechecks` の build でも通る |
| inline | `-m=2` で 7d-2c B の 8 関数と `Parent`・`IsNil`・`node` が inline 可。storechecker の objdump で `FindAncestor` などへの `CALL` が 0 |
| Check | Check KPC の inst が M3（または M4）の後より 100 M 以上減る。cycles は報告する（線は置かない） |
| Walk / Bind / Parse | §6.2 の KPC が線内（Walk ≤ 1.15、Bind ≤ 1.10、Parse は before から悪化しない） |

## 6. 計測と試験

### 6.1 heap（gate）

- `TestNativeRetained`（`internal/storeclitest/retained_test.go`、7e §9.4）を L01 の checked 地点で回す。7e の `_store-cli-impl-20261004/e/retained-pairs.sh` を元にする。
- 回数（§10 の 3 の推奨）：before と各段の後で Store を 3 run、Pointer は開始時に 3 run（Pointer の code は変えないので 1 回でよい。7e の 4,745.85 MB と 0.1% 以内であることを確かめる）。比は Store の中央値 / Pointer の中央値。HeapAlloc は決定的（7e の A/A [1.0000, 1.0001]、分析の再測定も一致）なので、3 run の幅が 0.1% を超えたら原因を調べる。
- あわせて、同じ run の強制 GC の mark CPU と通常 GC の回数・scan bytes を記録する（線は置かない。§7 の 3）。
- 各段の後に、分析の overlay（`s3b/overlay/`）で arena の末尾・Symbol の大きさ・node links の表を L01 で数え、見積りと並べる。
- **gate**：最後の段の後で、L01 の checked HeapAlloc の比 ≤ **0.80**。

### 6.2 KPC

- **Check**：`BenchmarkNativeCheckKPC/C01`（`internal/storeclitest/bench_test.go`）。binary は `CGO_ENABLED=1 CGO_LDFLAGS="-framework CoreFoundation -framework CoreServices" go test -tags kperf -c`。before と各段の後の binary を 1 回の sudo の session で交互に、各 5 run（各 run は backend ごとに別プロセス、`-benchtime=1x`）。比は同じ binary の Pointer で割る。
- 入力の path：`benchInputs` は既定で compile 時の source の path から C01 の config を決める（`repo.TestDataPath()`）。段ごとの source を別の場所（worktree、`git archive`）から build すると、入力の path の長さが binary ごとに変わり、Pointer の inst が動く（7d-2c 検証で archive build の Pointer が +0.9%）。全 binary に `STORE_CLI_BENCH_INPUTS="C01=<repo>/tsc/testdata/fixtures/compiler/tsconfig.store-cli-c01.json"` を渡して、同じ path の入力を読ませる。それでも Pointer の inst の幅が binary の間で 0.5% を超えたら、原因を調べてから比を読む。
- **Walk / Bind / Parse**：7d-2c abc の `kpc.sh` と同じ方法（`BenchmarkASTWalkKPCV1`・`BenchmarkASTBindKPCV1`・`BenchmarkASTParseKPCV1` と Store 側、`-benchtime 20x -count 5` × 3 run、比は同じ binary の `/store` と `/pointer`）。M1・M2（binder と Symbol）と C1（`node`）の後に取る。
- load は 1.5 未満を待つ。Spotlight が動いていたら止むまで待つ。

### 6.3 等価（各 commit の前）

7d-2c abc の `regress.sh`（`_store-checker-7d2c-abc/regress.sh`）を元にし、native の corpus を足す。

| 試験 | 範囲 |
| --- | --- |
| build・vet | `go build ./...`、`go vet` を tag なしと `storechecks` で |
| 生成器 | `node tools/scripts/tsc/generate.ts` と `cd tools && go run ./cmd/storetranslate` を走らせて差分が出ない |
| 段 1 strict | `TestCheckerCorpus` |
| compiler プロジェクト | `TestCompilerProjectEquivalence` |
| mode | `TestModeCorpus` |
| package | `go test ./internal/ast/store/... ./internal/store...`（tag なしと `storechecks`） |
| native の全母集団 | `TestNativeCheckerConfigurations`（`internal/storeclitest`）で 13,170 equal・不一致 0・panic 0。**最後の段の後に 1 回** |
| 段 2 | `TestCheckerConfigurations`。最後の段の後に 1 回 |

### 6.4 時間（報告のみ、線は置かない）

- L01 と C01 で `TestNativeStages`（`internal/storeclitest/stages_test.go`、分析 §3）を最後の段の後に回し、区間ごとの wall・utime・`proc_pid_rusage` の cycles と instructions、GC の回数と CPU を before と並べる。C01 は GOGC=off と 100 の両方、L01 は GOGC=100 で backend ごとに 3 run。
- L01 の wall は heap の削減で paging が減ると動く。§7 の 1 の判断の材料にする。

## 7. gate の取り方（分析の指示書 §9 から引き継いだもの）

本段の合否は heap（§6.1）だけで決める。時間の gate は §6.4 の結果を見て次の段で決める。次の 3 つは本書で方針を決める（§10 の 4〜6）。

1. **時間の gate**：7e の L01 wall ≤ 0.70 は、分析 §9.3 で AST に関わる部分（Store の Check の cycles の 10.7%）を全部消しても届かないと分かった。推奨は、本段では時間に線を置かず、heap を直した後の L01 の wall と cycles（§6.4）を見て、線と指標を決め直すこと。
2. **L01 の cycles の取り方**：推奨は `TestNativeStages` の `proc_pid_rusage` の cycles（user の cycles、全 thread、GC あり、root 不要）。C01 の Check の GOGC=off で KPC と 0.4% 以内だった（分析 §3.1）。KPC と違い GC の worker の cycles も入るので、「GC を含む CPU の cycles」として扱う。
3. **mark CPU の A/A**：7e の不安定さは runtime の推定値（wall 由来で paging の待ちを含む）による。推奨は、`TestNativeRetained` の 2 回目の強制 GC の前後で `proc_pid_rusage` の cycles と instructions も読み、user の cycles で A/A を取り直すこと（test 側の変更）。本段では記録だけにする。

## 8. 線を外れたとき

- **M1〜M3 の後に heap の比が 0.80 を超えた**：§10 の 1 で M4 が承認されていれば M4 に進む。承認が無ければ、ユーザーに報告して判断を仰ぐ。追加の候補は、checker の Symbol で常に 0 の `Members`・`Exports`・`ExportSymbol`（24 B）を別の構造に出す案（分析 §6.4。Pointer より小さくする最適化なので §10 の 2）。
- **段の見積りと実測が 30% 以上違う**：overlay の計数で、どの確保が残ったかを書く。
- **Check の cycles が段の条件を超えて増えた**：その段の objdump で増えた命令を数え、直す形を報告する。直せなければ、その段を外す案と一緒にユーザーに報告する。
- **等価が落ちた**：その段の commit を作らない。原因と、Pointer の同じ箇所の扱いを報告する。

## 9. 報告

- 置き場所：`store-checker-fix-report-20261006.md`。
- 書くこと：各段の変更（file、P2 の hash の行）、§6 の値と受入の結果、見積りとの照合、§7 の方針に沿って記録した値、指示書の外で判断したこと、仮説と確定の区別。

## 10. 決めること（着手前にユーザーへ確認）

**決定（2026-10-06、ユーザー）**：1〜7 は全部推奨どおり。1 は条件付き（M1〜M3 の後の L01 heap 比が 0.795 を超えたら M4）、2 は入れない、3 は Store 3 run・Pointer 3 run の中央値の比、4〜6 は推奨の方針、7 は分析の成果と本書を before として commit する。あわせて §6.3 の回帰の範囲を、以前の決定（commit ごとは触った範囲）に合わせた：commit ごとに build・vet（両 tag）、生成器、compiler・mode の等価、触った package を両 tag。段 1・段 2 の等価、`./internal/ast/store/... ./internal/store...` の全部（両 tag）、native の全母集団は最後の段の後に 1 回。

1. **M4 を本段に入れるか**。推奨は条件付き：M1〜M3 の後の heap の比が 0.795 を超えたら入れる。M1〜M3 の見積りは 0.802 で、線の外に 9 MB 残る。M4 は `storetranslate` の規則を足す大きめの変更なので、必要になってから行う。
2. **Pointer より小さくする最適化（`Members`・`Exports`・`ExportSymbol` を出す、約 −113 MB）を本段に入れるか**。推奨は入れない。本段の目的は Store の表現が持ち込んだ余分を戻すことで、これは両 backend に効く設計の変更なので、別の段で Pointer 側と合わせて決める。
3. **heap の測り方**：7e §9.5（A/A 10 組・A/B 30 組）ではなく、Store 3 run・Pointer 3 run の中央値の比にするか。推奨は 3 run。HeapAlloc は決定的で、7e の A/A は [1.0000, 1.0001] だった。最後の段の後だけは 7e §9.5 の組で取り直す案もある。
4. **時間の gate**（§7 の 1）：本段では線を置かず、heap の後に決め直す。推奨のとおりでよいか。
5. **L01 の cycles**（§7 の 2）：`proc_pid_rusage` の cycles を使う。推奨のとおりでよいか。
6. **mark CPU**（§7 の 3）：user の cycles を test 側で記録し、本段では線を置かない。推奨のとおりでよいか。
7. **before**：分析で足した test 5 本・`tools/cmd/perfclass`・分析の指示書と報告（未コミット）を先に commit し、その hash を before にするか。推奨は commit すること。本段の計測は `stages_test.go`・`replay_test.go`・overlay を使うので、before に含めておくと各段の source と binary の対応が追いやすい。
