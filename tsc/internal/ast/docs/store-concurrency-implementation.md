# Store の並列読み取りと生成用 Store の分離

## 問題と診断

対象は `/Volumes/SanDisk1TB/worktree/lock-profile`、元の revision は
`8067b4b419b2420236e396b4892f66c97141904b`。TSGolint は対象外。
作業開始時には StoreSet.Store が RWMutex を取得していた。チェック時の
共有 reader counter と、emit が parse Store の可変 slice に追記することは
別の問題であり、登録表のロックだけを外しても後者の race は解消しない。

添付資料の性能値は別 revision の測定であり `stale`。資料にある
`/private/tmp/lock-monaco-20260905` の生データはこの環境では `missing`。
候補 checkout は `git worktree list` に記録されている同リポジトリの
`cursor-ast-store-tests`、`lock-design-inv`、`store-redesign`、
`store-nolock-exp`、および本体 `no-yan/TypeScript` など。
選択は変更せず、この checkout の変更前実行ファイルを対照として保存した。

## 実装

- StoreSet は不変 directory、固定長 page、atomic pointer slot を使う。
  読み取りは共有カウンタを書き換えない。writer mutex は登録・削除に限定。
  directory は幾何的に成長し、登録のたびの全表コピーを行わない。
- Store の ID は domain と slot の公開後に設定する。同時 RegisterStore は
  冪等。別 domain の adoption は拒否し、foreign Remove は対象を消さない。
  Remove は配列や既存 Handle を破壊せず、ID は再利用しない。
- SourceFile は Store/root/Kind の不変ペアを atomic pointer で公開する。
  ParseRoot は Store の node slice にも触れない。
- Symbol.Declarations と ValueDeclaration は Handle を保持する。
  高頻度の宣言参照に登録表は不要。既存コミット `3bf9e00dfd` の変更を移植。
  GlobalRef は identity key として残す。
- 同一 Store のノード参照は引き続き 32 bit の NodeRef。外部の親・子・
  リスト要素だけ sparse map に Handle を保持する。これにより外部ノードの
  同一性、祖先、所有元の寿命を保ち、参照先をコピーも変更もしない。
- ListRef は 64 bit の owner-qualified identity にする。ノード内の同一
  Store のリスト slot は 32 bit のまま。外部リストの slot と所有者だけ
  sparse map に保持する。読み取りは所有元に、変更は自分の Store に限定。
  変更のないリストを Factory.Update* がそのまま再利用できる。
- emit は context 専用の Store に割り当てる。parse Store を解凍する
  EnterEmit/LeaveEmit を廃止し、Freeze を不可逆にする。
  Program.Emit は SingleThreaded の設定に従って並列実行する。
- SourceFile の emit 用 wrapper を Store のグローバルな所有元として
  上書きしない。EmitContext.SourceFileOf が現在の出力ビューを解決し、
  通常の semantic reader は元ファイルを読む。wrapper の元ファイル identity
  は OriginalSourceFile で比較する。
- pooled EmitContext.Reset は旧 Store の登録を解除し、新しい Store を使う。
  以前生成したノードのメタデータが次のファイルに置き換わることを防ぐ。

## 所有権とコスト

Store の登録は、登録後の AST 書き込みを同期しない。build 中は単一 writer、
Freeze 完了後の公開には既存の parse/bind/check の barrier を使う。
外部参照を公開する場合、参照先は凍結済みか、同一 writer の管理下でなければ
ならない。生成用 Store 自体を複数 writer が同時に変更する設計ではない。

Handle と外部所有者 map は GC が走査する。このコスト増と、宣言参照の
解決を省く効果の両方を評価する必要がある。nodeHeader は 24 bytes、
children は 32 bit の noscan 配列を維持する。ID の高水位は下がらず、
parse Store の LS eviction は引き続きライフサイクル管理側の課題。

## 再現と受入条件

生データ・metadata・変更開始時の diff は `.audit/store-concurrency/`。
`repo_root`、`tsgolint_git_rev`、`typescript_go_git_rev` が対象と一致するとき
のみ artifact を `current` とする。bench.txt に対象 Benchmark 行がない
stage は `unsupported`、未取得は `missing` と記録する。

```sh
go test ./tsc/internal/ast ./tsc/internal/printer ./tsc/internal/compiler
go test -race ./tsc/internal/ast ./tsc/internal/printer ./tsc/internal/compiler

go test ./tsc/internal/ast -run '^$' \
  -bench '^(BenchmarkStoreSetReadParallel|BenchmarkParseRootParallel)$' \
  -benchmem -cpu 1,4 -count 6

STORE_BENCH_PROJECT=/path/to/vscode/src/tsconfig.monaco.json \
  go test ./tsc/internal/compiler -run '^$' -bench '^BenchmarkStoreMonaco$' \
  -benchmem -benchtime=1x -count=6
benchstat old.txt new.txt
```

機能条件は、登録公開順序・domain・削除の回帰テスト、外部参照とリスト共有、
凍結後の変更拒否、並列 emit 中の parse 木の不変性、JS/d.ts/source map の
直列・並列一致。race 検出を性能比較に混ぜない。microbenchmark の速度を
実ワークロードの速度と同一視しない。

実ワークロード benchmark は config/parse/bind/check/close を含む1 Program
生成を1 opとする。CLI の process startup は含まない。時間 ns/op、
TotalAlloc に基づく B/op、Mallocs に基づく allocs/op を Go benchmark が
報告する。CLI の wall time と Check time は別に記録する。

性能の仮説は reader counter の競合と繰り返す registry lookup の削減。
変更前後の実測、hot paths、allocation drivers、結果に基づく次の行動は
artifact の結果レポートに記録する。測定前に速度改善を達成したとは扱わない。
