# accessor の税と祖先 walk の inline の計測 指示書

作成日: 2026-10-02。対象は計測を担当するエージェント。

[xctrace 報告](store-checker-7d2c-xctrace-report-20261002.md) の差 (checker 自身の分で約 320 M cycles/回) のうち、名前の付いていない 2 つを数で確かめる。**本段は測定で、production の変更は行わない。** 試作は scratch の overlay だけで作る。

## 背景 (xctrace 報告の検算で確定したこと)

- 両側の仕事量は同じ。
  - 型・symbol・signature・instantiation の数が一致する。
  - flow 解析の呼び出し回数も一致する (`narrowType` 863,574 回、`isTypeSubsetOfUnion` 141,141 回、`isMatchingReference` 1,980,717 回)。
  - 差は全部、1 回あたりの命令数。
- 差は 2 つの現象に分かれる。

| | 現象 | xctrace の値 | 候補 |
| --- | --- | --- | --- |
| (a) | 祖先 walk の関数 (`FindAncestor` ほか 8 個) が inline 予算を超え、呼び出しと callback の間接呼び出しが残る | 仕組み 3、+147 M cycles (上限) | B |
| (b) | inline された Store の accessor (`Parent`・`IsNil`・`Kind` など) の増えた命令が、呼び出し元の self に入る | 仕組み 6、+109 M cycles (+552 M inst) の大半と見ている (仮説) | D と accessor 全般 |

- 測ったままの Check の cycles 比は 1.150 で、確定した候補 A + C だけでは 1.101 (線 1.10 の外)。
- 線を越えられるかは、(a) と (b) の大きさで決まる。

| 前提 | 文書 |
| --- | --- |
| 仕組み別の分類、§4.1 の inline の表と objdump、§4.2 の `Parent()` の呼び出し元 | xctrace 報告 §3.2・§4 |
| 計数の overlay | `_store-checker-7d2c-xctrace/overlay/`、`_store-checker-7d2c-impl/` の `count/` |
| Check KPC の手順と binary の条件 | [7d-2c 実装指示書](store-checker-7d2c-implementation-instructions-20261002.md) §4、`measure_test.go` |
| inline した leaf と pprof | darwin の pprof は inline した leaf を過小に帰属するので、根拠にしない |

before は §6 の 1 で決める。開始時の HEAD と差分の hash を記録する。

## 0. 規則

- **変更してよいもの**: storeharness の test file、docs、scratch の overlay・スクリプト・binary。
- **変更しないもの**: production の source (store・storechecker・storeprinter・生成器と生成物)、Pointer の package、go.mod / go.sum。
- **試作の扱い**:
  - §2 の試作は overlay で production の file を差し替えて build する。repo の file は書き換えない。
  - 試作の採否は本段で決めない。
- **計数と時間の binary を分ける**: 計数を入れた binary で時間や KPC を測らない。§2 の KPC は、計数を入れない試作の overlay だけで build する。
- **sudo と許可ダイアログ**:
  - KPC は root が要る。コマンドを渡し、ユーザーが別のターミナルで `tee` した file を読む。file が空なら貼り付けを頼む。
  - binary を作り直した後の最初の計測では、画面の許可ダイアログに注意するようユーザーに伝える。
- **置き場所**:
  - 一時 binary・trace は scratchpad に置く。
  - 生出力・スクリプト・コマンド・終了コード・binary の sha256・overlay の JSON は `internal/ast/docs/_store-checker-7d2c-accessor/` に置き、commit しない。
- **記録の分け方**: 仮説と確定を分けて書く。単価 × 回数の見積りは「見積り」と書き、標本か KPC で測った値と区別する。

## 1. (b) accessor の税を数える

### 1.1 回数

overlay で、Store の次の accessor の呼び出しを、**呼び出し元の関数ごと**に数える。

| 対象 | 中身 |
| --- | --- |
| 必須 | `Node.Parent`、`Node.IsNil`、`Node.Kind`、`Store.node` (辺の解決全体)、list の解決 (`Store.list` と `List.At`)、`Node.File` |
| 追加 | 7d-2c の計数で多いと分かっている多相 accessor (`Expression`・`Name`・`Text`・`Type`・`Initializer`) |

- 呼び出し元は `runtime.Caller` で取ってよい。この binary は計数専用で、時間は測らない。
  - inline された呼び出し元も、`runtime.CallersFrames` で論理的な関数名が出る。
- **仕事**: §4 と同じ。compiler プロジェクト、`checkStore`、1 回。
- **Pointer 側**: 同じ仕事で `(*Node).Parent`・`Kind` の読み・`(*Node).Text` など、対応する accessor を数える。
  - Pointer の `Kind` と `Parent` は field の読みで、関数にできないので数えられない。
  - そのときは「Store と回数が同じ」とみなし、その仮定を報告に書く。仕事量と flow の回数が一致しているので、大きく外れないと見ている。

### 1.2 単価

各 accessor の 1 回あたりの命令数を、inline された形で `go tool objdump` から読む。

- **Store**: hot な呼び出し元 3 つ以上 (`isMatchingReference`・`narrowType`・`NameResolver.Resolve` を含める) で、accessor が展開された命令列を数える。
- **Pointer**: 同じ呼び出し元の対応する命令列を数える。
- 差を 1 回あたりの命令数とする。xctrace 報告の `Parent()` + `IsNil` は Store 9・Pointer 2 で、これと照合する。
- 呼び出し元で命令数が違う場合 (spill の有無など) は、幅で報告する。

### 1.3 照合

| 照合 | 方法 |
| --- | --- |
| 回数 × 単価の差の合計 ↔ xctrace の仕組み 6 の差 (+552 M inst/回) | 両方の値と割合を出す。照合の単位は inst だけ。cycles への換算はしない (inst の増えが cycles に出ない例が 7d-0 の storeexp にある) |
| 呼び出し元の関数ごと | 上位 20 の呼び出し元で、xctrace の self の差 (`classify.md` の共通関数の表) と計数 × 単価を並べる |

- **受入**:
  - 合計で仕組み 6 の 6 割以上を説明するか、説明できない理由を書く。
  - 説明できなかった分は、上位の関数を objdump で 1 つ以上読み、何の命令が増えたかを書く。

## 2. (a) 祖先 walk の inline を戻す試作

### 2.1 試作

overlay で `ast/store` の次の 8 関数を、inline 予算 (80) 内に収める。

- `FindAncestor`・`FindAncestorOrQuit`・`GetCombinedNodeFlags`・`GetRootDeclaration`・`IsGlobalSourceFile`・`GetSymbolId`・`HasQuestionToken`・`Node.File`
- **形は自由**。ただし意味 (戻り値・callback の呼ぶ順と回数・panic の条件) を変えない。候補:
  - foreign の枝 (`Store.node` の `foreignBit` の経路と panic) を、inline されない関数に出す。
  - 親ループの終了判定を、入口で 1 回だけ `IsNil` にし、ループの中は parent の辺の値で判定する ([Z1 §6 の ancestors helper](store-node-zero-design-20260926.md) の形)。
- **inline の確認**:
  - `-gcflags='-m=2'` を `./internal/ast/store` だけにかけ、8 関数が `can inline` になったこと (cost も) を記録する。
  - storechecker の `-m=2` は 8 GB 機で終わらないので使わない。
  - 呼び出し元で inline されたかは、xctrace 報告 §4.1 の callback (`getControlFlowContainer.func1` ほか) が、§2.2 の binary の objdump で直接呼び出しか inline になったかで見る。
- **一部しか戻らない場合**: 戻らない関数があれば、その関数と cost を書いて進む。全部を戻すことにこだわらない。

### 2.2 等価

試作の overlay で次の 3 つを通す。

- 段 1 strict の `TestCheckerCorpus`
- `TestCompilerProjectEquivalence`
- `go test ./internal/ast/store/...` (両 tag)

段 2 は回さない (§6 の 3)。

### 2.3 計測

- **KPC (ゲートの条件)**:
  - 同じ試作の overlay で build した kperf binary で、`BenchmarkCheckKPCStore` と `BenchmarkCheckKPCPointer` を交互に 5 run。
  - 比較の相手は、before の binary (overlay なし) の同じ 2 本を同じ session で交互に 5 run。
  - スクリプトは `kpc-check.sh` を元に、before と試作の 2 binary を交互に回す形にする。
- **報告する値**:
  - Store の cycles と inst の差 (試作 − before)、と Check の cycles 比 (Store / Pointer) の変化。
  - Pointer 同士の A/A (before と試作の binary で Pointer は同じコードなので、その差は A/A になる)。
- **Walk と Bind の KPC**:
  - 試作は `Parent`・`IsNil` の周りを変えうるので、既存の Walk / Bind の KPC (7d-0 の線: Walk ≤ 1.15、Bind ≤ 1.10) を取る。
    - Store 側は `BenchmarkStoreWalkKPCV1` (ast/store) と `BenchmarkStoreBindKPCV1` (storebinder) を、before と試作の overlay で build した 2 binary で 3 run ずつ。
    - 比の相手は Pointer の `BenchmarkASTWalkKPCV1` (ast) と `BenchmarkASTBindKPCV1` (binder) で、package が違うので別 binary になる。7d-0 と同じ組み方で取る。
  - 線を外れたら、そのことを書く。
- **xctrace は回さない**。KPC の差で (a) の大きさを確定し、仕組み 3 の上限 +147 M と並べる。

### 2.4 (a) と (b) の重なり

- 試作の形によっては、親ループの判定を変えることで (b) の `Parent`・`IsNil` の命令も減る。
- §1 の計数で試作の前後の命令数を objdump で比べ、KPC の差のうち (b) の分 (accessor の命令の減り) と (a) の分 (呼び出しと callback の除去) を見積りで分けて書く。

## 3. 受入

- **§1**: 回数と単価が両側でそろい、仕組み 6 との照合がある。
- **§2**:
  - 等価が通る。
  - before / 試作の KPC が 5 run ずつそろい、A/A (Pointer) の cycles の幅が 2% 以内。
  - 外れたら run を足すか、外れたまま理由を書く。
- **結論の表**: 次の 4 つの値を同じ単位 (Check の cycles 比、測ったままの基準) で並べる。
  - A・C の確定値 (xctrace 報告 §6)
  - (a) の実測 (§2 の KPC)
  - (b) の見積り (§1)
  - それらの組み合わせで線 1.10 に届くか
- **報告**: `store-checker-7d2c-accessor-tax-report-20261002.md` に、変更 (overlay の file と内容)、§1・§2 の値、仮説と確定の区別、次の改善段の候補と受入条件を書く。

## 4. 報告に含めること (xctrace 報告の訂正)

xctrace 報告の検算で分かった次の 4 点を、この報告の冒頭に「xctrace 報告の訂正」として書く。xctrace 報告そのものは書き換えない (§6 の 2)。

1. 「A だけで比 1.101」は C を差し引いた基準の値。測ったままのゲートでは A 単独 1.123、A + C 1.1014 で、どちらも線の外。
2. 受入の「8 割以上を説明」は、分類の合計が差の全部なので自明に通る。adapter を除いた checker 自身の差 (約 320 M cycles) のうち、名前の付いた仕組み (1・3・4・5) は約 198 M (62%) で、3 は上限。
3. A/A を外した pointer-2 は、E-core の sample が 1.6% (ほかの run は 0〜0.2%) と補完 14% が重なった run。
4. 両側の仕事量と flow の回数は一致する (背景の表)。`getFlowTypeOfReferenceEx` だけは Store が 494 回少ない (250,336 / 249,842)。費用には効かないが、理由は未確認として残す。

## 5. 時間の目安

| 作業 | 目安 |
| --- | --- |
| §1 の計数 | 1 プロセス数分 |
| §2.2 の等価 | 約 15 分 |
| §2.3 の KPC | Check 2 binary × 2 side × 5 run で 20〜40 分。Walk / Bind が別にかかる |

## 6. 決めること (着手前にユーザーへ確認)

1. **before**: xctrace 報告と `xctrace_test.go` (未コミット) を先に commit し、その hash を before にするか。推奨は commit すること。§2 の比較相手の binary を source と対応させるため。
2. **xctrace 報告の訂正の置き場所**: 推奨はこの段の報告に書くこと。xctrace 報告そのものを直す案もあるが、測定の記録と後の読み直しを分けておく方が追いやすい。
3. **§2 の等価の範囲**: 推奨は段 1 strict と compiler プロジェクトまで。試作は overlay で採否も決めないので、段 2 は採用の段で回す。
