# Store checker 基盤 (TODO 7d-0) 検証報告

作成日: 2026-09-27。検証指示は [store-checker-7d0-verification-instructions-20260924.md](store-checker-7d0-verification-instructions-20260924.md) (以下「検証指示」)、実装報告は [store-checker-7d0-implementation-report-20260927.md](store-checker-7d0-implementation-report-20260927.md) (以下「実装報告」)、設計は [store-checker-design-20260923.md](store-checker-design-20260923.md) (以下「7d 設計」)。生の出力は `_store-checker-7d0-results/` (ignore 対象、ローカルのみ)。検証者が作った file は `verify-*`、`retained/`、`retained2/`、`retained-compiler/`、`inline-*`、`asm-*`、`utilities-*`、`kpc-verify.sh`、`ns.txt`。同じディレクトリの `kpc*-20260927-*.txt` は実装時にユーザーが取った KPC、`verification-20260927/` は本検証と同時刻に走っていた別の検証プロセスの出力で、本報告は後者に依存しない。

## 1. 結論

> **2026-09-28 追記**: 同時刻の別検証 (`verification-20260927/correctness-code-audit.md`) が production の source identity の欠落 (P1、指示書 §3.6 の未実装) を見つけ、§6 後半の「7d-0 未合格」はそれによる。P1 とテスト網羅の欠けは修正済み、KPC は修正後の source で取り直しが要る。§12。

**正しさは合格、性能は 3 本の KPC と retained / GC が全部線の内側で合格。** 実装は直していない。変換器の自動化率だけが線 (案) に届かず、実装報告 §6.3 のとおりユーザーの判断待ち (§4)。

| 指標 | 線 | 結果 | 7c |
| --- | --- | --- | --- |
| parse / bind の等価 | mismatch 0、除外は 7c と同じ | bind 17,085 file mismatch 0、除外 330 file (理由ごとの file 番号が 7c と一致)、宣言の Store 検査 708,382 件 (7c と同数)。parse 17,415 file 一致 | 同じ |
| storage の操作列 | 17 項目、容量 4 通り | 17 項目全部あり、容量に依存する 11 項目 + flow graph の後半 + writer が 1 / 2 / 3 / 4096 で回る。読んで確かめた欠けは §3.2 の 3 件 (軽微) | — |
| Walk cycles/visit S/P | ≤ 1.15 | **1.054** (after2、ユーザー計測の中央値を再計算。Pointer 側の中央値を他 run の値で置くと 1.080) | 1.137 |
| Parse cycles/op S/P (checker.ts) | ≤ 1.00 | **0.824** (dom 0.865、7d-1 の before) | 0.824 |
| Bind cycles/node S/P | ≤ 1.10 | **1.020** (before 0.992 / 0.984) | 0.984 |
| retained B/node S/P | checker.ts ≤ 0.60、fixtures ≤ 0.70 | **0.565 / 0.601** (dom 0.739)。run 間 0.1% 以内 | 0.561 / 0.583 |
| GC mark CPU S/P | checker.ts ≤ 0.25、fixtures ≤ 0.35 | **§7 の retained2 の値** (低負荷で再計測) | 0.10 / 0.16 |
| compiler プロジェクト parse+bind retained S/P (7d-2 の線の前提) | 0.70 以下なら 7d-2 の線はそのまま | **§7** | — |
| 変換器 binder.go 試作 | (a)〜(d) 相当の 9 割 (案) | 全体 34% (22/64)。(a) 85%、(c) 58%、(b)(d) 0%。線の読み方の判断をユーザーに求める (§4.2) | — |

KPC (§6) は実装時にユーザーが取った raw (`kpc2-*-20260927-1715.txt`) を検証者が読み直したもの。その after2 の binary は、検証者が現在の source から `-tags kperf` で作り直した binary と **byte 一致** (`cmp` で 3 本とも同一) なので、現在の source の値として扱う。検証者自身の before → after → before の再計測は root が要るので `kpc-verify.sh` を置いた (§11)。

## 2. 環境と入力

- `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、HEAD `d8f8adf5d6` + 未コミットの 7d-0 (git status 37 項目、`verify-git-status-start.txt`)。before は同じ HEAD を `git worktree add --detach` した別ディレクトリ。
- go1.27.0 darwin/arm64、macOS 26.6.2、Apple M1 8 GB。
- source の sha256 (51 file、`verify-source-sha256-start.txt`) は開始時と終了時で一致。途中で 2 回だけ意図して触った: `utilities_manual.go` の hash の書き換え (§4.3、直後に復元して sha 一致を確認) と、storebinder に置いた一時ベンチ file (§7、実行後に削除)。
- load average: 開始 2.5、テストと inline 2.0〜3.6、**retained の 1 回目は 11〜18** (別の検証プロセスがベンチを回していた)、retained2 / compiler / ns は §7 に記す。
- 入力: 7b / 7c と同じ corpus 17,415 file、checker.ts / dom.generated.d.ts / fixtures (166 file)、compiler プロジェクト (`testdata/fixtures/compiler`、124 file)。

### artifact の状態

| artifact set | 状態 | 根拠 |
| --- | --- | --- |
| Vのテスト・通常ns・隔離retained/GC・inline | current | selected repo/revision、source hash、実行条件を記録 |
| 既存kpc/kpc2、旧impl retained | stale | 測定時のsource/binaryとの照合不足。旧afterは生成器変更前 |
| 既存Parse beforeのdom | unsupported | 該当Benchmark行が無い |
| 現行/変更前の新規KPC測定 | current | provenance付きbinaryをユーザーが実行、全9rawがPASS。低負荷条件は未達 |

before binaryは同じrevisionのgit archiveを `/private/tmp/7d0-head-correctness-baseline` に展開して新規構築した。selected repoの未コミット実装を含むafterとは意図して異なる。baselineとbinaryのhashはVのbefore provenanceに保存している。

## 3. 正しさ (検証指示 §1)

### 3.1 ガードと再現

| 検査 | 結果 |
| --- | --- |
| `node tools/scripts/tsc/generate.ts` + `(cd tools && go run ./cmd/storetranslate)` | 両方 exit 0、`git status` と source の sha256 が実行前後で不変 (`verify-generate.txt`、`verify-storetranslate.txt`: 166 関数 + P2 10 + 根 171) |
| `go build ./...`、`go vet` (tsc の 3 package、tools の変換器) | 全部通る (`verify-build-vet.txt`) |
| `git diff --stat HEAD -- internal/ast internal/binder internal/parser internal/checker internal/scanner go.mod` (store と docs を除く) | 空 (`verify-guard-diffstat.txt`) |
| production から store 系 package の import | 無し。唯一の hit は `docs/_store-node-zero-results-20260926/zero_contract_test.go` (ignore 対象の実験 file) |
| `go test` tag なし / `-tags storechecks` (store、storeparser、storebinder) | store と storebinder は両 tag で PASS。storeparser は `FuzzEquivalence/a708303f611b95c8` (`ExternalModuleIndicator differs`) だけ FAIL で、**HEAD の worktree でも同じ seed が同じ理由で落ちる** (検証者が再現)。7d-0 の変更とは無関係 |
| `go test ./internal/ast/ ./internal/parser/ ./internal/binder/` | PASS |
| `go test ./cmd/storetranslate/` (tools) | PASS (TestRules、TestRulesUnsupported、TestManualHash) |

### 3.2 parse / bind の等価と storage の操作列

- bind: `17415 files: 17085 compared, 330 excluded, 0 with a parse mismatch; 708382 declaration Stores checked`。除外の理由ごとの file 番号 (`binder_test.go:232` の 5 行) を 7c の `test-notag.txt` と diff して**同一** (`verify-excl-7c.txt` / `verify-excl-7d0.txt`)。JS の緩め (SkipReparsed + MaskFlags) は `bindequiv_test.go` / `parser_test.go` の diff に変更が無い。
- parse: 17,415 file、2,576,310 node、diagnostics は 17,405 file 同一 + JS 10 file の部分集合 (7c と同じ)。
- storage の操作列 (`store/storage_test.go`、実装報告 §7 の表): 17 項目を 1 つずつ読んだ。検証指示 §1 の 3 が名指しする箇所:
  - (1) source の子の `Parent()` が変わらないこと: `propName.Parent()` と `property` を `SameNode` で比べている (line 217)。項目 2 も `sourceParent` を先に取って比べる (272)。
  - (3) 容量 1 で境界をまたぐこと: `storeID(a) == storeID(parent) || storeID(a) == storeID(b)` を error にしている (312)。項目 1、4、14 も同じ assert を持つ。
  - (5) `storeChecks` の panic のテスト: `store.StoreChecks` (export_test の定数) で囲まれ、`-tags storechecks` の verbose ログで `TestStorageCloneNodeList` の 4 容量が PASS。
  - (7) 印の無い範囲外の値の panic: `TestStorageCorruptEdges` は tag で囲まれておらず、tag なしの実行で PASS。行数直後・遠く・source の Store の foreign 値・foreign 表の範囲外・foreign 表を持つ Store の小さい値の 5 通り。
  - K3 (検証指示 §1 の 4): `factory.go:302〜312` の `CloneNodeList` と `416〜427` の `SetListElement` に指示書 §3.6 の趣旨のコメントがある。テストは Clone の先、合成の元、source の list の clone の 3 つへの `SetListElement` が storeChecks で panic することを確かめる (416〜418)。
  - writer の所有者 (§1 の 5): `TestStorageWriterOwner` は 5 本の writer × {source ノード、root、別 region、seal 済み、`Node{}`、source の番兵} を tag なしで panic、同 region の合成ノードは通る。`SetLocals` / `SetListElement` は source と nil list の拒否だけを assert する (実装は同じ `checkWritableStore` を通るので欠陥ではない)。
  - 大きさ (§1 の 6): `NodeHeader` 32、`Node` 16、`Flow` 16、`FlowNode` 16、`NodeKey` 8 を assert、`Symbol` 112 (ast.Symbol 96)、`gid` は `atomic.Uint64` で 8 B。
  - flow graph (§1 の 9): `flowWalker` は `Flags` / `Node` / `Antecedent` / `Antecedents` / `SwitchClause` / `ReduceLabel` / `FlowList.Flow` / `Next` だけで辿る。`Bound` の表を読むのは `FlowCounts()` で「A にもある番号」を選ぶ箇所 (714) と export_test の `FlowOwner` (所有者の Store) だけで、比較の graph は内部表から作っていない。2 file の bind で同じ数値の `FlowRef` を選び (719)、B の flow を付けた合成ノードから辿った graph が B の Bound に解かれることを 4 容量で assert (726〜736)。
  - 取り込みの境界 (§1 の 10): 別 program の同じ数値の StoreID (assert あり、826) と別 region の合成ノードを `New` / `NewNodeList` / `SetParent` / `SetListElement` / `Update` に渡すと tag なしで panic、別 program の flow の `SetNodeFlow` も panic、同 region の別 Factory と同 program の source は通る。
  - hook の再入と再確保 (§1 の 11): 再入は容量 1 で内外の StoreID が違うことを assert (889)。`extra` は cap の変化を assert (916)。
  - private 名と gid (§1 の 12): 2 file × (C、Inner、D extends C) の `#x` / static `#y` / `#m` の 12 key が全部別、Members / Exports の raw key から宣言の symbol と `ValueDeclaration` に戻れる、2 file の class の local SymbolId が同じ (衝突の条件) ことを assert、bind の順を逆にしても同じ、`normalizeName` は使わない。gid は反復・16 goroutine の並列初回採番で同一、別 symbol は別の非零。上限は子プロセスで `MaxUint64-2` から採番し、2 回 panic して既存 id が不変。
- 読んで見つけた欠け (どれも実装の欠陥ではなく、テストの網羅の話。正しさの判定は変えない):
  1. 項目 13 (`TestStorageImportBoundary`) は容量 1 だけ。検査は `edge()` / `checkImport()` の token 比較で容量に依存しないが、指示書 §6.2 の「全部を 4 通り」からは外れる。
  2. 項目 15 は `texts` の再確保を assert していない (初期 cap 0 で 5 文字 → 20〜22 文字を書くので `strings.Builder` は必ず再確保するが、テストが証明していない)。
  3. 項目 8 の `SetLocals` / `SetListElement` は上のとおり source と nil だけ。
- `unsafe`: production は `store.go:249〜250` (`Node.Ref`) と `359` (`List.edges`) の 2 箇所だけ。`convert` / `storetest` にも無い。`Ref` / `FileRef` は store に残っていない (残る `FileReference` は `ast.FileReference`)。
- 逆参照と全件表 (§1 の 8): `Store` の field は `nodes / extra / src / texts / id / file / bound / foreignNodes / foreignLists / private / sealed / owner (*regionToken、1 byte の token) / space (*IDSpace、counter だけ)`。Factory / NodeRegion / checker への pointer も、Store の全件表も無い。package 変数は生成の shape 表と `nextSymbolId` だけ。

### 3.3 コードを読んで確かめた契約

- `node()` (`store.go:116〜124`): 速い側は `int(e) < len(s.nodes)`、遅い側は `e&foreignBit == 0` なら全 build で panic、foreign 表は bounds check つき。`list()` も同じ形。`List.At` は owner の Store で解く。`Refs()` は storeChecks で foreign を持つ Store を拒否し、使い手は storebinder (16) と storeparser (17) だけ。utility の出力は `Len` / `At`。
- `Factory`: `row()` が `len < cap` の Store を選び `newNode` が append する (cap = 1 + capacity で確保するので header は動かない)。生成の `New*` は `dst := f.row()` → `edge()` / `listEdge()` で foreign 表に足しながら payload を `extra` に書く → `newNode` で header と hook。**子の `parent` は書かない**。`Update*` は `SameNode` / `SameList` / 値で比べ、無変更なら `Node(node)` を返し New も hook も呼ばない。`Clone*` は `New*(node の子…)` + `cloneNode` (flags と range を写す、hook は OnUpdate → OnClone で Pointer の `cloneNode` と同じ順)。
- `SetParent` は child の Store の foreign 表に書く (古い Store のノードに新しい Store の親)。`SetListElement` は `edges()` を先に取ってから `edge()` を評価するので、`edge()` が foreign 表だけを伸ばし `extra` に触らない不変条件 (コメント `factory.go:296`) と合わせて slice は有効。
- 解決層 (`bound.go:252〜282`): `s.bound != nil` の分岐 1 つ、`private` が nil の Store (bind 前・遅延 JSDoc) は slot 0 しか読まないので nil を返す。storeChecks では Seal 前の読みを panic。
- `Node.File()`: `s.file` があれば返し、無ければ `Parent()` を辿る。nil は nil。
- `GetSymbolId` / `newSymbolId` / `IDSpace.Next`: CAS ループ、上限で counter を動かさず panic。

## 4. 移植の機械性と変換器 (検証指示 §2)

### 4.1 storebinder / storeparser の hunk

`git diff HEAD -- internal/storebinder internal/storeparser`: 48 hunk、+118/−100 (`binder-parser.diff`)。全 hunk を読み、分類は実装報告 §4 のとおり: §4 の 1 (`FinishFile` 2 か所)、§4 の 2 (id 層の改名 `SetFlowRef` / `SetSymbolId` / `SetLocalSymbolId` / `LocalsRef` / `Set…FlowRef`、`symbolOf`、root の `SymbolId()`)、§4 の 3 (D2: `core.Arena[store.Node]`、`append(…, node)`、`SetValueDeclaration(symbol, node)`、`isFunctionSymbol(symbol)`、`getInitializerSymbol`、pattern 名の `Key()`)、§4 の 4 (等価テストの `ref` → `declaration`、`countDeclarations`、seal テストの名前)、§4 の 5 (`NewBoundIn(scratch, b.s)`)、private 名 (`GetSymbolId` の 10 進)、Z1 (`SameNode` 5 か所と `store.AppendIfUnique`)、U2 (`IsImplicitlyExportedJSDocDeclaration(node)` / `IsModuleAugmentationExternal(node)`)、doc と dom sub-benchmark。**§4 以外の書き換えは無い。**

### 4.2 binder.go の試作

実装報告 §6.3 の数字 (`_store-checker-7d0-impl/binder-trial/binder-trial-compare.txt`) を読み直した。正規化は `compare.go` の `renameTable` 17 項目で、全部が名前の regex 置換 (`.SetFlow(` → `.SetFlowRef(` など。`Symbol()` → `SymbolId()` は `file.Symbol()` を除く文脈つき)。構造は書き換えていない。hunk の分類は実装報告が言うとおり追加行のキーワードによる自動分類で、7c 報告の数 (a 24 / b 14 / c 15 / d 8) とは一致しない。

| 分類 | hunk | 一致 | 不一致のうち設計変更 (D2 / Z1 / K3) | 不一致のうち 7c の (f) |
| --- | ---: | ---: | ---: | --- |
| (a) | 13 | 11 (85%) | 0 | 2: `declarationNameToString(b.file, …)` (f #4)、`getInitializerSymbol` の method 化 (f #11) |
| (c) | 19 | 11 (58%) | 6 | 2: `sourceFile.Statements()` / `EndOfFileToken()` (f #27)、`currentReturnTarget != 0` (flow の 0 比較) |
| (b) | 14 | 0 | 11 | 書き込み側 (`SetFlowRef`、`b.bound.*`、`GetLocals(b.bound, …)`) |
| (d) | 18 | 0 | 6 | flow の index 化 (`FlowRef`、slab) |

(a)(c) の不一致で設計変更でも (f) でもないものは無い。(b)(d) は変換器が読み側の規則しか持たないので原理的に一致しない (7d 設計 §2.1)。**線 (案) の「(a)〜(d) 相当の 9 割」は満たさない (34%)。(a)(c) だけなら 22/32 = 69%、設計変更と (f) を除けば 22/22。** 検証指示 §2 のとおり不合格ではなく「7d-2 の前に規則を足す」項目としてユーザーの判断を求める: (1) 線の分母に書き込み側の (b)(d) を入れるか、(2) 実装報告が挙げる候補 (list を slice に取る core helper の List 版、`*SourceFile` の field の File 版) を規則に足すか。訳せなかった 83 件の内訳は実装報告 §6.3 (node の field への書き込み 21、SourceFile の field / method 27、list を slice として渡す 9、`FlowNodeData` 6 など) で、`binder-trial-problems.txt` と一致する。

### 4.3 `store/utilities.go` の出し直しと P2

7c の手書き版 (HEAD) と 7d-0 の出力を関数単位で並べた (`utilities-funcdiff.txt`、検証者の `funcdiff.py`)。

- 集合: 7c 165 → 7d-0 166。7c だけ: `nilNode` (Z1 で `Node{}`)、`IsFunctionLikeKind` (kind だけの export された ast 関数は訳さず `ast.IsFunctionLikeKind` を呼ぶ規則。使い手は storeparser 1 + utilities 1)、P2 に移った 5 本 (`GetExternalModuleName`、`NodeHasName`、`IsPotentiallyExecutableNode`、`IsAsyncFunction`、`IsLocalsContainer`)。7d-0 だけ: `SymbolName` / `GetSymbolTable` / `GetMembers` / `GetExports` (symbol.go から根に入って移った)、`IsAsExpression` / `IsJSDocTypeAssertion` / `GetLeftmostAccessExpression` / `nodeContainsPosition` (Pointer の枝をそのまま訳して到達した)。
- 順序: kind の述語 49 本のブロックが 7c の手書きの位置・順から Pointer の source 順 (file 名・offset 順) に動いた。指示書 §5.1 の「元の関数の名前・順序・コメントを保つ」の「元」は Pointer なので規則どおりだが、**7c の順とは違う**。
- 本体: 共通 158 のうち 86 が同一、72 が違う。72 の差分を全部読み、内訳は (i) `==` → `SameNode` (`GetAssignmentTarget`、`IsIdentifierName`、`IsInTopLevelContext` など)、(ii) `node.nilNode()` → `Node{}`、(iii) `map[NodeRef]` + `Ref()` → `map[NodeKey]` + `Key()` (`getModuleInstanceState*`)、(iv) `Refs()` の range → `Len()` / `At()` (`getModuleInstanceStateWorker`、`…ForAliasTarget`)、(v) `(file, node)` → `(node)` と `node.Parent().File()` (`IsModuleAugmentationExternal`、`IsImplicitlyExportedJSDocDeclaration`)、(vi) `.AsIdentifier().Text()` → `.Text()` (`IsRequireCall`、`IsBindableObjectDefinePropertyCall`)、(vii) 7c が手で局所変数にまとめた読みが逐語に戻った (`GetAssignmentDeclarationKind`、`IsObjectLiteralOrClassExpressionMethodOrAccessor`、`IsRequireCall`、`GetElementOrPropertyAccessName`)、(viii) 1 行の述語の整形、(ix) 7c の (f) の解消 (`IsOuterExpression` の JSDoc 枝、`isVariableDeclarationInitializedWithRequireHelper` の panic → `GetLeftmostAccessExpression`)、(x) `IsForInOrOfStatement` に Pointer の `node != nil` が戻った。**分岐を変えるものは無い**。
- P2 (`utilities_manual.go`): 10 関数、各関数の直前に `storetranslate:pointer-hash <16 桁> <file>:<func>`。理由は実装報告 §6.2 の表と一致する (Bound が要る、JSDoc 引数、gid、slot 表、File の field、`core.FirstOrNil` / `core.Some` に list を渡す、`BodyData` が無い)。
- hash の照合: `GetLocals` の hash を 16 桁の 0 に書き換えて `go run ./cmd/storetranslate` を実行 → exit 1、`GetLocals: the Pointer function changed (hash e858c44b5f9bb737, recorded 0000000000000000)`、`utilities.go` は書き換えられず (`verify-hash-tamper.txt`)。復元後の sha256 は書き換え前と一致。`TestManualHash` も同じことを一時 file で確かめている。
- 7c の (f) / (g): 実装報告 §6.2 の 1 件ごとの扱い (規則 / P2 / 逐語で足りた) と funcdiff が合う。(g) は規則にしていない ((vii) のとおり逐語に戻っている)。

### 4.4 Z1 の generic helper と複合 key

- `node_helpers.go`: `FirstNonNil` / `MapNonNil` (T any、結果 Node、零値比較が `IsNil`)、`OrElse` (引数は caller が評価)、`SameMap` / `SameMapIndex` / `Deduplicate` / `AppendIfUnique` / `Contains` / `Index` (`SameNode`)。core 版と同じ制御構造 (SameMap の無変更時の入力返却、Deduplicate の `slices.Clone` からの再構築) を保つ。
- `node_helpers_test.go`: 零値、2 つの source の番兵、2 つの Store の実ノード、foreign の辺を経た同じ実ノードを混ぜた 9 要素の全部分列 (55 通り) で、`Key()` の列に対する core 版と、結果・callback の引数列 (回数と順序)・入力 slice の再利用・nil slice の保持を比べる。`OrElse` は全組で比べる。
- 変換テスト (`translate_test.go` + `testdata/rules.go.txt`): `FirstNonNil` / `MapNonNil` / `OrElse` は結果の型引数、`SameMap` などは T で判定し、`*ast.Symbol` の呼び出しは `core.` のまま (期待文字列で確認、出力は package store に対して型検査)。`nonExistentPropertyKey.propNode` と `accessibleChainCacheKey.location` は `NodeKey` になり、positional / keyed literal、nil literal (`NodeKey{}`)、field 代入、`Has` / `Add` / `make` / lookup が `Key()` に変換される。`unsupported.go.txt` は `core.FirstNonZero` の Node、読み戻される key の field (`is not a key only (read as a node at …)`)、list を slice に渡す箇所を問題として報告する。`any` への逃げは変換器に無い (`any(` / `interface{}` の出現なし)。
- `keys.go` は map key と `collections.Set` / `OrderedSet` / `SyncSet` の要素、`OrderedMap` / `SyncMap` の key から入れ子と配列を辿り、exported な field と読み戻しを未対応にする。

## 5. API と命令列 (検証指示 §3)

`go build -gcflags=-m=2` を現在の source (after) と HEAD の worktree (before) で取り、can / cannot と cost を関数名で突き合わせた (`inline-before-after.txt`、検証者の `inlinediff.py`)。通常 build と `-tags kperf` の判定は同一 (`diff -q` で SAME)。

| 対象 | 期待 | 結果 |
| --- | --- | --- |
| `node()`、`Parent()`、生成の子 accessor、`List.At` | can inline | `(*Store).node` 10 → **38**、`Node.Parent` 17 → **45**、typed view の子 (`QualifiedName.Left` など) 24 → **52**、list (`Block.Statements`) 16 → **43**、kind 多相の子 (`Node.Expression` など) 55 → **75**、`List.At` 50 → **78**。全部 can |
| `node()` の速い側 | 比較 1 + 分岐 1 | `MOVD R2,R3; LDP (R0),(R4,R5); CMP R5,R3; BGE; ADD R3<<5,R4,R1; JMP` — 比較 1・分岐 1 (bounds check と同じ数) + レジスタのコピー 1 + `JMP` 1 で、7d 設計 §2.6 の試作の +2 と同じ。遅い側は `TBZ $31` (印) + `LDP` (foreign 表) + `AND` + `CMP` / `BLS` (bounds) + `ADD R<<4` + `LDP` (`asm-accessors.txt`、`-overlay` で置いた `//go:noinline` の probe 経由) |
| `bind` の中の `node()` の形 | 子の辺と親 1 段だけ、増えていない | `ADD R<<5` (node() の速い側) は **2 箇所**で 7c と同じ (`asm-bind.txt`: どちらも `Parent()` 1 段)。`bind` の CALL は 61 → 66 で、増分は Z1 §4 の番兵の拒否 (`checkBinderWrite`、store.go:230〜231 が 27 命令) の `runtime.gopanic` 経路 |
| `FlowNode()` / `Symbol()` / `Locals()` | 分岐 1 つ、can inline | `FlowNode` 41、`Symbol` 40、`Locals` 76、`LocalSymbol` 68、`ReturnFlowNode` 69 で can。機械語は `MOVWU 24(R1)` (flow 語) + `MOVD 96(R0)` (`s.bound`) + `CBZ` の分岐 1 つ、bound 側は `Flow{b, ref}` を組むだけ、private 側は `CBNZW` (slot 0) + 表の bounds check |
| `Node.Ref()` | `LSR $5`、乗算なし | `MOVD (R0),R2; SUB R2,R1,R1; LSR $5,R1,R0` (cost 16、不変) |
| 7c の accessor の inline 判定 | cannot が増えていない | 同名で can → cannot は utility 8 本だけ (`FindAncestor` 56→89、`GetContainingClass` 79→108、`GetRootDeclaration` 50→106、`IsLiteralLikeElementAccess` 76→104、`IsLogicalOrCoalescingAssignmentExpression` 60→88、`IsLogicalOrCoalescingBinaryExpression` 66→94、`IsObjectLiteralOrClassExpressionMethodOrAccessor` 60→132、`IsPrologueDirective` 74→94)。原因は `Parent()` (+28) と子 accessor (+20〜28) の cost 増で、`IsObjectLiteralOrClassExpressionMethodOrAccessor` は (g) が逐語に戻って `Parent().Kind()` を 2 回読む分も乗る。cannot → can は無し |
| Z1 の `IsNil` | cost 6 → 11 が caller の予算を越えないか | 11 (`CBZ` + `MOVH` + `UBFX` + `CMPW` + `CSET`、分岐 2)。上の 8 本以外に IsNil で予算を越えた caller は無い。祖先ループ (probe `ProbeAncestors`) は 1 反復 11 命令 (nil 判定 3 + parent の解決 8) |
| 生成 accessor / Builder / Factory ctor の母数 | 件数の変化を退行と混同しない | typed view の accessor: before 576 can / 0 cannot → after 630 can / 0 cannot (+54 は解決層の resolver、削除なし)。Builder ctor: 178 cannot + 1 can で不変 (7c と同じく S1 が原因)。Factory ctor (新設、before なし): `New*` 194 cannot (cost 123〜794) + `NewNodeList` 63 / `NewModifierList` 67 can、`Update*` 167 cannot、`Clone*` 193 cannot |
| 実際の call site | 定義の can だけで完了にしない | storebinder + storeparser の `inlining call to` の件数: `(*Store).node` 595 → 477、`Node.Parent` 43 → 21、`Node.Kind` 277 → 254、`Node.IsNil` 88 → 98、`(*Store).list` 0 → 49、`List.At` 10 → 10。`node` / `Parent` の減少は上の utility 8 本が binder に inline されなくなった分 (binder の呼び出しは `GetContainingClass` 4、`IsObjectLiteralOrClassExpressionMethodOrAccessor` 2、`IsPrologueDirective` 2、`FindAncestor` 1、`IsLogicalOrCoalescingAssignmentExpression` 1) で、accessor 自体が call site で inline を外れた例は無い |
| `Node.File()` | (期待なし) | **cannot inline (cost 111)**。7d 設計 §2.7 の見積りは `GetSourceFileOfNode` が「1〜2 load」になるとして −90 inst/node を見込むが、今の形は呼び出し 1 回 + prologue が乗る (§8) |
| bce | 生成物の bounds check | `-d=ssa/check_bce` の報告は 7c 2,638 → 4,708 行 (views 994 → 1,223、foreach 1,053 → 1,229、factory 1,663 新設)。増分は accessor ごとの foreign 表の index の bounds check (遅い側) と Factory |

## 6. 費用 (検証指示 §4)

ユーザーが 2026-09-27 17:15 に取った raw (`kpc2-*-20260927-1715.txt`、`-test.benchtime 20x -test.count 5`、順序 before1 → after → after2 → before2) を検証者が読み直して中央値を出した (`kpc2-summary.txt`)。after2 の binary は現在の source から作り直した binary と byte 一致 (§1)。load average は 4.1 → 2.8 で線の 1.5 を切っていない (実装報告 §9 のとおり)。

| 指標 | 線 | before1 | after (最初の 7d-0) | **after2 (現在)** | before2 | 7c |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| Walk cycles S/P | ≤ 1.15 | 1.106 | 1.188 | **1.054** | 1.149 | 1.137 |
| Walk inst S/P | 報告 | 1.219 | 1.307 | 1.297 | 1.221 | |
| Parse cycles S/P (checker.ts) | ≤ 1.00 | — | 0.825 | **0.824** | — | 0.824 |
| Parse inst S/P | 報告 | — | 0.825 | 0.825 | — | |
| Parse cycles S/P (dom) | 7d-1 の before | — | 0.866 | **0.865** (inst 0.888) | — | |
| Bind cycles S/P | ≤ 1.10 | 0.992 | 1.027 | **1.020** | 0.984 | 0.984 |
| Bind inst S/P | 報告 | 1.091 | 1.137 | 1.143 | 1.089 | |

Store 側の絶対値 (中央値): Walk inst 26.31M → 27.97M (+6.3%、+5.6 inst/visit)、cycles 8.54M → 8.33M (−2.4%)、IPC 3.08 → 3.35。Bind inst 120.7M → 126.3M (+4.6%、+18.7 inst/node)、cycles 33.1M → 34.1M (+3.0%)、IPC 3.65 → 3.71。Parse inst 214.7M → 215.2M (+0.2%)、cycles 不変。B/op は Bind 6.51 → 6.95 MB (+6.8%、D2 の宣言 16 B と Symbol 112 B)、Parse 不変。Pointer 側は各回 ±2% 以内。

注意: after2 の Walk の Pointer 側は 5 run の spread が 4.8% で中央値 7.91M と他の 3 回 (7.67〜7.75M) より高い。他の回の Pointer 中央値 7.72M で割ると after2 の Walk は **1.080** で、それでも線の内側だが「7c より下がった」とまでは言えない (inst は +6.3% 増えている)。

FR1 / V1 / D2 の帰属 (before → after2 の差):

- FR1 (辺の解決): Walk の +5.6 inst/visit と Bind の +18.7 inst/node の主因。`node()` の速い側が +2 命令 (§5)、訪問 1 回あたり子の辺 1 + 親 0〜1 で +2〜4、残りは `ForEachChild` の case をまとめた判定と Z1 の `IsNil` (+5 命令/回、bind では 98 call site)。cycles は Walk で減り (呼び出しが消えて IPC が上がった)、Bind で +3.0%。
- V1 (解決層の分岐): bind は id 層を使うので Bind には出ない (asm-bind に `s.bound` の分岐なし)。Walk / Parse にも出ない。**V1 の費用は 7d-0 の 3 本には現れず、7d-2 の Check で初めて測れる。**
- D2 (宣言を `Node` で持つ): Bind の B/op +6.8% (allocs は不変)。cycles への寄与は切り分けていない。
- 番兵の拒否 (Z1 §4、実装報告 §8 の 10): bind の `checkBinderWrite` が 12 箇所 (比較 1 + 分岐 1 ずつ)。
- private 名の gid: KPC の入力に private member の宣言が無く、初回採番 0 回 (実装報告 §4)。費用は乗っていない。

ns (参考、`ns.txt`): §7 の末尾に記す。

### provenanceを固定した新規KPC

ユーザーが `V/run-kpc-current.sh` を実行し、20260927-175123のbefore1→current→before2を全3段で取得した。全9rawはPASS。source/binaryのSHAを実行前後で照合するスクリプトを使用。開始load4.59、終了3.56で、指定の1.5未満を満たさない。artifactはcurrentだが、**低負荷条件まで含めた性能ゲートは未確定**。

| 段 | before Store/Pointer cycles | current Store/Pointer cycles | 線 | current inst比 | current IPC P / S |
| --- | ---: | ---: | ---: | ---: | ---: |
| Walk | 1.1187 | 1.0876 | ≤1.15 | 1.2965 | 2.803 / 3.340 |
| Parse checker.ts | 0.8264 | 0.8214 | ≤1.00 | 0.8245 | 3.885 / 3.900 |
| Parse dom | unsupported | 0.8677 | before無し、後段基準 | 0.8881 | 4.089 / 4.186 |
| Bind cycles/node | 0.9870 | 1.0301 | ≤1.10 | 1.1378 | 3.290 / 3.642 |

beforeは前後2実行10標本、currentは5標本。比較は `V/benchstat-kpc-current-{walk,parse,bind}-before-after.txt` と同一binaryのpointer-store比較。Walkのcycles/visitはPointer25.842 / Store28.105、Bindは112.9 / 116.3 cycles/node。単位の異なるParseは67,055,588 / 55,076,693 cycles/op。

| 旧Storeからcurrent Storeへの差 | cycles | inst | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Walk | −3.55% | +6.39% | 0のまま | 0のまま |
| Parse checker | −0.71% | +0.88% | +101B、約0.00063% | 584のまま |
| Bind | +5.41%/op、+5.44%/node | +4.64%/op | +6.77% | −0.11% |

cycles/instの上記差はいずれもbenchstat p=.001。高負荷という測定条件はp値では解消しない。5標本のcurrentでは95%CIに標本数不足の注記がある。Walkの命令増とcycles減、Parseの命令増と小さいcycles減を、同じ改善指標としてまとめない。Bindの追加費用は再現しており、線内でも消さない。

KPC時のBind B/opはPointer7,423,983 / Store6,953,859、allocs/opは13,952 / 12,632。旧Storeは6,513,107B / 12,646allocs。GCオフと通常GCではscratch/poolの条件が異なるので§6冒頭の通常値とは混ぜない。FR1/Z1の辺解決とinline予算、V1の所有者解決、D2の宣言16B・Symbol112Bは因果候補。bindの中心はid層のままで、V1の分岐をBind差の原因と断定しない。独立した変更別計測がないため、合計差をFR1/V1/D2へ数値配賦することはできない。



### KPC 再実行 2026-09-27 17:56

最新artifactは `V/kpc-review-20260927-175655/` の集計と、その親の `kpc-*-20260927-175655.txt`。全9rawはPASS。selected repoとrevisionは§2と同じで、source・before source・全binaryのSHA照合が成功したため `current`。before Parse domは該当Benchmark行がなく `unsupported`。開始load1.82 / 終了1.80まで下がったが、指定の1.5未満は引き続き未達である。

| 段 | before cycles S/P | current cycles S/P | 線 | current inst S/P | IPC P / S |
| --- | ---: | ---: | ---: | ---: | ---: |
| walk | 1.1291 | 1.0727 | ≤1.15 | 1.2963 | 2.774 / 3.353 |
| parse | 0.8267 | 0.8227 | ≤1.00 | 0.8255 | 3.873 / 3.887 |
| parse dom | unsupported | 0.8681 | 後段の基準 | 0.8880 | 4.080 / 4.175 |
| bind | 0.9848 | 1.0180 | ≤1.10 | 1.1406 | 3.339 / 3.737 |

比較はこのartifact内の `benchstat-*-before-after.txt`、同一binaryの `benchstat-*-current-pointer-store.txt`、前回との `benchstat-*-previous-rerun.txt`。before1+before2の10標本とcurrent5標本を用いた。5標本は95%CIの必要数に不足する。

旧Storeに対する今回Storeの差は、Walk cycles −3.66% / inst +6.32%、Parse checker cycles +0.33% / inst +1.28%、Bind cycles/op +2.68%・cycles/node +2.63% / inst/op +4.50%。Parse cyclesはp=.008、その他のこれらの差はp=.001。Bind B/op +6.77%、allocs/op −0.11%も再現する。前回から今回の同じStore binaryのBind cycles/opは−2.71%（p=.008）、inst/op差は未解決（p=.095）であり、前回のcycle増分全体をコード変更へ帰属すべきでない。Parse checkerのbefore差の符号は前回の−0.71%から今回+0.33%へ変わり、小さいcycle効果の安定性は確認できない。

今回KPCの中央値。GCオフの数値であり、通常GC値とは混ぜない。

| stage / variant | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| WalkKPCV1/pointer-8 | 2,483,808 | 0 | 0 |
| WalkKPCV1/store-8 | 2,663,538 | 0 | 0 |
| ParseKPCV1/pointer-8 | 21,475,125 | 26,137,522 | 11,931 |
| ParseKPCV1/store-8 | 17,698,300 | 15,974,302 | 584 |
| ParseKPCV1/dom/pointer-8 | 9,682,438 | 9,906,383 | 2,790 |
| ParseKPCV1/dom/store-8 | 8,413,187 | 6,038,667 | 1,534 |
| BindKPCV1/pointer-8 | 10,553,690 | 7,423,983 | 13,952 |
| BindKPCV1/store-8 | 10,751,562 | 6,953,859 | 12,632 |

診断: 同一binaryのPointer比は3ゲートとも数値上は線内。FR1/Z1の辺解決とForEachChild、Bindの宣言・Symbolの追加費用という既存のhot path / allocation driverの評価は維持する。個別の費用配賦はこの測定だけではできない。次の対応はsource identityの正しさ修正と自動化率検証の補完。低負荷条件まで厳密に満たす性能合格には、1.5未満での再測定が必要。実装全体の「7d-0未合格」は変わらない。

## 7. retained heap と GC (検証指示 §5)

`BenchmarkStoreBindRetainedV1` を sub-benchmark 名で variant を 1 つに絞り、入力 × variant を交互に 5 run、各 run が別プロセス、`GODEBUG=gctrace=1` (`retained/`、`retained2/`、`retained-summary.py`)。ベンチの構築経路 (`bench_test.go:127〜160`) は `binds` のうち名前が一致した 1 つの `b.Run` の中だけで parse + bind し、もう片方は `b.Run` の filter で走らない。text の読み (`in.files(b)`) は両 variant 共通の baseline で `ReadMemStats(&before)` より前。等価テストは走らせない (`-test.run '^$'`)。retained は GC 2 回後の HeapAlloc の差、mark CPU は最後の forced GC (両 variant とも 8 回目) の `assist + background + idle` の ms。

1 回目 (`retained/`、**load average 11〜18**。別の検証プロセスがベンチを回していた最中で、GC の時間は参考にならない):

| 入力 | retained B/node Pointer / Store | 比 (線) | 7c | mark CPU ms P / S | 比 |
| --- | --- | ---: | ---: | --- | ---: |
| checker.ts | 109.40 / 61.82 | **0.565** (≤ 0.60) | 0.561 | 59.0 / 6.7 | 0.114 |
| fixtures | 124.70 / 74.97 | **0.601** (≤ 0.70) | 0.583 | 300 / 42 | 0.140 |
| dom | 128.50 / 94.94 | 0.739 | 0.696 | 17.5 / 7.4 | 0.423 |

retained B/node は 5 run で 0.1% 以内 (checker.ts store 61.82〜61.86) で load に依らず、実装報告 (61.86 / 74.96 / 95.03) と同じ。7c 比の増分は checker.ts +0.4 B/node、fixtures +2.3 B/node、dom +5.5 B/node で、D2 の見積り (宣言 +8 B、Symbol +16 B) の範囲。

2 回目 (`retained2/`、別プロセスの終了直後に取り直し。load average は 5.5 → 5.9 と表示されるが減衰中の平均で、CPU を食う別プロセスは無かった):

| 入力 | retained B/node Pointer / Store | 比 (線) | mark CPU ms Pointer / Store (中央値、min..max) | 比 (線) | 7c | wall `gc-ms` 比 |
| --- | --- | ---: | --- | ---: | ---: | ---: |
| checker.ts | 109.40 / 61.84 | **0.565** (≤ 0.60) | 24.2 (20.7..37.0) / 3.3 (2.9..5.0) | **0.136** (≤ 0.25) | 0.10 | 0.277 |
| fixtures | 124.60 / 74.96 | **0.602** (≤ 0.70) | 99.0 (95.0..113) / 20.5 (17.5..25.6) | **0.207** (≤ 0.35) | 0.16 | 0.287 |
| dom | 128.40 / 94.99 | 0.740 | 9.4 (7.0..9.5) / 3.6 (2.9..4.0) | 0.383 | — | 0.743 |

mark CPU は 7c (checker.ts 1.9 / 19.2 ms = 0.10、fixtures 13 / 82 = 0.16) より Store 側が増えた (checker.ts 3.3 ms、fixtures 20.5 ms)。7c の gctrace は別の日の別 checkout で load も違うので絶対値は `stale` として比べないが、D2 で `Declarations` が `[]Node` (要素 16 B、pointer 2 本) と `ValueDeclaration` (pointer 2 本) になり、symbol ごとに GC が辿る pointer が 2〜4 本増えたことと整合する (7d 設計の見積り 0.10 → 0.13 前後)。線の内側。
live heap (最後の forced GC 後): checker.ts Pointer 35 MB / Store 21 MB、fixtures 143 / 89 MB、dom 16 / 12 MB。forced GC の回数は両 variant とも 8。

compiler プロジェクト (7d 設計 §2.7 の check 後の線 retained ≤ 0.85 / mark CPU ≤ 0.75 の前提 = parse + bind の比が 0.56 前後): `BenchmarkStoreBindRetainedV1` に compiler プロジェクトの入力は無いので、同じ手順の一時ベンチ (`zz_verify_retained_test.go`、検証後に削除、source の sha256 不変) を `testdata/fixtures/compiler` に当てた。入力は `ParsedCommandLine.FileNames()` の root file **79 本** (lib.d.ts などの program が足す file は含まない。7d 設計 §2.7 の「124 file」は program 全体)、Pointer で 937,348 node (Check KPC の 936,205 node とほぼ同じ)。variant 別プロセス、交互 3 run、`GODEBUG=gctrace=1`。同じ一時ベンチは GC 2 回の直後 (結果が live のうち) に `pprof.Lookup("heap")` を書き出す (`retained-compiler/*.heap`):

| 入力 | retained B/node Pointer / Store | 比 | retained MB P / S | mark CPU ms P / S | 比 |
| --- | --- | ---: | --- | --- | ---: |
| compiler (79 file) | 121.80 / 69.05 | **0.567** | 114.1 / 63.4 | 77.0 / 13.8 | 0.179 |
| fixtures (同じ一時ベンチで再現) | 124.60 / 74.96 | 0.602 | 136.8 / 80.6 | 100 / 20.9 | 0.209 |

**compiler プロジェクトの parse + bind の比は 0.567 で、7d 設計 §2.7 が check 後の線 (retained ≤ 0.85、mark CPU ≤ 0.75) の前提に置いた「0.56 前後」のとおり。0.70 を超えていないので 7d-2 の線を直す必要は無い。**

live heap profile の監査 (`retained-compiler-heap-audit.txt`、`go tool pprof -sample_index=inuse_space`): Store のプロセス (live 78 MB) の割り当て元は `(*Builder).Finish` 38.0 MB (Store の列)、`declareSymbolEx` 配下の symbol arena 13.9 MB + 6.0 MB、`compilerProjectFiles` (text、両 variant 共通の baseline) 11.0 MB、`Bound.Compact` の `exact` 3.7 MB、`Declarations` の `[]Node` 2.0 MB、`NewLocals` 0.5 MB で、**`internal/ast`、`internal/parser`、`internal/binder` の割り当ては 1 つも無い**。Pointer のプロセス (live 121 MB) は `ast.Identifier` などの `slices.Grow` (node arena) と `binder.declareSymbolEx` で、`store` / `storeparser` / `storebinder` の割り当ては無い。ベンチの構築経路 (`binds` の 1 つだけが走る) と合わせて、各プロセスが対象 variant の AST だけを持つことを確かめた。profile だけで参照経路の不存在を証明したとは扱わないが、割り当て元に他方の package が無いことは示せた。

ns (参考、`ns.txt` / `ns-summary.txt`、`-benchtime 2s -count 5`、load average 5.2 → 4.2 で線の 1.5 を切っていないので判定には使わない): Walk checker.ts S/P 0.992 (dom 1.394、Store 側の spread 55%)、Parse checker.ts 0.759 (dom 0.805)、Bind checker.ts 1.025 (dom 0.986)。KPC の cycles 比 (Walk 1.054、Parse 0.824、Bind 1.020) と向きは合う。

## 8. 7d 設計への反映案

1. **FR1 の hot path の実測**: 辺の解決は設計 §2.6 の試作どおり速い側 +2 命令 (レジスタのコピーと JMP)。Walk +5.6 inst/visit、Bind +18.7 inst/node、cycles は Walk −2.4% (呼び出しの消滅と相殺) / Bind +3.0%。§2.7 の見積り「子の辺 +10、親 +8」は 32B 化の分を含むので、FR1 単独の +2 は見積りの内側。
2. **V1 の分岐**: 7d-0 の 3 本には出ない (bind は id 層)。`FlowNode()` / `Symbol()` は分岐 1 つ + 2 load で inline される。Check での実測は 7d-2 に持ち越し。
3. **D2**: retained は checker.ts +0.4 B/node、fixtures +2.3、dom +5.5 で見積りの範囲、Bind の B/op +6.8%。
4. **`Node.File()` が inline されない (cost 111)**: §2.7 の見積り (`GetSourceFileOfNode` 10.1 回/node が 1〜2 load になって −90 inst/node) はこのままでは出ない。7d-2 の前に「`s.file != nil` の速い側だけを inline し、親を辿る側を別関数にする」形 (cost 見込み 20〜30) を試す価値がある。正しさの変更ではないので 7d-0 では直していない。
5. **inline を失った utility 8 本**: `Parent()` と子 accessor の cost 増が原因で、checker の変換出力でも同じことが起きる (Pointer 版では field 読みで cost が低い)。Check KPC の切り分けに「inline を失った関数の一覧」を最初から入れる。
6. **変換器の自動化率**: (a)(c) は設計変更と (f) を除いて全一致、(b)(d) は書き込み側で原理的に 0。線の定義をユーザーが決める (§4.2)。
7. **7d-2 の線 (retained ≤ 0.85、mark CPU ≤ 0.75) の前提**: compiler プロジェクトの parse + bind の retained 比は 0.567 (mark CPU 0.179) で前提どおり。線はそのまま。ただし入力は root 79 file で、lib file を含む program 全体では未計測。
8. Symbol 112 B (ast.Symbol 96 B) は記録のみ。

## 9. 限界

- KPC は検証者自身では取り直していない (root が要る)。after2 の binary が現在の source の再ビルドと byte 一致することと、raw からの中央値の再計算で代えた。`kpc-verify.sh` を置いたので、ユーザーが走らせれば before1 → after → before2 の再計測になる。
- KPC 中の load average は 2.8〜4.1、retained の 1 回目は 11〜18 (別の検証プロセスと重なった)。retained の bytes は load に依らないが、GC の時間は 2 回目 (§7) を採る。
- Bind の +18.7 inst/node の関数別の帰属は取っていない (inline 判定と asm-bind の差からの推定)。
- storage テストの容量 4 通りは 11 項目 + 2 で、項目 13 は容量 1 だけ (§3.2)。
- `verification-20260927/` は本検証と無関係に同時刻に走った別プロセスの出力で、本報告の数字はそこから取っていない。

## 10. レビュー追加契約 §5 の段別対応状況

| 段 | 必須事項 | 7d-0 の状態 |
| --- | --- | --- |
| 7d-0 | private 名を `GetSymbolId` へ統一 | 済 (`storebinder/binder.go:411`、64bit の 10 進) |
| 7d-0 | local id と gid の用途分離 | 済 (`Symbol.id` は Bound の index、`gid` は `atomic.Uint64`) |
| 7d-0 | 複数 file / 同名 private の試験 | 済 (`TestSymbolIdAndPrivateNames`、§3.2) |
| 7d-0 | 採番契約の試験 (安定・並列初回・非零一意・上限) | 済 (同上 + `TestSymbolIdLimit`) |
| 7d-0 | variant 別プロセスの parse / bind retained / GC | 済 (§7)。live heap profile の監査は一時ベンチで取った |
| 7d-2 (未完了) | checker の private lookup、symbol の一対一対応と fallback 順序、数値一致を仮定しない等価 | 未着手 |
| 7d-2 (未完了) | Pointer AST を持たない測定 adapter (H1) | 未着手。source の Store に program の token と StoreID を渡す production の入口も未決 (実装報告 §8 の 1: 今は storeparser が StoreID 0 / space nil、テストは `SetSourceIdentity`) |
| 7d-2 (未完了) | 印字全区間と内訳の性能評価 | 未着手。Check の合格で代替しない |

実装報告 §8 の判断待ち 3 件 (source の Store の token、nil の `Flow` の `==`、RTA の動的辺) は本検証でも設計に書かれていない判断として残る。2 (nil の `Flow` どうしが `==` で一致しない) は 7d-2 の変換規則が要る点で、storage テストは `sameFlow` (`a == b || 両方 IsNil`) で回避している。

## 11. 再現手順と生データ

```sh
ROOT=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript; TSC=$ROOT/tsc; OUT=$TSC/internal/ast/docs/_store-checker-7d0-results
# §3: 生成物の再現、build / vet、ガード、テスト (verify-*.txt)
cd $ROOT && node tools/scripts/tsc/generate.ts && (cd tools && go run ./cmd/storetranslate) && git status --short
cd $TSC && go test -count=1 -v ./internal/ast/store/ ./internal/storeparser/ ./internal/storebinder/   # と -tags storechecks
# §4: binder-parser.diff、utilities-7c.go / utilities-7d0.go、utilities-funcdiff.txt (verify-scripts/funcdiff.py)、verify-hash-tamper.txt
# §5: inline-store{,-before}.txt、inline-before-after.txt (verify-scripts/inlinediff.py)、inline-callers{,-before}-full.txt、asm-accessors.txt (overlay の probe)、asm-bind{,-before}.txt、bce-store.txt
# §6: kpc2-*-20260927-1715.txt (ユーザー計測) → kpc2-summary.txt (verify-scripts/kpc-summary.py)。再計測は別ターミナルで:
bash $OUT/kpc-verify.sh
# §7: retained/ (load 高)、retained2/、retained-compiler/ (一時ベンチ)、retained{,2,-compiler}-summary.txt (verify-scripts/retained-summary.py)、retained-compiler-heap-audit.txt、ns.txt / ns-summary.txt
```

集計スクリプトは `verify-scripts/` に置いた (検証中に使ったセッションの scratchpad が消えたので、同じ内容を書き直して結果が再現することを確かめた)。before の worktree は検証後に外した (`git worktree remove`)。before の kperf binary は `/tmp/7d0-before/` に残してある。`asm-accessors.txt` の probe (`//go:noinline` の wrapper 13 本を `-overlay` で `internal/zzasmprobe` に置いて `go build` + `objdump`) と storebinder の一時ベンチ file は scratchpad と一緒に消えた。probe の中身は §5 の表に書いた命令列で読める。

| file | 節 |
| --- | --- |
| `verify-env.txt`、`verify-source-sha256-start.txt`、`verify-git-status-*.txt` | §2 |
| `verify-generate.txt`、`verify-storetranslate.txt`、`verify-build-vet.txt`、`verify-guard-diffstat.txt`、`verify-store-imports.txt`、`verify-test-*.txt`、`verify-excl-{7c,7d0}.txt` | §3 |
| `binder-parser.diff`、`generator.diff`、`utilities-{7c,7d0}.go`、`utilities-funcdiff.txt`、`verify-hash-tamper.txt` | §4 |
| `inline-*.txt`、`asm-*.txt`、`bce-store.txt` | §5 |
| `kpc2-summary.txt`、`kpc-verify.sh`、`*.kperf.test`、`/tmp/7d0-before/*.test` | §6 |
| `retained/`、`retained2/`、`retained-compiler/`、`retained-summary.txt`、`ns.txt` | §7 |

## 12. 指摘への対応 (2026-09-28)

本報告 §3.2 / §8 と、別検証の監査 (`verification-20260927/{correctness-code,translator,performance}-audit.md`) の指摘を扱った。

### 修正したもの

| 指摘 | 出どころ | 対応 |
| --- | --- | --- |
| P1: production で parse した source の Store に program の token (`space`) と StoreID を与える経路が無く、Factory が source ノードを `a node of another program or region` で拒否する。テストは `SetSourceIdentity` で補っていた | correctness-code-audit 追補、実装報告 §8 の 1 | `store.NewBuilder(src, space, id)` / `Reset(src, space, id)` が space を受け取り `Finish` が写す。`storeparser.ParseSourceFileIn(space, id, opts, text, kind)` を足し、`ParseSourceFile` は `(nil, 0)` (program 外の Store、どの Factory も取らない)。生成器の `convert` は `nil`。`SetSourceIdentity` を削除し、storage テストの `parseAndBind` は `ParseSourceFileIn` で parse する。Builder の `space` 設定を外す変異で storage テストが落ちることを確認 |
| 項目 13 が容量 1 だけ | §3.2 の 1 | 容量 1 / 2 / 3 / 4096 で回す。拒否の対象に「program 外で parse したノード」を足した |
| 項目 15 が `texts` の再確保を assert しない | §3.2 の 2 | `TextsCap` (export_test) で cap の変化を assert |
| 項目 8 の `SetLocals` / `SetListElement` は source と nil だけ | §3.2 の 3 | 別 region・seal 済み・零値・番兵 (SetLocals) と別 region・seal 済み (SetListElement) の拒否、同 region の合成は通ることを足した |
| `SameMapIndex` の index 引数、`AppendIfUnique` の入力再利用を直接比べていない | translator-audit | core 版との比較に index の列と再利用 (`slices.Clip` した入力を返したか) を足した |

検査: `go build ./...`、`go vet`、`go test` tag なし / `-tags storechecks` (store、storeparser、storebinder)、`go test ./cmd/storetranslate/`、`generate.ts` と `storetranslate` の再実行で生成物の差分なし。bind 等価は `17085 compared, 330 excluded, 0 with a parse mismatch; 708382 declaration Stores checked` で不変。storeparser の失敗は §3.1 の `FuzzEquivalence/a708303f611b95c8` (HEAD 由来) だけ。

### 直さなかったもの

- **`Node.File()` の inline (§8 の 4)**: 「速い側だけ inline、cost 20〜30」は成り立たない。非 inline の呼び出しは 1 つで cost 57 かかり、残り 23 に nil 契約 (零値と番兵の `File()` は nil) の判定が入らない。`parentFile` を `//go:noinline` にすると `File` は 89、条件の組み方を 9 通り試して最小 81 (予算 80)。79 に入る形は零値の `n.s` を参照する。`File()` / `parentFile` は元の形 (cost 111、遅い側の走査を含んで呼び出し 1 回) のまま。§2.7 の −90 inst/node は「呼び出し 1 回 + 2 load」で見直す (7d-2 の Check KPC で測る)。
- **fuzz seed `a708303f`**: HEAD でも同じ理由で落ちる JSX の module 判定の既存問題。7d-0 の範囲外。
- **変換器の比較器の厳密化と線の読み方** (translator-audit、§4.2): 比較器は追加行の包含率で、7c の分類との hunk 対応も残っていない。作り直しと線の定義はユーザーの判断。
- **`OrElse` の引数評価順のテスト**: 引数は呼び出し側が評価する Go の言語仕様で、helper の実装が変えられない。

### 残る作業

- **KPC の取り直し**: `NewBuilder` / `Reset` が 1 word 多く書くので、Parse / Bind / Walk の binary が変わった。§6 の値は修正前の source のもの。修正後の source から `-tags kperf` で作り直した current binary と source / binary の SHA マニフェストを `verification-20260927/kpc-20260928/` に置いた。before binary は `run-kpc-current.sh` と同じもの (SHA 一致を確認)。低負荷 (load < 1.5) で別ターミナルから `bash verification-20260927/kpc-20260928/run-kpc.sh` を走らせる (sudo、before1 → current → before2 の 3 段)。
- 実装報告 §8 の判断待ちは全部解決: 2 (nil の `Flow` の `==`) は下の「nil の Flow の正規化」、3 (RTA の動的辺) は下の「到達計算の置き換え」。変換器の自動化率はユーザー判断で後回し (比較器の精度は後から直せる)。

### 修正後の KPC (2026-09-28 20:28)

ユーザーが `kpc-20260928/run-kpc.sh` を実行した (`kpc-{before1,current,before2}-{walk,parse,bind}-20260928-202853.txt`、全 9 raw PASS、source / binary の SHA は実行前後で一致)。load average は 1.82 → 2.12 で線の 1.5 を切っていない。値は中央値の S/P 比 (同じ binary の Pointer との比)。

| 段 | 線 | before1 | **current** | before2 | 09-27 17:56 の current |
| --- | --- | ---: | ---: | ---: | ---: |
| Walk cycles/visit | ≤ 1.15 | 1.142 | **1.093** (inst 1.297) | 1.113 | 1.073 |
| Parse cycles/op (checker.ts) | ≤ 1.00 | 0.828 | **0.822** (inst 0.825) | 0.825 | 0.823 |
| Parse cycles/op (dom) | 7d-1 の基準 | — | **0.870** (inst 0.888) | — | 0.868 |
| Bind cycles/node | ≤ 1.10 | 0.986 | **1.014** (inst 1.141) | 0.988 | 1.018 |

- 3 本とも線の内側。P1 の修正 (`Reset` が 1 word 多く書く) は Parse の Store inst 215.0M (09-27 は 215.2M) で見えない。
- Walk の比が 09-27 より 0.02 高いのは Pointer 側の spread (2.8%) と before の揺れ (1.11〜1.14) の範囲で、Walk は P1 の変更を通らない。
- Store の before (2 回の平均) → current: Walk cycles −3.3% / inst +6.3%、Bind cycles/node +3.2% / inst/node +4.6%、Bind B/op +6.8% (6.51 → 6.95 MB)。§6 と同じ傾向。
- 低負荷条件は未達のまま。判定を厳密にするには load < 1.5 で取り直す。

### nil の Flow の正規化 (2026-09-28、実装報告 §8 の 2)

nil の Flow には `Flow{}` (零値、合成 Store の slot 0) と `Flow{b, 0}` (source の Store から読んだ flow の無いノード、Start の `Antecedent()`、`FlowList` の終端) があり、どれも `IsNil` だが互いに `==` でなかった。nil でない Flow は `{所有者の Bound, 番号}` で一意なので、食い違いは「nil と nil の `==`」と map の key だけ。今の checker の `==` と map key の箇所 (`flow.go:137` sharedFlows、`213` ReduceLabel target、`2524` lastFlowNode、`flowNodeReachable` / `flowNodePostSuper`、`FlowLoopKey`) はどれも片側が非 nil で、現状は食い違わないが、変換器は `==` を `==` に訳すので上流の変更で黙って壊れうる。

対応 (B 案): resolver が nil を全部 `Flow{}` にする。

- `Antecedent`、`FlowList.Flow`、`ReduceLabel` の target は `if r == 0 { return Flow{} }` を直接書く (早期 return、機械語は外れない分岐 1 本)。helper (`flowOf`) は置かない: `flowAt` が inline 予算で使えず規則を 1 か所に集められないので、規則は `Flow` の doc comment に書き、全 resolver を Pointer の graph と比べるテスト (下記) で強制する。`FlowList.Flow` と ReduceLabel の target は `addAntecedent` が要素の `Flags` を先に読む構造上 nil にならないが、「FlowRef を読む resolver は全部 0 を判定する」一律の規則にするため残す。
- `flowAt` (`FlowNode` と予約 slot の accessor) だけは選択 (`if i == 0 { b = nil }`) で `CMPW` + `CSEL` の分岐なし。flow の無いノードが多く、分岐だと予測が外れうるため (推測。7d-2 の Check KPC で確かめる)。
- package 変数の `NoFlow` は使わない (Go に struct の const は無く、`var` を返すと 2 word の global load になる)。
- inline: `FlowNode` 41 → 48、予約 slot の accessor 69 → 76、`Antecedent` / `FlowList.Flow` 24、`ReduceLabel` 42。他に can ↔ cannot が変わった関数は無い。
- テスト: storage テストの `sameFlow` を撤去して `==` に。flow graph の比較 (項目 12) は nil 側で `f == store.Flow{}` を要求し、`FlowNode` / `ReturnFlowNode` / `EndFlowNode` / `Antecedent` / `FlowList.Flow` / `ReduceLabel` の 6 経路を覆う。`Antecedent` と `flowAt` の nil 化をそれぞれ外す変異で落ちることを確認。
- 両 tag の test PASS (storeparser は既知 seed のみ)、bind 等価は不変。KPC は Walk / Bind が flow を読まない (bind は id 層) ので Parse / Walk / Bind には出ない見込みで、費用は 7d-2 の Check KPC で測る。

### 到達計算の置き換え (2026-09-28、実装報告 §8 の 3)

変換器の `reach` は RTA の call graph から静的な辺だけを使い、動的な辺 (関数値の呼び出し。RTA は program 全体で address を取られた同じ signature の関数を全部 callee にする) は集めて捨て、到達した関数が値として名前を書く ast 関数 (`referencedFuncs`) で代えていた。落ちていたのは `IsDecorator` (checker / parser が値で使う)、`newParentInChildrenSetter` (Parent を書く Pointer 専用)、`collectIdentifiersForSourceFile` (SourceFile の識別子集合) の 3 つ。

この規則は正しい: 出力が呼ぶ関数値は (1) 出力自身が名前を書く (拾われる)、(2) 呼び出し側から渡される (7d-2 の checker の訳が `store.IsDecorator` を書き、無ければコンパイルエラー)、(3) field / 変数から読む (Pointer の型のままでコンパイルエラー) のどれかで、取りこぼしは黙った差にならない。

RTA と SSA 構築 (tsc 全体 65,638 関数) は、使う辺が構文上の `F(...)` と同じなので要らない。`reach` を「本体 (closure を含む) に出てくる ast の package-level 関数の識別子を、呼び出しか値かを問わず辿る」(`namedFuncs`、`TypesInfo.Uses`、instantiation は元の generic) に置き換え、`rta` / `ssa` / `ssautil` の import と `withClosures` / `topLevel` / `referencedFuncs` を削除した。

- 到達集合 176 は置き換え前と同一 (`STORETRANSLATE_REACH=1` の出力を比較)、`utilities.go` の差は header の Reach 注記だけ。
- binder 試作 (`-binder`) の `binder-trial-compare.txt` / `binder-trial-problems.txt` は実装時の出力と一致 (22/64、83 problems)。
- `go test ./cmd/storetranslate/`、store / storebinder の test PASS。
- 7d 設計 §2.1 (E3) の checker の到達計算 (method と interface 呼び出しを含む) は別の話で、RTA が要るかは 7d-2 で決める (設計に追記)。

