# Binder checkpointのセルフレビュー（2026-09-11）

保存用ブランチ: `codex/binder-accessor-checkpoint`。
分岐元: `perf/parser-noderef-currency`、HEAD `32598cba146fa4dd7b6162b838630c90d865ab28`。
この段階ではcommit/pushしていない。ブランチだけでは未コミット内容のsnapshotにならないため、作業ファイルのarchiveと差分を別途保存する。

## 指摘

### [P1] 意味監査ドライバが存在しない旧ハーネスを読む

場所: [luna_accessor_audit.py:279](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tools/scripts/tsc/luna_accessor_audit.py:279)。

`make_audit_sources`は`tsc/internal/binder/bind_investigation_bench_test.go`を読むが、現行の共通ハーネスは`bind_bench_test.go`へ移動しており、旧ファイルは存在しない。空の一時build directoryに対して同関数を呼び、`FileNotFoundError`で停止することを再現した。新規snapshotからの意味監査binaryを生成できず、既存artifactの検証成功はこの故障を検出しない。

修正案: 現行の共通fixture helperと寿命helperを使うoverlayへ移行し、overlay内の旧パスも修正する。新旧ハーネスが同時にcompileされて定義が重複しないことを確認する。単なる文字列のパス置換だけで完了としない。`binder_investigation.py:92`にも同じ旧パス参照があるため、現行入口として維持するか歴史的ドライバとして明示する。

受け入れ条件: 空のartifact directoryから意味監査buildが成功し、fixtureを用いた監査が実行できること。現在のPython単体テスト4件はこの実ファイル依存の経路をカバーしていない。

### [P2] KPCハーネスにはStore登録解除漏れと旧反復方式が残る

場所: [bind_investigation_kperf_test.go:129](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/internal/binder/bind_investigation_kperf_test.go:129)。

各iterationでparse → runtime.GC → BindSourceFileを行うが、Storeを登録解除していない。BindSourceFileが登録したSourceFile/Storeはglobal registryに保持されるため、Store版だけiterationと共にASTが積み上がる。通常wall用`investigationBatch`で修正した寿命条件がKPCには反映されていない。さらにcounter取得区間にはStartTimer/StopTimerも入り、純粋なbind命令数として解釈できない付帯処理が残る。

修正案: boundedな同寿命batchを共用し、counterはbatchのbindだけを挟む。timer切替・準備GC・登録解除はcounter区間外に配置し、正常終了・エラー時とも解除する。pointer overlayでも同じtimed bodyを使う。

受け入れ条件: KPCを開く前にbatch寿命試験が成功すること。各batch後の登録数がbaselineへ戻り、counter区間がbindだけであることをコード／監査で確認してから再計測する。今回はPMU取得やKPC benchmarkは実行していない。直近の通常wallとInstrumentsは修正済みハーネスなので、この指摘で無効になるものではない。

## 機能・生成コードのレビュー

HEADとの差分と未追跡のAccessor/Span、境界試験を確認した。重点は以下。

- Parameter/BindingElement、各種loop、条件式、Binary、Call、OptionalChainのbind順。
- functions-firstの2pass維持、名前がmissingの場合のresolved引数の扱い。
- integer snapshotにFlags/Symbol/Flowを保持しないこと。
- foreign childのzero表現と、Handleを使うnarrowable処理のfallback。
- schema生成とAccess*のkind/shape検査、child-only projection。

この範囲で新しいBinder本体の機能不具合は確認しなかった。ただし上記監査ドライバの故障により、現在の全意味digestを新規生成して確認したとは言えない。過去の多数の局所実験スクリプトは入口・overlay・寿命を重点確認し、全実験を再実行してはいない。

## 検証

- `go test ./internal/ast ./internal/binder ./internal/compiler`: 成功（astはcache結果）。
- `go vet ./internal/ast ./internal/binder ./internal/compiler`: 成功。
- Python `test_luna_accessor_audit.py`: 4件成功。
- Python `test_binder_investigation.py`: 6件成功。
- schemaからStore Accessor / 旧SyntaxChildren / Binder walkerをメモリ上で再生成しgofmtした結果: 全3ファイル一致。作業ファイルを書き換えずに比較。
- `git diff --check`: 成功。
- `go test -tags=binderinvestigation,kperf -run '^$' ./internal/binder ./internal/testutil/kperf`: 両packageのbuild成功。PMUを開く試験は実行していない。
- `make_audit_sources`の実ファイル参照: 上記FileNotFoundErrorを再現。

新規benchmark、CPU profile、全repo `go test ./...`は実行していない。性能基準とartifact状態は[最新再測定](bind-batch-recheck-20260911.md)、[CPU分析](binder-instruments-analysis-20260911.md)を参照。

## 保存範囲

`.cursor/skills/verify-tsc/artifacts/20260911-binder-checkpoint-self-review/`に、変更済みtrackedファイルと未追跡ソース／文書、HEAD差分、SHA256一覧を保存する。`__pycache__`/pycは除外。巨大な既存benchmark binary/trace等のignored artifactはarchiveへ重複格納せず、既存の場所に保持する。Gitの変更はブランチ作成のみで、指摘の修正やcommitはこのレビューに混ぜていない。


## 指摘修正（後続依頼）

P1/P2とも修正済み。ブランチは`codex/binder-accessor-checkpoint`を維持。Binder本体の処理ロジックは変更していない。

- P1: 現行`bind_bench_test.go`を読むよう監査source生成・overlayを更新し、Store解除helperと寿命試験もsnapshotへ含めた。実ファイルを使う回帰試験を追加。旧investigation driverのprepare/prepare-kpcも共通の寿命overlayへ移行し、旧ハーネスとの重複定義を防止する。
- P2: KPCの各iteration parse/GCを、最大10個の独立AST batchへ置換。事前GC、timer切替、登録解除をcounter区間外へ配置。正常終了、counter取得失敗、counter逆行時にもbatchを解除。通常wallと同じGC無効の補助設定にも対応。
- counter read自体の境界費用は残り、KPCのns/op/B/opにはread呼出しの費用も含む。時間・割当の比較には通常wallハーネスを使う。実PMUでの性能測定は今回未実施。

検証結果:

1. 空のsnapshotから監査binaryを新規buildし、20入力の監査成功。既存syntax-scalarsの20入力の保存済み意味digestとも一致（新しい性能比較ではない）。
2. Store/pointer両版で、同じ共通ハーネスとKPC batchコードのoverlayをbuild。BatchLifetimeと、fake counterによる成功/前段失敗/後段失敗/逆行テストが全件成功。Storeはbatch終了後の登録数がbaselineへ戻ることを確認。
3. Python監査回帰試験5件、investigation回帰試験7件が成功。
4. `go vet -tags=binderinvestigation,kperf ./internal/binder`成功。

artifact: `.cursor/skills/verify-tsc/artifacts/20260911-checkpoint-review-fixes/`。監査binary/results、compare-saved.txt、両版lifetime-proofのoverlay/identity/tests.txtを保存。repo_rootと両revisionに基づくidentityはcurrent。新KPCのns/op/B/op/allocs/op、benchstat、実counter値はmissingで、旧性能結果を新ハーネスの値とは扱わない。
