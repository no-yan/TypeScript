# check phase のロックコスト計測プレイブック

check phase で ast 側の細粒度ロックがどれだけ時間を食っているかを、再現可能な数字で答えるための手順。対象は `tsc/internal/ast` の per-file RWMutex と `StoreSet.mu`、および `sync.Once` 群。checker pool 自身の mutex は対象外（後述）。

## 読み方

一つの箱が一つの作業単位。各箱は「証拠」を名指しする。証拠がファイル、ログ行、数字として存在するまで箱を閉じない。本文は手順、付録は根拠と記録。

## 前提となる事実

- check phase では各 goroutine が担当 checker の mutex を phase 全体にわたって握り続ける。`internal/compiler/checkerpool.go` の `forEachCheckerGroupDo` を参照。したがって checker mutex の待ち時間はゼロで、計測対象にならない。
- 高頻度で叩かれるのは以下。
  - `SourceFile.ParseStore()` / `ParseRoot()` / `ParseTreeRef()`。呼ぶたびに `parseStoreMu.RLock()`。`internal/ast/ast.go`
  - `ast.NodeOf()`。呼ぶたびにグローバル `StoreSet.mu.RLock()`。checker 内だけで約 180 呼び出し箇所。`internal/ast/store_identity.go`
  - `jsdocMu`、`ecmaLineMapMu`、`tokenCacheMu`、`declarationMapMu`、`dataMu`、各種 `sync.Once`（`bindOnce`、`identifiersOnce`、`nameTableOnce`、`positionMapOnce`）。
- 読み取りロック同士はブロックしない。したがって主コストは「待ち時間」ではなく、atomic 操作と cache line の奪い合いである可能性が高い。この 2 種類のコストは別の道具で測る。

| コストの種類 | 現れる場所 | 測る道具 |
| --- | --- | --- |
| ブロックして待った時間 | goroutine が sleep する | mutex profile、block profile、`go tool trace` |
| ブロックしない atomic と cache line 競合 | `RLock`/`RUnlock` 自体の CPU 時間が並列度に応じて膨らむ | CPU profile の 1 checker と N checker の差分 |
| 上記の合計が wall time に与える影響 | 経過時間 | ロックを no-op にしたビルドとの A/B |

trace（`--generateTrace`）に lock/block 時間を足す案は採らない。理由は付録 A。

## チェックリスト

### 0. 準備

- [ ] 計測用ディレクトリを決める。以下 `$OUT` と書く。ブランチ名と日付を含める。
- [ ] 対象プロジェクトを決める。既定は `/Volumes/SanDisk1TB/ghq/github.com/microsoft/vscode/src/tsconfig.json`。`node_modules` が未インストールなら `npm ci` を先に済ませる。診断エラーが出ても構わないが、ファイル数と check の実行が確認できること。
- [ ] tsc をビルドする。
  ```sh
  cd tsc && go build -tags noembed -o ../built/local/tsc ./cmd/tsc
  ```
  証拠: `built/local/tsc --version` の出力。
- [ ] 実行時間のベースラインを取る。
  ```sh
  hyperfine -w 1 -r 5 \
    'built/local/tsc -p <tsconfig> --noEmit --checkers 1' \
    'built/local/tsc -p <tsconfig> --noEmit --checkers 4' \
    'built/local/tsc -p <tsconfig> --noEmit --checkers 8'
  ```
  証拠: `$OUT/baseline.txt`。1 → 4 → 8 の speedup が線形から大きく外れていれば、並列化の阻害要因があるという最初の兆候。ただしロック以外（メモリ帯域、GC、担当ファイルの偏り）でも起きるので、この段階では結論を出さない。

### 1. mutex profile と block profile を有効にする

`internal/pprof/pprof.go` に追加する。既存の `--pprofDir` にそのまま乗る。

- [ ] `BeginProfiling` で以下を設定する。
  ```go
  runtime.SetMutexProfileFraction(1)
  runtime.SetBlockProfileRate(1)
  ```
  fraction 1 は「全競合イベントを記録」。計測専用なのでオーバーヘッドは許容する。本番の既定値には戻さない（このプレイブック用の変更は計測ブランチに留める）。
- [ ] `Stop` で `pprof.Lookup("mutex")` と `pprof.Lookup("block")` を `<pid>-mutexprofile.pb.gz` と `<pid>-blockprofile.pb.gz` に書き出す。既存の allocs 書き出しと同じ形。
- [ ] `--checkers 4` と `--checkers 8` で走らせる。
  ```sh
  built/local/tsc -p <tsconfig> --noEmit --checkers 4 --pprofDir $OUT/c4
  built/local/tsc -p <tsconfig> --noEmit --checkers 8 --pprofDir $OUT/c8
  ```
- [ ] 集計する。
  ```sh
  go tool pprof -top -sample_index=delay $OUT/c4/*-mutexprofile.pb.gz | head -30
  go tool pprof -top -sample_index=delay $OUT/c4/*-blockprofile.pb.gz | head -30
  ```
  mutex profile は Unlock した側のスタック、block profile は待った側のスタックを持つ。両方見る。block profile には channel 待ちや `WorkGroup` の待ち合わせも含まれるので、`sync.(*Mutex)` `sync.(*RWMutex)` `sync.(*Once)` の行だけを拾う。
  証拠: `$OUT/c4/mutex-top.txt`、`$OUT/c4/block-top.txt`、c8 も同様。
- [ ] 判定。ロック由来の delay 合計を wall time × checker 数（総 CPU 予算）で割る。1% 未満なら「ブロック時間は問題ではない」と記録して 2 へ進む。それ以上ならどのロックかを記録し、2 と 3 を両方やる。

### 2. ブロックしないコストを CPU profile の差分で見る

- [ ] 1 checker と 4 checker の CPU profile を取る。ステップ 1 と同じ実行で取れている。1 checker 分は別途取る。
  ```sh
  built/local/tsc -p <tsconfig> --noEmit --checkers 1 --pprofDir $OUT/c1
  ```
- [ ] `sync` と `runtime` の同期系シンボルに絞って flat time を比較する。
  ```sh
  for d in c1 c4 c8; do
    echo "== $d"
    go tool pprof -top -nodecount=40 \
      -focus='^sync\.\(\*(RW)?Mutex\)|^sync\.\(\*Once\)|semacquire|semrelease' \
      $OUT/$d/*-cpuprofile.pb.gz
  done > $OUT/sync-cpu.txt
  ```
- [ ] 呼び出し元別に分解する。`-peek` で `RLock` の直上の関数を見る。
  ```sh
  go tool pprof -peek='sync\.\(\*RWMutex\)\.RLock$' $OUT/c4/*-cpuprofile.pb.gz
  ```
  `ParseStore`、`ParseRoot`、`NodeOf`、`JSDoc` 系のどれが支配的かをここで確定する。
  証拠: `$OUT/sync-cpu.txt`、`$OUT/c4/rlock-peek.txt`。
- [ ] 判定。sync 系 flat の合計が総 CPU 時間に占める割合を c1 と c4 で比べる。c1 の値が「ロック操作そのものの単価」、c4 と c1 の差が「複数コアで同じ cache line を触ることの追加コスト」。合計が総 CPU の 2% 未満なら、ロック削減で得られる上限は 2% 程度と結論して終了できる。それ以上なら 3 へ。

### 3. ロックを消したビルドで上限を測る

> 訂正（Monaco の race 検出結果）: 以下の「check 中は書き込みなし」という前提は StoreSet の登録表には成立しない。並列 NewChecker が synth Store を登録し、診断用 NewEmitContext も Store を追加する。StoreSet.Store の読み取りロック削除は Add の append と競合する。no-lock は安全性を欠く計測専用実験であり、本番採用には登録表の安全な公開・参照・削除の再設計が必要。クラッシュしないことや診断一致は race がない証明にならない。速度差も安全な実装の達成保証ではない。

ステップ 2 で支配的だったロック一つについて、build tag で no-op にした計測専用ビルドを作る。正しさは無視する。他 checker からの書き込みが check 中に無いことは `internal/ast/store.go` 冒頭のコメント（check 中は append なし、map 書き込みなし）が前提なので、読み取りロックを外しても check 単体ならクラッシュしないはず。クラッシュしたらその事実自体が「書き込み競合が存在する」という発見なので記録する。

- [ ] `internal/ast/lockprofile_on.go` と `lockprofile_off.go` を作る。`//go:build nolock` で切り替わる小さなラッパ関数（例: `rlockParseStore(node) func()`）を置き、`ParseStore()` などの呼び出し箇所をそのラッパ経由に変える。変更は対象ロック一つに限る。
- [ ] 2 つのバイナリをビルドする。
  ```sh
  go build -tags noembed -o ../built/local/tsc ./cmd/tsc
  go build -tags 'noembed nolock' -o ../built/local/tsc-nolock ./cmd/tsc
  ```
- [ ] A/B する。
  ```sh
  hyperfine -w 1 -r 10 \
    'built/local/tsc -p <tsconfig> --noEmit --checkers 4' \
    'built/local/tsc-nolock -p <tsconfig> --noEmit --checkers 4'
  ```
  証拠: `$OUT/ab-<lockname>.txt`。
- [ ] 判定。差が hyperfine の標準偏差に埋もれるなら「このロックは消しても速くならない」。有意な差があれば、それが「このロックを設計で消した場合の上限利得」。実際の対策（ロックの外し方、Handle に Store をキャッシュする等）はこのプレイブックの範囲外で、別途設計する。

### 4. 記録

- [ ] `$OUT/README.md` に以下を書く。対象プロジェクト、コミット SHA、マシン、checker 数ごとの wall time、ステップ 1 と 2 の判定、ステップ 3 をやったならロック名と利得。
- [ ] 数字に基づく一文の結論を書く。例: 「check phase のロックコストは総 CPU の 0.8% で、うち `NodeOf` の `StoreSet.mu` が 0.6%。4 checker で消しても wall time は誤差範囲」。

## 付録 A. trace に lock/block 時間を足さない理由

- 読み取りロック同士は待たない。`RLock` の主コストは待ち時間ではないので、待ち時間を出力しても主要コストが見えない。
- uncontended な `RLock` は数十 ns で、`time.Now()` 二回分と同程度。計測が対象を歪める。
- trace イベントの出力は tracing パッケージ内で直列化される。ロックごとにイベントを出すと、計測自体が新しい競合を持ち込む。
- ロック呼び出しは百万回単位で、Chrome trace 形式で一件ずつ出すと出力が破綻する。集約するなら profiler の自作になり、Go 標準の mutex profile と同じ物を劣った形で作り直すことになる。
- trace が有用なのは phase 単位の粗い可視化のみ。`go tool trace` の goroutine timeline で代替できる。

## 付録 B. 補助的に `go tool trace` を使う場合

4 つの checker goroutine が実際に同時に走っているか、scheduler 待ちがどれだけあるかを見たいときに使う。ステップ 1 の前の当たりづけ向き。

`internal/pprof/pprof.go` に `runtime/trace` の `trace.Start` / `trace.Stop` を `BeginProfiling` / `Stop` に足し、`<pid>-trace.out` に書き出す。

```sh
go tool trace $OUT/c4/*-trace.out
```

ブラウザで「Synchronization blocking profile」と「Goroutine analysis」を見る。

## 付録 C. よくある誤読

- speedup が線形でないことをロックのせいにしない。GC、メモリ帯域、担当ファイルの偏り（`checkerpool.go` の association 重み付け）でも起きる。ロックが原因と言えるのはステップ 2 か 3 で数字が出たときだけ。
- mutex profile に `checkerpool.go` の `locks[idx]` が出たら、それは check 以外の phase（emit resolver 取得、global diagnostics）での待ち。check phase の話ではないので分けて記録する。
- `sync.Once.Do` の待ちは lazy 初期化の完了待ち。他 checker が同じファイルを先に触っている場合に起きる。block profile で `sync.(*Once).doSlow` が出たら、どの Once かを `-peek` で確認する。
