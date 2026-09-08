# kperf (kpc) による bind の命令数計測

目的: bind 1 回あたりの instructions / cycles を **サンプリングなしで正確に** 数え、
数 % の命令数削減を 1 run で判定できるようにする。

## なぜ xctrace では足りないか

2026-09-08/09 の計測は `xcrun xctrace record --template cpu-counter` の 1ms サンプルに同梱される
コア累積 PMC を、time-profile のバックトレースと `(sample-time, tid)` で結合して bind 区間だけ足したもの。
この方式は次の理由で ±8% ぶれる。

- bind の境界をまたぐ 1ms サンプルが丸ごとどちらかに帰属する。30 bind (約 500 サンプル) で境界サンプルが 1 割ある。
- Original (コード不変) の 3 run で instructions が 3.15〜3.41G と動いた。5% 以下の差は判定できない。
- IPC は分子分母が同じサンプルなので安定する (Port 3.74 が 3 run 一致) が、命令数そのものは確定しない。

kpc は **スレッドごとのカウンタを任意の 2 点で読む** 方式なので、bind 関数の前後で読めば境界の帰属問題がない。
命令数は同じ入力に対してほぼ決定的 (期待ばらつき 1% 未満) なので、A/B は 1 run ずつで足りる。

## 仕組み

- `/System/Library/PrivateFrameworks/kperf.framework/kperf` が private API `kpc_*` を公開する。
  dyld shared cache にあるので `dlopen` で読み込む (`-framework` でのリンクは stub が無く失敗する)。
- Apple Silicon の fixed counter は **PMC0 = cycles、PMC1 = instructions** (`/usr/share/kpep/a14.plist` の
  `FIXED_CYCLES` fixed_counter 0、`FIXED_INSTRUCTIONS` fixed_counter 1。M1 は a14 データベース)。
  fixed counter だけなら kpep (イベント名解決) は不要。
- `kpc_set_thread_counting` を有効にすると、カーネルがコンテキストスイッチでスレッドのカウンタを保存/復元する。
  `kpc_get_thread_counters(0, n, buf)` は **呼び出したスレッド** の累積値を返す。
- 制約
  - **root 必須** (`kpc_force_all_ctrs_set(1)` と `kpc_set_counting` が EPERM になる)。
  - Instruments / xctrace と PMU を取り合うので同時に走らせない。
  - fixed counter は EL0 と EL1 の両方を数える。ページフォルトや `madvise` の
    カーネル側命令も含まれる (下記「落とし穴」)。
  - Go のゴルーチンは OS スレッドを移動するので、計測区間は `runtime.LockOSThread()` で固定する。

使う API (すべて `int` 返り、0 が成功):

```c
#define KPC_CLASS_FIXED_MASK        (1u << 0)
#define KPC_CLASS_CONFIGURABLE_MASK (1u << 1)
#define KPC_MAX_COUNTERS 32

int      kpc_force_all_ctrs_set(int val);            // 1: 全カウンタを kperf 用に確保 (root)
int      kpc_set_counting(uint32_t classes);         // グローバルにカウント開始
int      kpc_set_thread_counting(uint32_t classes);  // スレッド別カウント開始
uint32_t kpc_get_counter_count(uint32_t classes);    // fixed は M1 で 2
int      kpc_get_thread_counters(uint32_t tid, uint32_t buf_count, uint64_t *buf); // tid=0 は自スレッド
int      kpc_set_config(uint32_t classes, uint64_t *config);  // configurable のみ
```

読み出しバッファの先頭 2 要素が fixed (cycles, instructions)、続いて configurable。

## 実装

`tsc/internal/testutil/kperf`（build tag `kperf && darwin && arm64`、それ以外はスタブ）と
`tsc/internal/binder/bind_kperf_bench_test.go`。通常の `go test`（tag なし、`CGO_ENABLED=0`）には乗らない。
`Read` 自体と `StartTimer` の命令 (数百) は誤差なので補正しない。

## 実行手順

`sudo go test` は root の HOME にビルドキャッシュを作るので、ビルドと実行を分ける。

```sh
cd tsc
CGO_ENABLED=1 go test -tags kperf -c -o /tmp/binder.kperf.test ./internal/binder
GOGC=off sudo /tmp/binder.kperf.test -test.run '^$' \
  -test.bench '^BenchmarkBindKPC$/^checker.ts$' -test.benchtime=30x -test.count 3
```

- Original 側は同じ `kperf` パッケージとベンチファイルを microsoft/typescript の作業コピーに置いて同じ手順で回す
  (`BindSourceFile` の呼び出し規約は同じ)。
- HEAD (変更前) は `go test -c -overlay` で対象ファイルを `git show HEAD:` に差し替えてビルドする
  (2026-09-09 の base.test と同じ手順)。
- 最初の 1 回は同じバイナリを 2 度回し、`inst/op` の差が 1% 未満であることを確認する。
  これが成り立たなければ環境 (他プロセス、xctrace の残骸、root 権限不足) を疑う。

## 判定基準

- **採用条件は cycles/op と inst/op の両方が下がること。** 2026-09-09 の生成 walker は
  inst が誤差内、IPC が 3.9 → 3.74 に低下し cycles が動かなかった。命令数だけ減って IPC が同じだけ落ちる変更は
  採用しない。
- inst/op は決定的なので 1% の差を信じてよい。cycles/op は周波数とキャッシュ状態で数 % ぶれるので
  3 run の中央値で見る。
- 参考値 (checker.ts、bind 1 回、xctrace 30 bind 集計 ÷ 30): Original inst ≈ 105M / cycles ≈ 35M、
  Port HEAD inst ≈ 195M / cycles ≈ 50M。kpc の値はこれより境界分だけ小さくなるはず。

## 落とし穴

- **root なしでは `kpc_force_all_ctrs_set` が失敗する。** ベンチは Skip にして通常の `go test` を壊さない。
- **EL1 の命令が混ざる。** Port は Store arena を新規確保するので、初回タッチのページフォルトと
  `madvise` のカーネル側命令が bind に計上される。Original と比べるときに Port だけ不利になる。
  純粋な user 命令が要るときは configurable counter に `INST_ALL` を EL0 限定で載せる (下記)。
  まずは fixed で測り、Original との差がカーネル分で説明できるかを xctrace の
  thread-state 別集計で確認するのが早い。
- **ゴルーチンの移動。** `b.Run` は親と別ゴルーチンなので、`LockOSThread` は
  サブベンチのコールバック内で取る。親で取っても checker.ts の bind 中に G が
  別 OS スレッドへ移り、`kpc_get_thread_counters(0)` が別スレッドの累積
  (しばしば小さい値) を返して負の差分になる。
  差分が負または極端に小さければこれ。
- **GC ワーカーは数えない。** スレッドカウンタなので、他スレッドで走る GC mark は含まれない。
  `GOGC=off` で回せば mutator 側のアシストも消える。比較対象の xctrace 集計も
  bind スタックを持つサンプルだけなので条件は揃う。
- **並列で xctrace を走らせない。** PMU を取られて `kpc_set_counting` が失敗するか、値が壊れる。
- **`kpc_force_all_ctrs_set(1)` は解放を忘れると他プロセス (Instruments) が使えなくなる。** `Close` で 0 に戻す。

## 拡張: configurable counter で内訳を取る

fixed で命令数と cycles が出た後、「何の命令が増えたか」を知りたければ
`kperfdata.framework` の kpep で名前付きイベントを configurable counter に載せる。
M1 (a14.plist) で bind の分析に使えるイベント:

| イベント | 意味 |
|---|---|
| `INST_BRANCH` | 分岐命令数 (switch / 呼び出し段数の指標) |
| `BRANCH_MISPRED_NONSPEC` | 分岐予測ミス |
| `INST_LDST` | ロード/ストア命令数 (Store の列アクセス回数の指標) |
| `L1D_CACHE_MISS_LD` | L1D ロードミス |
| `L1I_CACHE_MISS_DEMAND` | L1I ミス (生成 switch の i-cache 影響) |
| `MAP_DISPATCH_BUBBLE` | フロントエンド供給不足 |
| `INST_INT_ALU` | 整数 ALU 命令数 |

手順は kpep_db_create → kpep_config_create → kpep_config_force_counters →
kpep_config_add_event (イベントごと) → kpep_config_kpc_classes / kpep_config_kpc_map / kpep_config_kpc →
`kpc_set_config(classes, config)` → `kpc_set_counting(classes)`。
configurable は M1 で 8 本 (counter 2〜9) なので、1 run に載せるイベントは 8 個以内。
`kpep_config_kpc_map` が返す位置で読み出しバッファの要素を引く。
参考実装は ibireme の `kpc_demo.c` (kperf/kperfdata の関数シグネチャと a14 のイベント名を含む)。

## 関連

- `docs/bind-instruction-reduction.md`: 命令数削減の方針と 2026-09-09 の結果 (生成 walker は不採用)
- `tsc/internal/ast/docs/lock-cost-playbook.md`: xctrace / pprof の手順
