# Store checker の private 名・symbol 対応・計測境界

2026-09-26。レビュー4点の設計上の対応。実装・性能測定の完了報告ではない。[7d 設計](store-checker-design-20260923.md) の I1 / H1 / NB1 / §2.7 と、7d-0 の実装・検証指示を補う。以下の条件は古い「単スレッドなら番号一致」「private 名に file 内番号を使う」「Pointer source 対応を計測でも再利用できる」という記述を置き換える。

## 1. private 名は binder と checker で同じ採番を使う

現行 Pointer の `binder.GetSymbolNameForPrivateIdentifier` は `ast.GetSymbolId(containingClassSymbol)` を使う。一方 Store の同名関数は `containingClassSymbol.Id()` を使う。後者は Bound 内で密な番号なので、別 file の class と重複する。checker は private member lookup でもこの helper を呼ぶため、bind 時の保存と check 時の検索が同じ名前を作る必要がある。

7d-0 では以下を実装する。

- `Symbol.id` / `SymbolId()` は Bound の表の index 専用とする。private 名や checker の identity に使わない。
- `Symbol.gid` と `store.GetSymbolId` は Pointer と同じくプロセス内の非零・一意な64bit遅延採番。bind と check で共用し、file / checker / benchmark iteration ごとに reset しない。並列の初回呼び出しは同じ symbol に同じ番号を返す。上限で過去の番号を再発行しない。
- `storebinder.GetSymbolNameForPrivateIdentifier` は `store.GetSymbolId` を呼び、64bitの10進文字列を使う。`#<gid>@<description>` の構成は Pointer と揃える。
- 7d-2 の checker の helper 呼び出しは、この storebinder の同じ関数へ変換する。lookup ごとに別の採番・文字列形式を作らない。transient / synthetic class symbol にも同じ契約を適用する。

7d-0 の回帰例は、別 file でローカル SymbolId が同じ class、同一 file の入れ子 class、同じ `#x`、static / instance、継承を含める。異なる class の private 名が異なり、同じ class の bind と後続 lookup helper が同じ名前を返すこと、Members の正しい symbol が引けることを検証する。7d-2 では実際の property lookup、別 class への不正アクセスの診断、基底 class の private と派生 class の同名 private の区別を Pointer と比較する。

7c の `bindequiv_test.go` は private 名の数字を正規表現で消す。これは今回の名前衝突や binder/checker の食い違いの検証には不十分。除外を増やさず、class symbol の対応と private member の宣言から検証する専用テストを追加する。

## 2. symbol の番号ではなく対応と順序を検証する

`ast.GetSymbolId` の counter はプロセス全体であり、初期化、bind、前に走ったテストや warmup でも進む。単スレッドは並列競争を除くが、Pointer / Store の counter 初期値と採番要求順を一致させない。I1 は同じ採番方式を選ぶ設計であって、二つの表現間の数値一致の保証ではない。

等価検証は次の条件にする。

1. 同じ入力 file と宣言位置・kind・宣言の役割、owner / export / member の関係から Pointer symbol と Store symbol の一対一対応を作る。型と symbol の比較は独立した walker が行う。名前の数字を消した結果だけで対応づけない。
2. 宣言のない symbol は、対応済みの owner と member slot、または同じ検査操作で生成された役割から対応づける。候補が複数残ったら曖昧として失敗し、順番で任意に割り当てない。対応表は両方向に持ち、二つの symbol を一つへ潰さない。
3. 内部の `#<id>@name` と `__@name@<id>` に埋め込まれた番号を比べる場合だけ、対応済み symbol に結びついた番号として照合する。private 名は containing class、unique symbol はその symbol 自体の identity を検査する。各側の token が、その側の対応済み symbol の既存 gid と一致することを検査し、Pointer token と Store token の数値同値は要求しない。観測時に追加採番は起こさない。任意の数字やユーザー名を正規表現で消さない。pattern ambient module の既存の別規則と混ぜない。
4. `compareSymbols` の ID fallback は別途検査する。I1 の独立した lazy gid を維持するので、任意の対応ペアの ID の大小・fallback の符号が表現間で一致することは契約にしない。各側で同一性・反対称性・採番後の比較の安定性を検査する。その上で、同じ入力と操作列の実際の symbol / type walk、候補選択、印字の順序を比較し、差が出た箇所の採番 trace と公開結果への影響を調べる。公開出力や選ばれる候補が変わった場合は失敗であり、数字や並びの正規化で隠さない。未解決なら I1 を採用済みとせず、別の順序契約を設計してから進む。
5. 生の診断・related information・suggestion・印字文字列は引き続き完全一致で比較する。内部 ID 対応表を使ってユーザー向け出力を黙って正規化しない。違いが出たら採番順・tie break・print 経路を調べ、解決前に合格としない。

採番順を調べる補助テストでは、Pointer と Store をそれぞれ新しい子プロセスで実行し、入力順・options・要求操作を固定する。bind から check / print までの初回採番イベントを既存の戻り値から記録する。観測のために追加の GetSymbolId を呼んで採番を起こさない。初期 counter と事前採番の条件、初めて順序がずれたイベントを保存する。fresh process だけでも数値一致は保証しない。テストの都合で本番 counter を reset したり Pointer のコードを変更したりしない。

7d-0 では GetSymbolId の安定性・一意性・並列初回呼び出し・境界、および bind の private 名を検証する。checker の fallback の性質と実際の選択・出力順序、unique symbol・印字は7d-2の必須条件であり、7d-0 のテストで済んだとは扱わない。

## 3. retained / GC は比較用 Pointer AST のないプロセスで測る

等価テストの H1 は両方の AST を同時に持ってよい。Store の retained / GC ゲートには、そのハーネスをそのまま使わない。Pointer program を生かしたまま HeapAlloc の差分だけを取っても、GC が走査する graph と heap の条件は分離できない。

### 入力と保持の境界

比較用の準備プロセスで H1 の file 順・text・options・module 解決・file metadata・project reference 情報を値だけの fixture に出力する。測定プロセスは一方の表現だけを構築する。Store 側の adapter は fixture と Store file を使い、Pointer program を closure / interface / host / cache に保持しない。

単に SourceFiles を切り離すだけでは足りない。`checker.Program` と Host の全 method の戻り値を再帰的に監査し、`ParsedCommandLine` の config AST などの間接保持も値へ落とす。`GetSourceFile` 系は Store file、JSX runtime / import helper の specifier は Store のノードで再構成する。syntax-sensitive な解決は file identity・位置・kind・mode 等の値で索引を作り、未収録なら準備時に追加する。測定中に Pointer parser / program を呼ぶ fallback は認めない。fixture の完全性と adapter の動作は等価テストで確認する。

Store → Pointer の print adapter も、比較用 Pointer source を必要条件にしない。必要な source text/range/Original の情報を Store 側から変換する。印字が必要とする一時 Pointer ノードは実機能の費用なので除外しない。比較用 AST として区間外に先に作ったり、印字費用から差し引いたりしない。

### 測定手順と証拠

1. 同じ fixture を使い、Pointer / Store の別子プロセスを交互に実行する。各プロセスで fixture のみを読んだ共通 baseline を取る。
2. parse + bind、check、必要に応じ check + print の各段で、同じ意味の root（program adapter、files、Bound、checker と必要な cache）を生かす。GC を2回行い、HeapAlloc と live object の profile を保存する。root は計測終了まで KeepAlive する。
3. retained は共通 baseline からの増分と絶対量を両方報告する。baseline に対象 AST を入れない。pool を含む条件を揃え、各 variant で同じ数の forced GC を使う。
4. GC の測定は GC 有効で行い、同じ live graph の mark CPU、scan bytes、GC回数を保存する。GCオフの KPC は別実行とし、retained / GC の結果と混ぜない。
5. fixture の型の監査、Store 測定プロセスの構築経路、live heap profile の割り当て元を突き合わせる。比較用 `*ast.SourceFile` / `*ast.Node` graph が保持されていないことを確認する。live の Pointer ノードがあれば、print が必要とする生成元・数量・寿命を別記する。profile だけで参照経路の不存在を証明したとは扱わない。

測定用 adapter / fixture のメモリも隠さず内訳へ入れる。両側共通の text/options 等の控除条件は同じにする。checker を生かした段と release 後の段を混ぜない。印字後も SerializedTypeEntry 等が保持する Store AST は対象内であり、測定のために cache を消さない。

7d-0 の既存 parse / bind retained ベンチも variant 別プロセスで走らせ、比較用 AST を作っていないことを確認する。H1 の detached fixture と check 後の測定は7d-2で作る。既存の retained ≤0.85、mark CPU ≤0.75 × Pointer はこの隔離条件で判定する。条件を満たす artifact がなければ `missing` とし、混在ハーネスの結果で代用しない。

## 4. 印字は変換を含めて評価する

診断0の compiler 入力による Check KPC だけでは、Store → Pointer adapter と printer の費用を十分に評価できない。7d-2 の完了条件に以下を追加する。

| 入力 | 計測する操作 |
| --- | --- |
| compiler プロジェクトの型・symbol walk から固定した要求列 | TypeToString / SymbolToString を対応した対象・順番・flags で実行 |
| compiler / conformance から選んだ診断あり corpus | GetDiagnostics と suggestion / related information の生成。message の印字を含む |
| 大きい union / intersection / conditional、alias / unique symbol、source 再利用・Original / comment を含む例 | 長い型の構築・変換・印字、truncation の有無、cache miss / hit を区別 |

実入力を主たる証拠とし、最後の小さい例は経路の切り分けに使う。対象 file・位置・操作・flags・期待出力の hash と件数を manifest に固定し、Pointer と Store で同じ要求列を使う。型・symbol の identity の対応は §2 に従う。

主計測は **nodebuilder + Store → Pointer adapter + printer + 結果文字列取得** の全区間。Pointer は対応する nodebuilder + printer + 文字列取得を測る。変換を区間外に置かない。初回の新しい checker / cache miss と、同じ checker に同じ要求列を再実行する cache hit を別に報告し、都合のよい側だけを採らない。先に取得済みの結果文字列を返すだけの試験にしない。

- GC オフの KPC で inst / cycles / IPC、通常 GC の実行で ns/op・B/op・allocs/op、別実行で印字後 retained / GC を取る。raw と benchstat、環境と revision を保存する。
- 全区間とは別の instrumented run で nodebuilder / adapter / printer の内訳を取る。adapter の変換 node 数、memo hit、source 変換数、最大一時保持量を併記する。細かい計測の instrumentation を全区間のゲートへ混ぜない。
- 出力文字列は完全一致。Original / comments / 再利用部分木と再入印字の正しさも確認する。AST・EmitContext・対応表を作る費用も含める。
- 既存の Check cycles ≤1.10 × Pointer は維持するが、印字の合格を意味しない。印字全区間の代表入力で再現する退行があれば、Check が合格でも未解決として残し、自動的に採用しない。独自の許容率を新設せず、絶対増分・実 workload の頻度・allocation / retained との交換条件を示して採否を判断する。差が解決できなければ同等とは書かない。

## 5. 段ごとの完了条件

| 段 | このレビューで追加した必須事項 |
| --- | --- |
| 7d-0 | private 名を GetSymbolId へ統一、local id と gid の用途分離、複数 file / 同名 private の試験、採番契約の試験、variant 別プロセスの parse / bind retained / GC |
| 7d-2 | checker の private lookup、symbol の一対一対応と fallback 順序、数値一致を仮定しない等価、Pointer AST を持たない測定 adapter、印字全区間と内訳の性能評価 |

今回の検証は現行ソースと文書の整合確認のみ。選択 checkout は `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`。この追加契約を実装した KPC・retained・印字の artifact は `missing`。既存の時間測定から推定して埋めない。新たなベンチは実行していない。
