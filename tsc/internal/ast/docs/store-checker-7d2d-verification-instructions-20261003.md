# 7e の前段 (TODO 7d-2d) 検証指示書

作成日: 2026-10-03。対象は検証を担当するエージェント。

[実装指示書](store-checker-7d2d-implementation-instructions-20261003.md) の変更と測定を、独立に検証する。

- **before**: 実装の開始時の HEAD (実装報告に記録されたもの)。
- **after**: 検証の開始時の HEAD。

原則:

- 実装報告の数字を写さない。測定の条件と経路が契約どおりかを確かめる。
- 比較と受入の値は、自分で取り直した値で判定する。
- 改善を提案しても、実装はしない。

## 0. 規則と証拠

- **先に読むもの**:
  - 実装指示書と実装報告、その生出力 (`_store-checker-7d2d/`)
  - [7d-2c 検証報告](store-checker-7d2c-verification-report-20261002.md) §3.2・§3.5・§5.1・§7・§10
  - [レビュー契約](store-checker-review-contracts-20260926.md) §3・§4
- **source の hash**: 開始時と終了時に保存し、一致を確かめる。対象は tsc / tools の追跡・未追跡の `.go`・`.ts`・`.mod`・`.sum`・`.json` (結果の置き場 `internal/ast/docs/_*` を除く)。
- **記録するもの**: HEAD、before との差分、未コミット差分の hash、Go の版、環境変数、コマンド、終了コード。commit しない。
- **binary**: repo の tree から build し、違いは overlay で入れる。`git archive` で別の path に出した tree からは build しない (7d-2c 検証の残件 1)。
- **一時物**: scratchpad に置き、作り直すスクリプトと sha256 を結果の置き場に残す。build は `GOWORK=off`。
- **結果の置き場**: `internal/ast/docs/_store-checker-7d2d-results/`。
- **KPC**: root が要る。ユーザーに別のターミナルで `sudo … | tee` を依頼し、その file を読む。binary を作り直した後の最初の計測では、許可ダイアログに注意するよう伝える。

## 1. 範囲

- before から after までの変更を commit ごとに読み、実装指示書 §0 の範囲と照合する。
- **不合格になる変更**:
  - 生成物を手で直したもの
  - checker の Factory (`storechecker/region.go` の容量、`NewChecker` の Factory) の振る舞いを変えたもの
  - Pointer の package、go.mod / go.sum
- 計数の instrumentation が、追跡する変更とゲートの binary に入っていないこと。

## 2. A: 印字の Store の確保

### 2.1 案 1 の読み

直し方は案 1 (印字の Factory の Store を 16 行から倍々、上限 4,096、release で 16 に戻す) に決まっている (2026-10-03、ユーザー。実装指示書 §1.2)。

- コードを読み、次を確かめる:
  - 印字の EmitContext の Factory にだけ効き、checker の Factory には効かないこと。
  - 容量が 16・32・… と伸び、上限 4,096 で止まり、release で 16 に戻ること。`extraCap`・`textsCap` がその Store の容量に比例すること。
  - Store の行が動かないこと (不変条件 15)。
  - release の後の最初の node と、Store が満杯のときの node の両方で、正しい Store に書くこと。
  - `Factory` の hook (`OnCreate` など) の呼び方が変わらないこと。
- `ast/store` の Factory の試験に、案 1 の境目 (release の前後、容量の切り替わり、境目をまたぐ foreign の辺) を通る試験があるかを確かめる。無ければ scratch の試験で通す。

### 2.2 感度

scratch overlay で案 1 を 1 か所壊し (例: release の後も前の容量を使う、満杯の判定を 1 つずらす、上限を外す)、次のどれかで検出されることを確かめる:

- `ast/store` の試験
- `TestPrintOutputs`
- 回帰 (§5)

### 2.3 再測定

案 1 (after) と before (A の前の commit) を、repo の tree の上の 2 本の binary で取り直す。

| 計測 | 回し方 | 照合 |
| --- | --- | --- |
| 印字の ns・B/op・allocs/op | `BenchmarkPrint`、`STORE_CHECKER_PRINT_STRIDE=1000`、`-benchtime 1x -count 5`、benchstat | 実装報告の値と幅の内で一致するか |
| 印字の KPC | `BenchmarkPrintKPC`、同じ間引き、3 run | 同上 |
| Check の KPC | 実装指示書 §4 の手順 (load < 1.0、捨てる run、順を回す)、各 5 run | Check の cycles 比 ≤ 1.10 (7d-2c のゲートを保つ)。A/A ≤ 2% |
| Walk / Bind | 7d-2c 検証 §5.2 と同じ | 線内 (Walk ≤ 1.15、Bind ≤ 1.10)。Factory を変えた場合だけ必須 |
| retained (check の後・印字の後) | `TestRetained` (check と `STORE_CHECKER_RETAINED_WORK=print`)、各 3 run | check の後: retained ≤ 0.85、mark CPU ≤ 0.75。印字の後: before からの増減を書く |

- **値は自分で読む**: mark CPU は最後の強制 GC の gctrace の行から。B/op は benchstat の生出力から。
- **退行が消えたか**: 7d-2c 検証 §7 の configurations / build と診断の生成の B/op が、before からどれだけ減ったかを絶対値で書く (許容率を新設しない)。
- **合成の Store の数**: compiler プロジェクトの check で、印字の Factory の Store の数・容量ごとの数・foreign の辺の数を自分の overlay で数え、実装報告と照合する。

## 3. B: 同一性の試験の合成 specifier

- **読み**: 照合が `compareModes` (`configurations_test.go:185`) と同じ観点 (有無、2 回引いて同じ Node、形、text) を持つこと。`FixtureProgram` に記録に無い問い合わせを起こさないこと。
- **感度**: 7d-2c 検証の `mut3` を当てて、検証の sample (1,130 構成) で、合成 import を持つ 139 構成すべてが不一致になること。
- **追加の壊し方**: 2 種を scratch overlay で入れ、どちらも検出されることを確かめる。
  - specifier の text を 1 文字変える。
  - 親を file の root 以外にする。
- **誤検出なし**: `TestFixtureIdentityCompilerProject` と、段 2 の全構成の `TestFixtureIdentityConfigurations` が通ること。

## 4. C: fixture の byte

- **決定性**: after から `TestWriteCompilerProjectFixture` を自分で 3 回以上走らせ、sha256 が一致すること。実装報告の sha とも一致すること。
- **意味**: 7d-2c 検証の fixture (`42e2cf35…`、無ければ before から作り直したもの) と、module の添字を解いて全 field が同じであること (`fixture-semantic-diff.py`)。
- **感度**: 並べ替えを外した overlay で 2 回作り直し、揺れが出るか (sha が変わるか) を試す。揺れが出なくても失敗とはしない (もとの揺れは run 依存)。結果を書く。

## 5. 回帰

実装指示書 §5 の回帰を、after で再実行する。

- 段 1 strict、段 2 strict、`TestCompilerProjectEquivalence`、`TestModeCorpus`、両 tag の package の試験。既知の失敗は `FuzzEquivalence/a708303f611b95c8` だけ。
- `TestPrintOutputs` (間引きなし、全 201 target)
- build と vet (両 tag)
- 生成器 2 つの再実行で生成物が不変
- 終了時の source hash が、開始時と一致すること

## 6. 検証報告と判定

`store-checker-7d2d-verification-report-20261003.md` を作る。全コマンドと終了コード、成果物の場所を含める。

**判定は分けて書く**:

- **A**: 退行が消えたか (印字の B/op と cycles の before からの減り)。7d-2c のゲート (Check・retained・mark CPU) を保ったか。
- **B**: 感度 (139 構成すべてと追加の 2 種) と、誤検出なし。
- **C**: 決定性と意味の同一。
- **D**: A/A ≤ 2% で取れたか。
- **回帰と source の不変**

残件には、問題・証拠・再現・仮説・修正案・受入条件を付ける。改善の実装には進まない。
