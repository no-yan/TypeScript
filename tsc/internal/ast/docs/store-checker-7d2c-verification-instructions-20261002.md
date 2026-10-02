# Store checker の性能 (TODO 7d-2c) 検証指示書

作成日: 2026-10-02。対象は検証を担当するエージェント。

[実装指示書](store-checker-7d2c-implementation-instructions-20261002.md) の測定を独立に検証する。before は **9c93098cc3**。

- 実装報告の数字を写すのではなく、**測定の条件と経路が契約どおりか**を確かめる。
- ゲートの値は、自分で取り直した値で判定する。
- 改善を提案しても実装はしない。

## 0. 規則と証拠

- 先に読むもの: [7d 設計](store-checker-design-20260923.md) §2.7、[レビュー契約](store-checker-review-contracts-20260926.md) §3・§4、実装報告、生出力。
- 実装報告にあることと、検証で再現したことを区別して書く。
- source の sha256 を開始時と終了時に保存する。対象は tsc / tools の追跡・未追跡の `.go`・`.ts`・`.mod`・`.sum`・`.json`。一致しなければならない。
- 記録するもの: HEAD、before との差分、未コミット差分の hash、Go の版、環境変数、コマンド、終了コード。commit しない。
- 一時 binary・overlay・probe は scratchpad に置く。結果は `internal/ast/docs/_store-checker-7d2c-results/` に置く。
- KPC は root が要るので、ユーザーに別のターミナルで `sudo … | tee` を依頼し、そのファイルを読む。

## 1. 範囲

- 変更が実装指示書 §0 の範囲 (storeharness・計測用の test file・docs) だけであること。
- 不合格になる変更: storechecker・storeprinter・store の production、生成器と生成物、Pointer の package、go.mod / go.sum。
- instrumentation が追跡する変更に入っていないこと。ゲートの binary に計数のコードが入っていないこと。ゲートの binary は build の入力 (source の hash と tag) から確かめる。

## 2. 前提の等価

- 実装の §2 (compiler プロジェクトの等価) を自分で再実行し、equal であることを確かめる。
- 印字の要求列の出力一致も再実行する。
- 比較器が感度を持つことを、scratch overlay で確かめる。Store の記録の 1 つと、印字の出力の 1 文字を変えると、どちらも失敗すること。

## 3. 隔離の監査 (契約 §3)

### 3.1 fixture と adapter を読む

- fixture の型を再帰的に読み、`*ast.*`・`compiler.Program`・`ParsedCommandLine` の AST・Pointer の host / cache への参照が無いことを確かめる。
- 測定用 adapter の全 method が、fixture と Store file だけから答えることを確かめる。

### 3.2 検出感度

scratch overlay で 1 つずつ壊し、検出されることを確かめる。

| # | 故障 | 必須の検出 |
| --- | --- | --- |
| 1 | adapter の 1 つの method を Pointer program への委譲に戻す | 構築経路の監査、または live heap profile の Pointer の型 |
| 2 | 索引に無い syntax 依存の問い合わせに既定値を返す | fixture の完全性の試験 (同一性の試験で差が出る、または panic) |
| 3 | 合成 import を fixture から作らない (nil を返す) | 同一性の試験 |
| 4 | fixture の解決の 1 件を落とす | 同一性の試験、または panic |

### 3.3 同一性の試験を再実行する

測定用 adapter と H1 adapter で同じ記録になることを、自分で compiler プロジェクトと段 2 の sample (実装と別の seed) で確かめる。

### 3.4 live heap profile

Store の測定プロセスの profile を自分で取り、次のことを確かめる。

- `*ast.Node`・`*ast.SourceFile`・`compiler.Program` の割り当て元が無いこと。
- あれば、生成元・数量・寿命を調べる。印字が実際に作る一時ノードかどうかを区別する。

## 4. Check KPC の再測定

- **binary**: 実装と同じ source から、自分で kperf の binary を作る。build の入力の hash を記録する。
- **実行**: Pointer と Store を各 5 run、ユーザーに依頼する。
- **区間**: 区間の中身が実装指示書 §4 と §9 の決定 (suggestion を含めない、測定用 adapter) どおりであることを、ベンチのコードで読む。
  - parse・bind・fixture の読み込みが区間の外にあること。
  - Pointer と Store が同じ仕事 (同じ file の列、同じ API) をすること。
- **判定**: cycles の比が ≤ 1.10 か。inst・IPC・node あたりの値、A/A の幅を報告する。
- **照合**: 実装の値との差が A/A の幅を超えれば、原因を調べる。
- **旧基準値**: `BenchmarkASTCheckKPCV1` も取り直し、program 経由の値として並べる。

## 5. retained / GC の再測定

- 別の子プロセスで、Pointer と Store を交互に各 3 run 以上取る。
- 確かめること:
  - 共通の baseline と GC 2 回の手順。
  - root が `KeepAlive` されていること。
  - sync.Pool の条件が揃っていること。
- **判定**: retained の比 ≤ 0.85、mark CPU の比 ≤ 0.75。値は、最後の強制 GC の gctrace の行から自分で読む。
- **内訳**: 内訳の和が全体と合うことを照合する。adapter / fixture の分が入っていることも確かめる。
- **混在の禁止**: GC オフの KPC の値と混ぜていないこと。比較用の Pointer を保持した混在ハーネスの値で代用していないこと。条件を満たす artifact が無ければ `missing` とする。

## 6. 印字の全区間

- **区間の範囲**: nodebuilder + storeprinter + 文字列取得が区間の中にあることを、ベンチのコードで読む。
  - 取得済みの文字列を返すだけになっていないこと。
  - 印字の変換・cache の構築を区間の外に置いていないこと。
- **cache の状態**: cache miss と cache hit の両方が、それぞれ正しい状態で測られていること。
  - miss: 新しい checker。
  - hit: 同じ checker で同じ列を再実行。
- **manifest**: 要求列が Pointer と Store で同じであること (manifest の hash と件数)。
- **再測定**: KPC・ns/op・B/op・allocs/op を自分で取り直す。benchstat の結果を保存する。
- **内訳**: 内訳の instrumented run が、全区間の数字に混ざっていないこと。

## 7. 判断材料の計数

- 合成の Store の数と使用率、R4' の確保数と bytes を、自分の overlay で 1 回数え直す。
- 実装の値と一致すること。一致しなければ、計数の位置の違いを調べる。
- `getEffectiveCallArguments` の結果が resolveCall の外へ出る経路があるかを、自分でコードを読んで答える。

## 8. 回帰

実装指示書 §8 の回帰を再実行する。

- 段 1 strict、段 2 strict、`TestModeCorpus`、両 tag の一式。既知の失敗は `FuzzEquivalence/a708303f611b95c8` だけ。
- build と vet が通ること。
- 生成器 2 つを回しても生成物が不変であること。
- 終了時の source hash が開始時と一致すること。

## 9. 検証報告と判定

`store-checker-7d2c-verification-report-20261002.md` を作る。

含めるもの: 全コマンドと終了コード、成果物の場所、隔離の監査、感度検査、3 つのゲートの再測定、印字、計数、回帰、source の不変。

判定は分けて書く。

- **2c の測定の正しさ**: 次の 4 つがそろったか。
  - 契約どおりの条件で測ったか。
  - 隔離が成り立つか。
  - 比較器と監査に感度があるか。
  - 再測定が実装の値と幅の内で一致するか。
- **各ゲートの合否**: Check cycles・retained・mark CPU。印字は線が無いので、退行の有無と大きさだけ。

残件には、問題・証拠・再現・仮説・修正案・受入条件を付ける。改善の実装には進まない。
