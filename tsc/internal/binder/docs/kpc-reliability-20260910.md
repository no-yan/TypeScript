# KPCカウンタ逆行の切り分け（2026-09-10）

完了更新: v7で両イベント群のA/Aが通過し、8入力の命令数・cyclesと
4入力のload/store・cache比較が完了。[結果表](kpc-binder-results-20260910.md)参照。

## 問題

`kpc-build-v5`の事前検証で`counter 2 decreased`となり、binderの
EL0命令数・cycles比較を開始できなかった。負値の補正やwrap加算は行っていない。

選択repoは`/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、
revision `32598cba146fa4dd7b6162b838630c90d865ab28`。
pointer対照は`/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、
revision `8ac035a394c79e693a3a7d74cb170448503ee894`。TSGolintはnull。
候補checkout一覧・既存wall/alloc結果・Instruments帰属は
[binder実験ノート](binder-investigation-results-20260910.md)を参照。
比較checkoutは変更していない。

## 証拠

artifact: `.cursor/skills/verify-tsc/artifacts/20260910-kpc-reliability/`。
Goを除いたCの専用probeで、各条件3新規process、各100区間を記録した。
1万回と100万回の同じ整数ループを交互に実行し、全10counterのbefore/afterを保存。

| 条件 | 区間 | 逆行したcounter差分 | read error |
|---|---:|---:|---:|
| PMU初期化に使用した同じpthread | 300 | 5 | 0 |
| PMU初期化後に生成したpthread | 300 | 0 | 0 |
| 同新規pthread、開始前100ms待機 | 300 | 0 | 0 |

同じpthreadでは、例としてbefore=18446744073708603634、after=2053481
という値が出た。初回だけでなく87区間目・95区間目にも逆行した。
したがって「最初の一回を捨てる」方法では検証を満たさない。
新規pthreadのEL0命令数は1ループ当たり約5命令で、1万/100万回に対応した。
rawはprobe-output、集計はprobe-summary.json、source/binary hashはidentity.json。

[Apple XNUのkpc_thread.c](https://raw.githubusercontent.com/apple-oss-distributions/xnu/main/osfmk/kern/kpc_thread.c)
ではCPU側snapshot差分をthread側へ累積し、新規threadは別のbufferを得る。
既存threadに残る累積baselineのwrapと整合する観測である。
公開mainの実装と現ホストkernelが完全同一とは仮定せず、kernel内部の発生箇所を
断定しない。Goを外しても再現したため、GoのAST/GC自体による計数異常ではない。

## 修正

測定用kperf APIに`OnFreshThread(func() error)`を追加。
PMU設定後にCのpthread_createで新規OS threadを生成し、そのthread上でGoの
測定callbackを実行する。runtime/cgo.Handleでcallbackを渡し、join後に結果を返す。
callbackはLockOSThreadを保持する。既存threadを固定するだけでは代用にならない。
初期化・thread生成・joinはbinderの計測区間に含めない。

callbackのerrorとpanicを呼出元へ戻す単体テストも追加した。
callback内ではtesting.Fatal/runtime.Goexitを使用せず、errorを返す。
通常ビルドは変更せず、binderinvestigation+kperf+darwin+arm64+cgoの測定用buildのみ。

新buildは元artifactの`20260910-binder-investigation-02/kpc-build-v6/`。
両版・instructions/memoryの各群・各2新規processで自己検証した。
1processにつき3新規pthread×100組の短/長ループ、定期的なruntime.GCを含む。
**全8processがPASS**。計2,400組、4,800測定区間で逆行なし。
error伝播・panic伝播の単体テストもPASS。
これは計測器検証でありbinderの高速化結果ではない。

## 状態と採用条件

| stage | status | 根拠 |
|---|---|---|
| C probe | current | repo/revision、source/binary hash保存 |
| v6自己検証 | current | 両版・両イベント群が全てPASS |
| v5 BenchmarkBindInvestigationKPC | unsupported | Benchmark行なし、旧失敗ログ保持 |
| v6 instructions A/A | current | 精度条件通過、v7に流用しない |
| v6 memory A/A | current | 8round目のテスト初期化失敗で未完了、比較には使わない |
| v7 binder A/A・入力別比較 | current | 両群A/A通過、命令数8入力・memory4入力完了 |

ns/op、B/op、allocs/opはこの専用probeでは対象外。実binderの最新結果は従来の
checker 12,458,733.5→19,064,295 ns/op、7,425,344→12,799,524 B/op、
13,954→14,165 allocs/opのまま（pointer→Store）。
hot pathはwalk/helpers、allocation driverはsymbolIdx/flowIdx列・FlowNode/Handle拡大。
この修正でその診断を変更していない。

30bind×10roundsのchecker/dom A/Aをinstructionsとmemoryで別々に実施した。
同じbinary hash・event config hashで精度条件を通過したA/Aがなければ
paired計測を拒否する。instのCI±1%、cyclesのCI±1.5%を要求する。
Instrumentsと同時実行しない。CPU帰属は引き続きInstrumentsを使用し、pprofは使わない。

## 再現・実行環境

probe.cを`clang -O2`でbuildし、probe-run.shを管理者権限で実行。
Go自己検証はkpc-build-v6のbinaryに、各群のBINDER_KPC_CONFIGを指定して
`-test.run=^TestInvestigationKPC$ -test.v`を各2新規processで実行する。

binder測定は`/tmp/kpc-v7-campaign/run.sh`。
管理者processが外付けvolumeのPython sourceを読めなかったため、同じbinary・
fixtureを/tmpへコピーした。build identityのrepo_rootは元checkoutのまま。
v6では既存fixture packageの初期化がcompile時のsource pathからgo.modを探すため
途中で失敗した。v7は測定用overlayにBINDER_INVESTIGATION_REPO_ROOTを追加し、
このテスト初期化の参照先も分離した。コンパイラ本体は未変更。
runtimeのcwdは/tmp/kpc-v7-campaign/tsc、manifestはコピー先pathを使用する。
fixture内容は各SHA256で検証。root権限の取得はmacOS認証ダイアログに従う。
