# Store checker 全構成への接続と分類 (TODO 7d-2b-3) 検証指示書

作成日: 2026-10-01。対象は検証を担当するエージェント。2026-10-01 改訂: 作業指示書 §3.3 の診断対象の改訂 (`SkipDefaultLibCheck` のときの default lib を除く) に合わせて §3 を直した (ユーザー)。2026-10-02 追補: 合成 import を案 (c) で実装した (ユーザー)。§8 を足し、§1 の 1 と 7 は §8 に従う。

[作業指示書](store-checker-7d2b3-implementation-instructions-20261001.md) の接続と分類を独立に検証する。before は **c5969e29e53**。入力拡大と production の修正は分離する。本段の検証合格を「全構成等価の合格」と言い換えない。

## 0. 規則と証拠

- [7d 設計](store-checker-design-20260923.md)、[残りの設計](store-checker-7d2-design-remainder-20260929.md)、[レビュー契約](store-checker-review-contracts-20260926.md)、[2b-2 検証報告](store-checker-7d2b2-verification-report-20261001.md) を先に読む。
- 実装報告と既存の生出力を先に読む。実装報告に結果があることと、検証で再現したことを区別する。性能は測らない。
- 実装を直して equal を増やさない。ハーネスの正しさのバグだけ修正可。修正前の検出証拠を残し、修正後に影響範囲を再検証する。
- source の開始時・終了時 sha256 を保存する。対象は tsc / tools の追跡・未追跡 `.go`・`.ts`・`.mod`・`.sum`・`.json`。scratch の結果は除外する。認めたハーネス修正以外は一致しなければならない。
- HEAD、before との差分、未コミット差分 hash、Go の版、環境変数、入力 hash、コマンド、終了コードを記録する。commit しない。

```sh
ROOT=/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript
TSC=$ROOT/tsc
OUT=$TSC/internal/ast/docs/_store-checker-7d2b3-results
mkdir -p "$OUT"
```

build/test は `GOWORK=off`。一時 probe・overlay・binary は scratch に置く。長い実行はログと終了コードを回収し、timeout や途中停止を結果に混ぜない。

## 1. ガードと構築経路の読み

1. before からの source 変更が storeharness 内だけであること。Pointer package の test、Store API、P2、生成器、生成物、go.mod 等の変更は不合格。
2. 段 1 の入力、strict の既定、比較 field、除外条件が維持されていること。既知の失敗は `FuzzEquivalence/a708303f611b95c8` だけ。
3. 新しい入口が file map 全体を root にしないこと。descriptor が実効 options / roots / files / config / currentDirectory / symlink 等を保持すること。
4. 比較前に `CompileFiles` の診断・Emit 済み program を流用しないこと。fresh factory の再試行が段 1 の再構築に戻らないこと。options・cache 等の可変状態を共有しないこと。
5. Store が Pointer SourceFiles の text と parse 条件から独立 reparse すること。Pointer AST の変換、比較前の診断の並べ替え、ID の置換、Pointer printer への変換を導入していないこと。
6. content mapper の lifetime、case sensitivity、project references 等を実装報告が正確に説明していること。実装できない経路を未対応として全件一覧に残し、完全対応と書いていないこと。
7. 合成 import の除外が実際の Pointer API に基づき、未決の region 所有権を変更していないこと。

## 2. 入力一覧の独立照合

storeharness の列挙処理を oracle に使わない。scratch の testrunner package に probe を置き、元の `extractCompilerSettings`、`compilerVaryBy`、`getCompilerFileBasedTest` 等の構成列挙結果を直接得る。Go overlay / 複製で行い、元の checkout を変更しない。baseline を削除する `RunTests` を列挙目的で実行しない。

- compiler / conformance の全ファイル、全構成、構成なしの 1 回、skip と reason を比較する。構成が列挙できる option fatal は runner-rejected、列挙不能入力は別の input-error 一覧として取り落とさない。元 runner の拒否とハーネスの不備を区別し、全入力 file の処理結果も照合する。
- suite + 相対 path + 構成による同一性で照合する。表示名やファイル名だけを key にしない。case ID と index の一意性、同一入力の再列挙の安定性を確かめる。
- `*`、カンマ、boolean / enum、除外、重複除去、組合せ上限を含む fixture も用意する。
- 実装の manifest と独立一覧の差集合が空であること。固定の過去件数に合わせて追加ファイルを落とさない。
- filter / sample / shard が全一覧の index を再採番せず、選択範囲と未選択数を出すこと。全量実行では選択漏れ 0 を確かめる。

## 3. program の意味の独立照合

少数の特徴的な fixture と実コーパス代表を使い、元 runner の準備結果を scratch probe で取得する。実装と同じ写しをもう一つ書いて比較するのは禁止。runner の構築後 snapshot を oracle にするときは、診断・Emit **前**で採取する。

| 対象 | 見るもの |
| --- | --- |
| 複数 unit / config | file 順、config file、roots、otherFiles、実効 options |
| 最後の unit の reference / require | 最後だけが root になる条件と依存 file の存在 |
| 複数 root | 後方の root の診断と walker の取りこぼしが無い |
| config + directive | clone・既定値・構成の適用順、JS と module option |
| currentDirectory / case / symlink | 実パス、解決結果、SourceFiles の集合と順序 |
| libs / JSON | lib 選択と root の扱い、parse 条件 |
| Node16 / NodeNext | mode、emit syntax、resolved module の Pointer/Store 一致 |
| mapper / incremental 等 | 実際の runner 経路と寿命、または具体的な未対応理由 |
| 正当に空の roots | 元 runner と一致することと、空集合の明示 |

root・診断対象・walker 対象を別に検証する。段 2 の raw 診断と suggestion の対象を、次のとおり確かめる (作業指示書 §3.3、2026-10-01 改訂)。

- 対象は、program の SourceFiles から、`SkipDefaultLibCheck` が真のときの default lib を除いた全部であること。
- `SkipDefaultLibCheck` が偽の構成では、default lib も対象であること。
- default lib 以外 (ユーザーの .d.ts、checkJs でない JS、noCheck の program の file) を外していないこと。
- import された依存 file 内だけにある診断も検出すること。
- hadErrors もこの対象から計算すること。root 外で program に入った otherFiles の type / symbol も見る。`NoTypesAndSymbols` 等の baseline の出力スイッチで Q1 の比較を省略していないこと。suggestion・raw diagnostics・hadErrors の順序は作業指示書どおりであること。

## 4. 検出感度を確かめる

scratch overlay で 1 つずつ壊し、元に戻して次を試す。故障を入れたテスト自身が無条件に失敗する仕組みにはしない。実際の照合・集計・strict 判定で検出する。各 mutation の差分、実行コマンド、検出したテストと出力を保存する。

| # | 故障 | 必須の検出 |
| --- | --- | --- |
| 1 | 構成を 1 つ落とす | 独立 manifest 照合が失敗 |
| 2 | 異なる構成を同じ case ID にする | 一意性または全件照合が失敗 |
| 3 | otherFiles も全て root にする | runner snapshot 照合が失敗 |
| 4 | config / directive の option 適用順を逆にする | 優先順位 fixture が失敗 |
| 5 | 非 root の依存 file、または symlink を落とす | SourceFiles / 解決結果の照合が失敗 |
| 6 | 2 番目以降の root の診断を落とす | 複数 root の比較テストが失敗 (非 root の依存 file の診断を落とす変種も試す) |
| 7 | walker から otherFiles の識別子を落とす | 独立の対象/記録照合が失敗 |
| 8 | Pointer 再試行で初期 option / root の一部を段 1 の値にする | 再試行入力の同一性テストが失敗 |
| 9 | 決定的な構成の Store 診断を変える | strict が失敗し、非決定や未対応に分類されない |
| 10 | 未処理の case を JSONL から落として report を終える | report でも全件 accounting が失敗 |
| 11 | synthetic import の無い Store panic を未対応にする | reason の検証が失敗 |
| 12 | Store 出力に internal ID token を注入する | token 検出、strict が失敗 |

8 は構成依存の決定的 fixture で再試行経路を確実に通し、乱数や偶然の非決定を待たない。12 は `__@x@1` と生 byte `0xFE` の形の両方を試す。report では差の行が残ることも検証する。検出しない mutation はハーネスの欠陥であり、本段は不合格。

## 5. 全構成の再実行と分類の監査

全構成 report を独立に 1 回再実行する。実装時の全量実行と case ID / 入力 hash で突き合わせる。相違する結果だけを追加調査し、必要な構成を再実行する。未実行部分や未分類のまま終わらせない。

```sh
cd "$TSC"
GOWORK=off STORE_CHECKER_CONFIG_REPORT=1 STORE_CHECKER_CONFIG_OUT="$OUT/configurations.jsonl" STORE_CHECKER_CONFIG_TRACE="$OUT/configurations.trace" go test -count=1 -timeout 60m -run '^TestCheckerConfigurations$' -v ./internal/storeharness > "$OUT/configurations.txt" 2>&1
```

実行前に FILTER / SAMPLE / SHARDS / SHARD と段 1 の report 変数を解除し、実効環境を保存する。60m は成功を保証する値ではない。時間を超えたら件数・未完了を報告し、必要なら全 case を覆う shard に分けて完遂する。重複も照合する。

1. 全件 = equal + mismatch + runner-skipped + runner-rejected + adapter-unsupported + pointer-excluded + harness-error。harness-error は解消が必要。各 class の代表と、複数原因の重なりを確認する。
2. equal の case は必須比較を全て通過したものだけ。診断だけ実行、walker 未実行、空 roots の見落としを equal に混ぜない。
3. runner-skipped / runner-rejected は元 runner で理由を独立に確認する。列挙不能入力の別表も検証し、未展開の構成を分母から黙って除いた全構成合格を認めない。adapter-unsupported は具体的な API / 契約の不足を確認する。通常の multi-file/config/symlink の未実装を理由に完了しない。
4. synthetic import は JSX / helper を区別し、Pointer API からの実在を確認する。段 1 の 8 件と段 2 の case 件数を混同しない。所有権の報告はコード上の確定事項と仮説を区別する。
5. Pointer panic は Pointer のみで再現する。非決定は該当 descriptor の fresh Pointer を単独で 20 回実行し、raw record の変動を確認する。再現しなければ未確認とし、除外妥当性の合格を出さない。Store の記録が Pointer の変動範囲外なら別の差として残す。
6. mismatch は代表例の最初の raw 差から原因を辿る。token、CompareSymbols の性質違反、Store panic を普通の文字列差と混ぜない。原因ごとの件数の和と case 件数を混同しない。
7. strict を、既知 equal の fixture と既知 mismatch / 未対応の代表で実行し、前者は成功、後者は失敗すること。全量 report の成功を strict 成功として報告しない。

## 6. 回帰と再生成

```sh
cd "$TSC"
GOWORK=off go build ./...
GOWORK=off go vet ./internal/ast/... ./internal/store...
GOWORK=off go test -count=1 -timeout 60m -skip '^TestCheckerConfigurations$' -v ./internal/ast/store/... ./internal/store... > "$OUT/test-notag.txt" 2>&1
GOWORK=off go test -tags storechecks -count=1 -timeout 60m -skip '^TestCheckerConfigurations$' -v ./internal/ast/store/... ./internal/store... > "$OUT/test-storechecks.txt" 2>&1
```

上の回帰 suite は、§5 で別に検証した全構成テストだけを除く。従来のテストと新規 fixture は除かない。未対応を含む全構成 strict は失敗する契約なので、これを除いた回帰 suite の結果を全構成 strict 成功と書かない。段 1 に report を適用して通したものは strict 回帰の代用にしない。

段 1 `TestCheckerCorpus` の strict、`TestModeCorpus`、`TestPolymorphicAccessors`、`TestPointerWalkerBaselines` が実行されたことをログで確認する。段 1 の比較件数・除外理由と file、token、CompareSymbols の件数を 2b-2 と照合する。差があれば説明する。

生成器 2 つを実行し、生成物が変わらないことを確認する:

```sh
cd "$ROOT"
node tools/scripts/tsc/generate.ts
cd "$ROOT/tools"
GOWORK=off go run ./cmd/storetranslate
```

終了時 source hash は開始時と比較する。認めたハーネス修正があれば対象と理由を列挙し、それ以外が不変であることを示す。

## 7. 検証報告と判定

`store-checker-7d2b3-verification-report-20261001.md` を作る。全コマンドの終了コード、成果物の場所、独立 manifest / program 照合、12 項目の mutation 感度、全 status/class の件数、除外の独立再現、回帰、source 不変を含める。修正があれば修正前後の証拠を残す。

判定は分ける:

- **7d-2b-3 の接続・分類**: 通常入力を接続し、全構成を取りこぼしなく処理し、感度検査が通り、ハーネスの欠陥が無く、差と未対応を再現可能に分類し、段 1 を維持したか。
- **7d-2b 全体の等価**: 本段の合格から推論しない。不一致・合成 import 等の未対応が残る場合は未完了。最終除外の妥当性も別途判断する。

各残件に問題・証拠・再現・仮説・修正案・受入条件を付け、次の修正指示に渡す。性能の採用判断や測定へ進まない。

## 8. 追補: 合成 import の実装 (2026-10-02)

実装報告 §12 の追補を検証する。§1 の 1 (変更は storeharness 内だけ) と 7 (合成 import の除外) は、この節に置き換える。

### 8.1 範囲

- before からの source の変更は次だけであること。
  - storeharness と docs
  - `ast/store/syntheticimport.go`・`syntheticimport_test.go` (新設)
  - `ast/store/factory.go` の `checkImport` の comment
- `checkImport` と writer の検査のコード、ほかの store の production、Pointer の package、P2、生成器と生成物、go.mod / go.sum に差分が無いこと。

### 8.2 store の API

`NewSyntheticImport` を Pointer の `fileloader.createSyntheticImport` と読み比べ、次を独立に確かめる。

1. 形が同じ: StoreID が program の IDSpace のもので、owner と file を持たず、seal されていること。
   - StringLiteral の text は module 名、TokenFlags なし。
   - ImportDeclaration は specifier だけを持つ。
   - 範囲は (-1, -1)、flags 0。
2. 親は literal → decl → file の root (foreign 辺) で、root の Statements に無いこと。`File()` が file を返すこと。
3. NodeKey が program 内で一意であること (source、遅延 JSDoc、別の呼び出し、checker の合成と重ならない)。
4. file の source の Store を変えないこと。
5. 同じ program のどの region の writer も書けず、どの region も子・親として取り込めること。別の program の region は取り込めないこと。通常 build と storechecks の両方で確かめる。

### 8.3 ハーネス

- adapter が Pointer の program の map をそのまま写すこと (file の集合・役割・text)。checker への応答が同じ (path, 役割) で同じノードを返し、Pointer に無い file では nil を返すこと。
- 除外「synthetic import」と adapter-unsupported の分類の撤去で、ほかの status・除外の規則が変わっていないこと。
- `compareModes` が合成 specifier の mode・emit syntax・解決を Pointer と比べていること。片側だけにある場合を検出すること。
- 検出感度 (§4 と同じく scratch overlay で 1 つずつ):
  - (a) adapter が合成 specifier の代わりに nil を返す。
  - (b) text を変える。
  - (c) decl の親を root でないノードにする。
  - (d) 同じ (path, 役割) で毎回新しいノードを返す。
  - いずれも、fixture の試験か段 2 の比較で失敗すること。

### 8.4 実行

- 段 2 の全構成の report と strict を再実行し、実装報告 §12.4 と case ID・入力 hash で突き合わせる。
  - strict は通ること。
  - 実装報告の run3 の adapter-unsupported 118 case が、Store checker が合成 specifier を受け取った case と一致し、全部 equal であること。
- 段 1 の `TestCheckerCorpus` で合成 import の除外が 0 になり、該当 8 file が equal であること。
- §6 の回帰を、この節の変更を含めて行うこと。
