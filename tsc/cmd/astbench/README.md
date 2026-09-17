# astbench の使い方

`astbench` は、Go 製 TypeScript AST で同じ木を辿るコストを比較する CLI です。
実際の legacy pointer AST と store AST、または異なる revision の同じ表現を比較します。
木の構築・検証を計測区間から外し、root から child edge を辿る走査について、時間・訪問数・allocation・GC を記録します。
parser/checker 全体の性能測定や、型チェック結果の検証には使いません。

用途に応じて、次の操作を行います。

- AST の表現や accessor を変更した際に、同じ仕事の走査コストを比較する。
- 入力サイズを変え、`ns/visit` のサイズ依存性を調べる。
- 測定済みの raw と検証記録から、測定をやり直さずに比較表・グラフを再生成する。

## 最初に選ぶコマンド

| やりたいこと | コマンド | 実行すること |
| --- | --- | --- |
| 保存済みの結果が今回の checkout に対応するか確認する | `inspect` | identity と対象 benchmark 行を調べる |
| 1つの入力で pointer/store の訪問列が一致するか確認する | `verify` | 木を構築し、訪問順・読取属性を比較する。時間は測らない |
| 1つの入力を単発で測る | `sample` | 1表現を構築・warmup し、固定回数走査する |
| サイズ探索の計画を作る | `sweep-plan` | subtree 数を倍増する plan を出力する。benchmark は実行しない |
| 測定用の source と binary を固定する | `prepare` | before/after の snapshot と worker binary を作る |
| 計画に従って比較測定する | `run` | 訪問列検証、A/B・A/A の別 process 測定、解析出力を行う |
| 完了した探索から日常用の2サイズを選ぶ | `select-daily` | 選択根拠付き plan を作る。再測定しない |
| 保存済み結果を検証・再集計する | `collect` | JSON 検証結果、または解析ファイルを出力する |
| 保存済み結果を読む | `report` | Markdown レポートを標準出力へ出す |

比較には plan を用意し、`prepare`、`run`、`report` の順に実行します。
`sample` 1回だけでは A/B 比較や性能改善の証拠になりません。

## ビルドと共通の変数

このリポジトリの root から実行します。Go の必要バージョンは
[`tsc/go.mod`](../../go.mod) を参照してください。Git が必要で、統計比較には
`benchstat` を PATH に用意します。ホストの cache 情報は現在 macOS の `sysctl` から取得します。

```sh
REPO="$PWD"
ASTBENCH="/tmp/astbench"
ARTIFACTS="/tmp/astbench-runs"
(cd "$REPO/tsc" && go build -o "$ASTBENCH" ./cmd/astbench)
```

以下の例はこの3変数を使います。長期保存する場合は `ARTIFACTS` を永続ディレクトリへ変更してください。
`ARTIFACTS` は選択したリポジトリの外に置いてください。
`prepare` はリポジトリ内の出力先を、symlink 経由も含めてファイル作成前に拒否します。
生成済み snapshot が次の source snapshot に混入するのを防ぐためです。
`prepare` は worker も別途ビルドします。ビルドと別 campaign の測定を並行して実行しないでください。

各コマンドのフラグは `"$ASTBENCH" <command> -h` で確認できます。
現状は `-h` も終了コード1になります。位置引数は使わず、フラグで指定してください。

## まず1入力の同値性を確認する

`verify` と `sample` は、同じ形式の JSON を `--config` に渡します。
ファイルの path と inline JSON の両方を受け付けます。
これは worker 設定であり、`prepare --plan` に渡す campaign plan とは別形式です。

```sh
mkdir -p "$ARTIFACTS"
cat > "$ARTIFACTS/worker.json" <<'JSON'
{
  "case": "expression",
  "shape": "repeated",
  "nodes": 225,
  "subtrees": 32,
  "seed": 1,
  "layout_seed": 1,
  "layout": "construction",
  "representation": "store",
  "batch": 256
}
JSON
"$ASTBENCH" verify --config "$ARTIFACTS/worker.json" > "$ARTIFACTS/verify.json"
"$ASTBENCH" sample --config "$ARTIFACTS/worker.json" > "$ARTIFACTS/sample.json"
```

### `verify --config CONFIG`

合成入力では両表現を構築し、完全な訪問列と属性を比較します。
`representation` が `store` でも pointer/store の両方を検証します。
出力 JSON の `valid` と `entries`、`store_trace` / `pointer_trace` を確認します。
不一致は非ゼロ終了です。`batch` は共通形式の都合で正数が必要ですが、時間測定には使われません。
`fixture` は store のみで、pointer との同値性を証明するものではありません。

### `sample --config CONFIG`

指定された1表現だけを測り、結果 JSON を標準出力に出します。`batch` は1 process 内の走査回数です。
`GOMAXPROCS=1`、固定2回の warmup、計測中の GC off / memory limit off を使います。
構築・scratch・メモリ見積り・時計校正は区間外です。

`valid`、`reason`、`visits`、`checksum`、`ns_per_op`、`bytes_per_op`、`allocs_per_op`、
`gc_cycles` を確認します。1 op は root からの1走査で、`ns/visit = ns_per_op / visits` です。
`logical_nodes` は木全体の node 数であり、expression visitor の訪問数とは異なります。
`sample` 自体は表現間の完全な trace 比較をしないため、単発利用では先に `verify` を行います。
`run` はこの検証を自動で行います。

対応する worker 設定:

| フィールド | 値・意味 |
| --- | --- |
| `case` | `full-tree`（対照）/ `expression`（operator token 等を省略する暫定 visitor） |
| `shape` | `repeated` / `wide` / `deep` / `mixed` / `fixture` |
| `nodes` | 要求 node 数。通常は1〜1,000,000、`deep` は最大8,192。`fixture` は0 |
| `subtrees` | `repeated` の明示的な subtree 数。実 node 数は `1 + 7 * subtrees`。省略時は `nodes` から切り上げて解決し、要求数と実数を別々に保存する |
| `seed` | 合成入力の seed。明示して保存する |
| `layout` / `layout_seed` | 現在は `construction` のみ。layout seed は0または1で、配置を変更しない |
| `representation` | `pointer` / `store`。`fixture` は `store` のみ |
| `batch` | 正の走査回数。内部反復数を増やしても統計上は1 process = 1標本 |

## サイズ探索から daily まで

### 1. `sweep-plan`: 探索 plan を作る

```sh
"$ASTBENCH" sweep-plan --repo "$REPO" --run-id sweep-1 \
  --visitor expression --start-subtrees 32 --max-cases 10 \
  --batch 256 --memory-budget-bytes 268435456 --sample-timeout 1m \
  --seed 1 --out "$ARTIFACTS/sweep-plan.json"
```

| フラグ | 既定値・意味 |
| --- | --- |
| `--repo` / `--run-id` | 計画の対象 repo と仮の run ID。実際の campaign 値は `prepare` で設定される |
| `--visitor` | `expression`。`full-tree` を指定すると対照用の別探索になる |
| `--start-subtrees` | 32。ここから subtree 数を2倍ずつ増やす |
| `--max-cases` | 10。2〜10点の上限。予算により早く止まる |
| `--batch` | 256。各 cell の A/B で共通の走査回数 |
| `--memory-budget-bytes` | 268435456（256 MiB）。4096 bytes/node の保守的な計画見積りに使う。OS による RSS 制限ではない |
| `--sample-timeout` | `1m`。worker sample の timeout |
| `--seed` | 1。入力と A/B 実行順の再現に使う |
| `--out` | 新規 plan ファイル。省略時は標準出力。既存ファイルは上書きしない |

同じ7-node subtree を root の子として増やします。subtree 内の形は一定ですが、root の list 長は増えます。
既定値では225〜57,345 nodeの9点になり、10点目はメモリ計画予算で止まります。
この plan は before=pointer、after=store です。source の revision は次の `prepare` で指定します。

### 2. `prepare`: 比較する source を固定してビルドする

同じ revision の pointer/store を比較する例:

```sh
"$ASTBENCH" prepare --repo "$REPO" --before HEAD --after HEAD \
  --plan "$ARTIFACTS/sweep-plan.json" --out "$ARTIFACTS" --run-id sweep-1
```

| フラグ | 意味 |
| --- | --- |
| `--repo` | 必須。対象 checkout |
| `--before REF` | 必須。A側の Git ref |
| `--after REF` | B側の Git ref。次の `--after-working-tree` とどちらか一方が必須 |
| `--after-working-tree` | B側に未コミット変更を含む working tree を使う |
| `--plan FILE` | campaign plan。省略時の既存 preset は **store/store、mixed、256/16384 node、full-tree/expression**。sweep や選択済み daily ではない |
| `--out DIR` / `--artifacts DIR` | 必須。同じ意味の別名。run ディレクトリの親 |
| `--run-id ID` | 新しい run 名。省略時はUTC日時から生成 |

成功すると `ARTIFACTS/run-id` を標準出力に出します。
両 revision に、起動元 checkout の同じハーネスを overlay してビルドし、snapshot・hash・build log を保存します。
`--after HEAD` は dirty な変更を含みません。未コミットの変更を比較する場合は明示的に
`--after-working-tree` に置き換えてください。比較表現は ref ではなく plan で決まります。

たとえば store の変更前後だけを測る最短例は次です。

```sh
"$ASTBENCH" prepare --repo "$REPO" --before HEAD --after-working-tree \
  --out "$ARTIFACTS" --run-id store-change-1
```

既存 run は上書きしません。prepare/build が失敗した場合はログを残し、新しい run ID でやり直します。
選択済み daily plan では、現在の CPU/cache 情報が選択元と一致することも確認します。

### 3. `run`: 検証・比較測定・解析を実行する

```sh
RUN="$ARTIFACTS/sweep-1"
"$ASTBENCH" run --run "$RUN"
```

`--run` は準備済み run ディレクトリです。`--lane daily` も指定できますが、現在の対応 lane は daily のみです。
sweep は daily lane 内の mode であり、`--lane sweep` はありません。

各 cell で trace 同値性を確認し、AB順2ペア・BA順2ペアと A/A control を保存 seed で並べ替えて実行します。
生成 preset は A/A も4ペアなので、1 cell 当たり16 process samples です。
両表現は別 process で保持します。成功すると `analysis/` に比較結果を出します。

失敗や中断の attempt も残ります。同じ `run --run` を再実行すると完了済み slot を飛ばして続行します。
再開時も source/binary hash と trace を検証します。途中で frozen plan を編集しないでください。
macOS / Linux では OS advisory lock で campaign を直列に実行します。
所有 process が終了すると、SIGKILL の場合も OS が lock を解放します。
旧 PID ファイルだけが残っている場合も lock を取得できます。
全 process が同じ inode を使う必要があるため、lock ファイルは手動削除しないでください。
その他の OS の `run` は unsupported として失敗します。
この lock では他アプリの CPU 負荷や thermal 状態を制御できません。

### 4. `select-daily`: 探索から2点を固定する

以下の64 KiBはホストで確認した L1D 容量の例です。値や cell ID は保存した探索結果に合わせて選びます。

```sh
"$ASTBENCH" select-daily --run "$RUN" \
  --small expression-subtrees-32 --large expression-subtrees-4096 \
  --l1d-target-bytes 65536 --preset-version 1 \
  --reason '容量見積りと実行時間に基づく選択。small の cache residency は未証明' \
  --out "$ARTIFACTS/daily-plan.json"
"$ASTBENCH" prepare --repo "$REPO" --before HEAD --after HEAD \
  --plan "$ARTIFACTS/daily-plan.json" --out "$ARTIFACTS" --run-id daily-1
"$ASTBENCH" run --run "$ARTIFACTS/daily-1"
```

`--run` は完了済み sweep、`--small` / `--large` はその cell ID、`--reason` は選択理由です。
`--l1d-target-bytes` は保存済み host 情報の最小 L1D 容量以下にします。
`--preset-version` は既定1で、CPU・生成規則・visitor を変更して選び直す際は増やします。
`--out` は新規ファイル、未指定なら標準出力です。

指定した2点が容量と実行時間の条件を満たすか検査します。
欠測や失敗、未知の L1D / footprint、過大な時計 overhead、時間予算超過等を拒否します。
現在の条件は small の field-footprint 下限が L1D 以下、large が両表現とも L1D の4倍以上、
時計校正値が batch 時間の1%以下、選択 cell の worker・trace検証時間合計が90秒以下です。
この時間見積りには driver の全処理時間は含まれません。
small の下限が小さいだけでは L1D に収まる証明にならないため、選択結果は provisional です。

host 情報を取得できない環境では選択できません。現在、macOS 以外の CPU/cache 収集は未実装です。
その場合も sweep 自体は実行でき、取得できない情報を記録します。

## 保存済み結果を調べる

### `inspect --repo REPO --artifacts DIR [--bench REGEX]`

```sh
"$ASTBENCH" inspect --repo "$REPO" --artifacts "$RUN" --bench BenchmarkTraversal
```

`--artifacts` は1 run または run 群の親です。`--bench` は既定 `.*` で、`bench.txt` 内の benchmark 名に適用します。
`identity_status` と `stage_status` は別々に出力されます。

- `current`: repo_root・typescript_go_git_rev・tsgolint_git_rev が対象と一致。この CLI では TSGolint は null。
- `stale`: repo または revision が異なる／欠けている。
- `missing`: artifact path や identity がない。
- `unsupported`: artifact はあるが、要求した Benchmark 行がない。

`current` だけでは dirty source や build 条件の一致は保証しません。`identity.json` の source hash 等も確認します。
新しくコミットすれば旧 run の revision は `stale` になります。過去の結果を壊れた結果とみなす意味ではありません。

### `collect --run RUN [--out DIR]`

```sh
# 保存した attempts・plan・同値性証明を検証し、JSONを標準出力に出す
"$ASTBENCH" collect --run "$RUN" > "$ARTIFACTS/collection.json"

# 測定をせずに解析を再生成する
"$ASTBENCH" collect --run "$RUN" --out "$ARTIFACTS/reanalysis"
```

`--out` なしでは解析ファイルを生成しません。JSON の `complete`、`valid`、`reason` を確認してください。
結果が不完全でも JSON を返して終了コード0になる場合があります。
`--out` ありでは完全・有効な campaign が必要です。
出力先は元 run の `analysis/` または run の外部に置きます。派生ファイルは再生成されますが、raw attempts は変更しません。
benchstat がない場合は raw を残し、比較未実施と記録します。

### `report --run RUN`

```sh
"$ASTBENCH" report --run "$RUN" > "$RUN/report.md"
```

Markdown を標準出力に出す読み取り専用コマンドです。測定や解析の生成はしません。
解析がない／input digest と一致しない場合は比較 missing と表示するので、先に `collect --out` で
`"$RUN/analysis"` を再生成します。report も終了コードだけでなく completeness / validity を確認します。
外部 `--out` の解析を読むフラグはなく、report は常に run 内の `analysis/` を読みます。

## 出力の見方

| 保存場所 | 内容 |
| --- | --- |
| `plan.json`, `identity.json` | 入力、実行順、選択理由、source/binary の識別情報 |
| `snapshots/`, `build/` | before/after source と worker、build log |
| `control/verification-*.json` | 完全な訪問列の比較証明 |
| `attempts/NNNNNN/` | 引数・環境・時刻・stdout/stderr・metrics・raw `bench.txt`。失敗も保持 |
| `analysis/cells/<cell>/` | サイズ別の before/after・A/A raw と benchstat |
| `analysis/samples.tsv` | attempt 順序、validity、時間、訪問数 |
| `analysis/memory.tsv` | used、capacity、reachable heap estimate、peak RSS、access footprint を分離した表 |
| `analysis/scaling.tsv`, `scaling.svg` | 実 node 数を横軸、`ns/visit` を縦軸にした比較。標本と不確実性を表示 |
| `analysis/paired.json` | サイズごとの対応する A/B 比と bootstrap 区間 |

`B/op` / `allocs/op` は走査の計測区間についての値です。構築費や保持メモリ量ではありません。
未知のメモリ値は null と理由を保存し、RSS を AST 単独のサイズとは呼びません。
各サイズを独立に benchstat で比較します。4ペアは方向確認用で、非有意差は同等性の証明ではありません。
SVG の bootstrap 幾何平均と benchstat の中央値は異なる集計です。

現在は construction 配置、warm-repeat、wall-time の daily lane が対象です。
KPC decision lane、配置 shuffle、cache residency の証明、実 parser/checker 全体への一般化は対象外です。
生成ファイルを含む run を移動すると snapshot/binary の絶対path参照が壊れるため、元の保存場所を維持してください。

設計・検証の詳細:

- [ハーネスの主設計](../../internal/ast/docs/traversal-measurement-design-20260917.md)
- [M1サイズスケーリング設計](../../internal/ast/docs/traversal-m1-size-scaling-design-20260918.md)
- [実装と検証結果](../../internal/ast/docs/traversal-m1-size-scaling-implementation-20260918.md)
