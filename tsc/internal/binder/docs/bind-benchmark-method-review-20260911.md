# bind単体ベンチマークの方法レビュー（2026-09-11）

## 判断

最優先の修正はASTの寿命を揃えること。現行Store版はiterationごとに登録したStore/SourceFileを解放せず保持する。
現行pointer対Storeの約1.3倍という結果は、異なる保持条件を含むハーネスの観測値であり、Binder固有の退行率としての採用を保留する。
これは差が全て消えるという予測ではない。保持量はGCのscan費用だけでなくheap goalにも影響し、偏りの方向・大きさは未確定。

今回はコードと保存artifactのレビューのみ。新しいbenchmarkは実行せず、実装ソースも変更していない。

## 確認した問題

1. **P1: Storeの寿命が非対称。** `Store.appendList`はStoreを登録し、Binderも`ast.RegisterFile`を呼ぶ。
   `storeSlot`はStoreとSourceFileをatomic.Pointerで強参照する。ハーネスに`UnregisterStore`がないため、過去のAST・Symbol・Flowが残る。
   `runtime.KeepAlive`は寿命をその位置まで保証するもので、登録解除ではない。GCを呼んでもこの登録は解除されない。
   Go benchmarkの`runN(1)`による予備実行も同じプロセス内で起きるため、表示された10 iterations以外の状態も残り得る。
2. **P1: 強制GCの影響を時間除外だけでは分離できない。** GCはsync.Poolのprimary/victimを入れ替え、古いvictimを破棄する。
   両版にparserPool/binderPoolがある。毎回のGCはpool再利用、cache状態、heap goal、bind中の自然GCの発生条件に影響する。
   大きな生存heapを走査する処理をタイマー外へ置いても、その後の実行環境が等しくなる保証はない。
3. **P2: タイマー切替も実行状態に介入する。** Go 1.26のStartTimer/StopTimerはruntime.ReadMemStatsを呼ぶ。
   その時間は基本的に測定区間外だが、iterationごとの切替は無影響ではない。事前準備とbindをbatch単位に分ける方が測定境界を明確にできる。
4. **P1: A/A不合格でも前後差のp値を根拠に強く解釈した。** 固定探索としてrawを保存することと、退行率を確定することは別。
   精度未達と非対称な寿命条件を解消するまで約1.3倍を確定値として使わない。
   A/Aを比較の前にまとめて実行すると、比較中の状態変化を検出できない。次protocolでは同一バイナリ対照を比較ブロック内へ配置する。
5. **P2: CLIとの比較対象が異なる。** ハーネスは大型2ファイルの逐次BindSourceFile。CLIのBind timeはGetBindDiagnostics全体の経過時間。
   CLIは通常の並列経路があり、Store版ではFreezeも含む。入力分布・並列性・生存heapを揃えず倍率を一般化しない。

## 最小修正と受け入れ条件

- Store版はtimed bind終了後、全consumerの使用が終わった段階で、そのiterationが所有するStoreをUnregisterStoreする。
  pointer版は対応するSourceFileを参照し続けない。これはハーネスの寿命管理であり、Binder内部処理の最適化ではない。
- bind前に登録解除してはいけない。StoreIDからの解決を壊す可能性がある。
- b.Cleanupだけで最後に全Storeを解除しても、測定中のiterationごとの保持増加は解消しない。
- 計測外の監査でRegisteredStoreCountが毎回の開始値へ戻ることを確認する。診断・Symbol・Flow・意味digestも維持する。
- Cleanup後の生存heapとscan量がiteration数に比例して増え続けないことを別probeで確認する。
  registryのID/highWaterやdirectoryは完全には巻き戻らないため、live countとdirectoryの増加を区別する。
- 同じSourceFileを繰り返しBindして測ってはいけない。BindSourceFileはIsBoundで早期returnするため、各timed bindに新しい未bind ASTを用意する。

## 推奨する次のprotocol

### 自然GCを含むbind batch

同じ入力を同じ個数だけ事前parseし、両版とも全ASTを同じ寿命で保持する。固定した小さなbatchを一度だけbindする。
計測開始・終了はbatchの前後だけに置き、途中で強制GCやparseを挟まない。
GOGC=100。必要なら事前GCをbatchの前に一回だけ行う条件として明示し、CLIの自然な状態と同一とは呼ばない。
終了後にStore登録とAST参照を解放する。batch個数・GOMAXPROCS・新規プロセス単位を両版で固定する。
ns/opと割当をbind数で正規化する。固定batchでもRAMに収まるか、保持量を事前確認する。

### GCを除いたbind処理の補助対照

同じ有限batchを準備し、計測区間だけ自動GCを無効化する。命令数・cyclesを測る補助条件とする。
noscanによるGC改善を評価する条件ではない。自然GC込みの結果と別々に報告する。

### 実プロジェクトの主評価

ユーザーの同じ9,733ファイルのプロジェクト・同じoptions・同じsourceに対応するCLIバイナリを使う。
通常並列と逐次条件を区別して、parse+bind全体と診断のBind timeを測る。
同じ数のASTを保持し、GC CPU/assist/live/scanも対応する区間で採取する。
単体batchは原因分析、実プロジェクトは実用性能の評価として分ける。

いずれも実行順を事前にbalanced/randomized blockへ固定し、同一バイナリ対照を途中に含める。
rawを保存してbenchstatとpaired区間で比較する。精度を通すための事後的なround追加は行わない。

## 証拠の位置と状態

選択repoは`/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`（32598cba146fa4dd7b6162b838630c90d865ab28）、
pointerは`/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`（8ac035a394c79e693a3a7d74cb170448503ee894）。両tsgolint revisionはnull。
候補clone一覧は`20260911-current-pointer-bind-recheck/worktrees.txt`。今回も他cloneは未使用。
保存済み再計測artifactはidentity上current。方法上の問題があってもstaleへ付け替えず、解釈の制約を追記する。
修正ハーネスでの測定はmissing。新たなunsupported stageはない。

保存値（pointer→Store）はchecker 12,688,139.5→16,745,956 ns/op、7,425,345→12,799,499 B/op、13,954→14,165 allocs/op。
domは4,603,637.5→5,876,910.5 ns/op、5,287,312→7,870,383.5 B/op、16,562→16,684 allocs/op。
これらは元ハーネスの観測値として保持する。計測条件を修正した結果として使わない。
既知の広いhot pathはwalk/helpers、allocation driverはsymbolIdx/flows列、symbolRefs、FlowNode/Symbol/Handle大型化。
本レビューで新しいprofileや各要因の寄与は測っていない。次の作業は寿命監査・ハーネス修正を先に行い、固定protocolで再評価すること。


### 寿命を揃えるハーネス修正（2026-09-11）

[実装・検証記録](bind-batch-lifetime-fix-20260911.md)。最大10個のdistinct/unbound ASTを事前準備し、batch全体を一度計測してから登録解除する。両版の寿命テスト・上限検査成功。Store登録数は毎batchで0へ復帰。通常GC／GC無効の計96行は保存したが、A/Aは全条件不合格。測定中のlive binder.go変更も検出したため、固定snapshotの観測値として扱い、現在コードの確定倍率には使わない。
