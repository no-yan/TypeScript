# Store checker 基盤 (TODO 7d-0) 検証指示

作成日: 2026-09-24。対象は検証を担当するエージェント。何を作ったかは [store-checker-7d0-implementation-instructions-20260924.md](store-checker-7d0-implementation-instructions-20260924.md) (以下「実装指示書」)。設計は [store-checker-design-20260923.md](store-checker-design-20260923.md) (以下「7d 設計」)。

目的は 4 つ。**(1) parse / bind の結果が 7c と同じまま**、**(2) 合成ノードの storage の契約 (foreign の辺、所有者、容量の境界) が成り立つ**、**(3) FR1 (辺の foreign 解決) と V1 (所有者を運ぶ解決層) と D2 (宣言を `Node` で持つ) の費用を Walk / Parse / Bind の KPC と retained / GC で測り、既存の線を満たすか**、**(4) 変換器の自動化率**。

## 0. 規則

7c の検証指示 §0 と同じ。実装を直して数字を良くしない。直してよいのは正しさのバグだけで、直したら報告に書く。生の出力は `tsc/internal/ast/docs/_store-checker-7d0-results/` に保存する。コマンドは絶対パス (`ROOT=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、`TSC=$ROOT/tsc`、`OUT=$TSC/internal/ast/docs/_store-checker-7d0-results`)。環境 (go version、commit、load average) を記録する。

**計測前に source の sha256 を取り、終了時に不変を確認する** (`internal/ast/store/**/*.go`、`internal/storeparser/*.go`、`internal/storebinder/*.go`、`generate-go-store.ts`、`tools/cmd/storetranslate/*.go`)。

KPC は root が要るのでユーザーに依頼する (§4)。ns / allocs / retained / gctrace は自分で取る。

## 1. 正しさ

```sh
cd $ROOT && node tools/scripts/tsc/generate.ts && (cd tools && go run ./cmd/storetranslate) && git status --short
cd $TSC && go build ./... && go vet ./internal/ast/... ./internal/storeparser/... ./internal/storebinder/...
cd $ROOT/tools && go vet ./cmd/storetranslate/...
cd $TSC && git diff --stat <7d-0 の直前の commit> -- internal/ast internal/binder internal/parser internal/checker internal/scanner go.mod ':!internal/ast/store' ':!internal/ast/docs'   # 空
cd $TSC && go test -count=1 ./internal/ast/store/... ./internal/storeparser/... ./internal/storebinder/...
cd $TSC && go test -tags storechecks -count=1 ./internal/ast/store/... ./internal/storeparser/... ./internal/storebinder/...
cd $TSC && grep -rl '"github.com/microsoft/TypeScript/tsc/internal/\(ast/store\|storeparser\|storebinder\)' --include='*.go' . | grep -v 'internal/ast/store/\|internal/storeparser/\|internal/storebinder/'   # 空
cd $TSC && go test -count=1 ./internal/ast/ ./internal/parser/ ./internal/binder/
```

確認すること:

1. 実装指示書 §1 のガード。生成物が 2 つの生成器で再現すること。
2. **parse / bind の等価**: 7b / 7c の corpus テストが同じ入力 (17,415 file) で mismatch 0、除外は 7c と同じ 330 file と JS の緩め 2 件で、増えていないこと (理由ごとの file 番号を 7c の `test-notag.txt` と diff する)。`Declarations` の Store の検査 (旧 `Ref.file` の検査、実装指示書 §4 の 4) の件数が 7c の 708,382 と同じ桁であること。
3. **storage の操作列** (実装指示書 §6.2): 17 項目が全部あり、容量に依存する操作列が容量 1 / 2 / 3 / 大の 4 通りで回っていることをテストのコードで確かめる。**項目が抜けていれば pass は意味が無い。** とくに次を 1 つずつ読む: (1) source の子の `Parent()` が変わらないことを実際に比べているか、(3) 容量 1 で境界を必ずまたいでいるか (StoreID が変わっていることを assert しているか)、(5) `storeChecks` の panic のテストが tag 付きの build で走っているか、(7) 印の無い範囲外の値の panic が **tag なしの build** で走っているか。
4. **K3 の実装コメントと両方の拒否**: `CloneNodeList` と `SetListElement` の検査箇所に、実装指示書 §3.6 の趣旨のコメントがあること (無ければ差し戻す)。検査が Clone の先だけでなく元への書き込みも拒否すること (実装指示書 §6.2 の 5 のテストが両方の panic を確かめているか読む。先だけなら差し戻す)。
5. **writer の所有者の検査**: source の Store と別 region への書き込みが tag なしで panic すること。
6. **大きさ**: 一時テストで `unsafe.Sizeof` を確かめる (`NodeHeader` 32、`Node` 16、`Flow` 16、`FlowNode` 16)。`Symbol` は記録するだけ (見込み 112、固定の条件にしない)。`gid` が 64bit であること。
9. **flow graph の独立な比較** (実装指示書 §6.2 の 12): Store 側が V1 の API だけで辿っていること (`Bound` の内部表を読んでいないこと) をコードで確かめる。2 つの Bound で同じ数値の `FlowRef` を使うケースがあること。
10. **取り込みの境界** (§6.2 の 13): 別の program の同じ数値の StoreID、別の region の合成ノードが、`New` / `SetParent` / `SetListElement` の全部で tag なしの build で拒否されること。
11. **hook の再入と `extra` / `texts` の再確保** (§6.2 の 14、15): テストが実際に再入と再確保を起こしていること (cap や StoreID の変化を assert しているか)。
7. `unsafe` が `Node.Ref()` と `List` の raw 経路以外に無いこと。`Ref` / `FileRef` が store に残っていないこと。
8. Store から Factory / NodeRegion / checker への逆参照と、Store の全件表が無いこと (`store` の型定義を読む)。

12. **private 名と gid**: 実装指示書 §4 の変更と §6.2 の17を確認する。local SymbolId が同じ別 file の class を含め、保存と lookup の raw key が一致し、別 class の同名 private が混同されないこと。既存の `normalizeName` による数字除去をこのテストに使わない。gid の安定性・並列初回採番・非零一意性も確認する。Pointer と Store の番号そのものの一致は期待しない。

## 2. 移植の機械性と変換器

```sh
cd $TSC && git diff <7d-0 の直前の commit> -- internal/storebinder internal/storeparser > $OUT/binder-parser.diff
cd $TSC && git show <7d-0 の直前の commit>:tsc/internal/ast/store/utilities.go > $OUT/utilities-7c.go
cd $TSC && cp internal/ast/store/utilities.go $OUT/utilities-7d0.go
```

- `binder-parser.diff` の hunk を全部読み、実装指示書 §4 の 5 項目 (StoreID、id 層の名前、D2、等価テスト、`Bound.store`) 以外が無いこと。
- **変換器の binder.go の試作**: 実装報告の一致率を、7c の hunk の分類 (a)〜(d) ごとに数え直す。比較の前の名前の正規化表を読み、正規化が名前の対応だけ (構造を書き換えていない) であること。**線 (案): (a)〜(d) 相当の hunk の 9 割以上が一致。** 満たさなければ不合格ではなく「7d-2 の前に規則を足す」項目として、規則にならなかった箇所の一覧と一緒に報告し、ユーザーに判断を求める。
- **`store/utilities.go` の出し直し**: 7c の手書き版と関数単位で並べて diff する (7c 検証の `funcdiff.py` の手順)。関数の集合、順序、分岐が同じで、違いが実装指示書 §5.1 の規則の適用だけであること。P2 の関数 (`utilities_manual.go`) は 1 件ごとに hash の注記があり、実装報告の理由と合うこと。7c の (f) / (g) の 1 件ごとの扱い (規則 / P2 / 逐語で足りた) を報告と照合する。
- 変換器の hash 照合: `utilities_manual.go` の hash を 1 つ書き換えて変換器が止まることを一時的に確かめ、戻す。
- **Z1 の generic helper**: 実装指示書 §5.1 の Node 版への付け替えを、関数の同一性と実際の型引数で検査する。FirstNonNil / MapNonNil / OrElse は IsNil、SameMap / SameMapIndex / Deduplicate / AppendIfUnique は SameNode を使うこと。零値と複数 Store の番兵を混ぜた入力で意味を比べ、callback 回数・順序、OrElse の eager 評価、slice の再利用が変わっていないこと。Node 以外の呼び出しは維持する。未対応の comparable helper を interface 化で通していないこと。
- **Z1 の複合 key**: `NonExistentPropertyKey.propNode` と `accessibleChainCacheKey.location` が NodeKey になり、全構築・代入が Key()（nil literal は NodeKey{}）になっていること。map / Set の結果が nil の表現に依存しないこと。Node として読み戻す用途を持つ反例では、黙って field を置換せず未対応として報告する変換テストがあること。

### 正しさと性能の判定を分ける

inline、`Symbol` の大きさ、KPC の線は性能の判定。正しさ (§1) が通って性能が線を外したら「性能未達」として実装と数字を残す。安全の検査や共有の意味を弱めて線を通す変更が入っていたら差し戻す。

## 3. API と命令列

```sh
cd $TSC && go build -gcflags='-m=2' ./internal/ast/store/ > $OUT/inline-store-full.txt 2>&1
rg 'can inline|cannot inline' $OUT/inline-store-full.txt > $OUT/inline-store.txt
cd $TSC && go build -gcflags='github.com/microsoft/TypeScript/tsc/internal/...=-m=2' ./internal/storeparser/ ./internal/storebinder/ > $OUT/inline-callers-full.txt 2>&1
cd $TSC && go build -gcflags='-d=ssa/check_bce/debug=1' ./internal/ast/store/ 2>&1 | grep -i 'generated' > $OUT/bce-store.txt
cd $TSC && go test -c -o $OUT/store.test ./internal/ast/store/
cd $TSC && go tool objdump -s 'store\.\(\*Store\)\.node$|store\.Node\.Parent$|store\.Node\.FlowNode$|store\.Node\.Symbol$|store\.List\.At$' $OUT/store.test > $OUT/asm-accessors.txt
cd $TSC && go test -c -o $OUT/storebinder.test ./internal/storebinder/
cd $TSC && go tool objdump -s 'storebinder\.\(\*Binder\)\.bind$' $OUT/storebinder.test > $OUT/asm-bind.txt
```

| 対象 | 期待 |
| --- | --- |
| `node()`、`Parent()`、生成の子 accessor、`List.At` | can inline (7d 設計 §2.6 の試作では `node()` 25、`Parent()` 32、子 40)。**cannot なら理由を cost で示す** (遅い側が関数呼び出しになっていないか) |
| `node()` の速い側 | 比較 1 回と分岐 1 回 (今の bounds check と同じ数)。foreign の側は分岐の先で、bit 検査と foreign 表の bounds check がある。7d 設計 §2.6 の試作の +2 命令 (レジスタのコピーと `JMP`) と比べ、増えていれば理由 |
| `bind` の中の `node()` の形 | 7c 報告 §5 と同じ場所 (子の辺と親 1 段) だけ。増えていないこと |
| `FlowNode()` / `Symbol()` / `Locals()` | 分岐 (`s.bound != nil`) 1 つ。can inline |
| `Node.Ref()` | 7c と同じく shift (`LSR $5`)、乗算なし |
| 7c の accessor の inline 判定 | 7c の `inline-store.txt` と比べ、cannot が増えていないこと。増えたものを列挙 |
| Z1 の `IsNil` と生成 accessor / Builder・Factory ctor | before / after の can / cannot inline 件数、cost、変化した関数名と理由を記録する。IsNil の cost 6 → 11 が caller の予算を越えないか検査する |
| 実際の call site | `inline-callers-full.txt` の inlining call 診断と対応箇所の objdump を突き合わせる。定義の can inline だけで完了にしない |

inline の before / after は KPC と同じ source hash・Go version・tag 条件で取り、通常 build と kperf build で差があれば両方を残す。生成 accessor / Builder ctor / Factory ctor の母数と件数を分け、追加・削除による件数変化を inline の退行と混同しない。新設 Factory ctor に before がなければ明記する。過去の Builder ctor のように予算80の境界を越えるケースは、呼び出し先の cost 増加まで追う。inline されなかった関数は KPC の inst / cycles の差を切り分ける対象に含める。

## 4. KPC: before / after

**before** は 7d-0 の変更前の commit で取る。ユーザーが別ターミナルで (load average < 1.5 を待ってから):

```sh
# before (7d-0 の直前の commit を checkout した状態)
cd $TSC && go test -tags kperf -c -o /tmp/7d0-before/store.test ./internal/ast/store/ && go test -tags kperf -c -o /tmp/7d0-before/storeparser.test ./internal/storeparser/ && go test -tags kperf -c -o /tmp/7d0-before/storebinder.test ./internal/storebinder/
cd $TSC/internal/ast/store && sudo /tmp/7d0-before/store.test -test.run '^$' -test.bench 'StoreWalkKPCV1' -test.benchtime 20x -test.count 5 | tee $TSC/internal/ast/docs/_kpc-baselines/store-7d0-before-$(date +%Y%m%d).txt
cd $TSC/internal/storeparser && sudo /tmp/7d0-before/storeparser.test -test.run '^$' -test.bench 'StoreParseKPCV1' -test.benchtime 20x -test.count 5 | tee -a $TSC/internal/ast/docs/_kpc-baselines/store-7d0-before-$(date +%Y%m%d).txt
cd $TSC/internal/storebinder && sudo /tmp/7d0-before/storebinder.test -test.run '^$' -test.bench 'StoreBindKPCV1' -test.benchtime 20x -test.count 5 | tee -a $TSC/internal/ast/docs/_kpc-baselines/store-7d0-before-$(date +%Y%m%d).txt
# after (7d-0 の binary)。同じセッションで before の binary をもう一度走らせてから:
cd $TSC && go test -tags kperf -c -o $OUT/store.kperf.test ./internal/ast/store/ && go test -tags kperf -c -o $OUT/storeparser.kperf.test ./internal/storeparser/ && go test -tags kperf -c -o $OUT/storebinder.kperf.test ./internal/storebinder/
cd $TSC/internal/ast/store && sudo $OUT/store.kperf.test -test.run '^$' -test.bench 'StoreWalkKPCV1' -test.benchtime 20x -test.count 5 | tee $OUT/kpc-walk-$(date +%Y%m%d-%H%M).txt
cd $TSC/internal/storeparser && sudo $OUT/storeparser.kperf.test -test.run '^$' -test.bench 'StoreParseKPCV1' -test.benchtime 20x -test.count 5 | tee $OUT/kpc-parse-$(date +%Y%m%d-%H%M).txt
cd $TSC/internal/storebinder && sudo $OUT/storebinder.kperf.test -test.run '^$' -test.bench 'StoreBindKPCV1' -test.benchtime 20x -test.count 5 | tee $OUT/kpc-bind-$(date +%Y%m%d-%H%M).txt
```

before の Parse の dom の sub-benchmark は 7d-0 で足すので before には無い。after の値を 7d-1 の before として記録する。

判定は**同じ binary の pointer に対する比**で行い、before の比と並べる:

| 指標 | 線 | 7c の値 (Compact 後) |
| --- | --- | --- |
| Walk cycles/visit、store / pointer | ≤ 1.15 (ゲート 1) | 1.137 |
| Parse cycles/op、store / pointer (checker.ts) | ≤ 1.00 | 0.824 |
| **Bind cycles/node、store / pointer** | **≤ 1.10 (ゲート 2)** | **0.984** |
| 3 本の inst と IPC、before からの差 | 報告。**FR1 と V1 の費用はこの差** | |

線を超えたら次の順に切り分ける: (1) `node()` / `Parent()` / 子 accessor と生成 ctor が inline されているか (§3)、(2) `node()` の速い側と Z1 の nil 終了判定のある親ループの命令数、(3) V1 の分岐 (`FlowNode()` / `Symbol()` / `Locals()`。bind は id 層を使うはずなので Bind には出ないはず)、(4) D2 (`Declarations` が 8B → 16B、`Symbol` 96 → 112B の見込み。実測値を記録) の B/op と allocs。どれで説明できるかを報告に書く。

Z1 の性能判断もこの GC オフの KPC inst / cycles に含める。旧 Store 比は変更の費用を分ける補助であり、同一 binary の Pointer 比を省略しない。7d-0 の結果で後段の Check cycles ≤ 1.10 × Pointer を代替しない。親ループが寄与した場合の切り分け候補は 7d 設計 §2.7 と Z1 §6 の ancestors helper。まず反復数と最終命令列を確認し、安全の不変条件を維持して比較する。Z1 の高負荷・通常 GC の ns 記録は採用根拠にしない。

自分で取るもの (参考、load average を記録):

```sh
cd $TSC && go test -run '^$' -bench 'StoreWalkV1|StoreParseV1|StoreBindV1' -benchtime 2s -count 5 ./internal/ast/store/ ./internal/storeparser/ ./internal/storebinder/ | tee $OUT/ns.txt
```

## 5. retained heap と GC (D2)

**variant ごとに別の新しいプロセスで取る。** 下の retained コマンドは pointer / store を個別指定して実行する。各プロセスが対象 variant の AST だけを構築していることをベンチの構築経路と live heap profile で監査する。等価テストを先に同じプロセスで走らせない。共通の text / fixture の baseline と対象 root の KeepAlive を揃え、両 AST を保持したまま差し引く方式を使わない。詳しい契約は [レビュー追加契約 §3](store-checker-review-contracts-20260926.md)。

```sh
cd $TSC && go test -c -o $OUT/storebinder.test ./internal/storebinder/
# 以下を交互に各5回、独立プロセスで実行し、run別ファイルへ保存する。
cd $TSC/internal/storebinder && $OUT/storebinder.test -test.run '^$' -test.bench '^BenchmarkStoreBindRetainedV1$/^checker.ts$/^pointer$' -test.benchtime 1x -test.count 1 > $OUT/retained-checker-pointer-run1.txt
cd $TSC/internal/storebinder && $OUT/storebinder.test -test.run '^$' -test.bench '^BenchmarkStoreBindRetainedV1$/^checker.ts$/^store$' -test.benchtime 1x -test.count 1 > $OUT/retained-checker-store-run1.txt
cd $TSC/internal/storebinder && GODEBUG=gctrace=1 $OUT/storebinder.test -test.run '^$' -test.bench 'StoreBindRetainedV1/checker.ts/store' -test.benchtime 1x -test.count 1 2> $OUT/gctrace-checker-store.txt
cd $TSC/internal/storebinder && GODEBUG=gctrace=1 $OUT/storebinder.test -test.run '^$' -test.bench 'StoreBindRetainedV1/fixtures/store' -test.benchtime 1x -test.count 1 2> $OUT/gctrace-fixtures-store.txt
```

gctrace はテストバイナリを直接実行する。retained は GC 2 回の後の値で、既存ベンチのその次の forced GC が保持中の GC 測定に対応する。Pointer 側も選択 checkout で取り直す。

checker.ts 以外の fixtures / dom も上と同じく個別プロセスで保存する。gctrace は Pointer 側も同じ入力・GC回数で取り、最後の対象 forced GC の mark CPU を比較する。ns/op・B/op・allocs/op と retained / scan bytes は別指標として報告し、raw を benchstat で比較する。過去の値は checkout / compiler が違えば `stale` とし、同じになるとの仮定で採用しない。

7d-2 の check 後の計測では、H1 の Pointer program を保持する adapter は不可。値だけの fixture から Store adapter を構築し、比較用 Pointer source を持ち込まない。印字に実際に必要な一時 Pointer ノードの費用は含める。準備プロセス・fixture・live root・print adapter の監査と KPC / GC の実行分離をレビュー追加契約 §3に従って引き継ぐ。

| 指標 | 線 | 7c の値 | 7d 設計の見積り |
| --- | --- | --- | --- |
| retained B/node、store / pointer (parse + bind) | checker.ts ≤ 0.60、fixtures ≤ 0.70 | 0.561 / 0.583 | 宣言 1 つにつき +8B (`Ref` 8B → `Node` 16B)、`Symbol` +16B (96 → 112B)。checker.ts で約 +0.5〜1 B/node |
| GC の mark CPU、store / pointer | checker.ts ≤ 0.25、fixtures ≤ 0.35 | 0.10 / 0.16 | 宣言 1 つにつき pointer +2 本。checker.ts で 0.10 → 0.13 前後 (未測定の見積り) |

**7d 設計 §2.7 の check の後の線 (retained ≤ 0.85、mark CPU ≤ 0.75) の前提** (parse + bind の比が compiler プロジェクトでも 0.56 前後) を、ここで compiler プロジェクトの parse + bind について測る: `BenchmarkStoreBindRetainedV1` の入力に compiler プロジェクトの 124 file があれば使い、無ければ一時テストで 7c の retained の手順を compiler プロジェクトに当てる。比が 0.70 を超えていたら、7d-2 の線を直す必要があるとして報告する。

## 6. レポート

`tsc/internal/ast/docs/store-checker-7d0-verification-report-<YYYYMMDD>.md` に、既存の文書と同じ文体で:

1. 結論 (合否と数字。§4 / §5 の表を埋めたもの)
2. 環境と入力
3. 正しさ (§1。parse / bind の等価の範囲と除外、storage の操作列の項目と容量ごとの結果)
4. 移植の機械性と変換器 (§2。binder.go の試作の一致率、`store/utilities.go` の出し直しの差分と P2 の一覧)
5. API と命令列 (§3。生成 accessor / Builder・Factory ctor の inline 件数、cost、変化した関数名、実際の call site を含む)
6. 費用 (§4。before / after の 3 本と、FR1 / V1 / D2 の帰属)
7. retained heap と GC (§5。7d-2 の線の前提の確認を含む)
8. 7d 設計への反映案 (FR1 の hot path の実測、V1 の分岐、D2 の retained、変換器の自動化率、7d-2 の線を直す必要の有無)
9. 限界

10. [レビュー追加契約 §5](store-checker-review-contracts-20260926.md) の段別対応状況。7d-0 の private 名・gid と隔離メモリ計測を報告し、checker の symbol 対応・fallback 順序、Pointer AST を持たない H1、印字全区間の KPC / allocation / retained は7d-2の未完了条件として残す。Check が合格でも印字性能を検証済みとしない。
11. 再現手順と生データの場所

合格なら次は 7d-1 (JSDoc)。不合格でも §4 / §5 の数字は設計の賭けの実測なので、合否に関わらずレポートに残す。
